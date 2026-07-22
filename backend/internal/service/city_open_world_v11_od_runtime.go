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

const (
	cityOpenWorldRuntimeFactMobilityODGenerated  = "system.mobility.od.generated"
	cityOpenWorldRuntimeFactMobilityODSuppressed = "system.mobility.od.suppressed"
	cityOpenWorldRuntimeFactMobilityODCycleClose = "system.mobility.od.cycle.closed"
)

type cityOpenWorldMobilityODSourceRecord struct {
	id          int64
	actorID     int64
	actorStatus string
	source      CityOpenWorldMobilityODSource
}

func loadCityOpenWorldMobilityODPolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldMobilityODPolicy, error) {
	policy := &CityOpenWorldMobilityODPolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       generation_contract, metric_contract, cycle_ticks, maximum_generations_tick,
       source_count, generated_count, suppressed_count, metric_count, revision, metadata
FROM city_open_world_mobility_od_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash, &policy.BaselineTick,
		&policy.GenerationContract, &policy.MetricContract, &policy.CycleTicks,
		&policy.MaximumGenerationsTick, &policy.SourceCount, &policy.GeneratedCount,
		&policy.SuppressedCount, &policy.MetricCount, &policy.Revision, &policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V11 OD profile: %w", err)
	}
	if err = validateCityOpenWorldMobilityODPolicy(*policy); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_profile"}).WithCause(err)
	}
	return policy, nil
}

func updateCityOpenWorldMobilityODPolicy(
	ctx context.Context,
	tx *sql.Tx,
	worldID, generatedDelta, suppressedDelta, metricDelta int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_od_profiles
SET generated_count = generated_count + $2,
    suppressed_count = suppressed_count + $3,
    metric_count = metric_count + $4,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID, generatedDelta, suppressedDelta, metricDelta)
	if err != nil {
		return fmt.Errorf("update V11 OD profile: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_profile"})
	}
	return nil
}

func cityOpenWorldMobilityODDemandCode(sourceCode string, targetTick int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("mobility.od.demand.v1\x00%s\x00%d", sourceCode, targetTick)))
	return "mobility.demand.od." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldMobilityODCycleWindow(
	policy CityOpenWorldMobilityODPolicy,
	targetTick int64,
) (int64, int64, bool) {
	if targetTick <= policy.BaselineTick+policy.CycleTicks {
		return 0, 0, false
	}
	cycleEnd := targetTick - 1
	if (cycleEnd-policy.BaselineTick)%policy.CycleTicks != 0 {
		return 0, 0, false
	}
	return cycleEnd - policy.CycleTicks + 1, cycleEnd, true
}

