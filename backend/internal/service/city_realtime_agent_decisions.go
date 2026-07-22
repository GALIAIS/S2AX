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
	"time"
)

const (
	cityRealtimeAgentDecisionStateSchemaVersion = 1
	cityRealtimeAgentObservationSchemaVersion   = 1
	cityRealtimeAgentDecisionEnvelopeVersion    = "agent-decision-v1"
	cityRealtimeAgentFakeProviderCode           = "fake.deterministic"

	cityRealtimeAgentDecisionRequestQueued   = "queued"
	cityRealtimeAgentDecisionRequestLeased   = "leased"
	cityRealtimeAgentDecisionRequestAccepted = "accepted"
	cityRealtimeAgentDecisionRequestRejected = "rejected"
	cityRealtimeAgentDecisionRequestStale    = "stale"
	cityRealtimeAgentDecisionRequestFailed   = "failed_terminal"
	cityRealtimeAgentDecisionRequestCanceled = "cancelled"

	cityRealtimeAgentDecisionAttemptStarted   = "started"
	cityRealtimeAgentDecisionAttemptSucceeded = "succeeded"
	cityRealtimeAgentDecisionAttemptFailed    = "failed"

	cityRealtimeAgentDecisionAccepted = "accepted"
	cityRealtimeAgentDecisionRejected = "rejected"
	cityRealtimeAgentDecisionStale    = "stale"

	cityRealtimeAgentIntentPending   = "pending"
	cityRealtimeAgentIntentApplied   = "applied"
	cityRealtimeAgentIntentRejected  = "rejected"
	cityRealtimeAgentIntentStale     = "stale"
	cityRealtimeAgentIntentCancelled = "cancelled"

	cityRealtimeAgentOutboxQueued         = "queued"
	cityRealtimeAgentOutboxLeased         = "leased"
	cityRealtimeAgentOutboxSucceeded      = "succeeded"
	cityRealtimeAgentOutboxFailed         = "failed_terminal"
	cityRealtimeAgentOutboxCancelled      = "cancelled"
	cityRealtimeAgentIntentActionWait     = "agent.wait"
	cityRealtimeAgentIntentActionActivity = "character.activity.perform"
	cityRealtimeAgentIntentActionMove     = "character.move"
	cityRealtimeAgentIntentActionPortal   = "character.portal.traverse"
	cityRealtimeAgentIntentActionRole     = "character.role.change"

	cityRealtimeDueEventTypeAgentIntent         = "system.realtime.agent_intent"
	cityRealtimeDueEventTypeAgentDecisionWakeup = "system.realtime.agent_wakeup"

	cityRealtimeAgentDecisionLeaseDuration    = 30 * time.Second
	cityRealtimeAgentDecisionObservationTTLUS = int64(15 * 60 * cityRealtimeTimeQuantumUS)
	cityRealtimeAgentDecisionMaximumAttempts  = 3
)

var errCityRealtimeAgentDecisionLeaseBudgetExhausted = errors.New("realtime agent decision lease budget exhausted")

// cityRealtimeAgentDecisionHashState deliberately holds only unresolved work
// that can still influence a future Temporal Frame. Provider leases, timing,
// attempts, raw model output and terminal audit rows stay outside the canonical
// state. This prevents an external worker retry from changing a world hash.
type cityRealtimeAgentDecisionHashState struct {
	SchemaVersion   int                                       `json:"schema_version"`
	BindingHash     string                                    `json:"binding_hash"`
	PendingRequests []cityRealtimeAgentPendingDecisionRequest `json:"pending_requests"`
	PendingIntents  []cityRealtimeAgentPendingIntent          `json:"pending_intents"`
}

type cityRealtimeAgentPendingDecisionRequest struct {
	RequestCode           string `json:"request_code"`
	AgentCode             string `json:"agent_code"`
	ObservationHash       string `json:"observation_hash"`
	PreconditionHash      string `json:"precondition_hash"`
	ObservedFrameSequence int64  `json:"observed_frame_sequence"`
	ExpiresAtWorldTimeUS  int64  `json:"expires_at_world_time_us"`
}

type cityRealtimeAgentPendingIntent struct {
	IntentCode                string  `json:"intent_code"`
	DecisionCode              string  `json:"decision_code"`
	AgentCode                 string  `json:"agent_code"`
	ActorCode                 *string `json:"actor_code,omitempty"`
	ActionCode                string  `json:"action_code"`
	ArgumentsHash             string  `json:"arguments_hash"`
	PreconditionHash          string  `json:"precondition_hash"`
	ExecuteAfterFrameSequence int64   `json:"execute_after_frame_sequence"`
	ExecuteAtWorldTimeUS      int64   `json:"execute_at_world_time_us"`
}

type cityRealtimeAgentObservationRecord struct {
	ObservationCode          string
	AgentCode                string
	ObservedFrameSequence    int64
	ObservedTimelineCursor   string
	ObservedWorldTimeUS      int64
	ObservationSchemaVersion int
	ObservationSchemaHash    string
	RedactionPolicyCode      string
	TriggerKey               string
	Payload                  json.RawMessage
	PayloadHash              string
	PreconditionHash         string
	ExpiresAtWorldTimeUS     int64
	CreatedFrameSequence     int64
}

type cityRealtimeAgentDecisionRequestRecord struct {
	RequestCode            string
	AgentCode              string
	ObservationCode        string
	ObservationHash        string
	PreconditionHash       string
	ObservedFrameSequence  int64
	ExpiresAtWorldTimeUS   int64
	Status                 string
	AttemptCount           int
	LeaseOwner             *string
	LeaseExpiresAt         *time.Time
	RequestedFrameSequence int64
	TerminalFrameSequence  *int64
}

type cityRealtimeAgentDecisionAttemptRecord struct {
	AttemptCode   string
	RequestCode   string
	AttemptNumber int
	ProviderCode  string
	Status        string
	RequestHash   string
	ResponseHash  *string
}

type cityRealtimeAgentDecisionRecord struct {
	DecisionCode          string
	RequestCode           string
	AttemptCode           string
	DecisionStatus        string
	ActionCode            string
	Arguments             json.RawMessage
	ArgumentsHash         string
	ObservationHash       string
	PreconditionHash      string
	ReasonCode            string
	IntentCode            *string
	ResolvedFrameSequence int64
	DecisionHash          string
}

type cityRealtimeAgentIntentRecord struct {
	IntentCode                string
	DecisionCode              string
	AgentCode                 string
	ActorCode                 *string
	ActionCode                string
	Arguments                 json.RawMessage
	ArgumentsHash             string
	PreconditionHash          string
	ExecuteAfterFrameSequence int64
	ExecuteAtWorldTimeUS      int64
	Status                    string
	ScheduledFrameSequence    int64
	ResolvedFrameSequence     *int64
	IntentHash                string
}

// CityRealtimeAgentDecisionRequestInput is server-owned work scheduling. It
// intentionally has no HTTP route: A2 must not expose a browser endpoint that
// can make an Agent observe or act on arbitrary world state.
type CityRealtimeAgentDecisionRequestInput struct {
	WorldID   int64
	AgentCode string
	// TriggerKey is a server-owned, one-shot causal key. Repeating the exact
	// key for an agent returns its original request and must never append a new
	// Observation frame. Schedulers therefore derive a fresh key for each
	// independent decision opportunity.
	TriggerKey string
}

type CityRealtimeAgentDecisionRequestResult struct {
	RequestCode     string             `json:"request_code"`
	ObservationCode string             `json:"observation_code"`
	Status          string             `json:"status"`
	Frame           *CityTemporalFrame `json:"frame,omitempty"`
}

// CityRealtimeAgentFakeDecisionRunInput drives the deterministic A2 provider.
// It exists to exercise exactly the same request/attempt/decision/intent path
// without provider credentials, a network call, or a model-dependent outcome.
type CityRealtimeAgentFakeDecisionRunInput struct {
	WorldID     int64
	RequestCode string
	WorkerID    string
	// PreferredAction is an administrator/test-only deterministic adapter
	// selector. It never becomes a browser route or provider setting, and it
	// can select only an action already published in the sealed observation.
	PreferredAction string
}

type CityRealtimeAgentFakeDecisionRunResult struct {
	RequestCode  string             `json:"request_code"`
	DecisionCode string             `json:"decision_code,omitempty"`
	IntentCode   string             `json:"intent_code,omitempty"`
	Status       string             `json:"status"`
	Frame        *CityTemporalFrame `json:"frame,omitempty"`
}

type cityRealtimeAgentDecisionEnvelope struct {
	SchemaVersion    string                          `json:"schema_version"`
	RequestCode      string                          `json:"request_code"`
	ObservationHash  string                          `json:"observation_hash"`
	PreconditionHash string                          `json:"precondition_hash"`
	Intent           cityRealtimeAgentEnvelopeIntent `json:"intent"`
	ReasonCode       string                          `json:"reason_code"`
}

type cityRealtimeAgentEnvelopeIntent struct {
	ActionCode string         `json:"action_code"`
	Arguments  map[string]any `json:"arguments"`
}

type cityRealtimeAgentObservationSnapshot struct {
	ObservationCode       string
	ObservationSchemaHash string
	Payload               json.RawMessage
	PayloadHash           string
	PreconditionHash      string
	ExpiresAtWorldTimeUS  int64
}

func newCityRealtimeAgentDecisionHashState(binding cityRealtimeAgentPolicyBinding) *cityRealtimeAgentDecisionHashState {
	return &cityRealtimeAgentDecisionHashState{
		SchemaVersion:   cityRealtimeAgentDecisionStateSchemaVersion,
		BindingHash:     binding.BindingHash,
		PendingRequests: make([]cityRealtimeAgentPendingDecisionRequest, 0),
		PendingIntents:  make([]cityRealtimeAgentPendingIntent, 0),
	}
}

func cityRealtimeAgentPolicyVersionSupported(version string) bool {
	switch version {
	case cityRealtimeAgentCorePolicyVersionLegacy,
		cityRealtimeAgentCorePolicyVersionDecision,
		cityRealtimeAgentCorePolicyVersionAutonomy,
		cityRealtimeAgentCorePolicyVersion:
		return true
	default:
		return false
	}
}

func cityRealtimeAgentDecisionRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionDecision ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionAutonomy ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersion)
}

// cityRealtimeAgentCharacterControlRuntimeEnabled is intentionally pinned to
// 1.2.0 and newer. A2 worlds remain read/write compatible with their original
// wait-only decision runtime and never gain a personality/control event
// retroactively.
func cityRealtimeAgentCharacterControlRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionAutonomy ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersion)
}

// cityRealtimeAgentCharacterActionRuntimeEnabled is deliberately 1.3-only.
// A3.1 worlds remain frozen to their wait/activity action catalogue even after
// the binary learns later navigation and role adapters.
func cityRealtimeAgentCharacterActionRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		binding.PolicyVersion == cityRealtimeAgentCorePolicyVersion
}

func validateCityRealtimeAgentDecisionHashState(
	binding cityRealtimeAgentPolicyBinding,
	state cityRealtimeAgentDecisionHashState,
) error {
	if !cityRealtimeAgentDecisionRuntimeEnabled(binding) ||
		state.SchemaVersion != cityRealtimeAgentDecisionStateSchemaVersion ||
		state.BindingHash != binding.BindingHash || state.PendingRequests == nil || state.PendingIntents == nil {
		return fmt.Errorf("invalid realtime agent decision hash state")
	}
	for index, request := range state.PendingRequests {
		if !cityRealtimeAgentDecisionPendingRequestValid(request) {
			return fmt.Errorf("invalid realtime agent pending request")
		}
		if index > 0 && cityRealtimeAgentDecisionRequestCompare(state.PendingRequests[index-1], request) >= 0 {
			return fmt.Errorf("realtime agent pending requests are not in stable canonical order")
		}
	}
	for index, intent := range state.PendingIntents {
		if !cityRealtimeAgentPendingIntentValid(intent) {
			return fmt.Errorf("invalid realtime agent pending intent")
		}
		if index > 0 && cityRealtimeAgentPendingIntentCompare(state.PendingIntents[index-1], intent) >= 0 {
			return fmt.Errorf("realtime agent pending intents are not in stable canonical order")
		}
	}
	return nil
}

func cityRealtimeAgentDecisionPendingRequestValid(request cityRealtimeAgentPendingDecisionRequest) bool {
	return cityRealtimeAgentIdentifierValid(request.RequestCode, 96) &&
		cityRealtimeAgentIdentifierValid(request.AgentCode, 96) &&
		cityRealtimeSHA256Hex(request.ObservationHash) &&
		cityRealtimeSHA256Hex(request.PreconditionHash) &&
		request.ObservedFrameSequence > 0 && request.ExpiresAtWorldTimeUS >= 0
}

func cityRealtimeAgentPendingIntentValid(intent cityRealtimeAgentPendingIntent) bool {
	if !cityRealtimeAgentIdentifierValid(intent.IntentCode, 96) ||
		!cityRealtimeAgentIdentifierValid(intent.DecisionCode, 96) ||
		!cityRealtimeAgentIdentifierValid(intent.AgentCode, 96) ||
		!cityRealtimeAgentIdentifierValid(intent.ActionCode, 64) ||
		!cityRealtimeSHA256Hex(intent.ArgumentsHash) ||
		!cityRealtimeSHA256Hex(intent.PreconditionHash) ||
		intent.ExecuteAfterFrameSequence <= 0 || intent.ExecuteAtWorldTimeUS < 0 {
		return false
	}
	return intent.ActorCode == nil || cityRealtimeAgentIdentifierValid(*intent.ActorCode, 96)
}

func cityRealtimeAgentDecisionRequestCompare(left, right cityRealtimeAgentPendingDecisionRequest) int {
	if left.AgentCode < right.AgentCode {
		return -1
	}
	if left.AgentCode > right.AgentCode {
		return 1
	}
	if left.RequestCode < right.RequestCode {
		return -1
	}
	if left.RequestCode > right.RequestCode {
		return 1
	}
	return 0
}

func cityRealtimeAgentPendingIntentCompare(left, right cityRealtimeAgentPendingIntent) int {
	if left.ExecuteAtWorldTimeUS < right.ExecuteAtWorldTimeUS {
		return -1
	}
	if left.ExecuteAtWorldTimeUS > right.ExecuteAtWorldTimeUS {
		return 1
	}
	if left.IntentCode < right.IntentCode {
		return -1
	}
	if left.IntentCode > right.IntentCode {
		return 1
	}
	return 0
}

func loadCityRealtimeAgentDecisionHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	binding cityRealtimeAgentPolicyBinding,
	state *cityRealtimeAgentDecisionHashState,
) error {
	if state == nil || worldID <= 0 || !cityRealtimeAgentDecisionRuntimeEnabled(binding) {
		return ErrCityInvalidInput
	}
	state.SchemaVersion = cityRealtimeAgentDecisionStateSchemaVersion
	state.BindingHash = binding.BindingHash
	state.PendingRequests = make([]cityRealtimeAgentPendingDecisionRequest, 0)
	state.PendingIntents = make([]cityRealtimeAgentPendingIntent, 0)

	requestRows, err := queryer.QueryContext(ctx, `
SELECT request_code, agent_code, observation_hash, precondition_hash,
       observed_frame_sequence, expires_at_world_time_us
FROM city_realtime_agent_decision_requests
WHERE world_id = $1 AND status IN ('queued', 'leased')
ORDER BY agent_code ASC, request_code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load realtime agent pending decision requests: %w", err)
	}
	defer func() { _ = requestRows.Close() }()
	for requestRows.Next() {
		item := cityRealtimeAgentPendingDecisionRequest{}
		if err = requestRows.Scan(
			&item.RequestCode, &item.AgentCode, &item.ObservationHash, &item.PreconditionHash,
			&item.ObservedFrameSequence, &item.ExpiresAtWorldTimeUS,
		); err != nil {
			return err
		}
		state.PendingRequests = append(state.PendingRequests, item)
	}
	if err = requestRows.Err(); err != nil {
		return fmt.Errorf("iterate realtime agent pending decision requests: %w", err)
	}

	intentRows, err := queryer.QueryContext(ctx, `
SELECT intent_code, decision_code, agent_code, actor_code, action_code,
       arguments_hash, precondition_hash, execute_after_frame_sequence,
       execute_at_world_time_us
FROM city_realtime_agent_intents
WHERE world_id = $1 AND status = 'pending'
ORDER BY execute_at_world_time_us ASC, intent_code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load realtime agent pending intents: %w", err)
	}
	defer func() { _ = intentRows.Close() }()
	for intentRows.Next() {
		item := cityRealtimeAgentPendingIntent{}
		var actorCode sql.NullString
		if err = intentRows.Scan(
			&item.IntentCode, &item.DecisionCode, &item.AgentCode, &actorCode, &item.ActionCode,
			&item.ArgumentsHash, &item.PreconditionHash, &item.ExecuteAfterFrameSequence,
			&item.ExecuteAtWorldTimeUS,
		); err != nil {
			return err
		}
		item.ActorCode = cityRealtimeAgentNullStringPointer(actorCode)
		state.PendingIntents = append(state.PendingIntents, item)
	}
	if err = intentRows.Err(); err != nil {
		return fmt.Errorf("iterate realtime agent pending intents: %w", err)
	}
	return nil
}

func cityRealtimeAgentDecisionStableCode(prefix string, values ...string) (string, error) {
	if !cityRealtimeAgentIdentifierValid(prefix, 16) {
		return "", ErrCityInvalidInput
	}
	parts := append([]string{"city-realtime-agent-decision-code-v1", prefix}, values...)
	code := prefix + "." + cityOpenWorldPayloadHash([]byte(strings.Join(parts, "\x1f")))
	if !cityRealtimeAgentIdentifierValid(code, 96) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_code"})
	}
	return code, nil
}

func cityRealtimeAgentDecisionAllowedActions(
	binding cityRealtimeAgentPolicyBinding,
	agent cityRealtimeAgentInstance,
) ([]string, bool) {
	if !cityRealtimeAgentDecisionRuntimeEnabled(binding) || agent.LifecycleStatus != "active" {
		return nil, false
	}
	if binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionDecision {
		switch agent.AgentSubtype {
		case "system.root", "system.npc_manager":
			return []string{cityRealtimeAgentIntentActionWait}, agent.ControlMode == "system"
		case "character.npc":
			return []string{cityRealtimeAgentIntentActionWait}, agent.ControlMode == "autonomous"
		case "character.user":
			return []string{cityRealtimeAgentIntentActionWait}, agent.ControlMode == "assisted" || agent.ControlMode == "autonomous"
		default:
			return nil, false
		}
	}
	if binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionAutonomy {
		switch agent.AgentSubtype {
		case "system.root", "system.npc_manager":
			return []string{cityRealtimeAgentIntentActionWait}, agent.ControlMode == "system"
		case "character.npc":
			return []string{cityRealtimeAgentIntentActionWait}, agent.ControlMode == "autonomous"
		case "character.user":
			if agent.ControlMode != "autonomous" {
				return nil, false
			}
			return []string{cityRealtimeAgentIntentActionWait, cityRealtimeAgentIntentActionActivity}, true
		default:
			return nil, false
		}
	}
	switch agent.AgentSubtype {
	case "system.root", "system.npc_manager":
		return []string{cityRealtimeAgentIntentActionWait}, agent.ControlMode == "system"
	case "character.npc":
		return []string{cityRealtimeAgentIntentActionWait}, agent.ControlMode == "autonomous"
	case "character.user":
		if agent.ControlMode != "autonomous" {
			return nil, false
		}
		return []string{
			cityRealtimeAgentIntentActionWait,
			cityRealtimeAgentIntentActionActivity,
			cityRealtimeAgentIntentActionMove,
			cityRealtimeAgentIntentActionPortal,
			cityRealtimeAgentIntentActionRole,
		}, true
	default:
		return nil, false
	}
}

func cityRealtimeAgentDecisionActionAllowed(
	binding cityRealtimeAgentPolicyBinding,
	agent cityRealtimeAgentInstance,
	actionCode string,
) bool {
	allowed, available := cityRealtimeAgentDecisionAllowedActions(binding, agent)
	if !available {
		return false
	}
	index := sort.SearchStrings(allowed, actionCode)
	return index < len(allowed) && allowed[index] == actionCode
}

func cityRealtimeAgentDecisionAgentByCode(
	state *cityRealtimeAgentHashState,
	agentCode string,
) (cityRealtimeAgentInstance, bool) {
	if state == nil {
		return cityRealtimeAgentInstance{}, false
	}
	index := sort.Search(len(state.Agents), func(index int) bool {
		return state.Agents[index].AgentCode >= agentCode
	})
	if index >= len(state.Agents) || state.Agents[index].AgentCode != agentCode {
		return cityRealtimeAgentInstance{}, false
	}
	return state.Agents[index], true
}

func loadCityRealtimeAgentDecisionActorState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	agent cityRealtimeAgentInstance,
) (cityRealtimeActorState, error) {
	if worldID <= 0 || agent.ActorCode == nil || !cityRealtimeAgentIdentifierValid(*agent.ActorCode, 96) {
		return cityRealtimeActorState{}, ErrCityInvalidInput
	}
	state := cityRealtimeActorState{ActorCode: *agent.ActorCode}
	err := queryer.QueryRowContext(ctx, `
SELECT x, y, z, motion_state, position_revision, last_frame_sequence,
       state_hash, event_chain_hash
FROM city_realtime_actor_states
WHERE world_id = $1 AND actor_code = $2`, worldID, *agent.ActorCode).Scan(
		&state.X, &state.Y, &state.Z, &state.MotionState, &state.PositionRevision,
		&state.LastFrameSequence, &state.StateHash, &state.EventChainHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeActorState{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_actor_state"})
	}
	if err != nil {
		return cityRealtimeActorState{}, fmt.Errorf("load realtime agent actor state: %w", err)
	}
	if !cityRealtimeActorStateValid(state) {
		return cityRealtimeActorState{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_actor_state"})
	}
	return state, nil
}

func cityRealtimeAgentDecisionSnapshot(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	state *lockedCityRealtimeState,
	binding cityRealtimeAgentPolicyBinding,
	agent cityRealtimeAgentInstance,
	triggerKey string,
) (cityRealtimeAgentObservationSnapshot, error) {
	if worldID <= 0 || state == nil || !cityRealtimeAgentDecisionRuntimeEnabled(binding) ||
		!cityRealtimeAgentIdentifierValid(triggerKey, 96) {
		return cityRealtimeAgentObservationSnapshot{}, ErrCityInvalidInput
	}
	allowedActions, available := cityRealtimeAgentDecisionAllowedActions(binding, agent)
	if !available {
		return cityRealtimeAgentObservationSnapshot{}, ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "control_mode"})
	}

	actorSnapshot := map[string]any(nil)
	actorStateHash := ""
	characterStateHash := ""
	characterSnapshot := map[string]any(nil)
	personalitySeedHash := ""
	personalityRevision := int64(0)
	actionContextHash := ""
	if agent.ActorCode != nil {
		actorState, actorStateErr := loadCityRealtimeAgentDecisionActorState(ctx, queryer, worldID, agent)
		if actorStateErr != nil {
			return cityRealtimeAgentObservationSnapshot{}, actorStateErr
		}
		actorStateHash = actorState.StateHash
		actorSnapshot = map[string]any{
			"actor_code":        *agent.ActorCode,
			"position":          map[string]any{"x": actorState.X, "y": actorState.Y, "z": actorState.Z},
			"motion_state":      actorState.MotionState,
			"position_revision": actorState.PositionRevision,
		}
	}
	if agent.AgentSubtype == "character.user" {
		if agent.ActorCode == nil {
			return cityRealtimeAgentObservationSnapshot{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_character"})
		}
		profile, found, err := loadCityRealtimeCharacterProfile(ctx, queryer, worldID, *agent.ActorCode, false)
		if err != nil {
			return cityRealtimeAgentObservationSnapshot{}, err
		}
		if !found || !cityRealtimeCharacterProfileValid(profile) {
			return cityRealtimeAgentObservationSnapshot{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_character_profile"})
		}
		characterStateHash = profile.StateHash
		roles := make([]string, 0, len(profile.Roles))
		for _, role := range profile.Roles {
			roles = append(roles, role.RoleCode)
		}
		sort.Strings(roles)
		characterSnapshot = map[string]any{
			"energy_milli":         profile.EnergyMilli,
			"satiety_milli":        profile.SatietyMilli,
			"morale_milli":         profile.MoraleMilli,
			"civic_standing_milli": profile.CivicStandingMilli,
			"city_credit_units":    profile.CityCreditUnits,
			"roles":                roles,
		}
		if cityRealtimeAgentCharacterControlRuntimeEnabled(binding) {
			personality, personalityFound, personalityErr := loadCityRealtimeCharacterAgentPersonalityRevision(
				ctx, queryer, worldID, agent.AgentCode, false,
			)
			if personalityErr != nil {
				return cityRealtimeAgentObservationSnapshot{}, personalityErr
			}
			if agent.ControlMode == "autonomous" && !personalityFound {
				return cityRealtimeAgentObservationSnapshot{}, ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "personality"})
			}
			if personalityFound {
				personalitySeedHash = personality.SeedHash
				personalityRevision = personality.Revision
				// Only the revision/hash crosses the A2 observation boundary. The
				// owner-private seed is assembled as explicitly-delimited data by
				// the future A4 provider adapter, never by this durable queue.
				characterSnapshot["personality_revision"] = personalityRevision
				characterSnapshot["personality_seed_hash"] = personalitySeedHash
			}
		}
		if cityRealtimeAgentDecisionActionAllowed(binding, agent, cityRealtimeAgentIntentActionActivity) {
			actorState, actorStateErr := loadCityRealtimeAgentDecisionActorState(ctx, queryer, worldID, agent)
			if actorStateErr != nil {
				return cityRealtimeAgentObservationSnapshot{}, actorStateErr
			}
			lifeRuntime, runtimeErr := loadCityRealtimeCharacterLifeRuntime(ctx, queryer, worldID)
			if runtimeErr != nil {
				return cityRealtimeAgentObservationSnapshot{}, runtimeErr
			}
			if lifeRuntime == nil || !cityRealtimeCharacterProfileMatchesRuntime(profile, lifeRuntime) {
				return cityRealtimeAgentObservationSnapshot{}, ErrCityRealtimeCharacterRuntimeUnavailable
			}
			availability, availabilityErr := cityRealtimeCharacterActivityAvailability(
				ctx, queryer, worldID, state.currentWorldTimeUS, actorState, profile, lifeRuntime,
			)
			if availabilityErr != nil {
				return cityRealtimeAgentObservationSnapshot{}, availabilityErr
			}
			availableCodes := make([]string, 0, len(availability))
			for _, item := range availability {
				if item.Available {
					availableCodes = append(availableCodes, item.Code)
				}
			}
			characterSnapshot["available_activity_codes"] = availableCodes
		}
		if cityRealtimeAgentCharacterActionRuntimeEnabled(binding) {
			actorState, actorStateErr := loadCityRealtimeAgentDecisionActorState(ctx, queryer, worldID, agent)
			if actorStateErr != nil {
				return cityRealtimeAgentObservationSnapshot{}, actorStateErr
			}
			lifeRuntime, runtimeErr := loadCityRealtimeCharacterLifeRuntime(ctx, queryer, worldID)
			if runtimeErr != nil {
				return cityRealtimeAgentObservationSnapshot{}, runtimeErr
			}
			actionContext, contextErr := cityRealtimeAgentDecisionCharacterActionContext(
				ctx, queryer, worldID, state.currentWorldTimeUS, binding, agent, actorState, profile, lifeRuntime,
			)
			if contextErr != nil {
				return cityRealtimeAgentObservationSnapshot{}, contextErr
			}
			rawActionContext, marshalErr := json.Marshal(actionContext)
			if marshalErr != nil {
				return cityRealtimeAgentObservationSnapshot{}, fmt.Errorf("marshal realtime agent action context: %w", marshalErr)
			}
			if _, actionContextHash, contextErr = cityRealtimeCanonicalJSONObjectRaw(rawActionContext); contextErr != nil {
				return cityRealtimeAgentObservationSnapshot{}, fmt.Errorf("canonicalize realtime agent action context: %w", contextErr)
			}
			characterSnapshot["action_context"] = actionContext
		}
	}

	preconditionPayload := map[string]any{
		"schema_version":        cityRealtimeAgentObservationSchemaVersion,
		"binding_hash":          binding.BindingHash,
		"agent_code":            agent.AgentCode,
		"agent_instance_hash":   agent.InstanceHash,
		"agent_status":          agent.LifecycleStatus,
		"control_mode":          agent.ControlMode,
		"actor_state_hash":      actorStateHash,
		"character_state_hash":  characterStateHash,
		"personality_seed_hash": personalitySeedHash,
		"personality_revision":  personalityRevision,
		"allowed_actions":       allowedActions,
	}
	if actionContextHash != "" {
		preconditionPayload["action_context_hash"] = actionContextHash
	}
	_, preconditionHash, err := cityRealtimeCanonicalJSONObject(preconditionPayload)
	if err != nil {
		return cityRealtimeAgentObservationSnapshot{}, fmt.Errorf("canonicalize realtime agent precondition: %w", err)
	}
	payload := map[string]any{
		"schema_version": cityRealtimeAgentObservationSchemaVersion,
		"world": map[string]any{
			"timeline_cursor":         state.timelineCursor,
			"observed_frame_sequence": state.timelineFrameSequence,
			"observed_world_time_us":  state.currentWorldTimeUS,
		},
		"agent": map[string]any{
			"agent_code":         agent.AgentCode,
			"agent_subtype":      agent.AgentSubtype,
			"lifecycle_status":   agent.LifecycleStatus,
			"control_mode":       agent.ControlMode,
			"authorization_hash": agent.AuthorizationHash,
		},
		"allowed_actions":   allowedActions,
		"precondition_hash": preconditionHash,
	}
	if actorSnapshot != nil {
		payload["actor"] = actorSnapshot
	}
	if characterSnapshot != nil {
		payload["character"] = characterSnapshot
	}
	rawPayload, payloadHash, err := cityRealtimeCanonicalJSONObject(payload)
	if err != nil {
		return cityRealtimeAgentObservationSnapshot{}, fmt.Errorf("canonicalize realtime agent observation: %w", err)
	}
	observationCode, err := cityRealtimeAgentDecisionStableCode(
		"obs", binding.BindingHash, agent.AgentCode, strconv.FormatInt(state.timelineFrameSequence+1, 10), triggerKey, payloadHash,
	)
	if err != nil {
		return cityRealtimeAgentObservationSnapshot{}, err
	}
	if state.currentWorldTimeUS > cityRealtimeMaximumWorldTimeUS-cityRealtimeAgentDecisionObservationTTLUS {
		return cityRealtimeAgentObservationSnapshot{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_observation_expiry"})
	}
	return cityRealtimeAgentObservationSnapshot{
		ObservationCode:       observationCode,
		ObservationSchemaHash: cityOpenWorldPayloadHash([]byte("city-realtime-agent-observation-v1")),
		Payload:               rawPayload,
		PayloadHash:           payloadHash,
		PreconditionHash:      preconditionHash,
		ExpiresAtWorldTimeUS:  state.currentWorldTimeUS + cityRealtimeAgentDecisionObservationTTLUS,
	}, nil
}

func cityRealtimeAgentDecisionCurrentPreconditionHash(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	currentWorldTimeUS int64,
	binding cityRealtimeAgentPolicyBinding,
	agent cityRealtimeAgentInstance,
) (string, error) {
	// Rebuild through the same scope filter, but use a temporary immutable
	// realtime state so the current timeline cursor itself never makes a valid
	// decision stale. Actor/profile hashes and control state remain pinned.
	if currentWorldTimeUS < 0 {
		return "", ErrCityInvalidInput
	}
	state := &lockedCityRealtimeState{
		timelineFrameSequence: 0,
		timelineCursor:        "twf_000000000000",
		currentWorldTimeUS:    currentWorldTimeUS,
	}
	snapshot, err := cityRealtimeAgentDecisionSnapshot(ctx, queryer, worldID, state, binding, agent, "precondition.check")
	if err != nil {
		return "", err
	}
	return snapshot.PreconditionHash, nil
}

func cityRealtimeAgentDecisionRequestStatusActive(status string) bool {
	return status == cityRealtimeAgentDecisionRequestQueued || status == cityRealtimeAgentDecisionRequestLeased
}

func cityRealtimeAgentDecisionRequestStatusTerminal(status string) bool {
	switch status {
	case cityRealtimeAgentDecisionRequestAccepted, cityRealtimeAgentDecisionRequestRejected,
		cityRealtimeAgentDecisionRequestStale, cityRealtimeAgentDecisionRequestFailed,
		cityRealtimeAgentDecisionRequestCanceled:
		return true
	default:
		return false
	}
}

func enableCityRealtimeAgentDecisionWorkerGate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	requestCode string,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) {
		return ErrCityInvalidInput
	}
	for _, setting := range []struct {
		name  string
		value string
	}{
		{name: "sub2api.city_realtime_agent_worker_world_id", value: strconv.FormatInt(worldID, 10)},
		{name: "sub2api.city_realtime_agent_worker_request_code", value: requestCode},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, setting.value); err != nil {
			return fmt.Errorf("activate realtime agent worker gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func loadCityRealtimeAgentDecisionRequest(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	requestCode string,
	forUpdate bool,
) (cityRealtimeAgentDecisionRequestRecord, bool, error) {
	if worldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) {
		return cityRealtimeAgentDecisionRequestRecord{}, false, ErrCityInvalidInput
	}
	query := `
SELECT request_code, agent_code, observation_code, observation_hash,
       precondition_hash, observed_frame_sequence, expires_at_world_time_us,
       status, attempt_count, lease_owner, lease_expires_at,
       requested_frame_sequence, terminal_frame_sequence
FROM city_realtime_agent_decision_requests
WHERE world_id = $1 AND request_code = $2`
	if forUpdate {
		query += " FOR UPDATE"
	}
	item := cityRealtimeAgentDecisionRequestRecord{}
	var leaseOwner sql.NullString
	var leaseExpiresAt sql.NullTime
	var terminalFrameSequence sql.NullInt64
	err := queryer.QueryRowContext(ctx, query, worldID, requestCode).Scan(
		&item.RequestCode, &item.AgentCode, &item.ObservationCode, &item.ObservationHash,
		&item.PreconditionHash, &item.ObservedFrameSequence, &item.ExpiresAtWorldTimeUS,
		&item.Status, &item.AttemptCount, &leaseOwner, &leaseExpiresAt,
		&item.RequestedFrameSequence, &terminalFrameSequence,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeAgentDecisionRequestRecord{}, false, nil
	}
	if err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, false, fmt.Errorf("load realtime agent decision request: %w", err)
	}
	item.LeaseOwner = cityRealtimeAgentNullStringPointer(leaseOwner)
	if leaseExpiresAt.Valid {
		value := leaseExpiresAt.Time.UTC().Truncate(time.Microsecond)
		item.LeaseExpiresAt = &value
	}
	item.TerminalFrameSequence = nullInt64Pointer(terminalFrameSequence)
	if !cityRealtimeAgentDecisionRequestRecordValid(item) {
		return cityRealtimeAgentDecisionRequestRecord{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_request"})
	}
	return item, true, nil
}

