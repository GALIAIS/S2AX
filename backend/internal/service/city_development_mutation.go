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

type cityDevelopmentFactRecord struct {
	id   int64
	fact CityDevelopmentFact
}

type cityDevelopmentProjectRef struct {
	id                         int64
	worldID                    int64
	code                       string
	name                       string
	projectType                string
	districtID                 int64
	districtCode               string
	parcelID                   int64
	parcelCode                 string
	buildingID                 int64
	buildingCode               string
	primaryUse                 string
	developerEntityID          int64
	developerEntityCode        string
	targetFloorCount           *int32
	targetQualityMilli         *int64
	addedFloorCount            int32
	addedFloorAreaSQM          int64
	addedCapacityUnits         int64
	qualityDeltaMilli          int64
	requiredBasicMaterialUnits int64
	requiredCapitalGoodsUnits  int64
	requiredLaborUnits         int64
	plannedDurationTicks       int64
	status                     string
	progressMilli              int64
	submittedTick              int64
	reviewedTick               *int64
	startedTick                *int64
	plannedCompletionTick      *int64
	completedTick              *int64
	cancelledTick              *int64
	version                    int64
}

type cityDevelopmentExecution struct {
	pending            cityPendingEvent
	fact               *CityDevelopmentFact
	resourceOperations []*CityResourceOperation
}

type cityDevelopmentAutoExecution struct {
	facts                []CityDevelopmentFact
	events               []cityPendingEvent
	adjustmentCount      int
	nextFactSequence     int64
	nextResourceSequence int64
}

func (s *CityEconomyService) applyCityDevelopmentCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, resourceSequence int64,
	command *CityCommand,
) (cityDevelopmentExecution, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT city_development_command`); err != nil {
		return cityDevelopmentExecution{}, fmt.Errorf("create city development command savepoint: %w", err)
	}
	execution, err := s.postCityDevelopmentCommand(
		ctx, tx, worldID, targetTick, factSequence, resourceSequence, command,
	)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_development_command`); rollbackErr != nil {
			return cityDevelopmentExecution{}, fmt.Errorf(
				"rollback city development command savepoint after %v: %w", err, rollbackErr,
			)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT city_development_command`); releaseErr != nil {
			return cityDevelopmentExecution{}, fmt.Errorf("release rejected city development command savepoint: %w", releaseErr)
		}
		if code := cityDevelopmentBusinessRejectionCode(err); code != "" {
			return cityDevelopmentExecution{pending: rejectedCityCommand(command, code)}, nil
		}
		return cityDevelopmentExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_development_command`); err != nil {
		return cityDevelopmentExecution{}, fmt.Errorf("release city development command savepoint: %w", err)
	}
	return execution, nil
}

func (s *CityEconomyService) postCityDevelopmentCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, resourceSequence int64,
	command *CityCommand,
) (cityDevelopmentExecution, error) {
	switch command.CommandType {
	case CityCommandTypeDevelopmentSubmit:
		payload, err := decodeStoredCityCommandPayload[cityDevelopmentSubmitPayload](command)
		if err != nil {
			return cityDevelopmentExecution{}, err
		}
		return s.submitCityDevelopmentProject(ctx, tx, worldID, targetTick, factSequence, command, payload)
	case CityCommandTypeDevelopmentReview:
		payload, err := decodeStoredCityCommandPayload[cityDevelopmentReviewPayload](command)
		if err != nil {
			return cityDevelopmentExecution{}, err
		}
		return s.reviewCityDevelopmentProject(ctx, tx, worldID, targetTick, factSequence, command, payload)
	case CityCommandTypeDevelopmentStart:
		payload, err := decodeStoredCityCommandPayload[cityDevelopmentStartPayload](command)
		if err != nil {
			return cityDevelopmentExecution{}, err
		}
		return s.startCityDevelopmentProject(
			ctx, tx, worldID, targetTick, factSequence, resourceSequence, command, payload,
		)
	case CityCommandTypeDevelopmentCancel:
		payload, err := decodeStoredCityCommandPayload[cityDevelopmentCancelPayload](command)
		if err != nil {
			return cityDevelopmentExecution{}, err
		}
		return s.cancelCityDevelopmentProject(ctx, tx, worldID, targetTick, factSequence, command, payload)
	default:
		return cityDevelopmentExecution{}, ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"command_type": command.CommandType},
		)
	}
}

