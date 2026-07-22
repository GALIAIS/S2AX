-- Realtime V2 adds a sealed static spatial foundation without reusing the
-- tick-driven city_open_world_* tables.  The separate namespace is deliberate:
-- a realtime world can share a deterministic map and a temporal cursor while
-- remaining outside legacy materialization, actor, and hourly-reducer paths.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-realtime-v2',
    'supported',
    'city-realtime-state-v2+gzip',
    '["control","ledger","resources","calendar_demography","realtime_clock","temporal_frames","due_events","shared_world","static_worldgen","vertical_interiors","portal_topology","semantic_pixel_projection","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

ALTER TABLE city_world_time_states
    DROP CONSTRAINT IF EXISTS city_world_time_states_identity_check;

ALTER TABLE city_world_time_states
    ADD CONSTRAINT city_world_time_states_identity_check CHECK (
        temporal_engine_version IN ('city-openworld-realtime-v1', 'city-openworld-realtime-v2')
        AND clock_profile_hash ~ '^[0-9a-f]{64}$'
        AND lifecycle_status IN ('running', 'paused', 'archived', 'recovering')
        AND clock_state IN ('initializing', 'healthy', 'degraded', 'unsafe', 'recovering')
        AND current_world_time_us >= 0
        AND timeline_frame_sequence >= 0
        AND timeline_cursor ~ '^twf_[0-9]{12}$'
        AND (next_due_at_world_time_us IS NULL OR next_due_at_world_time_us >= current_world_time_us)
        AND (catchup_target_world_time_us IS NULL OR catchup_target_world_time_us >= current_world_time_us)
        AND recovery_state IN ('idle', 'catching_up', 'failed', 'held')
        AND version > 0
    );

ALTER TABLE city_temporal_frames
    DROP CONSTRAINT IF EXISTS city_temporal_frames_identity_check;

ALTER TABLE city_temporal_frames
    ADD CONSTRAINT city_temporal_frames_identity_check CHECK (
        frame_sequence >= 0
        AND timeline_cursor ~ '^twf_[0-9]{12}$'
        AND world_time_from_us >= 0
        AND world_time_to_us >= world_time_from_us
        AND temporal_engine_version IN ('city-openworld-realtime-v1', 'city-openworld-realtime-v2')
        AND clock_profile_hash ~ '^[0-9a-f]{64}$'
        AND frame_kind IN ('genesis', 'command', 'due_event', 'settlement', 'lifecycle', 'recovery', 'diagnostic')
        AND effective_utc_to >= effective_utc_from
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND (previous_state_hash IS NULL OR previous_state_hash ~ '^[0-9a-f]{64}$')
        AND due_event_digest ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(phase_summary) = 'object'
    );

