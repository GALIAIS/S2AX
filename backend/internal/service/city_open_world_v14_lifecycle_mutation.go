package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// V14 lifecycle mutations are intentionally isolated from the generic actor
// reducer. They are administrator commands that change only the *effective*
// assignment projection. The older V12 binding and V13 source rows are sealed
// input evidence and are therefore never updated here.

type cityOpenWorldCommuteLifecycleFacilityRecord struct {
	facility cityOpenWorldCommuteSourceFacility
	capacity int64
}

func ensureCityOpenWorldCommuteLifecycleEngine(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) error {
	var simulationVersion string
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version
FROM city_worlds
WHERE id = $1
FOR UPDATE`, worldID).Scan(&simulationVersion); err != nil {
		return fmt.Errorf("lock V14 commute lifecycle world: %w", err)
	}
	if !cityEngineSupportsOpenWorldCommuteLifecycle(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	return nil
}

func loadCityOpenWorldCommuteLifecycleAssignmentForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	actorCode string,
) (*cityOpenWorldCommuteLifecycleAssignmentRecord, error) {
	record := &cityOpenWorldCommuteLifecycleAssignmentRecord{}
	err := tx.QueryRowContext(ctx, `
SELECT epoch.code, epoch.binding_code, actor.id, actor.code, actor.status,
       epoch.epoch_number, epoch.assignment_kind, epoch.employment_role_code,
       epoch.home_facility_code, epoch.home_hub_code, epoch.work_facility_code,
       epoch.work_hub_code, epoch.period_ticks, epoch.outbound_phase, epoch.return_phase,
       epoch.origin_kind, epoch.opened_tick, epoch.metadata,
       latest.state, latest.reason_code
FROM city_open_world_commute_assignment_epochs epoch
JOIN city_open_world_actors actor
  ON actor.id = epoch.actor_id AND actor.world_id = epoch.world_id
JOIN LATERAL (
    SELECT transition.state, transition.reason_code
    FROM city_open_world_commute_assignment_transitions transition
    WHERE transition.world_id = epoch.world_id AND transition.assignment_code = epoch.code
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1
) latest ON TRUE
WHERE epoch.world_id = $1
  AND actor.code = $2
  AND latest.state IN ('active', 'suspended')
ORDER BY epoch.epoch_number DESC
LIMIT 1
FOR UPDATE OF epoch, actor`, worldID, actorCode).Scan(
		&record.assignment.Code, &record.assignment.BindingCode, &record.actorID, &record.actorCode, &record.actorStatus,
		&record.assignment.EpochNumber, &record.assignment.AssignmentKind, &record.assignment.EmploymentRole,
		&record.assignment.HomeFacilityCode, &record.assignment.HomeHubCode, &record.assignment.WorkFacilityCode,
		&record.assignment.WorkHubCode, &record.assignment.PeriodTicks, &record.assignment.OutboundPhase,
		&record.assignment.ReturnPhase, &record.assignment.OriginKind, &record.assignment.OpenedTick,
		&record.assignment.Metadata, &record.state, &record.reasonCode,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCommuteAssignmentNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("lock V14 commute lifecycle assignment for actor %s: %w", actorCode, err)
	}
	record.assignment.ActorCode = record.actorCode
	if !cityOpenWorldCommuteLifecycleTransitionStateValid(record.state) ||
		(record.state != cityOpenWorldCommuteLifecycleStateActive && record.state != cityOpenWorldCommuteLifecycleStateSuspended) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_assignment_state"})
	}
	return record, nil
}

func loadCityOpenWorldCommuteLifecycleFacilityForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	facilityCode string,
) (*cityOpenWorldCommuteLifecycleFacilityRecord, error) {
	record := &cityOpenWorldCommuteLifecycleFacilityRecord{}
	err := tx.QueryRowContext(ctx, `
SELECT facility.code, hub.code, facility.building_code, facility.facility_type_code,
       facility.anchor_x, facility.anchor_y, facility.anchor_z, facility.capacity_units
FROM city_open_world_facilities facility
JOIN city_open_world_mobility_hubs hub
  ON hub.world_id = facility.world_id AND hub.facility_id = facility.id
 AND hub.facility_code = facility.code AND hub.hub_kind = 'facility'
WHERE facility.world_id = $1 AND facility.code = $2 AND facility.state = 'active'
ORDER BY hub.code
LIMIT 1
FOR UPDATE OF facility, hub`, worldID, facilityCode).Scan(
		&record.facility.Code, &record.facility.HubCode, &record.facility.BuildingCode,
		&record.facility.FacilityTypeCode, &record.facility.AnchorX, &record.facility.AnchorY,
		&record.facility.AnchorZ, &record.capacity,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock V14 commute lifecycle facility %s: %w", facilityCode, err)
	}
	if record.capacity < 1 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_facility_capacity"})
	}
	return record, nil
}

func ensureCityOpenWorldCommuteLifecycleTransitionBudget(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldCommuteLifecyclePolicy,
	wanted int64,
) error {
	if policy == nil || wanted < 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_transition_budget"})
	}
	var existing int64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_open_world_commute_assignment_transitions
WHERE world_id = $1 AND transition_tick = $2`, worldID, targetTick).Scan(&existing); err != nil {
		return fmt.Errorf("count V14 commute lifecycle transitions: %w", err)
	}
	if existing+wanted > int64(policy.MaximumTransitionsTick) {
		return cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCommuteTransitionLimit)
	}
	return nil
}

