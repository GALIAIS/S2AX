-- A3.3 / bounded social slice. 1.5.0 adds one server-derived action:
-- character.social.greet. It records only a coarse, append-only relation fact;
-- no chat text, model output, private identity, wallet, reward, or legal state
-- crosses this adapter.

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
        "actions": [
          "agent.wait",
          "character.activity.perform",
          "character.case.acknowledge",
          "character.move",
          "character.portal.traverse",
          "character.role.change",
          "character.social.greet"
        ],
        "temporal_rule": "decision_then_future_frame",
        "user_control": ["manual", "assisted", "autonomous", "suspended"],
        "personality_revision_schema": "city-realtime-character-personality-v1",
        "action_context_schema": "city-realtime-character-action-context-v3",
        "case_response_schema": "city-realtime-character-case-response-v1",
        "social_response_schema": "city-realtime-character-social-v1"
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.5.0', 'published', 1, manifest,
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
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_agent_character_navigation_intents","realtime_agent_character_role_intents","realtime_agent_action_context"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_case_response","realtime_agent_case_response_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_social","realtime_agent_social_intents"]'::jsonb
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_social' THEN capabilities
    ELSE capabilities || '["realtime_character_social","realtime_agent_social_intents"]'::jsonb
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
          AND binding.policy_version IN ('1.1.0', '1.2.0', '1.3.0', '1.4.0', '1.5.0')
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
          AND binding.policy_version IN ('1.2.0', '1.3.0', '1.4.0', '1.5.0')
          AND bundle.status = 'published'
          AND bundle.policy_hash = binding.policy_hash
    )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_response_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_response_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0
             AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.4.0', '1.5.0')
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_response_mutation_enabled(
    target_world_id BIGINT,
    target_frame_sequence BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_response_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_case_response_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_character_case_response_world_bindings response ON response.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.4.0', '1.5.0')
             AND response.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_response_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_agent_binding_hash VARCHAR(64);
    expected_policy_id VARCHAR(96);
    expected_policy_version VARCHAR(24);
    expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_response_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-response binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT policy_id, policy_version, binding_hash
    INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings WHERE world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR
       expected_policy_version NOT IN ('1.4.0', '1.5.0') OR NEW.schema_version <> 1 OR
       NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-response binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-response-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-response binding hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TABLE IF NOT EXISTS city_realtime_character_social_world_bindings (
    world_id BIGINT PRIMARY KEY REFERENCES city_realtime_agent_world_bindings(world_id) ON DELETE RESTRICT,
    schema_version SMALLINT NOT NULL,
    agent_binding_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_character_social_binding_check CHECK (
        schema_version = 1
        AND agent_binding_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash = encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-social-binding-v1', agent_binding_hash), 'UTF8')), 'hex')
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_social_heads (
    world_id BIGINT NOT NULL REFERENCES city_realtime_character_social_world_bindings(world_id) ON DELETE RESTRICT,
    actor_code_low VARCHAR(96) NOT NULL,
    actor_code_high VARCHAR(96) NOT NULL,
    relation_revision BIGINT NOT NULL DEFAULT 0,
    last_frame_sequence BIGINT NOT NULL DEFAULT 0,
    affinity_milli BIGINT NOT NULL DEFAULT 0,
    interaction_count BIGINT NOT NULL DEFAULT 0,
    event_chain_hash VARCHAR(64) NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code_low, actor_code_high),
    CONSTRAINT city_realtime_character_social_head_low_fk
        FOREIGN KEY (world_id, actor_code_low)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_social_head_high_fk
        FOREIGN KEY (world_id, actor_code_high)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_social_head_check CHECK (
        actor_code_low ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND actor_code_high ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND actor_code_low < actor_code_high
        AND relation_revision >= 0
        AND last_frame_sequence >= 0
        AND affinity_milli BETWEEN 0 AND 1000
        AND interaction_count >= 0
        AND event_chain_hash ~ '^[0-9a-f]{64}$'
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND state_hash = encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-social-state-v1', actor_code_low, actor_code_high,
            relation_revision::text, last_frame_sequence::text, affinity_milli::text,
            interaction_count::text, event_chain_hash), 'UTF8')), 'hex')
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_social_events (
    world_id BIGINT NOT NULL,
    actor_code_low VARCHAR(96) NOT NULL,
    actor_code_high VARCHAR(96) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    interaction_code VARCHAR(32) NOT NULL,
    initiator_code VARCHAR(96) NOT NULL,
    recipient_code VARCHAR(96) NOT NULL,
    source_intent_code VARCHAR(96) NOT NULL,
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code_low, actor_code_high, event_sequence),
    CONSTRAINT city_realtime_character_social_event_head_fk
        FOREIGN KEY (world_id, actor_code_low, actor_code_high)
        REFERENCES city_realtime_character_social_heads(world_id, actor_code_low, actor_code_high) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_social_event_intent_fk
        FOREIGN KEY (world_id, source_intent_code)
        REFERENCES city_realtime_agent_intents(world_id, intent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_social_event_check CHECK (
        event_sequence > 0
        AND frame_sequence > 0
        AND interaction_code = 'greeted'
        AND initiator_code <> recipient_code
        AND (initiator_code = actor_code_low OR initiator_code = actor_code_high)
        AND (recipient_code = actor_code_low OR recipient_code = actor_code_high)
        AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND previous_event_hash ~ '^[0-9a-f]{64}$'
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND event_hash = encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-social-event-v1', actor_code_low, actor_code_high,
            event_sequence::text, frame_sequence::text, interaction_code,
            initiator_code, recipient_code, source_intent_code, previous_event_hash), 'UTF8')), 'hex')
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_social_heads_actor
    ON city_realtime_character_social_heads (world_id, actor_code_low, actor_code_high);
