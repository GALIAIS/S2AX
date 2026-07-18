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

func TestCitySpatialOvermapChunkClosesGenerationReplayAndRecoveryLoop(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-spatial-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-spatial-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(710042)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Spatial Foundation City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7, foundation.World.SimulationVersion)
	worldID := foundation.World.ID

	boundRuleSet, err := cityService.GetWorldSpatialRuleSet(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, cityspatial.DefaultRuleSetID, boundRuleSet.Profile.RuleSetID)
	require.Equal(t, boundRuleSet.RuleSet.ContentHash, boundRuleSet.Profile.RuleSetHash)
	require.Equal(t, int64(cityspatial.DefaultChunkSize), boundRuleSet.Profile.ChunkSize)
	require.Equal(t, cityspatial.DefaultGeneratorID, boundRuleSet.Profile.GeneratorID)
	require.Equal(t, cityspatial.DefaultGeneratorVersion, boundRuleSet.Profile.GeneratorVersion)

	overmap, err := cityService.GetOvermap(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, overmap.Tiles, 81)
	require.Equal(t, "fdf565491d2fed3079c6121dc108fc932c80ed7ff4a3d22e5cb3f5d64315730e", overmap.Profile.OvermapRootHash)
	require.Equal(t, int64(-4), overmap.Profile.MinimumChunkX)
	require.Equal(t, int64(4), overmap.Profile.MaximumChunkX)
	require.Equal(t, int64(-4), overmap.Profile.MinimumChunkY)
	require.Equal(t, int64(4), overmap.Profile.MaximumChunkY)
	_, err = cityService.GetOvermap(ctx, outsider.ID, worldID)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	chunks, err := cityService.ListMapChunks(ctx, service.CityMapChunkListInput{
		UserID: owner.ID, WorldID: worldID, MinimumX: -4, MaximumX: 4,
		MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	require.Empty(t, chunks)
	_, err = cityService.ListMapChunks(ctx, service.CityMapChunkListInput{
		UserID: owner.ID, WorldID: worldID, MinimumX: -5, MaximumX: 4,
		MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.ErrorIs(t, err, service.ErrCityInvalidInput)
	_, err = cityService.GetMapChunk(ctx, owner.ID, worldID, 0, 0, 0)
	require.ErrorIs(t, err, service.ErrCitySpatialChunkNotFound)

	expectedZero := int64(0)
	inputs := []service.CityCommandSubmitInput{
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "spatial-generate-center",
			CommandType: service.CityCommandTypeSpatialGenerateChunk,
			Payload:     []byte(`{"chunk_x":0,"chunk_y":0,"z":0}`), ExpectedWorldTick: &expectedZero,
		},
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "spatial-generate-negative",
			CommandType: service.CityCommandTypeSpatialGenerateChunk,
			Payload:     []byte(`{"chunk_x":-1,"chunk_y":0,"z":0}`), ExpectedWorldTick: &expectedZero,
		},
	}
	for _, input := range inputs {
		command, submitErr := cityService.SubmitCommand(ctx, input)
		require.NoError(t, submitErr)
		replayed, replayErr := cityService.SubmitCommand(ctx, input)
		require.NoError(t, replayErr)
		require.Equal(t, command.ID, replayed.ID)
	}
	stepOne, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "spatial-step-one", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Len(t, stepOne.SpatialMutations, 2)
	require.Equal(t, 2, stepOne.Tick.AppliedCommandCount)
	require.Zero(t, stepOne.Tick.RejectedCommandCount)
	for index, mutation := range stepOne.SpatialMutations {
		require.Equal(t, int64(index+1), mutation.Sequence)
		require.Equal(t, service.CitySpatialMutationChunkGenerated, mutation.MutationType)
		require.Equal(t, 1, mutation.ExpectedLineCount)
		require.Len(t, mutation.Lines, 1)
		require.Equal(t, int64(0), mutation.Lines[0].RevisionBefore)
		require.Equal(t, int64(1), mutation.Lines[0].RevisionAfter)
	}

	chunks, err = cityService.ListMapChunks(ctx, service.CityMapChunkListInput{
		UserID: owner.ID, WorldID: worldID, MinimumX: -1, MaximumX: 0,
		MinimumY: 0, MaximumY: 0, Z: 0,
	})
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	center, err := cityService.GetMapChunk(ctx, owner.ID, worldID, 0, 0, 0)
	require.NoError(t, err)
	require.Equal(t, "4d52eff5a7c63bbda584aaa53223aca98da2c903d98f5fe3dd6e3c28ac537c13", center.PayloadHash)
	require.Equal(t, center.PayloadHash, stepOne.SpatialMutations[0].Lines[0].PayloadHashAfter)
	require.NoError(t, cityspatial.ValidateChunkPayload(boundRuleSet.RuleSet, center.Payload))
	coveredCells := 0
	for _, run := range center.Payload.TerrainRuns {
		coveredCells += run.Length
	}
	require.Equal(t, 1024, coveredCells)

	firstPage, err := cityService.ListSpatialMutations(ctx, service.CitySpatialMutationListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, firstPage.Items, 1)
	require.NotNil(t, firstPage.NextCursor)
	secondPage, err := cityService.ListSpatialMutations(ctx, service.CitySpatialMutationListInput{
		UserID: owner.ID, WorldID: worldID, AfterTick: firstPage.NextCursor.Tick,
		AfterSequence: firstPage.NextCursor.Sequence, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Items, 1)
	require.Nil(t, secondPage.NextCursor)

	expectedOne := int64(1)
	rejectedInputs := []service.CityCommandSubmitInput{
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "spatial-duplicate-center",
			CommandType: service.CityCommandTypeSpatialGenerateChunk,
			Payload:     []byte(`{"chunk_x":0,"chunk_y":0,"z":0}`), ExpectedWorldTick: &expectedOne,
		},
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "spatial-outside-overmap",
			CommandType: service.CityCommandTypeSpatialGenerateChunk,
			Payload:     []byte(`{"chunk_x":5,"chunk_y":0,"z":0}`), ExpectedWorldTick: &expectedOne,
		},
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "spatial-non-surface",
			CommandType: service.CityCommandTypeSpatialGenerateChunk,
			Payload:     []byte(`{"chunk_x":1,"chunk_y":1,"z":1}`), ExpectedWorldTick: &expectedOne,
		},
	}
	for _, input := range rejectedInputs {
		_, err = cityService.SubmitCommand(ctx, input)
		require.NoError(t, err)
	}
	stepTwo, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "spatial-step-two", ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	require.Empty(t, stepTwo.SpatialMutations)
	require.Zero(t, stepTwo.Tick.AppliedCommandCount)
	require.Equal(t, 3, stepTwo.Tick.RejectedCommandCount)
	for _, command := range stepTwo.Commands {
		require.Equal(t, service.CityCommandStatusRejected, command.Status)
		require.NotNil(t, command.ErrorCode)
	}

	fromTick, targetTick := int64(0), int64(2)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "spatial-replay-zero-two",
		FromTick: &fromTick, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status)
	require.Equal(t, int64(2), replay.VerifiedTickCount)
	require.Equal(t, stepTwo.Tick.StateHash, *replay.ActualStateHash)

	var targetSnapshotID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_snapshots
