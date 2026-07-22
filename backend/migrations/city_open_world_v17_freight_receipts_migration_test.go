package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV17EnterpriseFreightReceiptMigrationPinsCustodyBoundary(t *testing.T) {
	content, err := FS.ReadFile("239_city_open_world_v17_freight_receipts.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v17",
		"openworld_v16_to_v17",
		"enterprise_freight_custody",
		"enterprise_freight_receipts",
		"create table if not exists city_open_world_enterprise_freight_receipt_profiles",
		"create table if not exists city_open_world_enterprise_freight_shipments",
		"create table if not exists city_open_world_enterprise_freight_shipment_lines",
		"create table if not exists city_open_world_enterprise_freight_receipt_facts",
		"create table if not exists city_open_world_enterprise_freight_shipment_transitions",
		"create table if not exists city_open_world_enterprise_freight_receipts",
		"v16_source_custody_snapshot_v1",
		"v15_atomic_delivery_receipt_gate_v1",
		"pre_v17_source_legacy_delivery_v1",
		"city_open_world_enterprise_freight_receipt_bootstrap_write_enabled",
		"city_open_world_enterprise_freight_receipt_write_enabled",
		"guard_city_open_world_enterprise_freight_receipt_profile",
		"guard_city_open_world_enterprise_freight_receipt_projection",
		"assert_city_open_world_enterprise_freight_receipt_foundation",
		"sub2api-open-world-enterprise-freight-receipt-catalog",
		"city_recovery_write_enabled",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "source.state <> 'suppressed'")
	require.Contains(t, sql, "shipment.state = 'awaiting_receipt'")
	require.Contains(t, sql, "receipt_count = received_count")
	require.Contains(t, sql, "shipment_count <= maximum_shipments")
	require.Contains(t, sql, "row_data := to_jsonb(new)")
	require.Contains(t, sql, "world_version not in ('city-openworld-v16', 'city-openworld-v17')")
	require.Contains(t, sql, "v16 sources already present at the v17 upgrade baseline")
	require.Contains(t, sql, "city-openworld-v15','city-openworld-v16','city-openworld-v17")
}
