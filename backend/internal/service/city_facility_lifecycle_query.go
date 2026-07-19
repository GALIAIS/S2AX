package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const (
	cityFacilityLifecycleQueryDefaultLimit = 50
	cityFacilityLifecycleQueryMaximumLimit = 200
)

type CityFacilityLifecycleQueryInput struct {
	UserID           int64
	WorldID          int64
	FacilityCode     string
	FacilityTypeCode string
	LifecycleStatus  string
	OperationType    string
	OperationStatus  string
	StaffingStatus   string
	IncidentStatus   string
	BudgetCode       string
	AfterCode        string
	AfterTick        int64
	AfterSequence    int64
	Limit            int
}

type CityFacilityLifecycleOverview struct {
	FacilityCount            int64 `json:"facility_count"`
	UncommissionedCount      int64 `json:"uncommissioned_count"`
	OperationalCount         int64 `json:"operational_count"`
	MaintenanceCount         int64 `json:"maintenance_count"`
	FailedCount              int64 `json:"failed_count"`
	DecommissioningCount     int64 `json:"decommissioning_count"`
	RetiredCount             int64 `json:"retired_count"`
	AverageConditionMilli    int   `json:"average_condition_milli"`
	AverageStaffingMilli     int   `json:"average_staffing_milli"`
	AverageEffectiveMilli    int   `json:"average_effective_milli"`
	MaintenanceOverdueCount  int64 `json:"maintenance_overdue_count"`
	OpenOperationCount       int64 `json:"open_operation_count"`
	OpenIncidentCount        int64 `json:"open_incident_count"`
	CommittedBudgetUnits     int64 `json:"committed_budget_units,string"`
	SpentBudgetUnits         int64 `json:"spent_budget_units,string"`
	EffectiveDispatchUnits   int64 `json:"effective_dispatch_units,string"`
	ActiveStaffAssignedUnits int64 `json:"active_staff_assigned_units,string"`
}

type CityFacilityLifecycleCatalogView struct {
	Availability      string                         `json:"availability"`
	SimulationVersion string                         `json:"simulation_version"`
	RequiredVersion   string                         `json:"required_version"`
	Profile           *CityFacilityLifecycleProfile  `json:"profile,omitempty"`
	Overview          *CityFacilityLifecycleOverview `json:"overview,omitempty"`
	Policies          []CityFacilityLifecyclePolicy  `json:"policies"`
}

type CityFacilityLifecycleStatePage struct {
	Availability      string                       `json:"availability"`
	SimulationVersion string                       `json:"simulation_version"`
	RequiredVersion   string                       `json:"required_version"`
	Items             []CityFacilityLifecycleState `json:"items"`
	NextCode          *string                      `json:"next_code,omitempty"`
}

type CityFacilityOperationPage struct {
	Availability      string                  `json:"availability"`
	SimulationVersion string                  `json:"simulation_version"`
	RequiredVersion   string                  `json:"required_version"`
	Items             []CityFacilityOperation `json:"items"`
	NextCode          *string                 `json:"next_code,omitempty"`
}

type CityFacilityStaffAssignmentPage struct {
	Availability      string                        `json:"availability"`
	SimulationVersion string                        `json:"simulation_version"`
	RequiredVersion   string                        `json:"required_version"`
	Items             []CityFacilityStaffAssignment `json:"items"`
	NextCode          *string                       `json:"next_code,omitempty"`
}

type CityFacilityIncidentPage struct {
	Availability      string                 `json:"availability"`
	SimulationVersion string                 `json:"simulation_version"`
	RequiredVersion   string                 `json:"required_version"`
	Items             []CityFacilityIncident `json:"items"`
	NextCode          *string                `json:"next_code,omitempty"`
}

type CityFacilityBudgetMovementCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type CityFacilityBudgetMovementPage struct {
	Availability      string                            `json:"availability"`
	SimulationVersion string                            `json:"simulation_version"`
	RequiredVersion   string                            `json:"required_version"`
	Items             []CityFacilityBudgetMovement      `json:"items"`
	NextCursor        *CityFacilityBudgetMovementCursor `json:"next_cursor,omitempty"`
}

type CityFacilityLifecycleFactPage struct {
	Availability      string                            `json:"availability"`
	SimulationVersion string                            `json:"simulation_version"`
	RequiredVersion   string                            `json:"required_version"`
	Items             []CityFacilityLifecycleFact       `json:"items"`
	NextCursor        *CityFacilityBudgetMovementCursor `json:"next_cursor,omitempty"`
}

