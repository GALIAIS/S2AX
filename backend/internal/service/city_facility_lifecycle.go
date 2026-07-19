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
)

const (
	CitySimulationVersionF8V2 = "city-f8-v2"

	CityFacilityLifecycleStatusUncommissioned  = "uncommissioned"
	CityFacilityLifecycleStatusOperational     = "operational"
	CityFacilityLifecycleStatusMaintenance     = "maintenance"
	CityFacilityLifecycleStatusFailed          = "failed"
	CityFacilityLifecycleStatusDecommissioning = "decommissioning"
	CityFacilityLifecycleStatusRetired         = "retired"

	CityFacilityOperationCommission   = "commission"
	CityFacilityOperationMaintenance  = "maintenance"
	CityFacilityOperationRepair       = "repair"
	CityFacilityOperationDecommission = "decommission"

	CityFacilityOperationStatusPlanned   = "planned"
	CityFacilityOperationStatusActive    = "active"
	CityFacilityOperationStatusCompleted = "completed"
	CityFacilityOperationStatusCancelled = "cancelled"

	CityFacilityLifecycleFactFacilityInitialized = "facility.initialized"
	CityFacilityLifecycleFactCapacityChanged     = "capacity.changed"
	CityFacilityLifecycleFactOperationScheduled  = "operation.scheduled"
	CityFacilityLifecycleFactOperationStarted    = "operation.started"
	CityFacilityLifecycleFactOperationCancelled  = "operation.cancelled"
	CityFacilityLifecycleFactOperationProgressed = "operation.progressed"
	CityFacilityLifecycleFactOperationCompleted  = "operation.completed"
	CityFacilityLifecycleFactStaffingConfigured  = "staffing.configured"
	CityFacilityLifecycleFactConditionChanged    = "condition.changed"
	CityFacilityLifecycleFactIncidentOpened      = "incident.opened"
	CityFacilityLifecycleFactIncidentResolved    = "incident.resolved"

	CityFacilityLifecyclePhaseCommand     = "command"
	CityFacilityLifecyclePhasePreService  = "pre_service"
	CityFacilityLifecyclePhasePostService = "post_service"

	cityFacilityLifecyclePolicyID      = "sub2api-facility-lifecycle"
	cityFacilityLifecyclePolicyVersion = "1.0.0"
	cityFacilityLifecycleMaximumItems  = 100_000
)

type CityFacilityLifecycleProfile struct {
	PolicyID            string          `json:"policy_id"`
	PolicyVersion       string          `json:"policy_version"`
	PolicyHash          string          `json:"policy_hash"`
	BaselineTick        int64           `json:"baseline_tick"`
	PolicyCount         int64           `json:"policy_count"`
	StateCount          int64           `json:"state_count"`
	OperationCount      int64           `json:"operation_count"`
	StaffingCount       int64           `json:"staffing_count"`
	IncidentCount       int64           `json:"incident_count"`
	FactCount           int64           `json:"fact_count"`
	BudgetMovementCount int64           `json:"budget_movement_count"`
	Revision            int64           `json:"revision"`
	Metadata            json.RawMessage `json:"metadata"`
}

type CityFacilityLifecyclePolicy struct {
	FacilityTypeCode              string                                 `json:"facility_type_code"`
	PolicyVersion                 string                                 `json:"policy_version"`
	PolicyHash                    string                                 `json:"policy_hash"`
	MaintenanceIntervalTicks      int64                                  `json:"maintenance_interval_ticks"`
	BaseDecayMilli                int                                    `json:"base_decay_milli"`
	UtilizationDecayMilli         int                                    `json:"utilization_decay_milli"`
	OverdueDecayMilli             int                                    `json:"overdue_decay_milli"`
	FailureThresholdMilli         int                                    `json:"failure_threshold_milli"`
	BaseFailurePPM                int                                    `json:"base_failure_ppm"`
	ConditionFailurePPM           int                                    `json:"condition_failure_ppm"`
	CapacityUnitsPerStaff         int64                                  `json:"capacity_units_per_staff"`
	MaintenanceRestoreMilli       int                                    `json:"maintenance_restore_milli"`
	RepairRestoreMilli            int                                    `json:"repair_restore_milli"`
	OperationCapacityQuantumUnits int64                                  `json:"operation_capacity_quantum_units"`
	OperationPlans                map[string]CityFacilityOperationPolicy `json:"operation_plans"`
	Payload                       json.RawMessage                        `json:"payload"`
}

type CityFacilityOperationPolicy struct {
	BaseDurationTicks             int64 `json:"base_duration_ticks"`
	CapacityQuantaPerDurationTick int64 `json:"capacity_quanta_per_duration_tick"`
	BasicMaterialUnitsPerQuantum  int64 `json:"basic_material_units_per_quantum"`
	CapitalGoodsUnitsPerQuantum   int64 `json:"capital_goods_units_per_quantum"`
	LaborUnitsPerQuantum          int64 `json:"labor_units_per_quantum"`
	BudgetUnitsPerQuantum         int64 `json:"budget_units_per_quantum"`
}

