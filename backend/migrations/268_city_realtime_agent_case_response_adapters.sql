-- A3.3 / Rule-Case slice: publish a separate 1.4.0 policy instead of
-- widening an existing world. A Character Agent may only acknowledge one of
-- its own already-applied law facts. It cannot decide guilt, alter a ruling,
-- waive a penalty, create a Case, or touch any platform wallet.

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
          "character.role.change"
        ],
        "temporal_rule": "decision_then_future_frame",
        "user_control": ["manual", "assisted", "autonomous", "suspended"],
        "personality_revision_schema": "city-realtime-character-personality-v1",
        "action_context_schema": "city-realtime-character-action-context-v2",
        "case_response_schema": "city-realtime-character-case-response-v1"
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.4.0', 'published', 1, manifest,
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
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_case_response' THEN capabilities
    ELSE capabilities ||
        '["realtime_character_case_response","realtime_agent_case_response_intents"]'::jsonb
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
          AND binding.policy_version IN ('1.1.0', '1.2.0', '1.3.0', '1.4.0')
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
          AND binding.policy_version IN ('1.2.0', '1.3.0', '1.4.0')
          AND bundle.status = 'published'
          AND bundle.policy_hash = binding.policy_hash
    )
$$;

CREATE TABLE IF NOT EXISTS city_realtime_character_case_response_world_bindings (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_realtime_agent_world_bindings(world_id) ON DELETE RESTRICT,
    schema_version SMALLINT NOT NULL,
    agent_binding_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_character_case_response_binding_check CHECK (
        schema_version = 1
        AND agent_binding_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_case_response_heads (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    response_revision BIGINT NOT NULL DEFAULT 0,
    last_frame_sequence BIGINT NOT NULL,
    event_chain_hash VARCHAR(64) NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code),
    CONSTRAINT city_realtime_character_case_response_head_binding_fk
        FOREIGN KEY (world_id)
        REFERENCES city_realtime_character_case_response_world_bindings(world_id) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_response_head_profile_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_character_profiles(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_response_head_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_case_response_head_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND response_revision >= 0
        AND last_frame_sequence > 0
        AND event_chain_hash ~ '^[0-9a-f]{64}$'
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_case_response_events (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    case_code VARCHAR(64) NOT NULL,
    law_event_sequence BIGINT NOT NULL,
    law_event_hash VARCHAR(64) NOT NULL,
    response_code VARCHAR(24) NOT NULL,
    source_intent_code VARCHAR(96) NOT NULL,
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, event_sequence),
    CONSTRAINT city_realtime_character_case_response_event_head_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_character_case_response_heads(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_response_event_law_fk
        FOREIGN KEY (world_id, actor_code, law_event_sequence)
        REFERENCES city_realtime_character_law_events(world_id, actor_code, event_sequence) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_response_event_intent_fk
        FOREIGN KEY (world_id, source_intent_code)
        REFERENCES city_realtime_agent_intents(world_id, intent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_response_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_case_response_event_case_unique UNIQUE (world_id, case_code),
    CONSTRAINT city_realtime_character_case_response_event_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND event_sequence > 0
        AND frame_sequence > 0
        AND case_code ~ '^law[.][0-9a-f]{16}[.][1-9][0-9]*$'
        AND law_event_sequence > 0
        AND law_event_hash ~ '^[0-9a-f]{64}$'
        AND response_code = 'acknowledged'
        AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND previous_event_hash ~ '^[0-9a-f]{64}$'
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_case_response_events_actor_case
    ON city_realtime_character_case_response_events (world_id, actor_code, case_code);

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
             AND agent.policy_version = '1.4.0'
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
           JOIN city_realtime_character_case_response_world_bindings response
             ON response.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.4.0'
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
        RAISE EXCEPTION 'city realtime character case-response binding is immutable outside genesis initialization'
            USING ERRCODE = '55000';
    END IF;
    SELECT policy_id, policy_version, binding_hash
    INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings
    WHERE world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR expected_policy_version <> '1.4.0'
       OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-response binding references an invalid agent policy'
            USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-response-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-response binding hash mismatch'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_response_head()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    profile_spawned_frame BIGINT;
    expected_genesis_hash VARCHAR(64);
    expected_state_hash VARCHAR(64);
    actor_kind_value VARCHAR(16);
    active_agent_count BIGINT;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NOT COALESCE(city_realtime_character_case_response_mutation_enabled(
            NEW.world_id,
            NULLIF(current_setting('sub2api.city_realtime_character_case_response_frame_sequence', TRUE), '')::BIGINT
        ), FALSE) THEN
            RAISE EXCEPTION 'city realtime character case-response head may be created only through a sealed reducer'
                USING ERRCODE = '55000';
        END IF;
        SELECT profile.spawned_frame_sequence, identity.actor_kind
        INTO profile_spawned_frame, actor_kind_value
        FROM city_realtime_character_profiles profile
        JOIN city_realtime_actor_identities identity
          ON identity.world_id = profile.world_id AND identity.actor_code = profile.actor_code
        WHERE profile.world_id = NEW.world_id AND profile.actor_code = NEW.actor_code;
        SELECT COUNT(*) INTO active_agent_count
        FROM city_realtime_agent_instances
        WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
          AND agent_subtype = 'character.user' AND lifecycle_status = 'active';
        expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-response-chain-v1', NEW.actor_code, profile_spawned_frame::text), 'UTF8')), 'hex');
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-response-state-v1', NEW.actor_code,
            NEW.response_revision::text, NEW.last_frame_sequence::text, NEW.event_chain_hash), 'UTF8')), 'hex');
        IF profile_spawned_frame IS NULL OR actor_kind_value <> 'character' OR active_agent_count <> 1
           OR NEW.response_revision <> 0 OR NEW.last_frame_sequence <> profile_spawned_frame
           OR NEW.event_chain_hash <> expected_genesis_hash OR NEW.state_hash <> expected_state_hash THEN
            RAISE EXCEPTION 'city realtime character case-response genesis head mismatch'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NOT city_realtime_character_case_response_mutation_enabled(NEW.world_id, NEW.last_frame_sequence)
           OR NEW.actor_code <> OLD.actor_code OR NEW.response_revision <> OLD.response_revision + 1
           OR NEW.last_frame_sequence <= OLD.last_frame_sequence OR NEW.event_chain_hash = OLD.event_chain_hash
           OR NEW.metadata <> OLD.metadata THEN
            RAISE EXCEPTION 'city realtime character case-response heads may change only through a sealed reducer'
                USING ERRCODE = '55000';
        END IF;
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-response-state-v1', NEW.actor_code,
            NEW.response_revision::text, NEW.last_frame_sequence::text, NEW.event_chain_hash), 'UTF8')), 'hex');
        IF NEW.state_hash <> expected_state_hash OR NOT EXISTS (
            SELECT 1
            FROM city_realtime_character_case_response_events event
            WHERE event.world_id = NEW.world_id AND event.actor_code = NEW.actor_code
              AND event.event_sequence = NEW.response_revision
              AND event.frame_sequence = NEW.last_frame_sequence
              AND event.event_hash = NEW.event_chain_hash
        ) THEN
            RAISE EXCEPTION 'city realtime character case-response head event mismatch'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character case-response heads are immutable outside sealed reducers'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_response_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    head_revision BIGINT;
    head_last_frame BIGINT;
    head_chain_hash VARCHAR(64);
    expected_law_sequence BIGINT;
    expected_law_hash VARCHAR(64);
    expected_event_hash VARCHAR(64);
    intent_actor_code VARCHAR(96);
    intent_action_code VARCHAR(64);
    intent_status VARCHAR(16);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_response_mutation_enabled(NEW.world_id, NEW.frame_sequence) THEN
        RAISE EXCEPTION 'city realtime character case-response events are append-only sealed facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT response_revision, last_frame_sequence, event_chain_hash
    INTO head_revision, head_last_frame, head_chain_hash
    FROM city_realtime_character_case_response_heads
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code;
    IF NOT FOUND OR NEW.event_sequence <> head_revision + 1
       OR NEW.frame_sequence <= head_last_frame OR NEW.previous_event_hash <> head_chain_hash THEN
        RAISE EXCEPTION 'city realtime character case-response event chain mismatch'
            USING ERRCODE = '23514';
    END IF;
    SELECT event_sequence, event_hash INTO expected_law_sequence, expected_law_hash
    FROM city_realtime_character_law_events
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code AND case_code = NEW.case_code;
    IF NOT FOUND OR NEW.law_event_sequence <> expected_law_sequence OR NEW.law_event_hash <> expected_law_hash THEN
        RAISE EXCEPTION 'city realtime character case-response event must reference its exact law fact'
            USING ERRCODE = '23514';
    END IF;
    SELECT actor_code, action_code, status
    INTO intent_actor_code, intent_action_code, intent_status
    FROM city_realtime_agent_intents
    WHERE world_id = NEW.world_id AND intent_code = NEW.source_intent_code;
    IF NOT FOUND OR intent_actor_code <> NEW.actor_code
       OR intent_action_code <> 'character.case.acknowledge' OR intent_status <> 'pending' THEN
        RAISE EXCEPTION 'city realtime character case-response event must have a pending matching intent'
            USING ERRCODE = '23514';
    END IF;
    expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-response-event-v1', NEW.actor_code,
        NEW.event_sequence::text, NEW.frame_sequence::text, NEW.case_code,
        NEW.law_event_sequence::text, NEW.law_event_hash, NEW.response_code,
        NEW.source_intent_code, NEW.previous_event_hash), 'UTF8')), 'hex');
    IF NEW.event_hash <> expected_event_hash THEN
        RAISE EXCEPTION 'city realtime character case-response event hash mismatch'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_case_response_binding_guard
    ON city_realtime_character_case_response_world_bindings;
