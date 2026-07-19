package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityActorNavigationMigrationExtendsCanonicalGuardsWithoutMutableProjection(t *testing.T) {
	content, err := FS.ReadFile("204_city_actor_navigation.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"migration_204_replace_function",
		"world_runtime_bootstrap_write_enabled(bigint)",
		"guard_world_runtime_fact_insert()",
		"assert_world_runtime_foundation(bigint)",
		"guard_city_enterprise_location_fact_insert()",
		"assert_city_enterprise_location_foundation(bigint)",
		"guard_city_development_fact_insert()",
		"assert_city_development_foundation(bigint)",
		"assert_city_land_foundation(bigint)",
		"guard_city_spatial_mutation_insert()",
		"assert_city_spatial_foundation(bigint)",
		"assert_world_actor_spatial_control_foundation(bigint)",
		"city-f7-v7",
		"actor_navigation",
		"f7_v6_to_f7_v7",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "city-f7-v6', 'city-f7-v7")
	require.Contains(t, sql, "then '1.1.0'")
	require.NotContains(t, sql, "create table if not exists city_navigation")
	require.NotContains(t, sql, "language plpython")
}
