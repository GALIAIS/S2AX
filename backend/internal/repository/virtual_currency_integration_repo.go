package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type virtualCurrencyIntegrationRepository struct {
	db *sql.DB
}

func NewVirtualCurrencyIntegrationRepository(db *sql.DB) service.VirtualCurrencyIntegrationRepository {
	return &virtualCurrencyIntegrationRepository{db: db}
}

func (r *virtualCurrencyIntegrationRepository) List(ctx context.Context, includeDisabled bool) ([]*service.VirtualCurrencyIntegrationRecord, error) {
	query := `
SELECT id, code, name, secret_ciphertext, secret_hint, status, metadata, created_by, created_at, updated_at
FROM virtual_currency_integrations`
	args := []any{}
	if !includeDisabled {
		query += " WHERE status = $1"
		args = append(args, service.VirtualCurrencyIntegrationStatusActive)
	}
	query += " ORDER BY id ASC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*service.VirtualCurrencyIntegrationRecord, 0)
	for rows.Next() {
		item, scanErr := r.scanIntegrationRow(rows)
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

func (r *virtualCurrencyIntegrationRepository) GetByID(ctx context.Context, id int64) (*service.VirtualCurrencyIntegrationRecord, error) {
	return r.getIntegration(ctx, r.db, "WHERE id = $1", id)
}

func (r *virtualCurrencyIntegrationRepository) GetByCode(ctx context.Context, code string) (*service.VirtualCurrencyIntegrationRecord, error) {
	return r.getIntegration(ctx, r.db, "WHERE LOWER(code) = LOWER($1)", strings.TrimSpace(code))
}

func (r *virtualCurrencyIntegrationRepository) Create(ctx context.Context, input service.VirtualCurrencyIntegrationCreateRepositoryInput) (*service.VirtualCurrencyIntegrationRecord, error) {
	metadata, err := marshalCurrencyMetadata(input.Metadata)
	if err != nil {
		return nil, err
	}
	item, err := r.scanIntegrationRow(r.db.QueryRowContext(ctx, `
INSERT INTO virtual_currency_integrations
    (code, name, secret_ciphertext, secret_hint, status, metadata, created_by)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
RETURNING id, code, name, secret_ciphertext, secret_hint, status, metadata, created_by, created_at, updated_at`,
		input.Code, input.Name, input.SecretCiphertext, input.SecretHint,
		service.VirtualCurrencyIntegrationStatusActive, metadata, nullableInt64(input.CreatedBy)))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, service.ErrVirtualCurrencyIntegrationCodeExists
		}
		return nil, err
	}
	return item, nil
}

func (r *virtualCurrencyIntegrationRepository) Update(ctx context.Context, id int64, input service.VirtualCurrencyIntegrationUpdateInput) (*service.VirtualCurrencyIntegrationRecord, error) {
	var metadata any
	if input.Metadata != nil {
		encoded, err := marshalCurrencyMetadata(input.Metadata)
		if err != nil {
			return nil, err
		}
		metadata = encoded
	}
	item, err := r.scanIntegrationRow(r.db.QueryRowContext(ctx, `
UPDATE virtual_currency_integrations
SET name = COALESCE($2, name), metadata = COALESCE($3::jsonb, metadata), updated_at = NOW()
WHERE id = $1
RETURNING id, code, name, secret_ciphertext, secret_hint, status, metadata, created_by, created_at, updated_at`,
		id, nullableString(input.Name), metadata))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVirtualCurrencyIntegrationNotFound
	}
	return item, err
}

func (r *virtualCurrencyIntegrationRepository) SetStatus(ctx context.Context, id int64, status string) (*service.VirtualCurrencyIntegrationRecord, error) {
	item, err := r.scanIntegrationRow(r.db.QueryRowContext(ctx, `
UPDATE virtual_currency_integrations
SET status = $2, updated_at = NOW()
WHERE id = $1
RETURNING id, code, name, secret_ciphertext, secret_hint, status, metadata, created_by, created_at, updated_at`, id, status))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVirtualCurrencyIntegrationNotFound
	}
	return item, err
}

func (r *virtualCurrencyIntegrationRepository) RotateSecret(ctx context.Context, input service.VirtualCurrencyIntegrationRotateRepositoryInput) (*service.VirtualCurrencyIntegrationRecord, error) {
	item, err := r.scanIntegrationRow(r.db.QueryRowContext(ctx, `
UPDATE virtual_currency_integrations
SET secret_ciphertext = $2, secret_hint = $3, updated_at = NOW()
WHERE id = $1
RETURNING id, code, name, secret_ciphertext, secret_hint, status, metadata, created_by, created_at, updated_at`,
		input.ID, input.SecretCiphertext, input.SecretHint))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVirtualCurrencyIntegrationNotFound
	}
	return item, err
}

