-- AI chat-completions 审核原因，用于风控记录复核与误判分析。
ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS reason TEXT NOT NULL DEFAULT '';

ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS audit_endpoint_type VARCHAR(32) NOT NULL DEFAULT '';

ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS audit_model VARCHAR(255) NOT NULL DEFAULT '';
