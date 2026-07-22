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
	defaultCityTickSchedulerInterval = time.Second
	defaultCityTickSchedulerBatch    = 32
	defaultCityTickSchedulerTimeout  = 45 * time.Second
	defaultCityTickSchedulerLease    = 60 * time.Second
	cityRealHourMilliseconds         = int64(time.Hour / time.Millisecond)
)

type cityWorldStepExecutor interface {
	StepWorld(ctx context.Context, input CityStepInput) (*CityStepResult, error)
}

type citySimulationEnabledChecker interface {
	IsCitySimulationEnabled(ctx context.Context) bool
}

type CityTickSchedulerReport struct {
	Candidates int `json:"candidates"`
	Processed  int `json:"processed"`
	Conflicts  int `json:"conflicts"`
	Failed     int `json:"failed"`
}

type CityTickScheduler struct {
	db             *sql.DB
	executor       cityWorldStepExecutor
	featureChecker citySimulationEnabledChecker
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

// SetCitySimulationEnabledChecker makes scheduled ticks observe the same
// feature switch as the HTTP API. It is called while the scheduler is wired,
// before Start, and intentionally keeps a nil checker backward compatible.
func (s *CityTickScheduler) SetCitySimulationEnabledChecker(checker citySimulationEnabledChecker) {
	if s != nil {
		s.featureChecker = checker
	}
}

func NewCityTickScheduler(
	db *sql.DB,
	executor cityWorldStepExecutor,
	interval time.Duration,
	batch int,
) *CityTickScheduler {
	if interval <= 0 {
		interval = defaultCityTickSchedulerInterval
	}
	if batch < 1 || batch > 100 {
		batch = defaultCityTickSchedulerBatch
	}
	runCtx, runCancel := context.WithCancel(context.Background())
	return &CityTickScheduler{
		db: db, executor: executor, interval: interval, batch: batch,
		timeout: defaultCityTickSchedulerTimeout, lease: defaultCityTickSchedulerLease,
		workerID: newCityTickSchedulerWorkerID(), stopCh: make(chan struct{}),
		runCtx: runCtx, runCancel: runCancel,
	}
}

func (s *CityTickScheduler) Start() {
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
		logger.LegacyPrintf("service.city_tick_scheduler", "[CityTickScheduler] started interval=%s batch=%d", s.interval, s.batch)
	})
}

func (s *CityTickScheduler) Stop() {
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
		if !started {
			return
		}
		s.wait.Wait()
		logger.LegacyPrintf("service.city_tick_scheduler", "[CityTickScheduler] stopped")
	})
}

func (s *CityTickScheduler) runLoop() {
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

func (s *CityTickScheduler) processOnce() {
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
		logger.LegacyPrintf("service.city_tick_scheduler", "[CityTickScheduler] sweep failed candidates=%d processed=%d conflicts=%d failed=%d err=%v",
			report.Candidates, report.Processed, report.Conflicts, report.Failed, err)
		return
	}
	if report.Processed > 0 || report.Conflicts > 0 {
		logger.LegacyPrintf("service.city_tick_scheduler", "[CityTickScheduler] sweep completed candidates=%d processed=%d conflicts=%d",
			report.Candidates, report.Processed, report.Conflicts)
	}
}

