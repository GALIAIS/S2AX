package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type AccountUsageVisibilityGrantScope string

const (
	AccountUsageVisibilityGrantExclusiveGroup AccountUsageVisibilityGrantScope = "exclusive_group"
	AccountUsageVisibilityGrantUserAccount    AccountUsageVisibilityGrantScope = "user_account"
)

var (
	ErrAccountUsageVisibilityGrantNotFound = infraerrors.NotFound(
		"ACCOUNT_USAGE_VISIBILITY_GRANT_NOT_FOUND",
		"account usage visibility grant not found",
	)
	ErrAccountUsageVisibilityGrantConflict = infraerrors.Conflict(
		"ACCOUNT_USAGE_VISIBILITY_GRANT_EXISTS",
		"an equivalent account usage visibility grant already exists",
	)
)

type AccountUsageVisibilityGrant struct {
	ID          int64                            `json:"id"`
	Scope       AccountUsageVisibilityGrantScope `json:"scope"`
	GroupID     int64                            `json:"group_id"`
	GroupName   string                           `json:"group_name"`
	UserID      *int64                           `json:"user_id,omitempty"`
	UserEmail   string                           `json:"user_email,omitempty"`
	Username    string                           `json:"username,omitempty"`
	AccountID   *int64                           `json:"account_id,omitempty"`
	AccountName string                           `json:"account_name,omitempty"`
	Platform    string                           `json:"platform,omitempty"`
	AccountType string                           `json:"account_type,omitempty"`
	CreatedBy   *int64                           `json:"created_by,omitempty"`
	CreatedAt   time.Time                        `json:"created_at"`
}

type AccountUsageVisibilityGrantInput struct {
	Scope       AccountUsageVisibilityGrantScope
	GroupID     int64
	UserID      int64
	AccountID   int64
	ActorUserID int64
}

func (s *AccountAllocationService) ListAccountUsageVisibilityGrants(ctx context.Context) ([]AccountUsageVisibilityGrant, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("account allocation database is unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			visibility.id,
			visibility.grant_scope,
			visibility.group_id,
			g.name,
			visibility.user_id,
			COALESCE(u.email, ''),
			COALESCE(u.username, ''),
			visibility.account_id,
			COALESCE(a.name, ''),
			COALESCE(a.platform, ''),
			COALESCE(a.type, ''),
			visibility.created_by,
			visibility.created_at
		FROM account_usage_visibility_grants visibility
		JOIN groups g ON g.id = visibility.group_id
		LEFT JOIN users u ON u.id = visibility.user_id
		LEFT JOIN accounts a ON a.id = visibility.account_id
		ORDER BY
			CASE visibility.grant_scope WHEN 'exclusive_group' THEN 0 ELSE 1 END,
			g.name ASC,
			u.email ASC NULLS FIRST,
			a.name ASC NULLS FIRST,
			visibility.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list account usage visibility grants: %w", err)
	}
	defer rows.Close()

	grants := make([]AccountUsageVisibilityGrant, 0)
	for rows.Next() {
		grant, err := scanAccountUsageVisibilityGrant(rows)
		if err != nil {
			return nil, fmt.Errorf("scan account usage visibility grant: %w", err)
		}
		grants = append(grants, *grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account usage visibility grants: %w", err)
	}
	return grants, nil
}

func (s *AccountAllocationService) CreateAccountUsageVisibilityGrant(ctx context.Context, input AccountUsageVisibilityGrantInput) (*AccountUsageVisibilityGrant, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("account allocation database is unavailable")
	}
	if err := validateAccountUsageVisibilityGrantInput(input); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin account usage visibility grant: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	switch input.Scope {
	case AccountUsageVisibilityGrantExclusiveGroup:
		var exclusive bool
		err = tx.QueryRowContext(ctx, `
			SELECT is_exclusive
			FROM groups
			WHERE id = $1 AND deleted_at IS NULL AND status = 'active'`,
			input.GroupID,
		).Scan(&exclusive)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, infraerrors.NotFound("ACCOUNT_USAGE_VISIBILITY_GROUP_NOT_FOUND", "active target group not found")
		}
		if err != nil {
			return nil, fmt.Errorf("validate account usage visibility group: %w", err)
		}
		if !exclusive {
			return nil, infraerrors.BadRequest(
				"ACCOUNT_USAGE_VISIBILITY_GROUP_NOT_EXCLUSIVE",
				"group-wide usage details can only be enabled for an exclusive group",
			)
		}
	case AccountUsageVisibilityGrantUserAccount:
		if err := ensureActiveAccountAllocationReferences(ctx, tx, input.UserID, input.GroupID); err != nil {
			return nil, err
		}
		accessStatus, err := resolveAccountAllocationAccess(ctx, tx, input.UserID, input.GroupID)
		if err != nil {
			return nil, err
		}
		if accessStatus != AccountAllocationAccessReady {
			return nil, ErrAccountAllocationAccessRequired
		}

		var accountInGroup bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM accounts a
				JOIN account_groups ag ON ag.account_id = a.id
				WHERE a.id = $1
					AND a.deleted_at IS NULL
					AND ag.group_id = $2
			)`,
			input.AccountID,
			input.GroupID,
		).Scan(&accountInGroup); err != nil {
			return nil, fmt.Errorf("validate account usage visibility account: %w", err)
		}
		if !accountInGroup {
			return nil, ErrAccountAllocationAccountUnavailable
		}

		var activelyLeased bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM account_allocation_assignments aa
				WHERE aa.account_id = $1
					AND aa.status = 'active'
			)`,
			input.AccountID,
		).Scan(&activelyLeased); err != nil {
			return nil, fmt.Errorf("validate account usage visibility lease: %w", err)
		}
		if activelyLeased {
			return nil, infraerrors.Conflict(
				"ACCOUNT_USAGE_VISIBILITY_ACCOUNT_LEASED",
				"account already has an active exclusive lease",
			)
		}
	}

	var grantID int64
	var userID, accountID any
	if input.Scope == AccountUsageVisibilityGrantUserAccount {
		userID = input.UserID
		accountID = input.AccountID
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO account_usage_visibility_grants (
			grant_scope, group_id, user_id, account_id, created_by
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		input.Scope,
		input.GroupID,
		userID,
		accountID,
		input.ActorUserID,
	).Scan(&grantID)
	if isUniqueViolation(err) {
		return nil, ErrAccountUsageVisibilityGrantConflict
	}
	if err != nil {
		return nil, fmt.Errorf("create account usage visibility grant: %w", err)
	}

	grant, err := getAccountUsageVisibilityGrant(ctx, tx, grantID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit account usage visibility grant: %w", err)
	}
	return grant, nil
}

