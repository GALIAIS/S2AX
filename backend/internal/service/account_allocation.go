package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
)

const (
	accountAllocationPolicyActive   = "active"
	accountAllocationPolicyDisabled = "disabled"
	accountAllocationAssignmentLive = "active"
	accountAllocationAssignmentGone = "released"

	accountAllocationMaxFallbackDesiredCount = 50
	accountAllocationDefaultBatchSize        = 100
	accountAllocationDefaultInterval         = 15 * time.Second
)

var (
	ErrAccountAllocationPolicyNotFound     = infraerrors.NotFound("ACCOUNT_ALLOCATION_POLICY_NOT_FOUND", "account allocation policy not found")
	ErrAccountAllocationAssignmentNotFound = infraerrors.NotFound("ACCOUNT_ALLOCATION_ASSIGNMENT_NOT_FOUND", "account allocation assignment not found")
	ErrAccountAllocationPolicyConflict     = infraerrors.Conflict("ACCOUNT_ALLOCATION_POLICY_EXISTS", "an allocation policy already exists for this user and group")
	ErrAccountAllocationAccountUnavailable = infraerrors.Conflict("ACCOUNT_ALLOCATION_ACCOUNT_UNAVAILABLE", "account is not an available member of the selected group")
	ErrAccountAllocationPolicyDisabled     = infraerrors.Conflict("ACCOUNT_ALLOCATION_POLICY_DISABLED", "account allocation policy is disabled")
)

// AccountAllocationService owns the durable account lease policy. It is a
// separate control-plane layer: group membership defines candidate eligibility,
// while this service makes a candidate exclusive to a user when leased.
type AccountAllocationService struct {
	db                *sql.DB
	reconcileInterval time.Duration
	policyBatchSize   int
	maxDesiredCount   int
	stopCh            chan struct{}
	startOnce         sync.Once
	stopOnce          sync.Once
	wg                sync.WaitGroup
}

type AccountAllocationPolicy struct {
	ID                    int64      `json:"id"`
	UserID                int64      `json:"user_id"`
	UserEmail             string     `json:"user_email"`
	Username              string     `json:"username"`
	GroupID               int64      `json:"group_id"`
	GroupName             string     `json:"group_name"`
	GroupPlatform         string     `json:"group_platform"`
	DesiredCount          int        `json:"desired_count"`
	AutoReplenish         bool       `json:"auto_replenish"`
	ReplaceOn401          bool       `json:"replace_on_401"`
	ReplaceOn429          bool       `json:"replace_on_429"`
	Status                string     `json:"status"`
	CreatedBy             *int64     `json:"created_by,omitempty"`
	LastReconciledAt      *time.Time `json:"last_reconciled_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	ActiveAssignmentCount int        `json:"active_assignment_count"`
	Shortage              int        `json:"shortage"`
}

type AccountAllocationPolicyInput struct {
	UserID        int64
	GroupID       int64
	DesiredCount  int
	AutoReplenish bool
	ReplaceOn401  bool
	ReplaceOn429  bool
	ActorUserID   int64
}

type AccountAllocationPolicyUpdate struct {
	DesiredCount  int
	AutoReplenish bool
	ReplaceOn401  bool
	ReplaceOn429  bool
	ActorUserID   int64
}

type AccountAllocationPolicyFilter struct {
	UserID  *int64
	GroupID *int64
	Status  string
}

type AccountAllocationAssignment struct {
	ID               int64      `json:"id"`
	PolicyID         int64      `json:"policy_id"`
	UserID           int64      `json:"user_id"`
	GroupID          int64      `json:"group_id"`
	AccountID        int64      `json:"account_id"`
	AccountName      string     `json:"account_name"`
	Platform         string     `json:"platform"`
	AccountType      string     `json:"account_type"`
	Concurrency      int        `json:"concurrency"`
	AccountStatus    string     `json:"account_status"`
	Schedulable      bool       `json:"schedulable"`
	RateLimitResetAt *time.Time `json:"rate_limit_reset_at,omitempty"`
	Status           string     `json:"status"`
	AssignedBy       *int64     `json:"assigned_by,omitempty"`
	AssignedAt       time.Time  `json:"assigned_at"`
	ReleasedAt       *time.Time `json:"released_at,omitempty"`
	ReleaseReason    string     `json:"release_reason,omitempty"`
	LastReconciledAt *time.Time `json:"last_reconciled_at,omitempty"`
}

// AccountAllocationCandidate is the administrator-only projection used when a
// policy needs a deliberate manual assignment. It is intentionally separate
// from the user-facing allocation DTO: administrators can identify an account,
// while users never receive account identifiers or names.
type AccountAllocationCandidate struct {
	AccountID   int64  `json:"account_id"`
	AccountName string `json:"account_name"`
	Platform    string `json:"platform"`
	AccountType string `json:"account_type"`
	Concurrency int    `json:"concurrency"`
	Priority    int    `json:"priority"`
}

// AccountAllocationUserAssignment is deliberately a whitelist view. It does
// not contain account identifiers, names, credentials, proxies, IPs, notes,
// raw health errors, models, or global account usage.
type AccountAllocationUserAssignment struct {
	AssignmentID int64  `json:"assignment_id"`
	PolicyID     int64  `json:"policy_id"`
	GroupID      int64  `json:"group_id"`
	GroupName    string `json:"group_name"`
	Platform     string `json:"platform"`
	AccountType  string `json:"account_type"`
	Capacity     struct {
		Concurrency int `json:"concurrency"`
	} `json:"capacity"`
	Status           string     `json:"status"`
	RateLimitResetAt *time.Time `json:"rate_limit_reset_at,omitempty"`
	Usage            struct {
		RequestCount int64 `json:"request_count"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
	AssignedAt time.Time `json:"assigned_at"`
}

