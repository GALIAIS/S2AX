-- Durable live-shadow evaluation. Shadow policy versions are evaluated against
-- unified decisions after ingestion, but never publish enforcement actions.

CREATE TABLE IF NOT EXISTS security_audit_shadow_evaluations (
    id                      BIGSERIAL PRIMARY KEY,
    decision_pk             BIGINT NOT NULL REFERENCES security_audit_decisions(id) ON DELETE CASCADE,
    policy_version_id       BIGINT NOT NULL REFERENCES security_audit_policy_versions(id) ON DELETE RESTRICT,
    policy_key              VARCHAR(96) NOT NULL,
    policy_version          BIGINT NOT NULL,
    risk_level              VARCHAR(24) NOT NULL,
    baseline_request_action VARCHAR(24) NOT NULL,
    proposed_request_action VARCHAR(24) NOT NULL,
    baseline_actions        JSONB NOT NULL DEFAULT '[]'::jsonb,
    proposed_actions        JSONB NOT NULL DEFAULT '[]'::jsonb,
    request_action_changed  BOOLEAN NOT NULL DEFAULT FALSE,
    actions_changed         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_security_audit_shadow_decision_policy
        UNIQUE (decision_pk, policy_version_id),
    CONSTRAINT chk_security_audit_shadow_risk
        CHECK (risk_level IN ('none', 'low', 'medium', 'high', 'critical')),
    CONSTRAINT chk_security_audit_shadow_baseline_request_action
        CHECK (baseline_request_action IN ('allow', 'warn', 'block')),
    CONSTRAINT chk_security_audit_shadow_proposed_request_action
        CHECK (proposed_request_action IN ('allow', 'warn', 'block'))
);

CREATE INDEX IF NOT EXISTS idx_security_audit_shadow_policy_created
    ON security_audit_shadow_evaluations(policy_key, policy_version, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_security_audit_shadow_changed
    ON security_audit_shadow_evaluations(policy_key, policy_version, created_at DESC, id DESC)
    WHERE request_action_changed OR actions_changed;

CREATE TABLE IF NOT EXISTS security_audit_shadow_watermark (
    id                  SMALLINT PRIMARY KEY DEFAULT 1,
    last_decision_pk    BIGINT NOT NULL DEFAULT 0,
    last_evaluated_at   TIMESTAMPTZ,
    last_error          VARCHAR(512) NOT NULL DEFAULT '',
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_security_audit_shadow_watermark_singleton CHECK (id = 1),
    CONSTRAINT chk_security_audit_shadow_watermark_cursor CHECK (last_decision_pk >= 0)
);

INSERT INTO security_audit_shadow_watermark(id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;
