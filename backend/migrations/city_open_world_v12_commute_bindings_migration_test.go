package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV12CommuteBindingsMigrationPinsCapacityLimitedFoundation(t *testing.T) {
	content, err := FS.ReadFile("233_city_open_world_v12_commute_bindings.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v12",
		"openworld_v11_to_v12",
		"create table if not exists city_open_world_commute_profiles",
		"create table if not exists city_open_world_commute_bindings",
		"deterministic_capacity_residence_assignment_v1",
		"npc.residence_employment",
		"city_open_world_commute_bootstrap_write_enabled",
		"guard_city_open_world_commute_profile",
		"guard_city_open_world_commute_binding",
		"assert_city_open_world_commute_foundation",
		"sub2api-open-world-commute-catalog",
		"city_recovery_write_enabled",
		"residence capacity is exceeded",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "update city_open_world_npc_profiles set home_facility_id")
	require.NotContains(t, sql, "create table if not exists city_commute_live_routes")
}