type CityFacilityLifecycleState struct {
	FacilityCode         string          `json:"facility_code"`
	FacilityTypeCode     string          `json:"facility_type_code"`
	LifecycleStatus      string          `json:"lifecycle_status"`
	ConditionMilli       int             `json:"condition_milli"`
	StaffRequiredUnits   int64           `json:"staff_required_units"`
	StaffAssignedUnits   int64           `json:"staff_assigned_units"`
	StaffingFactorMilli  int             `json:"staffing_factor_milli"`
	OperationFactorMilli int             `json:"operation_factor_milli"`
	EffectiveFactorMilli int             `json:"effective_factor_milli"`
	LastMaintenanceTick  *int64          `json:"last_maintenance_tick,omitempty"`
	MaintenanceDueTick   int64           `json:"maintenance_due_tick"`
	ActiveOperationCode  *string         `json:"active_operation_code,omitempty"`
	OpenIncidentCode     *string         `json:"open_incident_code,omitempty"`
	FailureCount         int64           `json:"failure_count"`
	UpdatedTick          int64           `json:"updated_tick"`
	Version              int64           `json:"version"`
	SourceFactTick       *int64          `json:"source_fact_tick,omitempty"`
	SourceFactSequence   *int64          `json:"source_fact_sequence,omitempty"`
	Metadata             json.RawMessage `json:"metadata"`
}

type CityFacilityOperation struct {
	Code                       string          `json:"code"`
	FacilityCode               string          `json:"facility_code"`
	OperationType              string          `json:"operation_type"`
	Status                     string          `json:"status"`
	SponsorEntityCode          string          `json:"sponsor_entity_code"`
	ExecutorEntityCode         string          `json:"executor_entity_code"`
	BudgetCode                 *string         `json:"budget_code,omitempty"`
	PlannedStartTick           int64           `json:"planned_start_tick"`
	StartedTick                *int64          `json:"started_tick,omitempty"`
	CompletedTick              *int64          `json:"completed_tick,omitempty"`
	DurationTicks              int64           `json:"duration_ticks"`
	ProgressMilli              int             `json:"progress_milli"`
	RequiredBasicMaterialUnits int64           `json:"required_basic_material_units"`
	RequiredCapitalGoodsUnits  int64           `json:"required_capital_goods_units"`
	RequiredLaborUnits         int64           `json:"required_labor_units"`
	BudgetUnits                int64           `json:"budget_units"`
	BudgetCommittedUnits       int64           `json:"budget_committed_units"`
	BudgetSpentUnits           int64           `json:"budget_spent_units"`
	CreatedTick                int64           `json:"created_tick"`
	UpdatedTick                int64           `json:"updated_tick"`
	Version                    int64           `json:"version"`
	SourceFactTick             int64           `json:"source_fact_tick"`
	SourceFactSequence         int64           `json:"source_fact_sequence"`
	Metadata                   json.RawMessage `json:"metadata"`
}

type CityFacilityStaffAssignment struct {
	Code               string          `json:"code"`
	FacilityCode       string          `json:"facility_code"`
	RoleCode           string          `json:"role_code"`
	SubjectKind        string          `json:"subject_kind"`
	SubjectCode        string          `json:"subject_code"`
	AssignedUnits      int64           `json:"assigned_units"`
	QualificationMilli int             `json:"qualification_milli"`
	EffectiveUnits     int64           `json:"effective_units"`
	Status             string          `json:"status"`
	CreatedTick        int64           `json:"created_tick"`
	UpdatedTick        int64           `json:"updated_tick"`
	Version            int64           `json:"version"`
	SourceFactTick     int64           `json:"source_fact_tick"`
	SourceFactSequence int64           `json:"source_fact_sequence"`
	Metadata           json.RawMessage `json:"metadata"`
}

type CityFacilityIncident struct {
	Code                  string          `json:"code"`
	FacilityCode          string          `json:"facility_code"`
	Status                string          `json:"status"`
	SeverityMilli         int             `json:"severity_milli"`
	ConditionBeforeMilli  int             `json:"condition_before_milli"`
	FailureProbabilityPPM int             `json:"failure_probability_ppm"`
	SampleValuePPM        int             `json:"sample_value_ppm"`
	PRNGProof             string          `json:"prng_proof"`
	OpenedTick            int64           `json:"opened_tick"`
	ResolvedTick          *int64          `json:"resolved_tick,omitempty"`
	RepairOperationCode   *string         `json:"repair_operation_code,omitempty"`
	Version               int64           `json:"version"`
	SourceFactTick        int64           `json:"source_fact_tick"`
	SourceFactSequence    int64           `json:"source_fact_sequence"`
	Metadata              json.RawMessage `json:"metadata"`
}

