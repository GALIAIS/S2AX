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

func TestCitySnapshotsReplayAndProjectionRecoveryCloseTheF5Loop(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-recovery-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-recovery-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(551177)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Recoverable City", Timezone: "Asia/Shanghai", Seed: &seed,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionV1, foundation.World.SimulationVersion)
	require.NotNil(t, foundation.World.StateHash)
	worldID := foundation.World.ID

	genesis, err := cityService.GetSnapshot(ctx, owner.ID, worldID, 0)
	require.NoError(t, err)
	require.Equal(t, service.CitySnapshotReasonGenesis, genesis.Reason)
	require.True(t, genesis.IntegrityVerified)
	require.Equal(t, *foundation.World.StateHash, genesis.StateHash)
	require.Positive(t, genesis.UncompressedSize)
	require.Positive(t, genesis.CompressedSize)

	expectedZero := int64(0)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-resume-command",
		CommandType: service.CityCommandTypeWorldResume, Payload: json.RawMessage(`{}`),
		ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	stepOne, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-step-1", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stepOne.Tick.Tick)

	expectedOne := int64(1)
	stepTwo, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-step-2", ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), stepTwo.Tick.Tick)
	require.Len(t, stepTwo.MarketSettlements, 4)

	page, err := cityService.ListSnapshots(ctx, service.CitySnapshotListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.NextCursor)
	require.Equal(t, []int64{0, 1}, []int64{page.Items[0].Tick, page.Items[1].Tick})
	nextPage, err := cityService.ListSnapshots(ctx, service.CitySnapshotListInput{
		UserID: owner.ID, WorldID: worldID, AfterTick: *page.NextCursor, Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, nextPage.Items, 1)
	require.Equal(t, int64(2), nextPage.Items[0].Tick)

	from, target := int64(0), int64(2)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-replay-0-2",
		FromTick: &from, TargetTick: &target,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status)
	require.Equal(t, int64(2), replay.VerifiedTickCount)
	require.NotNil(t, replay.ExpectedStateHash)
	require.Equal(t, replay.ExpectedStateHash, replay.ActualStateHash)
	require.Equal(t, stepTwo.Tick.StateHash, *replay.ActualStateHash)

	replayed, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-replay-0-2",
		FromTick: &from, TargetTick: &target,
	})
	require.NoError(t, err)
	require.Equal(t, replay.ID, replayed.ID)
	conflictingTarget := int64(1)
	_, err = cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-replay-0-2",
		FromTick: &from, TargetTick: &conflictingTarget,
	})
	require.ErrorIs(t, err, service.ErrCityReplayConflict)
	loadedReplay, err := cityService.GetReplay(ctx, owner.ID, worldID, replay.ID)
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, loadedReplay.Status)
	replayPage, err := cityService.ListReplays(ctx, service.CityAuditRunListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, replayPage.Items, 1)
	require.Equal(t, replay.ID, replayPage.Items[0].ID)
	_, err = cityService.GetReplay(ctx, outsider.ID, worldID, replay.ID)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_worlds SET name = 'Projection Drift' WHERE id = $1`, worldID)
	require.NoError(t, err)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-recovery-2", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status)
	require.NotNil(t, recovery.BeforeStateHash)
	require.NotNil(t, recovery.AfterStateHash)
	require.NotEqual(t, recovery.TargetStateHash, *recovery.BeforeStateHash)
	require.Equal(t, recovery.TargetStateHash, *recovery.AfterStateHash)
	require.Positive(t, recovery.RestoredProjectionCount)
	recoveredWorld, err := cityService.GetWorld(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "Recoverable City", recoveredWorld.World.Name)
	require.Equal(t, recovery.TargetStateHash, *recoveredWorld.World.StateHash)

	repeatedRecovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-recovery-2", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, recovery.ID, repeatedRecovery.ID)
	loadedRecovery, err := cityService.GetRecovery(ctx, owner.ID, worldID, recovery.ID)
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, loadedRecovery.Status)
	recoveryPage, err := cityService.ListRecoveries(ctx, service.CityAuditRunListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, recoveryPage.Items, 1)
	require.Equal(t, recovery.ID, recoveryPage.Items[0].ID)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_worlds SET name = 'Failed Recovery Must Roll Back' WHERE id = $1`, worldID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_economic_entities SET code = 'firm_corrupt'
WHERE world_id = $1 AND entity_type = 'firm'`, worldID)
	require.NoError(t, err)
	failedRecovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-recovery-failure", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusFailed, failedRecovery.Status)
	require.Zero(t, failedRecovery.RestoredProjectionCount)
	require.NotNil(t, failedRecovery.ErrorCode)
	var nameAfterFailedRecovery string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT name FROM city_worlds WHERE id = $1`, worldID).Scan(&nameAfterFailedRecovery))
	require.Equal(t, "Failed Recovery Must Roll Back", nameAfterFailedRecovery)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_economic_entities SET code = 'municipal_services'
WHERE world_id = $1 AND entity_type = 'firm'`, worldID)
	require.NoError(t, err)
	postFailureRecovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-recovery-after-failure", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	postFailureDetail := "<nil>"
	if postFailureRecovery.ErrorDetail != nil {
		postFailureDetail = *postFailureRecovery.ErrorDetail
	}
	require.Equalf(t, service.CityRecoveryStatusApplied, postFailureRecovery.Status,
		"post-failure recovery detail: %s", postFailureDetail)
	require.Equal(t, postFailureRecovery.TargetStateHash, *postFailureRecovery.AfterStateHash)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_snapshots SET state_hash = repeat('0', 64) WHERE world_id = $1 AND tick = 2`, worldID)
	require.ErrorContains(t, err, "immutable facts")
	_, err = integrationDB.ExecContext(ctx, `
DELETE FROM city_snapshots WHERE world_id = $1 AND tick = 2`, worldID)
	require.ErrorContains(t, err, "immutable facts")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_replay_runs SET status = 'failed', completed_at = NOW() WHERE id = $1`, replay.ID)
	require.ErrorContains(t, err, "running-to-terminal")

	_, err = cityService.GetSnapshot(ctx, outsider.ID, worldID, 2)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)
	_, err = cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: outsider.ID, WorldID: worldID, IdempotencyKey: "outsider-recovery", ReplayRunID: replay.ID,
	})
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	defaultReplay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-replay-current-defaults",
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), defaultReplay.FromTick)
	require.Equal(t, int64(2), defaultReplay.TargetTick)

	expectedTwo := int64(2)
	stepThree, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-step-3", ExpectedWorldTick: &expectedTwo,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), stepThree.Tick.Tick)

	retriedDefaultReplay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f5-replay-current-defaults",
	})
	require.NoError(t, err)
	require.Equal(t, defaultReplay.ID, retriedDefaultReplay.ID)
	require.Equal(t, int64(2), retriedDefaultReplay.TargetTick)
}
