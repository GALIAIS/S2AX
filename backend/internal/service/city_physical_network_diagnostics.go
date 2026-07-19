package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

const cityPhysicalNetworkMaximumDiagnosticEdges = 100

type CityPhysicalNetworkComponentDiagnostic struct {
	Index           int      `json:"index"`
	NodeCount       int      `json:"node_count"`
	EdgeCount       int      `json:"edge_count"`
	SupplyNodeCount int      `json:"supply_node_count"`
	DemandNodeCount int      `json:"demand_node_count"`
	NodeCodes       []string `json:"node_codes"`
	ServiceIsland   bool     `json:"service_island"`
}

type CityPhysicalNetworkEdgeDiagnostic struct {
	EdgeCode               string `json:"edge_code"`
	Status                 string `json:"status"`
	AvailableCapacityUnits int64  `json:"available_capacity_units"`
	LatestInputUnits       int64  `json:"latest_input_units"`
	LatestOutputUnits      int64  `json:"latest_output_units"`
	LatestLossUnits        int64  `json:"latest_loss_units"`
	UtilizationMilli       int    `json:"utilization_milli"`
	Saturated              bool   `json:"saturated"`
	Bottleneck             bool   `json:"bottleneck"`
}

type CityPhysicalNetworkDiagnosticSegment struct {
	Index             int    `json:"index"`
	EdgeCode          string `json:"edge_code"`
	Direction         string `json:"direction"`
	FromNodeCode      string `json:"from_node_code"`
	ToNodeCode        string `json:"to_node_code"`
	EdgeCapacityUnits int64  `json:"edge_capacity_units"`
	LossMilli         int    `json:"loss_milli"`
	InputUnits        int64  `json:"input_units"`
	OutputUnits       int64  `json:"output_units"`
	LossUnits         int64  `json:"loss_units"`
}

type CityPhysicalNetworkDiagnosticPath struct {
	Index                int                                    `json:"index"`
	CostUnits            int64                                  `json:"cost_units"`
	DispatchedUnits      int64                                  `json:"dispatched_units"`
	NetworkReceivedUnits int64                                  `json:"network_received_units"`
	NetworkLossUnits     int64                                  `json:"network_loss_units"`
	PathHash             string                                 `json:"path_hash"`
	Segments             []CityPhysicalNetworkDiagnosticSegment `json:"segments"`
}

type CityPhysicalNetworkRouteDiagnostic struct {
	SourceNodeCode       string                              `json:"source_node_code"`
	SinkNodeCode         string                              `json:"sink_node_code"`
	ProbeUnits           int64                               `json:"probe_units"`
	Reachable            bool                                `json:"reachable"`
	ReasonCode           string                              `json:"reason_code"`
	DispatchedUnits      int64                               `json:"dispatched_units"`
	NetworkReceivedUnits int64                               `json:"network_received_units"`
	NetworkLossUnits     int64                               `json:"network_loss_units"`
	Paths                []CityPhysicalNetworkDiagnosticPath `json:"paths"`
}

type CityPhysicalNetworkDiagnosticsView struct {
	Availability                 string                                   `json:"availability"`
	SimulationVersion            string                                   `json:"simulation_version"`
	RequiredVersion              string                                   `json:"required_version"`
	Network                      *CityPhysicalNetwork                     `json:"network,omitempty"`
	Policy                       *CityPhysicalNetworkPolicy               `json:"policy,omitempty"`
	LatestFlowTick               *int64                                   `json:"latest_flow_tick,omitempty"`
	ActiveNodeCount              int                                      `json:"active_node_count"`
	ActiveEdgeCount              int                                      `json:"active_edge_count"`
	ComponentCount               int                                      `json:"component_count"`
	IsolatedNodeCount            int                                      `json:"isolated_node_count"`
	ServiceIslandCount           int                                      `json:"service_island_count"`
	BottleneckEdgeCount          int                                      `json:"bottleneck_edge_count"`
	SaturatedEdgeCount           int                                      `json:"saturated_edge_count"`
	Components                   []CityPhysicalNetworkComponentDiagnostic `json:"components"`
	EdgeDiagnostics              []CityPhysicalNetworkEdgeDiagnostic      `json:"edge_diagnostics"`
	TruncatedEdgeDiagnosticCount int                                      `json:"truncated_edge_diagnostic_count"`
	Route                        *CityPhysicalNetworkRouteDiagnostic      `json:"route,omitempty"`
}

