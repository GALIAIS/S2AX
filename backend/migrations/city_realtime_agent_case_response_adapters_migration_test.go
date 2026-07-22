package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentCaseResponseAdaptersMigrationIsVersionedAndStrict(t *testing.T) {
	content, err := FS.ReadFile("268_city_realtime_agent_case_response_adapters.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-agent-core', '1.4.0'",
		"character.case.acknowledge",
		"city-realtime-character-action-context-v2",
		"city_realtime_character_case_response_world_bindings",
		"city_realtime_character_case_response_heads",
		"city_realtime_character_case_response_events",
		"city_realtime_character_case_response_mutation_enabled",
		"source_intent_code",
		"intent_action_code <> 'character.case.acknowledge'",
		"binding.policy_version in ('1.1.0', '1.2.0', '1.3.0', '1.4.0')",
		"binding.policy_version in ('1.2.0', '1.3.0', '1.4.0')",
		"realtime_character_case_response",
	} {
		require.Contains(t, sql, required)
	}

	for _, forbidden := range []string{
		"virtual_currency_wallet",
		"payment_order",
		"provider_secret",
		"raw_model_response",
		"platform_wallet",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