func (r *virtualCurrencyIntegrationRepository) ListScopes(ctx context.Context, integrationID int64) ([]*service.VirtualCurrencyIntegrationScope, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, integration_id, currency_id, group_id, enabled, can_earn, can_spend, can_settle, metadata, created_at, updated_at
FROM virtual_currency_integration_scopes
WHERE integration_id = $1
ORDER BY currency_id ASC, group_id ASC`, integrationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*service.VirtualCurrencyIntegrationScope, 0)
	for rows.Next() {
		item, scanErr := scanVirtualCurrencyIntegrationScope(rows)
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

func (r *virtualCurrencyIntegrationRepository) GetScope(ctx context.Context, integrationID, currencyID, groupID int64) (*service.VirtualCurrencyIntegrationScope, error) {
	item, err := scanVirtualCurrencyIntegrationScope(r.db.QueryRowContext(ctx, `
SELECT id, integration_id, currency_id, group_id, enabled, can_earn, can_spend, can_settle, metadata, created_at, updated_at
FROM virtual_currency_integration_scopes
WHERE integration_id = $1 AND currency_id = $2 AND group_id = $3`, integrationID, currencyID, groupID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVirtualCurrencyIntegrationNotFound
	}
	return item, err
}

func (r *virtualCurrencyIntegrationRepository) UpsertScope(ctx context.Context, input service.VirtualCurrencyIntegrationScopeInput) (*service.VirtualCurrencyIntegrationScope, error) {
	metadata, err := marshalCurrencyMetadata(input.Metadata)
	if err != nil {
		return nil, err
	}
	item, err := scanVirtualCurrencyIntegrationScope(r.db.QueryRowContext(ctx, `
INSERT INTO virtual_currency_integration_scopes
    (integration_id, currency_id, group_id, enabled, can_earn, can_spend, can_settle, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
ON CONFLICT (integration_id, currency_id, group_id) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    can_earn = EXCLUDED.can_earn,
    can_spend = EXCLUDED.can_spend,
    can_settle = EXCLUDED.can_settle,
    metadata = EXCLUDED.metadata,
    updated_at = NOW()
RETURNING id, integration_id, currency_id, group_id, enabled, can_earn, can_spend, can_settle, metadata, created_at, updated_at`,
		input.IntegrationID, input.CurrencyID, input.GroupID, input.Enabled, input.CanEarn, input.CanSpend, input.CanSettle, metadata))
	if err != nil {
		if isForeignKeyViolation(err) {
			return nil, service.ErrVirtualCurrencyIntegrationInvalid
		}
		return nil, err
	}
	return item, nil
}

func (r *virtualCurrencyIntegrationRepository) DeleteScope(ctx context.Context, integrationID, currencyID, groupID int64) error {
	result, err := r.db.ExecContext(ctx, `
DELETE FROM virtual_currency_integration_scopes
WHERE integration_id = $1 AND currency_id = $2 AND group_id = $3`, integrationID, currencyID, groupID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrVirtualCurrencyIntegrationNotFound
	}
	return nil
}

func (r *virtualCurrencyIntegrationRepository) getIntegration(ctx context.Context, client virtualCurrencySQLExecutor, where string, arg any) (*service.VirtualCurrencyIntegrationRecord, error) {
	item, err := r.scanIntegrationRow(virtualCurrencyQueryRow(ctx, client, `
SELECT id, code, name, secret_ciphertext, secret_hint, status, metadata, created_by, created_at, updated_at
FROM virtual_currency_integrations `+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVirtualCurrencyIntegrationNotFound
	}
	return item, err
}

func (r *virtualCurrencyIntegrationRepository) scanIntegrationRow(row interface{ Scan(...any) error }) (*service.VirtualCurrencyIntegrationRecord, error) {
	item := new(service.VirtualCurrencyIntegrationRecord)
	var (
		metadata  []byte
		createdBy sql.NullInt64
	)
	if err := row.Scan(&item.ID, &item.Code, &item.Name, &item.SecretCiphertext, &item.SecretHint, &item.Status, &metadata, &createdBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
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

func scanVirtualCurrencyIntegrationScope(row interface{ Scan(...any) error }) (*service.VirtualCurrencyIntegrationScope, error) {
	item := new(service.VirtualCurrencyIntegrationScope)
	var metadata []byte
	if err := row.Scan(&item.ID, &item.IntegrationID, &item.CurrencyID, &item.GroupID, &item.Enabled, &item.CanEarn, &item.CanSpend, &item.CanSettle, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.Metadata = decodeCurrencyMetadata(metadata)
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, nil
}
