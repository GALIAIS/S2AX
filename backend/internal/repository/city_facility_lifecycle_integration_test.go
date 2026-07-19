//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCityFacilityLifecycleCommissioningQueriesReplayAndRecovery(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("facility-lifecycle-owner-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("facility-lifecycle-outsider-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(760013)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Facility Lifecycle Foundation", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF8V2,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	currentTick := int64(0)

	physical, err := cityService.GetPhysicalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.NotNil(t, physical.Government)
	require.NotEmpty(t, physical.Firms)
	require.NotEmpty(t, physical.BudgetLines)
	land, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	require.NotEmpty(t, land.Buildings)

	var executor *service.CityFirmState
	var buildingCode string
	for _, firm := range physical.Firms {
		if firm.EmployeeUnits < 3 {
			continue
		}
		for _, building := range land.Buildings {
			if building.DistrictCode == firm.DistrictCode {
				executor = firm
				buildingCode = building.Code
				break
			}
		}
		if executor != nil {
			break
		}
	}
	require.NotNil(t, executor, "expected a staffed firm colocated with a building")

	var budget *service.CityGovernmentBudgetLine
	for _, candidate := range physical.BudgetLines {
		if candidate.AvailableUnits >= 100 {
			budget = candidate
			break
		}
	}
	require.NotNil(t, budget, "expected a government budget line with commissioning capacity")

	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	submit := func(key, commandType string, payload any) *service.CityCommand {
		expectedTick := currentTick
		command, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: marshalPayload(payload),
			ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, submitErr)
		return command
	}
	step := func(key string) *service.CityStepResult {
		expectedTick := currentTick
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, stepErr)
		currentTick++
		require.Equal(t, currentTick, result.Tick.Tick)
		return result
	}

	inventoryQuantity := func(entityID int64, districtCode, resourceCode string) int64 {
		for _, balance := range physical.Inventories {
			if balance.EntityID == entityID && balance.DistrictCode == districtCode &&
				balance.ResourceCode == resourceCode && balance.Status == "active" {
				if balance.QuantityUnits > balance.OpeningQuantityUnits {
					return balance.QuantityUnits
				}
				return balance.OpeningQuantityUnits
			}
		}
		return 0
	}
	prepareResource := func(resourceCode string, requiredUnits int64) int {
		available := inventoryQuantity(executor.EntityID, executor.DistrictCode, resourceCode)
		if available >= requiredUnits {
			return 0
		}
		missing := requiredUnits - available
		var donor *service.CityInventoryBalance
		for _, candidate := range physical.Inventories {
			candidateAvailable := candidate.QuantityUnits
			if candidate.OpeningQuantityUnits > candidateAvailable {
				candidateAvailable = candidate.OpeningQuantityUnits
			}
			if candidate.EntityID != executor.EntityID && candidate.ResourceCode == resourceCode &&
				candidate.Status == "active" && candidateAvailable >= missing {
				donor = candidate
				break
			}
		}
		require.NotNil(t, donor, "expected a donor for %s", resourceCode)
		submit("facility-prepare-"+resourceCode, service.CityCommandTypeResourceTransfer, map[string]any{
			"from_entity_id": donor.EntityID, "to_entity_id": executor.EntityID,
			"from_district_code": donor.DistrictCode,
			"to_district_code":   executor.DistrictCode,
			"resource_code":      resourceCode, "quantity_units": missing,
		})
		return 1
	}
	preparationCount := prepareResource("basic_material", 8)
	preparationCount += prepareResource("capital_goods", 5)

	submit("facility-lifecycle-resume", service.CityCommandTypeWorldResume, map[string]any{})
	submit("facility-lifecycle-register", service.CityCommandTypeFacilityRegister, map[string]any{
		"code": "facility_power_alpha", "name": "Power Alpha",
		"facility_type_code": "power_plant", "building_code": buildingCode,
		"reliability_milli": 1000, "metadata": map[string]any{"scenario": "f8.1"},
	})
	created := step("facility-lifecycle-create-step")
	require.Len(t, created.Commands, preparationCount+2)
	for _, command := range created.Commands {
		require.Equal(t, service.CityCommandStatusApplied, command.Status, "%+v", command)
	}
	require.Len(t, created.FacilityLifecycleFacts, 1)
	require.Equal(t, service.CityFacilityLifecycleFactFacilityInitialized,
		created.FacilityLifecycleFacts[0].FactType)

	submit("facility-lifecycle-capacity", service.CityCommandTypeFacilityCapacityConfigure, map[string]any{
		"facility_code": "facility_power_alpha", "service_code": "electric_power",
		"installed_capacity_units": 1000, "availability_milli": 1000,
		"expected_version": 0, "metadata": map[string]any{},
	})
	submit("facility-lifecycle-public-status", service.CityCommandTypeFacilityStatusTransition, map[string]any{
		"facility_code":    "facility_power_alpha",
		"to_status":        service.CityFacilityStatusOperational,
		"expected_version": 1, "metadata": map[string]any{},
	})
	submit("facility-lifecycle-staffing", service.CityCommandTypeFacilityStaffingConfigure, map[string]any{
		"code": "staff_power_alpha", "facility_code": "facility_power_alpha",
		"role_code": "operator", "subject_kind": "entity",
		"subject_code": executor.EntityCode, "assigned_units": 1,
		"status": "active", "expected_version": 0,
		"expected_facility_version": 2, "metadata": map[string]any{},
	})
	submit("facility-lifecycle-demand", service.CityCommandTypeServiceDemandConfigure, map[string]any{
		"code": "demand_power_alpha", "service_code": "electric_power",
		"subject_kind": "district", "subject_code": executor.DistrictCode,
		"requested_units_per_tick": 800, "priority": 500,
		"status":           service.CityServiceProjectionStatusActive,
		"expected_version": 0, "metadata": map[string]any{},
	})
	submit("facility-lifecycle-connection", service.CityCommandTypeServiceConnectionConfigure, map[string]any{
		"code": "connection_power_alpha", "facility_code": "facility_power_alpha",
		"service_code": "electric_power", "demand_code": "demand_power_alpha",
		"max_flow_units_per_tick": 1000, "loss_milli": 0, "preference": 500,
		"status":           service.CityServiceProjectionStatusActive,
		"expected_version": 0, "metadata": map[string]any{},
	})
	configured := step("facility-lifecycle-configure-step")
	require.Len(t, configured.Commands, 5)
	for _, command := range configured.Commands {
		require.Equal(t, service.CityCommandStatusApplied, command.Status, "%+v", command)
	}
	require.Len(t, configured.FacilityLifecycleFacts, 2)
	require.Len(t, configured.ServiceSettlements, 1)
	require.Zero(t, configured.ServiceSettlements[0].DeliveredUnits)
	require.Equal(t, int64(800), configured.ServiceSettlements[0].ShortageUnits)

	statePage, err := cityService.ListCityFacilityLifecycleStates(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, statePage.Items, 1)
	preCommissionState := statePage.Items[0]
	require.Equal(t, service.CityFacilityLifecycleStatusUncommissioned,
		preCommissionState.LifecycleStatus)
	require.Equal(t, int64(1), preCommissionState.StaffRequiredUnits)
	require.Equal(t, int64(1), preCommissionState.StaffAssignedUnits)
	require.Zero(t, preCommissionState.EffectiveFactorMilli)
	serviceFacilities, err := cityService.ListCityServiceFacilities(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID, FacilityCode: "facility_power_alpha", Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, serviceFacilities.Items, 1)
	require.Len(t, serviceFacilities.Items[0].Capacities, 1)
	require.Zero(t, serviceFacilities.Items[0].Capacities[0].DispatchCapacityUnits)

	validSchedule := submit("facility-lifecycle-schedule", service.CityCommandTypeFacilityOperationSchedule, map[string]any{
		"code": "commission_power_alpha", "facility_code": "facility_power_alpha",
		"operation_type":       service.CityFacilityOperationCommission,
		"sponsor_entity_code":  physical.Government.EntityCode,
		"executor_entity_code": executor.EntityCode, "budget_code": budget.Code,
		"planned_start_tick":        currentTick + 1,
		"expected_facility_version": 3, "metadata": map[string]any{},
	})
	staleSchedule := submit("facility-lifecycle-schedule-stale", service.CityCommandTypeFacilityOperationSchedule, map[string]any{
		"code": "commission_power_stale", "facility_code": "facility_power_alpha",
		"operation_type":       service.CityFacilityOperationCommission,
		"sponsor_entity_code":  physical.Government.EntityCode,
		"executor_entity_code": executor.EntityCode, "budget_code": budget.Code,
		"planned_start_tick":        currentTick + 1,
		"expected_facility_version": 3, "metadata": map[string]any{},
	})
	scheduled := step("facility-lifecycle-schedule-step")
	require.Len(t, scheduled.Commands, 2)
	require.Equal(t, validSchedule.ID, scheduled.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusApplied, scheduled.Commands[0].Status)
	require.Equal(t, staleSchedule.ID, scheduled.Commands[1].ID)
	require.Equal(t, service.CityCommandStatusRejected, scheduled.Commands[1].Status)
	require.NotNil(t, scheduled.Commands[1].ErrorCode)
	require.Equal(t, "CITY_FACILITY_LIFECYCLE_VERSION_CONFLICT", *scheduled.Commands[1].ErrorCode)
	require.Len(t, scheduled.FacilityLifecycleFacts, 1)
	require.Equal(t, service.CityFacilityLifecycleFactOperationScheduled,
		scheduled.FacilityLifecycleFacts[0].FactType)

	operations, err := cityService.ListCityFacilityOperations(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, operations.Items, 1)
	require.Equal(t, service.CityFacilityOperationStatusPlanned, operations.Items[0].Status)
	require.Equal(t, int64(100), operations.Items[0].BudgetCommittedUnits)
	budgetMovements, err := cityService.ListCityFacilityBudgetMovements(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, budgetMovements.Items, 1)
	require.Equal(t, "commit", budgetMovements.Items[0].MovementType)

	submit("facility-lifecycle-start", service.CityCommandTypeFacilityOperationStart, map[string]any{
		"operation_code":             "commission_power_alpha",
		"expected_operation_version": 1, "expected_facility_version": 4,
		"metadata": map[string]any{},
	})
	started := step("facility-lifecycle-start-step")
	require.Len(t, started.Commands, 1)
	require.Equal(t, service.CityCommandStatusApplied, started.Commands[0].Status)
	require.Len(t, started.Journals, 1)
	require.Len(t, started.ResourceOperations, 2)
	require.Len(t, started.FacilityLifecycleFacts, 2)
	require.Equal(t, service.CityFacilityLifecycleFactOperationStarted,
		started.FacilityLifecycleFacts[0].FactType)
	require.Equal(t, service.CityFacilityLifecycleFactOperationProgressed,
		started.FacilityLifecycleFacts[1].FactType)
	require.Len(t, started.ServiceSettlements, 1)
	require.Zero(t, started.ServiceSettlements[0].DeliveredUnits)

	progressed := step("facility-lifecycle-progress-step")
	require.Empty(t, progressed.Commands)
	require.Len(t, progressed.FacilityLifecycleFacts, 1)
	require.Equal(t, service.CityFacilityLifecycleFactOperationProgressed,
		progressed.FacilityLifecycleFacts[0].FactType)
	completed := step("facility-lifecycle-complete-step")
	require.Empty(t, completed.Commands)
	require.NotEmpty(t, completed.FacilityLifecycleFacts)
	require.Equal(t, service.CityFacilityLifecycleFactOperationCompleted,
		completed.FacilityLifecycleFacts[0].FactType)
	require.Len(t, completed.ServiceSettlements, 1)
	require.Equal(t, int64(800), completed.ServiceSettlements[0].DeliveredUnits)
	require.Zero(t, completed.ServiceSettlements[0].ShortageUnits)

	catalog, err := cityService.GetCityFacilityLifecycleCatalog(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID})
	require.NoError(t, err)
	require.Equal(t, service.CityServiceAvailabilityAvailable, catalog.Availability)
	require.NotNil(t, catalog.Profile)
	require.NotNil(t, catalog.Overview)
	require.Equal(t, int64(1), catalog.Overview.FacilityCount)
	require.Equal(t, int64(1), catalog.Overview.OperationalCount)
	require.Zero(t, catalog.Overview.OpenOperationCount)
	require.Equal(t, int64(100), catalog.Overview.SpentBudgetUnits)
	require.Equal(t, int64(1), catalog.Overview.ActiveStaffAssignedUnits)

	statePage, err = cityService.ListCityFacilityLifecycleStates(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, statePage.Items, 1)
	operationalState := statePage.Items[0]
	require.Equal(t, service.CityFacilityLifecycleStatusOperational, operationalState.LifecycleStatus)
	require.Equal(t, 1000, operationalState.OperationFactorMilli)
	require.Greater(t, operationalState.EffectiveFactorMilli, 0)
	require.NotNil(t, operationalState.LastMaintenanceTick)
	require.Nil(t, operationalState.ActiveOperationCode)

	operations, err = cityService.ListCityFacilityOperations(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, operations.Items, 1)
	require.Equal(t, service.CityFacilityOperationStatusCompleted, operations.Items[0].Status)
	require.Equal(t, 1000, operations.Items[0].ProgressMilli)
	require.Equal(t, int64(100), operations.Items[0].BudgetSpentUnits)
	staffing, err := cityService.ListCityFacilityStaffAssignments(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, staffing.Items, 1)
	require.Equal(t, int64(1), staffing.Items[0].EffectiveUnits)
	incidents, err := cityService.ListCityFacilityIncidents(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	budgetMovements, err = cityService.ListCityFacilityBudgetMovements(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, budgetMovements.Items, 2)
	require.Equal(t, "spend", budgetMovements.Items[1].MovementType)

	factFirstPage, err := cityService.ListCityFacilityLifecycleFacts(ctx,
		service.CityFacilityLifecycleQueryInput{
			UserID: owner.ID, WorldID: worldID, FacilityCode: "facility_power_alpha", Limit: 2,
		})
	require.NoError(t, err)
	require.Len(t, factFirstPage.Items, 2)
	require.NotNil(t, factFirstPage.NextCursor)
	factSecondPage, err := cityService.ListCityFacilityLifecycleFacts(ctx,
		service.CityFacilityLifecycleQueryInput{
			UserID: owner.ID, WorldID: worldID, FacilityCode: "facility_power_alpha",
			AfterTick:     factFirstPage.NextCursor.Tick,
			AfterSequence: factFirstPage.NextCursor.Sequence, Limit: 100,
		})
	require.NoError(t, err)
	require.NotEmpty(t, factSecondPage.Items)
	require.Equal(t, catalog.Profile.FactCount,
		int64(len(factFirstPage.Items)+len(factSecondPage.Items)))
	serviceFacilities, err = cityService.ListCityServiceFacilities(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID, FacilityCode: "facility_power_alpha", Limit: 10,
	})
	require.NoError(t, err)
	require.Equal(t, int64(operationalState.EffectiveFactorMilli),
		serviceFacilities.Items[0].Capacities[0].DispatchCapacityUnits)
	require.Equal(t, serviceFacilities.Items[0].Capacities[0].DispatchCapacityUnits,
		catalog.Overview.EffectiveDispatchUnits)

	_, err = cityService.GetCityFacilityLifecycleCatalog(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: outsider.ID, WorldID: worldID})
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)
	_, err = integrationDB.ExecContext(ctx,
		`SELECT assert_city_facility_lifecycle_foundation($1)`, worldID)
	require.NoError(t, err)

	canonicalCatalog := catalog
	canonicalStates := statePage
	canonicalOperations := operations
	canonicalStaffing := staffing
	canonicalIncidents := incidents
	canonicalBudgetMovements := budgetMovements
	canonicalFacts, err := cityService.ListCityFacilityLifecycleFacts(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 100})
	require.NoError(t, err)
	canonicalServiceFacilities := serviceFacilities

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "facility-lifecycle-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	if replay.ErrorDetail != nil {
		t.Logf("facility lifecycle replay detail: %s", *replay.ErrorDetail)
	}
	if replay.DivergenceTick != nil || replay.DivergencePath != nil {
		t.Logf("facility lifecycle replay divergence: tick=%v path=%v",
			replay.DivergenceTick, replay.DivergencePath)
		if replay.DivergenceTick != nil {
			t.Logf("facility lifecycle replay divergence tick value: %d", *replay.DivergenceTick)
		}
		if replay.DivergencePath != nil {
			t.Logf("facility lifecycle replay divergence path value: %s", *replay.DivergencePath)
		}
	}
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "facility-lifecycle-recovery",
		ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	if recovery.ErrorDetail != nil {
		t.Logf("facility lifecycle recovery detail: %s", *recovery.ErrorDetail)
	}
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)

	restoredCatalog, err := cityService.GetCityFacilityLifecycleCatalog(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID})
	require.NoError(t, err)
	restoredStates, err := cityService.ListCityFacilityLifecycleStates(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	restoredOperations, err := cityService.ListCityFacilityOperations(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	restoredStaffing, err := cityService.ListCityFacilityStaffAssignments(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	restoredIncidents, err := cityService.ListCityFacilityIncidents(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	restoredBudgetMovements, err := cityService.ListCityFacilityBudgetMovements(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	restoredFacts, err := cityService.ListCityFacilityLifecycleFacts(ctx,
		service.CityFacilityLifecycleQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 100})
	require.NoError(t, err)
	restoredServiceFacilities, err := cityService.ListCityServiceFacilities(ctx,
		service.CityServiceQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, canonicalCatalog, restoredCatalog)
	require.Equal(t, canonicalStates, restoredStates)
	require.Equal(t, canonicalOperations, restoredOperations)
	require.Equal(t, canonicalStaffing, restoredStaffing)
	require.Equal(t, canonicalIncidents, restoredIncidents)
	require.Equal(t, canonicalBudgetMovements, restoredBudgetMovements)
	require.Equal(t, canonicalFacts, restoredFacts)
	require.Equal(t, canonicalServiceFacilities, restoredServiceFacilities)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_facility_lifecycle_states
SET condition_milli = condition_milli - 1
WHERE world_id = $1`, worldID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `
DELETE FROM city_facility_lifecycle_facts WHERE world_id = $1`, worldID)
	require.Error(t, err)

	submit("physical-network-pause", service.CityCommandTypeWorldPause, map[string]any{})
	paused := step("physical-network-pause-step")
	require.Len(t, paused.Commands, 1)
	require.Equal(t, service.CityCommandStatusApplied, paused.Commands[0].Status)
	upgradeTick := currentTick
	physicalUpgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "physical-network-upgrade",
		TargetVersion: service.CitySimulationVersionF8V3,
	})
	require.NoError(t, err)
	if physicalUpgrade.ErrorDetail != nil {
		t.Logf("physical-network upgrade detail: %s", *physicalUpgrade.ErrorDetail)
	}
	require.Equal(t, service.CityUpgradeStatusApplied, physicalUpgrade.Status, "%+v", physicalUpgrade)
	engine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF8V3, engine.Version)
	var baselineNetworks, baselineNodes, baselineEdges int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT network_count, node_count, edge_count
FROM city_physical_network_profiles WHERE world_id = $1`, worldID).Scan(
		&baselineNetworks, &baselineNodes, &baselineEdges,
	))
	require.EqualValues(t, 1, baselineNetworks)
	require.EqualValues(t, 2, baselineNodes)
	require.EqualValues(t, 1, baselineEdges)

	submit("physical-network-resume", service.CityCommandTypeWorldResume, map[string]any{})
	routed := step("physical-network-routed-step")
	require.Len(t, routed.Commands, 1)
	require.Equal(t, service.CityCommandStatusApplied, routed.Commands[0].Status)
	require.Len(t, routed.ServiceSettlements, 1)
	require.Len(t, routed.ServiceAllocations, 1)
	require.Len(t, routed.PhysicalNetworkFacts, 1)
	require.Len(t, routed.PhysicalNetworkBatches, 1)
	require.Len(t, routed.PhysicalNetworkPaths, 1)
	require.Len(t, routed.PhysicalNetworkSegments, 1)
	routedAllocation := routed.ServiceAllocations[0]
	require.NotNil(t, routedAllocation.NetworkReceivedUnits)
	require.NotNil(t, routedAllocation.NetworkLossUnits)
	require.NotNil(t, routedAllocation.ConnectionLossUnits)
	require.NotNil(t, routedAllocation.NetworkPathCount)
	require.EqualValues(t, 800, *routedAllocation.NetworkReceivedUnits)
	require.Zero(t, *routedAllocation.NetworkLossUnits)
	require.Zero(t, *routedAllocation.ConnectionLossUnits)
	require.Equal(t, 1, *routedAllocation.NetworkPathCount)
	var factCount, batchCount, pathCount, segmentCount int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT fact_count, batch_count, path_count, segment_count
FROM city_physical_network_profiles WHERE world_id = $1`, worldID).Scan(
		&factCount, &batchCount, &pathCount, &segmentCount,
	))
	require.EqualValues(t, 1, factCount)
	require.EqualValues(t, 1, batchCount)
	require.EqualValues(t, 1, pathCount)
	require.EqualValues(t, 1, segmentCount)
	physicalCatalog, err := cityService.GetCityPhysicalNetworkCatalog(ctx,
		service.CityPhysicalNetworkQueryInput{UserID: owner.ID, WorldID: worldID})
	require.NoError(t, err)
	require.Equal(t, service.CityServiceAvailabilityAvailable, physicalCatalog.Availability)
	require.NotNil(t, physicalCatalog.Profile)
	require.NotNil(t, physicalCatalog.Overview)
	require.EqualValues(t, 1, physicalCatalog.Profile.NetworkCount)
	require.EqualValues(t, 800, physicalCatalog.Overview.LatestDispatchedUnits)
	require.EqualValues(t, 800, physicalCatalog.Overview.LatestNetworkReceivedUnits)
	physicalNetworks, err := cityService.ListCityPhysicalNetworks(ctx,
		service.CityPhysicalNetworkQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, physicalNetworks.Items, 1)
	physicalNodes, err := cityService.ListCityPhysicalNetworkNodes(ctx,
		service.CityPhysicalNetworkQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, physicalNodes.Items, 2)
	physicalEdges, err := cityService.ListCityPhysicalNetworkEdges(ctx,
		service.CityPhysicalNetworkQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, physicalEdges.Items, 1)
	physicalFlows, err := cityService.ListCityPhysicalNetworkFlows(ctx,
		service.CityPhysicalNetworkQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, physicalFlows.Items, 1)
	require.Len(t, physicalFlows.Items[0].Paths, 1)
	require.Len(t, physicalFlows.Items[0].Segments, 1)
	physicalFacts, err := cityService.ListCityPhysicalNetworkFacts(ctx,
		service.CityPhysicalNetworkQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, physicalFacts.Items, 1)
	require.Equal(t, service.CityPhysicalNetworkFactFlowSettled, physicalFacts.Items[0].FactType)
	var sourceNodeCode, sinkNodeCode string
	for _, node := range physicalNodes.Items {
		switch node.Role {
		case service.CityNetworkNodeRoleSupply:
			sourceNodeCode = node.Code
		case service.CityNetworkNodeRoleDemand:
			sinkNodeCode = node.Code
		}
	}
	require.NotEmpty(t, sourceNodeCode)
	require.NotEmpty(t, sinkNodeCode)
	physicalDiagnostics, err := cityService.GetCityPhysicalNetworkDiagnostics(ctx,
		service.CityPhysicalNetworkQueryInput{
			UserID: owner.ID, WorldID: worldID, NetworkCode: physicalNetworks.Items[0].Code,
			SourceNodeCode: sourceNodeCode, SinkNodeCode: sinkNodeCode, ProbeUnits: 100,
		})
	require.NoError(t, err)
	require.Equal(t, service.CityServiceAvailabilityAvailable, physicalDiagnostics.Availability)
	require.Equal(t, 1, physicalDiagnostics.ComponentCount)
	require.Zero(t, physicalDiagnostics.IsolatedNodeCount)
	require.Zero(t, physicalDiagnostics.ServiceIslandCount)
	require.Len(t, physicalDiagnostics.EdgeDiagnostics, 1)
	require.NotNil(t, physicalDiagnostics.Route)
	require.True(t, physicalDiagnostics.Route.Reachable)
	require.EqualValues(t, 100, physicalDiagnostics.Route.NetworkReceivedUnits)
	require.Len(t, physicalDiagnostics.Route.Paths, 1)
	_, err = integrationDB.ExecContext(ctx, `SELECT assert_city_physical_network_foundation($1)`, worldID)
	require.NoError(t, err)

	physicalTargetTick := currentTick
	physicalReplay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "physical-network-replay",
		FromTick: &upgradeTick, TargetTick: &physicalTargetTick,
	})
	require.NoError(t, err)
	if physicalReplay.ErrorDetail != nil {
		t.Logf("physical-network replay detail: %s", *physicalReplay.ErrorDetail)
	}
	require.Equal(t, service.CityReplayStatusVerified, physicalReplay.Status, "%+v", physicalReplay)
	physicalRecovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "physical-network-recovery",
		ReplayRunID: physicalReplay.ID,
	})
	require.NoError(t, err)
	if physicalRecovery.ErrorDetail != nil {
		t.Logf("physical-network recovery detail: %s", *physicalRecovery.ErrorDetail)
	}
	require.Equal(t, service.CityRecoveryStatusApplied, physicalRecovery.Status, "%+v", physicalRecovery)
	var restoredPhysicalFacts, restoredBatches, restoredPaths, restoredSegments int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT fact_count, batch_count, path_count, segment_count
FROM city_physical_network_profiles WHERE world_id = $1`, worldID).Scan(
		&restoredPhysicalFacts, &restoredBatches, &restoredPaths, &restoredSegments,
	))
	require.Equal(t, factCount, restoredPhysicalFacts)
	require.Equal(t, batchCount, restoredBatches)
	require.Equal(t, pathCount, restoredPaths)
	require.Equal(t, segmentCount, restoredSegments)
	restoredDiagnostics, err := cityService.GetCityPhysicalNetworkDiagnostics(ctx,
		service.CityPhysicalNetworkQueryInput{
			UserID: owner.ID, WorldID: worldID, NetworkCode: physicalNetworks.Items[0].Code,
			SourceNodeCode: sourceNodeCode, SinkNodeCode: sinkNodeCode, ProbeUnits: 100,
		})
	require.NoError(t, err)
	require.Equal(t, physicalDiagnostics.ComponentCount, restoredDiagnostics.ComponentCount)
	require.Equal(t, physicalDiagnostics.EdgeDiagnostics, restoredDiagnostics.EdgeDiagnostics)
	require.Equal(t, physicalDiagnostics.Route, restoredDiagnostics.Route)
	_, err = integrationDB.ExecContext(ctx, `SELECT assert_city_physical_network_foundation($1)`, worldID)
	require.NoError(t, err)

	network := physicalNetworks.Items[0]
	var supplyNode service.CityPhysicalNetworkNode
	for _, node := range physicalNodes.Items {
		if node.Role == service.CityNetworkNodeRoleSupply {
			supplyNode = node
			break
		}
	}
	require.NotEmpty(t, supplyNode.Code)
	require.NotNil(t, supplyNode.CapacityCode)
	edge := physicalEdges.Items[0]
	submit("physical-network-configure", service.CityCommandTypePhysicalNetworkConfigure, map[string]any{
		"code": network.Code, "name": "Explicit power grid",
		"service_code": network.ServiceCode, "status": service.CityNetworkStatusActive,
		"expected_version": network.Version, "metadata": map[string]any{"operator": "city"},
	})
	submit("physical-node-configure", service.CityCommandTypePhysicalNodeConfigure, map[string]any{
		"code": supplyNode.Code, "network_code": supplyNode.NetworkCode,
		"role": supplyNode.Role, "capacity_code": *supplyNode.CapacityCode,
		"district_code": *supplyNode.DistrictCode, "building_code": *supplyNode.BuildingCode,
		"world_x": int64(0), "world_y": int64(0), "world_z": 0,
		"status":           service.CityNetworkNodeStatusActive,
		"expected_version": supplyNode.Version, "metadata": map[string]any{"kind": "generator"},
	})
	submit("physical-edge-configure", service.CityCommandTypePhysicalEdgeConfigure, map[string]any{
		"code": edge.Code, "network_code": edge.NetworkCode,
		"from_node_code": edge.FromNodeCode, "to_node_code": edge.ToNodeCode,
		"direction": edge.Direction, "installed_capacity_units": edge.InstalledCapacityUnits,
		"availability_milli": 900, "loss_milli": edge.LossMilli,
		"base_cost_units": edge.BaseCostUnits, "status": service.CityNetworkEdgeStatusActive,
		"expected_version": edge.Version, "metadata": map[string]any{"corridor": "primary"},
	})
	submit("physical-edge-isolate", service.CityCommandTypePhysicalEdgeTransition, map[string]any{
		"edge_code": edge.Code, "to_status": service.CityNetworkEdgeStatusIsolated,
		"expected_version": edge.Version + 1, "metadata": map[string]any{"reason": "test_isolation"},
	})
	isolated := step("physical-network-command-step")
	require.Len(t, isolated.Commands, 4)
	for _, command := range isolated.Commands {
		require.Equal(t, service.CityCommandStatusApplied, command.Status)
	}
	require.Len(t, isolated.PhysicalNetworkFacts, 4)
	require.Empty(t, isolated.PhysicalNetworkBatches)
	require.Len(t, isolated.ServiceSettlements, 1)
	require.Zero(t, isolated.ServiceSettlements[0].DeliveredUnits)
	require.Equal(t, isolated.ServiceSettlements[0].RequestedUnits,
		isolated.ServiceSettlements[0].ShortageUnits)

	submit("physical-edge-reactivate", service.CityCommandTypePhysicalEdgeTransition, map[string]any{
		"edge_code": edge.Code, "to_status": service.CityNetworkEdgeStatusActive,
		"expected_version": edge.Version + 2, "metadata": map[string]any{"reason": "test_reactivation"},
	})
	reactivated := step("physical-network-reactivate-step")
	require.Len(t, reactivated.Commands, 1)
	require.Equal(t, service.CityCommandStatusApplied, reactivated.Commands[0].Status)
	require.Len(t, reactivated.PhysicalNetworkFacts, 2)
	require.Len(t, reactivated.PhysicalNetworkBatches, 1)
	require.Len(t, reactivated.ServiceAllocations, 1)
	require.EqualValues(t, 800, reactivated.ServiceAllocations[0].DeliveredUnits)

	commandTargetTick := currentTick
	commandReplay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "physical-network-command-replay",
		FromTick: &physicalTargetTick, TargetTick: &commandTargetTick,
	})
	require.NoError(t, err)
	if commandReplay.ErrorDetail != nil {
		t.Logf("physical-network command replay detail: %s", *commandReplay.ErrorDetail)
	}
	require.Equal(t, service.CityReplayStatusVerified, commandReplay.Status, "%+v", commandReplay)
	commandRecovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "physical-network-command-recovery",
		ReplayRunID: commandReplay.ID,
	})
	require.NoError(t, err)
	if commandRecovery.ErrorDetail != nil {
		t.Logf("physical-network command recovery detail: %s", *commandRecovery.ErrorDetail)
	}
	require.Equal(t, service.CityRecoveryStatusApplied, commandRecovery.Status, "%+v", commandRecovery)
	_, err = integrationDB.ExecContext(ctx, `SELECT assert_city_physical_network_foundation($1)`, worldID)
	require.NoError(t, err)
}
