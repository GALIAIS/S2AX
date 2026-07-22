package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	cityOpenWorldRuntimeRejectionMobilityUnavailable = "OPEN_WORLD_MOBILITY_UNAVAILABLE"
	cityOpenWorldRuntimeRejectionMobilityOrigin      = "OPEN_WORLD_MOBILITY_ORIGIN_INVALID"
	cityOpenWorldRuntimeRejectionMobilityDestination = "OPEN_WORLD_MOBILITY_DESTINATION_INVALID"
	cityOpenWorldRuntimeRejectionMobilityMode        = "OPEN_WORLD_MOBILITY_MODE_UNAVAILABLE"
)

type cityOpenWorldActorMobilityRequestPayload struct {
	ActorCode          string `json:"actor_code"`
	DestinationHubCode string `json:"destination_hub_code"`
	ModeCode           string `json:"mode_code"`
	PurposeCode        string `json:"purpose_code"`
	RequestedUnits     int64  `json:"requested_units"`
}

type cityOpenWorldMobilityDemandRecord struct {
	id           int64
	actorID      int64
	sourceFactID int64
	lastFactID   int64
	routeID      *int64
	demand       CityOpenWorldMobilityDemand
}

type cityOpenWorldMobilityRouteRecord struct {
	id           int64
	demandID     int64
	actorID      int64
	sourceFactID int64
	route        CityOpenWorldMobilityRoute
}

func isCityOpenWorldMobilityCommand(commandType string) bool {
	return commandType == CityCommandTypeOpenWorldActorMobilityRequest
}

func normalizeCityOpenWorldActorMobilityRequest(rawPayload json.RawMessage) (cityOpenWorldActorMobilityRequestPayload, error) {
	value := cityOpenWorldActorMobilityRequestPayload{}
	if err := decodeStrictCityObject(rawPayload, &value); err != nil {
		return value, ErrCityInvalidInput.WithCause(err)
	}
	value.ActorCode = strings.ToLower(strings.TrimSpace(value.ActorCode))
	value.DestinationHubCode = strings.ToLower(strings.TrimSpace(value.DestinationHubCode))
	value.ModeCode = strings.ToLower(strings.TrimSpace(value.ModeCode))
	value.PurposeCode = strings.ToLower(strings.TrimSpace(value.PurposeCode))
	if value.PurposeCode == "" {
		value.PurposeCode = "general"
	}
	if value.RequestedUnits == 0 {
		value.RequestedUnits = 1
	}
	if !worldRuntimeCodeValid(value.ActorCode, 128) || !worldRuntimeCodeValid(value.DestinationHubCode, 160) ||
		!worldRuntimeCodeValid(value.ModeCode, 64) || !worldRuntimeCodeValid(value.PurposeCode, 96) ||
		value.RequestedUnits < 1 || value.RequestedUnits > cityOpenWorldMobilityMaximumRequestUnits {
		return value, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "open_world_mobility_request"})
	}
	return value, nil
}

