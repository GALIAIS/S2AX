-- A4.2: an administrator may explicitly wake a delayed, still-queued Agent
-- decision after remediation (for example after registering a trusted local
-- adapter). This is not a provider call, lease, attempt, budget change,
-- circuit-breaker override, or Temporal Frame mutation. It does not seal a world frame; it only removes a future operational retry deadline and appends a narrow audit receipt.

CREATE OR REPLACE FUNCTION city_realtime_agent_operator_mutation_enabled(
    target_world_id BIGINT,
    target_request_code VARCHAR
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_agent_operator_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_agent_operator_request_code', TRUE) = target_request_code
       AND COALESCE(NULLIF(current_setting('sub2api.city_realtime_agent_operator_actor_user_id', TRUE), ''), '')
           ~ '^[1-9][0-9]*$'
$$;

CREATE TABLE IF NOT EXISTS city_realtime_agent_decision_operator_events (
    event_id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL,
    request_code VARCHAR(96) NOT NULL,
    event_type VARCHAR(32) NOT NULL,
    actor_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    previous_retry_not_before TIMESTAMPTZ NOT NULL,
    payload_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_agent_decision_operator_event_request_fk
        FOREIGN KEY (world_id, request_code)
        REFERENCES city_realtime_agent_decision_requests(world_id, request_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_decision_operator_event_check CHECK (
        request_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND event_type IN ('retry_requested')
        AND actor_user_id > 0
        AND previous_retry_not_before > created_at
        AND payload_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
        AND metadata = '{}'::jsonb
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_decision_operator_events_lookup
    ON city_realtime_agent_decision_operator_events (world_id, request_code, created_at DESC, event_id DESC);

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_decision_operator_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_operator_mutation_enabled(NEW.world_id, NEW.request_code)
       AND current_setting('sub2api.city_realtime_agent_operator_actor_user_id', TRUE) = NEW.actor_user_id::text THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent decision operator events require the administrator operator gate'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_agent_decision_operator_event_guard
    ON city_realtime_agent_decision_operator_events;
CREATE TRIGGER city_realtime_agent_decision_operator_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_decision_operator_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_decision_operator_event();

-- Preserve all pre-existing worker-only retry transitions and add exactly one
-- operator transition: queued + future retry deadline -> queued + no retry
-- deadline. It cannot change immutable request fields, attempt count, lease,
-- outbox, profile snapshot, breaker, budget ledger or canonical state.
CREATE OR REPLACE FUNCTION guard_city_realtime_agent_decision_request()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_profile_hash VARCHAR(64);
    expected_budget_hash VARCHAR(64);
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_decision_policy_enabled(NEW.world_id)
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.requested_frame_sequence)
       AND NEW.status = 'queued' AND NEW.attempt_count = 0
       AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL
       AND NEW.retry_not_before IS NULL AND NEW.terminal_frame_sequence IS NULL THEN
        IF NEW.model_profile_code IS NOT NULL THEN
            SELECT profile.profile_hash, profile.budget_hash
            INTO expected_profile_hash, expected_budget_hash
            FROM city_realtime_agent_model_profile_world_bindings binding
            JOIN city_realtime_agent_instances instance
              ON instance.world_id = binding.world_id AND instance.agent_code = NEW.agent_code
            JOIN city_realtime_agent_model_profile_versions profile
              ON profile.profile_code = binding.profile_code AND profile.profile_version = binding.profile_version
            JOIN city_realtime_agent_model_profile_heads head
              ON head.profile_code = profile.profile_code
            WHERE binding.world_id = NEW.world_id
              AND binding.agent_definition_code = instance.definition_code
              AND binding.binding_status = 'active'
              AND head.status = 'active'
              AND binding.profile_code = NEW.model_profile_code
              AND binding.profile_version = NEW.model_profile_version;
            IF expected_profile_hash IS NULL
               OR NEW.model_profile_hash <> expected_profile_hash
               OR NEW.model_budget_hash <> expected_budget_hash THEN
                RAISE EXCEPTION 'city realtime agent decision request model profile snapshot is invalid'
                    USING ERRCODE = '23514';
            END IF;
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND NEW.world_id = OLD.world_id AND NEW.request_code = OLD.request_code
       AND NEW.agent_code = OLD.agent_code AND NEW.observation_code = OLD.observation_code
       AND NEW.observation_hash = OLD.observation_hash AND NEW.precondition_hash = OLD.precondition_hash
       AND NEW.observed_frame_sequence = OLD.observed_frame_sequence
       AND NEW.expires_at_world_time_us = OLD.expires_at_world_time_us
       AND NEW.requested_frame_sequence = OLD.requested_frame_sequence
       AND NEW.model_profile_code IS NOT DISTINCT FROM OLD.model_profile_code
       AND NEW.model_profile_version IS NOT DISTINCT FROM OLD.model_profile_version
       AND NEW.model_profile_hash IS NOT DISTINCT FROM OLD.model_profile_hash
       AND NEW.model_budget_hash IS NOT DISTINCT FROM OLD.model_budget_hash
       AND NEW.metadata = OLD.metadata
       AND NEW.terminal_frame_sequence IS NULL
       AND (
           (
               city_realtime_agent_worker_mutation_enabled(NEW.world_id, NEW.request_code)
               AND (
                   (OLD.status = 'queued' AND NEW.status = 'leased'
                    AND NEW.attempt_count = OLD.attempt_count + 1
                    AND NEW.lease_owner IS NOT NULL AND NEW.lease_expires_at IS NOT NULL
                    AND NEW.retry_not_before IS NULL
                    AND (OLD.retry_not_before IS NULL OR OLD.retry_not_before <= NOW()))
                   OR
                   (OLD.status = 'leased' AND NEW.status = 'queued'
                    AND NEW.attempt_count = OLD.attempt_count
                    AND OLD.lease_expires_at <= NOW()
                    AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL
                    AND NEW.retry_not_before IS NULL)
                   OR
                   (OLD.status = 'leased' AND NEW.status = 'queued'
                    AND NEW.attempt_count = OLD.attempt_count
                    AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL
                    AND NEW.retry_not_before > NOW()
                    AND EXISTS (
                        SELECT 1
                        FROM city_realtime_agent_decision_attempts attempt
                        WHERE attempt.world_id = NEW.world_id
                          AND attempt.request_code = NEW.request_code
                          AND attempt.attempt_number = OLD.attempt_count
                          AND attempt.status = 'failed'
                    ))
                   OR
                   (OLD.status = 'queued' AND NEW.status = 'queued'
                    AND NEW.attempt_count = OLD.attempt_count
                    AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL
                    AND NEW.retry_not_before > NOW()
                    AND (OLD.retry_not_before IS NULL OR OLD.retry_not_before <= NOW()))
               )
           )
           OR
           (
               city_realtime_agent_operator_mutation_enabled(NEW.world_id, NEW.request_code)
               AND OLD.status = 'queued' AND NEW.status = 'queued'
               AND NEW.attempt_count = OLD.attempt_count
               AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL
               AND OLD.retry_not_before IS NOT NULL AND OLD.retry_not_before > NOW()
               AND NEW.retry_not_before IS NULL
           )
       ) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_agent_worker_mutation_enabled(NEW.world_id, NEW.request_code)
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.terminal_frame_sequence)
       AND OLD.status = 'leased' AND NEW.status IN ('accepted', 'rejected', 'stale', 'failed_terminal', 'cancelled')
       AND NEW.world_id = OLD.world_id AND NEW.request_code = OLD.request_code
       AND NEW.agent_code = OLD.agent_code AND NEW.observation_code = OLD.observation_code
       AND NEW.observation_hash = OLD.observation_hash AND NEW.precondition_hash = OLD.precondition_hash
       AND NEW.observed_frame_sequence = OLD.observed_frame_sequence
       AND NEW.expires_at_world_time_us = OLD.expires_at_world_time_us
       AND NEW.attempt_count = OLD.attempt_count
       AND NEW.requested_frame_sequence = OLD.requested_frame_sequence
       AND NEW.model_profile_code IS NOT DISTINCT FROM OLD.model_profile_code
       AND NEW.model_profile_version IS NOT DISTINCT FROM OLD.model_profile_version
       AND NEW.model_profile_hash IS NOT DISTINCT FROM OLD.model_profile_hash
       AND NEW.model_budget_hash IS NOT DISTINCT FROM OLD.model_budget_hash
       AND NEW.metadata = OLD.metadata
       AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL
       AND NEW.retry_not_before IS NULL
       AND NEW.terminal_frame_sequence > OLD.requested_frame_sequence THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent decision requests require a sealed frame, worker lease transition, or audited administrator retry'
        USING ERRCODE = '55000';
END;
$$;

COMMENT ON TABLE city_realtime_agent_decision_operator_events IS
    'Append-only administrator retry receipts for delayed realtime Agent dispatch; contains no provider payload, prompt, response, credential, route, billing or currency data.';

COMMENT ON FUNCTION guard_city_realtime_agent_decision_request() IS
    'Sealed realtime Agent requests permit immutable snapshots, worker leases, terminal frames, worker deferrals, and an audited administrator wake of an existing future retry deadline.';
