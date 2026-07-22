package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type staticCityRealtimeSchedulerChecker struct {
	cityEnabled     bool
	realtimeEnabled bool
}

func (c staticCityRealtimeSchedulerChecker) IsCitySimulationEnabled(context.Context) bool {
	return c.cityEnabled
}

func (c staticCityRealtimeSchedulerChecker) IsCityRealtimeSchedulerEnabled(context.Context) bool {
	return c.realtimeEnabled
}

type staticCityRealtimeClockAuthority struct {
	observation CityRealtimeClockObservation
	err         error
	profiles    []CityRealtimeClockProfile
}

type profileLimitedCityRealtimeClockAuthority struct {
	staticCityRealtimeClockAuthority
	supports bool
}

func (a *profileLimitedCityRealtimeClockAuthority) Supports(CityRealtimeClockProfile) bool {
	return a.supports
}

func (a *staticCityRealtimeClockAuthority) Observe(
	_ context.Context,
	profile CityRealtimeClockProfile,
) (CityRealtimeClockObservation, error) {
	a.profiles = append(a.profiles, profile)
	return a.observation, a.err
}

type recordingCityRealtimeDueEventExecutor struct {
	worldIDs          []int64
	observations      []CityRealtimeClockObservation
	clockUnsafeWorlds []int64
	recoveryWorlds    []int64
	result            *CityRealtimeDiagnosticDueEventProcessResult
	err               error
	clockUnsafeErr    error
	recoveryErr       error
	recoveryStarted   bool
}

func (e *recordingCityRealtimeDueEventExecutor) processRealtimeDueEventsWithClock(
	_ context.Context,
	worldID int64,
	observation CityRealtimeClockObservation,
	_ bool,
	_ string,
) (*CityRealtimeDiagnosticDueEventProcessResult, error) {
	e.worldIDs = append(e.worldIDs, worldID)
	e.observations = append(e.observations, observation)
	if e.err != nil {
		return nil, e.err
	}
	if e.result == nil {
		return &CityRealtimeDiagnosticDueEventProcessResult{}, nil
	}
	return e.result, nil
}

func (e *recordingCityRealtimeDueEventExecutor) recordRealtimeClockUnsafe(
	_ context.Context,
	worldID int64,
	_ string,
) (bool, error) {
	e.clockUnsafeWorlds = append(e.clockUnsafeWorlds, worldID)
	if e.clockUnsafeErr != nil {
		return false, e.clockUnsafeErr
	}
	return true, nil
}

func (e *recordingCityRealtimeDueEventExecutor) recoverRealtimeClockWithObservation(
	_ context.Context,
	worldID int64,
	_ CityRealtimeClockObservation,
) (bool, error) {
	e.recoveryWorlds = append(e.recoveryWorlds, worldID)
	if e.recoveryErr != nil {
		return false, e.recoveryErr
	}
	return e.recoveryStarted, nil
}

func TestCityRealtimeSchedulerFailsClosedWhenFeatureIsDisabled(t *testing.T) {
	scheduler := NewCityRealtimeScheduler(nil, nil, nil, time.Second, 1)
	scheduler.SetFeatureChecker(staticCityRealtimeSchedulerChecker{})

	report, err := scheduler.ProcessDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, CityRealtimeSchedulerReport{}, report)
}

