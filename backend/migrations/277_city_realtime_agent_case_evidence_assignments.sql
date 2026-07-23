-- A3.3b / bounded evidence-source correlation. 1.10.0 intentionally keeps
-- the 1.9.0 model action catalogue and observation wire shape unchanged.
-- A report never supplies evidence and no model/browser chooses a source.
-- The adapter can only correlate exactly one already-active sealed-Law handle
-- that predates a receipt; source expiry then closes the correlation window.

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
        "case_intake_schema": "city-realtime-character-case-intake-v1",
        "case_evidence_schema": "city-realtime-character-case-evidence-v1",
        "case_evidence_assignment_schema": "city-realtime-character-case-evidence-assignment-v1"
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.10.0', 'published', 1, manifest,
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
           OR NEW.capabilities = OLD.capabilities || '["realtime_actors","actor_position_events","member_safe_actor_projection"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_agents","agent_policy_binding","agent_lifecycle"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_life","realtime_character_activity","realtime_character_inventory"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_metabolism","realtime_character_metabolism_due_events"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_portals","realtime_character_interiors"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_progression","realtime_character_roles"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_agent_observations","realtime_agent_decisions","realtime_agent_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_agent_character_control","realtime_agent_personality_revisions","realtime_agent_activity_intents","realtime_agent_wakeups"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_agent_character_navigation_intents","realtime_agent_character_role_intents","realtime_agent_action_context"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_response","realtime_agent_case_response_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_social","realtime_agent_social_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_review","realtime_agent_case_review_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_report","realtime_agent_case_report_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_intake","realtime_agent_case_intake_process"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_evidence","realtime_agent_case_evidence_sources"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_evidence_assignment","realtime_agent_case_evidence_assignment"]'::jsonb
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_case_evidence_assignment' THEN capabilities
    ELSE capabilities || '["realtime_character_case_evidence_assignment","realtime_agent_case_evidence_assignment"]'::jsonb
END
WHERE version = 'city-openworld-realtime-v2';

CREATE OR REPLACE FUNCTION city_realtime_agent_decision_policy_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_realtime_agent_world_bindings binding
        JOIN city_realtime_agent_policy_bundles bundle
          ON bundle.policy_id = binding.policy_id AND bundle.policy_version = binding.policy_version
        WHERE binding.world_id = target_world_id
          AND binding.policy_id = 'city-realtime-agent-core'
          AND binding.policy_version IN ('1.1.0','1.2.0','1.3.0','1.4.0','1.5.0','1.6.0','1.7.0','1.8.0','1.9.0','1.10.0')
          AND bundle.status = 'published' AND bundle.policy_hash = binding.policy_hash
    )
$$;

