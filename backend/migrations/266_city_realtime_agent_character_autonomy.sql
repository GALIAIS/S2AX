-- A3.1: owner-scoped Character Agent configuration and the first real
-- deterministic action bridge.  Policy 1.2.0 is additive: worlds pinned to
-- 1.0.0 or 1.1.0 keep their historical hashes, authorization catalogues and
-- A2 wait-only intent semantics.

WITH core_policy AS (
    SELECT '{
      "schema_version": 1,
      "definitions": [
        {"code":"system.root","kind":"simulation","allowed_parents":[],"allowed_children":["system.npc_manager"]},
        {"code":"system.npc_manager","kind":"simulation","allowed_parents":["system.root"],"allowed_children":["character.npc"]},
        {"code":"character.npc","kind":"character","allowed_parents":["system.npc_manager"],"allowed_children":[]},
        {"code":"character.user","kind":"character","allowed_parents":[],"allowed_children":[]}
      ],
      "decision_runtime": {
        "observation_schema": "city-realtime-agent-observation-v1",
        "decision_schema": "agent-decision-v1",
        "actions": ["agent.wait", "character.activity.perform"],
        "temporal_rule": "decision_then_future_frame",
        "user_control": ["manual", "assisted", "autonomous", "suspended"],
        "personality_revision_schema": "city-realtime-character-personality-v1"
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.2.0', 'published', 1, manifest,
       encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex'), NOW()
FROM core_policy
ON CONFLICT (policy_id, policy_version) DO NOTHING;

CREATE OR REPLACE FUNCTION guard_city_engine_definition_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.version = 'city-openworld-realtime-v2'
       AND NEW.version = OLD.version
       AND NEW.status = OLD.status
       AND NEW.canonical_format = OLD.canonical_format
       AND (
           NEW.capabilities = OLD.capabilities
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_actors","actor_position_events","member_safe_actor_projection"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_agents","agent_policy_binding","agent_lifecycle"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_life","realtime_character_activity","realtime_character_inventory"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_metabolism","realtime_character_metabolism_due_events"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_portals","realtime_character_interiors"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_progression","realtime_character_roles"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_agent_observations","realtime_agent_decisions","realtime_agent_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_agent_character_control","realtime_agent_personality_revisions","realtime_agent_activity_intents","realtime_agent_wakeups"]'::jsonb
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_agent_character_control' THEN capabilities
    ELSE capabilities ||
        '["realtime_agent_character_control","realtime_agent_personality_revisions","realtime_agent_activity_intents","realtime_agent_wakeups"]'::jsonb
END
WHERE version = 'city-openworld-realtime-v2';

-- A2 tables are shared by 1.1 and 1.2 worlds.  The stricter owner/personality
-- feature gate below is intentionally 1.2-only.
CREATE OR REPLACE FUNCTION city_realtime_agent_decision_policy_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM city_realtime_agent_world_bindings binding
        JOIN city_realtime_agent_policy_bundles bundle
          ON bundle.policy_id = binding.policy_id
         AND bundle.policy_version = binding.policy_version
        WHERE binding.world_id = target_world_id
          AND binding.policy_id = 'city-realtime-agent-core'
          AND binding.policy_version IN ('1.1.0', '1.2.0')
          AND bundle.status = 'published'
          AND bundle.policy_hash = binding.policy_hash
    )
$$;

CREATE OR REPLACE FUNCTION city_realtime_agent_personality_policy_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM city_realtime_agent_world_bindings binding
        JOIN city_realtime_agent_policy_bundles bundle
          ON bundle.policy_id = binding.policy_id
         AND bundle.policy_version = binding.policy_version
        WHERE binding.world_id = target_world_id
          AND binding.policy_id = 'city-realtime-agent-core'
          AND binding.policy_version = '1.2.0'
          AND bundle.status = 'published'
          AND bundle.policy_hash = binding.policy_hash
    )
$$;

ALTER TABLE city_realtime_character_action_receipts
    DROP CONSTRAINT IF EXISTS city_realtime_character_action_receipt_check;

ALTER TABLE city_realtime_character_action_receipts
    ADD CONSTRAINT city_realtime_character_action_receipt_check CHECK (
        idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$'
        AND actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND action_type IN ('character.create', 'character.move', 'character.activity', 'character.agent.configure')
        AND request_hash ~ '^[0-9a-f]{64}$'
        AND frame_sequence > 0
        AND jsonb_typeof(result_payload) = 'object'
        AND result_payload::TEXT !~* '"(email|username|owner_user_id|prompt|provider|api_key|secret|memory|response)"[[:space:]]*:'
        AND result_hash ~ '^[0-9a-f]{64}$'
        AND result_hash = encode(sha256(convert_to(result_payload::text, 'UTF8')), 'hex')
    );

-- Control changes have their own append-only event kind.  A control event
-- preserves lifecycle status while changing only the validated control mode;
-- it is not permitted in pre-1.2 worlds.
ALTER TABLE city_realtime_agent_lifecycle_events
    DROP CONSTRAINT IF EXISTS city_realtime_agent_lifecycle_event_check;

ALTER TABLE city_realtime_agent_lifecycle_events
    ADD CONSTRAINT city_realtime_agent_lifecycle_event_check CHECK (
        event_sequence >= 0
        AND frame_sequence >= 0
        AND event_type IN ('spawn', 'lifecycle', 'control')
        AND (from_status IS NULL OR from_status IN ('draft', 'active', 'waiting', 'suspended', 'degraded', 'retiring', 'terminated'))
        AND to_status IN ('draft', 'active', 'waiting', 'suspended', 'degraded', 'retiring', 'terminated')
        AND control_mode IN ('system', 'autonomous', 'assisted', 'manual', 'suspended')
        AND reason_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND (previous_event_hash IS NULL OR previous_event_hash ~ '^[0-9a-f]{64}$')
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
        AND ((event_type = 'spawn' AND event_sequence = 0 AND from_status IS NULL)
             OR (event_type = 'lifecycle' AND event_sequence > 0 AND from_status IS NOT NULL)
             OR (event_type = 'control' AND event_sequence > 0 AND from_status = to_status))
    );

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_lifecycle_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_initialization_enabled(NEW.world_id)
       AND NEW.event_type = 'spawn'
       AND NEW.event_sequence = 0
       AND NEW.frame_sequence = 0 THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.frame_sequence)
       AND (
           NEW.event_type = 'lifecycle'
           OR (NEW.event_type = 'spawn' AND NEW.event_sequence = 0)
           OR (NEW.event_type = 'control' AND city_realtime_agent_personality_policy_enabled(NEW.world_id))
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent lifecycle events are append-only sealed frame facts'
        USING ERRCODE = '55000';
END;
$$;

-- A decision is normalized before it can become an intent.  Keeping the
-- action schema on the decision table stops a rejected/stale row from being a
-- side channel for unsupported future commands.
ALTER TABLE city_realtime_agent_decisions
    DROP CONSTRAINT IF EXISTS city_realtime_agent_decision_action_check;

ALTER TABLE city_realtime_agent_decisions
    ADD CONSTRAINT city_realtime_agent_decision_action_check CHECK (
        (action_code = 'agent.wait' AND arguments = '{}'::jsonb)
        OR
        (action_code = 'character.activity.perform'
         AND jsonb_typeof(arguments) = 'object'
         AND arguments ? 'activity_code'
         AND jsonb_typeof(arguments -> 'activity_code') = 'string'
         AND (arguments - 'activity_code') = '{}'::jsonb
         AND (arguments ->> 'activity_code') ~ '^[a-z][a-z0-9_.-]{1,63}$')
    );

ALTER TABLE city_realtime_agent_intents
    DROP CONSTRAINT IF EXISTS city_realtime_agent_intent_identity_check;

ALTER TABLE city_realtime_agent_intents
    ADD CONSTRAINT city_realtime_agent_intent_identity_check CHECK (
        intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND decision_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND agent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND (actor_code IS NULL OR actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$')
        AND (
            (action_code = 'agent.wait' AND arguments = '{}'::jsonb)
            OR
            (action_code = 'character.activity.perform'
             AND actor_code IS NOT NULL
             AND jsonb_typeof(arguments) = 'object'
             AND arguments ? 'activity_code'
             AND jsonb_typeof(arguments -> 'activity_code') = 'string'
             AND (arguments - 'activity_code') = '{}'::jsonb
             AND (arguments ->> 'activity_code') ~ '^[a-z][a-z0-9_.-]{1,63}$')
        )
        AND arguments_hash ~ '^[0-9a-f]{64}$'
        AND precondition_hash ~ '^[0-9a-f]{64}$'
        AND execute_after_frame_sequence > 0
        AND execute_at_world_time_us >= 0
        AND status IN ('pending', 'applied', 'rejected', 'stale', 'cancelled')
        AND scheduled_frame_sequence > 0
        AND intent_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
        AND ((status = 'pending' AND resolved_frame_sequence IS NULL)
             OR (status IN ('applied', 'rejected', 'stale', 'cancelled')
                 AND resolved_frame_sequence IS NOT NULL
                 AND resolved_frame_sequence > scheduled_frame_sequence))
    );

-- User configuration invalidates queued work before a model sees it.  A
-- worker-held lease is intentionally not rewritten here: the finalizer will
-- detect the changed precondition and seal it stale.  Outbox cancellation is
-- explicit so a queued record cannot be dispatched after its request closed.
ALTER TABLE city_realtime_agent_outbox
    DROP CONSTRAINT IF EXISTS city_realtime_agent_outbox_identity_check;

ALTER TABLE city_realtime_agent_outbox
    ADD CONSTRAINT city_realtime_agent_outbox_identity_check CHECK (
        outbox_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND request_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND dedup_key ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND status IN ('queued', 'leased', 'succeeded', 'failed_terminal', 'cancelled')
        AND created_frame_sequence > 0
        AND jsonb_typeof(metadata) = 'object'
        AND ((status = 'queued' AND lease_owner IS NULL AND lease_expires_at IS NULL AND completed_at IS NULL)
             OR (status = 'leased' AND lease_owner ~ '^[a-z][a-z0-9_.-]{1,63}$'
                 AND lease_expires_at IS NOT NULL AND completed_at IS NULL)
             OR (status IN ('succeeded', 'failed_terminal', 'cancelled') AND lease_owner IS NULL
                 AND lease_expires_at IS NULL AND completed_at IS NOT NULL))
    );

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_decision_request()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_decision_policy_enabled(NEW.world_id)
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.requested_frame_sequence)
       AND NEW.status = 'queued' AND NEW.attempt_count = 0
       AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL
       AND NEW.terminal_frame_sequence IS NULL THEN
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
       AND NEW.metadata = OLD.metadata
       AND NEW.terminal_frame_sequence IS NULL
       AND (
           (OLD.status = 'queued' AND NEW.status = 'leased'
            AND NEW.attempt_count = OLD.attempt_count + 1
            AND NEW.lease_owner IS NOT NULL AND NEW.lease_expires_at IS NOT NULL)
           OR
           (OLD.status = 'leased' AND NEW.status = 'queued'
            AND NEW.attempt_count = OLD.attempt_count
            AND OLD.lease_expires_at <= NOW()
            AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL)
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
       AND NEW.metadata = OLD.metadata
       AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL
       AND NEW.terminal_frame_sequence > OLD.requested_frame_sequence THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_agent_personality_policy_enabled(NEW.world_id)
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.terminal_frame_sequence)
       AND OLD.status = 'queued' AND NEW.status = 'cancelled'
       AND NEW.world_id = OLD.world_id AND NEW.request_code = OLD.request_code
       AND NEW.agent_code = OLD.agent_code AND NEW.observation_code = OLD.observation_code
       AND NEW.observation_hash = OLD.observation_hash AND NEW.precondition_hash = OLD.precondition_hash
       AND NEW.observed_frame_sequence = OLD.observed_frame_sequence
       AND NEW.expires_at_world_time_us = OLD.expires_at_world_time_us
       AND NEW.attempt_count = OLD.attempt_count
       AND NEW.requested_frame_sequence = OLD.requested_frame_sequence
       AND NEW.metadata = OLD.metadata
       AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL
       AND NEW.terminal_frame_sequence > OLD.requested_frame_sequence THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent decision requests require a sealed frame or worker lease transition'
        USING ERRCODE = '55000';
END;
$$;

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
                AND OLD.lease_expires_at <= NOW()
                AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL AND NEW.completed_at IS NULL)
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