// advanceCityOpenWorldV11MobilityOD runs before V9 scheduling. New automatic
// demands are deliberately stamped for this tick and V9 only sees them on the
// next tick, preserving the same causal boundary as player-submitted demand.
func advanceCityOpenWorldV11MobilityOD(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
) (cityOpenWorldRuntimeAutomaticExecution, error) {
	execution := cityOpenWorldRuntimeAutomaticExecution{
		facts: make([]CityOpenWorldRuntimeFact, 0), effects: make([]CityOpenWorldRuntimeEffect, 0),
		events: make([]worldRuntimeAutomaticEvent, 0), nextFactSeq: factSequence,
	}
	policy, err := loadCityOpenWorldMobilityODPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldMobilityODWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = closeCityOpenWorldMobilityODCycle(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if err = generateCityOpenWorldMobilityODDemands(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if len(execution.facts) > 0 {
		if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, int64(len(execution.facts)), 0, 0); err != nil {
			return execution, err
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_mobility_od_foundation($1)`, worldID); err != nil {
		return execution, fmt.Errorf("validate V11 OD foundation after advancement: %w", err)
	}
	return execution, nil
}

func closeCityOpenWorldMobilityODCycle(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldMobilityODPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_cycle"})
	}
	cycleStart, cycleEnd, due := cityOpenWorldMobilityODCycleWindow(*policy, targetTick)
	if !due {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM city_open_world_mobility_od_cycle_metrics
    WHERE world_id = $1 AND cycle_start_tick = $2
)`, worldID, cycleStart).Scan(&exists); err != nil {
		return fmt.Errorf("check V11 OD cycle close: %w", err)
	}
	if exists {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_cycle_duplicate"})
	}
	metric, err := collectCityOpenWorldMobilityODCycleMetric(ctx, tx, worldID, cycleStart, cycleEnd, targetTick)
	if err != nil {
		return err
	}
	metric.SourceFact = CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: execution.nextFactSeq}
	payload, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldMobilityODSchemaVersion,
		"cycle_start_tick": cycleStart,
		"cycle_end_tick":   cycleEnd,
		"metric":           metric,
		"contract":         cityOpenWorldMobilityODMetricContract,
	})
	if err != nil {
		return fmt.Errorf("marshal V11 OD cycle-close fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		factType: cityOpenWorldRuntimeFactMobilityODCycleClose, payload: payload,
	})
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldMobilityODSchemaVersion,
		"event_scope":    "cycle_occurrence_window_v1",
	})
	if err != nil {
		return fmt.Errorf("marshal V11 OD cycle metric metadata: %w", err)
	}
	metric.Metadata = metadata
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_od_cycle_metrics
    (world_id, cycle_start_tick, cycle_end_tick, closed_tick, source_fact_id,
     generated_count, suppressed_count, network_requested_count,
     network_scheduled_count, network_completed_count, network_expired_count,
     pending_demand_count, arrival_landed_count, arrival_blocked_count,
     arrival_failed_count, travel_ticks_total, congestion_ticks_total,
     peak_occupancy_milli, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19::jsonb)`,
		worldID, metric.CycleStartTick, metric.CycleEndTick, metric.ClosedTick, fact.id,
		metric.GeneratedCount, metric.SuppressedCount, metric.NetworkRequested,
		metric.NetworkScheduled, metric.NetworkCompleted, metric.NetworkExpired,
		metric.PendingDemandCount, metric.ArrivalLanded, metric.ArrivalBlocked,
		metric.ArrivalFailed, metric.TravelTicksTotal, metric.CongestionTicksTotal,
		metric.PeakOccupancyMilli, []byte(metric.Metadata)); err != nil {
		return fmt.Errorf("insert V11 OD cycle metric: %w", err)
	}
	if err = updateCityOpenWorldMobilityODPolicy(ctx, tx, worldID, 0, 0, 1); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.mobility_od_cycle_closed", payload: map[string]any{
		"cycle_start_tick": cycleStart, "cycle_end_tick": cycleEnd,
	}})
	return nil
}

func collectCityOpenWorldMobilityODCycleMetric(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, cycleStart, cycleEnd, closedTick int64,
) (CityOpenWorldMobilityODCycleMetric, error) {
	metric := CityOpenWorldMobilityODCycleMetric{
		CycleStartTick: cycleStart, CycleEndTick: cycleEnd, ClosedTick: closedTick,
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE fact_type = $4),
       COUNT(*) FILTER (WHERE fact_type = $5)
FROM city_open_world_runtime_facts
WHERE world_id = $1 AND tick BETWEEN $2 AND $3`,
		worldID, cycleStart, cycleEnd,
		cityOpenWorldRuntimeFactMobilityODGenerated, cityOpenWorldRuntimeFactMobilityODSuppressed,
	).Scan(&metric.GeneratedCount, &metric.SuppressedCount); err != nil {
		return metric, fmt.Errorf("collect V11 OD source events: %w", err)
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE requested_tick BETWEEN $2 AND $3),
       COUNT(*) FILTER (WHERE status IN ('pending', 'scheduled'))
FROM city_open_world_mobility_demands
WHERE world_id = $1`, worldID, cycleStart, cycleEnd).Scan(
		&metric.NetworkRequested, &metric.PendingDemandCount,
	); err != nil {
		return metric, fmt.Errorf("collect V11 OD demand metrics: %w", err)
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE scheduled_tick BETWEEN $2 AND $3),
       COUNT(*) FILTER (WHERE completed_tick BETWEEN $2 AND $3),
       COUNT(*) FILTER (WHERE expired_tick BETWEEN $2 AND $3)
FROM city_open_world_mobility_demands
WHERE world_id = $1`, worldID, cycleStart, cycleEnd).Scan(
		&metric.NetworkScheduled, &metric.NetworkCompleted, &metric.NetworkExpired,
	); err != nil {
		return metric, fmt.Errorf("collect V11 OD lifecycle metrics: %w", err)
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE fact_type = $4),
       COUNT(*) FILTER (WHERE fact_type = $5),
       COUNT(*) FILTER (WHERE fact_type = $6)
