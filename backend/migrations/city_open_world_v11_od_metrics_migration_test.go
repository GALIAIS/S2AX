package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV11ODMetricsMigrationPinsVersionedDemandContract(t *testing.T) {
	content, err := FS.ReadFile("232_city_open_world_v11_od_metrics.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v11",
		"openworld_v10_to_v11",
		"create table if not exists city_open_world_mobility_od_profiles",
		"create table if not exists city_open_world_mobility_od_sources",
		"create table if not exists city_open_world_mobility_od_cycle_metrics",
		"versioned_source_od_adapter_v1",
		"next_cycle_mobility_metrics_v1",
		"npc.assigned_facility_visit",
		"city_open_world_mobility_od_bootstrap_write_enabled",
		"city_open_world_mobility_od_write_enabled",
		"guard_city_open_world_mobility_od_source",
		"assert_city_open_world_mobility_od_foundation",
		"sub2api-open-world-mobility-od-catalog",
		"city_recovery_write_enabled",
		"system.mobility.od.generated",
		"system.mobility.od.cycle.closed",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "create table if not exists city_traffic_lanes")
	require.NotContains(t, sql, "update city_open_world_actor_locations set")
}
