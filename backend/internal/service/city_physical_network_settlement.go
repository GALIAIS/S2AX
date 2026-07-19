package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

type cityServicePhysicalNetworkPlanningState struct {
	required map[string]bool
	networks map[string]*cityServicePhysicalNetworkPlan
}

type cityServicePhysicalNetworkPlan struct {
	id               int64
	code             string
	serviceCode      string
	topologyRevision int64
	routeDirection   string
	policy           cityNetworkPlannerPolicy
	graph            *cityNetworkResidualGraph
	nodeCodes        []string
	nodesByCapacity  map[int64]string
	nodesByDemand    map[int64]string
	nodeIDs          map[string]int64
	edgeIDs          map[string]int64
	edges            []cityNetworkPlannerEdge
}

func (state *cityServicePhysicalNetworkPlanningState) usesNetwork(serviceCode string) bool {
	return state != nil && (state.required[serviceCode] || state.networks[serviceCode] != nil)
}

func (state *cityServicePhysicalNetworkPlanningState) route(
	serviceCode string,
	connectionID, capacityID, demandID int64,
	connectionCode string,
	maximumDispatch, maximumReceived int64,
) (cityNetworkRoutePlan, string, error) {
	if state == nil || !state.usesNetwork(serviceCode) {
		return cityNetworkRoutePlan{}, "", ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"field": "physical_network_policy"},
		)
	}
	network := state.networks[serviceCode]
	if network == nil || network.graph == nil {
		return cityNetworkRoutePlan{}, "", nil
	}
	source := network.nodesByCapacity[capacityID]
	sink := network.nodesByDemand[demandID]
	if network.routeDirection == CityNetworkRouteDemandToFacility {
		source, sink = sink, source
	}
	if source == "" || sink == "" {
		return cityNetworkRoutePlan{}, network.code, nil
	}
	plan, err := network.graph.route(cityNetworkRouteRequest{
		ConnectionCode: connectionCode, SourceNodeCode: source, SinkNodeCode: sink,
		MaximumDispatchedUnits: maximumDispatch, MaximumNetworkReceivedUnits: maximumReceived,
	})
	if err != nil {
		return cityNetworkRoutePlan{}, network.code, fmt.Errorf(
			"route physical network connection %d: %w", connectionID, err,
		)
	}
	return plan, network.code, nil
}

