-- Keep the user-facing assigned-account usage view bounded to an individual
-- lease window without scanning a user's complete usage history.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_user_account_created_at
    ON usage_logs (user_id, account_id, created_at);
