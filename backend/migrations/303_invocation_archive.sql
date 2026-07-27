-- Invocation Archive is deliberately opt-in. Gateway payloads never enter this
-- schema until an administrator enables a matching policy rule.

CREATE TABLE IF NOT EXISTS invocation_archive_config_versions (
    config_version BIGINT PRIMARY KEY,
    config_json    JSONB NOT NULL,
    config_digest  VARCHAR(64) NOT NULL,
    created_by     BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_invocation_archive_config_version_positive
        CHECK (config_version >= 1),
    CONSTRAINT chk_invocation_archive_config_json_object
        CHECK (jsonb_typeof(config_json) = 'object'),
    CONSTRAINT chk_invocation_archive_config_digest
        CHECK (config_digest ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_invocation_archive_config_versions_created
    ON invocation_archive_config_versions(created_at DESC, config_version DESC);

CREATE OR REPLACE FUNCTION reject_invocation_archive_config_version_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'invocation archive configuration history is append-only';
END;
$$;

DROP TRIGGER IF EXISTS trg_invocation_archive_config_versions_immutable
    ON invocation_archive_config_versions;
CREATE TRIGGER trg_invocation_archive_config_versions_immutable
    BEFORE UPDATE OR DELETE ON invocation_archive_config_versions
    FOR EACH ROW EXECUTE FUNCTION reject_invocation_archive_config_version_mutation();

CREATE TABLE IF NOT EXISTS invocation_archive_records (
    id                       BIGSERIAL PRIMARY KEY,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at               TIMESTAMPTZ NOT NULL,
    config_version           BIGINT NOT NULL,
    mode                     VARCHAR(16) NOT NULL,
    transport                VARCHAR(16) NOT NULL DEFAULT 'http',
    websocket_turn           INT NOT NULL DEFAULT 0,

    user_id                  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    user_label               VARCHAR(255) NOT NULL DEFAULT '',
    api_key_id               BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    api_key_name             VARCHAR(255) NOT NULL DEFAULT '',
    group_id                 BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    group_name               VARCHAR(255) NOT NULL DEFAULT '',

    request_id               VARCHAR(128) NOT NULL DEFAULT '',
    client_request_id        VARCHAR(128) NOT NULL DEFAULT '',
    method                   VARCHAR(16) NOT NULL DEFAULT '',
    request_path             VARCHAR(512) NOT NULL DEFAULT '',
    model                    VARCHAR(255) NOT NULL DEFAULT '',
    client_ip                VARCHAR(64) NOT NULL DEFAULT '',
    user_agent               VARCHAR(512) NOT NULL DEFAULT '',
    request_content_type     VARCHAR(255) NOT NULL DEFAULT '',
    response_content_type    VARCHAR(255) NOT NULL DEFAULT '',
    http_status              INT NOT NULL DEFAULT 0,

    request_total_bytes      BIGINT NOT NULL DEFAULT 0,
    request_captured_bytes   BIGINT NOT NULL DEFAULT 0,
    request_truncated        BOOLEAN NOT NULL DEFAULT FALSE,
    request_status           VARCHAR(32) NOT NULL DEFAULT 'empty',
    request_ciphertext       TEXT NOT NULL DEFAULT '',

    response_total_bytes     BIGINT NOT NULL DEFAULT 0,
    response_captured_bytes  BIGINT NOT NULL DEFAULT 0,
    response_truncated       BOOLEAN NOT NULL DEFAULT FALSE,
    response_status          VARCHAR(32) NOT NULL DEFAULT 'empty',
    response_ciphertext      TEXT NOT NULL DEFAULT '',
    outcome                  VARCHAR(32) NOT NULL DEFAULT 'completed',

    CONSTRAINT chk_invocation_archive_record_config_version
        CHECK (config_version >= 1),
    CONSTRAINT chk_invocation_archive_record_mode
        CHECK (mode IN ('request_only', 'full')),
    CONSTRAINT chk_invocation_archive_record_transport
        CHECK (transport IN ('http', 'websocket')),
    CONSTRAINT chk_invocation_archive_record_websocket_turn
        CHECK (websocket_turn >= 0),
    CONSTRAINT chk_invocation_archive_record_expiry
        CHECK (expires_at > created_at),
    CONSTRAINT chk_invocation_archive_request_bytes
        CHECK (request_total_bytes >= 0 AND request_captured_bytes >= 0 AND request_captured_bytes <= request_total_bytes),
    CONSTRAINT chk_invocation_archive_response_bytes
        CHECK (response_total_bytes >= 0 AND response_captured_bytes >= 0 AND response_captured_bytes <= response_total_bytes),
    CONSTRAINT chk_invocation_archive_request_status
        CHECK (request_status IN ('captured', 'empty', 'not_read', 'omitted', 'encryption_failed')),
    CONSTRAINT chk_invocation_archive_response_status
        CHECK (response_status IN ('captured', 'empty', 'not_read', 'omitted', 'encryption_failed')),
    CONSTRAINT chk_invocation_archive_outcome
        CHECK (outcome IN ('completed', 'client_error', 'server_error', 'websocket_error'))
);

CREATE INDEX IF NOT EXISTS idx_invocation_archive_records_created
    ON invocation_archive_records(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_invocation_archive_records_expires
    ON invocation_archive_records(expires_at ASC, id ASC);
CREATE INDEX IF NOT EXISTS idx_invocation_archive_records_user_created
    ON invocation_archive_records(user_id, created_at DESC) WHERE user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_invocation_archive_records_api_key_created
    ON invocation_archive_records(api_key_id, created_at DESC) WHERE api_key_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_invocation_archive_records_group_created
    ON invocation_archive_records(group_id, created_at DESC) WHERE group_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_invocation_archive_records_request_id
    ON invocation_archive_records(request_id) WHERE request_id <> '';
CREATE INDEX IF NOT EXISTS idx_invocation_archive_records_client_request_id
    ON invocation_archive_records(client_request_id) WHERE client_request_id <> '';

CREATE TABLE IF NOT EXISTS invocation_archive_access_logs (
    id          BIGSERIAL PRIMARY KEY,
    record_id   BIGINT NOT NULL,
    admin_id    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    reason      VARCHAR(256) NOT NULL DEFAULT '',
    outcome     VARCHAR(64) NOT NULL,
    client_ip   VARCHAR(64) NOT NULL DEFAULT '',
    user_agent  VARCHAR(512) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_invocation_archive_access_outcome
        CHECK (outcome IN ('revealed', 'direct_view_disabled', 'expired', 'unavailable', 'decrypt_failed'))
);

CREATE INDEX IF NOT EXISTS idx_invocation_archive_access_record_created
    ON invocation_archive_access_logs(record_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_invocation_archive_access_admin_created
    ON invocation_archive_access_logs(admin_id, created_at DESC) WHERE admin_id IS NOT NULL;

COMMENT ON TABLE invocation_archive_records IS
    'Opt-in encrypted gateway request/response archive. No headers, credentials, or upstream secrets are stored.';
COMMENT ON COLUMN invocation_archive_records.request_ciphertext IS
    'AES-GCM ciphertext containing a bounded client-visible request payload envelope.';
COMMENT ON COLUMN invocation_archive_records.response_ciphertext IS
    'AES-GCM ciphertext containing a bounded client-visible response payload envelope.';
COMMENT ON TABLE invocation_archive_access_logs IS
    'Append-only direct-view access evidence retained independently after the archived record is removed.';