func loadCityServicePhysicalNetworkPlanningState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityServicePhysicalNetworkPlanningState, error) {
	state := &cityServicePhysicalNetworkPlanningState{
		required: make(map[string]bool),
		networks: make(map[string]*cityServicePhysicalNetworkPlan),
	}
	policyRows, err := queryer.QueryContext(ctx, `
SELECT service.code, policy.network_required, policy.route_direction,
       policy.maximum_paths, policy.maximum_hops, policy.loss_cost_weight
FROM city_physical_network_policies policy
JOIN city_service_definitions service ON service.id = policy.service_definition_id
WHERE policy.world_id = $1 ORDER BY service.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load physical network settlement policies: %w", err)
	}
	policies := make(map[string]struct {
		direction string
		planner   cityNetworkPlannerPolicy
	})
	for policyRows.Next() {
		var serviceCode, direction string
		var required bool
		var planner cityNetworkPlannerPolicy
		if err = policyRows.Scan(
			&serviceCode, &required, &direction, &planner.MaximumPaths,
			&planner.MaximumHops, &planner.LossCostWeight,
		); err != nil {
			_ = policyRows.Close()
			return nil, fmt.Errorf("scan physical network settlement policy: %w", err)
		}
		state.required[serviceCode] = required
		policies[serviceCode] = struct {
			direction string
			planner   cityNetworkPlannerPolicy
		}{direction: direction, planner: planner}
	}
	if err = closeCityRows(policyRows, "iterate physical network settlement policies"); err != nil {
		return nil, err
	}
	networkRows, err := queryer.QueryContext(ctx, `
SELECT network.id, network.code, service.code, network.topology_revision
FROM city_physical_networks network
JOIN city_service_definitions service ON service.id = network.service_definition_id
WHERE network.world_id = $1 AND network.status = 'active'
ORDER BY service.code, network.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load active physical service networks: %w", err)
	}
	for networkRows.Next() {
		var network cityServicePhysicalNetworkPlan
		if err = networkRows.Scan(
			&network.id, &network.code, &network.serviceCode, &network.topologyRevision,
		); err != nil {
			_ = networkRows.Close()
			return nil, fmt.Errorf("scan active physical service network: %w", err)
		}
		policy, exists := policies[network.serviceCode]
		if !exists {
			_ = networkRows.Close()
			return nil, ErrCitySimulationInvariant.WithMetadata(
				map[string]string{"field": "physical_network_service_policy"},
			)
		}
		if _, duplicate := state.networks[network.serviceCode]; duplicate {
			_ = networkRows.Close()
			return nil, ErrCitySimulationInvariant.WithMetadata(
				map[string]string{"field": "physical_network_service_duplicate"},
			)
		}
		network.routeDirection = policy.direction
		network.policy = policy.planner
		network.nodeCodes = make([]string, 0)
		network.nodesByCapacity = make(map[int64]string)
		network.nodesByDemand = make(map[int64]string)
		network.nodeIDs = make(map[string]int64)
		network.edgeIDs = make(map[string]int64)
		network.edges = make([]cityNetworkPlannerEdge, 0)
		state.networks[network.serviceCode] = &network
	}
	if err = closeCityRows(networkRows, "iterate active physical service networks"); err != nil {
		return nil, err
	}
	if len(state.networks) == 0 {
		return state, nil
	}
	nodeRows, err := queryer.QueryContext(ctx, `
SELECT service.code, node.id, node.code, node.capacity_id, node.demand_id
FROM city_physical_network_nodes node
JOIN city_physical_networks network ON network.id = node.network_id
JOIN city_service_definitions service ON service.id = network.service_definition_id
WHERE node.world_id = $1 AND network.status = 'active' AND node.status = 'active'
ORDER BY service.code, node.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load active physical network nodes: %w", err)
	}
	for nodeRows.Next() {
		var serviceCode, code string
		var id int64
		var capacityID, demandID sql.NullInt64
		if err = nodeRows.Scan(&serviceCode, &id, &code, &capacityID, &demandID); err != nil {
			_ = nodeRows.Close()
			return nil, fmt.Errorf("scan active physical network node: %w", err)
		}
		network := state.networks[serviceCode]
		if network == nil {
			continue
		}
		network.nodeCodes = append(network.nodeCodes, code)
		network.nodeIDs[code] = id
		if capacityID.Valid {
			network.nodesByCapacity[capacityID.Int64] = code
		}
		if demandID.Valid {
			network.nodesByDemand[demandID.Int64] = code
		}
	}
	if err = closeCityRows(nodeRows, "iterate active physical network nodes"); err != nil {
		return nil, err
	}
	edgeRows, err := queryer.QueryContext(ctx, `
SELECT service.code, edge.id, edge.code, source.code, sink.code,
       edge.direction, edge.available_capacity_units, edge.loss_milli,
       edge.base_cost_units, edge.version
FROM city_physical_network_edges edge
JOIN city_physical_networks network ON network.id = edge.network_id
JOIN city_service_definitions service ON service.id = network.service_definition_id
JOIN city_physical_network_nodes source ON source.id = edge.from_node_id
JOIN city_physical_network_nodes sink ON sink.id = edge.to_node_id
WHERE edge.world_id = $1 AND network.status = 'active' AND edge.status = 'active'
ORDER BY service.code, edge.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load active physical network edges: %w", err)
	}
	for edgeRows.Next() {
		var serviceCode string
		var id int64
		var edge cityNetworkPlannerEdge
		if err = edgeRows.Scan(
			&serviceCode, &id, &edge.Code, &edge.FromNodeCode, &edge.ToNodeCode,
			&edge.Direction, &edge.AvailableCapacityUnits, &edge.LossMilli,
			&edge.BaseCostUnits, &edge.Version,
		); err != nil {
			_ = edgeRows.Close()
			return nil, fmt.Errorf("scan active physical network edge: %w", err)
		}
		network := state.networks[serviceCode]
		if network == nil {
			continue
		}
		network.edges = append(network.edges, edge)
		network.edgeIDs[edge.Code] = id
	}
	if err = closeCityRows(edgeRows, "iterate active physical network edges"); err != nil {
		return nil, err
	}
	for _, network := range state.networks {
		if len(network.nodeCodes) == 0 {
			continue
		}
		network.graph, err = newCityNetworkResidualGraph(
			network.nodeCodes, network.edges, network.policy,
		)
		if err != nil {
			return nil, fmt.Errorf("build physical network %s residual graph: %w", network.code, err)
		}
	}
	return state, nil
}