func loadCityRealtimeAgentDecisionRequestForTrigger(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	agentCode string,
	triggerKey string,
	forUpdate bool,
) (cityRealtimeAgentDecisionRequestRecord, bool, error) {
	if worldID <= 0 || !cityRealtimeAgentIdentifierValid(agentCode, 96) ||
		!cityRealtimeAgentIdentifierValid(triggerKey, 96) {
		return cityRealtimeAgentDecisionRequestRecord{}, false, ErrCityInvalidInput
	}
	query := `
SELECT request.request_code
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_observations observation
  ON observation.world_id = request.world_id
 AND observation.observation_code = request.observation_code
WHERE request.world_id = $1
  AND request.agent_code = $2
  AND observation.trigger_key = $3
ORDER BY request.requested_frame_sequence DESC
LIMIT 1`
	if forUpdate {
		query += " FOR UPDATE OF request"
	}
	var requestCode string
	err := queryer.QueryRowContext(ctx, query, worldID, agentCode, triggerKey).Scan(&requestCode)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeAgentDecisionRequestRecord{}, false, nil
	}
	if err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, false, fmt.Errorf("load realtime agent decision request trigger: %w", err)
	}
	return loadCityRealtimeAgentDecisionRequest(ctx, queryer, worldID, requestCode, forUpdate)
}

func cityRealtimeAgentDecisionRequestRecordValid(item cityRealtimeAgentDecisionRequestRecord) bool {
	if !cityRealtimeAgentIdentifierValid(item.RequestCode, 96) ||
		!cityRealtimeAgentIdentifierValid(item.AgentCode, 96) ||
		!cityRealtimeAgentIdentifierValid(item.ObservationCode, 96) ||
		!cityRealtimeSHA256Hex(item.ObservationHash) ||
		!cityRealtimeSHA256Hex(item.PreconditionHash) ||
		item.ObservedFrameSequence <= 0 || item.ExpiresAtWorldTimeUS < 0 ||
		item.RequestedFrameSequence <= 0 || item.AttemptCount < 0 {
		return false
	}
	if cityRealtimeAgentDecisionRequestStatusActive(item.Status) {
		if item.Status == cityRealtimeAgentDecisionRequestQueued {
			return item.LeaseOwner == nil && item.LeaseExpiresAt == nil && item.TerminalFrameSequence == nil
		}
		return item.LeaseOwner != nil && item.LeaseExpiresAt != nil && item.TerminalFrameSequence == nil
	}
	return cityRealtimeAgentDecisionRequestStatusTerminal(item.Status) && item.LeaseOwner == nil &&
		item.LeaseExpiresAt == nil && item.TerminalFrameSequence != nil &&
		*item.TerminalFrameSequence > item.RequestedFrameSequence
}

func loadCityRealtimeAgentObservation(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	observationCode string,
) (cityRealtimeAgentObservationRecord, bool, error) {
	if worldID <= 0 || !cityRealtimeAgentIdentifierValid(observationCode, 96) {
		return cityRealtimeAgentObservationRecord{}, false, ErrCityInvalidInput
	}
	item := cityRealtimeAgentObservationRecord{}
	var rawPayload []byte
	err := queryer.QueryRowContext(ctx, `
SELECT observation_code, agent_code, observed_frame_sequence,
       observed_timeline_cursor, observed_world_time_us,
       observation_schema_version, observation_schema_hash, redaction_policy_code,
       trigger_key, payload, payload_hash, precondition_hash,
       expires_at_world_time_us, created_frame_sequence
FROM city_realtime_agent_observations
WHERE world_id = $1 AND observation_code = $2`, worldID, observationCode).Scan(
		&item.ObservationCode, &item.AgentCode, &item.ObservedFrameSequence,
		&item.ObservedTimelineCursor, &item.ObservedWorldTimeUS,
		&item.ObservationSchemaVersion, &item.ObservationSchemaHash, &item.RedactionPolicyCode,
		&item.TriggerKey, &rawPayload, &item.PayloadHash, &item.PreconditionHash,
		&item.ExpiresAtWorldTimeUS, &item.CreatedFrameSequence,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeAgentObservationRecord{}, false, nil
	}
	if err != nil {
		return cityRealtimeAgentObservationRecord{}, false, fmt.Errorf("load realtime agent observation: %w", err)
	}
	payload, payloadHash, err := cityRealtimeCanonicalJSONObjectRaw(rawPayload)
	if err != nil || payloadHash != item.PayloadHash {
		return cityRealtimeAgentObservationRecord{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_observation_payload"})
	}
	item.Payload = payload
	if !cityRealtimeAgentObservationRecordValid(item) {
		return cityRealtimeAgentObservationRecord{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_observation"})
	}
	return item, true, nil
}

func cityRealtimeAgentObservationRecordValid(item cityRealtimeAgentObservationRecord) bool {
	return cityRealtimeAgentIdentifierValid(item.ObservationCode, 96) &&
		cityRealtimeAgentIdentifierValid(item.AgentCode, 96) &&
		item.ObservedFrameSequence > 0 && item.ObservedWorldTimeUS >= 0 &&
		item.ObservationSchemaVersion == cityRealtimeAgentObservationSchemaVersion &&
		cityRealtimeSHA256Hex(item.ObservationSchemaHash) &&
		cityRealtimeAgentIdentifierValid(item.RedactionPolicyCode, 64) &&
		cityRealtimeAgentIdentifierValid(item.TriggerKey, 96) &&
		cityRealtimeSHA256Hex(item.PayloadHash) && cityRealtimeSHA256Hex(item.PreconditionHash) &&
		item.ExpiresAtWorldTimeUS >= item.ObservedWorldTimeUS && item.CreatedFrameSequence == item.ObservedFrameSequence
}

func insertCityRealtimeAgentObservation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	agentCode string,
	observedFrameSequence int64,
	observedTimelineCursor string,
	observedWorldTimeUS int64,
	triggerKey string,
	snapshot cityRealtimeAgentObservationSnapshot,
	frameSequence int64,
) error {
	if tx == nil || worldID <= 0 || observedFrameSequence <= 0 ||
		observedWorldTimeUS < 0 || frameSequence != observedFrameSequence ||
		!cityRealtimeAgentIdentifierValid(agentCode, 96) || !cityRealtimeAgentIdentifierValid(triggerKey, 96) ||
		!cityRealtimeAgentIdentifierValid(snapshot.ObservationCode, 96) ||
		!cityRealtimeSHA256Hex(snapshot.ObservationSchemaHash) || !cityRealtimeSHA256Hex(snapshot.PayloadHash) ||
		!cityRealtimeSHA256Hex(snapshot.PreconditionHash) {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_observations
    (world_id, observation_code, agent_code, observed_frame_sequence,
     observed_timeline_cursor, observed_world_time_us,
     observation_schema_version, observation_schema_hash, redaction_policy_code,
     trigger_key, payload, payload_hash, precondition_hash,
     expires_at_world_time_us, created_frame_sequence, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'self_scope_v1',
	        $9, $10::jsonb, $11, $12, $13, $14, '{}'::jsonb)`,
		worldID, snapshot.ObservationCode, agentCode, observedFrameSequence, observedTimelineCursor,
		observedWorldTimeUS, cityRealtimeAgentObservationSchemaVersion, snapshot.ObservationSchemaHash,
		triggerKey, []byte(snapshot.Payload), snapshot.PayloadHash, snapshot.PreconditionHash,
		snapshot.ExpiresAtWorldTimeUS, frameSequence,
	); err != nil {
		return fmt.Errorf("insert realtime agent observation: %w", err)
	}
	return nil
}

func insertCityRealtimeAgentDecisionRequest(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	request cityRealtimeAgentDecisionRequestRecord,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentDecisionRequestRecordValid(request) ||
		request.Status != cityRealtimeAgentDecisionRequestQueued {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_decision_requests
    (world_id, request_code, agent_code, observation_code, observation_hash,
     precondition_hash, observed_frame_sequence, expires_at_world_time_us,
     status, attempt_count, requested_frame_sequence, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'queued', 0, $9, '{}'::jsonb)`,
		worldID, request.RequestCode, request.AgentCode, request.ObservationCode,
		request.ObservationHash, request.PreconditionHash, request.ObservedFrameSequence,
		request.ExpiresAtWorldTimeUS, request.RequestedFrameSequence,
	); err != nil {
		return fmt.Errorf("insert realtime agent decision request: %w", err)
	}
	return nil
}

func insertCityRealtimeAgentDecisionOutbox(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	requestCode string,
	frameSequence int64,
) error {
	outboxCode, err := cityRealtimeAgentDecisionStableCode("aob", requestCode)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_outbox
    (world_id, outbox_code, request_code, dedup_key, status,
     created_frame_sequence, metadata)
VALUES ($1, $2, $3, $4, 'queued', $5, '{}'::jsonb)`,
		worldID, outboxCode, requestCode, "decision."+requestCode, frameSequence,
	); err != nil {
		return fmt.Errorf("insert realtime agent decision outbox: %w", err)
	}
	return nil
}

// enqueueCityRealtimeAgentDecisionInFrame is the single sealed-frame enqueue
// path used by the trusted administrative scheduler and by the A3 wakeup
// reducer.  It has no provider dependency: it records a scope-filtered
// observation and an outbox item, while a later worker owns inference.
func enqueueCityRealtimeAgentDecisionInFrame(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	observationState *lockedCityRealtimeState,
	binding cityRealtimeAgentPolicyBinding,
	agent cityRealtimeAgentInstance,
	frameSequence int64,
	cursor string,
	triggerKey string,
) (*CityRealtimeAgentDecisionRequestResult, bool, error) {
	if tx == nil || worldID <= 0 || observationState == nil || frameSequence <= 0 ||
		observationState.timelineFrameSequence != frameSequence || observationState.timelineCursor != cursor ||
		!cityRealtimeAgentDecisionRuntimeEnabled(binding) || !cityRealtimeAgentIdentifierValid(triggerKey, 96) {
		return nil, false, ErrCityInvalidInput
	}
	if existing, exists, err := loadCityRealtimeAgentDecisionRequestForTrigger(
		ctx, tx, worldID, agent.AgentCode, triggerKey, true,
	); err != nil {
		return nil, false, err
	} else if exists {
		return &CityRealtimeAgentDecisionRequestResult{
			RequestCode: existing.RequestCode, ObservationCode: existing.ObservationCode, Status: existing.Status,
		}, false, nil
	}
	pendingDecision, pendingIntent, err := cityRealtimeCharacterAgentPendingWork(ctx, tx, worldID, agent.AgentCode)
	if err != nil {
		return nil, false, err
	}
	if pendingDecision || pendingIntent {
		return nil, false, ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "active_work"})
	}
	snapshot, err := cityRealtimeAgentDecisionSnapshot(
		ctx, tx, worldID, observationState, binding, agent, triggerKey,
	)
	if err != nil {
		return nil, false, err
	}
	requestCode, err := cityRealtimeAgentDecisionStableCode(
		"adr", binding.BindingHash, agent.AgentCode, snapshot.ObservationCode, triggerKey,
	)
	if err != nil {
		return nil, false, err
	}
	if existing, exists, loadErr := loadCityRealtimeAgentDecisionRequest(ctx, tx, worldID, requestCode, true); loadErr != nil {
		return nil, false, loadErr
	} else if exists {
		if existing.AgentCode != agent.AgentCode || existing.ObservationCode != snapshot.ObservationCode ||
			existing.ObservationHash != snapshot.PayloadHash || existing.PreconditionHash != snapshot.PreconditionHash {
			return nil, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_idempotency"})
		}
		return &CityRealtimeAgentDecisionRequestResult{
			RequestCode: requestCode, ObservationCode: snapshot.ObservationCode, Status: existing.Status,
		}, false, nil
	}
	if err = enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); err != nil {
		return nil, false, err
	}
	if err = insertCityRealtimeAgentObservation(
		ctx, tx, worldID, agent.AgentCode, frameSequence, cursor, observationState.currentWorldTimeUS,
		triggerKey, snapshot, frameSequence,
	); err != nil {
		return nil, false, err
	}
	request := cityRealtimeAgentDecisionRequestRecord{
		RequestCode: requestCode, AgentCode: agent.AgentCode, ObservationCode: snapshot.ObservationCode,
		ObservationHash: snapshot.PayloadHash, PreconditionHash: snapshot.PreconditionHash,
		ObservedFrameSequence: frameSequence, ExpiresAtWorldTimeUS: snapshot.ExpiresAtWorldTimeUS,
		Status: cityRealtimeAgentDecisionRequestQueued, AttemptCount: 0,
		RequestedFrameSequence: frameSequence,
	}
	if err = insertCityRealtimeAgentDecisionRequest(ctx, tx, worldID, request); err != nil {
		return nil, false, err
	}
	if err = insertCityRealtimeAgentDecisionOutbox(ctx, tx, worldID, requestCode, frameSequence); err != nil {
		return nil, false, err
	}
	return &CityRealtimeAgentDecisionRequestResult{
		RequestCode: requestCode, ObservationCode: snapshot.ObservationCode, Status: cityRealtimeAgentDecisionRequestQueued,
	}, true, nil
}

// QueueRealtimeAgentDecision creates one scope-filtered Observation and one
// durable inference work item in a sealed frame. It remains limited to
// trusted scheduler/admin callers; A3 owner controls schedule wakeups rather
// than exposing an arbitrary browser endpoint for observations or actions.
func (s *CityEconomyService) QueueRealtimeAgentDecision(
	ctx context.Context,
	input CityRealtimeAgentDecisionRequestInput,
) (*CityRealtimeAgentDecisionRequestResult, error) {
	agentCode := strings.TrimSpace(input.AgentCode)
	triggerKey := strings.TrimSpace(input.TriggerKey)
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if input.WorldID <= 0 || !cityRealtimeAgentIdentifierValid(agentCode, 96) ||
		!cityRealtimeAgentIdentifierValid(triggerKey, 96) {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent decision request transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(input.WorldID)); err != nil {
		return nil, fmt.Errorf("lock realtime agent decision world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, 0, input.WorldID)
	if err != nil {
		return nil, err
	}
	state, err := lockCityRealtimeState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if err = validateCityRealtimeCharacterCommandWindow(world, state); err != nil {
		return nil, err
	}
	if err = cityRealtimeRejectPendingDueAtCurrentTime(ctx, tx, input.WorldID, state.currentWorldTimeUS); err != nil {
		return nil, err
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if agentState == nil || agentState.Binding == nil || !cityRealtimeAgentDecisionRuntimeEnabled(*agentState.Binding) {
		return nil, ErrCityRealtimeAgentRuntimeUnavailable
	}
	agent, found := cityRealtimeAgentDecisionAgentByCode(agentState, agentCode)
	if !found {
		return nil, ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "agent_code"})
	}
	if existing, exists, loadErr := loadCityRealtimeAgentDecisionRequestForTrigger(
		ctx, tx, input.WorldID, agent.AgentCode, triggerKey, true,
	); loadErr != nil {
		return nil, loadErr
	} else if exists {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit realtime agent decision trigger replay: %w", err)
		}
		return &CityRealtimeAgentDecisionRequestResult{
			RequestCode: existing.RequestCode, ObservationCode: existing.ObservationCode, Status: existing.Status,
		}, nil
	}
	frameSequence, cursor, err := cityRealtimeCharacterNextFrame(state)
	if err != nil {
		return nil, err
	}
	observationState := *state
	observationState.timelineFrameSequence = frameSequence
	observationState.timelineCursor = cursor
	result, inserted, enqueueErr := enqueueCityRealtimeAgentDecisionInFrame(
		ctx, tx, input.WorldID, &observationState, *agentState.Binding, agent,
		frameSequence, cursor, triggerKey,
	)
	if enqueueErr != nil {
		return nil, enqueueErr
	}
	if !inserted {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit realtime agent decision request replay: %w", err)
		}
		return result, nil
	}
	if result.Frame, err = sealCityRealtimeCharacterFrame(
		ctx, tx, input.WorldID, world, state, frameSequence, cursor, "agent.decision.requested",
		map[string]any{
			"agent_observation_created":      1,
			"agent_decision_request_created": 1,
			"agent_outbox_created":           1,
		},
	); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent decision request: %w", err)
	}
	return result, nil
}

func insertCityRealtimeAgentDecisionAttempt(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	attempt cityRealtimeAgentDecisionAttemptRecord,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(attempt.AttemptCode, 96) ||
		!cityRealtimeAgentIdentifierValid(attempt.RequestCode, 96) || attempt.AttemptNumber <= 0 ||
		attempt.ProviderCode != cityRealtimeAgentFakeProviderCode ||
		attempt.Status != cityRealtimeAgentDecisionAttemptStarted || !cityRealtimeSHA256Hex(attempt.RequestHash) {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_decision_attempts
    (world_id, attempt_code, request_code, attempt_number, provider_code,
     status, request_hash, metadata)
VALUES ($1, $2, $3, $4, $5, 'started', $6, '{}'::jsonb)`,
		worldID, attempt.AttemptCode, attempt.RequestCode, attempt.AttemptNumber,
		attempt.ProviderCode, attempt.RequestHash,
	); err != nil {
		return fmt.Errorf("insert realtime agent decision attempt: %w", err)
	}
	return nil
}