CREATE TABLE IF NOT EXISTS city_realtime_character_agent_personality_revisions (
    world_id BIGINT NOT NULL,
    agent_code VARCHAR(96) NOT NULL,
    revision BIGINT NOT NULL,
    seed JSONB NOT NULL,
    seed_hash VARCHAR(64) NOT NULL,
    previous_seed_hash VARCHAR(64),
    changed_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_frame_sequence BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, agent_code, revision),
    CONSTRAINT city_realtime_character_agent_personality_agent_fk
        FOREIGN KEY (world_id, agent_code)
        REFERENCES city_realtime_agent_instances(world_id, agent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_agent_personality_frame_fk
        FOREIGN KEY (world_id, created_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_agent_personality_check CHECK (
        revision > 0
        AND jsonb_typeof(seed) = 'object'
        AND seed_hash ~ '^[0-9a-f]{64}$'
        AND (previous_seed_hash IS NULL OR previous_seed_hash ~ '^[0-9a-f]{64}$')
        AND changed_by_user_id > 0
        AND created_frame_sequence > 0
        AND seed::text !~* '"(api_key|secret|token|password|credential)"[[:space:]]*:'
        AND ((revision = 1 AND previous_seed_hash IS NULL)
             OR (revision > 1 AND previous_seed_hash IS NOT NULL))
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_agent_personality_latest
    ON city_realtime_character_agent_personality_revisions (world_id, agent_code, revision DESC);

CREATE OR REPLACE FUNCTION guard_city_realtime_character_agent_personality_revision()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    latest_revision BIGINT;
    latest_seed_hash VARCHAR(64);
    agent_owner_user_id BIGINT;
BEGIN
    IF TG_OP <> 'INSERT'
       OR NOT city_realtime_agent_personality_policy_enabled(NEW.world_id)
       OR NOT city_realtime_agent_mutation_enabled(NEW.world_id, NEW.created_frame_sequence) THEN
        RAISE EXCEPTION 'character Agent personality revisions are append-only sealed owner facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT owner_user_id
    INTO agent_owner_user_id
    FROM city_realtime_agent_instances
    WHERE world_id = NEW.world_id
      AND agent_code = NEW.agent_code
      AND agent_subtype = 'character.user';
    IF NOT FOUND OR agent_owner_user_id IS NULL OR agent_owner_user_id <> NEW.changed_by_user_id THEN
        RAISE EXCEPTION 'character Agent personality revision owner mismatch'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.seed_hash <> encode(sha256(convert_to(NEW.seed::text, 'UTF8')), 'hex') THEN
        RAISE EXCEPTION 'character Agent personality revision hash mismatch'
            USING ERRCODE = '23514';
    END IF;
    SELECT revision, seed_hash
    INTO latest_revision, latest_seed_hash
    FROM city_realtime_character_agent_personality_revisions
    WHERE world_id = NEW.world_id AND agent_code = NEW.agent_code
    ORDER BY revision DESC
    LIMIT 1;
    IF NOT FOUND THEN
        IF NEW.revision <> 1 OR NEW.previous_seed_hash IS NOT NULL THEN
            RAISE EXCEPTION 'initial character Agent personality revision mismatch'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.revision <> latest_revision + 1 OR NEW.previous_seed_hash <> latest_seed_hash THEN
        RAISE EXCEPTION 'character Agent personality revision chain mismatch'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_agent_personality_revision_guard
    ON city_realtime_character_agent_personality_revisions;
CREATE TRIGGER city_realtime_character_agent_personality_revision_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_agent_personality_revisions
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_agent_personality_revision();

COMMENT ON TABLE city_realtime_character_agent_personality_revisions IS
    'Owner-scoped immutable Character Agent personality seed revisions. They are not canonical city state and are never included in public actor projections.';
