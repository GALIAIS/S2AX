-- A3.3b / bounded Case-intake slice. 1.7.0 adds exactly one action:
-- character.case.report.file. It can file one immutable, unverified receipt
-- about an adjacent public Actor from the sealed social target catalogue. The
-- receipt is not a Law Case, assertion, evidence finding, ruling, penalty,
-- reward, wallet movement, or provider/model-output channel.

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
          "character.case.report.file",
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
        "case_review_schema": "city-realtime-character-case-review-v1",
        "case_report_schema": "city-realtime-character-case-report-v1"
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.7.0', 'published', 1, manifest,
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
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_case_report","realtime_agent_case_report_intents"]'::jsonb
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_case_report' THEN capabilities
    ELSE capabilities ||
        '["realtime_character_case_report","realtime_agent_case_report_intents"]'::jsonb
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
          AND binding.policy_version IN ('1.1.0', '1.2.0', '1.3.0', '1.4.0', '1.5.0', '1.6.0', '1.7.0')
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
          AND binding.policy_version IN ('1.2.0', '1.3.0', '1.4.0', '1.5.0', '1.6.0', '1.7.0')
          AND bundle.status = 'published'
          AND bundle.policy_hash = binding.policy_hash
    )
$$;

-- Retain the older adapters in a 1.7 world without mutating their historical
-- policy bindings or canonical state shapes.
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
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.4.0', '1.5.0', '1.6.0', '1.7.0')
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
             AND agent.policy_version IN ('1.4.0', '1.5.0', '1.6.0', '1.7.0')
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
       expected_policy_version NOT IN ('1.4.0', '1.5.0', '1.6.0', '1.7.0') OR NEW.schema_version <> 1 OR
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
             AND agent.policy_version IN ('1.5.0', '1.6.0', '1.7.0')
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
             AND agent.policy_version IN ('1.5.0', '1.6.0', '1.7.0')
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
    IF expected_policy_version NOT IN ('1.5.0', '1.6.0', '1.7.0') OR NEW.schema_version <> 1 OR
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
             AND agent.policy_version IN ('1.6.0', '1.7.0')
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
             AND agent.policy_version IN ('1.6.0', '1.7.0')
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
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR
       expected_policy_version NOT IN ('1.6.0', '1.7.0') OR NEW.schema_version <> 1 OR
       NEW.agent_binding_hash <> expected_agent_binding_hash THEN
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

