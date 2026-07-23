-- A3.3c / bounded realtime Character navigation plans. Policy 1.13.0 adds a
-- single server-derived portal-destination intent. The Agent chooses only a
-- finite published entrance code; the authoritative reducer recomputes one
-- surface step per realtime movement frame. This stores no route cache,
-- arbitrary coordinate, traffic reservation, reward, wallet, provider data,
-- or free-text payload.

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
          "character.navigation.plan",
          "character.portal.traverse",
          "character.role.change",
          "character.social.greet",
          "character.task.accept"
        ],
        "temporal_rule": "decision_then_future_frame",
        "user_control": ["manual", "assisted", "autonomous", "suspended"],
        "personality_revision_schema": "city-realtime-character-personality-v1",
        "action_context_schema": "city-realtime-character-action-context-v6",
        "case_response_schema": "city-realtime-character-case-response-v1",
        "social_response_schema": "city-realtime-character-social-v1",
        "case_review_schema": "city-realtime-character-case-review-v1",
        "case_report_schema": "city-realtime-character-case-report-v1",
        "case_intake_schema": "city-realtime-character-case-intake-v1",
        "case_evidence_schema": "city-realtime-character-case-evidence-v1",
        "case_evidence_assignment_schema": "city-realtime-character-case-evidence-assignment-v1",
        "case_procedure_dispatch_schema": "city-realtime-character-case-procedure-dispatch-v1",
        "task_schema": "city-realtime-character-task-v1",
        "navigation_plan_schema": "city-realtime-character-navigation-plan-v1"
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.13.0', 'published', 1, manifest,
       encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex'), NOW()
FROM core_policy
ON CONFLICT (policy_id, policy_version) DO NOTHING;

-- Preserve every historical action wire shape and add only one exact object
-- form. A decision cannot be replayed as an intent for another actor or an
-- arbitrary world coordinate.
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
        OR (action_code = 'character.navigation.plan' AND jsonb_typeof(arguments) = 'object'
            AND arguments ? 'destination_portal_code' AND jsonb_typeof(arguments -> 'destination_portal_code') = 'string'
            AND (arguments - 'destination_portal_code') = '{}'::jsonb
            AND (arguments ->> 'destination_portal_code') ~ '^[a-z][a-z0-9_.-]{1,127}$')
        OR (action_code = 'character.portal.traverse' AND jsonb_typeof(arguments) = 'object'
            AND arguments ? 'portal_code' AND jsonb_typeof(arguments -> 'portal_code') = 'string'
            AND (arguments - 'portal_code') = '{}'::jsonb
            AND (arguments ->> 'portal_code') ~ '^[a-z][a-z0-9_.-]{1,127}$')
        OR (action_code = 'character.role.change' AND jsonb_typeof(arguments) = 'object'
            AND arguments ? 'role_code' AND jsonb_typeof(arguments -> 'role_code') = 'string'
            AND (arguments - 'role_code') = '{}'::jsonb
            AND (arguments ->> 'role_code') ~ '^[a-z][a-z0-9_.-]{1,95}$')
        OR (action_code = 'character.task.accept' AND jsonb_typeof(arguments) = 'object'
            AND arguments ? 'task_code' AND jsonb_typeof(arguments -> 'task_code') = 'string'
            AND (arguments - 'task_code') = '{}'::jsonb
            AND (arguments ->> 'task_code') IN ('task.civic.cleanup', 'task.civic.shift'))
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
            OR (action_code = 'character.navigation.plan' AND actor_code IS NOT NULL
                AND jsonb_typeof(arguments) = 'object' AND arguments ? 'destination_portal_code'
                AND jsonb_typeof(arguments -> 'destination_portal_code') = 'string'
                AND (arguments - 'destination_portal_code') = '{}'::jsonb
                AND (arguments ->> 'destination_portal_code') ~ '^[a-z][a-z0-9_.-]{1,127}$')
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
            OR (action_code = 'character.task.accept' AND actor_code IS NOT NULL
                AND jsonb_typeof(arguments) = 'object' AND arguments ? 'task_code'
                AND jsonb_typeof(arguments -> 'task_code') = 'string'
                AND (arguments - 'task_code') = '{}'::jsonb
                AND (arguments ->> 'task_code') IN ('task.civic.cleanup', 'task.civic.shift'))
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
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_tasks","realtime_agent_task_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_navigation_plans","realtime_agent_navigation_plan_intents"]'::jsonb
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_navigation_plans' THEN capabilities
    ELSE capabilities || '["realtime_character_navigation_plans","realtime_agent_navigation_plan_intents"]'::jsonb
END
WHERE version = 'city-openworld-realtime-v2';

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
        WHEN '1.12.0' THEN 12 >= minimum_minor
        WHEN '1.13.0' THEN 13 >= minimum_minor
        ELSE FALSE
    END
$$;