func (s *CityEconomyService) submitCityDevelopmentProject(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
	command *CityCommand,
	payload cityDevelopmentSubmitPayload,
) (cityDevelopmentExecution, error) {
	plan, err := loadCityDevelopmentBuildingPlan(ctx, tx, worldID, payload)
	if err != nil {
		return cityDevelopmentExecution{}, err
	}
	var activeCount int
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_development_projects
WHERE world_id = $1 AND building_id = $2
  AND status IN ('submitted', 'approved', 'under_construction')`, worldID, plan.buildingID).Scan(&activeCount); err != nil {
		return cityDevelopmentExecution{}, fmt.Errorf("inspect active city development project: %w", err)
	}
	if activeCount != 0 {
		return cityDevelopmentExecution{}, cityDevelopmentReject(cityDevelopmentRejectionActiveConflict)
	}
	code := cityDevelopmentProjectCode(command.Sequence)
	name := payload.Name
	if name == "" {
		name = "Development " + strconv.FormatInt(command.Sequence, 10)
	}
	project := CityDevelopmentProject{
		Code: code, Name: name, ProjectType: payload.ProjectType,
		DistrictCode: plan.districtCode, ParcelCode: plan.parcelCode,
		BuildingCode: plan.buildingCode, PrimaryUse: plan.primaryUse,
		DeveloperEntityCode: plan.developerEntityCode,
		TargetFloorCount:    payload.TargetFloorCount, TargetQualityMilli: payload.TargetQualityMilli,
		AddedFloorCount: plan.addedFloorCount, AddedFloorAreaSQM: plan.addedFloorAreaSQM,
		AddedCapacityUnits: plan.addedCapacityUnits, QualityDeltaMilli: plan.qualityDeltaMilli,
		RequiredBasicMaterialUnits: plan.requiredBasicMaterialUnits,
		RequiredCapitalGoodsUnits:  plan.requiredCapitalGoodsUnits,
		RequiredLaborUnits:         plan.requiredLaborUnits,
		PlannedDurationTicks:       plan.plannedDurationTicks,
		Status:                     CityDevelopmentStatusSubmitted, ProgressMilli: 0,
		SubmittedTick: targetTick, Version: 1,
		Metadata: json.RawMessage(`{"policy_hash":"` + cityDevelopmentPolicyHash + `","schema_version":1}`),
	}
	factMetadata := map[string]any{"project": project, "schema_version": 1}
	fact, err := insertCityDevelopmentFact(ctx, tx, cityDevelopmentFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		projectCode: code, sourceCommandID: &command.ID,
		factType: CityDevelopmentFactSubmitted, toStatus: CityDevelopmentStatusSubmitted,
		progressBefore: 0, progressAfter: 0, versionBefore: 0, versionAfter: 1,
		metadata: factMetadata,
	})
	if err != nil {
		return cityDevelopmentExecution{}, err
	}
	metadata := []byte(project.Metadata)
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_development_projects
    (world_id, code, name, project_type, district_id, parcel_id, building_id,
     developer_entity_id, target_floor_count, target_quality_milli,
     added_floor_count, added_floor_area_sqm, added_capacity_units,
     quality_delta_milli, required_basic_material_units,
     required_capital_goods_units, required_labor_units, planned_duration_ticks,
     status, progress_milli, submitted_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, 'submitted', 0, $19, 1, $20::jsonb)`,
		worldID, code, name, payload.ProjectType, plan.districtID, plan.parcelID,
		plan.buildingID, plan.developerEntityID,
		developmentNullableInt32(payload.TargetFloorCount), developmentNullableInt64(payload.TargetQualityMilli),
		plan.addedFloorCount, plan.addedFloorAreaSQM, plan.addedCapacityUnits,
		plan.qualityDeltaMilli, plan.requiredBasicMaterialUnits,
		plan.requiredCapitalGoodsUnits, plan.requiredLaborUnits,
		plan.plannedDurationTicks, targetTick, metadata); err != nil {
		return cityDevelopmentExecution{}, fmt.Errorf("insert city development project: %w", err)
	}
	if err = advanceCityDevelopmentProfile(ctx, tx, worldID, 1, 1, 0); err != nil {
		return cityDevelopmentExecution{}, err
	}
	if err = postCityDevelopmentFact(ctx, tx, fact.id); err != nil {
		return cityDevelopmentExecution{}, err
	}
	eventPayload := cityDevelopmentEventPayload(&project, &fact.fact)
	return cityDevelopmentExecution{
		pending: cityPendingEvent{
			command: command, status: CityCommandStatusApplied,
			eventType: "city.development.submitted", payload: eventPayload,
			result: map[string]any{
				"applied": true, "project_code": code, "status": project.Status,
				"required_basic_material_units": plan.requiredBasicMaterialUnits,
				"required_capital_goods_units":  plan.requiredCapitalGoodsUnits,
				"required_labor_units":          plan.requiredLaborUnits,
				"planned_duration_ticks":        plan.plannedDurationTicks,
			},
		},
		fact: &fact.fact,
	}, nil
}

