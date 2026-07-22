package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentCharacterActionAdaptersMigrationIsVersionedAndStrict(t *testing.T) {
	content, err := FS.ReadFile("267_city_realtime_agent_character_action_adapters.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-agent-core', '1.3.0'",
		"character.move",
		"character.portal.traverse",
		"character.role.change",
		"city_realtime_agent_decision_policy_enabled",
		"binding.policy_version in ('1.1.0', '1.2.0', '1.3.0')",
		"city_realtime_agent_personality_policy_enabled",
		"binding.policy_version in ('1.2.0', '1.3.0')",
		"realtime_agent_character_navigation_intents",
		"realtime_agent_character_role_intents",
		"character.portal', 'character.role', 'character.agent.configure",
		"arguments ?& array['x', 'y', 'z']",
	} {
		require.Contains(t, sql, required)
	}

	for _, forbidden := range []string{
		"virtual_currency_wallet",
		"payment_order",
		"provider_secret",
		"raw_model_response",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
