-- A4.1/A4.2: provider failure retry timing is operational worker state. It is
-- deliberately outside the realtime canonical state so a transport retry never
-- changes the world timeline or state hash.

ALTER TABLE city_realtime_agent_decision_requests
    ADD COLUMN IF NOT EXISTS retry_not_before TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_decision_requests_retry_ready
    ON city_realtime_agent_decision_requests (world_id, retry_not_before, request_code)
    WHERE status = 'queued' AND retry_not_before IS NOT NULL;

-- Preserve the A4 immutable profile snapshot checks while extending queued
-- work with a worker-only retry deadline. A lease always clears the deadline;
-- completed and terminal work must not carry one.
ALTER TABLE city_realtime_agent_decision_requests
    DROP CONSTRAINT IF EXISTS city_realtime_agent_decision_request_identity_check;
ALTER TABLE city_realtime_agent_decision_requests
    ADD CONSTRAINT city_realtime_agent_decision_request_identity_check CHECK (
        request_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND agent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND observation_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND observation_hash ~ '^[0-9a-f]{64}$'
        AND precondition_hash ~ '^[0-9a-f]{64}$'
        AND observed_frame_sequence > 0
        AND expires_at_world_time_us >= 0
        AND status IN ('queued', 'leased', 'accepted', 'rejected', 'stale', 'failed_terminal', 'cancelled')
        AND attempt_count >= 0 AND attempt_count <= 32767
        AND requested_frame_sequence > 0
        AND jsonb_typeof(metadata) = 'object'
        AND (
            (model_profile_code IS NULL AND model_profile_version IS NULL
             AND model_profile_hash IS NULL AND model_budget_hash IS NULL)
            OR
            (model_profile_code ~ '^[a-z][a-z0-9_.-]{2,63}$'
             AND model_profile_version > 0
             AND model_profile_hash ~ '^[0-9a-f]{64}$'
             AND model_budget_hash ~ '^[0-9a-f]{64}$')
        )
        AND (
            (status = 'queued' AND lease_owner IS NULL AND lease_expires_at IS NULL
             AND terminal_frame_sequence IS NULL)
            OR (status = 'leased' AND lease_owner ~ '^[a-z][a-z0-9_.-]{1,63}$'
                AND lease_expires_at IS NOT NULL AND retry_not_before IS NULL
                AND terminal_frame_sequence IS NULL)
            OR (status IN ('accepted', 'rejected', 'stale', 'failed_terminal', 'cancelled')
                AND lease_owner IS NULL AND lease_expires_at IS NULL
                AND retry_not_before IS NULL
                AND terminal_frame_sequence IS NOT NULL AND terminal_frame_sequence > requested_frame_sequence)
        )
    );

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
       AND city_realtime_agent_worker_mutation_enabled(NEW.world_id, NEW.request_code)
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
    RAISE EXCEPTION 'city realtime agent decision requests require a sealed frame or worker lease transition'
        USING ERRCODE = '55000';
END;
$$;

-- The outbox has no independent retry timestamp. It follows the request's
-- failed current attempt, allowing the worker to release both records in the
-- same transaction without waiting for a lease to expire artificially.
CREATE OR REPLACE FUNCTION guard_city_realtime_agent_outbox()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_decision_policy_enabled(NEW.world_id)
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.created_frame_sequence)
       AND NEW.status = 'queued' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_agent_worker_mutation_enabled(NEW.world_id, NEW.request_code)
       AND NEW.world_id = OLD.world_id AND NEW.outbox_code = OLD.outbox_code
       AND NEW.request_code = OLD.request_code AND NEW.dedup_key = OLD.dedup_key
       AND NEW.created_frame_sequence = OLD.created_frame_sequence AND NEW.metadata = OLD.metadata
       AND ((OLD.status = 'queued' AND NEW.status = 'leased'
             AND NEW.lease_owner IS NOT NULL AND NEW.lease_expires_at IS NOT NULL AND NEW.completed_at IS NULL)
            OR (OLD.status = 'leased' AND NEW.status = 'queued'
                AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL AND NEW.completed_at IS NULL
                AND (OLD.lease_expires_at <= NOW()
                     OR EXISTS (
                         SELECT 1
                         FROM city_realtime_agent_decision_requests request
                         JOIN city_realtime_agent_decision_attempts attempt
                           ON attempt.world_id = request.world_id
                          AND attempt.request_code = request.request_code
                         WHERE request.world_id = NEW.world_id
                           AND request.request_code = NEW.request_code
                           AND attempt.attempt_number = request.attempt_count
                           AND attempt.status = 'failed'
                     )))
            OR (OLD.status = 'leased' AND NEW.status IN ('succeeded', 'failed_terminal')
                AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL AND NEW.completed_at IS NOT NULL)) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_agent_personality_policy_enabled(NEW.world_id)
       AND city_realtime_agent_mutation_enabled(NEW.world_id, (
           SELECT terminal_frame_sequence
           FROM city_realtime_agent_decision_requests request
           WHERE request.world_id = NEW.world_id AND request.request_code = NEW.request_code
       ))
       AND OLD.status = 'queued' AND NEW.status = 'cancelled'
       AND NEW.world_id = OLD.world_id AND NEW.outbox_code = OLD.outbox_code
       AND NEW.request_code = OLD.request_code AND NEW.dedup_key = OLD.dedup_key
       AND NEW.created_frame_sequence = OLD.created_frame_sequence AND NEW.metadata = OLD.metadata
       AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL AND NEW.completed_at IS NOT NULL THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent outbox changes require the worker gate'
        USING ERRCODE = '55000';
END;
$$;

COMMENT ON COLUMN city_realtime_agent_decision_requests.retry_not_before IS
    'Worker-only retry backoff deadline for provider failures; excluded from realtime canonical state.';
