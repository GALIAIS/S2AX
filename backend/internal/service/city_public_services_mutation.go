package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The per-projection ceiling keeps both loss-aware multiplication and a full
// 10,000-projection aggregate inside signed BIGINT bounds.
const (
	cityServiceMaximumProjectionCount int64 = 10_000
	cityServiceMaximumConfiguredUnits int64 = 922_337_203_685_477
)

const (
	cityServiceRejectionFacilityNotFound   = "CITY_FACILITY_NOT_FOUND"
	cityServiceRejectionFacilityConflict   = "CITY_FACILITY_CONFLICT"
	cityServiceRejectionBuildingInvalid    = "CITY_FACILITY_BUILDING_INVALID"
	cityServiceRejectionOwnerInvalid       = "CITY_FACILITY_OWNER_INVALID"
	cityServiceRejectionVersionConflict    = "CITY_SERVICE_VERSION_CONFLICT"
	cityServiceRejectionStateTransition    = "CITY_FACILITY_STATE_TRANSITION_INVALID"
	cityServiceRejectionCapacityNotFound   = "CITY_FACILITY_CAPACITY_NOT_FOUND"
	cityServiceRejectionServiceNotAllowed  = "CITY_FACILITY_SERVICE_NOT_ALLOWED"
	cityServiceRejectionSubjectInvalid     = "CITY_SERVICE_SUBJECT_INVALID"
	cityServiceRejectionDemandNotFound     = "CITY_SERVICE_DEMAND_NOT_FOUND"
	cityServiceRejectionDemandConflict     = "CITY_SERVICE_DEMAND_CONFLICT"
	cityServiceRejectionConnectionNotFound = "CITY_SERVICE_CONNECTION_NOT_FOUND"
	cityServiceRejectionConnectionConflict = "CITY_SERVICE_CONNECTION_CONFLICT"
	cityServiceRejectionServiceMismatch    = "CITY_SERVICE_DEFINITION_MISMATCH"
	cityServiceRejectionProjectionLimit    = "CITY_SERVICE_PROJECTION_LIMIT_REACHED"
)