type AccountAllocationEvent struct {
	ID           int64          `json:"id"`
	PolicyID     int64          `json:"policy_id"`
	AssignmentID *int64         `json:"assignment_id,omitempty"`
	EventType    string         `json:"event_type"`
	ActorUserID  *int64         `json:"actor_user_id,omitempty"`
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    time.Time      `json:"created_at"`
}

type AccountAllocationReconcileResult struct {
	PolicyID          int64 `json:"policy_id"`
	DesiredCount      int   `json:"desired_count"`
	ActiveBefore      int   `json:"active_before"`
	ActiveAfter       int   `json:"active_after"`
	ReleasedCount     int   `json:"released_count"`
	AssignedCount     int   `json:"assigned_count"`
	Shortage          int   `json:"shortage"`
	SkippedConcurrent bool  `json:"skipped_concurrent"`
}

// AccountAllocationCapabilities exposes deployment limits to the administrator
// UI. Keeping this server-provided avoids a stale frontend max value when an
// operator tightens the allocation configuration.
type AccountAllocationCapabilities struct {
	MaxDesiredCount          int `json:"max_desired_count"`
	ReconcileIntervalSeconds int `json:"reconcile_interval_seconds"`
}

func NewAccountAllocationService(db *sql.DB, cfg *config.Config) *AccountAllocationService {
	interval := accountAllocationDefaultInterval
	batchSize := accountAllocationDefaultBatchSize
	maxDesired := accountAllocationMaxFallbackDesiredCount
	if cfg != nil {
		if cfg.AccountAllocation.ReconcileIntervalSeconds > 0 {
			interval = time.Duration(cfg.AccountAllocation.ReconcileIntervalSeconds) * time.Second
		}
		if cfg.AccountAllocation.PolicyBatchSize > 0 {
			batchSize = cfg.AccountAllocation.PolicyBatchSize
		}
		if cfg.AccountAllocation.MaxDesiredCount > 0 {
			maxDesired = cfg.AccountAllocation.MaxDesiredCount
		}
	}
	return &AccountAllocationService{
		db:                db,
		reconcileInterval: interval,
		policyBatchSize:   batchSize,
		maxDesiredCount:   maxDesired,
		stopCh:            make(chan struct{}),
	}
}

func ProvideAccountAllocationService(db *sql.DB, cfg *config.Config) *AccountAllocationService {
	svc := NewAccountAllocationService(db, cfg)
	svc.Start()
	return svc
}

func (s *AccountAllocationService) Capabilities() AccountAllocationCapabilities {
	if s == nil {
		return AccountAllocationCapabilities{
			MaxDesiredCount:          accountAllocationMaxFallbackDesiredCount,
			ReconcileIntervalSeconds: int(accountAllocationDefaultInterval.Seconds()),
		}
	}
	return AccountAllocationCapabilities{
		MaxDesiredCount:          s.maxDesiredCount,
		ReconcileIntervalSeconds: int(s.reconcileInterval.Seconds()),
	}
}

func (s *AccountAllocationService) CreatePolicy(ctx context.Context, input AccountAllocationPolicyInput) (*AccountAllocationPolicy, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("account allocation database is unavailable")
	}
	if err := s.validatePolicyInput(input); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin account allocation policy transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Validate inside the write transaction. Checking before BEGIN leaves a
	// race where a user or group can be soft-deleted after validation but before
	// the policy row is created; its deletion trigger has already run by then
	// and cannot disable the newly inserted policy.
	if err := ensureActiveAccountAllocationReferences(ctx, tx, input.UserID, input.GroupID); err != nil {
		return nil, err
	}

	actorID := nullablePositiveID(input.ActorUserID)
	var policyID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO account_allocation_policies
			(user_id, group_id, desired_count, auto_replenish, replace_on_401, replace_on_429, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		input.UserID, input.GroupID, input.DesiredCount, input.AutoReplenish, input.ReplaceOn401, input.ReplaceOn429,
		accountAllocationPolicyActive, actorID,
	).Scan(&policyID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAccountAllocationPolicyConflict
		}
		return nil, fmt.Errorf("create account allocation policy: %w", err)
	}
	if err := insertAccountAllocationEvent(ctx, tx, policyID, nil, "policy_created", actorID, map[string]any{
		"desired_count": input.DesiredCount, "auto_replenish": input.AutoReplenish,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit account allocation policy: %w", err)
	}

	if input.AutoReplenish && input.DesiredCount > 0 {
		// A background worker will retry if this first reconciliation races with a
		// concurrent admin action. The policy remains visible with a shortage.
		_, _ = s.ReconcilePolicy(ctx, policyID)
	}
	return s.GetPolicy(ctx, policyID)
}

