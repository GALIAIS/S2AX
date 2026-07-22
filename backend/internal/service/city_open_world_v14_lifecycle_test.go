package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldCommuteLifecyclePolicy(t *testing.T) CityOpenWorldCommuteLifecyclePolicy {
	t.Helper()
	hash, err := cityOpenWorldCommuteLifecyclePolicyHash()
	require.NoError(t, err)
	return CityOpenWorldCommuteLifecyclePolicy{
		ProfileID:              cityOpenWorldCommuteLifecycleProfileID,
		ProfileVersion:         cityOpenWorldCommuteLifecycleProfileVersion,
		ContentHash:            hash,
		BaselineTick:           0,
		AssignmentContract:     cityOpenWorldCommuteLifecycleAssignmentContract,
		SourceContract:         cityOpenWorldCommuteLifecycleSourceContract,
		PeriodTicks:            cityOpenWorldCommutePeriodTicks,
		MaximumAssignments:     cityOpenWorldCommuteLifecycleMaximumAssignments,
		MaximumTransitionsTick: cityOpenWorldCommuteLifecycleMaximumTransitionsTick,
		MaximumGenerationsTick: cityOpenWorldCommuteLifecycleMaximumGenerationsTick,
		Revision:               1,
		Metadata:               json.RawMessage(`{}`),
	}
}

func newValidCityOpenWorldCommuteLifecycleSeed() cityOpenWorldCommuteLifecycleSeed {
	return cityOpenWorldCommuteLifecycleSeed{
		actorID:          1,
		bindingCode:      "commute.binding.test",
		actorCode:        "npc.1",
		employmentRole:   "employment.worker",
		homeFacilityCode: "facility.home",
		homeHubCode:      "hub.home",
		workFacilityCode: "facility.work",
		workHubCode:      "hub.work",
		periodTicks:      cityOpenWorldCommutePeriodTicks,
		outboundPhase:    4,
		returnPhase:      16,
	}
}

func newValidCityOpenWorldCommuteLifecycleBaseline(t *testing.T) *CityOpenWorldCommuteLifecycleState {
	t.Helper()
	seed := newValidCityOpenWorldCommuteLifecycleSeed()
	assignment := cityOpenWorldCommuteLifecycleAssignmentForSeed(seed, 0)
	policy := newValidCityOpenWorldCommuteLifecyclePolicy(t)
	policy.AssignmentCount = 1
	policy.ActiveAssignmentCount = 1
	policy.SourceCount = 2
	policy.TransitionCount = 1
	return &CityOpenWorldCommuteLifecycleState{
		Policy:      policy,
		Assignments: []CityOpenWorldCommuteAssignmentEpoch{assignment},
		Transitions: []CityOpenWorldCommuteAssignmentTransition{{
			AssignmentCode: assignment.Code,
			TransitionTick: 0,
			TransitionSeq:  0,
			State:          cityOpenWorldCommuteLifecycleStateActive,
			ReasonCode:     cityOpenWorldCommuteLifecycleReasonBaseline,
			Metadata:       json.RawMessage(`{"schema_version":1,"origin":"baseline"}`),
		}},
		Sources: cityOpenWorldCommuteLifecycleSourcesForAssignment(assignment, 0),
		Metrics: []CityOpenWorldCommuteLifecycleCycleMetric{},
	}
}

