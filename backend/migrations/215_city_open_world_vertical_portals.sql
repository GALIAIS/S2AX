-- V2 vertical topology.  Surface chunks remain sparse Z=0 projections, but
-- every generated building floor and its entrance/stair edges are immutable
-- facts.  Runtime access state is intentionally not stored here: a future
-- door lock or permission policy must be an auditable dynamic projection.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v3', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","markets","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v1', 'city-openworld-v2', 'city-openworld-v3')
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
              AND world.simulation_version IN ('city-openworld-v2', 'city-openworld-v3')
              AND world.current_tick >= 0
              AND world.state_hash IS NOT NULL
       )
$$;

CREATE TABLE IF NOT EXISTS city_open_world_portals (
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_portal_code_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,127}$'
    ),
    CONSTRAINT city_open_world_portal_building_fk
        FOREIGN KEY (world_id, building_code)
        REFERENCES city_open_world_buildings(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_portal_shape_check CHECK (
        (portal_type = 'entrance'
            AND from_floor_index = 0 AND to_floor_index = 0
            AND from_z = 0 AND to_z = 0)
        OR
        (portal_type = 'stairs'
            AND to_floor_index = from_floor_index + 1
            AND to_z = from_z + 1)
    )
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_portals_building
    ON city_open_world_portals (world_id, building_code, from_floor_index, to_floor_index, code);

DROP TRIGGER IF EXISTS city_open_world_portal_guard ON city_open_world_portals;
CREATE TRIGGER city_open_world_portal_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_portals
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_projection();
