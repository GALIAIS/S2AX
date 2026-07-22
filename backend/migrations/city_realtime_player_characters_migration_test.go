package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimePlayerCharactersMigrationKeepsWritesInsideSealedFrames(t *testing.T) {
	content, err := FS.ReadFile("259_city_realtime_player_characters.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city_realtime_character_action_receipts",
		"city_realtime_character_receipt_write_enabled",
		"guard_city_realtime_character_action_receipt",
		"sealed character creation",
		"character.create",
		"character.move",
		"owner_user_id",
		"city_realtime_actor_mutation_enabled",
	} {
		require.Contains(t, sql, required)
	}

	// This bridge belongs exclusively to the realtime-v2 tables; it must not
	// reopen the legacy tick-driven open-world actor family.
	require.NotContains(t, sql, "city_open_world_")
}
