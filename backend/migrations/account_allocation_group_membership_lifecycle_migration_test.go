package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountAllocationGroupMembershipLifecycleMigration(t *testing.T) {
	content, err := FS.ReadFile("224_account_allocation_group_membership_lifecycle.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"release_account_allocation_leases_after_account_group_delete",
		"after delete on account_groups",
		"account_allocation_release_after_account_group_delete",
		"release_reason = 'account_group_unbound'",
		"'assignment_released'",
	} {
		require.Contains(t, sql, required)
	}
}
