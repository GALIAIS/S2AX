package securityaudit

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type EnforcementWorker struct {
	repo        *PostgreSQLRepository
	owner       string
	concurrency int
	poll        time.Duration
	lease       time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

const securityAuditMaintenanceMaxBatches = 20

func NewEnforcementWorker(repo *PostgreSQLRepository) *EnforcementWorker {
	return &EnforcementWorker{
		repo: repo, owner: fmt.Sprintf("security-audit-%s", newSecurityID("worker")),
		concurrency: 2, poll: 500 * time.Millisecond, lease: 30 * time.Second,
	}
}

func (w *EnforcementWorker) Start(ctx context.Context) {
	if w == nil || w.repo == nil || w.repo.db == nil {
		return
	}
	w.mu.Lock()
	if w.cancel != nil {
		w.mu.Unlock()
		return
	}
	workerCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.mu.Unlock()
	for i := 0; i < w.concurrency; i++ {
		w.wg.Add(1)
		go w.run(workerCtx)
	}
	w.wg.Add(1)
	go w.maintenance(workerCtx)
}

func (w *EnforcementWorker) Shutdown(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	cancel := w.cancel
	w.cancel = nil
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *EnforcementWorker) run(ctx context.Context) {
	defer w.wg.Done()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		action, ok, err := w.repo.ClaimNextAction(ctx, w.owner, w.lease)
		if err != nil {
			slog.Warn("security_audit.action_claim_failed", "error", err)
			timer.Reset(w.poll)
			continue
		}
		if !ok {
			timer.Reset(w.poll)
			continue
		}
		if err := w.repo.ExecuteClaimedAction(ctx, action, w.owner); err != nil {
			slog.Warn("security_audit.action_execute_failed", "action_id", action.ActionID, "error", err)
		}
		timer.Reset(0)
	}
}

func (w *EnforcementWorker) maintenance(ctx context.Context) {
	defer w.wg.Done()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if _, err := w.repo.ReclaimStaleActions(ctx); err != nil {
				slog.Warn("security_audit.action_reclaim_failed", "error", err)
			}
			if err := w.repo.ExpireSecurityAuditRecords(ctx); err != nil {
				slog.Warn("security_audit.retention_maintenance_failed", "error", err)
			}
			if _, err := w.repo.AggregateClosedBehaviorSignalWindows(ctx, time.Now()); err != nil {
				w.repo.RecordBehaviorSignalError(ctx, err)
				slog.Warn("security_audit.signal_aggregation_failed", "error", err)
			} else {
				_, err := runBoundedMaintenanceBatches(
					ctx,
					securityAuditSignalEvaluationBatch,
					securityAuditMaintenanceMaxBatches,
					w.repo.EvaluateBehaviorSignals,
				)
				if err != nil {
					w.repo.RecordBehaviorSignalError(ctx, err)
					slog.Warn("security_audit.signal_evaluation_failed", "error", err)
				}
			}
			if _, err := runBoundedMaintenanceBatches(
				ctx,
				securityAuditShadowDecisionBatch,
				securityAuditMaintenanceMaxBatches,
				w.repo.EvaluateShadowPolicies,
			); err != nil {
				w.repo.RecordShadowEvaluationError(ctx, err)
				slog.Warn("security_audit.shadow_evaluation_failed", "error", err)
			}
			timer.Reset(30 * time.Second)
		}
	}
}

func runBoundedMaintenanceBatches(
	ctx context.Context,
	batchSize int64,
	maxBatches int,
	run func(context.Context) (int64, error),
) (int64, error) {
	if batchSize < 1 || maxBatches < 1 || run == nil {
		return 0, nil
	}
	var total int64
	for batch := 0; batch < maxBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		processed, err := run(ctx)
		if err != nil {
			return total, err
		}
		if processed < 0 {
			return total, fmt.Errorf("security audit maintenance returned a negative batch size")
		}
		total += processed
		if processed < batchSize {
			return total, nil
		}
	}
	return total, nil
}
