package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
)

const (
	cityOpenWorldRuntimeRejectionActorLimit                = "OPEN_WORLD_ACTOR_LIMIT_REACHED"
	cityOpenWorldRuntimeRejectionActorNotFound             = "OPEN_WORLD_ACTOR_NOT_FOUND"
	cityOpenWorldRuntimeRejectionDefinition                = "OPEN_WORLD_RUNTIME_DEFINITION_UNAVAILABLE"
	cityOpenWorldRuntimeRejectionRequirement               = "OPEN_WORLD_REQUIREMENT_NOT_SATISFIED"
	cityOpenWorldRuntimeRejectionActivityLimit             = "OPEN_WORLD_ACTIVITY_TICK_LIMIT_REACHED"
	cityOpenWorldRuntimeRejectionRoleActive                = "OPEN_WORLD_ROLE_ALREADY_ACTIVE"
	cityOpenWorldRuntimeRejectionRoleCooldown              = "OPEN_WORLD_ROLE_TRANSITION_COOLDOWN"
	cityOpenWorldRuntimeRejectionCellBlocked               = "OPEN_WORLD_CELL_BLOCKED"
	cityOpenWorldRuntimeRejectionCellOccupied              = "OPEN_WORLD_CELL_OCCUPIED"
	cityOpenWorldRuntimeRejectionLocationInvalid           = "OPEN_WORLD_LOCATION_INVALID"
	cityOpenWorldRuntimeRejectionSectorUnavailable         = "OPEN_WORLD_SECTOR_UNAVAILABLE"
	cityOpenWorldRuntimeRejectionPortalNotFound            = "OPEN_WORLD_PORTAL_NOT_FOUND"
	cityOpenWorldRuntimeRejectionPortalOutOfReach          = "OPEN_WORLD_PORTAL_OUT_OF_REACH"
	cityOpenWorldRuntimeRejectionPortalState               = "OPEN_WORLD_PORTAL_STATE_INVALID"
	cityOpenWorldRuntimeRejectionPortalAccess              = "OPEN_WORLD_PORTAL_ACCESS_DENIED"
	cityOpenWorldRuntimeRejectionControlDenied             = "OPEN_WORLD_ACTOR_CONTROL_DENIED"
	cityOpenWorldRuntimeRejectionControlMissing            = "OPEN_WORLD_CONTROL_GRANT_NOT_FOUND"
	cityOpenWorldRuntimeRejectionSpawnUnavailable          = "OPEN_WORLD_SPAWN_UNAVAILABLE"
	cityOpenWorldRuntimeRejectionCommuteAssignmentNotFound = "OPEN_WORLD_COMMUTE_ASSIGNMENT_NOT_FOUND"
	cityOpenWorldRuntimeRejectionCommuteStateUnchanged     = "OPEN_WORLD_COMMUTE_ASSIGNMENT_STATE_UNCHANGED"
	cityOpenWorldRuntimeRejectionCommuteNotOperational     = "OPEN_WORLD_COMMUTE_ASSIGNMENT_NOT_OPERATIONAL"
	cityOpenWorldRuntimeRejectionCommuteFacility           = "OPEN_WORLD_COMMUTE_FACILITY_UNAVAILABLE"
	cityOpenWorldRuntimeRejectionCommuteCapacity           = "OPEN_WORLD_COMMUTE_FACILITY_CAPACITY_REACHED"
	cityOpenWorldRuntimeRejectionCommuteAssignmentLimit    = "OPEN_WORLD_COMMUTE_ASSIGNMENT_LIMIT_REACHED"
	cityOpenWorldRuntimeRejectionCommuteTransitionLimit    = "OPEN_WORLD_COMMUTE_TRANSITION_LIMIT_REACHED"
)

type cityOpenWorldRuntimeBusinessError struct{ code string }

func (err *cityOpenWorldRuntimeBusinessError) Error() string { return err.code }

func cityOpenWorldRuntimeReject(code string) error {
	return &cityOpenWorldRuntimeBusinessError{code: code}
}