type CityFacilityBudgetMovement struct {
	SourceFactTick       int64  `json:"source_fact_tick"`
	SourceFactSequence   int64  `json:"source_fact_sequence"`
	OperationCode        string `json:"operation_code"`
	BudgetCode           string `json:"budget_code"`
	MovementType         string `json:"movement_type"`
	AmountUnits          int64  `json:"amount_units"`
	CommittedBeforeUnits int64  `json:"committed_before_units"`
	CommittedAfterUnits  int64  `json:"committed_after_units"`
	SpentBeforeUnits     int64  `json:"spent_before_units"`
	SpentAfterUnits      int64  `json:"spent_after_units"`
	BudgetVersionBefore  int64  `json:"budget_version_before"`
	BudgetVersionAfter   int64  `json:"budget_version_after"`
	Memo                 string `json:"memo"`
}

type CityFacilityLifecycleFact struct {
	Tick                  int64           `json:"tick"`
	Sequence              int64           `json:"sequence"`
	Phase                 string          `json:"phase"`
	SourceCommandSequence *int64          `json:"source_command_sequence,omitempty"`
	FactType              string          `json:"fact_type"`
	SubjectKind           string          `json:"subject_kind"`
	SubjectCode           string          `json:"subject_code"`
	VersionBefore         int64           `json:"version_before"`
	VersionAfter          int64           `json:"version_after"`
	Payload               json.RawMessage `json:"payload"`
}

type CityFacilityLifecycleStateSet struct {
	Profile          CityFacilityLifecycleProfile  `json:"profile"`
	Policies         []CityFacilityLifecyclePolicy `json:"policies"`
	States           []CityFacilityLifecycleState  `json:"states"`
	Operations       []CityFacilityOperation       `json:"operations"`
	StaffAssignments []CityFacilityStaffAssignment `json:"staff_assignments"`
	Incidents        []CityFacilityIncident        `json:"incidents"`
	BudgetMovements  []CityFacilityBudgetMovement  `json:"budget_movements"`
	Facts            []CityFacilityLifecycleFact   `json:"facts"`
}

type cityFacilityLifecycleHashState = CityFacilityLifecycleStateSet

type cityFacilityLifecyclePolicySeed struct {
	FacilityTypeCode              string                                 `json:"facility_type_code"`
	MaintenanceIntervalTicks      int64                                  `json:"maintenance_interval_ticks"`
	BaseDecayMilli                int                                    `json:"base_decay_milli"`
	UtilizationDecayMilli         int                                    `json:"utilization_decay_milli"`
	OverdueDecayMilli             int                                    `json:"overdue_decay_milli"`
	FailureThresholdMilli         int                                    `json:"failure_threshold_milli"`
	BaseFailurePPM                int                                    `json:"base_failure_ppm"`
	ConditionFailurePPM           int                                    `json:"condition_failure_ppm"`
	CapacityUnitsPerStaff         int64                                  `json:"capacity_units_per_staff"`
	MaintenanceRestoreMilli       int                                    `json:"maintenance_restore_milli"`
	RepairRestoreMilli            int                                    `json:"repair_restore_milli"`
	OperationCapacityQuantumUnits int64                                  `json:"operation_capacity_quantum_units"`
	OperationPlans                map[string]CityFacilityOperationPolicy `json:"operation_plans"`
	MaximumOverdueTicks           int64                                  `json:"maximum_overdue_ticks"`
	SchemaVersion                 int                                    `json:"schema_version"`
}