func (s *AccountAllocationService) UpdatePolicy(ctx context.Context, policyID int64, input AccountAllocationPolicyUpdate) (*AccountAllocationPolicy, error) {
	if policyID <= 0 {
		return nil, ErrAccountAllocationPolicyNotFound
	}
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("account allocation database is unavailable")
	}
	if input.DesiredCount < 0 || input.DesiredCount > s.maxDesiredCount {
		return nil, infraerrors.BadRequest("ACCOUNT_ALLOCATION_DESIRED_COUNT_INVALID", fmt.Sprintf("desired_count must be between 0 and %d", s.maxDesiredCount))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin account allocation policy update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := loadAccountAllocationPolicyForUpdate(ctx, tx, policyID, false); err != nil {
		if errors.Is(err, ErrAccountAllocationPolicyNotFound) {
			return nil, ErrAccountAllocationPolicyNotFound
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE account_allocation_policies
		SET desired_count = $2, auto_replenish = $3, replace_on_401 = $4, replace_on_429 = $5, updated_at = NOW()
		WHERE id = $1`, policyID, input.DesiredCount, input.AutoReplenish, input.ReplaceOn401, input.ReplaceOn429); err != nil {
		return nil, fmt.Errorf("update account allocation policy: %w", err)
	}
	actorID := nullablePositiveID(input.ActorUserID)
	if err := insertAccountAllocationEvent(ctx, tx, policyID, nil, "policy_updated", actorID, map[string]any{
		"desired_count": input.DesiredCount, "auto_replenish": input.AutoReplenish,
		"replace_on_401": input.ReplaceOn401, "replace_on_429": input.ReplaceOn429,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit account allocation policy update: %w", err)
	}
	if input.AutoReplenish && input.DesiredCount > 0 {
		_, _ = s.ReconcilePolicy(ctx, policyID)
	}
	return s.GetPolicy(ctx, policyID)
}

func (s *AccountAllocationService) SetPolicyStatus(ctx context.Context, policyID int64, enabled bool, actorUserID int64) (*AccountAllocationPolicy, error) {
	if policyID <= 0 {
		return nil, ErrAccountAllocationPolicyNotFound
	}
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("account allocation database is unavailable")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin account allocation policy status transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	locked, err := loadAccountAllocationPolicyForUpdate(ctx, tx, policyID, false)
	if err != nil {
		return nil, err
	}
	// A policy can remain in the audit history after its user or group is soft
	// deleted. Never allow an administrator to re-enable that historical policy
	// and silently restore scheduling against an inactive reference.
	if enabled {
		if err := ensureActiveAccountAllocationReferences(ctx, tx, locked.UserID, locked.GroupID); err != nil {
			return nil, err
		}
	}
	actorID := nullablePositiveID(actorUserID)
	status := accountAllocationPolicyDisabled
	eventType := "policy_disabled"
	if enabled {
		status = accountAllocationPolicyActive
		eventType = "policy_enabled"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE account_allocation_policies SET status = $2, updated_at = NOW() WHERE id = $1`, policyID, status); err != nil {
		return nil, fmt.Errorf("set account allocation policy status: %w", err)
	}
	if !enabled {
		assignmentIDs, err := releaseActiveAssignmentsForPolicy(ctx, tx, policyID, "policy_disabled")
		if err != nil {
			return nil, err
		}
		for _, assignmentID := range assignmentIDs {
			if err := insertAccountAllocationEvent(ctx, tx, policyID, &assignmentID, "assignment_released", actorID, map[string]any{"reason": "policy_disabled"}); err != nil {
				return nil, err
			}
		}
	}
	if err := insertAccountAllocationEvent(ctx, tx, policyID, nil, eventType, actorID, map[string]any{"desired_count": locked.DesiredCount}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit account allocation policy status: %w", err)
	}
	if enabled && locked.AutoReplenish && locked.DesiredCount > 0 {
		_, _ = s.ReconcilePolicy(ctx, policyID)
	}
	return s.GetPolicy(ctx, policyID)
}

// DeletePolicy releases live leases and hides the policy from normal control
// plane queries. Assignment and event history remains immutable for audit.
func (s *AccountAllocationService) DeletePolicy(ctx context.Context, policyID, actorUserID int64) error {
	if policyID <= 0 {
		return ErrAccountAllocationPolicyNotFound
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("account allocation database is unavailable")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin account allocation policy deletion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := loadAccountAllocationPolicyForUpdate(ctx, tx, policyID, false); err != nil {
		return err
	}
	actorID := nullablePositiveID(actorUserID)
	assignmentIDs, err := releaseActiveAssignmentsForPolicy(ctx, tx, policyID, "policy_deleted")
	if err != nil {
		return err
	}
	for _, assignmentID := range assignmentIDs {
		if err := insertAccountAllocationEvent(ctx, tx, policyID, &assignmentID, "assignment_released", actorID, map[string]any{"reason": "policy_deleted"}); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE account_allocation_policies
		SET status = $2, deleted_at = NOW(), last_reconciled_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, policyID, accountAllocationPolicyDisabled); err != nil {
		return fmt.Errorf("delete account allocation policy: %w", err)
	}
	if err := insertAccountAllocationEvent(ctx, tx, policyID, nil, "policy_deleted", actorID, map[string]any{"released_assignments": len(assignmentIDs)}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account allocation policy deletion: %w", err)
	}
	return nil
}

func (s *AccountAllocationService) GetPolicy(ctx context.Context, policyID int64) (*AccountAllocationPolicy, error) {
	if policyID <= 0 || s == nil || s.db == nil {
		return nil, ErrAccountAllocationPolicyNotFound
	}
	row := s.db.QueryRowContext(ctx, accountAllocationPolicySelect+`
		WHERE p.id = $1 AND p.deleted_at IS NULL
		GROUP BY p.id, u.id, g.id`, policyID)
	policy, err := scanAccountAllocationPolicy(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAccountAllocationPolicyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get account allocation policy: %w", err)
	}
	return policy, nil
}

func (s *AccountAllocationService) ListPolicies(ctx context.Context, filter AccountAllocationPolicyFilter, page, pageSize int) ([]AccountAllocationPolicy, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("account allocation database is unavailable")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	clauses := []string{"p.deleted_at IS NULL"}
	args := make([]any, 0, 3)
	if filter.UserID != nil && *filter.UserID > 0 {
		args = append(args, *filter.UserID)
		clauses = append(clauses, fmt.Sprintf("p.user_id = $%d", len(args)))
	}
	if filter.GroupID != nil && *filter.GroupID > 0 {
		args = append(args, *filter.GroupID)
		clauses = append(clauses, fmt.Sprintf("p.group_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		if status != accountAllocationPolicyActive && status != accountAllocationPolicyDisabled {
			return nil, 0, infraerrors.BadRequest("ACCOUNT_ALLOCATION_STATUS_INVALID", "status must be active or disabled")
		}
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("p.status = $%d", len(args)))
	}
	where := strings.Join(clauses, " AND ")
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_allocation_policies p WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count account allocation policies: %w", err)
	}

	args = append(args, pageSize, (page-1)*pageSize)
	query := accountAllocationPolicySelect + `
		WHERE ` + where + `
		GROUP BY p.id, u.id, g.id
		ORDER BY p.updated_at DESC, p.id DESC
		LIMIT $` + fmt.Sprint(len(args)-1) + ` OFFSET $` + fmt.Sprint(len(args))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list account allocation policies: %w", err)
	}
	defer rows.Close()
	items := make([]AccountAllocationPolicy, 0)
	for rows.Next() {
		policy, err := scanAccountAllocationPolicy(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan account allocation policy: %w", err)
		}
		items = append(items, *policy)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate account allocation policies: %w", err)
	}
	return items, total, nil
}

