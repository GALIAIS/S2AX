package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentModelProviderRetryMigrationIsWorkerBoundAndSecretFree(t *testing.T) {
	content, err := FS.ReadFile("284_city_realtime_agent_model_provider_retry_runtime.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"retry_not_before",
		"idx_city_realtime_agent_decision_requests_retry_ready",
		"guard_city_realtime_agent_decision_request",
		"guard_city_realtime_agent_outbox",
		"city_realtime_agent_worker_mutation_enabled",
		"attempt.status = 'failed'",
		"outside the realtime canonical state",
		"excluded from realtime canonical state",
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
