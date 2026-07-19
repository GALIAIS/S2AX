package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	CityServiceAvailabilityAvailable   = "available"
	CityServiceAvailabilityUnsupported = "unsupported"
)

type CityServiceOverview struct {
	FacilityCount              int64  `json:"facility_count"`
	OperationalFacilityCount   int64  `json:"operational_facility_count"`
	ActiveCapacityCount        int64  `json:"active_capacity_count"`
	DispatchCapacityUnits      int64  `json:"dispatch_capacity_units,string"`
	ActiveDemandCount          int64  `json:"active_demand_count"`
	RequestedUnitsPerTick      int64  `json:"requested_units_per_tick,string"`
	LatestSettlementTick       *int64 `json:"latest_settlement_tick,omitempty"`
	LatestRequestedUnits       int64  `json:"latest_requested_units,string"`
	LatestDeliveredUnits       int64  `json:"latest_delivered_units,string"`
	LatestShortageUnits        int64  `json:"latest_shortage_units,string"`
	LatestWeightedQualityMilli int    `json:"latest_weighted_quality_milli"`
}

type CityServiceCatalogView struct {
	Availability       string                       `json:"availability"`
	SimulationVersion  string                       `json:"simulation_version"`
	RequiredVersion    string                       `json:"required_version"`
	Profile            *CityServiceProfile          `json:"profile,omitempty"`
	Overview           *CityServiceOverview         `json:"overview,omitempty"`
	ServiceDefinitions []CityServiceDefinition      `json:"service_definitions"`
	FacilityTypes      []CityFacilityTypeDefinition `json:"facility_types"`
}

type CityServiceFacilityView struct {
	Facility   CityFacility                  `json:"facility"`
	Capacities []CityFacilityServiceCapacity `json:"capacities"`
}

type CityServiceFacilityPage struct {
	Availability      string                    `json:"availability"`
	SimulationVersion string                    `json:"simulation_version"`
	RequiredVersion   string                    `json:"required_version"`
	Items             []CityServiceFacilityView `json:"items"`
	NextCode          *string                   `json:"next_code,omitempty"`
}

type CityServiceDemandView struct {
	Demand           CityServiceDemand      `json:"demand"`
	LatestSettlement *CityServiceSettlement `json:"latest_settlement,omitempty"`
}

type CityServiceDemandPage struct {
	Availability      string                  `json:"availability"`
	SimulationVersion string                  `json:"simulation_version"`
	RequiredVersion   string                  `json:"required_version"`
	Items             []CityServiceDemandView `json:"items"`
	NextCode          *string                 `json:"next_code,omitempty"`
}

type CityServiceConnectionPage struct {
	Availability      string                  `json:"availability"`
	SimulationVersion string                  `json:"simulation_version"`
	RequiredVersion   string                  `json:"required_version"`
	Items             []CityServiceConnection `json:"items"`
	NextCode          *string                 `json:"next_code,omitempty"`
}

type CityServiceSettlementCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type CityServiceSettlementView struct {
	Settlement  CityServiceSettlement   `json:"settlement"`
	Allocations []CityServiceAllocation `json:"allocations"`
}

type CityServiceSettlementPage struct {
	Availability      string                       `json:"availability"`
	SimulationVersion string                       `json:"simulation_version"`
	RequiredVersion   string                       `json:"required_version"`
	Items             []CityServiceSettlementView  `json:"items"`
	NextCursor        *CityServiceSettlementCursor `json:"next_cursor,omitempty"`
}

