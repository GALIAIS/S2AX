package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// restoreCityOpenWorldMobilityArrivalProjection runs after V9 mobility has
// restored routes/demands and after the runtime fact ledger is available. It
// deliberately resolves all IDs from canonical code/fact references instead
// of persisting database surrogate IDs in snapshots.
func restoreCityOpenWorldMobilityArrivalProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	arrivals CityOpenWorldMobilityArrivalState,
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
) (int, error) {
	if err := validateCityOpenWorldMobilityArrivalState(&arrivals); err != nil {
		return 0, fmt.Errorf("validate V10 arrival recovery input: %w", err)
	}
	count := 0
	policy := arrivals.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_arrival_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     bridge_contract, landing_contract, maximum_arrivals_per_tick,
     landing_search_radius, maximum_blocked_attempts, arrival_count,
     landed_count, blocked_count, failed_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
        $11, $12, $13, $14, $15, $16::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash, policy.BaselineTick,
		policy.BridgeContract, policy.LandingContract, policy.MaximumArrivalsPerTick,
		policy.LandingSearchRadius, policy.MaximumBlockedAttempts, policy.ArrivalCount,
		policy.LandedCount, policy.BlockedCount, policy.FailedCount, policy.Revision,
		[]byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore open-world V10 arrival profile: %w", err)
	}
	count++
	for _, arrival := range arrivals.Arrivals {
		var routeID, demandID, actorID int64
		if err := tx.QueryRowContext(ctx, `
SELECT route.id, demand.id, actor.id
FROM city_open_world_mobility_routes route
JOIN city_open_world_mobility_demands demand
  ON demand.id = route.demand_id AND demand.world_id = route.world_id
JOIN city_open_world_actors actor
  ON actor.id = route.actor_id AND actor.world_id = route.world_id
WHERE route.world_id = $1 AND route.code = $2 AND demand.code = $3 AND actor.code = $4`,
			worldID, arrival.RouteCode, arrival.DemandCode, arrival.ActorCode,
		).Scan(&routeID, &demandID, &actorID); err != nil {
			return count, fmt.Errorf("restore open-world V10 arrival %s references unknown route/demand/actor: %w", arrival.Code, err)
		}
		sourceFactID, err := requireCityOpenWorldRecoveryFactID(factIDs, arrival.SourceFact)
		if err != nil {
			return count, fmt.Errorf("restore open-world V10 arrival %s source fact: %w", arrival.Code, err)
		}
		lastFactID, err := requireCityOpenWorldRecoveryFactID(factIDs, arrival.LastFact)
		if err != nil {
			return count, fmt.Errorf("restore open-world V10 arrival %s last fact: %w", arrival.Code, err)
		}
		landingFactID, err := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, arrival.LandingFact)
		if err != nil {
			return count, fmt.Errorf("restore open-world V10 arrival %s landing fact: %w", arrival.Code, err)
		}
		expectedRaw, marshalErr := json.Marshal(arrival.ExpectedOrigin)
		if marshalErr != nil {
			return count, fmt.Errorf("marshal open-world V10 arrival %s expected origin: %w", arrival.Code, marshalErr)
		}
		var landingRaw any
		if arrival.LandingLocation != nil {
			raw, landingErr := json.Marshal(*arrival.LandingLocation)
			if landingErr != nil {
				return count, fmt.Errorf("marshal open-world V10 arrival %s landing location: %w", arrival.Code, landingErr)
			}
			landingRaw = raw
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_arrivals
    (world_id, code, route_id, demand_id, actor_id, destination_hub_code,
     expected_origin, landing_location, status, blocked_attempts,
     next_attempt_tick, created_tick, updated_tick, source_fact_id, last_fact_id,
     landing_fact_id, landed_tick, failed_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10,
        $11, $12, $13, $14, $15, $16, $17, $18, $19, $20::jsonb)`,
			worldID, arrival.Code, routeID, demandID, actorID, arrival.DestinationHubCode,
			expectedRaw, landingRaw, arrival.Status, arrival.BlockedAttempts,
			arrival.NextAttemptTick, arrival.CreatedTick, arrival.UpdatedTick, sourceFactID, lastFactID,
			landingFactID, cityNullableInt64(arrival.LandedTick), cityNullableInt64(arrival.FailedTick),
			arrival.Version, []byte(arrival.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V10 arrival %s: %w", arrival.Code, err)
		}
		count++
	}
	return count, nil
}
