//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCityDevelopmentClosesCommandResourceProjectionReplayAndGuardLoop(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-development-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-development-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(740031)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Development Protocol City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V3,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7V3, foundation.World.SimulationVersion)
	worldID := foundation.World.ID

	initialDevelopment, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), initialDevelopment.Profile.BaselineTick)
	require.Zero(t, initialDevelopment.Profile.ProjectCount)
	require.Zero(t, initialDevelopment.Profile.FactCount)
	require.Zero(t, initialDevelopment.Profile.AdjustmentCount)
	require.Empty(t, initialDevelopment.Projects)
	require.Empty(t, initialDevelopment.Facts)
	require.Empty(t, initialDevelopment.Adjustments)
	require.NotEmpty(t, initialDevelopment.Developers)

	_, err = cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: outsider.ID, WorldID: worldID,
	})
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	landBefore, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	developer := initialDevelopment.Developers[0]
	buildingBefore := findDevelopmentBuilding(t, landBefore.Buildings, developer.DistrictCode)
	require.Less(t, buildingBefore.QualityMilli, int64(1500))
	targetQuality := buildingBefore.QualityMilli + 1

	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	submitAndStep := func(expectedTick int64, key, commandType string, payload json.RawMessage) *service.CityStepResult {
		command, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key + "-command",
			CommandType: commandType, Payload: payload, ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, submitErr)
		require.Equal(t, service.CityCommandStatusPending, command.Status)
		step, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key + "-step",
			ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, stepErr)
		require.Len(t, step.Commands, 1)
		require.Equal(t, service.CityCommandStatusApplied, step.Commands[0].Status)
		return step
	}

	submitted := submitAndStep(0, "development-submit", service.CityCommandTypeDevelopmentSubmit,
		marshalPayload(map[string]any{
			"project_type":  service.CityDevelopmentProjectRenovation,
			"building_code": buildingBefore.Code, "developer_entity_id": developer.EntityID,
			"target_quality_milli": targetQuality, "name": "First deterministic renovation",
		}))
	require.Len(t, submitted.DevelopmentFacts, 1)
	require.Equal(t, service.CityDevelopmentFactSubmitted, submitted.DevelopmentFacts[0].FactType)
	projectCode := submitted.DevelopmentFacts[0].ProjectCode
	require.Equal(t, "development_1", projectCode)

	state, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID, BuildingCode: buildingBefore.Code,
	})
	require.NoError(t, err)
	require.Len(t, state.Projects, 1)
	require.Equal(t, service.CityDevelopmentStatusSubmitted, state.Projects[0].Status)
	require.Equal(t, int64(1), state.Projects[0].QualityDeltaMilli)
	require.Positive(t, state.Projects[0].RequiredBasicMaterialUnits)
	require.Positive(t, state.Projects[0].RequiredCapitalGoodsUnits)
	require.Positive(t, state.Projects[0].RequiredLaborUnits)
	require.Equal(t, int64(1), state.Projects[0].PlannedDurationTicks)
	plannedProject := state.Projects[0]

	approved := submitAndStep(1, "development-approve", service.CityCommandTypeDevelopmentReview,
		marshalPayload(map[string]any{"project_code": projectCode, "decision": "approve", "note": "zoning accepted"}))
	require.Len(t, approved.DevelopmentFacts, 1)
	require.Equal(t, service.CityDevelopmentFactApproved, approved.DevelopmentFacts[0].FactType)

	startExpected := int64(2)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "development-start-command",
		CommandType: service.CityCommandTypeDevelopmentStart,
		Payload:     marshalPayload(map[string]any{"project_code": projectCode}), ExpectedWorldTick: &startExpected,
	})
	require.NoError(t, err)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "development-resume-command",
		CommandType: service.CityCommandTypeWorldResume, Payload: json.RawMessage(`{}`),
		ExpectedWorldTick: &startExpected,
	})
	require.NoError(t, err)
	started, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "development-start-step",
		ExpectedWorldTick: &startExpected,
	})
	require.NoError(t, err)
	require.Len(t, started.Commands, 2)
	require.Equal(t, 2, started.Tick.AppliedCommandCount)
	require.Len(t, started.DevelopmentFacts, 1)
	require.Equal(t, service.CityDevelopmentFactStarted, started.DevelopmentFacts[0].FactType)
	require.GreaterOrEqual(t, len(started.ResourceOperations), 2)
	resourceCodes := make(map[string]int64)
	for _, operation := range started.ResourceOperations {
		if operation.OperationType != "consumption" {
			continue
		}
		require.Len(t, operation.Entries, 1)
		resourceCodes[operation.Entries[0].ResourceCode] += operation.Entries[0].QuantityUnits
	}
	require.Equal(t, plannedProject.RequiredBasicMaterialUnits, resourceCodes["basic_material"])
	require.Equal(t, plannedProject.RequiredCapitalGoodsUnits, resourceCodes["capital_goods"])

	state, err = cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID, Status: service.CityDevelopmentStatusUnderConstruction,
	})
	require.NoError(t, err)
	require.Len(t, state.Projects, 1)
	require.Equal(t, plannedProject.RequiredLaborUnits, state.Developers[0].ReservedLaborUnits)
	require.Equal(t, state.Developers[0].EmployeeUnits-plannedProject.RequiredLaborUnits,
		state.Developers[0].AvailableLaborUnits)

	completionExpected := int64(3)
	completed, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "development-completion-step",
		ExpectedWorldTick: &completionExpected,
	})
	require.NoError(t, err)
	require.Len(t, completed.DevelopmentFacts, 1)
	require.Equal(t, service.CityDevelopmentFactCompleted, completed.DevelopmentFacts[0].FactType)
	require.Len(t, completed.BuildingAdjustments, 1)
	require.Equal(t, projectCode, completed.BuildingAdjustments[0].ProjectCode)
	require.Equal(t, int64(1), completed.BuildingAdjustments[0].QualityDeltaMilli)

	completedState, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID, Status: service.CityDevelopmentStatusCompleted,
	})
	require.NoError(t, err)
	require.Len(t, completedState.Projects, 1)
	require.Len(t, completedState.Adjustments, 1)
	require.Equal(t, int64(4), completedState.Profile.FactCount)
	require.Equal(t, int64(1), completedState.Profile.ProjectCount)
	require.Equal(t, int64(1), completedState.Profile.AdjustmentCount)
	require.Zero(t, completedState.Developers[0].ReservedLaborUnits)

	landAfter, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	buildingAfter := findBuildingByCode(t, landAfter.Buildings, buildingBefore.Code)
	require.Equal(t, targetQuality, buildingAfter.QualityMilli)
	require.Equal(t, buildingBefore.FloorCount, buildingAfter.FloorCount)
	require.Equal(t, buildingBefore.CapacityUnits, buildingAfter.CapacityUnits)

	firstPage, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID, BuildingCode: buildingBefore.Code, Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, firstPage.Facts, 2)
	require.NotNil(t, firstPage.NextCursor)
	secondPage, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID, BuildingCode: buildingBefore.Code,
		AfterTick: firstPage.NextCursor.Tick, AfterSequence: firstPage.NextCursor.Sequence, Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Facts, 2)
	require.Nil(t, secondPage.NextCursor)

	fromGenesis, targetTick := int64(0), int64(4)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "development-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	if replay.ErrorDetail != nil {
		t.Logf("development replay detail: %s", *replay.ErrorDetail)
	}
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_development_projects SET name = 'tampered'
WHERE world_id = $1 AND code = $2`, worldID, projectCode)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_building_adjustments SET quality_delta_milli = quality_delta_milli + 1
WHERE world_id = $1 AND project_code = $2`, worldID, projectCode)
	require.Error(t, err)

	var snapshotID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_snapshots
