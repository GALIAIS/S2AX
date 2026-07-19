package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func loadCityPhysicalNetworkHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityPhysicalNetworkHashState, error) {
	state := &CityPhysicalNetworkStateSet{
		Policies: make([]CityPhysicalNetworkPolicy, 0),
		Networks: make([]CityPhysicalNetwork, 0),
		Nodes:    make([]CityPhysicalNetworkNode, 0),
		Edges:    make([]CityPhysicalNetworkEdge, 0),
		Facts:    make([]CityPhysicalNetworkFact, 0),
		Batches:  make([]CityPhysicalNetworkFlowBatch, 0),
		Paths:    make([]CityPhysicalNetworkFlowPath, 0),
		Segments: make([]CityPhysicalNetworkFlowSegment, 0),
	}
	err := queryer.QueryRowContext(ctx, `
SELECT policy_id, policy_version, policy_hash, baseline_tick, policy_count,
       network_count, node_count, edge_count, fact_count, batch_count,
       path_count, segment_count, revision, metadata
FROM city_physical_network_profiles WHERE world_id = $1`, worldID).Scan(
		&state.Profile.PolicyID, &state.Profile.PolicyVersion, &state.Profile.PolicyHash,
		&state.Profile.BaselineTick, &state.Profile.PolicyCount,
		&state.Profile.NetworkCount, &state.Profile.NodeCount, &state.Profile.EdgeCount,
		&state.Profile.FactCount, &state.Profile.BatchCount, &state.Profile.PathCount,
		&state.Profile.SegmentCount, &state.Profile.Revision, &state.Profile.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityPhysicalNetworkStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load city physical network profile: %w", err)
	}
	loaders := []func(context.Context, citySQLQueryer, int64, *CityPhysicalNetworkStateSet) error{
		loadCityPhysicalNetworkPolicies,
		loadCityPhysicalNetworks,
		loadCityPhysicalNetworkNodes,
		loadCityPhysicalNetworkEdges,
		loadCityPhysicalNetworkFacts,
		loadCityPhysicalNetworkFlowBatches,
		loadCityPhysicalNetworkFlowPaths,
		loadCityPhysicalNetworkFlowSegments,
	}
	for _, load := range loaders {
		if err = load(ctx, queryer, worldID, state); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func loadCityPhysicalNetworkPolicies(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
	state *CityPhysicalNetworkStateSet,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT service.code, policy.policy_version, policy.policy_hash,
       policy.network_required, policy.route_direction, policy.maximum_nodes,
       policy.maximum_edges, policy.maximum_paths, policy.maximum_hops,
       policy.loss_cost_weight, policy.allow_bidirectional,
       policy.algorithm_version, policy.payload
FROM city_physical_network_policies policy
JOIN city_service_definitions service ON service.id = policy.service_definition_id
WHERE policy.world_id = $1 ORDER BY service.code`, worldID)
	if err != nil {
		return fmt.Errorf("load city physical network policies: %w", err)
	}
	for rows.Next() {
		var item CityPhysicalNetworkPolicy
		if err = rows.Scan(
			&item.ServiceCode, &item.PolicyVersion, &item.PolicyHash,
			&item.NetworkRequired, &item.RouteDirection, &item.MaximumNodes,
			&item.MaximumEdges, &item.MaximumPaths, &item.MaximumHops,
			&item.LossCostWeight, &item.AllowBidirectional,
			&item.AlgorithmVersion, &item.Payload,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city physical network policy: %w", err)
		}
		state.Policies = append(state.Policies, item)
	}
	return closeCityRows(rows, "iterate city physical network policies")
}

func loadCityPhysicalNetworks(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
	state *CityPhysicalNetworkStateSet,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT network.code, network.name, service.code, network.status,
       network.topology_revision, network.created_tick, network.updated_tick,
       network.version, COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0),
       network.metadata
FROM city_physical_networks network
JOIN city_service_definitions service ON service.id = network.service_definition_id
LEFT JOIN city_physical_network_facts fact ON fact.id = network.source_fact_id
WHERE network.world_id = $1 ORDER BY network.code`, worldID)
	if err != nil {
		return fmt.Errorf("load city physical networks: %w", err)
	}
	for rows.Next() {
		var item CityPhysicalNetwork
		if err = rows.Scan(
			&item.Code, &item.Name, &item.ServiceCode, &item.Status,
			&item.TopologyRevision, &item.CreatedTick, &item.UpdatedTick,
			&item.Version, &item.SourceFactTick, &item.SourceFactSequence,
			&item.Metadata,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city physical network: %w", err)
		}
		state.Networks = append(state.Networks, item)
	}
	return closeCityRows(rows, "iterate city physical networks")
}

func loadCityPhysicalNetworkNodes(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
	state *CityPhysicalNetworkStateSet,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT node.code, network.code, node.role,
       CASE WHEN capacity.id IS NULL THEN NULL ELSE facility.code || '.' || service.code END,
       demand.code, district.code, building.code,
       node.world_x, node.world_y, node.world_z, node.status,
       node.created_tick, node.updated_tick, node.version,
       COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0), node.metadata
FROM city_physical_network_nodes node
JOIN city_physical_networks network ON network.id = node.network_id
LEFT JOIN city_facility_service_capacities capacity ON capacity.id = node.capacity_id
LEFT JOIN city_facilities facility ON facility.id = capacity.facility_id
LEFT JOIN city_service_definitions service ON service.id = capacity.service_definition_id
LEFT JOIN city_service_demands demand ON demand.id = node.demand_id
LEFT JOIN city_districts district ON district.id = node.district_id
LEFT JOIN city_buildings building ON building.id = node.building_id
LEFT JOIN city_physical_network_facts fact ON fact.id = node.source_fact_id
WHERE node.world_id = $1 ORDER BY network.code, node.code`, worldID)
	if err != nil {
		return fmt.Errorf("load city physical network nodes: %w", err)
	}
	for rows.Next() {
		var item CityPhysicalNetworkNode
		var capacity, demand, district, building sql.NullString
		var worldX, worldY sql.NullInt64
		var worldZ sql.NullInt32
		if err = rows.Scan(
			&item.Code, &item.NetworkCode, &item.Role, &capacity, &demand,
			&district, &building, &worldX, &worldY, &worldZ, &item.Status,
			&item.CreatedTick, &item.UpdatedTick, &item.Version,
			&item.SourceFactTick, &item.SourceFactSequence, &item.Metadata,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city physical network node: %w", err)
		}
		item.CapacityCode = nullStringPointer(capacity)
		item.DemandCode = nullStringPointer(demand)
		item.DistrictCode = nullStringPointer(district)
		item.BuildingCode = nullStringPointer(building)
		item.WorldX = nullInt64Pointer(worldX)
		item.WorldY = nullInt64Pointer(worldY)
		if worldZ.Valid {
			value := int(worldZ.Int32)
			item.WorldZ = &value
		}
		state.Nodes = append(state.Nodes, item)
	}
	return closeCityRows(rows, "iterate city physical network nodes")
}

func loadCityPhysicalNetworkEdges(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
	state *CityPhysicalNetworkStateSet,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT edge.code, network.code, source.code, sink.code, edge.direction,
       edge.installed_capacity_units, edge.availability_milli,
       edge.available_capacity_units, edge.loss_milli, edge.base_cost_units,
       edge.status, edge.condition_milli, edge.failure_count,
       edge.created_tick, edge.updated_tick, edge.version,
       COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0), edge.metadata
FROM city_physical_network_edges edge
JOIN city_physical_networks network ON network.id = edge.network_id
JOIN city_physical_network_nodes source ON source.id = edge.from_node_id
JOIN city_physical_network_nodes sink ON sink.id = edge.to_node_id
LEFT JOIN city_physical_network_facts fact ON fact.id = edge.source_fact_id
WHERE edge.world_id = $1 ORDER BY network.code, edge.code`, worldID)
	if err != nil {
		return fmt.Errorf("load city physical network edges: %w", err)
	}
	for rows.Next() {
		var item CityPhysicalNetworkEdge
		if err = rows.Scan(
			&item.Code, &item.NetworkCode, &item.FromNodeCode, &item.ToNodeCode,
			&item.Direction, &item.InstalledCapacityUnits, &item.AvailabilityMilli,
			&item.AvailableCapacityUnits, &item.LossMilli, &item.BaseCostUnits,
			&item.Status, &item.ConditionMilli, &item.FailureCount,
			&item.CreatedTick, &item.UpdatedTick, &item.Version,
			&item.SourceFactTick, &item.SourceFactSequence, &item.Metadata,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city physical network edge: %w", err)
		}
		state.Edges = append(state.Edges, item)
	}
	return closeCityRows(rows, "iterate city physical network edges")
}

