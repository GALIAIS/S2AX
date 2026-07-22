package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldCommutePolicy(t *testing.T) CityOpenWorldCommutePolicy {
	t.Helper()
	hash, err := cityOpenWorldCommutePolicyHash(
		cityOpenWorldCommuteAssignmentContract,
		cityOpenWorldCommutePeriodTicks,
		cityOpenWorldCommuteMaximumBindings,
	)
	require.NoError(t, err)
	return CityOpenWorldCommutePolicy{
		ProfileID:          cityOpenWorldCommuteProfileID,
		ProfileVersion:     cityOpenWorldCommuteProfileVersion,
		ContentHash:        hash,
		BaselineTick:       0,
		AssignmentContract: cityOpenWorldCommuteAssignmentContract,
		PeriodTicks:        cityOpenWorldCommutePeriodTicks,
		MaximumBindings:    cityOpenWorldCommuteMaximumBindings,
		CandidateCount:     1,
		BindingCount:       1,
		ResidenceCount:     1,
		UsedResidenceUnits: 1,
		Revision:           1,
		Metadata:           json.RawMessage(`{}`),
	}
}

func newValidCityOpenWorldCommuteBinding() CityOpenWorldCommuteBinding {
	return CityOpenWorldCommuteBinding{
		Code:             "commute.binding.test",
		BindingKind:      cityOpenWorldCommuteBindingKindNPCResidenceWork,
		ActorCode:        "npc.1",
		EmploymentRole:   "employment.worker",
		HomeFacilityCode: "facility.home",
		HomeHubCode:      "hub.home",
		WorkFacilityCode: "facility.work",
		WorkHubCode:      "hub.work",
		PeriodTicks:      cityOpenWorldCommutePeriodTicks,
		OutboundPhase:    5,
		ReturnPhase:      17,
		Status:           cityOpenWorldCommuteBindingStatusActive,
		Version:          1,
		Metadata:         json.RawMessage(`{}`),
	}
}

func TestCityOpenWorldCommuteStatePinsCapacityBoundResidenceEmploymentContract(t *testing.T) {
	state := &CityOpenWorldCommuteState{
		Policy:   newValidCityOpenWorldCommutePolicy(t),
		Bindings: []CityOpenWorldCommuteBinding{newValidCityOpenWorldCommuteBinding()},
	}
	require.NoError(t, validateCityOpenWorldCommuteState(state))

	state.Bindings[0].ReturnPhase = 6
	require.Error(t, validateCityOpenWorldCommuteState(state))
}

func TestCityOpenWorldCommuteAssignmentIsDeterministicAndExplicitlyUnbound(t *testing.T) {
	candidates := []cityOpenWorldCommuteCandidate{
		{actorCode: "npc.alpha", employmentRole: "employment.worker", workFacilityCode: "facility.work", workHubCode: "hub.work", scheduleOffset: 3},
		{actorCode: "npc.beta", employmentRole: "employment.worker", workFacilityCode: "facility.work", workHubCode: "hub.work", scheduleOffset: 7},
		{actorCode: "npc.gamma", employmentRole: "employment.worker", workFacilityCode: "facility.work", workHubCode: "hub.work", scheduleOffset: 11},
	}
	newResidences := func() []cityOpenWorldCommuteResidence {
		return []cityOpenWorldCommuteResidence{
			{facilityCode: "facility.home.a", hubCode: "hub.home.a", capacity: 1},
			{facilityCode: "facility.home.b", hubCode: "hub.home.b", capacity: 1},
		}
	}

	first, firstUnbound := cityOpenWorldCommuteBindingsForCandidates(candidates, newResidences())
	second, secondUnbound := cityOpenWorldCommuteBindingsForCandidates(candidates, newResidences())
	require.Equal(t, int64(1), firstUnbound)
	require.Equal(t, firstUnbound, secondUnbound)
	require.Equal(t, first, second)
	require.Len(t, first, 2)
	require.NotEqual(t, first[0].HomeFacilityCode, first[1].HomeFacilityCode)
	for _, binding := range first {
		require.Equal(t, cityOpenWorldCommutePeriodTicks, binding.PeriodTicks)
		require.Equal(t, (binding.OutboundPhase+cityOpenWorldCommutePeriodTicks/2)%cityOpenWorldCommutePeriodTicks, binding.ReturnPhase)
	}
}

func TestCityOpenWorldCommuteBindingCodeIsStableAndBounded(t *testing.T) {
	first := cityOpenWorldCommuteBindingCode("npc.test")
	require.Equal(t, first, cityOpenWorldCommuteBindingCode("npc.test"))
	require.NotEqual(t, first, cityOpenWorldCommuteBindingCode("npc.other"))
	require.True(t, strings.HasPrefix(first, "commute.binding."))
	require.True(t, worldRuntimeCodeValid(first, 160))
}
