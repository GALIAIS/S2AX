package service

import (
	"container/heap"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

type cityNetworkPlannerEdge struct {
	Code                   string
	FromNodeCode           string
	ToNodeCode             string
	Direction              string
	AvailableCapacityUnits int64
	LossMilli              int
	BaseCostUnits          int64
	Version                int64
}

type cityNetworkPlannerPolicy struct {
	MaximumPaths   int
	MaximumHops    int
	LossCostWeight int64
}

type cityNetworkRouteRequest struct {
	ConnectionCode              string
	SourceNodeCode              string
	SinkNodeCode                string
	MaximumDispatchedUnits      int64
	MaximumNetworkReceivedUnits int64
}

type cityNetworkRoutePlan struct {
	DispatchedUnits      int64
	NetworkReceivedUnits int64
	NetworkLossUnits     int64
	Paths                []cityNetworkPathPlan
}

type cityNetworkPathPlan struct {
	PathIndex            int
	SourceNodeCode       string
	SinkNodeCode         string
	DispatchedUnits      int64
	NetworkReceivedUnits int64
	NetworkLossUnits     int64
	PathCostUnits        int64
	PathHash             string
	Segments             []cityNetworkSegmentPlan
}

type cityNetworkSegmentPlan struct {
	SegmentIndex      int
	EdgeCode          string
	EdgeVersion       int64
	Direction         string
	FromNodeCode      string
	ToNodeCode        string
	EdgeCapacityUnits int64
	LossMilli         int
	InputUnits        int64
	OutputUnits       int64
	LossUnits         int64
}

type cityNetworkResidualGraph struct {
	nodes     map[string]struct{}
	edges     map[string]cityNetworkPlannerEdge
	adjacency map[string][]cityNetworkArc
	remaining map[string]int64
	policy    cityNetworkPlannerPolicy
}

type cityNetworkArc struct {
	edgeCode  string
	from      string
	to        string
	direction string
	cost      int64
}

func newCityNetworkResidualGraph(
	nodeCodes []string,
	edges []cityNetworkPlannerEdge,
	policy cityNetworkPlannerPolicy,
) (*cityNetworkResidualGraph, error) {
	if len(nodeCodes) == 0 || len(nodeCodes) > cityPhysicalNetworkMaximumNodes ||
		len(edges) > cityPhysicalNetworkMaximumEdges || policy.MaximumPaths <= 0 ||
		policy.MaximumPaths > cityPhysicalNetworkMaximumPathsPerRequest ||
		policy.MaximumHops <= 0 || policy.MaximumHops > cityPhysicalNetworkMaximumHopsPerPath ||
		policy.LossCostWeight < 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_graph_limits"})
	}
	graph := &cityNetworkResidualGraph{
		nodes:     make(map[string]struct{}, len(nodeCodes)),
		edges:     make(map[string]cityNetworkPlannerEdge, len(edges)),
		adjacency: make(map[string][]cityNetworkArc, len(nodeCodes)),
		remaining: make(map[string]int64, len(edges)), policy: policy,
	}
	for _, code := range nodeCodes {
		if !cityServiceCodePattern.MatchString(code) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_node_code"})
		}
		if _, duplicate := graph.nodes[code]; duplicate {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_node_duplicate"})
		}
		graph.nodes[code] = struct{}{}
		graph.adjacency[code] = make([]cityNetworkArc, 0)
	}
	for _, edge := range edges {
		if !cityServiceCodePattern.MatchString(edge.Code) || edge.FromNodeCode == edge.ToNodeCode ||
			edge.Version <= 0 || edge.AvailableCapacityUnits < 0 ||
			edge.AvailableCapacityUnits > cityServiceMaximumConfiguredUnits ||
			edge.LossMilli < 0 || edge.LossMilli > 999 || edge.BaseCostUnits <= 0 ||
			(edge.Direction != CityNetworkEdgeDirectionDirected &&
				edge.Direction != CityNetworkEdgeDirectionBidirectional) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_edge"})
		}
		if _, exists := graph.nodes[edge.FromNodeCode]; !exists {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_edge_from"})
		}
		if _, exists := graph.nodes[edge.ToNodeCode]; !exists {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_edge_to"})
		}
		if _, duplicate := graph.edges[edge.Code]; duplicate {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_edge_duplicate"})
		}
		if edge.LossMilli > 0 && policy.LossCostWeight > math.MaxInt64/int64(edge.LossMilli) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_edge_cost"})
		}
		cost := edge.BaseCostUnits + policy.LossCostWeight*int64(edge.LossMilli)
		if cost < edge.BaseCostUnits {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_edge_cost"})
		}
		graph.edges[edge.Code] = edge
		graph.remaining[edge.Code] = edge.AvailableCapacityUnits
		graph.adjacency[edge.FromNodeCode] = append(graph.adjacency[edge.FromNodeCode], cityNetworkArc{
			edgeCode: edge.Code, from: edge.FromNodeCode, to: edge.ToNodeCode,
			direction: "forward", cost: cost,
		})
		if edge.Direction == CityNetworkEdgeDirectionBidirectional {
			graph.adjacency[edge.ToNodeCode] = append(graph.adjacency[edge.ToNodeCode], cityNetworkArc{
				edgeCode: edge.Code, from: edge.ToNodeCode, to: edge.FromNodeCode,
				direction: "reverse", cost: cost,
			})
		}
	}
	for node := range graph.adjacency {
		sort.Slice(graph.adjacency[node], func(i, j int) bool {
			left, right := graph.adjacency[node][i], graph.adjacency[node][j]
			if left.edgeCode != right.edgeCode {
				return left.edgeCode < right.edgeCode
			}
			if left.to != right.to {
				return left.to < right.to
			}
			return left.direction < right.direction
		})
	}
	return graph, nil
}

