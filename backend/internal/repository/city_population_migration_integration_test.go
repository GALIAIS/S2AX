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

func TestCityPopulationMigrationSerializesConcurrentIdempotentSteps(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-migration-concurrent-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(6200202)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Concurrent Migration City", Seed: &seed,
	})
	require.NoError(t, err)
	expected := int64(0)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: foundation.World.ID, IdempotencyKey: "concurrent-immigration",
		CommandType: service.CityCommandTypePopulationImmigrate, ExpectedWorldTick: &expected,
		Payload: []byte(`{"target_district_code":"north","income_band":"middle","child_units":1,"working_units":2,"senior_units":1}`),
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
				IdempotencyKey: "concurrent-migration-step", ExpectedWorldTick: &expected,
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
		require.Len(t, result.PopulationMigrations, 1)
		if first == nil {
			first = result
			continue
		}
		require.Equal(t, first.Tick.ID, result.Tick.ID)
		require.Equal(t, first.Tick.StateHash, result.Tick.StateHash)
		require.Equal(t, first.PopulationMigrations[0].ID, result.PopulationMigrations[0].ID)
	}
	var factCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_population_migrations WHERE world_id = $1`, foundation.World.ID).Scan(&factCount))
	require.Equal(t, 1, factCount)
}

func TestCityPopulationMigrationClosesTheF62Loop(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-migration-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-migration-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(6200201)
	startAt := time.Date(2000, time.December, 31, 15, 0, 0, 0, time.UTC)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Migration City", Timezone: "Asia/Shanghai",
		Seed: &seed, StartAt: &startAt, SimulationVersion: service.CitySimulationVersionF6V3,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6V3, foundation.World.SimulationVersion)
	worldID := foundation.World.ID

	before, err := cityService.GetPopulationState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	beforeTotal := cityPopulationStateTotal(before)
	expected := int64(0)
	commands := []service.CityCommandSubmitInput{
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "immigrate-north-low",
			CommandType: service.CityCommandTypePopulationImmigrate, ExpectedWorldTick: &expected,
			Payload: []byte(`{"target_district_code":"north","income_band":"low","child_units":6,"working_units":8,"senior_units":4,"reason":"regional inflow"}`),
		},
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "relocate-central-north-low",
			CommandType: service.CityCommandTypePopulationRelocate, ExpectedWorldTick: &expected,
			Payload: []byte(`{"source_district_code":"central","target_district_code":"north","income_band":"low","child_units":3,"working_units":5,"senior_units":2}`),
		},
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "emigrate-north-low",
			CommandType: service.CityCommandTypePopulationEmigrate, ExpectedWorldTick: &expected,
			Payload: []byte(`{"source_district_code":"north","income_band":"low","child_units":2,"working_units":3,"senior_units":1}`),
		},
	}
	var firstCommandID int64
	for index, input := range commands {
		command, submitErr := cityService.SubmitCommand(ctx, input)
		require.NoError(t, submitErr)
		if index == 0 {
			firstCommandID = command.ID
			repeated, repeatedErr := cityService.SubmitCommand(ctx, input)
			require.NoError(t, repeatedErr)
			require.Equal(t, command.ID, repeated.ID)
		}
	}

	step, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "migration-step-1", ExpectedWorldTick: &expected,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6V3, step.Tick.SimulationVersion)
	require.Len(t, step.PopulationMigrations, 3)
	require.Len(t, step.PopulationMovements, 1)
	require.Equal(t, firstCommandID, step.PopulationMigrations[0].SourceCommandID)
	require.Equal(t, []string{
		"city.population.immigrated",
		"city.population.relocated",
		"city.population.emigrated",
		"city.calendar.day_started",
		"city.calendar.month_started",
		"city.calendar.quarter_started",
		"city.calendar.year_started",
		"city.population.natural_change_posted",
		"city.tick.completed",
	}, cityEventTypes(step.Events))
	for index, migration := range step.PopulationMigrations {
		require.Equal(t, int64(index+1), migration.Sequence)
		require.NotNil(t, migration.PostedAt)
		require.Len(t, migration.Lines, migration.ExpectedLineCount)
	}
	relocation := step.PopulationMigrations[1]
	require.Equal(t, service.CityPopulationMigrationDistrictRelocation, relocation.MigrationType)
	require.Equal(t, "outflow", relocation.Lines[0].Direction)
	require.Equal(t, "inflow", relocation.Lines[1].Direction)
	for _, age := range []func(*service.CityPopulationMigrationLine) int64{
		func(line *service.CityPopulationMigrationLine) int64 { return line.ChildUnits },
		func(line *service.CityPopulationMigrationLine) int64 { return line.WorkingUnits },
		func(line *service.CityPopulationMigrationLine) int64 { return line.SeniorUnits },
	} {
		require.Equal(t, age(relocation.Lines[0]), age(relocation.Lines[1]))
	}

	after, err := cityService.GetPopulationState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	natural := step.PopulationMovements[0]
	require.Equal(t, beforeTotal+12+natural.TotalBirthUnits-natural.TotalDeathUnits, cityPopulationStateTotal(after))

	firstPage, err := cityService.ListPopulationMigrations(ctx, service.CityPopulationMigrationListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, firstPage.Items, 2)
	require.NotNil(t, firstPage.NextCursor)
	secondPage, err := cityService.ListPopulationMigrations(ctx, service.CityPopulationMigrationListInput{
		UserID: owner.ID, WorldID: worldID, AfterTick: firstPage.NextCursor.Tick,
		AfterSequence: firstPage.NextCursor.Sequence, Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Items, 1)
	detail, err := cityService.GetPopulationMigration(ctx, owner.ID, worldID, 1, 2)
	require.NoError(t, err)
	require.Len(t, detail.Lines, 2)
	_, err = cityService.ListPopulationMigrations(ctx, service.CityPopulationMigrationListInput{
		UserID: outsider.ID, WorldID: worldID,
	})
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	fromTick, targetTick := int64(0), int64(1)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "migration-replay-0-1",
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
	driftCityF61Projection(t, ctx, worldID, owner.ID, replay.ID, snapshotID, step.Tick.StateHash)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "migration-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status)
	restored, err := cityService.GetPopulationState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, cityPopulationProjectionSignature(after), cityPopulationProjectionSignature(restored))

	expected = 1
	physical, err := cityService.GetPhysicalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	var unavailableWorkingUnits int64
	for _, cohort := range physical.HouseholdCohorts {
		if cohort.DistrictCode == "central" && cohort.IncomeBand == "low" {
			unavailableWorkingUnits = cohort.WorkingAgeUnits - cohort.EmployedUnits + 1
		}
	}
	require.Positive(t, unavailableWorkingUnits)
	rejectedCommand, err := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "emigration-below-employment",
		CommandType: service.CityCommandTypePopulationEmigrate, ExpectedWorldTick: &expected,
		Payload: []byte(fmt.Sprintf(`{"source_district_code":"central","income_band":"low","child_units":0,"working_units":%d,"senior_units":0}`, unavailableWorkingUnits)),
	})
	require.NoError(t, err)
	rejectedStep, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "migration-step-2", ExpectedWorldTick: &expected,
	})
	require.NoError(t, err)
	require.Empty(t, rejectedStep.PopulationMigrations)
	var rejected *service.CityCommand
	for _, command := range rejectedStep.Commands {
		if command.ID == rejectedCommand.ID {
			rejected = command
		}
	}
	require.NotNil(t, rejected)
	require.Equal(t, service.CityCommandStatusRejected, rejected.Status)
	require.NotNil(t, rejected.ErrorCode)
	require.Equal(t, "CITY_POPULATION_MIGRATION_EMPLOYMENT_FLOOR", *rejected.ErrorCode)

	for expected = 2; expected < 48; expected++ {
		if expected == 12 {
			_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
				UserID: owner.ID, WorldID: worldID, IdempotencyKey: "longrun-immigration",
				CommandType: service.CityCommandTypePopulationImmigrate, ExpectedWorldTick: &expected,
				Payload: []byte(`{"target_district_code":"east","income_band":"middle","child_units":2,"working_units":3,"senior_units":1}`),
			})
			require.NoError(t, err)
		}
		if expected == 24 {
			_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
				UserID: owner.ID, WorldID: worldID, IdempotencyKey: "longrun-relocation",
				CommandType: service.CityCommandTypePopulationRelocate, ExpectedWorldTick: &expected,
				Payload: []byte(`{"source_district_code":"south","target_district_code":"west","income_band":"high","child_units":1,"working_units":1,"senior_units":1}`),
			})
			require.NoError(t, err)
		}
		_, err = cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID,
			IdempotencyKey: fmt.Sprintf("migration-longrun-step-%d", expected+1), ExpectedWorldTick: &expected,
		})
		require.NoError(t, err)
	}
	fromTick, targetTick = 1, 48
	longReplay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "migration-replay-1-48",
		FromTick: &fromTick, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, longReplay.Status)
	require.Equal(t, int64(47), longReplay.VerifiedTickCount)
	engine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.NotNil(t, engine.StateHash)
	require.Equal(t, "aa4e01150d90a8d12a971154b2b9669f3b26dfd28017aa33bea8ef84eb7f0bd1", *engine.StateHash)
}

func TestCityF62UpgradeIsAuditedAndAtomic(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-f62-upgrade-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(6200299)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "F6.1 Upgrade City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF6,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	engine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, []string{service.CitySimulationVersionF6V2}, engine.UpgradeTargets)
	expected := int64(0)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "pre-upgrade-immigration",
		CommandType: service.CityCommandTypePopulationImmigrate, ExpectedWorldTick: &expected,
		Payload: []byte(`{"target_district_code":"harbor","income_band":"middle","child_units":1,"working_units":2,"senior_units":1}`),
	})
	require.ErrorIs(t, err, service.ErrCityCommandVersion)

	dryRun, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f62-upgrade-plan",
		TargetVersion: service.CitySimulationVersionF6V2, DryRun: true,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusPlanned, dryRun.Status)
	engine, err = cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6, engine.Version)

	functionName := fmt.Sprintf("fail_f62_upgrade_snapshot_%d", worldID)
	triggerName := fmt.Sprintf("fail_f62_upgrade_snapshot_trigger_%d", worldID)
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`
CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.world_id = %d AND NEW.tick = 0
       AND NEW.simulation_version = 'city-f6-v2' AND NEW.reason = 'baseline' THEN
        RAISE EXCEPTION 'injected F6.2 upgrade failure';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER %s BEFORE INSERT ON city_snapshots
