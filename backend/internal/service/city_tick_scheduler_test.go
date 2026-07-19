package service

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type recordingCityWorldStepper struct {
	inputs []CityStepInput
	errors []error
}

func (s *recordingCityWorldStepper) StepWorld(_ context.Context, input CityStepInput) (*CityStepResult, error) {
	s.inputs = append(s.inputs, input)
	index := len(s.inputs) - 1
	if index < len(s.errors) && s.errors[index] != nil {
		return nil, s.errors[index]
	}
	return &CityStepResult{}, nil
}

type blockingCityWorldStepper struct {
	entered chan struct{}
	exited  chan struct{}
}

func (s *blockingCityWorldStepper) StepWorld(ctx context.Context, _ CityStepInput) (*CityStepResult, error) {
	close(s.entered)
	<-ctx.Done()
	close(s.exited)
	return nil, ctx.Err()
}

type staticCitySimulationChecker bool

func (c staticCitySimulationChecker) IsCitySimulationEnabled(context.Context) bool {
	return bool(c)
}

func TestCityTickSchedulerSkipsWhenCitySimulationIsDisabled(t *testing.T) {
	scheduler := NewCityTickScheduler(nil, nil, time.Second, 1)
	scheduler.SetCitySimulationEnabledChecker(staticCitySimulationChecker(false))

	report, err := scheduler.ProcessDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, CityTickSchedulerReport{}, report)
}

func TestCityRealTickDelayMilliseconds(t *testing.T) {
	paused, err := cityRealTickDelayMilliseconds(CityWorldStatusPaused, 1000)
	require.NoError(t, err)
	require.Zero(t, paused)

	tests := []struct {
		name       string
		speedMilli int64
		expected   int64
	}{
		{name: "one times", speedMilli: 1000, expected: 3_600_000},
		{name: "minimum speed", speedMilli: 1, expected: 3_600_000_000},
		{name: "maximum speed", speedMilli: 1_000_000, expected: 3_600},
		{name: "ceiling division", speedMilli: 3000, expected: 1_200_000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, delayErr := cityRealTickDelayMilliseconds(CityWorldStatusRunning, test.speedMilli)
			require.NoError(t, delayErr)
			require.Equal(t, test.expected, actual)
		})
	}
	_, err = cityRealTickDelayMilliseconds(CityWorldStatusRunning, 0)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
	_, err = cityRealTickDelayMilliseconds("failed", 1000)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func TestCityTickSchedulerProcessesDueWorldsWithDeterministicRequests(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	mock.ExpectQuery(`WITH due AS`).
		WithArgs(4, "test-worker", int64(60_000)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "current_tick"}).
			AddRow(int64(7), int64(11), int64(3)).
			AddRow(int64(8), int64(12), int64(9)))
	mock.ExpectExec(`UPDATE city_world_schedule_states`).
		WithArgs(int64(7), "test-worker").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE city_world_schedule_states`).
		WithArgs(int64(8), "test-worker").
		WillReturnResult(sqlmock.NewResult(0, 1))
	stepper := &recordingCityWorldStepper{errors: []error{nil, ErrCityExpectedTickConflict}}
	scheduler := NewCityTickScheduler(db, stepper, time.Second, 4)
	scheduler.workerID = "test-worker"
	scheduler.lease = time.Minute

	report, err := scheduler.ProcessDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, CityTickSchedulerReport{Candidates: 2, Processed: 1, Conflicts: 1}, report)
	require.Len(t, stepper.inputs, 2)
	require.Equal(t, int64(7), stepper.inputs[0].WorldID)
	require.Equal(t, int64(11), stepper.inputs[0].UserID)
	require.Equal(t, "city-scheduler-v1-7-3", stepper.inputs[0].IdempotencyKey)
	require.NotNil(t, stepper.inputs[0].ExpectedWorldTick)
	require.Equal(t, int64(3), *stepper.inputs[0].ExpectedWorldTick)
	mock.ExpectClose()
	require.NoError(t, db.Close())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCityTickSchedulerDefersFailedWorldAndReleasesItsLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	mock.ExpectQuery(`WITH due AS`).
		WithArgs(1, "test-worker", int64(60_000)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "current_tick"}).
			AddRow(int64(7), int64(11), int64(3)))
	mock.ExpectExec(`UPDATE city_world_schedule_states`).
		WithArgs(int64(7), "test-worker", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	stepper := &recordingCityWorldStepper{errors: []error{ErrCitySimulationInvariant}}
	scheduler := NewCityTickScheduler(db, stepper, time.Second, 1)
	scheduler.workerID = "test-worker"
	scheduler.lease = time.Minute

	report, err := scheduler.ProcessDue(context.Background())
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
	require.Equal(t, CityTickSchedulerReport{Candidates: 1, Failed: 1}, report)
	require.Len(t, stepper.inputs, 1)
	mock.ExpectClose()
	require.NoError(t, db.Close())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCityTickSchedulerStopCancelsActiveSweep(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	mock.ExpectQuery(`WITH due AS`).
		WithArgs(1, "test-worker", int64(60_000)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "current_tick"}).
			AddRow(int64(7), int64(11), int64(3)))
	stepper := &blockingCityWorldStepper{
		entered: make(chan struct{}),
		exited:  make(chan struct{}),
	}
	scheduler := NewCityTickScheduler(db, stepper, time.Hour, 1)
	scheduler.workerID = "test-worker"
	scheduler.lease = time.Minute
	scheduler.timeout = time.Minute
	scheduler.Start()

	select {
	case <-stepper.entered:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not enter the active sweep")
	}
	startedAt := time.Now()
	scheduler.Stop()
	require.Less(t, time.Since(startedAt), time.Second)
	select {
	case <-stepper.exited:
	default:
		t.Fatal("scheduler stop returned before the active sweep exited")
	}

	mock.ExpectClose()
	require.NoError(t, db.Close())
	require.NoError(t, mock.ExpectationsWereMet())
}
