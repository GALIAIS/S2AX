package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// cityOpenWorldServiceRequestRecord keeps storage identifiers private while a
// reducer is executing.  The canonical representation is always expressed in
// stable actor/provider codes and fact coordinates.
type cityOpenWorldServiceRequestRecord struct {
	id           int64
	actorID      int64
	sourceFactID int64
	lastFactID   int64
	providerID   *int64
	request      CityOpenWorldServiceRequest
	location     CityOpenWorldActorLocation
}

type cityOpenWorldServiceProviderRecord struct {
	id       int64
	provider CityOpenWorldServiceProvider
}

// requestCityOpenWorldActorService records an actor intent.  It never chooses
// a provider: spatial matching and capacity arbitration happen automatically
// on a later simulation tick.
func (s *CityEconomyService) requestCityOpenWorldActorService(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldActorServiceRequestPayload,
) (cityOpenWorldRuntimeExecution, error) {
	actor, err := loadControlledCityOpenWorldActor(
		ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityCommand,
	)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	catalog, err := loadCityOpenWorldServiceCatalogEntry(ctx, tx, worldID, payload.ServiceCode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject("OPEN_WORLD_SERVICE_UNAVAILABLE")
		}
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	priority := catalog.DefaultPriorityMilli
	if payload.PriorityMilli != nil {
		priority = *payload.PriorityMilli
	}
	requestCode := "service.request." + strconv.FormatInt(command.Sequence, 10)
	requestMetadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldServiceSchemaVersion,
		"origin":         "player_command",
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal V7 service request metadata: %w", err)
	}
	rootPayload, err := json.Marshal(map[string]any{
		"schema_version":    cityOpenWorldServiceSchemaVersion,
		"request_code":      requestCode,
		"actor_code":        actor.actor.Code,
		"service_code":      catalog.Code,
		"requested_units":   payload.RequestedUnits,
		"priority_milli":    priority,
		"earliest_dispatch": targetTick + 1,
		"deadline_tick":     targetTick + catalog.MaximumWaitTicks,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal V7 service request fact: %w", err)
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, actorID: &actor.id,
		factType: CityOpenWorldRuntimeFactServiceRequested, payload: rootPayload,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	var requestID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_service_requests
    (world_id, code, actor_id, service_code, status, priority_milli,
     requested_units, requested_tick, earliest_dispatch_tick, deadline_tick,
     source_fact_id, last_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $8, $9, $10, $10, 1, $11::jsonb)
RETURNING id`,
		worldID, requestCode, actor.id, catalog.Code, priority, payload.RequestedUnits,
		targetTick, targetTick+1, targetTick+catalog.MaximumWaitTicks, root.id, []byte(requestMetadata),
	).Scan(&requestID); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("insert V7 service request: %w", err)
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 0, 0); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	request := CityOpenWorldServiceRequest{
		Code: requestCode, ActorCode: actor.actor.Code, ServiceCode: catalog.Code,
		Status: "pending", PriorityMilli: priority, RequestedUnits: payload.RequestedUnits,
		RequestedTick: targetTick, EarliestDispatchTick: targetTick + 1,
		DeadlineTick: targetTick + catalog.MaximumWaitTicks,
		SourceFact:   CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence},
		LastFact:     &CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence},
		Version:      1, Metadata: requestMetadata,
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.service_requested", map[string]any{
			"request_code": requestCode, "request": request,
		}),
		facts:       []CityOpenWorldRuntimeFact{root.fact},
		effects:     []CityOpenWorldRuntimeEffect{},
		cases:       []CityOpenWorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence, nextCaseSeq: caseSequence,
	}, nil
}

func loadCityOpenWorldServiceCatalogEntry(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	code string,
) (*CityOpenWorldServiceCatalogEntry, error) {
	item := &CityOpenWorldServiceCatalogEntry{}
	err := queryer.QueryRowContext(ctx, `
SELECT code, name_key, category_code, definition_version, content_hash,
       maximum_wait_ticks, target_response_ticks, default_priority_milli, metadata
FROM city_open_world_service_catalog
WHERE world_id = $1 AND code = $2`, worldID, code).Scan(
		&item.Code, &item.NameKey, &item.CategoryCode, &item.Version, &item.ContentHash,
		&item.MaximumWaitTicks, &item.TargetResponseTicks, &item.DefaultPriorityMilli, &item.Metadata,
	)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func loadCityOpenWorldServiceCatalogMap(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (map[string]CityOpenWorldServiceCatalogEntry, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT code, name_key, category_code, definition_version, content_hash,
       maximum_wait_ticks, target_response_ticks, default_priority_milli, metadata
FROM city_open_world_service_catalog
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V7 service catalog map: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make(map[string]CityOpenWorldServiceCatalogEntry)
	for rows.Next() {
		item := CityOpenWorldServiceCatalogEntry{}
		if err = rows.Scan(
			&item.Code, &item.NameKey, &item.CategoryCode, &item.Version, &item.ContentHash,
			&item.MaximumWaitTicks, &item.TargetResponseTicks, &item.DefaultPriorityMilli, &item.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan V7 service catalog map: %w", err)
		}
		items[item.Code] = item
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V7 service catalog map: %w", err)
	}
	if len(items) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v7_service_catalog"})
	}
	return items, nil
}

func loadCityOpenWorldServiceMaximumQueueForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (int, error) {
	var maximum int
	err := tx.QueryRowContext(ctx, `
SELECT maximum_queue_per_provider
FROM city_open_world_service_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(&maximum)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v7_service_profile"})
	}
	if err != nil {
		return 0, fmt.Errorf("lock V7 service profile: %w", err)
	}
	if maximum < 1 || maximum > cityOpenWorldServiceMaximumQueuePerProvider {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v7_service_maximum_queue"})
	}
	return maximum, nil
}

