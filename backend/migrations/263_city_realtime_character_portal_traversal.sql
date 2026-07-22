-- Character portal traversal turns the already-persisted immutable entrance
-- and stair topology into a sealed player command. It does not modify static
-- world generation, profile catalogs, balances, or historic actor hashes.

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
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_portals' THEN capabilities
    ELSE capabilities ||
        '["realtime_character_portals","realtime_character_interiors"]'::jsonb
END
WHERE version = 'city-openworld-realtime-v2';

-- A nullable portal code is additive for historic event rows. New portal
-- movements must commit the topology edge into the position-event hash chain;
-- ordinary walking, spawn, patrol, and legacy events keep it NULL.
ALTER TABLE city_realtime_actor_position_events
    ADD COLUMN IF NOT EXISTS portal_code VARCHAR(128);

ALTER TABLE city_realtime_actor_position_events
    DROP CONSTRAINT IF EXISTS city_realtime_actor_position_event_check;

ALTER TABLE city_realtime_actor_position_events
    ADD CONSTRAINT city_realtime_actor_position_event_check CHECK (
        event_sequence >= 0
        AND frame_sequence >= 0
        AND event_kind IN ('spawn', 'move', 'despawn', 'teleport', 'portal')
        AND motion_state IN ('idle', 'walking', 'inside', 'unavailable')
        AND ((from_x IS NULL AND from_y IS NULL AND from_z IS NULL)
             OR (from_x IS NOT NULL AND from_y IS NOT NULL AND from_z IS NOT NULL))
        AND (
            (event_kind = 'portal'
             AND portal_code ~ '^[a-z][a-z0-9_.-]{1,127}$')
            OR
            (event_kind <> 'portal' AND portal_code IS NULL)
        )
        AND (previous_event_hash IS NULL OR previous_event_hash ~ '^[0-9a-f]{64}$')
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    );

-- The immutable interior payload is queried by exact cell coordinates during
-- authoritative movement. A GIN index avoids a full-building scan as cities
-- gain denser vertical structures.
CREATE INDEX IF NOT EXISTS idx_city_realtime_spatial_interiors_cells
    ON city_realtime_spatial_building_interiors
    USING GIN (cells jsonb_path_ops);

-- Extend the sealed receipt envelope without weakening its redaction or hash
-- fence. A portal code is static map topology, not a path or a target chosen
-- by the browser.
ALTER TABLE city_realtime_character_action_receipts
    DROP CONSTRAINT IF EXISTS city_realtime_character_action_receipt_check;

ALTER TABLE city_realtime_character_action_receipts
    ADD CONSTRAINT city_realtime_character_action_receipt_check CHECK (
        idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$'
        AND actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND action_type IN ('character.create', 'character.move', 'character.activity', 'character.portal')
        AND request_hash ~ '^[0-9a-f]{64}$'
        AND frame_sequence > 0
        AND jsonb_typeof(result_payload) = 'object'
        AND result_payload::TEXT !~* '"(email|username|owner_user_id|prompt|provider|api_key|secret|memory|response)"[[:space:]]*:'
        AND result_hash ~ '^[0-9a-f]{64}$'
        AND result_hash = encode(sha256(convert_to(result_payload::text, 'UTF8')), 'hex')
    );

COMMENT ON COLUMN city_realtime_actor_position_events.portal_code IS
    'Immutable static portal edge committed only by character.portal sealed frames; NULL for ordinary actor movement.';
