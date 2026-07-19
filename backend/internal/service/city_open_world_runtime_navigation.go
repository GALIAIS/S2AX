package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

type cityOpenWorldStaticPortal struct {
	Code           string
	BuildingCode   string
	PortalType     string
	FromFloorIndex int32
	ToFloorIndex   int32
	From           cityspatial.WorldgenPoint
	To             cityspatial.WorldgenPoint
	Bidirectional  bool
}

func cityOpenWorldRuntimeLocationFromPayload(payload cityOpenWorldActorMovePayload) (CityOpenWorldActorLocation, error) {
	address, err := cityspatial.SplitWorldCoordinate(cityspatial.WorldCoordinate{X: payload.X, Y: payload.Y, Z: payload.Z}, cityspatial.DefaultChunkSize)
	if err != nil {
		return CityOpenWorldActorLocation{}, ErrCityInvalidInput.WithCause(err)
	}
	sectorX, sectorY := cityOpenWorldSectorForChunk(address.Chunk.X, address.Chunk.Y)
	location := CityOpenWorldActorLocation{
		ActorCode: payload.ActorCode, SpaceKind: payload.SpaceKind, FloorIndex: payload.FloorIndex,
		X: payload.X, Y: payload.Y, Z: payload.Z, SectorX: sectorX, SectorY: sectorY,
		ChunkX: address.Chunk.X, ChunkY: address.Chunk.Y, LocalX: address.Local.X, LocalY: address.Local.Y,
	}
	if payload.SpaceKind == "interior" {
		location.LocationScope = payload.BuildingCode
		location.BuildingCode = cityOpenWorldStringPointer(payload.BuildingCode)
	} else {
		location.LocationScope = "surface"
	}
	return location, nil
}

func cityOpenWorldRuntimeLocationFromPortalPoint(
	actorCode, spaceKind, buildingCode string,
	floorIndex int32,
	point cityspatial.WorldgenPoint,
) (CityOpenWorldActorLocation, error) {
	return cityOpenWorldRuntimeLocationFromPayload(cityOpenWorldActorMovePayload{
		ActorCode: actorCode, SpaceKind: spaceKind, BuildingCode: buildingCode,
		FloorIndex: floorIndex, X: point.X, Y: point.Y, Z: point.Z,
	})
}

func cityOpenWorldRuntimeLocationEqual(left, right CityOpenWorldActorLocation) bool {
	leftBuilding, rightBuilding := "", ""
	if left.BuildingCode != nil {
		leftBuilding = *left.BuildingCode
	}
	if right.BuildingCode != nil {
		rightBuilding = *right.BuildingCode
	}
	return left.SpaceKind == right.SpaceKind && left.LocationScope == right.LocationScope &&
		leftBuilding == rightBuilding && left.FloorIndex == right.FloorIndex &&
		left.X == right.X && left.Y == right.Y && left.Z == right.Z
}

func cityOpenWorldRuntimeIsAdjacentMove(current, target CityOpenWorldActorLocation) bool {
	if current.SpaceKind != target.SpaceKind || current.LocationScope != target.LocationScope ||
		current.FloorIndex != target.FloorIndex || current.Z != target.Z {
		return false
	}
	currentBuilding, targetBuilding := "", ""
	if current.BuildingCode != nil {
		currentBuilding = *current.BuildingCode
	}
	if target.BuildingCode != nil {
		targetBuilding = *target.BuildingCode
	}
	if currentBuilding != targetBuilding {
		return false
	}
	dx, dy := current.X-target.X, current.Y-target.Y
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	return dx <= 1 && dy <= 1 && dx+dy > 0
}

func loadCityOpenWorldStaticPortal(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	portalCode string,
) (*cityOpenWorldStaticPortal, error) {
	portal := &cityOpenWorldStaticPortal{}
	err := queryer.QueryRowContext(ctx, `
SELECT code, building_code, portal_type, from_floor_index, to_floor_index,
       from_x, from_y, from_z, to_x, to_y, to_z, bidirectional
FROM city_open_world_portals
WHERE world_id = $1 AND code = $2`, worldID, portalCode).Scan(
		&portal.Code, &portal.BuildingCode, &portal.PortalType, &portal.FromFloorIndex, &portal.ToFloorIndex,
		&portal.From.X, &portal.From.Y, &portal.From.Z, &portal.To.X, &portal.To.Y, &portal.To.Z,
		&portal.Bidirectional,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionPortalNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load open-world static portal: %w", err)
	}
	return portal, nil
}

