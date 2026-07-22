-- city-openworld-v23 / F10.3.1a: manual carrier reserve and auditable
-- carrier-liability claim recovery. This is deliberately a narrow economic
-- successor to V22: it closes a carrier claim without inventing insurance,
-- pricing, SLA, or in-transit accounting before their own versions exist.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
SELECT
    'city-openworld-v23',
    'supported',
    'city-state-v1+gzip',
    COALESCE(
        (SELECT capabilities FROM city_engine_versions WHERE version = 'city-openworld-v22'),
        '[]'::jsonb
    ) || '["carrier_reserve","carrier_claim_recovery","seller_scoped_recovery_visibility"]'::jsonb
ON CONFLICT (version) DO UPDATE
SET status = EXCLUDED.status,
    canonical_format = EXCLUDED.canonical_format,
    capabilities = EXCLUDED.capabilities;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v22', 'city-openworld-v23', 'openworld_v22_to_v23')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_carrier_recovery_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_freight_settlement_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    carrier_actor_code VARCHAR(160) NOT NULL,
    carrier_firm_code VARCHAR(48) NOT NULL,
    funding_contract VARCHAR(96) NOT NULL,
    recovery_contract VARCHAR(96) NOT NULL,
    reserve_policy VARCHAR(48) NOT NULL,
    maximum_fundings_per_tick INTEGER NOT NULL CHECK (maximum_fundings_per_tick BETWEEN 1 AND 1000000),
    maximum_recoveries_per_tick INTEGER NOT NULL CHECK (maximum_recoveries_per_tick BETWEEN 1 AND 1000000),
    maximum_amount_units BIGINT NOT NULL CHECK (maximum_amount_units > 0),
    funding_count BIGINT NOT NULL DEFAULT 0 CHECK (funding_count >= 0),
    recovery_count BIGINT NOT NULL DEFAULT 0 CHECK (recovery_count >= 0),
    funded_units BIGINT NOT NULL DEFAULT 0 CHECK (funded_units >= 0),
    recovered_units BIGINT NOT NULL DEFAULT 0 CHECK (recovered_units >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_carrier_recovery_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-carrier-recovery'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND carrier_actor_code = 'system.freight.carrier'
        AND carrier_firm_code = 'system_freight_reserve'
        AND funding_contract = 'government_to_manual_carrier_reserve_v1'
        AND recovery_contract = 'carrier_claim_to_seller_cash_recovery_v1'
        AND reserve_policy = 'manual_reserve_only'
        AND maximum_fundings_per_tick = 32
        AND maximum_recoveries_per_tick = 256
        AND maximum_amount_units = 4611686018427387903
        AND revision = funding_count + recovery_count + 1
    ),
    CONSTRAINT city_open_world_carrier_recovery_profile_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'scope' = 'manual_carrier_reserve_and_claim_recovery'
        AND metadata->>'reserve_policy' = 'manual_reserve_only'
        AND metadata->>'claim_visibility' = 'seller_scoped_recovery_evidence'
    )
);

CREATE TABLE IF NOT EXISTS city_open_world_carrier_reserve_fundings (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_carrier_recovery_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    funding_tick BIGINT NOT NULL CHECK (funding_tick > 0),
    source_command_id BIGINT NOT NULL,
    amount_units BIGINT NOT NULL CHECK (amount_units > 0),
    journal_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_carrier_reserve_funding_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_carrier_reserve_funding_journal_fk
        FOREIGN KEY (journal_id, world_id)
        REFERENCES city_journals(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_carrier_reserve_funding_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
    ),
    CONSTRAINT city_open_world_carrier_reserve_funding_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'contract' = 'government_to_manual_carrier_reserve_v1'
    ),
    CONSTRAINT city_open_world_carrier_reserve_funding_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_carrier_reserve_funding_command_unique UNIQUE (world_id, source_command_id),
    CONSTRAINT city_open_world_carrier_reserve_funding_journal_unique UNIQUE (world_id, journal_id)
);