func (s *CityEconomyService) GetCityFacilityLifecycleCatalog(
	ctx context.Context, input CityFacilityLifecycleQueryInput,
) (*CityFacilityLifecycleCatalogView, error) {
	version, available, err := s.cityFacilityLifecycleQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	view := &CityFacilityLifecycleCatalogView{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion: CitySimulationVersionF8V2,
		Policies:        make([]CityFacilityLifecyclePolicy, 0),
	}
	if !available {
		return view, nil
	}
	state := &CityFacilityLifecycleStateSet{Policies: make([]CityFacilityLifecyclePolicy, 0)}
	if err = loadCityFacilityLifecycleProfile(ctx, s.db, input.WorldID, &state.Profile); err != nil {
		return nil, err
	}
	if err = loadCityFacilityLifecyclePolicies(ctx, s.db, input.WorldID, state); err != nil {
		return nil, err
	}
	overview, err := loadCityFacilityLifecycleOverview(ctx, s.db, input.WorldID)
	if err != nil {
		return nil, err
	}
	view.Availability = CityServiceAvailabilityAvailable
	view.Profile = &state.Profile
	view.Overview = overview
	view.Policies = state.Policies
	return view, nil
}

func (s *CityEconomyService) ListCityFacilityLifecycleStates(
	ctx context.Context, input CityFacilityLifecycleQueryInput,
) (*CityFacilityLifecycleStatePage, error) {
	if err := normalizeCityFacilityLifecycleQuery(&input, "state"); err != nil {
		return nil, err
	}
	version, available, err := s.cityFacilityLifecycleQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityFacilityLifecycleStatePage{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion: CitySimulationVersionF8V2,
		Items:           make([]CityFacilityLifecycleState, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
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
WHERE state.world_id = $1
  AND ($2 = '' OR facility.code = $2)
  AND ($3 = '' OR type.code = $3)
  AND ($4 = '' OR state.lifecycle_status = $4)
  AND ($5 = '' OR facility.code > $5)
ORDER BY facility.code
LIMIT $6`, input.WorldID, input.FacilityCode, input.FacilityTypeCode,
		input.LifecycleStatus, input.AfterCode, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list facility lifecycle states: %w", err)
	}
	for rows.Next() {
		var item CityFacilityLifecycleState
		var lastMaintenance, sourceTick, sourceSequence sql.NullInt64
		var activeOperation, openIncident sql.NullString
		if err = rows.Scan(&item.FacilityCode, &item.FacilityTypeCode,
			&item.LifecycleStatus, &item.ConditionMilli, &item.StaffRequiredUnits,
			&item.StaffAssignedUnits, &item.StaffingFactorMilli,
			&item.OperationFactorMilli, &item.EffectiveFactorMilli,
			&lastMaintenance, &item.MaintenanceDueTick, &activeOperation,
			&openIncident, &item.FailureCount, &item.UpdatedTick, &item.Version,
			&sourceTick, &sourceSequence, &item.Metadata); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan facility lifecycle state: %w", err)
		}
		item.LastMaintenanceTick = nullInt64Pointer(lastMaintenance)
		item.ActiveOperationCode = nullStringPointer(activeOperation)
		item.OpenIncidentCode = nullStringPointer(openIncident)
		item.SourceFactTick = nullInt64Pointer(sourceTick)
		item.SourceFactSequence = nullInt64Pointer(sourceSequence)
		page.Items = append(page.Items, item)
	}
	if err = closeCityRows(rows, "iterate facility lifecycle states"); err != nil {
		return nil, err
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		page.NextCode = stringPointer(page.Items[len(page.Items)-1].FacilityCode)
	}
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) ListCityFacilityOperations(
	ctx context.Context, input CityFacilityLifecycleQueryInput,
) (*CityFacilityOperationPage, error) {
	if err := normalizeCityFacilityLifecycleQuery(&input, "operation"); err != nil {
		return nil, err
	}
	version, available, err := s.cityFacilityLifecycleQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityFacilityOperationPage{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion: CitySimulationVersionF8V2, Items: make([]CityFacilityOperation, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
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
WHERE operation.world_id = $1
  AND ($2 = '' OR facility.code = $2)
  AND ($3 = '' OR operation.operation_type = $3)
  AND ($4 = '' OR operation.status = $4)
  AND ($5 = '' OR operation.code > $5)
ORDER BY operation.code
LIMIT $6`, input.WorldID, input.FacilityCode, input.OperationType,
		input.OperationStatus, input.AfterCode, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list facility operations: %w", err)
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
			return nil, fmt.Errorf("scan facility operation: %w", err)
		}
		item.BudgetCode = nullStringPointer(budget)
		item.StartedTick = nullInt64Pointer(started)
		item.CompletedTick = nullInt64Pointer(completed)
		page.Items = append(page.Items, item)
	}
	if err = closeCityRows(rows, "iterate facility operations"); err != nil {
		return nil, err
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		page.NextCode = stringPointer(page.Items[len(page.Items)-1].Code)
	}
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) ListCityFacilityStaffAssignments(
	ctx context.Context, input CityFacilityLifecycleQueryInput,
) (*CityFacilityStaffAssignmentPage, error) {
	if err := normalizeCityFacilityLifecycleQuery(&input, "staffing"); err != nil {
		return nil, err
	}
	version, available, err := s.cityFacilityLifecycleQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityFacilityStaffAssignmentPage{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion: CitySimulationVersionF8V2,
		Items:           make([]CityFacilityStaffAssignment, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT assignment.code, facility.code, assignment.role_code,
       assignment.subject_kind, assignment.subject_code, assignment.assigned_units,
       assignment.qualification_milli, assignment.effective_units, assignment.status,
       assignment.created_tick, assignment.updated_tick, assignment.version,
       fact.tick, fact.sequence, assignment.metadata
FROM city_facility_staff_assignments assignment
JOIN city_facilities facility ON facility.id = assignment.facility_id
JOIN city_facility_lifecycle_facts fact ON fact.id = assignment.source_fact_id
WHERE assignment.world_id = $1
  AND ($2 = '' OR facility.code = $2)
  AND ($3 = '' OR assignment.status = $3)
  AND ($4 = '' OR assignment.code > $4)
ORDER BY assignment.code
LIMIT $5`, input.WorldID, input.FacilityCode, input.StaffingStatus,
		input.AfterCode, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list facility staffing assignments: %w", err)
	}
	for rows.Next() {
		var item CityFacilityStaffAssignment
		if err = rows.Scan(&item.Code, &item.FacilityCode, &item.RoleCode,
			&item.SubjectKind, &item.SubjectCode, &item.AssignedUnits,
			&item.QualificationMilli, &item.EffectiveUnits, &item.Status,
			&item.CreatedTick, &item.UpdatedTick, &item.Version,
			&item.SourceFactTick, &item.SourceFactSequence, &item.Metadata); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan facility staffing assignment: %w", err)
		}
		page.Items = append(page.Items, item)
	}
	if err = closeCityRows(rows, "iterate facility staffing assignments"); err != nil {
		return nil, err
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		page.NextCode = stringPointer(page.Items[len(page.Items)-1].Code)
	}
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) ListCityFacilityIncidents(
	ctx context.Context, input CityFacilityLifecycleQueryInput,
) (*CityFacilityIncidentPage, error) {
	if err := normalizeCityFacilityLifecycleQuery(&input, "incident"); err != nil {
		return nil, err
	}
	version, available, err := s.cityFacilityLifecycleQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityFacilityIncidentPage{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion: CitySimulationVersionF8V2, Items: make([]CityFacilityIncident, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT incident.code, facility.code, incident.status, incident.severity_milli,
       incident.condition_before_milli, incident.failure_probability_ppm,
       incident.sample_value_ppm, incident.prng_proof, incident.opened_tick,
       incident.resolved_tick, incident.repair_operation_code, incident.version,
       fact.tick, fact.sequence, incident.metadata
FROM city_facility_incidents incident
JOIN city_facilities facility ON facility.id = incident.facility_id
JOIN city_facility_lifecycle_facts fact ON fact.id = incident.source_fact_id
WHERE incident.world_id = $1
  AND ($2 = '' OR facility.code = $2)
  AND ($3 = '' OR incident.status = $3)
  AND ($4 = '' OR incident.code > $4)
ORDER BY incident.code
LIMIT $5`, input.WorldID, input.FacilityCode, input.IncidentStatus,
		input.AfterCode, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list facility incidents: %w", err)
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
			return nil, fmt.Errorf("scan facility incident: %w", err)
		}
		item.ResolvedTick = nullInt64Pointer(resolved)
		item.RepairOperationCode = nullStringPointer(repair)
		page.Items = append(page.Items, item)
	}
	if err = closeCityRows(rows, "iterate facility incidents"); err != nil {
		return nil, err
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		page.NextCode = stringPointer(page.Items[len(page.Items)-1].Code)
	}
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) ListCityFacilityBudgetMovements(
	ctx context.Context, input CityFacilityLifecycleQueryInput,
) (*CityFacilityBudgetMovementPage, error) {
	if err := normalizeCityFacilityLifecycleQuery(&input, "budget"); err != nil {
		return nil, err
	}
	version, available, err := s.cityFacilityLifecycleQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityFacilityBudgetMovementPage{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion: CitySimulationVersionF8V2,
		Items:           make([]CityFacilityBudgetMovement, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, operation.code, budget.code,
       movement.movement_type, movement.amount_units,
       movement.committed_before_units, movement.committed_after_units,
       movement.spent_before_units, movement.spent_after_units,
       movement.budget_version_before, movement.budget_version_after, movement.memo
FROM city_facility_budget_movements movement
JOIN city_facility_lifecycle_facts fact ON fact.id = movement.source_fact_id
JOIN city_facility_operations operation ON operation.id = movement.operation_id
JOIN city_government_budget_lines budget ON budget.id = movement.budget_line_id
JOIN city_facilities facility ON facility.id = operation.facility_id
WHERE movement.world_id = $1
  AND ($2 = '' OR facility.code = $2)
  AND ($3 = '' OR budget.code = $3)
  AND (fact.tick > $4 OR (fact.tick = $4 AND fact.sequence > $5))
ORDER BY fact.tick, fact.sequence
LIMIT $6`, input.WorldID, input.FacilityCode, input.BudgetCode,
		input.AfterTick, input.AfterSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list facility budget movements: %w", err)
	}
	for rows.Next() {
		var item CityFacilityBudgetMovement
		if err = rows.Scan(&item.SourceFactTick, &item.SourceFactSequence,
			&item.OperationCode, &item.BudgetCode, &item.MovementType,
			&item.AmountUnits, &item.CommittedBeforeUnits, &item.CommittedAfterUnits,
			&item.SpentBeforeUnits, &item.SpentAfterUnits,
			&item.BudgetVersionBefore, &item.BudgetVersionAfter, &item.Memo); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan facility budget movement: %w", err)
		}
		page.Items = append(page.Items, item)
	}
	if err = closeCityRows(rows, "iterate facility budget movements"); err != nil {
		return nil, err
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = &CityFacilityBudgetMovementCursor{
			Tick: last.SourceFactTick, Sequence: last.SourceFactSequence,
		}
	}
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) ListCityFacilityLifecycleFacts(
	ctx context.Context, input CityFacilityLifecycleQueryInput,
) (*CityFacilityLifecycleFactPage, error) {
	if err := normalizeCityFacilityLifecycleQuery(&input, "fact"); err != nil {
		return nil, err
	}
	version, available, err := s.cityFacilityLifecycleQueryAvailability(ctx, input)
	if err != nil {
		return nil, err
	}
	page := &CityFacilityLifecycleFactPage{
		Availability: CityServiceAvailabilityUnsupported, SimulationVersion: version,
		RequiredVersion: CitySimulationVersionF8V2,
		Items:           make([]CityFacilityLifecycleFact, 0),
	}
	if !available {
		return page, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, fact.phase, command.sequence, fact.fact_type,
       fact.subject_kind, fact.subject_code, fact.version_before,
       fact.version_after, fact.payload
FROM city_facility_lifecycle_facts fact
LEFT JOIN city_commands command ON command.id = fact.source_command_id
WHERE fact.world_id = $1 AND fact.posted_at IS NOT NULL
  AND ($2 = '' OR fact.subject_code = $2
       OR fact.payload->>'facility_code' = $2
       OR fact.payload#>>'{state_before,facility_code}' = $2
       OR fact.payload#>>'{state_after,facility_code}' = $2
       OR fact.payload#>>'{operation_before,facility_code}' = $2
       OR fact.payload#>>'{operation_after,facility_code}' = $2
       OR fact.payload#>>'{assignment_before,facility_code}' = $2
       OR fact.payload#>>'{assignment_after,facility_code}' = $2
       OR fact.payload#>>'{incident_before,facility_code}' = $2
       OR fact.payload#>>'{incident_after,facility_code}' = $2)
  AND (fact.tick > $3 OR (fact.tick = $3 AND fact.sequence > $4))
ORDER BY fact.tick, fact.sequence
LIMIT $5`, input.WorldID, input.FacilityCode, input.AfterTick,
		input.AfterSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list facility lifecycle facts: %w", err)
	}
	for rows.Next() {
		var item CityFacilityLifecycleFact
		var commandSequence sql.NullInt64
		if err = rows.Scan(&item.Tick, &item.Sequence, &item.Phase, &commandSequence,
			&item.FactType, &item.SubjectKind, &item.SubjectCode,
			&item.VersionBefore, &item.VersionAfter, &item.Payload); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan facility lifecycle fact: %w", err)
		}
		item.SourceCommandSequence = nullInt64Pointer(commandSequence)
		page.Items = append(page.Items, item)
	}
	if err = closeCityRows(rows, "iterate facility lifecycle facts"); err != nil {
		return nil, err
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = &CityFacilityBudgetMovementCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	page.Availability = CityServiceAvailabilityAvailable
	return page, nil
}

func (s *CityEconomyService) cityFacilityLifecycleQueryAvailability(
	ctx context.Context, input CityFacilityLifecycleQueryInput,
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
		return "", false, fmt.Errorf("load facility lifecycle world version: %w", err)
	}
	return version, cityEngineSupportsFacilityLifecycle(version), nil
}

