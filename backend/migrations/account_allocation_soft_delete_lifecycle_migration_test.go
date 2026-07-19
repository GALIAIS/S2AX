package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountAllocationSoftDeleteLifecycleReleasesLeasesAndDisablesPolicies(t *testing.T) {
	content, err := FS.ReadFile("221_account_allocation_soft_delete_lifecycle.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"account_id is not null",
		"release_account_allocation_leases_after_account_soft_delete",
		"account_allocation_release_after_account_soft_delete",
		"after update of deleted_at on accounts",
		"disable_account_allocation_policies_after_user_soft_delete",
		"account_allocation_disable_after_user_soft_delete",
		"after update of deleted_at on users",
		"disable_account_allocation_policies_after_group_soft_delete",
		"account_allocation_disable_after_group_soft_delete",
		"after update of deleted_at on groups",
		"release_reason = 'account_removed'",
		"release_reason = 'user_removed'",
		"release_reason = 'group_removed'",
		"status = 'disabled'",
		"'assignment_released'",
		"'policy_disabled'",
	} {
		require.Contains(t, sql, required)
	}
}
