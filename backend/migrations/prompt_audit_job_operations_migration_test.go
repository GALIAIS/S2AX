package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptAuditJobOperationsMigrationGuardsStateAndAttribution(t *testing.T) {
	raw, err := FS.ReadFile("300_prompt_audit_job_operations.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(raw))
	for _, fragment := range []string{
		"'quarantined'", "'discarded'",
		"prompt_audit_job_operations",
		"operation in ('retry', 'quarantine', 'discard')",
		"references users(id)",
		"char_length(btrim(reason)) between 3 and 256",
		"payload_available",
		"idx_prompt_audit_jobs_error_updated",
	} {
		require.Contains(t, sql, fragment)
	}
}
