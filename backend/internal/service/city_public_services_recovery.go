package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type cityServiceRecoveryFactKey struct {
	tick     int64
	sequence int64
}

func restoreCityPublicServiceProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	state *cityHashState,
	preservedFactIDs map[cityServiceRecoveryFactKey]int64,
) (int, error) {
	if state == nil || state.PublicServices == nil || !cityEngineSupportsPublicServices(state.SimulationVersion) {
		return 0, fmt.Errorf("recovery F8 public-service state is unavailable")
	}
	publicServices := state.PublicServices
	if err := validateCityPublicServiceRecoveryState(
		publicServices, state.CurrentTick, state.SimulationVersion, state.FacilityLifecycle,
	); err != nil {
		return 0, err
	}
	count := 0
	var err error

	serviceIDs := make(map[string]int64, len(publicServices.ServiceDefinitions))
	for _, definition := range publicServices.ServiceDefinitions {
		var id int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO city_service_definitions
    (world_id, code, definition_version, definition_hash, name, category,
     unit_code, flow_kind, status, sort_order, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)
RETURNING id`, worldID, definition.Code, definition.DefinitionVersion,
			definition.DefinitionHash, definition.Name, definition.Category,
			definition.UnitCode, definition.FlowKind, definition.Status,
			definition.SortOrder, definition.Payload).Scan(&id)
		if err != nil {
			return count, fmt.Errorf("restore city service definition %s: %w", definition.Code, err)
		}
		serviceIDs[definition.Code] = id
		count++
	}

	facilityTypeIDs := make(map[string]int64, len(publicServices.FacilityTypes))
	for _, definition := range publicServices.FacilityTypes {
		allowed, marshalErr := json.Marshal(definition.AllowedServiceCodes)
		if marshalErr != nil {
			return count, fmt.Errorf("marshal recovery facility type %s: %w", definition.Code, marshalErr)
		}
		var id int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO city_facility_type_definitions
    (world_id, code, definition_version, definition_hash, name,
     minimum_floor_area_sqm, default_reliability_milli,
     allowed_service_codes, status, sort_order, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11::jsonb)
RETURNING id`, worldID, definition.Code, definition.DefinitionVersion,
			definition.DefinitionHash, definition.Name, definition.MinimumFloorAreaSQM,
			definition.DefaultReliabilityMilli, allowed, definition.Status,
			definition.SortOrder, definition.Payload).Scan(&id)
		if err != nil {
			return count, fmt.Errorf("restore city facility type %s: %w", definition.Code, err)
		}
		facilityTypeIDs[definition.Code] = id
		count++
	}

	factIDs := make(map[cityServiceRecoveryFactKey]int64, len(publicServices.Facts))
	for _, fact := range publicServices.Facts {
		var sourceCommandID any
		if fact.SourceCommandSequence != nil {
			var commandID int64
			if err = tx.QueryRowContext(ctx, `
SELECT id FROM city_commands
WHERE world_id = $1 AND sequence = $2 AND status = 'applied'
  AND processed_tick = $3`, worldID, *fact.SourceCommandSequence, fact.Tick).Scan(&commandID); err != nil {
				return count, fmt.Errorf("resolve recovery city service command %d: %w", *fact.SourceCommandSequence, err)
			}
			sourceCommandID = commandID
		}
		var id int64
		preservedID, preserve := preservedFactIDs[cityServiceRecoveryFactKey{tick: fact.Tick, sequence: fact.Sequence}]
		if preserve {
			err = tx.QueryRowContext(ctx, `
INSERT INTO city_service_facts
    (id, world_id, tick, sequence, source_command_id, fact_type, subject_kind,
     subject_code, version_before, version_after, payload, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, NOW())
RETURNING id`, preservedID, worldID, fact.Tick, fact.Sequence, sourceCommandID,
				fact.FactType, fact.SubjectKind, fact.SubjectCode, fact.VersionBefore,
				fact.VersionAfter, fact.Payload).Scan(&id)
		} else {
			err = tx.QueryRowContext(ctx, `
INSERT INTO city_service_facts
    (world_id, tick, sequence, source_command_id, fact_type, subject_kind,
     subject_code, version_before, version_after, payload, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, NOW())
RETURNING id`, worldID, fact.Tick, fact.Sequence, sourceCommandID, fact.FactType,
				fact.SubjectKind, fact.SubjectCode, fact.VersionBefore, fact.VersionAfter,
				fact.Payload).Scan(&id)
		}
		if err != nil {
			return count, fmt.Errorf("restore city service fact %d/%d: %w", fact.Tick, fact.Sequence, err)
		}
		factIDs[cityServiceRecoveryFactKey{tick: fact.Tick, sequence: fact.Sequence}] = id
		count++
	}

	facilityIDs := make(map[string]int64, len(publicServices.Facilities))
	for _, facility := range publicServices.Facilities {
		typeID, exists := facilityTypeIDs[facility.FacilityTypeCode]
		if !exists {
			return count, fmt.Errorf("recovery facility %s references unknown type", facility.Code)
		}
		factID, exists := factIDs[cityServiceRecoveryFactKey{tick: facility.SourceFactTick, sequence: facility.SourceFactSequence}]
		if !exists {
			return count, fmt.Errorf("recovery facility %s references unknown fact", facility.Code)
		}
		var districtID, buildingID int64
		if err = tx.QueryRowContext(ctx, `
SELECT district.id, building.id
FROM city_buildings building
JOIN city_districts district
  ON district.id = building.district_id AND district.world_id = building.world_id
WHERE building.world_id = $1 AND building.code = $2 AND district.code = $3`,
			worldID, facility.BuildingCode, facility.DistrictCode).Scan(&districtID, &buildingID); err != nil {
			return count, fmt.Errorf("resolve recovery facility building %s: %w", facility.Code, err)
		}
		var ownerID any
		if facility.OwnerEntityCode != nil {
			var id int64
			if err = tx.QueryRowContext(ctx, `
SELECT id FROM city_economic_entities WHERE world_id = $1 AND code = $2`,
				worldID, *facility.OwnerEntityCode).Scan(&id); err != nil {
				return count, fmt.Errorf("resolve recovery facility owner %s: %w", facility.Code, err)
			}
			ownerID = id
		}
		var id int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO city_facilities
    (world_id, code, name, facility_type_id, district_id, building_id,
     owner_entity_id, status, reliability_milli, created_tick, updated_tick,
     version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)
RETURNING id`, worldID, facility.Code, facility.Name, typeID, districtID,
			buildingID, ownerID, facility.Status, facility.ReliabilityMilli,
			facility.CreatedTick, facility.UpdatedTick, facility.Version,
			factID, facility.Metadata).Scan(&id)
		if err != nil {
			return count, fmt.Errorf("restore city facility %s: %w", facility.Code, err)
		}
		facilityIDs[facility.Code] = id
		count++
	}

	capacityIDs := make(map[string]int64, len(publicServices.Capacities))
	for _, capacity := range publicServices.Capacities {
		facilityID, facilityExists := facilityIDs[capacity.FacilityCode]
		serviceID, serviceExists := serviceIDs[capacity.ServiceCode]
		factID, factExists := factIDs[cityServiceRecoveryFactKey{tick: capacity.SourceFactTick, sequence: capacity.SourceFactSequence}]
		if !facilityExists || !serviceExists || !factExists {
			return count, fmt.Errorf("recovery capacity %s/%s references unknown identity", capacity.FacilityCode, capacity.ServiceCode)
		}
		var id int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO city_facility_service_capacities
    (world_id, facility_id, service_definition_id, installed_capacity_units,
     availability_milli, available_capacity_units, updated_tick, version,
     source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)
RETURNING id`, worldID, facilityID, serviceID, capacity.InstalledCapacityUnits,
			capacity.AvailabilityMilli, capacity.AvailableCapacityUnits,
			capacity.UpdatedTick, capacity.Version, factID, capacity.Metadata).Scan(&id)
		if err != nil {
			return count, fmt.Errorf("restore city service capacity %s/%s: %w", capacity.FacilityCode, capacity.ServiceCode, err)
		}
		capacityIDs[cityServiceCapacityRecoveryKey(capacity.FacilityCode, capacity.ServiceCode)] = id
		count++
	}

	demandIDs := make(map[string]int64, len(publicServices.Demands))
	for _, demand := range publicServices.Demands {
		serviceID, serviceExists := serviceIDs[demand.ServiceCode]
		factID, factExists := factIDs[cityServiceRecoveryFactKey{tick: demand.SourceFactTick, sequence: demand.SourceFactSequence}]
		if !serviceExists || !factExists {
			return count, fmt.Errorf("recovery demand %s references unknown service or fact", demand.Code)
		}
		subject, resolveErr := resolveCityServiceRecoverySubject(ctx, tx, worldID, demand)
		if resolveErr != nil || subject.districtCode != demand.DistrictCode ||
			!sameOptionalString(subject.buildingCode, demand.BuildingCode) {
			if resolveErr == nil {
				resolveErr = fmt.Errorf("subject location does not match snapshot")
			}
			return count, fmt.Errorf("resolve recovery city service demand %s: %w", demand.Code, resolveErr)
		}
		var id int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO city_service_demands
    (world_id, code, service_definition_id, subject_kind, subject_code,
     district_id, building_id, entity_id, actor_id, requested_units_per_tick,
     priority, status, created_tick, updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17::jsonb)
RETURNING id`, worldID, demand.Code, serviceID, demand.SubjectKind,
			demand.SubjectCode, subject.districtID, optionalInt64Value(subject.buildingID),
			optionalInt64Value(subject.entityID), optionalInt64Value(subject.actorID),
			demand.RequestedUnitsPerTick, demand.Priority, demand.Status,
			demand.CreatedTick, demand.UpdatedTick, demand.Version, factID,
			demand.Metadata).Scan(&id)
		if err != nil {
			return count, fmt.Errorf("restore city service demand %s: %w", demand.Code, err)
		}
		demandIDs[demand.Code] = id
		count++
	}

	connectionIDs := make(map[string]int64, len(publicServices.Connections))
	for _, connection := range publicServices.Connections {
		capacityID, capacityExists := capacityIDs[cityServiceCapacityRecoveryKey(connection.FacilityCode, connection.ServiceCode)]
		demandID, demandExists := demandIDs[connection.DemandCode]
		factID, factExists := factIDs[cityServiceRecoveryFactKey{tick: connection.SourceFactTick, sequence: connection.SourceFactSequence}]
		if !capacityExists || !demandExists || !factExists {
			return count, fmt.Errorf("recovery connection %s references unknown identity", connection.Code)
		}
		var id int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO city_service_connections
    (world_id, code, capacity_id, demand_id, max_flow_units_per_tick,
     loss_milli, preference, status, created_tick, updated_tick, version,
     source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)
RETURNING id`, worldID, connection.Code, capacityID, demandID,
			connection.MaxFlowUnitsPerTick, connection.LossMilli, connection.Preference,
			connection.Status, connection.CreatedTick, connection.UpdatedTick,
			connection.Version, factID, connection.Metadata).Scan(&id)
		if err != nil {
			return count, fmt.Errorf("restore city service connection %s: %w", connection.Code, err)
		}
		connectionIDs[connection.Code] = id
		count++
	}

	for _, allocation := range publicServices.Allocations {
		factID, factExists := factIDs[cityServiceRecoveryFactKey{tick: allocation.Tick, sequence: allocation.Sequence}]
		serviceID, serviceExists := serviceIDs[allocation.ServiceCode]
		facilityID, facilityExists := facilityIDs[allocation.FacilityCode]
		capacityID, capacityExists := capacityIDs[cityServiceCapacityRecoveryKey(allocation.FacilityCode, allocation.ServiceCode)]
		demandID, demandExists := demandIDs[allocation.DemandCode]
		connectionID, connectionExists := connectionIDs[allocation.ConnectionCode]
		if !factExists || !serviceExists || !facilityExists || !capacityExists || !demandExists || !connectionExists {
			return count, fmt.Errorf("recovery service allocation %d/%d/%d references unknown identity", allocation.Tick, allocation.Sequence, allocation.AllocationIndex)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_service_allocations
    (world_id, tick, sequence, allocation_index, source_fact_id,
     service_definition_id, facility_id, capacity_id, demand_id, connection_id,
     capacity_version, demand_version, connection_version,
     facility_capacity_units, connection_capacity_units, loss_milli,
     dispatched_units, network_received_units, network_loss_units,
     connection_loss_units, network_path_count, delivered_units, loss_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24::jsonb)`, worldID,
			allocation.Tick, allocation.Sequence, allocation.AllocationIndex, factID,
			serviceID, facilityID, capacityID, demandID, connectionID,
			allocation.CapacityVersion, allocation.DemandVersion,
			allocation.ConnectionVersion, allocation.FacilityCapacityUnits,
			allocation.ConnectionCapacityUnits, allocation.LossMilli,
			allocation.DispatchedUnits, allocation.NetworkReceivedUnits,
			allocation.NetworkLossUnits, allocation.ConnectionLossUnits,
			allocation.NetworkPathCount, allocation.DeliveredUnits,
			allocation.LossUnits, allocation.Metadata); err != nil {
			return count, fmt.Errorf("restore city service allocation %d/%d/%d: %w", allocation.Tick, allocation.Sequence, allocation.AllocationIndex, err)
		}
		count++
	}

	for _, settlement := range publicServices.Settlements {
		factID, factExists := factIDs[cityServiceRecoveryFactKey{tick: settlement.Tick, sequence: settlement.Sequence}]
		serviceID, serviceExists := serviceIDs[settlement.ServiceCode]
		demandID, demandExists := demandIDs[settlement.DemandCode]
		if !factExists || !serviceExists || !demandExists {
			return count, fmt.Errorf("recovery service settlement %d/%d references unknown identity", settlement.Tick, settlement.Sequence)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_service_settlements
    (world_id, tick, sequence, source_fact_id, service_definition_id,
     demand_id, demand_version, requested_units, delivered_units,
     shortage_units, allocation_count, quality_milli, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
			worldID, settlement.Tick, settlement.Sequence, factID, serviceID,
			demandID, settlement.DemandVersion, settlement.RequestedUnits,
			settlement.DeliveredUnits, settlement.ShortageUnits,
			settlement.AllocationCount, settlement.QualityMilli,
			settlement.Metadata); err != nil {
			return count, fmt.Errorf("restore city service settlement %d/%d: %w", settlement.Tick, settlement.Sequence, err)
		}
		count++
	}

	profile := publicServices.Profile
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_service_profiles
    (world_id, catalog_id, catalog_version, catalog_hash, settlement_version,
     baseline_tick, service_definition_count, facility_type_count,
     facility_count, capacity_count, demand_count, connection_count,
     fact_count, allocation_count, settlement_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17::jsonb)`, worldID, profile.CatalogID,
		profile.CatalogVersion, profile.CatalogHash, profile.SettlementVersion,
		profile.BaselineTick, profile.ServiceDefinitionCount,
		profile.FacilityTypeCount, profile.FacilityCount, profile.CapacityCount,
		profile.DemandCount, profile.ConnectionCount, profile.FactCount,
		profile.AllocationCount, profile.SettlementCount, profile.Revision,
		profile.Metadata); err != nil {
		return count, fmt.Errorf("restore city service profile: %w", err)
	}
	count++
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_service_foundation($1)`, worldID); err != nil {
		return count, fmt.Errorf("assert recovered city service foundation: %w", err)
	}
	return count, nil
}

func loadCityServiceRecoveryFactIDs(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (map[cityServiceRecoveryFactKey]int64, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, tick, sequence
FROM city_service_facts
WHERE world_id = $1
ORDER BY tick ASC, sequence ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city service recovery fact identities: %w", err)
	}
	identities := make(map[cityServiceRecoveryFactKey]int64)
	for rows.Next() {
		var id int64
		var key cityServiceRecoveryFactKey
		if err = rows.Scan(&id, &key.tick, &key.sequence); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan city service recovery fact identity: %w", err)
		}
		if _, duplicate := identities[key]; duplicate {
			_ = rows.Close()
			return nil, fmt.Errorf("duplicate city service recovery fact identity")
		}
		identities[key] = id
	}
	if err = closeCityRows(rows, "iterate city service recovery fact identities"); err != nil {
		return nil, err
	}
	return identities, nil
}

func clearCityPublicServiceProjection(ctx context.Context, tx *sql.Tx, worldID int64) (int, error) {
	tables := []string{
		"city_service_settlements", "city_service_allocations",
		"city_service_connections", "city_service_demands",
		"city_facility_service_capacities", "city_facilities",
		"city_service_facts", "city_facility_type_definitions",
		"city_service_definitions", "city_service_profiles",
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

func validateCityPublicServiceRecoveryState(
	state *cityPublicServiceHashState,
	currentTick int64,
	simulationVersion string,
	lifecycle *cityFacilityLifecycleHashState,
) error {
	if state == nil || currentTick < 0 || state.Profile.CatalogID != cityServiceCatalogID ||
		state.Profile.CatalogVersion != cityServiceCatalogVersion ||
		state.Profile.SettlementVersion != cityServiceSettlementVersion ||
		state.Profile.BaselineTick > currentTick ||
		state.Profile.ServiceDefinitionCount != int64(len(state.ServiceDefinitions)) ||
		state.Profile.FacilityTypeCount != int64(len(state.FacilityTypes)) ||
		state.Profile.FacilityCount != int64(len(state.Facilities)) ||
		state.Profile.CapacityCount != int64(len(state.Capacities)) ||
		state.Profile.DemandCount != int64(len(state.Demands)) ||
		state.Profile.ConnectionCount != int64(len(state.Connections)) ||
		state.Profile.FactCount != int64(len(state.Facts)) ||
		state.Profile.AllocationCount != int64(len(state.Allocations)) ||
		state.Profile.SettlementCount != int64(len(state.Settlements)) ||
		state.Profile.Revision != state.Profile.FactCount+1 ||
		state.Profile.FacilityCount > cityServiceMaximumProjectionCount ||
		state.Profile.CapacityCount > cityServiceMaximumProjectionCount ||
		state.Profile.DemandCount > cityServiceMaximumProjectionCount ||
		state.Profile.ConnectionCount > cityServiceMaximumProjectionCount {
		return fmt.Errorf("recovery public-service profile is inconsistent")
	}
	_, _, catalogHash, err := cityPublicServiceCatalog()
	if err != nil || state.Profile.CatalogHash != catalogHash {
		return fmt.Errorf("recovery public-service catalog hash is inconsistent")
	}
	serviceCodes := make(map[string]struct{}, len(state.ServiceDefinitions))
	for _, definition := range state.ServiceDefinitions {
		if _, duplicate := serviceCodes[definition.Code]; duplicate ||
			!cityServiceCodePattern.MatchString(definition.Code) || !json.Valid(definition.Payload) {
			return fmt.Errorf("recovery public-service definition is inconsistent")
		}
		serviceCodes[definition.Code] = struct{}{}
	}
	facilityTypeCodes := make(map[string]struct{}, len(state.FacilityTypes))
	for _, definition := range state.FacilityTypes {
		if _, duplicate := facilityTypeCodes[definition.Code]; duplicate ||
			!cityServiceCodePattern.MatchString(definition.Code) || !json.Valid(definition.Payload) {
			return fmt.Errorf("recovery facility type definition is inconsistent")
		}
		facilityTypeCodes[definition.Code] = struct{}{}
		for _, serviceCode := range definition.AllowedServiceCodes {
			if _, exists := serviceCodes[serviceCode]; !exists {
				return fmt.Errorf("recovery facility type references unknown service")
			}
		}
	}
	lastTick, lastSequence := int64(0), int64(0)
	factKeys := make(map[cityServiceRecoveryFactKey]CityServiceFact, len(state.Facts))
	for _, fact := range state.Facts {
		if fact.Tick <= 0 || fact.Tick > currentTick || fact.Sequence <= 0 ||
			(fact.Tick == lastTick && fact.Sequence != lastSequence+1) ||
			(fact.Tick != lastTick && fact.Sequence != 1) || !json.Valid(fact.Payload) {
			return fmt.Errorf("recovery public-service fact sequence is inconsistent")
		}
		key := cityServiceRecoveryFactKey{tick: fact.Tick, sequence: fact.Sequence}
		if _, duplicate := factKeys[key]; duplicate {
			return fmt.Errorf("recovery public-service fact identity is duplicated")
		}
		factKeys[key] = fact
		lastTick, lastSequence = fact.Tick, fact.Sequence
	}
	facilityStatuses := make(map[string]string, len(state.Facilities))
	for _, facility := range state.Facilities {
		_, duplicate := facilityStatuses[facility.Code]
		if _, exists := facilityTypeCodes[facility.FacilityTypeCode]; !exists || duplicate ||
			!cityServiceCodePattern.MatchString(facility.Code) || !isCityFacilityStatus(facility.Status) ||
			facility.ReliabilityMilli < 0 || facility.ReliabilityMilli > 1000 ||
			facility.CreatedTick <= 0 || facility.UpdatedTick < facility.CreatedTick ||
			facility.UpdatedTick > currentTick || facility.Version <= 0 || !json.Valid(facility.Metadata) ||
			!cityServiceRecoveryProjectionFactMatches(factKeys, facility.SourceFactTick,
				facility.SourceFactSequence, "facility", facility.Code, facility.Version) {
			return fmt.Errorf("recovery city facility is inconsistent")
		}
		facilityStatuses[facility.Code] = facility.Status
	}
	lifecycleFactors := make(map[string]int, len(facilityStatuses))
	if cityEngineSupportsFacilityLifecycle(simulationVersion) {
		if lifecycle == nil || len(lifecycle.States) != len(facilityStatuses) {
			return fmt.Errorf("recovery public-service lifecycle state is inconsistent")
		}
		for _, item := range lifecycle.States {
			if _, exists := facilityStatuses[item.FacilityCode]; !exists {
				return fmt.Errorf("recovery public-service lifecycle facility is unknown")
			}
			if _, duplicate := lifecycleFactors[item.FacilityCode]; duplicate ||
				item.EffectiveFactorMilli < 0 || item.EffectiveFactorMilli > 1000 {
				return fmt.Errorf("recovery public-service lifecycle factor is inconsistent")
			}
			lifecycleFactors[item.FacilityCode] = item.EffectiveFactorMilli
		}
	} else if lifecycle != nil {
		return fmt.Errorf("recovery pre-F8.1 public-service state contains lifecycle data")
	}
	capacities := make(map[string]CityFacilityServiceCapacity, len(state.Capacities))
	for _, capacity := range state.Capacities {
		facilityStatus, facilityExists := facilityStatuses[capacity.FacilityCode]
		key := cityServiceCapacityRecoveryKey(capacity.FacilityCode, capacity.ServiceCode)
		_, duplicate := capacities[key]
		expectedDispatch := cityServiceDispatchCapacity(facilityStatus, capacity.AvailableCapacityUnits)
		if cityEngineSupportsFacilityLifecycle(simulationVersion) {
			factor, exists := lifecycleFactors[capacity.FacilityCode]
			if !exists {
				return fmt.Errorf("recovery city service capacity lifecycle factor is unavailable")
			}
			expectedDispatch = cityFacilityEffectiveDispatchCapacity(
				facilityStatus, capacity.AvailableCapacityUnits, factor,
			)
		}
		if _, exists := serviceCodes[capacity.ServiceCode]; !exists || !facilityExists || duplicate ||
			capacity.InstalledCapacityUnits <= 0 || capacity.InstalledCapacityUnits > cityServiceMaximumConfiguredUnits ||
			capacity.AvailabilityMilli < 0 || capacity.AvailabilityMilli > 1000 ||
			capacity.AvailableCapacityUnits != capacity.InstalledCapacityUnits*int64(capacity.AvailabilityMilli)/1000 ||
			capacity.DispatchCapacityUnits != expectedDispatch ||
			capacity.UpdatedTick <= 0 || capacity.UpdatedTick > currentTick ||
			capacity.Version <= 0 || !json.Valid(capacity.Metadata) ||
			!cityServiceRecoveryProjectionFactMatches(factKeys, capacity.SourceFactTick,
				capacity.SourceFactSequence, "capacity", capacity.FacilityCode+"."+capacity.ServiceCode, capacity.Version) {
			return fmt.Errorf("recovery city service capacity is inconsistent")
		}
		capacities[key] = capacity
	}
	demands := make(map[string]CityServiceDemand, len(state.Demands))
	for _, demand := range state.Demands {
		_, duplicate := demands[demand.Code]
		buildingShapeValid := demand.BuildingCode == nil
		if demand.SubjectKind == "building" {
			buildingShapeValid = demand.BuildingCode != nil && *demand.BuildingCode == demand.SubjectCode
		}
		if _, exists := serviceCodes[demand.ServiceCode]; !exists || duplicate ||
			!cityServiceCodePattern.MatchString(demand.Code) ||
			!cityServiceSubjectCodePattern.MatchString(demand.SubjectCode) ||
			!isCityServiceSubjectKind(demand.SubjectKind) || !buildingShapeValid ||
			(demand.SubjectKind == "district" && demand.SubjectCode != demand.DistrictCode) ||
			demand.RequestedUnitsPerTick < 0 || demand.RequestedUnitsPerTick > cityServiceMaximumConfiguredUnits ||
			demand.Priority < 0 || demand.Priority > 1000 || !isCityServiceProjectionStatus(demand.Status) ||
			demand.CreatedTick <= 0 || demand.UpdatedTick < demand.CreatedTick ||
			demand.UpdatedTick > currentTick || demand.Version <= 0 || !json.Valid(demand.Metadata) ||
			!cityServiceRecoveryProjectionFactMatches(factKeys, demand.SourceFactTick,
				demand.SourceFactSequence, "demand", demand.Code, demand.Version) {
			return fmt.Errorf("recovery city service demand is inconsistent")
		}
		demands[demand.Code] = demand
	}
	connectionCodes := make(map[string]struct{}, len(state.Connections))
	for _, connection := range state.Connections {
		capacity, capacityExists := capacities[cityServiceCapacityRecoveryKey(connection.FacilityCode, connection.ServiceCode)]
		demand, demandExists := demands[connection.DemandCode]
		_, duplicate := connectionCodes[connection.Code]
		if duplicate || !capacityExists || !demandExists ||
			capacity.ServiceCode != demand.ServiceCode || connection.ServiceCode != demand.ServiceCode ||
			!cityServiceCodePattern.MatchString(connection.Code) ||
			connection.MaxFlowUnitsPerTick <= 0 || connection.MaxFlowUnitsPerTick > cityServiceMaximumConfiguredUnits ||
			connection.LossMilli < 0 || connection.LossMilli > 999 ||
			connection.Preference < 0 || connection.Preference > 1000 ||
			!isCityServiceProjectionStatus(connection.Status) || connection.CreatedTick <= 0 ||
			connection.UpdatedTick < connection.CreatedTick || connection.UpdatedTick > currentTick ||
			connection.Version <= 0 || !json.Valid(connection.Metadata) ||
			!cityServiceRecoveryProjectionFactMatches(factKeys, connection.SourceFactTick,
				connection.SourceFactSequence, "connection", connection.Code, connection.Version) {
			return fmt.Errorf("recovery city service connection is inconsistent")
		}
		connectionCodes[connection.Code] = struct{}{}
	}
	return nil
}

func cityServiceRecoveryProjectionFactMatches(
	facts map[cityServiceRecoveryFactKey]CityServiceFact,
	tick, sequence int64,
	subjectKind, subjectCode string,
	version int64,
) bool {
	fact, exists := facts[cityServiceRecoveryFactKey{tick: tick, sequence: sequence}]
	return exists && fact.SubjectKind == subjectKind && fact.SubjectCode == subjectCode &&
		fact.VersionAfter == version
}

func cityServiceCapacityRecoveryKey(facilityCode, serviceCode string) string {
	return facilityCode + "\x00" + serviceCode
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func resolveCityServiceRecoverySubject(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	demand CityServiceDemand,
) (*cityServiceSubjectRef, error) {
	ref := &cityServiceSubjectRef{districtCode: demand.DistrictCode}
	if err := queryer.QueryRowContext(ctx, `
SELECT id FROM city_districts WHERE world_id = $1 AND code = $2`,
		worldID, demand.DistrictCode).Scan(&ref.districtID); err != nil {
		return nil, err
	}
	switch demand.SubjectKind {
	case "district":
		if demand.SubjectCode != demand.DistrictCode || demand.BuildingCode != nil {
			return nil, fmt.Errorf("district demand identity is inconsistent")
		}
	case "building":
		if demand.BuildingCode == nil || demand.SubjectCode != *demand.BuildingCode {
			return nil, fmt.Errorf("building demand identity is inconsistent")
		}
		var buildingID int64
		if err := queryer.QueryRowContext(ctx, `
SELECT id FROM city_buildings
WHERE world_id = $1 AND code = $2 AND district_id = $3`,
			worldID, *demand.BuildingCode, ref.districtID).Scan(&buildingID); err != nil {
			return nil, err
		}
		buildingCode := *demand.BuildingCode
		ref.buildingID, ref.buildingCode = &buildingID, &buildingCode
	case "household", "enterprise":
		if demand.BuildingCode != nil {
			return nil, fmt.Errorf("entity demand cannot reference a building")
		}
		entityType := "household"
		if demand.SubjectKind == "enterprise" {
			entityType = "firm"
		}
		var entityID int64
		if err := queryer.QueryRowContext(ctx, `
SELECT id FROM city_economic_entities
WHERE world_id = $1 AND code = $2 AND entity_type = $3`,
			worldID, demand.SubjectCode, entityType).Scan(&entityID); err != nil {
			return nil, err
		}
		ref.entityID = &entityID
	case "actor":
		if demand.BuildingCode != nil {
			return nil, fmt.Errorf("actor demand cannot reference a building")
		}
		var actorID int64
		if err := queryer.QueryRowContext(ctx, `
SELECT id FROM world_actors
WHERE world_id = $1 AND code = $2 AND status = 'active'`,
			worldID, demand.SubjectCode).Scan(&actorID); err != nil {
			return nil, err
		}
		ref.actorID = &actorID
	default:
		return nil, fmt.Errorf("unsupported city service subject kind %q", demand.SubjectKind)
	}
	return ref, nil
}
