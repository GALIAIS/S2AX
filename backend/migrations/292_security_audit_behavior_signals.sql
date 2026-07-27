-- Durable behavior, usage, cost and error signals for the unified security-audit
-- control plane. Raw request payloads and IP addresses are deliberately excluded:
-- only bounded aggregates are retained.

CREATE TABLE IF NOT EXISTS security_audit_signal_windows (
    id                      BIGSERIAL PRIMARY KEY,
    bucket_start            TIMESTAMPTZ NOT NULL,
    bucket_seconds          INTEGER NOT NULL DEFAULT 60,
    subject_type            VARCHAR(24) NOT NULL,
    subject_id              BIGINT NOT NULL,
    user_id                 BIGINT REFERENCES users(id) ON DELETE SET NULL,
    api_key_id              BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    group_id                BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    subject_snapshot        VARCHAR(320) NOT NULL DEFAULT '',
    request_count           BIGINT NOT NULL DEFAULT 0,
    success_count           BIGINT NOT NULL DEFAULT 0,
    error_count             BIGINT NOT NULL DEFAULT 0,
    business_limited_count  BIGINT NOT NULL DEFAULT 0,
    token_count             BIGINT NOT NULL DEFAULT 0,
    actual_cost             NUMERIC(24, 10) NOT NULL DEFAULT 0,
    duration_sum_ms         BIGINT NOT NULL DEFAULT 0,
    duration_sample_count   BIGINT NOT NULL DEFAULT 0,
    duration_max_ms         INTEGER NOT NULL DEFAULT 0,
    distinct_ip_count       INTEGER NOT NULL DEFAULT 0,
    distinct_model_count    INTEGER NOT NULL DEFAULT 0,
    computed_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_security_audit_signal_window
        UNIQUE (bucket_start, bucket_seconds, subject_type, subject_id),
    CONSTRAINT chk_security_audit_signal_subject
        CHECK (subject_type IN ('user', 'api_key', 'group')),
    CONSTRAINT chk_security_audit_signal_bucket
        CHECK (bucket_seconds BETWEEN 60 AND 86400),
    CONSTRAINT chk_security_audit_signal_nonnegative
        CHECK (
            request_count >= 0
            AND success_count >= 0
            AND error_count >= 0
            AND business_limited_count >= 0
            AND token_count >= 0
            AND actual_cost >= 0
            AND duration_sum_ms >= 0
            AND duration_sample_count >= 0
            AND duration_max_ms >= 0
            AND distinct_ip_count >= 0
            AND distinct_model_count >= 0
        )
);

CREATE INDEX IF NOT EXISTS idx_security_audit_signal_subject_window
    ON security_audit_signal_windows(subject_type, subject_id, bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_security_audit_signal_bucket
    ON security_audit_signal_windows(bucket_start DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_security_audit_signal_user
    ON security_audit_signal_windows(user_id, bucket_start DESC)
    WHERE user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_security_audit_signal_api_key
    ON security_audit_signal_windows(api_key_id, bucket_start DESC)
    WHERE api_key_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_security_audit_signal_group
    ON security_audit_signal_windows(group_id, bucket_start DESC)
    WHERE group_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS security_audit_signal_evaluations (
    id                  BIGSERIAL PRIMARY KEY,
    anchor_window_id    BIGINT NOT NULL REFERENCES security_audit_signal_windows(id) ON DELETE CASCADE,
    policy_key          VARCHAR(96) NOT NULL,
    policy_version      BIGINT NOT NULL,
    rule_id             VARCHAR(96) NOT NULL,
    metric              VARCHAR(48) NOT NULL,
    window_minutes      INTEGER NOT NULL,
    observed_value      DOUBLE PRECISION NOT NULL,
    threshold_value     DOUBLE PRECISION NOT NULL,
    sample_count        BIGINT NOT NULL DEFAULT 0,
    score               DOUBLE PRECISION NOT NULL DEFAULT 0,
    severity            VARCHAR(24) NOT NULL,
    matched             BOOLEAN NOT NULL DEFAULT FALSE,
    decision_pk         BIGINT REFERENCES security_audit_decisions(id) ON DELETE SET NULL,
    evaluated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_security_audit_signal_evaluation
        UNIQUE (anchor_window_id, policy_key, policy_version, rule_id),
    CONSTRAINT chk_security_audit_signal_metric
        CHECK (metric IN (
            'request_count', 'token_count', 'actual_cost',
            'error_count', 'error_rate', 'business_limited_rate',
            'average_duration_ms', 'maximum_duration_ms',
            'distinct_ip_count', 'distinct_model_count'
        )),
    CONSTRAINT chk_security_audit_signal_eval_window
        CHECK (window_minutes BETWEEN 1 AND 1440),
    CONSTRAINT chk_security_audit_signal_eval_score
        CHECK (score >= 0 AND score <= 1),
    CONSTRAINT chk_security_audit_signal_eval_severity
        CHECK (severity IN ('low', 'medium', 'high', 'critical'))
);

CREATE INDEX IF NOT EXISTS idx_security_audit_signal_evaluation_matches
    ON security_audit_signal_evaluations(evaluated_at DESC, severity, id DESC)
    WHERE matched = TRUE;
CREATE INDEX IF NOT EXISTS idx_security_audit_signal_evaluation_decision
    ON security_audit_signal_evaluations(decision_pk)
    WHERE decision_pk IS NOT NULL;

CREATE TABLE IF NOT EXISTS security_audit_signal_watermark (
    id                  SMALLINT PRIMARY KEY,
    last_aggregated_at  TIMESTAMPTZ NOT NULL,
    last_evaluated_at   TIMESTAMPTZ NOT NULL,
    last_evaluated_window_id BIGINT NOT NULL DEFAULT 0,
    last_error          VARCHAR(512) NOT NULL DEFAULT '',
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_security_audit_signal_watermark_singleton CHECK (id = 1)
);

INSERT INTO security_audit_signal_watermark(
    id, last_aggregated_at, last_evaluated_at, last_evaluated_window_id
)
VALUES (
    1,
    date_trunc('minute', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' - INTERVAL '5 minutes',
    date_trunc('minute', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' - INTERVAL '5 minutes',
    0
)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS security_audit_notifications (
    id                  BIGSERIAL PRIMARY KEY,
    notification_id     VARCHAR(64) NOT NULL UNIQUE,
    action_id           BIGINT NOT NULL REFERENCES security_audit_actions(id) ON DELETE CASCADE,
    decision_pk         BIGINT NOT NULL REFERENCES security_audit_decisions(id) ON DELETE CASCADE,
    audience            VARCHAR(24) NOT NULL,
    recipient_user_id   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    severity            VARCHAR(24) NOT NULL,
    title               VARCHAR(240) NOT NULL,
    body                TEXT NOT NULL,
    status              VARCHAR(24) NOT NULL DEFAULT 'unread',
    read_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_security_audit_notification_action UNIQUE (action_id),
    CONSTRAINT chk_security_audit_notification_audience
        CHECK (audience IN ('admin', 'user')),
    CONSTRAINT chk_security_audit_notification_severity
        CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    CONSTRAINT chk_security_audit_notification_status
        CHECK (status IN ('unread', 'read', 'dismissed'))
);

CREATE INDEX IF NOT EXISTS idx_security_audit_notifications_admin
    ON security_audit_notifications(status, created_at DESC, id DESC)
    WHERE audience = 'admin';
CREATE INDEX IF NOT EXISTS idx_security_audit_notifications_user
    ON security_audit_notifications(recipient_user_id, status, created_at DESC, id DESC)
    WHERE audience = 'user';
