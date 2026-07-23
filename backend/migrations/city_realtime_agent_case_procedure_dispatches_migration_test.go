package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentCaseProcedureDispatchMigrationIsBoundedAndNonAdjudicative(t *testing.T) {
	content, err := FS.ReadFile("278_city_realtime_agent_case_procedure_dispatches.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-agent-core', '1.11.0'",
		"city-realtime-character-action-context-v4",
		"city-realtime-character-case-procedure-dispatch-v1",
		"city_realtime_character_case_procedure_dispatch_world_bindings",
		"city_realtime_character_case_procedure_dispatch_heads",
		"city_realtime_character_case_procedure_dispatch_events",
		"procedure_queued",
		"source_window_closed",
		"assignment_link_event_hash",
		"agent.policy_version = '1.11.0'",
		"city_realtime_agent_core_policy_at_least",
	} {
		require.Contains(t, sql, required)
	}

	for _, forbidden := range []string{
		"raw_model_response",
		"provider_secret",
		"payment_order",
		"virtual_currency_wallet",
		"platform_wallet",
		"fine_override",
		"ruling_override",
		"character.case.adjudicate",
		"report_text",
		"report_reason",
		"evidence_verified",
		"case_created",
		"reviewer_id",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
