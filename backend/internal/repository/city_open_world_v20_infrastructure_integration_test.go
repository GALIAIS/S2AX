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

func TestCityOpenWorldV20InfrastructureIsFactBackedAndRecoverable(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v20-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v20-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v20-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(20_200_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V20 Infrastructure", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV20,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)

	initial, err := cityService.GetCityOpenWorldInfrastructureState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "sub2api-open-world-infrastructure-assets", initial.Policy.ProfileID)
	require.Equal(t, "v19_node_corridor_asset_seed_v1", initial.Policy.AssetContract)
	require.Equal(t, "append_only_asset_transition_state_v1", initial.Policy.StateContract)
	require.Equal(t, int64(0), initial.Policy.BaselineTick)
	require.NotEmpty(t, initial.Assets)
	require.Equal(t, initial.Policy.AssetCount, int64(len(initial.Assets)))
	require.Equal(t, initial.Policy.AssetCount, int64(len(initial.States)))
	require.Equal(t, initial.Policy.AssetCount, int64(len(initial.Transitions)))
	require.Equal(t, initial.Policy.AssetCount, initial.Policy.NodeAssetCount+initial.Policy.SegmentAssetCount)
	for _, state := range initial.States {
		require.Equal(t, "operational", state.State)
		require.Equal(t, int64(1000), state.CapacityMilli)
		require.Nil(t, state.SourceFact)
	}
	viewerState, err := cityService.GetCityOpenWorldInfrastructureState(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, initial, viewerState)
	_, err = cityService.GetCityOpenWorldInfrastructureState(ctx, outsider.ID, worldID)
	require.Error(t, err)

	mobilityBefore, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)

	asset := initial.Assets[0]
	restrictedPayload, err := json.Marshal(map[string]any{
		"asset_code":     asset.Code,
		"state":          "restricted",
		"capacity_milli": 650,
		"reason_code":    "operator.capacity_reduction",
	})
	require.NoError(t, err)
	currentTick := int64(0)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: viewer.ID, WorldID: worldID, IdempotencyKey: "v20-viewer-transition-denied",
		CommandType: service.CityCommandTypeOpenWorldInfrastructureAssetTransition,
		Payload:     restrictedPayload, ExpectedWorldTick: &currentTick,
	})
	require.ErrorIs(t, err, service.ErrCityPermissionDenied)

	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v20-restrict-asset",
		CommandType: service.CityCommandTypeOpenWorldInfrastructureAssetTransition,
		Payload:     restrictedPayload, ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	step, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v20-restrict-asset-step", ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	require.Len(t, step.Commands, 1)
	currentTick = step.Tick.Tick

	restricted, err := cityService.GetCityOpenWorldInfrastructureState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, initial.Policy.TransitionCount+1, restricted.Policy.TransitionCount)
	require.Equal(t, initial.Policy.Revision+1, restricted.Policy.Revision)
	restrictedState := requireCityOpenWorldV20InfrastructureState(t, restricted, asset.Code)
	require.Equal(t, "restricted", restrictedState.State)
	require.Equal(t, int64(650), restrictedState.CapacityMilli)
	require.Equal(t, currentTick, restrictedState.EffectiveTick)
	require.NotNil(t, restrictedState.SourceFact)
	require.Equal(t, currentTick, restrictedState.SourceFact.Tick)
	require.GreaterOrEqual(t, restrictedState.SourceFact.Sequence, int64(1))
	requireCityOpenWorldV20InfrastructureTransition(t, restricted, asset.Code, "operational", "restricted", 650)

	mobilityAfterRestriction, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, mobilityBefore.Hubs, mobilityAfterRestriction.Hubs)
	require.Equal(t, mobilityBefore.Edges, mobilityAfterRestriction.Edges)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_open_world_infrastructure_asset_states
