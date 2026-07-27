package securityaudit

import (
	"encoding/json"
	"time"
)

const (
	PolicyStatusDraft     = "draft"
	PolicyStatusValidated = "validated"
	PolicyStatusShadow    = "shadow"
	PolicyStatusActive    = "active"
	PolicyStatusRetired   = "retired"

	ActionStatusPending    = "pending"
	ActionStatusProcessing = "processing"
	ActionStatusRetry      = "retry"
	ActionStatusSucceeded  = "succeeded"
	ActionStatusFailed     = "failed"
	ActionStatusCancelled  = "cancelled"
	ActionStatusReverted   = "reverted"
)

type PolicyScope struct {
	AllGroups bool     `json:"all_groups"`
	GroupIDs  []int64  `json:"group_ids"`
	UserIDs   []int64  `json:"user_ids"`
	APIKeyIDs []int64  `json:"api_key_ids"`
	Protocols []string `json:"protocols"`
	Endpoints []string `json:"endpoints"`
	Models    []string `json:"models"`
}

type PolicyDetector struct {
	ID        string `json:"id"`
	Enabled   bool   `json:"enabled"`
	TimeoutMS int    `json:"timeout_ms"`
}

type PolicyFailure struct {
	LocalError    FailureMode `json:"local_error"`
	RemoteTimeout FailureMode `json:"remote_timeout"`
	RemoteInvalid FailureMode `json:"remote_invalid"`
}

type PolicyActions struct {
	Low      []string `json:"low"`
	Medium   []string `json:"medium"`
	High     []string `json:"high"`
	Critical []string `json:"critical"`
}

type PolicyEvidence struct {
	Mode          string `json:"mode"`
	RetentionDays int    `json:"retention_days"`
}

type BehaviorSignalRule struct {
	ID             string  `json:"id"`
	Enabled        bool    `json:"enabled"`
	Metric         string  `json:"metric"`
	SubjectType    string  `json:"subject_type"`
	WindowMinutes  int     `json:"window_minutes"`
	Threshold      float64 `json:"threshold"`
	MinimumSamples int64   `json:"minimum_samples"`
	Severity       string  `json:"severity"`
}

type PolicySignals struct {
	Enabled bool                 `json:"enabled"`
	Rules   []BehaviorSignalRule `json:"rules"`
}

type SecurityPolicyConfig struct {
	Name      string           `json:"name"`
	Priority  int              `json:"priority"`
	Scope     PolicyScope      `json:"scope"`
	Mode      Mode             `json:"mode"`
	Detectors []PolicyDetector `json:"detectors"`
	Failure   PolicyFailure    `json:"failure"`
	Actions   PolicyActions    `json:"actions"`
	Evidence  PolicyEvidence   `json:"evidence"`
	Signals   PolicySignals    `json:"signals"`
}

type PolicyVersion struct {
	ID               int64                `json:"id"`
	PolicyKey        string               `json:"policy_key"`
	Version          int64                `json:"version"`
	Name             string               `json:"name"`
	Status           string               `json:"status"`
	Priority         int                  `json:"priority"`
	Config           SecurityPolicyConfig `json:"config"`
	ConfigDigest     string               `json:"config_digest"`
	ValidationErrors []string             `json:"validation_errors"`
	ChangeReason     string               `json:"change_reason"`
	CreatedBy        *int64               `json:"created_by,omitempty"`
	CreatedAt        time.Time            `json:"created_at"`
	ValidatedAt      *time.Time           `json:"validated_at,omitempty"`
	ShadowedAt       *time.Time           `json:"shadowed_at,omitempty"`
	ActivatedAt      *time.Time           `json:"activated_at,omitempty"`
	RetiredAt        *time.Time           `json:"retired_at,omitempty"`
}

