-- city-openworld-v24 / F10.3.1b: immutable carrier service contracts and
-- cash-only freight fees. V24 deliberately charges only V22 cases that were
-- created after the V24 baseline; it does not reinterpret historic delivery,
-- receipt, claim, reserve, pricing, credit, insurance, or SLA evidence.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
SELECT
    'city-openworld-v24',
    'supported',
    'city-state-v1+gzip',
    COALESCE(
        (SELECT capabilities FROM city_engine_versions WHERE version = 'city-openworld-v23'),
        '[]'::jsonb
    ) || '["carrier_service_contracts","cash_only_carrier_fees","seller_scoped_carrier_commerce"]'::jsonb
ON CONFLICT (version) DO UPDATE
SET status = EXCLUDED.status,
    canonical_format = EXCLUDED.canonical_format,
    capabilities = EXCLUDED.capabilities;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v23', 'city-openworld-v24', 'openworld_v23_to_v24')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_carrier_commerce_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_carrier_recovery_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    carrier_actor_code VARCHAR(160) NOT NULL,
    carrier_firm_code VARCHAR(48) NOT NULL,
    service_contract VARCHAR(96) NOT NULL,
    payment_contract VARCHAR(96) NOT NULL,
    fee_per_cargo_unit BIGINT NOT NULL CHECK (fee_per_cargo_unit > 0),
    maximum_contracts_per_tick INTEGER NOT NULL CHECK (maximum_contracts_per_tick BETWEEN 1 AND 1000000),
    maximum_payments_per_tick INTEGER NOT NULL CHECK (maximum_payments_per_tick BETWEEN 1 AND 1000000),
    contract_count BIGINT NOT NULL DEFAULT 0 CHECK (contract_count >= 0),
    payment_count BIGINT NOT NULL DEFAULT 0 CHECK (payment_count >= 0),
    quoted_cargo_units BIGINT NOT NULL DEFAULT 0 CHECK (quoted_cargo_units >= 0),
    paid_cargo_units BIGINT NOT NULL DEFAULT 0 CHECK (paid_cargo_units >= 0),
    paid_amount_units BIGINT NOT NULL DEFAULT 0 CHECK (paid_amount_units >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_carrier_commerce_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-carrier-commerce'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND carrier_actor_code = 'system.freight.carrier'
        AND carrier_firm_code = 'system_freight_reserve'
        AND service_contract = 'v22_case_quoted_carrier_service_v1'
        AND payment_contract = 'seller_cash_per_unit_carrier_fee_v1'
        AND fee_per_cargo_unit = 1
        AND maximum_contracts_per_tick = 256
        AND maximum_payments_per_tick = 256
        AND payment_count <= contract_count
        AND paid_cargo_units <= quoted_cargo_units
        AND paid_amount_units <= quoted_cargo_units * fee_per_cargo_unit
        AND revision = contract_count + payment_count + 1
    ),
    CONSTRAINT city_open_world_carrier_commerce_profile_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'scope' = 'post_baseline_v22_case_carrier_service_fee'
        AND metadata->>'pricing' = 'fixed_per_cargo_unit_v1'
        AND metadata->>'settlement' = 'cash_only_retry_without_credit_v1'
    )
);