func loadCityOpenWorldMobilityDynamicState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	state *CityOpenWorldMobilityState,
) error {
	if state == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_state"})
	}
	demandRows, err := queryer.QueryContext(ctx, `
SELECT demand.code, actor.code, demand.source_hub_code, demand.destination_hub_code,
       demand.mode_code, demand.purpose_code, demand.requested_units,
       demand.requested_tick, demand.earliest_departure_tick, demand.deadline_tick,
       demand.status, source_fact.tick, source_fact.sequence,
       last_fact.tick, last_fact.sequence, route.code, demand.scheduled_tick,
       demand.completed_tick, demand.expired_tick, demand.version, demand.metadata
FROM city_open_world_mobility_demands demand
JOIN city_open_world_actors actor
  ON actor.id = demand.actor_id AND actor.world_id = demand.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = demand.source_fact_id AND source_fact.world_id = demand.world_id
JOIN city_open_world_runtime_facts last_fact
  ON last_fact.id = demand.last_fact_id AND last_fact.world_id = demand.world_id
LEFT JOIN city_open_world_mobility_routes route
  ON route.id = demand.route_id AND route.world_id = demand.world_id
WHERE demand.world_id = $1
ORDER BY demand.requested_tick ASC, demand.code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load V9 mobility demands: %w", err)
	}
	for demandRows.Next() {
		item := CityOpenWorldMobilityDemand{}
		var routeCode sql.NullString
		var scheduledTick, completedTick, expiredTick sql.NullInt64
		lastFact := CityOpenWorldRuntimeFactRef{}
		if err = demandRows.Scan(
			&item.Code, &item.ActorCode, &item.SourceHubCode, &item.DestinationHubCode,
			&item.ModeCode, &item.PurposeCode, &item.RequestedUnits, &item.RequestedTick,
			&item.EarliestDepartureTick, &item.DeadlineTick, &item.Status,
			&item.SourceFact.Tick, &item.SourceFact.Sequence, &lastFact.Tick,
			&lastFact.Sequence, &routeCode, &scheduledTick, &completedTick, &expiredTick,
			&item.Version, &item.Metadata,
		); err != nil {
			_ = demandRows.Close()
			return fmt.Errorf("scan V9 mobility demand: %w", err)
		}
		item.LastFact = &lastFact
		item.RouteCode = nullStringPointer(routeCode)
		item.ScheduledTick = nullInt64Pointer(scheduledTick)
		item.CompletedTick = nullInt64Pointer(completedTick)
		item.ExpiredTick = nullInt64Pointer(expiredTick)
		state.Demands = append(state.Demands, item)
	}
	if err = closeCityRows(demandRows, "iterate V9 mobility demands"); err != nil {
		return err
	}
	routeRows, err := queryer.QueryContext(ctx, `
SELECT route.code, demand.code, actor.code, route.mode_code, route.source_hub_code,
       route.destination_hub_code, route.departure_tick, route.arrival_tick,
       route.base_travel_ticks, route.congestion_delay_ticks, route.status,
       source_fact.tick, source_fact.sequence, completion_fact.tick,
       completion_fact.sequence, route.completed_tick, route.version, route.metadata
FROM city_open_world_mobility_routes route
JOIN city_open_world_mobility_demands demand
  ON demand.id = route.demand_id AND demand.world_id = route.world_id
JOIN city_open_world_actors actor
  ON actor.id = route.actor_id AND actor.world_id = route.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = route.source_fact_id AND source_fact.world_id = route.world_id
LEFT JOIN city_open_world_runtime_facts completion_fact
  ON completion_fact.id = route.completion_fact_id AND completion_fact.world_id = route.world_id
WHERE route.world_id = $1
ORDER BY route.departure_tick ASC, route.code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load V9 mobility routes: %w", err)
	}
	for routeRows.Next() {
		item := CityOpenWorldMobilityRoute{}
		var completionFactTick, completionFactSequence, completedTick sql.NullInt64
		if err = routeRows.Scan(
			&item.Code, &item.DemandCode, &item.ActorCode, &item.ModeCode, &item.SourceHubCode,
			&item.DestinationHubCode, &item.DepartureTick, &item.ArrivalTick,
			&item.BaseTravelTicks, &item.CongestionDelayTicks, &item.Status,
			&item.SourceFact.Tick, &item.SourceFact.Sequence, &completionFactTick,
			&completionFactSequence, &completedTick, &item.Version, &item.Metadata,
		); err != nil {
			_ = routeRows.Close()
			return fmt.Errorf("scan V9 mobility route: %w", err)
		}
		if completionFactTick.Valid {
			item.CompletionFact = &CityOpenWorldRuntimeFactRef{Tick: completionFactTick.Int64, Sequence: completionFactSequence.Int64}
		}
		item.CompletedTick = nullInt64Pointer(completedTick)
		state.Routes = append(state.Routes, item)
	}
	if err = closeCityRows(routeRows, "iterate V9 mobility routes"); err != nil {
		return err
	}
	allocationRows, err := queryer.QueryContext(ctx, `
SELECT route.code, allocation.edge_code, allocation.departure_tick,
       allocation.allocated_units, allocation.capacity_units_per_tick,
       allocation.occupancy_milli, allocation.delay_ticks, allocation.version,
       allocation.metadata
FROM city_open_world_mobility_allocations allocation
JOIN city_open_world_mobility_routes route
  ON route.id = allocation.route_id AND route.world_id = allocation.world_id
WHERE allocation.world_id = $1
ORDER BY allocation.departure_tick ASC, route.code ASC, allocation.edge_code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load V9 mobility allocations: %w", err)
	}
	for allocationRows.Next() {
		item := CityOpenWorldMobilityAllocation{}
		if err = allocationRows.Scan(&item.RouteCode, &item.EdgeCode, &item.DepartureTick,
			&item.AllocatedUnits, &item.CapacityUnitsPerTick, &item.OccupancyMilli,
			&item.DelayTicks, &item.Version, &item.Metadata); err != nil {
			_ = allocationRows.Close()
			return fmt.Errorf("scan V9 mobility allocation: %w", err)
		}
		state.Allocations = append(state.Allocations, item)
	}
	if err = closeCityRows(allocationRows, "iterate V9 mobility allocations"); err != nil {
		return err
	}
	metricRows, err := queryer.QueryContext(ctx, `
SELECT actor.code, metric.requested_count, metric.scheduled_count,
       metric.completed_count, metric.expired_count, route.code,
       metric.last_event_tick, metric.version, metric.metadata
FROM city_open_world_mobility_actor_metrics metric
JOIN city_open_world_actors actor
  ON actor.id = metric.actor_id AND actor.world_id = metric.world_id
LEFT JOIN city_open_world_mobility_routes route
  ON route.id = metric.last_route_id AND route.world_id = metric.world_id
WHERE metric.world_id = $1
ORDER BY actor.code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load V9 mobility actor metrics: %w", err)
	}
	for metricRows.Next() {
		item := CityOpenWorldMobilityActorMetric{}
		var lastRouteCode sql.NullString
		if err = metricRows.Scan(&item.ActorCode, &item.RequestedCount, &item.ScheduledCount,
			&item.CompletedCount, &item.ExpiredCount, &lastRouteCode, &item.LastEventTick,
			&item.Version, &item.Metadata); err != nil {
			_ = metricRows.Close()
			return fmt.Errorf("scan V9 mobility actor metric: %w", err)
		}
		item.LastRouteCode = nullStringPointer(lastRouteCode)
		state.ActorMetrics = append(state.ActorMetrics, item)
	}
	return closeCityRows(metricRows, "iterate V9 mobility actor metrics")
}

func loadCityOpenWorldMobilityMode(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	code string,
) (*CityOpenWorldMobilityMode, error) {
	item := &CityOpenWorldMobilityMode{}
	err := queryer.QueryRowContext(ctx, `
SELECT code, unit_kind, speed_units_per_tick, capacity_units_per_tick,
       congestion_threshold_milli, maximum_delay_ticks, definition_version,
       content_hash, metadata
FROM city_open_world_mobility_modes
WHERE world_id = $1 AND code = $2`, worldID, code).Scan(
		&item.Code, &item.UnitKind, &item.SpeedUnitsPerTick, &item.CapacityUnitsPerTick,
		&item.CongestionThresholdMilli, &item.MaximumDelayTicks, &item.Version,
		&item.ContentHash, &item.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load V9 mobility mode %s: %w", code, err)
	}
	return item, nil
}

func loadCityOpenWorldMobilityHub(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	code string,
) (*CityOpenWorldMobilityHub, error) {
	item := &CityOpenWorldMobilityHub{}
	var facilityCode sql.NullString
	err := queryer.QueryRowContext(ctx, `
SELECT code, hub_kind, facility_code, zone_x, zone_y, anchor_x, anchor_y,
       anchor_z, definition_version, content_hash, metadata
FROM city_open_world_mobility_hubs
WHERE world_id = $1 AND code = $2`, worldID, code).Scan(
		&item.Code, &item.HubKind, &facilityCode, &item.ZoneX, &item.ZoneY,
		&item.AnchorX, &item.AnchorY, &item.AnchorZ, &item.Version, &item.ContentHash,
		&item.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load V9 mobility hub %s: %w", code, err)
	}
	item.FacilityCode = nullStringPointer(facilityCode)
	return item, nil
}

