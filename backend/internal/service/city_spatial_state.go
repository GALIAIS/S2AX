package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CityCommandTypeSpatialGenerateChunk = "spatial.generate_chunk"
	CitySpatialMutationChunkGenerated   = "chunk_generated"
	CitySpatialDefaultMinimumChunk      = cityspatial.DefaultOvermapMinimum
	CitySpatialDefaultMaximumChunk      = cityspatial.DefaultOvermapMaximum
	CitySpatialDefaultSurfaceZ          = cityspatial.SurfaceZ

	citySpatialDefaultChangeLimit = 50
	citySpatialMaximumChangeLimit = 200
	citySpatialMaximumQueryAxis   = 9
)

var (
	ErrCitySpatialStateNotFound = infraerrors.NotFound(
		"CITY_SPATIAL_STATE_NOT_FOUND", "city spatial state not found",
	)
	ErrCitySpatialChunkNotFound = infraerrors.NotFound(
		"CITY_SPATIAL_CHUNK_NOT_FOUND", "city spatial chunk not found",
	)
)

type CitySpatialProfile struct {
	WorldID          int64          `json:"world_id"`
	RuleSetID        string         `json:"rule_set_id"`
	RuleSetVersion   string         `json:"rule_set_version"`
	RuleSetHash      string         `json:"rule_set_hash"`
	ChunkSize        int64          `json:"chunk_size"`
	MinimumZ         int32          `json:"minimum_z"`
	MaximumZ         int32          `json:"maximum_z"`
	GeneratorID      string         `json:"generator_id"`
	GeneratorVersion string         `json:"generator_version"`
	MinimumChunkX    int64          `json:"minimum_chunk_x"`
	MaximumChunkX    int64          `json:"maximum_chunk_x"`
	MinimumChunkY    int64          `json:"minimum_chunk_y"`
	MaximumChunkY    int64          `json:"maximum_chunk_y"`
	OvermapSeedProof string         `json:"overmap_seed_proof"`
	OvermapRootHash  string         `json:"overmap_root_hash"`
	OvermapRevision  int64          `json:"overmap_revision"`
	Metadata         map[string]any `json:"metadata"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type CityWorldSpatialRuleSet struct {
	Profile *CitySpatialProfile `json:"profile"`
	RuleSet *CitySpatialRuleSet `json:"rule_set"`
}

type CityOvermapTile struct {
	ChunkX              int64          `json:"chunk_x"`
	ChunkY              int64          `json:"chunk_y"`
	Z                   int32          `json:"z"`
	DistrictCode        string         `json:"district_code"`
	TerrainDefinitionID string         `json:"terrain_definition_id"`
	RoadMask            uint8          `json:"road_mask"`
	RiverMask           uint8          `json:"river_mask"`
	Variant             int            `json:"variant"`
	TileHash            string         `json:"tile_hash"`
	Metadata            map[string]any `json:"metadata"`
}

type CityOvermapState struct {
	Profile *CitySpatialProfile `json:"profile"`
	Tiles   []*CityOvermapTile  `json:"tiles"`
}

type CityMapChunkSummary struct {
	ChunkX           int64  `json:"chunk_x"`
	ChunkY           int64  `json:"chunk_y"`
	Z                int32  `json:"z"`
	DistrictCode     string `json:"district_code"`
	GeneratorID      string `json:"generator_id"`
	GeneratorVersion string `json:"generator_version"`
	GenerationProof  string `json:"generation_proof"`
	Revision         int64  `json:"revision"`
	PayloadHash      string `json:"payload_hash"`
	GeneratedTick    int64  `json:"generated_tick"`
}

type CityMapChunk struct {
	CityMapChunkSummary
	WorldID          int64                    `json:"world_id"`
	RuleSetHash      string                   `json:"rule_set_hash"`
	Payload          cityspatial.ChunkPayload `json:"payload"`
	Metadata         map[string]any           `json:"metadata"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
	sourceMutationID int64
}