WHERE world_id = $1 AND tick = $2 AND simulation_version = $3`,
		worldID, targetTick, service.CitySimulationVersionF7V3).Scan(&snapshotID))
	driftCityDevelopmentProjection(
		t, ctx, worldID, owner.ID, replay.ID, snapshotID, targetTick,
		completed.Tick.StateHash, projectCode,
	)
	drifted, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID, BuildingCode: buildingBefore.Code,
	})
	require.NoError(t, err)
	require.Equal(t, "tampered recovery fixture", drifted.Projects[0].Name)
	require.Equal(t, int64(1), drifted.Adjustments[0].QualityDeltaMilli)

	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "development-recovery",
		ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	if recovery.ErrorDetail != nil {
		t.Logf("development recovery detail: %s", *recovery.ErrorDetail)
	}
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status)
	restored, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID, BuildingCode: buildingBefore.Code,
	})
	require.NoError(t, err)
	require.Equal(t, "First deterministic renovation", restored.Projects[0].Name)
	require.Equal(t, int64(1), restored.Adjustments[0].QualityDeltaMilli)
	restoredLand, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	require.Equal(t, targetQuality, findBuildingByCode(t, restoredLand.Buildings, buildingBefore.Code).QualityMilli)
}

func TestCityF7V2ToV3UpgradeIsAuditedAtomicAndPreservesLand(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-development-upgrade-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(740032)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Development Upgrade City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V2,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	beforeEngine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7V2, beforeEngine.Version)
	require.Equal(t, []string{service.CitySimulationVersionF7V3}, beforeEngine.UpgradeTargets)
	_, err = cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.ErrorIs(t, err, service.ErrCityDevelopmentStateNotFound)
	landBefore, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)

	dryRun, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "development-upgrade-plan",
		TargetVersion: service.CitySimulationVersionF7V3, DryRun: true,
	})
	require.NoError(t, err)
	if dryRun.ErrorDetail != nil {
		t.Logf("development upgrade plan detail: %s", *dryRun.ErrorDetail)
	}
	require.Equal(t, service.CityUpgradeStatusPlanned, dryRun.Status, "%+v", dryRun)
	var developmentProfiles int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM city_development_profiles WHERE world_id = $1`, worldID).Scan(&developmentProfiles))
	require.Zero(t, developmentProfiles)

	applied, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "development-upgrade-apply",
		TargetVersion: service.CitySimulationVersionF7V3,
	})
	require.NoError(t, err)
	if applied.ErrorDetail != nil {
		t.Logf("development upgrade detail: %s", *applied.ErrorDetail)
	}
	require.Equal(t, service.CityUpgradeStatusApplied, applied.Status, "%+v", applied)
	require.NotNil(t, applied.TargetSnapshotID)
	afterEngine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7V3, afterEngine.Version)
	require.Equal(t, []string{service.CitySimulationVersionF7V4}, afterEngine.UpgradeTargets)
	require.Contains(t, afterEngine.Stages, "development")

	development, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	require.Zero(t, development.Profile.ProjectCount)
	require.Zero(t, development.Profile.FactCount)
	require.Zero(t, development.Profile.AdjustmentCount)
	require.Equal(t, int64(0), development.Profile.BaselineTick)
	landAfter, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	require.Equal(t, landBefore.Profile.BaselineHash, landAfter.Profile.BaselineHash)
	require.Equal(t, landBefore.Buildings, landAfter.Buildings)
	require.Equal(t, landBefore.Portals, landAfter.Portals)

	var sourceSnapshots, targetSnapshots int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE simulation_version = $2),
       COUNT(*) FILTER (WHERE simulation_version = $3)
