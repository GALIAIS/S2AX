package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

type cityPhysicalNetworkTopologySyncResult struct {
	facts        []CityPhysicalNetworkFact
	nextSequence int64
}

type cityPhysicalNetworkTopologyNetworkRef struct {
	id               int64
	code             string
	topologyRevision int64
	version          int64
	baselineMode     string
	snapshot         CityPhysicalNetwork
}

type cityPhysicalNetworkTopologyNodeRef struct {
	id         int64
	code       string
	role       string
	capacityID sql.NullInt64
	demandID   sql.NullInt64
	districtID sql.NullInt64
	buildingID sql.NullInt64
	status     string
	version    int64
	snapshot   CityPhysicalNetworkNode
}

type cityPhysicalNetworkTopologyEdgeRef struct {
	id                     int64
	code                   string
	fromNodeID             int64
	toNodeID               int64
	installedCapacityUnits int64
	availableCapacityUnits int64
	lossMilli              int
	baseCostUnits          int64
	status                 string
	version                int64
	snapshot               CityPhysicalNetworkEdge
}

type cityPhysicalNetworkDesiredNode struct {
	code         string
	role         string
	capacityID   *int64
	capacityCode *string
	demandID     *int64
	demandCode   *string
	districtID   int64
	districtCode string
	buildingID   *int64
	buildingCode *string
	status       string
	metadata     json.RawMessage
}

type cityPhysicalNetworkDesiredEdge struct {
	code                   string
	fromNodeCode           string
	toNodeCode             string
	installedCapacityUnits int64
	baseCostUnits          int64
	status                 string
	metadata               json.RawMessage
}

type cityPhysicalNetworkTopologyProjection struct {
	Network *CityPhysicalNetwork      `json:"network,omitempty"`
	Nodes   []CityPhysicalNetworkNode `json:"nodes"`
	Edges   []CityPhysicalNetworkEdge `json:"edges"`
}

type cityPhysicalNetworkTopologyFactPayload struct {
	SchemaVersion        int                       `json:"schema_version"`
	Mode                 string                    `json:"mode"`
	ServiceCode          string                    `json:"service_code"`
	ConnectionCodes      []string                  `json:"connection_codes"`
	BeforeProjectionHash string                    `json:"before_projection_hash"`
	AfterProjectionHash  string                    `json:"after_projection_hash"`
	NetworkBefore        *CityPhysicalNetwork      `json:"network_before,omitempty"`
	NetworkAfter         CityPhysicalNetwork       `json:"network_after"`
	NodeBefore           []CityPhysicalNetworkNode `json:"node_before"`
	NodeUpserts          []CityPhysicalNetworkNode `json:"node_upserts"`
	EdgeBefore           []CityPhysicalNetworkEdge `json:"edge_before"`
	EdgeUpserts          []CityPhysicalNetworkEdge `json:"edge_upserts"`
}

func synchronizeCityPhysicalNetworkTopology(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, firstSequence int64,
) (cityPhysicalNetworkTopologySyncResult, error) {
	result := cityPhysicalNetworkTopologySyncResult{
		facts: make([]CityPhysicalNetworkFact, 0), nextSequence: firstSequence,
	}
	connections, err := loadCityPhysicalNetworkManagedConnections(ctx, tx, worldID, true)
	if err != nil {
		return result, err
	}
	if len(connections) == 0 {
		return result, nil
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_physical_network_auto_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10)); err != nil {
		return result, fmt.Errorf("enable physical network topology synchronization: %w", err)
	}
	grouped := make(map[string][]cityPhysicalNetworkBaselineConnection)
	for _, connection := range connections {
		grouped[connection.ServiceCode] = append(grouped[connection.ServiceCode], connection)
	}
	serviceCodes := make([]string, 0, len(grouped))
	for serviceCode := range grouped {
		serviceCodes = append(serviceCodes, serviceCode)
	}
	sort.Strings(serviceCodes)
	for _, serviceCode := range serviceCodes {
		connections := grouped[serviceCode]
		fact, changed, syncErr := synchronizeCityPhysicalServiceNetwork(
			ctx, tx, worldID, targetTick, result.nextSequence, connections,
		)
		if syncErr != nil {
			return result, syncErr
		}
		if changed {
			result.facts = append(result.facts, fact)
			result.nextSequence++
		}
	}
	return result, nil
}

