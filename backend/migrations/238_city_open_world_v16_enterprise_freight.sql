-- city-openworld-v16 / F9.2.C creates a deliberately narrow adapter from a
-- sealed V15 dispatch to a V9 cargo demand.  It records causal linkage and
-- transport observation only: route completion never transfers inventory,
-- settles an order, or moves the system carrier into the local actor space.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v16', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","version_vector","facilities","npc_lod","social_service_catalog","social_service_access","social_service_queue","social_service_response","cross_domain_impact_bridge","aggregate_mobility_graph","mobility_capacity_allocation","mobility_route_lifecycle","mobility_arrival_bridge","cross_scale_spatial_transfer","versioned_od_sources","automatic_od_demands","mobility_cycle_metrics","residence_employment_bindings","verified_commute_sources","facility_presence_origin_validation","dual_direction_commutes","commute_assignment_epochs","commute_lifecycle_transitions","enterprise_supply_chain_nodes","supply_orders","inventory_reservations","purchase_settlement","fact_backed_dispatch","atomic_delivery","enterprise_freight_source_adapter","enterprise_freight_transport_observation","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v15', 'city-openworld-v16', 'openworld_v15_to_v16')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_enterprise_freight_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_supply_chain_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    source_contract VARCHAR(96) NOT NULL,
    demand_contract VARCHAR(96) NOT NULL,
    completion_contract VARCHAR(96) NOT NULL,
    terminal_contract VARCHAR(96) NOT NULL,
    carrier_actor_code VARCHAR(160) NOT NULL,
    maximum_sources INTEGER NOT NULL CHECK (maximum_sources BETWEEN 1 AND 100000),
    maximum_generations_per_tick INTEGER NOT NULL CHECK (maximum_generations_per_tick BETWEEN 1 AND 100000),
    source_count BIGINT NOT NULL DEFAULT 0 CHECK (source_count >= 0),
    pending_count BIGINT NOT NULL DEFAULT 0 CHECK (pending_count >= 0),
    demand_count BIGINT NOT NULL DEFAULT 0 CHECK (demand_count >= 0),
    scheduled_count BIGINT NOT NULL DEFAULT 0 CHECK (scheduled_count >= 0),
    completed_count BIGINT NOT NULL DEFAULT 0 CHECK (completed_count >= 0),
    expired_count BIGINT NOT NULL DEFAULT 0 CHECK (expired_count >= 0),
    voided_count BIGINT NOT NULL DEFAULT 0 CHECK (voided_count >= 0),
    orphaned_count BIGINT NOT NULL DEFAULT 0 CHECK (orphaned_count >= 0),
    suppressed_count BIGINT NOT NULL DEFAULT 0 CHECK (suppressed_count >= 0),
    fact_count BIGINT NOT NULL DEFAULT 0 CHECK (fact_count >= 0),
    transition_count BIGINT NOT NULL DEFAULT 0 CHECK (transition_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_enterprise_freight_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-enterprise-freight'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND source_contract = 'v15_dispatched_fact_snapshot_v1'
        AND demand_contract = 'v9_system_carrier_demand_v1'
        AND completion_contract = 'v9_transport_observation_no_receipt_v1'
        AND terminal_contract = 'v15_terminal_pending_demand_void_v1'
        AND carrier_actor_code = 'system.freight.carrier'
        AND maximum_sources = 10000
        AND maximum_generations_per_tick = 128
    ),
    CONSTRAINT city_open_world_enterprise_freight_profile_counter_check CHECK (
        pending_count <= demand_count
        AND scheduled_count <= demand_count
        AND completed_count <= demand_count
        AND expired_count <= demand_count
        AND voided_count <= demand_count
        AND orphaned_count <= source_count
        AND suppressed_count <= source_count
    ),
    CONSTRAINT city_open_world_enterprise_freight_profile_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'scope' = 'dispatch_to_v9_freight_demand_only'
        AND metadata->>'receipt' = 'not_implemented'
        AND metadata->>'maximum_requested_units' = '32'
        AND metadata->>'mode_code' = 'freight'
        AND metadata->>'purpose_code' = 'enterprise.freight'
    )
);