-- Policy 1.12 task reducers remain part of the 1.13 character action surface.
-- Redefine their gates here rather than mutating migration 279, so historical
-- 1.12 worlds retain their exact binding while newly-created 1.13 worlds can
-- initialize and later settle the same sealed task ledger.
CREATE OR REPLACE FUNCTION city_realtime_character_task_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_task_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_activity_world_bindings activity ON activity.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 12)
             AND activity.binding_hash ~ '^[0-9a-f]{64}$'
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_task_mutation_enabled(
    target_world_id BIGINT,
    target_frame_sequence BIGINT,
    target_due_world_time_us BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_task_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_task_frame_sequence', TRUE) = target_frame_sequence::text
       AND current_setting('sub2api.city_realtime_character_task_due_world_time_us', TRUE) = target_due_world_time_us::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_activity_world_bindings activity ON activity.world_id = world.id
           JOIN city_realtime_character_task_world_bindings task ON task.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 12)
             AND task.agent_binding_hash = agent.binding_hash
             AND task.activity_binding_hash = activity.binding_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_task_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_agent_binding_hash VARCHAR(64);
    expected_activity_binding_hash VARCHAR(64);
    expected_catalog_hash VARCHAR(64);
    expected_catalog_status VARCHAR(16);
    expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_task_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character task binding is immutable outside genesis initialization'
            USING ERRCODE = '55000';
    END IF;
    SELECT binding_hash INTO expected_agent_binding_hash
    FROM city_realtime_agent_world_bindings
    WHERE world_id = NEW.world_id
      AND city_realtime_agent_core_policy_at_least(policy_id, policy_version, 12);
    SELECT binding_hash INTO expected_activity_binding_hash
    FROM city_realtime_character_activity_world_bindings
    WHERE world_id = NEW.world_id;
    SELECT catalog_hash, status INTO expected_catalog_hash, expected_catalog_status
    FROM city_realtime_character_task_catalogs
    WHERE catalog_id = NEW.catalog_id AND catalog_version = NEW.catalog_version;
    IF NOT FOUND OR expected_catalog_status <> 'published'
       OR expected_agent_binding_hash IS NULL OR expected_activity_binding_hash IS NULL
       OR NEW.schema_version <> 1
       OR NEW.agent_binding_hash <> expected_agent_binding_hash
       OR NEW.activity_binding_hash <> expected_activity_binding_hash
       OR NEW.catalog_hash <> expected_catalog_hash THEN
        RAISE EXCEPTION 'city realtime character task binding references an invalid runtime'
            USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-task-binding-v1', NEW.agent_binding_hash,
        NEW.activity_binding_hash, NEW.catalog_id, NEW.catalog_version, NEW.catalog_hash
    ), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime character task binding hash mismatch'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TABLE IF NOT EXISTS city_realtime_character_navigation_plan_world_bindings (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_realtime_agent_world_bindings(world_id) ON DELETE RESTRICT,
    schema_version INTEGER NOT NULL,
    agent_binding_hash VARCHAR(64) NOT NULL,
    spatial_context_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_character_navigation_plan_binding_spatial_fk
        FOREIGN KEY (world_id) REFERENCES city_realtime_spatial_bindings(world_id) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_navigation_plan_binding_check CHECK (
        schema_version = 1
        AND agent_binding_hash ~ '^[0-9a-f]{64}$'
        AND spatial_context_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash ~ '^[0-9a-f]{64}$'
        AND metadata = '{}'::jsonb
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_navigation_plan_heads (
    world_id BIGINT NOT NULL
        REFERENCES city_realtime_character_navigation_plan_world_bindings(world_id) ON DELETE RESTRICT,
    actor_code VARCHAR(96) NOT NULL,
    navigation_run_code VARCHAR(96) NOT NULL,
    destination_portal_code VARCHAR(128) NOT NULL,
    destination_x BIGINT NOT NULL,
    destination_y BIGINT NOT NULL,
    destination_z SMALLINT NOT NULL,
    source_intent_code VARCHAR(96) NOT NULL,
    plan_revision BIGINT NOT NULL,
    plan_status VARCHAR(16) NOT NULL,
    terminal_reason_code VARCHAR(32) NOT NULL DEFAULT '',
    steps_completed BIGINT NOT NULL,
    maximum_steps BIGINT NOT NULL,
    accepted_frame_sequence BIGINT NOT NULL,
    last_frame_sequence BIGINT NOT NULL,
    last_due_world_time_us BIGINT NOT NULL,
    next_due_world_time_us BIGINT,
    event_chain_hash VARCHAR(64) NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, navigation_run_code),
    CONSTRAINT city_realtime_character_navigation_plan_head_actor_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_navigation_plan_head_intent_fk
        FOREIGN KEY (world_id, source_intent_code)
        REFERENCES city_realtime_agent_intents(world_id, intent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_navigation_plan_head_accepted_frame_fk
        FOREIGN KEY (world_id, accepted_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_navigation_plan_head_last_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_navigation_plan_head_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND navigation_run_code ~ '^navigation[.]run[.][0-9a-f]{64}$'
        AND destination_portal_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND destination_z = 0
        AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND plan_revision >= 1
        AND plan_status IN ('active', 'arrived', 'blocked', 'cancelled')
        AND steps_completed BETWEEN 0 AND 32
        AND maximum_steps = 32
        AND accepted_frame_sequence > 0
        AND last_frame_sequence >= accepted_frame_sequence
        AND last_due_world_time_us >= 0
        AND MOD(last_due_world_time_us, 1000000) = 0
        AND event_chain_hash ~ '^[0-9a-f]{64}$'
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND metadata = '{}'::jsonb
        AND (
            (plan_status = 'active' AND terminal_reason_code = ''
             AND steps_completed < maximum_steps
             AND next_due_world_time_us = last_due_world_time_us + 1000000)
            OR (plan_status = 'arrived' AND terminal_reason_code = 'arrived'
                AND next_due_world_time_us IS NULL)
            OR (plan_status = 'blocked' AND terminal_reason_code IN ('blocked_path', 'blocked_occupied', 'blocked_step_limit')
                AND next_due_world_time_us IS NULL)
            OR (plan_status = 'cancelled' AND terminal_reason_code = 'cancelled_control'
                AND next_due_world_time_us IS NULL)
        )
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_city_realtime_character_navigation_one_active_per_actor
    ON city_realtime_character_navigation_plan_heads (world_id, actor_code)
    WHERE plan_status = 'active';
CREATE INDEX IF NOT EXISTS idx_city_realtime_character_navigation_heads_owner
    ON city_realtime_character_navigation_plan_heads (world_id, actor_code, accepted_frame_sequence DESC, navigation_run_code DESC);

CREATE TABLE IF NOT EXISTS city_realtime_character_navigation_plan_events (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    navigation_run_code VARCHAR(96) NOT NULL,
    destination_portal_code VARCHAR(128) NOT NULL,
    destination_x BIGINT NOT NULL,
    destination_y BIGINT NOT NULL,
    destination_z SMALLINT NOT NULL,
    source_intent_code VARCHAR(96) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    event_type VARCHAR(32) NOT NULL,
    from_x BIGINT NOT NULL,
    from_y BIGINT NOT NULL,
    from_z SMALLINT NOT NULL,
    to_x BIGINT NOT NULL,
    to_y BIGINT NOT NULL,
    to_z SMALLINT NOT NULL,
    steps_completed BIGINT NOT NULL,
    terminal_reason_code VARCHAR(32) NOT NULL DEFAULT '',
    actor_position_event_hash VARCHAR(64) NOT NULL DEFAULT '',
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, navigation_run_code, event_sequence),
    CONSTRAINT city_realtime_character_navigation_plan_event_head_fk
        FOREIGN KEY (world_id, actor_code, navigation_run_code)
        REFERENCES city_realtime_character_navigation_plan_heads(world_id, actor_code, navigation_run_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_navigation_plan_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_navigation_plan_event_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND navigation_run_code ~ '^navigation[.]run[.][0-9a-f]{64}$'
        AND destination_portal_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND destination_z = 0 AND from_z = 0 AND to_z = 0
        AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND event_sequence >= 1 AND frame_sequence > 0
        AND event_type IN ('navigation_planned', 'navigation_step', 'navigation_arrived', 'navigation_blocked', 'navigation_cancelled')
        AND steps_completed BETWEEN 0 AND 32
        AND (actor_position_event_hash = '' OR actor_position_event_hash ~ '^[0-9a-f]{64}$')
        AND previous_event_hash ~ '^[0-9a-f]{64}$'
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND metadata = '{}'::jsonb
        AND (
            (event_type IN ('navigation_planned', 'navigation_step') AND terminal_reason_code = '')
            OR (event_type = 'navigation_arrived' AND terminal_reason_code = 'arrived')
            OR (event_type = 'navigation_blocked' AND terminal_reason_code IN ('blocked_path', 'blocked_occupied', 'blocked_step_limit'))
            OR (event_type = 'navigation_cancelled' AND terminal_reason_code = 'cancelled_control')
        )
    )
);
CREATE INDEX IF NOT EXISTS idx_city_realtime_character_navigation_events_frame
    ON city_realtime_character_navigation_plan_events (world_id, frame_sequence, actor_code, navigation_run_code);

CREATE OR REPLACE FUNCTION city_realtime_character_navigation_plan_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_navigation_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_spatial_bindings spatial ON spatial.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.13.0'
             AND spatial.context_hash ~ '^[0-9a-f]{64}$'
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_navigation_plan_mutation_enabled(
    target_world_id BIGINT,
    target_frame_sequence BIGINT,
    target_due_world_time_us BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_navigation_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_navigation_frame_sequence', TRUE) = target_frame_sequence::text
       AND current_setting('sub2api.city_realtime_character_navigation_due_world_time_us', TRUE) = target_due_world_time_us::text
       AND target_due_world_time_us >= 0
       AND MOD(target_due_world_time_us, 1000000) = 0
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_spatial_bindings spatial ON spatial.world_id = world.id
           JOIN city_realtime_character_navigation_plan_world_bindings navigation ON navigation.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.13.0'
             AND navigation.agent_binding_hash = agent.binding_hash
             AND navigation.spatial_context_hash = spatial.context_hash
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_navigation_plan_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_agent_binding_hash VARCHAR(64);
    expected_spatial_context_hash VARCHAR(64);
    expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_navigation_plan_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character navigation plan binding is immutable outside genesis initialization'
            USING ERRCODE = '55000';
    END IF;
    SELECT agent.binding_hash, spatial.context_hash
    INTO expected_agent_binding_hash, expected_spatial_context_hash
    FROM city_realtime_agent_world_bindings agent
    JOIN city_realtime_spatial_bindings spatial ON spatial.world_id = agent.world_id
    WHERE agent.world_id = NEW.world_id
      AND agent.policy_id = 'city-realtime-agent-core'
      AND agent.policy_version = '1.13.0';
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-navigation-plan-binding-v1',
        expected_agent_binding_hash, expected_spatial_context_hash
    ), 'UTF8')), 'hex');
    IF expected_agent_binding_hash IS NULL OR expected_spatial_context_hash IS NULL
       OR NEW.schema_version <> 1
       OR NEW.agent_binding_hash <> expected_agent_binding_hash
       OR NEW.spatial_context_hash <> expected_spatial_context_hash
       OR NEW.binding_hash <> expected_binding_hash
       OR NEW.metadata <> '{}'::jsonb THEN
        RAISE EXCEPTION 'city realtime character navigation plan binding is invalid'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_navigation_plan_head()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    gate_frame BIGINT := NULLIF(current_setting('sub2api.city_realtime_character_navigation_frame_sequence', TRUE), '')::BIGINT;
    gate_due BIGINT := NULLIF(current_setting('sub2api.city_realtime_character_navigation_due_world_time_us', TRUE), '')::BIGINT;
    intent_actor_code VARCHAR(96);
    intent_action_code VARCHAR(64);
    intent_status VARCHAR(16);
    intent_arguments JSONB;
    intent_scheduled_frame BIGINT;
    actor_x BIGINT;
    actor_y BIGINT;
    actor_z SMALLINT;
    portal_type VARCHAR(16);
    portal_x BIGINT;
    portal_y BIGINT;
    portal_z SMALLINT;
    active_count BIGINT;
    expected_run_code VARCHAR(96);
    expected_genesis_hash VARCHAR(64);
    expected_event_hash VARCHAR(64);
    expected_state_hash VARCHAR(64);
    expected_aggregate_key VARCHAR(160);
    expected_dedup_key VARCHAR(160);
