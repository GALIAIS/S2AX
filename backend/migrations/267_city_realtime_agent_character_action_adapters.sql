-- A3.2: versioned Character Agent action adapters.  This policy expands only
-- new worlds to adjacent movement, immutable portal traversal and catalogued
-- role changes; 1.0/1.1/1.2 bindings retain their original action catalogues
-- and canonical hashes.

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
          "character.move",
          "character.portal.traverse",
          "character.role.change"
        ],
        "temporal_rule": "decision_then_future_frame",
        "user_control": ["manual", "assisted", "autonomous", "suspended"],
        "personality_revision_schema": "city-realtime-character-personality-v1",
        "action_context_schema": "city-realtime-character-action-context-v1"
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.3.0', 'published', 1, manifest,
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
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_agent_character_navigation_intents' THEN capabilities
    ELSE capabilities ||
        '["realtime_agent_character_navigation_intents","realtime_agent_character_role_intents","realtime_agent_action_context"]'::jsonb
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
          AND binding.policy_version IN ('1.1.0', '1.2.0', '1.3.0')
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
          AND binding.policy_version IN ('1.2.0', '1.3.0')
          AND bundle.status = 'published'
          AND bundle.policy_hash = binding.policy_hash
    )
$$;

-- Migration 266 accidentally narrowed the pre-existing portal/role receipt
-- catalogue.  Restore it while retaining the owner Agent configuration type.
ALTER TABLE city_realtime_character_action_receipts
    DROP CONSTRAINT IF EXISTS city_realtime_character_action_receipt_check;

ALTER TABLE city_realtime_character_action_receipts
    ADD CONSTRAINT city_realtime_character_action_receipt_check CHECK (
        idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$'
        AND actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND action_type IN ('character.create', 'character.move', 'character.activity', 'character.portal', 'character.role', 'character.agent.configure')
        AND request_hash ~ '^[0-9a-f]{64}$'
        AND frame_sequence > 0
        AND jsonb_typeof(result_payload) = 'object'
        AND result_payload::TEXT !~* '"(email|username|owner_user_id|prompt|provider|api_key|secret|memory|response)"[[:space:]]*:'
        AND result_hash ~ '^[0-9a-f]{64}$'
        AND result_hash = encode(sha256(convert_to(result_payload::text, 'UTF8')), 'hex')
    );

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
