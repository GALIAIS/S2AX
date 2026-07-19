-- Immutable ground-floor interiors for the V2 open-world genesis path.
-- Each record is an independently hashed C:DDA-style local plan. The chunk
-- projection already carries the same renderable facts; this table keeps the
-- complete authored floor plan available for future vertical traversal,
-- navigation, and audits without regenerating it in the client.

CREATE TABLE IF NOT EXISTS city_open_world_building_interiors (
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, building_code, floor_index),
    CONSTRAINT city_open_world_interior_building_fk
        FOREIGN KEY (world_id, building_code)
        REFERENCES city_open_world_buildings(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_interior_identity_check CHECK (
        layout_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND layout_style ~ '^[a-z][a-z0-9_.-]{1,63}$'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_interiors_world_building
    ON city_open_world_building_interiors (world_id, building_code, floor_index);

DROP TRIGGER IF EXISTS city_open_world_interior_guard ON city_open_world_building_interiors;
CREATE TRIGGER city_open_world_interior_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_building_interiors
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_projection();
