package service

import (
	"encoding/json"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CitySimulationVersionF8V3 = "city-f8-v3"

	CityNetworkStatusActive    = "active"
	CityNetworkStatusSuspended = "suspended"
	CityNetworkStatusRetired   = "retired"

	CityNetworkNodeRoleSupply   = "supply"
	CityNetworkNodeRoleDemand   = "demand"
	CityNetworkNodeRoleJunction = "junction"
	CityNetworkNodeRoleStorage  = "storage"
	CityNetworkNodeRoleGateway  = "gateway"

	CityNetworkNodeStatusActive  = "active"
	CityNetworkNodeStatusOffline = "offline"
	CityNetworkNodeStatusRetired = "retired"

	CityNetworkEdgeDirectionDirected      = "directed"
	CityNetworkEdgeDirectionBidirectional = "bidirectional"

	CityNetworkEdgeStatusActive   = "active"
	CityNetworkEdgeStatusIsolated = "isolated"
	CityNetworkEdgeStatusFailed   = "failed"
	CityNetworkEdgeStatusRetired  = "retired"

	CityNetworkRouteSupplyToDemand   = "supply_to_demand"
	CityNetworkRouteDemandToFacility = "demand_to_facility"

	CityPhysicalNetworkPhasePreNetwork = "pre_network"
	CityPhysicalNetworkPhaseSettlement = "settlement"

	CityPhysicalNetworkFactTopologySynchronized = "network.topology_synchronized"
	CityPhysicalNetworkFactFlowSettled          = "network.flow_settled"
	CityPhysicalNetworkFactNetworkConfigured    = "network.configured"
	CityPhysicalNetworkFactNodeConfigured       = "node.configured"
	CityPhysicalNetworkFactEdgeConfigured       = "edge.configured"
	CityPhysicalNetworkFactEdgeStateChanged     = "edge.state_changed"

	cityPhysicalNetworkPolicyID      = "sub2api-physical-networks"
	cityPhysicalNetworkPolicyVersion = "1.0.0"
	cityPhysicalNetworkSchemaVersion = 1

	cityPhysicalNetworkMaximumNodes           = 10_000
	cityPhysicalNetworkMaximumNetworks        = 10_000
	cityPhysicalNetworkMaximumEdges           = 50_000
	cityPhysicalNetworkMaximumPathsPerRequest = 32
	cityPhysicalNetworkMaximumHopsPerPath     = 128
	cityPhysicalNetworkMaximumSearchLabels    = 250_000
)

var ErrCityPhysicalNetworkStateNotFound = infraerrors.NotFound(
	"CITY_PHYSICAL_NETWORK_STATE_NOT_FOUND", "city physical network state not found",
)

type CityPhysicalNetworkProfile struct {
	PolicyID      string          `json:"policy_id"`
	PolicyVersion string          `json:"policy_version"`
	PolicyHash    string          `json:"policy_hash"`
	BaselineTick  int64           `json:"baseline_tick"`
	PolicyCount   int64           `json:"policy_count"`
	NetworkCount  int64           `json:"network_count"`
	NodeCount     int64           `json:"node_count"`
	EdgeCount     int64           `json:"edge_count"`
	FactCount     int64           `json:"fact_count"`
	BatchCount    int64           `json:"batch_count"`
	PathCount     int64           `json:"path_count"`
	SegmentCount  int64           `json:"segment_count"`
	Revision      int64           `json:"revision"`
	Metadata      json.RawMessage `json:"metadata"`
}

type CityPhysicalNetworkPolicy struct {
	ServiceCode        string          `json:"service_code"`
	PolicyVersion      string          `json:"policy_version"`
	PolicyHash         string          `json:"policy_hash"`
	NetworkRequired    bool            `json:"network_required"`
	RouteDirection     string          `json:"route_direction"`
	MaximumNodes       int             `json:"maximum_nodes"`
	MaximumEdges       int             `json:"maximum_edges"`
	MaximumPaths       int             `json:"maximum_paths"`
	MaximumHops        int             `json:"maximum_hops"`
	LossCostWeight     int64           `json:"loss_cost_weight"`
	AllowBidirectional bool            `json:"allow_bidirectional"`
	AlgorithmVersion   string          `json:"algorithm_version"`
	Payload            json.RawMessage `json:"payload"`
}

type CityPhysicalNetwork struct {
	Code               string          `json:"code"`
	Name               string          `json:"name"`
	ServiceCode        string          `json:"service_code"`
	Status             string          `json:"status"`
	TopologyRevision   int64           `json:"topology_revision"`
	CreatedTick        int64           `json:"created_tick"`
	UpdatedTick        int64           `json:"updated_tick"`
	Version            int64           `json:"version"`
	SourceFactTick     int64           `json:"source_fact_tick"`
	SourceFactSequence int64           `json:"source_fact_sequence"`
	Metadata           json.RawMessage `json:"metadata"`
}

type CityPhysicalNetworkNode struct {
	Code               string          `json:"code"`
	NetworkCode        string          `json:"network_code"`
	Role               string          `json:"role"`
	CapacityCode       *string         `json:"capacity_code,omitempty"`
	DemandCode         *string         `json:"demand_code,omitempty"`
	DistrictCode       *string         `json:"district_code,omitempty"`
	BuildingCode       *string         `json:"building_code,omitempty"`
	WorldX             *int64          `json:"world_x,omitempty"`
	WorldY             *int64          `json:"world_y,omitempty"`
	WorldZ             *int            `json:"world_z,omitempty"`
	Status             string          `json:"status"`
	CreatedTick        int64           `json:"created_tick"`
	UpdatedTick        int64           `json:"updated_tick"`
	Version            int64           `json:"version"`
	SourceFactTick     int64           `json:"source_fact_tick"`
	SourceFactSequence int64           `json:"source_fact_sequence"`
	Metadata           json.RawMessage `json:"metadata"`
}