FROM city_snapshots WHERE world_id = $1 AND tick = 0`,
		worldID, service.CitySimulationVersionF7V2, service.CitySimulationVersionF7V3).
		Scan(&sourceSnapshots, &targetSnapshots))
	require.Equal(t, 1, sourceSnapshots)
	require.Equal(t, 1, targetSnapshots)

	expectedZero := int64(0)
	step, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "development-upgraded-step",
		ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7V3, step.Tick.SimulationVersion)
	fromUpgrade, targetTick := int64(0), int64(1)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "development-upgraded-replay",
		FromTick: &fromUpgrade, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_worlds SET simulation_version = $2 WHERE id = $1`,
		worldID, service.CitySimulationVersionF7V2)
	require.Error(t, err)
}

func TestCityVerticalExpansionUpdatesEffectiveFloorsUnitsPortalsAndDistrictCapacity(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-expansion-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(740033)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Vertical Expansion City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V3,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	development, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	require.NotEmpty(t, development.Developers)
	developer := development.Developers[0]
	landBefore, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	buildingBefore, rule := findExpandableBuilding(
		t, landBefore.Buildings, landBefore.ZoningRules, developer.DistrictCode,
	)
	poolBefore := findUnitPoolByBuilding(t, landBefore.UnitPools, buildingBefore.Code)
	physicalBefore, err := cityService.GetPhysicalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	districtBefore := findDistrictByCode(t, physicalBefore.Districts, buildingBefore.DistrictCode)
	targetFloors := buildingBefore.FloorCount + 1

	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	submit := func(expectedTick int64, key, commandType string, payload json.RawMessage) {
		_, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: payload, ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, submitErr)
	}
	step := func(expectedTick int64, key string) *service.CityStepResult {
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, stepErr)
		return result
	}

	submit(0, "vertical-submit", service.CityCommandTypeDevelopmentSubmit,
		marshalPayload(map[string]any{
			"project_type":  service.CityDevelopmentProjectVerticalExpansion,
			"building_code": buildingBefore.Code, "developer_entity_id": developer.EntityID,
			"target_floor_count": targetFloors, "name": "One-floor vertical expansion",
		}))
	submitted := step(0, "vertical-submit-step")
	require.Len(t, submitted.DevelopmentFacts, 1)
	projectCode := submitted.DevelopmentFacts[0].ProjectCode
	projectState, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID, BuildingCode: buildingBefore.Code,
	})
	require.NoError(t, err)
	require.Len(t, projectState.Projects, 1)
	planned := projectState.Projects[0]
	t.Logf("vertical plan: footprint=%d material=%d capital=%d labor=%d employees=%d duration=%d",
		buildingBefore.FootprintAreaSQM, planned.RequiredBasicMaterialUnits,
		planned.RequiredCapitalGoodsUnits, planned.RequiredLaborUnits,
		developer.EmployeeUnits, planned.PlannedDurationTicks)
	require.Equal(t, int32(1), planned.AddedFloorCount)
	require.Equal(t, buildingBefore.FootprintAreaSQM, planned.AddedFloorAreaSQM)
	require.Equal(t, buildingBefore.FootprintAreaSQM/rule.SQMPerCapacityUnit, planned.AddedCapacityUnits)

	submit(1, "vertical-approve", service.CityCommandTypeDevelopmentReview,
		marshalPayload(map[string]any{"project_code": projectCode, "decision": "approve"}))
	_ = step(1, "vertical-approve-step")
	submit(2, "vertical-start", service.CityCommandTypeDevelopmentStart,
		marshalPayload(map[string]any{"project_code": projectCode}))
	submit(2, "vertical-resume", service.CityCommandTypeWorldResume, json.RawMessage(`{}`))
	started := step(2, "vertical-start-step")
	for _, command := range started.Commands {
		if command.Status != service.CityCommandStatusApplied {
			t.Logf("vertical command %s rejected: code=%v result=%v", command.CommandType, command.ErrorCode, command.Result)
		}
	}
	require.Equal(t, 2, started.Tick.AppliedCommandCount)
	require.Len(t, started.DevelopmentFacts, 1)
	require.Equal(t, service.CityDevelopmentFactStarted, started.DevelopmentFacts[0].FactType)

	projectState, err = cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID, BuildingCode: buildingBefore.Code,
	})
	require.NoError(t, err)
	require.Len(t, projectState.Projects, 1)
	active := projectState.Projects[0]
	require.NotNil(t, active.PlannedCompletionTick)
	for expected := int64(3); expected < *active.PlannedCompletionTick-1; expected++ {
		progress := step(expected, fmt.Sprintf("vertical-progress-%d", expected+1))
		require.Len(t, progress.DevelopmentFacts, 1)
		require.Equal(t, service.CityDevelopmentFactProgressed, progress.DevelopmentFacts[0].FactType)
		require.Less(t, progress.DevelopmentFacts[0].ProgressAfterMilli, int64(1000))
	}
	completionExpected := *active.PlannedCompletionTick - 1
	completed := step(completionExpected, "vertical-completed-step")
	require.Len(t, completed.DevelopmentFacts, 1)
	require.Equal(t, service.CityDevelopmentFactCompleted, completed.DevelopmentFacts[0].FactType)
	require.Len(t, completed.BuildingAdjustments, 1)

	landAfter, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	buildingAfter := findBuildingByCode(t, landAfter.Buildings, buildingBefore.Code)
	poolAfter := findUnitPoolByBuilding(t, landAfter.UnitPools, buildingBefore.Code)
	require.Equal(t, targetFloors, buildingAfter.FloorCount)
	require.Equal(t, buildingBefore.TopZ+1, buildingAfter.TopZ)
	require.Equal(t, buildingBefore.FloorAreaSQM+planned.AddedFloorAreaSQM, buildingAfter.FloorAreaSQM)
	require.Equal(t, buildingBefore.CapacityUnits+planned.AddedCapacityUnits, buildingAfter.CapacityUnits)
	require.Equal(t,
		poolBefore.UnitCount+planned.AddedCapacityUnits/poolBefore.CapacityUnitsPerUnit,
		poolAfter.UnitCount,
	)

	portalLayer, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: buildingAfter.Footprint.ChunkX, MaximumX: buildingAfter.Footprint.ChunkX,
		MinimumY: buildingAfter.Footprint.ChunkY, MaximumY: buildingAfter.Footprint.ChunkY,
		Z: buildingAfter.TopZ,
	})
	require.NoError(t, err)
	foundExpansionPortal := false
	for _, portal := range portalLayer.Portals {
		if portal.BuildingCode == buildingAfter.Code && portal.FromZ == buildingBefore.TopZ &&
			portal.ToZ == buildingAfter.TopZ && portal.Bidirectional {
			foundExpansionPortal = true
			break
		}
	}
	require.True(t, foundExpansionPortal)

	physicalAfter, err := cityService.GetPhysicalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	districtAfter := findDistrictByCode(t, physicalAfter.Districts, buildingBefore.DistrictCode)
	switch buildingBefore.PrimaryUse {
	case cityspatial.LandUseResidential:
		require.Equal(t, districtBefore.ResidentialCapacityUnits+planned.AddedCapacityUnits,
			districtAfter.ResidentialCapacityUnits)
	case cityspatial.LandUseCommercial:
		require.Equal(t, districtBefore.CommercialCapacityUnits+planned.AddedCapacityUnits,
			districtAfter.CommercialCapacityUnits)
	case cityspatial.LandUseIndustrial:
		require.Equal(t, districtBefore.IndustrialCapacityUnits+planned.AddedCapacityUnits,
			districtAfter.IndustrialCapacityUnits)
	default:
		t.Fatalf("unsupported building use %q", buildingBefore.PrimaryUse)
	}
}

