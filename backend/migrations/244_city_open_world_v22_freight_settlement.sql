-- city-openworld-v22 / F10.3: future-only partial freight settlement. V22
-- observes post-baseline V17/V18 custody evidence and settles the original
-- V15 order through an explicit successor; historic atomic-delivery evidence
-- is never rewritten or backfilled.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
SELECT
    'city-openworld-v22',
    'supported',
    'city-state-v1+gzip',
    COALESCE(
        (SELECT capabilities FROM city_engine_versions WHERE version = 'city-openworld-v21'),
        '[]'::jsonb
    ) || '["partial_freight_settlement","line_outcome_receipts","freight_loss_refunds","carrier_liability_claims"]'::jsonb
ON CONFLICT (version) DO UPDATE
SET status = EXCLUDED.status,
    canonical_format = EXCLUDED.canonical_format,
    capabilities = EXCLUDED.capabilities;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v21', 'city-openworld-v22', 'openworld_v21_to_v22')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_freight_settlement_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_effective_capacity_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    source_contract VARCHAR(96) NOT NULL,
    receipt_contract VARCHAR(96) NOT NULL,
    resource_contract VARCHAR(96) NOT NULL,
    financial_contract VARCHAR(96) NOT NULL,
    liability_contract VARCHAR(96) NOT NULL,
    maximum_orders INTEGER NOT NULL CHECK (maximum_orders BETWEEN 1 AND 1000000),
    maximum_cases_per_order INTEGER NOT NULL CHECK (maximum_cases_per_order BETWEEN 1 AND 1024),
    maximum_receipts_per_case INTEGER NOT NULL CHECK (maximum_receipts_per_case BETWEEN 1 AND 1024),
    maximum_receipts_per_tick INTEGER NOT NULL CHECK (maximum_receipts_per_tick BETWEEN 1 AND 1000000),
    order_count BIGINT NOT NULL DEFAULT 0 CHECK (order_count >= 0),
    case_count BIGINT NOT NULL DEFAULT 0 CHECK (case_count >= 0),
    receipt_count BIGINT NOT NULL DEFAULT 0 CHECK (receipt_count >= 0),
    claim_count BIGINT NOT NULL DEFAULT 0 CHECK (claim_count >= 0),
    accepted_units BIGINT NOT NULL DEFAULT 0 CHECK (accepted_units >= 0),
    lost_units BIGINT NOT NULL DEFAULT 0 CHECK (lost_units >= 0),
    rejected_units BIGINT NOT NULL DEFAULT 0 CHECK (rejected_units >= 0),
    refunded_units BIGINT NOT NULL DEFAULT 0 CHECK (refunded_units >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_freight_settlement_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-freight-settlement'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND source_contract = 'v17_shipment_or_v18_consignment_v1'
        AND receipt_contract = 'append_only_line_quantity_outcome_v1'
        AND resource_contract = 'immediate_partial_transfer_loss_consumption_v1'
        AND financial_contract = 'accepted_price_refund_and_carrier_claim_v1'
        AND liability_contract = 'seller_refund_or_carrier_claim_v1'
        AND maximum_orders = 10000
        AND maximum_cases_per_order = 128
        AND maximum_receipts_per_case = 128
        AND maximum_receipts_per_tick = 256
        AND order_count <= maximum_orders
        AND revision >= 1
    ),
    CONSTRAINT city_open_world_freight_settlement_profile_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'scope' = 'post_baseline_v17_v18_partial_freight_settlement'
        AND metadata->>'historical_behavior' = 'pre_baseline_sources_untracked'
        AND metadata->>'delivery_behavior' = 'v15_settled_successor_not_atomic_delivery'
    )
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_settlement_orders (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_freight_settlement_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    source_kind VARCHAR(24) NOT NULL,
    source_code VARCHAR(160) NOT NULL,
    order_code VARCHAR(160) NOT NULL,
    source_tick BIGINT NOT NULL CHECK (source_tick > 0),
    state VARCHAR(24) NOT NULL,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_freight_settlement_order_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND source_kind IN ('shipment','consignment')
        AND source_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND order_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND state IN ('awaiting_transport','receiving','settled','voided','blocked')
    ),
    CONSTRAINT city_open_world_freight_settlement_order_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_freight_settlement_order_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_freight_settlement_order_source_unique UNIQUE (world_id, source_kind, source_code)
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_settlement_cases (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_freight_settlement_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    settlement_order_code VARCHAR(160) NOT NULL,
    source_kind VARCHAR(24) NOT NULL,
    source_code VARCHAR(160) NOT NULL,
    transport_state VARCHAR(24) NOT NULL,
    state VARCHAR(24) NOT NULL,
    source_tick BIGINT NOT NULL CHECK (source_tick > 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_freight_settlement_case_order_fk
        FOREIGN KEY (world_id, settlement_order_code)
        REFERENCES city_open_world_freight_settlement_orders(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_settlement_case_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND source_kind IN ('shipment','consignment')
        AND source_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND transport_state IN ('awaiting_route','in_transit','awaiting_receipt','expired','voided','orphaned')
        AND state IN ('awaiting_outcome','receiving','settled')
    ),
    CONSTRAINT city_open_world_freight_settlement_case_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_freight_settlement_case_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_freight_settlement_case_source_unique UNIQUE (world_id, source_kind, source_code)
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_settlement_case_lines (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL,
    case_code VARCHAR(160) NOT NULL,
    source_line_no INTEGER NOT NULL CHECK (source_line_no > 0),
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
    CONSTRAINT city_open_world_freight_settlement_case_line_case_fk
        FOREIGN KEY (world_id, case_code)
        REFERENCES city_open_world_freight_settlement_cases(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_settlement_case_line_identity_check CHECK (
        resource_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND source_firm_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND source_district_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND destination_firm_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND destination_district_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND quantity_units::NUMERIC * unit_price_units::NUMERIC = total_price_units::NUMERIC
    ),
    CONSTRAINT city_open_world_freight_settlement_case_line_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_freight_settlement_case_line_unique UNIQUE (world_id, case_code, source_line_no)
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_settlement_receipts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_freight_settlement_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    case_code VARCHAR(160) NOT NULL,
    receipt_tick BIGINT NOT NULL CHECK (receipt_tick > 0),
    source_command_id BIGINT NOT NULL,
    liability_party VARCHAR(24) NOT NULL,
    refunded_units BIGINT NOT NULL DEFAULT 0 CHECK (refunded_units >= 0),
    resource_operation_id BIGINT,
    journal_id BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_freight_settlement_receipt_case_fk
        FOREIGN KEY (world_id, case_code)
        REFERENCES city_open_world_freight_settlement_cases(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_settlement_receipt_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_settlement_receipt_operation_fk
        FOREIGN KEY (resource_operation_id, world_id)
        REFERENCES city_resource_operations(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_settlement_receipt_journal_fk
        FOREIGN KEY (journal_id, world_id)
        REFERENCES city_journals(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_settlement_receipt_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND liability_party IN ('seller','carrier')
        AND ((refunded_units = 0 AND journal_id IS NULL) OR (refunded_units > 0 AND journal_id IS NOT NULL))
    ),
    CONSTRAINT city_open_world_freight_settlement_receipt_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_freight_settlement_receipt_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_freight_settlement_receipt_command_unique UNIQUE (world_id, source_command_id)
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_settlement_receipt_lines (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL,
    receipt_code VARCHAR(160) NOT NULL,
    case_code VARCHAR(160) NOT NULL,
    source_line_no INTEGER NOT NULL CHECK (source_line_no > 0),
    accepted_units BIGINT NOT NULL DEFAULT 0 CHECK (accepted_units >= 0),
    lost_units BIGINT NOT NULL DEFAULT 0 CHECK (lost_units >= 0),
    rejected_units BIGINT NOT NULL DEFAULT 0 CHECK (rejected_units >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_freight_settlement_receipt_line_receipt_fk
        FOREIGN KEY (world_id, receipt_code)
        REFERENCES city_open_world_freight_settlement_receipts(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_settlement_receipt_line_case_line_fk
        FOREIGN KEY (world_id, case_code, source_line_no)
        REFERENCES city_open_world_freight_settlement_case_lines(world_id, case_code, source_line_no) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_settlement_receipt_line_identity_check CHECK (
        accepted_units + lost_units + rejected_units > 0
    ),
    CONSTRAINT city_open_world_freight_settlement_receipt_line_unique UNIQUE (world_id, receipt_code, source_line_no)
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_settlement_claims (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_freight_settlement_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    receipt_code VARCHAR(160) NOT NULL,
    case_code VARCHAR(160) NOT NULL,
    liability_party VARCHAR(24) NOT NULL,
    claim_amount BIGINT NOT NULL CHECK (claim_amount > 0),
    state VARCHAR(24) NOT NULL,
    created_tick BIGINT NOT NULL CHECK (created_tick > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_freight_settlement_claim_receipt_fk
        FOREIGN KEY (world_id, receipt_code)
        REFERENCES city_open_world_freight_settlement_receipts(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_settlement_claim_case_fk
        FOREIGN KEY (world_id, case_code)
        REFERENCES city_open_world_freight_settlement_cases(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_settlement_claim_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND liability_party = 'carrier'
        AND state IN ('open','resolved')
    ),
    CONSTRAINT city_open_world_freight_settlement_claim_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_freight_settlement_claim_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_freight_settlement_claim_receipt_unique UNIQUE (world_id, receipt_code)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_freight_settlement_orders_state
    ON city_open_world_freight_settlement_orders (world_id, state, source_tick, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_freight_settlement_cases_state
    ON city_open_world_freight_settlement_cases (world_id, state, transport_state, source_tick, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_freight_settlement_receipts_case
    ON city_open_world_freight_settlement_receipts (world_id, case_code, receipt_tick, id);

CREATE OR REPLACE FUNCTION city_open_world_freight_settlement_recovery_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_recovery_write_enabled(target_world_id)
       OR COALESCE(current_setting('sub2api.city_open_world_freight_settlement_recovery_world_id', TRUE), '') = target_world_id::TEXT
$$;

CREATE OR REPLACE FUNCTION city_open_world_freight_settlement_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_freight_settlement_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v22'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v22'
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_freight_settlement_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_freight_settlement_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v22'
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_freight_settlement_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_freight_settlement_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_freight_settlement_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_freight_settlement_write_enabled(target_world_id)
       AND NEW.order_count >= OLD.order_count AND NEW.case_count >= OLD.case_count
       AND NEW.receipt_count >= OLD.receipt_count AND NEW.claim_count >= OLD.claim_count
       AND NEW.accepted_units >= OLD.accepted_units AND NEW.lost_units >= OLD.lost_units
       AND NEW.rejected_units >= OLD.rejected_units AND NEW.refunded_units >= OLD.refunded_units
       AND NEW.revision = OLD.revision + 1 THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V22 freight-settlement profile requires audited bootstrap, write, or recovery context'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_freight_settlement_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_freight_settlement_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND (city_open_world_freight_settlement_bootstrap_write_enabled(target_world_id)
                             OR city_open_world_freight_settlement_write_enabled(target_world_id)) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_freight_settlement_write_enabled(target_world_id)
       AND TG_TABLE_NAME IN ('city_open_world_freight_settlement_orders','city_open_world_freight_settlement_cases','city_open_world_freight_settlement_claims') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V22 freight-settlement projections are append-only audited evidence'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS guard_city_open_world_freight_settlement_profile_write ON city_open_world_freight_settlement_profiles;
CREATE TRIGGER guard_city_open_world_freight_settlement_profile_write
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_freight_settlement_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_freight_settlement_profile();

DO $$
DECLARE table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_open_world_freight_settlement_orders',
        'city_open_world_freight_settlement_cases',
        'city_open_world_freight_settlement_case_lines',
        'city_open_world_freight_settlement_receipts',
        'city_open_world_freight_settlement_receipt_lines',
        'city_open_world_freight_settlement_claims'
    ] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS guard_%I_write ON %I', table_name, table_name);
        EXECUTE format('CREATE TRIGGER guard_%I_write BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_freight_settlement_projection()', table_name, table_name);
    END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_freight_settlement_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version TEXT;
    world_tick BIGINT;
    profile_tick BIGINT;
    profile_orders BIGINT;
    profile_cases BIGINT;
    profile_receipts BIGINT;
    profile_claims BIGINT;
    profile_accepted BIGINT;
    profile_lost BIGINT;
    profile_rejected BIGINT;
    profile_refunded BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v22' THEN RETURN; END IF;
    SELECT baseline_tick, order_count, case_count, receipt_count, claim_count,
           accepted_units, lost_units, rejected_units, refunded_units
      INTO profile_tick, profile_orders, profile_cases, profile_receipts, profile_claims,
           profile_accepted, profile_lost, profile_rejected, profile_refunded
    FROM city_open_world_freight_settlement_profiles
    WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick
       OR profile_orders <> (SELECT COUNT(*) FROM city_open_world_freight_settlement_orders WHERE world_id = target_world_id)
       OR profile_cases <> (SELECT COUNT(*) FROM city_open_world_freight_settlement_cases WHERE world_id = target_world_id)
       OR profile_receipts <> (SELECT COUNT(*) FROM city_open_world_freight_settlement_receipts WHERE world_id = target_world_id)
       OR profile_claims <> (SELECT COUNT(*) FROM city_open_world_freight_settlement_claims WHERE world_id = target_world_id)
       OR profile_accepted <> COALESCE((SELECT SUM(accepted_units) FROM city_open_world_freight_settlement_receipt_lines WHERE world_id = target_world_id), 0)
       OR profile_lost <> COALESCE((SELECT SUM(lost_units) FROM city_open_world_freight_settlement_receipt_lines WHERE world_id = target_world_id), 0)
       OR profile_rejected <> COALESCE((SELECT SUM(rejected_units) FROM city_open_world_freight_settlement_receipt_lines WHERE world_id = target_world_id), 0)
       OR profile_refunded <> COALESCE((SELECT SUM(refunded_units) FROM city_open_world_freight_settlement_receipts WHERE world_id = target_world_id), 0) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement profile is missing or inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_orders settlement_order
        LEFT JOIN city_open_world_freight_settlement_cases settlement_case
          ON settlement_case.world_id = settlement_order.world_id
         AND settlement_case.settlement_order_code = settlement_order.code
        WHERE settlement_order.world_id = target_world_id
          AND (settlement_order.source_tick <= profile_tick
               OR NOT EXISTS (
                   SELECT 1 FROM city_open_world_freight_settlement_cases item
                   WHERE item.world_id = settlement_order.world_id
                     AND item.settlement_order_code = settlement_order.code
               )
               OR ((settlement_order.state = 'settled') IS DISTINCT FROM
                   ((SELECT COUNT(*) FROM city_open_world_freight_settlement_cases item
                     WHERE item.world_id = settlement_order.world_id
                       AND item.settlement_order_code = settlement_order.code
                       AND item.state = 'settled') =
                    (SELECT COUNT(*) FROM city_open_world_freight_settlement_cases item
                     WHERE item.world_id = settlement_order.world_id
                       AND item.settlement_order_code = settlement_order.code)))
               OR (settlement_case.code IS NOT NULL AND settlement_case.source_tick <> settlement_order.source_tick))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement order linkage is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_receipt_lines line
        JOIN city_open_world_freight_settlement_case_lines case_line
          ON case_line.world_id = line.world_id AND case_line.case_code = line.case_code AND case_line.source_line_no = line.source_line_no
        WHERE line.world_id = target_world_id
        GROUP BY line.world_id, line.case_code, line.source_line_no, case_line.quantity_units
        HAVING SUM(line.accepted_units + line.lost_units + line.rejected_units) > MAX(case_line.quantity_units)
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement receipt line exceeds the source quantity' USING ERRCODE = '23514';
    END IF;
END;
$$;

-- New V22 worlds still execute every V15-V21 initializer. Extend the exact
-- predecessor gate lists instead of weakening the predecessor contracts.
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
        definition := replace(definition, $old$'city-openworld-v21'$old$, $new$'city-openworld-v21','city-openworld-v22'$new$);
        IF position($needle$city-openworld-v22$needle$ IN definition) = 0 THEN
            RAISE EXCEPTION 'cannot extend V22 predecessor write gate %', target_function USING ERRCODE = '23514';
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
        $old$'city-openworld-v19','city-openworld-v20','city-openworld-v21'$old$,
        $new$'city-openworld-v19','city-openworld-v20','city-openworld-v21','city-openworld-v22'$new$);
    IF position($needle$city-openworld-v22$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V19 spatial-network bootstrap gate to V22' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;

ALTER TABLE city_open_world_enterprise_freight_receipt_profiles
    ADD COLUMN IF NOT EXISTS settled_count BIGINT NOT NULL DEFAULT 0 CHECK (settled_count >= 0);
ALTER TABLE city_open_world_freight_batch_profiles
    ADD COLUMN IF NOT EXISTS settled_count BIGINT NOT NULL DEFAULT 0 CHECK (settled_count >= 0);

-- V17/V18's original assertions intentionally return early outside their own
-- sealed engine versions.  V22 owns the successor writes, so its assertion
-- rechecks the relevant predecessor counters, custody links, and terminal
-- state relationship in one transaction instead of weakening historical
-- assertion semantics for older worlds.
CREATE OR REPLACE FUNCTION assert_city_open_world_freight_settlement_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version TEXT;
    world_tick BIGINT;
    profile_tick BIGINT;
    profile_orders BIGINT;
    profile_cases BIGINT;
    profile_receipts BIGINT;
    profile_claims BIGINT;
    profile_accepted BIGINT;
    profile_lost BIGINT;
    profile_rejected BIGINT;
    profile_refunded BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v22' THEN RETURN; END IF;
    IF NOT EXISTS (SELECT 1 FROM city_open_world_supply_chain_profiles WHERE world_id = target_world_id)
       OR NOT EXISTS (SELECT 1 FROM city_open_world_enterprise_freight_receipt_profiles WHERE world_id = target_world_id)
       OR NOT EXISTS (SELECT 1 FROM city_open_world_freight_batch_profiles WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement predecessor foundations are missing' USING ERRCODE = '23514';
    END IF;
    SELECT baseline_tick, order_count, case_count, receipt_count, claim_count,
           accepted_units, lost_units, rejected_units, refunded_units
      INTO profile_tick, profile_orders, profile_cases, profile_receipts, profile_claims,
           profile_accepted, profile_lost, profile_rejected, profile_refunded
    FROM city_open_world_freight_settlement_profiles
    WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick
       OR profile_orders <> (SELECT COUNT(*) FROM city_open_world_freight_settlement_orders WHERE world_id = target_world_id)
       OR profile_cases <> (SELECT COUNT(*) FROM city_open_world_freight_settlement_cases WHERE world_id = target_world_id)
       OR profile_receipts <> (SELECT COUNT(*) FROM city_open_world_freight_settlement_receipts WHERE world_id = target_world_id)
       OR profile_claims <> (SELECT COUNT(*) FROM city_open_world_freight_settlement_claims WHERE world_id = target_world_id)
       OR profile_accepted <> COALESCE((SELECT SUM(accepted_units) FROM city_open_world_freight_settlement_receipt_lines WHERE world_id = target_world_id), 0)
       OR profile_lost <> COALESCE((SELECT SUM(lost_units) FROM city_open_world_freight_settlement_receipt_lines WHERE world_id = target_world_id), 0)
       OR profile_rejected <> COALESCE((SELECT SUM(rejected_units) FROM city_open_world_freight_settlement_receipt_lines WHERE world_id = target_world_id), 0)
       OR profile_refunded <> COALESCE((SELECT SUM(refunded_units) FROM city_open_world_freight_settlement_receipts WHERE world_id = target_world_id), 0) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement profile is missing or inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_orders settlement_order
        LEFT JOIN city_open_world_freight_settlement_cases settlement_case
          ON settlement_case.world_id = settlement_order.world_id
         AND settlement_case.settlement_order_code = settlement_order.code
        WHERE settlement_order.world_id = target_world_id
          AND (settlement_order.source_tick <= profile_tick
               OR NOT EXISTS (
                   SELECT 1 FROM city_open_world_freight_settlement_cases item
                   WHERE item.world_id = settlement_order.world_id
                     AND item.settlement_order_code = settlement_order.code
               )
               OR ((settlement_order.state = 'settled') IS DISTINCT FROM
                   ((SELECT COUNT(*) FROM city_open_world_freight_settlement_cases item
                     WHERE item.world_id = settlement_order.world_id
                       AND item.settlement_order_code = settlement_order.code
                       AND item.state = 'settled') =
                    (SELECT COUNT(*) FROM city_open_world_freight_settlement_cases item
                     WHERE item.world_id = settlement_order.world_id
                       AND item.settlement_order_code = settlement_order.code)))
               OR (settlement_case.code IS NOT NULL AND settlement_case.source_tick <> settlement_order.source_tick))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement order linkage is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_receipt_lines line
        JOIN city_open_world_freight_settlement_case_lines case_line
          ON case_line.world_id = line.world_id AND case_line.case_code = line.case_code AND case_line.source_line_no = line.source_line_no
        WHERE line.world_id = target_world_id
        GROUP BY line.world_id, line.case_code, line.source_line_no, case_line.quantity_units
        HAVING SUM(line.accepted_units + line.lost_units + line.rejected_units) > MAX(case_line.quantity_units)
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement receipt line exceeds the source quantity' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_cases settlement_case
        JOIN city_open_world_freight_settlement_case_lines case_line
          ON case_line.world_id = settlement_case.world_id AND case_line.case_code = settlement_case.code
        LEFT JOIN (
            SELECT world_id, case_code, source_line_no,
                   SUM(accepted_units + lost_units + rejected_units) AS resolved_units
            FROM city_open_world_freight_settlement_receipt_lines
            WHERE world_id = target_world_id
            GROUP BY world_id, case_code, source_line_no
        ) outcomes
          ON outcomes.world_id = case_line.world_id AND outcomes.case_code = case_line.case_code
         AND outcomes.source_line_no = case_line.source_line_no
        WHERE settlement_case.world_id = target_world_id
        GROUP BY settlement_case.world_id, settlement_case.code, settlement_case.state
        HAVING (MAX(settlement_case.state) = 'settled') <> BOOL_AND(COALESCE(outcomes.resolved_units, 0) = case_line.quantity_units)
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement case outcomes are incomplete' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_cases settlement_case
        JOIN city_open_world_freight_settlement_orders settlement_order
          ON settlement_order.world_id = settlement_case.world_id
         AND settlement_order.code = settlement_case.settlement_order_code
        LEFT JOIN city_open_world_enterprise_freight_shipments shipment
          ON shipment.world_id = settlement_case.world_id
         AND shipment.code = settlement_case.source_code
         AND settlement_case.source_kind = 'shipment'
        LEFT JOIN city_open_world_freight_batch_consignments consignment
          ON consignment.world_id = settlement_case.world_id
         AND consignment.code = settlement_case.source_code
         AND settlement_case.source_kind = 'consignment'
        LEFT JOIN city_open_world_freight_batch_plans plan
          ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
        WHERE settlement_case.world_id = target_world_id
          AND ((settlement_case.source_kind = 'shipment' AND
                (shipment.code IS NULL OR settlement_order.source_code <> settlement_case.source_code
                 OR shipment.order_code <> settlement_order.order_code
                 OR (settlement_case.state = 'settled' AND shipment.state <> 'settled')
                 OR (settlement_case.state <> 'settled' AND shipment.state <> settlement_case.transport_state)))
               OR (settlement_case.source_kind = 'consignment' AND
                (consignment.code IS NULL OR plan.code <> settlement_order.source_code
                 OR plan.order_code <> settlement_order.order_code
                 OR (settlement_case.state = 'settled' AND consignment.state <> 'settled')
                 OR (settlement_case.state <> 'settled' AND consignment.state <> settlement_case.transport_state))))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement custody linkage is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_orders settlement_order
        JOIN LATERAL (
            SELECT transition.state
            FROM city_open_world_supply_chain_order_transitions transition
            WHERE transition.world_id = settlement_order.world_id AND transition.order_code = settlement_order.order_code
            ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
            LIMIT 1
        ) supply_state ON TRUE
        WHERE settlement_order.world_id = target_world_id
          AND ((settlement_order.state = 'settled' AND supply_state.state <> 'settled')
               OR (settlement_order.state <> 'settled' AND supply_state.state <> 'dispatched')
               OR EXISTS (
                    SELECT 1 FROM city_open_world_supply_chain_deliveries delivery
                    WHERE delivery.world_id = settlement_order.world_id AND delivery.order_code = settlement_order.order_code
               ))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 settlement must own the V15 terminal state' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_orders settlement_order
        WHERE settlement_order.world_id = target_world_id
          AND settlement_order.source_kind = 'consignment'
          AND settlement_order.state = 'settled'
          AND ((SELECT COUNT(*) FROM city_open_world_freight_settlement_cases settlement_case
                WHERE settlement_case.world_id = settlement_order.world_id
                  AND settlement_case.settlement_order_code = settlement_order.code)
               <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments consignment
                   WHERE consignment.world_id = settlement_order.world_id
                     AND consignment.plan_code = settlement_order.source_code))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 batch settlement omitted a consignment' USING ERRCODE = '23514';
    END IF;
    -- V17 can point its last fact at an external supply-chain settlement
    -- proof.  V18 cannot: its last_runtime_fact_id remains the mobility fact
    -- that made the consignment resolvable, while the settlement transition
    -- carries the external proof.  Validate both adapters explicitly so a
    -- successor state cannot be forged by merely flipping a status column.
    IF EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_shipments shipment
        LEFT JOIN city_open_world_enterprise_freight_receipt_facts last_fact
          ON last_fact.id = shipment.last_receipt_fact_id AND last_fact.world_id = shipment.world_id
        LEFT JOIN city_open_world_supply_chain_facts settlement_fact
          ON settlement_fact.id = last_fact.supply_chain_fact_id AND settlement_fact.world_id = shipment.world_id
        WHERE shipment.world_id = target_world_id
          AND shipment.state = 'settled'
          AND (last_fact.id IS NULL
               OR last_fact.fact_type <> 'settlement.confirmed'
               OR last_fact.evidence_kind <> 'supply_chain'
               OR settlement_fact.fact_type <> 'order.settled'
               OR settlement_fact.order_code <> shipment.order_code
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_open_world_enterprise_freight_shipment_transitions transition
                    WHERE transition.world_id = shipment.world_id
                      AND transition.shipment_code = shipment.code
                      AND transition.state = 'settled'
                      AND transition.reason_code = 'v22_freight_settlement_completed'
                      AND transition.source_fact_id = last_fact.id
               ))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 V17 settlement proof is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_consignments consignment
        JOIN city_open_world_freight_batch_plans plan
          ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
        LEFT JOIN city_open_world_freight_batch_facts last_runtime_fact
          ON last_runtime_fact.world_id = consignment.world_id
         AND last_runtime_fact.consignment_code = consignment.code
         AND last_runtime_fact.runtime_fact_id = consignment.last_runtime_fact_id
        WHERE consignment.world_id = target_world_id
          AND consignment.state = 'settled'
          AND (last_runtime_fact.id IS NULL
               OR last_runtime_fact.evidence_kind <> 'runtime'
               OR last_runtime_fact.fact_type NOT IN ('route.completed','demand.expired','demand.voided','transport.orphaned')
               OR EXISTS (
                    SELECT 1
                    FROM city_open_world_freight_batch_receipts receipt
                    WHERE receipt.world_id = consignment.world_id AND receipt.consignment_code = consignment.code
               )
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_open_world_freight_batch_transitions transition
                    JOIN city_open_world_freight_batch_facts settlement_adapter_fact
                      ON settlement_adapter_fact.id = transition.source_fact_id
                     AND settlement_adapter_fact.world_id = transition.world_id
                    JOIN city_open_world_supply_chain_facts settlement_fact
                      ON settlement_fact.id = settlement_adapter_fact.supply_chain_fact_id
                     AND settlement_fact.world_id = transition.world_id
                    WHERE transition.world_id = consignment.world_id
                      AND transition.consignment_code = consignment.code
                      AND transition.state = 'settled'
                      AND transition.reason_code = 'v22_freight_settlement_completed'
                      AND settlement_adapter_fact.fact_type = 'settlement.confirmed'
                      AND settlement_adapter_fact.evidence_kind = 'supply_chain'
                      AND settlement_fact.fact_type = 'order.settled'
                      AND settlement_fact.order_code = plan.order_code
               ))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 V18 settlement proof is invalid' USING ERRCODE = '23514';
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
                    AND consignment.state <> 'settled'
              ) THEN 'settled'
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
        RAISE EXCEPTION 'city open-world V22 V18 batch plan state is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_receipts receipt
        JOIN city_open_world_freight_settlement_cases settlement_case
          ON settlement_case.world_id = receipt.world_id AND settlement_case.code = receipt.case_code
        LEFT JOIN (
            SELECT line.world_id, line.receipt_code,
                   SUM(line.accepted_units + line.lost_units) AS outbound_units,
                   SUM((line.lost_units + line.rejected_units)::NUMERIC * case_line.unit_price_units::NUMERIC) AS refund_amount
            FROM city_open_world_freight_settlement_receipt_lines line
            JOIN city_open_world_freight_settlement_case_lines case_line
              ON case_line.world_id = line.world_id AND case_line.case_code = line.case_code
             AND case_line.source_line_no = line.source_line_no
            WHERE line.world_id = target_world_id
            GROUP BY line.world_id, line.receipt_code
        ) receipt_totals
          ON receipt_totals.world_id = receipt.world_id AND receipt_totals.receipt_code = receipt.code
        WHERE receipt.world_id = target_world_id
          AND (((COALESCE(receipt_totals.outbound_units, 0) > 0) <> (receipt.resource_operation_id IS NOT NULL))
               OR (receipt.refunded_units::NUMERIC <> COALESCE(receipt_totals.refund_amount, 0))
               OR ((receipt.refunded_units > 0) <> (receipt.journal_id IS NOT NULL))
               OR (receipt.liability_party = 'seller' AND EXISTS (
                    SELECT 1 FROM city_open_world_freight_settlement_claims claim
                    WHERE claim.world_id = receipt.world_id AND claim.receipt_code = receipt.code
               ))
               OR (receipt.liability_party = 'carrier' AND receipt.refunded_units > 0 AND NOT EXISTS (
                    SELECT 1 FROM city_open_world_freight_settlement_claims claim
                    WHERE claim.world_id = receipt.world_id AND claim.receipt_code = receipt.code
                      AND claim.liability_party = 'carrier' AND claim.claim_amount = receipt.refunded_units
               ))
               OR (receipt.liability_party = 'carrier' AND receipt.refunded_units = 0 AND EXISTS (
                    SELECT 1 FROM city_open_world_freight_settlement_claims claim
                    WHERE claim.world_id = receipt.world_id AND claim.receipt_code = receipt.code
               )))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement financial or resource evidence is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_receipt_profiles profile
        WHERE profile.world_id = target_world_id
          AND (profile.shipment_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id)
               OR profile.awaiting_route_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'awaiting_route')
               OR profile.in_transit_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'in_transit')
               OR profile.awaiting_receipt_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'awaiting_receipt')
               OR profile.received_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'received')
               OR profile.settled_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'settled')
               OR profile.expired_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'expired')
               OR profile.voided_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'voided')
               OR profile.orphaned_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'orphaned'))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 V17 successor counters are inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_profiles profile
        WHERE profile.world_id = target_world_id
          AND (profile.consignment_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id)
               OR profile.awaiting_route_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'awaiting_route')
               OR profile.in_transit_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'in_transit')
               OR profile.awaiting_receipt_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'awaiting_receipt')
               OR profile.received_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'received')
               OR profile.settled_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'settled')
               OR profile.expired_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'expired')
               OR profile.voided_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'voided')
               OR profile.orphaned_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'orphaned'))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 V18 successor counters are inconsistent' USING ERRCODE = '23514';
    END IF;
