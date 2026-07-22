-- A3.3b / bounded Law-Case review slice. 1.6.0 adds exactly one action:
-- character.case.review.file. The candidate must be a server-derived, own,
-- already-acknowledged Law Case. This adapter can only record a procedural
-- receipt and its deterministic closed_no_change result; it cannot accuse
-- another Actor, modify a ruling, waive a fine, or touch any wallet.

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
          "character.case.review.file",
          "character.move",
          "character.portal.traverse",
          "character.role.change",
          "character.social.greet"
        ],
        "temporal_rule": "decision_then_future_frame",
        "user_control": ["manual", "assisted", "autonomous", "suspended"],
        "personality_revision_schema": "city-realtime-character-personality-v1",
        "action_context_schema": "city-realtime-character-action-context-v4",
        "case_response_schema": "city-realtime-character-case-response-v1",
        "social_response_schema": "city-realtime-character-social-v1",
        "case_review_schema": "city-realtime-character-case-review-v1"
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.6.0', 'published', 1, manifest,
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
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_case_review","realtime_agent_case_review_intents"]'::jsonb
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_case_review' THEN capabilities
    ELSE capabilities ||
        '["realtime_character_case_review","realtime_agent_case_review_intents"]'::jsonb
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
          AND binding.policy_version IN ('1.1.0', '1.2.0', '1.3.0', '1.4.0', '1.5.0', '1.6.0')
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
          AND binding.policy_version IN ('1.2.0', '1.3.0', '1.4.0', '1.5.0', '1.6.0')
          AND bundle.status = 'published'
          AND bundle.policy_hash = binding.policy_hash
    )
$$;

-- The Case and social adapters are retained by 1.6.0, but their older world
-- bindings remain immutable. These successor-compatible gates only admit the
-- new policy during genesis or the next sealed frame of a 1.6 world.
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
             AND agent.policy_version IN ('1.4.0', '1.5.0', '1.6.0')
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
             AND agent.policy_version IN ('1.4.0', '1.5.0', '1.6.0')
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
    FROM city_realtime_agent_world_bindings
    WHERE world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR
       expected_policy_version NOT IN ('1.4.0', '1.5.0', '1.6.0') OR NEW.schema_version <> 1 OR
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
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.5.0', '1.6.0')
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
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.5.0', '1.6.0')
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
    IF expected_policy_version NOT IN ('1.5.0', '1.6.0') OR NEW.schema_version <> 1 OR
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

