package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentModelProfilesMigrationIsPinnedAndSecretFree(t *testing.T) {
	content, err := FS.ReadFile("282_city_realtime_agent_model_profiles.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city_realtime_agent_model_profile_versions",
		"city_realtime_agent_model_profile_heads",
		"city_realtime_agent_model_profile_world_bindings",
		"city_realtime_agent_model_usage_windows",
		"city_realtime_agent_model_attempt_budget_reservations",
		"system.fake.deterministic",
		"fake.deterministic",
		"sub2api.gateway",
		"model_profile_code",
		"model_profile_version",
		"model_profile_hash",
		"model_budget_hash",
		"reserved_input_tokens",
		"reserved_output_tokens",
		"city_realtime_agent_model_profile_hash",
		"city_realtime_agent_model_budget_hash",
		"guard_city_realtime_agent_decision_request",
		"guard_city_realtime_agent_decision_attempt",
		"city_realtime_agent_model_budget_worker_mutation_enabled",
		"assert_city_realtime_agent_model_profile_genesis",
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
