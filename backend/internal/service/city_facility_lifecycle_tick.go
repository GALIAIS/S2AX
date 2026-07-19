package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
)

type cityFacilityLifecycleAutomaticExecution struct {
	facts            []CityFacilityLifecycleFact
	events           []cityPendingEvent
	nextFactSequence int64
}

func newCityFacilityLifecycleAutomaticExecution(sequence int64) cityFacilityLifecycleAutomaticExecution {
	return cityFacilityLifecycleAutomaticExecution{
		facts:            make([]CityFacilityLifecycleFact, 0),
		events:           make([]cityPendingEvent, 0),
		nextFactSequence: sequence,
	}
}

func enableAutomaticCityFacilityLifecycle(
	ctx context.Context, tx *sql.Tx, worldID int64,
) error {
	if _, err := tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_facility_lifecycle_auto_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10)); err != nil {
		return fmt.Errorf("enable automatic city facility lifecycle: %w", err)
	}
	return nil
}

func loadActiveCityFacilityOperationCodes(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT operation.code
FROM city_facility_operations operation
JOIN city_facilities facility ON facility.id = operation.facility_id
WHERE operation.world_id = $1 AND operation.status = 'active'
ORDER BY facility.code, operation.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load active city facility operations: %w", err)
	}
	codes := make([]string, 0)
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			_ = rows.Close()
			return nil, err
		}
		codes = append(codes, code)
	}
	if err = closeCityRows(rows, "iterate active city facility operations"); err != nil {
		return nil, err
	}
	return codes, nil
}

