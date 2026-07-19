package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

type cityPhysicalNetworkRecoveryFactKey struct {
	tick     int64
	sequence int64
}

type cityPhysicalNetworkRecoveryPathKey struct {
	tick            int64
	sequence        int64
	serviceSequence int64
	allocationIndex int
	pathIndex       int
}

func validateCityPhysicalNetworkRecoveryState(state *cityHashState) error {
	if state == nil || state.PhysicalNetworks == nil || state.PublicServices == nil ||
		!cityEngineSupportsPhysicalNetworks(state.SimulationVersion) || state.CurrentTick < 0 {
		return fmt.Errorf("recovery F8.2 physical-network state is unavailable")
	}
	physical := state.PhysicalNetworks
	profile := physical.Profile
	if profile.PolicyID != cityPhysicalNetworkPolicyID ||
		profile.PolicyVersion != cityPhysicalNetworkPolicyVersion ||
		profile.BaselineTick < 0 || profile.BaselineTick > state.CurrentTick ||
		profile.PolicyCount != int64(len(physical.Policies)) ||
		profile.NetworkCount != int64(len(physical.Networks)) ||
		profile.NodeCount != int64(len(physical.Nodes)) ||
		profile.EdgeCount != int64(len(physical.Edges)) ||
		profile.FactCount != int64(len(physical.Facts)) ||
		profile.BatchCount != int64(len(physical.Batches)) ||
		profile.PathCount != int64(len(physical.Paths)) ||
		profile.SegmentCount != int64(len(physical.Segments)) ||
		profile.Revision != profile.FactCount+1 ||
		profile.NetworkCount > cityPhysicalNetworkMaximumNodes ||
		profile.NodeCount > cityPhysicalNetworkMaximumNodes ||
		profile.EdgeCount > cityPhysicalNetworkMaximumEdges || !json.Valid(profile.Metadata) {
		return fmt.Errorf("recovery physical-network profile is inconsistent")
	}
	expectedPolicies, expectedHash, err := cityPhysicalNetworkPolicyCatalog(
		state.PublicServices.ServiceDefinitions,
	)
	if err != nil || profile.PolicyHash != expectedHash ||
		!cityPhysicalNetworkPoliciesEqual(physical.Policies, expectedPolicies) {
		return fmt.Errorf("recovery physical-network policy catalog is inconsistent")
	}
	lastTick, lastSequence := int64(0), int64(0)
	for _, fact := range physical.Facts {
		if fact.Tick <= 0 || fact.Tick > state.CurrentTick || fact.Sequence <= 0 ||
			(fact.Tick == lastTick && fact.Sequence != lastSequence+1) ||
			(fact.Tick != lastTick && fact.Sequence != 1) || !json.Valid(fact.Payload) {
			return fmt.Errorf("recovery physical-network fact sequence is inconsistent")
		}
		lastTick, lastSequence = fact.Tick, fact.Sequence
	}
	baseline, err := deriveCityPhysicalNetworkRecoveryBaseline(physical)
	if err != nil {
		return err
	}
	replayed := &cityHashState{
		SimulationVersion: state.SimulationVersion,
		CurrentTick:       state.CurrentTick,
		PublicServices:    state.PublicServices,
		PhysicalNetworks:  baseline,
	}
	for _, fact := range physical.Facts {
		if err = reduceCityPhysicalNetworkFact(replayed, fact); err != nil {
			return fmt.Errorf("replay recovery physical-network fact %d/%d: %w",
				fact.Tick, fact.Sequence, err)
		}
	}
	sortCityPhysicalNetworkState(replayed.PhysicalNetworks)
	target, err := cloneCityPhysicalNetworkState(physical)
	if err != nil {
		return err
	}
	sortCityPhysicalNetworkState(target)
	if !reflect.DeepEqual(replayed.PhysicalNetworks, target) {
		return fmt.Errorf("recovery physical-network projection does not match fact replay")
	}
	return nil
}

