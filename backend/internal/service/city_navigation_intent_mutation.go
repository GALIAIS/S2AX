package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

func (s *CityEconomyService) setWorldNavigationIntent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload worldNavigationIntentSetPayload,
) (worldRuntimeExecution, error) {
	actor, err := loadWorldActorWithCapability(
		ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityCommand,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	profile, err := loadWorldNavigationProfile(ctx, tx, worldID)
	if err != nil {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionNavigationIntentUnavailable)
	}
	if payload.MaxSteps == 0 {
		payload.MaxSteps = profile.DefaultMaxSteps
	}
	if payload.MaxSteps < 1 || payload.MaxSteps > 1024 ||
		payload.Priority < -10 || payload.Priority > 10 ||
		(payload.OnBlocked != WorldNavigationOnBlockedRetry &&
			payload.OnBlocked != WorldNavigationOnBlockedCancel) {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionNavigationIntentInvalid)
	}
	if _, err = resolveWorldActorLocation(
		ctx, tx, worldID, actor.actor.Code, payload.Destination.X,
		payload.Destination.Y, payload.Destination.Z, "", "",
	); err != nil {
		if worldRuntimeBusinessRejectionCode(err) != "" {
			return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionNavigationIntentInvalid)
		}
		return worldRuntimeExecution{}, err
	}
	before, err := loadOptionalWorldNavigationIntentRecord(ctx, tx, worldID, actor.actor.Code, true)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	factType := WorldRuntimeFactNavigationIntentCreated
	beforeVersion := int64(0)
	if before != nil {
		factType = WorldRuntimeFactNavigationIntentReplaced
		beforeVersion = before.intent.Version
	}
	after := WorldActorNavigationIntent{
		ActorCode: actor.actor.Code, IntentCode: worldNavigationIntentCode(command.Sequence),
		Destination: payload.Destination, Status: WorldNavigationIntentStatusActive,
		OnBlocked: payload.OnBlocked, Priority: payload.Priority, MaxSteps: payload.MaxSteps,
		BudgetUnits: 0, BudgetGainUnits: profile.DefaultBudgetGainUnits,
		BudgetCapUnits: profile.DefaultBudgetCapUnits, BlockedAttempts: 0,
		NextAttemptTick: targetTick, CreatedTick: targetTick, UpdatedTick: targetTick,
		SourceFact: WorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence},
		Version:    beforeVersion + 1, Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	factPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "intent_before": worldNavigationIntentBefore(before),
		"request": payload, "intent_code": after.IntentCode,
	})
	if err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("marshal navigation intent set fact: %w", err)
	}
	fact, err := insertWorldRuntimeFact(ctx, tx, worldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, actorID: &actor.id,
		factType: factType, payload: factPayload,
	})
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = enableWorldRuntimeFactWrite(ctx, tx, fact.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	effect, err := applyWorldNavigationIntentEffect(
		ctx, tx, worldID, targetTick, effectSequence, 1, actor, fact, before, after,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = touchWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = updateWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 1, 0); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = postWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	fact.fact.ActorCode = stringPointer(actor.actor.Code)
	pending := cityPendingEvent{
		command: command, status: CityCommandStatusApplied,
		eventType: "world.actor.navigation_intent_set",
		payload:   map[string]any{"actor_code": actor.actor.Code, "intent": after},
		result:    map[string]any{"applied": true, "intent": after},
	}
	return worldRuntimeExecution{
		pending: pending, facts: []WorldRuntimeFact{fact.fact},
		effects: []WorldEffectOperation{*effect}, cases: []WorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence + 1,
		nextCaseSeq: caseSequence,
	}, nil
}