func TestCityDevelopmentRejectedStartIsAtomicAndCancellationKeepsSunkInputs(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-development-atomic-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(740034)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Development Atomicity City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V3,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	firmID := findEntityIDByType(t, foundation.Entities, service.CityEntityTypeFirm)
	householdID := findEntityIDByType(t, foundation.Entities, service.CityEntityTypeHousehold)
	development, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	require.NotEmpty(t, development.Developers)
	developer := development.Developers[0]
	require.Equal(t, firmID, developer.EntityID)
	landBefore, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	building := findDevelopmentBuilding(t, landBefore.Buildings, developer.DistrictCode)
	targetQuality := building.QualityMilli + 1

	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	submit := func(expectedTick int64, key, commandType string, payload json.RawMessage) {
		_, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: payload, ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, submitErr)
	}
	step := func(expectedTick int64, key string) *service.CityStepResult {
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, stepErr)
		return result
	}

	submit(0, "atomic-submit", service.CityCommandTypeDevelopmentSubmit,
		marshalPayload(map[string]any{
			"project_type":  service.CityDevelopmentProjectRenovation,
			"building_code": building.Code, "developer_entity_id": firmID,
			"target_quality_milli": targetQuality,
		}))
	submitted := step(0, "atomic-submit-step")
	projectCode := submitted.DevelopmentFacts[0].ProjectCode
	submit(1, "atomic-approve", service.CityCommandTypeDevelopmentReview,
		marshalPayload(map[string]any{"project_code": projectCode, "decision": "approve"}))
	_ = step(1, "atomic-approve-step")
	projectState, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID, BuildingCode: building.Code,
	})
	require.NoError(t, err)
	planned := projectState.Projects[0]

	transferPayload := func(fromID, toID, quantity int64) json.RawMessage {
		return marshalPayload(map[string]any{
			"from_entity_id": fromID, "to_entity_id": toID,
			"from_district_code": developer.DistrictCode, "to_district_code": developer.DistrictCode,
			"resource_code": "capital_goods", "quantity_units": quantity,
		})
	}
	submit(2, "atomic-drain-capital", service.CityCommandTypeResourceTransfer,
		transferPayload(firmID, householdID, 100))
	submit(2, "atomic-start-rejected", service.CityCommandTypeDevelopmentStart,
		marshalPayload(map[string]any{"project_code": projectCode}))
	rejected := step(2, "atomic-rejected-step")
	require.Equal(t, 1, rejected.Tick.AppliedCommandCount)
	require.Equal(t, 1, rejected.Tick.RejectedCommandCount)
	require.Empty(t, rejected.DevelopmentFacts)
	require.Equal(t, service.CityCommandStatusRejected, rejected.Commands[1].Status)
	require.NotNil(t, rejected.Commands[1].ErrorCode)
	require.Equal(t, "CITY_DEVELOPMENT_RESOURCE_INSUFFICIENT", *rejected.Commands[1].ErrorCode)
	projectState, err = cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID, BuildingCode: building.Code,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityDevelopmentStatusApproved, projectState.Projects[0].Status)
	require.Equal(t, int64(2), projectState.Profile.FactCount)

	submit(3, "atomic-restore-capital", service.CityCommandTypeResourceTransfer,
		transferPayload(householdID, firmID, 100))
	submit(3, "atomic-start", service.CityCommandTypeDevelopmentStart,
		marshalPayload(map[string]any{"project_code": projectCode}))
	started := step(3, "atomic-started-step")
	require.Equal(t, 2, started.Tick.AppliedCommandCount)
	require.Len(t, started.DevelopmentFacts, 1)
	require.Equal(t, service.CityDevelopmentFactStarted, started.DevelopmentFacts[0].FactType)

	submit(4, "atomic-cancel", service.CityCommandTypeDevelopmentCancel,
		marshalPayload(map[string]any{"project_code": projectCode, "reason": "test cancellation after mobilization"}))
	cancelled := step(4, "atomic-cancelled-step")
	require.Len(t, cancelled.DevelopmentFacts, 1)
	require.Equal(t, service.CityDevelopmentFactCancelled, cancelled.DevelopmentFacts[0].FactType)
	require.Empty(t, cancelled.BuildingAdjustments)
	finalState, err := cityService.GetDevelopmentState(ctx, service.CityDevelopmentQueryInput{
		UserID: owner.ID, WorldID: worldID, BuildingCode: building.Code,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityDevelopmentStatusCancelled, finalState.Projects[0].Status)
	require.Equal(t, int64(4), finalState.Profile.FactCount)
	require.Zero(t, finalState.Profile.AdjustmentCount)

	physical, err := cityService.GetPhysicalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(1_000)-planned.RequiredBasicMaterialUnits,
		findInventoryQuantity(t, physical.Inventories, firmID, developer.DistrictCode, "basic_material"))
	require.Equal(t, int64(100)-planned.RequiredCapitalGoodsUnits,
		findInventoryQuantity(t, physical.Inventories, firmID, developer.DistrictCode, "capital_goods"))
	landAfter, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	require.Equal(t, building, findBuildingByCode(t, landAfter.Buildings, building.Code))
}

