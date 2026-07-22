-- city-openworld-v6 freezes the complete immutable version vector used by a
-- newly created world.  It extends V5's independently-owned open-world
-- topology/runtime and does not retrofit or mutate any historical snapshot.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v6', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","version_vector","facilities","npc_lod","activities","rules","case_lifecycle","rewards","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v5', 'city-openworld-v6', 'openworld_v5_to_v6')
ON CONFLICT (from_version, to_version) DO NOTHING;

ALTER TABLE city_worlds
    ADD COLUMN IF NOT EXISTS version_vector_generation SMALLINT NOT NULL DEFAULT 0
    CHECK (version_vector_generation >= 0);

-- The vector has a header because an explicit engine upgrade creates a new
-- immutable baseline rather than overwriting the V6 contract it supersedes.
CREATE TABLE IF NOT EXISTS city_world_version_vectors (
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    generation SMALLINT NOT NULL CHECK (generation > 0),
    engine_version VARCHAR(32) NOT NULL CHECK (engine_version ~ '^city-openworld-v[0-9]+$'),
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    source_upgrade_run_id BIGINT REFERENCES city_world_upgrade_runs(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, generation)
);

CREATE TABLE IF NOT EXISTS city_world_version_bindings (
    world_id BIGINT NOT NULL,
    generation SMALLINT NOT NULL,
    component_code VARCHAR(32) NOT NULL,
    bundle_id VARCHAR(128) NOT NULL,
    bundle_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, generation, component_code),
    FOREIGN KEY (world_id, generation)
        REFERENCES city_world_version_vectors(world_id, generation) ON DELETE RESTRICT,
    CONSTRAINT city_world_version_binding_component_check CHECK (component_code IN (
        'content_catalog', 'economic_policy', 'engine', 'rule_bundle',
        'scenario', 'spatial_profile', 'worldgen_plan'
    )),
    CONSTRAINT city_world_version_binding_identity_check CHECK (
        bundle_id ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND bundle_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND content_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_world_version_binding_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_world_version_bindings_world
    ON city_world_version_bindings (world_id, generation, component_code);

CREATE OR REPLACE FUNCTION city_world_version_vector_write_enabled(
    target_world_id BIGINT,
    target_generation SMALLINT
)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_world_version_binding_world_id', TRUE), '') = target_world_id::TEXT
       AND COALESCE(current_setting('sub2api.city_world_version_binding_generation', TRUE), '') = target_generation::TEXT
       AND EXISTS (
            SELECT 1
            FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.version_vector_generation = target_generation
              AND world.simulation_version ~ '^city-openworld-v[0-9]+$'
              AND world.state_hash IS NULL
              AND (
                  (world.current_tick = 0 AND target_generation = 1)
                  OR EXISTS (
                      SELECT 1
                      FROM city_world_upgrade_runs upgrade
                      WHERE upgrade.id = CASE
                          WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                          THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT
                          ELSE NULL
                      END
                        AND upgrade.world_id = target_world_id
                        AND upgrade.to_version = world.simulation_version
                        AND upgrade.status = 'running'
                  )
              )
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_world_version_vector()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_world_version_vector_write_enabled(NEW.world_id, NEW.generation) THEN
        RAISE EXCEPTION 'city world version vectors are immutable outside genesis or audited upgrades'
            USING ERRCODE = '55000';
    END IF;
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = NEW.world_id;
    IF NEW.engine_version IS DISTINCT FROM world_version OR NEW.baseline_tick IS DISTINCT FROM world_tick THEN
        RAISE EXCEPTION 'city world version vector header does not match active world baseline'
            USING ERRCODE = '23514';
    END IF;
    IF (NEW.source_upgrade_run_id IS NULL AND (NEW.generation <> 1 OR world_tick <> 0))
       OR (NEW.source_upgrade_run_id IS NOT NULL AND NOT EXISTS (
            SELECT 1 FROM city_world_upgrade_runs upgrade
            WHERE upgrade.id = NEW.source_upgrade_run_id
              AND upgrade.world_id = NEW.world_id
              AND upgrade.to_version = NEW.engine_version
              AND upgrade.status = 'running'
       )) THEN
        RAISE EXCEPTION 'city world version vector source upgrade is invalid'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_world_version_vector_guard ON city_world_version_vectors;
CREATE TRIGGER city_world_version_vector_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_world_version_vectors
FOR EACH ROW EXECUTE FUNCTION guard_city_world_version_vector();

CREATE OR REPLACE FUNCTION guard_city_world_version_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
    target_generation SMALLINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    target_generation := COALESCE(NEW.generation, OLD.generation);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_world_version_vector_write_enabled(target_world_id, target_generation) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city world version bindings are immutable outside genesis or audited V5-to-V6 upgrade'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_world_version_binding_guard ON city_world_version_bindings;
CREATE TRIGGER city_world_version_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_world_version_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_world_version_binding();

CREATE OR REPLACE FUNCTION assert_city_world_version_vector(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    vector_generation SMALLINT;
    vector_version VARCHAR(32);
BEGIN
    SELECT simulation_version, version_vector_generation INTO world_version, vector_generation
    FROM city_worlds WHERE id = target_world_id;
    IF world_version !~ '^city-openworld-v[0-9]+$' OR vector_generation < 1 THEN
        RAISE EXCEPTION 'city world version vector only applies to versioned open worlds'
            USING ERRCODE = '23514';
    END IF;
    SELECT engine_version INTO vector_version
    FROM city_world_version_vectors
    WHERE world_id = target_world_id AND generation = vector_generation;
    IF vector_version IS DISTINCT FROM world_version THEN
        RAISE EXCEPTION 'city world active version vector header is missing or stale'
            USING ERRCODE = '23514';
    END IF;
    IF (SELECT COUNT(*) FROM city_world_version_bindings
        WHERE world_id = target_world_id AND generation = vector_generation) <> 7 THEN
        RAISE EXCEPTION 'city open-world V6 requires exactly seven version-vector bindings'
            USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM (VALUES
            ('content_catalog'), ('economic_policy'), ('engine'), ('rule_bundle'),
            ('scenario'), ('spatial_profile'), ('worldgen_plan')
        ) AS required(component_code)
        WHERE NOT EXISTS (
            SELECT 1 FROM city_world_version_bindings binding
            WHERE binding.world_id = target_world_id
              AND binding.generation = vector_generation
              AND binding.component_code = required.component_code
        )
    ) THEN
        RAISE EXCEPTION 'city open-world V6 version vector is incomplete'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

-- V6 retains the immutable V3 static generator contract.  The guards are
-- widened only for the new engine version; existing V1–V5 facts are untouched.
CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN (
                  'city-openworld-v1', 'city-openworld-v2', 'city-openworld-v3',
                  'city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6'
              )
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
              AND world.simulation_version IN (
                  'city-openworld-v2', 'city-openworld-v3', 'city-openworld-v4',
                  'city-openworld-v5', 'city-openworld-v6'
              )
              AND world.current_tick >= 0
              AND world.state_hash IS NOT NULL
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6')
              AND world.current_tick = 0
              AND world.state_hash IS NULL
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_fact_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6')
              AND world.current_tick >= 0
              AND world.state_hash IS NOT NULL
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_fact_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    world_tick BIGINT;
    world_version VARCHAR(32);
    command_status_value VARCHAR(16);
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version
    FROM city_worlds WHERE id = NEW.world_id;
    IF world_version NOT IN ('city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6')
       OR NEW.tick IS DISTINCT FROM world_tick + 1
       OR NEW.posted_at IS NOT NULL THEN
        RAISE EXCEPTION 'open-world runtime fact must be a draft for the next V4/V5/V6 tick'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.source_command_id IS NOT NULL THEN
        SELECT status INTO command_status_value
        FROM city_commands
        WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
        IF command_status_value IS DISTINCT FROM 'pending' THEN
            RAISE EXCEPTION 'open-world runtime fact requires a pending source command'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF NEW.source_command_id IS NULL AND NEW.parent_fact_id IS NULL
       AND NOT (world_version IN ('city-openworld-v5', 'city-openworld-v6') AND NEW.fact_type LIKE 'system.%') THEN
        RAISE EXCEPTION 'derived open-world runtime fact requires a parent fact'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
