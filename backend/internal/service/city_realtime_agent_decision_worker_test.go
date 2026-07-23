package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type staticCityRealtimeAgentDecisionWorkerChecker struct {
	cityEnabled   bool
	workerEnabled bool
}

func (c staticCityRealtimeAgentDecisionWorkerChecker) IsCitySimulationEnabled(context.Context) bool {
	return c.cityEnabled
}

func (c staticCityRealtimeAgentDecisionWorkerChecker) IsCityRealtimeAgentDecisionWorkerEnabled(context.Context) bool {
	return c.workerEnabled
}

type recordingCityRealtimeAgentDecisionWorkerExecutor struct {
	runInputs    []CityRealtimeAgentDecisionRunInput
	runContextOK bool
	result       *CityRealtimeAgentDecisionRunResult
	runErr       error
	deferInputs  []struct {
		worldID     int64
		requestCode string
		reason      string
	}
	deferErr error
}

func (e *recordingCityRealtimeAgentDecisionWorkerExecutor) RunRealtimeAgentDecision(
	ctx context.Context,
	input CityRealtimeAgentDecisionRunInput,
) (*CityRealtimeAgentDecisionRunResult, error) {
	e.runInputs = append(e.runInputs, input)
	e.runContextOK = IsCitySystemAdministrator(ctx)
	return e.result, e.runErr
}

func (e *recordingCityRealtimeAgentDecisionWorkerExecutor) deferRealtimeAgentDecisionDispatch(
	_ context.Context,
	worldID int64,
	requestCode string,
	reason string,
) (*time.Time, error) {
	e.deferInputs = append(e.deferInputs, struct {
		worldID     int64
		requestCode string
		reason      string
	}{worldID: worldID, requestCode: requestCode, reason: reason})
	if e.deferErr != nil {
		return nil, e.deferErr
	}
	deadline := time.Now().UTC().Add(time.Second)
	return &deadline, nil
}

func TestCityRealtimeAgentDecisionWorkerFailsClosedWhenDisabled(t *testing.T) {
	worker := NewCityRealtimeAgentDecisionWorker(nil, nil, time.Second, 1)
	worker.SetFeatureChecker(staticCityRealtimeAgentDecisionWorkerChecker{})

	report, err := worker.ProcessDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, CityRealtimeAgentDecisionWorkerReport{}, report)
}

func TestCityRealtimeAgentDecisionWorkerExecutesQueuedCandidateAsSystemWorker(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	mock.ExpectQuery(`SELECT request\.world_id, request\.request_code`).
		WithArgs(4).
		WillReturnRows(sqlmock.NewRows([]string{"world_id", "request_code"}).AddRow(int64(7), "adr.worker.ready"))
	executor := &recordingCityRealtimeAgentDecisionWorkerExecutor{
		result: &CityRealtimeAgentDecisionRunResult{RequestCode: "adr.worker.ready", Status: cityRealtimeAgentDecisionRequestAccepted},
	}
	worker := NewCityRealtimeAgentDecisionWorker(db, executor, time.Second, 4)
	worker.SetFeatureChecker(staticCityRealtimeAgentDecisionWorkerChecker{cityEnabled: true, workerEnabled: true})

	report, err := worker.ProcessDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, CityRealtimeAgentDecisionWorkerReport{Candidates: 1, Completed: 1}, report)
	require.Len(t, executor.runInputs, 1)
	require.Equal(t, int64(7), executor.runInputs[0].WorldID)
	require.Equal(t, "adr.worker.ready", executor.runInputs[0].RequestCode)
	require.NotEmpty(t, executor.runInputs[0].WorkerID)
	require.True(t, executor.runContextOK)
}

func TestCityRealtimeAgentDecisionWorkerDefersUnavailableProviderWithoutAttempt(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	mock.ExpectQuery(`SELECT request\.world_id, request\.request_code`).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"world_id", "request_code"}).AddRow(int64(8), "adr.worker.provider"))
	executor := &recordingCityRealtimeAgentDecisionWorkerExecutor{runErr: ErrCityRealtimeAgentProviderUnavailable}
	worker := NewCityRealtimeAgentDecisionWorker(db, executor, time.Second, 1)
	worker.SetFeatureChecker(staticCityRealtimeAgentDecisionWorkerChecker{cityEnabled: true, workerEnabled: true})

	report, err := worker.ProcessDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, CityRealtimeAgentDecisionWorkerReport{Candidates: 1, Deferred: 1}, report)
	require.Len(t, executor.deferInputs, 1)
	require.Equal(t, int64(8), executor.deferInputs[0].worldID)
	require.Equal(t, "adr.worker.provider", executor.deferInputs[0].requestCode)
	require.Equal(t, cityRealtimeAgentDecisionDispatchDeferAdapterUnavailable, executor.deferInputs[0].reason)
}

func TestCityRealtimeAgentDecisionWorkerDefersBudgetUntilTheNextWindow(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	mock.ExpectQuery(`SELECT request\.world_id, request\.request_code`).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"world_id", "request_code"}).AddRow(int64(9), "adr.worker.budget"))
	executor := &recordingCityRealtimeAgentDecisionWorkerExecutor{runErr: ErrCityRealtimeAgentModelBudgetExceeded}
	worker := NewCityRealtimeAgentDecisionWorker(db, executor, time.Second, 1)
	worker.SetFeatureChecker(staticCityRealtimeAgentDecisionWorkerChecker{cityEnabled: true, workerEnabled: true})

	report, err := worker.ProcessDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, CityRealtimeAgentDecisionWorkerReport{Candidates: 1, Deferred: 1}, report)
	require.Len(t, executor.deferInputs, 1)
	require.Equal(t, cityRealtimeAgentDecisionDispatchDeferBudgetWindow, executor.deferInputs[0].reason)
}

func TestCityRealtimeAgentDecisionNextBudgetWindowUsesNextUTCHour(t *testing.T) {
	now := time.Date(2026, time.July, 23, 10, 59, 59, 999999000, time.FixedZone("UTC+8", 8*60*60))
	deadline := cityRealtimeAgentDecisionNextBudgetWindow(now)
	require.Equal(t, time.Date(2026, time.July, 23, 4, 0, 1, 0, time.UTC), deadline)
}

func TestCityRealtimeAgentDecisionWorkerClassifiesSafeErrors(t *testing.T) {
	require.Equal(t, "PROVIDER_UNAVAILABLE", cityRealtimeAgentDecisionWorkerErrorCode(ErrCityRealtimeAgentProviderUnavailable))
	require.Equal(t, "MODEL_BUDGET_EXHAUSTED", cityRealtimeAgentDecisionWorkerErrorCode(ErrCityRealtimeAgentModelBudgetExceeded))
	require.Equal(t, "DECISION_QUARANTINED", cityRealtimeAgentDecisionWorkerErrorCode(ErrCityRealtimeAgentDecisionQuarantined))
	require.Equal(t, "DECISION_CONFLICT", cityRealtimeAgentDecisionWorkerErrorCode(ErrCityRealtimeAgentDecisionConflict))
	require.Equal(t, "WORKER_TIMEOUT", cityRealtimeAgentDecisionWorkerErrorCode(context.DeadlineExceeded))
	require.Equal(t, "WORKER_FAILURE", cityRealtimeAgentDecisionWorkerErrorCode(sql.ErrConnDone))
}