func (s *AccountAllocationService) DeleteAccountUsageVisibilityGrant(ctx context.Context, grantID int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("account allocation database is unavailable")
	}
	if grantID <= 0 {
		return infraerrors.BadRequest("ACCOUNT_USAGE_VISIBILITY_GRANT_ID_INVALID", "grant id must be positive")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM account_usage_visibility_grants WHERE id = $1`, grantID)
	if err != nil {
		return fmt.Errorf("delete account usage visibility grant: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted account usage visibility grant count: %w", err)
	}
	if affected == 0 {
		return ErrAccountUsageVisibilityGrantNotFound
	}
	return nil
}

func validateAccountUsageVisibilityGrantInput(input AccountUsageVisibilityGrantInput) error {
	if input.GroupID <= 0 || input.ActorUserID <= 0 {
		return infraerrors.BadRequest(
			"ACCOUNT_USAGE_VISIBILITY_REFERENCE_INVALID",
			"group_id and actor user id must be positive",
		)
	}
	switch input.Scope {
	case AccountUsageVisibilityGrantExclusiveGroup:
		if input.UserID != 0 || input.AccountID != 0 {
			return infraerrors.BadRequest(
				"ACCOUNT_USAGE_VISIBILITY_SCOPE_INVALID",
				"exclusive_group grants cannot include user_id or account_id",
			)
		}
	case AccountUsageVisibilityGrantUserAccount:
		if input.UserID <= 0 || input.AccountID <= 0 {
			return infraerrors.BadRequest(
				"ACCOUNT_USAGE_VISIBILITY_SCOPE_INVALID",
				"user_account grants require positive user_id and account_id",
			)
		}
	default:
		return infraerrors.BadRequest(
			"ACCOUNT_USAGE_VISIBILITY_SCOPE_INVALID",
			"scope must be exclusive_group or user_account",
		)
	}
	return nil
}

type accountUsageVisibilityGrantScanner interface {
	Scan(dest ...any) error
}

func getAccountUsageVisibilityGrant(ctx context.Context, queryer accountAllocationQueryRower, grantID int64) (*AccountUsageVisibilityGrant, error) {
	grant, err := scanAccountUsageVisibilityGrant(queryer.QueryRowContext(ctx, `
		SELECT
			visibility.id,
			visibility.grant_scope,
			visibility.group_id,
			g.name,
			visibility.user_id,
			COALESCE(u.email, ''),
			COALESCE(u.username, ''),
			visibility.account_id,
			COALESCE(a.name, ''),
			COALESCE(a.platform, ''),
			COALESCE(a.type, ''),
			visibility.created_by,
			visibility.created_at
		FROM account_usage_visibility_grants visibility
		JOIN groups g ON g.id = visibility.group_id
		LEFT JOIN users u ON u.id = visibility.user_id
		LEFT JOIN accounts a ON a.id = visibility.account_id
		WHERE visibility.id = $1`,
		grantID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAccountUsageVisibilityGrantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get account usage visibility grant: %w", err)
	}
	return grant, nil
}

func scanAccountUsageVisibilityGrant(scanner accountUsageVisibilityGrantScanner) (*AccountUsageVisibilityGrant, error) {
	var grant AccountUsageVisibilityGrant
	var scope string
	var userID, accountID, createdBy sql.NullInt64
	if err := scanner.Scan(
		&grant.ID,
		&scope,
		&grant.GroupID,
		&grant.GroupName,
		&userID,
		&grant.UserEmail,
		&grant.Username,
		&accountID,
		&grant.AccountName,
		&grant.Platform,
		&grant.AccountType,
		&createdBy,
		&grant.CreatedAt,
	); err != nil {
		return nil, err
	}
	grant.Scope = AccountUsageVisibilityGrantScope(scope)
	switch grant.Scope {
	case AccountUsageVisibilityGrantExclusiveGroup, AccountUsageVisibilityGrantUserAccount:
	default:
		return nil, fmt.Errorf("unknown account usage visibility grant scope %q", scope)
	}
	if userID.Valid {
		value := userID.Int64
		grant.UserID = &value
	}
	if accountID.Valid {
		value := accountID.Int64
		grant.AccountID = &value
	}
	if createdBy.Valid {
		value := createdBy.Int64
		grant.CreatedBy = &value
	}
	return &grant, nil
}
