package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const (
	cityPhysicalNetworkQueryDefaultLimit = 50
	cityPhysicalNetworkQueryMaximumLimit = 200
)

type CityPhysicalNetworkQueryInput struct {
	UserID         int64
	WorldID        int64
	ServiceCode    string
	NetworkCode    string
	Status         string
	Role           string
	Phase          string
	FactType       string
	SourceNodeCode string
	SinkNodeCode   string
	ProbeUnits     int64
	AfterCode      string
	AfterTick      int64
	AfterSequence  int64
	Limit          int
}

type CityPhysicalNetworkOverview struct {
	ActiveNetworkCount         int64  `json:"active_network_count"`
	ActiveNodeCount            int64  `json:"active_node_count"`
	ActiveEdgeCount            int64  `json:"active_edge_count"`
	IsolatedEdgeCount          int64  `json:"isolated_edge_count"`
	FailedEdgeCount            int64  `json:"failed_edge_count"`
	InstalledEdgeCapacityUnits int64  `json:"installed_edge_capacity_units,string"`
	AvailableEdgeCapacityUnits int64  `json:"available_edge_capacity_units,string"`
	LatestFlowTick             *int64 `json:"latest_flow_tick,omitempty"`
	LatestDispatchedUnits      int64  `json:"latest_dispatched_units,string"`
	LatestNetworkReceivedUnits int64  `json:"latest_network_received_units,string"`
	LatestNetworkLossUnits     int64  `json:"latest_network_loss_units,string"`
	LatestDeliveryRatioMilli   int    `json:"latest_delivery_ratio_milli"`
}

type CityPhysicalNetworkCatalogView struct {
	Availability      string                       `json:"availability"`
	SimulationVersion string                       `json:"simulation_version"`
	RequiredVersion   string                       `json:"required_version"`
	Profile           *CityPhysicalNetworkProfile  `json:"profile,omitempty"`
	Overview          *CityPhysicalNetworkOverview `json:"overview,omitempty"`
	Policies          []CityPhysicalNetworkPolicy  `json:"policies"`
}

type CityPhysicalNetworkPage struct {
	Availability      string                `json:"availability"`
	SimulationVersion string                `json:"simulation_version"`
	RequiredVersion   string                `json:"required_version"`
	Items             []CityPhysicalNetwork `json:"items"`
	NextCode          *string               `json:"next_code,omitempty"`
}

type CityPhysicalNetworkNodePage struct {
	Availability      string                    `json:"availability"`
	SimulationVersion string                    `json:"simulation_version"`
	RequiredVersion   string                    `json:"required_version"`
	Items             []CityPhysicalNetworkNode `json:"items"`
	NextCode          *string                   `json:"next_code,omitempty"`
}

type CityPhysicalNetworkEdgePage struct {
	Availability      string                    `json:"availability"`
	SimulationVersion string                    `json:"simulation_version"`
	RequiredVersion   string                    `json:"required_version"`
	Items             []CityPhysicalNetworkEdge `json:"items"`
	NextCode          *string                   `json:"next_code,omitempty"`
}

type CityPhysicalNetworkCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type CityPhysicalNetworkFlowView struct {
	Batch    CityPhysicalNetworkFlowBatch     `json:"batch"`
	Paths    []CityPhysicalNetworkFlowPath    `json:"paths"`
	Segments []CityPhysicalNetworkFlowSegment `json:"segments"`
}

type CityPhysicalNetworkFlowPage struct {
	Availability      string                        `json:"availability"`
	SimulationVersion string                        `json:"simulation_version"`
	RequiredVersion   string                        `json:"required_version"`
	Items             []CityPhysicalNetworkFlowView `json:"items"`
	NextCursor        *CityPhysicalNetworkCursor    `json:"next_cursor,omitempty"`
}

type CityPhysicalNetworkFactPage struct {
	Availability      string                     `json:"availability"`
	SimulationVersion string                     `json:"simulation_version"`
	RequiredVersion   string                     `json:"required_version"`
	Items             []CityPhysicalNetworkFact  `json:"items"`
	NextCursor        *CityPhysicalNetworkCursor `json:"next_cursor,omitempty"`
}

