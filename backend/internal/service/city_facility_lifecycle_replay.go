package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
)

type cityFacilityLifecycleStateFactPayload struct {
	SchemaVersion          int                         `json:"schema_version"`
	InstalledCapacityUnits *int64                      `json:"installed_capacity_units"`
	UtilizationMilli       *int                        `json:"utilization_milli"`
	OverdueTicks           *int64                      `json:"overdue_ticks"`
	DecayMilli             *int64                      `json:"decay_milli"`
	StateBefore            *CityFacilityLifecycleState `json:"state_before"`
	StateAfter             CityFacilityLifecycleState  `json:"state_after"`
}

type cityFacilityLifecycleJournalReference struct {
	Tick         int64  `json:"tick"`
	Sequence     int64  `json:"sequence"`
	OperationKey string `json:"operation_key"`
	AmountUnits  int64  `json:"amount_units"`
}

type cityFacilityLifecycleResourceReference struct {
	Tick          int64  `json:"tick"`
	Sequence      int64  `json:"sequence"`
	OperationKey  string `json:"operation_key"`
	ResourceCode  string `json:"resource_code"`
	QuantityUnits int64  `json:"quantity_units"`
}

type cityFacilityLifecycleOperationFactPayload struct {
	SchemaVersion      int                                       `json:"schema_version"`
	OperationBefore    *CityFacilityOperation                    `json:"operation_before"`
	OperationAfter     CityFacilityOperation                     `json:"operation_after"`
	StateBefore        *CityFacilityLifecycleState               `json:"state_before"`
	StateAfter         *CityFacilityLifecycleState               `json:"state_after"`
	Journal            *cityFacilityLifecycleJournalReference    `json:"journal"`
	ResourceOperations *[]cityFacilityLifecycleResourceReference `json:"resource_operations"`
	BudgetMovement     *CityFacilityBudgetMovement               `json:"budget_movement"`
	CommandMetadata    json.RawMessage                           `json:"command_metadata"`
}

type cityFacilityLifecycleStaffingFactPayload struct {
	SchemaVersion    int                          `json:"schema_version"`
	AssignmentBefore *CityFacilityStaffAssignment `json:"assignment_before"`
	AssignmentAfter  CityFacilityStaffAssignment  `json:"assignment_after"`
	StateBefore      CityFacilityLifecycleState   `json:"state_before"`
	StateAfter       CityFacilityLifecycleState   `json:"state_after"`
}

type cityFacilityLifecycleIncidentFactPayload struct {
	SchemaVersion  int                        `json:"schema_version"`
	IncidentBefore *CityFacilityIncident      `json:"incident_before"`
	IncidentAfter  CityFacilityIncident       `json:"incident_after"`
	StateBefore    CityFacilityLifecycleState `json:"state_before"`
	StateAfter     CityFacilityLifecycleState `json:"state_after"`
}

func replayCityFacilityLifecycleBeforeService(
	ctx context.Context, queryer citySQLQueryer,
	worldID, tick int64, state *cityHashState,
) error {
	return replayCityFacilityLifecycleFacts(ctx, queryer, worldID, tick, state, false)
}

func replayCityFacilityLifecycleAfterService(
	ctx context.Context, queryer citySQLQueryer,
	worldID, tick int64, state *cityHashState,
) error {
	return replayCityFacilityLifecycleFacts(ctx, queryer, worldID, tick, state, true)
}