func cityOpenWorldRuntimeBusinessRejectionCode(err error) string {
	var businessErr *cityOpenWorldRuntimeBusinessError
	if errors.As(err, &businessErr) {
		return businessErr.code
	}
	if errors.Is(err, ErrCityOpenWorldRuntimeDefinitionNotFound) {
		return cityOpenWorldRuntimeRejectionDefinition
	}
	return ""
}

type cityOpenWorldRuntimeActorRef struct {
	id       int64
	actor    CityOpenWorldActor
	location CityOpenWorldActorLocation
}

type cityOpenWorldRuntimeFactRecord struct {
	id   int64
	fact CityOpenWorldRuntimeFact
}

type cityOpenWorldRuntimeExecution struct {
	pending       cityPendingEvent
	facts         []CityOpenWorldRuntimeFact
	effects       []CityOpenWorldRuntimeEffect
	cases         []CityOpenWorldRuleCase
	nextFactSeq   int64
	nextEffectSeq int64
	nextCaseSeq   int64
}

func (s *CityEconomyService) applyCityOpenWorldRuntimeCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
) (cityOpenWorldRuntimeExecution, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT city_open_world_runtime_command`); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("create open-world runtime command savepoint: %w", err)
	}
	execution, err := s.postCityOpenWorldRuntimeCommand(
		ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command,
	)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_open_world_runtime_command`); rollbackErr != nil {
			return cityOpenWorldRuntimeExecution{}, fmt.Errorf("rollback open-world runtime command after %v: %w", err, rollbackErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT city_open_world_runtime_command`); releaseErr != nil {
			return cityOpenWorldRuntimeExecution{}, fmt.Errorf("release rejected open-world runtime command: %w", releaseErr)
		}
		if code := cityOpenWorldRuntimeBusinessRejectionCode(err); code != "" {
			return cityOpenWorldRuntimeExecution{
				pending:     rejectedCityCommand(command, code),
				nextFactSeq: factSequence, nextEffectSeq: effectSequence, nextCaseSeq: caseSequence,
			}, nil
		}
		return cityOpenWorldRuntimeExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_open_world_runtime_command`); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("release open-world runtime command savepoint: %w", err)
	}
	return execution, nil
}

