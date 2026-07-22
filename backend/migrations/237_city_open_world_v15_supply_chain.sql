-- city-openworld-v15 / F10.0 adds a narrow, append-only enterprise supply
-- chain contract.  It deliberately reuses F2 journals and F3 inventory
-- balances: orders never become a second monetary or warehouse projection.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v15', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","version_vector","facilities","npc_lod","social_service_catalog","social_service_access","social_service_queue","social_service_response","cross_domain_impact_bridge","aggregate_mobility_graph","mobility_capacity_allocation","mobility_route_lifecycle","mobility_arrival_bridge","cross_scale_spatial_transfer","versioned_od_sources","automatic_od_demands","mobility_cycle_metrics","residence_employment_bindings","verified_commute_sources","facility_presence_origin_validation","dual_direction_commutes","commute_assignment_epochs","commute_lifecycle_transitions","enterprise_supply_chain_nodes","supply_orders","inventory_reservations","purchase_settlement","fact_backed_dispatch","atomic_delivery","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v14', 'city-openworld-v15', 'openworld_v14_to_v15')
ON CONFLICT (from_version, to_version) DO NOTHING;

-- city_inventory_balances already pins an inventory's resource through
-- (id, world_id, resource_id).  Reservations additionally need a
-- world-scoped reference to the exact source balance; PostgreSQL requires an
-- explicit matching unique key for that defensive FK even though id is the
-- physical primary key.  Keep the key narrow and reusable for other
-- world-scoped evidence tables rather than dropping the reservation FK.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'city_inventory_balances'::regclass
          AND conname = 'city_inventory_balances_id_world_unique'
    ) THEN
        ALTER TABLE city_inventory_balances
            ADD CONSTRAINT city_inventory_balances_id_world_unique UNIQUE (id, world_id);
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS city_open_world_supply_chain_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_commute_lifecycle_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    node_contract VARCHAR(96) NOT NULL,
    order_contract VARCHAR(96) NOT NULL,
    settlement_contract VARCHAR(96) NOT NULL,
    delivery_contract VARCHAR(96) NOT NULL,
    maximum_orders INTEGER NOT NULL CHECK (maximum_orders BETWEEN 1 AND 100000),
    maximum_order_lines INTEGER NOT NULL CHECK (maximum_order_lines BETWEEN 1 AND 128),
    maximum_transitions_per_tick INTEGER NOT NULL CHECK (maximum_transitions_per_tick BETWEEN 1 AND 100000),
    accept_timeout_ticks BIGINT NOT NULL CHECK (accept_timeout_ticks BETWEEN 1 AND 1000000),
    dispatch_timeout_ticks BIGINT NOT NULL CHECK (dispatch_timeout_ticks BETWEEN 1 AND 1000000),
    node_count BIGINT NOT NULL DEFAULT 0 CHECK (node_count >= 0),
    order_count BIGINT NOT NULL DEFAULT 0 CHECK (order_count >= 0),
    active_order_count BIGINT NOT NULL DEFAULT 0 CHECK (active_order_count >= 0),
    fact_count BIGINT NOT NULL DEFAULT 0 CHECK (fact_count >= 0),
    reservation_count BIGINT NOT NULL DEFAULT 0 CHECK (reservation_count >= 0),
    release_count BIGINT NOT NULL DEFAULT 0 CHECK (release_count >= 0),
    dispatch_count BIGINT NOT NULL DEFAULT 0 CHECK (dispatch_count >= 0),
    delivery_count BIGINT NOT NULL DEFAULT 0 CHECK (delivery_count >= 0),
    settlement_count BIGINT NOT NULL DEFAULT 0 CHECK (settlement_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_supply_chain_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-supply-chain'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND node_contract = 'firm_facility_district_node_v1'
        AND order_contract = 'append_only_order_transition_v1'
        AND settlement_contract = 'acceptance_purchase_reversal_v1'
        AND delivery_contract = 'atomic_inventory_transfer_v1'
        AND maximum_orders = 10000
        AND maximum_order_lines = 32
        AND maximum_transitions_per_tick = 512
        AND accept_timeout_ticks = 12
        AND dispatch_timeout_ticks = 24
    ),
    CONSTRAINT city_open_world_supply_chain_profile_timeout_check CHECK (
        dispatch_timeout_ticks > accept_timeout_ticks
    ),
    CONSTRAINT city_open_world_supply_chain_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_supply_chain_nodes (
    id BIGSERIAL NOT NULL,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_supply_chain_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    firm_entity_id BIGINT NOT NULL,
    facility_id BIGINT NOT NULL,
    district_id BIGINT NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'active',
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_supply_chain_node_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_open_world_supply_chain_node_firm_facility_unique UNIQUE (world_id, firm_entity_id, facility_id),
    CONSTRAINT city_open_world_supply_chain_node_firm_fk
        FOREIGN KEY (firm_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_node_facility_fk
        FOREIGN KEY (facility_id, world_id)
        REFERENCES city_open_world_facilities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_node_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_node_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND state = 'active'
    ),
    CONSTRAINT city_open_world_supply_chain_node_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_supply_chain_facts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_supply_chain_profiles(world_id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    source_command_id BIGINT,
    order_code VARCHAR(160),
    fact_type VARCHAR(96) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_supply_chain_fact_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_supply_chain_fact_identity_check CHECK (
        (order_code IS NULL OR order_code ~ '^[a-z][a-z0-9_.-]{1,159}$')
        AND fact_type IN (
            'order.proposed', 'order.accepted', 'order.dispatched',
            'order.delivered', 'order.cancelled', 'order.expired', 'order.failed'
        )
    ),
    CONSTRAINT city_open_world_supply_chain_fact_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT city_open_world_supply_chain_fact_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_open_world_supply_chain_fact_id_world_unique UNIQUE (id, world_id)
);

CREATE TABLE IF NOT EXISTS city_open_world_supply_chain_orders (
    id BIGSERIAL NOT NULL,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_supply_chain_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    buyer_node_code VARCHAR(160) NOT NULL,
    seller_node_code VARCHAR(160) NOT NULL,
    created_tick BIGINT NOT NULL CHECK (created_tick > 0),
    accept_deadline_tick BIGINT NOT NULL CHECK (accept_deadline_tick > 0),
    dispatch_deadline_tick BIGINT NOT NULL CHECK (dispatch_deadline_tick > 0),
    created_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_supply_chain_order_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_open_world_supply_chain_order_buyer_fk
        FOREIGN KEY (world_id, buyer_node_code)
        REFERENCES city_open_world_supply_chain_nodes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_order_seller_fk
        FOREIGN KEY (world_id, seller_node_code)
        REFERENCES city_open_world_supply_chain_nodes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_order_created_fact_fk
        FOREIGN KEY (created_fact_id, world_id)
        REFERENCES city_open_world_supply_chain_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_supply_chain_order_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND buyer_node_code <> seller_node_code
        AND created_tick < accept_deadline_tick
        AND accept_deadline_tick < dispatch_deadline_tick
    ),
    CONSTRAINT city_open_world_supply_chain_order_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_supply_chain_order_lines (
    id BIGSERIAL NOT NULL,
    world_id BIGINT NOT NULL,
    order_code VARCHAR(160) NOT NULL,
    line_no INTEGER NOT NULL CHECK (line_no > 0),
    resource_id BIGINT NOT NULL,
    source_balance_id BIGINT NOT NULL,
    destination_balance_id BIGINT NOT NULL,
    quantity_units BIGINT NOT NULL CHECK (quantity_units > 0),
    unit_price_units BIGINT NOT NULL CHECK (unit_price_units > 0),
    total_price_units BIGINT NOT NULL CHECK (total_price_units > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, order_code, line_no),
    CONSTRAINT city_open_world_supply_chain_line_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_open_world_supply_chain_line_order_fk
        FOREIGN KEY (world_id, order_code)
        REFERENCES city_open_world_supply_chain_orders(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_line_resource_fk
        FOREIGN KEY (resource_id, world_id)
        REFERENCES city_resources(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_line_source_balance_fk
        FOREIGN KEY (source_balance_id, world_id, resource_id)
        REFERENCES city_inventory_balances(id, world_id, resource_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_line_destination_balance_fk
        FOREIGN KEY (destination_balance_id, world_id, resource_id)
        REFERENCES city_inventory_balances(id, world_id, resource_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_line_identity_check CHECK (
        source_balance_id <> destination_balance_id
        AND quantity_units::NUMERIC * unit_price_units::NUMERIC = total_price_units::NUMERIC
    ),
    CONSTRAINT city_open_world_supply_chain_line_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_supply_chain_line_order_resource_unique UNIQUE (world_id, order_code, resource_id)
);

CREATE TABLE IF NOT EXISTS city_open_world_supply_chain_order_transitions (
    id BIGSERIAL NOT NULL,
    world_id BIGINT NOT NULL,
    order_code VARCHAR(160) NOT NULL,
    transition_tick BIGINT NOT NULL CHECK (transition_tick > 0),
    transition_sequence BIGINT NOT NULL CHECK (transition_sequence > 0),
    state VARCHAR(16) NOT NULL,
    reason_code VARCHAR(96) NOT NULL,
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, order_code, transition_tick, transition_sequence),
    CONSTRAINT city_open_world_supply_chain_transition_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_open_world_supply_chain_transition_order_fk
        FOREIGN KEY (world_id, order_code)
        REFERENCES city_open_world_supply_chain_orders(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_transition_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_supply_chain_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_transition_identity_check CHECK (
        state IN ('proposed', 'accepted', 'dispatched', 'delivered', 'cancelled', 'expired', 'failed')
        AND reason_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
    ),
    CONSTRAINT city_open_world_supply_chain_transition_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_supply_chain_reservations (
    id BIGSERIAL NOT NULL,
    world_id BIGINT NOT NULL,
    order_code VARCHAR(160) NOT NULL,
    line_no INTEGER NOT NULL,
    source_balance_id BIGINT NOT NULL,
    quantity_units BIGINT NOT NULL CHECK (quantity_units > 0),
    reserved_tick BIGINT NOT NULL CHECK (reserved_tick > 0),
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, order_code, line_no),
    CONSTRAINT city_open_world_supply_chain_reservation_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_open_world_supply_chain_reservation_line_fk
        FOREIGN KEY (world_id, order_code, line_no)
        REFERENCES city_open_world_supply_chain_order_lines(world_id, order_code, line_no) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_reservation_balance_fk
        FOREIGN KEY (source_balance_id, world_id)
        REFERENCES city_inventory_balances(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_reservation_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_supply_chain_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_reservation_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_supply_chain_reservation_releases (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL,
    reservation_id BIGINT NOT NULL,
    released_tick BIGINT NOT NULL CHECK (released_tick > 0),
    reason_code VARCHAR(96) NOT NULL,
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_supply_chain_release_reservation_fk
        FOREIGN KEY (reservation_id, world_id)
        REFERENCES city_open_world_supply_chain_reservations(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_release_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_supply_chain_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_release_identity_check CHECK (
        reason_code IN ('delivered', 'cancelled', 'expired', 'failed')
    ),
    CONSTRAINT city_open_world_supply_chain_release_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_supply_chain_release_reservation_unique UNIQUE (world_id, reservation_id)
);

CREATE TABLE IF NOT EXISTS city_open_world_supply_chain_dispatches (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL,
    order_code VARCHAR(160) NOT NULL,
    dispatched_tick BIGINT NOT NULL CHECK (dispatched_tick > 0),
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_supply_chain_dispatch_order_fk
        FOREIGN KEY (world_id, order_code)
        REFERENCES city_open_world_supply_chain_orders(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_dispatch_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_supply_chain_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_dispatch_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_supply_chain_dispatch_order_unique UNIQUE (world_id, order_code)
);

CREATE TABLE IF NOT EXISTS city_open_world_supply_chain_deliveries (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL,
    order_code VARCHAR(160) NOT NULL,
    delivered_tick BIGINT NOT NULL CHECK (delivered_tick > 0),
    resource_operation_id BIGINT NOT NULL,
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_supply_chain_delivery_order_fk
        FOREIGN KEY (world_id, order_code)
        REFERENCES city_open_world_supply_chain_orders(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_delivery_operation_fk
        FOREIGN KEY (resource_operation_id, world_id)
        REFERENCES city_resource_operations(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_delivery_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_supply_chain_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_delivery_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_supply_chain_delivery_order_unique UNIQUE (world_id, order_code),
    CONSTRAINT city_open_world_supply_chain_delivery_operation_unique UNIQUE (world_id, resource_operation_id)
);

-- city_journals is keyed globally by id but its existing public uniqueness
-- includes the monetary unit. V15 settlement evidence is world-scoped, so
-- install the exact composite key required by its defensive foreign key.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'city_journals'::regclass
          AND conname = 'city_journals_id_world_unique'
    ) THEN
        ALTER TABLE city_journals
            ADD CONSTRAINT city_journals_id_world_unique UNIQUE (id, world_id);
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS city_open_world_supply_chain_settlements (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL,
    order_code VARCHAR(160) NOT NULL,
    settlement_kind VARCHAR(16) NOT NULL,
    journal_id BIGINT NOT NULL,
    source_fact_id BIGINT NOT NULL,
    reversal_of_settlement_id BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_supply_chain_settlement_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_open_world_supply_chain_settlement_order_fk
        FOREIGN KEY (world_id, order_code)
        REFERENCES city_open_world_supply_chain_orders(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_settlement_journal_fk
        FOREIGN KEY (journal_id, world_id)
        REFERENCES city_journals(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_settlement_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_supply_chain_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_settlement_reversal_fk
        FOREIGN KEY (reversal_of_settlement_id, world_id)
        REFERENCES city_open_world_supply_chain_settlements(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_supply_chain_settlement_identity_check CHECK (
        (settlement_kind = 'acceptance' AND reversal_of_settlement_id IS NULL)
        OR (settlement_kind = 'reversal' AND reversal_of_settlement_id IS NOT NULL)
    ),
    CONSTRAINT city_open_world_supply_chain_settlement_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_supply_chain_settlement_kind_unique UNIQUE (world_id, order_code, settlement_kind),
    CONSTRAINT city_open_world_supply_chain_settlement_reversal_unique UNIQUE (world_id, reversal_of_settlement_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_supply_chain_facts_world_tick
    ON city_open_world_supply_chain_facts (world_id, tick, sequence);
CREATE INDEX IF NOT EXISTS idx_city_open_world_supply_chain_orders_world_created
    ON city_open_world_supply_chain_orders (world_id, created_tick, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_supply_chain_transitions_world_order
    ON city_open_world_supply_chain_order_transitions (world_id, order_code, transition_tick DESC, transition_sequence DESC);
CREATE INDEX IF NOT EXISTS idx_city_open_world_supply_chain_reservations_balance
    ON city_open_world_supply_chain_reservations (world_id, source_balance_id);

-- A V15 automatic expiry is a machine-origin reversal, not an untracked
-- accounting exception. Manual reversals retain the established command-only
-- provenance requirement; auto expiry must declare its exact source tag.
ALTER TABLE city_journals
    DROP CONSTRAINT IF EXISTS city_journal_origin_check;
ALTER TABLE city_journals
    ADD CONSTRAINT city_journal_origin_check CHECK (
        (journal_type = 'opening'
            AND source_command_id IS NULL AND market_settlement_id IS NULL
            AND reversal_of_journal_id IS NULL)
        OR (journal_type = 'reversal' AND market_settlement_id IS NULL
            AND reversal_of_journal_id IS NOT NULL
            AND (
                source_command_id IS NOT NULL
                OR (source_command_id IS NULL
                    AND metadata->>'system_origin' = 'open_world_supply_chain.auto_expiry.v1')
            ))
        OR (journal_type NOT IN ('opening', 'reversal')
            AND reversal_of_journal_id IS NULL
            AND ((source_command_id IS NOT NULL)::INTEGER + (market_settlement_id IS NOT NULL)::INTEGER) = 1)
    );

CREATE OR REPLACE FUNCTION city_open_world_supply_chain_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_supply_chain_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v15'
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

CREATE OR REPLACE FUNCTION city_open_world_supply_chain_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_supply_chain_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v15'
              AND world.state_hash IS NOT NULL
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_supply_chain_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_supply_chain_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_supply_chain_write_enabled(target_world_id)
       AND (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.node_contract, OLD.order_contract,
            OLD.settlement_contract, OLD.delivery_contract, OLD.maximum_orders,
            OLD.maximum_order_lines, OLD.maximum_transitions_per_tick,
            OLD.accept_timeout_ticks, OLD.dispatch_timeout_ticks, OLD.node_count,
            OLD.metadata, OLD.created_at)
           IS NOT DISTINCT FROM
           (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.node_contract, NEW.order_contract,
            NEW.settlement_contract, NEW.delivery_contract, NEW.maximum_orders,
            NEW.maximum_order_lines, NEW.maximum_transitions_per_tick,
            NEW.accept_timeout_ticks, NEW.dispatch_timeout_ticks, NEW.node_count,
            NEW.metadata, NEW.created_at)
       AND NEW.revision = OLD.revision + 1 THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V15 supply-chain profile is immutable outside its audited projection write path'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_supply_chain_node()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_supply_chain_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V15 supply-chain nodes are frozen baseline identities'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_supply_chain_fact()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT; world_tick BIGINT; status_value VARCHAR(16);
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP <> 'INSERT' OR NOT city_open_world_supply_chain_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V15 supply-chain facts are append-only audited evidence' USING ERRCODE = '55000';
    END IF;
    SELECT current_tick INTO world_tick FROM city_worlds WHERE id = target_world_id;
    IF NEW.tick IS DISTINCT FROM world_tick + 1 THEN
        RAISE EXCEPTION 'open-world V15 supply-chain fact must target the next tick' USING ERRCODE = '23514';
    END IF;
    IF NEW.source_command_id IS NOT NULL THEN
        SELECT status INTO status_value FROM city_commands WHERE id = NEW.source_command_id AND world_id = target_world_id;
        IF status_value IS DISTINCT FROM 'pending' THEN
            RAISE EXCEPTION 'open-world V15 supply-chain fact requires a pending source command' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_supply_chain_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_supply_chain_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V15 supply-chain projections are immutable audited evidence'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_supply_chain_profile_guard ON city_open_world_supply_chain_profiles;
CREATE TRIGGER city_open_world_supply_chain_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_supply_chain_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_supply_chain_profile();
DROP TRIGGER IF EXISTS city_open_world_supply_chain_node_guard ON city_open_world_supply_chain_nodes;
CREATE TRIGGER city_open_world_supply_chain_node_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_supply_chain_nodes
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_supply_chain_node();
DROP TRIGGER IF EXISTS city_open_world_supply_chain_fact_guard ON city_open_world_supply_chain_facts;
CREATE TRIGGER city_open_world_supply_chain_fact_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_supply_chain_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_supply_chain_fact();

DO $$
DECLARE table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_open_world_supply_chain_orders',
        'city_open_world_supply_chain_order_lines',
        'city_open_world_supply_chain_order_transitions',
        'city_open_world_supply_chain_reservations',
        'city_open_world_supply_chain_reservation_releases',
        'city_open_world_supply_chain_dispatches',
        'city_open_world_supply_chain_deliveries',
        'city_open_world_supply_chain_settlements'
    ] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', table_name || '_guard', table_name);
        EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_supply_chain_projection()', table_name || '_guard', table_name);
    END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_supply_chain_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version VARCHAR(32); world_tick BIGINT; profile_tick BIGINT;
    nodes BIGINT; orders BIGINT; active_orders BIGINT; facts BIGINT; reservations BIGINT;
    releases BIGINT; dispatches BIGINT; deliveries BIGINT; settlements BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v15' THEN RETURN; END IF;

    SELECT baseline_tick, node_count, order_count, active_order_count, fact_count,
           reservation_count, release_count, dispatch_count, delivery_count, settlement_count
      INTO profile_tick, nodes, orders, active_orders, facts, reservations,
           releases, dispatches, deliveries, settlements
    FROM city_open_world_supply_chain_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick OR nodes < 2 THEN
        RAISE EXCEPTION 'city open-world V15 supply-chain profile is missing or invalid' USING ERRCODE = '23514';
    END IF;
    IF nodes <> (SELECT COUNT(*) FROM city_open_world_supply_chain_nodes WHERE world_id = target_world_id)
       OR orders <> (SELECT COUNT(*) FROM city_open_world_supply_chain_orders WHERE world_id = target_world_id)
       OR facts <> (SELECT COUNT(*) FROM city_open_world_supply_chain_facts WHERE world_id = target_world_id)
       OR reservations <> (SELECT COUNT(*) FROM city_open_world_supply_chain_reservations WHERE world_id = target_world_id)
       OR releases <> (SELECT COUNT(*) FROM city_open_world_supply_chain_reservation_releases WHERE world_id = target_world_id)
       OR dispatches <> (SELECT COUNT(*) FROM city_open_world_supply_chain_dispatches WHERE world_id = target_world_id)
       OR deliveries <> (SELECT COUNT(*) FROM city_open_world_supply_chain_deliveries WHERE world_id = target_world_id)
       OR settlements <> (SELECT COUNT(*) FROM city_open_world_supply_chain_settlements WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V15 supply-chain counters are inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_supply_chain_nodes node
        JOIN city_economic_entities firm ON firm.id = node.firm_entity_id AND firm.world_id = node.world_id
        JOIN city_open_world_facilities facility ON facility.id = node.facility_id AND facility.world_id = node.world_id
        JOIN city_districts district ON district.id = node.district_id AND district.world_id = node.world_id
        WHERE node.world_id = target_world_id
          AND (firm.entity_type <> 'firm' OR firm.status <> 'active' OR facility.state <> 'active')
    ) THEN
        RAISE EXCEPTION 'city open-world V15 supply-chain node identity is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_open_world_supply_chain_orders header
        WHERE header.world_id = target_world_id
          AND NOT EXISTS (
              SELECT 1 FROM city_open_world_supply_chain_order_lines line
              WHERE line.world_id = header.world_id AND line.order_code = header.code
          )
    ) THEN
        RAISE EXCEPTION 'city open-world V15 order has no lines' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_supply_chain_orders header
        JOIN city_open_world_supply_chain_nodes buyer
          ON buyer.world_id = header.world_id AND buyer.code = header.buyer_node_code
        JOIN city_open_world_supply_chain_nodes seller
          ON seller.world_id = header.world_id AND seller.code = header.seller_node_code
        WHERE header.world_id = target_world_id AND buyer.firm_entity_id = seller.firm_entity_id
    ) THEN
        RAISE EXCEPTION 'city open-world V15 order cannot trade with itself' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_supply_chain_order_lines line
        JOIN city_open_world_supply_chain_orders header
          ON header.world_id = line.world_id AND header.code = line.order_code
        JOIN city_open_world_supply_chain_nodes buyer
          ON buyer.world_id = header.world_id AND buyer.code = header.buyer_node_code
        JOIN city_open_world_supply_chain_nodes seller
          ON seller.world_id = header.world_id AND seller.code = header.seller_node_code
        JOIN city_inventory_balances source ON source.id = line.source_balance_id AND source.world_id = line.world_id
        JOIN city_inventory_balances destination ON destination.id = line.destination_balance_id AND destination.world_id = line.world_id
        WHERE line.world_id = target_world_id
          AND (source.entity_id <> seller.firm_entity_id OR source.district_id <> seller.district_id
               OR destination.entity_id <> buyer.firm_entity_id OR destination.district_id <> buyer.district_id
               OR source.resource_id <> line.resource_id OR destination.resource_id <> line.resource_id)
    ) THEN
        RAISE EXCEPTION 'city open-world V15 order line inventory scope is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        WITH ordered AS (
            SELECT transition.*, LAG(state) OVER (
                PARTITION BY world_id, order_code
                ORDER BY transition_tick, transition_sequence
            ) AS previous_state,
            ROW_NUMBER() OVER (
                PARTITION BY world_id, order_code
                ORDER BY transition_tick, transition_sequence
            ) AS ordinal
            FROM city_open_world_supply_chain_order_transitions transition
            WHERE world_id = target_world_id
        )
        SELECT 1 FROM ordered
        WHERE (ordinal = 1 AND state <> 'proposed')
           OR (ordinal > 1 AND NOT (
               (previous_state = 'proposed' AND state IN ('accepted', 'cancelled', 'expired'))
               OR (previous_state = 'accepted' AND state IN ('dispatched', 'cancelled', 'expired'))
               OR (previous_state = 'dispatched' AND state IN ('delivered', 'failed'))
           ))
    ) THEN
        RAISE EXCEPTION 'city open-world V15 order transition chain is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_open_world_supply_chain_orders header
        WHERE header.world_id = target_world_id
          AND NOT EXISTS (
              SELECT 1 FROM city_open_world_supply_chain_order_transitions transition
              WHERE transition.world_id = header.world_id AND transition.order_code = header.code
          )
    ) THEN
        RAISE EXCEPTION 'city open-world V15 order lacks a transition' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        WITH latest AS (
            SELECT DISTINCT ON (world_id, order_code) world_id, order_code, state
            FROM city_open_world_supply_chain_order_transitions
            WHERE world_id = target_world_id
            ORDER BY world_id, order_code, transition_tick DESC, transition_sequence DESC
        )
        SELECT 1 FROM latest current
        JOIN city_open_world_supply_chain_orders header
          ON header.world_id = current.world_id AND header.code = current.order_code
        WHERE (EXISTS (SELECT 1 FROM city_open_world_supply_chain_order_transitions accepted
                       WHERE accepted.world_id = current.world_id AND accepted.order_code = current.order_code
                         AND accepted.state = 'accepted')
               AND NOT EXISTS (SELECT 1 FROM city_open_world_supply_chain_settlements settlement
                               WHERE settlement.world_id = current.world_id AND settlement.order_code = current.order_code
                                 AND settlement.settlement_kind = 'acceptance'))
           OR (EXISTS (SELECT 1 FROM city_open_world_supply_chain_order_transitions accepted
                       WHERE accepted.world_id = current.world_id AND accepted.order_code = current.order_code
                         AND accepted.state = 'accepted')
               AND (SELECT COUNT(*) FROM city_open_world_supply_chain_reservations reservation
                    WHERE reservation.world_id = current.world_id AND reservation.order_code = current.order_code)
                   <> (SELECT COUNT(*) FROM city_open_world_supply_chain_order_lines line
                       WHERE line.world_id = current.world_id AND line.order_code = current.order_code))
           OR (EXISTS (SELECT 1 FROM city_open_world_supply_chain_order_transitions accepted
                       WHERE accepted.world_id = current.world_id AND accepted.order_code = current.order_code
                         AND accepted.state = 'accepted')
               AND current.state IN ('delivered', 'cancelled', 'expired', 'failed')
               AND (SELECT COUNT(*) FROM city_open_world_supply_chain_reservation_releases release
                    JOIN city_open_world_supply_chain_reservations reservation
                      ON reservation.id = release.reservation_id AND reservation.world_id = release.world_id
                    WHERE release.world_id = current.world_id AND reservation.order_code = current.order_code)
                   <> (SELECT COUNT(*) FROM city_open_world_supply_chain_order_lines line
                       WHERE line.world_id = current.world_id AND line.order_code = current.order_code))
           OR (current.state = 'dispatched'
               AND NOT EXISTS (SELECT 1 FROM city_open_world_supply_chain_dispatches dispatch
                               WHERE dispatch.world_id = current.world_id AND dispatch.order_code = current.order_code))
           OR (current.state = 'delivered'
               AND NOT EXISTS (SELECT 1 FROM city_open_world_supply_chain_deliveries delivery
                               WHERE delivery.world_id = current.world_id AND delivery.order_code = current.order_code))
           OR (current.state IN ('cancelled', 'expired', 'failed')
               AND EXISTS (SELECT 1 FROM city_open_world_supply_chain_order_transitions accepted
                           WHERE accepted.world_id = current.world_id AND accepted.order_code = current.order_code
                             AND accepted.state = 'accepted')
               AND NOT EXISTS (SELECT 1 FROM city_open_world_supply_chain_settlements settlement
                               WHERE settlement.world_id = current.world_id AND settlement.order_code = current.order_code
                                 AND settlement.settlement_kind = 'reversal'))
    ) THEN
        RAISE EXCEPTION 'city open-world V15 order lifecycle evidence is incomplete' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_supply_chain_reservations reservation
        LEFT JOIN city_open_world_supply_chain_reservation_releases release
          ON release.world_id = reservation.world_id AND release.reservation_id = reservation.id
        JOIN city_inventory_balances balance
          ON balance.id = reservation.source_balance_id AND balance.world_id = reservation.world_id
        WHERE reservation.world_id = target_world_id AND release.id IS NULL
        GROUP BY reservation.world_id, reservation.source_balance_id, balance.quantity_units
        HAVING SUM(reservation.quantity_units) > MAX(balance.quantity_units)
    ) THEN
        RAISE EXCEPTION 'city open-world V15 active reservations exceed inventory' USING ERRCODE = '23514';
    END IF;
    IF active_orders <> (
        SELECT COUNT(*)
        FROM (
            SELECT DISTINCT ON (world_id, order_code) world_id, order_code, state
            FROM city_open_world_supply_chain_order_transitions
            WHERE world_id = target_world_id
            ORDER BY world_id, order_code, transition_tick DESC, transition_sequence DESC
        ) latest WHERE latest.state IN ('proposed', 'accepted', 'dispatched')
    ) THEN
        RAISE EXCEPTION 'city open-world V15 active order counter is inconsistent' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_open_world_supply_chain_foundation_commit()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    PERFORM assert_city_open_world_supply_chain_foundation(target_world_id);
    RETURN NULL;
END;
$$;

DO $$
DECLARE table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_open_world_supply_chain_profiles',
        'city_open_world_supply_chain_nodes',
        'city_open_world_supply_chain_facts',
        'city_open_world_supply_chain_orders',
        'city_open_world_supply_chain_order_lines',
        'city_open_world_supply_chain_order_transitions',
        'city_open_world_supply_chain_reservations',
        'city_open_world_supply_chain_reservation_releases',
        'city_open_world_supply_chain_dispatches',
        'city_open_world_supply_chain_deliveries',
        'city_open_world_supply_chain_settlements',
        'city_inventory_balances'
    ] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', 'city_open_world_supply_chain_' || table_name || '_commit_check', table_name);
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_city_open_world_supply_chain_foundation_commit()', 'city_open_world_supply_chain_' || table_name || '_commit_check', table_name);
    END LOOP;
END;
$$;

-- V15 is a strict successor. These gates only widen predecessor bootstrap and
-- runtime version predicates; they do not make any predecessor projection
-- mutable outside its original scoped write contract.
CREATE OR REPLACE FUNCTION city_open_world_commute_lifecycle_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_lifecycle_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v14','city-openworld-v15')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_lifecycle_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_lifecycle_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v14','city-openworld-v15'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_source_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_source_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v13','city-openworld-v14','city-openworld-v15')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_source_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_source_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v13','city-openworld-v14','city-openworld-v15'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_commute_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_commute_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15')
                   AND world.state_hash IS NULL
                   AND (world.current_tick = 0 OR EXISTS (SELECT 1 FROM city_world_upgrade_runs upgrade
                        WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                          AND upgrade.world_id = target_world_id AND upgrade.to_version = world.simulation_version
                          AND upgrade.status = 'running')))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15')
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
                   AND world.simulation_version IN ('city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_arrival_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_arrival_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15')
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
                   AND world.simulation_version IN ('city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_mobility_od_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_mobility_od_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15')
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
                   AND world.simulation_version IN ('city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_service_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15')
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
                   AND world.simulation_version IN ('city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_impact_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15')
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
                   AND world.simulation_version IN ('city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15'))
$$;

CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v1','city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_materialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_materialization_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v2','city-openworld-v3','city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15')
                   AND world.current_tick = 0 AND world.state_hash IS NULL)
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_fact_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (SELECT 1 FROM city_worlds world WHERE world.id = target_world_id
                   AND world.simulation_version IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15')
                   AND world.current_tick >= 0 AND world.state_hash IS NOT NULL)
$$;

-- The scoped write predicate above is only one half of the runtime-fact
-- boundary.  The insert trigger also owns the exact next-tick draft rule and
-- must recognize V15, otherwise automatic V11+ OD facts make every V15 step
-- fail before the supply-chain stage can run.
CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_fact_insert()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE world_tick BIGINT; world_version VARCHAR(32); command_status_value VARCHAR(16);
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN RETURN NEW; END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version FROM city_worlds WHERE id = NEW.world_id;
    IF world_version NOT IN ('city-openworld-v4','city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15')
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
       AND NOT (world_version IN ('city-openworld-v5','city-openworld-v6','city-openworld-v7','city-openworld-v8','city-openworld-v9','city-openworld-v10','city-openworld-v11','city-openworld-v12','city-openworld-v13','city-openworld-v14','city-openworld-v15') AND NEW.fact_type LIKE 'system.%') THEN
        RAISE EXCEPTION 'derived open-world runtime fact requires a parent fact' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

-- A V15 delivery is the one additional command-backed resource transfer that
-- the generic F3 resource fact boundary permits.  Do not simply whitelist the
-- command type: require the already-drafted V15 delivery fact, its lifecycle
-- transition, and the seller node identity before the resource operation can
-- be sealed.  The delivery projection itself is inserted later in the same
-- transaction because it has a foreign key to the sealed operation.
CREATE OR REPLACE FUNCTION city_open_world_supply_delivery_resource_operation_authorized(target_operation_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT EXISTS (
        SELECT 1
        FROM city_resource_operations operation
        JOIN city_worlds world
          ON world.id = operation.world_id
        JOIN city_commands command
          ON command.id = operation.source_command_id
         AND command.world_id = operation.world_id
        JOIN city_open_world_supply_chain_facts fact
          ON fact.world_id = operation.world_id
         AND fact.source_command_id = operation.source_command_id
         AND fact.fact_type = 'order.delivered'
         AND fact.tick = operation.tick
        JOIN city_open_world_supply_chain_orders supply_order
          ON supply_order.world_id = fact.world_id
         AND supply_order.code = fact.order_code
        JOIN city_open_world_supply_chain_nodes seller
          ON seller.world_id = supply_order.world_id
         AND seller.code = supply_order.seller_node_code
        JOIN city_open_world_supply_chain_order_transitions transition
          ON transition.world_id = fact.world_id
         AND transition.order_code = fact.order_code
         AND transition.source_fact_id = fact.id
         AND transition.state = 'delivered'
        WHERE operation.id = target_operation_id
          AND world.simulation_version = 'city-openworld-v15'
          AND operation.operation_type = 'transfer'
          AND command.command_type = 'open_world.supply_order.deliver'
          AND operation.actor_entity_id = seller.firm_entity_id
          AND operation.district_id = seller.district_id
    )
$$;

-- The resource-operation verifier predates F10.0 and owns balance
-- conservation, posting immutability and standard command typing.  Replace it
-- explicitly rather than bypassing its source-command rule from Go; V15 adds
-- only the fact-backed delivery exception above while retaining the prior F8
-- facility-operation exception and every F3 conservation check.
CREATE OR REPLACE FUNCTION assert_city_resource_operation_ready(target_operation_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
    target_tick BIGINT;
    target_type VARCHAR(24);
    target_actor BIGINT;
    target_district BIGINT;
    target_recipe BIGINT;
    target_batches BIGINT;
    target_source_command BIGINT;
    entry_count BIGINT;
    distinct_resources BIGINT;
    incoming_units NUMERIC;
    outgoing_units NUMERIC;
    invalid_lines BOOLEAN;
    capacity_limit BIGINT;
    capacity_used NUMERIC;
    expected_command_type VARCHAR(64);
    required_command_type VARCHAR(64);
BEGIN
    SELECT world_id, tick, operation_type, actor_entity_id, district_id, recipe_id, batch_count,
           source_command_id
    INTO target_world_id, target_tick, target_type, target_actor, target_district,
         target_recipe, target_batches, target_source_command
    FROM city_resource_operations
    WHERE id = target_operation_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'city resource operation % does not exist', target_operation_id
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*), COUNT(DISTINCT resource_id),
           COALESCE(SUM(quantity_units) FILTER (WHERE direction = 'in'), 0),
           COALESCE(SUM(quantity_units) FILTER (WHERE direction = 'out'), 0)
    INTO entry_count, distinct_resources, incoming_units, outgoing_units
    FROM city_resource_entries
    WHERE operation_id = target_operation_id;

    IF entry_count = 0 THEN
        RAISE EXCEPTION 'city resource operation has no entries' USING ERRCODE = '23514';
    END IF;

    IF target_type <> 'opening' AND target_source_command IS NOT NULL THEN
        IF target_type = 'transfer' THEN
            required_command_type := 'resource.transfer';
        ELSIF target_type = 'consumption' THEN
            required_command_type := 'resource.consume';
        ELSIF target_type = 'production' THEN
            required_command_type := 'resource.produce';
        ELSE
            required_command_type := NULL;
        END IF;
        SELECT command_type INTO expected_command_type
        FROM city_commands
        WHERE id = target_source_command AND world_id = target_world_id;
        IF expected_command_type IS DISTINCT FROM required_command_type
           AND NOT (target_type = 'consumption' AND expected_command_type = 'facility.operation.start')
           AND NOT city_open_world_supply_delivery_resource_operation_authorized(target_operation_id) THEN
            RAISE EXCEPTION 'city resource operation does not match its source command'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    IF target_type <> 'transfer' AND EXISTS (
        SELECT 1
        FROM city_resource_entries entry
        JOIN city_inventory_balances balance ON balance.id = entry.balance_id
        WHERE entry.operation_id = target_operation_id
          AND (balance.entity_id <> target_actor OR balance.district_id <> target_district)
    ) THEN
        RAISE EXCEPTION 'city resource operation entries do not belong to the actor and district'
            USING ERRCODE = '23514';
    END IF;

    IF target_type = 'opening' THEN
        IF EXISTS (
            SELECT 1 FROM city_resource_entries
            WHERE operation_id = target_operation_id AND direction <> 'in'
        ) OR EXISTS (
            SELECT 1
            FROM city_resource_entries entry
            JOIN city_inventory_balances balance ON balance.id = entry.balance_id
            WHERE entry.operation_id = target_operation_id
              AND (
                  entry.quantity_units <> balance.opening_quantity_units
                  OR entry.quantity_before_units <> 0
                  OR entry.balance_version_before <> 0
              )
        ) OR entry_count <> (
            SELECT COUNT(*)
            FROM city_inventory_balances balance
            WHERE balance.world_id = target_world_id
              AND balance.entity_id = target_actor
              AND balance.district_id = target_district
              AND balance.opening_quantity_units > 0
        ) THEN
            RAISE EXCEPTION 'city opening resource operation must exactly post configured opening inventory'
                USING ERRCODE = '23514';
        END IF;
    ELSIF target_type = 'transfer' THEN
        IF entry_count <> 2 OR distinct_resources <> 1 OR incoming_units <> outgoing_units
           OR EXISTS (
               SELECT 1
               FROM city_resource_entries entry
               JOIN city_inventory_balances balance ON balance.id = entry.balance_id
               WHERE entry.operation_id = target_operation_id
                 AND entry.direction = 'out'
                 AND (balance.entity_id <> target_actor OR balance.district_id <> target_district)
           ) THEN
            RAISE EXCEPTION 'city resource transfer is not conserved'
                USING ERRCODE = '23514';
        END IF;
    ELSIF target_type = 'consumption' THEN
        IF entry_count <> 1 OR incoming_units <> 0 OR outgoing_units <= 0 THEN
            RAISE EXCEPTION 'city resource consumption must contain one explicit sink entry'
                USING ERRCODE = '23514';
        END IF;
    ELSIF target_type = 'production' THEN
        IF NOT EXISTS (
            SELECT 1
            FROM city_firm_recipes firm_recipe
            JOIN city_firm_states firm
              ON firm.entity_id = firm_recipe.firm_entity_id
             AND firm.world_id = firm_recipe.world_id
            WHERE firm_recipe.world_id = target_world_id
              AND firm_recipe.firm_entity_id = target_actor
              AND firm_recipe.recipe_id = target_recipe
              AND firm_recipe.status = 'active'
              AND firm.district_id = target_district
        ) THEN
            RAISE EXCEPTION 'city production recipe is not granted to the firm and district'
                USING ERRCODE = '23514';
        END IF;
        WITH expected AS (
            SELECT line.resource_id,
                   CASE line.direction WHEN 'input' THEN 'out' ELSE 'in' END AS direction,
                   line.quantity_units::numeric * target_batches::numeric AS quantity_units
            FROM city_production_recipe_lines line
            WHERE line.recipe_id = target_recipe
        ), actual AS (
            SELECT resource_id, direction, SUM(quantity_units)::numeric AS quantity_units
            FROM city_resource_entries
            WHERE operation_id = target_operation_id
            GROUP BY resource_id, direction
        )
        SELECT EXISTS (
            SELECT 1
            FROM expected
            FULL OUTER JOIN actual USING (resource_id, direction)
            WHERE expected.quantity_units IS NULL
               OR actual.quantity_units IS NULL
               OR expected.quantity_units <> actual.quantity_units
        ) INTO invalid_lines;

        IF invalid_lines OR NOT EXISTS (
            SELECT 1 FROM city_production_recipe_lines
            WHERE recipe_id = target_recipe AND direction = 'input'
        ) OR NOT EXISTS (
            SELECT 1 FROM city_production_recipe_lines
            WHERE recipe_id = target_recipe AND direction = 'output'
        ) THEN
            RAISE EXCEPTION 'city production entries do not match the fixed recipe'
                USING ERRCODE = '23514';
        END IF;

        SELECT production_capacity_units INTO capacity_limit
        FROM city_firm_states
        WHERE world_id = target_world_id AND entity_id = target_actor
        FOR UPDATE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'city production actor is not an active firm state'
                USING ERRCODE = '23514';
        END IF;

        SELECT COALESCE(SUM(operation.batch_count::numeric * recipe.capacity_units_per_batch::numeric), 0)
        INTO capacity_used
        FROM city_resource_operations operation
        JOIN city_production_recipes recipe ON recipe.id = operation.recipe_id
        WHERE operation.world_id = target_world_id
          AND operation.tick = target_tick
          AND operation.actor_entity_id = target_actor
          AND operation.operation_type = 'production'
          AND (operation.id = target_operation_id OR operation.posted_at IS NOT NULL);
        IF capacity_used > capacity_limit::numeric THEN
            RAISE EXCEPTION 'city production capacity exceeded' USING ERRCODE = '23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'unsupported city resource operation type' USING ERRCODE = '23514';
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
    IF world_version = 'city-openworld-v14' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-commute-lifecycle-catalog' AND binding.bundle_version = '1.0.0'
    ) THEN
        RAISE EXCEPTION 'city open-world V14 commute lifecycle version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v15' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-supply-chain-catalog' AND binding.bundle_version = '1.0.0'
    ) THEN
        RAISE EXCEPTION 'city open-world V15 supply-chain version vector is incomplete' USING ERRCODE = '23514';
    END IF;
END;
$$;
