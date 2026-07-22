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
	cityOpenWorldRuntimeFactCommuteLifecycleSourceGenerated  = "system.commute.lifecycle.source.generated"
	cityOpenWorldRuntimeFactCommuteLifecycleSourceSuppressed = "system.commute.lifecycle.source.suppressed"
	cityOpenWorldRuntimeFactCommuteLifecycleCycleClose       = "system.commute.lifecycle.cycle.closed"
	cityOpenWorldRuntimeFactCommuteLifecycleAutoSuspended    = "system.commute.lifecycle.assignment.auto.suspended"
	cityOpenWorldRuntimeFactCommuteLifecycleAutoResumed      = "system.commute.lifecycle.assignment.auto.resumed"
	cityOpenWorldRuntimeFactCommuteLifecycleRebound          = "system.commute.lifecycle.assignment.rebound"
	cityOpenWorldRuntimeFactCommuteLifecycleStateChanged     = "system.commute.lifecycle.assignment.state.changed"
	cityOpenWorldRuntimeFactCommuteLifecycleTerminated       = "system.commute.lifecycle.assignment.terminated"
)

type cityOpenWorldCommuteLifecycleSourceRecord struct {
	actorID     int64
	actorStatus string
	source      CityOpenWorldCommuteLifecycleSource
}

type cityOpenWorldCommuteLifecycleAssignmentRecord struct {
	assignment  CityOpenWorldCommuteAssignmentEpoch
	actorID     int64
	actorCode   string
	actorStatus string
	state       string
	reasonCode  string
}

func loadCityOpenWorldCommuteLifecyclePolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldCommuteLifecyclePolicy, error) {
	policy := &CityOpenWorldCommuteLifecyclePolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick, assignment_contract,
       source_contract, period_ticks, maximum_assignments, maximum_transitions_tick,
       maximum_generations_tick, assignment_count, active_assignment_count,
       suspended_assignment_count, superseded_assignment_count, terminated_assignment_count,
       source_count, generated_count, suppressed_count, transition_count, metric_count,
       revision, metadata
FROM city_open_world_commute_lifecycle_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash, &policy.BaselineTick,
		&policy.AssignmentContract, &policy.SourceContract, &policy.PeriodTicks,
		&policy.MaximumAssignments, &policy.MaximumTransitionsTick, &policy.MaximumGenerationsTick,
		&policy.AssignmentCount, &policy.ActiveAssignmentCount, &policy.SuspendedAssignmentCount,
		&policy.SupersededAssignmentCount, &policy.TerminatedAssignmentCount,
		&policy.SourceCount, &policy.GeneratedCount, &policy.SuppressedCount,
		&policy.TransitionCount, &policy.MetricCount, &policy.Revision, &policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V14 commute lifecycle profile: %w", err)
	}
	if err = validateCityOpenWorldCommuteLifecyclePolicy(*policy); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_profile"}).WithCause(err)
	}
	return policy, nil
}

type cityOpenWorldCommuteLifecyclePolicyDelta struct {
	assignments int64
	active      int64
	suspended   int64
	superseded  int64
	terminated  int64
	sources     int64
	generated   int64
	suppressed  int64
	transitions int64
	metrics     int64
}

func updateCityOpenWorldCommuteLifecyclePolicy(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	delta cityOpenWorldCommuteLifecyclePolicyDelta,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_commute_lifecycle_profiles
SET assignment_count = assignment_count + $2,
    active_assignment_count = active_assignment_count + $3,
    suspended_assignment_count = suspended_assignment_count + $4,
    superseded_assignment_count = superseded_assignment_count + $5,
    terminated_assignment_count = terminated_assignment_count + $6,
    source_count = source_count + $7,
    generated_count = generated_count + $8,
    suppressed_count = suppressed_count + $9,
    transition_count = transition_count + $10,
    metric_count = metric_count + $11,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID, delta.assignments, delta.active, delta.suspended,
		delta.superseded, delta.terminated, delta.sources, delta.generated,
		delta.suppressed, delta.transitions, delta.metrics)
	if err != nil {
		return fmt.Errorf("update V14 commute lifecycle profile: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_profile"})
	}
	return nil
}