type cityPhysicalNetworkPersistedAllocation struct {
	serviceFactID   int64
	serviceSequence int64
	allocationIndex int
	connectionID    int64
	plan            cityServiceAllocationPlan
}

type cityPhysicalNetworkFlowFactPayload struct {
	SchemaVersion int                              `json:"schema_version"`
	Batch         CityPhysicalNetworkFlowBatch     `json:"batch"`
	Paths         []CityPhysicalNetworkFlowPath    `json:"paths"`
	Segments      []CityPhysicalNetworkFlowSegment `json:"segments"`
}

func buildCityPhysicalNetworkFlowFactPayload(
	targetTick, sequence int64,
	network *cityServicePhysicalNetworkPlan,
	entries []cityPhysicalNetworkPersistedAllocation,
) (cityPhysicalNetworkFlowFactPayload, error) {
	if targetTick <= 0 || sequence <= 0 || network == nil ||
		!cityServiceCodePattern.MatchString(network.code) || network.topologyRevision <= 0 {
		return cityPhysicalNetworkFlowFactPayload{}, ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"field": "physical_network_flow_payload"},
		)
	}
	payload := cityPhysicalNetworkFlowFactPayload{
		SchemaVersion: cityPhysicalNetworkSchemaVersion,
		Batch: CityPhysicalNetworkFlowBatch{
			Tick: targetTick, Sequence: sequence, NetworkCode: network.code,
			ServiceCode: network.serviceCode, TopologyRevision: network.topologyRevision,
			AllocationCount: len(entries), SourceFactTick: targetTick,
			SourceFactSequence: sequence,
			Metadata:           json.RawMessage(`{"schema_version":1}`),
		},
		Paths:    make([]CityPhysicalNetworkFlowPath, 0),
		Segments: make([]CityPhysicalNetworkFlowSegment, 0),
	}
	for _, entry := range entries {
		if entry.serviceSequence <= 0 || entry.allocationIndex <= 0 ||
			entry.plan.NetworkCode != network.code || entry.plan.DispatchedUnits <= 0 ||
			entry.plan.NetworkReceivedUnits <= 0 ||
			entry.plan.NetworkReceivedUnits > entry.plan.DispatchedUnits {
			return cityPhysicalNetworkFlowFactPayload{}, ErrCitySimulationInvariant.WithMetadata(
				map[string]string{"field": "physical_network_flow_allocation"},
			)
		}
		var allocationDispatched, allocationReceived int64
		for _, path := range entry.plan.NetworkPaths {
			if path.PathIndex <= 0 || path.DispatchedUnits <= 0 ||
				path.NetworkReceivedUnits <= 0 || path.NetworkReceivedUnits > path.DispatchedUnits ||
				path.NetworkLossUnits != path.DispatchedUnits-path.NetworkReceivedUnits ||
				len(path.Segments) == 0 || len(path.PathHash) != 64 {
				return cityPhysicalNetworkFlowFactPayload{}, ErrCitySimulationInvariant.WithMetadata(
					map[string]string{"field": "physical_network_flow_path"},
				)
			}
			payload.Paths = append(payload.Paths, CityPhysicalNetworkFlowPath{
				Tick: targetTick, Sequence: sequence, ServiceSequence: entry.serviceSequence,
				AllocationIndex: entry.allocationIndex, PathIndex: path.PathIndex,
				NetworkCode: network.code, ConnectionCode: entry.plan.ConnectionCode,
				SourceNodeCode: path.SourceNodeCode, SinkNodeCode: path.SinkNodeCode,
				HopCount: len(path.Segments), DispatchedUnits: path.DispatchedUnits,
				NetworkReceivedUnits: path.NetworkReceivedUnits,
				NetworkLossUnits:     path.NetworkLossUnits, PathCostUnits: path.PathCostUnits,
				PathHash: path.PathHash, Metadata: json.RawMessage(`{"schema_version":1}`),
			})
			allocationDispatched += path.DispatchedUnits
			allocationReceived += path.NetworkReceivedUnits
			for _, segment := range path.Segments {
				payload.Segments = append(payload.Segments, CityPhysicalNetworkFlowSegment{
					Tick: targetTick, Sequence: sequence,
					ServiceSequence: entry.serviceSequence,
					AllocationIndex: entry.allocationIndex, PathIndex: path.PathIndex,
					SegmentIndex: segment.SegmentIndex, EdgeCode: segment.EdgeCode,
					EdgeVersion: segment.EdgeVersion, Direction: segment.Direction,
					FromNodeCode: segment.FromNodeCode, ToNodeCode: segment.ToNodeCode,
					EdgeCapacityUnits: segment.EdgeCapacityUnits,
					LossMilli:         segment.LossMilli, InputUnits: segment.InputUnits,
					OutputUnits: segment.OutputUnits, LossUnits: segment.LossUnits,
					Metadata: json.RawMessage(`{"schema_version":1}`),
				})
			}
		}
		if allocationDispatched != entry.plan.DispatchedUnits ||
			allocationReceived != entry.plan.NetworkReceivedUnits ||
			entry.plan.NetworkLossUnits != allocationDispatched-allocationReceived {
			return cityPhysicalNetworkFlowFactPayload{}, ErrCitySimulationInvariant.WithMetadata(
				map[string]string{"field": "physical_network_flow_allocation_conservation"},
			)
		}
		payload.Batch.DispatchedUnits += allocationDispatched
		payload.Batch.NetworkReceivedUnits += allocationReceived
	}
	payload.Batch.PathCount = len(payload.Paths)
	payload.Batch.SegmentCount = len(payload.Segments)
	payload.Batch.NetworkLossUnits = payload.Batch.DispatchedUnits - payload.Batch.NetworkReceivedUnits
	return payload, nil
}