func synchronizeCityPhysicalServiceNetwork(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, sequence int64,
	connections []cityPhysicalNetworkBaselineConnection,
) (CityPhysicalNetworkFact, bool, error) {
	if len(connections) == 0 {
		return CityPhysicalNetworkFact{}, false, nil
	}
	service := connections[0]
	networkCode, err := cityPhysicalNetworkBaselineCode("network", service.ServiceCode)
	if err != nil {
		return CityPhysicalNetworkFact{}, false, err
	}
	network, err := loadCityPhysicalNetworkTopologyNetwork(
		ctx, tx, worldID, service.ServiceID,
	)
	if err != nil {
		return CityPhysicalNetworkFact{}, false, err
	}
	if network != nil && network.baselineMode != "legacy_direct" {
		return CityPhysicalNetworkFact{}, false, nil
	}
	nodes, edges, err := loadCityPhysicalNetworkTopologyProjection(ctx, tx, worldID, network)
	if err != nil {
		return CityPhysicalNetworkFact{}, false, err
	}
	desiredNodes, desiredEdges, err := buildCityPhysicalNetworkDesiredTopology(connections)
	if err != nil {
		return CityPhysicalNetworkFact{}, false, err
	}
	if network != nil && !cityPhysicalNetworkTopologyContainsOnlyDesiredCodes(
		nodes, edges, desiredNodes, desiredEdges,
	) {
		return CityPhysicalNetworkFact{}, false, ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"field": "physical_topology_unmanaged_projection"},
		)
	}
	if network != nil && cityPhysicalNetworkTopologyMatches(nodes, edges, desiredNodes, desiredEdges) {
		return CityPhysicalNetworkFact{}, false, nil
	}
	versionBefore := int64(0)
	if network != nil {
		versionBefore = network.version
	}
	connectionCodes := make([]string, 0, len(connections))
	for _, connection := range connections {
		connectionCodes = append(connectionCodes, connection.Code)
	}
	payloadValue, err := buildCityPhysicalNetworkTopologyFactPayload(
		networkCode, service, targetTick, sequence, network, nodes, edges,
		desiredNodes, desiredEdges, connectionCodes,
	)
	if err != nil {
		return CityPhysicalNetworkFact{}, false, err
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return CityPhysicalNetworkFact{}, false, fmt.Errorf("marshal physical topology sync fact: %w", err)
	}
	var factID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_facts
    (world_id, tick, sequence, phase, fact_type, subject_kind, subject_code,
     version_before, version_after, payload)
