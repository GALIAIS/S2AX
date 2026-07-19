-- 开放世界模拟 F7.7：Actor 权威位置、可委托控制权与空间作用域规则。

CREATE TABLE IF NOT EXISTS world_actor_locations (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    actor_id BIGINT NOT NULL,
    space_kind VARCHAR(32) NOT NULL,
    space_code VARCHAR(128) NOT NULL,
    x BIGINT NOT NULL,
    y BIGINT NOT NULL,
    z INTEGER NOT NULL,
    chunk_x BIGINT NOT NULL,
    chunk_y BIGINT NOT NULL,
    local_x INTEGER NOT NULL,
    local_y INTEGER NOT NULL,
    anchor_kind VARCHAR(24),
    anchor_code VARCHAR(192),
    jurisdiction_code VARCHAR(32) NOT NULL,
    moved_tick BIGINT NOT NULL CHECK (moved_tick >= 0),
    source_fact_id BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_actor_location_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT world_actor_location_jurisdiction_fk
        FOREIGN KEY (world_id, jurisdiction_code)
        REFERENCES city_districts(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT world_actor_location_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_actor_location_space_check CHECK (
        space_kind ~ '^[a-z][a-z0-9_]{1,31}$'
        AND space_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
    ),
    CONSTRAINT world_actor_location_anchor_check CHECK (
        (anchor_kind IS NULL AND anchor_code IS NULL)
        OR (anchor_kind IN ('chunk', 'building', 'site')
            AND anchor_code ~ '^[a-z][a-z0-9_.-]{1,191}$')
    ),
    CONSTRAINT world_actor_location_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT world_actor_locations_world_actor_unique UNIQUE (world_id, actor_id),
    CONSTRAINT world_actor_locations_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_world_actor_locations_chunk
    ON world_actor_locations (world_id, chunk_x, chunk_y, z, actor_id);
CREATE INDEX IF NOT EXISTS idx_world_actor_locations_jurisdiction
    ON world_actor_locations (world_id, jurisdiction_code, actor_id);
CREATE INDEX IF NOT EXISTS idx_world_actor_locations_anchor
    ON world_actor_locations (world_id, anchor_kind, anchor_code, actor_id);

CREATE TABLE IF NOT EXISTS world_actor_control_grants (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    actor_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    capability VARCHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'revoked')),
    granted_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    granted_tick BIGINT NOT NULL CHECK (granted_tick >= 0),
    revoked_tick BIGINT,
    grant_source_fact_id BIGINT,
    revoke_source_fact_id BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_actor_control_grant_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT world_actor_control_grant_source_fact_fk
        FOREIGN KEY (grant_source_fact_id, world_id)
        REFERENCES world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_actor_control_revoke_source_fact_fk
        FOREIGN KEY (revoke_source_fact_id, world_id)
        REFERENCES world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_actor_control_grant_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND capability IN ('actor.command', 'actor.control.manage')
    ),
    CONSTRAINT world_actor_control_grant_lifecycle_check CHECK (
        (status = 'active' AND revoked_tick IS NULL AND revoke_source_fact_id IS NULL)
        OR (status = 'revoked' AND revoked_tick IS NOT NULL
            AND revoked_tick >= granted_tick AND revoke_source_fact_id IS NOT NULL)
    ),
    CONSTRAINT world_actor_control_grant_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT world_actor_control_grants_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT world_actor_control_grants_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_world_actor_control_grants_active
    ON world_actor_control_grants (world_id, actor_id, user_id, capability)
    WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_world_actor_control_grants_authorization
    ON world_actor_control_grants (world_id, user_id, capability, status, actor_id);
CREATE INDEX IF NOT EXISTS idx_world_actor_control_grants_history
    ON world_actor_control_grants (world_id, actor_id, granted_tick, code);

-- Every previous F7 foundation remains frozen. Extend only its explicit compatible
-- engine set; fail the migration if the expected predecessor definition differs.
CREATE OR REPLACE FUNCTION migration_202_replace_function(
    target REGPROCEDURE,
    needle TEXT,
    replacement TEXT
)
RETURNS VOID
LANGUAGE plpgsql
AS $migration$
DECLARE
    definition TEXT;
    patched TEXT;
