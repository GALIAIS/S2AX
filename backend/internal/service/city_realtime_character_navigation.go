package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

const cityRealtimeCharacterActionPortal = "character.portal"

// CityRealtimeCharacterPortalTransition is an owner-safe, server-derived
// navigation affordance. It contains static world topology only; no roster,
// ownership, agent, or access-control internals are disclosed.
type CityRealtimeCharacterPortalTransition struct {
	PortalCode   string                        `json:"portal_code"`
	PortalType   string                        `json:"portal_type"`
	Direction    string                        `json:"direction"`
	BuildingCode string                        `json:"building_code"`
	Target       CityRealtimeCharacterLocation `json:"target"`
}

// CityRealtimeCharacterLocation is reused by portal and local-interior
// projections. It deliberately carries coordinates only, never an Actor or
// user identity.
type CityRealtimeCharacterLocation struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
	Z int32 `json:"z"`
}

// CityRealtimeCharacterInteriorProjection gives a character the currently
// occupied floor's immutable navigation cells. Returning only the current
// floor keeps the read model bounded and does not expose any private state.
type CityRealtimeCharacterInteriorProjection struct {
	BuildingCode string                              `json:"building_code"`
	FloorIndex   int32                               `json:"floor_index"`
	Z            int32                               `json:"z"`
	LayoutStyle  string                              `json:"layout_style"`
	Cells        []CityRealtimeCharacterInteriorCell `json:"cells"`
}

type CityRealtimeCharacterInteriorCell struct {
	X           int64  `json:"x"`
	Y           int64  `json:"y"`
	Z           int32  `json:"z"`
	Kind        string `json:"kind"`
	Feature     string `json:"feature,omitempty"`
	Traversable bool   `json:"traversable"`
}

// CityRealtimeCharacterPortalTraverseInput intentionally accepts only an
// immutable portal code. The server derives the actor, source endpoint,
// direction, target, occupancy, and temporal frame under the world lock.
type CityRealtimeCharacterPortalTraverseInput struct {
	UserID         int64
	WorldID        int64
	PortalCode     string
	IdempotencyKey string
}

type cityRealtimeCharacterInteriorContext struct {
	BuildingCode string
	FloorIndex   int32
	Z            int32
	LayoutStyle  string
	Cells        []cityspatial.GeneratedWorldgenInteriorCell
	Cell         cityspatial.GeneratedWorldgenInteriorCell
}

func (item cityRealtimeCharacterInteriorContext) projection() CityRealtimeCharacterInteriorProjection {
	cells := make([]CityRealtimeCharacterInteriorCell, 0, len(item.Cells))
	for _, cell := range item.Cells {
		cells = append(cells, CityRealtimeCharacterInteriorCell{
			X:           cell.X,
			Y:           cell.Y,
			Z:           cell.Z,
			Kind:        string(cell.Kind),
			Feature:     cell.Feature,
			Traversable: cityRealtimeCharacterInteriorCellTraversable(cell),
		})
	}
	return CityRealtimeCharacterInteriorProjection{
		BuildingCode: item.BuildingCode,
		FloorIndex:   item.FloorIndex,
		Z:            item.Z,
		LayoutStyle:  item.LayoutStyle,
		Cells:        cells,
	}
}

type cityRealtimeCharacterPortalRecord struct {
	Code           string
	BuildingCode   string
	PortalType     string
	FromFloorIndex int32
	ToFloorIndex   int32
	From           cityRealtimeActorSpawnCandidate
	To             cityRealtimeActorSpawnCandidate
	Bidirectional  bool
	TopologyHash   string
	Revision       int64
}

