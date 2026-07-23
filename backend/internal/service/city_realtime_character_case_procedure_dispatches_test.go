package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterCaseProcedureDispatchRemainsBoundedToSourceWindow(t *testing.T) {
	reporter := "character.player.0123456789abcdef0123456789abcdef"
	subject := "character.player.fedcba9876543210fedcba9876543210"
	law := cityRealtimeCharacterLawEventRecord{
		ActorCode:              subject,
		EventSequence:          1,
		ActivityEventSequence:  1,
		FrameSequence:          17,
		CaseCode:               "law.fedcba9876543210.1",
		RuleCode:               "rule.public_disruption",
		Disposition:            "fine",
		PenaltyCityCreditUnits: 12,
		StandingDeltaMilli:     -140,
		PublicVisibility:       true,
		PreviousEventHash:      strings.Repeat("a", 64),
	}
	var err error
	law.EventHash, err = cityRealtimeCharacterLawEventHash(law)
	require.NoError(t, err)
	evidence, _, err := cityRealtimeCharacterCaptureCaseEvidenceFromLaw(law, 21*cityRealtimeTimeQuantumUS)
	require.NoError(t, err)
	report, reportEvent, err := cityRealtimeCharacterFileCaseReport(reporter, subject, "intent.case.report", 18)
	require.NoError(t, err)
	intake, _, err := cityRealtimeCharacterOpenCaseIntake(report, reportEvent, 18, 28*cityRealtimeTimeQuantumUS)
	require.NoError(t, err)
	assignment, _, err := cityRealtimeCharacterLinkCaseEvidenceAssignment(
		report, reportEvent, intake, evidence, 18, 20*cityRealtimeTimeQuantumUS,
	)
	require.NoError(t, err)

	queued, queuedEvent, err := cityRealtimeCharacterQueueCaseProcedureDispatch(assignment, 18)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseProcedureDispatchHeadValid(queued))
	require.True(t, cityRealtimeCharacterCaseProcedureDispatchEventValid(queuedEvent))
	require.Equal(t, cityRealtimeCharacterCaseProcedureDispatchQueued, queued.DispatchStatus)
	require.Equal(t, assignment.EventChainHash, queued.AssignmentLinkEventHash)
	require.Equal(t, queued.AssignmentLinkEventHash, queuedEvent.AssignmentLinkEventHash)
	require.NotContains(t, queued.AssignmentLinkEventHash, law.CaseCode)
	require.NotContains(t, queued.AssignmentLinkEventHash, law.RuleCode)

	expiredEvidence, _, err := cityRealtimeCharacterExpireCaseEvidence(evidence, 19, 21*cityRealtimeTimeQuantumUS)
	require.NoError(t, err)
	closedAssignment, _, err := cityRealtimeCharacterCloseCaseEvidenceAssignment(assignment, expiredEvidence, 19)
	require.NoError(t, err)
	closed, closedEvent, err := cityRealtimeCharacterCloseCaseProcedureDispatch(queued, closedAssignment, 19)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseProcedureDispatchHeadValid(closed))
	require.True(t, cityRealtimeCharacterCaseProcedureDispatchEventValid(closedEvent))
	require.Equal(t, cityRealtimeCharacterCaseProcedureDispatchSourceWindowClosed, closed.DispatchStatus)
	require.Equal(t, cityRealtimeCharacterCaseProcedureDispatchClosedEvent, closedEvent.EventType)
	require.Equal(t, queuedEvent.EventHash, closedEvent.PreviousEventHash)

	state := &cityRealtimeCharacterCaseProcedureDispatchHashState{
		SchemaVersion: cityRealtimeCharacterCaseProcedureDispatchSchemaVersion,
		Binding: &cityRealtimeCharacterCaseProcedureDispatchBinding{
			SchemaVersion:    cityRealtimeCharacterCaseProcedureDispatchSchemaVersion,
			AgentBindingHash: strings.Repeat("b", 64),
		},
		Heads: []cityRealtimeCharacterCaseProcedureDispatchHead{closed},
	}
	state.Binding.BindingHash = cityRealtimeCharacterCaseProcedureDispatchBindingHash(state.Binding.AgentBindingHash)
	require.NoError(t, validateCityRealtimeCharacterCaseProcedureDispatchHashState(state))
}
