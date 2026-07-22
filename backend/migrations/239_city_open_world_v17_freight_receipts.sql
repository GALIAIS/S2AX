-- city-openworld-v17 / F10.1 adds an auditable custody projection over V16
-- freight evidence. It does not introduce a second inventory balance: V15
-- remains the single owner of reservations and the atomic delivery transfer.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v17', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","version_vector","facilities","npc_lod","social_service_catalog","social_service_access","social_service_queue","social_service_response","cross_domain_impact_bridge","aggregate_mobility_graph","mobility_capacity_allocation","mobility_route_lifecycle","mobility_arrival_bridge","cross_scale_spatial_transfer","versioned_od_sources","automatic_od_demands","mobility_cycle_metrics","residence_employment_bindings","verified_commute_sources","facility_presence_origin_validation","dual_direction_commutes","commute_assignment_epochs","commute_lifecycle_transitions","enterprise_supply_chain_nodes","supply_orders","inventory_reservations","purchase_settlement","fact_backed_dispatch","atomic_delivery","enterprise_freight_source_adapter","enterprise_freight_transport_observation","enterprise_freight_custody","enterprise_freight_receipts","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v16', 'city-openworld-v17', 'openworld_v16_to_v17')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_enterprise_freight_receipt_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_enterprise_freight_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    shipment_contract VARCHAR(96) NOT NULL,
    receipt_contract VARCHAR(96) NOT NULL,
    legacy_contract VARCHAR(96) NOT NULL,
    maximum_shipments INTEGER NOT NULL CHECK (maximum_shipments BETWEEN 1 AND 100000),
    maximum_observations_per_tick INTEGER NOT NULL CHECK (maximum_observations_per_tick BETWEEN 1 AND 100000),
    shipment_count BIGINT NOT NULL DEFAULT 0 CHECK (shipment_count >= 0),
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
    CONSTRAINT city_open_world_enterprise_freight_receipt_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-enterprise-freight-receipts'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND shipment_contract = 'v16_source_custody_snapshot_v1'
        AND receipt_contract = 'v15_atomic_delivery_receipt_gate_v1'
        AND legacy_contract = 'pre_v17_source_legacy_delivery_v1'
        AND maximum_shipments = 10000
        AND maximum_observations_per_tick = 128
    ),
    CONSTRAINT city_open_world_enterprise_freight_receipt_profile_counter_check CHECK (
        shipment_count = awaiting_route_count + in_transit_count + awaiting_receipt_count
                         + received_count + expired_count + voided_count + orphaned_count
        AND shipment_count <= maximum_shipments
        AND receipt_count = received_count
        -- A shipment is rooted by source.created before the first observed
        -- transport transition is posted, so a fresh awaiting-route shipment
        -- legitimately has zero transitions.  Facts, rather than transitions,
        -- are the non-empty custody floor.
        AND fact_count >= shipment_count
        AND fact_count >= transition_count
    ),
    CONSTRAINT city_open_world_enterprise_freight_receipt_profile_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'scope' = 'v16_transport_custody_and_v15_receipt_gate'
        AND metadata->>'inventory' = 'v15_only_until_delivery'
        AND metadata->>'legacy' = 'pre_v17_sources_untracked'
    )
);

