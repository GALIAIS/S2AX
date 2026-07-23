package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentDecisionOperatorRetryMigrationIsNarrowAndSecretFree(t *testing.T) {
	content, err := FS.ReadFile("286_city_realtime_agent_decision_operator_retry.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city_realtime_agent_operator_mutation_enabled",
		"city_realtime_agent_decision_operator_events",
		"retry_requested",
		"old.status = 'queued' and new.status = 'queued'",
		"old.retry_not_before is not null and old.retry_not_before > now()",
		"new.retry_not_before is null",
		"append-only administrator retry receipts",
		"does not seal a world frame",
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
