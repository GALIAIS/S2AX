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

func TestCityOpenWorldV13CommuteSourcesAreVerifiedFactBackedAndRecoverable(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v13-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(13_130_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V13 Commute Sources", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV13,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	initial, err := cityService.GetCityOpenWorldCommuteSourceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(0), initial.Policy.BaselineTick)
	require.Greater(t, initial.Policy.SourceCount, int64(0))
	require.Equal(t, int64(len(initial.Sources)), initial.Policy.SourceCount)
	require.Equal(t, int64(0), initial.Policy.SourceCount%2)
	require.Empty(t, initial.Metrics)
	assertCityOpenWorldV13CommuteSourcePairs(t, initial)

	currentTick := int64(0)
	generatedFactCount := 0
	for currentTick < 25 {
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID,
			IdempotencyKey:    fmt.Sprintf("v13-commute-step-%d", currentTick+1),
			ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.NotNil(t, result.Tick)
		for _, fact := range result.OpenWorldRuntimeFacts {
			if fact.FactType == "system.commute.source.generated" {
				generatedFactCount++
			}
		}
		currentTick = result.Tick.Tick
	}
	require.Greater(t, generatedFactCount, 0, "V5 NPCs start at their work egress domain, so at least one return commute must be eligible")

	closed, err := cityService.GetCityOpenWorldCommuteSourceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Greater(t, closed.Policy.GeneratedCount, int64(0))
	require.Len(t, closed.Metrics, 1)
	require.Equal(t, int64(1), closed.Metrics[0].CycleStartTick)
	require.Equal(t, int64(24), closed.Metrics[0].CycleEndTick)
	require.Equal(t, int64(25), closed.Metrics[0].ClosedTick)

	generated := requireCityOpenWorldV13GeneratedCommuteSource(t, closed)
	mobility, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	demand := requireCityOpenWorldV13CommuteDemand(t, mobility, generated.Code)
	require.Equal(t, generated.ActorCode, demand.ActorCode)
	require.Equal(t, generated.PurposeCode, demand.PurposeCode)

	// V11 remains auditable but must not add generic traffic for this bound
	// actor once V13 has supplied the complete directional source pair.
	od, err := cityService.GetCityOpenWorldMobilityODState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	var matchingOD *service.CityOpenWorldMobilityODSource
	for index := range od.Sources {
		if od.Sources[index].ActorCode == generated.ActorCode {
			matchingOD = &od.Sources[index]
			break
		}
	}
	require.NotNil(t, matchingOD)
	require.Greater(t, matchingOD.SuppressedCount, int64(0))

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v13-commute-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	if replay.ErrorDetail != nil {
		t.Logf("V13 commute source replay detail: %s", *replay.ErrorDetail)
	}
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v13-commute-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldCommuteSourceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, closed, restored)
}

func TestCityOpenWorldV12UpgradeToV13PinsSourcesWithoutRewritingBindings(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v12-v13-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(13_130_002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V12 to V13 Commute Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV12,
		StyleProfileID:    "cn.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	commutesBefore, err := cityService.GetCityOpenWorldCommuteState(ctx, owner.ID, worldID)
	require.NoError(t, err)

	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v12-v13-commute-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV13,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)
	commutesAfter, err := cityService.GetCityOpenWorldCommuteState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, commutesBefore, commutesAfter)
	sources, err := cityService.GetCityOpenWorldCommuteSourceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(len(commutesBefore.Bindings)*2), sources.Policy.SourceCount)
	require.Zero(t, sources.Policy.GeneratedCount)
	require.Zero(t, sources.Policy.SuppressedCount)
	assertCityOpenWorldV13CommuteSourcePairs(t, sources)
}

func assertCityOpenWorldV13CommuteSourcePairs(t *testing.T, state *service.CityOpenWorldCommuteSourceState) {
	t.Helper()
	pairs := make(map[string]map[string]bool)
	for _, source := range state.Sources {
		if pairs[source.BindingCode] == nil {
			pairs[source.BindingCode] = map[string]bool{}
		}
		pairs[source.BindingCode][source.Direction] = true
	}
	for bindingCode, directions := range pairs {
		require.Truef(t, directions["outbound"], "%s lacks outbound source", bindingCode)
		require.Truef(t, directions["return"], "%s lacks return source", bindingCode)
	}
}

func requireCityOpenWorldV13GeneratedCommuteSource(t *testing.T, state *service.CityOpenWorldCommuteSourceState) service.CityOpenWorldCommuteSource {
	t.Helper()
	for _, source := range state.Sources {
		if source.GeneratedCount > 0 {
			return source
		}
	}
	t.Fatal("no generated V13 commute source")
	return service.CityOpenWorldCommuteSource{}
}

func requireCityOpenWorldV13CommuteDemand(t *testing.T, state *service.CityOpenWorldMobilityState, sourceCode string) service.CityOpenWorldMobilityDemand {
	t.Helper()
	for _, demand := range state.Demands {
		metadata := struct {
			SourceCode string `json:"commute_source_code"`
		}{}
		require.NoError(t, json.Unmarshal(demand.Metadata, &metadata))
		if metadata.SourceCode == sourceCode {
			return demand
		}
	}
	t.Fatalf("V13 commute demand for source %q not found", sourceCode)
	return service.CityOpenWorldMobilityDemand{}
}