func updateCityRealtimeAgentDecisionAttemptSucceeded(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	attemptCode string,
	responseHash string,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(attemptCode, 96) || !cityRealtimeSHA256Hex(responseHash) {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_attempts
SET status = 'succeeded', response_hash = $3, completed_at = NOW(), updated_at = NOW()
WHERE world_id = $1 AND attempt_code = $2 AND status = 'started'`, worldID, attemptCode, responseHash)
	if err != nil {
		return fmt.Errorf("complete realtime agent decision attempt: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime agent decision attempt completion: %w", rowsErr)
	} else if rows != 1 {
		return ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "attempt"})
	}
	return nil
}

func loadCityRealtimeAgentDecisionAttemptForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	requestCode string,
	attemptNumber int,
) (cityRealtimeAgentDecisionAttemptRecord, bool, error) {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) || attemptNumber <= 0 {
		return cityRealtimeAgentDecisionAttemptRecord{}, false, ErrCityInvalidInput
	}
	item := cityRealtimeAgentDecisionAttemptRecord{}
	var responseHash sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT attempt_code, request_code, attempt_number, provider_code, status,
       request_hash, response_hash
FROM city_realtime_agent_decision_attempts
WHERE world_id = $1 AND request_code = $2 AND attempt_number = $3
FOR UPDATE`, worldID, requestCode, attemptNumber).Scan(
		&item.AttemptCode, &item.RequestCode, &item.AttemptNumber, &item.ProviderCode,
		&item.Status, &item.RequestHash, &responseHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeAgentDecisionAttemptRecord{}, false, nil
	}
	if err != nil {
		return cityRealtimeAgentDecisionAttemptRecord{}, false, fmt.Errorf("load realtime agent decision attempt: %w", err)
	}
	item.ResponseHash = cityRealtimeAgentNullStringPointer(responseHash)
	if !cityRealtimeAgentIdentifierValid(item.AttemptCode, 96) || item.RequestCode != requestCode ||
		item.AttemptNumber != attemptNumber || item.ProviderCode != cityRealtimeAgentFakeProviderCode ||
		(item.Status != cityRealtimeAgentDecisionAttemptStarted && item.Status != cityRealtimeAgentDecisionAttemptSucceeded && item.Status != cityRealtimeAgentDecisionAttemptFailed) ||
		!cityRealtimeSHA256Hex(item.RequestHash) || (item.ResponseHash != nil && !cityRealtimeSHA256Hex(*item.ResponseHash)) {
		return cityRealtimeAgentDecisionAttemptRecord{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_attempt"})
	}
	return item, true, nil
}

func updateCityRealtimeAgentDecisionAttemptFailed(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	attemptCode string,
	errorCode string,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(attemptCode, 96) ||
		!cityRealtimeAgentIdentifierValid(errorCode, 64) {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_attempts
SET status = 'failed', error_code = $3, completed_at = NOW(), updated_at = NOW()
WHERE world_id = $1 AND attempt_code = $2 AND status = 'started'`, worldID, attemptCode, errorCode)
	if err != nil {
		return fmt.Errorf("fail realtime agent decision attempt: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime agent decision attempt failure: %w", rowsErr)
	} else if rows != 1 {
		return ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "attempt"})
	}
	return nil
}

func updateCityRealtimeAgentDecisionRequestLease(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	requestCode string,
	workerID string,
	now time.Time,
) (cityRealtimeAgentDecisionRequestRecord, cityRealtimeAgentObservationRecord, cityRealtimeAgentDecisionAttemptRecord, error) {
	request, found, err := loadCityRealtimeAgentDecisionRequest(ctx, tx, worldID, requestCode, true)
	if err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
	}
	if !found {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, ErrCityRealtimeAgentDecisionNotFound
	}
	if request.Status == cityRealtimeAgentDecisionRequestLeased && request.LeaseExpiresAt != nil && !request.LeaseExpiresAt.After(now) {
		if err = enableCityRealtimeAgentDecisionWorkerGate(ctx, tx, worldID, requestCode); err != nil {
			return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_requests
SET status = 'queued', lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW()
WHERE world_id = $1 AND request_code = $2 AND status = 'leased'`, worldID, requestCode); err != nil {
			return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, fmt.Errorf("requeue expired realtime agent decision lease: %w", err)
		}
		request.Status = cityRealtimeAgentDecisionRequestQueued
		request.LeaseOwner = nil
		request.LeaseExpiresAt = nil
		outboxResult, outboxErr := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_outbox
SET status = 'queued', lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW()
WHERE world_id = $1 AND request_code = $2 AND status = 'leased'
  AND lease_expires_at <= $3`, worldID, requestCode, now)
		if outboxErr != nil {
			return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, fmt.Errorf("requeue expired realtime agent decision outbox lease: %w", outboxErr)
		}
		if rows, rowsErr := outboxResult.RowsAffected(); rowsErr != nil {
			return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, fmt.Errorf("check realtime agent decision outbox requeue: %w", rowsErr)
		} else if rows != 1 {
			return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_outbox_requeue"})
		}
	}
	if request.Status != cityRealtimeAgentDecisionRequestQueued {
		if cityRealtimeAgentDecisionRequestStatusTerminal(request.Status) {
			return request, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, nil
		}
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{},
			ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_lease"})
	}
	if request.AttemptCount >= cityRealtimeAgentDecisionMaximumAttempts {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{},
			errCityRealtimeAgentDecisionLeaseBudgetExhausted
	}
	observation, found, err := loadCityRealtimeAgentObservation(ctx, tx, worldID, request.ObservationCode)
	if err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
	}
	if !found || observation.AgentCode != request.AgentCode || observation.PayloadHash != request.ObservationHash ||
		observation.PreconditionHash != request.PreconditionHash || observation.ObservedFrameSequence != request.ObservedFrameSequence {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_request_observation"})
	}
	if err = enableCityRealtimeAgentDecisionWorkerGate(ctx, tx, worldID, requestCode); err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
	}
	nextAttempt := request.AttemptCount + 1
	leaseExpiry := now.Add(cityRealtimeAgentDecisionLeaseDuration).UTC().Truncate(time.Microsecond)
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_requests
SET status = 'leased', attempt_count = $4, lease_owner = $5,
    lease_expires_at = $6, updated_at = NOW()
WHERE world_id = $1 AND request_code = $2 AND status = 'queued' AND attempt_count = $3`,
		worldID, requestCode, request.AttemptCount, nextAttempt, workerID, leaseExpiry,
	)
	if err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, fmt.Errorf("lease realtime agent decision request: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, fmt.Errorf("check realtime agent decision lease: %w", rowsErr)
	} else if rows != 1 {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{},
			ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_lease"})
	}
	requestHashPayload := map[string]any{
		"schema_version":    cityRealtimeAgentDecisionEnvelopeVersion,
		"request_code":      requestCode,
		"observation_hash":  observation.PayloadHash,
		"precondition_hash": observation.PreconditionHash,
		"provider_code":     cityRealtimeAgentFakeProviderCode,
		"attempt_number":    nextAttempt,
	}
	_, requestHash, err := cityRealtimeCanonicalJSONObject(requestHashPayload)
	if err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
	}
	attemptCode, err := cityRealtimeAgentDecisionStableCode("aat", requestCode, strconv.Itoa(nextAttempt), requestHash)
	if err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
	}
	attempt := cityRealtimeAgentDecisionAttemptRecord{
		AttemptCode: attemptCode, RequestCode: requestCode, AttemptNumber: nextAttempt,
		ProviderCode: cityRealtimeAgentFakeProviderCode, Status: cityRealtimeAgentDecisionAttemptStarted,
		RequestHash: requestHash,
	}
	if err = insertCityRealtimeAgentDecisionAttempt(ctx, tx, worldID, attempt); err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
	}
	outboxResult, outboxErr := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_outbox
