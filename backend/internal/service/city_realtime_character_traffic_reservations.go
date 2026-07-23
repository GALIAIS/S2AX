package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

const (
	cityRealtimeCharacterTrafficReservationSchemaVersion = 1
	cityRealtimeCharacterTrafficBindingVersion           = "city-realtime-character-traffic-binding-v1"
	cityRealtimeCharacterTrafficReservationStateVersion  = "city-realtime-character-traffic-reservation-state-v1"
	cityRealtimeCharacterTrafficReservationChainVersion  = "city-realtime-character-traffic-reservation-chain-v1"
	cityRealtimeCharacterTrafficReservationEventVersion  = "city-realtime-character-traffic-reservation-event-v1"
	cityRealtimeCharacterTrafficReservationRunVersion    = "city-realtime-character-traffic-reservation-run-v1"
	cityRealtimeCharacterTrafficCapacityPolicyID         = "city-realtime-pedestrian-capacity"
	cityRealtimeCharacterTrafficCapacityPolicyVersion    = "1.0.0"
	cityRealtimeCharacterTrafficReservationDuePriority   = 70

	cityRealtimeCharacterTrafficReservationGranted  = "granted"
	cityRealtimeCharacterTrafficReservationDenied   = "denied_capacity"
	cityRealtimeCharacterTrafficReservationConsumed = "consumed"
	cityRealtimeCharacterTrafficReservationReleased = "released"

	cityRealtimeCharacterTrafficReservationReasonCapacityUnavailable = "capacity_unavailable"
	cityRealtimeCharacterTrafficReservationReasonNavigationCancelled = "navigation_cancelled"
	cityRealtimeCharacterTrafficReservationReasonNavigationTerminal  = "navigation_terminal"

	cityRealtimeCharacterTrafficReservationEventGranted  = "traffic_reservation_granted"
	cityRealtimeCharacterTrafficReservationEventDenied   = "traffic_reservation_denied"
	cityRealtimeCharacterTrafficReservationEventConsumed = "traffic_reservation_consumed"
	cityRealtimeCharacterTrafficReservationEventReleased = "traffic_reservation_released"

	cityRealtimeDueEventTypeCharacterTrafficReservation = "system.realtime.character_traffic_reservation"
)

// CityRealtimeCharacterTrafficReservation is intentionally owner-safe. The
// target cell, slot key, other actors, sealed intent and Agent/provider data
// stay private; the shared map remains the authoritative public position view.
type CityRealtimeCharacterTrafficReservation struct {
	ReservationCode   string `json:"reservation_code"`
	NavigationRunCode string `json:"navigation_run_code"`
	PlanRevision      int64  `json:"plan_revision"`
	Status            string `json:"status"`
	ReasonCode        string `json:"reason_code,omitempty"`
	DueWorldTimeUS    int64  `json:"due_world_time_us"`
	Revision          int64  `json:"revision"`
	AcceptedFrame     int64  `json:"accepted_frame_sequence"`
	LastFrame         int64  `json:"last_frame_sequence"`
}

type CityRealtimeCharacterTrafficReservationListInput struct {
	UserID  int64
	WorldID int64
	Limit   int
}

type cityRealtimeCharacterTrafficCapacityPolicyManifest struct {
	SchemaVersion        int              `json:"schema_version"`
	Allocation           string           `json:"allocation"`
	ReservationQuantumUS int64            `json:"reservation_quantum_us"`
	TerrainCapacities    map[string]int64 `json:"terrain_capacities"`
}

type cityRealtimeCharacterTrafficCapacityPolicy struct {
	PolicyID      string
	PolicyVersion string
	Status        string
	Manifest      cityRealtimeCharacterTrafficCapacityPolicyManifest
	PolicyHash    string
}

type cityRealtimeCharacterTrafficReservationBinding struct {
	SchemaVersion      int    `json:"schema_version"`
	AgentBindingHash   string `json:"agent_binding_hash"`
	SpatialContextHash string `json:"spatial_context_hash"`
	CapacityPolicyID   string `json:"capacity_policy_id"`
	CapacityPolicyVer  string `json:"capacity_policy_version"`
	CapacityPolicyHash string `json:"capacity_policy_hash"`
	BindingHash        string `json:"binding_hash"`
}

type cityRealtimeCharacterTrafficReservationRuntime struct {
	Binding cityRealtimeCharacterTrafficReservationBinding
	Policy  cityRealtimeCharacterTrafficCapacityPolicy
}

// One head exists per navigation plan revision. This makes every single-cell
// capacity decision immutable and lets a retry use a new plan revision rather
// than mutating the previously granted or denied slot.
type cityRealtimeCharacterTrafficReservationHead struct {
	ActorCode             string
	NavigationRunCode     string
	PlanRevision          int64
	ReservationCode       string
	From                  cityRealtimeActorSpawnCandidate
	Target                cityRealtimeActorSpawnCandidate
	DueWorldTimeUS        int64
	ReservationRevision   int64
	ReservationStatus     string
	ReasonCode            string
	AcceptedFrameSequence int64
	LastFrameSequence     int64
	EventChainHash        string
	StateHash             string
}

type cityRealtimeCharacterTrafficReservationEvent struct {
	ActorCode              string
	NavigationRunCode      string
	PlanRevision           int64
	ReservationCode        string
	EventSequence          int64
	FrameSequence          int64
	EventType              string
	ReservationStatus      string
	ReasonCode             string
	From                   cityRealtimeActorSpawnCandidate
	Target                 cityRealtimeActorSpawnCandidate
	DueWorldTimeUS         int64
	ActorPositionEventHash string
	PreviousEventHash      string
	EventHash              string
}

type cityRealtimeCharacterTrafficReservationHashState struct {
	SchemaVersion int                                             `json:"schema_version"`
	Binding       *cityRealtimeCharacterTrafficReservationBinding `json:"binding,omitempty"`
	Heads         []cityRealtimeCharacterTrafficReservationHead   `json:"heads"`
}

type cityRealtimeCharacterTrafficReservationDuePayload struct {
	SchemaVersion     int    `json:"schema_version"`
	ActorCode         string `json:"actor_code"`
	NavigationRunCode string `json:"navigation_run_code"`
	PlanRevision      int64  `json:"plan_revision"`
}

func cityRealtimeCharacterTrafficReservationRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	// Traffic is a server-only execution protocol layered on the immutable 1.13
	// navigation policy. It intentionally adds no model action or Observation
	// field, so its compatibility is represented by the world binding rather
	// than an artificial policy bump.
	return cityRealtimeCharacterNavigationPlanRuntimeEnabled(binding)
}

func cityRealtimeCharacterTrafficCapacityPolicyManifestValid(manifest cityRealtimeCharacterTrafficCapacityPolicyManifest) bool {
	if manifest.SchemaVersion != cityRealtimeCharacterTrafficReservationSchemaVersion ||
		manifest.Allocation != "stable_due_event_order" ||
		manifest.ReservationQuantumUS != cityRealtimeTimeQuantumUS ||
		len(manifest.TerrainCapacities) != 5 {
		return false
	}
	for _, terrainID := range []string{
		"terrain.road", "terrain.sidewalk", "terrain.grass", "terrain.ground", "terrain.soil",
	} {
		if manifest.TerrainCapacities[terrainID] != 1 {
			return false
		}
	}
	return true
}

func cityRealtimeCharacterTrafficCapacityPolicyValid(policy cityRealtimeCharacterTrafficCapacityPolicy) bool {
	return policy.PolicyID == cityRealtimeCharacterTrafficCapacityPolicyID &&
		policy.PolicyVersion == cityRealtimeCharacterTrafficCapacityPolicyVersion &&
		policy.Status == "published" && cityRealtimeSHA256Hex(policy.PolicyHash) &&
		cityRealtimeCharacterTrafficCapacityPolicyManifestValid(policy.Manifest)
}

func cityRealtimeCharacterTrafficReservationBindingHash(binding cityRealtimeCharacterTrafficReservationBinding) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterTrafficBindingVersion,
		binding.AgentBindingHash,
		binding.SpatialContextHash,
		binding.CapacityPolicyID,
		binding.CapacityPolicyVer,
		binding.CapacityPolicyHash,
	}, "\x1f")))
}

func cityRealtimeCharacterTrafficReservationBindingValid(binding cityRealtimeCharacterTrafficReservationBinding) bool {
	return binding.SchemaVersion == cityRealtimeCharacterTrafficReservationSchemaVersion &&
		cityRealtimeSHA256Hex(binding.AgentBindingHash) && cityRealtimeSHA256Hex(binding.SpatialContextHash) &&
		binding.CapacityPolicyID == cityRealtimeCharacterTrafficCapacityPolicyID &&
		binding.CapacityPolicyVer == cityRealtimeCharacterTrafficCapacityPolicyVersion &&
		cityRealtimeSHA256Hex(binding.CapacityPolicyHash) && cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeCharacterTrafficReservationBindingHash(binding)
}

