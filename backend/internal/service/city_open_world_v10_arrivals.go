package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	cityOpenWorldMobilityArrivalSchemaVersion        = 1
	cityOpenWorldMobilityArrivalProfileID            = "sub2api-open-world-mobility-arrival"
	cityOpenWorldMobilityArrivalProfileVersion       = "1.0.0"
	cityOpenWorldMobilityArrivalBridgeContract       = "completed_route_next_tick_bridge_v1"
	cityOpenWorldMobilityArrivalLandingContract      = "validated_surface_anchor_landing_v1"
	cityOpenWorldMobilityArrivalMaximumPerTick       = 64
	cityOpenWorldMobilityArrivalLandingSearchRadius  = int64(12)
	cityOpenWorldMobilityArrivalMaximumBlocked       = 8
	cityOpenWorldMobilityArrivalStatusPending        = "pending"
	cityOpenWorldMobilityArrivalStatusBlocked        = "blocked"
	cityOpenWorldMobilityArrivalStatusLanded         = "landed"
	cityOpenWorldMobilityArrivalStatusFailed         = "failed"
	cityOpenWorldMobilityArrivalReasonOriginChanged  = "origin_location_changed"
	cityOpenWorldMobilityArrivalReasonTargetBlocked  = "landing_unavailable"
	cityOpenWorldMobilityArrivalReasonTargetInvalid  = "landing_target_invalid"
	cityOpenWorldMobilityArrivalReasonNavigationBusy = "navigation_intent_active"
)

// CityOpenWorldMobilityArrivalPolicy freezes the cross-scale hand-off rules
// independently from V9's aggregate route graph.  A route completion is not
// a raw coordinate write: V10 consumes it in a later tick, validates a real
// materialized surface landing, and records the resulting local hand-off.
type CityOpenWorldMobilityArrivalPolicy struct {
	ProfileID              string          `json:"profile_id"`
	ProfileVersion         string          `json:"profile_version"`
	ContentHash            string          `json:"content_hash"`
	BaselineTick           int64           `json:"baseline_tick"`
	BridgeContract         string          `json:"bridge_contract"`
	LandingContract        string          `json:"landing_contract"`
	MaximumArrivalsPerTick int             `json:"maximum_arrivals_per_tick"`
	LandingSearchRadius    int64           `json:"landing_search_radius"`
	MaximumBlockedAttempts int             `json:"maximum_blocked_attempts"`
	ArrivalCount           int64           `json:"arrival_count"`
	LandedCount            int64           `json:"landed_count"`
	BlockedCount           int64           `json:"blocked_count"`
	FailedCount            int64           `json:"failed_count"`
	Revision               int64           `json:"revision"`
	Metadata               json.RawMessage `json:"metadata"`
}

// CityOpenWorldMobilityArrival is the fact-backed boundary between macro
// travel and the open-world local coordinate model. ExpectedOrigin captures
// the actor state at demand acceptance.  If a player changes that state before
// hand-off, V10 fails safely rather than overwriting a newer local action.
type CityOpenWorldMobilityArrival struct {
	Code               string                       `json:"code"`
	RouteCode          string                       `json:"route_code"`
	DemandCode         string                       `json:"demand_code"`
	ActorCode          string                       `json:"actor_code"`
	DestinationHubCode string                       `json:"destination_hub_code"`
	ExpectedOrigin     CityOpenWorldActorLocation   `json:"expected_origin"`
	LandingLocation    *CityOpenWorldActorLocation  `json:"landing_location,omitempty"`
	Status             string                       `json:"status"`
	BlockedAttempts    int                          `json:"blocked_attempts"`
	NextAttemptTick    int64                        `json:"next_attempt_tick"`
	CreatedTick        int64                        `json:"created_tick"`
	UpdatedTick        int64                        `json:"updated_tick"`
	SourceFact         CityOpenWorldRuntimeFactRef  `json:"source_fact"`
	LastFact           CityOpenWorldRuntimeFactRef  `json:"last_fact"`
	LandingFact        *CityOpenWorldRuntimeFactRef `json:"landing_fact,omitempty"`
	LandedTick         *int64                       `json:"landed_tick,omitempty"`
	FailedTick         *int64                       `json:"failed_tick,omitempty"`
	Version            int64                        `json:"version"`
	Metadata           json.RawMessage              `json:"metadata"`
}