func cityPhysicalNetworkPoliciesEqual(
	actual, expected []CityPhysicalNetworkPolicy,
) bool {
	if len(actual) != len(expected) {
		return false
	}
	actual = append([]CityPhysicalNetworkPolicy(nil), actual...)
	expected = append([]CityPhysicalNetworkPolicy(nil), expected...)
	sort.Slice(actual, func(i, j int) bool { return actual[i].ServiceCode < actual[j].ServiceCode })
	sort.Slice(expected, func(i, j int) bool { return expected[i].ServiceCode < expected[j].ServiceCode })
	for index := range actual {
		leftPayload, rightPayload := actual[index].Payload, expected[index].Payload
		actual[index].Payload, expected[index].Payload = nil, nil
		if !reflect.DeepEqual(actual[index], expected[index]) ||
			!worldRuntimeJSONEqual(leftPayload, rightPayload) {
			return false
		}
	}
	return true
}

func deriveCityPhysicalNetworkRecoveryBaseline(
	target *cityPhysicalNetworkHashState,
) (*cityPhysicalNetworkHashState, error) {
	current, err := cloneCityPhysicalNetworkState(target)
	if err != nil {
		return nil, err
	}
	for index := len(current.Facts) - 1; index >= 0; index-- {
		fact := current.Facts[index]
		switch fact.FactType {
		case CityPhysicalNetworkFactNetworkConfigured,
			CityPhysicalNetworkFactNodeConfigured,
			CityPhysicalNetworkFactEdgeConfigured,
			CityPhysicalNetworkFactEdgeStateChanged:
			if err := reverseCityPhysicalNetworkCommandFact(current, fact); err != nil {
				return nil, err
			}
			continue
		}
		if fact.FactType != CityPhysicalNetworkFactTopologySynchronized {
			continue
		}
		var payload cityPhysicalNetworkTopologyFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode recovery physical topology fact: %w", err)
		}
		after, err := cityPhysicalNetworkTopologyProjectionFromState(current, fact.SubjectCode)
		if err != nil {
			return nil, err
		}
		afterHash, err := cityPhysicalNetworkTopologyProjectionHash(after)
		if err != nil || afterHash != payload.AfterProjectionHash {
			return nil, fmt.Errorf("recovery physical topology after hash is inconsistent")
		}
		networkIndex := cityPhysicalNetworkIndex(current.Networks, fact.SubjectCode)
		if networkIndex < 0 || !cityPhysicalNetworkSnapshotEqual(
			current.Networks[networkIndex], payload.NetworkAfter,
		) {
			return nil, fmt.Errorf("recovery physical topology network after snapshot is inconsistent")
		}
		edgeBefore := make(map[string]CityPhysicalNetworkEdge, len(payload.EdgeBefore))
		for _, item := range payload.EdgeBefore {
			edgeBefore[item.Code] = item
		}
		for _, upsert := range payload.EdgeUpserts {
			projectionIndex := cityPhysicalNetworkEdgeIndex(current.Edges, upsert.NetworkCode, upsert.Code)
			if projectionIndex < 0 || !cityPhysicalNetworkEdgeSnapshotEqual(current.Edges[projectionIndex], upsert) {
				return nil, fmt.Errorf("recovery physical topology edge after snapshot is inconsistent")
			}
			before, existed := edgeBefore[upsert.Code]
			if upsert.Version == 1 {
				if existed {
					return nil, fmt.Errorf("recovery physical topology edge creation has before state")
				}
				current.Edges = append(current.Edges[:projectionIndex], current.Edges[projectionIndex+1:]...)
			} else {
				if !existed || before.Version+1 != upsert.Version {
					return nil, fmt.Errorf("recovery physical topology edge before snapshot is missing")
				}
				current.Edges[projectionIndex] = before
			}
		}
		nodeBefore := make(map[string]CityPhysicalNetworkNode, len(payload.NodeBefore))
		for _, item := range payload.NodeBefore {
			nodeBefore[item.Code] = item
		}
		for _, upsert := range payload.NodeUpserts {
			projectionIndex := cityPhysicalNetworkNodeIndex(current.Nodes, upsert.NetworkCode, upsert.Code)
			if projectionIndex < 0 || !cityPhysicalNetworkNodeSnapshotEqual(current.Nodes[projectionIndex], upsert) {
				return nil, fmt.Errorf("recovery physical topology node after snapshot is inconsistent")
			}
			before, existed := nodeBefore[upsert.Code]
			if upsert.Version == 1 {
				if existed {
					return nil, fmt.Errorf("recovery physical topology node creation has before state")
				}
				current.Nodes = append(current.Nodes[:projectionIndex], current.Nodes[projectionIndex+1:]...)
			} else {
				if !existed || before.Version+1 != upsert.Version {
					return nil, fmt.Errorf("recovery physical topology node before snapshot is missing")
				}
				current.Nodes[projectionIndex] = before
			}
		}
		if payload.NetworkBefore == nil {
			if payload.NetworkAfter.Version != 1 {
				return nil, fmt.Errorf("recovery physical topology network creation chain is invalid")
			}
			current.Networks = append(current.Networks[:networkIndex], current.Networks[networkIndex+1:]...)
		} else {
			if payload.NetworkBefore.Version+1 != payload.NetworkAfter.Version {
				return nil, fmt.Errorf("recovery physical topology network before snapshot is invalid")
			}
			current.Networks[networkIndex] = *payload.NetworkBefore
		}
		before, err := cityPhysicalNetworkTopologyProjectionFromState(current, fact.SubjectCode)
		if err != nil {
			return nil, err
		}
		beforeHash, err := cityPhysicalNetworkTopologyProjectionHash(before)
		if err != nil || beforeHash != payload.BeforeProjectionHash {
			return nil, fmt.Errorf("recovery physical topology before hash is inconsistent")
		}
	}
	current.Facts = make([]CityPhysicalNetworkFact, 0)
	current.Batches = make([]CityPhysicalNetworkFlowBatch, 0)
	current.Paths = make([]CityPhysicalNetworkFlowPath, 0)
	current.Segments = make([]CityPhysicalNetworkFlowSegment, 0)
	current.Profile.NetworkCount = int64(len(current.Networks))
	current.Profile.NodeCount = int64(len(current.Nodes))
	current.Profile.EdgeCount = int64(len(current.Edges))
	current.Profile.FactCount = 0
	current.Profile.BatchCount = 0
	current.Profile.PathCount = 0
	current.Profile.SegmentCount = 0
	current.Profile.Revision = 1
	sortCityPhysicalNetworkState(current)
	return current, nil
}