func (s *CityEconomyService) cancelWorldNavigationIntent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload worldNavigationIntentCancelPayload,
) (worldRuntimeExecution, error) {
	actor, err := loadWorldActorWithCapability(
		ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityCommand,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	before, err := loadOptionalWorldNavigationIntentRecord(ctx, tx, worldID, actor.actor.Code, true)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if before == nil {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionNavigationIntentUnavailable)
	}
	if before.intent.Status != WorldNavigationIntentStatusActive &&
		before.intent.Status != WorldNavigationIntentStatusBlocked {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionNavigationIntentTerminal)
	}
	after := before.intent
	after.Status = WorldNavigationIntentStatusCancelled
	after.LastReason = worldNavigationLastReason(WorldNavigationReasonUserCancelled)
	after.NextAttemptTick = targetTick
	finalizeWorldNavigationIntent(&after, targetTick, factSequence, before.intent.Version+1)
	factPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "intent_before": before.intent,
		"reason": WorldNavigationReasonUserCancelled,
	})
	if err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("marshal navigation intent cancel fact: %w", err)
	}
	fact, err := insertWorldRuntimeFact(ctx, tx, worldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, actorID: &actor.id,
		factType: WorldRuntimeFactNavigationIntentCancelled, payload: factPayload,
	})
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = enableWorldRuntimeFactWrite(ctx, tx, fact.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	effect, err := applyWorldNavigationIntentEffect(
		ctx, tx, worldID, targetTick, effectSequence, 1, actor, fact, before, after,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = touchWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = updateWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 1, 0); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = postWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	fact.fact.ActorCode = stringPointer(actor.actor.Code)
	pending := cityPendingEvent{
		command: command, status: CityCommandStatusApplied,
		eventType: "world.actor.navigation_intent_cancelled",
		payload:   map[string]any{"actor_code": actor.actor.Code, "intent": after},
		result:    map[string]any{"applied": true, "intent": after},
	}
	return worldRuntimeExecution{
		pending: pending, facts: []WorldRuntimeFact{fact.fact},
		effects: []WorldEffectOperation{*effect}, cases: []WorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence + 1,
		nextCaseSeq: caseSequence,
	}, nil
}

func loadOptionalWorldNavigationIntentRecord(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	forUpdate bool,
) (*worldNavigationIntentRecord, error) {
	query := worldNavigationIntentSelect + `
WHERE intent.world_id = $1 AND actor.code = $2`
	if forUpdate {
		query += ` FOR UPDATE OF intent`
	}
	record, err := scanWorldNavigationIntentRecord(queryer.QueryRowContext(ctx, query, worldID, actorCode))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load optional world navigation intent %s: %w", actorCode, err)
	}
	return record, nil
}

func worldNavigationIntentBefore(record *worldNavigationIntentRecord) any {
	if record == nil {
		return nil
	}
	return record.intent
}

func finalizeWorldNavigationIntent(
	intent *WorldActorNavigationIntent,
	targetTick, factSequence, version int64,
) {
	intent.UpdatedTick = targetTick
	intent.SourceFact = WorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence}
	intent.Version = version
	intent.Metadata = json.RawMessage(`{"schema_version":1}`)
}

func applyWorldNavigationIntentEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	operationIndex int,
	actor *worldRuntimeActorRef,
	fact *worldRuntimeFactRecord,
	before *worldNavigationIntentRecord,
	after WorldActorNavigationIntent,
) (*WorldEffectOperation, error) {
	beforeVersion := int64(0)
	if before != nil {
		beforeVersion = before.intent.Version
		if before.actorID != actor.id || before.intent.ActorCode != actor.actor.Code ||
			after.Version != beforeVersion+1 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "world_navigation_intent_version",
			})
		}
	} else if after.Version != 1 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "world_navigation_intent_initial_version",
		})
	}
	if after.ActorCode != actor.actor.Code || after.SourceFact != fact.fact.Ref() ||
		after.UpdatedTick != targetTick || after.BudgetUnits < 0 ||
		after.BudgetUnits > after.BudgetCapUnits {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "world_navigation_intent_projection",
		})
	}
	if before == nil {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO world_actor_navigation_intents
    (world_id, actor_id, intent_code, destination_x, destination_y, destination_z,
     status, on_blocked, priority, max_steps, budget_units, budget_gain_units,
     budget_cap_units, blocked_attempts, last_reason, next_attempt_tick,
     created_tick, updated_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20, $21::jsonb)`,
			worldID, actor.id, after.IntentCode, after.Destination.X,
			after.Destination.Y, after.Destination.Z, after.Status, after.OnBlocked,
			after.Priority, after.MaxSteps, after.BudgetUnits, after.BudgetGainUnits,
			after.BudgetCapUnits, after.BlockedAttempts,
			nullableStringValue(after.LastReason), after.NextAttemptTick,
			after.CreatedTick, after.UpdatedTick, fact.id, after.Version,
			[]byte(after.Metadata)); err != nil {
			return nil, fmt.Errorf("insert world navigation intent projection: %w", err)
		}
	} else {
		result, err := tx.ExecContext(ctx, `
UPDATE world_actor_navigation_intents
SET intent_code = $3, destination_x = $4, destination_y = $5,
    destination_z = $6, status = $7, on_blocked = $8, priority = $9,
    max_steps = $10, budget_units = $11, budget_gain_units = $12,
    budget_cap_units = $13, blocked_attempts = $14, last_reason = $15,
    next_attempt_tick = $16, created_tick = $17, updated_tick = $18,
    source_fact_id = $19, version = $20, metadata = $21::jsonb,
    updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2 AND version = $22`,
			worldID, actor.id, after.IntentCode, after.Destination.X,
			after.Destination.Y, after.Destination.Z, after.Status, after.OnBlocked,
			after.Priority, after.MaxSteps, after.BudgetUnits, after.BudgetGainUnits,
			after.BudgetCapUnits, after.BlockedAttempts,
			nullableStringValue(after.LastReason), after.NextAttemptTick,
			after.CreatedTick, after.UpdatedTick, fact.id, after.Version,
			[]byte(after.Metadata), beforeVersion)
		if err != nil {
			return nil, fmt.Errorf("update world navigation intent projection: %w", err)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil || rows != 1 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "world_navigation_intent_concurrency",
			})
		}
	}
	effectPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "navigation_intent_before": worldNavigationIntentBefore(before),
		"navigation_intent_after": after,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal world navigation intent effect: %w", err)
	}
	delta := after.Version - beforeVersion
	operation := &WorldEffectOperation{
		Tick: targetTick, Sequence: effectSequence,
		SourceFact:     WorldRuntimeFactRef{Tick: fact.fact.Tick, Sequence: fact.fact.Sequence},
		OperationIndex: operationIndex, EffectType: WorldRuntimeEffectNavigationIntentSet,
		ExecutorVersion: worldRuntimeNavigationIntentVersion,
		TargetActorCode: stringPointer(actor.actor.Code), TargetKey: stringPointer("navigation.intent"),
		BeforeUnits: &beforeVersion, DeltaUnits: &delta, AfterUnits: &after.Version,
		Payload: effectPayload,
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO world_effect_operations
    (world_id, tick, sequence, source_fact_id, operation_index, effect_type,
     executor_version, target_actor_id, target_key, before_units, delta_units,
     after_units, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'navigation.intent', $9, $10, $11, $12::jsonb)`,
		worldID, targetTick, effectSequence, fact.id, operationIndex,
		WorldRuntimeEffectNavigationIntentSet, worldRuntimeNavigationIntentVersion,
		actor.id, beforeVersion, delta, after.Version, []byte(effectPayload)); err != nil {
		return nil, fmt.Errorf("insert world navigation intent effect: %w", err)
	}
	return operation, nil
}

type worldNavigationIntentCandidate struct {
	actorCode         string
	effectivePriority int64
	blockedAttempts   int
	createdTick       int64
}

