package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

type cityOpenWorldMobilityArrivalRecord struct {
	id           int64
	routeID      int64
	demandID     int64
	actorID      int64
	sourceFactID int64
	lastFactID   int64
	arrival      CityOpenWorldMobilityArrival
}

type cityOpenWorldMobilityArrivalCandidate struct {
	routeID      int64
	demandID     int64
	actorID      int64
	sourceFactID int64
	routeCode    string
	demandCode   string
	actorCode    string
	destination  string
	metadata     json.RawMessage
}

func cityOpenWorldMobilityArrivalCode(routeCode string) string {
	sum := sha256.Sum256([]byte(routeCode))
	return "mobility.arrival." + hex.EncodeToString(sum[:20])
}

func loadCityOpenWorldMobilityArrivalPolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldMobilityArrivalPolicy, error) {
	policy := &CityOpenWorldMobilityArrivalPolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       bridge_contract, landing_contract, maximum_arrivals_per_tick,
       landing_search_radius, maximum_blocked_attempts, arrival_count,
       landed_count, blocked_count, failed_count, revision, metadata
FROM city_open_world_mobility_arrival_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash, &policy.BaselineTick,
		&policy.BridgeContract, &policy.LandingContract, &policy.MaximumArrivalsPerTick,
		&policy.LandingSearchRadius, &policy.MaximumBlockedAttempts, &policy.ArrivalCount,
		&policy.LandedCount, &policy.BlockedCount, &policy.FailedCount, &policy.Revision,
		&policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v10_arrival_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V10 arrival profile: %w", err)
	}
	if err = validateCityOpenWorldMobilityArrivalPolicy(*policy); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v10_arrival_profile"}).WithCause(err)
	}
	return policy, nil
}

func updateCityOpenWorldMobilityArrivalPolicy(
	ctx context.Context,
	tx *sql.Tx,
	worldID, arrivalDelta, landedDelta, blockedDelta, failedDelta int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_arrival_profiles
SET arrival_count = arrival_count + $2,
    landed_count = landed_count + $3,
    blocked_count = blocked_count + $4,
    failed_count = failed_count + $5,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID, arrivalDelta, landedDelta, blockedDelta, failedDelta)
	if err != nil {
		return fmt.Errorf("update V10 arrival profile: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v10_arrival_profile"})
	}
	return nil
}