FROM city_open_world_runtime_facts
WHERE world_id = $1 AND tick BETWEEN $2 AND $3`,
		worldID, cycleStart, cycleEnd,
		CityOpenWorldRuntimeFactMobilityArrivalLanded,
		CityOpenWorldRuntimeFactMobilityArrivalBlocked,
		CityOpenWorldRuntimeFactMobilityArrivalFailed,
	).Scan(&metric.ArrivalLanded, &metric.ArrivalBlocked, &metric.ArrivalFailed); err != nil {
		return metric, fmt.Errorf("collect V11 OD arrival metrics: %w", err)
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT COALESCE(SUM(base_travel_ticks + congestion_delay_ticks), 0),
       COALESCE(SUM(congestion_delay_ticks), 0)
FROM city_open_world_mobility_routes
WHERE world_id = $1 AND departure_tick BETWEEN $2 AND $3`, worldID, cycleStart, cycleEnd).Scan(
		&metric.TravelTicksTotal, &metric.CongestionTicksTotal,
	); err != nil {
		return metric, fmt.Errorf("collect V11 OD route metrics: %w", err)
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT COALESCE(MAX(allocation.occupancy_milli), 0)
FROM city_open_world_mobility_allocations allocation
JOIN city_open_world_mobility_routes route
  ON route.id = allocation.route_id AND route.world_id = allocation.world_id
WHERE allocation.world_id = $1 AND route.departure_tick BETWEEN $2 AND $3`, worldID, cycleStart, cycleEnd).Scan(
		&metric.PeakOccupancyMilli,
	); err != nil {
		return metric, fmt.Errorf("collect V11 OD occupancy metrics: %w", err)
	}
	return metric, nil
}

func generateCityOpenWorldMobilityODDemands(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldMobilityODPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_generation"})
	}
	rows, err := tx.QueryContext(ctx, `
SELECT source.id, source.code, source.source_kind, actor.id, actor.code, actor.status,
       source.destination_facility_code, source.destination_hub_code,
       source.mode_code, source.purpose_code, source.requested_units,
       source.status, source.period_ticks, source.phase_offset,
       source.next_due_tick, source.last_transition_tick, source.generated_count,
       source.suppressed_count, source.version, source.metadata
FROM city_open_world_mobility_od_sources source
JOIN city_open_world_actors actor
  ON actor.id = source.actor_id AND actor.world_id = source.world_id
WHERE source.world_id = $1
  AND source.status = 'active'
  AND source.next_due_tick <= $2
ORDER BY source.next_due_tick ASC, source.code ASC
LIMIT $3
FOR UPDATE OF source, actor`, worldID, targetTick, policy.MaximumGenerationsTick)
	if err != nil {
		return fmt.Errorf("load V11 OD due sources: %w", err)
	}
	records := make([]cityOpenWorldMobilityODSourceRecord, 0)
	for rows.Next() {
		record := cityOpenWorldMobilityODSourceRecord{}
		if err = rows.Scan(
			&record.id, &record.source.Code, &record.source.SourceKind, &record.actorID,
			&record.source.ActorCode, &record.actorStatus, &record.source.DestinationFacilityCode,
			&record.source.DestinationHubCode, &record.source.ModeCode,
			&record.source.PurposeCode, &record.source.RequestedUnits, &record.source.Status,
			&record.source.PeriodTicks, &record.source.PhaseOffset, &record.source.NextDueTick,
			&record.source.LastTransitionTick, &record.source.GeneratedCount,
			&record.source.SuppressedCount, &record.source.Version, &record.source.Metadata,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan V11 OD due source: %w", err)
		}
		records = append(records, record)
	}
	if err = closeCityRows(rows, "iterate V11 OD due sources"); err != nil {
		return err
	}
	for _, record := range records {
		if err = generateCityOpenWorldMobilityODDemand(ctx, tx, worldID, targetTick, policy, &record, execution); err != nil {
			return err
		}
	}
	return nil
}

func generateCityOpenWorldMobilityODDemand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldMobilityODPolicy,
	record *cityOpenWorldMobilityODSourceRecord,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || record == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_source"})
	}
	if record.actorStatus != "active" {
		return suppressCityOpenWorldMobilityODSource(ctx, tx, worldID, targetTick, record, "actor_inactive", execution)
	}
	// V13 owns a complete residence↔work source pair for this actor. Keep the
	// V11 row and its cadence as immutable audit evidence, but do not let the
	// generic facility-visit adapter create a duplicate trip in the same world.
	superseded, err := cityOpenWorldMobilityODSourceSupersededByCommute(ctx, tx, worldID, record.actorID)
	if err != nil {
		return err
	}
	if superseded {
		return suppressCityOpenWorldMobilityODSource(ctx, tx, worldID, targetTick, record, "superseded_by_commute_source", execution)
	}
	location, err := loadCityOpenWorldActorLocationByCode(ctx, tx, worldID, record.source.ActorCode)
	if err != nil {
		return err
	}
	if location == nil || !cityOpenWorldMobilityArrivalLocationValid(*location, record.source.ActorCode) {
		return suppressCityOpenWorldMobilityODSource(ctx, tx, worldID, targetTick, record, "actor_location_invalid", execution)
	}
	var navigationBusy, mobilityBusy bool
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM city_open_world_actor_navigation_intents
    WHERE world_id = $1 AND actor_id = $2 AND status = 'active'
)`, worldID, record.actorID).Scan(&navigationBusy); err != nil {
		return fmt.Errorf("check V11 OD navigation conflict: %w", err)
	}
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM city_open_world_mobility_demands
    WHERE world_id = $1 AND actor_id = $2 AND status IN ('pending', 'scheduled')
)`, worldID, record.actorID).Scan(&mobilityBusy); err != nil {
		return fmt.Errorf("check V11 OD mobility conflict: %w", err)
	}
	if navigationBusy {
		return suppressCityOpenWorldMobilityODSource(ctx, tx, worldID, targetTick, record, "navigation_intent_active", execution)
	}
	if mobilityBusy {
		return suppressCityOpenWorldMobilityODSource(ctx, tx, worldID, targetTick, record, "mobility_demand_active", execution)
	}
	mode, err := loadCityOpenWorldMobilityMode(ctx, tx, worldID, record.source.ModeCode)
	if err != nil {
		return err
	}
	if mode == nil {
		return suppressCityOpenWorldMobilityODSource(ctx, tx, worldID, targetTick, record, "mode_unavailable", execution)
	}
	destination, err := loadCityOpenWorldMobilityHub(ctx, tx, worldID, record.source.DestinationHubCode)
	if err != nil {
		return err
	}
	if destination == nil || destination.HubKind != "facility" || destination.FacilityCode == nil || *destination.FacilityCode != record.source.DestinationFacilityCode {
		return suppressCityOpenWorldMobilityODSource(ctx, tx, worldID, targetTick, record, "destination_unavailable", execution)
	}
	var destinationActive bool
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM city_open_world_facilities facility
    JOIN city_open_world_mobility_hubs hub
      ON hub.world_id = facility.world_id
     AND hub.facility_id = facility.id
     AND hub.facility_code = facility.code
    WHERE facility.world_id = $1
      AND facility.code = $2
      AND facility.state = 'active'
      AND hub.code = $3
      AND hub.hub_kind = 'facility'
)`, worldID, record.source.DestinationFacilityCode, record.source.DestinationHubCode).Scan(&destinationActive); err != nil {
		return fmt.Errorf("verify V11 OD destination availability: %w", err)
	}
	if !destinationActive {
		return suppressCityOpenWorldMobilityODSource(ctx, tx, worldID, targetTick, record, "destination_unavailable", execution)
	}
	origin, err := findNearestCityOpenWorldMobilityZoneHub(ctx, tx, worldID, location.X, location.Y)
	if err != nil {
		return err
	}
	if origin == nil || origin.Code == destination.Code {
		return suppressCityOpenWorldMobilityODSource(ctx, tx, worldID, targetTick, record, "origin_unavailable", execution)
	}
	if err = generateCityOpenWorldMobilityODDemandFromSource(
		ctx, tx, worldID, targetTick, policy, record, *location, origin, destination, *mode, execution,
	); err != nil {
		return err
	}
	return nil
}