func loadCityPhysicalNetworkFacts(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
	state *CityPhysicalNetworkStateSet,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, fact.phase, command.sequence, fact.fact_type,
       fact.subject_kind, fact.subject_code, fact.version_before,
       fact.version_after, fact.payload
FROM city_physical_network_facts fact
LEFT JOIN city_commands command ON command.id = fact.source_command_id
WHERE fact.world_id = $1 AND fact.posted_at IS NOT NULL
ORDER BY fact.tick, fact.sequence`, worldID)
	if err != nil {
		return fmt.Errorf("load city physical network facts: %w", err)
	}
	for rows.Next() {
		var item CityPhysicalNetworkFact
		var commandSequence sql.NullInt64
		if err = rows.Scan(
			&item.Tick, &item.Sequence, &item.Phase, &commandSequence,
			&item.FactType, &item.SubjectKind, &item.SubjectCode,
			&item.VersionBefore, &item.VersionAfter, &item.Payload,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city physical network fact: %w", err)
		}
		item.SourceCommandSequence = nullInt64Pointer(commandSequence)
		state.Facts = append(state.Facts, item)
	}
	return closeCityRows(rows, "iterate city physical network facts")
}

func loadCityPhysicalNetworkFlowBatches(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
	state *CityPhysicalNetworkStateSet,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT batch.tick, batch.sequence, network.code, service.code,
       batch.topology_revision, batch.allocation_count, batch.path_count,
       batch.segment_count, batch.dispatched_units, batch.network_received_units,
       batch.network_loss_units, fact.tick, fact.sequence, batch.metadata
FROM city_physical_network_flow_batches batch
JOIN city_physical_networks network ON network.id = batch.network_id
JOIN city_service_definitions service ON service.id = network.service_definition_id
JOIN city_physical_network_facts fact ON fact.id = batch.source_fact_id
WHERE batch.world_id = $1 ORDER BY batch.tick, network.code`, worldID)
	if err != nil {
		return fmt.Errorf("load city physical network flow batches: %w", err)
	}
	for rows.Next() {
		var item CityPhysicalNetworkFlowBatch
		if err = rows.Scan(
			&item.Tick, &item.Sequence, &item.NetworkCode, &item.ServiceCode,
			&item.TopologyRevision, &item.AllocationCount, &item.PathCount,
			&item.SegmentCount, &item.DispatchedUnits, &item.NetworkReceivedUnits,
			&item.NetworkLossUnits, &item.SourceFactTick,
			&item.SourceFactSequence, &item.Metadata,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city physical network flow batch: %w", err)
		}
		state.Batches = append(state.Batches, item)
	}
	return closeCityRows(rows, "iterate city physical network flow batches")
}