type cityFacilityRegisterPayload struct {
	Code             string          `json:"code"`
	Name             string          `json:"name"`
	FacilityTypeCode string          `json:"facility_type_code"`
	BuildingCode     string          `json:"building_code"`
	OwnerEntityCode  string          `json:"owner_entity_code,omitempty"`
	ReliabilityMilli *int            `json:"reliability_milli,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
}

type cityFacilityStatusTransitionPayload struct {
	FacilityCode    string          `json:"facility_code"`
	ToStatus        string          `json:"to_status"`
	ExpectedVersion int64           `json:"expected_version"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

type cityFacilityCapacityConfigurePayload struct {
	FacilityCode           string          `json:"facility_code"`
	ServiceCode            string          `json:"service_code"`
	InstalledCapacityUnits int64           `json:"installed_capacity_units"`
	AvailabilityMilli      int             `json:"availability_milli"`
	ExpectedVersion        int64           `json:"expected_version"`
	Metadata               json.RawMessage `json:"metadata,omitempty"`
}

type cityServiceDemandConfigurePayload struct {
	Code                  string          `json:"code"`
	ServiceCode           string          `json:"service_code"`
	SubjectKind           string          `json:"subject_kind"`
	SubjectCode           string          `json:"subject_code"`
	RequestedUnitsPerTick int64           `json:"requested_units_per_tick"`
	Priority              int             `json:"priority"`
	Status                string          `json:"status"`
	ExpectedVersion       int64           `json:"expected_version"`
	Metadata              json.RawMessage `json:"metadata,omitempty"`
}

type cityServiceConnectionConfigurePayload struct {
	Code                string          `json:"code"`
	FacilityCode        string          `json:"facility_code"`
	ServiceCode         string          `json:"service_code"`
	DemandCode          string          `json:"demand_code"`
	MaxFlowUnitsPerTick int64           `json:"max_flow_units_per_tick"`
	LossMilli           int             `json:"loss_milli"`
	Preference          int             `json:"preference"`
	Status              string          `json:"status"`
	ExpectedVersion     int64           `json:"expected_version"`
	Metadata            json.RawMessage `json:"metadata,omitempty"`
}

type cityServiceBusinessError struct{ code string }

func (err *cityServiceBusinessError) Error() string { return err.code }

func cityServiceReject(code string) error { return &cityServiceBusinessError{code: code} }

func cityServiceBusinessRejectionCode(err error) string {
	var businessErr *cityServiceBusinessError
	if errors.As(err, &businessErr) {
		return businessErr.code
	}
	return ""
}

func normalizeCityServiceCommand(commandType string, raw json.RawMessage) (any, bool, error) {
	normalizeCode := func(value *string) error {
		normalized, valid := normalizeCityServiceCode(*value)
		*value = normalized
		if !valid {
			return ErrCityInvalidInput
		}
		return nil
	}
	normalizeMetadata := func(value *json.RawMessage) error {
		metadata, err := normalizeCityServiceMetadata(*value)
		if err == nil {
			*value = metadata
		}
		return err
	}
	switch commandType {
	case CityCommandTypeFacilityRegister:
		var value cityFacilityRegisterPayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		value.Name = strings.TrimSpace(value.Name)
		value.OwnerEntityCode = strings.ToLower(strings.TrimSpace(value.OwnerEntityCode))
		if normalizeCode(&value.Code) != nil || normalizeCode(&value.FacilityTypeCode) != nil ||
			normalizeCode(&value.BuildingCode) != nil ||
			(value.OwnerEntityCode != "" && normalizeCode(&value.OwnerEntityCode) != nil) ||
			utf8.RuneCountInString(value.Name) < 1 || utf8.RuneCountInString(value.Name) > 96 ||
			normalizeMetadata(&value.Metadata) != nil ||
			(value.ReliabilityMilli != nil && (*value.ReliabilityMilli < 0 || *value.ReliabilityMilli > 1000)) {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	case CityCommandTypeFacilityStatusTransition:
		var value cityFacilityStatusTransitionPayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		value.ToStatus = strings.ToLower(strings.TrimSpace(value.ToStatus))
		if normalizeCode(&value.FacilityCode) != nil || value.ExpectedVersion <= 0 ||
			!isCityFacilityStatus(value.ToStatus) || normalizeMetadata(&value.Metadata) != nil {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	case CityCommandTypeFacilityCapacityConfigure:
		var value cityFacilityCapacityConfigurePayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if normalizeCode(&value.FacilityCode) != nil || normalizeCode(&value.ServiceCode) != nil ||
			value.InstalledCapacityUnits <= 0 || value.InstalledCapacityUnits > cityServiceMaximumConfiguredUnits ||
			value.AvailabilityMilli < 0 || value.AvailabilityMilli > 1000 ||
			value.ExpectedVersion < 0 || normalizeMetadata(&value.Metadata) != nil {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	case CityCommandTypeServiceDemandConfigure:
		var value cityServiceDemandConfigurePayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		value.SubjectKind = strings.ToLower(strings.TrimSpace(value.SubjectKind))
		value.SubjectCode = strings.ToLower(strings.TrimSpace(value.SubjectCode))
		value.Status = strings.ToLower(strings.TrimSpace(value.Status))
		if normalizeCode(&value.Code) != nil || normalizeCode(&value.ServiceCode) != nil ||
			!cityServiceSubjectCodePattern.MatchString(value.SubjectCode) ||
			!isCityServiceSubjectKind(value.SubjectKind) ||
			value.RequestedUnitsPerTick < 0 || value.RequestedUnitsPerTick > cityServiceMaximumConfiguredUnits ||
			value.Priority < 0 || value.Priority > 1000 || !isCityServiceProjectionStatus(value.Status) ||
			value.ExpectedVersion < 0 || normalizeMetadata(&value.Metadata) != nil {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	case CityCommandTypeServiceConnectionConfigure:
		var value cityServiceConnectionConfigurePayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		value.Status = strings.ToLower(strings.TrimSpace(value.Status))
		if normalizeCode(&value.Code) != nil || normalizeCode(&value.FacilityCode) != nil ||
			normalizeCode(&value.ServiceCode) != nil || normalizeCode(&value.DemandCode) != nil ||
			value.MaxFlowUnitsPerTick <= 0 || value.MaxFlowUnitsPerTick > cityServiceMaximumConfiguredUnits ||
			value.LossMilli < 0 || value.LossMilli > 999 || value.Preference < 0 || value.Preference > 1000 ||
			!isCityServiceProjectionStatus(value.Status) || value.ExpectedVersion < 0 ||
			normalizeMetadata(&value.Metadata) != nil {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	default:
		return nil, false, nil
	}
}

func isCityFacilityStatus(value string) bool {
	switch value {
	case CityFacilityStatusOffline, CityFacilityStatusOperational,
		CityFacilityStatusDegraded, CityFacilityStatusRetired:
		return true
	default:
		return false
	}
}

func isCityServiceProjectionStatus(value string) bool {
	return value == CityServiceProjectionStatusActive ||
		value == CityServiceProjectionStatusSuspended ||
		value == CityServiceProjectionStatusRetired
}

func isCityServiceSubjectKind(value string) bool {
	switch value {
	case "district", "building", "household", "enterprise", "actor":
		return true
	default:
		return false
	}
}

type cityServiceExecution struct {
	pending cityPendingEvent
	fact    *CityServiceFact
}

type cityServiceFactRecord struct {
	id   int64
	fact CityServiceFact
}

type cityServiceProfileDeltas struct {
	facilities  int64
	capacities  int64
	demands     int64
	connections int64
	allocations int64
	settlements int64
}

func (s *CityEconomyService) applyCityServiceCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
	command *CityCommand,
) (cityServiceExecution, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT city_service_command`); err != nil {
		return cityServiceExecution{}, fmt.Errorf("create city service command savepoint: %w", err)
	}
	execution, err := s.postCityServiceCommand(ctx, tx, worldID, targetTick, factSequence, command)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_service_command`); rollbackErr != nil {
			return cityServiceExecution{}, fmt.Errorf("rollback city service command after %v: %w", err, rollbackErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT city_service_command`); releaseErr != nil {
			return cityServiceExecution{}, fmt.Errorf("release rejected city service command: %w", releaseErr)
		}
		if code := cityServiceBusinessRejectionCode(err); code != "" {
			return cityServiceExecution{pending: rejectedCityCommand(command, code)}, nil
		}
		return cityServiceExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_service_command`); err != nil {
		return cityServiceExecution{}, fmt.Errorf("release city service command: %w", err)
	}
	return execution, nil
}

func (s *CityEconomyService) postCityServiceCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
	command *CityCommand,
) (cityServiceExecution, error) {
	switch command.CommandType {
	case CityCommandTypeFacilityRegister:
		payload, err := decodeStoredCityCommandPayload[cityFacilityRegisterPayload](command)
		if err != nil {
			return cityServiceExecution{}, err
		}
		return s.registerCityFacility(ctx, tx, worldID, targetTick, factSequence, command, payload)
	case CityCommandTypeFacilityStatusTransition:
		payload, err := decodeStoredCityCommandPayload[cityFacilityStatusTransitionPayload](command)
		if err != nil {
			return cityServiceExecution{}, err
		}
		return s.transitionCityFacilityStatus(ctx, tx, worldID, targetTick, factSequence, command, payload)
	case CityCommandTypeFacilityCapacityConfigure:
		payload, err := decodeStoredCityCommandPayload[cityFacilityCapacityConfigurePayload](command)
		if err != nil {
			return cityServiceExecution{}, err
		}
		return s.configureCityFacilityCapacity(ctx, tx, worldID, targetTick, factSequence, command, payload)
	case CityCommandTypeServiceDemandConfigure:
		payload, err := decodeStoredCityCommandPayload[cityServiceDemandConfigurePayload](command)
		if err != nil {
			return cityServiceExecution{}, err
		}
		return s.configureCityServiceDemand(ctx, tx, worldID, targetTick, factSequence, command, payload)
	case CityCommandTypeServiceConnectionConfigure:
		payload, err := decodeStoredCityCommandPayload[cityServiceConnectionConfigurePayload](command)
		if err != nil {
			return cityServiceExecution{}, err
		}
		return s.configureCityServiceConnection(ctx, tx, worldID, targetTick, factSequence, command, payload)
	default:
		return cityServiceExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"command_type": command.CommandType})
	}
}

type cityFacilityRef struct {
	id                   int64
	code                 string
	name                 string
	facilityTypeID       int64
	facilityTypeCode     string
	facilityTypeVersion  string
	facilityTypeHash     string
	allowedServices      map[string]struct{}
	districtID           int64
	districtCode         string
	buildingID           int64
	buildingCode         string
	ownerEntityID        *int64
	ownerEntityCode      *string
	status               string
	reliabilityMilli     int
	lifecycleFactorMilli int
	lifecycleManaged     bool
	createdTick          int64
	updatedTick          int64
	version              int64
	metadata             json.RawMessage
}

type cityServiceDefinitionRef struct {
	id    int64
	value CityServiceDefinition
}

type cityServiceCapacityRef struct {
	id                     int64
	facilityID             int64
	facilityCode           string
	serviceDefinitionID    int64
	serviceCode            string
	installedCapacityUnits int64
	availabilityMilli      int
	availableCapacityUnits int64
	updatedTick            int64
	version                int64
	metadata               json.RawMessage
}

type cityServiceDemandRef struct {
	id                    int64
	code                  string
	serviceDefinitionID   int64
	serviceCode           string
	subjectKind           string
	subjectCode           string
	districtID            int64
	districtCode          string
	buildingID            *int64
	buildingCode          *string
	entityID              *int64
	actorID               *int64
	requestedUnitsPerTick int64
	priority              int
	status                string
	createdTick           int64
	updatedTick           int64
	version               int64
	metadata              json.RawMessage
}

type cityServiceConnectionRef struct {
	id                  int64
	code                string
	capacityID          int64
	demandID            int64
	maxFlowUnitsPerTick int64
	lossMilli           int
	preference          int
	status              string
	createdTick         int64
	updatedTick         int64
	version             int64
	metadata            json.RawMessage
}

func (s *CityEconomyService) registerCityFacility(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, payload cityFacilityRegisterPayload,
) (cityServiceExecution, error) {
	var duplicate int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_facilities
WHERE world_id = $1 AND (code = $2 OR building_id = (
    SELECT id FROM city_buildings WHERE world_id = $1 AND code = $3
))`, worldID, payload.Code, payload.BuildingCode).Scan(&duplicate); err != nil {
		return cityServiceExecution{}, fmt.Errorf("inspect city facility duplicate: %w", err)
	}
	if duplicate != 0 {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionFacilityConflict)
	}
	if err := ensureCityServiceProjectionSlot(ctx, tx, worldID, "facility"); err != nil {
		return cityServiceExecution{}, err
	}

	var typeID int64
	var typeVersion, typeHash string
	var minimumArea int64
	var defaultReliability int
	err := tx.QueryRowContext(ctx, `
SELECT id, definition_version, definition_hash, minimum_floor_area_sqm,
       default_reliability_milli
FROM city_facility_type_definitions
WHERE world_id = $1 AND code = $2 AND status = 'active'`,
		worldID, payload.FacilityTypeCode).Scan(
		&typeID, &typeVersion, &typeHash, &minimumArea, &defaultReliability,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionServiceNotAllowed)
	}
	if err != nil {
		return cityServiceExecution{}, fmt.Errorf("load city facility type: %w", err)
	}

	var buildingID, districtID, floorArea int64
	var districtCode string
	err = tx.QueryRowContext(ctx, `
SELECT building.id, district.id, district.code,
       building.floor_area_sqm + COALESCE((
           SELECT SUM(adjustment.added_floor_area_sqm)::BIGINT
           FROM city_building_adjustments adjustment
           WHERE adjustment.world_id = building.world_id
             AND adjustment.building_id = building.id
       ), 0)::BIGINT
FROM city_buildings building
JOIN city_districts district
  ON district.id = building.district_id AND district.world_id = building.world_id
WHERE building.world_id = $1 AND building.code = $2 AND building.status = 'active'
FOR UPDATE OF building`, worldID, payload.BuildingCode).Scan(
		&buildingID, &districtID, &districtCode, &floorArea,
	)
	if errors.Is(err, sql.ErrNoRows) || floorArea < minimumArea {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionBuildingInvalid)
	}
	if err != nil {
		return cityServiceExecution{}, fmt.Errorf("load city facility building: %w", err)
	}

	var ownerID any
	var ownerCode *string
	if payload.OwnerEntityCode != "" {
		var resolvedID int64
		err = tx.QueryRowContext(ctx, `
SELECT id FROM city_economic_entities
WHERE world_id = $1 AND code = $2 AND status = 'active'`,
			worldID, payload.OwnerEntityCode).Scan(&resolvedID)
		if errors.Is(err, sql.ErrNoRows) {
			return cityServiceExecution{}, cityServiceReject(cityServiceRejectionOwnerInvalid)
		}
		if err != nil {
			return cityServiceExecution{}, fmt.Errorf("load city facility owner: %w", err)
		}
		ownerID = resolvedID
		ownerCode = &payload.OwnerEntityCode
	}
	reliability := defaultReliability
	if payload.ReliabilityMilli != nil {
		reliability = *payload.ReliabilityMilli
	}
	after := CityFacility{
		Code: payload.Code, Name: payload.Name, FacilityTypeCode: payload.FacilityTypeCode,
		FacilityTypeVersion: typeVersion, FacilityTypeHash: typeHash,
		DistrictCode: districtCode, BuildingCode: payload.BuildingCode,
		OwnerEntityCode: ownerCode, Status: CityFacilityStatusOffline,
		ReliabilityMilli: reliability, CreatedTick: targetTick, UpdatedTick: targetTick,
		Version: 1, SourceFactTick: targetTick, SourceFactSequence: factSequence,
		Metadata: payload.Metadata,
	}
	fact, err := insertCityServiceFact(ctx, tx, cityServiceFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, factType: CityServiceFactFacilityRegistered,
		subjectKind: "facility", subjectCode: payload.Code, versionBefore: 0, versionAfter: 1,
		payload: map[string]any{"schema_version": 1, "facility_before": nil, "facility_after": after},
	})
	if err != nil {
		return cityServiceExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_facilities
    (world_id, code, name, facility_type_id, district_id, building_id,
     owner_entity_id, status, reliability_milli, created_tick, updated_tick,
     version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'offline', $8, $9, $9, 1, $10, $11::jsonb)`,
		worldID, payload.Code, payload.Name, typeID, districtID, buildingID,
		ownerID, reliability, targetTick, fact.id, payload.Metadata); err != nil {
		return cityServiceExecution{}, fmt.Errorf("insert city facility: %w", err)
	}
	if err = advanceCityServiceProfile(ctx, tx, worldID, cityServiceProfileDeltas{facilities: 1}); err != nil {
		return cityServiceExecution{}, err
	}
	if err = postCityServiceFact(ctx, tx, fact.id); err != nil {
		return cityServiceExecution{}, err
	}
	return appliedCityServiceExecution(command, fact, "city.service.facility.registered", map[string]any{
		"facility_code": payload.Code, "status": CityFacilityStatusOffline,
	}), nil
}

func (s *CityEconomyService) transitionCityFacilityStatus(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, payload cityFacilityStatusTransitionPayload,
) (cityServiceExecution, error) {
	facility, err := loadCityFacilityRef(ctx, tx, worldID, payload.FacilityCode, true)
	if err != nil {
		return cityServiceExecution{}, err
	}
	if facility.version != payload.ExpectedVersion {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionVersionConflict)
	}
	if !validCityFacilityTransition(facility.status, payload.ToStatus) {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionStateTransition)
	}
	before := cityFacilityFromRef(facility, 0, 0)
	after := before
	after.Status = payload.ToStatus
	after.UpdatedTick = targetTick
	after.Version++
	after.SourceFactTick = targetTick
	after.SourceFactSequence = factSequence
	after.Metadata = payload.Metadata
	fact, err := insertCityServiceFact(ctx, tx, cityServiceFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, factType: CityServiceFactFacilityStatusChanged,
		subjectKind: "facility", subjectCode: facility.code,
		versionBefore: before.Version, versionAfter: after.Version,
		payload: map[string]any{"schema_version": 1, "facility_before": before, "facility_after": after},
	})
	if err != nil {
		return cityServiceExecution{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_facilities
SET status = $3, updated_tick = $4, version = version + 1,
    source_fact_id = $5, metadata = $6::jsonb, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND version = $7`, worldID, facility.id,
		payload.ToStatus, targetTick, fact.id, payload.Metadata, payload.ExpectedVersion)
	if err != nil {
		return cityServiceExecution{}, fmt.Errorf("transition city facility status: %w", err)
	}
	if err = requireCityServiceRow(result, facility.code); err != nil {
		return cityServiceExecution{}, err
	}
	if err = advanceCityServiceProfile(ctx, tx, worldID, cityServiceProfileDeltas{}); err != nil {
		return cityServiceExecution{}, err
	}
	if err = postCityServiceFact(ctx, tx, fact.id); err != nil {
		return cityServiceExecution{}, err
	}
	return appliedCityServiceExecution(command, fact, "city.service.facility.status_changed", map[string]any{
		"facility_code": facility.code, "status": payload.ToStatus, "version": after.Version,
	}), nil
}

func validCityFacilityTransition(from, to string) bool {
	if from == to || from == CityFacilityStatusRetired {
		return false
	}
	switch from {
	case CityFacilityStatusOffline:
		return to == CityFacilityStatusOperational || to == CityFacilityStatusRetired
	case CityFacilityStatusOperational:
		return to == CityFacilityStatusDegraded || to == CityFacilityStatusOffline || to == CityFacilityStatusRetired
	case CityFacilityStatusDegraded:
		return to == CityFacilityStatusOperational || to == CityFacilityStatusOffline || to == CityFacilityStatusRetired
	default:
		return false
	}
}

func (s *CityEconomyService) configureCityFacilityCapacity(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, payload cityFacilityCapacityConfigurePayload,
) (cityServiceExecution, error) {
	facility, err := loadCityFacilityRef(ctx, tx, worldID, payload.FacilityCode, true)
	if err != nil {
		return cityServiceExecution{}, err
	}
	if facility.status == CityFacilityStatusRetired {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionStateTransition)
	}
	serviceDefinition, err := loadCityServiceDefinitionRef(ctx, tx, worldID, payload.ServiceCode)
	if err != nil {
		return cityServiceExecution{}, err
	}
	if _, allowed := facility.allowedServices[payload.ServiceCode]; !allowed {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionServiceNotAllowed)
	}
	before, err := loadOptionalCityServiceCapacityRef(ctx, tx, worldID, facility.id, serviceDefinition.id, true)
	if err != nil {
		return cityServiceExecution{}, err
	}
	if before == nil && payload.ExpectedVersion != 0 {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionCapacityNotFound)
	}
	if before != nil && before.version != payload.ExpectedVersion {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionVersionConflict)
	}
	if before == nil {
		if err = ensureCityServiceProjectionSlot(ctx, tx, worldID, "capacity"); err != nil {
			return cityServiceExecution{}, err
		}
	}
	versionBefore := int64(0)
	versionAfter := int64(1)
	var beforeValue any
	if before != nil {
		versionBefore, versionAfter = before.version, before.version+1
		beforeValue = cityServiceCapacityFromRef(
			before, facility.status, facility.lifecycleFactorMilli, 0, 0,
		)
	}
	available := payload.InstalledCapacityUnits * int64(payload.AvailabilityMilli) / 1000
	lifecycleFactor, err := predictCityFacilityLifecycleFactorAfterCapacity(
		ctx, tx, worldID, facility.id, serviceDefinition.id,
		payload.InstalledCapacityUnits, facility.lifecycleFactorMilli,
		facility.lifecycleManaged,
	)
	if err != nil {
		return cityServiceExecution{}, err
	}
	after := CityFacilityServiceCapacity{
		FacilityCode: facility.code, ServiceCode: serviceDefinition.value.Code,
		ServiceVersion:         serviceDefinition.value.DefinitionVersion,
		ServiceHash:            serviceDefinition.value.DefinitionHash,
		InstalledCapacityUnits: payload.InstalledCapacityUnits,
		AvailabilityMilli:      payload.AvailabilityMilli, AvailableCapacityUnits: available,
		DispatchCapacityUnits: cityFacilityEffectiveDispatchCapacity(
			facility.status, available, lifecycleFactor,
		),
		UpdatedTick: targetTick, Version: versionAfter,
		SourceFactTick: targetTick, SourceFactSequence: factSequence, Metadata: payload.Metadata,
	}
	subjectCode := facility.code + "." + serviceDefinition.value.Code
	fact, err := insertCityServiceFact(ctx, tx, cityServiceFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, factType: CityServiceFactFacilityCapacityConfigured,
		subjectKind: "capacity", subjectCode: subjectCode,
		versionBefore: versionBefore, versionAfter: versionAfter,
		payload: map[string]any{"schema_version": 1, "capacity_before": beforeValue, "capacity_after": after},
	})
	if err != nil {
		return cityServiceExecution{}, err
	}
	delta := int64(0)
	if before == nil {
		delta = 1
		_, err = tx.ExecContext(ctx, `
INSERT INTO city_facility_service_capacities
    (world_id, facility_id, service_definition_id, installed_capacity_units,
     availability_milli, available_capacity_units, updated_tick, version,
     source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8, $9::jsonb)`,
			worldID, facility.id, serviceDefinition.id, payload.InstalledCapacityUnits,
			payload.AvailabilityMilli, available, targetTick, fact.id, payload.Metadata)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `
UPDATE city_facility_service_capacities
SET installed_capacity_units = $4, availability_milli = $5,
    available_capacity_units = $6, updated_tick = $7, version = version + 1,
    source_fact_id = $8, metadata = $9::jsonb, updated_at = NOW()
WHERE world_id = $1 AND facility_id = $2 AND service_definition_id = $3 AND version = $10`,
			worldID, facility.id, serviceDefinition.id, payload.InstalledCapacityUnits,
			payload.AvailabilityMilli, available, targetTick, fact.id, payload.Metadata,
			payload.ExpectedVersion)
		if err == nil {
			err = requireCityServiceRow(result, subjectCode)
		}
	}
	if err != nil {
		return cityServiceExecution{}, fmt.Errorf("configure city facility capacity: %w", err)
	}
	if err = advanceCityServiceProfile(ctx, tx, worldID, cityServiceProfileDeltas{capacities: delta}); err != nil {
		return cityServiceExecution{}, err
	}
	if err = postCityServiceFact(ctx, tx, fact.id); err != nil {
		return cityServiceExecution{}, err
	}
	return appliedCityServiceExecution(command, fact, "city.service.capacity.configured", map[string]any{
		"facility_code": facility.code, "service_code": payload.ServiceCode,
		"available_capacity_units": available, "version": versionAfter,
	}), nil
}

type cityServiceSubjectRef struct {
	districtID   int64
	districtCode string
	buildingID   *int64
	buildingCode *string
	entityID     *int64
	actorID      *int64
}

func (s *CityEconomyService) configureCityServiceDemand(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, payload cityServiceDemandConfigurePayload,
) (cityServiceExecution, error) {
	serviceDefinition, err := loadCityServiceDefinitionRef(ctx, tx, worldID, payload.ServiceCode)
	if err != nil {
		return cityServiceExecution{}, err
	}
	subject, err := resolveCityServiceSubject(ctx, tx, worldID, payload.SubjectKind, payload.SubjectCode)
	if err != nil {
		return cityServiceExecution{}, err
	}
	before, err := loadOptionalCityServiceDemandRef(ctx, tx, worldID, payload.Code, true)
	if err != nil {
		return cityServiceExecution{}, err
	}
	if before == nil && payload.ExpectedVersion != 0 {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionDemandNotFound)
	}
	if before != nil {
		if before.version != payload.ExpectedVersion {
			return cityServiceExecution{}, cityServiceReject(cityServiceRejectionVersionConflict)
		}
		if before.status == CityServiceProjectionStatusRetired ||
			before.serviceDefinitionID != serviceDefinition.id || before.subjectKind != payload.SubjectKind ||
			before.subjectCode != payload.SubjectCode || before.districtID != subject.districtID ||
			!sameOptionalInt64(before.buildingID, subject.buildingID) ||
			!sameOptionalInt64(before.entityID, subject.entityID) || !sameOptionalInt64(before.actorID, subject.actorID) {
			return cityServiceExecution{}, cityServiceReject(cityServiceRejectionDemandConflict)
		}
	}
	if before == nil {
		if err = ensureCityServiceProjectionSlot(ctx, tx, worldID, "demand"); err != nil {
			return cityServiceExecution{}, err
		}
	}
	versionBefore, versionAfter := int64(0), int64(1)
	var beforeValue any
	createdTick := targetTick
	if before != nil {
		versionBefore, versionAfter, createdTick = before.version, before.version+1, before.createdTick
		beforeValue = cityServiceDemandFromRef(before, 0, 0)
	}
	after := CityServiceDemand{
		Code: payload.Code, ServiceCode: serviceDefinition.value.Code,
		ServiceVersion: serviceDefinition.value.DefinitionVersion,
		ServiceHash:    serviceDefinition.value.DefinitionHash,
		SubjectKind:    payload.SubjectKind, SubjectCode: payload.SubjectCode,
		DistrictCode: subject.districtCode, BuildingCode: subject.buildingCode,
		RequestedUnitsPerTick: payload.RequestedUnitsPerTick, Priority: payload.Priority,
		Status: payload.Status, CreatedTick: createdTick, UpdatedTick: targetTick,
		Version: versionAfter, SourceFactTick: targetTick,
		SourceFactSequence: factSequence, Metadata: payload.Metadata,
	}
	fact, err := insertCityServiceFact(ctx, tx, cityServiceFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, factType: CityServiceFactDemandConfigured,
		subjectKind: "demand", subjectCode: payload.Code,
		versionBefore: versionBefore, versionAfter: versionAfter,
		payload: map[string]any{"schema_version": 1, "demand_before": beforeValue, "demand_after": after},
	})
	if err != nil {
		return cityServiceExecution{}, err
	}
	delta := int64(0)
	if before == nil {
		delta = 1
		_, err = tx.ExecContext(ctx, `
INSERT INTO city_service_demands
    (world_id, code, service_definition_id, subject_kind, subject_code,
     district_id, building_id, entity_id, actor_id, requested_units_per_tick,
     priority, status, created_tick, updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $13, 1, $14, $15::jsonb)`, worldID, payload.Code,
			serviceDefinition.id, payload.SubjectKind, payload.SubjectCode, subject.districtID,
			optionalInt64Value(subject.buildingID), optionalInt64Value(subject.entityID),
			optionalInt64Value(subject.actorID), payload.RequestedUnitsPerTick,
			payload.Priority, payload.Status, targetTick, fact.id, payload.Metadata)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `
UPDATE city_service_demands
SET requested_units_per_tick = $3, priority = $4, status = $5,
    updated_tick = $6, version = version + 1, source_fact_id = $7,
    metadata = $8::jsonb, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND version = $9`, worldID, before.id,
			payload.RequestedUnitsPerTick, payload.Priority, payload.Status,
			targetTick, fact.id, payload.Metadata, payload.ExpectedVersion)
		if err == nil {
			err = requireCityServiceRow(result, payload.Code)
		}
	}
	if err != nil {
		return cityServiceExecution{}, fmt.Errorf("configure city service demand: %w", err)
	}
	if err = advanceCityServiceProfile(ctx, tx, worldID, cityServiceProfileDeltas{demands: delta}); err != nil {
		return cityServiceExecution{}, err
	}
	if err = postCityServiceFact(ctx, tx, fact.id); err != nil {
		return cityServiceExecution{}, err
	}
	return appliedCityServiceExecution(command, fact, "city.service.demand.configured", map[string]any{
		"demand_code": payload.Code, "service_code": payload.ServiceCode,
		"status": payload.Status, "version": versionAfter,
	}), nil
}

func (s *CityEconomyService) configureCityServiceConnection(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, payload cityServiceConnectionConfigurePayload,
) (cityServiceExecution, error) {
	facility, err := loadCityFacilityRef(ctx, tx, worldID, payload.FacilityCode, false)
	if err != nil {
		return cityServiceExecution{}, err
	}
	serviceDefinition, err := loadCityServiceDefinitionRef(ctx, tx, worldID, payload.ServiceCode)
	if err != nil {
		return cityServiceExecution{}, err
	}
	capacity, err := loadOptionalCityServiceCapacityRef(ctx, tx, worldID, facility.id, serviceDefinition.id, false)
	if err != nil {
		return cityServiceExecution{}, err
	}
	if capacity == nil {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionCapacityNotFound)
	}
	demand, err := loadOptionalCityServiceDemandRef(ctx, tx, worldID, payload.DemandCode, false)
	if err != nil {
		return cityServiceExecution{}, err
	}
	if demand == nil {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionDemandNotFound)
	}
	if demand.serviceDefinitionID != serviceDefinition.id {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionServiceMismatch)
	}
	before, err := loadOptionalCityServiceConnectionRef(ctx, tx, worldID, payload.Code, true)
	if err != nil {
		return cityServiceExecution{}, err
	}
	if before == nil && payload.ExpectedVersion != 0 {
		return cityServiceExecution{}, cityServiceReject(cityServiceRejectionConnectionNotFound)
	}
	if before != nil {
		if before.version != payload.ExpectedVersion {
			return cityServiceExecution{}, cityServiceReject(cityServiceRejectionVersionConflict)
		}
		if before.status == CityServiceProjectionStatusRetired ||
			before.capacityID != capacity.id || before.demandID != demand.id {
			return cityServiceExecution{}, cityServiceReject(cityServiceRejectionConnectionConflict)
		}
	}
	if before == nil {
		if err = ensureCityServiceProjectionSlot(ctx, tx, worldID, "connection"); err != nil {
			return cityServiceExecution{}, err
		}
	}
	versionBefore, versionAfter := int64(0), int64(1)
	createdTick := targetTick
	var beforeValue any
	if before != nil {
		versionBefore, versionAfter, createdTick = before.version, before.version+1, before.createdTick
		beforeValue = cityServiceConnectionFromRef(before, capacity, demand, 0, 0)
	}
	after := CityServiceConnection{
		Code: payload.Code, FacilityCode: facility.code, ServiceCode: payload.ServiceCode,
		DemandCode: payload.DemandCode, MaxFlowUnitsPerTick: payload.MaxFlowUnitsPerTick,
		LossMilli: payload.LossMilli, Preference: payload.Preference, Status: payload.Status,
		CreatedTick: createdTick, UpdatedTick: targetTick, Version: versionAfter,
		SourceFactTick: targetTick, SourceFactSequence: factSequence, Metadata: payload.Metadata,
	}
	fact, err := insertCityServiceFact(ctx, tx, cityServiceFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, factType: CityServiceFactConnectionConfigured,
		subjectKind: "connection", subjectCode: payload.Code,
		versionBefore: versionBefore, versionAfter: versionAfter,
		payload: map[string]any{"schema_version": 1, "connection_before": beforeValue, "connection_after": after},
	})
	if err != nil {
		return cityServiceExecution{}, err
	}
	delta := int64(0)
	if before == nil {
		delta = 1
		_, err = tx.ExecContext(ctx, `
INSERT INTO city_service_connections
    (world_id, code, capacity_id, demand_id, max_flow_units_per_tick,
     loss_milli, preference, status, created_tick, updated_tick, version,
     source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9, 1, $10, $11::jsonb)`,
			worldID, payload.Code, capacity.id, demand.id, payload.MaxFlowUnitsPerTick,
			payload.LossMilli, payload.Preference, payload.Status, targetTick, fact.id, payload.Metadata)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `
UPDATE city_service_connections
SET max_flow_units_per_tick = $3, loss_milli = $4, preference = $5,
    status = $6, updated_tick = $7, version = version + 1,
    source_fact_id = $8, metadata = $9::jsonb, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND version = $10`, worldID, before.id,
			payload.MaxFlowUnitsPerTick, payload.LossMilli, payload.Preference,
			payload.Status, targetTick, fact.id, payload.Metadata, payload.ExpectedVersion)
		if err == nil {
			err = requireCityServiceRow(result, payload.Code)
		}
	}
	if err != nil {
		return cityServiceExecution{}, fmt.Errorf("configure city service connection: %w", err)
	}
	if err = advanceCityServiceProfile(ctx, tx, worldID, cityServiceProfileDeltas{connections: delta}); err != nil {
		return cityServiceExecution{}, err
	}
	if err = postCityServiceFact(ctx, tx, fact.id); err != nil {
		return cityServiceExecution{}, err
	}
	return appliedCityServiceExecution(command, fact, "city.service.connection.configured", map[string]any{
		"connection_code": payload.Code, "facility_code": payload.FacilityCode,
		"demand_code": payload.DemandCode, "status": payload.Status, "version": versionAfter,
	}), nil
}

type cityServiceFactInsert struct {
	worldID         int64
	tick            int64
	sequence        int64
	sourceCommandID *int64
	factType        string
	subjectKind     string
	subjectCode     string
	versionBefore   int64
	versionAfter    int64
	payload         map[string]any
}

func insertCityServiceFact(ctx context.Context, tx *sql.Tx, input cityServiceFactInsert) (*cityServiceFactRecord, error) {
	payload, err := json.Marshal(input.payload)
	if err != nil {
		return nil, fmt.Errorf("marshal city service fact: %w", err)
	}
	var factID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_service_facts
    (world_id, tick, sequence, source_command_id, fact_type, subject_kind,
     subject_code, version_before, version_after, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)
RETURNING id`, input.worldID, input.tick, input.sequence,
		optionalInt64Value(input.sourceCommandID), input.factType, input.subjectKind,
		input.subjectCode, input.versionBefore, input.versionAfter, payload).Scan(&factID)
	if err != nil {
		return nil, fmt.Errorf("insert city service fact draft: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_service_fact_id', $1, TRUE)`,
		strconv.FormatInt(factID, 10)); err != nil {
		return nil, fmt.Errorf("activate city service fact gate: %w", err)
	}
	fact := CityServiceFact{
		Tick: input.tick, Sequence: input.sequence, FactType: input.factType,
		SubjectKind: input.subjectKind, SubjectCode: input.subjectCode,
		VersionBefore: input.versionBefore, VersionAfter: input.versionAfter, Payload: payload,
	}
	return &cityServiceFactRecord{id: factID, fact: fact}, nil
}

func postCityServiceFact(ctx context.Context, tx *sql.Tx, factID int64) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_service_facts SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, factID)
	if err != nil {
		return fmt.Errorf("post city service fact: %w", err)
	}
	return requireCityServiceRow(result, strconv.FormatInt(factID, 10))
}