func (item cityRealtimeCharacterPortalRecord) validate() error {
	if !cityRealtimeDueEventIdentifierValid(item.Code, 128) ||
		!cityRealtimeDueEventIdentifierValid(item.BuildingCode, 96) ||
		(item.PortalType != "entrance" && item.PortalType != "stairs") ||
		item.FromFloorIndex < 0 || item.ToFloorIndex < 0 || item.Revision != 1 ||
		!cityRealtimeSHA256Hex(item.TopologyHash) || item.From.Z < cityspatial.SurfaceZ ||
		item.To.Z < cityspatial.SurfaceZ {
		return errors.New("invalid character portal record")
	}
	if item.PortalType == "entrance" {
		if item.FromFloorIndex != 0 || item.ToFloorIndex != 0 ||
			item.From.Z != cityspatial.SurfaceZ || item.To.Z != cityspatial.SurfaceZ {
			return errors.New("invalid entrance portal topology")
		}
	} else if item.ToFloorIndex != item.FromFloorIndex+1 || item.To.Z != item.From.Z+1 {
		return errors.New("invalid stairs portal topology")
	}
	expected, err := cityspatial.ComputeOpenWorldPortalHash(cityspatial.GeneratedOpenWorldPortal{
		Code:           item.Code,
		BuildingCode:   item.BuildingCode,
		PortalType:     item.PortalType,
		FromFloorIndex: item.FromFloorIndex,
		ToFloorIndex:   item.ToFloorIndex,
		From:           cityspatial.WorldgenPoint{X: item.From.X, Y: item.From.Y, Z: item.From.Z},
		To:             cityspatial.WorldgenPoint{X: item.To.X, Y: item.To.Y, Z: item.To.Z},
		Bidirectional:  item.Bidirectional,
	})
	if err != nil || expected != item.TopologyHash {
		return errors.New("character portal topology hash mismatch")
	}
	return nil
}

func cityRealtimeCharacterInteriorCellTraversable(cell cityspatial.GeneratedWorldgenInteriorCell) bool {
	return cell.Kind == cityspatial.BuildingLayoutCellFloor || cell.Kind == cityspatial.BuildingLayoutCellDoor
}

func cityRealtimeCharacterPositionEquals(left, right cityRealtimeActorSpawnCandidate) bool {
	return left.X == right.X && left.Y == right.Y && left.Z == right.Z
}

