-- F7.10 dynamic portal state and declarative actor access control.

CREATE TABLE IF NOT EXISTS world_portal_states (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    portal_id BIGINT NOT NULL,
    state_code VARCHAR(16) NOT NULL CHECK (state_code IN ('open', 'closed', 'locked')),
    access_requirement JSONB NOT NULL,
    access_policy_hash VARCHAR(64) NOT NULL CHECK (access_policy_hash ~ '^[0-9a-f]{64}$'),
    changed_tick BIGINT NOT NULL CHECK (changed_tick >= 0),
    source_fact_id BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_portal_state_portal_fk
        FOREIGN KEY (portal_id, world_id)
        REFERENCES city_building_portals(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT world_portal_state_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_portal_state_requirement_check
        CHECK (jsonb_typeof(access_requirement) = 'object'),
    CONSTRAINT world_portal_state_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT world_portal_states_world_portal_unique UNIQUE (world_id, portal_id),
    CONSTRAINT world_portal_states_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_world_portal_states_world_state
    ON world_portal_states (world_id, state_code, portal_id);
CREATE INDEX IF NOT EXISTS idx_world_portal_states_source_fact
    ON world_portal_states (source_fact_id)
    WHERE source_fact_id IS NOT NULL;

-- Extend every frozen predecessor foundation only by its explicit F7.10
-- compatibility set. Any unexpected predecessor definition aborts migration.
CREATE OR REPLACE FUNCTION migration_205_replace_function(
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
        RAISE EXCEPTION 'migration 205 predecessor function % does not contain expected text', target
            USING ERRCODE = '23514';
    END IF;
    BEGIN
        EXECUTE patched;
    EXCEPTION WHEN OTHERS THEN
        RAISE EXCEPTION 'migration 205 failed to extend predecessor function % at %: %',
            target, needle, SQLERRM
            USING ERRCODE = SQLSTATE;
    END;
END;
$migration$;

SELECT migration_205_replace_function(
    'world_runtime_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
    $$world.simulation_version IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$,
    $$world.simulation_version IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$
);
SELECT migration_205_replace_function(
    'guard_world_runtime_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$
);
SELECT migration_205_replace_function(
    'assert_world_runtime_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$
);
SELECT migration_205_replace_function(
    'assert_world_runtime_foundation(bigint)'::REGPROCEDURE,
    $$CASE WHEN world_version IN ('city-f7-v6', 'city-f7-v7') THEN '1.1.0' ELSE '1.0.0' END$$,
    $$CASE WHEN world_version = 'city-f7-v8' THEN '1.2.0' WHEN world_version IN ('city-f7-v6', 'city-f7-v7') THEN '1.1.0' ELSE '1.0.0' END$$
);
SELECT migration_205_replace_function(
    'guard_city_enterprise_location_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$
);
SELECT migration_205_replace_function(
    'assert_city_enterprise_location_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$
);
SELECT migration_205_replace_function(
    'guard_city_development_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$
);
SELECT migration_205_replace_function(
    'assert_city_development_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$
);
SELECT migration_205_replace_function(
    'assert_city_land_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$,
    $$world_version NOT IN ('city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$
);
SELECT migration_205_replace_function(
    'guard_city_spatial_mutation_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$,
    $$world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$
);
SELECT migration_205_replace_function(
    'assert_city_spatial_foundation(bigint)'::REGPROCEDURE,
    $$world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$,
    $$world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$
);
SELECT migration_205_replace_function(
    'assert_world_actor_spatial_control_foundation(bigint)'::REGPROCEDURE,
    $$IF world_version NOT IN ('city-f7-v6', 'city-f7-v7') THEN$$,
    $$IF world_version NOT IN ('city-f7-v6', 'city-f7-v7', 'city-f7-v8') THEN$$
);
SELECT migration_205_replace_function(
    'assert_world_actor_spatial_control_foundation(bigint)'::REGPROCEDURE,
    $$AND runtime_version = '1.1.0'$$,
    $$AND runtime_version = CASE WHEN world_version = 'city-f7-v8' THEN '1.2.0' ELSE '1.1.0' END$$
);

DROP FUNCTION migration_205_replace_function(REGPROCEDURE, TEXT, TEXT);

CREATE OR REPLACE FUNCTION guard_world_portal_state_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    IF world_runtime_bootstrap_write_enabled(target_world_id) THEN
        IF TG_OP <> 'INSERT' THEN
            RAISE EXCEPTION 'world portal bootstrap permits inserts only' USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    IF world_runtime_fact_write_enabled(target_world_id) THEN
        IF TG_OP <> 'UPDATE'
           OR NEW.id IS DISTINCT FROM OLD.id
           OR NEW.world_id IS DISTINCT FROM OLD.world_id
           OR NEW.portal_id IS DISTINCT FROM OLD.portal_id
           OR NEW.created_at IS DISTINCT FROM OLD.created_at
           OR NEW.version <> OLD.version + 1
           OR NEW.changed_tick < OLD.changed_tick
           OR NEW.source_fact_id IS NULL
           OR NEW.source_fact_id IS NOT DISTINCT FROM OLD.source_fact_id THEN
            RAISE EXCEPTION 'invalid fact-backed world portal projection update'
                USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'world portal projection requires bootstrap, draft fact, or recovery context'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS world_portal_state_projection_guard ON world_portal_states;
CREATE TRIGGER world_portal_state_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_portal_states
FOR EACH ROW EXECUTE FUNCTION guard_world_portal_state_projection();

CREATE OR REPLACE FUNCTION assert_world_portal_access_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    portal_count BIGINT;
    state_count BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'world portal-access world does not exist' USING ERRCODE = '23514';
    END IF;

    IF world_version <> 'city-f7-v8' THEN
        IF EXISTS (SELECT 1 FROM world_portal_states WHERE world_id = target_world_id) THEN
            RAISE EXCEPTION 'pre-F7.10 engine cannot contain dynamic portal state'
                USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM world_runtime_profiles
        WHERE world_id = target_world_id
          AND runtime_id = 'sub2api-open-world-runtime'
          AND runtime_version = '1.2.0'
    ) THEN
        RAISE EXCEPTION 'world portal-access runtime profile is missing or invalid'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO portal_count
    FROM city_building_portals portal
    JOIN city_buildings building
      ON building.id = portal.building_id AND building.world_id = portal.world_id
    WHERE portal.world_id = target_world_id
      AND portal.status = 'active' AND building.status = 'active';
    SELECT COUNT(*) INTO state_count
    FROM world_portal_states WHERE world_id = target_world_id;
    IF state_count <> portal_count THEN
        RAISE EXCEPTION 'every active F7.10 portal must have exactly one dynamic state'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM world_portal_states value
    JOIN city_building_portals portal
      ON portal.id = value.portal_id AND portal.world_id = value.world_id
    JOIN city_buildings building
      ON building.id = portal.building_id AND building.world_id = portal.world_id
    LEFT JOIN world_runtime_facts source
      ON source.id = value.source_fact_id AND source.world_id = value.world_id
    LEFT JOIN world_effect_operations effect
      ON effect.source_fact_id = source.id
     AND effect.effect_type = CASE source.fact_type
            WHEN 'portal.state.changed' THEN 'portal.state.set'
            WHEN 'portal.access.changed' THEN 'portal.access.set'
            ELSE NULL
         END
    WHERE value.world_id = target_world_id
      AND (portal.status <> 'active' OR building.status <> 'active'
           OR value.changed_tick > world_tick
           OR (portal.portal_type = 'stair' AND value.state_code <> 'open')
           OR (value.source_fact_id IS NULL AND
               (value.version <> 1 OR value.metadata->>'source' IS DISTINCT FROM 'baseline'))
           OR (value.source_fact_id IS NOT NULL AND
               (value.version <= 1 OR source.id IS NULL OR source.posted_at IS NULL
                OR source.tick <> value.changed_tick
                OR source.fact_type NOT IN ('portal.state.changed', 'portal.access.changed')
                OR (source.fact_type = 'portal.state.changed' AND source.actor_id IS NULL)
                OR effect.id IS NULL OR effect.tick <> source.tick
                OR effect.executor_version <> '1.2.0'
                OR effect.target_key <> building.code || '.' || portal.code
                OR effect.before_units <> value.version - 1
                OR effect.delta_units <> 1 OR effect.after_units <> value.version
                OR effect.payload#>>'{portal_after,building_code}' <> building.code
                OR effect.payload#>>'{portal_after,portal_code}' <> portal.code
                OR effect.payload#>>'{portal_after,state_code}' <> value.state_code
                OR effect.payload#>>'{portal_after,access_policy_hash}' <> value.access_policy_hash
                OR (effect.payload#>'{portal_after,access_requirement}') IS DISTINCT FROM value.access_requirement)));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'world portal state projection is inconsistent'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_world_portal_access_foundation()
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
    PERFORM assert_world_portal_access_foundation(target_world_id);
    RETURN NULL;
END;
$$;

DO $$
DECLARE
    table_name TEXT;
    trigger_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'world_portal_states',
        'city_building_portals',
        'world_runtime_profiles',
        'world_runtime_facts',
        'world_effect_operations'
    ] LOOP
        trigger_name := table_name || '_portal_access_commit_check';
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', trigger_name, table_name);
        EXECUTE format(
            'CREATE CONSTRAINT TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I '
            || 'DEFERRABLE INITIALLY DEFERRED FOR EACH ROW '
            || 'EXECUTE FUNCTION check_world_portal_access_foundation()',
            trigger_name, table_name
        );
    END LOOP;
END;
$$;

DROP TRIGGER IF EXISTS city_world_portal_access_commit_check ON city_worlds;
CREATE CONSTRAINT TRIGGER city_world_portal_access_commit_check
AFTER INSERT OR UPDATE OF simulation_version, current_tick ON city_worlds
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_world_portal_access_foundation();

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f7-v8', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","spatial","development","enterprise_location","world_runtime","actor_spatial_control","actor_navigation","portal_access","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f7-v7', 'city-f7-v8', 'f7_v7_to_f7_v8')
ON CONFLICT (from_version, to_version) DO NOTHING;
