package service

import (
	"context"
	"database/sql"
	"fmt"
)

// restoreCityOpenWorldEffectiveCapacityProjection runs after V9 routes and
// V20 infrastructure have both been restored. It deliberately resolves every
// foreign key by canonical route/fact identity instead of serializing storage
// IDs into a snapshot.
func restoreCityOpenWorldEffectiveCapacityProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	effectiveCapacity CityOpenWorldEffectiveCapacityState,
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
) (int, error) {
	if err := validateCityOpenWorldEffectiveCapacityState(&effectiveCapacity); err != nil {
		return 0, fmt.Errorf("validate V21 effective-capacity recovery input: %w", err)
	}
	if err := activateCityOpenWorldEffectiveCapacityRecoveryWrite(ctx, tx, worldID); err != nil {
		return 0, err
	}
	count := 0
	policy := effectiveCapacity.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_effective_capacity_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     topology_contract, asset_contract, admission_contract, visibility_contract,
     maximum_admissions, admission_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash,
		policy.BaselineTick, policy.TopologyContract, policy.AssetContract,
		policy.AdmissionContract, policy.VisibilityContract, policy.MaximumAdmissions,
		policy.AdmissionCount, policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore V21 effective-capacity profile: %w", err)
	}
	count++
	for _, admission := range effectiveCapacity.Admissions {
		var routeID int64
		if err := tx.QueryRowContext(ctx, `
SELECT id
FROM city_open_world_mobility_routes
WHERE world_id = $1 AND code = $2`, worldID, admission.RouteCode).Scan(&routeID); err != nil {
			return count, fmt.Errorf("restore V21 admission route %s: %w", admission.RouteCode, err)
		}
		stateSourceFactID, stateFactErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, admission.StateSourceFact)
		if stateFactErr != nil {
			return count, fmt.Errorf("restore V21 admission %s state source fact: %w", admission.EdgeCode, stateFactErr)
		}
		scheduleFactID, scheduleFactErr := requireCityOpenWorldRecoveryFactID(factIDs, admission.ScheduleFact)
		if scheduleFactErr != nil {
			return count, fmt.Errorf("restore V21 admission %s schedule fact: %w", admission.EdgeCode, scheduleFactErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_effective_capacity_admissions
    (world_id, route_id, edge_code, departure_tick, corridor_code, asset_code,
     asset_state, state_effective_tick, state_source_fact_id, schedule_fact_id,
     baseline_capacity_units_per_tick, capacity_milli,
     effective_capacity_units_per_tick, allocated_units, occupancy_milli,
     delay_ticks, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17::jsonb)`,
			worldID, routeID, admission.EdgeCode, admission.DepartureTick,
			admission.CorridorCode, admission.AssetCode, admission.AssetState,
			admission.StateEffectiveTick, stateSourceFactID, scheduleFactID,
			admission.BaselineCapacityUnitsPerTick, admission.CapacityMilli,
			admission.EffectiveCapacityUnitsPerTick, admission.AllocatedUnits,
			admission.OccupancyMilli, admission.DelayTicks, []byte(admission.Metadata)); err != nil {
			return count, fmt.Errorf("restore V21 effective-capacity admission %s/%s: %w", admission.RouteCode, admission.EdgeCode, err)
		}
		count++
	}
	if err := assertCityOpenWorldEffectiveCapacityFoundation(ctx, tx, worldID); err != nil {
		return count, fmt.Errorf("validate restored V21 effective-capacity foundation: %w", err)
	}
	return count, nil
}