func selectWorldNavigationIntentCandidates(
	records []worldNavigationIntentRecord,
	profile WorldNavigationProfile,
	targetTick int64,
) ([]worldNavigationIntentCandidate, error) {
	candidates := make([]worldNavigationIntentCandidate, 0, len(records))
	for _, record := range records {
		intent := record.intent
		if intent.Status != WorldNavigationIntentStatusActive &&
			intent.Status != WorldNavigationIntentStatusBlocked ||
			intent.NextAttemptTick > targetTick {
			continue
		}
		age := targetTick - intent.UpdatedTick
		if age < 0 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "world_navigation_intent_future_tick",
			})
		}
		if age > profile.FairnessAgingCap {
			age = profile.FairnessAgingCap
		}
		candidates = append(candidates, worldNavigationIntentCandidate{
			actorCode: intent.ActorCode, effectivePriority: int64(intent.Priority) + age,
			blockedAttempts: intent.BlockedAttempts, createdTick: intent.CreatedTick,
		})
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].effectivePriority != candidates[right].effectivePriority {
			return candidates[left].effectivePriority > candidates[right].effectivePriority
		}
		if candidates[left].blockedAttempts != candidates[right].blockedAttempts {
			return candidates[left].blockedAttempts > candidates[right].blockedAttempts
		}
		if candidates[left].createdTick != candidates[right].createdTick {
			return candidates[left].createdTick < candidates[right].createdTick
		}
		return candidates[left].actorCode < candidates[right].actorCode
	})
	if len(candidates) > profile.MaximumIntentsPerTick {
		candidates = candidates[:profile.MaximumIntentsPerTick]
	}
	return candidates, nil
}

