package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterCaseReviewIsProceduralAndAppendOnly(t *testing.T) {
	item := cityRealtimeCharacterReviewableLawCase{
		ActorCode:             "character.player.0123456789abcdef0123456789abcdef",
		CaseCode:              "law.0123456789abcdef.3",
		LawEventSequence:      3,
		LawEventHash:          strings.Repeat("a", 64),
		ResponseEventSequence: 2,
		ResponseFrameSequence: 24,
		ResponseEventHash:     strings.Repeat("b", 64),
	}
	head, err := newCityRealtimeCharacterCaseReviewGenesisHead(item)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseReviewHeadValid(head))
	require.Equal(t, int64(0), head.ReviewRevision)
	require.Equal(t, cityRealtimeCharacterCaseReviewNone, head.ReviewStatus)
	require.Equal(t, item.ResponseFrameSequence, head.LastFrameSequence)

	dueWorldTimeUS, err := cityRealtimeCharacterCaseReviewResolutionDueWorldTime(61 * cityRealtimeTimeQuantumUS)
	require.NoError(t, err)
	require.Equal(t, int64(91)*cityRealtimeTimeQuantumUS, dueWorldTimeUS)
	filedHead, filedEvent, err := cityRealtimeCharacterFileCaseReview(
		head, item, "ait.case.review", 25, dueWorldTimeUS,
	)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseReviewEventValid(filedEvent))
	require.Equal(t, head.EventChainHash, filedEvent.PreviousEventHash)
	require.Equal(t, int64(1), filedEvent.EventSequence)
	require.Equal(t, cityRealtimeCharacterCaseReviewFiled, filedHead.ReviewStatus)
	require.Equal(t, item.LawEventHash, filedHead.LawEventHash)
	require.Equal(t, item.ResponseEventHash, filedHead.ResponseEventHash)

	closedHead, closedEvent, err := cityRealtimeCharacterCloseCaseReview(filedHead, 26, dueWorldTimeUS)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseReviewEventValid(closedEvent))
	require.Equal(t, filedHead.EventChainHash, closedEvent.PreviousEventHash)
	require.Equal(t, int64(2), closedEvent.EventSequence)
	require.Equal(t, cityRealtimeCharacterCaseReviewClosedNoChange, closedHead.ReviewStatus)
	require.Equal(t, filedHead.LawEventSequence, closedHead.LawEventSequence)
	require.Equal(t, filedHead.LawEventHash, closedHead.LawEventHash)
	require.Equal(t, filedHead.ResponseEventSequence, closedHead.ResponseEventSequence)
	require.Equal(t, filedHead.ResponseEventHash, closedHead.ResponseEventHash)
	require.Equal(t, filedHead.ResolutionDueWorldTimeUS, closedHead.ResolutionDueWorldTimeUS)

	_, _, err = cityRealtimeCharacterFileCaseReview(closedHead, item, "ait.case.review", 27, dueWorldTimeUS)
	require.ErrorIs(t, err, ErrCityInvalidInput)
	_, _, err = cityRealtimeCharacterCloseCaseReview(filedHead, filedHead.LastFrameSequence, dueWorldTimeUS)
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestCityRealtimeCharacterCaseReviewBindingAndHashStateAreStrict(t *testing.T) {
	binding := cityRealtimeCharacterCaseReviewBinding{
		SchemaVersion:    cityRealtimeCharacterCaseReviewSchemaVersion,
		AgentBindingHash: strings.Repeat("c", 64),
	}
	binding.BindingHash = cityRealtimeCharacterCaseReviewBindingHash(binding.AgentBindingHash)
	require.True(t, validateCityRealtimeCharacterCaseReviewBinding(binding))

	item := cityRealtimeCharacterReviewableLawCase{
		ActorCode:             "character.player.0123456789abcdef0123456789abcdef",
		CaseCode:              "law.0123456789abcdef.5",
		LawEventSequence:      5,
		LawEventHash:          strings.Repeat("d", 64),
		ResponseEventSequence: 4,
		ResponseFrameSequence: 31,
		ResponseEventHash:     strings.Repeat("e", 64),
	}
	head, err := newCityRealtimeCharacterCaseReviewGenesisHead(item)
	require.NoError(t, err)
	state := &cityRealtimeCharacterCaseReviewHashState{
		SchemaVersion: cityRealtimeCharacterCaseReviewSchemaVersion,
		Binding:       &binding,
		Heads:         []cityRealtimeCharacterCaseReviewHead{head},
	}
	require.NoError(t, validateCityRealtimeCharacterCaseReviewHashState(state))

	state.Heads[0].StateHash = strings.Repeat("0", 64)
	require.Error(t, validateCityRealtimeCharacterCaseReviewHashState(state))
}

