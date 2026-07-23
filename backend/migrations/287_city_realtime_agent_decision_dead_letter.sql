-- A4.2: an administrator may quarantine a still-queued Agent decision for
-- review. Quarantine is operational dispatch state only: it neither creates a
-- model attempt nor changes the request, outbox, budget, breaker, Temporal
-- Frame chain, or canonical world state. It does not seal a world frame.
-- Release is deliberately separate
-- from retry; an operator must explicitly wake a released delayed request.

CREATE OR REPLACE FUNCTION city_realtime_agent_decision_dead_letter_mutation_enabled(
    target_world_id BIGINT,
    target_request_code VARCHAR,
    expected_action VARCHAR
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT city_realtime_agent_operator_mutation_enabled(target_world_id, target_request_code)
       AND current_setting('sub2api.city_realtime_agent_operator_dead_letter_action', TRUE) = expected_action
$$;

CREATE TABLE IF NOT EXISTS city_realtime_agent_decision_dead_letters (
    world_id BIGINT NOT NULL,
    request_code VARCHAR(96) NOT NULL,
    dead_letter_status VARCHAR(16) NOT NULL,
    reason_code VARCHAR(64) NOT NULL,
    quarantined_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    quarantined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_by_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    released_at TIMESTAMPTZ,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, request_code),
    CONSTRAINT city_realtime_agent_decision_dead_letter_request_fk
        FOREIGN KEY (world_id, request_code)
        REFERENCES city_realtime_agent_decision_requests(world_id, request_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_decision_dead_letter_check CHECK (
        request_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND dead_letter_status IN ('quarantined', 'released')
        AND reason_code IN ('operator_review', 'provider_configuration', 'provider_incident', 'budget_review', 'world_maintenance')
        AND quarantined_by_user_id > 0
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
        AND metadata = '{}'::jsonb
        AND (
            (dead_letter_status = 'quarantined' AND released_by_user_id IS NULL AND released_at IS NULL)
            OR
            (dead_letter_status = 'released' AND released_by_user_id IS NOT NULL AND released_by_user_id > 0 AND released_at IS NOT NULL)
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_decision_dead_letters_active
    ON city_realtime_agent_decision_dead_letters (world_id, request_code)
    WHERE dead_letter_status = 'quarantined';

CREATE TABLE IF NOT EXISTS city_realtime_agent_decision_dead_letter_events (
    event_id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL,
    request_code VARCHAR(96) NOT NULL,
    event_type VARCHAR(16) NOT NULL,
    reason_code VARCHAR(64) NOT NULL,
    actor_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    payload_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_agent_decision_dead_letter_event_request_fk
        FOREIGN KEY (world_id, request_code)
        REFERENCES city_realtime_agent_decision_requests(world_id, request_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_decision_dead_letter_event_check CHECK (
        request_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND (
            (event_type = 'quarantined' AND reason_code IN ('operator_review', 'provider_configuration', 'provider_incident', 'budget_review', 'world_maintenance'))
            OR
            (event_type = 'released' AND reason_code = 'operator_release')
        )
        AND actor_user_id > 0
        AND payload_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
        AND metadata = '{}'::jsonb
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_decision_dead_letter_events_lookup
    ON city_realtime_agent_decision_dead_letter_events (world_id, request_code, created_at DESC, event_id DESC);

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_decision_dead_letter()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    configured_actor VARCHAR;
BEGIN
    configured_actor := current_setting('sub2api.city_realtime_agent_operator_actor_user_id', TRUE);
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_decision_dead_letter_mutation_enabled(NEW.world_id, NEW.request_code, 'quarantine')
       AND NEW.dead_letter_status = 'quarantined'
       AND configured_actor = NEW.quarantined_by_user_id::text THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND NEW.world_id = OLD.world_id AND NEW.request_code = OLD.request_code
       AND NEW.created_at = OLD.created_at
       AND (
           (
               OLD.dead_letter_status = 'quarantined' AND NEW.dead_letter_status = 'released'
               AND city_realtime_agent_decision_dead_letter_mutation_enabled(NEW.world_id, NEW.request_code, 'release')
               AND NEW.reason_code = OLD.reason_code
               AND NEW.quarantined_by_user_id = OLD.quarantined_by_user_id
               AND NEW.quarantined_at = OLD.quarantined_at
               AND configured_actor = NEW.released_by_user_id::text
           )
           OR
           (
               OLD.dead_letter_status = 'released' AND NEW.dead_letter_status = 'quarantined'
               AND city_realtime_agent_decision_dead_letter_mutation_enabled(NEW.world_id, NEW.request_code, 'quarantine')
               AND configured_actor = NEW.quarantined_by_user_id::text
               AND NEW.released_by_user_id IS NULL AND NEW.released_at IS NULL
           )
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent decision dead letters require the administrator operator gate'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_agent_decision_dead_letter_guard
    ON city_realtime_agent_decision_dead_letters;
CREATE TRIGGER city_realtime_agent_decision_dead_letter_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_decision_dead_letters
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_decision_dead_letter();

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_decision_dead_letter_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    configured_actor VARCHAR;
BEGIN
    configured_actor := current_setting('sub2api.city_realtime_agent_operator_actor_user_id', TRUE);
    IF TG_OP = 'INSERT'
       AND configured_actor = NEW.actor_user_id::text
       AND (
           (NEW.event_type = 'quarantined'
            AND city_realtime_agent_decision_dead_letter_mutation_enabled(NEW.world_id, NEW.request_code, 'quarantine'))
           OR
           (NEW.event_type = 'released'
            AND city_realtime_agent_decision_dead_letter_mutation_enabled(NEW.world_id, NEW.request_code, 'release'))
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent decision dead letter events are append-only and require the administrator operator gate'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_agent_decision_dead_letter_event_guard
    ON city_realtime_agent_decision_dead_letter_events;
CREATE TRIGGER city_realtime_agent_decision_dead_letter_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_decision_dead_letter_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_decision_dead_letter_event();

COMMENT ON TABLE city_realtime_agent_decision_dead_letters IS
    'Current administrator review/quarantine state for queued realtime Agent decisions; operational only and excluded from canonical world state.';

COMMENT ON TABLE city_realtime_agent_decision_dead_letter_events IS
    'Append-only administrator review receipts for realtime Agent decision quarantine and release; contains no model transcript, credential, route, billing or currency data.';
