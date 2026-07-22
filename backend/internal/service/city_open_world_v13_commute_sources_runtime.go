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
	cityOpenWorldRuntimeFactCommuteSourceGenerated  = "system.commute.source.generated"
	cityOpenWorldRuntimeFactCommuteSourceSuppressed = "system.commute.source.suppressed"
	cityOpenWorldRuntimeFactCommuteSourceCycleClose = "system.commute.source.cycle.closed"

	cityOpenWorldCommuteSourceReasonActorInactive          = "actor_inactive"
	cityOpenWorldCommuteSourceReasonEmploymentRoleInactive = "employment_role_inactive"
	cityOpenWorldCommuteSourceReasonOriginFacility         = "origin_facility_unavailable"
	cityOpenWorldCommuteSourceReasonDestinationFacility    = "destination_facility_unavailable"
	cityOpenWorldCommuteSourceReasonActorLocation          = "actor_location_invalid"
	cityOpenWorldCommuteSourceReasonExpectedOrigin         = "expected_origin_unavailable"
	cityOpenWorldCommuteSourceReasonNavigationBusy         = "navigation_intent_active"
	cityOpenWorldCommuteSourceReasonMobilityBusy           = "mobility_demand_active"
	cityOpenWorldCommuteSourceReasonModeUnavailable        = "mode_unavailable"
	cityOpenWorldCommuteSourceReasonOriginHub              = "origin_hub_unavailable"
	cityOpenWorldCommuteSourceReasonDestinationHub         = "destination_hub_unavailable"
)

type cityOpenWorldCommuteSourceRecord struct {
	id          int64
	actorID     int64
	actorStatus string
	source      CityOpenWorldCommuteSource
}

type cityOpenWorldCommuteSourceFacility struct {
	Code             string
	HubCode          string
	BuildingCode     string
	FacilityTypeCode string
	AnchorX          int64
	AnchorY          int64
	AnchorZ          int32
}

func loadCityOpenWorldCommuteSourcePolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldCommuteSourcePolicy, error) {
	policy := &CityOpenWorldCommuteSourcePolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       generation_contract, origin_contract, period_ticks, surface_egress_radius,
       maximum_generations_tick, source_count, generated_count, suppressed_count,
       metric_count, revision, metadata
FROM city_open_world_commute_source_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash, &policy.BaselineTick,
		&policy.GenerationContract, &policy.OriginContract, &policy.PeriodTicks,
		&policy.SurfaceEgressRadius, &policy.MaximumGenerationsTick, &policy.SourceCount,
		&policy.GeneratedCount, &policy.SuppressedCount, &policy.MetricCount,
		&policy.Revision, &policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_source_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V13 commute source profile: %w", err)
	}
	if err = validateCityOpenWorldCommuteSourcePolicy(*policy); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_source_profile"}).WithCause(err)
	}
	return policy, nil
}

func updateCityOpenWorldCommuteSourcePolicy(
	ctx context.Context,
	tx *sql.Tx,
	worldID, generatedDelta, suppressedDelta, metricDelta int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_commute_source_profiles
SET generated_count = generated_count + $2,
    suppressed_count = suppressed_count + $3,
    metric_count = metric_count + $4,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID, generatedDelta, suppressedDelta, metricDelta)
	if err != nil {
		return fmt.Errorf("update V13 commute source profile: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_source_profile"})
	}
	return nil
}

func cityOpenWorldCommuteSourceDemandCode(sourceCode string, targetTick int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("commute.demand.v1\x00%s\x00%d", sourceCode, targetTick)))
	return "mobility.demand.commute." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldCommuteSourceCycleWindow(
	policy CityOpenWorldCommuteSourcePolicy,
	targetTick int64,
) (int64, int64, bool) {
	if targetTick <= policy.BaselineTick+policy.PeriodTicks {
		return 0, 0, false
	}
	cycleEnd := targetTick - 1
	if (cycleEnd-policy.BaselineTick)%policy.PeriodTicks != 0 {
		return 0, 0, false
	}
	return cycleEnd - policy.PeriodTicks + 1, cycleEnd, true
}