func findDevelopmentBuilding(
	t *testing.T,
	buildings []cityspatial.GeneratedBuilding,
	districtCode string,
) cityspatial.GeneratedBuilding {
	t.Helper()
	for _, building := range buildings {
		if building.DistrictCode == districtCode && building.QualityMilli < 1500 {
			return building
		}
	}
	t.Fatalf("no development building in district %q", districtCode)
	return cityspatial.GeneratedBuilding{}
}

func findExpandableBuilding(
	t *testing.T,
	buildings []cityspatial.GeneratedBuilding,
	rules []cityspatial.LandZoningRule,
	districtCode string,
) (cityspatial.GeneratedBuilding, cityspatial.LandZoningRule) {
	t.Helper()
	rulesByUse := make(map[cityspatial.LandUse]cityspatial.LandZoningRule, len(rules))
	for _, rule := range rules {
		rulesByUse[rule.PrimaryUse] = rule
	}
	for _, building := range buildings {
		rule, ok := rulesByUse[building.PrimaryUse]
		if building.DistrictCode == districtCode && ok && building.FloorCount < rule.MaxFloors {
			return building, rule
		}
	}
	t.Fatalf("no expandable building in district %q", districtCode)
	return cityspatial.GeneratedBuilding{}, cityspatial.LandZoningRule{}
}