// CityOpenWorldMobilityArrivalState is a V10 sibling of V9 mobility state.
// It intentionally owns no hub topology or route capacity; those remain V9
// facts. This prevents a local arrival adapter from rewriting transport truth.
type CityOpenWorldMobilityArrivalState struct {
	Policy   CityOpenWorldMobilityArrivalPolicy `json:"policy"`
	Arrivals []CityOpenWorldMobilityArrival     `json:"arrivals"`
}

type cityOpenWorldMobilityArrivalDemandMetadata struct {
	ArrivalBridge *struct {
		Contract       string                     `json:"contract"`
		ExpectedOrigin CityOpenWorldActorLocation `json:"expected_origin"`
	} `json:"arrival_bridge,omitempty"`
}

func cityOpenWorldMobilityArrivalExpectedOrigin(
	raw json.RawMessage,
	actorCode string,
) (*CityOpenWorldActorLocation, error) {
	metadata := cityOpenWorldMobilityArrivalDemandMetadata{}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, fmt.Errorf("decode V10 mobility demand metadata: %w", err)
	}
	if metadata.ArrivalBridge == nil {
		return nil, nil
	}
	location := metadata.ArrivalBridge.ExpectedOrigin
	if metadata.ArrivalBridge.Contract != "captured_request_location_v1" ||
		!cityOpenWorldMobilityArrivalLocationValid(location, actorCode) {
		return nil, fmt.Errorf("invalid V10 mobility demand arrival contract")
	}
	return &location, nil
}

