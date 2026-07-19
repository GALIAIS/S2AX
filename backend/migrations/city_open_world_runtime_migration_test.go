package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldRuntimeMigrationKeepsV4RuntimeSeparateFromF7SpatialState(t *testing.T) {
	content, err := FS.ReadFile("216_city_open_world_runtime.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v4",
		"create table if not exists city_open_world_runtime_profiles",
		"create table if not exists city_open_world_runtime_definitions",
		"create table if not exists city_open_world_actors",
		"create table if not exists city_open_world_actor_locations",
		"create table if not exists city_open_world_portal_states",
		"create table if not exists city_open_world_runtime_facts",
		"create table if not exists city_open_world_runtime_effects",
		"create table if not exists city_open_world_rule_cases",
		"city_open_world_runtime_bootstrap_write_enabled",
		"city_open_world_runtime_fact_write_enabled",
		"city_open_world_actor_locations_cell_occupancy_unique",
		"guard_city_open_world_runtime_fact_immutable",
		"guard_city_open_world_runtime_projection",
		"city_open_world_portals",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "city_spatial_profiles")
	require.NotContains(t, sql, "city_overmap_tiles")
}
