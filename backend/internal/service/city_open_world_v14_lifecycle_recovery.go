package service

import (
	"context"
	"database/sql"
	"fmt"
)

// restoreCityOpenWorldCommuteLifecycleProjection restores the V14 successor
// epoch domain after V12 bindings, V13 source history, runtime facts, actors,
// facilities, and hubs are all present. IDs are deliberately re-resolved from
// the canonical snapshot so surrogate database IDs never leak into recovery.
func restoreCityOpenWorldCommuteLifecycleProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	lifecycle CityOpenWorldCommuteLifecycleState,
	actorIDs map[string]int64,
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
) (int, error) {
	if err := validateCityOpenWorldCommuteLifecycleState(&lifecycle); err != nil {
		return 0, fmt.Errorf("validate V14 commute lifecycle recovery input: %w", err)
	}
	count := 0
	policy := lifecycle.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_lifecycle_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     assignment_contract, source_contract, period_ticks, maximum_assignments,
     maximum_transitions_tick, maximum_generations_tick, assignment_count,
     active_assignment_count, suspended_assignment_count, superseded_assignment_count,
     terminated_assignment_count, source_count, generated_count, suppressed_count,
     transition_count, metric_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20, $21, $22, $23::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash, policy.BaselineTick,
		policy.AssignmentContract, policy.SourceContract, policy.PeriodTicks, policy.MaximumAssignments,
		policy.MaximumTransitionsTick, policy.MaximumGenerationsTick, policy.AssignmentCount,
		policy.ActiveAssignmentCount, policy.SuspendedAssignmentCount, policy.SupersededAssignmentCount,
		policy.TerminatedAssignmentCount, policy.SourceCount, policy.GeneratedCount, policy.SuppressedCount,
		policy.TransitionCount, policy.MetricCount, policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore open-world V14 commute lifecycle profile: %w", err)
	}
	count++

	for _, assignment := range lifecycle.Assignments {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, assignment.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world V14 assignment %s: %w", assignment.Code, actorErr)
		}
		openedFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, assignment.OpenedFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world V14 assignment %s opened fact: %w", assignment.Code, factErr)
		}
		var endpointsValid bool
		if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM city_open_world_commute_bindings binding
    JOIN city_open_world_facilities home
      ON home.world_id = binding.world_id AND home.code = $4 AND home.state = 'active'
    JOIN city_open_world_mobility_hubs home_hub
      ON home_hub.world_id = home.world_id AND home_hub.code = $5
     AND home_hub.facility_id = home.id AND home_hub.hub_kind = 'facility'
    JOIN city_open_world_facilities work
      ON work.world_id = binding.world_id AND work.code = $6 AND work.state = 'active'
    JOIN city_open_world_mobility_hubs work_hub
      ON work_hub.world_id = work.world_id AND work_hub.code = $7
     AND work_hub.facility_id = work.id AND work_hub.hub_kind = 'facility'
    WHERE binding.world_id = $1 AND binding.code = $2 AND binding.actor_id = $3
      AND home.facility_type_code = 'residence'
      AND work.facility_type_code <> 'residence'
)`, worldID, assignment.BindingCode, actorID, assignment.HomeFacilityCode,
			assignment.HomeHubCode, assignment.WorkFacilityCode, assignment.WorkHubCode).Scan(&endpointsValid); err != nil {
			return count, fmt.Errorf("verify open-world V14 assignment %s endpoints: %w", assignment.Code, err)
		}
		if !endpointsValid {
			return count, fmt.Errorf("restore open-world V14 assignment %s has invalid endpoints", assignment.Code)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_assignment_epochs
    (world_id, code, binding_code, actor_id, epoch_number, assignment_kind,
     employment_role_code, home_facility_code, home_hub_code, work_facility_code,
     work_hub_code, period_ticks, outbound_phase, return_phase, origin_kind,
     opened_tick, opened_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18::jsonb)`,
			worldID, assignment.Code, assignment.BindingCode, actorID, assignment.EpochNumber,
			assignment.AssignmentKind, assignment.EmploymentRole, assignment.HomeFacilityCode,
			assignment.HomeHubCode, assignment.WorkFacilityCode, assignment.WorkHubCode,
			assignment.PeriodTicks, assignment.OutboundPhase, assignment.ReturnPhase,
			assignment.OriginKind, assignment.OpenedTick, openedFactID, []byte(assignment.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V14 assignment %s: %w", assignment.Code, err)
		}
		count++
	}

	for _, transition := range lifecycle.Transitions {
		sourceFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, transition.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world V14 transition %s: %w", transition.AssignmentCode, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_assignment_transitions
    (world_id, assignment_code, transition_tick, transition_sequence, state,
     reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			worldID, transition.AssignmentCode, transition.TransitionTick, transition.TransitionSeq,
			transition.State, transition.ReasonCode, sourceFactID, []byte(transition.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V14 transition %s: %w", transition.AssignmentCode, err)
		}
		count++
	}

	for _, source := range lifecycle.Sources {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, source.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world V14 source %s: %w", source.Code, actorErr)
		}
		lastFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, source.LastFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world V14 source %s last fact: %w", source.Code, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_lifecycle_sources
    (world_id, code, assignment_code, binding_code, actor_id, source_kind, direction,
     employment_role_code, origin_facility_code, origin_hub_code, destination_facility_code,
     destination_hub_code, mode_code, purpose_code, requested_units, status, period_ticks,
     phase_offset, next_due_tick, last_transition_tick, last_fact_id, generated_count,
     suppressed_count, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25::jsonb)`,
			worldID, source.Code, source.AssignmentCode, source.BindingCode, actorID,
			source.SourceKind, source.Direction, source.EmploymentRoleCode,
			source.OriginFacilityCode, source.OriginHubCode, source.DestinationFacilityCode,
			source.DestinationHubCode, source.ModeCode, source.PurposeCode, source.RequestedUnits,
			source.Status, source.PeriodTicks, source.PhaseOffset, source.NextDueTick,
			source.LastTransitionTick, lastFactID, source.GeneratedCount, source.SuppressedCount,
			source.Version, []byte(source.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V14 source %s: %w", source.Code, err)
		}
		count++
	}

	for _, metric := range lifecycle.Metrics {
		sourceFactID, factErr := requireCityOpenWorldRecoveryFactID(factIDs, metric.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world V14 metric %d source fact: %w", metric.CycleStartTick, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_lifecycle_cycle_metrics
    (world_id, cycle_start_tick, cycle_end_tick, closed_tick, source_fact_id,
     transition_count, rebind_count, generated_count, suppressed_count,
     scheduled_demand_count, completed_demand_count, expired_demand_count,
     pending_demand_count, arrival_landed_count, arrival_blocked_count,
     arrival_failed_count, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17::jsonb)`,
			worldID, metric.CycleStartTick, metric.CycleEndTick, metric.ClosedTick,
			sourceFactID, metric.TransitionCount, metric.RebindCount, metric.GeneratedCount,
			metric.SuppressedCount, metric.ScheduledDemandCount, metric.CompletedDemandCount,
			metric.ExpiredDemandCount, metric.PendingDemandCount, metric.ArrivalLandedCount,
			metric.ArrivalBlockedCount, metric.ArrivalFailedCount, []byte(metric.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V14 metric %d: %w", metric.CycleStartTick, err)
		}
		count++
	}
	return count, nil
}
