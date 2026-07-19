package service

import (
	"container/heap"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CityNavigationVersion          = "1.1.0"
	CityNavigationDefaultMaxSteps  = 256
	CityNavigationMaximumMaxSteps  = 1024
	cityNavigationMaximumNodes     = 65536
	cityNavigationDiagonalScale    = int64(1414)
	cityNavigationScaleDenominator = int64(1000)

	CityNavigationBlockOutsideWorld     = "outside_world"
	CityNavigationBlockChunkUnavailable = "chunk_not_generated"
	CityNavigationBlockTerrain          = "terrain_blocked"
	CityNavigationBlockFurniture        = "furniture_blocked"
	CityNavigationBlockBuildingWall     = "building_wall"
	CityNavigationBlockVoid             = "void"
	CityNavigationBlockOccupied         = "actor_occupied"
	CityNavigationBlockPortalRequired   = "portal_required"
	CityNavigationBlockPortalClosed     = "portal_closed"
	CityNavigationBlockPortalLocked     = "portal_locked"
	CityNavigationBlockPortalAccess     = "portal_access_denied"
	CityNavigationBlockCorner           = "corner_blocked"
	CityNavigationBlockSearchLimit      = "search_limit"
	CityNavigationBlockUnreachable      = "unreachable"

	worldRuntimeRejectionNavigationUnavailable  = "WORLD_NAVIGATION_UNAVAILABLE"
	worldRuntimeRejectionNavigationBlocked      = "WORLD_NAVIGATION_CELL_BLOCKED"
	worldRuntimeRejectionNavigationOccupied     = "WORLD_NAVIGATION_CELL_OCCUPIED"
	worldRuntimeRejectionNavigationPortal       = "WORLD_NAVIGATION_PORTAL_REQUIRED"
	worldRuntimeRejectionNavigationPortalClosed = "WORLD_NAVIGATION_PORTAL_CLOSED"
	worldRuntimeRejectionNavigationPortalLocked = "WORLD_NAVIGATION_PORTAL_LOCKED"
	worldRuntimeRejectionNavigationPortalAccess = "WORLD_NAVIGATION_PORTAL_ACCESS_DENIED"
)

var ErrCityNavigationUnavailable = infraerrors.NotFound(
	"CITY_NAVIGATION_UNAVAILABLE", "city navigation is unavailable for this world",
)

type CityNavigationCoordinate struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
	Z int32 `json:"z"`
}

type CityNavigationPathInput struct {
	UserID      int64
	WorldID     int64
	ActorCode   string
	Destination CityNavigationCoordinate
	MaxSteps    int
}

type CityNavigationPathStep struct {
	Coordinate       CityNavigationCoordinate `json:"coordinate"`
	StepCost         int64                    `json:"step_cost"`
	TotalCost        int64                    `json:"total_cost"`
	AnchorKind       string                   `json:"anchor_kind"`
	AnchorCode       string                   `json:"anchor_code"`
	JurisdictionCode string                   `json:"jurisdiction_code"`
}

type CityNavigationPath struct {
	NavigationVersion string                   `json:"navigation_version"`
	WorldTick         int64                    `json:"world_tick"`
	SpatialRuleHash   string                   `json:"spatial_rule_hash"`
	ActorCode         string                   `json:"actor_code"`
	From              CityNavigationCoordinate `json:"from"`
	To                CityNavigationCoordinate `json:"to"`
	Reachable         bool                     `json:"reachable"`
	Reason            string                   `json:"reason,omitempty"`
	TotalCost         int64                    `json:"total_cost"`
	ExpandedNodes     int                      `json:"expanded_nodes"`
	Steps             []CityNavigationPathStep `json:"steps"`
}

type cityNavigationDefinition struct {
	movementCost int64
	passable     bool
}

type cityNavigationBuilding struct {
	code               string
	jurisdictionCode   string
	primaryUse         cityspatial.LandUse
	footprint          cityspatial.LandRectangle
	minimumX, maximumX int64
	minimumY, maximumY int64
	minimumZ, maximumZ int32
	layoutCells        map[CityNavigationCoordinate]cityspatial.BuildingLayoutCellKind
}

func (building cityNavigationBuilding) contains(coordinate CityNavigationCoordinate) bool {
	if building.layoutCells != nil {
		_, exists := building.layoutCells[coordinate]
		return exists
	}
	return coordinate.X >= building.minimumX && coordinate.X <= building.maximumX &&
		coordinate.Y >= building.minimumY && coordinate.Y <= building.maximumY &&
		coordinate.Z >= building.minimumZ && coordinate.Z <= building.maximumZ
}

func (building cityNavigationBuilding) edge(coordinate CityNavigationCoordinate) bool {
	if kind, exists := building.layoutCellKind(coordinate); exists {
		return !cityspatial.BuildingLayoutCellPassable(kind)
	}
	return building.contains(coordinate) &&
		(coordinate.X == building.minimumX || coordinate.X == building.maximumX ||
			coordinate.Y == building.minimumY || coordinate.Y == building.maximumY)
}

func (building cityNavigationBuilding) layoutCellKind(
	coordinate CityNavigationCoordinate,
) (cityspatial.BuildingLayoutCellKind, bool) {
	if building.layoutCells == nil {
		return "", false
	}
	kind, exists := building.layoutCells[coordinate]
	return kind, exists
}

type cityNavigationPortal struct {
	code              string
	buildingCode      string
	portalType        string
	from              CityNavigationCoordinate
	to                CityNavigationCoordinate
	bidirectional     bool
	stateCode         string
	accessRequirement WorldRequirementNode
	accessPolicyHash  string
}