func TestCityRealtimeSchedulerClaimsProductionLeaseAndCompletesIt(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	profileHash := strings.Repeat("a", 64)
	mock.ExpectQuery(`SELECT state\.world_id`).
		WithArgs(4).
		WillReturnRows(sqlmock.NewRows([]string{
			"world_id", "clock_state", "id", "clock_profile_hash", "source_clock_mode", "deployment_scope",
			"quantum_us", "maximum_uncertainty_us", "maximum_database_skew_us",
		}).AddRow(
			int64(7), cityRealtimeClockStateHealthy, "production-ntp-v1", profileHash, "system_ntp", "production",
			int64(1_000_000), int64(50_000), int64(500_000),
		))
	mock.ExpectQuery(`UPDATE city_realtime_schedule_states`).
		WithArgs(int64(7), sqlmock.AnyArg(), int64(60_000)).
		WillReturnRows(sqlmock.NewRows([]string{"world_id"}).AddRow(int64(7)))
	mock.ExpectExec(`UPDATE city_realtime_schedule_states`).
		WithArgs(int64(7), sqlmock.AnyArg(), "ntp-node-a").
		WillReturnResult(sqlmock.NewResult(0, 1))

	authority := &staticCityRealtimeClockAuthority{observation: CityRealtimeClockObservation{
		NodeID: "ntp-node-a", SourceClockMode: "system_ntp", HealthState: "healthy",
		EffectiveUTC: time.Date(2026, time.July, 21, 0, 0, 0, 0, time.UTC),
	}}
	executor := &recordingCityRealtimeDueEventExecutor{result: &CityRealtimeDiagnosticDueEventProcessResult{Resolved: true}}
	scheduler := NewCityRealtimeScheduler(db, executor, authority, time.Second, 4)
	scheduler.SetFeatureChecker(staticCityRealtimeSchedulerChecker{cityEnabled: true, realtimeEnabled: true})
	scheduler.lease = time.Minute

	report, err := scheduler.ProcessDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, CityRealtimeSchedulerReport{Candidates: 1, Claimed: 1, Processed: 1}, report)
	require.Equal(t, []int64{7}, executor.worldIDs)
	require.Len(t, authority.profiles, 1)
	require.Equal(t, "production-ntp-v1", authority.profiles[0].ID)
	require.Equal(t, profileHash, authority.profiles[0].Hash)
	require.Len(t, executor.observations, 1)
	require.Equal(t, "ntp-node-a", executor.observations[0].NodeID)
}

func TestCityRealtimeSchedulerBacksOffWhenClockAuthorityIsUnsafe(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	mock.ExpectQuery(`SELECT state\.world_id`).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{
			"world_id", "clock_state", "id", "clock_profile_hash", "source_clock_mode", "deployment_scope",
			"quantum_us", "maximum_uncertainty_us", "maximum_database_skew_us",
		}).AddRow(
			int64(7), cityRealtimeClockStateHealthy, "production-ntp-v1", strings.Repeat("b", 64), "system_ntp", "production",
			int64(1_000_000), int64(50_000), int64(500_000),
		))
	mock.ExpectQuery(`UPDATE city_realtime_schedule_states`).
		WithArgs(int64(7), sqlmock.AnyArg(), int64(60_000)).
		WillReturnRows(sqlmock.NewRows([]string{"world_id"}).AddRow(int64(7)))
	mock.ExpectExec(`UPDATE city_realtime_schedule_states`).
		WithArgs(int64(7), sqlmock.AnyArg(), "CLOCK_UNSAFE").
		WillReturnResult(sqlmock.NewResult(0, 1))

	authority := &staticCityRealtimeClockAuthority{err: ErrCityRealtimeClockUnsafe}
	executor := &recordingCityRealtimeDueEventExecutor{}
	scheduler := NewCityRealtimeScheduler(db, executor, authority, time.Second, 1)
	scheduler.SetFeatureChecker(staticCityRealtimeSchedulerChecker{cityEnabled: true, realtimeEnabled: true})
	scheduler.lease = time.Minute

	report, err := scheduler.ProcessDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, CityRealtimeSchedulerReport{Candidates: 1, Claimed: 1, ClockUnsafe: 1}, report)
	require.Empty(t, executor.worldIDs)
	require.Equal(t, []int64{7}, executor.clockUnsafeWorlds)
}

