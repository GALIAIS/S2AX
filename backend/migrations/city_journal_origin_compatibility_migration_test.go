package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityJournalOriginCompatibilityMigrationPreservesV15AutomaticExpiry(t *testing.T) {
	content, err := FS.ReadFile("270_city_journal_origin_compatibility.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"drop constraint if exists city_journal_origin_check",
		"add constraint city_journal_origin_check check",
		"journal_type = 'freight_fee'",
		"journal_type = 'reversal'",
		"reversal_of_journal_id is not null",
		"metadata->>'system_origin' = 'open_world_supply_chain.auto_expiry.v1'",
		"journal_type not in ('opening', 'reversal', 'freight_fee')",
	} {
		require.Contains(t, sql, required)
	}

	require.NotContains(t, sql, "system_origin is not null")
	require.NotContains(t, sql, "unsafe-eval")
}
