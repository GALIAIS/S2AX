package service

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReduceCityPhysicalNetworkTopologyFactVerifiesProjectionHashes(t *testing.T) {
	connection := cityPhysicalNetworkBaselineConnection{
		Code: "connection.hospital", Status: CityServiceProjectionStatusActive,
		MaxFlowUnits: 1_000, Preference: 100, ServiceID: 1,
		ServiceCode: "electric_power", ServiceName: "Electric power", FlowKind: "delivery",
		CapacityID: 10, FacilityCode: "facility.power", FacilityDistrictID: 20,
		FacilityDistrictCode: "district.core", FacilityBuildingID: 30,
		FacilityBuildingCode: "building.power", FacilityStatus: CityFacilityStatusOperational,
		DemandID: 40, DemandCode: "demand.hospital", DemandDistrictID: 20,
		DemandDistrictCode: "district.core", DemandBuildingID: validCityNullInt64(50),
		DemandBuildingCode: validCityNullString("building.hospital"),
		DemandStatus:       CityServiceProjectionStatusActive,
	}
	desiredNodes, desiredEdges, err := buildCityPhysicalNetworkDesiredTopology(
		[]cityPhysicalNetworkBaselineConnection{connection},
	)
	require.NoError(t, err)
	payload, err := buildCityPhysicalNetworkTopologyFactPayload(
		"network.electric_power", connection, 1, 1, nil,
		map[string]*cityPhysicalNetworkTopologyNodeRef{},
		map[string]*cityPhysicalNetworkTopologyEdgeRef{},
		desiredNodes, desiredEdges, []string{connection.Code},
	)
	require.NoError(t, err)
	fact := CityPhysicalNetworkFact{
		Tick: 1, Sequence: 1, Phase: CityPhysicalNetworkPhasePreNetwork,
		FactType:    CityPhysicalNetworkFactTopologySynchronized,
		SubjectKind: "network", SubjectCode: "network.electric_power",
		VersionBefore: 0, VersionAfter: 1, Payload: mustCityPhysicalNetworkTestJSON(t, payload),
	}
	state := &cityHashState{PhysicalNetworks: cityPhysicalNetworkReplayTestState()}
	require.NoError(t, reduceCityPhysicalNetworkFact(state, fact))
	require.Len(t, state.PhysicalNetworks.Networks, 1)
	require.Len(t, state.PhysicalNetworks.Nodes, 2)
	require.Len(t, state.PhysicalNetworks.Edges, 1)
	require.EqualValues(t, 1, state.PhysicalNetworks.Profile.FactCount)
	require.EqualValues(t, 2, state.PhysicalNetworks.Profile.Revision)

	tampered := cityPhysicalNetworkReplayTestState()
	payload.AfterProjectionHash = "0000000000000000000000000000000000000000000000000000000000000000"
	fact.Payload = mustCityPhysicalNetworkTestJSON(t, payload)
	require.Error(t, reduceCityPhysicalNetworkFact(
		&cityHashState{PhysicalNetworks: tampered}, fact,
	))
}