func advanceCityServiceProfile(ctx context.Context, tx *sql.Tx, worldID int64, delta cityServiceProfileDeltas) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_service_profiles
SET facility_count = facility_count + $2,
    capacity_count = capacity_count + $3,
    demand_count = demand_count + $4,
    connection_count = connection_count + $5,
    fact_count = fact_count + 1,
    allocation_count = allocation_count + $6,
    settlement_count = settlement_count + $7,
    revision = revision + 1, updated_at = NOW()
WHERE world_id = $1`, worldID, delta.facilities, delta.capacities, delta.demands,
		delta.connections, delta.allocations, delta.settlements)
	if err != nil {
		return fmt.Errorf("advance city service profile: %w", err)
	}
	return requireCityServiceRow(result, strconv.FormatInt(worldID, 10))
}

func requireCityServiceRow(result sql.Result, subject string) error {
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"city_service_subject": subject})
	}
	return nil
}

func ensureCityServiceProjectionSlot(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	kind string,
) error {
	var facilities, capacities, demands, connections int64
	if err := queryer.QueryRowContext(ctx, `
SELECT facility_count, capacity_count, demand_count, connection_count
FROM city_service_profiles WHERE world_id = $1`, worldID).Scan(
		&facilities, &capacities, &demands, &connections,
	); err != nil {
		return fmt.Errorf("load city service projection limits: %w", err)
	}
	var count int64
	switch kind {
	case "facility":
		count = facilities
	case "capacity":
		count = capacities
	case "demand":
		count = demands
	case "connection":
		count = connections
	default:
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_projection_kind"})
	}
	if count >= cityServiceMaximumProjectionCount {
		return cityServiceReject(cityServiceRejectionProjectionLimit)
	}
	return nil
}

func appliedCityServiceExecution(command *CityCommand, fact *cityServiceFactRecord, eventType string, result map[string]any) cityServiceExecution {
	payload := map[string]any{
		"fact_type": fact.fact.FactType, "subject_kind": fact.fact.SubjectKind,
		"subject_code": fact.fact.SubjectCode, "version_before": fact.fact.VersionBefore,
		"version_after": fact.fact.VersionAfter,
	}
	return cityServiceExecution{
		pending: cityPendingEvent{command: command, status: CityCommandStatusApplied,
			eventType: eventType, payload: payload, result: result},
		fact: &fact.fact,
	}
}

func loadCityServiceDefinitionRef(ctx context.Context, queryer citySQLQueryer, worldID int64, code string) (*cityServiceDefinitionRef, error) {
	ref := &cityServiceDefinitionRef{}
	err := queryer.QueryRowContext(ctx, `
