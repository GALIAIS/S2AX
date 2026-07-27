-- Protect prompt-audit source evidence at rest and record every reveal.
-- The legacy full_prompt column is retained temporarily so application startup
-- can encrypt old rows with the deployment key before clearing the plaintext.

ALTER TABLE prompt_audit_events
    ADD COLUMN IF NOT EXISTS evidence_ciphertext TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS evidence_status VARCHAR(32) NOT NULL DEFAULT 'not_stored',
    ADD COLUMN IF NOT EXISTS evidence_expires_at TIMESTAMPTZ;

UPDATE prompt_audit_events
SET evidence_status = 'legacy_plaintext'
WHERE full_prompt <> ''
  AND evidence_ciphertext = ''
  AND evidence_status = 'not_stored';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_prompt_audit_events_evidence_status'
    ) THEN
        ALTER TABLE prompt_audit_events
            ADD CONSTRAINT chk_prompt_audit_events_evidence_status
            CHECK (evidence_status IN (
                'not_stored',
                'encrypted',
                'expired',
                'encryption_failed',
                'legacy_plaintext'
            ));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_evidence_expiry
    ON prompt_audit_events(evidence_expires_at, id)
    WHERE evidence_ciphertext <> '';

CREATE TABLE IF NOT EXISTS prompt_audit_evidence_access_logs (
    id          BIGSERIAL PRIMARY KEY,
    event_id    BIGINT NOT NULL,
    admin_id    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    reason      VARCHAR(256) NOT NULL,
    outcome     VARCHAR(32) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_prompt_audit_evidence_access_outcome
        CHECK (outcome IN ('revealed', 'unavailable', 'expired', 'decrypt_failed'))
);

CREATE INDEX IF NOT EXISTS idx_prompt_audit_evidence_access_event
    ON prompt_audit_evidence_access_logs(event_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_evidence_access_admin
    ON prompt_audit_evidence_access_logs(admin_id, created_at DESC, id DESC);
