package service

import (
	"context"
	"database/sql"
	"fmt"
)

func requireCityOpenWorldRecoveryMobilityRouteID(routeIDs map[string]int64, code string) (int64, error) {
	id, found := routeIDs[code]
	if !found || id <= 0 {
		return 0, fmt.Errorf("unknown mobility route %s", code)
	}
	return id, nil
}

// restoreCityOpenWorldMobilityProjection restores the V9 topology and its
// mutable demand evidence after V5/V7/V8 have restored their prerequisite
// actors, facilities, runtime facts, services, and impacts. Routes and
// demands form a deliberately checked cycle in storage; route IDs are reserved
// first so a completed/scheduled demand can retain its mandatory route link
// without weakening that lifecycle invariant during recovery.
func restoreCityOpenWorldMobilityProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	mobility CityOpenWorldMobilityState,
	actorIDs, facilityIDs map[string]int64,
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
) (int, error) {
	if err := validateCityOpenWorldMobilityState(&mobility); err != nil {
		return 0, fmt.Errorf("validate V9 mobility recovery input: %w", err)
	}
	count := 0
	policy := mobility.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     topology_contract_version, scheduling_contract, maximum_schedules_per_tick,
     maximum_wait_ticks, mode_count, hub_count, edge_count, demand_count,
     route_count, allocation_count, completed_count, expired_count,
     actor_metric_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash, policy.BaselineTick,
		policy.TopologyContractVersion, policy.SchedulingContract, policy.MaximumSchedulesPerTick,
		policy.MaximumWaitTicks, policy.ModeCount, policy.HubCount, policy.EdgeCount,
		policy.DemandCount, policy.RouteCount, policy.AllocationCount, policy.CompletedCount,
		policy.ExpiredCount, policy.ActorMetricCount, policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore open-world V9 mobility profile: %w", err)
	}
	count++
	for _, mode := range mobility.Modes {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_modes
    (world_id, code, unit_kind, speed_units_per_tick, capacity_units_per_tick,
     congestion_threshold_milli, maximum_delay_ticks, definition_version,
     content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, mode.Code, mode.UnitKind, mode.SpeedUnitsPerTick, mode.CapacityUnitsPerTick,
			mode.CongestionThresholdMilli, mode.MaximumDelayTicks, mode.Version, mode.ContentHash,
			[]byte(mode.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V9 mobility mode %s: %w", mode.Code, err)
		}
		count++
	}
	for _, hub := range mobility.Hubs {
		var facilityID any
		if hub.FacilityCode != nil {
			value, found := facilityIDs[*hub.FacilityCode]
			if !found || value <= 0 {
				return count, fmt.Errorf("restore open-world V9 mobility hub %s: unknown facility %s", hub.Code, *hub.FacilityCode)
			}
			facilityID = value
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_hubs
    (world_id, code, hub_kind, facility_id, facility_code, zone_x, zone_y,
     anchor_x, anchor_y, anchor_z, definition_version, content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
			worldID, hub.Code, hub.HubKind, facilityID, cityOpenWorldNullableString(hub.FacilityCode),
			hub.ZoneX, hub.ZoneY, hub.AnchorX, hub.AnchorY, hub.AnchorZ, hub.Version,
			hub.ContentHash, []byte(hub.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V9 mobility hub %s: %w", hub.Code, err)
		}
		count++
	}
	for _, edge := range mobility.Edges {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_edges
    (world_id, code, mode_code, from_hub_code, to_hub_code, tier,
     distance_units, base_travel_ticks, capacity_units_per_tick,
     definition_version, content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
			worldID, edge.Code, edge.ModeCode, edge.FromHubCode, edge.ToHubCode, edge.Tier,
			edge.DistanceUnits, edge.BaseTravelTicks, edge.CapacityUnitsPerTick,
			edge.Version, edge.ContentHash, []byte(edge.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V9 mobility edge %s: %w", edge.Code, err)
		}
		count++
	}

	routeIDs := make(map[string]int64, len(mobility.Routes))
	for _, route := range mobility.Routes {
		var routeID int64
		if err := tx.QueryRowContext(ctx, `
SELECT nextval(pg_get_serial_sequence('city_open_world_mobility_routes', 'id'))`).Scan(&routeID); err != nil {
			return count, fmt.Errorf("reserve open-world V9 mobility route %s: %w", route.Code, err)
		}
		routeIDs[route.Code] = routeID
	}
	demandIDs := make(map[string]int64, len(mobility.Demands))
	for _, demand := range mobility.Demands {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, demand.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world V9 mobility demand %s: %w", demand.Code, actorErr)
		}
		sourceFactID, factErr := requireCityOpenWorldRecoveryFactID(factIDs, demand.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world V9 mobility demand %s: %w", demand.Code, factErr)
		}
		lastFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, demand.LastFact)
		if factErr != nil || lastFactID == nil {
			return count, fmt.Errorf("restore open-world V9 mobility demand %s: invalid last fact: %w", demand.Code, factErr)
		}
		var routeID any
		if demand.RouteCode != nil {
			resolved, routeErr := requireCityOpenWorldRecoveryMobilityRouteID(routeIDs, *demand.RouteCode)
			if routeErr != nil {
				return count, fmt.Errorf("restore open-world V9 mobility demand %s: %w", demand.Code, routeErr)
			}
			routeID = resolved
		}
		var demandID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_mobility_demands
    (world_id, code, actor_id, source_hub_code, destination_hub_code, mode_code,
     purpose_code, requested_units, requested_tick, earliest_departure_tick,
     deadline_tick, status, source_fact_id, last_fact_id, route_id,
     scheduled_tick, completed_tick, expired_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20::jsonb)
RETURNING id`,
			worldID, demand.Code, actorID, demand.SourceHubCode, demand.DestinationHubCode,
			demand.ModeCode, demand.PurposeCode, demand.RequestedUnits, demand.RequestedTick,
			demand.EarliestDepartureTick, demand.DeadlineTick, demand.Status, sourceFactID,
			lastFactID, routeID, cityNullableInt64(demand.ScheduledTick),
			cityNullableInt64(demand.CompletedTick), cityNullableInt64(demand.ExpiredTick),
			demand.Version, []byte(demand.Metadata)).Scan(&demandID); err != nil {
			return count, fmt.Errorf("restore open-world V9 mobility demand %s: %w", demand.Code, err)
		}
		demandIDs[demand.Code] = demandID
		count++
	}
	for _, route := range mobility.Routes {
		routeID, routeErr := requireCityOpenWorldRecoveryMobilityRouteID(routeIDs, route.Code)
		if routeErr != nil {
			return count, routeErr
		}
		demandID, found := demandIDs[route.DemandCode]
		if !found || demandID <= 0 {
			return count, fmt.Errorf("restore open-world V9 mobility route %s: unknown demand %s", route.Code, route.DemandCode)
		}
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, route.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world V9 mobility route %s: %w", route.Code, actorErr)
		}
		sourceFactID, factErr := requireCityOpenWorldRecoveryFactID(factIDs, route.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world V9 mobility route %s: %w", route.Code, factErr)
		}
		completionFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, route.CompletionFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world V9 mobility route %s: %w", route.Code, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_routes
    (id, world_id, code, demand_id, actor_id, mode_code, source_hub_code,
     destination_hub_code, departure_tick, arrival_tick, base_travel_ticks,
     congestion_delay_ticks, status, source_fact_id, completion_fact_id,
     completed_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18::jsonb)`,
			routeID, worldID, route.Code, demandID, actorID, route.ModeCode,
			route.SourceHubCode, route.DestinationHubCode, route.DepartureTick,
			route.ArrivalTick, route.BaseTravelTicks, route.CongestionDelayTicks,
			route.Status, sourceFactID, completionFactID, cityNullableInt64(route.CompletedTick),
			route.Version, []byte(route.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V9 mobility route %s: %w", route.Code, err)
		}
		count++
	}
	for _, allocation := range mobility.Allocations {
		routeID, routeErr := requireCityOpenWorldRecoveryMobilityRouteID(routeIDs, allocation.RouteCode)
		if routeErr != nil {
			return count, fmt.Errorf("restore open-world V9 mobility allocation %s: %w", allocation.EdgeCode, routeErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_allocations
    (world_id, route_id, edge_code, departure_tick, allocated_units,
     capacity_units_per_tick, occupancy_milli, delay_ticks, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, routeID, allocation.EdgeCode, allocation.DepartureTick,
			allocation.AllocatedUnits, allocation.CapacityUnitsPerTick,
			allocation.OccupancyMilli, allocation.DelayTicks, allocation.Version,
			[]byte(allocation.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V9 mobility allocation %s: %w", allocation.EdgeCode, err)
		}
		count++
	}
	for _, metric := range mobility.ActorMetrics {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, metric.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world V9 mobility actor metric %s: %w", metric.ActorCode, actorErr)
		}
		var routeID any
		if metric.LastRouteCode != nil {
			resolved, routeErr := requireCityOpenWorldRecoveryMobilityRouteID(routeIDs, *metric.LastRouteCode)
			if routeErr != nil {
				return count, fmt.Errorf("restore open-world V9 mobility actor metric %s: %w", metric.ActorCode, routeErr)
			}
			routeID = resolved
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_actor_metrics
    (world_id, actor_id, requested_count, scheduled_count, completed_count,
     expired_count, last_route_id, last_event_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, actorID, metric.RequestedCount, metric.ScheduledCount,
			metric.CompletedCount, metric.ExpiredCount, routeID, metric.LastEventTick,
			metric.Version, []byte(metric.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world V9 mobility actor metric %s: %w", metric.ActorCode, err)
		}
		count++
	}
	return count, nil
}
