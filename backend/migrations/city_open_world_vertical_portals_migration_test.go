package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldVerticalPortalsMigrationPinsImmutableTopology(t *testing.T) {
	content, err := FS.ReadFile("215_city_open_world_vertical_portals.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_open_world_portals",
		"city-openworld-v3",
		"city_open_world_materialization_write_enabled",
		"portal_type in ('entrance', 'stairs')",
		"topology_hash",
		"city_open_world_portal_building_fk",
		"city_open_world_portal_shape_check",
		"city_open_world_portal_guard",
		"guard_city_open_world_projection",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "world_portal_states")
}