func (portal cityNavigationPortal) connects(from, to CityNavigationCoordinate) bool {
	return portal.from == from && portal.to == to ||
		portal.bidirectional && portal.to == from && portal.from == to
}

func (portal cityNavigationPortal) other(coordinate CityNavigationCoordinate) (CityNavigationCoordinate, bool) {
	if portal.from == coordinate {
		return portal.to, true
	}
	if portal.bidirectional && portal.to == coordinate {
		return portal.from, true
	}
	return CityNavigationCoordinate{}, false
}

type cityNavigationChunk struct {
	terrain   []string
	furniture map[int]string
}

type cityNavigationCell struct {
	coordinate       CityNavigationCoordinate
	passable         bool
	movementCost     int64
	generated        bool
	blockReason      string
	terrainID        string
	furnitureID      string
	buildingCode     string
	insideBuilding   bool
	portalCodes      []string
	occupiedActors   []string
	anchorKind       string
	anchorCode       string
	jurisdictionCode string
}

type cityNavigationContext struct {
	ctx                 context.Context
	queryer             citySQLQueryer
	worldID             int64
	worldTick           int64
	profile             *CitySpatialProfile
	ruleHash            string
	minimumMovementCost int64
	definitions         map[string]cityNavigationDefinition
	buildings           []cityNavigationBuilding
	portals             []cityNavigationPortal
	portalsByCell       map[CityNavigationCoordinate][]cityNavigationPortal
	occupiedByCell      map[CityNavigationCoordinate][]string
	jurisdictionByChunk map[cityspatial.ChunkCoordinate]string
	chunks              map[cityspatial.ChunkCoordinate]*cityNavigationChunk
	missingChunks       map[cityspatial.ChunkCoordinate]struct{}
	actorIDByCode       map[string]int64
	portalAccessCache   map[string]bool
	dynamicPortalAccess bool
}

func (s *CityEconomyService) FindWorldActorPath(
	ctx context.Context,
	input CityNavigationPathInput,
) (*CityNavigationPath, error) {
	input.ActorCode = strings.ToLower(strings.TrimSpace(input.ActorCode))
	if input.UserID <= 0 || input.WorldID <= 0 || !worldRuntimeCodeValid(input.ActorCode, 128) {
		return nil, ErrCityInvalidInput
	}
	if input.MaxSteps == 0 {
		input.MaxSteps = CityNavigationDefaultMaxSteps
	}
	if input.MaxSteps < 1 || input.MaxSteps > CityNavigationMaximumMaxSteps {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "max_steps"})
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("begin city navigation snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = authorizeCityWorldRead(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	_, origin, simulationVersion, err := loadCityNavigationActor(
		ctx, tx, input.WorldID, input.UserID, input.ActorCode,
	)
	if err != nil {
		return nil, err
	}
	if !cityEngineSupportsWorldActorNavigation(simulationVersion) {
		return nil, ErrCityNavigationUnavailable
	}
	navigation, err := newCityNavigationContext(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	result, err := navigation.findPath(
		input.ActorCode,
		CityNavigationCoordinate{X: origin.X, Y: origin.Y, Z: origin.Z},
		input.Destination,
		input.MaxSteps,
	)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city navigation snapshot: %w", err)
	}
	return result, nil
}