func persistCityPhysicalNetworkFlows(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, firstSequence int64,
	planning *cityServicePhysicalNetworkPlanningState,
	allocations []cityPhysicalNetworkPersistedAllocation,
) ([]CityPhysicalNetworkFact, error) {
	if planning == nil || len(allocations) == 0 {
		return []CityPhysicalNetworkFact{}, nil
	}
	grouped := make(map[string][]cityPhysicalNetworkPersistedAllocation)
	for _, allocation := range allocations {
		if allocation.plan.NetworkCode != "" {
			grouped[allocation.plan.NetworkCode] = append(grouped[allocation.plan.NetworkCode], allocation)
		}
	}
	if len(grouped) == 0 {
		return []CityPhysicalNetworkFact{}, nil
	}
	if _, err := tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_physical_network_auto_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10)); err != nil {
		return nil, fmt.Errorf("enable automatic physical network settlement: %w", err)
	}
	codes := make([]string, 0, len(grouped))
	for code := range grouped {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	facts := make([]CityPhysicalNetworkFact, 0, len(codes))
	for sequenceIndex, code := range codes {
		entries := grouped[code]
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].serviceSequence != entries[j].serviceSequence {
				return entries[i].serviceSequence < entries[j].serviceSequence
			}
			return entries[i].allocationIndex < entries[j].allocationIndex
		})
		var network *cityServicePhysicalNetworkPlan
		for _, candidate := range planning.networks {
			if candidate.code == code {
				network = candidate
				break
			}
		}
		if network == nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(
				map[string]string{"field": "physical_network_flow_network"},
			)
		}
		sequence := firstSequence + int64(sequenceIndex)
		payloadValue, err := buildCityPhysicalNetworkFlowFactPayload(
			targetTick, sequence, network, entries,
		)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(payloadValue)
		if err != nil {
			return nil, fmt.Errorf("marshal physical network flow fact: %w", err)
		}
		batch := payloadValue.Batch
		dispatched, received := batch.DispatchedUnits, batch.NetworkReceivedUnits
		pathCount, segmentCount := batch.PathCount, batch.SegmentCount
		var factID int64
		if err = tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_facts
    (world_id, tick, sequence, phase, fact_type, subject_kind, subject_code,
     version_before, version_after, payload)
VALUES ($1, $2::bigint, $3, $6, $7, 'flow_batch', $4,
        $2::bigint - 1, $2::bigint, $5::jsonb)
