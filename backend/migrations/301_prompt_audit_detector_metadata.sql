-- Preserve detector protocol and provider trace metadata on every persisted
-- Prompt Audit event. The model value itself remains available only in the
-- encrypted configuration; events keep a one-way digest for incident
-- correlation without expanding credential/configuration exposure.

ALTER TABLE prompt_audit_events
    ADD COLUMN IF NOT EXISTS detector_adapter VARCHAR(64) NOT NULL DEFAULT 'qwen3guard_chat',
    ADD COLUMN IF NOT EXISTS provider_request_id VARCHAR(160) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS finish_reason VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS model_digest VARCHAR(64) NOT NULL DEFAULT '';

UPDATE prompt_audit_events
SET detector_adapter = CASE
    WHEN scanner_backend = 'openai-moderations' THEN 'openai_moderations'
    WHEN scanner_backend = 'strict-json-chat' THEN 'strict_json_chat'
    ELSE 'qwen3guard_chat'
END
WHERE scanner_backend IN ('openai-moderations', 'strict-json-chat', 'qwen3guard-openai');

ALTER TABLE prompt_audit_events
    DROP CONSTRAINT IF EXISTS chk_prompt_audit_events_detector_adapter;
ALTER TABLE prompt_audit_events
    ADD CONSTRAINT chk_prompt_audit_events_detector_adapter
        CHECK (detector_adapter IN ('qwen3guard_chat', 'openai_moderations', 'strict_json_chat'));

ALTER TABLE prompt_audit_events
    DROP CONSTRAINT IF EXISTS chk_prompt_audit_events_model_digest;
ALTER TABLE prompt_audit_events
    ADD CONSTRAINT chk_prompt_audit_events_model_digest
        CHECK (model_digest = '' OR model_digest ~ '^[0-9a-f]{64}$');

CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_provider_request
    ON prompt_audit_events(provider_request_id)
    WHERE provider_request_id <> '';

COMMENT ON COLUMN prompt_audit_events.detector_adapter IS
    'Immutable adapter contract used to normalize the provider response.';
COMMENT ON COLUMN prompt_audit_events.provider_request_id IS
    'Bounded provider trace identifier from the response header or envelope.';
COMMENT ON COLUMN prompt_audit_events.model_digest IS
    'SHA-256 digest of the model identifier returned by the detector.';
