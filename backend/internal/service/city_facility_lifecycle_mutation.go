package service

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	CityCommandTypeFacilityOperationSchedule = "facility.operation.schedule"
	CityCommandTypeFacilityOperationStart    = "facility.operation.start"
	CityCommandTypeFacilityOperationCancel   = "facility.operation.cancel"
	CityCommandTypeFacilityStaffingConfigure = "facility.staffing.configure"

	cityFacilityLifecycleRejectionFacilityNotFound   = "CITY_FACILITY_LIFECYCLE_NOT_FOUND"
	cityFacilityLifecycleRejectionOperationNotFound  = "CITY_FACILITY_OPERATION_NOT_FOUND"
	cityFacilityLifecycleRejectionOperationConflict  = "CITY_FACILITY_OPERATION_CONFLICT"
	cityFacilityLifecycleRejectionStateTransition    = "CITY_FACILITY_LIFECYCLE_TRANSITION_INVALID"
	cityFacilityLifecycleRejectionVersionConflict    = "CITY_FACILITY_LIFECYCLE_VERSION_CONFLICT"
	cityFacilityLifecycleRejectionEntityInvalid      = "CITY_FACILITY_OPERATION_ENTITY_INVALID"
	cityFacilityLifecycleRejectionBudgetInvalid      = "CITY_FACILITY_OPERATION_BUDGET_INVALID"
	cityFacilityLifecycleRejectionBudgetInsufficient = "CITY_FACILITY_OPERATION_BUDGET_INSUFFICIENT"
	cityFacilityLifecycleRejectionCashInsufficient   = "CITY_FACILITY_OPERATION_CASH_INSUFFICIENT"
	cityFacilityLifecycleRejectionResourceShortage   = "CITY_FACILITY_OPERATION_RESOURCE_SHORTAGE"
	cityFacilityLifecycleRejectionLaborShortage      = "CITY_FACILITY_OPERATION_LABOR_SHORTAGE"
	cityFacilityLifecycleRejectionStartNotDue        = "CITY_FACILITY_OPERATION_START_NOT_DUE"
	cityFacilityLifecycleRejectionStaffingConflict   = "CITY_FACILITY_STAFFING_CONFLICT"
	cityFacilityLifecycleRejectionQualification      = "CITY_FACILITY_STAFF_QUALIFICATION_INVALID"
)

type cityFacilityOperationSchedulePayload struct {
	Code                    string          `json:"code"`
	FacilityCode            string          `json:"facility_code"`
	OperationType           string          `json:"operation_type"`
	SponsorEntityCode       string          `json:"sponsor_entity_code"`
	ExecutorEntityCode      string          `json:"executor_entity_code"`
	BudgetCode              string          `json:"budget_code,omitempty"`
	PlannedStartTick        int64           `json:"planned_start_tick"`
	ExpectedFacilityVersion int64           `json:"expected_facility_version"`
	Metadata                json.RawMessage `json:"metadata,omitempty"`
}

type cityFacilityOperationStartPayload struct {
	OperationCode            string          `json:"operation_code"`
	ExpectedOperationVersion int64           `json:"expected_operation_version"`
	ExpectedFacilityVersion  int64           `json:"expected_facility_version"`
	Metadata                 json.RawMessage `json:"metadata,omitempty"`
}

type cityFacilityOperationCancelPayload struct {
	OperationCode            string          `json:"operation_code"`
	ExpectedOperationVersion int64           `json:"expected_operation_version"`
	ExpectedFacilityVersion  int64           `json:"expected_facility_version"`
	Metadata                 json.RawMessage `json:"metadata,omitempty"`
}

type cityFacilityStaffingConfigurePayload struct {
	Code                    string          `json:"code"`
	FacilityCode            string          `json:"facility_code"`
	RoleCode                string          `json:"role_code"`
	SubjectKind             string          `json:"subject_kind"`
	SubjectCode             string          `json:"subject_code"`
	AssignedUnits           int64           `json:"assigned_units"`
	Status                  string          `json:"status"`
	ExpectedVersion         int64           `json:"expected_version"`
	ExpectedFacilityVersion int64           `json:"expected_facility_version"`
	Metadata                json.RawMessage `json:"metadata,omitempty"`
}

type cityFacilityLifecycleBusinessError struct{ code string }

func (err *cityFacilityLifecycleBusinessError) Error() string { return err.code }

func cityFacilityLifecycleReject(code string) error {
	return &cityFacilityLifecycleBusinessError{code: code}
}

func cityFacilityLifecycleBusinessRejectionCode(err error) string {
	var businessErr *cityFacilityLifecycleBusinessError
	if errors.As(err, &businessErr) {
		return businessErr.code
	}
	return ""
}

func isCityFacilityLifecycleCommand(commandType string) bool {
	switch commandType {
	case CityCommandTypeFacilityOperationSchedule,
		CityCommandTypeFacilityOperationStart,
		CityCommandTypeFacilityOperationCancel,
		CityCommandTypeFacilityStaffingConfigure:
		return true
	default:
		return false
	}
}

func normalizeCityFacilityLifecycleCommand(commandType string, raw json.RawMessage) (any, bool, error) {
	normalizeCode := func(value *string, subject bool) bool {
		*value = strings.ToLower(strings.TrimSpace(*value))
		if subject {
			return cityServiceSubjectCodePattern.MatchString(*value)
		}
		return cityServiceCodePattern.MatchString(*value)
	}
	normalizeMetadata := func(value *json.RawMessage) bool {
		metadata, err := normalizeCityServiceMetadata(*value)
		if err != nil {
			return false
		}
		*value = metadata
		return true
	}
	switch commandType {
	case CityCommandTypeFacilityOperationSchedule:
		var value cityFacilityOperationSchedulePayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		value.OperationType = strings.ToLower(strings.TrimSpace(value.OperationType))
		value.BudgetCode = strings.ToLower(strings.TrimSpace(value.BudgetCode))
		if !normalizeCode(&value.Code, false) || !normalizeCode(&value.FacilityCode, false) ||
			!normalizeCode(&value.SponsorEntityCode, false) || !normalizeCode(&value.ExecutorEntityCode, false) ||
			(value.BudgetCode != "" && !normalizeCode(&value.BudgetCode, false)) ||
			!isCityFacilityOperationType(value.OperationType) || value.PlannedStartTick <= 0 ||
			value.ExpectedFacilityVersion <= 0 || !normalizeMetadata(&value.Metadata) {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	case CityCommandTypeFacilityOperationStart:
		var value cityFacilityOperationStartPayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if !normalizeCode(&value.OperationCode, false) || value.ExpectedOperationVersion <= 0 ||
			value.ExpectedFacilityVersion <= 0 || !normalizeMetadata(&value.Metadata) {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	case CityCommandTypeFacilityOperationCancel:
		var value cityFacilityOperationCancelPayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if !normalizeCode(&value.OperationCode, false) || value.ExpectedOperationVersion <= 0 ||
			value.ExpectedFacilityVersion <= 0 || !normalizeMetadata(&value.Metadata) {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	case CityCommandTypeFacilityStaffingConfigure:
		var value cityFacilityStaffingConfigurePayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		value.SubjectKind = strings.ToLower(strings.TrimSpace(value.SubjectKind))
		value.Status = strings.ToLower(strings.TrimSpace(value.Status))
		if !normalizeCode(&value.Code, false) || !normalizeCode(&value.FacilityCode, false) ||
			!normalizeCode(&value.RoleCode, false) || !normalizeCode(&value.SubjectCode, true) ||
			(value.SubjectKind != "entity" && value.SubjectKind != "actor") ||
			(value.Status != "active" && value.Status != "released") || value.AssignedUnits <= 0 ||
			value.AssignedUnits > cityServiceMaximumConfiguredUnits || value.ExpectedVersion < 0 ||
			value.ExpectedFacilityVersion <= 0 ||
			(value.SubjectKind == "actor" && value.AssignedUnits != 1) ||
			!normalizeMetadata(&value.Metadata) {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	default:
		return nil, false, nil
	}
}

func isCityFacilityOperationType(value string) bool {
	switch value {
	case CityFacilityOperationCommission, CityFacilityOperationMaintenance,
		CityFacilityOperationRepair, CityFacilityOperationDecommission:
		return true
	default:
		return false
	}
}

type cityFacilityLifecycleFactRecord struct {
	id   int64
	fact CityFacilityLifecycleFact
}

type cityFacilityLifecycleFactInsert struct {
	worldID         int64
	tick            int64
	sequence        int64
	phase           string
	sourceCommandID *int64
	factType        string
	subjectKind     string
	subjectCode     string
	versionBefore   int64
	versionAfter    int64
	payload         map[string]any
}

type cityFacilityLifecycleProfileDeltas struct {
	states          int64
	operations      int64
	staffing        int64
	incidents       int64
	budgetMovements int64
}

type cityFacilityLifecycleExecution struct {
	pending              cityPendingEvent
	facts                []CityFacilityLifecycleFact
	journals             []*CityJournal
	resourceOperations   []*CityResourceOperation
	nextFactSequence     int64
	nextJournalSequence  int64
	nextResourceSequence int64
}

type cityFacilityLifecycleRef struct {
	facilityID             int64
	policyID               int64
	districtID             int64
	districtCode           string
	installedCapacityUnits int64
	state                  CityFacilityLifecycleState
	policy                 CityFacilityLifecyclePolicy
}

type cityFacilityOperationRef struct {
	id               int64
	facilityID       int64
	sponsorEntityID  int64
	executorEntityID int64
	budgetLineID     *int64
	value            CityFacilityOperation
}

type cityFacilityStaffAssignmentRef struct {
	id       int64
	entityID *int64
	actorID  *int64
	value    CityFacilityStaffAssignment
}

type cityFacilityLifecycleEntityRef struct {
	id            int64
	code          string
	entityType    string
	staffCapacity int64
}

func insertCityFacilityLifecycleFact(
	ctx context.Context, tx *sql.Tx, input cityFacilityLifecycleFactInsert,
) (*cityFacilityLifecycleFactRecord, error) {
	payload, err := json.Marshal(input.payload)
	if err != nil {
		return nil, fmt.Errorf("marshal city facility lifecycle fact: %w", err)
	}
	var factID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_facility_lifecycle_facts
    (world_id, tick, sequence, phase, source_command_id, fact_type,
     subject_kind, subject_code, version_before, version_after, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)
RETURNING id`, input.worldID, input.tick, input.sequence, input.phase,
		optionalInt64Value(input.sourceCommandID), input.factType, input.subjectKind,
		input.subjectCode, input.versionBefore, input.versionAfter, payload).Scan(&factID)
	if err != nil {
		return nil, fmt.Errorf("insert city facility lifecycle fact draft: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_facility_lifecycle_fact_id', $1, TRUE)`,
		strconv.FormatInt(factID, 10)); err != nil {
		return nil, fmt.Errorf("activate city facility lifecycle fact gate: %w", err)
	}
	return &cityFacilityLifecycleFactRecord{id: factID, fact: CityFacilityLifecycleFact{
		Tick: input.tick, Sequence: input.sequence, Phase: input.phase,
		FactType: input.factType, SubjectKind: input.subjectKind,
		SubjectCode: input.subjectCode, VersionBefore: input.versionBefore,
		VersionAfter: input.versionAfter, Payload: payload,
	}}, nil
}

func postCityFacilityLifecycleFact(ctx context.Context, tx *sql.Tx, factID int64) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_facility_lifecycle_facts SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, factID)
	if err != nil {
		return fmt.Errorf("post city facility lifecycle fact: %w", err)
	}
	return requireCityFacilityLifecycleRow(result, strconv.FormatInt(factID, 10))
}

func advanceCityFacilityLifecycleProfile(
	ctx context.Context, tx *sql.Tx, worldID int64, delta cityFacilityLifecycleProfileDeltas,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_facility_lifecycle_profiles
SET state_count = state_count + $2,
    operation_count = operation_count + $3,
    staffing_count = staffing_count + $4,
    incident_count = incident_count + $5,
    fact_count = fact_count + 1,
    budget_movement_count = budget_movement_count + $6,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID, delta.states, delta.operations, delta.staffing,
		delta.incidents, delta.budgetMovements)
	if err != nil {
		return fmt.Errorf("advance city facility lifecycle profile: %w", err)
	}
	return requireCityFacilityLifecycleRow(result, strconv.FormatInt(worldID, 10))
}

