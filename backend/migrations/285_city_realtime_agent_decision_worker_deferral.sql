-- A4.2: a missing process-local provider adapter, an open circuit, or a
-- temporary model budget must defer queued work without forging a failed model
-- attempt. This is operational dispatch state only; it is excluded from the realtime canonical state,
-- Temporal Frame chain and player projection.

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
           OR
           -- The worker may delay a not-yet-leased row when execution cannot
           -- begin. No attempt count, profile snapshot, lease, outbox or
           -- canonical world state may change in this transition.
           (OLD.status = 'queued' AND NEW.status = 'queued'
            AND NEW.attempt_count = OLD.attempt_count
            AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL
            AND NEW.retry_not_before > NOW()
            AND (OLD.retry_not_before IS NULL OR OLD.retry_not_before <= NOW()))
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

COMMENT ON FUNCTION guard_city_realtime_agent_decision_request() IS
    'Sealed realtime Agent requests permit only immutable profile snapshots, leases, terminal frames, and worker-only operational retry deferral.';
