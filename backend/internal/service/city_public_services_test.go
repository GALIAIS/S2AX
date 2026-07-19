package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityPublicServiceCatalogIsDeterministicAndSelfDescribing(t *testing.T) {
	services, facilityTypes, catalogHash, err := cityPublicServiceCatalog()
	require.NoError(t, err)
	require.Len(t, services, 8)
	require.Len(t, facilityTypes, 9)
	require.Len(t, catalogHash, 64)

	servicesAgain, facilityTypesAgain, catalogHashAgain, err := cityPublicServiceCatalog()
	require.NoError(t, err)
	require.Equal(t, services, servicesAgain)
	require.Equal(t, facilityTypes, facilityTypesAgain)
	require.Equal(t, catalogHash, catalogHashAgain)

	serviceCodes := make(map[string]struct{}, len(services))
	for _, definition := range services {
		require.Len(t, definition.DefinitionHash, 64)
		require.True(t, json.Valid(definition.Payload))
		serviceCodes[definition.Code] = struct{}{}
	}
	for _, definition := range facilityTypes {
		require.Len(t, definition.DefinitionHash, 64)
		require.True(t, json.Valid(definition.Payload))
		for _, serviceCode := range definition.AllowedServiceCodes {
			_, exists := serviceCodes[serviceCode]
			require.Truef(t, exists, "facility type %s references unknown service %s", definition.Code, serviceCode)
		}
	}
}

func TestPlanCityServiceSettlementsHonorsPriorityAcrossSharedCapacity(t *testing.T) {
	capacity := cityServiceTestCapacity(10, 1_000)
	facility := cityServiceTestFacility(20, CityFacilityStatusOperational)
	high := cityServiceTestDemand(1, "demand.high", 700, 900)
	low := cityServiceTestDemand(2, "demand.low", 800, 100)

	plans, err := planCityServiceSettlements([]cityServiceSettlementPlanInput{
		{
			Demand: low,
			Connections: []cityServiceSettlementConnectionInput{{
				Connection: cityServiceTestConnection(102, "connection.low", capacity.id, low.id, 1_000, 0, 10),
				Capacity:   capacity, Facility: facility,
			}},
		},
		{
			Demand: high,
			Connections: []cityServiceSettlementConnectionInput{{
				Connection: cityServiceTestConnection(101, "connection.high", capacity.id, high.id, 1_000, 0, 10),
				Capacity:   capacity, Facility: facility,
			}},
		},
	})
	require.NoError(t, err)
	require.Len(t, plans, 2)

	require.Equal(t, "demand.high", plans[0].DemandCode)
	require.EqualValues(t, 700, plans[0].DeliveredUnits)
	require.Zero(t, plans[0].ShortageUnits)
	require.Equal(t, 1000, plans[0].QualityMilli)

	require.Equal(t, "demand.low", plans[1].DemandCode)
	require.EqualValues(t, 300, plans[1].DeliveredUnits)
	require.EqualValues(t, 500, plans[1].ShortageUnits)
	require.Equal(t, 375, plans[1].QualityMilli)
	require.EqualValues(t, 300, plans[1].Allocations[0].DispatchedUnits)
}

func TestPlanCityServiceSettlementsAppliesPreferenceAndIntegerLoss(t *testing.T) {
	demand := cityServiceTestDemand(1, "demand.hospital", 900, 500)
	preferredCapacity := cityServiceTestCapacity(10, 1_000)
	backupCapacity := cityServiceTestCapacity(11, 1_000)
	preferredFacility := cityServiceTestFacility(20, CityFacilityStatusOperational)
	backupFacility := cityServiceTestFacility(21, CityFacilityStatusOperational)

	plans, err := planCityServiceSettlements([]cityServiceSettlementPlanInput{{
		Demand: demand,
		Connections: []cityServiceSettlementConnectionInput{
			{
				Connection: cityServiceTestConnection(102, "connection.backup", backupCapacity.id, demand.id, 1_000, 0, 10),
				Capacity:   backupCapacity, Facility: backupFacility,
			},
			{
				Connection: cityServiceTestConnection(101, "connection.preferred", preferredCapacity.id, demand.id, 1_000, 100, 100),
				Capacity:   preferredCapacity, Facility: preferredFacility,
			},
		},
	}})
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Len(t, plans[0].Allocations, 1)
	allocation := plans[0].Allocations[0]
	require.Equal(t, "connection.preferred", allocation.ConnectionCode)
	require.EqualValues(t, 1_000, allocation.DispatchedUnits)
	require.EqualValues(t, 900, allocation.DeliveredUnits)
	require.EqualValues(t, 100, allocation.LossUnits)
	require.Zero(t, plans[0].ShortageUnits)
}

