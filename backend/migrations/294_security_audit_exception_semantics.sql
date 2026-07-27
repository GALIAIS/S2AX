-- Remove the misleading skip_detector exception effect. Exceptions are applied
-- after detectors have produced durable evidence, so they may suppress or
-- reduce follow-up actions but must not claim that a detector was skipped.
-- Existing rows are conservatively converted to warn_only.

UPDATE security_audit_exceptions
SET effect='warn_only'
WHERE effect='skip_detector';

ALTER TABLE security_audit_exceptions
    DROP CONSTRAINT IF EXISTS chk_security_audit_exception_effect;

ALTER TABLE security_audit_exceptions
    ADD CONSTRAINT chk_security_audit_exception_effect
    CHECK (effect IN ('allow_and_record', 'warn_only'));
