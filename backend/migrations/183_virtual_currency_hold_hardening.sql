-- Harden the hold state machine introduced with the virtual currency core.
-- This migration is additive and safe to re-run through the migration runner.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'virtual_currency_hold_status_check'
          AND conrelid = 'virtual_currency_holds'::regclass
    ) THEN
        ALTER TABLE virtual_currency_holds
            ADD CONSTRAINT virtual_currency_hold_status_check
            CHECK (status IN ('active', 'committed', 'released', 'expired'));
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'virtual_currency_hold_source_type_check'
          AND conrelid = 'virtual_currency_holds'::regclass
    ) THEN
        ALTER TABLE virtual_currency_holds
            ADD CONSTRAINT virtual_currency_hold_source_type_check
            CHECK (source_type ~ '^[a-z][a-z0-9_.-]{1,31}$');
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'virtual_currency_hold_amount_limit_check'
          AND conrelid = 'virtual_currency_holds'::regclass
    ) THEN
        ALTER TABLE virtual_currency_holds
            ADD CONSTRAINT virtual_currency_hold_amount_limit_check
            CHECK (amount_units <= 1152921504606846976);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_virtual_currency_holds_settlement
    ON virtual_currency_holds (currency_id, user_id, status, expires_at, id);
