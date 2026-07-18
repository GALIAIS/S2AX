package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type virtualCurrencyRepository struct {
	db *sql.DB
}

type virtualCurrencySQLExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type virtualCurrencyScannable interface {
	Scan(dest ...any) error
}

type virtualCurrencyRowsRow struct {
	rows *sql.Rows
	err  error
}

func (r *virtualCurrencyRowsRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	defer func() { _ = r.rows.Close() }()
	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	return r.rows.Scan(dest...)
}

func virtualCurrencyQueryRow(ctx context.Context, client virtualCurrencySQLExecutor, query string, args ...any) virtualCurrencyScannable {
	if queryRowClient, ok := client.(interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	}); ok {
		return queryRowClient.QueryRowContext(ctx, query, args...)
	}
	rows, err := client.QueryContext(ctx, query, args...)
	return &virtualCurrencyRowsRow{rows: rows, err: err}
}

const virtualCurrencyHoldSelect = `
SELECT h.id, h.currency_id, c.code, c.name, c.symbol, c.scale, h.user_id, h.group_id,
       h.amount_units, h.status, h.source_type, h.source_id, h.idempotency_key,
       h.expires_at, h.metadata, h.created_at, h.updated_at, h.settled_at
FROM virtual_currency_holds h
JOIN virtual_currencies c ON c.id = h.currency_id `

const (
	virtualCurrencyAccountUserAvailable  = "user_available"
	virtualCurrencyAccountUserReserved   = "user_reserved"
	virtualCurrencyAccountSystemIssuance = "system_issuance"
	virtualCurrencyAccountSystemSink     = "system_sink"
	virtualCurrencyAccountSystemAdjust   = "system_adjustment"
)

type virtualCurrencyPosting struct {
	accountKind string
	userID      any
	amountUnits int64
}

func NewVirtualCurrencyRepository(db *sql.DB) service.VirtualCurrencyRepository {
	return &virtualCurrencyRepository{db: db}
}

func (r *virtualCurrencyRepository) executor(ctx context.Context) virtualCurrencySQLExecutor {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx
	}
	return r.db
}

func (r *virtualCurrencyRepository) ListCurrencies(ctx context.Context, includeDisabled bool) ([]*service.VirtualCurrency, error) {
	query := `
SELECT id, code, name, symbol, description, scale, status, metadata, created_by, created_at, updated_at
FROM virtual_currencies`
	args := make([]any, 0, 1)
	if !includeDisabled {
		query += ` WHERE status = $1`
		args = append(args, service.VirtualCurrencyStatusActive)
	}
	query += ` ORDER BY id ASC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]*service.VirtualCurrency, 0)
	for rows.Next() {
		item, scanErr := scanVirtualCurrency(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *virtualCurrencyRepository) GetCurrencyByID(ctx context.Context, id int64) (*service.VirtualCurrency, error) {
	return r.getCurrency(ctx, r.executor(ctx), `WHERE id = $1`, id)
}

func (r *virtualCurrencyRepository) GetCurrencyByCode(ctx context.Context, code string) (*service.VirtualCurrency, error) {
	return r.getCurrency(ctx, r.executor(ctx), `WHERE LOWER(code) = LOWER($1)`, strings.TrimSpace(code))
}

func (r *virtualCurrencyRepository) CreateCurrency(ctx context.Context, input service.VirtualCurrencyCreateInput) (*service.VirtualCurrency, error) {
	metadata, err := marshalCurrencyMetadata(input.Metadata)
	if err != nil {
		return nil, err
	}
	item, err := r.scanCurrencyRow(r.db.QueryRowContext(ctx, `
INSERT INTO virtual_currencies (code, name, symbol, description, scale, status, metadata, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
RETURNING id, code, name, symbol, description, scale, status, metadata, created_by, created_at, updated_at`,
		input.Code, input.Name, input.Symbol, input.Description, input.Scale, service.VirtualCurrencyStatusActive, metadata, nullableInt64(input.CreatedBy)))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, service.ErrVirtualCurrencyCodeExists
		}
		return nil, err
	}
	return item, nil
}

func (r *virtualCurrencyRepository) UpdateCurrency(ctx context.Context, id int64, input service.VirtualCurrencyUpdateInput) (*service.VirtualCurrency, error) {
	var metadata any
	if input.Metadata != nil {
		encoded, err := marshalCurrencyMetadata(input.Metadata)
		if err != nil {
			return nil, err
		}
		metadata = encoded
	}
	item, err := r.scanCurrencyRow(r.db.QueryRowContext(ctx, `
UPDATE virtual_currencies
SET name = COALESCE($2, name),
    symbol = COALESCE($3, symbol),
    description = COALESCE($4, description),
    metadata = COALESCE($5::jsonb, metadata),
    updated_at = NOW()
WHERE id = $1
RETURNING id, code, name, symbol, description, scale, status, metadata, created_by, created_at, updated_at`,
		id, nullableString(input.Name), nullableString(input.Symbol), nullableString(input.Description), metadata))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrVirtualCurrencyNotFound
		}
		return nil, err
	}
	return item, nil
}

func (r *virtualCurrencyRepository) SetCurrencyStatus(ctx context.Context, id int64, status string) (*service.VirtualCurrency, error) {
	item, err := r.scanCurrencyRow(r.db.QueryRowContext(ctx, `
UPDATE virtual_currencies
SET status = $2, updated_at = NOW()
WHERE id = $1
RETURNING id, code, name, symbol, description, scale, status, metadata, created_by, created_at, updated_at`, id, status))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVirtualCurrencyNotFound
	}
	return item, err
}

func (r *virtualCurrencyRepository) ListGroupPolicies(ctx context.Context, currencyID int64) ([]*service.VirtualCurrencyGroupPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, currency_id, group_id, enabled, can_earn, can_spend, max_balance_units,
       metadata, created_at, updated_at
FROM virtual_currency_group_policies
WHERE currency_id = $1
ORDER BY group_id ASC`, currencyID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]*service.VirtualCurrencyGroupPolicy, 0)
	for rows.Next() {
		item, scanErr := scanVirtualCurrencyPolicy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *virtualCurrencyRepository) UpsertGroupPolicy(ctx context.Context, input service.VirtualCurrencyPolicyInput) (*service.VirtualCurrencyGroupPolicy, error) {
	metadata, err := marshalCurrencyMetadata(input.Metadata)
	if err != nil {
		return nil, err
	}
	item, err := scanPolicyRow(r.db.QueryRowContext(ctx, `
INSERT INTO virtual_currency_group_policies
    (currency_id, group_id, enabled, can_earn, can_spend, max_balance_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
ON CONFLICT (currency_id, group_id) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    can_earn = EXCLUDED.can_earn,
    can_spend = EXCLUDED.can_spend,
    max_balance_units = EXCLUDED.max_balance_units,
    metadata = EXCLUDED.metadata,
    updated_at = NOW()
RETURNING id, currency_id, group_id, enabled, can_earn, can_spend, max_balance_units,
          metadata, created_at, updated_at`,
		input.CurrencyID, input.GroupID, input.Enabled, input.CanEarn, input.CanSpend, nullableInt64(input.MaxBalanceUnits), metadata))
	if err != nil {
		if isForeignKeyViolation(err) {
			return nil, service.ErrVirtualCurrencyInvalidInput
		}
		return nil, err
	}
	return item, nil
}

