package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	cityRealtimeAgentDecisionQueueDefaultLimit           = 50
	cityRealtimeAgentDecisionQueueMaximumLimit           = 100
	cityRealtimeAgentDecisionDeadLetterEventDefaultLimit = 50
	cityRealtimeAgentDecisionDeadLetterEventMaximumLimit = 100

	cityRealtimeAgentDecisionQueueStatusActive   = "active"
	cityRealtimeAgentDecisionQueueStatusQueued   = "queued"
	cityRealtimeAgentDecisionQueueStatusLeased   = "leased"
	cityRealtimeAgentDecisionQueueStatusTerminal = "terminal"
	cityRealtimeAgentDecisionQueueStatusAll      = "all"

	cityRealtimeAgentDecisionDeadLetterStatusQuarantined = "quarantined"
	cityRealtimeAgentDecisionDeadLetterStatusReleased    = "released"

	cityRealtimeAgentDecisionDeadLetterReasonOperatorReview        = "operator_review"
	cityRealtimeAgentDecisionDeadLetterReasonProviderConfiguration = "provider_configuration"
	cityRealtimeAgentDecisionDeadLetterReasonProviderIncident      = "provider_incident"
	cityRealtimeAgentDecisionDeadLetterReasonBudgetReview          = "budget_review"
	cityRealtimeAgentDecisionDeadLetterReasonWorldMaintenance      = "world_maintenance"
	cityRealtimeAgentDecisionDeadLetterReasonOperatorRelease       = "operator_release"
	// Quarantines are deliberately not retried automatically. After this
	// duration they remain operationally visible in the administrator-only
	// health projection until a human releases or re-quarantines the request.
	cityRealtimeAgentDecisionDeadLetterStaleAfter = 24 * time.Hour
)

// CityRealtimeAgentDecisionQueueListInput is an administrator-only, bounded
// operational query. WorldID is intentionally mandatory: a control-plane
// operator may inspect one selected world but cannot accidentally turn this
// endpoint into an unbounded cross-world execution-history export.
type CityRealtimeAgentDecisionQueueListInput struct {
	WorldID      int64
	Status       string
	BeforeCursor string
	Limit        int
}

