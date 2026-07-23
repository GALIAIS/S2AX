package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterCaseEvidenceExpiresWithoutChangingItsSealedLawSource(t *testing.T) {
	law := cityRealtimeCharacterLawEventRecord{
		ActorCode:              "character.player.0123456789abcdef0123456789abcdef",
		EventSequence:          1,
		ActivityEventSequence:  1,
		FrameSequence:          17,
		CaseCode:               "law.0123456789abcdef.1",
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
	require.True(t, cityRealtimeCharacterCaseEvidenceLawSourceValid(law))

	dueWorldTimeUS, err := cityRealtimeCharacterCaseEvidenceExpirationDueWorldTime(8 * cityRealtimeTimeQuantumUS)
	require.NoError(t, err)
	head, captured, err := cityRealtimeCharacterCaptureCaseEvidenceFromLaw(law, dueWorldTimeUS)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseEvidenceHeadValid(head))
	require.True(t, cityRealtimeCharacterCaseEvidenceEventValid(captured))
	require.Equal(t, cityRealtimeCharacterCaseEvidenceActive, head.EvidenceStatus)
	require.Equal(t, cityRealtimeCharacterCaseEvidenceCaptured, captured.EventType)
	require.Equal(t, law.EventHash, head.SourceLawEventHash)
	require.Equal(t, law.FrameSequence, head.SourceFrameSequence)
	require.NotContains(t, head.EvidenceCode, law.CaseCode)
	require.NotContains(t, head.EvidenceCode, law.RuleCode)

	_, _, err = cityRealtimeCharacterExpireCaseEvidence(head, law.FrameSequence, dueWorldTimeUS)
	require.ErrorIs(t, err, ErrCityInvalidInput)
	next, expired, err := cityRealtimeCharacterExpireCaseEvidence(head, law.FrameSequence+1, dueWorldTimeUS)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseEvidenceHeadValid(next))
	require.True(t, cityRealtimeCharacterCaseEvidenceEventValid(expired))
	require.Equal(t, cityRealtimeCharacterCaseEvidenceExpired, next.EvidenceStatus)
	require.Equal(t, cityRealtimeCharacterCaseEvidenceExpiredEvent, expired.EventType)
	require.Equal(t, captured.EventHash, expired.PreviousEventHash)
	require.Equal(t, law.EventHash, next.SourceLawEventHash)

	state := &cityRealtimeCharacterCaseEvidenceHashState{
		SchemaVersion: cityRealtimeCharacterCaseEvidenceSchemaVersion,
		Binding: &cityRealtimeCharacterCaseEvidenceBinding{
			SchemaVersion:    cityRealtimeCharacterCaseEvidenceSchemaVersion,
			AgentBindingHash: strings.Repeat("b", 64),
		},
		Heads: []cityRealtimeCharacterCaseEvidenceHead{next},
	}
	state.Binding.BindingHash = cityRealtimeCharacterCaseEvidenceBindingHash(state.Binding.AgentBindingHash)
	require.NoError(t, validateCityRealtimeCharacterCaseEvidenceHashState(state))
}

func TestCityRealtimeCharacterCaseEvidenceRejectsMutatedLawSource(t *testing.T) {
	law := cityRealtimeCharacterLawEventRecord{
		ActorCode:              "character.player.0123456789abcdef0123456789abcdef",
		EventSequence:          1,
		ActivityEventSequence:  1,
		FrameSequence:          3,
		CaseCode:               "law.0123456789abcdef.1",
		RuleCode:               "rule.public_disruption",
		Disposition:            "fine",
		PenaltyCityCreditUnits: 12,
		StandingDeltaMilli:     -140,
		PublicVisibility:       true,
		PreviousEventHash:      strings.Repeat("c", 64),
	}
	var err error
	law.EventHash, err = cityRealtimeCharacterLawEventHash(law)
	require.NoError(t, err)
	law.PenaltyCityCreditUnits = 13
	require.False(t, cityRealtimeCharacterCaseEvidenceLawSourceValid(law))
	_, _, err = cityRealtimeCharacterCaptureCaseEvidenceFromLaw(law, 30*cityRealtimeTimeQuantumUS)
	require.ErrorIs(t, err, ErrCityInvalidInput)
}