SET status = 'leased', lease_owner = $3, lease_expires_at = $4, updated_at = NOW()
WHERE world_id = $1 AND request_code = $2 AND status = 'queued'`,
		worldID, requestCode, workerID, leaseExpiry,
	)
	if outboxErr != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, fmt.Errorf("lease realtime agent decision outbox: %w", outboxErr)
	}
	if rows, rowsErr := outboxResult.RowsAffected(); rowsErr != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, fmt.Errorf("check realtime agent decision outbox lease: %w", rowsErr)
	} else if rows != 1 {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_outbox_lease"})
	}
	request.AttemptCount = nextAttempt
	request.Status = cityRealtimeAgentDecisionRequestLeased
	request.LeaseOwner = &workerID
	request.LeaseExpiresAt = &leaseExpiry
	return request, observation, attempt, nil
}

func cityRealtimeAgentFakeDecisionEnvelope(
	request cityRealtimeAgentDecisionRequestRecord,
	observation cityRealtimeAgentObservationRecord,
	preferredAction string,
) (cityRealtimeAgentDecisionEnvelope, error) {
	envelope := cityRealtimeAgentDecisionEnvelope{
		SchemaVersion:    cityRealtimeAgentDecisionEnvelopeVersion,
		RequestCode:      request.RequestCode,
		ObservationHash:  observation.PayloadHash,
		PreconditionHash: observation.PreconditionHash,
		Intent: cityRealtimeAgentEnvelopeIntent{
			ActionCode: cityRealtimeAgentIntentActionWait,
			Arguments:  map[string]any{},
		},
		ReasonCode: "fake_provider_wait",
	}
	// The deterministic provider is a test/runtime adapter, not an authority.
	// It may choose only a server-published finite candidate and still has to
	// pass the envelope, precondition and due-event reducers below.
	var snapshot struct {
		AllowedActions []string `json:"allowed_actions"`
		Character      struct {
			AvailableActivityCodes []string                                `json:"available_activity_codes"`
			ActionContext          *cityRealtimeAgentDecisionActionContext `json:"action_context"`
		} `json:"character"`
	}
	if err := json.Unmarshal(observation.Payload, &snapshot); err != nil {
		return cityRealtimeAgentDecisionEnvelope{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_fake_observation"}).WithCause(err)
	}
	if snapshot.Character.ActionContext != nil && !cityRealtimeAgentDecisionActionContextValid(*snapshot.Character.ActionContext) {
		return cityRealtimeAgentDecisionEnvelope{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_fake_action_context"})
	}
	choose := func(actionCode string) (map[string]any, string, bool) {
		if !cityRealtimeAgentFakeAllowedAction(snapshot.AllowedActions, actionCode) {
			return nil, "", false
		}
		if actionCode == cityRealtimeAgentIntentActionWait {
			return map[string]any{}, "fake_provider_wait", true
		}

		activityCodes := snapshot.Character.AvailableActivityCodes
		if snapshot.Character.ActionContext != nil {
			activityCodes = snapshot.Character.ActionContext.AvailableActivityCodes
		}
		switch actionCode {
		case cityRealtimeAgentIntentActionActivity:
			for _, candidate := range []string{"work.civic_shift", "civic.cleanup", "consume.ration", "rest.short"} {
				if cityRealtimeAgentFakeAvailableActivity(activityCodes, candidate) {
					return map[string]any{"activity_code": candidate}, "fake_provider_activity", true
				}
			}
			if len(activityCodes) > 0 {
				codes := append([]string(nil), activityCodes...)
				sort.Strings(codes)
				return map[string]any{"activity_code": codes[0]}, "fake_provider_activity", true
			}
		case cityRealtimeAgentIntentActionPortal:
			if snapshot.Character.ActionContext != nil && len(snapshot.Character.ActionContext.AvailablePortalCodes) > 0 {
				return map[string]any{"portal_code": snapshot.Character.ActionContext.AvailablePortalCodes[0]}, "fake_provider_portal", true
			}
		case cityRealtimeAgentIntentActionRole:
			if snapshot.Character.ActionContext != nil && len(snapshot.Character.ActionContext.AvailableRoleCodes) > 0 {
				return map[string]any{"role_code": snapshot.Character.ActionContext.AvailableRoleCodes[0]}, "fake_provider_role", true
			}
		case cityRealtimeAgentIntentActionMove:
			if snapshot.Character.ActionContext != nil && len(snapshot.Character.ActionContext.AvailableMoveTargets) > 0 {
				target := snapshot.Character.ActionContext.AvailableMoveTargets[0]
				return map[string]any{"x": target.X, "y": target.Y, "z": target.Z}, "fake_provider_move", true
			}
		}
		return nil, "", false
	}
	if preferredAction != "" {
		arguments, reasonCode, available := choose(preferredAction)
		if !available {
			return cityRealtimeAgentDecisionEnvelope{},
				ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "fake_preferred_action"})
		}
		envelope.Intent.ActionCode = preferredAction
		envelope.Intent.Arguments = arguments
		envelope.ReasonCode = reasonCode
		return envelope, nil
	}
	for _, actionCode := range []string{
		cityRealtimeAgentIntentActionActivity,
		cityRealtimeAgentIntentActionPortal,
		cityRealtimeAgentIntentActionRole,
		cityRealtimeAgentIntentActionMove,
		cityRealtimeAgentIntentActionWait,
	} {
		arguments, reasonCode, available := choose(actionCode)
		if !available {
			continue
		}
		envelope.Intent.ActionCode = actionCode
		envelope.Intent.Arguments = arguments
		envelope.ReasonCode = reasonCode
		break
	}
	return envelope, nil
}

func cityRealtimeAgentFakeAllowedAction(actions []string, action string) bool {
	for _, item := range actions {
		if item == action {
			return true
		}
	}
	return false
}

func cityRealtimeAgentFakeAvailableActivity(codes []string, candidate string) bool {
	for _, code := range codes {
		if code == candidate {
			return true
		}
	}
	return false
}

func cityRealtimeAgentFakePreferredActionValid(actionCode string) bool {
	switch actionCode {
	case "",
		cityRealtimeAgentIntentActionWait,
		cityRealtimeAgentIntentActionActivity,
		cityRealtimeAgentIntentActionMove,
		cityRealtimeAgentIntentActionPortal,
		cityRealtimeAgentIntentActionRole:
		return true
	default:
		return false
	}
}

func cityRealtimeAgentDecisionActivityCodeFromArguments(arguments map[string]any) (string, error) {
	if len(arguments) != 1 {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	rawCode, exists := arguments["activity_code"]
	code, ok := rawCode.(string)
	code = strings.TrimSpace(code)
	if !exists || !ok || !cityRealtimeAgentIdentifierValid(code, 64) {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	return code, nil
}

func cityRealtimeAgentDecisionActivityCodeFromRawArguments(arguments json.RawMessage) (string, error) {
	decoded := make(map[string]any)
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_arguments"}).WithCause(err)
	}
	return cityRealtimeAgentDecisionActivityCodeFromArguments(decoded)
}

func validateCityRealtimeAgentDecisionEnvelope(
	binding cityRealtimeAgentPolicyBinding,
	agent cityRealtimeAgentInstance,
	request cityRealtimeAgentDecisionRequestRecord,
	envelope cityRealtimeAgentDecisionEnvelope,
) (json.RawMessage, string, error) {
	if !cityRealtimeAgentDecisionRuntimeEnabled(binding) ||
		envelope.SchemaVersion != cityRealtimeAgentDecisionEnvelopeVersion ||
		envelope.RequestCode != request.RequestCode ||
		envelope.ObservationHash != request.ObservationHash ||
		envelope.PreconditionHash != request.PreconditionHash ||
		!cityRealtimeAgentIdentifierValid(envelope.Intent.ActionCode, 64) ||
		!cityRealtimeAgentIdentifierValid(envelope.ReasonCode, 64) ||
		envelope.Intent.Arguments == nil || !cityRealtimeAgentDecisionActionAllowed(binding, agent, envelope.Intent.ActionCode) {
		return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "decision_envelope"})
	}
	switch envelope.Intent.ActionCode {
	case cityRealtimeAgentIntentActionWait:
		if len(envelope.Intent.Arguments) != 0 {
			return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
	case cityRealtimeAgentIntentActionActivity:
		if !cityRealtimeAgentCharacterControlRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" || agent.ActorCode == nil {
			return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
		if _, err := cityRealtimeAgentDecisionActivityCodeFromArguments(envelope.Intent.Arguments); err != nil {
			return nil, "", err
		}
	case cityRealtimeAgentIntentActionMove:
		if !cityRealtimeAgentCharacterActionRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" || agent.ActorCode == nil {
			return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
		if _, err := cityRealtimeAgentDecisionMoveTargetFromArguments(envelope.Intent.Arguments); err != nil {
			return nil, "", err
		}
	case cityRealtimeAgentIntentActionPortal:
		if !cityRealtimeAgentCharacterActionRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" || agent.ActorCode == nil {
			return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
		if _, err := cityRealtimeAgentDecisionPortalCodeFromArguments(envelope.Intent.Arguments); err != nil {
			return nil, "", err
		}
	case cityRealtimeAgentIntentActionRole:
		if !cityRealtimeAgentCharacterActionRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" || agent.ActorCode == nil {
			return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
		if _, err := cityRealtimeAgentDecisionRoleCodeFromArguments(envelope.Intent.Arguments); err != nil {
			return nil, "", err
		}
	default:
		return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	arguments, argumentsHash, err := cityRealtimeCanonicalJSONObject(envelope.Intent.Arguments)
	if err != nil {
		return nil, "", ErrCityInvalidInput.WithCause(err)
	}
	return arguments, argumentsHash, nil
}

func cityRealtimeAgentDecisionRecordHash(record cityRealtimeAgentDecisionRecord) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":          cityRealtimeAgentDecisionStateSchemaVersion,
		"decision_code":           record.DecisionCode,
		"request_code":            record.RequestCode,
		"attempt_code":            record.AttemptCode,
		"decision_status":         record.DecisionStatus,
		"action_code":             record.ActionCode,
		"arguments_hash":          record.ArgumentsHash,
		"observation_hash":        record.ObservationHash,
		"precondition_hash":       record.PreconditionHash,
		"reason_code":             record.ReasonCode,
		"intent_code":             record.IntentCode,
		"resolved_frame_sequence": record.ResolvedFrameSequence,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime agent decision: %w", err)
	}
	return hash, nil
}

func cityRealtimeAgentIntentRecordHash(record cityRealtimeAgentIntentRecord) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":               cityRealtimeAgentDecisionStateSchemaVersion,
		"intent_code":                  record.IntentCode,
		"decision_code":                record.DecisionCode,
		"agent_code":                   record.AgentCode,
		"actor_code":                   record.ActorCode,
		"action_code":                  record.ActionCode,
		"arguments_hash":               record.ArgumentsHash,
		"precondition_hash":            record.PreconditionHash,
		"execute_after_frame_sequence": record.ExecuteAfterFrameSequence,
		"execute_at_world_time_us":     record.ExecuteAtWorldTimeUS,
		"scheduled_frame_sequence":     record.ScheduledFrameSequence,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime agent intent: %w", err)
	}
	return hash, nil
}

func insertCityRealtimeAgentDecision(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	record cityRealtimeAgentDecisionRecord,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(record.DecisionCode, 96) ||
		!cityRealtimeAgentIdentifierValid(record.RequestCode, 96) || !cityRealtimeAgentIdentifierValid(record.AttemptCode, 96) ||
		!cityRealtimeAgentIdentifierValid(record.ActionCode, 64) || !cityRealtimeSHA256Hex(record.ArgumentsHash) ||
		!cityRealtimeSHA256Hex(record.ObservationHash) || !cityRealtimeSHA256Hex(record.PreconditionHash) ||
		!cityRealtimeAgentIdentifierValid(record.ReasonCode, 64) || record.ResolvedFrameSequence <= 0 ||
		!cityRealtimeSHA256Hex(record.DecisionHash) {
		return ErrCityInvalidInput
	}
	if record.IntentCode != nil && !cityRealtimeAgentIdentifierValid(*record.IntentCode, 96) {
		return ErrCityInvalidInput
	}
	if record.DecisionStatus != cityRealtimeAgentDecisionAccepted &&
		record.DecisionStatus != cityRealtimeAgentDecisionRejected && record.DecisionStatus != cityRealtimeAgentDecisionStale {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_decisions
    (world_id, decision_code, request_code, attempt_code, decision_index,
     decision_status, action_code, arguments, arguments_hash, observation_hash,
     precondition_hash, reason_code, intent_code, resolved_frame_sequence,
     decision_hash, metadata)
VALUES ($1, $2, $3, $4, 0, $5, $6, $7::jsonb, $8, $9, $10, $11, $12,
        $13, $14, '{}'::jsonb)`,
		worldID, record.DecisionCode, record.RequestCode, record.AttemptCode,
		record.DecisionStatus, record.ActionCode, []byte(record.Arguments), record.ArgumentsHash,
		record.ObservationHash, record.PreconditionHash, record.ReasonCode, record.IntentCode,
		record.ResolvedFrameSequence, record.DecisionHash,
	); err != nil {
		return fmt.Errorf("insert realtime agent decision: %w", err)
	}
	return nil
}

func insertCityRealtimeAgentIntent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	record cityRealtimeAgentIntentRecord,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(record.IntentCode, 96) ||
		!cityRealtimeAgentIdentifierValid(record.DecisionCode, 96) || !cityRealtimeAgentIdentifierValid(record.AgentCode, 96) ||
		!cityRealtimeAgentIdentifierValid(record.ActionCode, 64) || !cityRealtimeSHA256Hex(record.ArgumentsHash) ||
		!cityRealtimeSHA256Hex(record.PreconditionHash) || record.ExecuteAfterFrameSequence <= 0 ||
		record.ExecuteAtWorldTimeUS < 0 || record.ScheduledFrameSequence <= 0 ||
		record.Status != cityRealtimeAgentIntentPending || !cityRealtimeSHA256Hex(record.IntentHash) {
		return ErrCityInvalidInput
	}
	if record.ActorCode != nil && !cityRealtimeAgentIdentifierValid(*record.ActorCode, 96) {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_intents
    (world_id, intent_code, decision_code, agent_code, actor_code,
     action_code, arguments, arguments_hash, precondition_hash,
     execute_after_frame_sequence, execute_at_world_time_us, status,
     scheduled_frame_sequence, intent_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, 'pending',
        $12, $13, '{}'::jsonb)`,
		worldID, record.IntentCode, record.DecisionCode, record.AgentCode, record.ActorCode,
		record.ActionCode, []byte(record.Arguments), record.ArgumentsHash, record.PreconditionHash,
		record.ExecuteAfterFrameSequence, record.ExecuteAtWorldTimeUS,
		record.ScheduledFrameSequence, record.IntentHash,
	); err != nil {
		return fmt.Errorf("insert realtime agent intent: %w", err)
	}
	return nil
}

func updateCityRealtimeAgentDecisionRequestTerminal(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	requestCode string,
	status string,
	frameSequence int64,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) ||
		!cityRealtimeAgentDecisionRequestStatusTerminal(status) || frameSequence <= 0 {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_requests
SET status = $3, lease_owner = NULL, lease_expires_at = NULL,
    terminal_frame_sequence = $4, updated_at = NOW()
WHERE world_id = $1 AND request_code = $2 AND status = 'leased'`,
		worldID, requestCode, status, frameSequence,
	)
	if err != nil {
		return fmt.Errorf("complete realtime agent decision request: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime agent decision request completion: %w", rowsErr)
	} else if rows != 1 {
		return ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_terminal"})
	}
	return nil
}

