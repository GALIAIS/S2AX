-- Unified security-audit control plane. Prompt-audit and legacy moderation stay
-- available as adapters while these tables become the durable decision,
-- enforcement and human-review source of truth.

CREATE TABLE IF NOT EXISTS security_audit_policy_versions (
    id                  BIGSERIAL PRIMARY KEY,
    policy_key          VARCHAR(96) NOT NULL,
    version             BIGINT NOT NULL,
    name                VARCHAR(160) NOT NULL,
    status              VARCHAR(24) NOT NULL DEFAULT 'draft',
    priority            INTEGER NOT NULL DEFAULT 100,
    config              JSONB NOT NULL DEFAULT '{}'::jsonb,
    config_digest       VARCHAR(64) NOT NULL,
    validation_errors   JSONB NOT NULL DEFAULT '[]'::jsonb,
    change_reason       VARCHAR(512) NOT NULL DEFAULT '',
    created_by          BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    validated_at        TIMESTAMPTZ,
    shadowed_at         TIMESTAMPTZ,
    activated_at        TIMESTAMPTZ,
    retired_at          TIMESTAMPTZ,
    CONSTRAINT uq_security_audit_policy_version UNIQUE (policy_key, version),
    CONSTRAINT chk_security_audit_policy_status
        CHECK (status IN ('draft', 'validated', 'shadow', 'active', 'retired')),
    CONSTRAINT chk_security_audit_policy_priority CHECK (priority BETWEEN -100000 AND 100000)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_security_audit_policy_active
    ON security_audit_policy_versions(policy_key)
    WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_security_audit_policy_status
    ON security_audit_policy_versions(status, priority DESC, policy_key, version DESC);

CREATE TABLE IF NOT EXISTS security_audit_decisions (
    id                      BIGSERIAL PRIMARY KEY,
    decision_id             VARCHAR(64) NOT NULL UNIQUE,
    audit_id                VARCHAR(64) NOT NULL,
    source_type             VARCHAR(32) NOT NULL,
    source_event_id         BIGINT,
    request_id              VARCHAR(128) NOT NULL DEFAULT '',
    stage                   VARCHAR(32) NOT NULL DEFAULT 'pre_request',
    user_id                 BIGINT REFERENCES users(id) ON DELETE SET NULL,
    user_snapshot           VARCHAR(320) NOT NULL DEFAULT '',
    api_key_id              BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    api_key_snapshot        VARCHAR(160) NOT NULL DEFAULT '',
    group_id                BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    group_snapshot          VARCHAR(160) NOT NULL DEFAULT '',
    provider                VARCHAR(64) NOT NULL DEFAULT '',
    endpoint                VARCHAR(256) NOT NULL DEFAULT '',
    protocol                VARCHAR(64) NOT NULL DEFAULT '',
    requested_model         VARCHAR(256) NOT NULL DEFAULT '',
    policy_key              VARCHAR(96) NOT NULL,
    policy_version          BIGINT NOT NULL,
    canonicalizer_version   VARCHAR(32) NOT NULL,
    evaluation_status       VARCHAR(24) NOT NULL,
    risk_level              VARCHAR(24) NOT NULL,
    request_action          VARCHAR(24) NOT NULL,
    failure_mode            VARCHAR(32) NOT NULL DEFAULT '',
    failure_reason          VARCHAR(96) NOT NULL DEFAULT '',
    prompt_hash             VARCHAR(64) NOT NULL DEFAULT '',
    redacted_preview        TEXT NOT NULL DEFAULT '',
    detector_summary        JSONB NOT NULL DEFAULT '[]'::jsonb,
    candidate_actions       JSONB NOT NULL DEFAULT '[]'::jsonb,
    decision_digest         VARCHAR(64) NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_security_audit_decision_source UNIQUE (source_type, source_event_id),
    CONSTRAINT chk_security_audit_decision_source
        CHECK (source_type IN ('prompt_audit', 'legacy_moderation', 'cyber_policy', 'behavior', 'manual')),
    CONSTRAINT chk_security_audit_evaluation_status
        CHECK (evaluation_status IN ('complete', 'degraded', 'failed')),
    CONSTRAINT chk_security_audit_risk_level
        CHECK (risk_level IN ('none', 'low', 'medium', 'high', 'critical')),
    CONSTRAINT chk_security_audit_request_action
        CHECK (request_action IN ('allow', 'warn', 'block')),
    CONSTRAINT chk_security_audit_failure_mode
        CHECK (failure_mode IN ('', 'allow_and_record', 'block_and_record', 'fallback_local', 'degraded_observe'))
);

CREATE INDEX IF NOT EXISTS idx_security_audit_decisions_created
    ON security_audit_decisions(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_security_audit_decisions_request
    ON security_audit_decisions(request_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_security_audit_decisions_user
    ON security_audit_decisions(user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_security_audit_decisions_api_key
    ON security_audit_decisions(api_key_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_security_audit_decisions_group
    ON security_audit_decisions(group_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_security_audit_decisions_risk
    ON security_audit_decisions(risk_level, request_action, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_security_audit_decisions_policy
    ON security_audit_decisions(policy_key, policy_version, created_at DESC);

CREATE TABLE IF NOT EXISTS security_audit_evidence (
    id                      BIGSERIAL PRIMARY KEY,
    decision_pk             BIGINT NOT NULL REFERENCES security_audit_decisions(id) ON DELETE CASCADE,
    detector_id             VARCHAR(96) NOT NULL,
    detector_version        VARCHAR(64) NOT NULL DEFAULT '',
    outcome                 VARCHAR(24) NOT NULL,
    category                VARCHAR(96) NOT NULL DEFAULT '',
    score                   DOUBLE PRECISION NOT NULL DEFAULT 0,
    severity                VARCHAR(24) NOT NULL DEFAULT 'none',
    safe_summary            TEXT NOT NULL DEFAULT '',
    evidence_digest         VARCHAR(64) NOT NULL DEFAULT '',
    latency_ms              INTEGER NOT NULL DEFAULT 0,
    error_code              VARCHAR(96) NOT NULL DEFAULT '',
    evidence_ciphertext     TEXT NOT NULL DEFAULT '',
    encryption_key_id       VARCHAR(96) NOT NULL DEFAULT '',
    expires_at              TIMESTAMPTZ,
    hold_until              TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_security_audit_evidence_outcome
        CHECK (outcome IN ('matched', 'clear', 'error', 'skipped')),
    CONSTRAINT chk_security_audit_evidence_score CHECK (score >= 0 AND score <= 1),
    CONSTRAINT chk_security_audit_evidence_latency CHECK (latency_ms >= 0)
);

CREATE INDEX IF NOT EXISTS idx_security_audit_evidence_decision
    ON security_audit_evidence(decision_pk, id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_security_audit_evidence_detector
    ON security_audit_evidence(decision_pk, detector_id, category);
CREATE INDEX IF NOT EXISTS idx_security_audit_evidence_expiry
    ON security_audit_evidence(expires_at, id)
    WHERE evidence_ciphertext <> '';

CREATE TABLE IF NOT EXISTS security_audit_actions (
    id                      BIGSERIAL PRIMARY KEY,
    action_id               VARCHAR(64) NOT NULL UNIQUE,
    decision_pk             BIGINT NOT NULL REFERENCES security_audit_decisions(id) ON DELETE CASCADE,
    action_type             VARCHAR(48) NOT NULL,
    subject_type            VARCHAR(32) NOT NULL,
    subject_id              BIGINT NOT NULL DEFAULT 0,
    status                  VARCHAR(24) NOT NULL DEFAULT 'pending',
    idempotency_key         VARCHAR(192) NOT NULL UNIQUE,
    policy_action_version   BIGINT NOT NULL DEFAULT 1,
    attempts                INTEGER NOT NULL DEFAULT 0,
    max_attempts            INTEGER NOT NULL DEFAULT 5,
    lease_owner             VARCHAR(128) NOT NULL DEFAULT '',
    lease_expires_at        TIMESTAMPTZ,
    next_attempt_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    before_snapshot         JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_snapshot          JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code              VARCHAR(96) NOT NULL DEFAULT '',
    error_message           VARCHAR(512) NOT NULL DEFAULT '',
    requested_by            BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at            TIMESTAMPTZ,
    cancelled_at            TIMESTAMPTZ,
    reverted_at             TIMESTAMPTZ,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_security_audit_action_type
        CHECK (action_type IN (
            'record_hash', 'quarantine_session', 'throttle_api_key',
            'pause_api_key', 'pause_user', 'notify_user', 'notify_admin',
            'open_case', 'release_session', 'resume_api_key', 'resume_user'
        )),
    CONSTRAINT chk_security_audit_action_subject
        CHECK (subject_type IN ('request', 'session', 'api_key', 'user', 'case')),
    CONSTRAINT chk_security_audit_action_status
        CHECK (status IN ('pending', 'processing', 'retry', 'succeeded', 'failed', 'cancelled', 'reverted')),
    CONSTRAINT chk_security_audit_action_attempts
        CHECK (attempts >= 0 AND max_attempts BETWEEN 1 AND 100)
);

CREATE INDEX IF NOT EXISTS idx_security_audit_actions_queue
    ON security_audit_actions(next_attempt_at, id)
    WHERE status IN ('pending', 'retry');
CREATE INDEX IF NOT EXISTS idx_security_audit_actions_decision
    ON security_audit_actions(decision_pk, created_at, id);
CREATE INDEX IF NOT EXISTS idx_security_audit_actions_subject
    ON security_audit_actions(subject_type, subject_id, created_at DESC);

CREATE TABLE IF NOT EXISTS security_audit_outbox (
    id                  BIGSERIAL PRIMARY KEY,
    event_id            VARCHAR(64) NOT NULL UNIQUE,
    action_id           BIGINT REFERENCES security_audit_actions(id) ON DELETE CASCADE,
    topic               VARCHAR(96) NOT NULL,
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    status              VARCHAR(24) NOT NULL DEFAULT 'pending',
    attempts            INTEGER NOT NULL DEFAULT 0,
    max_attempts        INTEGER NOT NULL DEFAULT 10,
    lease_owner         VARCHAR(128) NOT NULL DEFAULT '',
    lease_expires_at    TIMESTAMPTZ,
    next_attempt_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error          VARCHAR(512) NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at        TIMESTAMPTZ,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_security_audit_outbox_status
        CHECK (status IN ('pending', 'processing', 'retry', 'published', 'failed', 'discarded')),
    CONSTRAINT chk_security_audit_outbox_attempts
        CHECK (attempts >= 0 AND max_attempts BETWEEN 1 AND 100)
);

CREATE INDEX IF NOT EXISTS idx_security_audit_outbox_queue
    ON security_audit_outbox(next_attempt_at, id)
    WHERE status IN ('pending', 'retry');

CREATE TABLE IF NOT EXISTS security_audit_cases (
    id                  BIGSERIAL PRIMARY KEY,
    case_id             VARCHAR(64) NOT NULL UNIQUE,
    primary_decision_pk BIGINT REFERENCES security_audit_decisions(id) ON DELETE SET NULL,
    title               VARCHAR(240) NOT NULL,
    severity            VARCHAR(24) NOT NULL,
    status              VARCHAR(32) NOT NULL DEFAULT 'open',
    assignee_id         BIGINT REFERENCES users(id) ON DELETE SET NULL,
    opened_reason       VARCHAR(512) NOT NULL DEFAULT '',
    resolution          VARCHAR(32) NOT NULL DEFAULT '',
    resolution_note     TEXT NOT NULL DEFAULT '',
    labels              JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by          BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at         TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ,
    CONSTRAINT chk_security_audit_case_severity
        CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    CONSTRAINT chk_security_audit_case_status
        CHECK (status IN ('open', 'reviewing', 'confirmed', 'false_positive', 'dismissed', 'expired')),
    CONSTRAINT chk_security_audit_case_resolution
        CHECK (resolution IN ('', 'confirmed', 'false_positive', 'dismissed', 'expired'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_security_audit_case_open_decision
    ON security_audit_cases(primary_decision_pk)
    WHERE status IN ('open', 'reviewing');
CREATE INDEX IF NOT EXISTS idx_security_audit_cases_queue
    ON security_audit_cases(status, severity, updated_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS security_audit_case_events (
    id              BIGSERIAL PRIMARY KEY,
    case_pk         BIGINT NOT NULL REFERENCES security_audit_cases(id) ON DELETE CASCADE,
    event_type      VARCHAR(48) NOT NULL,
    actor_id        BIGINT REFERENCES users(id) ON DELETE SET NULL,
    summary         VARCHAR(512) NOT NULL,
    details         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_security_audit_case_events_timeline
    ON security_audit_case_events(case_pk, created_at, id);

CREATE TABLE IF NOT EXISTS security_audit_exceptions (
    id                  BIGSERIAL PRIMARY KEY,
    exception_id        VARCHAR(64) NOT NULL UNIQUE,
    name                VARCHAR(160) NOT NULL,
    scope_type          VARCHAR(32) NOT NULL,
    scope_id            VARCHAR(256) NOT NULL,
    detector_id         VARCHAR(96) NOT NULL DEFAULT '',
    category            VARCHAR(96) NOT NULL DEFAULT '',
    effect              VARCHAR(32) NOT NULL DEFAULT 'allow_and_record',
    reason              VARCHAR(512) NOT NULL,
    status              VARCHAR(24) NOT NULL DEFAULT 'active',
    starts_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at          TIMESTAMPTZ,
    permanent           BOOLEAN NOT NULL DEFAULT FALSE,
    created_by          BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expired_at          TIMESTAMPTZ,
    revoked_by          BIGINT CONSTRAINT fk_security_audit_exception_revoked_by
                        REFERENCES users(id) ON DELETE SET NULL,
    revoked_reason      VARCHAR(512) NOT NULL DEFAULT '',
    CONSTRAINT chk_security_audit_exception_scope
        CHECK (scope_type IN ('user', 'api_key', 'group', 'model', 'endpoint', 'detector', 'category')),
    CONSTRAINT chk_security_audit_exception_effect
        CHECK (effect IN ('allow_and_record', 'warn_only', 'skip_detector')),
    CONSTRAINT chk_security_audit_exception_status
        CHECK (status IN ('active', 'expired', 'revoked')),
    CONSTRAINT chk_security_audit_exception_revocation
        CHECK (status <> 'revoked' OR (expired_at IS NOT NULL AND revoked_reason <> '')),
    CONSTRAINT chk_security_audit_exception_expiry
        CHECK (
            (permanent = TRUE AND expires_at IS NULL)
            OR (permanent = FALSE AND expires_at IS NOT NULL AND expires_at > starts_at)
        )
);

CREATE INDEX IF NOT EXISTS idx_security_audit_exceptions_active
    ON security_audit_exceptions(scope_type, scope_id, starts_at, expires_at)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS security_audit_feedback (
    id                  BIGSERIAL PRIMARY KEY,
    feedback_id         VARCHAR(64) NOT NULL UNIQUE,
    decision_pk         BIGINT NOT NULL REFERENCES security_audit_decisions(id) ON DELETE CASCADE,
    case_pk             BIGINT REFERENCES security_audit_cases(id) ON DELETE SET NULL,
    conclusion          VARCHAR(32) NOT NULL,
    corrected_category  VARCHAR(96) NOT NULL DEFAULT '',
    note                TEXT NOT NULL DEFAULT '',
    policy_key          VARCHAR(96) NOT NULL,
    policy_version      BIGINT NOT NULL,
    detector_snapshot   JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by          BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_security_audit_feedback_conclusion
        CHECK (conclusion IN ('confirmed', 'false_positive', 'false_negative', 'needs_more_info'))
);

CREATE INDEX IF NOT EXISTS idx_security_audit_feedback_decision
    ON security_audit_feedback(decision_pk, created_at DESC);

CREATE TABLE IF NOT EXISTS security_audit_endpoint_health (
    endpoint_id         VARCHAR(96) PRIMARY KEY,
    network_scope       VARCHAR(32) NOT NULL DEFAULT 'public_https',
    status              VARCHAR(24) NOT NULL DEFAULT 'unknown',
    breaker_state       VARCHAR(24) NOT NULL DEFAULT 'closed',
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    latency_ms          INTEGER NOT NULL DEFAULT 0,
    http_status         INTEGER NOT NULL DEFAULT 0,
    error_code          VARCHAR(96) NOT NULL DEFAULT '',
    checked_at          TIMESTAMPTZ,
    breaker_opened_at   TIMESTAMPTZ,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_security_audit_endpoint_status
        CHECK (status IN ('unknown', 'healthy', 'degraded', 'unhealthy')),
    CONSTRAINT chk_security_audit_breaker_state
        CHECK (breaker_state IN ('closed', 'open', 'half_open')),
    CONSTRAINT chk_security_audit_endpoint_failures CHECK (consecutive_failures >= 0)
);

CREATE TABLE IF NOT EXISTS security_audit_evidence_access_logs (
    id              BIGSERIAL PRIMARY KEY,
    decision_pk     BIGINT NOT NULL REFERENCES security_audit_decisions(id) ON DELETE CASCADE,
    admin_id        BIGINT REFERENCES users(id) ON DELETE SET NULL,
    reason          VARCHAR(256) NOT NULL,
    outcome         VARCHAR(32) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_security_audit_evidence_access_outcome
        CHECK (outcome IN ('revealed', 'unavailable', 'expired', 'decrypt_failed'))
);

CREATE INDEX IF NOT EXISTS idx_security_audit_evidence_access_decision
    ON security_audit_evidence_access_logs(decision_pk, created_at DESC, id DESC);