func (s *CityEconomyService) GetCityPhysicalNetworkCatalog(
	ctx context.Context, input CityPhysicalNetworkQueryInput,
) (*CityPhysicalNetworkCatalogView, error) {
	version, available, err := s.cityPhysicalNetworkQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	view := &CityPhysicalNetworkCatalogView{
		Availability:      CityServiceAvailabilityUnsupported,
		SimulationVersion: version, RequiredVersion: CitySimulationVersionF8V3,
		Policies: make([]CityPhysicalNetworkPolicy, 0),
	}
	if !available {
		return view, nil
	}
	state := &CityPhysicalNetworkStateSet{Policies: make([]CityPhysicalNetworkPolicy, 0)}
	if err = loadCityPhysicalNetworkProfile(ctx, s.db, input.WorldID, &state.Profile); err != nil {
		return nil, err
	}
	if err = loadCityPhysicalNetworkPolicies(ctx, s.db, input.WorldID, state); err != nil {
		return nil, err
	}
	overview, err := loadCityPhysicalNetworkOverview(ctx, s.db, input.WorldID)
	if err != nil {
		return nil, err
	}
	view.Availability = CityServiceAvailabilityAvailable
	view.Profile = &state.Profile
	view.Overview = overview
	view.Policies = state.Policies
	return view, nil
}