END;
$$;

-- V22 deliberately adds successor terminal states to the sealed V15/V17/V18
-- projections.  The old rows keep their old state machines; only writes made
-- through the V22 settlement reducer can reach these additional states.
ALTER TABLE city_open_world_supply_chain_facts
    DROP CONSTRAINT IF EXISTS city_open_world_supply_chain_fact_identity_check;
ALTER TABLE city_open_world_supply_chain_facts
    ADD CONSTRAINT city_open_world_supply_chain_fact_identity_check CHECK (
        (order_code IS NULL OR order_code ~ '^[a-z][a-z0-9_.-]{1,159}$')
        AND fact_type IN (
            'order.proposed', 'order.accepted', 'order.dispatched',
            'order.delivered', 'order.settled', 'order.cancelled',
            'order.expired', 'order.failed'
        )
    );

ALTER TABLE city_open_world_supply_chain_order_transitions
    DROP CONSTRAINT IF EXISTS city_open_world_supply_chain_transition_identity_check;
ALTER TABLE city_open_world_supply_chain_order_transitions
    ADD CONSTRAINT city_open_world_supply_chain_transition_identity_check CHECK (
        state IN ('proposed', 'accepted', 'dispatched', 'delivered', 'settled', 'cancelled', 'expired', 'failed')
        AND reason_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
    );