func requireCityFacilityLifecycleRow(result sql.Result, subject string) error {
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"city_facility_lifecycle_subject": subject,
		})
	}
	return nil
}

func cityFacilityStaffingFactor(required, assigned int64) int {
	if required <= 0 {
		return 1000
	}
	if assigned <= 0 {
		return 0
	}
	factor, err := cityMulDivFloor(assigned, 1000, required)
	if err != nil || factor <= 0 {
		return 0
	}
	if factor > 1000 {
		return 1000
	}
	return int(factor)
}

func cityFacilityLifecycleEffectiveFactor(status string, condition, staffing, operation int) int {
	if status != CityFacilityLifecycleStatusOperational {
		return 0
	}
	result := condition
	if staffing < result {
		result = staffing
	}
	if operation < result {
		result = operation
	}
	if result < 0 {
		return 0
	}
	if result > 1000 {
		return 1000
	}
	return result
}

func cityFacilityLifecycleMetadataWithString(
	raw json.RawMessage, key, value string,
) (json.RawMessage, error) {
	metadata := make(map[string]any)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return nil, fmt.Errorf("decode facility lifecycle metadata: %w", err)
		}
	}
	metadata[key] = value
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode facility lifecycle metadata: %w", err)
	}
	return encoded, nil
}

func cityFacilityLifecycleOperationPlan(
	policy CityFacilityLifecyclePolicy, operationType string, installedCapacity int64,
) (duration, basicMaterial, capitalGoods, labor, budget int64, err error) {
	plan, exists := policy.OperationPlans[operationType]
	if !exists || policy.OperationCapacityQuantumUnits <= 0 ||
		plan.BaseDurationTicks <= 0 || plan.CapacityQuantaPerDurationTick <= 0 ||
		plan.LaborUnitsPerQuantum <= 0 || plan.BudgetUnitsPerQuantum <= 0 {
		return 0, 0, 0, 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "facility_operation_policy",
		})
	}
	quanta := int64(1)
	if installedCapacity > 0 {
		quanta = (installedCapacity + policy.OperationCapacityQuantumUnits - 1) /
			policy.OperationCapacityQuantumUnits
	}
	durationQuanta := (quanta + plan.CapacityQuantaPerDurationTick - 1) /
		plan.CapacityQuantaPerDurationTick
	if plan.BaseDurationTicks > math.MaxInt64-durationQuanta {
		return 0, 0, 0, 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "facility_operation_duration"})
	}
	duration = plan.BaseDurationTicks + durationQuanta
	multiply := func(coefficient int64) (int64, error) {
		if coefficient == 0 {
			return 0, nil
		}
		if coefficient < 0 || quanta > math.MaxInt64/coefficient {
			return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "facility_operation_plan"})
		}
		return quanta * coefficient, nil
	}
	if basicMaterial, err = multiply(plan.BasicMaterialUnitsPerQuantum); err != nil {
		return 0, 0, 0, 0, 0, err
	}
	if capitalGoods, err = multiply(plan.CapitalGoodsUnitsPerQuantum); err != nil {
		return 0, 0, 0, 0, 0, err
	}
	if labor, err = multiply(plan.LaborUnitsPerQuantum); err != nil {
		return 0, 0, 0, 0, 0, err
	}
	if budget, err = multiply(plan.BudgetUnitsPerQuantum); err != nil {
		return 0, 0, 0, 0, 0, err
	}
	return duration, basicMaterial, capitalGoods, labor, budget, nil
}

func deriveCityFacilityFailureSample(
	version string, seed, tick int64, facilityCode, policyHash string, failureCount int64,
) (int, string) {
	proof := deriveCityRandomHex(
		version, seed, tick,
		"facility_failure:"+facilityCode+":"+policyHash,
		failureCount,
	)
	raw, _ := hex.DecodeString(proof[:16])
	return int(binary.BigEndian.Uint64(raw) % 1_000_000), proof
}

func cityFacilityIncidentCode(facilityCode string, tick, failureCount int64) string {
	suffix := deriveCityRandomHex(CitySimulationVersionF8V2, tick, failureCount, facilityCode, 0)[:12]
	prefix := facilityCode
	if len(prefix) > 55 {
		prefix = prefix[:55]
	}
	return "incident." + prefix + "." + suffix
}

func sortCityFacilityLifecycleFacts(items []CityFacilityLifecycleFact) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Tick != items[j].Tick {
			return items[i].Tick < items[j].Tick
		}
		return items[i].Sequence < items[j].Sequence
	})
}

func loadCityFacilityLifecycleRef(
	ctx context.Context, queryer citySQLQueryer, worldID int64, facilityCode string, lock bool,
) (*cityFacilityLifecycleRef, error) {
	query := `
SELECT facility.id, district.id, district.code,
       COALESCE(capacity.installed_capacity_units, 0)::BIGINT,
       policy.id, type.code, policy.policy_version, policy.policy_hash,
       policy.maintenance_interval_ticks, policy.base_decay_milli,
       policy.utilization_decay_milli, policy.overdue_decay_milli,
       policy.failure_threshold_milli, policy.base_failure_ppm,
       policy.condition_failure_ppm, policy.capacity_units_per_staff,
       policy.maintenance_restore_milli, policy.repair_restore_milli, policy.payload,
       state.lifecycle_status, state.condition_milli, state.staff_required_units,
       state.staff_assigned_units, state.staffing_factor_milli,
       state.operation_factor_milli, state.effective_factor_milli,
       state.last_maintenance_tick, state.maintenance_due_tick,
       state.active_operation_code, state.open_incident_code,
       state.failure_count, state.updated_tick, state.version,
       source.tick, source.sequence, state.metadata
FROM city_facilities facility
JOIN city_facility_type_definitions type ON type.id = facility.facility_type_id
JOIN city_districts district ON district.id = facility.district_id
JOIN city_facility_lifecycle_policies policy
  ON policy.world_id = facility.world_id AND policy.facility_type_id = facility.facility_type_id
JOIN city_facility_lifecycle_states state
  ON state.world_id = facility.world_id AND state.facility_id = facility.id
LEFT JOIN city_facility_lifecycle_facts source ON source.id = state.source_fact_id
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(item.installed_capacity_units), 0)::BIGINT AS installed_capacity_units
    FROM city_facility_service_capacities item
    WHERE item.world_id = facility.world_id AND item.facility_id = facility.id
) capacity ON TRUE
WHERE facility.world_id = $1 AND facility.code = $2`
	if lock {
		query += ` FOR UPDATE OF state, facility`
	}
	ref := &cityFacilityLifecycleRef{}
	var lastMaintenance, sourceTick, sourceSequence sql.NullInt64
	var activeOperation, openIncident sql.NullString
	err := queryer.QueryRowContext(ctx, query, worldID, facilityCode).Scan(
		&ref.facilityID, &ref.districtID, &ref.districtCode,
		&ref.installedCapacityUnits, &ref.policyID, &ref.state.FacilityTypeCode,
		&ref.policy.PolicyVersion, &ref.policy.PolicyHash,
		&ref.policy.MaintenanceIntervalTicks, &ref.policy.BaseDecayMilli,
		&ref.policy.UtilizationDecayMilli, &ref.policy.OverdueDecayMilli,
		&ref.policy.FailureThresholdMilli, &ref.policy.BaseFailurePPM,
		&ref.policy.ConditionFailurePPM, &ref.policy.CapacityUnitsPerStaff,
		&ref.policy.MaintenanceRestoreMilli, &ref.policy.RepairRestoreMilli,
		&ref.policy.Payload, &ref.state.LifecycleStatus, &ref.state.ConditionMilli,
		&ref.state.StaffRequiredUnits, &ref.state.StaffAssignedUnits,
		&ref.state.StaffingFactorMilli, &ref.state.OperationFactorMilli,
		&ref.state.EffectiveFactorMilli, &lastMaintenance,
		&ref.state.MaintenanceDueTick, &activeOperation, &openIncident,
		&ref.state.FailureCount, &ref.state.UpdatedTick, &ref.state.Version,
		&sourceTick, &sourceSequence, &ref.state.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityFacilityLifecycleReject(cityFacilityLifecycleRejectionFacilityNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load city facility lifecycle: %w", err)
	}
	ref.state.FacilityCode = facilityCode
	ref.state.LastMaintenanceTick = nullInt64Pointer(lastMaintenance)
	ref.state.ActiveOperationCode = nullStringPointer(activeOperation)
	ref.state.OpenIncidentCode = nullStringPointer(openIncident)
	ref.state.SourceFactTick = nullInt64Pointer(sourceTick)
	ref.state.SourceFactSequence = nullInt64Pointer(sourceSequence)
	ref.policy.FacilityTypeCode = ref.state.FacilityTypeCode
	var policySeed cityFacilityLifecyclePolicySeed
	if err = json.Unmarshal(ref.policy.Payload, &policySeed); err != nil ||
		policySeed.OperationCapacityQuantumUnits <= 0 || len(policySeed.OperationPlans) != 4 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "facility_lifecycle_policy_payload",
		})
	}
	ref.policy.OperationCapacityQuantumUnits = policySeed.OperationCapacityQuantumUnits
	ref.policy.OperationPlans = policySeed.OperationPlans
	return ref, nil
}