func TestPlanCityServiceSettlementsTreatsOfflineFacilityAsUnavailable(t *testing.T) {
	demand := cityServiceTestDemand(1, "demand.offline", 400, 500)
	capacity := cityServiceTestCapacity(10, 1_000)
	facility := cityServiceTestFacility(20, CityFacilityStatusOffline)

	plans, err := planCityServiceSettlements([]cityServiceSettlementPlanInput{{
		Demand: demand,
		Connections: []cityServiceSettlementConnectionInput{{
			Connection: cityServiceTestConnection(101, "connection.offline", capacity.id, demand.id, 1_000, 0, 10),
			Capacity:   capacity, Facility: facility,
		}},
	}})
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Empty(t, plans[0].Allocations)
	require.Zero(t, plans[0].DeliveredUnits)
	require.EqualValues(t, 400, plans[0].ShortageUnits)
	require.Zero(t, plans[0].QualityMilli)
}

func TestPlanCityServiceSettlementsComposesNetworkAndConnectionLoss(t *testing.T) {
	demand := cityServiceTestDemand(1, "demand.networked", 648, 500)
	capacity := cityServiceTestCapacity(10, 1_000)
	facility := cityServiceTestFacility(20, CityFacilityStatusOperational)
	networks := cityServiceTestPhysicalNetworkPlanningState(t,
		[]string{"source", "junction", "sink"},
		[]cityNetworkPlannerEdge{
			{Code: "edge_primary", FromNodeCode: "source", ToNodeCode: "junction", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1_000, LossMilli: 100, BaseCostUnits: 1, Version: 1},
			{Code: "edge_secondary", FromNodeCode: "junction", ToNodeCode: "sink", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1_000, LossMilli: 200, BaseCostUnits: 1, Version: 1},
		},
		map[int64]string{capacity.id: "source"}, map[int64]string{demand.id: "sink"},
	)

	plans, err := planCityServiceSettlementsWithNetworks([]cityServiceSettlementPlanInput{{
		Demand: demand,
		Connections: []cityServiceSettlementConnectionInput{{
			Connection: cityServiceTestConnection(101, "connection.networked", capacity.id, demand.id, 1_000, 100, 10),
			Capacity:   capacity, Facility: facility,
		}},
	}}, networks)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Len(t, plans[0].Allocations, 1)
	allocation := plans[0].Allocations[0]
	require.Equal(t, "network.electric_power", allocation.NetworkCode)
	require.EqualValues(t, 1_000, allocation.DispatchedUnits)
	require.EqualValues(t, 720, allocation.NetworkReceivedUnits)
	require.EqualValues(t, 280, allocation.NetworkLossUnits)
	require.EqualValues(t, 72, allocation.ConnectionLossUnits)
	require.EqualValues(t, 648, allocation.DeliveredUnits)
	require.EqualValues(t, 352, allocation.LossUnits)
	require.Len(t, allocation.NetworkPaths, 1)
	require.Zero(t, plans[0].ShortageUnits)
}