VALUES ($1, $2, $3, $4, $5, 'network', $6, $7, $7 + 1, $8::jsonb)
RETURNING id`, worldID, targetTick, sequence, CityPhysicalNetworkPhasePreNetwork,
		CityPhysicalNetworkFactTopologySynchronized, networkCode, versionBefore, payload).
		Scan(&factID); err != nil {
		return CityPhysicalNetworkFact{}, false, fmt.Errorf("insert physical topology sync fact: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_physical_network_fact_id', $1, TRUE)`,
		strconv.FormatInt(factID, 10)); err != nil {
		return CityPhysicalNetworkFact{}, false, fmt.Errorf("activate physical topology sync fact: %w", err)
	}
	networkDelta, nodeDelta, edgeDelta := int64(0), int64(0), int64(0)
	if network == nil {
		network = &cityPhysicalNetworkTopologyNetworkRef{
			code: networkCode, topologyRevision: 1, version: 1, baselineMode: "legacy_direct",
		}
		metadata := json.RawMessage(`{"baseline_mode":"legacy_direct","schema_version":1}`)
		if err = tx.QueryRowContext(ctx, `
INSERT INTO city_physical_networks
    (world_id, code, name, service_definition_id, status, topology_revision,
     created_tick, updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, 'active', 1, $5, $5, 1, $6, $7::jsonb)
RETURNING id`, worldID, networkCode, service.ServiceName+" network",
			service.ServiceID, targetTick, factID, metadata).Scan(&network.id); err != nil {
			return CityPhysicalNetworkFact{}, false, fmt.Errorf("create synchronized physical network: %w", err)
		}
		networkDelta = 1
	} else {
		if _, err = tx.ExecContext(ctx, `
UPDATE city_physical_networks
SET topology_revision = topology_revision + 1, updated_tick = $3,
    version = version + 1, source_fact_id = $4, updated_at = NOW()
WHERE world_id = $1 AND id = $2`, worldID, network.id, targetTick, factID); err != nil {
			return CityPhysicalNetworkFact{}, false, fmt.Errorf("advance synchronized physical network: %w", err)
		}
		network.topologyRevision++
		network.version++
	}
	nodeIDs := make(map[string]int64, len(desiredNodes))
	for _, desired := range desiredNodes {
		existing := nodes[desired.code]
		if existing == nil {
			var id int64
			if err = tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_nodes
    (world_id, network_id, code, role, capacity_id, demand_id, district_id,
     building_id, status, created_tick, updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, 1, $11, $12::jsonb)
RETURNING id`, worldID, network.id, desired.code, desired.role,
				optionalInt64Value(desired.capacityID), optionalInt64Value(desired.demandID),
				desired.districtID, optionalInt64Value(desired.buildingID), desired.status,
				targetTick, factID, desired.metadata).Scan(&id); err != nil {
				return CityPhysicalNetworkFact{}, false, fmt.Errorf("insert synchronized physical node %s: %w", desired.code, err)
			}
			nodeIDs[desired.code] = id
			nodeDelta++
			continue
		}
		nodeIDs[desired.code] = existing.id
		if cityPhysicalNetworkNodeMatches(existing, desired) {
			continue
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE city_physical_network_nodes
SET role = $3, capacity_id = $4, demand_id = $5, district_id = $6,
    building_id = $7, status = $8, updated_tick = $9,
    version = version + 1, source_fact_id = $10, metadata = $11::jsonb,
    updated_at = NOW()
WHERE world_id = $1 AND id = $2`, worldID, existing.id, desired.role,
			optionalInt64Value(desired.capacityID), optionalInt64Value(desired.demandID),
			desired.districtID, optionalInt64Value(desired.buildingID), desired.status,
			targetTick, factID, desired.metadata); err != nil {
			return CityPhysicalNetworkFact{}, false, fmt.Errorf("update synchronized physical node %s: %w", desired.code, err)
		}
	}
	for _, desired := range desiredEdges {
		fromNodeID, toNodeID := nodeIDs[desired.fromNodeCode], nodeIDs[desired.toNodeCode]
		if fromNodeID <= 0 || toNodeID <= 0 {
			return CityPhysicalNetworkFact{}, false, ErrCitySimulationInvariant.WithMetadata(
				map[string]string{"field": "physical_topology_endpoint"},
			)
		}
		existing := edges[desired.code]
		if existing == nil {
			if _, err = tx.ExecContext(ctx, `
INSERT INTO city_physical_network_edges
    (world_id, network_id, code, from_node_id, to_node_id, direction,
     installed_capacity_units, availability_milli, available_capacity_units,
     loss_milli, base_cost_units, status, condition_milli, created_tick,
     updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, 'directed', $6, 1000, $6, 0, $7, $8,
        1000, $9, $9, 1, $10, $11::jsonb)`, worldID, network.id, desired.code,
				fromNodeID, toNodeID, desired.installedCapacityUnits,
				desired.baseCostUnits, desired.status, targetTick, factID,
				desired.metadata); err != nil {
				return CityPhysicalNetworkFact{}, false, fmt.Errorf("insert synchronized physical edge %s: %w", desired.code, err)
			}
			edgeDelta++
			continue
		}
		if cityPhysicalNetworkEdgeMatches(existing, desired, fromNodeID, toNodeID) {
			continue
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE city_physical_network_edges
SET from_node_id = $3, to_node_id = $4, direction = 'directed',
    installed_capacity_units = $5, availability_milli = 1000,
    available_capacity_units = $5, loss_milli = 0, base_cost_units = $6,
    status = $7, condition_milli = 1000, failure_count = 0,
    updated_tick = $8, version = version + 1, source_fact_id = $9,
    metadata = $10::jsonb, updated_at = NOW()
WHERE world_id = $1 AND id = $2`, worldID, existing.id, fromNodeID, toNodeID,
			desired.installedCapacityUnits, desired.baseCostUnits, desired.status,
			targetTick, factID, desired.metadata); err != nil {
			return CityPhysicalNetworkFact{}, false, fmt.Errorf("update synchronized physical edge %s: %w", desired.code, err)
		}
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_physical_network_profiles
SET network_count = network_count + $2, node_count = node_count + $3,
    edge_count = edge_count + $4, fact_count = fact_count + 1,
    revision = revision + 1, updated_at = NOW()
WHERE world_id = $1`, worldID, networkDelta, nodeDelta, edgeDelta); err != nil {
		return CityPhysicalNetworkFact{}, false, fmt.Errorf("advance physical topology sync profile: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_physical_network_facts SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, factID); err != nil {
		return CityPhysicalNetworkFact{}, false, fmt.Errorf("post physical topology sync fact: %w", err)
	}
	fact := CityPhysicalNetworkFact{
		Tick: targetTick, Sequence: sequence, Phase: CityPhysicalNetworkPhasePreNetwork,
		FactType:    CityPhysicalNetworkFactTopologySynchronized,
		SubjectKind: "network", SubjectCode: networkCode,
		VersionBefore: versionBefore, VersionAfter: versionBefore + 1, Payload: payload,
	}
	return fact, true, nil
}

func cityPhysicalNetworkTopologyContainsOnlyDesiredCodes(
	nodes map[string]*cityPhysicalNetworkTopologyNodeRef,
	edges map[string]*cityPhysicalNetworkTopologyEdgeRef,
	desiredNodes []cityPhysicalNetworkDesiredNode,
	desiredEdges []cityPhysicalNetworkDesiredEdge,
) bool {
	desiredNodeCodes := make(map[string]struct{}, len(desiredNodes))
	for _, item := range desiredNodes {
		desiredNodeCodes[item.code] = struct{}{}
	}
	for code := range nodes {
		if _, exists := desiredNodeCodes[code]; !exists {
			return false
		}
	}
	desiredEdgeCodes := make(map[string]struct{}, len(desiredEdges))
	for _, item := range desiredEdges {
		desiredEdgeCodes[item.code] = struct{}{}
	}
	for code := range edges {
		if _, exists := desiredEdgeCodes[code]; !exists {
			return false
		}
	}
	return true
}

func loadCityPhysicalNetworkTopologyNetwork(
	ctx context.Context, queryer citySQLQueryer, worldID, serviceID int64,
) (*cityPhysicalNetworkTopologyNetworkRef, error) {
	item := &cityPhysicalNetworkTopologyNetworkRef{}
	err := queryer.QueryRowContext(ctx, `
SELECT network.id, network.code, network.name, service.code, network.status,
       network.topology_revision, network.created_tick, network.updated_tick,
       network.version, COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0),
       network.metadata, COALESCE(network.metadata->>'baseline_mode', '')
FROM city_physical_networks network
JOIN city_service_definitions service ON service.id = network.service_definition_id
LEFT JOIN city_physical_network_facts fact ON fact.id = network.source_fact_id
WHERE network.world_id = $1 AND network.service_definition_id = $2
  AND network.status <> 'retired'`,
		worldID, serviceID).Scan(
		&item.id, &item.snapshot.Code, &item.snapshot.Name, &item.snapshot.ServiceCode,
		&item.snapshot.Status, &item.snapshot.TopologyRevision,
		&item.snapshot.CreatedTick, &item.snapshot.UpdatedTick, &item.snapshot.Version,
		&item.snapshot.SourceFactTick, &item.snapshot.SourceFactSequence,
		&item.snapshot.Metadata, &item.baselineMode,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load synchronized physical network: %w", err)
	}
	item.code = item.snapshot.Code
	item.topologyRevision = item.snapshot.TopologyRevision
	item.version = item.snapshot.Version
	return item, nil
}

func loadCityPhysicalNetworkTopologyProjection(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	network *cityPhysicalNetworkTopologyNetworkRef,
) (map[string]*cityPhysicalNetworkTopologyNodeRef, map[string]*cityPhysicalNetworkTopologyEdgeRef, error) {
	nodes := make(map[string]*cityPhysicalNetworkTopologyNodeRef)
	edges := make(map[string]*cityPhysicalNetworkTopologyEdgeRef)
	if network == nil {
		return nodes, edges, nil
	}
	nodeRows, err := queryer.QueryContext(ctx, `
SELECT node.id, node.code, node.role, node.capacity_id, node.demand_id,
       node.district_id, node.building_id, node.status, node.version,
       CASE WHEN capacity.id IS NULL THEN NULL ELSE facility.code || '.' || service.code END,
       demand.code, district.code, building.code, node.world_x, node.world_y,
       node.world_z, node.created_tick, node.updated_tick,
       COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0), node.metadata
FROM city_physical_network_nodes node
LEFT JOIN city_facility_service_capacities capacity ON capacity.id = node.capacity_id
LEFT JOIN city_facilities facility ON facility.id = capacity.facility_id
LEFT JOIN city_service_definitions service ON service.id = capacity.service_definition_id
LEFT JOIN city_service_demands demand ON demand.id = node.demand_id
LEFT JOIN city_districts district ON district.id = node.district_id
LEFT JOIN city_buildings building ON building.id = node.building_id
LEFT JOIN city_physical_network_facts fact ON fact.id = node.source_fact_id
WHERE node.world_id = $1 AND node.network_id = $2 ORDER BY node.code`, worldID, network.id)
	if err != nil {
		return nil, nil, fmt.Errorf("load synchronized physical nodes: %w", err)
	}
	for nodeRows.Next() {
		item := &cityPhysicalNetworkTopologyNodeRef{}
		var capacityCode, demandCode, districtCode, buildingCode sql.NullString
		var worldX, worldY sql.NullInt64
		var worldZ sql.NullInt32
		if err = nodeRows.Scan(
			&item.id, &item.code, &item.role, &item.capacityID, &item.demandID,
			&item.districtID, &item.buildingID, &item.status, &item.version,
			&capacityCode, &demandCode, &districtCode, &buildingCode,
			&worldX, &worldY, &worldZ, &item.snapshot.CreatedTick,
			&item.snapshot.UpdatedTick, &item.snapshot.SourceFactTick,
			&item.snapshot.SourceFactSequence, &item.snapshot.Metadata,
		); err != nil {
			_ = nodeRows.Close()
			return nil, nil, fmt.Errorf("scan synchronized physical node: %w", err)
		}
		item.snapshot.Code = item.code
		item.snapshot.NetworkCode = network.code
		item.snapshot.Role = item.role
		item.snapshot.CapacityCode = nullStringPointer(capacityCode)
		item.snapshot.DemandCode = nullStringPointer(demandCode)
		item.snapshot.DistrictCode = nullStringPointer(districtCode)
		item.snapshot.BuildingCode = nullStringPointer(buildingCode)
		item.snapshot.WorldX = nullInt64Pointer(worldX)
		item.snapshot.WorldY = nullInt64Pointer(worldY)
		if worldZ.Valid {
			value := int(worldZ.Int32)
			item.snapshot.WorldZ = &value
		}
		item.snapshot.Status = item.status
		item.snapshot.Version = item.version
		nodes[item.code] = item
	}
	if err = closeCityRows(nodeRows, "iterate synchronized physical nodes"); err != nil {
		return nil, nil, err
	}
	edgeRows, err := queryer.QueryContext(ctx, `
SELECT edge.id, edge.code, edge.from_node_id, edge.to_node_id,
       edge.installed_capacity_units, edge.available_capacity_units,
       edge.loss_milli, edge.base_cost_units, edge.status, edge.version,
       source.code, sink.code, edge.direction, edge.availability_milli,
       edge.condition_milli, edge.failure_count, edge.created_tick,
       edge.updated_tick, COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0),
       edge.metadata
FROM city_physical_network_edges edge
JOIN city_physical_network_nodes source ON source.id = edge.from_node_id
JOIN city_physical_network_nodes sink ON sink.id = edge.to_node_id
LEFT JOIN city_physical_network_facts fact ON fact.id = edge.source_fact_id
WHERE edge.world_id = $1 AND edge.network_id = $2 ORDER BY edge.code`, worldID, network.id)
	if err != nil {
		return nil, nil, fmt.Errorf("load synchronized physical edges: %w", err)
	}
	for edgeRows.Next() {
		item := &cityPhysicalNetworkTopologyEdgeRef{}
		if err = edgeRows.Scan(
			&item.id, &item.code, &item.fromNodeID, &item.toNodeID,
			&item.installedCapacityUnits, &item.availableCapacityUnits,
			&item.lossMilli, &item.baseCostUnits, &item.status, &item.version,
			&item.snapshot.FromNodeCode, &item.snapshot.ToNodeCode,
			&item.snapshot.Direction, &item.snapshot.AvailabilityMilli,
			&item.snapshot.ConditionMilli, &item.snapshot.FailureCount,
			&item.snapshot.CreatedTick, &item.snapshot.UpdatedTick,
			&item.snapshot.SourceFactTick, &item.snapshot.SourceFactSequence,
			&item.snapshot.Metadata,
		); err != nil {
			_ = edgeRows.Close()
			return nil, nil, fmt.Errorf("scan synchronized physical edge: %w", err)
		}
		item.snapshot.Code = item.code
		item.snapshot.NetworkCode = network.code
		item.snapshot.InstalledCapacityUnits = item.installedCapacityUnits
		item.snapshot.AvailableCapacityUnits = item.availableCapacityUnits
		item.snapshot.LossMilli = item.lossMilli
		item.snapshot.BaseCostUnits = item.baseCostUnits
		item.snapshot.Status = item.status
		item.snapshot.Version = item.version
		edges[item.code] = item
	}
	if err = closeCityRows(edgeRows, "iterate synchronized physical edges"); err != nil {
		return nil, nil, err
	}
	return nodes, edges, nil
}

func buildCityPhysicalNetworkDesiredTopology(
	connections []cityPhysicalNetworkBaselineConnection,
) ([]cityPhysicalNetworkDesiredNode, []cityPhysicalNetworkDesiredEdge, error) {
	nodes := make(map[string]cityPhysicalNetworkDesiredNode)
	edges := make([]cityPhysicalNetworkDesiredEdge, 0, len(connections))
	for _, connection := range connections {
		supplyCode, err := cityPhysicalNetworkBaselineCode(
			"supply", connection.ServiceCode, connection.FacilityCode,
		)
		if err != nil {
			return nil, nil, err
		}
		capacityID := connection.CapacityID
		capacityCode := connection.FacilityCode + "." + connection.ServiceCode
		supplyStatus := CityNetworkNodeStatusActive
		if connection.FacilityStatus == CityFacilityStatusRetired ||
			connection.Status == CityServiceProjectionStatusRetired {
			supplyStatus = CityNetworkNodeStatusRetired
		}
		supplyMetadata, err := json.Marshal(map[string]any{
			"baseline_mode": "legacy_direct", "capacity_id": connection.CapacityID,
		})
		if err != nil {
			return nil, nil, err
		}
		supplyNode := cityPhysicalNetworkDesiredNode{
			code: supplyCode, role: CityNetworkNodeRoleSupply, capacityID: &capacityID,
			capacityCode: &capacityCode, districtID: connection.FacilityDistrictID,
			districtCode: connection.FacilityDistrictCode,
			buildingID:   &connection.FacilityBuildingID,
			buildingCode: &connection.FacilityBuildingCode, status: supplyStatus,
			metadata: supplyMetadata,
		}
		mergedSupply, err := mergeCityPhysicalNetworkDesiredNode(nodes[supplyCode], supplyNode)
		if err != nil {
			return nil, nil, err
		}
		nodes[supplyCode] = mergedSupply
		demandCode, err := cityPhysicalNetworkBaselineCode(
			"demand", connection.ServiceCode, connection.DemandCode,
		)
		if err != nil {
			return nil, nil, err
		}
		demandID := connection.DemandID
		demandReferenceCode := connection.DemandCode
		demandStatus := CityNetworkNodeStatusActive
		if connection.Status == CityServiceProjectionStatusRetired ||
			connection.DemandStatus == CityServiceProjectionStatusRetired {
			demandStatus = CityNetworkNodeStatusRetired
		} else if connection.DemandStatus == CityServiceProjectionStatusSuspended {
			demandStatus = CityNetworkNodeStatusOffline
		}
		demandMetadata, err := json.Marshal(map[string]any{
			"baseline_mode": "legacy_direct", "demand_id": connection.DemandID,
		})
		if err != nil {
			return nil, nil, err
		}
		var demandBuildingID *int64
		var demandBuildingCode *string
		if connection.DemandBuildingID.Valid {
			value := connection.DemandBuildingID.Int64
			demandBuildingID = &value
		}
		if connection.DemandBuildingCode.Valid {
			value := connection.DemandBuildingCode.String
			demandBuildingCode = &value
		}
		demandNode := cityPhysicalNetworkDesiredNode{
			code: demandCode, role: CityNetworkNodeRoleDemand, demandID: &demandID,
			demandCode: &demandReferenceCode, districtID: connection.DemandDistrictID,
			districtCode: connection.DemandDistrictCode, buildingID: demandBuildingID,
			buildingCode: demandBuildingCode, status: demandStatus, metadata: demandMetadata,
		}
		mergedDemand, err := mergeCityPhysicalNetworkDesiredNode(nodes[demandCode], demandNode)
		if err != nil {
			return nil, nil, err
		}
		nodes[demandCode] = mergedDemand
		fromNodeCode, toNodeCode := supplyCode, demandCode
		if connection.FlowKind == "collection" {
			fromNodeCode, toNodeCode = demandCode, supplyCode
		}
		edgeStatus := CityNetworkEdgeStatusIsolated
		if connection.Status == CityServiceProjectionStatusRetired {
			edgeStatus = CityNetworkEdgeStatusRetired
		} else if connection.Status == CityServiceProjectionStatusActive &&
			supplyStatus == CityNetworkNodeStatusActive && demandStatus == CityNetworkNodeStatusActive {
			edgeStatus = CityNetworkEdgeStatusActive
		}
		edgeCode, err := cityPhysicalNetworkBaselineCode(
			"edge", connection.ServiceCode, connection.Code,
		)
		if err != nil {
			return nil, nil, err
		}
		edgeMetadata, err := json.Marshal(map[string]any{
			"baseline_mode": "legacy_direct", "connection_code": connection.Code,
		})
		if err != nil {
			return nil, nil, err
		}
		edges = append(edges, cityPhysicalNetworkDesiredEdge{
			code: edgeCode, fromNodeCode: fromNodeCode, toNodeCode: toNodeCode,
			installedCapacityUnits: connection.MaxFlowUnits,
			baseCostUnits:          int64(1001 - connection.Preference), status: edgeStatus,
			metadata: edgeMetadata,
		})
	}
	orderedNodes := make([]cityPhysicalNetworkDesiredNode, 0, len(nodes))
	for _, node := range nodes {
		orderedNodes = append(orderedNodes, node)
	}
	sort.Slice(orderedNodes, func(i, j int) bool { return orderedNodes[i].code < orderedNodes[j].code })
	sort.Slice(edges, func(i, j int) bool { return edges[i].code < edges[j].code })
	return orderedNodes, edges, nil
}

func mergeCityPhysicalNetworkDesiredNode(
	existing, candidate cityPhysicalNetworkDesiredNode,
) (cityPhysicalNetworkDesiredNode, error) {
	if existing.code == "" {
		return candidate, nil
	}
	if existing.code != candidate.code || existing.role != candidate.role ||
		!sameOptionalInt64(existing.capacityID, candidate.capacityID) ||
		!sameOptionalInt64(existing.demandID, candidate.demandID) ||
		existing.districtID != candidate.districtID ||
		!sameOptionalInt64(existing.buildingID, candidate.buildingID) {
		return cityPhysicalNetworkDesiredNode{}, ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"field": "physical_topology_node_identity"},
		)
	}
	if cityPhysicalNetworkNodeStatusRank(candidate.status) >
		cityPhysicalNetworkNodeStatusRank(existing.status) {
		existing.status = candidate.status
	}
	return existing, nil
}

