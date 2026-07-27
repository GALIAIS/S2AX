-- Operational lifecycle for failed Prompt Audit jobs.
--
-- Failed jobs only contain redacted metadata in PostgreSQL. Their transient
-- input remains in Redis under the existing bounded TTL and is never copied to
-- this table. Operator retry is therefore possible only while that payload is
-- still available.

ALTER TABLE prompt_audit_jobs
    DROP CONSTRAINT IF EXISTS chk_prompt_audit_jobs_status;

ALTER TABLE prompt_audit_jobs
    ADD CONSTRAINT chk_prompt_audit_jobs_status
        CHECK (status IN (
            'staging', 'queued', 'processing', 'retry', 'done', 'failed',
            'quarantined', 'discarded'
        ));

CREATE TABLE IF NOT EXISTS prompt_audit_job_operations (
    id                BIGSERIAL PRIMARY KEY,
    job_id            BIGINT NOT NULL REFERENCES prompt_audit_jobs(id) ON DELETE CASCADE,
    operation         VARCHAR(32) NOT NULL,
    from_status       VARCHAR(32) NOT NULL,
    to_status         VARCHAR(32) NOT NULL,
    actor_id          BIGINT REFERENCES users(id) ON DELETE SET NULL,
    reason            VARCHAR(256) NOT NULL,
    payload_available BOOLEAN NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_prompt_audit_job_operations_operation
        CHECK (operation IN ('retry', 'quarantine', 'discard')),
    CONSTRAINT chk_prompt_audit_job_operations_status
        CHECK (
            from_status IN ('failed', 'quarantined')
            AND to_status IN ('queued', 'quarantined', 'discarded')
        ),
    CONSTRAINT chk_prompt_audit_job_operations_reason
        CHECK (char_length(btrim(reason)) BETWEEN 3 AND 256)
);

CREATE INDEX IF NOT EXISTS idx_prompt_audit_jobs_status_updated
    ON prompt_audit_jobs(status, updated_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_prompt_audit_jobs_error_updated
    ON prompt_audit_jobs(last_error_code, updated_at DESC, id DESC)
    WHERE status IN ('failed', 'quarantined');

CREATE INDEX IF NOT EXISTS idx_prompt_audit_job_operations_job_created
    ON prompt_audit_job_operations(job_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_prompt_audit_job_operations_actor_created
    ON prompt_audit_job_operations(actor_id, created_at DESC, id DESC);
