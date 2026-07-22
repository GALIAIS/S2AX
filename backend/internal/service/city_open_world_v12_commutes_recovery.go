package service

import (
	"context"
	"database/sql"
	"fmt"
)

// restoreCityOpenWorldCommuteProjection restores the V12 immutable binding
// bridge after its V5 identities and V9 facility hubs have been restored. The
// snapshot carries stable codes only; surrogate IDs are resolved inside this
// transaction and are never part of replay identity.
func restoreCityOpenWorldCommuteProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	commutes CityOpenWorldCommuteState,
	actorIDs map[string]int64,
) (int, error) {
	if err := validateCityOpenWorldCommuteState(&commutes); err != nil {
		return 0, fmt.Errorf("validate V12 commute recovery input: %w", err)
	}
	count := 0
	policy := commutes.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     assignment_contract, period_ticks, maximum_bindings, candidate_count,
     binding_count, unbound_candidate_count, residence_count,
     used_residence_units, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash,
		policy.BaselineTick, policy.AssignmentContract, policy.PeriodTicks,
		policy.MaximumBindings, policy.CandidateCount, policy.BindingCount,
		policy.UnboundCandidateCount, policy.ResidenceCount,
		policy.UsedResidenceUnits, policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore open-world V12 commute profile: %w", err)
	}
	count++

	for _, binding := range commutes.Bindings {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, binding.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world V12 commute binding %s: %w", binding.Code, actorErr)
		}
		var valid bool
		if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM city_open_world_facilities home
    JOIN city_open_world_mobility_hubs home_hub
      ON home_hub.world_id = home.world_id
     AND home_hub.facility_id = home.id
     AND home_hub.facility_code = home.code
     AND home_hub.hub_kind = 'facility'
    JOIN city_open_world_facilities work
      ON work.world_id = home.world_id
    JOIN city_open_world_mobility_hubs work_hub
      ON work_hub.world_id = work.world_id
     AND work_hub.facility_id = work.id
     AND work_hub.facility_code = work.code
     AND work_hub.hub_kind = 'facility'
    WHERE home.world_id = $1
      AND home.code = $2 AND home_hub.code = $3
      AND work.code = $4 AND work_hub.code = $5
	  AND home.facility_type_code = 'residence'
	  AND work.facility_type_code <> 'residence'
)`, worldID, binding.HomeFacilityCode, binding.HomeHubCode,
			binding.WorkFacilityCode, binding.WorkHubCode).Scan(&valid); err != nil {
			return count, fmt.Errorf("verify open-world V12 commute binding %s facilities: %w", binding.Code, err)
		}
		if !valid {
			return count, fmt.Errorf("restore open-world V12 commute binding %s has invalid facility binding", binding.Code)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_bindings
    (world_id, code, binding_kind, actor_id, employment_role_code,
     home_facility_code, home_hub_code, work_facility_code, work_hub_code,
     period_ticks, outbound_phase, return_phase, status, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb)`,
			worldID, binding.Code, binding.BindingKind, actorID, binding.EmploymentRole,
			binding.HomeFacilityCode, binding.HomeHubCode, binding.WorkFacilityCode,
			binding.WorkHubCode, binding.PeriodTicks, binding.OutboundPhase,
			binding.ReturnPhase, binding.Status, binding.Version, []byte(binding.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V12 commute binding %s: %w", binding.Code, err)
		}
		count++
	}
	return count, nil
}