// CityRealtimeAgentDecisionQueueItem is a deliberately redacted admin
// operational projection. It helps diagnose dispatch state without exposing
// Observation content, user personality, provider route/group/account, raw
// provider transcript, prompt, response, worker lease identity, hashes, or
// any currency/billing information.
type CityRealtimeAgentDecisionQueueItem struct {
	WorldID                 int64      `json:"world_id"`
	RequestCode             string     `json:"request_code"`
	AgentDefinitionCode     string     `json:"agent_definition_code"`
	RequestStatus           string     `json:"request_status"`
	OutboxStatus            string     `json:"outbox_status"`
	AttemptCount            int        `json:"attempt_count"`
	RetryNotBefore          *time.Time `json:"retry_not_before,omitempty"`
	ModelProfileCode        *string    `json:"model_profile_code,omitempty"`
	ModelProfileVersion     *int       `json:"model_profile_version,omitempty"`
	LastAttemptStatus       *string    `json:"last_attempt_status,omitempty"`
	LastErrorCode           *string    `json:"last_error_code,omitempty"`
	DeadLetterStatus        *string    `json:"dead_letter_status,omitempty"`
	DeadLetterReasonCode    *string    `json:"dead_letter_reason_code,omitempty"`
	DeadLetterQuarantinedAt *time.Time `json:"dead_letter_quarantined_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type CityRealtimeAgentDecisionQueuePage struct {
	Items      []CityRealtimeAgentDecisionQueueItem `json:"items"`
	NextCursor *string                              `json:"next_cursor,omitempty"`
}

// CityRealtimeAgentDecisionRetryInput permits one explicit administrator wake
// of an already-deferred request. It cannot create a new request, revive a
// terminal request, change a profile, clear a circuit breaker, release budget,
// select an account, or invoke a provider synchronously.
type CityRealtimeAgentDecisionRetryInput struct {
	AdministratorUserID int64
	WorldID             int64
	RequestCode         string
}

type CityRealtimeAgentDecisionRetryResult struct {
	WorldID                int64      `json:"world_id"`
	RequestCode            string     `json:"request_code"`
	RequestStatus          string     `json:"request_status"`
	PreviousRetryNotBefore *time.Time `json:"previous_retry_not_before,omitempty"`
}

// CityRealtimeAgentDecisionDeadLetterInput is a deliberately small
// administrator-only operational intervention. It can quarantine one queued
// request for review, but cannot change its sealed decision snapshot, profile,
// outbox, attempts, budget, breaker, or world state.
type CityRealtimeAgentDecisionDeadLetterInput struct {
	AdministratorUserID int64
	WorldID             int64
	RequestCode         string
	ReasonCode          string
}

// CityRealtimeAgentDecisionDeadLetterReleaseInput releases only an existing
// quarantine. It does not implicitly retry or execute the request; retry is a
// separate, audited operator action when a deferred request needs waking.
type CityRealtimeAgentDecisionDeadLetterReleaseInput struct {
	AdministratorUserID int64
	WorldID             int64
	RequestCode         string
}

// CityRealtimeAgentDecisionDeadLetterResult is a safe operational receipt.
// It intentionally omits observation text, profile routes, account data,
// prompts, provider output, billing and currency fields.
type CityRealtimeAgentDecisionDeadLetterResult struct {
	WorldID          int64  `json:"world_id"`
	RequestCode      string `json:"request_code"`
	DeadLetterStatus string `json:"dead_letter_status"`
	ReasonCode       string `json:"reason_code"`
}

// CityRealtimeAgentDecisionDeadLetterEventListInput is a bounded,
// administrator-only audit read. The event cursor is intentionally local to a
// single request, so it cannot be used to enumerate cross-world operations.
type CityRealtimeAgentDecisionDeadLetterEventListInput struct {
	WorldID       int64
	RequestCode   string
	BeforeEventID int64
	Limit         int
}

// CityRealtimeAgentDecisionDeadLetterEvent is the smallest useful operator
// receipt. It excludes hashes, observation content, profile/route/account
// data, prompt/transcript material, billing and currency information.
type CityRealtimeAgentDecisionDeadLetterEvent struct {
	EventID     int64     `json:"event_id"`
	EventType   string    `json:"event_type"`
	ReasonCode  string    `json:"reason_code"`
	ActorUserID int64     `json:"actor_user_id"`
	CreatedAt   time.Time `json:"created_at"`
}

type CityRealtimeAgentDecisionDeadLetterEventPage struct {
	Items             []CityRealtimeAgentDecisionDeadLetterEvent `json:"items"`
	NextBeforeEventID *int64                                     `json:"next_before_event_id,omitempty"`
}

type cityRealtimeAgentDecisionDeadLetterRecord struct {
	Status              string
	ReasonCode          string
	QuarantinedByUserID int64
	QuarantinedAt       time.Time
	ReleasedByUserID    *int64
	ReleasedAt          *time.Time
	StateHash           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type cityRealtimeAgentDecisionQueueCursor struct {
	CreatedAt   time.Time
	RequestCode string
}

func (cursor cityRealtimeAgentDecisionQueueCursor) String() string {
	return cursor.CreatedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano) + "|" + cursor.RequestCode
}

func parseCityRealtimeAgentDecisionQueueCursor(value string) (cityRealtimeAgentDecisionQueueCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return cityRealtimeAgentDecisionQueueCursor{}, nil
	}
	parts := strings.Split(value, "|")
	if len(parts) != 2 || !cityRealtimeAgentIdentifierValid(parts[1], 96) {
		return cityRealtimeAgentDecisionQueueCursor{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "before_cursor"})
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil || createdAt.IsZero() {
		return cityRealtimeAgentDecisionQueueCursor{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "before_cursor"})
	}
	return cityRealtimeAgentDecisionQueueCursor{
		CreatedAt:   createdAt.UTC().Truncate(time.Microsecond),
		RequestCode: parts[1],
	}, nil
}

func normalizeCityRealtimeAgentDecisionQueueStatus(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return cityRealtimeAgentDecisionQueueStatusActive, nil
	}
	switch value {
	case cityRealtimeAgentDecisionQueueStatusActive,
		cityRealtimeAgentDecisionQueueStatusQueued,
		cityRealtimeAgentDecisionQueueStatusLeased,
		cityRealtimeAgentDecisionQueueStatusTerminal,
		cityRealtimeAgentDecisionQueueStatusAll:
		return value, nil
	default:
		return "", ErrCityInvalidInput.WithMetadata(map[string]string{"field": "status"})
	}
}

func normalizeCityRealtimeAgentDecisionQueueLimit(value int) (int, error) {
	if value == 0 {
		return cityRealtimeAgentDecisionQueueDefaultLimit, nil
	}
	if value < 1 || value > cityRealtimeAgentDecisionQueueMaximumLimit {
		return 0, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "limit"})
	}
	return value, nil
}

func normalizeCityRealtimeAgentDecisionDeadLetterEventLimit(value int) (int, error) {
	if value == 0 {
		return cityRealtimeAgentDecisionDeadLetterEventDefaultLimit, nil
	}
	if value < 1 || value > cityRealtimeAgentDecisionDeadLetterEventMaximumLimit {
		return 0, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "limit"})
	}
	return value, nil
}

func cityRealtimeAgentDecisionQueueRequestStatusValid(value string) bool {
	switch value {
	case cityRealtimeAgentDecisionRequestQueued,
		cityRealtimeAgentDecisionRequestLeased,
		cityRealtimeAgentDecisionRequestAccepted,
		cityRealtimeAgentDecisionRequestRejected,
		cityRealtimeAgentDecisionRequestStale,
		cityRealtimeAgentDecisionRequestFailed,
		cityRealtimeAgentDecisionRequestCanceled:
		return true
	default:
		return false
	}
}

func cityRealtimeAgentDecisionQueueOutboxStatusValid(value string) bool {
	switch value {
	case cityRealtimeAgentOutboxQueued,
		cityRealtimeAgentOutboxLeased,
		cityRealtimeAgentOutboxSucceeded,
		cityRealtimeAgentOutboxFailed,
		cityRealtimeAgentOutboxCancelled:
		return true
	default:
		return false
	}
}

func cityRealtimeAgentDecisionQueueAttemptStatusValid(value string) bool {
	switch value {
	case cityRealtimeAgentDecisionAttemptStarted,
		cityRealtimeAgentDecisionAttemptSucceeded,
		cityRealtimeAgentDecisionAttemptFailed:
		return true
	default:
		return false
	}
}

func cityRealtimeAgentDecisionDeadLetterStatusValid(value string) bool {
	switch value {
	case cityRealtimeAgentDecisionDeadLetterStatusQuarantined,
		cityRealtimeAgentDecisionDeadLetterStatusReleased:
		return true
	default:
		return false
	}
}

func cityRealtimeAgentDecisionDeadLetterReasonValid(value string) bool {
	switch value {
	case cityRealtimeAgentDecisionDeadLetterReasonOperatorReview,
		cityRealtimeAgentDecisionDeadLetterReasonProviderConfiguration,
		cityRealtimeAgentDecisionDeadLetterReasonProviderIncident,
		cityRealtimeAgentDecisionDeadLetterReasonBudgetReview,
		cityRealtimeAgentDecisionDeadLetterReasonWorldMaintenance:
		return true
	default:
		return false
	}
}

func cityRealtimeAgentDecisionDeadLetterEventValid(item CityRealtimeAgentDecisionDeadLetterEvent) bool {
	if item.EventID <= 0 || item.ActorUserID <= 0 || item.CreatedAt.IsZero() {
		return false
	}
	switch item.EventType {
	case cityRealtimeAgentDecisionDeadLetterStatusQuarantined:
		return cityRealtimeAgentDecisionDeadLetterReasonValid(item.ReasonCode)
	case cityRealtimeAgentDecisionDeadLetterStatusReleased:
		return item.ReasonCode == cityRealtimeAgentDecisionDeadLetterReasonOperatorRelease
	default:
		return false
	}
}

func cityRealtimeAgentDecisionQueueStatusClause(status string) string {
	switch status {
	case cityRealtimeAgentDecisionQueueStatusActive:
		return " AND request.status IN ('queued', 'leased')"
	case cityRealtimeAgentDecisionQueueStatusQueued:
		return " AND request.status = 'queued'"
	case cityRealtimeAgentDecisionQueueStatusLeased:
		return " AND request.status = 'leased'"
	case cityRealtimeAgentDecisionQueueStatusTerminal:
		return " AND request.status IN ('accepted', 'rejected', 'stale', 'failed_terminal', 'cancelled')"
	default:
		return ""
	}
}

func cityRealtimeAgentDecisionQueueItemValid(item CityRealtimeAgentDecisionQueueItem) bool {
	if item.WorldID <= 0 || !cityRealtimeAgentIdentifierValid(item.RequestCode, 96) ||
		!cityRealtimeAgentModelDefinitionCodeAllowed(item.AgentDefinitionCode) ||
		!cityRealtimeAgentDecisionQueueRequestStatusValid(item.RequestStatus) ||
		!cityRealtimeAgentDecisionQueueOutboxStatusValid(item.OutboxStatus) ||
		item.AttemptCount < 0 || item.CreatedAt.IsZero() || item.UpdatedAt.IsZero() {
		return false
	}
	if (item.ModelProfileCode == nil) != (item.ModelProfileVersion == nil) {
		return false
	}
	if item.ModelProfileCode != nil && (!cityRealtimeAgentModelProfileCodeValid(*item.ModelProfileCode) || *item.ModelProfileVersion <= 0) {
		return false
	}
	if item.LastAttemptStatus != nil && !cityRealtimeAgentDecisionQueueAttemptStatusValid(*item.LastAttemptStatus) {
		return false
	}
	if item.LastErrorCode != nil && !cityRealtimeAgentIdentifierValid(*item.LastErrorCode, 64) {
		return false
	}
	if (item.DeadLetterStatus == nil) != (item.DeadLetterReasonCode == nil) {
		return false
	}
	if item.DeadLetterStatus != nil &&
		(!cityRealtimeAgentDecisionDeadLetterStatusValid(*item.DeadLetterStatus) ||
			!cityRealtimeAgentDecisionDeadLetterReasonValid(*item.DeadLetterReasonCode)) {
		return false
	}
	if item.DeadLetterStatus == nil && item.DeadLetterQuarantinedAt != nil {
		return false
	}
	if item.DeadLetterStatus != nil && *item.DeadLetterStatus == cityRealtimeAgentDecisionDeadLetterStatusQuarantined {
		if item.DeadLetterQuarantinedAt == nil || item.DeadLetterQuarantinedAt.IsZero() {
			return false
		}
	} else if item.DeadLetterQuarantinedAt != nil {
		return false
	}
	if item.RetryNotBefore != nil && (item.RequestStatus != cityRealtimeAgentDecisionRequestQueued || item.RetryNotBefore.IsZero()) {
		return false
	}
	return true
}

func enableCityRealtimeAgentDecisionDeadLetterOperatorGate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	requestCode string,
	administratorUserID int64,
	action string,
) error {
	if action != "quarantine" && action != "release" {
		return ErrCityInvalidInput
	}
	if err := enableCityRealtimeAgentDecisionOperatorGate(ctx, tx, worldID, requestCode, administratorUserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`,
		"sub2api.city_realtime_agent_operator_dead_letter_action", action); err != nil {
		return fmt.Errorf("activate realtime agent administrator dead letter gate: %w", err)
	}
	return nil
}