func (s *CityEconomyService) reviewCityDevelopmentProject(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
	command *CityCommand,
	payload cityDevelopmentReviewPayload,
) (cityDevelopmentExecution, error) {
	project, err := loadCityDevelopmentProjectRef(ctx, tx, worldID, payload.ProjectCode)
	if err != nil {
		return cityDevelopmentExecution{}, err
	}
	if project.status != CityDevelopmentStatusSubmitted {
		return cityDevelopmentExecution{}, cityDevelopmentReject(cityDevelopmentRejectionStateConflict)
	}
	factType := CityDevelopmentFactApproved
	toStatus := CityDevelopmentStatusApproved
	eventType := "city.development.approved"
	if payload.Decision == "reject" {
		factType = CityDevelopmentFactRejected
		toStatus = CityDevelopmentStatusRejected
		eventType = "city.development.rejected"
	}
	fromStatus := project.status
	fact, err := insertCityDevelopmentFact(ctx, tx, cityDevelopmentFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		projectCode: project.code, sourceCommandID: &command.ID,
		factType: factType, fromStatus: &fromStatus, toStatus: toStatus,
		progressBefore: 0, progressAfter: 0,
		versionBefore: project.version, versionAfter: project.version + 1,
		metadata: map[string]any{"decision": payload.Decision, "note": payload.Note, "schema_version": 1},
	})
	if err != nil {
		return cityDevelopmentExecution{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_development_projects
SET status = $3, reviewed_tick = $4, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND code = $2 AND status = 'submitted' AND version = $5`,
		worldID, project.code, toStatus, targetTick, project.version)
	if err != nil {
		return cityDevelopmentExecution{}, fmt.Errorf("review city development project: %w", err)
	}
	if err = requireCityDevelopmentRow(result, project.code); err != nil {
		return cityDevelopmentExecution{}, err
	}
	if err = advanceCityDevelopmentProfile(ctx, tx, worldID, 0, 1, 0); err != nil {
		return cityDevelopmentExecution{}, err
	}
	if err = postCityDevelopmentFact(ctx, tx, fact.id); err != nil {
		return cityDevelopmentExecution{}, err
	}
	return cityDevelopmentExecution{
		pending: cityPendingEvent{
			command: command, status: CityCommandStatusApplied, eventType: eventType,
			payload: map[string]any{
				"project_code": project.code, "from_status": fromStatus,
				"to_status": toStatus, "fact_tick": targetTick, "fact_sequence": factSequence,
			},
			result: map[string]any{"applied": true, "project_code": project.code, "status": toStatus},
		}, fact: &fact.fact,
	}, nil
}

func (s *CityEconomyService) startCityDevelopmentProject(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, resourceSequence int64,
	command *CityCommand,
	payload cityDevelopmentStartPayload,
) (cityDevelopmentExecution, error) {
	project, err := loadCityDevelopmentProjectRef(ctx, tx, worldID, payload.ProjectCode)
	if err != nil {
		return cityDevelopmentExecution{}, err
	}
	if project.status != CityDevelopmentStatusApproved {
		return cityDevelopmentExecution{}, cityDevelopmentReject(cityDevelopmentRejectionStateConflict)
	}
	currentPlan, err := loadCityDevelopmentBuildingPlan(ctx, tx, worldID, cityDevelopmentSubmitPayload{
		ProjectType: project.projectType, BuildingCode: project.buildingCode,
		DeveloperEntityID: project.developerEntityID,
		TargetFloorCount:  project.targetFloorCount, TargetQualityMilli: project.targetQualityMilli,
	})
	if err != nil {
		return cityDevelopmentExecution{}, err
	}
	if !cityDevelopmentPlanMatchesProject(currentPlan, project) {
		return cityDevelopmentExecution{}, cityDevelopmentReject(cityDevelopmentRejectionPlanStale)
	}
	var employeeUnits, reservedLabor int64
	if err = tx.QueryRowContext(ctx, `
SELECT firm.employee_units,
       COALESCE((SELECT SUM(active.required_labor_units)
                 FROM city_development_projects active
                 WHERE active.world_id = firm.world_id
                   AND active.developer_entity_id = firm.entity_id
                   AND active.status = 'under_construction'), 0)::BIGINT
FROM city_firm_states firm
WHERE firm.world_id = $1 AND firm.entity_id = $2
FOR UPDATE`, worldID, project.developerEntityID).Scan(&employeeUnits, &reservedLabor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cityDevelopmentExecution{}, cityDevelopmentReject(cityDevelopmentRejectionDeveloperInvalid)
		}
		return cityDevelopmentExecution{}, fmt.Errorf("lock city development labor capacity: %w", err)
	}
	if reservedLabor > employeeUnits || project.requiredLaborUnits > employeeUnits-reservedLabor {
		return cityDevelopmentExecution{}, cityDevelopmentReject(cityDevelopmentRejectionLabor)
	}
	if project.plannedDurationTicks > math.MaxInt64-targetTick {
		return cityDevelopmentExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "development_completion_tick"})
	}
	plannedCompletionTick := targetTick + project.plannedDurationTicks
	fromStatus := project.status
	fact, err := insertCityDevelopmentFact(ctx, tx, cityDevelopmentFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		projectCode: project.code, sourceCommandID: &command.ID,
		factType: CityDevelopmentFactStarted, fromStatus: &fromStatus,
		toStatus:       CityDevelopmentStatusUnderConstruction,
		progressBefore: 0, progressAfter: 0,
		versionBefore: project.version, versionAfter: project.version + 1,
		metadata: map[string]any{
			"started_tick": targetTick, "planned_completion_tick": plannedCompletionTick,
			"required_basic_material_units": project.requiredBasicMaterialUnits,
			"required_capital_goods_units":  project.requiredCapitalGoodsUnits,
			"required_labor_units":          project.requiredLaborUnits, "schema_version": 1,
		},
	})
	if err != nil {
		return cityDevelopmentExecution{}, err
	}
	operations := make([]*CityResourceOperation, 0, 2)
	resourceSpecs := []struct {
		code     string
		quantity int64
	}{
		{code: "basic_material", quantity: project.requiredBasicMaterialUnits},
		{code: "capital_goods", quantity: project.requiredCapitalGoodsUnits},
	}
	for index, requirement := range resourceSpecs {
		balance, balanceErr := ensureCityInventoryRef(
			ctx, tx, worldID, project.developerEntityID, project.districtCode, requirement.code,
		)
		if balanceErr != nil {
			return cityDevelopmentExecution{}, balanceErr
		}
		operation, operationErr := postCityResourceOperation(ctx, tx, cityResourceOperationSpec{
			worldID: worldID, tick: targetTick, sequence: resourceSequence + int64(index),
			operationKey:  "development:" + project.code + ":" + requirement.code,
			operationType: "consumption", actorEntityID: project.developerEntityID,
			districtID:  project.districtID,
			description: "Development input: " + requirement.code,
			metadata: map[string]any{
				"development_fact_id":      fact.id,
				"development_project_code": project.code,
				"resource_code":            requirement.code,
				"quantity_units":           requirement.quantity,
				"schema_version":           1,
			},
			lines: []cityResourcePostingLine{{
				balance: balance, direction: "out", quantityUnits: requirement.quantity,
				memo: "Development " + project.code,
			}},
		})
		if operationErr != nil {
			return cityDevelopmentExecution{}, operationErr
		}
		operations = append(operations, operation)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_development_projects
SET status = 'under_construction', started_tick = $3,
    planned_completion_tick = $4, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND code = $2 AND status = 'approved' AND version = $5`,
		worldID, project.code, targetTick, plannedCompletionTick, project.version)
	if err != nil {
		return cityDevelopmentExecution{}, fmt.Errorf("start city development project: %w", err)
	}
	if err = requireCityDevelopmentRow(result, project.code); err != nil {
		return cityDevelopmentExecution{}, err
	}
	if err = advanceCityDevelopmentProfile(ctx, tx, worldID, 0, 1, 0); err != nil {
		return cityDevelopmentExecution{}, err
	}
	if err = postCityDevelopmentFact(ctx, tx, fact.id); err != nil {
		return cityDevelopmentExecution{}, err
	}
	return cityDevelopmentExecution{
		pending: cityPendingEvent{
			command: command, status: CityCommandStatusApplied,
			eventType: "city.development.started",
			payload: map[string]any{
				"project_code": project.code, "from_status": fromStatus,
				"to_status": CityDevelopmentStatusUnderConstruction,
				"fact_tick": targetTick, "fact_sequence": factSequence,
				"planned_completion_tick":  plannedCompletionTick,
				"resource_operation_count": len(operations),
			},
			result: map[string]any{
				"applied": true, "project_code": project.code,
				"status":                  CityDevelopmentStatusUnderConstruction,
				"planned_completion_tick": plannedCompletionTick,
			},
		}, fact: &fact.fact, resourceOperations: operations,
	}, nil
}