const accountAllocationPolicySelect = `
	SELECT
		p.id, p.user_id, u.email, COALESCE(u.username, ''), p.group_id, g.name, COALESCE(g.platform, ''),
		p.desired_count, p.auto_replenish, p.replace_on_401, p.replace_on_429, p.status,
		p.created_by, p.last_reconciled_at, p.created_at, p.updated_at,
		COUNT(aa.id) FILTER (WHERE aa.status = 'active')
	FROM account_allocation_policies p
	JOIN users u ON u.id = p.user_id
	JOIN groups g ON g.id = p.group_id
	LEFT JOIN account_allocation_assignments aa ON aa.policy_id = p.id`

type accountAllocationSQLScanner interface {
	Scan(dest ...any) error
}

func scanAccountAllocationPolicy(scanner accountAllocationSQLScanner) (*AccountAllocationPolicy, error) {
	var policy AccountAllocationPolicy
	var createdBy sql.NullInt64
	var lastReconciledAt sql.NullTime
	if err := scanner.Scan(
		&policy.ID, &policy.UserID, &policy.UserEmail, &policy.Username, &policy.GroupID, &policy.GroupName, &policy.GroupPlatform,
		&policy.DesiredCount, &policy.AutoReplenish, &policy.ReplaceOn401, &policy.ReplaceOn429, &policy.Status,
		&createdBy, &lastReconciledAt, &policy.CreatedAt, &policy.UpdatedAt, &policy.ActiveAssignmentCount,
	); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		value := createdBy.Int64
		policy.CreatedBy = &value
	}
	if lastReconciledAt.Valid {
		value := lastReconciledAt.Time
		policy.LastReconciledAt = &value
	}
	policy.Shortage = max(0, policy.DesiredCount-policy.ActiveAssignmentCount)
	return &policy, nil
}

func (s *AccountAllocationService) AssignManual(ctx context.Context, policyID, accountID, actorUserID int64) (*AccountAllocationAssignment, error) {
	if policyID <= 0 || accountID <= 0 {
		return nil, ErrAccountAllocationAccountUnavailable
	}
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("account allocation database is unavailable")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin manual account allocation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	policy, err := loadAccountAllocationPolicyForUpdate(ctx, tx, policyID, false)
	if err != nil {
		return nil, err
	}
	if policy.Status != accountAllocationPolicyActive {
		return nil, ErrAccountAllocationPolicyDisabled
	}
	if err := ensureActiveAccountAllocationReferences(ctx, tx, policy.UserID, policy.GroupID); err != nil {
		return nil, err
	}

	var (
		selectedID   int64
		accountType  string
		accountExtra []byte
	)
	err = tx.QueryRowContext(ctx, `
		SELECT a.id, COALESCE(a.type, ''), COALESCE(a.extra, '{}'::jsonb)::text
		FROM accounts a
		JOIN account_groups ag ON ag.account_id = a.id AND ag.group_id = $2
		WHERE a.id = $1
			AND a.deleted_at IS NULL
			AND a.status = 'active'
			AND a.schedulable = TRUE
			AND (a.auto_pause_on_expired = FALSE OR a.expires_at IS NULL OR a.expires_at > NOW())
			AND (a.rate_limit_reset_at IS NULL OR a.rate_limit_reset_at <= NOW())
			AND (a.overload_until IS NULL OR a.overload_until <= NOW())
			AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until <= NOW())
			AND NOT EXISTS (
				SELECT 1 FROM account_allocation_assignments aa
				WHERE aa.account_id = a.id AND aa.status = 'active'
			)
		FOR UPDATE OF a SKIP LOCKED`, accountID, policy.GroupID).Scan(&selectedID, &accountType, &accountExtra)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAccountAllocationAccountUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("lock manual account allocation candidate: %w", err)
	}
	if accountAllocationQuotaExhausted(accountType, accountExtra) {
		return nil, ErrAccountAllocationAccountUnavailable
	}

	assignmentID, _, err := createAccountAllocationAssignment(ctx, tx, policy, selectedID, nullablePositiveID(actorUserID))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAccountAllocationAccountUnavailable
		}
		return nil, err
	}
	if err := insertAccountAllocationEvent(ctx, tx, policy.ID, &assignmentID, "assignment_assigned_manual", nullablePositiveID(actorUserID), nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit manual account allocation: %w", err)
	}
	return s.GetAssignment(ctx, assignmentID)
}

