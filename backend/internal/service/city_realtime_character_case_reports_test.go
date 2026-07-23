package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterCaseReportIsOneTimeAndNonEvidentiary(t *testing.T) {
	reporter := "character.player.0123456789abcdef0123456789abcdef"
	subject := "npc.citizen.0123456789abcdef0123456789abcdef"
	intent := "ait.case-report." + strings.Repeat("a", 64)
	head, event, err := cityRealtimeCharacterFileCaseReport(reporter, subject, intent, 17)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterCaseReportHeadValid(head))
	require.True(t, cityRealtimeCharacterCaseReportEventValid(event))
	require.Equal(t, int64(1), head.ReportRevision)
	require.Equal(t, cityRealtimeCharacterCaseReportFiledUnverified, head.ReportStatus)
	require.Equal(t, int64(17), head.FiledFrameSequence)
	require.Equal(t, int64(17), head.LastFrameSequence)
	require.Equal(t, event.EventHash, head.EventChainHash)
	require.Equal(t, cityRealtimeCharacterCaseReportFiledUnverified, event.EventType)

	genesis, err := cityRealtimeCharacterCaseReportChainGenesisHash(reporter, subject)
	require.NoError(t, err)
	require.Equal(t, genesis, event.PreviousEventHash)

	_, _, err = cityRealtimeCharacterFileCaseReport(reporter, reporter, intent, 18)
	require.ErrorIs(t, err, ErrCityInvalidInput)
	_, _, err = cityRealtimeCharacterFileCaseReport(reporter, subject, intent, 0)
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestCityRealtimeCharacterCaseReportMustUsePublishedAdjacentTarget(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	binding.PolicyVersion = cityRealtimeAgentCorePolicyVersionReport
	binding.BindingHash = cityRealtimeAgentBindingHash(binding)
	reporter := "character.player.0123456789abcdef0123456789abcdef"
	subject := "npc.citizen.0123456789abcdef0123456789abcdef"
	agent := cityRealtimeAgentInstance{
		AgentCode:       "agent.user.0123456789abcdef0123456789abcdef",
		AgentSubtype:    "character.user",
		LifecycleStatus: "active",
		ControlMode:     "autonomous",
		ActorCode:       &reporter,
	}
	contextPayload := cityRealtimeAgentDecisionActionContext{
		SchemaVersion:            4,
		AvailableActivityCodes:   []string{},
		AvailableMoveTargets:     []cityRealtimeAgentMoveTarget{},
		AvailablePortalCodes:     []string{},
		AvailableRoleCodes:       []string{},
		AvailableCaseCodes:       []string{},
		AvailableSocialTargets:   []string{subject},
		AvailableCaseReviewCodes: []string{},
	}
	allowed, available := cityRealtimeAgentDecisionAllowedActions(binding, agent)
	require.True(t, available)
	payload, err := json.Marshal(map[string]any{
		"allowed_actions": allowed,
		"character":       map[string]any{"action_context": contextPayload},
	})
	require.NoError(t, err)
	observation := cityRealtimeAgentObservationRecord{Payload: payload}
	require.NoError(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionCaseReport,
		map[string]any{"target_actor_code": subject},
	))
	require.ErrorIs(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionCaseReport,
		map[string]any{"target_actor_code": "npc.other.0123456789abcdef0123456789abcdef"},
	), ErrCityRealtimeAgentDecisionUnavailable)

	for _, arguments := range []map[string]any{
		{},
		{"target_actor_code": subject, "reason": "unsealed"},
		{"target_actor_code": 7},
	} {
		_, parseErr := cityRealtimeAgentDecisionCaseReportTargetCodeFromArguments(arguments)
		require.ErrorIs(t, parseErr, ErrCityRealtimeAgentDecisionUnavailable)
	}
}

func TestCityRealtimeCharacterCaseReportCursorIsStrict(t *testing.T) {
	cursor := cityRealtimeCharacterCaseReportCursor{
		FrameSequence:    23,
		SubjectActorCode: "npc.citizen.0123456789abcdef0123456789abcdef",
	}
	parsed, err := parseCityRealtimeCharacterCaseReportCursor(cursor.String())
	require.NoError(t, err)
	require.Equal(t, cursor, parsed)
	for _, value := range []string{
		"0|npc.citizen.0123456789abcdef0123456789abcdef",
		"23",
		"23|NPC.citizen.0123456789abcdef0123456789abcdef",
		"23|npc.citizen.0123456789abcdef0123456789abcdef|extra",
	} {
		_, parseErr := parseCityRealtimeCharacterCaseReportCursor(value)
		require.ErrorIs(t, parseErr, ErrCityInvalidInput)
	}
}