func cityOpenWorldCommuteLifecycleDemandCode(sourceCode string, targetTick int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("commute.lifecycle.demand.v1\x00%s\x00%d", sourceCode, targetTick)))
	return "mobility.demand.commute.lifecycle." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldCommuteLifecycleCycleWindow(
	policy CityOpenWorldCommuteLifecyclePolicy,
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

// advanceCityOpenWorldV14CommuteLifecycle is the sole V14 automatic source
// pass. It evaluates automatic state transitions before producing new V9
// demand, while the V9 scheduler still enforces a later-tick departure.
func advanceCityOpenWorldV14CommuteLifecycle(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
) (cityOpenWorldRuntimeAutomaticExecution, error) {
	execution := cityOpenWorldRuntimeAutomaticExecution{
		facts: make([]CityOpenWorldRuntimeFact, 0), effects: make([]CityOpenWorldRuntimeEffect, 0),
		events: make([]worldRuntimeAutomaticEvent, 0), nextFactSeq: factSequence,
	}
	policy, err := loadCityOpenWorldCommuteLifecyclePolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldCommuteLifecycleWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = advanceCityOpenWorldCommuteLifecycleAutomaticStates(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if err = closeCityOpenWorldCommuteLifecycleCycle(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if err = generateCityOpenWorldCommuteLifecycleDemands(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if len(execution.facts) > 0 {
		if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, int64(len(execution.facts)), 0, 0); err != nil {
			return execution, err
		}
	}
	if err = assertCityOpenWorldCommuteLifecycleFoundation(ctx, tx, worldID); err != nil {
		return execution, fmt.Errorf("validate V14 commute lifecycle foundation after advancement: %w", err)
	}
	return execution, nil
}

func advanceCityOpenWorldCommuteLifecycleAutomaticStates(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldCommuteLifecyclePolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_state"})
	}
	// Commands are applied before automatic reducers during a tick. Reserve the
	// transition budget already consumed by administrator actions so the
	// automatic pass cannot silently exceed the contract's per-tick limit.
	var existingTransitions int64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_open_world_commute_assignment_transitions
WHERE world_id = $1 AND transition_tick = $2`, worldID, targetTick).Scan(&existingTransitions); err != nil {
		return fmt.Errorf("count V14 commute lifecycle transitions before automatic advancement: %w", err)
	}
	remainingTransitions := int64(policy.MaximumTransitionsTick) - existingTransitions
	if remainingTransitions < 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_transition_budget"})
	}
	if remainingTransitions == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT epoch.code, epoch.binding_code, actor.id, actor.code, actor.status,
       epoch.epoch_number, epoch.assignment_kind, epoch.employment_role_code,
       epoch.home_facility_code, epoch.home_hub_code, epoch.work_facility_code,
       epoch.work_hub_code, epoch.period_ticks, epoch.outbound_phase, epoch.return_phase,
       epoch.origin_kind, epoch.opened_tick, epoch.metadata,
       latest.state, latest.reason_code
FROM city_open_world_commute_assignment_epochs epoch
JOIN city_open_world_actors actor
  ON actor.id = epoch.actor_id AND actor.world_id = epoch.world_id
JOIN LATERAL (
    SELECT transition.state, transition.reason_code
    FROM city_open_world_commute_assignment_transitions transition
    WHERE transition.world_id = epoch.world_id AND transition.assignment_code = epoch.code
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1
) latest ON TRUE
WHERE epoch.world_id = $1
  AND latest.state IN ('active', 'suspended')
ORDER BY epoch.code
LIMIT $2
FOR UPDATE OF epoch, actor`, worldID, remainingTransitions)
	if err != nil {
		return fmt.Errorf("load V14 commute lifecycle assignments: %w", err)
	}
	records := make([]cityOpenWorldCommuteLifecycleAssignmentRecord, 0)
	for rows.Next() {
		record := cityOpenWorldCommuteLifecycleAssignmentRecord{}
		if err = rows.Scan(
			&record.assignment.Code, &record.assignment.BindingCode, &record.actorID, &record.actorCode, &record.actorStatus,
			&record.assignment.EpochNumber, &record.assignment.AssignmentKind, &record.assignment.EmploymentRole,
			&record.assignment.HomeFacilityCode, &record.assignment.HomeHubCode, &record.assignment.WorkFacilityCode,
			&record.assignment.WorkHubCode, &record.assignment.PeriodTicks, &record.assignment.OutboundPhase,
			&record.assignment.ReturnPhase, &record.assignment.OriginKind, &record.assignment.OpenedTick,
			&record.assignment.Metadata, &record.state, &record.reasonCode,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan V14 commute lifecycle assignment: %w", err)
		}
		record.assignment.ActorCode = record.actorCode
		records = append(records, record)
	}
	if err = closeCityRows(rows, "iterate V14 commute lifecycle assignments"); err != nil {
		return err
	}
	for index := range records {
		if err = advanceCityOpenWorldCommuteLifecycleAssignmentState(ctx, tx, worldID, targetTick, &records[index], execution); err != nil {
			return err
		}
	}
	return nil
}

func advanceCityOpenWorldCommuteLifecycleAssignmentState(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldCommuteLifecycleAssignmentRecord,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if record == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_assignment"})
	}
	valid, reason, err := cityOpenWorldCommuteLifecycleAssignmentOperational(ctx, tx, worldID, record)
	if err != nil {
		return err
	}
	if record.state == cityOpenWorldCommuteLifecycleStateActive && !valid {
		return transitionCityOpenWorldCommuteLifecycleAssignment(
			ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteLifecycleStateSuspended,
			reason, cityOpenWorldRuntimeFactCommuteLifecycleAutoSuspended, true, execution,
		)
	}
	if record.state == cityOpenWorldCommuteLifecycleStateSuspended && valid &&
		cityOpenWorldCommuteLifecycleAutomaticSuspensionReason(record.reasonCode) {
		return transitionCityOpenWorldCommuteLifecycleAssignment(
			ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteLifecycleStateActive,
			cityOpenWorldCommuteLifecycleRestoredReason(record.reasonCode), cityOpenWorldRuntimeFactCommuteLifecycleAutoResumed, true, execution,
		)
	}
	return nil
}

