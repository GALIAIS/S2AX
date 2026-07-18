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

func TestCityCalendarDemographyClosesTheF61Loop(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	ownerA := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-demography-a-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	ownerB := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-demography-b-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-demography-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(6200101)
	startAt := time.Date(2000, time.December, 31, 15, 0, 0, 0, time.UTC)

	createWorld := func(ownerID int64) *service.CityWorldFoundation {
		foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
			OwnerUserID: ownerID, Name: "Calendar City", Timezone: "Asia/Shanghai",
			Seed: &seed, StartAt: &startAt,
		})
		require.NoError(t, err)
		require.Equal(t, service.CitySimulationVersionV1, foundation.World.SimulationVersion)
		return foundation
	}
	worldA := createWorld(ownerA.ID)
	worldB := createWorld(ownerB.ID)

	calendarBefore, err := cityService.GetCalendarState(ctx, ownerA.ID, worldA.World.ID)
	require.NoError(t, err)
	require.Equal(t, "2000-12-31", calendarBefore.LocalDate)
	require.Zero(t, calendarBefore.DayIndex)
	require.Zero(t, calendarBefore.MonthIndex)
	require.Zero(t, calendarBefore.QuarterIndex)
	require.Zero(t, calendarBefore.YearIndex)
	populationBefore, err := cityService.GetPopulationState(ctx, ownerA.ID, worldA.World.ID)
	require.NoError(t, err)
	require.NotEmpty(t, populationBefore.Cohorts)
	require.Equal(t, 12, populationBefore.Policy.PeriodsPerYear)
	populationBeforeTotal := cityPopulationStateTotal(populationBefore)

	expectedZero := int64(0)
	stepA, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID,
		IdempotencyKey: "f6-calendar-step", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	stepB, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerB.ID, WorldID: worldB.World.ID,
		IdempotencyKey: "f6-calendar-step", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stepA.Tick.Tick)
	require.Equal(t, service.CitySimulationVersionV1, stepA.Tick.SimulationVersion)
	require.Equal(t, stepA.Tick.StateHash, stepB.Tick.StateHash)
	require.Equal(t, stepA.Tick.PRNGProof, stepB.Tick.PRNGProof)
	require.Equal(t, startAt.Add(time.Hour), stepA.Tick.SimulatedTo)
	require.Equal(t, []string{
		"city.calendar.day_started",
		"city.calendar.month_started",
		"city.calendar.quarter_started",
		"city.calendar.year_started",
		"city.population.natural_change_posted",
		"city.tick.completed",
	}, cityEventTypes(stepA.Events))
	require.Equal(t, len(stepA.Events), stepA.Tick.EventCount)
	require.Len(t, stepA.PopulationMovements, 1)

	movement := stepA.PopulationMovements[0]
	require.Equal(t, service.CityPopulationMovementNaturalChange, movement.MovementType)
	require.Equal(t, "2001-01-01", movement.LocalMonth)
	require.NotNil(t, movement.PostedAt)
	require.Equal(t, len(populationBefore.Cohorts), movement.ExpectedLineCount)
	require.Len(t, movement.Lines, movement.ExpectedLineCount)
	var lineBirths, lineDeaths, lineTransitions int64
	for index, line := range movement.Lines {
		require.Equal(t, index+1, line.LineNo)
		require.Equal(t,
			line.ChildUnitsBefore+line.BirthUnits-line.ChildDeathUnits-line.ChildToWorkingUnits,
			line.ChildUnitsAfter,
		)
		require.Equal(t,
			line.WorkingUnitsBefore+line.ChildToWorkingUnits-line.WorkingDeathUnits-line.WorkingToSeniorUnits,
			line.WorkingUnitsAfter,
		)
		require.Equal(t,
			line.SeniorUnitsBefore+line.WorkingToSeniorUnits-line.SeniorDeathUnits,
			line.SeniorUnitsAfter,
		)
		lineBirths += line.BirthUnits
		lineDeaths += line.ChildDeathUnits + line.WorkingDeathUnits + line.SeniorDeathUnits
		lineTransitions += line.ChildToWorkingUnits + line.WorkingToSeniorUnits
	}
	require.Equal(t, movement.TotalBirthUnits, lineBirths)
	require.Equal(t, movement.TotalDeathUnits, lineDeaths)
	require.Equal(t, movement.TotalTransitionUnits, lineTransitions)

	calendarAfter, err := cityService.GetCalendarState(ctx, ownerA.ID, worldA.World.ID)
	require.NoError(t, err)
	require.Equal(t, "2001-01-01", calendarAfter.LocalDate)
	require.Equal(t, int64(1), calendarAfter.DayIndex)
	require.Equal(t, int64(1), calendarAfter.MonthIndex)
	require.Equal(t, int64(1), calendarAfter.QuarterIndex)
	require.Equal(t, int64(1), calendarAfter.YearIndex)
	require.Equal(t, int64(1), *calendarAfter.LastDailyTick)
	require.Equal(t, int64(1), *calendarAfter.LastMonthlyTick)
	require.Equal(t, int64(1), *calendarAfter.LastQuarterlyTick)
	require.Equal(t, int64(1), *calendarAfter.LastAnnualTick)
	populationAfter, err := cityService.GetPopulationState(ctx, ownerA.ID, worldA.World.ID)
	require.NoError(t, err)
	require.Equal(t, populationBeforeTotal+movement.TotalBirthUnits-movement.TotalDeathUnits,
		cityPopulationStateTotal(populationAfter))

	movementPage, err := cityService.ListPopulationMovements(ctx, service.CityPopulationMovementListInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, movementPage.Items, 1)
	require.Nil(t, movementPage.NextCursor)
	require.Equal(t, movement.ID, movementPage.Items[0].ID)
	afterMovementPage, err := cityService.ListPopulationMovements(ctx, service.CityPopulationMovementListInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID,
		AfterTick: movement.Tick, AfterSequence: movement.Sequence, Limit: 10,
	})
	require.NoError(t, err)
	require.Empty(t, afterMovementPage.Items)
	movementDetail, err := cityService.GetPopulationMovement(
		ctx, ownerA.ID, worldA.World.ID, movement.Tick, movement.Sequence,
	)
	require.NoError(t, err)
	require.Len(t, movementDetail.Lines, movement.ExpectedLineCount)
	_, err = cityService.GetCalendarState(ctx, outsider.ID, worldA.World.ID)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)
	_, err = cityService.ListPopulationMovements(ctx, service.CityPopulationMovementListInput{
		UserID: outsider.ID, WorldID: worldA.World.ID,
	})
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)
	_, err = cityService.GetPopulationMovement(ctx, outsider.ID, worldA.World.ID, 1, 1)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	fromTick, targetTick := int64(0), int64(1)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID, IdempotencyKey: "f6-replay-0-1",
		FromTick: &fromTick, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status)
	require.Equal(t, int64(1), replay.VerifiedTickCount)
	require.Equal(t, stepA.Tick.StateHash, *replay.ActualStateHash)

	var snapshotID int64
	var targetStateHash string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id, state_hash FROM city_snapshots