// advanceCityOpenWorldV7ServiceRequests executes only reducer-owned work.
// Commands are applied afterwards, so a request submitted at tick T is never
// visible to this routine before T+1.
func advanceCityOpenWorldV7ServiceRequests(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
) (cityOpenWorldRuntimeAutomaticExecution, error) {
	execution := cityOpenWorldRuntimeAutomaticExecution{
		facts: make([]CityOpenWorldRuntimeFact, 0), effects: make([]CityOpenWorldRuntimeEffect, 0),
		events: make([]worldRuntimeAutomaticEvent, 0), nextFactSeq: factSequence,
	}
	maximumQueue, err := loadCityOpenWorldServiceMaximumQueueForUpdate(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	catalog, err := loadCityOpenWorldServiceCatalogMap(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = advanceCityOpenWorldV7ExpiredServiceRequests(ctx, tx, worldID, targetTick, &execution); err != nil {
		return execution, err
	}
	if err = advanceCityOpenWorldV7PendingServiceRequests(ctx, tx, worldID, targetTick, maximumQueue, &execution); err != nil {
		return execution, err
	}
	if err = advanceCityOpenWorldV7QueuedServiceRequests(ctx, tx, worldID, targetTick, &execution); err != nil {
		return execution, err
	}
	if err = advanceCityOpenWorldV7DispatchedServiceRequests(ctx, tx, worldID, targetTick, catalog, &execution); err != nil {
		return execution, err
	}
	if len(execution.facts) > 0 {
		if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, int64(len(execution.facts)), 0, 0); err != nil {
			return execution, err
		}
	}
	return execution, nil
}

func loadCityOpenWorldServiceRequestIDs(
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

func loadCityOpenWorldServiceRequestForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID, requestID int64,
) (*cityOpenWorldServiceRequestRecord, error) {
	record := &cityOpenWorldServiceRequestRecord{}
	var providerID sql.NullInt64
	var providerCode sql.NullString
	var queuedTick, dispatchedTick, resolvedTick, queuePosition sql.NullInt64
	var lastFactID sql.NullInt64
	var lastFactTick, lastFactSequence sql.NullInt64
	var buildingCode sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT request_value.id, request_value.actor_id, request_value.source_fact_id,
       request_value.last_fact_id, request_value.provider_id,
       request_value.code, actor.code, request_value.service_code, request_value.status,
       request_value.priority_milli, request_value.requested_units, request_value.requested_tick,
       request_value.earliest_dispatch_tick, request_value.deadline_tick,
       request_value.queued_tick, provider.code, request_value.dispatched_tick,
       request_value.resolved_tick, request_value.queue_position,
       source_fact.tick, source_fact.sequence, last_fact.tick, last_fact.sequence,
       request_value.version, request_value.metadata,
       location.space_kind, location.location_scope, location.building_code,
       location.floor_index, location.x, location.y, location.z, location.sector_x,
       location.sector_y, location.chunk_x, location.chunk_y, location.local_x,
       location.local_y, location.moved_tick, location.version, location.metadata
FROM city_open_world_service_requests request_value
JOIN city_open_world_actors actor
  ON actor.id = request_value.actor_id AND actor.world_id = request_value.world_id
JOIN city_open_world_actor_locations location
  ON location.actor_id = actor.id AND location.world_id = actor.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = request_value.source_fact_id AND source_fact.world_id = request_value.world_id
LEFT JOIN city_open_world_runtime_facts last_fact
  ON last_fact.id = request_value.last_fact_id AND last_fact.world_id = request_value.world_id
LEFT JOIN city_open_world_service_providers provider
  ON provider.id = request_value.provider_id AND provider.world_id = request_value.world_id
WHERE request_value.world_id = $1 AND request_value.id = $2
FOR UPDATE OF request_value`, worldID, requestID).Scan(
		&record.id, &record.actorID, &record.sourceFactID, &lastFactID, &providerID,
		&record.request.Code, &record.request.ActorCode, &record.request.ServiceCode, &record.request.Status,
		&record.request.PriorityMilli, &record.request.RequestedUnits, &record.request.RequestedTick,
		&record.request.EarliestDispatchTick, &record.request.DeadlineTick,
		&queuedTick, &providerCode, &dispatchedTick, &resolvedTick, &queuePosition,
		&record.request.SourceFact.Tick, &record.request.SourceFact.Sequence,
		&lastFactTick, &lastFactSequence,
		&record.request.Version, &record.request.Metadata,
		&record.location.SpaceKind, &record.location.LocationScope, &buildingCode,
		&record.location.FloorIndex, &record.location.X, &record.location.Y, &record.location.Z,
		&record.location.SectorX, &record.location.SectorY, &record.location.ChunkX,
		&record.location.ChunkY, &record.location.LocalX, &record.location.LocalY,
		&record.location.MovedTick, &record.location.Version, &record.location.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock V7 service request: %w", err)
	}
	// The nullable last fact is always initialized by the command reducer.  A
	// missing value is treated as an old/corrupt projection instead of silently
	// selecting a new causal parent.
	if !lastFactID.Valid || !lastFactTick.Valid || !lastFactSequence.Valid {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v7_service_last_fact"})
	}
	record.lastFactID = lastFactID.Int64
	record.request.LastFact = &CityOpenWorldRuntimeFactRef{Tick: lastFactTick.Int64, Sequence: lastFactSequence.Int64}
	if providerID.Valid {
		value := providerID.Int64
		record.providerID = &value
	}
	if queuedTick.Valid {
		record.request.QueuedTick = cityOpenWorldInt64Pointer(queuedTick.Int64)
	}
	if providerCode.Valid {
		record.request.ProviderCode = cityOpenWorldStringPointer(providerCode.String)
	}
	if dispatchedTick.Valid {
		record.request.DispatchedTick = cityOpenWorldInt64Pointer(dispatchedTick.Int64)
	}
	if resolvedTick.Valid {
		record.request.ResolvedTick = cityOpenWorldInt64Pointer(resolvedTick.Int64)
	}
	if queuePosition.Valid {
		position := int(queuePosition.Int64)
		record.request.QueuePosition = &position
	}
	if buildingCode.Valid {
		record.location.BuildingCode = cityOpenWorldStringPointer(buildingCode.String)
	}
	record.location.ActorCode = record.request.ActorCode
	return record, nil
}

func loadCityOpenWorldServiceProviderForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID, providerID int64,
) (*cityOpenWorldServiceProviderRecord, error) {
	record := &cityOpenWorldServiceProviderRecord{id: providerID}
	err := tx.QueryRowContext(ctx, `
SELECT provider.code, facility.code, provider.service_code, provider.provider_kind,
       provider.status, provider.capacity_units_per_tick, provider.access_radius_units,
       provider.anchor_x, provider.anchor_y, provider.anchor_z, provider.last_settled_tick,
       provider.version, provider.metadata
FROM city_open_world_service_providers provider
JOIN city_open_world_facilities facility
  ON facility.id = provider.facility_id AND facility.world_id = provider.world_id
WHERE provider.world_id = $1 AND provider.id = $2
FOR UPDATE OF provider`, worldID, providerID).Scan(
		&record.provider.Code, &record.provider.FacilityCode, &record.provider.ServiceCode,
		&record.provider.ProviderKind, &record.provider.Status,
		&record.provider.CapacityUnitsPerTick, &record.provider.AccessRadiusUnits,
		&record.provider.AnchorX, &record.provider.AnchorY, &record.provider.AnchorZ,
		&record.provider.LastSettledTick, &record.provider.Version, &record.provider.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock V7 service provider: %w", err)
	}
	return record, nil
}

func findNearestCityOpenWorldServiceProviderForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	serviceCode string,
	location CityOpenWorldActorLocation,
	maximumQueue int,
) (*cityOpenWorldServiceProviderRecord, error) {
	var providerID int64
	err := tx.QueryRowContext(ctx, `
SELECT provider.id
FROM city_open_world_service_providers provider
WHERE provider.world_id = $1 AND provider.service_code = $2
  AND provider.status = 'active'
  AND ABS(provider.anchor_x - $3) + ABS(provider.anchor_y - $4) + ABS(provider.anchor_z - $5) <= provider.access_radius_units
  AND (
      SELECT COUNT(*)
      FROM city_open_world_service_requests queued
      WHERE queued.world_id = provider.world_id
        AND queued.provider_id = provider.id
        AND queued.status = 'queued'
  ) < $6
ORDER BY ABS(provider.anchor_x - $3) + ABS(provider.anchor_y - $4) + ABS(provider.anchor_z - $5), provider.code ASC
LIMIT 1
FOR UPDATE OF provider`, worldID, serviceCode, location.X, location.Y, location.Z, maximumQueue).Scan(&providerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select V7 service provider: %w", err)
	}
	return loadCityOpenWorldServiceProviderForUpdate(ctx, tx, worldID, providerID)
}

func persistCityOpenWorldServiceRequestTransition(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
	record *cityOpenWorldServiceRequestRecord,
	after CityOpenWorldServiceRequest,
	providerID *int64,
	factType string,
	detail map[string]any,
) (*cityOpenWorldRuntimeFactRecord, error) {
	if record == nil || record.lastFactID <= 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v7_service_request"})
	}
	after.LastFact = &CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence}
	after.Version = record.request.Version + 1
	payload := map[string]any{
		"schema_version": cityOpenWorldServiceSchemaVersion,
		"request_before": record.request,
		"request_after":  after,
	}
	for key, value := range detail {
		payload[key] = value
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal V7 service request transition: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		parentFactID: &record.lastFactID, actorID: &record.actorID,
		factType: factType, payload: raw,
	})
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_service_requests
SET status = $3, queued_tick = $4, provider_id = $5, dispatched_tick = $6,
    resolved_tick = $7, queue_position = $8, last_fact_id = $9,
    version = version + 1, metadata = $10::jsonb, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND version = $11`,
		worldID, record.id, after.Status,
		cityOpenWorldNullableInt64(after.QueuedTick), cityOpenWorldNullableInt64(providerID),
		cityOpenWorldNullableInt64(after.DispatchedTick), cityOpenWorldNullableInt64(after.ResolvedTick),
		cityOpenWorldNullableInt(after.QueuePosition), fact.id, []byte(after.Metadata), record.request.Version,
	)
	if err != nil {
		return nil, fmt.Errorf("transition V7 service request: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v7_service_request_version"})
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return nil, err
	}
	return fact, nil
}

func cityOpenWorldNullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func advanceCityOpenWorldV7ExpiredServiceRequests(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	ids, err := loadCityOpenWorldServiceRequestIDs(ctx, tx, `
SELECT id
FROM city_open_world_service_requests
WHERE world_id = $1
  AND status IN ('pending', 'queued')
  AND deadline_tick < $2
ORDER BY deadline_tick ASC, requested_tick ASC, code ASC
LIMIT $3`, worldID, targetTick, cityOpenWorldServiceMaximumRequestsPerTick)
	if err != nil {
		return fmt.Errorf("load expired V7 service requests: %w", err)
	}
	for _, id := range ids {
		record, loadErr := loadCityOpenWorldServiceRequestForUpdate(ctx, tx, worldID, id)
		if loadErr != nil || record == nil {
			if loadErr != nil {
				return loadErr
			}
			continue
		}
		if (record.request.Status != "pending" && record.request.Status != "queued") || record.request.DeadlineTick >= targetTick {
			continue
		}
		after := record.request
		after.Status = "expired"
		after.ResolvedTick = cityOpenWorldInt64Pointer(targetTick)
		after.QueuePosition = nil
		fact, transitionErr := persistCityOpenWorldServiceRequestTransition(
			ctx, tx, worldID, targetTick, execution.nextFactSeq, record, after, record.providerID,
			CityOpenWorldRuntimeFactServiceExpired, map[string]any{"reason": "deadline_elapsed"},
		)
		if transitionErr != nil {
			return transitionErr
		}
		responseCode := "service.response." + strconv.FormatInt(record.id, 10)
		responseMetadata, marshalErr := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldServiceSchemaVersion,
			"reason":         "deadline_elapsed",
		})
		if marshalErr != nil {
			return fmt.Errorf("marshal V7 service expiration response: %w", marshalErr)
		}
		if _, insertErr := tx.ExecContext(ctx, `
INSERT INTO city_open_world_service_responses
    (world_id, code, request_id, actor_id, service_code, provider_id, outcome,
     requested_tick, queued_tick, dispatched_tick, resolved_tick, response_ticks,
     delivered_units, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, 'expired', $7, $8, NULL, $9, $10, 0, $11, $12::jsonb)`,
			worldID, responseCode, record.id, record.actorID, record.request.ServiceCode,
			cityOpenWorldNullableInt64(record.providerID), record.request.RequestedTick,
			cityOpenWorldNullableInt64(record.request.QueuedTick), targetTick,
			targetTick-record.request.RequestedTick, fact.id, []byte(responseMetadata),
		); insertErr != nil {
			return fmt.Errorf("insert V7 service expiration response: %w", insertErr)
		}
		execution.facts = append(execution.facts, fact.fact)
		execution.nextFactSeq++
		execution.events = append(execution.events, worldRuntimeAutomaticEvent{
			eventType: "city.open_world.service_expired",
			payload:   map[string]any{"request_code": record.request.Code, "service_code": record.request.ServiceCode},
		})
	}
	return nil
}