func normalizeCityFacilityLifecycleQuery(
	input *CityFacilityLifecycleQueryInput, kind string,
) error {
	if input == nil || input.UserID <= 0 || input.WorldID <= 0 ||
		input.AfterTick < 0 || input.AfterSequence < 0 ||
		(input.AfterTick == 0 && input.AfterSequence != 0) {
		return ErrCityInvalidInput
	}
	for _, value := range []*string{
		&input.FacilityCode, &input.FacilityTypeCode, &input.LifecycleStatus,
		&input.OperationType, &input.OperationStatus, &input.StaffingStatus,
		&input.IncidentStatus, &input.BudgetCode, &input.AfterCode,
	} {
		*value = strings.ToLower(strings.TrimSpace(*value))
	}
	for _, value := range []string{
		input.FacilityCode, input.FacilityTypeCode, input.BudgetCode, input.AfterCode,
	} {
		if value != "" && !cityServiceCodePattern.MatchString(value) {
			return ErrCityInvalidInput
		}
	}
	if input.LifecycleStatus != "" && !validCityFacilityLifecycleStatus(input.LifecycleStatus) {
		return ErrCityInvalidInput
	}
	if input.OperationType != "" && !isCityFacilityOperationType(input.OperationType) {
		return ErrCityInvalidInput
	}
	if input.OperationStatus != "" && !validCityFacilityOperationStatus(input.OperationStatus) {
		return ErrCityInvalidInput
	}
	if input.StaffingStatus != "" && input.StaffingStatus != "active" && input.StaffingStatus != "released" {
		return ErrCityInvalidInput
	}
	if input.IncidentStatus != "" && input.IncidentStatus != "open" && input.IncidentStatus != "resolved" {
		return ErrCityInvalidInput
	}
	if kind != "fact" && kind != "budget" && (input.AfterTick != 0 || input.AfterSequence != 0) {
		return ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityFacilityLifecycleQueryDefaultLimit
	}
	if input.Limit > cityFacilityLifecycleQueryMaximumLimit {
		return ErrCityInvalidInput
	}
	return nil
}

