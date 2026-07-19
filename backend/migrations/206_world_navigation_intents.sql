-- F7.11 persistent actor movement intents, deterministic budgets, and Tick reservations.

CREATE TABLE IF NOT EXISTS world_navigation_profiles (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    profile_version VARCHAR(16) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    maximum_intents_per_tick INTEGER NOT NULL CHECK (maximum_intents_per_tick BETWEEN 1 AND 4096),
    default_budget_gain_units BIGINT NOT NULL CHECK (default_budget_gain_units > 0),
    default_budget_cap_units BIGINT NOT NULL CHECK (default_budget_cap_units >= default_budget_gain_units),
    default_max_steps INTEGER NOT NULL CHECK (default_max_steps BETWEEN 1 AND 1024),
    maximum_blocked_attempts INTEGER NOT NULL CHECK (maximum_blocked_attempts BETWEEN 1 AND 100000),
    maximum_retry_delay_ticks BIGINT NOT NULL CHECK (maximum_retry_delay_ticks BETWEEN 1 AND 1024),
    fairness_aging_cap BIGINT NOT NULL CHECK (fairness_aging_cap BETWEEN 1 AND 1000000),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_navigation_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS world_actor_navigation_intents (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    actor_id BIGINT NOT NULL,
    intent_code VARCHAR(192) NOT NULL,
    destination_x BIGINT NOT NULL,
    destination_y BIGINT NOT NULL,
    destination_z INTEGER NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'blocked', 'arrived', 'cancelled', 'failed')),
    on_blocked VARCHAR(16) NOT NULL CHECK (on_blocked IN ('retry', 'cancel')),
    priority INTEGER NOT NULL CHECK (priority BETWEEN -10 AND 10),
    max_steps INTEGER NOT NULL CHECK (max_steps BETWEEN 1 AND 1024),
    budget_units BIGINT NOT NULL CHECK (budget_units >= 0),
    budget_gain_units BIGINT NOT NULL CHECK (budget_gain_units > 0),
    budget_cap_units BIGINT NOT NULL CHECK (budget_cap_units >= budget_gain_units AND budget_units <= budget_cap_units),
    blocked_attempts INTEGER NOT NULL DEFAULT 0 CHECK (blocked_attempts >= 0),
    last_reason VARCHAR(64),
    next_attempt_tick BIGINT NOT NULL CHECK (next_attempt_tick >= 0),
    created_tick BIGINT NOT NULL CHECK (created_tick >= 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= created_tick),
    source_fact_id BIGINT NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_actor_navigation_intent_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT world_actor_navigation_intent_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_actor_navigation_intent_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT world_actor_navigation_intents_actor_unique UNIQUE (world_id, actor_id),
    CONSTRAINT world_actor_navigation_intents_code_unique UNIQUE (world_id, intent_code),
    CONSTRAINT world_actor_navigation_intents_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_world_actor_navigation_intents_due
    ON world_actor_navigation_intents (world_id, next_attempt_tick, status, actor_id)
    WHERE status IN ('active', 'blocked');

CREATE TABLE IF NOT EXISTS world_navigation_reservations (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    actor_id BIGINT NOT NULL,
    intent_code VARCHAR(192) NOT NULL,
    from_x BIGINT NOT NULL,
    from_y BIGINT NOT NULL,
    from_z INTEGER NOT NULL,
    to_x BIGINT NOT NULL,
    to_y BIGINT NOT NULL,
    to_z INTEGER NOT NULL,
    target_key VARCHAR(160) NOT NULL,
    edge_key VARCHAR(320) NOT NULL,
    step_cost BIGINT NOT NULL CHECK (step_cost > 0),
    source_fact_id BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status = 'consumed'),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_navigation_reservation_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT world_navigation_reservation_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_navigation_reservation_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT world_navigation_reservation_nonzero_step
        CHECK (from_x <> to_x OR from_y <> to_y OR from_z <> to_z),
    CONSTRAINT world_navigation_reservations_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT world_navigation_reservations_tick_target_unique UNIQUE (world_id, tick, target_key),
    CONSTRAINT world_navigation_reservations_tick_edge_unique UNIQUE (world_id, tick, edge_key),
    CONSTRAINT world_navigation_reservations_source_fact_unique UNIQUE (world_id, source_fact_id),
    CONSTRAINT world_navigation_reservations_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_world_navigation_reservations_actor_tick
    ON world_navigation_reservations (world_id, actor_id, tick DESC, sequence DESC);

