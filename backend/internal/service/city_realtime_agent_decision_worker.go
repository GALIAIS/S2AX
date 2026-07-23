package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	defaultCityRealtimeAgentDecisionWorkerInterval = time.Second
	defaultCityRealtimeAgentDecisionWorkerBatch    = 16
	defaultCityRealtimeAgentDecisionWorkerTimeout  = 315 * time.Second
	defaultCityRealtimeAgentDecisionWorkerDefer    = 30 * time.Second

	cityRealtimeAgentDecisionDispatchDeferAdapterUnavailable = "adapter_unavailable"
	cityRealtimeAgentDecisionDispatchDeferBudgetWindow       = "budget_window"
)

// cityRealtimeAgentDecisionWorkerFeatureChecker keeps model execution behind
// a dedicated, fail-closed switch. It intentionally does not reuse the clock
// scheduler flag: a deployment may observe and advance realtime clocks while
// keeping all external Agent model work disabled.
type cityRealtimeAgentDecisionWorkerFeatureChecker interface {
	IsCitySimulationEnabled(ctx context.Context) bool
	IsCityRealtimeAgentDecisionWorkerEnabled(ctx context.Context) bool
}

type cityRealtimeAgentDecisionWorkerExecutor interface {
	RunRealtimeAgentDecision(ctx context.Context, input CityRealtimeAgentDecisionRunInput) (*CityRealtimeAgentDecisionRunResult, error)
	deferRealtimeAgentDecisionDispatch(ctx context.Context, worldID int64, requestCode string, reason string) (*time.Time, error)
}

// CityRealtimeAgentDecisionWorkerReport contains only safe queue counters.
// It deliberately excludes provider messages, endpoints, credentials, model
// prompts, response bodies, account identity and worker lease information.
type CityRealtimeAgentDecisionWorkerReport struct {
	Candidates int `json:"candidates"`
	Completed  int `json:"completed"`
	Retried    int `json:"retried"`
	Deferred   int `json:"deferred"`
	Terminal   int `json:"terminal"`
	Conflicts  int `json:"conflicts"`
	Failed     int `json:"failed"`
}

type cityRealtimeAgentDecisionWorkerCandidate struct {
	worldID     int64
	requestCode string
}

// CityRealtimeAgentDecisionWorker consumes already-sealed Agent decision
// outbox rows. It cannot create observations, choose profiles, impersonate a
// browser user or bypass the reducer. Each invocation is delegated to the
// same profile/budget/breaker guarded service boundary used by tests.
type CityRealtimeAgentDecisionWorker struct {
	db             *sql.DB
	executor       cityRealtimeAgentDecisionWorkerExecutor
	featureChecker cityRealtimeAgentDecisionWorkerFeatureChecker
	interval       time.Duration
	batch          int
	timeout        time.Duration
	workerID       string

	startOnce   sync.Once
	stopOnce    sync.Once
	lifecycleMu sync.Mutex
	started     bool
	stopped     bool
	stopCh      chan struct{}
	runCtx      context.Context
	runCancel   context.CancelFunc
	wait        sync.WaitGroup
}

func NewCityRealtimeAgentDecisionWorker(
	db *sql.DB,
	executor cityRealtimeAgentDecisionWorkerExecutor,
	interval time.Duration,
	batch int,
) *CityRealtimeAgentDecisionWorker {
	if interval <= 0 {
		interval = defaultCityRealtimeAgentDecisionWorkerInterval
	}
	if batch < 1 || batch > 100 {
		batch = defaultCityRealtimeAgentDecisionWorkerBatch
	}
	runCtx, runCancel := context.WithCancel(context.Background())
	return &CityRealtimeAgentDecisionWorker{
		db:        db,
		executor:  executor,
		interval:  interval,
		batch:     batch,
		timeout:   defaultCityRealtimeAgentDecisionWorkerTimeout,
		workerID:  newCityRealtimeAgentDecisionWorkerID(),
		stopCh:    make(chan struct{}),
		runCtx:    runCtx,
		runCancel: runCancel,
	}
}

func (s *CityRealtimeAgentDecisionWorker) SetFeatureChecker(checker cityRealtimeAgentDecisionWorkerFeatureChecker) {
	if s != nil {
		s.featureChecker = checker
	}
}