BEGIN
    IF NOT COALESCE(city_realtime_character_navigation_plan_mutation_enabled(NEW.world_id, gate_frame, gate_due), FALSE) THEN
        RAISE EXCEPTION 'city realtime character navigation plan heads are immutable outside sealed reducers'
            USING ERRCODE = '55000';
    END IF;

    IF TG_OP = 'INSERT' THEN
        SELECT actor_code, action_code, status, arguments, scheduled_frame_sequence
        INTO intent_actor_code, intent_action_code, intent_status, intent_arguments, intent_scheduled_frame
        FROM city_realtime_agent_intents
        WHERE world_id = NEW.world_id AND intent_code = NEW.source_intent_code;
        SELECT state.x, state.y, state.z INTO actor_x, actor_y, actor_z
        FROM city_realtime_actor_states state
        WHERE state.world_id = NEW.world_id AND state.actor_code = NEW.actor_code;
        SELECT portal.portal_type, portal.from_x, portal.from_y, portal.from_z
        INTO portal_type, portal_x, portal_y, portal_z
        FROM city_realtime_spatial_portals portal
        WHERE portal.world_id = NEW.world_id AND portal.code = NEW.destination_portal_code;
        SELECT COUNT(*) INTO active_count
        FROM city_realtime_character_navigation_plan_heads
        WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code AND plan_status = 'active';
        expected_run_code := 'navigation.run.' || encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-navigation-plan-run-v1', NEW.actor_code,
            NEW.destination_portal_code, NEW.source_intent_code
        ), 'UTF8')), 'hex');
        expected_aggregate_key := 'navigation.aggregate.' || encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-navigation-plan-aggregate-v1', NEW.actor_code, NEW.navigation_run_code
        ), 'UTF8')), 'hex');
        expected_dedup_key := 'navigation.step.' || encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-navigation-plan-dedup-v1', NEW.actor_code, NEW.navigation_run_code,
            '1', NEW.next_due_world_time_us::text
        ), 'UTF8')), 'hex');
        expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-navigation-plan-chain-v1', NEW.actor_code, NEW.navigation_run_code,
            NEW.destination_portal_code, NEW.destination_x::text, NEW.destination_y::text,
            NEW.destination_z::text, NEW.source_intent_code, NEW.accepted_frame_sequence::text
        ), 'UTF8')), 'hex');
        expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-navigation-plan-event-v1', NEW.actor_code, NEW.navigation_run_code,
            NEW.destination_portal_code, NEW.destination_x::text, NEW.destination_y::text,
            NEW.destination_z::text, NEW.source_intent_code, '1', NEW.accepted_frame_sequence::text,
            'navigation_planned', actor_x::text, actor_y::text, actor_z::text,
            actor_x::text, actor_y::text, actor_z::text, '0', '', '', expected_genesis_hash
        ), 'UTF8')), 'hex');
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-navigation-plan-state-v1', NEW.actor_code, NEW.navigation_run_code,
            NEW.destination_portal_code, NEW.destination_x::text, NEW.destination_y::text,
            NEW.destination_z::text, NEW.source_intent_code, '1', 'active', '', '0', '32',
            NEW.accepted_frame_sequence::text, NEW.last_frame_sequence::text,
            NEW.last_due_world_time_us::text, NEW.next_due_world_time_us::text, expected_event_hash
        ), 'UTF8')), 'hex');
        IF intent_actor_code IS DISTINCT FROM NEW.actor_code
           OR intent_action_code IS DISTINCT FROM 'character.navigation.plan'
           OR intent_status IS DISTINCT FROM 'pending'
           OR intent_arguments IS DISTINCT FROM jsonb_build_object('destination_portal_code', NEW.destination_portal_code)
           OR intent_scheduled_frame >= NEW.accepted_frame_sequence
           OR actor_z IS DISTINCT FROM 0
           OR portal_type IS DISTINCT FROM 'entrance'
           OR portal_x IS DISTINCT FROM NEW.destination_x OR portal_y IS DISTINCT FROM NEW.destination_y
           OR portal_z IS DISTINCT FROM NEW.destination_z
           OR active_count <> 0
           OR NEW.navigation_run_code <> expected_run_code
           OR NEW.plan_revision <> 1 OR NEW.plan_status <> 'active' OR NEW.terminal_reason_code <> ''
           OR NEW.steps_completed <> 0 OR NEW.maximum_steps <> 32
           OR NEW.accepted_frame_sequence <> gate_frame OR NEW.last_frame_sequence <> gate_frame
           OR NEW.last_due_world_time_us <> gate_due
           OR NEW.next_due_world_time_us <> gate_due + 1000000
           OR NEW.event_chain_hash <> expected_event_hash OR NEW.state_hash <> expected_state_hash
           OR NEW.metadata <> '{}'::jsonb
           OR NOT EXISTS (
               SELECT 1 FROM city_due_events due
               WHERE due.world_id = NEW.world_id
                 AND due.event_type = 'system.realtime.character_navigation_step'
                 AND due.schema_version = 1
                 AND due.due_world_time_us = NEW.next_due_world_time_us
                 AND due.temporal_phase = 'movement'
                 AND due.priority = 90
                 AND due.aggregate_type = 'realtime_character_navigation'
                 AND due.aggregate_key = expected_aggregate_key
                 AND due.dedup_key = expected_dedup_key
                 AND due.source_kind = 'system'
                 AND due.source_reference = 'realtime_character_navigation_plan'
                 AND due.expected_version = 1
                 AND due.status = 'pending'
                 AND due.created_frame_sequence = NEW.accepted_frame_sequence
                 AND due.payload = jsonb_build_object(
                     'schema_version', 1, 'actor_code', NEW.actor_code,
                     'navigation_run_code', NEW.navigation_run_code,
                     'destination_portal_code', NEW.destination_portal_code,
                     'plan_revision', 1
                 )
           ) THEN
            RAISE EXCEPTION 'city realtime character navigation plan acceptance is not a sealed pending intent with one movement boundary'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'UPDATE' THEN
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-navigation-plan-state-v1', NEW.actor_code, NEW.navigation_run_code,
            NEW.destination_portal_code, NEW.destination_x::text, NEW.destination_y::text,
            NEW.destination_z::text, NEW.source_intent_code, NEW.plan_revision::text,
            NEW.plan_status, NEW.terminal_reason_code, NEW.steps_completed::text,
            NEW.maximum_steps::text, NEW.accepted_frame_sequence::text, NEW.last_frame_sequence::text,
            NEW.last_due_world_time_us::text, COALESCE(NEW.next_due_world_time_us::text, ''), NEW.event_chain_hash
        ), 'UTF8')), 'hex');
        IF NEW.actor_code <> OLD.actor_code OR NEW.navigation_run_code <> OLD.navigation_run_code
           OR NEW.destination_portal_code <> OLD.destination_portal_code
           OR NEW.destination_x <> OLD.destination_x OR NEW.destination_y <> OLD.destination_y
           OR NEW.destination_z <> OLD.destination_z OR NEW.source_intent_code <> OLD.source_intent_code
           OR NEW.maximum_steps <> OLD.maximum_steps OR NEW.accepted_frame_sequence <> OLD.accepted_frame_sequence
           OR NEW.plan_revision <> OLD.plan_revision + 1
           OR NEW.last_frame_sequence <> gate_frame OR NEW.last_frame_sequence <= OLD.last_frame_sequence
           OR NEW.last_due_world_time_us <> gate_due OR NEW.last_due_world_time_us < OLD.last_due_world_time_us
           OR NEW.event_chain_hash = OLD.event_chain_hash OR NEW.state_hash <> expected_state_hash
           OR NEW.metadata <> OLD.metadata THEN
            RAISE EXCEPTION 'city realtime character navigation plan head transition is invalid'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.plan_status = 'active' THEN
            expected_aggregate_key := 'navigation.aggregate.' || encode(sha256(convert_to(concat_ws(E'\x1f',
                'city-realtime-character-navigation-plan-aggregate-v1', NEW.actor_code, NEW.navigation_run_code
            ), 'UTF8')), 'hex');
            expected_dedup_key := 'navigation.step.' || encode(sha256(convert_to(concat_ws(E'\x1f',
                'city-realtime-character-navigation-plan-dedup-v1', NEW.actor_code, NEW.navigation_run_code,
                NEW.plan_revision::text, NEW.next_due_world_time_us::text
            ), 'UTF8')), 'hex');
            IF OLD.plan_status <> 'active' OR gate_due <> OLD.next_due_world_time_us
               OR NEW.terminal_reason_code <> '' OR NEW.steps_completed <> OLD.steps_completed + 1
               OR NEW.next_due_world_time_us <> gate_due + 1000000
               OR NOT EXISTS (
                   SELECT 1 FROM city_due_events due
                   WHERE due.world_id = NEW.world_id
                     AND due.event_type = 'system.realtime.character_navigation_step'
                     AND due.schema_version = 1 AND due.due_world_time_us = NEW.next_due_world_time_us
                     AND due.temporal_phase = 'movement' AND due.priority = 90
                     AND due.aggregate_type = 'realtime_character_navigation'
                     AND due.aggregate_key = expected_aggregate_key AND due.dedup_key = expected_dedup_key
                     AND due.source_kind = 'system' AND due.source_reference = 'realtime_character_navigation_plan'
                     AND due.expected_version = NEW.plan_revision AND due.status = 'pending'
                     AND due.created_frame_sequence = NEW.last_frame_sequence
                     AND due.payload = jsonb_build_object(
                         'schema_version', 1, 'actor_code', NEW.actor_code,
                         'navigation_run_code', NEW.navigation_run_code,
                         'destination_portal_code', NEW.destination_portal_code,
                         'plan_revision', NEW.plan_revision
                     )
               ) THEN
                RAISE EXCEPTION 'city realtime character navigation active transition lacks its next movement boundary'
                    USING ERRCODE = '23514';
            END IF;
        ELSIF NEW.plan_status IN ('arrived', 'blocked', 'cancelled') THEN
            IF OLD.plan_status <> 'active' OR NEW.next_due_world_time_us IS NOT NULL
               OR (NEW.plan_status <> 'cancelled' AND gate_due <> OLD.next_due_world_time_us)
               OR (NEW.plan_status = 'cancelled' AND NEW.steps_completed <> OLD.steps_completed)
               OR (NEW.plan_status = 'arrived' AND NEW.steps_completed NOT IN (OLD.steps_completed, OLD.steps_completed + 1))
               OR (NEW.plan_status = 'blocked' AND NEW.steps_completed NOT IN (OLD.steps_completed, OLD.steps_completed + 1)) THEN
                RAISE EXCEPTION 'city realtime character navigation terminal transition is invalid'
                    USING ERRCODE = '23514';
            END IF;
        ELSE
            RAISE EXCEPTION 'city realtime character navigation plan status is invalid'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'city realtime character navigation plan heads are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_navigation_plan_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    gate_frame BIGINT := NULLIF(current_setting('sub2api.city_realtime_character_navigation_frame_sequence', TRUE), '')::BIGINT;
    gate_due BIGINT := NULLIF(current_setting('sub2api.city_realtime_character_navigation_due_world_time_us', TRUE), '')::BIGINT;
    head_revision BIGINT;
    head_status VARCHAR(16);
    head_reason VARCHAR(32);
    head_steps BIGINT;
    head_last_frame BIGINT;
    head_chain_hash VARCHAR(64);
    head_destination_portal VARCHAR(128);
    head_destination_x BIGINT;
    head_destination_y BIGINT;
    head_destination_z SMALLINT;
    head_source_intent VARCHAR(96);
    intent_actor_code VARCHAR(96);
    intent_action_code VARCHAR(64);
    intent_status VARCHAR(16);
    intent_arguments JSONB;
    actor_x BIGINT;
    actor_y BIGINT;
    actor_z SMALLINT;
    expected_genesis_hash VARCHAR(64);
    expected_event_hash VARCHAR(64);
    previous_hash VARCHAR(64);
    previous_steps BIGINT;
    previous_to_x BIGINT;
    previous_to_y BIGINT;
    previous_to_z SMALLINT;