func updateCityRealtimeAgentDecisionOutboxTerminal(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	requestCode string,
	status string,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) ||
		(status != cityRealtimeAgentOutboxSucceeded && status != cityRealtimeAgentOutboxFailed) {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_outbox
SET status = $3, lease_owner = NULL, lease_expires_at = NULL,
    completed_at = NOW(), updated_at = NOW()
WHERE world_id = $1 AND request_code = $2 AND status = 'leased'`, worldID, requestCode, status)
	if err != nil {
		return fmt.Errorf("complete realtime agent decision outbox: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime agent decision outbox completion: %w", rowsErr)
	} else if rows != 1 {
		return ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "outbox_terminal"})
	}
	return nil
}

func scheduleCityRealtimeAgentIntentDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	intent cityRealtimeAgentIntentRecord,
	frameSequence int64,
) error {
	payload, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version": 1,
		"intent_code":    intent.IntentCode,
	})
	if err != nil {
		return err
	}
	dedupKey := "agent-intent." + intent.IntentCode
	if !cityRealtimeDueEventIdentifierValid(dedupKey, 160) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_dedup"})
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_due_events
    (world_id, event_type, schema_version, due_world_time_us, temporal_phase,
     priority, aggregate_type, aggregate_key, dedup_key, source_kind,
     source_reference, payload, payload_hash, expected_version, status,
     created_frame_sequence)
VALUES ($1, $2, 1, $3, 'activity', 0, 'realtime_agent', $4, $5, 'agent',
        'realtime_agent_runtime', $6::jsonb, $7, $8, 'pending', $9)`,
		worldID, cityRealtimeDueEventTypeAgentIntent, intent.ExecuteAtWorldTimeUS,
		intent.AgentCode, dedupKey, []byte(payload), payloadHash,
		intent.ExecuteAfterFrameSequence, frameSequence,
	); err != nil {
		return fmt.Errorf("schedule realtime agent intent due event: %w", err)
	}
	return nil
}

type cityRealtimeAgentDecisionWakeupDuePayload struct {
	SchemaVersion int    `json:"schema_version"`
	AgentCode     string `json:"agent_code"`
}

// scheduleCityRealtimeAgentDecisionWakeup creates a server-owned future
// opportunity to observe one autonomous Character Agent.  It deliberately
// creates no model work itself; the due-event reducer will revalidate the
// current control/personality state before enqueuing an Observation.
func scheduleCityRealtimeAgentDecisionWakeup(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	agentCode string,
	dueWorldTimeUS int64,
	frameSequence int64,
) (bool, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 || dueWorldTimeUS < 0 ||
		dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 || !cityRealtimeAgentIdentifierValid(agentCode, 96) {
		return false, ErrCityInvalidInput
	}
	triggerKey, err := cityRealtimeCharacterAgentWakeupTrigger(agentCode, dueWorldTimeUS)
	if err != nil {
		return false, err
	}
	payload, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version": 1,
		"agent_code":     agentCode,
	})
	if err != nil {
		return false, err
	}
	dedupKey := "agent-wakeup." + triggerKey
	if !cityRealtimeDueEventIdentifierValid(dedupKey, 160) {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_wakeup_dedup"})
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO city_due_events
    (world_id, event_type, schema_version, due_world_time_us, temporal_phase,
     priority, aggregate_type, aggregate_key, dedup_key, source_kind,
     source_reference, payload, payload_hash, expected_version, status,
     created_frame_sequence)
VALUES ($1, $2, 1, $3, 'activity', -1, 'realtime_agent', $4, $5, 'agent',
        'realtime_agent_runtime', $6::jsonb, $7, $8, 'pending', $9)
ON CONFLICT (world_id, dedup_key) DO NOTHING`,
		worldID, cityRealtimeDueEventTypeAgentDecisionWakeup, dueWorldTimeUS,
		agentCode, dedupKey, []byte(payload), payloadHash, frameSequence, frameSequence,
	)
	if err != nil {
		return false, fmt.Errorf("schedule realtime Agent decision wakeup: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count realtime Agent decision wakeup: %w", err)
	}
	return rows == 1, nil
}

func cityRealtimeAgentDecisionTerminalStatusForPrecondition(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	state *lockedCityRealtimeState,
	binding cityRealtimeAgentPolicyBinding,
	agent cityRealtimeAgentInstance,
	request cityRealtimeAgentDecisionRequestRecord,
	actionCode string,
	reasonCode string,
) (string, string, error) {
	if state == nil || state.currentWorldTimeUS > request.ExpiresAtWorldTimeUS {
		return cityRealtimeAgentDecisionRequestStale, "observation_expired", nil
	}
	if !cityRealtimeAgentDecisionRuntimeEnabled(binding) || !cityRealtimeAgentDecisionActionAllowed(binding, agent, actionCode) {
		return cityRealtimeAgentDecisionRequestStale, "agent_unavailable", nil
	}
	currentPreconditionHash, err := cityRealtimeAgentDecisionCurrentPreconditionHash(
		ctx, queryer, worldID, state.currentWorldTimeUS, binding, agent,
	)
	if err != nil {
		return "", "", err
	}
	if currentPreconditionHash != request.PreconditionHash {
		return cityRealtimeAgentDecisionRequestStale, "precondition_changed", nil
	}
	return cityRealtimeAgentDecisionRequestAccepted, reasonCode, nil
}

func (s *CityEconomyService) finalizeRealtimeAgentFakeDecision(
	ctx context.Context,
	worldID int64,
	workerID string,
	requestCode string,
	attempt cityRealtimeAgentDecisionAttemptRecord,
	envelope cityRealtimeAgentDecisionEnvelope,
) (*CityRealtimeAgentFakeDecisionRunResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent decision finalize transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(worldID)); err != nil {
		return nil, fmt.Errorf("lock realtime agent decision finalize world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, 0, worldID)
	if err != nil {
		return nil, err
	}
	state, err := lockCityRealtimeState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if err = validateCityRealtimeCharacterCommandWindow(world, state); err != nil {
		return nil, err
	}
	if err = cityRealtimeRejectPendingDueAtCurrentTime(ctx, tx, worldID, state.currentWorldTimeUS); err != nil {
		return nil, err
	}
	request, found, err := loadCityRealtimeAgentDecisionRequest(ctx, tx, worldID, requestCode, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeAgentDecisionNotFound
	}
	if request.Status != cityRealtimeAgentDecisionRequestLeased || request.LeaseOwner == nil || *request.LeaseOwner != workerID {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_lease"})
	}
	if request.LeaseExpiresAt == nil || !request.LeaseExpiresAt.After(time.Now().UTC()) {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_lease_expired"})
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if agentState == nil || agentState.Binding == nil || !cityRealtimeAgentDecisionRuntimeEnabled(*agentState.Binding) {
		return nil, ErrCityRealtimeAgentRuntimeUnavailable
	}
	agent, found := cityRealtimeAgentDecisionAgentByCode(agentState, request.AgentCode)
	if !found {
		return nil, ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "agent_code"})
	}
	if _, _, err = validateCityRealtimeAgentDecisionEnvelope(*agentState.Binding, agent, request, envelope); err != nil {
		return nil, err
	}
	if cityRealtimeAgentCharacterActionRuntimeEnabled(*agentState.Binding) && agent.AgentSubtype == "character.user" {
		observation, observationFound, observationErr := loadCityRealtimeAgentObservation(
			ctx, tx, worldID, request.ObservationCode,
		)
		if observationErr != nil {
			return nil, observationErr
		}
		if !observationFound || observation.AgentCode != request.AgentCode ||
			observation.PayloadHash != request.ObservationHash || observation.PreconditionHash != request.PreconditionHash ||
			observation.ObservedFrameSequence != request.ObservedFrameSequence {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_observation"})
		}
		if observationErr = cityRealtimeAgentDecisionValidatePublishedAction(
			*agentState.Binding, agent, observation, envelope.Intent.ActionCode, envelope.Intent.Arguments,
		); observationErr != nil {
			return nil, observationErr
		}
	}
	frameSequence, cursor, err := cityRealtimeCharacterNextFrame(state)
	if err != nil {
		return nil, err
	}
	if frameSequence <= request.ObservedFrameSequence {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_temporal_order"})
	}
	if err = enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); err != nil {
		return nil, err
	}
	if err = enableCityRealtimeAgentDecisionWorkerGate(ctx, tx, worldID, requestCode); err != nil {
		return nil, err
	}
	responseRaw, responseHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":    envelope.SchemaVersion,
		"request_code":      envelope.RequestCode,
		"observation_hash":  envelope.ObservationHash,
		"precondition_hash": envelope.PreconditionHash,
		"intent": map[string]any{
			"action_code": envelope.Intent.ActionCode,
			"arguments":   envelope.Intent.Arguments,
		},
		"reason_code": envelope.ReasonCode,
	})
	if err != nil {
		return nil, err
	}
	_ = responseRaw // The hash is retained; the raw fake response is intentionally not persisted.
	if err = updateCityRealtimeAgentDecisionAttemptSucceeded(ctx, tx, worldID, attempt.AttemptCode, responseHash); err != nil {
		return nil, err
	}
	requestStatus, reasonCode, err := cityRealtimeAgentDecisionTerminalStatusForPrecondition(
		ctx, tx, worldID, state, *agentState.Binding, agent, request,
		envelope.Intent.ActionCode, envelope.ReasonCode,
	)
	if err != nil {
		return nil, err
	}
	arguments, argumentsHash, err := cityRealtimeCanonicalJSONObject(envelope.Intent.Arguments)
	if err != nil {
		return nil, err
	}
	decisionStatus := cityRealtimeAgentDecisionAccepted
	if requestStatus == cityRealtimeAgentDecisionRequestStale {
		decisionStatus = cityRealtimeAgentDecisionStale
	}
	decisionCode, err := cityRealtimeAgentDecisionStableCode(
		"add", requestCode, attempt.AttemptCode, decisionStatus, envelope.Intent.ActionCode, argumentsHash, reasonCode,
	)
	if err != nil {
		return nil, err
	}
	var intent *cityRealtimeAgentIntentRecord
	var intentCode *string
	if requestStatus == cityRealtimeAgentDecisionRequestAccepted {
		if state.currentWorldTimeUS > cityRealtimeMaximumWorldTimeUS-cityRealtimeTimeQuantumUS {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_time"})
		}
		code, codeErr := cityRealtimeAgentDecisionStableCode("ait", decisionCode, agent.AgentCode, envelope.Intent.ActionCode, argumentsHash)
		if codeErr != nil {
			return nil, codeErr
		}
		intentCode = &code
		candidate := cityRealtimeAgentIntentRecord{
			IntentCode: code, DecisionCode: decisionCode, AgentCode: agent.AgentCode, ActorCode: agent.ActorCode,
			ActionCode: envelope.Intent.ActionCode, Arguments: arguments, ArgumentsHash: argumentsHash,
			PreconditionHash: request.PreconditionHash, ExecuteAfterFrameSequence: frameSequence,
			ExecuteAtWorldTimeUS: state.currentWorldTimeUS + cityRealtimeTimeQuantumUS,
			Status:               cityRealtimeAgentIntentPending, ScheduledFrameSequence: frameSequence,
		}
		candidate.IntentHash, err = cityRealtimeAgentIntentRecordHash(candidate)
		if err != nil {
			return nil, err
		}
		intent = &candidate
	}
	decision := cityRealtimeAgentDecisionRecord{
		DecisionCode: decisionCode, RequestCode: requestCode, AttemptCode: attempt.AttemptCode,
		DecisionStatus: decisionStatus, ActionCode: envelope.Intent.ActionCode, Arguments: arguments,
		ArgumentsHash: argumentsHash, ObservationHash: request.ObservationHash,
		PreconditionHash: request.PreconditionHash, ReasonCode: reasonCode, IntentCode: intentCode,
		ResolvedFrameSequence: frameSequence,
	}
	decision.DecisionHash, err = cityRealtimeAgentDecisionRecordHash(decision)
	if err != nil {
		return nil, err
	}
	if err = insertCityRealtimeAgentDecision(ctx, tx, worldID, decision); err != nil {
		return nil, err
	}
	if intent != nil {
		if err = insertCityRealtimeAgentIntent(ctx, tx, worldID, *intent); err != nil {
			return nil, err
		}
		if err = scheduleCityRealtimeAgentIntentDueEvent(ctx, tx, worldID, *intent, frameSequence); err != nil {
			return nil, err
		}
		state.nextDueAtWorldTimeUS, err = cityRealtimeNextPendingDue(ctx, tx, worldID)
		if err != nil || state.nextDueAtWorldTimeUS == nil {
			if err == nil {
				err = ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_due"})
			}
			return nil, err
		}
	}
	if err = updateCityRealtimeAgentDecisionRequestTerminal(ctx, tx, worldID, requestCode, requestStatus, frameSequence); err != nil {
		return nil, err
	}
	if err = updateCityRealtimeAgentDecisionOutboxTerminal(ctx, tx, worldID, requestCode, cityRealtimeAgentOutboxSucceeded); err != nil {
		return nil, err
	}
	result := &CityRealtimeAgentFakeDecisionRunResult{
		RequestCode: requestCode, DecisionCode: decisionCode, Status: requestStatus,
	}
	if intent != nil {
		result.IntentCode = intent.IntentCode
	}
	if result.Frame, err = sealCityRealtimeCharacterFrame(
		ctx, tx, worldID, world, state, frameSequence, cursor, "agent.decision.resolved",
		map[string]any{
			"agent_decision_resolved": 1,
			"agent_decision_accepted": boolToCityRealtimeCount(intent != nil),
			"agent_decision_stale":    boolToCityRealtimeCount(intent == nil),
			"agent_intent_scheduled":  boolToCityRealtimeCount(intent != nil),
		},
	); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent decision finalize: %w", err)
	}
	return result, nil
}

// finalizeRealtimeAgentDecisionLeaseBudget seals the one terminal path that
// can be reached without a provider result: an already-started worker attempt
// expired after the bounded retry budget. It does not invent a decision or
// intent. The attempt failure plus sealed request/outbox terminal frame is the
// durable audit record, and removes the unresolved work item from canonical
// state without ever letting a stale worker mutate the city.
func (s *CityEconomyService) finalizeRealtimeAgentDecisionLeaseBudget(
	ctx context.Context,
	worldID int64,
	requestCode string,
) (*CityRealtimeAgentFakeDecisionRunResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent decision lease-budget transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(worldID)); err != nil {
		return nil, fmt.Errorf("lock realtime agent decision lease-budget world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, 0, worldID)
	if err != nil {
		return nil, err
	}
	state, err := lockCityRealtimeState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if err = validateCityRealtimeCharacterCommandWindow(world, state); err != nil {
		return nil, err
	}
	if err = cityRealtimeRejectPendingDueAtCurrentTime(ctx, tx, worldID, state.currentWorldTimeUS); err != nil {
		return nil, err
	}
	request, found, err := loadCityRealtimeAgentDecisionRequest(ctx, tx, worldID, requestCode, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeAgentDecisionNotFound
	}
	now := time.Now().UTC()
	if request.Status != cityRealtimeAgentDecisionRequestLeased || request.LeaseExpiresAt == nil ||
		request.LeaseExpiresAt.After(now) || request.AttemptCount < cityRealtimeAgentDecisionMaximumAttempts {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_lease_budget"})
	}
	attempt, found, err := loadCityRealtimeAgentDecisionAttemptForUpdate(
		ctx, tx, worldID, requestCode, request.AttemptCount,
	)
	if err != nil {
		return nil, err
	}
	if !found || attempt.Status != cityRealtimeAgentDecisionAttemptStarted {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_budget_attempt"})
	}
	frameSequence, cursor, err := cityRealtimeCharacterNextFrame(state)
	if err != nil {
		return nil, err
	}
	if err = enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); err != nil {
		return nil, err
	}
	if err = enableCityRealtimeAgentDecisionWorkerGate(ctx, tx, worldID, requestCode); err != nil {
		return nil, err
	}
	if err = updateCityRealtimeAgentDecisionAttemptFailed(ctx, tx, worldID, attempt.AttemptCode, "lease_expired"); err != nil {
		return nil, err
	}
	if err = updateCityRealtimeAgentDecisionRequestTerminal(
		ctx, tx, worldID, requestCode, cityRealtimeAgentDecisionRequestFailed, frameSequence,
	); err != nil {
		return nil, err
	}
	if err = updateCityRealtimeAgentDecisionOutboxTerminal(
		ctx, tx, worldID, requestCode, cityRealtimeAgentOutboxFailed,
	); err != nil {
		return nil, err
	}
	result := &CityRealtimeAgentFakeDecisionRunResult{
		RequestCode: requestCode,
		Status:      cityRealtimeAgentDecisionRequestFailed,
	}
	if result.Frame, err = sealCityRealtimeCharacterFrame(
		ctx, tx, worldID, world, state, frameSequence, cursor, "agent.decision.failed",
		map[string]any{
			"agent_decision_failed":         1,
			"agent_decision_attempt_failed": 1,
			"agent_intent_scheduled":        0,
		},
	); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent decision lease-budget: %w", err)
	}
	return result, nil
}

// RunRealtimeAgentFakeDecision consumes exactly one request through the same
// durable worker boundary future model routes will use. It deliberately has no
// implicit loop or HTTP route: administrators/tests invoke it explicitly while
// A2 validates causality, idempotency, and failure isolation.
func (s *CityEconomyService) RunRealtimeAgentFakeDecision(
	ctx context.Context,
	input CityRealtimeAgentFakeDecisionRunInput,
) (*CityRealtimeAgentFakeDecisionRunResult, error) {
	requestCode := strings.TrimSpace(input.RequestCode)
	workerID := strings.TrimSpace(input.WorkerID)
	preferredAction := strings.TrimSpace(input.PreferredAction)
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if input.WorldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) ||
		!cityRealtimeAgentIdentifierValid(workerID, 64) || !cityRealtimeAgentFakePreferredActionValid(preferredAction) {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent fake decision lease transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(input.WorldID)); err != nil {
		return nil, fmt.Errorf("lock realtime agent fake decision world: %w", err)
	}
	request, observation, attempt, err := updateCityRealtimeAgentDecisionRequestLease(
		ctx, tx, input.WorldID, requestCode, workerID, time.Now().UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		if errors.Is(err, errCityRealtimeAgentDecisionLeaseBudgetExhausted) {
			_ = tx.Rollback()
			return s.finalizeRealtimeAgentDecisionLeaseBudget(ctx, input.WorldID, requestCode)
		}
		return nil, err
	}
	if cityRealtimeAgentDecisionRequestStatusTerminal(request.Status) {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit realtime agent fake decision replay: %w", err)
		}
		return &CityRealtimeAgentFakeDecisionRunResult{RequestCode: requestCode, Status: request.Status}, nil
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent fake decision lease: %w", err)
	}
	// There is intentionally no transaction open while the provider would run.
	// The deterministic adapter can select only a schema-valid server-published
	// finite candidate or wait command; the finalizer still rechecks every invariant.
	envelope, envelopeErr := cityRealtimeAgentFakeDecisionEnvelope(request, observation, preferredAction)
	if envelopeErr != nil {
		return nil, envelopeErr
	}
	return s.finalizeRealtimeAgentFakeDecision(ctx, input.WorldID, workerID, requestCode, attempt, envelope)
}

type cityRealtimeAgentIntentDuePayload struct {
	SchemaVersion int    `json:"schema_version"`
	IntentCode    string `json:"intent_code"`
}

func loadCityRealtimeAgentIntentForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	intentCode string,
) (cityRealtimeAgentIntentRecord, bool, error) {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(intentCode, 96) {
		return cityRealtimeAgentIntentRecord{}, false, ErrCityInvalidInput
	}
	item := cityRealtimeAgentIntentRecord{}
	var actorCode sql.NullString
	var resolvedFrameSequence sql.NullInt64
	var rawArguments []byte
	err := tx.QueryRowContext(ctx, `
SELECT intent_code, decision_code, agent_code, actor_code, action_code,
       arguments, arguments_hash, precondition_hash, execute_after_frame_sequence,
       execute_at_world_time_us, status, scheduled_frame_sequence,
       resolved_frame_sequence, intent_hash
FROM city_realtime_agent_intents
WHERE world_id = $1 AND intent_code = $2
FOR UPDATE`, worldID, intentCode).Scan(
		&item.IntentCode, &item.DecisionCode, &item.AgentCode, &actorCode, &item.ActionCode,
		&rawArguments, &item.ArgumentsHash, &item.PreconditionHash, &item.ExecuteAfterFrameSequence,
		&item.ExecuteAtWorldTimeUS, &item.Status, &item.ScheduledFrameSequence,
		&resolvedFrameSequence, &item.IntentHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeAgentIntentRecord{}, false, nil
	}
	if err != nil {
		return cityRealtimeAgentIntentRecord{}, false, fmt.Errorf("load realtime agent intent: %w", err)
	}
	arguments, argumentsHash, err := cityRealtimeCanonicalJSONObjectRaw(rawArguments)
	if err != nil || argumentsHash != item.ArgumentsHash {
		return cityRealtimeAgentIntentRecord{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_arguments"})
	}
	item.ActorCode = cityRealtimeAgentNullStringPointer(actorCode)
	item.Arguments = arguments
	item.ResolvedFrameSequence = nullInt64Pointer(resolvedFrameSequence)
	if !cityRealtimeAgentIntentRecordValid(item) {
		return cityRealtimeAgentIntentRecord{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent"})
	}
	return item, true, nil
}

func cityRealtimeAgentIntentRecordValid(record cityRealtimeAgentIntentRecord) bool {
	if !cityRealtimeAgentIdentifierValid(record.IntentCode, 96) ||
		!cityRealtimeAgentIdentifierValid(record.DecisionCode, 96) ||
		!cityRealtimeAgentIdentifierValid(record.AgentCode, 96) ||
		!cityRealtimeAgentIdentifierValid(record.ActionCode, 64) ||
		!cityRealtimeSHA256Hex(record.ArgumentsHash) || !cityRealtimeSHA256Hex(record.PreconditionHash) ||
		!cityRealtimeSHA256Hex(record.IntentHash) || record.ExecuteAfterFrameSequence <= 0 ||
		record.ExecuteAtWorldTimeUS < 0 || record.ScheduledFrameSequence <= 0 {
		return false
	}
	if record.ActorCode != nil && !cityRealtimeAgentIdentifierValid(*record.ActorCode, 96) {
		return false
	}
	switch record.Status {
	case cityRealtimeAgentIntentPending:
		return record.ResolvedFrameSequence == nil
	case cityRealtimeAgentIntentApplied, cityRealtimeAgentIntentRejected,
		cityRealtimeAgentIntentStale, cityRealtimeAgentIntentCancelled:
		return record.ResolvedFrameSequence != nil && *record.ResolvedFrameSequence > record.ScheduledFrameSequence
	default:
		return false
	}
}

func updateCityRealtimeAgentIntentTerminal(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	intentCode string,
	status string,
	frameSequence int64,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(intentCode, 96) || frameSequence <= 0 ||
		(status != cityRealtimeAgentIntentApplied && status != cityRealtimeAgentIntentRejected &&
			status != cityRealtimeAgentIntentStale && status != cityRealtimeAgentIntentCancelled) {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_intents
SET status = $3, resolved_frame_sequence = $4, updated_at = NOW()
WHERE world_id = $1 AND intent_code = $2 AND status = 'pending'`,
		worldID, intentCode, status, frameSequence,
	)
	if err != nil {
		return fmt.Errorf("complete realtime agent intent: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime agent intent completion: %w", rowsErr)
	} else if rows != 1 {
		return ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "intent_terminal"})
	}
	return nil
}