SET state = 'operational', capacity_milli = 1000
WHERE world_id = $1 AND asset_code = $2`, worldID, asset.Code)
	require.Error(t, err, "V20 current state must only change through an audited command")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_open_world_infrastructure_assets
SET asset_class = 'node.tampered'
WHERE world_id = $1 AND asset_code = $2`, worldID, asset.Code)
	require.Error(t, err, "V20 asset identity must remain immutable after genesis")

	maintenancePayload, err := json.Marshal(map[string]any{
		"asset_code":  asset.Code,
		"state":       "maintenance",
		"reason_code": "operator.maintenance",
	})
	require.NoError(t, err)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v20-maintain-asset",
		CommandType: service.CityCommandTypeOpenWorldInfrastructureAssetTransition,
		Payload:     maintenancePayload, ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	step, err = cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v20-maintain-asset-step", ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	currentTick = step.Tick.Tick

	operationalPayload, err := json.Marshal(map[string]any{
		"asset_code":  asset.Code,
		"state":       "operational",
		"reason_code": "operator.maintenance_complete",
	})
	require.NoError(t, err)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v20-restore-asset",
		CommandType: service.CityCommandTypeOpenWorldInfrastructureAssetTransition,
		Payload:     operationalPayload, ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	step, err = cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v20-restore-asset-step", ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	currentTick = step.Tick.Tick

	final, err := cityService.GetCityOpenWorldInfrastructureState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	finalState := requireCityOpenWorldV20InfrastructureState(t, final, asset.Code)
	require.Equal(t, "operational", finalState.State)
	require.Equal(t, int64(1000), finalState.CapacityMilli)
	require.Equal(t, currentTick, finalState.EffectiveTick)
	require.Equal(t, initial.Policy.TransitionCount+3, final.Policy.TransitionCount)
	require.Equal(t, initial.Policy.Revision+3, final.Policy.Revision)
	requireCityOpenWorldV20InfrastructureTransition(t, final, asset.Code, "restricted", "maintenance", 0)
	requireCityOpenWorldV20InfrastructureTransition(t, final, asset.Code, "maintenance", "operational", 1000)

	fromGenesis := int64(0)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v20-infrastructure-replay",
		FromTick: &fromGenesis, TargetTick: &currentTick,
	})
	require.NoError(t, err)
	if replay.Status != service.CityReplayStatusVerified {
		code, detail := "", ""
		if replay.ErrorCode != nil {
			code = *replay.ErrorCode
		}
		if replay.ErrorDetail != nil {
			detail = *replay.ErrorDetail
		}
		t.Fatalf("V20 infrastructure replay failed: status=%s code=%q detail=%q", replay.Status, code, detail)
	}
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v20-infrastructure-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldInfrastructureState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, final, restored)
}

func TestCityOpenWorldV19UpgradeToV20SeedsInfrastructureWithoutRewritingTransport(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v19-v20-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(20_200_002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V19 to V20 Infrastructure Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV19,
		StyleProfileID:    "cn.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	currentTick := int64(0)
	step, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v19-v20-infrastructure-step", ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	currentTick = step.Tick.Tick
	networkBefore, err := cityService.GetCityOpenWorldSpatialNetworkState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	mobilityBefore, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)

	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v19-v20-infrastructure-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV20,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)

	info, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV20, info.Version)
	require.NotNil(t, info.VersionVector)
	require.Equal(t, currentTick, info.VersionVector.BaselineTick)
	contentCatalog := requireCityOpenWorldVersionBinding(t, info.VersionVector, "content_catalog")
	require.Equal(t, "sub2api-open-world-infrastructure-catalog", contentCatalog.BundleID)
	require.Equal(t, "1.0.0", contentCatalog.BundleVersion)

	infrastructure, err := cityService.GetCityOpenWorldInfrastructureState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, currentTick, infrastructure.Policy.BaselineTick)
	require.Equal(t, int64(len(networkBefore.Nodes)+len(networkBefore.Corridors)), infrastructure.Policy.AssetCount)
	require.Equal(t, int64(len(networkBefore.Nodes)), infrastructure.Policy.NodeAssetCount)
	require.Equal(t, int64(len(networkBefore.Corridors)), infrastructure.Policy.SegmentAssetCount)
	require.Equal(t, infrastructure.Policy.AssetCount, infrastructure.Policy.TransitionCount)

	networkAfter, err := cityService.GetCityOpenWorldSpatialNetworkState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	mobilityAfter, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, networkBefore, networkAfter)
	require.Equal(t, mobilityBefore.Hubs, mobilityAfter.Hubs)
	require.Equal(t, mobilityBefore.Edges, mobilityAfter.Edges)
	require.Equal(t, mobilityBefore.Demands, mobilityAfter.Demands)
	require.Equal(t, mobilityBefore.Routes, mobilityAfter.Routes)
}

func requireCityOpenWorldV20InfrastructureState(
	t *testing.T,
	state *service.CityOpenWorldInfrastructureState,
	assetCode string,
) service.CityOpenWorldInfrastructureAssetState {
	t.Helper()
	for _, current := range state.States {
		if current.AssetCode == assetCode {
			return current
		}
	}
	t.Fatalf("infrastructure state for asset %q was not found", assetCode)
	return service.CityOpenWorldInfrastructureAssetState{}
}

func requireCityOpenWorldV20InfrastructureTransition(
	t *testing.T,
	state *service.CityOpenWorldInfrastructureState,
	assetCode, fromState, toState string,
	capacityMilli int64,
) service.CityOpenWorldInfrastructureAssetTransition {
	t.Helper()
	for _, transition := range state.Transitions {
		if transition.AssetCode == assetCode && transition.FromState == fromState &&
			transition.ToState == toState && transition.CapacityMilli == capacityMilli {
			return transition
		}
	}
	t.Fatalf("infrastructure transition %q -> %q for asset %q was not found", fromState, toState, assetCode)
	return service.CityOpenWorldInfrastructureAssetTransition{}
}

func requireCityOpenWorldVersionBinding(
	t *testing.T,
	vector *service.CityWorldVersionVector,
	componentCode string,
) service.CityWorldVersionBinding {
	t.Helper()
	for _, binding := range vector.Bindings {
		if binding.ComponentCode == componentCode {
			return binding
		}
	}
	t.Fatalf("version-vector binding %q was not found", componentCode)
	return service.CityWorldVersionBinding{}
}
