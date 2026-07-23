-- A3.3b / evidence-isolated Case-process foundation. 1.8.0 preserves the
-- 1.7.0 report action surface exactly, but creates a separate server-owned
-- work item beside its immutable filed_unverified receipt. The work item is
-- evidence_required and has exactly one automatic outcome in this slice:
-- expired_no_evidence. A report is not evidence, an accusation, a Law Case,
-- a ruling, a penalty, a reward, or a financial/progression mutation.

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
        "case_report_schema": "city-realtime-character-case-report-v1",
        "case_intake_schema": "city-realtime-character-case-intake-v1"
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.8.0', 'published', 1, manifest,
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
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_case_intake","realtime_agent_case_intake_process"]'::jsonb
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_case_intake' THEN capabilities
    ELSE capabilities ||
        '["realtime_character_case_intake","realtime_agent_case_intake_process"]'::jsonb
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
          AND binding.policy_version IN ('1.1.0', '1.2.0', '1.3.0', '1.4.0', '1.5.0', '1.6.0', '1.7.0', '1.8.0')
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
          AND binding.policy_version IN ('1.2.0', '1.3.0', '1.4.0', '1.5.0', '1.6.0', '1.7.0', '1.8.0')
          AND bundle.status = 'published'
          AND bundle.policy_hash = binding.policy_hash
    )
$$;

-- Retain the old adapters in a 1.8 world without modifying their historical
-- policy bindings or state shapes.
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
             AND agent.policy_version IN ('1.4.0', '1.5.0', '1.6.0', '1.7.0', '1.8.0')
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
             AND agent.policy_version IN ('1.4.0', '1.5.0', '1.6.0', '1.7.0', '1.8.0')
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
       expected_policy_version NOT IN ('1.4.0', '1.5.0', '1.6.0', '1.7.0', '1.8.0') OR NEW.schema_version <> 1 OR
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
             AND agent.policy_version IN ('1.5.0', '1.6.0', '1.7.0', '1.8.0')
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
             AND agent.policy_version IN ('1.5.0', '1.6.0', '1.7.0', '1.8.0')
             AND social.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_social_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_agent_binding_hash VARCHAR(64);
    expected_policy_id VARCHAR(96);
    expected_policy_version VARCHAR(24);
    expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_social_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character social binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT policy_id, policy_version, binding_hash
    INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings WHERE world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR
       expected_policy_version NOT IN ('1.5.0', '1.6.0', '1.7.0', '1.8.0') OR NEW.schema_version <> 1 OR
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
           JOIN city_realtime_character_case_response_world_bindings response ON response.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.6.0', '1.7.0', '1.8.0')
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
           JOIN city_realtime_character_case_review_world_bindings review ON review.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.6.0', '1.7.0', '1.8.0')
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
        RAISE EXCEPTION 'city realtime character case-review binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT agent.policy_id, agent.policy_version, agent.binding_hash
    INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings agent
    JOIN city_realtime_character_case_response_world_bindings response
      ON response.world_id = agent.world_id
     AND response.agent_binding_hash = agent.binding_hash
    WHERE agent.world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR
       expected_policy_version NOT IN ('1.6.0', '1.7.0', '1.8.0') OR NEW.schema_version <> 1 OR
       NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-review binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-review-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-review binding hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

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
             AND agent.policy_version IN ('1.7.0', '1.8.0')
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
             AND agent.policy_version IN ('1.7.0', '1.8.0')
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
       expected_policy_version NOT IN ('1.7.0', '1.8.0') OR NEW.schema_version <> 1 OR
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