func loadCityPhysicalNetworkFlowPaths(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
	state *CityPhysicalNetworkStateSet,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT batch.tick, batch.sequence, service_fact.sequence,
       path.allocation_index, path.path_index,
       network.code, connection.code, source.code, sink.code, path.hop_count,
       path.dispatched_units, path.network_received_units,
       path.network_loss_units, path.path_cost_units, path.path_hash, path.metadata
FROM city_physical_network_flow_paths path
JOIN city_physical_network_flow_batches batch ON batch.id = path.batch_id
JOIN city_service_facts service_fact ON service_fact.id = path.service_fact_id
JOIN city_physical_networks network ON network.id = batch.network_id
JOIN city_service_connections connection ON connection.id = path.connection_id
JOIN city_physical_network_nodes source ON source.id = path.source_node_id
JOIN city_physical_network_nodes sink ON sink.id = path.sink_node_id
WHERE path.world_id = $1
ORDER BY batch.tick, batch.sequence, service_fact.sequence,
         path.allocation_index, path.path_index`, worldID)
	if err != nil {
		return fmt.Errorf("load city physical network flow paths: %w", err)
	}
	for rows.Next() {
		var item CityPhysicalNetworkFlowPath
		if err = rows.Scan(
			&item.Tick, &item.Sequence, &item.ServiceSequence,
			&item.AllocationIndex, &item.PathIndex,
			&item.NetworkCode, &item.ConnectionCode, &item.SourceNodeCode,
			&item.SinkNodeCode, &item.HopCount, &item.DispatchedUnits,
			&item.NetworkReceivedUnits, &item.NetworkLossUnits,
			&item.PathCostUnits, &item.PathHash, &item.Metadata,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city physical network flow path: %w", err)
		}
		state.Paths = append(state.Paths, item)
	}
	return closeCityRows(rows, "iterate city physical network flow paths")
}

func loadCityPhysicalNetworkFlowSegments(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
	state *CityPhysicalNetworkStateSet,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT batch.tick, batch.sequence, service_fact.sequence,
       path.allocation_index, path.path_index, segment.segment_index,
       edge.code, segment.edge_version,
       segment.direction, source.code, sink.code, segment.edge_capacity_units,
       segment.loss_milli, segment.input_units, segment.output_units,
       segment.loss_units, segment.metadata
FROM city_physical_network_flow_segments segment
JOIN city_physical_network_flow_paths path ON path.id = segment.path_id
JOIN city_physical_network_flow_batches batch ON batch.id = path.batch_id
JOIN city_service_facts service_fact ON service_fact.id = path.service_fact_id
JOIN city_physical_network_edges edge ON edge.id = segment.edge_id
JOIN city_physical_network_nodes source ON source.id = segment.from_node_id
JOIN city_physical_network_nodes sink ON sink.id = segment.to_node_id
WHERE segment.world_id = $1
ORDER BY batch.tick, batch.sequence, service_fact.sequence,
         path.allocation_index, path.path_index, segment.segment_index`, worldID)
	if err != nil {
		return fmt.Errorf("load city physical network flow segments: %w", err)
	}
	for rows.Next() {
		var item CityPhysicalNetworkFlowSegment
		if err = rows.Scan(
			&item.Tick, &item.Sequence, &item.ServiceSequence,
			&item.AllocationIndex, &item.PathIndex,
			&item.SegmentIndex, &item.EdgeCode, &item.EdgeVersion,
			&item.Direction, &item.FromNodeCode, &item.ToNodeCode,
			&item.EdgeCapacityUnits, &item.LossMilli, &item.InputUnits,
			&item.OutputUnits, &item.LossUnits, &item.Metadata,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city physical network flow segment: %w", err)
		}
		state.Segments = append(state.Segments, item)
	}
	return closeCityRows(rows, "iterate city physical network flow segments")
}