SELECT id, code, definition_version, definition_hash, name, category,
       unit_code, flow_kind, status, sort_order, payload
FROM city_service_definitions
WHERE world_id = $1 AND code = $2 AND status = 'active'`, worldID, code).Scan(
		&ref.id, &ref.value.Code, &ref.value.DefinitionVersion,
		&ref.value.DefinitionHash, &ref.value.Name, &ref.value.Category,
		&ref.value.UnitCode, &ref.value.FlowKind, &ref.value.Status,
		&ref.value.SortOrder, &ref.value.Payload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityServiceReject(cityServiceRejectionServiceMismatch)
	}
	if err != nil {
		return nil, fmt.Errorf("load city service definition: %w", err)
	}
	return ref, nil
}

func loadCityFacilityRef(ctx context.Context, queryer citySQLQueryer, worldID int64, code string, lock bool) (*cityFacilityRef, error) {
	query := `
SELECT facility.id, facility.code, facility.name, type.id, type.code,
       type.definition_version, type.definition_hash, type.allowed_service_codes,
       district.id, district.code, building.id, building.code,
       facility.owner_entity_id, owner.code, facility.status,
       facility.reliability_milli, facility.created_tick, facility.updated_tick,
       facility.version, facility.metadata,
       COALESCE(lifecycle.effective_factor_milli, 1000),
       lifecycle.facility_id IS NOT NULL