func (s *CityEconomyService) cancelCityDevelopmentProject(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
	command *CityCommand,
	payload cityDevelopmentCancelPayload,
) (cityDevelopmentExecution, error) {
	project, err := loadCityDevelopmentProjectRef(ctx, tx, worldID, payload.ProjectCode)
	if err != nil {
		return cityDevelopmentExecution{}, err
	}
	switch project.status {
	case CityDevelopmentStatusSubmitted, CityDevelopmentStatusApproved,
		CityDevelopmentStatusUnderConstruction:
	default:
		return cityDevelopmentExecution{}, cityDevelopmentReject(cityDevelopmentRejectionStateConflict)
	}
	fromStatus := project.status
	fact, err := insertCityDevelopmentFact(ctx, tx, cityDevelopmentFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		projectCode: project.code, sourceCommandID: &command.ID,
		factType: CityDevelopmentFactCancelled, fromStatus: &fromStatus,
		toStatus:       CityDevelopmentStatusCancelled,
		progressBefore: project.progressMilli, progressAfter: project.progressMilli,
		versionBefore: project.version, versionAfter: project.version + 1,
		metadata: map[string]any{
			"reason":             payload.Reason,
			"resources_are_sunk": project.startedTick != nil,
			"schema_version":     1,
		},
	})
	if err != nil {
		return cityDevelopmentExecution{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_development_projects
SET status = 'cancelled', cancelled_tick = $3, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND code = $2
  AND status IN ('submitted', 'approved', 'under_construction') AND version = $4`,
		worldID, project.code, targetTick, project.version)
	if err != nil {
		return cityDevelopmentExecution{}, fmt.Errorf("cancel city development project: %w", err)
	}
	if err = requireCityDevelopmentRow(result, project.code); err != nil {
		return cityDevelopmentExecution{}, err
	}
	if err = advanceCityDevelopmentProfile(ctx, tx, worldID, 0, 1, 0); err != nil {
		return cityDevelopmentExecution{}, err
	}
	if err = postCityDevelopmentFact(ctx, tx, fact.id); err != nil {
		return cityDevelopmentExecution{}, err
	}
	return cityDevelopmentExecution{
		pending: cityPendingEvent{
			command: command, status: CityCommandStatusApplied,
			eventType: "city.development.cancelled",
			payload: map[string]any{
				"project_code": project.code, "from_status": fromStatus,
				"to_status": CityDevelopmentStatusCancelled,
				"fact_tick": targetTick, "fact_sequence": factSequence,
				"resources_are_sunk": project.startedTick != nil,
			},
			result: map[string]any{
				"applied": true, "project_code": project.code,
				"status": CityDevelopmentStatusCancelled,
			},
		}, fact: &fact.fact,
	}, nil
}

func (s *CityEconomyService) advanceCityDevelopmentProjects(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, resourceSequence int64,
) (cityDevelopmentAutoExecution, error) {
	execution := cityDevelopmentAutoExecution{
		facts: make([]CityDevelopmentFact, 0), events: make([]cityPendingEvent, 0),
		nextFactSequence: factSequence, nextResourceSequence: resourceSequence,
	}
	if _, err := tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_development_auto_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10)); err != nil {
		return execution, fmt.Errorf("activate city development automatic reducer: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
SELECT project.code
FROM city_development_projects project
WHERE project.world_id = $1 AND project.status = 'under_construction'
  AND project.started_tick < $2
ORDER BY project.code ASC
FOR UPDATE`, worldID, targetTick)
	if err != nil {
		return execution, fmt.Errorf("load active city development projects: %w", err)
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
	if err = closeCityRows(rows, "iterate active city development projects"); err != nil {
		return execution, err
	}
	for _, code := range codes {
		project, loadErr := loadCityDevelopmentProjectRef(ctx, tx, worldID, code)
		if loadErr != nil {
			return execution, loadErr
		}
		if project.startedTick == nil || project.plannedCompletionTick == nil {
			return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"project_code": code})
		}
		progress, progressErr := cityDevelopmentProgress(
			*project.startedTick, project.plannedDurationTicks, targetTick,
		)
		if progressErr != nil {
			return execution, progressErr
		}
		if progress <= project.progressMilli {
			continue
		}
		factType := CityDevelopmentFactProgressed
		toStatus := CityDevelopmentStatusUnderConstruction
		eventType := "city.development.progressed"
		if progress == 1000 {
			factType = CityDevelopmentFactCompleted
			toStatus = CityDevelopmentStatusCompleted
			eventType = "city.development.completed"
		}
		fromStatus := project.status
		metadata := map[string]any{
			"elapsed_ticks":          targetTick - *project.startedTick,
			"planned_duration_ticks": project.plannedDurationTicks,
			"schema_version":         1,
		}
		if factType == CityDevelopmentFactCompleted {
			metadata["adjustment"] = CityBuildingAdjustment{
				ProjectCode: project.code, BuildingCode: project.buildingCode,
				DistrictCode:       project.districtCode,
				AddedFloorCount:    project.addedFloorCount,
				AddedTopZ:          project.addedFloorCount,
				AddedFloorAreaSQM:  project.addedFloorAreaSQM,
				AddedCapacityUnits: project.addedCapacityUnits,
				QualityDeltaMilli:  project.qualityDeltaMilli,
				CompletedTick:      targetTick,
				Metadata:           json.RawMessage(`{"policy_hash":"` + cityDevelopmentPolicyHash + `","schema_version":1}`),
			}
		}
		fact, factErr := insertCityDevelopmentFact(ctx, tx, cityDevelopmentFactInsert{
			worldID: worldID, tick: targetTick, sequence: execution.nextFactSequence,
			projectCode: project.code, factType: factType,
			fromStatus: &fromStatus, toStatus: toStatus,
			progressBefore: project.progressMilli, progressAfter: progress,
			versionBefore: project.version, versionAfter: project.version + 1,
			metadata: metadata,
		})
		if factErr != nil {
			return execution, factErr
		}
		if factType == CityDevelopmentFactCompleted {
			if err = completeCityDevelopmentProject(ctx, tx, worldID, targetTick, fact.id, project); err != nil {
				return execution, err
			}
			execution.adjustmentCount++
		} else {
			result, updateErr := tx.ExecContext(ctx, `
UPDATE city_development_projects
SET progress_milli = $3, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND code = $2 AND status = 'under_construction' AND version = $4`,
				worldID, project.code, progress, project.version)
			if updateErr != nil {
				return execution, fmt.Errorf("advance city development progress: %w", updateErr)
			}
			if err = requireCityDevelopmentRow(result, project.code); err != nil {
				return execution, err
			}
		}
		if err = advanceCityDevelopmentProfile(ctx, tx, worldID, 0, 1,
			boolInt64(factType == CityDevelopmentFactCompleted)); err != nil {
			return execution, err
		}
		if err = postCityDevelopmentFact(ctx, tx, fact.id); err != nil {
			return execution, err
		}
		execution.facts = append(execution.facts, fact.fact)
		execution.events = append(execution.events, cityPendingEvent{
			eventType: eventType,
			payload: map[string]any{
				"project_code": project.code, "from_status": fromStatus,
				"to_status": toStatus, "progress_before_milli": project.progressMilli,
				"progress_after_milli": progress, "fact_tick": targetTick,
				"fact_sequence": execution.nextFactSequence,
			},
		})
		execution.nextFactSequence++
	}
	return execution, nil
}