type CityMapChunkListInput struct {
	UserID   int64
	WorldID  int64
	MinimumX int64
	MaximumX int64
	MinimumY int64
	MaximumY int64
	Z        int32
}

type CitySpatialMutationLine struct {
	LineNo            int     `json:"line_no"`
	ChunkX            int64   `json:"chunk_x"`
	ChunkY            int64   `json:"chunk_y"`
	Z                 int32   `json:"z"`
	RevisionBefore    int64   `json:"revision_before"`
	RevisionAfter     int64   `json:"revision_after"`
	PayloadHashBefore *string `json:"payload_hash_before,omitempty"`
	PayloadHashAfter  string  `json:"payload_hash_after"`
}

type CitySpatialMutation struct {
	ID                int64                      `json:"id"`
	WorldID           int64                      `json:"world_id"`
	Tick              int64                      `json:"tick"`
	Sequence          int64                      `json:"sequence"`
	SourceCommandID   int64                      `json:"source_command_id"`
	MutationType      string                     `json:"mutation_type"`
	ExpectedLineCount int                        `json:"expected_line_count"`
	Metadata          map[string]any             `json:"metadata"`
	PostedAt          time.Time                  `json:"posted_at"`
	CreatedAt         time.Time                  `json:"created_at"`
	Lines             []*CitySpatialMutationLine `json:"lines"`
}

type CitySpatialMutationCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type CitySpatialMutationPage struct {
	Items      []*CitySpatialMutation     `json:"items"`
	NextCursor *CitySpatialMutationCursor `json:"next_cursor,omitempty"`
}

type CitySpatialMutationListInput struct {
	UserID        int64
	WorldID       int64
	AfterTick     int64
	AfterSequence int64
	Limit         int
}

type cityHashSpatialProfile struct {
	RuleSetID        string          `json:"rule_set_id"`
	RuleSetVersion   string          `json:"rule_set_version"`
	RuleSetHash      string          `json:"rule_set_hash"`
	ChunkSize        int64           `json:"chunk_size"`
	MinimumZ         int32           `json:"minimum_z"`
	MaximumZ         int32           `json:"maximum_z"`
	GeneratorID      string          `json:"generator_id"`
	GeneratorVersion string          `json:"generator_version"`
	MinimumChunkX    int64           `json:"minimum_chunk_x"`
	MaximumChunkX    int64           `json:"maximum_chunk_x"`
	MinimumChunkY    int64           `json:"minimum_chunk_y"`
	MaximumChunkY    int64           `json:"maximum_chunk_y"`
	Metadata         json.RawMessage `json:"metadata"`
}

type cityHashSpatialOvermap struct {
	Revision  int64  `json:"revision"`
	TileCount int    `json:"tile_count"`
	SeedProof string `json:"seed_proof"`
	RootHash  string `json:"root_hash"`
}

type cityHashSpatialChunk struct {
	ChunkX           int64  `json:"chunk_x"`
	ChunkY           int64  `json:"chunk_y"`
	Z                int32  `json:"z"`
	DistrictCode     string `json:"district_code"`
	GeneratorID      string `json:"generator_id"`
	GeneratorVersion string `json:"generator_version"`
	GenerationProof  string `json:"generation_proof"`
	Revision         int64  `json:"revision"`
	PayloadHash      string `json:"payload_hash"`
	GeneratedTick    int64  `json:"generated_tick"`
}

type citySpatialHashState struct {
	Profile       cityHashSpatialProfile `json:"profile"`
	Overmap       cityHashSpatialOvermap `json:"overmap"`
	ChunkCount    int                    `json:"chunk_count"`
	ChunkHashRoot string                 `json:"chunk_hash_root"`
	Chunks        []cityHashSpatialChunk `json:"chunks"`
}

type citySpatialGenerationContext struct {
	profile *CitySpatialProfile
	ruleSet *cityspatial.RuleSet
	binding cityspatial.GeneratorBinding
	overmap *cityspatial.Overmap
}

