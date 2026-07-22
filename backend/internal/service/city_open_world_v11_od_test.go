package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldMobilityODPolicy(t *testing.T) CityOpenWorldMobilityODPolicy {
	t.Helper()
	hash, err := cityOpenWorldMobilityODPolicyHash(
		cityOpenWorldMobilityODGenerationContract,
		cityOpenWorldMobilityODMetricContract,
		cityOpenWorldMobilityODCycleTicks,
		cityOpenWorldMobilityODMaximumPerTick,
	)
	require.NoError(t, err)
	return CityOpenWorldMobilityODPolicy{
		ProfileID:              cityOpenWorldMobilityODProfileID,
		ProfileVersion:         cityOpenWorldMobilityODProfileVersion,
		ContentHash:            hash,
		BaselineTick:           0,
		GenerationContract:     cityOpenWorldMobilityODGenerationContract,
		MetricContract:         cityOpenWorldMobilityODMetricContract,
		CycleTicks:             cityOpenWorldMobilityODCycleTicks,
		MaximumGenerationsTick: cityOpenWorldMobilityODMaximumPerTick,
		SourceCount:            1,
		Revision:               1,
		Metadata:               json.RawMessage(`{}`),
	}
}

func newValidCityOpenWorldMobilityODSource() CityOpenWorldMobilityODSource {
	return CityOpenWorldMobilityODSource{
		Code:                    "mobility.od.source.test",
		SourceKind:              cityOpenWorldMobilityODSourceKindNPCWorkVisit,
		ActorCode:               "npc.1",
		DestinationFacilityCode: "facility.test",
		DestinationHubCode:      "hub.facility.test",
		ModeCode:                "walk",
		PurposeCode:             "routine.facility_visit",
		RequestedUnits:          1,
		Status:                  cityOpenWorldMobilityODSourceStatusActive,
		PeriodTicks:             cityOpenWorldMobilityODCycleTicks,
		PhaseOffset:             0,
		NextDueTick:             1,
		LastTransitionTick:      0,
		Version:                 1,
		Metadata:                json.RawMessage(`{}`),
	}
}

func TestCityOpenWorldMobilityODStatePinsVersionedSourceContract(t *testing.T) {
	state := &CityOpenWorldMobilityODState{
		Policy:  newValidCityOpenWorldMobilityODPolicy(t),
		Sources: []CityOpenWorldMobilityODSource{newValidCityOpenWorldMobilityODSource()},
		Metrics: []CityOpenWorldMobilityODCycleMetric{},
	}
	require.NoError(t, validateCityOpenWorldMobilityODState(state))

	state.Sources[0].PurposeCode = "unsealed.purpose"
	require.Error(t, validateCityOpenWorldMobilityODState(state))
}

func TestCityOpenWorldMobilityODStateRequiresAuditedSourceTransition(t *testing.T) {
	state := &CityOpenWorldMobilityODState{
		Policy:  newValidCityOpenWorldMobilityODPolicy(t),
		Sources: []CityOpenWorldMobilityODSource{newValidCityOpenWorldMobilityODSource()},
		Metrics: []CityOpenWorldMobilityODCycleMetric{},
	}
	state.Policy.GeneratedCount = 1
	state.Policy.Revision = 2
	state.Sources[0].GeneratedCount = 1
	state.Sources[0].Version = 2
	state.Sources[0].LastTransitionTick = 1
	state.Sources[0].NextDueTick = 25
	require.Error(t, validateCityOpenWorldMobilityODState(state))

	state.Sources[0].LastFact = &CityOpenWorldRuntimeFactRef{Tick: 1, Sequence: 1}
	require.NoError(t, validateCityOpenWorldMobilityODState(state))
}

func TestCityOpenWorldMobilityODCycleWindowClosesOnlyAfterCompleteCycle(t *testing.T) {
	policy := newValidCityOpenWorldMobilityODPolicy(t)
	for _, targetTick := range []int64{1, 24} {
		start, end, due := cityOpenWorldMobilityODCycleWindow(policy, targetTick)
		require.False(t, due)
		require.Zero(t, start)
		require.Zero(t, end)
	}

	start, end, due := cityOpenWorldMobilityODCycleWindow(policy, 25)
	require.True(t, due)
	require.Equal(t, int64(1), start)
	require.Equal(t, int64(24), end)

	start, end, due = cityOpenWorldMobilityODCycleWindow(policy, 49)
	require.True(t, due)
	require.Equal(t, int64(25), start)
	require.Equal(t, int64(48), end)
}

func TestCityOpenWorldMobilityODDemandCodeIsStableAndBounded(t *testing.T) {
	first := cityOpenWorldMobilityODDemandCode("mobility.od.source.test", 25)
	require.Equal(t, first, cityOpenWorldMobilityODDemandCode("mobility.od.source.test", 25))
	require.NotEqual(t, first, cityOpenWorldMobilityODDemandCode("mobility.od.source.test", 26))
	require.True(t, strings.HasPrefix(first, "mobility.demand.od."))
	require.True(t, worldRuntimeCodeValid(first, 160))
}
