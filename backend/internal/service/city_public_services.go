package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CityCommandTypeFacilityRegister            = "facility.register"
	CityCommandTypeFacilityStatusTransition    = "facility.status.transition"
	CityCommandTypeFacilityCapacityConfigure   = "facility.capacity.configure"
	CityCommandTypeServiceDemandConfigure      = "service.demand.configure"
	CityCommandTypeServiceConnectionConfigure  = "service.connection.configure"
	CityServiceFactFacilityRegistered          = "facility.registered"
	CityServiceFactFacilityStatusChanged       = "facility.status.changed"
	CityServiceFactFacilityCapacityConfigured  = "facility.capacity.configured"
	CityServiceFactDemandConfigured            = "service.demand.configured"
	CityServiceFactConnectionConfigured        = "service.connection.configured"
	CityServiceFactAllocationSettled           = "service.allocation.settled"
	CityFacilityStatusOffline                  = "offline"
	CityFacilityStatusOperational              = "operational"
	CityFacilityStatusDegraded                 = "degraded"
	CityFacilityStatusRetired                  = "retired"
	CityServiceProjectionStatusActive          = "active"
	CityServiceProjectionStatusSuspended       = "suspended"
	CityServiceProjectionStatusRetired         = "retired"
	cityServiceCatalogID                       = "sub2api-public-services"
	cityServiceCatalogVersion                  = "1.0.0"
	cityServiceSettlementVersion               = "1.0.0"
	cityServiceDefaultLimit                    = 100
	cityServiceMaximumLimit                    = 500
	cityServiceMaximumMetadataBytes            = 32 * 1024
	cityServiceMaximumSettlementFactsPerTick   = 10000
	cityServiceMaximumAllocationsPerSettlement = 1024
)

var (
	cityServiceCodePattern        = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,95}$`)
	cityServiceSubjectCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,127}$`)

	ErrCityServiceStateNotFound = infraerrors.NotFound(
		"CITY_SERVICE_STATE_NOT_FOUND", "city public service state not found",
	)
	ErrCityServiceDefinitionNotFound = infraerrors.NotFound(
		"CITY_SERVICE_DEFINITION_NOT_FOUND", "city service definition not found",
	)
	ErrCityFacilityTypeNotFound = infraerrors.NotFound(
		"CITY_FACILITY_TYPE_NOT_FOUND", "city facility type not found",
	)
)

type CityServiceProfile struct {
	CatalogID              string          `json:"catalog_id"`
	CatalogVersion         string          `json:"catalog_version"`
	CatalogHash            string          `json:"catalog_hash"`
	SettlementVersion      string          `json:"settlement_version"`
	BaselineTick           int64           `json:"baseline_tick"`
	ServiceDefinitionCount int64           `json:"service_definition_count"`
	FacilityTypeCount      int64           `json:"facility_type_count"`
	FacilityCount          int64           `json:"facility_count"`
	CapacityCount          int64           `json:"capacity_count"`
	DemandCount            int64           `json:"demand_count"`
	ConnectionCount        int64           `json:"connection_count"`
	FactCount              int64           `json:"fact_count"`
	AllocationCount        int64           `json:"allocation_count"`
	SettlementCount        int64           `json:"settlement_count"`
	Revision               int64           `json:"revision"`
	Metadata               json.RawMessage `json:"metadata"`
}

type CityServiceDefinition struct {
	Code              string          `json:"code"`
	DefinitionVersion string          `json:"definition_version"`
	DefinitionHash    string          `json:"definition_hash"`
	Name              string          `json:"name"`
	Category          string          `json:"category"`
	UnitCode          string          `json:"unit_code"`
	FlowKind          string          `json:"flow_kind"`
	Status            string          `json:"status"`
	SortOrder         int             `json:"sort_order"`
	Payload           json.RawMessage `json:"payload"`
}

type CityFacilityTypeDefinition struct {
	Code                    string          `json:"code"`
	DefinitionVersion       string          `json:"definition_version"`
	DefinitionHash          string          `json:"definition_hash"`
	Name                    string          `json:"name"`
	MinimumFloorAreaSQM     int64           `json:"minimum_floor_area_sqm"`
	DefaultReliabilityMilli int             `json:"default_reliability_milli"`
	AllowedServiceCodes     []string        `json:"allowed_service_codes"`
	Status                  string          `json:"status"`
	SortOrder               int             `json:"sort_order"`
	Payload                 json.RawMessage `json:"payload"`
}

