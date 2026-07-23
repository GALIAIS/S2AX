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

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
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

	cityRealtimeAgentOutboxQueued           = "queued"
	cityRealtimeAgentOutboxLeased           = "leased"
	cityRealtimeAgentOutboxSucceeded        = "succeeded"
	cityRealtimeAgentOutboxFailed           = "failed_terminal"
	cityRealtimeAgentOutboxCancelled        = "cancelled"
	cityRealtimeAgentIntentActionWait       = "agent.wait"
	cityRealtimeAgentIntentActionActivity   = "character.activity.perform"
	cityRealtimeAgentIntentActionCase       = "character.case.acknowledge"
	cityRealtimeAgentIntentActionCaseReport = "character.case.report.file"
	cityRealtimeAgentIntentActionCaseReview = "character.case.review.file"
	cityRealtimeAgentIntentActionMove       = "character.move"
	cityRealtimeAgentIntentActionPortal     = "character.portal.traverse"
	cityRealtimeAgentIntentActionRole       = "character.role.change"
	cityRealtimeAgentIntentActionSocial     = "character.social.greet"
	cityRealtimeAgentIntentActionTask       = "character.task.accept"
	cityRealtimeAgentIntentActionNavigation = "character.navigation.plan"

	cityRealtimeDueEventTypeAgentIntent         = "system.realtime.agent_intent"
	cityRealtimeDueEventTypeAgentDecisionWakeup = "system.realtime.agent_wakeup"

	cityRealtimeAgentDecisionLeaseDuration       = 30 * time.Second
	cityRealtimeAgentDecisionLeaseFinalizerGrace = 15 * time.Second
	cityRealtimeAgentDecisionObservationTTLUS    = int64(15 * 60 * cityRealtimeTimeQuantumUS)
	cityRealtimeAgentDecisionMaximumAttempts     = 3
)

var (
	errCityRealtimeAgentDecisionLeaseBudgetExhausted = errors.New("realtime agent decision lease budget exhausted")
	errCityRealtimeAgentDecisionRetryNotBefore       = errors.New("realtime agent decision retry is not due")
)

// cityRealtimeAgentDecisionLeaseDurationForProfile keeps a provider worker
// lease alive for the complete immutable profile timeout plus a bounded
// finalizer window. Without this, a valid long-running provider call could be
// leased by a second worker before the first worker has a chance to seal its
// result. Legacy/fake requests intentionally retain the short base lease.
func cityRealtimeAgentDecisionLeaseDurationForProfile(profile *CityRealtimeAgentModelProfile) time.Duration {
	duration := cityRealtimeAgentDecisionLeaseDuration
	if profile == nil || profile.TimeoutMS <= 0 {
		return duration
	}
	candidate := time.Duration(profile.TimeoutMS)*time.Millisecond + cityRealtimeAgentDecisionLeaseFinalizerGrace
	if candidate > duration {
		return candidate
	}
	return duration
}

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
	ModelProfileCode       *string
	ModelProfileVersion    *int
	ModelProfileHash       *string
	ModelBudgetHash        *string
	ObservedFrameSequence  int64
	ExpiresAtWorldTimeUS   int64
	Status                 string
	AttemptCount           int
	LeaseOwner             *string
	LeaseExpiresAt         *time.Time
	RetryNotBefore         *time.Time
	RequestedFrameSequence int64
	TerminalFrameSequence  *int64
}

type cityRealtimeAgentDecisionAttemptRecord struct {
	AttemptCode          string
	RequestCode          string
	AttemptNumber        int
	ProviderCode         string
	Status               string
	RequestHash          string
	ResponseHash         *string
	ModelProfileCode     *string
	ModelProfileVersion  *int
	ModelProfileHash     *string
	ModelBudgetHash      *string
	ReservedInputTokens  *int
	ReservedOutputTokens *int
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

// CityRealtimeAgentDecisionRunInput is the worker-only request for one
// provider-backed decision. The selected provider always comes from the
// immutable request snapshot; callers cannot select a model, route, account or
// action here. There is intentionally no HTTP route for this operation.
type CityRealtimeAgentDecisionRunInput struct {
	WorldID     int64
	RequestCode string
	WorkerID    string
}

// CityRealtimeAgentDecisionRunResult is a safe worker result. ErrorCode is a
// closed, provider-boundary classification only; provider text and raw output
// are never persisted or exposed. RetryNotBefore is advisory scheduling data
// outside the canonical world state.
type CityRealtimeAgentDecisionRunResult struct {
	RequestCode    string             `json:"request_code"`
	DecisionCode   string             `json:"decision_code,omitempty"`
	IntentCode     string             `json:"intent_code,omitempty"`
	Status         string             `json:"status"`
	ErrorCode      string             `json:"error_code,omitempty"`
	RetryNotBefore *time.Time         `json:"retry_not_before,omitempty"`
	Frame          *CityTemporalFrame `json:"frame,omitempty"`
}

// CityRealtimeAgentFakeDecisionRunInput drives the deterministic built-in
// provider. It exists for existing admin/test workflows and remains a narrow
// wrapper around the generic provider worker below.
type CityRealtimeAgentFakeDecisionRunInput struct {
	WorldID     int64
	RequestCode string
	WorkerID    string
	// PreferredAction is an administrator/test-only deterministic adapter
	// selector. It never becomes a browser route or provider setting, and it
	// can select only an action already published in the sealed observation.
	PreferredAction string
}

// Kept as an alias so existing deterministic test/admin callers retain their
// compile-time API while sharing the generic provider result contract.
type CityRealtimeAgentFakeDecisionRunResult = CityRealtimeAgentDecisionRunResult

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
		cityRealtimeAgentCorePolicyVersionActions,
		cityRealtimeAgentCorePolicyVersionCase,
		cityRealtimeAgentCorePolicyVersionSocial,
		cityRealtimeAgentCorePolicyVersionReview,
		cityRealtimeAgentCorePolicyVersionReport,
		cityRealtimeAgentCorePolicyVersionIntake,
		cityRealtimeAgentCorePolicyVersionEvidence,
		cityRealtimeAgentCorePolicyVersionEvidenceAssignment,
		cityRealtimeAgentCorePolicyVersionProcedureDispatch,
		cityRealtimeAgentCorePolicyVersionTask,
		cityRealtimeAgentCorePolicyVersionNavigationPlan:
		return true
	default:
		return false
	}
}

func cityRealtimeAgentDecisionRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionDecision ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionAutonomy ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionActions ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionCase ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionSocial ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReview ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReport ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionIntake ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidence ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidenceAssignment ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionProcedureDispatch ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan)
}

// cityRealtimeAgentCharacterControlRuntimeEnabled is intentionally pinned to
// 1.2.0 and newer. A2 worlds remain read/write compatible with their original
// wait-only decision runtime and never gain a personality/control event
// retroactively.
func cityRealtimeAgentCharacterControlRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionAutonomy ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionActions ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionCase ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionSocial ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReview ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReport ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionIntake ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidence ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidenceAssignment ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionProcedureDispatch ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan)
}

// cityRealtimeAgentCharacterActionRuntimeEnabled is deliberately limited to
// 1.3.0 and later. A3.1 worlds remain frozen to their wait/activity action
// catalogue even after the binary learns later action adapters.
func cityRealtimeAgentCharacterActionRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionActions ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionCase ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionSocial ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReview ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReport ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionIntake ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidence ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidenceAssignment ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionProcedureDispatch ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan)
}

// cityRealtimeAgentCharacterCaseRuntimeEnabled is available to the 1.4 Case
// adapter and to later policies that explicitly retain that adapter. Historical
// 1.0-1.3 worlds remain unchanged.
func cityRealtimeAgentCharacterCaseRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionCase ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionSocial ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReview ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReport ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionIntake ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidence ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidenceAssignment ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionProcedureDispatch ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan)
}

func cityRealtimeAgentCharacterSocialRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionSocial ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReview ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReport ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionIntake ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidence ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidenceAssignment ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionProcedureDispatch ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan)
}

// cityRealtimeAgentCharacterCaseReviewRuntimeEnabled is deliberately pinned
// to policy versions that explicitly retain the 1.6.0 review contract. A
// review is an owner-scoped procedural receipt for an already acknowledged
// Law Case; historical policies never gain that action or state shape after an
// executable upgrade.
func cityRealtimeAgentCharacterCaseReviewRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReview ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReport ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionIntake ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidence ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidenceAssignment ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionProcedureDispatch ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan)
}

// cityRealtimeAgentCharacterCaseReportRuntimeEnabled is pinned to policies
// that retain the 1.7 receipt contract. Policy 1.8 adds a separate server
// work item after that receipt; it does not widen the model action surface.
func cityRealtimeAgentCharacterCaseReportRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReport ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionIntake ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidence ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidenceAssignment ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionProcedureDispatch ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan)
}

// cityRealtimeAgentCharacterCaseIntakeRuntimeEnabled is pinned to policies
// that explicitly retain the 1.8 work-item contract. Historical 1.7 receipts
// remain immutable facts with no workflow state.
func cityRealtimeAgentCharacterCaseIntakeRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionIntake ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidence ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidenceAssignment ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionProcedureDispatch ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan)
}

// cityRealtimeAgentCharacterCaseEvidenceRuntimeEnabled is limited to policies
// that retain the sealed-law source. It does not add an Agent action or
// reinterpret any report.
func cityRealtimeAgentCharacterCaseEvidenceRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidence ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidenceAssignment ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionProcedureDispatch ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan)
}

// cityRealtimeAgentCharacterCaseEvidenceAssignmentRuntimeEnabled is limited
// to 1.10 and newer. A report, model, or browser cannot choose the source.
func cityRealtimeAgentCharacterCaseEvidenceAssignmentRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidenceAssignment ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionProcedureDispatch ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan)
}

// cityRealtimeAgentCharacterCaseProcedureDispatchRuntimeEnabled retains the
// 1.11 adapter in explicit later policies. It does not add an Agent action,
// reviewer, adjudication, or asset path.
func cityRealtimeAgentCharacterCaseProcedureDispatchRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionProcedureDispatch ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan)
}

