-- Keep shared-pool aggregation from scanning the complete usage log history.
-- This migration is intentionally non-transactional so PostgreSQL can build the
-- index concurrently in production.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_shared_quota_window
    ON usage_logs (group_id, created_at, user_id)
    WHERE subscription_id IS NOT NULL;
