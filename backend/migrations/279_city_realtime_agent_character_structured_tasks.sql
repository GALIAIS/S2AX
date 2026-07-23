-- A3.3b / structured Character tasks. Policy 1.12.0 adds one bounded
-- acceptance intent whose completion is provable only from an already-sealed
-- Agent activity event. Tasks introduce no task-specific reward, wallet,
-- case, rule, inventory, provider, or free-text surface.

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
          "character.social.greet",
          "character.task.accept"
        ],
        "temporal_rule": "decision_then_future_frame",
        "user_control": ["manual", "assisted", "autonomous", "suspended"],
        "personality_revision_schema": "city-realtime-character-personality-v1",
        "action_context_schema": "city-realtime-character-action-context-v5",
        "case_response_schema": "city-realtime-character-case-response-v1",
        "social_response_schema": "city-realtime-character-social-v1",
        "case_review_schema": "city-realtime-character-case-review-v1",
        "case_report_schema": "city-realtime-character-case-report-v1",
        "case_intake_schema": "city-realtime-character-case-intake-v1",
        "case_evidence_schema": "city-realtime-character-case-evidence-v1",
        "case_evidence_assignment_schema": "city-realtime-character-case-evidence-assignment-v1",
        "case_procedure_dispatch_schema": "city-realtime-character-case-procedure-dispatch-v1",
        "task_schema": "city-realtime-character-task-v1"
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.12.0', 'published', 1, manifest,
       encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex'), NOW()
FROM core_policy
ON CONFLICT (policy_id, policy_version) DO NOTHING;

-- 1.12 introduces one new Agent action. The decision and intent records are
-- independently constrained because a sealed decision cannot be replayed as a
-- different future intent. Keep every historical action shape unchanged and
-- admit only the finite task catalog published in this migration.
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
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_tasks' THEN capabilities
    ELSE capabilities || '["realtime_character_tasks","realtime_agent_task_intents"]'::jsonb
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
        ELSE FALSE
    END
$$;

-- Procedure dispatch is a pinned 1.11 adapter, but its immutable receipt
-- remains a prerequisite for a new 1.12 world. Preserve 1.11 worlds while
-- admitting the later policy only through the explicit compatibility helper.
CREATE OR REPLACE FUNCTION city_realtime_character_case_procedure_dispatch_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT current_setting('sub2api.city_realtime_character_case_procedure_dispatch_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1 FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_character_case_evidence_assignment_world_bindings assignment ON assignment.world_id = world.id
           WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 11)
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
             AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 11)
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
    WHERE agent.world_id = NEW.world_id
      AND city_realtime_agent_core_policy_at_least(agent.policy_id, agent.policy_version, 11);
    IF NOT FOUND OR NEW.schema_version <> 1 OR NEW.agent_binding_hash <> expected_agent_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-procedure dispatch binding references an invalid agent policy' USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-case-procedure-dispatch-binding-v1', NEW.agent_binding_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime character case-procedure dispatch binding hash mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TABLE IF NOT EXISTS city_realtime_character_task_catalogs (
    catalog_id VARCHAR(96) NOT NULL,
    catalog_version VARCHAR(24) NOT NULL,
    status VARCHAR(16) NOT NULL,
    catalog_schema_version INTEGER NOT NULL,
    manifest JSONB NOT NULL,
    catalog_hash VARCHAR(64) NOT NULL,
    published_at TIMESTAMPTZ NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (catalog_id, catalog_version),
    CONSTRAINT city_realtime_character_task_catalog_check CHECK (
        catalog_id = 'city-realtime-character-task-core'
        AND catalog_version = '1.0.0'
        AND status = 'published'
        AND catalog_schema_version = 1
        AND catalog_hash ~ '^[0-9a-f]{64}$'
        AND metadata = '{}'::jsonb
    )
);

CREATE OR REPLACE FUNCTION guard_city_realtime_character_task_catalog()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE expected_manifest JSONB;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'city realtime character task catalogs are immutable versions'
            USING ERRCODE = '55000';
    END IF;
    expected_manifest := jsonb_build_object(
        'schema_version', 1,
        'completion_contract', 'sealed_agent_activity_event',
        'tasks', jsonb_build_array(
            jsonb_build_object(
                'code', 'task.civic.cleanup',
                'activity_code', 'civic.cleanup',
                'expiration_delay_us', 300000000
            ),
            jsonb_build_object(
                'code', 'task.civic.shift',
                'activity_code', 'work.civic_shift',
                'expiration_delay_us', 300000000
            )
        )
    );
    IF NEW.manifest <> expected_manifest
       OR NEW.catalog_hash <> encode(sha256(convert_to(NEW.manifest::text, 'UTF8')), 'hex') THEN
        RAISE EXCEPTION 'city realtime character task catalog manifest or hash is invalid'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_task_catalog_guard
    ON city_realtime_character_task_catalogs;
