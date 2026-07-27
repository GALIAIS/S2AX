package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type contentModerationRepository struct {
	db *sql.DB
}

type contentModerationQueryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func NewContentModerationRepository(db *sql.DB) service.ContentModerationRepository {
	return &contentModerationRepository{db: db}
}

func (r *contentModerationRepository) CreateLog(ctx context.Context, log *service.ContentModerationLog) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin content moderation log transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := createContentModerationLog(ctx, tx, log); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit content moderation log transaction: %w", err)
	}
	return nil
}

func createContentModerationLog(ctx context.Context, queryer contentModerationQueryRower, log *service.ContentModerationLog) error {
	return createContentModerationLogWithActions(ctx, queryer, log, nil)
}

func createContentModerationLogWithActions(
	ctx context.Context,
	queryer contentModerationQueryRower,
	log *service.ContentModerationLog,
	additionalActions []string,
) error {
	if log == nil {
		return nil
	}
	categoryScores, err := json.Marshal(log.CategoryScores)
	if err != nil {
		return fmt.Errorf("marshal moderation category scores: %w", err)
	}
	thresholdSnapshot, err := json.Marshal(log.ThresholdSnapshot)
	if err != nil {
		return fmt.Errorf("marshal moderation thresholds: %w", err)
	}
	var userID any
	if log.UserID != nil {
		userID = *log.UserID
	}
	var apiKeyID any
	if log.APIKeyID != nil {
		apiKeyID = *log.APIKeyID
	}
	var groupID any
	if log.GroupID != nil {
		groupID = *log.GroupID
	}
	var latency any
	if log.UpstreamLatencyMS != nil {
		latency = *log.UpstreamLatencyMS
	}
	err = queryer.QueryRowContext(ctx, `
INSERT INTO content_moderation_logs (
    request_id, user_id, user_email, api_key_id, api_key_name, group_id, group_name,
    endpoint, provider, model, audit_endpoint_type, audit_model, mode, action, flagged, highest_category, highest_score,
    category_scores, threshold_snapshot, input_excerpt, upstream_latency_ms, error,
    violation_count, auto_banned, email_sent, queue_delay_ms, matched_keyword, reason
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12, $13, $14, $15, $16, $17,
    $18::jsonb, $19::jsonb, $20, $21, $22,
    $23, $24, $25, $26, $27, $28
) RETURNING id, created_at`,
		log.RequestID, userID, log.UserEmail, apiKeyID, log.APIKeyName, groupID, log.GroupName,
		log.Endpoint, log.Provider, log.Model, log.AuditEndpointType, log.AuditModel, log.Mode, log.Action, log.Flagged, log.HighestCategory, log.HighestScore,
		string(categoryScores), string(thresholdSnapshot), log.InputExcerpt, latency, log.Error,
		log.ViolationCount, log.AutoBanned, log.EmailSent, nullableIntPtr(log.QueueDelayMS), log.MatchedKeyword, log.Reason,
	).Scan(&log.ID, &log.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert content moderation log: %w", err)
	}
	return createLegacySecurityAuditDecision(ctx, queryer, log, additionalActions)
}

