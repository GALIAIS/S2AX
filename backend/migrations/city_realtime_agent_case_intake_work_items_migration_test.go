package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentCaseIntakeWorkItemsMigrationIsEvidenceIsolated(t *testing.T) {
	content, err := FS.ReadFile("275_city_realtime_agent_case_intake_work_items.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-agent-core', '1.8.0'",
		"character.case.report.file",
		"city-realtime-character-action-context-v4",
		"city-realtime-character-case-intake-v1",
		"city_realtime_character_case_intake_world_bindings",
		"city_realtime_character_case_intake_heads",
		"city_realtime_character_case_intake_events",
		"evidence_required",
		"expired_no_evidence",
		"system.realtime.character_case_intake_expire",
		"realtime_character_case_intake",
		"agent.policy_version in ('1.7.0', '1.8.0')",
		"agent.policy_version = '1.8.0'",
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
		"character.case.evidence.submit",
		"character.case.adjudicate",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
