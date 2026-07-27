-- Persist policy lifecycle actors and reasons in the same transaction as each
-- state change. Generic HTTP operation logs remain useful, but are not a
-- substitute for policy-native, queryable transition history.

CREATE TABLE IF NOT EXISTS security_audit_policy_transitions (
    id                  BIGSERIAL PRIMARY KEY,
    policy_version_id   BIGINT NOT NULL
                            REFERENCES security_audit_policy_versions(id) ON DELETE RESTRICT,
    policy_key          VARCHAR(96) NOT NULL,
    version             BIGINT NOT NULL,
    from_status         VARCHAR(24) NOT NULL,
    to_status           VARCHAR(24) NOT NULL,
    actor_id            BIGINT REFERENCES users(id) ON DELETE SET NULL,
    reason              VARCHAR(512) NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_security_audit_policy_transition_from
        CHECK (from_status IN ('draft', 'validated', 'shadow', 'active', 'retired')),
    CONSTRAINT chk_security_audit_policy_transition_to
        CHECK (to_status IN ('draft', 'validated', 'shadow', 'active', 'retired')),
    CONSTRAINT chk_security_audit_policy_transition_changed
        CHECK (from_status <> to_status)
);

CREATE INDEX IF NOT EXISTS idx_security_audit_policy_transition_version
    ON security_audit_policy_transitions(policy_key, version, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_security_audit_policy_transition_actor
    ON security_audit_policy_transitions(actor_id, created_at DESC, id DESC)
    WHERE actor_id IS NOT NULL;

COMMENT ON TABLE security_audit_policy_transitions IS
    'Append-only lifecycle history for validated, shadow, active, retired and rollback policy transitions.';