CREATE TABLE IF NOT EXISTS city_realtime_character_case_report_world_bindings (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_realtime_agent_world_bindings(world_id) ON DELETE RESTRICT,
    schema_version SMALLINT NOT NULL,
    agent_binding_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_character_case_report_binding_check CHECK (
        schema_version = 1
        AND agent_binding_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash = encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-report-binding-v1', agent_binding_hash), 'UTF8')), 'hex')
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_case_report_heads (
    world_id BIGINT NOT NULL
        REFERENCES city_realtime_character_case_report_world_bindings(world_id) ON DELETE RESTRICT,
    reporter_actor_code VARCHAR(96) NOT NULL,
    subject_actor_code VARCHAR(96) NOT NULL,
    report_revision BIGINT NOT NULL,
    report_status VARCHAR(24) NOT NULL,
    source_intent_code VARCHAR(96) NOT NULL,
    filed_frame_sequence BIGINT NOT NULL,
    last_frame_sequence BIGINT NOT NULL,
    event_chain_hash VARCHAR(64) NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, reporter_actor_code, subject_actor_code),
    CONSTRAINT city_realtime_character_case_report_head_reporter_fk
        FOREIGN KEY (world_id, reporter_actor_code)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_report_head_subject_fk
        FOREIGN KEY (world_id, subject_actor_code)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_report_head_intent_fk
        FOREIGN KEY (world_id, source_intent_code)
        REFERENCES city_realtime_agent_intents(world_id, intent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_report_head_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_case_report_head_check CHECK (
        reporter_actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND subject_actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND reporter_actor_code <> subject_actor_code
        AND report_revision = 1
        AND report_status = 'filed_unverified'
        AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND filed_frame_sequence > 0
        AND last_frame_sequence = filed_frame_sequence
        AND event_chain_hash ~ '^[0-9a-f]{64}$'
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND state_hash = encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-report-state-v1',
            reporter_actor_code, subject_actor_code, report_revision::text,
            report_status, source_intent_code, filed_frame_sequence::text,
            last_frame_sequence::text, event_chain_hash), 'UTF8')), 'hex')
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_case_report_events (
    world_id BIGINT NOT NULL,
    reporter_actor_code VARCHAR(96) NOT NULL,
    subject_actor_code VARCHAR(96) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    event_type VARCHAR(24) NOT NULL,
    source_intent_code VARCHAR(96) NOT NULL,
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, reporter_actor_code, subject_actor_code, event_sequence),
    CONSTRAINT city_realtime_character_case_report_event_head_fk
        FOREIGN KEY (world_id, reporter_actor_code, subject_actor_code)
        REFERENCES city_realtime_character_case_report_heads(world_id, reporter_actor_code, subject_actor_code)
        ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_report_event_intent_fk
        FOREIGN KEY (world_id, source_intent_code)
        REFERENCES city_realtime_agent_intents(world_id, intent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_report_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_case_report_event_check CHECK (
        reporter_actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND subject_actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND reporter_actor_code <> subject_actor_code
        AND event_sequence = 1
        AND frame_sequence > 0
        AND event_type = 'filed_unverified'
        AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND previous_event_hash ~ '^[0-9a-f]{64}$'
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_case_report_heads_reporter
    ON city_realtime_character_case_report_heads
        (world_id, reporter_actor_code, last_frame_sequence DESC, subject_actor_code ASC);
CREATE INDEX IF NOT EXISTS idx_city_realtime_character_case_report_events_frame
    ON city_realtime_character_case_report_events
        (world_id, frame_sequence, reporter_actor_code, subject_actor_code);

CREATE OR REPLACE FUNCTION city_realtime_character_case_report_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_report_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_social_world_bindings social ON social.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.7.0'
             AND social.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_report_mutation_enabled(
    target_world_id BIGINT,
    target_frame_sequence BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_report_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_case_report_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_social_world_bindings social ON social.world_id = world.id
           JOIN city_realtime_character_case_report_world_bindings report ON report.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.7.0'
             AND social.agent_binding_hash = agent.binding_hash
             AND report.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_report_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_agent_binding_hash VARCHAR(64);
    expected_policy_id VARCHAR(96);
    expected_policy_version VARCHAR(24);
    expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_report_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-report binding is immutable outside genesis initialization'
            USING ERRCODE = '55000';
    END IF;
    SELECT agent.policy_id, agent.policy_version, agent.binding_hash
    INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings agent
    JOIN city_realtime_character_social_world_bindings social
      ON social.world_id = agent.world_id
     AND social.agent_binding_hash = agent.binding_hash
    WHERE agent.world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR
       expected_policy_version <> '1.7.0' OR NEW.schema_version <> 1 OR
       NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-report binding references an invalid agent policy'
            USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-report-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-report binding hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_report_head()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    reporter_kind VARCHAR(16);
    reporter_status VARCHAR(16);
    subject_kind VARCHAR(16);
    subject_status VARCHAR(16);
    active_agent_count BIGINT;
    expected_genesis_hash VARCHAR(64);
    expected_event_hash VARCHAR(64);
    expected_state_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT COALESCE(city_realtime_character_case_report_mutation_enabled(
        NEW.world_id,
        NULLIF(current_setting('sub2api.city_realtime_character_case_report_frame_sequence', TRUE), '')::BIGINT
    ), FALSE) THEN
        RAISE EXCEPTION 'city realtime character case-report heads are immutable outside a sealed reducer'
            USING ERRCODE = '55000';
    END IF;
    SELECT actor_kind, lifecycle_status
    INTO reporter_kind, reporter_status
    FROM city_realtime_actor_identities
    WHERE world_id = NEW.world_id AND actor_code = NEW.reporter_actor_code;
    SELECT actor_kind, lifecycle_status
    INTO subject_kind, subject_status
    FROM city_realtime_actor_identities
    WHERE world_id = NEW.world_id AND actor_code = NEW.subject_actor_code;
    SELECT COUNT(*) INTO active_agent_count
    FROM city_realtime_agent_instances
    WHERE world_id = NEW.world_id
      AND actor_code = NEW.reporter_actor_code
      AND agent_subtype = 'character.user'
      AND lifecycle_status = 'active';
    expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-report-chain-v1',
        NEW.reporter_actor_code, NEW.subject_actor_code
    ), 'UTF8')), 'hex');
    expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-report-event-v1',
        NEW.reporter_actor_code, NEW.subject_actor_code, '1',
        NEW.filed_frame_sequence::text, 'filed_unverified',
        NEW.source_intent_code, expected_genesis_hash
    ), 'UTF8')), 'hex');
    expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-report-state-v1',
        NEW.reporter_actor_code, NEW.subject_actor_code, NEW.report_revision::text,
        NEW.report_status, NEW.source_intent_code, NEW.filed_frame_sequence::text,
        NEW.last_frame_sequence::text, NEW.event_chain_hash
    ), 'UTF8')), 'hex');
    IF reporter_kind IS DISTINCT FROM 'character' OR reporter_status IS DISTINCT FROM 'active'
       OR subject_kind NOT IN ('character', 'npc') OR subject_status IS DISTINCT FROM 'active'
       OR active_agent_count <> 1
       OR NEW.report_revision <> 1 OR NEW.report_status <> 'filed_unverified'
       OR NEW.filed_frame_sequence <> NEW.last_frame_sequence
       OR NEW.event_chain_hash <> expected_event_hash OR NEW.state_hash <> expected_state_hash THEN
        RAISE EXCEPTION 'city realtime character case-report head mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_report_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    head_revision BIGINT;
    head_status VARCHAR(24);
    head_source_intent_code VARCHAR(96);
    head_filed_frame BIGINT;
    head_last_frame BIGINT;
    head_chain_hash VARCHAR(64);
    intent_actor_code VARCHAR(96);
    intent_action_code VARCHAR(64);
    intent_status VARCHAR(16);
    intent_arguments JSONB;
    reporter_kind VARCHAR(16);
    reporter_status VARCHAR(16);
    subject_kind VARCHAR(16);
    subject_status VARCHAR(16);
    reporter_x BIGINT;
    reporter_y BIGINT;
    reporter_z INTEGER;
    subject_x BIGINT;
    subject_y BIGINT;
    subject_z INTEGER;
    expected_genesis_hash VARCHAR(64);
    expected_event_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_report_mutation_enabled(NEW.world_id, NEW.frame_sequence) THEN
        RAISE EXCEPTION 'city realtime character case-report events are append-only sealed facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT report_revision, report_status, source_intent_code, filed_frame_sequence,
           last_frame_sequence, event_chain_hash
    INTO head_revision, head_status, head_source_intent_code, head_filed_frame,
         head_last_frame, head_chain_hash
    FROM city_realtime_character_case_report_heads
    WHERE world_id = NEW.world_id
      AND reporter_actor_code = NEW.reporter_actor_code
      AND subject_actor_code = NEW.subject_actor_code;
    IF NOT FOUND OR head_revision <> 1 OR head_status <> 'filed_unverified'
       OR head_source_intent_code <> NEW.source_intent_code
       OR head_filed_frame <> NEW.frame_sequence OR head_last_frame <> NEW.frame_sequence
       OR head_chain_hash <> NEW.event_hash OR NEW.event_sequence <> 1
       OR NEW.event_type <> 'filed_unverified' THEN
        RAISE EXCEPTION 'city realtime character case-report event head mismatch' USING ERRCODE = '23514';
    END IF;
    SELECT actor_code, action_code, status, arguments
    INTO intent_actor_code, intent_action_code, intent_status, intent_arguments
    FROM city_realtime_agent_intents
    WHERE world_id = NEW.world_id AND intent_code = NEW.source_intent_code;
    IF NOT FOUND OR intent_actor_code <> NEW.reporter_actor_code
       OR intent_action_code <> 'character.case.report.file'
       OR intent_status <> 'pending'
       OR intent_arguments <> jsonb_build_object('target_actor_code', NEW.subject_actor_code) THEN
        RAISE EXCEPTION 'city realtime character case-report event must have an exact pending intent'
            USING ERRCODE = '23514';
    END IF;
    SELECT identity.actor_kind, identity.lifecycle_status, state.x, state.y, state.z
    INTO reporter_kind, reporter_status, reporter_x, reporter_y, reporter_z
    FROM city_realtime_actor_identities identity
    JOIN city_realtime_actor_states state
      ON state.world_id = identity.world_id AND state.actor_code = identity.actor_code
    WHERE identity.world_id = NEW.world_id AND identity.actor_code = NEW.reporter_actor_code;
    SELECT identity.actor_kind, identity.lifecycle_status, state.x, state.y, state.z
    INTO subject_kind, subject_status, subject_x, subject_y, subject_z
    FROM city_realtime_actor_identities identity
    JOIN city_realtime_actor_states state
      ON state.world_id = identity.world_id AND state.actor_code = identity.actor_code
    WHERE identity.world_id = NEW.world_id AND identity.actor_code = NEW.subject_actor_code;
    IF reporter_kind IS DISTINCT FROM 'character' OR reporter_status IS DISTINCT FROM 'active'
       OR subject_kind NOT IN ('character', 'npc') OR subject_status IS DISTINCT FROM 'active'
       OR reporter_x IS NULL OR reporter_y IS NULL OR reporter_z IS NULL
       OR subject_x IS NULL OR subject_y IS NULL OR subject_z IS NULL
       OR reporter_z <> subject_z
       OR NOT (
           (reporter_x > subject_x AND reporter_x - subject_x = 1 AND reporter_y = subject_y)
           OR (subject_x > reporter_x AND subject_x - reporter_x = 1 AND reporter_y = subject_y)
           OR (reporter_y > subject_y AND reporter_y - subject_y = 1 AND reporter_x = subject_x)
           OR (subject_y > reporter_y AND subject_y - reporter_y = 1 AND reporter_x = subject_x)
       ) THEN
        RAISE EXCEPTION 'city realtime character case-report target must remain adjacent and public'
            USING ERRCODE = '23514';
    END IF;
    expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-report-chain-v1',
        NEW.reporter_actor_code, NEW.subject_actor_code
    ), 'UTF8')), 'hex');
    expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-report-event-v1',
        NEW.reporter_actor_code, NEW.subject_actor_code, NEW.event_sequence::text,
        NEW.frame_sequence::text, NEW.event_type, NEW.source_intent_code,
        NEW.previous_event_hash
    ), 'UTF8')), 'hex');
    IF NEW.previous_event_hash <> expected_genesis_hash OR NEW.event_hash <> expected_event_hash THEN
        RAISE EXCEPTION 'city realtime character case-report event hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_case_report_binding_guard
    ON city_realtime_character_case_report_world_bindings;