type CityPhysicalNetworkEdge struct {
	Code                   string          `json:"code"`
	NetworkCode            string          `json:"network_code"`
	FromNodeCode           string          `json:"from_node_code"`
	ToNodeCode             string          `json:"to_node_code"`
	Direction              string          `json:"direction"`
	InstalledCapacityUnits int64           `json:"installed_capacity_units"`
	AvailabilityMilli      int             `json:"availability_milli"`
	AvailableCapacityUnits int64           `json:"available_capacity_units"`
	LossMilli              int             `json:"loss_milli"`
	BaseCostUnits          int64           `json:"base_cost_units"`
	Status                 string          `json:"status"`
	ConditionMilli         int             `json:"condition_milli"`
	FailureCount           int64           `json:"failure_count"`
	CreatedTick            int64           `json:"created_tick"`
	UpdatedTick            int64           `json:"updated_tick"`
	Version                int64           `json:"version"`
	SourceFactTick         int64           `json:"source_fact_tick"`
	SourceFactSequence     int64           `json:"source_fact_sequence"`
	Metadata               json.RawMessage `json:"metadata"`
}

type CityPhysicalNetworkFact struct {
	Tick                  int64           `json:"tick"`
	Sequence              int64           `json:"sequence"`
	Phase                 string          `json:"phase"`
	SourceCommandSequence *int64          `json:"source_command_sequence,omitempty"`
	FactType              string          `json:"fact_type"`
	SubjectKind           string          `json:"subject_kind"`
	SubjectCode           string          `json:"subject_code"`
	VersionBefore         int64           `json:"version_before"`
	VersionAfter          int64           `json:"version_after"`
	Payload               json.RawMessage `json:"payload"`
}

type CityPhysicalNetworkFlowBatch struct {
	Tick                 int64           `json:"tick"`
	Sequence             int64           `json:"sequence"`
	NetworkCode          string          `json:"network_code"`
	ServiceCode          string          `json:"service_code"`
	TopologyRevision     int64           `json:"topology_revision"`
	AllocationCount      int             `json:"allocation_count"`
	PathCount            int             `json:"path_count"`
	SegmentCount         int             `json:"segment_count"`
	DispatchedUnits      int64           `json:"dispatched_units"`
	NetworkReceivedUnits int64           `json:"network_received_units"`
	NetworkLossUnits     int64           `json:"network_loss_units"`
	SourceFactTick       int64           `json:"source_fact_tick"`
	SourceFactSequence   int64           `json:"source_fact_sequence"`
	Metadata             json.RawMessage `json:"metadata"`
}

type CityPhysicalNetworkFlowPath struct {
	Tick                 int64           `json:"tick"`
	Sequence             int64           `json:"sequence"`
	ServiceSequence      int64           `json:"service_sequence"`
	AllocationIndex      int             `json:"allocation_index"`
	PathIndex            int             `json:"path_index"`
	NetworkCode          string          `json:"network_code"`
	ConnectionCode       string          `json:"connection_code"`
	SourceNodeCode       string          `json:"source_node_code"`
	SinkNodeCode         string          `json:"sink_node_code"`
	HopCount             int             `json:"hop_count"`
	DispatchedUnits      int64           `json:"dispatched_units"`
	NetworkReceivedUnits int64           `json:"network_received_units"`
	NetworkLossUnits     int64           `json:"network_loss_units"`
	PathCostUnits        int64           `json:"path_cost_units"`
	PathHash             string          `json:"path_hash"`
	Metadata             json.RawMessage `json:"metadata"`
}

type CityPhysicalNetworkFlowSegment struct {
	Tick              int64           `json:"tick"`
	Sequence          int64           `json:"sequence"`
	ServiceSequence   int64           `json:"service_sequence"`
	AllocationIndex   int             `json:"allocation_index"`
	PathIndex         int             `json:"path_index"`
	SegmentIndex      int             `json:"segment_index"`
	EdgeCode          string          `json:"edge_code"`
	EdgeVersion       int64           `json:"edge_version"`
	Direction         string          `json:"direction"`
	FromNodeCode      string          `json:"from_node_code"`
	ToNodeCode        string          `json:"to_node_code"`
	EdgeCapacityUnits int64           `json:"edge_capacity_units"`
	LossMilli         int             `json:"loss_milli"`
	InputUnits        int64           `json:"input_units"`
	OutputUnits       int64           `json:"output_units"`
	LossUnits         int64           `json:"loss_units"`
	Metadata          json.RawMessage `json:"metadata"`
}

type CityPhysicalNetworkStateSet struct {
	Profile  CityPhysicalNetworkProfile       `json:"profile"`
	Policies []CityPhysicalNetworkPolicy      `json:"policies"`
	Networks []CityPhysicalNetwork            `json:"networks"`
	Nodes    []CityPhysicalNetworkNode        `json:"nodes"`
	Edges    []CityPhysicalNetworkEdge        `json:"edges"`
	Facts    []CityPhysicalNetworkFact        `json:"facts"`
	Batches  []CityPhysicalNetworkFlowBatch   `json:"batches"`
	Paths    []CityPhysicalNetworkFlowPath    `json:"paths"`
	Segments []CityPhysicalNetworkFlowSegment `json:"segments"`
}

type cityPhysicalNetworkHashState = CityPhysicalNetworkStateSet
