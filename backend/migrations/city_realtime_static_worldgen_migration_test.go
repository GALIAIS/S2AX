package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeStaticWorldgenMigrationDefinesSealedSeparateProjection(t *testing.T) {
	content, err := FS.ReadFile("253_city_realtime_static_worldgen.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-openworld-realtime-v2",
		"create table if not exists city_realtime_spatial_bindings",
		"create table if not exists city_realtime_spatial_regions",
		"create table if not exists city_realtime_spatial_sectors",
		"create table if not exists city_realtime_spatial_chunks",
		"create table if not exists city_realtime_spatial_buildings",
		"create table if not exists city_realtime_spatial_building_interiors",
		"create table if not exists city_realtime_spatial_portals",
		"city_realtime_spatial_binding_genesis_frame_fk",
		"deferrable initially deferred",
		"city_realtime_static_worldgen_write_enabled",
		"guard_city_realtime_static_worldgen_projection",
		"immutable outside genesis initialization",
	} {
		require.Contains(t, sql, required)
	}

	// V2 deliberately has a separate namespace. Accidentally creating rows in
	// the legacy materialization tables would re-enable their tick-bound
	// triggers and invalidate the realtime engine boundary.
	require.NotContains(t, sql, "insert into city_open_world_")
	require.NotContains(t, sql, "create table if not exists city_open_world_")
}