type PolicyTransition struct {
	ID              int64     `json:"id"`
	PolicyVersionID int64     `json:"policy_version_id"`
	PolicyKey       string    `json:"policy_key"`
	Version         int64     `json:"version"`
	FromStatus      string    `json:"from_status"`
	ToStatus        string    `json:"to_status"`
	ActorID         *int64    `json:"actor_id,omitempty"`
	Reason          string    `json:"reason"`
	CreatedAt       time.Time `json:"created_at"`
}

type PolicySummary struct {
	PolicyKey    string    `json:"policy_key"`
	Name         string    `json:"name"`
	Latest       int64     `json:"latest_version"`
	Active       *int64    `json:"active_version,omitempty"`
	Shadow       *int64    `json:"shadow_version,omitempty"`
	Status       string    `json:"status"`
	Priority     int       `json:"priority"`
	VersionCount int       `json:"version_count"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CreatePolicyRequest struct {
	PolicyKey    string               `json:"policy_key"`
	Config       SecurityPolicyConfig `json:"config"`
	ChangeReason string               `json:"change_reason"`
}

type PolicySimulationRequest struct {
	UserID     int64  `json:"user_id"`
	APIKeyID   int64  `json:"api_key_id"`
	GroupID    *int64 `json:"group_id"`
	Protocol   string `json:"protocol"`
	Endpoint   string `json:"endpoint"`
	Model      string `json:"model"`
	RiskLevel  string `json:"risk_level"`
	Failure    string `json:"failure"`
	PromptText string `json:"prompt_text"`
}

type PolicySimulationResult struct {
	Matched          bool              `json:"matched"`
	RequestAction    string            `json:"request_action"`
	CandidateActions []string          `json:"candidate_actions"`
	FailureMode      FailureMode       `json:"failure_mode,omitempty"`
	Policy           *PolicyVersion    `json:"policy,omitempty"`
	Trace            []SimulationTrace `json:"trace"`
	Warnings         []string          `json:"warnings"`
}

type PolicyReplayRequest struct {
	WindowHours int `json:"window_hours"`
	Limit       int `json:"limit"`
}

type PolicyReplayExample struct {
	DecisionPK       int64     `json:"decision_pk"`
	DecisionID       string    `json:"decision_id"`
	SourceType       string    `json:"source_type"`
	RiskLevel        string    `json:"risk_level"`
	PreviousAction   string    `json:"previous_action"`
	ProposedAction   string    `json:"proposed_action"`
	CandidateChanged bool      `json:"candidate_changed"`
	CreatedAt        time.Time `json:"created_at"`
}

type PolicyReplayResult struct {
	PolicyKey              string                `json:"policy_key"`
	PolicyVersion          int64                 `json:"policy_version"`
	ConfigDigest           string                `json:"config_digest"`
	WindowHours            int                   `json:"window_hours"`
	RequestedLimit         int                   `json:"requested_limit"`
	Analyzed               int                   `json:"analyzed"`
	Matched                int                   `json:"matched"`
	Unmatched              int                   `json:"unmatched"`
	ActionChanges          int                   `json:"action_changes"`
	StricterChanges        int                   `json:"stricter_changes"`
	LooserChanges          int                   `json:"looser_changes"`
	CandidateActionChanges int                   `json:"candidate_action_changes"`
	BySource               map[string]int        `json:"by_source"`
	ByProposedAction       map[string]int        `json:"by_proposed_action"`
	Examples               []PolicyReplayExample `json:"examples"`
	GeneratedAt            time.Time             `json:"generated_at"`
}

type PolicyShadowEvaluation struct {
	ID                    int64           `json:"id"`
	DecisionPK            int64           `json:"decision_pk"`
	DecisionID            string          `json:"decision_id"`
	SourceType            string          `json:"source_type"`
	PolicyVersionID       int64           `json:"policy_version_id"`
	PolicyKey             string          `json:"policy_key"`
	PolicyVersion         int64           `json:"policy_version"`
	RiskLevel             string          `json:"risk_level"`
	BaselineRequestAction string          `json:"baseline_request_action"`
	ProposedRequestAction string          `json:"proposed_request_action"`
	BaselineActions       []string        `json:"baseline_actions"`
	ProposedActions       []string        `json:"proposed_actions"`
	RequestActionChanged  bool            `json:"request_action_changed"`
	ActionsChanged        bool            `json:"actions_changed"`
	CreatedAt             time.Time       `json:"created_at"`
	DecisionCreatedAt     time.Time       `json:"decision_created_at"`
	DetectorSummary       json.RawMessage `json:"detector_summary"`
}

type PolicyShadowEvaluationSummary struct {
	PolicyKey               string                   `json:"policy_key"`
	PolicyVersion           int64                    `json:"policy_version"`
	WindowHours             int64                    `json:"window_hours"`
	Total                   int64                    `json:"total"`
	RequestActionChanges    int64                    `json:"request_action_changes"`
	CandidateActionChanges  int64                    `json:"candidate_action_changes"`
	StricterChanges         int64                    `json:"stricter_changes"`
	LooserChanges           int64                    `json:"looser_changes"`
	Unchanged               int64                    `json:"unchanged"`
	LastEvaluatedDecisionPK int64                    `json:"last_evaluated_decision_pk"`
	LastEvaluatedAt         *time.Time               `json:"last_evaluated_at,omitempty"`
	LastError               string                   `json:"last_error"`
	Items                   []PolicyShadowEvaluation `json:"items"`
}

type SimulationTrace struct {
	Step    string `json:"step"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail"`
}

