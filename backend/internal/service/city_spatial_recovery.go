package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

func restoreCitySpatialProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	state cityHashState,
) (int, error) {
	if !cityEngineSupportsSpatial(state.SimulationVersion) || state.Spatial == nil {
		return 0, fmt.Errorf("recovery F7 snapshot is missing spatial state")
	}
	spatial := state.Spatial
	profile := citySpatialProfileFromHash(worldID, spatial.Profile)
	profile.OvermapRevision = spatial.Overmap.Revision
	profile.OvermapSeedProof = spatial.Overmap.SeedProof
	profile.OvermapRootHash = spatial.Overmap.RootHash
	ruleSet, err := loadBoundCitySpatialRuleSet(profile)
	if err != nil {
		return 0, err
	}
	generatorVersion, err := citySpatialGeneratorVersion(state.SimulationVersion)
	if err != nil {
		return 0, err
	}
	binding, err := cityspatial.DefaultGeneratorBinding(generatorVersion, state.Seed, ruleSet)
	if err != nil {
		return 0, err
	}
	districtIDs, districtCodes, err := loadCitySpatialDistrictCatalog(ctx, tx, worldID)
	if err != nil {
		return 0, err
	}
	overmap, err := cityspatial.GenerateDefaultOvermap(binding, districtCodes)
	if err != nil {
		return 0, err
	}
	if overmap.RootHash != spatial.Overmap.RootHash || overmap.SeedProof != spatial.Overmap.SeedProof ||
		len(overmap.Tiles) != spatial.Overmap.TileCount {
		return 0, fmt.Errorf("recovery spatial overmap does not match snapshot")
	}
	root, err := citySpatialChunkHashRoot(spatial.Chunks)
	if err != nil || root != spatial.ChunkHashRoot || len(spatial.Chunks) != spatial.ChunkCount {
		return 0, fmt.Errorf("recovery spatial chunk root does not match snapshot")
	}

	count := 0
	deleteProjection := func(label, query string) error {
		result, execErr := tx.ExecContext(ctx, query, worldID)
		if execErr != nil {
			return fmt.Errorf("restore %s: %w", label, execErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("count restored %s: %w", label, rowsErr)
		}
		count += int(rows)
		return nil
	}
	if err = deleteProjection("city map chunks", `DELETE FROM city_map_chunks WHERE world_id = $1`); err != nil {
		return 0, err
	}
	if err = deleteProjection("city overmap tiles", `DELETE FROM city_overmap_tiles WHERE world_id = $1`); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO city_spatial_profiles
    (world_id, rule_set_id, rule_set_version, rule_set_hash, chunk_size,
     minimum_z, maximum_z, generator_id, generator_version,
     minimum_chunk_x, maximum_chunk_x, minimum_chunk_y, maximum_chunk_y,
     overmap_seed_proof, overmap_root_hash, overmap_revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17::jsonb)
ON CONFLICT (world_id) DO UPDATE SET
    rule_set_id = EXCLUDED.rule_set_id,
    rule_set_version = EXCLUDED.rule_set_version,
    rule_set_hash = EXCLUDED.rule_set_hash,
    chunk_size = EXCLUDED.chunk_size,
    minimum_z = EXCLUDED.minimum_z,
    maximum_z = EXCLUDED.maximum_z,
    generator_id = EXCLUDED.generator_id,
    generator_version = EXCLUDED.generator_version,
    minimum_chunk_x = EXCLUDED.minimum_chunk_x,
    maximum_chunk_x = EXCLUDED.maximum_chunk_x,
    minimum_chunk_y = EXCLUDED.minimum_chunk_y,
    maximum_chunk_y = EXCLUDED.maximum_chunk_y,
    overmap_seed_proof = EXCLUDED.overmap_seed_proof,
    overmap_root_hash = EXCLUDED.overmap_root_hash,
    overmap_revision = EXCLUDED.overmap_revision,
    metadata = EXCLUDED.metadata,
    updated_at = NOW()`, worldID, spatial.Profile.RuleSetID, spatial.Profile.RuleSetVersion,
		spatial.Profile.RuleSetHash, spatial.Profile.ChunkSize, spatial.Profile.MinimumZ,
		spatial.Profile.MaximumZ, spatial.Profile.GeneratorID, spatial.Profile.GeneratorVersion,
		spatial.Profile.MinimumChunkX, spatial.Profile.MaximumChunkX,
		spatial.Profile.MinimumChunkY, spatial.Profile.MaximumChunkY,
		spatial.Overmap.SeedProof, spatial.Overmap.RootHash, spatial.Overmap.Revision,
		[]byte(spatial.Profile.Metadata))
	if err != nil {
		return 0, fmt.Errorf("restore city spatial profile: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return 0, fmt.Errorf("restore city spatial profile affected %d rows", rows)
	}
	count++

	overmapStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_overmap_tiles
    (world_id, chunk_x, chunk_y, z, district_id, terrain_definition_id,
     road_mask, river_mask, variant, tile_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '{}'::jsonb)`)
	if err != nil {
		return 0, fmt.Errorf("prepare recovery overmap insert: %w", err)
	}
	defer func() { _ = overmapStatement.Close() }()
	for _, tile := range overmap.Tiles {
		districtID, ok := districtIDs[tile.DistrictCode]
		if !ok {
			return 0, fmt.Errorf("recovery overmap references unknown district %q", tile.DistrictCode)
		}
		if _, err = overmapStatement.ExecContext(ctx, worldID, tile.ChunkX, tile.ChunkY,
			tile.Z, districtID, tile.TerrainID, tile.RoadMask, tile.RiverMask,
			tile.Variant, tile.TileHash); err != nil {
			return 0, fmt.Errorf("restore city overmap tile %d,%d: %w", tile.ChunkX, tile.ChunkY, err)
		}
		count++
	}

	chunkStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_map_chunks
    (world_id, chunk_x, chunk_y, z, district_id, generator_id, generator_version,
     generation_proof, revision, payload, payload_hash, generated_tick,
     source_mutation_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, $13, '{}'::jsonb)`)
	if err != nil {
		return 0, fmt.Errorf("prepare recovery chunk insert: %w", err)
	}
	defer func() { _ = chunkStatement.Close() }()
	for _, expected := range spatial.Chunks {
		tile, found := spatialOvermapTileAt(overmap, expected.ChunkX, expected.ChunkY, expected.Z)
		if !found || tile.DistrictCode != expected.DistrictCode {
			return 0, fmt.Errorf("recovery chunk does not match overmap")
		}
		generated, generateErr := cityspatial.GenerateDefaultChunk(binding, ruleSet, tile)
		if generateErr != nil || expected.Revision != 1 || expected.GeneratedTick <= 0 ||
			expected.GeneratorID != binding.GeneratorID ||
			expected.GeneratorVersion != binding.GeneratorVersion ||
			expected.GenerationProof != generated.GenerationProof ||
			expected.PayloadHash != generated.PayloadHash {
			return 0, fmt.Errorf("recovery chunk does not match deterministic mapgen")
		}
		var mutationID int64
		if err = tx.QueryRowContext(ctx, `
SELECT mutation.id
FROM city_spatial_mutations mutation
JOIN city_spatial_mutation_lines line ON line.mutation_id = mutation.id
WHERE mutation.world_id = $1 AND mutation.tick = $2 AND mutation.posted_at IS NOT NULL
  AND line.chunk_x = $3 AND line.chunk_y = $4 AND line.z = $5
  AND line.payload_hash_after = $6`, worldID, expected.GeneratedTick,
			expected.ChunkX, expected.ChunkY, expected.Z, expected.PayloadHash).Scan(&mutationID); err != nil {
			return 0, fmt.Errorf("load recovery spatial mutation: %w", err)
		}
		districtID := districtIDs[expected.DistrictCode]
		if _, err = chunkStatement.ExecContext(ctx, worldID, expected.ChunkX, expected.ChunkY,
			expected.Z, districtID, expected.GeneratorID, expected.GeneratorVersion,
			expected.GenerationProof, expected.Revision, generated.CanonicalPayload,
			expected.PayloadHash, expected.GeneratedTick, mutationID); err != nil {
			return 0, fmt.Errorf("restore city map chunk %d,%d,%d: %w",
				expected.ChunkX, expected.ChunkY, expected.Z, err)
		}
		count++
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_spatial_foundation($1)`, worldID); err != nil {
		return 0, fmt.Errorf("validate recovered city spatial foundation: %w", err)
	}
	return count, nil
}