func (s *AccountAllocationService) ReleaseAssignment(ctx context.Context, policyID, assignmentID, actorUserID int64) error {
	if policyID <= 0 || assignmentID <= 0 {
		return ErrAccountAllocationAssignmentNotFound
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("account allocation database is unavailable")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin account allocation release: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := loadAccountAllocationPolicyForUpdate(ctx, tx, policyID, false); err != nil {
		return err
	}
	var found bool
	if err := tx.QueryRowContext(ctx, `
		UPDATE account_allocation_assignments
		SET status = $3, released_at = NOW(), release_reason = $4, updated_at = NOW()
		WHERE id = $1 AND policy_id = $2 AND status = $5
		RETURNING TRUE`, assignmentID, policyID, accountAllocationAssignmentGone, "manual_release", accountAllocationAssignmentLive).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return ErrAccountAllocationAssignmentNotFound
	} else if err != nil {
		return fmt.Errorf("release account allocation assignment: %w", err)
	}
	if err := insertAccountAllocationEvent(ctx, tx, policyID, &assignmentID, "assignment_released", nullablePositiveID(actorUserID), map[string]any{"reason": "manual_release"}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account allocation release: %w", err)
	}
	policy, err := s.GetPolicy(ctx, policyID)
	if err == nil && policy.Status == accountAllocationPolicyActive && policy.AutoReplenish && policy.DesiredCount > 0 {
		_, _ = s.ReconcilePolicy(ctx, policyID)
	}
	return nil
}

func (s *AccountAllocationService) GetAssignment(ctx context.Context, assignmentID int64) (*AccountAllocationAssignment, error) {
	if assignmentID <= 0 || s == nil || s.db == nil {
		return nil, ErrAccountAllocationAssignmentNotFound
	}
	row := s.db.QueryRowContext(ctx, accountAllocationAssignmentSelect+` WHERE aa.id = $1`, assignmentID)
	assignment, err := scanAccountAllocationAssignment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAccountAllocationAssignmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get account allocation assignment: %w", err)
	}
	return assignment, nil
}

func (s *AccountAllocationService) ListAssignments(ctx context.Context, policyID int64) ([]AccountAllocationAssignment, error) {
	if policyID <= 0 || s == nil || s.db == nil {
		return nil, ErrAccountAllocationPolicyNotFound
	}
	if _, err := s.GetPolicy(ctx, policyID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, accountAllocationAssignmentSelect+` WHERE aa.policy_id = $1 ORDER BY aa.status ASC, aa.assigned_at DESC, aa.id DESC`, policyID)
	if err != nil {
		return nil, fmt.Errorf("list account allocation assignments: %w", err)
	}
	defer rows.Close()
	items := make([]AccountAllocationAssignment, 0)
	for rows.Next() {
		assignment, err := scanAccountAllocationAssignment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account allocation assignments: %w", err)
	}
	return items, nil
}

const accountAllocationAssignmentSelect = `
	SELECT aa.id, aa.policy_id, aa.user_id, aa.group_id,
		CASE WHEN a.id IS NOT NULL AND a.deleted_at IS NULL THEN aa.account_id ELSE 0 END,
		COALESCE(CASE WHEN a.deleted_at IS NULL THEN a.name END, ''),
		COALESCE(CASE WHEN a.deleted_at IS NULL THEN a.platform END, ''),
		COALESCE(CASE WHEN a.deleted_at IS NULL THEN a.type END, ''),
		COALESCE(CASE WHEN a.deleted_at IS NULL THEN a.concurrency END, 0),
		COALESCE(CASE WHEN a.deleted_at IS NULL THEN a.status END, 'removed'),
		COALESCE(CASE WHEN a.deleted_at IS NULL THEN a.schedulable END, FALSE),
		CASE WHEN a.deleted_at IS NULL THEN a.rate_limit_reset_at ELSE NULL END,
		aa.status, aa.assigned_by, aa.assigned_at, aa.released_at, COALESCE(aa.release_reason, ''), aa.last_reconciled_at
	FROM account_allocation_assignments aa
	LEFT JOIN accounts a ON a.id = aa.account_id`