CREATE OR REPLACE FUNCTION city_realtime_agent_personality_policy_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_realtime_agent_world_bindings binding
        JOIN city_realtime_agent_policy_bundles bundle
          ON bundle.policy_id = binding.policy_id AND bundle.policy_version = binding.policy_version
        WHERE binding.world_id = target_world_id
          AND binding.policy_id = 'city-realtime-agent-core'
          AND binding.policy_version IN ('1.2.0','1.3.0','1.4.0','1.5.0','1.6.0','1.7.0','1.8.0','1.9.0','1.10.0')
          AND bundle.status = 'published' AND bundle.policy_hash = binding.policy_hash
    )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_response_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_response_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.4.0','1.5.0','1.6.0','1.7.0','1.8.0','1.9.0','1.10.0')
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_response_mutation_enabled(target_world_id BIGINT, target_frame_sequence BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_response_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_case_response_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_character_case_response_world_bindings response ON response.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running' AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.4.0','1.5.0','1.6.0','1.7.0','1.8.0','1.9.0','1.10.0')
             AND response.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_response_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE expected_agent_binding_hash VARCHAR(64); expected_policy_id VARCHAR(96); expected_policy_version VARCHAR(24); expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_response_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-response binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT policy_id, policy_version, binding_hash INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings WHERE world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR expected_policy_version NOT IN ('1.4.0','1.5.0','1.6.0','1.7.0','1.8.0','1.9.0','1.10.0')
       OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-response binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f', 'city-realtime-character-case-response-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN RAISE EXCEPTION 'city realtime character case-response binding hash mismatch' USING ERRCODE = '23514'; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_social_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_social_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.5.0','1.6.0','1.7.0','1.8.0','1.9.0','1.10.0')
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_social_mutation_enabled(target_world_id BIGINT, target_frame_sequence BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_social_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_social_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_character_social_world_bindings social ON social.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running' AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.5.0','1.6.0','1.7.0','1.8.0','1.9.0','1.10.0')
             AND social.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_social_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE expected_agent_binding_hash VARCHAR(64); expected_policy_id VARCHAR(96); expected_policy_version VARCHAR(24); expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_social_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character social binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT policy_id, policy_version, binding_hash INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash FROM city_realtime_agent_world_bindings WHERE world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR expected_policy_version NOT IN ('1.5.0','1.6.0','1.7.0','1.8.0','1.9.0','1.10.0')
       OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character social binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f', 'city-realtime-character-social-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN RAISE EXCEPTION 'city realtime character social binding hash mismatch' USING ERRCODE = '23514'; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_review_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_review_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_response_world_bindings response ON response.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.6.0','1.7.0','1.8.0','1.9.0','1.10.0')
             AND response.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_review_mutation_enabled(target_world_id BIGINT, target_frame_sequence BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_review_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_case_review_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_character_case_review_world_bindings review ON review.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running' AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.6.0','1.7.0','1.8.0','1.9.0','1.10.0')
             AND review.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_review_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE expected_agent_binding_hash VARCHAR(64); expected_policy_id VARCHAR(96); expected_policy_version VARCHAR(24); expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_review_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-review binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT agent.policy_id, agent.policy_version, agent.binding_hash INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings agent JOIN city_realtime_character_case_response_world_bindings response
      ON response.world_id = agent.world_id AND response.agent_binding_hash = agent.binding_hash WHERE agent.world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR expected_policy_version NOT IN ('1.6.0','1.7.0','1.8.0','1.9.0','1.10.0')
       OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-review binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f', 'city-realtime-character-case-review-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN RAISE EXCEPTION 'city realtime character case-review binding hash mismatch' USING ERRCODE = '23514'; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_report_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_report_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_social_world_bindings social ON social.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.7.0','1.8.0','1.9.0','1.10.0')
             AND social.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_report_mutation_enabled(target_world_id BIGINT, target_frame_sequence BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_report_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_case_report_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_character_case_report_world_bindings report ON report.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running' AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.7.0','1.8.0','1.9.0','1.10.0')
             AND report.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_report_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE expected_agent_binding_hash VARCHAR(64); expected_policy_id VARCHAR(96); expected_policy_version VARCHAR(24); expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_report_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-report binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT agent.policy_id, agent.policy_version, agent.binding_hash INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings agent JOIN city_realtime_character_social_world_bindings social
      ON social.world_id = agent.world_id AND social.agent_binding_hash = agent.binding_hash WHERE agent.world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR expected_policy_version NOT IN ('1.7.0','1.8.0','1.9.0','1.10.0')
       OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-report binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f', 'city-realtime-character-case-report-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN RAISE EXCEPTION 'city realtime character case-report binding hash mismatch' USING ERRCODE = '23514'; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_intake_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_intake_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_report_world_bindings report ON report.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.8.0','1.9.0','1.10.0')
             AND report.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_intake_mutation_enabled(target_world_id BIGINT, target_frame_sequence BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_intake_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_case_intake_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_report_world_bindings report ON report.world_id = world.id
           JOIN city_realtime_character_case_intake_world_bindings intake ON intake.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running' AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version IN ('1.8.0','1.9.0','1.10.0')
             AND report.agent_binding_hash = agent.binding_hash AND intake.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_intake_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE expected_agent_binding_hash VARCHAR(64); expected_policy_id VARCHAR(96); expected_policy_version VARCHAR(24); expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_intake_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-intake binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT agent.policy_id, agent.policy_version, agent.binding_hash INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings agent JOIN city_realtime_character_case_report_world_bindings report
      ON report.world_id = agent.world_id AND report.agent_binding_hash = agent.binding_hash WHERE agent.world_id = NEW.world_id;
    IF NOT FOUND OR expected_policy_id <> 'city-realtime-agent-core' OR expected_policy_version NOT IN ('1.8.0','1.9.0','1.10.0')
       OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-intake binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f', 'city-realtime-character-case-intake-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN RAISE EXCEPTION 'city realtime character case-intake binding hash mismatch' USING ERRCODE = '23514'; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_evidence_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_evidence_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_intake_world_bindings intake ON intake.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core' AND agent.policy_version IN ('1.9.0','1.10.0')
             AND intake.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_evidence_mutation_enabled(target_world_id BIGINT, target_frame_sequence BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_evidence_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_case_evidence_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_evidence_world_bindings evidence ON evidence.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running' AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core' AND agent.policy_version IN ('1.9.0','1.10.0')
             AND evidence.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_evidence_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE expected_agent_binding_hash VARCHAR(64); expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_evidence_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-evidence binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT agent.binding_hash INTO expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings agent JOIN city_realtime_character_case_intake_world_bindings intake
      ON intake.world_id = agent.world_id AND intake.agent_binding_hash = agent.binding_hash
    WHERE agent.world_id = NEW.world_id AND agent.policy_id = 'city-realtime-agent-core' AND agent.policy_version IN ('1.9.0','1.10.0');
    IF NOT FOUND OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-evidence binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f', 'city-realtime-character-case-evidence-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN RAISE EXCEPTION 'city realtime character case-evidence binding hash mismatch' USING ERRCODE = '23514'; END IF;
    RETURN NEW;
END;
$$;

CREATE TABLE IF NOT EXISTS city_realtime_character_case_evidence_assignment_world_bindings (
    world_id BIGINT PRIMARY KEY REFERENCES city_realtime_character_case_evidence_world_bindings(world_id) ON DELETE RESTRICT,
    schema_version SMALLINT NOT NULL,
    agent_binding_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_character_case_evidence_assignment_binding_check CHECK (
        schema_version = 1 AND agent_binding_hash ~ '^[0-9a-f]{64}$' AND binding_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash = encode(sha256(convert_to(concat_ws(E'\x1f', 'city-realtime-character-case-evidence-assignment-binding-v1', agent_binding_hash), 'UTF8')), 'hex')
        AND metadata = '{}'::jsonb
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_case_evidence_assignment_heads (
    world_id BIGINT NOT NULL REFERENCES city_realtime_character_case_evidence_assignment_world_bindings(world_id) ON DELETE RESTRICT,
    reporter_actor_code VARCHAR(96) NOT NULL,
    subject_actor_code VARCHAR(96) NOT NULL,
    report_event_sequence BIGINT NOT NULL,
    report_event_hash VARCHAR(64) NOT NULL,
    evidence_code VARCHAR(96) NOT NULL,
    source_law_event_sequence BIGINT NOT NULL,
    source_law_event_hash VARCHAR(64) NOT NULL,
    source_frame_sequence BIGINT NOT NULL,
    assignment_revision BIGINT NOT NULL,
    assignment_status VARCHAR(24) NOT NULL,
    assigned_frame_sequence BIGINT NOT NULL,
    last_frame_sequence BIGINT NOT NULL,
    event_chain_hash VARCHAR(64) NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, reporter_actor_code, subject_actor_code),
    CONSTRAINT city_realtime_case_evidence_assignment_head_evidence_uq UNIQUE (world_id, evidence_code),
    CONSTRAINT city_realtime_character_case_evidence_assignment_head_report_fk
        FOREIGN KEY (world_id, reporter_actor_code, subject_actor_code, report_event_sequence)
        REFERENCES city_realtime_character_case_report_events(world_id, reporter_actor_code, subject_actor_code, event_sequence) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_evidence_assignment_head_intake_fk
        FOREIGN KEY (world_id, reporter_actor_code, subject_actor_code)
        REFERENCES city_realtime_character_case_intake_heads(world_id, reporter_actor_code, subject_actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_case_evidence_assignment_head_evidence_fk
        FOREIGN KEY (world_id, evidence_code)
        REFERENCES city_realtime_character_case_evidence_heads(world_id, evidence_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_evidence_assignment_head_law_fk
        FOREIGN KEY (world_id, subject_actor_code, source_law_event_sequence)
        REFERENCES city_realtime_character_law_events(world_id, actor_code, event_sequence) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_evidence_assignment_head_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_case_evidence_assignment_head_check CHECK (
        reporter_actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND subject_actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND reporter_actor_code <> subject_actor_code AND report_event_sequence = 1 AND report_event_hash ~ '^[0-9a-f]{64}$'
        AND evidence_code = 'evidence.law.' || source_law_event_hash AND evidence_code ~ '^evidence[.]law[.][0-9a-f]{64}$'
        AND source_law_event_sequence > 0 AND source_law_event_hash ~ '^[0-9a-f]{64}$' AND source_frame_sequence > 0
        AND assignment_revision IN (1,2)
        AND ((assignment_revision = 1 AND assignment_status = 'linked_active')
             OR (assignment_revision = 2 AND assignment_status = 'source_window_closed'))
        AND assigned_frame_sequence > 0 AND last_frame_sequence >= assigned_frame_sequence
        AND event_chain_hash ~ '^[0-9a-f]{64}$' AND state_hash ~ '^[0-9a-f]{64}$'
        AND state_hash = encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-evidence-assignment-state-v1', reporter_actor_code, subject_actor_code,
            report_event_sequence::text, report_event_hash, evidence_code, source_law_event_sequence::text,
            source_law_event_hash, source_frame_sequence::text, assignment_revision::text, assignment_status,
            assigned_frame_sequence::text, last_frame_sequence::text, event_chain_hash
        ), 'UTF8')), 'hex')
        AND metadata = '{}'::jsonb
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_case_evidence_assignment_events (
    world_id BIGINT NOT NULL,
    reporter_actor_code VARCHAR(96) NOT NULL,
    subject_actor_code VARCHAR(96) NOT NULL,
    report_event_sequence BIGINT NOT NULL,
    report_event_hash VARCHAR(64) NOT NULL,
    evidence_code VARCHAR(96) NOT NULL,
    source_law_event_sequence BIGINT NOT NULL,
    source_law_event_hash VARCHAR(64) NOT NULL,
    source_frame_sequence BIGINT NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    event_type VARCHAR(32) NOT NULL,
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, reporter_actor_code, subject_actor_code, event_sequence),
    CONSTRAINT city_realtime_character_case_evidence_assignment_event_head_fk
        FOREIGN KEY (world_id, reporter_actor_code, subject_actor_code)
        REFERENCES city_realtime_character_case_evidence_assignment_heads(world_id, reporter_actor_code, subject_actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_evidence_assignment_event_evidence_fk
        FOREIGN KEY (world_id, evidence_code)
        REFERENCES city_realtime_character_case_evidence_heads(world_id, evidence_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_evidence_assignment_event_law_fk
        FOREIGN KEY (world_id, subject_actor_code, source_law_event_sequence)
        REFERENCES city_realtime_character_law_events(world_id, actor_code, event_sequence) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_case_evidence_assignment_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_case_evidence_assignment_event_check CHECK (
        reporter_actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND subject_actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND reporter_actor_code <> subject_actor_code AND report_event_sequence = 1 AND report_event_hash ~ '^[0-9a-f]{64}$'
        AND evidence_code = 'evidence.law.' || source_law_event_hash AND evidence_code ~ '^evidence[.]law[.][0-9a-f]{64}$'
        AND source_law_event_sequence > 0 AND source_law_event_hash ~ '^[0-9a-f]{64}$' AND source_frame_sequence > 0
        AND event_sequence IN (1,2)
        AND ((event_sequence = 1 AND event_type = 'independent_record_linked')
             OR (event_sequence = 2 AND event_type = 'source_window_closed'))
        AND frame_sequence > 0 AND previous_event_hash ~ '^[0-9a-f]{64}$' AND event_hash ~ '^[0-9a-f]{64}$'
        AND metadata = '{}'::jsonb
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_case_evidence_assignment_heads_reporter
    ON city_realtime_character_case_evidence_assignment_heads (world_id, reporter_actor_code, last_frame_sequence DESC, subject_actor_code ASC);
CREATE INDEX IF NOT EXISTS idx_city_realtime_character_case_evidence_assignment_events_frame
    ON city_realtime_character_case_evidence_assignment_events (world_id, frame_sequence, evidence_code);

CREATE OR REPLACE FUNCTION city_realtime_character_case_evidence_assignment_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_evidence_assignment_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_intake_world_bindings intake ON intake.world_id = world.id
           JOIN city_realtime_character_case_evidence_world_bindings evidence ON evidence.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core' AND agent.policy_version = '1.10.0'
             AND intake.agent_binding_hash = agent.binding_hash AND evidence.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_evidence_assignment_mutation_enabled(target_world_id BIGINT, target_frame_sequence BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_evidence_assignment_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_case_evidence_assignment_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_intake_world_bindings intake ON intake.world_id = world.id
           JOIN city_realtime_character_case_evidence_world_bindings evidence ON evidence.world_id = world.id
           JOIN city_realtime_character_case_evidence_assignment_world_bindings assignment ON assignment.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running' AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core' AND agent.policy_version = '1.10.0'
             AND intake.agent_binding_hash = agent.binding_hash AND evidence.agent_binding_hash = agent.binding_hash
             AND assignment.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_evidence_assignment_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE expected_agent_binding_hash VARCHAR(64); expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_evidence_assignment_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-evidence assignment binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT agent.binding_hash INTO expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings agent
    JOIN city_realtime_character_case_intake_world_bindings intake ON intake.world_id = agent.world_id AND intake.agent_binding_hash = agent.binding_hash
    JOIN city_realtime_character_case_evidence_world_bindings evidence ON evidence.world_id = agent.world_id AND evidence.agent_binding_hash = agent.binding_hash
    WHERE agent.world_id = NEW.world_id AND agent.policy_id = 'city-realtime-agent-core' AND agent.policy_version = '1.10.0';
    IF NOT FOUND OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-evidence assignment binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f', 'city-realtime-character-case-evidence-assignment-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN RAISE EXCEPTION 'city realtime character case-evidence assignment binding hash mismatch' USING ERRCODE = '23514'; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_evidence_assignment_head()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE report_hash VARCHAR(64); report_frame BIGINT; intake_hash VARCHAR(64); intake_revision BIGINT; intake_status VARCHAR(24); intake_frame BIGINT;
DECLARE source_sequence BIGINT; source_hash VARCHAR(64); source_frame BIGINT; source_revision BIGINT; source_status VARCHAR(16); source_last_frame BIGINT;
DECLARE expected_genesis_hash VARCHAR(64); expected_event_hash VARCHAR(64); expected_state_hash VARCHAR(64);
BEGIN
    IF NOT COALESCE(city_realtime_character_case_evidence_assignment_mutation_enabled(
        NEW.world_id, NULLIF(current_setting('sub2api.city_realtime_character_case_evidence_assignment_frame_sequence', TRUE), '')::BIGINT
    ), FALSE) THEN
        RAISE EXCEPTION 'city realtime character case-evidence assignment heads are immutable outside a sealed reducer' USING ERRCODE = '55000';
    END IF;
    SELECT report.event_hash, report.frame_sequence, intake.report_event_hash, intake.intake_revision, intake.intake_status, intake.opened_frame_sequence
    INTO report_hash, report_frame, intake_hash, intake_revision, intake_status, intake_frame
    FROM city_realtime_character_case_report_events report
    JOIN city_realtime_character_case_intake_heads intake
      ON intake.world_id = report.world_id AND intake.reporter_actor_code = report.reporter_actor_code AND intake.subject_actor_code = report.subject_actor_code
    WHERE report.world_id = NEW.world_id AND report.reporter_actor_code = NEW.reporter_actor_code
      AND report.subject_actor_code = NEW.subject_actor_code AND report.event_sequence = NEW.report_event_sequence;
    IF NOT FOUND OR report_hash <> NEW.report_event_hash OR intake_hash <> NEW.report_event_hash THEN
        RAISE EXCEPTION 'city realtime character case-evidence assignment report/intake mismatch' USING ERRCODE = '23514';
    END IF;
    SELECT source_law_event_sequence, source_law_event_hash, source_frame_sequence, evidence_revision, evidence_status, last_frame_sequence
    INTO source_sequence, source_hash, source_frame, source_revision, source_status, source_last_frame
    FROM city_realtime_character_case_evidence_heads
    WHERE world_id = NEW.world_id AND evidence_code = NEW.evidence_code AND subject_actor_code = NEW.subject_actor_code;
    IF NOT FOUND OR source_sequence <> NEW.source_law_event_sequence OR source_hash <> NEW.source_law_event_hash OR source_frame <> NEW.source_frame_sequence THEN
        RAISE EXCEPTION 'city realtime character case-evidence assignment source mismatch' USING ERRCODE = '23514';
    END IF;
    expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-evidence-assignment-state-v1', NEW.reporter_actor_code, NEW.subject_actor_code,
        NEW.report_event_sequence::text, NEW.report_event_hash, NEW.evidence_code, NEW.source_law_event_sequence::text,
        NEW.source_law_event_hash, NEW.source_frame_sequence::text, NEW.assignment_revision::text, NEW.assignment_status,
        NEW.assigned_frame_sequence::text, NEW.last_frame_sequence::text, NEW.event_chain_hash
    ), 'UTF8')), 'hex');
    IF TG_OP = 'INSERT' THEN
        expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-evidence-assignment-chain-v1', NEW.reporter_actor_code, NEW.subject_actor_code,
            NEW.report_event_sequence::text, NEW.report_event_hash, NEW.evidence_code, NEW.source_law_event_sequence::text,
            NEW.source_law_event_hash, NEW.source_frame_sequence::text
        ), 'UTF8')), 'hex');
        expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-evidence-assignment-event-v1', NEW.reporter_actor_code, NEW.subject_actor_code,
            NEW.report_event_sequence::text, NEW.report_event_hash, NEW.evidence_code, NEW.source_law_event_sequence::text,
            NEW.source_law_event_hash, NEW.source_frame_sequence::text, '1', NEW.assigned_frame_sequence::text,
            'independent_record_linked', expected_genesis_hash
        ), 'UTF8')), 'hex');
        IF source_revision <> 1 OR source_status <> 'active' OR source_frame >= NEW.assigned_frame_sequence
           OR report_frame <> NEW.assigned_frame_sequence OR intake_frame <> NEW.assigned_frame_sequence
           OR intake_revision <> 1 OR intake_status <> 'evidence_required'
           OR NEW.assignment_revision <> 1 OR NEW.assignment_status <> 'linked_active'
           OR NEW.last_frame_sequence <> NEW.assigned_frame_sequence
           OR NEW.event_chain_hash <> expected_event_hash OR NEW.state_hash <> expected_state_hash THEN
            RAISE EXCEPTION 'city realtime character case-evidence assignment head link mismatch' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NEW.reporter_actor_code <> OLD.reporter_actor_code OR NEW.subject_actor_code <> OLD.subject_actor_code
           OR NEW.report_event_sequence <> OLD.report_event_sequence OR NEW.report_event_hash <> OLD.report_event_hash
           OR NEW.evidence_code <> OLD.evidence_code OR NEW.source_law_event_sequence <> OLD.source_law_event_sequence
           OR NEW.source_law_event_hash <> OLD.source_law_event_hash OR NEW.source_frame_sequence <> OLD.source_frame_sequence
           OR NEW.assigned_frame_sequence <> OLD.assigned_frame_sequence OR NEW.assignment_revision <> OLD.assignment_revision + 1
           OR NEW.assignment_revision <> 2 OR NEW.assignment_status <> 'source_window_closed' OR NEW.last_frame_sequence <= OLD.last_frame_sequence
           OR source_revision <> 2 OR source_status <> 'expired' OR source_last_frame <> NEW.last_frame_sequence
           OR NEW.event_chain_hash = OLD.event_chain_hash OR NEW.state_hash <> expected_state_hash OR NEW.metadata <> OLD.metadata THEN
            RAISE EXCEPTION 'city realtime character case-evidence assignment head transition is invalid' USING ERRCODE = '23514';
        END IF;
        IF NOT EXISTS (
            SELECT 1 FROM city_realtime_character_case_evidence_assignment_events event
            WHERE event.world_id = NEW.world_id AND event.reporter_actor_code = NEW.reporter_actor_code
              AND event.subject_actor_code = NEW.subject_actor_code AND event.event_sequence = 2
              AND event.frame_sequence = NEW.last_frame_sequence AND event.event_type = 'source_window_closed'
              AND event.event_hash = NEW.event_chain_hash
        ) THEN
            RAISE EXCEPTION 'city realtime character case-evidence assignment close event mismatch' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character case-evidence assignment heads are immutable outside sealed reducers' USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_evidence_assignment_event()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE head_revision BIGINT; head_status VARCHAR(24); head_assigned_frame BIGINT; head_chain_hash VARCHAR(64);