func cityPhysicalNetworkNodeStatusRank(status string) int {
	switch status {
	case CityNetworkNodeStatusActive:
		return 3
	case CityNetworkNodeStatusOffline:
		return 2
	case CityNetworkNodeStatusRetired:
		return 1
	default:
		return 0
	}
}

func buildCityPhysicalNetworkTopologyFactPayload(
	networkCode string,
	service cityPhysicalNetworkBaselineConnection,
	targetTick, sequence int64,
	network *cityPhysicalNetworkTopologyNetworkRef,
	nodes map[string]*cityPhysicalNetworkTopologyNodeRef,
	edges map[string]*cityPhysicalNetworkTopologyEdgeRef,
	desiredNodes []cityPhysicalNetworkDesiredNode,
	desiredEdges []cityPhysicalNetworkDesiredEdge,
	connectionCodes []string,
) (cityPhysicalNetworkTopologyFactPayload, error) {
	before := cityPhysicalNetworkTopologyProjectionFromRefs(network, nodes, edges)
	beforeHash, err := cityPhysicalNetworkTopologyProjectionHash(before)
	if err != nil {
		return cityPhysicalNetworkTopologyFactPayload{}, err
	}
	networkAfter := CityPhysicalNetwork{
		Code: networkCode, Name: service.ServiceName + " network",
		ServiceCode: service.ServiceCode, Status: CityNetworkStatusActive,
		TopologyRevision: 1, CreatedTick: targetTick, UpdatedTick: targetTick,
		Version: 1, SourceFactTick: targetTick, SourceFactSequence: sequence,
		Metadata: json.RawMessage(`{"baseline_mode":"legacy_direct","schema_version":1}`),
	}
	if network != nil {
		networkAfter = network.snapshot
		networkAfter.TopologyRevision++
		networkAfter.UpdatedTick = targetTick
		networkAfter.Version++
		networkAfter.SourceFactTick = targetTick
		networkAfter.SourceFactSequence = sequence
	}
	nodeUpserts := make([]CityPhysicalNetworkNode, 0)
	nodeBefore := make([]CityPhysicalNetworkNode, 0)
	afterNodes := make(map[string]CityPhysicalNetworkNode, len(desiredNodes))
	for _, desired := range desiredNodes {
		existing := nodes[desired.code]
		if existing != nil && cityPhysicalNetworkNodeMatches(existing, desired) {
			afterNodes[desired.code] = existing.snapshot
			continue
		}
		upsert := cityPhysicalNetworkDesiredNodeSnapshot(
			networkCode, targetTick, sequence, existing, desired,
		)
		if existing != nil {
			nodeBefore = append(nodeBefore, existing.snapshot)
		}
		nodeUpserts = append(nodeUpserts, upsert)
		afterNodes[desired.code] = upsert
	}
	edgeUpserts := make([]CityPhysicalNetworkEdge, 0)
	edgeBefore := make([]CityPhysicalNetworkEdge, 0)
	afterEdges := make(map[string]CityPhysicalNetworkEdge, len(desiredEdges))
	for _, desired := range desiredEdges {
		existing := edges[desired.code]
		from := nodes[desired.fromNodeCode]
		to := nodes[desired.toNodeCode]
		fromID, toID := int64(0), int64(0)
		if from != nil {
			fromID = from.id
		}
		if to != nil {
			toID = to.id
		}
		if existing != nil && fromID > 0 && toID > 0 &&
			cityPhysicalNetworkEdgeMatches(existing, desired, fromID, toID) {
			afterEdges[desired.code] = existing.snapshot
			continue
		}
		upsert := cityPhysicalNetworkDesiredEdgeSnapshot(
			networkCode, targetTick, sequence, existing, desired,
		)
		if existing != nil {
			edgeBefore = append(edgeBefore, existing.snapshot)
		}
		edgeUpserts = append(edgeUpserts, upsert)
		afterEdges[desired.code] = upsert
	}
	after := cityPhysicalNetworkTopologyProjection{
		Network: &networkAfter,
		Nodes:   cityPhysicalNetworkSortedNodeSnapshots(afterNodes),
		Edges:   cityPhysicalNetworkSortedEdgeSnapshots(afterEdges),
	}
	afterHash, err := cityPhysicalNetworkTopologyProjectionHash(after)
	if err != nil {
		return cityPhysicalNetworkTopologyFactPayload{}, err
	}
	connectionCodes = append([]string(nil), connectionCodes...)
	sort.Strings(connectionCodes)
	return cityPhysicalNetworkTopologyFactPayload{
		SchemaVersion: cityPhysicalNetworkSchemaVersion,
		Mode:          "legacy_direct", ServiceCode: service.ServiceCode,
		ConnectionCodes: connectionCodes, BeforeProjectionHash: beforeHash,
		AfterProjectionHash: afterHash, NetworkBefore: before.Network,
		NetworkAfter: networkAfter, NodeBefore: nodeBefore,
		NodeUpserts: nodeUpserts, EdgeBefore: edgeBefore, EdgeUpserts: edgeUpserts,
	}, nil
}

