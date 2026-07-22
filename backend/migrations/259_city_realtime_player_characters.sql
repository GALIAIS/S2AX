-- Realtime player characters are public Actors with private ownership carried
-- only by their character Agent. They are created and moved through sealed
-- Temporal Frames; browsers cannot write actor rows or choose a frame.

ALTER TABLE city_realtime_actor_identities
    DROP CONSTRAINT IF EXISTS city_realtime_actor_identity_check;

ALTER TABLE city_realtime_actor_identities
    ADD CONSTRAINT city_realtime_actor_identity_check CHECK (
        actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND actor_kind IN ('npc', 'character', 'service')
        AND public_label = btrim(public_label)
        AND char_length(public_label) BETWEEN 1 AND 64
        -- Character labels are public map facts. Keep the database fence
        -- deliberately conservative even though the service applies the
        -- stricter Unicode name policy used by the renderer as well.
        AND public_label !~ '(^[[:space:]]|[[:space:]]$|[[:cntrl:]@/:<>])'
        AND position(chr(92) IN public_label) = 0
        AND appearance_variant ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND lifecycle_status IN ('active', 'inactive', 'retired')
        AND spawn_frame_sequence >= 0
        AND identity_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    );

ALTER TABLE city_realtime_actor_identities
    DROP CONSTRAINT IF EXISTS city_realtime_actor_identity_player_character_code_check;

ALTER TABLE city_realtime_actor_identities
    ADD CONSTRAINT city_realtime_actor_identity_player_character_code_check CHECK (
        actor_kind <> 'character'
        OR actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
    );

ALTER TABLE city_realtime_agent_instances
    DROP CONSTRAINT IF EXISTS city_realtime_agent_instance_user_character_code_check;

ALTER TABLE city_realtime_agent_instances
    ADD CONSTRAINT city_realtime_agent_instance_user_character_code_check CHECK (
        agent_subtype <> 'character.user'
        OR (
            actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
            AND agent_code = 'agent.' || actor_code
        )
    );

CREATE OR REPLACE FUNCTION guard_city_realtime_actor_identity()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' AND city_realtime_actor_initialization_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT'
       AND NEW.actor_kind = 'character'
       AND NEW.spawn_frame_sequence > 0
       AND city_realtime_actor_mutation_enabled(NEW.world_id, NEW.spawn_frame_sequence) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime actor identities are immutable outside genesis or sealed character creation'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_actor_state()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' AND city_realtime_actor_initialization_enabled(NEW.world_id)
       AND NEW.position_revision = 1
       AND NEW.last_frame_sequence = 0 THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT'
       AND NEW.position_revision = 1
       AND NEW.last_frame_sequence > 0
       AND city_realtime_actor_mutation_enabled(NEW.world_id, NEW.last_frame_sequence) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_actor_mutation_enabled(NEW.world_id, NEW.last_frame_sequence)
       AND NEW.position_revision = OLD.position_revision + 1
       AND NEW.last_frame_sequence > OLD.last_frame_sequence
       AND NEW.event_chain_hash <> OLD.event_chain_hash
       AND NEW.state_hash <> OLD.state_hash
       AND NEW.metadata = OLD.metadata THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime actor state may change only through the sealed frame reducer'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_actor_position_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' AND city_realtime_actor_initialization_enabled(NEW.world_id)
       AND NEW.event_sequence = 0
       AND NEW.frame_sequence = 0
       AND NEW.event_kind = 'spawn' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT'
       AND NEW.event_sequence = 0
       AND NEW.frame_sequence > 0
       AND NEW.event_kind = 'spawn'
       AND city_realtime_actor_mutation_enabled(NEW.world_id, NEW.frame_sequence) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT'
       AND city_realtime_actor_mutation_enabled(NEW.world_id, NEW.frame_sequence)
       AND NEW.event_sequence > 0 THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime actor position events are append-only sealed frame facts'
        USING ERRCODE = '55000';
END;
$$;

CREATE TABLE IF NOT EXISTS city_realtime_character_action_receipts (
    world_id BIGINT NOT NULL REFERENCES city_world_time_states(world_id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    idempotency_key VARCHAR(128) NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    action_type VARCHAR(32) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    frame_sequence BIGINT NOT NULL,
    result_payload JSONB NOT NULL,
    result_hash VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, user_id, idempotency_key),
    CONSTRAINT city_realtime_character_action_receipt_actor_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_action_receipt_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_action_receipt_check CHECK (
        idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$'
        AND actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND action_type IN ('character.create', 'character.move')
        AND request_hash ~ '^[0-9a-f]{64}$'
        AND frame_sequence > 0
        AND jsonb_typeof(result_payload) = 'object'
        AND result_payload::TEXT !~* '"(email|username|owner_user_id|prompt|provider|api_key|secret|memory|response)"[[:space:]]*:'
        AND result_hash ~ '^[0-9a-f]{64}$'
        AND result_hash = encode(sha256(convert_to(result_payload::text, 'UTF8')), 'hex')
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_action_receipts_actor_frame
    ON city_realtime_character_action_receipts (world_id, actor_code, frame_sequence DESC);

CREATE OR REPLACE FUNCTION city_realtime_character_receipt_write_enabled(
    target_world_id BIGINT,
    target_frame_sequence BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_receipt_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_receipt_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence = target_frame_sequence
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_action_receipt()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_character_receipt_write_enabled(NEW.world_id, NEW.frame_sequence) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character action receipts are immutable sealed-frame facts'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_action_receipt_guard
    ON city_realtime_character_action_receipts;
CREATE TRIGGER city_realtime_character_action_receipt_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_action_receipts
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_action_receipt();

COMMENT ON TABLE city_realtime_character_action_receipts IS
    'Private idempotency receipt for realtime player-character creation and movement. It contains no account profile, prompt, provider, memory, or model response.';
