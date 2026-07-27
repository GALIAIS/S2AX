package securityaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (r *PostgreSQLRepository) ListActions(ctx context.Context, filter ActionFilter, page, pageSize int) (*ActionPage, error) {
	page, pageSize = normalizePage(page, pageSize)
	where, args := buildActionWhere(filter)
	var total int64
	if err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM security_audit_actions a
JOIN security_audit_decisions d ON d.id=a.decision_pk`+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	limitIndex := len(args) + 1
	queryArgs := append(append([]any(nil), args...), pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, `
SELECT `+actionColumns("a")+`
FROM security_audit_actions a
JOIN security_audit_decisions d ON d.id=a.decision_pk`+where+
		fmt.Sprintf(` ORDER BY a.created_at DESC,a.id DESC LIMIT $%d OFFSET $%d`, limitIndex, limitIndex+1),
		queryArgs...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*EnforcementAction, 0, pageSize)
	for rows.Next() {
		item, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	pages := 0
	if total > 0 {
		pages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return &ActionPage{Items: items, Total: total, Page: page, PageSize: pageSize, Pages: pages}, rows.Err()
}

func (r *PostgreSQLRepository) GetAction(ctx context.Context, id int64) (*EnforcementAction, error) {
	item, err := scanAction(r.db.QueryRowContext(ctx, `
SELECT `+actionColumns("a")+`
FROM security_audit_actions a WHERE a.id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrActionNotFound
	}
	return item, err
}

