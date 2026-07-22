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
	defaultCityRealtimeSchedulerInterval = time.Second
	defaultCityRealtimeSchedulerBatch    = 32
	defaultCityRealtimeSchedulerTimeout  = 45 * time.Second
	defaultCityRealtimeSchedulerLease    = 60 * time.Second
)

// cityRealtimeSchedulerFeatureChecker deliberately requires both the global
// city feature and a distinct realtime worker opt-in. Realtime remains
// fail-closed until a verified production Clock Authority is wired.
type cityRealtimeSchedulerFeatureChecker interface {
	IsCitySimulationEnabled(ctx context.Context) bool
	IsCityRealtimeSchedulerEnabled(ctx context.Context) bool
}

type cityRealtimeDueEventExecutor interface {
	processRealtimeDueEventsWithClock(
		ctx context.Context,
		worldID int64,
		observation CityRealtimeClockObservation,
		requireDiagnosticProfile bool,
		frameReason string,
	) (*CityRealtimeDiagnosticDueEventProcessResult, error)
	recordRealtimeClockUnsafe(ctx context.Context, worldID int64, errorCode string) (bool, error)
	recoverRealtimeClockWithObservation(
		ctx context.Context,
		worldID int64,
		observation CityRealtimeClockObservation,
	) (bool, error)
}

// cityRealtimeClockAuthorityProfileSupporter is optional so existing test and
// future external authorities can retain the small Observe-only interface.
// Authorities that implement it let the scheduler skip profiles they do not
// own instead of falsely marking another time source unsafe.
type cityRealtimeClockAuthorityProfileSupporter interface {
	Supports(profile CityRealtimeClockProfile) bool
}

type CityRealtimeSchedulerReport struct {
	Candidates  int `json:"candidates"`
	Claimed     int `json:"claimed"`
	Processed   int `json:"processed"`
	Noop        int `json:"noop"`
	Conflicts   int `json:"conflicts"`
	ClockUnsafe int `json:"clock_unsafe"`
	Unsupported int `json:"unsupported"`
	Recovered   int `json:"recovered"`
	Failed      int `json:"failed"`
}

// CityRealtimeScheduler owns only operational leases and Clock Authority
// access. Canonical time and event transitions remain inside the temporal
// frame reducer, so a scheduler retry cannot duplicate a world decision.
type CityRealtimeScheduler struct {
	db             *sql.DB
	executor       cityRealtimeDueEventExecutor
	clockAuthority CityRealtimeClockAuthority
	featureChecker cityRealtimeSchedulerFeatureChecker
	interval       time.Duration
	batch          int
	timeout        time.Duration
	lease          time.Duration
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

func NewCityRealtimeScheduler(
	db *sql.DB,
	executor cityRealtimeDueEventExecutor,
	clockAuthority CityRealtimeClockAuthority,
	interval time.Duration,
	batch int,
) *CityRealtimeScheduler {
	if interval <= 0 {
		interval = defaultCityRealtimeSchedulerInterval
	}
	if batch < 1 || batch > 100 {
		batch = defaultCityRealtimeSchedulerBatch
	}
	if clockAuthority == nil {
		clockAuthority = cityRealtimeDenyClockAuthority{}
	}
	runCtx, runCancel := context.WithCancel(context.Background())
	return &CityRealtimeScheduler{
		db:             db,
		executor:       executor,
		clockAuthority: clockAuthority,
		interval:       interval,
		batch:          batch,
		timeout:        defaultCityRealtimeSchedulerTimeout,
		lease:          defaultCityRealtimeSchedulerLease,
		workerID:       newCityRealtimeSchedulerWorkerID(),
		stopCh:         make(chan struct{}),
		runCtx:         runCtx,
		runCancel:      runCancel,
	}
}

func (s *CityRealtimeScheduler) SetFeatureChecker(checker cityRealtimeSchedulerFeatureChecker) {
	if s != nil {
		s.featureChecker = checker
	}
}

func (s *CityRealtimeScheduler) Start() {
	if s == nil || s.db == nil || s.executor == nil || s.clockAuthority == nil {
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
		logger.LegacyPrintf("service.city_realtime_scheduler", "[CityRealtimeScheduler] started interval=%s batch=%d", s.interval, s.batch)
	})
}