type DetectorEvidence struct {
	ID              int64      `json:"id"`
	DetectorID      string     `json:"detector_id"`
	DetectorVersion string     `json:"detector_version"`
	Outcome         string     `json:"outcome"`
	Category        string     `json:"category"`
	Score           float64    `json:"score"`
	Severity        string     `json:"severity"`
	SafeSummary     string     `json:"safe_summary"`
	EvidenceDigest  string     `json:"evidence_digest"`
	LatencyMS       int        `json:"latency_ms"`
	ErrorCode       string     `json:"error_code"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	HoldUntil       *time.Time `json:"hold_until,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type UnifiedDecision struct {
	ID                   int64               `json:"id"`
	DecisionID           string              `json:"decision_id"`
	AuditID              string              `json:"audit_id"`
	SourceType           string              `json:"source_type"`
	SourceEventID        *int64              `json:"source_event_id,omitempty"`
	RequestID            string              `json:"request_id"`
	Stage                string              `json:"stage"`
	UserID               *int64              `json:"user_id,omitempty"`
	UserSnapshot         string              `json:"user_snapshot"`
	APIKeyID             *int64              `json:"api_key_id,omitempty"`
	APIKeySnapshot       string              `json:"api_key_snapshot"`
	GroupID              *int64              `json:"group_id,omitempty"`
	GroupSnapshot        string              `json:"group_snapshot"`
	Provider             string              `json:"provider"`
	Endpoint             string              `json:"endpoint"`
	Protocol             string              `json:"protocol"`
	RequestedModel       string              `json:"requested_model"`
	PolicyKey            string              `json:"policy_key"`
	PolicyVersion        int64               `json:"policy_version"`
	CanonicalizerVersion string              `json:"canonicalizer_version"`
	EvaluationStatus     string              `json:"evaluation_status"`
	RiskLevel            string              `json:"risk_level"`
	RequestAction        string              `json:"request_action"`
	FailureMode          FailureMode         `json:"failure_mode,omitempty"`
	FailureReason        string              `json:"failure_reason,omitempty"`
	PromptHash           string              `json:"prompt_hash"`
	RedactedPreview      string              `json:"redacted_preview"`
	DetectorSummary      []DetectorEvidence  `json:"detector_summary"`
	CandidateActions     []string            `json:"candidate_actions"`
	DecisionDigest       string              `json:"decision_digest"`
	Evidence             []DetectorEvidence  `json:"evidence,omitempty"`
	Actions              []EnforcementAction `json:"actions,omitempty"`
	CreatedAt            time.Time           `json:"created_at"`
}

