package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

const (
	cityOpenWorldV5NavigationDefaultMaximumSteps = 128
	cityOpenWorldV5NavigationMaximumSteps        = 2048
	cityOpenWorldV5NavigationMaximumPerTick      = 64
	cityOpenWorldV5NavigationMaximumBlocked      = 12
	cityOpenWorldV5NavigationMaximumRetryDelay   = 4
	cityOpenWorldV5NavigationVersion             = "1.0.0"

	cityOpenWorldV5NavigationStatusActive    = "active"
	cityOpenWorldV5NavigationStatusArrived   = "arrived"
	cityOpenWorldV5NavigationStatusCancelled = "cancelled"
	cityOpenWorldV5NavigationStatusFailed    = "failed"

	cityOpenWorldV5NavigationReasonArrived       = "target_reached"
	cityOpenWorldV5NavigationReasonCancelled     = "user_cancelled"
	cityOpenWorldV5NavigationReasonBlocked       = "route_blocked"
	cityOpenWorldV5NavigationReasonTargetInvalid = "target_invalid"
	cityOpenWorldV5NavigationReasonStepLimit     = "step_limit_reached"
)

type cityOpenWorldV5NavigationIntentRecord struct {
	actorID      int64
	sourceFactID int64
	intent       CityOpenWorldNavigationIntent
}

type cityOpenWorldV5NavigationActorRef struct {
	id        int64
	code      string
	actorType string
	location  CityOpenWorldActorLocation
}

const cityOpenWorldV5NavigationIntentSelect = `
SELECT intent.actor_id, intent.source_fact_id, actor.code, intent.intent_code,
       intent.target_space_kind, intent.target_location_scope, intent.target_building_code,
       intent.target_floor_index, intent.target_x, intent.target_y, intent.target_z,
       intent.status, intent.priority, intent.maximum_steps, intent.completed_steps,
       intent.blocked_attempts, intent.next_attempt_tick, intent.created_tick,
       intent.updated_tick, source_fact.tick, source_fact.sequence, intent.version, intent.metadata
FROM city_open_world_actor_navigation_intents intent
JOIN city_open_world_actors actor
  ON actor.id = intent.actor_id AND actor.world_id = intent.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = intent.source_fact_id AND source_fact.world_id = intent.world_id
`

func cityOpenWorldV5NavigationLocationPayloadValid(payload cityOpenWorldActorNavigationSetPayload) bool {
	switch payload.SpaceKind {
	case "surface":
		return payload.BuildingCode == "" && payload.FloorIndex == 0 && payload.Z == 0
	case "interior":
		return worldRuntimeCodeValid(payload.BuildingCode, 96) && payload.FloorIndex >= 0 && payload.Z == payload.FloorIndex
	default:
		return false
	}
}

func cityOpenWorldV5NavigationIntentTarget(intent CityOpenWorldNavigationIntent) (CityOpenWorldActorLocation, error) {
	return cityOpenWorldRuntimeLocationFromPayload(cityOpenWorldActorMovePayload{
		ActorCode: intent.ActorCode, SpaceKind: intent.TargetSpaceKind,
		BuildingCode: cityOpenWorldV5StringValue(intent.TargetBuildingCode), FloorIndex: intent.TargetFloorIndex,
		X: intent.TargetX, Y: intent.TargetY, Z: intent.TargetZ,
	})
}

func cityOpenWorldV5StringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func scanCityOpenWorldV5NavigationIntent(row cityScannable) (*cityOpenWorldV5NavigationIntentRecord, error) {
	record := &cityOpenWorldV5NavigationIntentRecord{}
	var buildingCode sql.NullString
	if err := row.Scan(
		&record.actorID, &record.sourceFactID, &record.intent.ActorCode, &record.intent.IntentCode,
		&record.intent.TargetSpaceKind, &record.intent.TargetLocationScope, &buildingCode,
		&record.intent.TargetFloorIndex, &record.intent.TargetX, &record.intent.TargetY, &record.intent.TargetZ,
		&record.intent.Status, &record.intent.Priority, &record.intent.MaximumSteps, &record.intent.CompletedSteps,
		&record.intent.BlockedAttempts, &record.intent.NextAttemptTick, &record.intent.CreatedTick,
		&record.intent.UpdatedTick, &record.intent.SourceFact.Tick, &record.intent.SourceFact.Sequence,
		&record.intent.Version, &record.intent.Metadata,
	); err != nil {
		return nil, err
	}
	if buildingCode.Valid {
		record.intent.TargetBuildingCode = cityOpenWorldStringPointer(buildingCode.String)
	}
	return record, nil
}

func loadCityOpenWorldV5NavigationIntent(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	forUpdate bool,
) (*cityOpenWorldV5NavigationIntentRecord, error) {
	query := cityOpenWorldV5NavigationIntentSelect + `
WHERE intent.world_id = $1 AND actor.code = $2`
	if forUpdate {
		query += ` FOR UPDATE OF intent`
	}
	record, err := scanCityOpenWorldV5NavigationIntent(queryer.QueryRowContext(ctx, query, worldID, actorCode))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load open-world V5 navigation intent %s: %w", actorCode, err)
	}
	return record, nil
}