type CityFacility struct {
	Code                string          `json:"code"`
	Name                string          `json:"name"`
	FacilityTypeCode    string          `json:"facility_type_code"`
	FacilityTypeVersion string          `json:"facility_type_version"`
	FacilityTypeHash    string          `json:"facility_type_hash"`
	DistrictCode        string          `json:"district_code"`
	BuildingCode        string          `json:"building_code"`
	OwnerEntityCode     *string         `json:"owner_entity_code,omitempty"`
	Status              string          `json:"status"`
	ReliabilityMilli    int             `json:"reliability_milli"`
	CreatedTick         int64           `json:"created_tick"`
	UpdatedTick         int64           `json:"updated_tick"`
	Version             int64           `json:"version"`
	SourceFactTick      int64           `json:"source_fact_tick"`
	SourceFactSequence  int64           `json:"source_fact_sequence"`
	Metadata            json.RawMessage `json:"metadata"`
}

type CityFacilityServiceCapacity struct {
	FacilityCode           string          `json:"facility_code"`
	ServiceCode            string          `json:"service_code"`
	ServiceVersion         string          `json:"service_version"`
	ServiceHash            string          `json:"service_hash"`
	InstalledCapacityUnits int64           `json:"installed_capacity_units"`
	AvailabilityMilli      int             `json:"availability_milli"`
	AvailableCapacityUnits int64           `json:"available_capacity_units"`
	DispatchCapacityUnits  int64           `json:"dispatch_capacity_units"`
	UpdatedTick            int64           `json:"updated_tick"`
	Version                int64           `json:"version"`
	SourceFactTick         int64           `json:"source_fact_tick"`
	SourceFactSequence     int64           `json:"source_fact_sequence"`
	Metadata               json.RawMessage `json:"metadata"`
}

type CityServiceDemand struct {
	Code                  string          `json:"code"`
	ServiceCode           string          `json:"service_code"`
	ServiceVersion        string          `json:"service_version"`
	ServiceHash           string          `json:"service_hash"`
	SubjectKind           string          `json:"subject_kind"`
	SubjectCode           string          `json:"subject_code"`
	DistrictCode          string          `json:"district_code"`
	BuildingCode          *string         `json:"building_code,omitempty"`
	RequestedUnitsPerTick int64           `json:"requested_units_per_tick"`
	Priority              int             `json:"priority"`
	Status                string          `json:"status"`
	CreatedTick           int64           `json:"created_tick"`
	UpdatedTick           int64           `json:"updated_tick"`
	Version               int64           `json:"version"`
	SourceFactTick        int64           `json:"source_fact_tick"`
	SourceFactSequence    int64           `json:"source_fact_sequence"`
	Metadata              json.RawMessage `json:"metadata"`
}

type CityServiceConnection struct {
	Code                string          `json:"code"`
	FacilityCode        string          `json:"facility_code"`
	ServiceCode         string          `json:"service_code"`
	DemandCode          string          `json:"demand_code"`
	MaxFlowUnitsPerTick int64           `json:"max_flow_units_per_tick"`
	LossMilli           int             `json:"loss_milli"`
	Preference          int             `json:"preference"`
	Status              string          `json:"status"`
	CreatedTick         int64           `json:"created_tick"`
	UpdatedTick         int64           `json:"updated_tick"`
	Version             int64           `json:"version"`
	SourceFactTick      int64           `json:"source_fact_tick"`
	SourceFactSequence  int64           `json:"source_fact_sequence"`
	Metadata            json.RawMessage `json:"metadata"`
}

type CityServiceFact struct {
	Tick                  int64           `json:"tick"`
	Sequence              int64           `json:"sequence"`
	SourceCommandSequence *int64          `json:"source_command_sequence,omitempty"`
	FactType              string          `json:"fact_type"`
	SubjectKind           string          `json:"subject_kind"`
	SubjectCode           string          `json:"subject_code"`
	VersionBefore         int64           `json:"version_before"`
	VersionAfter          int64           `json:"version_after"`
	Payload               json.RawMessage `json:"payload"`
}