// applyCityRealtimeAgentDecisionWakeupDueEvent consumes a future autonomous
// wakeup.  The event never executes a Character action; it merely constructs
// one fresh, scope-filtered Observation after checking the new control mode,
// current personality revision and the absence of already-active work.
func applyCityRealtimeAgentDecisionWakeupDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	event cityRealtimeDueEventRecord,
) (handled bool, applied bool, err error) {
	if event.EventType != cityRealtimeDueEventTypeAgentDecisionWakeup {
		return false, false, nil
	}
	if event.SchemaVersion != 1 || event.SourceKind != "agent" || event.TemporalPhase != "activity" ||
		event.AggregateType != "realtime_agent" || event.SourceReference != "realtime_agent_runtime" ||
		event.ExpectedVersion == nil || *event.ExpectedVersion <= 0 {
		return true, false, nil
	}
	var payload cityRealtimeAgentDecisionWakeupDuePayload
	if decodeErr := decodeStrictCityObject(event.Payload, &payload); decodeErr != nil || payload.SchemaVersion != 1 ||
		!cityRealtimeAgentIdentifierValid(payload.AgentCode, 96) || payload.AgentCode != event.AggregateKey {
		return true, false, nil
	}
	agentState, loadErr := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if loadErr != nil {
		return true, false, loadErr
	}
	if agentState == nil || agentState.Binding == nil || !cityRealtimeAgentCharacterControlRuntimeEnabled(*agentState.Binding) {
		return true, true, nil
	}
	agent, found := cityRealtimeAgentDecisionAgentByCode(agentState, payload.AgentCode)
	if !found || agent.AgentSubtype != "character.user" || agent.LifecycleStatus != "active" ||
		agent.ControlMode != "autonomous" {
		return true, true, nil
	}
	triggerKey, triggerErr := cityRealtimeCharacterAgentWakeupTrigger(agent.AgentCode, event.DueWorldTimeUS)
	if triggerErr != nil {
		return true, false, triggerErr
	}
	cursor, cursorErr := cityRealtimeTimelineCursor(frameSequence)
	if cursorErr != nil {
		return true, false, cursorErr
	}
	observationState := &lockedCityRealtimeState{
		timelineFrameSequence: frameSequence,
		timelineCursor:        cursor,
		currentWorldTimeUS:    event.DueWorldTimeUS,
	}
	_, _, enqueueErr := enqueueCityRealtimeAgentDecisionInFrame(
		ctx, tx, worldID, observationState, *agentState.Binding, agent,
		frameSequence, cursor, triggerKey,
	)
	if enqueueErr != nil {
		if errors.Is(enqueueErr, ErrCityRealtimeAgentDecisionUnavailable) {
			// A queued/leased request or pending intent already owns the next
			// action opportunity. Consuming this wakeup is therefore correct and
			// cannot produce duplicate causal work.
			return true, true, nil
		}
		return true, false, enqueueErr
	}
	return true, true, nil
}

func cityRealtimeAgentNextActivityWakeupWorldTime(
	currentWorldTimeUS, minimumIntervalUS int64,
) (int64, error) {
	if currentWorldTimeUS < 0 || minimumIntervalUS <= 0 ||
		currentWorldTimeUS > cityRealtimeMaximumWorldTimeUS-minimumIntervalUS {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_activity_wakeup"})
	}
	next := currentWorldTimeUS + minimumIntervalUS
	if remainder := next % cityRealtimeTimeQuantumUS; remainder != 0 {
		if next > cityRealtimeMaximumWorldTimeUS-(cityRealtimeTimeQuantumUS-remainder) {
			return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_activity_wakeup"})
		}
		next += cityRealtimeTimeQuantumUS - remainder
	}
	return next, nil
}

// cityRealtimeAgentScheduleAutonomousActionWakeup gives the A3.2 Character
// Agent a single deterministic continuation boundary after a non-activity
// action.  It is deliberately unavailable to the historical 1.1/1.2
// catalogues, so upgrading the executable cannot create new work in an old
// world.
func cityRealtimeAgentScheduleAutonomousActionWakeup(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence, currentWorldTimeUS, minimumIntervalUS int64,
	binding cityRealtimeAgentPolicyBinding,
	agent cityRealtimeAgentInstance,
) error {
	if tx == nil || worldID <= 0 || frameSequence <= 0 || currentWorldTimeUS < 0 || minimumIntervalUS <= 0 ||
		!cityRealtimeAgentCharacterActionRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" ||
		agent.ActorCode == nil || agent.ControlMode != "autonomous" {
		return ErrCityInvalidInput
	}
	nextWakeup, err := cityRealtimeAgentNextActivityWakeupWorldTime(currentWorldTimeUS, minimumIntervalUS)
	if err != nil {
		return err
	}
	_, err = scheduleCityRealtimeAgentDecisionWakeup(ctx, tx, worldID, agent.AgentCode, nextWakeup, frameSequence)
	return err
}

