package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV21EffectiveCapacityMigrationPinsAuditedFutureAdmissions(t *testing.T) {
	content, err := FS.ReadFile("243_city_open_world_v21_effective_capacity.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-openworld-v21",
		"openworld_v20_to_v21",
		"effective_infrastructure_capacity",
		"capacity_aware_route_admission",
		"create table if not exists city_open_world_effective_capacity_profiles",
		"create table if not exists city_open_world_effective_capacity_admissions",
		"v19_edge_corridor_mapping_v1",
		"v20_corridor_segment_ordinal_1_v1",
		"effective_infrastructure_capacity_v1",
		"next_tick_after_command_v1",
		"city_open_world_effective_capacity_bootstrap_write_enabled",
		"city_open_world_effective_capacity_write_enabled",
		"guard_city_open_world_effective_capacity_admission",
		"assert_city_open_world_effective_capacity_foundation",
		"next_tick_effective_capacity_v1",
		"city-openworld-v20','city-openworld-v21",
	} {
		require.Contains(t, sql, required)
	}

	require.Contains(t, sql, "schedule_fact.sequence")
	require.Contains(t, sql, "transition.transition_sequence < schedule_fact.sequence")
	require.Contains(t, sql, "city_recovery_write_enabled")
	require.NotContains(t, sql, "unsafe-eval")
}
