-- Make audit degradation explicit and queryable without treating an unavailable
-- detector as a policy violation.

ALTER TABLE prompt_audit_events
    ADD COLUMN IF NOT EXISTS evaluation_status VARCHAR(32) NOT NULL DEFAULT 'complete',
    ADD COLUMN IF NOT EXISTS failure_mode VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS failure_reason VARCHAR(64) NOT NULL DEFAULT '';

ALTER TABLE prompt_audit_events
    DROP CONSTRAINT IF EXISTS chk_prompt_audit_events_decision;
ALTER TABLE prompt_audit_events
    ADD CONSTRAINT chk_prompt_audit_events_decision
        CHECK (decision IN ('pass', 'flag', 'critical', 'degraded'));

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'chk_prompt_audit_events_evaluation_status'
    ) THEN
        ALTER TABLE prompt_audit_events
            ADD CONSTRAINT chk_prompt_audit_events_evaluation_status
            CHECK (evaluation_status IN ('complete', 'degraded', 'failed'));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'chk_prompt_audit_events_failure_mode'
    ) THEN
        ALTER TABLE prompt_audit_events
            ADD CONSTRAINT chk_prompt_audit_events_failure_mode
            CHECK (failure_mode IN ('', 'allow_and_record', 'block_and_record', 'fallback_local', 'degraded_observe'));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_evaluation_created
    ON prompt_audit_events(evaluation_status, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS prompt_audit_admission_counters (
    bucket_at       TIMESTAMPTZ NOT NULL,
    failure_reason  VARCHAR(64) NOT NULL,
    config_version BIGINT NOT NULL DEFAULT 1,
    group_id        BIGINT NOT NULL DEFAULT 0,
    count           BIGINT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bucket_at, failure_reason, config_version, group_id),
    CONSTRAINT chk_prompt_audit_admission_count CHECK (count >= 0)
);

CREATE INDEX IF NOT EXISTS idx_prompt_audit_admission_counters_updated
    ON prompt_audit_admission_counters(updated_at DESC);