// advanceCityOpenWorldV10MobilityArrivals is deliberately ordered after the
// V9 route reducer and before command handling. A route completed at tick T
// is eligible only when its completion fact is older than the current tick;
// therefore no same-tick aggregate route may silently rewrite local position.
func advanceCityOpenWorldV10MobilityArrivals(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence int64,
) (cityOpenWorldRuntimeAutomaticExecution, error) {
	execution := cityOpenWorldRuntimeAutomaticExecution{
		facts: make([]CityOpenWorldRuntimeFact, 0), effects: make([]CityOpenWorldRuntimeEffect, 0),
		events: make([]worldRuntimeAutomaticEvent, 0), nextFactSeq: factSequence, nextEffectSeq: effectSequence,
	}
	policy, err := loadCityOpenWorldMobilityArrivalPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldMobilityArrivalWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = registerCityOpenWorldMobilityArrivals(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if err = landCityOpenWorldMobilityArrivals(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if len(execution.facts) > 0 || len(execution.effects) > 0 {
		if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, int64(len(execution.facts)), int64(len(execution.effects)), 0); err != nil {
			return execution, err
		}
	}
	// Validate counters and lifecycle rows in the same transaction as the
	// facts that caused them. This catches projector drift before the tick can
	// become canonical rather than deferring discovery to a later read.
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_arrival_foundation($1)`, worldID); err != nil {
		return execution, fmt.Errorf("validate V10 arrival bridge after advancement: %w", err)
	}
	return execution, nil
}

func registerCityOpenWorldMobilityArrivals(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldMobilityArrivalPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v10_arrival_registration"})
	}
	rows, err := tx.QueryContext(ctx, `
SELECT route.id, demand.id, route.actor_id, route.completion_fact_id,
       route.code, demand.code, actor.code, route.destination_hub_code, demand.metadata
FROM city_open_world_mobility_routes route
JOIN city_open_world_mobility_demands demand
  ON demand.id = route.demand_id AND demand.world_id = route.world_id
JOIN city_open_world_actors actor
  ON actor.id = route.actor_id AND actor.world_id = route.world_id
JOIN city_open_world_runtime_facts completion_fact
  ON completion_fact.id = route.completion_fact_id AND completion_fact.world_id = route.world_id
WHERE route.world_id = $1
  AND route.status = 'completed'
  AND demand.status = 'completed'
  AND route.completion_fact_id IS NOT NULL
  AND completion_fact.tick < $2
  AND demand.requested_tick > $3
  AND COALESCE(demand.metadata->'transport_adapter'->>'arrival_bridge', '') <> 'excluded'
  AND NOT EXISTS (
      SELECT 1 FROM city_open_world_mobility_arrivals arrival
      WHERE arrival.world_id = route.world_id AND arrival.route_id = route.id
  )
ORDER BY completion_fact.tick ASC, completion_fact.sequence ASC, route.code ASC
LIMIT $4
FOR UPDATE OF route, demand`, worldID, targetTick, policy.BaselineTick, policy.MaximumArrivalsPerTick)
	if err != nil {
		return fmt.Errorf("load V10 eligible mobility arrivals: %w", err)
	}
	candidates := make([]cityOpenWorldMobilityArrivalCandidate, 0)
	for rows.Next() {
		item := cityOpenWorldMobilityArrivalCandidate{}
		if err = rows.Scan(&item.routeID, &item.demandID, &item.actorID, &item.sourceFactID,
			&item.routeCode, &item.demandCode, &item.actorCode, &item.destination, &item.metadata); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan V10 eligible mobility arrival: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err = rows.Close(); err != nil {
		return fmt.Errorf("iterate V10 eligible mobility arrivals: %w", err)
	}
	for _, candidate := range candidates {
		expectedOrigin, originErr := cityOpenWorldMobilityArrivalExpectedOrigin(candidate.metadata, candidate.actorCode)
		if originErr != nil || expectedOrigin == nil {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "open_world_v10_arrival_origin", "demand": candidate.demandCode,
			}).WithCause(originErr)
		}
		if err = insertCityOpenWorldMobilityArrivalPending(ctx, tx, worldID, targetTick, candidate, *expectedOrigin, execution); err != nil {
			return err
		}
	}
	return nil
}

func insertCityOpenWorldMobilityArrivalPending(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	candidate cityOpenWorldMobilityArrivalCandidate,
	expectedOrigin CityOpenWorldActorLocation,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	payload, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldMobilityArrivalSchemaVersion,
		"route_code":     candidate.routeCode, "demand_code": candidate.demandCode,
		"destination_hub_code": candidate.destination,
		"expected_origin":      expectedOrigin,
		"bridge_contract":      cityOpenWorldMobilityArrivalBridgeContract,
	})
	if err != nil {
		return fmt.Errorf("marshal V10 arrival pending fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		parentFactID: &candidate.sourceFactID, actorID: &candidate.actorID,
		factType: CityOpenWorldRuntimeFactMobilityArrivalPending, payload: payload,
	})
	if err != nil {
		return err
	}
	expectedRaw, err := json.Marshal(expectedOrigin)
	if err != nil {
		return fmt.Errorf("marshal V10 arrival expected origin: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":        cityOpenWorldMobilityArrivalSchemaVersion,
		"registration_contract": cityOpenWorldMobilityArrivalBridgeContract,
	})
	if err != nil {
		return fmt.Errorf("marshal V10 arrival metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_arrivals
    (world_id, code, route_id, demand_id, actor_id, destination_hub_code,
     expected_origin, status, blocked_attempts, next_attempt_tick, created_tick,
     updated_tick, source_fact_id, last_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, 'pending', 0, $8, $8,
        $8, $9, $10, 1, $11::jsonb)`,
		worldID, cityOpenWorldMobilityArrivalCode(candidate.routeCode), candidate.routeID,
		candidate.demandID, candidate.actorID, candidate.destination, []byte(expectedRaw),
		targetTick, candidate.sourceFactID, fact.id, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V10 mobility arrival %s: %w", candidate.routeCode, err)
	}
	if err = updateCityOpenWorldMobilityArrivalPolicy(ctx, tx, worldID, 1, 0, 0, 0); err != nil {
		return err
	}
	// Runtime effects, when present, may only reference a draft fact. Keep the
	// lifecycle consistent with V5 automatic navigation: project all rows first,
	// then seal the causal fact as the final durable step for this transition.
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.mobility_arrival_pending", payload: map[string]any{
		"route_code": candidate.routeCode, "demand_code": candidate.demandCode,
		"actor_code": candidate.actorCode,
	}})
	return nil
}