CREATE TRIGGER city_realtime_character_task_catalog_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_task_catalogs
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_task_catalog();

WITH task_catalog AS (
    SELECT '{
      "schema_version": 1,
      "completion_contract": "sealed_agent_activity_event",
      "tasks": [
        {"code":"task.civic.cleanup","activity_code":"civic.cleanup","expiration_delay_us":300000000},
        {"code":"task.civic.shift","activity_code":"work.civic_shift","expiration_delay_us":300000000}
      ]
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_character_task_catalogs
    (catalog_id, catalog_version, status, catalog_schema_version, manifest, catalog_hash, published_at)
SELECT 'city-realtime-character-task-core', '1.0.0', 'published', 1, manifest,
       encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex'), NOW()
FROM task_catalog
ON CONFLICT (catalog_id, catalog_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_realtime_character_task_world_bindings (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_realtime_agent_world_bindings(world_id) ON DELETE RESTRICT,
    schema_version INTEGER NOT NULL,
    agent_binding_hash VARCHAR(64) NOT NULL,
    activity_binding_hash VARCHAR(64) NOT NULL,
    catalog_id VARCHAR(96) NOT NULL,
    catalog_version VARCHAR(24) NOT NULL,
    catalog_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_character_task_binding_activity_fk
        FOREIGN KEY (world_id)
        REFERENCES city_realtime_character_activity_world_bindings(world_id) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_task_binding_catalog_fk
        FOREIGN KEY (catalog_id, catalog_version)
        REFERENCES city_realtime_character_task_catalogs(catalog_id, catalog_version) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_task_binding_check CHECK (
        schema_version = 1
        AND agent_binding_hash ~ '^[0-9a-f]{64}$'
        AND activity_binding_hash ~ '^[0-9a-f]{64}$'
        AND catalog_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash ~ '^[0-9a-f]{64}$'
        AND metadata = '{}'::jsonb
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_task_heads (
    world_id BIGINT NOT NULL
        REFERENCES city_realtime_character_task_world_bindings(world_id) ON DELETE RESTRICT,
    actor_code VARCHAR(96) NOT NULL,
    task_run_code VARCHAR(96) NOT NULL,
    task_code VARCHAR(64) NOT NULL,
    activity_code VARCHAR(64) NOT NULL,
    source_intent_code VARCHAR(96) NOT NULL,
    task_revision BIGINT NOT NULL,
    task_status VARCHAR(16) NOT NULL,
    accepted_frame_sequence BIGINT NOT NULL,
    expiration_due_world_time_us BIGINT NOT NULL,
    completion_activity_event_sequence BIGINT,
    completion_activity_event_hash VARCHAR(64),
    last_frame_sequence BIGINT NOT NULL,
    event_chain_hash VARCHAR(64) NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, task_run_code),
    CONSTRAINT city_realtime_character_task_head_actor_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_task_head_intent_fk
        FOREIGN KEY (world_id, source_intent_code)
        REFERENCES city_realtime_agent_intents(world_id, intent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_task_head_accepted_frame_fk
        FOREIGN KEY (world_id, accepted_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_task_head_last_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_task_head_completion_activity_fk
        FOREIGN KEY (world_id, actor_code, completion_activity_event_sequence)
        REFERENCES city_realtime_character_activity_events(world_id, actor_code, event_sequence)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_task_head_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND task_run_code ~ '^task[.]run[.][0-9a-f]{64}$'
        AND task_code IN ('task.civic.cleanup', 'task.civic.shift')
        AND ((task_code = 'task.civic.cleanup' AND activity_code = 'civic.cleanup')
             OR (task_code = 'task.civic.shift' AND activity_code = 'work.civic_shift'))
        AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND task_revision IN (1, 2)
        AND task_status IN ('accepted', 'completed', 'expired')
        AND accepted_frame_sequence > 0
        AND expiration_due_world_time_us > 0
        AND MOD(expiration_due_world_time_us, 1000000) = 0
        AND last_frame_sequence >= accepted_frame_sequence
        AND event_chain_hash ~ '^[0-9a-f]{64}$'
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND metadata = '{}'::jsonb
        AND (
            (task_revision = 1 AND task_status = 'accepted'
             AND last_frame_sequence = accepted_frame_sequence
             AND completion_activity_event_sequence IS NULL AND completion_activity_event_hash IS NULL)
            OR
            (task_revision = 2 AND task_status = 'completed'
             AND last_frame_sequence > accepted_frame_sequence
             AND completion_activity_event_sequence IS NOT NULL
             AND completion_activity_event_hash ~ '^[0-9a-f]{64}$')
            OR
            (task_revision = 2 AND task_status = 'expired'
             AND last_frame_sequence > accepted_frame_sequence
             AND completion_activity_event_sequence IS NULL AND completion_activity_event_hash IS NULL)
        )
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_city_realtime_character_task_one_active_per_actor
    ON city_realtime_character_task_heads (world_id, actor_code)
    WHERE task_status = 'accepted';

CREATE TABLE IF NOT EXISTS city_realtime_character_task_events (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    task_run_code VARCHAR(96) NOT NULL,
    task_code VARCHAR(64) NOT NULL,
    activity_code VARCHAR(64) NOT NULL,
    source_intent_code VARCHAR(96) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    event_type VARCHAR(24) NOT NULL,
    expiration_due_world_time_us BIGINT NOT NULL,
    completion_activity_event_sequence BIGINT,
    completion_activity_event_hash VARCHAR(64),
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, task_run_code, event_sequence),
    CONSTRAINT city_realtime_character_task_event_head_fk
        FOREIGN KEY (world_id, actor_code, task_run_code)
        REFERENCES city_realtime_character_task_heads(world_id, actor_code, task_run_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_task_event_intent_fk
        FOREIGN KEY (world_id, source_intent_code)
        REFERENCES city_realtime_agent_intents(world_id, intent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_task_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_task_event_completion_activity_fk
        FOREIGN KEY (world_id, actor_code, completion_activity_event_sequence)
        REFERENCES city_realtime_character_activity_events(world_id, actor_code, event_sequence)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_task_event_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND task_run_code ~ '^task[.]run[.][0-9a-f]{64}$'
        AND task_code IN ('task.civic.cleanup', 'task.civic.shift')
        AND ((task_code = 'task.civic.cleanup' AND activity_code = 'civic.cleanup')
             OR (task_code = 'task.civic.shift' AND activity_code = 'work.civic_shift'))
        AND source_intent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND event_sequence IN (1, 2)
        AND frame_sequence > 0
        AND event_type IN ('task_accepted', 'task_completed', 'task_expired')
        AND expiration_due_world_time_us > 0
        AND MOD(expiration_due_world_time_us, 1000000) = 0
        AND previous_event_hash ~ '^[0-9a-f]{64}$'
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND metadata = '{}'::jsonb
        AND (
            (event_sequence = 1 AND event_type = 'task_accepted'
             AND completion_activity_event_sequence IS NULL AND completion_activity_event_hash IS NULL)
            OR
            (event_sequence = 2 AND event_type = 'task_completed'
             AND completion_activity_event_sequence IS NOT NULL
             AND completion_activity_event_hash ~ '^[0-9a-f]{64}$')
            OR
            (event_sequence = 2 AND event_type = 'task_expired'
             AND completion_activity_event_sequence IS NULL AND completion_activity_event_hash IS NULL)
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_task_heads_owner
    ON city_realtime_character_task_heads (world_id, actor_code, accepted_frame_sequence DESC, task_run_code DESC);
CREATE INDEX IF NOT EXISTS idx_city_realtime_character_task_events_frame
    ON city_realtime_character_task_events (world_id, frame_sequence, actor_code, task_run_code);

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
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.12.0'
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
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.12.0'
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
      AND policy_id = 'city-realtime-agent-core' AND policy_version = '1.12.0';
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

CREATE OR REPLACE FUNCTION guard_city_realtime_character_task_head()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    gate_frame BIGINT := NULLIF(current_setting('sub2api.city_realtime_character_task_frame_sequence', TRUE), '')::BIGINT;
    gate_due BIGINT := NULLIF(current_setting('sub2api.city_realtime_character_task_due_world_time_us', TRUE), '')::BIGINT;
    intent_actor_code VARCHAR(96);
    intent_action_code VARCHAR(64);
    intent_status VARCHAR(16);
    intent_arguments JSONB;
    intent_scheduled_frame BIGINT;
    active_count BIGINT;
    activity_hash VARCHAR(64);
    activity_code_value VARCHAR(64);
    activity_frame BIGINT;
    expected_run_code VARCHAR(96);
    expected_genesis_hash VARCHAR(64);
    expected_event_hash VARCHAR(64);
    expected_state_hash VARCHAR(64);
    expected_aggregate_key VARCHAR(160);
    expected_dedup_key VARCHAR(160);
BEGIN
    IF NOT COALESCE(city_realtime_character_task_mutation_enabled(NEW.world_id, gate_frame, gate_due), FALSE) THEN
        RAISE EXCEPTION 'city realtime character task heads are immutable outside sealed reducers'
            USING ERRCODE = '55000';
    END IF;

    IF TG_OP = 'INSERT' THEN
        SELECT actor_code, action_code, status, arguments, scheduled_frame_sequence
        INTO intent_actor_code, intent_action_code, intent_status, intent_arguments, intent_scheduled_frame
        FROM city_realtime_agent_intents
        WHERE world_id = NEW.world_id AND intent_code = NEW.source_intent_code;
        expected_run_code := 'task.run.' || encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-task-run-v1', NEW.actor_code, NEW.task_code, NEW.source_intent_code
        ), 'UTF8')), 'hex');
        SELECT COUNT(*) INTO active_count
        FROM city_realtime_character_task_heads
        WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code AND task_status = 'accepted';
        expected_aggregate_key := 'task.aggregate.' || encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-task-aggregate-v1', NEW.actor_code, NEW.task_run_code
        ), 'UTF8')), 'hex');
        expected_dedup_key := 'task.expire.' || encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-task-expiry-dedup-v1', NEW.actor_code, NEW.task_run_code,
            NEW.source_intent_code, NEW.expiration_due_world_time_us::text
        ), 'UTF8')), 'hex');
        expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-task-chain-v1', NEW.actor_code, NEW.task_run_code,
            NEW.task_code, NEW.activity_code, NEW.source_intent_code,
            NEW.accepted_frame_sequence::text, NEW.expiration_due_world_time_us::text
        ), 'UTF8')), 'hex');
        expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-task-event-v1', NEW.actor_code, NEW.task_run_code,
            NEW.task_code, NEW.activity_code, NEW.source_intent_code, '1',
            NEW.accepted_frame_sequence::text, 'task_accepted',
            NEW.expiration_due_world_time_us::text, '0', '', expected_genesis_hash
        ), 'UTF8')), 'hex');
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-task-state-v1', NEW.actor_code, NEW.task_run_code,
            NEW.task_code, NEW.activity_code, NEW.source_intent_code, '1', 'accepted',
            NEW.accepted_frame_sequence::text, NEW.expiration_due_world_time_us::text,
            '0', '', NEW.last_frame_sequence::text, NEW.event_chain_hash
        ), 'UTF8')), 'hex');
        IF intent_actor_code IS DISTINCT FROM NEW.actor_code
           OR intent_action_code IS DISTINCT FROM 'character.task.accept'
           OR intent_status IS DISTINCT FROM 'pending'
           OR intent_arguments IS DISTINCT FROM jsonb_build_object('task_code', NEW.task_code)
           OR intent_scheduled_frame >= NEW.accepted_frame_sequence
           OR active_count <> 0
           OR NEW.task_run_code <> expected_run_code
           OR NEW.accepted_frame_sequence <> gate_frame
           OR NEW.expiration_due_world_time_us <> gate_due + 300000000
           OR NEW.task_revision <> 1 OR NEW.task_status <> 'accepted'
           OR NEW.last_frame_sequence <> NEW.accepted_frame_sequence
           OR NEW.event_chain_hash <> expected_event_hash
           OR NEW.state_hash <> expected_state_hash
           OR NOT EXISTS (
               SELECT 1 FROM city_due_events due
               WHERE due.world_id = NEW.world_id
                 AND due.event_type = 'system.realtime.character_task_expire'
                 AND due.schema_version = 1
                 AND due.due_world_time_us = NEW.expiration_due_world_time_us
                 AND due.temporal_phase = 'rule_effect'
                 AND due.priority = 110
                 AND due.aggregate_type = 'realtime_character_task'
                 AND due.aggregate_key = expected_aggregate_key
                 AND due.dedup_key = expected_dedup_key
                 AND due.source_kind = 'system'
                 AND due.source_reference = 'realtime_character_task'
                 AND due.expected_version = 1
                 AND due.status = 'pending'
                 AND due.created_frame_sequence = NEW.accepted_frame_sequence
                 AND due.payload = jsonb_build_object(
                     'schema_version', 1,
                     'actor_code', NEW.actor_code,
                     'task_run_code', NEW.task_run_code,
                     'task_code', NEW.task_code,
                     'source_intent_code', NEW.source_intent_code
                 )
           ) THEN
            RAISE EXCEPTION 'city realtime character task acceptance is not a sealed pending intent with one deadline'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'UPDATE' THEN
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-task-state-v1', NEW.actor_code, NEW.task_run_code,
            NEW.task_code, NEW.activity_code, NEW.source_intent_code,
            NEW.task_revision::text, NEW.task_status, NEW.accepted_frame_sequence::text,
            NEW.expiration_due_world_time_us::text,
            COALESCE(NEW.completion_activity_event_sequence, 0)::text,
            COALESCE(NEW.completion_activity_event_hash, ''),
            NEW.last_frame_sequence::text, NEW.event_chain_hash
        ), 'UTF8')), 'hex');
        IF NEW.actor_code <> OLD.actor_code OR NEW.task_run_code <> OLD.task_run_code
           OR NEW.task_code <> OLD.task_code OR NEW.activity_code <> OLD.activity_code
           OR NEW.source_intent_code <> OLD.source_intent_code
           OR NEW.accepted_frame_sequence <> OLD.accepted_frame_sequence
           OR NEW.expiration_due_world_time_us <> OLD.expiration_due_world_time_us
           OR NEW.task_revision <> OLD.task_revision + 1
           OR NEW.task_revision <> 2
           OR NEW.last_frame_sequence <> gate_frame
           OR NEW.last_frame_sequence <= OLD.last_frame_sequence
           OR NEW.event_chain_hash = OLD.event_chain_hash
           OR NEW.state_hash <> expected_state_hash
           OR NEW.metadata <> OLD.metadata THEN
            RAISE EXCEPTION 'city realtime character task head transition is invalid'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.task_status = 'completed' THEN
            SELECT event_hash, activity_code, frame_sequence
            INTO activity_hash, activity_code_value, activity_frame
            FROM city_realtime_character_activity_events
            WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
              AND event_sequence = NEW.completion_activity_event_sequence;
            IF gate_due >= NEW.expiration_due_world_time_us
               OR activity_hash IS DISTINCT FROM NEW.completion_activity_event_hash
               OR activity_code_value IS DISTINCT FROM NEW.activity_code
               OR activity_frame IS DISTINCT FROM NEW.last_frame_sequence THEN
                RAISE EXCEPTION 'city realtime character task completion lacks its exact earlier activity fact'
                    USING ERRCODE = '23514';
            END IF;
        ELSIF NEW.task_status = 'expired' THEN
            IF gate_due <> NEW.expiration_due_world_time_us
               OR NEW.completion_activity_event_sequence IS NOT NULL
               OR NEW.completion_activity_event_hash IS NOT NULL THEN
                RAISE EXCEPTION 'city realtime character task expiry must occur at its server deadline'
                    USING ERRCODE = '23514';
            END IF;
		ELSE
			RAISE EXCEPTION 'city realtime character task terminal status is invalid'
				USING ERRCODE = '23514';
		END IF;
		IF NOT EXISTS (
			SELECT 1 FROM city_realtime_character_task_events event
			WHERE event.world_id = NEW.world_id AND event.actor_code = NEW.actor_code
			  AND event.task_run_code = NEW.task_run_code AND event.event_sequence = 2
			  AND event.frame_sequence = NEW.last_frame_sequence
			  AND event.event_hash = NEW.event_chain_hash
			  AND event.event_type = CASE
			      WHEN NEW.task_status = 'completed' THEN 'task_completed'
			      WHEN NEW.task_status = 'expired' THEN 'task_expired'
			  END
		) THEN
			RAISE EXCEPTION 'city realtime character task head transition lacks its sealed event'
				USING ERRCODE = '23514';
		END IF;
		RETURN NEW;
    END IF;

    RAISE EXCEPTION 'city realtime character task heads are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_task_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    gate_frame BIGINT := NULLIF(current_setting('sub2api.city_realtime_character_task_frame_sequence', TRUE), '')::BIGINT;
    gate_due BIGINT := NULLIF(current_setting('sub2api.city_realtime_character_task_due_world_time_us', TRUE), '')::BIGINT;
    head_revision BIGINT;
    head_status VARCHAR(16);
    head_task_code VARCHAR(64);
    head_activity_code VARCHAR(64);
    head_source_intent_code VARCHAR(96);
    head_accepted_frame BIGINT;
    head_expiration_due BIGINT;
    head_last_frame BIGINT;
    head_chain_hash VARCHAR(64);
    intent_actor_code VARCHAR(96);
    intent_action_code VARCHAR(64);
    intent_status VARCHAR(16);
    intent_arguments JSONB;
    activity_hash VARCHAR(64);
    activity_code_value VARCHAR(64);
    activity_frame BIGINT;
    expected_genesis_hash VARCHAR(64);
    expected_event_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT'
       OR NOT COALESCE(city_realtime_character_task_mutation_enabled(NEW.world_id, gate_frame, gate_due), FALSE) THEN
        RAISE EXCEPTION 'city realtime character task events are append-only sealed facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT task_revision, task_status, task_code, activity_code, source_intent_code,
           accepted_frame_sequence, expiration_due_world_time_us,
           last_frame_sequence, event_chain_hash
    INTO head_revision, head_status, head_task_code, head_activity_code, head_source_intent_code,
         head_accepted_frame, head_expiration_due, head_last_frame, head_chain_hash
    FROM city_realtime_character_task_heads
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
      AND task_run_code = NEW.task_run_code;
    IF NOT FOUND
       OR NEW.task_code <> head_task_code
       OR NEW.activity_code <> head_activity_code
       OR NEW.source_intent_code <> head_source_intent_code
       OR NEW.expiration_due_world_time_us <> head_expiration_due
       OR NEW.frame_sequence <> gate_frame THEN
        RAISE EXCEPTION 'city realtime character task event head mismatch'
            USING ERRCODE = '23514';
    END IF;
    expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-task-event-v1', NEW.actor_code, NEW.task_run_code,
        NEW.task_code, NEW.activity_code, NEW.source_intent_code,
        NEW.event_sequence::text, NEW.frame_sequence::text, NEW.event_type,
        NEW.expiration_due_world_time_us::text,
        COALESCE(NEW.completion_activity_event_sequence, 0)::text,
        COALESCE(NEW.completion_activity_event_hash, ''),
        NEW.previous_event_hash
    ), 'UTF8')), 'hex');
    IF NEW.event_hash <> expected_event_hash THEN
        RAISE EXCEPTION 'city realtime character task event hash mismatch'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.event_sequence = 1 THEN
        SELECT actor_code, action_code, status, arguments
        INTO intent_actor_code, intent_action_code, intent_status, intent_arguments
        FROM city_realtime_agent_intents
        WHERE world_id = NEW.world_id AND intent_code = NEW.source_intent_code;
        expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-task-chain-v1', NEW.actor_code, NEW.task_run_code,
            NEW.task_code, NEW.activity_code, NEW.source_intent_code,
            NEW.frame_sequence::text, NEW.expiration_due_world_time_us::text
        ), 'UTF8')), 'hex');
        IF head_revision <> 1 OR head_status <> 'accepted'
           OR head_accepted_frame <> NEW.frame_sequence
           OR head_last_frame <> NEW.frame_sequence
           OR head_chain_hash <> NEW.event_hash
           OR NEW.event_type <> 'task_accepted'
           OR NEW.previous_event_hash <> expected_genesis_hash
           OR intent_actor_code IS DISTINCT FROM NEW.actor_code
           OR intent_action_code IS DISTINCT FROM 'character.task.accept'
           OR intent_status IS DISTINCT FROM 'pending'
           OR intent_arguments IS DISTINCT FROM jsonb_build_object('task_code', NEW.task_code) THEN
            RAISE EXCEPTION 'city realtime character task acceptance event is invalid'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.event_sequence <> 2
       OR head_revision <> 1 OR head_status <> 'accepted'
       OR head_chain_hash <> NEW.previous_event_hash
       OR head_last_frame >= NEW.frame_sequence THEN
        RAISE EXCEPTION 'city realtime character task terminal event sequence is invalid'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.event_type = 'task_completed' THEN
        SELECT event_hash, activity_code, frame_sequence
        INTO activity_hash, activity_code_value, activity_frame
        FROM city_realtime_character_activity_events
        WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
          AND event_sequence = NEW.completion_activity_event_sequence;
        IF gate_due >= NEW.expiration_due_world_time_us
           OR activity_hash IS DISTINCT FROM NEW.completion_activity_event_hash
           OR activity_code_value IS DISTINCT FROM NEW.activity_code
           OR activity_frame IS DISTINCT FROM NEW.frame_sequence THEN
            RAISE EXCEPTION 'city realtime character task completion event lacks its exact activity fact'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.event_type = 'task_expired' THEN
        IF gate_due <> NEW.expiration_due_world_time_us
           OR NEW.completion_activity_event_sequence IS NOT NULL
           OR NEW.completion_activity_event_hash IS NOT NULL THEN
            RAISE EXCEPTION 'city realtime character task expiry event is invalid'
                USING ERRCODE = '23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'city realtime character task event type is invalid'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_realtime_character_task_head_facts()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    event_count BIGINT;
    terminal_event_type VARCHAR(24);
    terminal_event_hash VARCHAR(64);
    terminal_event_frame BIGINT;
