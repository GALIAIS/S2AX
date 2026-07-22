//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV21EffectiveCapacityIsForwardOnlyAndRecoverable(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v21-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(21_000_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V21 Effective Capacity", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV21,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	initial, err := cityService.GetCityOpenWorldEffectiveCapacityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "sub2api-open-world-effective-capacity", initial.Policy.ProfileID)
	require.Equal(t, "v19_edge_corridor_mapping_v1", initial.Policy.TopologyContract)
	require.Equal(t, "v20_corridor_segment_ordinal_1_v1", initial.Policy.AssetContract)
	require.Equal(t, "effective_infrastructure_capacity_v1", initial.Policy.AdmissionContract)
	require.Equal(t, "next_tick_after_command_v1", initial.Policy.VisibilityContract)
	require.Empty(t, initial.Admissions)

	engine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV21, engine.Version)
	require.Contains(t, engine.Stages, "open_world_effective_capacity")
	require.NotNil(t, engine.VersionVector)
	contentCatalog := requireCityOpenWorldVersionBinding(t, engine.VersionVector, "content_catalog")
	require.Equal(t, "sub2api-open-world-effective-capacity-catalog", contentCatalog.BundleID)

	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	currentTick := int64(0)
	step := func(key string, expectedCommands int) *service.CityStepResult {
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.NotNil(t, result.Tick)
		require.Equal(t, currentTick+1, result.Tick.Tick)
		require.Len(t, result.Commands, expectedCommands)
		currentTick = result.Tick.Tick
		return result
	}
	submit := func(key, commandType string, payload json.RawMessage) {
		_, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: payload, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, submitErr)
	}

	submit("v21-create-actor", service.CityCommandTypeOpenWorldActorCreate, marshalPayload(map[string]any{
		"archetype_code": "urban_apprentice", "name": "Capacity Tester",
	}))
	step("v21-create-actor-step", 1)
	actors, err := cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	actorCode := actors[0].Code

	mobility, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	network, err := cityService.GetCityOpenWorldSpatialNetworkState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	infrastructure, err := cityService.GetCityOpenWorldInfrastructureState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	walkAssets := cityOpenWorldV21WalkSegmentAssets(t, mobility, network, infrastructure)
	require.NotEmpty(t, walkAssets)

	// Close every walk segment before a demand is created. The following request
	// has no eligible walk path; restoring those assets affects admission only in
	// the *next* automatic scheduling tick, never the command's own tick.
	for index, assetCode := range walkAssets {
		submit(fmt.Sprintf("v21-maintain-walk-%d", index), service.CityCommandTypeOpenWorldInfrastructureAssetTransition,
			marshalPayload(map[string]any{
				"asset_code": assetCode, "state": "maintenance", "reason_code": "test.capacity_pause",
			}))
	}
	step("v21-maintain-walk-step", len(walkAssets))
	maintenanceTick := currentTick
	maintenance, err := cityService.GetCityOpenWorldInfrastructureState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	for _, assetCode := range walkAssets {
		state := requireCityOpenWorldV20InfrastructureState(t, maintenance, assetCode)
		require.Equal(t, "maintenance", state.State)
		require.Equal(t, int64(0), state.CapacityMilli)
		require.Equal(t, maintenanceTick, state.EffectiveTick)
	}
	beforePlayerDemand, err := cityService.GetCityOpenWorldEffectiveCapacityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	admissionsBeforePlayerDemand := beforePlayerDemand.Policy.AdmissionCount

	submit("v21-walk-request", service.CityCommandTypeOpenWorldActorMobilityRequest, marshalPayload(map[string]any{
		"actor_code": actorCode, "destination_hub_code": "hub.interchange.central",
		"mode_code": "walk", "purpose_code": "commute", "requested_units": 1,
	}))
	step("v21-walk-request-step", 1)
	step("v21-walk-blocked-schedule-step", 0)
	blocked, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	playerDemand := requireCityOpenWorldV21DemandForActor(t, blocked, actorCode)
	require.Equal(t, "pending", playerDemand.Status)
	require.Empty(t, cityOpenWorldV21RoutesForDemand(blocked, playerDemand.Code))
	blockedCapacity, err := cityService.GetCityOpenWorldEffectiveCapacityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, admissionsBeforePlayerDemand, blockedCapacity.Policy.AdmissionCount)

	for index, assetCode := range walkAssets {
		submit(fmt.Sprintf("v21-restore-walk-%d", index), service.CityCommandTypeOpenWorldInfrastructureAssetTransition,
			marshalPayload(map[string]any{
				"asset_code": assetCode, "state": "operational", "reason_code": "test.capacity_restore",
			}))
	}
	step("v21-restore-walk-command-step", len(walkAssets))
	restoreTick := currentTick
	stillBlocked, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	stillBlockedDemand := requireCityOpenWorldV21DemandForActor(t, stillBlocked, actorCode)
	require.Equal(t, "pending", stillBlockedDemand.Status, "the V20 command becomes visible to V21 on the next tick")
	require.Empty(t, cityOpenWorldV21RoutesForDemand(stillBlocked, playerDemand.Code))

	step("v21-walk-schedule-after-restore", 0)
	scheduled, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	scheduledDemand := requireCityOpenWorldV21DemandForActor(t, scheduled, actorCode)
	require.Equal(t, "scheduled", scheduledDemand.Status)
	playerRoute := requireCityOpenWorldV21RouteForDemand(t, scheduled, scheduledDemand.Code)
	playerAllocations := cityOpenWorldV21AllocationsForRoute(scheduled, playerRoute.Code)
	require.NotEmpty(t, playerAllocations)

	admitted, err := cityService.GetCityOpenWorldEffectiveCapacityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	playerAdmissions := cityOpenWorldV21AdmissionsForRoute(admitted, playerRoute.Code)
	require.Len(t, playerAdmissions, len(playerAllocations))
	require.GreaterOrEqual(t, admitted.Policy.AdmissionCount, admissionsBeforePlayerDemand+int64(len(playerAdmissions)))
	require.Equal(t, admitted.Policy.AdmissionCount+1, admitted.Policy.Revision)
	for _, admission := range playerAdmissions {
		require.Equal(t, playerRoute.Code, admission.RouteCode)
		require.Equal(t, playerRoute.DepartureTick, admission.DepartureTick)
		require.Equal(t, "operational", admission.AssetState)
		require.Equal(t, int64(1000), admission.CapacityMilli)
		require.NotNil(t, admission.StateSourceFact)
		require.Equal(t, restoreTick, admission.StateSourceFact.Tick)
		require.GreaterOrEqual(t, admission.StateSourceFact.Sequence, int64(1))
		require.Equal(t, admission.BaselineCapacityUnitsPerTick, admission.EffectiveCapacityUnitsPerTick)
	}

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v21-effective-capacity-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v21-effective-capacity-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restoredCapacity, err := cityService.GetCityOpenWorldEffectiveCapacityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, admitted, restoredCapacity)
}