func TestPlanCityServiceSettlementsSharesNetworkResidualByDemandPriority(t *testing.T) {
	capacity := cityServiceTestCapacity(10, 1_000)
	facility := cityServiceTestFacility(20, CityFacilityStatusOperational)
	high := cityServiceTestDemand(1, "demand.high.network", 700, 900)
	low := cityServiceTestDemand(2, "demand.low.network", 700, 100)
	networks := cityServiceTestPhysicalNetworkPlanningState(t,
		[]string{"source", "junction", "high", "low"},
		[]cityNetworkPlannerEdge{
			{Code: "edge_trunk", FromNodeCode: "source", ToNodeCode: "junction", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1_000, BaseCostUnits: 1, Version: 1},
			{Code: "edge_high", FromNodeCode: "junction", ToNodeCode: "high", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1_000, BaseCostUnits: 1, Version: 1},
			{Code: "edge_low", FromNodeCode: "junction", ToNodeCode: "low", Direction: CityNetworkEdgeDirectionDirected, AvailableCapacityUnits: 1_000, BaseCostUnits: 1, Version: 1},
		},
		map[int64]string{capacity.id: "source"},
		map[int64]string{high.id: "high", low.id: "low"},
	)

	plans, err := planCityServiceSettlementsWithNetworks([]cityServiceSettlementPlanInput{
		{Demand: low, Connections: []cityServiceSettlementConnectionInput{{
			Connection: cityServiceTestConnection(102, "connection.low.network", capacity.id, low.id, 1_000, 0, 10),
			Capacity:   capacity, Facility: facility,
		}}},
		{Demand: high, Connections: []cityServiceSettlementConnectionInput{{
			Connection: cityServiceTestConnection(101, "connection.high.network", capacity.id, high.id, 1_000, 0, 10),
			Capacity:   capacity, Facility: facility,
		}}},
	}, networks)
	require.NoError(t, err)
	require.Len(t, plans, 2)
	require.Equal(t, "demand.high.network", plans[0].DemandCode)
	require.EqualValues(t, 700, plans[0].DeliveredUnits)
	require.Equal(t, "demand.low.network", plans[1].DemandCode)
	require.EqualValues(t, 300, plans[1].DeliveredUnits)
	require.EqualValues(t, 400, plans[1].ShortageUnits)
	require.Zero(t, networks.networks["electric_power"].graph.remaining["edge_trunk"])
}

func TestPlanCityServiceSettlementsTreatsUnavailableNetworkPathAsShortage(t *testing.T) {
	demand := cityServiceTestDemand(1, "demand.unreachable", 400, 500)
	capacity := cityServiceTestCapacity(10, 1_000)
	facility := cityServiceTestFacility(20, CityFacilityStatusOperational)
	networks := cityServiceTestPhysicalNetworkPlanningState(t,
		[]string{"source", "sink"}, nil,
		map[int64]string{capacity.id: "source"}, map[int64]string{demand.id: "sink"},
	)

	plans, err := planCityServiceSettlementsWithNetworks([]cityServiceSettlementPlanInput{{
		Demand: demand,
		Connections: []cityServiceSettlementConnectionInput{{
			Connection: cityServiceTestConnection(101, "connection.unreachable", capacity.id, demand.id, 1_000, 0, 10),
			Capacity:   capacity, Facility: facility,
		}},
	}}, networks)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Empty(t, plans[0].Allocations)
	require.Zero(t, plans[0].DeliveredUnits)
	require.EqualValues(t, 400, plans[0].ShortageUnits)
}

func TestPlanCityServiceSettlementsPreservesLegacyAllocationForOptionalNetwork(t *testing.T) {
	demand := cityServiceTestDemand(1, "demand.optional", 450, 500)
	capacity := cityServiceTestCapacity(10, 1_000)
	facility := cityServiceTestFacility(20, CityFacilityStatusOperational)
	networks := &cityServicePhysicalNetworkPlanningState{
		required: map[string]bool{"electric_power": false},
		networks: make(map[string]*cityServicePhysicalNetworkPlan),
	}

	plans, err := planCityServiceSettlementsWithNetworks([]cityServiceSettlementPlanInput{{
		Demand: demand,
		Connections: []cityServiceSettlementConnectionInput{{
			Connection: cityServiceTestConnection(101, "connection.optional", capacity.id, demand.id, 1_000, 100, 10),
			Capacity:   capacity, Facility: facility,
		}},
	}}, networks)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Len(t, plans[0].Allocations, 1)
	allocation := plans[0].Allocations[0]
	require.Empty(t, allocation.NetworkCode)
	require.Empty(t, allocation.NetworkPaths)
	require.EqualValues(t, 500, allocation.DispatchedUnits)
	require.EqualValues(t, 450, allocation.DeliveredUnits)
	require.EqualValues(t, 50, allocation.ConnectionLossUnits)
}

