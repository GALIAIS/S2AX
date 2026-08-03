-- Shared quota pools support independent short and long windows.
-- Migration 308 remains readable as a legacy single-window fallback; rows are
-- copied into the long window so existing configuration is not lost.
CREATE TABLE IF NOT EXISTS shared_quota_pool_windows (
    group_id                    BIGINT NOT NULL REFERENCES shared_quota_pools(group_id) ON DELETE CASCADE,
    window_key                  VARCHAR(32) NOT NULL,
    enabled                     BOOLEAN NOT NULL DEFAULT TRUE,
    window_seconds              INTEGER NOT NULL DEFAULT 18000 CHECK (window_seconds >= 300 AND window_seconds <= 2678400),
    capacity_usd                DECIMAL(20,10),
    reserve_ratio               DECIMAL(8,6) NOT NULL DEFAULT 0.15 CHECK (reserve_ratio >= 0 AND reserve_ratio < 1),
    soft_stop_ratio             DECIMAL(8,6) NOT NULL DEFAULT 0.85 CHECK (soft_stop_ratio > 0 AND soft_stop_ratio <= 1),
    hard_stop_ratio             DECIMAL(8,6) NOT NULL DEFAULT 0.95 CHECK (hard_stop_ratio > 0 AND hard_stop_ratio <= 1),
    upstream_capacity_usd       DECIMAL(20,10),
    upstream_utilization_percent DECIMAL(8,4) CHECK (upstream_utilization_percent >= 0 AND upstream_utilization_percent <= 100),
    window_start                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    window_end                  TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '5 hours'),
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, window_key),
    CONSTRAINT shared_quota_pool_windows_capacity_check CHECK (capacity_usd IS NULL OR capacity_usd > 0),
    CONSTRAINT shared_quota_pool_windows_key_format CHECK (window_key ~ '^[a-z][a-z0-9_-]{0,31}$'),
    CONSTRAINT shared_quota_pool_windows_ratio_order CHECK (soft_stop_ratio <= hard_stop_ratio),
    CONSTRAINT shared_quota_pool_windows_boundary_check CHECK (window_end > window_start)
);

INSERT INTO shared_quota_pool_windows (
    group_id, window_key, enabled, window_seconds, capacity_usd,
    reserve_ratio, soft_stop_ratio, hard_stop_ratio,
    upstream_capacity_usd, upstream_utilization_percent,
    window_start, window_end
)
SELECT
    group_id, 'long', enabled, window_seconds, capacity_usd,
    reserve_ratio, soft_stop_ratio, hard_stop_ratio,
    upstream_capacity_usd, upstream_utilization_percent,
    window_start, window_end
FROM shared_quota_pools
ON CONFLICT (group_id, window_key) DO NOTHING;

-- Existing single-window pools must expose the new short-window control without
-- inventing a 5-hour capacity. Administrators can set the capacity and enable
-- it explicitly; the legacy long window remains the only active window until
-- then.
INSERT INTO shared_quota_pool_windows (
    group_id, window_key, enabled, window_seconds, capacity_usd,
    reserve_ratio, soft_stop_ratio, hard_stop_ratio,
    window_start, window_end
)
SELECT
    group_id, 'short', FALSE, 18000, NULL,
    0.15, 0.85, 0.95,
    NOW(), NOW() + INTERVAL '5 hours'
FROM shared_quota_pools
ON CONFLICT (group_id, window_key) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_shared_quota_pool_windows_enabled
    ON shared_quota_pool_windows(group_id, enabled);
