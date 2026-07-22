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

func TestCityOpenWorldV9MobilityLifecycleIsFactBackedAndRecoverable(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v9-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(9590001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V9 Mobility Lifecycle", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV9,
		StyleProfileID:    "jp.metropolitan",
		SpawnPolicy:       "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	initial, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, initial.Modes, 3)
	require.GreaterOrEqual(t, len(initial.Hubs), 3)
	require.NotEmpty(t, initial.Edges)
	require.Empty(t, initial.Demands)
	require.Empty(t, initial.Routes)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_open_world_mobility_modes
SET speed_units_per_tick = speed_units_per_tick + 1
WHERE world_id = $1 AND code = 'walk'`, worldID)
	require.Error(t, err, "the V9 mobility topology must remain sealed after genesis")

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

	submit("v9-create-actor", service.CityCommandTypeOpenWorldActorCreate, marshalPayload(map[string]any{
		"archetype_code": "urban_apprentice", "name": "Mobility Tester",
	}))
	step("v9-create-actor-step", 1)
	actors, err := cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	require.NotNil(t, actors[0].Location)
	actorCode := actors[0].Code
	originalLocation := *actors[0].Location

	// The V9 route graph is aggregate-only. The central interchange is always
	// reachable from a zone hub and therefore exercises the normal two-tick
	// request/schedule boundary without depending on a specific seed layout.
	submit("v9-mobility-request", service.CityCommandTypeOpenWorldActorMobilityRequest, marshalPayload(map[string]any{
		"actor_code": actorCode, "destination_hub_code": "hub.interchange.central",
		"mode_code": "walk", "purpose_code": "commute", "requested_units": 1,
	}))
	step("v9-mobility-request-step", 1)
	pending, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, pending.Demands, 1)
	require.Empty(t, pending.Routes)
	require.Equal(t, "pending", pending.Demands[0].Status)
	require.Equal(t, currentTick, pending.Demands[0].RequestedTick)
	require.Equal(t, currentTick+1, pending.Demands[0].EarliestDepartureTick)
	require.NotNil(t, pending.Demands[0].LastFact)

	// Scheduling happens only in the following tick, before ordinary commands.
	step("v9-mobility-schedule-step", 0)
	scheduled, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, scheduled.Demands, 1)
	require.Len(t, scheduled.Routes, 1)
	require.NotEmpty(t, scheduled.Allocations)
	require.Equal(t, "scheduled", scheduled.Demands[0].Status)
	require.Equal(t, "scheduled", scheduled.Routes[0].Status)
	require.Equal(t, currentTick, *scheduled.Demands[0].ScheduledTick)
	require.Equal(t, currentTick, scheduled.Routes[0].DepartureTick)
	require.Greater(t, scheduled.Routes[0].ArrivalTick, currentTick)
	require.Equal(t, scheduled.Demands[0].Code, scheduled.Routes[0].DemandCode)
	for _, allocation := range scheduled.Allocations {
		require.Equal(t, scheduled.Routes[0].Code, allocation.RouteCode)
		require.Equal(t, scheduled.Routes[0].DepartureTick, allocation.DepartureTick)
		require.LessOrEqual(t, allocation.AllocatedUnits, allocation.CapacityUnitsPerTick)
	}

	for currentTick < scheduled.Routes[0].ArrivalTick {
		step(fmt.Sprintf("v9-mobility-arrival-%d", currentTick+1), 0)
	}
	completed, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, completed.Demands, 1)
	require.Len(t, completed.Routes, 1)
	require.Equal(t, "completed", completed.Demands[0].Status)
	require.Equal(t, "completed", completed.Routes[0].Status)
	require.NotNil(t, completed.Demands[0].CompletedTick)
	require.NotNil(t, completed.Routes[0].CompletedTick)
	require.NotNil(t, completed.Routes[0].CompletionFact)
	require.Len(t, completed.ActorMetrics, 1)
	require.Equal(t, int64(1), completed.ActorMetrics[0].RequestedCount)
	require.Equal(t, int64(1), completed.ActorMetrics[0].ScheduledCount)
	require.Equal(t, int64(1), completed.ActorMetrics[0].CompletedCount)
	require.Equal(t, int64(0), completed.ActorMetrics[0].ExpiredCount)

	actors, err = cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	require.Equal(t, originalLocation, *actors[0].Location, "V9 completion is a mobility fact, not a local navigation teleport")

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v9-mobility-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v9-mobility-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	if recovery.ErrorDetail != nil {
		t.Logf("V9 mobility recovery detail: %s", *recovery.ErrorDetail)
	}
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, completed, restored)
}

func TestCityOpenWorldV8UpgradeToV9PinsMobilityBaselineWithoutRetroactiveTrips(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v8-v9-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(9590002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V8 to V9 Mobility Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV8,
		StyleProfileID:    "jp.metropolitan",
		SpawnPolicy:       "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	currentTick := int64(0)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v8-upgrade-create-actor",
		CommandType:       service.CityCommandTypeOpenWorldActorCreate,
		Payload:           json.RawMessage(`{"archetype_code":"urban_apprentice","name":"V9 Upgrade Tester"}`),
		ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	result, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v8-upgrade-create-actor-step", ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	require.Len(t, result.Commands, 1)
	currentTick = result.Tick.Tick
	actorsBefore, err := cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actorsBefore, 1)

	engineBefore, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV8, engineBefore.Version)
	require.NotNil(t, engineBefore.VersionVector)
	upgradeTick := currentTick
	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v8-to-v9-mobility-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV9,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)

	engineAfter, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV9, engineAfter.Version)
	require.Contains(t, engineAfter.Stages, "open_world_mobility")
	require.NotNil(t, engineAfter.VersionVector)
	require.Equal(t, engineBefore.VersionVector.Generation+1, engineAfter.VersionVector.Generation)
	require.Equal(t, upgradeTick, engineAfter.VersionVector.BaselineTick)
	mobility, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, upgradeTick, mobility.Policy.BaselineTick)
	require.Len(t, mobility.Modes, 3)
	require.NotEmpty(t, mobility.Hubs)
	require.NotEmpty(t, mobility.Edges)
	require.Empty(t, mobility.Demands)
	require.Empty(t, mobility.Routes)

	actorsAfter, err := cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, actorsBefore, actorsAfter, "the V8 runtime state is historical evidence, not V9 bootstrap input")
}
