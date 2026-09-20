package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestSharedQuotaPoolRepositoryUsesAccountScopeAcrossGroups 锁定账号范围查询的
// SQL 形状，防止后续维护时重新加入目标分组或订阅过滤条件。
func TestSharedQuotaPoolRepositoryUsesAccountScopeAcrossGroups(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	start := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	accountID := int64(50680)
	mock.ExpectQuery(regexp.QuoteMeta(`
			SELECT user_id, COALESCE(SUM(total_cost), 0)
			FROM usage_logs
			WHERE account_id = $1
			  AND created_at >= $2
			  AND created_at < $3
			GROUP BY user_id`)).
		WithArgs(accountID, start, end).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "sum"}).
			AddRow(int64(5), 174.69).
			AddRow(int64(1), 142.81))

	repo := &sharedQuotaPoolRepository{db: db}
	total, byUser, err := repo.GetUsage(context.Background(), service.SharedQuotaUsageScope{
		GroupID:   31,
		AccountID: &accountID,
	}, start, end)
	require.NoError(t, err)
	require.InDelta(t, 317.50, total, 0.000001)
	require.InDelta(t, 174.69, byUser[5], 0.000001)
	require.InDelta(t, 142.81, byUser[1], 0.000001)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestSharedQuotaPoolRepositoryUsesMemberSubscriptionWindows 验证账号范围查询按
// 每个用户的订阅窗口过滤，避免把统一的官方账号起点当成成员额度起点。
func TestSharedQuotaPoolRepositoryUsesMemberSubscriptionWindows(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	end := time.Date(2026, 9, 26, 23, 15, 0, 0, time.UTC)
	userOneStart := end.Add(-7 * 24 * time.Hour)
	userTwoStart := end.Add(-24 * time.Hour)
	accountID := int64(50680)
	mock.ExpectQuery(regexp.QuoteMeta(`
			SELECT l.user_id, COALESCE(SUM(l.total_cost), 0)
			FROM usage_logs l
			JOIN (VALUES ($3::bigint,$4::timestamptz),($5::bigint,$6::timestamptz)) AS member_windows(user_id, window_start)
			  ON member_windows.user_id = l.user_id
			WHERE l.account_id = $1
			  AND l.created_at >= member_windows.window_start
			  AND l.created_at < $2
			GROUP BY l.user_id`)).
		WithArgs(accountID, end, int64(1), userOneStart, int64(5), userTwoStart).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "sum"}).
			AddRow(int64(1), 245.10).
			AddRow(int64(5), 173.40))

	repo := &sharedQuotaPoolRepository{db: db}
	total, byUser, err := repo.GetUsage(context.Background(), service.SharedQuotaUsageScope{AccountID: &accountID}, end.Add(-7*24*time.Hour), end, []service.SharedQuotaMemberUsageWindow{
		{UserID: 1, WindowStart: userOneStart},
		{UserID: 5, WindowStart: userTwoStart},
	})
	require.NoError(t, err)
	require.InDelta(t, 418.50, total, 0.000001)
	require.InDelta(t, 245.10, byUser[1], 0.000001)
	require.InDelta(t, 173.40, byUser[5], 0.000001)
	require.NoError(t, mock.ExpectationsWereMet())
}
