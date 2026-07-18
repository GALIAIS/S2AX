-- 可扩展虚拟货币核心表。
-- 与 users.balance（USD）完全隔离；金额使用最小单位 BIGINT 记账。
-- 迁移可重复执行，历史流水只追加不更新。

CREATE TABLE IF NOT EXISTS virtual_currencies (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(32) NOT NULL,
    name VARCHAR(64) NOT NULL,
    symbol VARCHAR(16) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    scale SMALLINT NOT NULL DEFAULT 0 CHECK (scale BETWEEN 0 AND 8),
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT virtual_currency_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_virtual_currencies_code
    ON virtual_currencies (LOWER(code));
CREATE INDEX IF NOT EXISTS idx_virtual_currencies_status
    ON virtual_currencies (status, id);

CREATE TABLE IF NOT EXISTS virtual_currency_group_policies (
    id BIGSERIAL PRIMARY KEY,
    currency_id BIGINT NOT NULL REFERENCES virtual_currencies(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    can_earn BOOLEAN NOT NULL DEFAULT TRUE,
    can_spend BOOLEAN NOT NULL DEFAULT TRUE,
    max_balance_units BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT virtual_currency_policy_max_balance_check
        CHECK (max_balance_units IS NULL OR max_balance_units > 0),
    CONSTRAINT virtual_currency_group_policy_unique
        UNIQUE (currency_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_virtual_currency_group_policies_group
    ON virtual_currency_group_policies (group_id, enabled);

CREATE TABLE IF NOT EXISTS virtual_currency_wallets (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    currency_id BIGINT NOT NULL REFERENCES virtual_currencies(id) ON DELETE RESTRICT,
    available_units BIGINT NOT NULL DEFAULT 0 CHECK (available_units >= 0),
    reserved_units BIGINT NOT NULL DEFAULT 0 CHECK (reserved_units >= 0),
    version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT virtual_currency_wallet_unique UNIQUE (user_id, currency_id)
);

CREATE INDEX IF NOT EXISTS idx_virtual_currency_wallets_currency
    ON virtual_currency_wallets (currency_id, available_units DESC);

CREATE TABLE IF NOT EXISTS virtual_currency_ledger_entries (
    id BIGSERIAL PRIMARY KEY,
    currency_id BIGINT NOT NULL REFERENCES virtual_currencies(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    delta_units BIGINT NOT NULL,
    available_delta_units BIGINT NOT NULL DEFAULT 0,
    reserved_delta_units BIGINT NOT NULL DEFAULT 0,
    available_after_units BIGINT NOT NULL CHECK (available_after_units >= 0),
    reserved_after_units BIGINT NOT NULL DEFAULT 0 CHECK (reserved_after_units >= 0),
    entry_type VARCHAR(24) NOT NULL,
    source_type VARCHAR(32) NOT NULL,
    source_id VARCHAR(128),
    idempotency_key VARCHAR(128) NOT NULL,
    request_fingerprint CHAR(64) NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT virtual_currency_ledger_idempotency_key_check
        CHECK (length(trim(idempotency_key)) BETWEEN 1 AND 128),
    CONSTRAINT virtual_currency_ledger_request_fingerprint_check
        CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT virtual_currency_ledger_delta_balance_check
        CHECK (delta_units = available_delta_units + reserved_delta_units),
    CONSTRAINT virtual_currency_ledger_entry_type_check
        CHECK (entry_type IN ('grant', 'spend', 'refund', 'adjustment', 'reserve', 'commit', 'release', 'expire')),
    CONSTRAINT virtual_currency_ledger_source_type_check
        CHECK (source_type ~ '^[a-z][a-z0-9_.-]{1,31}$'),
    CONSTRAINT virtual_currency_ledger_effect_check
        CHECK (delta_units <> 0 OR available_delta_units <> 0 OR reserved_delta_units <> 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_virtual_currency_ledger_idempotency
    ON virtual_currency_ledger_entries (currency_id, user_id, idempotency_key);
CREATE INDEX IF NOT EXISTS idx_virtual_currency_ledger_user_currency_created
    ON virtual_currency_ledger_entries (user_id, currency_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_virtual_currency_ledger_source
    ON virtual_currency_ledger_entries (source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_virtual_currency_ledger_group_created
    ON virtual_currency_ledger_entries (group_id, created_at DESC);

-- 预留表先随核心建立，第一阶段接口可不启用；后续游戏/订单接入直接复用。
CREATE TABLE IF NOT EXISTS virtual_currency_holds (
    id BIGSERIAL PRIMARY KEY,
    currency_id BIGINT NOT NULL REFERENCES virtual_currencies(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    amount_units BIGINT NOT NULL CHECK (amount_units > 0),
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    source_type VARCHAR(32) NOT NULL,
    source_id VARCHAR(128),
    idempotency_key VARCHAR(128) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at TIMESTAMPTZ,
    CONSTRAINT virtual_currency_hold_idempotency UNIQUE (currency_id, user_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_virtual_currency_holds_active_expiry
    ON virtual_currency_holds (status, expires_at);
CREATE INDEX IF NOT EXISTS idx_virtual_currency_holds_user
    ON virtual_currency_holds (user_id, currency_id, created_at DESC);
