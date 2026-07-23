-- A4.1: provider-availability circuit state.  This table is an external-I/O
-- control plane and is deliberately outside canonical realtime world state.

CREATE OR REPLACE FUNCTION city_realtime_agent_model_breaker_worker_mutation_enabled()
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_agent_model_budget_worker', TRUE) = 'on'
       AND NULLIF(current_setting('sub2api.city_realtime_agent_worker_world_id', TRUE), '') IS NOT NULL
       AND NULLIF(current_setting('sub2api.city_realtime_agent_worker_request_code', TRUE), '') IS NOT NULL
$$;

CREATE TABLE IF NOT EXISTS city_realtime_agent_model_circuit_breakers (
    profile_code VARCHAR(64) NOT NULL,
    profile_version INTEGER NOT NULL,
    profile_hash VARCHAR(64) NOT NULL,
    budget_hash VARCHAR(64) NOT NULL,
    breaker_state VARCHAR(16) NOT NULL,
    consecutive_provider_failures SMALLINT NOT NULL DEFAULT 0,
    opened_at TIMESTAMPTZ,
    cooldown_until TIMESTAMPTZ,
    probe_request_code VARCHAR(96),
    probe_lease_expires_at TIMESTAMPTZ,
    last_provider_failure_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (profile_code, profile_version),
    CONSTRAINT city_realtime_agent_model_circuit_breaker_profile_fk
        FOREIGN KEY (profile_code, profile_version)
        REFERENCES city_realtime_agent_model_profile_versions(profile_code, profile_version) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_model_circuit_breaker_check CHECK (
        profile_hash ~ '^[0-9a-f]{64}$'
        AND budget_hash ~ '^[0-9a-f]{64}$'
        AND breaker_state IN ('closed', 'open', 'half_open')
        AND consecutive_provider_failures BETWEEN 0 AND 32767
        AND (probe_request_code IS NULL OR probe_request_code ~ '^[a-z][a-z0-9_.-]{1,95}$')
        AND jsonb_typeof(metadata) = 'object'
        AND metadata = '{}'::jsonb
        AND (
            (breaker_state = 'closed'
             AND opened_at IS NULL AND cooldown_until IS NULL
             AND probe_request_code IS NULL AND probe_lease_expires_at IS NULL)
            OR
            (breaker_state = 'open'
             AND consecutive_provider_failures > 0
             AND opened_at IS NOT NULL AND cooldown_until IS NOT NULL
             AND cooldown_until > opened_at
             AND probe_request_code IS NULL AND probe_lease_expires_at IS NULL)
            OR
            (breaker_state = 'half_open'
             AND consecutive_provider_failures > 0
             AND opened_at IS NOT NULL AND cooldown_until IS NOT NULL
             AND cooldown_until > opened_at
             AND probe_request_code IS NOT NULL AND probe_lease_expires_at IS NOT NULL
             AND probe_lease_expires_at > cooldown_until)
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_model_circuit_breakers_ready
    ON city_realtime_agent_model_circuit_breakers (breaker_state, cooldown_until)
    WHERE breaker_state IN ('open', 'half_open');

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_model_circuit_breaker()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_profile_hash VARCHAR(64);
    expected_budget_hash VARCHAR(64);
BEGIN
    IF NOT city_realtime_agent_model_breaker_worker_mutation_enabled() THEN
        RAISE EXCEPTION 'city realtime agent model circuit breaker requires the worker gate'
            USING ERRCODE = '55000';
    END IF;
    SELECT profile_hash, budget_hash INTO expected_profile_hash, expected_budget_hash
    FROM city_realtime_agent_model_profile_versions
    WHERE profile_code = NEW.profile_code AND profile_version = NEW.profile_version;
    IF expected_profile_hash IS NULL OR NEW.profile_hash <> expected_profile_hash
       OR NEW.budget_hash <> expected_budget_hash OR NEW.metadata <> '{}'::jsonb THEN
        RAISE EXCEPTION 'city realtime agent model circuit breaker snapshot is invalid'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.breaker_state <> 'closed' OR NEW.consecutive_provider_failures <> 0
           OR NEW.opened_at IS NOT NULL OR NEW.cooldown_until IS NOT NULL
           OR NEW.probe_request_code IS NOT NULL OR NEW.probe_lease_expires_at IS NOT NULL THEN
            RAISE EXCEPTION 'city realtime agent model circuit breaker must start closed'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND NEW.profile_code = OLD.profile_code AND NEW.profile_version = OLD.profile_version
       AND NEW.profile_hash = OLD.profile_hash AND NEW.budget_hash = OLD.budget_hash
       AND NEW.created_at = OLD.created_at
       AND (
            (OLD.breaker_state = 'closed' AND NEW.breaker_state IN ('closed', 'open'))
            OR (OLD.breaker_state = 'open' AND NEW.breaker_state IN ('closed', 'open', 'half_open'))
            OR (OLD.breaker_state = 'half_open' AND NEW.breaker_state IN ('closed', 'open', 'half_open'))
       ) THEN
        NEW.updated_at := NOW();
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent model circuit breakers are worker-controlled state machines'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_agent_model_circuit_breaker_guard
    ON city_realtime_agent_model_circuit_breakers;
CREATE TRIGGER city_realtime_agent_model_circuit_breaker_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_model_circuit_breakers
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_model_circuit_breaker();

COMMENT ON TABLE city_realtime_agent_model_circuit_breakers IS
    'Worker-controlled provider availability state. It gates external Agent calls only and never alters realtime world time or canonical state.';
