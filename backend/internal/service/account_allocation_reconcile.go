package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lib/pq"
)

const accountAllocationReconcileTimeout = 20 * time.Second

// Start starts the background reconciler exactly once. It is intentionally
// separate from request handling: allocation changes remain short database
// transactions and gateway requests never wait for a refill.
func (s *AccountAllocationService) Start() {
	if s == nil || s.db == nil || s.reconcileInterval <= 0 {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runReconcileOnce()

			ticker := time.NewTicker(s.reconcileInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					s.runReconcileOnce()
				case <-s.stopCh:
					return
				}
			}
		}()
	})
}

// Stop waits for the reconciler to stop. It is safe to call for services that
// were not started, which keeps shutdown wiring tolerant of test fixtures.
func (s *AccountAllocationService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *AccountAllocationService) runReconcileOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), accountAllocationReconcileTimeout)
	defer cancel()
	if _, err := s.reconcileBatch(ctx); err != nil {
		slog.Warn("account allocation reconciliation failed", "error", err)
	}
}

// ReconcileAll processes a stable snapshot of every eligible active policy.
// This is the administrator-triggered repair operation; unlike the background
// worker it must not silently stop at policyBatchSize while presenting itself
// as a full reconciliation.
func (s *AccountAllocationService) ReconcileAll(ctx context.Context) ([]AccountAllocationReconcileResult, error) {
	return s.reconcilePolicies(ctx, 0)
}

// reconcileBatch processes one bounded, oldest-first page for the periodic
// worker. Manual-only policies with live leases are included so revoked access
// and unhealthy accounts cannot remain leased forever merely because automatic
// replenishment is disabled. Per-policy row locks make this safe when multiple
// server instances run the same loop.
func (s *AccountAllocationService) reconcileBatch(ctx context.Context) ([]AccountAllocationReconcileResult, error) {
	return s.reconcilePolicies(ctx, s.policyBatchSize)
}

func (s *AccountAllocationService) reconcilePolicies(ctx context.Context, limit int) ([]AccountAllocationReconcileResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("account allocation database is unavailable")
	}

	query := `
		SELECT id
		FROM account_allocation_policies
		WHERE status = $1
			AND deleted_at IS NULL
			AND (
				(auto_replenish = TRUE AND desired_count > 0)
				OR EXISTS (
					SELECT 1
					FROM account_allocation_assignments aa
					WHERE aa.policy_id = account_allocation_policies.id
						AND aa.status = $2
				)
			)
		ORDER BY last_reconciled_at NULLS FIRST, id ASC`
	args := []any{accountAllocationPolicyActive, accountAllocationAssignmentLive}
	if limit > 0 {
		query += ` LIMIT $3`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list account allocation policies for reconciliation: %w", err)
	}
	defer func() { _ = rows.Close() }()

	capacity := limit
	if capacity <= 0 {
		capacity = accountAllocationDefaultBatchSize
	}
	policyIDs := make([]int64, 0, capacity)
	for rows.Next() {
		var policyID int64
		if err := rows.Scan(&policyID); err != nil {
			return nil, fmt.Errorf("scan account allocation policy for reconciliation: %w", err)
		}
		policyIDs = append(policyIDs, policyID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account allocation policies for reconciliation: %w", err)
	}

	results := make([]AccountAllocationReconcileResult, 0, len(policyIDs))
	for _, policyID := range policyIDs {
		result, err := s.ReconcilePolicy(ctx, policyID)
		if errors.Is(err, ErrAccountAllocationPolicyNotFound) {
			continue
		}
		if err != nil {
			slog.Warn("account allocation policy reconciliation failed", "policy_id", policyID, "error", err)
			continue
		}
		results = append(results, result)
	}
	return results, nil
}

