-- Independent Fast/priority token prices for channel billing.
-- NULL preserves the legacy behavior: the standard channel price is reused.

ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS input_price_priority NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS output_price_priority NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS cache_write_price_priority NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS cache_read_price_priority NUMERIC(20,12);

ALTER TABLE channel_pricing_intervals
    ADD COLUMN IF NOT EXISTS input_price_priority NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS output_price_priority NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS cache_write_price_priority NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS cache_read_price_priority NUMERIC(20,12);

ALTER TABLE channel_account_stats_model_pricing
    ADD COLUMN IF NOT EXISTS input_price_priority NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS output_price_priority NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS cache_write_price_priority NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS cache_read_price_priority NUMERIC(20,12);

ALTER TABLE channel_account_stats_pricing_intervals
    ADD COLUMN IF NOT EXISTS input_price_priority NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS output_price_priority NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS cache_write_price_priority NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS cache_read_price_priority NUMERIC(20,12);

COMMENT ON COLUMN channel_model_pricing.input_price_priority IS 'Fast/priority input price per token; NULL inherits input_price';
COMMENT ON COLUMN channel_model_pricing.output_price_priority IS 'Fast/priority output price per token; NULL inherits output_price';
COMMENT ON COLUMN channel_model_pricing.cache_write_price_priority IS 'Fast/priority cache-write price per token; NULL inherits cache_write_price';
COMMENT ON COLUMN channel_model_pricing.cache_read_price_priority IS 'Fast/priority cache-read price per token; NULL inherits cache_read_price';