func loadCityNavigationActor(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, userID int64,
	actorCode string,
) (int64, *WorldActorLocation, string, error) {
	var actorID int64
	var simulationVersion string
	query := `
SELECT actor.id, world.simulation_version
FROM world_actors actor
JOIN city_worlds world ON world.id = actor.world_id
WHERE actor.world_id = $1 AND actor.code = $2 AND actor.status = 'active'
  AND (actor.owner_user_id = $3 OR EXISTS (
      SELECT 1
      FROM world_actor_control_grants grant_value
      JOIN city_members member
        ON member.world_id = grant_value.world_id AND member.user_id = grant_value.user_id
       AND member.status = 'active'
      WHERE grant_value.world_id = actor.world_id AND grant_value.actor_id = actor.id
        AND grant_value.user_id = $3 AND grant_value.capability = $4
        AND grant_value.status = 'active'
	  ))`
	args := []any{worldID, actorCode, userID, WorldActorCapabilityCommand}
	if IsCitySystemAdministrator(ctx) {
		query = `
SELECT actor.id, world.simulation_version
FROM world_actors actor
JOIN city_worlds world ON world.id = actor.world_id
WHERE actor.world_id = $1 AND actor.code = $2 AND actor.status = 'active'`
		args = []any{worldID, actorCode}
	}
	err := queryer.QueryRowContext(ctx, query, args...).Scan(&actorID, &simulationVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, "", ErrCityPermissionDenied
	}
	if err != nil {
		return 0, nil, "", fmt.Errorf("load navigation actor authorization: %w", err)
	}
	location, err := scanWorldActorLocation(queryer.QueryRowContext(ctx, `
SELECT actor.code, location.space_kind, location.space_code, location.x, location.y,
       location.z, location.chunk_x, location.chunk_y, location.local_x, location.local_y,
       location.anchor_kind, location.anchor_code, location.jurisdiction_code,
       location.moved_tick, fact.tick, fact.sequence, location.version, location.metadata
FROM world_actor_locations location
JOIN world_actors actor ON actor.id = location.actor_id AND actor.world_id = location.world_id
LEFT JOIN world_runtime_facts fact ON fact.id = location.source_fact_id AND fact.world_id = location.world_id
WHERE location.world_id = $1 AND location.actor_id = $2`, worldID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, "", ErrCityNavigationUnavailable
	}
	if err != nil {
		return 0, nil, "", fmt.Errorf("load navigation actor location: %w", err)
	}
	return actorID, location, simulationVersion, nil
}

func newCityNavigationContext(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityNavigationContext, error) {
	profile, err := loadCitySpatialProfile(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	ruleSet, err := loadBoundCitySpatialRuleSet(profile)
	if err != nil {
		return nil, err
	}
	navigation := &cityNavigationContext{
		ctx: ctx, queryer: queryer, worldID: worldID, profile: profile,
		ruleHash:            profile.RuleSetHash,
		definitions:         make(map[string]cityNavigationDefinition, len(ruleSet.Definitions)),
		portalsByCell:       make(map[CityNavigationCoordinate][]cityNavigationPortal),
		occupiedByCell:      make(map[CityNavigationCoordinate][]string),
		jurisdictionByChunk: make(map[cityspatial.ChunkCoordinate]string),
		chunks:              make(map[cityspatial.ChunkCoordinate]*cityNavigationChunk),
		missingChunks:       make(map[cityspatial.ChunkCoordinate]struct{}),
		actorIDByCode:       make(map[string]int64),
		portalAccessCache:   make(map[string]bool),
	}
	for _, definition := range ruleSet.Definitions {
		passable := false
		for _, flag := range definition.Flags {
			if flag == "passable" {
				passable = true
				break
			}
		}
		navigation.definitions[definition.ID] = cityNavigationDefinition{
			movementCost: int64(definition.MovementCost), passable: passable,
		}
		if passable && definition.MovementCost > 0 &&
			(navigation.minimumMovementCost == 0 || int64(definition.MovementCost) < navigation.minimumMovementCost) {
			navigation.minimumMovementCost = int64(definition.MovementCost)
		}
	}
	if navigation.minimumMovementCost == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "navigation_minimum_movement_cost"})
	}
	var simulationVersion string
	if err = queryer.QueryRowContext(ctx, `
SELECT current_tick, simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(
		&navigation.worldTick, &simulationVersion,
	); err != nil {
		return nil, fmt.Errorf("load navigation world tick: %w", err)
	}
	navigation.dynamicPortalAccess = cityEngineSupportsWorldPortalAccess(simulationVersion)
	if err = navigation.loadJurisdictions(); err != nil {
		return nil, err
	}
	if err = navigation.loadBuildings(); err != nil {
		return nil, err
	}
	if err = navigation.loadPortals(); err != nil {
		return nil, err
	}
	if err = navigation.hydrateBuildingLayouts(); err != nil {
		return nil, err
	}
	if err = navigation.loadOccupancy(); err != nil {
		return nil, err
	}
	return navigation, nil
}

func (navigation *cityNavigationContext) loadJurisdictions() error {
	rows, err := navigation.queryer.QueryContext(navigation.ctx, `
SELECT tile.chunk_x, tile.chunk_y, district.code
FROM city_overmap_tiles tile
JOIN city_districts district
  ON district.id = tile.district_id AND district.world_id = tile.world_id
WHERE tile.world_id = $1 AND tile.z = 0
ORDER BY tile.chunk_y ASC, tile.chunk_x ASC`, navigation.worldID)
	if err != nil {
		return fmt.Errorf("load navigation jurisdictions: %w", err)
	}
	for rows.Next() {
		var coordinate cityspatial.ChunkCoordinate
		var jurisdictionCode string
		if err = rows.Scan(&coordinate.X, &coordinate.Y, &jurisdictionCode); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan navigation jurisdiction: %w", err)
		}
		coordinate.Z = cityspatial.SurfaceZ
		navigation.jurisdictionByChunk[coordinate] = jurisdictionCode
	}
	return closeCityRows(rows, "iterate navigation jurisdictions")
}

func (navigation *cityNavigationContext) loadBuildings() error {
	rows, err := navigation.queryer.QueryContext(navigation.ctx, `
SELECT building.code, district.code, building.primary_use, building.chunk_x, building.chunk_y, building.footprint_z,
       building.local_min_x, building.local_min_y,
       building.local_max_x, building.local_max_y,
       building.base_z, building.top_z
FROM city_buildings building
JOIN city_districts district
  ON district.id = building.district_id AND district.world_id = building.world_id
WHERE building.world_id = $1 AND building.status = 'active'
ORDER BY building.code ASC`, navigation.worldID)
	if err != nil {
		return fmt.Errorf("load navigation buildings: %w", err)
	}
	for rows.Next() {
		var building cityNavigationBuilding
		var chunkX, chunkY int64
		var footprintZ int32
		var localMinimumX, localMinimumY, localMaximumX, localMaximumY int64
		if err = rows.Scan(&building.code, &building.jurisdictionCode, &building.primaryUse, &chunkX, &chunkY, &footprintZ,
			&localMinimumX, &localMinimumY, &localMaximumX, &localMaximumY,
			&building.minimumZ, &building.maximumZ); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan navigation building: %w", err)
		}
		building.minimumX = chunkX*navigation.profile.ChunkSize + localMinimumX
		building.maximumX = chunkX*navigation.profile.ChunkSize + localMaximumX
		building.minimumY = chunkY*navigation.profile.ChunkSize + localMinimumY
		building.maximumY = chunkY*navigation.profile.ChunkSize + localMaximumY
		building.footprint = cityspatial.LandRectangle{
			ChunkX: chunkX, ChunkY: chunkY, Z: footprintZ,
			LocalMinX: int32(localMinimumX), LocalMinY: int32(localMinimumY),
			LocalMaxX: int32(localMaximumX), LocalMaxY: int32(localMaximumY),
		}
		navigation.buildings = append(navigation.buildings, building)
	}
	return closeCityRows(rows, "iterate navigation buildings")
}

func (navigation *cityNavigationContext) hydrateBuildingLayouts() error {
	portalsByBuilding := make(map[string][]cityspatial.GeneratedBuildingPortal, len(navigation.buildings))
	for _, portal := range navigation.portals {
		portalsByBuilding[portal.buildingCode] = append(portalsByBuilding[portal.buildingCode],
			cityspatial.GeneratedBuildingPortal{
				Code: portal.code, BuildingCode: portal.buildingCode, PortalType: portal.portalType,
				FromX: portal.from.X, FromY: portal.from.Y, FromZ: portal.from.Z,
				ToX: portal.to.X, ToY: portal.to.Y, ToZ: portal.to.Z,
				Bidirectional: portal.bidirectional, Status: "active",
			},
		)
	}
	for index := range navigation.buildings {
		building := &navigation.buildings[index]
		layout, err := cityspatial.GenerateBuildingLayout(cityspatial.GeneratedBuilding{
			Code: building.code, PrimaryUse: building.primaryUse, Footprint: building.footprint,
			BaseZ: building.minimumZ, TopZ: building.maximumZ,
			FloorCount: building.maximumZ - building.minimumZ + 1,
		}, portalsByBuilding[building.code])
		if err != nil {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "navigation_building_layout", "building_code": building.code,
			}).WithCause(err)
		}
		cells := make(map[CityNavigationCoordinate]cityspatial.BuildingLayoutCellKind, len(layout.Cells))
		for _, cell := range layout.Cells {
			coordinate := CityNavigationCoordinate{X: cell.X, Y: cell.Y, Z: cell.Z}
			if _, duplicate := cells[coordinate]; duplicate {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{
					"field": "navigation_building_layout_cell", "building_code": building.code,
				})
			}
			cells[coordinate] = cell.Kind
		}
		building.layoutCells = cells
	}
	return nil
}

func (navigation *cityNavigationContext) loadPortals() error {
	rows, err := navigation.queryer.QueryContext(navigation.ctx, `
SELECT portal.code, building.code, portal.portal_type,
       portal.from_x, portal.from_y, portal.from_z,
       portal.to_x, portal.to_y, portal.to_z, portal.bidirectional,
       COALESCE(state_value.state_code, 'open'),
       COALESCE(state_value.access_requirement, '{"op":"all","items":[]}'::jsonb),
       COALESCE(state_value.access_policy_hash, '')
FROM city_building_portals portal
JOIN city_buildings building
  ON building.id = portal.building_id AND building.world_id = portal.world_id
LEFT JOIN world_portal_states state_value
  ON state_value.portal_id = portal.id AND state_value.world_id = portal.world_id
WHERE portal.world_id = $1 AND portal.status = 'active' AND building.status = 'active'
ORDER BY building.code ASC, portal.code ASC`, navigation.worldID)
	if err != nil {
		return fmt.Errorf("load navigation portals: %w", err)
	}
	for rows.Next() {
		var portal cityNavigationPortal
		var requirementRaw json.RawMessage
		if err = rows.Scan(&portal.code, &portal.buildingCode, &portal.portalType,
			&portal.from.X, &portal.from.Y, &portal.from.Z,
			&portal.to.X, &portal.to.Y, &portal.to.Z, &portal.bidirectional,
			&portal.stateCode, &requirementRaw, &portal.accessPolicyHash); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan navigation portal: %w", err)
		}
		if err = decodeStrictCityObject(requirementRaw, &portal.accessRequirement); err != nil {
			_ = rows.Close()
			return fmt.Errorf("decode navigation portal access requirement: %w", err)
		}
		requirement, _, policyHash, policyErr :=
			canonicalWorldPortalAccessRequirement(portal.accessRequirement)
		if policyErr != nil || portal.accessPolicyHash != "" && portal.accessPolicyHash != policyHash ||
			navigation.dynamicPortalAccess && portal.accessPolicyHash == "" {
			_ = rows.Close()
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "navigation_portal_access_policy"})
		}
		if portal.accessPolicyHash == "" {
			portal.accessPolicyHash = policyHash
		}
		portal.accessRequirement = requirement
		navigation.portals = append(navigation.portals, portal)
		navigation.portalsByCell[portal.from] = append(navigation.portalsByCell[portal.from], portal)
		navigation.portalsByCell[portal.to] = append(navigation.portalsByCell[portal.to], portal)
	}
	return closeCityRows(rows, "iterate navigation portals")
}

func (navigation *cityNavigationContext) loadOccupancy() error {
	rows, err := navigation.queryer.QueryContext(navigation.ctx, `
SELECT location.x, location.y, location.z, actor.id, actor.code
FROM world_actor_locations location
JOIN world_actors actor ON actor.id = location.actor_id AND actor.world_id = location.world_id
WHERE location.world_id = $1 AND actor.status = 'active'
ORDER BY location.z ASC, location.y ASC, location.x ASC, actor.code ASC`, navigation.worldID)
	if err != nil {
		return fmt.Errorf("load navigation actor occupancy: %w", err)
	}
	for rows.Next() {
		var coordinate CityNavigationCoordinate
		var actorID int64
		var actorCode string
		if err = rows.Scan(&coordinate.X, &coordinate.Y, &coordinate.Z, &actorID, &actorCode); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan navigation actor occupancy: %w", err)
		}
		navigation.occupiedByCell[coordinate] = append(navigation.occupiedByCell[coordinate], actorCode)
		navigation.actorIDByCode[actorCode] = actorID
	}
	return closeCityRows(rows, "iterate navigation actor occupancy")
}

func (navigation *cityNavigationContext) resolveCell(
	coordinate CityNavigationCoordinate,
	movingActorCode string,
) (cityNavigationCell, error) {
	cell := cityNavigationCell{coordinate: coordinate, movementCost: 0}
	address, err := cityspatial.SplitWorldCoordinate(
		cityspatial.WorldCoordinate{X: coordinate.X, Y: coordinate.Y, Z: coordinate.Z},
		navigation.profile.ChunkSize,
	)
	if err != nil || address.Chunk.X < navigation.profile.MinimumChunkX ||
		address.Chunk.X > navigation.profile.MaximumChunkX ||
		address.Chunk.Y < navigation.profile.MinimumChunkY ||
		address.Chunk.Y > navigation.profile.MaximumChunkY ||
		coordinate.Z < navigation.profile.MinimumZ || coordinate.Z > navigation.profile.MaximumZ {
		cell.blockReason = CityNavigationBlockOutsideWorld
		return cell, nil
	}
	cell.anchorKind = "chunk"
	cell.anchorCode = worldRuntimeChunkAnchorCode(address.Chunk.X, address.Chunk.Y, coordinate.Z)
	if coordinate.Z == cityspatial.SurfaceZ {
		cell.jurisdictionCode = navigation.jurisdictionByChunk[address.Chunk]
		if cell.jurisdictionCode == "" {
			cell.blockReason = CityNavigationBlockChunkUnavailable
			return cell, nil
		}
	}
	building := navigation.buildingAt(coordinate)
	portals := navigation.portalsByCell[coordinate]
	for _, portal := range portals {
		cell.portalCodes = append(cell.portalCodes, portal.buildingCode+":"+portal.code)
	}
	if building != nil {
		cell.buildingCode = building.code
		cell.insideBuilding = true
		cell.anchorKind, cell.anchorCode = "building", building.code
		cell.jurisdictionCode = building.jurisdictionCode
		cell.generated = true
		cell.terrainID = "terrain.floor"
		floor := navigation.definitions[cell.terrainID]
		cell.movementCost = floor.movementCost
		if kind, hasLayout := building.layoutCellKind(coordinate); hasLayout {
			cell.passable = floor.passable && (cityspatial.BuildingLayoutCellPassable(kind) || len(portals) > 0)
		} else {
			cell.passable = floor.passable && (!building.edge(coordinate) || len(portals) > 0)
		}
		if !cell.passable {
			cell.blockReason = CityNavigationBlockBuildingWall
		}
	} else if coordinate.Z != cityspatial.SurfaceZ {
		cell.blockReason = CityNavigationBlockVoid
	} else {
		chunk, available, loadErr := navigation.loadChunk(address.Chunk)
		if loadErr != nil {
			return cell, loadErr
		}
		if !available {
			cell.blockReason = CityNavigationBlockChunkUnavailable
			return cell, nil
		}
		cell.generated = true
		index := int(address.Local.Y)*int(navigation.profile.ChunkSize) + int(address.Local.X)
		if index < 0 || index >= len(chunk.terrain) {
			return cell, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "navigation_chunk_index"})
		}
		cell.terrainID = chunk.terrain[index]
		terrain := navigation.definitions[cell.terrainID]
		cell.movementCost, cell.passable = terrain.movementCost, terrain.passable
		if !cell.passable {
			cell.blockReason = CityNavigationBlockTerrain
		}
		if furnitureID := chunk.furniture[index]; furnitureID != "" {
			cell.furnitureID = furnitureID
			furniture := navigation.definitions[furnitureID]
			if len(portals) == 0 && !furniture.passable {
				cell.passable = false
				cell.blockReason = CityNavigationBlockFurniture
			} else if furniture.passable && furniture.movementCost > cell.movementCost {
				cell.movementCost = furniture.movementCost
			}
		}
		if len(portals) > 0 {
			cell.buildingCode = portals[0].buildingCode
		}
	}
	for _, actorCode := range navigation.occupiedByCell[coordinate] {
		if actorCode != movingActorCode {
			cell.occupiedActors = append(cell.occupiedActors, actorCode)
		}
	}
	if cell.passable && len(cell.occupiedActors) > 0 {
		cell.passable = false
		cell.blockReason = CityNavigationBlockOccupied
	}
	if cell.passable && cell.movementCost <= 0 {
		return cell, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "navigation_movement_cost"})
	}
	return cell, nil
}

func (navigation *cityNavigationContext) buildingAt(
	coordinate CityNavigationCoordinate,
) *cityNavigationBuilding {
	for index := range navigation.buildings {
		if navigation.buildings[index].contains(coordinate) {
			return &navigation.buildings[index]
		}
	}
	return nil
}

func (navigation *cityNavigationContext) loadChunk(
	coordinate cityspatial.ChunkCoordinate,
) (*cityNavigationChunk, bool, error) {
	if chunk, exists := navigation.chunks[coordinate]; exists {
		return chunk, true, nil
	}
	if _, missing := navigation.missingChunks[coordinate]; missing {
		return nil, false, nil
	}
	chunk, err := loadCityMapChunk(
		navigation.ctx, navigation.queryer, navigation.worldID,
		coordinate.X, coordinate.Y, coordinate.Z, navigation.profile,
	)
	if errors.Is(err, ErrCitySpatialChunkNotFound) {
		navigation.missingChunks[coordinate] = struct{}{}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	decoded := &cityNavigationChunk{
		terrain:   make([]string, 0, chunk.Payload.Width*chunk.Payload.Height),
		furniture: make(map[int]string, len(chunk.Payload.Furniture)),
	}
	for _, run := range chunk.Payload.TerrainRuns {
		for index := 0; index < run.Length; index++ {
			decoded.terrain = append(decoded.terrain, run.DefinitionID)
		}
	}
	for _, furniture := range chunk.Payload.Furniture {
		index := int(furniture.Y)*chunk.Payload.Width + int(furniture.X)
		decoded.furniture[index] = furniture.DefinitionID
	}
	navigation.chunks[coordinate] = decoded
	return decoded, true, nil
}

func (navigation *cityNavigationContext) transitionPortal(
	from, to CityNavigationCoordinate,
) *cityNavigationPortal {
	for index := range navigation.portalsByCell[from] {
		portal := navigation.portalsByCell[from][index]
		if portal.connects(from, to) {
			return &portal
		}
	}
	return nil
}

func (navigation *cityNavigationContext) validateTransition(
	fromCell, toCell cityNavigationCell,
) (*cityNavigationPortal, string) {
	from, to := fromCell.coordinate, toCell.coordinate
	dx, dy := absoluteInt64(to.X-from.X), absoluteInt64(to.Y-from.Y)
	dz := int64(to.Z - from.Z)
	if dz < 0 {
		dz = -dz
	}
	if dx > 1 || dy > 1 || dz > 1 || dx == 0 && dy == 0 && dz == 0 || dz > 0 && (dx > 0 || dy > 0) {
		return nil, CityNavigationBlockPortalRequired
	}
	portal := navigation.transitionPortal(from, to)
	if dz > 0 {
		if portal == nil {
			return nil, CityNavigationBlockPortalRequired
		}
		return portal, ""
	}
	if fromCell.insideBuilding != toCell.insideBuilding ||
		fromCell.insideBuilding && toCell.insideBuilding && fromCell.buildingCode != toCell.buildingCode {
		if portal == nil || portal.portalType != "entrance" {
			return nil, CityNavigationBlockPortalRequired
		}
	}
	return portal, ""
}

func (navigation *cityNavigationContext) validatePortalAccess(
	portal *cityNavigationPortal,
	movingActorCode string,
) (string, error) {
	if portal == nil || !navigation.dynamicPortalAccess {
		return "", nil
	}
	switch portal.stateCode {
	case WorldPortalStateOpen:
	case WorldPortalStateClosed:
		return CityNavigationBlockPortalClosed, nil
	case WorldPortalStateLocked:
		return CityNavigationBlockPortalLocked, nil
	default:
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "navigation_portal_state",
		})
	}
	actorID, exists := navigation.actorIDByCode[movingActorCode]
	if !exists || actorID <= 0 {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "navigation_actor_occupancy",
		})
	}
	cacheKey := strconv.FormatInt(actorID, 10) + ":" + portal.buildingCode + ":" +
		portal.code + ":" + portal.accessPolicyHash
	if allowed, cached := navigation.portalAccessCache[cacheKey]; cached {
		if allowed {
			return "", nil
		}
		return CityNavigationBlockPortalAccess, nil
	}
	evaluation, err := evaluateWorldPortalAccess(
		navigation.ctx,
		navigation.queryer,
		navigation.worldID,
		actorID,
		navigation.worldTick,
		portal.accessRequirement,
	)
	if err != nil {
		return "", err
	}
	navigation.portalAccessCache[cacheKey] = evaluation.Satisfied
	if !evaluation.Satisfied {
		return CityNavigationBlockPortalAccess, nil
	}
	return "", nil
}

func (navigation *cityNavigationContext) resolveStep(
	from CityNavigationCoordinate,
	to CityNavigationCoordinate,
	movingActorCode string,
) (cityNavigationCell, int64, string, error) {
	fromCell, err := navigation.resolveCell(from, movingActorCode)
	if err != nil {
		return cityNavigationCell{}, 0, "", err
	}
	toCell, err := navigation.resolveCell(to, movingActorCode)
	if err != nil {
		return cityNavigationCell{}, 0, "", err
	}
	if !toCell.passable {
		return toCell, 0, toCell.blockReason, nil
	}
	portal, reason := navigation.validateTransition(fromCell, toCell)
	if reason != "" {
		return toCell, 0, reason, nil
	}
	if reason, err = navigation.validatePortalAccess(portal, movingActorCode); err != nil || reason != "" {
		return toCell, 0, reason, err
	}
	dx, dy := absoluteInt64(to.X-from.X), absoluteInt64(to.Y-from.Y)
	if dx == 1 && dy == 1 && from.Z == to.Z {
		orthogonal := []CityNavigationCoordinate{
			{X: to.X, Y: from.Y, Z: from.Z},
			{X: from.X, Y: to.Y, Z: from.Z},
		}
		for _, candidate := range orthogonal {
			candidateCell, candidateErr := navigation.resolveCell(candidate, movingActorCode)
			if candidateErr != nil {
				return toCell, 0, "", candidateErr
			}
			if !candidateCell.passable {
				return toCell, 0, CityNavigationBlockCorner, nil
			}
			candidatePortal, candidateReason := navigation.validateTransition(fromCell, candidateCell)
			if candidateReason != "" {
				return toCell, 0, CityNavigationBlockCorner, nil
			}
			if candidateReason, candidateErr = navigation.validatePortalAccess(
				candidatePortal, movingActorCode,
			); candidateErr != nil {
				return toCell, 0, "", candidateErr
			} else if candidateReason != "" {
				return toCell, 0, CityNavigationBlockCorner, nil
			}
		}
	}
	cost := toCell.movementCost
	if dx == 1 && dy == 1 {
		cost = (cost*cityNavigationDiagonalScale + cityNavigationScaleDenominator - 1) /
			cityNavigationScaleDenominator
	}
	if portal != nil && portal.portalType == "stair" {
		if stairs := navigation.definitions["portal.stairs_up"].movementCost; stairs > cost {
			cost = stairs
		}
	}
	return toCell, cost, "", nil
}

type cityNavigationQueueItem struct {
	coordinate CityNavigationCoordinate
	cost       int64
	estimate   int64
	depth      int
	index      int
}

type cityNavigationQueue []*cityNavigationQueueItem

func (queue cityNavigationQueue) Len() int { return len(queue) }

func (queue cityNavigationQueue) Less(left, right int) bool {
	leftScore := queue[left].cost + queue[left].estimate
	rightScore := queue[right].cost + queue[right].estimate
	if leftScore != rightScore {
		return leftScore < rightScore
	}
	if queue[left].estimate != queue[right].estimate {
		return queue[left].estimate < queue[right].estimate
	}
	if queue[left].coordinate.Z != queue[right].coordinate.Z {
		return queue[left].coordinate.Z < queue[right].coordinate.Z
	}
	if queue[left].coordinate.Y != queue[right].coordinate.Y {
		return queue[left].coordinate.Y < queue[right].coordinate.Y
	}
	return queue[left].coordinate.X < queue[right].coordinate.X
}

func (queue cityNavigationQueue) Swap(left, right int) {
	queue[left], queue[right] = queue[right], queue[left]
	queue[left].index, queue[right].index = left, right
}

func (queue *cityNavigationQueue) Push(value any) {
	item := value.(*cityNavigationQueueItem)
	item.index = len(*queue)
	*queue = append(*queue, item)
}

func (queue *cityNavigationQueue) Pop() any {
	old := *queue
	item := old[len(old)-1]
	old[len(old)-1] = nil
	item.index = -1
	*queue = old[:len(old)-1]
	return item
}

var cityNavigationPlanarOffsets = []CityNavigationCoordinate{
	{X: 0, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 0}, {X: 1, Y: 1},
	{X: 0, Y: 1}, {X: -1, Y: 1}, {X: -1, Y: 0}, {X: -1, Y: -1},
}

func (navigation *cityNavigationContext) neighbors(
	coordinate CityNavigationCoordinate,
) []CityNavigationCoordinate {
	items := make([]CityNavigationCoordinate, 0, 8+len(navigation.portalsByCell[coordinate]))
	seen := make(map[CityNavigationCoordinate]struct{}, cap(items))
	for _, offset := range cityNavigationPlanarOffsets {
		candidate := CityNavigationCoordinate{
			X: coordinate.X + offset.X, Y: coordinate.Y + offset.Y, Z: coordinate.Z,
		}
		seen[candidate] = struct{}{}
		items = append(items, candidate)
	}
	for _, portal := range navigation.portalsByCell[coordinate] {
		candidate, ok := portal.other(coordinate)
		if !ok {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		items = append(items, candidate)
	}
	return items
}

func cityNavigationHeuristic(from, to CityNavigationCoordinate, minimumMovementCost int64) int64 {
	dx, dy := absoluteInt64(to.X-from.X), absoluteInt64(to.Y-from.Y)
	planar := dx
	if dy > planar {
		planar = dy
	}
	dz := int64(to.Z - from.Z)
	if dz < 0 {
		dz = -dz
	}
	return (planar + dz) * minimumMovementCost
}

func (navigation *cityNavigationContext) findPath(
	actorCode string,
	from, to CityNavigationCoordinate,
	maxSteps int,
) (*CityNavigationPath, error) {
	result := &CityNavigationPath{
		NavigationVersion: CityNavigationVersion,
		WorldTick:         navigation.worldTick, SpatialRuleHash: navigation.ruleHash,
		ActorCode: actorCode, From: from, To: to, Steps: []CityNavigationPathStep{},
	}
	startCell, err := navigation.resolveCell(from, actorCode)
	if err != nil {
		return nil, err
	}
	if from == to {
		result.Reachable = true
		result.Steps = []CityNavigationPathStep{navigation.pathStep(startCell, 0, 0)}
		return result, nil
	}
	destination, err := navigation.resolveCell(to, actorCode)
	if err != nil {
		return nil, err
	}
	if !destination.passable {
		result.Reason = destination.blockReason
		return result, nil
	}
	queue := &cityNavigationQueue{}
	heap.Init(queue)
	heap.Push(queue, &cityNavigationQueueItem{
		coordinate: from, estimate: cityNavigationHeuristic(from, to, navigation.minimumMovementCost),
	})
	costs := map[CityNavigationCoordinate]int64{from: 0}
	depths := map[CityNavigationCoordinate]int{from: 0}
	parents := make(map[CityNavigationCoordinate]CityNavigationCoordinate)
	stepCosts := make(map[CityNavigationCoordinate]int64)
	closed := make(map[CityNavigationCoordinate]struct{})
	hitStepLimit := false
	encounteredChunkGap := false
	encounteredPortalBlock := ""
	maximumNodes := maxSteps * 64
	if maximumNodes > cityNavigationMaximumNodes {
		maximumNodes = cityNavigationMaximumNodes
	}
	for queue.Len() > 0 {
		if result.ExpandedNodes%64 == 0 {
			select {
			case <-navigation.ctx.Done():
				return nil, navigation.ctx.Err()
			default:
			}
		}
		current := heap.Pop(queue).(*cityNavigationQueueItem)
		if known, ok := costs[current.coordinate]; !ok || known != current.cost {
			continue
		}
		if _, exists := closed[current.coordinate]; exists {
			continue
		}
		closed[current.coordinate] = struct{}{}
		result.ExpandedNodes++
		if result.ExpandedNodes > maximumNodes {
			result.Reason = CityNavigationBlockSearchLimit
			return result, nil
		}
		if current.coordinate == to {
			return navigation.reconstructPath(result, parents, stepCosts, costs, actorCode)
		}
		if current.depth >= maxSteps {
			hitStepLimit = true
			continue
		}
		for _, candidate := range navigation.neighbors(current.coordinate) {
			_, stepCost, reason, resolveErr := navigation.resolveStep(current.coordinate, candidate, actorCode)
			if resolveErr != nil {
				return nil, resolveErr
			}
			if reason != "" {
				if reason == CityNavigationBlockChunkUnavailable {
					encounteredChunkGap = true
				} else if encounteredPortalBlock == "" &&
					(reason == CityNavigationBlockPortalClosed ||
						reason == CityNavigationBlockPortalLocked ||
						reason == CityNavigationBlockPortalAccess) {
					encounteredPortalBlock = reason
				}
				continue
			}
			candidateCost := current.cost + stepCost
			knownCost, seen := costs[candidate]
			candidateDepth := current.depth + 1
			if seen && (candidateCost > knownCost || candidateCost == knownCost && candidateDepth >= depths[candidate]) {
				continue
			}
			costs[candidate], depths[candidate] = candidateCost, candidateDepth
			parents[candidate], stepCosts[candidate] = current.coordinate, stepCost
			heap.Push(queue, &cityNavigationQueueItem{
				coordinate: candidate, cost: candidateCost,
				estimate: cityNavigationHeuristic(candidate, to, navigation.minimumMovementCost), depth: candidateDepth,
			})
		}
	}
	switch {
	case hitStepLimit:
		result.Reason = CityNavigationBlockSearchLimit
	case encounteredPortalBlock != "":
		result.Reason = encounteredPortalBlock
	case encounteredChunkGap:
		result.Reason = CityNavigationBlockChunkUnavailable
	default:
		result.Reason = CityNavigationBlockUnreachable
	}
	return result, nil
}

func (navigation *cityNavigationContext) reconstructPath(
	result *CityNavigationPath,
	parents map[CityNavigationCoordinate]CityNavigationCoordinate,
	stepCosts, costs map[CityNavigationCoordinate]int64,
	actorCode string,
) (*CityNavigationPath, error) {
	coordinates := []CityNavigationCoordinate{result.To}
	for coordinates[len(coordinates)-1] != result.From {
		parent, exists := parents[coordinates[len(coordinates)-1]]
		if !exists {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "navigation_path_parent"})
		}
		coordinates = append(coordinates, parent)
	}
	for left, right := 0, len(coordinates)-1; left < right; left, right = left+1, right-1 {
		coordinates[left], coordinates[right] = coordinates[right], coordinates[left]
	}
	result.Steps = make([]CityNavigationPathStep, 0, len(coordinates))
	for index, coordinate := range coordinates {
		cell, err := navigation.resolveCell(coordinate, actorCode)
		if err != nil {
			return nil, err
		}
		stepCost := int64(0)
		if index > 0 {
			stepCost = stepCosts[coordinate]
		}
		result.Steps = append(result.Steps, navigation.pathStep(cell, stepCost, costs[coordinate]))
	}
	result.Reachable = true
	result.TotalCost = costs[result.To]
	return result, nil
}

func (navigation *cityNavigationContext) pathStep(
	cell cityNavigationCell,
	stepCost, totalCost int64,
) CityNavigationPathStep {
	return CityNavigationPathStep{
		Coordinate: cell.coordinate, StepCost: stepCost, TotalCost: totalCost,
		AnchorKind: cell.anchorKind, AnchorCode: cell.anchorCode,
		JurisdictionCode: cell.jurisdictionCode,
	}
}

func validateWorldActorNavigationStep(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	from CityNavigationCoordinate,
	to CityNavigationCoordinate,
) (*cityNavigationCell, error) {
	navigation, err := newCityNavigationContext(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	cell, _, reason, err := navigation.resolveStep(from, to, actorCode)
	if err != nil {
		return nil, err
	}
	if reason == "" {
		return &cell, nil
	}
	switch reason {
	case CityNavigationBlockOccupied:
		return nil, worldRuntimeReject(worldRuntimeRejectionNavigationOccupied)
	case CityNavigationBlockPortalRequired, CityNavigationBlockCorner:
		return nil, worldRuntimeReject(worldRuntimeRejectionNavigationPortal)
	case CityNavigationBlockPortalClosed:
		return nil, worldRuntimeReject(worldRuntimeRejectionNavigationPortalClosed)
	case CityNavigationBlockPortalLocked:
		return nil, worldRuntimeReject(worldRuntimeRejectionNavigationPortalLocked)
	case CityNavigationBlockPortalAccess:
		return nil, worldRuntimeReject(worldRuntimeRejectionNavigationPortalAccess)
	case CityNavigationBlockChunkUnavailable:
		return nil, worldRuntimeReject(worldRuntimeRejectionNavigationUnavailable)
	default:
		return nil, worldRuntimeReject(worldRuntimeRejectionNavigationBlocked)
	}
}

func resolveWorldActorNavigationAnchor(
	cell *cityNavigationCell,
	requestedKind, requestedCode string,
) (string, string, error) {
	if cell == nil {
		return "", "", worldRuntimeReject(worldRuntimeRejectionNavigationBlocked)
	}
	if cell.insideBuilding {
		switch requestedKind {
		case "":
			return "building", cell.buildingCode, nil
		case "building":
			if requestedCode == cell.buildingCode {
				return requestedKind, requestedCode, nil
			}
		case "site":
			return requestedKind, requestedCode, nil
		}
		return "", "", worldRuntimeReject(worldRuntimeRejectionLocationInvalid)
	}
	if requestedKind == "" {
		return "", "", nil
	}
	if requestedKind == "chunk" && requestedCode == cell.anchorCode {
		return requestedKind, requestedCode, nil
	}
	return "", "", worldRuntimeReject(worldRuntimeRejectionLocationInvalid)
}