func TestCityRealtimeSchedulerExcludesNonProductionProfiles(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	mock.ExpectQuery(`SELECT state\.world_id`).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{
			"world_id", "clock_state", "id", "clock_profile_hash", "source_clock_mode", "deployment_scope",
			"quantum_us", "maximum_uncertainty_us", "maximum_database_skew_us",
		}))

	authority := &staticCityRealtimeClockAuthority{}
	executor := &recordingCityRealtimeDueEventExecutor{}
	scheduler := NewCityRealtimeScheduler(db, executor, authority, time.Second, 1)
	scheduler.SetFeatureChecker(staticCityRealtimeSchedulerChecker{cityEnabled: true, realtimeEnabled: true})

	report, err := scheduler.ProcessDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, CityRealtimeSchedulerReport{}, report)
	require.Empty(t, authority.profiles)
	require.Empty(t, executor.worldIDs)
}

func TestCityRealtimeSchedulerSkipsProductionProfileOwnedByAnotherAuthority(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	mock.ExpectQuery(`SELECT state\.world_id`).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{
			"world_id", "clock_state", "id", "clock_profile_hash", "source_clock_mode", "deployment_scope",
			"quantum_us", "maximum_uncertainty_us", "maximum_database_skew_us",
		}).AddRow(
			int64(7), cityRealtimeClockStateHealthy, "production-private-time-v1", strings.Repeat("d", 64), "private_time_service", "production",
			int64(1_000_000), int64(50_000), int64(500_000),
		))

	authority := &profileLimitedCityRealtimeClockAuthority{supports: false}
	executor := &recordingCityRealtimeDueEventExecutor{}
	scheduler := NewCityRealtimeScheduler(db, executor, authority, time.Second, 1)
	scheduler.SetFeatureChecker(staticCityRealtimeSchedulerChecker{cityEnabled: true, realtimeEnabled: true})

	report, err := scheduler.ProcessDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, CityRealtimeSchedulerReport{Candidates: 1, Unsupported: 1}, report)
	require.Empty(t, authority.profiles)
	require.Empty(t, executor.worldIDs)
}

func TestCityRealtimeSchedulerStartsHeldWorldRecoveryAfterHealthyObservation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	mock.ExpectQuery(`SELECT state\.world_id`).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{
			"world_id", "clock_state", "id", "clock_profile_hash", "source_clock_mode", "deployment_scope",
			"quantum_us", "maximum_uncertainty_us", "maximum_database_skew_us",
		}).AddRow(
			int64(7), cityRealtimeClockStateUnsafe, "production-ntp-v1", strings.Repeat("c", 64), "system_ntp", "production",
			int64(1_000_000), int64(50_000), int64(500_000),
		))
	mock.ExpectQuery(`UPDATE city_realtime_schedule_states`).
		WithArgs(int64(7), sqlmock.AnyArg(), int64(60_000)).
		WillReturnRows(sqlmock.NewRows([]string{"world_id"}).AddRow(int64(7)))
	mock.ExpectExec(`UPDATE city_realtime_schedule_states`).
		WithArgs(int64(7), sqlmock.AnyArg(), "ntp-node-a").
		WillReturnResult(sqlmock.NewResult(0, 1))

	authority := &staticCityRealtimeClockAuthority{observation: CityRealtimeClockObservation{
		NodeID: "ntp-node-a", SourceClockMode: "system_ntp", HealthState: cityRealtimeClockStateHealthy,
		EffectiveUTC: time.Date(2026, time.July, 21, 0, 0, 0, 0, time.UTC),
	}}
	executor := &recordingCityRealtimeDueEventExecutor{recoveryStarted: true}
	scheduler := NewCityRealtimeScheduler(db, executor, authority, time.Second, 1)
	scheduler.SetFeatureChecker(staticCityRealtimeSchedulerChecker{cityEnabled: true, realtimeEnabled: true})
	scheduler.lease = time.Minute

	report, err := scheduler.ProcessDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, CityRealtimeSchedulerReport{Candidates: 1, Claimed: 1, Recovered: 1}, report)
	require.Equal(t, []int64{7}, executor.recoveryWorlds)
	require.Empty(t, executor.worldIDs)
}
