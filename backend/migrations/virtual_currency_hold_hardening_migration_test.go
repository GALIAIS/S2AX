package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVirtualCurrencyHoldHardeningMigrationAddsStateAndIndexes(t *testing.T) {
	content, err := FS.ReadFile("183_virtual_currency_hold_hardening.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "virtual_currency_hold_status_check")
	require.Contains(t, sql, "virtual_currency_hold_source_type_check")
	require.Contains(t, sql, "virtual_currency_hold_amount_limit_check")
	require.Contains(t, sql, "idx_virtual_currency_holds_settlement")
}