func cityPhysicalNetworkTopologyProjectionFromRefs(
	network *cityPhysicalNetworkTopologyNetworkRef,
	nodes map[string]*cityPhysicalNetworkTopologyNodeRef,
	edges map[string]*cityPhysicalNetworkTopologyEdgeRef,
) cityPhysicalNetworkTopologyProjection {
	projection := cityPhysicalNetworkTopologyProjection{
		Nodes: make([]CityPhysicalNetworkNode, 0, len(nodes)),
		Edges: make([]CityPhysicalNetworkEdge, 0, len(edges)),
	}
	if network != nil {
		item := network.snapshot
		projection.Network = &item
	}
	for _, item := range nodes {
		projection.Nodes = append(projection.Nodes, item.snapshot)
	}
	for _, item := range edges {
		projection.Edges = append(projection.Edges, item.snapshot)
	}
	sort.Slice(projection.Nodes, func(i, j int) bool {
		return projection.Nodes[i].Code < projection.Nodes[j].Code
	})
	sort.Slice(projection.Edges, func(i, j int) bool {
		return projection.Edges[i].Code < projection.Edges[j].Code
	})
	return projection
}

func cityPhysicalNetworkDesiredNodeSnapshot(
	networkCode string,
	targetTick, sequence int64,
	existing *cityPhysicalNetworkTopologyNodeRef,
	desired cityPhysicalNetworkDesiredNode,
) CityPhysicalNetworkNode {
	item := CityPhysicalNetworkNode{
		Code: desired.code, NetworkCode: networkCode, Role: desired.role,
		CapacityCode: desired.capacityCode, DemandCode: desired.demandCode,
		DistrictCode: &desired.districtCode, BuildingCode: desired.buildingCode,
		Status: desired.status, CreatedTick: targetTick, UpdatedTick: targetTick,
		Version: 1, SourceFactTick: targetTick, SourceFactSequence: sequence,
		Metadata: desired.metadata,
	}
	if existing != nil {
		item.WorldX = existing.snapshot.WorldX
		item.WorldY = existing.snapshot.WorldY
		item.WorldZ = existing.snapshot.WorldZ
		item.CreatedTick = existing.snapshot.CreatedTick
		item.Version = existing.version + 1
	}
	return item
}

