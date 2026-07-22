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

func TestCityOpenWorldV10ArrivalBridgeLandsOnlyAfterCompletedV9Route(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v10-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(10_100_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V10 Arrival Bridge", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV10,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	initial, err := cityService.GetCityOpenWorldMobilityArrivalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, initial.Arrivals)
	require.Equal(t, int64(0), initial.Policy.BaselineTick)

	currentTick := int64(0)
	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	step := func(key string, expectedCommands int) {
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.Len(t, result.Commands, expectedCommands)
		currentTick = result.Tick.Tick
	}
	submit := func(key, commandType string, payload json.RawMessage) {
		_, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, CommandType: commandType,
			Payload: payload, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, submitErr)
	}

	submit("v10-create-actor", service.CityCommandTypeOpenWorldActorCreate, marshalPayload(map[string]any{
		"archetype_code": "urban_apprentice", "name": "Arrival Tester",
	}))
	step("v10-create-actor-step", 1)
	actors, err := cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	actorCode := actors[0].Code

	submit("v10-mobility-request", service.CityCommandTypeOpenWorldActorMobilityRequest, marshalPayload(map[string]any{
		"actor_code": actorCode, "destination_hub_code": "hub.interchange.central",
		"mode_code": "walk", "purpose_code": "commute", "requested_units": 1,
	}))
	step("v10-mobility-request-step", 1)
	mobility, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, mobility.Demands, 1)
	var demandMetadata map[string]any
	require.NoError(t, json.Unmarshal(mobility.Demands[0].Metadata, &demandMetadata))
	require.Contains(t, demandMetadata, "arrival_bridge")

	step("v10-mobility-schedule-step", 0)
	mobility, err = cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, mobility.Routes, 1)
	for currentTick < mobility.Routes[0].ArrivalTick {
		step(fmt.Sprintf("v10-route-%d", currentTick+1), 0)
	}
	completed, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "completed", completed.Routes[0].Status)
	arrivals, err := cityService.GetCityOpenWorldMobilityArrivalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, arrivals.Arrivals, "completion tick may not bridge into local space")

	step("v10-arrival-bridge-step", 0)
	arrivals, err = cityService.GetCityOpenWorldMobilityArrivalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, arrivals.Arrivals, 1)
	arrival := arrivals.Arrivals[0]
	require.Equal(t, "landed", arrival.Status)
	require.Equal(t, completed.Routes[0].Code, arrival.RouteCode)
	require.NotNil(t, arrival.LandingLocation)
	require.NotNil(t, arrival.LandingFact)
	actors, err = cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	require.NotNil(t, actors[0].Location)
	require.Equal(t, *arrival.LandingLocation, *actors[0].Location)
	require.Equal(t, arrival.LandingFact, actors[0].Location.SourceFact)

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v10-arrival-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v10-arrival-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldMobilityArrivalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, arrivals, restored)
}

func TestCityOpenWorldV9UpgradeToV10DoesNotRetrofitPreUpgradeDemand(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v9-v10-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(10_100_002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V9 to V10 Arrival Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV9,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	currentTick := int64(0)
	submit := func(key, commandType string, payload json.RawMessage) {
		_, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, CommandType: commandType,
			Payload: payload, ExpectedWorldTick: &currentTick,
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
	submit("v9-v10-create", service.CityCommandTypeOpenWorldActorCreate, json.RawMessage(`{"archetype_code":"urban_apprentice","name":"Upgrade Arrival Tester"}`))
	step("v9-v10-create-step", 1)
	actors, err := cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	submit("v9-v10-request", service.CityCommandTypeOpenWorldActorMobilityRequest, json.RawMessage(fmt.Sprintf(`{"actor_code":%q,"destination_hub_code":"hub.interchange.central","mode_code":"walk","purpose_code":"commute","requested_units":1}`, actors[0].Code)))
	step("v9-v10-request-step", 1)
	step("v9-v10-schedule-step", 0)
	mobility, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, mobility.Routes, 1)
	preUpgradeDemandTick := mobility.Demands[0].RequestedTick
	upgradeTick := currentTick

	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v9-v10-upgrade", TargetVersion: service.CitySimulationVersionOpenWorldV10,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)
	arrivals, err := cityService.GetCityOpenWorldMobilityArrivalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, upgradeTick, arrivals.Policy.BaselineTick)
	require.LessOrEqual(t, preUpgradeDemandTick, arrivals.Policy.BaselineTick)

	for currentTick < mobility.Routes[0].ArrivalTick+2 {
		step(fmt.Sprintf("v9-v10-post-upgrade-%d", currentTick+1), 0)
	}
	arrivals, err = cityService.GetCityOpenWorldMobilityArrivalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, arrivals.Arrivals, "V9 demand rows lack the V10 captured-origin contract and must not be retrofitted")
}

