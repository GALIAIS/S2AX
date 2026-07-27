package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityAuditPolicyTransitionHistoryMigration(t *testing.T) {
	content, err := FS.ReadFile("296_security_audit_policy_transition_history.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "create table if not exists security_audit_policy_transitions")
	require.Contains(t, sql, "references security_audit_policy_versions(id) on delete restrict")
	require.Contains(t, sql, "actor_id")
	require.Contains(t, sql, "from_status <> to_status")
	require.Contains(t, sql, "idx_security_audit_policy_transition_version")
}