func cityOpenWorldCommuteLifecycleAutomaticSuspensionReason(reason string) bool {
	return reason == cityOpenWorldCommuteLifecycleReasonActorInactive ||
		reason == cityOpenWorldCommuteLifecycleReasonEmploymentRoleInactive ||
		reason == cityOpenWorldCommuteLifecycleReasonOriginFacilityUnavailable ||
		reason == cityOpenWorldCommuteLifecycleReasonDestinationUnavailable
}

func cityOpenWorldCommuteLifecycleRestoredReason(reason string) string {
	switch reason {
	case cityOpenWorldCommuteLifecycleReasonActorInactive:
		return cityOpenWorldCommuteLifecycleReasonActorRestored
	case cityOpenWorldCommuteLifecycleReasonEmploymentRoleInactive:
		return cityOpenWorldCommuteLifecycleReasonEmploymentRoleRestored
	case cityOpenWorldCommuteLifecycleReasonOriginFacilityUnavailable:
		return cityOpenWorldCommuteLifecycleReasonOriginFacilityRestored
	case cityOpenWorldCommuteLifecycleReasonDestinationUnavailable:
		return cityOpenWorldCommuteLifecycleReasonDestinationRestored
	default:
		return cityOpenWorldCommuteLifecycleReasonAdminResumed
	}
}