CREATE TABLE IF NOT EXISTS city_open_world_enterprise_freight_shipments (
    id BIGSERIAL NOT NULL,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_enterprise_freight_receipt_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    freight_source_code VARCHAR(160) NOT NULL,
    order_code VARCHAR(160) NOT NULL,
    seller_node_code VARCHAR(160) NOT NULL,
    buyer_node_code VARCHAR(160) NOT NULL,
    source_hub_code VARCHAR(160) NOT NULL,
    destination_hub_code VARCHAR(160) NOT NULL,
    source_tick BIGINT NOT NULL CHECK (source_tick > 0),
    requested_units BIGINT NOT NULL CHECK (requested_units > 0),
    state VARCHAR(24) NOT NULL,
    source_freight_fact_id BIGINT NOT NULL
        REFERENCES city_open_world_enterprise_freight_facts(id) ON DELETE RESTRICT,
    last_receipt_fact_id BIGINT,
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_enterprise_freight_shipment_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_open_world_enterprise_freight_shipment_source_unique UNIQUE (world_id, freight_source_code),
    CONSTRAINT city_open_world_enterprise_freight_shipment_order_unique UNIQUE (world_id, order_code),
    CONSTRAINT city_open_world_enterprise_freight_shipment_seller_node_fk
        FOREIGN KEY (world_id, seller_node_code)
        REFERENCES city_open_world_supply_chain_nodes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_shipment_buyer_node_fk
        FOREIGN KEY (world_id, buyer_node_code)
        REFERENCES city_open_world_supply_chain_nodes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_shipment_source_hub_fk
        FOREIGN KEY (world_id, source_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_shipment_destination_hub_fk
        FOREIGN KEY (world_id, destination_hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_shipment_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND freight_source_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND order_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND seller_node_code <> buyer_node_code
        AND source_hub_code <> destination_hub_code
        AND state IN ('awaiting_route','in_transit','awaiting_receipt','received','expired','voided','orphaned')
    ),
    CONSTRAINT city_open_world_enterprise_freight_shipment_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_enterprise_freight_shipment_lines (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL,
    shipment_code VARCHAR(160) NOT NULL,
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
    CONSTRAINT city_open_world_enterprise_freight_shipment_line_shipment_fk
        FOREIGN KEY (world_id, shipment_code)
        REFERENCES city_open_world_enterprise_freight_shipments(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_shipment_line_identity_check CHECK (
        resource_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND source_firm_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND source_district_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND destination_firm_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND destination_district_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND quantity_units::NUMERIC * unit_price_units::NUMERIC = total_price_units::NUMERIC
    ),
    CONSTRAINT city_open_world_enterprise_freight_shipment_line_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_enterprise_freight_shipment_line_unique UNIQUE (world_id, shipment_code, line_no)
);

CREATE TABLE IF NOT EXISTS city_open_world_enterprise_freight_receipt_facts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_enterprise_freight_receipt_profiles(world_id) ON DELETE RESTRICT,
    shipment_code VARCHAR(160) NOT NULL,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    fact_type VARCHAR(64) NOT NULL,
    evidence_kind VARCHAR(24) NOT NULL,
    freight_fact_id BIGINT
        REFERENCES city_open_world_enterprise_freight_facts(id) ON DELETE RESTRICT,
    supply_chain_fact_id BIGINT
        REFERENCES city_open_world_supply_chain_facts(id) ON DELETE RESTRICT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_enterprise_freight_receipt_fact_shipment_fk
        FOREIGN KEY (world_id, shipment_code)
        REFERENCES city_open_world_enterprise_freight_shipments(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_receipt_fact_identity_check CHECK (
        fact_type IN ('shipment.created','route.awaiting','transport.in_transit','transport.arrived',
                      'transport.expired','transport.voided','transport.orphaned','receipt.confirmed')
        AND evidence_kind IN ('enterprise_freight','supply_chain')
        AND ((evidence_kind = 'enterprise_freight' AND freight_fact_id IS NOT NULL AND supply_chain_fact_id IS NULL)
             OR (evidence_kind = 'supply_chain' AND freight_fact_id IS NULL AND supply_chain_fact_id IS NOT NULL))
    ),
    CONSTRAINT city_open_world_enterprise_freight_receipt_fact_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT city_open_world_enterprise_freight_receipt_fact_cursor_unique
        UNIQUE (world_id, shipment_code, evidence_kind, tick, sequence),
    CONSTRAINT city_open_world_enterprise_freight_receipt_fact_freight_unique UNIQUE (freight_fact_id),
    CONSTRAINT city_open_world_enterprise_freight_receipt_fact_supply_unique UNIQUE (supply_chain_fact_id)
);

ALTER TABLE city_open_world_enterprise_freight_shipments
    DROP CONSTRAINT IF EXISTS city_open_world_enterprise_freight_shipment_last_fact_fk;
ALTER TABLE city_open_world_enterprise_freight_shipments
    ADD CONSTRAINT city_open_world_enterprise_freight_shipment_last_fact_fk
        FOREIGN KEY (last_receipt_fact_id)
        REFERENCES city_open_world_enterprise_freight_receipt_facts(id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE IF NOT EXISTS city_open_world_enterprise_freight_shipment_transitions (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_enterprise_freight_receipt_profiles(world_id) ON DELETE RESTRICT,
    shipment_code VARCHAR(160) NOT NULL,
    transition_tick BIGINT NOT NULL CHECK (transition_tick > 0),
    transition_sequence BIGINT NOT NULL CHECK (transition_sequence > 0),
    state VARCHAR(24) NOT NULL,
    reason_code VARCHAR(96) NOT NULL,
    source_fact_id BIGINT NOT NULL
        REFERENCES city_open_world_enterprise_freight_receipt_facts(id) ON DELETE RESTRICT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_enterprise_freight_shipment_transition_shipment_fk
        FOREIGN KEY (world_id, shipment_code)
        REFERENCES city_open_world_enterprise_freight_shipments(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_shipment_transition_identity_check CHECK (
        state IN ('awaiting_route','in_transit','awaiting_receipt','received','expired','voided','orphaned')
        AND reason_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
    ),
    CONSTRAINT city_open_world_enterprise_freight_shipment_transition_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_enterprise_freight_shipment_transition_cursor_unique
        UNIQUE (world_id, shipment_code, transition_tick, transition_sequence),
    CONSTRAINT city_open_world_enterprise_freight_shipment_transition_fact_unique UNIQUE (source_fact_id)
);

CREATE TABLE IF NOT EXISTS city_open_world_enterprise_freight_receipts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_enterprise_freight_receipt_profiles(world_id) ON DELETE RESTRICT,
    shipment_code VARCHAR(160) NOT NULL,
    order_code VARCHAR(160) NOT NULL,
    received_tick BIGINT NOT NULL CHECK (received_tick > 0),
    supply_chain_delivery_id BIGINT NOT NULL
        REFERENCES city_open_world_supply_chain_deliveries(id) ON DELETE RESTRICT,
    resource_operation_id BIGINT NOT NULL
        REFERENCES city_resource_operations(id) ON DELETE RESTRICT,
    source_fact_id BIGINT NOT NULL
        REFERENCES city_open_world_enterprise_freight_receipt_facts(id) ON DELETE RESTRICT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_enterprise_freight_receipt_shipment_fk
        FOREIGN KEY (world_id, shipment_code)
        REFERENCES city_open_world_enterprise_freight_shipments(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_enterprise_freight_receipt_identity_check CHECK (
        order_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
    ),
    CONSTRAINT city_open_world_enterprise_freight_receipt_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_enterprise_freight_receipt_shipment_unique UNIQUE (world_id, shipment_code),
    CONSTRAINT city_open_world_enterprise_freight_receipt_order_unique UNIQUE (world_id, order_code),
    CONSTRAINT city_open_world_enterprise_freight_receipt_delivery_unique UNIQUE (supply_chain_delivery_id),
    CONSTRAINT city_open_world_enterprise_freight_receipt_operation_unique UNIQUE (resource_operation_id),
    CONSTRAINT city_open_world_enterprise_freight_receipt_fact_unique UNIQUE (source_fact_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_enterprise_freight_shipments_state
    ON city_open_world_enterprise_freight_shipments (world_id, state, source_tick, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_enterprise_freight_receipt_facts_shipment
    ON city_open_world_enterprise_freight_receipt_facts (world_id, shipment_code, tick, sequence);

CREATE OR REPLACE FUNCTION city_open_world_enterprise_freight_receipt_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_enterprise_freight_receipt_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v17'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v17'
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
                   AND world.simulation_version = 'city-openworld-v17')
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_enterprise_freight_receipt_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_enterprise_freight_receipt_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_enterprise_freight_receipt_write_enabled(target_world_id)
       AND NEW.revision = OLD.revision + 1
       AND (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.shipment_contract, OLD.receipt_contract,
            OLD.legacy_contract, OLD.maximum_shipments, OLD.maximum_observations_per_tick,
            OLD.metadata, OLD.created_at)
           IS NOT DISTINCT FROM
           (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.shipment_contract, NEW.receipt_contract,
            NEW.legacy_contract, NEW.maximum_shipments, NEW.maximum_observations_per_tick,
            NEW.metadata, NEW.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V17 freight-receipt profile is immutable outside its audited projection write path'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_enterprise_freight_receipt_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    target_world_id BIGINT; source_code_value VARCHAR(160); source_order_value VARCHAR(160);
    row_data JSONB;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' OR NOT city_open_world_enterprise_freight_receipt_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V17 freight-receipt projections require their audited write context'
            USING ERRCODE = '55000';
    END IF;
    -- One trigger function serves six projection tables with different row
    -- records.  Access table-specific columns through JSONB so PostgreSQL
    -- does not attempt to resolve (for example) source_fact_id on a line row
    -- while compiling this trigger for that table.
    row_data := to_jsonb(NEW);
    IF TG_TABLE_NAME = 'city_open_world_enterprise_freight_shipments' THEN
        SELECT fact.source_code, source.order_code
          INTO source_code_value, source_order_value
        FROM city_open_world_enterprise_freight_facts fact
        JOIN city_open_world_enterprise_freight_sources source
          ON source.world_id = fact.world_id AND source.code = fact.source_code
        WHERE fact.id = (row_data->>'source_freight_fact_id')::BIGINT
          AND fact.world_id = target_world_id
          AND fact.fact_type = 'source.created';
        IF source_code_value IS DISTINCT FROM row_data->>'freight_source_code'
           OR source_order_value IS DISTINCT FROM row_data->>'order_code' THEN
            RAISE EXCEPTION 'open-world V17 shipment must root in its V16 source.created fact'
                USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_open_world_enterprise_freight_receipt_facts' THEN
        IF row_data->>'evidence_kind' = 'enterprise_freight' AND NOT EXISTS (
            SELECT 1 FROM city_open_world_enterprise_freight_facts fact
            WHERE fact.id = (row_data->>'freight_fact_id')::BIGINT AND fact.world_id = target_world_id
              AND fact.tick = (row_data->>'tick')::BIGINT AND fact.sequence = (row_data->>'sequence')::BIGINT
        ) THEN
            RAISE EXCEPTION 'open-world V17 freight evidence must match its V16 fact cursor' USING ERRCODE = '23514';
        END IF;
        IF row_data->>'evidence_kind' = 'supply_chain' AND NOT EXISTS (
            SELECT 1 FROM city_open_world_supply_chain_facts fact
            WHERE fact.id = (row_data->>'supply_chain_fact_id')::BIGINT AND fact.world_id = target_world_id
              AND fact.tick = (row_data->>'tick')::BIGINT AND fact.sequence = (row_data->>'sequence')::BIGINT
              AND fact.fact_type = 'order.delivered'
        ) THEN
            RAISE EXCEPTION 'open-world V17 receipt evidence must match its V15 delivery fact cursor' USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_open_world_enterprise_freight_shipment_transitions' AND NOT EXISTS (
        SELECT 1 FROM city_open_world_enterprise_freight_receipt_facts fact
        WHERE fact.id = (row_data->>'source_fact_id')::BIGINT AND fact.world_id = target_world_id
          AND fact.shipment_code = row_data->>'shipment_code'
          AND fact.tick = (row_data->>'transition_tick')::BIGINT
          AND fact.sequence = (row_data->>'transition_sequence')::BIGINT
    ) THEN
        RAISE EXCEPTION 'open-world V17 shipment transition must reference its receipt fact cursor'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS v17_freight_receipt_profile_guard ON city_open_world_enterprise_freight_receipt_profiles;
CREATE TRIGGER v17_freight_receipt_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_receipt_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_enterprise_freight_receipt_profile();

DROP TRIGGER IF EXISTS v17_freight_receipt_shipment_guard ON city_open_world_enterprise_freight_shipments;
CREATE TRIGGER v17_freight_receipt_shipment_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_shipments
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_enterprise_freight_receipt_projection();
DROP TRIGGER IF EXISTS v17_freight_receipt_line_guard ON city_open_world_enterprise_freight_shipment_lines;
CREATE TRIGGER v17_freight_receipt_line_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_shipment_lines
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_enterprise_freight_receipt_projection();
DROP TRIGGER IF EXISTS v17_freight_receipt_fact_guard ON city_open_world_enterprise_freight_receipt_facts;
CREATE TRIGGER v17_freight_receipt_fact_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_receipt_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_enterprise_freight_receipt_projection();
DROP TRIGGER IF EXISTS v17_freight_receipt_transition_guard ON city_open_world_enterprise_freight_shipment_transitions;
CREATE TRIGGER v17_freight_receipt_transition_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_shipment_transitions
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_enterprise_freight_receipt_projection();
DROP TRIGGER IF EXISTS v17_freight_receipt_receipt_guard ON city_open_world_enterprise_freight_receipts;
CREATE TRIGGER v17_freight_receipt_receipt_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_receipts
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_enterprise_freight_receipt_projection();

CREATE OR REPLACE FUNCTION assert_city_open_world_enterprise_freight_receipt_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version VARCHAR(32); world_tick BIGINT; profile_tick BIGINT;
    shipments BIGINT; awaiting_route BIGINT; in_transit BIGINT; awaiting_receipt BIGINT;
    received BIGINT; expired BIGINT; voided BIGINT; orphaned BIGINT; facts BIGINT; transitions BIGINT; receipts BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v17' THEN RETURN; END IF;
    IF NOT EXISTS (SELECT 1 FROM city_open_world_supply_chain_profiles WHERE world_id = target_world_id)
       OR NOT EXISTS (SELECT 1 FROM city_open_world_enterprise_freight_profiles WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V17 freight-receipt predecessor foundations are missing' USING ERRCODE = '23514';
    END IF;
    SELECT baseline_tick, shipment_count, awaiting_route_count, in_transit_count,
           awaiting_receipt_count, received_count, expired_count, voided_count,
           orphaned_count, fact_count, transition_count, receipt_count
      INTO profile_tick, shipments, awaiting_route, in_transit, awaiting_receipt,
           received, expired, voided, orphaned, facts, transitions, receipts
    FROM city_open_world_enterprise_freight_receipt_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V17 freight-receipt profile is missing or invalid' USING ERRCODE = '23514';
    END IF;
    IF shipments <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id)
       OR awaiting_route <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'awaiting_route')
       OR in_transit <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'in_transit')
       OR awaiting_receipt <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'awaiting_receipt')
       OR received <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'received')
       OR expired <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'expired')
       OR voided <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'voided')
       OR orphaned <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'orphaned')
       OR facts <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_receipt_facts WHERE world_id = target_world_id)
       OR transitions <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipment_transitions WHERE world_id = target_world_id)
       OR receipts <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_receipts WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V17 freight-receipt counters are inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_shipments shipment
        JOIN city_open_world_enterprise_freight_sources source
          ON source.world_id = shipment.world_id AND source.code = shipment.freight_source_code
        JOIN city_open_world_enterprise_freight_facts root_fact
          ON root_fact.id = shipment.source_freight_fact_id AND root_fact.world_id = shipment.world_id
        LEFT JOIN city_open_world_enterprise_freight_receipt_facts last_fact
          ON last_fact.id = shipment.last_receipt_fact_id AND last_fact.world_id = shipment.world_id
        WHERE shipment.world_id = target_world_id
          AND (source.order_code <> shipment.order_code
               OR source.seller_node_code <> shipment.seller_node_code
               OR source.buyer_node_code <> shipment.buyer_node_code
               OR source.source_hub_code <> shipment.source_hub_code
               OR source.destination_hub_code <> shipment.destination_hub_code
               OR source.source_tick <> shipment.source_tick
               OR source.requested_units <> shipment.requested_units
               OR source.source_tick <= profile_tick
               OR source.state = 'suppressed'
               OR root_fact.fact_type <> 'source.created'
               OR root_fact.source_code <> shipment.freight_source_code
               OR last_fact.id IS NULL)
    ) OR EXISTS (
        SELECT 1 FROM city_open_world_enterprise_freight_shipments shipment
        WHERE shipment.world_id = target_world_id
          AND NOT EXISTS (SELECT 1 FROM city_open_world_enterprise_freight_shipment_lines line
                          WHERE line.world_id = shipment.world_id AND line.shipment_code = shipment.code)
    ) THEN
        RAISE EXCEPTION 'city open-world V17 shipment identity or line snapshot is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_shipment_lines line
        JOIN city_open_world_enterprise_freight_shipments shipment
          ON shipment.world_id = line.world_id AND shipment.code = line.shipment_code
        LEFT JOIN city_open_world_enterprise_freight_source_lines source_line
          ON source_line.world_id = shipment.world_id AND source_line.source_code = shipment.freight_source_code
         AND source_line.line_no = line.line_no
        WHERE line.world_id = target_world_id
          AND (source_line.id IS NULL
               OR source_line.resource_code <> line.resource_code
               OR source_line.source_firm_code <> line.source_firm_code
               OR source_line.source_district_code <> line.source_district_code
               OR source_line.destination_firm_code <> line.destination_firm_code
               OR source_line.destination_district_code <> line.destination_district_code
               OR source_line.quantity_units <> line.quantity_units
               OR source_line.unit_price_units <> line.unit_price_units
               OR source_line.total_price_units <> line.total_price_units)
    ) OR EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_shipments shipment
        WHERE shipment.world_id = target_world_id
          AND (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipment_lines line
               WHERE line.world_id = shipment.world_id AND line.shipment_code = shipment.code)
              <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_source_lines source_line
                  WHERE source_line.world_id = shipment.world_id AND source_line.source_code = shipment.freight_source_code)
    ) THEN
        RAISE EXCEPTION 'city open-world V17 shipment line snapshot diverges from V16 source' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_receipt_facts fact
        JOIN city_open_world_enterprise_freight_shipments shipment
          ON shipment.world_id = fact.world_id AND shipment.code = fact.shipment_code
        LEFT JOIN city_open_world_enterprise_freight_facts freight
          ON freight.id = fact.freight_fact_id AND freight.world_id = fact.world_id
        LEFT JOIN city_open_world_supply_chain_facts supply
          ON supply.id = fact.supply_chain_fact_id AND supply.world_id = fact.world_id
        WHERE fact.world_id = target_world_id
          AND ((fact.evidence_kind = 'enterprise_freight' AND
                (freight.source_code <> shipment.freight_source_code OR freight.tick <> fact.tick OR freight.sequence <> fact.sequence
                 OR (fact.fact_type = 'shipment.created' AND freight.fact_type <> 'source.created')
                 OR (fact.fact_type = 'route.awaiting' AND freight.fact_type <> 'demand.requested')
                 OR (fact.fact_type = 'transport.in_transit' AND freight.fact_type <> 'route.scheduled')
                 OR (fact.fact_type = 'transport.arrived' AND freight.fact_type <> 'route.completed')
                 OR (fact.fact_type = 'transport.expired' AND freight.fact_type <> 'demand.expired')
                 OR (fact.fact_type = 'transport.voided' AND freight.fact_type <> 'demand.voided')
                 OR (fact.fact_type = 'transport.orphaned' AND freight.fact_type <> 'transport.orphaned')))
               OR (fact.evidence_kind = 'supply_chain' AND
                   (fact.fact_type <> 'receipt.confirmed' OR supply.order_code <> shipment.order_code
                    OR supply.fact_type <> 'order.delivered' OR supply.tick <> fact.tick OR supply.sequence <> fact.sequence)))
    ) THEN
        RAISE EXCEPTION 'city open-world V17 receipt evidence is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_shipment_transitions transition
        JOIN city_open_world_enterprise_freight_receipt_facts fact
          ON fact.id = transition.source_fact_id AND fact.world_id = transition.world_id
        WHERE transition.world_id = target_world_id
          AND (fact.shipment_code <> transition.shipment_code
               OR fact.tick <> transition.transition_tick OR fact.sequence <> transition.transition_sequence
               OR (transition.state = 'awaiting_route' AND fact.fact_type <> 'route.awaiting')
               OR (transition.state = 'in_transit' AND fact.fact_type <> 'transport.in_transit')
               OR (transition.state = 'awaiting_receipt' AND fact.fact_type <> 'transport.arrived')
               OR (transition.state = 'received' AND fact.fact_type <> 'receipt.confirmed')
               OR (transition.state = 'expired' AND fact.fact_type <> 'transport.expired')
               OR (transition.state = 'voided' AND fact.fact_type <> 'transport.voided')
               OR (transition.state = 'orphaned' AND fact.fact_type <> 'transport.orphaned'))
    ) THEN
        RAISE EXCEPTION 'city open-world V17 shipment transition is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_shipments shipment
        WHERE shipment.world_id = target_world_id
          AND shipment.version <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipment_transitions transition
                                   WHERE transition.world_id = shipment.world_id AND transition.shipment_code = shipment.code)
    ) THEN
        RAISE EXCEPTION 'city open-world V17 shipment version is inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_receipts receipt
        JOIN city_open_world_enterprise_freight_shipments shipment
          ON shipment.world_id = receipt.world_id AND shipment.code = receipt.shipment_code
        JOIN city_open_world_supply_chain_deliveries delivery
          ON delivery.id = receipt.supply_chain_delivery_id AND delivery.world_id = receipt.world_id
        JOIN city_open_world_supply_chain_facts delivery_fact
          ON delivery_fact.id = delivery.source_fact_id AND delivery_fact.world_id = receipt.world_id
        JOIN city_resource_operations operation
          ON operation.id = receipt.resource_operation_id AND operation.world_id = receipt.world_id
        JOIN city_open_world_enterprise_freight_receipt_facts fact
          ON fact.id = receipt.source_fact_id AND fact.world_id = receipt.world_id
        WHERE receipt.world_id = target_world_id
          AND (receipt.order_code <> shipment.order_code OR receipt.order_code <> delivery.order_code
               OR receipt.received_tick <> delivery.delivered_tick OR receipt.received_tick <> delivery_fact.tick
               OR delivery.resource_operation_id <> operation.id OR delivery_fact.fact_type <> 'order.delivered'
               OR fact.fact_type <> 'receipt.confirmed' OR fact.supply_chain_fact_id <> delivery_fact.id
               OR shipment.state <> 'received')
    ) OR EXISTS (
        SELECT 1 FROM city_open_world_enterprise_freight_shipments shipment
        WHERE shipment.world_id = target_world_id
          AND ((shipment.state = 'received') <> EXISTS (
                SELECT 1 FROM city_open_world_enterprise_freight_receipts receipt
                WHERE receipt.world_id = shipment.world_id AND receipt.shipment_code = shipment.code))
    ) THEN
        RAISE EXCEPTION 'city open-world V17 receipt projection is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_supply_chain_deliveries delivery
        JOIN city_open_world_enterprise_freight_sources source
          ON source.world_id = delivery.world_id AND source.order_code = delivery.order_code
        WHERE delivery.world_id = target_world_id
          AND source.source_tick > profile_tick AND source.state <> 'suppressed'
          AND NOT EXISTS (
              SELECT 1
              FROM city_open_world_enterprise_freight_shipments shipment
              JOIN city_open_world_enterprise_freight_receipts receipt
                ON receipt.world_id = shipment.world_id AND receipt.shipment_code = shipment.code
              WHERE shipment.world_id = delivery.world_id
                AND shipment.freight_source_code = source.code
                AND receipt.supply_chain_delivery_id = delivery.id
          )
    ) THEN
        RAISE EXCEPTION 'city open-world V17 delivery lacks a completed custody receipt' USING ERRCODE = '23514';
    END IF;
