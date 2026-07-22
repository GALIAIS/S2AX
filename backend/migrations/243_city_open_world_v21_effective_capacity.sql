-- city-openworld-v21 / F9.3.2: explicit, forward-only consumption of V20
-- corridor-segment capacity by V9's future route admissions. Existing V9
-- allocations remain immutable historical evidence and are never backfilled.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
SELECT
    'city-openworld-v21',
    'supported',
    'city-state-v1+gzip',
    COALESCE(
        (SELECT capabilities FROM city_engine_versions WHERE version = 'city-openworld-v20'),
        '[]'::jsonb
    ) || '["effective_infrastructure_capacity","capacity_aware_route_admission","infrastructure_capacity_audit"]'::jsonb
ON CONFLICT (version) DO UPDATE
SET status = EXCLUDED.status,
    canonical_format = EXCLUDED.canonical_format,
    capabilities = EXCLUDED.capabilities;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v20', 'city-openworld-v21', 'openworld_v20_to_v21')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_effective_capacity_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_infrastructure_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    topology_contract VARCHAR(96) NOT NULL,
    asset_contract VARCHAR(96) NOT NULL,
    admission_contract VARCHAR(96) NOT NULL,
    visibility_contract VARCHAR(96) NOT NULL,
    maximum_admissions INTEGER NOT NULL CHECK (maximum_admissions BETWEEN 1 AND 1000000),
    admission_count BIGINT NOT NULL DEFAULT 0 CHECK (admission_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_effective_capacity_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-effective-capacity'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND topology_contract = 'v19_edge_corridor_mapping_v1'
        AND asset_contract = 'v20_corridor_segment_ordinal_1_v1'
        AND admission_contract = 'effective_infrastructure_capacity_v1'
        AND visibility_contract = 'next_tick_after_command_v1'
        AND maximum_admissions = 1000000
        AND admission_count <= maximum_admissions
        AND revision = admission_count + 1
    ),
    CONSTRAINT city_open_world_effective_capacity_profile_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'scope' = 'v20_corridor_assets_to_v9_future_admission_only'
        AND metadata->>'topology_contract' = 'v19_edge_corridor_mapping_v1'
        AND metadata->>'asset_contract' = 'v20_corridor_segment_ordinal_1_v1'
        AND metadata->>'admission_contract' = 'effective_infrastructure_capacity_v1'
        AND metadata->>'visibility_contract' = 'next_tick_after_command_v1'
    )
);