type DecisionFilter struct {
	RiskLevel        string     `json:"risk_level,omitempty"`
	RequestAction    string     `json:"request_action,omitempty"`
	EvaluationStatus string     `json:"evaluation_status,omitempty"`
	SourceType       string     `json:"source_type,omitempty"`
	UserID           *int64     `json:"user_id,omitempty"`
	APIKeyID         *int64     `json:"api_key_id,omitempty"`
	GroupID          *int64     `json:"group_id,omitempty"`
	PolicyKey        string     `json:"policy_key,omitempty"`
	Keyword          string     `json:"keyword,omitempty"`
	StartAt          *time.Time `json:"start_at,omitempty"`
	EndAt            *time.Time `json:"end_at,omitempty"`
}

type DecisionPage struct {
	Items    []*UnifiedDecision `json:"items"`
	Total    int64              `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Pages    int                `json:"pages"`
}

type EnforcementAction struct {
	ID                  int64           `json:"id"`
	ActionID            string          `json:"action_id"`
	DecisionPK          int64           `json:"decision_pk"`
	ActionType          string          `json:"action_type"`
	SubjectType         string          `json:"subject_type"`
	SubjectID           int64           `json:"subject_id"`
	Status              string          `json:"status"`
	IdempotencyKey      string          `json:"idempotency_key"`
	PolicyActionVersion int64           `json:"policy_action_version"`
	Attempts            int             `json:"attempts"`
	MaxAttempts         int             `json:"max_attempts"`
	LeaseOwner          string          `json:"lease_owner"`
	LeaseExpiresAt      *time.Time      `json:"lease_expires_at,omitempty"`
	NextAttemptAt       time.Time       `json:"next_attempt_at"`
	BeforeSnapshot      json.RawMessage `json:"before_snapshot"`
	AfterSnapshot       json.RawMessage `json:"after_snapshot"`
	ErrorCode           string          `json:"error_code"`
	ErrorMessage        string          `json:"error_message"`
	RequestedBy         *int64          `json:"requested_by,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	ProcessedAt         *time.Time      `json:"processed_at,omitempty"`
	CancelledAt         *time.Time      `json:"cancelled_at,omitempty"`
	RevertedAt          *time.Time      `json:"reverted_at,omitempty"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type ActionFilter struct {
	Status      string `json:"status,omitempty"`
	ActionType  string `json:"action_type,omitempty"`
	SubjectType string `json:"subject_type,omitempty"`
	SubjectID   *int64 `json:"subject_id,omitempty"`
	DecisionID  string `json:"decision_id,omitempty"`
}

type ActionPage struct {
	Items    []*EnforcementAction `json:"items"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
	Pages    int                  `json:"pages"`
}

type AuditCase struct {
	ID                int64            `json:"id"`
	CaseID            string           `json:"case_id"`
	PrimaryDecisionPK *int64           `json:"primary_decision_pk,omitempty"`
	Title             string           `json:"title"`
	Severity          string           `json:"severity"`
	Status            string           `json:"status"`
	AssigneeID        *int64           `json:"assignee_id,omitempty"`
	OpenedReason      string           `json:"opened_reason"`
	Resolution        string           `json:"resolution"`
	ResolutionNote    string           `json:"resolution_note"`
	Labels            []string         `json:"labels"`
	CreatedBy         *int64           `json:"created_by,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
	ResolvedAt        *time.Time       `json:"resolved_at,omitempty"`
	ExpiresAt         *time.Time       `json:"expires_at,omitempty"`
	Timeline          []AuditCaseEvent `json:"timeline,omitempty"`
}

type AuditCaseEvent struct {
	ID        int64           `json:"id"`
	EventType string          `json:"event_type"`
	ActorID   *int64          `json:"actor_id,omitempty"`
	Summary   string          `json:"summary"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"created_at"`
}

type CaseFilter struct {
	Status     string `json:"status,omitempty"`
	Severity   string `json:"severity,omitempty"`
	AssigneeID *int64 `json:"assignee_id,omitempty"`
	Keyword    string `json:"keyword,omitempty"`
}

type CasePage struct {
	Items    []*AuditCase `json:"items"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	Pages    int          `json:"pages"`
}

