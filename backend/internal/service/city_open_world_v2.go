package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

const CityCommandTypeOpenWorldSectorMaterialize = "open_world.sector.materialize"

type cityOpenWorldSectorMaterializePayload struct {
	SectorX int64 `json:"sector_x"`
	SectorY int64 `json:"sector_y"`
}

func isCityOpenWorldCommand(commandType string) bool {
	return commandType == CityCommandTypeOpenWorldSectorMaterialize ||
		isCityOpenWorldRuntimeCommand(commandType)
}

func normalizeCityOpenWorldCommand(
	commandType string,
	rawPayload json.RawMessage,
) (any, bool, error) {
	if payload, handled, err := normalizeCityOpenWorldRuntimeCommand(commandType, rawPayload); handled || err != nil {
		return payload, handled, err
	}
	if commandType != CityCommandTypeOpenWorldSectorMaterialize {
		return nil, false, nil
	}
	var payload cityOpenWorldSectorMaterializePayload
	if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
		return nil, true, ErrCityInvalidInput.WithCause(err)
	}
	if payload.SectorX < -cityOpenWorldMaximumSectorAbs || payload.SectorX > cityOpenWorldMaximumSectorAbs ||
		payload.SectorY < -cityOpenWorldMaximumSectorAbs || payload.SectorY > cityOpenWorldMaximumSectorAbs {
		return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "open_world_sector"})
	}
	return payload, true, nil
}

func initializeCityOpenWorldV2Foundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, seed int64,
	simulationVersion, profileID, spawnPolicy string,
) error {
	return initializeCityOpenWorldRegionFoundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
}

func initializeCityOpenWorldV3Foundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, seed int64,
	simulationVersion, profileID, spawnPolicy string,
) error {
	return initializeCityOpenWorldRegionFoundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy)
}

func initializeCityOpenWorldV4Foundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, seed int64,
	simulationVersion, profileID, spawnPolicy string,
) error {
	if err := initializeCityOpenWorldRegionFoundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy); err != nil {
		return err
	}
	return initializeCityOpenWorldRuntimeFoundation(ctx, tx, worldID)
}

func initializeCityOpenWorldV5Foundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, seed int64,
	simulationVersion, profileID, spawnPolicy string,
) error {
	if err := initializeCityOpenWorldRegionFoundation(ctx, tx, worldID, seed, simulationVersion, profileID, spawnPolicy); err != nil {
		return err
	}
	if err := initializeCityOpenWorldRuntimeFoundation(ctx, tx, worldID); err != nil {
		return err
	}
	return initializeCityOpenWorldV5SocialFoundation(ctx, tx, worldID)
}

func initializeCityOpenWorldRegionFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, seed int64,
	simulationVersion, profileID, spawnPolicy string,
) error {
	if !cityEngineSupportsOpenWorldMaterialization(simulationVersion) || spawnPolicy != cityOpenWorldSpawnPolicy {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	profile, err := cityspatial.WorldgenProfileByID(profileID)
	if err != nil {
		return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "style_profile_id"})
	}
	binding, ruleSet, err := cityOpenWorldRegionBinding(simulationVersion, seed, profile)
	if err != nil {
		return err
	}
	regionBounds := cityOpenWorldRegionBounds(0, 0)
	plan, err := cityspatial.GenerateWorldgenPlan(binding, profile, regionBounds)
	if err != nil || len(plan.Cities) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_region_plan"}).WithCause(err)
	}
	spawn := plan.Cities[0].Center
	spawnAddress, err := cityspatial.SplitWorldCoordinate(
		cityspatial.WorldCoordinate{X: spawn.X, Y: spawn.Y, Z: spawn.Z},
		cityspatial.DefaultChunkSize,
	)
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_spawn"}).WithCause(err)
	}
	sectorX, sectorY := cityOpenWorldSectorForChunk(spawnAddress.Chunk.X, spawnAddress.Chunk.Y)
	regionX, regionY := cityOpenWorldRegionForSector(sectorX, sectorY)
	if regionX != 0 || regionY != 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_spawn_region"})
	}
	surface, err := cityOpenWorldSurfaceForVersion(simulationVersion, plan, cityOpenWorldSectorBounds(sectorX, sectorY))
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_surface"}).WithCause(err)
	}
	if err = activateCityOpenWorldInitializationWrite(ctx, tx, worldID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_bindings
    (world_id, generator_id, generator_version, rule_set_id, rule_set_version, rule_set_hash,
     profile_id, profile_version, profile_hash, context_hash, seed, spawn_sector_x, spawn_sector_y,
     spawn_x, spawn_y, spawn_z, epoch, bootstrap_plan_hash, genesis_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, 1, $17, $18, '{}'::jsonb)`,
		worldID, binding.GeneratorID, binding.GeneratorVersion, ruleSet.ID, ruleSet.Version, ruleSet.ContentHash,
		binding.ProfileID, binding.ProfileVersion, binding.ProfileHash, binding.SpatialRootHash, seed,
		sectorX, sectorY, spawn.X, spawn.Y, spawn.Z, plan.BaselineHash, surface.ContentHash,
	); err != nil {
		return fmt.Errorf("insert open-world V2 binding: %w", err)
	}
	if err = insertCityOpenWorldRegion(ctx, tx, worldID, regionX, regionY, 0, plan.BaselineHash); err != nil {
		return err
	}
	return persistCityOpenWorldV2Sector(ctx, tx, worldID, sectorX, sectorY, 0, surface)
}

func (s *CityEconomyService) applyCityOpenWorldCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	command *CityCommand,
) (cityPendingEvent, error) {
	if command == nil {
		return cityPendingEvent{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_command"})
	}
	if command.CommandType != CityCommandTypeOpenWorldSectorMaterialize {
		return cityPendingEvent{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"command_type": command.CommandType})
	}
	payload, err := decodeStoredCityCommandPayload[cityOpenWorldSectorMaterializePayload](command)
	if err != nil {
		return cityPendingEvent{}, err
	}
	var existing bool
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM city_open_world_sectors
    WHERE world_id = $1 AND sector_x = $2 AND sector_y = $3 AND epoch = 1
)`, worldID, payload.SectorX, payload.SectorY).Scan(&existing); err != nil {
		return cityPendingEvent{}, fmt.Errorf("check open-world materialization: %w", err)
	}
	regionX, regionY := cityOpenWorldRegionForSector(payload.SectorX, payload.SectorY)
	if existing {
		return cityOpenWorldSectorPendingEvent(command, payload, regionX, regionY, "", true), nil
	}
	simulationVersion, binding, profile, ruleSet, err := loadCityOpenWorldRegionGenerator(ctx, tx, worldID)
	if err != nil {
		return cityPendingEvent{}, err
	}
	regionBounds := cityOpenWorldRegionBounds(regionX, regionY)
	plan, err := cityspatial.GenerateWorldgenPlan(binding, profile, regionBounds)
	if err != nil {
		return cityPendingEvent{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_region_plan"}).WithCause(err)
	}
	var storedPlanHash string
	err = tx.QueryRowContext(ctx, `
SELECT plan_hash FROM city_open_world_regions
WHERE world_id = $1 AND region_x = $2 AND region_y = $3 AND epoch = 1`,
		worldID, regionX, regionY,
	).Scan(&storedPlanHash)
	if err != nil && err != sql.ErrNoRows {
		return cityPendingEvent{}, fmt.Errorf("load open-world region: %w", err)
	}
	if err == nil && storedPlanHash != plan.BaselineHash {
		return cityPendingEvent{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_region_hash"})
	}
	surface, err := cityOpenWorldSurfaceForVersion(simulationVersion, plan, cityOpenWorldSectorBounds(payload.SectorX, payload.SectorY))
	if err != nil {
		return cityPendingEvent{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_surface"}).WithCause(err)
	}
	if err = activateCityOpenWorldMaterializationWrite(ctx, tx, worldID); err != nil {
		return cityPendingEvent{}, err
	}
	if storedPlanHash == "" {
		if err = insertCityOpenWorldRegion(ctx, tx, worldID, regionX, regionY, targetTick, plan.BaselineHash); err != nil {
			return cityPendingEvent{}, err
		}
	}
	if err = assertCityOpenWorldV2RuleSet(ruleSet); err != nil {
		return cityPendingEvent{}, err
	}
	if err = persistCityOpenWorldV2Sector(ctx, tx, worldID, payload.SectorX, payload.SectorY, targetTick, surface); err != nil {
		return cityPendingEvent{}, err
	}
	return cityOpenWorldSectorPendingEvent(command, payload, regionX, regionY, plan.BaselineHash, false), nil
}

func cityOpenWorldSectorPendingEvent(
	command *CityCommand,
	payload cityOpenWorldSectorMaterializePayload,
	regionX, regionY int64,
	planHash string,
	alreadyMaterialized bool,
) cityPendingEvent {
	payloadValue := map[string]any{
		"sector_x": payload.SectorX, "sector_y": payload.SectorY,
		"region_x": regionX, "region_y": regionY,
		"already_materialized": alreadyMaterialized,
	}
	if planHash != "" {
		payloadValue["plan_hash"] = planHash
	}
	return cityPendingEvent{
		command: command, status: CityCommandStatusApplied,
		eventType: "city.open_world.sector_materialized", payload: payloadValue,
		result: map[string]any{"applied": true, "already_materialized": alreadyMaterialized,
			"sector_x": payload.SectorX, "sector_y": payload.SectorY,
			"region_x": regionX, "region_y": regionY, "plan_hash": planHash},
	}
}

func cityOpenWorldRegionBinding(
	simulationVersion string,
	seed int64,
	profile *cityspatial.WorldgenProfile,
) (cityspatial.WorldgenBinding, cityspatial.RuleSet, error) {
	if !cityEngineSupportsOpenWorldMaterialization(simulationVersion) {
		return cityspatial.WorldgenBinding{}, cityspatial.RuleSet{}, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	registry, err := cityspatial.DefaultRegistry()
	if err != nil {
		return cityspatial.WorldgenBinding{}, cityspatial.RuleSet{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_rule_registry"}).WithCause(err)
	}
	ruleSet, err := registry.Get(cityspatial.DefaultRuleSetID)
	if err != nil {
		return cityspatial.WorldgenBinding{}, cityspatial.RuleSet{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_rule_set"}).WithCause(err)
	}
	if err = assertCityOpenWorldV2RuleSet(*ruleSet); err != nil {
		return cityspatial.WorldgenBinding{}, cityspatial.RuleSet{}, err
	}
	var binding cityspatial.WorldgenBinding
	switch simulationVersion {
	case CitySimulationVersionOpenWorldV2:
		binding, err = cityspatial.DefaultOpenWorldgenBindingV2(simulationVersion, seed, profile)
	case CitySimulationVersionOpenWorldV3, CitySimulationVersionOpenWorldV4, CitySimulationVersionOpenWorldV5:
		binding, err = cityspatial.DefaultOpenWorldgenBindingV3(simulationVersion, seed, profile)
	default:
		return cityspatial.WorldgenBinding{}, cityspatial.RuleSet{}, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	if err != nil {
		return cityspatial.WorldgenBinding{}, cityspatial.RuleSet{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_binding"}).WithCause(err)
	}
	return binding, *ruleSet, nil
}

func assertCityOpenWorldV2RuleSet(ruleSet cityspatial.RuleSet) error {
	if ruleSet.ChunkSize != cityspatial.DefaultChunkSize {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_rule_set_chunk_size"})
	}
	return nil
}

func loadCityOpenWorldRegionGenerator(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (string, cityspatial.WorldgenBinding, *cityspatial.WorldgenProfile, cityspatial.RuleSet, error) {
	var simulationVersion string
	if err := queryer.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&simulationVersion); err != nil {
		return "", cityspatial.WorldgenBinding{}, nil, cityspatial.RuleSet{}, fmt.Errorf("load open-world simulation version: %w", err)
	}
	if !cityEngineSupportsOpenWorldMaterialization(simulationVersion) {
		return "", cityspatial.WorldgenBinding{}, nil, cityspatial.RuleSet{}, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	persisted, err := loadCityOpenWorldBinding(ctx, queryer, worldID)
	if err != nil {
		return "", cityspatial.WorldgenBinding{}, nil, cityspatial.RuleSet{}, err
	}
	profile, err := cityspatial.WorldgenProfileByID(persisted.ProfileID)
	if err != nil || profile.Version != persisted.ProfileVersion || profile.ContentHash != persisted.ProfileHash {
		return "", cityspatial.WorldgenBinding{}, nil, cityspatial.RuleSet{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_profile_binding"}).WithCause(err)
	}
	binding, ruleSet, err := cityOpenWorldRegionBinding(simulationVersion, persisted.Seed, profile)
	if err != nil {
		return "", cityspatial.WorldgenBinding{}, nil, cityspatial.RuleSet{}, err
	}
	if persisted.GeneratorID != binding.GeneratorID || persisted.GeneratorVersion != binding.GeneratorVersion ||
		persisted.ContextHash != binding.SpatialRootHash || persisted.ProfileID != binding.ProfileID ||
		persisted.ProfileVersion != binding.ProfileVersion || persisted.ProfileHash != binding.ProfileHash ||
		persisted.RuleSetID != ruleSet.ID || persisted.RuleSetVersion != ruleSet.Version || persisted.RuleSetHash != ruleSet.ContentHash {
		return "", cityspatial.WorldgenBinding{}, nil, cityspatial.RuleSet{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_binding"})
	}
	return simulationVersion, binding, profile, ruleSet, nil
}

func cityOpenWorldSurfaceForVersion(
	simulationVersion string,
	plan *cityspatial.GeneratedWorldgenPlan,
	bounds cityspatial.WorldgenBounds,
) (*cityspatial.GeneratedOpenWorldSurfaceSector, error) {
	switch simulationVersion {
	case CitySimulationVersionOpenWorldV2:
		return cityspatial.GenerateOpenWorldSurfaceSector(plan, bounds)
	case CitySimulationVersionOpenWorldV3, CitySimulationVersionOpenWorldV4, CitySimulationVersionOpenWorldV5:
		return cityspatial.GenerateOpenWorldSurfaceSectorV2(plan, bounds)
	default:
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
}

func activateCityOpenWorldInitializationWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_open_world_initialize_world_id', $1, TRUE)`, strconv.FormatInt(worldID, 10)); err != nil {
		return fmt.Errorf("activate open-world initialization write gate: %w", err)
	}
	return nil
}

func activateCityOpenWorldMaterializationWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_open_world_materialization_world_id', $1, TRUE)`, strconv.FormatInt(worldID, 10)); err != nil {
		return fmt.Errorf("activate open-world materialization write gate: %w", err)
	}
	return nil
}

func insertCityOpenWorldRegion(
	ctx context.Context,
	tx *sql.Tx,
	worldID, regionX, regionY, generatedTick int64,
	planHash string,
) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_regions
    (world_id, region_x, region_y, epoch, chunk_size, region_size_chunks,
     status, plan_hash, generated_tick, revision, metadata)
VALUES ($1, $2, $3, 1, $4, $5, 'generated', $6, $7, 1, '{}'::jsonb)`,
		worldID, regionX, regionY, cityspatial.DefaultChunkSize, cityOpenWorldRegionSizeChunks,
		planHash, generatedTick,
	); err != nil {
		return fmt.Errorf("insert open-world region %d,%d: %w", regionX, regionY, err)
	}
	return nil
}

func persistCityOpenWorldV2Sector(
	ctx context.Context,
	tx *sql.Tx,
	worldID, sectorX, sectorY, generatedTick int64,
	surface *cityspatial.GeneratedOpenWorldSurfaceSector,
) error {
	if surface == nil || surface.PlanHash == "" || surface.ContentHash == "" {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_surface"})
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_sectors
    (world_id, sector_x, sector_y, epoch, chunk_size, sector_size_chunks, status,
     plan_hash, content_hash, generated_tick, revision, metadata)
VALUES ($1, $2, $3, 1, $4, $5, 'generated', $6, $7, $8, 1, '{}'::jsonb)`,
		worldID, sectorX, sectorY, cityspatial.DefaultChunkSize, cityOpenWorldSectorSizeChunks,
		surface.PlanHash, surface.ContentHash, generatedTick,
	); err != nil {
		return fmt.Errorf("insert open-world sector %d,%d: %w", sectorX, sectorY, err)
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
	ownedBuildings := make(map[string]struct{}, len(surface.Buildings))
	for _, building := range surface.Buildings {
		ownerX, ownerY := cityOpenWorldSectorForWorldPoint(building.Entrance.X, building.Entrance.Y)
		if ownerX != sectorX || ownerY != sectorY {
			continue
		}
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
		ownedBuildings[building.Code] = struct{}{}
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
		if _, owned := ownedBuildings[interior.BuildingCode]; !owned {
			continue
		}
		cells, marshalErr := json.Marshal(interior.Cells)
		if marshalErr != nil {
			return fmt.Errorf("marshal open-world interior %s floor %d: %w", interior.BuildingCode, interior.FloorIndex, marshalErr)
		}
		if _, err = interiorStatement.ExecContext(ctx, worldID, interior.BuildingCode, interior.FloorIndex,
			interior.Z, interior.LayoutVersion, interior.LayoutStyle, cells, interior.ContentHash); err != nil {
			return fmt.Errorf("insert open-world interior %s floor %d: %w", interior.BuildingCode, interior.FloorIndex, err)
		}
	}
	portalStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_open_world_portals
    (world_id, code, building_code, portal_type, from_floor_index, to_floor_index,
     from_x, from_y, from_z, to_x, to_y, to_z, bidirectional, topology_hash,
     revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 1, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare open-world portal insert: %w", err)
	}
	defer func() { _ = portalStatement.Close() }()
	for _, portal := range surface.Portals {
		if _, owned := ownedBuildings[portal.BuildingCode]; !owned {
			continue
		}
		topologyHash, hashErr := cityspatial.ComputeOpenWorldPortalHash(portal)
		if hashErr != nil {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_portal"}).WithCause(hashErr)
		}
		if _, err = portalStatement.ExecContext(ctx,
			worldID, portal.Code, portal.BuildingCode, portal.PortalType,
			portal.FromFloorIndex, portal.ToFloorIndex,
			portal.From.X, portal.From.Y, portal.From.Z,
			portal.To.X, portal.To.Y, portal.To.Z,
			portal.Bidirectional, topologyHash,
		); err != nil {
			return fmt.Errorf("insert open-world portal %s: %w", portal.Code, err)
		}
	}
	return nil
}

func cityOpenWorldRegionForSector(sectorX, sectorY int64) (int64, int64) {
	return cityOpenWorldFloorDiv(sectorX, cityOpenWorldRegionsPerAxis),
		cityOpenWorldFloorDiv(sectorY, cityOpenWorldRegionsPerAxis)
}

func cityOpenWorldRegionBounds(regionX, regionY int64) cityspatial.WorldgenBounds {
	return cityspatial.WorldgenBounds{
		MinimumChunkX: regionX * cityOpenWorldRegionSizeChunks,
		MaximumChunkX: regionX*cityOpenWorldRegionSizeChunks + cityOpenWorldRegionSizeChunks - 1,
		MinimumChunkY: regionY * cityOpenWorldRegionSizeChunks,
		MaximumChunkY: regionY*cityOpenWorldRegionSizeChunks + cityOpenWorldRegionSizeChunks - 1,
		Z:             cityspatial.SurfaceZ,
	}
}

func cityOpenWorldSectorForChunk(chunkX, chunkY int64) (int64, int64) {
	return cityOpenWorldFloorDiv(chunkX, cityOpenWorldSectorSizeChunks),
		cityOpenWorldFloorDiv(chunkY, cityOpenWorldSectorSizeChunks)
}

func cityOpenWorldSectorForWorldPoint(worldX, worldY int64) (int64, int64) {
	chunkX := cityOpenWorldFloorDiv(worldX, cityspatial.DefaultChunkSize)
	chunkY := cityOpenWorldFloorDiv(worldY, cityspatial.DefaultChunkSize)
	return cityOpenWorldSectorForChunk(chunkX, chunkY)
}