BEGIN
    IF TG_OP <> 'INSERT'
       OR NOT COALESCE(city_realtime_character_navigation_plan_mutation_enabled(NEW.world_id, gate_frame, gate_due), FALSE) THEN
        RAISE EXCEPTION 'city realtime character navigation plan events are append-only sealed facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT plan_revision, plan_status, terminal_reason_code, steps_completed,
           last_frame_sequence, event_chain_hash, destination_portal_code,
           destination_x, destination_y, destination_z, source_intent_code
    INTO head_revision, head_status, head_reason, head_steps,
         head_last_frame, head_chain_hash, head_destination_portal,
         head_destination_x, head_destination_y, head_destination_z, head_source_intent
    FROM city_realtime_character_navigation_plan_heads
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
      AND navigation_run_code = NEW.navigation_run_code;
    IF NOT FOUND
       OR NEW.frame_sequence <> gate_frame OR NEW.frame_sequence <> head_last_frame
       OR NEW.destination_portal_code <> head_destination_portal
       OR NEW.destination_x <> head_destination_x OR NEW.destination_y <> head_destination_y
       OR NEW.destination_z <> head_destination_z OR NEW.source_intent_code <> head_source_intent
       OR NEW.event_sequence <> head_revision OR NEW.steps_completed <> head_steps
       OR NEW.event_hash <> head_chain_hash THEN
        RAISE EXCEPTION 'city realtime character navigation plan event head mismatch'
            USING ERRCODE = '23514';
    END IF;
    expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-navigation-plan-event-v1', NEW.actor_code, NEW.navigation_run_code,
        NEW.destination_portal_code, NEW.destination_x::text, NEW.destination_y::text,
        NEW.destination_z::text, NEW.source_intent_code, NEW.event_sequence::text,
        NEW.frame_sequence::text, NEW.event_type, NEW.from_x::text, NEW.from_y::text,
        NEW.from_z::text, NEW.to_x::text, NEW.to_y::text, NEW.to_z::text,
        NEW.steps_completed::text, NEW.terminal_reason_code,
        NEW.actor_position_event_hash, NEW.previous_event_hash
    ), 'UTF8')), 'hex');
    IF NEW.event_hash <> expected_event_hash THEN
        RAISE EXCEPTION 'city realtime character navigation plan event hash mismatch'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.event_sequence = 1 THEN
        SELECT actor_code, action_code, status, arguments
        INTO intent_actor_code, intent_action_code, intent_status, intent_arguments
        FROM city_realtime_agent_intents
        WHERE world_id = NEW.world_id AND intent_code = NEW.source_intent_code;
        SELECT x, y, z INTO actor_x, actor_y, actor_z
        FROM city_realtime_actor_states
        WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code;
        expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-navigation-plan-chain-v1', NEW.actor_code, NEW.navigation_run_code,
            NEW.destination_portal_code, NEW.destination_x::text, NEW.destination_y::text,
            NEW.destination_z::text, NEW.source_intent_code, NEW.frame_sequence::text
        ), 'UTF8')), 'hex');
        IF head_status <> 'active' OR NEW.event_type <> 'navigation_planned'
           OR NEW.terminal_reason_code <> '' OR NEW.steps_completed <> 0
           OR NEW.actor_position_event_hash <> '' OR NEW.previous_event_hash <> expected_genesis_hash
           OR NEW.from_x <> actor_x OR NEW.from_y <> actor_y OR NEW.from_z <> actor_z
           OR NEW.to_x <> actor_x OR NEW.to_y <> actor_y OR NEW.to_z <> actor_z
           OR intent_actor_code IS DISTINCT FROM NEW.actor_code
           OR intent_action_code IS DISTINCT FROM 'character.navigation.plan'
           OR intent_status IS DISTINCT FROM 'pending'
           OR intent_arguments IS DISTINCT FROM jsonb_build_object('destination_portal_code', NEW.destination_portal_code) THEN
            RAISE EXCEPTION 'city realtime character navigation plan genesis event is invalid'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    SELECT event_hash, steps_completed, to_x, to_y, to_z
    INTO previous_hash, previous_steps, previous_to_x, previous_to_y, previous_to_z
    FROM city_realtime_character_navigation_plan_events
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
      AND navigation_run_code = NEW.navigation_run_code AND event_sequence = NEW.event_sequence - 1;
    IF previous_hash IS DISTINCT FROM NEW.previous_event_hash
       OR NEW.from_x IS DISTINCT FROM previous_to_x
       OR NEW.from_y IS DISTINCT FROM previous_to_y
       OR NEW.from_z IS DISTINCT FROM previous_to_z THEN
        RAISE EXCEPTION 'city realtime character navigation plan event chain or position continuity is invalid'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.event_type = 'navigation_step' THEN
        IF head_status <> 'active' OR NEW.terminal_reason_code <> '' OR NEW.actor_position_event_hash = ''
           OR NEW.steps_completed <> previous_steps + 1
           OR NOT EXISTS (
               SELECT 1 FROM city_realtime_actor_position_events position_event
               WHERE position_event.world_id = NEW.world_id AND position_event.actor_code = NEW.actor_code
                 AND position_event.frame_sequence = NEW.frame_sequence AND position_event.event_kind = 'move'
                 AND position_event.from_x = NEW.from_x AND position_event.from_y = NEW.from_y AND position_event.from_z = NEW.from_z
                 AND position_event.to_x = NEW.to_x AND position_event.to_y = NEW.to_y AND position_event.to_z = NEW.to_z
                 AND position_event.event_hash = NEW.actor_position_event_hash
           ) THEN
            RAISE EXCEPTION 'city realtime character navigation step lacks its exact position event'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.event_type = 'navigation_arrived' THEN
        IF head_status <> 'arrived' OR head_reason <> 'arrived' OR NEW.terminal_reason_code <> 'arrived'
           OR NEW.to_x <> NEW.destination_x OR NEW.to_y <> NEW.destination_y OR NEW.to_z <> NEW.destination_z
           OR (NEW.actor_position_event_hash = '' AND (
                NEW.from_x <> NEW.to_x OR NEW.from_y <> NEW.to_y OR NEW.from_z <> NEW.to_z
                OR NEW.steps_completed <> previous_steps
           ))
           OR (NEW.actor_position_event_hash <> '' AND (
                NEW.steps_completed <> previous_steps + 1
                OR NOT EXISTS (
                    SELECT 1 FROM city_realtime_actor_position_events position_event
                    WHERE position_event.world_id = NEW.world_id AND position_event.actor_code = NEW.actor_code
                      AND position_event.frame_sequence = NEW.frame_sequence AND position_event.event_kind = 'move'
                      AND position_event.from_x = NEW.from_x AND position_event.from_y = NEW.from_y AND position_event.from_z = NEW.from_z
                      AND position_event.to_x = NEW.to_x AND position_event.to_y = NEW.to_y AND position_event.to_z = NEW.to_z
                      AND position_event.event_hash = NEW.actor_position_event_hash
                )
           )) THEN
            RAISE EXCEPTION 'city realtime character navigation arrival event is invalid'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.event_type = 'navigation_blocked' THEN
        IF head_status <> 'blocked' OR head_reason <> NEW.terminal_reason_code
           OR (NEW.terminal_reason_code IN ('blocked_path', 'blocked_occupied') AND (
                NEW.actor_position_event_hash <> '' OR NEW.steps_completed <> previous_steps
                OR NEW.from_x <> NEW.to_x OR NEW.from_y <> NEW.to_y OR NEW.from_z <> NEW.to_z
           ))
           OR (NEW.terminal_reason_code = 'blocked_step_limit' AND (
                NEW.actor_position_event_hash = '' OR NEW.steps_completed <> previous_steps + 1
                OR NOT EXISTS (
                    SELECT 1 FROM city_realtime_actor_position_events position_event
                    WHERE position_event.world_id = NEW.world_id AND position_event.actor_code = NEW.actor_code
                      AND position_event.frame_sequence = NEW.frame_sequence AND position_event.event_kind = 'move'
                      AND position_event.from_x = NEW.from_x AND position_event.from_y = NEW.from_y AND position_event.from_z = NEW.from_z
                      AND position_event.to_x = NEW.to_x AND position_event.to_y = NEW.to_y AND position_event.to_z = NEW.to_z
                      AND position_event.event_hash = NEW.actor_position_event_hash
                )
           )) THEN
            RAISE EXCEPTION 'city realtime character navigation blocked event is invalid'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.event_type = 'navigation_cancelled' THEN
        IF head_status <> 'cancelled' OR head_reason <> 'cancelled_control'
           OR NEW.terminal_reason_code <> 'cancelled_control' OR NEW.actor_position_event_hash <> ''
           OR NEW.steps_completed <> previous_steps
           OR NEW.from_x <> NEW.to_x OR NEW.from_y <> NEW.to_y OR NEW.from_z <> NEW.to_z THEN
            RAISE EXCEPTION 'city realtime character navigation cancellation event is invalid'
                USING ERRCODE = '23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'city realtime character navigation plan event type is invalid'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_realtime_character_navigation_plan_head_facts()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    event_count BIGINT;
    terminal_event_type VARCHAR(32);
    terminal_event_hash VARCHAR(64);
    terminal_event_frame BIGINT;