func createLegacySecurityAuditDecision(
	ctx context.Context,
	queryer contentModerationQueryRower,
	log *service.ContentModerationLog,
	additionalActions []string,
) error {
	if log == nil || log.ID <= 0 {
		return nil
	}
	sourceType := "legacy_moderation"
	if log.Action == service.ContentModerationActionCyberPolicy {
		sourceType = "cyber_policy"
	}
	riskLevel, requestAction := legacySecurityRiskAndAction(log)
	evaluationStatus := "complete"
	if log.Action == service.ContentModerationActionError {
		evaluationStatus = "failed"
	}
	decisionID := fmt.Sprintf("dec_%s_%d", sourceType, log.ID)
	auditID := fmt.Sprintf("aud_%s_%d", sourceType, log.ID)
	detectorID := "legacy_content_moderation"
	if log.AuditEndpointType != "" {
		detectorID = "legacy_" + log.AuditEndpointType
	}
	summary := map[string]any{
		"detector_id": detectorID, "detector_version": "legacy-v1",
		"outcome": legacyEvidenceOutcome(log), "category": log.HighestCategory,
		"score": clampLegacyScore(log.HighestScore), "severity": riskLevel,
		"safe_summary": log.Reason, "latency_ms": nullableIntValue(log.UpstreamLatencyMS),
		"error_code": legacyErrorCode(log),
	}
	detectorRaw, _ := json.Marshal([]map[string]any{summary})
	candidateActions := []string{}
	if requestAction == "block" || riskLevel == "critical" {
		candidateActions = append(candidateActions, "open_case")
	}
	candidateActions = appendUniqueLegacyActions(candidateActions, additionalActions...)
	actionsRaw, _ := json.Marshal(candidateActions)
	digestInput, _ := json.Marshal(map[string]any{
		"source_type": sourceType, "source_event_id": log.ID, "request_id": log.RequestID,
		"risk_level": riskLevel, "request_action": requestAction, "detector": summary,
	})
	digest := sha256.Sum256(digestInput)
	var decisionPK int64
	if err := queryer.QueryRowContext(ctx, `
INSERT INTO security_audit_decisions(
    decision_id,audit_id,source_type,source_event_id,request_id,stage,
    user_id,user_snapshot,api_key_id,api_key_snapshot,group_id,group_snapshot,
    provider,endpoint,protocol,requested_model,policy_key,policy_version,
    canonicalizer_version,evaluation_status,risk_level,request_action,
    prompt_hash,redacted_preview,detector_summary,candidate_actions,decision_digest
) VALUES (
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'',$15,
    'legacy-content-moderation',1,'legacy-v1',$16,$17,$18,'',$19,$20::jsonb,$21::jsonb,$22
)
ON CONFLICT (source_type,source_event_id)
DO UPDATE SET
    evaluation_status=EXCLUDED.evaluation_status,
    risk_level=EXCLUDED.risk_level,
    request_action=EXCLUDED.request_action,
    detector_summary=EXCLUDED.detector_summary,
    candidate_actions=EXCLUDED.candidate_actions,
    decision_digest=EXCLUDED.decision_digest
RETURNING id`,
		decisionID, auditID, sourceType, log.ID, log.RequestID, legacySecurityStage(log),
		nullableInt64Pointer(log.UserID), log.UserEmail, nullableInt64Pointer(log.APIKeyID),
		log.APIKeyName, nullableInt64Pointer(log.GroupID), log.GroupName,
		log.Provider, log.Endpoint, log.Model, evaluationStatus, riskLevel, requestAction,
		log.InputExcerpt, detectorRaw, actionsRaw, hex.EncodeToString(digest[:]),
	).Scan(&decisionPK); err != nil {
		return fmt.Errorf("insert unified legacy security decision: %w", err)
	}
	var evidenceID int64
	if err := queryer.QueryRowContext(ctx, `
INSERT INTO security_audit_evidence(
    decision_pk,detector_id,detector_version,outcome,category,score,severity,
    safe_summary,evidence_digest,latency_ms,error_code
) SELECT $1,$2,'legacy-v1',$3,$4,$5,$6,$7,$8,$9,$10
WHERE NOT EXISTS (
    SELECT 1 FROM security_audit_evidence WHERE decision_pk=$1 AND detector_id=$2
)
RETURNING id`,
		decisionPK, detectorID, legacyEvidenceOutcome(log), log.HighestCategory,
		clampLegacyScore(log.HighestScore), riskLevel, log.Reason,
		legacyEvidenceDigest(log), nullableIntValue(log.UpstreamLatencyMS), legacyErrorCode(log),
	).Scan(&evidenceID); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("insert unified legacy security evidence: %w", err)
	}
	for _, actionType := range candidateActions {
		subjectType, subjectID, ok := legacyActionSubject(actionType, log)
		if !ok {
			continue
		}
		actionID := fmt.Sprintf("act_%s_%d_%s", sourceType, log.ID, actionType)
		var actionPK int64
		var persistedActionID string
		if err := queryer.QueryRowContext(ctx, `
INSERT INTO security_audit_actions(
    action_id,decision_pk,action_type,subject_type,subject_id,status,idempotency_key
) VALUES ($1,$2,$3,$4,$5,'pending',$6)
ON CONFLICT (idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key
RETURNING id,action_id`,
			actionID, decisionPK, actionType, subjectType, subjectID,
			fmt.Sprintf("%s:%s:%s:%d:1", decisionID, actionType, subjectType, subjectID),
		).Scan(&actionPK, &persistedActionID); err != nil {
			return fmt.Errorf("insert unified legacy security action: %w", err)
		}
		payload, _ := json.Marshal(map[string]any{
			"action_id": persistedActionID, "action_type": actionType, "decision_id": decisionID,
			"subject_type": subjectType, "subject_id": subjectID, "risk_level": riskLevel,
		})
		var outboxID int64
		if err := queryer.QueryRowContext(ctx, `
INSERT INTO security_audit_outbox(event_id,action_id,topic,payload)
VALUES ($1,$2,'security_audit.enforcement',$3::jsonb)
ON CONFLICT (event_id) DO UPDATE SET event_id=EXCLUDED.event_id
RETURNING id`, "out_"+persistedActionID, actionPK, payload).Scan(&outboxID); err != nil {
			return fmt.Errorf("insert unified legacy security outbox: %w", err)
		}
	}
	return nil
}

