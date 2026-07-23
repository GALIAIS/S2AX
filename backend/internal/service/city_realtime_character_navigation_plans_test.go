package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterNavigationPlanTransitionsRemainFiniteAndSealed(t *testing.T) {
	actor := "character.player.0123456789abcdef0123456789abcdef"
	state := cityRealtimeActorState{
		ActorCode:         actor,
		X:                 100,
		Y:                 200,
		Z:                 0,
		MotionState:       "idle",
		PositionRevision:  1,
		LastFrameSequence: 9,
		EventChainHash:    strings.Repeat("a", 64),
	}
	require.True(t, cityRealtimeActorStateValid(state))
	destination := cityRealtimeCharacterNavigationDestination{
		PortalCode: "portal.market.east",
		Target:     cityRealtimeActorSpawnCandidate{X: 102, Y: 200, Z: 0},
		PathLength: 2,
	}
	require.True(t, cityRealtimeDueEventIdentifierValid(destination.PortalCode, 128))
	head, planned, err := cityRealtimeCharacterNavigationPlanNew(
		state, destination, "intent.navigation.market", 10, 10*cityRealtimeTimeQuantumUS,
	)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterNavigationPlanHeadValid(head))
	require.True(t, cityRealtimeCharacterNavigationPlanEventValid(planned))
	require.Equal(t, cityRealtimeCharacterNavigationPlanActive, head.PlanStatus)
	require.Equal(t, int64(0), head.StepsCompleted)
	require.Equal(t, planned.EventHash, head.EventChainHash)

	stepped, stepEvent, err := cityRealtimeCharacterNavigationPlanAdvance(
		head, 11, 11*cityRealtimeTimeQuantumUS,
		cityRealtimeActorSpawnCandidate{X: 100, Y: 200, Z: 0},
		cityRealtimeActorSpawnCandidate{X: 101, Y: 200, Z: 0},
		strings.Repeat("b", 64), "", "",
	)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterNavigationPlanHeadValid(stepped))
	require.Equal(t, cityRealtimeCharacterNavigationPlanActive, stepped.PlanStatus)
	require.Equal(t, int64(1), stepped.StepsCompleted)
	require.Equal(t, cityRealtimeCharacterNavigationPlanEventStep, stepEvent.EventType)

	arrived, arrivedEvent, err := cityRealtimeCharacterNavigationPlanAdvance(
		stepped, 12, 12*cityRealtimeTimeQuantumUS,
		cityRealtimeActorSpawnCandidate{X: 101, Y: 200, Z: 0},
		cityRealtimeActorSpawnCandidate{X: 102, Y: 200, Z: 0},
		strings.Repeat("c", 64), "", "",
	)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterNavigationPlanHeadValid(arrived))
	require.Equal(t, cityRealtimeCharacterNavigationPlanArrived, arrived.PlanStatus)
	require.Nil(t, arrived.NextDueWorldTimeUS)
	require.Equal(t, cityRealtimeCharacterNavigationPlanEventArrived, arrivedEvent.EventType)

	cancelled, cancelledEvent, err := cityRealtimeCharacterNavigationPlanCancel(
		head, 12, 10*cityRealtimeTimeQuantumUS,
		cityRealtimeActorSpawnCandidate{X: 100, Y: 200, Z: 0},
	)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterNavigationPlanHeadValid(cancelled))
	require.Equal(t, cityRealtimeCharacterNavigationPlanCancelled, cancelled.PlanStatus)
	require.Equal(t, cityRealtimeCharacterNavigationPlanEventCancelled, cancelledEvent.EventType)
	require.Empty(t, cancelledEvent.ActorPositionEventHash)

	binding := cityRealtimeCharacterNavigationPlanBinding{
		SchemaVersion:      cityRealtimeCharacterNavigationPlanSchemaVersion,
		AgentBindingHash:   strings.Repeat("d", 64),
		SpatialContextHash: strings.Repeat("e", 64),
	}
	binding.BindingHash = cityRealtimeCharacterNavigationPlanBindingHash(binding)
	require.NoError(t, validateCityRealtimeCharacterNavigationPlanHashState(&cityRealtimeCharacterNavigationPlanHashState{
		SchemaVersion: cityRealtimeCharacterNavigationPlanSchemaVersion,
		Binding:       &binding,
		Heads:         []cityRealtimeCharacterNavigationPlanHead{arrived},
	}))
}

func TestCityRealtimeCharacterNavigationPlanPolicyAndActionContextAreVersioned(t *testing.T) {
	contextPayload := cityRealtimeAgentDecisionActionContext{
		SchemaVersion:                             6,
		AvailableActivityCodes:                    []string{},
		AvailableMoveTargets:                      []cityRealtimeAgentMoveTarget{},
		AvailablePortalCodes:                      []string{},
		AvailableRoleCodes:                        []string{},
		AvailableCaseCodes:                        []string{},
		AvailableSocialTargets:                    []string{},
		AvailableCaseReviewCodes:                  []string{},
		AvailableTaskCodes:                        []string{},
		AvailableNavigationDestinationPortalCodes: []string{"portal.market.east", "portal.market.west"},
	}
	require.True(t, cityRealtimeAgentDecisionActionContextValid(contextPayload))
	raw, err := json.Marshal(contextPayload)
	require.NoError(t, err)
	require.Contains(t, string(raw), "\"available_navigation_destination_portal_codes\"")
	var decoded cityRealtimeAgentDecisionActionContext
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.True(t, cityRealtimeAgentDecisionActionContextValid(decoded))

	contextPayload.SchemaVersion = 5
	require.False(t, cityRealtimeAgentDecisionActionContextValid(contextPayload), "v5 must not accept a v6 candidate field")

	actor := "character.player.0123456789abcdef0123456789abcdef"
	agent := cityRealtimeAgentInstance{
		AgentCode:       "agent.user.0123456789abcdef0123456789abcdef",
		AgentSubtype:    "character.user",
		LifecycleStatus: "active",
		ControlMode:     "autonomous",
		ActorCode:       &actor,
	}
	current := cityRealtimeAgentTestBinding()
	current.PolicyVersion = cityRealtimeAgentCorePolicyVersionNavigationPlan
	current.BindingHash = cityRealtimeAgentBindingHash(current)
	actions, available := cityRealtimeAgentDecisionAllowedActions(current, agent)
	require.True(t, available)
	require.Contains(t, actions, cityRealtimeAgentIntentActionNavigation)
	require.True(t, cityRealtimeCharacterNavigationPlanRuntimeEnabled(current))
	require.Equal(t, 6, cityRealtimeAgentDecisionActionContextSchemaVersion(current))

	historical := current
	historical.PolicyVersion = cityRealtimeAgentCorePolicyVersionTask
	historical.BindingHash = cityRealtimeAgentBindingHash(historical)
	historicalActions, historicalAvailable := cityRealtimeAgentDecisionAllowedActions(historical, agent)
	require.True(t, historicalAvailable)
	require.NotContains(t, historicalActions, cityRealtimeAgentIntentActionNavigation)
	require.False(t, cityRealtimeCharacterNavigationPlanRuntimeEnabled(historical))
}
