package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV22FreightSettlementMigrationPinsPartialSettlementEvidence(t *testing.T) {
	content, err := FS.ReadFile("244_city_open_world_v22_freight_settlement.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-openworld-v22",
		"openworld_v21_to_v22",
		"partial_freight_settlement",
		"create table if not exists city_open_world_freight_settlement_profiles",
		"create table if not exists city_open_world_freight_settlement_orders",
		"create table if not exists city_open_world_freight_settlement_cases",
		"create table if not exists city_open_world_freight_settlement_case_lines",
		"create table if not exists city_open_world_freight_settlement_receipts",
		"create table if not exists city_open_world_freight_settlement_receipt_lines",
		"create table if not exists city_open_world_freight_settlement_claims",
		"settlement.confirmed",
		"order.settled",
		"freight_refund",
		"freight_settlement",
		"settled_count",
		"assert_city_open_world_freight_settlement_foundation",
		"city_open_world_freight_settlement_bootstrap_write_enabled",
		"city_open_world_freight_settlement_write_enabled",
		"city_open_world_freight_settlement_recovery_world_id",
		"v22_freight_settlement_completed",
		"v22 v18 settlement proof is invalid",
		"last_runtime_fact_id remains the mobility fact",
	} {
		require.Contains(t, sql, required)
	}

	require.Contains(t, sql, "receipt_count = received_count")
	require.Contains(t, sql, "consignment.state <> 'settled'")
	require.Contains(t, sql, "last_runtime_fact.fact_type not in ('route.completed','demand.expired','demand.voided','transport.orphaned')")
	require.Contains(t, sql, "city_open_world_freight_settlement_receipt_line_unique")
	require.Contains(t, sql, "city_open_world_freight_settlement_receipt_command_unique")
	require.NotContains(t, sql, "unsafe-eval")
}

func TestCityOpenWorldV22FreightSettlementFailureClosureMigrationPinsNoReceiptEscapeHatch(t *testing.T) {
	content, err := FS.ReadFile("245_city_open_world_v22_freight_settlement_failure_closure.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"state in ('awaiting_outcome','receiving','settled','voided')",
		"city_open_world_freight_settlement_order_supply_order_unique",
		"unique (world_id, order_code)",
		"v15 failure",
		"voided case",
		"supply_state.state <> 'failed'",
		"receipt_count <> 0",
		"freight-settlement custody linkage is invalid",
		"freight-settlement financial or resource evidence is invalid",
		"assert_city_open_world_freight_settlement_foundation",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "unsafe-eval")
}

func TestCityOpenWorldV22FreightResourceOperationGuardPinsSettlementCompatibility(t *testing.T) {
	content, err := FS.ReadFile("248_city_open_world_v22_freight_resource_operation_guard.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"assert_city_resource_operation_ready_v22",
		"target_type <> 'freight_settlement'",
		"open_world.freight_settlement.receipt",
		"reason_code in ('delivered', 'settled', 'cancelled', 'expired', 'failed')",
		"guard_city_open_world_enterprise_freight_receipt_projection",
		"fact.fact_type = 'order.delivered'",
		"fact.fact_type = 'order.settled' and row_data->>'fact_type' = 'settlement.confirmed'",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "unsafe-eval")
}

func TestCityOpenWorldV22PartialBatchSettlementGuardPinsOrderLevelCustody(t *testing.T) {
	content, err := FS.ReadFile("249_city_open_world_v22_partial_batch_settlement_guard.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"assert_city_open_world_freight_settlement_foundation",
		"guard_city_open_world_freight_batch_projection",
		"settlement_order.state = 'settled' and shipment.state <> 'settled'",
		"settlement_order.state <> 'settled' and shipment.state <> settlement_case.transport_state",
		"settlement_order.state = 'settled' and consignment.state <> 'settled'",
		"settlement_order.state <> 'settled' and consignment.state <> settlement_case.transport_state",
		"fact.fact_type = 'order.settled' and row_data->>'fact_type' = 'settlement.confirmed'",
		"v22 settlement must own the v15 terminal state",
		"v22 v18 settlement proof is invalid",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "unsafe-eval")
}