func findNearestCityOpenWorldMobilityZoneHub(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, x, y int64,
) (*CityOpenWorldMobilityHub, error) {
	item := &CityOpenWorldMobilityHub{}
	var facilityCode sql.NullString
	err := queryer.QueryRowContext(ctx, `
SELECT code, hub_kind, facility_code, zone_x, zone_y, anchor_x, anchor_y,
       anchor_z, definition_version, content_hash, metadata
FROM city_open_world_mobility_hubs
WHERE world_id = $1 AND hub_kind = 'zone'
ORDER BY ABS(anchor_x - $2) + ABS(anchor_y - $3), code ASC
LIMIT 1`, worldID, x, y).Scan(
		&item.Code, &item.HubKind, &facilityCode, &item.ZoneX, &item.ZoneY,
		&item.AnchorX, &item.AnchorY, &item.AnchorZ, &item.Version, &item.ContentHash, &item.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find nearest V9 mobility zone hub: %w", err)
	}
	item.FacilityCode = nullStringPointer(facilityCode)
	return item, nil
}

func loadCityOpenWorldMobilityPolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldMobilityPolicy, error) {
	policy := &CityOpenWorldMobilityPolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       topology_contract_version, scheduling_contract, maximum_schedules_per_tick,
       maximum_wait_ticks, mode_count, hub_count, edge_count, demand_count,
       route_count, allocation_count, completed_count, expired_count,
       actor_metric_count, revision, metadata
FROM city_open_world_mobility_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash, &policy.BaselineTick,
		&policy.TopologyContractVersion, &policy.SchedulingContract, &policy.MaximumSchedulesPerTick,
		&policy.MaximumWaitTicks, &policy.ModeCount, &policy.HubCount, &policy.EdgeCount,
		&policy.DemandCount, &policy.RouteCount, &policy.AllocationCount, &policy.CompletedCount,
		&policy.ExpiredCount, &policy.ActorMetricCount, &policy.Revision, &policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V9 mobility policy: %w", err)
	}
	if policy.ProfileID != cityOpenWorldMobilityProfileID || policy.ProfileVersion != cityOpenWorldMobilityProfileVersion ||
		policy.TopologyContractVersion != cityOpenWorldMobilityTopologyContractVersion ||
		policy.SchedulingContract != cityOpenWorldMobilitySchedulingContract ||
		policy.MaximumSchedulesPerTick < 1 || policy.MaximumSchedulesPerTick > cityOpenWorldMobilityMaximumSchedulesPerTick ||
		policy.MaximumWaitTicks != cityOpenWorldMobilityMaximumWaitTicks {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_policy"})
	}
	return policy, nil
}

func updateCityOpenWorldMobilityPolicy(
	ctx context.Context,
	tx *sql.Tx,
	worldID, demandDelta, routeDelta, allocationDelta, completedDelta, expiredDelta, actorMetricDelta int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_profiles
SET demand_count = demand_count + $2,
    route_count = route_count + $3,
    allocation_count = allocation_count + $4,
    completed_count = completed_count + $5,
    expired_count = expired_count + $6,
    actor_metric_count = actor_metric_count + $7,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID, demandDelta, routeDelta, allocationDelta, completedDelta, expiredDelta, actorMetricDelta)
	if err != nil {
		return fmt.Errorf("update V9 mobility policy: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_policy"})
	}
	return nil
}

func (s *CityEconomyService) requestCityOpenWorldActorMobility(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldActorMobilityRequestPayload,
) (cityOpenWorldRuntimeExecution, error) {
	actor, err := loadControlledCityOpenWorldActor(ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityCommand)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	policy, err := loadCityOpenWorldMobilityPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if targetTick <= policy.BaselineTick {
		return cityOpenWorldRuntimeExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_baseline"})
	}
	mode, err := loadCityOpenWorldMobilityMode(ctx, tx, worldID, payload.ModeCode)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if mode == nil {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionMobilityMode)
	}
	destination, err := loadCityOpenWorldMobilityHub(ctx, tx, worldID, payload.DestinationHubCode)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if destination == nil {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionMobilityDestination)
	}
	origin, err := findNearestCityOpenWorldMobilityZoneHub(ctx, tx, worldID, actor.location.X, actor.location.Y)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if origin == nil || origin.Code == destination.Code {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionMobilityOrigin)
	}
	var simulationVersion string
	if err = tx.QueryRowContext(ctx, `
SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&simulationVersion); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("load mobility request world version: %w", err)
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	demandCode := "mobility.demand." + strconv.FormatInt(command.Sequence, 10)
	metadataValue := map[string]any{
		"schema_version":        cityOpenWorldMobilitySchemaVersion,
		"origin":                "player_command",
		"origin_location_scope": actor.location.LocationScope,
	}
	if cityEngineSupportsOpenWorldArrivalBridge(simulationVersion) {
		// The V10 bridge only consumes requests that captured a complete local
		// origin at acceptance time. Upgrade-era V9 demand rows deliberately do
		// not receive invented origins and are therefore never retrofitted.
		metadataValue["arrival_bridge"] = map[string]any{
			"contract":        "captured_request_location_v1",
			"expected_origin": actor.location,
		}
	}
	metadata, err := json.Marshal(metadataValue)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal V9 mobility demand metadata: %w", err)
	}
	factPayload, err := json.Marshal(map[string]any{
		"schema_version":          cityOpenWorldMobilitySchemaVersion,
		"demand_code":             demandCode,
		"actor_code":              actor.actor.Code,
		"source_hub_code":         origin.Code,
		"destination_hub_code":    destination.Code,
		"mode_code":               mode.Code,
		"purpose_code":            payload.PurposeCode,
		"requested_units":         payload.RequestedUnits,
		"earliest_departure_tick": targetTick + 1,
		"deadline_tick":           targetTick + policy.MaximumWaitTicks,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal V9 mobility request fact: %w", err)
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &actor.id, factType: CityOpenWorldRuntimeFactMobilityRequested, payload: factPayload,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	var demandID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_mobility_demands
    (world_id, code, actor_id, source_hub_code, destination_hub_code, mode_code,
     purpose_code, requested_units, requested_tick, earliest_departure_tick,
     deadline_tick, status, source_fact_id, last_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
        'pending', $12, $12, 1, $13::jsonb)
RETURNING id`, worldID, demandCode, actor.id, origin.Code, destination.Code, mode.Code,
		payload.PurposeCode, payload.RequestedUnits, targetTick, targetTick+1,
		targetTick+policy.MaximumWaitTicks, root.id, []byte(metadata)).Scan(&demandID); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("insert V9 mobility demand: %w", err)
	}
	metricCreated, err := updateCityOpenWorldMobilityActorMetric(ctx, tx, worldID, actor.id, actor.actor.Code,
		1, 0, 0, 0, nil, targetTick)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldMobilityPolicy(ctx, tx, worldID, 1, 0, 0, 0, 0, metricCreated); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 0, 0); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	demand := CityOpenWorldMobilityDemand{
		Code: demandCode, ActorCode: actor.actor.Code, SourceHubCode: origin.Code, DestinationHubCode: destination.Code,
		ModeCode: mode.Code, PurposeCode: payload.PurposeCode, RequestedUnits: payload.RequestedUnits,
		RequestedTick: targetTick, EarliestDepartureTick: targetTick + 1, DeadlineTick: targetTick + policy.MaximumWaitTicks,
		Status: "pending", SourceFact: CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence},
		LastFact: &CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence}, Version: 1, Metadata: metadata,
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.mobility_requested", map[string]any{
			"demand_code": demand.Code, "source_hub_code": demand.SourceHubCode,
			"destination_hub_code": demand.DestinationHubCode, "mode_code": demand.ModeCode,
		}),
		facts: []CityOpenWorldRuntimeFact{root.fact}, nextFactSeq: factSequence + 1,
		nextEffectSeq: effectSequence, nextCaseSeq: caseSequence,
	}, nil
}