func enableCityRealtimeAgentDecisionOperatorGate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	requestCode string,
	administratorUserID int64,
) error {
	if tx == nil || worldID <= 0 || administratorUserID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) {
		return ErrCityInvalidInput
	}
	for _, setting := range []struct {
		name  string
		value string
	}{
		{name: "sub2api.city_realtime_agent_operator_world_id", value: strconv.FormatInt(worldID, 10)},
		{name: "sub2api.city_realtime_agent_operator_request_code", value: requestCode},
		{name: "sub2api.city_realtime_agent_operator_actor_user_id", value: strconv.FormatInt(administratorUserID, 10)},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, setting.value); err != nil {
			return fmt.Errorf("activate realtime agent administrator operator gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func cityRealtimeAgentDecisionOperatorAuditHash(payload map[string]any) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(payload)
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime agent decision administrator retry audit: %w", err)
	}
	return hash, nil
}

func loadCityRealtimeAgentDecisionDeadLetter(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	requestCode string,
	forUpdate bool,
) (cityRealtimeAgentDecisionDeadLetterRecord, bool, error) {
	if queryer == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) {
		return cityRealtimeAgentDecisionDeadLetterRecord{}, false, ErrCityInvalidInput
	}
	query := `
SELECT dead_letter_status, reason_code, quarantined_by_user_id, quarantined_at,
       released_by_user_id, released_at, state_hash, created_at, updated_at
FROM city_realtime_agent_decision_dead_letters
WHERE world_id = $1 AND request_code = $2`
	if forUpdate {
		query += " FOR UPDATE"
	}
	item := cityRealtimeAgentDecisionDeadLetterRecord{}
	var releasedByUserID sql.NullInt64
	var releasedAt sql.NullTime
	err := queryer.QueryRowContext(ctx, query, worldID, requestCode).Scan(
		&item.Status, &item.ReasonCode, &item.QuarantinedByUserID, &item.QuarantinedAt,
		&releasedByUserID, &releasedAt, &item.StateHash, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeAgentDecisionDeadLetterRecord{}, false, nil
	}
	if err != nil {
		return cityRealtimeAgentDecisionDeadLetterRecord{}, false, fmt.Errorf("load realtime agent decision dead letter: %w", err)
	}
	if !cityRealtimeAgentDecisionDeadLetterStatusValid(item.Status) ||
		!cityRealtimeAgentDecisionDeadLetterReasonValid(item.ReasonCode) ||
		item.QuarantinedByUserID <= 0 || item.QuarantinedAt.IsZero() ||
		!cityRealtimeSHA256Hex(item.StateHash) || item.CreatedAt.IsZero() || item.UpdatedAt.IsZero() {
		return cityRealtimeAgentDecisionDeadLetterRecord{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_dead_letter"})
	}
	item.QuarantinedAt = item.QuarantinedAt.UTC().Truncate(time.Microsecond)
	item.CreatedAt = item.CreatedAt.UTC().Truncate(time.Microsecond)
	item.UpdatedAt = item.UpdatedAt.UTC().Truncate(time.Microsecond)
	if releasedByUserID.Valid {
		if releasedByUserID.Int64 <= 0 {
			return cityRealtimeAgentDecisionDeadLetterRecord{}, false,
				ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_dead_letter_release"})
		}
		value := releasedByUserID.Int64
		item.ReleasedByUserID = &value
	}
	if releasedAt.Valid {
		value := releasedAt.Time.UTC().Truncate(time.Microsecond)
		item.ReleasedAt = &value
	}
	if item.Status == cityRealtimeAgentDecisionDeadLetterStatusQuarantined &&
		(item.ReleasedByUserID != nil || item.ReleasedAt != nil) {
		return cityRealtimeAgentDecisionDeadLetterRecord{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_dead_letter_quarantine"})
	}
	if item.Status == cityRealtimeAgentDecisionDeadLetterStatusReleased &&
		(item.ReleasedByUserID == nil || item.ReleasedAt == nil) {
		return cityRealtimeAgentDecisionDeadLetterRecord{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_dead_letter_release"})
	}
	return item, true, nil
}

func cityRealtimeAgentDecisionQuarantined(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	requestCode string,
	forUpdate bool,
) (bool, error) {
	item, found, err := loadCityRealtimeAgentDecisionDeadLetter(ctx, queryer, worldID, requestCode, forUpdate)
	if err != nil || !found {
		return false, err
	}
	return item.Status == cityRealtimeAgentDecisionDeadLetterStatusQuarantined, nil
}

// RetryRealtimeAgentDecisionNow clears only a future retry deadline after an
// administrator repairs the local runtime. The normal worker subsequently
// re-checks the immutable profile, provider registry, budget and breaker; this
// method cannot bypass any of them. A successful wake is append-audited in the
// same transaction and is intentionally invisible to player projections.
func (s *CityEconomyService) RetryRealtimeAgentDecisionNow(
	ctx context.Context,
	input CityRealtimeAgentDecisionRetryInput,
) (*CityRealtimeAgentDecisionRetryResult, error) {
	requestCode := strings.TrimSpace(input.RequestCode)
	if !IsCitySystemAdministrator(ctx) || input.AdministratorUserID <= 0 {
		return nil, ErrCityManagementRequired
	}
	if s == nil || s.db == nil || input.WorldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent administrator retry transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(input.WorldID)); err != nil {
		return nil, fmt.Errorf("lock realtime agent administrator retry world: %w", err)
	}
	var simulationVersion string
	err = tx.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1 FOR SHARE`, input.WorldID).Scan(&simulationVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime agent administrator retry world: %w", err)
	}
	if simulationVersion != CitySimulationVersionRealtimeV2 {
		return nil, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": simulationVersion})
	}
	request, found, err := loadCityRealtimeAgentDecisionRequest(ctx, tx, input.WorldID, requestCode, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeAgentDecisionNotFound
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if request.Status != cityRealtimeAgentDecisionRequestQueued || request.LeaseOwner != nil || request.LeaseExpiresAt != nil ||
		request.RetryNotBefore == nil || !request.RetryNotBefore.After(now) {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_retry_state"})
	}
	var outboxStatus string
	var outboxLeaseOwner sql.NullString
	var outboxLeaseExpiresAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
SELECT status, lease_owner, lease_expires_at
FROM city_realtime_agent_outbox
WHERE world_id = $1 AND request_code = $2
FOR UPDATE`, input.WorldID, requestCode).Scan(&outboxStatus, &outboxLeaseOwner, &outboxLeaseExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_retry_outbox"})
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime agent administrator retry outbox: %w", err)
	}
	if outboxStatus != cityRealtimeAgentOutboxQueued || outboxLeaseOwner.Valid || outboxLeaseExpiresAt.Valid {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "outbox_retry_state"})
	}
	previousRetryNotBefore := cityRealtimeAgentDecisionRetryNotBeforeCopy(request.RetryNotBefore)
	if err = enableCityRealtimeAgentDecisionOperatorGate(ctx, tx, input.WorldID, requestCode, input.AdministratorUserID); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_requests
