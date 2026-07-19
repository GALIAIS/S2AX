package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
)

func replayCityPhysicalNetworkTopologyFacts(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	if state == nil || state.PhysicalNetworks == nil || tick <= 0 {
		return fmt.Errorf("physical-network replay state is unavailable")
	}
	facts, err := loadCityPhysicalNetworkReplayFacts(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	for _, fact := range facts {
		if fact.Phase != "command" && fact.Phase != CityPhysicalNetworkPhasePreNetwork {
			continue
		}
		if err = reduceCityPhysicalNetworkFact(state, fact); err != nil {
			return fmt.Errorf("reduce physical-network topology fact %d: %w", fact.Sequence, err)
		}
	}
	sortCityPhysicalNetworkState(state.PhysicalNetworks)
	return nil
}

func replayCityPhysicalNetworkFlowFacts(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	if state == nil || state.PhysicalNetworks == nil || state.PublicServices == nil || tick <= 0 {
		return fmt.Errorf("physical-network flow replay state is unavailable")
	}
	facts, err := loadCityPhysicalNetworkReplayFacts(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	for _, fact := range facts {
		if fact.Phase != CityPhysicalNetworkPhaseSettlement {
			continue
		}
		if err = reduceCityPhysicalNetworkFact(state, fact); err != nil {
			return fmt.Errorf("reduce physical-network flow fact %d: %w", fact.Sequence, err)
		}
	}
	sortCityPhysicalNetworkState(state.PhysicalNetworks)
	return nil
}

func loadCityPhysicalNetworkReplayFacts(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
) ([]CityPhysicalNetworkFact, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, fact.phase, command.sequence, fact.fact_type,
       fact.subject_kind, fact.subject_code, fact.version_before,
       fact.version_after, fact.payload
FROM city_physical_network_facts fact
LEFT JOIN city_commands command
  ON command.id = fact.source_command_id AND command.world_id = fact.world_id
WHERE fact.world_id = $1 AND fact.tick = $2 AND fact.posted_at IS NOT NULL
ORDER BY fact.sequence ASC`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load replay physical-network facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	facts := make([]CityPhysicalNetworkFact, 0)
	expectedSequence := int64(1)
	preNetworkPhaseSeen := false
	settlementPhaseSeen := false
	for rows.Next() {
		var fact CityPhysicalNetworkFact
		var commandSequence sql.NullInt64
		if err = rows.Scan(
			&fact.Tick, &fact.Sequence, &fact.Phase, &commandSequence,
			&fact.FactType, &fact.SubjectKind, &fact.SubjectCode,
			&fact.VersionBefore, &fact.VersionAfter, &fact.Payload,
		); err != nil {
			return nil, fmt.Errorf("scan replay physical-network fact: %w", err)
		}
		if fact.Tick != tick || fact.Sequence != expectedSequence || !json.Valid(fact.Payload) {
			return nil, fmt.Errorf("physical-network fact sequence is not contiguous")
		}
		switch fact.Phase {
		case "command":
			if preNetworkPhaseSeen || settlementPhaseSeen || !commandSequence.Valid {
				return nil, fmt.Errorf("physical-network command fact phase is invalid")
			}
		case CityPhysicalNetworkPhasePreNetwork:
			if settlementPhaseSeen || commandSequence.Valid {
				return nil, fmt.Errorf("physical-network pre-network fact follows settlement")
			}
			preNetworkPhaseSeen = true
		case CityPhysicalNetworkPhaseSettlement:
			if commandSequence.Valid {
				return nil, fmt.Errorf("physical-network settlement fact has command source")
			}
			settlementPhaseSeen = true
		default:
			return nil, fmt.Errorf("unsupported physical-network fact phase %q", fact.Phase)
		}
		if commandSequence.Valid {
			fact.SourceCommandSequence = int64Pointer(commandSequence.Int64)
		}
		facts = append(facts, fact)
		expectedSequence++
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replay physical-network facts: %w", err)
	}
	return facts, nil
}

func reduceCityPhysicalNetworkFact(state *cityHashState, fact CityPhysicalNetworkFact) error {
	if state == nil || state.PhysicalNetworks == nil || fact.Tick <= 0 ||
		fact.Sequence <= 0 || !json.Valid(fact.Payload) ||
		state.PhysicalNetworks.Profile.Revision != state.PhysicalNetworks.Profile.FactCount+1 {
		return fmt.Errorf("physical-network fact or profile is invalid")
	}
	switch fact.FactType {
	case CityPhysicalNetworkFactNetworkConfigured:
		if err := reduceCityPhysicalNetworkConfiguredFact(state, fact); err != nil {
			return err
		}
	case CityPhysicalNetworkFactNodeConfigured:
		if err := reduceCityPhysicalNetworkNodeConfiguredFact(state, fact); err != nil {
			return err
		}
	case CityPhysicalNetworkFactEdgeConfigured, CityPhysicalNetworkFactEdgeStateChanged:
		if err := reduceCityPhysicalNetworkEdgeConfiguredFact(state, fact); err != nil {
			return err
		}
	case CityPhysicalNetworkFactTopologySynchronized:
		if err := reduceCityPhysicalNetworkTopologyFact(state.PhysicalNetworks, fact); err != nil {
			return err
		}
	case CityPhysicalNetworkFactFlowSettled:
		if err := reduceCityPhysicalNetworkFlowFact(state, fact); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported physical-network fact type %q", fact.FactType)
	}
	state.PhysicalNetworks.Facts = append(state.PhysicalNetworks.Facts, fact)
	state.PhysicalNetworks.Profile.FactCount++
	state.PhysicalNetworks.Profile.Revision++
	return nil
}

func reduceCityPhysicalNetworkConfiguredFact(
	state *cityHashState, fact CityPhysicalNetworkFact,
) error {
	var payload cityPhysicalNetworkConfigureFactPayload
	if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
		return fmt.Errorf("decode configured physical-network fact: %w", err)
	}
	after := payload.NetworkAfter
	if payload.SchemaVersion != cityPhysicalNetworkSchemaVersion || fact.Phase != "command" ||
		fact.SourceCommandSequence == nil || fact.SubjectKind != "network" ||
		fact.SubjectCode != after.Code || fact.VersionAfter != fact.VersionBefore+1 ||
		after.Version != fact.VersionAfter || after.SourceFactTick != fact.Tick ||
		after.SourceFactSequence != fact.Sequence || after.UpdatedTick != fact.Tick ||
		!cityServiceCodePattern.MatchString(after.Code) ||
		cityPhysicalNetworkPolicyForService(state.PhysicalNetworks.Policies, after.ServiceCode) == nil ||
		!isCityPhysicalNetworkStatus(after.Status) || after.TopologyRevision <= 0 ||
		after.CreatedTick < 0 || after.CreatedTick > after.UpdatedTick ||
		!json.Valid(after.Metadata) || !cityPhysicalNetworkIsExplicit(after) {
		return fmt.Errorf("configured physical-network fact identity is invalid")
	}
	index := cityPhysicalNetworkIndex(state.PhysicalNetworks.Networks, after.Code)
	if payload.NetworkBefore == nil {
		if fact.VersionBefore != 0 || after.Version != 1 || after.TopologyRevision != 1 ||
			after.CreatedTick != fact.Tick || index >= 0 || after.Status == CityNetworkStatusRetired {
			return fmt.Errorf("configured physical-network creation chain is invalid")
		}
		for _, existing := range state.PhysicalNetworks.Networks {
			if existing.ServiceCode == after.ServiceCode && existing.Status != CityNetworkStatusRetired {
				return fmt.Errorf("configured physical-network live service is duplicated")
			}
		}
		state.PhysicalNetworks.Networks = append(state.PhysicalNetworks.Networks, after)
		state.PhysicalNetworks.Profile.NetworkCount++
		return nil
	}
	before := *payload.NetworkBefore
	if index < 0 || !cityPhysicalNetworkSnapshotEqual(state.PhysicalNetworks.Networks[index], before) ||
		fact.VersionBefore != before.Version || after.Code != before.Code ||
		after.ServiceCode != before.ServiceCode || after.CreatedTick != before.CreatedTick ||
		after.TopologyRevision != before.TopologyRevision+1 || after.Version != before.Version+1 ||
		!isCityPhysicalNetworkTransition(before.Status, after.Status) {
		return fmt.Errorf("configured physical-network update chain is invalid")
	}
	if after.Status == CityNetworkStatusRetired {
		for _, node := range state.PhysicalNetworks.Nodes {
			if node.NetworkCode == after.Code && node.Status != CityNetworkNodeStatusRetired {
				return fmt.Errorf("retired physical network retains a live node")
			}
		}
		for _, edge := range state.PhysicalNetworks.Edges {
			if edge.NetworkCode == after.Code && edge.Status != CityNetworkEdgeStatusRetired {
				return fmt.Errorf("retired physical network retains a live edge")
			}
		}
	}
	state.PhysicalNetworks.Networks[index] = after
	return nil
}

func reduceCityPhysicalNetworkNodeConfiguredFact(
	state *cityHashState, fact CityPhysicalNetworkFact,
) error {
	var payload cityPhysicalNetworkNodeConfigureFactPayload
	if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
		return fmt.Errorf("decode configured physical-network node fact: %w", err)
	}
	physical := state.PhysicalNetworks
	after := payload.NodeAfter
	networkIndex := cityPhysicalNetworkIndex(physical.Networks, after.NetworkCode)
	if payload.SchemaVersion != cityPhysicalNetworkSchemaVersion || fact.Phase != "command" ||
		fact.SourceCommandSequence == nil || fact.SubjectKind != "node" ||
		fact.SubjectCode != after.Code || fact.VersionAfter != fact.VersionBefore+1 ||
		after.Version != fact.VersionAfter || after.SourceFactTick != fact.Tick ||
		after.SourceFactSequence != fact.Sequence || after.UpdatedTick != fact.Tick ||
		networkIndex < 0 || !cityPhysicalNetworkSnapshotEqual(
		physical.Networks[networkIndex], payload.NetworkBefore,
	) || !cityPhysicalNetworkCommandTopologyBumpValid(
		payload.NetworkBefore, payload.NetworkAfter, fact,
	) || payload.NetworkAfter.Code != after.NetworkCode ||
		!validateCityPhysicalNetworkCommandNode(state, payload.NetworkAfter, after) {
		return fmt.Errorf("configured physical-network node identity is invalid")
	}
	nodeIndex := cityPhysicalNetworkNodeIndex(physical.Nodes, after.NetworkCode, after.Code)
	if payload.NodeBefore == nil {
		if fact.VersionBefore != 0 || after.Version != 1 || after.CreatedTick != fact.Tick || nodeIndex >= 0 {
			return fmt.Errorf("configured physical-network node creation chain is invalid")
		}
		physical.Nodes = append(physical.Nodes, after)
		physical.Profile.NodeCount++
	} else {
		before := *payload.NodeBefore
		if nodeIndex < 0 || !cityPhysicalNetworkNodeSnapshotEqual(physical.Nodes[nodeIndex], before) ||
			fact.VersionBefore != before.Version || after.Code != before.Code ||
			after.NetworkCode != before.NetworkCode || after.CreatedTick != before.CreatedTick ||
			after.Version != before.Version+1 || !isCityPhysicalNodeTransition(before.Status, after.Status) {
			return fmt.Errorf("configured physical-network node update chain is invalid")
		}
		if before.Status == CityNetworkNodeStatusActive &&
			(before.Role != after.Role ||
				!sameOptionalString(before.CapacityCode, after.CapacityCode) ||
				!sameOptionalString(before.DemandCode, after.DemandCode)) {
			for _, edge := range physical.Edges {
				if edge.NetworkCode == before.NetworkCode && edge.Status == CityNetworkEdgeStatusActive &&
					(edge.FromNodeCode == before.Code || edge.ToNodeCode == before.Code) {
					return fmt.Errorf("configured physical-network node changed a live edge binding")
				}
			}
		}
		physical.Nodes[nodeIndex] = after
	}
	physical.Networks[networkIndex] = payload.NetworkAfter
	return nil
}

func reduceCityPhysicalNetworkEdgeConfiguredFact(
	state *cityHashState, fact CityPhysicalNetworkFact,
) error {
	var payload cityPhysicalNetworkEdgeConfigureFactPayload
	if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
		return fmt.Errorf("decode configured physical-network edge fact: %w", err)
	}
	physical := state.PhysicalNetworks
	after := payload.EdgeAfter
	networkIndex := cityPhysicalNetworkIndex(physical.Networks, after.NetworkCode)
	if payload.SchemaVersion != cityPhysicalNetworkSchemaVersion || fact.Phase != "command" ||
		fact.SourceCommandSequence == nil || fact.SubjectKind != "edge" ||
		fact.SubjectCode != after.Code || fact.VersionAfter != fact.VersionBefore+1 ||
		after.Version != fact.VersionAfter || after.SourceFactTick != fact.Tick ||
		after.SourceFactSequence != fact.Sequence || after.UpdatedTick != fact.Tick ||
		networkIndex < 0 || !cityPhysicalNetworkSnapshotEqual(
		physical.Networks[networkIndex], payload.NetworkBefore,
	) || !cityPhysicalNetworkCommandTopologyBumpValid(
		payload.NetworkBefore, payload.NetworkAfter, fact,
	) || payload.NetworkAfter.Code != after.NetworkCode ||
		!validateCityPhysicalNetworkCommandEdge(physical, payload.NetworkAfter, after) {
		return fmt.Errorf("configured physical-network edge identity is invalid")
	}
	edgeIndex := cityPhysicalNetworkEdgeIndex(physical.Edges, after.NetworkCode, after.Code)
	if payload.EdgeBefore == nil {
		if fact.FactType != CityPhysicalNetworkFactEdgeConfigured || fact.VersionBefore != 0 ||
			after.Version != 1 || after.CreatedTick != fact.Tick || edgeIndex >= 0 ||
			after.Status == CityNetworkEdgeStatusFailed || after.Status == CityNetworkEdgeStatusRetired {
			return fmt.Errorf("configured physical-network edge creation chain is invalid")
		}
		physical.Edges = append(physical.Edges, after)
		physical.Profile.EdgeCount++
	} else {
		before := *payload.EdgeBefore
		if edgeIndex < 0 || !cityPhysicalNetworkEdgeSnapshotEqual(physical.Edges[edgeIndex], before) ||
			fact.VersionBefore != before.Version || after.Code != before.Code ||
			after.NetworkCode != before.NetworkCode || after.CreatedTick != before.CreatedTick ||
			after.Version != before.Version+1 {
			return fmt.Errorf("configured physical-network edge update chain is invalid")
		}
		if fact.FactType == CityPhysicalNetworkFactEdgeConfigured {
			if after.Status != before.Status || before.Status == CityNetworkEdgeStatusRetired {
				return fmt.Errorf("configured physical-network edge changed state")
			}
			if before.Status == CityNetworkEdgeStatusActive &&
				(before.FromNodeCode != after.FromNodeCode || before.ToNodeCode != after.ToNodeCode ||
					before.Direction != after.Direction) {
				return fmt.Errorf("configured active physical-network edge changed topology")
			}
		} else if fact.FactType == CityPhysicalNetworkFactEdgeStateChanged {
			if !isCityPhysicalEdgeTransition(before.Status, after.Status) ||
				!cityPhysicalNetworkEdgeTransitionSnapshotValid(before, after) {
				return fmt.Errorf("physical-network edge transition snapshot is invalid")
			}
		} else {
			return fmt.Errorf("physical-network edge fact type is invalid")
		}
		physical.Edges[edgeIndex] = after
	}
	physical.Networks[networkIndex] = payload.NetworkAfter
	return nil
}

func cityPhysicalNetworkCommandTopologyBumpValid(
	before, after CityPhysicalNetwork, fact CityPhysicalNetworkFact,
) bool {
	expected := before
	expected.TopologyRevision++
	expected.UpdatedTick = fact.Tick
	expected.Version++
	expected.SourceFactTick = fact.Tick
	expected.SourceFactSequence = fact.Sequence
	return cityPhysicalNetworkSnapshotEqual(expected, after)
}

func validateCityPhysicalNetworkCommandNode(
	state *cityHashState, network CityPhysicalNetwork, node CityPhysicalNetworkNode,
) bool {
	if !cityServiceCodePattern.MatchString(node.Code) || node.NetworkCode != network.Code ||
		!isCityPhysicalNetworkNodeStatus(node.Status) || node.CreatedTick < 0 ||
		node.Version <= 0 || !json.Valid(node.Metadata) ||
		(node.WorldX == nil || node.WorldY == nil || node.WorldZ == nil) &&
			(node.WorldX != nil || node.WorldY != nil || node.WorldZ != nil) ||
		node.Status == CityNetworkNodeStatusActive && network.Status != CityNetworkStatusActive {
		return false
	}
	switch node.Role {
	case CityNetworkNodeRoleSupply:
		if node.CapacityCode == nil || node.DemandCode != nil ||
			!cityPhysicalNetworkCapacityMatchesService(state.PublicServices, *node.CapacityCode, network.ServiceCode) {
			return false
		}
	case CityNetworkNodeRoleDemand:
		if node.DemandCode == nil || node.CapacityCode != nil {
			return false
		}
		index := cityServiceDemandIndex(state.PublicServices.Demands, *node.DemandCode)
		if index < 0 || state.PublicServices.Demands[index].ServiceCode != network.ServiceCode {
			return false
		}
	case CityNetworkNodeRoleJunction, CityNetworkNodeRoleStorage, CityNetworkNodeRoleGateway:
		if node.CapacityCode != nil || node.DemandCode != nil {
			return false
		}
	default:
		return false
	}
	for _, existing := range state.PhysicalNetworks.Nodes {
		if existing.NetworkCode != node.NetworkCode || existing.Code == node.Code ||
			existing.Status != CityNetworkNodeStatusActive || node.Status != CityNetworkNodeStatusActive {
			continue
		}
		if sameOptionalString(existing.CapacityCode, node.CapacityCode) && node.CapacityCode != nil ||
			sameOptionalString(existing.DemandCode, node.DemandCode) && node.DemandCode != nil {
			return false
		}
	}
	if node.Status != CityNetworkNodeStatusActive {
		for _, edge := range state.PhysicalNetworks.Edges {
			if edge.NetworkCode == node.NetworkCode && edge.Status == CityNetworkEdgeStatusActive &&
				(edge.FromNodeCode == node.Code || edge.ToNodeCode == node.Code) {
				return false
			}
		}
	}
	return true
}

func validateCityPhysicalNetworkCommandEdge(
	state *cityPhysicalNetworkHashState, network CityPhysicalNetwork,
	edge CityPhysicalNetworkEdge,
) bool {
	fromIndex := cityPhysicalNetworkNodeIndex(state.Nodes, edge.NetworkCode, edge.FromNodeCode)
	toIndex := cityPhysicalNetworkNodeIndex(state.Nodes, edge.NetworkCode, edge.ToNodeCode)
	policy := cityPhysicalNetworkPolicyForService(state.Policies, network.ServiceCode)
	if !cityServiceCodePattern.MatchString(edge.Code) || edge.NetworkCode != network.Code ||
		edge.FromNodeCode == edge.ToNodeCode || fromIndex < 0 || toIndex < 0 ||
		!isCityPhysicalNetworkEdgeDirection(edge.Direction) ||
		edge.Direction == CityNetworkEdgeDirectionBidirectional && (policy == nil || !policy.AllowBidirectional) ||
		edge.InstalledCapacityUnits <= 0 || edge.AvailabilityMilli < 0 || edge.AvailabilityMilli > 1000 ||
		edge.AvailableCapacityUnits != edge.InstalledCapacityUnits*int64(edge.AvailabilityMilli)/1000 ||
		edge.LossMilli < 0 || edge.LossMilli > 999 || edge.BaseCostUnits <= 0 ||
		!isCityPhysicalNetworkEdgeStatus(edge.Status) || edge.ConditionMilli < 0 ||
		edge.ConditionMilli > 1000 || edge.FailureCount < 0 || edge.CreatedTick < 0 ||
		edge.Version <= 0 || !json.Valid(edge.Metadata) {
		return false
	}
	if edge.Status == CityNetworkEdgeStatusActive &&
		(network.Status != CityNetworkStatusActive ||
			state.Nodes[fromIndex].Status != CityNetworkNodeStatusActive ||
			state.Nodes[toIndex].Status != CityNetworkNodeStatusActive ||
			edge.AvailableCapacityUnits <= 0 || edge.ConditionMilli <= 0) {
		return false
	}
	return true
}

func cityPhysicalNetworkCapacityMatchesService(
	state *cityPublicServiceHashState, capacityCode, serviceCode string,
) bool {
	if state == nil {
		return false
	}
	for _, capacity := range state.Capacities {
		if capacity.FacilityCode+"."+capacity.ServiceCode == capacityCode &&
			capacity.ServiceCode == serviceCode {
			return true
		}
	}
	return false
}

func cityPhysicalNetworkEdgeTransitionSnapshotValid(
	before, after CityPhysicalNetworkEdge,
) bool {
	expected := before
	expected.Status = after.Status
	expected.UpdatedTick = after.UpdatedTick
	expected.Version = after.Version
	expected.SourceFactTick = after.SourceFactTick
	expected.SourceFactSequence = after.SourceFactSequence
	expected.Metadata = after.Metadata
	if after.Status == CityNetworkEdgeStatusFailed {
		expected.ConditionMilli = 0
		expected.FailureCount++
	}
	return cityPhysicalNetworkEdgeSnapshotEqual(expected, after)
}

func reduceCityPhysicalNetworkTopologyFact(
	state *cityPhysicalNetworkHashState,
	fact CityPhysicalNetworkFact,
) error {
	var payload cityPhysicalNetworkTopologyFactPayload
	if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
		return fmt.Errorf("decode physical-network topology payload: %w", err)
	}
	if payload.SchemaVersion != cityPhysicalNetworkSchemaVersion || payload.Mode != "legacy_direct" ||
		fact.SourceCommandSequence != nil || fact.Phase != CityPhysicalNetworkPhasePreNetwork ||
		fact.SubjectKind != "network" || fact.SubjectCode != payload.NetworkAfter.Code ||
		payload.NetworkAfter.ServiceCode != payload.ServiceCode ||
		fact.VersionAfter != fact.VersionBefore+1 ||
		payload.NetworkAfter.Version != fact.VersionAfter ||
		payload.NetworkAfter.SourceFactTick != fact.Tick ||
		payload.NetworkAfter.SourceFactSequence != fact.Sequence ||
		len(payload.BeforeProjectionHash) != 64 || len(payload.AfterProjectionHash) != 64 ||
		!json.Valid(payload.NetworkAfter.Metadata) {
		return fmt.Errorf("physical-network topology fact identity is invalid")
	}
	before, err := cityPhysicalNetworkTopologyProjectionFromState(state, fact.SubjectCode)
	if err != nil {
		return err
	}
	beforeHash, err := cityPhysicalNetworkTopologyProjectionHash(before)
	if err != nil || beforeHash != payload.BeforeProjectionHash {
		return fmt.Errorf("physical-network topology before hash is invalid")
	}
	networkIndex := cityPhysicalNetworkIndex(state.Networks, fact.SubjectCode)
	if fact.VersionBefore == 0 {
		if networkIndex >= 0 || payload.NetworkBefore != nil || payload.NetworkAfter.Version != 1 ||
			payload.NetworkAfter.CreatedTick != fact.Tick {
			return fmt.Errorf("physical-network topology creation chain is invalid")
		}
		state.Networks = append(state.Networks, payload.NetworkAfter)
		state.Profile.NetworkCount++
	} else {
		if networkIndex < 0 || payload.NetworkBefore == nil ||
			!cityPhysicalNetworkSnapshotEqual(state.Networks[networkIndex], *payload.NetworkBefore) ||
			state.Networks[networkIndex].Version != fact.VersionBefore ||
			state.Networks[networkIndex].CreatedTick != payload.NetworkAfter.CreatedTick ||
			payload.NetworkAfter.TopologyRevision != state.Networks[networkIndex].TopologyRevision+1 {
			return fmt.Errorf("physical-network topology update chain is invalid")
		}
		state.Networks[networkIndex] = payload.NetworkAfter
	}
	nodeBefore := make(map[string]CityPhysicalNetworkNode, len(payload.NodeBefore))
	for _, node := range payload.NodeBefore {
		if _, duplicate := nodeBefore[node.Code]; duplicate || node.NetworkCode != fact.SubjectCode {
			return fmt.Errorf("physical-network topology node before snapshot is invalid")
		}
		nodeBefore[node.Code] = node
	}
	seenNodes := make(map[string]struct{}, len(payload.NodeUpserts))
	for _, node := range payload.NodeUpserts {
		if _, duplicate := seenNodes[node.Code]; duplicate {
			return fmt.Errorf("physical-network topology node upsert is duplicated")
		}
		seenNodes[node.Code] = struct{}{}
		if err = validateCityPhysicalNetworkNodeSnapshot(node, fact); err != nil {
			return err
		}
		index := cityPhysicalNetworkNodeIndex(state.Nodes, node.NetworkCode, node.Code)
		if index < 0 {
			if _, hasBefore := nodeBefore[node.Code]; hasBefore ||
				node.Version != 1 || node.CreatedTick != fact.Tick {
				return fmt.Errorf("physical-network node creation chain is invalid")
			}
			state.Nodes = append(state.Nodes, node)
			state.Profile.NodeCount++
		} else {
			beforeNode, hasBefore := nodeBefore[node.Code]
			if !hasBefore || !cityPhysicalNetworkNodeSnapshotEqual(state.Nodes[index], beforeNode) ||
				node.Version != state.Nodes[index].Version+1 ||
				node.CreatedTick != state.Nodes[index].CreatedTick {
				return fmt.Errorf("physical-network node update chain is invalid")
			}
			state.Nodes[index] = node
		}
	}
	for code := range nodeBefore {
		if _, exists := seenNodes[code]; !exists {
			return fmt.Errorf("physical-network topology node before snapshot is unused")
		}
	}
	edgeBefore := make(map[string]CityPhysicalNetworkEdge, len(payload.EdgeBefore))
	for _, edge := range payload.EdgeBefore {
		if _, duplicate := edgeBefore[edge.Code]; duplicate || edge.NetworkCode != fact.SubjectCode {
			return fmt.Errorf("physical-network topology edge before snapshot is invalid")
		}
		edgeBefore[edge.Code] = edge
	}
	seenEdges := make(map[string]struct{}, len(payload.EdgeUpserts))
	for _, edge := range payload.EdgeUpserts {
		if _, duplicate := seenEdges[edge.Code]; duplicate {
			return fmt.Errorf("physical-network topology edge upsert is duplicated")
		}
		seenEdges[edge.Code] = struct{}{}
		if err = validateCityPhysicalNetworkEdgeSnapshot(state, edge, fact); err != nil {
			return err
		}
		index := cityPhysicalNetworkEdgeIndex(state.Edges, edge.NetworkCode, edge.Code)
		if index < 0 {
			if _, hasBefore := edgeBefore[edge.Code]; hasBefore ||
				edge.Version != 1 || edge.CreatedTick != fact.Tick {
				return fmt.Errorf("physical-network edge creation chain is invalid")
			}
			state.Edges = append(state.Edges, edge)
			state.Profile.EdgeCount++
		} else {
			beforeEdge, hasBefore := edgeBefore[edge.Code]
			if !hasBefore || !cityPhysicalNetworkEdgeSnapshotEqual(state.Edges[index], beforeEdge) ||
				edge.Version != state.Edges[index].Version+1 ||
				edge.CreatedTick != state.Edges[index].CreatedTick {
				return fmt.Errorf("physical-network edge update chain is invalid")
			}
			state.Edges[index] = edge
		}
	}
	for code := range edgeBefore {
		if _, exists := seenEdges[code]; !exists {
			return fmt.Errorf("physical-network topology edge before snapshot is unused")
		}
	}
	after, err := cityPhysicalNetworkTopologyProjectionFromState(state, fact.SubjectCode)
	if err != nil {
		return err
	}
	afterHash, err := cityPhysicalNetworkTopologyProjectionHash(after)
	if err != nil || afterHash != payload.AfterProjectionHash {
		return fmt.Errorf("physical-network topology after hash is invalid")
	}
	return nil
}

func validateCityPhysicalNetworkNodeSnapshot(
	node CityPhysicalNetworkNode,
	fact CityPhysicalNetworkFact,
) error {
	if node.NetworkCode != fact.SubjectCode || !cityServiceCodePattern.MatchString(node.Code) ||
		node.SourceFactTick != fact.Tick || node.SourceFactSequence != fact.Sequence ||
		node.UpdatedTick != fact.Tick || node.CreatedTick < 0 || node.Version <= 0 ||
		!json.Valid(node.Metadata) || cityPhysicalNetworkNodeStatusRank(node.Status) == 0 {
		return fmt.Errorf("physical-network node snapshot is invalid")
	}
	switch node.Role {
	case CityNetworkNodeRoleSupply:
		if node.CapacityCode == nil || node.DemandCode != nil {
			return fmt.Errorf("physical-network supply binding is invalid")
		}
	case CityNetworkNodeRoleDemand:
		if node.DemandCode == nil || node.CapacityCode != nil {
			return fmt.Errorf("physical-network demand binding is invalid")
		}
	case CityNetworkNodeRoleJunction, CityNetworkNodeRoleStorage, CityNetworkNodeRoleGateway:
	default:
		return fmt.Errorf("physical-network node role is invalid")
	}
	return nil
}

func validateCityPhysicalNetworkEdgeSnapshot(
	state *cityPhysicalNetworkHashState,
	edge CityPhysicalNetworkEdge,
	fact CityPhysicalNetworkFact,
) error {
	if edge.NetworkCode != fact.SubjectCode || !cityServiceCodePattern.MatchString(edge.Code) ||
		edge.FromNodeCode == edge.ToNodeCode || edge.SourceFactTick != fact.Tick ||
		edge.SourceFactSequence != fact.Sequence || edge.UpdatedTick != fact.Tick ||
		edge.CreatedTick < 0 || edge.Version <= 0 || edge.InstalledCapacityUnits <= 0 ||
		edge.AvailabilityMilli < 0 || edge.AvailabilityMilli > 1000 ||
		edge.AvailableCapacityUnits != edge.InstalledCapacityUnits*int64(edge.AvailabilityMilli)/1000 ||
		edge.LossMilli < 0 || edge.LossMilli > 999 || edge.BaseCostUnits <= 0 ||
		edge.ConditionMilli < 0 || edge.ConditionMilli > 1000 || edge.FailureCount < 0 ||
		!json.Valid(edge.Metadata) ||
		(edge.Direction != CityNetworkEdgeDirectionDirected &&
			edge.Direction != CityNetworkEdgeDirectionBidirectional) ||
		(edge.Status != CityNetworkEdgeStatusActive &&
			edge.Status != CityNetworkEdgeStatusIsolated &&
			edge.Status != CityNetworkEdgeStatusFailed &&
			edge.Status != CityNetworkEdgeStatusRetired) ||
		cityPhysicalNetworkNodeIndex(state.Nodes, edge.NetworkCode, edge.FromNodeCode) < 0 ||
		cityPhysicalNetworkNodeIndex(state.Nodes, edge.NetworkCode, edge.ToNodeCode) < 0 {
		return fmt.Errorf("physical-network edge snapshot is invalid")
	}
	return nil
}

func reduceCityPhysicalNetworkFlowFact(state *cityHashState, fact CityPhysicalNetworkFact) error {
	var payload cityPhysicalNetworkFlowFactPayload
	if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
		return fmt.Errorf("decode physical-network flow payload: %w", err)
	}
	batch := payload.Batch
	if payload.SchemaVersion != cityPhysicalNetworkSchemaVersion ||
		fact.SourceCommandSequence != nil || fact.Phase != CityPhysicalNetworkPhaseSettlement ||
		fact.SubjectKind != "flow_batch" || fact.SubjectCode != batch.NetworkCode ||
		fact.VersionBefore != fact.Tick-1 || fact.VersionAfter != fact.Tick ||
		batch.Tick != fact.Tick || batch.Sequence != fact.Sequence ||
		batch.SourceFactTick != fact.Tick || batch.SourceFactSequence != fact.Sequence ||
		batch.AllocationCount <= 0 || batch.PathCount != len(payload.Paths) ||
		batch.SegmentCount != len(payload.Segments) || !json.Valid(batch.Metadata) ||
		batch.DispatchedUnits <= 0 || batch.NetworkReceivedUnits <= 0 ||
		batch.NetworkReceivedUnits > batch.DispatchedUnits ||
		batch.NetworkLossUnits != batch.DispatchedUnits-batch.NetworkReceivedUnits {
		return fmt.Errorf("physical-network flow fact identity is invalid")
	}
	physical := state.PhysicalNetworks
	networkIndex := cityPhysicalNetworkIndex(physical.Networks, batch.NetworkCode)
	if networkIndex < 0 || physical.Networks[networkIndex].ServiceCode != batch.ServiceCode ||
		physical.Networks[networkIndex].TopologyRevision != batch.TopologyRevision {
		return fmt.Errorf("physical-network flow topology snapshot is invalid")
	}
	for _, existing := range physical.Batches {
		if existing.Tick == batch.Tick && existing.NetworkCode == batch.NetworkCode {
			return fmt.Errorf("physical-network flow batch is duplicated")
		}
	}
	if err := validateCityPhysicalNetworkFlowPayload(state, payload); err != nil {
		return err
	}
	physical.Batches = append(physical.Batches, batch)
	physical.Paths = append(physical.Paths, payload.Paths...)
	physical.Segments = append(physical.Segments, payload.Segments...)
	physical.Profile.BatchCount++
	physical.Profile.PathCount += int64(len(payload.Paths))
	physical.Profile.SegmentCount += int64(len(payload.Segments))
	return nil
}

type cityPhysicalNetworkAllocationReplayKey struct {
	serviceSequence int64
	allocationIndex int
}

func validateCityPhysicalNetworkFlowPayload(
	state *cityHashState,
	payload cityPhysicalNetworkFlowFactPayload,
) error {
	physical := state.PhysicalNetworks
	batch := payload.Batch
	policy := cityPhysicalNetworkPolicyForService(physical.Policies, batch.ServiceCode)
	if policy == nil {
		return fmt.Errorf("physical-network flow policy is unavailable")
	}
	segmentsByPath := make(map[[3]int64][]CityPhysicalNetworkFlowSegment)
	for _, segment := range payload.Segments {
		key := [3]int64{segment.ServiceSequence, int64(segment.AllocationIndex), int64(segment.PathIndex)}
		segmentsByPath[key] = append(segmentsByPath[key], segment)
	}
	edgeUsage := make(map[string]int64)
	allocationDispatch := make(map[cityPhysicalNetworkAllocationReplayKey]int64)
	allocationReceived := make(map[cityPhysicalNetworkAllocationReplayKey]int64)
	allocationPathCount := make(map[cityPhysicalNetworkAllocationReplayKey]int)
	seenPaths := make(map[[3]int64]struct{}, len(payload.Paths))
	var batchDispatched, batchReceived int64
	for _, path := range payload.Paths {
		pathKey := [3]int64{path.ServiceSequence, int64(path.AllocationIndex), int64(path.PathIndex)}
		if _, duplicate := seenPaths[pathKey]; duplicate {
			return fmt.Errorf("physical-network flow path is duplicated")
		}
		seenPaths[pathKey] = struct{}{}
		allocation := cityServiceAllocationForPhysicalPath(
			state.PublicServices.Allocations, path.Tick, path.ServiceSequence, path.AllocationIndex,
		)
		if allocation == nil || allocation.ServiceCode != batch.ServiceCode ||
			allocation.ConnectionCode != path.ConnectionCode || path.Sequence != batch.Sequence ||
			path.Tick != batch.Tick || path.NetworkCode != batch.NetworkCode ||
			path.PathIndex <= 0 || path.DispatchedUnits <= 0 ||
			path.NetworkReceivedUnits <= 0 || path.NetworkReceivedUnits > path.DispatchedUnits ||
			path.NetworkLossUnits != path.DispatchedUnits-path.NetworkReceivedUnits ||
			path.HopCount <= 0 || len(path.PathHash) != 64 || !json.Valid(path.Metadata) {
			return fmt.Errorf("physical-network flow path identity is invalid")
		}
		segments := segmentsByPath[pathKey]
		if len(segments) != path.HopCount {
			return fmt.Errorf("physical-network flow path segment count is invalid")
		}
		sort.Slice(segments, func(i, j int) bool { return segments[i].SegmentIndex < segments[j].SegmentIndex })
		planSegments := make([]cityNetworkSegmentPlan, 0, len(segments))
		var pathCost int64
		for index, segment := range segments {
			if segment.Tick != batch.Tick || segment.Sequence != batch.Sequence ||
				segment.ServiceSequence != path.ServiceSequence ||
				segment.AllocationIndex != path.AllocationIndex ||
				segment.PathIndex != path.PathIndex || segment.SegmentIndex != index+1 ||
				!json.Valid(segment.Metadata) {
				return fmt.Errorf("physical-network flow segment identity is invalid")
			}
			edgeIndex := cityPhysicalNetworkEdgeIndex(physical.Edges, batch.NetworkCode, segment.EdgeCode)
			if edgeIndex < 0 {
				return fmt.Errorf("physical-network flow segment edge is unknown")
			}
			edge := physical.Edges[edgeIndex]
			if edge.Version != segment.EdgeVersion ||
				edge.AvailableCapacityUnits != segment.EdgeCapacityUnits ||
				edge.LossMilli != segment.LossMilli || segment.InputUnits <= 0 ||
				segment.OutputUnits != segment.InputUnits*int64(1000-segment.LossMilli)/1000 ||
				segment.LossUnits != segment.InputUnits-segment.OutputUnits ||
				!cityPhysicalNetworkSegmentDirectionMatches(edge, segment) {
				return fmt.Errorf("physical-network flow segment snapshot is invalid")
			}
			if index == 0 {
				if segment.FromNodeCode != path.SourceNodeCode || segment.InputUnits != path.DispatchedUnits {
					return fmt.Errorf("physical-network flow path source is invalid")
				}
			} else if segments[index-1].ToNodeCode != segment.FromNodeCode ||
				segments[index-1].OutputUnits != segment.InputUnits {
				return fmt.Errorf("physical-network flow path continuity is invalid")
			}
			if index == len(segments)-1 &&
				(segment.ToNodeCode != path.SinkNodeCode || segment.OutputUnits != path.NetworkReceivedUnits) {
				return fmt.Errorf("physical-network flow path sink is invalid")
			}
			if int64(segment.LossMilli) > 0 && policy.LossCostWeight > math.MaxInt64/int64(segment.LossMilli) {
				return fmt.Errorf("physical-network flow path cost overflows")
			}
			segmentCost := edge.BaseCostUnits + policy.LossCostWeight*int64(segment.LossMilli)
			if segmentCost < edge.BaseCostUnits || pathCost > math.MaxInt64-segmentCost {
				return fmt.Errorf("physical-network flow path cost overflows")
			}
			pathCost += segmentCost
			edgeUsage[edge.Code] += segment.InputUnits
			if edgeUsage[edge.Code] > edge.AvailableCapacityUnits {
				return fmt.Errorf("physical-network flow exceeds edge capacity")
			}
			planSegments = append(planSegments, cityNetworkSegmentPlan{
				SegmentIndex: segment.SegmentIndex, EdgeCode: segment.EdgeCode,
				EdgeVersion: segment.EdgeVersion, Direction: segment.Direction,
				FromNodeCode: segment.FromNodeCode, ToNodeCode: segment.ToNodeCode,
				EdgeCapacityUnits: segment.EdgeCapacityUnits, LossMilli: segment.LossMilli,
				InputUnits: segment.InputUnits, OutputUnits: segment.OutputUnits,
				LossUnits: segment.LossUnits,
			})
		}
		if path.PathCostUnits != pathCost {
			return fmt.Errorf("physical-network flow path cost is invalid")
		}
		plan := cityNetworkPathPlan{
			PathIndex: path.PathIndex, SourceNodeCode: path.SourceNodeCode,
			SinkNodeCode: path.SinkNodeCode, DispatchedUnits: path.DispatchedUnits,
			NetworkReceivedUnits: path.NetworkReceivedUnits,
			NetworkLossUnits:     path.NetworkLossUnits, PathCostUnits: path.PathCostUnits,
			Segments: planSegments,
		}
		hash, err := cityNetworkPathPlanHash(path.ConnectionCode, plan)
		if err != nil || hash != path.PathHash {
			return fmt.Errorf("physical-network flow path hash is invalid")
		}
		if err = validateCityPhysicalNetworkPathEndpoints(physical, *policy, *allocation, path); err != nil {
			return err
		}
		allocationKey := cityPhysicalNetworkAllocationReplayKey{
			serviceSequence: path.ServiceSequence, allocationIndex: path.AllocationIndex,
		}
		allocationDispatch[allocationKey] += path.DispatchedUnits
		allocationReceived[allocationKey] += path.NetworkReceivedUnits
		allocationPathCount[allocationKey]++
		batchDispatched += path.DispatchedUnits
		batchReceived += path.NetworkReceivedUnits
	}
	if len(seenPaths) != len(segmentsByPath) || len(allocationPathCount) != batch.AllocationCount ||
		batchDispatched != batch.DispatchedUnits || batchReceived != batch.NetworkReceivedUnits {
		return fmt.Errorf("physical-network flow batch totals are invalid")
	}
	for key, pathCount := range allocationPathCount {
		allocation := cityServiceAllocationForPhysicalPath(
			state.PublicServices.Allocations, batch.Tick, key.serviceSequence, key.allocationIndex,
		)
		if allocation == nil || allocation.NetworkPathCount == nil ||
			allocation.NetworkReceivedUnits == nil || allocation.NetworkLossUnits == nil ||
			*allocation.NetworkPathCount != pathCount ||
			allocationDispatch[key] != allocation.DispatchedUnits ||
			allocationReceived[key] != *allocation.NetworkReceivedUnits ||
			allocationDispatch[key]-allocationReceived[key] != *allocation.NetworkLossUnits {
			return fmt.Errorf("physical-network flow allocation totals are invalid")
		}
	}
	return nil
}

func validateCityPhysicalNetworkPathEndpoints(
	physical *cityPhysicalNetworkHashState,
	policy CityPhysicalNetworkPolicy,
	allocation CityServiceAllocation,
	path CityPhysicalNetworkFlowPath,
) error {
	sourceIndex := cityPhysicalNetworkNodeIndex(physical.Nodes, path.NetworkCode, path.SourceNodeCode)
	sinkIndex := cityPhysicalNetworkNodeIndex(physical.Nodes, path.NetworkCode, path.SinkNodeCode)
	if sourceIndex < 0 || sinkIndex < 0 {
		return fmt.Errorf("physical-network flow path endpoint is unknown")
	}
	source, sink := physical.Nodes[sourceIndex], physical.Nodes[sinkIndex]
	capacityCode := allocation.FacilityCode + "." + allocation.ServiceCode
	if policy.RouteDirection == CityNetworkRouteDemandToFacility {
		if source.DemandCode == nil || *source.DemandCode != allocation.DemandCode ||
			sink.CapacityCode == nil || *sink.CapacityCode != capacityCode {
			return fmt.Errorf("physical-network collection path endpoint binding is invalid")
		}
		return nil
	}
	if policy.RouteDirection != CityNetworkRouteSupplyToDemand ||
		source.CapacityCode == nil || *source.CapacityCode != capacityCode ||
		sink.DemandCode == nil || *sink.DemandCode != allocation.DemandCode {
		return fmt.Errorf("physical-network delivery path endpoint binding is invalid")
	}
	return nil
}

func cityPhysicalNetworkSegmentDirectionMatches(
	edge CityPhysicalNetworkEdge,
	segment CityPhysicalNetworkFlowSegment,
) bool {
	if segment.Direction == "forward" {
		return segment.FromNodeCode == edge.FromNodeCode && segment.ToNodeCode == edge.ToNodeCode
	}
	return segment.Direction == "reverse" &&
		edge.Direction == CityNetworkEdgeDirectionBidirectional &&
		segment.FromNodeCode == edge.ToNodeCode && segment.ToNodeCode == edge.FromNodeCode
}

func cityPhysicalNetworkTopologyProjectionFromState(
	state *cityPhysicalNetworkHashState,
	networkCode string,
) (cityPhysicalNetworkTopologyProjection, error) {
	projection := cityPhysicalNetworkTopologyProjection{
		Nodes: make([]CityPhysicalNetworkNode, 0),
		Edges: make([]CityPhysicalNetworkEdge, 0),
	}
	for index := range state.Networks {
		if state.Networks[index].Code == networkCode {
			if projection.Network != nil {
				return projection, fmt.Errorf("physical-network projection is duplicated")
			}
			item := state.Networks[index]
			projection.Network = &item
		}
	}
	for _, item := range state.Nodes {
		if item.NetworkCode == networkCode {
			projection.Nodes = append(projection.Nodes, item)
		}
	}
	for _, item := range state.Edges {
		if item.NetworkCode == networkCode {
			projection.Edges = append(projection.Edges, item)
		}
	}
	if projection.Network == nil && (len(projection.Nodes) > 0 || len(projection.Edges) > 0) {
		return projection, fmt.Errorf("physical-network child projection is orphaned")
	}
	sort.Slice(projection.Nodes, func(i, j int) bool { return projection.Nodes[i].Code < projection.Nodes[j].Code })
	sort.Slice(projection.Edges, func(i, j int) bool { return projection.Edges[i].Code < projection.Edges[j].Code })
	return projection, nil
}

func cityPhysicalNetworkSnapshotEqual(left, right CityPhysicalNetwork) bool {
	leftMetadata, rightMetadata := left.Metadata, right.Metadata
	left.Metadata, right.Metadata = nil, nil
	return reflect.DeepEqual(left, right) && worldRuntimeJSONEqual(leftMetadata, rightMetadata)
}

func cityPhysicalNetworkNodeSnapshotEqual(left, right CityPhysicalNetworkNode) bool {
	leftMetadata, rightMetadata := left.Metadata, right.Metadata
	left.Metadata, right.Metadata = nil, nil
	return reflect.DeepEqual(left, right) && worldRuntimeJSONEqual(leftMetadata, rightMetadata)
}

func cityPhysicalNetworkEdgeSnapshotEqual(left, right CityPhysicalNetworkEdge) bool {
	leftMetadata, rightMetadata := left.Metadata, right.Metadata
	left.Metadata, right.Metadata = nil, nil
	return reflect.DeepEqual(left, right) && worldRuntimeJSONEqual(leftMetadata, rightMetadata)
}

func cityPhysicalNetworkIndex(items []CityPhysicalNetwork, code string) int {
	for index := range items {
		if items[index].Code == code {
			return index
		}
	}
	return -1
}

func cityPhysicalNetworkNodeIndex(items []CityPhysicalNetworkNode, networkCode, code string) int {
	for index := range items {
		if items[index].NetworkCode == networkCode && items[index].Code == code {
			return index
		}
	}
	return -1
}

func cityPhysicalNetworkEdgeIndex(items []CityPhysicalNetworkEdge, networkCode, code string) int {
	for index := range items {
		if items[index].NetworkCode == networkCode && items[index].Code == code {
			return index
		}
	}
	return -1
}

func cityPhysicalNetworkPolicyForService(
	items []CityPhysicalNetworkPolicy,
	serviceCode string,
) *CityPhysicalNetworkPolicy {
	for index := range items {
		if items[index].ServiceCode == serviceCode {
			return &items[index]
		}
	}
	return nil
}

func cityServiceAllocationForPhysicalPath(
	items []CityServiceAllocation,
	tick, sequence int64,
	allocationIndex int,
) *CityServiceAllocation {
	for index := range items {
		if items[index].Tick == tick && items[index].Sequence == sequence &&
			items[index].AllocationIndex == allocationIndex {
			return &items[index]
		}
	}
	return nil
}

func sortCityPhysicalNetworkState(state *cityPhysicalNetworkHashState) {
	if state == nil {
		return
	}
	sort.Slice(state.Policies, func(i, j int) bool { return state.Policies[i].ServiceCode < state.Policies[j].ServiceCode })
	sort.Slice(state.Networks, func(i, j int) bool { return state.Networks[i].Code < state.Networks[j].Code })
	sort.Slice(state.Nodes, func(i, j int) bool {
		if state.Nodes[i].NetworkCode != state.Nodes[j].NetworkCode {
			return state.Nodes[i].NetworkCode < state.Nodes[j].NetworkCode
		}
		return state.Nodes[i].Code < state.Nodes[j].Code
	})
	sort.Slice(state.Edges, func(i, j int) bool {
		if state.Edges[i].NetworkCode != state.Edges[j].NetworkCode {
			return state.Edges[i].NetworkCode < state.Edges[j].NetworkCode
		}
		return state.Edges[i].Code < state.Edges[j].Code
	})
	sort.Slice(state.Facts, func(i, j int) bool {
		if state.Facts[i].Tick != state.Facts[j].Tick {
			return state.Facts[i].Tick < state.Facts[j].Tick
		}
		return state.Facts[i].Sequence < state.Facts[j].Sequence
	})
	sort.Slice(state.Batches, func(i, j int) bool {
		if state.Batches[i].Tick != state.Batches[j].Tick {
			return state.Batches[i].Tick < state.Batches[j].Tick
		}
		return state.Batches[i].Sequence < state.Batches[j].Sequence
	})
	sort.Slice(state.Paths, func(i, j int) bool {
		left, right := state.Paths[i], state.Paths[j]
		if left.Tick != right.Tick {
			return left.Tick < right.Tick
		}
		if left.Sequence != right.Sequence {
			return left.Sequence < right.Sequence
		}
		if left.ServiceSequence != right.ServiceSequence {
			return left.ServiceSequence < right.ServiceSequence
		}
		if left.AllocationIndex != right.AllocationIndex {
			return left.AllocationIndex < right.AllocationIndex
		}
		return left.PathIndex < right.PathIndex
	})
	sort.Slice(state.Segments, func(i, j int) bool {
		left, right := state.Segments[i], state.Segments[j]
		if left.Tick != right.Tick {
			return left.Tick < right.Tick
		}
		if left.Sequence != right.Sequence {
			return left.Sequence < right.Sequence
		}
		if left.ServiceSequence != right.ServiceSequence {
			return left.ServiceSequence < right.ServiceSequence
		}
		if left.AllocationIndex != right.AllocationIndex {
			return left.AllocationIndex < right.AllocationIndex
		}
		if left.PathIndex != right.PathIndex {
			return left.PathIndex < right.PathIndex
		}
		return left.SegmentIndex < right.SegmentIndex
	})
}
