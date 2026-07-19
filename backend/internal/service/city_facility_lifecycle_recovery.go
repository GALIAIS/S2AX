package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
)

type cityFacilityLifecycleRecoveryFactKey struct {
	tick     int64
	sequence int64
}

type cityFacilityLifecycleRecoveryIdentityMaps struct {
	facilities    map[string]int64
	facilityTypes map[string]string
	entities      map[string]int64
	entityTypes   map[string]string
	budgets       map[string]int64
	actors        map[string]int64
	commands      map[cityFacilityLifecycleRecoveryFactKey]int64
}

func loadCityFacilityLifecycleRecoveryFactIDs(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
) (map[cityFacilityLifecycleRecoveryFactKey]int64, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, tick, sequence
FROM city_facility_lifecycle_facts
WHERE world_id = $1
ORDER BY tick, sequence`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load facility lifecycle recovery fact identities: %w", err)
	}
	items := make(map[cityFacilityLifecycleRecoveryFactKey]int64)
	for rows.Next() {
		var id int64
		var key cityFacilityLifecycleRecoveryFactKey
		if err = rows.Scan(&id, &key.tick, &key.sequence); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan facility lifecycle recovery fact identity: %w", err)
		}
		if _, duplicate := items[key]; duplicate {
			_ = rows.Close()
			return nil, fmt.Errorf("duplicate facility lifecycle recovery fact identity")
		}
		items[key] = id
	}
	if err = closeCityRows(rows, "iterate facility lifecycle recovery fact identities"); err != nil {
		return nil, err
	}
	return items, nil
}

func clearCityFacilityLifecycleProjection(
	ctx context.Context, tx *sql.Tx, worldID int64,
) (int, error) {
	tables := []string{
		"city_facility_budget_movements",
		"city_facility_staff_assignments",
		"city_facility_incidents",
		"city_facility_operations",
		"city_facility_lifecycle_states",
		"city_facility_lifecycle_facts",
		"city_facility_lifecycle_policies",
		"city_facility_lifecycle_profiles",
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

func validateCityFacilityLifecycleRecoveryState(state *cityHashState) error {
	if state == nil || state.FacilityLifecycle == nil || state.PublicServices == nil ||
		!cityEngineSupportsFacilityLifecycle(state.SimulationVersion) {
		return fmt.Errorf("recovery F8.1 facility lifecycle state is unavailable")
	}
	lifecycle := state.FacilityLifecycle
	profile := lifecycle.Profile
	if profile.PolicyID != cityFacilityLifecyclePolicyID ||
		profile.PolicyVersion != cityFacilityLifecyclePolicyVersion ||
		profile.BaselineTick < 0 || profile.BaselineTick > state.CurrentTick ||
		profile.PolicyCount != int64(len(lifecycle.Policies)) ||
		profile.StateCount != int64(len(lifecycle.States)) ||
		profile.OperationCount != int64(len(lifecycle.Operations)) ||
		profile.StaffingCount != int64(len(lifecycle.StaffAssignments)) ||
		profile.IncidentCount != int64(len(lifecycle.Incidents)) ||
		profile.FactCount != int64(len(lifecycle.Facts)) ||
		profile.BudgetMovementCount != int64(len(lifecycle.BudgetMovements)) ||
		profile.Revision != profile.FactCount+1 || !json.Valid(profile.Metadata) ||
		profile.StateCount > 10_000 || profile.OperationCount > cityFacilityLifecycleMaximumItems ||
		profile.StaffingCount > cityFacilityLifecycleMaximumItems ||
		profile.IncidentCount > cityFacilityLifecycleMaximumItems {
		return fmt.Errorf("recovery facility lifecycle profile is inconsistent")
	}

	facilityTypes := make([]string, 0, len(state.PublicServices.FacilityTypes))
	for _, item := range state.PublicServices.FacilityTypes {
		facilityTypes = append(facilityTypes, item.Code)
	}
	expectedPolicies, expectedHash, err := cityFacilityLifecyclePolicyCatalog(facilityTypes)
	if err != nil || expectedHash != profile.PolicyHash ||
		!sameCityFacilityLifecyclePolicyCatalog(expectedPolicies, lifecycle.Policies) {
		return fmt.Errorf("recovery facility lifecycle policy catalog is inconsistent")
	}

	facts := make(map[cityFacilityLifecycleRecoveryFactKey]CityFacilityLifecycleFact, len(lifecycle.Facts))
	lastTick, lastSequence := int64(0), int64(0)
	for _, fact := range lifecycle.Facts {
		if fact.Tick <= 0 || fact.Tick > state.CurrentTick || fact.Sequence <= 0 ||
			(fact.Tick == lastTick && fact.Sequence != lastSequence+1) ||
			(fact.Tick != lastTick && fact.Sequence != 1) ||
			fact.VersionAfter != fact.VersionBefore+1 || !json.Valid(fact.Payload) ||
			!validCityFacilityLifecycleFactEnvelope(fact) {
			return fmt.Errorf("recovery facility lifecycle fact sequence is inconsistent")
		}
		key := cityFacilityLifecycleRecoveryFactKey{tick: fact.Tick, sequence: fact.Sequence}
		if _, duplicate := facts[key]; duplicate {
			return fmt.Errorf("recovery facility lifecycle fact identity is duplicated")
		}
		facts[key] = fact
		lastTick, lastSequence = fact.Tick, fact.Sequence
	}

	if err = validateCityFacilityLifecycleRecoveryProjections(state, facts); err != nil {
		return err
	}
	if err = validateCityFacilityLifecycleRecoveryReplay(lifecycle); err != nil {
		return err
	}
	return nil
}

func sameCityFacilityLifecyclePolicyCatalog(
	expected, actual []CityFacilityLifecyclePolicy,
) bool {
	if len(expected) != len(actual) {
		return false
	}
	for index := range expected {
		expectedPolicy, actualPolicy := expected[index], actual[index]
		var expectedSeed, actualSeed cityFacilityLifecyclePolicySeed
		if decodeStrictCityObject(expectedPolicy.Payload, &expectedSeed) != nil ||
			decodeStrictCityObject(actualPolicy.Payload, &actualSeed) != nil ||
			!reflect.DeepEqual(expectedSeed, actualSeed) {
			return false
		}
		expectedPolicy.Payload = nil
		actualPolicy.Payload = nil
		if !reflect.DeepEqual(expectedPolicy, actualPolicy) {
			return false
		}
	}
	return true
}

func validCityFacilityLifecycleFactEnvelope(fact CityFacilityLifecycleFact) bool {
	if !cityServiceSubjectCodePattern.MatchString(fact.SubjectCode) {
		return false
	}
	switch fact.FactType {
	case CityFacilityLifecycleFactFacilityInitialized, CityFacilityLifecycleFactCapacityChanged:
		return fact.Phase == CityFacilityLifecyclePhaseCommand &&
			fact.SourceCommandSequence != nil && fact.SubjectKind == "facility"
	case CityFacilityLifecycleFactOperationScheduled,
		CityFacilityLifecycleFactOperationStarted,
		CityFacilityLifecycleFactOperationCancelled:
		return fact.Phase == CityFacilityLifecyclePhaseCommand &&
			fact.SourceCommandSequence != nil && fact.SubjectKind == "operation"
	case CityFacilityLifecycleFactStaffingConfigured:
		return fact.Phase == CityFacilityLifecyclePhaseCommand &&
			fact.SourceCommandSequence != nil && fact.SubjectKind == "staffing"
	case CityFacilityLifecycleFactOperationProgressed,
		CityFacilityLifecycleFactOperationCompleted:
		return fact.Phase == CityFacilityLifecyclePhasePreService &&
			fact.SourceCommandSequence == nil && fact.SubjectKind == "operation"
	case CityFacilityLifecycleFactIncidentResolved:
		return fact.Phase == CityFacilityLifecyclePhasePreService &&
			fact.SourceCommandSequence == nil && fact.SubjectKind == "incident"
	case CityFacilityLifecycleFactConditionChanged:
		return fact.Phase == CityFacilityLifecyclePhasePostService &&
			fact.SourceCommandSequence == nil && fact.SubjectKind == "facility"
	case CityFacilityLifecycleFactIncidentOpened:
		return fact.Phase == CityFacilityLifecyclePhasePostService &&
			fact.SourceCommandSequence == nil && fact.SubjectKind == "incident"
	default:
		return false
	}
}

func validateCityFacilityLifecycleRecoveryProjections(
	state *cityHashState,
	facts map[cityFacilityLifecycleRecoveryFactKey]CityFacilityLifecycleFact,
) error {
	lifecycle := state.FacilityLifecycle
	facilities := make(map[string]string, len(state.PublicServices.Facilities))
	for _, facility := range state.PublicServices.Facilities {
		facilities[facility.Code] = facility.FacilityTypeCode
	}
	entities := make(map[string]string, len(state.Entities))
	for _, entity := range state.Entities {
		entities[entity.Code] = entity.EntityType
	}
	firms := make(map[string]int64, len(state.Physical.Firms))
	for _, firm := range state.Physical.Firms {
		firms[firm.EntityCode] = firm.EmployeeUnits
	}
	budgets := make(map[string]cityHashBudgetLine, len(state.Physical.BudgetLines))
	for _, budget := range state.Physical.BudgetLines {
		budgets[budget.Code] = budget
	}
	actors := make(map[string]struct{})
	if state.WorldRuntime != nil {
		for _, actor := range state.WorldRuntime.Actors {
			actors[actor.Code] = struct{}{}
		}
	}

	states := make(map[string]CityFacilityLifecycleState, len(lifecycle.States))
	for _, item := range lifecycle.States {
		typeCode, exists := facilities[item.FacilityCode]
		expectedStaffing := cityFacilityStaffingFactor(item.StaffRequiredUnits, item.StaffAssignedUnits)
		expectedEffective := cityFacilityLifecycleEffectiveFactor(
			item.LifecycleStatus, item.ConditionMilli, expectedStaffing, item.OperationFactorMilli,
		)
		if !exists || typeCode != item.FacilityTypeCode ||
			!validCityFacilityLifecycleStatus(item.LifecycleStatus) ||
			item.ConditionMilli < 0 || item.ConditionMilli > 1000 ||
			item.StaffRequiredUnits < 0 || item.StaffAssignedUnits < 0 ||
			item.StaffingFactorMilli != expectedStaffing ||
			item.OperationFactorMilli < 0 || item.OperationFactorMilli > 1000 ||
			item.EffectiveFactorMilli != expectedEffective ||
			item.MaintenanceDueTick < 0 || item.FailureCount < 0 ||
			item.UpdatedTick < 0 || item.UpdatedTick > state.CurrentTick ||
			item.Version <= 0 || !json.Valid(item.Metadata) ||
			!validCityFacilityLifecycleOptionalFactRef(item.SourceFactTick, item.SourceFactSequence, facts) {
			return fmt.Errorf("recovery facility lifecycle state is inconsistent")
		}
		if _, duplicate := states[item.FacilityCode]; duplicate {
			return fmt.Errorf("recovery facility lifecycle state is duplicated")
		}
		states[item.FacilityCode] = item
	}
	if len(states) != len(facilities) {
		return fmt.Errorf("recovery facility lifecycle state coverage is incomplete")
	}

	operations := make(map[string]CityFacilityOperation, len(lifecycle.Operations))
	openOperation := make(map[string]string)
	reservedLabor := make(map[string]int64)
	for _, item := range lifecycle.Operations {
		fact, factExists := facts[cityFacilityLifecycleRecoveryFactKey{
			tick: item.SourceFactTick, sequence: item.SourceFactSequence,
		}]
		_, facilityExists := facilities[item.FacilityCode]
		_, sponsorExists := entities[item.SponsorEntityCode]
		executorType, executorExists := entities[item.ExecutorEntityCode]
		_, firmExists := firms[item.ExecutorEntityCode]
		if !facilityExists || !sponsorExists || !executorExists || executorType != CityEntityTypeFirm ||
			!firmExists || !cityServiceCodePattern.MatchString(item.Code) ||
			!isCityFacilityOperationType(item.OperationType) ||
			!validCityFacilityOperationStatus(item.Status) ||
			item.PlannedStartTick <= 0 || item.PlannedStartTick > state.CurrentTick+1 ||
			item.DurationTicks <= 0 || item.ProgressMilli < 0 || item.ProgressMilli > 1000 ||
			item.RequiredBasicMaterialUnits < 0 || item.RequiredCapitalGoodsUnits < 0 ||
			item.RequiredLaborUnits <= 0 || item.BudgetUnits <= 0 ||
			item.BudgetCommittedUnits < 0 || item.BudgetSpentUnits < 0 ||
			item.BudgetCommittedUnits+item.BudgetSpentUnits > item.BudgetUnits ||
			item.CreatedTick <= 0 || item.CreatedTick > state.CurrentTick ||
			item.UpdatedTick < item.CreatedTick || item.UpdatedTick > state.CurrentTick ||
			item.Version <= 0 || !json.Valid(item.Metadata) || !factExists ||
			fact.SubjectKind != "operation" || fact.SubjectCode != item.Code ||
			fact.VersionAfter != item.Version || !validCityFacilityOperationTickShape(item) {
			return fmt.Errorf("recovery facility operation is inconsistent")
		}
		if item.BudgetCode != nil {
			if entities[item.SponsorEntityCode] != CityEntityTypeGovernment {
				return fmt.Errorf("recovery facility operation budget sponsor is inconsistent")
			}
			if _, exists := budgets[*item.BudgetCode]; !exists {
				return fmt.Errorf("recovery facility operation references unknown budget")
			}
		}
		if _, duplicate := operations[item.Code]; duplicate {
			return fmt.Errorf("recovery facility operation is duplicated")
		}
		operations[item.Code] = item
		if item.Status == CityFacilityOperationStatusPlanned || item.Status == CityFacilityOperationStatusActive {
			if _, duplicate := openOperation[item.FacilityCode]; duplicate {
				return fmt.Errorf("recovery facility has multiple open operations")
			}
			openOperation[item.FacilityCode] = item.Code
		}
		if item.Status == CityFacilityOperationStatusActive {
			reservedLabor[item.ExecutorEntityCode] += item.RequiredLaborUnits
		}
	}
	for code, reserved := range reservedLabor {
		if reserved > firms[code] {
			return fmt.Errorf("recovery facility operation labor reservation is inconsistent")
		}
	}

	assigned := make(map[string]int64)
	activeActors := make(map[string]struct{})
	assignments := make(map[string]struct{}, len(lifecycle.StaffAssignments))
	for _, item := range lifecycle.StaffAssignments {
		fact, factExists := facts[cityFacilityLifecycleRecoveryFactKey{
			tick: item.SourceFactTick, sequence: item.SourceFactSequence,
		}]
		_, facilityExists := facilities[item.FacilityCode]
		subjectExists := false
		if item.SubjectKind == "entity" {
			_, subjectExists = entities[item.SubjectCode]
		} else if item.SubjectKind == "actor" {
			_, subjectExists = actors[item.SubjectCode]
		}
		expectedEffective, multiplicationErr := cityMulDivFloor(
			item.AssignedUnits, item.QualificationMilli, 1000,
		)
		if multiplicationErr != nil || !facilityExists || !subjectExists ||
			!cityServiceCodePattern.MatchString(item.Code) ||
			!cityServiceCodePattern.MatchString(item.RoleCode) ||
			(item.SubjectKind != "entity" && item.SubjectKind != "actor") ||
			item.AssignedUnits <= 0 || (item.SubjectKind == "actor" && item.AssignedUnits != 1) ||
			item.QualificationMilli < 0 || item.QualificationMilli > 1000 ||
			item.EffectiveUnits != expectedEffective ||
			(item.Status != "active" && item.Status != "released") ||
			item.CreatedTick <= 0 || item.CreatedTick > state.CurrentTick ||
			item.UpdatedTick < item.CreatedTick || item.UpdatedTick > state.CurrentTick ||
			item.Version <= 0 || !json.Valid(item.Metadata) || !factExists ||
			fact.SubjectKind != "staffing" || fact.SubjectCode != item.Code ||
			fact.VersionAfter != item.Version {
			return fmt.Errorf("recovery facility staffing assignment is inconsistent")
		}
		if _, duplicate := assignments[item.Code]; duplicate {
			return fmt.Errorf("recovery facility staffing assignment is duplicated")
		}
		assignments[item.Code] = struct{}{}
		if item.Status == "active" {
			assigned[item.FacilityCode] += item.EffectiveUnits
			if item.SubjectKind == "actor" {
				if _, duplicate := activeActors[item.SubjectCode]; duplicate {
					return fmt.Errorf("recovery actor has multiple active staffing assignments")
				}
				activeActors[item.SubjectCode] = struct{}{}
			}
		}
	}

	incidents := make(map[string]CityFacilityIncident, len(lifecycle.Incidents))
	openIncident := make(map[string]string)
	for _, item := range lifecycle.Incidents {
		fact, factExists := facts[cityFacilityLifecycleRecoveryFactKey{
			tick: item.SourceFactTick, sequence: item.SourceFactSequence,
		}]
		_, facilityExists := facilities[item.FacilityCode]
		if !facilityExists || !cityServiceCodePattern.MatchString(item.Code) ||
			(item.Status != "open" && item.Status != "resolved") ||
			item.SeverityMilli <= 0 || item.SeverityMilli > 1000 ||
			item.ConditionBeforeMilli < 0 || item.ConditionBeforeMilli > 1000 ||
			item.FailureProbabilityPPM < 0 || item.FailureProbabilityPPM > 1_000_000 ||
			item.SampleValuePPM < 0 || item.SampleValuePPM >= 1_000_000 ||
			len(item.PRNGProof) != 64 || item.OpenedTick <= 0 || item.OpenedTick > state.CurrentTick ||
			item.Version <= 0 || !json.Valid(item.Metadata) || !factExists ||
			fact.SubjectKind != "incident" || fact.SubjectCode != item.Code ||
			fact.VersionAfter != item.Version || !validCityFacilityIncidentTickShape(item) {
			return fmt.Errorf("recovery facility incident is inconsistent")
		}
		if item.Status == "resolved" {
			operation, exists := operations[*item.RepairOperationCode]
			if !exists || operation.FacilityCode != item.FacilityCode ||
				operation.OperationType != CityFacilityOperationRepair ||
				operation.Status != CityFacilityOperationStatusCompleted {
				return fmt.Errorf("recovery facility incident repair reference is inconsistent")
			}
		}
		if _, duplicate := incidents[item.Code]; duplicate {
			return fmt.Errorf("recovery facility incident is duplicated")
		}
		incidents[item.Code] = item
		if item.Status == "open" {
			if _, duplicate := openIncident[item.FacilityCode]; duplicate {
				return fmt.Errorf("recovery facility has multiple open incidents")
			}
			openIncident[item.FacilityCode] = item.Code
		}
	}

	for facilityCode, item := range states {
		if assigned[facilityCode] != item.StaffAssignedUnits ||
			!sameOptionalCityFacilityCode(item.ActiveOperationCode, openOperation[facilityCode]) ||
			!sameOptionalCityFacilityCode(item.OpenIncidentCode, openIncident[facilityCode]) {
			return fmt.Errorf("recovery facility lifecycle head is inconsistent")
		}
	}
	return validateCityFacilityLifecycleBudgetMovements(lifecycle, operations, budgets, facts)
}

func validCityFacilityLifecycleOptionalFactRef(
	tick, sequence *int64,
	facts map[cityFacilityLifecycleRecoveryFactKey]CityFacilityLifecycleFact,
) bool {
	if tick == nil || sequence == nil {
		return tick == nil && sequence == nil
	}
	_, exists := facts[cityFacilityLifecycleRecoveryFactKey{tick: *tick, sequence: *sequence}]
	return exists
}

func validCityFacilityLifecycleStatus(value string) bool {
	switch value {
	case CityFacilityLifecycleStatusUncommissioned,
		CityFacilityLifecycleStatusOperational,
		CityFacilityLifecycleStatusMaintenance,
		CityFacilityLifecycleStatusFailed,
		CityFacilityLifecycleStatusDecommissioning,
		CityFacilityLifecycleStatusRetired:
		return true
	default:
		return false
	}
}

func validCityFacilityOperationStatus(value string) bool {
	switch value {
	case CityFacilityOperationStatusPlanned, CityFacilityOperationStatusActive,
		CityFacilityOperationStatusCompleted, CityFacilityOperationStatusCancelled:
		return true
	default:
		return false
	}
}

func validCityFacilityOperationTickShape(item CityFacilityOperation) bool {
	switch item.Status {
	case CityFacilityOperationStatusPlanned:
		return item.StartedTick == nil && item.CompletedTick == nil && item.ProgressMilli == 0 &&
			item.BudgetSpentUnits == 0
	case CityFacilityOperationStatusActive:
		return item.StartedTick != nil && item.CompletedTick == nil && item.ProgressMilli < 1000 &&
			item.BudgetCommittedUnits == 0 && item.BudgetSpentUnits == item.BudgetUnits
	case CityFacilityOperationStatusCompleted:
		return item.StartedTick != nil && item.CompletedTick != nil &&
			*item.CompletedTick >= *item.StartedTick && item.ProgressMilli == 1000 &&
			item.BudgetCommittedUnits == 0 && item.BudgetSpentUnits == item.BudgetUnits
	case CityFacilityOperationStatusCancelled:
		return item.StartedTick == nil && item.CompletedTick == nil && item.ProgressMilli == 0 &&
			item.BudgetCommittedUnits == 0 && item.BudgetSpentUnits == 0
	default:
		return false
	}
}

func validCityFacilityIncidentTickShape(item CityFacilityIncident) bool {
	if item.Status == "open" {
		return item.ResolvedTick == nil && item.RepairOperationCode == nil
	}
	return item.ResolvedTick != nil && item.RepairOperationCode != nil &&
		*item.ResolvedTick >= item.OpenedTick
}

func sameOptionalCityFacilityCode(pointer *string, value string) bool {
	if value == "" {
		return pointer == nil
	}
	return pointer != nil && *pointer == value
}

func validateCityFacilityLifecycleBudgetMovements(
	lifecycle *cityFacilityLifecycleHashState,
	operations map[string]CityFacilityOperation,
	budgets map[string]cityHashBudgetLine,
	facts map[cityFacilityLifecycleRecoveryFactKey]CityFacilityLifecycleFact,
) error {
	type operationTotals struct{ committed, spent, released int64 }
	totals := make(map[string]operationTotals)
	lastBudgetVersion := make(map[string]int64)
	lastBudgetCommitted := make(map[string]int64)
	lastBudgetSpent := make(map[string]int64)
	for _, movement := range lifecycle.BudgetMovements {
		fact, factExists := facts[cityFacilityLifecycleRecoveryFactKey{
			tick: movement.SourceFactTick, sequence: movement.SourceFactSequence,
		}]
		operation, operationExists := operations[movement.OperationCode]
		budget, budgetExists := budgets[movement.BudgetCode]
		if !factExists || !operationExists || !budgetExists || operation.BudgetCode == nil ||
			*operation.BudgetCode != movement.BudgetCode || fact.SubjectKind != "operation" ||
			fact.SubjectCode != movement.OperationCode || movement.AmountUnits <= 0 ||
			movement.CommittedBeforeUnits < 0 || movement.CommittedAfterUnits < 0 ||
			movement.SpentBeforeUnits < 0 || movement.SpentAfterUnits < 0 ||
			movement.BudgetVersionAfter != movement.BudgetVersionBefore+1 ||
			len(movement.Memo) > 256 || !validCityFacilityBudgetMovementShape(movement) {
			return fmt.Errorf("recovery facility budget movement is inconsistent")
		}
		if previousVersion, exists := lastBudgetVersion[movement.BudgetCode]; exists {
			if movement.BudgetVersionBefore < previousVersion ||
				movement.CommittedBeforeUnits != lastBudgetCommitted[movement.BudgetCode] ||
				movement.SpentBeforeUnits != lastBudgetSpent[movement.BudgetCode] {
				return fmt.Errorf("recovery facility budget movement chain is inconsistent")
			}
		}
		lastBudgetVersion[movement.BudgetCode] = movement.BudgetVersionAfter
		lastBudgetCommitted[movement.BudgetCode] = movement.CommittedAfterUnits
		lastBudgetSpent[movement.BudgetCode] = movement.SpentAfterUnits
		if movement.BudgetVersionAfter > budget.Version ||
			movement.CommittedAfterUnits > budget.AppropriatedUnits ||
			movement.SpentAfterUnits > budget.AppropriatedUnits {
			return fmt.Errorf("recovery facility budget movement exceeds current budget")
		}
		total := totals[movement.OperationCode]
		switch movement.MovementType {
		case "commit":
			total.committed += movement.AmountUnits
		case "spend":
			total.spent += movement.AmountUnits
		case "release":
			total.released += movement.AmountUnits
		default:
			return fmt.Errorf("recovery facility budget movement type is unsupported")
		}
		totals[movement.OperationCode] = total
	}
	for code, operation := range operations {
		total := totals[code]
		if operation.BudgetCode == nil {
			if total != (operationTotals{}) {
				return fmt.Errorf("recovery private facility operation has budget movements")
			}
			continue
		}
		if total.committed-total.spent-total.released != operation.BudgetCommittedUnits ||
			total.spent != operation.BudgetSpentUnits || total.committed != operation.BudgetUnits {
			return fmt.Errorf("recovery facility operation budget conservation is inconsistent")
		}
	}
	return nil
}

func validCityFacilityBudgetMovementShape(item CityFacilityBudgetMovement) bool {
	switch item.MovementType {
	case "commit":
		return item.CommittedAfterUnits == item.CommittedBeforeUnits+item.AmountUnits &&
			item.SpentAfterUnits == item.SpentBeforeUnits
	case "spend":
		return item.CommittedBeforeUnits == item.CommittedAfterUnits+item.AmountUnits &&
			item.SpentAfterUnits == item.SpentBeforeUnits+item.AmountUnits
	case "release":
		return item.CommittedBeforeUnits == item.CommittedAfterUnits+item.AmountUnits &&
			item.SpentAfterUnits == item.SpentBeforeUnits
	default:
		return false
	}
}

func validateCityFacilityLifecycleRecoveryReplay(target *cityFacilityLifecycleHashState) error {
	baselineStates := make(map[string]CityFacilityLifecycleState)
	initialized := make(map[string]struct{})
	remember := func(item *CityFacilityLifecycleState) {
		if item == nil {
			return
		}
		if _, created := initialized[item.FacilityCode]; created {
			return
		}
		if _, exists := baselineStates[item.FacilityCode]; !exists {
			baselineStates[item.FacilityCode] = *item
		}
	}
	for _, fact := range target.Facts {
		switch fact.FactType {
		case CityFacilityLifecycleFactFacilityInitialized,
			CityFacilityLifecycleFactCapacityChanged,
			CityFacilityLifecycleFactConditionChanged:
			var payload cityFacilityLifecycleStateFactPayload
			if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
				return fmt.Errorf("decode recovery facility state fact: %w", err)
			}
			if fact.FactType == CityFacilityLifecycleFactFacilityInitialized {
				initialized[payload.StateAfter.FacilityCode] = struct{}{}
				delete(baselineStates, payload.StateAfter.FacilityCode)
			} else {
				remember(payload.StateBefore)
			}
		case CityFacilityLifecycleFactOperationScheduled,
			CityFacilityLifecycleFactOperationStarted,
			CityFacilityLifecycleFactOperationCancelled,
			CityFacilityLifecycleFactOperationProgressed,
			CityFacilityLifecycleFactOperationCompleted:
			var payload cityFacilityLifecycleOperationFactPayload
			if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
				return fmt.Errorf("decode recovery facility operation fact: %w", err)
			}
			remember(payload.StateBefore)
		case CityFacilityLifecycleFactStaffingConfigured:
			var payload cityFacilityLifecycleStaffingFactPayload
			if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
				return fmt.Errorf("decode recovery facility staffing fact: %w", err)
			}
			remember(&payload.StateBefore)
		case CityFacilityLifecycleFactIncidentOpened, CityFacilityLifecycleFactIncidentResolved:
			var payload cityFacilityLifecycleIncidentFactPayload
			if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
				return fmt.Errorf("decode recovery facility incident fact: %w", err)
			}
			remember(&payload.StateBefore)
		}
	}
	for _, item := range target.States {
		if _, created := initialized[item.FacilityCode]; created {
			continue
		}
		if _, exists := baselineStates[item.FacilityCode]; !exists {
			baselineStates[item.FacilityCode] = item
		}
	}
	baseline := &CityFacilityLifecycleStateSet{
		Profile: CityFacilityLifecycleProfile{
			PolicyID: target.Profile.PolicyID, PolicyVersion: target.Profile.PolicyVersion,
			PolicyHash: target.Profile.PolicyHash, BaselineTick: target.Profile.BaselineTick,
			PolicyCount: target.Profile.PolicyCount, StateCount: int64(len(baselineStates)),
			Revision: 1, Metadata: target.Profile.Metadata,
		},
		Policies:         append([]CityFacilityLifecyclePolicy(nil), target.Policies...),
		States:           make([]CityFacilityLifecycleState, 0, len(baselineStates)),
		Operations:       make([]CityFacilityOperation, 0),
		StaffAssignments: make([]CityFacilityStaffAssignment, 0),
		Incidents:        make([]CityFacilityIncident, 0),
		BudgetMovements:  make([]CityFacilityBudgetMovement, 0),
		Facts:            make([]CityFacilityLifecycleFact, 0, len(target.Facts)),
	}
	for _, item := range baselineStates {
		baseline.States = append(baseline.States, item)
	}
	sortCityFacilityLifecycleState(baseline)
	for _, fact := range target.Facts {
		if err := reduceCityFacilityLifecycleFact(baseline, fact); err != nil {
			return fmt.Errorf("replay recovery facility lifecycle fact %d/%d: %w",
				fact.Tick, fact.Sequence, err)
		}
	}
	sortCityFacilityLifecycleState(baseline)
	if !reflect.DeepEqual(baseline, target) {
		return fmt.Errorf("recovery facility lifecycle projection does not match fact replay")
	}
	return nil
}

func restoreCityFacilityLifecycleProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	state *cityHashState,
	preservedFactIDs map[cityFacilityLifecycleRecoveryFactKey]int64,
) (int, error) {
	if err := validateCityFacilityLifecycleRecoveryState(state); err != nil {
		return 0, err
	}
	identities, err := loadCityFacilityLifecycleRecoveryIdentityMaps(ctx, tx, worldID)
	if err != nil {
		return 0, err
	}
	lifecycle := state.FacilityLifecycle
	count := 0
	policyIDs := make(map[string]int64, len(lifecycle.Policies))
	for _, policy := range lifecycle.Policies {
		var facilityTypeID int64
		if err = tx.QueryRowContext(ctx, `
SELECT id FROM city_facility_type_definitions WHERE world_id = $1 AND code = $2`,
			worldID, policy.FacilityTypeCode).Scan(&facilityTypeID); err != nil {
			return count, fmt.Errorf("resolve recovery facility lifecycle policy type %s: %w",
				policy.FacilityTypeCode, err)
		}
		var id int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO city_facility_lifecycle_policies
    (world_id, facility_type_id, policy_version, policy_hash,
     maintenance_interval_ticks, base_decay_milli, utilization_decay_milli,
     overdue_decay_milli, failure_threshold_milli, base_failure_ppm,
     condition_failure_ppm, capacity_units_per_staff,
     maintenance_restore_milli, repair_restore_milli, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb)
RETURNING id`, worldID, facilityTypeID, policy.PolicyVersion, policy.PolicyHash,
			policy.MaintenanceIntervalTicks, policy.BaseDecayMilli,
			policy.UtilizationDecayMilli, policy.OverdueDecayMilli,
			policy.FailureThresholdMilli, policy.BaseFailurePPM,
			policy.ConditionFailurePPM, policy.CapacityUnitsPerStaff,
			policy.MaintenanceRestoreMilli, policy.RepairRestoreMilli,
			[]byte(policy.Payload)).Scan(&id)
		if err != nil {
			return count, fmt.Errorf("restore facility lifecycle policy %s: %w", policy.FacilityTypeCode, err)
		}
		policyIDs[policy.FacilityTypeCode] = id
		count++
	}

	factIDs := make(map[cityFacilityLifecycleRecoveryFactKey]int64, len(lifecycle.Facts))
	for _, fact := range lifecycle.Facts {
		key := cityFacilityLifecycleRecoveryFactKey{tick: fact.Tick, sequence: fact.Sequence}
		var sourceCommandID any
		if fact.SourceCommandSequence != nil {
			id, exists := identities.commands[cityFacilityLifecycleRecoveryFactKey{
				tick: fact.Tick, sequence: *fact.SourceCommandSequence,
			}]
			if !exists {
				return count, fmt.Errorf("resolve recovery facility lifecycle command %d/%d",
					fact.Tick, *fact.SourceCommandSequence)
			}
			sourceCommandID = id
		}
		query := `
INSERT INTO city_facility_lifecycle_facts
    (world_id, tick, sequence, phase, source_command_id, fact_type,
     subject_kind, subject_code, version_before, version_after, payload, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, NOW())
RETURNING id`
		args := []any{worldID, fact.Tick, fact.Sequence, fact.Phase, sourceCommandID,
			fact.FactType, fact.SubjectKind, fact.SubjectCode, fact.VersionBefore,
			fact.VersionAfter, []byte(fact.Payload)}
		if preservedID := preservedFactIDs[key]; preservedID > 0 {
			query = `
INSERT INTO city_facility_lifecycle_facts
    (id, world_id, tick, sequence, phase, source_command_id, fact_type,
     subject_kind, subject_code, version_before, version_after, payload, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, NOW())
RETURNING id`
			args = append([]any{preservedID}, args...)
		}
		var id int64
		if err = tx.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
			return count, fmt.Errorf("restore facility lifecycle fact %d/%d: %w", fact.Tick, fact.Sequence, err)
		}
		factIDs[key] = id
		count++
	}

	operationIDs := make(map[string]int64, len(lifecycle.Operations))
	for _, operation := range lifecycle.Operations {
		facilityID := identities.facilities[operation.FacilityCode]
		sponsorID := identities.entities[operation.SponsorEntityCode]
		executorID := identities.entities[operation.ExecutorEntityCode]
		var budgetID any
		if operation.BudgetCode != nil {
			budgetID = identities.budgets[*operation.BudgetCode]
		}
		factID := factIDs[cityFacilityLifecycleRecoveryFactKey{
			tick: operation.SourceFactTick, sequence: operation.SourceFactSequence,
		}]
		var id int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO city_facility_operations
    (world_id, code, facility_id, operation_type, status,
     sponsor_entity_id, executor_entity_id, budget_line_id,
     planned_start_tick, started_tick, completed_tick, duration_ticks,
     progress_milli, required_basic_material_units,
     required_capital_goods_units, required_labor_units, budget_units,
     budget_committed_units, budget_spent_units, created_tick, updated_tick,
     version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24::jsonb)
RETURNING id`, worldID, operation.Code, facilityID, operation.OperationType,
			operation.Status, sponsorID, executorID, budgetID, operation.PlannedStartTick,
			cityNullableInt64(operation.StartedTick), cityNullableInt64(operation.CompletedTick),
			operation.DurationTicks, operation.ProgressMilli,
			operation.RequiredBasicMaterialUnits, operation.RequiredCapitalGoodsUnits,
			operation.RequiredLaborUnits, operation.BudgetUnits,
			operation.BudgetCommittedUnits, operation.BudgetSpentUnits,
			operation.CreatedTick, operation.UpdatedTick, operation.Version,
			factID, []byte(operation.Metadata)).Scan(&id)
		if err != nil {
			return count, fmt.Errorf("restore facility operation %s: %w", operation.Code, err)
		}
		operationIDs[operation.Code] = id
		count++
	}

	for _, assignment := range lifecycle.StaffAssignments {
		var entityID, actorID any
		if assignment.SubjectKind == "entity" {
			entityID = identities.entities[assignment.SubjectCode]
		} else {
			actorID = identities.actors[assignment.SubjectCode]
		}
		factID := factIDs[cityFacilityLifecycleRecoveryFactKey{
			tick: assignment.SourceFactTick, sequence: assignment.SourceFactSequence,
		}]
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_facility_staff_assignments
    (world_id, code, facility_id, role_code, subject_kind, subject_code,
     entity_id, actor_id, assigned_units, qualification_milli, effective_units,
     status, created_tick, updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17::jsonb)`, worldID, assignment.Code,
			identities.facilities[assignment.FacilityCode], assignment.RoleCode,
			assignment.SubjectKind, assignment.SubjectCode, entityID, actorID,
			assignment.AssignedUnits, assignment.QualificationMilli,
			assignment.EffectiveUnits, assignment.Status, assignment.CreatedTick,
			assignment.UpdatedTick, assignment.Version, factID,
			[]byte(assignment.Metadata)); err != nil {
			return count, fmt.Errorf("restore facility staffing assignment %s: %w", assignment.Code, err)
		}
		count++
	}

	for _, incident := range lifecycle.Incidents {
		factID := factIDs[cityFacilityLifecycleRecoveryFactKey{
			tick: incident.SourceFactTick, sequence: incident.SourceFactSequence,
		}]
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_facility_incidents
    (world_id, code, facility_id, status, severity_milli,
     condition_before_milli, failure_probability_ppm, sample_value_ppm,
     prng_proof, opened_tick, resolved_tick, repair_operation_code,
     version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15::jsonb)`, worldID, incident.Code,
			identities.facilities[incident.FacilityCode], incident.Status,
			incident.SeverityMilli, incident.ConditionBeforeMilli,
			incident.FailureProbabilityPPM, incident.SampleValuePPM,
			incident.PRNGProof, incident.OpenedTick, cityNullableInt64(incident.ResolvedTick),
			nullableStringValue(incident.RepairOperationCode), incident.Version,
			factID, []byte(incident.Metadata)); err != nil {
			return count, fmt.Errorf("restore facility incident %s: %w", incident.Code, err)
		}
		count++
	}

	for _, item := range lifecycle.States {
		var sourceFactID any
		if item.SourceFactTick != nil {
			sourceFactID = factIDs[cityFacilityLifecycleRecoveryFactKey{
				tick: *item.SourceFactTick, sequence: *item.SourceFactSequence,
			}]
		}
		policyID := policyIDs[item.FacilityTypeCode]
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_facility_lifecycle_states
    (world_id, facility_id, policy_id, lifecycle_status, condition_milli,
     staff_required_units, staff_assigned_units, staffing_factor_milli,
     operation_factor_milli, effective_factor_milli, last_maintenance_tick,
     maintenance_due_tick, active_operation_code, open_incident_code,
     failure_count, updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17, $18, $19::jsonb)`, worldID,
			identities.facilities[item.FacilityCode], policyID, item.LifecycleStatus,
			item.ConditionMilli, item.StaffRequiredUnits, item.StaffAssignedUnits,
			item.StaffingFactorMilli, item.OperationFactorMilli,
			item.EffectiveFactorMilli, cityNullableInt64(item.LastMaintenanceTick),
			item.MaintenanceDueTick, nullableStringValue(item.ActiveOperationCode),
			nullableStringValue(item.OpenIncidentCode), item.FailureCount,
			item.UpdatedTick, item.Version, sourceFactID, []byte(item.Metadata)); err != nil {
			return count, fmt.Errorf("restore facility lifecycle state %s: %w", item.FacilityCode, err)
		}
		count++
	}

	for _, movement := range lifecycle.BudgetMovements {
		factID := factIDs[cityFacilityLifecycleRecoveryFactKey{
			tick: movement.SourceFactTick, sequence: movement.SourceFactSequence,
		}]
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_facility_budget_movements
    (world_id, source_fact_id, operation_id, budget_line_id, movement_type,
     amount_units, committed_before_units, committed_after_units,
     spent_before_units, spent_after_units, budget_version_before,
     budget_version_after, memo)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			worldID, factID, operationIDs[movement.OperationCode],
			identities.budgets[movement.BudgetCode], movement.MovementType,
			movement.AmountUnits, movement.CommittedBeforeUnits,
			movement.CommittedAfterUnits, movement.SpentBeforeUnits,
			movement.SpentAfterUnits, movement.BudgetVersionBefore,
			movement.BudgetVersionAfter, movement.Memo); err != nil {
			return count, fmt.Errorf("restore facility budget movement %d/%d: %w",
				movement.SourceFactTick, movement.SourceFactSequence, err)
		}
		count++
	}

	profile := lifecycle.Profile
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_facility_lifecycle_profiles
    (world_id, policy_id, policy_version, policy_hash, baseline_tick,
     policy_count, state_count, operation_count, staffing_count,
     incident_count, fact_count, budget_movement_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`,
		worldID, profile.PolicyID, profile.PolicyVersion, profile.PolicyHash,
		profile.BaselineTick, profile.PolicyCount, profile.StateCount,
		profile.OperationCount, profile.StaffingCount, profile.IncidentCount,
		profile.FactCount, profile.BudgetMovementCount, profile.Revision,
		[]byte(profile.Metadata)); err != nil {
		return count, fmt.Errorf("restore facility lifecycle profile: %w", err)
	}
	count++
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_facility_lifecycle_foundation($1)`, worldID); err != nil {
		return count, fmt.Errorf("assert recovered facility lifecycle foundation: %w", err)
	}
	return count, nil
}

