package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldInfrastructureEffectiveCapacityCompatibilityMigrationIsNarrow(t *testing.T) {
	content, err := FS.ReadFile("272_city_open_world_infrastructure_effective_capacity_compatibility.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"assert_city_open_world_infrastructure_foundation(bigint)",
		"next_tick_effective_capacity_v1",
		"v9_scheduler_effective_from_tick",
		"city-openworld-v21','city-openworld-v22','city-openworld-v23','city-openworld-v24'",
		"create or replace function guard_city_open_world_infrastructure_transition()",
		"open-world v20 infrastructure transitions are append-only audited facts",
		"open-world v20 infrastructure transition must match its runtime fact cursor",
	} {
		require.Contains(t, sql, required)
	}

	require.NotContains(t, sql, "unsafe-eval")
}