func initializeCityFacilityLifecycleForServiceCommand(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, facilityCode string,
) (*CityFacilityLifecycleFact, error) {
	var facilityID, policyID, installedCapacity int64
	var facilityTypeCode string
	var interval, capacityPerStaff int64
	err := tx.QueryRowContext(ctx, `
SELECT facility.id, policy.id, type.code, policy.maintenance_interval_ticks,
       policy.capacity_units_per_staff,
       COALESCE(capacity.installed_capacity_units, 0)::BIGINT
FROM city_facilities facility
JOIN city_facility_type_definitions type ON type.id = facility.facility_type_id
JOIN city_facility_lifecycle_policies policy
  ON policy.world_id = facility.world_id AND policy.facility_type_id = facility.facility_type_id
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(item.installed_capacity_units), 0)::BIGINT AS installed_capacity_units
    FROM city_facility_service_capacities item
    WHERE item.world_id = facility.world_id AND item.facility_id = facility.id
) capacity ON TRUE
WHERE facility.world_id = $1 AND facility.code = $2
FOR UPDATE OF facility`, worldID, facilityCode).Scan(
		&facilityID, &policyID, &facilityTypeCode, &interval, &capacityPerStaff,
		&installedCapacity,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityFacilityLifecycleReject(cityFacilityLifecycleRejectionFacilityNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load registered facility lifecycle policy: %w", err)
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM city_facility_lifecycle_states
    WHERE world_id = $1 AND facility_id = $2
)`, worldID, facilityID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check registered facility lifecycle state: %w", err)
	}
	if exists {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "facility_lifecycle_duplicate"})
	}
	required := int64(0)
	if installedCapacity > 0 {
		required = (installedCapacity + capacityPerStaff - 1) / capacityPerStaff
	}
	state := CityFacilityLifecycleState{
		FacilityCode: facilityCode, FacilityTypeCode: facilityTypeCode,
		LifecycleStatus: CityFacilityLifecycleStatusUncommissioned,
		ConditionMilli:  1000, StaffRequiredUnits: required,
		StaffAssignedUnits: 0, StaffingFactorMilli: cityFacilityStaffingFactor(required, 0),
		OperationFactorMilli: 1000, EffectiveFactorMilli: 0,
		MaintenanceDueTick: targetTick + interval, UpdatedTick: targetTick,
		Version: 1, SourceFactTick: &targetTick, SourceFactSequence: &factSequence,
		Metadata: json.RawMessage(`{"schema_version":1,"staffing_source":"none"}`),
	}
	fact, err := insertCityFacilityLifecycleFact(ctx, tx, cityFacilityLifecycleFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		phase: CityFacilityLifecyclePhaseCommand, sourceCommandID: &command.ID,
		factType:    CityFacilityLifecycleFactFacilityInitialized,
		subjectKind: "facility", subjectCode: facilityCode,
		versionBefore: 0, versionAfter: 1,
		payload: map[string]any{"schema_version": 1, "state_after": state},
	})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_facility_lifecycle_states
    (world_id, facility_id, policy_id, lifecycle_status, condition_milli,
     staff_required_units, staff_assigned_units, staffing_factor_milli,
     operation_factor_milli, effective_factor_milli, maintenance_due_tick,
     failure_count, updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 0, $12, 1, $13, $14::jsonb)`,
		worldID, facilityID, policyID, state.LifecycleStatus, state.ConditionMilli,
		state.StaffRequiredUnits, state.StaffAssignedUnits, state.StaffingFactorMilli,
		state.OperationFactorMilli, state.EffectiveFactorMilli, state.MaintenanceDueTick,
		targetTick, fact.id, state.Metadata); err != nil {
		return nil, fmt.Errorf("initialize registered facility lifecycle: %w", err)
	}
	if err = advanceCityFacilityLifecycleProfile(ctx, tx, worldID,
		cityFacilityLifecycleProfileDeltas{states: 1}); err != nil {
		return nil, err
	}
	if err = postCityFacilityLifecycleFact(ctx, tx, fact.id); err != nil {
		return nil, err
	}
	return &fact.fact, nil
}

func updateCityFacilityLifecycleCapacityForServiceCommand(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, facilityCode string,
) (*CityFacilityLifecycleFact, error) {
	ref, err := loadCityFacilityLifecycleRef(ctx, tx, worldID, facilityCode, true)
	if err != nil {
		return nil, err
	}
	before := ref.state
	required := int64(0)
	if ref.installedCapacityUnits > 0 {
		required = (ref.installedCapacityUnits + ref.policy.CapacityUnitsPerStaff - 1) /
			ref.policy.CapacityUnitsPerStaff
	}
	after := before
	after.StaffRequiredUnits = required
	after.StaffingFactorMilli = cityFacilityStaffingFactor(required, after.StaffAssignedUnits)
	after.EffectiveFactorMilli = cityFacilityLifecycleEffectiveFactor(
		after.LifecycleStatus, after.ConditionMilli, after.StaffingFactorMilli,
		after.OperationFactorMilli,
	)
	after.UpdatedTick = targetTick
	after.Version++
	after.SourceFactTick = &targetTick
	after.SourceFactSequence = &factSequence
	fact, err := insertCityFacilityLifecycleFact(ctx, tx, cityFacilityLifecycleFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		phase: CityFacilityLifecyclePhaseCommand, sourceCommandID: &command.ID,
		factType:    CityFacilityLifecycleFactCapacityChanged,
		subjectKind: "facility", subjectCode: facilityCode,
		versionBefore: before.Version, versionAfter: after.Version,
		payload: map[string]any{
			"schema_version": 1, "installed_capacity_units": ref.installedCapacityUnits,
			"state_before": before, "state_after": after,
		},
	})
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_facility_lifecycle_states
SET staff_required_units = $3, staffing_factor_milli = $4,
    effective_factor_milli = $5, updated_tick = $6,
    version = version + 1, source_fact_id = $7, updated_at = NOW()
WHERE world_id = $1 AND facility_id = $2 AND version = $8`,
		worldID, ref.facilityID, after.StaffRequiredUnits, after.StaffingFactorMilli,
		after.EffectiveFactorMilli, targetTick, fact.id, before.Version)
	if err != nil {
		return nil, fmt.Errorf("update facility lifecycle capacity requirement: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, facilityCode); err != nil {
		return nil, err
	}
	if err = advanceCityFacilityLifecycleProfile(ctx, tx, worldID,
		cityFacilityLifecycleProfileDeltas{}); err != nil {
		return nil, err
	}
	if err = postCityFacilityLifecycleFact(ctx, tx, fact.id); err != nil {
		return nil, err
	}
	return &fact.fact, nil
}

func loadCityFacilityLifecycleEntityRef(
	ctx context.Context, queryer citySQLQueryer, worldID int64, code string, lock bool,
) (*cityFacilityLifecycleEntityRef, error) {
	query := `
SELECT entity.id, entity.code, entity.entity_type
FROM city_economic_entities entity
WHERE entity.world_id = $1 AND entity.code = $2 AND entity.status = 'active'`
	if lock {
		query += ` FOR UPDATE OF entity`
	}
	ref := &cityFacilityLifecycleEntityRef{}
	err := queryer.QueryRowContext(ctx, query, worldID, code).Scan(
		&ref.id, &ref.code, &ref.entityType,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityFacilityLifecycleReject(cityFacilityLifecycleRejectionEntityInvalid)
	}
	if err != nil {
		return nil, fmt.Errorf("load city facility lifecycle entity: %w", err)
	}
	capacityQuery := ""
	switch ref.entityType {
	case CityEntityTypeFirm:
		capacityQuery = `
SELECT employee_units FROM city_firm_states
WHERE world_id = $1 AND entity_id = $2`
	case CityEntityTypeGovernment:
		capacityQuery = `
SELECT public_service_capacity_units FROM city_government_states
WHERE world_id = $1 AND entity_id = $2`
	default:
		return nil, cityFacilityLifecycleReject(cityFacilityLifecycleRejectionEntityInvalid)
	}
	if lock {
		capacityQuery += ` FOR UPDATE`
	}
	if err = queryer.QueryRowContext(ctx, capacityQuery, worldID, ref.id).Scan(
		&ref.staffCapacity,
	); errors.Is(err, sql.ErrNoRows) || ref.staffCapacity < 0 {
		return nil, cityFacilityLifecycleReject(cityFacilityLifecycleRejectionEntityInvalid)
	} else if err != nil {
		return nil, fmt.Errorf("load city facility lifecycle entity capacity: %w", err)
	}
	return ref, nil
}

func loadCityFacilityBudgetLineRef(
	ctx context.Context, queryer citySQLQueryer, worldID int64, code string,
) (*cityBudgetLineRef, error) {
	ref := &cityBudgetLineRef{}
	err := queryer.QueryRowContext(ctx, `
SELECT id, code, appropriated_units, committed_units, spent_units, version
FROM city_government_budget_lines
WHERE world_id = $1 AND code = $2
FOR UPDATE`, worldID, code).Scan(
		&ref.id, &ref.code, &ref.appropriatedUnits, &ref.committedUnits,
		&ref.spentUnits, &ref.version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityFacilityLifecycleReject(cityFacilityLifecycleRejectionBudgetInvalid)
	}
	if err != nil {
		return nil, fmt.Errorf("load city facility operation budget: %w", err)
	}
	return ref, nil
}

func loadCityFacilityOperationRef(
	ctx context.Context, queryer citySQLQueryer, worldID int64, code string, lock bool,
) (*cityFacilityOperationRef, error) {
	query := `
