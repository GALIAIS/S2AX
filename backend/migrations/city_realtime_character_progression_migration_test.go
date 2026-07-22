package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterProgressionMigrationIsVersionedAndSealed(t *testing.T) {
	content, err := FS.ReadFile("264_city_realtime_character_progression.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-character-core', '1.3.0'",
		"\"progression\"",
		"city_realtime_character_attribute_states",
		"city_realtime_character_role_assignments",
		"city_realtime_character_progression_events",
		"progression_revision",
		"progression_event_chain_hash",
		"progression_state_hash",
		"guard_city_realtime_character_progression_event",
		"character.role",
		"realtime_character_progression",
		"on conflict (catalog_id, catalog_version) do nothing",
	} {
		require.Contains(t, sql, required)
	}

	// Role and attribute state remains world-local. The migration cannot alter
	// prior catalogs, actor world generation, or any platform wallet.
	require.NotContains(t, sql, "update city_realtime_character_activity_catalogs")
	require.NotContains(t, sql, "delete from city_realtime_character_activity_catalogs")
	require.NotContains(t, sql, "virtual_currency_wallet")
	require.NotContains(t, sql, "update city_realtime_spatial_")
}