func initializeCityF7SpatialFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, seed int64,
	simulationVersion string,
) error {
	generatorVersion, err := citySpatialGeneratorVersion(simulationVersion)
	if err != nil {
		return ErrCitySimulationVersion.WithCause(err)
	}
	registry, err := cityspatial.DefaultRegistry()
	if err != nil {
		return ErrCitySpatialRuleSetInvalid.WithCause(err)
	}
	ruleSet, err := registry.Get(cityspatial.DefaultRuleSetID)
	if err != nil {
		return ErrCitySpatialRuleSetInvalid.WithCause(err)
	}
	binding, err := cityspatial.DefaultGeneratorBinding(generatorVersion, seed, ruleSet)
	if err != nil {
		return ErrCitySpatialRuleSetInvalid.WithCause(err)
	}
	districtIDs, districtCodes, err := loadCitySpatialDistrictCatalog(ctx, tx, worldID)
	if err != nil {
		return err
	}
	overmap, err := cityspatial.GenerateDefaultOvermap(binding, districtCodes)
	if err != nil {
		return fmt.Errorf("generate city F7 overmap: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_f7_initialize_world_id', $1, TRUE)`, strconv.FormatInt(worldID, 10)); err != nil {
		return fmt.Errorf("activate city F7 initialization write gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_spatial_profiles
    (world_id, rule_set_id, rule_set_version, rule_set_hash, chunk_size,
     minimum_z, maximum_z, generator_id, generator_version,
     minimum_chunk_x, maximum_chunk_x, minimum_chunk_y, maximum_chunk_y,
     overmap_seed_proof, overmap_root_hash, overmap_revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, 1, '{}'::jsonb)`,
		worldID, ruleSet.ID, ruleSet.Version, ruleSet.ContentHash, ruleSet.ChunkSize,
		ruleSet.MinimumZ, ruleSet.MaximumZ, binding.GeneratorID, binding.GeneratorVersion,
		overmap.MinimumChunkX, overmap.MaximumChunkX, overmap.MinimumChunkY, overmap.MaximumChunkY,
		overmap.SeedProof, overmap.RootHash); err != nil {
		return fmt.Errorf("insert city F7 spatial profile: %w", err)
	}
	statement, err := tx.PrepareContext(ctx, `
INSERT INTO city_overmap_tiles
    (world_id, chunk_x, chunk_y, z, district_id, terrain_definition_id,
     road_mask, river_mask, variant, tile_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare city F7 overmap insert: %w", err)
	}
	defer func() { _ = statement.Close() }()
	for _, tile := range overmap.Tiles {
		districtID, ok := districtIDs[tile.DistrictCode]
		if !ok {
			return fmt.Errorf("city F7 overmap references unknown district %q", tile.DistrictCode)
		}
		if _, err = statement.ExecContext(ctx, worldID, tile.ChunkX, tile.ChunkY, tile.Z,
			districtID, tile.TerrainID, tile.RoadMask, tile.RiverMask, tile.Variant, tile.TileHash); err != nil {
			return fmt.Errorf("insert city F7 overmap tile %d,%d: %w", tile.ChunkX, tile.ChunkY, err)
		}
	}
	return nil
}

func (s *CityEconomyService) GetWorldSpatialRuleSet(
	ctx context.Context,
	userID, worldID int64,
) (*CityWorldSpatialRuleSet, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	profile, err := loadCitySpatialProfile(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	ruleSet, err := loadBoundCitySpatialRuleSet(profile)
	if err != nil {
		return nil, err
	}
	return &CityWorldSpatialRuleSet{Profile: profile, RuleSet: ruleSet}, nil
}

func (s *CityEconomyService) GetOvermap(
	ctx context.Context,
	userID, worldID int64,
) (*CityOvermapState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	profile, err := loadCitySpatialProfile(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	tiles, err := loadCityOvermapTiles(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	return &CityOvermapState{Profile: profile, Tiles: tiles}, nil
}

func (s *CityEconomyService) ListMapChunks(
	ctx context.Context,
	input CityMapChunkListInput,
) ([]*CityMapChunkSummary, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.MinimumX > input.MaximumX ||
		input.MinimumY > input.MaximumY || input.MaximumX-input.MinimumX+1 > citySpatialMaximumQueryAxis ||
		input.MaximumY-input.MinimumY+1 > citySpatialMaximumQueryAxis ||
		(input.MaximumX-input.MinimumX+1)*(input.MaximumY-input.MinimumY+1) >
			citySpatialMaximumQueryAxis*citySpatialMaximumQueryAxis {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	profile, err := loadCitySpatialProfile(ctx, s.db, input.WorldID)
	if err != nil {
		return nil, err
	}
	if input.MinimumX < profile.MinimumChunkX || input.MaximumX > profile.MaximumChunkX ||
		input.MinimumY < profile.MinimumChunkY || input.MaximumY > profile.MaximumChunkY ||
		input.Z < profile.MinimumZ || input.Z > profile.MaximumZ {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "spatial_bounds"})
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT chunk.chunk_x, chunk.chunk_y, chunk.z, district.code,
       chunk.generator_id, chunk.generator_version, chunk.generation_proof,
       chunk.revision, chunk.payload_hash, chunk.generated_tick
FROM city_map_chunks chunk
JOIN city_districts district ON district.id = chunk.district_id
WHERE chunk.world_id = $1 AND chunk.chunk_x BETWEEN $2 AND $3
  AND chunk.chunk_y BETWEEN $4 AND $5 AND chunk.z = $6
ORDER BY chunk.z ASC, chunk.chunk_y ASC, chunk.chunk_x ASC`, input.WorldID,
		input.MinimumX, input.MaximumX, input.MinimumY, input.MaximumY, input.Z)
	if err != nil {
		return nil, fmt.Errorf("list city map chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityMapChunkSummary, 0)
	for rows.Next() {
		item := &CityMapChunkSummary{}
		if err = scanCityMapChunkSummary(rows, item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city map chunks: %w", err)
	}
	generation, err := loadCitySpatialGenerationContextForWorld(ctx, s.db, input.WorldID, profile)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if _, err = validateCityMapChunkSummary(generation, item); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *CityEconomyService) GetMapChunk(
	ctx context.Context,
	userID, worldID, chunkX, chunkY int64,
	z int32,
) (*CityMapChunk, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	profile, err := loadCitySpatialProfile(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	if chunkX < profile.MinimumChunkX || chunkX > profile.MaximumChunkX ||
		chunkY < profile.MinimumChunkY || chunkY > profile.MaximumChunkY ||
		z < profile.MinimumZ || z > profile.MaximumZ {
		return nil, ErrCitySpatialChunkNotFound
	}
	item, err := loadCityMapChunk(ctx, s.db, worldID, chunkX, chunkY, z, profile)
	if err != nil {
		return nil, err
	}
	generation, err := loadCitySpatialGenerationContextForWorld(ctx, s.db, worldID, profile)
	if err != nil {
		return nil, err
	}
	generated, err := validateCityMapChunkSummary(generation, &item.CityMapChunkSummary)
	if err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(item.Payload)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, generated.CanonicalPayload) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_mapgen_payload"})
	}
	return item, nil
}

func (s *CityEconomyService) ListSpatialMutations(
	ctx context.Context,
	input CitySpatialMutationListInput,
) (*CitySpatialMutationPage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterTick < 0 || input.AfterSequence < 0 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = citySpatialDefaultChangeLimit
	}
	if input.Limit > citySpatialMaximumChangeLimit {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	items, err := loadCitySpatialMutations(ctx, s.db, input.WorldID,
		input.AfterTick, input.AfterSequence, input.Limit+1, 0)
	if err != nil {
		return nil, err
	}
	page := &CitySpatialMutationPage{Items: items}
	if len(items) > input.Limit {
		page.Items = items[:input.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = &CitySpatialMutationCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	return page, nil
}

func loadCitySpatialProfile(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CitySpatialProfile, error) {
	item := &CitySpatialProfile{}
	var metadata []byte
	err := queryer.QueryRowContext(ctx, `
SELECT world_id, rule_set_id, rule_set_version, rule_set_hash, chunk_size,
       minimum_z, maximum_z, generator_id, generator_version,
       minimum_chunk_x, maximum_chunk_x, minimum_chunk_y, maximum_chunk_y,
       overmap_seed_proof, overmap_root_hash, overmap_revision, metadata,
       created_at, updated_at
FROM city_spatial_profiles WHERE world_id = $1`, worldID).Scan(
		&item.WorldID, &item.RuleSetID, &item.RuleSetVersion, &item.RuleSetHash,
		&item.ChunkSize, &item.MinimumZ, &item.MaximumZ, &item.GeneratorID,
		&item.GeneratorVersion, &item.MinimumChunkX, &item.MaximumChunkX,
		&item.MinimumChunkY, &item.MaximumChunkY, &item.OvermapSeedProof,
		&item.OvermapRootHash, &item.OvermapRevision, &metadata,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySpatialStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load city spatial profile: %w", err)
	}
	item.Metadata, err = decodeCityJSONMap(metadata)
	if err != nil {
		return nil, fmt.Errorf("decode city spatial profile metadata: %w", err)
	}
	return item, nil
}

func loadCityOvermapTiles(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]*CityOvermapTile, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT tile.chunk_x, tile.chunk_y, tile.z, district.code,
       tile.terrain_definition_id, tile.road_mask, tile.river_mask,
       tile.variant, tile.tile_hash, tile.metadata
FROM city_overmap_tiles tile
JOIN city_districts district ON district.id = tile.district_id
WHERE tile.world_id = $1
ORDER BY tile.z ASC, tile.chunk_y ASC, tile.chunk_x ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city overmap tiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityOvermapTile, 0, 81)
	for rows.Next() {
		item := &CityOvermapTile{}
		var roadMask, riverMask int
		var metadata []byte
		if err = rows.Scan(&item.ChunkX, &item.ChunkY, &item.Z, &item.DistrictCode,
			&item.TerrainDefinitionID, &roadMask, &riverMask, &item.Variant,
			&item.TileHash, &metadata); err != nil {
			return nil, err
		}
		item.RoadMask, item.RiverMask = uint8(roadMask), uint8(riverMask)
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			return nil, fmt.Errorf("decode city overmap tile metadata: %w", err)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city overmap tiles: %w", err)
	}
	return items, nil
}

func loadCityMapChunk(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, chunkX, chunkY int64,
	z int32,
	profile *CitySpatialProfile,
) (*CityMapChunk, error) {
	item := &CityMapChunk{WorldID: worldID, RuleSetHash: profile.RuleSetHash}
	var payload, metadata []byte
	err := queryer.QueryRowContext(ctx, `
SELECT chunk.chunk_x, chunk.chunk_y, chunk.z, district.code,
       chunk.generator_id, chunk.generator_version, chunk.generation_proof,
       chunk.revision, chunk.payload_hash, chunk.generated_tick,
       chunk.payload, chunk.metadata, chunk.created_at, chunk.updated_at,
       chunk.source_mutation_id
FROM city_map_chunks chunk
JOIN city_districts district ON district.id = chunk.district_id
WHERE chunk.world_id = $1 AND chunk.chunk_x = $2 AND chunk.chunk_y = $3 AND chunk.z = $4`,
		worldID, chunkX, chunkY, z).Scan(
		&item.ChunkX, &item.ChunkY, &item.Z, &item.DistrictCode,
		&item.GeneratorID, &item.GeneratorVersion, &item.GenerationProof,
		&item.Revision, &item.PayloadHash, &item.GeneratedTick,
		&payload, &metadata, &item.CreatedAt, &item.UpdatedAt, &item.sourceMutationID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySpatialChunkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load city map chunk: %w", err)
	}
	ruleSet, err := loadBoundCitySpatialRuleSet(profile)
	if err != nil {
		return nil, err
	}
	canonical, err := decodeAndCanonicalizeCityChunkPayload(ruleSet, payload, &item.Payload)
	if err != nil {
		return nil, err
	}
	if sha256HexService(canonical) != item.PayloadHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_payload_hash"})
	}
	item.Metadata, err = decodeCityJSONMap(metadata)
	if err != nil {
		return nil, fmt.Errorf("decode city map chunk metadata: %w", err)
	}
	return item, nil
}

func loadCitySpatialHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	simulationVersion string,
	seed int64,
) (*citySpatialHashState, error) {
	profile, err := loadCitySpatialProfile(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	generation, err := loadCitySpatialGenerationContext(ctx, queryer, worldID, simulationVersion, seed, profile)
	if err != nil {
		return nil, err
	}
	storedTiles, err := loadCityOvermapTiles(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	if err = verifyCityOvermapProjection(generation.overmap, storedTiles); err != nil {
		return nil, err
	}
	chunks, err := loadCitySpatialChunkHashSummaries(ctx, queryer, worldID, generation)
	if err != nil {
		return nil, err
	}
	root, err := citySpatialChunkHashRoot(chunks)
	if err != nil {
		return nil, err
	}
	profileMetadata, err := json.Marshal(profile.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city spatial profile metadata: %w", err)
	}
	return &citySpatialHashState{
		Profile: cityHashSpatialProfile{
			RuleSetID: profile.RuleSetID, RuleSetVersion: profile.RuleSetVersion,
			RuleSetHash: profile.RuleSetHash, ChunkSize: profile.ChunkSize,
			MinimumZ: profile.MinimumZ, MaximumZ: profile.MaximumZ,
			GeneratorID: profile.GeneratorID, GeneratorVersion: profile.GeneratorVersion,
			MinimumChunkX: profile.MinimumChunkX, MaximumChunkX: profile.MaximumChunkX,
			MinimumChunkY: profile.MinimumChunkY, MaximumChunkY: profile.MaximumChunkY,
			Metadata: profileMetadata,
		},
		Overmap: cityHashSpatialOvermap{
			Revision: profile.OvermapRevision, TileCount: len(storedTiles),
			SeedProof: profile.OvermapSeedProof, RootHash: profile.OvermapRootHash,
		},
		ChunkCount: len(chunks), ChunkHashRoot: root, Chunks: chunks,
	}, nil
}

func loadCitySpatialGenerationContext(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	simulationVersion string,
	seed int64,
	profile *CitySpatialProfile,
) (*citySpatialGenerationContext, error) {
	generatorVersion, err := citySpatialGeneratorVersion(simulationVersion)
	if err != nil {
		return nil, ErrCitySimulationVersion.WithCause(err)
	}
	ruleSet, err := loadBoundCitySpatialRuleSet(profile)
	if err != nil {
		return nil, err
	}
	binding, err := cityspatial.DefaultGeneratorBinding(generatorVersion, seed, ruleSet)
	if err != nil || binding.GeneratorID != profile.GeneratorID ||
		binding.GeneratorVersion != profile.GeneratorVersion {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_generator_binding"})
	}
	_, districtCodes, err := loadCitySpatialDistrictCatalog(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	overmap, err := cityspatial.GenerateDefaultOvermap(binding, districtCodes)
	if err != nil {
		return nil, fmt.Errorf("regenerate city overmap: %w", err)
	}
	if overmap.SeedProof != profile.OvermapSeedProof || overmap.RootHash != profile.OvermapRootHash ||
		overmap.MinimumChunkX != profile.MinimumChunkX || overmap.MaximumChunkX != profile.MaximumChunkX ||
		overmap.MinimumChunkY != profile.MinimumChunkY || overmap.MaximumChunkY != profile.MaximumChunkY {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_overmap_root"})
	}
	return &citySpatialGenerationContext{profile: profile, ruleSet: ruleSet, binding: binding, overmap: overmap}, nil
}

func loadCitySpatialGenerationContextForWorld(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	profile *CitySpatialProfile,
) (*citySpatialGenerationContext, error) {
	var simulationVersion string
	var seed int64
	if err := queryer.QueryRowContext(ctx, `
SELECT simulation_version, seed FROM city_worlds WHERE id = $1`, worldID).
		Scan(&simulationVersion, &seed); err != nil {
		return nil, fmt.Errorf("load city spatial world binding: %w", err)
	}
	if !cityEngineSupportsSpatial(simulationVersion) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	return loadCitySpatialGenerationContext(ctx, queryer, worldID, simulationVersion, seed, profile)
}

func loadBoundCitySpatialRuleSet(profile *CitySpatialProfile) (*cityspatial.RuleSet, error) {
	if profile == nil {
		return nil, ErrCitySpatialStateNotFound
	}
	registry, err := cityspatial.DefaultRegistry()
	if err != nil {
		return nil, ErrCitySpatialRuleSetInvalid.WithCause(err)
	}
	ruleSet, err := registry.Get(profile.RuleSetID)
	if err != nil || ruleSet.Version != profile.RuleSetVersion || ruleSet.ContentHash != profile.RuleSetHash ||
		ruleSet.ChunkSize != profile.ChunkSize || ruleSet.MinimumZ != profile.MinimumZ ||
		ruleSet.MaximumZ != profile.MaximumZ {
		return nil, ErrCitySpatialRuleSetInvalid.WithCause(err)
	}
	return ruleSet, nil
}

func loadCitySpatialDistrictCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (map[string]int64, []string, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, code FROM city_districts WHERE world_id = $1 ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, nil, fmt.Errorf("load city spatial districts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := make(map[string]int64, 6)
	codes := make([]string, 0, 6)
	for rows.Next() {
		var id int64
		var code string
		if err = rows.Scan(&id, &code); err != nil {
			return nil, nil, err
		}
		ids[code] = id
		codes = append(codes, code)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	return ids, codes, nil
}

func verifyCityOvermapProjection(expected *cityspatial.Overmap, actual []*CityOvermapTile) error {
	if expected == nil || len(expected.Tiles) != len(actual) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_overmap_tiles"})
	}
	for index, expectedTile := range expected.Tiles {
		actualTile := actual[index]
		if actualTile.ChunkX != expectedTile.ChunkX || actualTile.ChunkY != expectedTile.ChunkY ||
			actualTile.Z != expectedTile.Z || actualTile.DistrictCode != expectedTile.DistrictCode ||
			actualTile.TerrainDefinitionID != expectedTile.TerrainID || actualTile.RoadMask != expectedTile.RoadMask ||
			actualTile.RiverMask != expectedTile.RiverMask || actualTile.Variant != expectedTile.Variant ||
			actualTile.TileHash != expectedTile.TileHash {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_overmap_tile"})
		}
	}
	return nil
}

func loadCitySpatialChunkHashSummaries(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	generation *citySpatialGenerationContext,
) ([]cityHashSpatialChunk, error) {
	if generation == nil || generation.profile == nil || generation.ruleSet == nil || generation.overmap == nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_generation_context"})
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT chunk.chunk_x, chunk.chunk_y, chunk.z, district.code,
       chunk.generator_id, chunk.generator_version, chunk.generation_proof,
       chunk.revision, chunk.payload_hash, chunk.generated_tick, chunk.payload
FROM city_map_chunks chunk
JOIN city_districts district ON district.id = chunk.district_id
WHERE chunk.world_id = $1
ORDER BY chunk.z ASC, chunk.chunk_y ASC, chunk.chunk_x ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city spatial chunks for hash: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]cityHashSpatialChunk, 0)
	for rows.Next() {
		var item cityHashSpatialChunk
		var payload []byte
		if err = rows.Scan(&item.ChunkX, &item.ChunkY, &item.Z, &item.DistrictCode,
			&item.GeneratorID, &item.GeneratorVersion, &item.GenerationProof,
			&item.Revision, &item.PayloadHash, &item.GeneratedTick, &payload); err != nil {
			return nil, err
		}
		var decoded cityspatial.ChunkPayload
		canonical, canonicalErr := decodeAndCanonicalizeCityChunkPayload(generation.ruleSet, payload, &decoded)
		summary := CityMapChunkSummary{
			ChunkX: item.ChunkX, ChunkY: item.ChunkY, Z: item.Z,
			DistrictCode: item.DistrictCode, GeneratorID: item.GeneratorID,
			GeneratorVersion: item.GeneratorVersion, GenerationProof: item.GenerationProof,
			Revision: item.Revision, PayloadHash: item.PayloadHash, GeneratedTick: item.GeneratedTick,
		}
		generated, summaryErr := validateCityMapChunkSummary(generation, &summary)
		if canonicalErr != nil || sha256HexService(canonical) != item.PayloadHash || summaryErr != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_payload_hash"})
		}
		if !bytes.Equal(canonical, generated.CanonicalPayload) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_mapgen_payload"})
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city spatial chunks for hash: %w", err)
	}
	return items, nil
}

func validateCityMapChunkSummary(
	generation *citySpatialGenerationContext,
	item *CityMapChunkSummary,
) (*cityspatial.GeneratedChunk, error) {
	if generation == nil || generation.profile == nil || generation.ruleSet == nil ||
		generation.overmap == nil || item == nil || item.Revision != 1 || item.GeneratedTick <= 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_chunk_summary"})
	}
	tile, found := spatialOvermapTileAt(generation.overmap, item.ChunkX, item.ChunkY, item.Z)
	if !found || tile.DistrictCode != item.DistrictCode ||
		item.GeneratorID != generation.binding.GeneratorID ||
		item.GeneratorVersion != generation.binding.GeneratorVersion {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_chunk_binding"})
	}
	generated, err := cityspatial.GenerateDefaultChunk(generation.binding, generation.ruleSet, tile)
	if err != nil || item.GenerationProof != generated.GenerationProof || item.PayloadHash != generated.PayloadHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_chunk_generation"})
	}
	return generated, nil
}

func citySpatialChunkHashRoot(chunks []cityHashSpatialChunk) (string, error) {
	for index := range chunks {
		if index > 0 {
			previous, current := chunks[index-1], chunks[index]
			if current.Z < previous.Z || current.Z == previous.Z &&
				(current.ChunkY < previous.ChunkY || current.ChunkY == previous.ChunkY && current.ChunkX <= previous.ChunkX) {
				return "", fmt.Errorf("city spatial chunks are not in canonical order")
			}
		}
	}
	raw, err := json.Marshal(chunks)
	if err != nil {
		return "", fmt.Errorf("marshal city spatial chunk root: %w", err)
	}
	return sha256HexService(raw), nil
}

func decodeAndCanonicalizeCityChunkPayload(
	ruleSet *cityspatial.RuleSet,
	raw []byte,
	target *cityspatial.ChunkPayload,
) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_payload"}).WithCause(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_payload"})
	}
	if err := cityspatial.ValidateChunkPayload(ruleSet, *target); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "spatial_payload"}).WithCause(err)
	}
	canonical, err := json.Marshal(target)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func scanCityMapChunkSummary(scanner cityScannable, item *CityMapChunkSummary) error {
	return scanner.Scan(&item.ChunkX, &item.ChunkY, &item.Z, &item.DistrictCode,
		&item.GeneratorID, &item.GeneratorVersion, &item.GenerationProof,
		&item.Revision, &item.PayloadHash, &item.GeneratedTick)
}

func sha256HexService(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func spatialOvermapTileAt(overmap *cityspatial.Overmap, x, y int64, z int32) (cityspatial.OvermapTile, bool) {
	if overmap == nil {
		return cityspatial.OvermapTile{}, false
	}
	index := sort.Search(len(overmap.Tiles), func(index int) bool {
		tile := overmap.Tiles[index]
		return tile.Z > z || tile.Z == z && (tile.ChunkY > y || tile.ChunkY == y && tile.ChunkX >= x)
	})
	if index >= len(overmap.Tiles) {
		return cityspatial.OvermapTile{}, false
	}
	tile := overmap.Tiles[index]
	return tile, tile.ChunkX == x && tile.ChunkY == y && tile.Z == z
}
