package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityAuditActionWhitelistMigrationMatchesRealExecutors(t *testing.T) {
	content, err := FS.ReadFile("298_security_audit_enforce_action_whitelist.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, action := range []string{
		"pause_api_key", "pause_user", "notify_user", "notify_admin", "open_case",
	} {
		require.Contains(t, sql, "'"+action+"'")
	}
	for _, unsupported := range []string{
		"record_hash", "quarantine_session", "throttle_api_key",
		"release_session", "resume_api_key", "resume_user",
	} {
		require.NotContains(t, sql, "'"+unsupported+"'")
	}
	require.Contains(t, sql, "error_code='unsupported_action'")
	require.Contains(t, sql, "update security_audit_outbox")
	require.Contains(t, sql, "drop constraint if exists chk_security_audit_action_type")
	require.Contains(t, sql, ")) not valid")
}
