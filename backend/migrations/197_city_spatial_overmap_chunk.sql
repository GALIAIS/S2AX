-- 城市模拟 F7.1：规则集绑定、确定性 Overmap、Chunk 投影与不可变空间事实。

CREATE TABLE IF NOT EXISTS city_spatial_profiles (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    rule_set_id VARCHAR(64) NOT NULL,
    rule_set_version VARCHAR(24) NOT NULL,
    rule_set_hash VARCHAR(64) NOT NULL,
    chunk_size SMALLINT NOT NULL,
    minimum_z SMALLINT NOT NULL,
    maximum_z SMALLINT NOT NULL,
    generator_id VARCHAR(64) NOT NULL,
    generator_version VARCHAR(24) NOT NULL,
    minimum_chunk_x BIGINT NOT NULL,
    maximum_chunk_x BIGINT NOT NULL,
    minimum_chunk_y BIGINT NOT NULL,
    maximum_chunk_y BIGINT NOT NULL,
    overmap_seed_proof VARCHAR(64) NOT NULL,
    overmap_root_hash VARCHAR(64) NOT NULL,
    overmap_revision BIGINT NOT NULL DEFAULT 1,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_spatial_profile_rule_set_check CHECK (
        rule_set_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND rule_set_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND rule_set_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_spatial_profile_generator_check CHECK (
        generator_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND generator_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
    ),
    CONSTRAINT city_spatial_profile_coordinate_check CHECK (
        chunk_size = 32
        AND minimum_z BETWEEN -32 AND 127
        AND maximum_z BETWEEN minimum_z AND 127
        AND minimum_chunk_x = -4 AND maximum_chunk_x = 4
        AND minimum_chunk_y = -4 AND maximum_chunk_y = 4
    ),
    CONSTRAINT city_spatial_profile_hash_check CHECK (
        overmap_seed_proof ~ '^[0-9a-f]{64}$'
        AND overmap_root_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_spatial_profile_revision_check CHECK (overmap_revision = 1),
    CONSTRAINT city_spatial_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_overmap_tiles (
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    chunk_x BIGINT NOT NULL,
    chunk_y BIGINT NOT NULL,
    z SMALLINT NOT NULL DEFAULT 0 CHECK (z = 0),
    district_id BIGINT NOT NULL,
    terrain_definition_id VARCHAR(64) NOT NULL,
    road_mask SMALLINT NOT NULL DEFAULT 0 CHECK (road_mask BETWEEN 0 AND 15),
    river_mask SMALLINT NOT NULL DEFAULT 0 CHECK (river_mask BETWEEN 0 AND 15),
    variant SMALLINT NOT NULL DEFAULT 0 CHECK (variant BETWEEN 0 AND 3),
    tile_hash VARCHAR(64) NOT NULL CHECK (tile_hash ~ '^[0-9a-f]{64}$'),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_overmap_tiles_pk PRIMARY KEY (world_id, chunk_x, chunk_y, z),
    CONSTRAINT city_overmap_tile_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_overmap_tile_coordinate_check CHECK (
        chunk_x BETWEEN -4 AND 4 AND chunk_y BETWEEN -4 AND 4
    ),
    CONSTRAINT city_overmap_tile_terrain_check CHECK (
        terrain_definition_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
    ),
    CONSTRAINT city_overmap_tile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_overmap_tiles_world_order
    ON city_overmap_tiles (world_id, z, chunk_y, chunk_x);

CREATE TABLE IF NOT EXISTS city_spatial_mutations (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    source_command_id BIGINT NOT NULL,
    mutation_type VARCHAR(32) NOT NULL,
    expected_line_count SMALLINT NOT NULL DEFAULT 1 CHECK (expected_line_count = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    posted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_spatial_mutation_type_check CHECK (mutation_type = 'chunk_generated'),
    CONSTRAINT city_spatial_mutation_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_spatial_mutation_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_spatial_mutation_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_spatial_mutations_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_spatial_mutations_source_command_unique UNIQUE (source_command_id),
    CONSTRAINT city_spatial_mutations_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_spatial_mutations_world_cursor
    ON city_spatial_mutations (world_id, tick, sequence);

CREATE TABLE IF NOT EXISTS city_map_chunks (
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    chunk_x BIGINT NOT NULL,
    chunk_y BIGINT NOT NULL,
    z SMALLINT NOT NULL,
    district_id BIGINT NOT NULL,
    generator_id VARCHAR(64) NOT NULL,
    generator_version VARCHAR(24) NOT NULL,
    generation_proof VARCHAR(64) NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    payload JSONB NOT NULL,
    payload_hash VARCHAR(64) NOT NULL,
    generated_tick BIGINT NOT NULL CHECK (generated_tick > 0),
    source_mutation_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_map_chunks_pk PRIMARY KEY (world_id, chunk_x, chunk_y, z),
    CONSTRAINT city_map_chunk_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_map_chunk_overmap_fk
        FOREIGN KEY (world_id, chunk_x, chunk_y, z)
        REFERENCES city_overmap_tiles(world_id, chunk_x, chunk_y, z) ON DELETE RESTRICT,
    CONSTRAINT city_map_chunk_mutation_fk
        FOREIGN KEY (source_mutation_id, world_id)
        REFERENCES city_spatial_mutations(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_map_chunk_generator_check CHECK (
        generator_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND generator_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND generation_proof ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_map_chunk_payload_check CHECK (
        jsonb_typeof(payload) = 'object'
        AND payload_hash ~ '^[0-9a-f]{64}$'
        AND pg_column_size(payload) <= 262144
    ),
    CONSTRAINT city_map_chunk_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_map_chunks_world_order
    ON city_map_chunks (world_id, z, chunk_y, chunk_x);

CREATE TABLE IF NOT EXISTS city_spatial_mutation_lines (
    id BIGSERIAL PRIMARY KEY,
    mutation_id BIGINT NOT NULL,
    world_id BIGINT NOT NULL,
    line_no SMALLINT NOT NULL CHECK (line_no = 1),
    chunk_x BIGINT NOT NULL,
    chunk_y BIGINT NOT NULL,
    z SMALLINT NOT NULL,
    revision_before BIGINT NOT NULL CHECK (revision_before = 0),
    revision_after BIGINT NOT NULL CHECK (revision_after = 1),
    payload_hash_before VARCHAR(64),
    payload_hash_after VARCHAR(64) NOT NULL CHECK (payload_hash_after ~ '^[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_spatial_mutation_line_before_check CHECK (payload_hash_before IS NULL),
    CONSTRAINT city_spatial_mutation_line_mutation_fk
        FOREIGN KEY (mutation_id, world_id)
        REFERENCES city_spatial_mutations(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_spatial_mutation_lines_line_unique UNIQUE (mutation_id, line_no),
    CONSTRAINT city_spatial_mutation_lines_chunk_unique UNIQUE (mutation_id, chunk_x, chunk_y, z)
);

CREATE OR REPLACE FUNCTION city_f7_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_f7_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.current_tick = 0
              AND world.state_hash IS NULL
              AND NOT EXISTS (SELECT 1 FROM city_ticks tick WHERE tick.world_id = world.id)
       )
$$;

CREATE OR REPLACE FUNCTION city_spatial_mutation_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_spatial_mutations mutation
        WHERE mutation.id = CASE
            WHEN COALESCE(current_setting('sub2api.city_spatial_mutation_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.city_spatial_mutation_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND mutation.world_id = target_world_id
          AND mutation.posted_at IS NULL
    )
$$;

CREATE OR REPLACE FUNCTION guard_city_spatial_profile_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF city_f7_initialization_write_enabled(NEW.world_id)
           OR city_engine_upgrade_write_enabled(NEW.world_id)
           OR city_recovery_write_enabled(NEW.world_id) THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'city spatial profile can only be initialized through genesis or audited upgrade'
            USING ERRCODE = '55000';
    ELSIF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(OLD.world_id) THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'city spatial profile cannot be deleted' USING ERRCODE = '55000';
    END IF;
    IF NEW.world_id IS DISTINCT FROM OLD.world_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR NOT city_recovery_write_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city spatial profile is immutable outside audited recovery'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_spatial_profile_projection_guard ON city_spatial_profiles;
CREATE TRIGGER city_spatial_profile_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_spatial_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_spatial_profile_projection();

CREATE OR REPLACE FUNCTION guard_city_overmap_tile_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF city_f7_initialization_write_enabled(NEW.world_id)
           OR city_engine_upgrade_write_enabled(NEW.world_id)
           OR city_recovery_write_enabled(NEW.world_id) THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'city overmap tiles can only be initialized through genesis or audited upgrade'
            USING ERRCODE = '55000';
    ELSIF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(OLD.world_id) THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'city overmap tiles cannot be deleted' USING ERRCODE = '55000';
    END IF;
    IF (NEW.world_id, NEW.chunk_x, NEW.chunk_y, NEW.z, NEW.created_at)
       IS DISTINCT FROM
       (OLD.world_id, OLD.chunk_x, OLD.chunk_y, OLD.z, OLD.created_at)
       OR NOT city_recovery_write_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city overmap tiles are immutable outside audited recovery'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_overmap_tile_projection_guard ON city_overmap_tiles;
CREATE TRIGGER city_overmap_tile_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_overmap_tiles
FOR EACH ROW EXECUTE FUNCTION guard_city_overmap_tile_projection();

CREATE OR REPLACE FUNCTION guard_city_spatial_mutation_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    command_type_value VARCHAR(64);
    command_status_value VARCHAR(16);
    world_tick BIGINT;
    world_version VARCHAR(32);
BEGIN
    SELECT command_type, status INTO command_type_value, command_status_value
    FROM city_commands WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
    SELECT current_tick, simulation_version INTO world_tick, world_version
    FROM city_worlds WHERE id = NEW.world_id;
    IF command_type_value IS DISTINCT FROM 'spatial.generate_chunk'
       OR command_status_value IS DISTINCT FROM 'pending'
       OR world_version IS DISTINCT FROM 'city-f7-v1'
       OR NEW.tick IS DISTINCT FROM world_tick + 1 THEN
        RAISE EXCEPTION 'city spatial mutation does not match a pending F7 generation command'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_spatial_mutation_insert_guard ON city_spatial_mutations;
CREATE TRIGGER city_spatial_mutation_insert_guard
BEFORE INSERT ON city_spatial_mutations
FOR EACH ROW EXECUTE FUNCTION guard_city_spatial_mutation_insert();

CREATE OR REPLACE FUNCTION guard_city_spatial_mutation_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city spatial mutations are immutable facts' USING ERRCODE = '55000';
    END IF;
    IF OLD.posted_at IS NOT NULL OR NEW.posted_at IS NULL
       OR (OLD.id, OLD.world_id, OLD.tick, OLD.sequence, OLD.source_command_id,
           OLD.mutation_type, OLD.expected_line_count, OLD.metadata, OLD.created_at)
          IS DISTINCT FROM
          (NEW.id, NEW.world_id, NEW.tick, NEW.sequence, NEW.source_command_id,
           NEW.mutation_type, NEW.expected_line_count, NEW.metadata, NEW.created_at) THEN
        RAISE EXCEPTION 'city spatial mutations permit only one draft-to-posted transition'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_spatial_mutation_write_guard ON city_spatial_mutations;
CREATE TRIGGER city_spatial_mutation_write_guard
BEFORE UPDATE OR DELETE ON city_spatial_mutations
FOR EACH ROW EXECUTE FUNCTION guard_city_spatial_mutation_write();

DROP TRIGGER IF EXISTS city_spatial_mutation_line_immutable_guard ON city_spatial_mutation_lines;
CREATE TRIGGER city_spatial_mutation_line_immutable_guard
BEFORE UPDATE OR DELETE ON city_spatial_mutation_lines
FOR EACH ROW EXECUTE FUNCTION reject_city_immutable_fact_mutation();

CREATE OR REPLACE FUNCTION guard_city_map_chunk_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF city_spatial_mutation_write_enabled(NEW.world_id)
           OR city_recovery_write_enabled(NEW.world_id) THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'city map chunks can only be created through a draft spatial mutation'
            USING ERRCODE = '55000';
    ELSIF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(OLD.world_id) THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'city map chunks cannot be deleted outside audited recovery'
            USING ERRCODE = '55000';
    END IF;
    IF (NEW.world_id, NEW.chunk_x, NEW.chunk_y, NEW.z, NEW.source_mutation_id, NEW.created_at)
       IS DISTINCT FROM
       (OLD.world_id, OLD.chunk_x, OLD.chunk_y, OLD.z, OLD.source_mutation_id, OLD.created_at)
       OR NOT city_recovery_write_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city map chunks are immutable outside audited recovery'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_map_chunk_projection_guard ON city_map_chunks;
CREATE TRIGGER city_map_chunk_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_map_chunks
FOR EACH ROW EXECUTE FUNCTION guard_city_map_chunk_projection();

CREATE OR REPLACE FUNCTION assert_city_spatial_mutation_committed()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    mutation_row city_spatial_mutations%ROWTYPE;
    command_type_value VARCHAR(64);
    command_status_value VARCHAR(16);
    command_tick BIGINT;
    actual_line_count BIGINT;
    invalid_line_count BIGINT;
BEGIN
    SELECT * INTO mutation_row FROM city_spatial_mutations WHERE id = NEW.id;
    IF mutation_row.posted_at IS NULL THEN
        RAISE EXCEPTION 'city spatial mutation must be posted before commit' USING ERRCODE = '23514';
    END IF;
    SELECT command_type, status, processed_tick
      INTO command_type_value, command_status_value, command_tick
    FROM city_commands
    WHERE id = mutation_row.source_command_id AND world_id = mutation_row.world_id;
    IF command_type_value IS DISTINCT FROM 'spatial.generate_chunk'
       OR command_status_value IS DISTINCT FROM 'applied'
       OR command_tick IS DISTINCT FROM mutation_row.tick THEN
        RAISE EXCEPTION 'city spatial mutation does not match its applied command'
            USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*) INTO actual_line_count
    FROM city_spatial_mutation_lines line WHERE line.mutation_id = mutation_row.id;
    IF actual_line_count <> mutation_row.expected_line_count THEN
        RAISE EXCEPTION 'city spatial mutation line count does not match header'
            USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*) INTO invalid_line_count
    FROM city_spatial_mutation_lines line
    LEFT JOIN city_map_chunks chunk
      ON chunk.world_id = line.world_id
     AND chunk.chunk_x = line.chunk_x AND chunk.chunk_y = line.chunk_y AND chunk.z = line.z
    WHERE line.mutation_id = mutation_row.id
      AND (line.world_id <> mutation_row.world_id
           OR line.line_no <> 1
           OR line.revision_before <> 0 OR line.revision_after <> 1
           OR line.payload_hash_before IS NOT NULL
           OR chunk.source_mutation_id IS DISTINCT FROM mutation_row.id
           OR chunk.generated_tick IS DISTINCT FROM mutation_row.tick
           OR chunk.revision IS DISTINCT FROM line.revision_after
           OR chunk.payload_hash IS DISTINCT FROM line.payload_hash_after
           OR mutation_row.metadata ->> 'generation_proof'
              IS DISTINCT FROM chunk.generation_proof);
    IF invalid_line_count <> 0 THEN
        RAISE EXCEPTION 'city spatial mutation line does not match its chunk projection'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_spatial_mutation_commit_check ON city_spatial_mutations;
CREATE CONSTRAINT TRIGGER city_spatial_mutation_commit_check
AFTER INSERT OR UPDATE ON city_spatial_mutations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_spatial_mutation_committed();

CREATE OR REPLACE FUNCTION assert_city_spatial_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    profile_count BIGINT;
    tile_count BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version INTO world_version FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city spatial world does not exist' USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*) INTO profile_count FROM city_spatial_profiles WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO tile_count FROM city_overmap_tiles WHERE world_id = target_world_id;
    IF world_version = 'city-f7-v1' THEN
        IF profile_count <> 1 OR tile_count <> 81 THEN
            RAISE EXCEPTION 'city F7 spatial profile or overmap is incomplete' USING ERRCODE = '23514';
        END IF;
        SELECT COUNT(*) INTO invalid_count
        FROM city_overmap_tiles tile
        JOIN city_spatial_profiles profile ON profile.world_id = tile.world_id
        LEFT JOIN city_districts district
          ON district.id = tile.district_id AND district.world_id = tile.world_id
        WHERE tile.world_id = target_world_id
          AND (district.id IS NULL
               OR tile.chunk_x < profile.minimum_chunk_x OR tile.chunk_x > profile.maximum_chunk_x
               OR tile.chunk_y < profile.minimum_chunk_y OR tile.chunk_y > profile.maximum_chunk_y
               OR tile.z <> 0);
        IF invalid_count <> 0 THEN
            RAISE EXCEPTION 'city F7 overmap contains invalid tiles' USING ERRCODE = '23514';
        END IF;
        SELECT COUNT(*) INTO invalid_count
        FROM city_map_chunks chunk
        JOIN city_spatial_profiles profile ON profile.world_id = chunk.world_id
        LEFT JOIN city_overmap_tiles tile
          ON tile.world_id = chunk.world_id
         AND tile.chunk_x = chunk.chunk_x AND tile.chunk_y = chunk.chunk_y AND tile.z = chunk.z
        LEFT JOIN city_spatial_mutations mutation
          ON mutation.id = chunk.source_mutation_id AND mutation.world_id = chunk.world_id
        WHERE chunk.world_id = target_world_id
          AND (tile.world_id IS NULL OR mutation.posted_at IS NULL
               OR chunk.generator_id <> profile.generator_id
               OR chunk.generator_version <> profile.generator_version
               OR chunk.generated_tick <> mutation.tick);
        IF invalid_count <> 0 THEN
            RAISE EXCEPTION 'city F7 chunk projection is inconsistent' USING ERRCODE = '23514';
        END IF;
    ELSIF profile_count <> 0 OR tile_count <> 0
          OR EXISTS (SELECT 1 FROM city_map_chunks WHERE world_id = target_world_id)
          OR EXISTS (SELECT 1 FROM city_spatial_mutations WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'legacy city engine cannot contain F7 spatial state' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_spatial_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    IF TG_TABLE_NAME = 'city_worlds' THEN
        target_world_id := COALESCE(
            (to_jsonb(NEW) ->> 'id')::BIGINT,
            (to_jsonb(OLD) ->> 'id')::BIGINT
        );
    ELSE
        target_world_id := COALESCE(
            (to_jsonb(NEW) ->> 'world_id')::BIGINT,
            (to_jsonb(OLD) ->> 'world_id')::BIGINT
        );
    END IF;
    PERFORM assert_city_spatial_foundation(target_world_id);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_spatial_profile_commit_check ON city_spatial_profiles;
CREATE CONSTRAINT TRIGGER city_spatial_profile_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_spatial_profiles
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_spatial_foundation();

DROP TRIGGER IF EXISTS city_overmap_tile_commit_check ON city_overmap_tiles;
CREATE CONSTRAINT TRIGGER city_overmap_tile_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_overmap_tiles
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_spatial_foundation();

DROP TRIGGER IF EXISTS city_map_chunk_commit_check ON city_map_chunks;
CREATE CONSTRAINT TRIGGER city_map_chunk_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_map_chunks
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_spatial_foundation();

DROP TRIGGER IF EXISTS city_spatial_world_commit_check ON city_worlds;
CREATE CONSTRAINT TRIGGER city_spatial_world_commit_check
AFTER INSERT OR UPDATE ON city_worlds
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_spatial_foundation();

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f7-v1', 'supported', 'city-state-v1+gzip',
        '["control","ledger","resources","calendar_demography","population_migration","household_lifecycle","spatial","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f6-v3', 'city-f7-v1', 'f6_v3_to_f7_v1')
ON CONFLICT (from_version, to_version) DO NOTHING;
