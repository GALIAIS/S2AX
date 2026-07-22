package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeActorRuntimeMigrationKeepsSharedProjectionAnonymousAndFrameBound(t *testing.T) {
	content, err := FS.ReadFile("257_city_realtime_actor_runtime.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"create table if not exists city_realtime_actor_identities",
		"create table if not exists city_realtime_actor_states",
		"create table if not exists city_realtime_actor_position_events",
		"city_realtime_actor_identity_spawn_frame_fk",
		"city_realtime_actor_state_frame_fk",
		"city_realtime_actor_position_event_frame_fk",
		"deferrable initially deferred",
		"city_realtime_actor_initialization_enabled",
		"city_realtime_actor_mutation_enabled",
		"guard_city_realtime_actor_identity",
		"guard_city_realtime_actor_state",
		"guard_city_realtime_actor_position_event",
		"sealed frame reducer",
		"append-only actor position chain",
		"realtime_actors",
	} {
		require.Contains(t, sql, required)
	}

	// The public actor projection cannot accidentally become a roster or a
	// private Agent store. Those concerns have a separate future control plane.
	for _, prohibited := range []string{
		"owner_user_id",
		"email",
		"username",
		"agent_memory",
		"prompt",
		"model_id",
		"city_open_world_",
	} {
		require.NotContains(t, sql, prohibited)
	}
}