func loadCityFacilityLifecycleRecoveryIdentityMaps(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
) (cityFacilityLifecycleRecoveryIdentityMaps, error) {
	items := cityFacilityLifecycleRecoveryIdentityMaps{
		facilities: make(map[string]int64), facilityTypes: make(map[string]string),
		entities: make(map[string]int64), entityTypes: make(map[string]string),
		budgets: make(map[string]int64), actors: make(map[string]int64),
		commands: make(map[cityFacilityLifecycleRecoveryFactKey]int64),
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT facility.id, facility.code, type.code
FROM city_facilities facility
JOIN city_facility_type_definitions type ON type.id = facility.facility_type_id
WHERE facility.world_id = $1`, worldID)
	if err != nil {
		return items, fmt.Errorf("load recovery facility identities: %w", err)
	}
	for rows.Next() {
		var id int64
		var code, typeCode string
		if err = rows.Scan(&id, &code, &typeCode); err != nil {
			_ = rows.Close()
			return items, err
		}
		items.facilities[code], items.facilityTypes[code] = id, typeCode
	}
	if err = closeCityRows(rows, "iterate recovery facility identities"); err != nil {
		return items, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT id, code, entity_type FROM city_economic_entities WHERE world_id = $1`, worldID)
	if err != nil {
		return items, fmt.Errorf("load recovery facility entity identities: %w", err)
	}
	for rows.Next() {
		var id int64
		var code, entityType string
		if err = rows.Scan(&id, &code, &entityType); err != nil {
			_ = rows.Close()
			return items, err
		}
		items.entities[code], items.entityTypes[code] = id, entityType
	}
	if err = closeCityRows(rows, "iterate recovery facility entity identities"); err != nil {
		return items, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT id, code FROM city_government_budget_lines WHERE world_id = $1`, worldID)
	if err != nil {
		return items, fmt.Errorf("load recovery facility budget identities: %w", err)
	}
	for rows.Next() {
		var id int64
		var code string
		if err = rows.Scan(&id, &code); err != nil {
			_ = rows.Close()
			return items, err
		}
		items.budgets[code] = id
	}
	if err = closeCityRows(rows, "iterate recovery facility budget identities"); err != nil {
		return items, err
	}

	rows, err = queryer.QueryContext(ctx, `SELECT id, code FROM world_actors WHERE world_id = $1`, worldID)
	if err != nil {
		return items, fmt.Errorf("load recovery facility actor identities: %w", err)
	}
	for rows.Next() {
		var id int64
		var code string
		if err = rows.Scan(&id, &code); err != nil {
			_ = rows.Close()
			return items, err
		}
		items.actors[code] = id
	}
	if err = closeCityRows(rows, "iterate recovery facility actor identities"); err != nil {
		return items, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT id, processed_tick, sequence
FROM city_commands
WHERE world_id = $1 AND status = 'applied' AND processed_tick IS NOT NULL`, worldID)
	if err != nil {
		return items, fmt.Errorf("load recovery facility command identities: %w", err)
	}
	for rows.Next() {
		var id, tick, sequence int64
		if err = rows.Scan(&id, &tick, &sequence); err != nil {
			_ = rows.Close()
			return items, err
		}
		items.commands[cityFacilityLifecycleRecoveryFactKey{tick: tick, sequence: sequence}] = id
	}
	if err = closeCityRows(rows, "iterate recovery facility command identities"); err != nil {
		return items, err
	}
	return items, nil
}
