-- city-openworld-v9 introduces the first sealed aggregate mobility layer.
-- It deliberately models facility/zone/interchange movement and capacity at
-- a tick boundary.  It does not claim lane-level traffic or move V5 actor
-- coordinates; a later local-navigation bridge can consume completed routes
-- without rewriting this fact-backed transport history.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v9', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","version_vector","facilities","npc_lod","social_service_catalog","social_service_access","social_service_queue","social_service_response","cross_domain_impact_bridge","aggregate_mobility_graph","mobility_capacity_allocation","mobility_route_lifecycle","delayed_effects","domain_metrics","activities","rules","case_lifecycle","rewards","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v8', 'city-openworld-v9', 'openworld_v8_to_v9')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_mobility_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_impact_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    topology_contract_version VARCHAR(48) NOT NULL,
    scheduling_contract VARCHAR(48) NOT NULL,
    maximum_schedules_per_tick INTEGER NOT NULL CHECK (maximum_schedules_per_tick BETWEEN 1 AND 100000),
    maximum_wait_ticks BIGINT NOT NULL CHECK (maximum_wait_ticks BETWEEN 1 AND 1000000),
    mode_count BIGINT NOT NULL CHECK (mode_count >= 0),
    hub_count BIGINT NOT NULL CHECK (hub_count >= 0),
    edge_count BIGINT NOT NULL CHECK (edge_count >= 0),
    demand_count BIGINT NOT NULL DEFAULT 0 CHECK (demand_count >= 0),
    route_count BIGINT NOT NULL DEFAULT 0 CHECK (route_count >= 0),
    allocation_count BIGINT NOT NULL DEFAULT 0 CHECK (allocation_count >= 0),
    completed_count BIGINT NOT NULL DEFAULT 0 CHECK (completed_count >= 0),
    expired_count BIGINT NOT NULL DEFAULT 0 CHECK (expired_count >= 0),
    actor_metric_count BIGINT NOT NULL DEFAULT 0 CHECK (actor_metric_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_mobility_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-mobility'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND topology_contract_version = 'facility-hub-zone-graph-v1'
        AND scheduling_contract = 'next_tick_capacity_v1'
        AND mode_count = 3
        AND completed_count + expired_count <= demand_count
        AND route_count <= demand_count
        AND actor_metric_count <= demand_count
    ),
    CONSTRAINT city_open_world_mobility_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_mobility_modes (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_mobility_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(64) NOT NULL,
    unit_kind VARCHAR(16) NOT NULL,
    speed_units_per_tick BIGINT NOT NULL CHECK (speed_units_per_tick > 0),
    capacity_units_per_tick BIGINT NOT NULL CHECK (capacity_units_per_tick > 0),
    congestion_threshold_milli INTEGER NOT NULL CHECK (congestion_threshold_milli BETWEEN 0 AND 999),
    maximum_delay_ticks BIGINT NOT NULL CHECK (maximum_delay_ticks >= 0),
    definition_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_mobility_mode_identity_check CHECK (
        code IN ('walk', 'transit', 'freight')
        AND unit_kind IN ('person', 'cargo')
        AND definition_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_open_world_mobility_mode_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_mobility_hubs (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_mobility_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    hub_kind VARCHAR(24) NOT NULL,
    facility_id BIGINT,
    facility_code VARCHAR(160),
    zone_x BIGINT NOT NULL,
    zone_y BIGINT NOT NULL,
    anchor_x BIGINT NOT NULL,
    anchor_y BIGINT NOT NULL,
    anchor_z INTEGER NOT NULL CHECK (anchor_z >= 0),
    definition_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_mobility_hub_facility_fk
        FOREIGN KEY (facility_id, world_id)
        REFERENCES city_open_world_facilities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_hub_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND hub_kind IN ('interchange', 'zone', 'facility')
        AND definition_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND ((hub_kind = 'facility' AND facility_id IS NOT NULL AND facility_code ~ '^[a-z][a-z0-9_.-]{1,159}$')
             OR (hub_kind <> 'facility' AND facility_id IS NULL AND facility_code IS NULL))
    ),
    CONSTRAINT city_open_world_mobility_hub_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_mobility_edges (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_mobility_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    mode_code VARCHAR(64) NOT NULL,
    from_hub_code VARCHAR(160) NOT NULL,
    to_hub_code VARCHAR(160) NOT NULL,
    tier VARCHAR(16) NOT NULL,
    distance_units BIGINT NOT NULL CHECK (distance_units >= 0),
    base_travel_ticks BIGINT NOT NULL CHECK (base_travel_ticks > 0),
    capacity_units_per_tick BIGINT NOT NULL CHECK (capacity_units_per_tick > 0),
    definition_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_mobility_edge_mode_fk
        FOREIGN KEY (world_id, mode_code)
        REFERENCES city_open_world_mobility_modes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_edge_from_hub_fk
        FOREIGN KEY (world_id, from_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_edge_to_hub_fk
        FOREIGN KEY (world_id, to_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_edge_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND tier IN ('local', 'trunk')
        AND from_hub_code <> to_hub_code
        AND definition_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_open_world_mobility_edge_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_mobility_demands (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_mobility_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    actor_id BIGINT NOT NULL,
    source_hub_code VARCHAR(160) NOT NULL,
    destination_hub_code VARCHAR(160) NOT NULL,
    mode_code VARCHAR(64) NOT NULL,
    purpose_code VARCHAR(96) NOT NULL,
    requested_units BIGINT NOT NULL CHECK (requested_units BETWEEN 1 AND 1000),
    requested_tick BIGINT NOT NULL CHECK (requested_tick > 0),
    earliest_departure_tick BIGINT NOT NULL,
    deadline_tick BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    source_fact_id BIGINT NOT NULL,
    last_fact_id BIGINT NOT NULL,
    route_id BIGINT,
    scheduled_tick BIGINT,
    completed_tick BIGINT,
    expired_tick BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_mobility_demand_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_demand_source_hub_fk
        FOREIGN KEY (world_id, source_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_demand_destination_hub_fk
        FOREIGN KEY (world_id, destination_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_demand_mode_fk
        FOREIGN KEY (world_id, mode_code)
        REFERENCES city_open_world_mobility_modes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_demand_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_mobility_demand_last_fact_fk
        FOREIGN KEY (last_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_mobility_demand_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND purpose_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND source_hub_code <> destination_hub_code
        AND earliest_departure_tick = requested_tick + 1
        AND deadline_tick >= earliest_departure_tick
        AND status IN ('pending', 'scheduled', 'completed', 'expired')
    ),
    CONSTRAINT city_open_world_mobility_demand_lifecycle_check CHECK (
        (status = 'pending' AND route_id IS NULL AND scheduled_tick IS NULL AND completed_tick IS NULL AND expired_tick IS NULL)
        OR (status = 'scheduled' AND route_id IS NOT NULL AND scheduled_tick IS NOT NULL AND completed_tick IS NULL AND expired_tick IS NULL)
        OR (status = 'completed' AND route_id IS NOT NULL AND scheduled_tick IS NOT NULL AND completed_tick IS NOT NULL AND expired_tick IS NULL)
        OR (status = 'expired' AND route_id IS NULL AND scheduled_tick IS NULL AND completed_tick IS NULL AND expired_tick IS NOT NULL)
    ),
    CONSTRAINT city_open_world_mobility_demand_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_mobility_demands_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_mobility_demands_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_mobility_demands_dispatch
    ON city_open_world_mobility_demands (world_id, status, earliest_departure_tick, deadline_tick, requested_tick, code);

CREATE TABLE IF NOT EXISTS city_open_world_mobility_routes (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_mobility_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    demand_id BIGINT NOT NULL,
    actor_id BIGINT NOT NULL,
    mode_code VARCHAR(64) NOT NULL,
    source_hub_code VARCHAR(160) NOT NULL,
    destination_hub_code VARCHAR(160) NOT NULL,
    departure_tick BIGINT NOT NULL CHECK (departure_tick > 0),
    arrival_tick BIGINT NOT NULL,
    base_travel_ticks BIGINT NOT NULL CHECK (base_travel_ticks > 0),
    congestion_delay_ticks BIGINT NOT NULL CHECK (congestion_delay_ticks >= 0),
    status VARCHAR(16) NOT NULL DEFAULT 'scheduled',
    source_fact_id BIGINT NOT NULL,
    completion_fact_id BIGINT,
    completed_tick BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_mobility_route_demand_fk
        FOREIGN KEY (demand_id, world_id)
        REFERENCES city_open_world_mobility_demands(id, world_id) ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_mobility_route_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_route_mode_fk
        FOREIGN KEY (world_id, mode_code)
        REFERENCES city_open_world_mobility_modes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_route_source_hub_fk
        FOREIGN KEY (world_id, source_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_route_destination_hub_fk
        FOREIGN KEY (world_id, destination_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_route_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_mobility_route_completion_fact_fk
        FOREIGN KEY (completion_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_mobility_route_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND source_hub_code <> destination_hub_code
        AND arrival_tick = departure_tick + base_travel_ticks + congestion_delay_ticks
        AND status IN ('scheduled', 'completed')
    ),
    CONSTRAINT city_open_world_mobility_route_lifecycle_check CHECK (
        (status = 'scheduled' AND completion_fact_id IS NULL AND completed_tick IS NULL)
        OR (status = 'completed' AND completion_fact_id IS NOT NULL AND completed_tick IS NOT NULL AND completed_tick >= arrival_tick)
    ),
    CONSTRAINT city_open_world_mobility_route_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_mobility_routes_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_mobility_routes_demand_unique UNIQUE (world_id, demand_id),
    CONSTRAINT city_open_world_mobility_routes_id_world_unique UNIQUE (id, world_id)
);

ALTER TABLE city_open_world_mobility_demands
    DROP CONSTRAINT IF EXISTS city_open_world_mobility_demand_route_fk;
ALTER TABLE city_open_world_mobility_demands
    ADD CONSTRAINT city_open_world_mobility_demand_route_fk
        FOREIGN KEY (route_id, world_id)
        REFERENCES city_open_world_mobility_routes(id, world_id) ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX IF NOT EXISTS idx_city_open_world_mobility_routes_due
    ON city_open_world_mobility_routes (world_id, status, arrival_tick, code);

CREATE TABLE IF NOT EXISTS city_open_world_mobility_allocations (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_mobility_profiles(world_id) ON DELETE RESTRICT,
    route_id BIGINT NOT NULL,
    edge_code VARCHAR(160) NOT NULL,
    departure_tick BIGINT NOT NULL CHECK (departure_tick > 0),
    allocated_units BIGINT NOT NULL CHECK (allocated_units > 0),
    capacity_units_per_tick BIGINT NOT NULL CHECK (capacity_units_per_tick > 0),
    occupancy_milli INTEGER NOT NULL CHECK (occupancy_milli BETWEEN 1 AND 1000),
    delay_ticks BIGINT NOT NULL CHECK (delay_ticks >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, route_id, edge_code),
    CONSTRAINT city_open_world_mobility_allocation_route_fk
        FOREIGN KEY (route_id, world_id)
        REFERENCES city_open_world_mobility_routes(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_mobility_allocation_edge_fk
        FOREIGN KEY (world_id, edge_code)
        REFERENCES city_open_world_mobility_edges(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_allocation_capacity_check CHECK (allocated_units <= capacity_units_per_tick),
    CONSTRAINT city_open_world_mobility_allocation_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_mobility_allocations_capacity
    ON city_open_world_mobility_allocations (world_id, edge_code, departure_tick);

CREATE TABLE IF NOT EXISTS city_open_world_mobility_actor_metrics (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_mobility_profiles(world_id) ON DELETE RESTRICT,
    actor_id BIGINT NOT NULL,
    requested_count BIGINT NOT NULL DEFAULT 0 CHECK (requested_count >= 0),
    scheduled_count BIGINT NOT NULL DEFAULT 0 CHECK (scheduled_count >= 0),
    completed_count BIGINT NOT NULL DEFAULT 0 CHECK (completed_count >= 0),
    expired_count BIGINT NOT NULL DEFAULT 0 CHECK (expired_count >= 0),
    last_route_id BIGINT,
    last_event_tick BIGINT NOT NULL CHECK (last_event_tick > 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_id),
    CONSTRAINT city_open_world_mobility_metric_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_mobility_metric_last_route_fk
        FOREIGN KEY (last_route_id, world_id)
        REFERENCES city_open_world_mobility_routes(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_mobility_metric_lifecycle_check CHECK (
        completed_count <= scheduled_count AND scheduled_count <= requested_count
        AND expired_count <= requested_count AND completed_count + expired_count <= requested_count
    ),
    CONSTRAINT city_open_world_mobility_metric_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE OR REPLACE FUNCTION city_open_world_mobility_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1
            FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v9'
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
                        AND upgrade.to_version = 'city-openworld-v9'
                        AND upgrade.status = 'running'
                  )
              )
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v9'
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_mobility_baseline()
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
    IF TG_OP = 'INSERT' AND city_open_world_mobility_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world mobility topology is immutable outside genesis, audited upgrade, or recovery'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_mobility_profile()
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
    IF TG_OP = 'INSERT' AND city_open_world_mobility_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_open_world_mobility_write_enabled(target_world_id)
       AND NEW.revision = OLD.revision + 1
       AND NEW.demand_count >= OLD.demand_count
       AND NEW.route_count >= OLD.route_count
       AND NEW.allocation_count >= OLD.allocation_count
       AND NEW.completed_count >= OLD.completed_count
       AND NEW.expired_count >= OLD.expired_count
       AND NEW.actor_metric_count >= OLD.actor_metric_count
       AND (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.topology_contract_version, OLD.scheduling_contract,
            OLD.maximum_schedules_per_tick, OLD.maximum_wait_ticks, OLD.mode_count,
            OLD.hub_count, OLD.edge_count, OLD.metadata, OLD.created_at)
           IS NOT DISTINCT FROM
           (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.topology_contract_version, NEW.scheduling_contract,
            NEW.maximum_schedules_per_tick, NEW.maximum_wait_ticks, NEW.mode_count,
            NEW.hub_count, NEW.edge_count, NEW.metadata, NEW.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world mobility profile may only advance audited runtime counters'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_mobility_demand()
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
    IF TG_OP = 'DELETE' OR NOT city_open_world_mobility_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world mobility demands require a runtime fact or recovery context'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'pending' OR NEW.source_fact_id <> NEW.last_fact_id
           OR NOT EXISTS (
               SELECT 1 FROM city_open_world_runtime_facts fact
               WHERE fact.id = NEW.source_fact_id AND fact.world_id = NEW.world_id
                 AND fact.tick = NEW.requested_tick
                 AND fact.fact_type = 'mobility.requested'
                 AND fact.actor_id = NEW.actor_id
           ) THEN
            RAISE EXCEPTION 'open-world mobility demand must be rooted in a matching mobility request fact'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF (OLD.world_id, OLD.code, OLD.actor_id, OLD.source_hub_code, OLD.destination_hub_code,
        OLD.mode_code, OLD.purpose_code, OLD.requested_units, OLD.requested_tick,
        OLD.earliest_departure_tick, OLD.deadline_tick, OLD.source_fact_id,
        OLD.metadata, OLD.created_at) IS DISTINCT FROM
       (NEW.world_id, NEW.code, NEW.actor_id, NEW.source_hub_code, NEW.destination_hub_code,
        NEW.mode_code, NEW.purpose_code, NEW.requested_units, NEW.requested_tick,
        NEW.earliest_departure_tick, NEW.deadline_tick, NEW.source_fact_id,
        NEW.metadata, NEW.created_at)
       OR NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'open-world mobility demand identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'pending' AND NEW.status = 'scheduled'
       AND NEW.route_id IS NOT NULL AND NEW.scheduled_tick IS NOT NULL
       AND EXISTS (
           SELECT 1 FROM city_open_world_runtime_facts fact
           WHERE fact.id = NEW.last_fact_id AND fact.world_id = NEW.world_id
             AND fact.tick = NEW.scheduled_tick AND fact.fact_type = 'mobility.scheduled'
             AND fact.actor_id = NEW.actor_id
       ) THEN RETURN NEW; END IF;
    IF OLD.status = 'scheduled' AND NEW.status = 'completed'
       AND NEW.completed_tick IS NOT NULL
       AND EXISTS (
           SELECT 1 FROM city_open_world_runtime_facts fact
           WHERE fact.id = NEW.last_fact_id AND fact.world_id = NEW.world_id
             AND fact.tick = NEW.completed_tick AND fact.fact_type = 'mobility.completed'
             AND fact.actor_id = NEW.actor_id
       ) THEN RETURN NEW; END IF;
    IF OLD.status = 'pending' AND NEW.status = 'expired'
       AND NEW.expired_tick IS NOT NULL
       AND EXISTS (
           SELECT 1 FROM city_open_world_runtime_facts fact
           WHERE fact.id = NEW.last_fact_id AND fact.world_id = NEW.world_id
             AND fact.tick = NEW.expired_tick AND fact.fact_type = 'mobility.expired'
             AND fact.actor_id = NEW.actor_id
       ) THEN RETURN NEW; END IF;
    RAISE EXCEPTION 'open-world mobility demand transition is invalid'
        USING ERRCODE = '23514';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_mobility_route()
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
    IF TG_OP = 'DELETE' OR NOT city_open_world_mobility_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world mobility routes require a runtime fact or recovery context'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'scheduled'
           OR NOT EXISTS (
               SELECT 1 FROM city_open_world_runtime_facts fact
               WHERE fact.id = NEW.source_fact_id AND fact.world_id = NEW.world_id
                 AND fact.tick = NEW.departure_tick AND fact.fact_type = 'mobility.scheduled'
                 AND fact.actor_id = NEW.actor_id
           ) THEN
            RAISE EXCEPTION 'open-world mobility route must be rooted in a matching schedule fact'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF (OLD.world_id, OLD.code, OLD.demand_id, OLD.actor_id, OLD.mode_code,
        OLD.source_hub_code, OLD.destination_hub_code, OLD.departure_tick,
        OLD.arrival_tick, OLD.base_travel_ticks, OLD.congestion_delay_ticks,
        OLD.source_fact_id, OLD.metadata, OLD.created_at) IS DISTINCT FROM
       (NEW.world_id, NEW.code, NEW.demand_id, NEW.actor_id, NEW.mode_code,
        NEW.source_hub_code, NEW.destination_hub_code, NEW.departure_tick,
        NEW.arrival_tick, NEW.base_travel_ticks, NEW.congestion_delay_ticks,
        NEW.source_fact_id, NEW.metadata, NEW.created_at)
       OR NEW.version <> OLD.version + 1
       OR OLD.status <> 'scheduled' OR NEW.status <> 'completed'
       OR NOT EXISTS (
           SELECT 1 FROM city_open_world_runtime_facts fact
           WHERE fact.id = NEW.completion_fact_id AND fact.world_id = NEW.world_id
             AND fact.tick = NEW.completed_tick AND fact.fact_type = 'mobility.completed'
             AND fact.actor_id = NEW.actor_id
       ) THEN
        RAISE EXCEPTION 'open-world mobility route transition is invalid'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_mobility_allocation()
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
    IF TG_OP <> 'INSERT' OR NOT city_open_world_mobility_write_enabled(target_world_id)
       OR NOT EXISTS (
            SELECT 1 FROM city_open_world_mobility_routes route
            WHERE route.id = NEW.route_id AND route.world_id = NEW.world_id
              AND route.status = 'scheduled' AND route.departure_tick = NEW.departure_tick
       ) THEN
        RAISE EXCEPTION 'open-world mobility allocations require a scheduled route or recovery context'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_mobility_actor_metric()
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
    IF TG_OP = 'INSERT' AND city_open_world_mobility_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_mobility_write_enabled(target_world_id)
       AND NEW.version = OLD.version + 1
       AND NEW.requested_count >= OLD.requested_count
       AND NEW.scheduled_count >= OLD.scheduled_count
       AND NEW.completed_count >= OLD.completed_count
       AND NEW.expired_count >= OLD.expired_count
       AND (OLD.world_id, OLD.actor_id, OLD.metadata, OLD.created_at)
           IS NOT DISTINCT FROM (NEW.world_id, NEW.actor_id, NEW.metadata, NEW.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world mobility metrics require audited runtime facts or recovery'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_mobility_profile_guard ON city_open_world_mobility_profiles;
CREATE TRIGGER city_open_world_mobility_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_mobility_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_mobility_profile();

DROP TRIGGER IF EXISTS city_open_world_mobility_mode_guard ON city_open_world_mobility_modes;
CREATE TRIGGER city_open_world_mobility_mode_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_mobility_modes
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_mobility_baseline();

DROP TRIGGER IF EXISTS city_open_world_mobility_hub_guard ON city_open_world_mobility_hubs;
CREATE TRIGGER city_open_world_mobility_hub_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_mobility_hubs
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_mobility_baseline();

DROP TRIGGER IF EXISTS city_open_world_mobility_edge_guard ON city_open_world_mobility_edges;
CREATE TRIGGER city_open_world_mobility_edge_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_mobility_edges
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_mobility_baseline();

DROP TRIGGER IF EXISTS city_open_world_mobility_demand_guard ON city_open_world_mobility_demands;
CREATE TRIGGER city_open_world_mobility_demand_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_mobility_demands
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_mobility_demand();

DROP TRIGGER IF EXISTS city_open_world_mobility_route_guard ON city_open_world_mobility_routes;
CREATE TRIGGER city_open_world_mobility_route_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_mobility_routes
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_mobility_route();

DROP TRIGGER IF EXISTS city_open_world_mobility_allocation_guard ON city_open_world_mobility_allocations;
CREATE TRIGGER city_open_world_mobility_allocation_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_mobility_allocations
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_mobility_allocation();

DROP TRIGGER IF EXISTS city_open_world_mobility_actor_metric_guard ON city_open_world_mobility_actor_metrics;
CREATE TRIGGER city_open_world_mobility_actor_metric_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_mobility_actor_metrics
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_mobility_actor_metric();

-- V9 widens its predecessors' successor gates without mutating any existing
-- V6/V7/V8 data.  Those older worlds retain their original immutable input
-- contracts; V9 only initializes new data for a V9 genesis or upgrade run.
CREATE OR REPLACE FUNCTION city_open_world_service_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_service_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v7', 'city-openworld-v8', 'city-openworld-v9')
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
                   AND world.simulation_version IN ('city-openworld-v7', 'city-openworld-v8', 'city-openworld-v9'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_impact_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v8', 'city-openworld-v9')
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
                   AND world.simulation_version IN ('city-openworld-v8', 'city-openworld-v9'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v1', 'city-openworld-v2', 'city-openworld-v3',
                       'city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7',
                       'city-openworld-v8', 'city-openworld-v9')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_materialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_materialization_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v2', 'city-openworld-v3', 'city-openworld-v4',
                       'city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7', 'city-openworld-v8', 'city-openworld-v9')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6',
                       'city-openworld-v7', 'city-openworld-v8', 'city-openworld-v9')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_fact_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6',
                       'city-openworld-v7', 'city-openworld-v8', 'city-openworld-v9')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
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
    IF city_recovery_write_enabled(NEW.world_id) THEN RETURN NEW; END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version FROM city_worlds WHERE id = NEW.world_id;
    IF world_version NOT IN ('city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7', 'city-openworld-v8', 'city-openworld-v9')
       OR NEW.tick IS DISTINCT FROM world_tick + 1 OR NEW.posted_at IS NOT NULL THEN
        RAISE EXCEPTION 'open-world runtime fact must be a draft for the next V4/V5/V6/V7/V8/V9 tick' USING ERRCODE = '23514';
    END IF;
    IF NEW.source_command_id IS NOT NULL THEN
        SELECT status INTO command_status_value FROM city_commands WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
        IF command_status_value IS DISTINCT FROM 'pending' THEN
            RAISE EXCEPTION 'open-world runtime fact requires a pending source command' USING ERRCODE = '23514';
        END IF;
    END IF;
    IF NEW.source_command_id IS NULL AND NEW.parent_fact_id IS NULL
       AND NOT (world_version IN ('city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7', 'city-openworld-v8', 'city-openworld-v9') AND NEW.fact_type LIKE 'system.%') THEN
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
    IF world_version NOT IN ('city-openworld-v7', 'city-openworld-v8', 'city-openworld-v9') THEN RETURN; END IF;
    SELECT baseline_tick INTO profile_tick FROM city_open_world_service_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL THEN RAISE EXCEPTION 'city open-world V7/V8/V9 service profile is missing' USING ERRCODE = '23514'; END IF;
    IF (SELECT COUNT(*) FROM city_open_world_service_catalog WHERE world_id = target_world_id) <> 4 THEN
        RAISE EXCEPTION 'city open-world V7/V8/V9 service catalog is incomplete' USING ERRCODE = '23514'; END IF;
    IF NOT EXISTS (SELECT 1 FROM city_open_world_service_providers WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V7/V8/V9 service providers are missing' USING ERRCODE = '23514'; END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_impact_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); profile_tick BIGINT; world_tick BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v8', 'city-openworld-v9') THEN RETURN; END IF;
    SELECT baseline_tick INTO profile_tick FROM city_open_world_impact_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V8/V9 impact profile is missing or has an invalid baseline' USING ERRCODE = '23514'; END IF;
    IF (SELECT COUNT(*) FROM city_open_world_impact_catalog WHERE world_id = target_world_id) <> 8 THEN
        RAISE EXCEPTION 'city open-world V8/V9 impact catalog is incomplete' USING ERRCODE = '23514'; END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_mobility_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    profile_tick BIGINT;
    world_tick BIGINT;
    expected_hash VARCHAR(64);
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v9' THEN RETURN; END IF;
    SELECT baseline_tick, content_hash INTO profile_tick, expected_hash
    FROM city_open_world_mobility_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick OR expected_hash IS NULL THEN
        RAISE EXCEPTION 'city open-world V9 mobility profile is missing or has an invalid baseline' USING ERRCODE = '23514'; END IF;
    IF (SELECT COUNT(*) FROM city_open_world_mobility_modes WHERE world_id = target_world_id) <> 3
       OR (SELECT COUNT(*) FROM city_open_world_mobility_hubs WHERE world_id = target_world_id) < 3
       OR (SELECT COUNT(*) FROM city_open_world_mobility_edges WHERE world_id = target_world_id) = 0 THEN
        RAISE EXCEPTION 'city open-world V9 mobility topology is incomplete' USING ERRCODE = '23514'; END IF;
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
        RAISE EXCEPTION 'city open-world V9 mobility profile counters are inconsistent' USING ERRCODE = '23514'; END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_world_version_vector(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE world_version VARCHAR(32); vector_generation SMALLINT; vector_version VARCHAR(32);
BEGIN
    SELECT simulation_version, version_vector_generation INTO world_version, vector_generation FROM city_worlds WHERE id = target_world_id;
    IF world_version !~ '^city-openworld-v[0-9]+$' OR vector_generation < 1 THEN
        RAISE EXCEPTION 'city world version vector only applies to versioned open worlds' USING ERRCODE = '23514'; END IF;
    SELECT engine_version INTO vector_version FROM city_world_version_vectors
    WHERE world_id = target_world_id AND generation = vector_generation;
    IF vector_version IS DISTINCT FROM world_version THEN
        RAISE EXCEPTION 'city world active version vector header is missing or stale' USING ERRCODE = '23514'; END IF;
    IF (SELECT COUNT(*) FROM city_world_version_bindings WHERE world_id = target_world_id AND generation = vector_generation) <> 7 THEN
        RAISE EXCEPTION 'city open-world version vector requires exactly seven bindings' USING ERRCODE = '23514'; END IF;
    IF EXISTS (SELECT 1 FROM (VALUES ('content_catalog'), ('economic_policy'), ('engine'), ('rule_bundle'),
        ('scenario'), ('spatial_profile'), ('worldgen_plan')) AS required(component_code)
        WHERE NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding
                          WHERE binding.world_id = target_world_id AND binding.generation = vector_generation
                            AND binding.component_code = required.component_code)) THEN
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
END;
$$;