FROM city_facilities facility
JOIN city_facility_type_definitions type ON type.id = facility.facility_type_id
JOIN city_districts district ON district.id = facility.district_id
JOIN city_buildings building ON building.id = facility.building_id
LEFT JOIN city_economic_entities owner ON owner.id = facility.owner_entity_id
LEFT JOIN city_facility_lifecycle_states lifecycle
  ON lifecycle.world_id = facility.world_id AND lifecycle.facility_id = facility.id
WHERE facility.world_id = $1 AND facility.code = $2`
	if lock {
		query += ` FOR UPDATE OF facility`
	}
	ref := &cityFacilityRef{}
	var allowed json.RawMessage
	var ownerID sql.NullInt64
	var ownerCode sql.NullString
	err := queryer.QueryRowContext(ctx, query, worldID, code).Scan(
		&ref.id, &ref.code, &ref.name, &ref.facilityTypeID, &ref.facilityTypeCode,
		&ref.facilityTypeVersion, &ref.facilityTypeHash, &allowed,
		&ref.districtID, &ref.districtCode, &ref.buildingID, &ref.buildingCode,
		&ownerID, &ownerCode, &ref.status, &ref.reliabilityMilli, &ref.createdTick,
		&ref.updatedTick, &ref.version, &ref.metadata, &ref.lifecycleFactorMilli,
		&ref.lifecycleManaged,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityServiceReject(cityServiceRejectionFacilityNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load city facility: %w", err)
	}
	ref.ownerEntityID = nullInt64Pointer(ownerID)
	ref.ownerEntityCode = nullStringPointer(ownerCode)
	var allowedCodes []string
	if err = json.Unmarshal(allowed, &allowedCodes); err != nil {
		return nil, fmt.Errorf("decode facility allowed services: %w", err)
	}
	ref.allowedServices = make(map[string]struct{}, len(allowedCodes))
	for _, serviceCode := range allowedCodes {
		ref.allowedServices[serviceCode] = struct{}{}
	}
	return ref, nil
}

func loadOptionalCityServiceCapacityRef(ctx context.Context, queryer citySQLQueryer, worldID, facilityID, serviceID int64, lock bool) (*cityServiceCapacityRef, error) {
	query := `
SELECT capacity.id, capacity.facility_id, facility.code,
       capacity.service_definition_id, service.code,
       capacity.installed_capacity_units, capacity.availability_milli,
       capacity.available_capacity_units, capacity.updated_tick,
       capacity.version, capacity.metadata
FROM city_facility_service_capacities capacity
JOIN city_facilities facility ON facility.id = capacity.facility_id
JOIN city_service_definitions service ON service.id = capacity.service_definition_id
WHERE capacity.world_id = $1 AND capacity.facility_id = $2
  AND capacity.service_definition_id = $3`
	if lock {
		query += ` FOR UPDATE OF capacity`
	}
	ref := &cityServiceCapacityRef{}
	err := queryer.QueryRowContext(ctx, query, worldID, facilityID, serviceID).Scan(
		&ref.id, &ref.facilityID, &ref.facilityCode, &ref.serviceDefinitionID,
		&ref.serviceCode, &ref.installedCapacityUnits, &ref.availabilityMilli,
		&ref.availableCapacityUnits, &ref.updatedTick, &ref.version, &ref.metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load city service capacity: %w", err)
	}
	return ref, nil
}

func loadOptionalCityServiceDemandRef(ctx context.Context, queryer citySQLQueryer, worldID int64, code string, lock bool) (*cityServiceDemandRef, error) {
	query := `
SELECT demand.id, demand.code, demand.service_definition_id, service.code,
       demand.subject_kind, demand.subject_code, district.id, district.code,
       demand.building_id, building.code, demand.entity_id, demand.actor_id,
       demand.requested_units_per_tick, demand.priority, demand.status,
       demand.created_tick, demand.updated_tick, demand.version, demand.metadata
FROM city_service_demands demand
JOIN city_service_definitions service ON service.id = demand.service_definition_id
JOIN city_districts district ON district.id = demand.district_id
LEFT JOIN city_buildings building ON building.id = demand.building_id
WHERE demand.world_id = $1 AND demand.code = $2`
	if lock {
		query += ` FOR UPDATE OF demand`
	}
	ref := &cityServiceDemandRef{}
	var buildingID, entityID, actorID sql.NullInt64
	var buildingCode sql.NullString
	err := queryer.QueryRowContext(ctx, query, worldID, code).Scan(
		&ref.id, &ref.code, &ref.serviceDefinitionID, &ref.serviceCode,
		&ref.subjectKind, &ref.subjectCode, &ref.districtID, &ref.districtCode,
		&buildingID, &buildingCode, &entityID, &actorID, &ref.requestedUnitsPerTick,
		&ref.priority, &ref.status, &ref.createdTick, &ref.updatedTick,
		&ref.version, &ref.metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load city service demand: %w", err)
	}
	ref.buildingID, ref.entityID, ref.actorID = nullInt64Pointer(buildingID), nullInt64Pointer(entityID), nullInt64Pointer(actorID)
	ref.buildingCode = nullStringPointer(buildingCode)
	return ref, nil
}

func loadOptionalCityServiceConnectionRef(ctx context.Context, queryer citySQLQueryer, worldID int64, code string, lock bool) (*cityServiceConnectionRef, error) {
	query := `
SELECT id, code, capacity_id, demand_id, max_flow_units_per_tick,
       loss_milli, preference, status, created_tick, updated_tick, version, metadata
FROM city_service_connections WHERE world_id = $1 AND code = $2`
	if lock {
		query += ` FOR UPDATE`
	}
	ref := &cityServiceConnectionRef{}
	err := queryer.QueryRowContext(ctx, query, worldID, code).Scan(
		&ref.id, &ref.code, &ref.capacityID, &ref.demandID,
		&ref.maxFlowUnitsPerTick, &ref.lossMilli, &ref.preference, &ref.status,
		&ref.createdTick, &ref.updatedTick, &ref.version, &ref.metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load city service connection: %w", err)
	}
	return ref, nil
}

func resolveCityServiceSubject(ctx context.Context, queryer citySQLQueryer, worldID int64, kind, code string) (*cityServiceSubjectRef, error) {
	ref := &cityServiceSubjectRef{}
	var err error
	switch kind {
	case "district":
		err = queryer.QueryRowContext(ctx, `
SELECT id, code FROM city_districts WHERE world_id = $1 AND code = $2`, worldID, code).Scan(&ref.districtID, &ref.districtCode)
	case "building":
		var buildingID int64
		var buildingCode string
		err = queryer.QueryRowContext(ctx, `
SELECT district.id, district.code, building.id, building.code
FROM city_buildings building
JOIN city_districts district ON district.id = building.district_id
WHERE building.world_id = $1 AND building.code = $2 AND building.status = 'active'`, worldID, code).Scan(
			&ref.districtID, &ref.districtCode, &buildingID, &buildingCode)
		ref.buildingID, ref.buildingCode = &buildingID, &buildingCode
	case "household":
		var entityID int64
		err = queryer.QueryRowContext(ctx, `
SELECT district.id, district.code, entity.id
FROM city_economic_entities entity
JOIN city_household_cohorts cohort
  ON cohort.entity_id = entity.id AND cohort.world_id = entity.world_id
JOIN city_districts district
  ON district.id = cohort.district_id AND district.world_id = cohort.world_id
WHERE entity.world_id = $1 AND entity.code = $2 AND entity.entity_type = 'household'
  AND entity.status = 'active'`, worldID, code).Scan(&ref.districtID, &ref.districtCode, &entityID)
		ref.entityID = &entityID
	case "enterprise":
		var entityID int64
		err = queryer.QueryRowContext(ctx, `
SELECT district.id, district.code, entity.id
FROM city_economic_entities entity
JOIN city_firm_states firm ON firm.entity_id = entity.id AND firm.world_id = entity.world_id
JOIN city_districts district ON district.id = firm.district_id AND district.world_id = firm.world_id
WHERE entity.world_id = $1 AND entity.code = $2 AND entity.entity_type = 'firm'
  AND entity.status = 'active'`, worldID, code).Scan(&ref.districtID, &ref.districtCode, &entityID)
		ref.entityID = &entityID
	case "actor":
		var actorID int64
		err = queryer.QueryRowContext(ctx, `
SELECT district.id, district.code, actor.id
FROM world_actors actor
JOIN world_actor_locations location
  ON location.actor_id = actor.id AND location.world_id = actor.world_id
JOIN city_districts district
  ON district.world_id = actor.world_id AND district.code = location.jurisdiction_code
