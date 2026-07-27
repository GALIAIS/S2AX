package securityaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

var notificationColumns = []string{
	"id", "notification_id", "action_id", "decision_pk", "audience", "recipient_user_id",
	"severity", "title", "body", "status", "read_at", "created_at",
}

func TestListUserSecurityAuditNotificationsScopesByRecipient(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)
	now := time.Date(2026, time.July, 24, 3, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)FROM security_audit_notifications n.*n\.audience='user'.*n\.recipient_user_id=\$1.*LIMIT \$2`).
		WithArgs(int64(42), 20).
		WillReturnRows(sqlmock.NewRows(notificationColumns).AddRow(
			int64(7), "ntf_safe", int64(11), int64(13), "user", int64(42),
			"medium", "请求安全提醒", "已脱敏的安全提醒", "unread", nil, now,
		))

	items, err := repo.ListUserSecurityAuditNotifications(context.Background(), 42, "", 20)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(42), *items[0].RecipientUserID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateUserSecurityAuditNotificationStatusRejectsOtherRecipient(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)

	mock.ExpectQuery(`(?s)UPDATE security_audit_notifications.*recipient_user_id=\$2.*RETURNING`).
		WithArgs(int64(7), int64(42), "read").
		WillReturnError(sql.ErrNoRows)

	_, err = repo.UpdateUserSecurityAuditNotificationStatus(context.Background(), 42, 7, "read")
	require.ErrorIs(t, err, ErrNotificationNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateAdminSecurityAuditNotificationStatusCannotMutateUserNotification(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)

	mock.ExpectQuery(`(?s)UPDATE security_audit_notifications.*id=\$1.*audience='admin'.*RETURNING`).
		WithArgs(int64(7), "read").
		WillReturnError(sql.ErrNoRows)

	_, err = repo.UpdateSecurityAuditNotificationStatus(context.Background(), 7, "read")
	require.ErrorIs(t, err, ErrNotificationNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMarkAllUserSecurityAuditNotificationsReadScopesByRecipient(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)

	mock.ExpectExec(`(?s)UPDATE security_audit_notifications.*audience='user'.*recipient_user_id=\$1.*status='unread'`).
		WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 3))

	count, err := repo.MarkAllUserSecurityAuditNotificationsRead(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, int64(3), count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMarkAllSecurityAuditNotificationsReadCanScopeByAudience(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)

	mock.ExpectExec(`(?s)UPDATE security_audit_notifications.*status='unread'.*audience=\$1`).
		WithArgs("admin").
		WillReturnResult(sqlmock.NewResult(0, 4))

	count, err := repo.MarkAllSecurityAuditNotificationsRead(context.Background(), "admin")
	require.NoError(t, err)
	require.Equal(t, int64(4), count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMarkAllSecurityAuditNotificationsReadRejectsUnknownAudience(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)

	_, err = repo.MarkAllSecurityAuditNotificationsRead(context.Background(), "external")
	require.ErrorIs(t, err, ErrInvalidTransition)
}

func TestMarkAllSecurityAuditNotificationsReadDefaultsToAdminAudience(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)

	mock.ExpectExec(`(?s)UPDATE security_audit_notifications.*status='unread'.*audience=\$1`).
		WithArgs("admin").
		WillReturnResult(sqlmock.NewResult(0, 2))

	count, err := repo.MarkAllSecurityAuditNotificationsRead(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMarkAllSecurityAuditNotificationsReadRejectsUserAudience(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)

	_, err = repo.MarkAllSecurityAuditNotificationsRead(context.Background(), "user")
	require.ErrorIs(t, err, ErrInvalidTransition)
}

func TestUserSecurityAuditNotificationViewOmitsInternalReferences(t *testing.T) {
	userID := int64(42)
	raw, err := json.Marshal((&SecurityAuditNotification{
		ID: 7, NotificationID: "ntf_safe", ActionID: 11, DecisionPK: 13,
		Audience: "user", RecipientUserID: &userID, Severity: "medium",
		Title: "请求安全提醒", Body: "已脱敏的安全提醒", Status: "unread",
	}).UserView())
	require.NoError(t, err)
	require.NotContains(t, string(raw), "action_id")
	require.NotContains(t, string(raw), "decision_pk")
	require.NotContains(t, string(raw), "recipient_user_id")
	require.NotContains(t, string(raw), "audience")
}