func cityOpenWorldCommuteLifecycleAssignmentOperational(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	record *cityOpenWorldCommuteLifecycleAssignmentRecord,
) (bool, string, error) {
	if record == nil || record.actorID <= 0 || record.assignment.Code == "" {
		return false, cityOpenWorldCommuteLifecycleReasonProfileMismatch, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_assignment"})
	}
	if record.actorStatus != "active" {
		return false, cityOpenWorldCommuteLifecycleReasonActorInactive, nil
	}
	var roleActive bool
	if err := queryer.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM city_open_world_actor_roles role
    WHERE role.world_id = $1 AND role.actor_id = $2
      AND role.role_code = $3 AND role.category_code = 'employment'
      AND role.status = 'active'
)`, worldID, record.actorID, record.assignment.EmploymentRole).Scan(&roleActive); err != nil {
		return false, "", fmt.Errorf("verify V14 commute lifecycle employment role: %w", err)
	}
	if !roleActive {
		return false, cityOpenWorldCommuteLifecycleReasonEmploymentRoleInactive, nil
	}
	home, err := loadCityOpenWorldCommuteSourceFacility(ctx, queryer, worldID, record.assignment.HomeFacilityCode, record.assignment.HomeHubCode)
	if err != nil {
		return false, "", err
	}
	if home == nil || !cityOpenWorldCommuteSourceFacilityMatchesDirection(cityOpenWorldCommuteSourceDirectionOutbound, *home, true) {
		return false, cityOpenWorldCommuteLifecycleReasonOriginFacilityUnavailable, nil
	}
	work, err := loadCityOpenWorldCommuteSourceFacility(ctx, queryer, worldID, record.assignment.WorkFacilityCode, record.assignment.WorkHubCode)
	if err != nil {
		return false, "", err
	}
	if work == nil || !cityOpenWorldCommuteSourceFacilityMatchesDirection(cityOpenWorldCommuteSourceDirectionOutbound, *work, false) {
		return false, cityOpenWorldCommuteLifecycleReasonDestinationUnavailable, nil
	}
	return true, "", nil
}

func transitionCityOpenWorldCommuteLifecycleAssignment(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldCommuteLifecycleAssignmentRecord,
	nextState, reasonCode, factType string,
	automatic bool,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if record == nil || execution == nil || !cityOpenWorldCommuteLifecycleTransitionAllowed(record.state, nextState) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_transition"})
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version":  cityOpenWorldCommuteLifecycleSchemaVersion,
		"assignment_code": record.assignment.Code,
		"binding_code":    record.assignment.BindingCode,
		"actor_code":      record.actorCode,
		"from_state":      record.state,
		"to_state":        nextState,
		"reason_code":     reasonCode,
		"automatic":       automatic,
	})
	if err != nil {
		return fmt.Errorf("marshal V14 commute lifecycle transition fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq, actorID: &record.actorID,
		factType: factType, payload: payload,
	})
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldCommuteLifecycleSchemaVersion,
		"automatic":      automatic,
		"previous_state": record.state,
	})
	if err != nil {
		return fmt.Errorf("marshal V14 commute lifecycle transition metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_assignment_transitions
    (world_id, assignment_code, transition_tick, transition_sequence, state,
     reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
		worldID, record.assignment.Code, targetTick, execution.nextFactSeq, nextState,
		reasonCode, fact.id, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V14 commute lifecycle transition: %w", err)
	}
	delta := cityOpenWorldCommuteLifecyclePolicyDelta{transitions: 1}
	switch record.state {
	case cityOpenWorldCommuteLifecycleStateActive:
		delta.active--
	case cityOpenWorldCommuteLifecycleStateSuspended:
		delta.suspended--
	}
	switch nextState {
	case cityOpenWorldCommuteLifecycleStateActive:
		delta.active++
	case cityOpenWorldCommuteLifecycleStateSuspended:
		delta.suspended++
	case cityOpenWorldCommuteLifecycleStateSuperseded:
		delta.superseded++
	case cityOpenWorldCommuteLifecycleStateTerminated:
		delta.terminated++
	}
	if err = updateCityOpenWorldCommuteLifecyclePolicy(ctx, tx, worldID, delta); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{
		eventType: "city.open_world.commute_lifecycle_state_changed",
		payload:   map[string]any{"assignment_code": record.assignment.Code, "actor_code": record.actorCode, "state": nextState, "reason_code": reasonCode},
	})
	return nil
}

func closeCityOpenWorldCommuteLifecycleCycle(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldCommuteLifecyclePolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_cycle"})
	}
	cycleStart, cycleEnd, due := cityOpenWorldCommuteLifecycleCycleWindow(*policy, targetTick)
	if !due {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM city_open_world_commute_lifecycle_cycle_metrics
    WHERE world_id = $1 AND cycle_start_tick = $2
)`, worldID, cycleStart).Scan(&exists); err != nil {
		return fmt.Errorf("check V14 commute lifecycle cycle close: %w", err)
	}
	if exists {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_cycle_duplicate"})
	}
	metric, err := collectCityOpenWorldCommuteLifecycleCycleMetric(ctx, tx, worldID, cycleStart, cycleEnd, targetTick)
	if err != nil {
		return err
	}
	metric.SourceFact = CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: execution.nextFactSeq}
	payload, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldCommuteLifecycleSchemaVersion,
		"cycle_start_tick": cycleStart,
		"cycle_end_tick":   cycleEnd,
		"metric":           metric,
		"contract":         cityOpenWorldCommuteLifecycleSourceContract,
	})
	if err != nil {
		return fmt.Errorf("marshal V14 commute lifecycle cycle-close fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		factType: cityOpenWorldRuntimeFactCommuteLifecycleCycleClose, payload: payload,
	})
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldCommuteLifecycleSchemaVersion,
		"event_scope":    "commute_lifecycle_occurrence_window_v1",
	})
	if err != nil {
		return fmt.Errorf("marshal V14 commute lifecycle cycle metadata: %w", err)
	}
	metric.Metadata = metadata
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_lifecycle_cycle_metrics
    (world_id, cycle_start_tick, cycle_end_tick, closed_tick, source_fact_id,
     transition_count, rebind_count, generated_count, suppressed_count,
     scheduled_demand_count, completed_demand_count, expired_demand_count,
     pending_demand_count, arrival_landed_count, arrival_blocked_count,
     arrival_failed_count, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17::jsonb)`,
		worldID, metric.CycleStartTick, metric.CycleEndTick, metric.ClosedTick, fact.id,
		metric.TransitionCount, metric.RebindCount, metric.GeneratedCount, metric.SuppressedCount,
		metric.ScheduledDemandCount, metric.CompletedDemandCount, metric.ExpiredDemandCount,
		metric.PendingDemandCount, metric.ArrivalLandedCount, metric.ArrivalBlockedCount,
		metric.ArrivalFailedCount, []byte(metric.Metadata)); err != nil {
		return fmt.Errorf("insert V14 commute lifecycle cycle metric: %w", err)
	}
	if err = updateCityOpenWorldCommuteLifecyclePolicy(ctx, tx, worldID, cityOpenWorldCommuteLifecyclePolicyDelta{metrics: 1}); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.commute_lifecycle_cycle_closed", payload: map[string]any{
		"cycle_start_tick": cycleStart, "cycle_end_tick": cycleEnd,
	}})
	return nil
}

func collectCityOpenWorldCommuteLifecycleCycleMetric(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, cycleStart, cycleEnd, closedTick int64,
) (CityOpenWorldCommuteLifecycleCycleMetric, error) {
	metric := CityOpenWorldCommuteLifecycleCycleMetric{
		CycleStartTick: cycleStart, CycleEndTick: cycleEnd, ClosedTick: closedTick,
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE fact_type LIKE 'system.commute.lifecycle.assignment.%'),
       COUNT(*) FILTER (WHERE fact_type = $4),
       COUNT(*) FILTER (WHERE fact_type = $5),
       COUNT(*) FILTER (WHERE fact_type = $6)
FROM city_open_world_runtime_facts
WHERE world_id = $1 AND tick BETWEEN $2 AND $3`, worldID, cycleStart, cycleEnd,
		cityOpenWorldRuntimeFactCommuteLifecycleRebound,
		cityOpenWorldRuntimeFactCommuteLifecycleSourceGenerated,
		cityOpenWorldRuntimeFactCommuteLifecycleSourceSuppressed,
	).Scan(&metric.TransitionCount, &metric.RebindCount, &metric.GeneratedCount, &metric.SuppressedCount); err != nil {
		return metric, fmt.Errorf("collect V14 commute lifecycle facts: %w", err)
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE demand.scheduled_tick BETWEEN $2 AND $3),
       COUNT(*) FILTER (WHERE demand.completed_tick BETWEEN $2 AND $3),
       COUNT(*) FILTER (WHERE demand.expired_tick BETWEEN $2 AND $3),
       COUNT(*) FILTER (WHERE demand.status IN ('pending', 'scheduled'))
