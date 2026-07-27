package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityAuditLiveShadowMigration(t *testing.T) {
	content, err := FS.ReadFile("297_security_audit_live_shadow_evaluation.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "create table if not exists security_audit_shadow_evaluations")
	require.Contains(t, sql, "unique (decision_pk, policy_version_id)")
	require.Contains(t, sql, "references security_audit_policy_versions(id) on delete restrict")
	require.Contains(t, sql, "create table if not exists security_audit_shadow_watermark")
	require.Contains(t, sql, "request_action_changed")
	require.Contains(t, sql, "actions_changed")
}