func landCityOpenWorldMobilityArrivals(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldMobilityArrivalPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v10_arrival_landing"})
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id
FROM city_open_world_mobility_arrivals
WHERE world_id = $1 AND status IN ('pending', 'blocked') AND next_attempt_tick <= $2
ORDER BY next_attempt_tick ASC, created_tick ASC, code ASC
LIMIT $3
FOR UPDATE`, worldID, targetTick, policy.MaximumArrivalsPerTick)
	if err != nil {
		return fmt.Errorf("load due V10 mobility arrivals: %w", err)
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan due V10 mobility arrival: %w", err)
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return fmt.Errorf("iterate due V10 mobility arrivals: %w", err)
	}
	for _, id := range ids {
		if err = advanceCityOpenWorldMobilityArrival(ctx, tx, worldID, targetTick, policy, id, execution); err != nil {
			return err
		}
	}
	return nil
}

func loadCityOpenWorldMobilityArrivalForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID, id int64,
) (*cityOpenWorldMobilityArrivalRecord, error) {
	record := &cityOpenWorldMobilityArrivalRecord{id: id}
	var expectedRaw, landingRaw []byte
	var landingTick, failedTick, landingFactTick, landingFactSequence sql.NullInt64
	err := tx.QueryRowContext(ctx, `
SELECT arrival.id, arrival.route_id, arrival.demand_id, arrival.actor_id,
       arrival.source_fact_id, arrival.last_fact_id, arrival.code, route.code,
       demand.code, actor.code, arrival.destination_hub_code, arrival.expected_origin,
       arrival.landing_location, arrival.status, arrival.blocked_attempts,
       arrival.next_attempt_tick, arrival.created_tick, arrival.updated_tick,
       source_fact.tick, source_fact.sequence, last_fact.tick, last_fact.sequence,
       landing_fact.tick, landing_fact.sequence, arrival.landed_tick, arrival.failed_tick,
       arrival.version, arrival.metadata
FROM city_open_world_mobility_arrivals arrival
JOIN city_open_world_mobility_routes route
  ON route.id = arrival.route_id AND route.world_id = arrival.world_id
JOIN city_open_world_mobility_demands demand
  ON demand.id = arrival.demand_id AND demand.world_id = arrival.world_id
JOIN city_open_world_actors actor
  ON actor.id = arrival.actor_id AND actor.world_id = arrival.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = arrival.source_fact_id AND source_fact.world_id = arrival.world_id
JOIN city_open_world_runtime_facts last_fact
  ON last_fact.id = arrival.last_fact_id AND last_fact.world_id = arrival.world_id
LEFT JOIN city_open_world_runtime_facts landing_fact
  ON landing_fact.id = arrival.landing_fact_id AND landing_fact.world_id = arrival.world_id
WHERE arrival.world_id = $1 AND arrival.id = $2
FOR UPDATE OF arrival`, worldID, id).Scan(
		&record.id, &record.routeID, &record.demandID, &record.actorID,
		&record.sourceFactID, &record.lastFactID, &record.arrival.Code, &record.arrival.RouteCode,
		&record.arrival.DemandCode, &record.arrival.ActorCode, &record.arrival.DestinationHubCode,
		&expectedRaw, &landingRaw, &record.arrival.Status, &record.arrival.BlockedAttempts,
		&record.arrival.NextAttemptTick, &record.arrival.CreatedTick, &record.arrival.UpdatedTick,
		&record.arrival.SourceFact.Tick, &record.arrival.SourceFact.Sequence,
		&record.arrival.LastFact.Tick, &record.arrival.LastFact.Sequence,
		&landingFactTick, &landingFactSequence, &landingTick, &failedTick,
		&record.arrival.Version, &record.arrival.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock V10 mobility arrival: %w", err)
	}
	if err = json.Unmarshal(expectedRaw, &record.arrival.ExpectedOrigin); err != nil {
		return nil, fmt.Errorf("decode V10 mobility arrival expected origin: %w", err)
	}
	if len(landingRaw) > 0 {
		landing := CityOpenWorldActorLocation{}
		if err = json.Unmarshal(landingRaw, &landing); err != nil {
			return nil, fmt.Errorf("decode V10 mobility arrival landing: %w", err)
		}
		record.arrival.LandingLocation = &landing
	}
	record.arrival.LandedTick = nullInt64Pointer(landingTick)
	record.arrival.FailedTick = nullInt64Pointer(failedTick)
	if landingFactTick.Valid {
		record.arrival.LandingFact = &CityOpenWorldRuntimeFactRef{Tick: landingFactTick.Int64, Sequence: landingFactSequence.Int64}
	}
	return record, nil
}

func advanceCityOpenWorldMobilityArrival(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldMobilityArrivalPolicy,
	id int64,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	record, err := loadCityOpenWorldMobilityArrivalForUpdate(ctx, tx, worldID, id)
	if err != nil || record == nil {
		return err
	}
	if (record.arrival.Status != cityOpenWorldMobilityArrivalStatusPending && record.arrival.Status != cityOpenWorldMobilityArrivalStatusBlocked) ||
		record.arrival.NextAttemptTick > targetTick {
		return nil
	}
	actor, err := loadCityOpenWorldV5NavigationActorForUpdate(ctx, tx, worldID, record.arrival.ActorCode)
	if err != nil {
		return err
	}
	if !cityOpenWorldRuntimeLocationEqual(actor.location, record.arrival.ExpectedOrigin) {
		return failCityOpenWorldMobilityArrival(ctx, tx, worldID, targetTick, record,
			cityOpenWorldMobilityArrivalReasonOriginChanged, map[string]any{"current_location": actor.location}, execution)
	}
	intent, err := loadCityOpenWorldV5NavigationIntent(ctx, tx, worldID, record.arrival.ActorCode, true)
	if err != nil {
		return err
	}
	if intent != nil && intent.intent.Status == cityOpenWorldV5NavigationStatusActive {
		return failCityOpenWorldMobilityArrival(ctx, tx, worldID, targetTick, record,
			cityOpenWorldMobilityArrivalReasonNavigationBusy, map[string]any{"navigation_intent_code": intent.intent.IntentCode}, execution)
	}
	hub, err := loadCityOpenWorldMobilityHub(ctx, tx, worldID, record.arrival.DestinationHubCode)
	if err != nil {
		return err
	}
	if hub == nil || hub.AnchorZ != 0 {
		return failCityOpenWorldMobilityArrival(ctx, tx, worldID, targetTick, record,
			cityOpenWorldMobilityArrivalReasonTargetInvalid, nil, execution)
	}
	anchor, err := cityOpenWorldRuntimeLocationFromPortalPoint(actor.actor.Code, "surface", "", 0,
		cityspatial.WorldgenPoint{X: hub.AnchorX, Y: hub.AnchorY, Z: 0})
	if err != nil {
		return failCityOpenWorldMobilityArrival(ctx, tx, worldID, targetTick, record,
			cityOpenWorldMobilityArrivalReasonTargetInvalid, nil, execution)
	}
	if _, _, err = ensureCityOpenWorldSectorMaterialized(ctx, tx, worldID, targetTick, anchor.SectorX, anchor.SectorY); err != nil {
		return fmt.Errorf("materialize V10 mobility arrival destination: %w", err)
	}
	landing, err := findCityOpenWorldRuntimeNearbySpawnLocation(
		ctx, tx, worldID, actor.id, actor.actor.Code, hub.AnchorX, hub.AnchorY, policy.LandingSearchRadius,
	)
	if err != nil {
		if cityOpenWorldRuntimeBusinessRejectionCode(err) == cityOpenWorldRuntimeRejectionSpawnUnavailable {
			return blockOrFailCityOpenWorldMobilityArrival(ctx, tx, worldID, targetTick, policy, record, execution)
		}
		return err
	}
	return landCityOpenWorldMobilityArrival(ctx, tx, worldID, targetTick, record, actor, landing, execution)
}

func cityOpenWorldMobilityArrivalFact(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldMobilityArrivalRecord,
	factType string,
	payload map[string]any,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) (*cityOpenWorldRuntimeFactRecord, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal V10 mobility arrival fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		parentFactID: &record.lastFactID, actorID: &record.actorID, factType: factType, payload: raw,
	})
	if err != nil {
		return nil, err
	}
	return fact, nil
}

func failCityOpenWorldMobilityArrival(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldMobilityArrivalRecord,
	reason string,
	detail map[string]any,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	payload := map[string]any{
		"schema_version": cityOpenWorldMobilityArrivalSchemaVersion,
		"arrival_code":   record.arrival.Code, "route_code": record.arrival.RouteCode,
		"reason": reason,
	}
	for key, value := range detail {
		payload[key] = value
	}
	fact, err := cityOpenWorldMobilityArrivalFact(ctx, tx, worldID, targetTick, record,
		CityOpenWorldRuntimeFactMobilityArrivalFailed, payload, execution)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_arrivals
SET status = 'failed', next_attempt_tick = $3, updated_tick = $3,
    last_fact_id = $4, failed_tick = $3, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status IN ('pending', 'blocked')`, worldID, record.id, targetTick, fact.id); err != nil {
		return fmt.Errorf("fail V10 mobility arrival %s: %w", record.arrival.Code, err)
	}
	if err = updateCityOpenWorldMobilityArrivalPolicy(ctx, tx, worldID, 0, 0, 0, 1); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.mobility_arrival_failed", payload: map[string]any{
		"arrival_code": record.arrival.Code, "route_code": record.arrival.RouteCode,
		"actor_code": record.arrival.ActorCode, "reason": reason,
	}})
	return nil
}