func cityOpenWorldRuntimePortalEndpoints(
	actorCode string,
	portal *cityOpenWorldStaticPortal,
) (CityOpenWorldActorLocation, CityOpenWorldActorLocation, error) {
	if portal == nil {
		return CityOpenWorldActorLocation{}, CityOpenWorldActorLocation{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_portal"})
	}
	fromSpace, toSpace := "interior", "interior"
	fromBuilding, toBuilding := portal.BuildingCode, portal.BuildingCode
	if portal.PortalType == "entrance" {
		fromSpace, fromBuilding = "surface", ""
	}
	from, err := cityOpenWorldRuntimeLocationFromPortalPoint(
		actorCode, fromSpace, fromBuilding, portal.FromFloorIndex, portal.From,
	)
	if err != nil {
		return CityOpenWorldActorLocation{}, CityOpenWorldActorLocation{}, err
	}
	to, err := cityOpenWorldRuntimeLocationFromPortalPoint(
		actorCode, toSpace, toBuilding, portal.ToFloorIndex, portal.To,
	)
	if err != nil {
		return CityOpenWorldActorLocation{}, CityOpenWorldActorLocation{}, err
	}
	return from, to, nil
}

func cityOpenWorldRuntimeValidatePassableLocation(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	location CityOpenWorldActorLocation,
) error {
	switch location.SpaceKind {
	case "surface":
		return cityOpenWorldRuntimeValidateSurfaceCell(ctx, queryer, worldID, location)
	case "interior":
		return cityOpenWorldRuntimeValidateInteriorCell(ctx, queryer, worldID, location)
	default:
		return cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionLocationInvalid)
	}
}

func cityOpenWorldRuntimeValidateSurfaceCell(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	location CityOpenWorldActorLocation,
) error {
	if location.Z != 0 || location.LocationScope != "surface" || location.BuildingCode != nil {
		return cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionLocationInvalid)
	}
	var rawPayload []byte
	err := queryer.QueryRowContext(ctx, `
SELECT payload FROM city_open_world_chunks
WHERE world_id = $1 AND chunk_x = $2 AND chunk_y = $3 AND z = 0`,
		worldID, location.ChunkX, location.ChunkY).Scan(&rawPayload)
	if errors.Is(err, sql.ErrNoRows) {
		return cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionSectorUnavailable)
	}
	if err != nil {
		return fmt.Errorf("load open-world surface chunk: %w", err)
	}
	var payload cityspatial.OpenWorldChunkPayload
	if err = json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("decode open-world surface chunk: %w", err)
	}
	if payload.Format != cityspatial.OpenWorldChunkPayloadFormat || payload.Width != int(cityspatial.DefaultChunkSize) ||
		payload.Height != int(cityspatial.DefaultChunkSize) || location.LocalX < 0 || location.LocalY < 0 ||
		location.LocalX >= int32(payload.Width) || location.LocalY >= int32(payload.Height) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_surface_payload"})
	}
	binding, err := loadCityOpenWorldBinding(ctx, queryer, worldID)
	if err != nil {
		return err
	}
	registry, err := cityspatial.DefaultRegistry()
	if err != nil {
		return fmt.Errorf("load open-world rule registry: %w", err)
	}
	ruleSet, err := registry.Get(binding.RuleSetID)
	if err != nil || ruleSet.Version != binding.RuleSetVersion || ruleSet.ContentHash != binding.RuleSetHash {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_rule_set"}).WithCause(err)
	}
	definitions := make(map[string]cityspatial.Definition, len(ruleSet.Definitions))
	for _, definition := range ruleSet.Definitions {
		definitions[definition.ID] = definition
	}
	index := int(location.LocalY)*payload.Width + int(location.LocalX)
	terrainID, found := cityOpenWorldRuntimeTerrainAt(payload.TerrainRuns, index)
	if !found || !cityOpenWorldRuntimeDefinitionPassable(definitions[terrainID]) {
		return cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCellBlocked)
	}
	for _, layer := range payload.Layers {
		if layer.X != location.LocalX || layer.Y != location.LocalY {
			continue
		}
		if layer.Kind != cityspatial.RuleKindStructure && layer.Kind != cityspatial.RuleKindFurniture && layer.Kind != cityspatial.RuleKindTerrain {
			continue
		}
		if !cityOpenWorldRuntimeDefinitionPassable(definitions[layer.DefinitionID]) {
			return cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCellBlocked)
		}
	}
	return nil
}