func (s *CityRealtimeAgentDecisionWorker) Start() {
	if s == nil || s.db == nil || s.executor == nil {
		return
	}
	s.startOnce.Do(func() {
		s.lifecycleMu.Lock()
		if s.stopped {
			s.lifecycleMu.Unlock()
			return
		}
		s.wait.Add(1)
		s.started = true
		s.lifecycleMu.Unlock()
		go s.runLoop()
		logger.LegacyPrintf("service.city_realtime_agent_decision_worker", "[CityRealtimeAgentDecisionWorker] started interval=%s batch=%d", s.interval, s.batch)
	})
}

func (s *CityRealtimeAgentDecisionWorker) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		s.lifecycleMu.Lock()
		s.stopped = true
		close(s.stopCh)
		if s.runCancel != nil {
			s.runCancel()
		}
		started := s.started
		s.lifecycleMu.Unlock()
		if started {
			s.wait.Wait()
			logger.LegacyPrintf("service.city_realtime_agent_decision_worker", "[CityRealtimeAgentDecisionWorker] stopped")
		}
	})
}

func (s *CityRealtimeAgentDecisionWorker) runLoop() {
	defer s.wait.Done()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.processOnce()
	for {
		select {
		case <-ticker.C:
			s.processOnce()
		case <-s.stopCh:
			return
		}
	}
}

func (s *CityRealtimeAgentDecisionWorker) processOnce() {
	baseCtx := s.runCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	report, err := s.ProcessDue(baseCtx)
	if err != nil {
		if errors.Is(err, context.Canceled) && errors.Is(baseCtx.Err(), context.Canceled) {
			return
		}
		logger.LegacyPrintf("service.city_realtime_agent_decision_worker", "[CityRealtimeAgentDecisionWorker] sweep failed candidates=%d completed=%d retried=%d deferred=%d terminal=%d conflicts=%d failed=%d code=%s",
			report.Candidates, report.Completed, report.Retried, report.Deferred, report.Terminal, report.Conflicts, report.Failed,
			cityRealtimeAgentDecisionWorkerErrorCode(err))
		return
	}
	if report.Completed > 0 || report.Retried > 0 || report.Deferred > 0 || report.Terminal > 0 || report.Conflicts > 0 || report.Failed > 0 {
		logger.LegacyPrintf("service.city_realtime_agent_decision_worker", "[CityRealtimeAgentDecisionWorker] sweep completed candidates=%d completed=%d retried=%d deferred=%d terminal=%d conflicts=%d failed=%d",
			report.Candidates, report.Completed, report.Retried, report.Deferred, report.Terminal, report.Conflicts, report.Failed)
	}
}

// ProcessDue is intentionally callable by tests and controlled operational
// tooling. It has no HTTP route. Individual provider calls receive an
// independent bounded timeout so a long profile cannot inherit a shorter
// scheduler-sweep deadline or block cleanup forever.
func (s *CityRealtimeAgentDecisionWorker) ProcessDue(ctx context.Context) (CityRealtimeAgentDecisionWorkerReport, error) {
	report := CityRealtimeAgentDecisionWorkerReport{}
	if s == nil {
		return report, fmt.Errorf("city realtime agent decision worker is not configured")
	}
	if s.featureChecker == nil || !s.featureChecker.IsCitySimulationEnabled(ctx) ||
		!s.featureChecker.IsCityRealtimeAgentDecisionWorkerEnabled(ctx) {
		return report, nil
	}
	if s.db == nil || s.executor == nil {
		return report, fmt.Errorf("city realtime agent decision worker is not configured")
	}
	candidates, err := s.listCandidates(ctx)
	if err != nil {
		return report, err
	}
	report.Candidates = len(candidates)
	workerCtx := WithCitySystemAdministrator(ctx)
	for _, candidate := range candidates {
		candidateCtx, cancel := context.WithTimeout(workerCtx, s.timeout)
		result, runErr := s.executor.RunRealtimeAgentDecision(candidateCtx, CityRealtimeAgentDecisionRunInput{
			WorldID: candidate.worldID, RequestCode: candidate.requestCode, WorkerID: s.workerID,
		})
		cancel()
		if runErr != nil {
			s.handleCandidateError(workerCtx, candidate, runErr, &report)
			continue
		}
		if result == nil {
			report.Failed++
			continue
		}
		switch result.Status {
		case cityRealtimeAgentDecisionRequestAccepted,
			cityRealtimeAgentDecisionRequestRejected,
			cityRealtimeAgentDecisionRequestStale:
			report.Completed++
		case cityRealtimeAgentDecisionRequestFailed,
			"cancelled":
			report.Terminal++
		case cityRealtimeAgentDecisionRequestQueued:
			if result.RetryNotBefore != nil {
				report.Retried++
			} else {
				report.Conflicts++
			}
		default:
			report.Failed++
		}
	}
	return report, nil
}

