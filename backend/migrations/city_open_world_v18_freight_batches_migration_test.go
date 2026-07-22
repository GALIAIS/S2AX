package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV18FreightBatchMigrationPinsOverflowPackingAndAtomicReceipt(t *testing.T) {
	content, err := FS.ReadFile("240_city_open_world_v18_freight_batches.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v18",
		"openworld_v17_to_v18",
		"freight-batch",
		"create table if not exists city_open_world_freight_batch_profiles",
		"create table if not exists city_open_world_freight_batch_plans",
		"create table if not exists city_open_world_freight_batch_consignments",
		"create table if not exists city_open_world_freight_batch_lines",
		"create table if not exists city_open_world_freight_batch_facts",
		"create table if not exists city_open_world_freight_batch_transitions",
		"create table if not exists city_open_world_freight_batch_receipts",
		"v16_suppressed_overflow_source_v1",
		"stable_line_capacity_packing_v1",
		"v9_freight_consignment_demand_v1",
		"all_consignment_arrivals_then_v15_atomic_delivery_v1",
		"city_open_world_freight_batch_bootstrap_write_enabled",
		"city_open_world_freight_batch_write_enabled",
		"guard_city_open_world_freight_batch_profile",
		"guard_city_open_world_freight_batch_projection",
		"assert_city_open_world_freight_batch_foundation",
		"sub2api-open-world-freight-batch-catalog",
		"city_recovery_write_enabled",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "source.state <> 'suppressed'")
	require.Contains(t, sql, "source.requested_units <= 32")
	require.Contains(t, sql, "requested_units bigint not null check (requested_units between 1 and 32)")
	require.Contains(t, sql, "having sum(consignment.requested_units) <> max(plan.required_units)")
	require.Contains(t, sql, "receipt_count = received_count")
	require.Contains(t, sql, "runtime_fact.fact_type <> 'mobility.requested'")
	require.Contains(t, sql, "runtime_fact.fact_type <> 'mobility.expired'")
	require.NotContains(t, sql, "system.mobility.requested")
	require.NotContains(t, sql, "system.mobility.expired")
	require.Contains(t, sql, "world_version not in ('city-openworld-v17','city-openworld-v18')")
	require.Contains(t, sql, "world.simulation_version in ('city-openworld-v17','city-openworld-v18')")
	require.Contains(t, sql, "city-openworld-v15','city-openworld-v16','city-openworld-v17','city-openworld-v18")
}