func advanceWorldNavigationIntents(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence int64,
) (worldRuntimeAutomaticExecution, error) {
	execution := worldRuntimeAutomaticExecution{
		facts: make([]WorldRuntimeFact, 0), effects: make([]WorldEffectOperation, 0),
		events:      make([]worldRuntimeAutomaticEvent, 0),
		nextFactSeq: factSequence, nextEffectSeq: effectSequence,
	}
	profile, err := loadWorldNavigationProfile(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	records, err := loadWorldNavigationIntentRecords(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	candidates, err := selectWorldNavigationIntentCandidates(records, *profile, targetTick)
	if err != nil {
		return execution, err
	}
	for _, candidate := range candidates {
		if _, err = tx.ExecContext(ctx, `SAVEPOINT world_navigation_intent_step`); err != nil {
			return execution, fmt.Errorf("create world navigation intent savepoint: %w", err)
		}
		step, stepErr := advanceWorldNavigationIntent(
			ctx, tx, worldID, targetTick, execution.nextFactSeq,
			execution.nextEffectSeq, *profile, candidate.actorCode,
		)
		if stepErr != nil {
			if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT world_navigation_intent_step`); rollbackErr != nil {
				return execution, fmt.Errorf("rollback navigation intent after %v: %w", stepErr, rollbackErr)
			}
			if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT world_navigation_intent_step`); releaseErr != nil {
				return execution, fmt.Errorf("release failed navigation intent savepoint: %w", releaseErr)
			}
			return execution, stepErr
		}
		if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT world_navigation_intent_step`); err != nil {
			return execution, fmt.Errorf("release world navigation intent savepoint: %w", err)
		}
		if step == nil {
			continue
		}
		execution.facts = append(execution.facts, step.facts...)
		execution.effects = append(execution.effects, step.effects...)
		execution.events = append(execution.events, step.events...)
		execution.nextFactSeq = step.nextFactSeq
		execution.nextEffectSeq = step.nextEffectSeq
	}
	return execution, nil
}

func advanceWorldNavigationIntent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence int64,
	profile WorldNavigationProfile,
	actorCode string,
) (*worldRuntimeAutomaticExecution, error) {
	record, err := loadOptionalWorldNavigationIntentRecord(ctx, tx, worldID, actorCode, true)
	if err != nil || record == nil {
		return nil, err
	}
	if record.intent.Status != WorldNavigationIntentStatusActive &&
		record.intent.Status != WorldNavigationIntentStatusBlocked ||
		record.intent.NextAttemptTick > targetTick {
		return nil, nil
	}
	actor := &worldRuntimeActorRef{id: record.actorID, actor: WorldActor{Code: actorCode}}
	current, err := loadWorldActorLocationForUpdate(ctx, tx, worldID, record.actorID, actorCode)
	if err != nil {
		return nil, err
	}
	after := record.intent
	budget, err := accrueWorldNavigationBudget(after, targetTick)
	if err != nil {
		return nil, err
	}
	after.BudgetUnits = budget
	from := CityNavigationCoordinate{X: current.X, Y: current.Y, Z: current.Z}
	if from == after.Destination {
		after.Status = WorldNavigationIntentStatusArrived
		after.BlockedAttempts = 0
		after.LastReason = worldNavigationLastReason(WorldNavigationReasonTargetReached)
		after.NextAttemptTick = targetTick
		return persistWorldNavigationIntentAutomaticState(
			ctx, tx, worldID, targetTick, factSequence, effectSequence,
			actor, record, after, WorldRuntimeFactNavigationIntentArrived,
			map[string]any{"reason": WorldNavigationReasonTargetReached, "position": from},
		)
	}
	navigation, err := newCityNavigationContext(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	navigation.worldTick = targetTick
	path, err := navigation.findPath(actorCode, from, after.Destination, after.MaxSteps)
	if err != nil {
		return nil, err
	}
	if !path.Reachable || len(path.Steps) < 2 {
		reason := path.Reason
		if reason == "" {
			reason = CityNavigationBlockUnreachable
		}
		return persistWorldNavigationBlockedState(
			ctx, tx, worldID, targetTick, factSequence, effectSequence,
			profile, actor, record, after, reason,
			map[string]any{"path": path},
		)
	}
	next := path.Steps[1]
	if next.StepCost < 1 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "world_navigation_step_cost",
		})
	}
	if after.BudgetUnits < next.StepCost {
		after.Status = WorldNavigationIntentStatusActive
		after.BlockedAttempts = 0
		after.LastReason = worldNavigationLastReason(WorldNavigationReasonBudgetInsufficient)
		after.NextAttemptTick = targetTick + 1
		return persistWorldNavigationIntentAutomaticState(
			ctx, tx, worldID, targetTick, factSequence, effectSequence,
			actor, record, after, WorldRuntimeFactNavigationIntentWaited,
			map[string]any{
				"reason":         WorldNavigationReasonBudgetInsufficient,
				"required_units": next.StepCost, "available_units": after.BudgetUnits,
				"path": path,
			},
		)
	}
	targetKey := worldNavigationCoordinateKey(next.Coordinate)
	edgeKey := worldNavigationEdgeKey(from, next.Coordinate)
	conflictReason, err := worldNavigationReservationConflict(
		ctx, tx, worldID, targetTick, targetKey, edgeKey,
	)
	if err != nil {
		return nil, err
	}
	if conflictReason != "" {
		return persistWorldNavigationBlockedState(
			ctx, tx, worldID, targetTick, factSequence, effectSequence,
			profile, actor, record, after, conflictReason,
			map[string]any{"path": path, "target_key": targetKey, "edge_key": edgeKey},
		)
	}
	destination, err := resolveWorldActorLocation(
		ctx, tx, worldID, actorCode, next.Coordinate.X, next.Coordinate.Y,
		next.Coordinate.Z, next.AnchorKind, next.AnchorCode,
	)
	if err != nil {
		return nil, err
	}
	after.Status = WorldNavigationIntentStatusActive
	after.BudgetUnits -= next.StepCost
	after.BlockedAttempts = 0
	after.LastReason = nil
	after.NextAttemptTick = targetTick + 1
	finalizeWorldNavigationIntent(&after, targetTick, factSequence, record.intent.Version+1)
	reservation := WorldNavigationReservation{
		Tick: targetTick, Sequence: factSequence, ActorCode: actorCode,
		IntentCode: after.IntentCode, From: from, To: next.Coordinate,
		TargetKey: targetKey, EdgeKey: edgeKey, StepCost: next.StepCost,
		SourceFact: WorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence},
		Status:     "consumed", Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	factPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "intent_before": record.intent,
		"path_proof": map[string]any{
			"navigation_version": path.NavigationVersion, "world_tick": path.WorldTick,
			"spatial_rule_hash": path.SpatialRuleHash,
			"expanded_nodes":    path.ExpandedNodes, "next_step": next,
		},
		"reservation": reservation,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal navigation progress fact: %w", err)
	}
	fact, err := insertWorldRuntimeFact(ctx, tx, worldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		parentFactID: &record.sourceFactID, actorID: &actor.id,
		factType: WorldRuntimeFactNavigationIntentProgressed,
		payload:  factPayload,
	})
	if err != nil {
		return nil, err
	}
	if err = enableWorldRuntimeFactWrite(ctx, tx, fact.id); err != nil {
		return nil, err
	}
	locationEffect, err := applyWorldActorLocationEffect(
		ctx, tx, worldID, targetTick, effectSequence, 1,
		actor, fact, destination, current,
	)
	if err != nil {
		return nil, err
	}
	intentEffect, err := applyWorldNavigationIntentEffect(
		ctx, tx, worldID, targetTick, effectSequence+1, 2,
		actor, fact, record, after,
	)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO world_navigation_reservations
    (world_id, tick, sequence, actor_id, intent_code,
     from_x, from_y, from_z, to_x, to_y, to_z, target_key, edge_key,
     step_cost, source_fact_id, status, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, 'consumed', $16::jsonb)`,
		worldID, targetTick, factSequence, actor.id, after.IntentCode,
		from.X, from.Y, from.Z, next.Coordinate.X, next.Coordinate.Y,
		next.Coordinate.Z, targetKey, edgeKey, next.StepCost, fact.id,
		[]byte(reservation.Metadata)); err != nil {
		return nil, fmt.Errorf("insert world navigation reservation: %w", err)
	}
	if err = touchWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return nil, err
	}
	if err = updateWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 2, 0); err != nil {
		return nil, err
	}
	if err = postWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return nil, err
	}
	fact.fact.ActorCode = stringPointer(actorCode)
	return &worldRuntimeAutomaticExecution{
		facts:   []WorldRuntimeFact{fact.fact},
		effects: []WorldEffectOperation{*locationEffect, *intentEffect},
		events: []worldRuntimeAutomaticEvent{{
			eventType: "world.actor.navigation_progressed",
			payload: map[string]any{
				"actor_code": actorCode, "intent": after,
				"location": destination, "reservation": reservation,
			},
		}},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence + 2,
	}, nil
}