func findUnitPoolByBuilding(
	t *testing.T,
	pools []cityspatial.GeneratedBuildingUnitPool,
	buildingCode string,
) cityspatial.GeneratedBuildingUnitPool {
	t.Helper()
	for _, pool := range pools {
		if pool.BuildingCode == buildingCode {
			return pool
		}
	}
	t.Fatalf("unit pool for building %q not found", buildingCode)
	return cityspatial.GeneratedBuildingUnitPool{}
}

func findEntityIDByType(
	t *testing.T,
	entities []*service.CityEconomicEntity,
	entityType string,
) int64 {
	t.Helper()
	for _, entity := range entities {
		if entity.EntityType == entityType {
			return entity.ID
		}
	}
	t.Fatalf("entity type %q not found", entityType)
	return 0
}

func findInventoryQuantity(
	t *testing.T,
	inventories []*service.CityInventoryBalance,
	entityID int64,
	districtCode, resourceCode string,
) int64 {
	t.Helper()
	for _, inventory := range inventories {
		if inventory.EntityID == entityID && inventory.DistrictCode == districtCode &&
			inventory.ResourceCode == resourceCode {
			return inventory.QuantityUnits
		}
	}
	t.Fatalf("inventory %d/%s/%s not found", entityID, districtCode, resourceCode)
	return 0
}