func (r *virtualCurrencyRepository) EnableForAllUsers(ctx context.Context, currencyID int64) ([]*service.VirtualCurrencyGroupPolicy, error) {
	rows, err := r.executor(ctx).QueryContext(ctx, `
WITH upserted AS (
    INSERT INTO virtual_currency_group_policies
        (currency_id, group_id, enabled, can_earn, can_spend, max_balance_units, metadata)
    SELECT $1, g.id, TRUE, TRUE, TRUE, NULL, '{}'::jsonb
    FROM groups g
    WHERE g.status = 'active'
      AND g.deleted_at IS NULL
      AND g.subscription_type = 'standard'
      AND g.is_exclusive = FALSE
    ON CONFLICT (currency_id, group_id) DO UPDATE SET
        enabled = TRUE,
        can_earn = TRUE,
        can_spend = TRUE,
        updated_at = NOW()
    RETURNING id, currency_id, group_id, enabled, can_earn, can_spend,
              max_balance_units, metadata, created_at, updated_at
)
SELECT id, currency_id, group_id, enabled, can_earn, can_spend,
       max_balance_units, metadata, created_at, updated_at
FROM upserted
ORDER BY group_id ASC`, currencyID)
	if err != nil {
		if isForeignKeyViolation(err) {
			return nil, service.ErrVirtualCurrencyNotFound
		}
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]*service.VirtualCurrencyGroupPolicy, 0)
	for rows.Next() {
		item, scanErr := scanVirtualCurrencyPolicy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *virtualCurrencyRepository) DeleteGroupPolicy(ctx context.Context, currencyID, groupID int64) error {
	result, err := r.db.ExecContext(ctx, `
DELETE FROM virtual_currency_group_policies
WHERE currency_id = $1 AND group_id = $2`, currencyID, groupID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrVirtualCurrencyPolicyNotFound
	}
	return nil
}

func (r *virtualCurrencyRepository) ApplyCurrencyDelta(ctx context.Context, input service.VirtualCurrencyDeltaInput) (*service.VirtualCurrencyLedgerEntry, error) {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.applyCurrencyDeltaTx(ctx, tx, input)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	entry, err := r.applyCurrencyDeltaTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return entry, nil
}

