package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func testCityNetworkPlannerPolicy() cityNetworkPlannerPolicy {
	return cityNetworkPlannerPolicy{
		MaximumPaths: 8, MaximumHops: 16, LossCostWeight: 1,
	}
}

func TestCityNetworkPlannerUsesStablePathAndCompoundsLoss(t *testing.T) {
	graph, err := newCityNetworkResidualGraph(
		[]string{"source", "alpha", "beta", "sink"},
		[]cityNetworkPlannerEdge{
			{Code: "edge_alpha_1", FromNodeCode: "source", ToNodeCode: "alpha", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1000, LossMilli: 100, BaseCostUnits: 10, Version: 1},
			{Code: "edge_alpha_2", FromNodeCode: "alpha", ToNodeCode: "sink", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1000, LossMilli: 200, BaseCostUnits: 10, Version: 1},
			{Code: "edge_beta_1", FromNodeCode: "source", ToNodeCode: "beta", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1000, LossMilli: 100, BaseCostUnits: 10, Version: 1},
			{Code: "edge_beta_2", FromNodeCode: "beta", ToNodeCode: "sink", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1000, LossMilli: 200, BaseCostUnits: 10, Version: 1},
		},
		testCityNetworkPlannerPolicy(),
	)
	require.NoError(t, err)
	plan, err := graph.route(cityNetworkRouteRequest{
		ConnectionCode: "connection_alpha", SourceNodeCode: "source", SinkNodeCode: "sink",
		MaximumDispatchedUnits: 1000, MaximumNetworkReceivedUnits: 1000,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1000), plan.DispatchedUnits)
	require.Equal(t, int64(720), plan.NetworkReceivedUnits)
	require.Equal(t, int64(280), plan.NetworkLossUnits)
	require.Len(t, plan.Paths, 1)
	require.Equal(t, "edge_alpha_1:forward,edge_alpha_2:forward", cityNetworkPathEdgeCodes(plan.Paths[0]))
	require.Len(t, plan.Paths[0].PathHash, 64)
	require.Equal(t, int64(900), plan.Paths[0].Segments[0].OutputUnits)
	require.Equal(t, 100, plan.Paths[0].Segments[0].LossMilli)
	require.Equal(t, int64(720), plan.Paths[0].Segments[1].OutputUnits)
	require.Equal(t, 200, plan.Paths[0].Segments[1].LossMilli)
}

func TestCityNetworkPlannerRespectsReceivedBoundAndDownstreamCapacity(t *testing.T) {
	graph, err := newCityNetworkResidualGraph(
		[]string{"source", "junction", "sink"},
		[]cityNetworkPlannerEdge{
			{Code: "edge_trunk", FromNodeCode: "source", ToNodeCode: "junction", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1000, BaseCostUnits: 1, Version: 1},
			{Code: "edge_bottleneck", FromNodeCode: "junction", ToNodeCode: "sink", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 100, BaseCostUnits: 1, Version: 1},
		},
		testCityNetworkPlannerPolicy(),
	)
	require.NoError(t, err)
	plan, err := graph.route(cityNetworkRouteRequest{
		ConnectionCode: "connection_alpha", SourceNodeCode: "source", SinkNodeCode: "sink",
		MaximumDispatchedUnits: 1000, MaximumNetworkReceivedUnits: 60,
	})
	require.NoError(t, err)
	require.Equal(t, int64(60), plan.DispatchedUnits)
	require.Equal(t, int64(60), plan.NetworkReceivedUnits)
	require.Equal(t, int64(940), graph.remaining["edge_trunk"])
	require.Equal(t, int64(40), graph.remaining["edge_bottleneck"])
}

func TestCityNetworkPlannerSharesBidirectionalCapacity(t *testing.T) {
	graph, err := newCityNetworkResidualGraph(
		[]string{"node_alpha", "node_beta"},
		[]cityNetworkPlannerEdge{{
			Code: "edge_shared", FromNodeCode: "node_alpha", ToNodeCode: "node_beta",
			Direction:              CityNetworkEdgeDirectionBidirectional,
			AvailableCapacityUnits: 10, BaseCostUnits: 1, Version: 3,
		}},
		testCityNetworkPlannerPolicy(),
	)
	require.NoError(t, err)
	forward, err := graph.route(cityNetworkRouteRequest{
		ConnectionCode: "connection_forward", SourceNodeCode: "node_alpha", SinkNodeCode: "node_beta",
		MaximumDispatchedUnits: 7, MaximumNetworkReceivedUnits: 7,
	})
	require.NoError(t, err)
	require.Equal(t, int64(7), forward.NetworkReceivedUnits)
	reverse, err := graph.route(cityNetworkRouteRequest{
		ConnectionCode: "connection_reverse", SourceNodeCode: "node_beta", SinkNodeCode: "node_alpha",
		MaximumDispatchedUnits: 10, MaximumNetworkReceivedUnits: 10,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), reverse.DispatchedUnits)
	require.Equal(t, "reverse", reverse.Paths[0].Segments[0].Direction)
	require.Zero(t, graph.remaining["edge_shared"])
}