CREATE INDEX IF NOT EXISTS idx_city_realtime_character_social_events_frame
    ON city_realtime_character_social_events (world_id, frame_sequence, actor_code_low, actor_code_high);

CREATE OR REPLACE FUNCTION city_realtime_character_social_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_social_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core' AND agent.policy_version = '1.5.0'
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_social_mutation_enabled(
    target_world_id BIGINT,
    target_frame_sequence BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_social_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_social_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_character_social_world_bindings social ON social.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core' AND agent.policy_version = '1.5.0'
             AND social.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_social_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_binding_hash VARCHAR(64);
    expected_agent_binding_hash VARCHAR(64);
    expected_policy_version VARCHAR(24);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_social_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character social binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT binding_hash, policy_version INTO expected_agent_binding_hash, expected_policy_version
    FROM city_realtime_agent_world_bindings WHERE world_id = NEW.world_id;
    IF expected_policy_version <> '1.5.0' OR NEW.schema_version <> 1 OR
       NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character social binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-social-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime character social binding hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_social_head()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_genesis_hash VARCHAR(64);
    expected_state_hash VARCHAR(64);
BEGIN
    IF TG_OP = 'INSERT' AND city_realtime_character_social_mutation_enabled(
        NEW.world_id, NULLIF(current_setting('sub2api.city_realtime_character_social_frame_sequence', TRUE), '')::BIGINT) THEN
        expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-social-chain-v1', NEW.actor_code_low, NEW.actor_code_high), 'UTF8')), 'hex');
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-social-state-v1', NEW.actor_code_low, NEW.actor_code_high,
            NEW.relation_revision::text, NEW.last_frame_sequence::text, NEW.affinity_milli::text,
            NEW.interaction_count::text, NEW.event_chain_hash), 'UTF8')), 'hex');
        IF NEW.relation_revision <> 0 OR NEW.last_frame_sequence <> 0 OR NEW.affinity_milli <> 0 OR
           NEW.interaction_count <> 0 OR NEW.event_chain_hash <> expected_genesis_hash OR
           NEW.state_hash <> expected_state_hash THEN
            RAISE EXCEPTION 'city realtime character social genesis head mismatch' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_realtime_character_social_mutation_enabled(NEW.world_id, NEW.last_frame_sequence)
       AND NEW.actor_code_low = OLD.actor_code_low AND NEW.actor_code_high = OLD.actor_code_high
       AND NEW.relation_revision = OLD.relation_revision + 1
       AND NEW.last_frame_sequence > OLD.last_frame_sequence
       AND NEW.interaction_count = OLD.interaction_count + 1
       AND NEW.affinity_milli >= OLD.affinity_milli AND NEW.affinity_milli <= 1000
       AND NEW.event_chain_hash <> OLD.event_chain_hash AND NEW.metadata = OLD.metadata
       AND EXISTS (
           SELECT 1 FROM city_realtime_character_social_events event
           WHERE event.world_id = NEW.world_id AND event.actor_code_low = NEW.actor_code_low
             AND event.actor_code_high = NEW.actor_code_high AND event.event_sequence = NEW.relation_revision
             AND event.frame_sequence = NEW.last_frame_sequence AND event.event_hash = NEW.event_chain_hash
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character social heads may change only through a sealed reducer' USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_social_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    head_revision BIGINT;
    head_last_frame BIGINT;
    head_chain_hash VARCHAR(64);
    intent_actor_code VARCHAR(96);
    intent_action_code VARCHAR(64);
    intent_status VARCHAR(16);
    expected_event_hash VARCHAR(64);
    initiator_status VARCHAR(16);
    recipient_status VARCHAR(16);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_social_mutation_enabled(NEW.world_id, NEW.frame_sequence) THEN
        RAISE EXCEPTION 'city realtime character social events are append-only sealed facts' USING ERRCODE = '55000';
    END IF;
    SELECT relation_revision, last_frame_sequence, event_chain_hash
    INTO head_revision, head_last_frame, head_chain_hash
    FROM city_realtime_character_social_heads
    WHERE world_id = NEW.world_id AND actor_code_low = NEW.actor_code_low AND actor_code_high = NEW.actor_code_high;
    IF NOT FOUND OR NEW.event_sequence <> head_revision + 1 OR NEW.frame_sequence <= head_last_frame OR
       NEW.previous_event_hash <> head_chain_hash THEN
        RAISE EXCEPTION 'city realtime character social event chain mismatch' USING ERRCODE = '23514';
    END IF;
    SELECT lifecycle_status INTO initiator_status
    FROM city_realtime_actor_identities WHERE world_id = NEW.world_id AND actor_code = NEW.initiator_code;
    SELECT lifecycle_status INTO recipient_status
    FROM city_realtime_actor_identities WHERE world_id = NEW.world_id AND actor_code = NEW.recipient_code;
    IF initiator_status <> 'active' OR recipient_status <> 'active' THEN
        RAISE EXCEPTION 'city realtime character social event requires active participants' USING ERRCODE = '23514';
    END IF;
    SELECT actor_code, action_code, status INTO intent_actor_code, intent_action_code, intent_status
    FROM city_realtime_agent_intents WHERE world_id = NEW.world_id AND intent_code = NEW.source_intent_code;
    IF NOT FOUND OR intent_actor_code <> NEW.initiator_code OR intent_action_code <> 'character.social.greet' OR
       intent_status <> 'pending' THEN
        RAISE EXCEPTION 'city realtime character social event must have a pending matching intent' USING ERRCODE = '23514';
    END IF;
    expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-social-event-v1', NEW.actor_code_low, NEW.actor_code_high,
        NEW.event_sequence::text, NEW.frame_sequence::text, NEW.interaction_code,
        NEW.initiator_code, NEW.recipient_code, NEW.source_intent_code, NEW.previous_event_hash), 'UTF8')), 'hex');
    IF NEW.event_hash <> expected_event_hash THEN
        RAISE EXCEPTION 'city realtime character social event hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_social_binding_guard
    ON city_realtime_character_social_world_bindings;
