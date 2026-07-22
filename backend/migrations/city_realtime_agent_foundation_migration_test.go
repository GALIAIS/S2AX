package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentFoundationMigrationPinsPolicyAndSealsLifecycle(t *testing.T) {
	content, err := FS.ReadFile("258_city_realtime_agent_foundation.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"create table if not exists city_realtime_agent_policy_bundles",
		"create table if not exists city_realtime_agent_world_bindings",
		"create table if not exists city_realtime_agent_instances",
		"create table if not exists city_realtime_agent_lifecycle_events",
		"city-realtime-agent-core",
		"city_realtime_agent_initialization_enabled",
		"city_realtime_agent_mutation_enabled",
		"guard_city_realtime_agent_world_binding",
		"guard_city_realtime_agent_instance",
		"guard_city_realtime_agent_lifecycle_event",
		"immutable outside genesis initialization",
		"sealed frame reducer",
		"append-only agent lifecycle chain",
		"realtime_agents",
	} {
		require.Contains(t, sql, required)
	}

	// The new realtime runtime must never reach into the legacy tick-driven
	// open-world table family while establishing its own lifecycle ledger.
	require.NotContains(t, sql, "city_open_world_")
}
