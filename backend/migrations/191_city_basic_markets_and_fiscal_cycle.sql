-- 城市模拟 F4：确定性经济周期、劳动/基础商品/住房市场和政府财政清算。
-- 所有市场结算都必须引用 F2 journal 和 F3 resource operation；市场表只保存结算事实与读模型。

INSERT INTO city_account_templates
    (world_id, entity_type, code, name, account_class, normal_side,
     allow_negative, is_required, sort_order, metadata)
SELECT world.id, 'government', 'rental_revenue', 'Rental Revenue', 'revenue', 'credit',
       FALSE, TRUE, 75, '{}'::jsonb
FROM city_worlds world
ON CONFLICT (world_id, entity_type, code) DO NOTHING;

INSERT INTO city_accounts
    (world_id, entity_id, entity_type, monetary_unit_id, template_id,
     allow_negative, current_balance_units, version, status, metadata)
SELECT entity.world_id, entity.id, entity.entity_type, unit.id, template.id,
       template.allow_negative, 0, 0, 'active', '{}'::jsonb
FROM city_economic_entities entity
JOIN city_monetary_units unit
  ON unit.world_id = entity.world_id AND unit.is_base
JOIN city_account_templates template
  ON template.world_id = entity.world_id
 AND template.entity_type = entity.entity_type
 AND template.code = 'rental_revenue'
