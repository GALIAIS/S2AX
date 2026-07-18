-- Personal operator pricing needs sub-0.001 rates without losing the value
-- when a usage snapshot is written. Keep enough integer room for existing
-- installations while standardising all multiplier snapshots to eight
-- fractional digits.
ALTER TABLE groups
    ALTER COLUMN rate_multiplier TYPE DECIMAL(18,8) USING rate_multiplier::numeric,
    ALTER COLUMN peak_rate_multiplier TYPE DECIMAL(18,8) USING peak_rate_multiplier::numeric,
    ALTER COLUMN image_rate_multiplier TYPE DECIMAL(18,8) USING image_rate_multiplier::numeric,
    ALTER COLUMN batch_image_discount_multiplier TYPE DECIMAL(18,8) USING batch_image_discount_multiplier::numeric,
    ALTER COLUMN batch_image_hold_multiplier TYPE DECIMAL(18,8) USING batch_image_hold_multiplier::numeric,
    ALTER COLUMN video_rate_multiplier TYPE DECIMAL(18,8) USING video_rate_multiplier::numeric;

ALTER TABLE accounts
    ALTER COLUMN rate_multiplier TYPE DECIMAL(18,8) USING rate_multiplier::numeric;

ALTER TABLE usage_logs
    ALTER COLUMN rate_multiplier TYPE DECIMAL(18,8) USING rate_multiplier::numeric,
    ALTER COLUMN account_rate_multiplier TYPE DECIMAL(18,8) USING account_rate_multiplier::numeric;

ALTER TABLE user_group_rate_multipliers
    ALTER COLUMN rate_multiplier TYPE DECIMAL(18,8) USING rate_multiplier::numeric;

ALTER TABLE batch_image_jobs
    ALTER COLUMN group_rate_multiplier TYPE DECIMAL(18,8) USING group_rate_multiplier::numeric,
    ALTER COLUMN account_rate_multiplier TYPE DECIMAL(18,8) USING account_rate_multiplier::numeric,
    ALTER COLUMN batch_discount_multiplier TYPE DECIMAL(18,8) USING batch_discount_multiplier::numeric,
    ALTER COLUMN hold_multiplier TYPE DECIMAL(18,8) USING hold_multiplier::numeric;