func (r *virtualCurrencyRepository) applyCurrencyDeltaTx(ctx context.Context, client virtualCurrencySQLExecutor, input service.VirtualCurrencyDeltaInput) (*service.VirtualCurrencyLedgerEntry, error) {

	currency, err := r.getCurrency(ctx, client, `WHERE id = $1`, input.CurrencyID)
	if err != nil {
		return nil, err
	}
	if _, err := client.ExecContext(ctx, `
INSERT INTO virtual_currency_wallets (user_id, currency_id)
VALUES ($1, $2)
	ON CONFLICT (user_id, currency_id) DO NOTHING`, input.UserID, input.CurrencyID); err != nil {
		if isForeignKeyViolation(err) {
			return nil, service.ErrVirtualCurrencyInvalidInput
		}
		return nil, err
	}

	var (
		walletID  int64
		available int64
		reserved  int64
		version   int64
	)
	if err := virtualCurrencyQueryRow(ctx, client, `
SELECT id, available_units, reserved_units, version
FROM virtual_currency_wallets
WHERE user_id = $1 AND currency_id = $2
FOR UPDATE`, input.UserID, input.CurrencyID).Scan(&walletID, &available, &reserved, &version); err != nil {
		return nil, err
	}

	// Locking the wallet before checking the ledger makes a duplicate request deterministic even when two workers arrive together.
	if existing, lookupErr := r.getLedgerByIdempotency(ctx, client, input.CurrencyID, input.UserID, input.IdempotencyKey); lookupErr == nil {
		if existing.RequestFingerprint != input.RequestFingerprint {
			return nil, service.ErrVirtualCurrencyIdempotency
		}
		return existing, nil
	} else if !errors.Is(lookupErr, sql.ErrNoRows) {
		return nil, lookupErr
	}

	// Historical idempotent requests are replayed even after an administrator
	// disables the currency or removes the policy. New mutations must pass the
	// current policy checks below.
	if currency.Status != service.VirtualCurrencyStatusActive && !input.AllowSettlementWithoutPolicy {
		return nil, service.ErrVirtualCurrencyDisabled
	}

	var (
		policyEnabled bool
		canEarn       bool
		canSpend      bool
		maxBalance    sql.NullInt64
	)
	policyErr := error(nil)
	if !input.AllowSettlementWithoutPolicy {
		policyErr = virtualCurrencyQueryRow(ctx, client, `
SELECT enabled, can_earn, can_spend, max_balance_units
FROM virtual_currency_group_policies
WHERE currency_id = $1 AND group_id = $2`, input.CurrencyID, input.GroupID).
			Scan(&policyEnabled, &canEarn, &canSpend, &maxBalance)
		if errors.Is(policyErr, sql.ErrNoRows) {
			return nil, service.ErrVirtualCurrencyGroupDenied
		}
		if policyErr != nil {
			return nil, policyErr
		}
		if !policyEnabled || (input.RequireCanSpend && !canSpend) || (input.RequireCanEarn && !canEarn) {
			return nil, service.ErrVirtualCurrencyGroupDenied
		}
	}
	if input.RequireUserAccess && !input.AllowSettlementWithoutPolicy {
		var allowed bool
		if err := virtualCurrencyQueryRow(ctx, client, `
SELECT EXISTS (
    SELECT 1
    FROM groups g
    WHERE g.id = $1
      AND g.status = 'active'
      AND g.deleted_at IS NULL
      AND (
        (g.subscription_type = 'standard' AND (
            NOT g.is_exclusive
            OR EXISTS (
                SELECT 1 FROM user_allowed_groups uag
                WHERE uag.user_id = $2 AND uag.group_id = g.id
            )
        ))
        OR (g.subscription_type = 'subscription' AND EXISTS (
            SELECT 1 FROM user_subscriptions us
            WHERE us.user_id = $2
              AND us.group_id = g.id
              AND us.status = 'active'
              AND us.deleted_at IS NULL
              AND (us.expires_at IS NULL OR us.expires_at > NOW())
        ))
      )
)`, input.GroupID, input.UserID).Scan(&allowed); err != nil {
			return nil, err
		}
		if !allowed {
			return nil, service.ErrVirtualCurrencyGroupDenied
		}
	}

	newAvailable, ok := safeAddInt64(available, input.AvailableDeltaUnits)
	if !ok || newAvailable < 0 {
		return nil, service.ErrVirtualCurrencyInsufficient
	}
	newReserved, ok := safeAddInt64(reserved, input.ReservedDeltaUnits)
	if !ok || newReserved < 0 {
		return nil, service.ErrVirtualCurrencyInsufficient
	}
	if maxBalance.Valid {
		total, totalOK := safeAddInt64(newAvailable, newReserved)
		if !totalOK || total > maxBalance.Int64 {
			return nil, service.ErrVirtualCurrencyLimitExceeded
		}
	}

	if _, err := client.ExecContext(ctx, `
UPDATE virtual_currency_wallets
SET available_units = $1,
    reserved_units = $2,
    version = version + 1,
    updated_at = NOW()
WHERE id = $3 AND version = $4`, newAvailable, newReserved, walletID, version); err != nil {
		return nil, err
	}

	metadata, err := marshalCurrencyMetadata(input.Metadata)
	if err != nil {
		return nil, err
	}
	journalID, err := r.createJournalTx(ctx, client, input, metadata)
	if err != nil {
		return nil, err
	}
	var entryID int64
	if err := virtualCurrencyQueryRow(ctx, client, `
INSERT INTO virtual_currency_ledger_entries
    (currency_id, user_id, group_id, delta_units, available_delta_units, reserved_delta_units,
     available_after_units, reserved_after_units, entry_type, source_type, source_id,
     idempotency_key, request_fingerprint, reason, metadata, created_by, journal_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''), $12, $13, $14, $15::jsonb, $16, $17)
RETURNING id`,
		input.CurrencyID, input.UserID, input.GroupID, input.DeltaUnits, input.AvailableDeltaUnits,
		input.ReservedDeltaUnits, newAvailable, newReserved, input.EntryType, input.SourceType,
		input.SourceID, input.IdempotencyKey, input.RequestFingerprint, input.Reason, metadata,
		nullableInt64(input.CreatedBy), journalID).Scan(&entryID); err != nil {
		if isUniqueViolation(err) {
			return nil, service.ErrVirtualCurrencyIdempotency
		}
		return nil, err
	}
	if err := r.insertJournalPostingsTx(ctx, client, journalID, input); err != nil {
		return nil, err
	}
	if err := r.sealJournalTx(ctx, client, journalID); err != nil {
		return nil, err
	}

	entry := &service.VirtualCurrencyLedgerEntry{
		ID:                  entryID,
		JournalID:           journalID,
		CurrencyID:          input.CurrencyID,
		CurrencyCode:        currency.Code,
		CurrencyName:        currency.Name,
		CurrencySymbol:      currency.Symbol,
		CurrencyScale:       currency.Scale,
		UserID:              input.UserID,
		GroupID:             int64Ptr(input.GroupID),
		DeltaUnits:          input.DeltaUnits,
		AvailableDeltaUnits: input.AvailableDeltaUnits,
		ReservedDeltaUnits:  input.ReservedDeltaUnits,
		AvailableAfterUnits: newAvailable,
		ReservedAfterUnits:  newReserved,
		EntryType:           input.EntryType,
		SourceType:          input.SourceType,
		SourceID:            nullableStringPtr(input.SourceID),
		IdempotencyKey:      input.IdempotencyKey,
		RequestFingerprint:  input.RequestFingerprint,
		Reason:              input.Reason,
		Metadata:            input.Metadata,
		CreatedBy:           input.CreatedBy,
		CreatedAt:           time.Now().UTC(),
	}
	return entry, nil
}

func (r *virtualCurrencyRepository) createJournalTx(ctx context.Context, client virtualCurrencySQLExecutor, input service.VirtualCurrencyDeltaInput, metadata string) (int64, error) {
	var journalID int64
	err := virtualCurrencyQueryRow(ctx, client, `
INSERT INTO virtual_currency_journals
    (currency_id, initiator_user_id, group_id, entry_type, source_type, source_id,
     idempotency_key, request_fingerprint, reason, metadata, created_by)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, $10::jsonb, $11)
RETURNING id`, input.CurrencyID, input.UserID, input.GroupID, input.EntryType, input.SourceType,
		input.SourceID, input.IdempotencyKey, input.RequestFingerprint, input.Reason, metadata,
		nullableInt64(input.CreatedBy)).Scan(&journalID)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, service.ErrVirtualCurrencyIdempotency
		}
		return 0, err
	}
	return journalID, nil
}

func (r *virtualCurrencyRepository) insertJournalPostingsTx(ctx context.Context, client virtualCurrencySQLExecutor, journalID int64, input service.VirtualCurrencyDeltaInput) error {
	for _, posting := range buildVirtualCurrencyPostings(input) {
		if _, err := client.ExecContext(ctx, `
INSERT INTO virtual_currency_postings
    (journal_id, currency_id, user_id, account_kind, amount_units)
VALUES ($1, $2, $3, $4, $5)`, journalID, input.CurrencyID, posting.userID, posting.accountKind, posting.amountUnits); err != nil {
			return err
		}
	}
	return nil
}