WHERE actor.world_id = $1 AND actor.code = $2 AND actor.status = 'active'`, worldID, code).Scan(
			&ref.districtID, &ref.districtCode, &actorID)
		ref.actorID = &actorID
	default:
		return nil, cityServiceReject(cityServiceRejectionSubjectInvalid)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityServiceReject(cityServiceRejectionSubjectInvalid)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve city service subject: %w", err)
	}
	return ref, nil
}

func cityFacilityFromRef(ref *cityFacilityRef, factTick, factSequence int64) CityFacility {
	return CityFacility{
		Code: ref.code, Name: ref.name, FacilityTypeCode: ref.facilityTypeCode,
		FacilityTypeVersion: ref.facilityTypeVersion, FacilityTypeHash: ref.facilityTypeHash,
		DistrictCode: ref.districtCode, BuildingCode: ref.buildingCode,
		OwnerEntityCode: ref.ownerEntityCode, Status: ref.status,
		ReliabilityMilli: ref.reliabilityMilli, CreatedTick: ref.createdTick,
		UpdatedTick: ref.updatedTick, Version: ref.version,
		SourceFactTick: factTick, SourceFactSequence: factSequence, Metadata: ref.metadata,
	}
}

func cityServiceCapacityFromRef(
	ref *cityServiceCapacityRef, facilityStatus string, lifecycleFactor int,
	factTick, factSequence int64,
) CityFacilityServiceCapacity {
	return CityFacilityServiceCapacity{
		FacilityCode: ref.facilityCode, ServiceCode: ref.serviceCode,
		InstalledCapacityUnits: ref.installedCapacityUnits,
		AvailabilityMilli:      ref.availabilityMilli, AvailableCapacityUnits: ref.availableCapacityUnits,
		DispatchCapacityUnits: cityFacilityEffectiveDispatchCapacity(
			facilityStatus, ref.availableCapacityUnits, lifecycleFactor,
		),
		UpdatedTick: ref.updatedTick, Version: ref.version,
		SourceFactTick: factTick, SourceFactSequence: factSequence, Metadata: ref.metadata,
	}
}

func cityServiceDemandFromRef(ref *cityServiceDemandRef, factTick, factSequence int64) CityServiceDemand {
	return CityServiceDemand{
		Code: ref.code, ServiceCode: ref.serviceCode, SubjectKind: ref.subjectKind,
		SubjectCode: ref.subjectCode, DistrictCode: ref.districtCode,
		BuildingCode: ref.buildingCode, RequestedUnitsPerTick: ref.requestedUnitsPerTick,
		Priority: ref.priority, Status: ref.status, CreatedTick: ref.createdTick,
		UpdatedTick: ref.updatedTick, Version: ref.version,
		SourceFactTick: factTick, SourceFactSequence: factSequence, Metadata: ref.metadata,
	}
}

func cityServiceConnectionFromRef(ref *cityServiceConnectionRef, capacity *cityServiceCapacityRef, demand *cityServiceDemandRef, factTick, factSequence int64) CityServiceConnection {
	return CityServiceConnection{
		Code: ref.code, FacilityCode: capacity.facilityCode, ServiceCode: capacity.serviceCode,
		DemandCode: demand.code, MaxFlowUnitsPerTick: ref.maxFlowUnitsPerTick,
		LossMilli: ref.lossMilli, Preference: ref.preference, Status: ref.status,
		CreatedTick: ref.createdTick, UpdatedTick: ref.updatedTick, Version: ref.version,
		SourceFactTick: factTick, SourceFactSequence: factSequence, Metadata: ref.metadata,
	}
}

func cityServiceDispatchCapacity(status string, available int64) int64 {
	if status == CityFacilityStatusOperational || status == CityFacilityStatusDegraded {
		return available
	}
	return 0
}

func predictCityFacilityLifecycleFactorAfterCapacity(
	ctx context.Context, queryer citySQLQueryer,
	worldID, facilityID, serviceDefinitionID, installedCapacity int64,
	currentFactor int, lifecycleManaged bool,
) (int, error) {
	if !lifecycleManaged {
		return 1000, nil
	}
	var lifecycleStatus string
	var condition, operationFactor int
	var assigned, capacityPerStaff, otherInstalled int64
	err := queryer.QueryRowContext(ctx, `
SELECT state.lifecycle_status, state.condition_milli,
       state.staff_assigned_units, state.operation_factor_milli,
       policy.capacity_units_per_staff,
       COALESCE((
           SELECT SUM(capacity.installed_capacity_units)::BIGINT
           FROM city_facility_service_capacities capacity
           WHERE capacity.world_id = state.world_id
             AND capacity.facility_id = state.facility_id
             AND capacity.service_definition_id <> $3
       ), 0)
FROM city_facility_lifecycle_states state
JOIN city_facility_lifecycle_policies policy
  ON policy.world_id = state.world_id AND policy.id = state.policy_id