func (s *CityRealtimeAgentDecisionWorker) handleCandidateError(
	ctx context.Context,
	candidate cityRealtimeAgentDecisionWorkerCandidate,
	runErr error,
	report *CityRealtimeAgentDecisionWorkerReport,
) {
	if report == nil {
		return
	}
	switch {
	case errors.Is(runErr, ErrCityRealtimeAgentDecisionQuarantined),
		errors.Is(runErr, ErrCityRealtimeAgentDecisionConflict),
		errors.Is(runErr, ErrCityRealtimeAgentDecisionNotFound):
		report.Conflicts++
	case errors.Is(runErr, ErrCityRealtimeAgentProviderUnavailable),
		errors.Is(runErr, ErrCityRealtimeAgentModelBudgetExceeded):
		reason := cityRealtimeAgentDecisionDispatchDeferAdapterUnavailable
		if errors.Is(runErr, ErrCityRealtimeAgentModelBudgetExceeded) {
			reason = cityRealtimeAgentDecisionDispatchDeferBudgetWindow
		}
		deferCtx, cancel := cityRealtimeAgentDecisionFinalizerContext(ctx)
		_, deferErr := s.executor.deferRealtimeAgentDecisionDispatch(deferCtx, candidate.worldID, candidate.requestCode, reason)
		cancel()
		if deferErr != nil {
			report.Failed++
			return
		}
		report.Deferred++
	default:
		report.Failed++
	}
}

// cityRealtimeAgentDecisionNextBudgetWindow returns the first safe sweep time
// after the UTC hourly ledgers roll over. All A4 budget scopes use the same
// UTC hour, so this avoids hammering a profile every few seconds once any
// scope is exhausted. A one-second guard absorbs host/database clock skew at
// the boundary without changing the ledger's actual window definition.
func cityRealtimeAgentDecisionNextBudgetWindow(now time.Time) time.Time {
	now = now.UTC().Truncate(time.Microsecond)
	return now.Truncate(time.Hour).Add(time.Hour).Add(time.Second)
}