SET retry_not_before = NULL, updated_at = NOW()
WHERE world_id = $1 AND request_code = $2
  AND status = 'queued' AND lease_owner IS NULL AND lease_expires_at IS NULL
  AND retry_not_before = $3`, input.WorldID, requestCode, *previousRetryNotBefore)
	if err != nil {
		return nil, fmt.Errorf("wake realtime agent decision retry: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return nil, fmt.Errorf("check realtime agent administrator retry wake: %w", rowsErr)
	} else if rows != 1 {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_retry_wake"})
	}
	payloadHash, err := cityRealtimeAgentDecisionOperatorAuditHash(map[string]any{
		"schema_version":            1,
		"event_type":                "retry_requested",
		"world_id":                  input.WorldID,
		"request_code":              requestCode,
		"actor_user_id":             input.AdministratorUserID,
		"previous_retry_not_before": previousRetryNotBefore.Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_decision_operator_events
    (world_id, request_code, event_type, actor_user_id, previous_retry_not_before, payload_hash, metadata)
VALUES ($1, $2, 'retry_requested', $3, $4, $5, '{}'::jsonb)`,
		input.WorldID, requestCode, input.AdministratorUserID, *previousRetryNotBefore, payloadHash,
	); err != nil {
		return nil, fmt.Errorf("append realtime agent administrator retry audit: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent administrator retry wake: %w", err)
	}
	return &CityRealtimeAgentDecisionRetryResult{
		WorldID:                input.WorldID,
		RequestCode:            requestCode,
		RequestStatus:          cityRealtimeAgentDecisionRequestQueued,
		PreviousRetryNotBefore: previousRetryNotBefore,
	}, nil
}