// loadCityRealtimeCharacterInteriorAt validates the persisted immutable
// interior before using it for a movement decision. A world that contains two
// interior definitions for one coordinate is malformed rather than allowing
// a path to be chosen by accidental query order.
func loadCityRealtimeCharacterInteriorAt(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	position cityRealtimeActorSpawnCandidate,
) (cityRealtimeCharacterInteriorContext, bool, error) {
	if worldID <= 0 || position.Z < cityspatial.SurfaceZ {
		return cityRealtimeCharacterInteriorContext{}, false, ErrCityInvalidInput
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT building_code, floor_index, z, layout_version, layout_style, cells,
       content_hash, revision
FROM city_realtime_spatial_building_interiors
WHERE world_id = $1
  AND z = $2
  AND cells @> jsonb_build_array(jsonb_build_object(
      'x', $3::BIGINT,
      'y', $4::BIGINT,
      'z', $2::SMALLINT
  ))
ORDER BY building_code ASC, floor_index ASC`, worldID, position.Z, position.X, position.Y)
	if err != nil {
		return cityRealtimeCharacterInteriorContext{}, false, fmt.Errorf("load realtime character interior coordinate: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var item cityRealtimeCharacterInteriorContext
	found := false
	for rows.Next() {
		var layoutVersion, contentHash string
		var rawCells []byte
		var revision int64
		candidate := cityRealtimeCharacterInteriorContext{}
		if err = rows.Scan(&candidate.BuildingCode, &candidate.FloorIndex, &candidate.Z,
			&layoutVersion, &candidate.LayoutStyle, &rawCells, &contentHash, &revision); err != nil {
			return cityRealtimeCharacterInteriorContext{}, false, err
		}
		if candidate.BuildingCode == "" || candidate.FloorIndex < 0 || candidate.Z != position.Z ||
			revision != 1 || !cityRealtimeSHA256Hex(contentHash) || layoutVersion == "" || candidate.LayoutStyle == "" {
			return cityRealtimeCharacterInteriorContext{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "realtime_character_interior_identity",
			})
		}
		if err = json.Unmarshal(rawCells, &candidate.Cells); err != nil || len(candidate.Cells) == 0 {
			return cityRealtimeCharacterInteriorContext{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "realtime_character_interior_cells",
			}).WithCause(err)
		}
		interior := cityspatial.GeneratedWorldgenBuildingInterior{
			BuildingCode:  candidate.BuildingCode,
			FloorIndex:    candidate.FloorIndex,
			Z:             candidate.Z,
			LayoutVersion: layoutVersion,
			LayoutStyle:   candidate.LayoutStyle,
			Cells:         candidate.Cells,
			ContentHash:   contentHash,
		}
		expectedHash, hashErr := cityspatial.ComputeWorldgenBuildingInteriorHash(&interior)
		if hashErr != nil || expectedHash != contentHash {
			return cityRealtimeCharacterInteriorContext{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "realtime_character_interior_hash",
			}).WithCause(hashErr)
		}
		cellFound := false
		for _, cell := range candidate.Cells {
			if cell.X == position.X && cell.Y == position.Y && cell.Z == position.Z {
				if cellFound {
					return cityRealtimeCharacterInteriorContext{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{
						"field": "realtime_character_interior_duplicate_cell",
					})
				}
				candidate.Cell = cell
				cellFound = true
			}
		}
		if !cellFound {
			return cityRealtimeCharacterInteriorContext{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "realtime_character_interior_coordinate",
			})
		}
		if found {
			return cityRealtimeCharacterInteriorContext{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "realtime_character_interior_overlap",
			})
		}
		item = candidate
		found = true
	}
	if err = rows.Err(); err != nil {
		return cityRealtimeCharacterInteriorContext{}, false, fmt.Errorf("iterate realtime character interior coordinate: %w", err)
	}
	return item, found, nil
}

func cityRealtimeCharacterWalkMotionState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	current cityRealtimeActorState,
	target cityRealtimeActorSpawnCandidate,
) (string, bool, error) {
	if !cityRealtimeCharacterAdjacentStep(current, target) {
		return "", false, nil
	}
	sourceInterior, sourceInside, err := loadCityRealtimeCharacterInteriorAt(ctx, queryer, worldID,
		cityRealtimeActorSpawnCandidate{X: current.X, Y: current.Y, Z: current.Z})
	if err != nil {
		return "", false, err
	}
	targetInterior, targetInside, err := loadCityRealtimeCharacterInteriorAt(ctx, queryer, worldID, target)
	if err != nil {
		return "", false, err
	}
	if sourceInside || targetInside {
		if !sourceInside || !targetInside || sourceInterior.BuildingCode != targetInterior.BuildingCode ||
			sourceInterior.FloorIndex != targetInterior.FloorIndex || !cityRealtimeCharacterInteriorCellTraversable(targetInterior.Cell) {
			return "", false, nil
		}
		return "inside", true, nil
	}
	if target.Z != cityspatial.SurfaceZ {
		return "", false, nil
	}
	traversable, err := cityRealtimeCharacterSurfaceCellTraversable(ctx, queryer, worldID, target)
	if err != nil || !traversable {
		return "", false, err
	}
	return "walking", true, nil
}

func scanCityRealtimeCharacterPortal(rows *sql.Rows) (cityRealtimeCharacterPortalRecord, error) {
	item := cityRealtimeCharacterPortalRecord{}
	err := rows.Scan(
		&item.Code, &item.BuildingCode, &item.PortalType, &item.FromFloorIndex, &item.ToFloorIndex,
		&item.From.X, &item.From.Y, &item.From.Z, &item.To.X, &item.To.Y, &item.To.Z,
		&item.Bidirectional, &item.TopologyHash, &item.Revision,
	)
	if err != nil {
		return cityRealtimeCharacterPortalRecord{}, err
	}
	if err = item.validate(); err != nil {
		return cityRealtimeCharacterPortalRecord{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "realtime_character_portal",
		}).WithCause(err)
	}
	return item, nil
}

func loadCityRealtimeCharacterPortal(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	portalCode string,
) (cityRealtimeCharacterPortalRecord, bool, error) {
	if worldID <= 0 || !cityRealtimeDueEventIdentifierValid(portalCode, 128) {
		return cityRealtimeCharacterPortalRecord{}, false, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "portal_code"})
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT code, building_code, portal_type, from_floor_index, to_floor_index,
       from_x, from_y, from_z, to_x, to_y, to_z, bidirectional,
       topology_hash, revision
FROM city_realtime_spatial_portals
WHERE world_id = $1 AND code = $2`, worldID, portalCode)
	if err != nil {
		return cityRealtimeCharacterPortalRecord{}, false, fmt.Errorf("load realtime character portal: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return cityRealtimeCharacterPortalRecord{}, false, fmt.Errorf("iterate realtime character portal: %w", err)
		}
		return cityRealtimeCharacterPortalRecord{}, false, nil
	}
	item, err := scanCityRealtimeCharacterPortal(rows)
	if err != nil {
		return cityRealtimeCharacterPortalRecord{}, false, err
	}
	if rows.Next() || rows.Err() != nil {
		return cityRealtimeCharacterPortalRecord{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "realtime_character_portal_duplicate",
		})
	}
	return item, true, nil
}