func (graph *cityNetworkResidualGraph) route(
	request cityNetworkRouteRequest,
) (cityNetworkRoutePlan, error) {
	plan := cityNetworkRoutePlan{Paths: make([]cityNetworkPathPlan, 0)}
	if graph == nil || !cityServiceCodePattern.MatchString(request.ConnectionCode) ||
		request.SourceNodeCode == request.SinkNodeCode || request.MaximumDispatchedUnits < 0 ||
		request.MaximumNetworkReceivedUnits < 0 {
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_route_request"})
	}
	if _, exists := graph.nodes[request.SourceNodeCode]; !exists {
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_route_source"})
	}
	if _, exists := graph.nodes[request.SinkNodeCode]; !exists {
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_route_sink"})
	}
	remainingDispatch := request.MaximumDispatchedUnits
	remainingReceived := request.MaximumNetworkReceivedUnits
	for len(plan.Paths) < graph.policy.MaximumPaths && remainingDispatch > 0 && remainingReceived > 0 {
		arcs, cost, found, err := graph.shortestResidualPath(
			request.SourceNodeCode, request.SinkNodeCode, remainingDispatch,
		)
		if err != nil {
			return cityNetworkRoutePlan{}, err
		}
		if !found {
			break
		}
		dispatch, segments, received, err := graph.maximumPathDispatch(
			arcs, remainingDispatch, remainingReceived,
		)
		if err != nil {
			return cityNetworkRoutePlan{}, err
		}
		if dispatch <= 0 || received <= 0 {
			break
		}
		for _, segment := range segments {
			residual := graph.remaining[segment.EdgeCode]
			if segment.InputUnits > residual {
				return cityNetworkRoutePlan{}, ErrCitySimulationInvariant.WithMetadata(
					map[string]string{"field": "network_edge_residual"},
				)
			}
			graph.remaining[segment.EdgeCode] = residual - segment.InputUnits
		}
		path := cityNetworkPathPlan{
			PathIndex: len(plan.Paths) + 1, SourceNodeCode: request.SourceNodeCode,
			SinkNodeCode: request.SinkNodeCode, DispatchedUnits: dispatch,
			NetworkReceivedUnits: received, NetworkLossUnits: dispatch - received,
			PathCostUnits: cost, Segments: segments,
		}
		path.PathHash, err = cityNetworkPathPlanHash(request.ConnectionCode, path)
		if err != nil {
			return cityNetworkRoutePlan{}, err
		}
		plan.Paths = append(plan.Paths, path)
		plan.DispatchedUnits += dispatch
		plan.NetworkReceivedUnits += received
		remainingDispatch -= dispatch
		remainingReceived -= received
	}
	plan.NetworkLossUnits = plan.DispatchedUnits - plan.NetworkReceivedUnits
	return plan, nil
}

