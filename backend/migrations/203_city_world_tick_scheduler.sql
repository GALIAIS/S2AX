-- Durable operational coordination for the city-world tick scheduler.
-- This table is intentionally excluded from canonical simulation state: it only
-- coordinates workers and records retry health around authoritative tick facts.

CREATE TABLE IF NOT EXISTS city_world_schedule_states (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE CASCADE,
    lease_token VARCHAR(64),
    lease_expires_at TIMESTAMPTZ,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    retry_not_before TIMESTAMPTZ,
    last_attempt_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_error_code VARCHAR(160),
    last_error_detail VARCHAR(1024),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_world_schedule_states_failures_check
        CHECK (consecutive_failures >= 0 AND consecutive_failures <= 1000000),
    CONSTRAINT city_world_schedule_states_lease_pair_check
        CHECK ((lease_token IS NULL) = (lease_expires_at IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_city_world_schedule_states_retry
    ON city_world_schedule_states (retry_not_before, world_id)
    WHERE retry_not_before IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_city_world_schedule_states_lease
    ON city_world_schedule_states (lease_expires_at, world_id)
    WHERE lease_expires_at IS NOT NULL;

COMMENT ON TABLE city_world_schedule_states IS
    'Operational leases and retry state for city tick workers; never part of canonical simulation state';