CREATE TRIGGER city_realtime_character_case_response_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_response_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_response_binding();

DROP TRIGGER IF EXISTS city_realtime_character_case_response_head_guard
    ON city_realtime_character_case_response_heads;
CREATE TRIGGER city_realtime_character_case_response_head_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_response_heads
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_response_head();

DROP TRIGGER IF EXISTS city_realtime_character_case_response_event_guard
    ON city_realtime_character_case_response_events;
CREATE TRIGGER city_realtime_character_case_response_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_response_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_response_event();

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
        OR
        (action_code = 'character.case.acknowledge'
         AND jsonb_typeof(arguments) = 'object'
         AND arguments ? 'case_code'
         AND jsonb_typeof(arguments -> 'case_code') = 'string'
         AND (arguments - 'case_code') = '{}'::jsonb
         AND (arguments ->> 'case_code') ~ '^law[.][0-9a-f]{16}[.][1-9][0-9]*$')
        OR
        (action_code = 'character.move'
         AND jsonb_typeof(arguments) = 'object'
         AND arguments ?& ARRAY['x', 'y', 'z']
         AND (arguments - ARRAY['x', 'y', 'z']) = '{}'::jsonb
         AND jsonb_typeof(arguments -> 'x') = 'number'
         AND jsonb_typeof(arguments -> 'y') = 'number'
         AND jsonb_typeof(arguments -> 'z') = 'number'
         AND (arguments ->> 'x') ~ '^-?[0-9]+$'
         AND (arguments ->> 'y') ~ '^-?[0-9]+$'
         AND (arguments ->> 'z') ~ '^-?[0-9]+$')
        OR
        (action_code = 'character.portal.traverse'
         AND jsonb_typeof(arguments) = 'object'
         AND arguments ? 'portal_code'
         AND jsonb_typeof(arguments -> 'portal_code') = 'string'
         AND (arguments - 'portal_code') = '{}'::jsonb
         AND (arguments ->> 'portal_code') ~ '^[a-z][a-z0-9_.-]{1,127}$')
        OR
        (action_code = 'character.role.change'
         AND jsonb_typeof(arguments) = 'object'
         AND arguments ? 'role_code'
         AND jsonb_typeof(arguments -> 'role_code') = 'string'
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
            OR
            (action_code = 'character.activity.perform'
             AND actor_code IS NOT NULL
             AND jsonb_typeof(arguments) = 'object'
             AND arguments ? 'activity_code'
             AND jsonb_typeof(arguments -> 'activity_code') = 'string'
             AND (arguments - 'activity_code') = '{}'::jsonb
             AND (arguments ->> 'activity_code') ~ '^[a-z][a-z0-9_.-]{1,63}$')
            OR
            (action_code = 'character.case.acknowledge'
             AND actor_code IS NOT NULL
             AND jsonb_typeof(arguments) = 'object'
             AND arguments ? 'case_code'
             AND jsonb_typeof(arguments -> 'case_code') = 'string'
             AND (arguments - 'case_code') = '{}'::jsonb
             AND (arguments ->> 'case_code') ~ '^law[.][0-9a-f]{16}[.][1-9][0-9]*$')
            OR
            (action_code = 'character.move'
             AND actor_code IS NOT NULL
             AND jsonb_typeof(arguments) = 'object'
             AND arguments ?& ARRAY['x', 'y', 'z']
             AND (arguments - ARRAY['x', 'y', 'z']) = '{}'::jsonb
             AND jsonb_typeof(arguments -> 'x') = 'number'
             AND jsonb_typeof(arguments -> 'y') = 'number'
             AND jsonb_typeof(arguments -> 'z') = 'number'
             AND (arguments ->> 'x') ~ '^-?[0-9]+$'
             AND (arguments ->> 'y') ~ '^-?[0-9]+$'
             AND (arguments ->> 'z') ~ '^-?[0-9]+$')
            OR
            (action_code = 'character.portal.traverse'
             AND actor_code IS NOT NULL
             AND jsonb_typeof(arguments) = 'object'
             AND arguments ? 'portal_code'
             AND jsonb_typeof(arguments -> 'portal_code') = 'string'
             AND (arguments - 'portal_code') = '{}'::jsonb
             AND (arguments ->> 'portal_code') ~ '^[a-z][a-z0-9_.-]{1,127}$')
            OR
            (action_code = 'character.role.change'
             AND actor_code IS NOT NULL
             AND jsonb_typeof(arguments) = 'object'
             AND arguments ? 'role_code'
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
                 AND resolved_frame_sequence IS NOT NULL
                 AND resolved_frame_sequence > scheduled_frame_sequence))
    );

COMMENT ON TABLE city_realtime_character_case_response_world_bindings IS
    'Genesis-pinned Rule/Case acknowledgement adapter for realtime-v2 policy 1.4.0 worlds.';
COMMENT ON TABLE city_realtime_character_case_response_heads IS
    'Bounded owner-scoped acknowledgement-chain heads; legal rulings remain in immutable law events.';
COMMENT ON TABLE city_realtime_character_case_response_events IS
    'Append-only acknowledgements of already-applied own-law facts. They cannot change Rule, Case, penalty, city ledger, or platform wallet.';
