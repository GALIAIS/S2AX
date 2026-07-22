package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

type cityOpenWorldImpactResponseRecord struct {
	id           int64
	actorID      int64
	sourceFactID int64
	response     CityOpenWorldServiceResponse
	requested    int64
}

type cityOpenWorldImpactEffectRecord struct {
	id            int64
	sourceFactID  int64
	targetActorID int64
	responseID    int64
	effect        CityOpenWorldImpactEffect
}

type cityOpenWorldImpactMetricRecord struct {
	exists  bool
	value   int64
	version int64
}

func cityOpenWorldImpactCatalogKey(serviceCode, outcome string) string {
	return serviceCode + "\x00" + outcome
}

func cityOpenWorldImpactEffectCode(responseCode, catalogCode string) string {
	sum := sha256.Sum256([]byte(responseCode + "\x00" + catalogCode))
	return "impact." + hex.EncodeToString(sum[:16])
}

func loadCityOpenWorldImpactPolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldImpactPolicy, error) {
	policy := &CityOpenWorldImpactPolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       source_contract_version, delivery_contract_version, maximum_schedules_per_tick,
       effect_count, applied_count, metric_count, revision, metadata
FROM city_open_world_impact_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash, &policy.BaselineTick,
		&policy.SourceContractVersion, &policy.DeliveryContractVersion,
		&policy.MaximumSchedulesPerTick, &policy.EffectCount, &policy.AppliedCount,
		&policy.MetricCount, &policy.Revision, &policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V8 impact policy: %w", err)
	}
	if policy.ProfileID != cityOpenWorldImpactProfileID ||
		policy.SourceContractVersion != cityOpenWorldImpactSourceContractVersion ||
		policy.DeliveryContractVersion != cityOpenWorldImpactDeliveryContractVersion ||
		policy.MaximumSchedulesPerTick < 1 ||
		policy.MaximumSchedulesPerTick > cityOpenWorldImpactMaximumSchedulesPerTick {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_policy"})
	}
	return policy, nil
}

func loadCityOpenWorldImpactCatalogMap(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (map[string]CityOpenWorldImpactCatalogEntry, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT code, source_kind, service_code, outcome, target_domain, metric_code,
       delta_units_per_source_unit, definition_version, content_hash, metadata
FROM city_open_world_impact_catalog
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V8 impact catalog: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make(map[string]CityOpenWorldImpactCatalogEntry)
	for rows.Next() {
		item := CityOpenWorldImpactCatalogEntry{}
		if err = rows.Scan(
			&item.Code, &item.SourceKind, &item.ServiceCode, &item.Outcome,
			&item.TargetDomain, &item.MetricCode, &item.DeltaUnitsPerSourceUnit,
			&item.Version, &item.ContentHash, &item.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan V8 impact catalog: %w", err)
		}
		key := cityOpenWorldImpactCatalogKey(item.ServiceCode, item.Outcome)
		if _, exists := items[key]; exists || item.SourceKind != cityOpenWorldImpactSourceKindService ||
			item.TargetDomain != cityOpenWorldImpactTargetDomainActor || item.DeltaUnitsPerSourceUnit == 0 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_catalog"})
		}
		items[key] = item
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V8 impact catalog: %w", err)
	}
	if len(items) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_catalog"})
	}
	return items, nil
}

