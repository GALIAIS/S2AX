package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

const cityRealtimeSpatialEpoch int64 = 1

// CityRealtimeSpatialBinding is the immutable semantic identity of a
// realtime-v2 world's generated space. It is deliberately asset-agnostic:
// visual packs resolve these semantic facts later and cannot change the
// physical map or the world's canonical state hash.
type CityRealtimeSpatialBinding struct {
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
	ChunkSize         int64  `json:"chunk_size"`
	SectorSizeChunks  int64  `json:"sector_size_chunks"`
	Epoch             int64  `json:"epoch"`
	BootstrapPlanHash string `json:"bootstrap_plan_hash"`
	GenesisHash       string `json:"genesis_hash"`
}

type cityRealtimeSpatialHashState struct {
	Binding   CityRealtimeSpatialBinding        `json:"binding"`
	Regions   []cityRealtimeSpatialRegionHash   `json:"regions"`
	Sectors   []cityRealtimeSpatialSectorHash   `json:"sectors"`
	Chunks    []cityRealtimeSpatialChunkHash    `json:"chunks"`
	Buildings []cityRealtimeSpatialBuildingHash `json:"buildings"`
	Interiors []cityRealtimeSpatialInteriorHash `json:"interiors"`
	Portals   []cityRealtimeSpatialPortalHash   `json:"portals"`
}

type cityRealtimeSpatialRegionHash struct {
	RegionX                   int64  `json:"region_x"`
	RegionY                   int64  `json:"region_y"`
	Epoch                     int64  `json:"epoch"`
	PlanHash                  string `json:"plan_hash"`
	MaterializedFrameSequence int64  `json:"materialized_frame_sequence"`
	Revision                  int64  `json:"revision"`
}

type cityRealtimeSpatialSectorHash struct {
	SectorX                   int64  `json:"sector_x"`
	SectorY                   int64  `json:"sector_y"`
	Epoch                     int64  `json:"epoch"`
	PlanHash                  string `json:"plan_hash"`
	ContentHash               string `json:"content_hash"`
	MaterializedFrameSequence int64  `json:"materialized_frame_sequence"`
	Revision                  int64  `json:"revision"`
}

type cityRealtimeSpatialChunkHash struct {
	ChunkX      int64  `json:"chunk_x"`
	ChunkY      int64  `json:"chunk_y"`
	Z           int32  `json:"z"`
	PayloadHash string `json:"payload_hash"`
	Revision    int64  `json:"revision"`
}

type cityRealtimeSpatialBuildingHash struct {
	Code          string `json:"code"`
	FootprintHash string `json:"footprint_hash"`
	Revision      int64  `json:"revision"`
}

type cityRealtimeSpatialInteriorHash struct {
	BuildingCode  string `json:"building_code"`
	FloorIndex    int32  `json:"floor_index"`
	Z             int32  `json:"z"`
	LayoutVersion string `json:"layout_version"`
	ContentHash   string `json:"content_hash"`
	Revision      int64  `json:"revision"`
}

type cityRealtimeSpatialPortalHash struct {
	Code         string `json:"code"`
	TopologyHash string `json:"topology_hash"`
	Revision     int64  `json:"revision"`
}

