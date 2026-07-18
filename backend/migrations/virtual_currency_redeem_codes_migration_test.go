package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVirtualCurrencyRedeemCodesMigrationKeepsLegacyCodesAndRewardsAtomic(t *testing.T) {
	content, err := FS.ReadFile("185_virtual_currency_redeem_codes.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, column := range []string{"currency_id", "currency_amount_units", "currency_group_id"} {
		require.Contains(t, sql, "add column if not exists "+column)
	}
	require.Contains(t, sql, "redeem_codes_virtual_currency_fields_check")
	require.Contains(t, sql, "type = 'virtual_currency'")
	require.Contains(t, sql, "type <> 'virtual_currency'")
	require.Contains(t, sql, "on delete restrict")
	require.Contains(t, sql, "idx_redeem_codes_currency_id")
}