func TestCityNetworkPlannerSplitsAcrossResidualAlternativePaths(t *testing.T) {
	graph, err := newCityNetworkResidualGraph(
		[]string{"source", "cheap", "alternate", "sink"},
		[]cityNetworkPlannerEdge{
			{Code: "edge_cheap_1", FromNodeCode: "source", ToNodeCode: "cheap", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 4, BaseCostUnits: 1, Version: 1},
			{Code: "edge_cheap_2", FromNodeCode: "cheap", ToNodeCode: "sink", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 4, BaseCostUnits: 1, Version: 1},
			{Code: "edge_alt_1", FromNodeCode: "source", ToNodeCode: "alternate", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 10, BaseCostUnits: 5, Version: 1},
			{Code: "edge_alt_2", FromNodeCode: "alternate", ToNodeCode: "sink", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 10, BaseCostUnits: 5, Version: 1},
		},
		testCityNetworkPlannerPolicy(),
	)
	require.NoError(t, err)
	plan, err := graph.route(cityNetworkRouteRequest{
		ConnectionCode: "connection_split", SourceNodeCode: "source", SinkNodeCode: "sink",
		MaximumDispatchedUnits: 10, MaximumNetworkReceivedUnits: 10,
	})
	require.NoError(t, err)
	require.Equal(t, int64(10), plan.NetworkReceivedUnits)
	require.Len(t, plan.Paths, 2)
	require.Equal(t, int64(4), plan.Paths[0].DispatchedUnits)
	require.Equal(t, int64(6), plan.Paths[1].DispatchedUnits)
	require.Equal(t, "edge_cheap_1:forward,edge_cheap_2:forward", cityNetworkPathEdgeCodes(plan.Paths[0]))
	require.Equal(t, "edge_alt_1:forward,edge_alt_2:forward", cityNetworkPathEdgeCodes(plan.Paths[1]))
}

func TestCityNetworkPlannerSkipsZeroOutputPath(t *testing.T) {
	policy := testCityNetworkPlannerPolicy()
	policy.LossCostWeight = 0
	graph, err := newCityNetworkResidualGraph(
		[]string{"source", "dead_end", "sink"},
		[]cityNetworkPlannerEdge{
			{Code: "edge_dead_1", FromNodeCode: "source", ToNodeCode: "dead_end", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1, LossMilli: 999, BaseCostUnits: 1, Version: 1},
			{Code: "edge_dead_2", FromNodeCode: "dead_end", ToNodeCode: "sink", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 100, BaseCostUnits: 1, Version: 1},
			{Code: "edge_live", FromNodeCode: "source", ToNodeCode: "sink", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 10, BaseCostUnits: 100, Version: 1},
		},
		policy,
	)
	require.NoError(t, err)
	plan, err := graph.route(cityNetworkRouteRequest{
		ConnectionCode: "connection_live", SourceNodeCode: "source", SinkNodeCode: "sink",
		MaximumDispatchedUnits: 10, MaximumNetworkReceivedUnits: 10,
	})
	require.NoError(t, err)
	require.Equal(t, int64(10), plan.NetworkReceivedUnits)
	require.Equal(t, "edge_live:forward", cityNetworkPathEdgeCodes(plan.Paths[0]))
}

func TestCityNetworkPlannerRejectsInvalidAndOverflowingGraphs(t *testing.T) {
	_, err := newCityNetworkResidualGraph(
		[]string{"source", "sink"},
		[]cityNetworkPlannerEdge{{
			Code: "edge_overflow", FromNodeCode: "source", ToNodeCode: "sink",
			Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1,
			LossMilli: 999, BaseCostUnits: math.MaxInt64, Version: 1,
		}},
		cityNetworkPlannerPolicy{MaximumPaths: 1, MaximumHops: 1, LossCostWeight: math.MaxInt64},
	)
	require.Error(t, err)

	_, err = newCityNetworkResidualGraph(
		[]string{"source", "source"}, nil, testCityNetworkPlannerPolicy(),
	)
	require.Error(t, err)
}

func TestCityNetworkPlannerReturnsEmptyWhenUnreachable(t *testing.T) {
	graph, err := newCityNetworkResidualGraph(
		[]string{"source", "sink"}, nil, testCityNetworkPlannerPolicy(),
	)
	require.NoError(t, err)
	plan, err := graph.route(cityNetworkRouteRequest{
		ConnectionCode: "connection_none", SourceNodeCode: "source", SinkNodeCode: "sink",
		MaximumDispatchedUnits: 100, MaximumNetworkReceivedUnits: 100,
	})
	require.NoError(t, err)
	require.Empty(t, plan.Paths)
	require.Zero(t, plan.DispatchedUnits)
}
