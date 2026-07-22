package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentCharacterAutonomyMigrationIsOwnerScopedAndVersioned(t *testing.T) {
	content, err := FS.ReadFile("266_city_realtime_agent_character_autonomy.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-agent-core', '1.2.0'",
		"city_realtime_agent_personality_policy_enabled",
		"city_realtime_character_agent_personality_revisions",
		"character.activity.perform",
		"event_type in ('spawn', 'lifecycle', 'control')",
		"city_realtime_agent_decision_policy_enabled",
		"binding.policy_version in ('1.1.0', '1.2.0')",
		"character.agent.configure",
		"city_realtime_agent_outbox",
		"status = 'cancelled'",
		"append-only sealed owner facts",
		"deferrable initially deferred",
	} {
		require.Contains(t, sql, required)
	}

	// A3.1 must not create a platform-money shortcut or persist a provider
	// prompt/response lane. Personality is owner data, while world effects still
	// go through the existing intent and activity reducers.
	for _, forbidden := range []string{
		"virtual_currency_wallet",
		"payment_order",
		"platform_ledger",
		"provider_secret",
		"raw_model_response",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
