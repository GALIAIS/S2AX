//go:build integration

package repository

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCityLandBuildingFoundationClosesGenesisQueryReplayAndRecoveryLoop(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-land-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-land-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(710042)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Land Foundation City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V2,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7V2, foundation.World.SimulationVersion)
	worldID := foundation.World.ID

	land, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	require.Equal(t, cityspatial.DefaultLandRuleSetID, land.Profile.RuleSetID)
	require.Equal(t, "4275912ce56d967b3596c5449ef28097623b0d1a9b80ea5f60e1a882f79e60c2", land.Profile.RuleSetHash)
	require.Zero(t, land.Profile.BaselineTick)
	require.Len(t, land.ZoningRules, 3)
	require.NotEmpty(t, land.Parcels)
	require.NotEmpty(t, land.Buildings)
	require.Len(t, land.Buildings, len(land.UnitPools))
	require.NotEmpty(t, land.HousingAllocations)
	require.NotEmpty(t, land.Portals)
	require.Equal(t, int64(len(land.Parcels)), land.Profile.ParcelCount)
	require.Equal(t, int64(len(land.Buildings)), land.Profile.BuildingCount)

	local, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: 0, MaximumX: 0, MinimumY: 0, MaximumY: 0, Z: 0,
	})
	require.NoError(t, err)
	for _, parcel := range local.Parcels {
		require.Zero(t, parcel.Geometry.ChunkX)
		require.Zero(t, parcel.Geometry.ChunkY)
	}
	_, err = cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: outsider.ID, WorldID: worldID,
		MinimumX: 0, MaximumX: 0, MinimumY: 0, MaximumY: 0, Z: 0,
	})
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	expectedZero := int64(0)
	step, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "city-land-step-one",
		ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), step.Tick.Tick)
	fromTick, targetTick := int64(0), int64(1)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "city-land-replay",
		FromTick: &fromTick, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	if replay.ErrorDetail != nil {
		t.Logf("replay detail: %s", *replay.ErrorDetail)
	}
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)

	var snapshotID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_snapshots
WHERE world_id = $1 AND tick = $2 AND simulation_version = $3`,
		worldID, targetTick, service.CitySimulationVersionF7V2).Scan(&snapshotID))
	driftCityLandProjection(t, ctx, worldID, owner.ID, replay.ID, snapshotID, targetTick, step.Tick.StateHash)
	_, err = cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.Error(t, err)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "city-land-recovery",
		ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status)
	restored, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	require.Equal(t, land.Profile.BaselineHash, restored.Profile.BaselineHash)
	require.Equal(t, land.Buildings, restored.Buildings)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_buildings SET quality_milli = quality_milli + 1
WHERE world_id = $1 AND id = (SELECT MIN(id) FROM city_buildings WHERE world_id = $1)`, worldID)
	require.Error(t, err)
}

func TestCityF7V1ToV2UpgradePreservesSpatialGenerationDomain(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-land-upgrade-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(710042)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Land Upgrade City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	expectedZero := int64(0)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "land-upgrade-generate",
		CommandType: service.CityCommandTypeSpatialGenerateChunk,
		Payload:     []byte(`{"chunk_x":0,"chunk_y":0,"z":0}`), ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	_, err = cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "land-upgrade-step",
		ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	beforeOvermap, err := cityService.GetOvermap(ctx, owner.ID, worldID)
	require.NoError(t, err)
	beforeChunk, err := cityService.GetMapChunk(ctx, owner.ID, worldID, 0, 0, 0)
	require.NoError(t, err)

	dryRun, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "land-upgrade-plan",
		TargetVersion: service.CitySimulationVersionF7V2, DryRun: true,
	})
	require.NoError(t, err)
	if dryRun.ErrorDetail != nil {
		t.Logf("upgrade detail: %s", *dryRun.ErrorDetail)
	}
	require.Equal(t, service.CityUpgradeStatusPlanned, dryRun.Status, "%+v", dryRun)
	var landProfileCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM city_land_profiles WHERE world_id = $1`, worldID).Scan(&landProfileCount))
	require.Zero(t, landProfileCount)

	applied, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "land-upgrade-apply",
		TargetVersion: service.CitySimulationVersionF7V2,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, applied.Status)
	afterOvermap, err := cityService.GetOvermap(ctx, owner.ID, worldID)
	require.NoError(t, err)
	afterChunk, err := cityService.GetMapChunk(ctx, owner.ID, worldID, 0, 0, 0)
	require.NoError(t, err)
	require.Equal(t, beforeOvermap.Profile.OvermapRootHash, afterOvermap.Profile.OvermapRootHash)
	require.Equal(t, beforeChunk.GenerationProof, afterChunk.GenerationProof)
	require.Equal(t, beforeChunk.PayloadHash, afterChunk.PayloadHash)
	land, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), land.Profile.BaselineTick)
}

func driftCityLandProjection(
	t *testing.T,
	ctx context.Context,
	worldID, ownerID, replayID, snapshotID, targetTick int64,
	targetHash string,
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
VALUES ($1, $2, $3, repeat('e', 64), $4, $5, $6, $7)
RETURNING id`, worldID, ownerID, "f73-test-land-drift", replayID, snapshotID, targetTick, targetHash).
		Scan(&recoveryRunID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SELECT set_config('sub2api.city_recovery_run_id', $1, TRUE)`,
		strconv.FormatInt(recoveryRunID, 10))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE city_buildings SET quality_milli = quality_milli + 1, updated_at = NOW()
WHERE world_id = $1 AND id = (SELECT MIN(id) FROM city_buildings WHERE world_id = $1)`, worldID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE city_recovery_runs
SET status = 'failed', error_code = 'TEST_DRIFT',
    error_detail = 'land integration drift fixture', completed_at = NOW()
WHERE id = $1 AND status = 'running'`, recoveryRunID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}