// initializeCityRealtimeStaticWorldgenFoundation binds only the v2 realtime
// engine to a V3 planner and a V2 vertical surface sector. The legacy
// city_open_world_* namespace has its own tick/materialization invariants and
// must not be reused here.
func initializeCityRealtimeStaticWorldgenFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, seed int64,
	simulationVersion, profileID, spawnPolicy string,
) error {
	if worldID <= 0 || seed <= 0 || !cityEngineSupportsRealtimeStaticWorldgen(simulationVersion) ||
		spawnPolicy != cityOpenWorldSpawnPolicy {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	profile, err := cityspatial.WorldgenProfileByID(profileID)
	if err != nil {
		return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "style_profile_id"})
	}
	registry, err := cityspatial.DefaultRegistry()
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_rule_registry"}).WithCause(err)
	}
	ruleSet, err := registry.Get(cityspatial.DefaultRuleSetID)
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_rule_set"}).WithCause(err)
	}
	if ruleSet.ChunkSize != cityspatial.DefaultChunkSize {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_chunk_size"})
	}
	binding, err := cityspatial.DefaultOpenWorldgenBindingV3(simulationVersion, seed, profile)
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_binding"}).WithCause(err)
	}
	regionX, regionY := int64(0), int64(0)
	regionBounds := cityOpenWorldRegionBounds(regionX, regionY)
	plan, err := cityspatial.GenerateWorldgenPlan(binding, profile, regionBounds)
	if err != nil || len(plan.Cities) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_bootstrap_plan"}).WithCause(err)
	}
	spawn := plan.Cities[0].Center
	spawnAddress, err := cityspatial.SplitWorldCoordinate(
		cityspatial.WorldCoordinate{X: spawn.X, Y: spawn.Y, Z: spawn.Z}, cityspatial.DefaultChunkSize,
	)
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_spawn"}).WithCause(err)
	}
	sectorX, sectorY := cityOpenWorldSectorForChunk(spawnAddress.Chunk.X, spawnAddress.Chunk.Y)
	if expectedRegionX, expectedRegionY := cityOpenWorldRegionForSector(sectorX, sectorY); expectedRegionX != regionX || expectedRegionY != regionY {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_spawn_region"})
	}
	surface, err := cityspatial.GenerateOpenWorldSurfaceSectorV2(plan, cityOpenWorldSectorBounds(sectorX, sectorY))
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_surface"}).WithCause(err)
	}
	if surface.PlanHash != plan.BaselineHash || surface.ContentHash == "" {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_surface_hash"})
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_static_worldgen_initialize_world_id', $1, TRUE)`, strconv.FormatInt(worldID, 10)); err != nil {
		return fmt.Errorf("activate realtime static worldgen write gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_spatial_bindings
    (world_id, generator_id, generator_version, rule_set_id, rule_set_version, rule_set_hash,
     profile_id, profile_version, profile_hash, context_hash, seed, spawn_sector_x, spawn_sector_y,
     spawn_x, spawn_y, spawn_z, epoch, bootstrap_plan_hash, genesis_hash, genesis_frame_sequence, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, 0, '{}'::jsonb)`,
		worldID, binding.GeneratorID, binding.GeneratorVersion, ruleSet.ID, ruleSet.Version, ruleSet.ContentHash,
		binding.ProfileID, binding.ProfileVersion, binding.ProfileHash, binding.SpatialRootHash, seed,
		sectorX, sectorY, spawn.X, spawn.Y, spawn.Z, cityRealtimeSpatialEpoch,
		plan.BaselineHash, surface.ContentHash,
	); err != nil {
		return fmt.Errorf("insert realtime static binding: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_spatial_regions
    (world_id, region_x, region_y, epoch, chunk_size, region_size_chunks,
     status, plan_hash, materialized_frame_sequence, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, 'generated', $7, 0, 1, '{}'::jsonb)`,
		worldID, regionX, regionY, cityRealtimeSpatialEpoch, cityspatial.DefaultChunkSize,
		cityOpenWorldRegionSizeChunks, plan.BaselineHash,
	); err != nil {
		return fmt.Errorf("insert realtime static region %d,%d: %w", regionX, regionY, err)
	}
	return persistCityRealtimeStaticSector(ctx, tx, worldID, sectorX, sectorY, surface)
}