func cityOpenWorldCommuteLifecycleFacilityAssignmentCount(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	facilityCode string,
	home bool,
	excludedAssignmentCode string,
) (int64, error) {
	columnClause := "epoch.work_facility_code = $2"
	if home {
		columnClause = "epoch.home_facility_code = $2"
	}
	query := `
SELECT COUNT(*)
FROM city_open_world_commute_assignment_epochs epoch
JOIN LATERAL (
    SELECT transition.state
    FROM city_open_world_commute_assignment_transitions transition
    WHERE transition.world_id = epoch.world_id AND transition.assignment_code = epoch.code
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1
) latest ON TRUE
WHERE epoch.world_id = $1
  AND ` + columnClause + `
  AND epoch.code <> $3
  AND latest.state IN ('active', 'suspended')`
	var count int64
	if err := tx.QueryRowContext(ctx, query, worldID, facilityCode, excludedAssignmentCode).Scan(&count); err != nil {
		return 0, fmt.Errorf("count V14 commute lifecycle facility assignments: %w", err)
	}
	return count, nil
}

func ensureCityOpenWorldCommuteLifecycleRebindCapacity(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previousAssignmentCode string,
	home, work cityOpenWorldCommuteLifecycleFacilityRecord,
) error {
	homeCount, err := cityOpenWorldCommuteLifecycleFacilityAssignmentCount(
		ctx, tx, worldID, home.facility.Code, true, previousAssignmentCode,
	)
	if err != nil {
		return err
	}
	if homeCount >= home.capacity {
		return cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCommuteCapacity)
	}
	workCount, err := cityOpenWorldCommuteLifecycleFacilityAssignmentCount(
		ctx, tx, worldID, work.facility.Code, false, previousAssignmentCode,
	)
	if err != nil {
		return err
	}
	if workCount >= work.capacity {
		return cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCommuteCapacity)
	}
	return nil
}

