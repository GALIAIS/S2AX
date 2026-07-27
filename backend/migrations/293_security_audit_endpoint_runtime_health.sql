-- Durable runtime health and circuit-breaker accounting for security audit endpoints.
-- This migration is additive and keeps the existing probe-facing columns compatible.

ALTER TABLE security_audit_endpoint_health
    ADD COLUMN IF NOT EXISTS request_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS success_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS timeout_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rate_limited_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS server_error_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS invalid_response_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS latency_sum_ms BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS latency_max_ms INTEGER NOT NULL DEFAULT 0;

ALTER TABLE security_audit_endpoint_health
    DROP CONSTRAINT IF EXISTS chk_security_audit_endpoint_runtime_counts;

ALTER TABLE security_audit_endpoint_health
    ADD CONSTRAINT chk_security_audit_endpoint_runtime_counts
    CHECK (
        request_count >= 0
        AND success_count >= 0
        AND success_count <= request_count
        AND timeout_count >= 0
        AND rate_limited_count >= 0
        AND server_error_count >= 0
        AND invalid_response_count >= 0
        AND latency_sum_ms >= 0
        AND latency_max_ms >= 0
    );

CREATE INDEX IF NOT EXISTS idx_security_audit_endpoint_breaker
    ON security_audit_endpoint_health(breaker_state, breaker_opened_at, endpoint_id);