func cityRealtimeCharacterTrafficReservationStatusValid(status string) bool {
	switch status {
	case cityRealtimeCharacterTrafficReservationGranted,
		cityRealtimeCharacterTrafficReservationDenied,
		cityRealtimeCharacterTrafficReservationConsumed,
		cityRealtimeCharacterTrafficReservationReleased:
		return true
	default:
		return false
	}
}

func cityRealtimeCharacterTrafficReservationReasonValid(status, reason string) bool {
	switch status {
	case cityRealtimeCharacterTrafficReservationGranted, cityRealtimeCharacterTrafficReservationConsumed:
		return reason == ""
	case cityRealtimeCharacterTrafficReservationDenied:
		return reason == cityRealtimeCharacterTrafficReservationReasonCapacityUnavailable
	case cityRealtimeCharacterTrafficReservationReleased:
		return reason == cityRealtimeCharacterTrafficReservationReasonNavigationCancelled ||
			reason == cityRealtimeCharacterTrafficReservationReasonNavigationTerminal
	default:
		return false
	}
}

func cityRealtimeCharacterTrafficReservationEventTypeValid(eventType string) bool {
	switch eventType {
	case cityRealtimeCharacterTrafficReservationEventGranted,
		cityRealtimeCharacterTrafficReservationEventDenied,
		cityRealtimeCharacterTrafficReservationEventConsumed,
		cityRealtimeCharacterTrafficReservationEventReleased:
		return true
	default:
		return false
	}
}

func cityRealtimeCharacterTrafficReservationRunCode(
	actorCode, navigationRunCode string,
	planRevision, dueWorldTimeUS int64,
) (string, error) {
	if !cityRealtimePlayerActorCodeValid(actorCode) || !cityRealtimeAgentIdentifierValid(navigationRunCode, 96) ||
		planRevision <= 0 || dueWorldTimeUS < 0 || dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return "", ErrCityInvalidInput
	}
	code := "traffic.reservation." + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterTrafficReservationRunVersion,
		actorCode,
		navigationRunCode,
		strconv.FormatInt(planRevision, 10),
		strconv.FormatInt(dueWorldTimeUS, 10),
	}, "\x1f")))
	if !cityRealtimeAgentIdentifierValid(code, 96) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_code"})
	}
	return code, nil
}

func cityRealtimeCharacterTrafficReservationHeadStaticFieldsValid(head cityRealtimeCharacterTrafficReservationHead) bool {
	return cityRealtimePlayerActorCodeValid(head.ActorCode) &&
		cityRealtimeAgentIdentifierValid(head.NavigationRunCode, 96) &&
		strings.HasPrefix(head.NavigationRunCode, "navigation.run.") &&
		head.PlanRevision > 0 && cityRealtimeAgentIdentifierValid(head.ReservationCode, 96) &&
		strings.HasPrefix(head.ReservationCode, "traffic.reservation.") &&
		head.From.Z == cityspatial.SurfaceZ && head.Target.Z == cityspatial.SurfaceZ &&
		cityRealtimeCharacterAdjacentStep(cityRealtimeActorState{X: head.From.X, Y: head.From.Y, Z: head.From.Z}, head.Target) &&
		head.DueWorldTimeUS >= 0 && head.DueWorldTimeUS%cityRealtimeTimeQuantumUS == 0 &&
		head.AcceptedFrameSequence > 0
}

func cityRealtimeCharacterTrafficReservationStateHashUnchecked(head cityRealtimeCharacterTrafficReservationHead) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterTrafficReservationStateVersion,
		head.ActorCode,
		head.NavigationRunCode,
		strconv.FormatInt(head.PlanRevision, 10),
		head.ReservationCode,
		strconv.FormatInt(head.From.X, 10),
		strconv.FormatInt(head.From.Y, 10),
		strconv.FormatInt(int64(head.From.Z), 10),
		strconv.FormatInt(head.Target.X, 10),
		strconv.FormatInt(head.Target.Y, 10),
		strconv.FormatInt(int64(head.Target.Z), 10),
		strconv.FormatInt(head.DueWorldTimeUS, 10),
		strconv.FormatInt(head.ReservationRevision, 10),
		head.ReservationStatus,
		head.ReasonCode,
		strconv.FormatInt(head.AcceptedFrameSequence, 10),
		strconv.FormatInt(head.LastFrameSequence, 10),
		head.EventChainHash,
	}, "\x1f")))
}

func cityRealtimeCharacterTrafficReservationHeadValid(head cityRealtimeCharacterTrafficReservationHead) bool {
	return cityRealtimeCharacterTrafficReservationHeadStaticFieldsValid(head) &&
		head.ReservationRevision > 0 && cityRealtimeCharacterTrafficReservationStatusValid(head.ReservationStatus) &&
		cityRealtimeCharacterTrafficReservationReasonValid(head.ReservationStatus, head.ReasonCode) &&
		head.LastFrameSequence >= head.AcceptedFrameSequence && cityRealtimeSHA256Hex(head.EventChainHash) &&
		cityRealtimeSHA256Hex(head.StateHash) && head.StateHash == cityRealtimeCharacterTrafficReservationStateHashUnchecked(head)
}

func cityRealtimeCharacterTrafficReservationChainGenesisHash(head cityRealtimeCharacterTrafficReservationHead) (string, error) {
	if !cityRealtimeCharacterTrafficReservationHeadStaticFieldsValid(head) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterTrafficReservationChainVersion,
		head.ActorCode,
		head.NavigationRunCode,
		strconv.FormatInt(head.PlanRevision, 10),
		head.ReservationCode,
		strconv.FormatInt(head.From.X, 10),
		strconv.FormatInt(head.From.Y, 10),
		strconv.FormatInt(int64(head.From.Z), 10),
		strconv.FormatInt(head.Target.X, 10),
		strconv.FormatInt(head.Target.Y, 10),
		strconv.FormatInt(int64(head.Target.Z), 10),
		strconv.FormatInt(head.DueWorldTimeUS, 10),
		strconv.FormatInt(head.AcceptedFrameSequence, 10),
	}, "\x1f"))), nil
}

func cityRealtimeCharacterTrafficReservationEventHash(event cityRealtimeCharacterTrafficReservationEvent) (string, error) {
	if !cityRealtimeCharacterTrafficReservationEventShapeValid(event) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterTrafficReservationEventVersion,
		event.ActorCode,
		event.NavigationRunCode,
		strconv.FormatInt(event.PlanRevision, 10),
		event.ReservationCode,
		strconv.FormatInt(event.EventSequence, 10),
		strconv.FormatInt(event.FrameSequence, 10),
		event.EventType,
		event.ReservationStatus,
		event.ReasonCode,
		strconv.FormatInt(event.From.X, 10),
		strconv.FormatInt(event.From.Y, 10),
		strconv.FormatInt(int64(event.From.Z), 10),
		strconv.FormatInt(event.Target.X, 10),
		strconv.FormatInt(event.Target.Y, 10),
		strconv.FormatInt(int64(event.Target.Z), 10),
		strconv.FormatInt(event.DueWorldTimeUS, 10),
		event.ActorPositionEventHash,
		event.PreviousEventHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterTrafficReservationEventShapeValid(event cityRealtimeCharacterTrafficReservationEvent) bool {
	return cityRealtimePlayerActorCodeValid(event.ActorCode) &&
		cityRealtimeAgentIdentifierValid(event.NavigationRunCode, 96) && event.PlanRevision > 0 &&
		cityRealtimeAgentIdentifierValid(event.ReservationCode, 96) && event.EventSequence > 0 &&
		event.FrameSequence > 0 && cityRealtimeCharacterTrafficReservationEventTypeValid(event.EventType) &&
		cityRealtimeCharacterTrafficReservationStatusValid(event.ReservationStatus) &&
		cityRealtimeCharacterTrafficReservationReasonValid(event.ReservationStatus, event.ReasonCode) &&
		event.From.Z == cityspatial.SurfaceZ && event.Target.Z == cityspatial.SurfaceZ &&
		cityRealtimeCharacterAdjacentStep(cityRealtimeActorState{X: event.From.X, Y: event.From.Y, Z: event.From.Z}, event.Target) &&
		event.DueWorldTimeUS >= 0 && event.DueWorldTimeUS%cityRealtimeTimeQuantumUS == 0 &&
		(event.ActorPositionEventHash == "" || cityRealtimeSHA256Hex(event.ActorPositionEventHash)) &&
		cityRealtimeSHA256Hex(event.PreviousEventHash)
}

func cityRealtimeCharacterTrafficReservationEventValid(event cityRealtimeCharacterTrafficReservationEvent) bool {
	return cityRealtimeCharacterTrafficReservationEventShapeValid(event) && cityRealtimeSHA256Hex(event.EventHash)
}

func cityRealtimeCharacterTrafficReservationDueDedupKey(head cityRealtimeCharacterTrafficReservationHead) (string, error) {
	if !cityRealtimePlayerActorCodeValid(head.ActorCode) || !cityRealtimeAgentIdentifierValid(head.NavigationRunCode, 96) ||
		head.PlanRevision <= 0 || head.DueWorldTimeUS < 0 || head.DueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return "", ErrCityInvalidInput
	}
	key := "traffic.reservation.request." + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		"city-realtime-character-traffic-reservation-dedup-v1",
		head.ActorCode,
		head.NavigationRunCode,
		strconv.FormatInt(head.PlanRevision, 10),
		strconv.FormatInt(head.DueWorldTimeUS, 10),
	}, "\x1f")))
	if !cityRealtimeDueEventIdentifierValid(key, 160) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_dedup"})
	}
	return key, nil
}