func insertCityOpenWorldCommuteLifecycleTransition(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, transitionSequence int64,
	assignmentCode, nextState, reasonCode string,
	fact *cityOpenWorldRuntimeFactRecord,
	previousState string,
) error {
	if fact == nil || fact.id <= 0 || assignmentCode == "" ||
		!cityOpenWorldCommuteLifecycleTransitionStateValid(nextState) || reasonCode == "" {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_transition"})
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldCommuteLifecycleSchemaVersion,
		"automatic":        false,
		"previous_state":   previousState,
		"source_fact_type": fact.fact.FactType,
	})
	if err != nil {
		return fmt.Errorf("marshal V14 commute lifecycle command transition metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_assignment_transitions
    (world_id, assignment_code, transition_tick, transition_sequence, state,
     reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
		worldID, assignmentCode, targetTick, transitionSequence, nextState,
		reasonCode, fact.id, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V14 commute lifecycle command transition: %w", err)
	}
	return nil
}

func cityOpenWorldCommuteLifecycleStateDelta(previous, next string) cityOpenWorldCommuteLifecyclePolicyDelta {
	delta := cityOpenWorldCommuteLifecyclePolicyDelta{transitions: 1}
	switch previous {
	case cityOpenWorldCommuteLifecycleStateActive:
		delta.active--
	case cityOpenWorldCommuteLifecycleStateSuspended:
		delta.suspended--
	}
	switch next {
	case cityOpenWorldCommuteLifecycleStateActive:
		delta.active++
	case cityOpenWorldCommuteLifecycleStateSuspended:
		delta.suspended++
	case cityOpenWorldCommuteLifecycleStateSuperseded:
		delta.superseded++
	case cityOpenWorldCommuteLifecycleStateTerminated:
		delta.terminated++
	}
	return delta
}

func (s *CityEconomyService) setCityOpenWorldCommuteAssignmentState(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldCommuteAssignmentSetStatePayload,
) (cityOpenWorldRuntimeExecution, error) {
	if err := ensureCityOpenWorldCommuteLifecycleEngine(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err := activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err := activateCityOpenWorldCommuteLifecycleWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	policy, err := loadCityOpenWorldCommuteLifecyclePolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = ensureCityOpenWorldCommuteLifecycleTransitionBudget(ctx, tx, worldID, targetTick, policy, 1); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	record, err := loadCityOpenWorldCommuteLifecycleAssignmentForUpdate(ctx, tx, worldID, payload.ActorCode)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if record.state == payload.State {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCommuteStateUnchanged)
	}
	if !cityOpenWorldCommuteLifecycleTransitionAllowed(record.state, payload.State) {
		return cityOpenWorldRuntimeExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_state_transition"})
	}
	if payload.State == cityOpenWorldCommuteLifecycleStateActive {
		operational, _, operationalErr := cityOpenWorldCommuteLifecycleAssignmentOperational(ctx, tx, worldID, record)
		if operationalErr != nil {
			return cityOpenWorldRuntimeExecution{}, operationalErr
		}
		if !operational {
			return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCommuteNotOperational)
		}
	}
	factPayload, err := json.Marshal(map[string]any{
		"schema_version":  cityOpenWorldCommuteLifecycleSchemaVersion,
		"assignment_code": record.assignment.Code,
		"binding_code":    record.assignment.BindingCode,
		"actor_code":      record.actorCode,
		"from_state":      record.state,
		"to_state":        payload.State,
		"reason_code":     payload.ReasonCode,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal V14 commute lifecycle state command fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &record.actorID, factType: cityOpenWorldRuntimeFactCommuteLifecycleStateChanged, payload: factPayload,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = insertCityOpenWorldCommuteLifecycleTransition(
		ctx, tx, worldID, targetTick, factSequence, record.assignment.Code, payload.State,
		payload.ReasonCode, fact, record.state,
	); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldCommuteLifecyclePolicy(
		ctx, tx, worldID, cityOpenWorldCommuteLifecycleStateDelta(record.state, payload.State),
	); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 0, 0); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = assertCityOpenWorldCommuteLifecycleFoundation(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("validate V14 commute lifecycle state command: %w", err)
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.commute_assignment_state_changed", map[string]any{
			"actor_code": record.actorCode, "assignment_code": record.assignment.Code,
			"state": payload.State, "reason_code": payload.ReasonCode,
		}),
		facts: []CityOpenWorldRuntimeFact{fact.fact}, effects: []CityOpenWorldRuntimeEffect{}, cases: []CityOpenWorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence, nextCaseSeq: caseSequence,
	}, nil
}

func insertCityOpenWorldCommuteLifecycleAssignmentEpoch(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	assignment CityOpenWorldCommuteAssignmentEpoch,
	actorID int64,
	fact *cityOpenWorldRuntimeFactRecord,
) error {
	if fact == nil || assignment.OpenedFact == nil || assignment.OpenedFact.Tick != assignment.OpenedTick {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_successor_epoch"})
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_assignment_epochs
    (world_id, code, binding_code, actor_id, epoch_number, assignment_kind,
     employment_role_code, home_facility_code, home_hub_code, work_facility_code,
     work_hub_code, period_ticks, outbound_phase, return_phase, origin_kind,
     opened_tick, opened_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18::jsonb)`,
		worldID, assignment.Code, assignment.BindingCode, actorID, assignment.EpochNumber,
		assignment.AssignmentKind, assignment.EmploymentRole, assignment.HomeFacilityCode,
		assignment.HomeHubCode, assignment.WorkFacilityCode, assignment.WorkHubCode,
		assignment.PeriodTicks, assignment.OutboundPhase, assignment.ReturnPhase,
		assignment.OriginKind, assignment.OpenedTick, fact.id, []byte(assignment.Metadata)); err != nil {
		return fmt.Errorf("insert V14 commute lifecycle successor epoch: %w", err)
	}
	return nil
}

func insertCityOpenWorldCommuteLifecycleSources(
	ctx context.Context,
	tx *sql.Tx,
	worldID, actorID int64,
	sources []CityOpenWorldCommuteLifecycleSource,
	fact *cityOpenWorldRuntimeFactRecord,
) error {
	if fact == nil || len(sources) != 2 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_successor_sources"})
	}
	for _, source := range sources {
		if source.LastFact == nil || source.LastFact.Tick != source.LastTransitionTick {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_successor_source_fact"})
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_lifecycle_sources
    (world_id, code, assignment_code, binding_code, actor_id, source_kind, direction,
     employment_role_code, origin_facility_code, origin_hub_code, destination_facility_code,
     destination_hub_code, mode_code, purpose_code, requested_units, status, period_ticks,
     phase_offset, next_due_tick, last_transition_tick, last_fact_id, generated_count,
     suppressed_count, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25::jsonb)`,
			worldID, source.Code, source.AssignmentCode, source.BindingCode, actorID,
			source.SourceKind, source.Direction, source.EmploymentRoleCode,
			source.OriginFacilityCode, source.OriginHubCode, source.DestinationFacilityCode,
			source.DestinationHubCode, source.ModeCode, source.PurposeCode, source.RequestedUnits,
			source.Status, source.PeriodTicks, source.PhaseOffset, source.NextDueTick,
			source.LastTransitionTick, fact.id, source.GeneratedCount, source.SuppressedCount,
			source.Version, []byte(source.Metadata)); err != nil {
			return fmt.Errorf("insert V14 commute lifecycle successor source %s: %w", source.Code, err)
		}
	}
	return nil
}

