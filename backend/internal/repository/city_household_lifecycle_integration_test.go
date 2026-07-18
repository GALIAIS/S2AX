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

func TestCityHouseholdLifecycleClosesCommandReplayAndRecoveryLoop(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-household-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-household-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(6300301)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Household Lifecycle City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF6V3,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6V3, foundation.World.SimulationVersion)
	worldID := foundation.World.ID

	before, err := cityService.GetPhysicalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.NotEmpty(t, before.HouseholdCohorts)
	for _, cohort := range before.HouseholdCohorts {
		require.Positive(t, cohort.HouseholdUnits)
		require.LessOrEqual(t, cohort.HouseholdUnits, cohort.PopulationUnits)
		require.Equal(t, cohort.HouseholdUnits, cohort.HousingDemandUnits)
		require.Positive(t, cohort.AverageHouseholdSizeMilli)
	}
	beforePopulation, beforeHouseholds, beforeEmployment := cityHouseholdTotals(before)
	beforeAccounts := cityAccountBalanceSignature(t, ctx, worldID)

	expected := int64(0)
	commands := []service.CityCommandSubmitInput{
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "household-split-central-low",
			CommandType: service.CityCommandTypeHouseholdAdjust, ExpectedWorldTick: &expected,
			Payload: []byte(`{"district_code":"central","income_band":"low","movement_type":"split","household_units":2,"reason":"integration split"}`),
		},
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "household-interleaved-relocation",
			CommandType: service.CityCommandTypePopulationRelocate, ExpectedWorldTick: &expected,
			Payload: []byte(`{"source_district_code":"central","target_district_code":"north","income_band":"low","child_units":1,"working_units":1,"senior_units":0}`),
		},
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "household-reclassify-central",
			CommandType: service.CityCommandTypeHouseholdReclassify, ExpectedWorldTick: &expected,
			Payload: []byte(`{"district_code":"central","source_income_band":"low","target_income_band":"middle","child_units":1,"working_units":2,"senior_units":0,"employed_units":1,"household_units":1,"occupied_units":0,"reason":"sustained income"}`),
		},
	}
	for _, input := range commands {
		command, submitErr := cityService.SubmitCommand(ctx, input)
		require.NoError(t, submitErr)
		repeated, repeatedErr := cityService.SubmitCommand(ctx, input)
		require.NoError(t, repeatedErr)
		require.Equal(t, command.ID, repeated.ID)
	}

	step, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "household-step-1", ExpectedWorldTick: &expected,
	})
	require.NoError(t, err)
	require.Len(t, step.HouseholdMovements, 2)
	require.Len(t, step.PopulationMigrations, 1)
	require.Equal(t, service.CityHouseholdMovementSplit, step.HouseholdMovements[0].MovementType)
	require.Equal(t, service.CityHouseholdMovementIncomeReclassification, step.HouseholdMovements[1].MovementType)
	require.Equal(t, []string{
		"city.household.split",
		"city.population.relocated",
		"city.household.income_reclassification",
		"city.tick.completed",
	}, cityEventTypes(step.Events))
	for index, movement := range step.HouseholdMovements {
		require.Equal(t, int64(index+1), movement.Sequence)
		require.Equal(t, service.CityHouseholdMovementOriginCommand, movement.Origin)
		require.NotNil(t, movement.SourceCommandID)
		require.NotNil(t, movement.PostedAt)
		require.Len(t, movement.Lines, movement.ExpectedLineCount)
	}

	after, err := cityService.GetPhysicalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	afterPopulation, afterHouseholds, afterEmployment := cityHouseholdTotals(after)
	require.Equal(t, beforePopulation, afterPopulation)
	require.Equal(t, beforeHouseholds+2, afterHouseholds)
	require.Equal(t, beforeEmployment, afterEmployment)
	require.Equal(t, beforeAccounts, cityAccountBalanceSignature(t, ctx, worldID))

	page, err := cityService.ListHouseholdMovements(ctx, service.CityHouseholdMovementListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.NextCursor)
	secondPage, err := cityService.ListHouseholdMovements(ctx, service.CityHouseholdMovementListInput{
		UserID: owner.ID, WorldID: worldID, AfterTick: page.NextCursor.Tick,
		AfterSequence: page.NextCursor.Sequence, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Items, 1)
	detail, err := cityService.GetHouseholdMovement(ctx, owner.ID, worldID, 1, 2)
	require.NoError(t, err)
	require.Len(t, detail.Lines, 2)
	_, err = cityService.ListHouseholdMovements(ctx, service.CityHouseholdMovementListInput{
		UserID: outsider.ID, WorldID: worldID,
	})
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	fromTick, targetTick := int64(0), int64(1)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "household-replay-0-1",
		FromTick: &fromTick, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status)
	require.Equal(t, step.Tick.StateHash, *replay.ActualStateHash)

	var snapshotID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_snapshots