func (r *virtualCurrencyRepository) sealJournalTx(ctx context.Context, client virtualCurrencySQLExecutor, journalID int64) error {
	result, err := client.ExecContext(ctx, `
UPDATE virtual_currency_journals
SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, journalID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("seal virtual currency journal %d: expected one draft journal, updated %d", journalID, affected)
	}
	return nil
}

func buildVirtualCurrencyPostings(input service.VirtualCurrencyDeltaInput) []virtualCurrencyPosting {
	postings := make([]virtualCurrencyPosting, 0, 3)
	if input.AvailableDeltaUnits != 0 {
		postings = append(postings, virtualCurrencyPosting{
			accountKind: virtualCurrencyAccountUserAvailable,
			userID:      input.UserID,
			amountUnits: input.AvailableDeltaUnits,
		})
	}
	if input.ReservedDeltaUnits != 0 {
		postings = append(postings, virtualCurrencyPosting{
			accountKind: virtualCurrencyAccountUserReserved,
			userID:      input.UserID,
			amountUnits: input.ReservedDeltaUnits,
		})
	}
	if input.DeltaUnits != 0 {
		accountKind := virtualCurrencyAccountSystemAdjust
		switch input.EntryType {
		case service.VirtualCurrencyEntryGrant:
			accountKind = virtualCurrencyAccountSystemIssuance
		case service.VirtualCurrencyEntrySpend, service.VirtualCurrencyEntryCommit, service.VirtualCurrencyEntryRefund:
			accountKind = virtualCurrencyAccountSystemSink
		}
		postings = append(postings, virtualCurrencyPosting{
			accountKind: accountKind,
			amountUnits: -input.DeltaUnits,
		})
	}
	return postings
}

func (r *virtualCurrencyRepository) ReserveHold(ctx context.Context, input service.VirtualCurrencyHoldReserveInput) (*service.VirtualCurrencyHold, *service.VirtualCurrencyLedgerEntry, error) {
	if input.ExpiresAt.IsZero() || !input.ExpiresAt.After(time.Now().UTC()) {
		return nil, nil, service.ErrVirtualCurrencyInvalidInput
	}
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.reserveHoldTx(ctx, tx, input)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	return r.reserveHoldWithTx(ctx, tx, input)
}

func (r *virtualCurrencyRepository) reserveHoldWithTx(ctx context.Context, tx *sql.Tx, input service.VirtualCurrencyHoldReserveInput) (*service.VirtualCurrencyHold, *service.VirtualCurrencyLedgerEntry, error) {
	hold, entry, err := r.reserveHoldTx(ctx, tx, input)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return hold, entry, nil
}

func (r *virtualCurrencyRepository) reserveHoldTx(ctx context.Context, client virtualCurrencySQLExecutor, input service.VirtualCurrencyHoldReserveInput) (*service.VirtualCurrencyHold, *service.VirtualCurrencyLedgerEntry, error) {
	entry, err := r.applyCurrencyDeltaTx(ctx, client, service.VirtualCurrencyDeltaInput{
		CurrencyID:          input.CurrencyID,
		UserID:              input.UserID,
		GroupID:             input.GroupID,
		DeltaUnits:          0,
		AvailableDeltaUnits: -input.AmountUnits,
		ReservedDeltaUnits:  input.AmountUnits,
		EntryType:           service.VirtualCurrencyEntryReserve,
		SourceType:          input.SourceType,
		SourceID:            input.SourceID,
		IdempotencyKey:      input.IdempotencyKey,
		RequestFingerprint:  input.RequestFingerprint,
		Reason:              input.Reason,
		Metadata:            input.Metadata,
		RequireUserAccess:   true,
		RequireCanSpend:     true,
	})
	if err != nil {
		return nil, nil, err
	}

	metadata, err := marshalCurrencyMetadata(input.Metadata)
	if err != nil {
		return nil, nil, err
	}
	var holdID int64
	err = virtualCurrencyQueryRow(ctx, client, `
INSERT INTO virtual_currency_holds
    (currency_id, user_id, group_id, amount_units, status, source_type, source_id,
     idempotency_key, expires_at, metadata)
VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, $10::jsonb)
RETURNING id`, input.CurrencyID, input.UserID, input.GroupID, input.AmountUnits,
		service.VirtualCurrencyHoldStatusActive, input.SourceType, input.SourceID,
		input.IdempotencyKey, input.ExpiresAt.UTC(), metadata).Scan(&holdID)
	if err != nil {
		if isUniqueViolation(err) {
			existing, lookupErr := r.getHoldByIdempotency(ctx, client, input.CurrencyID, input.UserID, input.IdempotencyKey)
			if lookupErr != nil {
				return nil, nil, lookupErr
			}
			return existing, entry, nil
		}
		return nil, nil, err
	}
	hold, err := r.getHold(ctx, client, "WHERE h.id = $1", holdID)
	if err != nil {
		return nil, nil, err
	}
	return hold, entry, nil
}

func (r *virtualCurrencyRepository) CommitHold(ctx context.Context, input service.VirtualCurrencyHoldSettlementRepositoryInput) (*service.VirtualCurrencyHold, *service.VirtualCurrencyLedgerEntry, error) {
	return r.settleHold(ctx, input, service.VirtualCurrencyEntryCommit, service.VirtualCurrencyHoldStatusCommitted)
}

func (r *virtualCurrencyRepository) ReleaseHold(ctx context.Context, input service.VirtualCurrencyHoldSettlementRepositoryInput) (*service.VirtualCurrencyHold, *service.VirtualCurrencyLedgerEntry, error) {
	return r.settleHold(ctx, input, service.VirtualCurrencyEntryRelease, service.VirtualCurrencyHoldStatusReleased)
}

func (r *virtualCurrencyRepository) ExpireExpiredHolds(ctx context.Context, currencyID int64, limit int) (int64, error) {
	if limit < 1 || limit > 500 {
		return 0, service.ErrVirtualCurrencyInvalidInput
	}
	query := `SELECT id, user_id
FROM virtual_currency_holds
WHERE status = $1 AND expires_at <= NOW()`
	args := []any{service.VirtualCurrencyHoldStatusActive}
	if currencyID > 0 {
		query += ` AND currency_id = $2`
		args = append(args, currencyID)
	}
	query += fmt.Sprintf(" ORDER BY expires_at ASC, id ASC LIMIT $%d", len(args)+1)
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	type expiredHoldCandidate struct {
		id     int64
		userID int64
	}
	candidates := make([]expiredHoldCandidate, 0, limit)
	for rows.Next() {
		var candidate expiredHoldCandidate
		if err := rows.Scan(&candidate.id, &candidate.userID); err != nil {
			return 0, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	var expired int64
	for _, candidate := range candidates {
		hold, err := r.GetHold(ctx, candidate.userID, candidate.id)
		if errors.Is(err, service.ErrVirtualCurrencyHoldNotFound) {
			continue
		}
		if err != nil {
			return expired, err
		}
		sourceID := fmt.Sprintf("hold-expiry-sweep:%d", candidate.id)
		reason := "expired hold sweep"
		metadata := map[string]any{"automatic": true}
		_, _, err = r.settleHold(ctx, service.VirtualCurrencyHoldSettlementRepositoryInput{
			HoldID:             candidate.id,
			UserID:             candidate.userID,
			Action:             service.VirtualCurrencyEntryRelease,
			SourceType:         "system",
			SourceID:           sourceID,
			IdempotencyKey:     fmt.Sprintf("sweep:%d", candidate.id),
			RequestFingerprint: service.VirtualCurrencyHoldMutationFingerprint(hold.CurrencyID, hold.UserID, dereferenceInt64(hold.GroupID), hold.AmountUnits, hold.ExpiresAt, service.VirtualCurrencyEntryRelease, "system", sourceID, reason, metadata),
			Reason:             reason,
			Metadata:           metadata,
		}, service.VirtualCurrencyEntryExpire, service.VirtualCurrencyHoldStatusExpired)
		if errors.Is(err, service.ErrVirtualCurrencyHoldState) || errors.Is(err, service.ErrVirtualCurrencyHoldNotFound) {
			continue
		}
		if err != nil {
			return expired, err
		}
		expired++
	}
	return expired, nil
}

func (r *virtualCurrencyRepository) settleHold(ctx context.Context, input service.VirtualCurrencyHoldSettlementRepositoryInput, entryType, settledStatus string) (*service.VirtualCurrencyHold, *service.VirtualCurrencyLedgerEntry, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	hold, err := r.getHold(ctx, tx, "WHERE h.id = $1 AND h.user_id = $2 FOR UPDATE OF h", input.HoldID, input.UserID)
	if errors.Is(err, service.ErrVirtualCurrencyHoldNotFound) {
		return nil, nil, err
	}
	if err != nil {
		return nil, nil, err
	}

	if existing, lookupErr := r.getLedgerByIdempotency(ctx, tx, hold.CurrencyID, input.UserID, input.IdempotencyKey); lookupErr == nil {
		if existing.RequestFingerprint != input.RequestFingerprint {
			return nil, nil, service.ErrVirtualCurrencyIdempotency
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		return hold, existing, nil
	} else if !errors.Is(lookupErr, sql.ErrNoRows) {
		return nil, nil, lookupErr
	}

	if hold.Status != service.VirtualCurrencyHoldStatusActive {
		return nil, nil, service.ErrVirtualCurrencyHoldState
	}
	now := time.Now().UTC()
	if input.Action == service.VirtualCurrencyEntryCommit && !hold.ExpiresAt.After(now) {
		return nil, nil, service.ErrVirtualCurrencyHoldExpired
	}
	if input.Action != service.VirtualCurrencyEntryCommit && input.Action != service.VirtualCurrencyEntryRelease {
		return nil, nil, service.ErrVirtualCurrencyInvalidInput
	}
	if input.Action == service.VirtualCurrencyEntryRelease && !hold.ExpiresAt.After(now) {
		entryType = service.VirtualCurrencyEntryExpire
		settledStatus = service.VirtualCurrencyHoldStatusExpired
	}

	// Releasing or expiring a hold returns the reserved amount to available
	// balance. A commit consumes the reserved amount without making it
	// available again.
	availableDelta := hold.AmountUnits
	reservedDelta := -hold.AmountUnits
	delta := int64(0)
	if input.Action == service.VirtualCurrencyEntryCommit {
		availableDelta = 0
		delta = -hold.AmountUnits
	}
	entry, err := r.applyCurrencyDeltaTx(ctx, tx, service.VirtualCurrencyDeltaInput{
		CurrencyID:                   hold.CurrencyID,
		UserID:                       hold.UserID,
		GroupID:                      dereferenceInt64(hold.GroupID),
		DeltaUnits:                   delta,
		AvailableDeltaUnits:          availableDelta,
		ReservedDeltaUnits:           reservedDelta,
		EntryType:                    entryType,
		SourceType:                   input.SourceType,
		SourceID:                     input.SourceID,
		IdempotencyKey:               input.IdempotencyKey,
		RequestFingerprint:           input.RequestFingerprint,
		Reason:                       input.Reason,
		Metadata:                     input.Metadata,
		AllowSettlementWithoutPolicy: true,
	})
	if err != nil {
		return nil, nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE virtual_currency_holds
SET status = $1, settled_at = NOW(), updated_at = NOW()
WHERE id = $2 AND user_id = $3 AND status = $4`, settledStatus, hold.ID, hold.UserID, service.VirtualCurrencyHoldStatusActive)
	if err != nil {
		return nil, nil, err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil {
		return nil, nil, affectedErr
	} else if affected != 1 {
		return nil, nil, service.ErrVirtualCurrencyHoldState
	}
	hold, err = r.getHold(ctx, tx, "WHERE h.id = $1 AND h.user_id = $2", hold.ID, hold.UserID)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return hold, entry, nil
}