func cityRealtimeCharacterTrafficReservationAggregateKey(
	actorCode, navigationRunCode string,
) (string, error) {
	if !cityRealtimePlayerActorCodeValid(actorCode) || !cityRealtimeAgentIdentifierValid(navigationRunCode, 96) {
		return "", ErrCityInvalidInput
	}
	key := "traffic.reservation.aggregate." + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		"city-realtime-character-traffic-reservation-aggregate-v1", actorCode, navigationRunCode,
	}, "\x1f")))
	if !cityRealtimeDueEventIdentifierValid(key, 160) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_aggregate"})
	}
	return key, nil
}

func loadCityRealtimeCharacterTrafficCapacityPolicy(
	ctx context.Context,
	queryer citySQLQueryer,
) (cityRealtimeCharacterTrafficCapacityPolicy, error) {
	policy := cityRealtimeCharacterTrafficCapacityPolicy{}
	var rawManifest []byte
	err := queryer.QueryRowContext(ctx, `
SELECT policy_id, policy_version, status, manifest, policy_hash
FROM city_realtime_character_traffic_capacity_policies
WHERE policy_id = $1 AND policy_version = $2`,
		cityRealtimeCharacterTrafficCapacityPolicyID, cityRealtimeCharacterTrafficCapacityPolicyVersion,
	).Scan(&policy.PolicyID, &policy.PolicyVersion, &policy.Status, &rawManifest, &policy.PolicyHash)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterTrafficCapacityPolicy{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_capacity_policy"})
	}
	if err != nil {
		return cityRealtimeCharacterTrafficCapacityPolicy{}, fmt.Errorf("load realtime character traffic capacity policy: %w", err)
	}
	if err = json.Unmarshal(rawManifest, &policy.Manifest); err != nil || !cityRealtimeCharacterTrafficCapacityPolicyValid(policy) {
		return cityRealtimeCharacterTrafficCapacityPolicy{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_capacity_policy"})
	}
	return policy, nil
}

func cityRealtimeCharacterTrafficReservationBindingForWorld(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	agentBinding cityRealtimeAgentPolicyBinding,
) (cityRealtimeCharacterTrafficReservationBinding, cityRealtimeCharacterTrafficCapacityPolicy, error) {
	if worldID <= 0 || !cityRealtimeCharacterTrafficReservationRuntimeEnabled(agentBinding) {
		return cityRealtimeCharacterTrafficReservationBinding{}, cityRealtimeCharacterTrafficCapacityPolicy{}, ErrCityInvalidInput
	}
	policy, err := loadCityRealtimeCharacterTrafficCapacityPolicy(ctx, queryer)
	if err != nil {
		return cityRealtimeCharacterTrafficReservationBinding{}, cityRealtimeCharacterTrafficCapacityPolicy{}, err
	}
	binding := cityRealtimeCharacterTrafficReservationBinding{
		SchemaVersion:      cityRealtimeCharacterTrafficReservationSchemaVersion,
		AgentBindingHash:   agentBinding.BindingHash,
		CapacityPolicyID:   policy.PolicyID,
		CapacityPolicyVer:  policy.PolicyVersion,
		CapacityPolicyHash: policy.PolicyHash,
	}
	var spatialWorldID int64
	err = queryer.QueryRowContext(ctx, `
SELECT spatial.world_id, spatial.context_hash
FROM city_realtime_spatial_bindings spatial
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = spatial.world_id
WHERE spatial.world_id = $1 AND agent.binding_hash = $2`, worldID, agentBinding.BindingHash,
	).Scan(&spatialWorldID, &binding.SpatialContextHash)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterTrafficReservationBinding{}, cityRealtimeCharacterTrafficCapacityPolicy{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_spatial_binding"})
	}
	if err != nil {
		return cityRealtimeCharacterTrafficReservationBinding{}, cityRealtimeCharacterTrafficCapacityPolicy{}, fmt.Errorf("load realtime character traffic spatial binding: %w", err)
	}
	if spatialWorldID != worldID {
		return cityRealtimeCharacterTrafficReservationBinding{}, cityRealtimeCharacterTrafficCapacityPolicy{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_spatial_world"})
	}
	binding.BindingHash = cityRealtimeCharacterTrafficReservationBindingHash(binding)
	if !cityRealtimeCharacterTrafficReservationBindingValid(binding) {
		return cityRealtimeCharacterTrafficReservationBinding{}, cityRealtimeCharacterTrafficCapacityPolicy{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_binding"})
	}
	return binding, policy, nil
}

func initializeCityRealtimeCharacterTrafficReservationFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	simulationVersion string,
) error {
	if tx == nil || worldID <= 0 || !cityEngineSupportsRealtimeStaticWorldgen(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if agentState == nil || agentState.Binding == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_policy"})
	}
	if !cityRealtimeCharacterTrafficReservationRuntimeEnabled(*agentState.Binding) {
		return nil
	}
	binding, _, err := cityRealtimeCharacterTrafficReservationBindingForWorld(ctx, tx, worldID, *agentState.Binding)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_character_traffic_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("activate realtime character traffic initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_traffic_reservation_world_bindings
    (world_id, schema_version, agent_binding_hash, spatial_context_hash,
     capacity_policy_id, capacity_policy_version, capacity_policy_hash, binding_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '{}'::jsonb)`,
		worldID, binding.SchemaVersion, binding.AgentBindingHash, binding.SpatialContextHash,
		binding.CapacityPolicyID, binding.CapacityPolicyVer, binding.CapacityPolicyHash, binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime character traffic reservation binding: %w", err)
	}
	return nil
}

func enableCityRealtimeCharacterTrafficReservationMutationGate(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence, dueWorldTimeUS int64,
) error {
	if tx == nil || worldID <= 0 || frameSequence <= 0 || dueWorldTimeUS < 0 ||
		dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return ErrCityInvalidInput
	}
	for _, setting := range []struct {
		name  string
		value int64
	}{
		{name: "sub2api.city_realtime_character_traffic_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_traffic_frame_sequence", value: frameSequence},
		{name: "sub2api.city_realtime_character_traffic_due_world_time_us", value: dueWorldTimeUS},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character traffic gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func loadCityRealtimeCharacterTrafficReservationRuntime(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterTrafficReservationRuntime, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterTrafficReservationBinding{}
	policy := cityRealtimeCharacterTrafficCapacityPolicy{}
	var rawManifest []byte
	var policyID, policyVersion, agentBindingHash, spatialContextHash string
	err := queryer.QueryRowContext(ctx, `
SELECT traffic.schema_version, traffic.agent_binding_hash, traffic.spatial_context_hash,
       traffic.capacity_policy_id, traffic.capacity_policy_version, traffic.capacity_policy_hash,
       traffic.binding_hash, agent.policy_id, agent.policy_version, agent.binding_hash,
       spatial.context_hash, policy.policy_id, policy.policy_version, policy.status,
       policy.manifest, policy.policy_hash
FROM city_realtime_character_traffic_reservation_world_bindings traffic
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = traffic.world_id
JOIN city_realtime_spatial_bindings spatial ON spatial.world_id = traffic.world_id
JOIN city_realtime_character_traffic_capacity_policies policy
  ON policy.policy_id = traffic.capacity_policy_id
 AND policy.policy_version = traffic.capacity_policy_version
WHERE traffic.world_id = $1`, worldID,
	).Scan(&binding.SchemaVersion, &binding.AgentBindingHash, &binding.SpatialContextHash,
		&binding.CapacityPolicyID, &binding.CapacityPolicyVer, &binding.CapacityPolicyHash, &binding.BindingHash,
		&policyID, &policyVersion, &agentBindingHash, &spatialContextHash,
		&policy.PolicyID, &policy.PolicyVersion, &policy.Status, &rawManifest, &policy.PolicyHash)
	if errors.Is(err, sql.ErrNoRows) {
		var headCount int
		if countErr := queryer.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM city_realtime_character_traffic_reservation_heads WHERE world_id = $1`, worldID,
		).Scan(&headCount); countErr != nil {
			return nil, fmt.Errorf("check historical realtime character traffic state: %w", countErr)
		}
		if headCount != 0 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_binding"})
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character traffic reservation binding: %w", err)
	}
	if err = json.Unmarshal(rawManifest, &policy.Manifest); err != nil ||
		policyID != cityRealtimeAgentCorePolicyID || policyVersion != cityRealtimeAgentCorePolicyVersionNavigationPlan ||
		binding.AgentBindingHash != agentBindingHash || binding.SpatialContextHash != spatialContextHash ||
		!cityRealtimeCharacterTrafficReservationBindingValid(binding) || !cityRealtimeCharacterTrafficCapacityPolicyValid(policy) ||
		binding.CapacityPolicyID != policy.PolicyID || binding.CapacityPolicyVer != policy.PolicyVersion ||
		binding.CapacityPolicyHash != policy.PolicyHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_binding"})
	}
	return &cityRealtimeCharacterTrafficReservationRuntime{Binding: binding, Policy: policy}, nil
}