func reverseCityPhysicalNetworkCommandFact(
	state *cityPhysicalNetworkHashState, fact CityPhysicalNetworkFact,
) error {
	if state == nil || fact.Phase != "command" || fact.SourceCommandSequence == nil {
		return fmt.Errorf("recovery physical-network command fact is invalid")
	}
	switch fact.FactType {
	case CityPhysicalNetworkFactNetworkConfigured:
		var payload cityPhysicalNetworkConfigureFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode recovery configured network fact: %w", err)
		}
		index := cityPhysicalNetworkIndex(state.Networks, payload.NetworkAfter.Code)
		if index < 0 || !cityPhysicalNetworkSnapshotEqual(state.Networks[index], payload.NetworkAfter) {
			return fmt.Errorf("recovery configured network after snapshot is inconsistent")
		}
		if payload.NetworkBefore == nil {
			for _, node := range state.Nodes {
				if node.NetworkCode == payload.NetworkAfter.Code {
					return fmt.Errorf("recovery configured network still has child nodes")
				}
			}
			state.Networks = append(state.Networks[:index], state.Networks[index+1:]...)
		} else {
			state.Networks[index] = *payload.NetworkBefore
		}
	case CityPhysicalNetworkFactNodeConfigured:
		var payload cityPhysicalNetworkNodeConfigureFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode recovery configured node fact: %w", err)
		}
		networkIndex := cityPhysicalNetworkIndex(state.Networks, payload.NetworkAfter.Code)
		nodeIndex := cityPhysicalNetworkNodeIndex(
			state.Nodes, payload.NodeAfter.NetworkCode, payload.NodeAfter.Code,
		)
		if networkIndex < 0 || nodeIndex < 0 ||
			!cityPhysicalNetworkSnapshotEqual(state.Networks[networkIndex], payload.NetworkAfter) ||
			!cityPhysicalNetworkNodeSnapshotEqual(state.Nodes[nodeIndex], payload.NodeAfter) {
			return fmt.Errorf("recovery configured node after snapshot is inconsistent")
		}
		if payload.NodeBefore == nil {
			for _, edge := range state.Edges {
				if edge.NetworkCode == payload.NodeAfter.NetworkCode &&
					(edge.FromNodeCode == payload.NodeAfter.Code || edge.ToNodeCode == payload.NodeAfter.Code) {
					return fmt.Errorf("recovery configured node still has child edges")
				}
			}
			state.Nodes = append(state.Nodes[:nodeIndex], state.Nodes[nodeIndex+1:]...)
		} else {
			state.Nodes[nodeIndex] = *payload.NodeBefore
		}
		state.Networks[networkIndex] = payload.NetworkBefore
	case CityPhysicalNetworkFactEdgeConfigured, CityPhysicalNetworkFactEdgeStateChanged:
		var payload cityPhysicalNetworkEdgeConfigureFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode recovery configured edge fact: %w", err)
		}
		networkIndex := cityPhysicalNetworkIndex(state.Networks, payload.NetworkAfter.Code)
		edgeIndex := cityPhysicalNetworkEdgeIndex(
			state.Edges, payload.EdgeAfter.NetworkCode, payload.EdgeAfter.Code,
		)
		if networkIndex < 0 || edgeIndex < 0 ||
			!cityPhysicalNetworkSnapshotEqual(state.Networks[networkIndex], payload.NetworkAfter) ||
			!cityPhysicalNetworkEdgeSnapshotEqual(state.Edges[edgeIndex], payload.EdgeAfter) {
			return fmt.Errorf("recovery configured edge after snapshot is inconsistent")
		}
		if payload.EdgeBefore == nil {
			state.Edges = append(state.Edges[:edgeIndex], state.Edges[edgeIndex+1:]...)
		} else {
			state.Edges[edgeIndex] = *payload.EdgeBefore
		}
		state.Networks[networkIndex] = payload.NetworkBefore
	default:
		return fmt.Errorf("unsupported recovery physical-network command fact %q", fact.FactType)
	}
	return nil
}