func cityFacilityLifecyclePolicyCatalog(facilityTypeCodes []string) ([]CityFacilityLifecyclePolicy, string, error) {
	codes := append([]string(nil), facilityTypeCodes...)
	sort.Strings(codes)
	policies := make([]CityFacilityLifecyclePolicy, 0, len(codes))
	for _, code := range codes {
		seed := cityFacilityLifecyclePolicySeed{
			FacilityTypeCode: code, MaintenanceIntervalTicks: 90,
			BaseDecayMilli: 1, UtilizationDecayMilli: 4, OverdueDecayMilli: 2,
			FailureThresholdMilli: 350, BaseFailurePPM: 200, ConditionFailurePPM: 20,
			CapacityUnitsPerStaff: 1000, MaintenanceRestoreMilli: 1000,
			RepairRestoreMilli: 850, OperationCapacityQuantumUnits: 1000,
			MaximumOverdueTicks: 100,
			OperationPlans: map[string]CityFacilityOperationPolicy{
				CityFacilityOperationCommission: {
					BaseDurationTicks: 2, CapacityQuantaPerDurationTick: 10,
					BasicMaterialUnitsPerQuantum: 8, CapitalGoodsUnitsPerQuantum: 5,
					LaborUnitsPerQuantum: 2, BudgetUnitsPerQuantum: 100,
				},
				CityFacilityOperationMaintenance: {
					BaseDurationTicks: 1, CapacityQuantaPerDurationTick: 20,
					BasicMaterialUnitsPerQuantum: 2, CapitalGoodsUnitsPerQuantum: 1,
					LaborUnitsPerQuantum: 1, BudgetUnitsPerQuantum: 20,
				},
				CityFacilityOperationRepair: {
					BaseDurationTicks: 2, CapacityQuantaPerDurationTick: 12,
					BasicMaterialUnitsPerQuantum: 5, CapitalGoodsUnitsPerQuantum: 3,
					LaborUnitsPerQuantum: 2, BudgetUnitsPerQuantum: 60,
				},
				CityFacilityOperationDecommission: {
					BaseDurationTicks: 2, CapacityQuantaPerDurationTick: 16,
					LaborUnitsPerQuantum: 1, BudgetUnitsPerQuantum: 30,
				},
			},
			SchemaVersion: 1,
		}
		payload, err := json.Marshal(seed)
		if err != nil {
			return nil, "", fmt.Errorf("marshal facility lifecycle policy %s: %w", code, err)
		}
		sum := sha256.Sum256(payload)
		policies = append(policies, CityFacilityLifecyclePolicy{
			FacilityTypeCode: code, PolicyVersion: cityFacilityLifecyclePolicyVersion,
			PolicyHash:               hex.EncodeToString(sum[:]),
			MaintenanceIntervalTicks: seed.MaintenanceIntervalTicks,
			BaseDecayMilli:           seed.BaseDecayMilli, UtilizationDecayMilli: seed.UtilizationDecayMilli,
			OverdueDecayMilli: seed.OverdueDecayMilli, FailureThresholdMilli: seed.FailureThresholdMilli,
			BaseFailurePPM: seed.BaseFailurePPM, ConditionFailurePPM: seed.ConditionFailurePPM,
			CapacityUnitsPerStaff:         seed.CapacityUnitsPerStaff,
			MaintenanceRestoreMilli:       seed.MaintenanceRestoreMilli,
			RepairRestoreMilli:            seed.RepairRestoreMilli,
			OperationCapacityQuantumUnits: seed.OperationCapacityQuantumUnits,
			OperationPlans:                seed.OperationPlans, Payload: payload,
		})
	}
	canonical, err := json.Marshal(struct {
		PolicyID      string                        `json:"policy_id"`
		PolicyVersion string                        `json:"policy_version"`
		Policies      []CityFacilityLifecyclePolicy `json:"policies"`
	}{cityFacilityLifecyclePolicyID, cityFacilityLifecyclePolicyVersion, policies})
	if err != nil {
		return nil, "", fmt.Errorf("marshal facility lifecycle catalog: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return policies, hex.EncodeToString(sum[:]), nil
}

func initializeCityFacilityLifecycleFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var sourceVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick FROM city_worlds WHERE id = $1`, worldID).
		Scan(&sourceVersion, &baselineTick); err != nil {
		return fmt.Errorf("load facility lifecycle baseline: %w", err)
	}
	if sourceVersion != CitySimulationVersionF8 &&
		!((sourceVersion == CitySimulationVersionF8V2 || sourceVersion == CitySimulationVersionF8V3) &&
			baselineTick == 0) {
		return fmt.Errorf("facility lifecycle requires F8.0 upgrade or direct F8.1 creation")
	}
	rows, err := tx.QueryContext(ctx, `
SELECT code FROM city_facility_type_definitions WHERE world_id = $1 ORDER BY code`, worldID)
	if err != nil {
		return fmt.Errorf("load facility types for lifecycle: %w", err)
	}
	typeCodes := make([]string, 0)
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			_ = rows.Close()
			return err
		}
		typeCodes = append(typeCodes, code)
	}
	if err = closeCityRows(rows, "iterate facility types for lifecycle"); err != nil {
		return err
	}
	policies, policyHash, err := cityFacilityLifecyclePolicyCatalog(typeCodes)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_facility_lifecycle_bootstrap_world_id', $1, true)`,
		fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable facility lifecycle bootstrap: %w", err)
	}
	metadata := json.RawMessage(`{"schema_version":1}`)
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_facility_lifecycle_profiles
    (world_id, policy_id, policy_version, policy_hash, baseline_tick,
     policy_count, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`, worldID,
		cityFacilityLifecyclePolicyID, cityFacilityLifecyclePolicyVersion,
		policyHash, baselineTick, len(policies), metadata); err != nil {
		return fmt.Errorf("insert facility lifecycle profile: %w", err)
	}
	for _, policy := range policies {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_facility_lifecycle_policies
    (world_id, facility_type_id, policy_version, policy_hash,
     maintenance_interval_ticks, base_decay_milli, utilization_decay_milli,
     overdue_decay_milli, failure_threshold_milli, base_failure_ppm,
     condition_failure_ppm, capacity_units_per_staff,
     maintenance_restore_milli, repair_restore_milli, payload)
SELECT $1, type.id, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb
FROM city_facility_type_definitions type
WHERE type.world_id = $1 AND type.code = $2`, worldID, policy.FacilityTypeCode,
			policy.PolicyVersion, policy.PolicyHash, policy.MaintenanceIntervalTicks,
			policy.BaseDecayMilli, policy.UtilizationDecayMilli, policy.OverdueDecayMilli,
			policy.FailureThresholdMilli, policy.BaseFailurePPM, policy.ConditionFailurePPM,
			policy.CapacityUnitsPerStaff, policy.MaintenanceRestoreMilli,
			policy.RepairRestoreMilli, policy.Payload); err != nil {
			return fmt.Errorf("insert facility lifecycle policy %s: %w", policy.FacilityTypeCode, err)
		}
	}
	legacy := sourceVersion == CitySimulationVersionF8
	result, err := tx.ExecContext(ctx, `
INSERT INTO city_facility_lifecycle_states
    (world_id, facility_id, policy_id, lifecycle_status, condition_milli,
     staff_required_units, staff_assigned_units, staffing_factor_milli,
     operation_factor_milli, effective_factor_milli, maintenance_due_tick,
     failure_count, updated_tick, version, metadata)
SELECT facility.world_id, facility.id, policy.id,
       CASE facility.status
         WHEN 'retired' THEN 'retired'
         WHEN 'operational' THEN 'operational'
         WHEN 'degraded' THEN 'operational'
         ELSE 'uncommissioned' END,
       1000,
       CASE WHEN capacity.installed_units = 0 THEN 0
            ELSE CEIL(capacity.installed_units::NUMERIC / policy.capacity_units_per_staff)::BIGINT END,
       CASE WHEN $2 AND facility.status IN ('operational', 'degraded')
            THEN CASE WHEN capacity.installed_units = 0 THEN 0
                 ELSE CEIL(capacity.installed_units::NUMERIC / policy.capacity_units_per_staff)::BIGINT END
            ELSE 0 END,
       CASE WHEN capacity.installed_units = 0 OR ($2 AND facility.status IN ('operational', 'degraded'))
            THEN 1000 ELSE 0 END,
       1000,
       CASE WHEN facility.status IN ('operational', 'degraded') THEN 1000 ELSE 0 END,
       CASE WHEN facility.status = 'retired' THEN 0
            ELSE $3 + policy.maintenance_interval_ticks END,
       0, $3, 1,
       jsonb_build_object('schema_version', 1, 'staffing_source',
            CASE WHEN $2 THEN 'legacy_baseline' ELSE 'none' END)
FROM city_facilities facility
JOIN city_facility_lifecycle_policies policy
  ON policy.world_id = facility.world_id AND policy.facility_type_id = facility.facility_type_id
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(capacity.installed_capacity_units), 0)::BIGINT AS installed_units
    FROM city_facility_service_capacities capacity
    WHERE capacity.world_id = facility.world_id AND capacity.facility_id = facility.id
) capacity ON TRUE
WHERE facility.world_id = $1`, worldID, legacy, baselineTick)
	if err != nil {
		return fmt.Errorf("initialize facility lifecycle states: %w", err)
	}
	stateCount, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count initialized lifecycle states: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_facility_lifecycle_profiles