func generateCityOpenWorldMobilityODDemandFromSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldMobilityODPolicy,
	record *cityOpenWorldMobilityODSourceRecord,
	location CityOpenWorldActorLocation,
	origin, destination *CityOpenWorldMobilityHub,
	mode CityOpenWorldMobilityMode,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || record == nil || origin == nil || destination == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_generation"})
	}
	demandCode := cityOpenWorldMobilityODDemandCode(record.source.Code, targetTick)
	generationPayload, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldMobilityODSchemaVersion,
		"source_code":    record.source.Code, "source_kind": record.source.SourceKind,
		"actor_code": record.source.ActorCode, "demand_code": demandCode,
		"destination_facility_code": record.source.DestinationFacilityCode,
	})
	if err != nil {
		return fmt.Errorf("marshal V11 OD generation fact: %w", err)
	}
	generationFact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq, actorID: &record.actorID,
		factType: cityOpenWorldRuntimeFactMobilityODGenerated, payload: generationPayload,
	})
	if err != nil {
		return err
	}
	mobilityPolicy, err := loadCityOpenWorldMobilityPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return err
	}
	deadline := targetTick + mobilityPolicy.MaximumWaitTicks
	requestPayload, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldMobilitySchemaVersion,
		"demand_code":    demandCode, "actor_code": record.source.ActorCode,
		"source_hub_code": origin.Code, "destination_hub_code": destination.Code,
		"mode_code": mode.Code, "purpose_code": record.source.PurposeCode,
		"requested_units":         record.source.RequestedUnits,
		"earliest_departure_tick": targetTick + 1,
		"deadline_tick":           deadline,
		"od_source_code":          record.source.Code,
	})
	if err != nil {
		return fmt.Errorf("marshal V11 OD mobility request fact: %w", err)
	}
	requestFact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq + 1,
		parentFactID: &generationFact.id, actorID: &record.actorID,
		factType: CityOpenWorldRuntimeFactMobilityRequested, payload: requestPayload,
	})
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldMobilitySchemaVersion,
		"origin":         "od_source", "od_contract": cityOpenWorldMobilityODGenerationContract,
		"od_source_code": record.source.Code,
		"arrival_bridge": map[string]any{
			"contract":        "captured_request_location_v1",
			"expected_origin": location,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal V11 OD mobility metadata: %w", err)
	}
	var demandID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_mobility_demands
    (world_id, code, actor_id, source_hub_code, destination_hub_code, mode_code,
     purpose_code, requested_units, requested_tick, earliest_departure_tick,
     deadline_tick, status, source_fact_id, last_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
        'pending', $12, $12, 1, $13::jsonb)