SELECT operation.id, operation.facility_id, facility.code, operation.operation_type,
       operation.status, operation.sponsor_entity_id, sponsor.code,
       operation.executor_entity_id, executor.code, operation.budget_line_id,
       budget.code, operation.planned_start_tick, operation.started_tick,
       operation.completed_tick, operation.duration_ticks, operation.progress_milli,
       operation.required_basic_material_units,
       operation.required_capital_goods_units, operation.required_labor_units,
       operation.budget_units, operation.budget_committed_units,
       operation.budget_spent_units, operation.created_tick,
       operation.updated_tick, operation.version, source.tick, source.sequence,
       operation.metadata
FROM city_facility_operations operation
JOIN city_facilities facility ON facility.id = operation.facility_id
JOIN city_economic_entities sponsor ON sponsor.id = operation.sponsor_entity_id
JOIN city_economic_entities executor ON executor.id = operation.executor_entity_id
LEFT JOIN city_government_budget_lines budget ON budget.id = operation.budget_line_id
JOIN city_facility_lifecycle_facts source ON source.id = operation.source_fact_id
WHERE operation.world_id = $1 AND operation.code = $2`
	if lock {
		query += ` FOR UPDATE OF operation`
	}
	ref := &cityFacilityOperationRef{}
	var budgetID sql.NullInt64
	var budgetCode sql.NullString
	var started, completed sql.NullInt64
	err := queryer.QueryRowContext(ctx, query, worldID, code).Scan(
		&ref.id, &ref.facilityID, &ref.value.FacilityCode,
		&ref.value.OperationType, &ref.value.Status, &ref.sponsorEntityID,
		&ref.value.SponsorEntityCode, &ref.executorEntityID,
		&ref.value.ExecutorEntityCode, &budgetID, &budgetCode,
		&ref.value.PlannedStartTick, &started, &completed,
		&ref.value.DurationTicks, &ref.value.ProgressMilli,
		&ref.value.RequiredBasicMaterialUnits,
		&ref.value.RequiredCapitalGoodsUnits, &ref.value.RequiredLaborUnits,
		&ref.value.BudgetUnits, &ref.value.BudgetCommittedUnits,
		&ref.value.BudgetSpentUnits, &ref.value.CreatedTick,
		&ref.value.UpdatedTick, &ref.value.Version,
		&ref.value.SourceFactTick, &ref.value.SourceFactSequence,
		&ref.value.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityFacilityLifecycleReject(cityFacilityLifecycleRejectionOperationNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load city facility operation: %w", err)
	}
	ref.value.Code = code
	ref.budgetLineID = nullInt64Pointer(budgetID)
	ref.value.BudgetCode = nullStringPointer(budgetCode)
	ref.value.StartedTick = nullInt64Pointer(started)
	ref.value.CompletedTick = nullInt64Pointer(completed)
	return ref, nil
}

func loadOptionalCityFacilityStaffAssignmentRef(
	ctx context.Context, queryer citySQLQueryer, worldID int64, code string, lock bool,
) (*cityFacilityStaffAssignmentRef, error) {
	query := `
SELECT assignment.id, facility.code, assignment.role_code,
       assignment.subject_kind, assignment.subject_code,
       assignment.entity_id, assignment.actor_id, assignment.assigned_units,
       assignment.qualification_milli, assignment.effective_units,
       assignment.status, assignment.created_tick, assignment.updated_tick,
       assignment.version, source.tick, source.sequence, assignment.metadata
FROM city_facility_staff_assignments assignment
JOIN city_facilities facility ON facility.id = assignment.facility_id
JOIN city_facility_lifecycle_facts source ON source.id = assignment.source_fact_id
WHERE assignment.world_id = $1 AND assignment.code = $2`
	if lock {
		query += ` FOR UPDATE OF assignment`
	}
	ref := &cityFacilityStaffAssignmentRef{}
	var entityID, actorID sql.NullInt64
	err := queryer.QueryRowContext(ctx, query, worldID, code).Scan(
		&ref.id, &ref.value.FacilityCode, &ref.value.RoleCode,
		&ref.value.SubjectKind, &ref.value.SubjectCode, &entityID, &actorID,
		&ref.value.AssignedUnits, &ref.value.QualificationMilli,
		&ref.value.EffectiveUnits, &ref.value.Status,
		&ref.value.CreatedTick, &ref.value.UpdatedTick, &ref.value.Version,
		&ref.value.SourceFactTick, &ref.value.SourceFactSequence,
		&ref.value.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load city facility staff assignment: %w", err)
	}
	ref.value.Code = code
	ref.entityID = nullInt64Pointer(entityID)
	ref.actorID = nullInt64Pointer(actorID)
	return ref, nil
}

func (s *CityEconomyService) applyCityFacilityLifecycleCommand(
	ctx context.Context, tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceSequence int64,
	unit *cityLedgerBaseUnit, command *CityCommand,
) (cityFacilityLifecycleExecution, error) {
	base := cityFacilityLifecycleExecution{
		facts:              make([]CityFacilityLifecycleFact, 0, 2),
		journals:           make([]*CityJournal, 0, 1),
		resourceOperations: make([]*CityResourceOperation, 0, 2),
		nextFactSequence:   factSequence, nextJournalSequence: journalSequence,
		nextResourceSequence: resourceSequence,
	}
	if _, err := tx.ExecContext(ctx, `SAVEPOINT city_facility_lifecycle_command`); err != nil {
		return base, fmt.Errorf("create city facility lifecycle savepoint: %w", err)
	}
	execution, err := s.postCityFacilityLifecycleCommand(
		ctx, tx, worldID, targetTick, factSequence, journalSequence,
		resourceSequence, unit, command,
	)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_facility_lifecycle_command`); rollbackErr != nil {
			return base, fmt.Errorf("rollback city facility lifecycle command after %v: %w", err, rollbackErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT city_facility_lifecycle_command`); releaseErr != nil {
			return base, fmt.Errorf("release rejected city facility lifecycle command: %w", releaseErr)
		}
		if code := cityFacilityLifecycleBusinessRejectionCode(err); code != "" {
			base.pending = rejectedCityCommand(command, code)
			return base, nil
		}
		return base, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_facility_lifecycle_command`); err != nil {
		return base, fmt.Errorf("release city facility lifecycle command: %w", err)
	}
	if execution.nextFactSequence == 0 {
		execution.nextFactSequence = factSequence
	}
	if execution.nextJournalSequence == 0 {
		execution.nextJournalSequence = journalSequence
	}
	if execution.nextResourceSequence == 0 {
		execution.nextResourceSequence = resourceSequence
	}
	return execution, nil
}

func (s *CityEconomyService) postCityFacilityLifecycleCommand(
	ctx context.Context, tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceSequence int64,
	unit *cityLedgerBaseUnit, command *CityCommand,
) (cityFacilityLifecycleExecution, error) {
	switch command.CommandType {
	case CityCommandTypeFacilityOperationSchedule:
		payload, err := decodeStoredCityCommandPayload[cityFacilityOperationSchedulePayload](command)
		if err != nil {
			return cityFacilityLifecycleExecution{}, err
		}
		return s.scheduleCityFacilityOperation(
			ctx, tx, worldID, targetTick, factSequence, command, payload,
		)
	case CityCommandTypeFacilityOperationStart:
		payload, err := decodeStoredCityCommandPayload[cityFacilityOperationStartPayload](command)
		if err != nil {
			return cityFacilityLifecycleExecution{}, err
		}
		return s.startCityFacilityOperation(
			ctx, tx, worldID, targetTick, factSequence, journalSequence,
			resourceSequence, unit, command, payload,
		)
	case CityCommandTypeFacilityOperationCancel:
		payload, err := decodeStoredCityCommandPayload[cityFacilityOperationCancelPayload](command)
		if err != nil {
			return cityFacilityLifecycleExecution{}, err
		}
		return s.cancelCityFacilityOperation(
			ctx, tx, worldID, targetTick, factSequence, command, payload,
		)
	case CityCommandTypeFacilityStaffingConfigure:
		payload, err := decodeStoredCityCommandPayload[cityFacilityStaffingConfigurePayload](command)
		if err != nil {
			return cityFacilityLifecycleExecution{}, err
		}
		return s.configureCityFacilityStaffing(
			ctx, tx, worldID, targetTick, factSequence, command, payload,
		)
	default:
		return cityFacilityLifecycleExecution{}, ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"command_type": command.CommandType},
		)
	}
}

func appliedCityFacilityLifecycleExecution(
	command *CityCommand, fact *cityFacilityLifecycleFactRecord,
	eventType string, result map[string]any,
) cityFacilityLifecycleExecution {
	return cityFacilityLifecycleExecution{
		pending: cityPendingEvent{
			command: command, status: CityCommandStatusApplied,
			eventType: eventType,
			payload: map[string]any{
				"fact_type":      fact.fact.FactType,
				"subject_kind":   fact.fact.SubjectKind,
				"subject_code":   fact.fact.SubjectCode,
				"version_before": fact.fact.VersionBefore,
				"version_after":  fact.fact.VersionAfter,
			},
			result: result,
		},
		facts:            []CityFacilityLifecycleFact{fact.fact},
		nextFactSequence: fact.fact.Sequence + 1,
	}
}

func validateCityFacilityOperationTransition(
	operationType string, state CityFacilityLifecycleState,
) error {
	switch operationType {
	case CityFacilityOperationCommission:
		if state.LifecycleStatus != CityFacilityLifecycleStatusUncommissioned ||
			state.OpenIncidentCode != nil {
			return cityFacilityLifecycleReject(cityFacilityLifecycleRejectionStateTransition)
		}
	case CityFacilityOperationMaintenance:
		if state.LifecycleStatus != CityFacilityLifecycleStatusOperational ||
			state.OpenIncidentCode != nil {
			return cityFacilityLifecycleReject(cityFacilityLifecycleRejectionStateTransition)
		}
	case CityFacilityOperationRepair:
		if state.LifecycleStatus != CityFacilityLifecycleStatusFailed ||
			state.OpenIncidentCode == nil {
			return cityFacilityLifecycleReject(cityFacilityLifecycleRejectionStateTransition)
		}
	case CityFacilityOperationDecommission:
		if state.LifecycleStatus != CityFacilityLifecycleStatusOperational &&
			state.LifecycleStatus != CityFacilityLifecycleStatusFailed {
			return cityFacilityLifecycleReject(cityFacilityLifecycleRejectionStateTransition)
		}
	default:
		return cityFacilityLifecycleReject(cityFacilityLifecycleRejectionStateTransition)
	}
	if state.ActiveOperationCode != nil {
		return cityFacilityLifecycleReject(cityFacilityLifecycleRejectionOperationConflict)
	}
	return nil
}

