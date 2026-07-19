package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldGroundInteriorsMigrationKeepsFloorPlansImmutable(t *testing.T) {
	content, err := FS.ReadFile("213_city_open_world_ground_interiors.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_open_world_building_interiors",
		"floor_index >= 0",
		"references city_open_world_buildings(world_id, code)",
		"content_hash",
		"city_open_world_interior_guard",
		"guard_city_open_world_projection",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "city_overmap_tiles")
}