type cityPhysicalNetworkDiagnosticNode struct {
	code   string
	role   string
	status string
}

type cityPhysicalNetworkDiagnosticEdge struct {
	value  CityPhysicalNetworkEdge
	input  int64
	output int64
	loss   int64
}

func (s *CityEconomyService) GetCityPhysicalNetworkDiagnostics(
	ctx context.Context, input CityPhysicalNetworkQueryInput,
) (*CityPhysicalNetworkDiagnosticsView, error) {
	if err := normalizeCityPhysicalNetworkQuery(&input, "diagnostic"); err != nil {
		return nil, err
	}
	version, available, err := s.cityPhysicalNetworkQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	view := &CityPhysicalNetworkDiagnosticsView{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion: CitySimulationVersionF8V3,
		Components:      make([]CityPhysicalNetworkComponentDiagnostic, 0),
		EdgeDiagnostics: make([]CityPhysicalNetworkEdgeDiagnostic, 0),
	}
	if !available {
		return view, nil
	}
	networkID, network, policy, err := loadCityPhysicalNetworkDiagnosticIdentity(
		ctx, s.db, input.WorldID, input.NetworkCode,
	)
	if err != nil {
		return nil, err
	}
	nodes, edges, err := loadCityPhysicalNetworkDiagnosticTopology(
		ctx, s.db, input.WorldID, networkID,
	)
	if err != nil {
		return nil, err
	}
	latestTick, err := loadCityPhysicalNetworkDiagnosticUtilization(
		ctx, s.db, input.WorldID, networkID, edges,
	)
	if err != nil {
		return nil, err
	}
	view.Availability = CityServiceAvailabilityAvailable
	view.Network = &network
	view.Policy = &policy
	view.LatestFlowTick = latestTick
	view.Components, view.IsolatedNodeCount = buildCityPhysicalNetworkComponents(nodes, edges)
	view.ComponentCount = len(view.Components)
	for _, component := range view.Components {
		view.ActiveNodeCount += component.NodeCount
		view.ActiveEdgeCount += component.EdgeCount
		if component.ServiceIsland {
			view.ServiceIslandCount++
		}
	}
	view.EdgeDiagnostics, view.BottleneckEdgeCount, view.SaturatedEdgeCount,
		view.TruncatedEdgeDiagnosticCount = buildCityPhysicalNetworkEdgeDiagnostics(edges)
	if input.SourceNodeCode != "" {
		view.Route, err = diagnoseCityPhysicalNetworkRoute(
			nodes, edges, policy, input.SourceNodeCode, input.SinkNodeCode, input.ProbeUnits,
		)
		if err != nil {
			return nil, err
		}
	}
	return view, nil
}