RETURNING id`, worldID, demandCode, record.actorID, origin.Code, destination.Code,
		mode.Code, record.source.PurposeCode, record.source.RequestedUnits, targetTick,
		targetTick+1, deadline, requestFact.id, []byte(metadata)).Scan(&demandID); err != nil {
		return fmt.Errorf("insert V11 OD mobility demand %s: %w", demandCode, err)
	}
	if demandID <= 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_demand"})
	}
	metricCreated, err := updateCityOpenWorldMobilityActorMetric(
		ctx, tx, worldID, record.actorID, record.source.ActorCode, 1, 0, 0, 0, nil, targetTick,
	)
	if err != nil {
		return err
	}
	if err = updateCityOpenWorldMobilityPolicy(ctx, tx, worldID, 1, 0, 0, 0, 0, metricCreated); err != nil {
		return err
	}
	if err = transitionCityOpenWorldMobilityODSource(
		ctx, tx, worldID, targetTick, record, generationFact, true,
	); err != nil {
		return err
	}
	if err = updateCityOpenWorldMobilityODPolicy(ctx, tx, worldID, 1, 0, 0); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, generationFact.id); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, requestFact.id); err != nil {
		return err
	}
	execution.facts = append(execution.facts, generationFact.fact, requestFact.fact)
	execution.nextFactSeq += 2
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.mobility_od_generated", payload: map[string]any{
		"source_code": record.source.Code, "demand_code": demandCode,
		"actor_code": record.source.ActorCode,
	}})
	return nil
}

func suppressCityOpenWorldMobilityODSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldMobilityODSourceRecord,
	reason string,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if record == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_suppression"})
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldMobilityODSchemaVersion,
		"source_code":    record.source.Code, "actor_code": record.source.ActorCode,
		"reason": reason,
	})
	if err != nil {
		return fmt.Errorf("marshal V11 OD suppression fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq, actorID: &record.actorID,
		factType: cityOpenWorldRuntimeFactMobilityODSuppressed, payload: payload,
	})
	if err != nil {
		return err
	}
	if err = transitionCityOpenWorldMobilityODSource(ctx, tx, worldID, targetTick, record, fact, false); err != nil {
		return err
	}
	if err = updateCityOpenWorldMobilityODPolicy(ctx, tx, worldID, 0, 1, 0); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.mobility_od_suppressed", payload: map[string]any{
		"source_code": record.source.Code, "actor_code": record.source.ActorCode, "reason": reason,
	}})
	return nil
}

func transitionCityOpenWorldMobilityODSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldMobilityODSourceRecord,
	fact *cityOpenWorldRuntimeFactRecord,
	generated bool,
) error {
	if record == nil || fact == nil || record.source.PeriodTicks < 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_source_transition"})
	}
	generatedDelta, suppressedDelta := int64(0), int64(1)
	if generated {
		generatedDelta, suppressedDelta = 1, 0
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_od_sources
SET next_due_tick = $4,
    last_transition_tick = $3,
    last_fact_id = $5,
    generated_count = generated_count + $6,
    suppressed_count = suppressed_count + $7,
    version = version + 1,
    updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'active'`,
		worldID, record.id, targetTick, targetTick+record.source.PeriodTicks, fact.id,
		generatedDelta, suppressedDelta)
	if err != nil {
		return fmt.Errorf("transition V11 OD source %s: %w", record.source.Code, err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_source_transition"})
	}
	return nil
}
