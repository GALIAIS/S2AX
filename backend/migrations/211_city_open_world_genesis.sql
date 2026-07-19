-- Open-world V2 genesis facts. This is deliberately separate from F7's
-- fixed 9x9 Overmap and from its single-chunk rectangular land projections.

CREATE TABLE IF NOT EXISTS city_open_world_bindings (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
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
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_binding_identity_check CHECK (
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
    )
);

CREATE TABLE IF NOT EXISTS city_open_world_sectors (
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    sector_x BIGINT NOT NULL,
    sector_y BIGINT NOT NULL,
    epoch BIGINT NOT NULL DEFAULT 1,
    chunk_size SMALLINT NOT NULL DEFAULT 32 CHECK (chunk_size = 32),
    sector_size_chunks SMALLINT NOT NULL DEFAULT 8 CHECK (sector_size_chunks = 8),
    status VARCHAR(16) NOT NULL DEFAULT 'generated' CHECK (status = 'generated'),
    plan_hash VARCHAR(64) NOT NULL CHECK (plan_hash ~ '^[0-9a-f]{64}$'),
    content_hash VARCHAR(64) NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    generated_tick BIGINT NOT NULL DEFAULT 0 CHECK (generated_tick >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, sector_x, sector_y, epoch),
    CONSTRAINT city_open_world_sector_binding_fk
        FOREIGN KEY (world_id) REFERENCES city_open_world_bindings(world_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_sectors_world_order
    ON city_open_world_sectors (world_id, epoch, sector_y, sector_x);

CREATE TABLE IF NOT EXISTS city_open_world_chunks (
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, chunk_x, chunk_y, z),
    CONSTRAINT city_open_world_chunk_sector_fk
        FOREIGN KEY (world_id, sector_x, sector_y, epoch)
        REFERENCES city_open_world_sectors(world_id, sector_x, sector_y, epoch) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_chunks_window
    ON city_open_world_chunks (world_id, z, chunk_y, chunk_x);

CREATE TABLE IF NOT EXISTS city_open_world_buildings (
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_building_sector_fk
        FOREIGN KEY (world_id, sector_x, sector_y, epoch)
        REFERENCES city_open_world_sectors(world_id, sector_x, sector_y, epoch) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_buildings_sector
    ON city_open_world_buildings (world_id, sector_y, sector_x, code);

CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v1'
              AND world.current_tick = 0
              AND world.state_hash IS NULL
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_projection()
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
    IF TG_OP = 'INSERT' AND city_open_world_initialization_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city open-world genesis facts are immutable outside initialization'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_binding_guard ON city_open_world_bindings;
CREATE TRIGGER city_open_world_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_projection();

DROP TRIGGER IF EXISTS city_open_world_sector_guard ON city_open_world_sectors;
CREATE TRIGGER city_open_world_sector_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_sectors
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_projection();

DROP TRIGGER IF EXISTS city_open_world_chunk_guard ON city_open_world_chunks;
CREATE TRIGGER city_open_world_chunk_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_chunks
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_projection();

DROP TRIGGER IF EXISTS city_open_world_building_guard ON city_open_world_buildings;
CREATE TRIGGER city_open_world_building_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_buildings
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_projection();
