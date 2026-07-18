-- Signed game/mission integration credentials and least-privilege currency scopes.
-- Secrets are encrypted by the application before they reach this table.

CREATE TABLE IF NOT EXISTS virtual_currency_integrations (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    secret_ciphertext TEXT NOT NULL,
    secret_hint VARCHAR(8) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT virtual_currency_integration_status_check
        CHECK (status IN ('active', 'disabled')),
    CONSTRAINT virtual_currency_integration_code_check
        CHECK (code ~ '^[a-z][a-z0-9_-]{2,63}$'),
    CONSTRAINT virtual_currency_integration_secret_check
        CHECK (length(secret_ciphertext) > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_virtual_currency_integrations_code
    ON virtual_currency_integrations (LOWER(code));
CREATE INDEX IF NOT EXISTS idx_virtual_currency_integrations_status
    ON virtual_currency_integrations (status, id);

CREATE TABLE IF NOT EXISTS virtual_currency_integration_scopes (
    id BIGSERIAL PRIMARY KEY,
    integration_id BIGINT NOT NULL REFERENCES virtual_currency_integrations(id) ON DELETE CASCADE,
    currency_id BIGINT NOT NULL REFERENCES virtual_currencies(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    can_earn BOOLEAN NOT NULL DEFAULT FALSE,
    can_spend BOOLEAN NOT NULL DEFAULT FALSE,
    can_settle BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT virtual_currency_integration_scope_unique
        UNIQUE (integration_id, currency_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_virtual_currency_integration_scopes_lookup
    ON virtual_currency_integration_scopes (integration_id, currency_id, group_id, enabled);