func (s *CityTickScheduler) ProcessDue(ctx context.Context) (CityTickSchedulerReport, error) {
	report := CityTickSchedulerReport{}
	if s == nil {
		return report, fmt.Errorf("city tick scheduler is not configured")
	}
	if s.featureChecker != nil && !s.featureChecker.IsCitySimulationEnabled(ctx) {
		return report, nil
	}
	if s.db == nil || s.executor == nil {
		return report, fmt.Errorf("city tick scheduler is not configured")
	}
	leaseMilliseconds := s.lease.Milliseconds()
	if leaseMilliseconds < 1 {
		leaseMilliseconds = defaultCityTickSchedulerLease.Milliseconds()
	}
	rows, err := s.db.QueryContext(ctx, `
WITH due AS (
    SELECT world.id, world.owner_user_id, world.current_tick,
           CASE WHEN EXISTS (
               SELECT 1 FROM city_commands command
               WHERE command.world_id = world.id AND command.status = 'pending'
           ) THEN 0 ELSE 1 END AS priority,
           world.next_tick_at
    FROM city_worlds world
    LEFT JOIN city_world_schedule_states schedule ON schedule.world_id = world.id
    WHERE world.status IN ('paused', 'running')
      -- Realtime worlds have their own Temporal Frame scheduler and must
      -- never be claimed by this legacy one-hour tick reducer.
      AND world.simulation_version NOT IN ('city-openworld-realtime-v1', 'city-openworld-realtime-v2')
      AND (
          (world.status = 'running' AND (world.next_tick_at IS NULL OR world.next_tick_at <= clock_timestamp()))
          OR EXISTS (
              SELECT 1 FROM city_commands command
              WHERE command.world_id = world.id AND command.status = 'pending'
          )
      )
      AND (schedule.retry_not_before IS NULL OR schedule.retry_not_before <= clock_timestamp())
      AND (schedule.lease_expires_at IS NULL OR schedule.lease_expires_at <= clock_timestamp())
    ORDER BY priority, world.next_tick_at NULLS FIRST, world.id
    LIMIT $1
), claimed AS (
    INSERT INTO city_world_schedule_states AS schedule
        (world_id, lease_token, lease_expires_at, last_attempt_at, updated_at)
    SELECT due.id, $2, clock_timestamp() + ($3 * INTERVAL '1 millisecond'),
           clock_timestamp(), clock_timestamp()
    FROM due
    ON CONFLICT (world_id) DO UPDATE
    SET lease_token = EXCLUDED.lease_token,
        lease_expires_at = EXCLUDED.lease_expires_at,
        last_attempt_at = EXCLUDED.last_attempt_at,
        updated_at = EXCLUDED.updated_at
    WHERE schedule.lease_expires_at IS NULL OR schedule.lease_expires_at <= clock_timestamp()
    RETURNING schedule.world_id
)
SELECT due.id, due.owner_user_id, due.current_tick
FROM due
JOIN claimed ON claimed.world_id = due.id
ORDER BY due.priority, due.next_tick_at NULLS FIRST, due.id`,
		s.batch, s.workerID, leaseMilliseconds)
	if err != nil {
		return report, fmt.Errorf("list due city worlds: %w", err)
	}
	type candidate struct {
		worldID, ownerUserID, currentTick int64
	}
	candidates := make([]candidate, 0, s.batch)
	for rows.Next() {
		var item candidate
		if err = rows.Scan(&item.worldID, &item.ownerUserID, &item.currentTick); err != nil {
			_ = rows.Close()
			return report, fmt.Errorf("scan due city world: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err = closeCityRows(rows, "iterate due city worlds"); err != nil {
		return report, err
	}
	report.Candidates = len(candidates)
	var failures []error
	for _, item := range candidates {
		if err = ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		expectedTick := item.currentTick
		_, stepErr := s.executor.StepWorld(ctx, CityStepInput{
			UserID: item.ownerUserID, WorldID: item.worldID,
			IdempotencyKey:    fmt.Sprintf("city-scheduler-v1-%d-%d", item.worldID, item.currentTick),
			ExpectedWorldTick: &expectedTick,
		})
		if stepErr == nil {
			report.Processed++
			if releaseErr := s.completeLease(ctx, item.worldID); releaseErr != nil {
				report.Failed++
				failures = append(failures, releaseErr)
			}
			continue
		}
		if ctx.Err() != nil {
			failures = append(failures, ctx.Err())
			break
		}
		if errors.Is(stepErr, ErrCityExpectedTickConflict) || errors.Is(stepErr, ErrCityStepIdempotencyConflict) {
			report.Conflicts++
			if releaseErr := s.releaseLease(ctx, item.worldID); releaseErr != nil {
				report.Failed++
				failures = append(failures, releaseErr)
			}
			continue
		}
		report.Failed++
		if deferErr := s.deferLease(ctx, item.worldID, stepErr); deferErr != nil {
			failures = append(failures, deferErr)
		}
		failures = append(failures, fmt.Errorf("step scheduled city world %d at tick %d: %w", item.worldID, item.currentTick, stepErr))
	}
	return report, errors.Join(failures...)
}

func (s *CityTickScheduler) completeLease(ctx context.Context, worldID int64) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE city_world_schedule_states
SET lease_token = NULL, lease_expires_at = NULL,
    consecutive_failures = 0, retry_not_before = NULL,
    last_success_at = clock_timestamp(), last_error_code = NULL,
    last_error_detail = NULL, updated_at = clock_timestamp()
WHERE world_id = $1 AND lease_token = $2`, worldID, s.workerID)
	return validateCitySchedulerLeaseResult(result, err, worldID, "complete")
}

func (s *CityTickScheduler) releaseLease(ctx context.Context, worldID int64) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE city_world_schedule_states
SET lease_token = NULL, lease_expires_at = NULL, updated_at = clock_timestamp()
WHERE world_id = $1 AND lease_token = $2`, worldID, s.workerID)
	return validateCitySchedulerLeaseResult(result, err, worldID, "release")
}

func (s *CityTickScheduler) deferLease(ctx context.Context, worldID int64, cause error) error {
	detailRunes := []rune(cause.Error())
	if len(detailRunes) > 1000 {
		detailRunes = detailRunes[:1000]
	}
	detail := string(detailRunes)
	result, err := s.db.ExecContext(ctx, `
UPDATE city_world_schedule_states
SET lease_token = NULL, lease_expires_at = NULL,
    consecutive_failures = LEAST(consecutive_failures + 1, 1000000),
    retry_not_before = clock_timestamp() + (
        LEAST(300000::numeric, 1000::numeric * POWER(2::numeric, LEAST(consecutive_failures, 8)))
        * INTERVAL '1 millisecond'
    ),
    last_error_code = $3, last_error_detail = $4, updated_at = clock_timestamp()
WHERE world_id = $1 AND lease_token = $2`,
		worldID, s.workerID, fmt.Sprintf("%T", cause), detail)
	return validateCitySchedulerLeaseResult(result, err, worldID, "defer")
}

func validateCitySchedulerLeaseResult(result sql.Result, err error, worldID int64, operation string) error {
	if err != nil {
		return fmt.Errorf("%s city tick lease for world %d: %w", operation, worldID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect %s city tick lease for world %d: %w", operation, worldID, err)
	}
	if affected != 1 {
		return fmt.Errorf("%s city tick lease for world %d: lease ownership lost", operation, worldID)
	}
	return nil
}

func newCityTickSchedulerWorkerID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "city-" + hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("city-fallback-%d", time.Now().UTC().UnixNano())
}

func cityRealTickDelayMilliseconds(status string, speedMilli int64) (int64, error) {
	if status == CityWorldStatusPaused {
		return 0, nil
	}
	if status != CityWorldStatusRunning || speedMilli < 1 || speedMilli > 1_000_000 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "scheduler_delay"})
	}
	numerator := cityRealHourMilliseconds * 1000
	return (numerator + speedMilli - 1) / speedMilli, nil
}