func TestPlanCityServiceSettlementsRejectsDuplicateConnectionProjection(t *testing.T) {
	demand := cityServiceTestDemand(1, "demand.duplicate", 400, 500)
	capacity := cityServiceTestCapacity(10, 1_000)
	facility := cityServiceTestFacility(20, CityFacilityStatusOperational)
	connection := cityServiceTestConnection(101, "connection.duplicate", capacity.id, demand.id, 1_000, 0, 10)

	_, err := planCityServiceSettlements([]cityServiceSettlementPlanInput{{
		Demand: demand,
		Connections: []cityServiceSettlementConnectionInput{
			{Connection: connection, Capacity: capacity, Facility: facility},
			{Connection: connection, Capacity: capacity, Facility: facility},
		},
	}})
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func TestNormalizeCityServiceCommandEnforcesOverflowSafeUnitBound(t *testing.T) {
	accepted := json.RawMessage(`{
		"facility_code":"facility.power",
		"service_code":"electric_power",
		"installed_capacity_units":922337203685477,
		"availability_milli":1000,
		"expected_version":0
	}`)
	_, handled, err := normalizeCityServiceCommand(CityCommandTypeFacilityCapacityConfigure, accepted)
	require.True(t, handled)
	require.NoError(t, err)

	rejected := json.RawMessage(`{
		"facility_code":"facility.power",
		"service_code":"electric_power",
		"installed_capacity_units":922337203685478,
		"availability_milli":1000,
		"expected_version":0
	}`)
	_, handled, err = normalizeCityServiceCommand(CityCommandTypeFacilityCapacityConfigure, rejected)
	require.True(t, handled)
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestReduceCityServiceFactReplaysFacilityVersionChain(t *testing.T) {
	state := &cityPublicServiceHashState{
		Profile:    CityServiceProfile{FactCount: 0, Revision: 1},
		Facilities: make([]CityFacility, 0), Facts: make([]CityServiceFact, 0),
	}
	commandSequence := int64(7)
	registered := CityFacility{
		Code: "facility.power", Name: "Power", FacilityTypeCode: "power_plant",
		Status: CityFacilityStatusOffline, ReliabilityMilli: 950,
		CreatedTick: 1, UpdatedTick: 1, Version: 1,
		SourceFactTick: 1, SourceFactSequence: 1, Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	fact := CityServiceFact{
		Tick: 1, Sequence: 1, SourceCommandSequence: &commandSequence,
		FactType: CityServiceFactFacilityRegistered, SubjectKind: "facility",
		SubjectCode: registered.Code, VersionBefore: 0, VersionAfter: 1,
		Payload: mustCityServiceTestJSON(t, cityServiceFacilityFactPayload{
			SchemaVersion: 1, FacilityAfter: registered,
		}),
	}
	require.NoError(t, reduceCityServiceFact(state, fact))
	require.EqualValues(t, 1, state.Profile.FacilityCount)
	require.EqualValues(t, 1, state.Profile.FactCount)
	require.EqualValues(t, 2, state.Profile.Revision)
	require.Equal(t, registered, state.Facilities[0])
	state.Capacities = append(state.Capacities, CityFacilityServiceCapacity{
		FacilityCode: registered.Code, ServiceCode: "electric_power",
		AvailableCapacityUnits: 1_000, DispatchCapacityUnits: 0,
	})

	before := registered
	before.SourceFactTick, before.SourceFactSequence = 0, 0
	operational := registered
	operational.Status = CityFacilityStatusOperational
	operational.UpdatedTick = 2
	operational.Version = 2
	operational.SourceFactTick = 2
	operational.SourceFactSequence = 1
	transitionSequence := int64(8)
	transition := CityServiceFact{
		Tick: 2, Sequence: 1, SourceCommandSequence: &transitionSequence,
		FactType: CityServiceFactFacilityStatusChanged, SubjectKind: "facility",
		SubjectCode: operational.Code, VersionBefore: 1, VersionAfter: 2,
		Payload: mustCityServiceTestJSON(t, cityServiceFacilityFactPayload{
			SchemaVersion: 1, FacilityBefore: &before, FacilityAfter: operational,
		}),
	}
	require.NoError(t, reduceCityServiceFact(state, transition))
	require.Equal(t, CityFacilityStatusOperational, state.Facilities[0].Status)
	require.EqualValues(t, 1_000, state.Capacities[0].DispatchCapacityUnits)
	require.EqualValues(t, 2, state.Profile.FactCount)
	require.EqualValues(t, 3, state.Profile.Revision)
}

func TestReduceCityServiceFactValidatesSettlementProjectionSnapshots(t *testing.T) {
	state := cityServiceSettlementReplayTestState()
	settlement := CityServiceSettlement{
		Tick: 2, Sequence: 1, ServiceCode: "electric_power", DemandCode: "demand.hospital",
		DemandVersion: 1, RequestedUnits: 900, DeliveredUnits: 900,
		ShortageUnits: 0, AllocationCount: 1, QualityMilli: 1000,
		Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	allocation := CityServiceAllocation{
		Tick: 2, Sequence: 1, AllocationIndex: 1,
		ServiceCode: "electric_power", FacilityCode: "facility.power",
		DemandCode: "demand.hospital", ConnectionCode: "connection.hospital",
		CapacityVersion: 1, DemandVersion: 1, ConnectionVersion: 1,
		FacilityCapacityUnits: 1_000, ConnectionCapacityUnits: 1_000,
		LossMilli: 100, DispatchedUnits: 1_000, DeliveredUnits: 900,
		LossUnits: 100, Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	fact := CityServiceFact{
		Tick: 2, Sequence: 1, FactType: CityServiceFactAllocationSettled,
		SubjectKind: "settlement", SubjectCode: "demand.hospital.2",
		Payload: mustCityServiceTestJSON(t, cityServiceSettlementFactPayload{
			SchemaVersion: 1, Settlement: settlement,
			Allocations: []CityServiceAllocation{allocation},
		}),
	}
	require.NoError(t, reduceCityServiceFact(state, fact))
	require.Len(t, state.Settlements, 1)
	require.Len(t, state.Allocations, 1)
	require.EqualValues(t, 1, state.Profile.SettlementCount)
	require.EqualValues(t, 1, state.Profile.AllocationCount)

	tamperedState := cityServiceSettlementReplayTestState()
	allocation.DeliveredUnits = 899
	allocation.LossUnits = 101
	fact.Payload = mustCityServiceTestJSON(t, cityServiceSettlementFactPayload{
		SchemaVersion: 1, Settlement: settlement,
		Allocations: []CityServiceAllocation{allocation},
	})
	require.Error(t, reduceCityServiceFact(tamperedState, fact))
}

func TestReduceCityServiceFactUsesEffectiveDispatchCapacitySnapshot(t *testing.T) {
	state := cityServiceSettlementReplayTestState()
	state.Capacities[0].DispatchCapacityUnits = 800
	state.Demands[0].RequestedUnitsPerTick = 720
	settlement := CityServiceSettlement{
		Tick: 2, Sequence: 1, ServiceCode: "electric_power", DemandCode: "demand.hospital",
		DemandVersion: 1, RequestedUnits: 720, DeliveredUnits: 720,
		ShortageUnits: 0, AllocationCount: 1, QualityMilli: 1000,
		Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	allocation := CityServiceAllocation{
		Tick: 2, Sequence: 1, AllocationIndex: 1,
		ServiceCode: "electric_power", FacilityCode: "facility.power",
		DemandCode: "demand.hospital", ConnectionCode: "connection.hospital",
		CapacityVersion: 1, DemandVersion: 1, ConnectionVersion: 1,
		FacilityCapacityUnits: 800, ConnectionCapacityUnits: 1_000,
		LossMilli: 100, DispatchedUnits: 800, DeliveredUnits: 720,
		LossUnits: 80, Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	fact := CityServiceFact{
		Tick: 2, Sequence: 1, FactType: CityServiceFactAllocationSettled,
		SubjectKind: "settlement", SubjectCode: "demand.hospital.2",
		Payload: mustCityServiceTestJSON(t, cityServiceSettlementFactPayload{
			SchemaVersion: 1, Settlement: settlement,
			Allocations: []CityServiceAllocation{allocation},
		}),
	}
	require.NoError(t, reduceCityServiceFact(state, fact))

	state = cityServiceSettlementReplayTestState()
	state.Capacities[0].DispatchCapacityUnits = 800
	state.Demands[0].RequestedUnitsPerTick = 720
	allocation.FacilityCapacityUnits = state.Capacities[0].AvailableCapacityUnits
	fact.Payload = mustCityServiceTestJSON(t, cityServiceSettlementFactPayload{
		SchemaVersion: 1, Settlement: settlement,
		Allocations: []CityServiceAllocation{allocation},
	})
	require.ErrorContains(t, reduceCityServiceFact(state, fact), "projection snapshot")
}

func cityServiceSettlementReplayTestState() *cityPublicServiceHashState {
	return &cityPublicServiceHashState{
		Profile: CityServiceProfile{FactCount: 0, Revision: 1},
		Facilities: []CityFacility{{
			Code: "facility.power", Status: CityFacilityStatusOperational, Version: 1,
		}},
		Capacities: []CityFacilityServiceCapacity{{
			FacilityCode: "facility.power", ServiceCode: "electric_power",
			InstalledCapacityUnits: 1_000, AvailabilityMilli: 1000,
			AvailableCapacityUnits: 1_000, DispatchCapacityUnits: 1_000, Version: 1,
		}},
		Demands: []CityServiceDemand{{
			Code: "demand.hospital", ServiceCode: "electric_power",
			RequestedUnitsPerTick: 900, Status: CityServiceProjectionStatusActive, Version: 1,
		}},
		Connections: []CityServiceConnection{{
			Code: "connection.hospital", FacilityCode: "facility.power",
			ServiceCode: "electric_power", DemandCode: "demand.hospital",
			MaxFlowUnitsPerTick: 1_000, LossMilli: 100,
			Status: CityServiceProjectionStatusActive, Version: 1,
		}},
		Facts: make([]CityServiceFact, 0), Allocations: make([]CityServiceAllocation, 0),
		Settlements: make([]CityServiceSettlement, 0),
	}
}

func mustCityServiceTestJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(value)
	require.NoError(t, err)
	return payload
}

func cityServiceTestDemand(id int64, code string, requested int64, priority int) cityServiceDemandRef {
	return cityServiceDemandRef{
		id: id, code: code, serviceDefinitionID: 1, serviceCode: "electric_power",
		requestedUnitsPerTick: requested, priority: priority,
		status: CityServiceProjectionStatusActive, createdTick: id, version: 1,
	}
}

func cityServiceTestCapacity(id, available int64) cityServiceCapacityRef {
	return cityServiceCapacityRef{
		id: id, facilityID: id + 10, facilityCode: "facility.test",
		serviceDefinitionID: 1, serviceCode: "electric_power",
		installedCapacityUnits: available, availabilityMilli: 1000,
		availableCapacityUnits: available, version: 1,
	}
}

func cityServiceTestFacility(id int64, status string) cityFacilityRef {
	return cityFacilityRef{id: id, code: "facility.test", status: status, version: 1}
}

func cityServiceTestConnection(
	id int64,
	code string,
	capacityID, demandID, maxFlow int64,
	lossMilli, preference int,
) cityServiceConnectionRef {
	return cityServiceConnectionRef{
		id: id, code: code, capacityID: capacityID, demandID: demandID,
		maxFlowUnitsPerTick: maxFlow, lossMilli: lossMilli, preference: preference,
		status: CityServiceProjectionStatusActive, version: 1,
	}
}

func cityServiceTestPhysicalNetworkPlanningState(
	t *testing.T,
	nodeCodes []string,
	edges []cityNetworkPlannerEdge,
	nodesByCapacity map[int64]string,
	nodesByDemand map[int64]string,
) *cityServicePhysicalNetworkPlanningState {
	t.Helper()
	graph, err := newCityNetworkResidualGraph(nodeCodes, edges, testCityNetworkPlannerPolicy())
	require.NoError(t, err)
	return &cityServicePhysicalNetworkPlanningState{
		required: map[string]bool{"electric_power": true},
		networks: map[string]*cityServicePhysicalNetworkPlan{
			"electric_power": {
				code: "network.electric_power", serviceCode: "electric_power",
				topologyRevision: 1, routeDirection: CityNetworkRouteSupplyToDemand,
				policy: testCityNetworkPlannerPolicy(), graph: graph,
				nodesByCapacity: nodesByCapacity, nodesByDemand: nodesByDemand,
			},
		},
	}
}
