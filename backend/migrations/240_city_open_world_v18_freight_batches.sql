-- city-openworld-v18 / F10.2 turns only V16 overflow evidence into several
-- capacity-bounded V9 consignments. V15 still owns inventory and settlement:
-- all consignments must arrive before its single atomic delivery is allowed.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v18', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","version_vector","facilities","npc_lod","social_service_catalog","social_service_access","social_service_queue","social_service_response","cross_domain_impact_bridge","aggregate_mobility_graph","mobility_capacity_allocation","mobility_route_lifecycle","mobility_arrival_bridge","cross_scale_spatial_transfer","versioned_od_sources","automatic_od_demands","mobility_cycle_metrics","residence_employment_bindings","verified_commute_sources","facility_presence_origin_validation","dual_direction_commutes","commute_assignment_epochs","commute_lifecycle_transitions","enterprise_supply_chain_nodes","supply_orders","inventory_reservations","purchase_settlement","fact_backed_dispatch","atomic_delivery","enterprise_freight_source_adapter","enterprise_freight_transport_observation","enterprise_freight_custody","enterprise_freight_receipts","enterprise_freight_overflow_batches","capacity_bounded_consignments","batch_receipt_gate","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v17', 'city-openworld-v18', 'openworld_v17_to_v18')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_freight_batch_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_enterprise_freight_receipt_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    source_contract VARCHAR(96) NOT NULL,
    packing_contract VARCHAR(96) NOT NULL,
    transport_contract VARCHAR(96) NOT NULL,
    receipt_contract VARCHAR(96) NOT NULL,
    maximum_units BIGINT NOT NULL CHECK (maximum_units BETWEEN 1 AND 1000000),
    maximum_consignments_per_plan INTEGER NOT NULL CHECK (maximum_consignments_per_plan BETWEEN 2 AND 100000),
    maximum_plans_per_tick INTEGER NOT NULL CHECK (maximum_plans_per_tick BETWEEN 1 AND 100000),
    maximum_observations_per_tick INTEGER NOT NULL CHECK (maximum_observations_per_tick BETWEEN 1 AND 100000),
    plan_count BIGINT NOT NULL DEFAULT 0 CHECK (plan_count >= 0),
    consignment_count BIGINT NOT NULL DEFAULT 0 CHECK (consignment_count >= 0),
    awaiting_route_count BIGINT NOT NULL DEFAULT 0 CHECK (awaiting_route_count >= 0),
    in_transit_count BIGINT NOT NULL DEFAULT 0 CHECK (in_transit_count >= 0),
    awaiting_receipt_count BIGINT NOT NULL DEFAULT 0 CHECK (awaiting_receipt_count >= 0),
    received_count BIGINT NOT NULL DEFAULT 0 CHECK (received_count >= 0),
    expired_count BIGINT NOT NULL DEFAULT 0 CHECK (expired_count >= 0),
    voided_count BIGINT NOT NULL DEFAULT 0 CHECK (voided_count >= 0),
    orphaned_count BIGINT NOT NULL DEFAULT 0 CHECK (orphaned_count >= 0),
    fact_count BIGINT NOT NULL DEFAULT 0 CHECK (fact_count >= 0),
    transition_count BIGINT NOT NULL DEFAULT 0 CHECK (transition_count >= 0),
    receipt_count BIGINT NOT NULL DEFAULT 0 CHECK (receipt_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_freight_batch_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-freight-batches'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND source_contract = 'v16_suppressed_overflow_source_v1'
        AND packing_contract = 'stable_line_capacity_packing_v1'
        AND transport_contract = 'v9_freight_consignment_demand_v1'
        AND receipt_contract = 'all_consignment_arrivals_then_v15_atomic_delivery_v1'
        AND maximum_units = 32
        AND maximum_consignments_per_plan = 128
        AND maximum_plans_per_tick = 64
        AND maximum_observations_per_tick = 128
    ),
    CONSTRAINT city_open_world_freight_batch_profile_counter_check CHECK (
        consignment_count = awaiting_route_count + in_transit_count + awaiting_receipt_count
                            + received_count + expired_count + voided_count + orphaned_count
        AND receipt_count = received_count
        AND fact_count >= consignment_count
        AND fact_count >= transition_count
        AND transition_count >= consignment_count
    ),
    CONSTRAINT city_open_world_freight_batch_profile_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'scope' = 'v16_suppressed_overflow_to_v9_multi_consignment'
        AND metadata->>'inventory' = 'v15_atomic_delivery_only'
        AND metadata->>'legacy' = 'pre_v18_overflow_sources_untracked'
    )
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_batch_plans (
    id BIGSERIAL NOT NULL,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_freight_batch_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    overflow_source_code VARCHAR(160) NOT NULL,
    order_code VARCHAR(160) NOT NULL,
    seller_node_code VARCHAR(160) NOT NULL,
    buyer_node_code VARCHAR(160) NOT NULL,
    source_hub_code VARCHAR(160) NOT NULL,
    destination_hub_code VARCHAR(160) NOT NULL,
    carrier_actor_id BIGINT NOT NULL,
    source_tick BIGINT NOT NULL CHECK (source_tick > 0),
    required_units BIGINT NOT NULL CHECK (required_units > 32),
    consignment_count INTEGER NOT NULL CHECK (consignment_count BETWEEN 2 AND 128),
    state VARCHAR(24) NOT NULL,
    source_runtime_fact_id BIGINT NOT NULL,
    last_runtime_fact_id BIGINT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_freight_batch_plan_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_open_world_freight_batch_plan_source_unique UNIQUE (world_id, overflow_source_code),
    CONSTRAINT city_open_world_freight_batch_plan_order_unique UNIQUE (world_id, order_code),
    CONSTRAINT city_open_world_freight_batch_plan_source_fk
        FOREIGN KEY (world_id, overflow_source_code)
        REFERENCES city_open_world_enterprise_freight_sources(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_plan_order_fk
        FOREIGN KEY (world_id, order_code)
        REFERENCES city_open_world_supply_chain_orders(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_plan_seller_fk
        FOREIGN KEY (world_id, seller_node_code)
        REFERENCES city_open_world_supply_chain_nodes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_plan_buyer_fk
        FOREIGN KEY (world_id, buyer_node_code)
        REFERENCES city_open_world_supply_chain_nodes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_plan_source_hub_fk
        FOREIGN KEY (world_id, source_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_plan_destination_hub_fk
        FOREIGN KEY (world_id, destination_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_plan_carrier_fk
        FOREIGN KEY (carrier_actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_plan_source_runtime_fact_fk
        FOREIGN KEY (source_runtime_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_freight_batch_plan_last_runtime_fact_fk
        FOREIGN KEY (last_runtime_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_freight_batch_plan_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND overflow_source_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND order_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND seller_node_code <> buyer_node_code
        AND source_hub_code <> destination_hub_code
        AND state IN ('active','ready','received','blocked')
    ),
    CONSTRAINT city_open_world_freight_batch_plan_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_batch_consignments (
    id BIGSERIAL NOT NULL,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_freight_batch_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    plan_code VARCHAR(160) NOT NULL,
    batch_no INTEGER NOT NULL CHECK (batch_no > 0),
    requested_units BIGINT NOT NULL CHECK (requested_units BETWEEN 1 AND 32),
    state VARCHAR(24) NOT NULL,
    mobility_demand_id BIGINT NOT NULL,
    mobility_route_id BIGINT,
    source_runtime_fact_id BIGINT NOT NULL,
    last_runtime_fact_id BIGINT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_freight_batch_consignment_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_open_world_freight_batch_consignment_plan_batch_unique UNIQUE (world_id, plan_code, batch_no),
    CONSTRAINT city_open_world_freight_batch_consignment_plan_fk
        FOREIGN KEY (world_id, plan_code)
        REFERENCES city_open_world_freight_batch_plans(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_consignment_demand_fk
        FOREIGN KEY (mobility_demand_id, world_id)
        REFERENCES city_open_world_mobility_demands(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_freight_batch_consignment_route_fk
        FOREIGN KEY (mobility_route_id, world_id)
        REFERENCES city_open_world_mobility_routes(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_freight_batch_consignment_source_runtime_fact_fk
        FOREIGN KEY (source_runtime_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_freight_batch_consignment_last_runtime_fact_fk
        FOREIGN KEY (last_runtime_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_freight_batch_consignment_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND state IN ('awaiting_route','in_transit','awaiting_receipt','received','expired','voided','orphaned')
        AND ((state IN ('awaiting_route','expired','voided') AND mobility_route_id IS NULL)
             OR (state IN ('in_transit','awaiting_receipt','received','orphaned') AND mobility_route_id IS NOT NULL))
    ),
    CONSTRAINT city_open_world_freight_batch_consignment_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_batch_lines (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL,
    consignment_code VARCHAR(160) NOT NULL,
    source_line_no INTEGER NOT NULL CHECK (source_line_no > 0),
    resource_code VARCHAR(160) NOT NULL,
    quantity_units BIGINT NOT NULL CHECK (quantity_units > 0),
    unit_price_units BIGINT NOT NULL CHECK (unit_price_units > 0),
    total_price_units BIGINT NOT NULL CHECK (total_price_units > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_freight_batch_line_consignment_fk
        FOREIGN KEY (world_id, consignment_code)
        REFERENCES city_open_world_freight_batch_consignments(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_line_identity_check CHECK (
        resource_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND quantity_units::NUMERIC * unit_price_units::NUMERIC = total_price_units::NUMERIC
    ),
    CONSTRAINT city_open_world_freight_batch_line_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_freight_batch_line_unique UNIQUE (world_id, consignment_code, source_line_no)
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_batch_facts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_freight_batch_profiles(world_id) ON DELETE RESTRICT,
    consignment_code VARCHAR(160) NOT NULL,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    fact_type VARCHAR(64) NOT NULL,
    evidence_kind VARCHAR(24) NOT NULL,
    runtime_fact_id BIGINT,
    supply_chain_fact_id BIGINT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_freight_batch_fact_consignment_fk
        FOREIGN KEY (world_id, consignment_code)
        REFERENCES city_open_world_freight_batch_consignments(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_fact_runtime_fk
        FOREIGN KEY (runtime_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_fact_supply_fk
        FOREIGN KEY (supply_chain_fact_id, world_id)
        REFERENCES city_open_world_supply_chain_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_fact_identity_check CHECK (
        fact_type IN ('consignment.created','demand.requested','route.scheduled','route.completed',
                      'demand.expired','demand.voided','transport.orphaned','receipt.confirmed')
        AND evidence_kind IN ('runtime','supply_chain')
        AND ((evidence_kind = 'runtime' AND runtime_fact_id IS NOT NULL AND supply_chain_fact_id IS NULL)
             OR (evidence_kind = 'supply_chain' AND runtime_fact_id IS NULL AND supply_chain_fact_id IS NOT NULL))
    ),
    CONSTRAINT city_open_world_freight_batch_fact_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT city_open_world_freight_batch_fact_cursor_unique
        UNIQUE (world_id, consignment_code, evidence_kind, tick, sequence),
    CONSTRAINT city_open_world_freight_batch_fact_runtime_unique UNIQUE (world_id, runtime_fact_id)
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_batch_transitions (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_freight_batch_profiles(world_id) ON DELETE RESTRICT,
    consignment_code VARCHAR(160) NOT NULL,
    transition_tick BIGINT NOT NULL CHECK (transition_tick > 0),
    transition_sequence BIGINT NOT NULL CHECK (transition_sequence > 0),
    state VARCHAR(24) NOT NULL,
    reason_code VARCHAR(96) NOT NULL,
    source_fact_id BIGINT NOT NULL
        REFERENCES city_open_world_freight_batch_facts(id) ON DELETE RESTRICT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_freight_batch_transition_consignment_fk
        FOREIGN KEY (world_id, consignment_code)
        REFERENCES city_open_world_freight_batch_consignments(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_transition_identity_check CHECK (
        state IN ('awaiting_route','in_transit','awaiting_receipt','received','expired','voided','orphaned')
        AND reason_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
    ),
    CONSTRAINT city_open_world_freight_batch_transition_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_freight_batch_transition_cursor_unique
        UNIQUE (world_id, consignment_code, transition_tick, transition_sequence),
    CONSTRAINT city_open_world_freight_batch_transition_fact_unique UNIQUE (source_fact_id)
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_batch_receipts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_freight_batch_profiles(world_id) ON DELETE RESTRICT,
    consignment_code VARCHAR(160) NOT NULL,
    plan_code VARCHAR(160) NOT NULL,
    order_code VARCHAR(160) NOT NULL,
    received_tick BIGINT NOT NULL CHECK (received_tick > 0),
    supply_chain_delivery_id BIGINT NOT NULL,
    resource_operation_id BIGINT NOT NULL,
    source_fact_id BIGINT NOT NULL
        REFERENCES city_open_world_freight_batch_facts(id) ON DELETE RESTRICT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_freight_batch_receipt_consignment_fk
        FOREIGN KEY (world_id, consignment_code)
        REFERENCES city_open_world_freight_batch_consignments(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_receipt_plan_fk
        FOREIGN KEY (world_id, plan_code)
        REFERENCES city_open_world_freight_batch_plans(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_receipt_delivery_fk
        FOREIGN KEY (supply_chain_delivery_id)
        REFERENCES city_open_world_supply_chain_deliveries(id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_receipt_operation_fk
        FOREIGN KEY (resource_operation_id)
        REFERENCES city_resource_operations(id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_batch_receipt_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_freight_batch_receipt_consignment_unique UNIQUE (world_id, consignment_code)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_freight_batch_plans_state
    ON city_open_world_freight_batch_plans (world_id, state, source_tick, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_freight_batch_consignments_state
    ON city_open_world_freight_batch_consignments (world_id, state, plan_code, batch_no);
CREATE INDEX IF NOT EXISTS idx_city_open_world_freight_batch_facts_consignment
    ON city_open_world_freight_batch_facts (world_id, consignment_code, tick, sequence);

CREATE OR REPLACE FUNCTION city_open_world_freight_batch_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_freight_batch_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v18'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v18'
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_freight_batch_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_freight_batch_world_id', TRUE), '') = target_world_id::TEXT
       AND (city_open_world_runtime_fact_write_enabled(target_world_id)
            OR city_open_world_supply_chain_write_enabled(target_world_id))
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-v18'
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_freight_batch_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_freight_batch_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_freight_batch_write_enabled(target_world_id)
       AND NEW.revision = OLD.revision + 1
       AND (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.source_contract, OLD.packing_contract,
            OLD.transport_contract, OLD.receipt_contract, OLD.maximum_units,
            OLD.maximum_consignments_per_plan, OLD.maximum_plans_per_tick,
            OLD.maximum_observations_per_tick, OLD.metadata, OLD.created_at)
           IS NOT DISTINCT FROM
           (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.source_contract, NEW.packing_contract,
            NEW.transport_contract, NEW.receipt_contract, NEW.maximum_units,
            NEW.maximum_consignments_per_plan, NEW.maximum_plans_per_tick,
            NEW.maximum_observations_per_tick, NEW.metadata, NEW.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V18 freight-batch profile is immutable outside its audited projection write path'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_freight_batch_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    target_world_id BIGINT;
    row_data JSONB;
    plan_source_code VARCHAR(160);
    plan_order_code VARCHAR(160);
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' OR NOT city_open_world_freight_batch_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V18 freight-batch projections require their audited write context'
            USING ERRCODE = '55000';
    END IF;
    -- One guarded function serves rows with deliberately different schemas.
    -- JSONB access prevents PostgreSQL from resolving a non-existent column
    -- while compiling this trigger for a line or receipt table.
    row_data := to_jsonb(NEW);
    IF TG_TABLE_NAME = 'city_open_world_freight_batch_plans' THEN
        SELECT source.code, source.order_code
          INTO plan_source_code, plan_order_code
        FROM city_open_world_enterprise_freight_sources source
        JOIN city_open_world_freight_batch_profiles profile
          ON profile.world_id = source.world_id
        WHERE source.world_id = target_world_id
          AND source.code = row_data->>'overflow_source_code'
          AND source.state = 'suppressed'
          AND source.source_tick > profile.baseline_tick
          AND source.requested_units > profile.maximum_units;
        IF plan_source_code IS DISTINCT FROM row_data->>'overflow_source_code'
           OR plan_order_code IS DISTINCT FROM row_data->>'order_code' THEN
            RAISE EXCEPTION 'open-world V18 freight-batch plan must root in a post-baseline V16 overflow source'
                USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_open_world_freight_batch_consignments' THEN
        IF NOT EXISTS (
            SELECT 1
            FROM city_open_world_freight_batch_plans plan
            JOIN city_open_world_mobility_demands demand
              ON demand.id = (row_data->>'mobility_demand_id')::BIGINT AND demand.world_id = plan.world_id
            WHERE plan.world_id = target_world_id AND plan.code = row_data->>'plan_code'
              AND demand.actor_id = plan.carrier_actor_id
              AND demand.source_hub_code = plan.source_hub_code
              AND demand.destination_hub_code = plan.destination_hub_code
              AND demand.mode_code = 'freight'
              AND demand.purpose_code = 'enterprise.freight_batch'
              AND demand.requested_units = (row_data->>'requested_units')::BIGINT
              AND demand.metadata->'transport_adapter'->>'kind' = 'enterprise_freight_batch_v1'
              AND demand.metadata->'transport_adapter'->>'plan_code' = plan.code
              AND demand.metadata->'transport_adapter'->>'consignment_code' = row_data->>'code'
        ) THEN
            RAISE EXCEPTION 'open-world V18 consignment must own its capacity-bounded V9 demand'
                USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_open_world_freight_batch_facts' THEN
        IF row_data->>'evidence_kind' = 'runtime' AND NOT EXISTS (
            SELECT 1 FROM city_open_world_runtime_facts fact
            WHERE fact.id = (row_data->>'runtime_fact_id')::BIGINT AND fact.world_id = target_world_id
              AND fact.tick = (row_data->>'tick')::BIGINT AND fact.sequence = (row_data->>'sequence')::BIGINT
        ) THEN
            RAISE EXCEPTION 'open-world V18 runtime evidence must match its runtime fact cursor' USING ERRCODE = '23514';
        END IF;
        IF row_data->>'evidence_kind' = 'supply_chain' AND NOT EXISTS (
            SELECT 1 FROM city_open_world_supply_chain_facts fact
            WHERE fact.id = (row_data->>'supply_chain_fact_id')::BIGINT AND fact.world_id = target_world_id
              AND fact.tick = (row_data->>'tick')::BIGINT AND fact.sequence = (row_data->>'sequence')::BIGINT
              AND fact.fact_type = 'order.delivered'
        ) THEN
            RAISE EXCEPTION 'open-world V18 receipt evidence must match its V15 delivery cursor' USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_open_world_freight_batch_transitions' AND NOT EXISTS (
        SELECT 1 FROM city_open_world_freight_batch_facts fact
        WHERE fact.id = (row_data->>'source_fact_id')::BIGINT AND fact.world_id = target_world_id
          AND fact.consignment_code = row_data->>'consignment_code'
          AND fact.tick = (row_data->>'transition_tick')::BIGINT
          AND fact.sequence = (row_data->>'transition_sequence')::BIGINT
    ) THEN
        RAISE EXCEPTION 'open-world V18 consignment transition must reference its batch fact cursor'
            USING ERRCODE = '23514';
    ELSIF TG_TABLE_NAME = 'city_open_world_freight_batch_receipts' THEN
        IF NOT EXISTS (
            SELECT 1
            FROM city_open_world_freight_batch_consignments consignment
            JOIN city_open_world_freight_batch_plans plan
              ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
            JOIN city_open_world_freight_batch_facts fact
              ON fact.id = (row_data->>'source_fact_id')::BIGINT AND fact.world_id = consignment.world_id
            JOIN city_open_world_supply_chain_deliveries delivery
              ON delivery.id = (row_data->>'supply_chain_delivery_id')::BIGINT AND delivery.world_id = consignment.world_id
            WHERE consignment.world_id = target_world_id
              AND consignment.code = row_data->>'consignment_code'
              AND plan.code = row_data->>'plan_code'
              AND plan.order_code = row_data->>'order_code'
              AND delivery.order_code = plan.order_code
              AND fact.consignment_code = consignment.code
              AND fact.fact_type = 'receipt.confirmed'
              AND fact.evidence_kind = 'supply_chain'
        ) THEN
            RAISE EXCEPTION 'open-world V18 receipt must bind one batch consignment to its V15 delivery'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_freight_batch_profile_guard ON city_open_world_freight_batch_profiles;
CREATE TRIGGER city_open_world_freight_batch_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_freight_batch_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_freight_batch_profile();

DO $$
DECLARE table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_open_world_freight_batch_plans',
        'city_open_world_freight_batch_consignments',
        'city_open_world_freight_batch_lines',
        'city_open_world_freight_batch_facts',
        'city_open_world_freight_batch_transitions',
        'city_open_world_freight_batch_receipts'
    ] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', table_name || '_guard', table_name);
        EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_freight_batch_projection()', table_name || '_guard', table_name);
    END LOOP;
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
          AND world.simulation_version IN ('city-openworld-v15','city-openworld-v16','city-openworld-v17','city-openworld-v18')
          AND operation.operation_type = 'transfer'
          AND command.command_type = 'open_world.supply_order.deliver'
          AND operation.actor_entity_id = seller.firm_entity_id
          AND operation.district_id = seller.district_id
          AND (
              world.simulation_version IN ('city-openworld-v15','city-openworld-v16')
              OR (
                  world.simulation_version IN ('city-openworld-v17','city-openworld-v18')
                  AND NOT EXISTS (
                      SELECT 1
                      FROM city_open_world_enterprise_freight_sources source
                      JOIN city_open_world_enterprise_freight_receipt_profiles profile
                        ON profile.world_id = source.world_id
                      WHERE source.world_id = operation.world_id
                        AND source.order_code = fact.order_code
                        AND source.source_tick > profile.baseline_tick
                        AND source.state <> 'suppressed'
                  )
                  OR EXISTS (
                      SELECT 1
                      FROM city_open_world_enterprise_freight_shipments shipment
                      JOIN city_open_world_enterprise_freight_receipt_profiles profile
                        ON profile.world_id = shipment.world_id
                      JOIN city_open_world_enterprise_freight_sources source
                        ON source.world_id = shipment.world_id AND source.code = shipment.freight_source_code
                      WHERE shipment.world_id = operation.world_id
                        AND shipment.order_code = fact.order_code
                        AND shipment.state = 'awaiting_receipt'
                        AND source.source_tick > profile.baseline_tick
                        AND source.state <> 'suppressed'
                  )
              )
          )
          AND (
              world.simulation_version <> 'city-openworld-v18'
              OR NOT EXISTS (
                  SELECT 1
                  FROM city_open_world_enterprise_freight_sources source
                  JOIN city_open_world_freight_batch_profiles profile
                    ON profile.world_id = source.world_id
                  WHERE source.world_id = operation.world_id
                    AND source.order_code = fact.order_code
                    AND source.state = 'suppressed'
                    AND source.source_tick > profile.baseline_tick
              )
              OR EXISTS (
                  SELECT 1
                  FROM city_open_world_freight_batch_plans plan
                  JOIN city_open_world_enterprise_freight_sources source
                    ON source.world_id = plan.world_id AND source.code = plan.overflow_source_code
                  WHERE plan.world_id = operation.world_id
                    AND plan.order_code = fact.order_code
                    AND plan.state = 'ready'
                    AND source.state = 'suppressed'
                    AND NOT EXISTS (
                        SELECT 1
                        FROM city_open_world_freight_batch_consignments consignment
                        WHERE consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
                          AND consignment.state <> 'awaiting_receipt'
                    )
              )
          )
    )
$$;

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
        SELECT 1 FROM (VALUES ('content_catalog'), ('economic_policy'), ('engine'),
                              ('rule_bundle'), ('scenario'), ('spatial_profile'), ('worldgen_plan')) AS required(component_code)
        WHERE NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding
                          WHERE binding.world_id = target_world_id
                            AND binding.generation = vector_generation
                            AND binding.component_code = required.component_code)
    ) THEN
        RAISE EXCEPTION 'city open-world version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v14' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-commute-lifecycle-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V14 commute lifecycle version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v15' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-supply-chain-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V15 supply-chain version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v16' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-enterprise-freight-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V16 enterprise-freight version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v17' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-enterprise-freight-receipt-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V17 freight-receipt version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v18' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-freight-batch-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V18 freight-batch version vector is incomplete' USING ERRCODE = '23514';
    END IF;
END;
$$;

-- Every predecessor projection remains active in V18. Rather than copying a
-- growing set of version-gate bodies, recompile their audited V17 definitions
-- with V18 appended to the terminal allow-list. The exact replacement is
-- deliberately checked so a future predecessor change cannot silently omit a
-- write gate for the new engine.
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
        definition := replace(
            definition,
            $needle$'city-openworld-v17')$needle$,
            $replacement$'city-openworld-v17','city-openworld-v18')$replacement$
        );
        IF position($replacement$city-openworld-v18$replacement$ IN definition) = 0 THEN
            RAISE EXCEPTION 'cannot extend V18 predecessor write gate %', target_function USING ERRCODE = '23514';
        END IF;
        EXECUTE definition;
    END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION city_open_world_enterprise_freight_receipt_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_enterprise_freight_receipt_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v17','city-openworld-v18')
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

CREATE OR REPLACE FUNCTION city_open_world_enterprise_freight_receipt_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_enterprise_freight_receipt_world_id', TRUE), '') = target_world_id::TEXT
       AND (city_open_world_runtime_fact_write_enabled(target_world_id)
            OR city_open_world_supply_chain_write_enabled(target_world_id))
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v17','city-openworld-v18'))
$$;

-- V17's deferred validator remains meaningful on V18 because it deliberately
-- ignores V16 sources in state=suppressed; those are validated by V18 below.
DO $$
DECLARE definition TEXT;
BEGIN
    definition := pg_get_functiondef('assert_city_open_world_enterprise_freight_receipt_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$IF world_version <> 'city-openworld-v17' THEN RETURN; END IF;$old$,
        $new$IF world_version NOT IN ('city-openworld-v17','city-openworld-v18') THEN RETURN; END IF;$new$
    );
    IF position($new$city-openworld-v18$new$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V17 receipt foundation to V18' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_freight_batch_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version VARCHAR(32); world_tick BIGINT; profile_tick BIGINT;
    plans BIGINT; consignments BIGINT; awaiting_route BIGINT; in_transit BIGINT;
    awaiting_receipt BIGINT; received BIGINT; expired BIGINT; voided BIGINT;
    orphaned BIGINT; facts BIGINT; transitions BIGINT; receipts BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v18' THEN RETURN; END IF;
    IF NOT EXISTS (SELECT 1 FROM city_open_world_supply_chain_profiles WHERE world_id = target_world_id)
       OR NOT EXISTS (SELECT 1 FROM city_open_world_enterprise_freight_profiles WHERE world_id = target_world_id)
       OR NOT EXISTS (SELECT 1 FROM city_open_world_enterprise_freight_receipt_profiles WHERE world_id = target_world_id)
       OR NOT EXISTS (SELECT 1 FROM city_open_world_mobility_profiles WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V18 freight-batch predecessor foundations are missing' USING ERRCODE = '23514';
    END IF;
    SELECT baseline_tick, plan_count, consignment_count, awaiting_route_count,
           in_transit_count, awaiting_receipt_count, received_count, expired_count,
           voided_count, orphaned_count, fact_count, transition_count, receipt_count
      INTO profile_tick, plans, consignments, awaiting_route, in_transit,
           awaiting_receipt, received, expired, voided, orphaned, facts,
           transitions, receipts
    FROM city_open_world_freight_batch_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V18 freight-batch profile is missing or invalid' USING ERRCODE = '23514';
    END IF;
    IF plans <> (SELECT COUNT(*) FROM city_open_world_freight_batch_plans WHERE world_id = target_world_id)
       OR consignments <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id)
       OR awaiting_route <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'awaiting_route')
       OR in_transit <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'in_transit')
       OR awaiting_receipt <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'awaiting_receipt')
       OR received <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'received')
       OR expired <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'expired')
       OR voided <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'voided')
       OR orphaned <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'orphaned')
       OR facts <> (SELECT COUNT(*) FROM city_open_world_freight_batch_facts WHERE world_id = target_world_id)
       OR transitions <> (SELECT COUNT(*) FROM city_open_world_freight_batch_transitions WHERE world_id = target_world_id)
       OR receipts <> (SELECT COUNT(*) FROM city_open_world_freight_batch_receipts WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V18 freight-batch counters are inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_plans plan
        JOIN city_open_world_enterprise_freight_sources source
          ON source.world_id = plan.world_id AND source.code = plan.overflow_source_code
        JOIN city_open_world_actors carrier
          ON carrier.id = plan.carrier_actor_id AND carrier.world_id = plan.world_id
        JOIN city_open_world_runtime_facts source_fact
          ON source_fact.id = plan.source_runtime_fact_id AND source_fact.world_id = plan.world_id
        JOIN city_open_world_runtime_facts last_fact
          ON last_fact.id = plan.last_runtime_fact_id AND last_fact.world_id = plan.world_id
        WHERE plan.world_id = target_world_id
          AND (source.state <> 'suppressed'
               OR source.source_tick <= profile_tick
               OR source.requested_units <> plan.required_units
               OR source.requested_units <= 32
               OR source.order_code <> plan.order_code
               OR source.seller_node_code <> plan.seller_node_code
               OR source.buyer_node_code <> plan.buyer_node_code
               OR source.source_hub_code <> plan.source_hub_code
               OR source.destination_hub_code <> plan.destination_hub_code
               OR source.carrier_actor_id <> plan.carrier_actor_id
               OR source.source_tick <> plan.source_tick
               OR source.source_runtime_fact_id <> plan.source_runtime_fact_id
               OR carrier.code <> 'system.freight.carrier'
               OR source_fact.posted_at IS NULL OR last_fact.posted_at IS NULL)
    ) THEN
        RAISE EXCEPTION 'city open-world V18 freight-batch plan identity is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_plans plan
        WHERE plan.world_id = target_world_id
          AND plan.consignment_count <> (
              SELECT COUNT(*) FROM city_open_world_freight_batch_consignments consignment
              WHERE consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
          )
    ) OR EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_plans plan
        JOIN city_open_world_freight_batch_consignments consignment
          ON consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
        WHERE plan.world_id = target_world_id
        GROUP BY plan.world_id, plan.code, plan.required_units
        HAVING SUM(consignment.requested_units) <> MAX(plan.required_units)
    ) THEN
        RAISE EXCEPTION 'city open-world V18 freight-batch plan quantity is inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_consignments consignment
        JOIN city_open_world_freight_batch_plans plan
          ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
        JOIN city_open_world_mobility_demands demand
          ON demand.id = consignment.mobility_demand_id AND demand.world_id = consignment.world_id
        LEFT JOIN city_open_world_mobility_routes route
          ON route.id = consignment.mobility_route_id AND route.world_id = consignment.world_id
        JOIN city_open_world_runtime_facts source_fact
          ON source_fact.id = consignment.source_runtime_fact_id AND source_fact.world_id = consignment.world_id
        JOIN city_open_world_runtime_facts last_fact
          ON last_fact.id = consignment.last_runtime_fact_id AND last_fact.world_id = consignment.world_id
        WHERE consignment.world_id = target_world_id
          AND (demand.actor_id <> plan.carrier_actor_id
               OR demand.source_hub_code <> plan.source_hub_code
               OR demand.destination_hub_code <> plan.destination_hub_code
               OR demand.mode_code <> 'freight'
               OR demand.purpose_code <> 'enterprise.freight_batch'
               OR demand.requested_units <> consignment.requested_units
               OR demand.metadata->'transport_adapter'->>'kind' <> 'enterprise_freight_batch_v1'
               OR demand.metadata->'transport_adapter'->>'plan_code' <> plan.code
               OR demand.metadata->'transport_adapter'->>'consignment_code' <> consignment.code
               OR source_fact.posted_at IS NULL OR last_fact.posted_at IS NULL
               OR (consignment.state IN ('in_transit','awaiting_receipt','received','orphaned') AND route.id IS NULL)
               OR (consignment.state IN ('awaiting_route','expired','voided') AND route.id IS NOT NULL))
    ) THEN
        RAISE EXCEPTION 'city open-world V18 consignment transport linkage is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_consignments consignment
        LEFT JOIN city_open_world_freight_batch_lines line
          ON line.world_id = consignment.world_id AND line.consignment_code = consignment.code
        WHERE consignment.world_id = target_world_id
        GROUP BY consignment.world_id, consignment.code, consignment.requested_units
        HAVING COUNT(line.id) = 0 OR COALESCE(SUM(line.quantity_units), 0) <> MAX(consignment.requested_units)
    ) OR EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_lines line
        JOIN city_open_world_freight_batch_consignments consignment
          ON consignment.world_id = line.world_id AND consignment.code = line.consignment_code
        JOIN city_open_world_freight_batch_plans plan
          ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
        LEFT JOIN city_open_world_enterprise_freight_source_lines source_line
          ON source_line.world_id = plan.world_id AND source_line.source_code = plan.overflow_source_code
         AND source_line.line_no = line.source_line_no
        WHERE line.world_id = target_world_id
          AND (source_line.id IS NULL OR source_line.resource_code <> line.resource_code
               OR source_line.unit_price_units <> line.unit_price_units)
    ) OR EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_source_lines source_line
        JOIN city_open_world_freight_batch_plans plan
          ON plan.world_id = source_line.world_id AND plan.overflow_source_code = source_line.source_code
        LEFT JOIN city_open_world_freight_batch_consignments consignment
          ON consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
        LEFT JOIN city_open_world_freight_batch_lines line
          ON line.world_id = consignment.world_id AND line.consignment_code = consignment.code
         AND line.source_line_no = source_line.line_no
        WHERE source_line.world_id = target_world_id
        GROUP BY source_line.world_id, source_line.source_code, source_line.line_no, source_line.quantity_units
        HAVING COALESCE(SUM(line.quantity_units), 0) <> MAX(source_line.quantity_units)
    ) THEN
        RAISE EXCEPTION 'city open-world V18 consignment line packing is not conservative' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_facts fact
        JOIN city_open_world_freight_batch_consignments consignment
          ON consignment.world_id = fact.world_id AND consignment.code = fact.consignment_code
        JOIN city_open_world_freight_batch_plans plan
          ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
        LEFT JOIN city_open_world_runtime_facts runtime_fact
          ON runtime_fact.id = fact.runtime_fact_id AND runtime_fact.world_id = fact.world_id
        LEFT JOIN city_open_world_supply_chain_facts supply_fact
          ON supply_fact.id = fact.supply_chain_fact_id AND supply_fact.world_id = fact.world_id
        WHERE fact.world_id = target_world_id
          AND ((fact.evidence_kind = 'runtime' AND
                (runtime_fact.id IS NULL OR runtime_fact.tick <> fact.tick OR runtime_fact.sequence <> fact.sequence
                 OR runtime_fact.posted_at IS NULL
                 OR (fact.fact_type = 'consignment.created' AND runtime_fact.fact_type <> 'system.enterprise_freight_batch.consignment.created')
                 OR (fact.fact_type = 'demand.requested' AND runtime_fact.fact_type <> 'mobility.requested')
                 OR (fact.fact_type = 'route.scheduled' AND runtime_fact.fact_type <> 'system.enterprise_freight_batch.route.scheduled')
                 OR (fact.fact_type = 'route.completed' AND runtime_fact.fact_type <> 'system.enterprise_freight_batch.route.completed')
                 OR (fact.fact_type = 'demand.expired' AND runtime_fact.fact_type <> 'system.enterprise_freight_batch.demand.expired')
                 OR (fact.fact_type = 'demand.voided' AND runtime_fact.fact_type <> 'mobility.expired')
                 OR (fact.fact_type = 'transport.orphaned' AND runtime_fact.fact_type <> 'system.enterprise_freight_batch.transport.orphaned')))
               OR (fact.evidence_kind = 'supply_chain' AND
                   (fact.fact_type <> 'receipt.confirmed' OR supply_fact.id IS NULL
                    OR supply_fact.fact_type <> 'order.delivered' OR supply_fact.order_code <> plan.order_code
                    OR supply_fact.tick <> fact.tick OR supply_fact.sequence <> fact.sequence)))
    ) THEN
        RAISE EXCEPTION 'city open-world V18 freight-batch evidence is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_transitions transition
        JOIN city_open_world_freight_batch_facts fact
          ON fact.id = transition.source_fact_id AND fact.world_id = transition.world_id
        WHERE transition.world_id = target_world_id
          AND (fact.consignment_code <> transition.consignment_code
               OR fact.tick <> transition.transition_tick OR fact.sequence <> transition.transition_sequence
               OR (transition.state = 'awaiting_route' AND fact.fact_type <> 'demand.requested')
               OR (transition.state = 'in_transit' AND fact.fact_type <> 'route.scheduled')
               OR (transition.state = 'awaiting_receipt' AND fact.fact_type <> 'route.completed')
               OR (transition.state = 'received' AND fact.fact_type <> 'receipt.confirmed')
               OR (transition.state = 'expired' AND fact.fact_type <> 'demand.expired')
               OR (transition.state = 'voided' AND fact.fact_type <> 'demand.voided')
               OR (transition.state = 'orphaned' AND fact.fact_type <> 'transport.orphaned'))
    ) OR EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_consignments consignment
        WHERE consignment.world_id = target_world_id
          AND consignment.version <> (
              SELECT COUNT(*) FROM city_open_world_freight_batch_transitions transition
              WHERE transition.world_id = consignment.world_id AND transition.consignment_code = consignment.code
          )
    ) OR EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_consignments consignment
        WHERE consignment.world_id = target_world_id
          AND consignment.state IS DISTINCT FROM (
              SELECT transition.state FROM city_open_world_freight_batch_transitions transition
              WHERE transition.world_id = consignment.world_id AND transition.consignment_code = consignment.code
              ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC LIMIT 1
          )
    ) THEN
        RAISE EXCEPTION 'city open-world V18 consignment state transition is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_receipts receipt
        JOIN city_open_world_freight_batch_consignments consignment
          ON consignment.world_id = receipt.world_id AND consignment.code = receipt.consignment_code
        JOIN city_open_world_freight_batch_plans plan
          ON plan.world_id = receipt.world_id AND plan.code = receipt.plan_code
        JOIN city_open_world_supply_chain_deliveries delivery
          ON delivery.id = receipt.supply_chain_delivery_id AND delivery.world_id = receipt.world_id
        JOIN city_open_world_supply_chain_facts delivery_fact
          ON delivery_fact.id = delivery.source_fact_id AND delivery_fact.world_id = receipt.world_id
        JOIN city_resource_operations operation
          ON operation.id = receipt.resource_operation_id AND operation.world_id = receipt.world_id
        JOIN city_open_world_freight_batch_facts fact
          ON fact.id = receipt.source_fact_id AND fact.world_id = receipt.world_id
        WHERE receipt.world_id = target_world_id
          AND (receipt.plan_code <> consignment.plan_code OR receipt.order_code <> plan.order_code
               OR receipt.order_code <> delivery.order_code OR receipt.received_tick <> delivery.delivered_tick
               OR receipt.received_tick <> delivery_fact.tick OR delivery.resource_operation_id <> operation.id
               OR delivery_fact.fact_type <> 'order.delivered' OR fact.fact_type <> 'receipt.confirmed'
               OR fact.supply_chain_fact_id <> delivery_fact.id OR consignment.state <> 'received')
    ) OR EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_consignments consignment
        WHERE consignment.world_id = target_world_id
          AND ((consignment.state = 'received') <> EXISTS (
              SELECT 1 FROM city_open_world_freight_batch_receipts receipt
              WHERE receipt.world_id = consignment.world_id AND receipt.consignment_code = consignment.code))
    ) THEN
        RAISE EXCEPTION 'city open-world V18 freight-batch receipt projection is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_plans plan
        WHERE plan.world_id = target_world_id
          AND plan.state IS DISTINCT FROM CASE
              WHEN EXISTS (
                  SELECT 1 FROM city_open_world_freight_batch_consignments consignment
                  WHERE consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
                    AND consignment.state IN ('expired','voided','orphaned')
              ) THEN 'blocked'
              WHEN NOT EXISTS (
                  SELECT 1 FROM city_open_world_freight_batch_consignments consignment
                  WHERE consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
                    AND consignment.state <> 'received'
              ) THEN 'received'
              WHEN NOT EXISTS (
                  SELECT 1 FROM city_open_world_freight_batch_consignments consignment
                  WHERE consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
                    AND consignment.state <> 'awaiting_receipt'
              ) THEN 'ready'
              ELSE 'active'
          END
    ) THEN
        RAISE EXCEPTION 'city open-world V18 freight-batch plan state is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_supply_chain_deliveries delivery
        JOIN city_open_world_enterprise_freight_sources source
          ON source.world_id = delivery.world_id AND source.order_code = delivery.order_code
        JOIN city_open_world_freight_batch_profiles profile
          ON profile.world_id = delivery.world_id
        LEFT JOIN city_open_world_freight_batch_plans plan
          ON plan.world_id = source.world_id AND plan.overflow_source_code = source.code
        WHERE delivery.world_id = target_world_id
          AND source.state = 'suppressed' AND source.source_tick > profile.baseline_tick
          AND (plan.code IS NULL OR plan.state <> 'received' OR EXISTS (
              SELECT 1 FROM city_open_world_freight_batch_consignments consignment
              LEFT JOIN city_open_world_freight_batch_receipts receipt
                ON receipt.world_id = consignment.world_id AND receipt.consignment_code = consignment.code
              WHERE consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
                AND (consignment.state <> 'received' OR receipt.supply_chain_delivery_id <> delivery.id)
          ))
    ) THEN
        RAISE EXCEPTION 'city open-world V18 delivery lacks complete batch receipts' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_open_world_freight_batch_foundation_commit()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        RETURN COALESCE(NEW, OLD);
    END IF;
    PERFORM assert_city_open_world_freight_batch_foundation(target_world_id);
    RETURN COALESCE(NEW, OLD);
END;
$$;

DROP TRIGGER IF EXISTS v18_freight_batch_profile_commit ON city_open_world_freight_batch_profiles;
CREATE CONSTRAINT TRIGGER v18_freight_batch_profile_commit
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_freight_batch_profiles
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_city_open_world_freight_batch_foundation_commit();

DO $$
DECLARE table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_open_world_freight_batch_plans',
        'city_open_world_freight_batch_consignments',
        'city_open_world_freight_batch_lines',
        'city_open_world_freight_batch_facts',
        'city_open_world_freight_batch_transitions',
        'city_open_world_freight_batch_receipts'
    ] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', 'v18_freight_batch_' || table_name || '_commit', table_name);
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_city_open_world_freight_batch_foundation_commit()', 'v18_freight_batch_' || table_name || '_commit', table_name);
    END LOOP;
END;
$$;