func TestCityOpenWorldV20UpgradeToV21DoesNotBackfillHistoricalAllocations(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v20-v21-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(21_000_002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V20 to V21 Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV20,
		StyleProfileID:    "cn.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	currentTick := int64(0)
	submit := func(key, commandType string, payload json.RawMessage) {
		_, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: payload, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, submitErr)
	}
	step := func(key string, expectedCommands int) {
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.Len(t, result.Commands, expectedCommands)
		currentTick = result.Tick.Tick
	}
	submit("v20-v21-create-actor", service.CityCommandTypeOpenWorldActorCreate,
		json.RawMessage(`{"archetype_code":"urban_apprentice","name":"Upgrade Capacity Tester"}`))
	step("v20-v21-create-actor-step", 1)
	actors, err := cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	submit("v20-v21-request", service.CityCommandTypeOpenWorldActorMobilityRequest, json.RawMessage(fmt.Sprintf(
		`{"actor_code":%q,"destination_hub_code":"hub.interchange.central","mode_code":"walk","purpose_code":"commute","requested_units":1}`,
		actors[0].Code)))
	step("v20-v21-request-step", 1)
	step("v20-v21-schedule-step", 0)
	legacy, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.NotEmpty(t, legacy.Allocations)

	upgradeTick := currentTick
	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v20-v21-effective-capacity-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV21,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)
	engine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV21, engine.Version)
	require.Equal(t, upgradeTick, engine.VersionVector.BaselineTick)

	effective, err := cityService.GetCityOpenWorldEffectiveCapacityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, upgradeTick, effective.Policy.BaselineTick)
	require.Empty(t, effective.Admissions, "V20 allocations must remain historical V9 evidence")
	afterUpgrade, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, legacy.Allocations, afterUpgrade.Allocations)
}