type CityServiceAllocation struct {
	Tick                    int64           `json:"tick"`
	Sequence                int64           `json:"sequence"`
	AllocationIndex         int             `json:"allocation_index"`
	ServiceCode             string          `json:"service_code"`
	FacilityCode            string          `json:"facility_code"`
	DemandCode              string          `json:"demand_code"`
	ConnectionCode          string          `json:"connection_code"`
	CapacityVersion         int64           `json:"capacity_version"`
	DemandVersion           int64           `json:"demand_version"`
	ConnectionVersion       int64           `json:"connection_version"`
	FacilityCapacityUnits   int64           `json:"facility_capacity_units"`
	ConnectionCapacityUnits int64           `json:"connection_capacity_units"`
	LossMilli               int             `json:"loss_milli"`
	DispatchedUnits         int64           `json:"dispatched_units"`
	NetworkReceivedUnits    *int64          `json:"network_received_units,omitempty"`
	NetworkLossUnits        *int64          `json:"network_loss_units,omitempty"`
	ConnectionLossUnits     *int64          `json:"connection_loss_units,omitempty"`
	NetworkPathCount        *int            `json:"network_path_count,omitempty"`
	DeliveredUnits          int64           `json:"delivered_units"`
	LossUnits               int64           `json:"loss_units"`
	Metadata                json.RawMessage `json:"metadata"`
}

type CityServiceSettlement struct {
	Tick            int64           `json:"tick"`
	Sequence        int64           `json:"sequence"`
	ServiceCode     string          `json:"service_code"`
	DemandCode      string          `json:"demand_code"`
	DemandVersion   int64           `json:"demand_version"`
	RequestedUnits  int64           `json:"requested_units"`
	DeliveredUnits  int64           `json:"delivered_units"`
	ShortageUnits   int64           `json:"shortage_units"`
	AllocationCount int             `json:"allocation_count"`
	QualityMilli    int             `json:"quality_milli"`
	Metadata        json.RawMessage `json:"metadata"`
}

type CityPublicServiceState struct {
	Profile            CityServiceProfile            `json:"profile"`
	ServiceDefinitions []CityServiceDefinition       `json:"service_definitions"`
	FacilityTypes      []CityFacilityTypeDefinition  `json:"facility_types"`
	Facilities         []CityFacility                `json:"facilities"`
	Capacities         []CityFacilityServiceCapacity `json:"capacities"`
	Demands            []CityServiceDemand           `json:"demands"`
	Connections        []CityServiceConnection       `json:"connections"`
	Facts              []CityServiceFact             `json:"facts"`
	Allocations        []CityServiceAllocation       `json:"allocations"`
	Settlements        []CityServiceSettlement       `json:"settlements"`
}

type cityPublicServiceHashState = CityPublicServiceState

type CityServiceQueryInput struct {
	UserID        int64
	WorldID       int64
	ServiceCode   string
	Status        string
	DistrictCode  string
	FacilityCode  string
	DemandCode    string
	AfterCode     string
	AfterTick     int64
	AfterSequence int64
	Limit         int
}

type cityServiceDefinitionSeed struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	UnitCode  string `json:"unit_code"`
	FlowKind  string `json:"flow_kind"`
	SortOrder int    `json:"sort_order"`
}

type cityFacilityTypeSeed struct {
	Code                    string   `json:"code"`
	Name                    string   `json:"name"`
	MinimumFloorAreaSQM     int64    `json:"minimum_floor_area_sqm"`
	DefaultReliabilityMilli int      `json:"default_reliability_milli"`
	AllowedServiceCodes     []string `json:"allowed_service_codes"`
	SortOrder               int      `json:"sort_order"`
}

var cityServiceDefinitionSeeds = []cityServiceDefinitionSeed{
	{Code: "electric_power", Name: "Electric power", Category: "utility", UnitCode: "energy_unit", FlowKind: "delivery", SortOrder: 10},
	{Code: "potable_water", Name: "Potable water", Category: "utility", UnitCode: "volume_unit", FlowKind: "delivery", SortOrder: 20},
	{Code: "wastewater", Name: "Wastewater treatment", Category: "waste", UnitCode: "volume_unit", FlowKind: "collection", SortOrder: 30},
	{Code: "solid_waste", Name: "Solid waste collection", Category: "waste", UnitCode: "mass_unit", FlowKind: "collection", SortOrder: 40},
	{Code: "education", Name: "Education", Category: "social", UnitCode: "service_slot", FlowKind: "capacity", SortOrder: 50},
	{Code: "healthcare", Name: "Healthcare", Category: "social", UnitCode: "service_slot", FlowKind: "capacity", SortOrder: 60},
	{Code: "fire_response", Name: "Fire response", Category: "safety", UnitCode: "coverage_unit", FlowKind: "capacity", SortOrder: 70},
	{Code: "police_response", Name: "Police response", Category: "safety", UnitCode: "coverage_unit", FlowKind: "capacity", SortOrder: 80},
}