func loadCityRealtimeCharacterPortalsAt(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	position cityRealtimeActorSpawnCandidate,
) ([]cityRealtimeCharacterPortalRecord, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT code, building_code, portal_type, from_floor_index, to_floor_index,
       from_x, from_y, from_z, to_x, to_y, to_z, bidirectional,
       topology_hash, revision
FROM city_realtime_spatial_portals
WHERE world_id = $1
  AND (
      (from_x = $2 AND from_y = $3 AND from_z = $4)
      OR (bidirectional AND to_x = $2 AND to_y = $3 AND to_z = $4)
  )
ORDER BY code ASC`, worldID, position.X, position.Y, position.Z)
	if err != nil {
		return nil, fmt.Errorf("load realtime character portal candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]cityRealtimeCharacterPortalRecord, 0)
	for rows.Next() {
		item, scanErr := scanCityRealtimeCharacterPortal(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character portal candidates: %w", err)
	}
	return items, nil
}

func cityRealtimeCharacterPortalEndpointValid(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	portal cityRealtimeCharacterPortalRecord,
	position cityRealtimeActorSpawnCandidate,
	floorIndex int32,
	inside bool,
) (bool, error) {
	interior, found, err := loadCityRealtimeCharacterInteriorAt(ctx, queryer, worldID, position)
	if err != nil {
		return false, err
	}
	if inside {
		return found && interior.BuildingCode == portal.BuildingCode && interior.FloorIndex == floorIndex &&
			cityRealtimeCharacterInteriorCellTraversable(interior.Cell), nil
	}
	if found || position.Z != cityspatial.SurfaceZ {
		return false, nil
	}
	return cityRealtimeCharacterSurfaceCellTraversable(ctx, queryer, worldID, position)
}

func cityRealtimeCharacterResolvePortalTransition(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	portal cityRealtimeCharacterPortalRecord,
	position cityRealtimeActorSpawnCandidate,
) (cityRealtimeActorSpawnCandidate, string, bool, bool, error) {
	forward := cityRealtimeCharacterPositionEquals(position, portal.From)
	reverse := portal.Bidirectional && cityRealtimeCharacterPositionEquals(position, portal.To)
	if !forward && !reverse {
		return cityRealtimeActorSpawnCandidate{}, "", false, false, nil
	}

	source := portal.From
	target := portal.To
	sourceFloor := portal.FromFloorIndex
	targetFloor := portal.ToFloorIndex
	sourceInside := portal.PortalType == "stairs"
	targetInside := true
	direction := ""
	if portal.PortalType == "entrance" {
		direction = "enter"
		sourceInside = false
	} else {
		direction = "ascend"
	}
	if reverse {
		source, target = target, source
		sourceFloor, targetFloor = targetFloor, sourceFloor
		sourceInside, targetInside = targetInside, sourceInside
		if portal.PortalType == "entrance" {
			direction = "exit"
		} else {
			direction = "descend"
		}
	}
	sourceValid, err := cityRealtimeCharacterPortalEndpointValid(ctx, queryer, worldID, portal, source, sourceFloor, sourceInside)
	if err != nil || !sourceValid {
		return cityRealtimeActorSpawnCandidate{}, "", false, false, err
	}
	targetValid, err := cityRealtimeCharacterPortalEndpointValid(ctx, queryer, worldID, portal, target, targetFloor, targetInside)
	if err != nil || !targetValid {
		return cityRealtimeActorSpawnCandidate{}, "", false, false, err
	}
	return target, direction, targetInside, true, nil
}

func cityRealtimeCharacterAvailablePortals(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	position cityRealtimeActorSpawnCandidate,
) ([]CityRealtimeCharacterPortalTransition, error) {
	items, err := loadCityRealtimeCharacterPortalsAt(ctx, queryer, worldID, position)
	if err != nil {
		return nil, err
	}
	result := make([]CityRealtimeCharacterPortalTransition, 0, len(items))
	for _, portal := range items {
		target, direction, _, allowed, resolveErr := cityRealtimeCharacterResolvePortalTransition(ctx, queryer, worldID, portal, position)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if !allowed {
			continue
		}
		occupied, occupancyErr := cityRealtimeActorPositionOccupied(ctx, queryer, worldID, actorCode, target)
		if occupancyErr != nil {
			return nil, occupancyErr
		}
		if occupied {
			continue
		}
		result = append(result, CityRealtimeCharacterPortalTransition{
			PortalCode: portal.Code, PortalType: portal.PortalType, Direction: direction,
			BuildingCode: portal.BuildingCode,
			Target:       CityRealtimeCharacterLocation{X: target.X, Y: target.Y, Z: target.Z},
		})
	}
	return result, nil
}

func advanceCityRealtimeCharacterPosition(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	record *cityRealtimeCharacterRecord,
	target cityRealtimeActorSpawnCandidate,
	eventKind, motionState, portalCode string,
) error {
	if tx == nil || record == nil || worldID <= 0 || frameSequence <= 0 ||
		(eventKind != "move" && eventKind != "portal") ||
		(motionState != "walking" && motionState != "inside") ||
		(eventKind == "portal" && !cityRealtimeDueEventIdentifierValid(portalCode, 128)) ||
		(eventKind != "portal" && portalCode != "") {
		return ErrCityInvalidInput
	}
	previousEventHash := record.state.EventChainHash
	nextRevision := record.state.PositionRevision + 1
	eventHash, err := cityRealtimeActorPositionEventHash(cityRealtimeActorPositionEventInput{
		ActorCode: record.identity.ActorCode, EventSequence: nextRevision - 1, FrameSequence: frameSequence,
		EventKind: eventKind, PortalCode: portalCode,
		From: &cityRealtimeActorSpawnCandidate{X: record.state.X, Y: record.state.Y, Z: record.state.Z},
		To:   target, MotionState: motionState, PreviousEventHash: &previousEventHash,
	})
	if err != nil {
		return err
	}
	nextState := cityRealtimeActorState{
		ActorCode: record.state.ActorCode, X: target.X, Y: target.Y, Z: target.Z,
		MotionState: motionState, PositionRevision: nextRevision, LastFrameSequence: frameSequence,
		EventChainHash: eventHash,
	}
	nextState.StateHash, err = cityRealtimeActorStateHash(nextState)
	if err != nil {
		return err
	}
	if err = enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, false); err != nil {
		return err
	}
	update, err := tx.ExecContext(ctx, `
UPDATE city_realtime_actor_states
SET x = $3, y = $4, z = $5, motion_state = $6,
    position_revision = $7, last_frame_sequence = $8,
    state_hash = $9, event_chain_hash = $10, updated_at = NOW()
WHERE world_id = $1 AND actor_code = $2
  AND position_revision = $11 AND last_frame_sequence = $12`,
		worldID, record.identity.ActorCode, nextState.X, nextState.Y, nextState.Z,
		nextState.MotionState, nextState.PositionRevision, nextState.LastFrameSequence,
		nextState.StateHash, nextState.EventChainHash,
		record.state.PositionRevision, record.state.LastFrameSequence,
	)
	if err != nil {
		return fmt.Errorf("advance realtime player character state: %w", err)
	}
	if rows, rowsErr := update.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime player character state advance: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_position_revision"})
	}
	var portalCodeValue any
	if portalCode != "" {
		portalCodeValue = portalCode
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_actor_position_events
    (world_id, actor_code, event_sequence, frame_sequence, event_kind, portal_code,
     from_x, from_y, from_z, to_x, to_y, to_z, motion_state,
     public_visibility, previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        TRUE, $14, $15, '{}'::jsonb)`,
		worldID, record.identity.ActorCode, nextRevision-1, frameSequence, eventKind, portalCodeValue,
		record.state.X, record.state.Y, record.state.Z, target.X, target.Y, target.Z, motionState,
		previousEventHash, eventHash,
	); err != nil {
		return fmt.Errorf("append realtime player character %s event: %w", eventKind, err)
	}
	record.state = nextState
	return nil
}