func persistCityRealtimeStaticSector(
	ctx context.Context,
	tx *sql.Tx,
	worldID, sectorX, sectorY int64,
	surface *cityspatial.GeneratedOpenWorldSurfaceSector,
) error {
	if surface == nil || !cityRealtimeSHA256Hex(surface.PlanHash) || !cityRealtimeSHA256Hex(surface.ContentHash) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_surface"})
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_spatial_sectors
    (world_id, sector_x, sector_y, epoch, chunk_size, sector_size_chunks, status,
     plan_hash, content_hash, materialized_frame_sequence, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, 'generated', $7, $8, 0, 1, '{}'::jsonb)`,
		worldID, sectorX, sectorY, cityRealtimeSpatialEpoch, cityspatial.DefaultChunkSize,
		cityOpenWorldSectorSizeChunks, surface.PlanHash, surface.ContentHash,
	); err != nil {
		return fmt.Errorf("insert realtime static sector %d,%d: %w", sectorX, sectorY, err)
	}
	chunkStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_realtime_spatial_chunks
    (world_id, sector_x, sector_y, epoch, chunk_x, chunk_y, z, payload, payload_hash, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, 1, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare realtime static chunk insert: %w", err)
	}
	defer func() { _ = chunkStatement.Close() }()
	for _, chunk := range surface.Chunks {
		if _, err = chunkStatement.ExecContext(ctx, worldID, sectorX, sectorY, cityRealtimeSpatialEpoch,
			chunk.Coordinate.X, chunk.Coordinate.Y, chunk.Coordinate.Z, chunk.CanonicalPayload, chunk.PayloadHash); err != nil {
			return fmt.Errorf("insert realtime static chunk %d,%d: %w", chunk.Coordinate.X, chunk.Coordinate.Y, err)
		}
	}
	buildingStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_realtime_spatial_buildings
    (world_id, code, sector_x, sector_y, epoch, city_code, lot_code, primary_use,
     archetype_code, layout_style, floor_count, entrance_x, entrance_y, entrance_z,
     footprint, footprint_hash, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, $16, 1, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare realtime static building insert: %w", err)
	}
	defer func() { _ = buildingStatement.Close() }()
	ownedBuildings := make(map[string]struct{}, len(surface.Buildings))
	for _, building := range surface.Buildings {
		ownerX, ownerY := cityOpenWorldSectorForWorldPoint(building.Entrance.X, building.Entrance.Y)
		if ownerX != sectorX || ownerY != sectorY {
			continue
		}
		footprint, marshalErr := json.Marshal(building.Footprint)
		if marshalErr != nil {
			return fmt.Errorf("marshal realtime static building %s footprint: %w", building.Code, marshalErr)
		}
		footprintHash := cityOpenWorldPayloadHash(footprint)
		if !cityRealtimeSHA256Hex(footprintHash) {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_building_hash"})
		}
		if _, err = buildingStatement.ExecContext(ctx, worldID, building.Code, sectorX, sectorY, cityRealtimeSpatialEpoch,
			building.CityCode, building.LotCode, building.PrimaryUse, building.ArchetypeCode, building.LayoutStyle,
			building.FloorCount, building.Entrance.X, building.Entrance.Y, building.Entrance.Z,
			footprint, footprintHash); err != nil {
			return fmt.Errorf("insert realtime static building %s: %w", building.Code, err)
		}
		ownedBuildings[building.Code] = struct{}{}
	}
	interiorStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_realtime_spatial_building_interiors
    (world_id, building_code, floor_index, z, layout_version, layout_style,
     cells, content_hash, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, 1, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare realtime static interior insert: %w", err)
	}
	defer func() { _ = interiorStatement.Close() }()
	for _, interior := range surface.Interiors {
		if _, owned := ownedBuildings[interior.BuildingCode]; !owned {
			continue
		}
		cells, marshalErr := json.Marshal(interior.Cells)
		if marshalErr != nil {
			return fmt.Errorf("marshal realtime static interior %s floor %d: %w", interior.BuildingCode, interior.FloorIndex, marshalErr)
		}
		if _, err = interiorStatement.ExecContext(ctx, worldID, interior.BuildingCode, interior.FloorIndex,
			interior.Z, interior.LayoutVersion, interior.LayoutStyle, cells, interior.ContentHash); err != nil {
			return fmt.Errorf("insert realtime static interior %s floor %d: %w", interior.BuildingCode, interior.FloorIndex, err)
		}
	}
	portalStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_realtime_spatial_portals
    (world_id, code, building_code, portal_type, from_floor_index, to_floor_index,
     from_x, from_y, from_z, to_x, to_y, to_z, bidirectional, topology_hash,
     revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 1, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare realtime static portal insert: %w", err)
	}
	defer func() { _ = portalStatement.Close() }()
	for _, portal := range surface.Portals {
		if _, owned := ownedBuildings[portal.BuildingCode]; !owned {
			continue
		}
		topologyHash, hashErr := cityspatial.ComputeOpenWorldPortalHash(portal)
		if hashErr != nil {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_portal"}).WithCause(hashErr)
		}
		if _, err = portalStatement.ExecContext(ctx, worldID, portal.Code, portal.BuildingCode, portal.PortalType,
			portal.FromFloorIndex, portal.ToFloorIndex, portal.From.X, portal.From.Y, portal.From.Z,
			portal.To.X, portal.To.Y, portal.To.Z, portal.Bidirectional, topologyHash); err != nil {
			return fmt.Errorf("insert realtime static portal %s: %w", portal.Code, err)
		}
	}
	return nil
}

func loadCityRealtimeSpatialBinding(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityRealtimeSpatialBinding, error) {
	item := &CityRealtimeSpatialBinding{}
	err := queryer.QueryRowContext(ctx, `
SELECT generator_id, generator_version, rule_set_id, rule_set_version, rule_set_hash,
       profile_id, profile_version, profile_hash, context_hash, seed,
       spawn_sector_x, spawn_sector_y, spawn_x, spawn_y, spawn_z, epoch,
       bootstrap_plan_hash, genesis_hash,
       (SELECT chunk_size FROM city_realtime_spatial_sectors
        WHERE world_id = binding.world_id
        ORDER BY epoch ASC, sector_y ASC, sector_x ASC
        LIMIT 1),
       (SELECT sector_size_chunks FROM city_realtime_spatial_sectors
        WHERE world_id = binding.world_id
        ORDER BY epoch ASC, sector_y ASC, sector_x ASC
        LIMIT 1)
FROM city_realtime_spatial_bindings binding
WHERE binding.world_id = $1`, worldID).Scan(
		&item.GeneratorID, &item.GeneratorVersion, &item.RuleSetID, &item.RuleSetVersion, &item.RuleSetHash,
		&item.ProfileID, &item.ProfileVersion, &item.ProfileHash, &item.ContextHash, &item.Seed,
		&item.SpawnSectorX, &item.SpawnSectorY, &item.SpawnX, &item.SpawnY, &item.SpawnZ, &item.Epoch,
		&item.BootstrapPlanHash, &item.GenesisHash, &item.ChunkSize, &item.SectorSizeChunks,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_binding"})
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime static binding: %w", err)
	}
	return item, nil
}

func loadCityRealtimeSpatialHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeSpatialHashState, error) {
	binding, err := loadCityRealtimeSpatialBinding(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state := &cityRealtimeSpatialHashState{
		Binding:   *binding,
		Regions:   make([]cityRealtimeSpatialRegionHash, 0),
		Sectors:   make([]cityRealtimeSpatialSectorHash, 0),
		Chunks:    make([]cityRealtimeSpatialChunkHash, 0),
		Buildings: make([]cityRealtimeSpatialBuildingHash, 0),
		Interiors: make([]cityRealtimeSpatialInteriorHash, 0),
		Portals:   make([]cityRealtimeSpatialPortalHash, 0),
	}
	regionRows, err := queryer.QueryContext(ctx, `
SELECT region_x, region_y, epoch, plan_hash, materialized_frame_sequence, revision
FROM city_realtime_spatial_regions
WHERE world_id = $1
ORDER BY epoch ASC, region_y ASC, region_x ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime static region hashes: %w", err)
	}
	for regionRows.Next() {
		var item cityRealtimeSpatialRegionHash
		if err = regionRows.Scan(&item.RegionX, &item.RegionY, &item.Epoch, &item.PlanHash,
			&item.MaterializedFrameSequence, &item.Revision); err != nil {
			_ = regionRows.Close()
			return nil, err
		}
		state.Regions = append(state.Regions, item)
	}
	if err = regionRows.Err(); err != nil {
		_ = regionRows.Close()
		return nil, err
	}
	_ = regionRows.Close()
	sectorRows, err := queryer.QueryContext(ctx, `
SELECT sector_x, sector_y, epoch, plan_hash, content_hash, materialized_frame_sequence, revision
FROM city_realtime_spatial_sectors
WHERE world_id = $1
ORDER BY epoch ASC, sector_y ASC, sector_x ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime static sector hashes: %w", err)
	}
	for sectorRows.Next() {
		var item cityRealtimeSpatialSectorHash
		if err = sectorRows.Scan(&item.SectorX, &item.SectorY, &item.Epoch, &item.PlanHash,
			&item.ContentHash, &item.MaterializedFrameSequence, &item.Revision); err != nil {
			_ = sectorRows.Close()
			return nil, err
		}
		state.Sectors = append(state.Sectors, item)
	}
	if err = sectorRows.Err(); err != nil {
		_ = sectorRows.Close()
		return nil, err
	}
	_ = sectorRows.Close()
	chunkRows, err := queryer.QueryContext(ctx, `
SELECT chunk_x, chunk_y, z, payload_hash, revision
FROM city_realtime_spatial_chunks
WHERE world_id = $1
ORDER BY z ASC, chunk_y ASC, chunk_x ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime static chunk hashes: %w", err)
	}
	for chunkRows.Next() {
		var item cityRealtimeSpatialChunkHash
		if err = chunkRows.Scan(&item.ChunkX, &item.ChunkY, &item.Z, &item.PayloadHash, &item.Revision); err != nil {
			_ = chunkRows.Close()
			return nil, err
		}
		state.Chunks = append(state.Chunks, item)
	}
	if err = chunkRows.Err(); err != nil {
		_ = chunkRows.Close()
		return nil, err
	}
	_ = chunkRows.Close()
	buildingRows, err := queryer.QueryContext(ctx, `
SELECT code, footprint_hash, revision
FROM city_realtime_spatial_buildings
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime static building hashes: %w", err)
	}
	for buildingRows.Next() {
		var item cityRealtimeSpatialBuildingHash
		if err = buildingRows.Scan(&item.Code, &item.FootprintHash, &item.Revision); err != nil {
			_ = buildingRows.Close()
			return nil, err
		}
		state.Buildings = append(state.Buildings, item)
	}
	if err = buildingRows.Err(); err != nil {
		_ = buildingRows.Close()
		return nil, err
	}
	_ = buildingRows.Close()
	interiorRows, err := queryer.QueryContext(ctx, `
SELECT building_code, floor_index, z, layout_version, content_hash, revision
FROM city_realtime_spatial_building_interiors
WHERE world_id = $1
ORDER BY building_code ASC, floor_index ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime static interior hashes: %w", err)
	}
	for interiorRows.Next() {
		var item cityRealtimeSpatialInteriorHash
		if err = interiorRows.Scan(&item.BuildingCode, &item.FloorIndex, &item.Z, &item.LayoutVersion,
			&item.ContentHash, &item.Revision); err != nil {
			_ = interiorRows.Close()
			return nil, err
		}
		state.Interiors = append(state.Interiors, item)
	}
	if err = interiorRows.Err(); err != nil {
		_ = interiorRows.Close()
		return nil, err
	}
	_ = interiorRows.Close()
	portalRows, err := queryer.QueryContext(ctx, `
SELECT code, topology_hash, revision
FROM city_realtime_spatial_portals
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime static portal hashes: %w", err)
	}
	defer func() { _ = portalRows.Close() }()
	for portalRows.Next() {
		var item cityRealtimeSpatialPortalHash
		if err = portalRows.Scan(&item.Code, &item.TopologyHash, &item.Revision); err != nil {
			return nil, err
		}
		state.Portals = append(state.Portals, item)
	}
	if err = portalRows.Err(); err != nil {
		return nil, err
	}
	if err = validateCityRealtimeSpatialHashState(state); err != nil {
		return nil, err
	}
	return state, nil
}

func validateCityRealtimeSpatialHashState(state *cityRealtimeSpatialHashState) error {
	if state == nil || state.Binding.GeneratorID == "" || state.Binding.GeneratorVersion == "" ||
		state.Binding.RuleSetID == "" || state.Binding.RuleSetVersion == "" || state.Binding.ProfileID == "" ||
		state.Binding.ProfileVersion == "" || state.Binding.Seed <= 0 || state.Binding.Epoch != cityRealtimeSpatialEpoch ||
		state.Binding.ChunkSize != cityspatial.DefaultChunkSize || state.Binding.SectorSizeChunks != cityOpenWorldSectorSizeChunks ||
		!cityRealtimeSHA256Hex(state.Binding.RuleSetHash) || !cityRealtimeSHA256Hex(state.Binding.ProfileHash) ||
		!cityRealtimeSHA256Hex(state.Binding.ContextHash) || !cityRealtimeSHA256Hex(state.Binding.BootstrapPlanHash) ||
		!cityRealtimeSHA256Hex(state.Binding.GenesisHash) || len(state.Regions) == 0 || len(state.Sectors) == 0 ||
		len(state.Chunks) == 0 {
		return fmt.Errorf("invalid realtime static worldgen state")
	}
	for _, item := range state.Regions {
		if item.Epoch != cityRealtimeSpatialEpoch || item.MaterializedFrameSequence != 0 || item.Revision != 1 ||
			!cityRealtimeSHA256Hex(item.PlanHash) {
			return fmt.Errorf("invalid realtime static region hash")
		}
	}
	for _, item := range state.Sectors {
		if item.Epoch != cityRealtimeSpatialEpoch || item.MaterializedFrameSequence != 0 || item.Revision != 1 ||
			!cityRealtimeSHA256Hex(item.PlanHash) || !cityRealtimeSHA256Hex(item.ContentHash) {
			return fmt.Errorf("invalid realtime static sector hash")
		}
	}
	for _, item := range state.Chunks {
		if item.Z != cityspatial.SurfaceZ || item.Revision != 1 || !cityRealtimeSHA256Hex(item.PayloadHash) {
			return fmt.Errorf("invalid realtime static chunk hash")
		}
	}
	for _, item := range state.Buildings {
		if item.Code == "" || item.Revision != 1 || !cityRealtimeSHA256Hex(item.FootprintHash) {
			return fmt.Errorf("invalid realtime static building hash")
		}
	}
	for _, item := range state.Interiors {
		if item.BuildingCode == "" || item.FloorIndex < 0 || item.Z < cityspatial.SurfaceZ ||
			item.LayoutVersion == "" || item.Revision != 1 || !cityRealtimeSHA256Hex(item.ContentHash) {
			return fmt.Errorf("invalid realtime static interior hash")
		}
	}
	for _, item := range state.Portals {
		if item.Code == "" || item.Revision != 1 || !cityRealtimeSHA256Hex(item.TopologyHash) {
			return fmt.Errorf("invalid realtime static portal hash")
		}
	}
	return nil
}