func cityOpenWorldV21WalkSegmentAssets(
	t *testing.T,
	mobility *service.CityOpenWorldMobilityState,
	network *service.CityOpenWorldSpatialNetworkState,
	infrastructure *service.CityOpenWorldInfrastructureState,
) []string {
	t.Helper()
	walkEdges := make(map[string]struct{})
	for _, edge := range mobility.Edges {
		if edge.ModeCode == "walk" {
			walkEdges[edge.Code] = struct{}{}
		}
	}
	corridors := make(map[string]string)
	for _, corridor := range network.Corridors {
		if _, found := walkEdges[corridor.EdgeCode]; found {
			corridors[corridor.Code] = corridor.EdgeCode
		}
	}
	assets := make([]string, 0, len(corridors))
	for _, asset := range infrastructure.Assets {
		if asset.AssetKind != "corridor_segment" || asset.SpatialCorridorCode == nil || asset.SegmentOrdinal != 1 {
			continue
		}
		if _, found := corridors[*asset.SpatialCorridorCode]; found {
			assets = append(assets, asset.Code)
		}
	}
	sort.Strings(assets)
	require.Len(t, assets, len(walkEdges), "every walk edge must map to exactly one V20 segment asset")
	return assets
}

func requireCityOpenWorldV21DemandForActor(
	t *testing.T,
	state *service.CityOpenWorldMobilityState,
	actorCode string,
) service.CityOpenWorldMobilityDemand {
	t.Helper()
	for _, demand := range state.Demands {
		if demand.ActorCode == actorCode {
			return demand
		}
	}
	t.Fatalf("mobility demand for actor %q was not found", actorCode)
	return service.CityOpenWorldMobilityDemand{}
}

func cityOpenWorldV21RoutesForDemand(
	state *service.CityOpenWorldMobilityState,
	demandCode string,
) []service.CityOpenWorldMobilityRoute {
	items := make([]service.CityOpenWorldMobilityRoute, 0, 1)
	for _, route := range state.Routes {
		if route.DemandCode == demandCode {
			items = append(items, route)
		}
	}
	return items
}

func requireCityOpenWorldV21RouteForDemand(
	t *testing.T,
	state *service.CityOpenWorldMobilityState,
	demandCode string,
) service.CityOpenWorldMobilityRoute {
	t.Helper()
	routes := cityOpenWorldV21RoutesForDemand(state, demandCode)
	require.Len(t, routes, 1)
	return routes[0]
}

func cityOpenWorldV21AllocationsForRoute(
	state *service.CityOpenWorldMobilityState,
	routeCode string,
) []service.CityOpenWorldMobilityAllocation {
	items := make([]service.CityOpenWorldMobilityAllocation, 0)
	for _, allocation := range state.Allocations {
		if allocation.RouteCode == routeCode {
			items = append(items, allocation)
		}
	}
	return items
}

func cityOpenWorldV21AdmissionsForRoute(
	state *service.CityOpenWorldEffectiveCapacityState,
	routeCode string,
) []service.CityOpenWorldEffectiveCapacityAdmission {
	items := make([]service.CityOpenWorldEffectiveCapacityAdmission, 0)
	for _, admission := range state.Admissions {
		if admission.RouteCode == routeCode {
			items = append(items, admission)
		}
	}
	return items
}