func TestCityOpenWorldCommuteLifecycleStatePinsSuccessorEpochHistory(t *testing.T) {
	state := newValidCityOpenWorldCommuteLifecycleBaseline(t)
	require.NoError(t, validateCityOpenWorldCommuteLifecycleState(state))

	predecessor := state.Assignments[0]
	openingFact := &CityOpenWorldRuntimeFactRef{Tick: 6, Sequence: 3}
	successor := predecessor
	successor.EpochNumber = 2
	successor.Code = cityOpenWorldCommuteAssignmentEpochCode(successor.BindingCode, successor.EpochNumber)
	successor.OriginKind = cityOpenWorldCommuteLifecycleOriginAdminRebind
	successor.OpenedTick = openingFact.Tick
	successor.OpenedFact = openingFact
	successor.Metadata = json.RawMessage(`{"schema_version":1,"origin":"admin_rebind"}`)

	state.Assignments = append(state.Assignments, successor)
	state.Transitions = append(state.Transitions,
		CityOpenWorldCommuteAssignmentTransition{
			AssignmentCode: predecessor.Code,
			TransitionTick: openingFact.Tick,
			TransitionSeq:  openingFact.Sequence,
			State:          cityOpenWorldCommuteLifecycleStateSuperseded,
			ReasonCode:     cityOpenWorldCommuteLifecycleReasonAdminRebind,
			SourceFact:     openingFact,
			Metadata:       json.RawMessage(`{"schema_version":1,"previous_state":"active"}`),
		},
		CityOpenWorldCommuteAssignmentTransition{
			AssignmentCode: successor.Code,
			TransitionTick: openingFact.Tick,
			TransitionSeq:  openingFact.Sequence,
			State:          cityOpenWorldCommuteLifecycleStateActive,
			ReasonCode:     cityOpenWorldCommuteLifecycleReasonAdminRebind,
			SourceFact:     openingFact,
			Metadata:       json.RawMessage(`{"schema_version":1,"previous_state":""}`),
		},
	)
	successorSources := cityOpenWorldCommuteLifecycleSourcesForAssignment(successor, openingFact.Tick)
	for index := range successorSources {
		successorSources[index].LastFact = openingFact
	}
	state.Sources = append(state.Sources, successorSources...)
	state.Policy.AssignmentCount = 2
	state.Policy.ActiveAssignmentCount = 1
	state.Policy.SupersededAssignmentCount = 1
	state.Policy.SourceCount = 4
	state.Policy.TransitionCount = 3

	sortCityOpenWorldCommuteLifecycleState(state)
	require.NoError(t, validateCityOpenWorldCommuteLifecycleState(state))

	state.Assignments[1].OpenedFact = nil
	require.Error(t, validateCityOpenWorldCommuteLifecycleState(state))
}

func TestCityOpenWorldCommuteLifecycleStateRejectsInvalidEpochSourcePair(t *testing.T) {
	state := newValidCityOpenWorldCommuteLifecycleBaseline(t)
	state.Sources[0].Code = "commute.lifecycle.source.invalid"
	require.Error(t, validateCityOpenWorldCommuteLifecycleState(state))
}

func TestCityOpenWorldCommuteLifecycleCycleWindowUsesPriorCompletedPeriod(t *testing.T) {
	policy := newValidCityOpenWorldCommuteLifecyclePolicy(t)
	policy.BaselineTick = 7
	_, _, due := cityOpenWorldCommuteLifecycleCycleWindow(policy, 31)
	require.False(t, due)
	start, end, due := cityOpenWorldCommuteLifecycleCycleWindow(policy, 32)
	require.True(t, due)
	require.Equal(t, int64(8), start)
	require.Equal(t, int64(31), end)
}

func TestNormalizeCityOpenWorldCommuteLifecycleCommands(t *testing.T) {
	rebind, handled, err := normalizeCityOpenWorldRuntimeCommand(
		CityCommandTypeOpenWorldCommuteAssignmentRebind,
		json.RawMessage(`{"actor_code":"NPC.1","employment_role_code":"Employment.Worker","home_facility_code":"Facility.Home","work_facility_code":"Facility.Work"}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	rebindPayload, ok := rebind.(cityOpenWorldCommuteAssignmentRebindPayload)
	require.True(t, ok)
	require.Equal(t, "npc.1", rebindPayload.ActorCode)
	require.Equal(t, cityOpenWorldCommuteLifecycleReasonAdminRebind, rebindPayload.ReasonCode)

	state, handled, err := normalizeCityOpenWorldRuntimeCommand(
		CityCommandTypeOpenWorldCommuteAssignmentSetState,
		json.RawMessage(`{"actor_code":"npc.1","state":"SUSPENDED"}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	statePayload, ok := state.(cityOpenWorldCommuteAssignmentSetStatePayload)
	require.True(t, ok)
	require.Equal(t, cityOpenWorldCommuteLifecycleReasonAdminSuspended, statePayload.ReasonCode)

	_, _, err = normalizeCityOpenWorldRuntimeCommand(
		CityCommandTypeOpenWorldCommuteAssignmentRebind,
		json.RawMessage(`{"actor_code":"npc.1","employment_role_code":"employment.worker","home_facility_code":"facility.same","work_facility_code":"facility.same"}`),
	)
	require.Error(t, err)
}