BEGIN
    SELECT COUNT(*) INTO event_count
    FROM city_realtime_character_navigation_plan_events
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
      AND navigation_run_code = NEW.navigation_run_code;
    IF event_count <> NEW.plan_revision OR NOT EXISTS (
        SELECT 1 FROM city_realtime_character_navigation_plan_events event
        WHERE event.world_id = NEW.world_id AND event.actor_code = NEW.actor_code
          AND event.navigation_run_code = NEW.navigation_run_code
          AND event.event_sequence = NEW.plan_revision
          AND event.event_hash = NEW.event_chain_hash
          AND event.frame_sequence = NEW.last_frame_sequence
    ) THEN
        RAISE EXCEPTION 'city realtime character navigation head lacks its sealed event chain'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.plan_status = 'active' THEN
        IF NOT EXISTS (
            SELECT 1 FROM city_realtime_character_navigation_plan_events event
            WHERE event.world_id = NEW.world_id AND event.actor_code = NEW.actor_code
              AND event.navigation_run_code = NEW.navigation_run_code
              AND event.event_sequence = NEW.plan_revision
              AND event.event_type IN ('navigation_planned', 'navigation_step')
        ) THEN
            RAISE EXCEPTION 'city realtime character navigation active head lacks a nonterminal event'
                USING ERRCODE = '23514';
        END IF;
        RETURN NULL;
    END IF;
    SELECT event_type, event_hash, frame_sequence
    INTO terminal_event_type, terminal_event_hash, terminal_event_frame
    FROM city_realtime_character_navigation_plan_events
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
      AND navigation_run_code = NEW.navigation_run_code AND event_sequence = NEW.plan_revision;
    IF terminal_event_type IS DISTINCT FROM (
            CASE NEW.plan_status
                WHEN 'arrived' THEN 'navigation_arrived'
                WHEN 'blocked' THEN 'navigation_blocked'
                WHEN 'cancelled' THEN 'navigation_cancelled'
            END
       )
       OR terminal_event_hash IS DISTINCT FROM NEW.event_chain_hash
       OR terminal_event_frame IS DISTINCT FROM NEW.last_frame_sequence THEN
        RAISE EXCEPTION 'city realtime character navigation terminal head lacks its sealed event'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_navigation_plan_binding_guard
    ON city_realtime_character_navigation_plan_world_bindings;