var cityFacilityTypeSeeds = []cityFacilityTypeSeed{
	{Code: "service_hub", Name: "Service hub", MinimumFloorAreaSQM: 1, DefaultReliabilityMilli: 1000, AllowedServiceCodes: []string{"education", "electric_power", "fire_response", "healthcare", "police_response", "potable_water", "solid_waste", "wastewater"}, SortOrder: 10},
	{Code: "power_plant", Name: "Power plant", MinimumFloorAreaSQM: 1, DefaultReliabilityMilli: 950, AllowedServiceCodes: []string{"electric_power"}, SortOrder: 20},
	{Code: "water_works", Name: "Water works", MinimumFloorAreaSQM: 1, DefaultReliabilityMilli: 950, AllowedServiceCodes: []string{"potable_water"}, SortOrder: 30},
	{Code: "wastewater_plant", Name: "Wastewater plant", MinimumFloorAreaSQM: 1, DefaultReliabilityMilli: 950, AllowedServiceCodes: []string{"wastewater"}, SortOrder: 40},
	{Code: "waste_processing", Name: "Waste processing", MinimumFloorAreaSQM: 1, DefaultReliabilityMilli: 950, AllowedServiceCodes: []string{"solid_waste"}, SortOrder: 50},
	{Code: "school", Name: "School", MinimumFloorAreaSQM: 1, DefaultReliabilityMilli: 900, AllowedServiceCodes: []string{"education"}, SortOrder: 60},
	{Code: "hospital", Name: "Hospital", MinimumFloorAreaSQM: 1, DefaultReliabilityMilli: 900, AllowedServiceCodes: []string{"healthcare"}, SortOrder: 70},
	{Code: "fire_station", Name: "Fire station", MinimumFloorAreaSQM: 1, DefaultReliabilityMilli: 900, AllowedServiceCodes: []string{"fire_response"}, SortOrder: 80},
	{Code: "police_station", Name: "Police station", MinimumFloorAreaSQM: 1, DefaultReliabilityMilli: 900, AllowedServiceCodes: []string{"police_response"}, SortOrder: 90},
}

func isCityServiceCommand(commandType string) bool {
	switch commandType {
	case CityCommandTypeFacilityRegister,
		CityCommandTypeFacilityStatusTransition,
		CityCommandTypeFacilityCapacityConfigure,
		CityCommandTypeServiceDemandConfigure,
		CityCommandTypeServiceConnectionConfigure:
		return true
	default:
		return false
	}
}

func cityPublicServiceCatalog() ([]CityServiceDefinition, []CityFacilityTypeDefinition, string, error) {
	services := make([]CityServiceDefinition, 0, len(cityServiceDefinitionSeeds))
	for _, seed := range cityServiceDefinitionSeeds {
		payload, err := json.Marshal(seed)
		if err != nil {
			return nil, nil, "", fmt.Errorf("marshal city service definition %s: %w", seed.Code, err)
		}
		sum := sha256.Sum256(payload)
		services = append(services, CityServiceDefinition{
			Code: seed.Code, DefinitionVersion: cityServiceCatalogVersion,
			DefinitionHash: hex.EncodeToString(sum[:]), Name: seed.Name,
			Category: seed.Category, UnitCode: seed.UnitCode, FlowKind: seed.FlowKind,
			Status: CityServiceProjectionStatusActive, SortOrder: seed.SortOrder, Payload: payload,
		})
	}
	types := make([]CityFacilityTypeDefinition, 0, len(cityFacilityTypeSeeds))
	for _, seed := range cityFacilityTypeSeeds {
		allowed := append([]string(nil), seed.AllowedServiceCodes...)
		sort.Strings(allowed)
		seed.AllowedServiceCodes = allowed
		payload, err := json.Marshal(seed)
		if err != nil {
			return nil, nil, "", fmt.Errorf("marshal city facility type %s: %w", seed.Code, err)
		}
		sum := sha256.Sum256(payload)
		types = append(types, CityFacilityTypeDefinition{
			Code: seed.Code, DefinitionVersion: cityServiceCatalogVersion,
			DefinitionHash: hex.EncodeToString(sum[:]), Name: seed.Name,
			MinimumFloorAreaSQM:     seed.MinimumFloorAreaSQM,
			DefaultReliabilityMilli: seed.DefaultReliabilityMilli,
			AllowedServiceCodes:     allowed, Status: CityServiceProjectionStatusActive,
			SortOrder: seed.SortOrder, Payload: payload,
		})
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Code < services[j].Code })
	sort.Slice(types, func(i, j int) bool { return types[i].Code < types[j].Code })
	catalogCanonical, err := json.Marshal(struct {
		CatalogID      string                       `json:"catalog_id"`
		CatalogVersion string                       `json:"catalog_version"`
		Services       []CityServiceDefinition      `json:"services"`
		FacilityTypes  []CityFacilityTypeDefinition `json:"facility_types"`
	}{cityServiceCatalogID, cityServiceCatalogVersion, services, types})
	if err != nil {
		return nil, nil, "", fmt.Errorf("marshal city service catalog: %w", err)
	}
	sum := sha256.Sum256(catalogCanonical)
	return services, types, hex.EncodeToString(sum[:]), nil
}

func initializeCityServiceFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	services, facilityTypes, catalogHash, err := cityPublicServiceCatalog()
	if err != nil {
		return err
	}
	var baselineTick int64
	if err = tx.QueryRowContext(ctx, `SELECT current_tick FROM city_worlds WHERE id = $1`, worldID).Scan(&baselineTick); err != nil {
		return fmt.Errorf("load city service baseline tick: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_service_bootstrap_world_id', $1, true)`,
		fmt.Sprintf("%d", worldID),
	); err != nil {
		return fmt.Errorf("enable city service bootstrap: %w", err)
	}
	metadata := json.RawMessage(`{"schema_version":1}`)
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_service_profiles
    (world_id, catalog_id, catalog_version, catalog_hash, settlement_version,
     baseline_tick, service_definition_count, facility_type_count, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`,
		worldID, cityServiceCatalogID, cityServiceCatalogVersion, catalogHash,
		cityServiceSettlementVersion, baselineTick, len(services), len(facilityTypes), metadata,
	); err != nil {
		return fmt.Errorf("insert city service profile: %w", err)
	}
	for _, definition := range services {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_service_definitions
    (world_id, code, definition_version, definition_hash, name, category,
     unit_code, flow_kind, status, sort_order, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)`,
			worldID, definition.Code, definition.DefinitionVersion, definition.DefinitionHash,
			definition.Name, definition.Category, definition.UnitCode, definition.FlowKind,
			definition.Status, definition.SortOrder, definition.Payload,
		); err != nil {
			return fmt.Errorf("insert city service definition %s: %w", definition.Code, err)
		}
	}
	for _, definition := range facilityTypes {
		allowed, marshalErr := json.Marshal(definition.AllowedServiceCodes)
		if marshalErr != nil {
			return fmt.Errorf("marshal allowed city facility services: %w", marshalErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_facility_type_definitions
    (world_id, code, definition_version, definition_hash, name,
     minimum_floor_area_sqm, default_reliability_milli, allowed_service_codes,
     status, sort_order, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11::jsonb)`,
			worldID, definition.Code, definition.DefinitionVersion, definition.DefinitionHash,
			definition.Name, definition.MinimumFloorAreaSQM, definition.DefaultReliabilityMilli,
			allowed, definition.Status, definition.SortOrder, definition.Payload,
		); err != nil {
			return fmt.Errorf("insert city facility type %s: %w", definition.Code, err)
		}
	}
	return nil
}

func loadCityPublicServiceHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityPublicServiceHashState, error) {
	state := &CityPublicServiceState{
		ServiceDefinitions: make([]CityServiceDefinition, 0),
		FacilityTypes:      make([]CityFacilityTypeDefinition, 0),
		Facilities:         make([]CityFacility, 0),
		Capacities:         make([]CityFacilityServiceCapacity, 0),
		Demands:            make([]CityServiceDemand, 0),
		Connections:        make([]CityServiceConnection, 0),
		Facts:              make([]CityServiceFact, 0),
		Allocations:        make([]CityServiceAllocation, 0),
		Settlements:        make([]CityServiceSettlement, 0),
	}
	err := queryer.QueryRowContext(ctx, `
SELECT catalog_id, catalog_version, catalog_hash, settlement_version,
       baseline_tick, service_definition_count, facility_type_count,
       facility_count, capacity_count, demand_count, connection_count,
       fact_count, allocation_count, settlement_count, revision, metadata
FROM city_service_profiles WHERE world_id = $1`, worldID).Scan(
		&state.Profile.CatalogID, &state.Profile.CatalogVersion, &state.Profile.CatalogHash,
		&state.Profile.SettlementVersion, &state.Profile.BaselineTick,
		&state.Profile.ServiceDefinitionCount, &state.Profile.FacilityTypeCount,
		&state.Profile.FacilityCount, &state.Profile.CapacityCount, &state.Profile.DemandCount,
		&state.Profile.ConnectionCount, &state.Profile.FactCount, &state.Profile.AllocationCount,
		&state.Profile.SettlementCount, &state.Profile.Revision, &state.Profile.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityServiceStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load city service profile: %w", err)
	}
	if err = loadCityServiceDefinitions(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityFacilityTypes(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityFacilities(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityServiceCapacities(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityServiceDemands(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityServiceConnections(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityServiceFacts(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityServiceAllocations(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityServiceSettlements(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	return state, nil
}

func loadCityServiceDefinitions(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityPublicServiceState) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT code, definition_version, definition_hash, name, category, unit_code,
       flow_kind, status, sort_order, payload
FROM city_service_definitions WHERE world_id = $1 ORDER BY code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load city service definitions: %w", err)
	}
	for rows.Next() {
		var item CityServiceDefinition
		if err = rows.Scan(&item.Code, &item.DefinitionVersion, &item.DefinitionHash,
			&item.Name, &item.Category, &item.UnitCode, &item.FlowKind, &item.Status,
			&item.SortOrder, &item.Payload); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city service definition: %w", err)
		}
		state.ServiceDefinitions = append(state.ServiceDefinitions, item)
	}
	return closeCityRows(rows, "iterate city service definitions")
}

func loadCityFacilityTypes(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityPublicServiceState) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT code, definition_version, definition_hash, name, minimum_floor_area_sqm,
       default_reliability_milli, allowed_service_codes, status, sort_order, payload
FROM city_facility_type_definitions WHERE world_id = $1 ORDER BY code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load city facility types: %w", err)
	}
	for rows.Next() {
		var item CityFacilityTypeDefinition
		var allowed json.RawMessage
		if err = rows.Scan(&item.Code, &item.DefinitionVersion, &item.DefinitionHash,
			&item.Name, &item.MinimumFloorAreaSQM, &item.DefaultReliabilityMilli,
			&allowed, &item.Status, &item.SortOrder, &item.Payload); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city facility type: %w", err)
		}
		if err = json.Unmarshal(allowed, &item.AllowedServiceCodes); err != nil {
			_ = rows.Close()
			return fmt.Errorf("decode city facility allowed services: %w", err)
		}
		state.FacilityTypes = append(state.FacilityTypes, item)
	}
	return closeCityRows(rows, "iterate city facility types")
}

func loadCityFacilities(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityPublicServiceState) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT facility.code, facility.name, type.code, type.definition_version,
       type.definition_hash, district.code, building.code, owner.code,
       facility.status, facility.reliability_milli, facility.created_tick,
       facility.updated_tick, facility.version, fact.tick, fact.sequence, facility.metadata
FROM city_facilities facility
JOIN city_facility_type_definitions type ON type.id = facility.facility_type_id
JOIN city_districts district ON district.id = facility.district_id
JOIN city_buildings building ON building.id = facility.building_id
LEFT JOIN city_economic_entities owner ON owner.id = facility.owner_entity_id
JOIN city_service_facts fact ON fact.id = facility.source_fact_id
WHERE facility.world_id = $1 ORDER BY facility.code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load city facilities: %w", err)
	}
	for rows.Next() {
		var item CityFacility
		var owner sql.NullString
		if err = rows.Scan(&item.Code, &item.Name, &item.FacilityTypeCode,
			&item.FacilityTypeVersion, &item.FacilityTypeHash, &item.DistrictCode,
			&item.BuildingCode, &owner, &item.Status, &item.ReliabilityMilli,
			&item.CreatedTick, &item.UpdatedTick, &item.Version, &item.SourceFactTick,
			&item.SourceFactSequence, &item.Metadata); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city facility: %w", err)
		}
		item.OwnerEntityCode = nullStringPointer(owner)
		state.Facilities = append(state.Facilities, item)
	}
	return closeCityRows(rows, "iterate city facilities")
}

func loadCityServiceCapacities(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityPublicServiceState) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT facility.code, service.code, service.definition_version, service.definition_hash,
       capacity.installed_capacity_units, capacity.availability_milli,
       capacity.available_capacity_units,
       CASE WHEN facility.status IN ('operational', 'degraded')
            THEN FLOOR(capacity.available_capacity_units::NUMERIC
                 * COALESCE(lifecycle.effective_factor_milli, 1000) / 1000)::BIGINT
            ELSE 0 END,
       capacity.updated_tick, capacity.version, fact.tick, fact.sequence, capacity.metadata
FROM city_facility_service_capacities capacity
JOIN city_facilities facility ON facility.id = capacity.facility_id
JOIN city_service_definitions service ON service.id = capacity.service_definition_id
JOIN city_service_facts fact ON fact.id = capacity.source_fact_id
LEFT JOIN city_facility_lifecycle_states lifecycle
  ON lifecycle.world_id = capacity.world_id AND lifecycle.facility_id = capacity.facility_id
WHERE capacity.world_id = $1 ORDER BY facility.code ASC, service.code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load city service capacities: %w", err)
	}
	for rows.Next() {
		var item CityFacilityServiceCapacity
		if err = rows.Scan(&item.FacilityCode, &item.ServiceCode, &item.ServiceVersion,
			&item.ServiceHash, &item.InstalledCapacityUnits, &item.AvailabilityMilli,
			&item.AvailableCapacityUnits, &item.DispatchCapacityUnits, &item.UpdatedTick,
			&item.Version, &item.SourceFactTick, &item.SourceFactSequence,
			&item.Metadata); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city service capacity: %w", err)
		}
		state.Capacities = append(state.Capacities, item)
	}
	return closeCityRows(rows, "iterate city service capacities")
}

func loadCityServiceDemands(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityPublicServiceState) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT demand.code, service.code, service.definition_version, service.definition_hash,
       demand.subject_kind, demand.subject_code, district.code, building.code,
       demand.requested_units_per_tick, demand.priority, demand.status,
       demand.created_tick, demand.updated_tick, demand.version,
       fact.tick, fact.sequence, demand.metadata
FROM city_service_demands demand
JOIN city_service_definitions service ON service.id = demand.service_definition_id
JOIN city_districts district ON district.id = demand.district_id
LEFT JOIN city_buildings building ON building.id = demand.building_id
JOIN city_service_facts fact ON fact.id = demand.source_fact_id
WHERE demand.world_id = $1 ORDER BY service.code ASC, demand.code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load city service demands: %w", err)
	}
	for rows.Next() {
		var item CityServiceDemand
		var building sql.NullString
		if err = rows.Scan(&item.Code, &item.ServiceCode, &item.ServiceVersion,
			&item.ServiceHash, &item.SubjectKind, &item.SubjectCode, &item.DistrictCode,
			&building, &item.RequestedUnitsPerTick, &item.Priority, &item.Status,
			&item.CreatedTick, &item.UpdatedTick, &item.Version, &item.SourceFactTick,
			&item.SourceFactSequence, &item.Metadata); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city service demand: %w", err)
		}
		item.BuildingCode = nullStringPointer(building)
		state.Demands = append(state.Demands, item)
	}
	return closeCityRows(rows, "iterate city service demands")
}

func loadCityServiceConnections(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityPublicServiceState) error {
	rows, err := queryer.QueryContext(ctx, `
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
WHERE connection.world_id = $1 ORDER BY demand.code ASC, connection.code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load city service connections: %w", err)
	}
	for rows.Next() {
		var item CityServiceConnection
		if err = rows.Scan(&item.Code, &item.FacilityCode, &item.ServiceCode,
			&item.DemandCode, &item.MaxFlowUnitsPerTick, &item.LossMilli,
			&item.Preference, &item.Status, &item.CreatedTick, &item.UpdatedTick,
			&item.Version, &item.SourceFactTick, &item.SourceFactSequence,
			&item.Metadata); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city service connection: %w", err)
		}
		state.Connections = append(state.Connections, item)
	}
	return closeCityRows(rows, "iterate city service connections")
}