WHERE world_id = $1 AND tick = $2 AND simulation_version = $3`,
		worldID, targetTick, service.CitySimulationVersionF7).Scan(&targetSnapshotID))
	driftCitySpatialChunkProjection(
		t, ctx, worldID, owner.ID, replay.ID, targetSnapshotID, targetTick, stepTwo.Tick.StateHash,
	)
	_, err = cityService.GetMapChunk(ctx, owner.ID, worldID, 0, 0, 0)
	require.Error(t, err)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "spatial-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status)
	require.Equal(t, stepTwo.Tick.StateHash, *recovery.AfterStateHash)
	require.Positive(t, recovery.RestoredProjectionCount)
	restored, err := cityService.GetMapChunk(ctx, owner.ID, worldID, 0, 0, 0)
	require.NoError(t, err)
	require.Equal(t, center.PayloadHash, restored.PayloadHash)
	require.Equal(t, center.Payload, restored.Payload)

	assertCitySpatialFactsRejectDirectMutation(t, ctx, worldID)
}

func TestCityF7UpgradeIsExplicitAuditedAndAtomic(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-f7-upgrade-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(710042)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "F7 Upgrade City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF6V3,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	require.Equal(t, service.CitySimulationVersionF6V3, foundation.World.SimulationVersion)

	engine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, []string{service.CitySimulationVersionF7}, engine.UpgradeTargets)
	expectedZero := int64(0)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "pre-f7-spatial-command",
		CommandType: service.CityCommandTypeSpatialGenerateChunk,
		Payload:     []byte(`{"chunk_x":0,"chunk_y":0,"z":0}`), ExpectedWorldTick: &expectedZero,
	})
	require.ErrorIs(t, err, service.ErrCityCommandVersion)

	dryRun, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f7-upgrade-plan",
		TargetVersion: service.CitySimulationVersionF7, DryRun: true,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusPlanned, dryRun.Status)
	engine, err = cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6V3, engine.Version)
	var profileCount, tileCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT (SELECT COUNT(*) FROM city_spatial_profiles WHERE world_id = $1),
       (SELECT COUNT(*) FROM city_overmap_tiles WHERE world_id = $1)`, worldID).
		Scan(&profileCount, &tileCount))
	require.Zero(t, profileCount)
	require.Zero(t, tileCount)

	applied, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f7-upgrade-apply",
		TargetVersion: service.CitySimulationVersionF7,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, applied.Status)
	require.NotNil(t, applied.TargetSnapshotID)
	engine, err = cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7, engine.Version)
	require.Equal(t, []string{service.CitySimulationVersionF7V2}, engine.UpgradeTargets)
	overmap, err := cityService.GetOvermap(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, overmap.Tiles, 81)
	require.Equal(t, "fdf565491d2fed3079c6121dc108fc932c80ed7ff4a3d22e5cb3f5d64315730e", overmap.Profile.OvermapRootHash)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "post-f7-spatial-command",
		CommandType: service.CityCommandTypeSpatialGenerateChunk,
		Payload:     []byte(`{"chunk_x":0,"chunk_y":0,"z":0}`), ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
}

