-- Official Analytics credit calibration for shared quota pools.
-- The existing official snapshot remains the current projection; these
-- nullable columns keep the migration additive and preserve old percent-only
-- configurations.

ALTER TABLE shared_quota_pool_official_snapshots
    ADD COLUMN IF NOT EXISTS analytics_used_credits DECIMAL(20,6),
    ADD COLUMN IF NOT EXISTS analytics_input_tokens BIGINT,
    ADD COLUMN IF NOT EXISTS analytics_cached_input_tokens BIGINT,
    ADD COLUMN IF NOT EXISTS analytics_output_tokens BIGINT,
    ADD COLUMN IF NOT EXISTS analytics_total_tokens BIGINT,
    ADD COLUMN IF NOT EXISTS analytics_start_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS analytics_end_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS analytics_fetched_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS analytics_credits_per_usd DECIMAL(12,6),
    ADD COLUMN IF NOT EXISTS analytics_source VARCHAR(64),
    ADD COLUMN IF NOT EXISTS analytics_status VARCHAR(32),
    ADD COLUMN IF NOT EXISTS analytics_confidence DECIMAL(6,4),
    ADD COLUMN IF NOT EXISTS analytics_record_count INTEGER,
    ADD COLUMN IF NOT EXISTS baseline_used_credits DECIMAL(20,6),
    ADD COLUMN IF NOT EXISTS baseline_used_percent DECIMAL(8,4),
    ADD COLUMN IF NOT EXISTS baseline_captured_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS baseline_reset_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_shared_quota_pool_official_snapshots_analytics_fetched
    ON shared_quota_pool_official_snapshots(analytics_fetched_at);
