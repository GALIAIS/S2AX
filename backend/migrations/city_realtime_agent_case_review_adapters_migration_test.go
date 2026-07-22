package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentCaseReviewAdaptersMigrationIsVersionedAndStrict(t *testing.T) {
	content, err := FS.ReadFile("273_city_realtime_agent_case_review_adapters.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-agent-core', '1.6.0'",
		"character.case.review.file",
		"city-realtime-character-action-context-v4",
		"city_realtime_character_case_review_world_bindings",
		"city_realtime_character_case_review_heads",
		"city_realtime_character_case_review_events",
		"city_realtime_character_case_review_mutation_enabled",
		"closed_no_change",
		"system.realtime.character_case_review_close",
		"intent_action_code <> 'character.case.review.file'",
		"binding.policy_version in ('1.1.0', '1.2.0', '1.3.0', '1.4.0', '1.5.0', '1.6.0')",
		"binding.policy_version in ('1.2.0', '1.3.0', '1.4.0', '1.5.0', '1.6.0')",
		"agent.policy_version in ('1.4.0', '1.5.0', '1.6.0')",
		"agent.policy_version in ('1.5.0', '1.6.0')",
		"agent.policy_version = '1.6.0'",
		"realtime_character_case_review",
	} {
		require.Contains(t, sql, required)
	}

	for _, forbidden := range []string{
		"virtual_currency_wallet",
		"payment_order",
		"provider_secret",
		"raw_model_response",
		"platform_wallet",
		"fine_override",
		"ruling_override",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