func (s *CityEconomyService) GetCityServiceCatalog(
	ctx context.Context,
	input CityServiceQueryInput,
) (*CityServiceCatalogView, error) {
	version, available, err := s.cityServiceQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	view := &CityServiceCatalogView{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion:    CitySimulationVersionF8,
		ServiceDefinitions: make([]CityServiceDefinition, 0),
		FacilityTypes:      make([]CityFacilityTypeDefinition, 0),
	}
	if !available {
		return view, nil
	}
	state := &CityPublicServiceState{
		ServiceDefinitions: make([]CityServiceDefinition, 0),
		FacilityTypes:      make([]CityFacilityTypeDefinition, 0),
	}
	if err = loadCityServiceProfile(ctx, s.db, input.WorldID, &state.Profile); err != nil {
		return nil, err
	}
	if err = loadCityServiceDefinitions(ctx, s.db, input.WorldID, state); err != nil {
		return nil, err
	}
	if err = loadCityFacilityTypes(ctx, s.db, input.WorldID, state); err != nil {
		return nil, err
	}
	overview, err := loadCityServiceOverview(ctx, s.db, input.WorldID)
	if err != nil {
		return nil, err
	}
	view.Availability = CityServiceAvailabilityAvailable
	view.Profile = &state.Profile
	view.Overview = overview
	view.ServiceDefinitions = state.ServiceDefinitions
	view.FacilityTypes = state.FacilityTypes
	return view, nil
}

