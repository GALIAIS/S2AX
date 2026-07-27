-- Make manual exception revocation attributable and reviewable. Existing
-- revoked rows are backfilled before the invariant is enabled.

ALTER TABLE security_audit_exceptions
    ADD COLUMN IF NOT EXISTS revoked_by BIGINT;

ALTER TABLE security_audit_exceptions
    ADD COLUMN IF NOT EXISTS revoked_reason VARCHAR(512) NOT NULL DEFAULT '';

UPDATE security_audit_exceptions
SET revoked_reason = 'legacy manual revocation',
    expired_at = COALESCE(expired_at, NOW())
WHERE status = 'revoked'
  AND revoked_reason = '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_security_audit_exception_revoked_by'
          AND conrelid = 'security_audit_exceptions'::regclass
    ) THEN
        ALTER TABLE security_audit_exceptions
            ADD CONSTRAINT fk_security_audit_exception_revoked_by
            FOREIGN KEY (revoked_by) REFERENCES users(id) ON DELETE SET NULL;
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_security_audit_exception_revocation'
          AND conrelid = 'security_audit_exceptions'::regclass
    ) THEN
        ALTER TABLE security_audit_exceptions
            ADD CONSTRAINT chk_security_audit_exception_revocation
            CHECK (status <> 'revoked' OR (expired_at IS NOT NULL AND revoked_reason <> ''));
    END IF;
END
$$;