// advanceCityOpenWorldV13CommuteSources is deliberately after V11's legacy
// compatibility pass and before V9 scheduling. New demand is therefore never
// scheduled in the generation tick, preserving the system-wide causal edge.
func advanceCityOpenWorldV13CommuteSources(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
) (cityOpenWorldRuntimeAutomaticExecution, error) {
	execution := cityOpenWorldRuntimeAutomaticExecution{
		facts: make([]CityOpenWorldRuntimeFact, 0), effects: make([]CityOpenWorldRuntimeEffect, 0),
		events: make([]worldRuntimeAutomaticEvent, 0), nextFactSeq: factSequence,
	}
	policy, err := loadCityOpenWorldCommuteSourcePolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldCommuteSourceWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = closeCityOpenWorldCommuteSourceCycle(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if err = generateCityOpenWorldCommuteSourceDemands(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if len(execution.facts) > 0 {
		if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, int64(len(execution.facts)), 0, 0); err != nil {
			return execution, err
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_commute_source_foundation($1)`, worldID); err != nil {
		return execution, fmt.Errorf("validate V13 commute source foundation after advancement: %w", err)
	}
	return execution, nil
}

func closeCityOpenWorldCommuteSourceCycle(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldCommuteSourcePolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_cycle"})
	}
	cycleStart, cycleEnd, due := cityOpenWorldCommuteSourceCycleWindow(*policy, targetTick)
	if !due {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM city_open_world_commute_cycle_metrics
    WHERE world_id = $1 AND cycle_start_tick = $2
)`, worldID, cycleStart).Scan(&exists); err != nil {
		return fmt.Errorf("check V13 commute cycle close: %w", err)
	}
	if exists {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_cycle_duplicate"})
	}
	metric, err := collectCityOpenWorldCommuteSourceCycleMetric(ctx, tx, worldID, cycleStart, cycleEnd, targetTick)
	if err != nil {
		return err
	}
	metric.SourceFact = CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: execution.nextFactSeq}
	payload, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldCommuteSourceSchemaVersion,
		"cycle_start_tick": cycleStart,
		"cycle_end_tick":   cycleEnd,
		"metric":           metric,
		"contract":         cityOpenWorldCommuteSourceGenerationContract,
	})
	if err != nil {
		return fmt.Errorf("marshal V13 commute cycle-close fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		factType: cityOpenWorldRuntimeFactCommuteSourceCycleClose, payload: payload,
	})
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldCommuteSourceSchemaVersion,
		"event_scope":    "commute_source_occurrence_window_v1",
	})
	if err != nil {
		return fmt.Errorf("marshal V13 commute cycle metadata: %w", err)
	}
	metric.Metadata = metadata
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_cycle_metrics
    (world_id, cycle_start_tick, cycle_end_tick, closed_tick, source_fact_id,
     outbound_generated_count, outbound_suppressed_count,
     outbound_origin_unavailable_count, return_generated_count,
     return_suppressed_count, return_origin_unavailable_count,
     scheduled_demand_count, completed_demand_count, expired_demand_count,
     pending_demand_count, arrival_landed_count, arrival_blocked_count,
     arrival_failed_count, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19::jsonb)`,
		worldID, metric.CycleStartTick, metric.CycleEndTick, metric.ClosedTick, fact.id,
		metric.OutboundGeneratedCount, metric.OutboundSuppressedCount,
		metric.OutboundOriginUnavailableCount, metric.ReturnGeneratedCount,
		metric.ReturnSuppressedCount, metric.ReturnOriginUnavailableCount,
		metric.ScheduledDemandCount, metric.CompletedDemandCount, metric.ExpiredDemandCount,
		metric.PendingDemandCount, metric.ArrivalLandedCount, metric.ArrivalBlockedCount,
		metric.ArrivalFailedCount, []byte(metric.Metadata)); err != nil {
		return fmt.Errorf("insert V13 commute cycle metric: %w", err)
	}
	if err = updateCityOpenWorldCommuteSourcePolicy(ctx, tx, worldID, 0, 0, 1); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.commute_cycle_closed", payload: map[string]any{
		"cycle_start_tick": cycleStart, "cycle_end_tick": cycleEnd,
	}})
	return nil
}

func collectCityOpenWorldCommuteSourceCycleMetric(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, cycleStart, cycleEnd, closedTick int64,
) (CityOpenWorldCommuteSourceCycleMetric, error) {
	metric := CityOpenWorldCommuteSourceCycleMetric{
		CycleStartTick: cycleStart, CycleEndTick: cycleEnd, ClosedTick: closedTick,
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT
    COUNT(*) FILTER (WHERE fact_type = $4 AND payload->>'direction' = 'outbound'),
    COUNT(*) FILTER (WHERE fact_type = $5 AND payload->>'direction' = 'outbound'),
    COUNT(*) FILTER (WHERE fact_type = $5 AND payload->>'direction' = 'outbound'
                      AND payload->>'reason' = $6),
    COUNT(*) FILTER (WHERE fact_type = $4 AND payload->>'direction' = 'return'),
    COUNT(*) FILTER (WHERE fact_type = $5 AND payload->>'direction' = 'return'),
    COUNT(*) FILTER (WHERE fact_type = $5 AND payload->>'direction' = 'return'
                      AND payload->>'reason' = $6)
FROM city_open_world_runtime_facts
WHERE world_id = $1 AND tick BETWEEN $2 AND $3`,
		worldID, cycleStart, cycleEnd, cityOpenWorldRuntimeFactCommuteSourceGenerated,
		cityOpenWorldRuntimeFactCommuteSourceSuppressed,
		cityOpenWorldCommuteSourceReasonExpectedOrigin,
	).Scan(
		&metric.OutboundGeneratedCount, &metric.OutboundSuppressedCount,
		&metric.OutboundOriginUnavailableCount, &metric.ReturnGeneratedCount,
		&metric.ReturnSuppressedCount, &metric.ReturnOriginUnavailableCount,
	); err != nil {
		return metric, fmt.Errorf("collect V13 commute source events: %w", err)
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE demand.scheduled_tick BETWEEN $2 AND $3),
       COUNT(*) FILTER (WHERE demand.completed_tick BETWEEN $2 AND $3),
       COUNT(*) FILTER (WHERE demand.expired_tick BETWEEN $2 AND $3),
       COUNT(*) FILTER (WHERE demand.status IN ('pending', 'scheduled'))