func cityOpenWorldMobilityArrivalPolicyHash(
	bridgeContract, landingContract string,
	maximumArrivalsPerTick int,
	landingSearchRadius int64,
	maximumBlockedAttempts int,
) (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion          int    `json:"schema_version"`
		ProfileID              string `json:"profile_id"`
		ProfileVersion         string `json:"profile_version"`
		BridgeContract         string `json:"bridge_contract"`
		LandingContract        string `json:"landing_contract"`
		MaximumArrivalsPerTick int    `json:"maximum_arrivals_per_tick"`
		LandingSearchRadius    int64  `json:"landing_search_radius"`
		MaximumBlockedAttempts int    `json:"maximum_blocked_attempts"`
	}{
		SchemaVersion:  cityOpenWorldMobilityArrivalSchemaVersion,
		ProfileID:      cityOpenWorldMobilityArrivalProfileID,
		ProfileVersion: cityOpenWorldMobilityArrivalProfileVersion,
		BridgeContract: bridgeContract, LandingContract: landingContract,
		MaximumArrivalsPerTick: maximumArrivalsPerTick,
		LandingSearchRadius:    landingSearchRadius,
		MaximumBlockedAttempts: maximumBlockedAttempts,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func activateCityOpenWorldMobilityArrivalBootstrapWrite(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_arrival_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world arrival bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldMobilityArrivalWrite(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_arrival_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world arrival write: %w", err)
	}
	return nil
}

// initializeCityOpenWorldV10ArrivalFoundation adds only the immutable bridge
// policy. It intentionally does not invent arrivals for completed V9 routes;
// V10's baseline tick excludes historical trips without a captured origin.
func initializeCityOpenWorldV10ArrivalFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("load V10 arrival world: %w", err)
	}
	if !cityEngineSupportsOpenWorldArrivalBridge(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_mobility_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V10 mobility prerequisite: %w", err)
	}
	if err := activateCityOpenWorldMobilityArrivalBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	contentHash, err := cityOpenWorldMobilityArrivalPolicyHash(
		cityOpenWorldMobilityArrivalBridgeContract,
		cityOpenWorldMobilityArrivalLandingContract,
		cityOpenWorldMobilityArrivalMaximumPerTick,
		cityOpenWorldMobilityArrivalLandingSearchRadius,
		cityOpenWorldMobilityArrivalMaximumBlocked,
	)
	if err != nil {
		return fmt.Errorf("hash V10 arrival policy: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":  cityOpenWorldMobilityArrivalSchemaVersion,
		"baseline_scope":  "post_baseline_demands_only",
		"origin_contract": "captured_request_location_v1",
		"landing_scope":   "validated_materialized_surface_v1",
	})
	if err != nil {
		return fmt.Errorf("marshal V10 arrival profile metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_arrival_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     bridge_contract, landing_contract, maximum_arrivals_per_tick,
     landing_search_radius, maximum_blocked_attempts, arrival_count,
     landed_count, blocked_count, failed_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
        0, 0, 0, 0, 1, $11::jsonb)`,
		worldID, cityOpenWorldMobilityArrivalProfileID, cityOpenWorldMobilityArrivalProfileVersion,
		contentHash, baselineTick, cityOpenWorldMobilityArrivalBridgeContract,
		cityOpenWorldMobilityArrivalLandingContract, cityOpenWorldMobilityArrivalMaximumPerTick,
		cityOpenWorldMobilityArrivalLandingSearchRadius, cityOpenWorldMobilityArrivalMaximumBlocked,
		[]byte(metadata)); err != nil {
		return fmt.Errorf("insert V10 arrival profile: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_arrival_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V10 arrival foundation: %w", err)
	}
	return nil
}

func loadCityOpenWorldMobilityArrivalState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldMobilityArrivalState, error) {
	state := &CityOpenWorldMobilityArrivalState{Arrivals: make([]CityOpenWorldMobilityArrival, 0)}
	if err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       bridge_contract, landing_contract, maximum_arrivals_per_tick,
       landing_search_radius, maximum_blocked_attempts, arrival_count,
       landed_count, blocked_count, failed_count, revision, metadata
FROM city_open_world_mobility_arrival_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash,
		&state.Policy.BaselineTick, &state.Policy.BridgeContract, &state.Policy.LandingContract,
		&state.Policy.MaximumArrivalsPerTick, &state.Policy.LandingSearchRadius,
		&state.Policy.MaximumBlockedAttempts, &state.Policy.ArrivalCount, &state.Policy.LandedCount,
		&state.Policy.BlockedCount, &state.Policy.FailedCount, &state.Policy.Revision,
		&state.Policy.Metadata,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityOpenWorldRuntimeNotFound
	} else if err != nil {
		return nil, fmt.Errorf("load V10 arrival profile: %w", err)
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT arrival.code, route.code, demand.code, actor.code, arrival.destination_hub_code,
       arrival.expected_origin, arrival.landing_location, arrival.status,
       arrival.blocked_attempts, arrival.next_attempt_tick, arrival.created_tick,
       arrival.updated_tick, source_fact.tick, source_fact.sequence,
       last_fact.tick, last_fact.sequence, landing_fact.tick, landing_fact.sequence,
       arrival.landed_tick, arrival.failed_tick, arrival.version, arrival.metadata
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
WHERE arrival.world_id = $1
ORDER BY arrival.created_tick ASC, arrival.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V10 mobility arrivals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		item := CityOpenWorldMobilityArrival{}
		var expectedRaw, landingRaw []byte
		var landingTick, failedTick, landingFactTick, landingFactSequence sql.NullInt64
		if err = rows.Scan(
			&item.Code, &item.RouteCode, &item.DemandCode, &item.ActorCode, &item.DestinationHubCode,
			&expectedRaw, &landingRaw, &item.Status, &item.BlockedAttempts,
			&item.NextAttemptTick, &item.CreatedTick, &item.UpdatedTick,
			&item.SourceFact.Tick, &item.SourceFact.Sequence,
			&item.LastFact.Tick, &item.LastFact.Sequence,
			&landingFactTick, &landingFactSequence, &landingTick, &failedTick,
			&item.Version, &item.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan V10 mobility arrival: %w", err)
		}
		if err = json.Unmarshal(expectedRaw, &item.ExpectedOrigin); err != nil {
			return nil, fmt.Errorf("decode V10 arrival expected origin %s: %w", item.Code, err)
		}
		if len(landingRaw) > 0 {
			landing := CityOpenWorldActorLocation{}
			if err = json.Unmarshal(landingRaw, &landing); err != nil {
				return nil, fmt.Errorf("decode V10 arrival landing %s: %w", item.Code, err)
			}
			item.LandingLocation = &landing
		}
		item.LandedTick = nullInt64Pointer(landingTick)
		item.FailedTick = nullInt64Pointer(failedTick)
		if landingFactTick.Valid {
			item.LandingFact = &CityOpenWorldRuntimeFactRef{Tick: landingFactTick.Int64, Sequence: landingFactSequence.Int64}
		}
		state.Arrivals = append(state.Arrivals, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V10 mobility arrivals: %w", err)
	}
	if err = validateCityOpenWorldMobilityArrivalState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v10_arrivals"}).WithCause(err)
	}
	return state, nil
}

func cityOpenWorldMobilityArrivalLocationValid(location CityOpenWorldActorLocation, actorCode string) bool {
	if location.ActorCode != actorCode || location.Version < 1 || location.MovedTick < 0 || !json.Valid(location.Metadata) {
		return false
	}
	normalized, err := cityOpenWorldRuntimeLocationFromPayload(cityOpenWorldActorMovePayload{
		ActorCode: actorCode, SpaceKind: location.SpaceKind,
		BuildingCode: cityOpenWorldV5StringValue(location.BuildingCode), FloorIndex: location.FloorIndex,
		X: location.X, Y: location.Y, Z: location.Z,
	})
	if err != nil || !cityOpenWorldRuntimeLocationEqual(normalized, location) {
		return false
	}
	return normalized.SectorX == location.SectorX && normalized.SectorY == location.SectorY &&
		normalized.ChunkX == location.ChunkX && normalized.ChunkY == location.ChunkY &&
		normalized.LocalX == location.LocalX && normalized.LocalY == location.LocalY
}

func validateCityOpenWorldMobilityArrivalState(state *CityOpenWorldMobilityArrivalState) error {
	if state == nil {
		return fmt.Errorf("V10 arrival state is missing")
	}
	policy := state.Policy
	if err := validateCityOpenWorldMobilityArrivalPolicy(policy); err != nil {
		return err
	}
	seenCodes := make(map[string]struct{}, len(state.Arrivals))
	seenRoutes := make(map[string]struct{}, len(state.Arrivals))
	landed, failed := int64(0), int64(0)
	blocked := int64(0)
	for _, arrival := range state.Arrivals {
		if _, duplicate := seenCodes[arrival.Code]; duplicate || !worldRuntimeCodeValid(arrival.Code, 160) ||
			arrival.RouteCode == "" || arrival.DemandCode == "" || !worldRuntimeCodeValid(arrival.RouteCode, 160) ||
			!worldRuntimeCodeValid(arrival.DemandCode, 160) || !worldRuntimeCodeValid(arrival.ActorCode, 128) ||
			!worldRuntimeCodeValid(arrival.DestinationHubCode, 160) ||
			!cityOpenWorldMobilityArrivalLocationValid(arrival.ExpectedOrigin, arrival.ActorCode) ||
			arrival.BlockedAttempts < 0 || arrival.BlockedAttempts > policy.MaximumBlockedAttempts ||
			arrival.NextAttemptTick < 1 || arrival.CreatedTick < 1 || arrival.UpdatedTick < arrival.CreatedTick ||
			arrival.SourceFact.Tick < 1 || arrival.SourceFact.Sequence < 1 ||
			arrival.LastFact.Tick < 1 || arrival.LastFact.Sequence < 1 || arrival.Version < 1 ||
			!json.Valid(arrival.Metadata) {
			return fmt.Errorf("invalid V10 arrival %s", arrival.Code)
		}
		if _, duplicate := seenRoutes[arrival.RouteCode]; duplicate {
			return fmt.Errorf("duplicated V10 arrival route %s", arrival.RouteCode)
		}
		seenCodes[arrival.Code] = struct{}{}
		seenRoutes[arrival.RouteCode] = struct{}{}
		blocked += int64(arrival.BlockedAttempts)
		switch arrival.Status {
		case cityOpenWorldMobilityArrivalStatusPending:
			if arrival.LandingLocation != nil || arrival.LandingFact != nil || arrival.LandedTick != nil || arrival.FailedTick != nil || arrival.BlockedAttempts != 0 {
				return fmt.Errorf("invalid pending V10 arrival %s", arrival.Code)
			}
		case cityOpenWorldMobilityArrivalStatusBlocked:
			if arrival.LandingLocation != nil || arrival.LandingFact != nil || arrival.LandedTick != nil || arrival.FailedTick != nil || arrival.BlockedAttempts < 1 {
				return fmt.Errorf("invalid blocked V10 arrival %s", arrival.Code)
			}
		case cityOpenWorldMobilityArrivalStatusLanded:
			if arrival.LandingLocation == nil || !cityOpenWorldMobilityArrivalLocationValid(*arrival.LandingLocation, arrival.ActorCode) ||
				arrival.LandingFact == nil || arrival.LandedTick == nil || arrival.FailedTick != nil || *arrival.LandedTick < arrival.CreatedTick {
				return fmt.Errorf("invalid landed V10 arrival %s", arrival.Code)
			}
			landed++
		case cityOpenWorldMobilityArrivalStatusFailed:
			if arrival.LandingLocation != nil || arrival.LandingFact != nil || arrival.LandedTick != nil || arrival.FailedTick == nil || *arrival.FailedTick < arrival.CreatedTick {
				return fmt.Errorf("invalid failed V10 arrival %s", arrival.Code)
			}
			failed++
		default:
			return fmt.Errorf("invalid V10 arrival status %s", arrival.Status)
		}
	}
	if policy.ArrivalCount != int64(len(state.Arrivals)) || policy.LandedCount != landed ||
		policy.FailedCount != failed || policy.BlockedCount != blocked || policy.LandedCount+policy.FailedCount > policy.ArrivalCount {
		return fmt.Errorf("V10 arrival policy counters are inconsistent")
	}
	return nil
}

func validateCityOpenWorldMobilityArrivalPolicy(policy CityOpenWorldMobilityArrivalPolicy) error {
	expectedHash, err := cityOpenWorldMobilityArrivalPolicyHash(
		policy.BridgeContract, policy.LandingContract, policy.MaximumArrivalsPerTick,
		policy.LandingSearchRadius, policy.MaximumBlockedAttempts,
	)
	if err != nil || policy.ProfileID != cityOpenWorldMobilityArrivalProfileID ||
		policy.ProfileVersion != cityOpenWorldMobilityArrivalProfileVersion ||
		policy.ContentHash != expectedHash || policy.BaselineTick < 0 ||
		policy.BridgeContract != cityOpenWorldMobilityArrivalBridgeContract ||
		policy.LandingContract != cityOpenWorldMobilityArrivalLandingContract ||
		policy.MaximumArrivalsPerTick < 1 || policy.MaximumArrivalsPerTick > 100000 ||
		policy.LandingSearchRadius < 0 || policy.LandingSearchRadius > 256 ||
		policy.MaximumBlockedAttempts < 1 || policy.MaximumBlockedAttempts > 64 ||
		policy.ArrivalCount < 0 || policy.LandedCount < 0 || policy.BlockedCount < 0 ||
		policy.FailedCount < 0 || policy.Revision < 1 || !json.Valid(policy.Metadata) {
		return fmt.Errorf("invalid V10 arrival policy")
	}
	return nil
}

func cityOpenWorldMobilityArrivalStaticCheckpointEqual(
	previous, checkpoint *CityOpenWorldMobilityArrivalState,
) bool {
	if previous == nil || checkpoint == nil {
		return previous == checkpoint
	}
	return previous.Policy.ProfileID == checkpoint.Policy.ProfileID &&
		previous.Policy.ProfileVersion == checkpoint.Policy.ProfileVersion &&
		previous.Policy.ContentHash == checkpoint.Policy.ContentHash &&
		previous.Policy.BaselineTick == checkpoint.Policy.BaselineTick &&
		previous.Policy.BridgeContract == checkpoint.Policy.BridgeContract &&
		previous.Policy.LandingContract == checkpoint.Policy.LandingContract &&
		previous.Policy.MaximumArrivalsPerTick == checkpoint.Policy.MaximumArrivalsPerTick &&
		previous.Policy.LandingSearchRadius == checkpoint.Policy.LandingSearchRadius &&
		previous.Policy.MaximumBlockedAttempts == checkpoint.Policy.MaximumBlockedAttempts &&
		string(previous.Policy.Metadata) == string(checkpoint.Policy.Metadata)
}

func (s *CityEconomyService) GetCityOpenWorldMobilityArrivalState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldMobilityArrivalState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V10 arrival world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldArrivalBridge(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldMobilityArrivalState(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	all, err := cityOpenWorldServiceMayReadAll(ctx, s.db, userID, worldID)
	if err != nil || all {
		return state, err
	}
	visible, err := cityOpenWorldServiceVisibleActorCodes(ctx, s.db, userID, worldID)
	if err != nil {
		return nil, err
	}
	filtered := make([]CityOpenWorldMobilityArrival, 0, len(state.Arrivals))
	for _, item := range state.Arrivals {
		if _, found := visible[item.ActorCode]; found {
			filtered = append(filtered, item)
		}
	}
	state.Arrivals = filtered
	return state, nil
}

func sortCityOpenWorldMobilityArrivals(items []CityOpenWorldMobilityArrival) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedTick != items[j].CreatedTick {
			return items[i].CreatedTick < items[j].CreatedTick
		}
		return items[i].Code < items[j].Code
	})
}
