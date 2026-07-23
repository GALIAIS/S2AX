package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentDecisionWorkerDeferralMigrationIsOperationalAndSecretFree(t *testing.T) {
	content, err := FS.ReadFile("285_city_realtime_agent_decision_worker_deferral.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"guard_city_realtime_agent_decision_request",
		"old.status = 'queued' and new.status = 'queued'",
		"new.retry_not_before > now()",
		"city_realtime_agent_worker_mutation_enabled",
		"excluded from the realtime canonical state",
		"no attempt count, profile snapshot, lease, outbox or",
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