func persistWorldNavigationBlockedState(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence int64,
	profile WorldNavigationProfile,
	actor *worldRuntimeActorRef,
	record *worldNavigationIntentRecord,
	after WorldActorNavigationIntent,
	reason string,
	detail map[string]any,
) (*worldRuntimeAutomaticExecution, error) {
	after, factType := resolveWorldNavigationBlockedOutcome(after, profile, targetTick, reason)
	if detail == nil {
		detail = make(map[string]any)
	}
	detail["reason"] = reason
	detail["blocked_attempts"] = after.BlockedAttempts
	return persistWorldNavigationIntentAutomaticState(
		ctx, tx, worldID, targetTick, factSequence, effectSequence,
		actor, record, after, factType, detail,
	)
}

func resolveWorldNavigationBlockedOutcome(
	after WorldActorNavigationIntent,
	profile WorldNavigationProfile,
	targetTick int64,
	reason string,
) (WorldActorNavigationIntent, string) {
	after.BlockedAttempts++
	factType := WorldRuntimeFactNavigationIntentBlocked
	switch {
	case after.OnBlocked == WorldNavigationOnBlockedCancel:
		after.Status = WorldNavigationIntentStatusCancelled
		factType = WorldRuntimeFactNavigationIntentCancelled
	case worldNavigationReasonPermanent(reason) ||
		after.BlockedAttempts >= profile.MaximumBlockedAttempts:
		after.Status = WorldNavigationIntentStatusFailed
		factType = WorldRuntimeFactNavigationIntentFailed
	default:
		after.Status = WorldNavigationIntentStatusBlocked
		after.NextAttemptTick = targetTick + worldNavigationRetryDelay(
			after.BlockedAttempts, profile.MaximumRetryDelayTicks,
		)
	}
	if after.Status != WorldNavigationIntentStatusBlocked {
		after.NextAttemptTick = targetTick
	}
	after.LastReason = worldNavigationLastReason(reason)
	return after, factType
}