CREATE OR REPLACE FUNCTION migration_206_replace_function(
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
        RAISE EXCEPTION 'migration 206 predecessor function % does not contain expected text', target
            USING ERRCODE = '23514';
    END IF;
    BEGIN
        EXECUTE patched;
    EXCEPTION WHEN OTHERS THEN
        RAISE EXCEPTION 'migration 206 failed to extend predecessor function % at %: %',
            target, needle, SQLERRM
            USING ERRCODE = SQLSTATE;
    END;
END;
$migration$;

SELECT migration_206_replace_function(
    'world_runtime_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
    $$world.simulation_version IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$,
    $$world.simulation_version IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$
);
SELECT migration_206_replace_function(
    'guard_world_runtime_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$
);
SELECT migration_206_replace_function(
    'assert_world_runtime_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$
);
SELECT migration_206_replace_function(
    'assert_world_runtime_foundation(bigint)'::REGPROCEDURE,
    $$CASE WHEN world_version = 'city-f7-v8' THEN '1.2.0' WHEN world_version IN ('city-f7-v6', 'city-f7-v7') THEN '1.1.0' ELSE '1.0.0' END$$,
    $$CASE WHEN world_version = 'city-f7-v9' THEN '1.3.0' WHEN world_version = 'city-f7-v8' THEN '1.2.0' WHEN world_version IN ('city-f7-v6', 'city-f7-v7') THEN '1.1.0' ELSE '1.0.0' END$$
);
SELECT migration_206_replace_function(
    'guard_city_enterprise_location_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$
);
SELECT migration_206_replace_function(
    'assert_city_enterprise_location_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$
);
SELECT migration_206_replace_function(
    'guard_city_development_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$
);
SELECT migration_206_replace_function(
    'assert_city_development_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$
);
SELECT migration_206_replace_function(
    'assert_city_land_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$,
    $$world_version NOT IN ('city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$
);
SELECT migration_206_replace_function(
    'guard_city_spatial_mutation_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$,
    $$world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$
);
SELECT migration_206_replace_function(
    'assert_city_spatial_foundation(bigint)'::REGPROCEDURE,
    $$world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8')$$,
    $$world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$
);
SELECT migration_206_replace_function(
    'assert_world_actor_spatial_control_foundation(bigint)'::REGPROCEDURE,
    $$IF world_version NOT IN ('city-f7-v6', 'city-f7-v7', 'city-f7-v8') THEN$$,
    $$IF world_version NOT IN ('city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9') THEN$$
);
SELECT migration_206_replace_function(
    'assert_world_actor_spatial_control_foundation(bigint)'::REGPROCEDURE,
    $$AND runtime_version = CASE WHEN world_version = 'city-f7-v8' THEN '1.2.0' ELSE '1.1.0' END$$,
    $$AND runtime_version = CASE WHEN world_version = 'city-f7-v9' THEN '1.3.0' WHEN world_version = 'city-f7-v8' THEN '1.2.0' ELSE '1.1.0' END$$
);
SELECT migration_206_replace_function(
    'assert_world_actor_spatial_control_foundation(bigint)'::REGPROCEDURE,
    $$source_fact.fact_type NOT IN ('actor.created', 'actor.location.moved')$$,
    $$source_fact.fact_type NOT IN ('actor.created', 'actor.location.moved', 'actor.navigation.intent.progressed')$$
);
SELECT migration_206_replace_function(
    'assert_world_portal_access_foundation(bigint)'::REGPROCEDURE,
    $$IF world_version <> 'city-f7-v8' THEN$$,
    $$IF world_version NOT IN ('city-f7-v8', 'city-f7-v9') THEN$$
);
SELECT migration_206_replace_function(
    'assert_world_portal_access_foundation(bigint)'::REGPROCEDURE,
    $$AND runtime_version = '1.2.0'$$,
    $$AND runtime_version = CASE WHEN world_version = 'city-f7-v9' THEN '1.3.0' ELSE '1.2.0' END$$
);

DROP FUNCTION migration_206_replace_function(REGPROCEDURE, TEXT, TEXT);

CREATE OR REPLACE FUNCTION guard_world_navigation_profile_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF world_runtime_bootstrap_write_enabled(target_world_id) AND TG_OP = 'INSERT' THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'world navigation profile requires bootstrap or recovery context'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS world_navigation_profile_projection_guard ON world_navigation_profiles;
CREATE TRIGGER world_navigation_profile_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_navigation_profiles
FOR EACH ROW EXECUTE FUNCTION guard_world_navigation_profile_projection();

CREATE OR REPLACE FUNCTION guard_world_actor_navigation_intent_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF NOT world_runtime_fact_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'world navigation intent requires draft fact or recovery context'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.version <> 1 OR NEW.source_fact_id IS NULL THEN
            RAISE EXCEPTION 'invalid initial world navigation intent projection'
                USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND NEW.id IS NOT DISTINCT FROM OLD.id
       AND NEW.world_id IS NOT DISTINCT FROM OLD.world_id
       AND NEW.actor_id IS NOT DISTINCT FROM OLD.actor_id
       AND NEW.created_at IS NOT DISTINCT FROM OLD.created_at
       AND NEW.version = OLD.version + 1
       AND NEW.updated_tick >= OLD.updated_tick
       AND NEW.source_fact_id IS DISTINCT FROM OLD.source_fact_id THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'invalid fact-backed world navigation intent update'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS world_actor_navigation_intent_projection_guard ON world_actor_navigation_intents;
CREATE TRIGGER world_actor_navigation_intent_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_actor_navigation_intents
FOR EACH ROW EXECUTE FUNCTION guard_world_actor_navigation_intent_projection();

CREATE OR REPLACE FUNCTION guard_world_navigation_reservation_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF world_runtime_fact_write_enabled(target_world_id) AND TG_OP = 'INSERT' THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'world navigation reservation is immutable and requires a draft fact'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS world_navigation_reservation_projection_guard ON world_navigation_reservations;
CREATE TRIGGER world_navigation_reservation_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_navigation_reservations
FOR EACH ROW EXECUTE FUNCTION guard_world_navigation_reservation_projection();

CREATE OR REPLACE FUNCTION assert_world_navigation_intent_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'world navigation-intent world does not exist' USING ERRCODE = '23514';
    END IF;

    IF world_version <> 'city-f7-v9' THEN
        IF EXISTS (SELECT 1 FROM world_navigation_profiles WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM world_actor_navigation_intents WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM world_navigation_reservations WHERE world_id = target_world_id) THEN
            RAISE EXCEPTION 'pre-F7.11 engine cannot contain navigation-intent state'
                USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM world_navigation_profiles
        WHERE world_id = target_world_id
          AND profile_version = '1.0.0'
          AND baseline_tick <= world_tick
          AND revision = 1
          AND metadata->>'schema_version' = '1'
    ) THEN
        RAISE EXCEPTION 'world navigation profile is missing or invalid'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM world_actor_navigation_intents intent
    JOIN world_actors actor
      ON actor.id = intent.actor_id AND actor.world_id = intent.world_id
    LEFT JOIN world_runtime_facts source
      ON source.id = intent.source_fact_id AND source.world_id = intent.world_id
    LEFT JOIN world_effect_operations effect
      ON effect.source_fact_id = source.id
     AND effect.effect_type = 'actor.navigation.intent.set'
    WHERE intent.world_id = target_world_id
      AND (actor.status <> 'active'
           OR intent.updated_tick > world_tick
           OR intent.next_attempt_tick < intent.updated_tick
           OR source.id IS NULL OR source.posted_at IS NULL
           OR source.actor_id IS DISTINCT FROM intent.actor_id
           OR source.tick <> intent.updated_tick
           OR source.fact_type NOT IN (
                'actor.navigation.intent.created', 'actor.navigation.intent.replaced',
                'actor.navigation.intent.cancelled', 'actor.navigation.intent.waited',
                'actor.navigation.intent.blocked', 'actor.navigation.intent.progressed',
                'actor.navigation.intent.arrived', 'actor.navigation.intent.failed'
           )
           OR effect.id IS NULL OR effect.tick <> source.tick
           OR effect.executor_version <> '1.3.0'
           OR effect.target_actor_id IS DISTINCT FROM intent.actor_id
           OR effect.target_key <> 'navigation.intent'
           OR effect.after_units <> intent.version
           OR effect.payload#>>'{navigation_intent_after,intent_code}' <> intent.intent_code
           OR effect.payload#>>'{navigation_intent_after,status}' <> intent.status
           OR (effect.payload#>>'{navigation_intent_after,version}')::BIGINT <> intent.version);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'world navigation intent projection is inconsistent'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM world_navigation_reservations reservation
    JOIN world_actors actor
      ON actor.id = reservation.actor_id AND actor.world_id = reservation.world_id
    JOIN world_runtime_facts source
      ON source.id = reservation.source_fact_id AND source.world_id = reservation.world_id
    WHERE reservation.world_id = target_world_id
      AND (reservation.tick > world_tick
           OR reservation.target_key <> reservation.to_x::TEXT || ':' || reservation.to_y::TEXT || ':' || reservation.to_z::TEXT
           OR reservation.edge_key <> LEAST(
                reservation.from_x::TEXT || ':' || reservation.from_y::TEXT || ':' || reservation.from_z::TEXT,
                reservation.to_x::TEXT || ':' || reservation.to_y::TEXT || ':' || reservation.to_z::TEXT
              ) || '|' || GREATEST(
                reservation.from_x::TEXT || ':' || reservation.from_y::TEXT || ':' || reservation.from_z::TEXT,
                reservation.to_x::TEXT || ':' || reservation.to_y::TEXT || ':' || reservation.to_z::TEXT
              )
           OR source.posted_at IS NULL OR source.tick <> reservation.tick
           OR source.actor_id IS DISTINCT FROM reservation.actor_id
           OR source.fact_type <> 'actor.navigation.intent.progressed'
           OR source.payload#>>'{reservation,intent_code}' <> reservation.intent_code
           OR source.payload#>>'{reservation,target_key}' <> reservation.target_key
           OR source.payload#>>'{reservation,edge_key}' <> reservation.edge_key
           OR (source.payload#>>'{reservation,step_cost}')::BIGINT <> reservation.step_cost);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'world navigation reservation history is inconsistent'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_world_navigation_intent_foundation()
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
    PERFORM assert_world_navigation_intent_foundation(target_world_id);
    RETURN NULL;
END;
$$;

DO $$
DECLARE
    table_name TEXT;
    trigger_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'world_navigation_profiles',
        'world_actor_navigation_intents',
        'world_navigation_reservations',
        'world_actors',
        'world_actor_locations',
        'world_runtime_profiles',
        'world_runtime_facts',
        'world_effect_operations'
    ] LOOP
        trigger_name := table_name || '_navigation_intent_commit_check';
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', trigger_name, table_name);
        EXECUTE format(
            'CREATE CONSTRAINT TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I '
            || 'DEFERRABLE INITIALLY DEFERRED FOR EACH ROW '
            || 'EXECUTE FUNCTION check_world_navigation_intent_foundation()',
            trigger_name, table_name
        );
    END LOOP;
END;
$$;

DROP TRIGGER IF EXISTS city_world_navigation_intent_commit_check ON city_worlds;
CREATE CONSTRAINT TRIGGER city_world_navigation_intent_commit_check
AFTER INSERT OR UPDATE OF simulation_version, current_tick ON city_worlds
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_world_navigation_intent_foundation();

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f7-v9', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","spatial","development","enterprise_location","world_runtime","actor_spatial_control","actor_navigation","portal_access","navigation_intents","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f7-v8', 'city-f7-v9', 'f7_v8_to_f7_v9')
ON CONFLICT (from_version, to_version) DO NOTHING;
