package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorldPortalAccessMigrationInstallsGuardedVersionedProjection(t *testing.T) {
	content, err := FS.ReadFile("205_world_portal_access.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists world_portal_states",
		"world_portal_states_world_portal_unique",
		"world_portal_state_portal_fk",
		"world_portal_state_source_fact_fk",
		"guard_world_portal_state_projection",
		"world_runtime_bootstrap_write_enabled",
		"world_runtime_fact_write_enabled",
		"city_recovery_write_enabled",
		"assert_world_portal_access_foundation",
		"assert_world_runtime_foundation",
		"assert_world_actor_spatial_control_foundation",
		"deferrable initially deferred",
		"city-f7-v8",
		"portal_access",
		"f7_v7_to_f7_v8",
		"runtime_version = '1.2.0'",
		"portal.state.changed",
		"portal.access.changed",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "city-f7-v7', 'city-f7-v8")
	require.NotContains(t, sql, "language plpython")
}
