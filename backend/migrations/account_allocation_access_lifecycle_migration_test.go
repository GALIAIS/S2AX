package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountAllocationAccessLifecycleMigration(t *testing.T) {
	content, err := FS.ReadFile("288_account_allocation_access_lifecycle.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"after delete on user_allowed_groups",
		"after update of status, expires_at, deleted_at on user_subscriptions",
		"after delete on user_subscriptions",
		"after update of status on users",
		"after update of status on groups",
		"target_group_access_unavailable",
		"target_user_unavailable",
		"target_group_unavailable",
		"'assignment_released'",
	} {
		require.Contains(t, sql, required)
	}
}