SET state_count = $2, updated_at = NOW()
WHERE world_id = $1`, worldID, stateCount); err != nil {
		return fmt.Errorf("finalize lifecycle profile: %w", err)
	}
	return nil
}

func cityFacilityEffectiveDispatchCapacity(status string, available int64, lifecycleFactor int) int64 {
	base := cityServiceDispatchCapacity(status, available)
	if base <= 0 || lifecycleFactor <= 0 {
		return 0
	}
	if lifecycleFactor >= 1000 {
		return base
	}
	result, err := cityMulDivFloor(base, lifecycleFactor, 1000)
	if err != nil {
		return 0
	}
	return result
}

func loadCityFacilityLifecycleHashState(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
) (*cityFacilityLifecycleHashState, error) {
	state := &CityFacilityLifecycleStateSet{
		Policies:         make([]CityFacilityLifecyclePolicy, 0),
		States:           make([]CityFacilityLifecycleState, 0),
		Operations:       make([]CityFacilityOperation, 0),
		StaffAssignments: make([]CityFacilityStaffAssignment, 0),
		Incidents:        make([]CityFacilityIncident, 0),
		BudgetMovements:  make([]CityFacilityBudgetMovement, 0),
		Facts:            make([]CityFacilityLifecycleFact, 0),
	}
	err := queryer.QueryRowContext(ctx, `
SELECT policy_id, policy_version, policy_hash, baseline_tick, policy_count,
       state_count, operation_count, staffing_count, incident_count, fact_count,
       budget_movement_count, revision, metadata
