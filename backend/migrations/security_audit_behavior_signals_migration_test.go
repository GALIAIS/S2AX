package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityAuditBehaviorSignalsMigrationDefinesBoundedAuditableProjection(t *testing.T) {
	content, err := FS.ReadFile("292_security_audit_behavior_signals.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"create table if not exists security_audit_signal_windows",
		"unique (bucket_start, bucket_seconds, subject_type, subject_id)",
		"create table if not exists security_audit_signal_evaluations",
		"unique (anchor_window_id, policy_key, policy_version, rule_id)",
		"create table if not exists security_audit_signal_watermark",
		"last_evaluated_window_id",
		"create table if not exists security_audit_notifications",
		"unique (action_id)",
		"check (subject_type in ('user', 'api_key', 'group'))",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "prompt_text")
	require.NotContains(t, sql, "request_body")
	require.NotContains(t, sql, "client_ip inet")
}
