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

// TestCityOpenWorldV8ImpactBridgeLifecycle proves the complete causal path
// against PostgreSQL: a V7 response is sealed first, then V8 schedules it,
// and only the next simulation step mutates the actor metric projection.
func TestCityOpenWorldV8ImpactBridgeLifecycle(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v8-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(9580001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V8 Impact Bridge", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV8,
		StyleProfileID:    "jp.metropolitan",
		SpawnPolicy:       "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	impacts, err := cityService.GetCityOpenWorldImpactState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, impacts.Catalog, 8)
	require.Empty(t, impacts.Effects)
	require.Empty(t, impacts.Metrics)
	require.Equal(t, int64(0), impacts.Policy.BaselineTick)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_open_world_impact_catalog
SET metric_code = 'service.invalid'
WHERE world_id = $1`, worldID)
	require.Error(t, err, "the sealed V8 impact catalog must not be editable")

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

	submit("v8-create-actor", service.CityCommandTypeOpenWorldActorCreate, marshalPayload(map[string]any{
		"archetype_code": "urban_apprentice", "name": "Impact Tester",
	}))
	step("v8-create-actor-step", 1)
	actors, err := cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	require.NotNil(t, actors[0].Location)
	actorCode := actors[0].Code

	services, err := cityService.GetCityOpenWorldServiceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	serviceCode, found := cityOpenWorldV8ReachableServiceCode(*actors[0].Location, services.Providers)
	require.True(t, found, "a V8 player spawn must have at least one reachable service provider")
	submit("v8-service-request", service.CityCommandTypeOpenWorldActorServiceRequest, marshalPayload(map[string]any{
		"actor_code": actorCode, "service_code": serviceCode, "requested_units": 1,
	}))
	step("v8-service-request-step", 1)

	var scheduled *service.CityOpenWorldImpactEffect
	for attempt := 0; attempt < 24 && scheduled == nil; attempt++ {
		step(fmt.Sprintf("v8-service-progress-%d", attempt), 0)
		state, stateErr := cityService.GetCityOpenWorldImpactState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		if len(state.Effects) == 0 {
			continue
		}
		require.Len(t, state.Effects, 1)
		candidate := state.Effects[0]
		scheduled = &candidate
		require.Equal(t, "scheduled", scheduled.Status)
		require.Equal(t, scheduled.ScheduledTick+1, scheduled.EffectiveTick)
		require.Empty(t, state.Metrics, "a scheduled effect may not mutate its target metric")
	}
	require.NotNil(t, scheduled, "reachable service request did not produce a V7 response in time")
	require.Greater(t, scheduled.EffectiveTick, currentTick)

	step("v8-impact-application-step", 0)
	state, err := cityService.GetCityOpenWorldImpactState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, state.Effects, 1)
	require.Equal(t, "applied", state.Effects[0].Status)
	require.NotNil(t, state.Effects[0].AppliedTick)
	require.NotNil(t, state.Effects[0].ApplicationFact)
	require.Len(t, state.Metrics, 1)
	require.Equal(t, actorCode, state.Metrics[0].TargetCode)
	require.Equal(t, state.Effects[0].MetricCode, state.Metrics[0].MetricCode)
	require.NotNil(t, state.Effects[0].AfterUnits)
	require.Equal(t, *state.Effects[0].AfterUnits, state.Metrics[0].ValueUnits)

	// The V8 bridge is part of canonical state, not a disposable dashboard
	// projection. Replaying and recovering this world must restore the sealed
	// response/effect/metric chain byte-for-byte at the public query boundary.
	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v8-impact-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	if replay.ErrorDetail != nil {
		t.Logf("V8 impact replay detail: %s", *replay.ErrorDetail)
	}
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v8-impact-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldImpactState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, state, restored)
}

// TestCityOpenWorldV7UpgradeToV8StartsImpactBridgeAtUpgradeBaseline verifies
// the migration boundary rather than only V8 genesis.  A sealed V7 service
// response is historical evidence and must survive the upgrade unchanged, but
// it may never acquire a retroactive V8 impact.  Only a response resolved
// after the V8 baseline may schedule and subsequently apply an effect.
func TestCityOpenWorldV7UpgradeToV8StartsImpactBridgeAtUpgradeBaseline(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v7-v8-upgrade-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(9580002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V7 to V8 Impact Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV7,
		StyleProfileID:    "jp.metropolitan",
		SpawnPolicy:       "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

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

	submit("v7-create-actor", service.CityCommandTypeOpenWorldActorCreate, marshalPayload(map[string]any{
		"archetype_code": "urban_apprentice", "name": "Upgrade Boundary Tester",
	}))
	step("v7-create-actor-step", 1)
	actors, err := cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	require.NotNil(t, actors[0].Location)
	actorCode := actors[0].Code

	services, err := cityService.GetCityOpenWorldServiceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	serviceCode, found := cityOpenWorldV8ReachableServiceCode(*actors[0].Location, services.Providers)
	require.True(t, found, "a V7 player spawn must have at least one reachable service provider")
	submit("v7-service-request", service.CityCommandTypeOpenWorldActorServiceRequest, marshalPayload(map[string]any{
		"actor_code": actorCode, "service_code": serviceCode, "requested_units": 1,
	}))
	step("v7-service-request-step", 1)

	var historicalResponse *service.CityOpenWorldServiceResponse
	for attempt := 0; attempt < 24 && historicalResponse == nil; attempt++ {
		step(fmt.Sprintf("v7-service-progress-%d", attempt), 0)
		state, stateErr := cityService.GetCityOpenWorldServiceState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		if len(state.Responses) == 0 {
			continue
		}
		require.Len(t, state.Responses, 1)
		candidate := state.Responses[0]
		historicalResponse = &candidate
	}
	require.NotNil(t, historicalResponse, "the V7 request did not resolve before upgrade")
	require.LessOrEqual(t, historicalResponse.ResolvedTick, currentTick)

	engineBefore, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV7, engineBefore.Version)
	require.NotNil(t, engineBefore.VersionVector)
	require.Equal(t, 1, engineBefore.VersionVector.Generation)

	upgradeTick := currentTick
	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v7-to-v8-impact-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV8,
	})
	require.NoError(t, err)
	if upgrade.ErrorDetail != nil {
		t.Logf("V7 to V8 upgrade detail: %s", *upgrade.ErrorDetail)
	}
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)

	engineAfter, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV8, engineAfter.Version)
	require.Contains(t, engineAfter.Stages, "open_world_impacts")
	require.NotNil(t, engineAfter.VersionVector)
	require.Equal(t, engineBefore.VersionVector.Generation+1, engineAfter.VersionVector.Generation)
	require.Equal(t, upgradeTick, engineAfter.VersionVector.BaselineTick)

	upgradedServices, err := cityService.GetCityOpenWorldServiceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, services.Catalog, upgradedServices.Catalog)
	require.Len(t, upgradedServices.Responses, 1)
	require.Equal(t, *historicalResponse, upgradedServices.Responses[0])
	impacts, err := cityService.GetCityOpenWorldImpactState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, impacts.Catalog, 8)
	require.Equal(t, upgradeTick, impacts.Policy.BaselineTick)
	require.Empty(t, impacts.Effects)
	require.Empty(t, impacts.Metrics)

	// A normal V8 tick scans old responses. The baseline predicate must leave
	// the V7 history untouched rather than backfilling an effect.
	step("v8-ignore-historical-service-response", 0)
	impacts, err = cityService.GetCityOpenWorldImpactState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, impacts.Effects)
	require.Empty(t, impacts.Metrics)

	submit("v8-service-request", service.CityCommandTypeOpenWorldActorServiceRequest, marshalPayload(map[string]any{
		"actor_code": actorCode, "service_code": serviceCode, "requested_units": 1,
	}))
	step("v8-service-request-step", 1)

	var scheduled *service.CityOpenWorldImpactEffect
	for attempt := 0; attempt < 24 && scheduled == nil; attempt++ {
		step(fmt.Sprintf("v8-service-progress-%d", attempt), 0)
		state, stateErr := cityService.GetCityOpenWorldImpactState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		if len(state.Effects) == 0 {
			continue
		}
		require.Len(t, state.Effects, 1)
		candidate := state.Effects[0]
		scheduled = &candidate
		require.Equal(t, "scheduled", scheduled.Status)
		require.NotEqual(t, historicalResponse.Code, scheduled.SourceResponseCode)
		require.Greater(t, scheduled.ScheduledTick, upgradeTick)
		require.Equal(t, scheduled.ScheduledTick+1, scheduled.EffectiveTick)
		require.Empty(t, state.Metrics)
	}
	require.NotNil(t, scheduled, "a V8 response did not schedule a delayed impact")

	step("v8-apply-post-upgrade-impact", 0)
	impacts, err = cityService.GetCityOpenWorldImpactState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, impacts.Effects, 1)
	require.Equal(t, "applied", impacts.Effects[0].Status)
	require.NotNil(t, impacts.Effects[0].ApplicationFact)
	require.Len(t, impacts.Metrics, 1)

	// Replay begins from the V8 upgrade baseline snapshot. Pre-upgrade V7
	// snapshots are intentionally a separate engine generation and must not be
	// mixed into a V8 reducer run.
	fromUpgrade, targetTick := upgradeTick, currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v8-upgrade-replay",
		FromTick: &fromUpgrade, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v8-upgrade-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldImpactState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, impacts, restored)
}

func cityOpenWorldV8ReachableServiceCode(
	location service.CityOpenWorldActorLocation,
	providers []service.CityOpenWorldServiceProvider,
) (string, bool) {
	for _, provider := range providers {
		distance := cityOpenWorldV8Abs(provider.AnchorX-location.X) +
			cityOpenWorldV8Abs(provider.AnchorY-location.Y) +
			cityOpenWorldV8Abs(int64(provider.AnchorZ)-int64(location.Z))
		if provider.Status == "active" && distance <= provider.AccessRadiusUnits {
			return provider.ServiceCode, true
		}
	}
	return "", false
}

func cityOpenWorldV8Abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