func completeCityDevelopmentProject(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factID int64,
	project *cityDevelopmentProjectRef,
) error {
	metadata := []byte(`{"policy_hash":"` + cityDevelopmentPolicyHash + `","schema_version":1}`)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_building_adjustments
    (world_id, project_code, building_id, district_id, completion_fact_id,
     added_floor_count, added_top_z, added_floor_area_sqm,
     added_capacity_units, quality_delta_milli, completed_tick, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8, $9, $10, $11::jsonb)`,
		worldID, project.code, project.buildingID, project.districtID, factID,
		project.addedFloorCount, project.addedFloorAreaSQM,
		project.addedCapacityUnits, project.qualityDeltaMilli, targetTick, metadata); err != nil {
		return fmt.Errorf("insert city building adjustment: %w", err)
	}
	if project.addedCapacityUnits > 0 {
		column := map[string]string{
			"residential": "residential_capacity_units",
			"commercial":  "commercial_capacity_units",
			"industrial":  "industrial_capacity_units",
		}[project.primaryUse]
		if column == "" {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "development_primary_use"})
		}
		query := `UPDATE city_districts SET ` + column + ` = ` + column + ` + $3, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND ` + column + ` <= 9223372036854775807 - $3`
		result, err := tx.ExecContext(ctx, query, worldID, project.districtID, project.addedCapacityUnits)
		if err != nil {
			return fmt.Errorf("increase city district development capacity: %w", err)
		}
		if err = requireCityDevelopmentRow(result, project.code); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_development_projects
SET status = 'completed', progress_milli = 1000, completed_tick = $3,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND code = $2 AND status = 'under_construction' AND version = $4`,
		worldID, project.code, targetTick, project.version)
	if err != nil {
		return fmt.Errorf("complete city development project: %w", err)
	}
	return requireCityDevelopmentRow(result, project.code)
}