func appendUniqueLegacyActions(actions []string, additional ...string) []string {
	seen := make(map[string]struct{}, len(actions)+len(additional))
	result := make([]string, 0, len(actions)+len(additional))
	for _, action := range append(append([]string(nil), actions...), additional...) {
		action = strings.TrimSpace(action)
		switch action {
		case "open_case", "pause_user", "notify_user", "notify_admin":
		default:
			continue
		}
		if _, exists := seen[action]; exists {
			continue
		}
		seen[action] = struct{}{}
		result = append(result, action)
	}
	return result
}

func legacyActionSubject(action string, log *service.ContentModerationLog) (string, int64, bool) {
	switch action {
	case "open_case", "notify_admin":
		return "request", 0, true
	case "pause_user", "notify_user":
		if log == nil || log.UserID == nil || *log.UserID <= 0 {
			return "", 0, false
		}
		return "user", *log.UserID, true
	default:
		return "", 0, false
	}
}

func legacySecurityRiskAndAction(log *service.ContentModerationLog) (string, string) {
	if log == nil {
		return "none", "allow"
	}
	if log.Action == service.ContentModerationActionCyberPolicy {
		return "critical", "block"
	}
	if !log.Flagged {
		if log.Action == service.ContentModerationActionError {
			return "low", "allow"
		}
		return "none", "allow"
	}
	action := "warn"
	switch log.Action {
	case service.ContentModerationActionBlock, service.ContentModerationActionHashBlock,
		service.ContentModerationActionKeywordBlock, service.ContentModerationActionPatternBlock:
		action = "block"
	}
	switch {
	case log.HighestScore >= 0.95:
		return "critical", action
	case log.HighestScore >= 0.80:
		return "high", action
	case log.HighestScore >= 0.50:
		return "medium", action
	default:
		return "low", action
	}
}

func legacySecurityStage(log *service.ContentModerationLog) string {
	if log != nil && log.Action == service.ContentModerationActionCyberPolicy {
		return "post_upstream"
	}
	return "pre_request"
}

