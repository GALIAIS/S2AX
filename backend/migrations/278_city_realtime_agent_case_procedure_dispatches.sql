-- A3.3b / constrained procedural dispatch. 1.11.0 preserves the complete
-- 1.10.0 model action and observation contracts. It only appends a
-- server-owned routing receipt after a unique sealed source correlation; it
-- creates no allegation, evidence finding, reviewer selection, case,
-- adjudication, penalty, reward, wallet, or other asset effect.

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
        "case_evidence_assignment_schema": "city-realtime-character-case-evidence-assignment-v1",
        "case_procedure_dispatch_schema": "city-realtime-character-case-procedure-dispatch-v1"
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.11.0', 'published', 1, manifest,
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
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_review","realtime_agent_case_review_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_report","realtime_agent_case_report_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_intake","realtime_agent_case_intake_work_items"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_evidence","realtime_agent_case_evidence_sources"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_evidence_assignment","realtime_agent_case_evidence_assignment"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_procedure_dispatch","realtime_agent_case_procedure_dispatch"]'::jsonb
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_case_procedure_dispatch' THEN capabilities
    ELSE capabilities || '["realtime_character_case_procedure_dispatch","realtime_agent_case_procedure_dispatch"]'::jsonb
END
WHERE version = 'city-openworld-realtime-v2';

-- The adapter chain is pinned to an explicit finite policy set. Never infer a
-- future policy from semver ordering: a later policy must be published and
-- admitted here before it can activate any historical reducer.
CREATE OR REPLACE FUNCTION city_realtime_agent_core_policy_at_least(
    target_policy_id VARCHAR,
    target_policy_version VARCHAR,
    minimum_minor INTEGER
)
RETURNS BOOLEAN
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT target_policy_id = 'city-realtime-agent-core' AND CASE target_policy_version
        WHEN '1.1.0' THEN 1 >= minimum_minor
        WHEN '1.2.0' THEN 2 >= minimum_minor
        WHEN '1.3.0' THEN 3 >= minimum_minor
        WHEN '1.4.0' THEN 4 >= minimum_minor
        WHEN '1.5.0' THEN 5 >= minimum_minor
        WHEN '1.6.0' THEN 6 >= minimum_minor
        WHEN '1.7.0' THEN 7 >= minimum_minor
        WHEN '1.8.0' THEN 8 >= minimum_minor
        WHEN '1.9.0' THEN 9 >= minimum_minor
        WHEN '1.10.0' THEN 10 >= minimum_minor
        WHEN '1.11.0' THEN 11 >= minimum_minor
        ELSE FALSE
    END
$$;

CREATE OR REPLACE FUNCTION city_realtime_agent_decision_policy_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_realtime_agent_world_bindings binding
        JOIN city_realtime_agent_policy_bundles bundle
          ON bundle.policy_id = binding.policy_id AND bundle.policy_version = binding.policy_version
        WHERE binding.world_id = target_world_id
          AND city_realtime_agent_core_policy_at_least(binding.policy_id, binding.policy_version, 1)
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
          AND city_realtime_agent_core_policy_at_least(binding.policy_id, binding.policy_version, 2)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 4)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 4)
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
    IF NOT FOUND OR NOT city_realtime_agent_core_policy_at_least(expected_policy_id, expected_policy_version, 4)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 5)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 5)
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
    SELECT policy_id, policy_version, binding_hash INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings WHERE world_id = NEW.world_id;
    IF NOT FOUND OR NOT city_realtime_agent_core_policy_at_least(expected_policy_id, expected_policy_version, 5)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 6)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 6)
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
    IF NOT FOUND OR NOT city_realtime_agent_core_policy_at_least(expected_policy_id, expected_policy_version, 6)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 7)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 7)
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
    IF NOT FOUND OR NOT city_realtime_agent_core_policy_at_least(expected_policy_id, expected_policy_version, 7)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 8)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 8)
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
    IF NOT FOUND OR NOT city_realtime_agent_core_policy_at_least(expected_policy_id, expected_policy_version, 8)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 9)
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
           JOIN city_realtime_character_case_intake_world_bindings intake ON intake.world_id = world.id
           JOIN city_realtime_character_case_evidence_world_bindings evidence ON evidence.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running' AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 9)
             AND intake.agent_binding_hash = agent.binding_hash AND evidence.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_evidence_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE expected_agent_binding_hash VARCHAR(64); expected_policy_id VARCHAR(96); expected_policy_version VARCHAR(24); expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_evidence_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-evidence binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT agent.policy_id, agent.policy_version, agent.binding_hash INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings agent JOIN city_realtime_character_case_intake_world_bindings intake
      ON intake.world_id = agent.world_id AND intake.agent_binding_hash = agent.binding_hash WHERE agent.world_id = NEW.world_id;
    IF NOT FOUND OR NOT city_realtime_agent_core_policy_at_least(expected_policy_id, expected_policy_version, 9)
       OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-evidence binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f', 'city-realtime-character-case-evidence-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN RAISE EXCEPTION 'city realtime character case-evidence binding hash mismatch' USING ERRCODE = '23514'; END IF;
    RETURN NEW;
