package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldRegionsMigrationPinsV2PlansAndCommandGatedWrites(t *testing.T) {
	content, err := FS.ReadFile("214_city_open_world_regions.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_open_world_regions",
		"region_size_chunks = 32",
		"city-openworld-v2",
		"city_open_world_materialization_write_enabled",
		"city_open_world_binding_guard",
		"city_open_world_region_guard",
		"state_hash is not null",
		"open_world_materialization",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "city_overmap_tiles")
}
