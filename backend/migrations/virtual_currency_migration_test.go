package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVirtualCurrencyCoreMigrationDefinesLedgerBoundaries(t *testing.T) {
	content, err := FS.ReadFile("182_virtual_currency_core.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	for _, table := range []string{
		"virtual_currencies",
		"virtual_currency_group_policies",
		"virtual_currency_wallets",
		"virtual_currency_ledger_entries",
		"virtual_currency_holds",
	} {
		require.Contains(t, sql, "create table if not exists "+table)
	}
	require.Contains(t, sql, "unique index if not exists idx_virtual_currency_ledger_idempotency")
	require.Contains(t, sql, "virtual_currency_ledger_idempotency_key_check")
	require.Contains(t, sql, "request_fingerprint char(64) not null")
	require.Contains(t, sql, "virtual_currency_ledger_request_fingerprint_check")
	require.Contains(t, sql, "virtual_currency_ledger_delta_balance_check")
	require.Contains(t, sql, "virtual_currency_ledger_entry_type_check")
	require.Contains(t, sql, "virtual_currency_ledger_source_type_check")
	require.Contains(t, sql, "on delete restrict")
}
