package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountAllocationUserUsageLookupIndexMigration(t *testing.T) {
	content, err := FS.ReadFile("222_account_allocation_user_usage_lookup_index_notx.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_user_account_created_at")
	require.Contains(t, sql, "ON usage_logs (user_id, account_id, created_at)")
}