END;
$$;

-- 1.10 assignment reducers remain unchanged; this replacement only admits
-- 1.11 worlds while preserving the same sealed-source prerequisites.
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 10)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 10)
             AND intake.agent_binding_hash = agent.binding_hash AND evidence.agent_binding_hash = agent.binding_hash
             AND assignment.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_evidence_assignment_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE expected_agent_binding_hash VARCHAR(64); expected_policy_id VARCHAR(96); expected_policy_version VARCHAR(24); expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_evidence_assignment_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-evidence assignment binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT agent.policy_id, agent.policy_version, agent.binding_hash INTO expected_policy_id, expected_policy_version, expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings agent
    JOIN city_realtime_character_case_intake_world_bindings intake ON intake.world_id = agent.world_id AND intake.agent_binding_hash = agent.binding_hash
    JOIN city_realtime_character_case_evidence_world_bindings evidence ON evidence.world_id = agent.world_id AND evidence.agent_binding_hash = agent.binding_hash
    WHERE agent.world_id = NEW.world_id;
    IF NOT FOUND OR NOT city_realtime_agent_core_policy_at_least(expected_policy_id, expected_policy_version, 10)
       OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-evidence assignment binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f', 'city-realtime-character-case-evidence-assignment-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN RAISE EXCEPTION 'city realtime character case-evidence assignment binding hash mismatch' USING ERRCODE = '23514'; END IF;
    RETURN NEW;
END;
$$;