func cloneCityPhysicalNetworkState(
	state *cityPhysicalNetworkHashState,
) (*cityPhysicalNetworkHashState, error) {
	if state == nil {
		return nil, nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("clone physical-network state: %w", err)
	}
	var cloned cityPhysicalNetworkHashState
	if err = json.Unmarshal(raw, &cloned); err != nil {
		return nil, fmt.Errorf("decode cloned physical-network state: %w", err)
	}
	return &cloned, nil
}

func loadCityPhysicalNetworkRecoveryFactIDs(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (map[cityPhysicalNetworkRecoveryFactKey]int64, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, tick, sequence FROM city_physical_network_facts
WHERE world_id = $1 ORDER BY tick, sequence`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load physical-network recovery fact identities: %w", err)
	}
	identities := make(map[cityPhysicalNetworkRecoveryFactKey]int64)
	for rows.Next() {
		var id int64
		var key cityPhysicalNetworkRecoveryFactKey
		if err = rows.Scan(&id, &key.tick, &key.sequence); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan physical-network recovery fact identity: %w", err)
		}
		if _, duplicate := identities[key]; duplicate {
			_ = rows.Close()
			return nil, fmt.Errorf("duplicate physical-network recovery fact identity")
		}
		identities[key] = id
	}
	if err = closeCityRows(rows, "iterate physical-network recovery fact identities"); err != nil {
		return nil, err
	}
	return identities, nil
}

func clearCityPhysicalNetworkProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (int, error) {
	tables := []string{
		"city_physical_network_flow_segments", "city_physical_network_flow_paths",
		"city_physical_network_flow_batches", "city_physical_network_edges",
		"city_physical_network_nodes", "city_physical_networks",
		"city_physical_network_facts", "city_physical_network_policies",
		"city_physical_network_profiles",
	}
	count := 0
	for _, table := range tables {
		result, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE world_id = $1`, worldID)
		if err != nil {
			return count, fmt.Errorf("clear recovery %s: %w", table, err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return count, fmt.Errorf("count cleared recovery %s: %w", table, err)
		}
		count += int(rows)
	}
	return count, nil
}

func restoreCityPhysicalNetworkProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	state *cityHashState,
	preservedFactIDs map[cityPhysicalNetworkRecoveryFactKey]int64,
) (int, error) {
	if err := validateCityPhysicalNetworkRecoveryState(state); err != nil {
		return 0, err
	}
	physical := state.PhysicalNetworks
	count := 0
	serviceIDs := make(map[string]int64, len(physical.Policies))
	for _, policy := range physical.Policies {
		var serviceID int64
		if err := tx.QueryRowContext(ctx, `
SELECT id FROM city_service_definitions WHERE world_id = $1 AND code = $2`,
			worldID, policy.ServiceCode).Scan(&serviceID); err != nil {
			return count, fmt.Errorf("resolve physical-network policy service %s: %w", policy.ServiceCode, err)
		}
		serviceIDs[policy.ServiceCode] = serviceID
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_physical_network_policies
    (world_id, service_definition_id, policy_version, policy_hash,
     network_required, route_direction, maximum_nodes, maximum_edges,
     maximum_paths, maximum_hops, loss_cost_weight, allow_bidirectional,
     algorithm_version, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`,
			worldID, serviceID, policy.PolicyVersion, policy.PolicyHash,
			policy.NetworkRequired, policy.RouteDirection, policy.MaximumNodes,
			policy.MaximumEdges, policy.MaximumPaths, policy.MaximumHops,
			policy.LossCostWeight, policy.AllowBidirectional,
			policy.AlgorithmVersion, policy.Payload); err != nil {
			return count, fmt.Errorf("restore physical-network policy %s: %w", policy.ServiceCode, err)
		}
		count++
	}
	factIDs := make(map[cityPhysicalNetworkRecoveryFactKey]int64, len(physical.Facts))
	for _, fact := range physical.Facts {
		var sourceCommandID any
		if fact.SourceCommandSequence != nil {
			var commandID int64
			if err := tx.QueryRowContext(ctx, `
SELECT id FROM city_commands
WHERE world_id = $1 AND processed_tick = $2 AND sequence = $3 AND status = 'applied'`,
				worldID, fact.Tick, *fact.SourceCommandSequence).Scan(&commandID); err != nil {
				return count, fmt.Errorf("resolve physical-network fact command %d/%d: %w",
					fact.Tick, *fact.SourceCommandSequence, err)
			}
			sourceCommandID = commandID
		}
		key := cityPhysicalNetworkRecoveryFactKey{tick: fact.Tick, sequence: fact.Sequence}
		preservedID, preserve := preservedFactIDs[key]
		var id int64
		var err error
		if preserve {
			err = tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_facts
    (id, world_id, tick, sequence, phase, source_command_id, fact_type,
     subject_kind, subject_code, version_before, version_after, payload, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, NOW())
RETURNING id`, preservedID, worldID, fact.Tick, fact.Sequence, fact.Phase,
				sourceCommandID, fact.FactType, fact.SubjectKind, fact.SubjectCode,
				fact.VersionBefore, fact.VersionAfter, fact.Payload).Scan(&id)
		} else {
			err = tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_facts
    (world_id, tick, sequence, phase, source_command_id, fact_type,
     subject_kind, subject_code, version_before, version_after, payload, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, NOW())
RETURNING id`, worldID, fact.Tick, fact.Sequence, fact.Phase, sourceCommandID,
				fact.FactType, fact.SubjectKind, fact.SubjectCode, fact.VersionBefore,
				fact.VersionAfter, fact.Payload).Scan(&id)
		}
		if err != nil {
			return count, fmt.Errorf("restore physical-network fact %d/%d: %w", fact.Tick, fact.Sequence, err)
		}
		factIDs[key] = id
		count++
	}
	networkIDs := make(map[string]int64, len(physical.Networks))
	for _, network := range physical.Networks {
		serviceID, exists := serviceIDs[network.ServiceCode]
		if !exists {
			return count, fmt.Errorf("physical network %s references unknown service", network.Code)
		}
		sourceFactID, err := cityPhysicalNetworkRecoverySourceFactID(
			factIDs, network.SourceFactTick, network.SourceFactSequence,
		)
		if err != nil {
			return count, err
		}
		var id int64
		if err = tx.QueryRowContext(ctx, `
INSERT INTO city_physical_networks
    (world_id, code, name, service_definition_id, status, topology_revision,
     created_tick, updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)
RETURNING id`, worldID, network.Code, network.Name, serviceID, network.Status,
			network.TopologyRevision, network.CreatedTick, network.UpdatedTick,
			network.Version, sourceFactID, network.Metadata).Scan(&id); err != nil {
			return count, fmt.Errorf("restore physical network %s: %w", network.Code, err)
		}
		networkIDs[network.Code] = id
		count++
	}
	nodeIDs := make(map[string]int64, len(physical.Nodes))
	for _, node := range physical.Nodes {
		networkID, exists := networkIDs[node.NetworkCode]
		if !exists {
			return count, fmt.Errorf("physical-network node %s references unknown network", node.Code)
		}
		capacityID, err := resolveCityPhysicalNetworkRecoveryCapacityID(ctx, tx, worldID, node.CapacityCode)
		if err != nil {
			return count, fmt.Errorf("resolve physical-network node capacity %s: %w", node.Code, err)
		}
		demandID, err := resolveCityPhysicalNetworkRecoveryCodeID(
			ctx, tx, "city_service_demands", worldID, node.DemandCode,
		)
		if err != nil {
			return count, fmt.Errorf("resolve physical-network node demand %s: %w", node.Code, err)
		}
		districtID, err := resolveCityPhysicalNetworkRecoveryCodeID(
			ctx, tx, "city_districts", worldID, node.DistrictCode,
		)
		if err != nil {
			return count, fmt.Errorf("resolve physical-network node district %s: %w", node.Code, err)
		}
		buildingID, err := resolveCityPhysicalNetworkRecoveryCodeID(
			ctx, tx, "city_buildings", worldID, node.BuildingCode,
		)
		if err != nil {
			return count, fmt.Errorf("resolve physical-network node building %s: %w", node.Code, err)
		}
		sourceFactID, err := cityPhysicalNetworkRecoverySourceFactID(
			factIDs, node.SourceFactTick, node.SourceFactSequence,
		)
		if err != nil {
			return count, err
		}
		var id int64
		if err = tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_nodes
    (world_id, network_id, code, role, capacity_id, demand_id, district_id,
     building_id, world_x, world_y, world_z, status, created_tick,
     updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17::jsonb)
RETURNING id`, worldID, networkID, node.Code, node.Role, capacityID, demandID,
			districtID, buildingID, cityPhysicalNetworkRecoveryNullableInt64(node.WorldX),
			cityPhysicalNetworkRecoveryNullableInt64(node.WorldY),
			cityPhysicalNetworkRecoveryNullableInt(node.WorldZ), node.Status,
			node.CreatedTick, node.UpdatedTick, node.Version, sourceFactID,
			node.Metadata).Scan(&id); err != nil {
			return count, fmt.Errorf("restore physical-network node %s: %w", node.Code, err)
		}
		nodeIDs[node.Code] = id
		count++
	}
	edgeIDs := make(map[string]int64, len(physical.Edges))
	for _, edge := range physical.Edges {
		networkID, networkExists := networkIDs[edge.NetworkCode]
		fromNodeID, fromExists := nodeIDs[edge.FromNodeCode]
		toNodeID, toExists := nodeIDs[edge.ToNodeCode]
		if !networkExists || !fromExists || !toExists {
			return count, fmt.Errorf("physical-network edge %s references unknown topology", edge.Code)
		}
		sourceFactID, err := cityPhysicalNetworkRecoverySourceFactID(
			factIDs, edge.SourceFactTick, edge.SourceFactSequence,
		)
		if err != nil {
			return count, err
		}
		var id int64
		if err = tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_edges
    (world_id, network_id, code, from_node_id, to_node_id, direction,
     installed_capacity_units, availability_milli, available_capacity_units,
     loss_milli, base_cost_units, status, condition_milli, failure_count,
     created_tick, updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19::jsonb)
RETURNING id`, worldID, networkID, edge.Code, fromNodeID, toNodeID,
			edge.Direction, edge.InstalledCapacityUnits, edge.AvailabilityMilli,
			edge.AvailableCapacityUnits, edge.LossMilli, edge.BaseCostUnits,
			edge.Status, edge.ConditionMilli, edge.FailureCount, edge.CreatedTick,
			edge.UpdatedTick, edge.Version, sourceFactID, edge.Metadata).Scan(&id); err != nil {
			return count, fmt.Errorf("restore physical-network edge %s: %w", edge.Code, err)
		}
		edgeIDs[edge.Code] = id
		count++
	}
	batchIDs := make(map[cityPhysicalNetworkRecoveryFactKey]int64, len(physical.Batches))
	for _, batch := range physical.Batches {
		networkID, networkExists := networkIDs[batch.NetworkCode]
		factID, factExists := factIDs[cityPhysicalNetworkRecoveryFactKey{
			tick: batch.SourceFactTick, sequence: batch.SourceFactSequence,
		}]
		if !networkExists || !factExists {
			return count, fmt.Errorf("physical-network flow batch %d/%d references unknown identity",
				batch.Tick, batch.Sequence)
		}
		var id int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_flow_batches
    (world_id, network_id, tick, sequence, source_fact_id, topology_revision,
     allocation_count, path_count, segment_count, dispatched_units,
     network_received_units, network_loss_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)
RETURNING id`, worldID, networkID, batch.Tick, batch.Sequence, factID,
			batch.TopologyRevision, batch.AllocationCount, batch.PathCount,
			batch.SegmentCount, batch.DispatchedUnits, batch.NetworkReceivedUnits,
			batch.NetworkLossUnits, batch.Metadata).Scan(&id); err != nil {
			return count, fmt.Errorf("restore physical-network flow batch %d/%d: %w",
				batch.Tick, batch.Sequence, err)
		}
		batchIDs[cityPhysicalNetworkRecoveryFactKey{tick: batch.Tick, sequence: batch.Sequence}] = id
		count++
	}
	connectionIDs := make(map[string]int64, len(state.PublicServices.Connections))
	for _, connection := range state.PublicServices.Connections {
		var id int64
		if err := tx.QueryRowContext(ctx, `
SELECT id FROM city_service_connections WHERE world_id = $1 AND code = $2`,
			worldID, connection.Code).Scan(&id); err != nil {
			return count, fmt.Errorf("resolve physical-network flow connection %s: %w", connection.Code, err)
		}
		connectionIDs[connection.Code] = id
	}
	pathIDs := make(map[cityPhysicalNetworkRecoveryPathKey]int64, len(physical.Paths))
	for _, path := range physical.Paths {
		batchID, batchExists := batchIDs[cityPhysicalNetworkRecoveryFactKey{
			tick: path.Tick, sequence: path.Sequence,
		}]
		connectionID, connectionExists := connectionIDs[path.ConnectionCode]
		sourceNodeID, sourceExists := nodeIDs[path.SourceNodeCode]
		sinkNodeID, sinkExists := nodeIDs[path.SinkNodeCode]
		if !batchExists || !connectionExists || !sourceExists || !sinkExists {
			return count, fmt.Errorf("physical-network path references unknown identity")
		}
		var serviceFactID int64
		if err := tx.QueryRowContext(ctx, `
SELECT id FROM city_service_facts
WHERE world_id = $1 AND tick = $2 AND sequence = $3`,
			worldID, path.Tick, path.ServiceSequence).Scan(&serviceFactID); err != nil {
			return count, fmt.Errorf("resolve physical-network path service fact %d/%d: %w",
				path.Tick, path.ServiceSequence, err)
		}
		var id int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_flow_paths
    (world_id, batch_id, service_fact_id, allocation_index, path_index,
     connection_id, source_node_id, sink_node_id, hop_count,
     dispatched_units, network_received_units, network_loss_units,
     path_cost_units, path_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb)
RETURNING id`, worldID, batchID, serviceFactID, path.AllocationIndex,
			path.PathIndex, connectionID, sourceNodeID, sinkNodeID, path.HopCount,
			path.DispatchedUnits, path.NetworkReceivedUnits, path.NetworkLossUnits,
			path.PathCostUnits, path.PathHash, path.Metadata).Scan(&id); err != nil {
			return count, fmt.Errorf("restore physical-network flow path: %w", err)
		}
		key := cityPhysicalNetworkRecoveryPathKey{
			tick: path.Tick, sequence: path.Sequence,
			serviceSequence: path.ServiceSequence, allocationIndex: path.AllocationIndex,
			pathIndex: path.PathIndex,
		}
		pathIDs[key] = id
		count++
	}
	for _, segment := range physical.Segments {
		pathID, pathExists := pathIDs[cityPhysicalNetworkRecoveryPathKey{
			tick: segment.Tick, sequence: segment.Sequence,
			serviceSequence: segment.ServiceSequence,
			allocationIndex: segment.AllocationIndex, pathIndex: segment.PathIndex,
		}]
		edgeID, edgeExists := edgeIDs[segment.EdgeCode]
		fromNodeID, fromExists := nodeIDs[segment.FromNodeCode]
		toNodeID, toExists := nodeIDs[segment.ToNodeCode]
		if !pathExists || !edgeExists || !fromExists || !toExists {
			return count, fmt.Errorf("physical-network segment references unknown identity")
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_physical_network_flow_segments
    (world_id, path_id, segment_index, edge_id, edge_version, direction,
     from_node_id, to_node_id, edge_capacity_units, loss_milli,
     input_units, output_units, loss_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`,
			worldID, pathID, segment.SegmentIndex, edgeID, segment.EdgeVersion,
			segment.Direction, fromNodeID, toNodeID, segment.EdgeCapacityUnits,
			segment.LossMilli, segment.InputUnits, segment.OutputUnits,
			segment.LossUnits, segment.Metadata); err != nil {
			return count, fmt.Errorf("restore physical-network flow segment: %w", err)
		}
		count++
	}
	profile := physical.Profile
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_physical_network_profiles
    (world_id, policy_id, policy_version, policy_hash, baseline_tick,
     policy_count, network_count, node_count, edge_count, fact_count,
     batch_count, path_count, segment_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb)`,
		worldID, profile.PolicyID, profile.PolicyVersion, profile.PolicyHash,
		profile.BaselineTick, profile.PolicyCount, profile.NetworkCount,
		profile.NodeCount, profile.EdgeCount, profile.FactCount,
		profile.BatchCount, profile.PathCount, profile.SegmentCount,
		profile.Revision, profile.Metadata); err != nil {
		return count, fmt.Errorf("restore physical-network profile: %w", err)
	}
	count++
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_physical_network_foundation($1)`, worldID); err != nil {
		return count, fmt.Errorf("assert recovered physical-network foundation: %w", err)
	}
	return count, nil
}

