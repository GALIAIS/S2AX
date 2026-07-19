package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Automatic runtime work is deliberately bounded.  A world with a large
// number of expired temporary states remains live; excess rows are processed
// deterministically on the following tick rather than making one simulation
// step unbounded.
const cityOpenWorldRuntimeMaximumAutomaticStatusExpirations = 256

type cityOpenWorldRuntimeAutomaticExecution struct {
	facts         []CityOpenWorldRuntimeFact
	effects       []CityOpenWorldRuntimeEffect
	events        []worldRuntimeAutomaticEvent
	nextFactSeq   int64
	nextEffectSeq int64
}

type expiringCityOpenWorldRuntimeStatus struct {
	id             int64
	actorID        int64
	actorCode      string
	instanceCode   string
	statusCode     string
	intensityUnits int64
	stacks         int
	grantedTick    int64
	expiresTick    int64
	sourceFactID   int64
	sourceFact     CityOpenWorldRuntimeFactRef
	version        int64
	metadata       json.RawMessage
}

// expireCityOpenWorldRuntimeStatuses turns elapsed duration into an immutable
// V4 fact plus an auditable effect.  It intentionally does not use the F7
// world-runtime expiration path: every table, actor reference and fact in this
// flow belongs to city_open_world_runtime_*.
func expireCityOpenWorldRuntimeStatuses(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence int64,
) (cityOpenWorldRuntimeAutomaticExecution, error) {
	execution := cityOpenWorldRuntimeAutomaticExecution{
		facts: make([]CityOpenWorldRuntimeFact, 0), effects: make([]CityOpenWorldRuntimeEffect, 0),
		events: make([]worldRuntimeAutomaticEvent, 0), nextFactSeq: factSequence, nextEffectSeq: effectSequence,
	}
	rows, err := tx.QueryContext(ctx, `
SELECT status.id, status.actor_id, actor.code, status.instance_code, status.status_code,
       status.intensity_units, status.stacks, status.granted_tick, status.expires_tick,
       status.source_fact_id, source.tick, source.sequence, status.version, status.metadata
FROM city_open_world_actor_statuses AS status
JOIN city_open_world_actors AS actor
  ON actor.id = status.actor_id AND actor.world_id = status.world_id
JOIN city_open_world_runtime_facts AS source
  ON source.id = status.source_fact_id AND source.world_id = status.world_id
WHERE status.world_id = $1
  AND status.lifecycle_status = 'active'
  AND status.expires_tick IS NOT NULL
  AND status.expires_tick <= $2
ORDER BY actor.code ASC, status.status_code ASC, status.instance_code ASC
LIMIT $3
FOR UPDATE OF status`, worldID, targetTick, cityOpenWorldRuntimeMaximumAutomaticStatusExpirations)
	if err != nil {
		return execution, fmt.Errorf("load expiring open-world actor statuses: %w", err)
	}
	items := make([]expiringCityOpenWorldRuntimeStatus, 0)
	for rows.Next() {
		item := expiringCityOpenWorldRuntimeStatus{}
		if err = rows.Scan(
			&item.id, &item.actorID, &item.actorCode, &item.instanceCode, &item.statusCode,
			&item.intensityUnits, &item.stacks, &item.grantedTick, &item.expiresTick,
			&item.sourceFactID, &item.sourceFact.Tick, &item.sourceFact.Sequence,
			&item.version, &item.metadata,
		); err != nil {
			_ = rows.Close()
			return execution, fmt.Errorf("scan expiring open-world actor status: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate expiring open-world actor statuses"); err != nil {
		return execution, err
	}
	if len(items) == 0 {
		return execution, nil
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}

	for _, item := range items {
		definition, definitionErr := loadCityOpenWorldRuntimeDefinition(
			ctx, tx, worldID, WorldRuntimeDefinitionStatus, item.statusCode,
		)
		if definitionErr != nil {
			return execution, definitionErr
		}
		statusBefore := CityOpenWorldActorStatus{
			ActorCode: item.actorCode, InstanceCode: item.instanceCode, StatusCode: item.statusCode,
			Lifecycle: "active", IntensityUnits: item.intensityUnits, Stacks: item.stacks,
			GrantedTick: item.grantedTick, ExpiresTick: cityOpenWorldInt64Pointer(item.expiresTick),
			SourceFact: &item.sourceFact, Version: item.version, Metadata: item.metadata,
		}
		factPayload, marshalErr := json.Marshal(map[string]any{
			"schema_version":  1,
			"status_before":   statusBefore,
			"expiration_tick": targetTick,
			"reason":          "duration_elapsed",
		})
		if marshalErr != nil {
			return execution, fmt.Errorf("marshal open-world status expiration fact: %w", marshalErr)
		}
		fact, insertErr := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
			worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
			parentFactID: &item.sourceFactID, actorID: &item.actorID,
			factType: CityOpenWorldRuntimeFactStatusExpired, definition: definition, payload: factPayload,
		})
		if insertErr != nil {
			return execution, insertErr
		}
		execution.nextFactSeq++
		endedTick := targetTick
		statusAfter := statusBefore
		statusAfter.Lifecycle = "expired"
		statusAfter.EndedTick = &endedTick
		statusAfter.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: fact.fact.Tick, Sequence: fact.fact.Sequence}
		statusAfter.Version++
		effectPayload, marshalErr := json.Marshal(map[string]any{
			"schema_version": 1,
			"status_before":  statusBefore,
			"status_after":   statusAfter,
		})
		if marshalErr != nil {
			return execution, fmt.Errorf("marshal open-world status expiration effect: %w", marshalErr)
		}
		result, updateErr := tx.ExecContext(ctx, `
UPDATE city_open_world_actor_statuses
SET lifecycle_status = 'expired', ended_tick = $3, source_fact_id = $4,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND lifecycle_status = 'active'`,
			worldID, item.id, targetTick, fact.id,
		)
		if updateErr != nil {
			return execution, fmt.Errorf("expire open-world actor status: %w", updateErr)
		}
		updated, rowsErr := result.RowsAffected()
		if rowsErr != nil || updated != 1 {
			return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "open_world_runtime_status_expiration",
			})
		}
		before := int64(item.stacks)
		after := int64(0)
		delta := -before
		effect, effectErr := insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
			worldID: worldID, tick: targetTick, sequence: execution.nextEffectSeq, sourceFact: fact,
			operationIndex: 1, effectType: WorldRuntimeEffectStatusExpire, targetActorID: &item.actorID,
			targetActorCode: cityOpenWorldStringPointer(item.actorCode), targetKey: cityOpenWorldStringPointer(item.statusCode),
			beforeUnits: &before, deltaUnits: &delta, afterUnits: &after, payload: effectPayload,
		})
		if effectErr != nil {
			return execution, effectErr
		}
		execution.nextEffectSeq++
		if err = touchCityOpenWorldActor(ctx, tx, worldID, item.actorID, targetTick); err != nil {
			return execution, err
		}
		if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
			return execution, err
		}
		execution.facts = append(execution.facts, fact.fact)
		execution.effects = append(execution.effects, effect)
		execution.events = append(execution.events, worldRuntimeAutomaticEvent{
			eventType: "city.open_world.actor.status_expired",
			payload: map[string]any{
				"actor_code": item.actorCode, "status_code": item.statusCode,
				"instance_code": item.instanceCode, "expiration_tick": targetTick,
			},
		})
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, int64(len(execution.facts)), int64(len(execution.effects)), 0); err != nil {
		return execution, err
	}
	return execution, nil
}

func cityOpenWorldInt64Pointer(value int64) *int64 { return &value }