type cityDevelopmentFactInsert struct {
	worldID         int64
	tick            int64
	sequence        int64
	projectCode     string
	sourceCommandID *int64
	factType        string
	fromStatus      *string
	toStatus        string
	progressBefore  int64
	progressAfter   int64
	versionBefore   int64
	versionAfter    int64
	metadata        map[string]any
}

func insertCityDevelopmentFact(
	ctx context.Context,
	tx *sql.Tx,
	input cityDevelopmentFactInsert,
) (*cityDevelopmentFactRecord, error) {
	metadata, err := json.Marshal(input.metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city development fact metadata: %w", err)
	}
	var factID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_development_facts
    (world_id, tick, sequence, project_code, source_command_id, fact_type,
     from_status, to_status, progress_before_milli, progress_after_milli,
     project_version_before, project_version_after, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)
RETURNING id`, input.worldID, input.tick, input.sequence, input.projectCode,
		developmentNullableInt64(input.sourceCommandID), input.factType,
		developmentNullableString(input.fromStatus), input.toStatus,
		input.progressBefore, input.progressAfter, input.versionBefore,
		input.versionAfter, metadata).Scan(&factID)
	if err != nil {
		return nil, fmt.Errorf("insert city development fact draft: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_development_fact_id', $1, TRUE)`,
		strconv.FormatInt(factID, 10)); err != nil {
		return nil, fmt.Errorf("activate city development fact write gate: %w", err)
	}
	fact := CityDevelopmentFact{
		Tick: input.tick, Sequence: input.sequence, ProjectCode: input.projectCode,
		FactType: input.factType, FromStatus: input.fromStatus, ToStatus: input.toStatus,
		ProgressBeforeMilli: input.progressBefore, ProgressAfterMilli: input.progressAfter,
		ProjectVersionBefore: input.versionBefore, ProjectVersionAfter: input.versionAfter,
		Metadata: metadata,
	}
	return &cityDevelopmentFactRecord{id: factID, fact: fact}, nil
}

