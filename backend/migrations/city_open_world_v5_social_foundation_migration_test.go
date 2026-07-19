package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV5SocialFoundationKeepsRuntimeSeparateAndGuarded(t *testing.T) {
	content, err := FS.ReadFile("218_city_open_world_v5_social_foundation.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v5",
		"create table if not exists city_open_world_scenario_bindings",
		"create table if not exists city_open_world_facilities",
		"create table if not exists city_open_world_npc_profiles",
		"create table if not exists city_open_world_actor_navigation_intents",
		"system.%",
		"guard_city_open_world_runtime_projection",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "city_spatial_profiles")
	require.NotContains(t, sql, "city_overmap_tiles")
}