func updateCityOpenWorldMobilityActorMetric(
	ctx context.Context,
	tx *sql.Tx,
	worldID, actorID int64,
	actorCode string,
	requestedDelta, scheduledDelta, completedDelta, expiredDelta int64,
	lastRouteID *int64,
	lastEventTick int64,
) (int64, error) {
	var lockedActorID int64
	err := tx.QueryRowContext(ctx, `
SELECT actor_id
FROM city_open_world_mobility_actor_metrics
WHERE world_id = $1 AND actor_id = $2
FOR UPDATE`, worldID, actorID).Scan(&lockedActorID)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("lock V9 mobility actor metric: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{"schema_version": cityOpenWorldMobilitySchemaVersion})
	if err != nil {
		return 0, err
	}
	if !exists {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_actor_metrics
    (world_id, actor_id, requested_count, scheduled_count, completed_count,
     expired_count, last_route_id, last_event_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9::jsonb)`,
			worldID, actorID, requestedDelta, scheduledDelta, completedDelta, expiredDelta,
			cityOpenWorldNullableInt64(lastRouteID), lastEventTick, []byte(metadata)); err != nil {
			return 0, fmt.Errorf("insert V9 mobility actor metric %s: %w", actorCode, err)
		}
		return 1, nil
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_actor_metrics
SET requested_count = requested_count + $3,
    scheduled_count = scheduled_count + $4,
    completed_count = completed_count + $5,
    expired_count = expired_count + $6,
    last_route_id = COALESCE($7, last_route_id),
    last_event_tick = $8,
    version = version + 1,
    updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2`, worldID, actorID, requestedDelta, scheduledDelta,
		completedDelta, expiredDelta, cityOpenWorldNullableInt64(lastRouteID), lastEventTick); err != nil {
		return 0, fmt.Errorf("update V9 mobility actor metric %s: %w", actorCode, err)
	}
	return 0, nil
}

func advanceCityOpenWorldV9Mobility(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
	simulationVersion string,
) (cityOpenWorldRuntimeAutomaticExecution, error) {
	execution := cityOpenWorldRuntimeAutomaticExecution{
		facts: make([]CityOpenWorldRuntimeFact, 0), effects: make([]CityOpenWorldRuntimeEffect, 0),
		events: make([]worldRuntimeAutomaticEvent, 0), nextFactSeq: factSequence,
	}
	policy, err := loadCityOpenWorldMobilityPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = completeCityOpenWorldMobilityRoutes(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if err = expireCityOpenWorldMobilityDemands(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if err = scheduleCityOpenWorldMobilityDemands(ctx, tx, worldID, targetTick, simulationVersion, policy, &execution); err != nil {
		return execution, err
	}
	if len(execution.facts) > 0 {
		if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, int64(len(execution.facts)), 0, 0); err != nil {
			return execution, err
		}
	}
	return execution, nil
}

func cityOpenWorldMobilitySchedulingSlots(
	policy *CityOpenWorldMobilityPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) int {
	if policy == nil || execution == nil {
		return 0
	}
	alreadyScheduled := 0
	for _, fact := range execution.facts {
		if fact.FactType == CityOpenWorldRuntimeFactMobilityScheduled {
			alreadyScheduled++
		}
	}
	remaining := policy.MaximumSchedulesPerTick - alreadyScheduled
	if remaining < 0 {
		return 0
	}
	return remaining
}

func cityOpenWorldMobilityDemandIDs(
	ctx context.Context,
	queryer citySQLQueryer,
	query string,
	args ...any,
) ([]int64, error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		items = append(items, id)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func cityOpenWorldMobilityRouteIDs(
	ctx context.Context,
	queryer citySQLQueryer,
	query string,
	args ...any,
) ([]int64, error) {
	return cityOpenWorldMobilityDemandIDs(ctx, queryer, query, args...)
}

func loadCityOpenWorldMobilityDemandForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID, demandID int64,
) (*cityOpenWorldMobilityDemandRecord, error) {
	record := &cityOpenWorldMobilityDemandRecord{id: demandID}
	var routeID sql.NullInt64
	var routeCode sql.NullString
	var scheduledTick, completedTick, expiredTick sql.NullInt64
	lastFact := CityOpenWorldRuntimeFactRef{}
	err := tx.QueryRowContext(ctx, `
SELECT demand.id, demand.actor_id, demand.source_fact_id, demand.last_fact_id,
       demand.route_id, demand.code, actor.code, demand.source_hub_code,
       demand.destination_hub_code, demand.mode_code, demand.purpose_code,
       demand.requested_units, demand.requested_tick, demand.earliest_departure_tick,
       demand.deadline_tick, demand.status, source_fact.tick, source_fact.sequence,
       last_fact.tick, last_fact.sequence, route.code, demand.scheduled_tick,
       demand.completed_tick, demand.expired_tick, demand.version, demand.metadata
FROM city_open_world_mobility_demands demand
JOIN city_open_world_actors actor
  ON actor.id = demand.actor_id AND actor.world_id = demand.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = demand.source_fact_id AND source_fact.world_id = demand.world_id
JOIN city_open_world_runtime_facts last_fact
  ON last_fact.id = demand.last_fact_id AND last_fact.world_id = demand.world_id
LEFT JOIN city_open_world_mobility_routes route
  ON route.id = demand.route_id AND route.world_id = demand.world_id
WHERE demand.world_id = $1 AND demand.id = $2
FOR UPDATE OF demand`, worldID, demandID).Scan(
		&record.id, &record.actorID, &record.sourceFactID, &record.lastFactID, &routeID,
		&record.demand.Code, &record.demand.ActorCode, &record.demand.SourceHubCode,
		&record.demand.DestinationHubCode, &record.demand.ModeCode, &record.demand.PurposeCode,
		&record.demand.RequestedUnits, &record.demand.RequestedTick, &record.demand.EarliestDepartureTick,
		&record.demand.DeadlineTick, &record.demand.Status, &record.demand.SourceFact.Tick,
		&record.demand.SourceFact.Sequence, &lastFact.Tick, &lastFact.Sequence,
		&routeCode, &scheduledTick, &completedTick, &expiredTick, &record.demand.Version, &record.demand.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock V9 mobility demand: %w", err)
	}
	record.routeID = nullInt64Pointer(routeID)
	record.demand.LastFact = &lastFact
	record.demand.RouteCode = nullStringPointer(routeCode)
	record.demand.ScheduledTick = nullInt64Pointer(scheduledTick)
	record.demand.CompletedTick = nullInt64Pointer(completedTick)
	record.demand.ExpiredTick = nullInt64Pointer(expiredTick)
	return record, nil
}

func loadCityOpenWorldMobilityRouteForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID, routeID int64,
) (*cityOpenWorldMobilityRouteRecord, error) {
	record := &cityOpenWorldMobilityRouteRecord{id: routeID}
	var completionFactTick, completionFactSequence, completedTick sql.NullInt64
	err := tx.QueryRowContext(ctx, `
SELECT route.id, route.demand_id, route.actor_id, route.source_fact_id,
       route.code, demand.code, actor.code, route.mode_code, route.source_hub_code,
       route.destination_hub_code, route.departure_tick, route.arrival_tick,
       route.base_travel_ticks, route.congestion_delay_ticks, route.status,
       source_fact.tick, source_fact.sequence, completion_fact.tick,
       completion_fact.sequence, route.completed_tick, route.version, route.metadata
FROM city_open_world_mobility_routes route
JOIN city_open_world_mobility_demands demand
  ON demand.id = route.demand_id AND demand.world_id = route.world_id
JOIN city_open_world_actors actor
  ON actor.id = route.actor_id AND actor.world_id = route.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = route.source_fact_id AND source_fact.world_id = route.world_id
LEFT JOIN city_open_world_runtime_facts completion_fact
  ON completion_fact.id = route.completion_fact_id AND completion_fact.world_id = route.world_id
WHERE route.world_id = $1 AND route.id = $2
FOR UPDATE OF route`, worldID, routeID).Scan(
		&record.id, &record.demandID, &record.actorID, &record.sourceFactID,
		&record.route.Code, &record.route.DemandCode, &record.route.ActorCode,
		&record.route.ModeCode, &record.route.SourceHubCode, &record.route.DestinationHubCode,
		&record.route.DepartureTick, &record.route.ArrivalTick, &record.route.BaseTravelTicks,
		&record.route.CongestionDelayTicks, &record.route.Status, &record.route.SourceFact.Tick,
		&record.route.SourceFact.Sequence, &completionFactTick, &completionFactSequence, &completedTick,
		&record.route.Version, &record.route.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock V9 mobility route: %w", err)
	}
	if completionFactTick.Valid {
		record.route.CompletionFact = &CityOpenWorldRuntimeFactRef{Tick: completionFactTick.Int64, Sequence: completionFactSequence.Int64}
	}
	record.route.CompletedTick = nullInt64Pointer(completedTick)
	return record, nil
}

func completeCityOpenWorldMobilityRoutes(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldMobilityPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_completion"})
	}
	ids, err := cityOpenWorldMobilityRouteIDs(ctx, tx, `
SELECT id
FROM city_open_world_mobility_routes
WHERE world_id = $1 AND status = 'scheduled' AND arrival_tick <= $2
ORDER BY arrival_tick ASC, code ASC
LIMIT $3
FOR UPDATE`, worldID, targetTick, policy.MaximumSchedulesPerTick)
	if err != nil {
		return fmt.Errorf("load due V9 mobility routes: %w", err)
	}
	for _, id := range ids {
		route, routeErr := loadCityOpenWorldMobilityRouteForUpdate(ctx, tx, worldID, id)
		if routeErr != nil {
			return routeErr
		}
		if route == nil || route.route.Status != "scheduled" || route.route.ArrivalTick > targetTick {
			continue
		}
		demand, demandErr := loadCityOpenWorldMobilityDemandForUpdate(ctx, tx, worldID, route.demandID)
		if demandErr != nil {
			return demandErr
		}
		if demand == nil || demand.demand.Status != "scheduled" || demand.routeID == nil || *demand.routeID != route.id {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_route_demand"})
		}
		payload, marshalErr := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldMobilitySchemaVersion,
			"route_code":     route.route.Code, "demand_code": route.route.DemandCode,
			"arrival_tick": route.route.ArrivalTick, "destination_hub_code": route.route.DestinationHubCode,
			"location_projection": "unchanged",
		})
		if marshalErr != nil {
			return fmt.Errorf("marshal V9 mobility completion fact: %w", marshalErr)
		}
		fact, factErr := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
			worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq, parentFactID: &route.sourceFactID,
			actorID: &route.actorID, factType: CityOpenWorldRuntimeFactMobilityCompleted, payload: payload,
		})
		if factErr != nil {
			return factErr
		}
		if factErr = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); factErr != nil {
			return factErr
		}
		if _, updateErr := tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_routes
