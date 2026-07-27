-- Async Prompt Audit jobs must execute with the endpoint, scanner, and
-- retention configuration that admitted the job. Keeping only the mutable
-- settings row causes queued work to silently change semantics after an
-- administrator edits the configuration.

CREATE TABLE IF NOT EXISTS prompt_audit_config_versions (
    config_version BIGINT PRIMARY KEY,
    config_json     JSONB NOT NULL,
    config_digest   VARCHAR(64) NOT NULL,
    created_by      BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_prompt_audit_config_version_positive
        CHECK (config_version >= 1),
    CONSTRAINT chk_prompt_audit_config_json_object
        CHECK (jsonb_typeof(config_json) = 'object'),
    CONSTRAINT chk_prompt_audit_config_digest
        CHECK (config_digest ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_prompt_audit_config_versions_created
    ON prompt_audit_config_versions(created_at DESC, config_version DESC);

CREATE OR REPLACE FUNCTION reject_prompt_audit_config_version_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'prompt audit configuration history is append-only';
END;
$$;

DROP TRIGGER IF EXISTS trg_prompt_audit_config_versions_immutable
    ON prompt_audit_config_versions;
CREATE TRIGGER trg_prompt_audit_config_versions_immutable
    BEFORE UPDATE OR DELETE ON prompt_audit_config_versions
    FOR EACH ROW EXECUTE FUNCTION reject_prompt_audit_config_version_mutation();

COMMENT ON TABLE prompt_audit_config_versions IS
    'Append-only encrypted configuration snapshots used by version-pinned asynchronous audit jobs.';
COMMENT ON COLUMN prompt_audit_config_versions.config_json IS
    'Canonical storage configuration; endpoint credentials remain AES ciphertext.';