WHERE world_id = $1 AND tick = 1 AND simulation_version = $2`,
		worldID, service.CitySimulationVersionF6V3).Scan(&snapshotID))
	driftCityHouseholdProjection(t, ctx, worldID, owner.ID, replay.ID, snapshotID, step.Tick.StateHash)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "household-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status)
	restored, err := cityService.GetPhysicalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, cityHouseholdProjectionSignature(after), cityHouseholdProjectionSignature(restored))

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_household_cohorts SET household_units = household_units + 1
WHERE world_id = $1 AND id = (SELECT MIN(id) FROM city_household_cohorts WHERE world_id = $1)`, worldID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_household_movements SET metadata = '{"tampered":true}'::jsonb
WHERE world_id = $1 AND tick = 1 AND sequence = 1`, worldID)
	require.Error(t, err)

	for expected = 1; expected < 48; expected++ {
		_, err = cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID,
			IdempotencyKey: fmt.Sprintf("household-longrun-step-%d", expected+1), ExpectedWorldTick: &expected,
		})
		require.NoError(t, err)
	}
	fromTick, targetTick = 1, 48
	longReplay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "household-replay-1-48",
		FromTick: &fromTick, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, longReplay.Status)
	require.Equal(t, int64(47), longReplay.VerifiedTickCount)
}

func TestCityF63UpgradeIsExplicitAuditedAndAtomic(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-f63-upgrade-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(6300399)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "F6.3 Upgrade City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF6V2,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	before, err := cityService.GetPhysicalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6V2, before.SimulationVersion)
	require.Zero(t, before.HouseholdCohorts[0].HouseholdUnits)
	require.NotEqual(t, before.HouseholdCohorts[0].HouseholdUnits, before.HouseholdCohorts[0].HousingDemandUnits)

	engine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, []string{service.CitySimulationVersionF6V3}, engine.UpgradeTargets)
	expected := int64(0)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "pre-f63-household-command",
		CommandType: service.CityCommandTypeHouseholdAdjust, ExpectedWorldTick: &expected,
		Payload: []byte(`{"district_code":"central","income_band":"low","movement_type":"split","household_units":1}`),
	})
	require.ErrorIs(t, err, service.ErrCityCommandVersion)

	dryRun, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f63-upgrade-plan",
		TargetVersion: service.CitySimulationVersionF6V3, DryRun: true,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusPlanned, dryRun.Status)
	engine, err = cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6V2, engine.Version)

	applied, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f63-upgrade-apply",
		TargetVersion: service.CitySimulationVersionF6V3,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, applied.Status)
	engine, err = cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6V3, engine.Version)
	require.Equal(t, []string{service.CitySimulationVersionF7}, engine.UpgradeTargets)
	after, err := cityService.GetPhysicalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6V3, after.SimulationVersion)
	for _, cohort := range after.HouseholdCohorts {
		require.Positive(t, cohort.HouseholdUnits)
		require.Equal(t, cohort.HouseholdUnits, cohort.HousingDemandUnits)
		require.LessOrEqual(t, cohort.HouseholdUnits, cohort.PopulationUnits)
	}
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "post-f63-household-command",
		CommandType: service.CityCommandTypeHouseholdAdjust, ExpectedWorldTick: &expected,
		Payload: []byte(`{"district_code":"central","income_band":"low","movement_type":"split","household_units":1}`),
	})
	require.NoError(t, err)
}

func TestCityHouseholdMovementSerializesConcurrentIdempotentSteps(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-household-concurrent-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(6300302)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Concurrent Household City", Seed: &seed,
	})
	require.NoError(t, err)
	expected := int64(0)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: foundation.World.ID, IdempotencyKey: "concurrent-household-split",
		CommandType: service.CityCommandTypeHouseholdAdjust, ExpectedWorldTick: &expected,
		Payload: []byte(`{"district_code":"central","income_band":"middle","movement_type":"split","household_units":1}`),
	})
	require.NoError(t, err)

	results := make(chan *service.CityStepResult, 2)
	errors := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, stepErr := service.NewCityEconomyService(integrationDB).StepWorld(ctx, service.CityStepInput{
				UserID: owner.ID, WorldID: foundation.World.ID,
				IdempotencyKey: "concurrent-household-step", ExpectedWorldTick: &expected,
			})
			results <- result
			errors <- stepErr
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	for stepErr := range errors {
		require.NoError(t, stepErr)
	}
	var first *service.CityStepResult
	for result := range results {
		require.NotNil(t, result)
		require.Len(t, result.HouseholdMovements, 1)
		if first == nil {
			first = result
			continue
		}
		require.Equal(t, first.Tick.ID, result.Tick.ID)
		require.Equal(t, first.HouseholdMovements[0].ID, result.HouseholdMovements[0].ID)
	}
	var factCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_household_movements WHERE world_id = $1`, foundation.World.ID).Scan(&factCount))
	require.Equal(t, 1, factCount)
}