func legacyEvidenceOutcome(log *service.ContentModerationLog) string {
	if log == nil {
		return "skipped"
	}
	if log.Action == service.ContentModerationActionError {
		return "error"
	}
	if log.Flagged {
		return "matched"
	}
	return "clear"
}

func legacyErrorCode(log *service.ContentModerationLog) string {
	if log == nil || log.Error == "" {
		return ""
	}
	if log.Action == service.ContentModerationActionCyberPolicy {
		return "cyber_policy"
	}
	return "legacy_moderation_error"
}

func legacyEvidenceDigest(log *service.ContentModerationLog) string {
	if log == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(log.HighestCategory + ":" + log.Reason + ":" + log.MatchedKeyword))
	return hex.EncodeToString(sum[:])
}

func clampLegacyScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func nullableInt64Pointer(value *int64) any {
	if value == nil || *value <= 0 {
		return nil
	}
	return *value
}

func nullableIntValue(value *int) int {
	if value == nil || *value < 0 {
		return 0
	}
	return *value
}

func (r *contentModerationRepository) CreateFlaggedLog(ctx context.Context, log *service.ContentModerationLog, since time.Time, excludeCyberPolicy bool) error {
	_, err := r.createFlaggedLog(ctx, log, since, excludeCyberPolicy, nil)
	return err
}

func (r *contentModerationRepository) CreateFlaggedLogWithEnforcement(
	ctx context.Context,
	log *service.ContentModerationLog,
	since time.Time,
	excludeCyberPolicy bool,
	plan service.ContentModerationEnforcementPlan,
) (service.ContentModerationEnforcementResult, error) {
	return r.createFlaggedLog(ctx, log, since, excludeCyberPolicy, &plan)
}

func (r *contentModerationRepository) createFlaggedLog(
	ctx context.Context,
	log *service.ContentModerationLog,
	since time.Time,
	excludeCyberPolicy bool,
	plan *service.ContentModerationEnforcementPlan,
) (service.ContentModerationEnforcementResult, error) {
	result := service.ContentModerationEnforcementResult{ManagedByUnifiedActions: plan != nil}
	if log == nil {
		return result, nil
	}
	if log.UserID == nil || *log.UserID <= 0 {
		return result, r.CreateLog(ctx, log)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin content moderation flagged log transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, *log.UserID); err != nil {
		return result, fmt.Errorf("lock content moderation user counter: %w", err)
	}
	count := 0
	if !since.IsZero() {
		count, err = countFlaggedByUserSince(ctx, tx, *log.UserID, since, excludeCyberPolicy)
		if err != nil {
			return result, err
		}
	}
	log.ViolationCount = count + 1
	additionalActions := make([]string, 0, 3)
	if plan != nil {
		if plan.NotifyUser {
			additionalActions = append(additionalActions, "notify_user")
		}
		if plan.NotifyAdmin {
			additionalActions = append(additionalActions, "notify_admin")
		}
		if plan.AutoPauseEnabled && plan.PauseThreshold > 0 && log.ViolationCount >= plan.PauseThreshold {
			additionalActions = append(additionalActions, "pause_user")
			result.PauseScheduled = true
		}
	}
	if err = createContentModerationLogWithActions(ctx, tx, log, additionalActions); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, fmt.Errorf("commit content moderation flagged log transaction: %w", err)
	}
	return result, nil
}

