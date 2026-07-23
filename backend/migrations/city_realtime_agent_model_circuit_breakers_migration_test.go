package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentModelCircuitBreakersMigrationIsWorkerBound(t *testing.T) {
	content, err := FS.ReadFile("283_city_realtime_agent_model_circuit_breakers.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city_realtime_agent_model_circuit_breakers",
		"city_realtime_agent_model_breaker_worker_mutation_enabled",
		"guard_city_realtime_agent_model_circuit_breaker",
		"breaker_state in ('closed', 'open', 'half_open')",
		"consecutive_provider_failures",
		"probe_request_code",
		"probe_lease_expires_at",
		"profile_hash",
		"budget_hash",
		"requires the worker gate",
		"never alters realtime world time or canonical state",
	} {
		require.Contains(t, sql, required)
	}

	for _, forbidden := range []string{
		"raw_model_response",
		"provider_secret",
		"prompt_text",
		"system_prompt",
		"payment_order",
		"virtual_currency_wallet",
		"reward_amount",
		"reward_currency",
		"account_id",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