SET status = 'completed', completed_tick = $3, completion_fact_id = $4,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'scheduled'`, worldID, route.id, targetTick, fact.id); updateErr != nil {
			return fmt.Errorf("complete V9 mobility route %s: %w", route.route.Code, updateErr)
		}
		if _, updateErr := tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_demands
SET status = 'completed', completed_tick = $3, last_fact_id = $4,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'scheduled'`, worldID, demand.id, targetTick, fact.id); updateErr != nil {
			return fmt.Errorf("complete V9 mobility demand %s: %w", demand.demand.Code, updateErr)
		}
		if _, metricErr := updateCityOpenWorldMobilityActorMetric(ctx, tx, worldID, route.actorID, route.route.ActorCode,
			0, 0, 1, 0, &route.id, targetTick); metricErr != nil {
			return metricErr
		}
		if updateErr := updateCityOpenWorldMobilityPolicy(ctx, tx, worldID, 0, 0, 0, 1, 0, 0); updateErr != nil {
			return updateErr
		}
		execution.facts = append(execution.facts, fact.fact)
		execution.nextFactSeq++
		execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.mobility_completed", payload: map[string]any{
			"route_code": route.route.Code, "demand_code": route.route.DemandCode, "actor_code": route.route.ActorCode,
		}})
	}
	return nil
}

