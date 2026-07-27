package securityaudit

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

var enforcementActionColumns = []string{
	"id", "action_id", "decision_pk", "action_type", "subject_type", "subject_id",
	"status", "idempotency_key", "policy_action_version", "attempts", "max_attempts",
	"lease_owner", "lease_expires_at", "next_attempt_at", "before_snapshot",
	"after_snapshot", "error_code", "error_message", "requested_by", "created_at",
	"processed_at", "cancelled_at", "reverted_at", "updated_at",
}

func TestFalsePositiveRevertRestoresSucceededPauseAndLegacyOutcome(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	now := time.Date(2026, time.July, 24, 4, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM security_audit_actions a.*a\.decision_pk=\$1.*a\.status='succeeded'.*FOR UPDATE`).
		WithArgs(int64(17)).
		WillReturnRows(sqlmock.NewRows(enforcementActionColumns).AddRow(
			int64(31), "act_pause", int64(17), "pause_user", "user", int64(42),
			"succeeded", "idem", int64(1), 1, 5,
			"", nil, now, []byte(`{"status":"active"}`),
			[]byte(`{"status":"disabled","changed":true}`), "", "", nil, now,
			now, nil, nil, now,
		))
	mock.ExpectQuery(`SELECT status FROM users WHERE id=\$1 FOR UPDATE`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("disabled"))
	mock.ExpectExec(`UPDATE users SET status=\$2,updated_at=NOW\(\) WHERE id=\$1 AND status='disabled'`).
		WithArgs(int64(42), "active").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE content_moderation_logs.*SET auto_banned=FALSE.*WHERE id=.*security_audit_decisions.*id=\$1`).
		WithArgs(int64(17)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE security_audit_actions.*SET status='reverted'.*WHERE id=\$1 AND status='succeeded'`).
		WithArgs(int64(31), int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, err := db.Begin()
	require.NoError(t, err)
	count, err := revertSucceededPauseActionsForDecision(context.Background(), tx, 17, 9)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFalsePositiveRevertRefusesToOverwriteLaterManualStatusChange(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	now := time.Date(2026, time.July, 24, 4, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM security_audit_actions a.*a\.decision_pk=\$1.*a\.status='succeeded'.*FOR UPDATE`).
		WithArgs(int64(17)).
		WillReturnRows(sqlmock.NewRows(enforcementActionColumns).AddRow(
			int64(31), "act_pause", int64(17), "pause_user", "user", int64(42),
			"succeeded", "idem", int64(1), 1, 5,
			"", nil, now, []byte(`{"status":"active"}`),
			[]byte(`{"status":"disabled","changed":true}`), "", "", nil, now,
			now, nil, nil, now,
		))
	mock.ExpectQuery(`SELECT status FROM users WHERE id=\$1 FOR UPDATE`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	mock.ExpectRollback()

	tx, err := db.Begin()
	require.NoError(t, err)
	_, err = revertSucceededPauseActionsForDecision(context.Background(), tx, 17, 9)
	require.ErrorContains(t, err, "subject status changed after enforcement")
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