FROM city_open_world_mobility_demands demand
JOIN city_open_world_commute_lifecycle_sources source
  ON source.world_id = demand.world_id
 AND source.code = demand.metadata->>'commute_lifecycle_source_code'
WHERE demand.world_id = $1`, worldID, cycleStart, cycleEnd).Scan(
		&metric.ScheduledDemandCount, &metric.CompletedDemandCount,
		&metric.ExpiredDemandCount, &metric.PendingDemandCount,
	); err != nil {
		return metric, fmt.Errorf("collect V14 commute lifecycle demand metrics: %w", err)
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE arrival.landed_tick BETWEEN $2 AND $3),
       COALESCE(SUM(arrival.blocked_attempts) FILTER (WHERE arrival.updated_tick BETWEEN $2 AND $3), 0),
       COUNT(*) FILTER (WHERE arrival.failed_tick BETWEEN $2 AND $3)
FROM city_open_world_mobility_arrivals arrival
JOIN city_open_world_mobility_demands demand
  ON demand.world_id = arrival.world_id AND demand.id = arrival.demand_id
JOIN city_open_world_commute_lifecycle_sources source
  ON source.world_id = demand.world_id
 AND source.code = demand.metadata->>'commute_lifecycle_source_code'
WHERE arrival.world_id = $1`, worldID, cycleStart, cycleEnd).Scan(
		&metric.ArrivalLandedCount, &metric.ArrivalBlockedCount, &metric.ArrivalFailedCount,
	); err != nil {
		return metric, fmt.Errorf("collect V14 commute lifecycle arrival metrics: %w", err)
	}
	return metric, nil
}

