package securityaudit

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func actionRow(status string, requestedBy any) *sqlmock.Rows {
	now := time.Date(2026, time.July, 24, 6, 0, 0, 0, time.UTC)
	return sqlmock.NewRows(enforcementActionColumns).AddRow(
		int64(31), "act_31", int64(17), "notify_admin", "request", int64(0),
		status, "idem-31", int64(1), 0, 5,
		"", nil, now, []byte(`{}`), []byte(`{}`), "", "", requestedBy,
		now, nil, nil, nil, now,
	)
}

func TestRetryActionRollsBackWhenOutboxIsMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &PostgreSQLRepository{db: db}

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)UPDATE security_audit_actions.*status='retry'.*RETURNING`).
		WithArgs(int64(31), int64(9)).
		WillReturnRows(actionRow("retry", int64(9)))
	mock.ExpectExec(`(?s)UPDATE security_audit_outbox.*status='retry'.*WHERE action_id=\$1`).
		WithArgs(int64(31)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	_, err = repo.RetryAction(context.Background(), 31, 9)
	require.ErrorContains(t, err, "expected one affected row")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCancelActionCommitsActionAndOutboxTogether(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &PostgreSQLRepository{db: db}

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)UPDATE security_audit_actions.*status='cancelled'.*RETURNING`).
		WithArgs(int64(31), int64(9)).
		WillReturnRows(actionRow("cancelled", int64(9)))
	mock.ExpectExec(`(?s)UPDATE security_audit_outbox.*status='discarded'.*WHERE action_id=\$1`).
		WithArgs(int64(31)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	item, err := repo.CancelAction(context.Background(), 31, 9)
	require.NoError(t, err)
	require.Equal(t, ActionStatusCancelled, item.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMarkClaimedActionSucceededRejectsMissingOutboxLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE security_audit_actions.*status='succeeded'.*lease_owner=\$2`).
		WithArgs(int64(31), "worker-1", 2, []byte(`{}`), []byte(`{"ok":true}`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE security_audit_outbox.*status='published'.*lease_owner=\$2`).
		WithArgs(int64(31), "worker-1").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{})
	require.NoError(t, err)
	action := &EnforcementAction{ID: 31, Attempts: 2}
	err = markClaimedActionSucceeded(
		context.Background(),
		tx,
		action,
		"worker-1",
		[]byte(`{}`),
		[]byte(`{"ok":true}`),
	)
	require.ErrorContains(t, err, "expected one affected row")
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
