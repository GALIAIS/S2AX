package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldEffectiveCapacityState(t *testing.T, baselineTick int64) *CityOpenWorldEffectiveCapacityState {
	t.Helper()
	contentHash, err := cityOpenWorldEffectiveCapacityPolicyHash()
	require.NoError(t, err)
	metadata, err := cityOpenWorldEffectiveCapacityPolicyMetadata()
	require.NoError(t, err)
	return &CityOpenWorldEffectiveCapacityState{
		Policy: CityOpenWorldEffectiveCapacityPolicy{
			ProfileID:          cityOpenWorldEffectiveCapacityProfileID,
			ProfileVersion:     cityOpenWorldEffectiveCapacityProfileVersion,
			ContentHash:        contentHash,
			BaselineTick:       baselineTick,
			TopologyContract:   cityOpenWorldEffectiveCapacityTopologyContract,
			AssetContract:      cityOpenWorldEffectiveCapacityAssetContract,
			AdmissionContract:  cityOpenWorldEffectiveCapacityAdmissionContract,
			VisibilityContract: cityOpenWorldEffectiveCapacityVisibilityContract,
			MaximumAdmissions:  cityOpenWorldEffectiveCapacityMaximumAdmissions,
			AdmissionCount:     0,
			Revision:           1,
			Metadata:           metadata,
		},
		Admissions: []CityOpenWorldEffectiveCapacityAdmission{},
	}
}

func TestCityOpenWorldEffectiveCapacityFormulaAndAdmissionMetadata(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		base, milli int64
		expected    int64
		shouldFail  bool
	}{
		{name: "operational", base: 9, milli: 1000, expected: 9},
		{name: "restricted floor", base: 9, milli: 650, expected: 5},
		{name: "fraction rounds down", base: 1, milli: 999, expected: 0},
		{name: "closed", base: 64, milli: 0, expected: 0},
		{name: "invalid milli", base: 64, milli: 1001, shouldFail: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			actual, err := cityOpenWorldEffectiveCapacityUnits(testCase.base, testCase.milli)
			if testCase.shouldFail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.expected, actual)
		})
	}

	edge := CityOpenWorldEffectiveCapacityEdge{
		EdgeCode: "edge.walk.a", CorridorCode: "corridor.walk.a", AssetCode: "infrastructure.asset.segment.a",
		AssetState: cityOpenWorldInfrastructureStateRestricted, StateEffectiveTick: 8,
		StateSourceFact:              &CityOpenWorldRuntimeFactRef{Tick: 8, Sequence: 4},
		BaselineCapacityUnitsPerTick: 9, CapacityMilli: 650, EffectiveCapacityUnitsPerTick: 5,
	}
	raw, err := cityOpenWorldEffectiveCapacityAllocationMetadataFor(edge)
	require.NoError(t, err)
	metadata, marked, err := cityOpenWorldEffectiveCapacityAllocationMetadataFromRaw(raw)
	require.NoError(t, err)
	require.True(t, marked)
	require.Equal(t, edge.CorridorCode, metadata.CorridorCode)
	require.Equal(t, edge.EffectiveCapacityUnitsPerTick, metadata.EffectiveCapacityUnitsPerTick)

	legacy, marked, err := cityOpenWorldEffectiveCapacityAllocationMetadataFromRaw(json.RawMessage(`{"schema_version":1,"allocation_contract":"edge_departure_tick"}`))
	require.NoError(t, err)
	require.False(t, marked)
	require.Equal(t, cityOpenWorldEffectiveCapacityAllocationMetadata{}, legacy)
}

func TestCityOpenWorldEffectiveCapacityUsesFactSequenceForVisibility(t *testing.T) {
	infrastructure := &CityOpenWorldInfrastructureState{Transitions: []CityOpenWorldInfrastructureAssetTransition{
		{
			AssetCode: "infrastructure.asset.segment.a", TransitionTick: 0, TransitionSeq: 0,
			FromState: "", ToState: cityOpenWorldInfrastructureStateOperational, CapacityMilli: 1000,
		},
		{
			AssetCode: "infrastructure.asset.segment.a", TransitionTick: 7, TransitionSeq: 4,
			FromState: cityOpenWorldInfrastructureStateOperational, ToState: cityOpenWorldInfrastructureStateRestricted,
			CapacityMilli: 500, SourceFact: &CityOpenWorldRuntimeFactRef{Tick: 7, Sequence: 4},
		},
	}}

	before, err := cityOpenWorldEffectiveCapacityStateAtSchedule(
		infrastructure, "infrastructure.asset.segment.a", CityOpenWorldRuntimeFactRef{Tick: 7, Sequence: 3},
	)
	require.NoError(t, err)
	require.Equal(t, cityOpenWorldInfrastructureStateOperational, before.State)
	require.Equal(t, int64(1000), before.CapacityMilli)

	after, err := cityOpenWorldEffectiveCapacityStateAtSchedule(
		infrastructure, "infrastructure.asset.segment.a", CityOpenWorldRuntimeFactRef{Tick: 7, Sequence: 5},
	)
	require.NoError(t, err)
	require.Equal(t, cityOpenWorldInfrastructureStateRestricted, after.State)
	require.Equal(t, int64(500), after.CapacityMilli)
	require.Equal(t, &CityOpenWorldRuntimeFactRef{Tick: 7, Sequence: 4}, after.SourceFact)
}

func TestCityOpenWorldEffectiveCapacityRoutingCanSelectAnAlternatePath(t *testing.T) {
	edges := []CityOpenWorldMobilityEdge{
		{Code: "edge.direct", ModeCode: "walk", FromHubCode: "hub.source", ToHubCode: "hub.destination", BaseTravelTicks: 1},
		{Code: "edge.detour.one", ModeCode: "walk", FromHubCode: "hub.source", ToHubCode: "hub.detour", BaseTravelTicks: 2},
		{Code: "edge.detour.two", ModeCode: "walk", FromHubCode: "hub.detour", ToHubCode: "hub.destination", BaseTravelTicks: 2},
	}
	policy := newValidCityOpenWorldEffectiveCapacityState(t, 0).Policy
	state := &cityOpenWorldEffectiveCapacitySchedulingState{
		policy: &policy,
		edges: map[string]CityOpenWorldEffectiveCapacityEdge{
			"edge.direct":     {EdgeCode: "edge.direct", EffectiveCapacityUnitsPerTick: 0},
			"edge.detour.one": {EdgeCode: "edge.detour.one", EffectiveCapacityUnitsPerTick: 5},
			"edge.detour.two": {EdgeCode: "edge.detour.two", EffectiveCapacityUnitsPerTick: 5},
		},
		allocatedByEdge: map[string]int64{},
	}

	path, err := cityOpenWorldMobilityShortestPathEligible(
		edges, "walk", "hub.source", "hub.destination", func(edge CityOpenWorldMobilityEdge) bool {
			return state.edgeAvailable(edge, 1)
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"edge.detour.one", "edge.detour.two"}, cityOpenWorldMobilityPathCodes(path))

	_, _, err = state.reserve(path[0], 5)
	require.NoError(t, err)
	require.False(t, state.edgeAvailable(path[0], 1))
	require.NoError(t, state.release(path[0].Code, 5))
	require.True(t, state.edgeAvailable(path[0], 5))
}