func advanceCityOpenWorldV7PendingServiceRequests(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	maximumQueue int,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	ids, err := loadCityOpenWorldServiceRequestIDs(ctx, tx, `
SELECT id
FROM city_open_world_service_requests
WHERE world_id = $1 AND status = 'pending' AND deadline_tick >= $2
ORDER BY requested_tick ASC, code ASC
LIMIT $3`, worldID, targetTick, cityOpenWorldServiceMaximumRequestsPerTick)
	if err != nil {
		return fmt.Errorf("load pending V7 service requests: %w", err)
	}
	for _, id := range ids {
		record, loadErr := loadCityOpenWorldServiceRequestForUpdate(ctx, tx, worldID, id)
		if loadErr != nil || record == nil {
			if loadErr != nil {
				return loadErr
			}
			continue
		}
		if record.request.Status != "pending" || record.request.DeadlineTick < targetTick {
			continue
		}
		provider, providerErr := findNearestCityOpenWorldServiceProviderForUpdate(
			ctx, tx, worldID, record.request.ServiceCode, record.location, maximumQueue,
		)
		if providerErr != nil {
			return providerErr
		}
		if provider == nil {
			// No reachable/free endpoint is not a rejection. The request remains
			// pending until a deterministic deadline transition records expiry.
			continue
		}
		after := record.request
		after.Status = "queued"
		after.QueuedTick = cityOpenWorldInt64Pointer(targetTick)
		after.ProviderCode = cityOpenWorldStringPointer(provider.provider.Code)
		after.QueuePosition = nil // ranking is derived from the frozen sort key.
		providerID := provider.id
		fact, transitionErr := persistCityOpenWorldServiceRequestTransition(
			ctx, tx, worldID, targetTick, execution.nextFactSeq, record, after, &providerID,
			CityOpenWorldRuntimeFactServiceQueued, map[string]any{
				"provider_code": provider.provider.Code,
				"access_model":  cityOpenWorldServiceAccessModelVersion,
			},
		)
		if transitionErr != nil {
			return transitionErr
		}
		execution.facts = append(execution.facts, fact.fact)
		execution.nextFactSeq++
		execution.events = append(execution.events, worldRuntimeAutomaticEvent{
			eventType: "city.open_world.service_queued",
			payload:   map[string]any{"request_code": record.request.Code, "provider_code": provider.provider.Code},
		})
	}
	return nil
}