ALTER TABLE city_open_world_enterprise_freight_receipt_profiles
    ADD COLUMN IF NOT EXISTS settled_count BIGINT NOT NULL DEFAULT 0 CHECK (settled_count >= 0);
ALTER TABLE city_open_world_enterprise_freight_receipt_profiles
    DROP CONSTRAINT IF EXISTS city_open_world_enterprise_freight_receipt_profile_counter_check;
ALTER TABLE city_open_world_enterprise_freight_receipt_profiles
    ADD CONSTRAINT city_open_world_enterprise_freight_receipt_profile_counter_check CHECK (
        shipment_count = awaiting_route_count + in_transit_count + awaiting_receipt_count
                         + received_count + settled_count + expired_count + voided_count + orphaned_count
        AND shipment_count <= maximum_shipments
        AND receipt_count = received_count
        AND fact_count >= shipment_count
        AND fact_count >= transition_count
    );
ALTER TABLE city_open_world_enterprise_freight_shipments
    DROP CONSTRAINT IF EXISTS city_open_world_enterprise_freight_shipment_identity_check;
ALTER TABLE city_open_world_enterprise_freight_shipments
    ADD CONSTRAINT city_open_world_enterprise_freight_shipment_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND freight_source_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND order_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND seller_node_code <> buyer_node_code
        AND source_hub_code <> destination_hub_code
        AND state IN ('awaiting_route','in_transit','awaiting_receipt','received','settled','expired','voided','orphaned')
    );