func loadCityPhysicalNetworkDiagnosticIdentity(
	ctx context.Context, queryer citySQLQueryer, worldID int64, networkCode string,
) (int64, CityPhysicalNetwork, CityPhysicalNetworkPolicy, error) {
	var networkID int64
	var network CityPhysicalNetwork
	var policy CityPhysicalNetworkPolicy
	err := queryer.QueryRowContext(ctx, `
SELECT network.id, network.code, network.name, service.code, network.status,
       network.topology_revision, network.created_tick, network.updated_tick,
       network.version, COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0),
       network.metadata, policy.policy_version, policy.policy_hash,
       policy.network_required, policy.route_direction, policy.maximum_nodes,
       policy.maximum_edges, policy.maximum_paths, policy.maximum_hops,
       policy.loss_cost_weight, policy.allow_bidirectional,
       policy.algorithm_version, policy.payload
FROM city_physical_networks network
JOIN city_service_definitions service ON service.id = network.service_definition_id
JOIN city_physical_network_policies policy
  ON policy.world_id = network.world_id
 AND policy.service_definition_id = network.service_definition_id
LEFT JOIN city_physical_network_facts fact ON fact.id = network.source_fact_id
WHERE network.world_id = $1 AND network.code = $2`, worldID, networkCode).Scan(
		&networkID, &network.Code, &network.Name, &network.ServiceCode, &network.Status,
		&network.TopologyRevision, &network.CreatedTick, &network.UpdatedTick,
		&network.Version, &network.SourceFactTick, &network.SourceFactSequence,
		&network.Metadata, &policy.PolicyVersion, &policy.PolicyHash,
		&policy.NetworkRequired, &policy.RouteDirection, &policy.MaximumNodes,
		&policy.MaximumEdges, &policy.MaximumPaths, &policy.MaximumHops,
		&policy.LossCostWeight, &policy.AllowBidirectional,
		&policy.AlgorithmVersion, &policy.Payload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, network, policy, ErrCityPhysicalNetworkStateNotFound
	}
	if err != nil {
		return 0, network, policy, fmt.Errorf("load physical network diagnostic identity: %w", err)
	}
	policy.ServiceCode = network.ServiceCode
	return networkID, network, policy, nil
}

func loadCityPhysicalNetworkDiagnosticTopology(
	ctx context.Context, queryer citySQLQueryer, worldID, networkID int64,
) ([]cityPhysicalNetworkDiagnosticNode, []*cityPhysicalNetworkDiagnosticEdge, error) {
	nodeRows, err := queryer.QueryContext(ctx, `
SELECT code, role, status FROM city_physical_network_nodes
WHERE world_id = $1 AND network_id = $2 ORDER BY code`, worldID, networkID)
	if err != nil {
		return nil, nil, fmt.Errorf("load physical network diagnostic nodes: %w", err)
	}
	nodes := make([]cityPhysicalNetworkDiagnosticNode, 0)
	for nodeRows.Next() {
		var item cityPhysicalNetworkDiagnosticNode
		if err = nodeRows.Scan(&item.code, &item.role, &item.status); err != nil {
			_ = nodeRows.Close()
			return nil, nil, fmt.Errorf("scan physical network diagnostic node: %w", err)
		}
		nodes = append(nodes, item)
	}
	if err = closeCityRows(nodeRows, "iterate physical network diagnostic nodes"); err != nil {
		return nil, nil, err
	}
	edgeRows, err := queryer.QueryContext(ctx, `
SELECT edge.code, source.code, sink.code, edge.direction,
       edge.installed_capacity_units, edge.availability_milli,
       edge.available_capacity_units, edge.loss_milli, edge.base_cost_units,
       edge.status, edge.condition_milli, edge.failure_count, edge.version
FROM city_physical_network_edges edge
JOIN city_physical_network_nodes source ON source.id = edge.from_node_id
JOIN city_physical_network_nodes sink ON sink.id = edge.to_node_id
WHERE edge.world_id = $1 AND edge.network_id = $2 ORDER BY edge.code`, worldID, networkID)
	if err != nil {
		return nil, nil, fmt.Errorf("load physical network diagnostic edges: %w", err)
	}
	edges := make([]*cityPhysicalNetworkDiagnosticEdge, 0)
	for edgeRows.Next() {
		item := &cityPhysicalNetworkDiagnosticEdge{}
		if err = edgeRows.Scan(
			&item.value.Code, &item.value.FromNodeCode, &item.value.ToNodeCode,
			&item.value.Direction, &item.value.InstalledCapacityUnits,
			&item.value.AvailabilityMilli, &item.value.AvailableCapacityUnits,
			&item.value.LossMilli, &item.value.BaseCostUnits, &item.value.Status,
			&item.value.ConditionMilli, &item.value.FailureCount, &item.value.Version,
		); err != nil {
			_ = edgeRows.Close()
			return nil, nil, fmt.Errorf("scan physical network diagnostic edge: %w", err)
		}
		edges = append(edges, item)
	}
	if err = closeCityRows(edgeRows, "iterate physical network diagnostic edges"); err != nil {
		return nil, nil, err
	}
	return nodes, edges, nil
}