BEGIN
    SELECT pg_get_functiondef(target) INTO definition;
    patched := replace(definition, needle, replacement);
    IF patched = definition THEN
        IF POSITION(replacement IN definition) > 0 THEN
            RETURN;
        END IF;
        RAISE EXCEPTION 'migration 202 predecessor function % does not contain expected text', target
            USING ERRCODE = '23514';
    END IF;
    BEGIN
        EXECUTE patched;
    EXCEPTION WHEN OTHERS THEN
        RAISE EXCEPTION 'migration 202 failed to extend predecessor function % at %: %',
            target, needle, SQLERRM
            USING ERRCODE = SQLSTATE;
    END;
END;
$migration$;

SELECT migration_202_replace_function(
    'world_runtime_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
    $$world.simulation_version = 'city-f7-v5'$$,
    $$world.simulation_version IN ('city-f7-v5', 'city-f7-v6')$$
);
SELECT migration_202_replace_function(
    'guard_world_runtime_fact_insert()'::REGPROCEDURE,
    $$world_version IS DISTINCT FROM 'city-f7-v5'$$,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6')$$
);
SELECT migration_202_replace_function(
    'assert_world_runtime_foundation(bigint)'::REGPROCEDURE,
    $$world_version <> 'city-f7-v5'$$,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6')$$
);
SELECT migration_202_replace_function(
    'assert_world_runtime_foundation(bigint)'::REGPROCEDURE,
    $$profile_row.runtime_version <> '1.0.0'$$,
    $$profile_row.runtime_version <> (CASE world_version WHEN 'city-f7-v6' THEN '1.1.0' ELSE '1.0.0' END)$$
);
SELECT migration_202_replace_function(
    'guard_city_enterprise_location_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5')$$,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$
);
SELECT migration_202_replace_function(
    'assert_city_enterprise_location_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5')$$,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$
);
SELECT migration_202_replace_function(
    'guard_city_development_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5')$$,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$
);
SELECT migration_202_replace_function(
    'assert_city_development_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5')$$,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$
);
SELECT migration_202_replace_function(
    'assert_city_land_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5')$$,
    $$world_version NOT IN ('city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$
);
SELECT migration_202_replace_function(
    'guard_city_spatial_mutation_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5')$$,
    $$world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$
);
SELECT migration_202_replace_function(
    'assert_city_spatial_foundation(bigint)'::REGPROCEDURE,
    $$world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5')$$,
    $$world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$
);

DROP FUNCTION migration_202_replace_function(REGPROCEDURE, TEXT, TEXT);

CREATE OR REPLACE FUNCTION guard_world_actor_spatial_control_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id)
       OR world_runtime_bootstrap_write_enabled(target_world_id)
       OR world_runtime_fact_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'world actor spatial-control projection requires bootstrap, draft fact, or recovery context'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS world_actor_location_projection_guard ON world_actor_locations;
CREATE TRIGGER world_actor_location_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_actor_locations
FOR EACH ROW EXECUTE FUNCTION guard_world_actor_spatial_control_projection();

DROP TRIGGER IF EXISTS world_actor_control_grant_projection_guard ON world_actor_control_grants;
CREATE TRIGGER world_actor_control_grant_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_actor_control_grants
FOR EACH ROW EXECUTE FUNCTION guard_world_actor_spatial_control_projection();