func (r *virtualCurrencyRepository) GetHold(ctx context.Context, userID, holdID int64) (*service.VirtualCurrencyHold, error) {
	return r.getHold(ctx, r.db, "WHERE h.id = $1 AND h.user_id = $2", holdID, userID)
}

func (r *virtualCurrencyRepository) ListHolds(ctx context.Context, query service.VirtualCurrencyHoldQuery) ([]*service.VirtualCurrencyHold, *pagination.PaginationResult, error) {
	where := "WHERE h.user_id = $1"
	args := []any{query.UserID}
	if query.CurrencyCode != "" {
		where += " AND LOWER(c.code) = LOWER($2)"
		args = append(args, query.CurrencyCode)
	}
	if query.Status != "" {
		where += fmt.Sprintf(" AND h.status = $%d", len(args)+1)
		args = append(args, query.Status)
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM virtual_currency_holds h JOIN virtual_currencies c ON c.id = h.currency_id "+where, args...).Scan(&total); err != nil {
		return nil, nil, err
	}
	page := query.Page
	pageSize := query.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	dataArgs := append([]any{}, args...)
	limitPos := len(dataArgs) + 1
	offsetPos := len(dataArgs) + 2
	dataArgs = append(dataArgs, pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, virtualCurrencyHoldSelect+where+fmt.Sprintf(" ORDER BY h.created_at DESC, h.id DESC LIMIT $%d OFFSET $%d", limitPos, offsetPos), dataArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*service.VirtualCurrencyHold, 0)
	for rows.Next() {
		item, scanErr := scanVirtualCurrencyHold(rows)
		if scanErr != nil {
			return nil, nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	pages := int64(0)
	if total > 0 {
		pages = (total + int64(pageSize) - 1) / int64(pageSize)
	}
	return items, &pagination.PaginationResult{Total: total, Page: page, PageSize: pageSize, Pages: int(pages)}, nil
}

func (r *virtualCurrencyRepository) ListUserWallets(ctx context.Context, userID int64) ([]*service.VirtualCurrencyWallet, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT vc.id, vc.code, vc.name, vc.symbol, vc.scale,
       COALESCE(w.available_units, 0), COALESCE(w.reserved_units, 0),
       COALESCE(w.updated_at, vc.updated_at),
       COALESCE(ARRAY(
           SELECT DISTINCT ep.group_id
           FROM virtual_currency_group_policies ep
           JOIN groups eg ON eg.id = ep.group_id
           WHERE ep.currency_id = vc.id
             AND ep.enabled = TRUE
             AND eg.status = 'active'
             AND eg.deleted_at IS NULL
             AND (
               (eg.subscription_type = 'standard' AND (
                   NOT eg.is_exclusive
                   OR EXISTS (
                       SELECT 1 FROM user_allowed_groups euag
                       WHERE euag.user_id = $1 AND euag.group_id = eg.id
                   )
               ))
               OR (eg.subscription_type = 'subscription' AND EXISTS (
                   SELECT 1 FROM user_subscriptions eus
                   WHERE eus.user_id = $1
                     AND eus.group_id = eg.id
                     AND eus.status = 'active'
                     AND eus.deleted_at IS NULL
                     AND (eus.expires_at IS NULL OR eus.expires_at > NOW())
               ))
             )
           ORDER BY ep.group_id
       ), '{}'::BIGINT[])
FROM virtual_currencies vc
LEFT JOIN virtual_currency_wallets w
       ON w.currency_id = vc.id AND w.user_id = $1
WHERE vc.status = 'active'
  AND (
      w.id IS NOT NULL
      OR EXISTS (
          SELECT 1
          FROM virtual_currency_group_policies ep
          JOIN groups eg ON eg.id = ep.group_id
          WHERE ep.currency_id = vc.id
            AND ep.enabled = TRUE
            AND eg.status = 'active'
            AND eg.deleted_at IS NULL
            AND (
              (eg.subscription_type = 'standard' AND (
                  NOT eg.is_exclusive
                  OR EXISTS (
                      SELECT 1 FROM user_allowed_groups euag
                      WHERE euag.user_id = $1 AND euag.group_id = eg.id
                  )
              ))
              OR (eg.subscription_type = 'subscription' AND EXISTS (
                  SELECT 1 FROM user_subscriptions eus
                  WHERE eus.user_id = $1
                    AND eus.group_id = eg.id
                    AND eus.status = 'active'
                    AND eus.deleted_at IS NULL
                    AND (eus.expires_at IS NULL OR eus.expires_at > NOW())
              ))
            )
      )
  )
GROUP BY vc.id, vc.code, vc.name, vc.symbol, vc.scale, vc.updated_at,
         w.available_units, w.reserved_units, w.updated_at
ORDER BY vc.id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]*service.VirtualCurrencyWallet, 0)
	for rows.Next() {
		item := new(service.VirtualCurrencyWallet)
		if err := rows.Scan(
			&item.CurrencyID, &item.CurrencyCode, &item.CurrencyName, &item.CurrencySymbol, &item.CurrencyScale,
			&item.AvailableUnits, &item.ReservedUnits, &item.UpdatedAt, pq.Array(&item.GroupIDs),
		); err != nil {
			return nil, err
		}
		item.UpdatedAt = item.UpdatedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *virtualCurrencyRepository) ListLedger(ctx context.Context, query service.VirtualCurrencyLedgerQuery) ([]*service.VirtualCurrencyLedgerEntry, *pagination.PaginationResult, error) {
	where := `WHERE l.user_id = $1`
	args := []any{query.UserID}
	if query.CurrencyCode != "" {
		where += ` AND LOWER(c.code) = LOWER($2)`
		args = append(args, query.CurrencyCode)
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM virtual_currency_ledger_entries l
JOIN virtual_currencies c ON c.id = l.currency_id `+where, args...).Scan(&total); err != nil {
		return nil, nil, err
	}

	page := query.Page
	pageSize := query.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	dataArgs := append([]any{}, args...)
	limitPos := len(dataArgs) + 1
	offsetPos := len(dataArgs) + 2
	dataArgs = append(dataArgs, pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, `
SELECT l.id, l.journal_id, l.currency_id, c.code, c.name, c.symbol, c.scale, l.user_id, l.group_id,
       l.delta_units, l.available_delta_units, l.reserved_delta_units,
       l.available_after_units, l.reserved_after_units, l.entry_type, l.source_type,
       l.source_id, l.idempotency_key, l.request_fingerprint, l.reason, l.metadata,
       l.created_by, l.created_at
FROM virtual_currency_ledger_entries l
JOIN virtual_currencies c ON c.id = l.currency_id `+where+fmt.Sprintf(` ORDER BY l.created_at DESC, l.id DESC LIMIT $%d OFFSET $%d`, limitPos, offsetPos), dataArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]*service.VirtualCurrencyLedgerEntry, 0)
	for rows.Next() {
		item, scanErr := scanVirtualCurrencyLedger(rows)
		if scanErr != nil {
			return nil, nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	pages := int64(0)
	if total > 0 {
		pages = (total + int64(pageSize) - 1) / int64(pageSize)
	}
	return items, &pagination.PaginationResult{Total: total, Page: page, PageSize: pageSize, Pages: int(pages)}, nil
}

const virtualCurrencyReconciliationCTE = `
WITH latest AS (
    SELECT DISTINCT ON (currency_id, user_id)
           currency_id, user_id, available_after_units, reserved_after_units
    FROM virtual_currency_ledger_entries
    WHERE currency_id = $1
    ORDER BY currency_id, user_id, created_at DESC, id DESC
), recon AS (
    SELECT w.user_id,
           w.available_units,
           w.reserved_units,
           l.available_after_units,
           l.reserved_after_units,
           TRUE AS wallet_exists,
           (l.user_id IS NOT NULL) AS ledger_snapshot_found
    FROM virtual_currency_wallets w
    LEFT JOIN latest l ON l.currency_id = w.currency_id AND l.user_id = w.user_id
    WHERE w.currency_id = $1
    UNION ALL
    SELECT l.user_id,
           0,
           0,
           l.available_after_units,
           l.reserved_after_units,
           FALSE,
           TRUE
    FROM latest l
    LEFT JOIN virtual_currency_wallets w
      ON w.currency_id = l.currency_id AND w.user_id = l.user_id
    WHERE w.id IS NULL
)`

func (r *virtualCurrencyRepository) ReconcileCurrency(ctx context.Context, currencyID int64, sampleLimit int) (*service.VirtualCurrencyReconciliationReport, error) {
	var report service.VirtualCurrencyReconciliationReport
	if err := r.db.QueryRowContext(ctx, virtualCurrencyReconciliationCTE+`
SELECT
    COUNT(*) FILTER (WHERE wallet_exists),
    COUNT(*) FILTER (WHERE ledger_snapshot_found),
    COUNT(*) FILTER (
        WHERE NOT wallet_exists
           OR NOT ledger_snapshot_found
           OR available_units <> COALESCE(available_after_units, 0)
           OR reserved_units <> COALESCE(reserved_after_units, 0)
    )
FROM recon`, currencyID).Scan(&report.WalletCount, &report.LedgerUserCount, &report.MismatchCount); err != nil {
		return nil, err
	}
	report.CurrencyID = currencyID
	report.SampleLimit = sampleLimit
	report.CheckedAt = time.Now().UTC()
	if err := r.db.QueryRowContext(ctx, `
WITH wallet_totals AS (
    SELECT COALESCE(SUM(available_units), 0) AS available_units,
           COALESCE(SUM(reserved_units), 0) AS reserved_units
    FROM virtual_currency_wallets
    WHERE currency_id = $1
),
posting_totals AS (
    SELECT COUNT(*) AS posting_count,
           COALESCE(SUM(amount_units) FILTER (WHERE account_kind = 'user_available'), 0) AS user_available_units,
           COALESCE(SUM(amount_units) FILTER (WHERE account_kind = 'user_reserved'), 0) AS user_reserved_units,
           COALESCE(SUM(amount_units) FILTER (WHERE account_kind = 'system_issuance'), 0) AS issuance_units,
           COALESCE(SUM(amount_units) FILTER (WHERE account_kind = 'system_sink'), 0) AS sink_units,
           COALESCE(SUM(amount_units) FILTER (WHERE account_kind = 'system_adjustment'), 0) AS adjustment_units,
           COALESCE(SUM(amount_units), 0) AS conservation_delta_units
    FROM virtual_currency_postings
    WHERE currency_id = $1
),
journal_totals AS (
    SELECT COUNT(*) AS journal_count
    FROM virtual_currency_journals
    WHERE currency_id = $1
),
invalid_journal_totals AS (
    SELECT COUNT(*) AS invalid_journal_count
    FROM (
        SELECT j.id
        FROM virtual_currency_journals j
        LEFT JOIN virtual_currency_postings p ON p.journal_id = j.id
        WHERE j.currency_id = $1
        GROUP BY j.id, j.posted_at
        HAVING j.posted_at IS NULL OR COUNT(p.id) < 2 OR COALESCE(SUM(p.amount_units), 0) <> 0
    ) invalid
)
SELECT j.journal_count,
       p.posting_count,
       i.invalid_journal_count,
       w.available_units::TEXT,
       w.reserved_units::TEXT,
       p.user_available_units::TEXT,
       p.user_reserved_units::TEXT,
       (-p.issuance_units)::TEXT,
       p.sink_units::TEXT,
       (-p.adjustment_units)::TEXT,
       (w.available_units + w.reserved_units - p.user_available_units - p.user_reserved_units)::TEXT,
       p.conservation_delta_units::TEXT
FROM wallet_totals w
CROSS JOIN posting_totals p
CROSS JOIN journal_totals j
CROSS JOIN invalid_journal_totals i`, currencyID).Scan(
		&report.Accounting.JournalCount,
		&report.Accounting.PostingCount,
		&report.Accounting.InvalidJournalCount,
		&report.Accounting.WalletAvailableUnits,
		&report.Accounting.WalletReservedUnits,
		&report.Accounting.PostedUserAvailableUnits,
		&report.Accounting.PostedUserReservedUnits,
		&report.Accounting.GrossIssuedUnits,
		&report.Accounting.NetSinkUnits,
		&report.Accounting.NetAdjustmentUnits,
		&report.Accounting.ProjectionDeltaUnits,
		&report.Accounting.ConservationDeltaUnits,
	); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, virtualCurrencyReconciliationCTE+`
SELECT user_id, available_units, reserved_units,
       COALESCE(available_after_units, 0), COALESCE(reserved_after_units, 0),
       wallet_exists, ledger_snapshot_found
FROM recon
WHERE NOT wallet_exists
   OR NOT ledger_snapshot_found
   OR available_units <> COALESCE(available_after_units, 0)
   OR reserved_units <> COALESCE(reserved_after_units, 0)
ORDER BY user_id ASC
LIMIT $2`, currencyID, sampleLimit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	report.Mismatches = make([]*service.VirtualCurrencyReconciliationMismatch, 0)
	for rows.Next() {
		item := new(service.VirtualCurrencyReconciliationMismatch)
		if err := rows.Scan(&item.UserID, &item.WalletAvailable, &item.WalletReserved, &item.LedgerAvailable, &item.LedgerReserved, &item.WalletExists, &item.LedgerSnapshotFound); err != nil {
			return nil, err
		}
		report.Mismatches = append(report.Mismatches, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &report, nil
}

func (r *virtualCurrencyRepository) getCurrency(ctx context.Context, client virtualCurrencySQLExecutor, where string, arg any) (*service.VirtualCurrency, error) {
	item, err := r.scanCurrencyRow(virtualCurrencyQueryRow(ctx, client, `
SELECT id, code, name, symbol, description, scale, status, metadata, created_by, created_at, updated_at
FROM virtual_currencies `+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVirtualCurrencyNotFound
	}
	return item, err
}

func (r *virtualCurrencyRepository) getHold(ctx context.Context, client virtualCurrencySQLExecutor, where string, args ...any) (*service.VirtualCurrencyHold, error) {
	item, err := scanVirtualCurrencyHold(virtualCurrencyQueryRow(ctx, client, virtualCurrencyHoldSelect+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVirtualCurrencyHoldNotFound
	}
	return item, err
}

func (r *virtualCurrencyRepository) getHoldByIdempotency(ctx context.Context, client virtualCurrencySQLExecutor, currencyID, userID int64, key string) (*service.VirtualCurrencyHold, error) {
	return r.getHold(ctx, client, "WHERE h.currency_id = $1 AND h.user_id = $2 AND h.idempotency_key = $3", currencyID, userID, key)
}

func (r *virtualCurrencyRepository) scanCurrencyRow(row interface{ Scan(...any) error }) (*service.VirtualCurrency, error) {
	item := new(service.VirtualCurrency)
	var (
		metadata  []byte
		createdBy sql.NullInt64
	)
	if err := row.Scan(&item.ID, &item.Code, &item.Name, &item.Symbol, &item.Description, &item.Scale, &item.Status, &metadata, &createdBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.Metadata = decodeCurrencyMetadata(metadata)
	if createdBy.Valid {
		item.CreatedBy = &createdBy.Int64
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, nil
}

func scanVirtualCurrency(rows interface{ Scan(...any) error }) (*service.VirtualCurrency, error) {
	item := new(service.VirtualCurrency)
	var (
		metadata  []byte
		createdBy sql.NullInt64
	)
	if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Symbol, &item.Description, &item.Scale, &item.Status, &metadata, &createdBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.Metadata = decodeCurrencyMetadata(metadata)
	if createdBy.Valid {
		item.CreatedBy = &createdBy.Int64
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, nil
}

func scanVirtualCurrencyPolicy(row interface{ Scan(...any) error }) (*service.VirtualCurrencyGroupPolicy, error) {
	item := new(service.VirtualCurrencyGroupPolicy)
	var (
		maxBalance sql.NullInt64
		metadata   []byte
	)
	if err := row.Scan(&item.ID, &item.CurrencyID, &item.GroupID, &item.Enabled, &item.CanEarn, &item.CanSpend, &maxBalance, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.Metadata = decodeCurrencyMetadata(metadata)
	if maxBalance.Valid {
		item.MaxBalanceUnits = &maxBalance.Int64
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, nil
}

func scanVirtualCurrencyHold(row interface{ Scan(...any) error }) (*service.VirtualCurrencyHold, error) {
	item := new(service.VirtualCurrencyHold)
	var (
		groupID   sql.NullInt64
		sourceID  sql.NullString
		metadata  []byte
		settledAt sql.NullTime
	)
	if err := row.Scan(
		&item.ID, &item.CurrencyID, &item.CurrencyCode, &item.CurrencyName, &item.CurrencySymbol, &item.CurrencyScale,
		&item.UserID, &groupID, &item.AmountUnits, &item.Status, &item.SourceType, &sourceID, &item.IdempotencyKey,
		&item.ExpiresAt, &metadata, &item.CreatedAt, &item.UpdatedAt, &settledAt,
	); err != nil {
		return nil, err
	}
	if groupID.Valid {
		item.GroupID = &groupID.Int64
	}
	if sourceID.Valid {
		item.SourceID = &sourceID.String
	}
	item.Metadata = decodeCurrencyMetadata(metadata)
	if settledAt.Valid {
		settled := settledAt.Time.UTC()
		item.SettledAt = &settled
	}
	item.ExpiresAt = item.ExpiresAt.UTC()
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, nil
}

func scanPolicyRow(row interface{ Scan(...any) error }) (*service.VirtualCurrencyGroupPolicy, error) {
	return scanVirtualCurrencyPolicy(row)
}

func (r *virtualCurrencyRepository) getLedgerByIdempotency(ctx context.Context, client virtualCurrencySQLExecutor, currencyID, userID int64, key string) (*service.VirtualCurrencyLedgerEntry, error) {
	return scanVirtualCurrencyLedger(virtualCurrencyQueryRow(ctx, client, `
SELECT l.id, l.journal_id, l.currency_id, c.code, c.name, c.symbol, c.scale, l.user_id, l.group_id,
       l.delta_units, l.available_delta_units, l.reserved_delta_units,
       l.available_after_units, l.reserved_after_units, l.entry_type, l.source_type,
       l.source_id, l.idempotency_key, l.request_fingerprint, l.reason, l.metadata,
       l.created_by, l.created_at
FROM virtual_currency_ledger_entries l
JOIN virtual_currencies c ON c.id = l.currency_id
WHERE l.currency_id = $1 AND l.user_id = $2 AND l.idempotency_key = $3`, currencyID, userID, key))
}

func scanVirtualCurrencyLedger(row interface{ Scan(...any) error }) (*service.VirtualCurrencyLedgerEntry, error) {
	item := new(service.VirtualCurrencyLedgerEntry)
	var (
		groupID            sql.NullInt64
		sourceID           sql.NullString
		requestFingerprint string
		metadata           []byte
		createdBy          sql.NullInt64
	)
	if err := row.Scan(
		&item.ID, &item.JournalID, &item.CurrencyID, &item.CurrencyCode, &item.CurrencyName, &item.CurrencySymbol, &item.CurrencyScale,
		&item.UserID, &groupID, &item.DeltaUnits, &item.AvailableDeltaUnits, &item.ReservedDeltaUnits,
		&item.AvailableAfterUnits, &item.ReservedAfterUnits, &item.EntryType, &item.SourceType,
		&sourceID, &item.IdempotencyKey, &requestFingerprint, &item.Reason, &metadata, &createdBy, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	if groupID.Valid {
		item.GroupID = &groupID.Int64
	}
	if sourceID.Valid {
		item.SourceID = &sourceID.String
	}
	if createdBy.Valid {
		item.CreatedBy = &createdBy.Int64
	}
	item.Metadata = decodeCurrencyMetadata(metadata)
	item.RequestFingerprint = requestFingerprint
	item.CreatedAt = item.CreatedAt.UTC()
	return item, nil
}

func marshalCurrencyMetadata(metadata map[string]any) (string, error) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal virtual currency metadata: %w", err)
	}
	return string(encoded), nil
}

func decodeCurrencyMetadata(raw []byte) map[string]any {
	metadata := map[string]any{}
	if len(raw) == 0 {
		return metadata
	}
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata == nil {
		return map[string]any{}
	}
	return metadata
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func dereferenceInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func safeAddInt64(left, right int64) (int64, bool) {
	if right > 0 && left > int64(^uint64(0)>>1)-right {
		return 0, false
	}
	if right < 0 && left < -int64(^uint64(0)>>1)-1-right {
		return 0, false
	}
	return left + right, true
}

func isForeignKeyViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr != nil && string(pqErr.Code) == "23503"
}