func scanAccountAllocationAssignment(scanner accountAllocationSQLScanner) (*AccountAllocationAssignment, error) {
	var assignment AccountAllocationAssignment
	var rateLimitResetAt, releasedAt, lastReconciledAt sql.NullTime
	var accountID, assignedBy sql.NullInt64
	if err := scanner.Scan(
		&assignment.ID, &assignment.PolicyID, &assignment.UserID, &assignment.GroupID, &accountID,
		&assignment.AccountName, &assignment.Platform, &assignment.AccountType, &assignment.Concurrency,
		&assignment.AccountStatus, &assignment.Schedulable, &rateLimitResetAt,
		&assignment.Status, &assignedBy, &assignment.AssignedAt, &releasedAt, &assignment.ReleaseReason, &lastReconciledAt,
	); err != nil {
		return nil, err
	}
	if assignedBy.Valid {
		value := assignedBy.Int64
		assignment.AssignedBy = &value
	}
	if accountID.Valid {
		assignment.AccountID = accountID.Int64
	}
	if rateLimitResetAt.Valid {
		value := rateLimitResetAt.Time
		assignment.RateLimitResetAt = &value
	}
	if releasedAt.Valid {
		value := releasedAt.Time
		assignment.ReleasedAt = &value
	}
	if lastReconciledAt.Valid {
		value := lastReconciledAt.Time
		assignment.LastReconciledAt = &value
	}
	return &assignment, nil
}

func (s *AccountAllocationService) ListEvents(ctx context.Context, policyID int64, page, pageSize int) ([]AccountAllocationEvent, int64, error) {
	if policyID <= 0 || s == nil || s.db == nil {
		return nil, 0, ErrAccountAllocationPolicyNotFound
	}
	if _, err := s.GetPolicy(ctx, policyID); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_allocation_events WHERE policy_id = $1`, policyID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count account allocation events: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, policy_id, assignment_id, event_type, actor_user_id, metadata, created_at
		FROM account_allocation_events
		WHERE policy_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, policyID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list account allocation events: %w", err)
	}
	defer rows.Close()
	items := make([]AccountAllocationEvent, 0)
	for rows.Next() {
		var item AccountAllocationEvent
		var assignmentID, actorUserID sql.NullInt64
		var metadata []byte
		if err := rows.Scan(&item.ID, &item.PolicyID, &assignmentID, &item.EventType, &actorUserID, &metadata, &item.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan account allocation event: %w", err)
		}
		if assignmentID.Valid {
			value := assignmentID.Int64
			item.AssignmentID = &value
		}
		if actorUserID.Valid {
			value := actorUserID.Int64
			item.ActorUserID = &value
		}
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &item.Metadata)
		}
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate account allocation events: %w", err)
	}
	return items, total, nil
}

func (s *AccountAllocationService) ListUserAssignments(ctx context.Context, userID int64) ([]AccountAllocationUserAssignment, error) {
	if userID <= 0 || s == nil || s.db == nil {
		return []AccountAllocationUserAssignment{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			aa.id, aa.policy_id, p.group_id, g.name, COALESCE(g.platform, ''), COALESCE(a.type, ''),
			COALESCE(a.concurrency, 0), COALESCE(a.status, 'removed'), COALESCE(a.schedulable, FALSE),
			a.rate_limit_reset_at, aa.assigned_at,
			COUNT(ul.id), COALESCE(SUM(
				ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens
			), 0)
		FROM account_allocation_assignments aa
		JOIN account_allocation_policies p ON p.id = aa.policy_id
		JOIN groups g ON g.id = p.group_id
		LEFT JOIN accounts a ON a.id = aa.account_id
		-- Only show the current user's usage inside this lease window. Without
		-- the time bound, a reassigned account could expose the user's own older
		-- shared-pool usage as if it belonged to the new allocation.
		LEFT JOIN usage_logs ul ON ul.account_id = aa.account_id
			AND ul.user_id = p.user_id
			AND ul.created_at >= aa.assigned_at
			AND (aa.released_at IS NULL OR ul.created_at < aa.released_at)
		WHERE p.user_id = $1 AND p.status = 'active' AND aa.status = 'active'
		GROUP BY aa.id, aa.policy_id, p.group_id, g.id, a.id
		ORDER BY g.name ASC, aa.assigned_at ASC, aa.id ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user account allocations: %w", err)
	}
	defer rows.Close()
	items := make([]AccountAllocationUserAssignment, 0)
	for rows.Next() {
		var item AccountAllocationUserAssignment
		var accountStatus string
		var schedulable bool
		var rateLimitResetAt sql.NullTime
		if err := rows.Scan(
			&item.AssignmentID, &item.PolicyID, &item.GroupID, &item.GroupName, &item.Platform, &item.AccountType,
			&item.Capacity.Concurrency, &accountStatus, &schedulable, &rateLimitResetAt, &item.AssignedAt,
			&item.Usage.RequestCount, &item.Usage.TotalTokens,
		); err != nil {
			return nil, fmt.Errorf("scan user account allocation: %w", err)
		}
		if rateLimitResetAt.Valid {
			value := rateLimitResetAt.Time
			item.RateLimitResetAt = &value
		}
		item.Status = accountAllocationDisplayStatus(accountStatus, schedulable, item.RateLimitResetAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user account allocations: %w", err)
	}
	return items, nil
}

func accountAllocationDisplayStatus(accountStatus string, schedulable bool, rateLimitResetAt *time.Time) string {
	if rateLimitResetAt != nil && rateLimitResetAt.After(time.Now()) {
		return "cooling"
	}
	if accountStatus != StatusActive || !schedulable {
		return "unavailable"
	}
	return "ready"
}

// ListManualCandidates returns healthy, currently unleased accounts from the
// policy's group. It is the only supported source for the manual-assignment
// picker, which keeps the UI from racing a raw account list or exposing a
// misleading already-leased candidate.
func (s *AccountAllocationService) ListManualCandidates(ctx context.Context, policyID int64, query string, limit int) ([]AccountAllocationCandidate, error) {
	if policyID <= 0 || s == nil || s.db == nil {
		return nil, ErrAccountAllocationPolicyNotFound
	}
	policy, err := s.GetPolicy(ctx, policyID)
	if err != nil {
		return nil, err
	}
	if policy.Status != accountAllocationPolicyActive {
		return nil, ErrAccountAllocationPolicyDisabled
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query = strings.TrimSpace(query)
	if len([]rune(query)) > 100 {
		query = string([]rune(query)[:100])
	}
	candidateLimit := limit * 4
	if candidateLimit < limit {
		candidateLimit = limit
	}
	if candidateLimit > 200 {
		candidateLimit = 200
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.name, COALESCE(a.platform, ''), COALESCE(a.type, ''),
			COALESCE(a.concurrency, 0), COALESCE(a.priority, 0), COALESCE(a.extra, '{}'::jsonb)::text
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
			AND ($4 = '' OR a.name ILIKE '%' || $4 || '%' OR a.platform ILIKE '%' || $4 || '%' OR a.type ILIKE '%' || $4 || '%')
		ORDER BY a.priority ASC, a.id ASC
		LIMIT $5`, policy.GroupID, StatusActive, accountAllocationAssignmentLive, query, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("list account allocation manual candidates: %w", err)
	}
	defer rows.Close()

	items := make([]AccountAllocationCandidate, 0)
	for rows.Next() {
		var item AccountAllocationCandidate
		var rawExtra []byte
		if err := rows.Scan(&item.AccountID, &item.AccountName, &item.Platform, &item.AccountType, &item.Concurrency, &item.Priority, &rawExtra); err != nil {
			return nil, fmt.Errorf("scan account allocation manual candidate: %w", err)
		}
		if accountAllocationQuotaExhausted(item.AccountType, rawExtra) {
			continue
		}
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account allocation manual candidates: %w", err)
	}
	return items, nil
}