func advanceCityOpenWorldV7QueuedServiceRequests(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	ids, err := loadCityOpenWorldServiceRequestIDs(ctx, tx, `
SELECT request_value.id
FROM city_open_world_service_requests request_value
JOIN city_open_world_service_providers provider
  ON provider.id = request_value.provider_id AND provider.world_id = request_value.world_id
WHERE request_value.world_id = $1
  AND request_value.status = 'queued'
  AND request_value.earliest_dispatch_tick <= $2
  AND provider.status = 'active'
ORDER BY request_value.priority_milli DESC, request_value.requested_tick ASC,
         request_value.code ASC, provider.code ASC
LIMIT $3`, worldID, targetTick, cityOpenWorldServiceMaximumRequestsPerTick)
	if err != nil {
		return fmt.Errorf("load queued V7 service requests: %w", err)
	}
	type capacityState struct {
		provider  *cityOpenWorldServiceProviderRecord
		remaining int64
		settled   bool
	}
	capacities := make(map[int64]*capacityState)
	for _, id := range ids {
		record, loadErr := loadCityOpenWorldServiceRequestForUpdate(ctx, tx, worldID, id)
		if loadErr != nil || record == nil {
			if loadErr != nil {
				return loadErr
			}
			continue
		}
		if record.request.Status != "queued" || record.request.EarliestDispatchTick > targetTick || record.providerID == nil {
			continue
		}
		capacity := capacities[*record.providerID]
		if capacity == nil {
			provider, providerErr := loadCityOpenWorldServiceProviderForUpdate(ctx, tx, worldID, *record.providerID)
			if providerErr != nil {
				return providerErr
			}
			if provider == nil || provider.provider.Status != "active" || provider.provider.LastSettledTick > targetTick {
				continue
			}
			remaining := provider.provider.CapacityUnitsPerTick
			if provider.provider.LastSettledTick == targetTick {
				remaining = 0
			}
			capacity = &capacityState{provider: provider, remaining: remaining}
			capacities[*record.providerID] = capacity
		}
		if capacity.remaining <= 0 {
			continue
		}
		reserved := record.request.RequestedUnits
		if reserved > capacity.remaining {
			reserved = capacity.remaining
		}
		if reserved <= 0 {
			continue
		}
		after := record.request
		after.Status = "dispatched"
		after.DispatchedTick = cityOpenWorldInt64Pointer(targetTick)
		after.QueuePosition = nil
		after.Metadata = cityOpenWorldServiceMetadataWithReservedUnits(record.request.Metadata, reserved)
		fact, transitionErr := persistCityOpenWorldServiceRequestTransition(
			ctx, tx, worldID, targetTick, execution.nextFactSeq, record, after, record.providerID,
			CityOpenWorldRuntimeFactServiceDispatched, map[string]any{
				"provider_code":  capacity.provider.provider.Code,
				"reserved_units": reserved,
			},
		)
		if transitionErr != nil {
			return transitionErr
		}
		if !capacity.settled {
			result, updateErr := tx.ExecContext(ctx, `
UPDATE city_open_world_service_providers
SET last_settled_tick = $3, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND version = $4`,
				worldID, capacity.provider.id, targetTick, capacity.provider.provider.Version,
			)
			if updateErr != nil {
				return fmt.Errorf("settle V7 service provider: %w", updateErr)
			}
			rows, rowsErr := result.RowsAffected()
			if rowsErr != nil || rows != 1 {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v7_service_provider_version"})
			}
			capacity.settled = true
		}
		capacity.remaining -= reserved
		execution.facts = append(execution.facts, fact.fact)
		execution.nextFactSeq++
		execution.events = append(execution.events, worldRuntimeAutomaticEvent{
			eventType: "city.open_world.service_dispatched",
			payload:   map[string]any{"request_code": record.request.Code, "provider_code": capacity.provider.provider.Code, "reserved_units": reserved},
		})
	}
	return nil
}