func (s *CityEconomyService) postCityOpenWorldRuntimeCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
) (cityOpenWorldRuntimeExecution, error) {
	if command == nil || command.ID <= 0 || command.UserID <= 0 {
		return cityOpenWorldRuntimeExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_runtime_command"})
	}
	switch command.CommandType {
	case CityCommandTypeOpenWorldActorCreate:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldActorCreatePayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.createCityOpenWorldActor(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldActorMove:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldActorMovePayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.moveCityOpenWorldActor(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldActorPortalUse:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldActorPortalUsePayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.useCityOpenWorldActorPortal(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldActorActivityPerform:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldActorActivityPayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.performCityOpenWorldActorActivity(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldActorRoleTransition:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldActorRoleTransitionPayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.transitionCityOpenWorldActorRole(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldActorControlGrant, CityCommandTypeOpenWorldActorControlRevoke:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldActorControlPayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.changeCityOpenWorldActorControl(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence,
			command, payload, command.CommandType == CityCommandTypeOpenWorldActorControlGrant)
	case CityCommandTypeOpenWorldPortalStateSet:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldPortalStatePayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.changeCityOpenWorldPortalState(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldPortalAccessSet:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldPortalAccessPayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.changeCityOpenWorldPortalAccess(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldActorNavigationSet:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldActorNavigationSetPayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.setCityOpenWorldV5NavigationIntent(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldActorNavigationCancel:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldActorNavigationCancelPayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.cancelCityOpenWorldV5NavigationIntent(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldActorServiceRequest:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldActorServiceRequestPayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.requestCityOpenWorldActorService(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldActorMobilityRequest:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldActorMobilityRequestPayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.requestCityOpenWorldActorMobility(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldCommuteAssignmentSetState:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldCommuteAssignmentSetStatePayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.setCityOpenWorldCommuteAssignmentState(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldCommuteAssignmentRebind:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldCommuteAssignmentRebindPayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.rebindCityOpenWorldCommuteAssignment(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeOpenWorldInfrastructureAssetTransition:
		payload, err := decodeStoredCityCommandPayload[cityOpenWorldInfrastructureAssetTransitionPayload](command)
		if err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
		return s.transitionCityOpenWorldInfrastructureAsset(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	default:
		return cityOpenWorldRuntimeExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"command_type": command.CommandType})
	}
}

func (s *CityEconomyService) createCityOpenWorldActor(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldActorCreatePayload,
) (cityOpenWorldRuntimeExecution, error) {
	profile, err := loadCityOpenWorldRuntimeProfileForUpdate(ctx, tx, worldID)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	var currentCount int
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_open_world_actors
WHERE world_id = $1 AND owner_user_id = $2 AND status = 'active'`, worldID, command.UserID).Scan(&currentCount); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("count player open-world actors: %w", err)
	}
	if currentCount >= profile.MaximumPlayerActorsPerMember {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionActorLimit)
	}
	archetypeDefinition, err := loadCityOpenWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionArchetype, payload.ArchetypeCode)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	archetype, err := decodeWorldRuntimeDefinition[worldRuntimeArchetypeDefinition](archetypeDefinition)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, ErrCitySimulationInvariant.WithCause(err)
	}
	if archetype.ActorTypeCode != "character" {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionDefinition)
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	actorCode := "actor." + strconv.FormatInt(command.Sequence, 10)
	var actorID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_actors
    (world_id, code, owner_user_id, actor_type_code, name, status,
     archetype_code, archetype_version, created_tick, updated_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, 'active', $6, $7, $8, $8, 1, '{"origin":"player"}'::jsonb)
RETURNING id`, worldID, actorCode, command.UserID, archetype.ActorTypeCode, payload.Name,
		archetypeDefinition.Code, archetypeDefinition.Version, targetTick).Scan(&actorID); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("insert open-world actor: %w", err)
	}
	rootPayload, err := json.Marshal(map[string]any{
		"actor_code": actorCode, "archetype_code": archetypeDefinition.Code, "name": payload.Name,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal actor creation fact: %w", err)
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &actorID, factType: CityOpenWorldRuntimeFactActorCreated,
		definition: archetypeDefinition, payload: rootPayload,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	attributeCodes := make([]string, 0, len(archetype.InitialAttributes))
	for code := range archetype.InitialAttributes {
		attributeCodes = append(attributeCodes, code)
	}
	sort.Strings(attributeCodes)
	for _, code := range attributeCodes {
		definition, definitionErr := loadCityOpenWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionAttribute, code)
		if definitionErr != nil {
			return cityOpenWorldRuntimeExecution{}, definitionErr
		}
		attribute, decodeErr := decodeWorldRuntimeDefinition[worldRuntimeAttributeDefinition](definition)
		if decodeErr != nil || archetype.InitialAttributes[code] < attribute.MinimumUnits || archetype.InitialAttributes[code] > attribute.MaximumUnits {
			return cityOpenWorldRuntimeExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_archetype_attribute"}).WithCause(decodeErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_attributes
    (world_id, actor_id, attribute_code, value_units, experience_units, last_changed_tick, version, metadata)
VALUES ($1, $2, $3, $4, 0, $5, 1, '{}'::jsonb)`,
			worldID, actorID, code, archetype.InitialAttributes[code], targetTick); err != nil {
			return cityOpenWorldRuntimeExecution{}, fmt.Errorf("insert open-world actor attribute: %w", err)
		}
	}
	roleCodes := append([]string(nil), archetype.InitialRoles...)
	sort.Strings(roleCodes)
	for _, code := range roleCodes {
		definition, definitionErr := loadCityOpenWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionRole, code)
		if definitionErr != nil {
			return cityOpenWorldRuntimeExecution{}, definitionErr
		}
		role, decodeErr := decodeWorldRuntimeDefinition[worldRuntimeRoleDefinition](definition)
		if decodeErr != nil {
			return cityOpenWorldRuntimeExecution{}, ErrCitySimulationInvariant.WithCause(decodeErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_roles
    (world_id, actor_id, role_code, category_code, status, granted_tick, version, metadata)
VALUES ($1, $2, $3, $4, 'active', $5, 1, '{}'::jsonb)`, worldID, actorID, code, role.CategoryCode, targetTick); err != nil {
			return cityOpenWorldRuntimeExecution{}, fmt.Errorf("insert open-world actor role: %w", err)
		}
	}
	spawn, err := findCityOpenWorldRuntimeSpawnLocation(ctx, tx, worldID, actorID, actorCode)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = insertCityOpenWorldActorLocation(ctx, tx, worldID, actorID, targetTick, root.id, spawn); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	controlCode := "control." + strconv.FormatInt(actorID, 10) + "." + strconv.FormatInt(command.UserID, 10) + ".owner"
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_controls
    (world_id, code, actor_id, user_id, capability, status, granted_by_user_id,
     granted_tick, grant_source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, 'active', $4, $6, $7, 1, '{}'::jsonb),
       ($1, $8, $3, $4, $9, 'active', $4, $6, $7, 1, '{}'::jsonb)`,
		worldID, controlCode+".command", actorID, command.UserID, WorldActorCapabilityCommand,
		targetTick, root.id, controlCode, WorldActorCapabilityManageControl); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("insert open-world actor owner controls: %w", err)
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 1, 1, 0, 0); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.actor.created", map[string]any{
			"actor_code": actorCode, "archetype_code": archetypeDefinition.Code, "location": spawn,
		}),
		facts: []CityOpenWorldRuntimeFact{root.fact}, effects: []CityOpenWorldRuntimeEffect{}, cases: []CityOpenWorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence, nextCaseSeq: caseSequence,
	}, nil
}

func (s *CityEconomyService) moveCityOpenWorldActor(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldActorMovePayload,
) (cityOpenWorldRuntimeExecution, error) {
	actor, err := loadControlledCityOpenWorldActor(ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityCommand)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	target, err := cityOpenWorldRuntimeLocationFromPayload(payload)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if !cityOpenWorldRuntimeIsAdjacentMove(actor.location, target) {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionLocationInvalid)
	}
	if err = cityOpenWorldRuntimeValidatePassableLocation(ctx, tx, worldID, target); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	occupied, err := cityOpenWorldRuntimeLocationOccupied(ctx, tx, worldID, actor.id, target)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if occupied {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCellOccupied)
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	payloadRaw, err := json.Marshal(map[string]any{"from": actor.location, "to": target})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal open-world move fact: %w", err)
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &actor.id, factType: CityOpenWorldRuntimeFactLocationMoved, payload: payloadRaw,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldActorLocation(ctx, tx, worldID, actor.id, targetTick, root.id, target); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = touchCityOpenWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	effect, err := insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
		worldID: worldID, tick: targetTick, sequence: effectSequence, sourceFact: root,
		operationIndex: 1, effectType: WorldRuntimeEffectLocationSet, targetActorID: &actor.id,
		targetActorCode: &actor.actor.Code, targetKey: cityOpenWorldStringPointer("location"), payload: payloadRaw,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 1, 0); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.actor.moved", map[string]any{"actor_code": actor.actor.Code, "to": target}),
		facts:   []CityOpenWorldRuntimeFact{root.fact}, effects: []CityOpenWorldRuntimeEffect{effect}, cases: []CityOpenWorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence + 1, nextCaseSeq: caseSequence,
	}, nil
}

func (s *CityEconomyService) useCityOpenWorldActorPortal(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldActorPortalUsePayload,
) (cityOpenWorldRuntimeExecution, error) {
	actor, err := loadControlledCityOpenWorldActor(ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityCommand)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	portal, err := loadCityOpenWorldStaticPortal(ctx, tx, worldID, payload.PortalCode)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	from, to, err := cityOpenWorldRuntimePortalEndpoints(actor.actor.Code, portal)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	target := CityOpenWorldActorLocation{}
	if cityOpenWorldRuntimeLocationEqual(actor.location, from) {
		target = to
	} else if portal.Bidirectional && cityOpenWorldRuntimeLocationEqual(actor.location, to) {
		target = from
	} else {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionPortalOutOfReach)
	}
	state, err := loadCityOpenWorldRuntimePortalStateForUse(ctx, tx, worldID, portal)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if state.StateCode != WorldPortalStateOpen {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionPortalState)
	}
	evaluation, err := evaluateCityOpenWorldRuntimeRequirement(ctx, tx, worldID, actor.id, targetTick, state.AccessRequirement)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if !evaluation.Satisfied {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionPortalAccess)
	}
	if err = cityOpenWorldRuntimeValidatePassableLocation(ctx, tx, worldID, target); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	occupied, err := cityOpenWorldRuntimeLocationOccupied(ctx, tx, worldID, actor.id, target)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if occupied {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCellOccupied)
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	raw, err := json.Marshal(map[string]any{"portal_code": portal.Code, "from": actor.location, "to": target})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal portal traversal fact: %w", err)
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &actor.id, factType: CityOpenWorldRuntimeFactPortalTraversed, payload: raw,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldActorLocation(ctx, tx, worldID, actor.id, targetTick, root.id, target); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = touchCityOpenWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	effect, err := insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
		worldID: worldID, tick: targetTick, sequence: effectSequence, sourceFact: root,
		operationIndex: 1, effectType: WorldRuntimeEffectLocationSet, targetActorID: &actor.id,
		targetActorCode: &actor.actor.Code, targetKey: cityOpenWorldStringPointer(portal.Code), payload: raw,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 1, 0); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.actor.portal_traversed", map[string]any{"actor_code": actor.actor.Code, "portal_code": portal.Code, "to": target}),
		facts:   []CityOpenWorldRuntimeFact{root.fact}, effects: []CityOpenWorldRuntimeEffect{effect}, cases: []CityOpenWorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence + 1, nextCaseSeq: caseSequence,
	}, nil
}

type cityOpenWorldRuntimeFactInsert struct {
	worldID         int64
	tick            int64
	sequence        int64
	sourceCommandID *int64
	parentFactID    *int64
	actorID         *int64
	factType        string
	definition      *CityOpenWorldRuntimeDefinition
	payload         json.RawMessage
}

func insertCityOpenWorldRuntimeFact(
	ctx context.Context,
	tx *sql.Tx,
	input cityOpenWorldRuntimeFactInsert,
) (*cityOpenWorldRuntimeFactRecord, error) {
	var definitionKind, definitionCode, definitionVersion, definitionHash any
	if input.definition != nil {
		definitionKind, definitionCode = input.definition.Kind, input.definition.Code
		definitionVersion, definitionHash = input.definition.Version, input.definition.Hash
	}
	record := &cityOpenWorldRuntimeFactRecord{}
	if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_runtime_facts
    (world_id, tick, sequence, source_command_id, parent_fact_id, actor_id,
     fact_type, definition_kind, definition_code, definition_version, definition_hash, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)
RETURNING id`, input.worldID, input.tick, input.sequence, cityOpenWorldNullableInt64(input.sourceCommandID),
		cityOpenWorldNullableInt64(input.parentFactID), cityOpenWorldNullableInt64(input.actorID), input.factType,
		definitionKind, definitionCode, definitionVersion, definitionHash, []byte(input.payload)).Scan(&record.id); err != nil {
		return nil, fmt.Errorf("insert open-world runtime fact %s: %w", input.factType, err)
	}
	record.fact = CityOpenWorldRuntimeFact{Tick: input.tick, Sequence: input.sequence, FactType: input.factType, Payload: input.payload}
	if input.sourceCommandID != nil {
		var commandSequence int64
		if err := tx.QueryRowContext(ctx, `SELECT sequence FROM city_commands WHERE id = $1 AND world_id = $2`, *input.sourceCommandID, input.worldID).Scan(&commandSequence); err != nil {
			return nil, fmt.Errorf("load open-world runtime fact source command: %w", err)
		}
		record.fact.SourceCommandSequence = &commandSequence
	}
	if input.parentFactID != nil {
		var parent CityOpenWorldRuntimeFactRef
		if err := tx.QueryRowContext(ctx, `
SELECT tick, sequence FROM city_open_world_runtime_facts WHERE id = $1 AND world_id = $2`, *input.parentFactID, input.worldID).Scan(&parent.Tick, &parent.Sequence); err != nil {
			return nil, fmt.Errorf("load open-world runtime parent fact: %w", err)
		}
		record.fact.Parent = &parent
	}
	if input.actorID != nil {
		var code string
		if err := tx.QueryRowContext(ctx, `
SELECT code FROM city_open_world_actors WHERE id = $1 AND world_id = $2`, *input.actorID, input.worldID).Scan(&code); err != nil {
			return nil, fmt.Errorf("load open-world runtime fact actor: %w", err)
		}
		record.fact.ActorCode = cityOpenWorldStringPointer(code)
	}
	if input.definition != nil {
		record.fact.DefinitionKind = cityOpenWorldStringPointer(input.definition.Kind)
		record.fact.DefinitionCode = cityOpenWorldStringPointer(input.definition.Code)
		record.fact.DefinitionVersion = cityOpenWorldStringPointer(input.definition.Version)
		record.fact.DefinitionHash = cityOpenWorldStringPointer(input.definition.Hash)
	}
	return record, nil
}

func postCityOpenWorldRuntimeFact(ctx context.Context, tx *sql.Tx, factID int64) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_runtime_facts SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, factID)
	if err != nil {
		return fmt.Errorf("post open-world runtime fact: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_runtime_fact_post"})
	}
	return nil
}

func updateCityOpenWorldRuntimeProfile(
	ctx context.Context,
	tx *sql.Tx,
	worldID, actorDelta, factDelta, effectDelta, caseDelta int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_runtime_profiles
SET actor_count = actor_count + $2,
    fact_count = fact_count + $3,
    effect_count = effect_count + $4,
    case_count = case_count + $5,
    revision = revision + $3,
    updated_at = NOW()
WHERE world_id = $1`, worldID, actorDelta, factDelta, effectDelta, caseDelta)
	if err != nil {
		return fmt.Errorf("update open-world runtime profile: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_runtime_profile"})
	}
	return nil
}

func loadCityOpenWorldRuntimeProfileForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldRuntimeProfile, error) {
	profile := &CityOpenWorldRuntimeProfile{}
	err := tx.QueryRowContext(ctx, `
SELECT runtime_id, runtime_version, catalog_version, catalog_hash, baseline_tick,
       maximum_player_actors_per_member, actor_count, fact_count, effect_count,
       case_count, revision, metadata
FROM city_open_world_runtime_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&profile.RuntimeID, &profile.RuntimeVersion, &profile.CatalogVersion, &profile.CatalogHash,
		&profile.BaselineTick, &profile.MaximumPlayerActorsPerMember, &profile.ActorCount,
		&profile.FactCount, &profile.EffectCount, &profile.CaseCount, &profile.Revision, &profile.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityOpenWorldRuntimeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock open-world runtime profile: %w", err)
	}
	return profile, nil
}

func loadControlledCityOpenWorldActor(
	ctx context.Context,
	tx *sql.Tx,
	worldID, userID int64,
	actorCode, capability string,
) (*cityOpenWorldRuntimeActorRef, error) {
	item := &cityOpenWorldRuntimeActorRef{}
	var ownerID sql.NullInt64
	var archetypeCode, archetypeVersion sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT actor.id, actor.code, actor.owner_user_id, actor.actor_type_code, actor.name, actor.status,
       actor.archetype_code, actor.archetype_version, actor.created_tick, actor.updated_tick,
       actor.version, actor.metadata
FROM city_open_world_actors actor
WHERE actor.world_id = $1 AND actor.code = $2 AND actor.status = 'active'
  AND (actor.owner_user_id = $3 OR EXISTS (
      SELECT 1
      FROM city_open_world_actor_controls grant_value
      JOIN city_members member
        ON member.world_id = grant_value.world_id AND member.user_id = grant_value.user_id
       AND member.status = 'active'
      WHERE grant_value.world_id = actor.world_id
        AND grant_value.actor_id = actor.id
        AND grant_value.user_id = $3
        AND grant_value.capability = $4
        AND grant_value.status = 'active'
  ))
FOR UPDATE`, worldID, actorCode, userID, capability).Scan(
		&item.id, &item.actor.Code, &ownerID, &item.actor.ActorTypeCode, &item.actor.Name, &item.actor.Status,
		&archetypeCode, &archetypeVersion, &item.actor.CreatedTick, &item.actor.UpdatedTick,
		&item.actor.Version, &item.actor.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionActorNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load controlled open-world actor: %w", err)
	}
	item.actor.OwnerUserID = nullInt64Pointer(ownerID)
	if archetypeCode.Valid {
		item.actor.ArchetypeCode = cityOpenWorldStringPointer(archetypeCode.String)
		item.actor.ArchetypeVersion = cityOpenWorldStringPointer(archetypeVersion.String)
	}
	location, err := loadCityOpenWorldActorLocationByCode(ctx, tx, worldID, actorCode)
	if err != nil {
		return nil, err
	}
	if location == nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_actor_location"})
	}
	item.location = *location
	item.actor.Location = location
	return item, nil
}

func touchCityOpenWorldActor(ctx context.Context, tx *sql.Tx, worldID, actorID, targetTick int64) error {
	if _, err := tx.ExecContext(ctx, `
UPDATE city_open_world_actors
SET updated_tick = $3, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2`, worldID, actorID, targetTick); err != nil {
		return fmt.Errorf("touch open-world actor: %w", err)
	}
	return nil
}

type cityOpenWorldRuntimeEffectInsert struct {
	worldID         int64
	tick            int64
	sequence        int64
	sourceFact      *cityOpenWorldRuntimeFactRecord
	operationIndex  int
	effectType      string
	targetActorID   *int64
	targetActorCode *string
	targetKey       *string
	beforeUnits     *int64
	deltaUnits      *int64
	afterUnits      *int64
	payload         json.RawMessage
}

func insertCityOpenWorldRuntimeEffect(
	ctx context.Context,
	tx *sql.Tx,
	input cityOpenWorldRuntimeEffectInsert,
) (CityOpenWorldRuntimeEffect, error) {
	if input.sourceFact == nil || input.operationIndex <= 0 {
		return CityOpenWorldRuntimeEffect{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_runtime_effect"})
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_runtime_effects
    (world_id, tick, sequence, source_fact_id, operation_index, effect_type,
     target_actor_id, target_key, before_units, delta_units, after_units, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
		input.worldID, input.tick, input.sequence, input.sourceFact.id, input.operationIndex, input.effectType,
		cityOpenWorldNullableInt64(input.targetActorID), cityOpenWorldNullableString(input.targetKey),
		cityOpenWorldNullableInt64(input.beforeUnits), cityOpenWorldNullableInt64(input.deltaUnits),
		cityOpenWorldNullableInt64(input.afterUnits), []byte(input.payload)); err != nil {
		return CityOpenWorldRuntimeEffect{}, fmt.Errorf("insert open-world runtime effect: %w", err)
	}
	return CityOpenWorldRuntimeEffect{
		Tick: input.tick, Sequence: input.sequence, SourceFact: CityOpenWorldRuntimeFactRef{
			Tick: input.sourceFact.fact.Tick, Sequence: input.sourceFact.fact.Sequence,
		}, OperationIndex: input.operationIndex, EffectType: input.effectType,
		ExecutorVersion: cityOpenWorldRuntimeExecutorVersion, TargetActorCode: input.targetActorCode,
		TargetKey: input.targetKey, BeforeUnits: input.beforeUnits, DeltaUnits: input.deltaUnits,
		AfterUnits: input.afterUnits, Payload: input.payload,
	}, nil
}

func cityOpenWorldRuntimePending(command *CityCommand, eventType string, payload map[string]any) cityPendingEvent {
	result := map[string]any{"applied": true}
	for key, value := range payload {
		result[key] = value
	}
	return cityPendingEvent{command: command, status: CityCommandStatusApplied, eventType: eventType, payload: payload, result: result}
}

func cityOpenWorldNullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func cityOpenWorldRuntimeSaturatingAdd(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	if right < 0 && left < math.MinInt64-right {
		return math.MinInt64
	}
	return left + right
}