CREATE TABLE IF NOT EXISTS city_realtime_character_case_procedure_dispatch_world_bindings (
    world_id BIGINT PRIMARY KEY REFERENCES city_realtime_character_case_evidence_assignment_world_bindings(world_id) ON DELETE RESTRICT,
    schema_version SMALLINT NOT NULL,
    agent_binding_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_character_case_procedure_dispatch_binding_check CHECK (
        schema_version = 1 AND agent_binding_hash ~ '^[0-9a-f]{64}$' AND binding_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash = encode(sha256(convert_to(concat_ws(E'\x1f', 'city-realtime-character-case-procedure-dispatch-binding-v1', agent_binding_hash), 'UTF8')), 'hex')
        AND metadata = '{}'::jsonb
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_case_procedure_dispatch_heads (
    world_id BIGINT NOT NULL REFERENCES city_realtime_character_case_procedure_dispatch_world_bindings(world_id) ON DELETE RESTRICT,
    reporter_actor_code VARCHAR(96) NOT NULL,
    subject_actor_code VARCHAR(96) NOT NULL,
    assignment_event_sequence BIGINT NOT NULL,
    assignment_link_event_hash VARCHAR(64) NOT NULL,
    dispatch_revision BIGINT NOT NULL,
    dispatch_status VARCHAR(24) NOT NULL,
    queued_frame_sequence BIGINT NOT NULL,
    last_frame_sequence BIGINT NOT NULL,
    event_chain_hash VARCHAR(64) NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, reporter_actor_code, subject_actor_code),
    CONSTRAINT city_realtime_case_procedure_dispatch_link_uq UNIQUE (world_id, assignment_link_event_hash),
    CONSTRAINT city_realtime_case_procedure_dispatch_head_assignment_fk
        FOREIGN KEY (world_id, reporter_actor_code, subject_actor_code)
        REFERENCES city_realtime_character_case_evidence_assignment_heads(world_id, reporter_actor_code, subject_actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_case_procedure_dispatch_head_assignment_event_fk
        FOREIGN KEY (world_id, reporter_actor_code, subject_actor_code, assignment_event_sequence)
        REFERENCES city_realtime_character_case_evidence_assignment_events(world_id, reporter_actor_code, subject_actor_code, event_sequence) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_case_procedure_dispatch_head_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_case_procedure_dispatch_head_check CHECK (
        reporter_actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND subject_actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND reporter_actor_code <> subject_actor_code AND assignment_event_sequence = 1
        AND assignment_link_event_hash ~ '^[0-9a-f]{64}$'
        AND dispatch_revision IN (1,2)
        AND ((dispatch_revision = 1 AND dispatch_status = 'queued')
             OR (dispatch_revision = 2 AND dispatch_status = 'source_window_closed'))
        AND queued_frame_sequence > 0 AND last_frame_sequence >= queued_frame_sequence
        AND ((dispatch_revision = 1 AND last_frame_sequence = queued_frame_sequence)
             OR (dispatch_revision = 2 AND last_frame_sequence > queued_frame_sequence))
        AND event_chain_hash ~ '^[0-9a-f]{64}$' AND state_hash ~ '^[0-9a-f]{64}$'
        AND state_hash = encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-procedure-dispatch-state-v1', reporter_actor_code, subject_actor_code,
            assignment_event_sequence::text, assignment_link_event_hash, dispatch_revision::text, dispatch_status,
            queued_frame_sequence::text, last_frame_sequence::text, event_chain_hash
        ), 'UTF8')), 'hex')
        AND metadata = '{}'::jsonb
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_case_procedure_dispatch_events (
    world_id BIGINT NOT NULL,
    reporter_actor_code VARCHAR(96) NOT NULL,
    subject_actor_code VARCHAR(96) NOT NULL,
    assignment_event_sequence BIGINT NOT NULL,
    assignment_link_event_hash VARCHAR(64) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    event_type VARCHAR(32) NOT NULL,
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, reporter_actor_code, subject_actor_code, event_sequence),
    CONSTRAINT city_realtime_case_procedure_dispatch_event_head_fk
        FOREIGN KEY (world_id, reporter_actor_code, subject_actor_code)
        REFERENCES city_realtime_character_case_procedure_dispatch_heads(world_id, reporter_actor_code, subject_actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_case_procedure_dispatch_event_assignment_fk
        FOREIGN KEY (world_id, reporter_actor_code, subject_actor_code, assignment_event_sequence)
        REFERENCES city_realtime_character_case_evidence_assignment_events(world_id, reporter_actor_code, subject_actor_code, event_sequence) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_case_procedure_dispatch_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_case_procedure_dispatch_event_check CHECK (
        reporter_actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND subject_actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND reporter_actor_code <> subject_actor_code AND assignment_event_sequence = 1
        AND assignment_link_event_hash ~ '^[0-9a-f]{64}$'
        AND event_sequence IN (1,2)
        AND ((event_sequence = 1 AND event_type = 'procedure_queued')
             OR (event_sequence = 2 AND event_type = 'source_window_closed'))
        AND frame_sequence > 0 AND previous_event_hash ~ '^[0-9a-f]{64}$' AND event_hash ~ '^[0-9a-f]{64}$'
        AND metadata = '{}'::jsonb
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_case_procedure_dispatch_heads_reporter
    ON city_realtime_character_case_procedure_dispatch_heads (world_id, reporter_actor_code, last_frame_sequence DESC, subject_actor_code ASC);
CREATE INDEX IF NOT EXISTS idx_city_realtime_character_case_procedure_dispatch_events_frame
    ON city_realtime_character_case_procedure_dispatch_events (world_id, frame_sequence, assignment_link_event_hash);

CREATE OR REPLACE FUNCTION city_realtime_character_case_procedure_dispatch_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_procedure_dispatch_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_evidence_assignment_world_bindings assignment ON assignment.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core' AND agent.policy_version = '1.11.0'
             AND assignment.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_case_procedure_dispatch_mutation_enabled(target_world_id BIGINT, target_frame_sequence BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_procedure_dispatch_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_case_procedure_dispatch_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_evidence_assignment_world_bindings assignment ON assignment.world_id = world.id
           JOIN city_realtime_character_case_procedure_dispatch_world_bindings dispatch ON dispatch.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running' AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core' AND agent.policy_version = '1.11.0'
             AND assignment.agent_binding_hash = agent.binding_hash AND dispatch.agent_binding_hash = agent.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_procedure_dispatch_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE expected_agent_binding_hash VARCHAR(64); expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_procedure_dispatch_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character case-procedure dispatch binding is immutable outside genesis initialization' USING ERRCODE = '55000';
    END IF;
    SELECT agent.binding_hash INTO expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings agent
    JOIN city_realtime_character_case_evidence_assignment_world_bindings assignment
      ON assignment.world_id = agent.world_id AND assignment.agent_binding_hash = agent.binding_hash
    WHERE agent.world_id = NEW.world_id AND agent.policy_id = 'city-realtime-agent-core' AND agent.policy_version = '1.11.0';
    IF NOT FOUND OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-procedure dispatch binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f', 'city-realtime-character-case-procedure-dispatch-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-procedure dispatch binding hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_procedure_dispatch_head()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE assignment_revision BIGINT; assignment_status VARCHAR(24); assignment_assigned_frame BIGINT; assignment_last_frame BIGINT;
DECLARE assignment_link_hash VARCHAR(64); assignment_link_frame BIGINT; assignment_link_type VARCHAR(32);
DECLARE expected_genesis_hash VARCHAR(64); expected_event_hash VARCHAR(64); expected_state_hash VARCHAR(64);
BEGIN
    IF NOT COALESCE(city_realtime_character_case_procedure_dispatch_mutation_enabled(
        NEW.world_id, NULLIF(current_setting('sub2api.city_realtime_character_case_procedure_dispatch_frame_sequence', TRUE), '')::BIGINT
    ), FALSE) THEN
        RAISE EXCEPTION 'city realtime character case-procedure dispatch heads are immutable outside sealed reducers' USING ERRCODE = '55000';
    END IF;
    SELECT assignment.assignment_revision, assignment.assignment_status,
           assignment.assigned_frame_sequence, assignment.last_frame_sequence
    INTO assignment_revision, assignment_status, assignment_assigned_frame, assignment_last_frame
    FROM city_realtime_character_case_evidence_assignment_heads assignment
    WHERE assignment.world_id = NEW.world_id AND assignment.reporter_actor_code = NEW.reporter_actor_code
      AND assignment.subject_actor_code = NEW.subject_actor_code;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city realtime character case-procedure dispatch assignment head is missing' USING ERRCODE = '23514';
    END IF;
    SELECT event.event_hash, event.frame_sequence, event.event_type
    INTO assignment_link_hash, assignment_link_frame, assignment_link_type
    FROM city_realtime_character_case_evidence_assignment_events event
    WHERE event.world_id = NEW.world_id AND event.reporter_actor_code = NEW.reporter_actor_code
      AND event.subject_actor_code = NEW.subject_actor_code AND event.event_sequence = 1;
    IF NOT FOUND OR NEW.assignment_event_sequence <> 1 OR NEW.assignment_link_event_hash <> assignment_link_hash
       OR assignment_link_type <> 'independent_record_linked' THEN
        RAISE EXCEPTION 'city realtime character case-procedure dispatch assignment link is invalid' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'INSERT' THEN
        expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-procedure-dispatch-chain-v1', NEW.reporter_actor_code, NEW.subject_actor_code,
            NEW.assignment_event_sequence::text, NEW.assignment_link_event_hash
        ), 'UTF8')), 'hex');
        expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-procedure-dispatch-event-v1', NEW.reporter_actor_code, NEW.subject_actor_code,
            NEW.assignment_event_sequence::text, NEW.assignment_link_event_hash, '1', NEW.queued_frame_sequence::text,
            'procedure_queued', expected_genesis_hash
        ), 'UTF8')), 'hex');
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-procedure-dispatch-state-v1', NEW.reporter_actor_code, NEW.subject_actor_code,
            NEW.assignment_event_sequence::text, NEW.assignment_link_event_hash, '1', 'queued',
            NEW.queued_frame_sequence::text, NEW.last_frame_sequence::text, NEW.event_chain_hash
        ), 'UTF8')), 'hex');
        IF assignment_revision <> 1 OR assignment_status <> 'linked_active'
           OR assignment_assigned_frame <> assignment_link_frame OR assignment_link_frame <> NEW.queued_frame_sequence
           OR NEW.dispatch_revision <> 1 OR NEW.dispatch_status <> 'queued'
           OR NEW.last_frame_sequence <> NEW.queued_frame_sequence
           OR NEW.event_chain_hash <> expected_event_hash OR NEW.state_hash <> expected_state_hash THEN
            RAISE EXCEPTION 'city realtime character case-procedure dispatch head queue mismatch' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-procedure-dispatch-state-v1', NEW.reporter_actor_code, NEW.subject_actor_code,
            NEW.assignment_event_sequence::text, NEW.assignment_link_event_hash, NEW.dispatch_revision::text, NEW.dispatch_status,
            NEW.queued_frame_sequence::text, NEW.last_frame_sequence::text, NEW.event_chain_hash
        ), 'UTF8')), 'hex');
        IF NEW.reporter_actor_code <> OLD.reporter_actor_code OR NEW.subject_actor_code <> OLD.subject_actor_code
           OR NEW.assignment_event_sequence <> OLD.assignment_event_sequence OR NEW.assignment_link_event_hash <> OLD.assignment_link_event_hash
           OR NEW.queued_frame_sequence <> OLD.queued_frame_sequence OR NEW.dispatch_revision <> OLD.dispatch_revision + 1
           OR NEW.dispatch_revision <> 2 OR NEW.dispatch_status <> 'source_window_closed'
           OR NEW.last_frame_sequence <= OLD.last_frame_sequence
           OR assignment_revision <> 2 OR assignment_status <> 'source_window_closed' OR assignment_last_frame <> NEW.last_frame_sequence
           OR NEW.event_chain_hash = OLD.event_chain_hash OR NEW.state_hash <> expected_state_hash OR NEW.metadata <> OLD.metadata THEN
            RAISE EXCEPTION 'city realtime character case-procedure dispatch head transition is invalid' USING ERRCODE = '23514';
        END IF;
        IF NOT EXISTS (
            SELECT 1 FROM city_realtime_character_case_procedure_dispatch_events event
            WHERE event.world_id = NEW.world_id AND event.reporter_actor_code = NEW.reporter_actor_code
              AND event.subject_actor_code = NEW.subject_actor_code AND event.event_sequence = 2
              AND event.frame_sequence = NEW.last_frame_sequence AND event.event_type = 'source_window_closed'
              AND event.event_hash = NEW.event_chain_hash
        ) THEN
            RAISE EXCEPTION 'city realtime character case-procedure dispatch close event mismatch' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character case-procedure dispatch heads are immutable outside sealed reducers' USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_case_procedure_dispatch_event()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE head_revision BIGINT; head_status VARCHAR(24); head_queued_frame BIGINT; head_chain_hash VARCHAR(64); head_link_hash VARCHAR(64);