func TestCityRealtimeCharacterCaseReviewArgumentsRejectFreeFormValues(t *testing.T) {
	code, err := cityRealtimeAgentDecisionCaseReviewCodeFromArguments(map[string]any{
		"case_code": "law.0123456789abcdef.1",
	})
	require.NoError(t, err)
	require.Equal(t, "law.0123456789abcdef.1", code)
	for _, arguments := range []map[string]any{
		{},
		{"case_code": "law.0123456789abcdef.1", "outcome": "reverse"},
		{"case_code": "law.0123456789abcdef.0"},
		{"case_code": 42},
	} {
		_, err = cityRealtimeAgentDecisionCaseReviewCodeFromArguments(arguments)
		require.ErrorIs(t, err, ErrCityRealtimeAgentDecisionUnavailable)
	}
}

func TestCityRealtimeCharacterCaseReviewCloseKeysAreDeterministic(t *testing.T) {
	actorCode := "character.player.0123456789abcdef0123456789abcdef"
	caseCode := "law.0123456789abcdef.1"
	dedupKey, err := cityRealtimeCharacterCaseReviewCloseDedupKey(actorCode, caseCode, "ait.case.review")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(dedupKey, "case-review-close."))
	require.Len(t, dedupKey, len("case-review-close.")+64)
	aggregateKey, err := cityRealtimeCharacterCaseReviewAggregateKey(actorCode, caseCode)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(aggregateKey, "case-review:"))
	require.Len(t, aggregateKey, len("case-review:")+64)
	_, err = cityRealtimeCharacterCaseReviewCloseDedupKey(actorCode, "case.untrusted", "ait.case.review")
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestCityRealtimeCharacterCaseReviewCursorAndProjectionAreBounded(t *testing.T) {
	cursor := cityRealtimeCharacterCaseReviewCursor{
		LastFrameSequence: 42,
		CaseCode:          "law.0123456789abcdef.3",
	}
	parsed, err := parseCityRealtimeCharacterCaseReviewCursor(cursor.String())
	require.NoError(t, err)
	require.Equal(t, cursor, parsed)
	for _, value := range []string{
		"0|law.0123456789abcdef.3",
		"42|law.0123456789abcdef.0",
		"42|case.0123456789abcdef.3",
		"42|law.0123456789abcdef.3|extra",
	} {
		_, err = parseCityRealtimeCharacterCaseReviewCursor(value)
		require.ErrorIs(t, err, ErrCityInvalidInput)
	}

	item := CityRealtimeCharacterCaseReview{
		CaseCode:                 "law.0123456789abcdef.3",
		RuleCode:                 "rule.public_disruption",
		Disposition:              "fine",
		PenaltyCityCreditUnits:   12,
		ReviewRevision:           2,
		ReviewStatus:             cityRealtimeCharacterCaseReviewClosedNoChange,
		FiledFrameSequence:       40,
		ResolutionDueWorldTimeUS: 90 * cityRealtimeTimeQuantumUS,
		LastFrameSequence:        42,
	}
	require.NoError(t, validateCityRealtimeCharacterCaseReviewProjection(item))
	item.ReviewStatus = cityRealtimeCharacterCaseReviewFiled
	require.Error(t, validateCityRealtimeCharacterCaseReviewProjection(item))
}