func (s *CityEconomyService) rebindCityOpenWorldCommuteAssignment(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldCommuteAssignmentRebindPayload,
) (cityOpenWorldRuntimeExecution, error) {
	if err := ensureCityOpenWorldCommuteLifecycleEngine(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err := activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err := activateCityOpenWorldCommuteLifecycleWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	policy, err := loadCityOpenWorldCommuteLifecyclePolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if policy.AssignmentCount >= int64(policy.MaximumAssignments) {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCommuteAssignmentLimit)
	}
	if err = ensureCityOpenWorldCommuteLifecycleTransitionBudget(ctx, tx, worldID, targetTick, policy, 2); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	previous, err := loadCityOpenWorldCommuteLifecycleAssignmentForUpdate(ctx, tx, worldID, payload.ActorCode)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}

	// Lock shared facility capacity in code order. This makes competing admin
	// rebinds serializable without relying on a best-effort count check.
	facilityCodes := []string{payload.HomeFacilityCode, payload.WorkFacilityCode}
	if facilityCodes[1] < facilityCodes[0] {
		facilityCodes[0], facilityCodes[1] = facilityCodes[1], facilityCodes[0]
	}
	lockedFacilities := make(map[string]cityOpenWorldCommuteLifecycleFacilityRecord, 2)
	for _, code := range facilityCodes {
		facility, facilityErr := loadCityOpenWorldCommuteLifecycleFacilityForUpdate(ctx, tx, worldID, code)
		if facilityErr != nil {
			return cityOpenWorldRuntimeExecution{}, facilityErr
		}
		if facility == nil {
			return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCommuteFacility)
		}
		lockedFacilities[code] = *facility
	}
	home, work := lockedFacilities[payload.HomeFacilityCode], lockedFacilities[payload.WorkFacilityCode]
	if !cityOpenWorldCommuteSourceFacilityMatchesDirection(cityOpenWorldCommuteSourceDirectionOutbound, home.facility, true) ||
		!cityOpenWorldCommuteSourceFacilityMatchesDirection(cityOpenWorldCommuteSourceDirectionOutbound, work.facility, false) {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCommuteFacility)
	}
	if err = ensureCityOpenWorldCommuteLifecycleRebindCapacity(ctx, tx, worldID, previous.assignment.Code, home, work); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	phase := previous.assignment.OutboundPhase
	if payload.OutboundPhase != nil {
		phase = *payload.OutboundPhase
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":           cityOpenWorldCommuteLifecycleSchemaVersion,
		"origin":                   cityOpenWorldCommuteLifecycleOriginAdminRebind,
		"replaces_assignment_code": previous.assignment.Code,
		"source_command_sequence":  command.Sequence,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal V14 commute lifecycle successor metadata: %w", err)
	}
	successor := CityOpenWorldCommuteAssignmentEpoch{
		Code:             cityOpenWorldCommuteAssignmentEpochCode(previous.assignment.BindingCode, previous.assignment.EpochNumber+1),
		BindingCode:      previous.assignment.BindingCode,
		ActorCode:        previous.actorCode,
		EpochNumber:      previous.assignment.EpochNumber + 1,
		AssignmentKind:   cityOpenWorldCommuteLifecycleAssignmentKind,
		EmploymentRole:   payload.EmploymentRoleCode,
		HomeFacilityCode: home.facility.Code,
		HomeHubCode:      home.facility.HubCode,
		WorkFacilityCode: work.facility.Code,
		WorkHubCode:      work.facility.HubCode,
		PeriodTicks:      cityOpenWorldCommutePeriodTicks,
		OutboundPhase:    phase,
		ReturnPhase:      (phase + cityOpenWorldCommutePeriodTicks/2) % cityOpenWorldCommutePeriodTicks,
		OriginKind:       cityOpenWorldCommuteLifecycleOriginAdminRebind,
		OpenedTick:       targetTick,
		Metadata:         metadata,
	}
	candidate := &cityOpenWorldCommuteLifecycleAssignmentRecord{
		assignment: successor, actorID: previous.actorID, actorCode: previous.actorCode,
		actorStatus: previous.actorStatus, state: cityOpenWorldCommuteLifecycleStateActive,
	}
	operational, _, operationalErr := cityOpenWorldCommuteLifecycleAssignmentOperational(ctx, tx, worldID, candidate)
	if operationalErr != nil {
		return cityOpenWorldRuntimeExecution{}, operationalErr
	}
	if !operational {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCommuteNotOperational)
	}
	mode, err := loadCityOpenWorldMobilityMode(ctx, tx, worldID, cityOpenWorldCommuteSourceModeWalk)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if mode == nil {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionCommuteFacility)
	}
	factPayload, err := json.Marshal(map[string]any{
		"schema_version":            cityOpenWorldCommuteLifecycleSchemaVersion,
		"actor_code":                previous.actorCode,
		"binding_code":              previous.assignment.BindingCode,
		"previous_assignment_code":  previous.assignment.Code,
		"successor_assignment_code": successor.Code,
		"previous_state":            previous.state,
		"employment_role_code":      successor.EmploymentRole,
		"home_facility_code":        successor.HomeFacilityCode,
		"home_hub_code":             successor.HomeHubCode,
		"work_facility_code":        successor.WorkFacilityCode,
		"work_hub_code":             successor.WorkHubCode,
		"outbound_phase":            successor.OutboundPhase,
		"return_phase":              successor.ReturnPhase,
		"reason_code":               payload.ReasonCode,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal V14 commute lifecycle rebind fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &previous.actorID, factType: cityOpenWorldRuntimeFactCommuteLifecycleRebound, payload: factPayload,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = insertCityOpenWorldCommuteLifecycleTransition(
		ctx, tx, worldID, targetTick, factSequence, previous.assignment.Code,
		cityOpenWorldCommuteLifecycleStateSuperseded, payload.ReasonCode, fact, previous.state,
	); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	successor.OpenedFact = &CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence}
	if err = insertCityOpenWorldCommuteLifecycleAssignmentEpoch(ctx, tx, worldID, successor, previous.actorID, fact); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = insertCityOpenWorldCommuteLifecycleTransition(
		ctx, tx, worldID, targetTick, factSequence, successor.Code,
		cityOpenWorldCommuteLifecycleStateActive, payload.ReasonCode, fact, "",
	); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	sources := cityOpenWorldCommuteLifecycleSourcesForAssignment(successor, targetTick)
	for index := range sources {
		sources[index].LastFact = &CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence}
		sources[index].LastTransitionTick = targetTick
		sources[index].NextDueTick = targetTick + 1 + sources[index].PhaseOffset
	}
	if err = insertCityOpenWorldCommuteLifecycleSources(ctx, tx, worldID, previous.actorID, sources, fact); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	delta := cityOpenWorldCommuteLifecycleStateDelta(previous.state, cityOpenWorldCommuteLifecycleStateSuperseded)
	delta.assignments++
	delta.sources += int64(len(sources))
	delta.transitions++ // successor's initial active transition.
	delta.active++
	if err = updateCityOpenWorldCommuteLifecyclePolicy(ctx, tx, worldID, delta); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 0, 0); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = assertCityOpenWorldCommuteLifecycleFoundation(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("validate V14 commute lifecycle rebind command: %w", err)
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.commute_assignment_rebound", map[string]any{
			"actor_code": previous.actorCode, "previous_assignment_code": previous.assignment.Code,
			"assignment_code": successor.Code, "home_facility_code": successor.HomeFacilityCode,
			"work_facility_code": successor.WorkFacilityCode, "employment_role_code": successor.EmploymentRole,
		}),
		facts: []CityOpenWorldRuntimeFact{fact.fact}, effects: []CityOpenWorldRuntimeEffect{}, cases: []CityOpenWorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence, nextCaseSeq: caseSequence,
	}, nil
}
