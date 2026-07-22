package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV6VersionVectorMigrationIsImmutableAndComplete(t *testing.T) {
	content, err := FS.ReadFile("226_city_open_world_v6_version_vector.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v6",
		"create table if not exists city_world_version_vectors",
		"create table if not exists city_world_version_bindings",
		"version_vector_generation",
		"city_world_version_vector_write_enabled",
		"assert_city_world_version_vector",
		"city-openworld-v5', 'city-openworld-v6",
		"openworld_v5_to_v6",
		"content_catalog",
		"economic_policy",
		"worldgen_plan",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "city_spatial_profiles")
	require.NotContains(t, sql, "city_overmap_tiles")
}
