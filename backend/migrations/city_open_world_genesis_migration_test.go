package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldGenesisMigrationKeepsV2FactsSeparateAndImmutable(t *testing.T) {
	content, err := FS.ReadFile("211_city_open_world_genesis.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_open_world_bindings",
		"create table if not exists city_open_world_sectors",
		"create table if not exists city_open_world_chunks",
		"create table if not exists city_open_world_buildings",
		"city_open_world_initialization_write_enabled",
		"guard_city_open_world_projection",
		"city-openworld-v1",
		"rule_set_hash",
		"sector_size_chunks = 8",
		"chunk_size = 32",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "city_overmap_tiles")
	require.NotContains(t, sql, "language plpython")
}
