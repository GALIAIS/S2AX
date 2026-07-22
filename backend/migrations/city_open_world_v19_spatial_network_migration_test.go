package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV19SpatialNetworkMigrationPinsStaticTopologyAndV18Compatibility(t *testing.T) {
	content, err := FS.ReadFile("241_city_open_world_v19_spatial_network.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city_engine_versions (version, status, canonical_format, capabilities)",
		"city-openworld-v19",
		"openworld_v18_to_v19",
		"spatial_transport_identity",
		"worldgen_transport_styles",
		"static_node_corridor_topology",
		"create table if not exists city_open_world_spatial_network_profiles",
		"create table if not exists city_open_world_spatial_network_nodes",
		"create table if not exists city_open_world_spatial_network_corridors",
		"v9_hub_edge_spatial_corridor_v1",
		"worldgen_transport_style_catalog_v1",
		"city_open_world_spatial_network_bootstrap_write_enabled",
		"guard_city_open_world_spatial_network_profile",
		"guard_city_open_world_spatial_network_projection",
		"assert_city_open_world_spatial_network_foundation",
		"sub2api-open-world-spatial-network-catalog",
		"city-openworld-v18','city-openworld-v19",
		"city_open_world_freight_batch_foundation",
		"city_open_world_supply_delivery_resource_operation_authorized",
	} {
		require.Contains(t, sql, required)
	}

	require.NotContains(t, sql, "city_engine_versions (version, description, is_active)")
	require.Contains(t, sql, "profile_nodes <> (select count(*) from city_open_world_mobility_hubs")
	require.Contains(t, sql, "profile_corridors <> (select count(*) from city_open_world_mobility_edges")
	require.Contains(t, sql, "cannot extend v19 predecessor write gate")
	require.Contains(t, sql, "city_recovery_write_enabled")
}