func (r *contentModerationRepository) ListLogs(ctx context.Context, filter service.ContentModerationLogFilter) ([]service.ContentModerationLog, *pagination.PaginationResult, error) {
	where, args := buildContentModerationLogWhere(filter)
	whereSQL := "WHERE " + strings.Join(where, " AND ")

	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM content_moderation_logs l "+whereSQL, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("count content moderation logs: %w", err)
	}

	params := filter.Pagination
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, params.Limit(), params.Offset())
	rows, err := r.db.QueryContext(ctx, `
SELECT
    l.id, l.request_id, l.user_id, l.user_email, l.api_key_id, l.api_key_name, l.group_id, l.group_name,
    l.endpoint, l.provider, l.model, l.audit_endpoint_type, l.audit_model, l.mode, l.action, l.flagged, l.highest_category, l.highest_score,
    l.category_scores, l.threshold_snapshot, l.input_excerpt, l.upstream_latency_ms, l.error,
    l.violation_count, l.auto_banned, l.email_sent, COALESCE(u.status, ''), l.queue_delay_ms, l.matched_keyword, l.reason, l.created_at
FROM content_moderation_logs l
LEFT JOIN users u ON u.id = l.user_id `+whereSQL+`
ORDER BY l.created_at DESC, l.id DESC
LIMIT $`+fmt.Sprint(len(queryArgs)-1)+` OFFSET $`+fmt.Sprint(len(queryArgs)),
		queryArgs...,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("list content moderation logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.ContentModerationLog, 0)
	for rows.Next() {
		var item service.ContentModerationLog
		var userID, apiKeyID, groupID, latency, queueDelay sql.NullInt64
		var scoresRaw, thresholdsRaw []byte
		if err := rows.Scan(
			&item.ID,
			&item.RequestID,
			&userID,
			&item.UserEmail,
			&apiKeyID,
			&item.APIKeyName,
			&groupID,
			&item.GroupName,
			&item.Endpoint,
			&item.Provider,
			&item.Model,
			&item.AuditEndpointType,
			&item.AuditModel,
			&item.Mode,
			&item.Action,
			&item.Flagged,
			&item.HighestCategory,
			&item.HighestScore,
			&scoresRaw,
			&thresholdsRaw,
			&item.InputExcerpt,
			&latency,
			&item.Error,
			&item.ViolationCount,
			&item.AutoBanned,
			&item.EmailSent,
			&item.UserStatus,
			&queueDelay,
			&item.MatchedKeyword,
			&item.Reason,
			&item.CreatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("scan content moderation log: %w", err)
		}
		if userID.Valid {
			v := userID.Int64
			item.UserID = &v
		}
		if apiKeyID.Valid {
			v := apiKeyID.Int64
			item.APIKeyID = &v
		}
		if groupID.Valid {
			v := groupID.Int64
			item.GroupID = &v
		}
		if latency.Valid {
			v := int(latency.Int64)
			item.UpstreamLatencyMS = &v
		}
		if queueDelay.Valid {
			v := int(queueDelay.Int64)
			item.QueueDelayMS = &v
		}
		item.CategoryScores = map[string]float64{}
		_ = json.Unmarshal(scoresRaw, &item.CategoryScores)
		item.ThresholdSnapshot = map[string]float64{}
		_ = json.Unmarshal(thresholdsRaw, &item.ThresholdSnapshot)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate content moderation logs: %w", err)
	}
	return items, paginationResultFromTotal(total, params), nil
}

func (r *contentModerationRepository) CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time, excludeCyberPolicy bool) (int, error) {
	if userID <= 0 {
		return 0, nil
	}
	return countFlaggedByUserSince(ctx, r.db, userID, since, excludeCyberPolicy)
}

func countFlaggedByUserSince(ctx context.Context, queryer contentModerationQueryRower, userID int64, since time.Time, excludeCyberPolicy bool) (int, error) {
	// SQL 中的 'cyber_policy' 字面量须与 service.ContentModerationActionCyberPolicy 保持一致。
	var count int
	err := queryer.QueryRowContext(ctx, `
WITH last_auto_ban AS (
    SELECT MAX(created_at) AS at
    FROM content_moderation_logs
    WHERE user_id = $1 AND auto_banned = TRUE
)
SELECT COUNT(*)
FROM content_moderation_logs
WHERE user_id = $1
  AND flagged = TRUE
  AND action <> 'hash_block'
  AND ($3::bool IS FALSE OR action <> 'cyber_policy')
  AND created_at >= $2
  AND created_at > COALESCE((SELECT at FROM last_auto_ban), '-infinity'::timestamptz)
`, userID, since, excludeCyberPolicy).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count user content moderation flagged logs: %w", err)
	}
	return count, nil
}

