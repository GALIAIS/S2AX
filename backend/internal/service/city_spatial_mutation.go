package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

type cityGenerateChunkPayload struct {
	ChunkX int64 `json:"chunk_x"`
	ChunkY int64 `json:"chunk_y"`
	Z      int32 `json:"z"`
}

func isCitySpatialCommand(commandType string) bool {
	return commandType == CityCommandTypeSpatialGenerateChunk
}

func normalizeCitySpatialCommand(
	commandType string,
	rawPayload json.RawMessage,
) (any, bool, error) {
	if !isCitySpatialCommand(commandType) {
		return nil, false, nil
	}
	var payload cityGenerateChunkPayload
	if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
		return nil, true, ErrCityInvalidInput.WithCause(err)
	}
	if err := cityspatial.ValidateZ(payload.Z, cityspatial.MinimumZ, cityspatial.MaximumZ); err != nil {
		return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "z"}).WithCause(err)
	}
	return payload, true, nil
}

func (s *CityEconomyService) applyCitySpatialCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, tick, sequence int64,
	command *CityCommand,
) (cityPendingEvent, *CitySpatialMutation, error) {
	pending := cityPendingEvent{
		command: command, status: CityCommandStatusApplied,
		eventType: "city.spatial.chunk_generated",
		result:    map[string]any{"applied": true},
	}
	chunkX, okX := cityJSONInteger(command.Payload["chunk_x"])
	chunkY, okY := cityJSONInteger(command.Payload["chunk_y"])
	zValue, okZ := cityJSONInteger(command.Payload["z"])
	if !okX || !okY || !okZ || zValue < int64(cityspatial.MinimumZ) || zValue > int64(cityspatial.MaximumZ) {
		return rejectedCityCommand(command, "CITY_SPATIAL_COORDINATE_INVALID"), nil, nil
	}
	z := int32(zValue)
	var simulationVersion string
	var seed int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, seed FROM city_worlds WHERE id = $1`, worldID).
		Scan(&simulationVersion, &seed); err != nil {
		return pending, nil, fmt.Errorf("load city spatial generation world: %w", err)
	}
	profile, err := loadCitySpatialProfile(ctx, tx, worldID)
	if err != nil {
		return pending, nil, err
	}
	if z != cityspatial.SurfaceZ || chunkX < profile.MinimumChunkX || chunkX > profile.MaximumChunkX ||
		chunkY < profile.MinimumChunkY || chunkY > profile.MaximumChunkY {
		return rejectedCityCommand(command, "CITY_SPATIAL_COORDINATE_OUT_OF_BOUNDS"), nil, nil
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM city_map_chunks
    WHERE world_id = $1 AND chunk_x = $2 AND chunk_y = $3 AND z = $4
)`, worldID, chunkX, chunkY, z).Scan(&exists); err != nil {
		return pending, nil, fmt.Errorf("check city map chunk existence: %w", err)
	}
	if exists {
		return rejectedCityCommand(command, "CITY_SPATIAL_CHUNK_ALREADY_GENERATED"), nil, nil
	}
	generation, err := loadCitySpatialGenerationContext(
		ctx, tx, worldID, simulationVersion, seed, profile,
	)
	if err != nil {
		return pending, nil, err
	}
	expectedTile, found := spatialOvermapTileAt(generation.overmap, chunkX, chunkY, z)
	if !found {
		return rejectedCityCommand(command, "CITY_SPATIAL_COORDINATE_OUT_OF_BOUNDS"), nil, nil
	}
	districtID, storedTile, err := loadCityOvermapTileForGeneration(ctx, tx, worldID, chunkX, chunkY, z)
	if err != nil {
		return pending, nil, err
	}
	if storedTile != expectedTile {
		return pending, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_overmap_tile"})
	}
	generated, err := cityspatial.GenerateDefaultChunk(generation.binding, generation.ruleSet, expectedTile)
	if err != nil {
		return pending, nil, fmt.Errorf("generate city map chunk: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{"generation_proof": generated.GenerationProof})
	if err != nil {
		return pending, nil, err
	}
	var mutationID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_spatial_mutations
    (world_id, tick, sequence, source_command_id, mutation_type, expected_line_count, metadata)
VALUES ($1, $2, $3, $4, $5, 1, $6::jsonb)
RETURNING id`, worldID, tick, sequence, command.ID,
		CitySpatialMutationChunkGenerated, metadata).Scan(&mutationID); err != nil {
		return pending, nil, fmt.Errorf("insert city spatial mutation: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_spatial_mutation_id', $1, TRUE)`, strconv.FormatInt(mutationID, 10)); err != nil {
		return pending, nil, fmt.Errorf("activate city spatial mutation write gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_map_chunks
    (world_id, chunk_x, chunk_y, z, district_id, generator_id, generator_version,
     generation_proof, revision, payload, payload_hash, generated_tick,
     source_mutation_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9::jsonb, $10, $11, $12, '{}'::jsonb)`,
		worldID, chunkX, chunkY, z, districtID, profile.GeneratorID,
		profile.GeneratorVersion, generated.GenerationProof, generated.CanonicalPayload,
		generated.PayloadHash, tick, mutationID); err != nil {
		return pending, nil, fmt.Errorf("insert city map chunk: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_spatial_mutation_lines
    (mutation_id, world_id, line_no, chunk_x, chunk_y, z,
     revision_before, revision_after, payload_hash_before, payload_hash_after)
VALUES ($1, $2, 1, $3, $4, $5, 0, 1, NULL, $6)`,
		mutationID, worldID, chunkX, chunkY, z, generated.PayloadHash); err != nil {
		return pending, nil, fmt.Errorf("insert city spatial mutation line: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_spatial_mutations SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, mutationID); err != nil {
		return pending, nil, fmt.Errorf("post city spatial mutation: %w", err)
	}
	mutation, err := loadCitySpatialMutationByID(ctx, tx, worldID, mutationID)
	if err != nil {
		return pending, nil, err
	}
	pending.payload = map[string]any{
		"chunk_x": chunkX, "chunk_y": chunkY, "z": z,
		"mutation_id": mutationID, "payload_hash": generated.PayloadHash,
	}
	pending.result["mutation_id"] = mutationID
	pending.result["payload_hash"] = generated.PayloadHash
	pending.result["chunk_x"] = chunkX
	pending.result["chunk_y"] = chunkY
	pending.result["z"] = z
	return pending, mutation, nil
}

func loadCityOvermapTileForGeneration(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, chunkX, chunkY int64,
	z int32,
) (int64, cityspatial.OvermapTile, error) {
	var districtID int64
	var tile cityspatial.OvermapTile
	var roadMask, riverMask int
	err := queryer.QueryRowContext(ctx, `
SELECT tile.district_id, tile.chunk_x, tile.chunk_y, tile.z, district.code,
       tile.terrain_definition_id, tile.road_mask, tile.river_mask,
       tile.variant, tile.tile_hash
FROM city_overmap_tiles tile
JOIN city_districts district ON district.id = tile.district_id
WHERE tile.world_id = $1 AND tile.chunk_x = $2 AND tile.chunk_y = $3 AND tile.z = $4`,
		worldID, chunkX, chunkY, z).Scan(
		&districtID, &tile.ChunkX, &tile.ChunkY, &tile.Z, &tile.DistrictCode,
		&tile.TerrainID, &roadMask, &riverMask, &tile.Variant, &tile.TileHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, tile, ErrCitySpatialChunkNotFound
	}
	if err != nil {
		return 0, tile, fmt.Errorf("load city overmap tile for generation: %w", err)
	}
	tile.RoadMask, tile.RiverMask = uint8(roadMask), uint8(riverMask)
	return districtID, tile, nil
}

func loadCitySpatialMutationsForTick(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
) ([]*CitySpatialMutation, error) {
	return loadCitySpatialMutations(ctx, queryer, worldID, 0, 0, 0, tick)
}

func loadCitySpatialMutations(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, afterTick, afterSequence int64,
	limit int,
	exactTick int64,
) ([]*CitySpatialMutation, error) {
	query := `
SELECT id, world_id, tick, sequence, source_command_id, mutation_type,
       expected_line_count, metadata, posted_at, created_at
FROM city_spatial_mutations
WHERE world_id = $1 AND posted_at IS NOT NULL
  AND (tick > $2 OR (tick = $2 AND sequence > $3))
ORDER BY tick ASC, sequence ASC
LIMIT $4`
	args := []any{worldID, afterTick, afterSequence, limit}
	if exactTick > 0 {
		query = `
SELECT id, world_id, tick, sequence, source_command_id, mutation_type,
       expected_line_count, metadata, posted_at, created_at
FROM city_spatial_mutations
WHERE world_id = $1 AND tick = $2 AND posted_at IS NOT NULL
ORDER BY sequence ASC`
		args = []any{worldID, exactTick}
	}
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load city spatial mutations: %w", err)
	}
	items := make([]*CitySpatialMutation, 0)
	for rows.Next() {
		item := &CitySpatialMutation{Lines: make([]*CitySpatialMutationLine, 0, 1)}
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.Tick, &item.Sequence,
			&item.SourceCommandID, &item.MutationType, &item.ExpectedLineCount, &metadata,
			&item.PostedAt, &item.CreatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode city spatial mutation metadata: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city spatial mutations"); err != nil {
		return nil, err
	}
	for _, item := range items {
		if err = loadCitySpatialMutationLines(ctx, queryer, item); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func loadCitySpatialMutationByID(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, mutationID int64,
) (*CitySpatialMutation, error) {
	item := &CitySpatialMutation{Lines: make([]*CitySpatialMutationLine, 0, 1)}
	var metadata []byte
	err := queryer.QueryRowContext(ctx, `
SELECT id, world_id, tick, sequence, source_command_id, mutation_type,
       expected_line_count, metadata, posted_at, created_at
FROM city_spatial_mutations
WHERE world_id = $1 AND id = $2 AND posted_at IS NOT NULL`, worldID, mutationID).Scan(
		&item.ID, &item.WorldID, &item.Tick, &item.Sequence, &item.SourceCommandID,
		&item.MutationType, &item.ExpectedLineCount, &metadata, &item.PostedAt, &item.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("load city spatial mutation: %w", err)
	}
	item.Metadata, err = decodeCityJSONMap(metadata)
	if err != nil {
		return nil, err
	}
	if err = loadCitySpatialMutationLines(ctx, queryer, item); err != nil {
		return nil, err
	}
	return item, nil
}

func loadCitySpatialMutationLines(
	ctx context.Context,
	queryer citySQLQueryer,
	mutation *CitySpatialMutation,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT line_no, chunk_x, chunk_y, z, revision_before, revision_after,
       payload_hash_before, payload_hash_after
FROM city_spatial_mutation_lines
WHERE mutation_id = $1 AND world_id = $2
ORDER BY line_no ASC`, mutation.ID, mutation.WorldID)
	if err != nil {
		return fmt.Errorf("load city spatial mutation lines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		item := &CitySpatialMutationLine{}
		var before sql.NullString
		if err = rows.Scan(&item.LineNo, &item.ChunkX, &item.ChunkY, &item.Z,
			&item.RevisionBefore, &item.RevisionAfter, &before,
			&item.PayloadHashAfter); err != nil {
			return err
		}
		if before.Valid {
			item.PayloadHashBefore = &before.String
		}
		mutation.Lines = append(mutation.Lines, item)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(mutation.Lines) != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_mutation_lines"})
	}
	return nil
}

func replayCitySpatialMutations(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	if state == nil || state.Spatial == nil || !cityEngineSupportsSpatial(state.SimulationVersion) {
		return fmt.Errorf("spatial facts require a spatial-capable city engine")
	}
	mutations, err := loadCitySpatialMutationsForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	profile := citySpatialProfileFromHash(worldID, state.Spatial.Profile)
	ruleSet, err := loadBoundCitySpatialRuleSet(profile)
	if err != nil {
		return err
	}
	generatorVersion, err := citySpatialGeneratorVersion(state.SimulationVersion)
	if err != nil {
		return err
	}
	binding, err := cityspatial.DefaultGeneratorBinding(generatorVersion, state.Seed, ruleSet)
	if err != nil {
		return err
	}
	districtCodes := make([]string, 0, len(state.Physical.Districts))
	for _, district := range state.Physical.Districts {
		districtCodes = append(districtCodes, district.Code)
	}
	overmap, err := cityspatial.GenerateDefaultOvermap(binding, districtCodes)
	if err != nil || overmap.RootHash != state.Spatial.Overmap.RootHash ||
		overmap.SeedProof != state.Spatial.Overmap.SeedProof {
		return fmt.Errorf("replay spatial overmap binding is invalid")
	}
	index := make(map[string]struct{}, len(state.Spatial.Chunks)+len(mutations))
	for _, chunk := range state.Spatial.Chunks {
		index[cityspatial.StableChunkKey(cityspatial.ChunkCoordinate{X: chunk.ChunkX, Y: chunk.ChunkY, Z: chunk.Z})] = struct{}{}
	}
	for position, mutation := range mutations {
		if mutation.Sequence != int64(position+1) || mutation.MutationType != CitySpatialMutationChunkGenerated ||
			mutation.ExpectedLineCount != 1 ||
			len(mutation.Lines) != 1 {
			return fmt.Errorf("spatial mutation sequence or shape is invalid")
		}
		line := mutation.Lines[0]
		if line.LineNo != 1 || line.RevisionBefore != 0 || line.RevisionAfter != 1 ||
			line.PayloadHashBefore != nil {
			return fmt.Errorf("spatial mutation revision chain is invalid")
		}
		if err = verifyReplayedSpatialCommand(ctx, queryer, mutation, line); err != nil {
			return err
		}
		coordinate := cityspatial.ChunkCoordinate{X: line.ChunkX, Y: line.ChunkY, Z: line.Z}
		key := cityspatial.StableChunkKey(coordinate)
		if _, duplicate := index[key]; duplicate {
			return fmt.Errorf("spatial mutation generates an existing chunk")
		}
		tile, found := spatialOvermapTileAt(overmap, line.ChunkX, line.ChunkY, line.Z)
		if !found {
			return fmt.Errorf("spatial mutation is outside the overmap")
		}
		generated, generateErr := cityspatial.GenerateDefaultChunk(binding, ruleSet, tile)
		generationProof, proofOK := mutation.Metadata["generation_proof"].(string)
		if generateErr != nil || generated.PayloadHash != line.PayloadHashAfter ||
			!proofOK || generationProof != generated.GenerationProof {
			return fmt.Errorf("spatial mutation does not match deterministic mapgen")
		}
		state.Spatial.Chunks = append(state.Spatial.Chunks, cityHashSpatialChunk{
			ChunkX: line.ChunkX, ChunkY: line.ChunkY, Z: line.Z,
			DistrictCode: tile.DistrictCode, GeneratorID: binding.GeneratorID,
			GeneratorVersion: binding.GeneratorVersion, GenerationProof: generated.GenerationProof,
			Revision: line.RevisionAfter, PayloadHash: line.PayloadHashAfter, GeneratedTick: tick,
		})
		index[key] = struct{}{}
	}
	sort.Slice(state.Spatial.Chunks, func(i, j int) bool {
		left, right := state.Spatial.Chunks[i], state.Spatial.Chunks[j]
		if left.Z != right.Z {
			return left.Z < right.Z
		}
		if left.ChunkY != right.ChunkY {
			return left.ChunkY < right.ChunkY
		}
		return left.ChunkX < right.ChunkX
	})
	state.Spatial.ChunkCount = len(state.Spatial.Chunks)
	state.Spatial.ChunkHashRoot, err = citySpatialChunkHashRoot(state.Spatial.Chunks)
	return err
}

func verifyReplayedSpatialCommand(
	ctx context.Context,
	queryer citySQLQueryer,
	mutation *CitySpatialMutation,
	line *CitySpatialMutationLine,
) error {
	var commandType, status string
	var processedTick sql.NullInt64
	var payload []byte
	err := queryer.QueryRowContext(ctx, `
SELECT command_type, status, processed_tick, payload
FROM city_commands WHERE id = $1 AND world_id = $2`, mutation.SourceCommandID, mutation.WorldID).
		Scan(&commandType, &status, &processedTick, &payload)
	if err != nil {
		return fmt.Errorf("load replay spatial command: %w", err)
	}
	var value cityGenerateChunkPayload
	if err = json.Unmarshal(payload, &value); err != nil ||
		commandType != CityCommandTypeSpatialGenerateChunk || status != CityCommandStatusApplied ||
		!processedTick.Valid || processedTick.Int64 != mutation.Tick ||
		value.ChunkX != line.ChunkX || value.ChunkY != line.ChunkY || value.Z != line.Z {
		return fmt.Errorf("spatial mutation does not match its applied command")
	}
	return nil
}

func citySpatialProfileFromHash(worldID int64, profile cityHashSpatialProfile) *CitySpatialProfile {
	return &CitySpatialProfile{
		WorldID: worldID, RuleSetID: profile.RuleSetID, RuleSetVersion: profile.RuleSetVersion,
		RuleSetHash: profile.RuleSetHash, ChunkSize: profile.ChunkSize,
		MinimumZ: profile.MinimumZ, MaximumZ: profile.MaximumZ,
		GeneratorID: profile.GeneratorID, GeneratorVersion: profile.GeneratorVersion,
		MinimumChunkX: profile.MinimumChunkX, MaximumChunkX: profile.MaximumChunkX,
		MinimumChunkY: profile.MinimumChunkY, MaximumChunkY: profile.MaximumChunkY,
	}
}