func postCityDevelopmentFact(ctx context.Context, tx *sql.Tx, factID int64) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_development_facts SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, factID)
	if err != nil {
		return fmt.Errorf("post city development fact: %w", err)
	}
	return requireCityDevelopmentRow(result, strconv.FormatInt(factID, 10))
}

func advanceCityDevelopmentProfile(
	ctx context.Context,
	tx *sql.Tx,
	worldID, projectDelta, factDelta, adjustmentDelta int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_development_profiles
SET project_count = project_count + $2,
    fact_count = fact_count + $3,
    adjustment_count = adjustment_count + $4,
    revision = revision + 1, updated_at = NOW()
WHERE world_id = $1`, worldID, projectDelta, factDelta, adjustmentDelta)
	if err != nil {
		return fmt.Errorf("advance city development profile: %w", err)
	}
	return requireCityDevelopmentRow(result, strconv.FormatInt(worldID, 10))
}

func loadCityDevelopmentBuildingPlan(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	payload cityDevelopmentSubmitPayload,
) (*cityDevelopmentBuildingPlan, error) {
	var plan cityDevelopmentBuildingPlan
	var currentFloors, currentArea, currentCapacity, currentQuality int64
	err := tx.QueryRowContext(ctx, `
SELECT building.id, building.code, parcel.id, parcel.code, district.id, district.code,
       building.primary_use, developer.id, developer.code,
       building.floor_count + COALESCE(adjustment.added_floors, 0),
       building.floor_area_sqm + COALESCE(adjustment.added_area, 0),
       building.capacity_units + COALESCE(adjustment.added_capacity, 0),
       building.quality_milli + COALESCE(adjustment.quality_delta, 0),
       building.top_z, building.footprint_area_sqm, parcel.area_sqm,
       rule.max_floor_area_ratio_milli, rule.max_floors, rule.sqm_per_capacity_unit
FROM city_buildings building
JOIN city_parcels parcel ON parcel.id = building.parcel_id AND parcel.world_id = building.world_id
JOIN city_districts district ON district.id = building.district_id AND district.world_id = building.world_id
JOIN city_zoning_rules rule ON rule.world_id = building.world_id AND rule.code = parcel.zone_code
JOIN city_economic_entities developer
  ON developer.world_id = building.world_id AND developer.id = $3
JOIN city_firm_states firm
  ON firm.world_id = developer.world_id AND firm.entity_id = developer.id
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(value.added_floor_count), 0)::BIGINT AS added_floors,
           COALESCE(SUM(value.added_floor_area_sqm), 0)::BIGINT AS added_area,
           COALESCE(SUM(value.added_capacity_units), 0)::BIGINT AS added_capacity,
           COALESCE(SUM(value.quality_delta_milli), 0)::BIGINT AS quality_delta
    FROM city_building_adjustments value
    WHERE value.world_id = building.world_id AND value.building_id = building.id
) adjustment ON TRUE
WHERE building.world_id = $1 AND building.code = $2
  AND building.status = 'active' AND parcel.status = 'active'
  AND developer.status = 'active' AND developer.entity_type = 'firm'
  AND firm.district_id = building.district_id
FOR UPDATE OF building, parcel, developer, firm`, worldID, payload.BuildingCode,
		payload.DeveloperEntityID).Scan(
		&plan.buildingID, &plan.buildingCode, &plan.parcelID, &plan.parcelCode,
		&plan.districtID, &plan.districtCode, &plan.primaryUse,
		&plan.developerEntityID, &plan.developerEntityCode,
		&currentFloors, &currentArea, &currentCapacity, &currentQuality,
		&plan.baseTopZ, &plan.footprintAreaSQM, &plan.parcelAreaSQM,
		&plan.maxFloorAreaRatioMilli, &plan.maxFloors, &plan.sqmPerCapacityUnit,
	)
	if errors.Is(err, sql.ErrNoRows) {
		var buildingExists bool
		if checkErr := tx.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM city_buildings WHERE world_id = $1 AND code = $2 AND status = 'active')`,
			worldID, payload.BuildingCode).Scan(&buildingExists); checkErr != nil {
			return nil, fmt.Errorf("inspect city development building: %w", checkErr)
		}
		if !buildingExists {
			return nil, cityDevelopmentReject(cityDevelopmentRejectionBuildingNotFound)
		}
		return nil, cityDevelopmentReject(cityDevelopmentRejectionDeveloperInvalid)
	}
	if err != nil {
		return nil, fmt.Errorf("load city development building plan: %w", err)
	}
	derived, err := deriveCityDevelopmentPlan(
		payload.ProjectType, currentFloors, currentArea, currentCapacity, currentQuality,
		plan.footprintAreaSQM, plan.parcelAreaSQM, plan.maxFloorAreaRatioMilli,
		plan.maxFloors, plan.sqmPerCapacityUnit,
		payload.TargetFloorCount, payload.TargetQualityMilli,
	)
	if err != nil {
		return nil, err
	}
	derived.buildingID, derived.buildingCode = plan.buildingID, plan.buildingCode
	derived.parcelID, derived.parcelCode = plan.parcelID, plan.parcelCode
	derived.districtID, derived.districtCode = plan.districtID, plan.districtCode
	derived.primaryUse = plan.primaryUse
	derived.developerEntityID, derived.developerEntityCode = plan.developerEntityID, plan.developerEntityCode
	derived.baseTopZ = plan.baseTopZ
	return derived, nil
}

