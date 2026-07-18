package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRateMultiplierPrecisionMigrationCoversEveryPersistedMultiplier(t *testing.T) {
	content, err := FS.ReadFile("178_expand_rate_multiplier_precision.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	for _, table := range []string{
		"groups",
		"accounts",
		"usage_logs",
		"user_group_rate_multipliers",
		"batch_image_jobs",
	} {
		require.Contains(t, sql, "alter table "+table)
	}

	require.Equal(t, 14, strings.Count(sql, "decimal(18,8)"))
	require.Contains(t, sql, "using rate_multiplier::numeric")
}