// ReconcilePolicy atomically releases unusable assignments and fills the
// remaining desired capacity from unassigned, healthy accounts in the policy
// group. It never automatically removes healthy assignments merely because the
// desired count was reduced; that action stays explicitly administrator-driven.
func (s *AccountAllocationService) ReconcilePolicy(ctx context.Context, policyID int64) (AccountAllocationReconcileResult, error) {
	result := AccountAllocationReconcileResult{
		PolicyID:     policyID,
		AccessStatus: AccountAllocationAccessReady,
	}
	if policyID <= 0 {
		return result, ErrAccountAllocationPolicyNotFound
	}
	if s == nil || s.db == nil {
		return result, fmt.Errorf("account allocation database is unavailable")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin account allocation reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	policy, skipped, err := lockAccountAllocationPolicyForReconcile(ctx, tx, policyID)
	if err != nil {
		return result, err
	}
	if skipped {
		result.SkippedConcurrent = true
		return result, nil
	}
	result.DesiredCount = policy.DesiredCount

	if policy.Status != accountAllocationPolicyActive {
		if err := markAccountAllocationPolicyReconciled(ctx, tx, policy.ID); err != nil {
			return result, err
		}
		if err := tx.Commit(); err != nil {
			return result, fmt.Errorf("commit disabled account allocation reconciliation: %w", err)
		}
		return result, nil
	}
	accessStatus, err := resolveAccountAllocationAccess(ctx, tx, policy.UserID, policy.GroupID)
	if err != nil {
		return result, err
	}
	result.AccessStatus = accessStatus

	activeBefore, err := countActiveAccountAllocationAssignments(ctx, tx, policy.ID)
	if err != nil {
		return result, err
	}
	result.ActiveBefore = activeBefore

	// An allocation lease is useful only while the target user can actually use
	// the policy group. Release inaccessible leases immediately, but retain the
	// active policy so a later subscription renewal or permission grant can be
	// detected and replenished automatically.
	if accessStatus != AccountAllocationAccessReady {
		reason := accountAllocationBlockedReleaseReason(accessStatus)
		assignmentIDs, err := releaseActiveAssignmentsForPolicy(ctx, tx, policy.ID, reason)
		if err != nil {
			return result, err
		}
		for _, assignmentID := range assignmentIDs {
			if err := insertAccountAllocationEvent(ctx, tx, policy.ID, &assignmentID, "assignment_released", nil, map[string]any{
				"reason":        reason,
				"access_status": accessStatus,
			}); err != nil {
				return result, err
			}
		}
		if len(assignmentIDs) > 0 {
			if err := insertAccountAllocationEvent(ctx, tx, policy.ID, nil, "policy_access_blocked", nil, map[string]any{
				"access_status":        accessStatus,
				"released_assignments": len(assignmentIDs),
			}); err != nil {
				return result, err
			}
		}
		result.ReleasedCount = len(assignmentIDs)
		result.ActiveAfter = 0
		result.Shortage = policy.DesiredCount
		if err := markAccountAllocationPolicyReconciled(ctx, tx, policy.ID); err != nil {
			return result, err
		}
		if err := tx.Commit(); err != nil {
			return result, fmt.Errorf("commit blocked account allocation reconciliation: %w", err)
		}
		return result, nil
	}

	released, err := s.releaseUnhealthyAccountAllocationAssignments(ctx, tx, policy)
	if err != nil {
		return result, err
	}
	result.ReleasedCount = released

	activeAfterRelease, err := countActiveAccountAllocationAssignments(ctx, tx, policy.ID)
	if err != nil {
		return result, err
	}

	if policy.AutoReplenish && activeAfterRelease < policy.DesiredCount {
		needed := policy.DesiredCount - activeAfterRelease
		assigned, err := s.fillAccountAllocationAssignments(ctx, tx, policy, needed, "reconciler")
		if err != nil {
			return result, err
		}
		result.AssignedCount = assigned
	}

	activeFinal, err := countActiveAccountAllocationAssignments(ctx, tx, policy.ID)
	if err != nil {
		return result, err
	}
	result.ActiveAfter = activeFinal
	result.Shortage = max(0, policy.DesiredCount-activeFinal)

	if err := markAccountAllocationAssignmentsReconciled(ctx, tx, policy.ID); err != nil {
		return result, err
	}
	if err := markAccountAllocationPolicyReconciled(ctx, tx, policy.ID); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit account allocation reconciliation: %w", err)
	}
	return result, nil
}