func TestReduceCityPhysicalNetworkFlowFactVerifiesPathAndAllocationProof(t *testing.T) {
	graph, err := newCityNetworkResidualGraph(
		[]string{"supply.power", "demand.hospital"},
		[]cityNetworkPlannerEdge{{
			Code: "edge.power", FromNodeCode: "supply.power", ToNodeCode: "demand.hospital",
			Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1_000,
			LossMilli: 100, BaseCostUnits: 1, Version: 1,
		}},
		testCityNetworkPlannerPolicy(),
	)
	require.NoError(t, err)
	route, err := graph.route(cityNetworkRouteRequest{
		ConnectionCode: "connection.hospital", SourceNodeCode: "supply.power",
		SinkNodeCode: "demand.hospital", MaximumDispatchedUnits: 1_000,
		MaximumNetworkReceivedUnits: 900,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1_000, route.DispatchedUnits)
	require.EqualValues(t, 900, route.NetworkReceivedUnits)

	physical := cityPhysicalNetworkReplayTestState()
	physical.Policies = []CityPhysicalNetworkPolicy{{
		ServiceCode: "electric_power", RouteDirection: CityNetworkRouteSupplyToDemand,
		LossCostWeight: 1,
	}}
	physical.Networks = []CityPhysicalNetwork{{
		Code: "network.electric_power", ServiceCode: "electric_power",
		Status: CityNetworkStatusActive, TopologyRevision: 1, Version: 1,
		Metadata: json.RawMessage(`{"schema_version":1}`),
	}}
	capacityCode := "facility.power.electric_power"
	demandCode := "demand.hospital"
	physical.Nodes = []CityPhysicalNetworkNode{
		{Code: "supply.power", NetworkCode: "network.electric_power", Role: CityNetworkNodeRoleSupply, CapacityCode: &capacityCode, Status: CityNetworkNodeStatusActive, Version: 1, Metadata: json.RawMessage(`{"schema_version":1}`)},
		{Code: "demand.hospital", NetworkCode: "network.electric_power", Role: CityNetworkNodeRoleDemand, DemandCode: &demandCode, Status: CityNetworkNodeStatusActive, Version: 1, Metadata: json.RawMessage(`{"schema_version":1}`)},
	}
	physical.Edges = []CityPhysicalNetworkEdge{{
		Code: "edge.power", NetworkCode: "network.electric_power",
		FromNodeCode: "supply.power", ToNodeCode: "demand.hospital",
		Direction: CityNetworkEdgeDirectionDirected, InstalledCapacityUnits: 1_000,
		AvailabilityMilli: 1_000, AvailableCapacityUnits: 1_000, LossMilli: 100,
		BaseCostUnits: 1, Status: CityNetworkEdgeStatusActive,
		ConditionMilli: 1_000, Version: 1, Metadata: json.RawMessage(`{"schema_version":1}`),
	}}
	physical.Profile.NetworkCount = 1
	physical.Profile.NodeCount = 2
	physical.Profile.EdgeCount = 1
	networkReceived, networkLoss, connectionLoss, pathCount := int64(900), int64(100), int64(90), 1
	allocation := CityServiceAllocation{
		Tick: 2, Sequence: 1, AllocationIndex: 1, ServiceCode: "electric_power",
		FacilityCode: "facility.power", DemandCode: "demand.hospital",
		ConnectionCode: "connection.hospital", DispatchedUnits: 1_000,
		NetworkReceivedUnits: &networkReceived, NetworkLossUnits: &networkLoss,
		ConnectionLossUnits: &connectionLoss, NetworkPathCount: &pathCount,
		DeliveredUnits: 810, LossUnits: 190, Metadata: json.RawMessage(`{"schema_version":2}`),
	}
	entry := cityPhysicalNetworkPersistedAllocation{
		serviceFactID: 1, serviceSequence: 1, allocationIndex: 1, connectionID: 1,
		plan: cityServiceAllocationPlan{
			ConnectionCode: "connection.hospital", NetworkCode: "network.electric_power",
			DispatchedUnits: route.DispatchedUnits, NetworkReceivedUnits: route.NetworkReceivedUnits,
			NetworkLossUnits: route.NetworkLossUnits, NetworkPaths: route.Paths,
		},
	}
	payload, err := buildCityPhysicalNetworkFlowFactPayload(
		2, 1,
		&cityServicePhysicalNetworkPlan{
			code: "network.electric_power", serviceCode: "electric_power", topologyRevision: 1,
		},
		[]cityPhysicalNetworkPersistedAllocation{entry},
	)
	require.NoError(t, err)
	fact := CityPhysicalNetworkFact{
		Tick: 2, Sequence: 1, Phase: CityPhysicalNetworkPhaseSettlement,
		FactType: CityPhysicalNetworkFactFlowSettled, SubjectKind: "flow_batch",
		SubjectCode: "network.electric_power", VersionBefore: 1, VersionAfter: 2,
		Payload: mustCityPhysicalNetworkTestJSON(t, payload),
	}
	state := &cityHashState{
		PhysicalNetworks: cloneCityPhysicalNetworkTestState(t, physical),
		PublicServices:   &cityPublicServiceHashState{Allocations: []CityServiceAllocation{allocation}},
	}
	require.NoError(t, reduceCityPhysicalNetworkFact(state, fact))
	require.Len(t, state.PhysicalNetworks.Batches, 1)
	require.Len(t, state.PhysicalNetworks.Paths, 1)
	require.Len(t, state.PhysicalNetworks.Segments, 1)

	tamperedPayload := payload
	tamperedPayload.Segments = append([]CityPhysicalNetworkFlowSegment(nil), payload.Segments...)
	tamperedPayload.Segments[0].OutputUnits--
	tamperedState := &cityHashState{
		PhysicalNetworks: cloneCityPhysicalNetworkTestState(t, physical),
		PublicServices:   &cityPublicServiceHashState{Allocations: []CityServiceAllocation{allocation}},
	}
	fact.Payload = mustCityPhysicalNetworkTestJSON(t, tamperedPayload)
	require.Error(t, reduceCityPhysicalNetworkFact(tamperedState, fact))
}

func TestReduceCityPhysicalNetworkCommandFactsRejectUnsafeTerminalAndTopologyChanges(t *testing.T) {
	t.Run("retired network with live node", func(t *testing.T) {
		physical := cityPhysicalNetworkReplayTestState()
		physical.Policies = []CityPhysicalNetworkPolicy{{ServiceCode: "electric_power"}}
		before := CityPhysicalNetwork{
			Code: "grid.core", Name: "Core Grid", ServiceCode: "electric_power",
			Status: CityNetworkStatusActive, TopologyRevision: 1, CreatedTick: 1,
			UpdatedTick: 1, Version: 1, SourceFactTick: 1, SourceFactSequence: 1,
			Metadata: json.RawMessage(`{"baseline_mode":"explicit","schema_version":1}`),
		}
		physical.Networks = []CityPhysicalNetwork{before}
		physical.Nodes = []CityPhysicalNetworkNode{{
			Code: "junction.core", NetworkCode: before.Code, Role: CityNetworkNodeRoleJunction,
			Status: CityNetworkNodeStatusActive, Version: 1, Metadata: json.RawMessage(`{"schema_version":1}`),
		}}
		physical.Profile.NetworkCount = 1
		physical.Profile.NodeCount = 1
		after := before
		after.Status = CityNetworkStatusRetired
		after.TopologyRevision = 2
		after.UpdatedTick = 2
		after.Version = 2
		after.SourceFactTick = 2
		after.SourceFactSequence = 1
		commandSequence := int64(1)
		fact := CityPhysicalNetworkFact{
			Tick: 2, Sequence: 1, Phase: "command", SourceCommandSequence: &commandSequence,
			FactType: CityPhysicalNetworkFactNetworkConfigured, SubjectKind: "network",
			SubjectCode: before.Code, VersionBefore: 1, VersionAfter: 2,
			Payload: mustCityPhysicalNetworkTestJSON(t, cityPhysicalNetworkConfigureFactPayload{
				SchemaVersion: cityPhysicalNetworkSchemaVersion, NetworkBefore: &before, NetworkAfter: after,
			}),
		}
		require.ErrorContains(t, reduceCityPhysicalNetworkFact(
			&cityHashState{PhysicalNetworks: physical}, fact,
		), "live node")
	})

	t.Run("active edge endpoint mutation", func(t *testing.T) {
		physical := cityPhysicalNetworkReplayTestState()
		physical.Policies = []CityPhysicalNetworkPolicy{{
			ServiceCode: "electric_power", AllowBidirectional: true,
		}}
		networkBefore := CityPhysicalNetwork{
			Code: "grid.core", Name: "Core Grid", ServiceCode: "electric_power",
			Status: CityNetworkStatusActive, TopologyRevision: 2, CreatedTick: 1,
			UpdatedTick: 2, Version: 2, SourceFactTick: 2, SourceFactSequence: 1,
			Metadata: json.RawMessage(`{"baseline_mode":"explicit","schema_version":1}`),
		}
		physical.Networks = []CityPhysicalNetwork{networkBefore}
		physical.Nodes = []CityPhysicalNetworkNode{
			{Code: "node.a", NetworkCode: networkBefore.Code, Role: CityNetworkNodeRoleJunction, Status: CityNetworkNodeStatusActive, Version: 1, Metadata: json.RawMessage(`{"schema_version":1}`)},
			{Code: "node.b", NetworkCode: networkBefore.Code, Role: CityNetworkNodeRoleJunction, Status: CityNetworkNodeStatusActive, Version: 1, Metadata: json.RawMessage(`{"schema_version":1}`)},
		}
		edgeBefore := CityPhysicalNetworkEdge{
			Code: "edge.ab", NetworkCode: networkBefore.Code, FromNodeCode: "node.a", ToNodeCode: "node.b",
			Direction: CityNetworkEdgeDirectionDirected, InstalledCapacityUnits: 100,
			AvailabilityMilli: 1000, AvailableCapacityUnits: 100, BaseCostUnits: 1,
			Status: CityNetworkEdgeStatusActive, ConditionMilli: 1000, CreatedTick: 2,
			UpdatedTick: 2, Version: 1, SourceFactTick: 2, SourceFactSequence: 2,
			Metadata: json.RawMessage(`{"baseline_mode":"explicit","schema_version":1}`),
		}
		physical.Edges = []CityPhysicalNetworkEdge{edgeBefore}
		physical.Profile.NetworkCount = 1
		physical.Profile.NodeCount = 2
		physical.Profile.EdgeCount = 1
		networkAfter := networkBefore
		networkAfter.TopologyRevision = 3
		networkAfter.UpdatedTick = 3
		networkAfter.Version = 3
		networkAfter.SourceFactTick = 3
		networkAfter.SourceFactSequence = 1
		edgeAfter := edgeBefore
		edgeAfter.Direction = CityNetworkEdgeDirectionBidirectional
		edgeAfter.UpdatedTick = 3
		edgeAfter.Version = 2
		edgeAfter.SourceFactTick = 3
		edgeAfter.SourceFactSequence = 1
		commandSequence := int64(2)
		fact := CityPhysicalNetworkFact{
			Tick: 3, Sequence: 1, Phase: "command", SourceCommandSequence: &commandSequence,
			FactType: CityPhysicalNetworkFactEdgeConfigured, SubjectKind: "edge",
			SubjectCode: edgeBefore.Code, VersionBefore: 1, VersionAfter: 2,
			Payload: mustCityPhysicalNetworkTestJSON(t, cityPhysicalNetworkEdgeConfigureFactPayload{
				SchemaVersion: cityPhysicalNetworkSchemaVersion, NetworkBefore: networkBefore,
				NetworkAfter: networkAfter, EdgeBefore: &edgeBefore, EdgeAfter: edgeAfter,
			}),
		}
		require.ErrorContains(t, reduceCityPhysicalNetworkFact(
			&cityHashState{PhysicalNetworks: physical}, fact,
		), "changed topology")
	})
}

func cityPhysicalNetworkReplayTestState() *cityPhysicalNetworkHashState {
	return &cityPhysicalNetworkHashState{
		Profile:  CityPhysicalNetworkProfile{Revision: 1},
		Policies: make([]CityPhysicalNetworkPolicy, 0),
		Networks: make([]CityPhysicalNetwork, 0), Nodes: make([]CityPhysicalNetworkNode, 0),
		Edges: make([]CityPhysicalNetworkEdge, 0), Facts: make([]CityPhysicalNetworkFact, 0),
		Batches: make([]CityPhysicalNetworkFlowBatch, 0), Paths: make([]CityPhysicalNetworkFlowPath, 0),
		Segments: make([]CityPhysicalNetworkFlowSegment, 0),
	}
}

func validCityNullInt64(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: true}
}

func validCityNullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: true}
}

func mustCityPhysicalNetworkTestJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}

func cloneCityPhysicalNetworkTestState(
	t *testing.T, state *cityPhysicalNetworkHashState,
) *cityPhysicalNetworkHashState {
	t.Helper()
	raw, err := json.Marshal(state)
	require.NoError(t, err)
	var cloned cityPhysicalNetworkHashState
	require.NoError(t, json.Unmarshal(raw, &cloned))
	return &cloned
}