CREATE TABLE IF NOT EXISTS city_realtime_character_case_review_world_bindings (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_realtime_agent_world_bindings(world_id) ON DELETE RESTRICT,
    schema_version SMALLINT NOT NULL,
    agent_binding_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_character_case_review_binding_check CHECK (
        schema_version = 1
        AND agent_binding_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash = encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-review-binding-v1', agent_binding_hash), 'UTF8')), 'hex')
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_case_review_heads (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    case_code VARCHAR(64) NOT NULL,
    law_event_sequence BIGINT NOT NULL,
    law_event_hash VARCHAR(64) NOT NULL,
    response_event_sequence BIGINT NOT NULL,
    response_event_hash VARCHAR(64) NOT NULL,
    review_revision BIGINT NOT NULL DEFAULT 0,
    review_status VARCHAR(24) NOT NULL,
    source_intent_code VARCHAR(96) NOT NULL DEFAULT '',
    filed_frame_sequence BIGINT NOT NULL DEFAULT 0,
    resolution_due_world_time_us BIGINT NOT NULL DEFAULT 0,
    last_frame_sequence BIGINT NOT NULL,
    event_chain_hash VARCHAR(64) NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, case_code),
    CONSTRAINT city_realtime_character_case_review_head_binding_fk
        FOREIGN KEY (world_id)
        REFERENCES city_realtime_character_case_review_world_bindings(world_id) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_review_head_law_fk
        FOREIGN KEY (world_id, actor_code, law_event_sequence)
        REFERENCES city_realtime_character_law_events(world_id, actor_code, event_sequence) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_review_head_response_fk
        FOREIGN KEY (world_id, actor_code, response_event_sequence)
        REFERENCES city_realtime_character_case_response_events(world_id, actor_code, event_sequence) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_review_head_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_case_review_head_case_unique UNIQUE (world_id, case_code),
    CONSTRAINT city_realtime_character_case_review_head_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND case_code ~ '^law[.][0-9a-f]{16}[.][1-9][0-9]*$'
        AND law_event_sequence > 0
        AND law_event_hash ~ '^[0-9a-f]{64}$'
        AND response_event_sequence > 0
        AND response_event_hash ~ '^[0-9a-f]{64}$'
        AND last_frame_sequence > 0
        AND event_chain_hash ~ '^[0-9a-f]{64}$'
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND (
            (review_revision = 0 AND review_status = 'none' AND source_intent_code = ''
             AND filed_frame_sequence = 0 AND resolution_due_world_time_us = 0)
            OR
            (review_revision = 1 AND review_status = 'filed'
             AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
             AND filed_frame_sequence > 0 AND last_frame_sequence = filed_frame_sequence
             AND resolution_due_world_time_us > 0)
            OR
            (review_revision = 2 AND review_status = 'closed_no_change'
             AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
             AND filed_frame_sequence > 0 AND last_frame_sequence > filed_frame_sequence
             AND resolution_due_world_time_us > 0)
        )
        AND resolution_due_world_time_us % 1000000 = 0
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_case_review_events (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    case_code VARCHAR(64) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    event_type VARCHAR(24) NOT NULL,
    source_intent_code VARCHAR(96) NOT NULL,
    resolution_due_world_time_us BIGINT NOT NULL,
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, case_code, event_sequence),
    CONSTRAINT city_realtime_character_case_review_event_head_fk
        FOREIGN KEY (world_id, actor_code, case_code)
        REFERENCES city_realtime_character_case_review_heads(world_id, actor_code, case_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_review_event_intent_fk
        FOREIGN KEY (world_id, source_intent_code)
        REFERENCES city_realtime_agent_intents(world_id, intent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_review_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_case_review_event_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND case_code ~ '^law[.][0-9a-f]{16}[.][1-9][0-9]*$'
        AND frame_sequence > 0
        AND ((event_sequence = 1 AND event_type = 'filed')
             OR (event_sequence = 2 AND event_type = 'closed_no_change'))
        AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND resolution_due_world_time_us > 0
        AND resolution_due_world_time_us % 1000000 = 0
        AND previous_event_hash ~ '^[0-9a-f]{64}$'
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_case_review_heads_actor
    ON city_realtime_character_case_review_heads (world_id, actor_code, last_frame_sequence DESC, case_code ASC);
CREATE INDEX IF NOT EXISTS idx_city_realtime_character_case_review_events_frame
    ON city_realtime_character_case_review_events (world_id, frame_sequence, actor_code, case_code);

CREATE OR REPLACE FUNCTION city_realtime_character_case_review_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_review_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_response_world_bindings response
             ON response.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0
             AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.6.0'
             AND response.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_review_mutation_enabled(
    target_world_id BIGINT,
    target_frame_sequence BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_review_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_case_review_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_response_world_bindings response
             ON response.world_id = world.id
           JOIN city_realtime_character_case_review_world_bindings review
             ON review.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.6.0'
             AND response.agent_binding_hash = agent.binding_hash
             AND review.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_review_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_agent_binding_hash VARCHAR(64);
    expected_policy_id VARCHAR(96);
    expected_policy_version VARCHAR(24);
    expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_review_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-review binding is immutable outside genesis initialization'
            USING ERRCODE = '55000';
    END IF;
    SELECT agent.policy_id, agent.policy_version, agent.binding_hash
    INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings agent
    JOIN city_realtime_character_case_response_world_bindings response
      ON response.world_id = agent.world_id
     AND response.agent_binding_hash = agent.binding_hash
    WHERE agent.world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR expected_policy_version <> '1.6.0'
       OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-review binding references an invalid agent policy'
            USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-review-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-review binding hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_review_head()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    response_event_sequence BIGINT;
    response_frame_sequence BIGINT;
    response_law_sequence BIGINT;
    response_law_hash VARCHAR(64);
    response_event_hash VARCHAR(64);
    response_code_value VARCHAR(24);
    law_event_hash_value VARCHAR(64);
    actor_kind_value VARCHAR(16);
    active_agent_count BIGINT;
    expected_chain_hash VARCHAR(64);
    expected_state_hash VARCHAR(64);
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NOT COALESCE(city_realtime_character_case_review_mutation_enabled(
            NEW.world_id,
            NULLIF(current_setting('sub2api.city_realtime_character_case_review_frame_sequence', TRUE), '')::BIGINT
        ), FALSE) THEN
            RAISE EXCEPTION 'city realtime character case-review head may be created only through a sealed reducer'
                USING ERRCODE = '55000';
        END IF;
        SELECT response.event_sequence, response.frame_sequence, response.law_event_sequence,
               response.law_event_hash, response.event_hash, response.response_code, law.event_hash,
               identity.actor_kind
        INTO response_event_sequence, response_frame_sequence, response_law_sequence,
             response_law_hash, response_event_hash, response_code_value, law_event_hash_value,
             actor_kind_value
        FROM city_realtime_character_case_response_events response
        JOIN city_realtime_character_law_events law
          ON law.world_id = response.world_id
         AND law.actor_code = response.actor_code
         AND law.event_sequence = response.law_event_sequence
         AND law.case_code = response.case_code
        JOIN city_realtime_actor_identities identity
          ON identity.world_id = response.world_id
         AND identity.actor_code = response.actor_code
        WHERE response.world_id = NEW.world_id
          AND response.actor_code = NEW.actor_code
          AND response.case_code = NEW.case_code;
        SELECT COUNT(*) INTO active_agent_count
        FROM city_realtime_agent_instances
        WHERE world_id = NEW.world_id
          AND actor_code = NEW.actor_code
          AND agent_subtype = 'character.user'
          AND lifecycle_status = 'active';
        expected_chain_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-review-chain-v1', NEW.actor_code, NEW.case_code,
            NEW.law_event_hash, NEW.response_event_hash), 'UTF8')), 'hex');
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-review-state-v1', NEW.actor_code, NEW.case_code,
            NEW.law_event_sequence::text, NEW.law_event_hash, NEW.response_event_sequence::text,
            NEW.response_event_hash, NEW.review_revision::text, NEW.review_status,
            NEW.source_intent_code, NEW.filed_frame_sequence::text,
            NEW.resolution_due_world_time_us::text, NEW.last_frame_sequence::text,
            NEW.event_chain_hash), 'UTF8')), 'hex');
        IF response_event_sequence IS NULL OR actor_kind_value <> 'character' OR active_agent_count <> 1
           OR response_code_value <> 'acknowledged'
           OR NEW.law_event_sequence <> response_law_sequence OR NEW.law_event_hash <> response_law_hash
           OR NEW.law_event_hash <> law_event_hash_value
           OR NEW.response_event_sequence <> response_event_sequence
           OR NEW.response_event_hash <> response_event_hash
           OR NEW.review_revision <> 0 OR NEW.review_status <> 'none' OR NEW.source_intent_code <> ''
           OR NEW.filed_frame_sequence <> 0 OR NEW.resolution_due_world_time_us <> 0
           OR NEW.last_frame_sequence <> response_frame_sequence
           OR NEW.event_chain_hash <> expected_chain_hash OR NEW.state_hash <> expected_state_hash THEN
            RAISE EXCEPTION 'city realtime character case-review genesis head mismatch' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'UPDATE' THEN
        IF NOT city_realtime_character_case_review_mutation_enabled(NEW.world_id, NEW.last_frame_sequence)
           OR NEW.actor_code <> OLD.actor_code OR NEW.case_code <> OLD.case_code
           OR NEW.law_event_sequence <> OLD.law_event_sequence OR NEW.law_event_hash <> OLD.law_event_hash
           OR NEW.response_event_sequence <> OLD.response_event_sequence OR NEW.response_event_hash <> OLD.response_event_hash
           OR NEW.review_revision <> OLD.review_revision + 1
           OR NEW.last_frame_sequence <= OLD.last_frame_sequence
           OR NEW.event_chain_hash = OLD.event_chain_hash OR NEW.metadata <> OLD.metadata THEN
            RAISE EXCEPTION 'city realtime character case-review heads may change only through a sealed reducer'
                USING ERRCODE = '55000';
        END IF;
        IF (NEW.review_revision = 1 AND (NEW.review_status <> 'filed'
              OR NEW.source_intent_code = '' OR NEW.filed_frame_sequence <> NEW.last_frame_sequence
              OR NEW.resolution_due_world_time_us <= 0))
           OR (NEW.review_revision = 2 AND (NEW.review_status <> 'closed_no_change'
              OR NEW.source_intent_code <> OLD.source_intent_code
              OR NEW.filed_frame_sequence <> OLD.filed_frame_sequence
              OR NEW.resolution_due_world_time_us <> OLD.resolution_due_world_time_us)) THEN
            RAISE EXCEPTION 'city realtime character case-review transition is invalid' USING ERRCODE = '23514';
        END IF;
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-review-state-v1', NEW.actor_code, NEW.case_code,
            NEW.law_event_sequence::text, NEW.law_event_hash, NEW.response_event_sequence::text,
            NEW.response_event_hash, NEW.review_revision::text, NEW.review_status,
            NEW.source_intent_code, NEW.filed_frame_sequence::text,
            NEW.resolution_due_world_time_us::text, NEW.last_frame_sequence::text,
            NEW.event_chain_hash), 'UTF8')), 'hex');
        IF NEW.state_hash <> expected_state_hash OR NOT EXISTS (
            SELECT 1
            FROM city_realtime_character_case_review_events event
            WHERE event.world_id = NEW.world_id
              AND event.actor_code = NEW.actor_code
              AND event.case_code = NEW.case_code
              AND event.event_sequence = NEW.review_revision
              AND event.frame_sequence = NEW.last_frame_sequence
              AND event.event_hash = NEW.event_chain_hash
              AND event.source_intent_code = NEW.source_intent_code
              AND event.resolution_due_world_time_us = NEW.resolution_due_world_time_us
        ) THEN
            RAISE EXCEPTION 'city realtime character case-review head event mismatch' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'city realtime character case-review heads are immutable outside sealed reducers'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_review_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    head_revision BIGINT;
    head_status VARCHAR(24);
    head_last_frame BIGINT;
    head_filed_frame BIGINT;
    head_due_world_time BIGINT;
    head_chain_hash VARCHAR(64);
    head_source_intent_code VARCHAR(96);
    head_law_sequence BIGINT;
    head_law_hash VARCHAR(64);
    head_response_sequence BIGINT;
    head_response_hash VARCHAR(64);
    intent_actor_code VARCHAR(96);
    intent_action_code VARCHAR(64);
    intent_status VARCHAR(16);
    intent_arguments JSONB;
    expected_event_hash VARCHAR(64);
    expected_aggregate_key VARCHAR(160);
    expected_dedup_key VARCHAR(160);
    expected_payload JSONB;
    expected_payload_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_review_mutation_enabled(NEW.world_id, NEW.frame_sequence) THEN
        RAISE EXCEPTION 'city realtime character case-review events are append-only sealed facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT review_revision, review_status, last_frame_sequence, filed_frame_sequence,
           resolution_due_world_time_us, event_chain_hash, source_intent_code,
           law_event_sequence, law_event_hash, response_event_sequence, response_event_hash
    INTO head_revision, head_status, head_last_frame, head_filed_frame,
         head_due_world_time, head_chain_hash, head_source_intent_code,
         head_law_sequence, head_law_hash, head_response_sequence, head_response_hash
    FROM city_realtime_character_case_review_heads
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code AND case_code = NEW.case_code;
    IF NOT FOUND OR NEW.event_sequence <> head_revision + 1 OR NEW.frame_sequence <= head_last_frame
       OR NEW.previous_event_hash <> head_chain_hash THEN
        RAISE EXCEPTION 'city realtime character case-review event chain mismatch' USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM city_realtime_character_case_response_events response
        JOIN city_realtime_character_law_events law
          ON law.world_id = response.world_id
         AND law.actor_code = response.actor_code
         AND law.event_sequence = response.law_event_sequence
         AND law.case_code = response.case_code
        WHERE response.world_id = NEW.world_id
          AND response.actor_code = NEW.actor_code
          AND response.case_code = NEW.case_code
          AND response.event_sequence = head_response_sequence
          AND response.event_hash = head_response_hash
          AND response.law_event_sequence = head_law_sequence
          AND response.law_event_hash = head_law_hash
          AND law.event_hash = head_law_hash
          AND response.response_code = 'acknowledged'
    ) THEN
        RAISE EXCEPTION 'city realtime character case-review event must reference its acknowledged own-law fact'
            USING ERRCODE = '23514';
    END IF;
    SELECT actor_code, action_code, status, arguments
    INTO intent_actor_code, intent_action_code, intent_status, intent_arguments
    FROM city_realtime_agent_intents
    WHERE world_id = NEW.world_id AND intent_code = NEW.source_intent_code;
    IF NOT FOUND OR intent_actor_code <> NEW.actor_code OR intent_action_code <> 'character.case.review.file'
       OR intent_arguments <> jsonb_build_object('case_code', NEW.case_code) THEN
        RAISE EXCEPTION 'city realtime character case-review event must have an exact matching intent'
            USING ERRCODE = '23514';
    END IF;
    expected_payload := jsonb_build_object(
        'actor_code', NEW.actor_code,
        'case_code', NEW.case_code,
        'schema_version', 1,
        'source_intent_code', NEW.source_intent_code
    );
    expected_payload_hash := encode(sha256(convert_to(format(
        '{"actor_code":"%s","case_code":"%s","schema_version":1,"source_intent_code":"%s"}',
        NEW.actor_code, NEW.case_code, NEW.source_intent_code
    ), 'UTF8')), 'hex');
    expected_aggregate_key := 'case-review:' || encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-review-close-v1', NEW.actor_code, NEW.case_code
    ), 'UTF8')), 'hex');
    expected_dedup_key := 'case-review-close.' || encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-review-close-v1', NEW.actor_code, NEW.case_code,
        NEW.source_intent_code
    ), 'UTF8')), 'hex');

    IF NEW.event_sequence = 1 THEN
        IF NEW.event_type <> 'filed' OR head_revision <> 0 OR head_status <> 'none'
           OR head_source_intent_code <> '' OR intent_status <> 'pending' THEN
            RAISE EXCEPTION 'city realtime character case-review filing transition is invalid' USING ERRCODE = '23514';
        END IF;
        IF NOT EXISTS (
            SELECT 1
            FROM city_due_events due
            WHERE due.world_id = NEW.world_id
              AND due.event_type = 'system.realtime.character_case_review_close'
              AND due.schema_version = 1
              AND due.due_world_time_us = NEW.resolution_due_world_time_us
              AND due.temporal_phase = 'rule_effect'
              AND due.priority = 100
              AND due.aggregate_type = 'realtime_case_review'
              AND due.aggregate_key = expected_aggregate_key
              AND due.dedup_key = expected_dedup_key
              AND due.source_kind = 'system'
              AND due.source_reference = 'realtime_character_case_review'
              AND due.payload = expected_payload
              AND due.payload_hash = expected_payload_hash
              AND due.expected_version = 1
              AND due.status = 'pending'
              AND due.created_frame_sequence = NEW.frame_sequence
        ) THEN
            RAISE EXCEPTION 'city realtime character case-review filing requires one sealed closure due event'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.event_sequence = 2 THEN
        IF NEW.event_type <> 'closed_no_change' OR head_revision <> 1 OR head_status <> 'filed'
           OR NEW.source_intent_code <> head_source_intent_code
           OR NEW.resolution_due_world_time_us <> head_due_world_time
           OR intent_status <> 'applied' THEN
            RAISE EXCEPTION 'city realtime character case-review closure transition is invalid' USING ERRCODE = '23514';
        END IF;
        IF NOT EXISTS (
            SELECT 1
            FROM city_due_events due
            WHERE due.world_id = NEW.world_id
              AND due.event_type = 'system.realtime.character_case_review_close'
              AND due.schema_version = 1
              AND due.due_world_time_us = NEW.resolution_due_world_time_us
              AND due.temporal_phase = 'rule_effect'
              AND due.priority = 100
              AND due.aggregate_type = 'realtime_case_review'
              AND due.aggregate_key = expected_aggregate_key
              AND due.dedup_key = expected_dedup_key
              AND due.source_kind = 'system'
              AND due.source_reference = 'realtime_character_case_review'
              AND due.payload = expected_payload
              AND due.payload_hash = expected_payload_hash
              AND due.expected_version = 1
              AND due.status = 'pending'
              AND due.created_frame_sequence = head_filed_frame
        ) THEN
            RAISE EXCEPTION 'city realtime character case-review closure requires its pending sealed due event'
                USING ERRCODE = '23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'city realtime character case-review event sequence is invalid' USING ERRCODE = '23514';
    END IF;

    expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-review-event-v1', NEW.actor_code, NEW.case_code,
        NEW.event_sequence::text, NEW.frame_sequence::text, NEW.event_type,
        NEW.source_intent_code, NEW.resolution_due_world_time_us::text,
        NEW.previous_event_hash
    ), 'UTF8')), 'hex');
    IF NEW.event_hash <> expected_event_hash THEN
        RAISE EXCEPTION 'city realtime character case-review event hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_case_review_binding_guard
    ON city_realtime_character_case_review_world_bindings;
