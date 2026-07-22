package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeEngineCapabilityTransitionKeepsImmutableDefinitionFence(t *testing.T) {
	content, err := FS.ReadFile("256a_city_realtime_engine_capability_transition.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"guard_city_engine_definition_immutable",
		"city-openworld-realtime-v2",
		"realtime_actors",
		"realtime_agents",
		"new.status = old.status",
		"new.canonical_format = old.canonical_format",
		"city engine definitions are immutable",
	} {
		require.Contains(t, sql, required)
	}
}