func (s *CityRealtimeAgentDecisionWorker) listCandidates(ctx context.Context) ([]cityRealtimeAgentDecisionWorkerCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT request.world_id, request.request_code
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_outbox outbox
  ON outbox.world_id = request.world_id AND outbox.request_code = request.request_code
JOIN city_worlds world ON world.id = request.world_id
JOIN city_world_time_states state ON state.world_id = request.world_id
WHERE world.simulation_version = 'city-openworld-realtime-v2'
  AND world.status = 'running'
  AND state.lifecycle_status = 'running'
  AND request.status = 'queued'
  AND outbox.status = 'queued'
  AND (request.retry_not_before IS NULL OR request.retry_not_before <= NOW())
  AND NOT EXISTS (
      SELECT 1
      FROM city_realtime_agent_decision_dead_letters dead_letter
      WHERE dead_letter.world_id = request.world_id
        AND dead_letter.request_code = request.request_code
        AND dead_letter.dead_letter_status = 'quarantined'
  )
ORDER BY COALESCE(request.retry_not_before, request.created_at) ASC,
         request.created_at ASC, request.world_id ASC, request.request_code ASC
LIMIT $1`, s.batch)
	if err != nil {
		return nil, fmt.Errorf("list city realtime agent decision worker candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]cityRealtimeAgentDecisionWorkerCandidate, 0, s.batch)
	for rows.Next() {
		item := cityRealtimeAgentDecisionWorkerCandidate{}
		if err = rows.Scan(&item.worldID, &item.requestCode); err != nil {
			return nil, fmt.Errorf("scan city realtime agent decision worker candidate: %w", err)
		}
		if item.worldID <= 0 || !cityRealtimeAgentIdentifierValid(item.requestCode, 96) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_worker_candidate"})
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city realtime agent decision worker candidates: %w", err)
	}
	return items, nil
}

// deferRealtimeAgentDecisionDispatch keeps a queue row operationally dormant
// when its process-local adapter is unavailable, its breaker is cooling down,
// or a bounded profile budget is temporarily full. It neither creates an
// attempt nor writes a frame, so the next eligible worker is still the sole
// authority that can perform a decision.
func (s *CityEconomyService) deferRealtimeAgentDecisionDispatch(
	ctx context.Context,
	worldID int64,
	requestCode string,
	reason string,
) (*time.Time, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if s == nil || s.db == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(requestCode, 96) ||
		(reason != cityRealtimeAgentDecisionDispatchDeferAdapterUnavailable &&
			reason != cityRealtimeAgentDecisionDispatchDeferBudgetWindow) {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent decision dispatch deferral transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(worldID)); err != nil {
		return nil, fmt.Errorf("lock realtime agent decision dispatch deferral world: %w", err)
	}
	request, found, err := loadCityRealtimeAgentDecisionRequest(ctx, tx, worldID, requestCode, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeAgentDecisionNotFound
	}
	if cityRealtimeAgentDecisionRequestStatusTerminal(request.Status) {
		return nil, nil
	}
	quarantined, err := cityRealtimeAgentDecisionQuarantined(ctx, tx, worldID, requestCode, true)
	if err != nil {
		return nil, err
	}
	if quarantined {
		return nil, ErrCityRealtimeAgentDecisionQuarantined
	}
	if request.Status != cityRealtimeAgentDecisionRequestQueued || request.LeaseOwner != nil || request.LeaseExpiresAt != nil {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_dispatch_state"})
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if request.RetryNotBefore != nil && request.RetryNotBefore.After(now) {
		return cityRealtimeAgentDecisionRetryNotBeforeCopy(request.RetryNotBefore), nil
	}
	deadline := now.Add(defaultCityRealtimeAgentDecisionWorkerDefer).UTC().Truncate(time.Microsecond)
	if reason == cityRealtimeAgentDecisionDispatchDeferBudgetWindow {
		deadline = cityRealtimeAgentDecisionNextBudgetWindow(now)
	}
	profile, err := cityRealtimeAgentDecisionExecutionProfile(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	if profile != nil {
		breaker, breakerFound, breakerErr := loadCityRealtimeAgentModelCircuitBreakerForUpdate(ctx, tx, profile)
		if breakerErr != nil {
			return nil, breakerErr
		}
		if breakerFound && breaker.CooldownUntil != nil && breaker.CooldownUntil.After(deadline) {
			deadline = breaker.CooldownUntil.UTC().Truncate(time.Microsecond)
		}
	}
	if err = enableCityRealtimeAgentDecisionWorkerGate(ctx, tx, worldID, requestCode); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_requests
SET retry_not_before = $3, updated_at = NOW()
WHERE world_id = $1 AND request_code = $2
  AND status = 'queued' AND lease_owner IS NULL AND lease_expires_at IS NULL
  AND (retry_not_before IS NULL OR retry_not_before <= NOW())`, worldID, requestCode, deadline)
	if err != nil {
		return nil, fmt.Errorf("defer realtime agent decision dispatch: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return nil, fmt.Errorf("check realtime agent decision dispatch deferral: %w", rowsErr)
	} else if rows != 1 {
		return nil, ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "request_dispatch_deferral"})
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent decision dispatch deferral: %w", err)
	}
	return &deadline, nil
}

func cityRealtimeAgentDecisionWorkerErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "WORKER_CANCELLED"
	case errors.Is(err, context.DeadlineExceeded):
		return "WORKER_TIMEOUT"
	case errors.Is(err, ErrCityRealtimeAgentProviderUnavailable):
		return "PROVIDER_UNAVAILABLE"
	case errors.Is(err, ErrCityRealtimeAgentModelBudgetExceeded):
		return "MODEL_BUDGET_EXHAUSTED"
	case errors.Is(err, ErrCityRealtimeAgentDecisionQuarantined):
		return "DECISION_QUARANTINED"
	case errors.Is(err, ErrCityRealtimeAgentDecisionConflict):
		return "DECISION_CONFLICT"
	case errors.Is(err, ErrCitySimulationInvariant):
		return "SIMULATION_INVARIANT"
	default:
		return "WORKER_FAILURE"
	}
}

func newCityRealtimeAgentDecisionWorkerID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "agent-decision-worker-v1-fallback"
	}
	return "agent-decision-worker-v1-" + hex.EncodeToString(raw[:])
}