CREATE TABLE IF NOT EXISTS city_open_world_carrier_service_contracts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_carrier_commerce_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    case_code VARCHAR(160) NOT NULL,
    source_kind VARCHAR(24) NOT NULL,
    source_code VARCHAR(160) NOT NULL,
    seller_firm_entity_id BIGINT NOT NULL,
    carrier_firm_entity_id BIGINT NOT NULL,
    carrier_actor_code VARCHAR(160) NOT NULL,
    source_tick BIGINT NOT NULL CHECK (source_tick > 0),
    contract_tick BIGINT NOT NULL CHECK (contract_tick > 0),
    cargo_units BIGINT NOT NULL CHECK (cargo_units > 0),
    fee_per_cargo_unit BIGINT NOT NULL CHECK (fee_per_cargo_unit > 0),
    quoted_fee_units BIGINT NOT NULL CHECK (quoted_fee_units > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_carrier_service_contract_case_fk
        FOREIGN KEY (world_id, case_code)
        REFERENCES city_open_world_freight_settlement_cases(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_carrier_service_contract_seller_fk
        FOREIGN KEY (seller_firm_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_carrier_service_contract_carrier_fk
        FOREIGN KEY (carrier_firm_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_carrier_service_contract_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND case_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND source_kind IN ('shipment','consignment')
        AND source_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND carrier_actor_code = 'system.freight.carrier'
        AND seller_firm_entity_id <> carrier_firm_entity_id
        AND contract_tick >= source_tick
        AND cargo_units <= 4611686018427387903
        AND cargo_units * fee_per_cargo_unit = quoted_fee_units
    ),
    CONSTRAINT city_open_world_carrier_service_contract_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'contract' = 'v22_case_quoted_carrier_service_v1'
        AND metadata->>'case_code' = case_code
        AND metadata->>'source_kind' = source_kind
        AND metadata->>'source_code' = source_code
        AND metadata->>'carrier_actor' = carrier_actor_code
        AND metadata->>'cargo_units' = cargo_units::TEXT
        AND metadata->>'fee_per_cargo_unit' = fee_per_cargo_unit::TEXT
        AND metadata->>'quoted_fee_units' = quoted_fee_units::TEXT
    ),
    CONSTRAINT city_open_world_carrier_service_contract_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_carrier_service_contract_case_unique UNIQUE (world_id, case_code)
);

CREATE TABLE IF NOT EXISTS city_open_world_carrier_fee_payments (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_carrier_commerce_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    contract_code VARCHAR(160) NOT NULL,
    case_code VARCHAR(160) NOT NULL,
    seller_firm_entity_id BIGINT NOT NULL,
    carrier_firm_entity_id BIGINT NOT NULL,
    payment_tick BIGINT NOT NULL CHECK (payment_tick > 0),
    cargo_units BIGINT NOT NULL CHECK (cargo_units > 0),
    amount_units BIGINT NOT NULL CHECK (amount_units > 0),
    journal_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_carrier_fee_payment_contract_fk
        FOREIGN KEY (world_id, contract_code)
        REFERENCES city_open_world_carrier_service_contracts(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_carrier_fee_payment_case_fk
        FOREIGN KEY (world_id, case_code)
        REFERENCES city_open_world_freight_settlement_cases(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_carrier_fee_payment_seller_fk
        FOREIGN KEY (seller_firm_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_carrier_fee_payment_carrier_fk
        FOREIGN KEY (carrier_firm_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_carrier_fee_payment_journal_fk
        FOREIGN KEY (journal_id, world_id)
        REFERENCES city_journals(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_carrier_fee_payment_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND contract_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND case_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND seller_firm_entity_id <> carrier_firm_entity_id
    ),
    CONSTRAINT city_open_world_carrier_fee_payment_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'contract' = 'seller_cash_per_unit_carrier_fee_v1'
        AND metadata->>'contract_code' = contract_code
        AND metadata->>'case_code' = case_code
        AND metadata->>'cargo_units' = cargo_units::TEXT
        AND metadata->>'amount_units' = amount_units::TEXT
    ),
    CONSTRAINT city_open_world_carrier_fee_payment_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_carrier_fee_payment_contract_unique UNIQUE (world_id, contract_code),
    CONSTRAINT city_open_world_carrier_fee_payment_journal_unique UNIQUE (world_id, journal_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_carrier_service_contracts_timeline
    ON city_open_world_carrier_service_contracts (world_id, source_tick, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_carrier_service_contracts_seller
    ON city_open_world_carrier_service_contracts (world_id, seller_firm_entity_id, source_tick, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_carrier_fee_payments_timeline
    ON city_open_world_carrier_fee_payments (world_id, payment_tick, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_carrier_fee_payments_seller
    ON city_open_world_carrier_fee_payments (world_id, seller_firm_entity_id, payment_tick, code);

-- A V24 fee is an automatic domain settlement rather than a user command or
-- market-cycle result. Give it a dedicated, source-free origin shape and keep
-- all previous journal origins unchanged.
ALTER TABLE city_journals DROP CONSTRAINT IF EXISTS city_journal_type_check;
ALTER TABLE city_journals ADD CONSTRAINT city_journal_type_check CHECK (
    journal_type IN (
        'opening', 'cash_transfer', 'wage', 'purchase', 'tax', 'subsidy',
        'reversal', 'government_spend', 'rent', 'facility_capital',
        'facility_operation', 'freight_refund', 'freight_fee'
    )
);
ALTER TABLE city_journals DROP CONSTRAINT IF EXISTS city_journal_origin_check;
ALTER TABLE city_journals ADD CONSTRAINT city_journal_origin_check CHECK (
    (journal_type = 'opening'
        AND source_command_id IS NULL AND market_settlement_id IS NULL
        AND reversal_of_journal_id IS NULL)
    OR (journal_type = 'freight_fee'
        AND source_command_id IS NULL AND market_settlement_id IS NULL
        AND reversal_of_journal_id IS NULL)
    OR (journal_type = 'reversal'
        AND source_command_id IS NOT NULL AND market_settlement_id IS NULL
        AND reversal_of_journal_id IS NOT NULL)
    OR (journal_type NOT IN ('opening', 'reversal', 'freight_fee')
        AND reversal_of_journal_id IS NULL
        AND ((source_command_id IS NOT NULL)::INTEGER + (market_settlement_id IS NOT NULL)::INTEGER) = 1)
);

CREATE OR REPLACE FUNCTION city_open_world_carrier_commerce_recovery_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_recovery_write_enabled(target_world_id)
       OR COALESCE(current_setting('sub2api.city_open_world_carrier_commerce_recovery_world_id', TRUE), '') = target_world_id::TEXT
$$;

CREATE OR REPLACE FUNCTION city_open_world_carrier_commerce_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_carrier_commerce_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v24'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v24'
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_carrier_commerce_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_carrier_commerce_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v24'
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_carrier_commerce_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_carrier_commerce_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_carrier_commerce_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_carrier_commerce_write_enabled(target_world_id)
       AND (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.carrier_actor_code, NEW.carrier_firm_code,
            NEW.service_contract, NEW.payment_contract, NEW.fee_per_cargo_unit,
            NEW.maximum_contracts_per_tick, NEW.maximum_payments_per_tick,
            NEW.metadata, NEW.created_at)
           IS NOT DISTINCT FROM
           (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.carrier_actor_code, OLD.carrier_firm_code,
            OLD.service_contract, OLD.payment_contract, OLD.fee_per_cargo_unit,
            OLD.maximum_contracts_per_tick, OLD.maximum_payments_per_tick,
            OLD.metadata, OLD.created_at)
       AND NEW.revision = OLD.revision + 1
       AND ((NEW.contract_count = OLD.contract_count + 1
             AND NEW.payment_count = OLD.payment_count
             AND NEW.quoted_cargo_units > OLD.quoted_cargo_units
             AND NEW.paid_cargo_units = OLD.paid_cargo_units
             AND NEW.paid_amount_units = OLD.paid_amount_units)
            OR (NEW.contract_count = OLD.contract_count
                AND NEW.payment_count = OLD.payment_count + 1
                AND NEW.quoted_cargo_units = OLD.quoted_cargo_units
                AND NEW.paid_cargo_units > OLD.paid_cargo_units
                AND NEW.paid_amount_units > OLD.paid_amount_units)) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V24 carrier-commerce profile requires audited bootstrap, write, or recovery context'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_carrier_commerce_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_carrier_commerce_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_carrier_commerce_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V24 carrier-commerce projections are append-only audited evidence'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS guard_city_open_world_carrier_commerce_profile_write ON city_open_world_carrier_commerce_profiles;
CREATE TRIGGER guard_city_open_world_carrier_commerce_profile_write
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_carrier_commerce_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_carrier_commerce_profile();

DO $$
DECLARE table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_open_world_carrier_service_contracts',
        'city_open_world_carrier_fee_payments'
    ] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS guard_%I_write ON %I', table_name, table_name);
        EXECUTE format('CREATE TRIGGER guard_%I_write BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_carrier_commerce_projection()', table_name, table_name);
    END LOOP;
END;
$$;

-- V23 remains usable after upgrade. Preserve its manual reserve and recovery
-- controls exactly, merely widening their engine-version gates to V24.
CREATE OR REPLACE FUNCTION city_open_world_carrier_recovery_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_carrier_recovery_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v23','city-openworld-v24')
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version IN ('city-openworld-v23','city-openworld-v24')
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_carrier_recovery_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_carrier_recovery_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v23','city-openworld-v24')
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_freight_settlement_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_freight_settlement_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v22','city-openworld-v23','city-openworld-v24')
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_freight_settlement_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_freight_settlement_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v22','city-openworld-v23','city-openworld-v24')
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version IN ('city-openworld-v22','city-openworld-v23','city-openworld-v24')
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_freight_settlement_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
DECLARE is_carrier_recovery BOOLEAN;
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
    is_carrier_recovery := city_open_world_carrier_recovery_write_enabled(target_world_id)
        AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v23','city-openworld-v24')
        );
    IF TG_OP = 'UPDATE' AND city_open_world_freight_settlement_write_enabled(target_world_id) THEN
        IF is_carrier_recovery THEN
            IF TG_TABLE_NAME = 'city_open_world_freight_settlement_claims'
               AND OLD.state = 'open' AND NEW.state = 'resolved'
               AND (NEW.world_id, NEW.code, NEW.receipt_code, NEW.case_code,
                    NEW.liability_party, NEW.claim_amount, NEW.created_tick, NEW.metadata,
                    NEW.created_at)
                   IS NOT DISTINCT FROM
                   (OLD.world_id, OLD.code, OLD.receipt_code, OLD.case_code,
                    OLD.liability_party, OLD.claim_amount, OLD.created_tick, OLD.metadata,
                    OLD.created_at)
               AND EXISTS (
                    SELECT 1
                    FROM city_open_world_freight_claim_recoveries recovery
                    WHERE recovery.world_id = NEW.world_id
                      AND recovery.claim_code = NEW.code
                      AND recovery.case_code = NEW.case_code
                      AND recovery.amount_units = NEW.claim_amount
               ) THEN
                RETURN NEW;
            END IF;
        ELSIF TG_TABLE_NAME IN (
            'city_open_world_freight_settlement_orders',
            'city_open_world_freight_settlement_cases',
            'city_open_world_freight_settlement_claims'
        ) THEN
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION 'open-world V22 freight-settlement projections require audited bootstrap, reducer, or recovery context'
        USING ERRCODE = '55000';
END;
$$;

-- V20/V21 are still initialized for every new V24 world and continue to
-- receive audited runtime facts after genesis. Earlier successor migrations
-- stopped these gates at V22, which made a V23/V24 world fail before its own
-- profile could be written. Re-declare the narrow gates here rather than
-- weakening the underlying infrastructure/effective-capacity triggers.
CREATE OR REPLACE FUNCTION city_open_world_infrastructure_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_infrastructure_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN (
                  'city-openworld-v20','city-openworld-v21','city-openworld-v22',
                  'city-openworld-v23','city-openworld-v24'
              )
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version IN (
                          'city-openworld-v20','city-openworld-v21','city-openworld-v22',
                          'city-openworld-v23','city-openworld-v24'
                      )
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
              AND world.simulation_version IN (
                  'city-openworld-v20','city-openworld-v21','city-openworld-v22',
                  'city-openworld-v23','city-openworld-v24'
              )
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_effective_capacity_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_effective_capacity_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN (
                  'city-openworld-v21','city-openworld-v22','city-openworld-v23','city-openworld-v24'
              )
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version IN (
                          'city-openworld-v21','city-openworld-v22','city-openworld-v23','city-openworld-v24'
                      )
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
              AND world.simulation_version IN (
                  'city-openworld-v21','city-openworld-v22','city-openworld-v23','city-openworld-v24'
              )
       )
$$;

-- Retain exact predecessor contracts instead of copying their large function
-- bodies. Migration 246 has already extended these names to V23; this narrow
-- successor extends that exact version tail to V24.
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
        definition := replace(definition, $old$'city-openworld-v22','city-openworld-v23'$old$, $new$'city-openworld-v22','city-openworld-v23','city-openworld-v24'$new$);
        IF position($needle$city-openworld-v24$needle$ IN definition) = 0 THEN
            RAISE EXCEPTION 'cannot extend V24 predecessor write gate %', target_function USING ERRCODE = '23514';
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
        $old$'city-openworld-v19','city-openworld-v20','city-openworld-v21','city-openworld-v22','city-openworld-v23'$old$,
        $new$'city-openworld-v19','city-openworld-v20','city-openworld-v21','city-openworld-v22','city-openworld-v23','city-openworld-v24'$new$);
    IF position($needle$city-openworld-v24$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V19 spatial-network bootstrap gate to V24' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;

DO $$
DECLARE definition TEXT;
BEGIN
    definition := pg_get_functiondef('assert_city_open_world_freight_settlement_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$world_version NOT IN ('city-openworld-v22','city-openworld-v23')$old$,
        $new$world_version NOT IN ('city-openworld-v22','city-openworld-v23','city-openworld-v24')$new$
    );
    IF position($needle$city-openworld-v24$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V22 freight-settlement assertion to V24' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;

    definition := pg_get_functiondef('assert_city_open_world_carrier_recovery_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$WHERE id = target_world_id AND simulation_version = 'city-openworld-v23';$old$,
        $new$WHERE id = target_world_id AND simulation_version IN ('city-openworld-v23','city-openworld-v24');$new$
    );
    IF position($needle$city-openworld-v24$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V23 carrier-recovery assertion to V24' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;

-- Foundation assertions are part of the same successor contract as their
-- write gates. Keep V19/V20/V21 validation live for V24 worlds instead of
-- silently returning before the predecessor graph has been checked.
DO $$
DECLARE definition TEXT;
BEGIN
    definition := pg_get_functiondef('assert_city_open_world_spatial_network_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$world_version NOT IN ('city-openworld-v19','city-openworld-v20')$old$,
        $new$world_version NOT IN ('city-openworld-v19','city-openworld-v20','city-openworld-v21','city-openworld-v22','city-openworld-v23','city-openworld-v24')$new$
    );
    IF position($needle$city-openworld-v24$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V19 spatial-network assertion to V24' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;

    definition := pg_get_functiondef('assert_city_open_world_infrastructure_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$world_version <> 'city-openworld-v20'$old$,
        $new$world_version NOT IN ('city-openworld-v20','city-openworld-v21','city-openworld-v22','city-openworld-v23','city-openworld-v24')$new$
    );
    IF position($needle$city-openworld-v24$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V20 infrastructure assertion to V24' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;

    definition := pg_get_functiondef('assert_city_open_world_effective_capacity_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$world_version NOT IN ('city-openworld-v21','city-openworld-v22')$old$,
        $new$world_version NOT IN ('city-openworld-v21','city-openworld-v22','city-openworld-v23','city-openworld-v24')$new$
    );
    IF position($needle$city-openworld-v24$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V21 effective-capacity assertion to V24' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_carrier_commerce_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_tick BIGINT;
    profile_tick BIGINT;
    profile_contracts BIGINT;
    profile_payments BIGINT;
    profile_quoted BIGINT;
    profile_paid_cargo BIGINT;
    profile_paid_amount BIGINT;
BEGIN
    SELECT current_tick INTO world_tick
    FROM city_worlds
    WHERE id = target_world_id AND simulation_version = 'city-openworld-v24';
    IF NOT FOUND THEN RETURN; END IF;

    SELECT baseline_tick, contract_count, payment_count, quoted_cargo_units,
           paid_cargo_units, paid_amount_units
      INTO profile_tick, profile_contracts, profile_payments, profile_quoted,
           profile_paid_cargo, profile_paid_amount
    FROM city_open_world_carrier_commerce_profiles
    WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick
       OR profile_contracts <> (SELECT COUNT(*) FROM city_open_world_carrier_service_contracts WHERE world_id = target_world_id)
       OR profile_payments <> (SELECT COUNT(*) FROM city_open_world_carrier_fee_payments WHERE world_id = target_world_id)
       OR profile_quoted <> COALESCE((SELECT SUM(cargo_units) FROM city_open_world_carrier_service_contracts WHERE world_id = target_world_id), 0)
       OR profile_paid_cargo <> COALESCE((SELECT SUM(cargo_units) FROM city_open_world_carrier_fee_payments WHERE world_id = target_world_id), 0)
       OR profile_paid_amount <> COALESCE((SELECT SUM(amount_units) FROM city_open_world_carrier_fee_payments WHERE world_id = target_world_id), 0)
       OR profile_payments > profile_contracts
       OR profile_paid_cargo > profile_quoted THEN
        RAISE EXCEPTION 'city open-world V24 carrier-commerce profile is missing or inconsistent' USING ERRCODE = '23514';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM city_open_world_actors actor
        WHERE actor.world_id = target_world_id
          AND actor.code = 'system.freight.carrier'
          AND actor.actor_type_code = 'system.freight_carrier'
          AND actor.status = 'active' AND actor.owner_user_id IS NULL
    ) OR NOT EXISTS (
        SELECT 1 FROM city_economic_entities firm
        WHERE firm.world_id = target_world_id
          AND firm.entity_type = 'firm'
          AND firm.code = 'system_freight_reserve'
          AND firm.status = 'active' AND firm.owner_user_id IS NULL
    ) THEN
        RAISE EXCEPTION 'city open-world V24 carrier-commerce identity is invalid' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_carrier_service_contracts contract
        JOIN city_open_world_freight_settlement_cases settlement_case
          ON settlement_case.world_id = contract.world_id AND settlement_case.code = contract.case_code
        JOIN city_economic_entities seller
          ON seller.id = contract.seller_firm_entity_id AND seller.world_id = contract.world_id
        JOIN city_economic_entities carrier
          ON carrier.id = contract.carrier_firm_entity_id AND carrier.world_id = contract.world_id
        JOIN LATERAL (
            SELECT COALESCE(SUM(line.quantity_units), 0)::BIGINT AS cargo_units,
                   MIN(line.source_firm_code) AS seller_firm_code,
                   MAX(line.source_firm_code) AS seller_firm_code_max
            FROM city_open_world_freight_settlement_case_lines line
            WHERE line.world_id = contract.world_id AND line.case_code = contract.case_code
        ) case_lines ON TRUE
        WHERE contract.world_id = target_world_id
          AND (contract.source_tick <= profile_tick
               OR settlement_case.source_tick <> contract.source_tick
               OR settlement_case.source_kind <> contract.source_kind
               OR settlement_case.source_code <> contract.source_code
               OR seller.entity_type <> 'firm' OR seller.status <> 'active'
               OR carrier.entity_type <> 'firm' OR carrier.status <> 'active'
               OR carrier.code <> 'system_freight_reserve'
               OR contract.carrier_actor_code <> 'system.freight.carrier'
               OR case_lines.cargo_units <> contract.cargo_units
               OR case_lines.seller_firm_code IS NULL
               OR case_lines.seller_firm_code <> case_lines.seller_firm_code_max
               OR case_lines.seller_firm_code <> seller.code
               OR contract.quoted_fee_units <> contract.cargo_units * contract.fee_per_cargo_unit
               OR contract.metadata->>'seller_firm' <> seller.code
               OR contract.metadata->>'carrier_firm' <> carrier.code)
    ) THEN
        RAISE EXCEPTION 'city open-world V24 carrier-service contract evidence is invalid' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_carrier_fee_payments payment
        JOIN city_open_world_carrier_service_contracts contract
          ON contract.world_id = payment.world_id AND contract.code = payment.contract_code
        JOIN city_open_world_freight_settlement_cases settlement_case
          ON settlement_case.world_id = payment.world_id AND settlement_case.code = payment.case_code
        JOIN city_economic_entities seller
          ON seller.id = payment.seller_firm_entity_id AND seller.world_id = payment.world_id
        JOIN city_economic_entities carrier
          ON carrier.id = payment.carrier_firm_entity_id AND carrier.world_id = payment.world_id
        JOIN city_journals journal
          ON journal.id = payment.journal_id AND journal.world_id = payment.world_id
        WHERE payment.world_id = target_world_id
          AND (payment.case_code <> contract.case_code
               OR payment.seller_firm_entity_id <> contract.seller_firm_entity_id
               OR payment.carrier_firm_entity_id <> contract.carrier_firm_entity_id
               OR payment.payment_tick <= contract.contract_tick
               OR payment.cargo_units <> contract.cargo_units
               OR payment.amount_units <> contract.quoted_fee_units
               OR settlement_case.state <> 'settled'
               OR seller.entity_type <> 'firm' OR seller.status <> 'active'
               OR carrier.code <> 'system_freight_reserve'
               OR journal.journal_type <> 'freight_fee'
               OR journal.source_command_id IS NOT NULL OR journal.market_settlement_id IS NOT NULL
               OR journal.tick <> payment.payment_tick OR journal.posted_at IS NULL
               OR journal.operation_key <> 'open_world.carrier_fee.payment.' || contract.code
               OR journal.metadata->>'contract' <> 'seller_cash_per_unit_carrier_fee_v1'
               OR journal.metadata->>'contract_code' <> contract.code
               OR journal.metadata->>'case_code' <> payment.case_code
               OR journal.metadata->>'seller_firm' <> seller.code
               OR journal.metadata->>'carrier_firm' <> carrier.code
               OR journal.metadata->>'cargo_units' <> payment.cargo_units::TEXT
               OR journal.metadata->>'amount_units' <> payment.amount_units::TEXT
               OR payment.metadata->>'seller_firm' <> seller.code
               OR payment.metadata->>'carrier_firm' <> carrier.code
               OR (SELECT COUNT(*) FROM city_journal_entries entry WHERE entry.journal_id = journal.id) <> 4
               OR NOT EXISTS (
                    SELECT 1 FROM city_journal_entries entry
                    JOIN city_accounts account ON account.id = entry.account_id
                    JOIN city_account_templates template ON template.id = account.template_id
                    WHERE entry.journal_id = journal.id AND account.entity_id = seller.id
                      AND template.code = 'transfer_expense'
                      AND entry.debit_units = payment.amount_units AND entry.credit_units = 0
               )
               OR NOT EXISTS (
                    SELECT 1 FROM city_journal_entries entry
                    JOIN city_accounts account ON account.id = entry.account_id
                    JOIN city_account_templates template ON template.id = account.template_id
                    WHERE entry.journal_id = journal.id AND account.entity_id = seller.id
                      AND template.code = 'cash'
                      AND entry.debit_units = 0 AND entry.credit_units = payment.amount_units
               )
               OR NOT EXISTS (
                    SELECT 1 FROM city_journal_entries entry
                    JOIN city_accounts account ON account.id = entry.account_id
                    JOIN city_account_templates template ON template.id = account.template_id
                    WHERE entry.journal_id = journal.id AND account.entity_id = carrier.id
                      AND template.code = 'cash'
                      AND entry.debit_units = payment.amount_units AND entry.credit_units = 0
               )
               OR NOT EXISTS (
                    SELECT 1 FROM city_journal_entries entry
                    JOIN city_accounts account ON account.id = entry.account_id
                    JOIN city_account_templates template ON template.id = account.template_id
                    WHERE entry.journal_id = journal.id AND account.entity_id = carrier.id
                      AND template.code = 'revenue'
                      AND entry.debit_units = 0 AND entry.credit_units = payment.amount_units
               ))
    ) THEN
        RAISE EXCEPTION 'city open-world V24 carrier-fee payment evidence is invalid' USING ERRCODE = '23514';
    END IF;
END;
$$;