func scanCityRealtimeCharacterTrafficReservationHead(scanner cityScannable) (cityRealtimeCharacterTrafficReservationHead, error) {
	head := cityRealtimeCharacterTrafficReservationHead{}
	err := scanner.Scan(
		&head.ActorCode, &head.NavigationRunCode, &head.PlanRevision, &head.ReservationCode,
		&head.From.X, &head.From.Y, &head.From.Z, &head.Target.X, &head.Target.Y, &head.Target.Z,
		&head.DueWorldTimeUS, &head.ReservationRevision, &head.ReservationStatus, &head.ReasonCode,
		&head.AcceptedFrameSequence, &head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
	)
	if err != nil {
		return cityRealtimeCharacterTrafficReservationHead{}, err
	}
	if !cityRealtimeCharacterTrafficReservationHeadValid(head) {
		return cityRealtimeCharacterTrafficReservationHead{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_head"})
	}
	return head, nil
}

func loadCityRealtimeCharacterTrafficReservationHead(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode, navigationRunCode string,
	planRevision int64,
	forUpdate bool,
) (cityRealtimeCharacterTrafficReservationHead, bool, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) ||
		!cityRealtimeAgentIdentifierValid(navigationRunCode, 96) || planRevision <= 0 {
		return cityRealtimeCharacterTrafficReservationHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT actor_code, navigation_run_code, plan_revision, reservation_code,
       from_x, from_y, from_z, target_x, target_y, target_z,
       due_world_time_us, reservation_revision, reservation_status, reason_code,
       accepted_frame_sequence, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_traffic_reservation_heads
WHERE world_id = $1 AND actor_code = $2 AND navigation_run_code = $3 AND plan_revision = $4`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head, err := scanCityRealtimeCharacterTrafficReservationHead(queryer.QueryRowContext(ctx, query, worldID, actorCode, navigationRunCode, planRevision))
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterTrafficReservationHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterTrafficReservationHead{}, false, fmt.Errorf("load realtime character traffic reservation head: %w", err)
	}
	return head, true, nil
}

func cityRealtimeCharacterTrafficReservationProjection(head cityRealtimeCharacterTrafficReservationHead) CityRealtimeCharacterTrafficReservation {
	return CityRealtimeCharacterTrafficReservation{
		ReservationCode:   head.ReservationCode,
		NavigationRunCode: head.NavigationRunCode,
		PlanRevision:      head.PlanRevision,
		Status:            head.ReservationStatus,
		ReasonCode:        head.ReasonCode,
		DueWorldTimeUS:    head.DueWorldTimeUS,
		Revision:          head.ReservationRevision,
		AcceptedFrame:     head.AcceptedFrameSequence,
		LastFrame:         head.LastFrameSequence,
	}
}

func cityRealtimeCharacterTrafficReservationNew(
	actorCode, navigationRunCode string,
	planRevision int64,
	from, target cityRealtimeActorSpawnCandidate,
	dueWorldTimeUS, frameSequence int64,
	status, reasonCode string,
) (cityRealtimeCharacterTrafficReservationHead, cityRealtimeCharacterTrafficReservationEvent, error) {
	if !cityRealtimePlayerActorCodeValid(actorCode) || !cityRealtimeAgentIdentifierValid(navigationRunCode, 96) ||
		planRevision <= 0 || from.Z != cityspatial.SurfaceZ || target.Z != cityspatial.SurfaceZ ||
		!cityRealtimeCharacterAdjacentStep(cityRealtimeActorState{X: from.X, Y: from.Y, Z: from.Z}, target) ||
		dueWorldTimeUS < 0 || dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 || frameSequence <= 0 ||
		(status != cityRealtimeCharacterTrafficReservationGranted && status != cityRealtimeCharacterTrafficReservationDenied) ||
		!cityRealtimeCharacterTrafficReservationReasonValid(status, reasonCode) {
		return cityRealtimeCharacterTrafficReservationHead{}, cityRealtimeCharacterTrafficReservationEvent{}, ErrCityInvalidInput
	}
	reservationCode, err := cityRealtimeCharacterTrafficReservationRunCode(actorCode, navigationRunCode, planRevision, dueWorldTimeUS)
	if err != nil {
		return cityRealtimeCharacterTrafficReservationHead{}, cityRealtimeCharacterTrafficReservationEvent{}, err
	}
	head := cityRealtimeCharacterTrafficReservationHead{
		ActorCode:             actorCode,
		NavigationRunCode:     navigationRunCode,
		PlanRevision:          planRevision,
		ReservationCode:       reservationCode,
		From:                  from,
		Target:                target,
		DueWorldTimeUS:        dueWorldTimeUS,
		ReservationRevision:   1,
		ReservationStatus:     status,
		ReasonCode:            reasonCode,
		AcceptedFrameSequence: frameSequence,
		LastFrameSequence:     frameSequence,
	}
	genesisHash, err := cityRealtimeCharacterTrafficReservationChainGenesisHash(head)
	if err != nil {
		return cityRealtimeCharacterTrafficReservationHead{}, cityRealtimeCharacterTrafficReservationEvent{}, err
	}
	eventType := cityRealtimeCharacterTrafficReservationEventGranted
	if status == cityRealtimeCharacterTrafficReservationDenied {
		eventType = cityRealtimeCharacterTrafficReservationEventDenied
	}
	event := cityRealtimeCharacterTrafficReservationEvent{
		ActorCode:         head.ActorCode,
		NavigationRunCode: head.NavigationRunCode,
		PlanRevision:      head.PlanRevision,
		ReservationCode:   head.ReservationCode,
		EventSequence:     1,
		FrameSequence:     frameSequence,
		EventType:         eventType,
		ReservationStatus: status,
		ReasonCode:        reasonCode,
		From:              from,
		Target:            target,
		DueWorldTimeUS:    dueWorldTimeUS,
		PreviousEventHash: genesisHash,
	}
	if event.EventHash, err = cityRealtimeCharacterTrafficReservationEventHash(event); err != nil {
		return cityRealtimeCharacterTrafficReservationHead{}, cityRealtimeCharacterTrafficReservationEvent{}, err
	}
	head.EventChainHash = event.EventHash
	head.StateHash = cityRealtimeCharacterTrafficReservationStateHashUnchecked(head)
	if !cityRealtimeCharacterTrafficReservationHeadValid(head) || !cityRealtimeCharacterTrafficReservationEventValid(event) {
		return cityRealtimeCharacterTrafficReservationHead{}, cityRealtimeCharacterTrafficReservationEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_new"})
	}
	return head, event, nil
}

func cityRealtimeCharacterTrafficReservationAdvance(
	head cityRealtimeCharacterTrafficReservationHead,
	frameSequence int64,
	status, reasonCode, positionEventHash string,
) (cityRealtimeCharacterTrafficReservationHead, cityRealtimeCharacterTrafficReservationEvent, error) {
	if !cityRealtimeCharacterTrafficReservationHeadValid(head) || head.ReservationStatus != cityRealtimeCharacterTrafficReservationGranted ||
		frameSequence < head.LastFrameSequence ||
		(frameSequence == head.LastFrameSequence &&
			(head.ReservationRevision != 1 || head.AcceptedFrameSequence != frameSequence ||
				status != cityRealtimeCharacterTrafficReservationConsumed)) ||
		(status != cityRealtimeCharacterTrafficReservationConsumed && status != cityRealtimeCharacterTrafficReservationReleased) ||
		!cityRealtimeCharacterTrafficReservationReasonValid(status, reasonCode) ||
		((status == cityRealtimeCharacterTrafficReservationConsumed) != cityRealtimeSHA256Hex(positionEventHash)) ||
		(status == cityRealtimeCharacterTrafficReservationReleased && positionEventHash != "") {
		return cityRealtimeCharacterTrafficReservationHead{}, cityRealtimeCharacterTrafficReservationEvent{}, ErrCityInvalidInput
	}
	next := head
	next.ReservationRevision++
	next.ReservationStatus = status
	next.ReasonCode = reasonCode
	next.LastFrameSequence = frameSequence
	eventType := cityRealtimeCharacterTrafficReservationEventConsumed
	if status == cityRealtimeCharacterTrafficReservationReleased {
		eventType = cityRealtimeCharacterTrafficReservationEventReleased
	}
	event := cityRealtimeCharacterTrafficReservationEvent{
		ActorCode:              next.ActorCode,
		NavigationRunCode:      next.NavigationRunCode,
		PlanRevision:           next.PlanRevision,
		ReservationCode:        next.ReservationCode,
		EventSequence:          next.ReservationRevision,
		FrameSequence:          frameSequence,
		EventType:              eventType,
		ReservationStatus:      status,
		ReasonCode:             reasonCode,
		From:                   next.From,
		Target:                 next.Target,
		DueWorldTimeUS:         next.DueWorldTimeUS,
		ActorPositionEventHash: positionEventHash,
		PreviousEventHash:      head.EventChainHash,
	}
	var err error
	if event.EventHash, err = cityRealtimeCharacterTrafficReservationEventHash(event); err != nil {
		return cityRealtimeCharacterTrafficReservationHead{}, cityRealtimeCharacterTrafficReservationEvent{}, err
	}
	next.EventChainHash = event.EventHash
	next.StateHash = cityRealtimeCharacterTrafficReservationStateHashUnchecked(next)
	if !cityRealtimeCharacterTrafficReservationHeadValid(next) || !cityRealtimeCharacterTrafficReservationEventValid(event) {
		return cityRealtimeCharacterTrafficReservationHead{}, cityRealtimeCharacterTrafficReservationEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_transition"})
	}
	return next, event, nil
}

func insertCityRealtimeCharacterTrafficReservationHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	head cityRealtimeCharacterTrafficReservationHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterTrafficReservationHeadValid(head) ||
		head.ReservationRevision != 1 ||
		(head.ReservationStatus != cityRealtimeCharacterTrafficReservationGranted && head.ReservationStatus != cityRealtimeCharacterTrafficReservationDenied) {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_traffic_reservation_heads
    (world_id, actor_code, navigation_run_code, plan_revision, reservation_code,
     from_x, from_y, from_z, target_x, target_y, target_z, due_world_time_us,
     reservation_revision, reservation_status, reason_code,
     accepted_frame_sequence, last_frame_sequence, event_chain_hash, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17, $18, $19, '{}'::jsonb)`,
		worldID, head.ActorCode, head.NavigationRunCode, head.PlanRevision, head.ReservationCode,
		head.From.X, head.From.Y, head.From.Z, head.Target.X, head.Target.Y, head.Target.Z, head.DueWorldTimeUS,
		head.ReservationRevision, head.ReservationStatus, head.ReasonCode,
		head.AcceptedFrameSequence, head.LastFrameSequence, head.EventChainHash, head.StateHash,
	); err != nil {
		return fmt.Errorf("insert realtime character traffic reservation head: %w", err)
	}
	return nil
}

func updateCityRealtimeCharacterTrafficReservationHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previous, next cityRealtimeCharacterTrafficReservationHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterTrafficReservationHeadValid(previous) ||
		!cityRealtimeCharacterTrafficReservationHeadValid(next) || previous.ActorCode != next.ActorCode ||
		previous.NavigationRunCode != next.NavigationRunCode || previous.PlanRevision != next.PlanRevision ||
		previous.ReservationCode != next.ReservationCode || next.ReservationRevision != previous.ReservationRevision+1 {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_traffic_reservation_heads
SET reservation_revision = $5, reservation_status = $6, reason_code = $7,
    last_frame_sequence = $8, event_chain_hash = $9, state_hash = $10, updated_at = NOW()
WHERE world_id = $1 AND actor_code = $2 AND navigation_run_code = $3 AND plan_revision = $4
  AND reservation_revision = $11 AND reservation_status = $12 AND reason_code = $13
  AND last_frame_sequence = $14 AND event_chain_hash = $15 AND state_hash = $16`,
		worldID, next.ActorCode, next.NavigationRunCode, next.PlanRevision, next.ReservationRevision,
		next.ReservationStatus, next.ReasonCode, next.LastFrameSequence, next.EventChainHash, next.StateHash,
		previous.ReservationRevision, previous.ReservationStatus, previous.ReasonCode,
		previous.LastFrameSequence, previous.EventChainHash, previous.StateHash,
	)
	if err != nil {
		return fmt.Errorf("advance realtime character traffic reservation head: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime character traffic reservation update: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_revision"})
	}
	return nil
}

func insertCityRealtimeCharacterTrafficReservationEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	event cityRealtimeCharacterTrafficReservationEvent,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterTrafficReservationEventValid(event) {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_traffic_reservation_events
    (world_id, actor_code, navigation_run_code, plan_revision, reservation_code,
     event_sequence, frame_sequence, event_type, reservation_status, reason_code,
     from_x, from_y, from_z, target_x, target_y, target_z, due_world_time_us,
     actor_position_event_hash, previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
        $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, '{}'::jsonb)`,
		worldID, event.ActorCode, event.NavigationRunCode, event.PlanRevision, event.ReservationCode,
		event.EventSequence, event.FrameSequence, event.EventType, event.ReservationStatus, event.ReasonCode,
		event.From.X, event.From.Y, event.From.Z, event.Target.X, event.Target.Y, event.Target.Z, event.DueWorldTimeUS,
		event.ActorPositionEventHash, event.PreviousEventHash, event.EventHash,
	); err != nil {
		return fmt.Errorf("append realtime character traffic reservation event: %w", err)
	}
	return nil
}