WHERE state.world_id = $1 AND state.facility_id = $2`,
		worldID, facilityID, serviceDefinitionID).Scan(
		&lifecycleStatus, &condition, &assigned, &operationFactor,
		&capacityPerStaff, &otherInstalled,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return currentFactor, nil
	}
	if err != nil {
		return 0, fmt.Errorf("predict facility lifecycle capacity factor: %w", err)
	}
	if otherInstalled > math.MaxInt64-installedCapacity {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "facility_installed_capacity"})
	}
	total := otherInstalled + installedCapacity
	required := int64(0)
	if total > 0 {
		required = (total + capacityPerStaff - 1) / capacityPerStaff
	}
	staffing := cityFacilityStaffingFactor(required, assigned)
	return cityFacilityLifecycleEffectiveFactor(
		lifecycleStatus, condition, staffing, operationFactor,
	), nil
}

func cityFacilitySettlementLifecycleFactor(facility cityFacilityRef) int {
	if !facility.lifecycleManaged {
		return 1000
	}
	return facility.lifecycleFactorMilli
}

func sameOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func optionalInt64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

// Stable settlement planning is pure and uses integer arithmetic only.
type cityServiceSettlementPlanInput struct {
	Demand      cityServiceDemandRef
	Connections []cityServiceSettlementConnectionInput
}

type cityServiceSettlementConnectionInput struct {
	Connection cityServiceConnectionRef
	Capacity   cityServiceCapacityRef
	Facility   cityFacilityRef
}

type cityServiceAllocationPlan struct {
	ConnectionID            int64
	ConnectionCode          string
	CapacityID              int64
	FacilityID              int64
	FacilityCode            string
	ServiceDefinitionID     int64
	ServiceCode             string
	CapacityVersion         int64
	DemandVersion           int64
	ConnectionVersion       int64
	FacilityCapacityUnits   int64
	ConnectionCapacityUnits int64
	LossMilli               int
	DispatchedUnits         int64
	NetworkCode             string
	NetworkReceivedUnits    int64
	NetworkLossUnits        int64
	ConnectionLossUnits     int64
	NetworkPaths            []cityNetworkPathPlan
	DeliveredUnits          int64
	LossUnits               int64
}

type cityServiceDemandSettlementPlan struct {
	DemandID       int64
	DemandCode     string
	ServiceID      int64
	ServiceCode    string
	DemandVersion  int64
	RequestedUnits int64
	DeliveredUnits int64
	ShortageUnits  int64
	QualityMilli   int
	Allocations    []cityServiceAllocationPlan
}

func planCityServiceSettlements(inputs []cityServiceSettlementPlanInput) ([]cityServiceDemandSettlementPlan, error) {
	return planCityServiceSettlementsWithNetworks(inputs, nil)
}

func planCityServiceSettlementsWithNetworks(
	inputs []cityServiceSettlementPlanInput,
	networks *cityServicePhysicalNetworkPlanningState,
) ([]cityServiceDemandSettlementPlan, error) {
	ordered := append([]cityServiceSettlementPlanInput(nil), inputs...)
	if err := validateCityServiceSettlementInputs(ordered); err != nil {
		return nil, err
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i].Demand, ordered[j].Demand
		if left.serviceCode != right.serviceCode {
			return left.serviceCode < right.serviceCode
		}
		if left.priority != right.priority {
			return left.priority > right.priority
		}
		if left.createdTick != right.createdTick {
			return left.createdTick < right.createdTick
		}
		return left.code < right.code
	})
	remainingCapacity := make(map[int64]int64)
	plans := make([]cityServiceDemandSettlementPlan, 0, len(ordered))
	for _, input := range ordered {
		demand := input.Demand
		connections := append([]cityServiceSettlementConnectionInput(nil), input.Connections...)
		sort.SliceStable(connections, func(i, j int) bool {
			if connections[i].Connection.preference != connections[j].Connection.preference {
				return connections[i].Connection.preference > connections[j].Connection.preference
			}
			return connections[i].Connection.code < connections[j].Connection.code
		})
		plan := cityServiceDemandSettlementPlan{
			DemandID: demand.id, DemandCode: demand.code,
			ServiceID: demand.serviceDefinitionID, ServiceCode: demand.serviceCode,
			DemandVersion: demand.version, RequestedUnits: demand.requestedUnitsPerTick,
			Allocations: make([]cityServiceAllocationPlan, 0),
		}
		remainingDemand := demand.requestedUnitsPerTick
		for _, inputConnection := range connections {
			connection, capacity, facility := inputConnection.Connection, inputConnection.Capacity, inputConnection.Facility
			if remainingDemand == 0 {
				break
			}
			facilityLimit, initialized := remainingCapacity[capacity.id]
			if !initialized {
				facilityLimit = cityFacilityEffectiveDispatchCapacity(
					facility.status, capacity.availableCapacityUnits,
					cityFacilitySettlementLifecycleFactor(facility),
				)
				remainingCapacity[capacity.id] = facilityLimit
			}
			if facilityLimit <= 0 {
				continue
			}
			retention := int64(1000 - connection.lossMilli)
			maximumInput := minCityServiceInt64(connection.maxFlowUnitsPerTick, facilityLimit)
			var dispatch, networkReceived, networkLoss int64
			var networkCode string
			var networkPaths []cityNetworkPathPlan
			if networks != nil && networks.usesNetwork(demand.serviceCode) {
				maximumReceived := (remainingDemand*1000 + retention - 1) / retention
				route, code, routeErr := networks.route(
					demand.serviceCode, connection.id, capacity.id, demand.id,
					connection.code, maximumInput, maximumReceived,
				)
				if routeErr != nil {
					return nil, routeErr
				}
				dispatch = route.DispatchedUnits
				networkReceived = route.NetworkReceivedUnits
				networkLoss = route.NetworkLossUnits
				networkCode = code
				networkPaths = route.Paths
			} else {
				requiredDispatch := (remainingDemand*1000 + retention - 1) / retention
				dispatch = minCityServiceInt64(requiredDispatch, maximumInput)
				networkReceived = dispatch
			}
			if dispatch <= 0 || networkReceived <= 0 {
				continue
			}
			delivered := networkReceived * retention / 1000
			if delivered <= 0 {
				continue
			}
			if delivered > remainingDemand {
				delivered = remainingDemand
			}
			connectionLoss := networkReceived - delivered
			loss := networkLoss + connectionLoss
			plan.Allocations = append(plan.Allocations, cityServiceAllocationPlan{
				ConnectionID: connection.id, ConnectionCode: connection.code,
				CapacityID: capacity.id, FacilityID: facility.id, FacilityCode: facility.code,
				ServiceDefinitionID: capacity.serviceDefinitionID, ServiceCode: capacity.serviceCode,
				CapacityVersion: capacity.version, DemandVersion: demand.version,
				ConnectionVersion: connection.version,
				FacilityCapacityUnits: cityFacilityEffectiveDispatchCapacity(
					facility.status, capacity.availableCapacityUnits,
					cityFacilitySettlementLifecycleFactor(facility),
				),
				ConnectionCapacityUnits: connection.maxFlowUnitsPerTick, LossMilli: connection.lossMilli,
				DispatchedUnits: dispatch, NetworkCode: networkCode,
				NetworkReceivedUnits: networkReceived, NetworkLossUnits: networkLoss,
				ConnectionLossUnits: connectionLoss, NetworkPaths: networkPaths,
				DeliveredUnits: delivered, LossUnits: loss,
			})
			remainingCapacity[capacity.id] = facilityLimit - dispatch
			remainingDemand -= delivered
			if len(plan.Allocations) > cityServiceMaximumAllocationsPerSettlement {
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_allocation_limit"})
			}
		}
		plan.DeliveredUnits = plan.RequestedUnits - remainingDemand
		plan.ShortageUnits = remainingDemand
		plan.QualityMilli = 1000
		if plan.RequestedUnits > 0 {
			plan.QualityMilli = int(plan.DeliveredUnits * 1000 / plan.RequestedUnits)
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

type cityServiceCapacityPlanIdentity struct {
	facilityID             int64
	serviceDefinitionID    int64
	installedCapacityUnits int64
	availabilityMilli      int
	availableCapacityUnits int64
	version                int64
	facilityStatus         string
}

func validateCityServiceSettlementInputs(inputs []cityServiceSettlementPlanInput) error {
	demandIDs := make(map[int64]struct{}, len(inputs))
	demandCodes := make(map[string]struct{}, len(inputs))
	connectionIDs := make(map[int64]struct{})
	connectionCodes := make(map[string]struct{})
	capacities := make(map[int64]cityServiceCapacityPlanIdentity)
	for _, input := range inputs {
		demand := input.Demand
		if demand.id <= 0 || demand.serviceDefinitionID <= 0 || demand.version <= 0 ||
			!cityServiceCodePattern.MatchString(demand.code) ||
			!cityServiceCodePattern.MatchString(demand.serviceCode) ||
			demand.status != CityServiceProjectionStatusActive ||
			demand.requestedUnitsPerTick < 0 || demand.requestedUnitsPerTick > cityServiceMaximumConfiguredUnits ||
			demand.priority < 0 || demand.priority > 1000 || demand.createdTick < 0 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_settlement_demand"})
		}
		if _, duplicate := demandIDs[demand.id]; duplicate {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_demand_id_duplicate"})
		}
		if _, duplicate := demandCodes[demand.code]; duplicate {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_demand_code_duplicate"})
		}
		demandIDs[demand.id] = struct{}{}
		demandCodes[demand.code] = struct{}{}
		if len(input.Connections) > cityServiceMaximumAllocationsPerSettlement {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_connection_limit"})
		}
		for _, candidate := range input.Connections {
			connection, capacity, facility := candidate.Connection, candidate.Capacity, candidate.Facility
			if connection.id <= 0 || connection.version <= 0 ||
				!cityServiceCodePattern.MatchString(connection.code) ||
				connection.status != CityServiceProjectionStatusActive ||
				connection.capacityID != capacity.id || connection.demandID != demand.id ||
				connection.maxFlowUnitsPerTick <= 0 || connection.maxFlowUnitsPerTick > cityServiceMaximumConfiguredUnits ||
				connection.lossMilli < 0 || connection.lossMilli > 999 ||
				connection.preference < 0 || connection.preference > 1000 {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_connection_plan"})
			}
			if _, duplicate := connectionIDs[connection.id]; duplicate {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_connection_id_duplicate"})
			}
			if _, duplicate := connectionCodes[connection.code]; duplicate {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_connection_code_duplicate"})
			}
			connectionIDs[connection.id] = struct{}{}
			connectionCodes[connection.code] = struct{}{}
			if capacity.id <= 0 || capacity.version <= 0 || capacity.facilityID != facility.id ||
				capacity.serviceDefinitionID != demand.serviceDefinitionID ||
				capacity.serviceCode != demand.serviceCode ||
				capacity.installedCapacityUnits <= 0 || capacity.installedCapacityUnits > cityServiceMaximumConfiguredUnits ||
				capacity.availabilityMilli < 0 || capacity.availabilityMilli > 1000 ||
				capacity.availableCapacityUnits < 0 ||
				capacity.availableCapacityUnits != capacity.installedCapacityUnits*int64(capacity.availabilityMilli)/1000 {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_capacity_plan"})
			}
			if facility.id <= 0 || facility.version <= 0 ||
				!cityServiceCodePattern.MatchString(facility.code) || !isCityFacilityStatus(facility.status) {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_facility_plan"})
			}
			identity := cityServiceCapacityPlanIdentity{
				facilityID: facility.id, serviceDefinitionID: capacity.serviceDefinitionID,
				installedCapacityUnits: capacity.installedCapacityUnits,
				availabilityMilli:      capacity.availabilityMilli,
				availableCapacityUnits: capacity.availableCapacityUnits,
				version:                capacity.version, facilityStatus: facility.status,
			}
			if existing, exists := capacities[capacity.id]; exists && existing != identity {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_capacity_snapshot_conflict"})
			}
			capacities[capacity.id] = identity
		}
	}
	return nil
}

func minCityServiceInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

type cityServiceAutomaticExecution struct {
	facts                           []CityServiceFact
	allocations                     []CityServiceAllocation
	settlements                     []CityServiceSettlement
	physicalNetworkFacts            []CityPhysicalNetworkFact
	physicalNetworkBatches          []CityPhysicalNetworkFlowBatch
	physicalNetworkPaths            []CityPhysicalNetworkFlowPath
	physicalNetworkSegments         []CityPhysicalNetworkFlowSegment
	events                          []cityPendingEvent
	nextFactSequence                int64
	nextPhysicalNetworkFactSequence int64
}

func advanceCityServiceSettlements(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, physicalNetworkFactSequence int64,
	simulationVersion string,
) (cityServiceAutomaticExecution, error) {
	execution := cityServiceAutomaticExecution{
		facts: make([]CityServiceFact, 0), allocations: make([]CityServiceAllocation, 0),
		settlements:                     make([]CityServiceSettlement, 0),
		physicalNetworkFacts:            make([]CityPhysicalNetworkFact, 0),
		physicalNetworkBatches:          make([]CityPhysicalNetworkFlowBatch, 0),
		physicalNetworkPaths:            make([]CityPhysicalNetworkFlowPath, 0),
		physicalNetworkSegments:         make([]CityPhysicalNetworkFlowSegment, 0),
		events:                          make([]cityPendingEvent, 0),
		nextFactSequence:                factSequence,
		nextPhysicalNetworkFactSequence: physicalNetworkFactSequence,
	}
	if _, err := tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_service_auto_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10)); err != nil {
		return execution, fmt.Errorf("enable automatic city service settlement: %w", err)
	}
	inputs, err := loadCityServiceSettlementPlanInputs(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	if len(inputs) > cityServiceMaximumSettlementFactsPerTick {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_settlement_fact_limit"})
	}
	var networkPlanning *cityServicePhysicalNetworkPlanningState
	if cityEngineSupportsPhysicalNetworks(simulationVersion) {
		topologySync, syncErr := synchronizeCityPhysicalNetworkTopology(
			ctx, tx, worldID, targetTick, physicalNetworkFactSequence,
		)
		if syncErr != nil {
			return execution, syncErr
		}
		execution.physicalNetworkFacts = append(
			execution.physicalNetworkFacts, topologySync.facts...,
		)
		physicalNetworkFactSequence = topologySync.nextSequence
		networkPlanning, err = loadCityServicePhysicalNetworkPlanningState(ctx, tx, worldID)
		if err != nil {
			return execution, err
		}
	}
	plans, err := planCityServiceSettlementsWithNetworks(inputs, networkPlanning)
	if err != nil {
		return execution, err
	}
	persistedNetworkAllocations := make([]cityPhysicalNetworkPersistedAllocation, 0)
	for _, plan := range plans {
		sequence := execution.nextFactSequence
		allocations := make([]CityServiceAllocation, 0, len(plan.Allocations))
		for index, allocationPlan := range plan.Allocations {
			allocation := CityServiceAllocation{
				Tick: targetTick, Sequence: sequence, AllocationIndex: index + 1,
				ServiceCode: allocationPlan.ServiceCode, FacilityCode: allocationPlan.FacilityCode,
				DemandCode: plan.DemandCode, ConnectionCode: allocationPlan.ConnectionCode,
				CapacityVersion:         allocationPlan.CapacityVersion,
				DemandVersion:           allocationPlan.DemandVersion,
				ConnectionVersion:       allocationPlan.ConnectionVersion,
				FacilityCapacityUnits:   allocationPlan.FacilityCapacityUnits,
				ConnectionCapacityUnits: allocationPlan.ConnectionCapacityUnits,
				LossMilli:               allocationPlan.LossMilli,
				DispatchedUnits:         allocationPlan.DispatchedUnits,
				DeliveredUnits:          allocationPlan.DeliveredUnits,
				LossUnits:               allocationPlan.LossUnits,
				Metadata:                json.RawMessage(`{"schema_version":1}`),
			}
			if allocationPlan.NetworkCode != "" {
				networkReceived := allocationPlan.NetworkReceivedUnits
				networkLoss := allocationPlan.NetworkLossUnits
				connectionLoss := allocationPlan.ConnectionLossUnits
				pathCount := len(allocationPlan.NetworkPaths)
				allocation.NetworkReceivedUnits = &networkReceived
				allocation.NetworkLossUnits = &networkLoss
				allocation.ConnectionLossUnits = &connectionLoss
				allocation.NetworkPathCount = &pathCount
				allocation.Metadata = json.RawMessage(`{"network_routed":true,"schema_version":2}`)
			}
			allocations = append(allocations, allocation)
		}
		settlement := CityServiceSettlement{
			Tick: targetTick, Sequence: sequence, ServiceCode: plan.ServiceCode,
			DemandCode: plan.DemandCode, DemandVersion: plan.DemandVersion,
			RequestedUnits: plan.RequestedUnits, DeliveredUnits: plan.DeliveredUnits,
			ShortageUnits: plan.ShortageUnits, AllocationCount: len(allocations),
			QualityMilli: plan.QualityMilli, Metadata: json.RawMessage(`{"schema_version":1}`),
		}
		fact, insertErr := insertCityServiceFact(ctx, tx, cityServiceFactInsert{
			worldID: worldID, tick: targetTick, sequence: sequence,
			factType: CityServiceFactAllocationSettled, subjectKind: "settlement",
			subjectCode: plan.DemandCode + "." + strconv.FormatInt(targetTick, 10),
			payload: map[string]any{
				"schema_version": 1, "settlement": settlement, "allocations": allocations,
			},
		})
		if insertErr != nil {
			return execution, insertErr
		}
		for index, allocationPlan := range plan.Allocations {
			if _, insertErr = tx.ExecContext(ctx, `