FROM city_open_world_mobility_demands demand
JOIN city_open_world_commute_sources source
  ON source.world_id = demand.world_id
 AND source.code = demand.metadata->>'commute_source_code'
WHERE demand.world_id = $1`, worldID, cycleStart, cycleEnd).Scan(
		&metric.ScheduledDemandCount, &metric.CompletedDemandCount,
		&metric.ExpiredDemandCount, &metric.PendingDemandCount,
	); err != nil {
		return metric, fmt.Errorf("collect V13 commute demand metrics: %w", err)
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE arrival.landed_tick BETWEEN $2 AND $3),
       COALESCE(SUM(arrival.blocked_attempts) FILTER (WHERE arrival.updated_tick BETWEEN $2 AND $3), 0),
       COUNT(*) FILTER (WHERE arrival.failed_tick BETWEEN $2 AND $3)
FROM city_open_world_mobility_arrivals arrival
JOIN city_open_world_mobility_demands demand
  ON demand.world_id = arrival.world_id AND demand.id = arrival.demand_id
JOIN city_open_world_commute_sources source
  ON source.world_id = demand.world_id
 AND source.code = demand.metadata->>'commute_source_code'
WHERE arrival.world_id = $1`, worldID, cycleStart, cycleEnd).Scan(
		&metric.ArrivalLandedCount, &metric.ArrivalBlockedCount, &metric.ArrivalFailedCount,
	); err != nil {
		return metric, fmt.Errorf("collect V13 commute arrival metrics: %w", err)
	}
	return metric, nil
}

