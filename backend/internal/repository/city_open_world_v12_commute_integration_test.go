//go:build integration

package repository

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV12CommuteBindingsAreCapacityLimitedAndRecoverable(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v12-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(12_120_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V12 Commute Bindings", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV12,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	initial, err := cityService.GetCityOpenWorldCommuteState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(0), initial.Policy.BaselineTick)
	require.Greater(t, initial.Policy.CandidateCount, int64(0))
	require.Greater(t, initial.Policy.ResidenceCount, int64(0))
	require.Equal(t, initial.Policy.CandidateCount, initial.Policy.BindingCount+initial.Policy.UnboundCandidateCount)
	require.Equal(t, initial.Policy.BindingCount, initial.Policy.UsedResidenceUnits)
	require.Greater(t, initial.Policy.BindingCount, int64(0))
	for _, binding := range initial.Bindings {
		require.Equal(t, "npc.residence_employment", binding.BindingKind)
		require.NotEqual(t, binding.HomeFacilityCode, binding.WorkFacilityCode)
		require.Equal(t, int64(24), binding.PeriodTicks)
		require.Equal(t, (binding.OutboundPhase+12)%24, binding.ReturnPhase)
	}

	currentTick := int64(0)
	_, err = cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v12-commute-step", ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	currentTick++
	afterStep, err := cityService.GetCityOpenWorldCommuteState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, initial, afterStep, "V12 bindings are immutable and must not create a hidden commute stream")

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v12-commute-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v12-commute-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldCommuteState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, afterStep, restored)
}

func TestCityOpenWorldV11UpgradeToV12PinsCommuteBaselineWithoutRewritingOD(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v11-v12-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(12_120_002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V11 to V12 Commute Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV11,
		StyleProfileID:    "cn.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	odBefore, err := cityService.GetCityOpenWorldMobilityODState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v11-v12-commute-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV12,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)

	odAfter, err := cityService.GetCityOpenWorldMobilityODState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, odBefore, odAfter)
	commutes, err := cityService.GetCityOpenWorldCommuteState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(0), commutes.Policy.BaselineTick)
	require.Equal(t, commutes.Policy.CandidateCount, commutes.Policy.BindingCount+commutes.Policy.UnboundCandidateCount)
}
