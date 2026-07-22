package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldCommuteSourcePolicy(t *testing.T) CityOpenWorldCommuteSourcePolicy {
	t.Helper()
	hash, err := cityOpenWorldCommuteSourcePolicyHash(
		cityOpenWorldCommuteSourceGenerationContract,
		cityOpenWorldCommuteSourceOriginContract,
		cityOpenWorldCommutePeriodTicks,
		cityOpenWorldCommuteSourceSurfaceEgressRadius,
		cityOpenWorldCommuteSourceMaximumGenerationsTick,
	)
	require.NoError(t, err)
	return CityOpenWorldCommuteSourcePolicy{
		ProfileID:              cityOpenWorldCommuteSourceProfileID,
		ProfileVersion:         cityOpenWorldCommuteSourceProfileVersion,
		ContentHash:            hash,
		BaselineTick:           0,
		GenerationContract:     cityOpenWorldCommuteSourceGenerationContract,
		OriginContract:         cityOpenWorldCommuteSourceOriginContract,
		PeriodTicks:            cityOpenWorldCommutePeriodTicks,
		SurfaceEgressRadius:    cityOpenWorldCommuteSourceSurfaceEgressRadius,
		MaximumGenerationsTick: cityOpenWorldCommuteSourceMaximumGenerationsTick,
		Revision:               1,
		Metadata:               json.RawMessage(`{}`),
	}
}

func TestCityOpenWorldCommuteSourceStatePinsBidirectionalBindingContract(t *testing.T) {
	seed := cityOpenWorldCommuteSourceSeed{
		actorID: 1, bindingCode: "commute.binding.test", actorCode: "npc.1",
		employmentRoleCode: "employment.worker", homeFacilityCode: "facility.home",
		homeHubCode: "hub.home", workFacilityCode: "facility.work", workHubCode: "hub.work",
		periodTicks: 24, outboundPhase: 4, returnPhase: 16,
	}
	sources, err := cityOpenWorldCommuteSourcesForSeeds([]cityOpenWorldCommuteSourceSeed{seed}, 0)
	require.NoError(t, err)
	policy := newValidCityOpenWorldCommuteSourcePolicy(t)
	policy.SourceCount = int64(len(sources))
	state := &CityOpenWorldCommuteSourceState{Policy: policy, Sources: sources, Metrics: []CityOpenWorldCommuteSourceCycleMetric{}}
	require.NoError(t, validateCityOpenWorldCommuteSourceState(state))
	require.Len(t, state.Sources, 2)
	directions := map[string]bool{}
	for _, source := range state.Sources {
		directions[source.Direction] = true
	}
	require.True(t, directions[cityOpenWorldCommuteSourceDirectionOutbound])
	require.True(t, directions[cityOpenWorldCommuteSourceDirectionReturn])

	state.Sources[0].Direction = "invalid"
	require.Error(t, validateCityOpenWorldCommuteSourceState(state))
}

func TestCityOpenWorldCommuteSourceFacilityPresenceAcceptsInteriorOrBoundedSurfaceEgress(t *testing.T) {
	facility := cityOpenWorldCommuteSourceFacility{
		Code: "facility.home", HubCode: "hub.home", BuildingCode: "building.home",
		FacilityTypeCode: "residence", AnchorX: 100, AnchorY: 200, AnchorZ: 0,
	}
	buildingCode := "building.home"
	interior := CityOpenWorldActorLocation{
		ActorCode: "npc.1", SpaceKind: "interior", LocationScope: "building",
		BuildingCode: &buildingCode, Z: 3,
	}
	require.True(t, cityOpenWorldCommuteSourceLocationAtFacility(interior, facility, 24))

	surface := CityOpenWorldActorLocation{
		ActorCode: "npc.1", SpaceKind: "surface", LocationScope: "surface", X: 124, Y: 176, Z: 0,
	}
	require.True(t, cityOpenWorldCommuteSourceLocationAtFacility(surface, facility, 24))
	surface.X = 125
	require.False(t, cityOpenWorldCommuteSourceLocationAtFacility(surface, facility, 24))

	wrongBuilding := interior
	wrongCode := "building.other"
	wrongBuilding.BuildingCode = &wrongCode
	require.False(t, cityOpenWorldCommuteSourceLocationAtFacility(wrongBuilding, facility, 24))
}

func TestCityOpenWorldCommuteSourceCycleWindowRequiresCompletedPriorPeriod(t *testing.T) {
	policy := newValidCityOpenWorldCommuteSourcePolicy(t)
	_, _, due := cityOpenWorldCommuteSourceCycleWindow(policy, 24)
	require.False(t, due)
	start, end, due := cityOpenWorldCommuteSourceCycleWindow(policy, 25)
	require.True(t, due)
	require.Equal(t, int64(1), start)
	require.Equal(t, int64(24), end)
}
