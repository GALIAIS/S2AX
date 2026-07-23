package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterCaseEvidenceAssignmentIsBoundedAndClosesWithSource(t *testing.T) {
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
	evidenceDueWorldTimeUS, err := cityRealtimeCharacterCaseEvidenceExpirationDueWorldTime(20 * cityRealtimeTimeQuantumUS)
	require.NoError(t, err)
	evidence, _, err := cityRealtimeCharacterCaptureCaseEvidenceFromLaw(law, evidenceDueWorldTimeUS)
	require.NoError(t, err)

	report, reportEvent, err := cityRealtimeCharacterFileCaseReport(reporter, subject, "intent.case.report", 18)
	require.NoError(t, err)
	intakeDueWorldTimeUS, err := cityRealtimeCharacterCaseIntakeExpirationDueWorldTime(21 * cityRealtimeTimeQuantumUS)
	require.NoError(t, err)
	intake, _, err := cityRealtimeCharacterOpenCaseIntake(report, reportEvent, 18, intakeDueWorldTimeUS)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseEvidenceAssignmentEligible(
		report, reportEvent, intake, evidence, 18, 21*cityRealtimeTimeQuantumUS,
	))

	assignment, linked, err := cityRealtimeCharacterLinkCaseEvidenceAssignment(
		report, reportEvent, intake, evidence, 18, 21*cityRealtimeTimeQuantumUS,
	)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(assignment))
	require.True(t, cityRealtimeCharacterCaseEvidenceAssignmentEventValid(linked))
	require.Equal(t, cityRealtimeCharacterCaseEvidenceAssignmentLinked, assignment.AssignmentStatus)
	require.Equal(t, evidence.EvidenceCode, assignment.EvidenceCode)
	require.Equal(t, evidence.SourceLawEventHash, assignment.SourceLawEventHash)
	require.NotContains(t, assignment.EvidenceCode, law.CaseCode)
	require.NotContains(t, assignment.EvidenceCode, law.RuleCode)

	expiredEvidence, _, err := cityRealtimeCharacterExpireCaseEvidence(evidence, 19, evidenceDueWorldTimeUS)
	require.NoError(t, err)
	closed, closedEvent, err := cityRealtimeCharacterCloseCaseEvidenceAssignment(assignment, expiredEvidence, 19)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(closed))
	require.True(t, cityRealtimeCharacterCaseEvidenceAssignmentEventValid(closedEvent))
	require.Equal(t, cityRealtimeCharacterCaseEvidenceAssignmentClosed, closed.AssignmentStatus)
	require.Equal(t, cityRealtimeCharacterCaseEvidenceAssignmentClosedEvent, closedEvent.EventType)
	require.Equal(t, linked.EventHash, closedEvent.PreviousEventHash)

	state := &cityRealtimeCharacterCaseEvidenceAssignmentHashState{
		SchemaVersion: cityRealtimeCharacterCaseEvidenceAssignmentSchemaVersion,
		Binding: &cityRealtimeCharacterCaseEvidenceAssignmentBinding{
			SchemaVersion:    cityRealtimeCharacterCaseEvidenceAssignmentSchemaVersion,
			AgentBindingHash: strings.Repeat("b", 64),
		},
		Heads: []cityRealtimeCharacterCaseEvidenceAssignmentHead{closed},
	}
	state.Binding.BindingHash = cityRealtimeCharacterCaseEvidenceAssignmentBindingHash(state.Binding.AgentBindingHash)
	require.NoError(t, validateCityRealtimeCharacterCaseEvidenceAssignmentHashState(state))
}

func TestCityRealtimeCharacterCaseEvidenceAssignmentRejectsCurrentOrExpiredSource(t *testing.T) {
	reporter := "character.player.0123456789abcdef0123456789abcdef"
	subject := "character.player.fedcba9876543210fedcba9876543210"
	report, reportEvent, err := cityRealtimeCharacterFileCaseReport(reporter, subject, "intent.case.report", 17)
	require.NoError(t, err)
	intake, _, err := cityRealtimeCharacterOpenCaseIntake(report, reportEvent, 17, 40*cityRealtimeTimeQuantumUS)
	require.NoError(t, err)
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
		PreviousEventHash:      strings.Repeat("c", 64),
	}
	law.EventHash, err = cityRealtimeCharacterLawEventHash(law)
	require.NoError(t, err)
	evidence, _, err := cityRealtimeCharacterCaptureCaseEvidenceFromLaw(law, 40*cityRealtimeTimeQuantumUS)
	require.NoError(t, err)
	require.False(t, cityRealtimeCharacterCaseEvidenceAssignmentEligible(
		report, reportEvent, intake, evidence, 17, 20*cityRealtimeTimeQuantumUS,
	), "a source captured in the receipt frame cannot be correlated")

	expired, _, err := cityRealtimeCharacterExpireCaseEvidence(evidence, 18, 40*cityRealtimeTimeQuantumUS)
	require.NoError(t, err)
	require.False(t, cityRealtimeCharacterCaseEvidenceAssignmentEligible(
		report, reportEvent, intake, expired, 18, 20*cityRealtimeTimeQuantumUS,
	), "an expired source cannot be correlated")
}
