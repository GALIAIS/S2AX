package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterCaseAcknowledgementIsAppendOnlyAndDeterministic(t *testing.T) {
	actorCode := "character.player.0123456789abcdef0123456789abcdef"
	head, err := newCityRealtimeCharacterCaseResponseGenesisHead(actorCode, 7)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseResponseHeadValid(head))
	require.Equal(t, int64(0), head.ResponseRevision)

	lawCase := cityRealtimeCharacterOpenLawCase{
		ActorCode:              actorCode,
		LawEventSequence:       3,
		LawFrameSequence:       21,
		CaseCode:               "law.0123456789abcdef.3",
		RuleCode:               "rule.public_disruption",
		Disposition:            "fine",
		PenaltyCityCreditUnits: 12,
		LawEventHash:           strings.Repeat("a", 64),
	}
	next, event, err := cityRealtimeCharacterAcknowledgeLawCase(head, lawCase, "ait.case.acknowledge", 24)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseResponseEventValid(event))
	require.Equal(t, head.EventChainHash, event.PreviousEventHash)
	require.Equal(t, int64(1), event.EventSequence)
	require.Equal(t, int64(1), next.ResponseRevision)
	require.Equal(t, event.EventHash, next.EventChainHash)
	require.True(t, cityRealtimeCharacterCaseResponseHeadValid(next))

	_, _, err = cityRealtimeCharacterAcknowledgeLawCase(next, lawCase, "ait.case.acknowledge", 24)
	require.ErrorIs(t, err, ErrCityInvalidInput, "a response can never share or precede its head frame")
}

func TestCityRealtimeCharacterCaseResponseBindingAndHashStateAreStrict(t *testing.T) {
	binding := cityRealtimeCharacterCaseResponseBinding{
		SchemaVersion:    cityRealtimeCharacterCaseResponseSchemaVersion,
		AgentBindingHash: strings.Repeat("b", 64),
	}
	binding.BindingHash = cityRealtimeCharacterCaseResponseBindingHash(binding.AgentBindingHash)
	require.True(t, validateCityRealtimeCharacterCaseResponseBinding(binding))

	head, err := newCityRealtimeCharacterCaseResponseGenesisHead(
		"character.player.0123456789abcdef0123456789abcdef", 9,
	)
	require.NoError(t, err)
	state := &cityRealtimeCharacterCaseResponseHashState{
		SchemaVersion: cityRealtimeCharacterCaseResponseSchemaVersion,
		Binding:       &binding,
		Heads:         []cityRealtimeCharacterCaseResponseHead{head},
	}
	require.NoError(t, validateCityRealtimeCharacterCaseResponseHashState(state))

	state.Heads[0].StateHash = strings.Repeat("0", 64)
	require.Error(t, validateCityRealtimeCharacterCaseResponseHashState(state))
}

func TestCityRealtimeCharacterCaseCodeAndArgumentsRejectFreeFormValues(t *testing.T) {
	require.True(t, cityRealtimeCharacterLawCaseCodeValid("law.0123456789abcdef.1"))
	for _, value := range []string{
		"law.0123456789abcdef.0",
		"law.0123456789ABCDEf.1",
		"case.0123456789abcdef.1",
		"law.0123456789abcdef.1.extra",
	} {
		require.False(t, cityRealtimeCharacterLawCaseCodeValid(value))
	}
	code, err := cityRealtimeAgentDecisionCaseCodeFromArguments(map[string]any{
		"case_code": "law.0123456789abcdef.1",
	})
	require.NoError(t, err)
	require.Equal(t, "law.0123456789abcdef.1", code)
	_, err = cityRealtimeAgentDecisionCaseCodeFromArguments(map[string]any{
		"case_code": "law.0123456789abcdef.1", "override": true,
	})
	require.ErrorIs(t, err, ErrCityRealtimeAgentDecisionUnavailable)
}