CREATE TABLE IF NOT EXISTS city_open_world_freight_claim_recoveries (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_carrier_recovery_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    claim_code VARCHAR(160) NOT NULL,
    case_code VARCHAR(160) NOT NULL,
    seller_firm_entity_id BIGINT NOT NULL,
    carrier_firm_entity_id BIGINT NOT NULL,
    recovery_tick BIGINT NOT NULL CHECK (recovery_tick > 0),
    source_command_id BIGINT NOT NULL,
    amount_units BIGINT NOT NULL CHECK (amount_units > 0),
    journal_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_freight_claim_recovery_claim_fk
        FOREIGN KEY (world_id, claim_code)
        REFERENCES city_open_world_freight_settlement_claims(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_claim_recovery_seller_fk
        FOREIGN KEY (seller_firm_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_claim_recovery_carrier_fk
        FOREIGN KEY (carrier_firm_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_claim_recovery_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_claim_recovery_journal_fk
        FOREIGN KEY (journal_id, world_id)
        REFERENCES city_journals(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_freight_claim_recovery_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND claim_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND case_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND seller_firm_entity_id <> carrier_firm_entity_id
    ),
    CONSTRAINT city_open_world_freight_claim_recovery_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'contract' = 'carrier_claim_to_seller_cash_recovery_v1'
    ),
    CONSTRAINT city_open_world_freight_claim_recovery_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_freight_claim_recovery_claim_unique UNIQUE (world_id, claim_code),
    CONSTRAINT city_open_world_freight_claim_recovery_command_unique UNIQUE (world_id, source_command_id),
    CONSTRAINT city_open_world_freight_claim_recovery_journal_unique UNIQUE (world_id, journal_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_carrier_reserve_fundings_timeline
    ON city_open_world_carrier_reserve_fundings (world_id, funding_tick, id);
CREATE INDEX IF NOT EXISTS idx_city_open_world_freight_claim_recoveries_timeline
    ON city_open_world_freight_claim_recoveries (world_id, recovery_tick, id);
CREATE INDEX IF NOT EXISTS idx_city_open_world_freight_claim_recoveries_seller
    ON city_open_world_freight_claim_recoveries (world_id, seller_firm_entity_id, recovery_tick, id);

CREATE OR REPLACE FUNCTION city_open_world_carrier_recovery_recovery_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_recovery_write_enabled(target_world_id)
       OR COALESCE(current_setting('sub2api.city_open_world_carrier_recovery_recovery_world_id', TRUE), '') = target_world_id::TEXT
$$;

CREATE OR REPLACE FUNCTION city_open_world_carrier_recovery_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_carrier_recovery_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v23'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v23'
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
              AND world.simulation_version = 'city-openworld-v23'
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_carrier_recovery_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_carrier_recovery_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_carrier_recovery_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_carrier_recovery_write_enabled(target_world_id)
       AND (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.carrier_actor_code, NEW.carrier_firm_code,
            NEW.funding_contract, NEW.recovery_contract, NEW.reserve_policy,
            NEW.maximum_fundings_per_tick, NEW.maximum_recoveries_per_tick,
            NEW.maximum_amount_units, NEW.metadata, NEW.created_at)
           IS NOT DISTINCT FROM
           (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.carrier_actor_code, OLD.carrier_firm_code,
            OLD.funding_contract, OLD.recovery_contract, OLD.reserve_policy,
            OLD.maximum_fundings_per_tick, OLD.maximum_recoveries_per_tick,
            OLD.maximum_amount_units, OLD.metadata, OLD.created_at)
       AND NEW.revision = OLD.revision + 1
       AND ((NEW.funding_count = OLD.funding_count + 1
             AND NEW.recovery_count = OLD.recovery_count
             AND NEW.funded_units > OLD.funded_units
             AND NEW.recovered_units = OLD.recovered_units)
            OR (NEW.funding_count = OLD.funding_count
                AND NEW.recovery_count = OLD.recovery_count + 1
                AND NEW.funded_units = OLD.funded_units
                AND NEW.recovered_units > OLD.recovered_units)) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V23 carrier-recovery profile requires audited bootstrap, write, or recovery context'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_carrier_recovery_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_carrier_recovery_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_carrier_recovery_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V23 carrier-recovery projections are append-only audited evidence'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS guard_city_open_world_carrier_recovery_profile_write ON city_open_world_carrier_recovery_profiles;
CREATE TRIGGER guard_city_open_world_carrier_recovery_profile_write
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_carrier_recovery_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_carrier_recovery_profile();

DO $$
DECLARE table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_open_world_carrier_reserve_fundings',
        'city_open_world_freight_claim_recoveries'
    ] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS guard_%I_write ON %I', table_name, table_name);
        EXECUTE format('CREATE TRIGGER guard_%I_write BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_carrier_recovery_projection()', table_name, table_name);
    END LOOP;
END;
$$;

-- V23 resolves a V22 carrier claim but does not alter V22's receipt rows or
-- counters. Keep V22's write guard narrow while allowing it to observe the
-- audited V23 successor context.
CREATE OR REPLACE FUNCTION city_open_world_freight_settlement_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_freight_settlement_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v22','city-openworld-v23')
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_freight_settlement_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_freight_settlement_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v22','city-openworld-v23')
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version IN ('city-openworld-v22','city-openworld-v23')
                      AND upgrade.status = 'running'
              ))
       )
