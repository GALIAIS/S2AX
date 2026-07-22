package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	cityOpenWorldSectorSizeChunks = int64(8)
	cityOpenWorldBootstrapMinimum = int64(-8)
	cityOpenWorldBootstrapMaximum = int64(7)
	cityOpenWorldMaximumAxis      = int64(256)
	cityOpenWorldRegionSizeChunks = int64(32)
	cityOpenWorldRegionsPerAxis   = cityOpenWorldRegionSizeChunks / cityOpenWorldSectorSizeChunks
	cityOpenWorldMaximumSectorAbs = int64(1_000_000)
	// Keep request coordinates bounded to the same finite world envelope used
	// by the V2 materialization command. Apart from being an explicit product
	// boundary, this makes all later chunk/sector arithmetic overflow-safe.
	cityOpenWorldMaximumWorldCoordinate = cityOpenWorldMaximumSectorAbs * cityOpenWorldSectorSizeChunks * cityspatial.DefaultChunkSize
	cityOpenWorldSpawnPolicy            = "city_center"
)

var ErrCityOpenWorldNotFound = infraerrors.NotFound(
	"CITY_OPEN_WORLD_NOT_FOUND", "open-world genesis state not found",
)

var ErrCityOpenWorldInteriorNotFound = infraerrors.NotFound(
	"CITY_OPEN_WORLD_INTERIOR_NOT_FOUND", "open-world building interior not found",
)