CREATE TABLE IF NOT EXISTS city_open_world_enterprise_freight_sources (
    id BIGSERIAL NOT NULL,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_enterprise_freight_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    order_code VARCHAR(160) NOT NULL,
    seller_node_code VARCHAR(160) NOT NULL,
    buyer_node_code VARCHAR(160) NOT NULL,
    source_hub_code VARCHAR(160) NOT NULL,
    destination_hub_code VARCHAR(160) NOT NULL,
    carrier_actor_id BIGINT NOT NULL,
    dispatch_fact_id BIGINT NOT NULL,
    dispatch_tick BIGINT NOT NULL CHECK (dispatch_tick > 0),
    source_tick BIGINT NOT NULL CHECK (source_tick > 0),
    mobility_deadline_tick BIGINT NOT NULL CHECK (mobility_deadline_tick > 0),
    requested_units BIGINT NOT NULL CHECK (requested_units > 0),
    state VARCHAR(24) NOT NULL,
    mobility_demand_id BIGINT,
    mobility_route_id BIGINT,
    source_runtime_fact_id BIGINT NOT NULL,
    last_runtime_fact_id BIGINT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_enterprise_freight_source_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_open_world_enterprise_freight_source_order_unique UNIQUE (world_id, order_code),
    CONSTRAINT city_open_world_enterprise_freight_source_seller_node_fk
        FOREIGN KEY (world_id, seller_node_code)
        REFERENCES city_open_world_supply_chain_nodes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_source_buyer_node_fk
        FOREIGN KEY (world_id, buyer_node_code)
        REFERENCES city_open_world_supply_chain_nodes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_source_source_hub_fk
        FOREIGN KEY (world_id, source_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_source_destination_hub_fk
        FOREIGN KEY (world_id, destination_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_source_carrier_fk
        FOREIGN KEY (carrier_actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_source_dispatch_fact_fk
        FOREIGN KEY (dispatch_fact_id, world_id)
        REFERENCES city_open_world_supply_chain_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_source_demand_fk
        FOREIGN KEY (mobility_demand_id, world_id)
        REFERENCES city_open_world_mobility_demands(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_enterprise_freight_source_route_fk
        FOREIGN KEY (mobility_route_id, world_id)
        REFERENCES city_open_world_mobility_routes(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_enterprise_freight_source_runtime_fact_fk
        FOREIGN KEY (source_runtime_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_enterprise_freight_source_last_fact_fk
        FOREIGN KEY (last_runtime_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_enterprise_freight_source_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND order_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND seller_node_code <> buyer_node_code
        AND source_hub_code <> destination_hub_code
        AND dispatch_tick < source_tick
        AND source_tick < mobility_deadline_tick
        AND state IN ('demand_pending','route_scheduled','route_completed','demand_expired','voided','transport_orphaned','suppressed')
        AND ((state = 'suppressed' AND requested_units > 32)
             OR (state <> 'suppressed' AND requested_units BETWEEN 1 AND 32))
    ),
    CONSTRAINT city_open_world_enterprise_freight_source_lifecycle_check CHECK (
        (state = 'suppressed' AND mobility_demand_id IS NULL AND mobility_route_id IS NULL)
        OR (state IN ('demand_pending','demand_expired','voided') AND mobility_demand_id IS NOT NULL AND mobility_route_id IS NULL)
        OR (state IN ('route_scheduled','route_completed','transport_orphaned') AND mobility_demand_id IS NOT NULL AND mobility_route_id IS NOT NULL)
    ),
    CONSTRAINT city_open_world_enterprise_freight_source_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_enterprise_freight_source_lines (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL,
    source_code VARCHAR(160) NOT NULL,
    line_no INTEGER NOT NULL CHECK (line_no > 0),
    resource_code VARCHAR(160) NOT NULL,
    source_firm_code VARCHAR(160) NOT NULL,
    source_district_code VARCHAR(160) NOT NULL,
    destination_firm_code VARCHAR(160) NOT NULL,
    destination_district_code VARCHAR(160) NOT NULL,
    quantity_units BIGINT NOT NULL CHECK (quantity_units > 0),
    unit_price_units BIGINT NOT NULL CHECK (unit_price_units > 0),
    total_price_units BIGINT NOT NULL CHECK (total_price_units > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_enterprise_freight_line_source_fk
        FOREIGN KEY (world_id, source_code)
        REFERENCES city_open_world_enterprise_freight_sources(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_line_identity_check CHECK (
        resource_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND source_firm_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND source_district_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND destination_firm_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND destination_district_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND quantity_units::NUMERIC * unit_price_units::NUMERIC = total_price_units::NUMERIC
    ),
    CONSTRAINT city_open_world_enterprise_freight_line_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_enterprise_freight_line_unique UNIQUE (world_id, source_code, line_no)
);

CREATE TABLE IF NOT EXISTS city_open_world_enterprise_freight_facts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_enterprise_freight_profiles(world_id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    source_code VARCHAR(160) NOT NULL,
    fact_type VARCHAR(64) NOT NULL,
    runtime_fact_id BIGINT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_enterprise_freight_fact_source_fk
        FOREIGN KEY (world_id, source_code)
        REFERENCES city_open_world_enterprise_freight_sources(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_fact_runtime_fk
        FOREIGN KEY (runtime_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_enterprise_freight_fact_identity_check CHECK (
        fact_type IN ('source.created','source.suppressed','demand.requested','route.scheduled','route.completed','demand.expired','demand.voided','transport.orphaned')
    ),
    CONSTRAINT city_open_world_enterprise_freight_fact_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT city_open_world_enterprise_freight_fact_cursor_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_open_world_enterprise_freight_fact_runtime_unique UNIQUE (world_id, runtime_fact_id)
);

CREATE TABLE IF NOT EXISTS city_open_world_enterprise_freight_transitions (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_enterprise_freight_profiles(world_id) ON DELETE RESTRICT,
    source_code VARCHAR(160) NOT NULL,
    transition_tick BIGINT NOT NULL CHECK (transition_tick > 0),
    transition_sequence BIGINT NOT NULL CHECK (transition_sequence > 0),
    state VARCHAR(24) NOT NULL,
    reason_code VARCHAR(96) NOT NULL,
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_enterprise_freight_transition_source_fk
        FOREIGN KEY (world_id, source_code)
        REFERENCES city_open_world_enterprise_freight_sources(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_transition_fact_fk
        FOREIGN KEY (source_fact_id)
        REFERENCES city_open_world_enterprise_freight_facts(id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_transition_identity_check CHECK (
        state IN ('demand_pending','route_scheduled','route_completed','demand_expired','voided','transport_orphaned','suppressed')
        AND reason_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
    ),
    CONSTRAINT city_open_world_enterprise_freight_transition_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_enterprise_freight_transition_cursor_unique UNIQUE (world_id, source_code, transition_tick, transition_sequence)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_enterprise_freight_sources_state
    ON city_open_world_enterprise_freight_sources (world_id, state, source_tick, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_enterprise_freight_facts_source
    ON city_open_world_enterprise_freight_facts (world_id, source_code, tick, sequence);

CREATE OR REPLACE FUNCTION city_open_world_enterprise_freight_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_enterprise_freight_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v16'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v16'
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_enterprise_freight_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_enterprise_freight_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version = 'city-openworld-v16')
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_enterprise_freight_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_enterprise_freight_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_enterprise_freight_write_enabled(target_world_id)
       AND NEW.revision = OLD.revision + 1
       AND (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.source_contract, OLD.demand_contract,
            OLD.completion_contract, OLD.terminal_contract, OLD.carrier_actor_code,
            OLD.maximum_sources, OLD.maximum_generations_per_tick, OLD.metadata, OLD.created_at)
           IS NOT DISTINCT FROM
           (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.source_contract, NEW.demand_contract,
            NEW.completion_contract, NEW.terminal_contract, NEW.carrier_actor_code,
            NEW.maximum_sources, NEW.maximum_generations_per_tick, NEW.metadata, NEW.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V16 enterprise-freight profile is immutable outside its audited projection write path'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_enterprise_freight_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT; source_fact BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' OR NOT city_open_world_enterprise_freight_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V16 enterprise-freight projections require their audited write context'
            USING ERRCODE = '55000';
    END IF;
    IF TG_TABLE_NAME = 'city_open_world_enterprise_freight_sources' THEN
        source_fact := CASE WHEN TG_OP = 'INSERT' THEN NEW.last_runtime_fact_id ELSE NEW.last_runtime_fact_id END;
        IF NOT EXISTS (
            SELECT 1 FROM city_open_world_runtime_facts fact
            WHERE fact.id = source_fact AND fact.world_id = target_world_id
              AND fact.posted_at IS NULL
        ) THEN
            RAISE EXCEPTION 'open-world V16 enterprise-freight source must reference a draft runtime fact'
                USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_open_world_enterprise_freight_facts' THEN
        IF NOT EXISTS (
            SELECT 1 FROM city_open_world_runtime_facts fact
            WHERE fact.id = NEW.runtime_fact_id AND fact.world_id = target_world_id
              AND fact.tick = NEW.tick AND fact.sequence = NEW.sequence
              AND fact.posted_at IS NULL
        ) THEN
            RAISE EXCEPTION 'open-world V16 enterprise-freight fact must reference its draft runtime fact'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_enterprise_freight_profile_guard ON city_open_world_enterprise_freight_profiles;
CREATE TRIGGER city_open_world_enterprise_freight_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_enterprise_freight_profile();

DO $$
DECLARE table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_open_world_enterprise_freight_sources',
        'city_open_world_enterprise_freight_source_lines',
        'city_open_world_enterprise_freight_facts',
        'city_open_world_enterprise_freight_transitions'
    ] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', table_name || '_guard', table_name);
        EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_enterprise_freight_projection()', table_name || '_guard', table_name);
    END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_enterprise_freight_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version VARCHAR(32); world_tick BIGINT; profile_tick BIGINT;
    sources BIGINT; pending BIGINT; demands BIGINT; scheduled BIGINT; completed BIGINT;
    expired BIGINT; voided BIGINT; orphaned BIGINT; suppressed BIGINT; facts BIGINT; transitions BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v16' THEN RETURN; END IF;
    IF NOT EXISTS (SELECT 1 FROM city_open_world_supply_chain_profiles WHERE world_id = target_world_id)
       OR NOT EXISTS (SELECT 1 FROM city_open_world_mobility_profiles WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V16 enterprise-freight predecessor foundations are missing' USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM city_open_world_mobility_modes mode
        WHERE mode.world_id = target_world_id
          AND mode.code = 'freight'
          AND mode.unit_kind = 'cargo'
          AND mode.capacity_units_per_tick >= 32
    ) OR EXISTS (
        SELECT 1
        FROM city_open_world_mobility_edges edge
        WHERE edge.world_id = target_world_id
          AND edge.mode_code = 'freight'
          AND edge.capacity_units_per_tick < 32
    ) THEN
        RAISE EXCEPTION 'city open-world V16 enterprise-freight requires a 32-unit freight edge baseline' USING ERRCODE = '23514';
    END IF;
    SELECT baseline_tick, source_count, pending_count, demand_count, scheduled_count,
           completed_count, expired_count, voided_count, orphaned_count, suppressed_count,
           fact_count, transition_count
      INTO profile_tick, sources, pending, demands, scheduled, completed, expired,
           voided, orphaned, suppressed, facts, transitions
    FROM city_open_world_enterprise_freight_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V16 enterprise-freight profile is missing or invalid' USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM city_open_world_actors actor
        WHERE actor.world_id = target_world_id
          AND actor.code = 'system.freight.carrier'
          AND actor.actor_type_code = 'system.freight_carrier'
          AND actor.status = 'active' AND actor.owner_user_id IS NULL
    ) OR EXISTS (
        SELECT 1 FROM city_open_world_actor_locations location
        JOIN city_open_world_actors actor ON actor.id = location.actor_id AND actor.world_id = location.world_id
        WHERE actor.world_id = target_world_id AND actor.code = 'system.freight.carrier'
    ) THEN
        RAISE EXCEPTION 'city open-world V16 enterprise-freight carrier identity is invalid' USING ERRCODE = '23514';
    END IF;
    IF sources <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_sources WHERE world_id = target_world_id)
       OR pending <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_sources WHERE world_id = target_world_id AND state = 'demand_pending')
       OR demands <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_sources WHERE world_id = target_world_id AND mobility_demand_id IS NOT NULL)
       OR scheduled <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_sources WHERE world_id = target_world_id AND state = 'route_scheduled')
       OR completed <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_sources WHERE world_id = target_world_id AND state = 'route_completed')
       OR expired <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_sources WHERE world_id = target_world_id AND state = 'demand_expired')
       OR voided <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_sources WHERE world_id = target_world_id AND state = 'voided')
       OR orphaned <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_sources WHERE world_id = target_world_id AND state = 'transport_orphaned')
       OR suppressed <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_sources WHERE world_id = target_world_id AND state = 'suppressed')
       OR facts <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_facts WHERE world_id = target_world_id)
       OR transitions <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_transitions WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V16 enterprise-freight counters are inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_sources source
        JOIN city_open_world_supply_chain_facts dispatch_fact
          ON dispatch_fact.id = source.dispatch_fact_id AND dispatch_fact.world_id = source.world_id
        JOIN city_open_world_actors carrier
          ON carrier.id = source.carrier_actor_id AND carrier.world_id = source.world_id
        WHERE source.world_id = target_world_id
          AND (dispatch_fact.fact_type <> 'order.dispatched'
               OR dispatch_fact.tick <> source.dispatch_tick
               OR carrier.code <> 'system.freight.carrier'
               OR carrier.owner_user_id IS NOT NULL
               OR (source.state = 'suppressed' AND source.requested_units <= 32)
               OR (source.state <> 'suppressed' AND source.requested_units > 32))
    ) OR EXISTS (
        SELECT 1 FROM city_open_world_enterprise_freight_sources source
        WHERE source.world_id = target_world_id
          AND NOT EXISTS (
              SELECT 1 FROM city_open_world_enterprise_freight_source_lines line
              WHERE line.world_id = source.world_id AND line.source_code = source.code
          )
    ) THEN
        RAISE EXCEPTION 'city open-world V16 enterprise-freight source identity is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_facts fact
        JOIN city_open_world_runtime_facts runtime_fact
          ON runtime_fact.id = fact.runtime_fact_id AND runtime_fact.world_id = fact.world_id
        WHERE fact.world_id = target_world_id
          AND (runtime_fact.tick <> fact.tick OR runtime_fact.sequence <> fact.sequence OR runtime_fact.posted_at IS NULL)
    ) OR EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_sources source
        JOIN city_open_world_mobility_demands demand
          ON demand.id = source.mobility_demand_id AND demand.world_id = source.world_id
        WHERE source.world_id = target_world_id
          AND demand.metadata->'transport_adapter'->>'arrival_bridge' IS DISTINCT FROM 'excluded'
    ) THEN
        RAISE EXCEPTION 'city open-world V16 enterprise-freight runtime linkage is invalid' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_open_world_enterprise_freight_foundation_commit()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        RETURN COALESCE(NEW, OLD);
    END IF;
    PERFORM assert_city_open_world_enterprise_freight_foundation(target_world_id);
    RETURN COALESCE(NEW, OLD);
