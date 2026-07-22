-- Realtime actor runtime is deliberately separate from the legacy
-- tick-driven open-world actor tables. A realtime-v2 actor is an anonymous,
-- shared-world simulation entity: member-safe projections expose only its
-- public code, generic label, visual variant, and spatial state.  Ownership,
-- private agent memory, and control grants are intentionally not modelled in
-- this first runtime slice.

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_actors' THEN capabilities
    ELSE capabilities || '["realtime_actors","actor_position_events","member_safe_actor_projection"]'::jsonb
END
WHERE version = 'city-openworld-realtime-v2';

CREATE TABLE IF NOT EXISTS city_realtime_actor_identities (
    world_id BIGINT NOT NULL REFERENCES city_world_time_states(world_id) ON DELETE RESTRICT,
    actor_code VARCHAR(96) NOT NULL,
    actor_kind VARCHAR(24) NOT NULL,
    public_label VARCHAR(64) NOT NULL,
    appearance_variant VARCHAR(64) NOT NULL,
    lifecycle_status VARCHAR(16) NOT NULL DEFAULT 'active',
    spawn_x BIGINT NOT NULL,
    spawn_y BIGINT NOT NULL,
    spawn_z SMALLINT NOT NULL DEFAULT 0,
    spawn_frame_sequence BIGINT NOT NULL DEFAULT 0,
    identity_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code),
    CONSTRAINT city_realtime_actor_identity_check CHECK (
        actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND actor_kind IN ('npc', 'character', 'service')
        AND public_label ~ '^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$'
        AND appearance_variant ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND lifecycle_status IN ('active', 'inactive', 'retired')
        AND spawn_frame_sequence >= 0
        AND identity_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    ),
    CONSTRAINT city_realtime_actor_identity_spawn_frame_fk
        FOREIGN KEY (world_id, spawn_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_actor_identities_world_status
    ON city_realtime_actor_identities (world_id, lifecycle_status, actor_code);

CREATE TABLE IF NOT EXISTS city_realtime_actor_states (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    x BIGINT NOT NULL,
    y BIGINT NOT NULL,
    z SMALLINT NOT NULL DEFAULT 0,
    motion_state VARCHAR(16) NOT NULL DEFAULT 'idle',
    position_revision BIGINT NOT NULL DEFAULT 1,
    last_frame_sequence BIGINT NOT NULL DEFAULT 0,
    state_hash VARCHAR(64) NOT NULL,
    event_chain_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code),
    CONSTRAINT city_realtime_actor_state_identity_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_actor_state_check CHECK (
        motion_state IN ('idle', 'walking', 'inside', 'unavailable')
        AND position_revision > 0
        AND last_frame_sequence >= 0
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND event_chain_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    ),
    CONSTRAINT city_realtime_actor_state_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_actor_states_window
    ON city_realtime_actor_states (world_id, z, y, x, actor_code);

CREATE TABLE IF NOT EXISTS city_realtime_actor_position_events (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    event_kind VARCHAR(16) NOT NULL,
    from_x BIGINT,
    from_y BIGINT,
    from_z SMALLINT,
    to_x BIGINT NOT NULL,
    to_y BIGINT NOT NULL,
    to_z SMALLINT NOT NULL DEFAULT 0,
    motion_state VARCHAR(16) NOT NULL,
    public_visibility BOOLEAN NOT NULL DEFAULT TRUE,
    previous_event_hash VARCHAR(64),
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, event_sequence),
    CONSTRAINT city_realtime_actor_position_event_identity_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_actor_position_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_actor_position_event_check CHECK (
        event_sequence >= 0
        AND frame_sequence >= 0
        AND event_kind IN ('spawn', 'move', 'despawn', 'teleport')
        AND motion_state IN ('idle', 'walking', 'inside', 'unavailable')
        AND ((from_x IS NULL AND from_y IS NULL AND from_z IS NULL)
             OR (from_x IS NOT NULL AND from_y IS NOT NULL AND from_z IS NOT NULL))
        AND (previous_event_hash IS NULL OR previous_event_hash ~ '^[0-9a-f]{64}$')
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_actor_position_events_world_frame
    ON city_realtime_actor_position_events (world_id, frame_sequence, actor_code, event_sequence);

CREATE OR REPLACE FUNCTION city_realtime_actor_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_actor_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0
             AND world.state_hash IS NULL
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_actor_mutation_enabled(
    target_world_id BIGINT,
    target_frame_sequence BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_actor_mutation_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_actor_mutation_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_actor_identity()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' AND city_realtime_actor_initialization_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime actor identities are immutable outside genesis initialization'
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
       AND city_realtime_actor_mutation_enabled(NEW.world_id, NEW.frame_sequence)
       AND NEW.event_sequence > 0 THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime actor position events are append-only sealed frame facts'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_actor_identity_guard ON city_realtime_actor_identities;
CREATE TRIGGER city_realtime_actor_identity_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_actor_identities
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_actor_identity();

DROP TRIGGER IF EXISTS city_realtime_actor_state_guard ON city_realtime_actor_states;
CREATE TRIGGER city_realtime_actor_state_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_actor_states
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_actor_state();

DROP TRIGGER IF EXISTS city_realtime_actor_position_event_guard ON city_realtime_actor_position_events;
CREATE TRIGGER city_realtime_actor_position_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_actor_position_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_actor_position_event();

COMMENT ON TABLE city_realtime_actor_identities IS
    'Anonymous shared-world actor identities. No user identifiers, agent memory, or control grants are stored here.';
COMMENT ON TABLE city_realtime_actor_states IS
    'Current actor state hashed into realtime canonical state; only the sealed temporal reducer may mutate it.';
COMMENT ON TABLE city_realtime_actor_position_events IS
    'Append-only actor position chain. The state stores the current chain head so canonical state remains bounded.';