END;
$$;

-- V17 retains every V16 transport invariant.  Redefine the predecessor
-- assertion rather than relying on its original version guard, otherwise a
-- malformed freight source could evade the V16 deferred checks once a world
-- has advanced to the receipt layer.
CREATE OR REPLACE FUNCTION assert_city_open_world_enterprise_freight_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version VARCHAR(32); world_tick BIGINT; profile_tick BIGINT;
    sources BIGINT; pending BIGINT; demands BIGINT; scheduled BIGINT; completed BIGINT;
    expired BIGINT; voided BIGINT; orphaned BIGINT; suppressed BIGINT; facts BIGINT; transitions BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v16', 'city-openworld-v17') THEN RETURN; END IF;
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

CREATE OR REPLACE FUNCTION check_city_open_world_enterprise_freight_receipt_foundation_commit()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        RETURN COALESCE(NEW, OLD);
    END IF;
    PERFORM assert_city_open_world_enterprise_freight_receipt_foundation(target_world_id);
    RETURN COALESCE(NEW, OLD);
END;
$$;

DROP TRIGGER IF EXISTS v17_freight_receipt_profile_commit ON city_open_world_enterprise_freight_receipt_profiles;
CREATE CONSTRAINT TRIGGER v17_freight_receipt_profile_commit
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_receipt_profiles
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_city_open_world_enterprise_freight_receipt_foundation_commit();
DROP TRIGGER IF EXISTS v17_freight_receipt_shipment_commit ON city_open_world_enterprise_freight_shipments;
CREATE CONSTRAINT TRIGGER v17_freight_receipt_shipment_commit
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_shipments
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_city_open_world_enterprise_freight_receipt_foundation_commit();
DROP TRIGGER IF EXISTS v17_freight_receipt_line_commit ON city_open_world_enterprise_freight_shipment_lines;
CREATE CONSTRAINT TRIGGER v17_freight_receipt_line_commit
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_shipment_lines
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_city_open_world_enterprise_freight_receipt_foundation_commit();
DROP TRIGGER IF EXISTS v17_freight_receipt_fact_commit ON city_open_world_enterprise_freight_receipt_facts;
CREATE CONSTRAINT TRIGGER v17_freight_receipt_fact_commit
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_receipt_facts
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_city_open_world_enterprise_freight_receipt_foundation_commit();
DROP TRIGGER IF EXISTS v17_freight_receipt_transition_commit ON city_open_world_enterprise_freight_shipment_transitions;
CREATE CONSTRAINT TRIGGER v17_freight_receipt_transition_commit
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_shipment_transitions
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_city_open_world_enterprise_freight_receipt_foundation_commit();
DROP TRIGGER IF EXISTS v17_freight_receipt_receipt_commit ON city_open_world_enterprise_freight_receipts;
CREATE CONSTRAINT TRIGGER v17_freight_receipt_receipt_commit
AFTER INSERT OR UPDATE OR DELETE ON city_open_world_enterprise_freight_receipts
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_city_open_world_enterprise_freight_receipt_foundation_commit();