func loadCityPhysicalNetworkResultsForTick(
	ctx context.Context, queryer citySQLQueryer, worldID, tick int64,
) ([]CityPhysicalNetworkFact, []CityPhysicalNetworkFlowBatch,
	[]CityPhysicalNetworkFlowPath, []CityPhysicalNetworkFlowSegment, error,
) {
	facts := make([]CityPhysicalNetworkFact, 0)
	factRows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, fact.phase, command.sequence, fact.fact_type,
       fact.subject_kind, fact.subject_code, fact.version_before,
       fact.version_after, fact.payload
FROM city_physical_network_facts fact
LEFT JOIN city_commands command
  ON command.id = fact.source_command_id AND command.world_id = fact.world_id
WHERE fact.world_id = $1 AND fact.tick = $2 AND fact.posted_at IS NOT NULL
ORDER BY fact.sequence`, worldID, tick)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load physical network facts for tick: %w", err)
	}
	for factRows.Next() {
		var item CityPhysicalNetworkFact
		var commandSequence sql.NullInt64
		if err = factRows.Scan(
			&item.Tick, &item.Sequence, &item.Phase, &commandSequence,
			&item.FactType, &item.SubjectKind, &item.SubjectCode,
			&item.VersionBefore, &item.VersionAfter, &item.Payload,
		); err != nil {
			_ = factRows.Close()
			return nil, nil, nil, nil, fmt.Errorf("scan physical network fact for tick: %w", err)
		}
		item.SourceCommandSequence = nullInt64Pointer(commandSequence)
		facts = append(facts, item)
	}
	if err = closeCityRows(factRows, "iterate physical network facts for tick"); err != nil {
		return nil, nil, nil, nil, err
	}

	batches := make([]CityPhysicalNetworkFlowBatch, 0)
	batchRows, err := queryer.QueryContext(ctx, `
SELECT batch.tick, batch.sequence, network.code, service.code,
       batch.topology_revision, batch.allocation_count, batch.path_count,
       batch.segment_count, batch.dispatched_units, batch.network_received_units,
       batch.network_loss_units, fact.tick, fact.sequence, batch.metadata
FROM city_physical_network_flow_batches batch
JOIN city_physical_networks network ON network.id = batch.network_id
JOIN city_service_definitions service ON service.id = network.service_definition_id
JOIN city_physical_network_facts fact ON fact.id = batch.source_fact_id
WHERE batch.world_id = $1 AND batch.tick = $2
ORDER BY batch.sequence, network.code`, worldID, tick)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load physical network batches for tick: %w", err)
	}
	for batchRows.Next() {
		var item CityPhysicalNetworkFlowBatch
		if err = batchRows.Scan(
			&item.Tick, &item.Sequence, &item.NetworkCode, &item.ServiceCode,
			&item.TopologyRevision, &item.AllocationCount, &item.PathCount,
			&item.SegmentCount, &item.DispatchedUnits, &item.NetworkReceivedUnits,
			&item.NetworkLossUnits, &item.SourceFactTick,
			&item.SourceFactSequence, &item.Metadata,
		); err != nil {
			_ = batchRows.Close()
			return nil, nil, nil, nil, fmt.Errorf("scan physical network batch for tick: %w", err)
		}
		batches = append(batches, item)
	}
	if err = closeCityRows(batchRows, "iterate physical network batches for tick"); err != nil {
		return nil, nil, nil, nil, err
	}

	paths := make([]CityPhysicalNetworkFlowPath, 0)
	pathRows, err := queryer.QueryContext(ctx, `
SELECT batch.tick, batch.sequence, service_fact.sequence,
       path.allocation_index, path.path_index, network.code, connection.code,
       source.code, sink.code, path.hop_count, path.dispatched_units,
       path.network_received_units, path.network_loss_units,
       path.path_cost_units, path.path_hash, path.metadata
FROM city_physical_network_flow_paths path
JOIN city_physical_network_flow_batches batch ON batch.id = path.batch_id
JOIN city_service_facts service_fact ON service_fact.id = path.service_fact_id
JOIN city_physical_networks network ON network.id = batch.network_id
JOIN city_service_connections connection ON connection.id = path.connection_id
JOIN city_physical_network_nodes source ON source.id = path.source_node_id
JOIN city_physical_network_nodes sink ON sink.id = path.sink_node_id
WHERE path.world_id = $1 AND batch.tick = $2
ORDER BY batch.sequence, service_fact.sequence,
         path.allocation_index, path.path_index`, worldID, tick)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load physical network paths for tick: %w", err)
	}
	for pathRows.Next() {
		var item CityPhysicalNetworkFlowPath
		if err = pathRows.Scan(
			&item.Tick, &item.Sequence, &item.ServiceSequence,
			&item.AllocationIndex, &item.PathIndex, &item.NetworkCode,
			&item.ConnectionCode, &item.SourceNodeCode, &item.SinkNodeCode,
			&item.HopCount, &item.DispatchedUnits, &item.NetworkReceivedUnits,
			&item.NetworkLossUnits, &item.PathCostUnits, &item.PathHash,
			&item.Metadata,
		); err != nil {
			_ = pathRows.Close()
			return nil, nil, nil, nil, fmt.Errorf("scan physical network path for tick: %w", err)
		}
		paths = append(paths, item)
	}
	if err = closeCityRows(pathRows, "iterate physical network paths for tick"); err != nil {
		return nil, nil, nil, nil, err
	}

	segments := make([]CityPhysicalNetworkFlowSegment, 0)
	segmentRows, err := queryer.QueryContext(ctx, `
SELECT batch.tick, batch.sequence, service_fact.sequence,
       path.allocation_index, path.path_index, segment.segment_index,
       edge.code, segment.edge_version, segment.direction,
       source.code, sink.code, segment.edge_capacity_units,
       segment.loss_milli, segment.input_units, segment.output_units,
       segment.loss_units, segment.metadata
FROM city_physical_network_flow_segments segment
JOIN city_physical_network_flow_paths path ON path.id = segment.path_id
JOIN city_physical_network_flow_batches batch ON batch.id = path.batch_id
JOIN city_service_facts service_fact ON service_fact.id = path.service_fact_id
JOIN city_physical_network_edges edge ON edge.id = segment.edge_id
JOIN city_physical_network_nodes source ON source.id = segment.from_node_id
JOIN city_physical_network_nodes sink ON sink.id = segment.to_node_id
WHERE segment.world_id = $1 AND batch.tick = $2
ORDER BY batch.sequence, service_fact.sequence, path.allocation_index,
         path.path_index, segment.segment_index`, worldID, tick)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load physical network segments for tick: %w", err)
	}
	for segmentRows.Next() {
		var item CityPhysicalNetworkFlowSegment
		if err = segmentRows.Scan(
			&item.Tick, &item.Sequence, &item.ServiceSequence,
			&item.AllocationIndex, &item.PathIndex, &item.SegmentIndex,
			&item.EdgeCode, &item.EdgeVersion, &item.Direction,
			&item.FromNodeCode, &item.ToNodeCode, &item.EdgeCapacityUnits,
			&item.LossMilli, &item.InputUnits, &item.OutputUnits,
			&item.LossUnits, &item.Metadata,
		); err != nil {
			_ = segmentRows.Close()
			return nil, nil, nil, nil, fmt.Errorf("scan physical network segment for tick: %w", err)
		}
		segments = append(segments, item)
	}
	if err = closeCityRows(segmentRows, "iterate physical network segments for tick"); err != nil {
		return nil, nil, nil, nil, err
	}
	return facts, batches, paths, segments, nil
}