func TestCityOpenWorldV10ArrivalBridgeFailsWhenOriginHasChanged(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v10-origin-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(10_100_003)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V10 Arrival Origin Guard", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV10,
		StyleProfileID:    "cn.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	currentTick := int64(0)
	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	step := func(key string, expectedCommands int) *service.CityStepResult {
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.Len(t, result.Commands, expectedCommands)
		currentTick = result.Tick.Tick
		return result
	}
	submit := func(key, commandType string, payload json.RawMessage) {
		_, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, CommandType: commandType,
			Payload: payload, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, submitErr)
	}

	submit("v10-origin-create", service.CityCommandTypeOpenWorldActorCreate, marshalPayload(map[string]any{
		"archetype_code": "urban_apprentice", "name": "Origin Guard Tester",
	}))
	step("v10-origin-create-step", 1)
	actors, err := cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	require.NotNil(t, actors[0].Location)
	actorCode := actors[0].Code
	origin := *actors[0].Location

	submit("v10-origin-request", service.CityCommandTypeOpenWorldActorMobilityRequest, marshalPayload(map[string]any{
		"actor_code": actorCode, "destination_hub_code": "hub.interchange.central",
		"mode_code": "walk", "purpose_code": "commute", "requested_units": 1,
	}))
	step("v10-origin-request-step", 1)
	step("v10-origin-schedule-step", 0)
	mobility, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, mobility.Routes, 1)

	// Change the actor's local position through the existing V5 intent reducer.
	// V10 must retain the captured request origin and later fail rather than
	// overwrite this newer local action when the aggregate route completes.
	target := findCityOpenWorldV5AdjacentPassableTarget(t, ctx, cityService, owner.ID, worldID, actorCode, origin)
	submit("v10-origin-navigation", service.CityCommandTypeOpenWorldActorNavigationSet,
		marshalPayload(cityOpenWorldV5NavigationPayload(actorCode, target)))
	step("v10-origin-navigation-step", 1)
	actors, err = cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	require.NotNil(t, actors[0].Location)
	require.Equal(t, target.X, actors[0].Location.X)
	require.Equal(t, target.Y, actors[0].Location.Y)

	for currentTick < mobility.Routes[0].ArrivalTick {
		step(fmt.Sprintf("v10-origin-route-%d", currentTick+1), 0)
	}
	completed, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "completed", completed.Routes[0].Status)
	bridgeStep := step("v10-origin-bridge-step", 0)
	factTypes := make([]string, 0, len(bridgeStep.OpenWorldRuntimeFacts))
	for _, fact := range bridgeStep.OpenWorldRuntimeFacts {
		factTypes = append(factTypes, fact.FactType)
	}
	require.Contains(t, factTypes, service.CityOpenWorldRuntimeFactMobilityArrivalPending,
		"the bridge must still register a pending audit fact")
	arrivals, err := cityService.GetCityOpenWorldMobilityArrivalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, arrivals.Arrivals, 1)
	arrival := arrivals.Arrivals[0]
	require.Equal(t, "failed", arrival.Status)
	require.Equal(t, origin, arrival.ExpectedOrigin)
	require.Nil(t, arrival.LandingLocation)
	require.Nil(t, arrival.LandingFact)
	require.NotNil(t, arrival.FailedTick)
	require.Equal(t, currentTick, *arrival.FailedTick)

	actors, err = cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	require.NotNil(t, actors[0].Location)
	require.Equal(t, target.X, actors[0].Location.X)
	require.Equal(t, target.Y, actors[0].Location.Y)
}