func cityOpenWorldRuntimeTerrainAt(runs []cityspatial.TerrainRun, index int) (string, bool) {
	if index < 0 {
		return "", false
	}
	position := 0
	for _, run := range runs {
		if run.Length <= 0 {
			return "", false
		}
		if index < position+run.Length {
			return run.DefinitionID, true
		}
		position += run.Length
	}
	return "", false
}

func cityOpenWorldRuntimeDefinitionPassable(definition cityspatial.Definition) bool {
	for _, flag := range definition.Flags {
		if flag == "passable" {
			return true
		}
	}
	return false
}

func cityOpenWorldRuntimeValidateInteriorCell(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	location CityOpenWorldActorLocation,
) error {
	if location.BuildingCode == nil || location.LocationScope != *location.BuildingCode ||
		location.FloorIndex < 0 || location.Z != location.FloorIndex {
		return cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionLocationInvalid)
	}
	var rawCells []byte
	err := queryer.QueryRowContext(ctx, `
SELECT cells FROM city_open_world_building_interiors
WHERE world_id = $1 AND building_code = $2 AND floor_index = $3`,
		worldID, *location.BuildingCode, location.FloorIndex).Scan(&rawCells)
	if errors.Is(err, sql.ErrNoRows) {
		return cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionLocationInvalid)
	}
	if err != nil {
		return fmt.Errorf("load open-world interior cells: %w", err)
	}
	var cells []cityspatial.GeneratedWorldgenInteriorCell
	if err = json.Unmarshal(rawCells, &cells); err != nil {
		return fmt.Errorf("decode open-world interior cells: %w", err)
	}
	for _, cell := range cells {
		if cell.X == location.X && cell.Y == location.Y && cell.Z == location.Z {
			if cityspatial.BuildingLayoutCellPassable(cell.Kind) {
				return nil
			}
			return cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCellBlocked)
		}
	}
	return cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCellBlocked)
}

func cityOpenWorldRuntimeLocationOccupied(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, excludingActorID int64,
	location CityOpenWorldActorLocation,
) (bool, error) {
	var occupied bool
	err := queryer.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM city_open_world_actor_locations
    WHERE world_id = $1 AND actor_id <> $2 AND space_kind = $3 AND location_scope = $4
      AND floor_index = $5 AND x = $6 AND y = $7 AND z = $8
)`, worldID, excludingActorID, location.SpaceKind, location.LocationScope,
		location.FloorIndex, location.X, location.Y, location.Z).Scan(&occupied)
	if err != nil {
		return false, fmt.Errorf("check open-world cell occupancy: %w", err)
	}
	return occupied, nil
}

func findCityOpenWorldRuntimeSpawnLocation(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, excludingActorID int64,
	actorCode string,
) (CityOpenWorldActorLocation, error) {
	binding, err := loadCityOpenWorldBinding(ctx, queryer, worldID)
	if err != nil {
		return CityOpenWorldActorLocation{}, err
	}
	return findCityOpenWorldRuntimeNearbySpawnLocation(
		ctx, queryer, worldID, excludingActorID, actorCode,
		binding.SpawnX, binding.SpawnY, 12,
	)
}

// findCityOpenWorldRuntimeNearbySpawnLocation scans a deterministic square
// ring around a static anchor. It deliberately checks the persisted V3
// surface payload for every candidate, so V5 genesis NPCs neither depend on
// the browser map nor create a second coordinate model.
func findCityOpenWorldRuntimeNearbySpawnLocation(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, excludingActorID int64,
	actorCode string,
	anchorX, anchorY, maximumRadius int64,
) (CityOpenWorldActorLocation, error) {
	if maximumRadius < 0 || maximumRadius > 256 {
		return CityOpenWorldActorLocation{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "open_world_spawn_radius"})
	}
	for radius := int64(0); radius <= maximumRadius; radius++ {
		for offsetY := -radius; offsetY <= radius; offsetY++ {
			for offsetX := -radius; offsetX <= radius; offsetX++ {
				if maxCityOpenWorldAbs(offsetX, offsetY) != radius {
					continue
				}
				location, locationErr := cityOpenWorldRuntimeLocationFromPortalPoint(
					actorCode, "surface", "", 0,
					cityspatial.WorldgenPoint{X: anchorX + offsetX, Y: anchorY + offsetY, Z: 0},
				)
				if locationErr != nil {
					continue
				}
				if validationErr := cityOpenWorldRuntimeValidatePassableLocation(ctx, queryer, worldID, location); validationErr != nil {
					continue
				}
				occupied, occupancyErr := cityOpenWorldRuntimeLocationOccupied(ctx, queryer, worldID, excludingActorID, location)
				if occupancyErr != nil {
					return CityOpenWorldActorLocation{}, occupancyErr
				}
				if !occupied {
					return location, nil
				}
			}
		}
	}
	return CityOpenWorldActorLocation{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionSpawnUnavailable)
}

func maxCityOpenWorldAbs(left, right int64) int64 {
	if left < 0 {
		left = -left
	}
	if right < 0 {
		right = -right
	}
	if left > right {
		return left
	}
	return right
}

func insertCityOpenWorldActorLocation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, actorID, targetTick, sourceFactID int64,
	location CityOpenWorldActorLocation,
) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_locations
    (world_id, actor_id, space_kind, location_scope, building_code, floor_index,
     x, y, z, sector_x, sector_y, chunk_x, chunk_y, local_x, local_y,
     moved_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, 1, '{}'::jsonb)`,
		worldID, actorID, location.SpaceKind, location.LocationScope, cityOpenWorldNullableString(location.BuildingCode),
		location.FloorIndex, location.X, location.Y, location.Z, location.SectorX, location.SectorY,
		location.ChunkX, location.ChunkY, location.LocalX, location.LocalY, targetTick, sourceFactID); err != nil {
		return fmt.Errorf("insert open-world actor location: %w", err)
	}
	return nil
}