func generateCityOpenWorldCommuteSourceDemands(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldCommuteSourcePolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_generation"})
	}
	rows, err := tx.QueryContext(ctx, `
SELECT source.id, source.code, source.binding_code, source.source_kind, source.direction,
       actor.id, actor.code, actor.status, source.employment_role_code,
       source.origin_facility_code, source.origin_hub_code,
       source.destination_facility_code, source.destination_hub_code,
       source.mode_code, source.purpose_code, source.requested_units,
       source.status, source.period_ticks, source.phase_offset, source.next_due_tick,
       source.last_transition_tick, source.generated_count, source.suppressed_count,
       source.version, source.metadata
FROM city_open_world_commute_sources source
JOIN city_open_world_actors actor
  ON actor.id = source.actor_id AND actor.world_id = source.world_id
WHERE source.world_id = $1
  AND source.status = 'active'
  AND source.next_due_tick <= $2
ORDER BY source.next_due_tick ASC, source.code ASC
LIMIT $3
FOR UPDATE OF source, actor`, worldID, targetTick, policy.MaximumGenerationsTick)
	if err != nil {
		return fmt.Errorf("load V13 due commute sources: %w", err)
	}
	records := make([]cityOpenWorldCommuteSourceRecord, 0)
	for rows.Next() {
		record := cityOpenWorldCommuteSourceRecord{}
		if err = rows.Scan(
			&record.id, &record.source.Code, &record.source.BindingCode, &record.source.SourceKind,
			&record.source.Direction, &record.actorID, &record.source.ActorCode,
			&record.actorStatus, &record.source.EmploymentRoleCode,
			&record.source.OriginFacilityCode, &record.source.OriginHubCode,
			&record.source.DestinationFacilityCode, &record.source.DestinationHubCode,
			&record.source.ModeCode, &record.source.PurposeCode, &record.source.RequestedUnits,
			&record.source.Status, &record.source.PeriodTicks, &record.source.PhaseOffset,
			&record.source.NextDueTick, &record.source.LastTransitionTick,
			&record.source.GeneratedCount, &record.source.SuppressedCount,
			&record.source.Version, &record.source.Metadata,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan V13 due commute source: %w", err)
		}
		records = append(records, record)
	}
	if err = closeCityRows(rows, "iterate V13 due commute sources"); err != nil {
		return err
	}
	for index := range records {
		if err = generateCityOpenWorldCommuteSourceDemand(ctx, tx, worldID, targetTick, policy, &records[index], execution); err != nil {
			return err
		}
	}
	return nil
}