// TraverseRealtimeCharacterPortal moves only the caller's character through
// an immutable adjacent portal edge. It is intentionally distinct from normal
// cell walking so entrances and vertical stairs remain auditable topology
// transitions instead of browser-selected teleports.
func (s *CityEconomyService) TraverseRealtimeCharacterPortal(
	ctx context.Context,
	input CityRealtimeCharacterPortalTraverseInput,
) (*CityRealtimeCharacterMutationResult, error) {
	portalCode := strings.TrimSpace(input.PortalCode)
	idempotencyKey, err := normalizeCityRealtimeCharacterIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if input.UserID <= 0 || input.WorldID <= 0 || !cityRealtimeDueEventIdentifierValid(portalCode, 128) {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "portal_code"})
	}
	requestHash, err := cityRealtimeCharacterRequestHash(cityRealtimeCharacterActionPortal, map[string]any{
		"world_id": input.WorldID,
		"user_id":  input.UserID,
		"portal":   portalCode,
	})
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime character portal transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockCityRealtimeCharacterWorld(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	world, err := lockCityWorld(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	state, err := lockCityRealtimeState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if err = validateCityRealtimeCharacterCommandWindow(world, state); err != nil {
		return nil, err
	}
	if receipt, found, receiptErr := loadCityRealtimeCharacterActionReceipt(
		ctx, tx, input.WorldID, input.UserID, idempotencyKey,
	); receiptErr != nil {
		return nil, receiptErr
	} else if found {
		return completeCityRealtimeCharacterReceipt(tx, receipt, cityRealtimeCharacterActionPortal, requestHash)
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	lifeRuntime, err := loadCityRealtimeCharacterLifeRuntime(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if agentState == nil || agentState.Binding == nil || lifeRuntime == nil {
		return nil, ErrCityRealtimeCharacterRuntimeUnavailable
	}
	record, found, err := loadCityRealtimeOwnedCharacter(ctx, tx, input.WorldID, input.UserID, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterNotFound
	}
	if record.agent.LifecycleStatus != "active" || record.identity.LifecycleStatus != "active" || record.agent.ControlMode != "manual" {
		return nil, ErrCityRealtimeCharacterControlUnavailable
	}
	profile, profileFound, profileErr := loadCityRealtimeCharacterProfile(ctx, tx, input.WorldID, record.identity.ActorCode, false)
	if profileErr != nil {
		return nil, profileErr
	}
	if !profileFound || !cityRealtimeCharacterProfileMatchesRuntime(profile, lifeRuntime) {
		return nil, ErrCityRealtimeCharacterRuntimeUnavailable
	}
	portal, portalFound, portalErr := loadCityRealtimeCharacterPortal(ctx, tx, input.WorldID, portalCode)
	if portalErr != nil {
		return nil, portalErr
	}
	if !portalFound {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "portal_code"})
	}
	target, direction, targetInside, allowed, transitionErr := cityRealtimeCharacterResolvePortalTransition(
		ctx, tx, input.WorldID, portal,
		cityRealtimeActorSpawnCandidate{X: record.state.X, Y: record.state.Y, Z: record.state.Z},
	)
	if transitionErr != nil {
		return nil, transitionErr
	}
	if !allowed {
		return nil, ErrCityRealtimeCharacterControlUnavailable.WithMetadata(map[string]string{"field": "portal"})
	}
	occupied, err := cityRealtimeActorPositionOccupied(ctx, tx, input.WorldID, record.identity.ActorCode, target)
	if err != nil {
		return nil, err
	}
	if occupied {
		return nil, ErrCityRealtimeCharacterControlUnavailable.WithMetadata(map[string]string{"field": "destination_occupied"})
	}
	if err = cityRealtimeRejectPendingDueAtCurrentTime(ctx, tx, input.WorldID, state.currentWorldTimeUS); err != nil {
		return nil, err
	}
	frameSequence, cursor, err := cityRealtimeCharacterNextFrame(state)
	if err != nil {
		return nil, err
	}
	motionState := "walking"
	if targetInside {
		motionState = "inside"
	}
	if err = advanceCityRealtimeCharacterPosition(ctx, tx, input.WorldID, frameSequence, &record, target, "portal", motionState, portal.Code); err != nil {
		return nil, err
	}
	life, lifeErr := cityRealtimeCharacterLifeProjection(profile, lifeRuntime)
	if lifeErr != nil {
		return nil, lifeErr
	}
	result := &CityRealtimeCharacterMutationResult{
		Character: record.projection(),
		Life:      cityRealtimeCharacterLifePointer(life),
	}
	if result.Frame, err = sealCityRealtimeCharacterFrame(
		ctx, tx, input.WorldID, world, state, frameSequence, cursor, cityRealtimeCharacterActionPortal,
		map[string]any{
			"character_created": 0,
			"character_moved":   0,
			"character_portal":  1,
			"portal_direction":  direction,
		},
	); err != nil {
		return nil, err
	}
	if err = canonicalizeCityRealtimeCharacterMutationResult(result); err != nil {
		return nil, err
	}
	if err = storeCityRealtimeCharacterActionReceipt(
		ctx, tx, input.WorldID, input.UserID, idempotencyKey, record.identity.ActorCode,
		cityRealtimeCharacterActionPortal, requestHash, frameSequence, *result,
	); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character portal traversal: %w", err)
	}
	return result, nil
}
