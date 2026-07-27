package securityaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (r *PostgreSQLRepository) ListExceptions(ctx context.Context, includeInactive bool) ([]*AuditException, error) {
	where := `
WHERE status='active' AND starts_at<=NOW() AND (permanent OR expires_at>NOW())`
	if includeInactive {
		where = ""
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT `+exceptionColumns("e")+`
FROM security_audit_exceptions e `+where+`
ORDER BY e.status,e.created_at DESC,e.id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*AuditException, 0)
	for rows.Next() {
		item, err := scanException(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) CreateException(ctx context.Context, request CreateExceptionRequest, actorID int64) (*AuditException, error) {
	now := time.Now()
	if err := validateException(request, now); err != nil {
		return nil, err
	}
	startsAt := now
	if request.StartsAt != nil {
		startsAt = *request.StartsAt
	}
	item, err := scanException(r.db.QueryRowContext(ctx, `
INSERT INTO security_audit_exceptions(
    exception_id,name,scope_type,scope_id,detector_id,category,effect,reason,
    status,starts_at,expires_at,permanent,created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'active',$9,$10,$11,$12)
RETURNING `+exceptionColumns("security_audit_exceptions"),
		newSecurityID("exc"), strings.TrimSpace(request.Name), strings.TrimSpace(request.ScopeType),
		TrimRunes(strings.TrimSpace(request.ScopeID), 256), TrimRunes(strings.TrimSpace(request.DetectorID), 96),
		TrimRunes(strings.TrimSpace(request.Category), 96), strings.TrimSpace(request.Effect),
		TrimRunes(strings.TrimSpace(request.Reason), 512), startsAt, request.ExpiresAt,
		request.Permanent, nullableID(actorID)))
	return item, err
}

func (r *PostgreSQLRepository) ExpireException(ctx context.Context, id, actorID int64, reason string) (*AuditException, error) {
	item, err := scanException(r.db.QueryRowContext(ctx, `
UPDATE security_audit_exceptions
SET status='revoked',expired_at=NOW(),revoked_by=$2,revoked_reason=$3
WHERE id=$1 AND status='active'
RETURNING `+exceptionColumns("security_audit_exceptions"),
		id, nullableID(actorID), TrimRunes(strings.TrimSpace(reason), 512)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrExceptionNotFound
	}
	return item, err
}

func exceptionColumns(alias string) string {
	return alias + `.id,` + alias + `.exception_id,` + alias + `.name,` + alias + `.scope_type,` +
		alias + `.scope_id,` + alias + `.detector_id,` + alias + `.category,` + alias + `.effect,` +
		alias + `.reason,` + alias + `.status,` + alias + `.starts_at,` + alias + `.expires_at,` +
		alias + `.permanent,` + alias + `.created_by,` + alias + `.created_at,` + alias + `.expired_at,` +
		alias + `.revoked_by,` + alias + `.revoked_reason`
}

func scanException(row rowScanner) (*AuditException, error) {
	item := &AuditException{}
	var expiresAt, expiredAt sql.NullTime
	var createdBy, revokedBy sql.NullInt64
	if err := row.Scan(
		&item.ID, &item.ExceptionID, &item.Name, &item.ScopeType, &item.ScopeID,
		&item.DetectorID, &item.Category, &item.Effect, &item.Reason, &item.Status,
		&item.StartsAt, &expiresAt, &item.Permanent, &createdBy, &item.CreatedAt, &expiredAt,
		&revokedBy, &item.RevokedReason,
	); err != nil {
		return nil, err
	}
	item.ExpiresAt = nullableTime(expiresAt)
	item.ExpiredAt = nullableTime(expiredAt)
	if createdBy.Valid {
		item.CreatedBy = &createdBy.Int64
	}
	if revokedBy.Valid {
		item.RevokedBy = &revokedBy.Int64
	}
	return item, nil
}

func (r *PostgreSQLRepository) CreateFeedback(ctx context.Context, decisionPK, actorID int64, request FeedbackRequest) (map[string]any, error) {
	conclusion := strings.TrimSpace(request.Conclusion)
	switch conclusion {
	case "confirmed", "false_positive", "false_negative", "needs_more_info":
	default:
		return nil, errors.New("反馈结论无效")
	}
	var policyKey string
	var policyVersion int64
	var detectorRaw []byte
	if err := r.db.QueryRowContext(ctx, `
SELECT policy_key,policy_version,detector_summary
FROM security_audit_decisions WHERE id=$1`, decisionPK).Scan(
		&policyKey, &policyVersion, &detectorRaw,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDecisionNotFound
	} else if err != nil {
		return nil, err
	}
	var casePK any
	if caseID := strings.TrimSpace(request.CaseID); caseID != "" {
		var id int64
		if err := r.db.QueryRowContext(ctx, `SELECT id FROM security_audit_cases WHERE case_id=$1`, caseID).Scan(&id); err != nil {
			return nil, ErrCaseNotFound
		}
		casePK = id
	}
	feedbackID := newSecurityID("fb")
	var id int64
	var createdAt time.Time
	err := r.db.QueryRowContext(ctx, `
INSERT INTO security_audit_feedback(
    feedback_id,decision_pk,case_pk,conclusion,corrected_category,note,
    policy_key,policy_version,detector_snapshot,created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)
RETURNING id,created_at`,
		feedbackID, decisionPK, casePK, conclusion,
		TrimRunes(strings.TrimSpace(request.CorrectedCategory), 96),
		TrimRunes(strings.TrimSpace(request.Note), 4000),
		policyKey, policyVersion, detectorRaw, nullableID(actorID),
	).Scan(&id, &createdAt)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "feedback_id": feedbackID, "decision_pk": decisionPK,
		"conclusion": conclusion, "created_at": createdAt,
	}, nil
}

func (r *PostgreSQLRepository) UpsertEndpointHealth(ctx context.Context, endpoint ActiveEndpoint, result ProbeResult) error {
	status := "unhealthy"
	if result.OK {
		status = "healthy"
	} else if result.Retryable {
		status = "degraded"
	}
	networkScope := string(NormalizeNetworkScope(endpoint.NetworkScope, endpoint.BaseURL))
	timeoutIncrement := 0
	if result.ErrorCode == "prompt_guard_timeout" {
		timeoutIncrement = 1
	}
	rateLimitedIncrement := 0
	if result.HTTPStatus == 429 {
		rateLimitedIncrement = 1
	}
	serverErrorIncrement := 0
	if result.HTTPStatus >= 500 {
		serverErrorIncrement = 1
	}
	invalidIncrement := 0
	if result.ErrorCode == ErrorCodeInvalidResponse {
		invalidIncrement = 1
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO security_audit_endpoint_health(
    endpoint_id,network_scope,status,breaker_state,consecutive_failures,
    latency_ms,http_status,error_code,checked_at,request_count,success_count,
    timeout_count,rate_limited_count,server_error_count,invalid_response_count,
    latency_sum_ms,latency_max_ms
) VALUES (
    $1,$2,$3,'closed',
    CASE WHEN $4::boolean THEN 0 ELSE 1 END,$5::integer,$6::integer,$7,$8,1,
    CASE WHEN $4::boolean THEN 1 ELSE 0 END,$9,$10,$11,$12,$5::bigint,$5::integer
)
ON CONFLICT (endpoint_id) DO UPDATE SET
    network_scope=EXCLUDED.network_scope,
    status=EXCLUDED.status,
    consecutive_failures=CASE
        WHEN EXCLUDED.status='healthy' THEN 0
        ELSE security_audit_endpoint_health.consecutive_failures+1
    END,
    breaker_state=CASE
        WHEN EXCLUDED.status='healthy' THEN 'closed'
        WHEN security_audit_endpoint_health.breaker_state='half_open'
          OR security_audit_endpoint_health.consecutive_failures+1 >= 5 THEN 'open'
        ELSE security_audit_endpoint_health.breaker_state
    END,
    breaker_opened_at=CASE
        WHEN EXCLUDED.status='healthy' THEN NULL
        WHEN security_audit_endpoint_health.breaker_state='half_open'
          OR security_audit_endpoint_health.consecutive_failures+1 >= 5 THEN NOW()
        ELSE security_audit_endpoint_health.breaker_opened_at
    END,
    request_count=security_audit_endpoint_health.request_count+1,
    success_count=security_audit_endpoint_health.success_count+EXCLUDED.success_count,
    timeout_count=security_audit_endpoint_health.timeout_count+EXCLUDED.timeout_count,
    rate_limited_count=security_audit_endpoint_health.rate_limited_count+EXCLUDED.rate_limited_count,
    server_error_count=security_audit_endpoint_health.server_error_count+EXCLUDED.server_error_count,
    invalid_response_count=security_audit_endpoint_health.invalid_response_count+EXCLUDED.invalid_response_count,
    latency_sum_ms=security_audit_endpoint_health.latency_sum_ms+EXCLUDED.latency_sum_ms,
    latency_max_ms=GREATEST(security_audit_endpoint_health.latency_max_ms,EXCLUDED.latency_max_ms),
    latency_ms=((
        security_audit_endpoint_health.latency_sum_ms+EXCLUDED.latency_sum_ms
    )/(security_audit_endpoint_health.request_count+1))::integer,
    http_status=EXCLUDED.http_status,
    error_code=EXCLUDED.error_code,
    checked_at=EXCLUDED.checked_at,
    updated_at=NOW()`,
		endpoint.ID, networkScope, status, result.OK, result.LatencyMS,
		result.HTTPStatus, TrimRunes(result.ErrorCode, 96), result.CheckedAt,
		timeoutIncrement, rateLimitedIncrement, serverErrorIncrement, invalidIncrement)
	return err
}

// BeginEndpointAttempt atomically admits one half-open probe after cooldown. Closed
// endpoints remain unconstrained; open and already half-open endpoints are skipped.
func (r *PostgreSQLRepository) BeginEndpointAttempt(
	ctx context.Context,
	endpoint ActiveEndpoint,
	cooldown time.Duration,
) (bool, error) {
	if cooldown <= 0 {
		cooldown = time.Minute
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	networkScope := string(NormalizeNetworkScope(endpoint.NetworkScope, endpoint.BaseURL))
	if _, err = tx.ExecContext(ctx, `
INSERT INTO security_audit_endpoint_health(endpoint_id,network_scope,status,breaker_state)
VALUES ($1,$2,'unknown','closed')
ON CONFLICT (endpoint_id) DO NOTHING`, endpoint.ID, networkScope); err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE security_audit_endpoint_health
SET breaker_state='half_open',updated_at=NOW()
WHERE endpoint_id=$1
  AND breaker_state='open'
  AND breaker_opened_at <= NOW()-($2 * INTERVAL '1 millisecond')`,
		endpoint.ID, cooldown.Milliseconds())
	if err != nil {
		return false, err
	}
	transitioned, _ := result.RowsAffected()
	var state string
	if err = tx.QueryRowContext(ctx, `
SELECT breaker_state
FROM security_audit_endpoint_health
WHERE endpoint_id=$1
FOR UPDATE`, endpoint.ID).Scan(&state); err != nil {
		return false, err
	}
	allowed := state == "closed" || transitioned == 1
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return allowed, nil
}

func (r *PostgreSQLRepository) ListEndpointHealth(ctx context.Context) ([]EndpointHealth, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT endpoint_id,network_scope,status,breaker_state,consecutive_failures,
       request_count,success_count,timeout_count,rate_limited_count,
       server_error_count,invalid_response_count,latency_sum_ms,latency_max_ms,
       latency_ms,http_status,error_code,checked_at,breaker_opened_at,updated_at
FROM security_audit_endpoint_health
ORDER BY endpoint_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]EndpointHealth, 0)
	for rows.Next() {
		var item EndpointHealth
		var checkedAt, breakerOpenedAt sql.NullTime
		if err := rows.Scan(
			&item.EndpointID, &item.NetworkScope, &item.Status, &item.BreakerState,
			&item.ConsecutiveFailures, &item.RequestCount, &item.SuccessCount,
			&item.TimeoutCount, &item.RateLimitedCount, &item.ServerErrorCount,
			&item.InvalidResponseCount, &item.LatencySumMS, &item.LatencyMaxMS,
			&item.LatencyMS, &item.HTTPStatus,
			&item.ErrorCode, &checkedAt, &breakerOpenedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.CheckedAt = nullableTime(checkedAt)
		item.BreakerOpenedAt = nullableTime(breakerOpenedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) ResetEndpointBreaker(ctx context.Context, endpointID string) (*EndpointHealth, error) {
	var item EndpointHealth
	var checkedAt, breakerOpenedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
UPDATE security_audit_endpoint_health
SET breaker_state='closed',consecutive_failures=0,breaker_opened_at=NULL,updated_at=NOW()
WHERE endpoint_id=$1
RETURNING endpoint_id,network_scope,status,breaker_state,consecutive_failures,
          request_count,success_count,timeout_count,rate_limited_count,
          server_error_count,invalid_response_count,latency_sum_ms,latency_max_ms,
          latency_ms,http_status,error_code,checked_at,breaker_opened_at,updated_at`,
		strings.TrimSpace(endpointID)).Scan(
		&item.EndpointID, &item.NetworkScope, &item.Status, &item.BreakerState,
		&item.ConsecutiveFailures, &item.RequestCount, &item.SuccessCount,
		&item.TimeoutCount, &item.RateLimitedCount, &item.ServerErrorCount,
		&item.InvalidResponseCount, &item.LatencySumMS, &item.LatencyMaxMS,
		&item.LatencyMS, &item.HTTPStatus,
		&item.ErrorCode, &checkedAt, &breakerOpenedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("审核节点不存在")
	}
	if err != nil {
		return nil, err
	}
	item.CheckedAt = nullableTime(checkedAt)
	item.BreakerOpenedAt = nullableTime(breakerOpenedAt)
	return &item, nil
}

func (r *PostgreSQLRepository) ExpireSecurityAuditRecords(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `
UPDATE security_audit_exceptions
SET status='expired',expired_at=NOW()
WHERE status='active' AND permanent=FALSE AND expires_at<=NOW()`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE security_audit_cases
SET status='expired',resolution='expired',resolved_at=NOW(),updated_at=NOW()
WHERE status IN ('open','reviewing') AND expires_at IS NOT NULL AND expires_at<=NOW()`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE security_audit_evidence
SET evidence_ciphertext='',encryption_key_id=''
WHERE evidence_ciphertext<>'' AND expires_at IS NOT NULL AND expires_at<=NOW()
  AND (hold_until IS NULL OR hold_until<=NOW())`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
DELETE FROM security_audit_signal_windows
WHERE bucket_start<NOW()-INTERVAL '30 days'`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
DELETE FROM security_audit_notifications
WHERE status IN ('read','dismissed') AND created_at<NOW()-INTERVAL '90 days'`); err != nil {
		return err
	}
	return tx.Commit()
}

func detectorSnapshotFromRaw(raw []byte) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage("[]")
	}
	return append(json.RawMessage(nil), raw...)
}