type CaseTransitionRequest struct {
	Status         string   `json:"status"`
	ResolutionNote string   `json:"resolution_note"`
	Labels         []string `json:"labels"`
	AssigneeID     *int64   `json:"assignee_id"`
	RevertActions  bool     `json:"revert_actions"`
}

type AuditException struct {
	ID            int64      `json:"id"`
	ExceptionID   string     `json:"exception_id"`
	Name          string     `json:"name"`
	ScopeType     string     `json:"scope_type"`
	ScopeID       string     `json:"scope_id"`
	DetectorID    string     `json:"detector_id"`
	Category      string     `json:"category"`
	Effect        string     `json:"effect"`
	Reason        string     `json:"reason"`
	Status        string     `json:"status"`
	StartsAt      time.Time  `json:"starts_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	Permanent     bool       `json:"permanent"`
	CreatedBy     *int64     `json:"created_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiredAt     *time.Time `json:"expired_at,omitempty"`
	RevokedBy     *int64     `json:"revoked_by,omitempty"`
	RevokedReason string     `json:"revoked_reason"`
}

type CreateExceptionRequest struct {
	Name       string     `json:"name"`
	ScopeType  string     `json:"scope_type"`
	ScopeID    string     `json:"scope_id"`
	DetectorID string     `json:"detector_id"`
	Category   string     `json:"category"`
	Effect     string     `json:"effect"`
	Reason     string     `json:"reason"`
	StartsAt   *time.Time `json:"starts_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Permanent  bool       `json:"permanent"`
}

type ExpireExceptionRequest struct {
	Reason string `json:"reason"`
}

type FeedbackRequest struct {
	Conclusion        string `json:"conclusion"`
	CorrectedCategory string `json:"corrected_category"`
	Note              string `json:"note"`
	CaseID            string `json:"case_id"`
}

type SecurityAuditOverview struct {
	WindowHours            int64            `json:"window_hours"`
	TotalDecisions         int64            `json:"total_decisions"`
	Allowed                int64            `json:"allowed"`
	Warned                 int64            `json:"warned"`
	Blocked                int64            `json:"blocked"`
	Degraded               int64            `json:"degraded"`
	OpenCases              int64            `json:"open_cases"`
	PendingActions         int64            `json:"pending_actions"`
	FailedActions          int64            `json:"failed_actions"`
	ActivePolicies         int64            `json:"active_policies"`
	ActiveExceptions       int64            `json:"active_exceptions"`
	BehaviorMatches        int64            `json:"behavior_matches"`
	UnreadNotifications    int64            `json:"unread_notifications"`
	DetectorP95MS          int64            `json:"detector_p95_ms"`
	OldestPendingActionSec int64            `json:"oldest_pending_action_seconds"`
	FalsePositiveCount     int64            `json:"false_positive_count"`
	FalseNegativeCount     int64            `json:"false_negative_count"`
	EvidenceRevealCount    int64            `json:"evidence_reveal_count"`
	SignalLagSeconds       int64            `json:"signal_lag_seconds"`
	SignalLastAggregatedAt *time.Time       `json:"signal_last_aggregated_at,omitempty"`
	SignalLastError        string           `json:"signal_last_error"`
	ByRisk                 map[string]int64 `json:"by_risk"`
	BySource               map[string]int64 `json:"by_source"`
	GeneratedAt            time.Time        `json:"generated_at"`
}

type BehaviorSignalWindow struct {
	ID                   int64     `json:"id"`
	BucketStart          time.Time `json:"bucket_start"`
	BucketSeconds        int       `json:"bucket_seconds"`
	SubjectType          string    `json:"subject_type"`
	SubjectID            int64     `json:"subject_id"`
	UserID               *int64    `json:"user_id,omitempty"`
	APIKeyID             *int64    `json:"api_key_id,omitempty"`
	GroupID              *int64    `json:"group_id,omitempty"`
	SubjectSnapshot      string    `json:"subject_snapshot"`
	RequestCount         int64     `json:"request_count"`
	SuccessCount         int64     `json:"success_count"`
	ErrorCount           int64     `json:"error_count"`
	BusinessLimitedCount int64     `json:"business_limited_count"`
	TokenCount           int64     `json:"token_count"`
	ActualCost           float64   `json:"actual_cost"`
	DurationSumMS        int64     `json:"duration_sum_ms"`
	DurationSampleCount  int64     `json:"duration_sample_count"`
	DurationMaxMS        int       `json:"duration_max_ms"`
	DistinctIPCount      int       `json:"distinct_ip_count"`
	DistinctModelCount   int       `json:"distinct_model_count"`
	MatchedRules         int       `json:"matched_rules"`
	HighestSeverity      string    `json:"highest_severity"`
	ComputedAt           time.Time `json:"computed_at"`
}

type BehaviorSignalFilter struct {
	SubjectType string
	SubjectID   *int64
	MatchedOnly bool
	StartAt     *time.Time
	EndAt       *time.Time
}

type BehaviorSignalPage struct {
	Items    []*BehaviorSignalWindow `json:"items"`
	Total    int64                   `json:"total"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"page_size"`
	Pages    int                     `json:"pages"`
}

type SecurityAuditNotification struct {
	ID              int64      `json:"id"`
	NotificationID  string     `json:"notification_id"`
	ActionID        int64      `json:"action_id"`
	DecisionPK      int64      `json:"decision_pk"`
	Audience        string     `json:"audience"`
	RecipientUserID *int64     `json:"recipient_user_id,omitempty"`
	Severity        string     `json:"severity"`
	Title           string     `json:"title"`
	Body            string     `json:"body"`
	Status          string     `json:"status"`
	ReadAt          *time.Time `json:"read_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// UserSecurityAuditNotification is the deliberately reduced notification view
// exposed to the notification recipient. Internal decision/action identifiers
// and recipient metadata remain admin-only.
type UserSecurityAuditNotification struct {
	ID             int64      `json:"id"`
	NotificationID string     `json:"notification_id"`
	Severity       string     `json:"severity"`
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	Status         string     `json:"status"`
	ReadAt         *time.Time `json:"read_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

func (notification *SecurityAuditNotification) UserView() UserSecurityAuditNotification {
	return UserSecurityAuditNotification{
		ID:             notification.ID,
		NotificationID: notification.NotificationID,
		Severity:       notification.Severity,
		Title:          notification.Title,
		Body:           notification.Body,
		Status:         notification.Status,
		ReadAt:         notification.ReadAt,
		CreatedAt:      notification.CreatedAt,
	}
}

type EndpointHealth struct {
	EndpointID           string     `json:"endpoint_id"`
	NetworkScope         string     `json:"network_scope"`
	Status               string     `json:"status"`
	BreakerState         string     `json:"breaker_state"`
	ConsecutiveFailures  int        `json:"consecutive_failures"`
	RequestCount         int64      `json:"request_count"`
	SuccessCount         int64      `json:"success_count"`
	TimeoutCount         int64      `json:"timeout_count"`
	RateLimitedCount     int64      `json:"rate_limited_count"`
	ServerErrorCount     int64      `json:"server_error_count"`
	InvalidResponseCount int64      `json:"invalid_response_count"`
	LatencySumMS         int64      `json:"latency_sum_ms"`
	LatencyMaxMS         int        `json:"latency_max_ms"`
	LatencyMS            int        `json:"latency_ms"`
	HTTPStatus           int        `json:"http_status"`
	ErrorCode            string     `json:"error_code"`
	CheckedAt            *time.Time `json:"checked_at,omitempty"`
	BreakerOpenedAt      *time.Time `json:"breaker_opened_at,omitempty"`
	UpdatedAt            time.Time  `json:"updated_at"`
}