type cityNetworkPathLabel struct {
	node         string
	hops         int
	cost         int64
	maximumUnits int64
	key          string
	arcs         []cityNetworkArc
	heapIndex    int
	active       bool
}

type cityNetworkPathHeap []*cityNetworkPathLabel

func (items cityNetworkPathHeap) Len() int { return len(items) }
func (items cityNetworkPathHeap) Less(i, j int) bool {
	left, right := items[i], items[j]
	if left.cost != right.cost {
		return left.cost < right.cost
	}
	if left.hops != right.hops {
		return left.hops < right.hops
	}
	if left.key != right.key {
		return left.key < right.key
	}
	return left.node < right.node
}
func (items cityNetworkPathHeap) Swap(i, j int) {
	items[i], items[j] = items[j], items[i]
	items[i].heapIndex, items[j].heapIndex = i, j
}
func (items *cityNetworkPathHeap) Push(value any) {
	label := value.(*cityNetworkPathLabel)
	label.heapIndex = len(*items)
	*items = append(*items, label)
}
func (items *cityNetworkPathHeap) Pop() any {
	old := *items
	last := old[len(old)-1]
	old[len(old)-1] = nil
	last.heapIndex = -1
	*items = old[:len(old)-1]
	return last
}

func (graph *cityNetworkResidualGraph) shortestResidualPath(
	source, sink string, maximumDispatch int64,
) ([]cityNetworkArc, int64, bool, error) {
	if maximumDispatch <= 0 {
		return nil, 0, false, nil
	}
	queue := make(cityNetworkPathHeap, 0)
	heap.Init(&queue)
	start := &cityNetworkPathLabel{
		node: source, maximumUnits: maximumDispatch, key: source,
		arcs: make([]cityNetworkArc, 0), active: true,
	}
	heap.Push(&queue, start)
	best := map[string][]*cityNetworkPathLabel{cityNetworkLabelKey(source, 0): {start}}
	labelCount := 1
	for queue.Len() > 0 {
		current := heap.Pop(&queue).(*cityNetworkPathLabel)
		if !current.active {
			continue
		}
		if current.node == sink {
			return current.arcs, current.cost, true, nil
		}
		if current.hops >= graph.policy.MaximumHops {
			continue
		}
		for _, arc := range graph.adjacency[current.node] {
			if graph.remaining[arc.edgeCode] <= 0 || cityNetworkPathContainsNode(current, arc.to) {
				continue
			}
			edge := graph.edges[arc.edgeCode]
			maximumInput := current.maximumUnits
			if graph.remaining[arc.edgeCode] < maximumInput {
				maximumInput = graph.remaining[arc.edgeCode]
			}
			maximumOutput, outputErr := cityMulDivFloor(
				maximumInput, 1000-edge.LossMilli, 1000,
			)
			if outputErr != nil {
				return nil, 0, false, outputErr
			}
			if maximumOutput <= 0 {
				continue
			}
			if current.cost > math.MaxInt64-arc.cost {
				return nil, 0, false, ErrCitySimulationInvariant.WithMetadata(
					map[string]string{"field": "network_path_cost"},
				)
			}
			arcs := append(append([]cityNetworkArc(nil), current.arcs...), arc)
			label := &cityNetworkPathLabel{
				node: arc.to, hops: current.hops + 1, cost: current.cost + arc.cost,
				maximumUnits: maximumOutput,
				key:          current.key + "\x1f" + arc.edgeCode + ":" + arc.direction + "\x1f" + arc.to,
				arcs:         arcs, active: true,
			}
			labelKey := cityNetworkLabelKey(label.node, label.hops)
			existing := best[labelKey]
			dominated := false
			for _, candidate := range existing {
				if candidate.active && cityNetworkPathLabelDominates(candidate, label) {
					dominated = true
					break
				}
			}
			if dominated {
				continue
			}
			kept := existing[:0]
			for _, candidate := range existing {
				if candidate.active && cityNetworkPathLabelDominates(label, candidate) {
					candidate.active = false
					continue
				}
				if candidate.active {
					kept = append(kept, candidate)
				}
			}
			best[labelKey] = append(kept, label)
			heap.Push(&queue, label)
			labelCount++
			if labelCount > cityPhysicalNetworkMaximumSearchLabels {
				return nil, 0, false, ErrCitySimulationInvariant.WithMetadata(
					map[string]string{"field": "network_path_search_limit"},
				)
			}
		}
	}
	return nil, 0, false, nil
}