func cityOpenWorldServiceMetadataWithReservedUnits(raw json.RawMessage, reserved int64) json.RawMessage {
	metadata := make(map[string]any)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &metadata)
	}
	metadata["reserved_units"] = reserved
	result, err := json.Marshal(metadata)
	if err != nil {
		return raw
	}
	return result
}

func cityOpenWorldServiceReservedUnits(raw json.RawMessage, fallback int64) int64 {
	var metadata struct {
		ReservedUnits *int64 `json:"reserved_units"`
	}
	if err := json.Unmarshal(raw, &metadata); err == nil && metadata.ReservedUnits != nil &&
		*metadata.ReservedUnits > 0 && *metadata.ReservedUnits <= fallback {
		return *metadata.ReservedUnits
	}
	return fallback
}

func advanceCityOpenWorldV7DispatchedServiceRequests(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	catalog map[string]CityOpenWorldServiceCatalogEntry,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	ids, err := loadCityOpenWorldServiceRequestIDs(ctx, tx, `
SELECT id
FROM city_open_world_service_requests
WHERE world_id = $1 AND status = 'dispatched'
ORDER BY dispatched_tick ASC, code ASC
LIMIT $2`, worldID, cityOpenWorldServiceMaximumRequestsPerTick)
	if err != nil {
		return fmt.Errorf("load dispatched V7 service requests: %w", err)
	}
	for _, id := range ids {
		record, loadErr := loadCityOpenWorldServiceRequestForUpdate(ctx, tx, worldID, id)
		if loadErr != nil || record == nil {
			if loadErr != nil {
				return loadErr
			}
			continue
		}
		if record.request.Status != "dispatched" || record.request.DispatchedTick == nil {
			continue
		}
		definition, found := catalog[record.request.ServiceCode]
		if !found {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v7_service_catalog_binding"})
		}
		if targetTick < *record.request.DispatchedTick+definition.TargetResponseTicks {
			continue
		}
		if record.providerID == nil {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v7_service_dispatched_provider"})
		}
		reserved := cityOpenWorldServiceReservedUnits(record.request.Metadata, record.request.RequestedUnits)
		after := record.request
		after.Status = "served"
		after.ResolvedTick = cityOpenWorldInt64Pointer(targetTick)
		after.QueuePosition = nil
		fact, transitionErr := persistCityOpenWorldServiceRequestTransition(
			ctx, tx, worldID, targetTick, execution.nextFactSeq, record, after, record.providerID,
			CityOpenWorldRuntimeFactServiceResponded, map[string]any{
				"outcome": "served", "delivered_units": reserved,
			},
		)
		if transitionErr != nil {
			return transitionErr
		}
		responseCode := "service.response." + strconv.FormatInt(record.id, 10)
		responseMetadata, marshalErr := json.Marshal(map[string]any{
			"schema_version":    cityOpenWorldServiceSchemaVersion,
			"effect_visibility": "next_tick_only",
		})
		if marshalErr != nil {
			return fmt.Errorf("marshal V7 service response: %w", marshalErr)
		}
		if _, insertErr := tx.ExecContext(ctx, `
INSERT INTO city_open_world_service_responses
    (world_id, code, request_id, actor_id, service_code, provider_id, outcome,
     requested_tick, queued_tick, dispatched_tick, resolved_tick, response_ticks,
     delivered_units, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, 'served', $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`,
			worldID, responseCode, record.id, record.actorID, record.request.ServiceCode,
			cityOpenWorldNullableInt64(record.providerID), record.request.RequestedTick,
			cityOpenWorldNullableInt64(record.request.QueuedTick), cityOpenWorldNullableInt64(record.request.DispatchedTick),
			targetTick, targetTick-record.request.RequestedTick, reserved, fact.id, []byte(responseMetadata),
		); insertErr != nil {
			return fmt.Errorf("insert V7 service response: %w", insertErr)
		}
		execution.facts = append(execution.facts, fact.fact)
		execution.nextFactSeq++
		execution.events = append(execution.events, worldRuntimeAutomaticEvent{
			eventType: "city.open_world.service_responded",
			payload:   map[string]any{"request_code": record.request.Code, "outcome": "served", "delivered_units": reserved},
		})
	}
	return nil
}
