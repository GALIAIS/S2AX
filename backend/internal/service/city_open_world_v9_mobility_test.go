package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldMobilityState(t *testing.T) *CityOpenWorldMobilityState {
	t.Helper()
	binding := &CityOpenWorldBinding{SpawnX: 192, SpawnY: -64, SpawnZ: 0}
	facilities := []cityOpenWorldMobilityFacilitySeed{
		{ID: 11, Code: "facility.market", X: 0, Y: 0, Z: 0},
		{ID: 12, Code: "facility.school", X: 128, Y: 0, Z: 0},
		{ID: 13, Code: "facility.clinic", X: 256, Y: -64, Z: 0},
	}
	modes, hubs, edges, err := buildCityOpenWorldMobilityTopology(binding, facilities)
	require.NoError(t, err)
	contentHash, err := cityOpenWorldMobilityTopologyHash(modes, hubs, edges)
	require.NoError(t, err)
	return &CityOpenWorldMobilityState{
		Policy: CityOpenWorldMobilityPolicy{
			ProfileID:               cityOpenWorldMobilityProfileID,
			ProfileVersion:          cityOpenWorldMobilityProfileVersion,
			ContentHash:             contentHash,
			BaselineTick:            0,
			TopologyContractVersion: cityOpenWorldMobilityTopologyContractVersion,
			SchedulingContract:      cityOpenWorldMobilitySchedulingContract,
			MaximumSchedulesPerTick: cityOpenWorldMobilityMaximumSchedulesPerTick,
			MaximumWaitTicks:        cityOpenWorldMobilityMaximumWaitTicks,
			ModeCount:               int64(len(modes)),
			HubCount:                int64(len(hubs)),
			EdgeCount:               int64(len(edges)),
			Revision:                1,
			Metadata:                json.RawMessage(`{}`),
		},
		Modes:        modes,
		Hubs:         hubs,
		Edges:        edges,
		Demands:      []CityOpenWorldMobilityDemand{},
		Routes:       []CityOpenWorldMobilityRoute{},
		Allocations:  []CityOpenWorldMobilityAllocation{},
		ActorMetrics: []CityOpenWorldMobilityActorMetric{},
	}
}

func TestCityOpenWorldV9MobilityTopologyIsDeterministicAndConnected(t *testing.T) {
	first := newValidCityOpenWorldMobilityState(t)
	second := newValidCityOpenWorldMobilityState(t)
	require.Equal(t, first.Modes, second.Modes)
	require.Equal(t, first.Hubs, second.Hubs)
	require.Equal(t, first.Edges, second.Edges)
	require.Equal(t, first.Policy.ContentHash, second.Policy.ContentHash)
	require.NoError(t, validateCityOpenWorldMobilityState(first))

	// JSONB rewrites object-key ordering when it round-trips through PostgreSQL.
	// The topology checksum must bind simulation fields, not that presentation
	// detail, otherwise a valid world becomes unreadable immediately after save.
	metadataNormalized := *first
	metadataNormalized.Modes = append([]CityOpenWorldMobilityMode(nil), first.Modes...)
	metadataNormalized.Modes[0].Metadata = json.RawMessage(`{"delay_contract":"post_allocation_occupancy_v1","schema_version":1,"capacity_scope":"edge_departure_tick"}`)
	require.NoError(t, validateCityOpenWorldMobilityState(&metadataNormalized))

	legacyHash, err := cityOpenWorldMobilityLegacyTopologyHash(first.Modes, first.Hubs, first.Edges)
	require.NoError(t, err)
	legacy := *first
	legacy.Policy.ContentHash = legacyHash
	require.NoError(t, validateCityOpenWorldMobilityState(&legacy), "the short-lived raw-metadata V9 checksum remains recoverable")

	sourceHubCode := cityOpenWorldMobilityZoneHubCode(0, 0)
	for _, mode := range first.Modes {
		path, err := cityOpenWorldMobilityShortestPath(first.Edges, mode.Code, sourceHubCode, "hub.interchange.central")
		require.NoErrorf(t, err, "mode %s", mode.Code)
		require.NotEmptyf(t, path, "mode %s", mode.Code)
		require.Equal(t, sourceHubCode, path[0].FromHubCode)
		require.Equal(t, "hub.interchange.central", path[len(path)-1].ToHubCode)
	}

	first.Edges[0].CapacityUnitsPerTick++
	require.Error(t, validateCityOpenWorldMobilityState(first), "sealed topology changes must invalidate the content hash")
}

func TestCityOpenWorldV9MobilityRoutingAndCongestionAreDeterministic(t *testing.T) {
	edges := []CityOpenWorldMobilityEdge{
		{Code: "edge.a", ModeCode: "walk", FromHubCode: "hub.source", ToHubCode: "hub.a", BaseTravelTicks: 2},
		{Code: "edge.b", ModeCode: "walk", FromHubCode: "hub.source", ToHubCode: "hub.b", BaseTravelTicks: 2},
		{Code: "edge.a.destination", ModeCode: "walk", FromHubCode: "hub.a", ToHubCode: "hub.destination", BaseTravelTicks: 3},
		{Code: "edge.b.destination", ModeCode: "walk", FromHubCode: "hub.b", ToHubCode: "hub.destination", BaseTravelTicks: 3},
	}
	path, err := cityOpenWorldMobilityShortestPath(edges, "walk", "hub.source", "hub.destination")
	require.NoError(t, err)
	require.Equal(t, []string{"edge.a", "edge.a.destination"}, cityOpenWorldMobilityPathCodes(path))

	mode := CityOpenWorldMobilityMode{CongestionThresholdMilli: 700, MaximumDelayTicks: 4}
	occupancy, delay, err := cityOpenWorldMobilityCongestionDelay(mode, 45, 64)
	require.NoError(t, err)
	require.Equal(t, 704, occupancy)
	require.Equal(t, int64(1), delay)
	occupancy, delay, err = cityOpenWorldMobilityCongestionDelay(mode, 64, 64)
	require.NoError(t, err)
	require.Equal(t, 1000, occupancy)
	require.Equal(t, int64(4), delay)
	_, _, err = cityOpenWorldMobilityCongestionDelay(mode, 65, 64)
	require.Error(t, err)
}

func TestCityOpenWorldV9MobilitySchedulingLimitDoesNotStarvePendingTrips(t *testing.T) {
	policy := &CityOpenWorldMobilityPolicy{MaximumSchedulesPerTick: 3}
	execution := &cityOpenWorldRuntimeAutomaticExecution{facts: []CityOpenWorldRuntimeFact{
		{FactType: CityOpenWorldRuntimeFactMobilityCompleted},
		{FactType: CityOpenWorldRuntimeFactMobilityExpired},
		{FactType: CityOpenWorldRuntimeFactMobilityScheduled},
	}}
	require.Equal(t, 2, cityOpenWorldMobilitySchedulingSlots(policy, execution))
	execution.facts = append(execution.facts,
		CityOpenWorldRuntimeFact{FactType: CityOpenWorldRuntimeFactMobilityScheduled},
		CityOpenWorldRuntimeFact{FactType: CityOpenWorldRuntimeFactMobilityScheduled},
	)
	require.Equal(t, 0, cityOpenWorldMobilitySchedulingSlots(policy, execution))
}
