//go:build integration

package repository

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCityEngineHardeningSupportsVersionCoexistenceAndAtomicUpgrade(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	legacyOwner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-engine-legacy-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	legacyPeerOwner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-engine-peer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	currentOwner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-engine-current-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	startAt := time.Date(2000, time.December, 31, 14, 0, 0, 0, time.UTC)
	seed := int64(194001)

	create := func(ownerID int64, version string) *service.CityWorldFoundation {
		world, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
			OwnerUserID: ownerID, Name: "Versioned City", Timezone: "Asia/Shanghai",
			Seed: &seed, StartAt: &startAt, SimulationVersion: version,
		})
		require.NoError(t, err)
		return world
	}
	legacy := create(legacyOwner.ID, service.CitySimulationVersionF5)
	legacyPeer := create(legacyPeerOwner.ID, service.CitySimulationVersionF5)
	current := create(currentOwner.ID, service.CitySimulationVersionF6)

	legacyInfo, err := cityService.GetEngineInfo(ctx, legacyOwner.ID, legacy.World.ID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF5, legacyInfo.Version)
	require.Equal(t, []string{service.CitySimulationVersionF6}, legacyInfo.UpgradeTargets)
	require.NotContains(t, legacyInfo.Stages, "calendar_demography")
	require.Equal(t, int64(1), legacyInfo.SnapshotCount)
	require.Positive(t, legacyInfo.SnapshotBytes)
	require.Nil(t, legacyInfo.LastTickDurationMS)
	currentInfo, err := cityService.GetEngineInfo(ctx, currentOwner.ID, current.World.ID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6, currentInfo.Version)
	require.Contains(t, currentInfo.Stages, "calendar_demography")
	peerTick := int64(0)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: legacyPeerOwner.ID, WorldID: legacyPeer.World.ID,
		IdempotencyKey: "peer-pending-before-upgrade", CommandType: service.CityCommandTypeWorldRename,
		Payload: []byte(`{"name":"Peer With Pending Command"}`), ExpectedWorldTick: &peerTick,
	})
	require.NoError(t, err)
	_, err = cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: legacyPeerOwner.ID, WorldID: legacyPeer.World.ID,
		IdempotencyKey: "peer-upgrade-with-pending-command", TargetVersion: service.CitySimulationVersionF6,
	})
	require.ErrorIs(t, err, service.ErrCityUpgradeState)

	var wait sync.WaitGroup
	wait.Add(2)
	errorsByWorld := make(chan error, 2)
	step := func(ownerID, worldID int64, key string) {
		defer wait.Done()
		expected := int64(0)
		_, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: ownerID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &expected,
		})
		errorsByWorld <- stepErr
	}
	go step(legacyOwner.ID, legacy.World.ID, "legacy-concurrent-step")
	go step(currentOwner.ID, current.World.ID, "current-concurrent-step")
	wait.Wait()
	close(errorsByWorld)
	for stepErr := range errorsByWorld {
		require.NoError(t, stepErr)
	}
	staleTick := int64(0)
	_, err = cityService.StepWorld(ctx, service.CityStepInput{
		UserID: currentOwner.ID, WorldID: current.World.ID,
		IdempotencyKey: "stale-client-step", ExpectedWorldTick: &staleTick,
	})
	require.ErrorIs(t, err, service.ErrCityExpectedTickConflict)
	currentInfo, err = cityService.GetEngineInfo(ctx, currentOwner.ID, current.World.ID)
	require.NoError(t, err)
	require.Zero(t, currentInfo.FailedTickCount)

	for expected := int64(1); expected < 3; expected++ {
		_, err = cityService.StepWorld(ctx, service.CityStepInput{
			UserID: legacyOwner.ID, WorldID: legacy.World.ID,
			IdempotencyKey: fmt.Sprintf("legacy-step-%d", expected+1), ExpectedWorldTick: &expected,
		})
		require.NoError(t, err)
	}
	targetLegacyTick := int64(3)
	fromGenesis := int64(0)
	legacyReplay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: legacyOwner.ID, WorldID: legacy.World.ID, IdempotencyKey: "legacy-replay",
		FromTick: &fromGenesis, TargetTick: &targetLegacyTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, legacyReplay.Status)
	require.Equal(t, int64(3), legacyReplay.VerifiedTickCount)

	calendarBefore, err := cityService.GetCalendarState(ctx, legacyOwner.ID, legacy.World.ID)
	require.NoError(t, err)
	require.Equal(t, "2000-12-31", calendarBefore.LocalDate)

	dryRun, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: legacyOwner.ID, WorldID: legacy.World.ID, IdempotencyKey: "upgrade-plan",
		TargetVersion: service.CitySimulationVersionF6, DryRun: true,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusPlanned, dryRun.Status)
	require.NotNil(t, dryRun.AfterStateHash)
	require.Nil(t, dryRun.TargetSnapshotID)
	legacyInfo, err = cityService.GetEngineInfo(ctx, legacyOwner.ID, legacy.World.ID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF5, legacyInfo.Version)
	calendarAfterPlan, err := cityService.GetCalendarState(ctx, legacyOwner.ID, legacy.World.ID)
	require.NoError(t, err)
	require.Equal(t, calendarBefore.LocalDate, calendarAfterPlan.LocalDate)

	applied, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: legacyOwner.ID, WorldID: legacy.World.ID, IdempotencyKey: "upgrade-apply",
		TargetVersion: service.CitySimulationVersionF6, DryRun: false,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, applied.Status)
	require.NotNil(t, applied.TargetSnapshotID)
	require.NotNil(t, applied.AfterStateHash)
	require.NotEqual(t, applied.BeforeStateHash, *applied.AfterStateHash)
	legacyInfo, err = cityService.GetEngineInfo(ctx, legacyOwner.ID, legacy.World.ID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6, legacyInfo.Version)
	require.Equal(t, targetLegacyTick, legacyInfo.CurrentTick)
	calendarAfterUpgrade, err := cityService.GetCalendarState(ctx, legacyOwner.ID, legacy.World.ID)
	require.NoError(t, err)
	require.Equal(t, "2001-01-01", calendarAfterUpgrade.LocalDate)
	require.Zero(t, calendarAfterUpgrade.DayIndex)

	var sourceSnapshots, targetSnapshots int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE simulation_version = $2),
       COUNT(*) FILTER (WHERE simulation_version = $3)