func (s *CityRealtimeScheduler) Stop() {
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
			logger.LegacyPrintf("service.city_realtime_scheduler", "[CityRealtimeScheduler] stopped")
		}
	})
}

func (s *CityRealtimeScheduler) runLoop() {
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

func (s *CityRealtimeScheduler) processOnce() {
	baseCtx := s.runCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, s.timeout)
	defer cancel()
	report, err := s.ProcessDue(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) && errors.Is(baseCtx.Err(), context.Canceled) {
			return
		}
		logger.LegacyPrintf("service.city_realtime_scheduler", "[CityRealtimeScheduler] sweep failed candidates=%d claimed=%d processed=%d unsupported=%d unsafe=%d failed=%d err=%v",
			report.Candidates, report.Claimed, report.Processed, report.Unsupported, report.ClockUnsafe, report.Failed, err)
		return
	}
	if report.Processed > 0 || report.Conflicts > 0 || report.ClockUnsafe > 0 || report.Unsupported > 0 {
		logger.LegacyPrintf("service.city_realtime_scheduler", "[CityRealtimeScheduler] sweep completed candidates=%d claimed=%d processed=%d noop=%d conflicts=%d unsupported=%d unsafe=%d failed=%d",
			report.Candidates, report.Claimed, report.Processed, report.Noop, report.Conflicts, report.Unsupported, report.ClockUnsafe, report.Failed)
	}
}

func (s *CityRealtimeScheduler) ProcessDue(ctx context.Context) (CityRealtimeSchedulerReport, error) {
	report := CityRealtimeSchedulerReport{}
	if s == nil {
		return report, fmt.Errorf("city realtime scheduler is not configured")
	}
	if s.featureChecker == nil || !s.featureChecker.IsCitySimulationEnabled(ctx) ||
		!s.featureChecker.IsCityRealtimeSchedulerEnabled(ctx) {
		return report, nil
	}
	if s.db == nil || s.executor == nil || s.clockAuthority == nil {
		return report, fmt.Errorf("city realtime scheduler is not configured")
	}
	// The worker is a server-owned actor. Its context is deliberately marked
	// before it enters the temporal reducer so it can acquire the world row
	// without impersonating a member or exposing any internal primitive by HTTP.
	workerCtx := WithCitySystemAdministrator(ctx)
	candidates, err := s.listCandidates(ctx)
	if err != nil {
		return report, err
	}
	report.Candidates = len(candidates)
	for _, candidate := range candidates {
		if capability, ok := s.clockAuthority.(cityRealtimeClockAuthorityProfileSupporter); ok && !capability.Supports(candidate.profile) {
			report.Unsupported++
			continue
		}
		leaseToken, claimed, claimErr := s.claimLease(ctx, candidate.worldID)
		if claimErr != nil {
			report.Failed++
			continue
		}
		if !claimed {
			report.Conflicts++
			continue
		}
		report.Claimed++
		observation, observeErr := s.clockAuthority.Observe(workerCtx, candidate.profile)
		if observeErr != nil {
			report.ClockUnsafe++
			if _, markErr := s.executor.recordRealtimeClockUnsafe(workerCtx, candidate.worldID, cityRealtimeSchedulerErrorCode(observeErr)); markErr != nil {
				report.Failed++
				if deferErr := s.deferLease(ctx, candidate.worldID, leaseToken, cityRealtimeSchedulerErrorCode(observeErr)); deferErr != nil {
					return report, errors.Join(markErr, deferErr)
				}
				return report, markErr
			}
			if err = s.deferLease(ctx, candidate.worldID, leaseToken, cityRealtimeSchedulerErrorCode(observeErr)); err != nil {
				return report, err
			}
			continue
		}
		if candidate.clockState == cityRealtimeClockStateUnsafe {
			recovered, recoveryErr := s.executor.recoverRealtimeClockWithObservation(workerCtx, candidate.worldID, observation)
			if recoveryErr != nil {
				if errors.Is(recoveryErr, ErrCityRealtimeClockUnsafe) {
					report.ClockUnsafe++
				} else {
					report.Failed++
				}
				if err = s.deferLease(ctx, candidate.worldID, leaseToken, cityRealtimeSchedulerErrorCode(recoveryErr)); err != nil {
					return report, err
				}
				continue
			}
			if err = s.completeLease(ctx, candidate.worldID, leaseToken, observation.NodeID); err != nil {
				return report, err
			}
			if recovered {
				report.Recovered++
			} else {
				report.Noop++
			}
			continue
		}
		result, processErr := s.executor.processRealtimeDueEventsWithClock(
			workerCtx, candidate.worldID, observation, false, "clock_authority",
		)
		if processErr != nil {
			if errors.Is(processErr, ErrCityRealtimeClockUnsafe) {
				report.ClockUnsafe++
				if _, markErr := s.executor.recordRealtimeClockUnsafe(workerCtx, candidate.worldID, cityRealtimeSchedulerErrorCode(processErr)); markErr != nil {
					report.Failed++
					if deferErr := s.deferLease(ctx, candidate.worldID, leaseToken, cityRealtimeSchedulerErrorCode(processErr)); deferErr != nil {
						return report, errors.Join(markErr, deferErr)
					}
					return report, markErr
				}
			} else {
				report.Failed++
			}
			if err = s.deferLease(ctx, candidate.worldID, leaseToken, cityRealtimeSchedulerErrorCode(processErr)); err != nil {
				return report, err
			}
			continue
		}
		if err = s.completeLease(ctx, candidate.worldID, leaseToken, observation.NodeID); err != nil {
			return report, err
		}
		if result.Resolved {
			report.Processed++
		} else {
			report.Noop++
		}
	}
	return report, nil
}

