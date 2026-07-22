-- city-openworld-v7 adds open-world-native social services.  It deliberately
-- does not reuse the historical F8 public-service tables: V7 providers are
-- attached to V5 open-world facilities and all queue/response state remains
-- inside the V4+ fact/runtime domain.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v7', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","version_vector","facilities","npc_lod","social_service_catalog","social_service_access","social_service_queue","social_service_response","activities","rules","case_lifecycle","rewards","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v6', 'city-openworld-v7', 'openworld_v6_to_v7')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_service_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    access_model_version VARCHAR(32) NOT NULL,
    dispatch_model_version VARCHAR(32) NOT NULL,
    maximum_queue_per_provider INTEGER NOT NULL CHECK (maximum_queue_per_provider BETWEEN 1 AND 100000),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_service_profile_identity_check CHECK (
        profile_id ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND profile_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND access_model_version ~ '^[a-z][a-z0-9_.-]{1,31}$'
        AND dispatch_model_version ~ '^[a-z][a-z0-9_.-]{1,31}$'
    ),
    CONSTRAINT city_open_world_service_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_service_catalog (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_service_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(64) NOT NULL,
    name_key VARCHAR(160) NOT NULL,
    category_code VARCHAR(64) NOT NULL,
    definition_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    maximum_wait_ticks BIGINT NOT NULL CHECK (maximum_wait_ticks BETWEEN 1 AND 8760),
    target_response_ticks BIGINT NOT NULL CHECK (target_response_ticks BETWEEN 1 AND 8760),
    default_priority_milli INTEGER NOT NULL CHECK (default_priority_milli BETWEEN -100000 AND 100000),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_service_catalog_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND category_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND definition_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND char_length(name_key) BETWEEN 1 AND 160
    ),
    CONSTRAINT city_open_world_service_catalog_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_service_providers (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_service_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    facility_id BIGINT NOT NULL,
    service_code VARCHAR(64) NOT NULL,
    provider_kind VARCHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    capacity_units_per_tick BIGINT NOT NULL CHECK (capacity_units_per_tick > 0),
    access_radius_units BIGINT NOT NULL CHECK (access_radius_units > 0),
    anchor_x BIGINT NOT NULL,
    anchor_y BIGINT NOT NULL,
    anchor_z SMALLINT NOT NULL CHECK (anchor_z BETWEEN 0 AND 127),
    last_settled_tick BIGINT NOT NULL DEFAULT 0 CHECK (last_settled_tick >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_service_provider_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND provider_kind ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND status IN ('active', 'suspended', 'closed')
    ),
    CONSTRAINT city_open_world_service_provider_facility_fk
        FOREIGN KEY (facility_id, world_id)
        REFERENCES city_open_world_facilities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_service_provider_service_fk
        FOREIGN KEY (world_id, service_code)
        REFERENCES city_open_world_service_catalog(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_service_provider_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_service_providers_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_service_providers_world_facility_service_unique UNIQUE (world_id, facility_id, service_code),
    CONSTRAINT city_open_world_service_providers_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_service_providers_dispatch
    ON city_open_world_service_providers (world_id, service_code, status, code);

CREATE TABLE IF NOT EXISTS city_open_world_service_requests (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_service_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    actor_id BIGINT NOT NULL,
    service_code VARCHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    priority_milli INTEGER NOT NULL CHECK (priority_milli BETWEEN -100000 AND 100000),
    requested_units BIGINT NOT NULL CHECK (requested_units BETWEEN 1 AND 1000),
    requested_tick BIGINT NOT NULL CHECK (requested_tick > 0),
    earliest_dispatch_tick BIGINT NOT NULL CHECK (earliest_dispatch_tick > requested_tick),
    deadline_tick BIGINT NOT NULL CHECK (deadline_tick >= earliest_dispatch_tick),
    queued_tick BIGINT,
    provider_id BIGINT,
    dispatched_tick BIGINT,
    resolved_tick BIGINT,
    queue_position INTEGER,
    source_fact_id BIGINT NOT NULL,
    last_fact_id BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_service_request_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND status IN ('pending', 'queued', 'dispatched', 'served', 'expired', 'cancelled')
    ),
    CONSTRAINT city_open_world_service_request_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_service_request_service_fk
        FOREIGN KEY (world_id, service_code)
        REFERENCES city_open_world_service_catalog(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_service_request_provider_fk
        FOREIGN KEY (provider_id, world_id)
        REFERENCES city_open_world_service_providers(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_service_request_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_service_request_last_fact_fk
        FOREIGN KEY (last_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_service_request_timing_check CHECK (
        (queued_tick IS NULL OR queued_tick >= requested_tick)
        AND (dispatched_tick IS NULL OR (queued_tick IS NOT NULL AND dispatched_tick >= queued_tick))
        AND (resolved_tick IS NULL OR resolved_tick >= requested_tick)
        AND (queue_position IS NULL OR queue_position > 0)
    ),
    CONSTRAINT city_open_world_service_request_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_service_requests_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_service_requests_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_service_requests_dispatch
    ON city_open_world_service_requests (world_id, status, earliest_dispatch_tick, deadline_tick, priority_milli DESC, requested_tick, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_service_requests_provider_queue
    ON city_open_world_service_requests (world_id, provider_id, status, priority_milli DESC, requested_tick, code)
    WHERE status = 'queued';

CREATE TABLE IF NOT EXISTS city_open_world_service_responses (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_service_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    request_id BIGINT NOT NULL,
    actor_id BIGINT NOT NULL,
    service_code VARCHAR(64) NOT NULL,
    provider_id BIGINT,
    outcome VARCHAR(16) NOT NULL CHECK (outcome IN ('served', 'expired')),
    requested_tick BIGINT NOT NULL CHECK (requested_tick > 0),
    queued_tick BIGINT,
    dispatched_tick BIGINT,
    resolved_tick BIGINT NOT NULL CHECK (resolved_tick > 0),
    response_ticks BIGINT NOT NULL CHECK (response_ticks >= 0),
    delivered_units BIGINT NOT NULL CHECK (delivered_units >= 0),
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_service_response_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND resolved_tick >= requested_tick
        AND (outcome <> 'served' OR (provider_id IS NOT NULL AND dispatched_tick IS NOT NULL AND delivered_units > 0))
        AND (outcome <> 'expired' OR delivered_units = 0)
    ),
    CONSTRAINT city_open_world_service_response_request_fk
        FOREIGN KEY (request_id, world_id)
        REFERENCES city_open_world_service_requests(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_service_response_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_service_response_service_fk
        FOREIGN KEY (world_id, service_code)
        REFERENCES city_open_world_service_catalog(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_service_response_provider_fk
        FOREIGN KEY (provider_id, world_id)
        REFERENCES city_open_world_service_providers(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_service_response_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_service_response_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_service_responses_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_service_responses_request_unique UNIQUE (world_id, request_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_service_responses_history
    ON city_open_world_service_responses (world_id, resolved_tick, code);

CREATE OR REPLACE FUNCTION city_open_world_service_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_service_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1
            FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v7'
              AND world.state_hash IS NULL
              AND (
                  world.current_tick = 0
                  OR EXISTS (
                      SELECT 1 FROM city_world_upgrade_runs upgrade
                      WHERE upgrade.id = CASE
                          WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                          THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT
                          ELSE NULL
                      END
                        AND upgrade.world_id = target_world_id
                        AND upgrade.to_version = 'city-openworld-v7'
                        AND upgrade.status = 'running'
                  )
              )
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v7'
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_service_baseline()
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
    IF TG_OP = 'INSERT' AND city_open_world_service_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world service baseline is immutable outside genesis, audited upgrade, or recovery'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_service_provider()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id)
       OR city_open_world_service_bootstrap_write_enabled(target_world_id)
       OR city_open_world_service_fact_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world service provider requires bootstrap, fact, or recovery context'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_service_request()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id)
       OR city_open_world_service_fact_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world service request requires fact or recovery context'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_service_response()
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
    IF TG_OP = 'INSERT' AND city_open_world_service_fact_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world service responses are immutable outside fact or recovery context'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_service_profile_guard ON city_open_world_service_profiles;
CREATE TRIGGER city_open_world_service_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_service_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_service_baseline();

DROP TRIGGER IF EXISTS city_open_world_service_catalog_guard ON city_open_world_service_catalog;
CREATE TRIGGER city_open_world_service_catalog_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_service_catalog
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_service_baseline();

DROP TRIGGER IF EXISTS city_open_world_service_provider_guard ON city_open_world_service_providers;
CREATE TRIGGER city_open_world_service_provider_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_service_providers
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_service_provider();

DROP TRIGGER IF EXISTS city_open_world_service_request_guard ON city_open_world_service_requests;
CREATE TRIGGER city_open_world_service_request_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_service_requests
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_service_request();

DROP TRIGGER IF EXISTS city_open_world_service_response_guard ON city_open_world_service_responses;
CREATE TRIGGER city_open_world_service_response_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_service_responses
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_service_response();

-- V7 extends guards without modifying the semantics of V1--V6 rows.  The
-- recovery branch is intentionally explicit: immutable generated/runtime
-- projections can be reconstructed only within the audited recovery run.
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
                  'city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7'
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
                  'city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7'
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
              AND world.simulation_version IN ('city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7')
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
              AND world.simulation_version IN ('city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7')
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
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_initialization_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city open-world binding is immutable outside initialization or recovery'
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
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND (
        city_open_world_initialization_write_enabled(target_world_id)
        OR city_open_world_materialization_write_enabled(target_world_id)
    ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city open-world projection facts are immutable outside initialization, materialization, or recovery'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_definition()
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
    IF TG_OP = 'INSERT' AND city_open_world_runtime_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world runtime definitions are immutable outside genesis or recovery'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_scenario_binding()
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
    IF TG_OP = 'INSERT' AND city_open_world_runtime_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world scenario bindings are immutable outside genesis or recovery'
        USING ERRCODE = '55000';
END;
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
    IF world_version NOT IN ('city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7')
       OR NEW.tick IS DISTINCT FROM world_tick + 1
       OR NEW.posted_at IS NOT NULL THEN
        RAISE EXCEPTION 'open-world runtime fact must be a draft for the next V4/V5/V6/V7 tick'
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
       AND NOT (world_version IN ('city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7') AND NEW.fact_type LIKE 'system.%') THEN
        RAISE EXCEPTION 'derived open-world runtime fact requires a parent fact'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_service_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    profile_tick BIGINT;
BEGIN
    SELECT simulation_version INTO world_version FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v7' THEN
        RETURN;
    END IF;
    SELECT baseline_tick INTO profile_tick
    FROM city_open_world_service_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL THEN
        RAISE EXCEPTION 'city open-world V7 service profile is missing' USING ERRCODE = '23514';
    END IF;
    IF (SELECT COUNT(*) FROM city_open_world_service_catalog WHERE world_id = target_world_id) <> 4 THEN
        RAISE EXCEPTION 'city open-world V7 service catalog is incomplete' USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM city_open_world_service_providers WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V7 service providers are missing' USING ERRCODE = '23514';
    END IF;
END;
$$;

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
        RAISE EXCEPTION 'city open-world version vector requires exactly seven bindings'
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
        RAISE EXCEPTION 'city open-world version vector is incomplete'
            USING ERRCODE = '23514';
    END IF;
END;
$$;
