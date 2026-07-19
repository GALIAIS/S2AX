-- Open-world V2: immutable 32x32 chunk region plans and command-gated
-- sector materialization.  V1 genesis rows remain unchanged; V2 stores the
-- plan hash once per region and derives every contained 8x8 sector from it.

ALTER TABLE city_engine_versions
    DROP CONSTRAINT IF EXISTS city_engine_version_code_check;

ALTER TABLE city_engine_versions
    ADD CONSTRAINT city_engine_version_code_check
    CHECK (version ~ '^city-(f[0-9]+|openworld)-v[0-9]+$');

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES
    ('city-openworld-v1', 'supported', 'city-state-v1+gzip',
     '["control","ledger","resources","calendar_demography","open_world_genesis","markets","snapshot","replay"]'::jsonb),
    ('city-openworld-v2', 'supported', 'city-state-v1+gzip',
     '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","markets","snapshot","replay","recovery_verification"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_regions (
    world_id BIGINT NOT NULL REFERENCES city_open_world_bindings(world_id) ON DELETE RESTRICT,
    region_x BIGINT NOT NULL,
    region_y BIGINT NOT NULL,
    epoch BIGINT NOT NULL DEFAULT 1 CHECK (epoch = 1),
    chunk_size SMALLINT NOT NULL DEFAULT 32 CHECK (chunk_size = 32),
    region_size_chunks SMALLINT NOT NULL DEFAULT 32 CHECK (region_size_chunks = 32),
    status VARCHAR(16) NOT NULL DEFAULT 'generated' CHECK (status = 'generated'),
    plan_hash VARCHAR(64) NOT NULL CHECK (plan_hash ~ '^[0-9a-f]{64}$'),
    generated_tick BIGINT NOT NULL DEFAULT 0 CHECK (generated_tick >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, region_x, region_y, epoch)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_regions_world_order
    ON city_open_world_regions (world_id, epoch, region_y, region_x);

CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v1', 'city-openworld-v2')
              AND world.current_tick = 0
              AND world.state_hash IS NULL
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_materialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_materialization_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v2'
              AND world.current_tick >= 0
              AND world.state_hash IS NOT NULL
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_binding_projection()
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
    RAISE EXCEPTION 'city open-world binding is immutable outside initialization'
        USING ERRCODE = '55000';
END;
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
    IF TG_OP = 'INSERT' AND (
        city_open_world_initialization_write_enabled(target_world_id)
        OR city_open_world_materialization_write_enabled(target_world_id)
    ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city open-world projection facts are immutable outside initialization or materialization'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_binding_guard ON city_open_world_bindings;
CREATE TRIGGER city_open_world_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_binding_projection();

DROP TRIGGER IF EXISTS city_open_world_region_guard ON city_open_world_regions;
CREATE TRIGGER city_open_world_region_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_regions
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

DROP TRIGGER IF EXISTS city_open_world_interior_guard ON city_open_world_building_interiors;
CREATE TRIGGER city_open_world_interior_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_building_interiors
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_projection();