CREATE TRIGGER city_realtime_character_navigation_plan_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_navigation_plan_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_navigation_plan_binding();

DROP TRIGGER IF EXISTS city_realtime_character_navigation_plan_head_guard
    ON city_realtime_character_navigation_plan_heads;
CREATE TRIGGER city_realtime_character_navigation_plan_head_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_navigation_plan_heads
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_navigation_plan_head();

DROP TRIGGER IF EXISTS city_realtime_character_navigation_plan_event_guard
    ON city_realtime_character_navigation_plan_events;
CREATE TRIGGER city_realtime_character_navigation_plan_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_navigation_plan_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_navigation_plan_event();

DROP TRIGGER IF EXISTS city_realtime_character_navigation_plan_head_fact_guard
    ON city_realtime_character_navigation_plan_heads;
CREATE CONSTRAINT TRIGGER city_realtime_character_navigation_plan_head_fact_guard
AFTER INSERT OR UPDATE ON city_realtime_character_navigation_plan_heads
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_realtime_character_navigation_plan_head_facts();

COMMENT ON TABLE city_realtime_character_navigation_plan_heads IS
    'A3.3c owner-private finite route plans. Every movement is recomputed and sealed by a realtime movement frame.';
COMMENT ON TABLE city_realtime_character_navigation_plan_events IS
    'A3.3c append-only navigation facts. The ledger stores no route cache, traffic reservation, provider data, wallet, reward, or free text.';
