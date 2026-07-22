package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentDecisionRuntimeMigrationIsSealedAndDeferred(t *testing.T) {
	content, err := FS.ReadFile("265_city_realtime_agent_decision_runtime.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-agent-core', '1.1.0'",
		"city_realtime_agent_observations",
		"city_realtime_agent_decision_requests",
		"city_realtime_agent_decision_attempts",
		"city_realtime_agent_decisions",
		"city_realtime_agent_intents",
		"city_realtime_agent_outbox",
		"city_realtime_agent_decision_policy_enabled",
		"city_realtime_agent_worker_mutation_enabled",
		"decision_then_future_frame",
		"guard_city_realtime_agent_observation",
		"guard_city_realtime_agent_decision_request",
		"guard_city_realtime_agent_decision_attempt",
		"guard_city_realtime_agent_decision",
		"guard_city_realtime_agent_intent",
		"guard_city_realtime_agent_outbox",
		"deferrable initially deferred",
		"uq_city_realtime_agent_observations_trigger",
		"realtime_agent_decisions",
	} {
		require.Contains(t, sql, required)
	}

	// The decision runtime must never gain a direct path to platform balance,
	// provider secrets, or the legacy tick-driven world family.
	for _, forbidden := range []string{
		"virtual_currency_wallet",
		"payment_order",
		"api_key",
		"city_open_world_",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
