package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV16EnterpriseFreightMigrationPinsAdapterBoundary(t *testing.T) {
	content, err := FS.ReadFile("238_city_open_world_v16_enterprise_freight.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v16",
		"openworld_v15_to_v16",
		"enterprise_freight_source_adapter",
		"create table if not exists city_open_world_enterprise_freight_profiles",
		"create table if not exists city_open_world_enterprise_freight_sources",
		"create table if not exists city_open_world_enterprise_freight_source_lines",
		"create table if not exists city_open_world_enterprise_freight_facts",
		"create table if not exists city_open_world_enterprise_freight_transitions",
		"v15_dispatched_fact_snapshot_v1",
		"v9_system_carrier_demand_v1",
		"v9_transport_observation_no_receipt_v1",
		"v15_terminal_pending_demand_void_v1",
		"system.freight.carrier",
		"city_open_world_enterprise_freight_bootstrap_write_enabled",
		"city_open_world_enterprise_freight_write_enabled",
		"guard_city_open_world_enterprise_freight_profile",
		"guard_city_open_world_enterprise_freight_projection",
		"assert_city_open_world_enterprise_freight_foundation",
		"sub2api-open-world-enterprise-freight-catalog",
		"city_recovery_write_enabled",
		"arrival_bridge' is distinct from 'excluded'",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "requested_units > 0")
	require.Contains(t, sql, "metadata->>'maximum_requested_units' = '32'")
	require.Contains(t, sql, "state <> 'suppressed' and requested_units between 1 and 32")
	require.Contains(t, sql, "mode.capacity_units_per_tick >= 32")
	require.Contains(t, sql, "edge.capacity_units_per_tick < 32")
	require.Contains(t, sql, "requires a 32-unit freight edge baseline")
	require.Contains(t, sql, "source_hub_code <> destination_hub_code")
	require.Contains(t, sql, "quantity_units::numeric * unit_price_units::numeric = total_price_units::numeric")
	require.Contains(t, sql, "city_open_world_enterprise_freight_source_order_unique")
	require.Contains(t, sql, "city_open_world_enterprise_freight_fact_runtime_unique")
	require.Contains(t, sql, "city-openworld-v15','city-openworld-v16')")
}
