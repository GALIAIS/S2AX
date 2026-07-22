-- A2: durable Agent observations, decision requests/attempts, normalized
-- decisions and future-frame intents. This migration intentionally contains no
-- provider credential, prompt, memory body, platform wallet, or user-facing
-- control plane. External work remains non-canonical until a sealed intent is
-- scheduled for a later Temporal Frame.

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
        "actions": ["agent.wait"],
        "temporal_rule": "decision_then_future_frame"
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.1.0', 'published', 1, manifest,
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
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_agent_decisions' THEN capabilities
    ELSE capabilities ||
        '["realtime_agent_observations","realtime_agent_decisions","realtime_agent_intents"]'::jsonb
END
WHERE version = 'city-openworld-realtime-v2';

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
          AND binding.policy_version = '1.1.0'
          AND bundle.status = 'published'
          AND bundle.policy_hash = binding.policy_hash
    )
$$;

CREATE OR REPLACE FUNCTION city_realtime_agent_worker_mutation_enabled(
    target_world_id BIGINT,
    target_request_code VARCHAR
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_agent_worker_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_agent_worker_request_code', TRUE) = target_request_code
       AND city_realtime_agent_decision_policy_enabled(target_world_id)
$$;

CREATE TABLE IF NOT EXISTS city_realtime_agent_observations (
    world_id BIGINT NOT NULL REFERENCES city_realtime_agent_world_bindings(world_id) ON DELETE RESTRICT,
    observation_code VARCHAR(96) NOT NULL,
    agent_code VARCHAR(96) NOT NULL,
    observed_frame_sequence BIGINT NOT NULL,
    observed_timeline_cursor VARCHAR(32) NOT NULL,
    observed_world_time_us BIGINT NOT NULL,
    observation_schema_version SMALLINT NOT NULL,
    observation_schema_hash VARCHAR(64) NOT NULL,
    redaction_policy_code VARCHAR(64) NOT NULL,
    trigger_key VARCHAR(96) NOT NULL,
    payload JSONB NOT NULL,
    payload_hash VARCHAR(64) NOT NULL,
    precondition_hash VARCHAR(64) NOT NULL,
    expires_at_world_time_us BIGINT NOT NULL,
    created_frame_sequence BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, observation_code),
    CONSTRAINT city_realtime_agent_observation_agent_fk
        FOREIGN KEY (world_id, agent_code)
        REFERENCES city_realtime_agent_instances(world_id, agent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_observation_observed_frame_fk
        FOREIGN KEY (world_id, observed_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_observation_created_frame_fk
        FOREIGN KEY (world_id, created_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_observation_identity_check CHECK (
        observation_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND agent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND observed_frame_sequence > 0
        AND observed_timeline_cursor ~ '^twf_[0-9]{12}$'
        AND observed_world_time_us >= 0
        AND observation_schema_version = 1
        AND observation_schema_hash ~ '^[0-9a-f]{64}$'
        AND redaction_policy_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND trigger_key ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND jsonb_typeof(payload) = 'object'
        AND payload_hash ~ '^[0-9a-f]{64}$'
        AND precondition_hash ~ '^[0-9a-f]{64}$'
        AND expires_at_world_time_us >= observed_world_time_us
        AND created_frame_sequence = observed_frame_sequence
        AND jsonb_typeof(metadata) = 'object'
    ),
    CONSTRAINT city_realtime_agent_observation_unique_trigger
        UNIQUE (world_id, agent_code, observed_frame_sequence, observation_schema_hash, trigger_key)
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_observations_world_agent_frame
    ON city_realtime_agent_observations (world_id, agent_code, observed_frame_sequence DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uq_city_realtime_agent_observations_trigger
    ON city_realtime_agent_observations (world_id, agent_code, trigger_key);

CREATE TABLE IF NOT EXISTS city_realtime_agent_decision_requests (
    world_id BIGINT NOT NULL,
    request_code VARCHAR(96) NOT NULL,
    agent_code VARCHAR(96) NOT NULL,
    observation_code VARCHAR(96) NOT NULL,
    observation_hash VARCHAR(64) NOT NULL,
    precondition_hash VARCHAR(64) NOT NULL,
    observed_frame_sequence BIGINT NOT NULL,
    expires_at_world_time_us BIGINT NOT NULL,
    status VARCHAR(24) NOT NULL,
    attempt_count SMALLINT NOT NULL DEFAULT 0,
    lease_owner VARCHAR(64),
    lease_expires_at TIMESTAMPTZ,
    requested_frame_sequence BIGINT NOT NULL,
    terminal_frame_sequence BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, request_code),
    CONSTRAINT city_realtime_agent_decision_request_agent_fk
        FOREIGN KEY (world_id, agent_code)
        REFERENCES city_realtime_agent_instances(world_id, agent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_decision_request_observation_fk
        FOREIGN KEY (world_id, observation_code)
        REFERENCES city_realtime_agent_observations(world_id, observation_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_decision_request_frame_fk
        FOREIGN KEY (world_id, requested_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_decision_request_terminal_frame_fk
        FOREIGN KEY (world_id, terminal_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_decision_request_identity_check CHECK (
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
            (status = 'queued' AND lease_owner IS NULL AND lease_expires_at IS NULL AND terminal_frame_sequence IS NULL)
            OR (status = 'leased' AND lease_owner ~ '^[a-z][a-z0-9_.-]{1,63}$' AND lease_expires_at IS NOT NULL AND terminal_frame_sequence IS NULL)
            OR (status IN ('accepted', 'rejected', 'stale', 'failed_terminal', 'cancelled')
                AND lease_owner IS NULL AND lease_expires_at IS NULL
                AND terminal_frame_sequence IS NOT NULL AND terminal_frame_sequence > requested_frame_sequence)
        )
    ),
    CONSTRAINT city_realtime_agent_decision_request_one_observation UNIQUE (world_id, observation_code)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_realtime_agent_decision_requests_active_agent
    ON city_realtime_agent_decision_requests (world_id, agent_code)
    WHERE status IN ('queued', 'leased');
CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_decision_requests_status
    ON city_realtime_agent_decision_requests (world_id, status, expires_at_world_time_us, request_code);

CREATE TABLE IF NOT EXISTS city_realtime_agent_decision_attempts (
    world_id BIGINT NOT NULL,
    attempt_code VARCHAR(96) NOT NULL,
    request_code VARCHAR(96) NOT NULL,
    attempt_number SMALLINT NOT NULL,
    provider_code VARCHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    response_hash VARCHAR(64),
    error_code VARCHAR(64),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, attempt_code),
    CONSTRAINT city_realtime_agent_decision_attempt_request_fk
        FOREIGN KEY (world_id, request_code)
        REFERENCES city_realtime_agent_decision_requests(world_id, request_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_decision_attempt_identity_check CHECK (
        attempt_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND request_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND attempt_number > 0
        AND provider_code = 'fake.deterministic'
        AND status IN ('started', 'succeeded', 'failed')
        AND request_hash ~ '^[0-9a-f]{64}$'
        AND (response_hash IS NULL OR response_hash ~ '^[0-9a-f]{64}$')
        AND (error_code IS NULL OR error_code ~ '^[a-z][a-z0-9_.-]{1,63}$')
        AND jsonb_typeof(metadata) = 'object'
        AND (
            (status = 'started' AND response_hash IS NULL AND error_code IS NULL AND completed_at IS NULL)
            OR (status = 'succeeded' AND response_hash ~ '^[0-9a-f]{64}$' AND error_code IS NULL AND completed_at IS NOT NULL)
            OR (status = 'failed' AND response_hash IS NULL AND error_code IS NOT NULL AND completed_at IS NOT NULL)
        )
    ),
    CONSTRAINT city_realtime_agent_decision_attempt_unique_number UNIQUE (world_id, request_code, attempt_number)
);

CREATE TABLE IF NOT EXISTS city_realtime_agent_decisions (
    world_id BIGINT NOT NULL,
    decision_code VARCHAR(96) NOT NULL,
    request_code VARCHAR(96) NOT NULL,
    attempt_code VARCHAR(96) NOT NULL,
    decision_index SMALLINT NOT NULL,
    decision_status VARCHAR(16) NOT NULL,
    action_code VARCHAR(64) NOT NULL,
    arguments JSONB NOT NULL,
    arguments_hash VARCHAR(64) NOT NULL,
    observation_hash VARCHAR(64) NOT NULL,
    precondition_hash VARCHAR(64) NOT NULL,
    reason_code VARCHAR(64) NOT NULL,
    intent_code VARCHAR(96),
    resolved_frame_sequence BIGINT NOT NULL,
    decision_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, decision_code),
    CONSTRAINT city_realtime_agent_decision_request_fk
        FOREIGN KEY (world_id, request_code)
        REFERENCES city_realtime_agent_decision_requests(world_id, request_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_decision_attempt_fk
        FOREIGN KEY (world_id, attempt_code)
        REFERENCES city_realtime_agent_decision_attempts(world_id, attempt_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_decision_frame_fk
        FOREIGN KEY (world_id, resolved_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_decision_identity_check CHECK (
        decision_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND request_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND attempt_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND decision_index = 0
        AND decision_status IN ('accepted', 'rejected', 'stale')
        AND action_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND jsonb_typeof(arguments) = 'object'
        AND arguments_hash ~ '^[0-9a-f]{64}$'
        AND observation_hash ~ '^[0-9a-f]{64}$'
        AND precondition_hash ~ '^[0-9a-f]{64}$'
        AND reason_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND (intent_code IS NULL OR intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$')
        AND resolved_frame_sequence > 0
        AND decision_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
        AND ((decision_status = 'accepted' AND intent_code IS NOT NULL)
             OR (decision_status IN ('rejected', 'stale') AND intent_code IS NULL))
    ),
    CONSTRAINT city_realtime_agent_decision_one_per_request UNIQUE (world_id, request_code, decision_index)
);

CREATE TABLE IF NOT EXISTS city_realtime_agent_intents (
    world_id BIGINT NOT NULL,
    intent_code VARCHAR(96) NOT NULL,
    decision_code VARCHAR(96) NOT NULL,
    agent_code VARCHAR(96) NOT NULL,
    actor_code VARCHAR(96),
    action_code VARCHAR(64) NOT NULL,
    arguments JSONB NOT NULL,
    arguments_hash VARCHAR(64) NOT NULL,
    precondition_hash VARCHAR(64) NOT NULL,
    execute_after_frame_sequence BIGINT NOT NULL,
    execute_at_world_time_us BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL,
    scheduled_frame_sequence BIGINT NOT NULL,
    resolved_frame_sequence BIGINT,
    intent_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, intent_code),
    CONSTRAINT city_realtime_agent_intent_decision_fk
        FOREIGN KEY (world_id, decision_code)
        REFERENCES city_realtime_agent_decisions(world_id, decision_code) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_intent_agent_fk
        FOREIGN KEY (world_id, agent_code)
        REFERENCES city_realtime_agent_instances(world_id, agent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_intent_actor_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_intent_scheduled_frame_fk
        FOREIGN KEY (world_id, scheduled_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_intent_resolved_frame_fk
        FOREIGN KEY (world_id, resolved_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_intent_identity_check CHECK (
        intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND decision_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND agent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND (actor_code IS NULL OR actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$')
        AND action_code = 'agent.wait'
        AND arguments = '{}'::jsonb
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
    ),
    CONSTRAINT city_realtime_agent_intent_one_per_decision UNIQUE (world_id, decision_code)
);

ALTER TABLE city_realtime_agent_decisions
    DROP CONSTRAINT IF EXISTS city_realtime_agent_decision_intent_fk;
ALTER TABLE city_realtime_agent_decisions
    ADD CONSTRAINT city_realtime_agent_decision_intent_fk
    FOREIGN KEY (world_id, intent_code)
    REFERENCES city_realtime_agent_intents(world_id, intent_code) ON DELETE RESTRICT
    DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_intents_pending
    ON city_realtime_agent_intents (world_id, execute_at_world_time_us, intent_code)
    WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS city_realtime_agent_outbox (
    world_id BIGINT NOT NULL,
    outbox_code VARCHAR(96) NOT NULL,
    request_code VARCHAR(96) NOT NULL,
    dedup_key VARCHAR(160) NOT NULL,
    status VARCHAR(24) NOT NULL,
    lease_owner VARCHAR(64),
    lease_expires_at TIMESTAMPTZ,
    created_frame_sequence BIGINT NOT NULL,
    completed_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, outbox_code),
    CONSTRAINT city_realtime_agent_outbox_request_fk
        FOREIGN KEY (world_id, request_code)
        REFERENCES city_realtime_agent_decision_requests(world_id, request_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_outbox_frame_fk
        FOREIGN KEY (world_id, created_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_outbox_identity_check CHECK (
        outbox_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND request_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND dedup_key ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND status IN ('queued', 'leased', 'succeeded', 'failed_terminal')
        AND created_frame_sequence > 0
        AND jsonb_typeof(metadata) = 'object'
        AND ((status = 'queued' AND lease_owner IS NULL AND lease_expires_at IS NULL AND completed_at IS NULL)
             OR (status = 'leased' AND lease_owner ~ '^[a-z][a-z0-9_.-]{1,63}$'
                 AND lease_expires_at IS NOT NULL AND completed_at IS NULL)
             OR (status IN ('succeeded', 'failed_terminal') AND lease_owner IS NULL
                 AND lease_expires_at IS NULL AND completed_at IS NOT NULL))
    ),
    CONSTRAINT city_realtime_agent_outbox_one_request UNIQUE (world_id, request_code),
    CONSTRAINT city_realtime_agent_outbox_dedup UNIQUE (world_id, dedup_key)
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_outbox_dispatch
    ON city_realtime_agent_outbox (world_id, status, created_at, outbox_code)
    WHERE status IN ('queued', 'leased');

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_observation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_decision_policy_enabled(NEW.world_id)
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.created_frame_sequence)
       AND NEW.observed_frame_sequence = NEW.created_frame_sequence THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent observations are immutable sealed-frame snapshots'
        USING ERRCODE = '55000';
END;
$$;

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
    RAISE EXCEPTION 'city realtime agent decision requests require a sealed frame or worker lease transition'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_decision_attempt()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_worker_mutation_enabled(NEW.world_id, NEW.request_code)
       AND NEW.status = 'started' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_agent_worker_mutation_enabled(NEW.world_id, NEW.request_code)
       AND OLD.status = 'started' AND NEW.status IN ('succeeded', 'failed')
       AND NEW.world_id = OLD.world_id AND NEW.attempt_code = OLD.attempt_code
       AND NEW.request_code = OLD.request_code AND NEW.attempt_number = OLD.attempt_number
       AND NEW.provider_code = OLD.provider_code AND NEW.request_hash = OLD.request_hash
       AND NEW.metadata = OLD.metadata THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent decision attempts are worker-audited append-only records'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_decision()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_decision_policy_enabled(NEW.world_id)
       AND city_realtime_agent_worker_mutation_enabled(NEW.world_id, NEW.request_code)
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.resolved_frame_sequence) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent decisions are immutable sealed-frame facts'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_intent()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_decision_policy_enabled(NEW.world_id)
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.scheduled_frame_sequence)
       AND NEW.status = 'pending' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_agent_decision_policy_enabled(NEW.world_id)
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.resolved_frame_sequence)
       AND OLD.status = 'pending' AND NEW.status IN ('applied', 'rejected', 'stale', 'cancelled')
       AND NEW.world_id = OLD.world_id AND NEW.intent_code = OLD.intent_code
       AND NEW.decision_code = OLD.decision_code AND NEW.agent_code = OLD.agent_code
       AND NEW.actor_code IS NOT DISTINCT FROM OLD.actor_code
       AND NEW.action_code = OLD.action_code AND NEW.arguments = OLD.arguments
       AND NEW.arguments_hash = OLD.arguments_hash AND NEW.precondition_hash = OLD.precondition_hash
       AND NEW.execute_after_frame_sequence = OLD.execute_after_frame_sequence
       AND NEW.execute_at_world_time_us = OLD.execute_at_world_time_us
       AND NEW.scheduled_frame_sequence = OLD.scheduled_frame_sequence
       AND NEW.intent_hash = OLD.intent_hash AND NEW.metadata = OLD.metadata
       AND NEW.resolved_frame_sequence > OLD.scheduled_frame_sequence THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent intents may change only through the sealed due-event reducer'
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
    RAISE EXCEPTION 'city realtime agent outbox changes require the worker gate'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_agent_observation_guard ON city_realtime_agent_observations;
CREATE TRIGGER city_realtime_agent_observation_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_observations
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_observation();

DROP TRIGGER IF EXISTS city_realtime_agent_decision_request_guard ON city_realtime_agent_decision_requests;
CREATE TRIGGER city_realtime_agent_decision_request_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_decision_requests
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_decision_request();

DROP TRIGGER IF EXISTS city_realtime_agent_decision_attempt_guard ON city_realtime_agent_decision_attempts;
CREATE TRIGGER city_realtime_agent_decision_attempt_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_decision_attempts
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_decision_attempt();

DROP TRIGGER IF EXISTS city_realtime_agent_decision_guard ON city_realtime_agent_decisions;
CREATE TRIGGER city_realtime_agent_decision_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_decisions
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_decision();

DROP TRIGGER IF EXISTS city_realtime_agent_intent_guard ON city_realtime_agent_intents;
CREATE TRIGGER city_realtime_agent_intent_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_intents
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_intent();

DROP TRIGGER IF EXISTS city_realtime_agent_outbox_guard ON city_realtime_agent_outbox;
CREATE TRIGGER city_realtime_agent_outbox_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_outbox
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_outbox();

COMMENT ON TABLE city_realtime_agent_observations IS
    'Immutable scope-filtered Agent observation snapshots. Payloads contain no platform credentials or provider response.';
COMMENT ON TABLE city_realtime_agent_decision_requests IS
    'Durable Agent inference work items; only unresolved request descriptors enter the canonical state.';
COMMENT ON TABLE city_realtime_agent_decision_attempts IS
    'Worker audit for deterministic fake-provider attempts and later bounded model routes.';
COMMENT ON TABLE city_realtime_agent_decisions IS
    'Immutable normalized Agent output. It cannot directly mutate city state.';
COMMENT ON TABLE city_realtime_agent_intents IS
    'Sealed future-frame Agent-origin intents. The due-event reducer is their only execution boundary.';
COMMENT ON TABLE city_realtime_agent_outbox IS
    'Typed inference outbox with independent worker leases; it is not shared with account-pool jobs.';