// QuarantineRealtimeAgentDecision prevents a queued, unleased request from
// being selected by a worker while an administrator investigates it. It is an
// operational dead letter, not a terminal decision: the sealed request and
// outbox remain untouched so an explicit release can resume ordinary worker
// processing without reconstructing any simulation state.
func (s *CityEconomyService) QuarantineRealtimeAgentDecision(
	ctx context.Context,
	input CityRealtimeAgentDecisionDeadLetterInput,
) (*CityRealtimeAgentDecisionDeadLetterResult, error) {
	requestCode := strings.TrimSpace(input.RequestCode)
	reasonCode := strings.ToLower(strings.TrimSpace(input.ReasonCode))
	if !IsCitySystemAdministrator(ctx) || input.AdministratorUserID <= 0 {
		return nil, ErrCityManagementRequired
	}
	if s == nil || s.db == nil || input.WorldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) ||
		!cityRealtimeAgentDecisionDeadLetterReasonValid(reasonCode) {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent decision quarantine transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(input.WorldID)); err != nil {
		return nil, fmt.Errorf("lock realtime agent decision quarantine world: %w", err)
	}
	if err = cityRealtimeAgentDecisionRequireOperationalQueuedRequest(ctx, tx, input.WorldID, requestCode); err != nil {
		return nil, err
	}
	existing, found, err := loadCityRealtimeAgentDecisionDeadLetter(ctx, tx, input.WorldID, requestCode, true)
	if err != nil {
		return nil, err
	}
	if found && existing.Status == cityRealtimeAgentDecisionDeadLetterStatusQuarantined {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "dead_letter_status"})
	}
	if err = enableCityRealtimeAgentDecisionDeadLetterOperatorGate(
		ctx, tx, input.WorldID, requestCode, input.AdministratorUserID, "quarantine",
	); err != nil {
		return nil, err
	}
	stateHash, err := cityRealtimeAgentDecisionOperatorAuditHash(map[string]any{
		"schema_version":     1,
		"event_type":         "quarantined",
		"world_id":           input.WorldID,
		"request_code":       requestCode,
		"dead_letter_status": cityRealtimeAgentDecisionDeadLetterStatusQuarantined,
		"reason_code":        reasonCode,
		"actor_user_id":      input.AdministratorUserID,
	})
	if err != nil {
		return nil, err
	}
	if found {
		result, updateErr := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_dead_letters
SET dead_letter_status = 'quarantined', reason_code = $3,
    quarantined_by_user_id = $4, quarantined_at = NOW(),
    released_by_user_id = NULL, released_at = NULL,
    state_hash = $5, updated_at = NOW()
WHERE world_id = $1 AND request_code = $2 AND dead_letter_status = 'released'`,
			input.WorldID, requestCode, reasonCode, input.AdministratorUserID, stateHash,
		)
		if updateErr != nil {
			return nil, fmt.Errorf("re-quarantine realtime agent decision: %w", updateErr)
		}
		if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
			return nil, fmt.Errorf("check realtime agent decision re-quarantine: %w", rowsErr)
		} else if rows != 1 {
			return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "dead_letter_requarantine"})
		}
	} else if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_decision_dead_letters
    (world_id, request_code, dead_letter_status, reason_code, quarantined_by_user_id, state_hash, metadata)
VALUES ($1, $2, 'quarantined', $3, $4, $5, '{}'::jsonb)`,
		input.WorldID, requestCode, reasonCode, input.AdministratorUserID, stateHash,
	); err != nil {
		return nil, fmt.Errorf("quarantine realtime agent decision: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_decision_dead_letter_events
    (world_id, request_code, event_type, reason_code, actor_user_id, payload_hash, metadata)
VALUES ($1, $2, 'quarantined', $3, $4, $5, '{}'::jsonb)`,
		input.WorldID, requestCode, reasonCode, input.AdministratorUserID, stateHash,
	); err != nil {
		return nil, fmt.Errorf("append realtime agent decision quarantine audit: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent decision quarantine: %w", err)
	}
	return &CityRealtimeAgentDecisionDeadLetterResult{
		WorldID:          input.WorldID,
		RequestCode:      requestCode,
		DeadLetterStatus: cityRealtimeAgentDecisionDeadLetterStatusQuarantined,
		ReasonCode:       reasonCode,
	}, nil
}

// ReleaseRealtimeAgentDecisionDeadLetter lifts an active operational
// quarantine without executing or waking the request. Keeping release and
// retry distinct makes each privileged intervention independently auditable
// and prevents an operator from accidentally resuming a deferred workload.
func (s *CityEconomyService) ReleaseRealtimeAgentDecisionDeadLetter(
	ctx context.Context,
	input CityRealtimeAgentDecisionDeadLetterReleaseInput,
) (*CityRealtimeAgentDecisionDeadLetterResult, error) {
	requestCode := strings.TrimSpace(input.RequestCode)
	if !IsCitySystemAdministrator(ctx) || input.AdministratorUserID <= 0 {
		return nil, ErrCityManagementRequired
	}
	if s == nil || s.db == nil || input.WorldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent decision dead letter release transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(input.WorldID)); err != nil {
		return nil, fmt.Errorf("lock realtime agent decision dead letter release world: %w", err)
	}
	if err = cityRealtimeAgentDecisionRequireOperationalQueuedRequest(ctx, tx, input.WorldID, requestCode); err != nil {
		return nil, err
	}
	existing, found, err := loadCityRealtimeAgentDecisionDeadLetter(ctx, tx, input.WorldID, requestCode, true)
	if err != nil {
		return nil, err
	}
	if !found || existing.Status != cityRealtimeAgentDecisionDeadLetterStatusQuarantined {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "dead_letter_status"})
	}
	if err = enableCityRealtimeAgentDecisionDeadLetterOperatorGate(
		ctx, tx, input.WorldID, requestCode, input.AdministratorUserID, "release",
	); err != nil {
		return nil, err
	}
	stateHash, err := cityRealtimeAgentDecisionOperatorAuditHash(map[string]any{
		"schema_version":     1,
		"event_type":         "released",
		"world_id":           input.WorldID,
		"request_code":       requestCode,
		"dead_letter_status": cityRealtimeAgentDecisionDeadLetterStatusReleased,
		"reason_code":        existing.ReasonCode,
		"actor_user_id":      input.AdministratorUserID,
	})
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_dead_letters
SET dead_letter_status = 'released', released_by_user_id = $3, released_at = NOW(),
    state_hash = $4, updated_at = NOW()
WHERE world_id = $1 AND request_code = $2 AND dead_letter_status = 'quarantined'`,
		input.WorldID, requestCode, input.AdministratorUserID, stateHash,
	)
	if err != nil {
		return nil, fmt.Errorf("release realtime agent decision dead letter: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return nil, fmt.Errorf("check realtime agent decision dead letter release: %w", rowsErr)
	} else if rows != 1 {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "dead_letter_release"})
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_decision_dead_letter_events
    (world_id, request_code, event_type, reason_code, actor_user_id, payload_hash, metadata)
VALUES ($1, $2, 'released', $3, $4, $5, '{}'::jsonb)`,
		input.WorldID, requestCode, cityRealtimeAgentDecisionDeadLetterReasonOperatorRelease,
		input.AdministratorUserID, stateHash,
	); err != nil {
		return nil, fmt.Errorf("append realtime agent decision dead letter release audit: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent decision dead letter release: %w", err)
	}
	return &CityRealtimeAgentDecisionDeadLetterResult{
		WorldID:          input.WorldID,
		RequestCode:      requestCode,
		DeadLetterStatus: cityRealtimeAgentDecisionDeadLetterStatusReleased,
		ReasonCode:       existing.ReasonCode,
	}, nil
}

// ListRealtimeAgentDecisionDeadLetterEvents returns a keyset-paginated,
// administrator-only audit trail for one request. It never returns a payload
// hash or any provider/observation data, and has no side effect on the
// quarantine, retry, lease, worker or canonical world state.
func (s *CityEconomyService) ListRealtimeAgentDecisionDeadLetterEvents(
	ctx context.Context,
	input CityRealtimeAgentDecisionDeadLetterEventListInput,
) (*CityRealtimeAgentDecisionDeadLetterEventPage, error) {
	requestCode := strings.TrimSpace(input.RequestCode)
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if s == nil || s.db == nil || input.WorldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) || input.BeforeEventID < 0 {
		return nil, ErrCityInvalidInput
	}
	limit, err := normalizeCityRealtimeAgentDecisionDeadLetterEventLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent decision dead letter event projection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var simulationVersion string
	err = tx.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, input.WorldID).Scan(&simulationVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime agent decision dead letter event world: %w", err)
	}
	if simulationVersion != CitySimulationVersionRealtimeV2 {
		return nil, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": simulationVersion})
	}
	var requestExists bool
	err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM city_realtime_agent_decision_requests
    WHERE world_id = $1 AND request_code = $2
)`, input.WorldID, requestCode).Scan(&requestExists)
	if err != nil {
		return nil, fmt.Errorf("check realtime agent decision dead letter event request: %w", err)
	}
	if !requestExists {
		return nil, ErrCityRealtimeAgentDecisionNotFound
	}
	query := `
SELECT event_id, event_type, reason_code, actor_user_id, created_at
FROM city_realtime_agent_decision_dead_letter_events
WHERE world_id = $1 AND request_code = $2`
	args := []any{input.WorldID, requestCode}
	if input.BeforeEventID > 0 {
		args = append(args, input.BeforeEventID)
		query += " AND event_id < $" + strconv.Itoa(len(args))
	}
	args = append(args, limit+1)
	query += "\nORDER BY event_id DESC\nLIMIT $" + strconv.Itoa(len(args))
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list realtime agent decision dead letter events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	page := &CityRealtimeAgentDecisionDeadLetterEventPage{
		Items: make([]CityRealtimeAgentDecisionDeadLetterEvent, 0, limit),
	}
	for rows.Next() {
		item := CityRealtimeAgentDecisionDeadLetterEvent{}
		if err = rows.Scan(&item.EventID, &item.EventType, &item.ReasonCode, &item.ActorUserID, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan realtime agent decision dead letter event: %w", err)
		}
		item.CreatedAt = item.CreatedAt.UTC().Truncate(time.Microsecond)
		if !cityRealtimeAgentDecisionDeadLetterEventValid(item) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_dead_letter_event"})
		}
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime agent decision dead letter events: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime agent decision dead letter events: %w", err)
	}
	if len(page.Items) > limit {
		value := page.Items[limit-1].EventID
		page.NextBeforeEventID = &value
		page.Items = page.Items[:limit]
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent decision dead letter event projection: %w", err)
	}
	return page, nil
}

