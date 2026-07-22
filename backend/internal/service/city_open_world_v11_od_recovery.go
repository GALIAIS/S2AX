package service

import (
	"context"
	"database/sql"
	"fmt"
)

// restoreCityOpenWorldMobilityODProjection runs only after V5 actors and
// facilities, V9 mobility topology, V10 arrival policy, and the runtime fact
// ledger have been restored. The snapshot intentionally stores stable codes
// and fact references rather than database surrogate IDs.
func restoreCityOpenWorldMobilityODProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	od CityOpenWorldMobilityODState,
	actorIDs map[string]int64,
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
) (int, error) {
	if err := validateCityOpenWorldMobilityODState(&od); err != nil {
		return 0, fmt.Errorf("validate V11 OD recovery input: %w", err)
	}
	count := 0
	policy := od.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_od_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     generation_contract, metric_contract, cycle_ticks, maximum_generations_tick,
     source_count, generated_count, suppressed_count, metric_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
        $10, $11, $12, $13, $14, $15::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash,
		policy.BaselineTick, policy.GenerationContract, policy.MetricContract,
		policy.CycleTicks, policy.MaximumGenerationsTick, policy.SourceCount,
		policy.GeneratedCount, policy.SuppressedCount, policy.MetricCount,
		policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore open-world V11 OD profile: %w", err)
	}
	count++
	for _, source := range od.Sources {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, source.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world V11 OD source %s: %w", source.Code, actorErr)
		}
		var destinationValid bool
		if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM city_open_world_facilities facility
    JOIN city_open_world_mobility_hubs hub
      ON hub.world_id = facility.world_id
     AND hub.facility_id = facility.id
     AND hub.facility_code = facility.code
    WHERE facility.world_id = $1
      AND facility.code = $2
      AND hub.code = $3
      AND hub.hub_kind = 'facility'
)`, worldID, source.DestinationFacilityCode, source.DestinationHubCode).Scan(&destinationValid); err != nil {
			return count, fmt.Errorf("verify open-world V11 OD source %s destination: %w", source.Code, err)
		}
		if !destinationValid {
			return count, fmt.Errorf("restore open-world V11 OD source %s has invalid destination binding", source.Code)
		}
		lastFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, source.LastFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world V11 OD source %s last fact: %w", source.Code, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_od_sources
    (world_id, code, source_kind, actor_id, destination_facility_code,
     destination_hub_code, mode_code, purpose_code, requested_units, status,
     period_ticks, phase_offset, next_due_tick, last_transition_tick,
     last_fact_id, generated_count, suppressed_count, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
        $11, $12, $13, $14, $15, $16, $17, $18, $19::jsonb)`,
			worldID, source.Code, source.SourceKind, actorID,
			source.DestinationFacilityCode, source.DestinationHubCode,
			source.ModeCode, source.PurposeCode, source.RequestedUnits, source.Status,
			source.PeriodTicks, source.PhaseOffset, source.NextDueTick,
			source.LastTransitionTick, lastFactID, source.GeneratedCount,
			source.SuppressedCount, source.Version, []byte(source.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V11 OD source %s: %w", source.Code, err)
		}
		count++
	}
	for _, metric := range od.Metrics {
		sourceFactID, factErr := requireCityOpenWorldRecoveryFactID(factIDs, metric.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world V11 OD cycle %d source fact: %w", metric.CycleStartTick, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_od_cycle_metrics
    (world_id, cycle_start_tick, cycle_end_tick, closed_tick, source_fact_id,
     generated_count, suppressed_count, network_requested_count,
     network_scheduled_count, network_completed_count, network_expired_count,
     pending_demand_count, arrival_landed_count, arrival_blocked_count,
     arrival_failed_count, travel_ticks_total, congestion_ticks_total,
     peak_occupancy_milli, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17, $18, $19::jsonb)`,
			worldID, metric.CycleStartTick, metric.CycleEndTick, metric.ClosedTick,
			sourceFactID, metric.GeneratedCount, metric.SuppressedCount,
			metric.NetworkRequested, metric.NetworkScheduled, metric.NetworkCompleted,
			metric.NetworkExpired, metric.PendingDemandCount, metric.ArrivalLanded,
			metric.ArrivalBlocked, metric.ArrivalFailed, metric.TravelTicksTotal,
			metric.CongestionTicksTotal, metric.PeakOccupancyMilli, []byte(metric.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V11 OD cycle %d: %w", metric.CycleStartTick, err)
		}
		count++
	}
	return count, nil
}
