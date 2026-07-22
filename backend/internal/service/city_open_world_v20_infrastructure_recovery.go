package service

import (
	"context"
	"database/sql"
	"fmt"
)

// restoreCityOpenWorldInfrastructureProjection restores V20 after runtime
// facts and V19 topology have been restored. Source fact IDs are resolved
// solely from canonical (tick, sequence) identities.
func restoreCityOpenWorldInfrastructureProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	infrastructure CityOpenWorldInfrastructureState,
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
) (int, error) {
	if err := validateCityOpenWorldInfrastructureState(&infrastructure); err != nil {
		return 0, fmt.Errorf("validate V20 infrastructure recovery input: %w", err)
	}
	if err := activateCityOpenWorldInfrastructureRecoveryWrite(ctx, tx, worldID); err != nil {
		return 0, err
	}
	count := 0
	policy := infrastructure.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_infrastructure_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     asset_contract, state_contract, maximum_assets, asset_count, node_asset_count,
     segment_asset_count, transition_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash, policy.BaselineTick,
		policy.AssetContract, policy.StateContract, policy.MaximumAssets, policy.AssetCount,
		policy.NodeAssetCount, policy.SegmentAssetCount, policy.TransitionCount,
		policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore V20 infrastructure profile: %w", err)
	}
	count++
	for _, asset := range infrastructure.Assets {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_infrastructure_assets
    (world_id, code, asset_kind, spatial_node_code, spatial_corridor_code,
     segment_ordinal, asset_class, definition_version, content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, asset.Code, asset.AssetKind, cityOpenWorldNullableString(asset.SpatialNodeCode),
			cityOpenWorldNullableString(asset.SpatialCorridorCode), asset.SegmentOrdinal,
			asset.AssetClass, asset.DefinitionVersion, asset.ContentHash, []byte(asset.Metadata)); err != nil {
			return count, fmt.Errorf("restore V20 infrastructure asset %s: %w", asset.Code, err)
		}
		count++
	}
	for _, current := range infrastructure.States {
		sourceFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, current.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore V20 infrastructure state %s source fact: %w", current.AssetCode, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_infrastructure_asset_states
    (world_id, asset_code, state, capacity_milli, effective_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			worldID, current.AssetCode, current.State, current.CapacityMilli, current.EffectiveTick,
			sourceFactID, current.Version, []byte(current.Metadata)); err != nil {
			return count, fmt.Errorf("restore V20 infrastructure state %s: %w", current.AssetCode, err)
		}
		count++
	}
	for _, transition := range infrastructure.Transitions {
		sourceFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, transition.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore V20 infrastructure transition %s source fact: %w", transition.AssetCode, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_infrastructure_asset_transitions
    (world_id, asset_code, transition_tick, transition_sequence, from_state,
     to_state, capacity_milli, reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, transition.AssetCode, transition.TransitionTick, transition.TransitionSeq,
			transition.FromState, transition.ToState, transition.CapacityMilli,
			transition.ReasonCode, sourceFactID, []byte(transition.Metadata)); err != nil {
			return count, fmt.Errorf("restore V20 infrastructure transition %s: %w", transition.AssetCode, err)
		}
		count++
	}
	if err := assertCityOpenWorldInfrastructureFoundation(ctx, tx, worldID); err != nil {
		return count, fmt.Errorf("validate restored V20 infrastructure foundation: %w", err)
	}
	return count, nil
}
