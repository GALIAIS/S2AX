package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityPhysicalNetworkDiagnosticsBuildsComponentsAndServiceIslands(t *testing.T) {
	nodes := []cityPhysicalNetworkDiagnosticNode{
		{code: "source", role: CityNetworkNodeRoleSupply, status: CityNetworkNodeStatusActive},
		{code: "junction", role: CityNetworkNodeRoleJunction, status: CityNetworkNodeStatusActive},
		{code: "sink", role: CityNetworkNodeRoleDemand, status: CityNetworkNodeStatusActive},
		{code: "isolated_sink", role: CityNetworkNodeRoleDemand, status: CityNetworkNodeStatusActive},
		{code: "offline_source", role: CityNetworkNodeRoleSupply, status: CityNetworkNodeStatusOffline},
	}
	edges := []*cityPhysicalNetworkDiagnosticEdge{
		{value: CityPhysicalNetworkEdge{Code: "edge_source", FromNodeCode: "source", ToNodeCode: "junction", Status: CityNetworkEdgeStatusActive}},
		{value: CityPhysicalNetworkEdge{Code: "edge_sink", FromNodeCode: "junction", ToNodeCode: "sink", Status: CityNetworkEdgeStatusActive}},
		{value: CityPhysicalNetworkEdge{Code: "edge_failed", FromNodeCode: "source", ToNodeCode: "isolated_sink", Status: CityNetworkEdgeStatusFailed}},
	}

	components, isolated := buildCityPhysicalNetworkComponents(nodes, edges)

	require.Equal(t, 1, isolated)
	require.Len(t, components, 2)
	require.Equal(t, []string{"isolated_sink"}, components[0].NodeCodes)
	require.True(t, components[0].ServiceIsland)
	require.Equal(t, []string{"junction", "sink", "source"}, components[1].NodeCodes)
	require.Equal(t, 3, components[1].NodeCount)
	require.Equal(t, 2, components[1].EdgeCount)
	require.Equal(t, 1, components[1].SupplyNodeCount)
	require.Equal(t, 1, components[1].DemandNodeCount)
	require.False(t, components[1].ServiceIsland)
}

func TestCityPhysicalNetworkDiagnosticsRanksUtilizationAndBottlenecks(t *testing.T) {
	edges := []*cityPhysicalNetworkDiagnosticEdge{
		{value: CityPhysicalNetworkEdge{Code: "edge_low", Status: CityNetworkEdgeStatusActive, AvailableCapacityUnits: 100}, input: 20, output: 18, loss: 2},
		{value: CityPhysicalNetworkEdge{Code: "edge_hot", Status: CityNetworkEdgeStatusActive, AvailableCapacityUnits: 100}, input: 95, output: 90, loss: 5},
		{value: CityPhysicalNetworkEdge{Code: "edge_full", Status: CityNetworkEdgeStatusActive, AvailableCapacityUnits: 100}, input: 100, output: 100},
		{value: CityPhysicalNetworkEdge{Code: "edge_failed", Status: CityNetworkEdgeStatusFailed, AvailableCapacityUnits: 100}, input: 100, output: 100},
	}

	items, bottlenecks, saturated, truncated := buildCityPhysicalNetworkEdgeDiagnostics(edges)

	require.Len(t, items, 4)
	require.Equal(t, 2, bottlenecks)
	require.Equal(t, 1, saturated)
	require.Zero(t, truncated)
	require.Equal(t, "edge_full", items[0].EdgeCode)
	require.True(t, items[0].Saturated)
	require.Equal(t, "edge_hot", items[1].EdgeCode)
	require.True(t, items[1].Bottleneck)
	require.Equal(t, 950, items[1].UtilizationMilli)
	require.False(t, items[2].Bottleneck)
}