$$;

-- V22 remains active in a V23 world, so its normal receipt reducer still
-- needs the original update path. While a V23 carrier-recovery command is in
-- flight, however, narrow V22's mutable claim surface to the exact audited
-- open->resolved successor transition. This prevents a reserve command from
-- becoming a broad V22 projection-write capability.
CREATE OR REPLACE FUNCTION guard_city_open_world_freight_settlement_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
DECLARE is_v23_carrier_recovery BOOLEAN;
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
    is_v23_carrier_recovery := city_open_world_carrier_recovery_write_enabled(target_world_id)
        AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id AND world.simulation_version = 'city-openworld-v23'
        );
    IF TG_OP = 'UPDATE' AND city_open_world_freight_settlement_write_enabled(target_world_id) THEN
        IF is_v23_carrier_recovery THEN
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

-- Preserve the complete V22.1 assertion body and change only its version
-- applicability. Dynamic replacement keeps the V22 closure protections from
-- being duplicated or silently weakened in this successor migration.
DO $$
DECLARE definition TEXT;
BEGIN
    definition := pg_get_functiondef('assert_city_open_world_freight_settlement_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$world_version <> 'city-openworld-v22'$old$,
        $new$world_version NOT IN ('city-openworld-v22','city-openworld-v23')$new$
    );
    IF position($needle$city-openworld-v23$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V22 freight-settlement assertion to V23' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;

-- New V23 worlds still run every prior open-world initializer. Extend only
-- exact version lists so predecessor write contracts remain strict for older
-- worlds rather than being broadened to arbitrary engine versions.
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
        definition := replace(definition, $old$'city-openworld-v22'$old$, $new$'city-openworld-v22','city-openworld-v23'$new$);
        IF position($needle$city-openworld-v23$needle$ IN definition) = 0 THEN
            RAISE EXCEPTION 'cannot extend V23 predecessor write gate %', target_function USING ERRCODE = '23514';
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
        $old$'city-openworld-v19','city-openworld-v20','city-openworld-v21','city-openworld-v22'$old$,
        $new$'city-openworld-v19','city-openworld-v20','city-openworld-v21','city-openworld-v22','city-openworld-v23'$new$);
    IF position($needle$city-openworld-v23$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V19 spatial-network bootstrap gate to V23' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_carrier_recovery_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_tick BIGINT;
    profile_tick BIGINT;
    profile_fundings BIGINT;
    profile_recoveries BIGINT;
    profile_funded BIGINT;
    profile_recovered BIGINT;
BEGIN
    SELECT current_tick INTO world_tick
    FROM city_worlds
    WHERE id = target_world_id AND simulation_version = 'city-openworld-v23';
    IF NOT FOUND THEN RETURN; END IF;

    SELECT baseline_tick, funding_count, recovery_count, funded_units, recovered_units
      INTO profile_tick, profile_fundings, profile_recoveries, profile_funded, profile_recovered
    FROM city_open_world_carrier_recovery_profiles
    WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick
       OR profile_fundings <> (SELECT COUNT(*) FROM city_open_world_carrier_reserve_fundings WHERE world_id = target_world_id)
       OR profile_recoveries <> (SELECT COUNT(*) FROM city_open_world_freight_claim_recoveries WHERE world_id = target_world_id)
       OR profile_funded <> COALESCE((SELECT SUM(amount_units) FROM city_open_world_carrier_reserve_fundings WHERE world_id = target_world_id), 0)
       OR profile_recovered <> COALESCE((SELECT SUM(amount_units) FROM city_open_world_freight_claim_recoveries WHERE world_id = target_world_id), 0)
       OR profile_recovered > profile_funded THEN
        RAISE EXCEPTION 'city open-world V23 carrier-recovery profile is missing or inconsistent' USING ERRCODE = '23514';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM city_open_world_actors actor
        WHERE actor.world_id = target_world_id
          AND actor.code = 'system.freight.carrier'
          AND actor.actor_type_code = 'system.freight_carrier'
          AND actor.status = 'active'
          AND actor.owner_user_id IS NULL
    ) OR NOT EXISTS (
        SELECT 1
        FROM city_economic_entities firm
        WHERE firm.world_id = target_world_id
          AND firm.entity_type = 'firm'
          AND firm.code = 'system_freight_reserve'
          AND firm.status = 'active'
          AND firm.owner_user_id IS NULL
          AND firm.metadata->>'opening_policy' = 'manual_reserve_only'
    ) THEN
        RAISE EXCEPTION 'city open-world V23 carrier-reserve identity is invalid' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_carrier_reserve_fundings funding
        JOIN city_commands command
          ON command.id = funding.source_command_id AND command.world_id = funding.world_id
        JOIN city_journals journal
          ON journal.id = funding.journal_id AND journal.world_id = funding.world_id
        WHERE funding.world_id = target_world_id
          AND (command.command_type <> 'open_world.carrier_reserve.fund'
               OR command.status <> 'applied'
               OR journal.journal_type <> 'subsidy'
               OR journal.source_command_id <> command.id
               OR journal.tick <> funding.funding_tick
               OR journal.posted_at IS NULL
               OR journal.operation_key <> 'open_world.carrier_reserve.fund.' || command.sequence::TEXT
               OR funding.metadata->>'contract' <> 'government_to_manual_carrier_reserve_v1'
               OR funding.metadata->>'carrier_firm' <> 'system_freight_reserve'
               OR funding.metadata->>'amount_units' <> funding.amount_units::TEXT
               OR journal.metadata->>'contract' <> 'government_to_manual_carrier_reserve_v1'
               OR journal.metadata->>'carrier_firm' <> 'system_freight_reserve'
               OR journal.metadata->>'amount_units' <> funding.amount_units::TEXT
               OR (SELECT COUNT(*) FROM city_journal_entries entry WHERE entry.journal_id = journal.id) <> 4
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_journal_entries entry
                    JOIN city_accounts account ON account.id = entry.account_id
                    JOIN city_economic_entities entity ON entity.id = account.entity_id
                    JOIN city_account_templates template ON template.id = account.template_id
                    WHERE entry.journal_id = journal.id
                      AND entity.world_id = funding.world_id
                      AND entity.entity_type = 'government'
                      AND template.code = 'subsidy_expense'
                      AND entry.debit_units = funding.amount_units AND entry.credit_units = 0
               )
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_journal_entries entry
                    JOIN city_accounts account ON account.id = entry.account_id
                    JOIN city_economic_entities entity ON entity.id = account.entity_id
                    JOIN city_account_templates template ON template.id = account.template_id
                    WHERE entry.journal_id = journal.id
                      AND entity.world_id = funding.world_id
                      AND entity.code = 'system_freight_reserve'
                      AND template.code = 'cash'
                      AND entry.debit_units = funding.amount_units AND entry.credit_units = 0
               )
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_journal_entries entry
                    JOIN city_accounts account ON account.id = entry.account_id
                    JOIN city_economic_entities entity ON entity.id = account.entity_id
                    JOIN city_account_templates template ON template.id = account.template_id
                    WHERE entry.journal_id = journal.id
                      AND entity.world_id = funding.world_id
                      AND entity.entity_type = 'government'
                      AND template.code = 'cash'
                      AND entry.debit_units = 0 AND entry.credit_units = funding.amount_units
               )
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_journal_entries entry
                    JOIN city_accounts account ON account.id = entry.account_id
                    JOIN city_economic_entities entity ON entity.id = account.entity_id
                    JOIN city_account_templates template ON template.id = account.template_id
                    WHERE entry.journal_id = journal.id
                      AND entity.world_id = funding.world_id
                      AND entity.code = 'system_freight_reserve'
                      AND template.code = 'equity'
                      AND entry.debit_units = 0 AND entry.credit_units = funding.amount_units
               ))
    ) THEN
        RAISE EXCEPTION 'city open-world V23 carrier reserve funding evidence is invalid' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_claim_recoveries recovery
        JOIN city_open_world_freight_settlement_claims claim
          ON claim.world_id = recovery.world_id AND claim.code = recovery.claim_code
        JOIN city_economic_entities seller
          ON seller.id = recovery.seller_firm_entity_id AND seller.world_id = recovery.world_id
        JOIN city_economic_entities carrier
          ON carrier.id = recovery.carrier_firm_entity_id AND carrier.world_id = recovery.world_id
        JOIN city_commands command
          ON command.id = recovery.source_command_id AND command.world_id = recovery.world_id
        JOIN city_journals journal
          ON journal.id = recovery.journal_id AND journal.world_id = recovery.world_id
        WHERE recovery.world_id = target_world_id
          AND (claim.case_code <> recovery.case_code
               OR claim.liability_party <> 'carrier'
               OR claim.state <> 'resolved'
               OR claim.claim_amount <> recovery.amount_units
               OR seller.entity_type <> 'firm' OR seller.status <> 'active'
               OR carrier.entity_type <> 'firm' OR carrier.status <> 'active'
               OR carrier.code <> 'system_freight_reserve'
               OR command.command_type <> 'open_world.freight_claim.resolve'
               OR command.status <> 'applied'
               OR journal.journal_type <> 'cash_transfer'
               OR journal.source_command_id <> command.id
               OR journal.tick <> recovery.recovery_tick
               OR journal.posted_at IS NULL
               OR journal.operation_key <> 'open_world.freight_claim.resolve.' || command.sequence::TEXT
               OR recovery.metadata->>'contract' <> 'carrier_claim_to_seller_cash_recovery_v1'
               OR recovery.metadata->>'claim_code' <> recovery.claim_code
               OR recovery.metadata->>'case_code' <> recovery.case_code
               OR recovery.metadata->>'seller_firm' <> seller.code
               OR recovery.metadata->>'carrier_firm' <> 'system_freight_reserve'
               OR recovery.metadata->>'amount_units' <> recovery.amount_units::TEXT
               OR journal.metadata->>'contract' <> 'carrier_claim_to_seller_cash_recovery_v1'
               OR journal.metadata->>'claim_code' <> recovery.claim_code
               OR journal.metadata->>'case_code' <> recovery.case_code
               OR journal.metadata->>'seller_firm' <> seller.code
               OR journal.metadata->>'carrier_firm' <> 'system_freight_reserve'
               OR journal.metadata->>'amount_units' <> recovery.amount_units::TEXT
               OR (SELECT COUNT(*) FROM city_journal_entries entry WHERE entry.journal_id = journal.id) <> 4
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_journal_entries entry
                    JOIN city_accounts account ON account.id = entry.account_id
                    JOIN city_economic_entities entity ON entity.id = account.entity_id
                    JOIN city_account_templates template ON template.id = account.template_id
                    WHERE entry.journal_id = journal.id
                      AND entity.world_id = recovery.world_id
                      AND entity.code = 'system_freight_reserve'
                      AND template.code = 'transfer_expense'
                      AND entry.debit_units = recovery.amount_units AND entry.credit_units = 0
               )
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_journal_entries entry
                    JOIN city_accounts account ON account.id = entry.account_id
                    JOIN city_economic_entities entity ON entity.id = account.entity_id
                    JOIN city_account_templates template ON template.id = account.template_id
                    WHERE entry.journal_id = journal.id
                      AND entity.world_id = recovery.world_id
                      AND entity.id = seller.id AND template.code = 'cash'
                      AND entry.debit_units = recovery.amount_units AND entry.credit_units = 0
               )
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_journal_entries entry
                    JOIN city_accounts account ON account.id = entry.account_id
                    JOIN city_economic_entities entity ON entity.id = account.entity_id
                    JOIN city_account_templates template ON template.id = account.template_id
                    WHERE entry.journal_id = journal.id
                      AND entity.world_id = recovery.world_id
                      AND entity.code = 'system_freight_reserve'
                      AND template.code = 'cash'
                      AND entry.debit_units = 0 AND entry.credit_units = recovery.amount_units
               )
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_journal_entries entry
                    JOIN city_accounts account ON account.id = entry.account_id
                    JOIN city_economic_entities entity ON entity.id = account.entity_id
                    JOIN city_account_templates template ON template.id = account.template_id
                    WHERE entry.journal_id = journal.id
                      AND entity.world_id = recovery.world_id
                      AND entity.id = seller.id AND template.code = 'other_income'
                      AND entry.debit_units = 0 AND entry.credit_units = recovery.amount_units
               )
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_open_world_freight_settlement_case_lines line
                    WHERE line.world_id = recovery.world_id
                      AND line.case_code = recovery.case_code
               )
               OR EXISTS (
                    SELECT 1
                    FROM city_open_world_freight_settlement_case_lines line
                    WHERE line.world_id = recovery.world_id
                      AND line.case_code = recovery.case_code
                      AND line.source_firm_code <> seller.code
               ))
    ) THEN
        RAISE EXCEPTION 'city open-world V23 freight claim recovery evidence is invalid' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_claims claim
        WHERE claim.world_id = target_world_id
          AND claim.liability_party = 'carrier'
          AND claim.state = 'resolved'
          AND NOT EXISTS (
                SELECT 1 FROM city_open_world_freight_claim_recoveries recovery
                WHERE recovery.world_id = claim.world_id AND recovery.claim_code = claim.code
          )
    ) THEN
        RAISE EXCEPTION 'city open-world V23 resolved carrier claim has no recovery evidence' USING ERRCODE = '23514';
    END IF;
END;
$$;
