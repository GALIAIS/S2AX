package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV24CarrierCommerceMigrationPinsCausalCashOnlySettlement(t *testing.T) {
	content, err := FS.ReadFile("247_city_open_world_v24_carrier_commerce.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-openworld-v24",
		"openworld_v23_to_v24",
		"carrier_service_contracts",
		"cash_only_carrier_fees",
		"create table if not exists city_open_world_carrier_commerce_profiles",
		"create table if not exists city_open_world_carrier_service_contracts",
		"create table if not exists city_open_world_carrier_fee_payments",
		"v22_case_quoted_carrier_service_v1",
		"seller_cash_per_unit_carrier_fee_v1",
		"freight_fee",
		"assert_city_open_world_carrier_commerce_foundation",
		"carrier_commerce_write_enabled",
		"city_open_world_infrastructure_bootstrap_write_enabled",
		"city_open_world_effective_capacity_bootstrap_write_enabled",
		"cannot extend v20 infrastructure assertion to v24",
		"cannot extend v21 effective-capacity assertion to v24",
		"payment.payment_tick <= contract.contract_tick",
		"contract.source_tick <= profile_tick",
		"system_freight_reserve",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "unique (world_id, case_code)")
	require.Contains(t, sql, "unique (world_id, contract_code)")
	require.Contains(t, sql, "unique (world_id, journal_id)")
	require.NotContains(t, sql, "unsafe-eval")
}
