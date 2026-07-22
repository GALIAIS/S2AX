-- city-openworld-v13 consumes V12's sealed residence/employment bindings
-- through verified, dual-direction commute sources. It never changes the V9
-- route allocator or V10 arrival bridge: this migration owns only source
-- eligibility, cadence, evidence, and closed-cycle observations.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v13', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","version_vector","facilities","npc_lod","social_service_catalog","social_service_access","social_service_queue","social_service_response","cross_domain_impact_bridge","aggregate_mobility_graph","mobility_capacity_allocation","mobility_route_lifecycle","mobility_arrival_bridge","cross_scale_spatial_transfer","versioned_od_sources","automatic_od_demands","mobility_cycle_metrics","residence_employment_bindings","verified_commute_sources","facility_presence_origin_validation","dual_direction_commutes","delayed_effects","domain_metrics","activities","rules","case_lifecycle","rewards","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v12', 'city-openworld-v13', 'openworld_v12_to_v13')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_commute_source_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_commute_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    generation_contract VARCHAR(96) NOT NULL,
    origin_contract VARCHAR(96) NOT NULL,
    period_ticks BIGINT NOT NULL CHECK (period_ticks BETWEEN 2 AND 1000000),
    surface_egress_radius BIGINT NOT NULL CHECK (surface_egress_radius BETWEEN 0 AND 1000000),
    maximum_generations_tick INTEGER NOT NULL CHECK (maximum_generations_tick BETWEEN 1 AND 100000),
    source_count BIGINT NOT NULL DEFAULT 0 CHECK (source_count >= 0),
    generated_count BIGINT NOT NULL DEFAULT 0 CHECK (generated_count >= 0),
    suppressed_count BIGINT NOT NULL DEFAULT 0 CHECK (suppressed_count >= 0),
    metric_count BIGINT NOT NULL DEFAULT 0 CHECK (metric_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_commute_source_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-commute-source'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND generation_contract = 'verified_facility_presence_od_v1'
        AND origin_contract = 'facility_interior_or_surface_egress_v1'
        AND period_ticks = 24
        AND surface_egress_radius = 24
        AND maximum_generations_tick = 128
        AND source_count % 2 = 0
    ),
    CONSTRAINT city_open_world_commute_source_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_commute_sources (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_commute_source_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    binding_code VARCHAR(160) NOT NULL,
    source_kind VARCHAR(64) NOT NULL,
    direction VARCHAR(16) NOT NULL,
    actor_id BIGINT NOT NULL,
    employment_role_code VARCHAR(96) NOT NULL,
    origin_facility_code VARCHAR(160) NOT NULL,
    origin_hub_code VARCHAR(160) NOT NULL,
    destination_facility_code VARCHAR(160) NOT NULL,
    destination_hub_code VARCHAR(160) NOT NULL,
    mode_code VARCHAR(64) NOT NULL,
    purpose_code VARCHAR(96) NOT NULL,
    requested_units BIGINT NOT NULL CHECK (requested_units BETWEEN 1 AND 1000),
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    period_ticks BIGINT NOT NULL CHECK (period_ticks BETWEEN 2 AND 1000000),
    phase_offset BIGINT NOT NULL CHECK (phase_offset >= 0),
    next_due_tick BIGINT NOT NULL CHECK (next_due_tick > 0),
    last_transition_tick BIGINT NOT NULL CHECK (last_transition_tick >= 0),
    last_fact_id BIGINT,
    generated_count BIGINT NOT NULL DEFAULT 0 CHECK (generated_count >= 0),
    suppressed_count BIGINT NOT NULL DEFAULT 0 CHECK (suppressed_count >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_commute_source_binding_fk
        FOREIGN KEY (world_id, binding_code)
        REFERENCES city_open_world_commute_bindings(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_source_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_source_origin_facility_fk
        FOREIGN KEY (world_id, origin_facility_code)
        REFERENCES city_open_world_facilities(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_source_destination_facility_fk
        FOREIGN KEY (world_id, destination_facility_code)
        REFERENCES city_open_world_facilities(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_source_origin_hub_fk
        FOREIGN KEY (world_id, origin_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_source_destination_hub_fk
        FOREIGN KEY (world_id, destination_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_source_mode_fk
        FOREIGN KEY (world_id, mode_code)
        REFERENCES city_open_world_mobility_modes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_source_last_fact_fk
        FOREIGN KEY (last_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_commute_source_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND binding_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND employment_role_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND origin_facility_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND origin_hub_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND destination_facility_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND destination_hub_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND origin_facility_code <> destination_facility_code
        AND mode_code = 'walk'
        AND requested_units = 1
        AND status = 'active'
        AND period_ticks = 24
        AND phase_offset < period_ticks
        AND next_due_tick > last_transition_tick
        AND version = 1 + generated_count + suppressed_count
        AND ((direction = 'outbound' AND source_kind = 'npc.residence_to_work' AND purpose_code = 'routine.commute.outbound')
             OR (direction = 'return' AND source_kind = 'npc.work_to_residence' AND purpose_code = 'routine.commute.return'))
    ),
    CONSTRAINT city_open_world_commute_source_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_commute_sources_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_commute_sources_world_binding_direction_unique UNIQUE (world_id, binding_code, direction),
    CONSTRAINT city_open_world_commute_sources_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_commute_sources_due
    ON city_open_world_commute_sources (world_id, status, next_due_tick, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_commute_sources_actor
    ON city_open_world_commute_sources (world_id, actor_id, status);

CREATE TABLE IF NOT EXISTS city_open_world_commute_cycle_metrics (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_commute_source_profiles(world_id) ON DELETE RESTRICT,
    cycle_start_tick BIGINT NOT NULL CHECK (cycle_start_tick > 0),
    cycle_end_tick BIGINT NOT NULL,
    closed_tick BIGINT NOT NULL,
    source_fact_id BIGINT NOT NULL,
    outbound_generated_count BIGINT NOT NULL CHECK (outbound_generated_count >= 0),
    outbound_suppressed_count BIGINT NOT NULL CHECK (outbound_suppressed_count >= 0),
    outbound_origin_unavailable_count BIGINT NOT NULL CHECK (outbound_origin_unavailable_count >= 0),
    return_generated_count BIGINT NOT NULL CHECK (return_generated_count >= 0),
    return_suppressed_count BIGINT NOT NULL CHECK (return_suppressed_count >= 0),
    return_origin_unavailable_count BIGINT NOT NULL CHECK (return_origin_unavailable_count >= 0),
    scheduled_demand_count BIGINT NOT NULL CHECK (scheduled_demand_count >= 0),
    completed_demand_count BIGINT NOT NULL CHECK (completed_demand_count >= 0),
    expired_demand_count BIGINT NOT NULL CHECK (expired_demand_count >= 0),
    pending_demand_count BIGINT NOT NULL CHECK (pending_demand_count >= 0),
    arrival_landed_count BIGINT NOT NULL CHECK (arrival_landed_count >= 0),
    arrival_blocked_count BIGINT NOT NULL CHECK (arrival_blocked_count >= 0),
    arrival_failed_count BIGINT NOT NULL CHECK (arrival_failed_count >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, cycle_start_tick),
    CONSTRAINT city_open_world_commute_cycle_metric_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_commute_cycle_metric_window_check CHECK (
        cycle_end_tick >= cycle_start_tick
        AND closed_tick = cycle_end_tick + 1
        AND cycle_end_tick - cycle_start_tick + 1 = 24
    ),
    CONSTRAINT city_open_world_commute_cycle_metric_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_commute_cycle_metrics_closed
    ON city_open_world_commute_cycle_metrics (world_id, closed_tick);

CREATE OR REPLACE FUNCTION city_open_world_commute_source_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_source_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v13'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v13'
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_source_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_source_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-v13'
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_commute_source_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_commute_source_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_commute_source_write_enabled(target_world_id)
       AND NEW.revision = OLD.revision + 1
       AND NEW.generated_count >= OLD.generated_count
       AND NEW.suppressed_count >= OLD.suppressed_count
       AND NEW.metric_count >= OLD.metric_count
       AND (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.generation_contract, OLD.origin_contract,
            OLD.period_ticks, OLD.surface_egress_radius, OLD.maximum_generations_tick,
            OLD.source_count, OLD.metadata, OLD.created_at)
           IS NOT DISTINCT FROM
           (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.generation_contract, NEW.origin_contract,
            NEW.period_ticks, NEW.surface_egress_radius, NEW.maximum_generations_tick,
            NEW.source_count, NEW.metadata, NEW.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world commute source profile may only advance audited runtime counters'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_commute_source()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
DECLARE baseline_tick_value BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' THEN
        SELECT baseline_tick INTO baseline_tick_value
        FROM city_open_world_commute_source_profiles WHERE world_id = target_world_id;
        IF city_open_world_commute_source_bootstrap_write_enabled(target_world_id)
           AND NEW.last_fact_id IS NULL
           AND NEW.generated_count = 0 AND NEW.suppressed_count = 0
           AND NEW.version = 1
           AND NEW.last_transition_tick = baseline_tick_value
           AND NEW.next_due_tick = baseline_tick_value + 1 + NEW.phase_offset THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'open-world commute source must be initialized from the sealed V13 baseline'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'DELETE' OR NOT city_open_world_commute_source_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world commute sources require a runtime fact or recovery context'
            USING ERRCODE = '55000';
    END IF;
    IF (OLD.world_id, OLD.code, OLD.binding_code, OLD.source_kind, OLD.direction,
        OLD.actor_id, OLD.employment_role_code, OLD.origin_facility_code,
        OLD.origin_hub_code, OLD.destination_facility_code, OLD.destination_hub_code,
        OLD.mode_code, OLD.purpose_code, OLD.requested_units, OLD.status,
        OLD.period_ticks, OLD.phase_offset, OLD.metadata, OLD.created_at)
       IS DISTINCT FROM
       (NEW.world_id, NEW.code, NEW.binding_code, NEW.source_kind, NEW.direction,
        NEW.actor_id, NEW.employment_role_code, NEW.origin_facility_code,
        NEW.origin_hub_code, NEW.destination_facility_code, NEW.destination_hub_code,
        NEW.mode_code, NEW.purpose_code, NEW.requested_units, NEW.status,
        NEW.period_ticks, NEW.phase_offset, NEW.metadata, NEW.created_at)
       OR NEW.version <> OLD.version + 1
       OR NEW.last_transition_tick < OLD.last_transition_tick
       OR NEW.next_due_tick <> NEW.last_transition_tick + OLD.period_ticks THEN
        RAISE EXCEPTION 'open-world commute source identity or cadence is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.generated_count = OLD.generated_count + 1
       AND NEW.suppressed_count = OLD.suppressed_count
       AND EXISTS (
            SELECT 1 FROM city_open_world_runtime_facts fact
            WHERE fact.id = NEW.last_fact_id AND fact.world_id = NEW.world_id
              AND fact.actor_id = NEW.actor_id AND fact.tick = NEW.last_transition_tick
              AND fact.fact_type = 'system.commute.source.generated'
       ) THEN
        RETURN NEW;
    END IF;
    IF NEW.generated_count = OLD.generated_count
       AND NEW.suppressed_count = OLD.suppressed_count + 1
       AND EXISTS (
            SELECT 1 FROM city_open_world_runtime_facts fact
            WHERE fact.id = NEW.last_fact_id AND fact.world_id = NEW.world_id
              AND fact.actor_id = NEW.actor_id AND fact.tick = NEW.last_transition_tick
              AND fact.fact_type = 'system.commute.source.suppressed'
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world commute source transition is invalid'
        USING ERRCODE = '23514';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_commute_cycle_metric()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_commute_source_write_enabled(target_world_id)
       AND EXISTS (
            SELECT 1 FROM city_open_world_runtime_facts fact
            WHERE fact.id = NEW.source_fact_id AND fact.world_id = NEW.world_id
              AND fact.tick = NEW.closed_tick
              AND fact.fact_type = 'system.commute.source.cycle.closed'
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world commute cycle metrics are immutable audited evidence'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_commute_source_profile_guard ON city_open_world_commute_source_profiles;
CREATE TRIGGER city_open_world_commute_source_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_commute_source_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_commute_source_profile();

DROP TRIGGER IF EXISTS city_open_world_commute_source_guard ON city_open_world_commute_sources;
CREATE TRIGGER city_open_world_commute_source_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_commute_sources
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_commute_source();

DROP TRIGGER IF EXISTS city_open_world_commute_cycle_metric_guard ON city_open_world_commute_cycle_metrics;
CREATE TRIGGER city_open_world_commute_cycle_metric_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_commute_cycle_metrics
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_commute_cycle_metric();

CREATE OR REPLACE FUNCTION assert_city_open_world_commute_source_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT; world_tick BIGINT;
DECLARE source_total BIGINT; generated_total BIGINT; suppressed_total BIGINT; metric_total BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v13' THEN RETURN; END IF;
    SELECT baseline_tick, source_count, generated_count, suppressed_count, metric_count
    INTO profile_tick, source_total, generated_total, suppressed_total, metric_total
    FROM city_open_world_commute_source_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick OR source_total IS NULL OR source_total % 2 <> 0 THEN
        RAISE EXCEPTION 'city open-world V13 commute source profile is missing or has an invalid baseline' USING ERRCODE = '23514';
    END IF;
    IF source_total <> (SELECT COUNT(*) FROM city_open_world_commute_sources WHERE world_id = target_world_id)
       OR generated_total <> COALESCE((SELECT SUM(generated_count) FROM city_open_world_commute_sources WHERE world_id = target_world_id), 0)
       OR suppressed_total <> COALESCE((SELECT SUM(suppressed_count) FROM city_open_world_commute_sources WHERE world_id = target_world_id), 0)
       OR metric_total <> (SELECT COUNT(*) FROM city_open_world_commute_cycle_metrics WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V13 commute source counters are inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_commute_bindings binding
        WHERE binding.world_id = target_world_id
          AND (NOT EXISTS (
                SELECT 1 FROM city_open_world_commute_sources source
                WHERE source.world_id = binding.world_id AND source.binding_code = binding.code
                  AND source.direction = 'outbound'
              ) OR NOT EXISTS (
                SELECT 1 FROM city_open_world_commute_sources source
                WHERE source.world_id = binding.world_id AND source.binding_code = binding.code
                  AND source.direction = 'return'
              ))
    ) THEN
        RAISE EXCEPTION 'city open-world V13 commute binding lacks a directional source pair' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_commute_sources source
        JOIN city_open_world_commute_bindings binding
          ON binding.world_id = source.world_id AND binding.code = source.binding_code
        JOIN city_open_world_facilities origin_facility
          ON origin_facility.world_id = source.world_id AND origin_facility.code = source.origin_facility_code
        JOIN city_open_world_facilities destination_facility
          ON destination_facility.world_id = source.world_id AND destination_facility.code = source.destination_facility_code
        JOIN city_open_world_mobility_hubs origin_hub
          ON origin_hub.world_id = source.world_id AND origin_hub.code = source.origin_hub_code
        JOIN city_open_world_mobility_hubs destination_hub
          ON destination_hub.world_id = source.world_id AND destination_hub.code = source.destination_hub_code
        WHERE source.world_id = target_world_id
          AND (source.actor_id <> binding.actor_id
               OR source.employment_role_code <> binding.employment_role_code
               OR origin_hub.hub_kind <> 'facility' OR origin_hub.facility_code <> origin_facility.code
               OR destination_hub.hub_kind <> 'facility' OR destination_hub.facility_code <> destination_facility.code
               OR (source.direction = 'outbound' AND (
                    source.origin_facility_code <> binding.home_facility_code
                    OR source.origin_hub_code <> binding.home_hub_code
                    OR source.destination_facility_code <> binding.work_facility_code
                    OR source.destination_hub_code <> binding.work_hub_code
                    OR origin_facility.facility_type_code <> 'residence'
                    OR destination_facility.facility_type_code = 'residence'))
               OR (source.direction = 'return' AND (
                    source.origin_facility_code <> binding.work_facility_code
                    OR source.origin_hub_code <> binding.work_hub_code
                    OR source.destination_facility_code <> binding.home_facility_code
                    OR source.destination_hub_code <> binding.home_hub_code
                    OR origin_facility.facility_type_code = 'residence'
                    OR destination_facility.facility_type_code <> 'residence')))
    ) THEN
        RAISE EXCEPTION 'city open-world V13 commute source identity is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_open_world_commute_cycle_metrics metric
        JOIN city_open_world_runtime_facts fact
          ON fact.id = metric.source_fact_id AND fact.world_id = metric.world_id
        WHERE metric.world_id = target_world_id
          AND (fact.fact_type <> 'system.commute.source.cycle.closed' OR fact.tick <> metric.closed_tick)
    ) THEN
        RAISE EXCEPTION 'city open-world V13 commute metric evidence is invalid' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_open_world_commute_source_foundation_commit()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    PERFORM assert_city_open_world_commute_source_foundation(target_world_id);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_commute_source_profile_commit_check ON city_open_world_commute_source_profiles;
CREATE CONSTRAINT TRIGGER city_open_world_commute_source_profile_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_commute_source_profiles
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_open_world_commute_source_foundation_commit();

DROP TRIGGER IF EXISTS city_open_world_commute_source_commit_check ON city_open_world_commute_sources;
CREATE CONSTRAINT TRIGGER city_open_world_commute_source_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_commute_sources
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_open_world_commute_source_foundation_commit();

DROP TRIGGER IF EXISTS city_open_world_commute_cycle_metric_commit_check ON city_open_world_commute_cycle_metrics;
CREATE CONSTRAINT TRIGGER city_open_world_commute_cycle_metric_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_commute_cycle_metrics
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_open_world_commute_source_foundation_commit();

-- V13 is a strict successor. Redefine predecessor gates so a V13 genesis or
-- paused V12→V13 upgrade can reuse the sealed lower-level baselines without a
-- permissive write path.
CREATE OR REPLACE FUNCTION city_open_world_commute_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v12','city-openworld-v13')
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

CREATE OR REPLACE FUNCTION assert_city_open_world_commute_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version VARCHAR(32); world_tick BIGINT; profile_tick BIGINT;
    candidate_total BIGINT; binding_total BIGINT; unbound_total BIGINT;
    residence_total BIGINT; used_units_total BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v12','city-openworld-v13') THEN RETURN; END IF;
    SELECT baseline_tick, candidate_count, binding_count, unbound_candidate_count,
           residence_count, used_residence_units
    INTO profile_tick, candidate_total, binding_total, unbound_total,
         residence_total, used_units_total
    FROM city_open_world_commute_profiles WHERE world_id = target_world_id;
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
        JOIN city_open_world_actors actor ON actor.id = binding.actor_id AND actor.world_id = binding.world_id
        JOIN city_open_world_facilities home ON home.world_id = binding.world_id AND home.code = binding.home_facility_code
        JOIN city_open_world_mobility_hubs home_hub ON home_hub.world_id = binding.world_id AND home_hub.code = binding.home_hub_code
        JOIN city_open_world_facilities work ON work.world_id = binding.world_id AND work.code = binding.work_facility_code
        JOIN city_open_world_mobility_hubs work_hub ON work_hub.world_id = binding.world_id AND work_hub.code = binding.work_hub_code
        WHERE binding.world_id = target_world_id
          AND (home.facility_type_code <> 'residence'
               OR home_hub.hub_kind <> 'facility' OR home_hub.facility_id <> home.id OR home_hub.facility_code <> home.code
               OR work.facility_type_code = 'residence'
               OR work_hub.hub_kind <> 'facility' OR work_hub.facility_id <> work.id OR work_hub.facility_code <> work.code
               OR NOT EXISTS (
                    SELECT 1 FROM city_open_world_actor_roles role
                    WHERE role.world_id = binding.world_id AND role.actor_id = binding.actor_id
                      AND role.role_code = binding.employment_role_code AND role.category_code = 'employment'
               ))
    ) THEN
        RAISE EXCEPTION 'city open-world V12 commute binding identity is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_open_world_commute_bindings binding
        JOIN city_open_world_facilities home ON home.world_id = binding.world_id AND home.code = binding.home_facility_code
        WHERE binding.world_id = target_world_id
        GROUP BY binding.home_facility_code, home.capacity_units HAVING COUNT(*) > MAX(home.capacity_units)
    ) THEN
        RAISE EXCEPTION 'city open-world V12 residence capacity is exceeded' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_open_world_commute_bindings binding
        JOIN city_open_world_facilities work ON work.world_id = binding.world_id AND work.code = binding.work_facility_code
        WHERE binding.world_id = target_world_id
        GROUP BY binding.work_facility_code, work.capacity_units HAVING COUNT(*) > MAX(work.capacity_units)
    ) THEN
        RAISE EXCEPTION 'city open-world V12 work capacity is exceeded' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13')
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
                   AND world.simulation_version IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_arrival_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_arrival_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13')
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
                   AND world.simulation_version IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_od_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_od_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v11','city-openworld-v12','city-openworld-v13')
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
                   AND world.simulation_version IN ('city-openworld-v11','city-openworld-v12','city-openworld-v13'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_service_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13')
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
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_impact_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13')
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
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v1','city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_materialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_materialization_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_fact_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_fact_insert()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE world_tick BIGINT; world_version VARCHAR(32); command_status_value VARCHAR(16);
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN RETURN NEW; END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version FROM city_worlds WHERE id = NEW.world_id;
    IF world_version NOT IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13')
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
       AND NOT (world_version IN ('city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13') AND NEW.fact_type LIKE 'system.%') THEN
        RAISE EXCEPTION 'derived open-world runtime fact requires a parent fact' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_service_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT;
BEGIN
    SELECT simulation_version INTO world_version FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13') THEN RETURN; END IF;
    SELECT baseline_tick INTO profile_tick FROM city_open_world_service_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL THEN RAISE EXCEPTION 'city open-world V7+ service profile is missing' USING ERRCODE = '23514'; END IF;
    IF (SELECT COUNT(*) FROM city_open_world_service_catalog WHERE world_id = target_world_id) <> 4
       OR NOT EXISTS (SELECT 1 FROM city_open_world_service_providers WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V7+ service foundation is incomplete' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_impact_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT; world_tick BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13') THEN RETURN; END IF;
    SELECT baseline_tick INTO profile_tick FROM city_open_world_impact_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V8+ impact profile is missing or has an invalid baseline' USING ERRCODE = '23514';
    END IF;
    IF (SELECT COUNT(*) FROM city_open_world_impact_catalog WHERE world_id = target_world_id) <> 8 THEN
        RAISE EXCEPTION 'city open-world V8+ impact catalog is incomplete' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_mobility_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT; world_tick BIGINT; expected_hash VARCHAR(64);
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13') THEN RETURN; END IF;
    SELECT baseline_tick, content_hash INTO profile_tick, expected_hash FROM city_open_world_mobility_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick OR expected_hash IS NULL THEN
        RAISE EXCEPTION 'city open-world V9+ mobility profile is missing or has an invalid baseline' USING ERRCODE = '23514';
    END IF;
    IF (SELECT COUNT(*) FROM city_open_world_mobility_modes WHERE world_id = target_world_id) <> 3
       OR (SELECT COUNT(*) FROM city_open_world_mobility_hubs WHERE world_id = target_world_id) < 3
       OR (SELECT COUNT(*) FROM city_open_world_mobility_edges WHERE world_id = target_world_id) = 0 THEN
        RAISE EXCEPTION 'city open-world V9+ mobility topology is incomplete' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (SELECT 1 FROM city_open_world_mobility_profiles profile WHERE profile.world_id = target_world_id
        AND (profile.mode_count <> (SELECT COUNT(*) FROM city_open_world_mobility_modes WHERE world_id = target_world_id)
             OR profile.hub_count <> (SELECT COUNT(*) FROM city_open_world_mobility_hubs WHERE world_id = target_world_id)
             OR profile.edge_count <> (SELECT COUNT(*) FROM city_open_world_mobility_edges WHERE world_id = target_world_id)
             OR profile.demand_count <> (SELECT COUNT(*) FROM city_open_world_mobility_demands WHERE world_id = target_world_id)
             OR profile.route_count <> (SELECT COUNT(*) FROM city_open_world_mobility_routes WHERE world_id = target_world_id)
             OR profile.allocation_count <> (SELECT COUNT(*) FROM city_open_world_mobility_allocations WHERE world_id = target_world_id)
             OR profile.actor_metric_count <> (SELECT COUNT(*) FROM city_open_world_mobility_actor_metrics WHERE world_id = target_world_id))) THEN
        RAISE EXCEPTION 'city open-world V9+ mobility profile counters are inconsistent' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_arrival_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT; world_tick BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13') THEN RETURN; END IF;
    SELECT baseline_tick INTO profile_tick FROM city_open_world_mobility_arrival_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V10+ arrival profile is missing or has an invalid baseline' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (SELECT 1 FROM city_open_world_mobility_arrival_profiles profile WHERE profile.world_id = target_world_id
        AND (profile.arrival_count <> (SELECT COUNT(*) FROM city_open_world_mobility_arrivals WHERE world_id = target_world_id)
             OR profile.landed_count <> (SELECT COUNT(*) FROM city_open_world_mobility_arrivals WHERE world_id = target_world_id AND status = 'landed')
             OR profile.failed_count <> (SELECT COUNT(*) FROM city_open_world_mobility_arrivals WHERE world_id = target_world_id AND status = 'failed')
             OR profile.blocked_count <> COALESCE((SELECT SUM(blocked_attempts) FROM city_open_world_mobility_arrivals WHERE world_id = target_world_id), 0))) THEN
        RAISE EXCEPTION 'city open-world V10+ arrival profile counters are inconsistent' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_mobility_od_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT; world_tick BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v11','city-openworld-v12','city-openworld-v13') THEN RETURN; END IF;
    SELECT baseline_tick INTO profile_tick FROM city_open_world_mobility_od_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V11+ OD profile is missing or has an invalid baseline' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (SELECT 1 FROM city_open_world_mobility_od_profiles profile WHERE profile.world_id = target_world_id
        AND (profile.source_count <> (SELECT COUNT(*) FROM city_open_world_mobility_od_sources WHERE world_id = target_world_id)
             OR profile.generated_count <> COALESCE((SELECT SUM(generated_count) FROM city_open_world_mobility_od_sources WHERE world_id = target_world_id), 0)
             OR profile.suppressed_count <> COALESCE((SELECT SUM(suppressed_count) FROM city_open_world_mobility_od_sources WHERE world_id = target_world_id), 0)
             OR profile.metric_count <> (SELECT COUNT(*) FROM city_open_world_mobility_od_cycle_metrics WHERE world_id = target_world_id))) THEN
        RAISE EXCEPTION 'city open-world V11+ OD profile counters are inconsistent' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_world_version_vector(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); vector_generation SMALLINT; vector_version VARCHAR(32);
BEGIN
    SELECT simulation_version, version_vector_generation INTO world_version, vector_generation FROM city_worlds WHERE id = target_world_id;
    IF world_version !~ '^city-openworld-v[0-9]+$' OR vector_generation < 1 THEN
        RAISE EXCEPTION 'city world version vector only applies to versioned open worlds' USING ERRCODE = '23514';
    END IF;
    SELECT engine_version INTO vector_version FROM city_world_version_vectors WHERE world_id = target_world_id AND generation = vector_generation;
    IF vector_version IS DISTINCT FROM world_version THEN
        RAISE EXCEPTION 'city world active version vector header is missing or stale' USING ERRCODE = '23514';
    END IF;
    IF (SELECT COUNT(*) FROM city_world_version_bindings WHERE world_id = target_world_id AND generation = vector_generation) <> 7 THEN
        RAISE EXCEPTION 'city open-world version vector requires exactly seven bindings' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (SELECT 1 FROM (VALUES ('content_catalog'), ('economic_policy'), ('engine'), ('rule_bundle'), ('scenario'), ('spatial_profile'), ('worldgen_plan')) AS required(component_code)
        WHERE NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id AND binding.generation = vector_generation AND binding.component_code = required.component_code)) THEN
        RAISE EXCEPTION 'city open-world version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v8' AND NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id AND binding.generation = vector_generation AND binding.component_code = 'content_catalog' AND binding.bundle_id = 'sub2api-open-world-impact-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V8 impact version vector is incomplete' USING ERRCODE = '23514'; END IF;
    IF world_version = 'city-openworld-v9' AND NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id AND binding.generation = vector_generation AND binding.component_code = 'content_catalog' AND binding.bundle_id = 'sub2api-open-world-mobility-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V9 mobility version vector is incomplete' USING ERRCODE = '23514'; END IF;
    IF world_version = 'city-openworld-v10' AND NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id AND binding.generation = vector_generation AND binding.component_code = 'content_catalog' AND binding.bundle_id = 'sub2api-open-world-mobility-arrival-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V10 arrival version vector is incomplete' USING ERRCODE = '23514'; END IF;
    IF world_version = 'city-openworld-v11' AND NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id AND binding.generation = vector_generation AND binding.component_code = 'content_catalog' AND binding.bundle_id = 'sub2api-open-world-mobility-od-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V11 OD version vector is incomplete' USING ERRCODE = '23514'; END IF;
    IF world_version = 'city-openworld-v12' AND NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id AND binding.generation = vector_generation AND binding.component_code = 'content_catalog' AND binding.bundle_id = 'sub2api-open-world-commute-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V12 commute version vector is incomplete' USING ERRCODE = '23514'; END IF;
    IF world_version = 'city-openworld-v13' AND NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id AND binding.generation = vector_generation AND binding.component_code = 'content_catalog' AND binding.bundle_id = 'sub2api-open-world-commute-source-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V13 commute source version vector is incomplete' USING ERRCODE = '23514'; END IF;
END;
$$;