func loadCityPhysicalNetworkDiagnosticUtilization(
	ctx context.Context, queryer citySQLQueryer, worldID, networkID int64,
	edges []*cityPhysicalNetworkDiagnosticEdge,
) (*int64, error) {
	var latest sql.NullInt64
	if err := queryer.QueryRowContext(ctx, `
SELECT MAX(tick) FROM city_physical_network_flow_batches
WHERE world_id = $1 AND network_id = $2`, worldID, networkID).Scan(&latest); err != nil {
		return nil, fmt.Errorf("load physical network diagnostic latest tick: %w", err)
	}
	if !latest.Valid {
		return nil, nil
	}
	index := make(map[string]*cityPhysicalNetworkDiagnosticEdge, len(edges))
	for _, edge := range edges {
		index[edge.value.Code] = edge
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT edge.code, COALESCE(SUM(segment.input_units), 0)::BIGINT,
       COALESCE(SUM(segment.output_units), 0)::BIGINT,
       COALESCE(SUM(segment.loss_units), 0)::BIGINT
FROM city_physical_network_flow_segments segment
JOIN city_physical_network_flow_paths path ON path.id = segment.path_id
JOIN city_physical_network_flow_batches batch ON batch.id = path.batch_id
JOIN city_physical_network_edges edge ON edge.id = segment.edge_id
WHERE segment.world_id = $1 AND batch.network_id = $2 AND batch.tick = $3
GROUP BY edge.code ORDER BY edge.code`, worldID, networkID, latest.Int64)
	if err != nil {
		return nil, fmt.Errorf("load physical network diagnostic utilization: %w", err)
	}
	for rows.Next() {
		var code string
		var input, output, loss int64
		if err = rows.Scan(&code, &input, &output, &loss); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan physical network diagnostic utilization: %w", err)
		}
		if edge := index[code]; edge != nil {
			edge.input, edge.output, edge.loss = input, output, loss
		}
	}
	if err = closeCityRows(rows, "iterate physical network diagnostic utilization"); err != nil {
		return nil, err
	}
	return int64Pointer(latest.Int64), nil
}

func buildCityPhysicalNetworkComponents(
	nodes []cityPhysicalNetworkDiagnosticNode,
	edges []*cityPhysicalNetworkDiagnosticEdge,
) ([]CityPhysicalNetworkComponentDiagnostic, int) {
	roles := make(map[string]string)
	adjacency := make(map[string][]string)
	for _, node := range nodes {
		if node.status != CityNetworkNodeStatusActive {
			continue
		}
		roles[node.code] = node.role
		adjacency[node.code] = make([]string, 0)
	}
	activeEdges := make([]*cityPhysicalNetworkDiagnosticEdge, 0)
	for _, edge := range edges {
		if edge.value.Status != CityNetworkEdgeStatusActive ||
			roles[edge.value.FromNodeCode] == "" || roles[edge.value.ToNodeCode] == "" {
			continue
		}
		adjacency[edge.value.FromNodeCode] = append(adjacency[edge.value.FromNodeCode], edge.value.ToNodeCode)
		adjacency[edge.value.ToNodeCode] = append(adjacency[edge.value.ToNodeCode], edge.value.FromNodeCode)
		activeEdges = append(activeEdges, edge)
	}
	for code := range adjacency {
		sort.Strings(adjacency[code])
	}
	codes := make([]string, 0, len(adjacency))
	for code := range adjacency {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	visited := make(map[string]bool, len(codes))
	components := make([]CityPhysicalNetworkComponentDiagnostic, 0)
	isolated := 0
	for _, start := range codes {
		if visited[start] {
			continue
		}
		queue := []string{start}
		visited[start] = true
		memberSet := make(map[string]bool)
		members := make([]string, 0)
		component := CityPhysicalNetworkComponentDiagnostic{Index: len(components) + 1}
		for len(queue) > 0 {
			code := queue[0]
			queue = queue[1:]
			members = append(members, code)
			memberSet[code] = true
			switch roles[code] {
			case CityNetworkNodeRoleSupply:
				component.SupplyNodeCount++
			case CityNetworkNodeRoleDemand:
				component.DemandNodeCount++
			}
			for _, neighbor := range adjacency[code] {
				if !visited[neighbor] {
					visited[neighbor] = true
					queue = append(queue, neighbor)
				}
			}
		}
		sort.Strings(members)
		component.NodeCount = len(members)
		if len(members) == 1 && len(adjacency[members[0]]) == 0 {
			isolated++
		}
		for _, edge := range activeEdges {
			if memberSet[edge.value.FromNodeCode] && memberSet[edge.value.ToNodeCode] {
				component.EdgeCount++
			}
		}
		component.ServiceIsland = component.SupplyNodeCount == 0 || component.DemandNodeCount == 0
		if len(members) > 32 {
			component.NodeCodes = append([]string(nil), members[:32]...)
		} else {
			component.NodeCodes = members
		}
		components = append(components, component)
	}
	return components, isolated
}

func buildCityPhysicalNetworkEdgeDiagnostics(
	edges []*cityPhysicalNetworkDiagnosticEdge,
) ([]CityPhysicalNetworkEdgeDiagnostic, int, int, int) {
	items := make([]CityPhysicalNetworkEdgeDiagnostic, 0, len(edges))
	bottleneckCount, saturatedCount := 0, 0
	for _, edge := range edges {
		utilization := 0
		if edge.value.AvailableCapacityUnits > 0 {
			utilization = int(edge.input * 1000 / edge.value.AvailableCapacityUnits)
			if utilization > 1000 {
				utilization = 1000
			}
		}
		item := CityPhysicalNetworkEdgeDiagnostic{
			EdgeCode: edge.value.Code, Status: edge.value.Status,
			AvailableCapacityUnits: edge.value.AvailableCapacityUnits,
			LatestInputUnits:       edge.input, LatestOutputUnits: edge.output,
			LatestLossUnits: edge.loss, UtilizationMilli: utilization,
			Saturated:  edge.value.Status == CityNetworkEdgeStatusActive && utilization >= 1000,
			Bottleneck: edge.value.Status == CityNetworkEdgeStatusActive && utilization >= 900,
		}
		if item.Bottleneck {
			bottleneckCount++
		}
		if item.Saturated {
			saturatedCount++
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Bottleneck != items[j].Bottleneck {
			return items[i].Bottleneck
		}
		if items[i].UtilizationMilli != items[j].UtilizationMilli {
			return items[i].UtilizationMilli > items[j].UtilizationMilli
		}
		return items[i].EdgeCode < items[j].EdgeCode
	})
	truncated := 0
	if len(items) > cityPhysicalNetworkMaximumDiagnosticEdges {
		truncated = len(items) - cityPhysicalNetworkMaximumDiagnosticEdges
		items = items[:cityPhysicalNetworkMaximumDiagnosticEdges]
	}
	return items, bottleneckCount, saturatedCount, truncated
}

func diagnoseCityPhysicalNetworkRoute(
	nodes []cityPhysicalNetworkDiagnosticNode,
	edges []*cityPhysicalNetworkDiagnosticEdge,
	policy CityPhysicalNetworkPolicy,
	source, sink string,
	probeUnits int64,
) (*CityPhysicalNetworkRouteDiagnostic, error) {
	if probeUnits == 0 {
		probeUnits = 1
	}
	result := &CityPhysicalNetworkRouteDiagnostic{
		SourceNodeCode: source, SinkNodeCode: sink, ProbeUnits: probeUnits,
		ReasonCode: "no_capacity_path", Paths: make([]CityPhysicalNetworkDiagnosticPath, 0),
	}
	nodeCodes := make([]string, 0)
	active := make(map[string]bool)
	for _, node := range nodes {
		if node.status == CityNetworkNodeStatusActive {
			active[node.code] = true
			nodeCodes = append(nodeCodes, node.code)
		}
	}
	if !active[source] {
		result.ReasonCode = "source_not_active"
		return result, nil
	}
	if !active[sink] {
		result.ReasonCode = "sink_not_active"
		return result, nil
	}
	plannerEdges := make([]cityNetworkPlannerEdge, 0)
	for _, edge := range edges {
		if edge.value.Status != CityNetworkEdgeStatusActive || edge.value.ConditionMilli <= 0 ||
			edge.value.AvailableCapacityUnits <= 0 || !active[edge.value.FromNodeCode] || !active[edge.value.ToNodeCode] {
			continue
		}
		plannerEdges = append(plannerEdges, cityNetworkPlannerEdge{
			Code: edge.value.Code, FromNodeCode: edge.value.FromNodeCode,
			ToNodeCode: edge.value.ToNodeCode, Direction: edge.value.Direction,
			AvailableCapacityUnits: edge.value.AvailableCapacityUnits,
			LossMilli:              edge.value.LossMilli, BaseCostUnits: edge.value.BaseCostUnits,
			Version: edge.value.Version,
		})
	}
	graph, err := newCityNetworkResidualGraph(nodeCodes, plannerEdges, cityNetworkPlannerPolicy{
		MaximumPaths: policy.MaximumPaths, MaximumHops: policy.MaximumHops,
		LossCostWeight: policy.LossCostWeight,
	})
	if err != nil {
		return nil, err
	}
	plan, err := graph.route(cityNetworkRouteRequest{
		ConnectionCode: "diagnostic.route", SourceNodeCode: source, SinkNodeCode: sink,
		MaximumDispatchedUnits: probeUnits, MaximumNetworkReceivedUnits: probeUnits,
	})
	if err != nil {
		return nil, err
	}
	if len(plan.Paths) == 0 {
		return result, nil
	}
	result.Reachable = true
	result.ReasonCode = "reachable"
	result.DispatchedUnits = plan.DispatchedUnits
	result.NetworkReceivedUnits = plan.NetworkReceivedUnits
	result.NetworkLossUnits = plan.NetworkLossUnits
	for _, path := range plan.Paths {
		item := CityPhysicalNetworkDiagnosticPath{
			Index: path.PathIndex, CostUnits: path.PathCostUnits,
			DispatchedUnits:      path.DispatchedUnits,
			NetworkReceivedUnits: path.NetworkReceivedUnits,
			NetworkLossUnits:     path.NetworkLossUnits, PathHash: path.PathHash,
			Segments: make([]CityPhysicalNetworkDiagnosticSegment, 0, len(path.Segments)),
		}
		for _, segment := range path.Segments {
			item.Segments = append(item.Segments, CityPhysicalNetworkDiagnosticSegment{
				Index: segment.SegmentIndex, EdgeCode: segment.EdgeCode,
				Direction: segment.Direction, FromNodeCode: segment.FromNodeCode,
				ToNodeCode: segment.ToNodeCode, EdgeCapacityUnits: segment.EdgeCapacityUnits,
				LossMilli: segment.LossMilli, InputUnits: segment.InputUnits,
				OutputUnits: segment.OutputUnits, LossUnits: segment.LossUnits,
			})
		}
		result.Paths = append(result.Paths, item)
	}
	return result, nil
}