func replayCityFacilityLifecycleFacts(
	ctx context.Context, queryer citySQLQueryer,
	worldID, tick int64, state *cityHashState, postService bool,
) error {
	if state == nil || state.FacilityLifecycle == nil || tick <= 0 {
		return fmt.Errorf("facility lifecycle replay state is unavailable")
	}
	phasePredicate := `fact.phase IN ('command', 'pre_service')`
	if postService {
		phasePredicate = `fact.phase = 'post_service'`
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, fact.phase, command.sequence,
       fact.fact_type, fact.subject_kind, fact.subject_code,
       fact.version_before, fact.version_after, fact.payload
FROM city_facility_lifecycle_facts fact
LEFT JOIN city_commands command
  ON command.id = fact.source_command_id AND command.world_id = fact.world_id
WHERE fact.world_id = $1 AND fact.tick = $2 AND fact.posted_at IS NOT NULL
  AND `+phasePredicate+`
ORDER BY fact.sequence ASC`, worldID, tick)
	if err != nil {
		return fmt.Errorf("load replay city facility lifecycle facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	expectedSequence := int64(1)
	for _, existing := range state.FacilityLifecycle.Facts {
		if existing.Tick == tick && existing.Sequence >= expectedSequence {
			expectedSequence = existing.Sequence + 1
		}
	}
	for rows.Next() {
		var fact CityFacilityLifecycleFact
		var commandSequence sql.NullInt64
		if err = rows.Scan(
			&fact.Tick, &fact.Sequence, &fact.Phase, &commandSequence,
			&fact.FactType, &fact.SubjectKind, &fact.SubjectCode,
			&fact.VersionBefore, &fact.VersionAfter, &fact.Payload,
		); err != nil {
			return fmt.Errorf("scan replay city facility lifecycle fact: %w", err)
		}
		if fact.Tick != tick || fact.Sequence != expectedSequence {
			return fmt.Errorf("facility lifecycle fact sequence is not contiguous")
		}
		if commandSequence.Valid {
			fact.SourceCommandSequence = int64Pointer(commandSequence.Int64)
		}
		if err = reduceCityFacilityLifecycleFact(state.FacilityLifecycle, fact); err != nil {
			return fmt.Errorf("reduce facility lifecycle fact %d: %w", fact.Sequence, err)
		}
		if err = replayCityFacilityLifecyclePhysicalProjection(state, fact); err != nil {
			return fmt.Errorf("project facility lifecycle fact %d: %w", fact.Sequence, err)
		}
		expectedSequence++
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate replay city facility lifecycle facts: %w", err)
	}
	sortCityFacilityLifecycleState(state.FacilityLifecycle)
	refreshAllCityServiceDispatchCapacities(state.PublicServices, state.FacilityLifecycle)
	return nil
}

func reduceCityFacilityLifecycleFact(
	state *cityFacilityLifecycleHashState, fact CityFacilityLifecycleFact,
) error {
	if state == nil || fact.Tick <= 0 || fact.Sequence <= 0 ||
		fact.VersionAfter != fact.VersionBefore+1 || !json.Valid(fact.Payload) ||
		state.Profile.Revision != state.Profile.FactCount+1 {
		return fmt.Errorf("facility lifecycle fact or profile is invalid")
	}
	if fact.Phase == CityFacilityLifecyclePhaseCommand {
		if fact.SourceCommandSequence == nil {
			return fmt.Errorf("facility lifecycle command fact has no source command")
		}
	} else if fact.SourceCommandSequence != nil ||
		(fact.Phase != CityFacilityLifecyclePhasePreService &&
			fact.Phase != CityFacilityLifecyclePhasePostService) {
		return fmt.Errorf("facility lifecycle automatic fact origin is invalid")
	}
	switch fact.FactType {
	case CityFacilityLifecycleFactFacilityInitialized,
		CityFacilityLifecycleFactCapacityChanged,
		CityFacilityLifecycleFactConditionChanged:
		var payload cityFacilityLifecycleStateFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode facility lifecycle state fact: %w", err)
		}
		if payload.SchemaVersion != 1 || payload.StateAfter.FacilityCode != fact.SubjectCode ||
			payload.StateAfter.Version != fact.VersionAfter ||
			!sameCityFacilityLifecycleFactSource(payload.StateAfter, fact) {
			return fmt.Errorf("facility lifecycle state fact identity is invalid")
		}
		if err := validateCityFacilityLifecycleStateFactPayload(state, fact, payload); err != nil {
			return err
		}
		index := cityFacilityLifecycleStateIndex(state.States, fact.SubjectCode)
		if fact.FactType == CityFacilityLifecycleFactFacilityInitialized {
			if fact.VersionBefore != 0 || index >= 0 || payload.StateBefore != nil {
				return fmt.Errorf("facility lifecycle initialization chain is invalid")
			}
			state.States = append(state.States, payload.StateAfter)
			state.Profile.StateCount++
		} else {
			if index < 0 || payload.StateBefore == nil ||
				state.States[index].Version != fact.VersionBefore ||
				payload.StateBefore.Version != fact.VersionBefore {
				return fmt.Errorf("facility lifecycle state chain is invalid")
			}
			state.States[index] = payload.StateAfter
		}
	case CityFacilityLifecycleFactOperationScheduled,
		CityFacilityLifecycleFactOperationStarted,
		CityFacilityLifecycleFactOperationCancelled,
		CityFacilityLifecycleFactOperationProgressed,
		CityFacilityLifecycleFactOperationCompleted:
		var payload cityFacilityLifecycleOperationFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode facility lifecycle operation fact: %w", err)
		}
		if payload.SchemaVersion != 1 || payload.OperationAfter.Code != fact.SubjectCode ||
			payload.OperationAfter.Version != fact.VersionAfter ||
			payload.OperationAfter.SourceFactTick != fact.Tick ||
			payload.OperationAfter.SourceFactSequence != fact.Sequence {
			return fmt.Errorf("facility lifecycle operation fact identity is invalid")
		}
		if err := validateCityFacilityLifecycleOperationFactPayload(fact, payload); err != nil {
			return err
		}
		index := cityFacilityOperationIndex(state.Operations, fact.SubjectCode)
		if fact.FactType == CityFacilityLifecycleFactOperationScheduled {
			if fact.VersionBefore != 0 || index >= 0 || payload.OperationBefore != nil {
				return fmt.Errorf("facility operation scheduling chain is invalid")
			}
			state.Operations = append(state.Operations, payload.OperationAfter)
			state.Profile.OperationCount++
		} else {
			if index < 0 || payload.OperationBefore == nil ||
				state.Operations[index].Version != fact.VersionBefore ||
				payload.OperationBefore.Version != fact.VersionBefore {
				return fmt.Errorf("facility operation chain is invalid")
			}
			state.Operations[index] = payload.OperationAfter
		}
		if payload.StateAfter != nil {
			if err := reduceCityFacilityLifecycleEmbeddedState(
				state, payload.StateBefore, *payload.StateAfter, fact,
			); err != nil {
				return err
			}
		}
		if payload.BudgetMovement != nil {
			if payload.BudgetMovement.SourceFactTick != fact.Tick ||
				payload.BudgetMovement.SourceFactSequence != fact.Sequence ||
				payload.BudgetMovement.OperationCode != payload.OperationAfter.Code {
				return fmt.Errorf("facility budget movement fact identity is invalid")
			}
			state.BudgetMovements = append(state.BudgetMovements, *payload.BudgetMovement)
			state.Profile.BudgetMovementCount++
		}
	case CityFacilityLifecycleFactStaffingConfigured:
		var payload cityFacilityLifecycleStaffingFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode facility staffing fact: %w", err)
		}
		if payload.SchemaVersion != 1 || payload.AssignmentAfter.Code != fact.SubjectCode ||
			payload.AssignmentAfter.Version != fact.VersionAfter ||
			payload.AssignmentAfter.SourceFactTick != fact.Tick ||
			payload.AssignmentAfter.SourceFactSequence != fact.Sequence {
			return fmt.Errorf("facility staffing fact identity is invalid")
		}
		index := cityFacilityStaffAssignmentIndex(state.StaffAssignments, fact.SubjectCode)
		if fact.VersionBefore == 0 {
			if index >= 0 || payload.AssignmentBefore != nil {
				return fmt.Errorf("facility staffing creation chain is invalid")
			}
			state.StaffAssignments = append(state.StaffAssignments, payload.AssignmentAfter)
			state.Profile.StaffingCount++
		} else {
			if index < 0 || payload.AssignmentBefore == nil ||
				state.StaffAssignments[index].Version != fact.VersionBefore {
				return fmt.Errorf("facility staffing update chain is invalid")
			}
			state.StaffAssignments[index] = payload.AssignmentAfter
		}
		if err := reduceCityFacilityLifecycleEmbeddedState(
			state, &payload.StateBefore, payload.StateAfter, fact,
		); err != nil {
			return err
		}
	case CityFacilityLifecycleFactIncidentOpened,
		CityFacilityLifecycleFactIncidentResolved:
		var payload cityFacilityLifecycleIncidentFactPayload
		if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode facility incident fact: %w", err)
		}
		if payload.SchemaVersion != 1 || payload.IncidentAfter.Code != fact.SubjectCode ||
			payload.IncidentAfter.Version != fact.VersionAfter ||
			payload.IncidentAfter.SourceFactTick != fact.Tick ||
			payload.IncidentAfter.SourceFactSequence != fact.Sequence {
			return fmt.Errorf("facility incident fact identity is invalid")
		}
		index := cityFacilityIncidentIndex(state.Incidents, fact.SubjectCode)
		if fact.FactType == CityFacilityLifecycleFactIncidentOpened {
			if fact.VersionBefore != 0 || index >= 0 || payload.IncidentBefore != nil {
				return fmt.Errorf("facility incident opening chain is invalid")
			}
			state.Incidents = append(state.Incidents, payload.IncidentAfter)
			state.Profile.IncidentCount++
		} else {
			if index < 0 || payload.IncidentBefore == nil ||
				state.Incidents[index].Version != fact.VersionBefore {
				return fmt.Errorf("facility incident resolution chain is invalid")
			}
			state.Incidents[index] = payload.IncidentAfter
		}
		if err := reduceCityFacilityLifecycleEmbeddedState(
			state, &payload.StateBefore, payload.StateAfter, fact,
		); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported facility lifecycle fact type %q", fact.FactType)
	}
	state.Facts = append(state.Facts, fact)
	state.Profile.FactCount++
	state.Profile.Revision++
	return nil
}

func validateCityFacilityLifecycleStateFactPayload(
	state *cityFacilityLifecycleHashState,
	fact CityFacilityLifecycleFact,
	payload cityFacilityLifecycleStateFactPayload,
) error {
	hasCapacity := payload.InstalledCapacityUnits != nil
	hasWear := payload.UtilizationMilli != nil || payload.OverdueTicks != nil || payload.DecayMilli != nil
	switch fact.FactType {
	case CityFacilityLifecycleFactFacilityInitialized:
		if hasCapacity || hasWear || payload.StateBefore != nil {
			return fmt.Errorf("facility lifecycle initialization payload shape is invalid")
		}
	case CityFacilityLifecycleFactCapacityChanged:
		if !hasCapacity || hasWear || payload.StateBefore == nil ||
			*payload.InstalledCapacityUnits < 0 ||
			*payload.InstalledCapacityUnits > cityServiceMaximumConfiguredUnits {
			return fmt.Errorf("facility lifecycle capacity payload shape is invalid")
		}
		policy := cityFacilityLifecyclePolicyForType(state, payload.StateAfter.FacilityTypeCode)
		if policy == nil || policy.CapacityUnitsPerStaff <= 0 {
			return fmt.Errorf("facility lifecycle capacity policy is unavailable")
		}
		expectedRequired := int64(0)
		if *payload.InstalledCapacityUnits > 0 {
			expectedRequired = (*payload.InstalledCapacityUnits-1)/policy.CapacityUnitsPerStaff + 1
		}
		if payload.StateAfter.StaffRequiredUnits != expectedRequired {
			return fmt.Errorf("facility lifecycle capacity staffing projection is invalid")
		}
	case CityFacilityLifecycleFactConditionChanged:
		if hasCapacity || payload.StateBefore == nil || payload.UtilizationMilli == nil ||
			payload.OverdueTicks == nil || payload.DecayMilli == nil ||
			*payload.UtilizationMilli < 0 || *payload.UtilizationMilli > 1000 ||
			*payload.OverdueTicks < 0 || *payload.DecayMilli <= 0 || *payload.DecayMilli > 1000 {
			return fmt.Errorf("facility lifecycle wear payload shape is invalid")
		}
		expectedCondition := payload.StateBefore.ConditionMilli - int(*payload.DecayMilli)
		if expectedCondition < 0 {
			expectedCondition = 0
		}
		if payload.StateAfter.ConditionMilli != expectedCondition {
			return fmt.Errorf("facility lifecycle wear projection is invalid")
		}
	default:
		return fmt.Errorf("unsupported facility lifecycle state fact type %q", fact.FactType)
	}
	return nil
}

func validateCityFacilityLifecycleOperationFactPayload(
	fact CityFacilityLifecycleFact,
	payload cityFacilityLifecycleOperationFactPayload,
) error {
	before, after := payload.OperationBefore, payload.OperationAfter
	if before != nil && (before.Code != after.Code || before.FacilityCode != after.FacilityCode ||
		before.Version != fact.VersionBefore) {
		return fmt.Errorf("facility operation before-image identity is invalid")
	}
	hasCommandMetadata := len(payload.CommandMetadata) > 0
	if hasCommandMetadata && (!json.Valid(payload.CommandMetadata) ||
		string(payload.CommandMetadata) == "null" || payload.CommandMetadata[0] != '{') {
		return fmt.Errorf("facility operation command metadata is invalid")
	}
	switch fact.FactType {
	case CityFacilityLifecycleFactOperationScheduled:
		if before != nil || after.Status != CityFacilityOperationStatusPlanned ||
			payload.StateBefore == nil || payload.StateAfter == nil ||
			payload.Journal != nil || payload.ResourceOperations != nil || hasCommandMetadata {
			return fmt.Errorf("facility operation scheduling payload shape is invalid")
		}
	case CityFacilityLifecycleFactOperationStarted:
		if before == nil || before.Status != CityFacilityOperationStatusPlanned ||
			after.Status != CityFacilityOperationStatusActive ||
			payload.StateBefore == nil || payload.StateAfter == nil ||
			payload.Journal == nil || payload.ResourceOperations == nil || !hasCommandMetadata {
			return fmt.Errorf("facility operation start payload shape is invalid")
		}
		if err := validateCityFacilityLifecycleStartEvidence(fact, payload); err != nil {
			return err
		}
	case CityFacilityLifecycleFactOperationCancelled:
		if before == nil || before.Status != CityFacilityOperationStatusPlanned ||
			after.Status != CityFacilityOperationStatusCancelled ||
			payload.StateBefore == nil || payload.StateAfter == nil ||
			payload.Journal != nil || payload.ResourceOperations != nil || !hasCommandMetadata {
			return fmt.Errorf("facility operation cancellation payload shape is invalid")
		}
	case CityFacilityLifecycleFactOperationProgressed:
		if before == nil || before.Status != CityFacilityOperationStatusActive ||
			after.Status != CityFacilityOperationStatusActive ||
			after.ProgressMilli <= before.ProgressMilli || after.ProgressMilli >= 1000 ||
			payload.StateBefore != nil || payload.StateAfter != nil || payload.Journal != nil ||
			payload.ResourceOperations != nil || payload.BudgetMovement != nil || hasCommandMetadata {
			return fmt.Errorf("facility operation progress payload shape is invalid")
		}
	case CityFacilityLifecycleFactOperationCompleted:
		if before == nil || before.Status != CityFacilityOperationStatusActive ||
			after.Status != CityFacilityOperationStatusCompleted || after.ProgressMilli != 1000 ||
			payload.StateBefore == nil || payload.StateAfter == nil || payload.Journal != nil ||
			payload.ResourceOperations != nil || payload.BudgetMovement != nil || hasCommandMetadata {
			return fmt.Errorf("facility operation completion payload shape is invalid")
		}
	default:
		return fmt.Errorf("unsupported facility lifecycle operation fact type %q", fact.FactType)
	}
	return nil
}

func validateCityFacilityLifecycleStartEvidence(
	fact CityFacilityLifecycleFact,
	payload cityFacilityLifecycleOperationFactPayload,
) error {
	operation := payload.OperationAfter
	journal := payload.Journal
	if journal.Tick != fact.Tick || journal.Sequence <= 0 ||
		journal.OperationKey != "facility:"+operation.Code ||
		journal.AmountUnits != operation.BudgetUnits {
		return fmt.Errorf("facility operation start journal evidence is invalid")
	}
	expected := make(map[string]int64, 2)
	if operation.RequiredBasicMaterialUnits > 0 {
		expected["basic_material"] = operation.RequiredBasicMaterialUnits
	}
	if operation.RequiredCapitalGoodsUnits > 0 {
		expected["capital_goods"] = operation.RequiredCapitalGoodsUnits
	}
	if len(*payload.ResourceOperations) != len(expected) {
		return fmt.Errorf("facility operation start resource evidence count is invalid")
	}
	seen := make(map[string]struct{}, len(expected))
	previousSequence := int64(0)
	for index, resource := range *payload.ResourceOperations {
		quantity, exists := expected[resource.ResourceCode]
		if !exists || resource.Tick != fact.Tick || resource.Sequence <= 0 ||
			resource.QuantityUnits != quantity ||
			resource.OperationKey != "facility:"+operation.Code+":"+resource.ResourceCode {
			return fmt.Errorf("facility operation start resource evidence is invalid")
		}
		if _, duplicate := seen[resource.ResourceCode]; duplicate {
			return fmt.Errorf("facility operation start resource evidence is duplicated")
		}
		if index > 0 && resource.Sequence != previousSequence+1 {
			return fmt.Errorf("facility operation start resource sequence is not contiguous")
		}
		seen[resource.ResourceCode] = struct{}{}
		previousSequence = resource.Sequence
	}
	return nil
}

func replayCityFacilityLifecyclePhysicalProjection(
	state *cityHashState, fact CityFacilityLifecycleFact,
) error {
	if state == nil || state.FacilityLifecycle == nil {
		return fmt.Errorf("facility lifecycle physical projection is unavailable")
	}
	switch fact.FactType {
	case CityFacilityLifecycleFactOperationScheduled,
		CityFacilityLifecycleFactOperationStarted,
		CityFacilityLifecycleFactOperationCancelled,
		CityFacilityLifecycleFactOperationProgressed,
		CityFacilityLifecycleFactOperationCompleted:
	default:
		return nil
	}
	var payload cityFacilityLifecycleOperationFactPayload
	if err := decodeStrictCityObject(fact.Payload, &payload); err != nil {
		return fmt.Errorf("decode facility operation physical projection: %w", err)
	}
	movement := payload.BudgetMovement
	if movement == nil {
		if payload.OperationAfter.BudgetCode != nil &&
			fact.FactType == CityFacilityLifecycleFactOperationScheduled {
			return fmt.Errorf("government facility operation has no budget commitment")
		}
		return nil
	}
	if payload.OperationAfter.BudgetCode == nil ||
		*payload.OperationAfter.BudgetCode != movement.BudgetCode ||
		movement.OperationCode != payload.OperationAfter.Code ||
		movement.SourceFactTick != fact.Tick ||
		movement.SourceFactSequence != fact.Sequence ||
		movement.AmountUnits <= 0 ||
		movement.BudgetVersionAfter != movement.BudgetVersionBefore+1 ||
		!validCityFacilityBudgetMovementShape(*movement) {
		return fmt.Errorf("facility lifecycle budget movement identity is invalid")
	}
	position := -1
	for index := range state.Physical.BudgetLines {
		if state.Physical.BudgetLines[index].Code == movement.BudgetCode {
			if position >= 0 {
				return fmt.Errorf("facility lifecycle budget code is duplicated")
			}
			position = index
		}
	}
	if position < 0 {
		return fmt.Errorf("facility lifecycle budget movement references unknown budget")
	}
	budget := &state.Physical.BudgetLines[position]
	if budget.EntityCode != payload.OperationAfter.SponsorEntityCode ||
		budget.CommittedUnits != movement.CommittedBeforeUnits ||
		budget.SpentUnits != movement.SpentBeforeUnits ||
		budget.Version != movement.BudgetVersionBefore ||
		movement.CommittedAfterUnits < 0 || movement.SpentAfterUnits < 0 ||
		movement.CommittedAfterUnits > budget.AppropriatedUnits ||
		movement.SpentAfterUnits > budget.AppropriatedUnits-movement.CommittedAfterUnits {
		return fmt.Errorf("facility lifecycle budget projection chain is broken")
	}
	budget.CommittedUnits = movement.CommittedAfterUnits
	budget.SpentUnits = movement.SpentAfterUnits
	budget.Version = movement.BudgetVersionAfter
	return nil
}

func cityFacilityLifecyclePolicyForType(
	state *cityFacilityLifecycleHashState, facilityTypeCode string,
) *CityFacilityLifecyclePolicy {
	if state == nil {
		return nil
	}
	for index := range state.Policies {
		if state.Policies[index].FacilityTypeCode == facilityTypeCode {
			return &state.Policies[index]
		}
	}
	return nil
}

func sameCityFacilityLifecycleFactSource(
	state CityFacilityLifecycleState, fact CityFacilityLifecycleFact,
) bool {
	return state.SourceFactTick != nil && state.SourceFactSequence != nil &&
		*state.SourceFactTick == fact.Tick && *state.SourceFactSequence == fact.Sequence
}

func reduceCityFacilityLifecycleEmbeddedState(
	state *cityFacilityLifecycleHashState,
	before *CityFacilityLifecycleState,
	after CityFacilityLifecycleState,
	fact CityFacilityLifecycleFact,
) error {
	index := cityFacilityLifecycleStateIndex(state.States, after.FacilityCode)
	if index < 0 || before == nil ||
		state.States[index].Version != before.Version ||
		after.Version != before.Version+1 ||
		!sameCityFacilityLifecycleFactSource(after, fact) {
		return fmt.Errorf("embedded facility lifecycle state chain is invalid")
	}
	state.States[index] = after
	return nil
}

func cityFacilityLifecycleStateIndex(items []CityFacilityLifecycleState, code string) int {
	for index := range items {
		if items[index].FacilityCode == code {
			return index
		}
	}
	return -1
}

func cityFacilityOperationIndex(items []CityFacilityOperation, code string) int {
	for index := range items {
		if items[index].Code == code {
			return index
		}
	}
	return -1
}

func cityFacilityStaffAssignmentIndex(items []CityFacilityStaffAssignment, code string) int {
	for index := range items {
		if items[index].Code == code {
			return index
		}
	}
	return -1
}

func cityFacilityIncidentIndex(items []CityFacilityIncident, code string) int {
	for index := range items {
		if items[index].Code == code {
			return index
		}
	}
	return -1
}

func sortCityFacilityLifecycleState(state *cityFacilityLifecycleHashState) {
	if state == nil {
		return
	}
	sort.Slice(state.States, func(i, j int) bool {
		return state.States[i].FacilityCode < state.States[j].FacilityCode
	})
	sort.Slice(state.Operations, func(i, j int) bool {
		return state.Operations[i].Code < state.Operations[j].Code
	})
	sort.Slice(state.StaffAssignments, func(i, j int) bool {
		return state.StaffAssignments[i].Code < state.StaffAssignments[j].Code
	})
	sort.Slice(state.Incidents, func(i, j int) bool {
		return state.Incidents[i].Code < state.Incidents[j].Code
	})
	sort.Slice(state.BudgetMovements, func(i, j int) bool {
		left, right := state.BudgetMovements[i], state.BudgetMovements[j]
		if left.SourceFactTick != right.SourceFactTick {
			return left.SourceFactTick < right.SourceFactTick
		}
		return left.SourceFactSequence < right.SourceFactSequence
	})
	sortCityFacilityLifecycleFacts(state.Facts)
}
