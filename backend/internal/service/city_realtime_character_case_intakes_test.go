package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterCaseIntakeExpiresWithoutPromotingReport(t *testing.T) {
	reporter := "character.player.0123456789abcdef0123456789abcdef"
	subject := "npc.citizen.0123456789abcdef0123456789abcdef"
	intent := "ait.case-intake." + strings.Repeat("b", 64)
	report, reportEvent, err := cityRealtimeCharacterFileCaseReport(reporter, subject, intent, 17)
	require.NoError(t, err)
	dueWorldTimeUS, err := cityRealtimeCharacterCaseIntakeExpirationDueWorldTime(8 * cityRealtimeTimeQuantumUS)
	require.NoError(t, err)

	head, opened, err := cityRealtimeCharacterOpenCaseIntake(report, reportEvent, 17, dueWorldTimeUS)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseIntakeHeadValid(head))
	require.True(t, cityRealtimeCharacterCaseIntakeEventValid(opened))
	require.Equal(t, int64(1), head.IntakeRevision)
	require.Equal(t, cityRealtimeCharacterCaseIntakeEvidenceRequired, head.IntakeStatus)
	require.Equal(t, reportEvent.EventHash, head.ReportEventHash)
	require.Equal(t, opened.EventHash, head.EventChainHash)
	require.Equal(t, cityRealtimeCharacterCaseIntakeEvidenceRequired, opened.EventType)

	genesis, err := cityRealtimeCharacterCaseIntakeChainGenesisHash(
		reporter, subject, reportEvent.EventSequence, reportEvent.EventHash,
	)
	require.NoError(t, err)
	require.Equal(t, genesis, opened.PreviousEventHash)

	_, _, err = cityRealtimeCharacterExpireCaseIntake(head, 17, dueWorldTimeUS)
	require.ErrorIs(t, err, ErrCityInvalidInput)
	next, expired, err := cityRealtimeCharacterExpireCaseIntake(head, 18, dueWorldTimeUS)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseIntakeHeadValid(next))
	require.True(t, cityRealtimeCharacterCaseIntakeEventValid(expired))
	require.Equal(t, int64(2), next.IntakeRevision)
	require.Equal(t, cityRealtimeCharacterCaseIntakeExpiredNoEvidence, next.IntakeStatus)
	require.Equal(t, reportEvent.EventHash, next.ReportEventHash)
	require.Equal(t, cityRealtimeCharacterCaseIntakeExpiredNoEvidence, expired.EventType)
	require.Equal(t, opened.EventHash, expired.PreviousEventHash)

	state := &cityRealtimeCharacterCaseIntakeHashState{
		SchemaVersion: cityRealtimeCharacterCaseIntakeSchemaVersion,
		Binding: &cityRealtimeCharacterCaseIntakeBinding{
			SchemaVersion:    cityRealtimeCharacterCaseIntakeSchemaVersion,
			AgentBindingHash: strings.Repeat("c", 64),
		},
		Heads: []cityRealtimeCharacterCaseIntakeHead{next},
	}
	state.Binding.BindingHash = cityRealtimeCharacterCaseIntakeBindingHash(state.Binding.AgentBindingHash)
	require.NoError(t, validateCityRealtimeCharacterCaseIntakeHashState(state))
}

func TestCityRealtimeCharacterCaseIntakePolicyRetainsTheSealedReportAction(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	binding.PolicyVersion = cityRealtimeAgentCorePolicyVersionIntake
	binding.BindingHash = cityRealtimeAgentBindingHash(binding)
	reporter := "character.player.0123456789abcdef0123456789abcdef"
	agent := cityRealtimeAgentInstance{
		AgentCode:       "agent.user.0123456789abcdef0123456789abcdef",
		AgentSubtype:    "character.user",
		LifecycleStatus: "active",
		ControlMode:     "autonomous",
		ActorCode:       &reporter,
	}
	allowed, available := cityRealtimeAgentDecisionAllowedActions(binding, agent)
	require.True(t, available)
	require.Contains(t, allowed, cityRealtimeAgentIntentActionCaseReport)
	require.True(t, cityRealtimeAgentCharacterCaseReportRuntimeEnabled(binding))
	require.True(t, cityRealtimeAgentCharacterCaseIntakeRuntimeEnabled(binding))
	require.NotContains(t, allowed, "character.case.evidence.submit")
	require.NotContains(t, allowed, "character.case.adjudicate")
}
