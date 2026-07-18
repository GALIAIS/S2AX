package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenWorldRuntimeFoundationDefinesGuardedGenericFactAndEffectChain(t *testing.T) {
	content, err := FS.ReadFile("201_open_world_runtime_foundation.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists world_runtime_profiles",
		"create table if not exists world_runtime_definitions",
		"create table if not exists world_actors",
		"create table if not exists world_actor_attributes",
		"create table if not exists world_actor_roles",
		"create table if not exists world_actor_statuses",
		"create table if not exists world_runtime_facts",
		"create table if not exists world_effect_operations",
		"create table if not exists world_rule_cases",
		"world_runtime_bootstrap_write_enabled",
		"world_runtime_fact_write_enabled",
		"guard_world_runtime_fact_immutable",
		"guard_world_runtime_projection_write",
		"guard_world_effect_operation_immutable",
		"assert_world_runtime_foundation",
		"city-f7-v5",
		"f7_v4_to_f7_v5",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "language plpython")
}
