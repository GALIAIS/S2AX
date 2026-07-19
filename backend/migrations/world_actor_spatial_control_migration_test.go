package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorldActorSpatialControlMigrationDefinesGuardedCanonicalProjection(t *testing.T) {
	content, err := FS.ReadFile("202_world_actor_spatial_control.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists world_actor_locations",
		"create table if not exists world_actor_control_grants",
		"idx_world_actor_control_grants_active",
		"guard_world_actor_spatial_control_projection",
		"assert_world_actor_spatial_control_foundation",
		"check_world_actor_spatial_control_foundation",
		"deferrable initially deferred",
		"city-f7-v6",
		"f7_v5_to_f7_v6",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "capability in ('actor.command', 'actor.control.manage')")
	require.Contains(t, sql, "every f7.7 actor must have exactly one authoritative location")
	require.NotContains(t, sql, "language plpython")
}
