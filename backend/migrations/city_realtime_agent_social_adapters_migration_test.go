package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentSocialAdaptersMigrationIsVersionedAndStrict(t *testing.T) {
	content, err := FS.ReadFile("269_city_realtime_agent_social_adapters.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-agent-core', '1.5.0'",
		"character.social.greet",
		"city-realtime-character-action-context-v3",
		"city_realtime_character_social_world_bindings",
		"city_realtime_character_social_heads",
		"city_realtime_character_social_events",
		"city_realtime_character_social_mutation_enabled",
		"target_actor_code",
		"binding.policy_version in ('1.1.0', '1.2.0', '1.3.0', '1.4.0', '1.5.0')",
		"binding.policy_version in ('1.2.0', '1.3.0', '1.4.0', '1.5.0')",
		"agent.policy_version in ('1.4.0', '1.5.0')",
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
