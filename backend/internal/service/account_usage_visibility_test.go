package service

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestCreateAccountUsageVisibilityGrantForExclusiveGroup(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	createdAt := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT is_exclusive")).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"is_exclusive"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO account_usage_visibility_grants")).
		WithArgs(AccountUsageVisibilityGrantExclusiveGroup, int64(9), nil, nil, int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(31)))
	mock.ExpectQuery(`(?s)FROM account_usage_visibility_grants visibility.*WHERE visibility\.id = \$1`).
		WithArgs(int64(31)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "grant_scope", "group_id", "group_name", "user_id", "user_email", "username",
			"account_id", "account_name", "platform", "account_type", "created_by", "created_at",
		}).AddRow(
			int64(31), "exclusive_group", int64(9), "VIP Pool", nil, "", "", nil, "", "", "", int64(1), createdAt,
		))
	mock.ExpectCommit()

	svc := NewAccountAllocationService(db, nil)
	grant, err := svc.CreateAccountUsageVisibilityGrant(context.Background(), AccountUsageVisibilityGrantInput{
		Scope:       AccountUsageVisibilityGrantExclusiveGroup,
		GroupID:     9,
		ActorUserID: 1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(31), grant.ID)
	require.Equal(t, AccountUsageVisibilityGrantExclusiveGroup, grant.Scope)
	require.Equal(t, "VIP Pool", grant.GroupName)
	require.Nil(t, grant.UserID)
	require.Nil(t, grant.AccountID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestValidateAccountUsageVisibilityGrantInputRequiresDirectTargets(t *testing.T) {
	err := validateAccountUsageVisibilityGrantInput(AccountUsageVisibilityGrantInput{
		Scope:       AccountUsageVisibilityGrantUserAccount,
		GroupID:     9,
		ActorUserID: 1,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "user_account grants require")
}

func TestCreateAccountUsageVisibilityGrantForUserAccount(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	createdAt := time.Date(2026, 7, 28, 11, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM users")).
		WithArgs(int64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM groups")).
		WithArgs(int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`(?s)SELECT CASE.*FROM users u.*JOIN groups g`).
		WithArgs(
			int64(8),
			int64(12),
			AccountAllocationAccessUserUnavailable,
			AccountAllocationAccessGroupUnavailable,
			AccountAllocationAccessSubscriptionRequired,
			AccountAllocationAccessGroupPermission,
			AccountAllocationAccessReady,
		).
		WillReturnRows(sqlmock.NewRows([]string{"access_status"}).AddRow(AccountAllocationAccessReady))
	mock.ExpectQuery(`(?s)SELECT EXISTS\(.*FROM accounts a.*JOIN account_groups ag`).
		WithArgs(int64(44), int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`(?s)SELECT EXISTS\(.*FROM account_allocation_assignments aa`).
		WithArgs(int64(44)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO account_usage_visibility_grants")).
		WithArgs(AccountUsageVisibilityGrantUserAccount, int64(12), int64(8), int64(44), int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(32)))
	mock.ExpectQuery(`(?s)FROM account_usage_visibility_grants visibility.*WHERE visibility\.id = \$1`).
		WithArgs(int64(32)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "grant_scope", "group_id", "group_name", "user_id", "user_email", "username",
			"account_id", "account_name", "platform", "account_type", "created_by", "created_at",
		}).AddRow(
			int64(32), "user_account", int64(12), "Open pool", int64(8), "member@example.com", "member",
			int64(44), "Pool account", "openai", "oauth", int64(1), createdAt,
		))
	mock.ExpectCommit()

	svc := NewAccountAllocationService(db, nil)
	grant, err := svc.CreateAccountUsageVisibilityGrant(context.Background(), AccountUsageVisibilityGrantInput{
		Scope:       AccountUsageVisibilityGrantUserAccount,
		GroupID:     12,
		UserID:      8,
		AccountID:   44,
		ActorUserID: 1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(32), grant.ID)
	require.Equal(t, AccountUsageVisibilityGrantUserAccount, grant.Scope)
	require.Equal(t, int64(8), *grant.UserID)
	require.Equal(t, int64(44), *grant.AccountID)
	require.NoError(t, mock.ExpectationsWereMet())
}
