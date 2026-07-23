package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentCaseEvidenceAssignmentsMigrationIsBoundedAndNonAdjudicative(t *testing.T) {
	content, err := FS.ReadFile("277_city_realtime_agent_case_evidence_assignments.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-agent-core', '1.10.0'",
		"city-realtime-character-action-context-v4",
		"city-realtime-character-case-evidence-assignment-v1",
		"city_realtime_character_case_evidence_assignment_world_bindings",
		"city_realtime_character_case_evidence_assignment_heads",
		"city_realtime_character_case_evidence_assignment_events",
		"independent_record_linked",
		"source_window_closed",
		"assignment_head_evidence_uq",
		"agent.policy_version = '1.10.0'",
		"agent.policy_version in ('1.9.0','1.10.0')",
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
		"evidence_verified",
		"case_created",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