FROM city_facility_lifecycle_profiles WHERE world_id = $1`, worldID).Scan(
		&state.Profile.PolicyID, &state.Profile.PolicyVersion, &state.Profile.PolicyHash,
		&state.Profile.BaselineTick, &state.Profile.PolicyCount, &state.Profile.StateCount,
		&state.Profile.OperationCount, &state.Profile.StaffingCount,
		&state.Profile.IncidentCount, &state.Profile.FactCount,
		&state.Profile.BudgetMovementCount, &state.Profile.Revision,
		&state.Profile.Metadata)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("city facility lifecycle state not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load facility lifecycle profile: %w", err)
	}
	if err = loadCityFacilityLifecyclePolicies(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityFacilityLifecycleStates(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityFacilityOperations(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityFacilityStaffAssignments(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityFacilityIncidents(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityFacilityBudgetMovements(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = loadCityFacilityLifecycleFacts(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	return state, nil
}

func loadCityFacilityLifecyclePolicies(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityFacilityLifecycleStateSet) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT type.code, policy.policy_version, policy.policy_hash,
       policy.maintenance_interval_ticks, policy.base_decay_milli,
       policy.utilization_decay_milli, policy.overdue_decay_milli,
       policy.failure_threshold_milli, policy.base_failure_ppm,
       policy.condition_failure_ppm, policy.capacity_units_per_staff,
       policy.maintenance_restore_milli, policy.repair_restore_milli, policy.payload
FROM city_facility_lifecycle_policies policy
JOIN city_facility_type_definitions type ON type.id = policy.facility_type_id
WHERE policy.world_id = $1 ORDER BY type.code`, worldID)
	if err != nil {
		return fmt.Errorf("load facility lifecycle policies: %w", err)
	}
	for rows.Next() {
		var item CityFacilityLifecyclePolicy
		if err = rows.Scan(&item.FacilityTypeCode, &item.PolicyVersion, &item.PolicyHash,
			&item.MaintenanceIntervalTicks, &item.BaseDecayMilli,
			&item.UtilizationDecayMilli, &item.OverdueDecayMilli,
			&item.FailureThresholdMilli, &item.BaseFailurePPM,
			&item.ConditionFailurePPM, &item.CapacityUnitsPerStaff,
			&item.MaintenanceRestoreMilli, &item.RepairRestoreMilli, &item.Payload); err != nil {
			_ = rows.Close()
			return err
		}
		var payload cityFacilityLifecyclePolicySeed
		if err = json.Unmarshal(item.Payload, &payload); err != nil ||
			payload.OperationCapacityQuantumUnits <= 0 || len(payload.OperationPlans) != 4 {
			_ = rows.Close()
			return fmt.Errorf("decode facility lifecycle policy %s", item.FacilityTypeCode)
		}
		item.OperationCapacityQuantumUnits = payload.OperationCapacityQuantumUnits
		item.OperationPlans = payload.OperationPlans
		state.Policies = append(state.Policies, item)
	}
	return closeCityRows(rows, "iterate facility lifecycle policies")
}

func loadCityFacilityLifecycleStates(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityFacilityLifecycleStateSet) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT facility.code, type.code, state.lifecycle_status, state.condition_milli,
       state.staff_required_units, state.staff_assigned_units,
       state.staffing_factor_milli, state.operation_factor_milli,
       state.effective_factor_milli, state.last_maintenance_tick,
       state.maintenance_due_tick, state.active_operation_code,
       state.open_incident_code, state.failure_count, state.updated_tick,
       state.version, fact.tick, fact.sequence, state.metadata