FROM city_snapshots WHERE world_id = $1 AND tick = $4`,
		legacy.World.ID, service.CitySimulationVersionF5, service.CitySimulationVersionF6, targetLegacyTick).
		Scan(&sourceSnapshots, &targetSnapshots))
	require.Equal(t, 1, sourceSnapshots)
	require.Equal(t, 1, targetSnapshots)
	legacySnapshot, err := cityService.GetSnapshotVersion(
		ctx, legacyOwner.ID, legacy.World.ID, targetLegacyTick, service.CitySimulationVersionF5,
	)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF5, legacySnapshot.SimulationVersion)
	legacySnapshots, err := cityService.ListSnapshots(ctx, service.CitySnapshotListInput{
		UserID: legacyOwner.ID, WorldID: legacy.World.ID,
		SimulationVersion: service.CitySimulationVersionF5, Limit: 20,
	})
	require.NoError(t, err)
	require.NotEmpty(t, legacySnapshots.Items)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_worlds SET simulation_version = $2 WHERE id = $1`, legacy.World.ID, service.CitySimulationVersionF5)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `
INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ($1, $2, 'forbidden_reverse_path')`, service.CitySimulationVersionF6, service.CitySimulationVersionF5)
	require.Error(t, err)

	peerExpected := int64(0)
	_, err = cityService.StepWorld(ctx, service.CityStepInput{
		UserID: legacyPeerOwner.ID, WorldID: legacyPeer.World.ID,
		IdempotencyKey: "legacy-peer-after-upgrade", ExpectedWorldTick: &peerExpected,
	})
	require.NoError(t, err)
	peerInfo, err := cityService.GetEngineInfo(ctx, legacyPeerOwner.ID, legacyPeer.World.ID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF5, peerInfo.Version)

	postUpgradeExpected := targetLegacyTick
	postUpgradeStep, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: legacyOwner.ID, WorldID: legacy.World.ID,
		IdempotencyKey: "post-upgrade-step", ExpectedWorldTick: &postUpgradeExpected,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6, postUpgradeStep.Tick.SimulationVersion)
	fromUpgrade := targetLegacyTick
	targetF6Tick := targetLegacyTick + 1
	f6Replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: legacyOwner.ID, WorldID: legacy.World.ID, IdempotencyKey: "post-upgrade-replay",
		FromTick: &fromUpgrade, TargetTick: &targetF6Tick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, f6Replay.Status)

	assertCityStepRollsBackWhenSnapshotWriteFails(t, cityService, legacyOwner.ID, legacy.World.ID, targetF6Tick)
	peerExpected = 1
	_, err = cityService.StepWorld(ctx, service.CityStepInput{
		UserID: legacyPeerOwner.ID, WorldID: legacyPeer.World.ID,
		IdempotencyKey: "legacy-peer-after-other-world-failure", ExpectedWorldTick: &peerExpected,
	})
	require.NoError(t, err)
	assertCityUpgradeRollsBackWhenTargetSnapshotFails(
		t, cityService, legacyPeerOwner.ID, legacyPeer.World.ID, peerExpected+1,
	)
}