// cityRealtimeAgentDecisionRequireOperationalQueuedRequest protects both
// dead-letter transitions from racing a worker lease. It does not change
// request/outbox state; it only proves that the operator is reviewing a row
// that is still safe to pause or release.
func cityRealtimeAgentDecisionRequireOperationalQueuedRequest(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	requestCode string,
) error {
	var simulationVersion string
	err := tx.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1 FOR SHARE`, worldID).Scan(&simulationVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCityWorldNotFound
	}
	if err != nil {
		return fmt.Errorf("load realtime agent decision dead letter world: %w", err)
	}
	if simulationVersion != CitySimulationVersionRealtimeV2 {
		return ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": simulationVersion})
	}
	request, found, err := loadCityRealtimeAgentDecisionRequest(ctx, tx, worldID, requestCode, true)
	if err != nil {
		return err
	}
	if !found {
		return ErrCityRealtimeAgentDecisionNotFound
	}
	if request.Status != cityRealtimeAgentDecisionRequestQueued || request.LeaseOwner != nil || request.LeaseExpiresAt != nil {
		return ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_dead_letter_state"})
	}
	var outboxStatus string
	var outboxLeaseOwner sql.NullString
	var outboxLeaseExpiresAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
SELECT status, lease_owner, lease_expires_at
FROM city_realtime_agent_outbox
WHERE world_id = $1 AND request_code = $2
FOR UPDATE`, worldID, requestCode).Scan(&outboxStatus, &outboxLeaseOwner, &outboxLeaseExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_dead_letter_outbox"})
	}
	if err != nil {
		return fmt.Errorf("load realtime agent decision dead letter outbox: %w", err)
	}
	if outboxStatus != cityRealtimeAgentOutboxQueued || outboxLeaseOwner.Valid || outboxLeaseExpiresAt.Valid {
		return ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "outbox_dead_letter_state"})
	}
	return nil
}