func loadCityFacilityLifecycleProfile(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
	profile *CityFacilityLifecycleProfile,
) error {
	if profile == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "facility_lifecycle_profile_target"})
	}
	err := queryer.QueryRowContext(ctx, `
SELECT policy_id, policy_version, policy_hash, baseline_tick, policy_count,
       state_count, operation_count, staffing_count, incident_count, fact_count,
       budget_movement_count, revision, metadata
FROM city_facility_lifecycle_profiles WHERE world_id = $1`, worldID).Scan(
		&profile.PolicyID, &profile.PolicyVersion, &profile.PolicyHash,
		&profile.BaselineTick, &profile.PolicyCount, &profile.StateCount,
		&profile.OperationCount, &profile.StaffingCount, &profile.IncidentCount,
		&profile.FactCount, &profile.BudgetMovementCount, &profile.Revision,
		&profile.Metadata,
	)
	if err != nil {
		return fmt.Errorf("load facility lifecycle profile: %w", err)
	}
	return nil
}

func loadCityFacilityLifecycleOverview(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
) (*CityFacilityLifecycleOverview, error) {
	item := &CityFacilityLifecycleOverview{}
	err := queryer.QueryRowContext(ctx, `
WITH world AS (
    SELECT current_tick FROM city_worlds WHERE id = $1
), lifecycle AS (
    SELECT COUNT(*)::BIGINT AS facility_count,
           COUNT(*) FILTER (WHERE lifecycle_status = 'uncommissioned')::BIGINT AS uncommissioned_count,
           COUNT(*) FILTER (WHERE lifecycle_status = 'operational')::BIGINT AS operational_count,
           COUNT(*) FILTER (WHERE lifecycle_status = 'maintenance')::BIGINT AS maintenance_count,
           COUNT(*) FILTER (WHERE lifecycle_status = 'failed')::BIGINT AS failed_count,
           COUNT(*) FILTER (WHERE lifecycle_status = 'decommissioning')::BIGINT AS decommissioning_count,
           COUNT(*) FILTER (WHERE lifecycle_status = 'retired')::BIGINT AS retired_count,
           COALESCE(FLOOR(AVG(condition_milli)), 0)::INTEGER AS average_condition,
           COALESCE(FLOOR(AVG(staffing_factor_milli)), 0)::INTEGER AS average_staffing,
           COALESCE(FLOOR(AVG(effective_factor_milli)), 0)::INTEGER AS average_effective,
           COUNT(*) FILTER (
               WHERE lifecycle_status = 'operational'
                 AND maintenance_due_tick <= (SELECT current_tick FROM world)
           )::BIGINT AS overdue_count,
           COALESCE(SUM(staff_assigned_units), 0)::BIGINT AS assigned_units
    FROM city_facility_lifecycle_states WHERE world_id = $1
), operations AS (
    SELECT COUNT(*) FILTER (WHERE status IN ('planned', 'active'))::BIGINT AS open_count,
           COALESCE(SUM(budget_committed_units), 0)::BIGINT AS committed_units,
           COALESCE(SUM(budget_spent_units), 0)::BIGINT AS spent_units
    FROM city_facility_operations WHERE world_id = $1
), incidents AS (
    SELECT COUNT(*) FILTER (WHERE status = 'open')::BIGINT AS open_count
    FROM city_facility_incidents WHERE world_id = $1
), capacity AS (
    SELECT COALESCE(SUM(
        CASE WHEN facility.status IN ('operational', 'degraded')
             THEN FLOOR(capacity.available_capacity_units::NUMERIC * lifecycle.effective_factor_milli / 1000)::BIGINT
             ELSE 0 END
    ), 0)::BIGINT AS dispatch_units
    FROM city_facility_service_capacities capacity
    JOIN city_facilities facility ON facility.id = capacity.facility_id
    JOIN city_facility_lifecycle_states lifecycle
      ON lifecycle.world_id = facility.world_id AND lifecycle.facility_id = facility.id
    WHERE capacity.world_id = $1
)
SELECT lifecycle.facility_count, lifecycle.uncommissioned_count,
       lifecycle.operational_count, lifecycle.maintenance_count,
       lifecycle.failed_count, lifecycle.decommissioning_count,
       lifecycle.retired_count, lifecycle.average_condition,
       lifecycle.average_staffing, lifecycle.average_effective,
       lifecycle.overdue_count, operations.open_count, incidents.open_count,
       operations.committed_units, operations.spent_units,
       capacity.dispatch_units, lifecycle.assigned_units
FROM lifecycle CROSS JOIN operations CROSS JOIN incidents CROSS JOIN capacity`, worldID).Scan(
		&item.FacilityCount, &item.UncommissionedCount, &item.OperationalCount,
		&item.MaintenanceCount, &item.FailedCount, &item.DecommissioningCount,
		&item.RetiredCount, &item.AverageConditionMilli,
		&item.AverageStaffingMilli, &item.AverageEffectiveMilli,
		&item.MaintenanceOverdueCount, &item.OpenOperationCount,
		&item.OpenIncidentCount, &item.CommittedBudgetUnits,
		&item.SpentBudgetUnits, &item.EffectiveDispatchUnits,
		&item.ActiveStaffAssignedUnits,
	)
	if err != nil {
		return nil, fmt.Errorf("load facility lifecycle overview: %w", err)
	}
	return item, nil
}
