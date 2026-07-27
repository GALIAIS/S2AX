-- Keep the database contract aligned with the action worker. Migration 291
-- intentionally reserved future action names, but accepting rows without a
-- real executor creates a false-enforcement risk. Historical terminal rows are
-- retained for audit; NOT VALID still enforces the whitelist for every new or
-- subsequently updated row.

UPDATE security_audit_actions
SET status='failed',
    processed_at=NOW(),
    lease_owner='',
    lease_expires_at=NULL,
    error_code='unsupported_action',
    error_message='action type has no registered security-audit executor',
    updated_at=NOW()
WHERE action_type NOT IN (
    'pause_api_key', 'pause_user', 'notify_user', 'notify_admin', 'open_case'
)
  AND status IN ('pending', 'processing', 'retry');

UPDATE security_audit_outbox AS outbox
SET status='failed',
    lease_owner='',
    lease_expires_at=NULL,
    last_error='action type has no registered security-audit executor',
    updated_at=NOW()
FROM security_audit_actions AS action
WHERE outbox.action_id=action.id
  AND action.action_type NOT IN (
      'pause_api_key', 'pause_user', 'notify_user', 'notify_admin', 'open_case'
  )
  AND outbox.status IN ('pending', 'processing', 'retry');

ALTER TABLE security_audit_actions
    DROP CONSTRAINT IF EXISTS chk_security_audit_action_type;

ALTER TABLE security_audit_actions
    ADD CONSTRAINT chk_security_audit_action_type
    CHECK (action_type IN (
        'pause_api_key', 'pause_user', 'notify_user', 'notify_admin', 'open_case'
    )) NOT VALID;

COMMENT ON CONSTRAINT chk_security_audit_action_type ON security_audit_actions IS
    'New and updated rows must use an action type with a registered executor; legacy terminal rows remain auditable';