type cityRealtimeSchedulerCandidate struct {
	worldID    int64
	clockState string
	profile    CityRealtimeClockProfile
}

func (s *CityRealtimeScheduler) listCandidates(ctx context.Context) ([]cityRealtimeSchedulerCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT state.world_id, state.clock_state, profile.id, state.clock_profile_hash,
       profile.source_clock_mode, profile.deployment_scope, profile.quantum_us,
       profile.maximum_uncertainty_us, profile.maximum_database_skew_us
FROM city_world_time_states state
JOIN city_worlds world ON world.id = state.world_id
JOIN city_clock_profiles profile ON profile.id = state.clock_profile_id
JOIN city_realtime_schedule_states schedule ON schedule.world_id = state.world_id
WHERE world.simulation_version IN ('city-openworld-realtime-v1', 'city-openworld-realtime-v2')
  AND world.status = 'running'
  AND state.lifecycle_status = 'running'
  AND profile.deployment_scope = 'production'
  AND (
        (state.clock_state IN ('initializing', 'healthy')
         AND state.next_due_at_world_time_us IS NOT NULL)
     OR (state.clock_state = 'recovering'
         AND state.recovery_state = 'catching_up'
         AND state.catchup_target_world_time_us IS NOT NULL)
     OR (state.clock_state = 'unsafe'
         AND state.recovery_state = 'held')
  )
  AND (schedule.retry_not_before IS NULL OR schedule.retry_not_before <= NOW())
ORDER BY state.next_due_at_world_time_us ASC, state.world_id ASC
LIMIT $1`, s.batch)
	if err != nil {
		return nil, fmt.Errorf("list city realtime scheduler candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]cityRealtimeSchedulerCandidate, 0, s.batch)
	for rows.Next() {
		item := cityRealtimeSchedulerCandidate{}
		if err = rows.Scan(
			&item.worldID, &item.clockState, &item.profile.ID, &item.profile.Hash,
			&item.profile.SourceClockMode, &item.profile.DeploymentScope,
			&item.profile.TimeQuantumUS, &item.profile.MaximumUncertaintyUS,
			&item.profile.MaximumDatabaseSkewUS,
		); err != nil {
			return nil, fmt.Errorf("scan city realtime scheduler candidate: %w", err)
		}
		if item.worldID <= 0 || !cityRealtimeSHA256Hex(item.profile.Hash) ||
			item.profile.DeploymentScope != "production" ||
			(item.clockState != cityRealtimeClockStateInitializing &&
				item.clockState != cityRealtimeClockStateHealthy &&
				item.clockState != cityRealtimeClockStateRecovering &&
				item.clockState != cityRealtimeClockStateUnsafe) ||
			item.profile.TimeQuantumUS != cityRealtimeTimeQuantumUS {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_scheduler_candidate"})
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city realtime scheduler candidates: %w", err)
	}
	return items, nil
}

func (s *CityRealtimeScheduler) claimLease(ctx context.Context, worldID int64) (string, bool, error) {
	leaseMilliseconds := s.lease.Milliseconds()
	if leaseMilliseconds <= 0 {
		leaseMilliseconds = defaultCityRealtimeSchedulerLease.Milliseconds()
	}
	token, err := newCityRealtimeLeaseToken()
	if err != nil {
		return "", false, err
	}
	var claimedWorldID int64
	err = s.db.QueryRowContext(ctx, `
UPDATE city_realtime_schedule_states
SET lease_token = $2,
    lease_expires_at = NOW() + ($3 * INTERVAL '1 millisecond'),
    last_attempt_at = NOW(),
    updated_at = NOW()
WHERE world_id = $1
  AND (lease_token IS NULL OR lease_expires_at <= NOW())
  AND (retry_not_before IS NULL OR retry_not_before <= NOW())
RETURNING world_id`, worldID, token, leaseMilliseconds).Scan(&claimedWorldID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("claim city realtime scheduler lease: %w", err)
	}
	if claimedWorldID != worldID {
		return "", false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_scheduler_lease"})
	}
	return token, true, nil
}

func (s *CityRealtimeScheduler) completeLease(ctx context.Context, worldID int64, leaseToken, nodeID string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE city_realtime_schedule_states
SET lease_token = NULL,
    lease_expires_at = NULL,
    node_id = $3,
    consecutive_failures = 0,
    retry_not_before = NULL,
    last_success_at = NOW(),
    last_error_code = NULL,
    last_error_detail = NULL,
    updated_at = NOW()
WHERE world_id = $1 AND lease_token = $2`, worldID, leaseToken, nodeID)
	if err != nil {
		return fmt.Errorf("complete city realtime scheduler lease: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check city realtime scheduler completion: %w", err)
	}
	if rowsAffected != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_scheduler_lease_completion"})
	}
	return nil
}

