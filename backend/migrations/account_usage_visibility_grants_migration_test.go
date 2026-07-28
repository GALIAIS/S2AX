package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountUsageVisibilityGrantsMigration(t *testing.T) {
	content, err := FS.ReadFile("306_account_usage_visibility_grants.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"create table if not exists account_usage_visibility_grants",
		"grant_scope in ('exclusive_group', 'user_account')",
		"chk_account_usage_visibility_grant_shape",
		"on delete cascade",
		"uq_account_usage_visibility_exclusive_group",
		"uq_account_usage_visibility_user_account",
	} {
		require.Contains(t, sql, required)
	}
}