func generateCityOpenWorldCommuteLifecycleDemands(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldCommuteLifecyclePolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_generation"})
	}
	rows, err := tx.QueryContext(ctx, `
SELECT source.code, source.assignment_code, source.binding_code, actor.id, actor.code, actor.status,
       source.source_kind, source.direction, source.employment_role_code,
       source.origin_facility_code, source.origin_hub_code,
       source.destination_facility_code, source.destination_hub_code,
       source.mode_code, source.purpose_code, source.requested_units,
       source.status, source.period_ticks, source.phase_offset, source.next_due_tick,
       source.last_transition_tick, source.generated_count, source.suppressed_count,
       source.version, source.metadata
FROM city_open_world_commute_lifecycle_sources source
JOIN city_open_world_actors actor
  ON actor.id = source.actor_id AND actor.world_id = source.world_id
JOIN LATERAL (
    SELECT transition.state
    FROM city_open_world_commute_assignment_transitions transition
    WHERE transition.world_id = source.world_id AND transition.assignment_code = source.assignment_code
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1
) lifecycle ON lifecycle.state = 'active'
WHERE source.world_id = $1
  AND source.status = 'active'
  AND source.next_due_tick <= $2
ORDER BY source.next_due_tick ASC, source.code ASC
LIMIT $3
FOR UPDATE OF source, actor`, worldID, targetTick, policy.MaximumGenerationsTick)
	if err != nil {
		return fmt.Errorf("load V14 due commute lifecycle sources: %w", err)
	}
	records := make([]cityOpenWorldCommuteLifecycleSourceRecord, 0)
	for rows.Next() {
		record := cityOpenWorldCommuteLifecycleSourceRecord{}
		if err = rows.Scan(
			&record.source.Code, &record.source.AssignmentCode, &record.source.BindingCode,
			&record.actorID, &record.source.ActorCode, &record.actorStatus,
			&record.source.SourceKind, &record.source.Direction, &record.source.EmploymentRoleCode,
			&record.source.OriginFacilityCode, &record.source.OriginHubCode,
			&record.source.DestinationFacilityCode, &record.source.DestinationHubCode,
			&record.source.ModeCode, &record.source.PurposeCode, &record.source.RequestedUnits,
			&record.source.Status, &record.source.PeriodTicks, &record.source.PhaseOffset,
			&record.source.NextDueTick, &record.source.LastTransitionTick,
			&record.source.GeneratedCount, &record.source.SuppressedCount,
			&record.source.Version, &record.source.Metadata,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan V14 due commute lifecycle source: %w", err)
		}
		records = append(records, record)
	}
	if err = closeCityRows(rows, "iterate V14 due commute lifecycle sources"); err != nil {
		return err
	}
	for index := range records {
		if err = generateCityOpenWorldCommuteLifecycleDemand(ctx, tx, worldID, targetTick, &records[index], execution); err != nil {
			return err
		}
	}
	return nil
}

