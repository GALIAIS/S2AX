-- Extend one-time redeem codes with an atomic virtual-currency reward.
-- Existing balance, concurrency, subscription, and invitation codes remain
-- unchanged: all three columns stay NULL for those code types.

ALTER TABLE redeem_codes
    ADD COLUMN IF NOT EXISTS currency_id BIGINT,
    ADD COLUMN IF NOT EXISTS currency_amount_units BIGINT,
    ADD COLUMN IF NOT EXISTS currency_group_id BIGINT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'redeem_codes_currency_id_fkey'
    ) THEN
        ALTER TABLE redeem_codes
            ADD CONSTRAINT redeem_codes_currency_id_fkey
            FOREIGN KEY (currency_id) REFERENCES virtual_currencies(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'redeem_codes_currency_group_id_fkey'
    ) THEN
        ALTER TABLE redeem_codes
            ADD CONSTRAINT redeem_codes_currency_group_id_fkey
            FOREIGN KEY (currency_group_id) REFERENCES groups(id) ON DELETE RESTRICT;
    END IF;
END $$;

ALTER TABLE redeem_codes
    DROP CONSTRAINT IF EXISTS redeem_codes_virtual_currency_fields_check;

ALTER TABLE redeem_codes
    ADD CONSTRAINT redeem_codes_virtual_currency_fields_check CHECK (
        (
            type = 'virtual_currency'
            AND currency_id IS NOT NULL
            AND currency_amount_units IS NOT NULL
            AND currency_amount_units > 0
            AND currency_group_id IS NOT NULL
        )
        OR (
            type <> 'virtual_currency'
            AND currency_id IS NULL
            AND currency_amount_units IS NULL
            AND currency_group_id IS NULL
        )
    );

ALTER TABLE redeem_codes
    DROP CONSTRAINT IF EXISTS redeem_codes_currency_amount_units_check;

ALTER TABLE redeem_codes
    ADD CONSTRAINT redeem_codes_currency_amount_units_check CHECK (
        currency_amount_units IS NULL OR currency_amount_units <= 1152921504606846976
    );

CREATE INDEX IF NOT EXISTS idx_redeem_codes_currency_id
    ON redeem_codes (currency_id, currency_group_id, status);