func scheduleCityRealtimeCharacterTrafficReservationDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, createdFrameSequence int64,
	head cityRealtimeCharacterNavigationPlanHead,
	runtime *cityRealtimeCharacterTrafficReservationRuntime,
) error {
	if tx == nil || worldID <= 0 || createdFrameSequence <= 0 || runtime == nil ||
		!cityRealtimeCharacterTrafficReservationBindingValid(runtime.Binding) ||
		!cityRealtimeCharacterTrafficCapacityPolicyValid(runtime.Policy) ||
		!cityRealtimeCharacterNavigationPlanHeadValid(head) ||
		head.PlanStatus != cityRealtimeCharacterNavigationPlanActive || head.NextDueWorldTimeUS == nil {
		return ErrCityInvalidInput
	}
	prototype := cityRealtimeCharacterTrafficReservationHead{
		ActorCode:             head.ActorCode,
		NavigationRunCode:     head.NavigationRunCode,
		PlanRevision:          head.PlanRevision,
		From:                  cityRealtimeActorSpawnCandidate{Z: cityspatial.SurfaceZ},
		Target:                cityRealtimeActorSpawnCandidate{X: 1, Z: cityspatial.SurfaceZ},
		DueWorldTimeUS:        *head.NextDueWorldTimeUS,
		AcceptedFrameSequence: createdFrameSequence,
	}
	// The target is intentionally not known before the reservation reducer. It
	// is recomputed from the current actor state at the due boundary. The
	// prototype exists only to share the exact deterministic dedup derivation.
	dedupKey, err := cityRealtimeCharacterTrafficReservationDueDedupKey(prototype)
	if err != nil {
		return err
	}
	aggregateKey, err := cityRealtimeCharacterTrafficReservationAggregateKey(head.ActorCode, head.NavigationRunCode)
	if err != nil {
		return err
	}
	payload, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":      cityRealtimeCharacterTrafficReservationSchemaVersion,
		"actor_code":          head.ActorCode,
		"navigation_run_code": head.NavigationRunCode,
		"plan_revision":       head.PlanRevision,
	})
	if err != nil {
		return fmt.Errorf("canonicalize realtime character traffic reservation payload: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_due_events
    (world_id, event_type, schema_version, due_world_time_us, temporal_phase,
     priority, aggregate_type, aggregate_key, dedup_key, source_kind,
     source_reference, payload, payload_hash, expected_version, status,
     created_frame_sequence)
VALUES ($1, $2, 1, $3, 'movement', $4, 'realtime_character_traffic', $5, $6, 'system',
        'realtime_character_traffic_reservation', $7::jsonb, $8, $9, 'pending', $10)`,
		worldID, cityRealtimeDueEventTypeCharacterTrafficReservation, *head.NextDueWorldTimeUS,
		cityRealtimeCharacterTrafficReservationDuePriority, aggregateKey, dedupKey,
		[]byte(payload), payloadHash, head.PlanRevision, createdFrameSequence,
	); err != nil {
		return fmt.Errorf("schedule realtime character traffic reservation: %w", err)
	}
	return nil
}

func decodeCityRealtimeCharacterTrafficReservationDuePayload(
	event cityRealtimeDueEventRecord,
) (cityRealtimeCharacterTrafficReservationDuePayload, bool) {
	payload := cityRealtimeCharacterTrafficReservationDuePayload{}
	if err := decodeStrictCityObject(event.Payload, &payload); err != nil ||
		payload.SchemaVersion != cityRealtimeCharacterTrafficReservationSchemaVersion ||
		!cityRealtimePlayerActorCodeValid(payload.ActorCode) || !cityRealtimeAgentIdentifierValid(payload.NavigationRunCode, 96) ||
		payload.PlanRevision <= 0 {
		return cityRealtimeCharacterTrafficReservationDuePayload{}, false
	}
	_, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":      payload.SchemaVersion,
		"actor_code":          payload.ActorCode,
		"navigation_run_code": payload.NavigationRunCode,
		"plan_revision":       payload.PlanRevision,
	})
	if err != nil || payloadHash != event.PayloadHash {
		return cityRealtimeCharacterTrafficReservationDuePayload{}, false
	}
	return payload, true
}

// cityRealtimeCharacterTrafficReservationTarget recomputes only the immediate
// movement candidate. It deliberately does not persist or return the rest of
// the path; navigation will recompute it again before it consumes a grant.
func cityRealtimeCharacterTrafficReservationTarget(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	head cityRealtimeCharacterNavigationPlanHead,
	record cityRealtimeCharacterRecord,
) (cityRealtimeActorSpawnCandidate, bool, error) {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterNavigationPlanHeadValid(head) ||
		head.PlanStatus != cityRealtimeCharacterNavigationPlanActive || !cityRealtimeActorStateValid(record.state) ||
		record.state.ActorCode != head.ActorCode || record.state.Z != cityspatial.SurfaceZ {
		return cityRealtimeActorSpawnCandidate{}, false, ErrCityInvalidInput
	}
	portal, portalFound, err := loadCityRealtimeCharacterPortal(ctx, tx, worldID, head.DestinationPortalCode)
	if err != nil {
		return cityRealtimeActorSpawnCandidate{}, false, err
	}
	if !portalFound || portal.PortalType != "entrance" || portal.From != head.Destination || portal.From.Z != cityspatial.SurfaceZ {
		return cityRealtimeActorSpawnCandidate{}, false, nil
	}
	origin := cityRealtimeActorSpawnCandidate{X: record.state.X, Y: record.state.Y, Z: record.state.Z}
	occupied, err := loadCityRealtimeCharacterNavigationOccupiedSurfaceCells(
		ctx, tx, worldID, head.ActorCode, origin, cityRealtimeCharacterNavigationPlanMaximumSteps,
	)
	if err != nil {
		return cityRealtimeActorSpawnCandidate{}, false, err
	}
	cache := newCityRealtimeNavigationSurfaceCache(ctx, tx, worldID)
	path, pathFound, err := cityRealtimeCharacterNavigationFindPath(
		cache, origin, head.Destination, occupied, cityRealtimeCharacterNavigationPlanMaximumSteps-head.StepsCompleted,
	)
	if err != nil {
		return cityRealtimeActorSpawnCandidate{}, false, err
	}
	if !pathFound || len(path) <= 1 {
		return cityRealtimeActorSpawnCandidate{}, false, nil
	}
	target := path[1]
	motionState, traversable, err := cityRealtimeCharacterWalkMotionState(ctx, tx, worldID, record.state, target)
	if err != nil {
		return cityRealtimeActorSpawnCandidate{}, false, err
	}
	if !traversable || motionState != "walking" {
		return cityRealtimeActorSpawnCandidate{}, false, nil
	}
	isOccupied, err := cityRealtimeActorPositionOccupied(ctx, tx, worldID, head.ActorCode, target)
	if err != nil {
		return cityRealtimeActorSpawnCandidate{}, false, err
	}
	if isOccupied {
		return cityRealtimeActorSpawnCandidate{}, false, nil
	}
	return target, true, nil
}

func cityRealtimeCharacterTrafficTerrainIDAt(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	position cityRealtimeActorSpawnCandidate,
) (string, bool, error) {
	if worldID <= 0 || position.Z != cityspatial.SurfaceZ {
		return "", false, ErrCityInvalidInput
	}
	address, err := cityspatial.SplitWorldCoordinate(cityspatial.WorldCoordinate{
		X: position.X, Y: position.Y, Z: position.Z,
	}, cityspatial.DefaultChunkSize)
	if err != nil {
		return "", false, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "traffic_target"}).WithCause(err)
	}
	var rawPayload []byte
	err = queryer.QueryRowContext(ctx, `
SELECT payload
FROM city_realtime_spatial_chunks
WHERE world_id = $1 AND chunk_x = $2 AND chunk_y = $3 AND z = $4`,
		worldID, address.Chunk.X, address.Chunk.Y, address.Chunk.Z,
	).Scan(&rawPayload)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("load realtime character traffic chunk: %w", err)
	}
	payload := cityspatial.OpenWorldChunkPayload{}
	if err = json.Unmarshal(rawPayload, &payload); err != nil {
		return "", false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_payload"}).WithCause(err)
	}
	if err = cityspatial.ValidateOpenWorldChunkPayload(payload); err != nil {
		return "", false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_payload"}).WithCause(err)
	}
	cellIndex := int(address.Local.Y)*payload.Width + int(address.Local.X)
	if cellIndex < 0 || cellIndex >= payload.Width*payload.Height {
		return "", false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_cell"})
	}
	for _, layer := range payload.Layers {
		if int(layer.X) == int(address.Local.X) && int(layer.Y) == int(address.Local.Y) && layer.Kind == cityspatial.RuleKindStructure {
			return "", false, nil
		}
	}
	terrainID, found := cityRealtimeTerrainDefinitionAt(payload.TerrainRuns, cellIndex)
	return terrainID, found, nil
}

func cityRealtimeCharacterTrafficCellCapacity(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	runtime *cityRealtimeCharacterTrafficReservationRuntime,
	target cityRealtimeActorSpawnCandidate,
) (int64, bool, error) {
	if runtime == nil || !cityRealtimeCharacterTrafficReservationBindingValid(runtime.Binding) ||
		!cityRealtimeCharacterTrafficCapacityPolicyValid(runtime.Policy) {
		return 0, false, ErrCityInvalidInput
	}
	terrainID, found, err := cityRealtimeCharacterTrafficTerrainIDAt(ctx, queryer, worldID, target)
	if err != nil || !found {
		return 0, false, err
	}
	capacity, known := runtime.Policy.Manifest.TerrainCapacities[terrainID]
	if !known || capacity < 1 || capacity > 64 {
		return 0, false, nil
	}
	return capacity, true, nil
}

func cityRealtimeCharacterTrafficGrantedReservationCount(
	ctx context.Context,
	tx *sql.Tx,
	worldID, dueWorldTimeUS int64,
	target cityRealtimeActorSpawnCandidate,
) (int64, error) {
	if tx == nil || worldID <= 0 || dueWorldTimeUS < 0 || dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 || target.Z != cityspatial.SurfaceZ {
		return 0, ErrCityInvalidInput
	}
	var count int64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_character_traffic_reservation_heads
WHERE world_id = $1 AND due_world_time_us = $2
  AND target_x = $3 AND target_y = $4 AND target_z = $5
  AND reservation_status = 'granted'`,
		worldID, dueWorldTimeUS, target.X, target.Y, target.Z,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count realtime character traffic slot grants: %w", err)
	}
	return count, nil
}

func applyCityRealtimeCharacterTrafficReservationDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	event cityRealtimeDueEventRecord,
) (bool, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 ||
		event.EventType != cityRealtimeDueEventTypeCharacterTrafficReservation ||
		event.SchemaVersion != cityRealtimeCharacterTrafficReservationSchemaVersion ||
		event.SourceKind != cityRealtimeDueEventSourceKindSystem || event.TemporalPhase != "movement" ||
		event.Priority != cityRealtimeCharacterTrafficReservationDuePriority ||
		event.AggregateType != "realtime_character_traffic" ||
		event.SourceReference != "realtime_character_traffic_reservation" || event.ExpectedVersion == nil {
		return false, nil
	}
	payload, validPayload := decodeCityRealtimeCharacterTrafficReservationDuePayload(event)
	if !validPayload || *event.ExpectedVersion != payload.PlanRevision {
		return false, nil
	}
	runtime, err := loadCityRealtimeCharacterTrafficReservationRuntime(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if runtime == nil {
		return false, nil
	}
	head, found, err := loadCityRealtimeCharacterNavigationPlanHead(
		ctx, tx, worldID, payload.ActorCode, payload.NavigationRunCode, true,
	)
	if err != nil {
		return false, err
	}
	if !found || head.PlanStatus != cityRealtimeCharacterNavigationPlanActive ||
		head.PlanRevision != payload.PlanRevision || head.NextDueWorldTimeUS == nil ||
		*head.NextDueWorldTimeUS != event.DueWorldTimeUS {
		return false, nil
	}
	expectedDedupKey, err := cityRealtimeCharacterTrafficReservationDueDedupKey(cityRealtimeCharacterTrafficReservationHead{
		ActorCode: head.ActorCode, NavigationRunCode: head.NavigationRunCode, PlanRevision: head.PlanRevision,
		DueWorldTimeUS: event.DueWorldTimeUS,
	})
	if err != nil {
		return false, err
	}
	expectedAggregateKey, err := cityRealtimeCharacterTrafficReservationAggregateKey(head.ActorCode, head.NavigationRunCode)
	if err != nil || event.DedupKey != expectedDedupKey || event.AggregateKey != expectedAggregateKey {
		return false, err
	}
	if _, alreadyExists, existingErr := loadCityRealtimeCharacterTrafficReservationHead(
		ctx, tx, worldID, head.ActorCode, head.NavigationRunCode, head.PlanRevision, true,
	); existingErr != nil {
		return false, existingErr
	} else if alreadyExists {
		return false, nil
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if agentState == nil || agentState.Binding == nil || agentState.Binding.BindingHash != runtime.Binding.AgentBindingHash ||
		!cityRealtimeCharacterTrafficReservationRuntimeEnabled(*agentState.Binding) {
		return false, nil
	}
	agent, agentFound := cityRealtimeCharacterNavigationAgentForActor(agentState, head.ActorCode)
	if !agentFound || agent.LifecycleStatus != "active" || agent.ControlMode != "autonomous" {
		return false, nil
	}
	record, recordFound, err := loadCityRealtimeCharacterNavigationRecordForUpdate(ctx, tx, worldID, head.ActorCode)
	if err != nil {
		return false, err
	}
	if !recordFound || record.identity.LifecycleStatus != "active" || record.state.Z != cityspatial.SurfaceZ {
		return false, nil
	}
	target, needed, err := cityRealtimeCharacterTrafficReservationTarget(ctx, tx, worldID, head, record)
	if err != nil || !needed {
		return false, err
	}
	capacity, capacityKnown, err := cityRealtimeCharacterTrafficCellCapacity(ctx, tx, worldID, runtime, target)
	if err != nil || !capacityKnown {
		return false, err
	}
	grantedCount, err := cityRealtimeCharacterTrafficGrantedReservationCount(ctx, tx, worldID, event.DueWorldTimeUS, target)
	if err != nil {
		return false, err
	}
	status := cityRealtimeCharacterTrafficReservationGranted
	reason := ""
	if grantedCount >= capacity {
		status = cityRealtimeCharacterTrafficReservationDenied
		reason = cityRealtimeCharacterTrafficReservationReasonCapacityUnavailable
	}
	origin := cityRealtimeActorSpawnCandidate{X: record.state.X, Y: record.state.Y, Z: record.state.Z}
	reservation, reservationEvent, err := cityRealtimeCharacterTrafficReservationNew(
		head.ActorCode, head.NavigationRunCode, head.PlanRevision, origin, target,
		event.DueWorldTimeUS, frameSequence, status, reason,
	)
	if err != nil {
		return false, err
	}
	if err = enableCityRealtimeCharacterTrafficReservationMutationGate(ctx, tx, worldID, frameSequence, event.DueWorldTimeUS); err != nil {
		return false, err
	}
	if err = insertCityRealtimeCharacterTrafficReservationHead(ctx, tx, worldID, reservation); err != nil {
		return false, err
	}
	if err = insertCityRealtimeCharacterTrafficReservationEvent(ctx, tx, worldID, reservationEvent); err != nil {
		return false, err
	}
	return true, nil
}

// consumeCityRealtimeCharacterTrafficReservation is called only after the
// navigation reducer appended the exact Actor position event. It turns a
// one-quantum grant into a sealed consumption receipt; it never moves an Actor
// by itself.
func consumeCityRealtimeCharacterTrafficReservation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence, dueWorldTimeUS int64,
	head cityRealtimeCharacterNavigationPlanHead,
	from, target cityRealtimeActorSpawnCandidate,
	actorPositionEventHash string,
) (bool, error) {
	runtime, err := loadCityRealtimeCharacterTrafficReservationRuntime(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if runtime == nil {
		return false, nil
	}
	reservation, found, err := loadCityRealtimeCharacterTrafficReservationHead(
		ctx, tx, worldID, head.ActorCode, head.NavigationRunCode, head.PlanRevision, true,
	)
	if err != nil {
		return false, err
	}
	if !found || reservation.ReservationStatus != cityRealtimeCharacterTrafficReservationGranted ||
		reservation.DueWorldTimeUS != dueWorldTimeUS || reservation.From != from || reservation.Target != target ||
		!cityRealtimeSHA256Hex(actorPositionEventHash) {
		return false, nil
	}
	next, reservationEvent, err := cityRealtimeCharacterTrafficReservationAdvance(
		reservation, frameSequence, cityRealtimeCharacterTrafficReservationConsumed, "", actorPositionEventHash,
	)
	if err != nil {
		return false, err
	}
	if err = enableCityRealtimeCharacterTrafficReservationMutationGate(ctx, tx, worldID, frameSequence, dueWorldTimeUS); err != nil {
		return false, err
	}
	if err = updateCityRealtimeCharacterTrafficReservationHead(ctx, tx, worldID, reservation, next); err != nil {
		return false, err
	}
	if err = insertCityRealtimeCharacterTrafficReservationEvent(ctx, tx, worldID, reservationEvent); err != nil {
		return false, err
	}
	return true, nil
}

func scanCityRealtimeCharacterTrafficReservationEvent(scanner cityScannable) (cityRealtimeCharacterTrafficReservationEvent, error) {
	event := cityRealtimeCharacterTrafficReservationEvent{}
	err := scanner.Scan(
		&event.ActorCode, &event.NavigationRunCode, &event.PlanRevision, &event.ReservationCode,
		&event.EventSequence, &event.FrameSequence, &event.EventType, &event.ReservationStatus, &event.ReasonCode,
		&event.From.X, &event.From.Y, &event.From.Z, &event.Target.X, &event.Target.Y, &event.Target.Z,
		&event.DueWorldTimeUS, &event.ActorPositionEventHash, &event.PreviousEventHash, &event.EventHash,
	)
	if err != nil {
		return cityRealtimeCharacterTrafficReservationEvent{}, err
	}
	if !cityRealtimeCharacterTrafficReservationEventValid(event) {
		return cityRealtimeCharacterTrafficReservationEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_event"})
	}
	return event, nil
}

func loadCityRealtimeCharacterTrafficReservationEvents(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterTrafficReservationHead,
) ([]cityRealtimeCharacterTrafficReservationEvent, error) {
	if worldID <= 0 || !cityRealtimeCharacterTrafficReservationHeadValid(head) {
		return nil, ErrCityInvalidInput
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code, navigation_run_code, plan_revision, reservation_code,
       event_sequence, frame_sequence, event_type, reservation_status, reason_code,
       from_x, from_y, from_z, target_x, target_y, target_z, due_world_time_us,
       actor_position_event_hash, previous_event_hash, event_hash
FROM city_realtime_character_traffic_reservation_events
WHERE world_id = $1 AND actor_code = $2 AND navigation_run_code = $3 AND plan_revision = $4
ORDER BY event_sequence ASC`, worldID, head.ActorCode, head.NavigationRunCode, head.PlanRevision)
	if err != nil {
		return nil, fmt.Errorf("load realtime character traffic reservation events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]cityRealtimeCharacterTrafficReservationEvent, 0, head.ReservationRevision)
	for rows.Next() {
		event, scanErr := scanCityRealtimeCharacterTrafficReservationEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, event)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character traffic reservation events: %w", err)
	}
	return items, nil
}

func cityRealtimeCharacterTrafficReservationEventMatchesHead(
	event cityRealtimeCharacterTrafficReservationEvent,
	head cityRealtimeCharacterTrafficReservationHead,
) bool {
	return event.ActorCode == head.ActorCode && event.NavigationRunCode == head.NavigationRunCode &&
		event.PlanRevision == head.PlanRevision && event.ReservationCode == head.ReservationCode &&
		event.From == head.From && event.Target == head.Target && event.DueWorldTimeUS == head.DueWorldTimeUS
}

func validateCityRealtimeCharacterTrafficReservationHeadHistory(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterTrafficReservationHead,
) error {
	if !cityRealtimeCharacterTrafficReservationHeadValid(head) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_head"})
	}
	events, err := loadCityRealtimeCharacterTrafficReservationEvents(ctx, queryer, worldID, head)
	if err != nil {
		return err
	}
	if int64(len(events)) != head.ReservationRevision || len(events) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_chain"})
	}
	genesisHash, err := cityRealtimeCharacterTrafficReservationChainGenesisHash(head)
	if err != nil {
		return err
	}
	previousHash := genesisHash
	for index, event := range events {
		if !cityRealtimeCharacterTrafficReservationEventValid(event) ||
			!cityRealtimeCharacterTrafficReservationEventMatchesHead(event, head) ||
			event.EventSequence != int64(index+1) || event.PreviousEventHash != previousHash ||
			event.FrameSequence < head.AcceptedFrameSequence {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_chain"})
		}
		expectedHash, hashErr := cityRealtimeCharacterTrafficReservationEventHash(event)
		if hashErr != nil || event.EventHash != expectedHash {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_hash"})
		}
		if index == 0 {
			expectedType := cityRealtimeCharacterTrafficReservationEventGranted
			if event.ReservationStatus == cityRealtimeCharacterTrafficReservationDenied {
				expectedType = cityRealtimeCharacterTrafficReservationEventDenied
			}
			if event.EventType != expectedType || event.ActorPositionEventHash != "" ||
				event.FrameSequence != head.AcceptedFrameSequence {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_genesis"})
			}
		} else {
			if index != 1 || events[0].ReservationStatus != cityRealtimeCharacterTrafficReservationGranted ||
				event.ReservationStatus != head.ReservationStatus {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_transition"})
			}
			switch event.EventType {
			case cityRealtimeCharacterTrafficReservationEventConsumed:
				if event.ReservationStatus != cityRealtimeCharacterTrafficReservationConsumed || event.ReasonCode != "" ||
					!cityRealtimeSHA256Hex(event.ActorPositionEventHash) || !cityRealtimeCharacterTrafficPositionEventExists(ctx, queryer, worldID, event) {
					return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_consumption"})
				}
			case cityRealtimeCharacterTrafficReservationEventReleased:
				if event.ReservationStatus != cityRealtimeCharacterTrafficReservationReleased || event.ActorPositionEventHash != "" ||
					!cityRealtimeCharacterTrafficReservationReasonValid(event.ReservationStatus, event.ReasonCode) {
					return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_release"})
				}
			default:
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_transition"})
			}
		}
		previousHash = event.EventHash
	}
	last := events[len(events)-1]
	if last.EventHash != head.EventChainHash || last.EventSequence != head.ReservationRevision ||
		last.FrameSequence != head.LastFrameSequence || last.ReservationStatus != head.ReservationStatus ||
		last.ReasonCode != head.ReasonCode {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_head_chain"})
	}
	return nil
}

func cityRealtimeCharacterTrafficPositionEventExists(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	event cityRealtimeCharacterTrafficReservationEvent,
) bool {
	if event.ActorPositionEventHash == "" {
		return false
	}
	var count int
	err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_actor_position_events
WHERE world_id = $1 AND actor_code = $2 AND frame_sequence = $3
  AND event_kind = 'move' AND from_x = $4 AND from_y = $5 AND from_z = $6
  AND to_x = $7 AND to_y = $8 AND to_z = $9 AND event_hash = $10`,
		worldID, event.ActorCode, event.FrameSequence,
		event.From.X, event.From.Y, event.From.Z,
		event.Target.X, event.Target.Y, event.Target.Z, event.ActorPositionEventHash,
	).Scan(&count)
	return err == nil && count == 1
}

func loadCityRealtimeCharacterTrafficReservationHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterTrafficReservationHashState, error) {
	runtime, err := loadCityRealtimeCharacterTrafficReservationRuntime(ctx, queryer, worldID)
	if err != nil || runtime == nil {
		return nil, err
	}
	state := &cityRealtimeCharacterTrafficReservationHashState{
		SchemaVersion: cityRealtimeCharacterTrafficReservationSchemaVersion,
		Binding:       &runtime.Binding,
		Heads:         make([]cityRealtimeCharacterTrafficReservationHead, 0),
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code, navigation_run_code, plan_revision, reservation_code,
       from_x, from_y, from_z, target_x, target_y, target_z,
       due_world_time_us, reservation_revision, reservation_status, reason_code,
       accepted_frame_sequence, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_traffic_reservation_heads
WHERE world_id = $1
ORDER BY actor_code ASC, navigation_run_code ASC, plan_revision ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime character traffic reservation hash heads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		head, scanErr := scanCityRealtimeCharacterTrafficReservationHead(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		state.Heads = append(state.Heads, head)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character traffic reservation hash heads: %w", err)
	}
	if err = validateCityRealtimeCharacterTrafficReservationHashState(state); err != nil {
		return nil, err
	}
	return state, nil
}

func validateCityRealtimeCharacterTrafficReservationHashState(state *cityRealtimeCharacterTrafficReservationHashState) error {
	if state == nil || state.SchemaVersion != cityRealtimeCharacterTrafficReservationSchemaVersion ||
		state.Binding == nil || state.Heads == nil || !cityRealtimeCharacterTrafficReservationBindingValid(*state.Binding) {
		return errors.New("invalid realtime character traffic reservation hash state")
	}
	for index, head := range state.Heads {
		if !cityRealtimeCharacterTrafficReservationHeadValid(head) {
			return errors.New("invalid realtime character traffic reservation head")
		}
		if index > 0 {
			previous := state.Heads[index-1]
			if previous.ActorCode > head.ActorCode ||
				(previous.ActorCode == head.ActorCode && previous.NavigationRunCode > head.NavigationRunCode) ||
				(previous.ActorCode == head.ActorCode && previous.NavigationRunCode == head.NavigationRunCode && previous.PlanRevision >= head.PlanRevision) {
				return errors.New("unordered realtime character traffic reservation heads")
			}
		}
	}
	return nil
}

// ListRealtimeCharacterTrafficReservations returns only an owner's coarse
// reservation receipts. It deliberately exposes neither a cell coordinate nor
// a mutation surface for traffic capacity.
func (s *CityEconomyService) ListRealtimeCharacterTrafficReservations(
	ctx context.Context,
	input CityRealtimeCharacterTrafficReservationListInput,
) ([]CityRealtimeCharacterTrafficReservation, error) {
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 200 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "limit"})
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin realtime character traffic reservation read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockCityRealtimeCharacterWorld(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	record, found, err := loadCityRealtimeOwnedCharacter(ctx, tx, input.WorldID, input.UserID, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterNotFound
	}
	runtime, err := loadCityRealtimeCharacterTrafficReservationRuntime(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if runtime == nil {
		return []CityRealtimeCharacterTrafficReservation{}, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT actor_code, navigation_run_code, plan_revision, reservation_code,
       from_x, from_y, from_z, target_x, target_y, target_z,
       due_world_time_us, reservation_revision, reservation_status, reason_code,
       accepted_frame_sequence, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_traffic_reservation_heads
WHERE world_id = $1 AND actor_code = $2
ORDER BY accepted_frame_sequence DESC, navigation_run_code DESC, plan_revision DESC
LIMIT $3`, input.WorldID, record.identity.ActorCode, limit)
	if err != nil {
		return nil, fmt.Errorf("list realtime character traffic reservations: %w", err)
	}
	headItems := make([]cityRealtimeCharacterTrafficReservationHead, 0)
	for rows.Next() {
		head, scanErr := scanCityRealtimeCharacterTrafficReservationHead(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		headItems = append(headItems, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character traffic reservations: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character traffic reservations: %w", err)
	}
	items := make([]CityRealtimeCharacterTrafficReservation, 0, len(headItems))
	for _, head := range headItems {
		if err = validateCityRealtimeCharacterTrafficReservationHeadHistory(ctx, tx, input.WorldID, head); err != nil {
			return nil, err
		}
		items = append(items, cityRealtimeCharacterTrafficReservationProjection(head))
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character traffic reservation read: %w", err)
	}
	return items, nil
}