// accountAllocationQuotaExhausted keeps manual and automatic leasing aligned
// with the normal scheduler's quota rule. The JSON comes from a PostgreSQL
// jsonb column; malformed legacy data is left to the normal scheduler's final
// health validation rather than being guessed as exhausted here.
func accountAllocationQuotaExhausted(accountType string, rawExtra []byte) bool {
	projection := Account{Type: accountType, Extra: map[string]any{}}
	if len(rawExtra) > 0 && json.Unmarshal(rawExtra, &projection.Extra) != nil {
		return false
	}
	return projection.IsAPIKeyOrBedrock() && projection.IsQuotaExceeded()
}

// FilterCandidates is called by gateway schedulers after they have applied
// normal account health/model rules. It is intentionally fail-closed: an error
// means the caller must not fall back to a shared account for a managed user.
func (s *AccountAllocationService) FilterCandidates(ctx context.Context, userID int64, groupID *int64, candidates []Account) ([]Account, error) {
	if s == nil || s.db == nil || len(candidates) == 0 {
		return candidates, nil
	}
	managed := false
	if userID > 0 && groupID != nil && *groupID > 0 {
		var err error
		managed, err = s.hasActivePolicy(ctx, userID, *groupID)
		if err != nil {
			return nil, err
		}
	}
	ids := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.ID > 0 {
			ids = append(ids, candidate.ID)
		}
	}
	owners, err := s.activeAssignmentOwners(ctx, ids)
	if err != nil {
		return nil, err
	}
	filtered := make([]Account, 0, len(candidates))
	for _, candidate := range candidates {
		owner, allocated := owners[candidate.ID]
		if managed {
			if allocated && owner.UserID == userID && owner.GroupID == *groupID {
				filtered = append(filtered, candidate)
			}
			continue
		}
		if !allocated {
			filtered = append(filtered, candidate)
		}
	}
	return filtered, nil
}

// CanUseAccount is the sticky-session guard. It complements candidate filtering
// so an old session cannot keep an account after its lease changes owner.
func (s *AccountAllocationService) CanUseAccount(ctx context.Context, userID int64, groupID *int64, accountID int64) (bool, error) {
	if s == nil || s.db == nil || accountID <= 0 {
		return true, nil
	}
	managed := false
	if userID > 0 && groupID != nil && *groupID > 0 {
		var err error
		managed, err = s.hasActivePolicy(ctx, userID, *groupID)
		if err != nil {
			return false, err
		}
	}
	owners, err := s.activeAssignmentOwners(ctx, []int64{accountID})
	if err != nil {
		return false, err
	}
	owner, allocated := owners[accountID]
	if managed {
		return allocated && owner.UserID == userID && owner.GroupID == *groupID, nil
	}
	return !allocated, nil
}

func (s *AccountAllocationService) hasActivePolicy(ctx context.Context, userID, groupID int64) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM account_allocation_policies
			WHERE user_id = $1 AND group_id = $2 AND status = 'active' AND deleted_at IS NULL
		)`, userID, groupID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check account allocation policy: %w", err)
	}
	return exists, nil
}

type accountAllocationOwner struct {
	UserID  int64
	GroupID int64
}

func (s *AccountAllocationService) activeAssignmentOwners(ctx context.Context, accountIDs []int64) (map[int64]accountAllocationOwner, error) {
	owners := make(map[int64]accountAllocationOwner)
	if len(accountIDs) == 0 {
		return owners, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT aa.account_id, p.user_id, aa.group_id
		FROM account_allocation_assignments aa
		JOIN account_allocation_policies p ON p.id = aa.policy_id
		WHERE aa.status = 'active' AND p.status = 'active' AND p.deleted_at IS NULL
			AND aa.account_id = ANY($1)`, pq.Array(accountIDs))
	if err != nil {
		return nil, fmt.Errorf("load account allocation owners: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var accountID int64
		var owner accountAllocationOwner
		if err := rows.Scan(&accountID, &owner.UserID, &owner.GroupID); err != nil {
			return nil, fmt.Errorf("scan account allocation owner: %w", err)
		}
		owners[accountID] = owner
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account allocation owners: %w", err)
	}
	return owners, nil
}

