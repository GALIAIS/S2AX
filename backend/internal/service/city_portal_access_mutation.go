package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

func worldPortalTargetKey(buildingCode, portalCode string) string {
	return buildingCode + "." + portalCode
}

func worldPortalWithinInteractionRange(
	location WorldActorLocation,
	from, to CityNavigationCoordinate,
) bool {
	actor := CityNavigationCoordinate{X: location.X, Y: location.Y, Z: location.Z}
	for _, endpoint := range []CityNavigationCoordinate{from, to} {
		if actor == endpoint {
			return true
		}
		if actor.Z == endpoint.Z && absoluteInt64(actor.X-endpoint.X) <= 1 &&
			absoluteInt64(actor.Y-endpoint.Y) <= 1 {
			return true
		}
	}
	return false
}

func nextWorldPortalState(current, action string) (string, bool) {
	switch action {
	case WorldPortalActionOpen:
		return WorldPortalStateOpen, current == WorldPortalStateClosed
	case WorldPortalActionClose:
		return WorldPortalStateClosed, current == WorldPortalStateOpen
	case WorldPortalActionLock:
		return WorldPortalStateLocked, current == WorldPortalStateClosed
	case WorldPortalActionUnlock:
		return WorldPortalStateClosed, current == WorldPortalStateLocked
	default:
		return "", false
	}
}

func (s *CityEconomyService) transitionWorldPortalState(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload worldPortalStateTransitionPayload,
) (worldRuntimeExecution, error) {
	actor, err := loadWorldActorWithCapability(
		ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityCommand,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	location, err := loadWorldActorLocationForUpdate(ctx, tx, worldID, actor.id, actor.actor.Code)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	portal, err := loadWorldPortalStateRecord(
		ctx, tx, worldID, payload.BuildingCode, payload.PortalCode, true,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if portal.state.PortalType != "entrance" {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionPortalStateInvalid)
	}
	if !worldPortalWithinInteractionRange(*location, portal.from, portal.to) {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionPortalOutOfReach)
	}
	evaluation, err := evaluateWorldPortalAccess(
		ctx, tx, worldID, actor.id, targetTick-1, portal.state.AccessRequirement,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if !evaluation.Satisfied {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionPortalAccessDenied)
	}
	nextState, valid := nextWorldPortalState(portal.state.StateCode, payload.Action)
	if !valid {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionPortalStateInvalid)
	}
	before := portal.state
	after := before
	after.StateCode = nextState
	factPayload, err := json.Marshal(map[string]any{
		"schema_version":    1,
		"portal_before":     before,
		"action":            payload.Action,
		"access_evaluation": evaluation,
	})
	if err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("marshal world portal transition fact: %w", err)
	}
	return applyWorldPortalMutation(
		ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence,
		command, actor, portal, before, after,
		WorldRuntimeFactPortalStateChanged, WorldRuntimeEffectPortalStateSet,
		"world.portal.state_changed", factPayload,
	)
}

func (s *CityEconomyService) configureWorldPortalAccess(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload worldPortalAccessConfigurePayload,
) (worldRuntimeExecution, error) {
	var ownerUserID int64
	if err := tx.QueryRowContext(ctx, `
SELECT owner_user_id FROM city_worlds WHERE id = $1`, worldID).Scan(&ownerUserID); err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("load portal policy world owner: %w", err)
	}
	if ownerUserID != command.UserID {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionPortalAccessDenied)
	}
	requirement, _, policyHash, err := canonicalWorldPortalAccessRequirement(payload.Requirements)
	if err != nil {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionPortalPolicyInvalid)
	}
	if err = validateWorldPortalRequirementReferences(ctx, tx, worldID, requirement); err != nil {
		return worldRuntimeExecution{}, err
	}
	portal, err := loadWorldPortalStateRecord(
		ctx, tx, worldID, payload.BuildingCode, payload.PortalCode, true,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if portal.state.AccessPolicyHash == policyHash {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionPortalPolicyInvalid)
	}
	before := portal.state
	after := before
	after.AccessRequirement = requirement
	after.AccessPolicyHash = policyHash
	factPayload, err := json.Marshal(map[string]any{
		"schema_version":     1,
		"portal_before":      before,
		"access_requirement": requirement,
		"access_policy_hash": policyHash,
	})
	if err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("marshal world portal policy fact: %w", err)
	}
	return applyWorldPortalMutation(
		ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence,
		command, nil, portal, before, after,
		WorldRuntimeFactPortalAccessChanged, WorldRuntimeEffectPortalAccessSet,
		"world.portal.access_changed", factPayload,
	)
}