func assertCityStepRollsBackWhenSnapshotWriteFails(
	t *testing.T,
	cityService *service.CityEconomyService,
	ownerID, worldID, currentTick int64,
) {
	t.Helper()
	ctx := context.Background()
	functionName := fmt.Sprintf("fail_city_snapshot_%d", worldID)
	triggerName := fmt.Sprintf("fail_city_snapshot_trigger_%d", worldID)
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.world_id = %d AND NEW.tick = %d AND NEW.reason = 'tick' THEN
        RAISE EXCEPTION 'injected snapshot failure';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER %s BEFORE INSERT ON city_snapshots
FOR EACH ROW EXECUTE FUNCTION %s();`, functionName, worldID, currentTick+1, triggerName, functionName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(
			"DROP TRIGGER IF EXISTS %s ON city_snapshots; DROP FUNCTION IF EXISTS %s();",
			triggerName, functionName,
		))
	})

	var beforeHash string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT state_hash FROM city_worlds WHERE id = $1`, worldID).Scan(&beforeHash))
	expected := currentTick
	_, err = cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerID, WorldID: worldID,
		IdempotencyKey: "snapshot-failure-step", ExpectedWorldTick: &expected,
	})
	require.Error(t, err)

	var afterTick int64
	var afterHash string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT current_tick, state_hash FROM city_worlds WHERE id = $1`, worldID).Scan(&afterTick, &afterHash))
	require.Equal(t, currentTick, afterTick)
	require.Equal(t, beforeHash, afterHash)
	var tickFacts, eventFacts, snapshots int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT
    (SELECT COUNT(*) FROM city_ticks WHERE world_id = $1 AND tick = $2),
    (SELECT COUNT(*) FROM city_events WHERE world_id = $1 AND tick = $2),
    (SELECT COUNT(*) FROM city_snapshots WHERE world_id = $1 AND tick = $2)`,
		worldID, currentTick+1).Scan(&tickFacts, &eventFacts, &snapshots))
	require.Zero(t, tickFacts)
	require.Zero(t, eventFacts)
	require.Zero(t, snapshots)
	engineInfo, infoErr := cityService.GetEngineInfo(ctx, ownerID, worldID)
	require.NoError(t, infoErr)
	require.Equal(t, int64(1), engineInfo.FailedTickCount)
	require.NotNil(t, engineInfo.LastFailureCode)
	require.Equal(t, "CITY_TICK_FAILED", *engineInfo.LastFailureCode)
	require.NotNil(t, engineInfo.LastFailureAt)
	var failureVersion string
	var failureWorldTick int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT simulation_version, world_tick FROM city_tick_failures
WHERE world_id = $1 ORDER BY id DESC LIMIT 1`, worldID).Scan(&failureVersion, &failureWorldTick))
	require.Equal(t, service.CitySimulationVersionF6, failureVersion)
	require.Equal(t, currentTick, failureWorldTick)

	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(
		"DROP TRIGGER IF EXISTS %s ON city_snapshots; DROP FUNCTION IF EXISTS %s();",
		triggerName, functionName,
	))
	require.NoError(t, err)
	retryExpected := currentTick
	retry, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerID, WorldID: worldID,
		IdempotencyKey: "snapshot-failure-step", ExpectedWorldTick: &retryExpected,
	})
	require.NoError(t, err)
	require.Equal(t, currentTick+1, retry.Tick.Tick)
}

func assertCityUpgradeRollsBackWhenTargetSnapshotFails(
	t *testing.T,
	cityService *service.CityEconomyService,
	ownerID, worldID, currentTick int64,
) {
	t.Helper()
	ctx := context.Background()
	functionName := fmt.Sprintf("fail_city_upgrade_snapshot_%d", worldID)
	triggerName := fmt.Sprintf("fail_city_upgrade_snapshot_trigger_%d", worldID)
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.world_id = %d AND NEW.tick = %d
       AND NEW.simulation_version = 'city-f6-v1' AND NEW.reason = 'baseline' THEN
        RAISE EXCEPTION 'injected upgrade snapshot failure';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER %s BEFORE INSERT ON city_snapshots
FOR EACH ROW EXECUTE FUNCTION %s();`, functionName, worldID, currentTick, triggerName, functionName))
	require.NoError(t, err)
	dropFailure := func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(
			"DROP TRIGGER IF EXISTS %s ON city_snapshots; DROP FUNCTION IF EXISTS %s();",
			triggerName, functionName,
		))
	}
	t.Cleanup(dropFailure)

	failed, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: ownerID, WorldID: worldID, IdempotencyKey: "injected-upgrade-failure",
		TargetVersion: service.CitySimulationVersionF6,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusFailed, failed.Status)
	require.NotNil(t, failed.ErrorCode)
	require.Nil(t, failed.TargetSnapshotID)
	engine, err := cityService.GetEngineInfo(ctx, ownerID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF5, engine.Version)
	require.Equal(t, currentTick, engine.CurrentTick)
	var targetSnapshots int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_snapshots
WHERE world_id = $1 AND tick = $2 AND simulation_version = $3`,
		worldID, currentTick, service.CitySimulationVersionF6).Scan(&targetSnapshots))
	require.Zero(t, targetSnapshots)

	dropFailure()
	retried, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: ownerID, WorldID: worldID, IdempotencyKey: "upgrade-after-injected-failure",
		TargetVersion: service.CitySimulationVersionF6,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, retried.Status)
}
