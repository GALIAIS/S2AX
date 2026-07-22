-- city-openworld-v14 adds an append-only successor-epoch lifecycle above the
-- sealed V12 residence/employment binding and V13 commute-source evidence.
-- Historical bindings/sources remain immutable; every administrative change
-- creates a fact-backed transition and, for a rebind, a new assignment epoch.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v14', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","version_vector","facilities","npc_lod","social_service_catalog","social_service_access","social_service_queue","social_service_response","cross_domain_impact_bridge","aggregate_mobility_graph","mobility_capacity_allocation","mobility_route_lifecycle","mobility_arrival_bridge","cross_scale_spatial_transfer","versioned_od_sources","automatic_od_demands","mobility_cycle_metrics","residence_employment_bindings","verified_commute_sources","facility_presence_origin_validation","dual_direction_commutes","commute_assignment_epochs","commute_lifecycle_transitions","commute_rebinding","commute_pause_resume","delayed_effects","domain_metrics","activities","rules","case_lifecycle","rewards","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v13', 'city-openworld-v14', 'openworld_v13_to_v14')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_commute_lifecycle_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_commute_source_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    assignment_contract VARCHAR(96) NOT NULL,
    source_contract VARCHAR(96) NOT NULL,
    period_ticks BIGINT NOT NULL CHECK (period_ticks BETWEEN 2 AND 1000000),
    maximum_assignments INTEGER NOT NULL CHECK (maximum_assignments BETWEEN 1 AND 100000),
    maximum_transitions_tick INTEGER NOT NULL CHECK (maximum_transitions_tick BETWEEN 1 AND 100000),
    maximum_generations_tick INTEGER NOT NULL CHECK (maximum_generations_tick BETWEEN 1 AND 100000),
    assignment_count BIGINT NOT NULL DEFAULT 0 CHECK (assignment_count >= 0),
    active_assignment_count BIGINT NOT NULL DEFAULT 0 CHECK (active_assignment_count >= 0),
    suspended_assignment_count BIGINT NOT NULL DEFAULT 0 CHECK (suspended_assignment_count >= 0),
    superseded_assignment_count BIGINT NOT NULL DEFAULT 0 CHECK (superseded_assignment_count >= 0),
    terminated_assignment_count BIGINT NOT NULL DEFAULT 0 CHECK (terminated_assignment_count >= 0),
    source_count BIGINT NOT NULL DEFAULT 0 CHECK (source_count >= 0),
    generated_count BIGINT NOT NULL DEFAULT 0 CHECK (generated_count >= 0),
    suppressed_count BIGINT NOT NULL DEFAULT 0 CHECK (suppressed_count >= 0),
    transition_count BIGINT NOT NULL DEFAULT 0 CHECK (transition_count >= 0),
    metric_count BIGINT NOT NULL DEFAULT 0 CHECK (metric_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_commute_lifecycle_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-commute-lifecycle'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND assignment_contract = 'immutable_assignment_epoch_lifecycle_v1'
        AND source_contract = 'active_epoch_verified_facility_presence_od_v1'
        AND period_ticks = 24
        AND maximum_assignments = 4096
        AND maximum_transitions_tick = 512
        AND maximum_generations_tick = 128
        AND active_assignment_count + suspended_assignment_count
            + superseded_assignment_count + terminated_assignment_count = assignment_count
        AND source_count = assignment_count * 2
    ),
    CONSTRAINT city_open_world_commute_lifecycle_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_commute_assignment_epochs (
    id BIGSERIAL NOT NULL,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_commute_lifecycle_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    binding_code VARCHAR(160) NOT NULL,
    actor_id BIGINT NOT NULL,
    epoch_number BIGINT NOT NULL CHECK (epoch_number > 0),
    assignment_kind VARCHAR(64) NOT NULL,
    employment_role_code VARCHAR(96) NOT NULL,
    home_facility_code VARCHAR(160) NOT NULL,
    home_hub_code VARCHAR(160) NOT NULL,
    work_facility_code VARCHAR(160) NOT NULL,
    work_hub_code VARCHAR(160) NOT NULL,
    period_ticks BIGINT NOT NULL CHECK (period_ticks BETWEEN 2 AND 1000000),
    outbound_phase BIGINT NOT NULL CHECK (outbound_phase >= 0),
    return_phase BIGINT NOT NULL CHECK (return_phase >= 0),
    origin_kind VARCHAR(32) NOT NULL,
    opened_tick BIGINT NOT NULL CHECK (opened_tick >= 0),
    opened_fact_id BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_commute_assignment_epoch_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_open_world_commute_assignment_epoch_binding_unique UNIQUE (world_id, binding_code, epoch_number),
    CONSTRAINT city_open_world_commute_assignment_epoch_binding_fk
        FOREIGN KEY (world_id, binding_code)
        REFERENCES city_open_world_commute_bindings(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_assignment_epoch_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_assignment_epoch_home_facility_fk
        FOREIGN KEY (world_id, home_facility_code)
        REFERENCES city_open_world_facilities(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_assignment_epoch_work_facility_fk
        FOREIGN KEY (world_id, work_facility_code)
        REFERENCES city_open_world_facilities(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_assignment_epoch_home_hub_fk
        FOREIGN KEY (world_id, home_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_assignment_epoch_work_hub_fk
        FOREIGN KEY (world_id, work_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_assignment_epoch_fact_fk
        FOREIGN KEY (opened_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_commute_assignment_epoch_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND binding_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND employment_role_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND home_facility_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND home_hub_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND work_facility_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND work_hub_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND assignment_kind = 'npc.residence_employment'
        AND origin_kind IN ('v13_baseline', 'admin_rebind')
        AND home_facility_code <> work_facility_code
        AND period_ticks = 24
        AND outbound_phase < period_ticks
        AND return_phase = (outbound_phase + period_ticks / 2) % period_ticks
    ),
    CONSTRAINT city_open_world_commute_assignment_epoch_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_commute_assignment_epochs_actor
    ON city_open_world_commute_assignment_epochs (world_id, actor_id, epoch_number DESC);

CREATE TABLE IF NOT EXISTS city_open_world_commute_assignment_transitions (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_commute_lifecycle_profiles(world_id) ON DELETE RESTRICT,
    assignment_code VARCHAR(160) NOT NULL,
    transition_tick BIGINT NOT NULL CHECK (transition_tick >= 0),
    transition_sequence BIGINT NOT NULL CHECK (transition_sequence >= 0),
    state VARCHAR(16) NOT NULL,
    reason_code VARCHAR(96) NOT NULL,
    source_fact_id BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, assignment_code, transition_tick, transition_sequence),
    CONSTRAINT city_open_world_commute_assignment_transition_assignment_fk
        FOREIGN KEY (world_id, assignment_code)
        REFERENCES city_open_world_commute_assignment_epochs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_assignment_transition_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_commute_assignment_transition_identity_check CHECK (
        state IN ('active', 'suspended', 'superseded', 'terminated')
        AND reason_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND ((source_fact_id IS NULL AND state = 'active' AND reason_code = 'baseline_initialized'
              AND transition_sequence = 0) OR source_fact_id IS NOT NULL)
    ),
    CONSTRAINT city_open_world_commute_assignment_transition_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_commute_assignment_transitions_current
    ON city_open_world_commute_assignment_transitions (world_id, assignment_code, transition_tick DESC, transition_sequence DESC);

CREATE TABLE IF NOT EXISTS city_open_world_commute_lifecycle_sources (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_commute_lifecycle_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    assignment_code VARCHAR(160) NOT NULL,
    binding_code VARCHAR(160) NOT NULL,
    actor_id BIGINT NOT NULL,
    source_kind VARCHAR(64) NOT NULL,
    direction VARCHAR(16) NOT NULL,
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
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_commute_lifecycle_source_assignment_fk
        FOREIGN KEY (world_id, assignment_code)
        REFERENCES city_open_world_commute_assignment_epochs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_lifecycle_source_binding_fk
        FOREIGN KEY (world_id, binding_code)
        REFERENCES city_open_world_commute_bindings(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_lifecycle_source_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_lifecycle_source_origin_facility_fk
        FOREIGN KEY (world_id, origin_facility_code)
        REFERENCES city_open_world_facilities(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_lifecycle_source_destination_facility_fk
        FOREIGN KEY (world_id, destination_facility_code)
        REFERENCES city_open_world_facilities(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_lifecycle_source_origin_hub_fk
        FOREIGN KEY (world_id, origin_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_lifecycle_source_destination_hub_fk
        FOREIGN KEY (world_id, destination_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_lifecycle_source_mode_fk
        FOREIGN KEY (world_id, mode_code)
        REFERENCES city_open_world_mobility_modes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_commute_lifecycle_source_last_fact_fk
        FOREIGN KEY (last_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_commute_lifecycle_source_pair_unique UNIQUE (world_id, assignment_code, direction),
    CONSTRAINT city_open_world_commute_lifecycle_source_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND assignment_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
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
    CONSTRAINT city_open_world_commute_lifecycle_source_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_commute_lifecycle_sources_due
    ON city_open_world_commute_lifecycle_sources (world_id, status, next_due_tick, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_commute_lifecycle_sources_actor
    ON city_open_world_commute_lifecycle_sources (world_id, actor_id, status);

CREATE TABLE IF NOT EXISTS city_open_world_commute_lifecycle_cycle_metrics (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_commute_lifecycle_profiles(world_id) ON DELETE RESTRICT,
    cycle_start_tick BIGINT NOT NULL CHECK (cycle_start_tick > 0),
    cycle_end_tick BIGINT NOT NULL,
    closed_tick BIGINT NOT NULL,
    source_fact_id BIGINT NOT NULL,
    transition_count BIGINT NOT NULL CHECK (transition_count >= 0),
    rebind_count BIGINT NOT NULL CHECK (rebind_count >= 0),
    generated_count BIGINT NOT NULL CHECK (generated_count >= 0),
    suppressed_count BIGINT NOT NULL CHECK (suppressed_count >= 0),
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
    CONSTRAINT city_open_world_commute_lifecycle_metric_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_commute_lifecycle_metric_window_check CHECK (
        cycle_end_tick >= cycle_start_tick
        AND closed_tick = cycle_end_tick + 1
        AND cycle_end_tick - cycle_start_tick + 1 = 24
    ),
    CONSTRAINT city_open_world_commute_lifecycle_metric_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_commute_lifecycle_metrics_closed
    ON city_open_world_commute_lifecycle_cycle_metrics (world_id, closed_tick);

CREATE OR REPLACE FUNCTION city_open_world_commute_lifecycle_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_lifecycle_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v14'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v14'
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_lifecycle_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_lifecycle_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-v14'
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_commute_lifecycle_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_commute_lifecycle_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_commute_lifecycle_write_enabled(target_world_id)
       AND NEW.revision = OLD.revision + 1
       AND NEW.assignment_count >= OLD.assignment_count
       AND NEW.source_count >= OLD.source_count
       AND NEW.generated_count >= OLD.generated_count
       AND NEW.suppressed_count >= OLD.suppressed_count
       AND NEW.transition_count >= OLD.transition_count
       AND NEW.metric_count >= OLD.metric_count
       AND (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.assignment_contract, OLD.source_contract,
            OLD.period_ticks, OLD.maximum_assignments, OLD.maximum_transitions_tick,
            OLD.maximum_generations_tick, OLD.metadata, OLD.created_at)
           IS NOT DISTINCT FROM
           (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.assignment_contract, NEW.source_contract,
            NEW.period_ticks, NEW.maximum_assignments, NEW.maximum_transitions_tick,
            NEW.maximum_generations_tick, NEW.metadata, NEW.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V14 commute lifecycle profile may only advance audited runtime counters'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_commute_assignment_epoch()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_commute_lifecycle_bootstrap_write_enabled(target_world_id)
       AND NEW.origin_kind = 'v13_baseline' AND NEW.epoch_number = 1 AND NEW.opened_fact_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_commute_lifecycle_write_enabled(target_world_id)
       AND NEW.origin_kind = 'admin_rebind' AND NEW.opened_fact_id IS NOT NULL
       AND EXISTS (
            SELECT 1 FROM city_open_world_runtime_facts fact
            WHERE fact.id = NEW.opened_fact_id AND fact.world_id = NEW.world_id
              AND fact.tick = NEW.opened_tick
              AND fact.fact_type = 'system.commute.lifecycle.assignment.rebound'
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V14 commute assignment epochs are immutable fact-backed evidence'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_commute_assignment_transition()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_commute_lifecycle_bootstrap_write_enabled(target_world_id)
       AND NEW.state = 'active' AND NEW.reason_code = 'baseline_initialized'
       AND NEW.source_fact_id IS NULL AND NEW.transition_sequence = 0 THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_commute_lifecycle_write_enabled(target_world_id)
       AND NEW.source_fact_id IS NOT NULL
       AND EXISTS (
            SELECT 1 FROM city_open_world_runtime_facts fact
            WHERE fact.id = NEW.source_fact_id AND fact.world_id = NEW.world_id
              AND fact.tick = NEW.transition_tick
              AND fact.fact_type IN (
                  'system.commute.lifecycle.assignment.rebound',
                  'system.commute.lifecycle.assignment.state.changed',
                  'system.commute.lifecycle.assignment.auto.suspended',
                  'system.commute.lifecycle.assignment.auto.resumed',
                  'system.commute.lifecycle.assignment.terminated'
              )
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V14 commute lifecycle transitions require a matching runtime fact'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_commute_lifecycle_source()
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
        FROM city_open_world_commute_lifecycle_profiles WHERE world_id = target_world_id;
        IF city_open_world_commute_lifecycle_bootstrap_write_enabled(target_world_id)
           AND NEW.last_fact_id IS NULL AND NEW.generated_count = 0 AND NEW.suppressed_count = 0
           AND NEW.version = 1 AND NEW.last_transition_tick = baseline_tick_value
           AND NEW.next_due_tick = baseline_tick_value + 1 + NEW.phase_offset THEN
            RETURN NEW;
        END IF;
        IF city_open_world_commute_lifecycle_write_enabled(target_world_id)
           AND NEW.last_fact_id IS NOT NULL AND NEW.generated_count = 0 AND NEW.suppressed_count = 0
           AND NEW.version = 1
           AND EXISTS (
               SELECT 1 FROM city_open_world_runtime_facts fact
               WHERE fact.id = NEW.last_fact_id AND fact.world_id = NEW.world_id
                 AND fact.tick = NEW.last_transition_tick
                 AND fact.fact_type = 'system.commute.lifecycle.assignment.rebound'
           ) THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'open-world V14 commute lifecycle source must be initialized from a sealed epoch'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'DELETE' OR NOT city_open_world_commute_lifecycle_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V14 commute lifecycle sources require a runtime fact or recovery context'
            USING ERRCODE = '55000';
    END IF;
    IF (OLD.world_id, OLD.code, OLD.assignment_code, OLD.binding_code, OLD.actor_id,
        OLD.source_kind, OLD.direction, OLD.employment_role_code, OLD.origin_facility_code,
        OLD.origin_hub_code, OLD.destination_facility_code, OLD.destination_hub_code,
        OLD.mode_code, OLD.purpose_code, OLD.requested_units, OLD.status, OLD.period_ticks,
        OLD.phase_offset, OLD.metadata, OLD.created_at)
       IS DISTINCT FROM
       (NEW.world_id, NEW.code, NEW.assignment_code, NEW.binding_code, NEW.actor_id,
        NEW.source_kind, NEW.direction, NEW.employment_role_code, NEW.origin_facility_code,
        NEW.origin_hub_code, NEW.destination_facility_code, NEW.destination_hub_code,
        NEW.mode_code, NEW.purpose_code, NEW.requested_units, NEW.status, NEW.period_ticks,
        NEW.phase_offset, NEW.metadata, NEW.created_at)
       OR NEW.version <> OLD.version + 1
       OR NEW.last_transition_tick < OLD.last_transition_tick
       OR NEW.next_due_tick <> NEW.last_transition_tick + OLD.period_ticks THEN
        RAISE EXCEPTION 'open-world V14 commute lifecycle source identity or cadence is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.generated_count = OLD.generated_count + 1 AND NEW.suppressed_count = OLD.suppressed_count
       AND EXISTS (SELECT 1 FROM city_open_world_runtime_facts fact
                   WHERE fact.id = NEW.last_fact_id AND fact.world_id = NEW.world_id
                     AND fact.actor_id = NEW.actor_id AND fact.tick = NEW.last_transition_tick
                     AND fact.fact_type = 'system.commute.lifecycle.source.generated') THEN
        RETURN NEW;
    END IF;
    IF NEW.generated_count = OLD.generated_count AND NEW.suppressed_count = OLD.suppressed_count + 1
       AND EXISTS (SELECT 1 FROM city_open_world_runtime_facts fact
                   WHERE fact.id = NEW.last_fact_id AND fact.world_id = NEW.world_id
                     AND fact.actor_id = NEW.actor_id AND fact.tick = NEW.last_transition_tick
                     AND fact.fact_type = 'system.commute.lifecycle.source.suppressed') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V14 commute lifecycle source transition is invalid'
        USING ERRCODE = '23514';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_commute_lifecycle_metric()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_commute_lifecycle_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_open_world_runtime_facts fact
                   WHERE fact.id = NEW.source_fact_id AND fact.world_id = NEW.world_id
                     AND fact.tick = NEW.closed_tick
                     AND fact.fact_type = 'system.commute.lifecycle.cycle.closed') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V14 commute lifecycle metrics are immutable audited evidence'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_commute_lifecycle_profile_guard ON city_open_world_commute_lifecycle_profiles;
CREATE TRIGGER city_open_world_commute_lifecycle_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_commute_lifecycle_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_commute_lifecycle_profile();

DROP TRIGGER IF EXISTS city_open_world_commute_assignment_epoch_guard ON city_open_world_commute_assignment_epochs;
CREATE TRIGGER city_open_world_commute_assignment_epoch_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_commute_assignment_epochs
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_commute_assignment_epoch();

DROP TRIGGER IF EXISTS city_open_world_commute_assignment_transition_guard ON city_open_world_commute_assignment_transitions;
CREATE TRIGGER city_open_world_commute_assignment_transition_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_commute_assignment_transitions
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_commute_assignment_transition();

DROP TRIGGER IF EXISTS city_open_world_commute_lifecycle_source_guard ON city_open_world_commute_lifecycle_sources;
CREATE TRIGGER city_open_world_commute_lifecycle_source_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_commute_lifecycle_sources
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_commute_lifecycle_source();

DROP TRIGGER IF EXISTS city_open_world_commute_lifecycle_metric_guard ON city_open_world_commute_lifecycle_cycle_metrics;
CREATE TRIGGER city_open_world_commute_lifecycle_metric_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_commute_lifecycle_cycle_metrics
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_commute_lifecycle_metric();

CREATE OR REPLACE FUNCTION assert_city_open_world_commute_lifecycle_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version VARCHAR(32); world_tick BIGINT; profile_tick BIGINT;
    assignment_total BIGINT; active_total BIGINT; suspended_total BIGINT;
    superseded_total BIGINT; terminated_total BIGINT; source_total BIGINT;
    generated_total BIGINT; suppressed_total BIGINT; transition_total BIGINT; metric_total BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v14' THEN RETURN; END IF;
    SELECT baseline_tick, assignment_count, active_assignment_count, suspended_assignment_count,
           superseded_assignment_count, terminated_assignment_count, source_count,
           generated_count, suppressed_count, transition_count, metric_count
    INTO profile_tick, assignment_total, active_total, suspended_total,
         superseded_total, terminated_total, source_total,
         generated_total, suppressed_total, transition_total, metric_total
    FROM city_open_world_commute_lifecycle_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick OR assignment_total IS NULL
       OR source_total <> assignment_total * 2 THEN
        RAISE EXCEPTION 'city open-world V14 commute lifecycle profile is missing or invalid' USING ERRCODE = '23514';
    END IF;
    IF assignment_total <> (SELECT COUNT(*) FROM city_open_world_commute_assignment_epochs WHERE world_id = target_world_id)
       OR source_total <> (SELECT COUNT(*) FROM city_open_world_commute_lifecycle_sources WHERE world_id = target_world_id)
       OR transition_total <> (SELECT COUNT(*) FROM city_open_world_commute_assignment_transitions WHERE world_id = target_world_id)
       OR generated_total <> COALESCE((SELECT SUM(generated_count) FROM city_open_world_commute_lifecycle_sources WHERE world_id = target_world_id), 0)
       OR suppressed_total <> COALESCE((SELECT SUM(suppressed_count) FROM city_open_world_commute_lifecycle_sources WHERE world_id = target_world_id), 0)
       OR metric_total <> (SELECT COUNT(*) FROM city_open_world_commute_lifecycle_cycle_metrics WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V14 commute lifecycle counters are inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_open_world_commute_bindings binding
        WHERE binding.world_id = target_world_id
          AND NOT EXISTS (SELECT 1 FROM city_open_world_commute_assignment_epochs epoch
                          WHERE epoch.world_id = binding.world_id AND epoch.binding_code = binding.code)
    ) THEN
        RAISE EXCEPTION 'city open-world V14 binding lacks a successor assignment epoch' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_open_world_commute_assignment_epochs epoch
        WHERE epoch.world_id = target_world_id
          AND NOT EXISTS (SELECT 1 FROM city_open_world_commute_assignment_transitions transition
                          WHERE transition.world_id = epoch.world_id AND transition.assignment_code = epoch.code)
    ) THEN
        RAISE EXCEPTION 'city open-world V14 assignment epoch lacks a lifecycle transition' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_open_world_commute_assignment_epochs epoch
        WHERE epoch.world_id = target_world_id
          AND (NOT EXISTS (SELECT 1 FROM city_open_world_commute_lifecycle_sources source
                           WHERE source.world_id = epoch.world_id AND source.assignment_code = epoch.code
                             AND source.direction = 'outbound')
               OR NOT EXISTS (SELECT 1 FROM city_open_world_commute_lifecycle_sources source
                              WHERE source.world_id = epoch.world_id AND source.assignment_code = epoch.code
                                AND source.direction = 'return'))
    ) THEN
        RAISE EXCEPTION 'city open-world V14 assignment epoch lacks a directional source pair' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        WITH latest AS (
            SELECT DISTINCT ON (assignment_code) assignment_code, state
            FROM city_open_world_commute_assignment_transitions
            WHERE world_id = target_world_id
            ORDER BY assignment_code, transition_tick DESC, transition_sequence DESC
        )
        SELECT 1
        FROM city_open_world_commute_assignment_epochs epoch
        JOIN latest ON latest.assignment_code = epoch.code
        JOIN city_open_world_facilities home
          ON home.world_id = epoch.world_id AND home.code = epoch.home_facility_code
        WHERE epoch.world_id = target_world_id AND latest.state IN ('active', 'suspended')
        GROUP BY epoch.home_facility_code, home.capacity_units
        HAVING COUNT(*) > MAX(home.capacity_units)
    ) OR EXISTS (
        WITH latest AS (
            SELECT DISTINCT ON (assignment_code) assignment_code, state
            FROM city_open_world_commute_assignment_transitions
            WHERE world_id = target_world_id
            ORDER BY assignment_code, transition_tick DESC, transition_sequence DESC
        )
        SELECT 1
        FROM city_open_world_commute_assignment_epochs epoch
        JOIN latest ON latest.assignment_code = epoch.code
        JOIN city_open_world_facilities work
          ON work.world_id = epoch.world_id AND work.code = epoch.work_facility_code
        WHERE epoch.world_id = target_world_id AND latest.state IN ('active', 'suspended')
        GROUP BY epoch.work_facility_code, work.capacity_units
        HAVING COUNT(*) > MAX(work.capacity_units)
    ) THEN
        RAISE EXCEPTION 'city open-world V14 effective assignment capacity is exceeded' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_commute_lifecycle_sources source
        JOIN city_open_world_commute_assignment_epochs epoch
          ON epoch.world_id = source.world_id AND epoch.code = source.assignment_code
        WHERE source.world_id = target_world_id
          AND (source.binding_code <> epoch.binding_code
               OR source.actor_id <> epoch.actor_id
               OR source.employment_role_code <> epoch.employment_role_code
               OR (source.direction = 'outbound' AND (
                    source.origin_facility_code <> epoch.home_facility_code
                    OR source.origin_hub_code <> epoch.home_hub_code
                    OR source.destination_facility_code <> epoch.work_facility_code
                    OR source.destination_hub_code <> epoch.work_hub_code
                    OR source.phase_offset <> epoch.outbound_phase))
               OR (source.direction = 'return' AND (
                    source.origin_facility_code <> epoch.work_facility_code
                    OR source.origin_hub_code <> epoch.work_hub_code
                    OR source.destination_facility_code <> epoch.home_facility_code
                    OR source.destination_hub_code <> epoch.home_hub_code
                    OR source.phase_offset <> epoch.return_phase)))
    ) THEN
        RAISE EXCEPTION 'city open-world V14 commute lifecycle source identity is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_open_world_commute_lifecycle_cycle_metrics metric
        JOIN city_open_world_runtime_facts fact
          ON fact.id = metric.source_fact_id AND fact.world_id = metric.world_id
        WHERE metric.world_id = target_world_id
          AND (fact.fact_type <> 'system.commute.lifecycle.cycle.closed' OR fact.tick <> metric.closed_tick)
    ) THEN
        RAISE EXCEPTION 'city open-world V14 commute lifecycle metric evidence is invalid' USING ERRCODE = '23514';
    END IF;
    IF (SELECT COUNT(*) FROM city_open_world_commute_assignment_epochs epoch
        JOIN LATERAL (
            SELECT state FROM city_open_world_commute_assignment_transitions transition
            WHERE transition.world_id = epoch.world_id AND transition.assignment_code = epoch.code
            ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC LIMIT 1
        ) latest ON TRUE
        WHERE epoch.world_id = target_world_id AND latest.state = 'active') <> active_total
       OR (SELECT COUNT(*) FROM city_open_world_commute_assignment_epochs epoch
        JOIN LATERAL (
            SELECT state FROM city_open_world_commute_assignment_transitions transition
            WHERE transition.world_id = epoch.world_id AND transition.assignment_code = epoch.code
            ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC LIMIT 1
        ) latest ON TRUE
        WHERE epoch.world_id = target_world_id AND latest.state = 'suspended') <> suspended_total
       OR (SELECT COUNT(*) FROM city_open_world_commute_assignment_epochs epoch
        JOIN LATERAL (
            SELECT state FROM city_open_world_commute_assignment_transitions transition
            WHERE transition.world_id = epoch.world_id AND transition.assignment_code = epoch.code
            ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC LIMIT 1
        ) latest ON TRUE
        WHERE epoch.world_id = target_world_id AND latest.state = 'superseded') <> superseded_total
       OR (SELECT COUNT(*) FROM city_open_world_commute_assignment_epochs epoch
        JOIN LATERAL (
            SELECT state FROM city_open_world_commute_assignment_transitions transition
            WHERE transition.world_id = epoch.world_id AND transition.assignment_code = epoch.code
            ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC LIMIT 1
        ) latest ON TRUE
        WHERE epoch.world_id = target_world_id AND latest.state = 'terminated') <> terminated_total THEN
        RAISE EXCEPTION 'city open-world V14 assignment-state counters are inconsistent' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_open_world_commute_lifecycle_foundation_commit()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    PERFORM assert_city_open_world_commute_lifecycle_foundation(target_world_id);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_commute_lifecycle_profile_commit_check ON city_open_world_commute_lifecycle_profiles;
CREATE CONSTRAINT TRIGGER city_open_world_commute_lifecycle_profile_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_commute_lifecycle_profiles
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_open_world_commute_lifecycle_foundation_commit();

DROP TRIGGER IF EXISTS city_open_world_commute_assignment_epoch_commit_check ON city_open_world_commute_assignment_epochs;
CREATE CONSTRAINT TRIGGER city_open_world_commute_assignment_epoch_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_commute_assignment_epochs
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_open_world_commute_lifecycle_foundation_commit();

DROP TRIGGER IF EXISTS city_open_world_commute_assignment_transition_commit_check ON city_open_world_commute_assignment_transitions;
CREATE CONSTRAINT TRIGGER city_open_world_commute_assignment_transition_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_commute_assignment_transitions
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_open_world_commute_lifecycle_foundation_commit();

DROP TRIGGER IF EXISTS city_open_world_commute_lifecycle_source_commit_check ON city_open_world_commute_lifecycle_sources;
CREATE CONSTRAINT TRIGGER city_open_world_commute_lifecycle_source_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_commute_lifecycle_sources
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_open_world_commute_lifecycle_foundation_commit();

DROP TRIGGER IF EXISTS city_open_world_commute_lifecycle_metric_commit_check ON city_open_world_commute_lifecycle_cycle_metrics;
CREATE CONSTRAINT TRIGGER city_open_world_commute_lifecycle_metric_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_commute_lifecycle_cycle_metrics
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_open_world_commute_lifecycle_foundation_commit();

-- V14 is a strict successor. The following gates widen only the engine
-- version list so V14 genesis/upgrade can instantiate the frozen lower-level
-- V5–V13 projections; none of the predecessor write contracts become
-- generally mutable.
CREATE OR REPLACE FUNCTION city_open_world_commute_source_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_source_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v13', 'city-openworld-v14')
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

CREATE OR REPLACE FUNCTION city_open_world_commute_source_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_source_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v13', 'city-openworld-v14'))
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_commute_source_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT; world_tick BIGINT;
DECLARE source_total BIGINT; generated_total BIGINT; suppressed_total BIGINT; metric_total BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v13', 'city-openworld-v14') THEN RETURN; END IF;
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
        SELECT 1 FROM city_open_world_commute_bindings binding
        WHERE binding.world_id = target_world_id
          AND (NOT EXISTS (SELECT 1 FROM city_open_world_commute_sources source
                           WHERE source.world_id = binding.world_id AND source.binding_code = binding.code
                             AND source.direction = 'outbound')
               OR NOT EXISTS (SELECT 1 FROM city_open_world_commute_sources source
                              WHERE source.world_id = binding.world_id AND source.binding_code = binding.code
                                AND source.direction = 'return'))
    ) THEN
        RAISE EXCEPTION 'city open-world V13 commute binding lacks a directional source pair' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v12','city-openworld-v13','city-openworld-v14')
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
DECLARE world_version VARCHAR(32); world_tick BIGINT; profile_tick BIGINT;
DECLARE candidate_total BIGINT; binding_total BIGINT; unbound_total BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v12','city-openworld-v13','city-openworld-v14') THEN RETURN; END IF;
    SELECT baseline_tick, candidate_count, binding_count, unbound_candidate_count
    INTO profile_tick, candidate_total, binding_total, unbound_total
    FROM city_open_world_commute_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick OR candidate_total <> binding_total + unbound_total
       OR binding_total <> (SELECT COUNT(*) FROM city_open_world_commute_bindings WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V12 commute foundation is missing or inconsistent' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_arrival_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_arrival_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_arrival_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_od_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_od_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_od_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_service_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_impact_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v1','city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_materialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_materialization_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_fact_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_fact_insert()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE world_tick BIGINT; world_version VARCHAR(32); command_status_value VARCHAR(16);
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN RETURN NEW; END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version FROM city_worlds WHERE id = NEW.world_id;
    IF world_version NOT IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14')
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
       AND NOT (world_version IN ('city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14') AND NEW.fact_type LIKE 'system.%') THEN
        RAISE EXCEPTION 'derived open-world runtime fact requires a parent fact' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
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
    IF world_version = 'city-openworld-v14' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding
        WHERE binding.world_id = target_world_id AND binding.generation = vector_generation
          AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-commute-lifecycle-catalog'
          AND binding.bundle_version = '1.0.0'
    ) THEN
        RAISE EXCEPTION 'city open-world V14 commute lifecycle version vector is incomplete' USING ERRCODE = '23514';
    END IF;
END;
$$;
