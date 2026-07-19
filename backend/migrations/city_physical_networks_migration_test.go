package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityPhysicalNetworksMigrationInstallsConservedRoutedGraph(t *testing.T) {
	content, err := FS.ReadFile("209_city_physical_networks.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_physical_network_profiles",
		"create table if not exists city_physical_network_policies",
		"create table if not exists city_physical_network_facts",
		"create table if not exists city_physical_networks",
		"create table if not exists city_physical_network_nodes",
		"create table if not exists city_physical_network_edges",
		"create table if not exists city_physical_network_flow_batches",
		"create table if not exists city_physical_network_flow_paths",
		"create table if not exists city_physical_network_flow_segments",
		"network_received_units",
		"network_loss_units",
		"connection_loss_units",
		"network_path_count",
		"city_physical_network_bootstrap_write_enabled",
		"city_physical_network_fact_write_enabled",
		"guard_city_physical_network_fact_insert",
		"guard_city_physical_network_fact_immutable",
		"guard_city_physical_network_projection",
		"guard_city_physical_network_flow",
		"assert_city_physical_network_fact_committed",
		"assert_city_physical_network_foundation",
		"city_physical_network_flow_batch_units_check",
		"city_physical_network_flow_path_units_check",
		"city_physical_network_flow_segment_units_check",
		"city physical network flow row is not bound to the active fact snapshot",
		"retired city f8.2 physical network retains live assets",
		"segment.loss_milli",
		"idx_city_physical_network_nodes_active_capacity",
		"idx_city_physical_network_nodes_active_demand",
		"city-f8-v3",
		"f8_v2_to_f8_v3",
		"physical_networks",
		"network_flow",
		"network.topology_synchronized",
		"pre_network",
		"deferrable initially deferred",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "world.simulation_version in ('city-f8-v2', 'city-f8-v3')")
	require.Contains(t, sql, "world_version not in ('city-f8-v2', 'city-f8-v3')")
	require.Contains(t, sql, "city_recovery_write_enabled")
	require.NotContains(t, sql, "language plpython")
}