DECLARE source_revision BIGINT; source_status VARCHAR(16); source_last_frame BIGINT;
DECLARE expected_genesis_hash VARCHAR(64); expected_event_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_evidence_assignment_mutation_enabled(NEW.world_id, NEW.frame_sequence) THEN
        RAISE EXCEPTION 'city realtime character case-evidence assignment events are append-only sealed facts' USING ERRCODE = '55000';
    END IF;
    SELECT assignment_revision, assignment_status, assigned_frame_sequence, event_chain_hash
    INTO head_revision, head_status, head_assigned_frame, head_chain_hash
    FROM city_realtime_character_case_evidence_assignment_heads
    WHERE world_id = NEW.world_id AND reporter_actor_code = NEW.reporter_actor_code
      AND subject_actor_code = NEW.subject_actor_code
      AND report_event_sequence = NEW.report_event_sequence AND report_event_hash = NEW.report_event_hash
      AND evidence_code = NEW.evidence_code AND source_law_event_sequence = NEW.source_law_event_sequence
      AND source_law_event_hash = NEW.source_law_event_hash AND source_frame_sequence = NEW.source_frame_sequence;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city realtime character case-evidence assignment event head mismatch' USING ERRCODE = '23514';
    END IF;
    SELECT evidence_revision, evidence_status, last_frame_sequence INTO source_revision, source_status, source_last_frame
    FROM city_realtime_character_case_evidence_heads
    WHERE world_id = NEW.world_id AND evidence_code = NEW.evidence_code AND subject_actor_code = NEW.subject_actor_code
      AND source_law_event_sequence = NEW.source_law_event_sequence
      AND source_law_event_hash = NEW.source_law_event_hash AND source_frame_sequence = NEW.source_frame_sequence;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city realtime character case-evidence assignment event source missing' USING ERRCODE = '23514';
    END IF;
    IF NEW.event_sequence = 1 THEN
        expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-evidence-assignment-chain-v1', NEW.reporter_actor_code, NEW.subject_actor_code,
            NEW.report_event_sequence::text, NEW.report_event_hash, NEW.evidence_code, NEW.source_law_event_sequence::text,
            NEW.source_law_event_hash, NEW.source_frame_sequence::text
        ), 'UTF8')), 'hex');
        IF head_revision <> 1 OR head_status <> 'linked_active' OR source_revision <> 1 OR source_status <> 'active'
           OR NEW.event_type <> 'independent_record_linked' OR NEW.frame_sequence <> head_assigned_frame
           OR NEW.previous_event_hash <> expected_genesis_hash THEN
            RAISE EXCEPTION 'city realtime character case-evidence assignment link event is invalid' USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.event_sequence = 2 THEN
        IF head_revision <> 1 OR head_status <> 'linked_active' OR source_revision <> 2 OR source_status <> 'expired'
           OR NEW.event_type <> 'source_window_closed' OR NEW.frame_sequence <> source_last_frame
           OR NEW.previous_event_hash <> head_chain_hash THEN
            RAISE EXCEPTION 'city realtime character case-evidence assignment close event is invalid' USING ERRCODE = '23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'city realtime character case-evidence assignment event sequence is invalid' USING ERRCODE = '23514';
    END IF;
    expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-evidence-assignment-event-v1', NEW.reporter_actor_code, NEW.subject_actor_code,
        NEW.report_event_sequence::text, NEW.report_event_hash, NEW.evidence_code, NEW.source_law_event_sequence::text,
        NEW.source_law_event_hash, NEW.source_frame_sequence::text, NEW.event_sequence::text, NEW.frame_sequence::text,
        NEW.event_type, NEW.previous_event_hash
    ), 'UTF8')), 'hex');
    IF NEW.event_hash <> expected_event_hash THEN RAISE EXCEPTION 'city realtime character case-evidence assignment event hash mismatch' USING ERRCODE = '23514'; END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_case_evidence_assignment_binding_guard ON city_realtime_character_case_evidence_assignment_world_bindings;
