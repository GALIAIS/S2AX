package securityaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

func (r *PostgreSQLRepository) executeNotificationAction(
	ctx context.Context,
	action *EnforcementAction,
	owner string,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var decisionID, risk, preview string
	var userID sql.NullInt64
	if err = tx.QueryRowContext(ctx, `
SELECT decision_id,risk_level,redacted_preview,user_id
FROM security_audit_decisions
WHERE id=$1
FOR SHARE`, action.DecisionPK).Scan(&decisionID, &risk, &preview, &userID); err != nil {
		return err
	}
	audience := "admin"
	var recipient any
	if action.ActionType == "notify_user" {
		audience = "user"
		if !userID.Valid || userID.Int64 <= 0 {
			_ = tx.Rollback()
			return r.failClaimedAction(ctx, action, owner, "recipient_unavailable", "用户通知缺少接收人")
		}
		recipient = userID.Int64
	}
	title := "安全审计提醒"
	if audience == "user" {
		title = "请求安全提醒"
	}
	body := TrimRunes(strings.TrimSpace(preview), 2000)
	if body == "" {
		body = "安全策略检测到需要关注的活动；审计编号：" + decisionID
	}
	notificationID := newSecurityID("ntf")
	var notificationPK int64
	var persistedNotificationID string
	if err = tx.QueryRowContext(ctx, `
INSERT INTO security_audit_notifications(
    notification_id,action_id,decision_pk,audience,recipient_user_id,
    severity,title,body,status
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'unread')
ON CONFLICT (action_id)
DO UPDATE SET action_id=EXCLUDED.action_id
RETURNING id,notification_id`,
		notificationID, action.ID, action.DecisionPK, audience, recipient,
		normalizeCaseSeverity(risk), title, body,
	).Scan(&notificationPK, &persistedNotificationID); err != nil {
		return err
	}
	before := []byte(`{}`)
	after, _ := json.Marshal(map[string]any{
		"notification_id": persistedNotificationID,
		"notification_pk": notificationPK,
		"audience":        audience,
	})
	if err = markClaimedActionSucceeded(ctx, tx, action, owner, before, after); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgreSQLRepository) ListSecurityAuditNotifications(
	ctx context.Context,
	status, audience string,
	limit int,
) ([]*SecurityAuditNotification, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	clauses := []string{"WHERE 1=1"}
	args := make([]any, 0, 3)
	if value := strings.TrimSpace(status); value != "" {
		args = append(args, value)
		clauses = append(clauses, "AND n.status=$"+strconv.Itoa(len(args)))
	}
	if value := strings.TrimSpace(audience); value != "" {
		args = append(args, value)
		clauses = append(clauses, "AND n.audience=$"+strconv.Itoa(len(args)))
	}
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, `
SELECT id,notification_id,action_id,decision_pk,audience,recipient_user_id,
       severity,title,body,status,read_at,created_at
FROM security_audit_notifications n
`+strings.Join(clauses, " ")+`
ORDER BY n.created_at DESC,n.id DESC
LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*SecurityAuditNotification, 0, limit)
	for rows.Next() {
		item, err := scanSecurityAuditNotification(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) ListUserSecurityAuditNotifications(
	ctx context.Context,
	userID int64,
	status string,
	limit int,
) ([]*SecurityAuditNotification, error) {
	if userID <= 0 {
		return nil, ErrNotificationNotFound
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	args := []any{userID}
	statusClause := ""
	if value := strings.TrimSpace(status); value != "" {
		if !validNotificationStatus(value) {
			return nil, ErrInvalidTransition
		}
		args = append(args, value)
		statusClause = "AND n.status=$2"
	}
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, `
SELECT id,notification_id,action_id,decision_pk,audience,recipient_user_id,
       severity,title,body,status,read_at,created_at
FROM security_audit_notifications n
WHERE n.audience='user'
  AND n.recipient_user_id=$1
  `+statusClause+`
ORDER BY n.created_at DESC,n.id DESC
LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*SecurityAuditNotification, 0, limit)
	for rows.Next() {
		item, scanErr := scanSecurityAuditNotification(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) UpdateSecurityAuditNotificationStatus(
	ctx context.Context,
	id int64,
	status string,
) (*SecurityAuditNotification, error) {
	status = strings.TrimSpace(status)
	if !validNotificationStatus(status) {
		return nil, ErrInvalidTransition
	}
	item, err := scanSecurityAuditNotification(r.db.QueryRowContext(ctx, `
UPDATE security_audit_notifications
SET status=$2,
    read_at=CASE WHEN $2='read' THEN COALESCE(read_at,NOW()) WHEN $2='unread' THEN NULL ELSE read_at END
WHERE id=$1
  AND audience='admin'
RETURNING id,notification_id,action_id,decision_pk,audience,recipient_user_id,
          severity,title,body,status,read_at,created_at`,
		id, status,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotificationNotFound
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (r *PostgreSQLRepository) MarkAllSecurityAuditNotificationsRead(
	ctx context.Context,
	audience string,
) (int64, error) {
	audience = strings.TrimSpace(audience)
	if audience == "" {
		audience = "admin"
	}
	if audience != "admin" {
		return 0, ErrInvalidTransition
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE security_audit_notifications
SET status='read',
    read_at=COALESCE(read_at,NOW())
WHERE status='unread'
  AND audience=$1`, audience)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *PostgreSQLRepository) UpdateUserSecurityAuditNotificationStatus(
	ctx context.Context,
	userID, id int64,
	status string,
) (*SecurityAuditNotification, error) {
	status = strings.TrimSpace(status)
	if userID <= 0 || !validNotificationStatus(status) {
		return nil, ErrInvalidTransition
	}
	item, err := scanSecurityAuditNotification(r.db.QueryRowContext(ctx, `
UPDATE security_audit_notifications
SET status=$3,
    read_at=CASE WHEN $3='read' THEN COALESCE(read_at,NOW()) WHEN $3='unread' THEN NULL ELSE read_at END
WHERE id=$1
  AND audience='user'
  AND recipient_user_id=$2
RETURNING id,notification_id,action_id,decision_pk,audience,recipient_user_id,
          severity,title,body,status,read_at,created_at`,
		id, userID, status,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotificationNotFound
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (r *PostgreSQLRepository) MarkAllUserSecurityAuditNotificationsRead(
	ctx context.Context,
	userID int64,
) (int64, error) {
	if userID <= 0 {
		return 0, ErrNotificationNotFound
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE security_audit_notifications
SET status='read',
    read_at=COALESCE(read_at,NOW())
WHERE audience='user'
  AND recipient_user_id=$1
  AND status='unread'`, userID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type notificationScanner interface {
	Scan(dest ...any) error
}

func scanSecurityAuditNotification(scanner notificationScanner) (*SecurityAuditNotification, error) {
	item := &SecurityAuditNotification{}
	var recipientID sql.NullInt64
	var readAt sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.NotificationID, &item.ActionID, &item.DecisionPK,
		&item.Audience, &recipientID, &item.Severity, &item.Title, &item.Body,
		&item.Status, &readAt, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	if recipientID.Valid {
		item.RecipientUserID = &recipientID.Int64
	}
	item.ReadAt = nullableTime(readAt)
	return item, nil
}

func validNotificationStatus(status string) bool {
	return status == "read" || status == "dismissed" || status == "unread"
}
