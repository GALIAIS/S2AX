package service

import (
	"context"
	"database/sql"
	"fmt"
)

func requireCityOpenWorldRecoveryResponseID(responseIDs map[string]int64, code string) (int64, error) {
	id, found := responseIDs[code]
	if !found || id <= 0 {
		return 0, fmt.Errorf("unknown service response %s", code)
	}
	return id, nil
}

func requireCityOpenWorldRecoveryImpactEffectID(effectIDs map[string]int64, code string) (int64, error) {
	id, found := effectIDs[code]
	if !found || id <= 0 {
		return 0, fmt.Errorf("unknown impact effect %s", code)
	}
	return id, nil
}

// restoreCityOpenWorldImpactProjection rebuilds only V8's mutable bridge
// state. It receives IDs produced by the V7/runtime restore instead of
// relying on historical database primary keys, so a recovered snapshot stays
// deterministic even when physical IDs are regenerated.
func restoreCityOpenWorldImpactProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	impacts CityOpenWorldImpactState,
	actorIDs map[string]int64,
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
	responseIDs map[string]int64,
) (int, error) {
	if err := validateCityOpenWorldImpactState(&impacts); err != nil {
		return 0, fmt.Errorf("validate V8 impact recovery input: %w", err)
	}
	count := 0
	policy := impacts.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_impact_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     source_contract_version, delivery_contract_version, maximum_schedules_per_tick,
     effect_count, applied_count, metric_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash, policy.BaselineTick,
		policy.SourceContractVersion, policy.DeliveryContractVersion, policy.MaximumSchedulesPerTick,
		policy.EffectCount, policy.AppliedCount, policy.MetricCount, policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore open-world V8 impact profile: %w", err)
	}
	count++
	for _, entry := range impacts.Catalog {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_impact_catalog
    (world_id, code, source_kind, service_code, outcome, target_domain,
     metric_code, delta_units_per_source_unit, definition_version,
     content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)`,
			worldID, entry.Code, entry.SourceKind, entry.ServiceCode, entry.Outcome,
			entry.TargetDomain, entry.MetricCode, entry.DeltaUnitsPerSourceUnit,
			entry.Version, entry.ContentHash, []byte(entry.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V8 impact catalog %s: %w", entry.Code, err)
		}
		count++
	}
	effectIDs := make(map[string]int64, len(impacts.Effects))
	for _, effect := range impacts.Effects {
		responseID, responseErr := requireCityOpenWorldRecoveryResponseID(responseIDs, effect.SourceResponseCode)
		if responseErr != nil {
			return count, fmt.Errorf("restore open-world V8 impact %s: %w", effect.Code, responseErr)
		}
		sourceFactID, factErr := requireCityOpenWorldRecoveryFactID(factIDs, effect.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world V8 impact %s: %w", effect.Code, factErr)
		}
		targetActorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, effect.TargetCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world V8 impact %s: %w", effect.Code, actorErr)
		}
		applicationFactID, applicationErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, effect.ApplicationFact)
		if applicationErr != nil {
			return count, fmt.Errorf("restore open-world V8 impact %s: %w", effect.Code, applicationErr)
		}
		var effectID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_impact_effects
    (world_id, code, source_response_id, source_fact_id, catalog_code,
     target_actor_id, target_domain, target_code, metric_code, source_units,
     delta_units, scheduled_tick, effective_tick, status, applied_tick,
     application_fact_id, before_units, after_units, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20::jsonb)
RETURNING id`,
			worldID, effect.Code, responseID, sourceFactID, effect.CatalogCode,
			targetActorID, effect.TargetDomain, effect.TargetCode, effect.MetricCode,
			effect.SourceUnits, effect.DeltaUnits, effect.ScheduledTick, effect.EffectiveTick,
			effect.Status, cityNullableInt64(effect.AppliedTick), applicationFactID,
			cityNullableInt64(effect.BeforeUnits), cityNullableInt64(effect.AfterUnits),
			effect.Version, []byte(effect.Metadata)).Scan(&effectID); err != nil {
			return count, fmt.Errorf("restore open-world V8 impact effect %s: %w", effect.Code, err)
		}
		effectIDs[effect.Code] = effectID
		count++
	}
	for _, metric := range impacts.Metrics {
		lastEffectID, effectErr := requireCityOpenWorldRecoveryImpactEffectID(effectIDs, metric.LastEffectCode)
		if effectErr != nil {
			return count, fmt.Errorf("restore open-world V8 impact metric %s/%s: %w", metric.TargetCode, metric.MetricCode, effectErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_impact_metrics
    (world_id, target_domain, target_code, metric_code, value_units,
     last_applied_tick, last_effect_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`,
			worldID, metric.TargetDomain, metric.TargetCode, metric.MetricCode,
			metric.ValueUnits, metric.LastAppliedTick, lastEffectID, metric.Version,
			[]byte(metric.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V8 impact metric %s/%s: %w", metric.TargetCode, metric.MetricCode, err)
		}
		count++
	}
	return count, nil
}
