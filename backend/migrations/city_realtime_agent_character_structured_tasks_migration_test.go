package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentCharacterStructuredTasksMigrationIsBoundedAndSealed(t *testing.T) {
	content, err := FS.ReadFile("279_city_realtime_agent_character_structured_tasks.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-agent-core', '1.12.0'",
		"city-realtime-character-action-context-v5",
		"city-realtime-character-task-v1",
		"character.task.accept",
		"city_realtime_character_task_catalogs",
		"city_realtime_character_task_world_bindings",
		"city_realtime_character_task_heads",
		"city_realtime_character_task_events",
		"city_realtime_agent_decision_action_check",
		"city_realtime_agent_intent_identity_check",
		"task_accepted",
		"task_completed",
		"task_expired",
		"sealed_agent_activity_event",
		"city_realtime_character_task_head_fact_guard",
		"city_realtime_agent_core_policy_at_least",
		"when '1.12.0' then 12 >= minimum_minor",
		"city_realtime_character_case_procedure_dispatch_initialization_enabled",
	} {
		require.Contains(t, sql, required)
	}

	for _, forbidden := range []string{
		"raw_model_response",
		"provider_secret",
		"payment_order",
		"virtual_currency_wallet",
		"platform_wallet",
		"task_reward",
		"reward_amount",
		"reward_currency",
		"case_created",
		"adjudicat",
		"free_text",
		"prompt_text",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