func (r *PostgreSQLRepository) RetryAction(ctx context.Context, id, actorID int64) (*EnforcementAction, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	item, err := scanAction(tx.QueryRowContext(ctx, `
UPDATE security_audit_actions
SET status='retry',attempts=0,next_attempt_at=NOW(),lease_owner='',lease_expires_at=NULL,
    error_code='',error_message='',requested_by=COALESCE($2,requested_by),updated_at=NOW()
WHERE id=$1 AND status IN ('failed','cancelled')
RETURNING `+actionColumns("security_audit_actions"), id, nullableID(actorID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidTransition
	}
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE security_audit_outbox
SET status='retry',attempts=0,next_attempt_at=NOW(),lease_owner='',lease_expires_at=NULL,
    last_error='',updated_at=NOW()
WHERE action_id=$1`, id)
	if err != nil {
		return nil, err
	}
	if err = requireExactlyOneRow(result, "security audit retry outbox"); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *PostgreSQLRepository) CancelAction(ctx context.Context, id, actorID int64) (*EnforcementAction, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	item, err := scanAction(tx.QueryRowContext(ctx, `
UPDATE security_audit_actions
SET status='cancelled',cancelled_at=NOW(),lease_owner='',lease_expires_at=NULL,
    requested_by=COALESCE($2,requested_by),updated_at=NOW()
WHERE id=$1 AND status IN ('pending','retry','failed')
RETURNING `+actionColumns("security_audit_actions"), id, nullableID(actorID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidTransition
	}
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE security_audit_outbox
SET status='discarded',lease_owner='',lease_expires_at=NULL,updated_at=NOW()
WHERE action_id=$1 AND status IN ('pending','retry','failed')`, id)
	if err != nil {
		return nil, err
	}
	if err = requireExactlyOneRow(result, "security audit cancel outbox"); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *PostgreSQLRepository) RevertAction(ctx context.Context, id, actorID int64) (*EnforcementAction, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	action, err := scanAction(tx.QueryRowContext(ctx, `
SELECT `+actionColumns("a")+`
FROM security_audit_actions a WHERE a.id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrActionNotFound
	}
	if err != nil {
		return nil, err
	}
	if action.Status != ActionStatusSucceeded {
		return nil, ErrInvalidTransition
	}
	switch action.ActionType {
	case "open_case":
		result, updateErr := tx.ExecContext(ctx, `
UPDATE security_audit_cases
SET status='dismissed',resolution='dismissed',resolution_note='关联动作由管理员撤销',
    resolved_at=NOW(),updated_at=NOW()
WHERE primary_decision_pk=$1 AND status IN ('open','reviewing')`, action.DecisionPK)
		if updateErr != nil {
			return nil, updateErr
		}
		if err = requireExactlyOneRow(result, "security audit open-case revert"); err != nil {
			return nil, fmt.Errorf("%w: linked case is no longer reversible", ErrInvalidTransition)
		}
	case "pause_api_key", "pause_user":
		if err = revertPauseActionInTx(ctx, tx, action); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: action %s is not safely reversible", ErrInvalidTransition, action.ActionType)
	}
	action, err = scanAction(tx.QueryRowContext(ctx, `
UPDATE security_audit_actions
SET status='reverted',reverted_at=NOW(),requested_by=COALESCE($2,requested_by),updated_at=NOW()
WHERE id=$1
RETURNING `+actionColumns("security_audit_actions"), id, nullableID(actorID)))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return action, nil
}

func (r *PostgreSQLRepository) ClaimNextAction(ctx context.Context, owner string, lease time.Duration) (*EnforcementAction, bool, error) {
	if lease <= 0 {
		lease = 30 * time.Second
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var actionID int64
	err = tx.QueryRowContext(ctx, `
SELECT a.id
FROM security_audit_outbox o
JOIN security_audit_actions a ON a.id=o.action_id
WHERE o.status IN ('pending','retry')
  AND o.next_attempt_at<=NOW()
  AND a.status IN ('pending','retry')
  AND a.next_attempt_at<=NOW()
ORDER BY o.next_attempt_at,o.id
FOR UPDATE OF o,a SKIP LOCKED
LIMIT 1`).Scan(&actionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	item, err := scanAction(tx.QueryRowContext(ctx, `
UPDATE security_audit_actions
SET status='processing',attempts=attempts+1,lease_owner=$2,
    lease_expires_at=NOW()+($3 * INTERVAL '1 millisecond'),updated_at=NOW()
WHERE id=$1
RETURNING `+actionColumns("security_audit_actions"), actionID, owner, lease.Milliseconds()))
	if err != nil {
		return nil, false, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE security_audit_outbox
SET status='processing',attempts=attempts+1,lease_owner=$2,
    lease_expires_at=NOW()+($3 * INTERVAL '1 millisecond'),updated_at=NOW()
WHERE action_id=$1 AND status IN ('pending','retry')`,
		actionID, owner, lease.Milliseconds())
	if err != nil {
		return nil, false, err
	}
	if err = requireExactlyOneRow(result, "security audit claim outbox"); err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, true, nil
}

func (r *PostgreSQLRepository) ExecuteClaimedAction(ctx context.Context, action *EnforcementAction, owner string) error {
	if action == nil {
		return nil
	}
	switch action.ActionType {
	case "open_case":
		return r.executeOpenCaseAction(ctx, action, owner)
	case "notify_admin", "notify_user":
		return r.executeNotificationAction(ctx, action, owner)
	case "pause_api_key":
		return r.executePauseSubjectAction(ctx, action, owner, "api_keys", false)
	case "pause_user":
		return r.executePauseSubjectAction(ctx, action, owner, "users", true)
	default:
		return r.failClaimedAction(ctx, action, owner, "unsupported_action", "动作执行器尚不支持该动作")
	}
}

func (r *PostgreSQLRepository) executePauseSubjectAction(
	ctx context.Context,
	action *EnforcementAction,
	owner, table string,
	protectAdmin bool,
) error {
	if action.SubjectID <= 0 {
		return r.failClaimedAction(ctx, action, owner, "invalid_subject", "处置主体无效")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var currentStatus string
	var role string
	if protectAdmin {
		err = tx.QueryRowContext(ctx, `SELECT status,role FROM users WHERE id=$1 FOR UPDATE`, action.SubjectID).Scan(&currentStatus, &role)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT status,'' FROM api_keys WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, action.SubjectID).Scan(&currentStatus, &role)
	}
	if errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		return r.failClaimedAction(ctx, action, owner, "subject_not_found", "处置主体不存在")
	}
	if err != nil {
		return err
	}
	if protectAdmin && role == "admin" {
		_ = tx.Rollback()
		return r.failClaimedAction(ctx, action, owner, "admin_protected", "管理员账户不得被自动暂停")
	}
	before, _ := json.Marshal(map[string]any{"status": currentStatus})
	changed := currentStatus != "disabled"
	if changed {
		query := `UPDATE ` + table + ` SET status='disabled',updated_at=NOW() WHERE id=$1`
		if _, err = tx.ExecContext(ctx, query, action.SubjectID); err != nil {
			return err
		}
		if action.ActionType == "pause_user" {
			if _, err = tx.ExecContext(ctx, `
UPDATE content_moderation_logs
SET auto_banned=TRUE
WHERE id=(
    SELECT source_event_id
    FROM security_audit_decisions
    WHERE id=$1
      AND source_type IN ('legacy_moderation','cyber_policy')
      AND source_event_id IS NOT NULL
)`, action.DecisionPK); err != nil {
				return err
			}
		}
	}
	after, _ := json.Marshal(map[string]any{"status": "disabled", "changed": changed})
	if err = markClaimedActionSucceeded(ctx, tx, action, owner, before, after); err != nil {
		return err
	}
	return tx.Commit()
}

func markClaimedActionSucceeded(
	ctx context.Context,
	tx *sql.Tx,
	action *EnforcementAction,
	owner string,
	before, after []byte,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE security_audit_actions
SET status='succeeded',before_snapshot=$4::jsonb,after_snapshot=$5::jsonb,
    processed_at=NOW(),lease_owner='',lease_expires_at=NULL,error_code='',
    error_message='',updated_at=NOW()
WHERE id=$1 AND status='processing' AND lease_owner=$2 AND attempts=$3`,
		action.ID, owner, action.Attempts, before, after)
	if err != nil {
		return err
	}
	if err = requireExactlyOneRow(result, "security audit action lease"); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `
UPDATE security_audit_outbox
SET status='published',published_at=NOW(),lease_owner='',lease_expires_at=NULL,updated_at=NOW()
WHERE action_id=$1 AND status='processing' AND lease_owner=$2`, action.ID, owner)
	if err != nil {
		return err
	}
	return requireExactlyOneRow(result, "security audit publish outbox")
}

func revertSubjectStatus(ctx context.Context, tx *sql.Tx, table string, subjectID int64, beforeRaw json.RawMessage) error {
	if subjectID <= 0 {
		return ErrInvalidTransition
	}
	var before struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(beforeRaw, &before); err != nil || before.Status == "" {
		return fmt.Errorf("%w: before snapshot unavailable", ErrInvalidTransition)
	}
	var current string
	query := `SELECT status FROM ` + table + ` WHERE id=$1 FOR UPDATE`
	if err := tx.QueryRowContext(ctx, query, subjectID).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: subject not found", ErrInvalidTransition)
	} else if err != nil {
		return err
	}
	if current != "disabled" {
		return fmt.Errorf("%w: subject status changed after enforcement", ErrInvalidTransition)
	}
	update := `UPDATE ` + table + ` SET status=$2,updated_at=NOW() WHERE id=$1 AND status='disabled'`
	result, err := tx.ExecContext(ctx, update, subjectID, before.Status)
	if err != nil {
		return err
	}
	return requireExactlyOneRow(result, "security audit subject revert")
}

func revertPauseActionInTx(ctx context.Context, tx *sql.Tx, action *EnforcementAction) error {
	if action == nil {
		return ErrInvalidTransition
	}
	table := ""
	switch action.ActionType {
	case "pause_api_key":
		table = "api_keys"
	case "pause_user":
		table = "users"
	default:
		return fmt.Errorf("%w: action %s is not a pause action", ErrInvalidTransition, action.ActionType)
	}
	if err := revertSubjectStatus(ctx, tx, table, action.SubjectID, action.BeforeSnapshot); err != nil {
		return err
	}
	if action.ActionType == "pause_user" {
		if _, err := tx.ExecContext(ctx, `
UPDATE content_moderation_logs
SET auto_banned=FALSE
WHERE id=(
    SELECT source_event_id
    FROM security_audit_decisions
    WHERE id=$1
      AND source_type IN ('legacy_moderation','cyber_policy')
      AND source_event_id IS NOT NULL
)`, action.DecisionPK); err != nil {
			return err
		}
	}
	return nil
}

func (r *PostgreSQLRepository) executeOpenCaseAction(ctx context.Context, action *EnforcementAction, owner string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var decisionID, risk, requestID, preview string
	if err = tx.QueryRowContext(ctx, `
SELECT decision_id,risk_level,request_id,redacted_preview
FROM security_audit_decisions
WHERE id=$1`, action.DecisionPK).Scan(&decisionID, &risk, &requestID, &preview); err != nil {
		_ = tx.Rollback()
		return r.retryClaimedAction(ctx, action, owner, "decision_unavailable", err.Error())
	}
	title := "安全审计案件"
	if requestID != "" {
		title = "安全审计 · " + TrimRunes(requestID, 96)
	}
	caseID := newSecurityID("case")
	var casePK int64
	var persistedCaseID string
	err = tx.QueryRowContext(ctx, `
INSERT INTO security_audit_cases(
    case_id,primary_decision_pk,title,severity,status,opened_reason
) VALUES ($1,$2,$3,$4,'open',$5)
ON CONFLICT (primary_decision_pk) WHERE status IN ('open','reviewing')
DO UPDATE SET updated_at=NOW()
RETURNING id,case_id`, caseID, action.DecisionPK, title, normalizeCaseSeverity(risk),
		TrimRunes(defaultString(preview, "自动高风险判定"), 512)).Scan(&casePK, &persistedCaseID)
	if err != nil {
		return err
	}
	details, _ := json.Marshal(map[string]any{"action_id": action.ActionID, "decision_id": decisionID})
	if _, err = tx.ExecContext(ctx, `
INSERT INTO security_audit_case_events(case_pk,event_type,summary,details)
VALUES ($1,'opened','由安全策略自动开案',$2::jsonb)`, casePK, details); err != nil {
		return err
	}
	before := []byte(`{}`)
	after, _ := json.Marshal(map[string]any{"case_id": persistedCaseID, "case_pk": casePK})
	if err = markClaimedActionSucceeded(ctx, tx, action, owner, before, after); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgreSQLRepository) retryClaimedAction(ctx context.Context, action *EnforcementAction, owner, code, message string) error {
	if action.Attempts >= action.MaxAttempts {
		return r.failClaimedAction(ctx, action, owner, code, message)
	}
	delay := time.Second * time.Duration(1<<min(action.Attempts, 8))
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
UPDATE security_audit_actions
SET status='retry',next_attempt_at=NOW()+($4 * INTERVAL '1 millisecond'),
    lease_owner='',lease_expires_at=NULL,error_code=$5,error_message=$6,updated_at=NOW()
WHERE id=$1 AND status='processing' AND lease_owner=$2 AND attempts=$3`,
		action.ID, owner, action.Attempts, delay.Milliseconds(), code, TrimRunes(message, 512))
	if err != nil {
		return err
	}
	if err = requireExactlyOneRow(result, "security audit retry action lease"); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `
UPDATE security_audit_outbox
SET status='retry',next_attempt_at=NOW()+($3 * INTERVAL '1 millisecond'),
    lease_owner='',lease_expires_at=NULL,last_error=$4,updated_at=NOW()
WHERE action_id=$1 AND status='processing' AND lease_owner=$2`,
		action.ID, owner, delay.Milliseconds(), TrimRunes(message, 512))
	if err != nil {
		return err
	}
	if err = requireExactlyOneRow(result, "security audit retry outbox lease"); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgreSQLRepository) failClaimedAction(ctx context.Context, action *EnforcementAction, owner, code, message string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
UPDATE security_audit_actions
SET status='failed',processed_at=NOW(),lease_owner='',lease_expires_at=NULL,
    error_code=$4,error_message=$5,updated_at=NOW()
WHERE id=$1 AND status='processing' AND lease_owner=$2 AND attempts=$3`,
		action.ID, owner, action.Attempts, code, TrimRunes(message, 512))
	if err != nil {
		return err
	}
	if err = requireExactlyOneRow(result, "security audit fail action lease"); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `
UPDATE security_audit_outbox
SET status='failed',lease_owner='',lease_expires_at=NULL,last_error=$3,updated_at=NOW()
WHERE action_id=$1 AND status='processing' AND lease_owner=$2`,
		action.ID, owner, TrimRunes(message, 512))
	if err != nil {
		return err
	}
	if err = requireExactlyOneRow(result, "security audit fail outbox lease"); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgreSQLRepository) ReclaimStaleActions(ctx context.Context) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
WITH reclaimed_actions AS (
    UPDATE security_audit_actions
    SET status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'retry' END,
        next_attempt_at=NOW(),lease_owner='',lease_expires_at=NULL,
        error_code='lease_expired',error_message='worker lease expired',updated_at=NOW()
    WHERE status='processing' AND lease_expires_at<NOW()
    RETURNING id,status
)
UPDATE security_audit_outbox o
SET status=CASE WHEN a.status='failed' THEN 'failed' ELSE 'retry' END,
    next_attempt_at=NOW(),lease_owner='',lease_expires_at=NULL,
    last_error='worker lease expired',updated_at=NOW()
FROM reclaimed_actions a
WHERE o.action_id=a.id`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func actionColumns(alias string) string {
	return alias + `.id,` + alias + `.action_id,` + alias + `.decision_pk,` + alias + `.action_type,` +
		alias + `.subject_type,` + alias + `.subject_id,` + alias + `.status,` + alias + `.idempotency_key,` +
		alias + `.policy_action_version,` + alias + `.attempts,` + alias + `.max_attempts,` +
		alias + `.lease_owner,` + alias + `.lease_expires_at,` + alias + `.next_attempt_at,` +
		alias + `.before_snapshot,` + alias + `.after_snapshot,` + alias + `.error_code,` +
		alias + `.error_message,` + alias + `.requested_by,` + alias + `.created_at,` +
		alias + `.processed_at,` + alias + `.cancelled_at,` + alias + `.reverted_at,` + alias + `.updated_at`
}

func scanAction(row rowScanner) (*EnforcementAction, error) {
	item := &EnforcementAction{}
	var leaseExpiresAt, processedAt, cancelledAt, revertedAt sql.NullTime
	var requestedBy sql.NullInt64
	if err := row.Scan(
		&item.ID, &item.ActionID, &item.DecisionPK, &item.ActionType, &item.SubjectType,
		&item.SubjectID, &item.Status, &item.IdempotencyKey, &item.PolicyActionVersion,
		&item.Attempts, &item.MaxAttempts, &item.LeaseOwner, &leaseExpiresAt,
		&item.NextAttemptAt, &item.BeforeSnapshot, &item.AfterSnapshot, &item.ErrorCode,
		&item.ErrorMessage, &requestedBy, &item.CreatedAt, &processedAt, &cancelledAt,
		&revertedAt, &item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	item.LeaseExpiresAt = nullableTime(leaseExpiresAt)
	item.ProcessedAt = nullableTime(processedAt)
	item.CancelledAt = nullableTime(cancelledAt)
	item.RevertedAt = nullableTime(revertedAt)
	if requestedBy.Valid {
		item.RequestedBy = &requestedBy.Int64
	}
	return item, nil
}

func buildActionWhere(filter ActionFilter) (string, []any) {
	clauses := []string{" WHERE 1=1"}
	args := make([]any, 0)
	add := func(template string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(template, len(args)))
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		add(" AND a.status=$%d", value)
	}
	if value := strings.TrimSpace(filter.ActionType); value != "" {
		add(" AND a.action_type=$%d", value)
	}
	if value := strings.TrimSpace(filter.SubjectType); value != "" {
		add(" AND a.subject_type=$%d", value)
	}
	if filter.SubjectID != nil {
		add(" AND a.subject_id=$%d", *filter.SubjectID)
	}
	if value := strings.TrimSpace(filter.DecisionID); value != "" {
		add(" AND d.decision_id=$%d", value)
	}
	return strings.Join(clauses, ""), args
}

func (r *PostgreSQLRepository) ListCases(ctx context.Context, filter CaseFilter, page, pageSize int) (*CasePage, error) {
	page, pageSize = normalizePage(page, pageSize)
	where, args := buildCaseWhere(filter)
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_audit_cases c`+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	limitIndex := len(args) + 1
	queryArgs := append(append([]any(nil), args...), pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, `
SELECT `+caseColumns("c")+`
FROM security_audit_cases c`+where+
		fmt.Sprintf(` ORDER BY
            CASE c.severity WHEN 'critical' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 ELSE 1 END DESC,
            c.updated_at DESC,c.id DESC
            LIMIT $%d OFFSET $%d`, limitIndex, limitIndex+1), queryArgs...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*AuditCase, 0, pageSize)
	for rows.Next() {
		item, err := scanCase(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	pages := 0
	if total > 0 {
		pages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return &CasePage{Items: items, Total: total, Page: page, PageSize: pageSize, Pages: pages}, rows.Err()
}

func (r *PostgreSQLRepository) GetCase(ctx context.Context, id int64) (*AuditCase, error) {
	item, err := scanCase(r.db.QueryRowContext(ctx, `
SELECT `+caseColumns("c")+` FROM security_audit_cases c WHERE c.id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCaseNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id,event_type,actor_id,summary,details,created_at
FROM security_audit_case_events WHERE case_pk=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	item.Timeline = []AuditCaseEvent{}
	for rows.Next() {
		var event AuditCaseEvent
		var actorID sql.NullInt64
		if err := rows.Scan(&event.ID, &event.EventType, &actorID, &event.Summary, &event.Details, &event.CreatedAt); err != nil {
			return nil, err
		}
		if actorID.Valid {
			event.ActorID = &actorID.Int64
		}
		item.Timeline = append(item.Timeline, event)
	}
	return item, rows.Err()
}

func (r *PostgreSQLRepository) OpenCaseForDecision(ctx context.Context, decisionPK, actorID int64, reason string) (*AuditCase, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var decisionID, risk, requestID string
	if err = tx.QueryRowContext(ctx, `
SELECT decision_id,risk_level,request_id FROM security_audit_decisions WHERE id=$1`,
		decisionPK).Scan(&decisionID, &risk, &requestID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDecisionNotFound
	} else if err != nil {
		return nil, err
	}
	caseID := newSecurityID("case")
	item, err := scanCase(tx.QueryRowContext(ctx, `
INSERT INTO security_audit_cases(
    case_id,primary_decision_pk,title,severity,status,opened_reason,created_by
) VALUES ($1,$2,$3,$4,'open',$5,$6)
ON CONFLICT (primary_decision_pk) WHERE status IN ('open','reviewing')
DO UPDATE SET updated_at=NOW()
RETURNING `+caseColumns("security_audit_cases"),
		caseID, decisionPK, "安全审计 · "+defaultString(requestID, decisionID),
		normalizeCaseSeverity(risk), TrimRunes(strings.TrimSpace(reason), 512), nullableID(actorID)))
	if err != nil {
		return nil, err
	}
	details, _ := json.Marshal(map[string]any{"decision_id": decisionID})
	if _, err = tx.ExecContext(ctx, `
INSERT INTO security_audit_case_events(case_pk,event_type,actor_id,summary,details)
VALUES ($1,'opened',$2,'管理员创建案件',$3::jsonb)`,
		item.ID, nullableID(actorID), details); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *PostgreSQLRepository) TransitionCase(ctx context.Context, id, actorID int64, request CaseTransitionRequest) (*AuditCase, error) {
	target := strings.TrimSpace(request.Status)
	valid := map[string]bool{
		"open": true, "reviewing": true, "confirmed": true,
		"false_positive": true, "dismissed": true, "expired": true,
	}
	if !valid[target] {
		return nil, ErrInvalidTransition
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM security_audit_cases WHERE id=$1 FOR UPDATE`, id).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCaseNotFound
	} else if err != nil {
		return nil, err
	}
	if !validCaseTransition(current, target) {
		return nil, ErrInvalidTransition
	}
	labels := canonicalStrings(request.Labels)
	labelsRaw, _ := json.Marshal(labels)
	resolution := ""
	var resolvedAt any
	if target == "confirmed" || target == "false_positive" || target == "dismissed" || target == "expired" {
		resolution = target
		resolvedAt = time.Now()
	}
	item, err := scanCase(tx.QueryRowContext(ctx, `
UPDATE security_audit_cases
SET status=$2,resolution=$3,resolution_note=$4,labels=$5::jsonb,
    assignee_id=$6,resolved_at=$7,updated_at=NOW()
WHERE id=$1
RETURNING `+caseColumns("security_audit_cases"),
		id, target, resolution, TrimRunes(strings.TrimSpace(request.ResolutionNote), 4000),
		labelsRaw, request.AssigneeID, resolvedAt))
	if err != nil {
		return nil, err
	}
	details, _ := json.Marshal(map[string]any{"from": current, "to": target, "revert_actions": request.RevertActions})
	if _, err = tx.ExecContext(ctx, `
INSERT INTO security_audit_case_events(case_pk,event_type,actor_id,summary,details)
VALUES ($1,'status_changed',$2,$3,$4::jsonb)`,
		id, nullableID(actorID), "案件状态由 "+current+" 变更为 "+target, details); err != nil {
		return nil, err
	}
	if target == "false_positive" && request.RevertActions {
		if item.PrimaryDecisionPK == nil {
			return nil, ErrInvalidTransition
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE security_audit_outbox AS outbox
SET status='discarded',lease_owner='',lease_expires_at=NULL,
    last_error='cancelled after false-positive review',updated_at=NOW()
FROM security_audit_actions AS action
WHERE outbox.action_id=action.id
  AND action.decision_pk=$1
  AND action.status IN ('pending','retry','failed')
  AND outbox.status IN ('pending','retry','failed')`, *item.PrimaryDecisionPK); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE security_audit_actions
SET status='cancelled',cancelled_at=NOW(),requested_by=COALESCE($2,requested_by),updated_at=NOW()
WHERE decision_pk=$1 AND status IN ('pending','retry','failed')`,
			*item.PrimaryDecisionPK, nullableID(actorID)); err != nil {
			return nil, err
		}
		reverted, revertErr := revertSucceededPauseActionsForDecision(
			ctx,
			tx,
			*item.PrimaryDecisionPK,
			actorID,
		)
		if revertErr != nil {
			return nil, revertErr
		}
		if reverted > 0 {
			revertDetails, _ := json.Marshal(map[string]any{
				"decision_pk": *item.PrimaryDecisionPK,
				"count":       reverted,
			})
			if _, err = tx.ExecContext(ctx, `
INSERT INTO security_audit_case_events(case_pk,event_type,actor_id,summary,details)
VALUES ($1,'actions_reverted',$2,$3,$4::jsonb)`,
				id, nullableID(actorID), "误判结论已安全撤销主体暂停动作", revertDetails); err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

func revertSucceededPauseActionsForDecision(
	ctx context.Context,
	tx *sql.Tx,
	decisionPK, actorID int64,
) (int, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT `+actionColumns("a")+`
FROM security_audit_actions a
WHERE a.decision_pk=$1
  AND a.status='succeeded'
  AND a.action_type IN ('pause_api_key','pause_user')
ORDER BY a.id
FOR UPDATE`, decisionPK)
	if err != nil {
		return 0, err
	}
	actions := make([]*EnforcementAction, 0)
	for rows.Next() {
		action, scanErr := scanAction(rows)
		if scanErr != nil {
			_ = rows.Close()
			return 0, scanErr
		}
		actions = append(actions, action)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	for _, action := range actions {
		if err = revertPauseActionInTx(ctx, tx, action); err != nil {
			return 0, err
		}
		result, updateErr := tx.ExecContext(ctx, `
UPDATE security_audit_actions
SET status='reverted',reverted_at=NOW(),requested_by=COALESCE($2,requested_by),updated_at=NOW()
WHERE id=$1 AND status='succeeded'`, action.ID, nullableID(actorID))
		if updateErr != nil {
			return 0, updateErr
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return 0, errors.New("security audit action changed during false-positive revert")
		}
	}
	return len(actions), nil
}

func caseColumns(alias string) string {
	return alias + `.id,` + alias + `.case_id,` + alias + `.primary_decision_pk,` + alias + `.title,` +
		alias + `.severity,` + alias + `.status,` + alias + `.assignee_id,` + alias + `.opened_reason,` +
		alias + `.resolution,` + alias + `.resolution_note,` + alias + `.labels,` + alias + `.created_by,` +
		alias + `.created_at,` + alias + `.updated_at,` + alias + `.resolved_at,` + alias + `.expires_at`
}

func scanCase(row rowScanner) (*AuditCase, error) {
	item := &AuditCase{}
	var primaryDecision, assigneeID, createdBy sql.NullInt64
	var labelsRaw []byte
	var resolvedAt, expiresAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.CaseID, &primaryDecision, &item.Title, &item.Severity,
		&item.Status, &assigneeID, &item.OpenedReason, &item.Resolution,
		&item.ResolutionNote, &labelsRaw, &createdBy, &item.CreatedAt,
		&item.UpdatedAt, &resolvedAt, &expiresAt,
	); err != nil {
		return nil, err
	}
	if primaryDecision.Valid {
		item.PrimaryDecisionPK = &primaryDecision.Int64
	}
	if assigneeID.Valid {
		item.AssigneeID = &assigneeID.Int64
	}
	if createdBy.Valid {
		item.CreatedBy = &createdBy.Int64
	}
	item.ResolvedAt = nullableTime(resolvedAt)
	item.ExpiresAt = nullableTime(expiresAt)
	_ = json.Unmarshal(labelsRaw, &item.Labels)
	if item.Labels == nil {
		item.Labels = []string{}
	}
	return item, nil
}