func cityPhysicalNetworkDesiredEdgeSnapshot(
	networkCode string,
	targetTick, sequence int64,
	existing *cityPhysicalNetworkTopologyEdgeRef,
	desired cityPhysicalNetworkDesiredEdge,
) CityPhysicalNetworkEdge {
	item := CityPhysicalNetworkEdge{
		Code: desired.code, NetworkCode: networkCode,
		FromNodeCode: desired.fromNodeCode, ToNodeCode: desired.toNodeCode,
		Direction:              CityNetworkEdgeDirectionDirected,
		InstalledCapacityUnits: desired.installedCapacityUnits,
		AvailabilityMilli:      1000, AvailableCapacityUnits: desired.installedCapacityUnits,
		LossMilli: 0, BaseCostUnits: desired.baseCostUnits, Status: desired.status,
		ConditionMilli: 1000, FailureCount: 0,
		CreatedTick: targetTick, UpdatedTick: targetTick, Version: 1,
		SourceFactTick: targetTick, SourceFactSequence: sequence,
		Metadata: desired.metadata,
	}
	if existing != nil {
		item.CreatedTick = existing.snapshot.CreatedTick
		item.Version = existing.version + 1
	}
	return item
}

func cityPhysicalNetworkSortedNodeSnapshots(
	items map[string]CityPhysicalNetworkNode,
) []CityPhysicalNetworkNode {
	result := make([]CityPhysicalNetworkNode, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Code < result[j].Code })
	return result
}