func (s *CityEconomyService) ListCityPhysicalNetworks(
	ctx context.Context, input CityPhysicalNetworkQueryInput,
) (*CityPhysicalNetworkPage, error) {
	if err := normalizeCityPhysicalNetworkQuery(&input, "network"); err != nil {
		return nil, err
	}
	version, available, err := s.cityPhysicalNetworkQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityPhysicalNetworkPage{
		Availability:      CityServiceAvailabilityUnsupported,
		SimulationVersion: version, RequiredVersion: CitySimulationVersionF8V3,
		Items: make([]CityPhysicalNetwork, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT network.code, network.name, service.code, network.status,
       network.topology_revision, network.created_tick, network.updated_tick,
       network.version, COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0),
       network.metadata
FROM city_physical_networks network
JOIN city_service_definitions service ON service.id = network.service_definition_id
LEFT JOIN city_physical_network_facts fact ON fact.id = network.source_fact_id
WHERE network.world_id = $1
  AND ($2 = '' OR service.code = $2)
  AND ($3 = '' OR network.status = $3)
  AND ($4 = '' OR network.code > $4)
ORDER BY network.code
LIMIT $5`, input.WorldID, input.ServiceCode, input.Status,
		input.AfterCode, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city physical networks: %w", err)
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
			return nil, fmt.Errorf("scan city physical network: %w", err)
		}
		page.Items = append(page.Items, item)
	}
	if err = closeCityRows(rows, "iterate city physical networks"); err != nil {
		return nil, err
	}
	setCityPhysicalNetworkCodeCursor(&page.Items, input.Limit, &page.NextCode,
		func(item CityPhysicalNetwork) string { return item.Code })
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) ListCityPhysicalNetworkNodes(
	ctx context.Context, input CityPhysicalNetworkQueryInput,
) (*CityPhysicalNetworkNodePage, error) {
	if err := normalizeCityPhysicalNetworkQuery(&input, "node"); err != nil {
		return nil, err
	}
	version, available, err := s.cityPhysicalNetworkQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityPhysicalNetworkNodePage{
		Availability:      CityServiceAvailabilityUnsupported,
		SimulationVersion: version, RequiredVersion: CitySimulationVersionF8V3,
		Items: make([]CityPhysicalNetworkNode, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT node.code, network.code, node.role,
       CASE WHEN capacity.id IS NULL THEN NULL ELSE facility.code || '.' || service.code END,
       demand.code, district.code, building.code,
       node.world_x, node.world_y, node.world_z, node.status,
       node.created_tick, node.updated_tick, node.version,
       COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0), node.metadata
FROM city_physical_network_nodes node
JOIN city_physical_networks network ON network.id = node.network_id
JOIN city_service_definitions network_service ON network_service.id = network.service_definition_id
LEFT JOIN city_facility_service_capacities capacity ON capacity.id = node.capacity_id
LEFT JOIN city_facilities facility ON facility.id = capacity.facility_id
LEFT JOIN city_service_definitions service ON service.id = capacity.service_definition_id
LEFT JOIN city_service_demands demand ON demand.id = node.demand_id
LEFT JOIN city_districts district ON district.id = node.district_id
LEFT JOIN city_buildings building ON building.id = node.building_id
LEFT JOIN city_physical_network_facts fact ON fact.id = node.source_fact_id
WHERE node.world_id = $1
  AND ($2 = '' OR network_service.code = $2)
  AND ($3 = '' OR network.code = $3)
  AND ($4 = '' OR node.status = $4)
  AND ($5 = '' OR node.role = $5)
  AND ($6 = '' OR node.code > $6)
ORDER BY node.code
LIMIT $7`, input.WorldID, input.ServiceCode, input.NetworkCode,
		input.Status, input.Role, input.AfterCode, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city physical network nodes: %w", err)
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
			return nil, fmt.Errorf("scan city physical network node: %w", err)
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
		page.Items = append(page.Items, item)
	}
	if err = closeCityRows(rows, "iterate city physical network nodes"); err != nil {
		return nil, err
	}
	setCityPhysicalNetworkCodeCursor(&page.Items, input.Limit, &page.NextCode,
		func(item CityPhysicalNetworkNode) string { return item.Code })
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) ListCityPhysicalNetworkEdges(
	ctx context.Context, input CityPhysicalNetworkQueryInput,
) (*CityPhysicalNetworkEdgePage, error) {
	if err := normalizeCityPhysicalNetworkQuery(&input, "edge"); err != nil {
		return nil, err
	}
	version, available, err := s.cityPhysicalNetworkQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityPhysicalNetworkEdgePage{
		Availability:      CityServiceAvailabilityUnsupported,
		SimulationVersion: version, RequiredVersion: CitySimulationVersionF8V3,
		Items: make([]CityPhysicalNetworkEdge, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT edge.code, network.code, source.code, sink.code, edge.direction,
       edge.installed_capacity_units, edge.availability_milli,
       edge.available_capacity_units, edge.loss_milli, edge.base_cost_units,
       edge.status, edge.condition_milli, edge.failure_count,
       edge.created_tick, edge.updated_tick, edge.version,
       COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0), edge.metadata
FROM city_physical_network_edges edge
JOIN city_physical_networks network ON network.id = edge.network_id
JOIN city_service_definitions service ON service.id = network.service_definition_id
JOIN city_physical_network_nodes source ON source.id = edge.from_node_id
JOIN city_physical_network_nodes sink ON sink.id = edge.to_node_id
LEFT JOIN city_physical_network_facts fact ON fact.id = edge.source_fact_id
WHERE edge.world_id = $1
  AND ($2 = '' OR service.code = $2)
  AND ($3 = '' OR network.code = $3)
  AND ($4 = '' OR edge.status = $4)
  AND ($5 = '' OR edge.code > $5)
ORDER BY edge.code
LIMIT $6`, input.WorldID, input.ServiceCode, input.NetworkCode,
		input.Status, input.AfterCode, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city physical network edges: %w", err)
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
			return nil, fmt.Errorf("scan city physical network edge: %w", err)
		}
		page.Items = append(page.Items, item)
	}
	if err = closeCityRows(rows, "iterate city physical network edges"); err != nil {
		return nil, err
	}
	setCityPhysicalNetworkCodeCursor(&page.Items, input.Limit, &page.NextCode,
		func(item CityPhysicalNetworkEdge) string { return item.Code })
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) ListCityPhysicalNetworkFlows(
	ctx context.Context, input CityPhysicalNetworkQueryInput,
) (*CityPhysicalNetworkFlowPage, error) {
	if err := normalizeCityPhysicalNetworkQuery(&input, "flow"); err != nil {
		return nil, err
	}
	version, available, err := s.cityPhysicalNetworkQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityPhysicalNetworkFlowPage{
		Availability:      CityServiceAvailabilityUnsupported,
		SimulationVersion: version, RequiredVersion: CitySimulationVersionF8V3,
		Items: make([]CityPhysicalNetworkFlowView, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT batch.tick, batch.sequence, network.code, service.code,
       batch.topology_revision, batch.allocation_count, batch.path_count,
       batch.segment_count, batch.dispatched_units, batch.network_received_units,
       batch.network_loss_units, fact.tick, fact.sequence, batch.metadata
FROM city_physical_network_flow_batches batch
JOIN city_physical_networks network ON network.id = batch.network_id
JOIN city_service_definitions service ON service.id = network.service_definition_id
JOIN city_physical_network_facts fact ON fact.id = batch.source_fact_id
WHERE batch.world_id = $1
  AND ($2 = '' OR service.code = $2)
  AND ($3 = '' OR network.code = $3)
  AND (batch.tick > $4 OR (batch.tick = $4 AND batch.sequence > $5))
ORDER BY batch.tick, batch.sequence
LIMIT $6`, input.WorldID, input.ServiceCode, input.NetworkCode,
		input.AfterTick, input.AfterSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city physical network flows: %w", err)
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
			return nil, fmt.Errorf("scan city physical network flow: %w", err)
		}
		page.Items = append(page.Items, CityPhysicalNetworkFlowView{
			Batch: item, Paths: make([]CityPhysicalNetworkFlowPath, 0),
			Segments: make([]CityPhysicalNetworkFlowSegment, 0),
		})
	}
	if err = closeCityRows(rows, "iterate city physical network flows"); err != nil {
		return nil, err
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		last := page.Items[len(page.Items)-1].Batch
		page.NextCursor = &CityPhysicalNetworkCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	if len(page.Items) > 0 {
		if err = s.loadCityPhysicalNetworkFlowDetails(ctx, input, page.Items); err != nil {
			return nil, err
		}
	}
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) loadCityPhysicalNetworkFlowDetails(
	ctx context.Context, input CityPhysicalNetworkQueryInput,
	items []CityPhysicalNetworkFlowView,
) error {
	first, last := items[0].Batch, items[len(items)-1].Batch
	index := make(map[cityServiceRecoveryFactKey]int, len(items))
	for itemIndex := range items {
		batch := items[itemIndex].Batch
		index[cityServiceRecoveryFactKey{tick: batch.Tick, sequence: batch.Sequence}] = itemIndex
	}
	pathRows, err := s.db.QueryContext(ctx, `
SELECT batch.tick, batch.sequence, service_fact.sequence,
       path.allocation_index, path.path_index, network.code, connection.code,
       source.code, sink.code, path.hop_count, path.dispatched_units,
       path.network_received_units, path.network_loss_units,
       path.path_cost_units, path.path_hash, path.metadata
FROM city_physical_network_flow_paths path
JOIN city_physical_network_flow_batches batch ON batch.id = path.batch_id
JOIN city_service_facts service_fact ON service_fact.id = path.service_fact_id
JOIN city_physical_networks network ON network.id = batch.network_id
JOIN city_service_definitions service ON service.id = network.service_definition_id
JOIN city_service_connections connection ON connection.id = path.connection_id
JOIN city_physical_network_nodes source ON source.id = path.source_node_id
JOIN city_physical_network_nodes sink ON sink.id = path.sink_node_id
WHERE path.world_id = $1
  AND ($2 = '' OR service.code = $2)
  AND ($3 = '' OR network.code = $3)
  AND (batch.tick > $4 OR (batch.tick = $4 AND batch.sequence >= $5))
  AND (batch.tick < $6 OR (batch.tick = $6 AND batch.sequence <= $7))
ORDER BY batch.tick, batch.sequence, service_fact.sequence,
         path.allocation_index, path.path_index`, input.WorldID,
		input.ServiceCode, input.NetworkCode, first.Tick, first.Sequence,
		last.Tick, last.Sequence)
	if err != nil {
		return fmt.Errorf("list city physical network flow paths: %w", err)
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
			return fmt.Errorf("scan city physical network flow path: %w", err)
		}
		if itemIndex, exists := index[cityServiceRecoveryFactKey{tick: item.Tick, sequence: item.Sequence}]; exists {
			items[itemIndex].Paths = append(items[itemIndex].Paths, item)
		}
	}
	if err = closeCityRows(pathRows, "iterate city physical network flow paths"); err != nil {
		return err
	}
	segmentRows, err := s.db.QueryContext(ctx, `
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
JOIN city_physical_networks network ON network.id = batch.network_id
JOIN city_service_definitions service ON service.id = network.service_definition_id
JOIN city_physical_network_edges edge ON edge.id = segment.edge_id
JOIN city_physical_network_nodes source ON source.id = segment.from_node_id
JOIN city_physical_network_nodes sink ON sink.id = segment.to_node_id
WHERE segment.world_id = $1
  AND ($2 = '' OR service.code = $2)
  AND ($3 = '' OR network.code = $3)
  AND (batch.tick > $4 OR (batch.tick = $4 AND batch.sequence >= $5))
  AND (batch.tick < $6 OR (batch.tick = $6 AND batch.sequence <= $7))
ORDER BY batch.tick, batch.sequence, service_fact.sequence,
         path.allocation_index, path.path_index, segment.segment_index`, input.WorldID,
		input.ServiceCode, input.NetworkCode, first.Tick, first.Sequence,
		last.Tick, last.Sequence)
	if err != nil {
		return fmt.Errorf("list city physical network flow segments: %w", err)
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
			return fmt.Errorf("scan city physical network flow segment: %w", err)
		}
		if itemIndex, exists := index[cityServiceRecoveryFactKey{tick: item.Tick, sequence: item.Sequence}]; exists {
			items[itemIndex].Segments = append(items[itemIndex].Segments, item)
		}
	}
	return closeCityRows(segmentRows, "iterate city physical network flow segments")
}