func driftCitySpatialChunkProjection(
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
VALUES ($1, $2, $3, repeat('d', 64), $4, $5, $6, $7)
RETURNING id`, worldID, ownerID, "f7-test-spatial-drift", replayID, snapshotID, targetTick, targetHash).
		Scan(&recoveryRunID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SELECT set_config('sub2api.city_recovery_run_id', $1, TRUE)`,
		strconv.FormatInt(recoveryRunID, 10))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE city_map_chunks
SET payload = jsonb_set(payload, '{format}', '"tampered"'::jsonb), updated_at = NOW()
WHERE world_id = $1 AND chunk_x = 0 AND chunk_y = 0 AND z = 0`, worldID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE city_recovery_runs
SET status = 'failed', error_code = 'TEST_DRIFT',
    error_detail = 'spatial integration drift fixture', completed_at = NOW()
WHERE id = $1 AND status = 'running'`, recoveryRunID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func assertCitySpatialFactsRejectDirectMutation(t *testing.T, ctx context.Context, worldID int64) {
	t.Helper()
	statements := []string{
		`UPDATE city_spatial_profiles SET metadata = '{"tampered":true}'::jsonb WHERE world_id = $1`,
		`UPDATE city_overmap_tiles SET variant = variant + 1 WHERE world_id = $1 AND chunk_x = 0 AND chunk_y = 0 AND z = 0`,
		`UPDATE city_map_chunks SET revision = revision + 1 WHERE world_id = $1 AND chunk_x = 0 AND chunk_y = 0 AND z = 0`,
		`UPDATE city_spatial_mutations SET metadata = '{"tampered":true}'::jsonb WHERE world_id = $1 AND tick = 1 AND sequence = 1`,
		`UPDATE city_spatial_mutation_lines SET payload_hash_after = repeat('0', 64) WHERE world_id = $1 AND mutation_id = (SELECT MIN(id) FROM city_spatial_mutations WHERE world_id = $1)`,
	}
	for _, statement := range statements {
		_, err := integrationDB.ExecContext(ctx, statement, worldID)
		require.Error(t, err, statement)
	}
}
