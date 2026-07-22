package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV23CarrierRecoveryMigrationPinsManualReserveAndClaimClosure(t *testing.T) {
	content, err := FS.ReadFile("246_city_open_world_v23_carrier_recovery.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-openworld-v23",
		"openworld_v22_to_v23",
		"carrier_reserve",
		"carrier_claim_recovery",
		"create table if not exists city_open_world_carrier_recovery_profiles",
		"create table if not exists city_open_world_carrier_reserve_fundings",
		"create table if not exists city_open_world_freight_claim_recoveries",
		"manual_reserve_only",
		"government_to_manual_carrier_reserve_v1",
		"carrier_claim_to_seller_cash_recovery_v1",
		"system_freight_reserve",
		"assert_city_open_world_carrier_recovery_foundation",
		"resolved carrier claim has no recovery evidence",
		"carrier_recovery_write_enabled",
		"city-openworld-v22','city-openworld-v23",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "unique (world_id, claim_code)")
	require.Contains(t, sql, "recovery_count + 1")
	require.NotContains(t, sql, "unsafe-eval")
}

func TestCityOpenWorldV23CommandStatusGuardPinsAuditedPendingWindow(t *testing.T) {
	content, err := FS.ReadFile("250_city_open_world_v23_command_status_and_v22_version_guard.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"assert_city_open_world_freight_settlement_foundation",
		"world_version not in ('city-openworld-v22','city-openworld-v23','city-openworld-v24')",
		"assert_city_open_world_carrier_recovery_foundation",
		"command.status = 'pending'",
		"city_open_world_carrier_recovery_write_enabled(funding.world_id)",
		"city_open_world_carrier_recovery_write_enabled(recovery.world_id)",
		"journal.journal_type <> 'subsidy'",
		"journal.journal_type <> 'cash_transfer'",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "unsafe-eval")
}
