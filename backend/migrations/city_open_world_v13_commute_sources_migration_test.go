package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV13CommuteSourcesMigrationPinsVerifiedDualDirectionFoundation(t *testing.T) {
	content, err := FS.ReadFile("234_city_open_world_v13_commute_sources.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v13",
		"openworld_v12_to_v13",
		"create table if not exists city_open_world_commute_source_profiles",
		"create table if not exists city_open_world_commute_sources",
		"create table if not exists city_open_world_commute_cycle_metrics",
		"verified_facility_presence_od_v1",
		"facility_interior_or_surface_egress_v1",
		"npc.residence_to_work",
		"npc.work_to_residence",
		"city_open_world_commute_source_bootstrap_write_enabled",
		"guard_city_open_world_commute_source",
		"assert_city_open_world_commute_source_foundation",
		"sub2api-open-world-commute-source-catalog",
		"city_recovery_write_enabled",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "update city_open_world_npc_profiles set home_facility_id")
	require.NotContains(t, sql, "create table if not exists city_commute_live_routes")
}
