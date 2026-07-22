package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterLifeMigrationKeepsActivitiesSealedAndNonRedeemable(t *testing.T) {
	content, err := FS.ReadFile("260_city_realtime_character_life.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city_realtime_character_activity_catalogs",
		"city_realtime_character_activity_world_bindings",
		"city_realtime_character_profiles",
		"city_realtime_character_inventory_stacks",
		"city_realtime_character_activity_events",
		"city_realtime_character_law_events",
		"city_realtime_character_activity_mutation_enabled",
		"guard_city_realtime_character_profile",
		"guard_city_realtime_character_activity_event",
		"character.activity",
		"city_credit",
		"platform wallets",
		"conduct.disruption",
	} {
		require.Contains(t, sql, required)
	}

	// The realtime character life bridge must stay separate from the legacy
	// tick-driven open-world family and from direct platform-wallet writes.
	require.NotContains(t, sql, "city_open_world_")
	require.NotContains(t, sql, "virtual_currency_wallet")
}