func cityPhysicalNetworkSortedEdgeSnapshots(
	items map[string]CityPhysicalNetworkEdge,
) []CityPhysicalNetworkEdge {
	result := make([]CityPhysicalNetworkEdge, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Code < result[j].Code })
	return result
}

func cityPhysicalNetworkTopologyProjectionHash(
	projection cityPhysicalNetworkTopologyProjection,
) (string, error) {
	if projection.Nodes == nil {
		projection.Nodes = make([]CityPhysicalNetworkNode, 0)
	}
	if projection.Edges == nil {
		projection.Edges = make([]CityPhysicalNetworkEdge, 0)
	}
	raw, err := json.Marshal(projection)
	if err != nil {
		return "", fmt.Errorf("marshal physical topology projection: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityPhysicalNetworkTopologyMatches(
	nodes map[string]*cityPhysicalNetworkTopologyNodeRef,
	edges map[string]*cityPhysicalNetworkTopologyEdgeRef,
	desiredNodes []cityPhysicalNetworkDesiredNode,
	desiredEdges []cityPhysicalNetworkDesiredEdge,
) bool {
	if len(nodes) != len(desiredNodes) || len(edges) != len(desiredEdges) {
		return false
	}
	for _, desired := range desiredNodes {
		if !cityPhysicalNetworkNodeMatches(nodes[desired.code], desired) {
			return false
		}
	}
	for _, desired := range desiredEdges {
		from := nodes[desired.fromNodeCode]
		to := nodes[desired.toNodeCode]
		if from == nil || to == nil ||
			!cityPhysicalNetworkEdgeMatches(edges[desired.code], desired, from.id, to.id) {
			return false
		}
	}
	return true
}

func cityPhysicalNetworkNodeMatches(
	existing *cityPhysicalNetworkTopologyNodeRef,
	desired cityPhysicalNetworkDesiredNode,
) bool {
	if existing == nil || existing.role != desired.role || existing.status != desired.status ||
		!cityNullInt64EqualsPointer(existing.capacityID, desired.capacityID) ||
		!cityNullInt64EqualsPointer(existing.demandID, desired.demandID) ||
		!cityNullInt64EqualsPointer(existing.districtID, &desired.districtID) ||
		!cityNullInt64EqualsPointer(existing.buildingID, desired.buildingID) ||
		!cityOptionalStringEquals(existing.snapshot.CapacityCode, desired.capacityCode) ||
		!cityOptionalStringEquals(existing.snapshot.DemandCode, desired.demandCode) ||
		!cityOptionalStringEquals(existing.snapshot.DistrictCode, &desired.districtCode) ||
		!cityOptionalStringEquals(existing.snapshot.BuildingCode, desired.buildingCode) ||
		!worldRuntimeJSONEqual(existing.snapshot.Metadata, desired.metadata) {
		return false
	}
	return true
}

func cityPhysicalNetworkEdgeMatches(
	existing *cityPhysicalNetworkTopologyEdgeRef,
	desired cityPhysicalNetworkDesiredEdge,
	fromNodeID, toNodeID int64,
) bool {
	return existing != nil && existing.fromNodeID == fromNodeID && existing.toNodeID == toNodeID &&
		existing.snapshot.Direction == CityNetworkEdgeDirectionDirected &&
		existing.installedCapacityUnits == desired.installedCapacityUnits &&
		existing.snapshot.AvailabilityMilli == 1000 &&
		existing.availableCapacityUnits == desired.installedCapacityUnits &&
		existing.lossMilli == 0 && existing.baseCostUnits == desired.baseCostUnits &&
		existing.status == desired.status && existing.snapshot.ConditionMilli == 1000 &&
		existing.snapshot.FailureCount == 0 &&
		worldRuntimeJSONEqual(existing.snapshot.Metadata, desired.metadata)
}

func cityNullInt64EqualsPointer(value sql.NullInt64, pointer *int64) bool {
	if pointer == nil {
		return !value.Valid
	}
	return value.Valid && value.Int64 == *pointer
}

func cityOptionalStringEquals(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