DECLARE assignment_revision BIGINT; assignment_status VARCHAR(24); assignment_last_frame BIGINT;
DECLARE assignment_link_hash VARCHAR(64); assignment_link_frame BIGINT; assignment_link_type VARCHAR(32);
DECLARE expected_genesis_hash VARCHAR(64); expected_event_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_case_procedure_dispatch_mutation_enabled(NEW.world_id, NEW.frame_sequence) THEN
        RAISE EXCEPTION 'city realtime character case-procedure dispatch events are append-only sealed facts' USING ERRCODE = '55000';
    END IF;
    SELECT dispatch.dispatch_revision, dispatch.dispatch_status, dispatch.queued_frame_sequence,
           dispatch.event_chain_hash, dispatch.assignment_link_event_hash
    INTO head_revision, head_status, head_queued_frame, head_chain_hash, head_link_hash
    FROM city_realtime_character_case_procedure_dispatch_heads dispatch
    WHERE dispatch.world_id = NEW.world_id AND dispatch.reporter_actor_code = NEW.reporter_actor_code
      AND dispatch.subject_actor_code = NEW.subject_actor_code;
    IF NOT FOUND OR NEW.assignment_event_sequence <> 1 OR NEW.assignment_link_event_hash <> head_link_hash THEN
        RAISE EXCEPTION 'city realtime character case-procedure dispatch event head mismatch' USING ERRCODE = '23514';
    END IF;
    SELECT assignment.assignment_revision, assignment.assignment_status, assignment.last_frame_sequence
    INTO assignment_revision, assignment_status, assignment_last_frame
    FROM city_realtime_character_case_evidence_assignment_heads assignment
    WHERE assignment.world_id = NEW.world_id AND assignment.reporter_actor_code = NEW.reporter_actor_code
      AND assignment.subject_actor_code = NEW.subject_actor_code;
    SELECT event.event_hash, event.frame_sequence, event.event_type
    INTO assignment_link_hash, assignment_link_frame, assignment_link_type
    FROM city_realtime_character_case_evidence_assignment_events event
    WHERE event.world_id = NEW.world_id AND event.reporter_actor_code = NEW.reporter_actor_code
      AND event.subject_actor_code = NEW.subject_actor_code AND event.event_sequence = 1;
    IF NOT FOUND OR assignment_link_hash <> NEW.assignment_link_event_hash
       OR assignment_link_type <> 'independent_record_linked' THEN
        RAISE EXCEPTION 'city realtime character case-procedure dispatch event assignment link is invalid' USING ERRCODE = '23514';
    END IF;
    IF NEW.event_sequence = 1 THEN
        expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-case-procedure-dispatch-chain-v1', NEW.reporter_actor_code, NEW.subject_actor_code,
            NEW.assignment_event_sequence::text, NEW.assignment_link_event_hash
        ), 'UTF8')), 'hex');
        IF head_revision <> 1 OR head_status <> 'queued' OR assignment_revision <> 1 OR assignment_status <> 'linked_active'
           OR assignment_link_frame <> head_queued_frame OR NEW.event_type <> 'procedure_queued'
           OR NEW.frame_sequence <> head_queued_frame OR NEW.previous_event_hash <> expected_genesis_hash THEN
            RAISE EXCEPTION 'city realtime character case-procedure dispatch queue event is invalid' USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.event_sequence = 2 THEN
        IF head_revision <> 1 OR head_status <> 'queued' OR assignment_revision <> 2 OR assignment_status <> 'source_window_closed'
           OR NEW.event_type <> 'source_window_closed' OR NEW.frame_sequence <> assignment_last_frame
           OR NEW.previous_event_hash <> head_chain_hash THEN
            RAISE EXCEPTION 'city realtime character case-procedure dispatch close event is invalid' USING ERRCODE = '23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'city realtime character case-procedure dispatch event sequence is invalid' USING ERRCODE = '23514';
    END IF;
    expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-procedure-dispatch-event-v1', NEW.reporter_actor_code, NEW.subject_actor_code,
        NEW.assignment_event_sequence::text, NEW.assignment_link_event_hash, NEW.event_sequence::text,
        NEW.frame_sequence::text, NEW.event_type, NEW.previous_event_hash
    ), 'UTF8')), 'hex');
    IF NEW.event_hash <> expected_event_hash THEN
        RAISE EXCEPTION 'city realtime character case-procedure dispatch event hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_case_procedure_dispatch_binding_guard ON city_realtime_character_case_procedure_dispatch_world_bindings;