WHERE entity.entity_type = 'government'
ON CONFLICT (entity_id, monetary_unit_id, template_id) DO NOTHING;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'city_household_cohorts_id_world_unique'
    ) THEN
        ALTER TABLE city_household_cohorts
            ADD CONSTRAINT city_household_cohorts_id_world_unique UNIQUE (id, world_id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'city_government_budget_lines_id_world_unique'
    ) THEN
        ALTER TABLE city_government_budget_lines
            ADD CONSTRAINT city_government_budget_lines_id_world_unique UNIQUE (id, world_id);
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS city_economic_cycle_states (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    cycle_index BIGINT NOT NULL DEFAULT 0 CHECK (cycle_index >= 0),
    cadence_ticks INTEGER NOT NULL DEFAULT 24 CHECK (cadence_ticks BETWEEN 1 AND 8760),
    next_due_tick BIGINT NOT NULL DEFAULT 1 CHECK (next_due_tick > 0),
    last_settled_tick BIGINT CHECK (last_settled_tick IS NULL OR last_settled_tick > 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_economic_cycle_order_check CHECK (
        last_settled_tick IS NULL OR next_due_tick > last_settled_tick
    ),
    CONSTRAINT city_economic_cycle_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_economic_policies (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    labor_demand_capacity_milli INTEGER NOT NULL DEFAULT 1000 CHECK (
        labor_demand_capacity_milli BETWEEN 0 AND 1000
    ),
    goods_demand_population_divisor BIGINT NOT NULL DEFAULT 10 CHECK (
        goods_demand_population_divisor BETWEEN 1 AND 1000000
    ),
    household_wage_tax_milli INTEGER NOT NULL DEFAULT 100 CHECK (
        household_wage_tax_milli BETWEEN 0 AND 1000
    ),
    firm_sales_tax_milli INTEGER NOT NULL DEFAULT 50 CHECK (
        firm_sales_tax_milli BETWEEN 0 AND 1000
    ),
    procurement_share_milli INTEGER NOT NULL DEFAULT 250 CHECK (
        procurement_share_milli BETWEEN 0 AND 1000
    ),
    social_support_share_milli INTEGER NOT NULL DEFAULT 100 CHECK (
        social_support_share_milli BETWEEN 0 AND 1000
    ),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_economic_policy_spending_share_check CHECK (
        procurement_share_milli + social_support_share_milli <= 1000
    ),
    CONSTRAINT city_economic_policy_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_market_states (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    monetary_unit_id BIGINT NOT NULL,
    resource_id BIGINT,
    market_code VARCHAR(32) NOT NULL,
    quote_units BIGINT NOT NULL CHECK (quote_units > 0),
    floor_units BIGINT NOT NULL CHECK (floor_units > 0),
    ceiling_units BIGINT NOT NULL CHECK (ceiling_units >= floor_units),
    maximum_adjustment_milli INTEGER NOT NULL CHECK (maximum_adjustment_milli BETWEEN 1 AND 500),
    last_clearing_tick BIGINT CHECK (last_clearing_tick IS NULL OR last_clearing_tick > 0),
    last_clearing_price_units BIGINT CHECK (last_clearing_price_units IS NULL OR last_clearing_price_units > 0),
    last_demand_units BIGINT NOT NULL DEFAULT 0 CHECK (last_demand_units >= 0),
    last_supply_units BIGINT NOT NULL DEFAULT 0 CHECK (last_supply_units >= 0),
    last_cleared_units BIGINT NOT NULL DEFAULT 0 CHECK (last_cleared_units >= 0),
    last_unmet_demand_units BIGINT NOT NULL DEFAULT 0 CHECK (last_unmet_demand_units >= 0),
    last_excess_supply_units BIGINT NOT NULL DEFAULT 0 CHECK (last_excess_supply_units >= 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_market_states_unit_fk
        FOREIGN KEY (monetary_unit_id, world_id)
        REFERENCES city_monetary_units(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_market_states_resource_fk
        FOREIGN KEY (resource_id, world_id)
        REFERENCES city_resources(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_market_state_code_check CHECK (market_code IN ('labor', 'basic_goods', 'housing')),
    CONSTRAINT city_market_state_resource_check CHECK (
        (market_code = 'labor' AND resource_id IS NULL)
        OR (market_code IN ('basic_goods', 'housing') AND resource_id IS NOT NULL)
    ),
    CONSTRAINT city_market_state_quote_range_check CHECK (
        quote_units BETWEEN floor_units AND ceiling_units
        AND (last_clearing_price_units IS NULL OR last_clearing_price_units BETWEEN floor_units AND ceiling_units)
    ),
    CONSTRAINT city_market_state_last_quantity_check CHECK (
        last_cleared_units + last_unmet_demand_units = last_demand_units
        AND last_cleared_units + last_excess_supply_units = last_supply_units
    ),
    CONSTRAINT city_market_state_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_market_states_world_code_unique UNIQUE (world_id, market_code),
    CONSTRAINT city_market_states_id_world_unique UNIQUE (id, world_id)
);

CREATE TABLE IF NOT EXISTS city_market_settlements (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    monetary_unit_id BIGINT NOT NULL,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence SMALLINT NOT NULL CHECK (sequence BETWEEN 1 AND 16),
    cycle_index BIGINT NOT NULL CHECK (cycle_index > 0),
    settlement_key VARCHAR(128) NOT NULL,
    settlement_type VARCHAR(32) NOT NULL,
    clearing_price_units BIGINT NOT NULL DEFAULT 0 CHECK (clearing_price_units >= 0),
    demand_units BIGINT NOT NULL DEFAULT 0 CHECK (demand_units >= 0),
    supply_units BIGINT NOT NULL DEFAULT 0 CHECK (supply_units >= 0),
    cleared_units BIGINT NOT NULL DEFAULT 0 CHECK (cleared_units >= 0),
    unmet_demand_units BIGINT NOT NULL DEFAULT 0 CHECK (unmet_demand_units >= 0),
    excess_supply_units BIGINT NOT NULL DEFAULT 0 CHECK (excess_supply_units >= 0),
    gross_amount_units BIGINT NOT NULL DEFAULT 0 CHECK (gross_amount_units >= 0),
    journal_count INTEGER NOT NULL DEFAULT 0 CHECK (journal_count >= 0),
    resource_operation_count INTEGER NOT NULL DEFAULT 0 CHECK (resource_operation_count >= 0),
    allocation_count INTEGER NOT NULL DEFAULT 0 CHECK (allocation_count >= 0),
    budget_movement_count INTEGER NOT NULL DEFAULT 0 CHECK (budget_movement_count >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted_at TIMESTAMPTZ,
    CONSTRAINT city_market_settlements_unit_fk
        FOREIGN KEY (monetary_unit_id, world_id)
        REFERENCES city_monetary_units(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_market_settlements_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_market_settlement_key_check CHECK (
        char_length(settlement_key) BETWEEN 1 AND 128 AND settlement_key = btrim(settlement_key)
    ),
    CONSTRAINT city_market_settlement_type_check CHECK (
        settlement_type IN ('labor', 'basic_goods', 'housing', 'fiscal')
    ),
    CONSTRAINT city_market_settlement_price_check CHECK (
        (settlement_type = 'fiscal' AND clearing_price_units = 0)
        OR (settlement_type <> 'fiscal' AND clearing_price_units > 0)
    ),
    CONSTRAINT city_market_settlement_quantity_check CHECK (
        cleared_units + unmet_demand_units = demand_units
        AND cleared_units + excess_supply_units = supply_units
    ),
    CONSTRAINT city_market_settlement_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_market_settlement_posted_at_check CHECK (posted_at IS NULL OR posted_at >= created_at),
    CONSTRAINT city_market_settlements_world_key_unique UNIQUE (world_id, settlement_key),
    CONSTRAINT city_market_settlements_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_market_settlements_world_cycle_type_unique UNIQUE (world_id, cycle_index, settlement_type),
    CONSTRAINT city_market_settlements_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_market_settlements_id_world_unit_unique UNIQUE (id, world_id, monetary_unit_id)
);

CREATE INDEX IF NOT EXISTS idx_city_market_settlements_world_cursor
    ON city_market_settlements (world_id, tick, sequence);

ALTER TABLE city_journals
    ADD COLUMN IF NOT EXISTS market_settlement_id BIGINT;

ALTER TABLE city_journals
    DROP CONSTRAINT IF EXISTS city_journal_type_check;
ALTER TABLE city_journals
    ADD CONSTRAINT city_journal_type_check CHECK (
        journal_type IN (
            'opening', 'cash_transfer', 'wage', 'purchase', 'tax', 'subsidy',
            'rent', 'government_spend', 'reversal'
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

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'city_journals_market_settlement_fk'
    ) THEN
        ALTER TABLE city_journals
            ADD CONSTRAINT city_journals_market_settlement_fk
            FOREIGN KEY (market_settlement_id, world_id, monetary_unit_id)
            REFERENCES city_market_settlements(id, world_id, monetary_unit_id)
            ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_city_journals_market_settlement
    ON city_journals (market_settlement_id)
    WHERE market_settlement_id IS NOT NULL;

ALTER TABLE city_resource_operations
    ADD COLUMN IF NOT EXISTS market_settlement_id BIGINT;

ALTER TABLE city_resource_operations
    DROP CONSTRAINT IF EXISTS city_resource_operation_origin_check;
ALTER TABLE city_resource_operations
    ADD CONSTRAINT city_resource_operation_origin_check CHECK (
        (operation_type = 'opening'
            AND source_command_id IS NULL AND market_settlement_id IS NULL)
        OR (operation_type <> 'opening'
            AND ((source_command_id IS NOT NULL)::INTEGER + (market_settlement_id IS NOT NULL)::INTEGER) = 1)
    );

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'city_resource_operations_market_settlement_fk'
    ) THEN
        ALTER TABLE city_resource_operations
            ADD CONSTRAINT city_resource_operations_market_settlement_fk
            FOREIGN KEY (market_settlement_id, world_id)
            REFERENCES city_market_settlements(id, world_id)
            ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_city_resource_operations_market_settlement
    ON city_resource_operations (market_settlement_id)
    WHERE market_settlement_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS city_market_allocations (
    id BIGSERIAL PRIMARY KEY,
    settlement_id BIGINT NOT NULL,
    world_id BIGINT NOT NULL,
    monetary_unit_id BIGINT NOT NULL,
    line_no INTEGER NOT NULL CHECK (line_no > 0),
    allocation_type VARCHAR(24) NOT NULL,
    cohort_id BIGINT,
    from_entity_id BIGINT,
    to_entity_id BIGINT,
    district_id BIGINT,
    resource_id BIGINT,
    quantity_units BIGINT NOT NULL DEFAULT 0 CHECK (quantity_units >= 0),
    unit_price_units BIGINT NOT NULL DEFAULT 0 CHECK (unit_price_units >= 0),
    amount_units BIGINT NOT NULL DEFAULT 0 CHECK (amount_units >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_market_allocations_settlement_fk
        FOREIGN KEY (settlement_id, world_id, monetary_unit_id)
        REFERENCES city_market_settlements(id, world_id, monetary_unit_id) ON DELETE RESTRICT,
    CONSTRAINT city_market_allocations_cohort_fk
        FOREIGN KEY (cohort_id, world_id)
        REFERENCES city_household_cohorts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_market_allocations_from_entity_fk
        FOREIGN KEY (from_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_market_allocations_to_entity_fk
        FOREIGN KEY (to_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_market_allocations_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_market_allocations_resource_fk
        FOREIGN KEY (resource_id, world_id)
        REFERENCES city_resources(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_market_allocation_type_check CHECK (
        allocation_type IN ('employment', 'goods', 'housing', 'tax', 'spending')
    ),
    CONSTRAINT city_market_allocation_measure_check CHECK (
        (allocation_type IN ('employment', 'goods', 'housing')
            AND quantity_units > 0 AND unit_price_units > 0
            AND amount_units::NUMERIC = quantity_units::NUMERIC * unit_price_units::NUMERIC)
        OR (allocation_type IN ('tax', 'spending')
            AND quantity_units = 0 AND unit_price_units = 0 AND amount_units > 0)
    ),
    CONSTRAINT city_market_allocation_party_check CHECK (
        from_entity_id IS NOT NULL OR to_entity_id IS NOT NULL
    ),
    CONSTRAINT city_market_allocation_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_market_allocations_line_unique UNIQUE (settlement_id, line_no)
);

CREATE INDEX IF NOT EXISTS idx_city_market_allocations_world_cohort
    ON city_market_allocations (world_id, cohort_id, settlement_id);

CREATE TABLE IF NOT EXISTS city_housing_occupancies (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    cohort_id BIGINT NOT NULL,
    district_id BIGINT NOT NULL,
    occupied_units BIGINT NOT NULL DEFAULT 0 CHECK (occupied_units >= 0),
    unmet_units BIGINT NOT NULL DEFAULT 0 CHECK (unmet_units >= 0),
    rent_price_units BIGINT NOT NULL CHECK (rent_price_units > 0),
    last_settled_tick BIGINT CHECK (last_settled_tick IS NULL OR last_settled_tick > 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_housing_occupancies_cohort_fk
        FOREIGN KEY (cohort_id, world_id)
        REFERENCES city_household_cohorts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_housing_occupancies_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_housing_occupancy_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_housing_occupancies_world_cohort_unique UNIQUE (world_id, cohort_id)
);

CREATE TABLE IF NOT EXISTS city_budget_movements (
    id BIGSERIAL PRIMARY KEY,
    settlement_id BIGINT NOT NULL,
    world_id BIGINT NOT NULL,
    budget_line_id BIGINT NOT NULL,
    line_no INTEGER NOT NULL CHECK (line_no > 0),
    movement_type VARCHAR(16) NOT NULL,
    amount_units BIGINT NOT NULL CHECK (amount_units > 0),
    spent_before_units BIGINT NOT NULL CHECK (spent_before_units >= 0),
    spent_after_units BIGINT NOT NULL CHECK (spent_after_units >= 0),
    budget_version_before BIGINT NOT NULL CHECK (budget_version_before >= 0),
    budget_version_after BIGINT NOT NULL CHECK (budget_version_after > 0),
    memo VARCHAR(256) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_budget_movements_settlement_fk
        FOREIGN KEY (settlement_id, world_id)
        REFERENCES city_market_settlements(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_budget_movements_budget_fk
        FOREIGN KEY (budget_line_id, world_id)
        REFERENCES city_government_budget_lines(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_budget_movement_type_check CHECK (movement_type = 'spend'),
    CONSTRAINT city_budget_movement_projection_check CHECK (
        spent_after_units = spent_before_units + amount_units
        AND budget_version_after = budget_version_before + 1
    ),
    CONSTRAINT city_budget_movement_memo_check CHECK (char_length(memo) <= 256),
    CONSTRAINT city_budget_movements_line_unique UNIQUE (settlement_id, line_no)
);

CREATE INDEX IF NOT EXISTS idx_city_budget_movements_budget
    ON city_budget_movements (world_id, budget_line_id, id);

CREATE OR REPLACE FUNCTION city_f4_write_enabled()
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_f4_write', TRUE), '') = 'on'
$$;

CREATE OR REPLACE FUNCTION guard_city_economic_cycle_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT city_f4_write_enabled() THEN
        RAISE EXCEPTION 'city economic cycle can only advance through a posted market cycle'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_economic_policy_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT city_f4_write_enabled() THEN
        RAISE EXCEPTION 'city economic policy can only change through a versioned policy command'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_economic_policy_projection_guard ON city_economic_policies;
CREATE TRIGGER city_economic_policy_projection_guard
BEFORE UPDATE OR DELETE ON city_economic_policies
FOR EACH ROW EXECUTE FUNCTION guard_city_economic_policy_projection();

DROP TRIGGER IF EXISTS city_economic_cycle_projection_guard ON city_economic_cycle_states;
CREATE TRIGGER city_economic_cycle_projection_guard
BEFORE UPDATE OR DELETE ON city_economic_cycle_states
FOR EACH ROW EXECUTE FUNCTION guard_city_economic_cycle_projection();

CREATE OR REPLACE FUNCTION guard_city_market_state_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT city_f4_write_enabled() THEN
        RAISE EXCEPTION 'city market state can only change through a draft settlement'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_market_state_projection_guard ON city_market_states;
CREATE TRIGGER city_market_state_projection_guard
BEFORE UPDATE OR DELETE ON city_market_states
FOR EACH ROW EXECUTE FUNCTION guard_city_market_state_projection();

CREATE OR REPLACE FUNCTION guard_city_labor_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT city_f4_write_enabled() THEN
        IF TG_TABLE_NAME = 'city_household_cohorts'
           AND (NEW.employed_units, NEW.version) IS DISTINCT FROM (OLD.employed_units, OLD.version) THEN
            RAISE EXCEPTION 'city household employment can only change through labor settlement'
                USING ERRCODE = '55000';
        ELSIF TG_TABLE_NAME = 'city_firm_states'
           AND (NEW.employee_units, NEW.version) IS DISTINCT FROM (OLD.employee_units, OLD.version) THEN
            RAISE EXCEPTION 'city firm employment can only change through labor settlement'
                USING ERRCODE = '55000';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_household_employment_projection_guard ON city_household_cohorts;
CREATE TRIGGER city_household_employment_projection_guard
BEFORE UPDATE ON city_household_cohorts
FOR EACH ROW EXECUTE FUNCTION guard_city_labor_projection();

DROP TRIGGER IF EXISTS city_firm_employment_projection_guard ON city_firm_states;
CREATE TRIGGER city_firm_employment_projection_guard
BEFORE UPDATE ON city_firm_states
FOR EACH ROW EXECUTE FUNCTION guard_city_labor_projection();

CREATE OR REPLACE FUNCTION guard_city_housing_occupancy_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT city_f4_write_enabled() THEN
        RAISE EXCEPTION 'city housing occupancy can only change through housing settlement'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_housing_occupancy_projection_guard ON city_housing_occupancies;
CREATE TRIGGER city_housing_occupancy_projection_guard
BEFORE UPDATE OR DELETE ON city_housing_occupancies
FOR EACH ROW EXECUTE FUNCTION guard_city_housing_occupancy_projection();

CREATE OR REPLACE FUNCTION guard_city_budget_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT city_f4_write_enabled()
       AND (NEW.appropriated_units, NEW.committed_units, NEW.spent_units, NEW.version)
           IS DISTINCT FROM
           (OLD.appropriated_units, OLD.committed_units, OLD.spent_units, OLD.version) THEN
        RAISE EXCEPTION 'city budget projection can only change through a budget movement'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_budget_projection_guard ON city_government_budget_lines;
CREATE TRIGGER city_budget_projection_guard
BEFORE UPDATE ON city_government_budget_lines
FOR EACH ROW EXECUTE FUNCTION guard_city_budget_projection();

CREATE OR REPLACE FUNCTION guard_city_market_fact_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'city market allocations and budget movements are immutable facts'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_market_origin_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.market_settlement_id IS DISTINCT FROM OLD.market_settlement_id THEN
        RAISE EXCEPTION 'city market settlement origin is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_journal_market_origin_guard ON city_journals;
CREATE TRIGGER city_journal_market_origin_guard
BEFORE UPDATE ON city_journals
FOR EACH ROW EXECUTE FUNCTION guard_city_market_origin_immutable();

DROP TRIGGER IF EXISTS city_resource_market_origin_guard ON city_resource_operations;
CREATE TRIGGER city_resource_market_origin_guard
BEFORE UPDATE ON city_resource_operations
FOR EACH ROW EXECUTE FUNCTION guard_city_market_origin_immutable();

DROP TRIGGER IF EXISTS city_market_allocation_immutable_guard ON city_market_allocations;
CREATE TRIGGER city_market_allocation_immutable_guard
BEFORE UPDATE OR DELETE ON city_market_allocations
FOR EACH ROW EXECUTE FUNCTION guard_city_market_fact_immutable();

DROP TRIGGER IF EXISTS city_budget_movement_immutable_guard ON city_budget_movements;
CREATE TRIGGER city_budget_movement_immutable_guard
BEFORE UPDATE OR DELETE ON city_budget_movements
FOR EACH ROW EXECUTE FUNCTION guard_city_market_fact_immutable();

CREATE OR REPLACE FUNCTION guard_city_market_settlement_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.posted_at IS NOT NULL THEN
            RAISE EXCEPTION 'city market settlements must be inserted as drafts'
                USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' OR OLD.posted_at IS NOT NULL OR NEW.posted_at IS NULL
       OR (OLD.id, OLD.world_id, OLD.monetary_unit_id, OLD.tick, OLD.sequence,
           OLD.cycle_index, OLD.settlement_key, OLD.settlement_type,
           OLD.clearing_price_units, OLD.demand_units, OLD.supply_units,
           OLD.cleared_units, OLD.unmet_demand_units, OLD.excess_supply_units,
           OLD.gross_amount_units, OLD.metadata, OLD.created_at)
          IS DISTINCT FROM
          (NEW.id, NEW.world_id, NEW.monetary_unit_id, NEW.tick, NEW.sequence,
           NEW.cycle_index, NEW.settlement_key, NEW.settlement_type,
           NEW.clearing_price_units, NEW.demand_units, NEW.supply_units,
           NEW.cleared_units, NEW.unmet_demand_units, NEW.excess_supply_units,
           NEW.gross_amount_units, NEW.metadata, NEW.created_at) THEN
        RAISE EXCEPTION 'city market settlements permit only one draft-to-posted transition'
            USING ERRCODE = '55000';
    END IF;
    PERFORM assert_city_market_settlement_ready(NEW.id);
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION post_city_market_state(
    target_settlement_id BIGINT,
    target_market_state_id BIGINT,
    expected_version BIGINT,
    next_quote_units BIGINT
)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    settlement_row city_market_settlements%ROWTYPE;
    state_row city_market_states%ROWTYPE;
BEGIN
    SELECT * INTO settlement_row FROM city_market_settlements
    WHERE id = target_settlement_id FOR UPDATE;
    IF NOT FOUND OR settlement_row.posted_at IS NOT NULL OR settlement_row.settlement_type = 'fiscal' THEN
        RAISE EXCEPTION 'city market state requires a matching draft settlement' USING ERRCODE = '55000';
    END IF;
    SELECT * INTO state_row FROM city_market_states
    WHERE id = target_market_state_id FOR UPDATE;
    IF NOT FOUND OR state_row.world_id <> settlement_row.world_id
       OR state_row.market_code <> settlement_row.settlement_type
       OR state_row.version <> expected_version
       OR next_quote_units < state_row.floor_units OR next_quote_units > state_row.ceiling_units THEN
        RAISE EXCEPTION 'city market state version or scope mismatch' USING ERRCODE = '40001';
    END IF;
    PERFORM set_config('sub2api.city_f4_write', 'on', TRUE);
    UPDATE city_market_states
    SET quote_units = next_quote_units,
        last_clearing_tick = settlement_row.tick,
        last_clearing_price_units = settlement_row.clearing_price_units,
        last_demand_units = settlement_row.demand_units,
        last_supply_units = settlement_row.supply_units,
        last_cleared_units = settlement_row.cleared_units,
        last_unmet_demand_units = settlement_row.unmet_demand_units,
        last_excess_supply_units = settlement_row.excess_supply_units,
        version = version + 1,
        updated_at = NOW()
    WHERE id = target_market_state_id;
    PERFORM set_config('sub2api.city_f4_write', 'off', TRUE);
END;
$$;

CREATE OR REPLACE FUNCTION post_city_household_employment(
    target_cohort_id BIGINT,
    target_world_id BIGINT,
    expected_version BIGINT,
    target_employed_units BIGINT
)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    affected BIGINT;
BEGIN
    PERFORM set_config('sub2api.city_f4_write', 'on', TRUE);
    UPDATE city_household_cohorts
    SET employed_units = target_employed_units, version = version + 1, updated_at = NOW()
    WHERE id = target_cohort_id AND world_id = target_world_id
      AND version = expected_version AND target_employed_units BETWEEN 0 AND working_age_units;
    GET DIAGNOSTICS affected = ROW_COUNT;
    PERFORM set_config('sub2api.city_f4_write', 'off', TRUE);
    IF affected <> 1 THEN
        RAISE EXCEPTION 'city household employment version or quantity mismatch' USING ERRCODE = '40001';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION post_city_firm_employment(
    target_firm_entity_id BIGINT,
    target_world_id BIGINT,
    expected_version BIGINT,
    target_employee_units BIGINT
)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    affected BIGINT;
BEGIN
    PERFORM set_config('sub2api.city_f4_write', 'on', TRUE);
    UPDATE city_firm_states
    SET employee_units = target_employee_units, version = version + 1, updated_at = NOW()
    WHERE entity_id = target_firm_entity_id AND world_id = target_world_id
      AND version = expected_version
      AND target_employee_units BETWEEN 0 AND production_capacity_units;
    GET DIAGNOSTICS affected = ROW_COUNT;
    PERFORM set_config('sub2api.city_f4_write', 'off', TRUE);
    IF affected <> 1 THEN
        RAISE EXCEPTION 'city firm employment version or quantity mismatch' USING ERRCODE = '40001';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION post_city_housing_occupancy(
    target_settlement_id BIGINT,
    target_occupancy_id BIGINT,
    expected_version BIGINT,
    target_occupied_units BIGINT,
    target_unmet_units BIGINT,
    target_rent_price_units BIGINT
)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    settlement_row city_market_settlements%ROWTYPE;
    occupancy_row city_housing_occupancies%ROWTYPE;
BEGIN
    SELECT * INTO settlement_row FROM city_market_settlements
    WHERE id = target_settlement_id FOR UPDATE;
    IF NOT FOUND OR settlement_row.posted_at IS NOT NULL OR settlement_row.settlement_type <> 'housing' THEN
        RAISE EXCEPTION 'city housing occupancy requires a draft housing settlement' USING ERRCODE = '55000';
    END IF;
    SELECT * INTO occupancy_row FROM city_housing_occupancies
    WHERE id = target_occupancy_id FOR UPDATE;
    IF NOT FOUND OR occupancy_row.world_id <> settlement_row.world_id
       OR occupancy_row.version <> expected_version
       OR target_occupied_units < 0 OR target_unmet_units < 0 OR target_rent_price_units <= 0 THEN
        RAISE EXCEPTION 'city housing occupancy version or quantity mismatch' USING ERRCODE = '40001';
    END IF;
    PERFORM set_config('sub2api.city_f4_write', 'on', TRUE);
    UPDATE city_housing_occupancies
    SET occupied_units = target_occupied_units,
        unmet_units = target_unmet_units,
        rent_price_units = target_rent_price_units,
        last_settled_tick = settlement_row.tick,
        version = version + 1,
        updated_at = NOW()
    WHERE id = target_occupancy_id;
    PERFORM set_config('sub2api.city_f4_write', 'off', TRUE);
END;
$$;

CREATE OR REPLACE FUNCTION post_city_budget_spend(
    target_settlement_id BIGINT,
    target_budget_line_id BIGINT,
    target_line_no INTEGER,
    expected_version BIGINT,
    target_amount_units BIGINT,
    target_memo TEXT
)
RETURNS BIGINT
LANGUAGE plpgsql
AS $$
DECLARE
    settlement_row city_market_settlements%ROWTYPE;
    budget_row city_government_budget_lines%ROWTYPE;
    movement_id BIGINT;
BEGIN
    SELECT * INTO settlement_row FROM city_market_settlements
    WHERE id = target_settlement_id FOR UPDATE;
    IF NOT FOUND OR settlement_row.posted_at IS NOT NULL OR settlement_row.settlement_type <> 'fiscal' THEN
        RAISE EXCEPTION 'city budget spend requires a draft fiscal settlement' USING ERRCODE = '55000';
    END IF;
    SELECT * INTO budget_row FROM city_government_budget_lines
    WHERE id = target_budget_line_id FOR UPDATE;
    IF NOT FOUND OR budget_row.world_id <> settlement_row.world_id
       OR budget_row.version <> expected_version OR target_amount_units <= 0
       OR budget_row.spent_units::NUMERIC + target_amount_units::NUMERIC
          + budget_row.committed_units::NUMERIC > budget_row.appropriated_units::NUMERIC
       OR char_length(COALESCE(target_memo, '')) > 256 THEN
        RAISE EXCEPTION 'city budget spend exceeds authorization or version' USING ERRCODE = '23514';
    END IF;
    PERFORM set_config('sub2api.city_f4_write', 'on', TRUE);
    UPDATE city_government_budget_lines
    SET spent_units = spent_units + target_amount_units,
        version = version + 1,
        updated_at = NOW()
    WHERE id = target_budget_line_id;
    PERFORM set_config('sub2api.city_f4_write', 'off', TRUE);
    INSERT INTO city_budget_movements
        (settlement_id, world_id, budget_line_id, line_no, movement_type, amount_units,
         spent_before_units, spent_after_units, budget_version_before, budget_version_after, memo)
    VALUES
        (target_settlement_id, settlement_row.world_id, target_budget_line_id, target_line_no,
         'spend', target_amount_units, budget_row.spent_units,
         budget_row.spent_units + target_amount_units, budget_row.version, budget_row.version + 1,
         COALESCE(target_memo, ''))
    RETURNING id INTO movement_id;
    RETURN movement_id;
END;
$$;

CREATE OR REPLACE FUNCTION advance_city_economic_cycle(
    target_world_id BIGINT,
    expected_cycle_index BIGINT,
    target_tick BIGINT
)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    state_row city_economic_cycle_states%ROWTYPE;
BEGIN
    SELECT * INTO state_row FROM city_economic_cycle_states
    WHERE world_id = target_world_id FOR UPDATE;
    IF NOT FOUND OR state_row.cycle_index <> expected_cycle_index
       OR target_tick < state_row.next_due_tick THEN
        RAISE EXCEPTION 'city economic cycle version or due tick mismatch' USING ERRCODE = '40001';
    END IF;
    IF (SELECT COUNT(*) FROM city_market_settlements
        WHERE world_id = target_world_id AND cycle_index = expected_cycle_index + 1
          AND posted_at IS NOT NULL) <> 4 THEN
        RAISE EXCEPTION 'city economic cycle requires four posted settlements' USING ERRCODE = '23514';
    END IF;
    PERFORM set_config('sub2api.city_f4_write', 'on', TRUE);
    UPDATE city_economic_cycle_states
    SET cycle_index = cycle_index + 1,
        last_settled_tick = target_tick,
        next_due_tick = target_tick + cadence_ticks,
        version = version + 1,
        updated_at = NOW()
    WHERE world_id = target_world_id;
    PERFORM set_config('sub2api.city_f4_write', 'off', TRUE);
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_labor_projection(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    household_employment BIGINT;
    firm_employment BIGINT;
BEGIN
    SELECT COALESCE(SUM(employed_units), 0) INTO household_employment
    FROM city_household_cohorts WHERE world_id = target_world_id;
    SELECT COALESCE(SUM(employee_units), 0) INTO firm_employment
    FROM city_firm_states WHERE world_id = target_world_id;
    IF household_employment <> firm_employment THEN
        RAISE EXCEPTION 'city labor projection is not conserved' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_labor_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM assert_city_labor_projection(CASE WHEN TG_OP = 'DELETE' THEN OLD.world_id ELSE NEW.world_id END);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_household_labor_commit_check ON city_household_cohorts;
CREATE CONSTRAINT TRIGGER city_household_labor_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_household_cohorts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_labor_projection();

DROP TRIGGER IF EXISTS city_firm_labor_commit_check ON city_firm_states;
CREATE CONSTRAINT TRIGGER city_firm_labor_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_firm_states
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_labor_projection();

CREATE OR REPLACE FUNCTION assert_city_housing_projection(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    occupied BIGINT;
    housing_supply BIGINT;
    bad_cohorts BIGINT;
BEGIN
    SELECT COALESCE(SUM(occupied_units), 0) INTO occupied
    FROM city_housing_occupancies WHERE world_id = target_world_id;
    SELECT COALESCE(SUM(balance.quantity_units), 0) INTO housing_supply
    FROM city_inventory_balances balance
    JOIN city_resources resource ON resource.id = balance.resource_id
    WHERE balance.world_id = target_world_id AND resource.code = 'housing_units';
    SELECT COUNT(*) INTO bad_cohorts
    FROM city_housing_occupancies occupancy
    JOIN city_household_cohorts cohort ON cohort.id = occupancy.cohort_id
    WHERE occupancy.world_id = target_world_id
      AND occupancy.occupied_units + occupancy.unmet_units <> cohort.housing_demand_units;
    IF occupied > housing_supply OR bad_cohorts <> 0 THEN
        RAISE EXCEPTION 'city housing occupancy exceeds conserved supply or demand' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_housing_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM assert_city_housing_projection(CASE WHEN TG_OP = 'DELETE' THEN OLD.world_id ELSE NEW.world_id END);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_housing_projection_commit_check ON city_housing_occupancies;
CREATE CONSTRAINT TRIGGER city_housing_projection_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_housing_occupancies
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_housing_projection();

DROP TRIGGER IF EXISTS city_housing_inventory_commit_check ON city_inventory_balances;
CREATE CONSTRAINT TRIGGER city_housing_inventory_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_inventory_balances
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_housing_projection();

DROP TRIGGER IF EXISTS city_housing_demand_commit_check ON city_household_cohorts;
CREATE CONSTRAINT TRIGGER city_housing_demand_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_household_cohorts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_housing_projection();

CREATE OR REPLACE FUNCTION assert_city_market_settlement_ready(target_settlement_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    settlement_row city_market_settlements%ROWTYPE;
    actual_journals BIGINT;
    unposted_journals BIGINT;
    actual_operations BIGINT;
    unposted_operations BIGINT;
    actual_allocations BIGINT;
    allocation_amount BIGINT;
    actual_budget_movements BIGINT;
BEGIN
    SELECT * INTO settlement_row FROM city_market_settlements
    WHERE id = target_settlement_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city market settlement not found' USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*), COUNT(*) FILTER (WHERE posted_at IS NULL)
      INTO actual_journals, unposted_journals
    FROM city_journals WHERE market_settlement_id = target_settlement_id;
    SELECT COUNT(*), COUNT(*) FILTER (WHERE posted_at IS NULL)
      INTO actual_operations, unposted_operations
    FROM city_resource_operations WHERE market_settlement_id = target_settlement_id;
    SELECT COUNT(*), COALESCE(SUM(amount_units), 0)
      INTO actual_allocations, allocation_amount
    FROM city_market_allocations WHERE settlement_id = target_settlement_id;
    SELECT COUNT(*) INTO actual_budget_movements
    FROM city_budget_movements WHERE settlement_id = target_settlement_id;
    IF actual_journals <> settlement_row.journal_count OR unposted_journals <> 0
       OR actual_operations <> settlement_row.resource_operation_count OR unposted_operations <> 0
       OR actual_allocations <> settlement_row.allocation_count
       OR actual_budget_movements <> settlement_row.budget_movement_count
       OR allocation_amount <> settlement_row.gross_amount_units THEN
        RAISE EXCEPTION 'city market settlement summary does not match posted facts'
            USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_market_allocations allocation
        WHERE allocation.settlement_id = target_settlement_id
          AND NOT (
              (settlement_row.settlement_type = 'labor' AND allocation.allocation_type = 'employment')
              OR (settlement_row.settlement_type = 'basic_goods' AND allocation.allocation_type = 'goods')
              OR (settlement_row.settlement_type = 'housing' AND allocation.allocation_type = 'housing')
              OR (settlement_row.settlement_type = 'fiscal'
                  AND allocation.allocation_type IN ('tax', 'spending'))
          )
    ) THEN
        RAISE EXCEPTION 'city market settlement contains allocations from another phase'
            USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_journals journal
        WHERE journal.market_settlement_id = target_settlement_id
          AND NOT (
              (settlement_row.settlement_type = 'labor' AND journal.journal_type = 'wage')
              OR (settlement_row.settlement_type = 'basic_goods' AND journal.journal_type = 'purchase')
              OR (settlement_row.settlement_type = 'housing' AND journal.journal_type = 'rent')
              OR (settlement_row.settlement_type = 'fiscal'
                  AND journal.journal_type IN ('tax', 'subsidy', 'government_spend'))
          )
    ) OR EXISTS (
        SELECT 1 FROM city_resource_operations operation
        WHERE operation.market_settlement_id = target_settlement_id
          AND settlement_row.settlement_type <> 'basic_goods'
    ) THEN
        RAISE EXCEPTION 'city market settlement contains facts from another phase'
            USING ERRCODE = '23514';
    END IF;
    IF settlement_row.settlement_type <> 'fiscal' AND NOT EXISTS (
        SELECT 1 FROM city_market_states market
        WHERE market.world_id = settlement_row.world_id
          AND market.market_code = settlement_row.settlement_type
          AND market.last_clearing_tick = settlement_row.tick
          AND market.last_clearing_price_units = settlement_row.clearing_price_units
          AND market.last_demand_units = settlement_row.demand_units
          AND market.last_supply_units = settlement_row.supply_units
          AND market.last_cleared_units = settlement_row.cleared_units
          AND market.last_unmet_demand_units = settlement_row.unmet_demand_units
          AND market.last_excess_supply_units = settlement_row.excess_supply_units
    ) THEN
        RAISE EXCEPTION 'city market settlement projection was not advanced'
            USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_budget_movements movement
        JOIN city_government_budget_lines budget ON budget.id = movement.budget_line_id
        WHERE movement.settlement_id = target_settlement_id
          AND (budget.spent_units <> movement.spent_after_units
               OR budget.version <> movement.budget_version_after)
    ) THEN
        RAISE EXCEPTION 'city budget movement projection is stale'
            USING ERRCODE = '23514';
    END IF;
    IF settlement_row.settlement_type = 'labor' THEN
        PERFORM assert_city_labor_projection(settlement_row.world_id);
    ELSIF settlement_row.settlement_type = 'housing' THEN
        PERFORM assert_city_housing_projection(settlement_row.world_id);
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS city_market_settlement_write_guard ON city_market_settlements;
CREATE TRIGGER city_market_settlement_write_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_market_settlements
FOR EACH ROW EXECUTE FUNCTION guard_city_market_settlement_write();

CREATE OR REPLACE FUNCTION assert_city_market_settlement_committed()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    actual_posted_at TIMESTAMPTZ;
BEGIN
    SELECT posted_at INTO actual_posted_at
    FROM city_market_settlements WHERE id = NEW.id;
    IF actual_posted_at IS NULL THEN
        RAISE EXCEPTION 'city market settlement must be posted before commit' USING ERRCODE = '23514';
    END IF;
    PERFORM assert_city_market_settlement_ready(NEW.id);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_market_settlement_commit_check ON city_market_settlements;
CREATE CONSTRAINT TRIGGER city_market_settlement_commit_check
AFTER INSERT ON city_market_settlements
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_market_settlement_committed();

CREATE OR REPLACE FUNCTION initialize_city_f4_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    base_unit_id BIGINT;
    base_unit_scale INTEGER;
    firm_employment BIGINT;
    labor_supply BIGINT;
    allocated BIGINT;
BEGIN
    SELECT id, scale INTO base_unit_id, base_unit_scale
    FROM city_monetary_units WHERE world_id = target_world_id AND is_base;
    IF base_unit_id IS NULL THEN
        RAISE EXCEPTION 'city F4 foundation requires a base monetary unit' USING ERRCODE = '23514';
    END IF;
    INSERT INTO city_economic_cycle_states (world_id)
    VALUES (target_world_id)
    ON CONFLICT (world_id) DO NOTHING;

    INSERT INTO city_economic_policies (world_id, metadata)
    VALUES (target_world_id, jsonb_build_object('schema_version', 1))
    ON CONFLICT (world_id) DO NOTHING;

    INSERT INTO city_market_states
        (world_id, monetary_unit_id, resource_id, market_code, quote_units,
         floor_units, ceiling_units, maximum_adjustment_milli, metadata)
    SELECT target_world_id, base_unit_id, resource.id, seed.market_code,
           (seed.quote_major::NUMERIC * power(10::NUMERIC, base_unit_scale))::BIGINT,
           (seed.floor_major::NUMERIC * power(10::NUMERIC, base_unit_scale))::BIGINT,
           (seed.ceiling_major::NUMERIC * power(10::NUMERIC, base_unit_scale))::BIGINT,
           seed.maximum_adjustment_milli,
           jsonb_build_object('schema_version', 1, 'quantity_basis', seed.quantity_basis)
    FROM (VALUES
        ('labor', NULL::VARCHAR, 10::BIGINT, 8::BIGINT, 1000::BIGINT, 50, 'worker_cycle'),
        ('basic_goods', 'consumer_goods', 5::BIGINT, 1::BIGINT, 500::BIGINT, 50, 'goods_bundle'),
        ('housing', 'housing_units', 1::BIGINT, 1::BIGINT, 200::BIGINT, 50, 'dwelling_cycle')
    ) AS seed(market_code, resource_code, quote_major, floor_major, ceiling_major,
              maximum_adjustment_milli, quantity_basis)
    LEFT JOIN city_resources resource
      ON resource.world_id = target_world_id AND resource.code = seed.resource_code
    ON CONFLICT (world_id, market_code) DO NOTHING;

    INSERT INTO city_housing_occupancies
        (world_id, cohort_id, district_id, occupied_units, unmet_units, rent_price_units)
    SELECT cohort.world_id, cohort.id, cohort.district_id, 0, cohort.housing_demand_units,
           market.quote_units
    FROM city_household_cohorts cohort
    JOIN city_market_states market
      ON market.world_id = cohort.world_id AND market.market_code = 'housing'
    WHERE cohort.world_id = target_world_id
    ON CONFLICT (world_id, cohort_id) DO NOTHING;

    SELECT COALESCE(SUM(employee_units), 0) INTO firm_employment
    FROM city_firm_states WHERE world_id = target_world_id;
    SELECT COALESCE(SUM(working_age_units), 0) INTO labor_supply
    FROM city_household_cohorts WHERE world_id = target_world_id;
    IF firm_employment > labor_supply THEN
        RAISE EXCEPTION 'city F4 opening employment exceeds labor supply' USING ERRCODE = '23514';
    END IF;
    PERFORM set_config('sub2api.city_f4_write', 'on', TRUE);
    WITH shares AS (
        SELECT cohort.id,
               CASE WHEN labor_supply = 0 THEN 0
                    ELSE (cohort.working_age_units * firm_employment) / labor_supply END AS base_units,
               CASE WHEN labor_supply = 0 THEN 0
                    ELSE (cohort.working_age_units * firm_employment) % labor_supply END AS remainder_units
        FROM city_household_cohorts cohort
        WHERE cohort.world_id = target_world_id
    ), totals AS (
        SELECT COALESCE(SUM(base_units), 0) AS base_total FROM shares
    ), ranked AS (
        SELECT shares.*,
               ROW_NUMBER() OVER (ORDER BY remainder_units DESC, id ASC) AS remainder_rank,
               firm_employment - totals.base_total AS remainder_count
        FROM shares CROSS JOIN totals
    )
    UPDATE city_household_cohorts cohort
    SET employed_units = ranked.base_units
        + CASE WHEN ranked.remainder_rank <= ranked.remainder_count THEN 1 ELSE 0 END,
        version = version + 1,
        updated_at = NOW()
    FROM ranked WHERE cohort.id = ranked.id;
    PERFORM set_config('sub2api.city_f4_write', 'off', TRUE);
    SELECT COALESCE(SUM(employed_units), 0) INTO allocated
    FROM city_household_cohorts WHERE world_id = target_world_id;
    IF allocated <> firm_employment THEN
        RAISE EXCEPTION 'city F4 opening labor allocation is not conserved' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_f4_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    cycle_count BIGINT;
    policy_count BIGINT;
    market_count BIGINT;
    occupancy_count BIGINT;
    cohort_count BIGINT;
    bad_market_count BIGINT;
BEGIN
    SELECT COUNT(*) INTO cycle_count FROM city_economic_cycle_states WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO policy_count FROM city_economic_policies WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO market_count FROM city_market_states WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO occupancy_count FROM city_housing_occupancies WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO cohort_count FROM city_household_cohorts WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO bad_market_count
    FROM city_market_states market
    LEFT JOIN city_resources resource ON resource.id = market.resource_id
    WHERE market.world_id = target_world_id
      AND ((market.market_code = 'basic_goods' AND resource.code <> 'consumer_goods')
        OR (market.market_code = 'housing' AND resource.code <> 'housing_units'));
    IF cycle_count <> 1 OR policy_count <> 1 OR market_count <> 3 OR occupancy_count <> cohort_count
       OR bad_market_count <> 0 THEN
        RAISE EXCEPTION 'city world % has an incomplete F4 foundation', target_world_id
            USING ERRCODE = '23514';
    END IF;
    PERFORM assert_city_labor_projection(target_world_id);
    PERFORM assert_city_housing_projection(target_world_id);
END;
$$;

SELECT initialize_city_f4_foundation(id) FROM city_worlds;
SELECT assert_city_f4_foundation(id) FROM city_worlds;

UPDATE city_worlds
SET simulation_version = 'city-f4-v1', state_hash = NULL
WHERE simulation_version = 'city-f3-v1';