func generateCityOpenWorldCommuteSourceDemand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldCommuteSourcePolicy,
	record *cityOpenWorldCommuteSourceRecord,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || record == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_source"})
	}
	if record.actorStatus != "active" {
		return suppressCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonActorInactive, nil, execution)
	}
	var employmentRoleActive bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM city_open_world_actor_roles role
    WHERE role.world_id = $1 AND role.actor_id = $2
      AND role.role_code = $3 AND role.category_code = 'employment'
      AND role.status = 'active'
)`, worldID, record.actorID, record.source.EmploymentRoleCode).Scan(&employmentRoleActive); err != nil {
		return fmt.Errorf("verify V13 commute employment role: %w", err)
	}
	if !employmentRoleActive {
		return suppressCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonEmploymentRoleInactive, nil, execution)
	}
	originFacility, err := loadCityOpenWorldCommuteSourceFacility(ctx, tx, worldID, record.source.OriginFacilityCode, record.source.OriginHubCode)
	if err != nil {
		return err
	}
	if originFacility == nil || !cityOpenWorldCommuteSourceFacilityMatchesDirection(record.source.Direction, *originFacility, true) {
		return suppressCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonOriginFacility, nil, execution)
	}
	destinationFacility, err := loadCityOpenWorldCommuteSourceFacility(ctx, tx, worldID, record.source.DestinationFacilityCode, record.source.DestinationHubCode)
	if err != nil {
		return err
	}
	if destinationFacility == nil || !cityOpenWorldCommuteSourceFacilityMatchesDirection(record.source.Direction, *destinationFacility, false) {
		return suppressCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonDestinationFacility, nil, execution)
	}
	location, err := loadCityOpenWorldActorLocationByCode(ctx, tx, worldID, record.source.ActorCode)
	if err != nil {
		return err
	}
	if location == nil || !cityOpenWorldMobilityArrivalLocationValid(*location, record.source.ActorCode) {
		return suppressCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonActorLocation, nil, execution)
	}
	if !cityOpenWorldCommuteSourceLocationAtFacility(*location, *originFacility, policy.SurfaceEgressRadius) {
		summary := map[string]any{
			"space_kind": location.SpaceKind, "location_scope": location.LocationScope,
			"building_code": cityOpenWorldV5StringValue(location.BuildingCode),
		}
		return suppressCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonExpectedOrigin, summary, execution)
	}
	var navigationBusy, mobilityBusy bool
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM city_open_world_actor_navigation_intents
    WHERE world_id = $1 AND actor_id = $2 AND status = 'active'
)`, worldID, record.actorID).Scan(&navigationBusy); err != nil {
		return fmt.Errorf("check V13 commute navigation conflict: %w", err)
	}
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM city_open_world_mobility_demands
    WHERE world_id = $1 AND actor_id = $2 AND status IN ('pending', 'scheduled')
)`, worldID, record.actorID).Scan(&mobilityBusy); err != nil {
		return fmt.Errorf("check V13 commute mobility conflict: %w", err)
	}
	if navigationBusy {
		return suppressCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonNavigationBusy, nil, execution)
	}
	if mobilityBusy {
		return suppressCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonMobilityBusy, nil, execution)
	}
	mode, err := loadCityOpenWorldMobilityMode(ctx, tx, worldID, record.source.ModeCode)
	if err != nil {
		return err
	}
	if mode == nil {
		return suppressCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonModeUnavailable, nil, execution)
	}
	originHub, err := loadCityOpenWorldMobilityHub(ctx, tx, worldID, record.source.OriginHubCode)
	if err != nil {
		return err
	}
	if originHub == nil || originHub.HubKind != "facility" || originHub.FacilityCode == nil || *originHub.FacilityCode != record.source.OriginFacilityCode {
		return suppressCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonOriginHub, nil, execution)
	}
	destinationHub, err := loadCityOpenWorldMobilityHub(ctx, tx, worldID, record.source.DestinationHubCode)
	if err != nil {
		return err
	}
	if destinationHub == nil || destinationHub.HubKind != "facility" || destinationHub.FacilityCode == nil || *destinationHub.FacilityCode != record.source.DestinationFacilityCode {
		return suppressCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonDestinationHub, nil, execution)
	}
	if originHub.Code == destinationHub.Code {
		return suppressCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonOriginHub, nil, execution)
	}
	return generateCityOpenWorldCommuteSourceDemandFromSource(
		ctx, tx, worldID, targetTick, record, *location, *originHub, *destinationHub, *mode, execution,
	)
}

func loadCityOpenWorldCommuteSourceFacility(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	facilityCode, hubCode string,
) (*cityOpenWorldCommuteSourceFacility, error) {
	item := &cityOpenWorldCommuteSourceFacility{}
	err := queryer.QueryRowContext(ctx, `
SELECT facility.code, hub.code, facility.building_code, facility.facility_type_code,
       facility.anchor_x, facility.anchor_y, facility.anchor_z
FROM city_open_world_facilities facility
JOIN city_open_world_mobility_hubs hub
  ON hub.world_id = facility.world_id AND hub.facility_id = facility.id
 AND hub.facility_code = facility.code AND hub.hub_kind = 'facility'