ALTER TABLE city_open_world_enterprise_freight_receipt_facts
    DROP CONSTRAINT IF EXISTS city_open_world_enterprise_freight_receipt_fact_identity_check;
ALTER TABLE city_open_world_enterprise_freight_receipt_facts
    ADD CONSTRAINT city_open_world_enterprise_freight_receipt_fact_identity_check CHECK (
        fact_type IN ('shipment.created','route.awaiting','transport.in_transit','transport.arrived',
                      'transport.expired','transport.voided','transport.orphaned','receipt.confirmed','settlement.confirmed')
        AND evidence_kind IN ('enterprise_freight','supply_chain')
        AND ((evidence_kind = 'enterprise_freight' AND freight_fact_id IS NOT NULL AND supply_chain_fact_id IS NULL)
             OR (evidence_kind = 'supply_chain' AND freight_fact_id IS NULL AND supply_chain_fact_id IS NOT NULL))
    );
ALTER TABLE city_open_world_enterprise_freight_shipment_transitions
    DROP CONSTRAINT IF EXISTS city_open_world_enterprise_freight_shipment_transition_identity_check;
ALTER TABLE city_open_world_enterprise_freight_shipment_transitions
    ADD CONSTRAINT city_open_world_enterprise_freight_shipment_transition_identity_check CHECK (
        state IN ('awaiting_route','in_transit','awaiting_receipt','received','settled','expired','voided','orphaned')
        AND reason_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
    );