func expireCityOpenWorldMobilityDemands(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldMobilityPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_expiration"})
	}
	ids, err := cityOpenWorldMobilityDemandIDs(ctx, tx, `
SELECT id
FROM city_open_world_mobility_demands
WHERE world_id = $1 AND status = 'pending' AND deadline_tick < $2
ORDER BY deadline_tick ASC, requested_tick ASC, code ASC
LIMIT $3
FOR UPDATE`, worldID, targetTick, policy.MaximumSchedulesPerTick)
	if err != nil {
		return fmt.Errorf("load expired V9 mobility demands: %w", err)
	}
	for _, id := range ids {
		demand, demandErr := loadCityOpenWorldMobilityDemandForUpdate(ctx, tx, worldID, id)
		if demandErr != nil {
			return demandErr
		}
		if demand == nil || demand.demand.Status != "pending" || demand.demand.DeadlineTick >= targetTick {
			continue
		}
		payload, marshalErr := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldMobilitySchemaVersion,
			"demand_code":    demand.demand.Code, "deadline_tick": demand.demand.DeadlineTick,
			"expired_tick": targetTick,
		})
		if marshalErr != nil {
			return fmt.Errorf("marshal V9 mobility expiration fact: %w", marshalErr)
		}
		fact, factErr := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
			worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq, parentFactID: &demand.sourceFactID,
			actorID: &demand.actorID, factType: CityOpenWorldRuntimeFactMobilityExpired, payload: payload,
		})
		if factErr != nil {
			return factErr
		}
		if factErr = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); factErr != nil {
			return factErr
		}
		if _, updateErr := tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_demands
SET status = 'expired', expired_tick = $3, last_fact_id = $4,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'pending'`, worldID, demand.id, targetTick, fact.id); updateErr != nil {
			return fmt.Errorf("expire V9 mobility demand %s: %w", demand.demand.Code, updateErr)
		}
		if _, metricErr := updateCityOpenWorldMobilityActorMetric(ctx, tx, worldID, demand.actorID, demand.demand.ActorCode,
			0, 0, 0, 1, nil, targetTick); metricErr != nil {
			return metricErr
		}
		if updateErr := updateCityOpenWorldMobilityPolicy(ctx, tx, worldID, 0, 0, 0, 0, 1, 0); updateErr != nil {
			return updateErr
		}
		execution.facts = append(execution.facts, fact.fact)
		execution.nextFactSeq++
		execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.mobility_expired", payload: map[string]any{
			"demand_code": demand.demand.Code, "actor_code": demand.demand.ActorCode,
		}})
	}
	return nil
}

func cityOpenWorldMobilityShortestPath(
	edges []CityOpenWorldMobilityEdge,
	modeCode, sourceHubCode, destinationHubCode string,
) ([]CityOpenWorldMobilityEdge, error) {
	return cityOpenWorldMobilityShortestPathEligible(edges, modeCode, sourceHubCode, destinationHubCode, nil)
}

// cityOpenWorldMobilityShortestPathEligible keeps V9's deterministic
// tie-breaking while allowing a later engine generation to close individual
// edges for one departure tick. A nil predicate is exactly the historic V9
// route selection behavior.
func cityOpenWorldMobilityShortestPathEligible(
	edges []CityOpenWorldMobilityEdge,
	modeCode, sourceHubCode, destinationHubCode string,
	eligible func(CityOpenWorldMobilityEdge) bool,
) ([]CityOpenWorldMobilityEdge, error) {
	type predecessor struct {
		hub  string
		edge CityOpenWorldMobilityEdge
	}
	bySource := make(map[string][]CityOpenWorldMobilityEdge)
	nodes := map[string]struct{}{sourceHubCode: {}, destinationHubCode: {}}
	for _, edge := range edges {
		if edge.ModeCode != modeCode || (eligible != nil && !eligible(edge)) {
			continue
		}
		bySource[edge.FromHubCode] = append(bySource[edge.FromHubCode], edge)
		nodes[edge.FromHubCode] = struct{}{}
		nodes[edge.ToHubCode] = struct{}{}
	}
	for code := range bySource {
		sort.Slice(bySource[code], func(i, j int) bool {
			if bySource[code][i].ToHubCode != bySource[code][j].ToHubCode {
				return bySource[code][i].ToHubCode < bySource[code][j].ToHubCode
			}
			return bySource[code][i].Code < bySource[code][j].Code
		})
	}
	const infinite = int64(^uint64(0) >> 1)
	distance := make(map[string]int64, len(nodes))
	for node := range nodes {
		distance[node] = infinite
	}
	distance[sourceHubCode] = 0
	previous := make(map[string]predecessor)
	visited := make(map[string]bool, len(nodes))
	for len(visited) < len(nodes) {
		candidate := ""
		candidateDistance := infinite
		for node, value := range distance {
			if !visited[node] && (value < candidateDistance || value == candidateDistance && (candidate == "" || node < candidate)) {
				candidate, candidateDistance = node, value
			}
		}
		if candidate == "" || candidateDistance == infinite {
			break
		}
		if candidate == destinationHubCode {
			break
		}
		visited[candidate] = true
		for _, edge := range bySource[candidate] {
			if edge.BaseTravelTicks > infinite-candidateDistance {
				continue
			}
			candidateCost := candidateDistance + edge.BaseTravelTicks
			current := distance[edge.ToHubCode]
			prior, hadPrior := previous[edge.ToHubCode]
			if candidateCost < current || (candidateCost == current && (!hadPrior || edge.Code < prior.edge.Code)) {
				distance[edge.ToHubCode] = candidateCost
				previous[edge.ToHubCode] = predecessor{hub: candidate, edge: edge}
			}
		}
	}
	if _, found := previous[destinationHubCode]; !found {
		return nil, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionMobilityUnavailable)
	}
	reversed := make([]CityOpenWorldMobilityEdge, 0)
	for current := destinationHubCode; current != sourceHubCode; {
		item, found := previous[current]
		if !found {
			return nil, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionMobilityUnavailable)
		}
		reversed = append(reversed, item.edge)
		current = item.hub
	}
	path := make([]CityOpenWorldMobilityEdge, len(reversed))
	for index := range reversed {
		path[len(reversed)-1-index] = reversed[index]
	}
	return path, nil
}

func cityOpenWorldMobilityAllocationState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	edgeCode string,
	departureTick int64,
) (int64, error) {
	var allocated int64
	if err := queryer.QueryRowContext(ctx, `