CREATE TABLE IF NOT EXISTS city_realtime_spatial_bindings (
    world_id BIGINT PRIMARY KEY REFERENCES city_world_time_states(world_id) ON DELETE RESTRICT,
    generator_id VARCHAR(64) NOT NULL,
    generator_version VARCHAR(24) NOT NULL,
    rule_set_id VARCHAR(64) NOT NULL,
    rule_set_version VARCHAR(24) NOT NULL,
    rule_set_hash VARCHAR(64) NOT NULL,
    profile_id VARCHAR(64) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    profile_hash VARCHAR(64) NOT NULL,
    context_hash VARCHAR(64) NOT NULL,
    seed BIGINT NOT NULL CHECK (seed > 0),
    spawn_sector_x BIGINT NOT NULL,
    spawn_sector_y BIGINT NOT NULL,
    spawn_x BIGINT NOT NULL,
    spawn_y BIGINT NOT NULL,
    spawn_z SMALLINT NOT NULL DEFAULT 0,
    epoch BIGINT NOT NULL DEFAULT 1 CHECK (epoch = 1),
    bootstrap_plan_hash VARCHAR(64) NOT NULL,
    genesis_hash VARCHAR(64) NOT NULL,
    genesis_frame_sequence BIGINT NOT NULL DEFAULT 0 CHECK (genesis_frame_sequence = 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_spatial_binding_identity_check CHECK (
        generator_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND generator_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND rule_set_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND rule_set_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND rule_set_hash ~ '^[0-9a-f]{64}$'
        AND profile_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND profile_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND profile_hash ~ '^[0-9a-f]{64}$'
        AND context_hash ~ '^[0-9a-f]{64}$'
        AND bootstrap_plan_hash ~ '^[0-9a-f]{64}$'
        AND genesis_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    ),
    CONSTRAINT city_realtime_spatial_binding_genesis_frame_fk
        FOREIGN KEY (world_id, genesis_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE IF NOT EXISTS city_realtime_spatial_regions (
    world_id BIGINT NOT NULL REFERENCES city_realtime_spatial_bindings(world_id) ON DELETE RESTRICT,
    region_x BIGINT NOT NULL,
    region_y BIGINT NOT NULL,
    epoch BIGINT NOT NULL DEFAULT 1 CHECK (epoch = 1),
    chunk_size SMALLINT NOT NULL DEFAULT 32 CHECK (chunk_size = 32),
    region_size_chunks SMALLINT NOT NULL DEFAULT 32 CHECK (region_size_chunks = 32),
    status VARCHAR(16) NOT NULL DEFAULT 'generated' CHECK (status = 'generated'),
    plan_hash VARCHAR(64) NOT NULL CHECK (plan_hash ~ '^[0-9a-f]{64}$'),
    materialized_frame_sequence BIGINT NOT NULL DEFAULT 0 CHECK (materialized_frame_sequence = 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, region_x, region_y, epoch),
    CONSTRAINT city_realtime_spatial_region_frame_fk
        FOREIGN KEY (world_id, materialized_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_spatial_regions_world_order
    ON city_realtime_spatial_regions (world_id, epoch, region_y, region_x);

CREATE TABLE IF NOT EXISTS city_realtime_spatial_sectors (
    world_id BIGINT NOT NULL REFERENCES city_realtime_spatial_bindings(world_id) ON DELETE RESTRICT,
    sector_x BIGINT NOT NULL,
    sector_y BIGINT NOT NULL,
    epoch BIGINT NOT NULL DEFAULT 1 CHECK (epoch = 1),
    chunk_size SMALLINT NOT NULL DEFAULT 32 CHECK (chunk_size = 32),
    sector_size_chunks SMALLINT NOT NULL DEFAULT 8 CHECK (sector_size_chunks = 8),
    status VARCHAR(16) NOT NULL DEFAULT 'generated' CHECK (status = 'generated'),
    plan_hash VARCHAR(64) NOT NULL CHECK (plan_hash ~ '^[0-9a-f]{64}$'),
    content_hash VARCHAR(64) NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    materialized_frame_sequence BIGINT NOT NULL DEFAULT 0 CHECK (materialized_frame_sequence = 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, sector_x, sector_y, epoch),
    CONSTRAINT city_realtime_spatial_sector_frame_fk
        FOREIGN KEY (world_id, materialized_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_spatial_sectors_world_order
    ON city_realtime_spatial_sectors (world_id, epoch, sector_y, sector_x);

CREATE TABLE IF NOT EXISTS city_realtime_spatial_chunks (
    world_id BIGINT NOT NULL,
    sector_x BIGINT NOT NULL,
    sector_y BIGINT NOT NULL,
    epoch BIGINT NOT NULL DEFAULT 1,
    chunk_x BIGINT NOT NULL,
    chunk_y BIGINT NOT NULL,
    z SMALLINT NOT NULL DEFAULT 0 CHECK (z = 0),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object' AND pg_column_size(payload) <= 262144),
    payload_hash VARCHAR(64) NOT NULL CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, chunk_x, chunk_y, z),
    CONSTRAINT city_realtime_spatial_chunk_sector_fk
        FOREIGN KEY (world_id, sector_x, sector_y, epoch)
        REFERENCES city_realtime_spatial_sectors(world_id, sector_x, sector_y, epoch) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_spatial_chunks_window
    ON city_realtime_spatial_chunks (world_id, z, chunk_y, chunk_x);

CREATE TABLE IF NOT EXISTS city_realtime_spatial_buildings (
    world_id BIGINT NOT NULL,
    code VARCHAR(96) NOT NULL,
    sector_x BIGINT NOT NULL,
    sector_y BIGINT NOT NULL,
    epoch BIGINT NOT NULL DEFAULT 1,
    city_code VARCHAR(96) NOT NULL,
    lot_code VARCHAR(96) NOT NULL,
    primary_use VARCHAR(24) NOT NULL CHECK (primary_use IN ('residential', 'commercial', 'industrial')),
    archetype_code VARCHAR(96) NOT NULL,
    layout_style VARCHAR(64) NOT NULL,
    floor_count INTEGER NOT NULL CHECK (floor_count > 0),
    entrance_x BIGINT NOT NULL,
    entrance_y BIGINT NOT NULL,
    entrance_z SMALLINT NOT NULL DEFAULT 0,
    footprint JSONB NOT NULL CHECK (jsonb_typeof(footprint) = 'array' AND jsonb_array_length(footprint) > 0),
    footprint_hash VARCHAR(64) NOT NULL CHECK (footprint_hash ~ '^[0-9a-f]{64}$'),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_realtime_spatial_building_sector_fk
        FOREIGN KEY (world_id, sector_x, sector_y, epoch)
        REFERENCES city_realtime_spatial_sectors(world_id, sector_x, sector_y, epoch) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_spatial_buildings_sector
    ON city_realtime_spatial_buildings (world_id, sector_y, sector_x, code);

CREATE TABLE IF NOT EXISTS city_realtime_spatial_building_interiors (
    world_id BIGINT NOT NULL,
    building_code VARCHAR(96) NOT NULL,
    floor_index INTEGER NOT NULL CHECK (floor_index >= 0),
    z SMALLINT NOT NULL CHECK (z BETWEEN 0 AND 127),
    layout_version VARCHAR(24) NOT NULL,
    layout_style VARCHAR(64) NOT NULL,
    cells JSONB NOT NULL CHECK (
        jsonb_typeof(cells) = 'array'
        AND jsonb_array_length(cells) > 0
        AND pg_column_size(cells) <= 1048576
    ),
    content_hash VARCHAR(64) NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, building_code, floor_index),
    CONSTRAINT city_realtime_spatial_interior_building_fk
        FOREIGN KEY (world_id, building_code)
        REFERENCES city_realtime_spatial_buildings(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_spatial_interior_identity_check CHECK (
        layout_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND layout_style ~ '^[a-z][a-z0-9_.-]{1,63}$'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_spatial_interiors_world_building
    ON city_realtime_spatial_building_interiors (world_id, building_code, floor_index);

CREATE TABLE IF NOT EXISTS city_realtime_spatial_portals (
    world_id BIGINT NOT NULL,
    code VARCHAR(128) NOT NULL,
    building_code VARCHAR(96) NOT NULL,
    portal_type VARCHAR(16) NOT NULL CHECK (portal_type IN ('entrance', 'stairs')),
    from_floor_index INTEGER NOT NULL CHECK (from_floor_index >= 0),
    to_floor_index INTEGER NOT NULL CHECK (to_floor_index >= 0),
    from_x BIGINT NOT NULL,
    from_y BIGINT NOT NULL,
    from_z SMALLINT NOT NULL CHECK (from_z BETWEEN 0 AND 127),
    to_x BIGINT NOT NULL,
    to_y BIGINT NOT NULL,
    to_z SMALLINT NOT NULL CHECK (to_z BETWEEN 0 AND 127),
    bidirectional BOOLEAN NOT NULL DEFAULT TRUE,
    topology_hash VARCHAR(64) NOT NULL CHECK (topology_hash ~ '^[0-9a-f]{64}$'),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_realtime_spatial_portal_code_check CHECK (code ~ '^[a-z][a-z0-9_.-]{1,127}$'),
    CONSTRAINT city_realtime_spatial_portal_building_fk
        FOREIGN KEY (world_id, building_code)
        REFERENCES city_realtime_spatial_buildings(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_spatial_portal_shape_check CHECK (
        (portal_type = 'entrance'
            AND from_floor_index = 0 AND to_floor_index = 0
            AND from_z = 0 AND to_z = 0)
        OR
        (portal_type = 'stairs'
            AND to_floor_index = from_floor_index + 1
            AND to_z = from_z + 1)
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_spatial_portals_building
    ON city_realtime_spatial_portals (world_id, building_code, from_floor_index, to_floor_index, code);

CREATE OR REPLACE FUNCTION city_realtime_static_worldgen_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_realtime_static_worldgen_initialize_world_id', TRUE), '') = target_world_id::TEXT
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

CREATE OR REPLACE FUNCTION guard_city_realtime_static_worldgen_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        target_world_id := OLD.world_id;
    ELSE
        target_world_id := NEW.world_id;
    END IF;
    IF TG_OP = 'INSERT' AND city_realtime_static_worldgen_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime static worldgen facts are immutable outside genesis initialization'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_spatial_binding_guard ON city_realtime_spatial_bindings;
CREATE TRIGGER city_realtime_spatial_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_spatial_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_static_worldgen_projection();

DROP TRIGGER IF EXISTS city_realtime_spatial_region_guard ON city_realtime_spatial_regions;
CREATE TRIGGER city_realtime_spatial_region_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_spatial_regions
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_static_worldgen_projection();

DROP TRIGGER IF EXISTS city_realtime_spatial_sector_guard ON city_realtime_spatial_sectors;
CREATE TRIGGER city_realtime_spatial_sector_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_spatial_sectors
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_static_worldgen_projection();

DROP TRIGGER IF EXISTS city_realtime_spatial_chunk_guard ON city_realtime_spatial_chunks;
CREATE TRIGGER city_realtime_spatial_chunk_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_spatial_chunks
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_static_worldgen_projection();

DROP TRIGGER IF EXISTS city_realtime_spatial_building_guard ON city_realtime_spatial_buildings;
CREATE TRIGGER city_realtime_spatial_building_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_spatial_buildings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_static_worldgen_projection();

DROP TRIGGER IF EXISTS city_realtime_spatial_interior_guard ON city_realtime_spatial_building_interiors;
CREATE TRIGGER city_realtime_spatial_interior_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_spatial_building_interiors
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_static_worldgen_projection();

DROP TRIGGER IF EXISTS city_realtime_spatial_portal_guard ON city_realtime_spatial_portals;
CREATE TRIGGER city_realtime_spatial_portal_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_spatial_portals
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_static_worldgen_projection();