func loadCityServiceFacts(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityPublicServiceState) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, command.sequence, fact.fact_type,
       fact.subject_kind, fact.subject_code, fact.version_before,
       fact.version_after, fact.payload
FROM city_service_facts fact
LEFT JOIN city_commands command ON command.id = fact.source_command_id
WHERE fact.world_id = $1 AND fact.posted_at IS NOT NULL
ORDER BY fact.tick ASC, fact.sequence ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load city service facts: %w", err)
	}
	for rows.Next() {
		var item CityServiceFact
		var commandSequence sql.NullInt64
		if err = rows.Scan(&item.Tick, &item.Sequence, &commandSequence, &item.FactType,
			&item.SubjectKind, &item.SubjectCode, &item.VersionBefore,
			&item.VersionAfter, &item.Payload); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city service fact: %w", err)
		}
		item.SourceCommandSequence = nullInt64Pointer(commandSequence)
		state.Facts = append(state.Facts, item)
	}
	return closeCityRows(rows, "iterate city service facts")
}

func loadCityServiceAllocations(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityPublicServiceState) error {
	rows, err := queryer.QueryContext(ctx, `
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
ORDER BY allocation.tick ASC, allocation.sequence ASC, allocation.allocation_index ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load city service allocations: %w", err)
	}
	for rows.Next() {
		var item CityServiceAllocation
		var networkReceived, networkLoss, connectionLoss sql.NullInt64
		var networkPathCount sql.NullInt32
		if err = rows.Scan(&item.Tick, &item.Sequence, &item.AllocationIndex,
			&item.ServiceCode, &item.FacilityCode, &item.DemandCode,
			&item.ConnectionCode, &item.CapacityVersion, &item.DemandVersion,
			&item.ConnectionVersion, &item.FacilityCapacityUnits,
			&item.ConnectionCapacityUnits, &item.LossMilli, &item.DispatchedUnits,
			&networkReceived, &networkLoss, &connectionLoss, &networkPathCount,
			&item.DeliveredUnits, &item.LossUnits, &item.Metadata); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city service allocation: %w", err)
		}
		item.NetworkReceivedUnits = nullInt64Pointer(networkReceived)
		item.NetworkLossUnits = nullInt64Pointer(networkLoss)
		item.ConnectionLossUnits = nullInt64Pointer(connectionLoss)
		if networkPathCount.Valid {
			value := int(networkPathCount.Int32)
			item.NetworkPathCount = &value
		}
		state.Allocations = append(state.Allocations, item)
	}
	return closeCityRows(rows, "iterate city service allocations")
}

func loadCityServiceSettlements(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityPublicServiceState) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT settlement.tick, settlement.sequence, service.code, demand.code,
       settlement.demand_version, settlement.requested_units,
       settlement.delivered_units, settlement.shortage_units,
       settlement.allocation_count, settlement.quality_milli, settlement.metadata
FROM city_service_settlements settlement
JOIN city_service_definitions service ON service.id = settlement.service_definition_id
JOIN city_service_demands demand ON demand.id = settlement.demand_id
WHERE settlement.world_id = $1
ORDER BY settlement.tick ASC, settlement.sequence ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load city service settlements: %w", err)
	}
	for rows.Next() {
		var item CityServiceSettlement
		if err = rows.Scan(&item.Tick, &item.Sequence, &item.ServiceCode,
			&item.DemandCode, &item.DemandVersion, &item.RequestedUnits,
			&item.DeliveredUnits, &item.ShortageUnits, &item.AllocationCount,
			&item.QualityMilli, &item.Metadata); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city service settlement: %w", err)
		}
		state.Settlements = append(state.Settlements, item)
	}
	return closeCityRows(rows, "iterate city service settlements")
}

func normalizeCityServiceCode(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	return value, cityServiceCodePattern.MatchString(value)
}

func normalizeCityServiceMetadata(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`), nil
	}
	if len(raw) > cityServiceMaximumMetadataBytes || !json.Valid(raw) {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "metadata"})
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "metadata"})
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "metadata"})
	}
	return canonical, nil
}
