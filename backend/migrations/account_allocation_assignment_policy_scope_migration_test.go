package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountAllocationAssignmentPolicyScopeMigration(t *testing.T) {
	content, err := FS.ReadFile("223_account_allocation_assignment_policy_scope.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD CONSTRAINT account_allocation_policies_id_user_group_unique UNIQUE (id, user_id, group_id)")
	require.Contains(t, sql, "ADD CONSTRAINT account_allocation_assignments_policy_scope_fkey FOREIGN KEY (policy_id, user_id, group_id)")
	require.Contains(t, sql, "REFERENCES account_allocation_policies (id, user_id, group_id) ON DELETE RESTRICT")
}