ALTER TABLE city_open_world_freight_batch_profiles
    ADD COLUMN IF NOT EXISTS settled_count BIGINT NOT NULL DEFAULT 0 CHECK (settled_count >= 0);
ALTER TABLE city_open_world_freight_batch_profiles
    DROP CONSTRAINT IF EXISTS city_open_world_freight_batch_profile_counter_check;
ALTER TABLE city_open_world_freight_batch_profiles
    ADD CONSTRAINT city_open_world_freight_batch_profile_counter_check CHECK (
        consignment_count = awaiting_route_count + in_transit_count + awaiting_receipt_count
                            + received_count + settled_count + expired_count + voided_count + orphaned_count
        AND receipt_count = received_count
        AND fact_count >= consignment_count
        AND fact_count >= transition_count
        AND transition_count >= consignment_count
    );
ALTER TABLE city_open_world_freight_batch_plans
    DROP CONSTRAINT IF EXISTS city_open_world_freight_batch_plan_identity_check;
ALTER TABLE city_open_world_freight_batch_plans
    ADD CONSTRAINT city_open_world_freight_batch_plan_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND overflow_source_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND order_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND seller_node_code <> buyer_node_code
        AND source_hub_code <> destination_hub_code
        AND state IN ('active','ready','received','settled','blocked')
    );