func loadCityOpenWorldImpactResponseRecords(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, baselineTick int64,
	includeCurrentTick bool,
	limit int,
) ([]cityOpenWorldImpactResponseRecord, error) {
	if limit < 1 {
		return []cityOpenWorldImpactResponseRecord{}, nil
	}
	comparison := "<"
	if includeCurrentTick {
		comparison = "="
	}
	query := `
SELECT response.id, response.code, response.actor_id, actor.code,
       response.service_code, response.outcome, response.requested_tick,
       response.queued_tick, response.dispatched_tick, response.resolved_tick,
       response.response_ticks, response.delivered_units, source_fact.id,
       source_fact.tick, source_fact.sequence, request_value.requested_units,
       response.metadata
FROM city_open_world_service_responses response
JOIN city_open_world_service_requests request_value
  ON request_value.id = response.request_id AND request_value.world_id = response.world_id
JOIN city_open_world_actors actor
  ON actor.id = response.actor_id AND actor.world_id = response.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = response.source_fact_id AND source_fact.world_id = response.world_id
WHERE response.world_id = $1
  AND response.resolved_tick ` + comparison + ` $2
  AND response.resolved_tick > $3
  AND NOT EXISTS (
      SELECT 1 FROM city_open_world_impact_effects effect_value
      WHERE effect_value.world_id = response.world_id
        AND effect_value.source_response_id = response.id
  )
ORDER BY response.resolved_tick ASC, response.code ASC
LIMIT $4`
	rows, err := tx.QueryContext(ctx, query, worldID, targetTick, baselineTick, limit)
	if err != nil {
		return nil, fmt.Errorf("load V8 response impacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]cityOpenWorldImpactResponseRecord, 0)
	for rows.Next() {
		item := cityOpenWorldImpactResponseRecord{}
		var queuedTick, dispatchedTick sql.NullInt64
		if err = rows.Scan(
			&item.id, &item.response.Code, &item.actorID, &item.response.ActorCode,
			&item.response.ServiceCode, &item.response.Outcome, &item.response.RequestedTick,
			&queuedTick, &dispatchedTick, &item.response.ResolvedTick,
			&item.response.ResponseTicks, &item.response.DeliveredUnits, &item.sourceFactID,
			&item.response.SourceFact.Tick, &item.response.SourceFact.Sequence, &item.requested,
			&item.response.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan V8 response impact: %w", err)
		}
		if queuedTick.Valid {
			item.response.QueuedTick = cityOpenWorldInt64Pointer(queuedTick.Int64)
		}
		if dispatchedTick.Valid {
			item.response.DispatchedTick = cityOpenWorldInt64Pointer(dispatchedTick.Int64)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V8 response impacts: %w", err)
	}
	return items, nil
}

func updateCityOpenWorldImpactProfile(
	ctx context.Context,
	tx *sql.Tx,
	worldID, effectDelta, appliedDelta, metricDelta int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_impact_profiles
SET effect_count = effect_count + $2,
    applied_count = applied_count + $3,
    metric_count = metric_count + $4,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID, effectDelta, appliedDelta, metricDelta)
	if err != nil {
		return fmt.Errorf("update V8 impact profile: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_profile"})
	}
	return nil
}

// scheduleCityOpenWorldV8ServiceImpacts is deliberately invoked twice per
// tick: once before applications for overdue/past responses, then after the
// V7 reducer for responses resolved in the current tick. The latter can never
// run through the application pass until the following tick.
func scheduleCityOpenWorldV8ServiceImpacts(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	includeCurrentTick bool,
) ([]worldRuntimeAutomaticEvent, error) {
	policy, err := loadCityOpenWorldImpactPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return nil, err
	}
	catalog, err := loadCityOpenWorldImpactCatalogMap(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	responses, err := loadCityOpenWorldImpactResponseRecords(
		ctx, tx, worldID, targetTick, policy.BaselineTick, includeCurrentTick,
		policy.MaximumSchedulesPerTick,
	)
	if err != nil {
		return nil, err
	}
	events := make([]worldRuntimeAutomaticEvent, 0, len(responses))
	for _, response := range responses {
		entry, found := catalog[cityOpenWorldImpactCatalogKey(response.response.ServiceCode, response.response.Outcome)]
		if !found {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_catalog_binding"})
		}
		sourceUnits := response.response.DeliveredUnits
		if response.response.Outcome == "expired" {
			sourceUnits = response.requested
		}
		if sourceUnits < 1 || sourceUnits > cityOpenWorldServiceMaximumUnitsPerRequest {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_source_units"})
		}
		delta := sourceUnits * entry.DeltaUnitsPerSourceUnit
		if delta == 0 || (entry.DeltaUnitsPerSourceUnit > 0 && delta < 0) ||
			(entry.DeltaUnitsPerSourceUnit < 0 && delta > 0) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_delta"})
		}
		metadata, marshalErr := json.Marshal(map[string]any{
			"schema_version":      cityOpenWorldImpactSchemaVersion,
			"source_contract":     cityOpenWorldImpactSourceContractVersion,
			"delivery_contract":   cityOpenWorldImpactDeliveryContractVersion,
			"source_outcome":      response.response.Outcome,
			"source_service_code": response.response.ServiceCode,
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal V8 impact effect metadata: %w", marshalErr)
		}
		result, insertErr := tx.ExecContext(ctx, `
INSERT INTO city_open_world_impact_effects
    (world_id, code, source_response_id, source_fact_id, catalog_code,
     target_actor_id, target_domain, target_code, metric_code, source_units,
     delta_units, scheduled_tick, effective_tick, status, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        'scheduled', 1, $14::jsonb)
ON CONFLICT (world_id, source_response_id, catalog_code) DO NOTHING`,
			worldID, cityOpenWorldImpactEffectCode(response.response.Code, entry.Code), response.id,
			response.sourceFactID, entry.Code, response.actorID, entry.TargetDomain,
			response.response.ActorCode, entry.MetricCode, sourceUnits, delta,
			response.response.ResolvedTick, response.response.ResolvedTick+1, []byte(metadata))
		if insertErr != nil {
			return nil, fmt.Errorf("insert V8 impact effect for %s: %w", response.response.Code, insertErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return nil, rowsErr
		}
		if rows == 0 {
			continue
		}
		if rows != 1 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_schedule"})
		}
		if err = updateCityOpenWorldImpactProfile(ctx, tx, worldID, 1, 0, 0); err != nil {
			return nil, err
		}
		events = append(events, worldRuntimeAutomaticEvent{
			eventType: "city.open_world.impact_scheduled",
			payload: map[string]any{
				"source_response_code": response.response.Code,
				"impact_code":          entry.Code,
				"effective_tick":       response.response.ResolvedTick + 1,
			},
		})
	}
	return events, nil
}

func loadCityOpenWorldImpactEffectIDs(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, targetTick int64,
	limit int,
) ([]int64, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id
FROM city_open_world_impact_effects
WHERE world_id = $1 AND status = 'scheduled' AND effective_tick <= $2
ORDER BY effective_tick ASC, code ASC
LIMIT $3`, worldID, targetTick, limit)
	if err != nil {
		return nil, fmt.Errorf("load due V8 impact effects: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due V8 impact effects: %w", err)
	}
	return ids, nil
}

func loadCityOpenWorldImpactEffectForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID, effectID int64,
) (*cityOpenWorldImpactEffectRecord, error) {
	record := &cityOpenWorldImpactEffectRecord{id: effectID}
	var appliedTick, applicationTick, applicationSequence sql.NullInt64
	var beforeUnits, afterUnits sql.NullInt64
	err := tx.QueryRowContext(ctx, `
SELECT effect_value.source_fact_id, effect_value.target_actor_id,
       effect_value.source_response_id, effect_value.code, response.code,
       source_fact.tick, source_fact.sequence, effect_value.catalog_code,
       effect_value.target_domain, effect_value.target_code, effect_value.metric_code,
       effect_value.source_units, effect_value.delta_units, effect_value.scheduled_tick,
       effect_value.effective_tick, effect_value.status, effect_value.applied_tick,
       application_fact.tick, application_fact.sequence, effect_value.before_units,
       effect_value.after_units, effect_value.version, effect_value.metadata
FROM city_open_world_impact_effects effect_value
JOIN city_open_world_service_responses response
  ON response.id = effect_value.source_response_id AND response.world_id = effect_value.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = effect_value.source_fact_id AND source_fact.world_id = effect_value.world_id
LEFT JOIN city_open_world_runtime_facts application_fact
  ON application_fact.id = effect_value.application_fact_id AND application_fact.world_id = effect_value.world_id
WHERE effect_value.world_id = $1 AND effect_value.id = $2
FOR UPDATE OF effect_value`, worldID, effectID).Scan(
		&record.sourceFactID, &record.targetActorID, &record.responseID,
		&record.effect.Code, &record.effect.SourceResponseCode,
		&record.effect.SourceFact.Tick, &record.effect.SourceFact.Sequence,
		&record.effect.CatalogCode, &record.effect.TargetDomain, &record.effect.TargetCode,
		&record.effect.MetricCode, &record.effect.SourceUnits, &record.effect.DeltaUnits,
		&record.effect.ScheduledTick, &record.effect.EffectiveTick, &record.effect.Status,
		&appliedTick, &applicationTick, &applicationSequence, &beforeUnits, &afterUnits,
		&record.effect.Version, &record.effect.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock V8 impact effect: %w", err)
	}
	if appliedTick.Valid {
		record.effect.AppliedTick = cityOpenWorldInt64Pointer(appliedTick.Int64)
	}
	if applicationTick.Valid {
		record.effect.ApplicationFact = &CityOpenWorldRuntimeFactRef{Tick: applicationTick.Int64, Sequence: applicationSequence.Int64}
	}
	record.effect.BeforeUnits = nullInt64Pointer(beforeUnits)
	record.effect.AfterUnits = nullInt64Pointer(afterUnits)
	return record, nil
}

func loadCityOpenWorldImpactMetricForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	targetDomain, targetCode, metricCode string,
) (*cityOpenWorldImpactMetricRecord, error) {
	record := &cityOpenWorldImpactMetricRecord{}
	err := tx.QueryRowContext(ctx, `
SELECT value_units, version
FROM city_open_world_impact_metrics
WHERE world_id = $1 AND target_domain = $2 AND target_code = $3 AND metric_code = $4
FOR UPDATE`, worldID, targetDomain, targetCode, metricCode).Scan(&record.value, &record.version)
	if errors.Is(err, sql.ErrNoRows) {
		return record, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock V8 impact metric: %w", err)
	}
	record.exists = true
	return record, nil
}

func applyCityOpenWorldImpactMetric(
	ctx context.Context,
	tx *sql.Tx,
	worldID, effectID, targetTick int64,
	effect CityOpenWorldImpactEffect,
	metric *cityOpenWorldImpactMetricRecord,
	applicationFactID int64,
) (int64, int64, error) {
	if metric == nil || applicationFactID <= 0 {
		return 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_metric"})
	}
	before := metric.value
	after := cityOpenWorldRuntimeSaturatingAdd(before, effect.DeltaUnits)
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldImpactSchemaVersion,
		"accumulator":    "saturating_add",
	})
	if err != nil {
		return 0, 0, fmt.Errorf("marshal V8 impact metric metadata: %w", err)
	}
	if metric.exists {
		result, updateErr := tx.ExecContext(ctx, `
UPDATE city_open_world_impact_metrics
SET value_units = $5, last_applied_tick = $6, last_effect_id = $7,
    version = version + 1, metadata = $8::jsonb, updated_at = NOW()
WHERE world_id = $1 AND target_domain = $2 AND target_code = $3 AND metric_code = $4
  AND version = $9`,
			worldID, effect.TargetDomain, effect.TargetCode, effect.MetricCode, after,
			targetTick, effectID, []byte(metadata), metric.version)
		if updateErr != nil {
			return 0, 0, fmt.Errorf("update V8 impact metric: %w", updateErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil || rows != 1 {
			return 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_metric_version"})
		}
		return before, after, nil
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_impact_metrics
    (world_id, target_domain, target_code, metric_code, value_units,
     last_applied_tick, last_effect_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8::jsonb)`,
		worldID, effect.TargetDomain, effect.TargetCode, effect.MetricCode, after,
		targetTick, effectID, []byte(metadata)); err != nil {
		return 0, 0, fmt.Errorf("insert V8 impact metric: %w", err)
	}
	return before, after, nil
}