FROM city_facility_lifecycle_states state
JOIN city_facilities facility ON facility.id = state.facility_id
JOIN city_facility_type_definitions type ON type.id = facility.facility_type_id
LEFT JOIN city_facility_lifecycle_facts fact ON fact.id = state.source_fact_id
WHERE state.world_id = $1 ORDER BY facility.code`, worldID)
	if err != nil {
		return fmt.Errorf("load facility lifecycle states: %w", err)
	}
	for rows.Next() {
		var item CityFacilityLifecycleState
		var lastMaintenance, sourceTick, sourceSeq sql.NullInt64
		var activeOperation, openIncident sql.NullString
		if err = rows.Scan(&item.FacilityCode, &item.FacilityTypeCode,
			&item.LifecycleStatus, &item.ConditionMilli, &item.StaffRequiredUnits,
			&item.StaffAssignedUnits, &item.StaffingFactorMilli,
			&item.OperationFactorMilli, &item.EffectiveFactorMilli,
			&lastMaintenance, &item.MaintenanceDueTick, &activeOperation,
			&openIncident, &item.FailureCount, &item.UpdatedTick, &item.Version,
			&sourceTick, &sourceSeq, &item.Metadata); err != nil {
			_ = rows.Close()
			return err
		}
		item.LastMaintenanceTick = nullInt64Pointer(lastMaintenance)
		item.ActiveOperationCode = nullStringPointer(activeOperation)
		item.OpenIncidentCode = nullStringPointer(openIncident)
		item.SourceFactTick = nullInt64Pointer(sourceTick)
		item.SourceFactSequence = nullInt64Pointer(sourceSeq)
		state.States = append(state.States, item)
	}
	return closeCityRows(rows, "iterate facility lifecycle states")
}

func loadCityFacilityOperations(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityFacilityLifecycleStateSet) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT operation.code, facility.code, operation.operation_type, operation.status,
       sponsor.code, executor.code, budget.code, operation.planned_start_tick,
       operation.started_tick, operation.completed_tick, operation.duration_ticks,
       operation.progress_milli, operation.required_basic_material_units,
       operation.required_capital_goods_units, operation.required_labor_units,
       operation.budget_units, operation.budget_committed_units,
       operation.budget_spent_units, operation.created_tick, operation.updated_tick,
       operation.version, fact.tick, fact.sequence, operation.metadata
FROM city_facility_operations operation
JOIN city_facilities facility ON facility.id = operation.facility_id
JOIN city_economic_entities sponsor ON sponsor.id = operation.sponsor_entity_id
JOIN city_economic_entities executor ON executor.id = operation.executor_entity_id
LEFT JOIN city_government_budget_lines budget ON budget.id = operation.budget_line_id
JOIN city_facility_lifecycle_facts fact ON fact.id = operation.source_fact_id
WHERE operation.world_id = $1 ORDER BY operation.code`, worldID)
	if err != nil {
		return fmt.Errorf("load facility operations: %w", err)
	}
	for rows.Next() {
		var item CityFacilityOperation
		var budget sql.NullString
		var started, completed sql.NullInt64
		if err = rows.Scan(&item.Code, &item.FacilityCode, &item.OperationType,
			&item.Status, &item.SponsorEntityCode, &item.ExecutorEntityCode, &budget,
			&item.PlannedStartTick, &started, &completed, &item.DurationTicks,
			&item.ProgressMilli, &item.RequiredBasicMaterialUnits,
			&item.RequiredCapitalGoodsUnits, &item.RequiredLaborUnits,
			&item.BudgetUnits, &item.BudgetCommittedUnits, &item.BudgetSpentUnits,
			&item.CreatedTick, &item.UpdatedTick, &item.Version,
			&item.SourceFactTick, &item.SourceFactSequence, &item.Metadata); err != nil {
			_ = rows.Close()
			return err
		}
		item.BudgetCode = nullStringPointer(budget)
		item.StartedTick = nullInt64Pointer(started)
		item.CompletedTick = nullInt64Pointer(completed)
		state.Operations = append(state.Operations, item)
	}
	return closeCityRows(rows, "iterate facility operations")
}

func loadCityFacilityStaffAssignments(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityFacilityLifecycleStateSet) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT assignment.code, facility.code, assignment.role_code,
       assignment.subject_kind, assignment.subject_code, assignment.assigned_units,
       assignment.qualification_milli, assignment.effective_units, assignment.status,
       assignment.created_tick, assignment.updated_tick, assignment.version,
       fact.tick, fact.sequence, assignment.metadata
FROM city_facility_staff_assignments assignment
JOIN city_facilities facility ON facility.id = assignment.facility_id
JOIN city_facility_lifecycle_facts fact ON fact.id = assignment.source_fact_id
WHERE assignment.world_id = $1 ORDER BY assignment.code`, worldID)
	if err != nil {
		return fmt.Errorf("load facility staffing: %w", err)
	}
	for rows.Next() {
		var item CityFacilityStaffAssignment
		if err = rows.Scan(&item.Code, &item.FacilityCode, &item.RoleCode,
			&item.SubjectKind, &item.SubjectCode, &item.AssignedUnits,
			&item.QualificationMilli, &item.EffectiveUnits, &item.Status,
			&item.CreatedTick, &item.UpdatedTick, &item.Version,
			&item.SourceFactTick, &item.SourceFactSequence, &item.Metadata); err != nil {
			_ = rows.Close()
			return err
		}
		state.StaffAssignments = append(state.StaffAssignments, item)
	}
	return closeCityRows(rows, "iterate facility staffing")
}

func loadCityFacilityIncidents(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityFacilityLifecycleStateSet) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT incident.code, facility.code, incident.status, incident.severity_milli,
       incident.condition_before_milli, incident.failure_probability_ppm,
       incident.sample_value_ppm, incident.prng_proof, incident.opened_tick,
       incident.resolved_tick, incident.repair_operation_code, incident.version,
       fact.tick, fact.sequence, incident.metadata
FROM city_facility_incidents incident
JOIN city_facilities facility ON facility.id = incident.facility_id
JOIN city_facility_lifecycle_facts fact ON fact.id = incident.source_fact_id
WHERE incident.world_id = $1 ORDER BY incident.code`, worldID)
	if err != nil {
		return fmt.Errorf("load facility incidents: %w", err)
	}
	for rows.Next() {
		var item CityFacilityIncident
		var resolved sql.NullInt64
		var repair sql.NullString
		if err = rows.Scan(&item.Code, &item.FacilityCode, &item.Status,
			&item.SeverityMilli, &item.ConditionBeforeMilli,
			&item.FailureProbabilityPPM, &item.SampleValuePPM, &item.PRNGProof,
			&item.OpenedTick, &resolved, &repair, &item.Version,
			&item.SourceFactTick, &item.SourceFactSequence, &item.Metadata); err != nil {
			_ = rows.Close()
			return err
		}
		item.ResolvedTick = nullInt64Pointer(resolved)
		item.RepairOperationCode = nullStringPointer(repair)
		state.Incidents = append(state.Incidents, item)
	}
	return closeCityRows(rows, "iterate facility incidents")
}

