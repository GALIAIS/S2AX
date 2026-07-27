package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptAuditConfigVersionMigrationPinsAndProtectsHistory(t *testing.T) {
	raw, err := FS.ReadFile("302_prompt_audit_config_versions.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(raw))
	for _, fragment := range []string{
		"prompt_audit_config_versions",
		"config_version bigint primary key",
		"config_json     jsonb not null",
		"config_digest   varchar(64) not null",
		"references users(id) on delete set null",
		"before update or delete",
		"append-only",
	} {
		require.Contains(t, sql, fragment)
	}
}
