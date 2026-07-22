-- city-openworld-v12 freezes an authoritative, capacity-limited residence /
-- employment binding for eligible V5 NPCs. It deliberately does not create a
-- second demand stream yet: V13+ must consume these bindings through explicit
-- source facts and verified local origin checks.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v12', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","version_vector","facilities","npc_lod","social_service_catalog","social_service_access","social_service_queue","social_service_response","cross_domain_impact_bridge","aggregate_mobility_graph","mobility_capacity_allocation","mobility_route_lifecycle","mobility_arrival_bridge","cross_scale_spatial_transfer","versioned_od_sources","automatic_od_demands","mobility_cycle_metrics","residence_employment_bindings","capacity_limited_residency","delayed_effects","domain_metrics","activities","rules","case_lifecycle","rewards","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v11', 'city-openworld-v12', 'openworld_v11_to_v12')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_commute_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_mobility_od_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    assignment_contract VARCHAR(96) NOT NULL,
    period_ticks BIGINT NOT NULL CHECK (period_ticks BETWEEN 2 AND 1000000),
    maximum_bindings INTEGER NOT NULL CHECK (maximum_bindings BETWEEN 1 AND 100000),
    candidate_count BIGINT NOT NULL DEFAULT 0 CHECK (candidate_count >= 0),
    binding_count BIGINT NOT NULL DEFAULT 0 CHECK (binding_count >= 0),
    unbound_candidate_count BIGINT NOT NULL DEFAULT 0 CHECK (unbound_candidate_count >= 0),
    residence_count BIGINT NOT NULL DEFAULT 0 CHECK (residence_count >= 0),
    used_residence_units BIGINT NOT NULL DEFAULT 0 CHECK (used_residence_units >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_commute_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-commute-binding'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND assignment_contract = 'deterministic_capacity_residence_assignment_v1'
        AND period_ticks = 24
        AND maximum_bindings = 4096
        AND candidate_count = binding_count + unbound_candidate_count
        AND binding_count <= maximum_bindings
        AND used_residence_units = binding_count
        AND revision = 1
    ),
    CONSTRAINT city_open_world_commute_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_commute_bindings (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_commute_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    binding_kind VARCHAR(64) NOT NULL,
    actor_id BIGINT NOT NULL,
    employment_role_code VARCHAR(96) NOT NULL,
    home_facility_code VARCHAR(160) NOT NULL,
    home_hub_code VARCHAR(160) NOT NULL,
    work_facility_code VARCHAR(160) NOT NULL,
    work_hub_code VARCHAR(160) NOT NULL,
    period_ticks BIGINT NOT NULL CHECK (period_ticks BETWEEN 2 AND 1000000),
    outbound_phase BIGINT NOT NULL CHECK (outbound_phase >= 0),
    return_phase BIGINT NOT NULL CHECK (return_phase >= 0),
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_commute_binding_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_binding_home_facility_fk
        FOREIGN KEY (world_id, home_facility_code)
        REFERENCES city_open_world_facilities(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_binding_work_facility_fk
        FOREIGN KEY (world_id, work_facility_code)
        REFERENCES city_open_world_facilities(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_binding_home_hub_fk
        FOREIGN KEY (world_id, home_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_binding_work_hub_fk
        FOREIGN KEY (world_id, work_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_binding_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND binding_kind = 'npc.residence_employment'
        AND employment_role_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND home_facility_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND home_hub_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND work_facility_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND work_hub_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND home_facility_code <> work_facility_code
        AND period_ticks = 24
        AND outbound_phase < period_ticks
        AND return_phase < period_ticks
        AND return_phase = (outbound_phase + period_ticks / 2) % period_ticks
        AND status = 'active'
        AND version = 1
    ),
    CONSTRAINT city_open_world_commute_binding_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_commute_bindings_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_commute_bindings_world_actor_unique UNIQUE (world_id, actor_id),
    CONSTRAINT city_open_world_commute_bindings_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_commute_bindings_home
    ON city_open_world_commute_bindings (world_id, home_facility_code, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_commute_bindings_work
    ON city_open_world_commute_bindings (world_id, work_facility_code, code);

CREATE OR REPLACE FUNCTION city_open_world_commute_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v12'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v12'
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_commute_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_commute_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world commute profiles are immutable outside V12 genesis or audited upgrade'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_commute_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_commute_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world commute bindings are immutable audited evidence'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_commute_profile_guard ON city_open_world_commute_profiles;
CREATE TRIGGER city_open_world_commute_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_commute_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_commute_profile();

DROP TRIGGER IF EXISTS city_open_world_commute_binding_guard ON city_open_world_commute_bindings;
CREATE TRIGGER city_open_world_commute_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_commute_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_commute_binding();

CREATE OR REPLACE FUNCTION assert_city_open_world_commute_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    profile_tick BIGINT;
    candidate_total BIGINT;
    binding_total BIGINT;
    unbound_total BIGINT;
    residence_total BIGINT;
    used_units_total BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v12' THEN
        RETURN;
    END IF;
    SELECT baseline_tick, candidate_count, binding_count, unbound_candidate_count,
           residence_count, used_residence_units
    INTO profile_tick, candidate_total, binding_total, unbound_total,
         residence_total, used_units_total
    FROM city_open_world_commute_profiles
    WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V12 commute profile is missing or has an invalid baseline' USING ERRCODE = '23514';
    END IF;
    IF candidate_total <> binding_total + unbound_total
       OR used_units_total <> binding_total
       OR binding_total <> (SELECT COUNT(*) FROM city_open_world_commute_bindings WHERE world_id = target_world_id)
       OR residence_total < 0 THEN
        RAISE EXCEPTION 'city open-world V12 commute profile counters are inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_commute_bindings binding
        JOIN city_open_world_actors actor
          ON actor.id = binding.actor_id AND actor.world_id = binding.world_id
        JOIN city_open_world_facilities home
          ON home.world_id = binding.world_id AND home.code = binding.home_facility_code
        JOIN city_open_world_mobility_hubs home_hub
          ON home_hub.world_id = binding.world_id AND home_hub.code = binding.home_hub_code
        JOIN city_open_world_facilities work
          ON work.world_id = binding.world_id AND work.code = binding.work_facility_code
        JOIN city_open_world_mobility_hubs work_hub
          ON work_hub.world_id = binding.world_id AND work_hub.code = binding.work_hub_code
        WHERE binding.world_id = target_world_id
          AND (home.facility_type_code <> 'residence'
               OR home_hub.hub_kind <> 'facility' OR home_hub.facility_id <> home.id
               OR home_hub.facility_code <> home.code
               OR work.facility_type_code = 'residence'
               OR work_hub.hub_kind <> 'facility' OR work_hub.facility_id <> work.id
               OR work_hub.facility_code <> work.code
               OR NOT EXISTS (
                    SELECT 1 FROM city_open_world_actor_roles role
                    WHERE role.world_id = binding.world_id AND role.actor_id = binding.actor_id
                      AND role.role_code = binding.employment_role_code
                      AND role.category_code = 'employment'
               ))
    ) THEN
        RAISE EXCEPTION 'city open-world V12 commute binding identity is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_commute_bindings binding
        JOIN city_open_world_facilities home
          ON home.world_id = binding.world_id AND home.code = binding.home_facility_code
        WHERE binding.world_id = target_world_id
        GROUP BY binding.home_facility_code, home.capacity_units
        HAVING COUNT(*) > MAX(home.capacity_units)
    ) THEN
        RAISE EXCEPTION 'city open-world V12 residence capacity is exceeded' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_commute_bindings binding
        JOIN city_open_world_facilities work
          ON work.world_id = binding.world_id AND work.code = binding.work_facility_code
        WHERE binding.world_id = target_world_id
        GROUP BY binding.work_facility_code, work.capacity_units
        HAVING COUNT(*) > MAX(work.capacity_units)
    ) THEN
        RAISE EXCEPTION 'city open-world V12 work capacity is exceeded' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_open_world_commute_foundation_commit()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    PERFORM assert_city_open_world_commute_foundation(target_world_id);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_commute_profile_commit_check ON city_open_world_commute_profiles;
CREATE CONSTRAINT TRIGGER city_open_world_commute_profile_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_commute_profiles
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_open_world_commute_foundation_commit();

DROP TRIGGER IF EXISTS city_open_world_commute_binding_commit_check ON city_open_world_commute_bindings;
CREATE CONSTRAINT TRIGGER city_open_world_commute_binding_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_commute_bindings
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_open_world_commute_foundation_commit();

-- V12 is a strict successor. Re-declare predecessor gates rather than relying
-- on a permissive fallback, so new-world genesis and paused upgrade remain
-- constrained to the complete V12 dependency chain.
CREATE OR REPLACE FUNCTION city_open_world_mobility_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (
                        SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_arrival_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_arrival_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (
                        SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_arrival_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_arrival_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_od_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_od_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v11','city-openworld-v12')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (
                        SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_od_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_od_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v11','city-openworld-v12'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_service_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (
                        SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_impact_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (
                        SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v1','city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_materialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_materialization_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_fact_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_fact_insert()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE world_tick BIGINT; world_version VARCHAR(32); command_status_value VARCHAR(16);
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN RETURN NEW; END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version FROM city_worlds WHERE id = NEW.world_id;
    IF world_version NOT IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12')
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
       AND NOT (world_version IN ('city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12') AND NEW.fact_type LIKE 'system.%') THEN
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
    IF world_version NOT IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12') THEN RETURN; END IF;
    SELECT baseline_tick, content_hash INTO profile_tick, expected_hash FROM city_open_world_mobility_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick OR expected_hash IS NULL THEN
        RAISE EXCEPTION 'city open-world V9+ mobility profile is missing or has an invalid baseline' USING ERRCODE = '23514'; END IF;
    IF (SELECT COUNT(*) FROM city_open_world_mobility_modes WHERE world_id = target_world_id) <> 3
       OR (SELECT COUNT(*) FROM city_open_world_mobility_hubs WHERE world_id = target_world_id) < 3
       OR (SELECT COUNT(*) FROM city_open_world_mobility_edges WHERE world_id = target_world_id) = 0 THEN
        RAISE EXCEPTION 'city open-world V9+ mobility topology is incomplete' USING ERRCODE = '23514'; END IF;
    IF EXISTS (SELECT 1 FROM city_open_world_mobility_profiles profile WHERE profile.world_id = target_world_id
        AND (profile.mode_count <> (SELECT COUNT(*) FROM city_open_world_mobility_modes WHERE world_id = target_world_id)
             OR profile.hub_count <> (SELECT COUNT(*) FROM city_open_world_mobility_hubs WHERE world_id = target_world_id)
             OR profile.edge_count <> (SELECT COUNT(*) FROM city_open_world_mobility_edges WHERE world_id = target_world_id)
             OR profile.demand_count <> (SELECT COUNT(*) FROM city_open_world_mobility_demands WHERE world_id = target_world_id)
             OR profile.route_count <> (SELECT COUNT(*) FROM city_open_world_mobility_routes WHERE world_id = target_world_id)
             OR profile.allocation_count <> (SELECT COUNT(*) FROM city_open_world_mobility_allocations WHERE world_id = target_world_id)
             OR profile.actor_metric_count <> (SELECT COUNT(*) FROM city_open_world_mobility_actor_metrics WHERE world_id = target_world_id))) THEN
        RAISE EXCEPTION 'city open-world V9+ mobility profile counters are inconsistent' USING ERRCODE = '23514'; END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_service_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT;
BEGIN
    SELECT simulation_version INTO world_version FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12') THEN RETURN; END IF;
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
    IF world_version NOT IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12') THEN RETURN; END IF;
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
    IF world_version NOT IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12') THEN RETURN; END IF;
    SELECT baseline_tick INTO profile_tick FROM city_open_world_mobility_arrival_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V10+ arrival profile is missing or has an invalid baseline' USING ERRCODE = '23514'; END IF;
    IF EXISTS (SELECT 1 FROM city_open_world_mobility_arrival_profiles profile WHERE profile.world_id = target_world_id
        AND (profile.arrival_count <> (SELECT COUNT(*) FROM city_open_world_mobility_arrivals WHERE world_id = target_world_id)
             OR profile.landed_count <> (SELECT COUNT(*) FROM city_open_world_mobility_arrivals WHERE world_id = target_world_id AND status = 'landed')
             OR profile.failed_count <> (SELECT COUNT(*) FROM city_open_world_mobility_arrivals WHERE world_id = target_world_id AND status = 'failed')
             OR profile.blocked_count <> COALESCE((SELECT SUM(blocked_attempts) FROM city_open_world_mobility_arrivals WHERE world_id = target_world_id), 0))) THEN
        RAISE EXCEPTION 'city open-world V10+ arrival profile counters are inconsistent' USING ERRCODE = '23514'; END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_mobility_od_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT; world_tick BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v11','city-openworld-v12') THEN RETURN; END IF;
    SELECT baseline_tick INTO profile_tick FROM city_open_world_mobility_od_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V11+ OD profile is missing or has an invalid baseline' USING ERRCODE = '23514'; END IF;
    IF EXISTS (SELECT 1 FROM city_open_world_mobility_od_profiles profile WHERE profile.world_id = target_world_id
        AND (profile.source_count <> (SELECT COUNT(*) FROM city_open_world_mobility_od_sources WHERE world_id = target_world_id)
             OR profile.generated_count <> COALESCE((SELECT SUM(generated_count) FROM city_open_world_mobility_od_sources WHERE world_id = target_world_id), 0)
             OR profile.suppressed_count <> COALESCE((SELECT SUM(suppressed_count) FROM city_open_world_mobility_od_sources WHERE world_id = target_world_id), 0)
             OR profile.metric_count <> (SELECT COUNT(*) FROM city_open_world_mobility_od_cycle_metrics WHERE world_id = target_world_id))) THEN
        RAISE EXCEPTION 'city open-world V11+ OD profile counters are inconsistent' USING ERRCODE = '23514'; END IF;
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
    IF world_version = 'city-openworld-v8' AND NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
        AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
        AND binding.bundle_id = 'sub2api-open-world-impact-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V8 impact version vector is incomplete' USING ERRCODE = '23514'; END IF;
    IF world_version = 'city-openworld-v9' AND NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
        AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
        AND binding.bundle_id = 'sub2api-open-world-mobility-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V9 mobility version vector is incomplete' USING ERRCODE = '23514'; END IF;
    IF world_version = 'city-openworld-v10' AND NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
        AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
        AND binding.bundle_id = 'sub2api-open-world-mobility-arrival-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V10 arrival version vector is incomplete' USING ERRCODE = '23514'; END IF;
    IF world_version = 'city-openworld-v11' AND NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
        AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
        AND binding.bundle_id = 'sub2api-open-world-mobility-od-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V11 OD version vector is incomplete' USING ERRCODE = '23514'; END IF;
    IF world_version = 'city-openworld-v12' AND NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
        AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
        AND binding.bundle_id = 'sub2api-open-world-commute-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V12 commute version vector is incomplete' USING ERRCODE = '23514'; END IF;
END;
$$;
