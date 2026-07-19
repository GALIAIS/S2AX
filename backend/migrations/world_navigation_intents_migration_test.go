package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorldNavigationIntentsMigrationInstallsGuardedVersionedProjection(t *testing.T) {
	content, err := FS.ReadFile("206_world_navigation_intents.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists world_navigation_profiles",
		"create table if not exists world_actor_navigation_intents",
		"create table if not exists world_navigation_reservations",
		"world_actor_navigation_intents_actor_unique",
		"world_navigation_reservations_tick_target_unique",
		"world_navigation_reservations_tick_edge_unique",
		"guard_world_navigation_profile_projection",
		"guard_world_actor_navigation_intent_projection",
		"guard_world_navigation_reservation_projection",
		"assert_world_navigation_intent_foundation",
		"assert_world_runtime_foundation",
		"assert_world_actor_spatial_control_foundation",
		"assert_world_portal_access_foundation",
		"deferrable initially deferred",
		"city-f7-v9",
		"navigation_intents",
		"f7_v8_to_f7_v9",
		"runtime_version = case when world_version = 'city-f7-v9' then '1.3.0'",
		"actor.navigation.intent.progressed",
		"actor.navigation.intent.set",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "city-f7-v8', 'city-f7-v9")
	require.NotContains(t, sql, "language plpython")
}