func loadCityDevelopmentProjectRef(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	projectCode string,
) (*cityDevelopmentProjectRef, error) {
	project := &cityDevelopmentProjectRef{}
	var targetFloors sql.NullInt32
	var targetQuality, reviewed, started, planned, completed, cancelled sql.NullInt64
	err := tx.QueryRowContext(ctx, `
SELECT project.id, project.world_id, project.code, project.name, project.project_type,
       project.district_id, district.code, project.parcel_id, parcel.code,
       project.building_id, building.code, building.primary_use,
       project.developer_entity_id, developer.code,
       project.target_floor_count, project.target_quality_milli,
       project.added_floor_count, project.added_floor_area_sqm,
       project.added_capacity_units, project.quality_delta_milli,
       project.required_basic_material_units, project.required_capital_goods_units,
       project.required_labor_units, project.planned_duration_ticks,
       project.status, project.progress_milli, project.submitted_tick,
       project.reviewed_tick, project.started_tick, project.planned_completion_tick,
       project.completed_tick, project.cancelled_tick, project.version
FROM city_development_projects project
JOIN city_districts district ON district.id = project.district_id
JOIN city_parcels parcel ON parcel.id = project.parcel_id
JOIN city_buildings building ON building.id = project.building_id
JOIN city_economic_entities developer ON developer.id = project.developer_entity_id
WHERE project.world_id = $1 AND project.code = $2
FOR UPDATE OF project`, worldID, projectCode).Scan(
		&project.id, &project.worldID, &project.code, &project.name, &project.projectType,
		&project.districtID, &project.districtCode, &project.parcelID, &project.parcelCode,
		&project.buildingID, &project.buildingCode, &project.primaryUse,
		&project.developerEntityID, &project.developerEntityCode,
		&targetFloors, &targetQuality, &project.addedFloorCount,
		&project.addedFloorAreaSQM, &project.addedCapacityUnits,
		&project.qualityDeltaMilli, &project.requiredBasicMaterialUnits,
		&project.requiredCapitalGoodsUnits, &project.requiredLaborUnits,
		&project.plannedDurationTicks, &project.status, &project.progressMilli,
		&project.submittedTick, &reviewed, &started, &planned, &completed,
		&cancelled, &project.version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityDevelopmentReject(cityDevelopmentRejectionProjectNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load city development project: %w", err)
	}
	if targetFloors.Valid {
		value := targetFloors.Int32
		project.targetFloorCount = &value
	}
	project.targetQualityMilli = nullableInt64Pointer(targetQuality)
	project.reviewedTick = nullableInt64Pointer(reviewed)
	project.startedTick = nullableInt64Pointer(started)
	project.plannedCompletionTick = nullableInt64Pointer(planned)
	project.completedTick = nullableInt64Pointer(completed)
	project.cancelledTick = nullableInt64Pointer(cancelled)
	return project, nil
}

func cityDevelopmentPlanMatchesProject(plan *cityDevelopmentBuildingPlan, project *cityDevelopmentProjectRef) bool {
	return plan != nil && project != nil &&
		plan.buildingID == project.buildingID && plan.parcelID == project.parcelID &&
		plan.districtID == project.districtID && plan.developerEntityID == project.developerEntityID &&
		plan.addedFloorCount == project.addedFloorCount &&
		plan.addedFloorAreaSQM == project.addedFloorAreaSQM &&
		plan.addedCapacityUnits == project.addedCapacityUnits &&
		plan.qualityDeltaMilli == project.qualityDeltaMilli &&
		plan.requiredBasicMaterialUnits == project.requiredBasicMaterialUnits &&
		plan.requiredCapitalGoodsUnits == project.requiredCapitalGoodsUnits &&
		plan.requiredLaborUnits == project.requiredLaborUnits &&
		plan.plannedDurationTicks == project.plannedDurationTicks
}

func requireCityDevelopmentRow(result sql.Result, identity string) error {
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"development_identity": identity})
	}
	return nil
}

func cityDevelopmentEventPayload(
	project *CityDevelopmentProject,
	fact *CityDevelopmentFact,
) map[string]any {
	return map[string]any{
		"project_code": project.Code, "project_type": project.ProjectType,
		"building_code": project.BuildingCode, "district_code": project.DistrictCode,
		"developer_entity_code": project.DeveloperEntityCode,
		"to_status":             fact.ToStatus, "fact_tick": fact.Tick,
		"fact_sequence": fact.Sequence,
	}
}

func developmentNullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