FOR EACH ROW EXECUTE FUNCTION %s();`, functionName, worldID, triggerName, functionName))
	require.NoError(t, err)
	dropFailure := func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(
			"DROP TRIGGER IF EXISTS %s ON city_snapshots; DROP FUNCTION IF EXISTS %s();",
			triggerName, functionName,
		))
	}
	t.Cleanup(dropFailure)
	failed, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f62-upgrade-injected-failure",
		TargetVersion: service.CitySimulationVersionF6V2,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusFailed, failed.Status)
	engine, err = cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6, engine.Version)
	dropFailure()

	applied, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "f62-upgrade-apply",
		TargetVersion: service.CitySimulationVersionF6V2,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, applied.Status)
	engine, err = cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF6V2, engine.Version)
	require.Equal(t, []string{service.CitySimulationVersionF6V3}, engine.UpgradeTargets)

	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "post-upgrade-immigration",
		CommandType: service.CityCommandTypePopulationImmigrate, ExpectedWorldTick: &expected,
		Payload: []byte(`{"target_district_code":"harbor","income_band":"middle","child_units":1,"working_units":2,"senior_units":1}`),
	})
	require.NoError(t, err)
	step, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "post-f62-upgrade-step", ExpectedWorldTick: &expected,
	})
	require.NoError(t, err)
	require.Len(t, step.PopulationMigrations, 1)
	fromTick, targetTick := int64(0), int64(1)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "post-f62-upgrade-replay",
		FromTick: &fromTick, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status)
}