BEGIN
    SELECT COUNT(*) INTO event_count
    FROM city_realtime_character_task_events
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
      AND task_run_code = NEW.task_run_code;
    IF NEW.task_revision = 1 THEN
        IF event_count <> 1 OR NOT EXISTS (
            SELECT 1 FROM city_realtime_character_task_events
            WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
              AND task_run_code = NEW.task_run_code AND event_sequence = 1
              AND event_type = 'task_accepted' AND event_hash = NEW.event_chain_hash
              AND frame_sequence = NEW.accepted_frame_sequence
        ) THEN
            RAISE EXCEPTION 'city realtime character task accepted head lacks its sealed event'
                USING ERRCODE = '23514';
        END IF;
        RETURN NULL;
    END IF;
    SELECT event_type, event_hash, frame_sequence
    INTO terminal_event_type, terminal_event_hash, terminal_event_frame
    FROM city_realtime_character_task_events
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
      AND task_run_code = NEW.task_run_code AND event_sequence = 2;
    IF event_count <> 2
       OR terminal_event_type IS DISTINCT FROM (
            CASE
                WHEN NEW.task_status = 'completed' THEN 'task_completed'
                WHEN NEW.task_status = 'expired' THEN 'task_expired'
            END
          )
       OR terminal_event_hash IS DISTINCT FROM NEW.event_chain_hash
       OR terminal_event_frame IS DISTINCT FROM NEW.last_frame_sequence THEN
        RAISE EXCEPTION 'city realtime character task terminal head lacks its sealed event'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_task_binding_guard
    ON city_realtime_character_task_world_bindings;
CREATE TRIGGER city_realtime_character_task_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_task_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_task_binding();

DROP TRIGGER IF EXISTS city_realtime_character_task_head_guard
    ON city_realtime_character_task_heads;
CREATE TRIGGER city_realtime_character_task_head_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_task_heads
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_task_head();

DROP TRIGGER IF EXISTS city_realtime_character_task_event_guard
    ON city_realtime_character_task_events;
CREATE TRIGGER city_realtime_character_task_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_task_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_task_event();

DROP TRIGGER IF EXISTS city_realtime_character_task_head_fact_guard
    ON city_realtime_character_task_heads;
CREATE CONSTRAINT TRIGGER city_realtime_character_task_head_fact_guard
AFTER INSERT OR UPDATE ON city_realtime_character_task_heads
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_realtime_character_task_head_facts();

COMMENT ON TABLE city_realtime_character_task_heads IS
    'A3.3b owner-private structured task heads. Completion is bound only to a sealed autonomous Agent activity event.';
COMMENT ON TABLE city_realtime_character_task_events IS
    'A3.3b append-only structured task facts; no task reward, wallet, provider, case, or free-text data is stored.';
