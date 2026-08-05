-- Keep account-statistics pricing feature parity with channel token pricing.
-- NULL preserves the existing fallback to the text input price.
ALTER TABLE channel_account_stats_model_pricing
    ADD COLUMN IF NOT EXISTS image_input_price NUMERIC(20,12);