SELECT COALESCE(SUM(allocated_units), 0)
FROM city_open_world_mobility_allocations
WHERE world_id = $1 AND edge_code = $2 AND departure_tick = $3`, worldID, edgeCode, departureTick).Scan(&allocated); err != nil {
		return 0, fmt.Errorf("load V9 mobility edge allocation: %w", err)
	}
	return allocated, nil
}

func cityOpenWorldMobilityCongestionDelay(mode CityOpenWorldMobilityMode, allocated, capacity int64) (int, int64, error) {
	if capacity < 1 || allocated < 1 || allocated > capacity {
		return 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_capacity"})
	}
	occupancy := int(cityOpenWorldMobilityCeilDiv(allocated*1_000, capacity))
	if occupancy > 1_000 {
		return 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_occupancy"})
	}
	if occupancy <= mode.CongestionThresholdMilli || mode.MaximumDelayTicks == 0 {
		return occupancy, 0, nil
	}
	delayNumerator := int64(occupancy-mode.CongestionThresholdMilli) * mode.MaximumDelayTicks
	delayDenominator := int64(1_000 - mode.CongestionThresholdMilli)
	delay := cityOpenWorldMobilityCeilDiv(delayNumerator, delayDenominator)
	if delay > mode.MaximumDelayTicks {
		delay = mode.MaximumDelayTicks
	}
	return occupancy, delay, nil
}

func scheduleCityOpenWorldMobilityDemands(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	simulationVersion string,
	policy *CityOpenWorldMobilityPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_scheduling"})
	}
	remaining := cityOpenWorldMobilitySchedulingSlots(policy, execution)
	if remaining < 1 {
		return nil
	}
	ids, err := cityOpenWorldMobilityDemandIDs(ctx, tx, `
SELECT id
FROM city_open_world_mobility_demands
WHERE world_id = $1 AND status = 'pending'
  AND requested_tick < $2 AND earliest_departure_tick <= $2
  AND deadline_tick >= $2
