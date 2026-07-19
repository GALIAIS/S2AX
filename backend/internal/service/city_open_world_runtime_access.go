package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

func loadCityOpenWorldRuntimePortalStateForUse(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	portal *cityOpenWorldStaticPortal,
) (CityOpenWorldPortalState, error) {
	if portal == nil {
		return CityOpenWorldPortalState{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_portal"})
	}
	item := CityOpenWorldPortalState{PortalCode: portal.Code, BuildingCode: portal.BuildingCode, PortalType: portal.PortalType}
	var rawRequirement []byte
	var factTick, factSequence sql.NullInt64
	err := queryer.QueryRowContext(ctx, `
SELECT state_code, access_requirement, access_policy_hash, changed_tick,
       fact.tick, fact.sequence, state.version, state.metadata
FROM city_open_world_portal_states state
LEFT JOIN city_open_world_runtime_facts fact ON fact.id = state.source_fact_id AND fact.world_id = state.world_id
WHERE state.world_id = $1 AND state.portal_code = $2`, worldID, portal.Code).Scan(
		&item.StateCode, &rawRequirement, &item.AccessPolicyHash, &item.ChangedTick,
		&factTick, &factSequence, &item.Version, &item.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		requirement, raw, hash, canonicalErr := canonicalWorldPortalAccessRequirement(publicWorldPortalAccessRequirement())
		if canonicalErr != nil {
			return CityOpenWorldPortalState{}, canonicalErr
		}
		item.StateCode = WorldPortalStateOpen
		item.AccessRequirement = requirement
		item.AccessPolicyHash = hash
		item.Metadata = json.RawMessage(`{}`)
		_ = raw
		return item, nil
	}
	if err != nil {
		return CityOpenWorldPortalState{}, fmt.Errorf("load open-world portal runtime state: %w", err)
	}
	if err = json.Unmarshal(rawRequirement, &item.AccessRequirement); err != nil {
		return CityOpenWorldPortalState{}, fmt.Errorf("decode open-world portal runtime requirement: %w", err)
	}
	if factTick.Valid {
		item.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
	}
	return item, nil
}

func (s *CityEconomyService) changeCityOpenWorldPortalState(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldPortalStatePayload,
) (cityOpenWorldRuntimeExecution, error) {
	actor, err := loadControlledCityOpenWorldActor(ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityManageControl)
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
	if !cityOpenWorldRuntimeLocationEqual(actor.location, from) && !cityOpenWorldRuntimeLocationEqual(actor.location, to) {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionPortalOutOfReach)
	}
	previous, err := loadCityOpenWorldRuntimePortalStateForUse(ctx, tx, worldID, portal)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	nextState := previous.StateCode
	switch payload.Action {
	case WorldPortalActionOpen:
		nextState = WorldPortalStateOpen
	case WorldPortalActionClose:
		nextState = WorldPortalStateClosed
	case WorldPortalActionLock:
		nextState = WorldPortalStateLocked
	case WorldPortalActionUnlock:
		nextState = WorldPortalStateClosed
	default:
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionPortalState)
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	raw, err := json.Marshal(map[string]any{
		"portal_code": portal.Code, "previous_state": previous.StateCode, "state": nextState, "action": payload.Action,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal open-world portal state fact: %w", err)
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &actor.id, factType: CityOpenWorldRuntimeFactPortalStateChanged, payload: raw,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	canonicalRequirement, requirementRaw, requirementHash, err := canonicalWorldPortalAccessRequirement(previous.AccessRequirement)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, ErrCitySimulationInvariant.WithCause(err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_portal_states
    (world_id, portal_code, state_code, access_requirement, access_policy_hash,
     changed_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, 1, '{}'::jsonb)
ON CONFLICT (world_id, portal_code) DO UPDATE
SET state_code = EXCLUDED.state_code, changed_tick = EXCLUDED.changed_tick,
    source_fact_id = EXCLUDED.source_fact_id, version = city_open_world_portal_states.version + 1,
    updated_at = NOW()`, worldID, portal.Code, nextState, []byte(requirementRaw), requirementHash, targetTick, root.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("set open-world portal state: %w", err)
	}
	_ = canonicalRequirement
	effect, err := insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
		worldID: worldID, tick: targetTick, sequence: effectSequence, sourceFact: root, operationIndex: 1,
		effectType: WorldRuntimeEffectPortalStateSet, targetActorID: &actor.id,
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
		pending: cityOpenWorldRuntimePending(command, "city.open_world.portal.state_changed", map[string]any{"portal_code": portal.Code, "state": nextState}),
		facts:   []CityOpenWorldRuntimeFact{root.fact}, effects: []CityOpenWorldRuntimeEffect{effect}, cases: []CityOpenWorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence + 1, nextCaseSeq: caseSequence,
	}, nil
}

func (s *CityEconomyService) changeCityOpenWorldPortalAccess(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldPortalAccessPayload,
) (cityOpenWorldRuntimeExecution, error) {
	actor, err := loadControlledCityOpenWorldActor(ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityManageControl)
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
	if !cityOpenWorldRuntimeLocationEqual(actor.location, from) && !cityOpenWorldRuntimeLocationEqual(actor.location, to) {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionPortalOutOfReach)
	}
	canonicalRequirement, rawRequirement, requirementHash, err := canonicalWorldPortalAccessRequirement(payload.Requirements)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionRequirement)
	}
	if err = validateCityOpenWorldRuntimeRequirementReferences(ctx, tx, worldID, canonicalRequirement); err != nil {
		if errors.Is(err, ErrCityOpenWorldRuntimeDefinitionNotFound) {
			return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionDefinition)
		}
		return cityOpenWorldRuntimeExecution{}, err
	}
	previous, err := loadCityOpenWorldRuntimePortalStateForUse(ctx, tx, worldID, portal)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	raw, err := json.Marshal(map[string]any{
		"portal_code": portal.Code, "state": previous.StateCode, "access_policy_hash": requirementHash,
		"requirements": canonicalRequirement,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal open-world portal access fact: %w", err)
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &actor.id, factType: CityOpenWorldRuntimeFactPortalAccessChanged, payload: raw,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_portal_states
    (world_id, portal_code, state_code, access_requirement, access_policy_hash,
     changed_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, 1, '{}'::jsonb)
ON CONFLICT (world_id, portal_code) DO UPDATE
SET access_requirement = EXCLUDED.access_requirement, access_policy_hash = EXCLUDED.access_policy_hash,
    changed_tick = EXCLUDED.changed_tick, source_fact_id = EXCLUDED.source_fact_id,
    version = city_open_world_portal_states.version + 1, updated_at = NOW()`,
		worldID, portal.Code, previous.StateCode, []byte(rawRequirement), requirementHash, targetTick, root.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("set open-world portal access: %w", err)
	}
	effect, err := insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
		worldID: worldID, tick: targetTick, sequence: effectSequence, sourceFact: root, operationIndex: 1,
		effectType: WorldRuntimeEffectPortalAccessSet, targetActorID: &actor.id,
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
		pending: cityOpenWorldRuntimePending(command, "city.open_world.portal.access_changed", map[string]any{"portal_code": portal.Code, "access_policy_hash": requirementHash}),
		facts:   []CityOpenWorldRuntimeFact{root.fact}, effects: []CityOpenWorldRuntimeEffect{effect}, cases: []CityOpenWorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence + 1, nextCaseSeq: caseSequence,
	}, nil
}

func (s *CityEconomyService) changeCityOpenWorldActorControl(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldActorControlPayload,
	grant bool,
) (cityOpenWorldRuntimeExecution, error) {
	actor, err := loadControlledCityOpenWorldActor(ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityManageControl)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	var activeMember bool
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM city_members WHERE world_id = $1 AND user_id = $2 AND status = 'active'
)`, worldID, payload.UserID).Scan(&activeMember); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("verify open-world control member: %w", err)
	}
	if !activeMember {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionControlDenied)
	}
	changed := make([]string, 0, len(payload.Capabilities))
	for _, capability := range payload.Capabilities {
		var exists bool
		if err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM city_open_world_actor_controls
    WHERE world_id = $1 AND actor_id = $2 AND user_id = $3 AND capability = $4 AND status = 'active'
)`, worldID, actor.id, payload.UserID, capability).Scan(&exists); err != nil {
			return cityOpenWorldRuntimeExecution{}, fmt.Errorf("check open-world control: %w", err)
		}
		if exists == grant {
			continue
		}
		changed = append(changed, capability)
	}
	if len(changed) == 0 {
		return cityOpenWorldRuntimeExecution{
			pending: cityOpenWorldRuntimePending(command, "city.open_world.actor.control_unchanged", map[string]any{
				"actor_code": actor.actor.Code, "capabilities": payload.Capabilities, "changed": false,
			}),
			facts: []CityOpenWorldRuntimeFact{}, effects: []CityOpenWorldRuntimeEffect{}, cases: []CityOpenWorldRuleCase{},
			nextFactSeq: factSequence, nextEffectSeq: effectSequence, nextCaseSeq: caseSequence,
		}, nil
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	raw, err := json.Marshal(map[string]any{"actor_code": actor.actor.Code, "user_id": payload.UserID, "capabilities": changed, "grant": grant})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal open-world control fact: %w", err)
	}
	factType := CityOpenWorldRuntimeFactControlRevoked
	if grant {
		factType = CityOpenWorldRuntimeFactControlGranted
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &actor.id, factType: factType, payload: raw,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	effects := make([]CityOpenWorldRuntimeEffect, 0, len(changed))
	for index, capability := range changed {
		if grant {
			code := "grant." + strconv.FormatInt(actor.id, 10) + "." + strconv.FormatInt(payload.UserID, 10) + "." + capability + "." + strconv.FormatInt(command.Sequence, 10)
			if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_controls
    (world_id, code, actor_id, user_id, capability, status, granted_by_user_id,
     granted_tick, grant_source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, 'active', $6, $7, $8, 1, '{}'::jsonb)`,
				worldID, code, actor.id, payload.UserID, capability, command.UserID, targetTick, root.id); err != nil {
				return cityOpenWorldRuntimeExecution{}, fmt.Errorf("grant open-world actor control: %w", err)
			}
		} else {
			result, updateErr := tx.ExecContext(ctx, `
UPDATE city_open_world_actor_controls
SET status = 'revoked', revoked_tick = $5, revoke_source_fact_id = $6,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2 AND user_id = $3 AND capability = $4 AND status = 'active'`,
				worldID, actor.id, payload.UserID, capability, targetTick, root.id)
			if updateErr != nil {
				return cityOpenWorldRuntimeExecution{}, fmt.Errorf("revoke open-world actor control: %w", updateErr)
			}
			rows, rowsErr := result.RowsAffected()
			if rowsErr != nil || rows != 1 {
				return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionControlMissing)
			}
		}
		effectType := WorldRuntimeEffectControlRevoke
		if grant {
			effectType = WorldRuntimeEffectControlGrant
		}
		capabilityPayload, marshalErr := json.Marshal(map[string]any{"user_id": payload.UserID, "capability": capability, "grant": grant})
		if marshalErr != nil {
			return cityOpenWorldRuntimeExecution{}, marshalErr
		}
		effect, effectErr := insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
			worldID: worldID, tick: targetTick, sequence: effectSequence + int64(index), sourceFact: root,
			operationIndex: index + 1, effectType: effectType, targetActorID: &actor.id,
			targetActorCode: &actor.actor.Code, targetKey: cityOpenWorldStringPointer(capability), payload: capabilityPayload,
		})
		if effectErr != nil {
			return cityOpenWorldRuntimeExecution{}, effectErr
		}
		effects = append(effects, effect)
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, 1, int64(len(effects)), 0); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.actor.control_changed", map[string]any{"actor_code": actor.actor.Code, "capabilities": changed, "grant": grant}),
		facts:   []CityOpenWorldRuntimeFact{root.fact}, effects: effects, cases: []CityOpenWorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence + int64(len(effects)), nextCaseSeq: caseSequence,
	}, nil
}
