-- 城市模拟 F3：区域、人口/企业/政府状态，以及受不可变操作保护的实物库存。
-- 金额继续只由 F2 复式总账承载；本迁移只记录实体能力、预算授权和实物数量。

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'city_economic_entities_id_world_unique'
    ) THEN
        ALTER TABLE city_economic_entities
            ADD CONSTRAINT city_economic_entities_id_world_unique UNIQUE (id, world_id);
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS city_districts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(32) NOT NULL,
    name VARCHAR(64) NOT NULL,
    sort_order SMALLINT NOT NULL CHECK (sort_order >= 0),
    area_units BIGINT NOT NULL CHECK (area_units > 0),
    developable_area_units BIGINT NOT NULL CHECK (
        developable_area_units >= 0 AND developable_area_units <= area_units
    ),
    residential_capacity_units BIGINT NOT NULL DEFAULT 0 CHECK (residential_capacity_units >= 0),
    commercial_capacity_units BIGINT NOT NULL DEFAULT 0 CHECK (commercial_capacity_units >= 0),
    industrial_capacity_units BIGINT NOT NULL DEFAULT 0 CHECK (industrial_capacity_units >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_district_code_check CHECK (code ~ '^[a-z][a-z0-9_]{1,31}$'),
    CONSTRAINT city_district_name_check CHECK (char_length(btrim(name)) BETWEEN 1 AND 64),
    CONSTRAINT city_district_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_districts_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_districts_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_districts_world_order
    ON city_districts (world_id, sort_order, code);

CREATE TABLE IF NOT EXISTS city_household_cohorts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    district_id BIGINT NOT NULL,
    entity_id BIGINT NOT NULL,
    entity_type VARCHAR(16) NOT NULL DEFAULT 'household',
    income_band VARCHAR(16) NOT NULL,
    population_units BIGINT NOT NULL CHECK (population_units > 0),
    working_age_units BIGINT NOT NULL CHECK (working_age_units >= 0),
    employed_units BIGINT NOT NULL CHECK (employed_units >= 0),
    housing_demand_units BIGINT NOT NULL CHECK (housing_demand_units >= 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_household_cohorts_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_household_cohorts_entity_fk
        FOREIGN KEY (entity_id, world_id, entity_type)
        REFERENCES city_economic_entities(id, world_id, entity_type) ON DELETE RESTRICT,
    CONSTRAINT city_household_cohort_entity_type_check CHECK (entity_type = 'household'),
    CONSTRAINT city_household_cohort_income_band_check CHECK (income_band IN ('low', 'middle', 'high')),
    CONSTRAINT city_household_cohort_population_check CHECK (
        working_age_units <= population_units
        AND employed_units <= working_age_units
        AND housing_demand_units <= population_units
    ),
    CONSTRAINT city_household_cohort_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_household_cohorts_world_district_band_unique
        UNIQUE (world_id, district_id, income_band)
);

CREATE INDEX IF NOT EXISTS idx_city_household_cohorts_world_entity
    ON city_household_cohorts (world_id, entity_id, district_id, income_band);

CREATE TABLE IF NOT EXISTS city_firm_states (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    entity_id BIGINT NOT NULL,
    entity_type VARCHAR(16) NOT NULL DEFAULT 'firm',
    district_id BIGINT NOT NULL,
    industry_code VARCHAR(32) NOT NULL,
    employee_units BIGINT NOT NULL DEFAULT 0 CHECK (employee_units >= 0),
    capital_stock_units BIGINT NOT NULL DEFAULT 0 CHECK (capital_stock_units >= 0),
    production_capacity_units BIGINT NOT NULL CHECK (production_capacity_units > 0),
    productivity_milli BIGINT NOT NULL DEFAULT 1000 CHECK (productivity_milli BETWEEN 1 AND 1000000),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_firm_states_entity_fk
        FOREIGN KEY (entity_id, world_id, entity_type)
        REFERENCES city_economic_entities(id, world_id, entity_type) ON DELETE RESTRICT,
    CONSTRAINT city_firm_states_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_firm_state_entity_type_check CHECK (entity_type = 'firm'),
    CONSTRAINT city_firm_state_industry_code_check CHECK (industry_code ~ '^[a-z][a-z0-9_]{1,31}$'),
    CONSTRAINT city_firm_state_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_firm_states_world_entity_unique UNIQUE (world_id, entity_id),
    CONSTRAINT city_firm_states_entity_world_unique UNIQUE (entity_id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_firm_states_world_district
    ON city_firm_states (world_id, district_id, industry_code, entity_id);

CREATE TABLE IF NOT EXISTS city_government_states (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    entity_id BIGINT NOT NULL,
    entity_type VARCHAR(16) NOT NULL DEFAULT 'government',
    administrative_capacity_units BIGINT NOT NULL CHECK (administrative_capacity_units > 0),
    public_service_capacity_units BIGINT NOT NULL CHECK (public_service_capacity_units > 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_government_states_entity_fk
        FOREIGN KEY (entity_id, world_id, entity_type)
        REFERENCES city_economic_entities(id, world_id, entity_type) ON DELETE RESTRICT,
    CONSTRAINT city_government_state_entity_type_check CHECK (entity_type = 'government'),
    CONSTRAINT city_government_state_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_government_states_world_unique UNIQUE (world_id),
    CONSTRAINT city_government_states_entity_world_unique UNIQUE (entity_id, world_id)
);

-- 预算行是支出授权上限，不是现金余额；实际资金仍以 F2 journal/account 为唯一事实来源。
CREATE TABLE IF NOT EXISTS city_government_budget_lines (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    government_entity_id BIGINT NOT NULL,
    monetary_unit_id BIGINT NOT NULL,
    code VARCHAR(32) NOT NULL,
    name VARCHAR(64) NOT NULL,
    appropriated_units BIGINT NOT NULL CHECK (appropriated_units >= 0),
    committed_units BIGINT NOT NULL DEFAULT 0 CHECK (committed_units >= 0),
    spent_units BIGINT NOT NULL DEFAULT 0 CHECK (spent_units >= 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_government_budget_entity_fk
        FOREIGN KEY (government_entity_id, world_id)
        REFERENCES city_government_states(entity_id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_government_budget_unit_fk
        FOREIGN KEY (monetary_unit_id, world_id)
        REFERENCES city_monetary_units(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_government_budget_code_check CHECK (code ~ '^[a-z][a-z0-9_]{1,31}$'),
    CONSTRAINT city_government_budget_name_check CHECK (char_length(btrim(name)) BETWEEN 1 AND 64),
    CONSTRAINT city_government_budget_envelope_check CHECK (
        committed_units::numeric + spent_units::numeric <= appropriated_units::numeric
    ),
    CONSTRAINT city_government_budget_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_government_budget_world_code_unique UNIQUE (world_id, code)
);

CREATE TABLE IF NOT EXISTS city_resources (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(48) NOT NULL,
    name VARCHAR(96) NOT NULL,
    resource_kind VARCHAR(24) NOT NULL,
    unit_code VARCHAR(24) NOT NULL,
    unit_scale SMALLINT NOT NULL DEFAULT 0 CHECK (unit_scale BETWEEN 0 AND 6),
    storable BOOLEAN NOT NULL DEFAULT TRUE,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_resource_code_check CHECK (code ~ '^[a-z][a-z0-9_]{1,47}$'),
    CONSTRAINT city_resource_name_check CHECK (char_length(btrim(name)) BETWEEN 1 AND 96),
    CONSTRAINT city_resource_kind_check CHECK (
        resource_kind IN ('raw_material', 'consumer_good', 'capital_good', 'housing', 'land')
    ),
    CONSTRAINT city_resource_unit_code_check CHECK (unit_code ~ '^[a-z][a-z0-9_]{0,23}$'),
    CONSTRAINT city_resource_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT city_resource_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_resources_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_resources_id_world_unique UNIQUE (id, world_id)
);

CREATE TABLE IF NOT EXISTS city_inventory_balances (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    entity_id BIGINT NOT NULL,
    entity_type VARCHAR(16) NOT NULL,
    district_id BIGINT NOT NULL,
    resource_id BIGINT NOT NULL,
    opening_quantity_units BIGINT NOT NULL DEFAULT 0 CHECK (opening_quantity_units >= 0),
    quantity_units BIGINT NOT NULL DEFAULT 0 CHECK (quantity_units >= 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_inventory_balances_entity_fk
        FOREIGN KEY (entity_id, world_id, entity_type)
        REFERENCES city_economic_entities(id, world_id, entity_type) ON DELETE RESTRICT,
    CONSTRAINT city_inventory_balances_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_inventory_balances_resource_fk
        FOREIGN KEY (resource_id, world_id)
        REFERENCES city_resources(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_inventory_balance_entity_type_check CHECK (
        entity_type IN ('household', 'firm', 'government')
    ),
    CONSTRAINT city_inventory_balance_status_check CHECK (status IN ('active', 'closed')),
    CONSTRAINT city_inventory_balance_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_inventory_balances_scope_unique
        UNIQUE (world_id, entity_id, district_id, resource_id),
    CONSTRAINT city_inventory_balances_id_world_resource_unique
        UNIQUE (id, world_id, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_city_inventory_balances_world_entity
    ON city_inventory_balances (world_id, entity_id, district_id, resource_id);

CREATE TABLE IF NOT EXISTS city_production_recipes (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(48) NOT NULL,
    name VARCHAR(96) NOT NULL,
    industry_code VARCHAR(32) NOT NULL,
    capacity_units_per_batch BIGINT NOT NULL CHECK (capacity_units_per_batch > 0),
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_production_recipe_code_check CHECK (code ~ '^[a-z][a-z0-9_]{1,47}$'),
    CONSTRAINT city_production_recipe_name_check CHECK (char_length(btrim(name)) BETWEEN 1 AND 96),
    CONSTRAINT city_production_recipe_industry_check CHECK (industry_code ~ '^[a-z][a-z0-9_]{1,31}$'),
    CONSTRAINT city_production_recipe_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT city_production_recipe_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_production_recipes_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_production_recipes_id_world_unique UNIQUE (id, world_id)
);

CREATE TABLE IF NOT EXISTS city_production_recipe_lines (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    recipe_id BIGINT NOT NULL,
    resource_id BIGINT NOT NULL,
    direction VARCHAR(8) NOT NULL,
    quantity_units BIGINT NOT NULL CHECK (quantity_units > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_production_recipe_lines_recipe_fk
        FOREIGN KEY (recipe_id, world_id)
        REFERENCES city_production_recipes(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_production_recipe_lines_resource_fk
        FOREIGN KEY (resource_id, world_id)
        REFERENCES city_resources(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_production_recipe_line_direction_check CHECK (direction IN ('input', 'output')),
    CONSTRAINT city_production_recipe_lines_resource_unique UNIQUE (recipe_id, resource_id)
);

CREATE TABLE IF NOT EXISTS city_firm_recipes (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    firm_entity_id BIGINT NOT NULL,
    recipe_id BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_firm_recipes_firm_fk
        FOREIGN KEY (firm_entity_id, world_id)
        REFERENCES city_firm_states(entity_id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_firm_recipes_recipe_fk
        FOREIGN KEY (recipe_id, world_id)
        REFERENCES city_production_recipes(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_firm_recipe_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT city_firm_recipes_unique UNIQUE (world_id, firm_entity_id, recipe_id)
);

CREATE TABLE IF NOT EXISTS city_resource_operations (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    operation_key VARCHAR(128) NOT NULL,
    operation_type VARCHAR(24) NOT NULL,
    source_command_id BIGINT,
    actor_entity_id BIGINT NOT NULL,
    district_id BIGINT NOT NULL,
    recipe_id BIGINT,
    batch_count BIGINT,
    description VARCHAR(256) NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted_at TIMESTAMPTZ,
    CONSTRAINT city_resource_operations_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_resource_operations_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_resource_operations_actor_fk
        FOREIGN KEY (actor_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_resource_operations_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_resource_operations_recipe_fk
        FOREIGN KEY (recipe_id, world_id)
        REFERENCES city_production_recipes(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_resource_operation_key_check CHECK (
        char_length(operation_key) BETWEEN 1 AND 128 AND operation_key = btrim(operation_key)
    ),
    CONSTRAINT city_resource_operation_type_check CHECK (
        operation_type IN ('opening', 'transfer', 'production', 'consumption')
    ),
    CONSTRAINT city_resource_operation_origin_check CHECK (
        (operation_type = 'opening' AND source_command_id IS NULL)
        OR (operation_type <> 'opening' AND source_command_id IS NOT NULL)
    ),
    CONSTRAINT city_resource_operation_recipe_check CHECK (
        (operation_type = 'production' AND recipe_id IS NOT NULL AND batch_count > 0)
        OR (operation_type <> 'production' AND recipe_id IS NULL AND batch_count IS NULL)
    ),
    CONSTRAINT city_resource_operation_description_check CHECK (char_length(description) <= 256),
    CONSTRAINT city_resource_operation_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_resource_operation_posted_at_check CHECK (posted_at IS NULL OR posted_at >= created_at),
    CONSTRAINT city_resource_operations_world_key_unique UNIQUE (world_id, operation_key),
    CONSTRAINT city_resource_operations_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_resource_operations_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_resource_operations_world_cursor
    ON city_resource_operations (world_id, tick, sequence);
CREATE UNIQUE INDEX IF NOT EXISTS idx_city_resource_operations_one_per_command
    ON city_resource_operations (source_command_id)
    WHERE source_command_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_city_resource_operations_one_opening_per_scope
    ON city_resource_operations (world_id, actor_entity_id, district_id)
    WHERE operation_type = 'opening';

CREATE TABLE IF NOT EXISTS city_resource_entries (
    id BIGSERIAL PRIMARY KEY,
    operation_id BIGINT NOT NULL,
    world_id BIGINT NOT NULL,
    resource_id BIGINT NOT NULL,
    balance_id BIGINT NOT NULL,
    line_no INTEGER NOT NULL CHECK (line_no > 0),
    direction VARCHAR(8) NOT NULL,
    quantity_units BIGINT NOT NULL CHECK (quantity_units > 0),
    quantity_before_units BIGINT NOT NULL CHECK (quantity_before_units >= 0),
    quantity_after_units BIGINT NOT NULL CHECK (quantity_after_units >= 0),
    balance_version_before BIGINT NOT NULL CHECK (balance_version_before >= 0),
    balance_version_after BIGINT NOT NULL CHECK (balance_version_after > 0),
    memo VARCHAR(256) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_resource_entries_operation_fk
        FOREIGN KEY (operation_id, world_id)
        REFERENCES city_resource_operations(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_resource_entries_balance_fk
        FOREIGN KEY (balance_id, world_id, resource_id)
        REFERENCES city_inventory_balances(id, world_id, resource_id) ON DELETE RESTRICT,
    CONSTRAINT city_resource_entry_direction_check CHECK (direction IN ('in', 'out')),
    CONSTRAINT city_resource_entry_projection_check CHECK (
        (direction = 'in' AND quantity_after_units::numeric = quantity_before_units::numeric + quantity_units::numeric)
        OR (direction = 'out' AND quantity_before_units::numeric = quantity_after_units::numeric + quantity_units::numeric)
    ),
    CONSTRAINT city_resource_entry_version_check CHECK (
        balance_version_after = balance_version_before + 1
    ),
    CONSTRAINT city_resource_entry_memo_check CHECK (char_length(memo) <= 256),
    CONSTRAINT city_resource_entries_line_unique UNIQUE (operation_id, line_no),
    CONSTRAINT city_resource_entries_balance_unique UNIQUE (operation_id, balance_id)
);

CREATE INDEX IF NOT EXISTS idx_city_resource_entries_balance
    ON city_resource_entries (world_id, balance_id, id);

CREATE OR REPLACE FUNCTION guard_city_inventory_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    active_operation TEXT;
    operation_is_draft BOOLEAN;
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.world_id IS DISTINCT FROM OLD.world_id
       OR NEW.entity_id IS DISTINCT FROM OLD.entity_id
       OR NEW.entity_type IS DISTINCT FROM OLD.entity_type
       OR NEW.district_id IS DISTINCT FROM OLD.district_id
       OR NEW.resource_id IS DISTINCT FROM OLD.resource_id
       OR NEW.opening_quantity_units IS DISTINCT FROM OLD.opening_quantity_units
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'city inventory balance identity is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF NEW.quantity_units IS NOT DISTINCT FROM OLD.quantity_units
       AND NEW.version IS NOT DISTINCT FROM OLD.version THEN
        RETURN NEW;
    END IF;

    active_operation := current_setting('city.active_resource_operation_id', TRUE);
    IF active_operation IS NULL OR active_operation !~ '^[1-9][0-9]*$' THEN
        RAISE EXCEPTION 'city inventory balances can only change through a draft resource operation'
            USING ERRCODE = '55000';
    END IF;

    SELECT posted_at IS NULL
    INTO operation_is_draft
    FROM city_resource_operations
    WHERE id = active_operation::BIGINT AND world_id = NEW.world_id;

    IF operation_is_draft IS DISTINCT FROM TRUE
       OR NEW.version <> OLD.version + 1
       OR NEW.status IS DISTINCT FROM OLD.status
       OR NEW.metadata IS DISTINCT FROM OLD.metadata THEN
        RAISE EXCEPTION 'invalid city inventory projection update'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_inventory_projection_guard ON city_inventory_balances;
CREATE TRIGGER city_inventory_projection_guard
BEFORE UPDATE ON city_inventory_balances
FOR EACH ROW EXECUTE FUNCTION guard_city_inventory_projection();

CREATE OR REPLACE FUNCTION post_city_resource_entry(
    target_operation_id BIGINT,
    target_balance_id BIGINT,
    target_line_no INTEGER,
    target_direction VARCHAR,
    target_quantity_units BIGINT,
    target_memo VARCHAR
)
RETURNS BIGINT
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
    operation_is_draft BOOLEAN;
    target_resource_id BIGINT;
    balance_status VARCHAR(16);
    quantity_before BIGINT;
    quantity_after BIGINT;
    version_before BIGINT;
    created_entry_id BIGINT;
BEGIN
    IF target_line_no IS NULL OR target_line_no <= 0
       OR target_direction NOT IN ('in', 'out')
       OR target_quantity_units IS NULL OR target_quantity_units <= 0
       OR char_length(COALESCE(target_memo, '')) > 256 THEN
        RAISE EXCEPTION 'invalid city resource entry' USING ERRCODE = '23514';
    END IF;

    SELECT world_id, posted_at IS NULL
    INTO target_world_id, operation_is_draft
    FROM city_resource_operations
    WHERE id = target_operation_id
    FOR UPDATE;

    IF operation_is_draft IS DISTINCT FROM TRUE THEN
        RAISE EXCEPTION 'city resource operation % is sealed or missing', target_operation_id
            USING ERRCODE = '55000';
    END IF;

    SELECT resource_id, status, quantity_units, version
    INTO target_resource_id, balance_status, quantity_before, version_before
    FROM city_inventory_balances
    WHERE id = target_balance_id AND world_id = target_world_id
    FOR UPDATE;

    IF balance_status IS DISTINCT FROM 'active' THEN
        RAISE EXCEPTION 'city inventory balance % is inactive or missing', target_balance_id
            USING ERRCODE = '23514';
    END IF;

    IF target_direction = 'in' THEN
        IF quantity_before > 9223372036854775807 - target_quantity_units THEN
            RAISE EXCEPTION 'city inventory quantity overflow' USING ERRCODE = '22003';
        END IF;
        quantity_after := quantity_before + target_quantity_units;
    ELSE
        IF quantity_before < target_quantity_units THEN
            RAISE EXCEPTION 'city inventory has insufficient quantity' USING ERRCODE = '23514';
        END IF;
        quantity_after := quantity_before - target_quantity_units;
    END IF;

    PERFORM set_config('city.active_resource_operation_id', target_operation_id::TEXT, TRUE);
    UPDATE city_inventory_balances
    SET quantity_units = quantity_after, version = version_before + 1, updated_at = NOW()
    WHERE id = target_balance_id AND world_id = target_world_id AND version = version_before;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city inventory projection version conflict' USING ERRCODE = '40001';
    END IF;

    INSERT INTO city_resource_entries
        (operation_id, world_id, resource_id, balance_id, line_no, direction,
         quantity_units, quantity_before_units, quantity_after_units,
         balance_version_before, balance_version_after, memo)
    VALUES
        (target_operation_id, target_world_id, target_resource_id, target_balance_id,
         target_line_no, target_direction, target_quantity_units, quantity_before,
         quantity_after, version_before, version_before + 1, COALESCE(target_memo, ''))
    RETURNING id INTO created_entry_id;
    RETURN created_entry_id;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_resource_entry_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    active_operation TEXT;
    operation_is_draft BOOLEAN;
    current_quantity BIGINT;
    current_version BIGINT;
BEGIN
    active_operation := current_setting('city.active_resource_operation_id', TRUE);
    IF active_operation IS NULL OR active_operation !~ '^[1-9][0-9]*$'
       OR active_operation::BIGINT <> NEW.operation_id THEN
        RAISE EXCEPTION 'city resource entries can only be created by post_city_resource_entry'
            USING ERRCODE = '55000';
    END IF;

    SELECT posted_at IS NULL INTO operation_is_draft
    FROM city_resource_operations
    WHERE id = NEW.operation_id AND world_id = NEW.world_id;
    SELECT quantity_units, version INTO current_quantity, current_version
    FROM city_inventory_balances
    WHERE id = NEW.balance_id AND world_id = NEW.world_id AND resource_id = NEW.resource_id;

    IF operation_is_draft IS DISTINCT FROM TRUE
       OR current_quantity IS DISTINCT FROM NEW.quantity_after_units
       OR current_version IS DISTINCT FROM NEW.balance_version_after THEN
        RAISE EXCEPTION 'city resource entry must match its locked inventory projection'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_resource_entry_insert_guard ON city_resource_entries;
CREATE TRIGGER city_resource_entry_insert_guard
BEFORE INSERT ON city_resource_entries
FOR EACH ROW EXECUTE FUNCTION guard_city_resource_entry_insert();

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
        IF expected_command_type IS DISTINCT FROM required_command_type THEN
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

CREATE OR REPLACE FUNCTION guard_city_resource_operation_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.posted_at IS NOT NULL THEN
            RAISE EXCEPTION 'city resource operations must be inserted as drafts'
                USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city resource operations are immutable facts'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.posted_at IS NULL AND NEW.posted_at IS NOT NULL
       AND NEW.id IS NOT DISTINCT FROM OLD.id
       AND NEW.world_id IS NOT DISTINCT FROM OLD.world_id
       AND NEW.tick IS NOT DISTINCT FROM OLD.tick
       AND NEW.sequence IS NOT DISTINCT FROM OLD.sequence
       AND NEW.operation_key IS NOT DISTINCT FROM OLD.operation_key
       AND NEW.operation_type IS NOT DISTINCT FROM OLD.operation_type
       AND NEW.source_command_id IS NOT DISTINCT FROM OLD.source_command_id
       AND NEW.actor_entity_id IS NOT DISTINCT FROM OLD.actor_entity_id
       AND NEW.district_id IS NOT DISTINCT FROM OLD.district_id
       AND NEW.recipe_id IS NOT DISTINCT FROM OLD.recipe_id
       AND NEW.batch_count IS NOT DISTINCT FROM OLD.batch_count
       AND NEW.description IS NOT DISTINCT FROM OLD.description
       AND NEW.metadata IS NOT DISTINCT FROM OLD.metadata
       AND NEW.created_at IS NOT DISTINCT FROM OLD.created_at THEN
        PERFORM assert_city_resource_operation_ready(OLD.id);
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city resource operations permit only one draft-to-posted transition'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_resource_operation_write_guard ON city_resource_operations;
CREATE TRIGGER city_resource_operation_write_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_resource_operations
FOR EACH ROW EXECUTE FUNCTION guard_city_resource_operation_write();

CREATE OR REPLACE FUNCTION guard_city_resource_entry_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'city resource entries are immutable facts' USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_resource_entry_immutable_guard ON city_resource_entries;
CREATE TRIGGER city_resource_entry_immutable_guard
BEFORE UPDATE OR DELETE ON city_resource_entries
FOR EACH ROW EXECUTE FUNCTION guard_city_resource_entry_immutable();

CREATE OR REPLACE FUNCTION assert_city_resource_operation_committed()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM city_resource_operations
        WHERE id = NEW.id AND posted_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'city resource operation must be posted before commit'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_resource_operation_commit_check ON city_resource_operations;
CREATE CONSTRAINT TRIGGER city_resource_operation_commit_check
AFTER INSERT OR UPDATE ON city_resource_operations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_resource_operation_committed();

CREATE OR REPLACE FUNCTION initialize_city_f3_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    household_id BIGINT;
    firm_id BIGINT;
    government_id BIGINT;
    central_id BIGINT;
    base_unit_id BIGINT;
    base_unit_scale INTEGER;
BEGIN
    SELECT id INTO household_id FROM city_economic_entities
    WHERE world_id = target_world_id AND code = 'founding_household' AND entity_type = 'household';
    SELECT id INTO firm_id FROM city_economic_entities
    WHERE world_id = target_world_id AND code = 'municipal_services' AND entity_type = 'firm';
    SELECT id INTO government_id FROM city_economic_entities
    WHERE world_id = target_world_id AND code = 'city_government' AND entity_type = 'government';
    SELECT id, scale INTO base_unit_id, base_unit_scale FROM city_monetary_units
    WHERE world_id = target_world_id AND is_base;
    IF household_id IS NULL OR firm_id IS NULL OR government_id IS NULL OR base_unit_id IS NULL THEN
        RAISE EXCEPTION 'city F3 foundation requires household, firm, government and base monetary unit'
            USING ERRCODE = '23514';
    END IF;

    INSERT INTO city_districts
        (world_id, code, name, sort_order, area_units, developable_area_units,
         residential_capacity_units, commercial_capacity_units, industrial_capacity_units)
    VALUES
        (target_world_id, 'central', 'Central District', 10, 12000000, 5400000, 18000, 9000, 2500),
        (target_world_id, 'north', 'North District', 20, 18000000, 10800000, 14000, 3500, 7000),
        (target_world_id, 'south', 'South District', 30, 16000000, 9600000, 16000, 4500, 5000),
        (target_world_id, 'east', 'East District', 40, 20000000, 13000000, 12000, 5000, 9000),
        (target_world_id, 'west', 'West District', 50, 22000000, 14300000, 11000, 3000, 10500),
        (target_world_id, 'harbor', 'Harbor District', 60, 14000000, 7000000, 8000, 6000, 8000)
    ON CONFLICT (world_id, code) DO NOTHING;

    SELECT id INTO central_id FROM city_districts
    WHERE world_id = target_world_id AND code = 'central';

    INSERT INTO city_household_cohorts
        (world_id, district_id, entity_id, income_band, population_units,
         working_age_units, employed_units, housing_demand_units)
    SELECT target_world_id, district.id, household_id, band.code,
           district_weight.base_population * band.weight,
           (district_weight.base_population * band.weight * 65) / 100,
           (district_weight.base_population * band.weight * band.employment_percent) / 100,
           (district_weight.base_population * band.weight * 95) / 100
    FROM city_districts district
    JOIN (VALUES
        ('central', 420), ('north', 300), ('south', 340),
        ('east', 320), ('west', 280), ('harbor', 240)
    ) AS district_weight(code, base_population) ON district_weight.code = district.code
    CROSS JOIN (VALUES
        ('low', 5, 48), ('middle', 3, 58), ('high', 2, 61)
    ) AS band(code, weight, employment_percent)
    WHERE district.world_id = target_world_id
    ON CONFLICT (world_id, district_id, income_band) DO NOTHING;

    INSERT INTO city_firm_states
        (world_id, entity_id, district_id, industry_code, employee_units,
         capital_stock_units, production_capacity_units, productivity_milli)
    VALUES (target_world_id, firm_id, central_id, 'basic_services', 320, 1500, 400, 1000)
    ON CONFLICT (world_id, entity_id) DO NOTHING;

    INSERT INTO city_government_states
        (world_id, entity_id, administrative_capacity_units, public_service_capacity_units)
    VALUES (target_world_id, government_id, 1000, 5000)
    ON CONFLICT (world_id) DO NOTHING;

    INSERT INTO city_government_budget_lines
        (world_id, government_entity_id, monetary_unit_id, code, name, appropriated_units)
    SELECT target_world_id, government_id, base_unit_id, budget.code, budget.name,
           (budget.major_units::numeric * power(10::numeric, base_unit_scale))::BIGINT
    FROM (VALUES
        ('education', 'Education', 2000000::BIGINT),
        ('healthcare', 'Healthcare', 1500000::BIGINT),
        ('public_safety', 'Public Safety', 1000000::BIGINT),
        ('transport', 'Transport', 1500000::BIGINT),
        ('environment', 'Environment', 500000::BIGINT),
        ('social_protection', 'Social Protection', 1500000::BIGINT),
        ('capital_projects', 'Capital Projects', 1000000::BIGINT)
    ) AS budget(code, name, major_units)
    ON CONFLICT (world_id, code) DO NOTHING;

    INSERT INTO city_resources
        (world_id, code, name, resource_kind, unit_code, unit_scale, storable)
    VALUES
        (target_world_id, 'basic_material', 'Basic Material', 'raw_material', 'unit', 0, TRUE),
        (target_world_id, 'food', 'Food', 'consumer_good', 'unit', 0, TRUE),
        (target_world_id, 'consumer_goods', 'Consumer Goods', 'consumer_good', 'unit', 0, TRUE),
        (target_world_id, 'capital_goods', 'Capital Goods', 'capital_good', 'unit', 0, TRUE),
        (target_world_id, 'housing_units', 'Housing Units', 'housing', 'dwelling', 0, TRUE),
        (target_world_id, 'developable_land', 'Developable Land', 'land', 'square_meter', 0, TRUE)
    ON CONFLICT (world_id, code) DO NOTHING;

    INSERT INTO city_production_recipes
        (world_id, code, name, industry_code, capacity_units_per_batch)
    VALUES
        (target_world_id, 'basic_goods', 'Basic Goods Production', 'basic_services', 1),
        (target_world_id, 'housing_construction', 'Housing Construction', 'basic_services', 20)
    ON CONFLICT (world_id, code) DO NOTHING;

    INSERT INTO city_production_recipe_lines
        (world_id, recipe_id, resource_id, direction, quantity_units)
    SELECT target_world_id, recipe.id, resource.id, line.direction, line.quantity_units
    FROM (VALUES
        ('basic_goods', 'basic_material', 'input', 2::BIGINT),
        ('basic_goods', 'consumer_goods', 'output', 1::BIGINT),
        ('housing_construction', 'developable_land', 'input', 100::BIGINT),
        ('housing_construction', 'capital_goods', 'input', 5::BIGINT),
        ('housing_construction', 'housing_units', 'output', 1::BIGINT)
    ) AS line(recipe_code, resource_code, direction, quantity_units)
    JOIN city_production_recipes recipe
      ON recipe.world_id = target_world_id AND recipe.code = line.recipe_code
    JOIN city_resources resource
      ON resource.world_id = target_world_id AND resource.code = line.resource_code
    ON CONFLICT (recipe_id, resource_id) DO NOTHING;

    INSERT INTO city_firm_recipes (world_id, firm_entity_id, recipe_id)
    SELECT target_world_id, firm_id, recipe.id
    FROM city_production_recipes recipe
    WHERE recipe.world_id = target_world_id AND recipe.status = 'active'
    ON CONFLICT (world_id, firm_entity_id, recipe_id) DO NOTHING;

    INSERT INTO city_inventory_balances
        (world_id, entity_id, entity_type, district_id, resource_id, opening_quantity_units)
    SELECT target_world_id, entity.id, entity.entity_type, central_id,
           resource.id, seed.opening_quantity_units
    FROM (VALUES
        ('founding_household', 'food', 300::BIGINT),
        ('founding_household', 'consumer_goods', 30::BIGINT),
        ('municipal_services', 'basic_material', 1000::BIGINT),
        ('municipal_services', 'consumer_goods', 100::BIGINT),
        ('municipal_services', 'capital_goods', 100::BIGINT),
        ('city_government', 'capital_goods', 1000::BIGINT),
        ('city_government', 'housing_units', 5000::BIGINT),
        ('city_government', 'developable_land', 1000000::BIGINT)
    ) AS seed(entity_code, resource_code, opening_quantity_units)
    JOIN city_economic_entities entity
      ON entity.world_id = target_world_id AND entity.code = seed.entity_code
    JOIN city_resources resource
      ON resource.world_id = target_world_id AND resource.code = seed.resource_code
    ON CONFLICT (world_id, entity_id, district_id, resource_id) DO NOTHING;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_f3_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    district_count BIGINT;
    cohort_count BIGINT;
    firm_count BIGINT;
    government_count BIGINT;
    budget_count BIGINT;
    resource_count BIGINT;
    recipe_count BIGINT;
    inventory_count BIGINT;
    labor_supply BIGINT;
    firm_employment BIGINT;
BEGIN
    SELECT COUNT(*) INTO district_count FROM city_districts WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO cohort_count FROM city_household_cohorts WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO firm_count FROM city_firm_states WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO government_count FROM city_government_states WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO budget_count FROM city_government_budget_lines WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO resource_count FROM city_resources WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO recipe_count FROM city_production_recipes WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO inventory_count FROM city_inventory_balances WHERE world_id = target_world_id;
    SELECT COALESCE(SUM(employed_units), 0) INTO labor_supply
    FROM city_household_cohorts WHERE world_id = target_world_id;
    SELECT COALESCE(SUM(employee_units), 0) INTO firm_employment
    FROM city_firm_states WHERE world_id = target_world_id;
    IF district_count <> 6 OR cohort_count <> 18 OR firm_count < 1 OR government_count <> 1
       OR budget_count <> 7 OR resource_count < 6 OR recipe_count < 2 OR inventory_count < 8
       OR firm_employment > labor_supply THEN
        RAISE EXCEPTION 'city world % has an incomplete F3 foundation', target_world_id
            USING ERRCODE = '23514';
    END IF;
END;
$$;

SELECT initialize_city_f3_foundation(id) FROM city_worlds;
SELECT assert_city_f3_foundation(id) FROM city_worlds;

UPDATE city_worlds
SET simulation_version = 'city-f3-v1', state_hash = NULL
WHERE simulation_version = 'city-f2-v1';
