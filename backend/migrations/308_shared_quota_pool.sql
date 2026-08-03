-- Shared subscription quota pools.
-- The feature is opt-in; existing groups keep the legacy per-subscription limits.

CREATE TABLE IF NOT EXISTS shared_quota_pools (
    group_id                    BIGINT PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
    enabled                     BOOLEAN NOT NULL DEFAULT FALSE,
    window_seconds              INTEGER NOT NULL DEFAULT 604800,
    capacity_usd                DECIMAL(20,10),
    reserve_ratio               DECIMAL(8,6) NOT NULL DEFAULT 0.15,
    soft_stop_ratio             DECIMAL(8,6) NOT NULL DEFAULT 0.85,
    hard_stop_ratio             DECIMAL(8,6) NOT NULL DEFAULT 0.95,
    borrow_enabled              BOOLEAN NOT NULL DEFAULT TRUE,
    borrow_multiplier           DECIMAL(8,4) NOT NULL DEFAULT 1.5,
    upstream_capacity_usd       DECIMAL(20,10),
    upstream_utilization_percent DECIMAL(8,4),
    window_start                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    window_end                  TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '7 days'),
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT shared_quota_pools_window_seconds_check
        CHECK (window_seconds BETWEEN 3600 AND 2678400),
    CONSTRAINT shared_quota_pools_capacity_check
        CHECK (capacity_usd IS NULL OR capacity_usd > 0),
    CONSTRAINT shared_quota_pools_reserve_check
        CHECK (reserve_ratio >= 0 AND reserve_ratio < 1),
    CONSTRAINT shared_quota_pools_soft_stop_check
        CHECK (soft_stop_ratio > 0 AND soft_stop_ratio <= 1),
    CONSTRAINT shared_quota_pools_hard_stop_check
        CHECK (hard_stop_ratio > 0 AND hard_stop_ratio <= 1),
    CONSTRAINT shared_quota_pools_stop_order_check
        CHECK (soft_stop_ratio <= hard_stop_ratio),
    CONSTRAINT shared_quota_pools_borrow_multiplier_check
        CHECK (borrow_multiplier >= 1 AND borrow_multiplier <= 10),
    CONSTRAINT shared_quota_pools_upstream_utilization_check
        CHECK (upstream_utilization_percent IS NULL OR upstream_utilization_percent >= 0 AND upstream_utilization_percent <= 100)
);

CREATE TABLE IF NOT EXISTS shared_quota_pool_members (
    group_id       BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    weight         DECIMAL(12,6) NOT NULL DEFAULT 1,
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, user_id),
    CONSTRAINT shared_quota_pool_members_weight_check CHECK (weight > 0 AND weight <= 100000)
);

CREATE INDEX IF NOT EXISTS idx_shared_quota_pool_members_user
    ON shared_quota_pool_members(user_id);