func (s *CityEconomyService) ListCityServiceFacilities(
	ctx context.Context,
	input CityServiceQueryInput,
) (*CityServiceFacilityPage, error) {
	if err := normalizeCityServiceListQuery(&input, "facility"); err != nil {
		return nil, err
	}
	version, available, err := s.cityServiceQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityServiceFacilityPage{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion: CitySimulationVersionF8, Items: make([]CityServiceFacilityView, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT facility.code, facility.name, type.code, type.definition_version,
       type.definition_hash, district.code, building.code, owner.code,
       facility.status, facility.reliability_milli, facility.created_tick,
       facility.updated_tick, facility.version, fact.tick, fact.sequence,
       facility.metadata
FROM city_facilities facility
JOIN city_facility_type_definitions type ON type.id = facility.facility_type_id
JOIN city_districts district ON district.id = facility.district_id
JOIN city_buildings building ON building.id = facility.building_id
LEFT JOIN city_economic_entities owner ON owner.id = facility.owner_entity_id
JOIN city_service_facts fact ON fact.id = facility.source_fact_id
WHERE facility.world_id = $1
  AND ($2 = '' OR facility.status = $2)
  AND ($3 = '' OR district.code = $3)
  AND ($4 = '' OR EXISTS (
      SELECT 1 FROM city_facility_service_capacities capacity
      JOIN city_service_definitions service ON service.id = capacity.service_definition_id
      WHERE capacity.world_id = facility.world_id
        AND capacity.facility_id = facility.id AND service.code = $4
  ))
  AND ($5 = '' OR facility.code > $5)
ORDER BY facility.code ASC
LIMIT $6`, input.WorldID, input.Status, input.DistrictCode,
		input.ServiceCode, input.AfterCode, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city service facilities: %w", err)
	}
	for rows.Next() {
		var facility CityFacility
		var owner sql.NullString
		if err = rows.Scan(
			&facility.Code, &facility.Name, &facility.FacilityTypeCode,
			&facility.FacilityTypeVersion, &facility.FacilityTypeHash,
			&facility.DistrictCode, &facility.BuildingCode, &owner,
			&facility.Status, &facility.ReliabilityMilli, &facility.CreatedTick,
			&facility.UpdatedTick, &facility.Version, &facility.SourceFactTick,
			&facility.SourceFactSequence, &facility.Metadata,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan city service facility: %w", err)
		}
		facility.OwnerEntityCode = nullStringPointer(owner)
		page.Items = append(page.Items, CityServiceFacilityView{
			Facility: facility, Capacities: make([]CityFacilityServiceCapacity, 0),
		})
	}
	if err = closeCityRows(rows, "iterate city service facilities"); err != nil {
		return nil, err
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		page.NextCode = stringPointer(page.Items[len(page.Items)-1].Facility.Code)
	}
	if len(page.Items) > 0 {
		capacityState := &CityPublicServiceState{Capacities: make([]CityFacilityServiceCapacity, 0)}
		if err = loadCityServiceCapacities(ctx, s.db, input.WorldID, capacityState); err != nil {
			return nil, err
		}
		index := make(map[string]int, len(page.Items))
		for itemIndex := range page.Items {
			index[page.Items[itemIndex].Facility.Code] = itemIndex
		}
		for _, capacity := range capacityState.Capacities {
			if itemIndex, exists := index[capacity.FacilityCode]; exists {
				page.Items[itemIndex].Capacities = append(page.Items[itemIndex].Capacities, capacity)
			}
		}
	}
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) ListCityServiceDemands(
	ctx context.Context,
	input CityServiceQueryInput,
) (*CityServiceDemandPage, error) {
	if err := normalizeCityServiceListQuery(&input, "demand"); err != nil {
		return nil, err
	}
	version, available, err := s.cityServiceQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityServiceDemandPage{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion: CitySimulationVersionF8, Items: make([]CityServiceDemandView, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT demand.code, service.code, service.definition_version, service.definition_hash,
       demand.subject_kind, demand.subject_code, district.code, building.code,
       demand.requested_units_per_tick, demand.priority, demand.status,
       demand.created_tick, demand.updated_tick, demand.version,
       fact.tick, fact.sequence, demand.metadata,
       latest.tick, latest.sequence, latest.demand_version, latest.requested_units,
       latest.delivered_units, latest.shortage_units, latest.allocation_count,
       latest.quality_milli, latest.metadata
FROM city_service_demands demand
JOIN city_service_definitions service ON service.id = demand.service_definition_id
JOIN city_districts district ON district.id = demand.district_id
LEFT JOIN city_buildings building ON building.id = demand.building_id
JOIN city_service_facts fact ON fact.id = demand.source_fact_id
LEFT JOIN LATERAL (
    SELECT settlement.tick, settlement.sequence, settlement.demand_version,
           settlement.requested_units, settlement.delivered_units,
           settlement.shortage_units, settlement.allocation_count,
           settlement.quality_milli, settlement.metadata
    FROM city_service_settlements settlement
    WHERE settlement.world_id = demand.world_id AND settlement.demand_id = demand.id
    ORDER BY settlement.tick DESC, settlement.sequence DESC LIMIT 1
) latest ON TRUE
WHERE demand.world_id = $1
  AND ($2 = '' OR demand.status = $2)
  AND ($3 = '' OR district.code = $3)
  AND ($4 = '' OR service.code = $4)
  AND ($5 = '' OR demand.code > $5)
ORDER BY demand.code ASC
LIMIT $6`, input.WorldID, input.Status, input.DistrictCode,
		input.ServiceCode, input.AfterCode, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city service demands: %w", err)
	}
	for rows.Next() {
		var demand CityServiceDemand
		var building sql.NullString
		var latestTick, latestSequence, latestDemandVersion sql.NullInt64
		var latestRequested, latestDelivered, latestShortage sql.NullInt64
		var latestAllocationCount, latestQuality sql.NullInt64
		var latestMetadata sql.NullString
		if err = rows.Scan(
			&demand.Code, &demand.ServiceCode, &demand.ServiceVersion,
			&demand.ServiceHash, &demand.SubjectKind, &demand.SubjectCode,
			&demand.DistrictCode, &building, &demand.RequestedUnitsPerTick,
			&demand.Priority, &demand.Status, &demand.CreatedTick,
			&demand.UpdatedTick, &demand.Version, &demand.SourceFactTick,
			&demand.SourceFactSequence, &demand.Metadata, &latestTick,
			&latestSequence, &latestDemandVersion, &latestRequested,
			&latestDelivered, &latestShortage, &latestAllocationCount,
			&latestQuality, &latestMetadata,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan city service demand: %w", err)
		}
		demand.BuildingCode = nullStringPointer(building)
		view := CityServiceDemandView{Demand: demand}
		if latestTick.Valid {
			view.LatestSettlement = &CityServiceSettlement{
				Tick: latestTick.Int64, Sequence: latestSequence.Int64,
				ServiceCode: demand.ServiceCode, DemandCode: demand.Code,
				DemandVersion:  latestDemandVersion.Int64,
				RequestedUnits: latestRequested.Int64, DeliveredUnits: latestDelivered.Int64,
				ShortageUnits:   latestShortage.Int64,
				AllocationCount: int(latestAllocationCount.Int64),
				QualityMilli:    int(latestQuality.Int64), Metadata: json.RawMessage(latestMetadata.String),
			}
		}
		page.Items = append(page.Items, view)
	}
	if err = closeCityRows(rows, "iterate city service demands"); err != nil {
		return nil, err
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		page.NextCode = stringPointer(page.Items[len(page.Items)-1].Demand.Code)
	}
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) ListCityServiceConnections(
	ctx context.Context,
	input CityServiceQueryInput,
) (*CityServiceConnectionPage, error) {
	if err := normalizeCityServiceListQuery(&input, "connection"); err != nil {
		return nil, err
	}
	version, available, err := s.cityServiceQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityServiceConnectionPage{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion: CitySimulationVersionF8, Items: make([]CityServiceConnection, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT connection.code, facility.code, service.code, demand.code,
       connection.max_flow_units_per_tick, connection.loss_milli,
       connection.preference, connection.status, connection.created_tick,
       connection.updated_tick, connection.version, fact.tick, fact.sequence,
       connection.metadata
FROM city_service_connections connection
JOIN city_facility_service_capacities capacity ON capacity.id = connection.capacity_id
JOIN city_facilities facility ON facility.id = capacity.facility_id
JOIN city_service_definitions service ON service.id = capacity.service_definition_id
JOIN city_service_demands demand ON demand.id = connection.demand_id
JOIN city_service_facts fact ON fact.id = connection.source_fact_id
WHERE connection.world_id = $1
  AND ($2 = '' OR connection.status = $2)
  AND ($3 = '' OR service.code = $3)
  AND ($4 = '' OR facility.code = $4)
  AND ($5 = '' OR demand.code = $5)
  AND ($6 = '' OR connection.code > $6)
ORDER BY connection.code ASC
LIMIT $7`, input.WorldID, input.Status, input.ServiceCode, input.FacilityCode,
		input.DemandCode, input.AfterCode, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city service connections: %w", err)
	}
	for rows.Next() {
		var item CityServiceConnection
		if err = rows.Scan(
			&item.Code, &item.FacilityCode, &item.ServiceCode, &item.DemandCode,
			&item.MaxFlowUnitsPerTick, &item.LossMilli, &item.Preference, &item.Status,
			&item.CreatedTick, &item.UpdatedTick, &item.Version,
			&item.SourceFactTick, &item.SourceFactSequence, &item.Metadata,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan city service connection: %w", err)
		}
		page.Items = append(page.Items, item)
	}
	if err = closeCityRows(rows, "iterate city service connections"); err != nil {
		return nil, err
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		page.NextCode = stringPointer(page.Items[len(page.Items)-1].Code)
	}
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) ListCityServiceSettlements(
	ctx context.Context,
	input CityServiceQueryInput,
) (*CityServiceSettlementPage, error) {
	if err := normalizeCityServiceListQuery(&input, "settlement"); err != nil ||
		input.AfterTick < 0 || input.AfterSequence < 0 ||
		(input.AfterTick == 0 && input.AfterSequence != 0) {
		return nil, ErrCityInvalidInput
	}
	version, available, err := s.cityServiceQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityServiceSettlementPage{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion: CitySimulationVersionF8, Items: make([]CityServiceSettlementView, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT settlement.tick, settlement.sequence, service.code, demand.code,
       settlement.demand_version, settlement.requested_units,
       settlement.delivered_units, settlement.shortage_units,
       settlement.allocation_count, settlement.quality_milli, settlement.metadata
FROM city_service_settlements settlement
JOIN city_service_definitions service ON service.id = settlement.service_definition_id
JOIN city_service_demands demand ON demand.id = settlement.demand_id
WHERE settlement.world_id = $1
  AND ($2 = '' OR service.code = $2)
  AND ($3 = '' OR demand.code = $3)
  AND (settlement.tick > $4 OR
       (settlement.tick = $4 AND settlement.sequence > $5))
ORDER BY settlement.tick ASC, settlement.sequence ASC
LIMIT $6`, input.WorldID, input.ServiceCode, input.DemandCode,
		input.AfterTick, input.AfterSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city service settlements: %w", err)
	}
	for rows.Next() {
		var item CityServiceSettlement
		if err = rows.Scan(
			&item.Tick, &item.Sequence, &item.ServiceCode, &item.DemandCode,
			&item.DemandVersion, &item.RequestedUnits, &item.DeliveredUnits,
			&item.ShortageUnits, &item.AllocationCount, &item.QualityMilli,
			&item.Metadata,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan city service settlement: %w", err)
		}
		page.Items = append(page.Items, CityServiceSettlementView{
			Settlement: item, Allocations: make([]CityServiceAllocation, 0),
		})
	}
	if err = closeCityRows(rows, "iterate city service settlements"); err != nil {
		return nil, err
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		last := page.Items[len(page.Items)-1].Settlement
		page.NextCursor = &CityServiceSettlementCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	if len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1].Settlement
		allocationRows, allocationErr := s.db.QueryContext(ctx, `
SELECT allocation.tick, allocation.sequence, allocation.allocation_index,
       service.code, facility.code, demand.code, connection.code,
       allocation.capacity_version, allocation.demand_version,
       allocation.connection_version, allocation.facility_capacity_units,
       allocation.connection_capacity_units, allocation.loss_milli,
       allocation.dispatched_units, allocation.network_received_units,
       allocation.network_loss_units, allocation.connection_loss_units,
       allocation.network_path_count, allocation.delivered_units,
       allocation.loss_units, allocation.metadata
FROM city_service_allocations allocation
JOIN city_service_definitions service ON service.id = allocation.service_definition_id
JOIN city_facilities facility ON facility.id = allocation.facility_id
JOIN city_service_demands demand ON demand.id = allocation.demand_id
JOIN city_service_connections connection ON connection.id = allocation.connection_id
WHERE allocation.world_id = $1
  AND ($2 = '' OR service.code = $2)
  AND ($3 = '' OR demand.code = $3)
  AND (allocation.tick > $4 OR
       (allocation.tick = $4 AND allocation.sequence > $5))
  AND (allocation.tick < $6 OR
       (allocation.tick = $6 AND allocation.sequence <= $7))
ORDER BY allocation.tick ASC, allocation.sequence ASC, allocation.allocation_index ASC`,
			input.WorldID, input.ServiceCode, input.DemandCode,
			input.AfterTick, input.AfterSequence, last.Tick, last.Sequence)
		if allocationErr != nil {
			return nil, fmt.Errorf("list city service settlement allocations: %w", allocationErr)
		}
		bySettlement := make(map[cityServiceRecoveryFactKey]int, len(page.Items))
		for index := range page.Items {
			settlement := page.Items[index].Settlement
			bySettlement[cityServiceRecoveryFactKey{tick: settlement.Tick, sequence: settlement.Sequence}] = index
		}
		for allocationRows.Next() {
			var item CityServiceAllocation
			var networkReceived, networkLoss, connectionLoss sql.NullInt64
			var networkPathCount sql.NullInt32
			if err = allocationRows.Scan(
				&item.Tick, &item.Sequence, &item.AllocationIndex, &item.ServiceCode,
				&item.FacilityCode, &item.DemandCode, &item.ConnectionCode,
				&item.CapacityVersion, &item.DemandVersion, &item.ConnectionVersion,
				&item.FacilityCapacityUnits, &item.ConnectionCapacityUnits,
				&item.LossMilli, &item.DispatchedUnits, &networkReceived,
				&networkLoss, &connectionLoss, &networkPathCount, &item.DeliveredUnits,
				&item.LossUnits, &item.Metadata,
			); err != nil {
				_ = allocationRows.Close()
				return nil, fmt.Errorf("scan city service settlement allocation: %w", err)
			}
			item.NetworkReceivedUnits = nullInt64Pointer(networkReceived)
			item.NetworkLossUnits = nullInt64Pointer(networkLoss)
			item.ConnectionLossUnits = nullInt64Pointer(connectionLoss)
			if networkPathCount.Valid {
				value := int(networkPathCount.Int32)
				item.NetworkPathCount = &value
			}
			if index, exists := bySettlement[cityServiceRecoveryFactKey{tick: item.Tick, sequence: item.Sequence}]; exists {
				page.Items[index].Allocations = append(page.Items[index].Allocations, item)
			}
		}
		if err = closeCityRows(allocationRows, "iterate city service settlement allocations"); err != nil {
			return nil, err
		}
	}
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func loadCityServiceResultsForTick(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
) ([]CityServiceFact, []CityServiceAllocation, []CityServiceSettlement, error) {
	facts := make([]CityServiceFact, 0)
	factRows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, command.sequence, fact.fact_type,
       fact.subject_kind, fact.subject_code, fact.version_before,
       fact.version_after, fact.payload
FROM city_service_facts fact
LEFT JOIN city_commands command
  ON command.id = fact.source_command_id AND command.world_id = fact.world_id
WHERE fact.world_id = $1 AND fact.tick = $2 AND fact.posted_at IS NOT NULL
ORDER BY fact.sequence ASC`, worldID, tick)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load city service tick facts: %w", err)
	}
	for factRows.Next() {
		var item CityServiceFact
		var commandSequence sql.NullInt64
		if err = factRows.Scan(
			&item.Tick, &item.Sequence, &commandSequence, &item.FactType,
			&item.SubjectKind, &item.SubjectCode, &item.VersionBefore,
			&item.VersionAfter, &item.Payload,
		); err != nil {
			_ = factRows.Close()
			return nil, nil, nil, fmt.Errorf("scan city service tick fact: %w", err)
		}
		item.SourceCommandSequence = nullInt64Pointer(commandSequence)
		facts = append(facts, item)
	}
	if err = closeCityRows(factRows, "iterate city service tick facts"); err != nil {
		return nil, nil, nil, err
	}

	allocations := make([]CityServiceAllocation, 0)
	allocationRows, err := queryer.QueryContext(ctx, `
SELECT allocation.tick, allocation.sequence, allocation.allocation_index,
       service.code, facility.code, demand.code, connection.code,
       allocation.capacity_version, allocation.demand_version,
       allocation.connection_version, allocation.facility_capacity_units,
       allocation.connection_capacity_units, allocation.loss_milli,
       allocation.dispatched_units, allocation.network_received_units,
       allocation.network_loss_units, allocation.connection_loss_units,
       allocation.network_path_count, allocation.delivered_units,
       allocation.loss_units, allocation.metadata
FROM city_service_allocations allocation
JOIN city_service_definitions service ON service.id = allocation.service_definition_id
JOIN city_facilities facility ON facility.id = allocation.facility_id
JOIN city_service_demands demand ON demand.id = allocation.demand_id
JOIN city_service_connections connection ON connection.id = allocation.connection_id
WHERE allocation.world_id = $1 AND allocation.tick = $2
ORDER BY allocation.sequence ASC, allocation.allocation_index ASC`, worldID, tick)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load city service tick allocations: %w", err)
	}
	for allocationRows.Next() {
		var item CityServiceAllocation
		var networkReceived, networkLoss, connectionLoss sql.NullInt64
		var networkPathCount sql.NullInt32
		if err = allocationRows.Scan(
			&item.Tick, &item.Sequence, &item.AllocationIndex, &item.ServiceCode,
			&item.FacilityCode, &item.DemandCode, &item.ConnectionCode,
			&item.CapacityVersion, &item.DemandVersion, &item.ConnectionVersion,
			&item.FacilityCapacityUnits, &item.ConnectionCapacityUnits,
			&item.LossMilli, &item.DispatchedUnits, &networkReceived,
			&networkLoss, &connectionLoss, &networkPathCount, &item.DeliveredUnits,
			&item.LossUnits, &item.Metadata,
		); err != nil {
			_ = allocationRows.Close()
			return nil, nil, nil, fmt.Errorf("scan city service tick allocation: %w", err)
		}
		item.NetworkReceivedUnits = nullInt64Pointer(networkReceived)
		item.NetworkLossUnits = nullInt64Pointer(networkLoss)
		item.ConnectionLossUnits = nullInt64Pointer(connectionLoss)
		if networkPathCount.Valid {
			value := int(networkPathCount.Int32)
			item.NetworkPathCount = &value
		}
		allocations = append(allocations, item)
	}
	if err = closeCityRows(allocationRows, "iterate city service tick allocations"); err != nil {
		return nil, nil, nil, err
	}

	settlements := make([]CityServiceSettlement, 0)
	settlementRows, err := queryer.QueryContext(ctx, `
SELECT settlement.tick, settlement.sequence, service.code, demand.code,
       settlement.demand_version, settlement.requested_units,
       settlement.delivered_units, settlement.shortage_units,
       settlement.allocation_count, settlement.quality_milli, settlement.metadata
FROM city_service_settlements settlement
JOIN city_service_definitions service ON service.id = settlement.service_definition_id
JOIN city_service_demands demand ON demand.id = settlement.demand_id
WHERE settlement.world_id = $1 AND settlement.tick = $2
ORDER BY settlement.sequence ASC`, worldID, tick)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load city service tick settlements: %w", err)
	}
	for settlementRows.Next() {
		var item CityServiceSettlement
		if err = settlementRows.Scan(
			&item.Tick, &item.Sequence, &item.ServiceCode, &item.DemandCode,
			&item.DemandVersion, &item.RequestedUnits, &item.DeliveredUnits,
			&item.ShortageUnits, &item.AllocationCount, &item.QualityMilli,
			&item.Metadata,
		); err != nil {
			_ = settlementRows.Close()
			return nil, nil, nil, fmt.Errorf("scan city service tick settlement: %w", err)
		}
		settlements = append(settlements, item)
	}
	if err = closeCityRows(settlementRows, "iterate city service tick settlements"); err != nil {
		return nil, nil, nil, err
	}
	return facts, allocations, settlements, nil
}

func (s *CityEconomyService) cityServiceQueryAvailability(
	ctx context.Context,
	input CityServiceQueryInput,
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
		return "", false, fmt.Errorf("load city service world version: %w", err)
	}
	return version, cityEngineSupportsPublicServices(version), nil
}

func normalizeCityServiceListQuery(input *CityServiceQueryInput, kind string) error {
	if input == nil || input.UserID <= 0 || input.WorldID <= 0 {
		return ErrCityInvalidInput
	}
	input.ServiceCode = strings.ToLower(strings.TrimSpace(input.ServiceCode))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.DistrictCode = strings.ToLower(strings.TrimSpace(input.DistrictCode))
	input.FacilityCode = strings.ToLower(strings.TrimSpace(input.FacilityCode))
	input.DemandCode = strings.ToLower(strings.TrimSpace(input.DemandCode))
	input.AfterCode = strings.ToLower(strings.TrimSpace(input.AfterCode))
	for _, value := range []string{
		input.ServiceCode, input.DistrictCode, input.FacilityCode,
		input.DemandCode, input.AfterCode,
	} {
		if value != "" && !cityServiceCodePattern.MatchString(value) {
			return ErrCityInvalidInput
		}
	}
	if input.Status != "" {
		valid := false
		if kind == "facility" {
			valid = isCityFacilityStatus(input.Status)
		} else if kind == "demand" || kind == "connection" {
			valid = isCityServiceProjectionStatus(input.Status)
		}
		if !valid {
			return ErrCityInvalidInput
		}
	}
	if input.Limit <= 0 {
		input.Limit = cityServiceDefaultLimit
	}
	if input.Limit > cityServiceMaximumLimit {
		return ErrCityInvalidInput
	}
	return nil
}

func loadCityServiceProfile(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	profile *CityServiceProfile,
) error {
	if profile == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_profile_target"})
	}
	err := queryer.QueryRowContext(ctx, `
SELECT catalog_id, catalog_version, catalog_hash, settlement_version,
       baseline_tick, service_definition_count, facility_type_count,
       facility_count, capacity_count, demand_count, connection_count,
       fact_count, allocation_count, settlement_count, revision, metadata
FROM city_service_profiles WHERE world_id = $1`, worldID).Scan(
		&profile.CatalogID, &profile.CatalogVersion, &profile.CatalogHash,
		&profile.SettlementVersion, &profile.BaselineTick,
		&profile.ServiceDefinitionCount, &profile.FacilityTypeCount,
		&profile.FacilityCount, &profile.CapacityCount, &profile.DemandCount,
		&profile.ConnectionCount, &profile.FactCount, &profile.AllocationCount,
		&profile.SettlementCount, &profile.Revision, &profile.Metadata,
	)
	if err != nil {
		return fmt.Errorf("load city service profile: %w", err)
	}
	return nil
}

func loadCityServiceOverview(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityServiceOverview, error) {
	view := &CityServiceOverview{}
	var latestTick sql.NullInt64
	err := queryer.QueryRowContext(ctx, `
WITH latest AS (
    SELECT MAX(tick) AS tick FROM city_service_settlements WHERE world_id = $1
), facility_summary AS (
    SELECT COUNT(*)::BIGINT AS facility_count,
           COUNT(*) FILTER (WHERE status = 'operational')::BIGINT AS operational_count
    FROM city_facilities WHERE world_id = $1
), capacity_summary AS (
    SELECT COUNT(*) FILTER (
               WHERE facility.status IN ('operational', 'degraded')
                 AND COALESCE(lifecycle.effective_factor_milli, 1000) > 0
           )::BIGINT AS active_count,
           COALESCE(SUM(CASE WHEN facility.status IN ('operational', 'degraded')
                       THEN FLOOR(
                           capacity.available_capacity_units::NUMERIC
                           * COALESCE(lifecycle.effective_factor_milli, 1000)::NUMERIC
                           / 1000
                       )::BIGINT ELSE 0 END), 0)::BIGINT AS dispatch_units
    FROM city_facility_service_capacities capacity
    JOIN city_facilities facility ON facility.id = capacity.facility_id
    LEFT JOIN city_facility_lifecycle_states lifecycle
      ON lifecycle.world_id = capacity.world_id
     AND lifecycle.facility_id = capacity.facility_id
    WHERE capacity.world_id = $1
), demand_summary AS (
    SELECT COUNT(*) FILTER (WHERE status = 'active')::BIGINT AS active_count,
           COALESCE(SUM(requested_units_per_tick) FILTER (WHERE status = 'active'), 0)::BIGINT AS requested_units
    FROM city_service_demands WHERE world_id = $1
), settlement_summary AS (
    SELECT COALESCE(SUM(requested_units), 0)::BIGINT AS requested_units,
           COALESCE(SUM(delivered_units), 0)::BIGINT AS delivered_units,
           COALESCE(SUM(shortage_units), 0)::BIGINT AS shortage_units,
           CASE
             WHEN MAX(latest.tick) IS NULL THEN 0
             WHEN COALESCE(SUM(requested_units), 0) = 0 THEN 1000
             ELSE FLOOR(
               SUM(delivered_units)::NUMERIC * 1000 / SUM(requested_units)::NUMERIC
             )::INTEGER
           END AS weighted_quality_milli
    FROM city_service_settlements settlement, latest
    WHERE settlement.world_id = $1 AND settlement.tick = latest.tick
)
SELECT facility_summary.facility_count, facility_summary.operational_count,
       capacity_summary.active_count, capacity_summary.dispatch_units,
       demand_summary.active_count, demand_summary.requested_units, latest.tick,
       settlement_summary.requested_units, settlement_summary.delivered_units,
       settlement_summary.shortage_units, settlement_summary.weighted_quality_milli
FROM facility_summary, capacity_summary, demand_summary, latest, settlement_summary`, worldID).Scan(
		&view.FacilityCount, &view.OperationalFacilityCount,
		&view.ActiveCapacityCount, &view.DispatchCapacityUnits,
		&view.ActiveDemandCount, &view.RequestedUnitsPerTick, &latestTick,
		&view.LatestRequestedUnits, &view.LatestDeliveredUnits,
		&view.LatestShortageUnits, &view.LatestWeightedQualityMilli,
	)
	if err != nil {
		return nil, fmt.Errorf("load city service overview: %w", err)
	}
	if latestTick.Valid {
		view.LatestSettlementTick = int64Pointer(latestTick.Int64)
	}
	return view, nil
}