func advanceCityFacilityOperations(
	ctx context.Context, tx *sql.Tx,
	worldID, targetTick, factSequence int64,
) (cityFacilityLifecycleAutomaticExecution, error) {
	execution := newCityFacilityLifecycleAutomaticExecution(factSequence)
	if err := enableAutomaticCityFacilityLifecycle(ctx, tx, worldID); err != nil {
		return execution, err
	}
	codes, err := loadActiveCityFacilityOperationCodes(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	for _, code := range codes {
		operationRef, loadErr := loadCityFacilityOperationRef(ctx, tx, worldID, code, true)
		if loadErr != nil {
			return execution, loadErr
		}
		operation := operationRef.value
		if operation.Status != CityFacilityOperationStatusActive || operation.StartedTick == nil ||
			operation.DurationTicks <= 0 || targetTick < *operation.StartedTick {
			return execution, ErrCitySimulationInvariant.WithMetadata(
				map[string]string{"facility_operation_code": operation.Code},
			)
		}
		elapsed := targetTick - *operation.StartedTick + 1
		if elapsed <= 0 || elapsed > math.MaxInt64/1000 {
			return execution, ErrCitySimulationInvariant.WithMetadata(
				map[string]string{"field": "facility_operation_elapsed"},
			)
		}
		progress := int(elapsed * 1000 / operation.DurationTicks)
		if progress > 1000 {
			progress = 1000
		}
		if progress <= operation.ProgressMilli {
			continue
		}
		if progress < 1000 {
			fact, progressErr := progressCityFacilityOperation(
				ctx, tx, worldID, targetTick, execution.nextFactSequence,
				operationRef, progress,
			)
			if progressErr != nil {
				return execution, progressErr
			}
			execution.facts = append(execution.facts, fact.fact)
			execution.events = append(execution.events, cityPendingEvent{
				eventType: "city.facility.operation.progressed",
				payload: map[string]any{
					"operation_code": operation.Code,
					"facility_code":  operation.FacilityCode,
					"progress_milli": progress,
				},
			})
			execution.nextFactSequence++
			continue
		}
		completion, completionErr := completeCityFacilityOperation(
			ctx, tx, worldID, targetTick, execution.nextFactSequence, operationRef,
		)
		if completionErr != nil {
			return execution, completionErr
		}
		execution.facts = append(execution.facts, completion.facts...)
		execution.events = append(execution.events, completion.events...)
		execution.nextFactSequence = completion.nextFactSequence
	}
	return execution, nil
}

func progressCityFacilityOperation(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, sequence int64,
	operationRef *cityFacilityOperationRef, progress int,
) (*cityFacilityLifecycleFactRecord, error) {
	before := operationRef.value
	after := before
	after.ProgressMilli = progress
	after.UpdatedTick = targetTick
	after.Version++
	after.SourceFactTick = targetTick
	after.SourceFactSequence = sequence
	fact, err := insertCityFacilityLifecycleFact(ctx, tx, cityFacilityLifecycleFactInsert{
		worldID: worldID, tick: targetTick, sequence: sequence,
		phase:       CityFacilityLifecyclePhasePreService,
		factType:    CityFacilityLifecycleFactOperationProgressed,
		subjectKind: "operation", subjectCode: before.Code,
		versionBefore: before.Version, versionAfter: after.Version,
		payload: map[string]any{
			"schema_version": 1, "operation_before": before,
			"operation_after": after,
		},
	})
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_facility_operations
SET progress_milli = $3, updated_tick = $4,
    version = version + 1, source_fact_id = $5, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'active' AND version = $6`,
		worldID, operationRef.id, progress, targetTick, fact.id, before.Version)
	if err != nil {
		return nil, fmt.Errorf("progress city facility operation: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, before.Code); err != nil {
		return nil, err
	}
	if err = advanceCityFacilityLifecycleProfile(ctx, tx, worldID,
		cityFacilityLifecycleProfileDeltas{}); err != nil {
		return nil, err
	}
	if err = postCityFacilityLifecycleFact(ctx, tx, fact.id); err != nil {
		return nil, err
	}
	return fact, nil
}

func cityFacilityCompletedLifecycleState(
	before CityFacilityLifecycleState, policy CityFacilityLifecyclePolicy,
	operationType string, targetTick int64,
) CityFacilityLifecycleState {
	after := before
	after.ActiveOperationCode = nil
	after.OperationFactorMilli = 1000
	switch operationType {
	case CityFacilityOperationCommission:
		after.LifecycleStatus = CityFacilityLifecycleStatusOperational
		after.ConditionMilli = 1000
	case CityFacilityOperationMaintenance:
		after.LifecycleStatus = CityFacilityLifecycleStatusOperational
		if after.ConditionMilli < policy.MaintenanceRestoreMilli {
			after.ConditionMilli = policy.MaintenanceRestoreMilli
		}
	case CityFacilityOperationRepair:
		after.LifecycleStatus = CityFacilityLifecycleStatusOperational
		if after.ConditionMilli < policy.RepairRestoreMilli {
			after.ConditionMilli = policy.RepairRestoreMilli
		}
	case CityFacilityOperationDecommission:
		after.LifecycleStatus = CityFacilityLifecycleStatusRetired
		after.OperationFactorMilli = 0
	}
	if operationType != CityFacilityOperationDecommission {
		maintenanceTick := targetTick
		after.LastMaintenanceTick = &maintenanceTick
		if targetTick <= math.MaxInt64-policy.MaintenanceIntervalTicks {
			after.MaintenanceDueTick = targetTick + policy.MaintenanceIntervalTicks
		}
	}
	after.EffectiveFactorMilli = cityFacilityLifecycleEffectiveFactor(
		after.LifecycleStatus, after.ConditionMilli,
		after.StaffingFactorMilli, after.OperationFactorMilli,
	)
	after.UpdatedTick = targetTick
	after.Version++
	return after
}

func completeCityFacilityOperation(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, sequence int64,
	operationRef *cityFacilityOperationRef,
) (cityFacilityLifecycleAutomaticExecution, error) {
	execution := newCityFacilityLifecycleAutomaticExecution(sequence)
	operation := operationRef.value
	ref, err := loadCityFacilityLifecycleRef(
		ctx, tx, worldID, operation.FacilityCode, true,
	)
	if err != nil {
		return execution, err
	}
	if ref.state.ActiveOperationCode == nil ||
		*ref.state.ActiveOperationCode != operation.Code {
		return execution, ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"facility_operation_code": operation.Code},
		)
	}
	beforeState := ref.state
	afterState := cityFacilityCompletedLifecycleState(
		beforeState, ref.policy, operation.OperationType, targetTick,
	)
	afterState.SourceFactTick = &targetTick
	afterState.SourceFactSequence = &sequence
	completedTick := targetTick
	afterOperation := operation
	afterOperation.Status = CityFacilityOperationStatusCompleted
	afterOperation.ProgressMilli = 1000
	afterOperation.CompletedTick = &completedTick
	afterOperation.UpdatedTick = targetTick
	afterOperation.Version++
	afterOperation.SourceFactTick = targetTick
	afterOperation.SourceFactSequence = sequence
	fact, err := insertCityFacilityLifecycleFact(ctx, tx, cityFacilityLifecycleFactInsert{
		worldID: worldID, tick: targetTick, sequence: sequence,
		phase:       CityFacilityLifecyclePhasePreService,
		factType:    CityFacilityLifecycleFactOperationCompleted,
		subjectKind: "operation", subjectCode: operation.Code,
		versionBefore: operation.Version, versionAfter: afterOperation.Version,
		payload: map[string]any{
			"schema_version": 1, "operation_before": operation,
			"operation_after": afterOperation, "state_before": beforeState,
			"state_after": afterState,
		},
	})
	if err != nil {
		return execution, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_facility_operations
SET status = 'completed', progress_milli = 1000, completed_tick = $3,
    updated_tick = $3, version = version + 1,
    source_fact_id = $4, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'active' AND version = $5`,
		worldID, operationRef.id, targetTick, fact.id, operation.Version)
	if err != nil {
		return execution, fmt.Errorf("complete city facility operation: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, operation.Code); err != nil {
		return execution, err
	}
	result, err = tx.ExecContext(ctx, `
UPDATE city_facility_lifecycle_states
SET lifecycle_status = $3, condition_milli = $4,
    operation_factor_milli = $5, effective_factor_milli = $6,
    last_maintenance_tick = $7, maintenance_due_tick = $8,
    active_operation_code = NULL, updated_tick = $9,
    version = version + 1, source_fact_id = $10, updated_at = NOW()
WHERE world_id = $1 AND facility_id = $2 AND version = $11
  AND active_operation_code = $12`, worldID, ref.facilityID,
		afterState.LifecycleStatus, afterState.ConditionMilli,
		afterState.OperationFactorMilli, afterState.EffectiveFactorMilli,
		optionalInt64Value(afterState.LastMaintenanceTick), afterState.MaintenanceDueTick,
		targetTick, fact.id, beforeState.Version, operation.Code)
	if err != nil {
		return execution, fmt.Errorf("complete city facility lifecycle state: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, operation.FacilityCode); err != nil {
		return execution, err
	}
	if err = advanceCityFacilityLifecycleProfile(ctx, tx, worldID,
		cityFacilityLifecycleProfileDeltas{}); err != nil {
		return execution, err
	}
	if err = postCityFacilityLifecycleFact(ctx, tx, fact.id); err != nil {
		return execution, err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.events = append(execution.events, cityPendingEvent{
		eventType: "city.facility.operation.completed",
		payload: map[string]any{
			"operation_code": operation.Code, "facility_code": operation.FacilityCode,
			"operation_type":   operation.OperationType,
			"lifecycle_status": afterState.LifecycleStatus,
			"condition_milli":  afterState.ConditionMilli,
		},
	})
	execution.nextFactSequence++
	if operation.OperationType == CityFacilityOperationRepair {
		resolution, resolveErr := resolveCityFacilityIncident(
			ctx, tx, worldID, targetTick, execution.nextFactSequence,
			operation, afterState,
		)
		if resolveErr != nil {
			return execution, resolveErr
		}
		execution.facts = append(execution.facts, resolution.fact)
		execution.events = append(execution.events, cityPendingEvent{
			eventType: "city.facility.incident.resolved",
			payload: map[string]any{
				"facility_code":  operation.FacilityCode,
				"operation_code": operation.Code,
				"incident_code":  resolution.fact.SubjectCode,
			},
		})
		execution.nextFactSequence++
	}
	return execution, nil
}

func resolveCityFacilityIncident(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, sequence int64,
	operation CityFacilityOperation, state CityFacilityLifecycleState,
) (*cityFacilityLifecycleFactRecord, error) {
	if state.OpenIncidentCode == nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"facility_code": operation.FacilityCode},
		)
	}
	beforeIncident := CityFacilityIncident{}
	var incidentID int64
	var resolved sql.NullInt64
	var repair sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT incident.id, incident.code, facility.code, incident.status,
       incident.severity_milli, incident.condition_before_milli,
       incident.failure_probability_ppm, incident.sample_value_ppm,
       incident.prng_proof, incident.opened_tick, incident.resolved_tick,
       incident.repair_operation_code, incident.version,
       source.tick, source.sequence, incident.metadata
FROM city_facility_incidents incident
JOIN city_facilities facility ON facility.id = incident.facility_id
JOIN city_facility_lifecycle_facts source ON source.id = incident.source_fact_id
WHERE incident.world_id = $1 AND incident.code = $2 AND incident.status = 'open'
FOR UPDATE OF incident`, worldID, *state.OpenIncidentCode).Scan(
		&incidentID, &beforeIncident.Code, &beforeIncident.FacilityCode,
		&beforeIncident.Status, &beforeIncident.SeverityMilli,
		&beforeIncident.ConditionBeforeMilli,
		&beforeIncident.FailureProbabilityPPM, &beforeIncident.SampleValuePPM,
		&beforeIncident.PRNGProof, &beforeIncident.OpenedTick, &resolved,
		&repair, &beforeIncident.Version, &beforeIncident.SourceFactTick,
		&beforeIncident.SourceFactSequence, &beforeIncident.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"incident_code": *state.OpenIncidentCode},
		)
	}
	if err != nil {
		return nil, fmt.Errorf("load city facility incident for resolution: %w", err)
	}
	afterIncident := beforeIncident
	afterIncident.Status = "resolved"
	afterIncident.ResolvedTick = &targetTick
	afterIncident.RepairOperationCode = &operation.Code
	afterIncident.Version++
	afterIncident.SourceFactTick = targetTick
	afterIncident.SourceFactSequence = sequence
	beforeState := state
	afterState := state
	afterState.OpenIncidentCode = nil
	afterState.UpdatedTick = targetTick
	afterState.Version++
	afterState.SourceFactTick = &targetTick
	afterState.SourceFactSequence = &sequence
	fact, err := insertCityFacilityLifecycleFact(ctx, tx, cityFacilityLifecycleFactInsert{
		worldID: worldID, tick: targetTick, sequence: sequence,
		phase:       CityFacilityLifecyclePhasePreService,
		factType:    CityFacilityLifecycleFactIncidentResolved,
		subjectKind: "incident", subjectCode: beforeIncident.Code,
		versionBefore: beforeIncident.Version, versionAfter: afterIncident.Version,
		payload: map[string]any{
			"schema_version": 1, "incident_before": beforeIncident,
			"incident_after": afterIncident, "state_before": beforeState,
			"state_after": afterState,
		},
	})
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_facility_incidents
SET status = 'resolved', resolved_tick = $3,
    repair_operation_code = $4, version = version + 1,
    source_fact_id = $5, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'open' AND version = $6`,
		worldID, incidentID, targetTick, operation.Code, fact.id, beforeIncident.Version)
	if err != nil {
		return nil, fmt.Errorf("resolve city facility incident: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, beforeIncident.Code); err != nil {
		return nil, err
	}
	result, err = tx.ExecContext(ctx, `
UPDATE city_facility_lifecycle_states
SET open_incident_code = NULL, updated_tick = $2,
    version = version + 1, source_fact_id = $3, updated_at = NOW()
WHERE world_id = $1 AND facility_id = (
    SELECT id FROM city_facilities WHERE world_id = $1 AND code = $4
) AND version = $5 AND open_incident_code = $6`, worldID,
		targetTick, fact.id, operation.FacilityCode, state.Version,
		beforeIncident.Code)
	if err != nil {
		return nil, fmt.Errorf("clear resolved city facility incident: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, beforeIncident.Code); err != nil {
		return nil, err
	}
	if err = advanceCityFacilityLifecycleProfile(ctx, tx, worldID,
		cityFacilityLifecycleProfileDeltas{}); err != nil {
		return nil, err
	}
	if err = postCityFacilityLifecycleFact(ctx, tx, fact.id); err != nil {
		return nil, err
	}
	return fact, nil
}

func loadCityFacilityServiceUtilization(
	ctx context.Context, queryer citySQLQueryer,
	worldID, targetTick int64, ref *cityFacilityLifecycleRef,
) (effectiveCapacity, dispatched int64, err error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT facility.status, capacity.available_capacity_units
FROM city_facility_service_capacities capacity
JOIN city_facilities facility ON facility.id = capacity.facility_id
WHERE capacity.world_id = $1 AND capacity.facility_id = $2
ORDER BY capacity.id`, worldID, ref.facilityID)
	if err != nil {
		return 0, 0, fmt.Errorf("load city facility service capacities for wear: %w", err)
	}
	for rows.Next() {
		var status string
		var available int64
		if err = rows.Scan(&status, &available); err != nil {
			_ = rows.Close()
			return 0, 0, err
		}
		capacity := cityFacilityEffectiveDispatchCapacity(
			status, available, ref.state.EffectiveFactorMilli,
		)
		if effectiveCapacity > math.MaxInt64-capacity {
			_ = rows.Close()
			return 0, 0, ErrCitySimulationInvariant.WithMetadata(
				map[string]string{"field": "facility_effective_capacity"},
			)
		}
		effectiveCapacity += capacity
	}
	if err = closeCityRows(rows, "iterate city facility service capacities for wear"); err != nil {
		return 0, 0, err
	}
	err = queryer.QueryRowContext(ctx, `
SELECT COALESCE(SUM(allocation.dispatched_units), 0)::BIGINT
FROM city_service_allocations allocation
WHERE allocation.world_id = $1 AND allocation.tick = $2
  AND allocation.facility_id = $3`, worldID, targetTick, ref.facilityID).Scan(&dispatched)
	if err != nil {
		return 0, 0, fmt.Errorf("load city facility dispatched service for wear: %w", err)
	}
	return effectiveCapacity, dispatched, nil
}

func settleCityFacilityWearAndFailures(
	ctx context.Context, tx *sql.Tx,
	worldID, targetTick, factSequence int64,
) (cityFacilityLifecycleAutomaticExecution, error) {
	execution := newCityFacilityLifecycleAutomaticExecution(factSequence)
	if err := enableAutomaticCityFacilityLifecycle(ctx, tx, worldID); err != nil {
		return execution, err
	}
	var version string
	var seed int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, seed FROM city_worlds WHERE id = $1`, worldID).Scan(
		&version, &seed,
	); err != nil {
		return execution, fmt.Errorf("load city facility failure seed: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
SELECT facility.code
FROM city_facility_lifecycle_states state
JOIN city_facilities facility ON facility.id = state.facility_id
WHERE state.world_id = $1 AND state.lifecycle_status = 'operational'
ORDER BY facility.code`, worldID)
	if err != nil {
		return execution, fmt.Errorf("load operational city facilities for wear: %w", err)
	}
	codes := make([]string, 0)
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			_ = rows.Close()
			return execution, err
		}
		codes = append(codes, code)
	}
	if err = closeCityRows(rows, "iterate operational city facilities for wear"); err != nil {
		return execution, err
	}
	for _, code := range codes {
		ref, loadErr := loadCityFacilityLifecycleRef(ctx, tx, worldID, code, true)
		if loadErr != nil {
			return execution, loadErr
		}
		if ref.state.LifecycleStatus != CityFacilityLifecycleStatusOperational {
			continue
		}
		effectiveCapacity, dispatched, utilizationErr := loadCityFacilityServiceUtilization(
			ctx, tx, worldID, targetTick, ref,
		)
		if utilizationErr != nil {
			return execution, utilizationErr
		}
		utilizationMilli := int64(0)
		if effectiveCapacity > 0 {
			utilizationMilli, utilizationErr = cityMulDivFloor(dispatched, 1000, effectiveCapacity)
			if utilizationErr != nil {
				return execution, utilizationErr
			}
			if utilizationMilli > 1000 {
				utilizationMilli = 1000
			}
		}
		var policySeed cityFacilityLifecyclePolicySeed
		if err = json.Unmarshal(ref.policy.Payload, &policySeed); err != nil {
			return execution, fmt.Errorf("decode city facility wear policy: %w", err)
		}
		overdueTicks := int64(0)
		if targetTick > ref.state.MaintenanceDueTick {
			overdueTicks = targetTick - ref.state.MaintenanceDueTick
			if overdueTicks > policySeed.MaximumOverdueTicks {
				overdueTicks = policySeed.MaximumOverdueTicks
			}
		}
		utilizationDecay := int64(ref.policy.UtilizationDecayMilli) * utilizationMilli / 1000
		overdueDecay := int64(ref.policy.OverdueDecayMilli) * overdueTicks
		decay := int64(ref.policy.BaseDecayMilli) + utilizationDecay + overdueDecay
		if decay > 1000 {
			decay = 1000
		}
		conditionAfter := ref.state.ConditionMilli - int(decay)
		if conditionAfter < 0 {
			conditionAfter = 0
		}
		if conditionAfter != ref.state.ConditionMilli {
			fact, conditionErr := changeCityFacilityCondition(
				ctx, tx, worldID, targetTick, execution.nextFactSequence,
				ref, conditionAfter, int(utilizationMilli), overdueTicks, decay,
			)
			if conditionErr != nil {
				return execution, conditionErr
			}
			execution.facts = append(execution.facts, fact.fact)
			execution.events = append(execution.events, cityPendingEvent{
				eventType: "city.facility.condition.changed",
				payload: map[string]any{
					"facility_code": code, "condition_milli": conditionAfter,
					"decay_milli": decay, "utilization_milli": utilizationMilli,
				},
			})
			execution.nextFactSequence++
			ref.state.ConditionMilli = conditionAfter
			ref.state.EffectiveFactorMilli = cityFacilityLifecycleEffectiveFactor(
				ref.state.LifecycleStatus, conditionAfter,
				ref.state.StaffingFactorMilli, ref.state.OperationFactorMilli,
			)
			ref.state.UpdatedTick = targetTick
			ref.state.Version++
			ref.state.SourceFactTick = &targetTick
			conditionSequence := execution.nextFactSequence - 1
			ref.state.SourceFactSequence = &conditionSequence
		}
		if ref.state.OpenIncidentCode != nil {
			continue
		}
		probability := int64(ref.policy.BaseFailurePPM)
		if ref.state.ConditionMilli < ref.policy.FailureThresholdMilli {
			penalty := int64(ref.policy.FailureThresholdMilli-ref.state.ConditionMilli) *
				int64(ref.policy.ConditionFailurePPM)
			if penalty > 1_000_000-probability {
				probability = 1_000_000
			} else {
				probability += penalty
			}
		}
		sample, proof := deriveCityFacilityFailureSample(
			version, seed, targetTick, code, ref.policy.PolicyHash,
			ref.state.FailureCount,
		)
		if sample >= int(probability) {
			continue
		}
		fact, incident, incidentErr := openCityFacilityIncident(
			ctx, tx, worldID, targetTick, execution.nextFactSequence,
			ref, int(probability), sample, proof,
		)
		if incidentErr != nil {
			return execution, incidentErr
		}
		execution.facts = append(execution.facts, fact.fact)
		execution.events = append(execution.events, cityPendingEvent{
			eventType: "city.facility.incident.opened",
			payload: map[string]any{
				"facility_code": code, "incident_code": incident.Code,
				"severity_milli":          incident.SeverityMilli,
				"failure_probability_ppm": probability,
				"sample_value_ppm":        sample,
			},
		})
		execution.nextFactSequence++
	}
	return execution, nil
}

func changeCityFacilityCondition(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, sequence int64,
	ref *cityFacilityLifecycleRef, conditionAfter, utilizationMilli int,
	overdueTicks, decay int64,
) (*cityFacilityLifecycleFactRecord, error) {
	before := ref.state
	after := before
	after.ConditionMilli = conditionAfter
	after.EffectiveFactorMilli = cityFacilityLifecycleEffectiveFactor(
		after.LifecycleStatus, after.ConditionMilli,
		after.StaffingFactorMilli, after.OperationFactorMilli,
	)
	after.UpdatedTick = targetTick
	after.Version++
	after.SourceFactTick = &targetTick
	after.SourceFactSequence = &sequence
	fact, err := insertCityFacilityLifecycleFact(ctx, tx, cityFacilityLifecycleFactInsert{
		worldID: worldID, tick: targetTick, sequence: sequence,
		phase:       CityFacilityLifecyclePhasePostService,
		factType:    CityFacilityLifecycleFactConditionChanged,
		subjectKind: "facility", subjectCode: before.FacilityCode,
		versionBefore: before.Version, versionAfter: after.Version,
		payload: map[string]any{
			"schema_version": 1, "state_before": before, "state_after": after,
			"utilization_milli": utilizationMilli, "overdue_ticks": overdueTicks,
			"decay_milli": decay,
		},
	})
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_facility_lifecycle_states
SET condition_milli = $3, effective_factor_milli = $4,
    updated_tick = $5, version = version + 1,
    source_fact_id = $6, updated_at = NOW()
WHERE world_id = $1 AND facility_id = $2 AND version = $7`,
		worldID, ref.facilityID, after.ConditionMilli,
		after.EffectiveFactorMilli, targetTick, fact.id, before.Version)
	if err != nil {
		return nil, fmt.Errorf("change city facility condition: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, before.FacilityCode); err != nil {
		return nil, err
	}
	if err = advanceCityFacilityLifecycleProfile(ctx, tx, worldID,
		cityFacilityLifecycleProfileDeltas{}); err != nil {
		return nil, err
	}
	if err = postCityFacilityLifecycleFact(ctx, tx, fact.id); err != nil {
		return nil, err
	}
	return fact, nil
}

func openCityFacilityIncident(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, sequence int64,
	ref *cityFacilityLifecycleRef, probability, sample int, proof string,
) (*cityFacilityLifecycleFactRecord, CityFacilityIncident, error) {
	beforeState := ref.state
	incidentCode := cityFacilityIncidentCode(
		beforeState.FacilityCode, targetTick, beforeState.FailureCount+1,
	)
	severity := 1000 - beforeState.ConditionMilli
	if severity < 1 {
		severity = 1
	}
	incident := CityFacilityIncident{
		Code: incidentCode, FacilityCode: beforeState.FacilityCode,
		Status: "open", SeverityMilli: severity,
		ConditionBeforeMilli:  beforeState.ConditionMilli,
		FailureProbabilityPPM: probability, SampleValuePPM: sample,
		PRNGProof: proof, OpenedTick: targetTick, Version: 1,
		SourceFactTick: targetTick, SourceFactSequence: sequence,
		Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	afterState := beforeState
	afterState.LifecycleStatus = CityFacilityLifecycleStatusFailed
	afterState.OpenIncidentCode = &incidentCode
	afterState.FailureCount++
	afterState.EffectiveFactorMilli = 0
	afterState.UpdatedTick = targetTick
	afterState.Version++
	afterState.SourceFactTick = &targetTick
	afterState.SourceFactSequence = &sequence
	fact, err := insertCityFacilityLifecycleFact(ctx, tx, cityFacilityLifecycleFactInsert{
		worldID: worldID, tick: targetTick, sequence: sequence,
		phase:       CityFacilityLifecyclePhasePostService,
		factType:    CityFacilityLifecycleFactIncidentOpened,
		subjectKind: "incident", subjectCode: incidentCode,
		versionBefore: 0, versionAfter: 1,
		payload: map[string]any{
			"schema_version": 1, "incident_after": incident,
			"state_before": beforeState, "state_after": afterState,
		},
	})
	if err != nil {
		return nil, CityFacilityIncident{}, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_facility_incidents
    (world_id, code, facility_id, status, severity_milli,
     condition_before_milli, failure_probability_ppm, sample_value_ppm,
     prng_proof, opened_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, 'open', $4, $5, $6, $7, $8, $9, 1, $10, $11::jsonb)`,
		worldID, incident.Code, ref.facilityID, incident.SeverityMilli,
		incident.ConditionBeforeMilli, incident.FailureProbabilityPPM,
		incident.SampleValuePPM, incident.PRNGProof, targetTick,
		fact.id, incident.Metadata); err != nil {
		return nil, CityFacilityIncident{}, fmt.Errorf("open city facility incident: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_facility_lifecycle_states
SET lifecycle_status = 'failed', open_incident_code = $3,
    failure_count = failure_count + 1, effective_factor_milli = 0,
    updated_tick = $4, version = version + 1,
    source_fact_id = $5, updated_at = NOW()
WHERE world_id = $1 AND facility_id = $2 AND version = $6
  AND open_incident_code IS NULL`, worldID, ref.facilityID,
		incident.Code, targetTick, fact.id, beforeState.Version)
	if err != nil {
		return nil, CityFacilityIncident{}, fmt.Errorf("fail city facility lifecycle state: %w", err)
	}
	if err = requireCityFacilityLifecycleRow(result, beforeState.FacilityCode); err != nil {
		return nil, CityFacilityIncident{}, err
	}
	if err = advanceCityFacilityLifecycleProfile(ctx, tx, worldID,
		cityFacilityLifecycleProfileDeltas{incidents: 1}); err != nil {
		return nil, CityFacilityIncident{}, err
	}
	if err = postCityFacilityLifecycleFact(ctx, tx, fact.id); err != nil {
		return nil, CityFacilityIncident{}, err
	}
	return fact, incident, nil
}