ALTER TABLE city_open_world_freight_batch_consignments
    DROP CONSTRAINT IF EXISTS city_open_world_freight_batch_consignment_identity_check;
ALTER TABLE city_open_world_freight_batch_consignments
    ADD CONSTRAINT city_open_world_freight_batch_consignment_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND state IN ('awaiting_route','in_transit','awaiting_receipt','received','settled','expired','voided','orphaned')
        AND ((state IN ('awaiting_route','expired','voided') AND mobility_route_id IS NULL)
             OR (state IN ('in_transit','awaiting_receipt','received','settled','orphaned') AND mobility_route_id IS NOT NULL))
    );
ALTER TABLE city_open_world_freight_batch_facts
    DROP CONSTRAINT IF EXISTS city_open_world_freight_batch_fact_identity_check;
ALTER TABLE city_open_world_freight_batch_facts
    ADD CONSTRAINT city_open_world_freight_batch_fact_identity_check CHECK (
        fact_type IN ('consignment.created','demand.requested','route.scheduled','route.completed',
                      'demand.expired','demand.voided','transport.orphaned','receipt.confirmed','settlement.confirmed')
        AND evidence_kind IN ('runtime','supply_chain')
        AND ((evidence_kind = 'runtime' AND runtime_fact_id IS NOT NULL AND supply_chain_fact_id IS NULL)
             OR (evidence_kind = 'supply_chain' AND runtime_fact_id IS NULL AND supply_chain_fact_id IS NOT NULL))
    );