CREATE TABLE IF NOT EXISTS city_realtime_character_case_intake_world_bindings (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_realtime_character_case_report_world_bindings(world_id) ON DELETE RESTRICT,
    schema_version SMALLINT NOT NULL,
    agent_binding_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_character_case_intake_binding_check CHECK (
        schema_version = 1
        AND agent_binding_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash = encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-intake-binding-v1', agent_binding_hash), 'UTF8')), 'hex')
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_case_intake_heads (
    world_id BIGINT NOT NULL
        REFERENCES city_realtime_character_case_intake_world_bindings(world_id) ON DELETE RESTRICT,
    reporter_actor_code VARCHAR(96) NOT NULL,
    subject_actor_code VARCHAR(96) NOT NULL,
    report_event_sequence BIGINT NOT NULL,
    report_event_hash VARCHAR(64) NOT NULL,
    intake_revision BIGINT NOT NULL,
    intake_status VARCHAR(24) NOT NULL,
    source_intent_code VARCHAR(96) NOT NULL,
    opened_frame_sequence BIGINT NOT NULL,
    expiration_due_world_time_us BIGINT NOT NULL,
    last_frame_sequence BIGINT NOT NULL,
    event_chain_hash VARCHAR(64) NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, reporter_actor_code, subject_actor_code),
    CONSTRAINT city_realtime_character_case_intake_head_report_fk
        FOREIGN KEY (world_id, reporter_actor_code, subject_actor_code, report_event_sequence)
        REFERENCES city_realtime_character_case_report_events
            (world_id, reporter_actor_code, subject_actor_code, event_sequence)
        ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_intake_head_intent_fk
        FOREIGN KEY (world_id, source_intent_code)
        REFERENCES city_realtime_agent_intents(world_id, intent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_intake_head_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_case_intake_head_check CHECK (
        reporter_actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND subject_actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND reporter_actor_code <> subject_actor_code
        AND report_event_sequence = 1
        AND report_event_hash ~ '^[0-9a-f]{64}$'
        AND intake_revision IN (1, 2)
        AND intake_status IN ('evidence_required', 'expired_no_evidence')
        AND ((intake_revision = 1 AND intake_status = 'evidence_required')
             OR (intake_revision = 2 AND intake_status = 'expired_no_evidence'))
        AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND opened_frame_sequence > 0
        AND expiration_due_world_time_us > 0
        AND mod(expiration_due_world_time_us, 1000000) = 0
        AND last_frame_sequence >= opened_frame_sequence
        AND event_chain_hash ~ '^[0-9a-f]{64}$'
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND state_hash = encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-intake-state-v1',
            reporter_actor_code, subject_actor_code, report_event_sequence::text,
            report_event_hash, intake_revision::text, intake_status,
            source_intent_code, opened_frame_sequence::text,
            expiration_due_world_time_us::text, last_frame_sequence::text,
            event_chain_hash), 'UTF8')), 'hex')
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_case_intake_events (
    world_id BIGINT NOT NULL,
    reporter_actor_code VARCHAR(96) NOT NULL,
    subject_actor_code VARCHAR(96) NOT NULL,
    report_event_sequence BIGINT NOT NULL,
    report_event_hash VARCHAR(64) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    event_type VARCHAR(24) NOT NULL,
    source_intent_code VARCHAR(96) NOT NULL,
    expiration_due_world_time_us BIGINT NOT NULL,
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, reporter_actor_code, subject_actor_code, event_sequence),
    CONSTRAINT city_realtime_character_case_intake_event_head_fk
        FOREIGN KEY (world_id, reporter_actor_code, subject_actor_code)
        REFERENCES city_realtime_character_case_intake_heads(world_id, reporter_actor_code, subject_actor_code)
        ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_intake_event_intent_fk
        FOREIGN KEY (world_id, source_intent_code)
        REFERENCES city_realtime_agent_intents(world_id, intent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_intake_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_case_intake_event_check CHECK (
        reporter_actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND subject_actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND reporter_actor_code <> subject_actor_code
        AND report_event_sequence = 1
        AND report_event_hash ~ '^[0-9a-f]{64}$'
        AND event_sequence IN (1, 2)
        AND ((event_sequence = 1 AND event_type = 'evidence_required')
             OR (event_sequence = 2 AND event_type = 'expired_no_evidence'))
        AND frame_sequence > 0
        AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND expiration_due_world_time_us > 0
        AND mod(expiration_due_world_time_us, 1000000) = 0
        AND previous_event_hash ~ '^[0-9a-f]{64}$'
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_case_intake_heads_reporter
    ON city_realtime_character_case_intake_heads
        (world_id, reporter_actor_code, last_frame_sequence DESC, subject_actor_code ASC);
CREATE INDEX IF NOT EXISTS idx_city_realtime_character_case_intake_events_frame
    ON city_realtime_character_case_intake_events
        (world_id, frame_sequence, reporter_actor_code, subject_actor_code);

CREATE OR REPLACE FUNCTION city_realtime_character_case_intake_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_intake_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_report_world_bindings report ON report.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.8.0'
             AND report.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_intake_mutation_enabled(
    target_world_id BIGINT,
    target_frame_sequence BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_intake_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_case_intake_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_report_world_bindings report ON report.world_id = world.id
           JOIN city_realtime_character_case_intake_world_bindings intake ON intake.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.8.0'
             AND report.agent_binding_hash = agent.binding_hash
             AND intake.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_intake_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_agent_binding_hash VARCHAR(64);
    expected_policy_id VARCHAR(96);
    expected_policy_version VARCHAR(24);
    expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_intake_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-intake binding is immutable outside genesis initialization'
            USING ERRCODE = '55000';
    END IF;
    SELECT agent.policy_id, agent.policy_version, agent.binding_hash
    INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings agent
    JOIN city_realtime_character_case_report_world_bindings report
      ON report.world_id = agent.world_id
     AND report.agent_binding_hash = agent.binding_hash
    WHERE agent.world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR
       expected_policy_version <> '1.8.0' OR NEW.schema_version <> 1 OR
       NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-intake binding references an invalid agent policy'
            USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-intake-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-intake binding hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_intake_head()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    report_status VARCHAR(24);
    report_source_intent_code VARCHAR(96);
    report_filed_frame BIGINT;
    report_chain_hash VARCHAR(64);
    report_event_type VARCHAR(24);
    report_event_frame BIGINT;
    report_event_source_intent_code VARCHAR(96);
    report_previous_event_hash VARCHAR(64);
    expected_report_genesis_hash VARCHAR(64);
    expected_event_hash VARCHAR(64);
    expected_state_hash VARCHAR(64);
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NOT COALESCE(city_realtime_character_case_intake_mutation_enabled(
            NEW.world_id,
            NULLIF(current_setting('sub2api.city_realtime_character_case_intake_frame_sequence', TRUE), '')::BIGINT
        ), FALSE) THEN
            RAISE EXCEPTION 'city realtime character case-intake heads are immutable outside a sealed reducer'
                USING ERRCODE = '55000';
        END IF;
        SELECT report.report_status, report.source_intent_code, report.filed_frame_sequence,
               report.event_chain_hash, event.event_type, event.frame_sequence,
               event.source_intent_code, event.previous_event_hash
        INTO report_status, report_source_intent_code, report_filed_frame,
             report_chain_hash, report_event_type, report_event_frame,
             report_event_source_intent_code, report_previous_event_hash
        FROM city_realtime_character_case_report_heads report
        JOIN city_realtime_character_case_report_events event
          ON event.world_id = report.world_id
         AND event.reporter_actor_code = report.reporter_actor_code
         AND event.subject_actor_code = report.subject_actor_code
         AND event.event_sequence = report.report_revision
        WHERE report.world_id = NEW.world_id
          AND report.reporter_actor_code = NEW.reporter_actor_code
          AND report.subject_actor_code = NEW.subject_actor_code;
        expected_report_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-report-chain-v1',
            NEW.reporter_actor_code, NEW.subject_actor_code
        ), 'UTF8')), 'hex');
        expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-intake-event-v1',
            NEW.reporter_actor_code, NEW.subject_actor_code,
            NEW.report_event_sequence::text, NEW.report_event_hash,
            '1', NEW.opened_frame_sequence::text, 'evidence_required',
            NEW.source_intent_code, NEW.expiration_due_world_time_us::text,
            encode(sha256(convert_to(concat_ws(E'\x1f',
                'city-realtime-character-case-intake-chain-v1',
                NEW.reporter_actor_code, NEW.subject_actor_code,
                NEW.report_event_sequence::text, NEW.report_event_hash
            ), 'UTF8')), 'hex')
        ), 'UTF8')), 'hex');
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-intake-state-v1',
            NEW.reporter_actor_code, NEW.subject_actor_code,
            NEW.report_event_sequence::text, NEW.report_event_hash,
            NEW.intake_revision::text, NEW.intake_status, NEW.source_intent_code,
            NEW.opened_frame_sequence::text, NEW.expiration_due_world_time_us::text,
            NEW.last_frame_sequence::text, NEW.event_chain_hash
        ), 'UTF8')), 'hex');
        IF report_status IS DISTINCT FROM 'filed_unverified'
           OR report_source_intent_code IS DISTINCT FROM NEW.source_intent_code
           OR report_filed_frame IS DISTINCT FROM NEW.opened_frame_sequence
           OR report_chain_hash IS DISTINCT FROM NEW.report_event_hash
           OR report_event_type IS DISTINCT FROM 'filed_unverified'
           OR report_event_frame IS DISTINCT FROM NEW.opened_frame_sequence
           OR report_event_source_intent_code IS DISTINCT FROM NEW.source_intent_code
           OR report_previous_event_hash IS DISTINCT FROM expected_report_genesis_hash
           OR NEW.report_event_sequence <> 1
           OR NEW.intake_revision <> 1 OR NEW.intake_status <> 'evidence_required'
           OR NEW.last_frame_sequence <> NEW.opened_frame_sequence
           OR NEW.event_chain_hash <> expected_event_hash OR NEW.state_hash <> expected_state_hash THEN
            RAISE EXCEPTION 'city realtime character case-intake head mismatch' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'UPDATE' THEN
        IF NOT COALESCE(city_realtime_character_case_intake_mutation_enabled(
            NEW.world_id,
            NULLIF(current_setting('sub2api.city_realtime_character_case_intake_frame_sequence', TRUE), '')::BIGINT
        ), FALSE) THEN
            RAISE EXCEPTION 'city realtime character case-intake heads are immutable outside a sealed reducer'
                USING ERRCODE = '55000';
        END IF;
        IF NEW.reporter_actor_code <> OLD.reporter_actor_code OR NEW.subject_actor_code <> OLD.subject_actor_code
           OR NEW.report_event_sequence <> OLD.report_event_sequence OR NEW.report_event_hash <> OLD.report_event_hash
           OR NEW.intake_revision <> OLD.intake_revision + 1 OR NEW.last_frame_sequence <= OLD.last_frame_sequence
           OR NEW.source_intent_code <> OLD.source_intent_code
           OR NEW.opened_frame_sequence <> OLD.opened_frame_sequence
           OR NEW.expiration_due_world_time_us <> OLD.expiration_due_world_time_us
           OR NEW.event_chain_hash = OLD.event_chain_hash OR NEW.metadata <> OLD.metadata THEN
            RAISE EXCEPTION 'city realtime character case-intake heads may change only through a sealed reducer'
                USING ERRCODE = '55000';
        END IF;
        IF NEW.intake_revision <> 2 OR NEW.intake_status <> 'expired_no_evidence' THEN
            RAISE EXCEPTION 'city realtime character case-intake transition is invalid' USING ERRCODE = '23514';
        END IF;
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-intake-state-v1',
            NEW.reporter_actor_code, NEW.subject_actor_code,
            NEW.report_event_sequence::text, NEW.report_event_hash,
            NEW.intake_revision::text, NEW.intake_status, NEW.source_intent_code,
            NEW.opened_frame_sequence::text, NEW.expiration_due_world_time_us::text,
            NEW.last_frame_sequence::text, NEW.event_chain_hash
        ), 'UTF8')), 'hex');
        IF NEW.state_hash <> expected_state_hash OR NOT EXISTS (
            SELECT 1
            FROM city_realtime_character_case_intake_events event
            WHERE event.world_id = NEW.world_id
              AND event.reporter_actor_code = NEW.reporter_actor_code
              AND event.subject_actor_code = NEW.subject_actor_code
              AND event.event_sequence = NEW.intake_revision
              AND event.frame_sequence = NEW.last_frame_sequence
              AND event.event_hash = NEW.event_chain_hash
              AND event.event_type = 'expired_no_evidence'
              AND event.source_intent_code = NEW.source_intent_code
              AND event.expiration_due_world_time_us = NEW.expiration_due_world_time_us
        ) THEN
            RAISE EXCEPTION 'city realtime character case-intake head event mismatch' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character case-intake heads are immutable outside sealed reducers'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_intake_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    head_revision BIGINT;
    head_status VARCHAR(24);
    head_source_intent_code VARCHAR(96);
    head_opened_frame BIGINT;
    head_due_world_time BIGINT;
    head_chain_hash VARCHAR(64);
    head_report_sequence BIGINT;
    head_report_hash VARCHAR(64);
    intent_actor_code VARCHAR(96);
    intent_action_code VARCHAR(64);
    intent_status VARCHAR(16);
    intent_arguments JSONB;
    report_status VARCHAR(24);
    report_source_intent_code VARCHAR(96);
    report_event_hash VARCHAR(64);
    expected_report_genesis_hash VARCHAR(64);
    expected_intake_genesis_hash VARCHAR(64);
    expected_event_hash VARCHAR(64);
    expected_aggregate_key VARCHAR(160);
    expected_dedup_key VARCHAR(160);
    expected_payload JSONB;
    expected_payload_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_intake_mutation_enabled(NEW.world_id, NEW.frame_sequence) THEN
        RAISE EXCEPTION 'city realtime character case-intake events are append-only sealed facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT intake.intake_revision, intake.intake_status, intake.source_intent_code, intake.opened_frame_sequence,
           intake.expiration_due_world_time_us, intake.event_chain_hash,
           intake.report_event_sequence, intake.report_event_hash
    INTO head_revision, head_status, head_source_intent_code, head_opened_frame,
         head_due_world_time, head_chain_hash, head_report_sequence, head_report_hash
    FROM city_realtime_character_case_intake_heads intake
    WHERE intake.world_id = NEW.world_id
      AND intake.reporter_actor_code = NEW.reporter_actor_code
      AND intake.subject_actor_code = NEW.subject_actor_code;
    IF NOT FOUND OR NEW.report_event_sequence <> head_report_sequence OR NEW.report_event_hash <> head_report_hash
       OR head_revision <> 1
       OR (NEW.event_sequence = 1 AND NEW.frame_sequence <> head_opened_frame)
       OR (NEW.event_sequence = 2 AND NEW.frame_sequence <= head_opened_frame) THEN
        RAISE EXCEPTION 'city realtime character case-intake event head mismatch' USING ERRCODE = '23514';
    END IF;
    SELECT report.report_status, report.source_intent_code, event.event_hash
    INTO report_status, report_source_intent_code, report_event_hash
    FROM city_realtime_character_case_report_heads report
    JOIN city_realtime_character_case_report_events event
      ON event.world_id = report.world_id
     AND event.reporter_actor_code = report.reporter_actor_code
     AND event.subject_actor_code = report.subject_actor_code
     AND event.event_sequence = 1
    WHERE report.world_id = NEW.world_id
      AND report.reporter_actor_code = NEW.reporter_actor_code
      AND report.subject_actor_code = NEW.subject_actor_code;
    IF report_status IS DISTINCT FROM 'filed_unverified'
       OR report_source_intent_code IS DISTINCT FROM NEW.source_intent_code
       OR report_event_hash IS DISTINCT FROM NEW.report_event_hash THEN
        RAISE EXCEPTION 'city realtime character case-intake event must reference its immutable report receipt'
            USING ERRCODE = '23514';
    END IF;
    SELECT actor_code, action_code, status, arguments
    INTO intent_actor_code, intent_action_code, intent_status, intent_arguments
    FROM city_realtime_agent_intents
    WHERE world_id = NEW.world_id AND intent_code = NEW.source_intent_code;
    IF NOT FOUND OR intent_actor_code <> NEW.reporter_actor_code
       OR intent_action_code <> 'character.case.report.file'
       OR intent_arguments <> jsonb_build_object('target_actor_code', NEW.subject_actor_code) THEN
        RAISE EXCEPTION 'city realtime character case-intake event must have an exact report intent'
            USING ERRCODE = '23514';
    END IF;
    expected_report_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-report-chain-v1',
        NEW.reporter_actor_code, NEW.subject_actor_code
    ), 'UTF8')), 'hex');
    expected_intake_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-intake-chain-v1',
        NEW.reporter_actor_code, NEW.subject_actor_code,
        NEW.report_event_sequence::text, NEW.report_event_hash
    ), 'UTF8')), 'hex');
    expected_payload := jsonb_build_object(
        'report_event_hash', NEW.report_event_hash,
        'report_event_sequence', NEW.report_event_sequence,
        'reporter_actor_code', NEW.reporter_actor_code,
        'schema_version', 1,
        'source_intent_code', NEW.source_intent_code,
        'subject_actor_code', NEW.subject_actor_code
    );
    expected_payload_hash := encode(sha256(convert_to(format(
        '{"report_event_hash":"%s","report_event_sequence":%s,"reporter_actor_code":"%s","schema_version":1,"source_intent_code":"%s","subject_actor_code":"%s"}',
        NEW.report_event_hash, NEW.report_event_sequence::text, NEW.reporter_actor_code,
        NEW.source_intent_code, NEW.subject_actor_code
    ), 'UTF8')), 'hex');
    expected_aggregate_key := 'case-intake:' || encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-intake-expiry-v1',
        NEW.reporter_actor_code, NEW.subject_actor_code, NEW.report_event_hash
    ), 'UTF8')), 'hex');
    expected_dedup_key := 'case-intake-expire.' || encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-intake-expiry-v1',
        NEW.reporter_actor_code, NEW.subject_actor_code, NEW.report_event_hash,
        NEW.source_intent_code
    ), 'UTF8')), 'hex');

    IF NEW.event_sequence = 1 THEN
        IF NEW.event_type <> 'evidence_required' OR head_revision <> 1 OR head_status <> 'evidence_required'
           OR head_source_intent_code <> NEW.source_intent_code
           OR head_chain_hash <> NEW.event_hash OR intent_status <> 'pending'
           OR NEW.frame_sequence <> head_opened_frame
           OR NEW.expiration_due_world_time_us <> head_due_world_time
           OR NEW.previous_event_hash <> expected_intake_genesis_hash THEN
            RAISE EXCEPTION 'city realtime character case-intake opening transition is invalid' USING ERRCODE = '23514';
        END IF;
        IF NOT EXISTS (
            SELECT 1
            FROM city_due_events due
            WHERE due.world_id = NEW.world_id
              AND due.event_type = 'system.realtime.character_case_intake_expire'
              AND due.schema_version = 1
              AND due.due_world_time_us = NEW.expiration_due_world_time_us
              AND due.temporal_phase = 'rule_effect'
              AND due.priority = 100
              AND due.aggregate_type = 'realtime_case_intake'
              AND due.aggregate_key = expected_aggregate_key
              AND due.dedup_key = expected_dedup_key
              AND due.source_kind = 'system'
              AND due.source_reference = 'realtime_character_case_intake'
              AND due.payload = expected_payload
              AND due.payload_hash = expected_payload_hash
              AND due.expected_version = 1
              AND due.status = 'pending'
              AND due.created_frame_sequence = NEW.frame_sequence
        ) THEN
            RAISE EXCEPTION 'city realtime character case-intake opening requires one sealed expiry due event'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.event_sequence = 2 THEN
        IF NEW.event_type <> 'expired_no_evidence' OR head_revision <> 1 OR head_status <> 'evidence_required'
           OR head_source_intent_code <> NEW.source_intent_code
           OR intent_status <> 'applied'
           OR NEW.expiration_due_world_time_us <> head_due_world_time
           OR NEW.previous_event_hash <> head_chain_hash THEN
            RAISE EXCEPTION 'city realtime character case-intake expiry transition is invalid' USING ERRCODE = '23514';
        END IF;
        IF NOT EXISTS (
            SELECT 1
            FROM city_due_events due
            WHERE due.world_id = NEW.world_id
              AND due.event_type = 'system.realtime.character_case_intake_expire'
              AND due.schema_version = 1
              AND due.due_world_time_us = NEW.expiration_due_world_time_us
              AND due.temporal_phase = 'rule_effect'
              AND due.priority = 100
              AND due.aggregate_type = 'realtime_case_intake'
              AND due.aggregate_key = expected_aggregate_key
              AND due.dedup_key = expected_dedup_key
              AND due.source_kind = 'system'
              AND due.source_reference = 'realtime_character_case_intake'
              AND due.payload = expected_payload
              AND due.payload_hash = expected_payload_hash
              AND due.expected_version = 1
              AND due.status = 'pending'
              AND due.created_frame_sequence = head_opened_frame
        ) THEN
            RAISE EXCEPTION 'city realtime character case-intake expiry requires its pending sealed due event'
                USING ERRCODE = '23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'city realtime character case-intake event sequence is invalid' USING ERRCODE = '23514';
    END IF;

    expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-intake-event-v1',
        NEW.reporter_actor_code, NEW.subject_actor_code,
        NEW.report_event_sequence::text, NEW.report_event_hash,
        NEW.event_sequence::text, NEW.frame_sequence::text, NEW.event_type,
        NEW.source_intent_code, NEW.expiration_due_world_time_us::text,
        NEW.previous_event_hash
    ), 'UTF8')), 'hex');
    IF NEW.event_hash <> expected_event_hash THEN
        RAISE EXCEPTION 'city realtime character case-intake event hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_case_intake_binding_guard
    ON city_realtime_character_case_intake_world_bindings;
CREATE TRIGGER city_realtime_character_case_intake_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_intake_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_intake_binding();

DROP TRIGGER IF EXISTS city_realtime_character_case_intake_head_guard
    ON city_realtime_character_case_intake_heads;
CREATE TRIGGER city_realtime_character_case_intake_head_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_intake_heads
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_intake_head();

DROP TRIGGER IF EXISTS city_realtime_character_case_intake_event_guard
    ON city_realtime_character_case_intake_events;
CREATE TRIGGER city_realtime_character_case_intake_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_intake_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_intake_event();

COMMENT ON TABLE city_realtime_character_case_intake_heads IS
    'Genesis-pinned, evidence-isolated Case-intake work items for realtime-v2 policy 1.8.0 worlds.';