type CityOpenWorldBinding struct {
	WorldID           int64     `json:"world_id"`
	GeneratorID       string    `json:"generator_id"`
	GeneratorVersion  string    `json:"generator_version"`
	RuleSetID         string    `json:"rule_set_id"`
	RuleSetVersion    string    `json:"rule_set_version"`
	RuleSetHash       string    `json:"rule_set_hash"`
	ProfileID         string    `json:"profile_id"`
	ProfileVersion    string    `json:"profile_version"`
	ProfileHash       string    `json:"profile_hash"`
	ContextHash       string    `json:"context_hash"`
	Seed              int64     `json:"seed"`
	SpawnSectorX      int64     `json:"spawn_sector_x"`
	SpawnSectorY      int64     `json:"spawn_sector_y"`
	SpawnX            int64     `json:"spawn_x"`
	SpawnY            int64     `json:"spawn_y"`
	SpawnZ            int32     `json:"spawn_z"`
	Epoch             int64     `json:"epoch"`
	BootstrapPlanHash string    `json:"bootstrap_plan_hash"`
	GenesisHash       string    `json:"genesis_hash"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type CityOpenWorldSector struct {
	SectorX          int64     `json:"sector_x"`
	SectorY          int64     `json:"sector_y"`
	Epoch            int64     `json:"epoch"`
	ChunkSize        int64     `json:"chunk_size"`
	SectorSizeChunks int64     `json:"sector_size_chunks"`
	Status           string    `json:"status"`
	PlanHash         string    `json:"plan_hash"`
	ContentHash      string    `json:"content_hash"`
	GeneratedTick    int64     `json:"generated_tick"`
	Revision         int64     `json:"revision"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// CityOpenWorldRegion is the immutable planning boundary used by
// city-openworld-v2.  A sector never carries a standalone plan: all sectors
// in a region are materialized from the same recorded plan hash.
type CityOpenWorldRegion struct {
	RegionX          int64     `json:"region_x"`
	RegionY          int64     `json:"region_y"`
	Epoch            int64     `json:"epoch"`
	ChunkSize        int64     `json:"chunk_size"`
	RegionSizeChunks int64     `json:"region_size_chunks"`
	Status           string    `json:"status"`
	PlanHash         string    `json:"plan_hash"`
	GeneratedTick    int64     `json:"generated_tick"`
	Revision         int64     `json:"revision"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CityOpenWorldChunk struct {
	ChunkX      int64                             `json:"chunk_x"`
	ChunkY      int64                             `json:"chunk_y"`
	Z           int32                             `json:"z"`
	Payload     cityspatial.OpenWorldChunkPayload `json:"payload"`
	PayloadHash string                            `json:"payload_hash"`
	Revision    int64                             `json:"revision"`
}

type CityOpenWorldBuilding struct {
	Code                  string                      `json:"code"`
	CityCode              string                      `json:"city_code"`
	LotCode               string                      `json:"lot_code"`
	PrimaryUse            cityspatial.LandUse         `json:"primary_use"`
	ArchetypeCode         string                      `json:"archetype_code"`
	LayoutStyle           string                      `json:"layout_style"`
	FloorCount            int32                       `json:"floor_count"`
	Entrance              cityspatial.WorldgenPoint   `json:"entrance"`
	Footprint             []cityspatial.WorldgenPoint `json:"footprint"`
	FootprintHash         string                      `json:"footprint_hash"`
	InteriorFloorCount    int32                       `json:"interior_floor_count"`
	GroundInteriorVersion string                      `json:"ground_interior_version,omitempty"`
	GroundInteriorHash    string                      `json:"ground_interior_hash,omitempty"`
	Revision              int64                       `json:"revision"`
}

type CityOpenWorldBuildingInterior struct {
	BuildingCode  string                                      `json:"building_code"`
	FloorIndex    int32                                       `json:"floor_index"`
	Z             int32                                       `json:"z"`
	LayoutVersion string                                      `json:"layout_version"`
	LayoutStyle   string                                      `json:"layout_style"`
	Cells         []cityspatial.GeneratedWorldgenInteriorCell `json:"cells"`
	ContentHash   string                                      `json:"content_hash"`
	Revision      int64                                       `json:"revision"`
}

// CityOpenWorldPortal is immutable generated topology.  It says that two
// local-map positions connect; mutable lock/access state intentionally lives
// in a later runtime domain instead of rewriting world-generation facts.
type CityOpenWorldPortal struct {
	Code           string                    `json:"code"`
	BuildingCode   string                    `json:"building_code"`
	PortalType     string                    `json:"portal_type"`
	FromFloorIndex int32                     `json:"from_floor_index"`
	ToFloorIndex   int32                     `json:"to_floor_index"`
	From           cityspatial.WorldgenPoint `json:"from"`
	To             cityspatial.WorldgenPoint `json:"to"`
	Bidirectional  bool                      `json:"bidirectional"`
	TopologyHash   string                    `json:"topology_hash"`
	Revision       int64                     `json:"revision"`
}

type CityOpenWorldGenerationState struct {
	Binding CityOpenWorldBinding  `json:"binding"`
	Regions []CityOpenWorldRegion `json:"regions"`
	Sectors []CityOpenWorldSector `json:"sectors"`
}

type CityOpenWorldMapInput struct {
	UserID   int64
	WorldID  int64
	MinimumX int64
	MaximumX int64
	MinimumY int64
	MaximumY int64
	Z        int32
}

type CityOpenWorldMap struct {
	Binding   CityOpenWorldBinding    `json:"binding"`
	Chunks    []CityOpenWorldChunk    `json:"chunks"`
	Buildings []CityOpenWorldBuilding `json:"buildings"`
}

type cityOpenWorldHashState struct {
	Binding CityOpenWorldBindingHash `json:"binding"`
	// Regions was introduced with V2.  Omitting an empty value preserves the
	// byte-exact canonical form of already-created V1 snapshots.
	Regions   []CityOpenWorldRegionHash   `json:"regions,omitempty"`
	Sectors   []CityOpenWorldSectorHash   `json:"sectors"`
	Chunks    []CityOpenWorldChunkHash    `json:"chunks"`
	Buildings []CityOpenWorldBuildingHash `json:"buildings"`
	Interiors []CityOpenWorldInteriorHash `json:"interiors"`
	// Portals was introduced with V2 vertical topology.  Empty remains omitted
	// so historical V1 canonical snapshots retain their original byte shape.
	Portals []CityOpenWorldPortalHash `json:"portals,omitempty"`
}

type CityOpenWorldRegionHash struct {
	RegionX       int64  `json:"region_x"`
	RegionY       int64  `json:"region_y"`
	Epoch         int64  `json:"epoch"`
	PlanHash      string `json:"plan_hash"`
	GeneratedTick int64  `json:"generated_tick"`
	Revision      int64  `json:"revision"`
}

type CityOpenWorldBindingHash struct {
	GeneratorID       string `json:"generator_id"`
	GeneratorVersion  string `json:"generator_version"`
	RuleSetID         string `json:"rule_set_id"`
	RuleSetVersion    string `json:"rule_set_version"`
	RuleSetHash       string `json:"rule_set_hash"`
	ProfileID         string `json:"profile_id"`
	ProfileVersion    string `json:"profile_version"`
	ProfileHash       string `json:"profile_hash"`
	ContextHash       string `json:"context_hash"`
	Seed              int64  `json:"seed"`
	SpawnSectorX      int64  `json:"spawn_sector_x"`
	SpawnSectorY      int64  `json:"spawn_sector_y"`
	SpawnX            int64  `json:"spawn_x"`
	SpawnY            int64  `json:"spawn_y"`
	SpawnZ            int32  `json:"spawn_z"`
	Epoch             int64  `json:"epoch"`
	BootstrapPlanHash string `json:"bootstrap_plan_hash"`
	GenesisHash       string `json:"genesis_hash"`
}

type CityOpenWorldSectorHash struct {
	SectorX       int64  `json:"sector_x"`
	SectorY       int64  `json:"sector_y"`
	Epoch         int64  `json:"epoch"`
	PlanHash      string `json:"plan_hash"`
	ContentHash   string `json:"content_hash"`
	GeneratedTick int64  `json:"generated_tick"`
	Revision      int64  `json:"revision"`
}

type CityOpenWorldChunkHash struct {
	ChunkX      int64  `json:"chunk_x"`
	ChunkY      int64  `json:"chunk_y"`
	Z           int32  `json:"z"`
	PayloadHash string `json:"payload_hash"`
	Revision    int64  `json:"revision"`
}

type CityOpenWorldBuildingHash struct {
	Code          string `json:"code"`
	FootprintHash string `json:"footprint_hash"`
	Revision      int64  `json:"revision"`
}

type CityOpenWorldInteriorHash struct {
	BuildingCode  string `json:"building_code"`
	FloorIndex    int32  `json:"floor_index"`
	Z             int32  `json:"z"`
	LayoutVersion string `json:"layout_version"`
	ContentHash   string `json:"content_hash"`
	Revision      int64  `json:"revision"`
}

type CityOpenWorldPortalHash struct {
	Code         string `json:"code"`
	TopologyHash string `json:"topology_hash"`
	Revision     int64  `json:"revision"`
}

func (s *CityEconomyService) ListOpenWorldStyleProfiles(
	ctx context.Context,
	userID int64,
) ([]cityspatial.WorldgenProfileSummary, error) {
	if userID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	profiles, err := cityspatial.ListWorldgenProfiles()
	if err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_profile_catalog"}).WithCause(err)
	}
	return profiles, nil
}

func (s *CityEconomyService) GetOpenWorldStyleProfile(
	ctx context.Context,
	userID int64,
	profileID string,
) (*cityspatial.WorldgenProfile, error) {
	if userID <= 0 || strings.TrimSpace(profileID) == "" {
		return nil, ErrCityInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	profile, err := cityspatial.WorldgenProfileByID(profileID)
	if err != nil {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "style_profile_id"})
	}
	return profile, nil
}

func initializeCityOpenWorldFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, seed int64,
	simulationVersion, profileID, spawnPolicy string,
) error {
	switch simulationVersion {
	case CitySimulationVersionOpenWorld:
		return initializeCityOpenWorldV1Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV2:
		return initializeCityOpenWorldV2Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV3:
		return initializeCityOpenWorldV3Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV4:
		return initializeCityOpenWorldV4Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV5:
		return initializeCityOpenWorldV5Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV6:
		return initializeCityOpenWorldV6Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV7:
		return initializeCityOpenWorldV7Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV8:
		return initializeCityOpenWorldV8Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV9:
		return initializeCityOpenWorldV9Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV10:
		return initializeCityOpenWorldV10Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV11:
		return initializeCityOpenWorldV11Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV12:
		return initializeCityOpenWorldV12Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV13:
		return initializeCityOpenWorldV13Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV14:
		return initializeCityOpenWorldV14Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV15:
		return initializeCityOpenWorldV15Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV16:
		return initializeCityOpenWorldV16Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV17:
		return initializeCityOpenWorldV17Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV18:
		return initializeCityOpenWorldV18Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV19:
		return initializeCityOpenWorldV19Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV20:
		return initializeCityOpenWorldV20Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV21:
		return initializeCityOpenWorldV21Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV22:
		return initializeCityOpenWorldV22Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV23:
		return initializeCityOpenWorldV23Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	case CitySimulationVersionOpenWorldV24:
		return initializeCityOpenWorldV24Foundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
	default:
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
}

// initializeCityOpenWorldV1Foundation intentionally preserves the original
// one-sector bootstrap semantics for existing v1 worlds.  New worlds use the
// v2 region materializer below and never enter this path.
func initializeCityOpenWorldV1Foundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, seed int64,
	simulationVersion, profileID, spawnPolicy string,
) error {
	if simulationVersion != CitySimulationVersionOpenWorld || spawnPolicy != cityOpenWorldSpawnPolicy {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	profile, err := cityspatial.WorldgenProfileByID(profileID)
	if err != nil {
		return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "style_profile_id"})
	}
	registry, err := cityspatial.DefaultRegistry()
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_rule_registry"}).WithCause(err)
	}
	ruleSet, err := registry.Get(cityspatial.DefaultRuleSetID)
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_rule_set"}).WithCause(err)
	}
	if ruleSet.ChunkSize != cityspatial.DefaultChunkSize {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_rule_set_chunk_size"})
	}
	binding, err := cityspatial.DefaultOpenWorldgenBinding(simulationVersion, seed, profile)
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_binding"}).WithCause(err)
	}
	bootstrapBounds := cityspatial.WorldgenBounds{
		MinimumChunkX: cityOpenWorldBootstrapMinimum, MaximumChunkX: cityOpenWorldBootstrapMaximum,
		MinimumChunkY: cityOpenWorldBootstrapMinimum, MaximumChunkY: cityOpenWorldBootstrapMaximum,
		Z: cityspatial.SurfaceZ,
	}
	plan, err := cityspatial.GenerateWorldgenPlan(binding, profile, bootstrapBounds)
	if err != nil || len(plan.Cities) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_bootstrap_plan"}).WithCause(err)
	}
	spawn := plan.Cities[0].Center
	spawnAddress, err := cityspatial.SplitWorldCoordinate(cityspatial.WorldCoordinate{X: spawn.X, Y: spawn.Y, Z: spawn.Z}, cityspatial.DefaultChunkSize)
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_spawn"}).WithCause(err)
	}
	sectorX := cityOpenWorldFloorDiv(spawnAddress.Chunk.X, cityOpenWorldSectorSizeChunks)
	sectorY := cityOpenWorldFloorDiv(spawnAddress.Chunk.Y, cityOpenWorldSectorSizeChunks)
	sectorBounds := cityOpenWorldSectorBounds(sectorX, sectorY)
	surface, err := cityspatial.GenerateOpenWorldSurfaceSector(plan, sectorBounds)
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_surface"}).WithCause(err)
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_open_world_initialize_world_id', $1, TRUE)`, strconv.FormatInt(worldID, 10)); err != nil {
		return fmt.Errorf("activate open-world initialization write gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_bindings
    (world_id, generator_id, generator_version, rule_set_id, rule_set_version, rule_set_hash,
     profile_id, profile_version, profile_hash, context_hash, seed, spawn_sector_x, spawn_sector_y,
     spawn_x, spawn_y, spawn_z, epoch, bootstrap_plan_hash, genesis_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, 1, $17, $18, '{}'::jsonb)`,
		worldID, binding.GeneratorID, binding.GeneratorVersion, ruleSet.ID, ruleSet.Version, ruleSet.ContentHash,
		binding.ProfileID, binding.ProfileVersion, binding.ProfileHash, binding.SpatialRootHash, seed,
		sectorX, sectorY, spawn.X, spawn.Y, spawn.Z, plan.BaselineHash, surface.ContentHash); err != nil {
		return fmt.Errorf("insert open-world binding: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_sectors
    (world_id, sector_x, sector_y, epoch, chunk_size, sector_size_chunks, status,
     plan_hash, content_hash, generated_tick, revision, metadata)
VALUES ($1, $2, $3, 1, $4, $5, 'generated', $6, $7, 0, 1, '{}'::jsonb)`,
		worldID, sectorX, sectorY, cityspatial.DefaultChunkSize, cityOpenWorldSectorSizeChunks,
		plan.BaselineHash, surface.ContentHash); err != nil {
		return fmt.Errorf("insert open-world seed sector: %w", err)
	}
	chunkStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_open_world_chunks
    (world_id, sector_x, sector_y, epoch, chunk_x, chunk_y, z, payload, payload_hash, revision, metadata)
VALUES ($1, $2, $3, 1, $4, $5, $6, $7::jsonb, $8, 1, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare open-world chunk insert: %w", err)
	}
	defer func() { _ = chunkStatement.Close() }()
	for _, chunk := range surface.Chunks {
		if _, err = chunkStatement.ExecContext(ctx, worldID, sectorX, sectorY, chunk.Coordinate.X, chunk.Coordinate.Y,
			chunk.Coordinate.Z, chunk.CanonicalPayload, chunk.PayloadHash); err != nil {
			return fmt.Errorf("insert open-world chunk %d,%d: %w", chunk.Coordinate.X, chunk.Coordinate.Y, err)
		}
	}
	buildingStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_open_world_buildings
    (world_id, code, sector_x, sector_y, epoch, city_code, lot_code, primary_use,
     archetype_code, layout_style, floor_count, entrance_x, entrance_y, entrance_z,
     footprint, footprint_hash, revision, metadata)
VALUES ($1, $2, $3, $4, 1, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15, 1, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare open-world building insert: %w", err)
	}
	defer func() { _ = buildingStatement.Close() }()
	for _, building := range surface.Buildings {
		footprint, marshalErr := json.Marshal(building.Footprint)
		if marshalErr != nil {
			return fmt.Errorf("marshal open-world building %s footprint: %w", building.Code, marshalErr)
		}
		if _, err = buildingStatement.ExecContext(ctx, worldID, building.Code, sectorX, sectorY, building.CityCode,
			building.LotCode, building.PrimaryUse, building.ArchetypeCode, building.LayoutStyle, building.FloorCount,
			building.Entrance.X, building.Entrance.Y, building.Entrance.Z, footprint,
			cityOpenWorldFootprintHash(building.Footprint)); err != nil {
			return fmt.Errorf("insert open-world building %s: %w", building.Code, err)
		}
	}
	interiorStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_open_world_building_interiors
    (world_id, building_code, floor_index, z, layout_version, layout_style,
     cells, content_hash, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, 1, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare open-world interior insert: %w", err)
	}
	defer func() { _ = interiorStatement.Close() }()
	for _, interior := range surface.Interiors {
		cells, marshalErr := json.Marshal(interior.Cells)
		if marshalErr != nil {
			return fmt.Errorf("marshal open-world interior %s floor %d: %w", interior.BuildingCode, interior.FloorIndex, marshalErr)
		}
		if _, err = interiorStatement.ExecContext(ctx, worldID, interior.BuildingCode, interior.FloorIndex,
			interior.Z, interior.LayoutVersion, interior.LayoutStyle, cells, interior.ContentHash); err != nil {
			return fmt.Errorf("insert open-world interior %s floor %d: %w", interior.BuildingCode, interior.FloorIndex, err)
		}
	}
	return nil
}

func (s *CityEconomyService) GetOpenWorldGeneration(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldGenerationState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	binding, err := loadCityOpenWorldBinding(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	sectors, err := loadCityOpenWorldSectors(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	regions, err := loadCityOpenWorldRegions(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	return &CityOpenWorldGenerationState{Binding: *binding, Regions: regions, Sectors: sectors}, nil
}

func (s *CityEconomyService) GetOpenWorldMap(
	ctx context.Context,
	input CityOpenWorldMapInput,
) (*CityOpenWorldMap, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.MinimumX > input.MaximumX || input.MinimumY > input.MaximumY ||
		!cityOpenWorldValidWorldCoordinate(input.MinimumX) || !cityOpenWorldValidWorldCoordinate(input.MaximumX) ||
		!cityOpenWorldValidWorldCoordinate(input.MinimumY) || !cityOpenWorldValidWorldCoordinate(input.MaximumY) ||
		input.MaximumX-input.MinimumX+1 > cityOpenWorldMaximumAxis ||
		input.MaximumY-input.MinimumY+1 > cityOpenWorldMaximumAxis || input.Z != cityspatial.SurfaceZ {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	binding, err := loadCityOpenWorldBinding(ctx, s.db, input.WorldID)
	if err != nil {
		return nil, err
	}
	minimumChunkX := cityOpenWorldFloorDiv(input.MinimumX, cityspatial.DefaultChunkSize)
	maximumChunkX := cityOpenWorldFloorDiv(input.MaximumX, cityspatial.DefaultChunkSize)
	minimumChunkY := cityOpenWorldFloorDiv(input.MinimumY, cityspatial.DefaultChunkSize)
	maximumChunkY := cityOpenWorldFloorDiv(input.MaximumY, cityspatial.DefaultChunkSize)
	chunks, err := loadCityOpenWorldChunks(ctx, s.db, input.WorldID, minimumChunkX, maximumChunkX, minimumChunkY, maximumChunkY, input.Z)
	if err != nil {
		return nil, err
	}
	buildings, err := loadCityOpenWorldBuildings(ctx, s.db, input.WorldID, minimumChunkX, maximumChunkX, minimumChunkY, maximumChunkY)
	if err != nil {
		return nil, err
	}
	return &CityOpenWorldMap{Binding: *binding, Chunks: chunks, Buildings: buildings}, nil
}

func cityOpenWorldValidWorldCoordinate(value int64) bool {
	return value >= -cityOpenWorldMaximumWorldCoordinate && value <= cityOpenWorldMaximumWorldCoordinate
}

func (s *CityEconomyService) GetOpenWorldBuildingInterior(
	ctx context.Context,
	userID, worldID int64,
	buildingCode string,
	floorIndex int32,
) (*CityOpenWorldBuildingInterior, error) {
	if userID <= 0 || worldID <= 0 || strings.TrimSpace(buildingCode) == "" || floorIndex < 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	item := &CityOpenWorldBuildingInterior{}
	var cells []byte
	err := s.db.QueryRowContext(ctx, `
SELECT building_code, floor_index, z, layout_version, layout_style, cells, content_hash, revision
FROM city_open_world_building_interiors
WHERE world_id = $1 AND building_code = $2 AND floor_index = $3`,
		worldID, strings.TrimSpace(buildingCode), floorIndex,
	).Scan(&item.BuildingCode, &item.FloorIndex, &item.Z, &item.LayoutVersion, &item.LayoutStyle,
		&cells, &item.ContentHash, &item.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityOpenWorldInteriorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load open-world building interior: %w", err)
	}
	if err = json.Unmarshal(cells, &item.Cells); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_interior"}).WithCause(err)
	}
	contentHash, err := cityspatial.ComputeWorldgenBuildingInteriorHash(&cityspatial.GeneratedWorldgenBuildingInterior{
		BuildingCode: item.BuildingCode, FloorIndex: item.FloorIndex, Z: item.Z,
		LayoutVersion: item.LayoutVersion, LayoutStyle: item.LayoutStyle, Cells: item.Cells,
	})
	if err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_interior"}).WithCause(err)
	}
	if contentHash != item.ContentHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_interior_hash"})
	}
	return item, nil
}

func (s *CityEconomyService) ListOpenWorldBuildingPortals(
	ctx context.Context,
	userID, worldID int64,
	buildingCode string,
) ([]CityOpenWorldPortal, error) {
	if userID <= 0 || worldID <= 0 || strings.TrimSpace(buildingCode) == "" {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	items, err := loadCityOpenWorldPortals(ctx, s.db, worldID, strings.TrimSpace(buildingCode))
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		var exists bool
		if err = s.db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM city_open_world_buildings WHERE world_id = $1 AND code = $2
)`, worldID, strings.TrimSpace(buildingCode)).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check open-world building: %w", err)
		}
		if !exists {
			return nil, ErrCityOpenWorldInteriorNotFound
		}
	}
	return items, nil
}

func loadCityOpenWorldBinding(ctx context.Context, queryer citySQLQueryer, worldID int64) (*CityOpenWorldBinding, error) {
	item := &CityOpenWorldBinding{}
	err := queryer.QueryRowContext(ctx, `
SELECT world_id, generator_id, generator_version, rule_set_id, rule_set_version, rule_set_hash,
       profile_id, profile_version, profile_hash, context_hash, seed, spawn_sector_x, spawn_sector_y,
       spawn_x, spawn_y, spawn_z, epoch, bootstrap_plan_hash, genesis_hash, created_at, updated_at
FROM city_open_world_bindings WHERE world_id = $1`, worldID).Scan(
		&item.WorldID, &item.GeneratorID, &item.GeneratorVersion, &item.RuleSetID, &item.RuleSetVersion,
		&item.RuleSetHash, &item.ProfileID, &item.ProfileVersion, &item.ProfileHash, &item.ContextHash,
		&item.Seed, &item.SpawnSectorX, &item.SpawnSectorY, &item.SpawnX, &item.SpawnY, &item.SpawnZ,
		&item.Epoch, &item.BootstrapPlanHash, &item.GenesisHash, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityOpenWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load open-world binding: %w", err)
	}
	return item, nil
}

func loadCityOpenWorldSectors(ctx context.Context, queryer citySQLQueryer, worldID int64) ([]CityOpenWorldSector, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT sector_x, sector_y, epoch, chunk_size, sector_size_chunks, status, plan_hash,
       content_hash, generated_tick, revision, created_at, updated_at
FROM city_open_world_sectors WHERE world_id = $1
ORDER BY epoch ASC, sector_y ASC, sector_x ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world sectors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldSector, 0)
	for rows.Next() {
		var item CityOpenWorldSector
		if err = rows.Scan(&item.SectorX, &item.SectorY, &item.Epoch, &item.ChunkSize, &item.SectorSizeChunks,
			&item.Status, &item.PlanHash, &item.ContentHash, &item.GeneratedTick, &item.Revision,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan open-world sector: %w", err)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world sectors: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldRegions(ctx context.Context, queryer citySQLQueryer, worldID int64) ([]CityOpenWorldRegion, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT region_x, region_y, epoch, chunk_size, region_size_chunks, status,
       plan_hash, generated_tick, revision, created_at, updated_at
FROM city_open_world_regions WHERE world_id = $1
ORDER BY epoch ASC, region_y ASC, region_x ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world regions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldRegion, 0)
	for rows.Next() {
		var item CityOpenWorldRegion
		if err = rows.Scan(&item.RegionX, &item.RegionY, &item.Epoch, &item.ChunkSize,
			&item.RegionSizeChunks, &item.Status, &item.PlanHash, &item.GeneratedTick,
			&item.Revision, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan open-world region: %w", err)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world regions: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldPortals(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	buildingCode string,
) ([]CityOpenWorldPortal, error) {
	query := `
SELECT code, building_code, portal_type, from_floor_index, to_floor_index,
       from_x, from_y, from_z, to_x, to_y, to_z, bidirectional,
       topology_hash, revision
FROM city_open_world_portals
WHERE world_id = $1`
	arguments := []any{worldID}
	if buildingCode != "" {
		query += " AND building_code = $2"
		arguments = append(arguments, buildingCode)
	}
	query += " ORDER BY building_code ASC, from_floor_index ASC, to_floor_index ASC, code ASC"
	rows, err := queryer.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("load open-world portals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldPortal, 0)
	for rows.Next() {
		var item CityOpenWorldPortal
		if err = rows.Scan(
			&item.Code, &item.BuildingCode, &item.PortalType, &item.FromFloorIndex, &item.ToFloorIndex,
			&item.From.X, &item.From.Y, &item.From.Z, &item.To.X, &item.To.Y, &item.To.Z,
			&item.Bidirectional, &item.TopologyHash, &item.Revision,
		); err != nil {
			return nil, fmt.Errorf("scan open-world portal: %w", err)
		}
		expectedHash, hashErr := cityspatial.ComputeOpenWorldPortalHash(cityspatial.GeneratedOpenWorldPortal{
			Code: item.Code, BuildingCode: item.BuildingCode, PortalType: item.PortalType,
			FromFloorIndex: item.FromFloorIndex, ToFloorIndex: item.ToFloorIndex,
			From: item.From, To: item.To, Bidirectional: item.Bidirectional,
		})
		if hashErr != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_portal_hash"}).WithCause(hashErr)
		}
		if expectedHash != item.TopologyHash {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_portal_hash"})
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world portals: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldChunks(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, minimumChunkX, maximumChunkX, minimumChunkY, maximumChunkY int64,
	z int32,
) ([]CityOpenWorldChunk, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT chunk_x, chunk_y, z, payload, payload_hash, revision
FROM city_open_world_chunks
WHERE world_id = $1 AND z = $2 AND chunk_x BETWEEN $3 AND $4 AND chunk_y BETWEEN $5 AND $6
ORDER BY chunk_y ASC, chunk_x ASC`, worldID, z, minimumChunkX, maximumChunkX, minimumChunkY, maximumChunkY)
	if err != nil {
		return nil, fmt.Errorf("load open-world chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldChunk, 0)
	for rows.Next() {
		var item CityOpenWorldChunk
		var payload []byte
		if err = rows.Scan(&item.ChunkX, &item.ChunkY, &item.Z, &payload, &item.PayloadHash, &item.Revision); err != nil {
			return nil, fmt.Errorf("scan open-world chunk: %w", err)
		}
		if err = json.Unmarshal(payload, &item.Payload); err != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_chunk_payload"}).WithCause(err)
		}
		if err = cityspatial.ValidateOpenWorldChunkPayload(item.Payload); err != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_chunk_payload"}).WithCause(err)
		}
		canonical, marshalErr := json.Marshal(item.Payload)
		if marshalErr != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_chunk_hash"}).WithCause(marshalErr)
		}
		if cityOpenWorldPayloadHash(canonical) != item.PayloadHash {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_chunk_hash"})
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world chunks: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldBuildings(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, minimumChunkX, maximumChunkX, minimumChunkY, maximumChunkY int64,
) ([]CityOpenWorldBuilding, error) {
	// A V2 building is persisted by the sector containing its entrance, while
	// its footprint may cross one sector edge.  Read one owner-sector margin
	// and filter exact footprints below so inspection remains available from
	// every materialized chunk without duplicating building facts.
	minimumSectorX := cityOpenWorldFloorDiv(minimumChunkX, cityOpenWorldSectorSizeChunks) - 1
	maximumSectorX := cityOpenWorldFloorDiv(maximumChunkX, cityOpenWorldSectorSizeChunks) + 1
	minimumSectorY := cityOpenWorldFloorDiv(minimumChunkY, cityOpenWorldSectorSizeChunks) - 1
	maximumSectorY := cityOpenWorldFloorDiv(maximumChunkY, cityOpenWorldSectorSizeChunks) + 1
	rows, err := queryer.QueryContext(ctx, `
SELECT building.code, building.city_code, building.lot_code, building.primary_use, building.archetype_code,
       building.layout_style, building.floor_count, building.entrance_x, building.entrance_y,
       building.entrance_z, building.footprint, building.footprint_hash,
       COALESCE(interior.floor_count, 0), COALESCE(interior.ground_layout_version, ''),
       COALESCE(interior.ground_content_hash, ''), building.revision
FROM city_open_world_buildings AS building
LEFT JOIN (
    SELECT world_id, building_code, COUNT(*)::INTEGER AS floor_count,
           MAX(layout_version) FILTER (WHERE floor_index = 0) AS ground_layout_version,
           MAX(content_hash) FILTER (WHERE floor_index = 0) AS ground_content_hash
    FROM city_open_world_building_interiors
    WHERE world_id = $1
    GROUP BY world_id, building_code
) AS interior ON interior.world_id = building.world_id AND interior.building_code = building.code
WHERE building.world_id = $1 AND building.sector_x BETWEEN $2 AND $3 AND building.sector_y BETWEEN $4 AND $5
ORDER BY building.code ASC`, worldID, minimumSectorX, maximumSectorX, minimumSectorY, maximumSectorY)
	if err != nil {
		return nil, fmt.Errorf("load open-world buildings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldBuilding, 0)
	for rows.Next() {
		var item CityOpenWorldBuilding
		var footprint []byte
		if err = rows.Scan(&item.Code, &item.CityCode, &item.LotCode, &item.PrimaryUse, &item.ArchetypeCode,
			&item.LayoutStyle, &item.FloorCount, &item.Entrance.X, &item.Entrance.Y, &item.Entrance.Z,
			&footprint, &item.FootprintHash, &item.InteriorFloorCount, &item.GroundInteriorVersion,
			&item.GroundInteriorHash, &item.Revision); err != nil {
			return nil, fmt.Errorf("scan open-world building: %w", err)
		}
		if err = json.Unmarshal(footprint, &item.Footprint); err != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_building"}).WithCause(err)
		}
		if cityOpenWorldFootprintHash(item.Footprint) != item.FootprintHash {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_building"})
		}
		if !cityOpenWorldFootprintIntersectsChunkWindow(
			item.Footprint, minimumChunkX, maximumChunkX, minimumChunkY, maximumChunkY,
		) {
			continue
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world buildings: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldHashState(ctx context.Context, queryer citySQLQueryer, worldID int64) (*cityOpenWorldHashState, error) {
	binding, err := loadCityOpenWorldBinding(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	sectors, err := loadCityOpenWorldSectors(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	regions, err := loadCityOpenWorldRegions(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state := &cityOpenWorldHashState{
		Binding: CityOpenWorldBindingHash{
			GeneratorID: binding.GeneratorID, GeneratorVersion: binding.GeneratorVersion,
			RuleSetID: binding.RuleSetID, RuleSetVersion: binding.RuleSetVersion, RuleSetHash: binding.RuleSetHash,
			ProfileID: binding.ProfileID, ProfileVersion: binding.ProfileVersion, ProfileHash: binding.ProfileHash,
			ContextHash: binding.ContextHash, Seed: binding.Seed, SpawnSectorX: binding.SpawnSectorX,
			SpawnSectorY: binding.SpawnSectorY, SpawnX: binding.SpawnX, SpawnY: binding.SpawnY,
			SpawnZ: binding.SpawnZ, Epoch: binding.Epoch, BootstrapPlanHash: binding.BootstrapPlanHash,
			GenesisHash: binding.GenesisHash,
		},
		Regions: make([]CityOpenWorldRegionHash, 0, len(regions)),
		Sectors: make([]CityOpenWorldSectorHash, 0, len(sectors)),
		Chunks:  make([]CityOpenWorldChunkHash, 0), Buildings: make([]CityOpenWorldBuildingHash, 0),
		Interiors: make([]CityOpenWorldInteriorHash, 0),
		Portals:   make([]CityOpenWorldPortalHash, 0),
	}
	for _, region := range regions {
		state.Regions = append(state.Regions, CityOpenWorldRegionHash{
			RegionX: region.RegionX, RegionY: region.RegionY, Epoch: region.Epoch,
			PlanHash: region.PlanHash, GeneratedTick: region.GeneratedTick, Revision: region.Revision,
		})
	}
	for _, sector := range sectors {
		state.Sectors = append(state.Sectors, CityOpenWorldSectorHash{
			SectorX: sector.SectorX, SectorY: sector.SectorY, Epoch: sector.Epoch,
			PlanHash: sector.PlanHash, ContentHash: sector.ContentHash, GeneratedTick: sector.GeneratedTick, Revision: sector.Revision,
		})
	}
	portals, err := loadCityOpenWorldPortals(ctx, queryer, worldID, "")
	if err != nil {
		return nil, err
	}
	for _, portal := range portals {
		state.Portals = append(state.Portals, CityOpenWorldPortalHash{
			Code: portal.Code, TopologyHash: portal.TopologyHash, Revision: portal.Revision,
		})
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT chunk_x, chunk_y, z, payload_hash, revision
FROM city_open_world_chunks WHERE world_id = $1
ORDER BY z ASC, chunk_y ASC, chunk_x ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world chunk hashes: %w", err)
	}
	for rows.Next() {
		var item CityOpenWorldChunkHash
		if err = rows.Scan(&item.ChunkX, &item.ChunkY, &item.Z, &item.PayloadHash, &item.Revision); err != nil {
			_ = rows.Close()
			return nil, err
		}
		state.Chunks = append(state.Chunks, item)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	rows, err = queryer.QueryContext(ctx, `
SELECT building_code, floor_index, z, layout_version, content_hash, revision
FROM city_open_world_building_interiors
WHERE world_id = $1
ORDER BY building_code ASC, floor_index ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world interior hashes: %w", err)
	}
	for rows.Next() {
		var item CityOpenWorldInteriorHash
		if err = rows.Scan(&item.BuildingCode, &item.FloorIndex, &item.Z, &item.LayoutVersion, &item.ContentHash, &item.Revision); err != nil {
			_ = rows.Close()
			return nil, err
		}
		state.Interiors = append(state.Interiors, item)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	rows, err = queryer.QueryContext(ctx, `
SELECT code, footprint_hash, revision
FROM city_open_world_buildings WHERE world_id = $1 ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world building hashes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item CityOpenWorldBuildingHash
		if err = rows.Scan(&item.Code, &item.FootprintHash, &item.Revision); err != nil {
			return nil, err
		}
		state.Buildings = append(state.Buildings, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return state, nil
}

func cityOpenWorldSectorBounds(sectorX, sectorY int64) cityspatial.WorldgenBounds {
	return cityspatial.WorldgenBounds{
		MinimumChunkX: sectorX * cityOpenWorldSectorSizeChunks,
		MaximumChunkX: sectorX*cityOpenWorldSectorSizeChunks + cityOpenWorldSectorSizeChunks - 1,
		MinimumChunkY: sectorY * cityOpenWorldSectorSizeChunks,
		MaximumChunkY: sectorY*cityOpenWorldSectorSizeChunks + cityOpenWorldSectorSizeChunks - 1,
		Z:             cityspatial.SurfaceZ,
	}
}

func cityOpenWorldFloorDiv(value, divisor int64) int64 {
	quotient := value / divisor
	remainder := value % divisor
	if remainder < 0 {
		quotient--
	}
	return quotient
}

func cityOpenWorldPayloadHash(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func cityOpenWorldFootprintHash(footprint []cityspatial.WorldgenPoint) string {
	raw, err := json.Marshal(footprint)
	if err != nil {
		return ""
	}
	return cityOpenWorldPayloadHash(raw)
}

func cityOpenWorldFootprintIntersectsChunkWindow(
	footprint []cityspatial.WorldgenPoint,
	minimumChunkX, maximumChunkX, minimumChunkY, maximumChunkY int64,
) bool {
	for _, point := range footprint {
		chunkX := cityOpenWorldFloorDiv(point.X, cityspatial.DefaultChunkSize)
		chunkY := cityOpenWorldFloorDiv(point.Y, cityspatial.DefaultChunkSize)
		if chunkX >= minimumChunkX && chunkX <= maximumChunkX &&
			chunkY >= minimumChunkY && chunkY <= maximumChunkY {
			return true
		}
	}
	return false
}