func cityOpenWorldV5NavigationIntentBefore(record *cityOpenWorldV5NavigationIntentRecord) any {
	if record == nil {
		return nil
	}
	return record.intent
}

func cityOpenWorldV5NavigationIntentCode(commandSequence int64) string {
	return "navigation." + strconv.FormatInt(commandSequence, 10)
}

func cityOpenWorldV5NavigationMetadata(reason string) (json.RawMessage, error) {
	metadata := map[string]any{"schema_version": 1, "navigation_version": cityOpenWorldV5NavigationVersion}
	if reason != "" {
		metadata["last_reason"] = reason
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (s *CityEconomyService) setCityOpenWorldV5NavigationIntent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldActorNavigationSetPayload,
) (cityOpenWorldRuntimeExecution, error) {
	if err := ensureCityOpenWorldV5SocialRuntime(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	actor, err := loadControlledCityOpenWorldActor(
		ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityCommand,
	)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	target, err := cityOpenWorldRuntimeLocationFromPayload(cityOpenWorldActorMovePayload{
		ActorCode: payload.ActorCode, SpaceKind: payload.SpaceKind, BuildingCode: payload.BuildingCode,
		FloorIndex: payload.FloorIndex, X: payload.X, Y: payload.Y, Z: payload.Z,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = cityOpenWorldRuntimeValidatePassableLocation(ctx, tx, worldID, target); err != nil {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldV5NavigationReasonTargetInvalid)
	}
	before, err := loadCityOpenWorldV5NavigationIntent(ctx, tx, worldID, actor.actor.Code, true)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	metadata, err := cityOpenWorldV5NavigationMetadata("")
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	after := CityOpenWorldNavigationIntent{
		ActorCode: actor.actor.Code, IntentCode: cityOpenWorldV5NavigationIntentCode(command.Sequence),
		TargetSpaceKind: target.SpaceKind, TargetLocationScope: target.LocationScope,
		TargetBuildingCode: target.BuildingCode, TargetFloorIndex: target.FloorIndex,
		TargetX: target.X, TargetY: target.Y, TargetZ: target.Z,
		Status: cityOpenWorldV5NavigationStatusActive, Priority: payload.Priority,
		MaximumSteps: payload.MaximumSteps, NextAttemptTick: targetTick,
		CreatedTick: targetTick, UpdatedTick: targetTick, Version: 1, Metadata: metadata,
	}
	factType := CityOpenWorldRuntimeFactNavigationCreated
	if before != nil {
		factType = CityOpenWorldRuntimeFactNavigationReplaced
		after.Version = before.intent.Version + 1
	}
	factPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "navigation_intent_before": cityOpenWorldV5NavigationIntentBefore(before),
		"request": payload, "target": target,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal V5 navigation set fact: %w", err)
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &actor.id, factType: factType, payload: factPayload,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	after.SourceFact = CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence}
	effect, err := applyCityOpenWorldV5NavigationIntentEffect(
		ctx, tx, worldID, targetTick, effectSequence, 1, actor, root, before, after,
	)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = touchCityOpenWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 1, 0); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.actor.navigation_set", map[string]any{
			"actor_code": actor.actor.Code, "intent": after,
		}),
		facts: []CityOpenWorldRuntimeFact{root.fact}, effects: []CityOpenWorldRuntimeEffect{effect},
		cases: []CityOpenWorldRuleCase{}, nextFactSeq: factSequence + 1,
		nextEffectSeq: effectSequence + 1, nextCaseSeq: caseSequence,
	}, nil
}

func (s *CityEconomyService) cancelCityOpenWorldV5NavigationIntent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldActorNavigationCancelPayload,
) (cityOpenWorldRuntimeExecution, error) {
	if err := ensureCityOpenWorldV5SocialRuntime(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	actor, err := loadControlledCityOpenWorldActor(
		ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityCommand,
	)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	before, err := loadCityOpenWorldV5NavigationIntent(ctx, tx, worldID, actor.actor.Code, true)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if before == nil || before.intent.Status != cityOpenWorldV5NavigationStatusActive {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionLocationInvalid)
	}
	after := before.intent
	after.Status = cityOpenWorldV5NavigationStatusCancelled
	after.NextAttemptTick = targetTick
	after.UpdatedTick = targetTick
	after.Version++
	metadata, err := cityOpenWorldV5NavigationMetadata(cityOpenWorldV5NavigationReasonCancelled)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	after.Metadata = metadata
	factPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "navigation_intent_before": before.intent,
		"reason": cityOpenWorldV5NavigationReasonCancelled,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal V5 navigation cancel fact: %w", err)
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &actor.id, factType: CityOpenWorldRuntimeFactNavigationCancelled, payload: factPayload,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	after.SourceFact = CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence}
	effect, err := applyCityOpenWorldV5NavigationIntentEffect(
		ctx, tx, worldID, targetTick, effectSequence, 1, actor, root, before, after,
	)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = touchCityOpenWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 1, 0); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.actor.navigation_cancelled", map[string]any{
			"actor_code": actor.actor.Code, "intent": after,
		}),
		facts: []CityOpenWorldRuntimeFact{root.fact}, effects: []CityOpenWorldRuntimeEffect{effect},
		cases: []CityOpenWorldRuleCase{}, nextFactSeq: factSequence + 1,
		nextEffectSeq: effectSequence + 1, nextCaseSeq: caseSequence,
	}, nil
}