func (s *CityEconomyService) ListCityPhysicalNetworkFacts(
	ctx context.Context, input CityPhysicalNetworkQueryInput,
) (*CityPhysicalNetworkFactPage, error) {
	if err := normalizeCityPhysicalNetworkQuery(&input, "fact"); err != nil {
		return nil, err
	}
	version, available, err := s.cityPhysicalNetworkQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityPhysicalNetworkFactPage{
		Availability:      CityServiceAvailabilityUnsupported,
		SimulationVersion: version, RequiredVersion: CitySimulationVersionF8V3,
		Items: make([]CityPhysicalNetworkFact, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, fact.phase, command.sequence, fact.fact_type,
       fact.subject_kind, fact.subject_code, fact.version_before,
       fact.version_after, fact.payload
FROM city_physical_network_facts fact
LEFT JOIN city_commands command
  ON command.id = fact.source_command_id AND command.world_id = fact.world_id
WHERE fact.world_id = $1 AND fact.posted_at IS NOT NULL
  AND ($2 = '' OR fact.phase = $2)
  AND ($3 = '' OR fact.fact_type = $3)
  AND (fact.tick > $4 OR (fact.tick = $4 AND fact.sequence > $5))
  AND ($6 = '' OR EXISTS (
      SELECT 1
      FROM city_physical_networks network
      JOIN city_service_definitions service ON service.id = network.service_definition_id
      WHERE network.world_id = fact.world_id AND service.code = $6
        AND (
          (fact.subject_kind IN ('network', 'flow_batch') AND fact.subject_code = network.code)
          OR (fact.subject_kind = 'node' AND EXISTS (
              SELECT 1 FROM city_physical_network_nodes node
              WHERE node.network_id = network.id AND node.code = fact.subject_code
          ))
          OR (fact.subject_kind = 'edge' AND EXISTS (
              SELECT 1 FROM city_physical_network_edges edge
              WHERE edge.network_id = network.id AND edge.code = fact.subject_code
          ))
        )
  ))
  AND ($7 = '' OR EXISTS (
      SELECT 1 FROM city_physical_networks network
      WHERE network.world_id = fact.world_id AND network.code = $7
        AND (
          (fact.subject_kind IN ('network', 'flow_batch') AND fact.subject_code = network.code)
          OR (fact.subject_kind = 'node' AND EXISTS (
              SELECT 1 FROM city_physical_network_nodes node
              WHERE node.network_id = network.id AND node.code = fact.subject_code
          ))
          OR (fact.subject_kind = 'edge' AND EXISTS (
              SELECT 1 FROM city_physical_network_edges edge
              WHERE edge.network_id = network.id AND edge.code = fact.subject_code
          ))
        )
  ))
ORDER BY fact.tick, fact.sequence
LIMIT $8`, input.WorldID, input.Phase, input.FactType,
		input.AfterTick, input.AfterSequence, input.ServiceCode,
		input.NetworkCode, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city physical network facts: %w", err)
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
			return nil, fmt.Errorf("scan city physical network fact: %w", err)
		}
		item.SourceCommandSequence = nullInt64Pointer(commandSequence)
		page.Items = append(page.Items, item)
	}
	if err = closeCityRows(rows, "iterate city physical network facts"); err != nil {
		return nil, err
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = &CityPhysicalNetworkCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func loadCityPhysicalNetworkProfile(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
	profile *CityPhysicalNetworkProfile,
) error {
	if profile == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "physical_network_profile_target"})
	}
	err := queryer.QueryRowContext(ctx, `
SELECT policy_id, policy_version, policy_hash, baseline_tick, policy_count,
       network_count, node_count, edge_count, fact_count, batch_count,
       path_count, segment_count, revision, metadata
FROM city_physical_network_profiles WHERE world_id = $1`, worldID).Scan(
		&profile.PolicyID, &profile.PolicyVersion, &profile.PolicyHash,
		&profile.BaselineTick, &profile.PolicyCount, &profile.NetworkCount,
		&profile.NodeCount, &profile.EdgeCount, &profile.FactCount,
		&profile.BatchCount, &profile.PathCount, &profile.SegmentCount,
		&profile.Revision, &profile.Metadata,
	)
	if err != nil {
		return fmt.Errorf("load city physical network profile: %w", err)
	}
	return nil
}

func loadCityPhysicalNetworkOverview(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
) (*CityPhysicalNetworkOverview, error) {
	item := &CityPhysicalNetworkOverview{}
	var latestTick sql.NullInt64
	err := queryer.QueryRowContext(ctx, `
WITH latest AS (
    SELECT MAX(tick) AS tick
    FROM city_physical_network_flow_batches WHERE world_id = $1
), network_summary AS (
    SELECT COUNT(*) FILTER (WHERE status = 'active')::BIGINT AS active_count
    FROM city_physical_networks WHERE world_id = $1
), node_summary AS (
    SELECT COUNT(*) FILTER (WHERE status = 'active')::BIGINT AS active_count
    FROM city_physical_network_nodes WHERE world_id = $1
), edge_summary AS (
    SELECT COUNT(*) FILTER (WHERE status = 'active')::BIGINT AS active_count,
           COUNT(*) FILTER (WHERE status = 'isolated')::BIGINT AS isolated_count,
           COUNT(*) FILTER (WHERE status = 'failed')::BIGINT AS failed_count,
           COALESCE(SUM(installed_capacity_units), 0)::BIGINT AS installed_units,
           COALESCE(SUM(available_capacity_units), 0)::BIGINT AS available_units
    FROM city_physical_network_edges WHERE world_id = $1
), flow_summary AS (
    SELECT COALESCE(SUM(dispatched_units), 0)::BIGINT AS dispatched_units,
           COALESCE(SUM(network_received_units), 0)::BIGINT AS received_units,
           COALESCE(SUM(network_loss_units), 0)::BIGINT AS loss_units
    FROM city_physical_network_flow_batches batch, latest
    WHERE batch.world_id = $1 AND batch.tick = latest.tick
)
SELECT network_summary.active_count, node_summary.active_count,
       edge_summary.active_count, edge_summary.isolated_count,
       edge_summary.failed_count, edge_summary.installed_units,
       edge_summary.available_units, latest.tick,
       flow_summary.dispatched_units, flow_summary.received_units,
       flow_summary.loss_units,
       CASE WHEN flow_summary.dispatched_units = 0 THEN 1000
            ELSE FLOOR(flow_summary.received_units::NUMERIC * 1000
                       / flow_summary.dispatched_units::NUMERIC)::INTEGER END
FROM network_summary, node_summary, edge_summary, latest, flow_summary`, worldID).Scan(
		&item.ActiveNetworkCount, &item.ActiveNodeCount, &item.ActiveEdgeCount,
		&item.IsolatedEdgeCount, &item.FailedEdgeCount,
		&item.InstalledEdgeCapacityUnits, &item.AvailableEdgeCapacityUnits,
		&latestTick, &item.LatestDispatchedUnits,
		&item.LatestNetworkReceivedUnits, &item.LatestNetworkLossUnits,
		&item.LatestDeliveryRatioMilli,
	)
	if err != nil {
		return nil, fmt.Errorf("load city physical network overview: %w", err)
	}
	if latestTick.Valid {
		item.LatestFlowTick = int64Pointer(latestTick.Int64)
	}
	return item, nil
}

func (s *CityEconomyService) cityPhysicalNetworkQueryAvailability(
	ctx context.Context, input CityPhysicalNetworkQueryInput,
) (string, bool, error) {
	if input.UserID <= 0 || input.WorldID <= 0 {
		return "", false, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return "", false, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `
SELECT simulation_version FROM city_worlds WHERE id = $1`, input.WorldID).Scan(&version); err != nil {
		return "", false, fmt.Errorf("load physical network world version: %w", err)
	}
	return version, cityEngineSupportsPhysicalNetworks(version), nil
}

func normalizeCityPhysicalNetworkQuery(
	input *CityPhysicalNetworkQueryInput, kind string,
) error {
	if input == nil || input.UserID <= 0 || input.WorldID <= 0 ||
		input.AfterTick < 0 || input.AfterSequence < 0 {
		return ErrCityInvalidInput
	}
	input.ServiceCode = strings.ToLower(strings.TrimSpace(input.ServiceCode))
	input.NetworkCode = strings.ToLower(strings.TrimSpace(input.NetworkCode))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.Role = strings.ToLower(strings.TrimSpace(input.Role))
	input.Phase = strings.ToLower(strings.TrimSpace(input.Phase))
	input.FactType = strings.ToLower(strings.TrimSpace(input.FactType))
	input.SourceNodeCode = strings.ToLower(strings.TrimSpace(input.SourceNodeCode))
	input.SinkNodeCode = strings.ToLower(strings.TrimSpace(input.SinkNodeCode))
	input.AfterCode = strings.ToLower(strings.TrimSpace(input.AfterCode))
	for _, value := range []string{
		input.ServiceCode, input.NetworkCode, input.FactType, input.SourceNodeCode,
		input.SinkNodeCode, input.AfterCode,
	} {
		if value != "" && !cityServiceCodePattern.MatchString(value) {
			return ErrCityInvalidInput
		}
	}
	if input.Status != "" && !isCityPhysicalNetworkQueryStatus(kind, input.Status) {
		return ErrCityInvalidInput
	}
	if input.Role != "" && !isCityPhysicalNetworkNodeRole(input.Role) {
		return ErrCityInvalidInput
	}
	if input.Phase != "" && input.Phase != "command" &&
		input.Phase != CityPhysicalNetworkPhasePreNetwork &&
		input.Phase != CityPhysicalNetworkPhaseSettlement {
		return ErrCityInvalidInput
	}
	if (kind != "node" && input.Role != "") ||
		(kind != "fact" && (input.Phase != "" || input.FactType != "")) {
		return ErrCityInvalidInput
	}
	if kind == "diagnostic" {
		if input.NetworkCode == "" || input.ServiceCode != "" || input.Limit != 0 ||
			(input.SourceNodeCode == "") != (input.SinkNodeCode == "") ||
			input.SourceNodeCode == input.SinkNodeCode && input.SourceNodeCode != "" ||
			input.ProbeUnits < 0 || input.ProbeUnits > cityServiceMaximumConfiguredUnits ||
			input.Status != "" || input.Role != "" || input.AfterCode != "" ||
			input.AfterTick != 0 || input.AfterSequence != 0 {
			return ErrCityInvalidInput
		}
	} else if input.SourceNodeCode != "" || input.SinkNodeCode != "" || input.ProbeUnits != 0 {
		return ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityPhysicalNetworkQueryDefaultLimit
	}
	if input.Limit > cityPhysicalNetworkQueryMaximumLimit {
		return ErrCityInvalidInput
	}
	return nil
}

func isCityPhysicalNetworkQueryStatus(kind, status string) bool {
	switch kind {
	case "network":
		return status == CityNetworkStatusActive || status == CityNetworkStatusSuspended ||
			status == CityNetworkStatusRetired
	case "node":
		return status == CityNetworkNodeStatusActive || status == CityNetworkNodeStatusOffline ||
			status == CityNetworkNodeStatusRetired
	case "edge":
		return status == CityNetworkEdgeStatusActive || status == CityNetworkEdgeStatusIsolated ||
			status == CityNetworkEdgeStatusFailed || status == CityNetworkEdgeStatusRetired
	default:
		return false
	}
}

func isCityPhysicalNetworkNodeRole(role string) bool {
	return role == CityNetworkNodeRoleSupply || role == CityNetworkNodeRoleDemand ||
		role == CityNetworkNodeRoleJunction || role == CityNetworkNodeRoleStorage ||
		role == CityNetworkNodeRoleGateway
}

func setCityPhysicalNetworkCodeCursor[T any](
	items *[]T, limit int, next **string, code func(T) string,
) {
	if items == nil || len(*items) <= limit {
		return
	}
	*items = (*items)[:limit]
	value := code((*items)[len(*items)-1])
	*next = &value
}
