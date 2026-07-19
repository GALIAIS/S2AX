-- city-openworld-v5 keeps V3's immutable Region/Sector/Interior topology and
-- V4's independent actor runtime, then adds the first durable social-world
-- primitives.  It is a new engine contract rather than a retrofit of V4:
-- existing V4 snapshots remain byte-stable and V5 genesis binds its own
-- scenario, facility and NPC baseline.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v5', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","facilities","npc_lod","activities","rules","case_lifecycle","rewards","snapshot","replay","recovery_verification"]'::jsonb
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
              AND world.simulation_version IN (
                  'city-openworld-v1', 'city-openworld-v2', 'city-openworld-v3',
                  'city-openworld-v4', 'city-openworld-v5'
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
              AND world.simulation_version IN ('city-openworld-v2', 'city-openworld-v3', 'city-openworld-v4', 'city-openworld-v5')
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
              AND world.simulation_version IN ('city-openworld-v4', 'city-openworld-v5')
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
              AND world.simulation_version IN ('city-openworld-v4', 'city-openworld-v5')
              AND world.current_tick >= 0
              AND world.state_hash IS NOT NULL
       )
$$;

-- V5 has deterministic autonomous reducers.  Their root facts are explicit
-- `system.*` facts, are still restricted to the next tick and the protected
-- runtime write context, and cannot be forged through the command API.
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
    IF world_version NOT IN ('city-openworld-v4', 'city-openworld-v5')
       OR NEW.tick IS DISTINCT FROM world_tick + 1
       OR NEW.posted_at IS NOT NULL THEN
        RAISE EXCEPTION 'open-world runtime fact must be a draft for the next V4/V5 tick'
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
       AND NOT (world_version = 'city-openworld-v5' AND NEW.fact_type LIKE 'system.%') THEN
        RAISE EXCEPTION 'derived open-world runtime fact requires a parent fact'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TABLE IF NOT EXISTS city_open_world_scenario_bindings (
    world_id BIGINT PRIMARY KEY REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
    scenario_id VARCHAR(64) NOT NULL,
    scenario_version VARCHAR(24) NOT NULL,
    scenario_hash VARCHAR(64) NOT NULL,
    profile_id VARCHAR(64) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    profile_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_scenario_binding_identity_check CHECK (
        scenario_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND scenario_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND scenario_hash ~ '^[0-9a-f]{64}$'
        AND profile_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND profile_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND profile_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_open_world_scenario_binding_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_facilities (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    building_code VARCHAR(96) NOT NULL,
    facility_type_code VARCHAR(64) NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'active',
    capacity_units BIGINT NOT NULL CHECK (capacity_units > 0),
    anchor_x BIGINT NOT NULL,
    anchor_y BIGINT NOT NULL,
    anchor_z SMALLINT NOT NULL CHECK (anchor_z BETWEEN 0 AND 127),
    last_settled_tick BIGINT NOT NULL DEFAULT 0 CHECK (last_settled_tick >= 0),
    source_fact_id BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_facility_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND facility_type_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND state IN ('active', 'suspended', 'closed')
    ),
    CONSTRAINT city_open_world_facility_building_fk
        FOREIGN KEY (world_id, building_code)
        REFERENCES city_open_world_buildings(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_facility_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_facility_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_facilities_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_facilities_world_building_unique UNIQUE (world_id, building_code),
    CONSTRAINT city_open_world_facilities_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_facilities_type_state
    ON city_open_world_facilities (world_id, facility_type_code, state, code);

CREATE TABLE IF NOT EXISTS city_open_world_npc_profiles (
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
    actor_id BIGINT NOT NULL,
    behavior_code VARCHAR(96) NOT NULL,
    behavior_version VARCHAR(24) NOT NULL,
    behavior_hash VARCHAR(64) NOT NULL,
    home_facility_id BIGINT,
    work_facility_id BIGINT,
    lod_tier VARCHAR(16) NOT NULL DEFAULT 'active',
    schedule_offset INTEGER NOT NULL DEFAULT 0 CHECK (schedule_offset BETWEEN 0 AND 167),
    next_action_tick BIGINT NOT NULL DEFAULT 1 CHECK (next_action_tick > 0),
    last_action_tick BIGINT NOT NULL DEFAULT 0 CHECK (last_action_tick >= 0),
    behavior_state JSONB NOT NULL DEFAULT '{}'::jsonb,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_id),
    CONSTRAINT city_open_world_npc_profile_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_npc_profile_home_fk
        FOREIGN KEY (home_facility_id, world_id)
        REFERENCES city_open_world_facilities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_npc_profile_work_fk
        FOREIGN KEY (work_facility_id, world_id)
        REFERENCES city_open_world_facilities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_npc_profile_identity_check CHECK (
        behavior_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND behavior_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND behavior_hash ~ '^[0-9a-f]{64}$'
        AND lod_tier IN ('active', 'coarse', 'dormant')
        AND last_action_tick < next_action_tick
    ),
    CONSTRAINT city_open_world_npc_profile_state_check CHECK (jsonb_typeof(behavior_state) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_npc_profiles_schedule
    ON city_open_world_npc_profiles (world_id, lod_tier, next_action_tick, actor_id);

CREATE TABLE IF NOT EXISTS city_open_world_actor_navigation_intents (
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
    actor_id BIGINT NOT NULL,
    intent_code VARCHAR(160) NOT NULL,
    target_space_kind VARCHAR(16) NOT NULL,
    target_location_scope VARCHAR(128) NOT NULL,
    target_building_code VARCHAR(96),
    target_floor_index INTEGER NOT NULL DEFAULT 0,
    target_x BIGINT NOT NULL,
    target_y BIGINT NOT NULL,
    target_z SMALLINT NOT NULL CHECK (target_z BETWEEN 0 AND 127),
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    priority INTEGER NOT NULL DEFAULT 0 CHECK (priority BETWEEN -1000 AND 1000),
    maximum_steps INTEGER NOT NULL DEFAULT 128 CHECK (maximum_steps BETWEEN 1 AND 2048),
    completed_steps INTEGER NOT NULL DEFAULT 0 CHECK (completed_steps >= 0),
    blocked_attempts INTEGER NOT NULL DEFAULT 0 CHECK (blocked_attempts >= 0),
    next_attempt_tick BIGINT NOT NULL CHECK (next_attempt_tick > 0),
    created_tick BIGINT NOT NULL CHECK (created_tick > 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= created_tick),
    source_fact_id BIGINT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_id),
    CONSTRAINT city_open_world_navigation_intent_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_navigation_intent_building_fk
        FOREIGN KEY (world_id, target_building_code)
        REFERENCES city_open_world_buildings(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_navigation_intent_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_navigation_intent_identity_check CHECK (
        intent_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND status IN ('active', 'arrived', 'cancelled', 'failed')
        AND (
            (target_space_kind = 'surface' AND target_location_scope = 'surface'
                AND target_building_code IS NULL AND target_floor_index = 0 AND target_z = 0)
            OR (target_space_kind = 'interior' AND target_building_code IS NOT NULL
                AND target_location_scope = target_building_code AND target_floor_index >= 0
                AND target_z = target_floor_index)
        )
    ),
    CONSTRAINT city_open_world_navigation_intent_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_navigation_intents_active
    ON city_open_world_actor_navigation_intents (world_id, status, next_attempt_tick, priority DESC, actor_id)
    WHERE status = 'active';

CREATE OR REPLACE FUNCTION guard_city_open_world_scenario_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF TG_OP = 'INSERT' AND city_open_world_runtime_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world scenario bindings are immutable outside V5 genesis'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_scenario_binding_guard ON city_open_world_scenario_bindings;
CREATE TRIGGER city_open_world_scenario_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_scenario_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_scenario_binding();

DROP TRIGGER IF EXISTS city_open_world_facility_guard ON city_open_world_facilities;
CREATE TRIGGER city_open_world_facility_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_facilities
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_projection();

DROP TRIGGER IF EXISTS city_open_world_npc_profile_guard ON city_open_world_npc_profiles;
CREATE TRIGGER city_open_world_npc_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_npc_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_projection();

DROP TRIGGER IF EXISTS city_open_world_navigation_intent_guard ON city_open_world_actor_navigation_intents;
CREATE TRIGGER city_open_world_navigation_intent_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_actor_navigation_intents
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_projection();
