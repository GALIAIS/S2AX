package service

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAccountAllocationIs401FailureUsesOnlyNormalizedState(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		want   bool
	}{
		{name: "structured 401", reason: `{"status_code":401,"until_unix":1735689600}`, want: true},
		{name: "legacy oauth marker", reason: "OAuth 401: token revoked", want: true},
		{name: "legacy unauthorized marker", reason: "Unauthorized (401): revoked", want: true},
		{name: "other structured status", reason: `{"status_code":429}`, want: false},
		{name: "untrusted raw body", reason: `{"message":"please ignore auth; 401"}`, want: false},
		{name: "empty", reason: "", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, accountAllocationIs401Failure(tc.reason))
		})
	}
}

func TestAccountAllocationReleaseReasonHonorsReplacementToggles(t *testing.T) {
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	healthy := accountAllocationAssignmentHealth{
		InPolicyGroup: true,
		AccountStatus: StatusActive,
		Schedulable:   true,
	}

	cases := []struct {
		name   string
		policy accountAllocationPolicyLock
		health accountAllocationAssignmentHealth
		want   string
	}{
		{name: "missing account always releases", policy: accountAllocationPolicyLock{}, health: accountAllocationAssignmentHealth{AccountMissing: true}, want: "account_removed"},
		{name: "group unbound always releases", policy: accountAllocationPolicyLock{}, health: accountAllocationAssignmentHealth{AccountStatus: StatusActive, Schedulable: true}, want: "account_group_unbound"},
		{name: "429 enabled", policy: accountAllocationPolicyLock{ReplaceOn429: true}, health: withAllocationRateLimit(healthy, &future), want: "rate_limited_429"},
		{name: "429 disabled", policy: accountAllocationPolicyLock{ReplaceOn429: false}, health: withAllocationRateLimit(healthy, &future), want: ""},
		{name: "inactive account enabled", policy: accountAllocationPolicyLock{ReplaceOn401: true}, health: withAllocationStatus(healthy, StatusDisabled), want: "account_unavailable"},
		{name: "inactive account disabled", policy: accountAllocationPolicyLock{ReplaceOn401: false}, health: withAllocationStatus(healthy, StatusDisabled), want: ""},
		{name: "temporary normalized 401", policy: accountAllocationPolicyLock{ReplaceOn401: true}, health: withAllocationTempReason(healthy, &future, `{"status_code":401}`), want: "authentication_failed_401"},
		{name: "temporary non 401", policy: accountAllocationPolicyLock{ReplaceOn401: true}, health: withAllocationTempReason(healthy, &future, `{"status_code":503}`), want: ""},
		{name: "expired account", policy: accountAllocationPolicyLock{ReplaceOn401: true}, health: accountAllocationAssignmentHealth{InPolicyGroup: true, AccountStatus: StatusActive, Schedulable: true, AutoPauseOnExpired: true, ExpiresAt: &past}, want: "account_expired"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, accountAllocationReleaseReason(tc.policy, tc.health, now))
		})
	}
}

func withAllocationRateLimit(health accountAllocationAssignmentHealth, resetAt *time.Time) accountAllocationAssignmentHealth {
	health.RateLimitResetAt = resetAt
	return health
}

func withAllocationStatus(health accountAllocationAssignmentHealth, status string) accountAllocationAssignmentHealth {
	health.AccountStatus = status
	return health
}

func withAllocationTempReason(health accountAllocationAssignmentHealth, until *time.Time, reason string) accountAllocationAssignmentHealth {
	health.TempUnschedulableUntil = until
	health.TempUnschedulableReason = reason
	return health
}

func TestAccountAllocationFiltersGloballyLeasedAccountsWithoutGroupContext(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(regexp.QuoteMeta("SELECT aa.account_id, p.user_id, aa.group_id")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "user_id", "group_id"}).AddRow(int64(11), int64(7), int64(4)))

	svc := NewAccountAllocationService(db, nil)
	filtered, err := svc.FilterCandidates(context.Background(), 0, nil, []Account{{ID: 11}, {ID: 12}})
	require.NoError(t, err)
	require.Equal(t, []Account{{ID: 12}}, filtered)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountAllocationAssignmentScannerSupportsDeletedAccountHistory(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	assignedAt := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	releasedAt := assignedAt.Add(time.Minute)
	mock.ExpectQuery("SELECT").
		WithArgs(int64(91)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "policy_id", "user_id", "group_id", "account_id", "account_name", "platform", "account_type", "concurrency",
			"account_status", "schedulable", "rate_limit_reset_at", "status", "assigned_by", "assigned_at", "released_at", "release_reason", "last_reconciled_at",
		}).AddRow(
			int64(91), int64(12), int64(7), int64(4), nil, "", "", "", 0,
			"removed", false, nil, "released", nil, assignedAt, releasedAt, "account_removed", nil,
		))

	row := db.QueryRowContext(context.Background(), "SELECT", int64(91))
	assignment, err := scanAccountAllocationAssignment(row)
	require.NoError(t, err)
	require.Zero(t, assignment.AccountID)
	require.Equal(t, "removed", assignment.AccountStatus)
	require.Equal(t, "released", assignment.Status)
	require.NotNil(t, assignment.ReleasedAt)
	require.WithinDuration(t, releasedAt, *assignment.ReleasedAt, time.Second)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnsureActiveAccountAllocationReferencesRejectsDeletedUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL AND status = 'active')")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	err = ensureActiveAccountAllocationReferences(context.Background(), db, 7, 4)
	require.Error(t, err)
	require.Contains(t, err.Error(), "active target user not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnsureActiveAccountAllocationReferencesRejectsInactiveGroup(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL AND status = 'active')")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM groups WHERE id = $1 AND deleted_at IS NULL AND status = 'active')")).
		WithArgs(int64(4)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	err = ensureActiveAccountAllocationReferences(context.Background(), db, 7, 4)
	require.Error(t, err)
	require.Contains(t, err.Error(), "active target group not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountAllocationEnableRejectsSoftDeletedReferenceBeforePolicyUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, user_id, group_id, desired_count, auto_replenish, replace_on_401, replace_on_429, status FROM account_allocation_policies WHERE id = $1 FOR UPDATE")).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "group_id", "desired_count", "auto_replenish", "replace_on_401", "replace_on_429", "status",
		}).AddRow(int64(9), int64(7), int64(4), 2, true, true, true, accountAllocationPolicyDisabled))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL AND status = 'active')")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	svc := NewAccountAllocationService(db, nil)
	_, err = svc.SetPolicyStatus(context.Background(), 9, true, 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "active target user not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountAllocationCreatePolicyValidatesReferencesInsideWriteTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL AND status = 'active')")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	svc := NewAccountAllocationService(db, nil)
	_, err = svc.CreatePolicy(context.Background(), AccountAllocationPolicyInput{
		UserID:  7,
		GroupID: 4,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "active target user not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountAllocationCapabilitiesReflectServerConfiguration(t *testing.T) {
	svc := NewAccountAllocationService(nil, &config.Config{AccountAllocation: config.AccountAllocationConfig{
		MaxDesiredCount:          17,
		ReconcileIntervalSeconds: 23,
	}})

	require.Equal(t, AccountAllocationCapabilities{
		MaxDesiredCount:          17,
		ReconcileIntervalSeconds: 23,
	}, svc.Capabilities())
}