func resolveCityPhysicalNetworkRecoveryCapacityID(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	code *string,
) (any, error) {
	if code == nil {
		return nil, nil
	}
	var id int64
	err := queryer.QueryRowContext(ctx, `
SELECT capacity.id
FROM city_facility_service_capacities capacity
JOIN city_facilities facility ON facility.id = capacity.facility_id
JOIN city_service_definitions service ON service.id = capacity.service_definition_id
WHERE capacity.world_id = $1 AND facility.code || '.' || service.code = $2`,
		worldID, *code).Scan(&id)
	return id, err
}

func resolveCityPhysicalNetworkRecoveryCodeID(
	ctx context.Context,
	queryer citySQLQueryer,
	table string,
	worldID int64,
	code *string,
) (any, error) {
	if code == nil {
		return nil, nil
	}
	var query string
	switch table {
	case "city_service_demands":
		query = `SELECT id FROM city_service_demands WHERE world_id = $1 AND code = $2`
	case "city_districts":
		query = `SELECT id FROM city_districts WHERE world_id = $1 AND code = $2`
	case "city_buildings":
		query = `SELECT id FROM city_buildings WHERE world_id = $1 AND code = $2`
	default:
		return nil, fmt.Errorf("unsupported physical-network recovery identity table")
	}
	var id int64
	err := queryer.QueryRowContext(ctx, query, worldID, *code).Scan(&id)
	return id, err
}

func cityPhysicalNetworkRecoverySourceFactID(
	identities map[cityPhysicalNetworkRecoveryFactKey]int64,
	tick, sequence int64,
) (any, error) {
	if tick == 0 && sequence == 0 {
		return nil, nil
	}
	id, exists := identities[cityPhysicalNetworkRecoveryFactKey{tick: tick, sequence: sequence}]
	if !exists {
		return nil, fmt.Errorf("physical-network source fact %d/%d is unavailable", tick, sequence)
	}
	return id, nil
}

func cityPhysicalNetworkRecoveryNullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func cityPhysicalNetworkRecoveryNullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func cityPhysicalNetworkRecoveryNullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
