package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentCharacterNavigationPlansMigrationIsBoundedAndSealed(t *testing.T) {
	content, err := FS.ReadFile("280_city_realtime_agent_character_navigation_plans.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-agent-core', '1.13.0'",
		"city-realtime-character-action-context-v6",
		"city-realtime-character-navigation-plan-v1",
		"character.navigation.plan",
		"city_realtime_character_navigation_plan_world_bindings",
		"city_realtime_character_navigation_plan_heads",
		"city_realtime_character_navigation_plan_events",
		"system.realtime.character_navigation_step",
		"navigation_planned",
		"navigation_step",
		"navigation_arrived",
		"navigation_blocked",
		"navigation_cancelled",
		"city_realtime_character_navigation_plan_head_fact_guard",
		"city_realtime_agent_core_policy_at_least",
		"when '1.13.0' then 13 >= minimum_minor",
		"realtime_character_navigation_plans",
		"maximum_steps = 32",
	} {
		require.Contains(t, sql, required)
	}

	for _, forbidden := range []string{
		"raw_model_response",
		"provider_secret",
		"payment_order",
		"virtual_currency_wallet",
		"platform_wallet",
		"reward_amount",
		"reward_currency",
		"traffic_reservation",
		"route_cache",
		"free_text",
		"prompt_text",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