func TestCityPhysicalNetworkDiagnosticsProbesReachabilityWithoutMutatingProjection(t *testing.T) {
	nodes := []cityPhysicalNetworkDiagnosticNode{
		{code: "source", role: CityNetworkNodeRoleSupply, status: CityNetworkNodeStatusActive},
		{code: "junction", role: CityNetworkNodeRoleJunction, status: CityNetworkNodeStatusActive},
		{code: "sink", role: CityNetworkNodeRoleDemand, status: CityNetworkNodeStatusActive},
		{code: "offline", role: CityNetworkNodeRoleDemand, status: CityNetworkNodeStatusOffline},
	}
	edges := []*cityPhysicalNetworkDiagnosticEdge{
		{value: CityPhysicalNetworkEdge{Code: "edge_1", FromNodeCode: "source", ToNodeCode: "junction", Direction: CityNetworkEdgeDirectionDirected, Status: CityNetworkEdgeStatusActive, AvailableCapacityUnits: 100, LossMilli: 100, BaseCostUnits: 1, ConditionMilli: 1000, Version: 2}},
		{value: CityPhysicalNetworkEdge{Code: "edge_2", FromNodeCode: "junction", ToNodeCode: "sink", Direction: CityNetworkEdgeDirectionDirected, Status: CityNetworkEdgeStatusActive, AvailableCapacityUnits: 100, LossMilli: 200, BaseCostUnits: 1, ConditionMilli: 1000, Version: 3}},
	}
	policy := CityPhysicalNetworkPolicy{MaximumPaths: 4, MaximumHops: 8, LossCostWeight: 1}

	route, err := diagnoseCityPhysicalNetworkRoute(nodes, edges, policy, "source", "sink", 100)
	require.NoError(t, err)
	require.True(t, route.Reachable)
	require.Equal(t, "reachable", route.ReasonCode)
	require.Equal(t, int64(100), route.DispatchedUnits)
	require.Equal(t, int64(72), route.NetworkReceivedUnits)
	require.Equal(t, int64(28), route.NetworkLossUnits)
	require.Len(t, route.Paths, 1)
	require.Len(t, route.Paths[0].Segments, 2)
	require.Equal(t, int64(100), edges[0].value.AvailableCapacityUnits)

	unreachable, err := diagnoseCityPhysicalNetworkRoute(nodes, edges, policy, "source", "offline", 1)
	require.NoError(t, err)
	require.False(t, unreachable.Reachable)
	require.Equal(t, "sink_not_active", unreachable.ReasonCode)
}

func TestNormalizeCityPhysicalNetworkDiagnosticQueryIsStrict(t *testing.T) {
	valid := CityPhysicalNetworkQueryInput{
		UserID: 1, WorldID: 2, NetworkCode: " Grid.Main ",
		SourceNodeCode: " Source ", SinkNodeCode: " Sink ", ProbeUnits: 50,
	}
	require.NoError(t, normalizeCityPhysicalNetworkQuery(&valid, "diagnostic"))
	require.Equal(t, "grid.main", valid.NetworkCode)
	require.Equal(t, "source", valid.SourceNodeCode)
	require.Equal(t, cityPhysicalNetworkQueryDefaultLimit, valid.Limit)

	invalid := []CityPhysicalNetworkQueryInput{
		{UserID: 1, WorldID: 2},
		{UserID: 1, WorldID: 2, NetworkCode: "grid", SourceNodeCode: "source"},
		{UserID: 1, WorldID: 2, NetworkCode: "grid", SourceNodeCode: "same", SinkNodeCode: "same"},
		{UserID: 1, WorldID: 2, NetworkCode: "grid", ServiceCode: "electric_power"},
		{UserID: 1, WorldID: 2, NetworkCode: "grid", Limit: 1},
		{UserID: 1, WorldID: 2, NetworkCode: "grid", ProbeUnits: -1},
	}
	for _, item := range invalid {
		require.ErrorIs(t, normalizeCityPhysicalNetworkQuery(&item, "diagnostic"), ErrCityInvalidInput)
	}
}