RETURNING id`, worldID, targetTick, sequence, code, payload,
			CityPhysicalNetworkPhaseSettlement, CityPhysicalNetworkFactFlowSettled).
			Scan(&factID); err != nil {
			return nil, fmt.Errorf("insert physical network flow fact %s: %w", code, err)
		}
		if _, err = tx.ExecContext(ctx,
			`SELECT set_config('sub2api.city_physical_network_fact_id', $1, TRUE)`,
			strconv.FormatInt(factID, 10)); err != nil {
			return nil, fmt.Errorf("activate physical network flow fact: %w", err)
		}
		var batchID int64
		if err = tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_flow_batches
    (world_id, network_id, tick, sequence, source_fact_id, topology_revision,
     allocation_count, path_count, segment_count, dispatched_units,
     network_received_units, network_loss_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        '{"schema_version":1}'::jsonb)
RETURNING id`, worldID, network.id, targetTick, sequence, factID,
			network.topologyRevision, batch.AllocationCount, pathCount, segmentCount,
			dispatched, received, dispatched-received).Scan(&batchID); err != nil {
			return nil, fmt.Errorf("insert physical network flow batch %s: %w", code, err)
		}
		for _, entry := range entries {
			for _, path := range entry.plan.NetworkPaths {
				sourceNodeID := network.nodeIDs[path.SourceNodeCode]
				sinkNodeID := network.nodeIDs[path.SinkNodeCode]
				if sourceNodeID <= 0 || sinkNodeID <= 0 {
					return nil, ErrCitySimulationInvariant.WithMetadata(
						map[string]string{"field": "physical_network_flow_endpoint"},
					)
				}
				var pathID int64
				if err = tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_flow_paths
    (world_id, batch_id, service_fact_id, allocation_index, path_index,
     connection_id, source_node_id, sink_node_id, hop_count,
     dispatched_units, network_received_units, network_loss_units,
     path_cost_units, path_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        '{"schema_version":1}'::jsonb)
RETURNING id`, worldID, batchID, entry.serviceFactID, entry.allocationIndex,
					path.PathIndex, entry.connectionID, sourceNodeID, sinkNodeID,
					len(path.Segments), path.DispatchedUnits, path.NetworkReceivedUnits,
					path.NetworkLossUnits, path.PathCostUnits, path.PathHash).Scan(&pathID); err != nil {
					return nil, fmt.Errorf("insert physical network flow path: %w", err)
				}
				for _, segment := range path.Segments {
					edgeID := network.edgeIDs[segment.EdgeCode]
					fromNodeID := network.nodeIDs[segment.FromNodeCode]
					toNodeID := network.nodeIDs[segment.ToNodeCode]
					if edgeID <= 0 || fromNodeID <= 0 || toNodeID <= 0 {
						return nil, ErrCitySimulationInvariant.WithMetadata(
							map[string]string{"field": "physical_network_flow_segment"},
						)
					}
					if _, err = tx.ExecContext(ctx, `
INSERT INTO city_physical_network_flow_segments
    (world_id, path_id, segment_index, edge_id, edge_version, direction,
     from_node_id, to_node_id, edge_capacity_units, loss_milli,
     input_units, output_units, loss_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        '{"schema_version":1}'::jsonb)`, worldID, pathID, segment.SegmentIndex,
						edgeID, segment.EdgeVersion, segment.Direction,
						fromNodeID, toNodeID, segment.EdgeCapacityUnits,
						segment.LossMilli, segment.InputUnits, segment.OutputUnits,
						segment.LossUnits); err != nil {
						return nil, fmt.Errorf("insert physical network flow segment: %w", err)
					}
				}
			}
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE city_physical_network_profiles
SET fact_count = fact_count + 1, batch_count = batch_count + 1,
    path_count = path_count + $2, segment_count = segment_count + $3,
    revision = revision + 1, updated_at = NOW()
WHERE world_id = $1`, worldID, pathCount, segmentCount); err != nil {
			return nil, fmt.Errorf("advance physical network flow profile: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE city_physical_network_facts SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, factID); err != nil {
			return nil, fmt.Errorf("post physical network flow fact: %w", err)
		}
		facts = append(facts, CityPhysicalNetworkFact{
			Tick: targetTick, Sequence: sequence, Phase: CityPhysicalNetworkPhaseSettlement,
			FactType: CityPhysicalNetworkFactFlowSettled, SubjectKind: "flow_batch",
			SubjectCode: code, VersionBefore: targetTick - 1,
			VersionAfter: targetTick, Payload: payload,
		})
	}
	return facts, nil
}