WHERE world_id = $1 AND tick = 1 AND simulation_version = $2`,
		worldA.World.ID, service.CitySimulationVersionV1).Scan(&snapshotID, &targetStateHash))
	driftCityF61Projection(t, ctx, worldA.World.ID, ownerA.ID, replay.ID, snapshotID, targetStateHash)

	driftedCalendar, err := cityService.GetCalendarState(ctx, ownerA.ID, worldA.World.ID)
	require.NoError(t, err)
	require.Equal(t, calendarAfter.DayIndex, driftedCalendar.DayIndex)
	require.Equal(t, calendarAfter.Version+1, driftedCalendar.Version)
	driftedPopulation, err := cityService.GetPopulationState(ctx, ownerA.ID, worldA.World.ID)
	require.NoError(t, err)
	require.Equal(t, cityPopulationStateTotal(populationAfter)+1, cityPopulationStateTotal(driftedPopulation))
	require.Equal(t, populationAfter.Policy.BirthRatePPM+1, driftedPopulation.Policy.BirthRatePPM)

	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID,
		IdempotencyKey: "f6-recover-calendar-demography", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status)
	require.NotNil(t, recovery.BeforeStateHash)
	require.NotNil(t, recovery.AfterStateHash)
	require.NotEqual(t, recovery.TargetStateHash, *recovery.BeforeStateHash)
	require.Equal(t, targetStateHash, recovery.TargetStateHash)
	require.Equal(t, targetStateHash, *recovery.AfterStateHash)
	require.Positive(t, recovery.RestoredProjectionCount)

	restoredCalendar, err := cityService.GetCalendarState(ctx, ownerA.ID, worldA.World.ID)
	require.NoError(t, err)
	require.Equal(t, cityCalendarProjectionSignature(calendarAfter), cityCalendarProjectionSignature(restoredCalendar))
	restoredPopulation, err := cityService.GetPopulationState(ctx, ownerA.ID, worldA.World.ID)
	require.NoError(t, err)
	require.Equal(t, cityPopulationProjectionSignature(populationAfter), cityPopulationProjectionSignature(restoredPopulation))
	restoredWorld, err := cityService.GetWorld(ctx, ownerA.ID, worldA.World.ID)
	require.NoError(t, err)
	require.Equal(t, targetStateHash, *restoredWorld.World.StateHash)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_calendar_states SET day_index = day_index + 1 WHERE world_id = $1`, worldA.World.ID)
	require.ErrorContains(t, err, "immutable boundary")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_demographic_cohort_states SET child_units = child_units + 1