func lockAccountAllocationPolicyForReconcile(ctx context.Context, tx *sql.Tx, policyID int64) (accountAllocationPolicyLock, bool, error) {
	policy, err := loadAccountAllocationPolicyForUpdate(ctx, tx, policyID, true)
	if err == nil {
		return policy, false, nil
	}
	if !errors.Is(err, ErrAccountAllocationPolicyNotFound) {
		return accountAllocationPolicyLock{}, false, err
	}

	var exists bool
	if existsErr := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_allocation_policies WHERE id = $1 AND deleted_at IS NULL)`, policyID).Scan(&exists); existsErr != nil {
		return accountAllocationPolicyLock{}, false, fmt.Errorf("check account allocation policy after lock miss: %w", existsErr)
	}
	if exists {
		return accountAllocationPolicyLock{}, true, nil
	}
	return accountAllocationPolicyLock{}, false, ErrAccountAllocationPolicyNotFound
}

type accountAllocationAssignmentHealth struct {
	AssignmentID            int64
	AccountID               int64
	AccountMissing          bool
	AccountRemoved          bool
	InPolicyGroup           bool
	AccountStatus           string
	Schedulable             bool
	ExpiresAt               *time.Time
	AutoPauseOnExpired      bool
	RateLimitResetAt        *time.Time
	TempUnschedulableUntil  *time.Time
	TempUnschedulableReason string
	ErrorMessage            string
}

func (s *AccountAllocationService) releaseUnhealthyAccountAllocationAssignments(ctx context.Context, tx *sql.Tx, policy accountAllocationPolicyLock) (int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			aa.id,
			aa.account_id,
			a.id IS NULL,
			COALESCE(a.deleted_at IS NOT NULL, FALSE),
			EXISTS(SELECT 1 FROM account_groups ag WHERE ag.account_id = aa.account_id AND ag.group_id = aa.group_id),
			COALESCE(a.status, ''),
			COALESCE(a.schedulable, FALSE),
			a.expires_at,
			COALESCE(a.auto_pause_on_expired, FALSE),
			a.rate_limit_reset_at,
			a.temp_unschedulable_until,
			COALESCE(a.temp_unschedulable_reason, ''),
			COALESCE(a.error_message, '')
		FROM account_allocation_assignments aa
		LEFT JOIN accounts a ON a.id = aa.account_id
		WHERE aa.policy_id = $1 AND aa.status = $2
		FOR UPDATE OF aa`, policy.ID, accountAllocationAssignmentLive)
	if err != nil {
		return 0, fmt.Errorf("list account allocation assignment health: %w", err)
	}
	defer func() { _ = rows.Close() }()

	healthRows := make([]accountAllocationAssignmentHealth, 0)
	for rows.Next() {
		var item accountAllocationAssignmentHealth
		var expiresAt, rateLimitResetAt, tempUnschedulableUntil sql.NullTime
		if err := rows.Scan(
			&item.AssignmentID,
			&item.AccountID,
			&item.AccountMissing,
			&item.AccountRemoved,
			&item.InPolicyGroup,
			&item.AccountStatus,
			&item.Schedulable,
			&expiresAt,
			&item.AutoPauseOnExpired,
			&rateLimitResetAt,
			&tempUnschedulableUntil,
			&item.TempUnschedulableReason,
			&item.ErrorMessage,
		); err != nil {
			return 0, fmt.Errorf("scan account allocation assignment health: %w", err)
		}
		if expiresAt.Valid {
			value := expiresAt.Time
			item.ExpiresAt = &value
		}
		if rateLimitResetAt.Valid {
			value := rateLimitResetAt.Time
			item.RateLimitResetAt = &value
		}
		if tempUnschedulableUntil.Valid {
			value := tempUnschedulableUntil.Time
			item.TempUnschedulableUntil = &value
		}
		healthRows = append(healthRows, item)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate account allocation assignment health: %w", err)
	}

	now := time.Now()
	released := 0
	for _, health := range healthRows {
		reason := accountAllocationReleaseReason(policy, health, now)
		if reason == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_allocation_assignments
			SET status = $2, released_at = NOW(), release_reason = $3, updated_at = NOW(), last_reconciled_at = NOW()
			WHERE id = $1 AND status = $4`, health.AssignmentID, accountAllocationAssignmentGone, reason, accountAllocationAssignmentLive); err != nil {
			return 0, fmt.Errorf("release unhealthy account allocation assignment: %w", err)
		}
		assignmentID := health.AssignmentID
		if err := insertAccountAllocationEvent(ctx, tx, policy.ID, &assignmentID, "assignment_released", nil, map[string]any{"reason": reason}); err != nil {
			return 0, err
		}
		released++
	}
	return released, nil
}

func accountAllocationReleaseReason(policy accountAllocationPolicyLock, health accountAllocationAssignmentHealth, now time.Time) string {
	if health.AccountMissing || health.AccountRemoved {
		return "account_removed"
	}
	if !health.InPolicyGroup {
		return "account_group_unbound"
	}
	if health.AutoPauseOnExpired && health.ExpiresAt != nil && !health.ExpiresAt.After(now) {
		return "account_expired"
	}
	if policy.ReplaceOn429 && health.RateLimitResetAt != nil && health.RateLimitResetAt.After(now) {
		return "rate_limited_429"
	}
	isAuthenticationFailure := accountAllocationIs401Failure(health.TempUnschedulableReason) ||
		accountAllocationIs401Failure(health.ErrorMessage)
	if isAuthenticationFailure {
		if policy.ReplaceOn401 {
			return "authentication_failed_401"
		}
		return ""
	}
	if health.AccountStatus != StatusActive || !health.Schedulable {
		return "account_unavailable"
	}
	return ""
}

// accountAllocationIs401Failure reads only the normalized local health marker.
// Structured TempUnschedState is preferred; canonical legacy prefixes preserve
// existing accounts until their upstream handlers migrate fully to structured
// state. Raw upstream response bodies are never inspected or persisted here.
func accountAllocationIs401Failure(reason string) bool {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return false
	}
	var state struct {
		StatusCode int `json:"status_code"`
	}
	if json.Unmarshal([]byte(reason), &state) == nil && state.StatusCode == 401 {
		return true
	}
	lower := strings.ToLower(reason)
	return strings.HasPrefix(lower, "authentication failed (401):") ||
		strings.HasPrefix(lower, "oauth 401:") ||
		strings.HasPrefix(lower, "unauthorized (401):")
}

func (s *AccountAllocationService) fillAccountAllocationAssignments(ctx context.Context, tx *sql.Tx, policy accountAllocationPolicyLock, needed int, source string, excludedAccountIDs ...int64) (int, error) {
	if needed <= 0 {
		return 0, nil
	}

	candidateLimit := needed * 4
	if candidateLimit < needed {
		candidateLimit = needed
	}
	if candidateLimit > 200 {
		candidateLimit = 200
	}
	if excludedAccountIDs == nil {
		excludedAccountIDs = []int64{}
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT a.id, COALESCE(a.type, ''), COALESCE(a.extra, '{}'::jsonb)::text
		FROM accounts a
		JOIN account_groups ag ON ag.account_id = a.id AND ag.group_id = $1
		WHERE a.deleted_at IS NULL
			AND a.status = $2
			AND a.schedulable = TRUE
			AND (a.auto_pause_on_expired = FALSE OR a.expires_at IS NULL OR a.expires_at > NOW())
			AND (a.rate_limit_reset_at IS NULL OR a.rate_limit_reset_at <= NOW())
			AND (a.overload_until IS NULL OR a.overload_until <= NOW())
			AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until <= NOW())
			AND NOT EXISTS (
				SELECT 1 FROM account_allocation_assignments aa
				WHERE aa.account_id = a.id AND aa.status = $3
			)
			AND NOT (a.id = ANY($4::BIGINT[]))
		ORDER BY a.priority ASC, a.id ASC
		LIMIT $5
		FOR UPDATE OF a SKIP LOCKED`,
		policy.GroupID,
		StatusActive,
		accountAllocationAssignmentLive,
		pq.Array(excludedAccountIDs),
		candidateLimit,
	)
	if err != nil {
		return 0, fmt.Errorf("lock account allocation fill candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	accountIDs := make([]int64, 0, needed)
	for rows.Next() {
		var (
			accountID    int64
			accountType  string
			accountExtra []byte
		)
		if err := rows.Scan(&accountID, &accountType, &accountExtra); err != nil {
			return 0, fmt.Errorf("scan account allocation fill candidate: %w", err)
		}
		if accountAllocationQuotaExhausted(accountType, accountExtra) {
			continue
		}
		accountIDs = append(accountIDs, accountID)
		if len(accountIDs) >= needed {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate account allocation fill candidates: %w", err)
	}
	// lib/pq requires every result set on a transaction connection to be fully
	// closed before another statement is issued. We stop after collecting the
	// requested number of candidates, so the deferred close would otherwise run
	// too late: the following INSERT ... RETURNING may observe a stale protocol
	// response (for example, "unexpected Parse response 'C'").
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close account allocation fill candidates: %w", err)
	}

	assigned := 0
	if strings.TrimSpace(source) == "" {
		source = "reconciler"
	}
	for _, accountID := range accountIDs {
		assignmentID, _, err := createAccountAllocationAssignment(ctx, tx, policy, accountID, nil)
		if err != nil {
			if isUniqueViolation(err) {
				continue
			}
			return 0, err
		}
		if err := insertAccountAllocationEvent(ctx, tx, policy.ID, &assignmentID, "assignment_assigned_auto", nil, map[string]any{"source": source}); err != nil {
			return 0, err
		}
		assigned++
	}
	return assigned, nil
}

func countActiveAccountAllocationAssignments(ctx context.Context, tx *sql.Tx, policyID int64) (int, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_allocation_assignments WHERE policy_id = $1 AND status = $2`, policyID, accountAllocationAssignmentLive).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active account allocation assignments: %w", err)
	}
	return count, nil
}

func markAccountAllocationAssignmentsReconciled(ctx context.Context, tx *sql.Tx, policyID int64) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE account_allocation_assignments
		SET last_reconciled_at = NOW(), updated_at = NOW()
		WHERE policy_id = $1 AND status = $2`, policyID, accountAllocationAssignmentLive); err != nil {
		return fmt.Errorf("mark account allocation assignments reconciled: %w", err)
	}
	return nil
}

func markAccountAllocationPolicyReconciled(ctx context.Context, tx *sql.Tx, policyID int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE account_allocation_policies SET last_reconciled_at = NOW(), updated_at = NOW() WHERE id = $1`, policyID); err != nil {
		return fmt.Errorf("mark account allocation policy reconciled: %w", err)
	}
	return nil
}