func generateCityOpenWorldCommuteLifecycleDemand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldCommuteLifecycleSourceRecord,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if record == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_source"})
	}
	if record.actorStatus != "active" {
		return suppressCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonActorInactive, nil, execution)
	}
	var employmentRoleActive bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM city_open_world_actor_roles role
    WHERE role.world_id = $1 AND role.actor_id = $2
      AND role.role_code = $3 AND role.category_code = 'employment'
      AND role.status = 'active'
)`, worldID, record.actorID, record.source.EmploymentRoleCode).Scan(&employmentRoleActive); err != nil {
		return fmt.Errorf("verify V14 commute lifecycle employment role: %w", err)
	}
	if !employmentRoleActive {
		return suppressCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonEmploymentRoleInactive, nil, execution)
	}
	originFacility, err := loadCityOpenWorldCommuteSourceFacility(ctx, tx, worldID, record.source.OriginFacilityCode, record.source.OriginHubCode)
	if err != nil {
		return err
	}
	if originFacility == nil || !cityOpenWorldCommuteSourceFacilityMatchesDirection(record.source.Direction, *originFacility, true) {
		return suppressCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonOriginFacility, nil, execution)
	}
	destinationFacility, err := loadCityOpenWorldCommuteSourceFacility(ctx, tx, worldID, record.source.DestinationFacilityCode, record.source.DestinationHubCode)
	if err != nil {
		return err
	}
	if destinationFacility == nil || !cityOpenWorldCommuteSourceFacilityMatchesDirection(record.source.Direction, *destinationFacility, false) {
		return suppressCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonDestinationFacility, nil, execution)
	}
	location, err := loadCityOpenWorldActorLocationByCode(ctx, tx, worldID, record.source.ActorCode)
	if err != nil {
		return err
	}
	if location == nil || !cityOpenWorldMobilityArrivalLocationValid(*location, record.source.ActorCode) {
		return suppressCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonActorLocation, nil, execution)
	}
	if !cityOpenWorldCommuteSourceLocationAtFacility(*location, *originFacility, cityOpenWorldCommuteSourceSurfaceEgressRadius) {
		summary := map[string]any{
			"space_kind": location.SpaceKind, "location_scope": location.LocationScope,
			"building_code": cityOpenWorldV5StringValue(location.BuildingCode),
		}
		return suppressCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonExpectedOrigin, summary, execution)
	}
	var navigationBusy, mobilityBusy bool
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM city_open_world_actor_navigation_intents
    WHERE world_id = $1 AND actor_id = $2 AND status = 'active'
)`, worldID, record.actorID).Scan(&navigationBusy); err != nil {
		return fmt.Errorf("check V14 commute lifecycle navigation conflict: %w", err)
	}
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM city_open_world_mobility_demands
    WHERE world_id = $1 AND actor_id = $2 AND status IN ('pending', 'scheduled')
)`, worldID, record.actorID).Scan(&mobilityBusy); err != nil {
		return fmt.Errorf("check V14 commute lifecycle mobility conflict: %w", err)
	}
	if navigationBusy {
		return suppressCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonNavigationBusy, nil, execution)
	}
	if mobilityBusy {
		return suppressCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonMobilityBusy, nil, execution)
	}
	mode, err := loadCityOpenWorldMobilityMode(ctx, tx, worldID, record.source.ModeCode)
	if err != nil {
		return err
	}
	if mode == nil {
		return suppressCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonModeUnavailable, nil, execution)
	}
	originHub, err := loadCityOpenWorldMobilityHub(ctx, tx, worldID, record.source.OriginHubCode)
	if err != nil {
		return err
	}
	if originHub == nil || originHub.HubKind != "facility" || originHub.FacilityCode == nil || *originHub.FacilityCode != record.source.OriginFacilityCode {
		return suppressCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonOriginHub, nil, execution)
	}
	destinationHub, err := loadCityOpenWorldMobilityHub(ctx, tx, worldID, record.source.DestinationHubCode)
	if err != nil {
		return err
	}
	if destinationHub == nil || destinationHub.HubKind != "facility" || destinationHub.FacilityCode == nil || *destinationHub.FacilityCode != record.source.DestinationFacilityCode || originHub.Code == destinationHub.Code {
		return suppressCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, cityOpenWorldCommuteSourceReasonDestinationHub, nil, execution)
	}
	return generateCityOpenWorldCommuteLifecycleDemandFromSource(
		ctx, tx, worldID, targetTick, record, *location, *originHub, *destinationHub, *mode, execution,
	)
}

func generateCityOpenWorldCommuteLifecycleDemandFromSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldCommuteLifecycleSourceRecord,
	location CityOpenWorldActorLocation,
	origin, destination CityOpenWorldMobilityHub,
	mode CityOpenWorldMobilityMode,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if record == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_generation"})
	}
	demandCode := cityOpenWorldCommuteLifecycleDemandCode(record.source.Code, targetTick)
	generationPayload, err := json.Marshal(map[string]any{
		"schema_version":            cityOpenWorldCommuteLifecycleSchemaVersion,
		"source_code":               record.source.Code,
		"assignment_code":           record.source.AssignmentCode,
		"binding_code":              record.source.BindingCode,
		"source_kind":               record.source.SourceKind,
		"direction":                 record.source.Direction,
		"actor_code":                record.source.ActorCode,
		"demand_code":               demandCode,
		"origin_facility_code":      record.source.OriginFacilityCode,
		"destination_facility_code": record.source.DestinationFacilityCode,
	})
	if err != nil {
		return fmt.Errorf("marshal V14 commute lifecycle generation fact: %w", err)
	}
	generationFact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq, actorID: &record.actorID,
		factType: cityOpenWorldRuntimeFactCommuteLifecycleSourceGenerated, payload: generationPayload,
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
		"schema_version":                cityOpenWorldMobilitySchemaVersion,
		"demand_code":                   demandCode,
		"actor_code":                    record.source.ActorCode,
		"source_hub_code":               origin.Code,
		"destination_hub_code":          destination.Code,
		"mode_code":                     mode.Code,
		"purpose_code":                  record.source.PurposeCode,
		"requested_units":               record.source.RequestedUnits,
		"earliest_departure_tick":       targetTick + 1,
		"deadline_tick":                 deadline,
		"commute_lifecycle_source_code": record.source.Code,
		"commute_assignment_code":       record.source.AssignmentCode,
		"commute_binding_code":          record.source.BindingCode,
		"commute_direction":             record.source.Direction,
	})
	if err != nil {
		return fmt.Errorf("marshal V14 commute lifecycle mobility request fact: %w", err)
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
		"schema_version":                cityOpenWorldMobilitySchemaVersion,
		"origin":                        "commute_lifecycle_source",
		"commute_contract":              cityOpenWorldCommuteLifecycleSourceContract,
		"commute_lifecycle_source_code": record.source.Code,
		"commute_assignment_code":       record.source.AssignmentCode,
		"commute_binding_code":          record.source.BindingCode,
		"commute_direction":             record.source.Direction,
		"arrival_bridge": map[string]any{
			"contract":        "captured_request_location_v1",
			"expected_origin": location,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal V14 commute lifecycle mobility metadata: %w", err)
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
		return fmt.Errorf("insert V14 commute lifecycle mobility demand %s: %w", demandCode, err)
	}
	if demandID <= 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_demand"})
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
	if err = transitionCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, generationFact, true); err != nil {
		return err
	}
	if err = updateCityOpenWorldCommuteLifecyclePolicy(ctx, tx, worldID, cityOpenWorldCommuteLifecyclePolicyDelta{generated: 1}); err != nil {
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
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.commute_lifecycle_source_generated", payload: map[string]any{
		"source_code": record.source.Code, "assignment_code": record.source.AssignmentCode,
		"binding_code": record.source.BindingCode, "demand_code": demandCode,
		"actor_code": record.source.ActorCode, "direction": record.source.Direction,
	}})
	return nil
}

func suppressCityOpenWorldCommuteLifecycleSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldCommuteLifecycleSourceRecord,
	reason string,
	detail map[string]any,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if record == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_suppression"})
	}
	payloadValue := map[string]any{
		"schema_version":                cityOpenWorldCommuteLifecycleSchemaVersion,
		"source_code":                   record.source.Code,
		"assignment_code":               record.source.AssignmentCode,
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
		return fmt.Errorf("marshal V14 commute lifecycle suppression fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq, actorID: &record.actorID,
		factType: cityOpenWorldRuntimeFactCommuteLifecycleSourceSuppressed, payload: payload,
	})
	if err != nil {
		return err
	}
	if err = transitionCityOpenWorldCommuteLifecycleSource(ctx, tx, worldID, targetTick, record, fact, false); err != nil {
		return err
	}
	if err = updateCityOpenWorldCommuteLifecyclePolicy(ctx, tx, worldID, cityOpenWorldCommuteLifecyclePolicyDelta{suppressed: 1}); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.commute_lifecycle_source_suppressed", payload: map[string]any{
		"source_code": record.source.Code, "assignment_code": record.source.AssignmentCode,
		"actor_code": record.source.ActorCode, "direction": record.source.Direction, "reason": reason,
	}})
	return nil
}

func transitionCityOpenWorldCommuteLifecycleSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldCommuteLifecycleSourceRecord,
	fact *cityOpenWorldRuntimeFactRecord,
	generated bool,
) error {
	if record == nil || fact == nil || record.source.PeriodTicks < 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_source_transition"})
	}
	generatedDelta, suppressedDelta := int64(0), int64(1)
	if generated {
		generatedDelta, suppressedDelta = 1, 0
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_commute_lifecycle_sources
SET next_due_tick = $4,
    last_transition_tick = $3,
    last_fact_id = $5,
    generated_count = generated_count + $6,
    suppressed_count = suppressed_count + $7,
    version = version + 1,
    updated_at = NOW()
WHERE world_id = $1 AND code = $2 AND status = 'active'`,
		worldID, record.source.Code, targetTick, targetTick+record.source.PeriodTicks, fact.id,
		generatedDelta, suppressedDelta)
	if err != nil {
		return fmt.Errorf("transition V14 commute lifecycle source %s: %w", record.source.Code, err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_source_transition"})
	}
	return nil
}