-- V17 is a strict successor. These replacements preserve each predecessor's
-- existing write gate and only add V17 where V17 genesis/recovery needs it.
CREATE OR REPLACE FUNCTION city_open_world_supply_chain_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_supply_chain_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v15','city-openworld-v16','city-openworld-v17')
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
                   AND world.simulation_version IN ('city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_enterprise_freight_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_enterprise_freight_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v16','city-openworld-v17')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_enterprise_freight_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_enterprise_freight_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v16','city-openworld-v17'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_lifecycle_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_lifecycle_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_lifecycle_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_lifecycle_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_source_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_source_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_source_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_source_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_arrival_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_arrival_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_arrival_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_od_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_od_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_od_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_service_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_impact_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v1','city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_materialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_materialization_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_fact_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_fact_insert()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE world_tick BIGINT; world_version VARCHAR(32); command_status_value VARCHAR(16);
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN RETURN NEW; END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version FROM city_worlds WHERE id = NEW.world_id;
    IF world_version NOT IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17')
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
       AND NOT (world_version IN ('city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15','city-openworld-v16','city-openworld-v17') AND NEW.fact_type LIKE 'system.%') THEN
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
          AND world.simulation_version IN ('city-openworld-v15','city-openworld-v16','city-openworld-v17')
          AND operation.operation_type = 'transfer'
          AND command.command_type = 'open_world.supply_order.deliver'
          AND operation.actor_entity_id = seller.firm_entity_id
          AND operation.district_id = seller.district_id
          AND (
              world.simulation_version <> 'city-openworld-v17'
              -- V16 sources already present at the V17 upgrade baseline are
              -- intentionally legacy. They retain V16 delivery semantics and
              -- must not be blocked merely because V17 has no fabricated
              -- custody receipt for historical transport.
              OR NOT EXISTS (
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
END;
$$;