END;
$$;

DO $$
DECLARE table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_open_world_enterprise_freight_profiles',
        'city_open_world_enterprise_freight_sources',
        'city_open_world_enterprise_freight_source_lines',
        'city_open_world_enterprise_freight_facts',
        'city_open_world_enterprise_freight_transitions'
    ] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', 'city_open_world_enterprise_freight_' || table_name || '_commit_check', table_name);
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_city_open_world_enterprise_freight_foundation_commit()', 'city_open_world_enterprise_freight_' || table_name || '_commit_check', table_name);
    END LOOP;
END;
$$;

-- V16 is a strict successor. Extend every predecessor write boundary without
-- making older-world rows mutable. Runtime bootstrap additionally accepts a
-- running V15->V16 upgrade so the system carrier can be added at the frozen
-- baseline tick before the new snapshot is sealed.
CREATE OR REPLACE FUNCTION city_open_world_supply_chain_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_supply_chain_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v15','city-openworld-v16')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_supply_chain_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_supply_chain_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v15','city-openworld-v16')
                   AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_lifecycle_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_lifecycle_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v14','city-openworld-v15','city-openworld-v16')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_lifecycle_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_lifecycle_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v14','city-openworld-v15','city-openworld-v16'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_source_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_source_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_source_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_source_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_arrival_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_arrival_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_arrival_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_od_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_od_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_od_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_service_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_impact_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v1','city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_materialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_materialization_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_fact_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_fact_insert()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE world_tick BIGINT; world_version VARCHAR(32); command_status_value VARCHAR(16);
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN RETURN NEW; END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version FROM city_worlds WHERE id = NEW.world_id;
    IF world_version NOT IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16')
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
       AND NOT (world_version IN ('city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16') AND NEW.fact_type LIKE 'system.%') THEN
        RAISE EXCEPTION 'derived open-world runtime fact requires a parent fact' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION city_open_world_supply_delivery_resource_operation_authorized(target_operation_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT EXISTS (
        SELECT 1
        FROM city_resource_operations operation
        JOIN city_worlds world ON world.id = operation.world_id
        JOIN city_commands command ON command.id = operation.source_command_id AND command.world_id = operation.world_id
        JOIN city_open_world_supply_chain_facts fact
          ON fact.world_id = operation.world_id AND fact.source_command_id = operation.source_command_id
         AND fact.fact_type = 'order.delivered' AND fact.tick = operation.tick
        JOIN city_open_world_supply_chain_orders supply_order
          ON supply_order.world_id = fact.world_id AND supply_order.code = fact.order_code
        JOIN city_open_world_supply_chain_nodes seller
          ON seller.world_id = supply_order.world_id AND seller.code = supply_order.seller_node_code
        JOIN city_open_world_supply_chain_order_transitions transition
          ON transition.world_id = fact.world_id AND transition.order_code = fact.order_code
         AND transition.source_fact_id = fact.id AND transition.state = 'delivered'
        WHERE operation.id = target_operation_id
          AND world.simulation_version IN ('city-openworld-v15','city-openworld-v16')
          AND operation.operation_type = 'transfer'
          AND command.command_type = 'open_world.supply_order.deliver'
          AND operation.actor_entity_id = seller.firm_entity_id
          AND operation.district_id = seller.district_id
    )
$$;

-- V16 owns a new immutable content-catalog bundle. Keep the database-level
-- assertion aligned with the Go canonical-vector validator so an upgrade can
-- never commit with the V15 catalog still active.
CREATE OR REPLACE FUNCTION assert_city_world_version_vector(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); vector_generation SMALLINT; vector_version VARCHAR(32);
BEGIN
    SELECT simulation_version, version_vector_generation
      INTO world_version, vector_generation
    FROM city_worlds WHERE id = target_world_id;
    IF world_version !~ '^city-openworld-v[0-9]+$' OR vector_generation < 1 THEN
        RAISE EXCEPTION 'city world version vector only applies to versioned open worlds' USING ERRCODE = '23514';
    END IF;
    SELECT engine_version INTO vector_version
    FROM city_world_version_vectors
    WHERE world_id = target_world_id AND generation = vector_generation;
    IF vector_version IS DISTINCT FROM world_version THEN
        RAISE EXCEPTION 'city world active version vector header is missing or stale' USING ERRCODE = '23514';
    END IF;
    IF (SELECT COUNT(*) FROM city_world_version_bindings
        WHERE world_id = target_world_id AND generation = vector_generation) <> 7 THEN
        RAISE EXCEPTION 'city open-world version vector requires exactly seven bindings' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM (VALUES ('content_catalog'), ('economic_policy'), ('engine'),
                     ('rule_bundle'), ('scenario'), ('spatial_profile'),
                     ('worldgen_plan')) AS required(component_code)
        WHERE NOT EXISTS (
            SELECT 1 FROM city_world_version_bindings binding
            WHERE binding.world_id = target_world_id
              AND binding.generation = vector_generation
              AND binding.component_code = required.component_code
        )
    ) THEN
        RAISE EXCEPTION 'city open-world version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v14' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding
        WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation
          AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-commute-lifecycle-catalog'
          AND binding.bundle_version = '1.0.0'
    ) THEN
        RAISE EXCEPTION 'city open-world V14 commute lifecycle version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v15' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding
        WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation
          AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-supply-chain-catalog'
          AND binding.bundle_version = '1.0.0'
    ) THEN
        RAISE EXCEPTION 'city open-world V15 supply-chain version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v16' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding
        WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation
          AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-enterprise-freight-catalog'
          AND binding.bundle_version = '1.0.0'
    ) THEN
        RAISE EXCEPTION 'city open-world V16 enterprise-freight version vector is incomplete' USING ERRCODE = '23514';
    END IF;
END;
$$;