// insertCityOpenWorldV5GenesisActorLocation is intentionally separate from
// fact-backed movement. V5 NPCs are part of the immutable genesis baseline at
// tick zero, before there is a command/fact ledger entry to reference. Later
// movement always uses insert/updateCityOpenWorldActorLocation with a posted
// fact as its source.
func insertCityOpenWorldV5GenesisActorLocation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, actorID int64,
	location CityOpenWorldActorLocation,
) error {
	if worldID <= 0 || actorID <= 0 || location.ActorCode == "" {
		return ErrCityInvalidInput
	}
	if err := cityOpenWorldRuntimeValidatePassableLocation(ctx, tx, worldID, location); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_locations
    (world_id, actor_id, space_kind, location_scope, building_code, floor_index,
     x, y, z, sector_x, sector_y, chunk_x, chunk_y, local_x, local_y,
     moved_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, 0, NULL, 1, '{}'::jsonb)`,
		worldID, actorID, location.SpaceKind, location.LocationScope, cityOpenWorldNullableString(location.BuildingCode),
		location.FloorIndex, location.X, location.Y, location.Z, location.SectorX, location.SectorY,
		location.ChunkX, location.ChunkY, location.LocalX, location.LocalY,
	); err != nil {
		return fmt.Errorf("insert V5 genesis actor location: %w", err)
	}
	return nil
}

func updateCityOpenWorldActorLocation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, actorID, targetTick, sourceFactID int64,
	location CityOpenWorldActorLocation,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_actor_locations
SET space_kind = $3, location_scope = $4, building_code = $5, floor_index = $6,
    x = $7, y = $8, z = $9, sector_x = $10, sector_y = $11, chunk_x = $12,
    chunk_y = $13, local_x = $14, local_y = $15, moved_tick = $16,
    source_fact_id = $17, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2`,
		worldID, actorID, location.SpaceKind, location.LocationScope, cityOpenWorldNullableString(location.BuildingCode),
		location.FloorIndex, location.X, location.Y, location.Z, location.SectorX, location.SectorY,
		location.ChunkX, location.ChunkY, location.LocalX, location.LocalY, targetTick, sourceFactID)
	if err != nil {
		return fmt.Errorf("update open-world actor location: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_actor_location"})
	}
	return nil
}

func cityOpenWorldNullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
