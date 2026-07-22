-- city-openworld-v10 adds a deliberate cross-scale hand-off: a completed V9
-- aggregate route may later become a validated V5 local surface location.
-- The V9 route remains immutable; every local landing has its own fact chain.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v10', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","version_vector","facilities","npc_lod","social_service_catalog","social_service_access","social_service_queue","social_service_response","cross_domain_impact_bridge","aggregate_mobility_graph","mobility_capacity_allocation","mobility_route_lifecycle","mobility_arrival_bridge","cross_scale_spatial_transfer","delayed_effects","domain_metrics","activities","rules","case_lifecycle","rewards","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v9', 'city-openworld-v10', 'openworld_v9_to_v10')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_mobility_arrival_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_mobility_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    bridge_contract VARCHAR(64) NOT NULL,
    landing_contract VARCHAR(64) NOT NULL,
    maximum_arrivals_per_tick INTEGER NOT NULL CHECK (maximum_arrivals_per_tick BETWEEN 1 AND 100000),
    landing_search_radius BIGINT NOT NULL CHECK (landing_search_radius BETWEEN 0 AND 256),
    maximum_blocked_attempts INTEGER NOT NULL CHECK (maximum_blocked_attempts BETWEEN 1 AND 64),
    arrival_count BIGINT NOT NULL DEFAULT 0 CHECK (arrival_count >= 0),
    landed_count BIGINT NOT NULL DEFAULT 0 CHECK (landed_count >= 0),
    blocked_count BIGINT NOT NULL DEFAULT 0 CHECK (blocked_count >= 0),
    failed_count BIGINT NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_mobility_arrival_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-mobility-arrival'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND bridge_contract = 'completed_route_next_tick_bridge_v1'
        AND landing_contract = 'validated_surface_anchor_landing_v1'
        AND landed_count + failed_count <= arrival_count
    ),
    CONSTRAINT city_open_world_mobility_arrival_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_mobility_arrivals (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_mobility_arrival_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    route_id BIGINT NOT NULL,
    demand_id BIGINT NOT NULL,
    actor_id BIGINT NOT NULL,
    destination_hub_code VARCHAR(160) NOT NULL,
    expected_origin JSONB NOT NULL,
    landing_location JSONB,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    blocked_attempts INTEGER NOT NULL DEFAULT 0 CHECK (blocked_attempts >= 0),
    next_attempt_tick BIGINT NOT NULL CHECK (next_attempt_tick > 0),
    created_tick BIGINT NOT NULL CHECK (created_tick > 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= created_tick),
    source_fact_id BIGINT NOT NULL,
    last_fact_id BIGINT NOT NULL,
    landing_fact_id BIGINT,
    landed_tick BIGINT,
    failed_tick BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_mobility_arrival_route_fk
        FOREIGN KEY (route_id, world_id)
        REFERENCES city_open_world_mobility_routes(id, world_id) ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_mobility_arrival_demand_fk
        FOREIGN KEY (demand_id, world_id)
        REFERENCES city_open_world_mobility_demands(id, world_id) ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_mobility_arrival_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_arrival_hub_fk
        FOREIGN KEY (world_id, destination_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_arrival_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_mobility_arrival_last_fact_fk
        FOREIGN KEY (last_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_mobility_arrival_landing_fact_fk
        FOREIGN KEY (landing_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_mobility_arrival_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND status IN ('pending', 'blocked', 'landed', 'failed')
        AND jsonb_typeof(expected_origin) = 'object'
        AND (landing_location IS NULL OR jsonb_typeof(landing_location) = 'object')
    ),
    CONSTRAINT city_open_world_mobility_arrival_lifecycle_check CHECK (
        (status = 'pending' AND blocked_attempts = 0 AND landing_location IS NULL
         AND landing_fact_id IS NULL AND landed_tick IS NULL AND failed_tick IS NULL)
        OR (status = 'blocked' AND blocked_attempts >= 1 AND landing_location IS NULL
            AND landing_fact_id IS NULL AND landed_tick IS NULL AND failed_tick IS NULL)
        OR (status = 'landed' AND landing_location IS NOT NULL AND landing_fact_id IS NOT NULL
            AND landed_tick IS NOT NULL AND failed_tick IS NULL)
        OR (status = 'failed' AND landing_location IS NULL AND landing_fact_id IS NULL
            AND landed_tick IS NULL AND failed_tick IS NOT NULL)
    ),
    CONSTRAINT city_open_world_mobility_arrival_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_mobility_arrivals_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_mobility_arrivals_route_unique UNIQUE (world_id, route_id),
    CONSTRAINT city_open_world_mobility_arrivals_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_mobility_arrivals_due
    ON city_open_world_mobility_arrivals (world_id, status, next_attempt_tick, created_tick, code);

-- V10 is a strict successor of V9, so the V9 foundation gates accept V10
-- genesis/upgrade state without weakening historical V9 world rows.
CREATE OR REPLACE FUNCTION city_open_world_mobility_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v9', 'city-openworld-v10')
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = world.simulation_version
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v9', 'city-openworld-v10'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_arrival_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_arrival_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v10'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v10'
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_arrival_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_arrival_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version = 'city-openworld-v10')
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_mobility_arrival_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_arrival_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_arrival_write_enabled(target_world_id)
       AND NEW.revision = OLD.revision + 1
       AND NEW.arrival_count >= OLD.arrival_count
       AND NEW.landed_count >= OLD.landed_count
       AND NEW.blocked_count >= OLD.blocked_count
       AND NEW.failed_count >= OLD.failed_count
       AND (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.bridge_contract, OLD.landing_contract,
            OLD.maximum_arrivals_per_tick, OLD.landing_search_radius,
            OLD.maximum_blocked_attempts, OLD.metadata, OLD.created_at)
           IS NOT DISTINCT FROM
           (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.bridge_contract, NEW.landing_contract,
            NEW.maximum_arrivals_per_tick, NEW.landing_search_radius,
            NEW.maximum_blocked_attempts, NEW.metadata, NEW.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world mobility arrival profile may only advance audited counters'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_mobility_arrival()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT; maximum_blocked_attempts_value INTEGER;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' OR NOT city_open_world_arrival_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world mobility arrivals require a runtime fact or recovery context'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'pending' OR NEW.blocked_attempts <> 0
           OR NEW.next_attempt_tick <> NEW.created_tick
           OR NOT EXISTS (
                SELECT 1 FROM city_open_world_mobility_routes route
                JOIN city_open_world_runtime_facts completion_fact
                  ON completion_fact.id = route.completion_fact_id
                 AND completion_fact.world_id = route.world_id
                JOIN city_open_world_runtime_facts pending_fact
                  ON pending_fact.id = NEW.last_fact_id
                 AND pending_fact.world_id = NEW.world_id
                WHERE route.id = NEW.route_id AND route.world_id = NEW.world_id
                  AND route.demand_id = NEW.demand_id AND route.actor_id = NEW.actor_id
                  AND route.destination_hub_code = NEW.destination_hub_code
                  AND route.status = 'completed' AND completion_fact.id = NEW.source_fact_id
                  AND completion_fact.fact_type = 'mobility.completed'
                  AND completion_fact.tick < NEW.created_tick
                  AND pending_fact.fact_type = 'mobility.arrival.pending'
                  AND pending_fact.actor_id = NEW.actor_id
                  AND pending_fact.tick = NEW.created_tick
           ) THEN
            RAISE EXCEPTION 'open-world mobility arrival must root in a completed route and pending arrival fact'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF (OLD.world_id, OLD.code, OLD.route_id, OLD.demand_id, OLD.actor_id,
        OLD.destination_hub_code, OLD.expected_origin, OLD.created_tick,
        OLD.source_fact_id, OLD.metadata, OLD.created_at) IS DISTINCT FROM
       (NEW.world_id, NEW.code, NEW.route_id, NEW.demand_id, NEW.actor_id,
        NEW.destination_hub_code, NEW.expected_origin, NEW.created_tick,
        NEW.source_fact_id, NEW.metadata, NEW.created_at)
       OR NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'open-world mobility arrival identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    SELECT maximum_blocked_attempts INTO maximum_blocked_attempts_value
    FROM city_open_world_mobility_arrival_profiles
    WHERE world_id = target_world_id;
    IF maximum_blocked_attempts_value IS NULL
       OR NEW.blocked_attempts > maximum_blocked_attempts_value THEN
        RAISE EXCEPTION 'open-world mobility arrival exceeds sealed blocked-attempt policy'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.status IN ('pending', 'blocked') AND NEW.status = 'blocked'
       AND NEW.blocked_attempts = OLD.blocked_attempts + 1
       AND NEW.updated_tick >= OLD.updated_tick
       AND NEW.next_attempt_tick = NEW.updated_tick + 1
       AND EXISTS (SELECT 1 FROM city_open_world_runtime_facts fact
                   WHERE fact.id = NEW.last_fact_id AND fact.world_id = NEW.world_id
                     AND fact.parent_fact_id = OLD.last_fact_id
                     AND fact.actor_id = NEW.actor_id AND fact.tick = NEW.updated_tick
                     AND fact.fact_type = 'mobility.arrival.blocked') THEN
        RETURN NEW;
    END IF;
    IF OLD.status IN ('pending', 'blocked') AND NEW.status = 'landed'
       AND NEW.landing_fact_id = NEW.last_fact_id
       AND NEW.landed_tick = NEW.updated_tick
       AND NEW.next_attempt_tick = NEW.updated_tick
       AND EXISTS (SELECT 1 FROM city_open_world_runtime_facts fact
                   WHERE fact.id = NEW.last_fact_id AND fact.world_id = NEW.world_id
                     AND fact.parent_fact_id = OLD.last_fact_id
                     AND fact.actor_id = NEW.actor_id AND fact.tick = NEW.updated_tick
                     AND fact.fact_type = 'mobility.arrival.landed') THEN
        RETURN NEW;
    END IF;
    IF OLD.status IN ('pending', 'blocked') AND NEW.status = 'failed'
       AND NEW.failed_tick = NEW.updated_tick
       AND NEW.next_attempt_tick = NEW.updated_tick
       AND EXISTS (SELECT 1 FROM city_open_world_runtime_facts fact
                   WHERE fact.id = NEW.last_fact_id AND fact.world_id = NEW.world_id
                     AND fact.parent_fact_id = OLD.last_fact_id
                     AND fact.actor_id = NEW.actor_id AND fact.tick = NEW.updated_tick
                     AND fact.fact_type = 'mobility.arrival.failed') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world mobility arrival transition is invalid'
        USING ERRCODE = '23514';
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_mobility_arrival_profile_guard ON city_open_world_mobility_arrival_profiles;
CREATE TRIGGER city_open_world_mobility_arrival_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_mobility_arrival_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_mobility_arrival_profile();

DROP TRIGGER IF EXISTS city_open_world_mobility_arrival_guard ON city_open_world_mobility_arrivals;
CREATE TRIGGER city_open_world_mobility_arrival_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_mobility_arrivals
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_mobility_arrival();

-- V10 uses all prior open-world foundations. These successor gates are kept
-- explicit so a V10 world never relies on an accidental permissive fallback.
CREATE OR REPLACE FUNCTION city_open_world_service_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_service_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10')
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = world.simulation_version
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_impact_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10')
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = world.simulation_version
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v1','city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_materialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_materialization_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_fact_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_fact_insert()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE world_tick BIGINT; world_version VARCHAR(32); command_status_value VARCHAR(16);
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN RETURN NEW; END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version FROM city_worlds WHERE id = NEW.world_id;
    IF world_version NOT IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10')
       OR NEW.tick IS DISTINCT FROM world_tick + 1 OR NEW.posted_at IS NOT NULL THEN
        RAISE EXCEPTION 'open-world runtime fact must be a draft for the next open-world tick' USING ERRCODE = '23514';
    END IF;
    IF NEW.source_command_id IS NOT NULL THEN
        SELECT status INTO command_status_value FROM city_commands WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
        IF command_status_value IS DISTINCT FROM 'pending' THEN
            RAISE EXCEPTION 'open-world runtime fact requires a pending source command' USING ERRCODE = '23514';
        END IF;
    END IF;
    IF NEW.source_command_id IS NULL AND NEW.parent_fact_id IS NULL
       AND NOT (world_version IN ('city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10') AND NEW.fact_type LIKE 'system.%') THEN
        RAISE EXCEPTION 'derived open-world runtime fact requires a parent fact' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_mobility_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT; world_tick BIGINT; expected_hash VARCHAR(64);
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v9', 'city-openworld-v10') THEN RETURN; END IF;
    SELECT baseline_tick, content_hash INTO profile_tick, expected_hash FROM city_open_world_mobility_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick OR expected_hash IS NULL THEN
        RAISE EXCEPTION 'city open-world V9/V10 mobility profile is missing or has an invalid baseline' USING ERRCODE = '23514'; END IF;
    IF (SELECT COUNT(*) FROM city_open_world_mobility_modes WHERE world_id = target_world_id) <> 3
       OR (SELECT COUNT(*) FROM city_open_world_mobility_hubs WHERE world_id = target_world_id) < 3
       OR (SELECT COUNT(*) FROM city_open_world_mobility_edges WHERE world_id = target_world_id) = 0 THEN
        RAISE EXCEPTION 'city open-world V9/V10 mobility topology is incomplete' USING ERRCODE = '23514'; END IF;
    IF EXISTS (
        SELECT 1 FROM city_open_world_mobility_profiles profile
        WHERE profile.world_id = target_world_id
          AND (profile.mode_count <> (SELECT COUNT(*) FROM city_open_world_mobility_modes WHERE world_id = target_world_id)
               OR profile.hub_count <> (SELECT COUNT(*) FROM city_open_world_mobility_hubs WHERE world_id = target_world_id)
               OR profile.edge_count <> (SELECT COUNT(*) FROM city_open_world_mobility_edges WHERE world_id = target_world_id)
               OR profile.demand_count <> (SELECT COUNT(*) FROM city_open_world_mobility_demands WHERE world_id = target_world_id)
               OR profile.route_count <> (SELECT COUNT(*) FROM city_open_world_mobility_routes WHERE world_id = target_world_id)
               OR profile.allocation_count <> (SELECT COUNT(*) FROM city_open_world_mobility_allocations WHERE world_id = target_world_id)
               OR profile.actor_metric_count <> (SELECT COUNT(*) FROM city_open_world_mobility_actor_metrics WHERE world_id = target_world_id))
    ) THEN
        RAISE EXCEPTION 'city open-world V9/V10 mobility profile counters are inconsistent' USING ERRCODE = '23514'; END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_service_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT;
BEGIN
    SELECT simulation_version INTO world_version FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10') THEN RETURN; END IF;
    SELECT baseline_tick INTO profile_tick FROM city_open_world_service_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL THEN RAISE EXCEPTION 'city open-world V7+ service profile is missing' USING ERRCODE = '23514'; END IF;
    IF (SELECT COUNT(*) FROM city_open_world_service_catalog WHERE world_id = target_world_id) <> 4 THEN
        RAISE EXCEPTION 'city open-world V7+ service catalog is incomplete' USING ERRCODE = '23514'; END IF;
    IF NOT EXISTS (SELECT 1 FROM city_open_world_service_providers WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V7+ service providers are missing' USING ERRCODE = '23514'; END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_impact_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT; world_tick BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10') THEN RETURN; END IF;
    SELECT baseline_tick INTO profile_tick FROM city_open_world_impact_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V8+ impact profile is missing or has an invalid baseline' USING ERRCODE = '23514'; END IF;
    IF (SELECT COUNT(*) FROM city_open_world_impact_catalog WHERE world_id = target_world_id) <> 8 THEN
        RAISE EXCEPTION 'city open-world V8+ impact catalog is incomplete' USING ERRCODE = '23514'; END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_arrival_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT; world_tick BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v10' THEN RETURN; END IF;
    SELECT baseline_tick INTO profile_tick FROM city_open_world_mobility_arrival_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V10 arrival profile is missing or has an invalid baseline' USING ERRCODE = '23514'; END IF;
    IF EXISTS (
        SELECT 1 FROM city_open_world_mobility_arrival_profiles profile
        WHERE profile.world_id = target_world_id
          AND (profile.arrival_count <> (SELECT COUNT(*) FROM city_open_world_mobility_arrivals WHERE world_id = target_world_id)
               OR profile.landed_count <> (SELECT COUNT(*) FROM city_open_world_mobility_arrivals WHERE world_id = target_world_id AND status = 'landed')
               OR profile.failed_count <> (SELECT COUNT(*) FROM city_open_world_mobility_arrivals WHERE world_id = target_world_id AND status = 'failed')
               OR profile.blocked_count <> COALESCE((SELECT SUM(blocked_attempts) FROM city_open_world_mobility_arrivals WHERE world_id = target_world_id), 0))
    ) THEN
        RAISE EXCEPTION 'city open-world V10 arrival profile counters are inconsistent' USING ERRCODE = '23514'; END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_world_version_vector(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); vector_generation SMALLINT; vector_version VARCHAR(32);
BEGIN
    SELECT simulation_version, version_vector_generation INTO world_version, vector_generation FROM city_worlds WHERE id = target_world_id;
    IF world_version !~ '^city-openworld-v[0-9]+$' OR vector_generation < 1 THEN
        RAISE EXCEPTION 'city world version vector only applies to versioned open worlds' USING ERRCODE = '23514'; END IF;
    SELECT engine_version INTO vector_version FROM city_world_version_vectors WHERE world_id = target_world_id AND generation = vector_generation;
    IF vector_version IS DISTINCT FROM world_version THEN
        RAISE EXCEPTION 'city world active version vector header is missing or stale' USING ERRCODE = '23514'; END IF;
    IF (SELECT COUNT(*) FROM city_world_version_bindings WHERE world_id = target_world_id AND generation = vector_generation) <> 7 THEN
        RAISE EXCEPTION 'city open-world version vector requires exactly seven bindings' USING ERRCODE = '23514'; END IF;
    IF EXISTS (SELECT 1 FROM (VALUES ('content_catalog'), ('economic_policy'), ('engine'), ('rule_bundle'), ('scenario'), ('spatial_profile'), ('worldgen_plan')) AS required(component_code)
        WHERE NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id AND binding.generation = vector_generation AND binding.component_code = required.component_code)) THEN
        RAISE EXCEPTION 'city open-world version vector is incomplete' USING ERRCODE = '23514'; END IF;
    IF world_version = 'city-openworld-v8' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-impact-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V8 impact version vector is incomplete' USING ERRCODE = '23514'; END IF;
    IF world_version = 'city-openworld-v9' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-mobility-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V9 mobility version vector is incomplete' USING ERRCODE = '23514'; END IF;
    IF world_version = 'city-openworld-v10' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-mobility-arrival-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V10 arrival version vector is incomplete' USING ERRCODE = '23514'; END IF;
END;
$$;