CREATE TRIGGER city_realtime_character_case_review_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_review_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_review_binding();

DROP TRIGGER IF EXISTS city_realtime_character_case_review_head_guard
    ON city_realtime_character_case_review_heads;
CREATE TRIGGER city_realtime_character_case_review_head_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_review_heads
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_review_head();

DROP TRIGGER IF EXISTS city_realtime_character_case_review_event_guard
    ON city_realtime_character_case_review_events;
CREATE TRIGGER city_realtime_character_case_review_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_review_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_review_event();

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
        OR (action_code = 'character.case.review.file' AND jsonb_typeof(arguments) = 'object'
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
            OR (action_code = 'character.case.review.file' AND actor_code IS NOT NULL
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
                 AND resolved_frame_sequence IS NOT NULL
                 AND resolved_frame_sequence > scheduled_frame_sequence))
    );

COMMENT ON TABLE city_realtime_character_case_review_world_bindings IS
    'Genesis-pinned procedural Law-Case review adapter for realtime-v2 policy 1.6.0 worlds.';
COMMENT ON TABLE city_realtime_character_case_review_heads IS
    'Bounded owner-scoped Case-review receipt heads. They preserve the originating Law fact and may only close without legal or economic change.';
COMMENT ON TABLE city_realtime_character_case_review_events IS
    'Append-only procedural review facts. They cannot accuse another actor, change a ruling or penalty, touch city credit, inventory, wallet, provider data, or raw model output.';
