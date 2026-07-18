//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// This test is intentionally opt-in because it writes and verifies 10,000 full
// snapshots. Run with CITY_ENGINE_LONG_RUN=1 when changing engine state shape,
// reducers, transaction ordering, or PostgreSQL versions.
func TestCityEngineTenThousandTickGoldenReplay(t *testing.T) {
	if os.Getenv("CITY_ENGINE_LONG_RUN") != "1" {
		t.Skip("set CITY_ENGINE_LONG_RUN=1 to run the 10,000-tick engine gate")
	}
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-engine-longrun-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(19410000)
	world, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Ten Thousand Tick City", Timezone: "America/New_York", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF6,
	})
	require.NoError(t, err)
	zero := int64(0)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: world.World.ID, IdempotencyKey: "longrun-resume",
		CommandType: service.CityCommandTypeWorldResume, Payload: []byte(`{}`), ExpectedWorldTick: &zero,
	})
	require.NoError(t, err)

	const (
		targetTick        int64 = 10_000
		expectedStateHash       = "856dd27de0f1929b9fd4b3413ed0085583dc6a02829d6de2d614ea6f638a83c4"
	)
	for expected := int64(0); expected < targetTick; expected++ {
		if expected == targetTick/2 {
			cityService = service.NewCityEconomyService(integrationDB)
		}
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: world.World.ID,
			IdempotencyKey: fmt.Sprintf("longrun-step-%05d", expected+1), ExpectedWorldTick: &expected,
		})
		require.NoErrorf(t, stepErr, "tick %d failed", expected+1)
		require.Equal(t, expected+1, result.Tick.Tick)
	}

	fromTick := int64(0)
	target := targetTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: world.World.ID, IdempotencyKey: "longrun-replay",
		FromTick: &fromTick, TargetTick: &target,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status)
	require.Equal(t, targetTick, replay.VerifiedTickCount)

	engine, err := cityService.GetEngineInfo(ctx, owner.ID, world.World.ID)
	require.NoError(t, err)
	require.Equal(t, targetTick, engine.CurrentTick)
	require.Equal(t, targetTick+1, engine.SnapshotCount)
	require.Zero(t, engine.FailedTickCount)
	require.NotNil(t, engine.StateHash)
	require.Equal(t, expectedStateHash, *engine.StateHash)
	var invalidAccounts, invalidInventories, invalidCohorts int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT
    (SELECT COUNT(*) FROM city_accounts
     WHERE world_id = $1 AND NOT allow_negative AND current_balance_units < 0),
    (SELECT COUNT(*) FROM city_inventory_balances WHERE world_id = $1 AND quantity_units < 0),
    (SELECT COUNT(*) FROM city_household_cohorts
     WHERE world_id = $1 AND (population_units < 0 OR working_age_units < 0
       OR employed_units < 0 OR employed_units > working_age_units))`,
		world.World.ID).Scan(&invalidAccounts, &invalidInventories, &invalidCohorts))
	require.Zero(t, invalidAccounts)
	require.Zero(t, invalidInventories)
	require.Zero(t, invalidCohorts)
}
