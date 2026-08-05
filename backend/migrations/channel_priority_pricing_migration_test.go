package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelPriorityPricingMigrationIsAdditiveAcrossBillingTables(t *testing.T) {
	content, err := FS.ReadFile("313_channel_priority_pricing.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, table := range []string{
		"channel_model_pricing",
		"channel_pricing_intervals",
		"channel_account_stats_model_pricing",
		"channel_account_stats_pricing_intervals",
	} {
		require.Contains(t, sql, "alter table "+table)
	}
	for _, column := range []string{
		"input_price_priority",
		"output_price_priority",
		"cache_write_price_priority",
		"cache_read_price_priority",
	} {
		require.Equal(t, 4, strings.Count(sql, "add column if not exists "+column))
	}
}