ALTER TABLE city_open_world_freight_batch_transitions
    DROP CONSTRAINT IF EXISTS city_open_world_freight_batch_transition_identity_check;
ALTER TABLE city_open_world_freight_batch_transitions
    ADD CONSTRAINT city_open_world_freight_batch_transition_identity_check CHECK (
        state IN ('awaiting_route','in_transit','awaiting_receipt','received','settled','expired','voided','orphaned')
        AND reason_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
    );

-- Do not overload a full purchase reversal for a per-line freight loss.  A
-- dedicated journal/resource operation type keeps the financial and physical
-- evidence independently queryable while retaining the existing double-entry
-- and inventory posting invariants.
ALTER TABLE city_journals
    DROP CONSTRAINT IF EXISTS city_journal_type_check;
ALTER TABLE city_journals
    ADD CONSTRAINT city_journal_type_check CHECK (
        journal_type IN (
            'opening', 'cash_transfer', 'wage', 'purchase', 'tax', 'subsidy',
            'reversal', 'government_spend', 'rent', 'facility_capital',
            'facility_operation', 'freight_refund'
        )
    );
ALTER TABLE city_journals
    DROP CONSTRAINT IF EXISTS city_journal_origin_check;
ALTER TABLE city_journals
    ADD CONSTRAINT city_journal_origin_check CHECK (
        (journal_type = 'opening'
            AND source_command_id IS NULL AND market_settlement_id IS NULL
            AND reversal_of_journal_id IS NULL)
        OR (journal_type = 'reversal'
            AND source_command_id IS NOT NULL AND market_settlement_id IS NULL
            AND reversal_of_journal_id IS NOT NULL)
        OR (journal_type NOT IN ('opening', 'reversal')
            AND reversal_of_journal_id IS NULL
            AND ((source_command_id IS NOT NULL)::INTEGER + (market_settlement_id IS NOT NULL)::INTEGER) = 1)
    );