CREATE OR REPLACE FUNCTION assert_world_actor_spatial_control_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    actor_count BIGINT;
    location_count BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'world actor spatial-control world does not exist' USING ERRCODE = '23514';
    END IF;

    IF world_version <> 'city-f7-v6' THEN
        IF EXISTS (SELECT 1 FROM world_actor_locations WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM world_actor_control_grants WHERE world_id = target_world_id) THEN
            RAISE EXCEPTION 'pre-F7.7 engine cannot contain actor spatial-control state'
                USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM world_runtime_profiles
        WHERE world_id = target_world_id
          AND runtime_id = 'sub2api-open-world-runtime'
          AND runtime_version = '1.1.0'
    ) THEN
        RAISE EXCEPTION 'world actor spatial-control runtime profile is missing or invalid'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO actor_count FROM world_actors WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO location_count FROM world_actor_locations WHERE world_id = target_world_id;
    IF location_count <> actor_count THEN
        RAISE EXCEPTION 'every F7.7 actor must have exactly one authoritative location'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM world_actor_locations location
    JOIN world_actors actor
      ON actor.id = location.actor_id AND actor.world_id = location.world_id
    JOIN city_spatial_profiles profile ON profile.world_id = location.world_id
    LEFT JOIN city_overmap_tiles tile
      ON tile.world_id = location.world_id AND tile.chunk_x = location.chunk_x
     AND tile.chunk_y = location.chunk_y AND tile.z = 0
    LEFT JOIN city_districts district
      ON district.id = tile.district_id AND district.world_id = tile.world_id
    LEFT JOIN world_runtime_facts source_fact
      ON source_fact.id = location.source_fact_id AND source_fact.world_id = location.world_id
    WHERE location.world_id = target_world_id
      AND (location.space_kind <> 'city_grid' OR location.space_code <> 'primary'
           OR location.chunk_x <> floor(location.x::NUMERIC / profile.chunk_size)::BIGINT
           OR location.chunk_y <> floor(location.y::NUMERIC / profile.chunk_size)::BIGINT
           OR location.local_x <> location.x - location.chunk_x * profile.chunk_size
           OR location.local_y <> location.y - location.chunk_y * profile.chunk_size
           OR location.local_x < 0 OR location.local_x >= profile.chunk_size
           OR location.local_y < 0 OR location.local_y >= profile.chunk_size
           OR location.chunk_x < profile.minimum_chunk_x OR location.chunk_x > profile.maximum_chunk_x
           OR location.chunk_y < profile.minimum_chunk_y OR location.chunk_y > profile.maximum_chunk_y
           OR location.z < profile.minimum_z OR location.z > profile.maximum_z
           OR district.id IS NULL OR district.code <> location.jurisdiction_code
           OR location.moved_tick > world_tick
           OR (location.source_fact_id IS NULL AND location.metadata->>'source' IS DISTINCT FROM 'baseline')
           OR (location.source_fact_id IS NOT NULL AND
               (source_fact.id IS NULL OR source_fact.posted_at IS NULL
                OR source_fact.actor_id <> location.actor_id
                OR source_fact.tick <> location.moved_tick
                OR source_fact.fact_type NOT IN ('actor.created', 'actor.location.moved')))
           OR CASE location.anchor_kind
                WHEN 'chunk' THEN location.z <> 0 OR location.anchor_code <>
                    format('chunk.z%s.x%s.y%s', location.z, location.chunk_x, location.chunk_y)
                WHEN 'building' THEN NOT EXISTS (
                    SELECT 1 FROM city_buildings building
                    WHERE building.world_id = location.world_id AND building.code = location.anchor_code
                      AND location.x BETWEEN building.chunk_x * profile.chunk_size + building.local_min_x
                                         AND building.chunk_x * profile.chunk_size + building.local_max_x
                      AND location.y BETWEEN building.chunk_y * profile.chunk_size + building.local_min_y
                                         AND building.chunk_y * profile.chunk_size + building.local_max_y
                      AND location.z BETWEEN building.base_z AND building.top_z
                      AND building.district_id = district.id
                )
                WHEN 'site' THEN NOT EXISTS (
                    SELECT 1
                    FROM city_enterprise_sites site
                    JOIN city_buildings building
                      ON building.id = site.building_id AND building.world_id = site.world_id
                    WHERE site.world_id = location.world_id AND site.code = location.anchor_code
                      AND site.status = 'active'
                      AND location.x BETWEEN building.chunk_x * profile.chunk_size + building.local_min_x
                                         AND building.chunk_x * profile.chunk_size + building.local_max_x
                      AND location.y BETWEEN building.chunk_y * profile.chunk_size + building.local_min_y
                                         AND building.chunk_y * profile.chunk_size + building.local_max_y
                      AND location.z BETWEEN building.base_z AND building.top_z
                      AND site.district_id = district.id
                )
                ELSE TRUE
              END);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'world actor authoritative location projection is inconsistent'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM world_actor_control_grants value
    JOIN world_actors actor
      ON actor.id = value.actor_id AND actor.world_id = value.world_id
    LEFT JOIN world_runtime_facts grant_fact
      ON grant_fact.id = value.grant_source_fact_id AND grant_fact.world_id = value.world_id
    LEFT JOIN world_runtime_facts revoke_fact
      ON revoke_fact.id = value.revoke_source_fact_id AND revoke_fact.world_id = value.world_id
    WHERE value.world_id = target_world_id
      AND ((value.grant_source_fact_id IS NULL AND
            (value.metadata->>'source' IS DISTINCT FROM 'baseline'
             OR actor.owner_user_id IS DISTINCT FROM value.user_id
             OR value.granted_by_user_id <> value.user_id))
           OR (value.grant_source_fact_id IS NOT NULL AND
               (grant_fact.id IS NULL OR grant_fact.posted_at IS NULL
                OR grant_fact.actor_id <> value.actor_id OR grant_fact.tick <> value.granted_tick
                OR grant_fact.fact_type NOT IN ('actor.created', 'actor.control.granted')))
           OR (value.status = 'revoked' AND
               (revoke_fact.id IS NULL OR revoke_fact.posted_at IS NULL
                OR revoke_fact.actor_id <> value.actor_id OR revoke_fact.tick <> value.revoked_tick
                OR revoke_fact.fact_type <> 'actor.control.revoked')));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'world actor control grant projection is inconsistent'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM world_actors actor
    CROSS JOIN (VALUES ('actor.command'::VARCHAR), ('actor.control.manage'::VARCHAR)) capability(code)
    LEFT JOIN world_actor_control_grants value
      ON value.world_id = actor.world_id AND value.actor_id = actor.id
     AND value.user_id = actor.owner_user_id AND value.capability = capability.code
     AND value.status = 'active'
    WHERE actor.world_id = target_world_id AND actor.owner_user_id IS NOT NULL
      AND value.id IS NULL;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'world actor owner must retain command and control-management capabilities'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_world_actor_spatial_control_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(
        NULLIF(to_jsonb(NEW)->>'world_id', '')::BIGINT,
        NULLIF(to_jsonb(OLD)->>'world_id', '')::BIGINT,
        NULLIF(to_jsonb(NEW)->>'id', '')::BIGINT,
        NULLIF(to_jsonb(OLD)->>'id', '')::BIGINT
    );
    PERFORM assert_world_runtime_foundation(target_world_id);
    PERFORM assert_world_actor_spatial_control_foundation(target_world_id);
    RETURN NULL;
END;
$$;

DO $$
DECLARE
    table_name TEXT;
    trigger_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'world_actor_locations',
        'world_actor_control_grants',
        'world_actors',
        'world_runtime_profiles'
    ] LOOP
        trigger_name := table_name || '_spatial_control_commit_check';
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', trigger_name, table_name);
        EXECUTE format(
            'CREATE CONSTRAINT TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I '
            || 'DEFERRABLE INITIALLY DEFERRED FOR EACH ROW '
            || 'EXECUTE FUNCTION check_world_actor_spatial_control_foundation()',
            trigger_name, table_name
        );
    END LOOP;
END;
$$;

DROP TRIGGER IF EXISTS city_world_spatial_control_commit_check ON city_worlds;
CREATE CONSTRAINT TRIGGER city_world_spatial_control_commit_check
AFTER INSERT OR UPDATE OF simulation_version, current_tick ON city_worlds
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_world_actor_spatial_control_foundation();

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f7-v6', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","spatial","development","enterprise_location","world_runtime","actor_spatial_control","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f7-v5', 'city-f7-v6', 'f7_v5_to_f7_v6')
ON CONFLICT (from_version, to_version) DO NOTHING;
