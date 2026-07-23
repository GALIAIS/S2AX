package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentCaseEvidenceSourcesMigrationIsIndependentAndRevocable(t *testing.T) {
	content, err := FS.ReadFile("276_city_realtime_agent_case_evidence_sources.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-agent-core', '1.9.0'",
		"city-realtime-character-action-context-v4",
		"city-realtime-character-case-evidence-v1",
		"city_realtime_character_case_evidence_world_bindings",
		"city_realtime_character_case_evidence_heads",
		"city_realtime_character_case_evidence_events",
		"server.sealed_law_event",
		"source_captured",
		"source_expired",
		"system.realtime.character_case_evidence_expire",
		"realtime_case_evidence",
		"agent.policy_version = '1.9.0'",
		"agent.policy_version in ('1.8.0','1.9.0')",
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
		"report_text",
		"report_reason",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