ORDER BY earliest_departure_tick ASC, requested_tick ASC, code ASC
LIMIT $3
FOR UPDATE`, worldID, targetTick, remaining)
	if err != nil {
		return fmt.Errorf("load pending V9 mobility demands: %w", err)
	}
	state, err := loadCityOpenWorldMobilityState(ctx, tx, worldID)
	if err != nil {
		return err
	}
	modeByCode := cityOpenWorldMobilityModeByCode(state.Modes)
	var effectiveCapacity *cityOpenWorldEffectiveCapacitySchedulingState
	if cityEngineSupportsOpenWorldEffectiveCapacity(simulationVersion) {
		effectiveCapacity, err = loadCityOpenWorldEffectiveCapacitySchedulingState(
			ctx, tx, worldID, targetTick, state,
		)
		if err != nil {
			return err
		}
	}
	for _, id := range ids {
		demand, demandErr := loadCityOpenWorldMobilityDemandForUpdate(ctx, tx, worldID, id)
		if demandErr != nil {
			return demandErr
		}
		if demand == nil || demand.demand.Status != "pending" || demand.demand.RequestedTick >= targetTick ||
			demand.demand.EarliestDepartureTick > targetTick || demand.demand.DeadlineTick < targetTick {
			continue
		}
		mode, found := modeByCode[demand.demand.ModeCode]
		if !found {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_mode"})
		}
		var path []CityOpenWorldMobilityEdge
		var pathErr error
		if effectiveCapacity != nil {
			path, pathErr = cityOpenWorldMobilityShortestPathEligible(
				state.Edges, mode.Code, demand.demand.SourceHubCode, demand.demand.DestinationHubCode,
				func(edge CityOpenWorldMobilityEdge) bool {
					return effectiveCapacity.edgeAvailable(edge, demand.demand.RequestedUnits)
				},
			)
		} else {
			path, pathErr = cityOpenWorldMobilityShortestPath(
				state.Edges, mode.Code, demand.demand.SourceHubCode, demand.demand.DestinationHubCode,
			)
		}
		if pathErr != nil {
			if cityOpenWorldRuntimeBusinessRejectionCode(pathErr) != "" {
				continue
			}
			return pathErr
		}
		allocations := make([]CityOpenWorldMobilityAllocation, 0, len(path))
		baseTicks, delayTicks := int64(0), int64(0)
		capacityAvailable := true
		for _, edge := range path {
			capacityUnits := edge.CapacityUnitsPerTick
			used := int64(0)
			var allocationMetadata json.RawMessage
			if effectiveCapacity != nil {
				capacity, nextUsed, reserveErr := effectiveCapacity.reserve(edge, demand.demand.RequestedUnits)
				if reserveErr != nil {
					return reserveErr
				}
				capacityUnits = capacity.EffectiveCapacityUnitsPerTick
				used = nextUsed - demand.demand.RequestedUnits
				allocationMetadata, reserveErr = cityOpenWorldEffectiveCapacityAllocationMetadataFor(capacity)
				if reserveErr != nil {
					return reserveErr
				}
			} else {
				usedErr := error(nil)
				used, usedErr = cityOpenWorldMobilityAllocationState(ctx, tx, worldID, edge.Code, targetTick)
				if usedErr != nil {
					return usedErr
				}
				if used > capacityUnits-demand.demand.RequestedUnits {
					capacityAvailable = false
					break
				}
				metadata, metadataErr := json.Marshal(map[string]any{
					"schema_version":      cityOpenWorldMobilitySchemaVersion,
					"allocation_contract": "edge_departure_tick",
				})
				if metadataErr != nil {
					return metadataErr
				}
				allocationMetadata = metadata
			}
			occupancy, delay, delayErr := cityOpenWorldMobilityCongestionDelay(
				mode, used+demand.demand.RequestedUnits, capacityUnits,
			)
			if delayErr != nil {
				return delayErr
			}
			allocations = append(allocations, CityOpenWorldMobilityAllocation{
				EdgeCode: edge.Code, DepartureTick: targetTick, AllocatedUnits: demand.demand.RequestedUnits,
				CapacityUnitsPerTick: capacityUnits, OccupancyMilli: occupancy, DelayTicks: delay,
				Version: 1, Metadata: allocationMetadata,
			})
			baseTicks += edge.BaseTravelTicks
			delayTicks += delay
		}
		if !capacityAvailable {
			continue
		}
		if baseTicks < 1 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_route_duration"})
		}
		routeCode := cityOpenWorldMobilityRouteCode(demand.demand.Code)
		arrivalTick := targetTick + baseTicks + delayTicks
		metadata, metadataErr := json.Marshal(map[string]any{
			"schema_version":      cityOpenWorldMobilitySchemaVersion,
			"path_contract":       "directed_weighted_path_v1",
			"location_projection": "unchanged",
		})
		if metadataErr != nil {
			return metadataErr
		}
		payload, marshalErr := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldMobilitySchemaVersion,
			"route_code":     routeCode, "demand_code": demand.demand.Code,
			"departure_tick": targetTick, "arrival_tick": arrivalTick,
			"base_travel_ticks": baseTicks, "congestion_delay_ticks": delayTicks,
			"edge_codes": cityOpenWorldMobilityPathCodes(path),
		})
		if marshalErr != nil {
			return fmt.Errorf("marshal V9 mobility schedule fact: %w", marshalErr)
		}
		fact, factErr := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
			worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq, parentFactID: &demand.sourceFactID,
			actorID: &demand.actorID, factType: CityOpenWorldRuntimeFactMobilityScheduled, payload: payload,
		})
		if factErr != nil {
			return factErr
		}
		if factErr = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); factErr != nil {
			return factErr
		}
		var routeID int64
		if factErr = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_mobility_routes
    (world_id, code, demand_id, actor_id, mode_code, source_hub_code,
     destination_hub_code, departure_tick, arrival_tick, base_travel_ticks,
     congestion_delay_ticks, status, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
        'scheduled', $12, 1, $13::jsonb)
RETURNING id`, worldID, routeCode, demand.id, demand.actorID, demand.demand.ModeCode,
			demand.demand.SourceHubCode, demand.demand.DestinationHubCode, targetTick, arrivalTick,
			baseTicks, delayTicks, fact.id, []byte(metadata)).Scan(&routeID); factErr != nil {
			return fmt.Errorf("insert V9 mobility route %s: %w", routeCode, factErr)
		}
		for index := range allocations {
			allocations[index].RouteCode = routeCode
			if _, factErr = tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_allocations
    (world_id, route_id, edge_code, departure_tick, allocated_units,
     capacity_units_per_tick, occupancy_milli, delay_ticks, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
				worldID, routeID, allocations[index].EdgeCode, allocations[index].DepartureTick,
				allocations[index].AllocatedUnits, allocations[index].CapacityUnitsPerTick,
				allocations[index].OccupancyMilli, allocations[index].DelayTicks,
				allocations[index].Version, []byte(allocations[index].Metadata)); factErr != nil {
				return fmt.Errorf("insert V9 mobility allocation %s: %w", allocations[index].EdgeCode, factErr)
			}
		}
		if effectiveCapacity != nil {
			if factErr = recordCityOpenWorldEffectiveCapacityAdmissions(
				ctx, tx, worldID, routeID, fact.id,
				CityOpenWorldRuntimeFactRef{Tick: fact.fact.Tick, Sequence: fact.fact.Sequence},
				allocations, effectiveCapacity,
			); factErr != nil {
				return factErr
			}
		}
		if _, factErr = tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_demands
SET status = 'scheduled', route_id = $3, scheduled_tick = $4, last_fact_id = $5,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'pending'`, worldID, demand.id, routeID, targetTick, fact.id); factErr != nil {
			return fmt.Errorf("schedule V9 mobility demand %s: %w", demand.demand.Code, factErr)
		}
		if _, metricErr := updateCityOpenWorldMobilityActorMetric(ctx, tx, worldID, demand.actorID, demand.demand.ActorCode,
			0, 1, 0, 0, &routeID, targetTick); metricErr != nil {
			return metricErr
		}
		if updateErr := updateCityOpenWorldMobilityPolicy(ctx, tx, worldID, 0, 1, int64(len(allocations)), 0, 0, 0); updateErr != nil {
			return updateErr
		}
		execution.facts = append(execution.facts, fact.fact)
		execution.nextFactSeq++
		execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.mobility_scheduled", payload: map[string]any{
			"route_code": routeCode, "demand_code": demand.demand.Code, "actor_code": demand.demand.ActorCode,
			"arrival_tick": arrivalTick,
		}})
	}
	if effectiveCapacity != nil && effectiveCapacity.admissionsWritten > 0 {
		if err = assertCityOpenWorldEffectiveCapacityFoundation(ctx, tx, worldID); err != nil {
			return fmt.Errorf("validate V21 effective-capacity scheduling: %w", err)
		}
	}
	return nil
}

func cityOpenWorldMobilityPathCodes(path []CityOpenWorldMobilityEdge) []string {
	items := make([]string, len(path))
	for index := range path {
		items[index] = path[index].Code
	}
	return items
}

func cityOpenWorldMobilityRouteCode(demandCode string) string {
	return "mobility.route." + strconv.FormatUint(uint64(len(demandCode)), 16) + "." + cityOpenWorldMobilityEdgeCode("route", demandCode, "v1")[5:]
}