CREATE TRIGGER city_realtime_character_case_evidence_assignment_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_evidence_assignment_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_evidence_assignment_binding();

DROP TRIGGER IF EXISTS city_realtime_character_case_evidence_assignment_head_guard ON city_realtime_character_case_evidence_assignment_heads;
CREATE TRIGGER city_realtime_character_case_evidence_assignment_head_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_evidence_assignment_heads
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_evidence_assignment_head();

DROP TRIGGER IF EXISTS city_realtime_character_case_evidence_assignment_event_guard ON city_realtime_character_case_evidence_assignment_events;
CREATE TRIGGER city_realtime_character_case_evidence_assignment_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_evidence_assignment_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_evidence_assignment_event();

COMMENT ON TABLE city_realtime_character_case_evidence_assignment_world_bindings IS
    'Genesis-pinned 1.10 source-correlation adapter; it grants no user, Agent, or model evidence authority.';
COMMENT ON TABLE city_realtime_character_case_evidence_assignment_heads IS
    'Append-only procedural correlations between a receipt and one independent sealed-Law source handle; never a case, allegation, ruling, penalty, reward, or wallet entry.';
COMMENT ON TABLE city_realtime_character_case_evidence_assignment_events IS
    'Sealed lifecycle of a source correlation. Source expiry closes future procedural use without rewriting any report, intake, Law event, Rule, Case, or asset state.';