// cityRealtimeAgentCharacterTaskRuntimeEnabled preserves the 1.12 task
// contract in later explicit policies. Earlier worlds never receive task
// candidates or a task-completion state shape.
func cityRealtimeAgentCharacterTaskRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		(binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask ||
			binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan)
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
	if binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionActions {
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
	if binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionSocial {
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
				cityRealtimeAgentIntentActionCase,
				cityRealtimeAgentIntentActionMove,
				cityRealtimeAgentIntentActionPortal,
				cityRealtimeAgentIntentActionRole,
				cityRealtimeAgentIntentActionSocial,
			}, true
		default:
			return nil, false
		}
	}
	if binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReview {
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
				cityRealtimeAgentIntentActionCase,
				cityRealtimeAgentIntentActionCaseReview,
				cityRealtimeAgentIntentActionMove,
				cityRealtimeAgentIntentActionPortal,
				cityRealtimeAgentIntentActionRole,
				cityRealtimeAgentIntentActionSocial,
			}, true
		default:
			return nil, false
		}
	}
	if binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionReport ||
		binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionIntake ||
		binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidence ||
		binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionEvidenceAssignment ||
		binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionProcedureDispatch {
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
				cityRealtimeAgentIntentActionCase,
				cityRealtimeAgentIntentActionCaseReport,
				cityRealtimeAgentIntentActionCaseReview,
				cityRealtimeAgentIntentActionMove,
				cityRealtimeAgentIntentActionPortal,
				cityRealtimeAgentIntentActionRole,
				cityRealtimeAgentIntentActionSocial,
			}, true
		default:
			return nil, false
		}
	}
	if binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionTask {
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
				cityRealtimeAgentIntentActionCase,
				cityRealtimeAgentIntentActionCaseReport,
				cityRealtimeAgentIntentActionCaseReview,
				cityRealtimeAgentIntentActionMove,
				cityRealtimeAgentIntentActionPortal,
				cityRealtimeAgentIntentActionRole,
				cityRealtimeAgentIntentActionSocial,
				cityRealtimeAgentIntentActionTask,
			}, true
		default:
			return nil, false
		}
	}
	if binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan {
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
				cityRealtimeAgentIntentActionCase,
				cityRealtimeAgentIntentActionCaseReport,
				cityRealtimeAgentIntentActionCaseReview,
				cityRealtimeAgentIntentActionMove,
				cityRealtimeAgentIntentActionNavigation,
				cityRealtimeAgentIntentActionPortal,
				cityRealtimeAgentIntentActionRole,
				cityRealtimeAgentIntentActionSocial,
				cityRealtimeAgentIntentActionTask,
			}, true
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
			cityRealtimeAgentIntentActionCase,
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
       retry_not_before,
       requested_frame_sequence, terminal_frame_sequence,
       model_profile_code, model_profile_version, model_profile_hash, model_budget_hash