CREATE TABLE IF NOT EXISTS city_open_world_effective_capacity_admissions (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_effective_capacity_profiles(world_id) ON DELETE RESTRICT,
    route_id BIGINT NOT NULL,
    edge_code VARCHAR(160) NOT NULL,
    departure_tick BIGINT NOT NULL CHECK (departure_tick > 0),
    corridor_code VARCHAR(160) NOT NULL,
    asset_code VARCHAR(160) NOT NULL,
    asset_state VARCHAR(24) NOT NULL,
    state_effective_tick BIGINT NOT NULL CHECK (state_effective_tick >= 0),
    state_source_fact_id BIGINT,
    schedule_fact_id BIGINT NOT NULL,
    baseline_capacity_units_per_tick BIGINT NOT NULL CHECK (baseline_capacity_units_per_tick > 0),
    capacity_milli BIGINT NOT NULL CHECK (capacity_milli BETWEEN 0 AND 1000),
    effective_capacity_units_per_tick BIGINT NOT NULL CHECK (effective_capacity_units_per_tick > 0),
    allocated_units BIGINT NOT NULL CHECK (allocated_units > 0),
    occupancy_milli INTEGER NOT NULL CHECK (occupancy_milli BETWEEN 1 AND 1000),
    delay_ticks BIGINT NOT NULL CHECK (delay_ticks >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, route_id, edge_code),
    CONSTRAINT city_open_world_effective_capacity_admission_route_fk
        FOREIGN KEY (route_id, world_id)
        REFERENCES city_open_world_mobility_routes(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_effective_capacity_admission_edge_fk
        FOREIGN KEY (world_id, edge_code)
        REFERENCES city_open_world_mobility_edges(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_effective_capacity_admission_corridor_fk
        FOREIGN KEY (world_id, corridor_code)
        REFERENCES city_open_world_spatial_network_corridors(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_effective_capacity_admission_asset_fk
        FOREIGN KEY (world_id, asset_code)
        REFERENCES city_open_world_infrastructure_assets(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_effective_capacity_admission_state_fact_fk
        FOREIGN KEY (state_source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_effective_capacity_admission_schedule_fact_fk
        FOREIGN KEY (schedule_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_effective_capacity_admission_shape_check CHECK (
        allocated_units <= effective_capacity_units_per_tick
        AND effective_capacity_units_per_tick <= baseline_capacity_units_per_tick
        AND ((asset_state = 'operational' AND capacity_milli = 1000)
             OR (asset_state = 'restricted' AND capacity_milli BETWEEN 1 AND 999))
    ),
    CONSTRAINT city_open_world_effective_capacity_admission_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'admission_contract' = 'effective_infrastructure_capacity_v1'
        AND metadata->>'topology_contract' = 'v19_edge_corridor_mapping_v1'
        AND metadata->>'asset_contract' = 'v20_corridor_segment_ordinal_1_v1'
        AND metadata->>'visibility_contract' = 'next_tick_after_command_v1'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_effective_capacity_admissions_timeline
    ON city_open_world_effective_capacity_admissions (world_id, departure_tick, edge_code, route_id);
CREATE INDEX IF NOT EXISTS idx_city_open_world_effective_capacity_admissions_asset
    ON city_open_world_effective_capacity_admissions (world_id, asset_code, departure_tick);

CREATE OR REPLACE FUNCTION city_open_world_effective_capacity_recovery_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_recovery_write_enabled(target_world_id)
       OR COALESCE(current_setting('sub2api.city_open_world_effective_capacity_recovery_world_id', TRUE), '') = target_world_id::TEXT
$$;

CREATE OR REPLACE FUNCTION city_open_world_effective_capacity_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_effective_capacity_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v21'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v21'
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_effective_capacity_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_effective_capacity_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v21'
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_effective_capacity_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_effective_capacity_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_effective_capacity_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_effective_capacity_write_enabled(target_world_id)
       AND NEW.admission_count = OLD.admission_count + 1
       AND NEW.revision = OLD.revision + 1
       AND (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.topology_contract, OLD.asset_contract,
            OLD.admission_contract, OLD.visibility_contract, OLD.maximum_admissions,
            OLD.metadata, OLD.created_at)
           IS NOT DISTINCT FROM
           (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.topology_contract, NEW.asset_contract,
            NEW.admission_contract, NEW.visibility_contract, NEW.maximum_admissions,
            NEW.metadata, NEW.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V21 effective-capacity profile requires audited bootstrap, admission, or recovery context'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_effective_capacity_admission()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    target_world_id BIGINT;
    expected_capacity BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_effective_capacity_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP <> 'INSERT' OR NOT city_open_world_effective_capacity_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V21 effective-capacity admissions are append-only audited evidence'
            USING ERRCODE = '55000';
    END IF;
    expected_capacity := floor((NEW.baseline_capacity_units_per_tick::NUMERIC * NEW.capacity_milli::NUMERIC) / 1000)::BIGINT;
    IF expected_capacity <> NEW.effective_capacity_units_per_tick
       OR NEW.effective_capacity_units_per_tick < NEW.allocated_units
       OR NOT EXISTS (
            SELECT 1
            FROM city_open_world_effective_capacity_profiles profile
            JOIN city_open_world_mobility_routes route
              ON route.id = NEW.route_id AND route.world_id = profile.world_id
            JOIN city_open_world_runtime_facts schedule_fact
              ON schedule_fact.id = NEW.schedule_fact_id AND schedule_fact.world_id = profile.world_id
            JOIN city_open_world_mobility_allocations allocation
              ON allocation.world_id = profile.world_id AND allocation.route_id = NEW.route_id
             AND allocation.edge_code = NEW.edge_code
            JOIN city_open_world_spatial_network_corridors corridor
              ON corridor.world_id = profile.world_id AND corridor.edge_code = NEW.edge_code
            JOIN city_open_world_infrastructure_assets asset
              ON asset.world_id = profile.world_id AND asset.asset_kind = 'corridor_segment'
             AND asset.spatial_corridor_code = corridor.code AND asset.segment_ordinal = 1
            JOIN LATERAL (
                SELECT transition.*
                FROM city_open_world_infrastructure_asset_transitions transition
                WHERE transition.world_id = profile.world_id AND transition.asset_code = asset.code
                  AND (transition.transition_tick < schedule_fact.tick
                       OR (transition.transition_tick = schedule_fact.tick
                           AND transition.transition_sequence < schedule_fact.sequence))
                ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
                LIMIT 1
            ) state_transition ON TRUE
            WHERE profile.world_id = target_world_id
              AND profile.baseline_tick < NEW.departure_tick
              AND profile.admission_count < profile.maximum_admissions
              AND route.departure_tick = NEW.departure_tick
              AND route.source_fact_id = NEW.schedule_fact_id
              AND schedule_fact.fact_type = 'mobility.scheduled'
              AND schedule_fact.tick = NEW.departure_tick
              AND allocation.departure_tick = NEW.departure_tick
              AND allocation.allocated_units = NEW.allocated_units
              AND allocation.capacity_units_per_tick = NEW.effective_capacity_units_per_tick
              AND allocation.occupancy_milli = NEW.occupancy_milli
              AND allocation.delay_ticks = NEW.delay_ticks
              AND corridor.code = NEW.corridor_code
              AND corridor.capacity_units_per_tick = NEW.baseline_capacity_units_per_tick
              AND asset.code = NEW.asset_code
              AND state_transition.to_state = NEW.asset_state
              AND state_transition.capacity_milli = NEW.capacity_milli
              AND state_transition.transition_tick = NEW.state_effective_tick
              AND state_transition.source_fact_id IS NOT DISTINCT FROM NEW.state_source_fact_id
              AND allocation.metadata->>'capacity_contract' = 'effective_infrastructure_capacity_v1'
              AND allocation.metadata->>'corridor_code' = NEW.corridor_code
              AND allocation.metadata->>'asset_code' = NEW.asset_code
              AND allocation.metadata->>'asset_state' = NEW.asset_state
              AND allocation.metadata->>'state_effective_tick' = NEW.state_effective_tick::TEXT
              AND allocation.metadata->>'baseline_capacity_units_per_tick' = NEW.baseline_capacity_units_per_tick::TEXT
              AND allocation.metadata->>'capacity_milli' = NEW.capacity_milli::TEXT
              AND allocation.metadata->>'effective_capacity_units_per_tick' = NEW.effective_capacity_units_per_tick::TEXT
       ) THEN
        RAISE EXCEPTION 'open-world V21 effective-capacity admission must match V9 route, V19 corridor, and visible V20 state'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_effective_capacity_profile_guard ON city_open_world_effective_capacity_profiles;
CREATE TRIGGER city_open_world_effective_capacity_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_effective_capacity_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_effective_capacity_profile();

DROP TRIGGER IF EXISTS city_open_world_effective_capacity_admission_guard ON city_open_world_effective_capacity_admissions;
CREATE TRIGGER city_open_world_effective_capacity_admission_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_effective_capacity_admissions
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_effective_capacity_admission();

CREATE OR REPLACE FUNCTION assert_city_open_world_effective_capacity_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    profile_tick BIGINT;
    profile_count BIGINT;
    profile_revision BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v21' THEN RETURN; END IF;
    IF NOT EXISTS (SELECT 1 FROM city_open_world_infrastructure_profiles WHERE world_id = target_world_id)
       OR NOT EXISTS (SELECT 1 FROM city_open_world_spatial_network_profiles WHERE world_id = target_world_id)
       OR NOT EXISTS (SELECT 1 FROM city_open_world_mobility_profiles WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V21 effective-capacity predecessor foundations are missing' USING ERRCODE = '23514';
    END IF;
    SELECT baseline_tick, admission_count, revision
      INTO profile_tick, profile_count, profile_revision
    FROM city_open_world_effective_capacity_profiles
    WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick
       OR profile_count <> (SELECT COUNT(*) FROM city_open_world_effective_capacity_admissions WHERE world_id = target_world_id)
       OR profile_revision <> profile_count + 1 THEN
        RAISE EXCEPTION 'city open-world V21 effective-capacity profile is missing or inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_effective_capacity_admissions admission
        JOIN city_open_world_mobility_routes route
          ON route.id = admission.route_id AND route.world_id = admission.world_id
        JOIN city_open_world_runtime_facts schedule_fact
          ON schedule_fact.id = admission.schedule_fact_id AND schedule_fact.world_id = admission.world_id
        JOIN city_open_world_mobility_allocations allocation
          ON allocation.world_id = admission.world_id AND allocation.route_id = admission.route_id
         AND allocation.edge_code = admission.edge_code
        JOIN city_open_world_spatial_network_corridors corridor
          ON corridor.world_id = admission.world_id AND corridor.edge_code = admission.edge_code
        JOIN city_open_world_infrastructure_assets asset
          ON asset.world_id = admission.world_id AND asset.asset_kind = 'corridor_segment'
         AND asset.spatial_corridor_code = corridor.code AND asset.segment_ordinal = 1
        JOIN LATERAL (
            SELECT transition.*
            FROM city_open_world_infrastructure_asset_transitions transition
            WHERE transition.world_id = admission.world_id AND transition.asset_code = asset.code
              AND (transition.transition_tick < schedule_fact.tick
                   OR (transition.transition_tick = schedule_fact.tick
                       AND transition.transition_sequence < schedule_fact.sequence))
            ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
            LIMIT 1
        ) state_transition ON TRUE
        WHERE admission.world_id = target_world_id
          AND (admission.departure_tick <= profile_tick
               OR route.departure_tick <> admission.departure_tick
               OR route.source_fact_id <> admission.schedule_fact_id
               OR schedule_fact.fact_type <> 'mobility.scheduled'
               OR schedule_fact.tick <> admission.departure_tick
               OR allocation.departure_tick <> admission.departure_tick
               OR allocation.allocated_units <> admission.allocated_units
               OR allocation.capacity_units_per_tick <> admission.effective_capacity_units_per_tick
               OR allocation.occupancy_milli <> admission.occupancy_milli
               OR allocation.delay_ticks <> admission.delay_ticks
               OR corridor.code <> admission.corridor_code
               OR corridor.capacity_units_per_tick <> admission.baseline_capacity_units_per_tick
               OR asset.code <> admission.asset_code
               OR state_transition.to_state <> admission.asset_state
               OR state_transition.capacity_milli <> admission.capacity_milli
               OR state_transition.transition_tick <> admission.state_effective_tick
               OR state_transition.source_fact_id IS DISTINCT FROM admission.state_source_fact_id
               OR floor((admission.baseline_capacity_units_per_tick::NUMERIC * admission.capacity_milli::NUMERIC) / 1000)::BIGINT <> admission.effective_capacity_units_per_tick
               OR allocation.metadata->>'capacity_contract' <> 'effective_infrastructure_capacity_v1')
    ) THEN
        RAISE EXCEPTION 'city open-world V21 effective-capacity admission evidence is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_mobility_allocations allocation
        JOIN city_open_world_mobility_routes route
          ON route.id = allocation.route_id AND route.world_id = allocation.world_id
        LEFT JOIN city_open_world_effective_capacity_admissions admission
          ON admission.world_id = allocation.world_id AND admission.route_id = allocation.route_id
         AND admission.edge_code = allocation.edge_code
        WHERE allocation.world_id = target_world_id
          AND ((allocation.departure_tick > profile_tick AND (admission.route_id IS NULL
               OR allocation.metadata->>'capacity_contract' <> 'effective_infrastructure_capacity_v1'))
               OR (allocation.departure_tick <= profile_tick AND admission.route_id IS NOT NULL))
    ) THEN
        RAISE EXCEPTION 'city open-world V21 allocation admission coverage is invalid' USING ERRCODE = '23514';
    END IF;
END;
$$;

-- V21 genesis still executes the V19/V20 initializers. Extend the exact V20
-- gates rather than weakening predecessor data models globally.
CREATE OR REPLACE FUNCTION city_open_world_infrastructure_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_infrastructure_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v20','city-openworld-v21')
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version IN ('city-openworld-v20','city-openworld-v21')
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_infrastructure_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_infrastructure_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v20','city-openworld-v21')
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_infrastructure_transition()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    target_world_id BIGINT;
    previous_state VARCHAR(24);
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_infrastructure_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_infrastructure_bootstrap_write_enabled(target_world_id)
       AND NEW.from_state = '' AND NEW.to_state = 'operational'
       AND NEW.capacity_milli = 1000 AND NEW.reason_code = 'baseline_initialized'
       AND NEW.source_fact_id IS NULL AND NEW.transition_sequence = 0
       AND NEW.metadata->>'schema_version' = '1'
       AND NEW.metadata->>'origin' = 'baseline'
       AND NEW.metadata->>'scheduler' = 'not_consumed_by_v9'
       AND EXISTS (SELECT 1 FROM city_open_world_infrastructure_profiles profile
                   WHERE profile.world_id = target_world_id AND profile.baseline_tick = NEW.transition_tick) THEN
        RETURN NEW;
    END IF;
    IF TG_OP <> 'INSERT' OR NOT city_open_world_infrastructure_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure transitions are append-only audited facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT transition.to_state INTO previous_state
    FROM city_open_world_infrastructure_asset_transitions transition
    WHERE transition.world_id = target_world_id AND transition.asset_code = NEW.asset_code
      AND (transition.transition_tick < NEW.transition_tick
           OR (transition.transition_tick = NEW.transition_tick AND transition.transition_sequence < NEW.transition_sequence))
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1;
    IF previous_state IS NULL OR NEW.from_state <> previous_state
       OR NEW.reason_code = 'baseline_initialized'
       OR NEW.metadata->>'schema_version' <> '1'
       OR NEW.metadata->>'origin' <> 'command'
       OR NEW.metadata->>'previous_state' <> NEW.from_state
       OR NEW.metadata->>'scheduler' <> 'not_consumed_by_v9'
       OR NOT ((NEW.from_state = 'operational' AND NEW.to_state IN ('restricted', 'maintenance', 'closed'))
               OR (NEW.from_state = 'restricted' AND NEW.to_state IN ('operational', 'maintenance', 'closed'))
               OR (NEW.from_state = 'maintenance' AND NEW.to_state IN ('operational', 'closed'))
               OR (NEW.from_state = 'closed' AND NEW.to_state = 'construction')
               OR (NEW.from_state = 'construction' AND NEW.to_state IN ('operational', 'closed'))) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure transition violates the lifecycle state machine'
            USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM city_open_world_runtime_facts fact
        WHERE fact.id = NEW.source_fact_id AND fact.world_id = target_world_id
          AND fact.tick = NEW.transition_tick AND fact.sequence = NEW.transition_sequence
          AND fact.fact_type = 'infrastructure.asset.transitioned'
          AND fact.payload->>'asset_code' = NEW.asset_code
          AND fact.payload->>'from_state' = NEW.from_state
          AND fact.payload->>'to_state' = NEW.to_state
          AND fact.payload->>'capacity_milli' = NEW.capacity_milli::TEXT
          AND fact.payload->>'reason_code' = NEW.reason_code
          AND ((fact.payload->>'v9_scheduler_effect' = 'none'
                AND COALESCE(fact.payload->>'v9_scheduler_effective_from_tick', '0') = '0')
               OR (fact.payload->>'v9_scheduler_effect' = 'next_tick_effective_capacity_v1'
                   AND fact.payload->>'v9_scheduler_effective_from_tick' = (NEW.transition_tick + 1)::TEXT
                   AND EXISTS (SELECT 1 FROM city_worlds world
                               WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-v21')))
    ) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure transition must match its runtime fact cursor'
            USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM city_open_world_infrastructure_asset_states state
        WHERE state.world_id = target_world_id AND state.asset_code = NEW.asset_code
          AND state.state = NEW.to_state AND state.capacity_milli = NEW.capacity_milli
          AND state.effective_tick = NEW.transition_tick AND state.source_fact_id = NEW.source_fact_id
          AND state.metadata->>'previous_state' = NEW.from_state
    ) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure transition must match the current state projection'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

-- V21 needs every predecessor projection to accept audited writes. Each
-- target is already V20-aware; extending its terminal version list preserves
-- the existing guards and rejects a migration if an expected definition drifts.
DO $$
DECLARE
    target_function REGPROCEDURE;
    definition TEXT;
BEGIN
    FOREACH target_function IN ARRAY ARRAY[
        'city_open_world_supply_chain_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_supply_chain_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_enterprise_freight_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_enterprise_freight_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_enterprise_freight_receipt_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_enterprise_freight_receipt_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_freight_batch_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_freight_batch_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_commute_lifecycle_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_commute_lifecycle_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_commute_source_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_commute_source_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_commute_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_mobility_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_mobility_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_arrival_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_arrival_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_mobility_od_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_mobility_od_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_service_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_service_fact_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_impact_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_impact_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_initialization_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_materialization_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_runtime_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_runtime_fact_write_enabled(bigint)'::REGPROCEDURE,
        'guard_city_open_world_runtime_fact_insert()'::REGPROCEDURE,
        'assert_city_open_world_enterprise_freight_foundation(bigint)'::REGPROCEDURE
    ] LOOP
        definition := pg_get_functiondef(target_function);
        definition := replace(definition, $old$'city-openworld-v20')$old$, $new$'city-openworld-v20','city-openworld-v21')$new$);
        IF position($needle$city-openworld-v21$needle$ IN definition) = 0 THEN
            RAISE EXCEPTION 'cannot extend V21 predecessor write gate %', target_function USING ERRCODE = '23514';
        END IF;
        EXECUTE definition;
    END LOOP;
END;
$$;

DO $$
DECLARE definition TEXT;
BEGIN
    definition := pg_get_functiondef('city_open_world_spatial_network_bootstrap_write_enabled(bigint)'::REGPROCEDURE);
    definition := replace(definition,
        $old$'city-openworld-v19','city-openworld-v20'$old$,
        $new$'city-openworld-v19','city-openworld-v20','city-openworld-v21'$new$);
    IF position($needle$city-openworld-v21$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V19 spatial-network bootstrap gate to V21' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;
