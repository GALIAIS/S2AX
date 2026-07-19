package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
)

type cityServiceFacilityFactPayload struct {
	SchemaVersion  int           `json:"schema_version"`
	FacilityBefore *CityFacility `json:"facility_before"`
	FacilityAfter  CityFacility  `json:"facility_after"`
}

type cityServiceCapacityFactPayload struct {
	SchemaVersion  int                          `json:"schema_version"`
	CapacityBefore *CityFacilityServiceCapacity `json:"capacity_before"`
	CapacityAfter  CityFacilityServiceCapacity  `json:"capacity_after"`
}

type cityServiceDemandFactPayload struct {
	SchemaVersion int                `json:"schema_version"`
	DemandBefore  *CityServiceDemand `json:"demand_before"`
	DemandAfter   CityServiceDemand  `json:"demand_after"`
}

type cityServiceConnectionFactPayload struct {
	SchemaVersion    int                    `json:"schema_version"`
	ConnectionBefore *CityServiceConnection `json:"connection_before"`
	ConnectionAfter  CityServiceConnection  `json:"connection_after"`
}

type cityServiceSettlementFactPayload struct {
	SchemaVersion int                     `json:"schema_version"`
	Settlement    CityServiceSettlement   `json:"settlement"`
	Allocations   []CityServiceAllocation `json:"allocations"`
}

