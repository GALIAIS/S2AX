-- Official rolling-window quota synchronization for shared subscription pools.
-- Manual USD remains the default; official_percent is opt-in per window.

ALTER TABLE shared_quota_pools
    ADD COLUMN IF NOT EXISTS capacity_mode VARCHAR(32) NOT NULL DEFAULT 'manual_usd',
    ADD COLUMN IF NOT EXISTS upstream_account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL;

ALTER TABLE shared_quota_pool_windows
    ADD COLUMN IF NOT EXISTS capacity_mode VARCHAR(32) NOT NULL DEFAULT 'manual_usd',
    ADD COLUMN IF NOT EXISTS upstream_account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS shared_quota_pool_official_snapshots (
    group_id             BIGINT NOT NULL REFERENCES shared_quota_pools(group_id) ON DELETE CASCADE,
    window_key           VARCHAR(32) NOT NULL,
    account_id           BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    used_percent         DECIMAL(8,4) NOT NULL CHECK (used_percent >= 0 AND used_percent <= 100),
    limit_window_seconds BIGINT NOT NULL CHECK (limit_window_seconds > 0),
    reset_at             TIMESTAMPTZ,
    fetched_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, window_key)
);

CREATE INDEX IF NOT EXISTS idx_shared_quota_pool_official_snapshots_fetched
    ON shared_quota_pool_official_snapshots(fetched_at);