WHERE id = (SELECT MIN(id) FROM city_demographic_cohort_states WHERE world_id = $1)`, worldA.World.ID)
	require.ErrorContains(t, err, "draft population or household fact")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_demographic_policies SET birth_rate_ppm = birth_rate_ppm + 1
WHERE world_id = $1`, worldA.World.ID)
	require.ErrorContains(t, err, "future versioned policy command")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_household_cohorts SET population_units = population_units + 1
WHERE id = (SELECT MIN(id) FROM city_household_cohorts WHERE world_id = $1)`, worldA.World.ID)
	require.ErrorContains(t, err, "posted projections")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_calendar_boundaries SET period_index = period_index + 1
WHERE world_id = $1 AND tick = 1 AND sequence = 1`, worldA.World.ID)
	require.ErrorContains(t, err, "immutable facts")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_population_movements SET total_birth_units = total_birth_units + 1 WHERE id = $1`, movement.ID)
	require.ErrorContains(t, err, "draft-to-posted")
	_, err = integrationDB.ExecContext(ctx, `
DELETE FROM city_population_movement_lines WHERE movement_id = $1 AND line_no = 1`, movement.ID)
	require.ErrorContains(t, err, "immutable facts")
}

func driftCityF61Projection(
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
VALUES ($1, $2, $3, repeat('a', 64), $4, $5, 1, $6)
RETURNING id`, worldID, ownerID, "f6-test-drift", replayID, snapshotID, targetHash).Scan(&recoveryRunID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SELECT set_config('sub2api.city_recovery_run_id', $1, TRUE)`,
		strconv.FormatInt(recoveryRunID, 10))
	require.NoError(t, err)

	var cohortID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT cohort_id FROM city_demographic_cohort_states
WHERE world_id = $1 ORDER BY id ASC LIMIT 1 FOR UPDATE`, worldID).Scan(&cohortID))
	_, err = tx.ExecContext(ctx, `
UPDATE city_calendar_states
SET version = version + 1,
    metadata = '{"schema_version":1,"drift":true}'::jsonb,
    updated_at = NOW()
WHERE world_id = $1`, worldID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE city_demographic_policies
SET birth_rate_ppm = birth_rate_ppm + 1, version = version + 1, updated_at = NOW()
WHERE world_id = $1`, worldID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE city_demographic_cohort_states
SET child_units = child_units + 1, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND cohort_id = $2`, worldID, cohortID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE city_household_cohorts
SET population_units = population_units + 1, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2`, worldID, cohortID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE city_recovery_runs
SET status = 'failed', error_code = 'TEST_DRIFT', error_detail = 'integration drift fixture', completed_at = NOW()
WHERE id = $1 AND status = 'running'`, recoveryRunID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func cityPopulationStateTotal(state *service.CityPopulationState) int64 {
	var total int64
	for _, cohort := range state.Cohorts {
		total += cohort.ChildUnits + cohort.WorkingUnits + cohort.SeniorUnits
	}
	return total
}

func cityEventTypes(events []*service.CityEvent) []string {
	result := make([]string, 0, len(events))
	for _, event := range events {
		result = append(result, event.EventType)
	}
	return result
}

func cityCalendarProjectionSignature(state *service.CityCalendarState) string {
	return fmt.Sprintf("%s|%d|%d|%d|%d|%s|%s|%s|%s|%d",
		state.LocalDate, state.DayIndex, state.MonthIndex, state.QuarterIndex, state.YearIndex,
		cityOptionalTick(state.LastDailyTick), cityOptionalTick(state.LastMonthlyTick),
		cityOptionalTick(state.LastQuarterlyTick), cityOptionalTick(state.LastAnnualTick), state.Version)
}

func cityOptionalTick(value *int64) string {
	if value == nil {
		return "-"
	}
	return strconv.FormatInt(*value, 10)
}

func cityPopulationProjectionSignature(state *service.CityPopulationState) []string {
	result := []string{fmt.Sprintf("%s|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d",
		state.Policy.ParameterSetCode, state.Policy.ParameterVersion, state.Policy.PeriodsPerYear,
		state.Policy.BirthRatePPM, state.Policy.ChildDeathRatePPM,
		state.Policy.WorkingDeathRatePPM, state.Policy.SeniorDeathRatePPM,
		state.Policy.ChildToWorkingRatePPM, state.Policy.WorkingToSeniorRatePPM,
		state.Policy.Version, len(state.Cohorts))}
	for _, cohort := range state.Cohorts {
		result = append(result, fmt.Sprintf(
			"%s|%s|%s|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d",
			cohort.DistrictCode, cohort.EntityCode, cohort.IncomeBand,
			cohort.ChildUnits, cohort.WorkingUnits, cohort.SeniorUnits,
			cohort.BirthRemainder, cohort.ChildDeathRemainder,
			cohort.WorkingDeathRemainder, cohort.SeniorDeathRemainder,
			cohort.ChildAgingRemainder, cohort.WorkingAgingRemainder, cohort.Version,
		))
	}
	return result
}
