package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountAllocationControlPlaneMigrationDefinesExclusiveAuditableLeases(t *testing.T) {
	content, err := FS.ReadFile("219_account_allocation_control_plane.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"create table if not exists account_allocation_policies",
		"create table if not exists account_allocation_assignments",
		"create table if not exists account_allocation_events",
		"account_allocation_policies_user_group_unique",
		"account_allocation_assignments_one_active_account",
		"where status = 'active'",
		"metadata jsonb not null default '{}'::jsonb",
		"account_id bigint not null references accounts(id) on delete restrict",
	} {
		require.Contains(t, sql, required)
	}
}