INSERT INTO city_service_allocations
    (world_id, tick, sequence, allocation_index, source_fact_id,
     service_definition_id, facility_id, capacity_id, demand_id, connection_id,
     capacity_version, demand_version, connection_version,
     facility_capacity_units, connection_capacity_units, loss_milli,
     dispatched_units, network_received_units, network_loss_units,
     connection_loss_units, network_path_count, delivered_units, loss_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24::jsonb)`,
				worldID, targetTick, sequence, index+1, fact.id,
				allocationPlan.ServiceDefinitionID, allocationPlan.FacilityID,
				allocationPlan.CapacityID, plan.DemandID, allocationPlan.ConnectionID,
				allocationPlan.CapacityVersion, allocationPlan.DemandVersion,
				allocationPlan.ConnectionVersion, allocationPlan.FacilityCapacityUnits,
				allocationPlan.ConnectionCapacityUnits, allocationPlan.LossMilli,
				allocationPlan.DispatchedUnits,
				optionalCityNetworkInt64(allocationPlan.NetworkCode, allocationPlan.NetworkReceivedUnits),
				optionalCityNetworkInt64(allocationPlan.NetworkCode, allocationPlan.NetworkLossUnits),
				optionalCityNetworkInt64(allocationPlan.NetworkCode, allocationPlan.ConnectionLossUnits),
				optionalCityNetworkInt(allocationPlan.NetworkCode, len(allocationPlan.NetworkPaths)),
				allocationPlan.DeliveredUnits, allocationPlan.LossUnits,
				allocations[index].Metadata); insertErr != nil {
				return execution, fmt.Errorf("insert city service allocation: %w", insertErr)
			}
			if allocationPlan.NetworkCode != "" {
				persistedNetworkAllocations = append(persistedNetworkAllocations,
					cityPhysicalNetworkPersistedAllocation{
						serviceFactID: fact.id, serviceSequence: sequence,
						allocationIndex: index + 1,
						connectionID:    allocationPlan.ConnectionID, plan: allocationPlan,
					})
			}
		}
		if _, insertErr = tx.ExecContext(ctx, `
INSERT INTO city_service_settlements
    (world_id, tick, sequence, source_fact_id, service_definition_id,
     demand_id, demand_version, requested_units, delivered_units,
     shortage_units, allocation_count, quality_milli, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        '{"schema_version":1}'::jsonb)`, worldID, targetTick, sequence,
			fact.id, plan.ServiceID, plan.DemandID, plan.DemandVersion,
			plan.RequestedUnits, plan.DeliveredUnits, plan.ShortageUnits,
			len(plan.Allocations), plan.QualityMilli); insertErr != nil {
			return execution, fmt.Errorf("insert city service settlement: %w", insertErr)
		}
		if insertErr = advanceCityServiceProfile(ctx, tx, worldID, cityServiceProfileDeltas{
			allocations: int64(len(plan.Allocations)), settlements: 1,
		}); insertErr != nil {
			return execution, insertErr
		}
		if insertErr = postCityServiceFact(ctx, tx, fact.id); insertErr != nil {
			return execution, insertErr
		}
		execution.facts = append(execution.facts, fact.fact)
		execution.allocations = append(execution.allocations, allocations...)
		execution.settlements = append(execution.settlements, settlement)
		execution.events = append(execution.events, cityPendingEvent{
			eventType: "city.service.allocation.settled",
			payload: map[string]any{
				"demand_code": plan.DemandCode, "service_code": plan.ServiceCode,
				"requested_units": plan.RequestedUnits, "delivered_units": plan.DeliveredUnits,
				"shortage_units": plan.ShortageUnits, "quality_milli": plan.QualityMilli,
				"allocation_count": len(plan.Allocations),
			},
		})
		execution.nextFactSequence++
	}
	flowFacts, err := persistCityPhysicalNetworkFlows(
		ctx, tx, worldID, targetTick, physicalNetworkFactSequence,
		networkPlanning, persistedNetworkAllocations,
	)
	if err != nil {
		return execution, err
	}
	execution.physicalNetworkFacts = append(execution.physicalNetworkFacts, flowFacts...)
	for _, fact := range flowFacts {
		var payload cityPhysicalNetworkFlowFactPayload
		if err = json.Unmarshal(fact.Payload, &payload); err != nil {
			return execution, fmt.Errorf("decode persisted physical network flow fact: %w", err)
		}
		execution.physicalNetworkBatches = append(execution.physicalNetworkBatches, payload.Batch)
		execution.physicalNetworkPaths = append(execution.physicalNetworkPaths, payload.Paths...)
		execution.physicalNetworkSegments = append(execution.physicalNetworkSegments, payload.Segments...)
	}
	execution.nextPhysicalNetworkFactSequence = physicalNetworkFactSequence + int64(len(flowFacts))
	return execution, nil
}

func optionalCityNetworkInt64(networkCode string, value int64) any {
	if networkCode == "" {
		return nil
	}
	return value
}

func optionalCityNetworkInt(networkCode string, value int) any {
	if networkCode == "" {
		return nil
	}
	return value
}

func loadCityServiceSettlementPlanInputs(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]cityServiceSettlementPlanInput, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT demand.id, demand.code, demand.service_definition_id, service.code,
       demand.subject_kind, demand.subject_code, district.id, district.code,
       demand.building_id, building.code, demand.entity_id, demand.actor_id,
       demand.requested_units_per_tick, demand.priority, demand.status,
       demand.created_tick, demand.updated_tick, demand.version, demand.metadata
FROM city_service_demands demand
JOIN city_service_definitions service ON service.id = demand.service_definition_id
JOIN city_districts district ON district.id = demand.district_id
LEFT JOIN city_buildings building ON building.id = demand.building_id
WHERE demand.world_id = $1 AND demand.status = 'active'`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city service settlement demands: %w", err)
	}
	inputs := make([]cityServiceSettlementPlanInput, 0)
	byDemandID := make(map[int64]int)
	for rows.Next() {
		var demand cityServiceDemandRef
		var buildingID, entityID, actorID sql.NullInt64
		var buildingCode sql.NullString
		if err = rows.Scan(&demand.id, &demand.code, &demand.serviceDefinitionID,
			&demand.serviceCode, &demand.subjectKind, &demand.subjectCode,
			&demand.districtID, &demand.districtCode, &buildingID, &buildingCode,
			&entityID, &actorID, &demand.requestedUnitsPerTick, &demand.priority,
			&demand.status, &demand.createdTick, &demand.updatedTick, &demand.version,
			&demand.metadata); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan city service settlement demand: %w", err)
		}
		demand.buildingID = nullInt64Pointer(buildingID)
		demand.entityID = nullInt64Pointer(entityID)
		demand.actorID = nullInt64Pointer(actorID)
		demand.buildingCode = nullStringPointer(buildingCode)
		byDemandID[demand.id] = len(inputs)
		inputs = append(inputs, cityServiceSettlementPlanInput{
			Demand: demand, Connections: make([]cityServiceSettlementConnectionInput, 0),
		})
	}
	if err = closeCityRows(rows, "iterate city service settlement demands"); err != nil {
		return nil, err
	}
	if len(inputs) == 0 {
		return inputs, nil
	}
	connectionRows, err := queryer.QueryContext(ctx, `
SELECT connection.id, connection.code, connection.capacity_id,
       connection.demand_id, connection.max_flow_units_per_tick,
       connection.loss_milli, connection.preference, connection.status,
       connection.created_tick, connection.updated_tick, connection.version,
       connection.metadata,
       capacity.id, capacity.facility_id, facility.code,
       capacity.service_definition_id, service.code,
       capacity.installed_capacity_units, capacity.availability_milli,
       capacity.available_capacity_units, capacity.updated_tick,
       capacity.version, capacity.metadata,
       facility.id, facility.code, facility.name, type.id, type.code,
       type.definition_version, type.definition_hash, type.allowed_service_codes,
       district.id, district.code, building.id, building.code,
       facility.owner_entity_id, owner.code, facility.status,
       facility.reliability_milli, facility.created_tick, facility.updated_tick,
       facility.version, facility.metadata,
       COALESCE(lifecycle.effective_factor_milli, 1000),
       lifecycle.facility_id IS NOT NULL
FROM city_service_connections connection
JOIN city_facility_service_capacities capacity ON capacity.id = connection.capacity_id
JOIN city_facilities facility ON facility.id = capacity.facility_id
JOIN city_service_definitions service ON service.id = capacity.service_definition_id
JOIN city_facility_type_definitions type ON type.id = facility.facility_type_id
JOIN city_districts district ON district.id = facility.district_id
JOIN city_buildings building ON building.id = facility.building_id
LEFT JOIN city_economic_entities owner ON owner.id = facility.owner_entity_id
LEFT JOIN city_facility_lifecycle_states lifecycle
  ON lifecycle.world_id = facility.world_id AND lifecycle.facility_id = facility.id
WHERE connection.world_id = $1 AND connection.status = 'active'`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city service settlement connections: %w", err)
	}
	for connectionRows.Next() {
		var connection cityServiceConnectionRef
		var capacity cityServiceCapacityRef
		var facility cityFacilityRef
		var capacityID, facilityID int64
		var allowed json.RawMessage
		var ownerID sql.NullInt64
		var ownerCode sql.NullString
		if err = connectionRows.Scan(
			&connection.id, &connection.code, &connection.capacityID, &connection.demandID,
			&connection.maxFlowUnitsPerTick, &connection.lossMilli, &connection.preference,
			&connection.status, &connection.createdTick, &connection.updatedTick,
			&connection.version, &connection.metadata,
			&capacityID, &capacity.facilityID, &capacity.facilityCode,
			&capacity.serviceDefinitionID, &capacity.serviceCode,
			&capacity.installedCapacityUnits, &capacity.availabilityMilli,
			&capacity.availableCapacityUnits, &capacity.updatedTick,
			&capacity.version, &capacity.metadata,
			&facilityID, &facility.code, &facility.name, &facility.facilityTypeID,
			&facility.facilityTypeCode, &facility.facilityTypeVersion,
			&facility.facilityTypeHash, &allowed, &facility.districtID,
			&facility.districtCode, &facility.buildingID, &facility.buildingCode,
			&ownerID, &ownerCode, &facility.status, &facility.reliabilityMilli,
			&facility.createdTick, &facility.updatedTick, &facility.version,
			&facility.metadata, &facility.lifecycleFactorMilli,
			&facility.lifecycleManaged,
		); err != nil {
			_ = connectionRows.Close()
			return nil, fmt.Errorf("scan city service settlement connection: %w", err)
		}
		capacity.id = capacityID
		facility.id = facilityID
		facility.ownerEntityID = nullInt64Pointer(ownerID)
		facility.ownerEntityCode = nullStringPointer(ownerCode)
		var allowedCodes []string
		if err = json.Unmarshal(allowed, &allowedCodes); err != nil {
			_ = connectionRows.Close()
			return nil, fmt.Errorf("decode settlement facility services: %w", err)
		}
		facility.allowedServices = make(map[string]struct{}, len(allowedCodes))
		for _, code := range allowedCodes {
			facility.allowedServices[code] = struct{}{}
		}
		index, exists := byDemandID[connection.demandID]
		if !exists {
			_ = connectionRows.Close()
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "service_connection_demand"})
		}
		inputs[index].Connections = append(inputs[index].Connections, cityServiceSettlementConnectionInput{
			Connection: connection, Capacity: capacity, Facility: facility,
		})
	}
	if err = closeCityRows(connectionRows, "iterate city service settlement connections"); err != nil {
		return nil, err
	}
	return inputs, nil
}