FROM city_realtime_agent_decision_requests
WHERE world_id = $1 AND request_code = $2`
	if forUpdate {
		query += " FOR UPDATE"
	}
	item := cityRealtimeAgentDecisionRequestRecord{}
	var leaseOwner sql.NullString
	var leaseExpiresAt, retryNotBefore sql.NullTime
	var terminalFrameSequence sql.NullInt64
	var modelProfileCode, modelProfileHash, modelBudgetHash sql.NullString
	var modelProfileVersion sql.NullInt64
	err := queryer.QueryRowContext(ctx, query, worldID, requestCode).Scan(
		&item.RequestCode, &item.AgentCode, &item.ObservationCode, &item.ObservationHash,
		&item.PreconditionHash, &item.ObservedFrameSequence, &item.ExpiresAtWorldTimeUS,
		&item.Status, &item.AttemptCount, &leaseOwner, &leaseExpiresAt, &retryNotBefore,
		&item.RequestedFrameSequence, &terminalFrameSequence,
		&modelProfileCode, &modelProfileVersion, &modelProfileHash, &modelBudgetHash,
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
	item.RetryNotBefore = cityRealtimeAgentNullTimePointer(retryNotBefore)
	item.TerminalFrameSequence = nullInt64Pointer(terminalFrameSequence)
	item.ModelProfileCode = cityRealtimeAgentNullStringPointer(modelProfileCode)
	item.ModelProfileVersion = cityRealtimeAgentNullIntPointer(modelProfileVersion)
	item.ModelProfileHash = cityRealtimeAgentNullStringPointer(modelProfileHash)
	item.ModelBudgetHash = cityRealtimeAgentNullStringPointer(modelBudgetHash)
	if !cityRealtimeAgentDecisionRequestRecordValid(item) {
		return cityRealtimeAgentDecisionRequestRecord{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_request"})
	}
	return item, true, nil
}

func cityRealtimeAgentNullIntPointer(value sql.NullInt64) *int {
	if !value.Valid || value.Int64 < int64(-int(^uint(0)>>1)-1) || value.Int64 > int64(int(^uint(0)>>1)) {
		return nil
	}
	result := int(value.Int64)
	return &result
}

func cityRealtimeAgentDecisionModelSnapshotValid(
	profileCode *string,
	profileVersion *int,
	profileHash *string,
	budgetHash *string,
) bool {
	if profileCode == nil && profileVersion == nil && profileHash == nil && budgetHash == nil {
		return true
	}
	return profileCode != nil && profileVersion != nil && profileHash != nil && budgetHash != nil &&
		cityRealtimeAgentModelProfileCodeValid(*profileCode) && *profileVersion > 0 &&
		cityRealtimeSHA256Hex(*profileHash) && cityRealtimeSHA256Hex(*budgetHash)
}

// cityRealtimeAgentDecisionExecutionProfile resolves the immutable tuple that
// was captured when the request was queued. It intentionally does not inspect
// the mutable profile head: disabling a profile blocks new requests at enqueue
// time, while already queued work remains reproducible against its snapshot.
func cityRealtimeAgentDecisionExecutionProfile(
	ctx context.Context,
	queryer citySQLQueryer,
	request cityRealtimeAgentDecisionRequestRecord,
) (*CityRealtimeAgentModelProfile, error) {
	if !cityRealtimeAgentDecisionModelSnapshotValid(
		request.ModelProfileCode, request.ModelProfileVersion, request.ModelProfileHash, request.ModelBudgetHash,
	) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_model_snapshot"})
	}
	if request.ModelProfileCode == nil {
		return nil, nil
	}
	profile, found, err := loadCityRealtimeAgentModelProfileVersion(
		ctx, queryer, *request.ModelProfileCode, *request.ModelProfileVersion,
	)
	if err != nil {
		return nil, err
	}
	if !found || profile.ProfileHash != *request.ModelProfileHash || profile.BudgetHash != *request.ModelBudgetHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_model_profile"})
	}
	return profile, nil
}

func cityRealtimeAgentDecisionProviderCode(profile *CityRealtimeAgentModelProfile) string {
	if profile == nil {
		return cityRealtimeAgentFakeProviderCode
	}
	return profile.ProviderCode
}

func cityRealtimeAgentDecisionMaximumAttemptsForProfile(profile *CityRealtimeAgentModelProfile) int {
	if profile == nil {
		return cityRealtimeAgentDecisionMaximumAttempts
	}
	return profile.RetryLimit + 1
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
		item.RequestedFrameSequence <= 0 || item.AttemptCount < 0 ||
		!cityRealtimeAgentDecisionModelSnapshotValid(
			item.ModelProfileCode, item.ModelProfileVersion, item.ModelProfileHash, item.ModelBudgetHash,
		) {
		return false
	}
	if cityRealtimeAgentDecisionRequestStatusActive(item.Status) {
		if item.Status == cityRealtimeAgentDecisionRequestQueued {
			return item.LeaseOwner == nil && item.LeaseExpiresAt == nil && item.TerminalFrameSequence == nil &&
				(item.RetryNotBefore == nil || !item.RetryNotBefore.IsZero())
		}
		return item.LeaseOwner != nil && item.LeaseExpiresAt != nil && item.RetryNotBefore == nil && item.TerminalFrameSequence == nil
	}
	return cityRealtimeAgentDecisionRequestStatusTerminal(item.Status) && item.LeaseOwner == nil &&
		item.LeaseExpiresAt == nil && item.RetryNotBefore == nil && item.TerminalFrameSequence != nil &&
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
     status, attempt_count, requested_frame_sequence,
     model_profile_code, model_profile_version, model_profile_hash, model_budget_hash,
     metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'queued', 0, $9,
        $10, $11, $12, $13, '{}'::jsonb)`,
		worldID, request.RequestCode, request.AgentCode, request.ObservationCode,
		request.ObservationHash, request.PreconditionHash, request.ObservedFrameSequence,
		request.ExpiresAtWorldTimeUS, request.RequestedFrameSequence,
		request.ModelProfileCode, request.ModelProfileVersion, request.ModelProfileHash, request.ModelBudgetHash,
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
	executionProfile, err := cityRealtimeAgentModelProfileForAgent(ctx, tx, worldID, agent)
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
	if executionProfile != nil {
		profileCode := executionProfile.Code
		profileVersion := executionProfile.Version
		profileHash := executionProfile.ProfileHash
		budgetHash := executionProfile.BudgetHash
		request.ModelProfileCode = &profileCode
		request.ModelProfileVersion = &profileVersion
		request.ModelProfileHash = &profileHash
		request.ModelBudgetHash = &budgetHash
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
		!cityRealtimeAgentIdentifierValid(attempt.ProviderCode, 64) ||
		attempt.Status != cityRealtimeAgentDecisionAttemptStarted || !cityRealtimeSHA256Hex(attempt.RequestHash) ||
		!cityRealtimeAgentDecisionModelSnapshotValid(
			attempt.ModelProfileCode, attempt.ModelProfileVersion, attempt.ModelProfileHash, attempt.ModelBudgetHash,
		) {
		return ErrCityInvalidInput
	}
	if attempt.ModelProfileCode == nil {
		if attempt.ProviderCode != cityRealtimeAgentFakeProviderCode || attempt.ReservedInputTokens != nil || attempt.ReservedOutputTokens != nil {
			return ErrCityInvalidInput
		}
	} else if attempt.ReservedInputTokens == nil || attempt.ReservedOutputTokens == nil ||
		*attempt.ReservedInputTokens <= 0 || *attempt.ReservedOutputTokens <= 0 {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_decision_attempts
    (world_id, attempt_code, request_code, attempt_number, provider_code,
     status, request_hash,
     model_profile_code, model_profile_version, model_profile_hash, model_budget_hash,
     reserved_input_tokens, reserved_output_tokens, metadata)
VALUES ($1, $2, $3, $4, $5, 'started', $6,
        $7, $8, $9, $10, $11, $12, '{}'::jsonb)`,
		worldID, attempt.AttemptCode, attempt.RequestCode, attempt.AttemptNumber,
		attempt.ProviderCode, attempt.RequestHash,
		attempt.ModelProfileCode, attempt.ModelProfileVersion, attempt.ModelProfileHash, attempt.ModelBudgetHash,
		attempt.ReservedInputTokens, attempt.ReservedOutputTokens,
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
	var modelProfileCode, modelProfileHash, modelBudgetHash sql.NullString
	var modelProfileVersion, reservedInputTokens, reservedOutputTokens sql.NullInt64
	err := tx.QueryRowContext(ctx, `
SELECT attempt_code, request_code, attempt_number, provider_code, status,
       request_hash, response_hash,
       model_profile_code, model_profile_version, model_profile_hash, model_budget_hash,
       reserved_input_tokens, reserved_output_tokens
FROM city_realtime_agent_decision_attempts
WHERE world_id = $1 AND request_code = $2 AND attempt_number = $3
FOR UPDATE`, worldID, requestCode, attemptNumber).Scan(
		&item.AttemptCode, &item.RequestCode, &item.AttemptNumber, &item.ProviderCode,
		&item.Status, &item.RequestHash, &responseHash,
		&modelProfileCode, &modelProfileVersion, &modelProfileHash, &modelBudgetHash,
		&reservedInputTokens, &reservedOutputTokens,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeAgentDecisionAttemptRecord{}, false, nil
	}
	if err != nil {
		return cityRealtimeAgentDecisionAttemptRecord{}, false, fmt.Errorf("load realtime agent decision attempt: %w", err)
	}
	item.ResponseHash = cityRealtimeAgentNullStringPointer(responseHash)
	item.ModelProfileCode = cityRealtimeAgentNullStringPointer(modelProfileCode)
	item.ModelProfileVersion = cityRealtimeAgentNullIntPointer(modelProfileVersion)
	item.ModelProfileHash = cityRealtimeAgentNullStringPointer(modelProfileHash)
	item.ModelBudgetHash = cityRealtimeAgentNullStringPointer(modelBudgetHash)
	item.ReservedInputTokens = cityRealtimeAgentNullIntPointer(reservedInputTokens)
	item.ReservedOutputTokens = cityRealtimeAgentNullIntPointer(reservedOutputTokens)
	if !cityRealtimeAgentDecisionAttemptRecordValid(item) || item.RequestCode != requestCode || item.AttemptNumber != attemptNumber {
		return cityRealtimeAgentDecisionAttemptRecord{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_attempt"})
	}
	return item, true, nil
}

func cityRealtimeAgentDecisionAttemptRecordValid(item cityRealtimeAgentDecisionAttemptRecord) bool {
	if !cityRealtimeAgentIdentifierValid(item.AttemptCode, 96) ||
		!cityRealtimeAgentIdentifierValid(item.RequestCode, 96) || item.AttemptNumber <= 0 ||
		!cityRealtimeAgentIdentifierValid(item.ProviderCode, 64) ||
		(item.Status != cityRealtimeAgentDecisionAttemptStarted && item.Status != cityRealtimeAgentDecisionAttemptSucceeded && item.Status != cityRealtimeAgentDecisionAttemptFailed) ||
		!cityRealtimeSHA256Hex(item.RequestHash) || (item.ResponseHash != nil && !cityRealtimeSHA256Hex(*item.ResponseHash)) ||
		!cityRealtimeAgentDecisionModelSnapshotValid(
			item.ModelProfileCode, item.ModelProfileVersion, item.ModelProfileHash, item.ModelBudgetHash,
		) {
		return false
	}
	if item.ModelProfileCode == nil {
		return item.ProviderCode == cityRealtimeAgentFakeProviderCode &&
			item.ReservedInputTokens == nil && item.ReservedOutputTokens == nil
	}
	return item.ReservedInputTokens != nil && item.ReservedOutputTokens != nil &&
		*item.ReservedInputTokens > 0 && *item.ReservedOutputTokens > 0
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
	expectedProviderCode string,
	now time.Time,
) (cityRealtimeAgentDecisionRequestRecord, cityRealtimeAgentObservationRecord, cityRealtimeAgentDecisionAttemptRecord, error) {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) ||
		!cityRealtimeAgentIdentifierValid(workerID, 64) || !cityRealtimeAgentIdentifierValid(expectedProviderCode, 64) {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, ErrCityInvalidInput
	}
	now = now.UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, ErrCityInvalidInput
	}
	request, found, err := loadCityRealtimeAgentDecisionRequest(ctx, tx, worldID, requestCode, true)
	if err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
	}
	if !found {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, ErrCityRealtimeAgentDecisionNotFound
	}
	if cityRealtimeAgentDecisionRequestStatusTerminal(request.Status) {
		return request, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, nil
	}
	quarantined, err := cityRealtimeAgentDecisionQuarantined(ctx, tx, worldID, requestCode, true)
	if err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
	}
	if quarantined {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{},
			ErrCityRealtimeAgentDecisionQuarantined
	}
	if request.Status == cityRealtimeAgentDecisionRequestQueued && request.RetryNotBefore != nil && request.RetryNotBefore.After(now) {
		return request, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, errCityRealtimeAgentDecisionRetryNotBefore
	}
	executionProfile, err := cityRealtimeAgentDecisionExecutionProfile(ctx, tx, request)
	if err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
	}
	providerCode := cityRealtimeAgentDecisionProviderCode(executionProfile)
	if providerCode != expectedProviderCode {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{},
			ErrCityRealtimeAgentProviderUnavailable.WithMetadata(map[string]string{"provider_code": providerCode})
	}
	maximumAttempts := cityRealtimeAgentDecisionMaximumAttemptsForProfile(executionProfile)
	if request.Status == cityRealtimeAgentDecisionRequestLeased && request.LeaseExpiresAt != nil && !request.LeaseExpiresAt.After(now) {
		if err = enableCityRealtimeAgentDecisionWorkerGate(ctx, tx, worldID, requestCode); err != nil {
			return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
		}
		expiredAttempt, attemptFound, attemptErr := loadCityRealtimeAgentDecisionAttemptForUpdate(
			ctx, tx, worldID, requestCode, request.AttemptCount,
		)
		if attemptErr != nil {
			return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, attemptErr
		}
		if !attemptFound || expiredAttempt.Status != cityRealtimeAgentDecisionAttemptStarted {
			return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{},
				ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_expired_attempt"})
		}
		if err = updateCityRealtimeAgentDecisionAttemptFailed(ctx, tx, worldID, expiredAttempt.AttemptCode, "lease_expired"); err != nil {
			return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_requests
SET status = 'queued', lease_owner = NULL, lease_expires_at = NULL,
    retry_not_before = NULL, updated_at = NOW()
WHERE world_id = $1 AND request_code = $2 AND status = 'leased'`, worldID, requestCode); err != nil {
			return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, fmt.Errorf("requeue expired realtime agent decision lease: %w", err)
		}
		request.Status = cityRealtimeAgentDecisionRequestQueued
		request.LeaseOwner = nil
		request.LeaseExpiresAt = nil
		request.RetryNotBefore = nil
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
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{},
			ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_lease"})
	}
	if request.AttemptCount >= maximumAttempts {
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
	if err = enableCityRealtimeAgentModelBudgetWorkerGate(ctx, tx, worldID, requestCode); err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
	}
	if executionProfile != nil {
		if err = acquireCityRealtimeAgentModelCircuitBreaker(ctx, tx, executionProfile, requestCode, now); err != nil {
			return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
		}
	}
	nextAttempt := request.AttemptCount + 1
	leaseExpiry := now.Add(cityRealtimeAgentDecisionLeaseDurationForProfile(executionProfile)).UTC().Truncate(time.Microsecond)
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_requests
SET status = 'leased', attempt_count = $4, lease_owner = $5,
    lease_expires_at = $6, retry_not_before = NULL, updated_at = NOW()
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
		"provider_code":     providerCode,
		"attempt_number":    nextAttempt,
	}
	if executionProfile != nil {
		requestHashPayload["model_profile_code"] = executionProfile.Code
		requestHashPayload["model_profile_version"] = executionProfile.Version
		requestHashPayload["model_profile_hash"] = executionProfile.ProfileHash
		requestHashPayload["model_budget_hash"] = executionProfile.BudgetHash
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
		ProviderCode: providerCode, Status: cityRealtimeAgentDecisionAttemptStarted,
		RequestHash: requestHash,
	}
	if executionProfile != nil {
		profileCode := executionProfile.Code
		profileVersion := executionProfile.Version
		profileHash := executionProfile.ProfileHash
		budgetHash := executionProfile.BudgetHash
		reservedInputTokens := executionProfile.MaxInputTokens
		reservedOutputTokens := executionProfile.MaxOutputTokens
		attempt.ModelProfileCode = &profileCode
		attempt.ModelProfileVersion = &profileVersion
		attempt.ModelProfileHash = &profileHash
		attempt.ModelBudgetHash = &budgetHash
		attempt.ReservedInputTokens = &reservedInputTokens
		attempt.ReservedOutputTokens = &reservedOutputTokens
	}
	if err = insertCityRealtimeAgentDecisionAttempt(ctx, tx, worldID, attempt); err != nil {
		return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
	}
	if executionProfile != nil {
		if err = reserveCityRealtimeAgentModelAttemptBudget(
			ctx, tx, worldID, request, attempt, executionProfile, now,
		); err != nil {
			return cityRealtimeAgentDecisionRequestRecord{}, cityRealtimeAgentObservationRecord{}, cityRealtimeAgentDecisionAttemptRecord{}, err
		}
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
	request.RetryNotBefore = nil
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
		case cityRealtimeAgentIntentActionTask:
			if snapshot.Character.ActionContext != nil {
				for _, candidate := range []string{"task.civic.shift", "task.civic.cleanup"} {
					if cityRealtimeAgentActionContextContains(snapshot.Character.ActionContext.AvailableTaskCodes, candidate) {
						return map[string]any{"task_code": candidate}, "fake_provider_task", true
					}
				}
			}
		case cityRealtimeAgentIntentActionNavigation:
			if snapshot.Character.ActionContext != nil && len(snapshot.Character.ActionContext.AvailableNavigationDestinationPortalCodes) > 0 {
				return map[string]any{"destination_portal_code": snapshot.Character.ActionContext.AvailableNavigationDestinationPortalCodes[0]}, "fake_provider_navigation", true
			}
		case cityRealtimeAgentIntentActionCase:
			if snapshot.Character.ActionContext != nil && len(snapshot.Character.ActionContext.AvailableCaseCodes) > 0 {
				return map[string]any{"case_code": snapshot.Character.ActionContext.AvailableCaseCodes[0]}, "fake_provider_case", true
			}
		case cityRealtimeAgentIntentActionCaseReview:
			if snapshot.Character.ActionContext != nil && len(snapshot.Character.ActionContext.AvailableCaseReviewCodes) > 0 {
				return map[string]any{"case_code": snapshot.Character.ActionContext.AvailableCaseReviewCodes[0]}, "fake_provider_case_review", true
			}
		case cityRealtimeAgentIntentActionCaseReport:
			if snapshot.Character.ActionContext != nil && len(snapshot.Character.ActionContext.AvailableSocialTargets) > 0 {
				return map[string]any{"target_actor_code": snapshot.Character.ActionContext.AvailableSocialTargets[0]}, "fake_provider_case_report", true
			}
		case cityRealtimeAgentIntentActionSocial:
			if snapshot.Character.ActionContext != nil && len(snapshot.Character.ActionContext.AvailableSocialTargets) > 0 {
				return map[string]any{"target_actor_code": snapshot.Character.ActionContext.AvailableSocialTargets[0]}, "fake_provider_social", true
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
		cityRealtimeAgentIntentActionCaseReview,
		cityRealtimeAgentIntentActionCase,
		cityRealtimeAgentIntentActionSocial,
		cityRealtimeAgentIntentActionActivity,
		cityRealtimeAgentIntentActionPortal,
		cityRealtimeAgentIntentActionNavigation,
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
		cityRealtimeAgentIntentActionCase,
		cityRealtimeAgentIntentActionCaseReport,
		cityRealtimeAgentIntentActionCaseReview,
		cityRealtimeAgentIntentActionSocial,
		cityRealtimeAgentIntentActionTask,
		cityRealtimeAgentIntentActionNavigation,
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
	case cityRealtimeAgentIntentActionTask:
		if !cityRealtimeAgentCharacterTaskRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" || agent.ActorCode == nil {
			return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
		if _, err := cityRealtimeAgentDecisionTaskCodeFromArguments(envelope.Intent.Arguments); err != nil {
			return nil, "", err
		}
	case cityRealtimeAgentIntentActionNavigation:
		if !cityRealtimeCharacterNavigationPlanRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" || agent.ActorCode == nil {
			return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
		if _, err := cityRealtimeAgentDecisionNavigationPortalCodeFromArguments(envelope.Intent.Arguments); err != nil {
			return nil, "", err
		}
	case cityRealtimeAgentIntentActionCase:
		if !cityRealtimeAgentCharacterCaseRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" || agent.ActorCode == nil {
			return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
		if _, err := cityRealtimeAgentDecisionCaseCodeFromArguments(envelope.Intent.Arguments); err != nil {
			return nil, "", err
		}
	case cityRealtimeAgentIntentActionCaseReview:
		if !cityRealtimeAgentCharacterCaseReviewRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" || agent.ActorCode == nil {
			return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
		if _, err := cityRealtimeAgentDecisionCaseReviewCodeFromArguments(envelope.Intent.Arguments); err != nil {
			return nil, "", err
		}
	case cityRealtimeAgentIntentActionCaseReport:
		if !cityRealtimeAgentCharacterCaseReportRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" || agent.ActorCode == nil {
			return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
		if _, err := cityRealtimeAgentDecisionCaseReportTargetCodeFromArguments(envelope.Intent.Arguments); err != nil {
			return nil, "", err
		}
	case cityRealtimeAgentIntentActionSocial:
		if !cityRealtimeAgentCharacterSocialRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" || agent.ActorCode == nil {
			return nil, "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
		if _, err := cityRealtimeAgentDecisionSocialTargetCodeFromArguments(envelope.Intent.Arguments); err != nil {
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
    retry_not_before = NULL, terminal_frame_sequence = $4, updated_at = NOW()
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

// requeueCityRealtimeAgentDecisionAfterProviderFailure releases a completed
// failed attempt back to the durable queue with a bounded retry deadline. It
// does not seal a world frame: the request remains unresolved canonical work,
// while retry scheduling is external-I/O operational state. The database guard
// verifies that the current leased attempt is already failed before allowing
// this immediate release.
func requeueCityRealtimeAgentDecisionAfterProviderFailure(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	requestCode string,
	retryNotBefore time.Time,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) || retryNotBefore.IsZero() {
		return ErrCityInvalidInput
	}
	retryNotBefore = retryNotBefore.UTC().Truncate(time.Microsecond)
	if !retryNotBefore.After(time.Now().UTC()) {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_requests
SET status = 'queued', lease_owner = NULL, lease_expires_at = NULL,
    retry_not_before = $3, updated_at = NOW()
WHERE world_id = $1 AND request_code = $2 AND status = 'leased'`,
		worldID, requestCode, retryNotBefore,
	)
	if err != nil {
		return fmt.Errorf("requeue realtime agent decision after provider failure: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime agent decision provider retry requeue: %w", rowsErr)
	} else if rows != 1 {
		return ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_provider_retry"})
	}
	return nil
}

func requeueCityRealtimeAgentDecisionOutboxAfterProviderFailure(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	requestCode string,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_outbox
SET status = 'queued', lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW()
WHERE world_id = $1 AND request_code = $2 AND status = 'leased'`, worldID, requestCode)
	if err != nil {
		return fmt.Errorf("requeue realtime agent decision outbox after provider failure: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime agent decision outbox provider retry requeue: %w", rowsErr)
	} else if rows != 1 {
		return ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "outbox_provider_retry"})
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

func (s *CityEconomyService) finalizeRealtimeAgentDecision(
	ctx context.Context,
	worldID int64,
	workerID string,
	requestCode string,
	attempt cityRealtimeAgentDecisionAttemptRecord,
	envelope cityRealtimeAgentDecisionEnvelope,
) (*CityRealtimeAgentDecisionRunResult, error) {
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
	executionProfile, err := cityRealtimeAgentDecisionExecutionProfile(ctx, tx, request)
	if err != nil {
		return nil, err
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
	if err = enableCityRealtimeAgentModelBudgetWorkerGate(ctx, tx, worldID, requestCode); err != nil {
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
	if executionProfile != nil {
		if err = closeCityRealtimeAgentModelCircuitBreaker(ctx, tx, executionProfile, time.Now().UTC()); err != nil {
			return nil, err
		}
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
	result := &CityRealtimeAgentDecisionRunResult{
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
) (*CityRealtimeAgentDecisionRunResult, error) {
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
	executionProfile, err := cityRealtimeAgentDecisionExecutionProfile(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	maximumAttempts := cityRealtimeAgentDecisionMaximumAttemptsForProfile(executionProfile)
	now := time.Now().UTC()
	if request.Status != cityRealtimeAgentDecisionRequestLeased || request.LeaseExpiresAt == nil ||
		request.LeaseExpiresAt.After(now) || request.AttemptCount < maximumAttempts {
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
	result := &CityRealtimeAgentDecisionRunResult{
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

const (
	cityRealtimeAgentDecisionProviderInitialRetryDelay = 5 * time.Second
	cityRealtimeAgentDecisionProviderMaximumRetryDelay = 2 * time.Minute
	cityRealtimeAgentDecisionProviderFinalizerTimeout  = 15 * time.Second
)

type cityRealtimeAgentDecisionProviderResolution struct {
	request  cityRealtimeAgentDecisionRequestRecord
	profile  *CityRealtimeAgentModelProfile
	provider CityRealtimeAgentDecisionProvider
}

type cityRealtimeAgentDecisionRunOptions struct {
	expectedProviderCode string
	fakePreferredAction  string
}

func cityRealtimeAgentDecisionProviderRetryDelay(attemptNumber int) time.Duration {
	if attemptNumber <= 1 {
		return cityRealtimeAgentDecisionProviderInitialRetryDelay
	}
	delay := cityRealtimeAgentDecisionProviderInitialRetryDelay
	for index := 1; index < attemptNumber && delay < cityRealtimeAgentDecisionProviderMaximumRetryDelay; index++ {
		if delay > cityRealtimeAgentDecisionProviderMaximumRetryDelay/2 {
			return cityRealtimeAgentDecisionProviderMaximumRetryDelay
		}
		delay *= 2
	}
	if delay > cityRealtimeAgentDecisionProviderMaximumRetryDelay {
		return cityRealtimeAgentDecisionProviderMaximumRetryDelay
	}
	return delay
}

func cityRealtimeAgentDecisionProviderTimeout(profile *CityRealtimeAgentModelProfile) time.Duration {
	if profile == nil || profile.TimeoutMS <= 0 {
		return cityRealtimeAgentDecisionLeaseDuration
	}
	return time.Duration(profile.TimeoutMS) * time.Millisecond
}

func cityRealtimeAgentDecisionFinalizerContext(ctx context.Context) (context.Context, context.CancelFunc) {
	// A provider timeout/cancellation must not strand an already-started attempt.
	// Keep request values (including the trusted worker identity) while detaching
	// the provider call's cancellation signal and bounding the cleanup itself.
	return context.WithTimeout(context.WithoutCancel(ctx), cityRealtimeAgentDecisionProviderFinalizerTimeout)
}

func cityRealtimeAgentDecisionRetryNotBeforeCopy(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := value.UTC().Truncate(time.Microsecond)
	return &result
}

func (s *CityEconomyService) resolveRealtimeAgentDecisionProvider(
	ctx context.Context,
	worldID int64,
	requestCode string,
	expectedProviderCode string,
) (cityRealtimeAgentDecisionProviderResolution, error) {
	if s == nil || s.db == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) {
		return cityRealtimeAgentDecisionProviderResolution{}, ErrCityInvalidInput
	}
	request, found, err := loadCityRealtimeAgentDecisionRequest(ctx, s.db, worldID, requestCode, false)
	if err != nil {
		return cityRealtimeAgentDecisionProviderResolution{}, err
	}
	if !found {
		return cityRealtimeAgentDecisionProviderResolution{}, ErrCityRealtimeAgentDecisionNotFound
	}
	resolution := cityRealtimeAgentDecisionProviderResolution{request: request}
	if cityRealtimeAgentDecisionRequestStatusTerminal(request.Status) ||
		(request.Status == cityRealtimeAgentDecisionRequestQueued && request.RetryNotBefore != nil && request.RetryNotBefore.After(time.Now().UTC())) {
		return resolution, nil
	}
	profile, err := cityRealtimeAgentDecisionExecutionProfile(ctx, s.db, request)
	if err != nil {
		return cityRealtimeAgentDecisionProviderResolution{}, err
	}
	resolution.profile = profile
	providerCode := cityRealtimeAgentDecisionProviderCode(profile)
	if expectedProviderCode != "" && providerCode != expectedProviderCode {
		return cityRealtimeAgentDecisionProviderResolution{},
			ErrCityRealtimeAgentProviderUnavailable.WithMetadata(map[string]string{"provider_code": providerCode})
	}
	provider, providerFound := s.cityRealtimeAgentDecisionProvider(providerCode)
	if !providerFound || provider == nil || strings.ToLower(strings.TrimSpace(provider.ProviderCode())) != providerCode {
		return cityRealtimeAgentDecisionProviderResolution{},
			ErrCityRealtimeAgentProviderUnavailable.WithMetadata(map[string]string{"provider_code": providerCode})
	}
	resolution.provider = provider
	return resolution, nil
}

func (s *CityEconomyService) finalizeRealtimeAgentDecisionProviderFailure(
	ctx context.Context,
	worldID int64,
	workerID string,
	requestCode string,
	attempt cityRealtimeAgentDecisionAttemptRecord,
	errorCode string,
) (*CityRealtimeAgentDecisionRunResult, error) {
	if s == nil || s.db == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(workerID, 64) ||
		!cityRealtimeAgentIdentifierValid(requestCode, 96) || !cityRealtimeAgentIdentifierValid(attempt.AttemptCode, 96) ||
		!cityRealtimeAgentProviderErrorCodeValid(errorCode) {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent provider failure finalize transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(worldID)); err != nil {
		return nil, fmt.Errorf("lock realtime agent provider failure world: %w", err)
	}
	request, found, err := loadCityRealtimeAgentDecisionRequest(ctx, tx, worldID, requestCode, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeAgentDecisionNotFound
	}
	if request.Status != cityRealtimeAgentDecisionRequestLeased || request.LeaseOwner == nil || *request.LeaseOwner != workerID ||
		request.LeaseExpiresAt == nil || !request.LeaseExpiresAt.After(time.Now().UTC()) {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_lease"})
	}
	currentAttempt, attemptFound, err := loadCityRealtimeAgentDecisionAttemptForUpdate(
		ctx, tx, worldID, requestCode, request.AttemptCount,
	)
	if err != nil {
		return nil, err
	}
	if !attemptFound || currentAttempt.Status != cityRealtimeAgentDecisionAttemptStarted ||
		currentAttempt.AttemptCode != attempt.AttemptCode || currentAttempt.ProviderCode != attempt.ProviderCode {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "attempt"})
	}
	profile, err := cityRealtimeAgentDecisionExecutionProfile(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	if currentAttempt.ProviderCode != cityRealtimeAgentDecisionProviderCode(profile) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_provider_attempt"})
	}
	if err = enableCityRealtimeAgentModelBudgetWorkerGate(ctx, tx, worldID, requestCode); err != nil {
		return nil, err
	}
	if err = updateCityRealtimeAgentDecisionAttemptFailed(ctx, tx, worldID, currentAttempt.AttemptCode, errorCode); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if profile != nil && cityRealtimeAgentModelProviderFailureAttributable(errorCode) {
		if err = recordCityRealtimeAgentModelProviderFailure(ctx, tx, profile, errorCode, now); err != nil {
			return nil, err
		}
	}
	if cityRealtimeAgentProviderFailureRetryable(errorCode) &&
		request.AttemptCount < cityRealtimeAgentDecisionMaximumAttemptsForProfile(profile) {
		retryNotBefore := now.Add(cityRealtimeAgentDecisionProviderRetryDelay(request.AttemptCount)).UTC().Truncate(time.Microsecond)
		if err = requeueCityRealtimeAgentDecisionAfterProviderFailure(ctx, tx, worldID, requestCode, retryNotBefore); err != nil {
			return nil, err
		}
		if err = requeueCityRealtimeAgentDecisionOutboxAfterProviderFailure(ctx, tx, worldID, requestCode); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit realtime agent provider retry: %w", err)
		}
		return &CityRealtimeAgentDecisionRunResult{
			RequestCode:    requestCode,
			Status:         cityRealtimeAgentDecisionRequestQueued,
			ErrorCode:      errorCode,
			RetryNotBefore: &retryNotBefore,
		}, nil
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
	frameSequence, cursor, err := cityRealtimeCharacterNextFrame(state)
	if err != nil {
		return nil, err
	}
	if err = enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); err != nil {
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
	result := &CityRealtimeAgentDecisionRunResult{
		RequestCode: requestCode,
		Status:      cityRealtimeAgentDecisionRequestFailed,
		ErrorCode:   errorCode,
	}
	if result.Frame, err = sealCityRealtimeCharacterFrame(
		ctx, tx, worldID, world, state, frameSequence, cursor, "agent.decision.failed",
		map[string]any{
			"agent_decision_failed":           1,
			"agent_decision_attempt_failed":   1,
			"agent_decision_provider_failure": boolToCityRealtimeCount(cityRealtimeAgentModelProviderFailureAttributable(errorCode)),
			"agent_intent_scheduled":          0,
		},
	); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent provider terminal failure: %w", err)
	}
	return result, nil
}

// RunRealtimeAgentDecision processes one already-queued request through the
// profile-selected trusted provider. It is worker-only and does not expose a
// browser endpoint. The provider executes outside every SQL transaction; the
// result is parsed strictly and finalized through the same sealed Decision /
// Intent reducer path used by the deterministic adapter.
func (s *CityEconomyService) RunRealtimeAgentDecision(
	ctx context.Context,
	input CityRealtimeAgentDecisionRunInput,
) (*CityRealtimeAgentDecisionRunResult, error) {
	return s.runRealtimeAgentDecision(ctx, input, cityRealtimeAgentDecisionRunOptions{})
}

func (s *CityEconomyService) runRealtimeAgentDecision(
	ctx context.Context,
	input CityRealtimeAgentDecisionRunInput,
	options cityRealtimeAgentDecisionRunOptions,
) (*CityRealtimeAgentDecisionRunResult, error) {
	requestCode := strings.TrimSpace(input.RequestCode)
	workerID := strings.TrimSpace(input.WorkerID)
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if s == nil || s.db == nil || input.WorldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) ||
		!cityRealtimeAgentIdentifierValid(workerID, 64) ||
		(options.expectedProviderCode != "" && !cityRealtimeAgentIdentifierValid(options.expectedProviderCode, 64)) ||
		!cityRealtimeAgentFakePreferredActionValid(options.fakePreferredAction) {
		return nil, ErrCityInvalidInput
	}
	quarantined, err := cityRealtimeAgentDecisionQuarantined(ctx, s.db, input.WorldID, requestCode, false)
	if err != nil {
		return nil, err
	}
	if quarantined {
		return nil, ErrCityRealtimeAgentDecisionQuarantined
	}
	resolution, err := s.resolveRealtimeAgentDecisionProvider(
		ctx, input.WorldID, requestCode, options.expectedProviderCode,
	)
	if err != nil {
		return nil, err
	}
	if cityRealtimeAgentDecisionRequestStatusTerminal(resolution.request.Status) {
		return &CityRealtimeAgentDecisionRunResult{RequestCode: requestCode, Status: resolution.request.Status}, nil
	}
	if resolution.request.Status == cityRealtimeAgentDecisionRequestQueued && resolution.request.RetryNotBefore != nil &&
		resolution.request.RetryNotBefore.After(time.Now().UTC()) {
		return &CityRealtimeAgentDecisionRunResult{
			RequestCode:    requestCode,
			Status:         cityRealtimeAgentDecisionRequestQueued,
			RetryNotBefore: cityRealtimeAgentDecisionRetryNotBeforeCopy(resolution.request.RetryNotBefore),
		}, nil
	}
	if resolution.provider == nil {
		return nil, ErrCityRealtimeAgentProviderUnavailable.WithMetadata(map[string]string{
			"provider_code": cityRealtimeAgentDecisionProviderCode(resolution.profile),
		})
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent decision lease transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(input.WorldID)); err != nil {
		return nil, fmt.Errorf("lock realtime agent decision world: %w", err)
	}
	request, observation, attempt, err := updateCityRealtimeAgentDecisionRequestLease(
		ctx, tx, input.WorldID, requestCode, workerID, cityRealtimeAgentDecisionProviderCode(resolution.profile),
		time.Now().UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		if errors.Is(err, errCityRealtimeAgentDecisionLeaseBudgetExhausted) {
			_ = tx.Rollback()
			return s.finalizeRealtimeAgentDecisionLeaseBudget(ctx, input.WorldID, requestCode)
		}
		if errors.Is(err, errCityRealtimeAgentDecisionRetryNotBefore) {
			return &CityRealtimeAgentDecisionRunResult{
				RequestCode:    requestCode,
				Status:         cityRealtimeAgentDecisionRequestQueued,
				RetryNotBefore: cityRealtimeAgentDecisionRetryNotBeforeCopy(request.RetryNotBefore),
			}, nil
		}
		return nil, err
	}
	if cityRealtimeAgentDecisionRequestStatusTerminal(request.Status) {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit realtime agent decision replay: %w", err)
		}
		return &CityRealtimeAgentDecisionRunResult{RequestCode: requestCode, Status: request.Status}, nil
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent decision lease: %w", err)
	}

	providerRequest := cityRealtimeAgentDecisionProviderRequest(
		input.WorldID, request, observation, attempt, resolution.profile, options.fakePreferredAction,
	)
	providerCtx, cancelProvider := context.WithTimeout(ctx, cityRealtimeAgentDecisionProviderTimeout(resolution.profile))
	providerResponse, providerErr := cityRealtimeAgentExecuteDecisionProvider(providerCtx, resolution.provider, providerRequest)
	if providerErr == nil && providerCtx.Err() != nil {
		providerErr = providerCtx.Err()
	}
	cancelProvider()
	finalizerCtx, cancelFinalizer := cityRealtimeAgentDecisionFinalizerContext(ctx)
	defer cancelFinalizer()
	if providerErr != nil {
		return s.finalizeRealtimeAgentDecisionProviderFailure(
			finalizerCtx, input.WorldID, workerID, requestCode, attempt,
			cityRealtimeAgentProviderErrorCodeFrom(providerErr),
		)
	}
	envelope, decodeErr := decodeCityRealtimeAgentProviderDecisionEnvelope(providerResponse.DecisionEnvelope)
	if decodeErr != nil {
		return s.finalizeRealtimeAgentDecisionProviderFailure(
			finalizerCtx, input.WorldID, workerID, requestCode, attempt,
			cityRealtimeAgentProviderErrorCodeFrom(decodeErr),
		)
	}
	result, finalizeErr := s.finalizeRealtimeAgentDecision(
		finalizerCtx, input.WorldID, workerID, requestCode, attempt, envelope,
	)
	if errors.Is(finalizeErr, ErrCityRealtimeAgentDecisionUnavailable) || errors.Is(finalizeErr, ErrCityInvalidInput) {
		return s.finalizeRealtimeAgentDecisionProviderFailure(
			finalizerCtx, input.WorldID, workerID, requestCode, attempt,
			cityRealtimeAgentProviderErrorInvalidResponse,
		)
	}
	return result, finalizeErr
}

// RunRealtimeAgentFakeDecision is the deterministic compatibility wrapper.
// It keeps the existing narrow preferred-action test hook, but now crosses the
// exact same provider registry, timeout, parser, retry and finalizer boundary
// as every future external model adapter.
func (s *CityEconomyService) RunRealtimeAgentFakeDecision(
	ctx context.Context,
	input CityRealtimeAgentFakeDecisionRunInput,
) (*CityRealtimeAgentFakeDecisionRunResult, error) {
	preferredAction := strings.TrimSpace(input.PreferredAction)
	if !cityRealtimeAgentFakePreferredActionValid(preferredAction) {
		return nil, ErrCityInvalidInput
	}
	return s.runRealtimeAgentDecision(ctx, CityRealtimeAgentDecisionRunInput{
		WorldID:     input.WorldID,
		RequestCode: input.RequestCode,
		WorkerID:    input.WorkerID,
	}, cityRealtimeAgentDecisionRunOptions{
		expectedProviderCode: cityRealtimeAgentFakeProviderCode,
		fakePreferredAction:  preferredAction,
	})
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
	case cityRealtimeAgentIntentActionNavigation:
		if !cityRealtimeCharacterNavigationPlanRuntimeEnabled(*agentState.Binding) ||
			agent.AgentSubtype != "character.user" || agent.ActorCode == nil || intent.ActorCode == nil ||
			*intent.ActorCode != *agent.ActorCode {
			return markStale()
		}
		portalCode, portalCodeErr := cityRealtimeAgentDecisionNavigationPortalCodeFromRawArguments(intent.Arguments)
		if portalCodeErr != nil {
			return markStale()
		}
		record, recordFound, recordErr := loadCityRealtimeCharacterNavigationRecordForUpdate(ctx, tx, worldID, *agent.ActorCode)
		if recordErr != nil {
			return true, false, recordErr
		}
		if !recordFound || record.identity.LifecycleStatus != "active" || record.state.Z != cityspatial.SurfaceZ {
			return markStale()
		}
		runtime, runtimeErr := loadCityRealtimeCharacterNavigationPlanRuntime(ctx, tx, worldID)
		if runtimeErr != nil {
			return true, false, runtimeErr
		}
		if runtime == nil || runtime.Binding.AgentBindingHash != agentState.Binding.BindingHash {
			return markStale()
		}
		if _, activeFound, activeErr := loadCityRealtimeCharacterActiveNavigationPlan(ctx, tx, worldID, *agent.ActorCode, true); activeErr != nil {
			return true, false, activeErr
		} else if activeFound {
			return markStale()
		}
		destinations, availabilityErr := cityRealtimeCharacterAvailableNavigationDestinations(
			ctx, tx, worldID, *agent.ActorCode, record.state, *agentState.Binding,
		)
		if availabilityErr != nil {
			return true, false, availabilityErr
		}
		destination, destinationFound := cityRealtimeCharacterNavigationDestinationByPortalCode(destinations, portalCode)
		if !destinationFound {
			return markStale()
		}
		head, planEvent, planErr := cityRealtimeCharacterNavigationPlanNew(
			record.state, destination, intent.IntentCode, frameSequence, event.DueWorldTimeUS,
		)
		if planErr != nil {
			return markStale()
		}
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return true, false, gateErr
		}
		if gateErr := enableCityRealtimeCharacterNavigationPlanMutationGate(ctx, tx, worldID, frameSequence, event.DueWorldTimeUS); gateErr != nil {
			return true, false, gateErr
		}
		// Create the future movement fact before the active head.  The SQL
		// guard enforces that an accepted plan can never become visible without
		// its next authoritative movement boundary.
		if scheduleErr := scheduleCityRealtimeCharacterNavigationPlanStepDueEvent(ctx, tx, worldID, frameSequence, head); scheduleErr != nil {
			return true, false, scheduleErr
		}
		if insertErr := insertCityRealtimeCharacterNavigationPlanHead(ctx, tx, worldID, head); insertErr != nil {
			return true, false, insertErr
		}
		if insertErr := insertCityRealtimeCharacterNavigationPlanEvent(ctx, tx, worldID, planEvent); insertErr != nil {
			return true, false, insertErr
		}
		if updateErr := updateCityRealtimeAgentIntentTerminal(ctx, tx, worldID, intent.IntentCode, cityRealtimeAgentIntentApplied, frameSequence); updateErr != nil {
			return true, false, updateErr
		}
		// Navigation owns the continuation schedule. Its terminal reducer is
		// the sole place that returns the Agent to the decision loop.
		return true, true, nil
	case cityRealtimeAgentIntentActionTask:
		if !cityRealtimeAgentCharacterTaskRuntimeEnabled(*agentState.Binding) ||
			agent.AgentSubtype != "character.user" || agent.ActorCode == nil || intent.ActorCode == nil ||
			*intent.ActorCode != *agent.ActorCode {
			return markStale()
		}
		taskCode, taskCodeErr := cityRealtimeAgentDecisionTaskCodeFromRawArguments(intent.Arguments)
		if taskCodeErr != nil {
			return markStale()
		}
		actorState, actorStateErr := loadCityRealtimeAgentDecisionActorState(ctx, tx, worldID, agent)
		if actorStateErr != nil {
			return true, false, actorStateErr
		}
		lifeRuntime, lifeRuntimeErr := loadCityRealtimeCharacterLifeRuntime(ctx, tx, worldID)
		if lifeRuntimeErr != nil {
			return true, false, lifeRuntimeErr
		}
		if lifeRuntime == nil {
			return markStale()
		}
		profile, profileFound, profileErr := loadCityRealtimeCharacterProfile(ctx, tx, worldID, *agent.ActorCode, true)
		if profileErr != nil {
			return true, false, profileErr
		}
		if !profileFound || !cityRealtimeCharacterProfileMatchesRuntime(profile, lifeRuntime) {
			return markStale()
		}
		taskRuntime, taskRuntimeErr := loadCityRealtimeCharacterTaskRuntime(ctx, tx, worldID)
		if taskRuntimeErr != nil {
			return true, false, taskRuntimeErr
		}
		if taskRuntime == nil || taskRuntime.Binding.AgentBindingHash != agentState.Binding.BindingHash ||
			taskRuntime.Binding.ActivityBindingHash != lifeRuntime.Binding.BindingHash {
			return markStale()
		}
		definition, definitionFound := taskRuntime.Definitions[taskCode]
		if !definitionFound {
			return markStale()
		}
		availableTaskCodes, availabilityErr := cityRealtimeCharacterAvailableTaskCodes(
			ctx, tx, worldID, event.DueWorldTimeUS, actorState, profile, lifeRuntime, *agent.ActorCode, *agentState.Binding,
		)
		if availabilityErr != nil {
			return true, false, availabilityErr
		}
		if !cityRealtimeAgentActionContextContains(availableTaskCodes, taskCode) {
			return markStale()
		}
		if _, activeFound, activeErr := loadCityRealtimeCharacterActiveTask(ctx, tx, worldID, *agent.ActorCode, true); activeErr != nil {
			return true, false, activeErr
		} else if activeFound {
			return markStale()
		}
		expirationDueWorldTimeUS, expirationErr := cityRealtimeCharacterTaskExpirationDueWorldTime(event.DueWorldTimeUS, definition)
		if expirationErr != nil {
			return true, false, expirationErr
		}
		taskHead, taskEvent, taskBuildErr := cityRealtimeCharacterAcceptTask(
			definition, *agent.ActorCode, intent.IntentCode, frameSequence, expirationDueWorldTimeUS,
		)
		if taskBuildErr != nil {
			return markStale()
		}
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return true, false, gateErr
		}
		if taskGateErr := enableCityRealtimeCharacterTaskMutationGate(ctx, tx, worldID, frameSequence, event.DueWorldTimeUS); taskGateErr != nil {
			return true, false, taskGateErr
		}
		// The expiry fact is written first so the accepted head can be proven to
		// have exactly one server-owned deadline before it becomes visible.
		if scheduleErr := scheduleCityRealtimeCharacterTaskExpiryDueEvent(ctx, tx, worldID, frameSequence, taskHead); scheduleErr != nil {
			return true, false, scheduleErr
		}
		if insertErr := insertCityRealtimeCharacterTaskHead(ctx, tx, worldID, taskHead); insertErr != nil {
			return true, false, insertErr
		}
		if insertErr := insertCityRealtimeCharacterTaskEvent(ctx, tx, worldID, taskEvent); insertErr != nil {
			return true, false, insertErr
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
		// A structured task can only complete here, after the normal Agent
		// activity event has been sealed. Manual activity commands never enter
		// this reducer, and an activity at the exact expiry timestamp remains a
		// normal activity while the task's later rule-effect event expires it.
		if cityRealtimeAgentCharacterTaskRuntimeEnabled(*agentState.Binding) {
			taskRuntime, taskRuntimeErr := loadCityRealtimeCharacterTaskRuntime(ctx, tx, worldID)
			if taskRuntimeErr != nil {
				return true, false, taskRuntimeErr
			}
			if taskRuntime == nil || taskRuntime.Binding.AgentBindingHash != agentState.Binding.BindingHash ||
				taskRuntime.Binding.ActivityBindingHash != lifeRuntime.Binding.BindingHash {
				return true, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_runtime"})
			}
			activeTask, activeTaskFound, activeTaskErr := loadCityRealtimeCharacterActiveTask(
				ctx, tx, worldID, *agent.ActorCode, true,
			)
			if activeTaskErr != nil {
				return true, false, activeTaskErr
			}
			if activeTaskFound && activeTask.ActivityCode == transition.Activity.ActivityCode &&
				event.DueWorldTimeUS < activeTask.ExpirationDueWorldTimeUS {
				if historyErr := validateCityRealtimeCharacterTaskHeadHistory(ctx, tx, worldID, activeTask); historyErr != nil {
					return true, false, historyErr
				}
				nextTask, taskEvent, taskTransitionErr := cityRealtimeCharacterCompleteTask(
					activeTask, transition.Activity, frameSequence, event.DueWorldTimeUS,
				)
				if taskTransitionErr != nil {
					return true, false, taskTransitionErr
				}
				if taskGateErr := enableCityRealtimeCharacterTaskMutationGate(ctx, tx, worldID, frameSequence, event.DueWorldTimeUS); taskGateErr != nil {
					return true, false, taskGateErr
				}
				if taskInsertErr := insertCityRealtimeCharacterTaskEvent(ctx, tx, worldID, taskEvent); taskInsertErr != nil {
					return true, false, taskInsertErr
				}
				if taskUpdateErr := updateCityRealtimeCharacterTaskHead(ctx, tx, worldID, activeTask, nextTask); taskUpdateErr != nil {
					return true, false, taskUpdateErr
				}
			}
		}
		if transition.Law != nil {
			if insertErr := insertCityRealtimeCharacterLawEvent(ctx, tx, worldID, *agent.ActorCode, *transition.Law); insertErr != nil {
				return true, false, insertErr
			}
			if _, evidenceErr := captureCityRealtimeCharacterCaseEvidenceFromLaw(
				ctx, tx, worldID, frameSequence, event.DueWorldTimeUS, *transition.Law,
			); evidenceErr != nil {
				return true, false, evidenceErr
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
	case cityRealtimeAgentIntentActionCase:
		if !cityRealtimeAgentCharacterCaseRuntimeEnabled(*agentState.Binding) ||
			agent.AgentSubtype != "character.user" || agent.ActorCode == nil || intent.ActorCode == nil ||
			*intent.ActorCode != *agent.ActorCode {
			return markStale()
		}
		caseCode, caseCodeErr := cityRealtimeAgentDecisionCaseCodeFromRawArguments(intent.Arguments)
		if caseCodeErr != nil {
			return markStale()
		}
		profile, profileFound, profileErr := loadCityRealtimeCharacterProfile(ctx, tx, worldID, *agent.ActorCode, true)
		if profileErr != nil {
			return true, false, profileErr
		}
		if !profileFound {
			return markStale()
		}
		caseRuntime, caseRuntimeErr := loadCityRealtimeCharacterCaseResponseBinding(ctx, tx, worldID)
		if caseRuntimeErr != nil {
			return true, false, caseRuntimeErr
		}
		if caseRuntime == nil || caseRuntime.AgentBindingHash != agentState.Binding.BindingHash {
			return markStale()
		}
		lawCase, lawCaseFound, lawCaseErr := loadCityRealtimeCharacterOpenLawCase(
			ctx, tx, worldID, *agent.ActorCode, caseCode, true,
		)
		if lawCaseErr != nil {
			return true, false, lawCaseErr
		}
		if !lawCaseFound {
			return markStale()
		}
		head, headFound, headErr := loadCityRealtimeCharacterCaseResponseHead(
			ctx, tx, worldID, *agent.ActorCode, true,
		)
		if headErr != nil {
			return true, false, headErr
		}
		if !headFound {
			head, headErr = newCityRealtimeCharacterCaseResponseGenesisHead(profile.ActorCode, profile.SpawnedFrameSequence)
			if headErr != nil {
				return true, false, headErr
			}
		}
		if historyErr := validateCityRealtimeCharacterCaseResponseHeadHistory(
			ctx, tx, worldID, profile, head,
		); historyErr != nil {
			return true, false, historyErr
		}
		nextHead, response, responseErr := cityRealtimeCharacterAcknowledgeLawCase(
			head, lawCase, intent.IntentCode, frameSequence,
		)
		if responseErr != nil {
			return markStale()
		}
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return true, false, gateErr
		}
		if gateErr := enableCityRealtimeCharacterCaseResponseMutationGate(ctx, tx, worldID, frameSequence); gateErr != nil {
			return true, false, gateErr
		}
		if !headFound {
			if insertErr := insertCityRealtimeCharacterCaseResponseHead(ctx, tx, worldID, head); insertErr != nil {
				return true, false, insertErr
			}
		}
		if insertErr := insertCityRealtimeCharacterCaseResponseEvent(ctx, tx, worldID, response); insertErr != nil {
			return true, false, insertErr
		}
		if updateErr := updateCityRealtimeCharacterCaseResponseHead(ctx, tx, worldID, head, nextHead); updateErr != nil {
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
	case cityRealtimeAgentIntentActionCaseReview:
		if !cityRealtimeAgentCharacterCaseReviewRuntimeEnabled(*agentState.Binding) ||
			agent.AgentSubtype != "character.user" || agent.ActorCode == nil || intent.ActorCode == nil ||
			*intent.ActorCode != *agent.ActorCode {
			return markStale()
		}
		caseCode, caseCodeErr := cityRealtimeAgentDecisionCaseReviewCodeFromRawArguments(intent.Arguments)
		if caseCodeErr != nil {
			return markStale()
		}
		reviewBinding, reviewBindingErr := loadCityRealtimeCharacterCaseReviewBinding(ctx, tx, worldID)
		if reviewBindingErr != nil {
			return true, false, reviewBindingErr
		}
		if reviewBinding == nil || reviewBinding.AgentBindingHash != agentState.Binding.BindingHash {
			return markStale()
		}
		item, itemFound, itemErr := loadCityRealtimeCharacterReviewableLawCase(
			ctx, tx, worldID, event.DueWorldTimeUS, *agent.ActorCode, caseCode, true,
		)
		if itemErr != nil {
			return true, false, itemErr
		}
		if !itemFound {
			return markStale()
		}
		head, headFound, headErr := loadCityRealtimeCharacterCaseReviewHead(
			ctx, tx, worldID, *agent.ActorCode, caseCode, true,
		)
		if headErr != nil {
			return true, false, headErr
		}
		if headFound {
			return markStale()
		}
		head, headErr = newCityRealtimeCharacterCaseReviewGenesisHead(item)
		if headErr != nil {
			return true, false, headErr
		}
		resolutionDueWorldTimeUS, dueErr := cityRealtimeCharacterCaseReviewResolutionDueWorldTime(event.DueWorldTimeUS)
		if dueErr != nil {
			return true, false, dueErr
		}
		nextHead, reviewEvent, reviewErr := cityRealtimeCharacterFileCaseReview(
			head, item, intent.IntentCode, frameSequence, resolutionDueWorldTimeUS,
		)
		if reviewErr != nil {
			return markStale()
		}
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return true, false, gateErr
		}
		if gateErr := enableCityRealtimeCharacterCaseReviewMutationGate(ctx, tx, worldID, frameSequence); gateErr != nil {
			return true, false, gateErr
		}
		// The closure is inserted before the filed event so the database-side
		// fact guard can prove that the review has exactly one sealed next step.
		if scheduleErr := scheduleCityRealtimeCharacterCaseReviewCloseDueEvent(
			ctx, tx, worldID, resolutionDueWorldTimeUS, frameSequence, item, intent.IntentCode,
		); scheduleErr != nil {
			return true, false, scheduleErr
		}
		if insertErr := insertCityRealtimeCharacterCaseReviewHead(ctx, tx, worldID, head); insertErr != nil {
			return true, false, insertErr
		}
		if insertErr := insertCityRealtimeCharacterCaseReviewEvent(ctx, tx, worldID, reviewEvent); insertErr != nil {
			return true, false, insertErr
		}
		if updateErr := updateCityRealtimeCharacterCaseReviewHead(ctx, tx, worldID, head, nextHead); updateErr != nil {
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
	case cityRealtimeAgentIntentActionCaseReport:
		if !cityRealtimeAgentCharacterCaseReportRuntimeEnabled(*agentState.Binding) ||
			agent.AgentSubtype != "character.user" || agent.ActorCode == nil || intent.ActorCode == nil ||
			*intent.ActorCode != *agent.ActorCode {
			return markStale()
		}
		targetCode, targetCodeErr := cityRealtimeAgentDecisionCaseReportTargetCodeFromRawArguments(intent.Arguments)
		if targetCodeErr != nil {
			return markStale()
		}
		actorState, actorStateErr := loadCityRealtimeAgentDecisionActorState(ctx, tx, worldID, agent)
		if actorStateErr != nil {
			return true, false, actorStateErr
		}
		target, targetFound, targetErr := loadCityRealtimeCharacterSocialTarget(ctx, tx, worldID, targetCode, true)
		if targetErr != nil {
			return true, false, targetErr
		}
		if !targetFound || target.ActorCode == actorState.ActorCode ||
			!cityRealtimeCharacterSocialTargetAllowed(actorState, target) {
			return markStale()
		}
		reportBinding, bindingErr := loadCityRealtimeCharacterCaseReportBinding(ctx, tx, worldID)
		if bindingErr != nil {
			return true, false, bindingErr
		}
		if reportBinding == nil || reportBinding.AgentBindingHash != agentState.Binding.BindingHash {
			return markStale()
		}
		if _, reportFound, reportErr := loadCityRealtimeCharacterCaseReportHead(
			ctx, tx, worldID, actorState.ActorCode, target.ActorCode, true,
		); reportErr != nil {
			return true, false, reportErr
		} else if reportFound {
			return markStale()
		}
		head, reportEvent, reportErr := cityRealtimeCharacterFileCaseReport(
			actorState.ActorCode, target.ActorCode, intent.IntentCode, frameSequence,
		)
		if reportErr != nil {
			return markStale()
		}
		var intakeHead cityRealtimeCharacterCaseIntakeHead
		var intakeEvent cityRealtimeCharacterCaseIntakeEvent
		intakeEnabled := cityRealtimeAgentCharacterCaseIntakeRuntimeEnabled(*agentState.Binding)
		if intakeEnabled {
			intakeBinding, intakeBindingErr := loadCityRealtimeCharacterCaseIntakeBinding(ctx, tx, worldID)
			if intakeBindingErr != nil {
				return true, false, intakeBindingErr
			}
			if intakeBinding == nil || intakeBinding.AgentBindingHash != agentState.Binding.BindingHash {
				return markStale()
			}
			if _, intakeFound, intakeHeadErr := loadCityRealtimeCharacterCaseIntakeHead(
				ctx, tx, worldID, actorState.ActorCode, target.ActorCode, true,
			); intakeHeadErr != nil {
				return true, false, intakeHeadErr
			} else if intakeFound {
				return markStale()
			}
			expirationDueWorldTimeUS, dueErr := cityRealtimeCharacterCaseIntakeExpirationDueWorldTime(event.DueWorldTimeUS)
			if dueErr != nil {
				return true, false, dueErr
			}
			var intakeBuildErr error
			intakeHead, intakeEvent, intakeBuildErr = cityRealtimeCharacterOpenCaseIntake(
				head, reportEvent, frameSequence, expirationDueWorldTimeUS,
			)
			if intakeBuildErr != nil {
				return markStale()
			}
		}
		var assignmentHead cityRealtimeCharacterCaseEvidenceAssignmentHead
		var assignmentEvent cityRealtimeCharacterCaseEvidenceAssignmentEvent
		assignmentCreated := false
		if cityRealtimeAgentCharacterCaseEvidenceAssignmentRuntimeEnabled(*agentState.Binding) {
			if !intakeEnabled {
				return markStale()
			}
			assignmentBinding, assignmentBindingErr := loadCityRealtimeCharacterCaseEvidenceAssignmentBinding(ctx, tx, worldID)
			if assignmentBindingErr != nil {
				return true, false, assignmentBindingErr
			}
			if assignmentBinding == nil || assignmentBinding.AgentBindingHash != agentState.Binding.BindingHash {
				return markStale()
			}
			candidate, candidateFound, candidateErr := findCityRealtimeCharacterCaseEvidenceAssignmentCandidate(
				ctx, tx, worldID, target.ActorCode, frameSequence, event.DueWorldTimeUS,
			)
			if candidateErr != nil {
				return true, false, candidateErr
			}
			if candidateFound {
				if _, assignmentFound, assignmentHeadErr := loadCityRealtimeCharacterCaseEvidenceAssignmentHead(
					ctx, tx, worldID, actorState.ActorCode, target.ActorCode, true,
				); assignmentHeadErr != nil {
					return true, false, assignmentHeadErr
				} else if assignmentFound {
					return markStale()
				}
				if _, sourceAssigned, sourceAssignmentErr := loadCityRealtimeCharacterCaseEvidenceAssignmentByEvidenceCode(
					ctx, tx, worldID, candidate.EvidenceCode, true,
				); sourceAssignmentErr != nil {
					return true, false, sourceAssignmentErr
				} else if sourceAssigned {
					return markStale()
				}
				var assignmentBuildErr error
				assignmentHead, assignmentEvent, assignmentBuildErr = cityRealtimeCharacterLinkCaseEvidenceAssignment(
					head, reportEvent, intakeHead, candidate, frameSequence, event.DueWorldTimeUS,
				)
				if assignmentBuildErr != nil {
					return markStale()
				}
				assignmentCreated = true
			}
		}
		var procedureDispatchHead cityRealtimeCharacterCaseProcedureDispatchHead
		var procedureDispatchEvent cityRealtimeCharacterCaseProcedureDispatchEvent
		procedureDispatchCreated := false
		if cityRealtimeAgentCharacterCaseProcedureDispatchRuntimeEnabled(*agentState.Binding) && assignmentCreated {
			procedureDispatchBinding, procedureDispatchBindingErr := loadCityRealtimeCharacterCaseProcedureDispatchBinding(ctx, tx, worldID)
			if procedureDispatchBindingErr != nil {
				return true, false, procedureDispatchBindingErr
			}
			if procedureDispatchBinding == nil || procedureDispatchBinding.AgentBindingHash != agentState.Binding.BindingHash {
				return markStale()
			}
			var procedureDispatchBuildErr error
			procedureDispatchHead, procedureDispatchEvent, procedureDispatchBuildErr = cityRealtimeCharacterQueueCaseProcedureDispatch(
				assignmentHead, frameSequence,
			)
			if procedureDispatchBuildErr != nil {
				return markStale()
			}
			procedureDispatchCreated = true
		}
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return true, false, gateErr
		}
		if gateErr := enableCityRealtimeCharacterCaseReportMutationGate(ctx, tx, worldID, frameSequence); gateErr != nil {
			return true, false, gateErr
		}
		if intakeEnabled {
			if gateErr := enableCityRealtimeCharacterCaseIntakeMutationGate(ctx, tx, worldID, frameSequence); gateErr != nil {
				return true, false, gateErr
			}
			// The expiry is persisted before the intake fact. The database guard
			// can therefore prove that evidence_required is not an unbounded
			// workflow state and has exactly one server-owned next step.
			if scheduleErr := scheduleCityRealtimeCharacterCaseIntakeExpiryDueEvent(
				ctx, tx, worldID, intakeHead.ExpirationDueWorldTimeUS, frameSequence, intakeHead,
			); scheduleErr != nil {
				return true, false, scheduleErr
			}
		}
		if assignmentCreated {
			if gateErr := enableCityRealtimeCharacterCaseEvidenceAssignmentMutationGate(ctx, tx, worldID, frameSequence); gateErr != nil {
				return true, false, gateErr
			}
		}
		if procedureDispatchCreated {
			if gateErr := enableCityRealtimeCharacterCaseProcedureDispatchMutationGate(ctx, tx, worldID, frameSequence); gateErr != nil {
				return true, false, gateErr
			}
		}
		if insertErr := insertCityRealtimeCharacterCaseReportHead(ctx, tx, worldID, head); insertErr != nil {
			return true, false, insertErr
		}
		if insertErr := insertCityRealtimeCharacterCaseReportEvent(ctx, tx, worldID, reportEvent); insertErr != nil {
			return true, false, insertErr
		}
		if intakeEnabled {
			if insertErr := insertCityRealtimeCharacterCaseIntakeHead(ctx, tx, worldID, intakeHead); insertErr != nil {
				return true, false, insertErr
			}
			if insertErr := insertCityRealtimeCharacterCaseIntakeEvent(ctx, tx, worldID, intakeEvent); insertErr != nil {
				return true, false, insertErr
			}
		}
		if assignmentCreated {
			if insertErr := insertCityRealtimeCharacterCaseEvidenceAssignmentHead(ctx, tx, worldID, assignmentHead); insertErr != nil {
				return true, false, insertErr
			}
			if insertErr := insertCityRealtimeCharacterCaseEvidenceAssignmentEvent(ctx, tx, worldID, assignmentEvent); insertErr != nil {
				return true, false, insertErr
			}
		}
		if procedureDispatchCreated {
			if insertErr := insertCityRealtimeCharacterCaseProcedureDispatchHead(ctx, tx, worldID, procedureDispatchHead); insertErr != nil {
				return true, false, insertErr
			}
			if insertErr := insertCityRealtimeCharacterCaseProcedureDispatchEvent(ctx, tx, worldID, procedureDispatchEvent); insertErr != nil {
				return true, false, insertErr
			}
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
	case cityRealtimeAgentIntentActionSocial:
		if !cityRealtimeAgentCharacterSocialRuntimeEnabled(*agentState.Binding) ||
			agent.AgentSubtype != "character.user" || agent.ActorCode == nil || intent.ActorCode == nil ||
			*intent.ActorCode != *agent.ActorCode {
			return markStale()
		}
		targetCode, targetCodeErr := cityRealtimeAgentDecisionSocialTargetCodeFromRawArguments(intent.Arguments)
		if targetCodeErr != nil {
			return markStale()
		}
		actorState, actorStateErr := loadCityRealtimeAgentDecisionActorState(ctx, tx, worldID, agent)
		if actorStateErr != nil {
			return true, false, actorStateErr
		}
		target, targetFound, targetErr := loadCityRealtimeCharacterSocialTarget(ctx, tx, worldID, targetCode, true)
		if targetErr != nil {
			return true, false, targetErr
		}
		if !targetFound || target.ActorCode == actorState.ActorCode ||
			!cityRealtimeCharacterSocialTargetAllowed(actorState, target) {
			return markStale()
		}
		socialBinding, bindingErr := loadCityRealtimeCharacterSocialBinding(ctx, tx, worldID)
		if bindingErr != nil {
			return true, false, bindingErr
		}
		if socialBinding == nil || socialBinding.AgentBindingHash != agentState.Binding.BindingHash {
			return markStale()
		}
		lowCode, highCode, pairErr := cityRealtimeCharacterSocialPair(actorState.ActorCode, target.ActorCode)
		if pairErr != nil {
			return markStale()
		}
		head, headFound, headErr := loadCityRealtimeCharacterSocialHead(ctx, tx, worldID, lowCode, highCode, true)
		if headErr != nil {
			return true, false, headErr
		}
		if !headFound {
			head, headErr = newCityRealtimeCharacterSocialGenesisHead(lowCode, highCode)
			if headErr != nil {
				return true, false, headErr
			}
		}
		if historyErr := validateCityRealtimeCharacterSocialHeadHistory(ctx, tx, worldID, head); historyErr != nil {
			return true, false, historyErr
		}
		nextHead, socialEvent, transitionErr := cityRealtimeCharacterSocialGreet(
			head, actorState.ActorCode, target.ActorCode, intent.IntentCode, frameSequence,
		)
		if transitionErr != nil {
			return markStale()
		}
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return true, false, gateErr
		}
		if gateErr := enableCityRealtimeCharacterSocialMutationGate(ctx, tx, worldID, frameSequence); gateErr != nil {
			return true, false, gateErr
		}
		if !headFound {
			if insertErr := insertCityRealtimeCharacterSocialHead(ctx, tx, worldID, head); insertErr != nil {
				return true, false, insertErr
			}
		}
		if insertErr := insertCityRealtimeCharacterSocialEvent(ctx, tx, worldID, socialEvent); insertErr != nil {
			return true, false, insertErr
		}
		if updateErr := updateCityRealtimeCharacterSocialHead(ctx, tx, worldID, head, nextHead); updateErr != nil {
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