func applyWorldPortalMutation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	actor *worldRuntimeActorRef,
	portal *worldPortalStateRecord,
	before, after WorldPortalState,
	factType, effectType, eventType string,
	factPayload json.RawMessage,
) (worldRuntimeExecution, error) {
	if portal == nil || before.Version < 1 || after.BuildingCode != before.BuildingCode ||
		after.PortalCode != before.PortalCode || after.PortalType != before.PortalType {
		return worldRuntimeExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_portal_mutation"})
	}
	var actorID *int64
	if actor != nil {
		actorID = &actor.id
	}
	fact, err := insertWorldRuntimeFact(ctx, tx, worldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, actorID: actorID,
		factType: factType, payload: factPayload,
	})
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = enableWorldRuntimeFactWrite(ctx, tx, fact.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	after.ChangedTick = targetTick
	after.SourceFact = &WorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence}
	after.Version = before.Version + 1
	after.Metadata = json.RawMessage(`{"schema_version":1}`)
	_, accessRaw, accessHash, err := canonicalWorldPortalAccessRequirement(after.AccessRequirement)
	if err != nil || accessHash != after.AccessPolicyHash {
		return worldRuntimeExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_portal_access_policy"})
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE world_portal_states
SET state_code = $3, access_requirement = $4::jsonb, access_policy_hash = $5,
    changed_tick = $6, source_fact_id = $7, version = $8, metadata = $9::jsonb,
    updated_at = NOW()
WHERE world_id = $1 AND id = $2`, worldID, portal.id, after.StateCode, []byte(accessRaw),
		after.AccessPolicyHash, targetTick, fact.id, after.Version, []byte(after.Metadata)); err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("update world portal state projection: %w", err)
	}
	effectPayload, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"portal_before":  before,
		"portal_after":   after,
	})
	if err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("marshal world portal effect: %w", err)
	}
	operation, err := insertWorldPortalEffect(
		ctx, tx, worldID, targetTick, effectSequence, actor, fact,
		effectType, worldPortalTargetKey(before.BuildingCode, before.PortalCode),
		before.Version, after.Version, effectPayload,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if actor != nil {
		if err = touchWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
			return worldRuntimeExecution{}, err
		}
	}
	if err = updateWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 1, 0); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = postWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	pending := cityPendingEvent{
		command: command, status: CityCommandStatusApplied, eventType: eventType,
		payload: map[string]any{
			"building_code": after.BuildingCode, "portal_code": after.PortalCode,
			"state": after,
		},
		result: map[string]any{"applied": true, "state": after},
	}
	return worldRuntimeExecution{
		pending: pending, facts: []WorldRuntimeFact{fact.fact},
		effects: []WorldEffectOperation{*operation}, cases: []WorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence + 1,
		nextCaseSeq: caseSequence,
	}, nil
}

func insertWorldPortalEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	actor *worldRuntimeActorRef,
	fact *worldRuntimeFactRecord,
	effectType, targetKey string,
	before, after int64,
	payload json.RawMessage,
) (*WorldEffectOperation, error) {
	delta := after - before
	operation := &WorldEffectOperation{
		Tick: targetTick, Sequence: effectSequence,
		SourceFact:     WorldRuntimeFactRef{Tick: fact.fact.Tick, Sequence: fact.fact.Sequence},
		OperationIndex: 1, EffectType: effectType,
		ExecutorVersion: worldRuntimePortalAccessVersion,
		TargetKey:       stringPointer(targetKey), BeforeUnits: &before,
		DeltaUnits: &delta, AfterUnits: &after, Payload: payload,
	}
	var actorID any
	if actor != nil {
		actorID = actor.id
		operation.TargetActorCode = stringPointer(actor.actor.Code)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO world_effect_operations
    (world_id, tick, sequence, source_fact_id, operation_index, effect_type,
     executor_version, target_actor_id, target_key, before_units, delta_units,
     after_units, payload)
VALUES ($1, $2, $3, $4, 1, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
		worldID, targetTick, effectSequence, fact.id, effectType,
		worldRuntimePortalAccessVersion, actorID, targetKey, before, delta, after,
		[]byte(payload)); err != nil {
		return nil, fmt.Errorf("insert world portal effect %s: %w", effectType, err)
	}
	return operation, nil
}
