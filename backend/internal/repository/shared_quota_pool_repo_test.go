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
	defer db.Close()

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