CREATE TRIGGER city_realtime_character_case_procedure_dispatch_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_procedure_dispatch_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_procedure_dispatch_binding();

DROP TRIGGER IF EXISTS city_realtime_character_case_procedure_dispatch_head_guard ON city_realtime_character_case_procedure_dispatch_heads;
CREATE TRIGGER city_realtime_character_case_procedure_dispatch_head_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_procedure_dispatch_heads
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_procedure_dispatch_head();

DROP TRIGGER IF EXISTS city_realtime_character_case_procedure_dispatch_event_guard ON city_realtime_character_case_procedure_dispatch_events;
CREATE TRIGGER city_realtime_character_case_procedure_dispatch_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_case_procedure_dispatch_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_case_procedure_dispatch_event();

COMMENT ON TABLE city_realtime_character_case_procedure_dispatch_world_bindings IS
    'Genesis-pinned 1.11 procedural dispatch adapter; it grants no user, Agent, model, or reviewer routing authority.';
COMMENT ON TABLE city_realtime_character_case_procedure_dispatch_heads IS
    'Bounded server-owned routing receipts anchored only to a sealed 1.10 assignment-link event; never a case, finding, decision, penalty, reward, or asset record.';
COMMENT ON TABLE city_realtime_character_case_procedure_dispatch_events IS
    'Sealed queue/closure lifecycle for a future bounded procedure. Source-window closure never rewrites a report, intake, Law event, Rule, Case, or asset state.';