func applyCityOpenWorldV5NavigationIntentEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	operationIndex int,
	actor *cityOpenWorldRuntimeActorRef,
	fact *cityOpenWorldRuntimeFactRecord,
	before *cityOpenWorldV5NavigationIntentRecord,
	after CityOpenWorldNavigationIntent,
) (CityOpenWorldRuntimeEffect, error) {
	if actor == nil || fact == nil || after.ActorCode != actor.actor.Code ||
		after.SourceFact != (CityOpenWorldRuntimeFactRef{Tick: fact.fact.Tick, Sequence: fact.fact.Sequence}) ||
		after.UpdatedTick != targetTick || after.MaximumSteps < 1 ||
		after.MaximumSteps > cityOpenWorldV5NavigationMaximumSteps ||
		after.CompletedSteps < 0 || after.BlockedAttempts < 0 {
		return CityOpenWorldRuntimeEffect{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_navigation_projection"})
	}
	beforeVersion := int64(0)
	if before == nil {
		if after.Version != 1 {
			return CityOpenWorldRuntimeEffect{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_navigation_initial_version"})
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_navigation_intents
    (world_id, actor_id, intent_code, target_space_kind, target_location_scope,
     target_building_code, target_floor_index, target_x, target_y, target_z,
     status, priority, maximum_steps, completed_steps, blocked_attempts,
     next_attempt_tick, created_tick, updated_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
        $16, $17, $18, $19, $20, $21::jsonb)`,
			worldID, actor.id, after.IntentCode, after.TargetSpaceKind, after.TargetLocationScope,
			cityOpenWorldNullableString(after.TargetBuildingCode), after.TargetFloorIndex,
			after.TargetX, after.TargetY, after.TargetZ, after.Status, after.Priority,
			after.MaximumSteps, after.CompletedSteps, after.BlockedAttempts, after.NextAttemptTick,
			after.CreatedTick, after.UpdatedTick, fact.id, after.Version, []byte(after.Metadata),
		); err != nil {
			return CityOpenWorldRuntimeEffect{}, fmt.Errorf("insert open-world V5 navigation intent: %w", err)
		}
	} else {
		beforeVersion = before.intent.Version
		if before.actorID != actor.id || before.intent.ActorCode != actor.actor.Code || after.Version != beforeVersion+1 {
			return CityOpenWorldRuntimeEffect{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_navigation_version"})
		}
		result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_actor_navigation_intents
SET intent_code = $3, target_space_kind = $4, target_location_scope = $5,
    target_building_code = $6, target_floor_index = $7, target_x = $8,
    target_y = $9, target_z = $10, status = $11, priority = $12,
    maximum_steps = $13, completed_steps = $14, blocked_attempts = $15,
    next_attempt_tick = $16, created_tick = $17, updated_tick = $18,
    source_fact_id = $19, version = $20, metadata = $21::jsonb, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2 AND version = $22`,
			worldID, actor.id, after.IntentCode, after.TargetSpaceKind, after.TargetLocationScope,
			cityOpenWorldNullableString(after.TargetBuildingCode), after.TargetFloorIndex,
			after.TargetX, after.TargetY, after.TargetZ, after.Status, after.Priority,
			after.MaximumSteps, after.CompletedSteps, after.BlockedAttempts, after.NextAttemptTick,
			after.CreatedTick, after.UpdatedTick, fact.id, after.Version, []byte(after.Metadata), beforeVersion,
		)
		if err != nil {
			return CityOpenWorldRuntimeEffect{}, fmt.Errorf("update open-world V5 navigation intent: %w", err)
		}
		if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
			return CityOpenWorldRuntimeEffect{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_navigation_concurrency"})
		}
	}
	effectPayload, err := json.Marshal(map[string]any{
		"schema_version":           1,
		"navigation_intent_before": cityOpenWorldV5NavigationIntentBefore(before),
		"navigation_intent_after":  after,
	})
	if err != nil {
		return CityOpenWorldRuntimeEffect{}, fmt.Errorf("marshal open-world V5 navigation effect: %w", err)
	}
	delta := after.Version - beforeVersion
	return insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
		worldID: worldID, tick: targetTick, sequence: effectSequence, sourceFact: fact,
		operationIndex: operationIndex, effectType: WorldRuntimeEffectNavigationIntentSet,
		targetActorID: &actor.id, targetActorCode: &actor.actor.Code,
		targetKey:   cityOpenWorldStringPointer("navigation.intent"),
		beforeUnits: &beforeVersion, deltaUnits: &delta, afterUnits: &after.Version,
		payload: effectPayload,
	})
}
