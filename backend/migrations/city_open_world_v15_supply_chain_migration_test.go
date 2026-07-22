package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV15SupplyChainMigrationPinsFactBackedLifecycle(t *testing.T) {
	content, err := FS.ReadFile("237_city_open_world_v15_supply_chain.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v15",
		"openworld_v14_to_v15",
		"create table if not exists city_open_world_supply_chain_profiles",
		"create table if not exists city_open_world_supply_chain_nodes",
		"create table if not exists city_open_world_supply_chain_facts",
		"create table if not exists city_open_world_supply_chain_orders",
		"create table if not exists city_open_world_supply_chain_order_lines",
		"create table if not exists city_open_world_supply_chain_order_transitions",
		"create table if not exists city_open_world_supply_chain_reservations",
		"create table if not exists city_open_world_supply_chain_reservation_releases",
		"create table if not exists city_open_world_supply_chain_dispatches",
		"create table if not exists city_open_world_supply_chain_deliveries",
		"create table if not exists city_open_world_supply_chain_settlements",
		"append_only_order_transition_v1",
		"acceptance_purchase_reversal_v1",
		"atomic_inventory_transfer_v1",
		"city_open_world_supply_chain_bootstrap_write_enabled",
		"city_open_world_supply_chain_write_enabled",
		"city_inventory_balances_id_world_unique",
		"guard_city_open_world_runtime_fact_insert",
		"city_open_world_supply_delivery_resource_operation_authorized",
		"assert_city_resource_operation_ready",
		"guard_city_open_world_supply_chain_profile",
		"assert_city_open_world_supply_chain_foundation",
		"sub2api-open-world-supply-chain-catalog",
		"city_recovery_write_enabled",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "source_balance_id <> destination_balance_id")
	require.Contains(t, sql, "quantity_units::numeric * unit_price_units::numeric = total_price_units::numeric")
	require.Contains(t, sql, "city_open_world_supply_chain_release_reservation_unique")
	require.Contains(t, sql, "city_open_world_supply_chain_delivery_operation_unique")
	require.Contains(t, sql, "city_open_world_supply_chain_settlement_id_world_unique")
	require.Contains(t, sql, "city-openworld-v14','city-openworld-v15')\n       or new.tick is distinct from world_tick + 1")
	require.Contains(t, sql, "open_world.supply_order.deliver")
	require.Contains(t, sql, "and not city_open_world_supply_delivery_resource_operation_authorized(target_operation_id)")
}
