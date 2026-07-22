package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV9MobilityMigrationPinsAggregateCapacityContract(t *testing.T) {
	content, err := FS.ReadFile("229_city_open_world_v9_mobility.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v9",
		"openworld_v8_to_v9",
		"create table if not exists city_open_world_mobility_profiles",
		"create table if not exists city_open_world_mobility_modes",
		"create table if not exists city_open_world_mobility_hubs",
		"create table if not exists city_open_world_mobility_edges",
		"create table if not exists city_open_world_mobility_demands",
		"create table if not exists city_open_world_mobility_routes",
		"create table if not exists city_open_world_mobility_allocations",
		"create table if not exists city_open_world_mobility_actor_metrics",
		"next_tick_capacity_v1",
		"facility-hub-zone-graph-v1",
		"city_open_world_mobility_bootstrap_write_enabled",
		"guard_city_open_world_mobility_demand",
		"guard_city_open_world_mobility_route",
		"assert_city_open_world_mobility_foundation",
		"sub2api-open-world-mobility-catalog",
		"city_recovery_write_enabled",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "create table if not exists city_traffic_lanes")
	require.NotContains(t, sql, "update city_open_world_actor_locations set")
}