ALTER TABLE city_resource_operations
    DROP CONSTRAINT IF EXISTS city_resource_operation_type_check;
ALTER TABLE city_resource_operations
    ADD CONSTRAINT city_resource_operation_type_check CHECK (
        operation_type IN ('opening', 'transfer', 'production', 'consumption', 'freight_settlement')
    );

CREATE OR REPLACE FUNCTION city_open_world_infrastructure_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_infrastructure_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v20','city-openworld-v21','city-openworld-v22')
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version IN ('city-openworld-v20','city-openworld-v21','city-openworld-v22')
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
              AND world.simulation_version IN ('city-openworld-v20','city-openworld-v21','city-openworld-v22')
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_effective_capacity_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_effective_capacity_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v21','city-openworld-v22')
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version IN ('city-openworld-v21','city-openworld-v22')
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
              AND world.simulation_version IN ('city-openworld-v21','city-openworld-v22')
       )
$$;

DO $$
DECLARE definition TEXT;
BEGIN
    definition := pg_get_functiondef('assert_city_open_world_effective_capacity_foundation(bigint)'::REGPROCEDURE);
    definition := replace(definition,
        $old$world_version <> 'city-openworld-v21'$old$,
        $new$world_version NOT IN ('city-openworld-v21','city-openworld-v22')$new$);
    IF position($needle$city-openworld-v22$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V21 effective-capacity assertion to V22' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;
