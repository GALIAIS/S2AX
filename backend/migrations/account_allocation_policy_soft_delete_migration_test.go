package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountAllocationPolicySoftDeleteMigrationKeepsAuditAndAllowsReplacement(t *testing.T) {
	content, err := FS.ReadFile("225_account_allocation_policy_soft_delete.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"add column if not exists deleted_at",
		"drop constraint if exists account_allocation_policies_user_group_unique",
		"account_allocation_policies_user_group_live_unique",
		"where deleted_at is null",
		"where status = 'active' and deleted_at is null",
	} {
		require.Contains(t, sql, required)
	}
}