func (s *CityEconomyService) scheduleCityFacilityOperation(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, payload cityFacilityOperationSchedulePayload,
) (cityFacilityLifecycleExecution, error) {
	ref, err := loadCityFacilityLifecycleRef(ctx, tx, worldID, payload.FacilityCode, true)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if ref.state.Version != payload.ExpectedFacilityVersion {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionVersionConflict,
		)
	}
	if payload.PlannedStartTick < targetTick {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionStartNotDue,
		)
	}
	if err = validateCityFacilityOperationTransition(payload.OperationType, ref.state); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if payload.OperationType != CityFacilityOperationDecommission &&
		ref.installedCapacityUnits <= 0 {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionStateTransition,
		)
	}
	var duplicate bool
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM city_facility_operations WHERE world_id = $1 AND code = $2
)`, worldID, payload.Code).Scan(&duplicate); err != nil {
		return cityFacilityLifecycleExecution{}, fmt.Errorf("check city facility operation identity: %w", err)
	}
	if duplicate {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionOperationConflict,
		)
	}
	sponsor, err := loadCityFacilityLifecycleEntityRef(
		ctx, tx, worldID, payload.SponsorEntityCode, true,
	)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	executor, err := loadCityFacilityLifecycleEntityRef(
		ctx, tx, worldID, payload.ExecutorEntityCode, true,
	)
	if err != nil || executor.entityType != CityEntityTypeFirm {
		if err != nil {
			return cityFacilityLifecycleExecution{}, err
		}
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionEntityInvalid,
		)
	}
	if sponsor.entityType != CityEntityTypeGovernment && sponsor.entityType != CityEntityTypeFirm {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionEntityInvalid,
		)
	}
	duration, basicMaterial, capitalGoods, labor, budget, err :=
		cityFacilityLifecycleOperationPlan(ref.policy, payload.OperationType, ref.installedCapacityUnits)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	var budgetLine *cityBudgetLineRef
	if sponsor.entityType == CityEntityTypeGovernment {
		if payload.BudgetCode == "" {
			return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
				cityFacilityLifecycleRejectionBudgetInvalid,
			)
		}
		budgetLine, err = loadCityFacilityBudgetLineRef(ctx, tx, worldID, payload.BudgetCode)
		if err != nil {
			return cityFacilityLifecycleExecution{}, err
		}
		available := budgetLine.appropriatedUnits - budgetLine.committedUnits - budgetLine.spentUnits
		if available < budget {
			return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
				cityFacilityLifecycleRejectionBudgetInsufficient,
			)
		}
	} else if payload.BudgetCode != "" {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionBudgetInvalid,
		)
	}
	beforeState := ref.state
	afterState := beforeState
	afterState.ActiveOperationCode = &payload.Code
	afterState.UpdatedTick = targetTick
	afterState.Version++
	afterState.SourceFactTick = &targetTick
	afterState.SourceFactSequence = &factSequence
	operation := CityFacilityOperation{
		Code: payload.Code, FacilityCode: payload.FacilityCode,
		OperationType: payload.OperationType, Status: CityFacilityOperationStatusPlanned,
		SponsorEntityCode: sponsor.code, ExecutorEntityCode: executor.code,
		PlannedStartTick: payload.PlannedStartTick, DurationTicks: duration,
		RequiredBasicMaterialUnits: basicMaterial,
		RequiredCapitalGoodsUnits:  capitalGoods, RequiredLaborUnits: labor,
		BudgetUnits: budget, CreatedTick: targetTick, UpdatedTick: targetTick,
		Version: 1, SourceFactTick: targetTick, SourceFactSequence: factSequence,
		Metadata: payload.Metadata,
	}
	var budgetLineID any
	var budgetMovement any
	if budgetLine != nil {
		operation.BudgetCode = &budgetLine.code
		operation.BudgetCommittedUnits = budget
		budgetLineID = budgetLine.id
		budgetMovement = CityFacilityBudgetMovement{
			SourceFactTick: targetTick, SourceFactSequence: factSequence,
			OperationCode: operation.Code, BudgetCode: budgetLine.code,
			MovementType: "commit", AmountUnits: budget,
			CommittedBeforeUnits: budgetLine.committedUnits,
			CommittedAfterUnits:  budgetLine.committedUnits + budget,
			SpentBeforeUnits:     budgetLine.spentUnits,
			SpentAfterUnits:      budgetLine.spentUnits,
			BudgetVersionBefore:  budgetLine.version,
			BudgetVersionAfter:   budgetLine.version + 1,
			Memo:                 "Facility operation " + operation.Code,
		}
	}
	fact, err := insertCityFacilityLifecycleFact(ctx, tx, cityFacilityLifecycleFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		phase: CityFacilityLifecyclePhaseCommand, sourceCommandID: &command.ID,
		factType:    CityFacilityLifecycleFactOperationScheduled,
		subjectKind: "operation", subjectCode: operation.Code,
		versionBefore: 0, versionAfter: 1,
		payload: map[string]any{
			"schema_version": 1, "operation_after": operation,
			"state_before": beforeState, "state_after": afterState,
			"budget_movement": budgetMovement,
		},
	})
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	var operationID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_facility_operations
    (world_id, code, facility_id, operation_type, status,
     sponsor_entity_id, executor_entity_id, budget_line_id,
     planned_start_tick, duration_ticks, progress_milli,
     required_basic_material_units, required_capital_goods_units,
     required_labor_units, budget_units, budget_committed_units,
     budget_spent_units, created_tick, updated_tick, version,
     source_fact_id, metadata)
VALUES ($1, $2, $3, $4, 'planned', $5, $6, $7, $8, $9, 0,
        $10, $11, $12, $13, $14, 0, $15, $15, 1, $16, $17::jsonb)
RETURNING id`, worldID, operation.Code, ref.facilityID, operation.OperationType,
		sponsor.id, executor.id, budgetLineID, operation.PlannedStartTick,
		operation.DurationTicks, operation.RequiredBasicMaterialUnits,
		operation.RequiredCapitalGoodsUnits, operation.RequiredLaborUnits,
		operation.BudgetUnits, operation.BudgetCommittedUnits, targetTick,
		fact.id, operation.Metadata).Scan(&operationID)
	if err != nil {
		return cityFacilityLifecycleExecution{}, fmt.Errorf("schedule city facility operation: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_facility_lifecycle_states
SET active_operation_code = $3, updated_tick = $4,
    version = version + 1, source_fact_id = $5, updated_at = NOW()
WHERE world_id = $1 AND facility_id = $2 AND version = $6
  AND active_operation_code IS NULL`, worldID, ref.facilityID, operation.Code,
		targetTick, fact.id, beforeState.Version)
	if err != nil {
		return cityFacilityLifecycleExecution{}, fmt.Errorf("reserve facility lifecycle operation: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, operation.Code); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	budgetDelta := int64(0)
	if budgetLine != nil {
		var movementID int64
		if err = tx.QueryRowContext(ctx, `
SELECT post_city_facility_budget_movement($1, $2, $3, $4, 'commit', $5, $6)`,
			fact.id, operationID, budgetLine.id, budgetLine.version,
			operation.BudgetUnits, "Facility operation "+operation.Code).Scan(&movementID); err != nil {
			return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
				cityFacilityLifecycleRejectionBudgetInsufficient,
			)
		}
		budgetDelta = 1
	}
	if err = advanceCityFacilityLifecycleProfile(ctx, tx, worldID,
		cityFacilityLifecycleProfileDeltas{operations: 1, budgetMovements: budgetDelta}); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if err = postCityFacilityLifecycleFact(ctx, tx, fact.id); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	execution := appliedCityFacilityLifecycleExecution(command, fact,
		"city.facility.operation.scheduled", map[string]any{
			"operation": operation, "facility_state": afterState,
		})
	return execution, nil
}

func cityFacilityLifecycleStatusForActiveOperation(operationType, current string) string {
	switch operationType {
	case CityFacilityOperationMaintenance:
		return CityFacilityLifecycleStatusMaintenance
	case CityFacilityOperationRepair:
		return CityFacilityLifecycleStatusFailed
	case CityFacilityOperationDecommission:
		return CityFacilityLifecycleStatusDecommissioning
	default:
		return current
	}
}

func cityFacilityOperationExpenseAccount(entityType, operationType string) string {
	if entityType == CityEntityTypeGovernment {
		if operationType == CityFacilityOperationCommission ||
			operationType == CityFacilityOperationDecommission {
			return "capital_expenditure"
		}
		return "public_service_expense"
	}
	if operationType == CityFacilityOperationCommission ||
		operationType == CityFacilityOperationDecommission {
		return "fixed_assets"
	}
	return "wage_expense"
}

func loadCityFacilityExecutorReservedLabor(
	ctx context.Context, queryer citySQLQueryer,
	worldID, executorEntityID, excludedOperationID int64,
) (int64, error) {
	var development, operations, staffing int64
	err := queryer.QueryRowContext(ctx, `
SELECT
    COALESCE((
        SELECT SUM(project.required_labor_units)::BIGINT
        FROM city_development_projects project
        WHERE project.world_id = $1 AND project.developer_entity_id = $2
          AND project.status = 'under_construction'
    ), 0),
    COALESCE((
        SELECT SUM(operation.required_labor_units)::BIGINT
        FROM city_facility_operations operation
        WHERE operation.world_id = $1 AND operation.executor_entity_id = $2
          AND operation.status = 'active' AND operation.id <> $3
    ), 0),
    COALESCE((
        SELECT SUM(assignment.assigned_units)::BIGINT
        FROM city_facility_staff_assignments assignment
        WHERE assignment.world_id = $1 AND assignment.entity_id = $2
          AND assignment.status = 'active'
    ), 0)`, worldID, executorEntityID, excludedOperationID).Scan(
		&development, &operations, &staffing,
	)
	if err != nil {
		return 0, fmt.Errorf("load city facility executor reservations: %w", err)
	}
	if development > math.MaxInt64-operations || development+operations > math.MaxInt64-staffing {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "facility_labor_reservation"})
	}
	return development + operations + staffing, nil
}

func (s *CityEconomyService) startCityFacilityOperation(
	ctx context.Context, tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceSequence int64,
	unit *cityLedgerBaseUnit, command *CityCommand,
	payload cityFacilityOperationStartPayload,
) (cityFacilityLifecycleExecution, error) {
	operationRef, err := loadCityFacilityOperationRef(
		ctx, tx, worldID, payload.OperationCode, true,
	)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	operation := operationRef.value
	if operation.Status != CityFacilityOperationStatusPlanned {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionStateTransition,
		)
	}
	if operation.Version != payload.ExpectedOperationVersion {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionVersionConflict,
		)
	}
	if targetTick < operation.PlannedStartTick {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionStartNotDue,
		)
	}
	ref, err := loadCityFacilityLifecycleRef(
		ctx, tx, worldID, operation.FacilityCode, true,
	)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if ref.state.Version != payload.ExpectedFacilityVersion ||
		ref.state.ActiveOperationCode == nil ||
		*ref.state.ActiveOperationCode != operation.Code {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionVersionConflict,
		)
	}
	stateForValidation := ref.state
	stateForValidation.ActiveOperationCode = nil
	if err = validateCityFacilityOperationTransition(operation.OperationType, stateForValidation); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	sponsor, err := loadCityFacilityLifecycleEntityRef(
		ctx, tx, worldID, operation.SponsorEntityCode, true,
	)
	if err != nil || sponsor.id != operationRef.sponsorEntityID {
		if err != nil {
			return cityFacilityLifecycleExecution{}, err
		}
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionEntityInvalid,
		)
	}
	executor, err := loadCityFacilityLifecycleEntityRef(
		ctx, tx, worldID, operation.ExecutorEntityCode, true,
	)
	if err != nil || executor.id != operationRef.executorEntityID ||
		executor.entityType != CityEntityTypeFirm {
		if err != nil {
			return cityFacilityLifecycleExecution{}, err
		}
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionEntityInvalid,
		)
	}
	reservedLabor, err := loadCityFacilityExecutorReservedLabor(
		ctx, tx, worldID, executor.id, operationRef.id,
	)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if reservedLabor > executor.staffCapacity ||
		operation.RequiredLaborUnits > executor.staffCapacity-reservedLabor {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionLaborShortage,
		)
	}
	if unit == nil {
		return cityFacilityLifecycleExecution{}, ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"field": "facility_operation_monetary_unit"},
		)
	}
	var budgetLine *cityBudgetLineRef
	if sponsor.entityType == CityEntityTypeGovernment {
		if operation.BudgetCode == nil || operationRef.budgetLineID == nil {
			return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
				cityFacilityLifecycleRejectionBudgetInvalid,
			)
		}
		budgetLine, err = loadCityFacilityBudgetLineRef(ctx, tx, worldID, *operation.BudgetCode)
		if err != nil {
			return cityFacilityLifecycleExecution{}, err
		}
		if budgetLine.id != *operationRef.budgetLineID ||
			operation.BudgetCommittedUnits != operation.BudgetUnits ||
			budgetLine.committedUnits < operation.BudgetUnits {
			return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
				cityFacilityLifecycleRejectionBudgetInvalid,
			)
		}
	}
	beforeState := ref.state
	afterState := beforeState
	afterState.LifecycleStatus = cityFacilityLifecycleStatusForActiveOperation(
		operation.OperationType, beforeState.LifecycleStatus,
	)
	afterState.OperationFactorMilli = 0
	afterState.EffectiveFactorMilli = cityFacilityLifecycleEffectiveFactor(
		afterState.LifecycleStatus, afterState.ConditionMilli,
		afterState.StaffingFactorMilli, afterState.OperationFactorMilli,
	)
	afterState.Metadata, err = cityFacilityLifecycleMetadataWithString(
		afterState.Metadata, "staffing_source", "assignments",
	)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	afterState.UpdatedTick = targetTick
	afterState.Version++
	afterState.SourceFactTick = &targetTick
	afterState.SourceFactSequence = &factSequence
	startedTick := targetTick
	afterOperation := operation
	afterOperation.Status = CityFacilityOperationStatusActive
	afterOperation.StartedTick = &startedTick
	afterOperation.BudgetCommittedUnits = 0
	afterOperation.BudgetSpentUnits = operation.BudgetUnits
	afterOperation.UpdatedTick = targetTick
	afterOperation.Version++
	afterOperation.SourceFactTick = targetTick
	afterOperation.SourceFactSequence = factSequence
	resourceRefs := make([]map[string]any, 0, 2)
	nextResourceSequence := resourceSequence
	for _, requirement := range []struct {
		code     string
		quantity int64
	}{
		{code: "basic_material", quantity: operation.RequiredBasicMaterialUnits},
		{code: "capital_goods", quantity: operation.RequiredCapitalGoodsUnits},
	} {
		if requirement.quantity <= 0 {
			continue
		}
		resourceRefs = append(resourceRefs, map[string]any{
			"tick": targetTick, "sequence": nextResourceSequence,
			"operation_key": "facility:" + operation.Code + ":" + requirement.code,
			"resource_code": requirement.code, "quantity_units": requirement.quantity,
		})
		nextResourceSequence++
	}
	journalRef := map[string]any{
		"tick": targetTick, "sequence": journalSequence,
		"operation_key": "facility:" + operation.Code,
		"amount_units":  operation.BudgetUnits,
	}
	var budgetMovement any
	if budgetLine != nil {
		budgetMovement = CityFacilityBudgetMovement{
			SourceFactTick: targetTick, SourceFactSequence: factSequence,
			OperationCode: operation.Code, BudgetCode: budgetLine.code,
			MovementType: "spend", AmountUnits: operation.BudgetUnits,
			CommittedBeforeUnits: budgetLine.committedUnits,
			CommittedAfterUnits:  budgetLine.committedUnits - operation.BudgetUnits,
			SpentBeforeUnits:     budgetLine.spentUnits,
			SpentAfterUnits:      budgetLine.spentUnits + operation.BudgetUnits,
			BudgetVersionBefore:  budgetLine.version,
			BudgetVersionAfter:   budgetLine.version + 1,
			Memo:                 "Start facility operation " + operation.Code,
		}
	}
	fact, err := insertCityFacilityLifecycleFact(ctx, tx, cityFacilityLifecycleFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		phase: CityFacilityLifecyclePhaseCommand, sourceCommandID: &command.ID,
		factType:    CityFacilityLifecycleFactOperationStarted,
		subjectKind: "operation", subjectCode: operation.Code,
		versionBefore: operation.Version, versionAfter: afterOperation.Version,
		payload: map[string]any{
			"schema_version": 1, "operation_before": operation,
			"operation_after": afterOperation, "state_before": beforeState,
			"state_after": afterState, "journal": journalRef,
			"resource_operations": resourceRefs, "budget_movement": budgetMovement,
			"command_metadata": payload.Metadata,
		},
	})
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	resourceOperations := make([]*CityResourceOperation, 0, len(resourceRefs))
	nextResourceSequence = resourceSequence
	for _, requirement := range []struct {
		code     string
		quantity int64
	}{
		{code: "basic_material", quantity: operation.RequiredBasicMaterialUnits},
		{code: "capital_goods", quantity: operation.RequiredCapitalGoodsUnits},
	} {
		if requirement.quantity <= 0 {
			continue
		}
		balance, balanceErr := ensureCityInventoryRef(
			ctx, tx, worldID, executor.id, ref.districtCode, requirement.code,
		)
		if balanceErr != nil {
			return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
				cityFacilityLifecycleRejectionResourceShortage,
			)
		}
		resourceOperation, operationErr := postCityResourceOperation(
			ctx, tx, cityResourceOperationSpec{
				worldID: worldID, tick: targetTick, sequence: nextResourceSequence,
				operationKey:  "facility:" + operation.Code + ":" + requirement.code,
				operationType: "consumption", sourceCommandID: &command.ID,
				actorEntityID: executor.id, districtID: ref.districtID,
				description: "Facility operation input: " + requirement.code,
				metadata: map[string]any{
					"facility_lifecycle_fact_id": fact.id,
					"facility_operation_code":    operation.Code,
					"resource_code":              requirement.code,
					"quantity_units":             requirement.quantity,
					"schema_version":             1,
				},
				lines: []cityResourcePostingLine{{
					balance: balance, direction: "out",
					quantityUnits: requirement.quantity,
					memo:          "Facility operation " + operation.Code,
				}},
			},
		)
		if operationErr != nil {
			if cityResourceBusinessRejectionCode(operationErr) != "" {
				return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
					cityFacilityLifecycleRejectionResourceShortage,
				)
			}
			return cityFacilityLifecycleExecution{}, operationErr
		}
		resourceOperations = append(resourceOperations, resourceOperation)
		nextResourceSequence++
	}
	sponsorExpense, err := loadCityLedgerAccount(
		ctx, tx, worldID, unit.id, sponsor.id, sponsor.entityType,
		cityFacilityOperationExpenseAccount(sponsor.entityType, operation.OperationType),
	)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	executorRevenue, err := loadCityLedgerAccount(
		ctx, tx, worldID, unit.id, executor.id, CityEntityTypeFirm, "revenue",
	)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	lines := []cityLedgerPostingLine{{
		account: sponsorExpense, debitUnits: operation.BudgetUnits,
		memo: "Facility operation " + operation.Code,
	}}
	if sponsor.id == executor.id {
		lines = append(lines, cityLedgerPostingLine{
			account: executorRevenue, creditUnits: operation.BudgetUnits,
			memo: "Internal facility operation " + operation.Code,
		})
	} else {
		sponsorCash, loadErr := loadCityLedgerAccount(
			ctx, tx, worldID, unit.id, sponsor.id, sponsor.entityType, "cash",
		)
		if loadErr != nil {
			return cityFacilityLifecycleExecution{}, loadErr
		}
		executorCash, loadErr := loadCityLedgerAccount(
			ctx, tx, worldID, unit.id, executor.id, CityEntityTypeFirm, "cash",
		)
		if loadErr != nil {
			return cityFacilityLifecycleExecution{}, loadErr
		}
		lines = append(lines,
			cityLedgerPostingLine{account: sponsorCash, creditUnits: operation.BudgetUnits, memo: "Facility operation payment " + operation.Code},
			cityLedgerPostingLine{account: executorCash, debitUnits: operation.BudgetUnits, memo: "Facility operation receipt " + operation.Code},
			cityLedgerPostingLine{account: executorRevenue, creditUnits: operation.BudgetUnits, memo: "Facility operation revenue " + operation.Code},
		)
	}
	journalType := "facility_operation"
	if operation.OperationType == CityFacilityOperationCommission ||
		operation.OperationType == CityFacilityOperationDecommission {
		journalType = "facility_capital"
	}
	journal, err := postCityJournal(ctx, tx, cityLedgerJournalSpec{
		worldID: worldID, unit: unit, tick: targetTick, sequence: journalSequence,
		operationKey: "facility:" + operation.Code, journalType: journalType,
		sourceCommandID: &command.ID, description: "Facility operation " + operation.Code,
		metadata: map[string]any{
			"facility_lifecycle_fact_id": fact.id,
			"facility_operation_code":    operation.Code,
			"operation_type":             operation.OperationType,
			"schema_version":             1,
		},
		lines: lines,
	})
	if err != nil {
		if cityLedgerBusinessRejectionCode(err) != "" {
			return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
				cityFacilityLifecycleRejectionCashInsufficient,
			)
		}
		return cityFacilityLifecycleExecution{}, err
	}
	budgetDelta := int64(0)
	if budgetLine != nil {
		var movementID int64
		if err = tx.QueryRowContext(ctx, `
SELECT post_city_facility_budget_movement($1, $2, $3, $4, 'spend', $5, $6)`,
			fact.id, operationRef.id, budgetLine.id, budgetLine.version,
			operation.BudgetUnits, "Start facility operation "+operation.Code).Scan(&movementID); err != nil {
			return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
				cityFacilityLifecycleRejectionBudgetInvalid,
			)
		}
		budgetDelta = 1
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_facility_operations
SET status = 'active', started_tick = $3,
    budget_committed_units = 0, budget_spent_units = budget_units,
    updated_tick = $3, version = version + 1,
    source_fact_id = $4, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'planned' AND version = $5`,
		worldID, operationRef.id, targetTick, fact.id, operation.Version)
	if err != nil {
		return cityFacilityLifecycleExecution{}, fmt.Errorf("start city facility operation: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, operation.Code); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	result, err = tx.ExecContext(ctx, `
UPDATE city_facility_lifecycle_states
SET lifecycle_status = $3, operation_factor_milli = 0,
    effective_factor_milli = $4, updated_tick = $5,
    version = version + 1, source_fact_id = $6,
    metadata = $7::jsonb, updated_at = NOW()
WHERE world_id = $1 AND facility_id = $2 AND version = $8
  AND active_operation_code = $9`, worldID, ref.facilityID,
		afterState.LifecycleStatus, afterState.EffectiveFactorMilli, targetTick,
		fact.id, afterState.Metadata, beforeState.Version, operation.Code)
	if err != nil {
		return cityFacilityLifecycleExecution{}, fmt.Errorf("activate city facility lifecycle operation: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, operation.Code); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if err = advanceCityFacilityLifecycleProfile(ctx, tx, worldID,
		cityFacilityLifecycleProfileDeltas{budgetMovements: budgetDelta}); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if err = postCityFacilityLifecycleFact(ctx, tx, fact.id); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	execution := appliedCityFacilityLifecycleExecution(command, fact,
		"city.facility.operation.started", map[string]any{
			"operation": afterOperation, "facility_state": afterState,
			"journal_tick": journal.Tick, "journal_sequence": journal.Sequence,
			"resource_operation_count": len(resourceOperations),
		})
	execution.journals = []*CityJournal{journal}
	execution.resourceOperations = resourceOperations
	execution.nextJournalSequence = journalSequence + 1
	execution.nextResourceSequence = nextResourceSequence
	return execution, nil
}

func (s *CityEconomyService) cancelCityFacilityOperation(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, payload cityFacilityOperationCancelPayload,
) (cityFacilityLifecycleExecution, error) {
	operationRef, err := loadCityFacilityOperationRef(
		ctx, tx, worldID, payload.OperationCode, true,
	)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	operation := operationRef.value
	if operation.Status != CityFacilityOperationStatusPlanned {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionStateTransition,
		)
	}
	if operation.Version != payload.ExpectedOperationVersion {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionVersionConflict,
		)
	}
	ref, err := loadCityFacilityLifecycleRef(
		ctx, tx, worldID, operation.FacilityCode, true,
	)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if ref.state.Version != payload.ExpectedFacilityVersion ||
		ref.state.ActiveOperationCode == nil ||
		*ref.state.ActiveOperationCode != operation.Code {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionVersionConflict,
		)
	}
	var budgetLine *cityBudgetLineRef
	if operation.BudgetCode != nil {
		if operationRef.budgetLineID == nil || operation.BudgetCommittedUnits != operation.BudgetUnits {
			return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
				cityFacilityLifecycleRejectionBudgetInvalid,
			)
		}
		budgetLine, err = loadCityFacilityBudgetLineRef(ctx, tx, worldID, *operation.BudgetCode)
		if err != nil {
			return cityFacilityLifecycleExecution{}, err
		}
		if budgetLine.id != *operationRef.budgetLineID ||
			budgetLine.committedUnits < operation.BudgetUnits {
			return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
				cityFacilityLifecycleRejectionBudgetInvalid,
			)
		}
	}
	beforeState := ref.state
	afterState := beforeState
	afterState.ActiveOperationCode = nil
	afterState.UpdatedTick = targetTick
	afterState.Version++
	afterState.SourceFactTick = &targetTick
	afterState.SourceFactSequence = &factSequence
	afterOperation := operation
	afterOperation.Status = CityFacilityOperationStatusCancelled
	afterOperation.BudgetCommittedUnits = 0
	afterOperation.BudgetSpentUnits = 0
	afterOperation.UpdatedTick = targetTick
	afterOperation.Version++
	afterOperation.SourceFactTick = targetTick
	afterOperation.SourceFactSequence = factSequence
	var budgetMovement any
	if budgetLine != nil {
		budgetMovement = CityFacilityBudgetMovement{
			SourceFactTick: targetTick, SourceFactSequence: factSequence,
			OperationCode: operation.Code, BudgetCode: budgetLine.code,
			MovementType: "release", AmountUnits: operation.BudgetUnits,
			CommittedBeforeUnits: budgetLine.committedUnits,
			CommittedAfterUnits:  budgetLine.committedUnits - operation.BudgetUnits,
			SpentBeforeUnits:     budgetLine.spentUnits,
			SpentAfterUnits:      budgetLine.spentUnits,
			BudgetVersionBefore:  budgetLine.version,
			BudgetVersionAfter:   budgetLine.version + 1,
			Memo:                 "Cancel facility operation " + operation.Code,
		}
	}
	fact, err := insertCityFacilityLifecycleFact(ctx, tx, cityFacilityLifecycleFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		phase: CityFacilityLifecyclePhaseCommand, sourceCommandID: &command.ID,
		factType:    CityFacilityLifecycleFactOperationCancelled,
		subjectKind: "operation", subjectCode: operation.Code,
		versionBefore: operation.Version, versionAfter: afterOperation.Version,
		payload: map[string]any{
			"schema_version": 1, "operation_before": operation,
			"operation_after": afterOperation, "state_before": beforeState,
			"state_after": afterState, "budget_movement": budgetMovement,
			"command_metadata": payload.Metadata,
		},
	})
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	budgetDelta := int64(0)
	if budgetLine != nil {
		var movementID int64
		if err = tx.QueryRowContext(ctx, `
SELECT post_city_facility_budget_movement($1, $2, $3, $4, 'release', $5, $6)`,
			fact.id, operationRef.id, budgetLine.id, budgetLine.version,
			operation.BudgetUnits, "Cancel facility operation "+operation.Code).Scan(&movementID); err != nil {
			return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
				cityFacilityLifecycleRejectionBudgetInvalid,
			)
		}
		budgetDelta = 1
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_facility_operations
SET status = 'cancelled', budget_committed_units = 0,
    budget_spent_units = 0, updated_tick = $3,
    version = version + 1, source_fact_id = $4, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'planned' AND version = $5`,
		worldID, operationRef.id, targetTick, fact.id, operation.Version)
	if err != nil {
		return cityFacilityLifecycleExecution{}, fmt.Errorf("cancel city facility operation: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, operation.Code); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	result, err = tx.ExecContext(ctx, `
UPDATE city_facility_lifecycle_states
SET active_operation_code = NULL, updated_tick = $3,
    version = version + 1, source_fact_id = $4, updated_at = NOW()
WHERE world_id = $1 AND facility_id = $2 AND version = $5
  AND active_operation_code = $6`, worldID, ref.facilityID,
		targetTick, fact.id, beforeState.Version, operation.Code)
	if err != nil {
		return cityFacilityLifecycleExecution{}, fmt.Errorf("release facility lifecycle operation: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, operation.Code); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if err = advanceCityFacilityLifecycleProfile(ctx, tx, worldID,
		cityFacilityLifecycleProfileDeltas{budgetMovements: budgetDelta}); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if err = postCityFacilityLifecycleFact(ctx, tx, fact.id); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	execution := appliedCityFacilityLifecycleExecution(command, fact,
		"city.facility.operation.cancelled", map[string]any{
			"operation": afterOperation, "facility_state": afterState,
		})
	return execution, nil
}

func loadCityFacilityActorQualification(
	ctx context.Context, queryer citySQLQueryer, worldID int64, actorCode string,
) (actorID int64, qualification int, err error) {
	var status string
	var discipline int64
	var technician, apprentice bool
	err = queryer.QueryRowContext(ctx, `
SELECT actor.id, actor.status, COALESCE(attribute.value_units, 0),
       EXISTS (
           SELECT 1 FROM world_actor_roles role
           WHERE role.world_id = actor.world_id AND role.actor_id = actor.id
             AND role.role_code = 'profession.technician' AND role.status = 'active'
       ),
       EXISTS (
           SELECT 1 FROM world_actor_roles role
           WHERE role.world_id = actor.world_id AND role.actor_id = actor.id
             AND role.role_code = 'profession.apprentice' AND role.status = 'active'
       )
FROM world_actors actor
LEFT JOIN world_actor_attributes attribute
  ON attribute.world_id = actor.world_id AND attribute.actor_id = actor.id
 AND attribute.attribute_code = 'discipline'
WHERE actor.world_id = $1 AND actor.code = $2
FOR UPDATE OF actor`, worldID, actorCode).Scan(
		&actorID, &status, &discipline, &technician, &apprentice,
	)
	if errors.Is(err, sql.ErrNoRows) || status != "active" {
		return 0, 0, cityFacilityLifecycleReject(cityFacilityLifecycleRejectionQualification)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("load city facility actor qualification: %w", err)
	}
	if discipline < 0 {
		discipline = 0
	}
	if discipline > 100_000 {
		discipline = 100_000
	}
	switch {
	case technician:
		qualification = 800 + int(discipline*200/100_000)
	case apprentice:
		qualification = 600 + int(discipline*300/100_000)
	default:
		return 0, 0, cityFacilityLifecycleReject(cityFacilityLifecycleRejectionQualification)
	}
	return actorID, qualification, nil
}

func loadCityFacilityEntityReservedCapacity(
	ctx context.Context, queryer citySQLQueryer,
	worldID, entityID, excludedAssignmentID int64,
) (int64, error) {
	var assignments, operations, development int64
	err := queryer.QueryRowContext(ctx, `
SELECT
    COALESCE((
        SELECT SUM(assignment.assigned_units)::BIGINT
        FROM city_facility_staff_assignments assignment
        WHERE assignment.world_id = $1 AND assignment.entity_id = $2
          AND assignment.status = 'active' AND assignment.id <> $3
    ), 0),
    COALESCE((
        SELECT SUM(operation.required_labor_units)::BIGINT
        FROM city_facility_operations operation
        WHERE operation.world_id = $1 AND operation.executor_entity_id = $2
          AND operation.status = 'active'
    ), 0),
    COALESCE((
        SELECT SUM(project.required_labor_units)::BIGINT
        FROM city_development_projects project
        WHERE project.world_id = $1 AND project.developer_entity_id = $2
          AND project.status = 'under_construction'
    ), 0)`, worldID, entityID, excludedAssignmentID).Scan(
		&assignments, &operations, &development,
	)
	if err != nil {
		return 0, fmt.Errorf("load city facility staffing reservations: %w", err)
	}
	if assignments > math.MaxInt64-operations || assignments+operations > math.MaxInt64-development {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "facility_staffing_reservation"})
	}
	return assignments + operations + development, nil
}

func loadCityFacilityAssignedEffectiveUnits(
	ctx context.Context, queryer citySQLQueryer,
	worldID, facilityID, excludedAssignmentID int64,
) (int64, error) {
	var assigned int64
	err := queryer.QueryRowContext(ctx, `
SELECT COALESCE(SUM(effective_units), 0)::BIGINT
FROM city_facility_staff_assignments
WHERE world_id = $1 AND facility_id = $2 AND status = 'active'
  AND id <> $3`, worldID, facilityID, excludedAssignmentID).Scan(&assigned)
	if err != nil {
		return 0, fmt.Errorf("load city facility assigned staff: %w", err)
	}
	return assigned, nil
}

func (s *CityEconomyService) configureCityFacilityStaffing(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, payload cityFacilityStaffingConfigurePayload,
) (cityFacilityLifecycleExecution, error) {
	ref, err := loadCityFacilityLifecycleRef(
		ctx, tx, worldID, payload.FacilityCode, true,
	)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if ref.state.Version != payload.ExpectedFacilityVersion ||
		ref.state.LifecycleStatus == CityFacilityLifecycleStatusRetired {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionVersionConflict,
		)
	}
	before, err := loadOptionalCityFacilityStaffAssignmentRef(
		ctx, tx, worldID, payload.Code, true,
	)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if before == nil && payload.ExpectedVersion != 0 {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionStaffingConflict,
		)
	}
	if before != nil && (before.value.Version != payload.ExpectedVersion ||
		before.value.FacilityCode != payload.FacilityCode ||
		before.value.RoleCode != payload.RoleCode ||
		before.value.SubjectKind != payload.SubjectKind ||
		before.value.SubjectCode != payload.SubjectCode) {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionStaffingConflict,
		)
	}
	if before == nil && payload.Status == "released" {
		return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
			cityFacilityLifecycleRejectionStaffingConflict,
		)
	}
	excludedAssignmentID := int64(0)
	if before != nil {
		excludedAssignmentID = before.id
	}
	var entityID, actorID *int64
	qualification := 1000
	if payload.SubjectKind == "entity" {
		entity, loadErr := loadCityFacilityLifecycleEntityRef(
			ctx, tx, worldID, payload.SubjectCode, true,
		)
		if loadErr != nil {
			return cityFacilityLifecycleExecution{}, loadErr
		}
		entityID = &entity.id
		if payload.Status == "active" {
			reserved, reserveErr := loadCityFacilityEntityReservedCapacity(
				ctx, tx, worldID, entity.id, excludedAssignmentID,
			)
			if reserveErr != nil {
				return cityFacilityLifecycleExecution{}, reserveErr
			}
			if reserved > entity.staffCapacity || payload.AssignedUnits > entity.staffCapacity-reserved {
				return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
					cityFacilityLifecycleRejectionLaborShortage,
				)
			}
		}
	} else {
		id, actorQualification, qualificationErr := loadCityFacilityActorQualification(
			ctx, tx, worldID, payload.SubjectCode,
		)
		if qualificationErr != nil {
			return cityFacilityLifecycleExecution{}, qualificationErr
		}
		actorID = &id
		qualification = actorQualification
		if payload.Status == "active" {
			var conflict bool
			if err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM city_facility_staff_assignments
    WHERE world_id = $1 AND actor_id = $2 AND status = 'active' AND id <> $3
)`, worldID, id, excludedAssignmentID).Scan(&conflict); err != nil {
				return cityFacilityLifecycleExecution{}, fmt.Errorf("check city facility actor staffing: %w", err)
			}
			if conflict {
				return cityFacilityLifecycleExecution{}, cityFacilityLifecycleReject(
					cityFacilityLifecycleRejectionStaffingConflict,
				)
			}
		}
	}
	effectiveUnits, err := cityMulDivFloor(payload.AssignedUnits, qualification, 1000)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	versionBefore, versionAfter := int64(0), int64(1)
	createdTick := targetTick
	var beforeValue any
	if before != nil {
		versionBefore, versionAfter = before.value.Version, before.value.Version+1
		createdTick = before.value.CreatedTick
		beforeValue = before.value
	}
	afterAssignment := CityFacilityStaffAssignment{
		Code: payload.Code, FacilityCode: payload.FacilityCode,
		RoleCode: payload.RoleCode, SubjectKind: payload.SubjectKind,
		SubjectCode: payload.SubjectCode, AssignedUnits: payload.AssignedUnits,
		QualificationMilli: qualification, EffectiveUnits: effectiveUnits,
		Status: payload.Status, CreatedTick: createdTick, UpdatedTick: targetTick,
		Version: versionAfter, SourceFactTick: targetTick,
		SourceFactSequence: factSequence, Metadata: payload.Metadata,
	}
	assigned, err := loadCityFacilityAssignedEffectiveUnits(
		ctx, tx, worldID, ref.facilityID, excludedAssignmentID,
	)
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if payload.Status == "active" {
		if assigned > math.MaxInt64-effectiveUnits {
			return cityFacilityLifecycleExecution{}, ErrCitySimulationInvariant.WithMetadata(
				map[string]string{"field": "facility_staffing_total"},
			)
		}
		assigned += effectiveUnits
	}
	beforeState := ref.state
	afterState := beforeState
	afterState.StaffAssignedUnits = assigned
	afterState.StaffingFactorMilli = cityFacilityStaffingFactor(
		afterState.StaffRequiredUnits, assigned,
	)
	afterState.EffectiveFactorMilli = cityFacilityLifecycleEffectiveFactor(
		afterState.LifecycleStatus, afterState.ConditionMilli,
		afterState.StaffingFactorMilli, afterState.OperationFactorMilli,
	)
	afterState.UpdatedTick = targetTick
	afterState.Version++
	afterState.SourceFactTick = &targetTick
	afterState.SourceFactSequence = &factSequence
	fact, err := insertCityFacilityLifecycleFact(ctx, tx, cityFacilityLifecycleFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		phase: CityFacilityLifecyclePhaseCommand, sourceCommandID: &command.ID,
		factType:    CityFacilityLifecycleFactStaffingConfigured,
		subjectKind: "staffing", subjectCode: payload.Code,
		versionBefore: versionBefore, versionAfter: versionAfter,
		payload: map[string]any{
			"schema_version": 1, "assignment_before": beforeValue,
			"assignment_after": afterAssignment, "state_before": beforeState,
			"state_after": afterState,
		},
	})
	if err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if before == nil {
		_, err = tx.ExecContext(ctx, `
INSERT INTO city_facility_staff_assignments
    (world_id, code, facility_id, role_code, subject_kind, subject_code,
     entity_id, actor_id, assigned_units, qualification_milli,
     effective_units, status, created_tick, updated_tick, version,
     source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $13, 1, $14, $15::jsonb)`, worldID, payload.Code,
			ref.facilityID, payload.RoleCode, payload.SubjectKind,
			payload.SubjectCode, optionalInt64Value(entityID), optionalInt64Value(actorID),
			payload.AssignedUnits, qualification, effectiveUnits, payload.Status,
			targetTick, fact.id, payload.Metadata)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `
UPDATE city_facility_staff_assignments
SET assigned_units = $3, qualification_milli = $4, effective_units = $5,
    status = $6, updated_tick = $7, version = version + 1,
    source_fact_id = $8, metadata = $9::jsonb, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND version = $10`, worldID, before.id,
			payload.AssignedUnits, qualification, effectiveUnits, payload.Status,
			targetTick, fact.id, payload.Metadata, before.value.Version)
		if err == nil {
			err = requireCityFacilityLifecycleRow(result, payload.Code)
		}
	}
	if err != nil {
		return cityFacilityLifecycleExecution{}, fmt.Errorf("configure city facility staffing: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_facility_lifecycle_states
SET staff_assigned_units = $3, staffing_factor_milli = $4,
    effective_factor_milli = $5, updated_tick = $6,
    version = version + 1, source_fact_id = $7,
    metadata = $8::jsonb, updated_at = NOW()
WHERE world_id = $1 AND facility_id = $2 AND version = $9`,
		worldID, ref.facilityID, assigned, afterState.StaffingFactorMilli,
		afterState.EffectiveFactorMilli, targetTick, fact.id,
		afterState.Metadata, beforeState.Version)
	if err != nil {
		return cityFacilityLifecycleExecution{}, fmt.Errorf("update city facility staffing factor: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, payload.FacilityCode); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	staffingDelta := int64(0)
	if before == nil {
		staffingDelta = 1
	}
	if err = advanceCityFacilityLifecycleProfile(ctx, tx, worldID,
		cityFacilityLifecycleProfileDeltas{staffing: staffingDelta}); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	if err = postCityFacilityLifecycleFact(ctx, tx, fact.id); err != nil {
		return cityFacilityLifecycleExecution{}, err
	}
	execution := appliedCityFacilityLifecycleExecution(command, fact,
		"city.facility.staffing.configured", map[string]any{
			"assignment": afterAssignment, "facility_state": afterState,
		})
	return execution, nil
}