CREATE TRIGGER city_realtime_character_case_report_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_report_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_report_binding();

DROP TRIGGER IF EXISTS city_realtime_character_case_report_head_guard
    ON city_realtime_character_case_report_heads;
CREATE TRIGGER city_realtime_character_case_report_head_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_report_heads
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_report_head();

DROP TRIGGER IF EXISTS city_realtime_character_case_report_event_guard
    ON city_realtime_character_case_report_events;
CREATE TRIGGER city_realtime_character_case_report_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_report_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_report_event();

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
        OR (action_code = 'character.case.report.file' AND jsonb_typeof(arguments) = 'object'
            AND arguments ? 'target_actor_code' AND jsonb_typeof(arguments -> 'target_actor_code') = 'string'
            AND (arguments - 'target_actor_code') = '{}'::jsonb
            AND (arguments ->> 'target_actor_code') ~ '^[a-z][a-z0-9_.-]{1,95}$')
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
            OR (action_code = 'character.case.report.file' AND actor_code IS NOT NULL
                AND jsonb_typeof(arguments) = 'object' AND arguments ? 'target_actor_code'
                AND jsonb_typeof(arguments -> 'target_actor_code') = 'string'
                AND (arguments - 'target_actor_code') = '{}'::jsonb
                AND (arguments ->> 'target_actor_code') ~ '^[a-z][a-z0-9_.-]{1,95}$')
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

COMMENT ON TABLE city_realtime_character_case_report_world_bindings IS
    'Genesis-pinned non-evidentiary Case-intake adapter for realtime-v2 policy 1.7.0 worlds.';
COMMENT ON TABLE city_realtime_character_case_report_heads IS
    'One immutable unverified intake receipt per reporter/subject pair. It is not a Law Case and cannot create a ruling, sanction, reward, wallet movement, or provider side effect.';
COMMENT ON TABLE city_realtime_character_case_report_events IS
    'Append-only adjacent-public Actor report receipts with no text, evidence claim, private identity, model output, or legal/economic effect.';