func cityHouseholdTotals(state *service.CityPhysicalState) (population, households, employment int64) {
	for _, cohort := range state.HouseholdCohorts {
		population += cohort.PopulationUnits
		households += cohort.HouseholdUnits
		employment += cohort.EmployedUnits
	}
	return population, households, employment
}

func cityHouseholdProjectionSignature(state *service.CityPhysicalState) []string {
	result := make([]string, 0, len(state.HouseholdCohorts))
	for _, cohort := range state.HouseholdCohorts {
		result = append(result, fmt.Sprintf("%s|%s|%d|%d|%d|%d|%d|%d",
			cohort.DistrictCode, cohort.IncomeBand, cohort.PopulationUnits,
			cohort.WorkingAgeUnits, cohort.EmployedUnits, cohort.HouseholdUnits,
			cohort.HousingDemandUnits, cohort.Version))
	}
	return result
}

func cityAccountBalanceSignature(t *testing.T, ctx context.Context, worldID int64) string {
	t.Helper()
	var signature string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COALESCE(STRING_AGG(id::TEXT || ':' || current_balance_units::TEXT, ',' ORDER BY id), '')
FROM city_accounts WHERE world_id = $1`, worldID).Scan(&signature))
	return signature
}

func driftCityHouseholdProjection(
	t *testing.T,
	ctx context.Context,
	worldID, ownerID, replayID, snapshotID int64,
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
VALUES ($1, $2, $3, repeat('b', 64), $4, $5, 1, $6)
RETURNING id`, worldID, ownerID, "f63-test-drift", replayID, snapshotID, targetHash).Scan(&recoveryRunID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SELECT set_config('sub2api.city_recovery_run_id', $1, TRUE)`,
		strconv.FormatInt(recoveryRunID, 10))
	require.NoError(t, err)
	var cohortID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT id FROM city_household_cohorts
WHERE world_id = $1 ORDER BY id ASC LIMIT 1 FOR UPDATE`, worldID).Scan(&cohortID))
	_, err = tx.ExecContext(ctx, `
UPDATE city_household_cohorts
SET household_units = household_units + 1, housing_demand_units = housing_demand_units + 1,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2`, worldID, cohortID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE city_housing_occupancies
SET unmet_units = unmet_units + 1, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND cohort_id = $2`, worldID, cohortID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE city_recovery_runs
SET status = 'failed', error_code = 'TEST_DRIFT',
    error_detail = 'household integration drift fixture', completed_at = NOW()
WHERE id = $1 AND status = 'running'`, recoveryRunID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}
