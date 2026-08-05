package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountStatsImageInputPriceMigrationIsAdditive(t *testing.T) {
	content, err := FS.ReadFile("314_account_stats_image_input_price.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "alter table channel_account_stats_model_pricing")
	require.Contains(t, sql, "add column if not exists image_input_price")
}
