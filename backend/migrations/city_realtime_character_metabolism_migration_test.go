package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterMetabolismMigrationIsVersionedAndServerOwned(t *testing.T) {
	content, err := FS.ReadFile("262_city_realtime_character_metabolism.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-character-core', '1.2.0'",
		"\"metabolism\"",
		"\"interval_us\": 60000000",
		"state_schema_version",
		"metabolism_revision",
		"last_metabolism_world_time_us",
		"guard_city_realtime_character_profile",
		"realtime_character_metabolism",
		"on conflict (catalog_id, catalog_version) do nothing",
	} {
		require.Contains(t, sql, required)
	}

	// Legacy catalogs remain immutable. Passive simulation state is local to
	// the city world and must never mutate a platform balance directly.
	require.NotContains(t, sql, "update city_realtime_character_activity_catalogs")
	require.NotContains(t, sql, "delete from city_realtime_character_activity_catalogs")
	require.NotContains(t, sql, "virtual_currency_wallet")
}