func blockOrFailCityOpenWorldMobilityArrival(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldMobilityArrivalPolicy,
	record *cityOpenWorldMobilityArrivalRecord,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	// MaximumBlockedAttempts is the number of recorded `blocked` lifecycle
	// transitions. Once those retries have all been recorded, the next attempt
	// terminates with an explicit failure fact instead of exceeding policy.
	if record.arrival.BlockedAttempts >= policy.MaximumBlockedAttempts {
		return failCityOpenWorldMobilityArrival(ctx, tx, worldID, targetTick, record,
			cityOpenWorldMobilityArrivalReasonTargetBlocked,
			map[string]any{"blocked_attempts": record.arrival.BlockedAttempts}, execution)
	}
	payload := map[string]any{
		"schema_version": cityOpenWorldMobilityArrivalSchemaVersion,
		"arrival_code":   record.arrival.Code, "route_code": record.arrival.RouteCode,
		"reason":          cityOpenWorldMobilityArrivalReasonTargetBlocked,
		"blocked_attempt": record.arrival.BlockedAttempts + 1,
	}
	fact, err := cityOpenWorldMobilityArrivalFact(ctx, tx, worldID, targetTick, record,
		CityOpenWorldRuntimeFactMobilityArrivalBlocked, payload, execution)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_arrivals
SET status = 'blocked', blocked_attempts = blocked_attempts + 1,
    next_attempt_tick = $3, updated_tick = $4, last_fact_id = $5,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status IN ('pending', 'blocked')`,
		worldID, record.id, targetTick+1, targetTick, fact.id); err != nil {
		return fmt.Errorf("block V10 mobility arrival %s: %w", record.arrival.Code, err)
	}
	if err = updateCityOpenWorldMobilityArrivalPolicy(ctx, tx, worldID, 0, 0, 1, 0); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.mobility_arrival_blocked", payload: map[string]any{
		"arrival_code": record.arrival.Code, "route_code": record.arrival.RouteCode,
		"actor_code": record.arrival.ActorCode,
	}})
	return nil
}

func landCityOpenWorldMobilityArrival(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	record *cityOpenWorldMobilityArrivalRecord,
	actor *cityOpenWorldRuntimeActorRef,
	landing CityOpenWorldActorLocation,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	payload := map[string]any{
		"schema_version": cityOpenWorldMobilityArrivalSchemaVersion,
		"arrival_code":   record.arrival.Code, "route_code": record.arrival.RouteCode,
		"from": actor.location, "to": landing,
		"landing_contract": cityOpenWorldMobilityArrivalLandingContract,
	}
	fact, err := cityOpenWorldMobilityArrivalFact(ctx, tx, worldID, targetTick, record,
		CityOpenWorldRuntimeFactMobilityArrivalLanded, payload, execution)
	if err != nil {
		return err
	}
	landing.MovedTick = targetTick
	landing.Version = actor.location.Version + 1
	landing.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: fact.fact.Tick, Sequence: fact.fact.Sequence}
	landing.Metadata = json.RawMessage(`{}`)
	if err = updateCityOpenWorldActorLocation(ctx, tx, worldID, actor.id, targetTick, fact.id, landing); err != nil {
		return err
	}
	locationPayload, err := json.Marshal(map[string]any{
		"from": actor.location, "to": landing, "source": "mobility_arrival_bridge",
	})
	if err != nil {
		return fmt.Errorf("marshal V10 arrival location effect: %w", err)
	}
	effect, err := insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextEffectSeq, sourceFact: fact,
		operationIndex: 1, effectType: WorldRuntimeEffectLocationSet, targetActorID: &actor.id,
		targetActorCode: &actor.actor.Code, targetKey: cityOpenWorldStringPointer("location"), payload: locationPayload,
	})
	if err != nil {
		return err
	}
	landingRaw, err := json.Marshal(landing)
	if err != nil {
		return fmt.Errorf("marshal V10 arrival landing location: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_arrivals
SET landing_location = $3::jsonb, status = 'landed', next_attempt_tick = $4,
    updated_tick = $4, last_fact_id = $5, landing_fact_id = $5, landed_tick = $4,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status IN ('pending', 'blocked')`,
		worldID, record.id, []byte(landingRaw), targetTick, fact.id); err != nil {
		return fmt.Errorf("land V10 mobility arrival %s: %w", record.arrival.Code, err)
	}
	if err = updateCityOpenWorldMobilityArrivalPolicy(ctx, tx, worldID, 0, 1, 0, 0); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.effects = append(execution.effects, effect)
	execution.nextFactSeq++
	execution.nextEffectSeq++
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.mobility_arrival_landed", payload: map[string]any{
		"arrival_code": record.arrival.Code, "route_code": record.arrival.RouteCode,
		"actor_code": record.arrival.ActorCode, "landing_location": landing,
	}})
	return nil
}