func findDistrictByCode(
	t *testing.T,
	districts []*service.CityDistrict,
	code string,
) *service.CityDistrict {
	t.Helper()
	for _, district := range districts {
		if district.Code == code {
			return district
		}
	}
	t.Fatalf("district %q not found", code)
	return nil
}

func findBuildingByCode(
	t *testing.T,
	buildings []cityspatial.GeneratedBuilding,
	code string,
) cityspatial.GeneratedBuilding {
	t.Helper()
	for _, building := range buildings {
		if building.Code == code {
			return building
		}
	}
	t.Fatalf("building %q not found", code)
	return cityspatial.GeneratedBuilding{}
}

func driftCityDevelopmentProjection(
	t *testing.T,
	ctx context.Context,
	worldID, ownerID, replayID, snapshotID, targetTick int64,
	targetHash, projectCode string,
) {
	t.Helper()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	var recoveryRunID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_recovery_runs
    (world_id, requested_by_user_id, client_request_id, request_fingerprint,
     replay_run_id, target_snapshot_id, target_tick, target_state_hash)
VALUES ($1, $2, $3, repeat('d', 64), $4, $5, $6, $7)
RETURNING id`, worldID, ownerID, "f74-test-development-drift", replayID,
		snapshotID, targetTick, targetHash).Scan(&recoveryRunID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SELECT set_config('sub2api.city_recovery_run_id', $1, TRUE)`,
		strconv.FormatInt(recoveryRunID, 10))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE city_development_projects SET name = 'tampered recovery fixture', updated_at = NOW()
WHERE world_id = $1 AND code = $2`, worldID, projectCode)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE city_recovery_runs
SET status = 'failed', error_code = 'TEST_DRIFT',
    error_detail = 'development integration drift fixture', completed_at = NOW()
WHERE id = $1 AND status = 'running'`, recoveryRunID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}