func (r *contentModerationRepository) UpdateLogEmailSent(ctx context.Context, id int64, sent bool) error {
	_, err := r.db.ExecContext(ctx, `UPDATE content_moderation_logs SET email_sent = $1 WHERE id = $2`, sent, id)
	if err != nil {
		return fmt.Errorf("update content moderation log email_sent: %w", err)
	}
	return nil
}

func (r *contentModerationRepository) UpdateLogOutcomes(ctx context.Context, id int64, autoBanned, emailSent bool) error {
	_, err := r.db.ExecContext(ctx, `
UPDATE content_moderation_logs
SET auto_banned = $1, email_sent = $2
WHERE id = $3
`, autoBanned, emailSent, id)
	if err != nil {
		return fmt.Errorf("update content moderation log outcomes: %w", err)
	}
	return nil
}

func (r *contentModerationRepository) CleanupExpiredLogs(ctx context.Context, hitBefore time.Time, nonHitBefore time.Time) (*service.ContentModerationCleanupResult, error) {
	result := &service.ContentModerationCleanupResult{FinishedAt: time.Now()}
	if r == nil || r.db == nil {
		return result, nil
	}
	hitExec, err := r.db.ExecContext(ctx, `
DELETE FROM content_moderation_logs
WHERE flagged = TRUE AND created_at < $1
`, hitBefore)
	if err != nil {
		return nil, fmt.Errorf("delete expired hit content moderation logs: %w", err)
	}
	result.DeletedHit, _ = hitExec.RowsAffected()

	nonHitExec, err := r.db.ExecContext(ctx, `
DELETE FROM content_moderation_logs
WHERE flagged = FALSE AND created_at < $1
`, nonHitBefore)
	if err != nil {
		return nil, fmt.Errorf("delete expired non-hit content moderation logs: %w", err)
	}
	result.DeletedNonHit, _ = nonHitExec.RowsAffected()

	result.FinishedAt = time.Now()
	return result, nil
}

func nullableIntPtr(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func buildContentModerationLogWhere(filter service.ContentModerationLogFilter) ([]string, []any) {
	where := []string{"l.id IS NOT NULL"}
	args := make([]any, 0)
	add := func(expr string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(expr, len(args)))
	}
	switch strings.ToLower(strings.TrimSpace(filter.Result)) {
	case "hit", "flagged":
		where = append(where, "l.flagged = TRUE")
	case "blocked", "block":
		where = append(where, "l.action IN ('block', 'keyword_block', 'pattern_block', 'hash_block')")
	case "pass", "allow":
		where = append(where, "l.flagged = FALSE AND l.error = ''")
	case "error":
		where = append(where, "l.error <> ''")
	}
	if filter.GroupID != nil {
		add("l.group_id = $%d", *filter.GroupID)
	}
	if endpoint := strings.TrimSpace(filter.Endpoint); endpoint != "" {
		add("l.endpoint = $%d", endpoint)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + search + "%"
		args = append(args, like, like, like, like, like, like, like)
		idx := len(args) - 6
		where = append(where, fmt.Sprintf("(l.request_id ILIKE $%d OR l.user_email ILIKE $%d OR l.api_key_name ILIKE $%d OR l.model ILIKE $%d OR l.audit_model ILIKE $%d OR l.input_excerpt ILIKE $%d OR l.reason ILIKE $%d)", idx, idx+1, idx+2, idx+3, idx+4, idx+5, idx+6))
	}
	if filter.From != nil && !filter.From.IsZero() {
		add("l.created_at >= $%d", *filter.From)
	}
	if filter.To != nil && !filter.To.IsZero() {
		add("l.created_at <= $%d", *filter.To)
	}
	return where, args
}