CREATE TRIGGER city_realtime_character_social_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_social_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_social_binding();

DROP TRIGGER IF EXISTS city_realtime_character_social_head_guard
    ON city_realtime_character_social_heads;
CREATE TRIGGER city_realtime_character_social_head_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_social_heads
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_social_head();

DROP TRIGGER IF EXISTS city_realtime_character_social_event_guard
    ON city_realtime_character_social_events;
CREATE TRIGGER city_realtime_character_social_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_social_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_social_event();

ALTER TABLE city_realtime_agent_decisions
    DROP CONSTRAINT IF EXISTS city_realtime_agent_decision_action_check;

ALTER TABLE city_realtime_agent_decisions
    ADD CONSTRAINT city_realtime_agent_decision_action_check CHECK (
        (action_code = 'agent.wait' AND arguments = '{}'::jsonb)
        OR (action_code = 'character.activity.perform' AND jsonb_typeof(arguments) = 'object'
            AND arguments ? 'activity_code' AND jsonb_typeof(arguments -> 'activity_code') = 'string'
            AND (arguments - 'activity_code') = '{}'::jsonb
            AND (arguments ->> 'activity_code') ~ '^[a-z][a-z0-9_.-]{1,63}$')
        OR (action_code = 'character.case.acknowledge' AND jsonb_typeof(arguments) = 'object'
            AND arguments ? 'case_code' AND jsonb_typeof(arguments -> 'case_code') = 'string'
            AND (arguments - 'case_code') = '{}'::jsonb
            AND (arguments ->> 'case_code') ~ '^law[.][0-9a-f]{16}[.][1-9][0-9]*$')
        OR (action_code = 'character.social.greet' AND jsonb_typeof(arguments) = 'object'
            AND arguments ? 'target_actor_code' AND jsonb_typeof(arguments -> 'target_actor_code') = 'string'
            AND (arguments - 'target_actor_code') = '{}'::jsonb
            AND (arguments ->> 'target_actor_code') ~ '^[a-z][a-z0-9_.-]{1,95}$')
        OR (action_code = 'character.move' AND jsonb_typeof(arguments) = 'object'
            AND arguments ?& ARRAY['x', 'y', 'z'] AND (arguments - ARRAY['x', 'y', 'z']) = '{}'::jsonb
            AND jsonb_typeof(arguments -> 'x') = 'number' AND jsonb_typeof(arguments -> 'y') = 'number'
            AND jsonb_typeof(arguments -> 'z') = 'number' AND (arguments ->> 'x') ~ '^-?[0-9]+$'
            AND (arguments ->> 'y') ~ '^-?[0-9]+$' AND (arguments ->> 'z') ~ '^-?[0-9]+$')
        OR (action_code = 'character.portal.traverse' AND jsonb_typeof(arguments) = 'object'
            AND arguments ? 'portal_code' AND jsonb_typeof(arguments -> 'portal_code') = 'string'
            AND (arguments - 'portal_code') = '{}'::jsonb
            AND (arguments ->> 'portal_code') ~ '^[a-z][a-z0-9_.-]{1,127}$')
        OR (action_code = 'character.role.change' AND jsonb_typeof(arguments) = 'object'
            AND arguments ? 'role_code' AND jsonb_typeof(arguments -> 'role_code') = 'string'
            AND (arguments - 'role_code') = '{}'::jsonb
            AND (arguments ->> 'role_code') ~ '^[a-z][a-z0-9_.-]{1,95}$')
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
            OR (action_code = 'character.activity.perform' AND actor_code IS NOT NULL
                AND jsonb_typeof(arguments) = 'object' AND arguments ? 'activity_code'
                AND jsonb_typeof(arguments -> 'activity_code') = 'string'
                AND (arguments - 'activity_code') = '{}'::jsonb
                AND (arguments ->> 'activity_code') ~ '^[a-z][a-z0-9_.-]{1,63}$')
            OR (action_code = 'character.case.acknowledge' AND actor_code IS NOT NULL
                AND jsonb_typeof(arguments) = 'object' AND arguments ? 'case_code'
                AND jsonb_typeof(arguments -> 'case_code') = 'string'
                AND (arguments - 'case_code') = '{}'::jsonb
                AND (arguments ->> 'case_code') ~ '^law[.][0-9a-f]{16}[.][1-9][0-9]*$')
            OR (action_code = 'character.social.greet' AND actor_code IS NOT NULL
                AND jsonb_typeof(arguments) = 'object' AND arguments ? 'target_actor_code'
                AND jsonb_typeof(arguments -> 'target_actor_code') = 'string'
                AND (arguments - 'target_actor_code') = '{}'::jsonb
                AND (arguments ->> 'target_actor_code') ~ '^[a-z][a-z0-9_.-]{1,95}$')
            OR (action_code = 'character.move' AND actor_code IS NOT NULL
                AND jsonb_typeof(arguments) = 'object' AND arguments ?& ARRAY['x', 'y', 'z']
                AND (arguments - ARRAY['x', 'y', 'z']) = '{}'::jsonb
                AND jsonb_typeof(arguments -> 'x') = 'number' AND jsonb_typeof(arguments -> 'y') = 'number'
                AND jsonb_typeof(arguments -> 'z') = 'number' AND (arguments ->> 'x') ~ '^-?[0-9]+$'
                AND (arguments ->> 'y') ~ '^-?[0-9]+$' AND (arguments ->> 'z') ~ '^-?[0-9]+$')
            OR (action_code = 'character.portal.traverse' AND actor_code IS NOT NULL
                AND jsonb_typeof(arguments) = 'object' AND arguments ? 'portal_code'
                AND jsonb_typeof(arguments -> 'portal_code') = 'string'
                AND (arguments - 'portal_code') = '{}'::jsonb
                AND (arguments ->> 'portal_code') ~ '^[a-z][a-z0-9_.-]{1,127}$')
            OR (action_code = 'character.role.change' AND actor_code IS NOT NULL
                AND jsonb_typeof(arguments) = 'object' AND arguments ? 'role_code'
                AND jsonb_typeof(arguments -> 'role_code') = 'string'
                AND (arguments - 'role_code') = '{}'::jsonb
                AND (arguments ->> 'role_code') ~ '^[a-z][a-z0-9_.-]{1,95}$')
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
                 AND resolved_frame_sequence IS NOT NULL AND resolved_frame_sequence > scheduled_frame_sequence))
    );

COMMENT ON TABLE city_realtime_character_social_world_bindings IS
    'Genesis-pinned bounded social adapter for realtime-v2 policy 1.5.0 worlds.';
COMMENT ON TABLE city_realtime_character_social_heads IS
    'Bounded public relation heads keyed by anonymous actor pair; no private identity or chat content.';
COMMENT ON TABLE city_realtime_character_social_events IS
    'Append-only server-validated greeting facts. They do not change wallets, rewards, law, or provider state.';
