package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountAllocationAccountDeletionRetainsReleasedLeaseHistory(t *testing.T) {
	content, err := FS.ReadFile("220_account_allocation_account_deletion_history.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"alter column account_id drop not null",
		"foreign key (account_id) references accounts(id) on delete set null",
		"create or replace function release_account_allocation_leases_before_account_delete",
		"create trigger account_allocation_release_before_account_delete",
		"before delete on accounts",
		"status = 'released'",
		"release_reason = 'account_removed'",
		"'assignment_released'",
	} {
		require.Contains(t, sql, required)
	}
}
