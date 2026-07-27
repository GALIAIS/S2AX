package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityAuditRecordHashMigrationRemovesFalseExecutorContract(t *testing.T) {
	content, err := FS.ReadFile("295_security_audit_remove_unsupported_record_hash.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "jsonb_array_elements")
	require.Contains(t, sql, "record_hash")
	require.Contains(t, sql, "error_code='unsupported_action'")
	require.Contains(t, sql, "update security_audit_outbox")
}