WHERE facility.world_id = $1 AND facility.code = $2 AND hub.code = $3
  AND facility.state = 'active'`, worldID, facilityCode, hubCode).Scan(
		&item.Code, &item.HubCode, &item.BuildingCode, &item.FacilityTypeCode,
		&item.AnchorX, &item.AnchorY, &item.AnchorZ,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load V13 commute facility %s: %w", facilityCode, err)
	}
	return item, nil
}

func cityOpenWorldCommuteSourceFacilityMatchesDirection(
	direction string,
	facility cityOpenWorldCommuteSourceFacility,
	origin bool,
) bool {
	if facility.Code == "" || facility.HubCode == "" || facility.BuildingCode == "" || facility.AnchorZ != 0 {
		return false
	}
	if direction == cityOpenWorldCommuteSourceDirectionOutbound {
		if origin {
			return facility.FacilityTypeCode == "residence"
		}
		return facility.FacilityTypeCode != "residence"
	}
	if direction == cityOpenWorldCommuteSourceDirectionReturn {
		if origin {
			return facility.FacilityTypeCode != "residence"
		}
		return facility.FacilityTypeCode == "residence"
	}
	return false
}

func cityOpenWorldCommuteSourceLocationAtFacility(
	location CityOpenWorldActorLocation,
	facility cityOpenWorldCommuteSourceFacility,
	surfaceEgressRadius int64,
) bool {
	if surfaceEgressRadius < 0 || location.Z < 0 {
		return false
	}
	if location.SpaceKind == "interior" && location.BuildingCode != nil &&
		*location.BuildingCode == facility.BuildingCode {
		return true
	}
	if location.SpaceKind != "surface" || location.LocationScope != "surface" ||
		location.BuildingCode != nil || location.Z != 0 {
		return false
	}
	return maxCityOpenWorldAbs(location.X-facility.AnchorX, location.Y-facility.AnchorY) <= surfaceEgressRadius
}

func generateCityOpenWorldCommuteSourceDemandFromSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldCommuteSourceRecord,
	location CityOpenWorldActorLocation,
	origin, destination CityOpenWorldMobilityHub,
	mode CityOpenWorldMobilityMode,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if record == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_generation"})
	}
	demandCode := cityOpenWorldCommuteSourceDemandCode(record.source.Code, targetTick)
	generationPayload, err := json.Marshal(map[string]any{
		"schema_version":            cityOpenWorldCommuteSourceSchemaVersion,
		"source_code":               record.source.Code,
		"binding_code":              record.source.BindingCode,
		"source_kind":               record.source.SourceKind,
		"direction":                 record.source.Direction,
		"actor_code":                record.source.ActorCode,
		"demand_code":               demandCode,
		"origin_facility_code":      record.source.OriginFacilityCode,
		"destination_facility_code": record.source.DestinationFacilityCode,
	})
	if err != nil {
		return fmt.Errorf("marshal V13 commute generation fact: %w", err)
	}
	generationFact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq, actorID: &record.actorID,
		factType: cityOpenWorldRuntimeFactCommuteSourceGenerated, payload: generationPayload,
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
		"schema_version":          cityOpenWorldMobilitySchemaVersion,
		"demand_code":             demandCode,
		"actor_code":              record.source.ActorCode,
		"source_hub_code":         origin.Code,
		"destination_hub_code":    destination.Code,
		"mode_code":               mode.Code,
		"purpose_code":            record.source.PurposeCode,
		"requested_units":         record.source.RequestedUnits,
		"earliest_departure_tick": targetTick + 1,
		"deadline_tick":           deadline,
		"commute_source_code":     record.source.Code,
		"commute_binding_code":    record.source.BindingCode,
		"commute_direction":       record.source.Direction,
	})
	if err != nil {
		return fmt.Errorf("marshal V13 commute mobility request fact: %w", err)
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
		"schema_version":       cityOpenWorldMobilitySchemaVersion,
		"origin":               "commute_source",
		"commute_contract":     cityOpenWorldCommuteSourceGenerationContract,
		"commute_source_code":  record.source.Code,
		"commute_binding_code": record.source.BindingCode,
		"commute_direction":    record.source.Direction,
		"arrival_bridge": map[string]any{
			"contract":        "captured_request_location_v1",
			"expected_origin": location,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal V13 commute mobility metadata: %w", err)
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
		return fmt.Errorf("insert V13 commute mobility demand %s: %w", demandCode, err)
	}
	if demandID <= 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_demand"})
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
	if err = transitionCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, generationFact, true); err != nil {
		return err
	}
	if err = updateCityOpenWorldCommuteSourcePolicy(ctx, tx, worldID, 1, 0, 0); err != nil {
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
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.commute_source_generated", payload: map[string]any{
		"source_code": record.source.Code, "binding_code": record.source.BindingCode,
		"demand_code": demandCode, "actor_code": record.source.ActorCode,
		"direction": record.source.Direction,
	}})
	return nil
}

func suppressCityOpenWorldCommuteSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldCommuteSourceRecord,
	reason string,
	detail map[string]any,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if record == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_suppression"})
	}
	payloadValue := map[string]any{
		"schema_version":                cityOpenWorldCommuteSourceSchemaVersion,
		"source_code":                   record.source.Code,
		"binding_code":                  record.source.BindingCode,
		"actor_code":                    record.source.ActorCode,
		"direction":                     record.source.Direction,
		"reason":                        reason,
		"expected_origin_facility_code": record.source.OriginFacilityCode,
	}
	if detail != nil {
		payloadValue["detail"] = detail
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return fmt.Errorf("marshal V13 commute suppression fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq, actorID: &record.actorID,
		factType: cityOpenWorldRuntimeFactCommuteSourceSuppressed, payload: payload,
	})
	if err != nil {
		return err
	}
	if err = transitionCityOpenWorldCommuteSource(ctx, tx, worldID, targetTick, record, fact, false); err != nil {
		return err
	}
	if err = updateCityOpenWorldCommuteSourcePolicy(ctx, tx, worldID, 0, 1, 0); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.commute_source_suppressed", payload: map[string]any{
		"source_code": record.source.Code, "binding_code": record.source.BindingCode,
		"actor_code": record.source.ActorCode, "direction": record.source.Direction,
		"reason": reason,
	}})
	return nil
}

func transitionCityOpenWorldCommuteSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldCommuteSourceRecord,
	fact *cityOpenWorldRuntimeFactRecord,
	generated bool,
) error {
	if record == nil || fact == nil || record.source.PeriodTicks < 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_source_transition"})
	}
	generatedDelta, suppressedDelta := int64(0), int64(1)
	if generated {
		generatedDelta, suppressedDelta = 1, 0
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_commute_sources
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
		return fmt.Errorf("transition V13 commute source %s: %w", record.source.Code, err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_source_transition"})
	}
	return nil
}

// cityOpenWorldMobilityODSourceSupersededByCommute keeps V11 source rows as
// immutable historical evidence while preventing duplicate future traffic for
// an actor whose complete V13 commute source pair is active.
func cityOpenWorldMobilityODSourceSupersededByCommute(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, actorID int64,
) (bool, error) {
	var hasV13History bool
	if err := queryer.QueryRowContext(ctx, `
	SELECT simulation_version IN ($2, $3) FROM city_worlds WHERE id = $1`,
		worldID, CitySimulationVersionOpenWorldV13, CitySimulationVersionOpenWorldV14).Scan(&hasV13History); err != nil {
		return false, fmt.Errorf("load V13 commute source world version: %w", err)
	}
	if !hasV13History {
		return false, nil
	}
	var complete bool
	if err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*) = 2
   AND COUNT(DISTINCT source.direction) = 2
FROM city_open_world_commute_sources source
WHERE source.world_id = $1
  AND source.actor_id = $2
  AND source.status = 'active'
  AND source.direction IN ('outbound', 'return')`, worldID, actorID).Scan(&complete); err != nil {
		return false, fmt.Errorf("check V13 commute source pair: %w", err)
	}
	return complete, nil
}
