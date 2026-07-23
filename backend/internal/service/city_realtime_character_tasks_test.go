package service

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterTaskAcceptCompleteAndExpireRemainSealed(t *testing.T) {
	actor := "character.player.0123456789abcdef0123456789abcdef"
	definitions := cityRealtimeCharacterTaskCatalogDefinitions()
	cleanup := definitions[0]
	shift := definitions[1]

	accepted, acceptedEvent, err := cityRealtimeCharacterAcceptTask(
		cleanup, actor, "intent.task.cleanup", 17, 317*cityRealtimeTimeQuantumUS,
	)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterTaskHeadValid(accepted))
	require.True(t, cityRealtimeCharacterTaskEventValid(acceptedEvent))
	require.Equal(t, cityRealtimeCharacterTaskAccepted, accepted.TaskStatus)
	require.Equal(t, acceptedEvent.EventHash, accepted.EventChainHash)
	require.Equal(t, cleanup.ActivityCode, accepted.ActivityCode)

	activity := cityRealtimeCharacterActivityEventRecord{
		ActorCode:     actor,
		EventSequence: 9,
		FrameSequence: 18,
		ActivityCode:  cleanup.ActivityCode,
		EventHash:     strings.Repeat("a", 64),
	}
	completed, completedEvent, err := cityRealtimeCharacterCompleteTask(
		accepted, activity, 18, 18*cityRealtimeTimeQuantumUS,
	)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterTaskHeadValid(completed))
	require.True(t, cityRealtimeCharacterTaskEventValid(completedEvent))
	require.Equal(t, cityRealtimeCharacterTaskCompleted, completed.TaskStatus)
	require.Equal(t, int64(9), completed.CompletionActivityEventSequence)
	require.Equal(t, activity.EventHash, completed.CompletionActivityEventHash)
	require.Equal(t, acceptedEvent.EventHash, completedEvent.PreviousEventHash)

	_, _, err = cityRealtimeCharacterCompleteTask(accepted, activity, 18, accepted.ExpirationDueWorldTimeUS)
	require.ErrorIs(t, err, ErrCityInvalidInput, "the exact deadline belongs to the later expiry reducer")
	wrongActivity := activity
	wrongActivity.ActivityCode = shift.ActivityCode
	_, _, err = cityRealtimeCharacterCompleteTask(accepted, wrongActivity, 18, 18*cityRealtimeTimeQuantumUS)
	require.ErrorIs(t, err, ErrCityInvalidInput)

	expiring, _, err := cityRealtimeCharacterAcceptTask(
		shift, actor, "intent.task.shift", 20, 320*cityRealtimeTimeQuantumUS,
	)
	require.NoError(t, err)
	expired, expiredEvent, err := cityRealtimeCharacterExpireTask(
		expiring, 21, expiring.ExpirationDueWorldTimeUS,
	)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterTaskHeadValid(expired))
	require.True(t, cityRealtimeCharacterTaskEventValid(expiredEvent))
	require.Equal(t, cityRealtimeCharacterTaskExpired, expired.TaskStatus)
	require.Equal(t, expiring.EventChainHash, expiredEvent.PreviousEventHash)

	heads := []cityRealtimeCharacterTaskHead{completed, expired}
	sort.Slice(heads, func(left, right int) bool {
		if heads[left].ActorCode == heads[right].ActorCode {
			return heads[left].TaskRunCode < heads[right].TaskRunCode
		}
		return heads[left].ActorCode < heads[right].ActorCode
	})
	binding := cityRealtimeCharacterTaskBinding{
		SchemaVersion:       cityRealtimeCharacterTaskSchemaVersion,
		AgentBindingHash:    strings.Repeat("b", 64),
		ActivityBindingHash: strings.Repeat("c", 64),
		CatalogID:           cityRealtimeCharacterTaskCatalogID,
		CatalogVersion:      cityRealtimeCharacterTaskCatalogVersion,
		CatalogHash:         strings.Repeat("d", 64),
	}
	binding.BindingHash = cityRealtimeCharacterTaskBindingHash(binding)
	require.NoError(t, validateCityRealtimeCharacterTaskHashState(&cityRealtimeCharacterTaskHashState{
		SchemaVersion: cityRealtimeCharacterTaskSchemaVersion,
		Binding:       &binding,
		Heads:         heads,
	}))
}

func TestCityRealtimeCharacterTaskActionContextAndPolicyAreVersioned(t *testing.T) {
	actor := "character.player.0123456789abcdef0123456789abcdef"
	contextPayload := cityRealtimeAgentDecisionActionContext{
		SchemaVersion:            5,
		AvailableActivityCodes:   []string{"civic.cleanup", "work.civic_shift"},
		AvailableMoveTargets:     []cityRealtimeAgentMoveTarget{},
		AvailablePortalCodes:     []string{},
		AvailableRoleCodes:       []string{},
		AvailableCaseCodes:       []string{},
		AvailableSocialTargets:   []string{},
		AvailableCaseReviewCodes: []string{},
		AvailableTaskCodes:       []string{"task.civic.cleanup", "task.civic.shift"},
	}
	require.True(t, cityRealtimeAgentDecisionActionContextValid(contextPayload))
	raw, err := json.Marshal(contextPayload)
	require.NoError(t, err)
	require.Contains(t, string(raw), "\"available_task_codes\"")
	var decoded cityRealtimeAgentDecisionActionContext
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.True(t, cityRealtimeAgentDecisionActionContextValid(decoded))

	contextPayload.SchemaVersion = 4
	require.False(t, cityRealtimeAgentDecisionActionContextValid(contextPayload))

	agent := cityRealtimeAgentInstance{
		AgentCode:       "agent.user.0123456789abcdef0123456789abcdef",
		AgentSubtype:    "character.user",
		LifecycleStatus: "active",
		ControlMode:     "autonomous",
		ActorCode:       &actor,
	}
	current := cityRealtimeAgentTestBinding()
	current.PolicyVersion = cityRealtimeAgentCorePolicyVersionTask
	current.BindingHash = cityRealtimeAgentBindingHash(current)
	allowed, available := cityRealtimeAgentDecisionAllowedActions(current, agent)
	require.True(t, available)
	require.Contains(t, allowed, cityRealtimeAgentIntentActionTask)
	require.True(t, cityRealtimeAgentCharacterTaskRuntimeEnabled(current))

	historical := current
	historical.PolicyVersion = cityRealtimeAgentCorePolicyVersionProcedureDispatch
	historical.BindingHash = cityRealtimeAgentBindingHash(historical)
	historicalAllowed, historicalAvailable := cityRealtimeAgentDecisionAllowedActions(historical, agent)
	require.True(t, historicalAvailable)
	require.NotContains(t, historicalAllowed, cityRealtimeAgentIntentActionTask)
	require.False(t, cityRealtimeAgentCharacterTaskRuntimeEnabled(historical))
}