func advanceCityOpenWorldV8ImpactEffects(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence int64,
) (cityOpenWorldRuntimeAutomaticExecution, error) {
	execution := cityOpenWorldRuntimeAutomaticExecution{
		facts: make([]CityOpenWorldRuntimeFact, 0), effects: make([]CityOpenWorldRuntimeEffect, 0),
		events: make([]worldRuntimeAutomaticEvent, 0), nextFactSeq: factSequence, nextEffectSeq: effectSequence,
	}
	policy, err := loadCityOpenWorldImpactPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	ids, err := loadCityOpenWorldImpactEffectIDs(ctx, tx, worldID, targetTick, policy.MaximumSchedulesPerTick)
	if err != nil {
		return execution, err
	}
	for _, id := range ids {
		record, loadErr := loadCityOpenWorldImpactEffectForUpdate(ctx, tx, worldID, id)
		if loadErr != nil || record == nil {
			if loadErr != nil {
				return execution, loadErr
			}
			continue
		}
		if record.effect.Status != "scheduled" || record.effect.EffectiveTick > targetTick ||
			record.effect.TargetDomain != cityOpenWorldImpactTargetDomainActor || record.targetActorID <= 0 {
			continue
		}
		metric, metricErr := loadCityOpenWorldImpactMetricForUpdate(
			ctx, tx, worldID, record.effect.TargetDomain, record.effect.TargetCode, record.effect.MetricCode,
		)
		if metricErr != nil {
			return execution, metricErr
		}
		payload, marshalErr := json.Marshal(map[string]any{
			"schema_version":       cityOpenWorldImpactSchemaVersion,
			"impact_effect_code":   record.effect.Code,
			"source_response_code": record.effect.SourceResponseCode,
			"source_fact":          record.effect.SourceFact,
			"catalog_code":         record.effect.CatalogCode,
			"target_domain":        record.effect.TargetDomain,
			"target_code":          record.effect.TargetCode,
			"metric_code":          record.effect.MetricCode,
			"source_units":         record.effect.SourceUnits,
			"delta_units":          record.effect.DeltaUnits,
			"scheduled_tick":       record.effect.ScheduledTick,
			"effective_tick":       record.effect.EffectiveTick,
		})
		if marshalErr != nil {
			return execution, fmt.Errorf("marshal V8 impact application fact: %w", marshalErr)
		}
		fact, factErr := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
			worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
			parentFactID: &record.sourceFactID, actorID: &record.targetActorID,
			factType: CityOpenWorldRuntimeFactImpactApplied, payload: payload,
		})
		if factErr != nil {
			return execution, factErr
		}
		before, after, applyErr := applyCityOpenWorldImpactMetric(
			ctx, tx, worldID, record.id, targetTick, record.effect, metric, fact.id,
		)
		if applyErr != nil {
			return execution, applyErr
		}
		payload, marshalErr = json.Marshal(map[string]any{
			"schema_version":       cityOpenWorldImpactSchemaVersion,
			"impact_effect_code":   record.effect.Code,
			"source_response_code": record.effect.SourceResponseCode,
			"target_domain":        record.effect.TargetDomain,
			"target_code":          record.effect.TargetCode,
			"metric_code":          record.effect.MetricCode,
			"before_units":         before,
			"delta_units":          record.effect.DeltaUnits,
			"after_units":          after,
		})
		if marshalErr != nil {
			return execution, fmt.Errorf("marshal V8 impact runtime effect: %w", marshalErr)
		}
		targetCode := record.effect.TargetCode
		metricCode := record.effect.MetricCode
		beforeCopy, deltaCopy, afterCopy := before, record.effect.DeltaUnits, after
		runtimeEffect, runtimeErr := insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
			worldID: worldID, tick: targetTick, sequence: execution.nextEffectSeq,
			sourceFact: fact, operationIndex: 1, effectType: "impact.metric.applied",
			targetActorID: &record.targetActorID, targetActorCode: &targetCode,
			targetKey: &metricCode, beforeUnits: &beforeCopy, deltaUnits: &deltaCopy,
			afterUnits: &afterCopy, payload: payload,
		})
		if runtimeErr != nil {
			return execution, runtimeErr
		}
		result, updateErr := tx.ExecContext(ctx, `
UPDATE city_open_world_impact_effects
SET status = 'applied', applied_tick = $3, application_fact_id = $4,
    before_units = $5, after_units = $6, version = version + 1,
    updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'scheduled' AND version = $7`,
			worldID, record.id, targetTick, fact.id, before, after, record.effect.Version)
		if updateErr != nil {
			return execution, fmt.Errorf("apply V8 impact effect %s: %w", record.effect.Code, updateErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil || rows != 1 {
			return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_effect_version"})
		}
		if postErr := postCityOpenWorldRuntimeFact(ctx, tx, fact.id); postErr != nil {
			return execution, postErr
		}
		metricCountDelta := int64(0)
		if !metric.exists {
			metricCountDelta = 1
		}
		if err = updateCityOpenWorldImpactProfile(ctx, tx, worldID, 0, 1, metricCountDelta); err != nil {
			return execution, err
		}
		execution.facts = append(execution.facts, fact.fact)
		execution.effects = append(execution.effects, runtimeEffect)
		execution.nextFactSeq++
		execution.nextEffectSeq++
		execution.events = append(execution.events, worldRuntimeAutomaticEvent{
			eventType: "city.open_world.impact_applied",
			payload: map[string]any{
				"impact_effect_code":   record.effect.Code,
				"source_response_code": record.effect.SourceResponseCode,
				"target_code":          record.effect.TargetCode,
				"metric_code":          record.effect.MetricCode,
				"after_units":          after,
			},
		})
	}
	if len(execution.facts) > 0 {
		if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, int64(len(execution.facts)), int64(len(execution.effects)), 0); err != nil {
			return execution, err
		}
	}
	return execution, nil
}
