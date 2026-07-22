package service

import (
	"context"
	"database/sql"
	"fmt"
)

// restoreCityOpenWorldCommuteSourceProjection runs after V12 bindings, V9
// mobility topology, V10 arrivals, and the runtime fact ledger are restored.
// It resolves all database IDs from stable snapshot codes/fact references so
// recovery never makes surrogate identity part of the canonical contract.
func restoreCityOpenWorldCommuteSourceProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	sources CityOpenWorldCommuteSourceState,
	actorIDs map[string]int64,
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
) (int, error) {
	if err := validateCityOpenWorldCommuteSourceState(&sources); err != nil {
		return 0, fmt.Errorf("validate V13 commute source recovery input: %w", err)
	}
	count := 0
	policy := sources.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_source_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     generation_contract, origin_contract, period_ticks, surface_egress_radius,
     maximum_generations_tick, source_count, generated_count, suppressed_count,
     metric_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
        $11, $12, $13, $14, $15, $16::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash,
		policy.BaselineTick, policy.GenerationContract, policy.OriginContract,
		policy.PeriodTicks, policy.SurfaceEgressRadius, policy.MaximumGenerationsTick,
		policy.SourceCount, policy.GeneratedCount, policy.SuppressedCount,
		policy.MetricCount, policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore open-world V13 commute source profile: %w", err)
	}
	count++
	for _, source := range sources.Sources {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, source.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world V13 commute source %s: %w", source.Code, actorErr)
		}
		var endpointValid bool
		if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM city_open_world_commute_bindings binding
    JOIN city_open_world_facilities origin_facility
      ON origin_facility.world_id = binding.world_id AND origin_facility.code = $4
    JOIN city_open_world_mobility_hubs origin_hub
      ON origin_hub.world_id = binding.world_id AND origin_hub.code = $5
    JOIN city_open_world_facilities destination_facility
      ON destination_facility.world_id = binding.world_id AND destination_facility.code = $6
    JOIN city_open_world_mobility_hubs destination_hub
      ON destination_hub.world_id = binding.world_id AND destination_hub.code = $7
    WHERE binding.world_id = $1 AND binding.code = $2 AND binding.actor_id = $3
      AND origin_hub.hub_kind = 'facility' AND origin_hub.facility_code = origin_facility.code
      AND destination_hub.hub_kind = 'facility' AND destination_hub.facility_code = destination_facility.code
)`, worldID, source.BindingCode, actorID, source.OriginFacilityCode,
			source.OriginHubCode, source.DestinationFacilityCode, source.DestinationHubCode).Scan(&endpointValid); err != nil {
			return count, fmt.Errorf("verify open-world V13 commute source %s endpoints: %w", source.Code, err)
		}
		if !endpointValid {
			return count, fmt.Errorf("restore open-world V13 commute source %s has invalid binding endpoints", source.Code)
		}
		lastFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, source.LastFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world V13 commute source %s last fact: %w", source.Code, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_sources
    (world_id, code, binding_code, source_kind, direction, actor_id,
     employment_role_code, origin_facility_code, origin_hub_code,
     destination_facility_code, destination_hub_code, mode_code, purpose_code,
     requested_units, status, period_ticks, phase_offset, next_due_tick,
     last_transition_tick, last_fact_id, generated_count, suppressed_count,
     version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24::jsonb)`,
			worldID, source.Code, source.BindingCode, source.SourceKind, source.Direction,
			actorID, source.EmploymentRoleCode, source.OriginFacilityCode,
			source.OriginHubCode, source.DestinationFacilityCode, source.DestinationHubCode,
			source.ModeCode, source.PurposeCode, source.RequestedUnits, source.Status,
			source.PeriodTicks, source.PhaseOffset, source.NextDueTick,
			source.LastTransitionTick, lastFactID, source.GeneratedCount,
			source.SuppressedCount, source.Version, []byte(source.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V13 commute source %s: %w", source.Code, err)
		}
		count++
	}
	for _, metric := range sources.Metrics {
		sourceFactID, factErr := requireCityOpenWorldRecoveryFactID(factIDs, metric.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world V13 commute cycle %d source fact: %w", metric.CycleStartTick, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_cycle_metrics
    (world_id, cycle_start_tick, cycle_end_tick, closed_tick, source_fact_id,
     outbound_generated_count, outbound_suppressed_count,
     outbound_origin_unavailable_count, return_generated_count,
     return_suppressed_count, return_origin_unavailable_count,
     scheduled_demand_count, completed_demand_count, expired_demand_count,
     pending_demand_count, arrival_landed_count, arrival_blocked_count,
     arrival_failed_count, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17, $18, $19::jsonb)`,
			worldID, metric.CycleStartTick, metric.CycleEndTick, metric.ClosedTick,
			sourceFactID, metric.OutboundGeneratedCount, metric.OutboundSuppressedCount,
			metric.OutboundOriginUnavailableCount, metric.ReturnGeneratedCount,
			metric.ReturnSuppressedCount, metric.ReturnOriginUnavailableCount,
			metric.ScheduledDemandCount, metric.CompletedDemandCount,
			metric.ExpiredDemandCount, metric.PendingDemandCount,
			metric.ArrivalLandedCount, metric.ArrivalBlockedCount,
			metric.ArrivalFailedCount, []byte(metric.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V13 commute cycle %d: %w", metric.CycleStartTick, err)
		}
		count++
	}
	return count, nil
}