// ListRealtimeAgentDecisionQueue returns a stable, keyset-paginated snapshot
// of a single V2 world's dispatch queue for administrators. It is read-only:
// retry deadlines, leases, attempts and breakers are never changed by this
// endpoint. A future replay operation must be a separate audited mutation,
// not an implicit side effect of inspection.
func (s *CityEconomyService) ListRealtimeAgentDecisionQueue(
	ctx context.Context,
	input CityRealtimeAgentDecisionQueueListInput,
) (*CityRealtimeAgentDecisionQueuePage, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if s == nil || s.db == nil || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	status, err := normalizeCityRealtimeAgentDecisionQueueStatus(input.Status)
	if err != nil {
		return nil, err
	}
	limit, err := normalizeCityRealtimeAgentDecisionQueueLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	cursor, err := parseCityRealtimeAgentDecisionQueueCursor(input.BeforeCursor)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent decision queue projection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var simulationVersion string
	err = tx.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, input.WorldID).Scan(&simulationVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime agent decision queue world: %w", err)
	}
	if simulationVersion != CitySimulationVersionRealtimeV2 {
		return nil, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": simulationVersion})
	}

	query := `
SELECT request.request_code, instance.definition_code,
       request.status, outbox.status, request.attempt_count, request.retry_not_before,
       request.model_profile_code, request.model_profile_version,
       latest_attempt.status, latest_attempt.error_code,
       dead_letter.dead_letter_status, dead_letter.reason_code,
       CASE WHEN dead_letter.dead_letter_status = 'quarantined' THEN dead_letter.quarantined_at ELSE NULL END,
       request.created_at, request.updated_at
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_instances instance
  ON instance.world_id = request.world_id AND instance.agent_code = request.agent_code
JOIN city_realtime_agent_outbox outbox
  ON outbox.world_id = request.world_id AND outbox.request_code = request.request_code
LEFT JOIN city_realtime_agent_decision_dead_letters dead_letter
  ON dead_letter.world_id = request.world_id AND dead_letter.request_code = request.request_code
LEFT JOIN LATERAL (
    SELECT attempt.status, attempt.error_code
    FROM city_realtime_agent_decision_attempts attempt
    WHERE attempt.world_id = request.world_id AND attempt.request_code = request.request_code
    ORDER BY attempt.attempt_number DESC, attempt.attempt_code DESC
    LIMIT 1
) latest_attempt ON TRUE
WHERE request.world_id = $1` + cityRealtimeAgentDecisionQueueStatusClause(status)
	args := []any{input.WorldID}
	if !cursor.CreatedAt.IsZero() {
		args = append(args, cursor.CreatedAt, cursor.RequestCode)
		query += "\n  AND (request.created_at, request.request_code) < ($" + strconv.Itoa(len(args)-1) + ", $" + strconv.Itoa(len(args)) + ")"
	}
	args = append(args, limit+1)
	query += "\nORDER BY request.created_at DESC, request.request_code DESC\nLIMIT $" + strconv.Itoa(len(args))

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list realtime agent decision queue: %w", err)
	}
	defer func() { _ = rows.Close() }()
	page := &CityRealtimeAgentDecisionQueuePage{Items: make([]CityRealtimeAgentDecisionQueueItem, 0, limit)}
	for rows.Next() {
		item := CityRealtimeAgentDecisionQueueItem{WorldID: input.WorldID}
		var retryNotBefore sql.NullTime
		var profileCode, latestAttemptStatus, latestErrorCode sql.NullString
		var deadLetterStatus, deadLetterReasonCode sql.NullString
		var deadLetterQuarantinedAt sql.NullTime
		var profileVersion sql.NullInt64
		if err = rows.Scan(
			&item.RequestCode, &item.AgentDefinitionCode,
			&item.RequestStatus, &item.OutboxStatus, &item.AttemptCount, &retryNotBefore,
			&profileCode, &profileVersion,
			&latestAttemptStatus, &latestErrorCode,
			&deadLetterStatus, &deadLetterReasonCode, &deadLetterQuarantinedAt,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan realtime agent decision queue: %w", err)
		}
		item.RetryNotBefore = cityRealtimeNullTimePointer(retryNotBefore)
		item.ModelProfileCode = cityRealtimeAgentNullStringPointer(profileCode)
		item.ModelProfileVersion = cityRealtimeAgentNullIntPointer(profileVersion)
		item.LastAttemptStatus = cityRealtimeAgentNullStringPointer(latestAttemptStatus)
		item.LastErrorCode = cityRealtimeAgentNullStringPointer(latestErrorCode)
		item.DeadLetterStatus = cityRealtimeAgentNullStringPointer(deadLetterStatus)
		item.DeadLetterReasonCode = cityRealtimeAgentNullStringPointer(deadLetterReasonCode)
		item.DeadLetterQuarantinedAt = cityRealtimeNullTimePointer(deadLetterQuarantinedAt)
		item.CreatedAt = item.CreatedAt.UTC().Truncate(time.Microsecond)
		item.UpdatedAt = item.UpdatedAt.UTC().Truncate(time.Microsecond)
		if !cityRealtimeAgentDecisionQueueItemValid(item) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_queue_item"})
		}
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime agent decision queue: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime agent decision queue: %w", err)
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		next := (cityRealtimeAgentDecisionQueueCursor{CreatedAt: last.CreatedAt, RequestCode: last.RequestCode}).String()
		page.NextCursor = &next
		page.Items = page.Items[:limit]
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent decision queue projection: %w", err)
	}
	return page, nil
}
