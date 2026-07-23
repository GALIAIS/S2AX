package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentDecisionDeadLetterMigrationIsOperationalAndSecretFree(t *testing.T) {
	content, err := FS.ReadFile("287_city_realtime_agent_decision_dead_letter.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city_realtime_agent_decision_dead_letters",
		"city_realtime_agent_decision_dead_letter_events",
		"dead_letter_status in ('quarantined', 'released')",
		"append-only administrator review receipts",
		"operator gate",
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