func replayCityServiceFacts(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	if state == nil || state.PublicServices == nil || tick <= 0 {
		return fmt.Errorf("public-service replay state is unavailable")
	}
	refreshAllCityServiceDispatchCapacities(state.PublicServices, state.FacilityLifecycle)
	rows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, command.sequence, fact.fact_type,
       fact.subject_kind, fact.subject_code, fact.version_before,
       fact.version_after, fact.payload
FROM city_service_facts fact
LEFT JOIN city_commands command
  ON command.id = fact.source_command_id AND command.world_id = fact.world_id
WHERE fact.world_id = $1 AND fact.tick = $2 AND fact.posted_at IS NOT NULL
ORDER BY fact.sequence ASC`, worldID, tick)
	if err != nil {
		return fmt.Errorf("load replay city service facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	expectedSequence := int64(1)
	for rows.Next() {
		var fact CityServiceFact
		var commandSequence sql.NullInt64
		if err = rows.Scan(
			&fact.Tick, &fact.Sequence, &commandSequence, &fact.FactType,
			&fact.SubjectKind, &fact.SubjectCode, &fact.VersionBefore,
			&fact.VersionAfter, &fact.Payload,
		); err != nil {
			return fmt.Errorf("scan replay city service fact: %w", err)
		}
		if fact.Tick != tick || fact.Sequence != expectedSequence {
			return fmt.Errorf("public-service fact sequence is not contiguous")
		}
		if commandSequence.Valid {
			fact.SourceCommandSequence = int64Pointer(commandSequence.Int64)
		}
		if err = reduceCityServiceFactWithLifecycle(
			state.PublicServices, state.FacilityLifecycle, fact,
		); err != nil {
			return fmt.Errorf("reduce public-service fact %d: %w", fact.Sequence, err)
		}
		expectedSequence++
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate replay city service facts: %w", err)
	}
	sortCityPublicServiceState(state.PublicServices)
	return nil
}

func reduceCityServiceFact(state *cityPublicServiceHashState, fact CityServiceFact) error {
	return reduceCityServiceFactWithLifecycle(state, nil, fact)
}

func reduceCityServiceFactWithLifecycle(
	state *cityPublicServiceHashState,
	lifecycle *cityFacilityLifecycleHashState,
	fact CityServiceFact,
) error {
	if state == nil || fact.Tick <= 0 || fact.Sequence <= 0 || !json.Valid(fact.Payload) ||
		state.Profile.Revision != state.Profile.FactCount+1 {
		return fmt.Errorf("public-service fact or profile is invalid")
	}
	switch fact.FactType {
	case CityServiceFactFacilityRegistered, CityServiceFactFacilityStatusChanged:
		var payload cityServiceFacilityFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode facility fact payload: %w", err)
		}
		if payload.SchemaVersion != 1 {
			return fmt.Errorf("facility fact payload schema is unsupported")
		}
		if err := validateCityServiceProjectionFact(
			fact, "facility", payload.FacilityAfter.Code,
			payload.FacilityAfter.Version, payload.FacilityAfter.SourceFactTick,
			payload.FacilityAfter.SourceFactSequence,
		); err != nil {
			return err
		}
		index := cityServiceFacilityIndex(state.Facilities, payload.FacilityAfter.Code)
		if fact.FactType == CityServiceFactFacilityRegistered {
			if fact.VersionBefore != 0 || index >= 0 || payload.FacilityBefore != nil {
				return fmt.Errorf("facility registration chain is invalid")
			}
			state.Facilities = append(state.Facilities, payload.FacilityAfter)
			state.Profile.FacilityCount++
		} else {
			if index < 0 || payload.FacilityBefore == nil ||
				state.Facilities[index].Version != fact.VersionBefore {
				return fmt.Errorf("facility transition chain is invalid")
			}
			state.Facilities[index] = payload.FacilityAfter
		}
		refreshCityServiceDispatchCapacities(
			state, lifecycle, payload.FacilityAfter.Code, payload.FacilityAfter.Status,
		)
	case CityServiceFactFacilityCapacityConfigured:
		var payload cityServiceCapacityFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode capacity fact payload: %w", err)
		}
		if payload.SchemaVersion != 1 {
			return fmt.Errorf("capacity fact payload schema is unsupported")
		}
		subjectCode := payload.CapacityAfter.FacilityCode + "." + payload.CapacityAfter.ServiceCode
		if err := validateCityServiceProjectionFact(
			fact, "capacity", subjectCode, payload.CapacityAfter.Version,
			payload.CapacityAfter.SourceFactTick, payload.CapacityAfter.SourceFactSequence,
		); err != nil {
			return err
		}
		facilityIndex := cityServiceFacilityIndex(state.Facilities, payload.CapacityAfter.FacilityCode)
		if facilityIndex < 0 || payload.CapacityAfter.DispatchCapacityUnits != cityFacilityEffectiveDispatchCapacity(
			state.Facilities[facilityIndex].Status,
			payload.CapacityAfter.AvailableCapacityUnits,
			cityFacilityLifecycleReplayFactor(lifecycle, payload.CapacityAfter.FacilityCode),
		) {
			return fmt.Errorf("capacity dispatch snapshot is invalid")
		}
		index := cityServiceCapacityIndex(state.Capacities, payload.CapacityAfter.FacilityCode, payload.CapacityAfter.ServiceCode)
		if fact.VersionBefore == 0 {
			if index >= 0 || payload.CapacityBefore != nil {
				return fmt.Errorf("capacity creation chain is invalid")
			}
			state.Capacities = append(state.Capacities, payload.CapacityAfter)
			state.Profile.CapacityCount++
		} else {
			if index < 0 || payload.CapacityBefore == nil || state.Capacities[index].Version != fact.VersionBefore {
				return fmt.Errorf("capacity update chain is invalid")
			}
			state.Capacities[index] = payload.CapacityAfter
		}
	case CityServiceFactDemandConfigured:
		var payload cityServiceDemandFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode demand fact payload: %w", err)
		}
		if payload.SchemaVersion != 1 {
			return fmt.Errorf("demand fact payload schema is unsupported")
		}
		if err := validateCityServiceProjectionFact(
			fact, "demand", payload.DemandAfter.Code, payload.DemandAfter.Version,
			payload.DemandAfter.SourceFactTick, payload.DemandAfter.SourceFactSequence,
		); err != nil {
			return err
		}
		index := cityServiceDemandIndex(state.Demands, payload.DemandAfter.Code)
		if fact.VersionBefore == 0 {
			if index >= 0 || payload.DemandBefore != nil {
				return fmt.Errorf("demand creation chain is invalid")
			}
			state.Demands = append(state.Demands, payload.DemandAfter)
			state.Profile.DemandCount++
		} else {
			if index < 0 || payload.DemandBefore == nil || state.Demands[index].Version != fact.VersionBefore {
				return fmt.Errorf("demand update chain is invalid")
			}
			state.Demands[index] = payload.DemandAfter
		}
	case CityServiceFactConnectionConfigured:
		var payload cityServiceConnectionFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode connection fact payload: %w", err)
		}
		if payload.SchemaVersion != 1 {
			return fmt.Errorf("connection fact payload schema is unsupported")
		}
		if err := validateCityServiceProjectionFact(
			fact, "connection", payload.ConnectionAfter.Code,
			payload.ConnectionAfter.Version, payload.ConnectionAfter.SourceFactTick,
			payload.ConnectionAfter.SourceFactSequence,
		); err != nil {
			return err
		}
		index := cityServiceConnectionIndex(state.Connections, payload.ConnectionAfter.Code)
		if fact.VersionBefore == 0 {
			if index >= 0 || payload.ConnectionBefore != nil {
				return fmt.Errorf("connection creation chain is invalid")
			}
			state.Connections = append(state.Connections, payload.ConnectionAfter)
			state.Profile.ConnectionCount++
		} else {
			if index < 0 || payload.ConnectionBefore == nil || state.Connections[index].Version != fact.VersionBefore {
				return fmt.Errorf("connection update chain is invalid")
			}
			state.Connections[index] = payload.ConnectionAfter
		}
	case CityServiceFactAllocationSettled:
		var payload cityServiceSettlementFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode settlement fact payload: %w", err)
		}
		if payload.SchemaVersion != 1 {
			return fmt.Errorf("settlement fact payload schema is unsupported")
		}
		if fact.SourceCommandSequence != nil || fact.SubjectKind != "settlement" ||
			fact.SubjectCode != payload.Settlement.DemandCode+"."+fmt.Sprint(fact.Tick) ||
			fact.VersionBefore != 0 || fact.VersionAfter != 0 ||
			payload.Settlement.Tick != fact.Tick || payload.Settlement.Sequence != fact.Sequence ||
			payload.Settlement.AllocationCount != len(payload.Allocations) {
			return fmt.Errorf("settlement fact chain is invalid")
		}
		if err := validateCityServiceSettlementReplayPayload(state, payload); err != nil {
			return err
		}
		state.Allocations = append(state.Allocations, payload.Allocations...)
		state.Settlements = append(state.Settlements, payload.Settlement)
		state.Profile.AllocationCount += int64(len(payload.Allocations))
		state.Profile.SettlementCount++
	default:
		return fmt.Errorf("unsupported public-service fact type %q", fact.FactType)
	}
	state.Facts = append(state.Facts, fact)
	state.Profile.FactCount++
	state.Profile.Revision++
	return nil
}

func validateCityServiceSettlementReplayPayload(
	state *cityPublicServiceHashState,
	payload cityServiceSettlementFactPayload,
) error {
	settlement := payload.Settlement
	if len(payload.Allocations) > cityServiceMaximumAllocationsPerSettlement ||
		!json.Valid(settlement.Metadata) || settlement.RequestedUnits < 0 ||
		settlement.DeliveredUnits < 0 || settlement.ShortageUnits < 0 ||
		settlement.DeliveredUnits+settlement.ShortageUnits != settlement.RequestedUnits {
		return fmt.Errorf("settlement balance is invalid")
	}
	quality := 1000
	if settlement.RequestedUnits > 0 {
		quality = int(settlement.DeliveredUnits * 1000 / settlement.RequestedUnits)
	}
	if settlement.QualityMilli != quality {
		return fmt.Errorf("settlement quality is invalid")
	}
	demandIndex := cityServiceDemandIndex(state.Demands, settlement.DemandCode)
	if demandIndex < 0 || state.Demands[demandIndex].ServiceCode != settlement.ServiceCode ||
		state.Demands[demandIndex].Version != settlement.DemandVersion ||
		state.Demands[demandIndex].RequestedUnitsPerTick != settlement.RequestedUnits {
		return fmt.Errorf("settlement demand snapshot is invalid")
	}
	for _, existing := range state.Settlements {
		if existing.Tick == settlement.Tick && existing.DemandCode == settlement.DemandCode {
			return fmt.Errorf("settlement demand tick is duplicated")
		}
	}
	dispatchedByCapacity := make(map[string]int64)
	capacitySnapshot := make(map[string]int64)
	var delivered int64
	for index, allocation := range payload.Allocations {
		if allocation.Tick != settlement.Tick || allocation.Sequence != settlement.Sequence ||
			allocation.AllocationIndex != index+1 || allocation.DemandCode != settlement.DemandCode ||
			allocation.ServiceCode != settlement.ServiceCode || !json.Valid(allocation.Metadata) ||
			allocation.CapacityVersion <= 0 || allocation.DemandVersion != settlement.DemandVersion ||
			allocation.ConnectionVersion <= 0 || allocation.FacilityCapacityUnits <= 0 ||
			allocation.ConnectionCapacityUnits <= 0 || allocation.LossMilli < 0 || allocation.LossMilli > 999 ||
			allocation.DispatchedUnits <= 0 || allocation.DeliveredUnits < 0 || allocation.LossUnits < 0 ||
			allocation.DeliveredUnits+allocation.LossUnits != allocation.DispatchedUnits ||
			!validateCityServiceAllocationLossDecomposition(allocation) {
			return fmt.Errorf("settlement allocation chain is invalid")
		}
		capacityIndex := cityServiceCapacityIndex(state.Capacities, allocation.FacilityCode, allocation.ServiceCode)
		facilityIndex := cityServiceFacilityIndex(state.Facilities, allocation.FacilityCode)
		connectionIndex := cityServiceConnectionIndex(state.Connections, allocation.ConnectionCode)
		if capacityIndex < 0 || facilityIndex < 0 || connectionIndex < 0 {
			return fmt.Errorf("settlement allocation identity is unknown")
		}
		capacity := state.Capacities[capacityIndex]
		connection := state.Connections[connectionIndex]
		if capacity.Version != allocation.CapacityVersion ||
			capacity.DispatchCapacityUnits != allocation.FacilityCapacityUnits ||
			connection.Version != allocation.ConnectionVersion ||
			connection.FacilityCode != allocation.FacilityCode ||
			connection.ServiceCode != allocation.ServiceCode ||
			connection.DemandCode != allocation.DemandCode ||
			connection.MaxFlowUnitsPerTick != allocation.ConnectionCapacityUnits ||
			connection.LossMilli != allocation.LossMilli {
			return fmt.Errorf("settlement allocation projection snapshot is invalid")
		}
		key := cityServiceCapacityRecoveryKey(allocation.FacilityCode, allocation.ServiceCode)
		if existing, exists := capacitySnapshot[key]; exists && existing != allocation.FacilityCapacityUnits {
			return fmt.Errorf("settlement capacity snapshot is inconsistent")
		}
		capacitySnapshot[key] = allocation.FacilityCapacityUnits
		dispatchedByCapacity[key] += allocation.DispatchedUnits
		if dispatchedByCapacity[key] > allocation.FacilityCapacityUnits ||
			allocation.DispatchedUnits > allocation.ConnectionCapacityUnits {
			return fmt.Errorf("settlement allocation exceeds capacity")
		}
		delivered += allocation.DeliveredUnits
	}
	if delivered != settlement.DeliveredUnits {
		return fmt.Errorf("settlement delivered units do not match allocations")
	}
	return nil
}

func validateCityServiceAllocationLossDecomposition(allocation CityServiceAllocation) bool {
	networkFieldCount := 0
	if allocation.NetworkReceivedUnits != nil {
		networkFieldCount++
	}
	if allocation.NetworkLossUnits != nil {
		networkFieldCount++
	}
	if allocation.ConnectionLossUnits != nil {
		networkFieldCount++
	}
	if allocation.NetworkPathCount != nil {
		networkFieldCount++
	}
	if networkFieldCount == 0 {
		return allocation.DeliveredUnits ==
			allocation.DispatchedUnits*int64(1000-allocation.LossMilli)/1000
	}
	if networkFieldCount != 4 || *allocation.NetworkPathCount <= 0 ||
		*allocation.NetworkReceivedUnits <= 0 || *allocation.NetworkLossUnits < 0 ||
		*allocation.ConnectionLossUnits < 0 ||
		allocation.DispatchedUnits != *allocation.NetworkReceivedUnits+*allocation.NetworkLossUnits ||
		*allocation.NetworkReceivedUnits != allocation.DeliveredUnits+*allocation.ConnectionLossUnits ||
		allocation.LossUnits != *allocation.NetworkLossUnits+*allocation.ConnectionLossUnits {
		return false
	}
	return allocation.DeliveredUnits ==
		*allocation.NetworkReceivedUnits*int64(1000-allocation.LossMilli)/1000
}

func validateCityServiceProjectionFact(
	fact CityServiceFact,
	subjectKind, subjectCode string,
	version, sourceTick, sourceSequence int64,
) error {
	if fact.SourceCommandSequence == nil || fact.SubjectKind != subjectKind ||
		fact.SubjectCode != subjectCode || fact.VersionAfter != fact.VersionBefore+1 ||
		version != fact.VersionAfter || sourceTick != fact.Tick || sourceSequence != fact.Sequence {
		return fmt.Errorf("public-service projection fact identity is invalid")
	}
	return nil
}

func cityServiceFacilityIndex(items []CityFacility, code string) int {
	for index := range items {
		if items[index].Code == code {
			return index
		}
	}
	return -1
}

func cityServiceCapacityIndex(items []CityFacilityServiceCapacity, facilityCode, serviceCode string) int {
	for index := range items {
		if items[index].FacilityCode == facilityCode && items[index].ServiceCode == serviceCode {
			return index
		}
	}
	return -1
}

func refreshCityServiceDispatchCapacities(
	state *cityPublicServiceHashState,
	lifecycle *cityFacilityLifecycleHashState,
	facilityCode, facilityStatus string,
) {
	factor := cityFacilityLifecycleReplayFactor(lifecycle, facilityCode)
	for index := range state.Capacities {
		if state.Capacities[index].FacilityCode == facilityCode {
			state.Capacities[index].DispatchCapacityUnits = cityFacilityEffectiveDispatchCapacity(
				facilityStatus, state.Capacities[index].AvailableCapacityUnits, factor,
			)
		}
	}
}

func cityFacilityLifecycleReplayFactor(
	lifecycle *cityFacilityLifecycleHashState, facilityCode string,
) int {
	if lifecycle == nil {
		return 1000
	}
	for index := range lifecycle.States {
		if lifecycle.States[index].FacilityCode == facilityCode {
			return lifecycle.States[index].EffectiveFactorMilli
		}
	}
	return 1000
}

func refreshAllCityServiceDispatchCapacities(
	state *cityPublicServiceHashState, lifecycle *cityFacilityLifecycleHashState,
) {
	if state == nil {
		return
	}
	statuses := make(map[string]string, len(state.Facilities))
	for _, facility := range state.Facilities {
		statuses[facility.Code] = facility.Status
	}
	for index := range state.Capacities {
		capacity := &state.Capacities[index]
		capacity.DispatchCapacityUnits = cityFacilityEffectiveDispatchCapacity(
			statuses[capacity.FacilityCode], capacity.AvailableCapacityUnits,
			cityFacilityLifecycleReplayFactor(lifecycle, capacity.FacilityCode),
		)
	}
}

func cityServiceDemandIndex(items []CityServiceDemand, code string) int {
	for index := range items {
		if items[index].Code == code {
			return index
		}
	}
	return -1
}

func cityServiceConnectionIndex(items []CityServiceConnection, code string) int {
	for index := range items {
		if items[index].Code == code {
			return index
		}
	}
	return -1
}

func sortCityPublicServiceState(state *cityPublicServiceHashState) {
	if state == nil {
		return
	}
	sort.Slice(state.Facilities, func(i, j int) bool { return state.Facilities[i].Code < state.Facilities[j].Code })
	sort.Slice(state.Capacities, func(i, j int) bool {
		if state.Capacities[i].FacilityCode != state.Capacities[j].FacilityCode {
			return state.Capacities[i].FacilityCode < state.Capacities[j].FacilityCode
		}
		return state.Capacities[i].ServiceCode < state.Capacities[j].ServiceCode
	})
	sort.Slice(state.Demands, func(i, j int) bool {
		if state.Demands[i].ServiceCode != state.Demands[j].ServiceCode {
			return state.Demands[i].ServiceCode < state.Demands[j].ServiceCode
		}
		return state.Demands[i].Code < state.Demands[j].Code
	})
	sort.Slice(state.Connections, func(i, j int) bool {
		if state.Connections[i].DemandCode != state.Connections[j].DemandCode {
			return state.Connections[i].DemandCode < state.Connections[j].DemandCode
		}
		return state.Connections[i].Code < state.Connections[j].Code
	})
	sort.Slice(state.Facts, func(i, j int) bool {
		if state.Facts[i].Tick != state.Facts[j].Tick {
			return state.Facts[i].Tick < state.Facts[j].Tick
		}
		return state.Facts[i].Sequence < state.Facts[j].Sequence
	})
	sort.Slice(state.Allocations, func(i, j int) bool {
		if state.Allocations[i].Tick != state.Allocations[j].Tick {
			return state.Allocations[i].Tick < state.Allocations[j].Tick
		}
		if state.Allocations[i].Sequence != state.Allocations[j].Sequence {
			return state.Allocations[i].Sequence < state.Allocations[j].Sequence
		}
		return state.Allocations[i].AllocationIndex < state.Allocations[j].AllocationIndex
	})
	sort.Slice(state.Settlements, func(i, j int) bool {
		if state.Settlements[i].Tick != state.Settlements[j].Tick {
			return state.Settlements[i].Tick < state.Settlements[j].Tick
		}
		return state.Settlements[i].Sequence < state.Settlements[j].Sequence
	})
}