func buildCaseWhere(filter CaseFilter) (string, []any) {
	clauses := []string{" WHERE 1=1"}
	args := make([]any, 0)
	add := func(template string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(template, len(args)))
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		add(" AND c.status=$%d", value)
	}
	if value := strings.TrimSpace(filter.Severity); value != "" {
		add(" AND c.severity=$%d", value)
	}
	if filter.AssigneeID != nil {
		add(" AND c.assignee_id=$%d", *filter.AssigneeID)
	}
	if value := strings.TrimSpace(filter.Keyword); value != "" {
		add(" AND (c.title ILIKE $%d OR c.opened_reason ILIKE $%d OR c.resolution_note ILIKE $%d)", "%"+value+"%")
		index := len(args)
		clauses[len(clauses)-1] = fmt.Sprintf(
			" AND (c.title ILIKE $%d OR c.opened_reason ILIKE $%d OR c.resolution_note ILIKE $%d)",
			index, index, index,
		)
	}
	return strings.Join(clauses, ""), args
}

func validCaseTransition(current, target string) bool {
	if current == target {
		return true
	}
	allowed := map[string]map[string]bool{
		"open":      {"reviewing": true, "confirmed": true, "false_positive": true, "dismissed": true, "expired": true},
		"reviewing": {"open": true, "confirmed": true, "false_positive": true, "dismissed": true, "expired": true},
	}
	return allowed[current][target]
}

func normalizeCaseSeverity(value string) string {
	switch strings.TrimSpace(value) {
	case "low", "medium", "high", "critical":
		return strings.TrimSpace(value)
	default:
		return "medium"
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func requireExactlyOneRow(result sql.Result, operation string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s rows affected: %w", operation, err)
	}
	if affected != 1 {
		return fmt.Errorf("%s expected one affected row, got %d", operation, affected)
	}
	return nil
}