func loadCityFacilityBudgetMovements(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityFacilityLifecycleStateSet) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, operation.code, budget.code,
       movement.movement_type, movement.amount_units,
       movement.committed_before_units, movement.committed_after_units,
       movement.spent_before_units, movement.spent_after_units,
       movement.budget_version_before, movement.budget_version_after, movement.memo
FROM city_facility_budget_movements movement
JOIN city_facility_lifecycle_facts fact ON fact.id = movement.source_fact_id
JOIN city_facility_operations operation ON operation.id = movement.operation_id
JOIN city_government_budget_lines budget ON budget.id = movement.budget_line_id
WHERE movement.world_id = $1 ORDER BY fact.tick, fact.sequence`, worldID)
	if err != nil {
		return fmt.Errorf("load facility budget movements: %w", err)
	}
	for rows.Next() {
		var item CityFacilityBudgetMovement
		if err = rows.Scan(&item.SourceFactTick, &item.SourceFactSequence,
			&item.OperationCode, &item.BudgetCode, &item.MovementType,
			&item.AmountUnits, &item.CommittedBeforeUnits, &item.CommittedAfterUnits,
			&item.SpentBeforeUnits, &item.SpentAfterUnits,
			&item.BudgetVersionBefore, &item.BudgetVersionAfter, &item.Memo); err != nil {
			_ = rows.Close()
			return err
		}
		state.BudgetMovements = append(state.BudgetMovements, item)
	}
	return closeCityRows(rows, "iterate facility budget movements")
}

func loadCityFacilityLifecycleFacts(ctx context.Context, queryer citySQLQueryer, worldID int64, state *CityFacilityLifecycleStateSet) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, fact.phase, command.sequence, fact.fact_type,
       fact.subject_kind, fact.subject_code, fact.version_before,
       fact.version_after, fact.payload
FROM city_facility_lifecycle_facts fact
LEFT JOIN city_commands command ON command.id = fact.source_command_id
WHERE fact.world_id = $1 AND fact.posted_at IS NOT NULL
ORDER BY fact.tick, fact.sequence`, worldID)
	if err != nil {
		return fmt.Errorf("load facility lifecycle facts: %w", err)
	}
	for rows.Next() {
		var item CityFacilityLifecycleFact
		var commandSequence sql.NullInt64
		if err = rows.Scan(&item.Tick, &item.Sequence, &item.Phase, &commandSequence,
			&item.FactType, &item.SubjectKind, &item.SubjectCode,
			&item.VersionBefore, &item.VersionAfter, &item.Payload); err != nil {
			_ = rows.Close()
			return err
		}
		item.SourceCommandSequence = nullInt64Pointer(commandSequence)
		state.Facts = append(state.Facts, item)
	}
	return closeCityRows(rows, "iterate facility lifecycle facts")
}

func loadCityFacilityLifecycleFactsForTick(
	ctx context.Context, queryer citySQLQueryer, worldID, tick int64,
) ([]CityFacilityLifecycleFact, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, fact.phase, command.sequence, fact.fact_type,
       fact.subject_kind, fact.subject_code, fact.version_before,
       fact.version_after, fact.payload
FROM city_facility_lifecycle_facts fact
LEFT JOIN city_commands command ON command.id = fact.source_command_id
WHERE fact.world_id = $1 AND fact.tick = $2 AND fact.posted_at IS NOT NULL
ORDER BY fact.sequence`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load facility lifecycle facts for tick: %w", err)
	}
	items := make([]CityFacilityLifecycleFact, 0)
	for rows.Next() {
		var item CityFacilityLifecycleFact
		var commandSequence sql.NullInt64
		if err = rows.Scan(&item.Tick, &item.Sequence, &item.Phase, &commandSequence,
			&item.FactType, &item.SubjectKind, &item.SubjectCode,
			&item.VersionBefore, &item.VersionAfter, &item.Payload); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan facility lifecycle fact for tick: %w", err)
		}
		item.SourceCommandSequence = nullInt64Pointer(commandSequence)
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate facility lifecycle facts for tick"); err != nil {
		return nil, err
	}
	return items, nil
}