func cityNetworkLabelKey(node string, hops int) string {
	return node + "\x00" + fmt.Sprintf("%03d", hops)
}

func cityNetworkPathLabelDominates(left, right *cityNetworkPathLabel) bool {
	if left.cost > right.cost || left.maximumUnits < right.maximumUnits {
		return false
	}
	if left.cost < right.cost || left.maximumUnits > right.maximumUnits {
		return true
	}
	return left.key <= right.key
}

func cityNetworkPathContainsNode(label *cityNetworkPathLabel, node string) bool {
	if label.node == node {
		return true
	}
	for _, arc := range label.arcs {
		if arc.from == node {
			return true
		}
	}
	return false
}

func (graph *cityNetworkResidualGraph) maximumPathDispatch(
	arcs []cityNetworkArc, maximumDispatch, maximumReceived int64,
) (int64, []cityNetworkSegmentPlan, int64, error) {
	if len(arcs) == 0 || maximumDispatch <= 0 || maximumReceived <= 0 {
		return 0, nil, 0, nil
	}
	upper := maximumDispatch
	if graph.remaining[arcs[0].edgeCode] < upper {
		upper = graph.remaining[arcs[0].edgeCode]
	}
	evaluate := func(dispatch int64) ([]cityNetworkSegmentPlan, int64, bool, error) {
		value := dispatch
		segments := make([]cityNetworkSegmentPlan, 0, len(arcs))
		for index, arc := range arcs {
			edge := graph.edges[arc.edgeCode]
			if value > graph.remaining[edge.Code] {
				return nil, 0, false, nil
			}
			output, err := cityMulDivFloor(value, 1000-edge.LossMilli, 1000)
			if err != nil {
				return nil, 0, false, err
			}
			segments = append(segments, cityNetworkSegmentPlan{
				SegmentIndex: index + 1, EdgeCode: edge.Code, EdgeVersion: edge.Version,
				Direction: arc.direction, FromNodeCode: arc.from, ToNodeCode: arc.to,
				EdgeCapacityUnits: edge.AvailableCapacityUnits,
				LossMilli:         edge.LossMilli,
				InputUnits:        value, OutputUnits: output, LossUnits: value - output,
			})
			value = output
		}
		return segments, value, value <= maximumReceived, nil
	}
	low, high := int64(0), upper
	for low < high {
		middle := low + (high-low+1)/2
		_, _, valid, err := evaluate(middle)
		if err != nil {
			return 0, nil, 0, err
		}
		if valid {
			low = middle
		} else {
			high = middle - 1
		}
	}
	if low <= 0 {
		return 0, nil, 0, nil
	}
	segments, received, valid, err := evaluate(low)
	if err != nil || !valid || received <= 0 {
		return 0, nil, 0, err
	}
	return low, segments, received, nil
}

func cityNetworkPathPlanHash(connectionCode string, path cityNetworkPathPlan) (string, error) {
	if !cityServiceCodePattern.MatchString(connectionCode) || len(path.Segments) == 0 {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "network_path_hash"})
	}
	canonical := struct {
		SchemaVersion   int                      `json:"schema_version"`
		ConnectionCode  string                   `json:"connection_code"`
		SourceNodeCode  string                   `json:"source_node_code"`
		SinkNodeCode    string                   `json:"sink_node_code"`
		DispatchedUnits int64                    `json:"dispatched_units"`
		ReceivedUnits   int64                    `json:"network_received_units"`
		CostUnits       int64                    `json:"path_cost_units"`
		Segments        []cityNetworkSegmentPlan `json:"segments"`
	}{
		SchemaVersion: cityPhysicalNetworkSchemaVersion, ConnectionCode: connectionCode,
		SourceNodeCode: path.SourceNodeCode, SinkNodeCode: path.SinkNodeCode,
		DispatchedUnits: path.DispatchedUnits, ReceivedUnits: path.NetworkReceivedUnits,
		CostUnits: path.PathCostUnits, Segments: path.Segments,
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal city network path proof: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityNetworkPathEdgeCodes(path cityNetworkPathPlan) string {
	codes := make([]string, 0, len(path.Segments))
	for _, segment := range path.Segments {
		codes = append(codes, segment.EdgeCode+":"+segment.Direction)
	}
	return strings.Join(codes, ",")
}