func (s *AccountAllocationService) validatePolicyInput(input AccountAllocationPolicyInput) error {
	if input.UserID <= 0 || input.GroupID <= 0 {
		return infraerrors.BadRequest("ACCOUNT_ALLOCATION_REFERENCE_INVALID", "user_id and group_id must be positive")
	}
	if input.DesiredCount < 0 || input.DesiredCount > s.maxDesiredCount {
		return infraerrors.BadRequest("ACCOUNT_ALLOCATION_DESIRED_COUNT_INVALID", fmt.Sprintf("desired_count must be between 0 and %d", s.maxDesiredCount))
	}
	return nil
}

// accountAllocationQueryRower is implemented by both *sql.DB and *sql.Tx.
// Keeping reference validation on the caller's transaction closes the race
// between a policy enable operation and a concurrent soft deletion of the
// referenced user or group.
type accountAllocationQueryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func ensureActiveAccountAllocationReferences(ctx context.Context, queryer accountAllocationQueryRower, userID, groupID int64) error {
	var userExists bool
	if err := queryer.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL AND status = 'active')`, userID).Scan(&userExists); err != nil {
		return fmt.Errorf("validate account allocation user: %w", err)
	}
	if !userExists {
		return infraerrors.NotFound("ACCOUNT_ALLOCATION_USER_NOT_FOUND", "active target user not found")
	}
	var groupExists bool
	if err := queryer.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE id = $1 AND deleted_at IS NULL AND status = 'active')`, groupID).Scan(&groupExists); err != nil {
		return fmt.Errorf("validate account allocation group: %w", err)
	}
	if !groupExists {
		return infraerrors.NotFound("ACCOUNT_ALLOCATION_GROUP_NOT_FOUND", "active target group not found")
	}
	return nil
}

type accountAllocationPolicyLock struct {
	ID            int64
	UserID        int64
	GroupID       int64
	DesiredCount  int
	AutoReplenish bool
	ReplaceOn401  bool
	ReplaceOn429  bool
	Status        string
}

func loadAccountAllocationPolicyForUpdate(ctx context.Context, tx *sql.Tx, policyID int64, skipLocked bool) (accountAllocationPolicyLock, error) {
	query := `
		SELECT id, user_id, group_id, desired_count, auto_replenish, replace_on_401, replace_on_429, status
		FROM account_allocation_policies WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`
	if skipLocked {
		query += " SKIP LOCKED"
	}
	var policy accountAllocationPolicyLock
	err := tx.QueryRowContext(ctx, query, policyID).Scan(
		&policy.ID, &policy.UserID, &policy.GroupID, &policy.DesiredCount, &policy.AutoReplenish,
		&policy.ReplaceOn401, &policy.ReplaceOn429, &policy.Status,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return accountAllocationPolicyLock{}, ErrAccountAllocationPolicyNotFound
	}
	if err != nil {
		return accountAllocationPolicyLock{}, fmt.Errorf("lock account allocation policy: %w", err)
	}
	return policy, nil
}

func createAccountAllocationAssignment(ctx context.Context, tx *sql.Tx, policy accountAllocationPolicyLock, accountID int64, assignedBy *int64) (int64, time.Time, error) {
	var assignmentID int64
	var assignedAt time.Time
	err := tx.QueryRowContext(ctx, `
		INSERT INTO account_allocation_assignments
			(policy_id, user_id, group_id, account_id, status, assigned_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, assigned_at`, policy.ID, policy.UserID, policy.GroupID, accountID, accountAllocationAssignmentLive, assignedBy).Scan(&assignmentID, &assignedAt)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("create account allocation assignment: %w", err)
	}
	return assignmentID, assignedAt, nil
}

func releaseActiveAssignmentsForPolicy(ctx context.Context, tx *sql.Tx, policyID int64, reason string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		UPDATE account_allocation_assignments
		SET status = $2, released_at = NOW(), release_reason = $3, updated_at = NOW()
		WHERE policy_id = $1 AND status = $4
		RETURNING id`, policyID, accountAllocationAssignmentGone, reason, accountAllocationAssignmentLive)
	if err != nil {
		return nil, fmt.Errorf("release active account allocation assignments: %w", err)
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan released account allocation assignment: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate released account allocation assignments: %w", err)
	}
	return ids, nil
}

func insertAccountAllocationEvent(ctx context.Context, tx *sql.Tx, policyID int64, assignmentID *int64, eventType string, actorUserID *int64, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal account allocation event metadata: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_allocation_events (policy_id, assignment_id, event_type, actor_user_id, metadata)
		VALUES ($1, $2, $3, $4, $5::jsonb)`, policyID, assignmentID, eventType, actorUserID, string(payload)); err != nil {
		return fmt.Errorf("insert account allocation event: %w", err)
	}
	return nil
}

func nullablePositiveID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	value := id
	return &value
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}