func (s *CityRealtimeScheduler) deferLease(ctx context.Context, worldID int64, leaseToken, errorCode string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE city_realtime_schedule_states
SET lease_token = NULL,
    lease_expires_at = NULL,
    consecutive_failures = LEAST(consecutive_failures + 1, 1000000),
    retry_not_before = NOW() + (LEAST((consecutive_failures + 1) * 5, 300) * INTERVAL '1 second'),
    last_error_code = $3,
    last_error_detail = 'realtime scheduler attempt failed',
    updated_at = NOW()
WHERE world_id = $1 AND lease_token = $2`, worldID, leaseToken, errorCode)
	if err != nil {
		return fmt.Errorf("defer city realtime scheduler lease: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check city realtime scheduler deferral: %w", err)
	}
	if rowsAffected != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_scheduler_lease_deferral"})
	}
	return nil
}

func cityRealtimeSchedulerErrorCode(err error) string {
	if errors.Is(err, ErrCityRealtimeClockUnsafe) {
		return "CLOCK_UNSAFE"
	}
	if errors.Is(err, ErrCitySimulationInvariant) {
		return "SIMULATION_INVARIANT"
	}
	return "SCHEDULER_FAILURE"
}

func newCityRealtimeSchedulerWorkerID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "realtime-scheduler-v1-fallback"
	}
	return "realtime-scheduler-v1-" + hex.EncodeToString(raw[:])
}

func newCityRealtimeLeaseToken() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate city realtime scheduler lease token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