func persistWorldNavigationIntentAutomaticState(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence int64,
	actor *worldRuntimeActorRef,
	record *worldNavigationIntentRecord,
	after WorldActorNavigationIntent,
	factType string,
	detail map[string]any,
) (*worldRuntimeAutomaticExecution, error) {
	finalizeWorldNavigationIntent(&after, targetTick, factSequence, record.intent.Version+1)
	payload := map[string]any{
		"schema_version": 1, "intent_before": record.intent,
	}
	for key, value := range detail {
		payload[key] = value
	}
	factPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal automatic navigation intent fact: %w", err)
	}
	fact, err := insertWorldRuntimeFact(ctx, tx, worldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		parentFactID: &record.sourceFactID, actorID: &actor.id,
		factType: factType, payload: factPayload,
	})
	if err != nil {
		return nil, err
	}
	if err = enableWorldRuntimeFactWrite(ctx, tx, fact.id); err != nil {
		return nil, err
	}
	effect, err := applyWorldNavigationIntentEffect(
		ctx, tx, worldID, targetTick, effectSequence, 1,
		actor, fact, record, after,
	)
	if err != nil {
		return nil, err
	}
	if err = touchWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return nil, err
	}
	if err = updateWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 1, 0); err != nil {
		return nil, err
	}
	if err = postWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return nil, err
	}
	fact.fact.ActorCode = stringPointer(actor.actor.Code)
	return &worldRuntimeAutomaticExecution{
		facts: []WorldRuntimeFact{fact.fact}, effects: []WorldEffectOperation{*effect},
		events: []worldRuntimeAutomaticEvent{{
			eventType: worldNavigationEventType(factType),
			payload:   map[string]any{"actor_code": actor.actor.Code, "intent": after},
		}},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence + 1,
	}, nil
}

func accrueWorldNavigationBudget(
	intent WorldActorNavigationIntent,
	targetTick int64,
) (int64, error) {
	if intent.BudgetUnits < 0 || intent.BudgetGainUnits < 1 ||
		intent.BudgetCapUnits < intent.BudgetGainUnits ||
		intent.BudgetUnits > intent.BudgetCapUnits || targetTick < intent.UpdatedTick {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "world_navigation_budget",
		})
	}
	elapsed := targetTick - intent.UpdatedTick
	remaining := intent.BudgetCapUnits - intent.BudgetUnits
	if remaining == 0 || elapsed == 0 {
		return intent.BudgetUnits, nil
	}
	ticksToCap := remaining / intent.BudgetGainUnits
	if remaining%intent.BudgetGainUnits != 0 {
		ticksToCap++
	}
	if elapsed >= ticksToCap {
		return intent.BudgetCapUnits, nil
	}
	return intent.BudgetUnits + elapsed*intent.BudgetGainUnits, nil
}

func worldNavigationRetryDelay(blockedAttempts int, maximum int64) int64 {
	delay := int64(1 + blockedAttempts/4)
	if delay > maximum {
		return maximum
	}
	return delay
}

func worldNavigationReasonPermanent(reason string) bool {
	return reason == CityNavigationBlockOutsideWorld || reason == CityNavigationBlockVoid ||
		reason == WorldNavigationReasonTargetInvalid
}

func worldNavigationReservationConflict(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	targetKey, edgeKey string,
) (string, error) {
	var targetExists, edgeExists bool
	if err := queryer.QueryRowContext(ctx, `
SELECT EXISTS (
           SELECT 1 FROM world_navigation_reservations
           WHERE world_id = $1 AND tick = $2 AND target_key = $3
       ),
       EXISTS (
           SELECT 1 FROM world_navigation_reservations
           WHERE world_id = $1 AND tick = $2 AND edge_key = $4
       )`, worldID, tick, targetKey, edgeKey).Scan(&targetExists, &edgeExists); err != nil {
		return "", fmt.Errorf("check world navigation reservation conflict: %w", err)
	}
	if targetExists {
		return WorldNavigationReasonReservationTarget, nil
	}
	if edgeExists {
		return WorldNavigationReasonReservationEdge, nil
	}
	return "", nil
}

func worldNavigationEventType(factType string) string {
	switch factType {
	case WorldRuntimeFactNavigationIntentArrived:
		return "world.actor.navigation_arrived"
	case WorldRuntimeFactNavigationIntentCancelled:
		return "world.actor.navigation_cancelled"
	case WorldRuntimeFactNavigationIntentFailed:
		return "world.actor.navigation_failed"
	case WorldRuntimeFactNavigationIntentWaited:
		return "world.actor.navigation_waited"
	default:
		return "world.actor.navigation_blocked"
	}
}