// applyCityRealtimeAgentIntentDueEvent is the only bridge from a normalized
// Agent intent into a city reducer.  A2's wait remains a no-op.  A3.1 adds the
// existing Character activity reducer under the same world-time, scope,
// precondition, role, inventory and rule checks used by manual actions.
func applyCityRealtimeAgentIntentDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	event cityRealtimeDueEventRecord,
) (handled bool, applied bool, err error) {
	if event.EventType != cityRealtimeDueEventTypeAgentIntent {
		return false, false, nil
	}
	if event.SchemaVersion != 1 || event.SourceKind != "agent" || event.TemporalPhase != "activity" ||
		event.AggregateType != "realtime_agent" || event.SourceReference != "realtime_agent_runtime" ||
		event.ExpectedVersion == nil || *event.ExpectedVersion <= 0 {
		return true, false, nil
	}
	var payload cityRealtimeAgentIntentDuePayload
	if decodeErr := decodeStrictCityObject(event.Payload, &payload); decodeErr != nil || payload.SchemaVersion != 1 ||
		!cityRealtimeAgentIdentifierValid(payload.IntentCode, 96) {
		return true, false, nil
	}
	intent, found, loadErr := loadCityRealtimeAgentIntentForUpdate(ctx, tx, worldID, payload.IntentCode)
	if loadErr != nil {
		return true, false, loadErr
	}
	if !found || intent.Status != cityRealtimeAgentIntentPending ||
		intent.ExecuteAtWorldTimeUS != event.DueWorldTimeUS ||
		intent.ExecuteAfterFrameSequence != *event.ExpectedVersion ||
		intent.ScheduledFrameSequence >= frameSequence {
		return true, false, nil
	}
	agentState, loadErr := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if loadErr != nil {
		return true, false, loadErr
	}
	if agentState == nil || agentState.Binding == nil || !cityRealtimeAgentDecisionRuntimeEnabled(*agentState.Binding) {
		return true, false, nil
	}
	agent, found := cityRealtimeAgentDecisionAgentByCode(agentState, intent.AgentCode)
	markStale := func() (bool, bool, error) {
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return true, false, gateErr
		}
		if updateErr := updateCityRealtimeAgentIntentTerminal(ctx, tx, worldID, intent.IntentCode, cityRealtimeAgentIntentStale, frameSequence); updateErr != nil {
			return true, false, updateErr
		}
		if found && cityRealtimeAgentCharacterActionRuntimeEnabled(*agentState.Binding) &&
			agent.AgentSubtype == "character.user" && agent.ActorCode != nil && agent.ControlMode == "autonomous" {
			if wakeupErr := cityRealtimeAgentScheduleAutonomousActionWakeup(
				ctx, tx, worldID, frameSequence, event.DueWorldTimeUS, cityRealtimeTimeQuantumUS, *agentState.Binding, agent,
			); wakeupErr != nil {
				return true, false, wakeupErr
			}
		}
		return true, false, nil
	}
	if !found || !cityRealtimeAgentDecisionActionAllowed(*agentState.Binding, agent, intent.ActionCode) {
		return markStale()
	}
	currentPreconditionHash, preconditionErr := cityRealtimeAgentDecisionCurrentPreconditionHash(
		ctx, tx, worldID, event.DueWorldTimeUS, *agentState.Binding, agent,
	)
	if preconditionErr != nil {
		return true, false, preconditionErr
	}
	if currentPreconditionHash != intent.PreconditionHash {
		return markStale()
	}
	switch intent.ActionCode {
	case cityRealtimeAgentIntentActionWait:
		if string(intent.Arguments) != "{}" {
			return markStale()
		}
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return true, false, gateErr
		}
		if updateErr := updateCityRealtimeAgentIntentTerminal(ctx, tx, worldID, intent.IntentCode, cityRealtimeAgentIntentApplied, frameSequence); updateErr != nil {
			return true, false, updateErr
		}
		if cityRealtimeAgentCharacterActionRuntimeEnabled(*agentState.Binding) &&
			agent.AgentSubtype == "character.user" && agent.ActorCode != nil && agent.ControlMode == "autonomous" {
			if wakeupErr := cityRealtimeAgentScheduleAutonomousActionWakeup(
				ctx, tx, worldID, frameSequence, event.DueWorldTimeUS, cityRealtimeTimeQuantumUS, *agentState.Binding, agent,
			); wakeupErr != nil {
				return true, false, wakeupErr
			}
		}
		return true, true, nil
	case cityRealtimeAgentIntentActionActivity:
		if !cityRealtimeAgentCharacterControlRuntimeEnabled(*agentState.Binding) ||
			agent.AgentSubtype != "character.user" || agent.ActorCode == nil || intent.ActorCode == nil ||
			*intent.ActorCode != *agent.ActorCode {
			return markStale()
		}
		activityCode, activityCodeErr := cityRealtimeAgentDecisionActivityCodeFromRawArguments(intent.Arguments)
		if activityCodeErr != nil {
			return markStale()
		}
		actorState, actorStateErr := loadCityRealtimeAgentDecisionActorState(ctx, tx, worldID, agent)
		if actorStateErr != nil {
			return true, false, actorStateErr
		}
		lifeRuntime, runtimeErr := loadCityRealtimeCharacterLifeRuntime(ctx, tx, worldID)
		if runtimeErr != nil {
			return true, false, runtimeErr
		}
		if lifeRuntime == nil {
			return markStale()
		}
		definition, definitionFound := lifeRuntime.Definitions[activityCode]
		if !definitionFound {
			return markStale()
		}
		profile, profileFound, profileErr := loadCityRealtimeCharacterProfile(ctx, tx, worldID, *agent.ActorCode, true)
		if profileErr != nil {
			return true, false, profileErr
		}
		if !profileFound || !cityRealtimeCharacterProfileMatchesRuntime(profile, lifeRuntime) {
			return markStale()
		}
		availability, availabilityErr := cityRealtimeCharacterActivityAvailability(
			ctx, tx, worldID, event.DueWorldTimeUS, actorState, profile, lifeRuntime,
		)
		if availabilityErr != nil {
			return true, false, availabilityErr
		}
		available := cityRealtimeCharacterActivityAvailabilityByCode(availability, activityCode)
		if available == nil || !available.Available {
			return markStale()
		}
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return true, false, gateErr
		}
		if gateErr := enableCityRealtimeCharacterActivityMutationGates(ctx, tx, worldID, frameSequence); gateErr != nil {
			return true, false, gateErr
		}
		transition, transitionErr := cityRealtimeCharacterApplyActivityWithRuntime(
			profile, lifeRuntime, definition, frameSequence, event.DueWorldTimeUS,
		)
		if transitionErr != nil {
			return true, false, transitionErr
		}
		if transition.Inventory != nil {
			if updateErr := updateCityRealtimeCharacterInventoryStack(ctx, tx, worldID, *agent.ActorCode, *transition.Inventory); updateErr != nil {
				return true, false, updateErr
			}
		}
		for _, attribute := range transition.AttributeUpdates {
			if updateErr := updateCityRealtimeCharacterAttributeState(ctx, tx, worldID, *agent.ActorCode, attribute); updateErr != nil {
				return true, false, updateErr
			}
		}
		if insertErr := insertCityRealtimeCharacterActivityEvent(ctx, tx, worldID, *agent.ActorCode, transition.Activity); insertErr != nil {
			return true, false, insertErr
		}
		if transition.Law != nil {
			if insertErr := insertCityRealtimeCharacterLawEvent(ctx, tx, worldID, *agent.ActorCode, *transition.Law); insertErr != nil {
				return true, false, insertErr
			}
		}
		if transition.ProgressionEvent != nil {
			if insertErr := insertCityRealtimeCharacterProgressionEvent(ctx, tx, worldID, *agent.ActorCode, *transition.ProgressionEvent); insertErr != nil {
				return true, false, insertErr
			}
		}
		if updateErr := updateCityRealtimeCharacterProfile(ctx, tx, worldID, profile, transition.Profile); updateErr != nil {
			return true, false, updateErr
		}
		if updateErr := updateCityRealtimeAgentIntentTerminal(ctx, tx, worldID, intent.IntentCode, cityRealtimeAgentIntentApplied, frameSequence); updateErr != nil {
			return true, false, updateErr
		}
		if cityRealtimeAgentCharacterActionRuntimeEnabled(*agentState.Binding) {
			if wakeupErr := cityRealtimeAgentScheduleAutonomousActionWakeup(
				ctx, tx, worldID, frameSequence, event.DueWorldTimeUS, definition.MinimumIntervalUS, *agentState.Binding, agent,
			); wakeupErr != nil {
				return true, false, wakeupErr
			}
		} else {
			nextWakeup, wakeupErr := cityRealtimeAgentNextActivityWakeupWorldTime(event.DueWorldTimeUS, definition.MinimumIntervalUS)
			if wakeupErr != nil {
				return true, false, wakeupErr
			}
			if _, wakeupErr = scheduleCityRealtimeAgentDecisionWakeup(ctx, tx, worldID, agent.AgentCode, nextWakeup, frameSequence); wakeupErr != nil {
				return true, false, wakeupErr
			}
		}
		return true, true, nil
	case cityRealtimeAgentIntentActionMove:
		if !cityRealtimeAgentCharacterActionRuntimeEnabled(*agentState.Binding) ||
			agent.AgentSubtype != "character.user" || agent.ActorCode == nil || intent.ActorCode == nil ||
			*intent.ActorCode != *agent.ActorCode {
			return markStale()
		}
		target, targetErr := cityRealtimeAgentDecisionMoveTargetFromRawArguments(intent.Arguments)
		if targetErr != nil {
			return markStale()
		}
		actorState, actorStateErr := loadCityRealtimeAgentDecisionActorState(ctx, tx, worldID, agent)
		if actorStateErr != nil {
			return true, false, actorStateErr
		}
		motionState, traversable, motionErr := cityRealtimeCharacterWalkMotionState(ctx, tx, worldID, actorState, target)
		if motionErr != nil {
			return true, false, motionErr
		}
		if !traversable || (motionState != "walking" && motionState != "inside") {
			return markStale()
		}
		occupied, occupancyErr := cityRealtimeActorPositionOccupied(ctx, tx, worldID, *agent.ActorCode, target)
		if occupancyErr != nil {
			return true, false, occupancyErr
		}
		if occupied {
			return markStale()
		}
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return true, false, gateErr
		}
		record := cityRealtimeCharacterRecord{
			agent: agent,
			identity: cityRealtimeActorIdentity{
				ActorCode: *agent.ActorCode,
			},
			state: actorState,
		}
		if advanceErr := advanceCityRealtimeCharacterPosition(
			ctx, tx, worldID, frameSequence, &record, target, "move", motionState, "",
		); advanceErr != nil {
			return true, false, advanceErr
		}
		if updateErr := updateCityRealtimeAgentIntentTerminal(ctx, tx, worldID, intent.IntentCode, cityRealtimeAgentIntentApplied, frameSequence); updateErr != nil {
			return true, false, updateErr
		}
		if wakeupErr := cityRealtimeAgentScheduleAutonomousActionWakeup(
			ctx, tx, worldID, frameSequence, event.DueWorldTimeUS, cityRealtimeTimeQuantumUS, *agentState.Binding, agent,
		); wakeupErr != nil {
			return true, false, wakeupErr
		}
		return true, true, nil
	case cityRealtimeAgentIntentActionPortal:
		if !cityRealtimeAgentCharacterActionRuntimeEnabled(*agentState.Binding) ||
			agent.AgentSubtype != "character.user" || agent.ActorCode == nil || intent.ActorCode == nil ||
			*intent.ActorCode != *agent.ActorCode {
			return markStale()
		}
		portalCode, portalCodeErr := cityRealtimeAgentDecisionPortalCodeFromRawArguments(intent.Arguments)
		if portalCodeErr != nil {
			return markStale()
		}
		actorState, actorStateErr := loadCityRealtimeAgentDecisionActorState(ctx, tx, worldID, agent)
		if actorStateErr != nil {
			return true, false, actorStateErr
		}
		portal, portalFound, portalErr := loadCityRealtimeCharacterPortal(ctx, tx, worldID, portalCode)
		if portalErr != nil {
			return true, false, portalErr
		}
		if !portalFound {
			return markStale()
		}
		target, _, targetInside, allowed, transitionErr := cityRealtimeCharacterResolvePortalTransition(
			ctx, tx, worldID, portal,
			cityRealtimeActorSpawnCandidate{X: actorState.X, Y: actorState.Y, Z: actorState.Z},
		)
		if transitionErr != nil {
			return true, false, transitionErr
		}
		if !allowed {
			return markStale()
		}
		occupied, occupancyErr := cityRealtimeActorPositionOccupied(ctx, tx, worldID, *agent.ActorCode, target)
		if occupancyErr != nil {
			return true, false, occupancyErr
		}
		if occupied {
			return markStale()
		}
		motionState := "walking"
		if targetInside {
			motionState = "inside"
		}
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return true, false, gateErr
		}
		record := cityRealtimeCharacterRecord{
			agent: agent,
			identity: cityRealtimeActorIdentity{
				ActorCode: *agent.ActorCode,
			},
			state: actorState,
		}
		if advanceErr := advanceCityRealtimeCharacterPosition(
			ctx, tx, worldID, frameSequence, &record, target, "portal", motionState, portal.Code,
		); advanceErr != nil {
			return true, false, advanceErr
		}
		if updateErr := updateCityRealtimeAgentIntentTerminal(ctx, tx, worldID, intent.IntentCode, cityRealtimeAgentIntentApplied, frameSequence); updateErr != nil {
			return true, false, updateErr
		}
		if wakeupErr := cityRealtimeAgentScheduleAutonomousActionWakeup(
			ctx, tx, worldID, frameSequence, event.DueWorldTimeUS, cityRealtimeTimeQuantumUS, *agentState.Binding, agent,
		); wakeupErr != nil {
			return true, false, wakeupErr
		}
		return true, true, nil
	case cityRealtimeAgentIntentActionRole:
		if !cityRealtimeAgentCharacterActionRuntimeEnabled(*agentState.Binding) ||
			agent.AgentSubtype != "character.user" || agent.ActorCode == nil || intent.ActorCode == nil ||
			*intent.ActorCode != *agent.ActorCode {
			return markStale()
		}
		roleCode, roleCodeErr := cityRealtimeAgentDecisionRoleCodeFromRawArguments(intent.Arguments)
		if roleCodeErr != nil {
			return markStale()
		}
		lifeRuntime, runtimeErr := loadCityRealtimeCharacterLifeRuntime(ctx, tx, worldID)
		if runtimeErr != nil {
			return true, false, runtimeErr
		}
		if !cityRealtimeCharacterProgressionRuntimeEnabled(lifeRuntime) {
			return markStale()
		}
		profile, profileFound, profileErr := loadCityRealtimeCharacterProfile(ctx, tx, worldID, *agent.ActorCode, true)
		if profileErr != nil {
			return true, false, profileErr
		}
		if !profileFound || !cityRealtimeCharacterProfileMatchesRuntime(profile, lifeRuntime) {
			return markStale()
		}
		nextProfile, previousAssignment, nextAssignment, progressionEvent, _, roleErr := cityRealtimeCharacterApplyRoleChange(
			profile, lifeRuntime, roleCode, frameSequence,
		)
		if roleErr != nil {
			return markStale()
		}
		if nextAssignment == nil {
			return true, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_role_assignment"})
		}
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return true, false, gateErr
		}
		if gateErr := enableCityRealtimeCharacterActivityMutationGates(ctx, tx, worldID, frameSequence); gateErr != nil {
			return true, false, gateErr
		}
		if previousAssignment == nil {
			if insertErr := insertCityRealtimeCharacterRoleAssignment(ctx, tx, worldID, *agent.ActorCode, *nextAssignment); insertErr != nil {
				return true, false, insertErr
			}
		} else if updateErr := updateCityRealtimeCharacterRoleAssignment(
			ctx, tx, worldID, *agent.ActorCode, *previousAssignment, *nextAssignment,
		); updateErr != nil {
			return true, false, updateErr
		}
		if insertErr := insertCityRealtimeCharacterProgressionEvent(ctx, tx, worldID, *agent.ActorCode, progressionEvent); insertErr != nil {
			return true, false, insertErr
		}
		if updateErr := updateCityRealtimeCharacterProfile(ctx, tx, worldID, profile, nextProfile); updateErr != nil {
			return true, false, updateErr
		}
		if updateErr := updateCityRealtimeAgentIntentTerminal(ctx, tx, worldID, intent.IntentCode, cityRealtimeAgentIntentApplied, frameSequence); updateErr != nil {
			return true, false, updateErr
		}
		if wakeupErr := cityRealtimeAgentScheduleAutonomousActionWakeup(
			ctx, tx, worldID, frameSequence, event.DueWorldTimeUS, cityRealtimeTimeQuantumUS, *agentState.Binding, agent,
		); wakeupErr != nil {
			return true, false, wakeupErr
		}
		return true, true, nil
	default:
		return markStale()
	}
}
