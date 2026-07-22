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

func TestCityOpenWorldV19SpatialNetworkIsStaticAndRecoverable(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v19-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v19-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v19-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(19_190_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V19 Spatial Network", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV19,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)

	network, err := cityService.GetCityOpenWorldSpatialNetworkState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "sub2api-transport-jp-metropolitan", network.Policy.TransportStyleID)
	require.Equal(t, int64(0), network.Policy.BaselineTick)
	require.NotEmpty(t, network.Nodes)
	require.NotEmpty(t, network.Corridors)
	require.Equal(t, network.Policy.NodeCount, int64(len(network.Nodes)))
	require.Equal(t, network.Policy.CorridorCount, int64(len(network.Corridors)))
	for _, node := range network.Nodes {
		require.NotEmpty(t, node.Code)
		require.NotEmpty(t, node.HubCode)
		require.NotEmpty(t, node.NodeClass)
	}
	for _, corridor := range network.Corridors {
		require.NotEmpty(t, corridor.Code)
		require.NotEmpty(t, corridor.EdgeCode)
		require.NotEmpty(t, corridor.CorridorClass)
		require.NotEqual(t, corridor.FromNodeCode, corridor.ToNodeCode)
	}
	viewerNetwork, err := cityService.GetCityOpenWorldSpatialNetworkState(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, network, viewerNetwork)
	_, err = cityService.GetCityOpenWorldSpatialNetworkState(ctx, outsider.ID, worldID)
	require.Error(t, err)

	mobility, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, network.Nodes, len(mobility.Hubs))
	require.Len(t, network.Corridors, len(mobility.Edges))
	require.Empty(t, mobility.Demands)
	require.Empty(t, mobility.Routes)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_open_world_spatial_network_nodes
SET node_class = 'tampered_node'
WHERE world_id = $1 AND code = $2`, worldID, network.Nodes[0].Code)
	require.Error(t, err, "the V19 topology must remain immutable after genesis")

	currentTick := int64(0)
	step, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v19-spatial-baseline-step", ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	require.Equal(t, currentTick+1, step.Tick.Tick)
	currentTick = step.Tick.Tick

	fromGenesis := int64(0)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v19-spatial-replay",
		FromTick: &fromGenesis, TargetTick: &currentTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v19-spatial-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldSpatialNetworkState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, network, restored)
}

func TestCityOpenWorldV18UpgradeToV19MapsFrozenTopologyWithoutBackfill(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v18-v19-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(19_190_002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V18 to V19 Spatial Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV18,
		StyleProfileID:    "cn.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	currentTick := int64(0)
	step, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v18-v19-spatial-step", ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	currentTick = step.Tick.Tick
	mobilityBefore, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	batchesBefore, err := cityService.GetCityOpenWorldFreightBatchState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, batchesBefore.Plans)
	require.Empty(t, batchesBefore.Consignments)

	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v18-v19-spatial-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV19,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)

	info, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV19, info.Version)
	require.NotNil(t, info.VersionVector)
	require.Equal(t, currentTick, info.VersionVector.BaselineTick)
	var contentCatalog *service.CityWorldVersionBinding
	for index := range info.VersionVector.Bindings {
		binding := &info.VersionVector.Bindings[index]
		if binding.ComponentCode == "content_catalog" {
			contentCatalog = binding
			break
		}
	}
	require.NotNil(t, contentCatalog)
	require.Equal(t, "sub2api-open-world-spatial-network-catalog", contentCatalog.BundleID)
	require.Equal(t, "1.0.0", contentCatalog.BundleVersion)
	network, err := cityService.GetCityOpenWorldSpatialNetworkState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "sub2api-transport-cn-metropolitan", network.Policy.TransportStyleID)
	require.Equal(t, currentTick, network.Policy.BaselineTick)
	require.Len(t, network.Nodes, len(mobilityBefore.Hubs))
	require.Len(t, network.Corridors, len(mobilityBefore.Edges))

	mobilityAfter, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, mobilityBefore.Hubs, mobilityAfter.Hubs)
	require.Equal(t, mobilityBefore.Edges, mobilityAfter.Edges)
	require.Equal(t, mobilityBefore.Demands, mobilityAfter.Demands, "V19 must not create or rewrite V9 demand history")
	require.Equal(t, mobilityBefore.Routes, mobilityAfter.Routes, "V19 must not create or rewrite V9 route history")
	batchesAfter, err := cityService.GetCityOpenWorldFreightBatchState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, batchesBefore.Plans, batchesAfter.Plans)
	require.Equal(t, batchesBefore.Consignments, batchesAfter.Consignments)
}
