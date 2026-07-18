-- 城市模拟 F7.5：企业经营场所、商业/工业空间占用和原子迁址事实链。

CREATE TABLE IF NOT EXISTS city_enterprise_location_profiles (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    policy_id VARCHAR(64) NOT NULL,
    policy_version VARCHAR(24) NOT NULL,
    policy_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    baseline_hash VARCHAR(64) NOT NULL,
    site_count BIGINT NOT NULL DEFAULT 0 CHECK (site_count >= 0),
    fact_count BIGINT NOT NULL DEFAULT 0 CHECK (fact_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_enterprise_location_profile_policy_check CHECK (
        policy_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND policy_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND policy_hash ~ '^[0-9a-f]{64}$'
        AND baseline_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_enterprise_location_profile_metadata_check
        CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_enterprise_sites (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    firm_entity_id BIGINT NOT NULL,
    entity_type VARCHAR(16) NOT NULL DEFAULT 'firm',
    district_id BIGINT NOT NULL,
    building_id BIGINT NOT NULL,
    pool_id BIGINT NOT NULL,
    site_type VARCHAR(24) NOT NULL CHECK (
        site_type IN ('headquarters', 'office', 'production', 'warehouse', 'retail')
    ),
    name VARCHAR(128) NOT NULL,
    occupied_units BIGINT NOT NULL CHECK (occupied_units > 0),
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'closed')),
    opened_tick BIGINT NOT NULL CHECK (opened_tick >= 0),
    last_changed_tick BIGINT NOT NULL CHECK (last_changed_tick >= opened_tick),
    closed_tick BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_enterprise_site_firm_fk
        FOREIGN KEY (firm_entity_id, world_id, entity_type)
        REFERENCES city_economic_entities(id, world_id, entity_type) ON DELETE RESTRICT,
    CONSTRAINT city_enterprise_site_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_enterprise_site_building_fk
        FOREIGN KEY (building_id, world_id)
        REFERENCES city_buildings(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_enterprise_site_pool_fk
        FOREIGN KEY (pool_id, world_id)
        REFERENCES city_building_unit_pools(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_enterprise_site_entity_type_check CHECK (entity_type = 'firm'),
    CONSTRAINT city_enterprise_site_code_check CHECK (code ~ '^[a-z][a-z0-9_]{1,159}$'),
    CONSTRAINT city_enterprise_site_name_check CHECK (
        char_length(name) BETWEEN 1 AND 128 AND name = btrim(name)
    ),
    CONSTRAINT city_enterprise_site_lifecycle_check CHECK (
        (status = 'active' AND closed_tick IS NULL)
        OR (status = 'closed' AND closed_tick IS NOT NULL
            AND closed_tick = last_changed_tick AND closed_tick >= opened_tick)
    ),
    CONSTRAINT city_enterprise_site_primary_check CHECK (
        NOT is_primary OR site_type IN ('headquarters', 'production')
    ),
    CONSTRAINT city_enterprise_site_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_enterprise_sites_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_enterprise_sites_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_enterprise_sites_world_firm
    ON city_enterprise_sites (world_id, firm_entity_id, status, site_type, code);
CREATE INDEX IF NOT EXISTS idx_city_enterprise_sites_world_pool
    ON city_enterprise_sites (world_id, pool_id, status, code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_city_enterprise_sites_one_active_headquarters
    ON city_enterprise_sites (world_id, firm_entity_id)
    WHERE status = 'active' AND site_type = 'headquarters';
CREATE UNIQUE INDEX IF NOT EXISTS idx_city_enterprise_sites_one_primary_per_type
    ON city_enterprise_sites (world_id, firm_entity_id, site_type)
    WHERE status = 'active' AND is_primary;

CREATE TABLE IF NOT EXISTS city_enterprise_location_facts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    source_command_id BIGINT NOT NULL,
    firm_entity_id BIGINT NOT NULL,
    entity_type VARCHAR(16) NOT NULL DEFAULT 'firm',
    site_code VARCHAR(160),
    fact_type VARCHAR(16) NOT NULL CHECK (fact_type IN ('opened', 'resized', 'closed', 'relocated')),
    from_status VARCHAR(16),
    to_status VARCHAR(16),
    occupied_before_units BIGINT NOT NULL DEFAULT 0 CHECK (occupied_before_units >= 0),
    occupied_after_units BIGINT NOT NULL DEFAULT 0 CHECK (occupied_after_units >= 0),
    site_version_before BIGINT NOT NULL DEFAULT 0 CHECK (site_version_before >= 0),
    site_version_after BIGINT NOT NULL DEFAULT 0 CHECK (site_version_after >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted_at TIMESTAMPTZ,
    CONSTRAINT city_enterprise_location_fact_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_enterprise_location_fact_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_enterprise_location_fact_firm_fk
        FOREIGN KEY (firm_entity_id, world_id, entity_type)
        REFERENCES city_economic_entities(id, world_id, entity_type) ON DELETE RESTRICT,
    CONSTRAINT city_enterprise_location_fact_site_fk
        FOREIGN KEY (world_id, site_code)
        REFERENCES city_enterprise_sites(world_id, code) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_enterprise_location_fact_entity_type_check CHECK (entity_type = 'firm'),
    CONSTRAINT city_enterprise_location_fact_transition_check CHECK (
        (fact_type = 'opened' AND site_code IS NOT NULL
         AND from_status IS NULL AND to_status = 'active'
         AND occupied_before_units = 0 AND occupied_after_units > 0
         AND site_version_before = 0 AND site_version_after = 1)
        OR (fact_type = 'resized' AND site_code IS NOT NULL
            AND from_status = 'active' AND to_status = 'active'
            AND occupied_before_units > 0 AND occupied_after_units > 0
            AND occupied_before_units <> occupied_after_units
            AND site_version_after = site_version_before + 1)
        OR (fact_type = 'closed' AND site_code IS NOT NULL
            AND from_status = 'active' AND to_status = 'closed'
            AND occupied_before_units > 0 AND occupied_after_units = 0
            AND site_version_after = site_version_before + 1)
        OR (fact_type = 'relocated' AND site_code IS NULL
            AND from_status IS NULL AND to_status IS NULL
            AND occupied_before_units = 0 AND occupied_after_units = 0
            AND site_version_before = 0 AND site_version_after = 0)
    ),
    CONSTRAINT city_enterprise_location_fact_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_enterprise_location_fact_posted_check CHECK (posted_at IS NULL OR posted_at >= created_at),
    CONSTRAINT city_enterprise_location_facts_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_enterprise_location_facts_command_unique UNIQUE (source_command_id),
    CONSTRAINT city_enterprise_location_facts_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_enterprise_location_facts_firm_history
    ON city_enterprise_location_facts (world_id, firm_entity_id, tick, sequence);
CREATE INDEX IF NOT EXISTS idx_city_enterprise_location_facts_site_history
    ON city_enterprise_location_facts (world_id, site_code, site_version_after)
    WHERE site_code IS NOT NULL;

CREATE TABLE IF NOT EXISTS city_enterprise_location_baselines (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick >= 0),
    policy_hash VARCHAR(64) NOT NULL CHECK (policy_hash ~ '^[0-9a-f]{64}$'),
    baseline_hash VARCHAR(64) NOT NULL CHECK (baseline_hash ~ '^[0-9a-f]{64}$'),
    site_count BIGINT NOT NULL CHECK (site_count >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    posted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_enterprise_location_baseline_metadata_check
        CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE OR REPLACE FUNCTION city_enterprise_location_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_enterprise_location_facts fact
        WHERE fact.id = CASE
            WHEN COALESCE(current_setting('sub2api.city_enterprise_location_fact_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.city_enterprise_location_fact_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND fact.world_id = target_world_id
          AND fact.posted_at IS NULL
    )
$$;

CREATE OR REPLACE FUNCTION guard_city_enterprise_location_fact_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    world_tick BIGINT;
    world_version VARCHAR(32);
    command_type_value VARCHAR(64);
    command_status_value VARCHAR(16);
    expected_fact_type VARCHAR(16);
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version
    FROM city_worlds WHERE id = NEW.world_id;
    IF world_version IS DISTINCT FROM 'city-f7-v4'
       OR NEW.tick IS DISTINCT FROM world_tick + 1
       OR NEW.posted_at IS NOT NULL THEN
        RAISE EXCEPTION 'city enterprise location fact must be a draft for the next F7.5 tick'
            USING ERRCODE = '23514';
    END IF;
    SELECT command_type, status INTO command_type_value, command_status_value
    FROM city_commands
    WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
    expected_fact_type := CASE command_type_value
        WHEN 'enterprise.site.open' THEN 'opened'
        WHEN 'enterprise.site.resize' THEN 'resized'
        WHEN 'enterprise.site.close' THEN 'closed'
        WHEN 'enterprise.relocate' THEN 'relocated'
        ELSE NULL
    END;
    IF command_status_value IS DISTINCT FROM 'pending'
       OR expected_fact_type IS DISTINCT FROM NEW.fact_type THEN
        RAISE EXCEPTION 'city enterprise location fact does not match its pending source command'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_enterprise_location_fact_insert_guard ON city_enterprise_location_facts;
CREATE TRIGGER city_enterprise_location_fact_insert_guard
BEFORE INSERT ON city_enterprise_location_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_enterprise_location_fact_insert();

CREATE OR REPLACE FUNCTION guard_city_enterprise_location_fact_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(OLD.world_id) THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'city enterprise location facts are immutable' USING ERRCODE = '55000';
    END IF;
    IF city_recovery_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    IF OLD.posted_at IS NOT NULL OR NEW.posted_at IS NULL
       OR (OLD.id, OLD.world_id, OLD.tick, OLD.sequence, OLD.source_command_id,
           OLD.firm_entity_id, OLD.entity_type, OLD.site_code, OLD.fact_type,
           OLD.from_status, OLD.to_status, OLD.occupied_before_units,
           OLD.occupied_after_units, OLD.site_version_before, OLD.site_version_after,
           OLD.metadata, OLD.created_at)
          IS DISTINCT FROM
          (NEW.id, NEW.world_id, NEW.tick, NEW.sequence, NEW.source_command_id,
           NEW.firm_entity_id, NEW.entity_type, NEW.site_code, NEW.fact_type,
           NEW.from_status, NEW.to_status, NEW.occupied_before_units,
           NEW.occupied_after_units, NEW.site_version_before, NEW.site_version_after,
           NEW.metadata, NEW.created_at) THEN
        RAISE EXCEPTION 'city enterprise location facts permit only one draft-to-posted transition'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_enterprise_location_fact_immutable_guard ON city_enterprise_location_facts;
CREATE TRIGGER city_enterprise_location_fact_immutable_guard
BEFORE UPDATE OR DELETE ON city_enterprise_location_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_enterprise_location_fact_immutable();

CREATE OR REPLACE FUNCTION guard_city_enterprise_location_profile()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := CASE WHEN TG_OP = 'DELETE' THEN OLD.world_id ELSE NEW.world_id END;
    IF city_recovery_write_enabled(target_world_id) THEN
        RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF city_f7_initialization_write_enabled(NEW.world_id)
           OR city_engine_upgrade_write_enabled(NEW.world_id) THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'city enterprise location profile requires genesis or audited upgrade'
            USING ERRCODE = '55000';
    ELSIF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city enterprise location profile cannot be deleted' USING ERRCODE = '55000';
    END IF;
    IF NOT city_enterprise_location_fact_write_enabled(NEW.world_id)
       OR (NEW.world_id, NEW.policy_id, NEW.policy_version, NEW.policy_hash,
           NEW.baseline_tick, NEW.baseline_hash, NEW.created_at)
          IS DISTINCT FROM
          (OLD.world_id, OLD.policy_id, OLD.policy_version, OLD.policy_hash,
           OLD.baseline_tick, OLD.baseline_hash, OLD.created_at) THEN
        RAISE EXCEPTION 'city enterprise location profile can only advance through a draft fact'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_enterprise_location_profile_guard ON city_enterprise_location_profiles;
CREATE TRIGGER city_enterprise_location_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_enterprise_location_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_enterprise_location_profile();

CREATE OR REPLACE FUNCTION guard_city_enterprise_location_baseline()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF city_f7_initialization_write_enabled(NEW.world_id)
           OR city_engine_upgrade_write_enabled(NEW.world_id)
           OR city_recovery_write_enabled(NEW.world_id) THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'city enterprise location baseline requires genesis or audited upgrade'
            USING ERRCODE = '55000';
    END IF;
    IF city_recovery_write_enabled(OLD.world_id) THEN
        RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
    END IF;
    RAISE EXCEPTION 'city enterprise location baseline is immutable' USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_enterprise_location_baseline_guard ON city_enterprise_location_baselines;
CREATE TRIGGER city_enterprise_location_baseline_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_enterprise_location_baselines
FOR EACH ROW EXECUTE FUNCTION guard_city_enterprise_location_baseline();

CREATE OR REPLACE FUNCTION guard_city_enterprise_site_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := CASE WHEN TG_OP = 'DELETE' THEN OLD.world_id ELSE NEW.world_id END;
    IF city_recovery_write_enabled(target_world_id) THEN
        RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF city_f7_initialization_write_enabled(NEW.world_id)
           OR city_engine_upgrade_write_enabled(NEW.world_id)
           OR city_enterprise_location_fact_write_enabled(NEW.world_id) THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'city enterprise site requires genesis, upgrade, or a draft location fact'
            USING ERRCODE = '55000';
    ELSIF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city enterprise sites cannot be deleted' USING ERRCODE = '55000';
    END IF;
    IF NOT city_enterprise_location_fact_write_enabled(NEW.world_id)
       OR (NEW.id, NEW.world_id, NEW.code, NEW.firm_entity_id, NEW.entity_type,
           NEW.opened_tick, NEW.created_at)
          IS DISTINCT FROM
          (OLD.id, OLD.world_id, OLD.code, OLD.firm_entity_id, OLD.entity_type,
           OLD.opened_tick, OLD.created_at)
       OR NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'city enterprise site projection can only advance through its draft fact'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_enterprise_site_projection_guard ON city_enterprise_sites;
CREATE TRIGGER city_enterprise_site_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_enterprise_sites
FOR EACH ROW EXECUTE FUNCTION guard_city_enterprise_site_projection();

CREATE OR REPLACE FUNCTION guard_city_enterprise_firm_location_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    fact_row city_enterprise_location_facts%ROWTYPE;
BEGIN
    IF NEW.district_id IS NOT DISTINCT FROM OLD.district_id THEN
        RETURN NEW;
    END IF;
    IF city_recovery_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    IF NOT city_enterprise_location_fact_write_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city firm district can only change through an enterprise relocation fact'
            USING ERRCODE = '55000';
    END IF;
    SELECT * INTO fact_row
    FROM city_enterprise_location_facts
    WHERE id = current_setting('sub2api.city_enterprise_location_fact_id', TRUE)::BIGINT;
    IF fact_row.fact_type IS DISTINCT FROM 'relocated'
       OR fact_row.firm_entity_id IS DISTINCT FROM NEW.entity_id
       OR NEW.version <> OLD.version + 1
       OR (NEW.world_id, NEW.entity_id, NEW.entity_type, NEW.industry_code,
           NEW.employee_units, NEW.capital_stock_units, NEW.production_capacity_units,
           NEW.productivity_milli, NEW.created_at)
          IS DISTINCT FROM
          (OLD.world_id, OLD.entity_id, OLD.entity_type, OLD.industry_code,
           OLD.employee_units, OLD.capital_stock_units, OLD.production_capacity_units,
           OLD.productivity_milli, OLD.created_at) THEN
        RAISE EXCEPTION 'city firm relocation may only change district and version'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_enterprise_firm_location_projection_guard ON city_firm_states;
CREATE TRIGGER city_enterprise_firm_location_projection_guard
BEFORE UPDATE OF district_id ON city_firm_states
FOR EACH ROW EXECUTE FUNCTION guard_city_enterprise_firm_location_projection();

-- F4 protects employment/version as one projection. A relocation also advances
-- firm version, but may not alter employment or any production quantity.
CREATE OR REPLACE FUNCTION guard_city_labor_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_TABLE_NAME = 'city_household_cohorts' THEN
        IF NEW.id IS DISTINCT FROM OLD.id OR NEW.world_id IS DISTINCT FROM OLD.world_id
           OR NEW.district_id IS DISTINCT FROM OLD.district_id
           OR NEW.entity_id IS DISTINCT FROM OLD.entity_id
           OR NEW.entity_type IS DISTINCT FROM OLD.entity_type
           OR NEW.income_band IS DISTINCT FROM OLD.income_band
           OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
            RAISE EXCEPTION 'city household cohort identity is immutable' USING ERRCODE = '55000';
        END IF;
        IF city_recovery_write_enabled(NEW.world_id) THEN
            RETURN NEW;
        ELSIF city_engine_upgrade_write_enabled(NEW.world_id)
              OR city_f63_initialization_write_enabled(NEW.world_id) THEN
            IF (NEW.population_units, NEW.working_age_units, NEW.employed_units, NEW.metadata)
               IS DISTINCT FROM
               (OLD.population_units, OLD.working_age_units, OLD.employed_units, OLD.metadata)
               OR NEW.household_units <> NEW.housing_demand_units
               OR NEW.version <> OLD.version + 1 THEN
                RAISE EXCEPTION 'invalid city household initialization projection' USING ERRCODE = '55000';
            END IF;
        ELSIF city_f4_write_enabled() THEN
            IF (NEW.population_units, NEW.working_age_units, NEW.household_units,
                NEW.housing_demand_units, NEW.metadata)
               IS DISTINCT FROM
               (OLD.population_units, OLD.working_age_units, OLD.household_units,
                OLD.housing_demand_units, OLD.metadata)
               OR NEW.version <> OLD.version + 1 THEN
                RAISE EXCEPTION 'invalid city household labor settlement projection'
                    USING ERRCODE = '55000';
            END IF;
        ELSIF city_f6_movement_write_enabled(NEW.world_id)
              OR city_f62_migration_write_enabled(NEW.world_id) THEN
            IF (NEW.employed_units, NEW.household_units, NEW.housing_demand_units, NEW.metadata)
               IS DISTINCT FROM
               (OLD.employed_units, OLD.household_units, OLD.housing_demand_units, OLD.metadata)
               OR NEW.version <> OLD.version + 1 THEN
                RAISE EXCEPTION 'invalid city household demographic projection'
                    USING ERRCODE = '55000';
            END IF;
        ELSIF city_f63_household_write_enabled(NEW.world_id) THEN
            IF NEW.metadata IS DISTINCT FROM OLD.metadata
               OR NEW.household_units <> NEW.housing_demand_units
               OR NEW.version <> OLD.version + 1 THEN
                RAISE EXCEPTION 'invalid city household lifecycle projection'
                    USING ERRCODE = '55000';
            END IF;
        ELSIF (NEW.population_units, NEW.working_age_units, NEW.employed_units,
               NEW.household_units, NEW.housing_demand_units, NEW.version, NEW.metadata)
              IS DISTINCT FROM
              (OLD.population_units, OLD.working_age_units, OLD.employed_units,
               OLD.household_units, OLD.housing_demand_units, OLD.version, OLD.metadata) THEN
            RAISE EXCEPTION 'city household cohort can only change through posted projections'
                USING ERRCODE = '55000';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_firm_states'
          AND NOT city_recovery_write_enabled(NEW.world_id)
          AND NOT city_f4_write_enabled()
          AND NOT (
              city_enterprise_location_fact_write_enabled(NEW.world_id)
              AND NEW.employee_units IS NOT DISTINCT FROM OLD.employee_units
              AND NEW.version = OLD.version + 1
              AND NEW.district_id IS DISTINCT FROM OLD.district_id
          )
          AND (NEW.employee_units, NEW.version) IS DISTINCT FROM (OLD.employee_units, OLD.version) THEN
        RAISE EXCEPTION 'city firm employment can only change through labor settlement'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_enterprise_location_fact_committed()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM city_enterprise_location_facts fact
        WHERE fact.id = NEW.id AND fact.posted_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'city enterprise location fact must be posted before commit'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_enterprise_location_fact_commit_check ON city_enterprise_location_facts;
CREATE CONSTRAINT TRIGGER city_enterprise_location_fact_commit_check
AFTER INSERT OR UPDATE ON city_enterprise_location_facts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_enterprise_location_fact_committed();

-- A relocation moves every non-zero resource in one conserved, multi-resource
-- transfer. It remains one operation per source command, but unlike an ordinary
-- resource.transfer it may contain one out/in pair for each resource code.
CREATE OR REPLACE FUNCTION assert_city_enterprise_relocation_resource_operation_ready(
    target_operation_id BIGINT
)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    operation_row city_resource_operations%ROWTYPE;
    fact_row city_enterprise_location_facts%ROWTYPE;
    command_type_value VARCHAR(64);
    command_status_value VARCHAR(16);
    from_district_id BIGINT;
    to_district_id BIGINT;
    from_district_code VARCHAR(48);
    to_district_code VARCHAR(48);
    entry_count BIGINT;
    outgoing_count BIGINT;
    expected_resource_count BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT * INTO operation_row
    FROM city_resource_operations
    WHERE id = target_operation_id;
    IF NOT FOUND OR operation_row.operation_type <> 'transfer'
       OR operation_row.source_command_id IS NULL
       OR COALESCE(operation_row.metadata ->> 'enterprise_location_fact_id', '') !~ '^[1-9][0-9]*$'
       OR COALESCE(operation_row.metadata ->> 'resource_count', '') !~ '^[1-9][0-9]*$' THEN
        RAISE EXCEPTION 'invalid enterprise relocation resource operation header'
            USING ERRCODE = '23514';
    END IF;

    SELECT * INTO fact_row
    FROM city_enterprise_location_facts
    WHERE id = (operation_row.metadata ->> 'enterprise_location_fact_id')::BIGINT;
    SELECT command_type, status INTO command_type_value, command_status_value
    FROM city_commands
    WHERE id = operation_row.source_command_id AND world_id = operation_row.world_id;
    from_district_code := fact_row.metadata -> 'firm_before' ->> 'district_code';
    to_district_code := fact_row.metadata -> 'firm_after' ->> 'district_code';
    SELECT id INTO from_district_id FROM city_districts
    WHERE world_id = operation_row.world_id AND code = from_district_code;
    SELECT id INTO to_district_id FROM city_districts
    WHERE world_id = operation_row.world_id AND code = to_district_code;
    IF fact_row.id IS NULL OR fact_row.fact_type <> 'relocated'
       OR fact_row.posted_at IS NOT NULL
       OR fact_row.id::TEXT IS DISTINCT FROM current_setting('sub2api.city_enterprise_location_fact_id', TRUE)
       OR fact_row.world_id <> operation_row.world_id
       OR fact_row.tick <> operation_row.tick
       OR fact_row.source_command_id <> operation_row.source_command_id
       OR fact_row.firm_entity_id <> operation_row.actor_entity_id
       OR command_type_value IS DISTINCT FROM 'enterprise.relocate'
       OR command_status_value IS DISTINCT FROM 'pending'
       OR from_district_id IS NULL OR to_district_id IS NULL
       OR from_district_id = to_district_id
       OR operation_row.district_id <> from_district_id
       OR operation_row.metadata ->> 'from_district_code' IS DISTINCT FROM from_district_code
       OR operation_row.metadata ->> 'to_district_code' IS DISTINCT FROM to_district_code
       OR jsonb_typeof(fact_row.metadata -> 'resource_operation_sequences') <> 'array'
       OR jsonb_array_length(fact_row.metadata -> 'resource_operation_sequences') <> 1
       OR NOT EXISTS (
           SELECT 1
           FROM jsonb_array_elements_text(fact_row.metadata -> 'resource_operation_sequences') value
           WHERE value ~ '^[1-9][0-9]*$' AND value::BIGINT = operation_row.sequence
       ) THEN
        RAISE EXCEPTION 'enterprise relocation resource operation is not bound to its draft fact'
            USING ERRCODE = '23514';
    END IF;

    expected_resource_count := (operation_row.metadata ->> 'resource_count')::BIGINT;
    SELECT COUNT(*), COUNT(*) FILTER (WHERE entry.direction = 'out')
    INTO entry_count, outgoing_count
    FROM city_resource_entries entry
    WHERE entry.operation_id = operation_row.id;
    IF entry_count <> expected_resource_count * 2
       OR outgoing_count <> expected_resource_count THEN
        RAISE EXCEPTION 'enterprise relocation resource entry count is incomplete'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM (
        SELECT entry.resource_id,
               COUNT(*) AS pair_count,
               COUNT(*) FILTER (WHERE entry.direction = 'out') AS out_count,
               COUNT(*) FILTER (WHERE entry.direction = 'in') AS in_count,
               SUM(entry.quantity_units) FILTER (WHERE entry.direction = 'out') AS out_units,
               SUM(entry.quantity_units) FILTER (WHERE entry.direction = 'in') AS in_units
        FROM city_resource_entries entry
        WHERE entry.operation_id = operation_row.id
        GROUP BY entry.resource_id
    ) pair
    WHERE pair.pair_count <> 2 OR pair.out_count <> 1 OR pair.in_count <> 1
       OR pair.out_units <> pair.in_units;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'enterprise relocation resource pairs are not conserved'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_resource_entries entry
    JOIN city_inventory_balances balance
      ON balance.id = entry.balance_id AND balance.world_id = entry.world_id
    WHERE entry.operation_id = operation_row.id
      AND (balance.entity_id <> operation_row.actor_entity_id
           OR (entry.direction = 'out' AND (
                  balance.district_id <> from_district_id
                  OR entry.quantity_before_units <> entry.quantity_units
                  OR entry.quantity_after_units <> 0
              ))
           OR (entry.direction = 'in' AND balance.district_id <> to_district_id));
    IF invalid_count <> 0 OR EXISTS (
        SELECT 1
        FROM city_inventory_balances balance
        WHERE balance.world_id = operation_row.world_id
          AND balance.entity_id = operation_row.actor_entity_id
          AND balance.district_id = from_district_id
          AND balance.status = 'active' AND balance.quantity_units <> 0
    ) THEN
        RAISE EXCEPTION 'enterprise relocation did not exhaust the source inventory'
            USING ERRCODE = '23514';
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
       AND NEW.market_settlement_id IS NOT DISTINCT FROM OLD.market_settlement_id
       AND NEW.actor_entity_id IS NOT DISTINCT FROM OLD.actor_entity_id
       AND NEW.district_id IS NOT DISTINCT FROM OLD.district_id
       AND NEW.recipe_id IS NOT DISTINCT FROM OLD.recipe_id
       AND NEW.batch_count IS NOT DISTINCT FROM OLD.batch_count
       AND NEW.description IS NOT DISTINCT FROM OLD.description
       AND NEW.metadata IS NOT DISTINCT FROM OLD.metadata
       AND NEW.created_at IS NOT DISTINCT FROM OLD.created_at THEN
        IF NEW.metadata ? 'enterprise_location_fact_id' THEN
            PERFORM assert_city_enterprise_relocation_resource_operation_ready(OLD.id);
        ELSE
            PERFORM assert_city_resource_operation_ready(OLD.id);
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city resource operations permit only one draft-to-posted transition'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_enterprise_location_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    profile_row city_enterprise_location_profiles%ROWTYPE;
    baseline_row city_enterprise_location_baselines%ROWTYPE;
    actual_sites BIGINT;
    actual_facts BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city enterprise location world does not exist' USING ERRCODE = '23514';
    END IF;
    IF world_version <> 'city-f7-v4' THEN
        IF EXISTS (SELECT 1 FROM city_enterprise_location_profiles WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_enterprise_sites WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_enterprise_location_facts WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_enterprise_location_baselines WHERE world_id = target_world_id) THEN
            RAISE EXCEPTION 'legacy city engine cannot contain F7.5 enterprise location state'
                USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;

    SELECT * INTO profile_row
    FROM city_enterprise_location_profiles WHERE world_id = target_world_id;
    SELECT * INTO baseline_row
    FROM city_enterprise_location_baselines WHERE world_id = target_world_id;
    IF profile_row.world_id IS NULL OR baseline_row.world_id IS NULL
       OR profile_row.policy_id <> 'sub2api-enterprise-location'
       OR profile_row.policy_version <> '1.0.0'
       OR profile_row.policy_hash <> 'b5ec620c0b3bbe81b564a59fe0c372bce97932b31d7d5af341fe62a2b362f39d'
       OR baseline_row.policy_hash <> profile_row.policy_hash
       OR baseline_row.baseline_hash <> profile_row.baseline_hash
       OR baseline_row.tick <> profile_row.baseline_tick
       OR baseline_row.tick > world_tick THEN
        RAISE EXCEPTION 'city F7.5 enterprise location profile or baseline is inconsistent'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO actual_sites FROM city_enterprise_sites WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_facts FROM city_enterprise_location_facts
    WHERE world_id = target_world_id AND posted_at IS NOT NULL;
    IF (profile_row.site_count, profile_row.fact_count, profile_row.revision)
       IS DISTINCT FROM (actual_sites, actual_facts, actual_facts + 1)
       OR baseline_row.site_count > actual_sites THEN
        RAISE EXCEPTION 'city F7.5 enterprise location counters are inconsistent'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_enterprise_sites site
    JOIN city_economic_entities entity
      ON entity.id = site.firm_entity_id AND entity.world_id = site.world_id
     AND entity.entity_type = site.entity_type
    JOIN city_firm_states firm
      ON firm.entity_id = site.firm_entity_id AND firm.world_id = site.world_id
    JOIN city_districts district
      ON district.id = site.district_id AND district.world_id = site.world_id
    JOIN city_buildings building
      ON building.id = site.building_id AND building.world_id = site.world_id
    JOIN city_parcels parcel
      ON parcel.id = building.parcel_id AND parcel.world_id = building.world_id
    JOIN city_building_unit_pools pool
      ON pool.id = site.pool_id AND pool.world_id = site.world_id
    WHERE site.world_id = target_world_id
      AND (entity.entity_type <> 'firm'
           OR building.district_id <> site.district_id
           OR parcel.district_id <> site.district_id
           OR pool.district_id <> site.district_id
           OR pool.building_id <> site.building_id
           OR pool.use_type <> building.primary_use
           OR (site.site_type IN ('headquarters', 'office', 'retail') AND pool.use_type <> 'commercial')
           OR (site.site_type IN ('production', 'warehouse') AND pool.use_type <> 'industrial')
           OR site.opened_tick > world_tick OR site.last_changed_tick > world_tick
           OR (site.status = 'closed' AND site.closed_tick > world_tick)
           OR (entity.status = 'closed' AND site.status = 'active'));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.5 enterprise site identity, use, or lifecycle is inconsistent'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_economic_entities entity
    JOIN city_firm_states firm
      ON firm.entity_id = entity.id AND firm.world_id = entity.world_id
    LEFT JOIN LATERAL (
        SELECT COUNT(*) FILTER (WHERE site.site_type = 'headquarters')::BIGINT AS headquarters,
               COUNT(*) FILTER (WHERE site.site_type = 'production')::BIGINT AS production,
               COUNT(*)::BIGINT AS active_sites,
               MIN(site.district_id) FILTER (WHERE site.site_type = 'headquarters') AS headquarters_district
        FROM city_enterprise_sites site
        WHERE site.world_id = entity.world_id AND site.firm_entity_id = entity.id
          AND site.status = 'active'
    ) sites ON TRUE
    WHERE entity.world_id = target_world_id AND entity.entity_type = 'firm'
      AND ((entity.status = 'active' AND (
                sites.headquarters <> 1
                OR (firm.production_capacity_units > 0 AND sites.production < 1)
                OR sites.headquarters_district <> firm.district_id
                OR sites.active_sites > 32
           ))
           OR (entity.status = 'closed' AND sites.active_sites <> 0));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.5 required enterprise sites or primary district are inconsistent'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_building_unit_pools pool
    LEFT JOIN LATERAL (
        SELECT COALESCE(SUM(site.occupied_units), 0)::BIGINT AS occupied
        FROM city_enterprise_sites site
        WHERE site.world_id = pool.world_id AND site.pool_id = pool.id
          AND site.status = 'active'
    ) enterprise ON TRUE
    LEFT JOIN LATERAL (
        SELECT COALESCE(SUM(adjustment.added_capacity_units), 0)::BIGINT AS added_capacity
        FROM city_building_adjustments adjustment
        WHERE adjustment.world_id = pool.world_id AND adjustment.building_id = pool.building_id
    ) development ON TRUE
    WHERE pool.world_id = target_world_id
      AND (enterprise.occupied > pool.unit_count + development.added_capacity / pool.capacity_units_per_unit
           OR (pool.use_type = 'residential' AND enterprise.occupied <> 0)
           OR (pool.use_type IN ('commercial', 'industrial') AND pool.occupied_unit_count <> 0));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.5 enterprise occupancy exceeds effective building pool supply'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_enterprise_sites site
    LEFT JOIN LATERAL (
        SELECT COUNT(*)::BIGINT AS fact_count,
               MAX(fact.site_version_after)::BIGINT AS last_version,
               (ARRAY_AGG(fact.to_status ORDER BY fact.site_version_after DESC))[1] AS last_status,
               (ARRAY_AGG(fact.occupied_after_units ORDER BY fact.site_version_after DESC))[1] AS last_occupied
        FROM city_enterprise_location_facts fact
        WHERE fact.world_id = site.world_id AND fact.site_code = site.code
          AND fact.posted_at IS NOT NULL
    ) history ON TRUE
    WHERE site.world_id = target_world_id
      AND history.fact_count > 0
      AND (history.last_version <> site.version
           OR history.last_status <> site.status
           OR history.last_occupied <> CASE WHEN site.status = 'active' THEN site.occupied_units ELSE 0 END);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.5 enterprise site fact head is inconsistent'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_enterprise_location_facts fact
    JOIN city_commands command
      ON command.id = fact.source_command_id AND command.world_id = fact.world_id
    WHERE fact.world_id = target_world_id AND fact.posted_at IS NOT NULL
      AND (fact.tick > world_tick OR command.status <> 'applied'
           OR (fact.fact_type = 'opened' AND command.command_type <> 'enterprise.site.open')
           OR (fact.fact_type = 'resized' AND command.command_type <> 'enterprise.site.resize')
           OR (fact.fact_type = 'closed' AND command.command_type <> 'enterprise.site.close')
           OR (fact.fact_type = 'relocated' AND command.command_type <> 'enterprise.relocate'));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.5 posted enterprise location fact origin is inconsistent'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_enterprise_location_facts fact
    LEFT JOIN LATERAL (
        SELECT COUNT(*)::BIGINT AS operation_count,
               COALESCE(ARRAY_AGG(operation.sequence ORDER BY operation.sequence), ARRAY[]::BIGINT[]) AS sequences
        FROM city_resource_operations operation
        WHERE operation.world_id = fact.world_id
          AND operation.source_command_id = fact.source_command_id
          AND operation.operation_type = 'transfer'
          AND operation.posted_at IS NOT NULL
          AND operation.metadata ->> 'enterprise_location_fact_id' = fact.id::TEXT
    ) resource ON TRUE
    WHERE fact.world_id = target_world_id AND fact.fact_type = 'relocated'
      AND fact.posted_at IS NOT NULL
      AND (jsonb_typeof(fact.metadata -> 'resource_operation_sequences') <> 'array'
           OR resource.operation_count <> jsonb_array_length(fact.metadata -> 'resource_operation_sequences')
           OR resource.sequences <> COALESCE(ARRAY(
               SELECT value::BIGINT
               FROM jsonb_array_elements_text(fact.metadata -> 'resource_operation_sequences') value
               ORDER BY value::BIGINT
           ), ARRAY[]::BIGINT[]));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.5 relocation resource operation linkage is inconsistent'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_enterprise_location_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := CASE
        WHEN TG_TABLE_NAME = 'city_worlds' THEN COALESCE(
            (to_jsonb(NEW) ->> 'id')::BIGINT, (to_jsonb(OLD) ->> 'id')::BIGINT
        )
        ELSE COALESCE(
            (to_jsonb(NEW) ->> 'world_id')::BIGINT, (to_jsonb(OLD) ->> 'world_id')::BIGINT
        )
    END;
    PERFORM assert_city_enterprise_location_foundation(target_world_id);
    RETURN NULL;
END;
$$;

DO $$
DECLARE
    table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_enterprise_location_profiles', 'city_enterprise_sites',
        'city_enterprise_location_facts', 'city_enterprise_location_baselines',
        'city_firm_states', 'city_economic_entities', 'city_building_adjustments'
    ] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', table_name || '_enterprise_location_commit_check', table_name);
        EXECUTE format(
            'CREATE CONSTRAINT TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I '
            || 'DEFERRABLE INITIALLY DEFERRED FOR EACH ROW '
            || 'EXECUTE FUNCTION check_city_enterprise_location_foundation()',
            table_name || '_enterprise_location_commit_check', table_name
        );
    END LOOP;
END;
$$;

DROP TRIGGER IF EXISTS city_enterprise_location_world_commit_check ON city_worlds;
CREATE CONSTRAINT TRIGGER city_enterprise_location_world_commit_check
AFTER INSERT OR UPDATE ON city_worlds
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_enterprise_location_foundation();

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f7-v4', 'supported', 'city-state-v1+gzip',
        '["control","ledger","resources","calendar_demography","population_migration","household_lifecycle","spatial","land","development","enterprise_location","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f7-v3', 'city-f7-v4', 'f7_v3_to_f7_v4')
ON CONFLICT (from_version, to_version) DO NOTHING;

-- F7.5 keeps all F7.4 development commands available. Rebind the insert guard
-- and assertion to both compatible engine versions without weakening F7.4.
CREATE OR REPLACE FUNCTION guard_city_development_fact_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    world_tick BIGINT;
    world_version VARCHAR(32);
    command_type_value VARCHAR(64);
    command_status_value VARCHAR(16);
    expected_fact_type VARCHAR(24);
    decision_value TEXT;
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version
    FROM city_worlds WHERE id = NEW.world_id;
    IF world_version NOT IN ('city-f7-v3', 'city-f7-v4')
       OR NEW.tick IS DISTINCT FROM world_tick + 1 THEN
        RAISE EXCEPTION 'city development fact must target the next F7.4-compatible tick'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.source_command_id IS NULL THEN
        IF NEW.fact_type NOT IN ('progressed', 'completed')
           OR COALESCE(current_setting('sub2api.city_development_auto_world_id', TRUE), '') <> NEW.world_id::TEXT THEN
            RAISE EXCEPTION 'automatic city development fact is not authorized'
                USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    SELECT command_type, status, payload ->> 'decision'
    INTO command_type_value, command_status_value, decision_value
    FROM city_commands
    WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
    expected_fact_type := CASE command_type_value
        WHEN 'development.submit' THEN 'submitted'
        WHEN 'development.start' THEN 'started'
        WHEN 'development.cancel' THEN 'cancelled'
        WHEN 'development.review' THEN CASE decision_value
            WHEN 'approve' THEN 'approved'
            WHEN 'reject' THEN 'rejected'
            ELSE NULL
        END
        ELSE NULL
    END;
    IF command_status_value IS DISTINCT FROM 'pending'
       OR expected_fact_type IS DISTINCT FROM NEW.fact_type THEN
        RAISE EXCEPTION 'city development fact does not match its pending source command'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_development_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    profile_row city_development_profiles%ROWTYPE;
    baseline_row city_development_baselines%ROWTYPE;
    actual_projects BIGINT;
    actual_facts BIGINT;
    actual_adjustments BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city development world does not exist' USING ERRCODE = '23514';
    END IF;
    IF world_version NOT IN ('city-f7-v3', 'city-f7-v4') THEN
        IF EXISTS (SELECT 1 FROM city_development_profiles WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_development_projects WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_development_facts WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_building_adjustments WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_development_baselines WHERE world_id = target_world_id) THEN
            RAISE EXCEPTION 'legacy city engine cannot contain F7.4 development state'
                USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;

    SELECT * INTO profile_row FROM city_development_profiles WHERE world_id = target_world_id;
    SELECT * INTO baseline_row FROM city_development_baselines WHERE world_id = target_world_id;
    IF profile_row.world_id IS NULL OR baseline_row.world_id IS NULL
       OR profile_row.policy_id <> 'sub2api-development'
       OR profile_row.policy_version <> '1.0.0'
       OR profile_row.policy_hash <> 'b1bbc919b39020a5bc4760fb0ee80468d286a4d74b97d4bbae8f8601c5bb9f3f'
       OR baseline_row.policy_hash <> profile_row.policy_hash
       OR baseline_row.baseline_hash <> 'fcb3ae78e18e4b3adb2db1cd9535403f61f28a04fee5eb13ac6ad284ca89459c'
       OR baseline_row.tick <> profile_row.baseline_tick
       OR baseline_row.tick > world_tick THEN
        RAISE EXCEPTION 'city F7.4 profile or baseline is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO actual_projects FROM city_development_projects WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_facts FROM city_development_facts
    WHERE world_id = target_world_id AND posted_at IS NOT NULL;
    SELECT COUNT(*) INTO actual_adjustments FROM city_building_adjustments WHERE world_id = target_world_id;
    IF (profile_row.project_count, profile_row.fact_count, profile_row.adjustment_count,
        profile_row.revision)
       IS DISTINCT FROM
       (actual_projects, actual_facts, actual_adjustments, actual_facts + 1) THEN
        RAISE EXCEPTION 'city F7.4 profile counters are inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_development_projects project
    JOIN city_buildings building
      ON building.id = project.building_id AND building.world_id = project.world_id
    JOIN city_parcels parcel
      ON parcel.id = project.parcel_id AND parcel.world_id = project.world_id
    JOIN city_economic_entities developer
      ON developer.id = project.developer_entity_id AND developer.world_id = project.world_id
    LEFT JOIN city_firm_states firm
      ON firm.entity_id = developer.id AND firm.world_id = developer.world_id
    LEFT JOIN LATERAL (
        SELECT COUNT(*)::BIGINT AS fact_count,
               MAX(fact.project_version_after)::BIGINT AS last_version,
               (ARRAY_AGG(fact.to_status ORDER BY fact.project_version_after DESC))[1] AS last_status,
               (ARRAY_AGG(fact.progress_after_milli ORDER BY fact.project_version_after DESC))[1] AS last_progress
        FROM city_development_facts fact
        WHERE fact.world_id = project.world_id AND fact.project_code = project.code
          AND fact.posted_at IS NOT NULL
    ) history ON TRUE
    WHERE project.world_id = target_world_id
      AND (project.district_id <> building.district_id OR project.parcel_id <> building.parcel_id
           OR parcel.district_id <> project.district_id OR developer.entity_type <> 'firm'
           OR developer.status <> 'active' OR firm.entity_id IS NULL
           OR firm.district_id <> project.district_id
           OR history.fact_count <> project.version OR history.last_version <> project.version
           OR history.last_status <> project.status OR history.last_progress <> project.progress_milli);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.4 project identity, developer, or fact head is inconsistent'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_development_facts fact
    LEFT JOIN city_commands command
      ON command.id = fact.source_command_id AND command.world_id = fact.world_id
    WHERE fact.world_id = target_world_id AND fact.posted_at IS NOT NULL
      AND (fact.tick > world_tick
           OR (fact.source_command_id IS NOT NULL AND command.status <> 'applied')
           OR (fact.fact_type = 'submitted' AND command.command_type <> 'development.submit')
           OR (fact.fact_type IN ('approved', 'rejected') AND command.command_type <> 'development.review')
           OR (fact.fact_type = 'started' AND command.command_type <> 'development.start')
           OR (fact.fact_type = 'cancelled' AND command.command_type <> 'development.cancel'));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.4 posted fact origin is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_development_projects project
    JOIN city_buildings building
      ON building.id = project.building_id AND building.world_id = project.world_id
    JOIN city_parcels parcel
      ON parcel.id = project.parcel_id AND parcel.world_id = project.world_id
    JOIN city_zoning_rules rule
      ON rule.world_id = project.world_id AND rule.code = parcel.zone_code
    LEFT JOIN LATERAL (
        SELECT COALESCE(SUM(adjustment.added_floor_count), 0)::BIGINT AS floors,
               COALESCE(SUM(adjustment.added_floor_area_sqm), 0)::BIGINT AS area,
               COALESCE(SUM(adjustment.quality_delta_milli), 0)::BIGINT AS quality
        FROM city_building_adjustments adjustment
        WHERE adjustment.world_id = building.world_id AND adjustment.building_id = building.id
          AND adjustment.project_code <> project.code
    ) prior ON TRUE
    WHERE project.world_id = target_world_id
      AND ((project.project_type = 'vertical_expansion'
            AND (project.target_floor_count <> building.floor_count + prior.floors + project.added_floor_count
                 OR building.floor_count + prior.floors + project.added_floor_count > rule.max_floors
                 OR building.floor_area_sqm + prior.area + project.added_floor_area_sqm
                    > (parcel.area_sqm::NUMERIC * rule.max_floor_area_ratio_milli / 1000)::BIGINT))
           OR (project.project_type = 'renovation'
               AND project.target_quality_milli <> building.quality_milli + prior.quality + project.quality_delta_milli));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.4 project plan violates its effective building envelope'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_development_projects project
    LEFT JOIN city_building_adjustments adjustment
      ON adjustment.world_id = project.world_id AND adjustment.project_code = project.code
    WHERE project.world_id = target_world_id
      AND ((project.status = 'completed' AND (
              adjustment.id IS NULL OR adjustment.building_id <> project.building_id
              OR adjustment.district_id <> project.district_id
              OR adjustment.added_floor_count <> project.added_floor_count
              OR adjustment.added_floor_area_sqm <> project.added_floor_area_sqm
              OR adjustment.added_capacity_units <> project.added_capacity_units
              OR adjustment.quality_delta_milli <> project.quality_delta_milli
              OR adjustment.completed_tick <> project.completed_tick
          )) OR (project.status <> 'completed' AND adjustment.id IS NOT NULL));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.4 completed project adjustment is inconsistent'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_development_projects project
    JOIN city_development_facts fact
      ON fact.world_id = project.world_id AND fact.project_code = project.code
     AND fact.fact_type = 'started' AND fact.posted_at IS NOT NULL
    LEFT JOIN LATERAL (
        SELECT COUNT(*)::BIGINT AS operation_count,
               COALESCE(SUM(entry.quantity_units) FILTER (WHERE resource.code = 'basic_material'), 0)::BIGINT AS material,
               COALESCE(SUM(entry.quantity_units) FILTER (WHERE resource.code = 'capital_goods'), 0)::BIGINT AS capital,
               COUNT(DISTINCT resource.code) AS resource_count
        FROM city_resource_operations operation
        JOIN city_resource_entries entry ON entry.operation_id = operation.id
        JOIN city_resources resource ON resource.id = entry.resource_id
        WHERE operation.world_id = project.world_id AND operation.tick = fact.tick
          AND operation.operation_type = 'consumption' AND operation.posted_at IS NOT NULL
          AND operation.metadata ->> 'development_project_code' = project.code
          AND operation.metadata ->> 'development_fact_id' = fact.id::TEXT
          AND operation.actor_entity_id = project.developer_entity_id
          AND operation.district_id = project.district_id
          AND entry.direction = 'out'
    ) consumed ON TRUE
    WHERE project.world_id = target_world_id
      AND (consumed.operation_count <> 2 OR consumed.resource_count <> 2
           OR consumed.material <> project.required_basic_material_units
           OR consumed.capital <> project.required_capital_goods_units);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.4 started project resource consumption is inconsistent'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_firm_states firm
    JOIN LATERAL (
        SELECT COALESCE(SUM(project.required_labor_units), 0)::BIGINT AS reserved
        FROM city_development_projects project
        WHERE project.world_id = firm.world_id AND project.developer_entity_id = firm.entity_id
          AND project.status = 'under_construction'
    ) labor ON TRUE
    WHERE firm.world_id = target_world_id AND labor.reserved > firm.employee_units;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.4 construction labor reservations exceed firm capacity'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

-- F7.5 consumes the immutable F7.3 land baseline and F7.4 adjustments.
CREATE OR REPLACE FUNCTION assert_city_land_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    profile_row city_land_profiles%ROWTYPE;
    baseline_row city_land_baselines%ROWTYPE;
    actual_zoning BIGINT;
    actual_parcels BIGINT;
    actual_buildings BIGINT;
    actual_pools BIGINT;
    actual_allocations BIGINT;
    actual_portals BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city land world does not exist' USING ERRCODE = '23514';
    END IF;

    IF world_version NOT IN ('city-f7-v2', 'city-f7-v3', 'city-f7-v4') THEN
        IF EXISTS (SELECT 1 FROM city_land_profiles WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_zoning_rules WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_parcels WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_buildings WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_building_unit_pools WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_housing_allocations WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_building_portals WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_land_baselines WHERE world_id = target_world_id) THEN
            RAISE EXCEPTION 'legacy city engine cannot contain F7.3 land state' USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;

    SELECT * INTO profile_row FROM city_land_profiles WHERE world_id = target_world_id;
    SELECT * INTO baseline_row FROM city_land_baselines WHERE world_id = target_world_id;
    IF profile_row.world_id IS NULL OR baseline_row.world_id IS NULL THEN
        RAISE EXCEPTION 'city F7.3 land profile or baseline is missing' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO actual_zoning FROM city_zoning_rules WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_parcels FROM city_parcels WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_buildings FROM city_buildings WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_pools FROM city_building_unit_pools WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_allocations FROM city_housing_allocations WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_portals FROM city_building_portals WHERE world_id = target_world_id;

    IF profile_row.rule_set_id <> 'sub2api-land'
       OR profile_row.rule_set_version <> '1.0.0'
       OR profile_row.rule_set_hash <> '4275912ce56d967b3596c5449ef28097623b0d1a9b80ea5f60e1a882f79e60c2'
       OR profile_row.nominal_cell_area_sqm <> 1500
       OR profile_row.spatial_overmap_root_hash IS DISTINCT FROM (
            SELECT overmap_root_hash FROM city_spatial_profiles WHERE world_id = target_world_id
       )
       OR profile_row.baseline_hash <> baseline_row.baseline_hash
       OR profile_row.rule_set_hash <> baseline_row.rule_set_hash
       OR baseline_row.tick > world_tick
       OR (baseline_row.tick > 0 AND NOT EXISTS (
            SELECT 1 FROM city_ticks WHERE world_id = target_world_id AND tick = baseline_row.tick
       ))
       OR (profile_row.zoning_rule_count, profile_row.parcel_count, profile_row.building_count,
           profile_row.unit_pool_count, profile_row.housing_allocation_count, profile_row.portal_count)
          IS DISTINCT FROM
          (actual_zoning, actual_parcels, actual_buildings,
           actual_pools, actual_allocations, actual_portals)
       OR (baseline_row.zoning_rule_count, baseline_row.parcel_count, baseline_row.building_count,
           baseline_row.unit_pool_count, baseline_row.housing_allocation_count, baseline_row.portal_count)
          IS DISTINCT FROM
          (actual_zoning, actual_parcels, actual_buildings,
           actual_pools, actual_allocations, actual_portals) THEN
        RAISE EXCEPTION 'city F7.3 land profile or baseline is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM (VALUES
        ('commercial'::VARCHAR, 4000::BIGINT, 600::BIGINT, 16::SMALLINT, 25::BIGINT),
        ('industrial'::VARCHAR, 1500::BIGINT, 700::BIGINT, 4::SMALLINT, 40::BIGINT),
        ('residential'::VARCHAR, 3000::BIGINT, 450::BIGINT, 12::SMALLINT, 90::BIGINT)
    ) expected(code, far_milli, coverage_milli, max_floors, sqm_per_capacity)
    FULL JOIN (
        SELECT * FROM city_zoning_rules scoped_rule
        WHERE scoped_rule.world_id = target_world_id
    ) rule ON rule.code = expected.code
    WHERE expected.code IS NULL OR rule.code IS NULL
       OR rule.primary_use <> expected.code OR rule.max_floor_area_ratio_milli <> expected.far_milli
       OR rule.max_coverage_milli <> expected.coverage_milli OR rule.max_floors <> expected.max_floors
       OR rule.sqm_per_capacity_unit <> expected.sqm_per_capacity OR rule.status <> 'active';
    IF invalid_count <> 0 OR actual_zoning <> 3 THEN
        RAISE EXCEPTION 'city F7.3 zoning rules do not match the bound rule set' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_districts district
    LEFT JOIN LATERAL (
        SELECT COALESCE(SUM(parcel.area_sqm), 0)::BIGINT AS area_sqm
        FROM city_parcels parcel
        WHERE parcel.world_id = district.world_id AND parcel.district_id = district.id
    ) parcel_sum ON TRUE
    WHERE district.world_id = target_world_id
      AND parcel_sum.area_sqm <> district.developable_area_units;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 parcel area does not conserve district developable area'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_parcels parcel
    JOIN city_overmap_tiles tile
      ON tile.world_id = parcel.world_id AND tile.chunk_x = parcel.chunk_x
     AND tile.chunk_y = parcel.chunk_y AND tile.z = parcel.z
    WHERE parcel.world_id = target_world_id
      AND (tile.district_id <> parcel.district_id OR parcel.developable_area_sqm <> parcel.area_sqm);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 parcel projection is inconsistent with immutable overmap'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_buildings building
    JOIN city_parcels parcel
      ON parcel.id = building.parcel_id AND parcel.world_id = building.world_id
    JOIN city_zoning_rules rule
      ON rule.world_id = building.world_id AND rule.code = parcel.zone_code
    WHERE building.world_id = target_world_id
      AND (building.district_id <> parcel.district_id OR building.primary_use <> parcel.zone_code
           OR building.chunk_x <> parcel.chunk_x OR building.chunk_y <> parcel.chunk_y
           OR building.footprint_z <> parcel.z
           OR building.local_min_x < parcel.local_min_x OR building.local_min_y < parcel.local_min_y
           OR building.local_max_x > parcel.local_max_x OR building.local_max_y > parcel.local_max_y
           OR building.floor_count > rule.max_floors
           OR building.footprint_area_sqm::NUMERIC
              > parcel.area_sqm::NUMERIC * rule.max_coverage_milli::NUMERIC / 1000
           OR building.floor_area_sqm::NUMERIC
              > parcel.area_sqm::NUMERIC * rule.max_floor_area_ratio_milli::NUMERIC / 1000
           OR building.completed_tick > world_tick);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 building geometry or zoning envelope is invalid'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_districts district
    LEFT JOIN LATERAL (
        SELECT
            COALESCE(SUM(building.capacity_units + COALESCE(adjustment.capacity, 0))
                FILTER (WHERE building.primary_use = 'residential'), 0)::BIGINT AS residential,
            COALESCE(SUM(building.capacity_units + COALESCE(adjustment.capacity, 0))
                FILTER (WHERE building.primary_use = 'commercial'), 0)::BIGINT AS commercial,
            COALESCE(SUM(building.capacity_units + COALESCE(adjustment.capacity, 0))
                FILTER (WHERE building.primary_use = 'industrial'), 0)::BIGINT AS industrial
        FROM city_buildings building
        LEFT JOIN LATERAL (
            SELECT COALESCE(SUM(value.added_capacity_units), 0)::BIGINT AS capacity
            FROM city_building_adjustments value
            WHERE value.world_id = building.world_id AND value.building_id = building.id
        ) adjustment ON TRUE
        WHERE building.world_id = district.world_id AND building.district_id = district.id
    ) capacity ON TRUE
    WHERE district.world_id = target_world_id
      AND (capacity.residential <> district.residential_capacity_units
           OR capacity.commercial <> district.commercial_capacity_units
           OR capacity.industrial <> district.industrial_capacity_units);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7 effective building capacity does not match district aggregates'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_buildings building
    LEFT JOIN city_building_unit_pools pool
      ON pool.world_id = building.world_id AND pool.building_id = building.id
    WHERE building.world_id = target_world_id
      AND (pool.id IS NULL OR pool.district_id <> building.district_id
           OR pool.use_type <> building.primary_use OR pool.capacity_units_per_unit <> 1
           OR pool.unit_count <> building.capacity_units
           OR pool.occupied_unit_count <> building.occupied_units);
    IF invalid_count <> 0 OR actual_pools <> actual_buildings THEN
        RAISE EXCEPTION 'city F7.3 baseline unit pool does not match baseline building capacity'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_housing_allocations allocation
    JOIN city_building_unit_pools pool
      ON pool.id = allocation.pool_id AND pool.world_id = allocation.world_id
    JOIN city_household_cohorts cohort
      ON cohort.id = allocation.cohort_id AND cohort.world_id = allocation.world_id
    JOIN city_districts district
      ON district.id = allocation.district_id AND district.world_id = allocation.world_id
    JOIN city_economic_entities entity
      ON entity.id = cohort.entity_id AND entity.world_id = cohort.world_id
    WHERE allocation.world_id = target_world_id
      AND (pool.use_type <> 'residential' OR pool.district_id <> allocation.district_id
           OR cohort.district_id <> allocation.district_id
           OR allocation.cohort_key <> district.code || '/' || entity.code || '/' || cohort.income_band);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 housing allocation identity is invalid'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_building_unit_pools pool
    LEFT JOIN LATERAL (
        SELECT COALESCE(SUM(allocation.allocated_units), 0)::BIGINT AS allocated_units
        FROM city_housing_allocations allocation
        WHERE allocation.world_id = pool.world_id AND allocation.pool_id = pool.id
    ) allocated ON TRUE
    WHERE pool.world_id = target_world_id
      AND allocated.allocated_units <> pool.occupied_unit_count;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 housing allocations do not match pool occupancy'
            USING ERRCODE = '23514';
    END IF;

    IF world_tick = baseline_row.tick THEN
        SELECT COUNT(*) INTO invalid_count
        FROM city_household_cohorts cohort
        LEFT JOIN LATERAL (
            SELECT COALESCE(SUM(allocation.allocated_units), 0)::BIGINT AS allocated_units
            FROM city_housing_allocations allocation
            WHERE allocation.world_id = cohort.world_id AND allocation.cohort_id = cohort.id
        ) allocated ON TRUE
        WHERE cohort.world_id = target_world_id
          AND allocated.allocated_units <> cohort.household_units;
        IF invalid_count <> 0 THEN
            RAISE EXCEPTION 'city F7.3 housing allocations do not conserve household units'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_building_portals portal
    JOIN city_buildings building
      ON building.id = portal.building_id AND building.world_id = portal.world_id
    WHERE portal.world_id = target_world_id
      AND (portal.district_id <> building.district_id OR NOT portal.bidirectional
           OR portal.from_z < building.base_z OR portal.from_z > building.top_z
           OR portal.to_z < building.base_z OR portal.to_z > building.top_z
           OR portal.to_x < building.chunk_x * 32 + building.local_min_x
           OR portal.to_x > building.chunk_x * 32 + building.local_max_x
           OR portal.to_y < building.chunk_y * 32 + building.local_min_y
           OR portal.to_y > building.chunk_y * 32 + building.local_max_y);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 building portal projection is invalid'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

-- F7.5 still consumes the frozen F7.1 map-generation domain.
CREATE OR REPLACE FUNCTION guard_city_spatial_mutation_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    command_type_value VARCHAR(64);
    command_status_value VARCHAR(16);
    world_tick BIGINT;
    world_version VARCHAR(32);
BEGIN
    SELECT command_type, status INTO command_type_value, command_status_value
    FROM city_commands WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
    SELECT current_tick, simulation_version INTO world_tick, world_version
    FROM city_worlds WHERE id = NEW.world_id;
    IF command_type_value IS DISTINCT FROM 'spatial.generate_chunk'
       OR command_status_value IS DISTINCT FROM 'pending'
       OR world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4')
       OR NEW.tick IS DISTINCT FROM world_tick + 1 THEN
        RAISE EXCEPTION 'city spatial mutation does not match a pending spatial generation command'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_spatial_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    profile_count BIGINT;
    tile_count BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version INTO world_version FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city spatial world does not exist' USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*) INTO profile_count FROM city_spatial_profiles WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO tile_count FROM city_overmap_tiles WHERE world_id = target_world_id;
    IF world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4') THEN
        IF profile_count <> 1 OR tile_count <> 81 THEN
            RAISE EXCEPTION 'city spatial profile or overmap is incomplete' USING ERRCODE = '23514';
        END IF;
        SELECT COUNT(*) INTO invalid_count
        FROM city_overmap_tiles tile
        JOIN city_spatial_profiles profile ON profile.world_id = tile.world_id
        LEFT JOIN city_districts district
          ON district.id = tile.district_id AND district.world_id = tile.world_id
        WHERE tile.world_id = target_world_id
          AND (district.id IS NULL
               OR tile.chunk_x < profile.minimum_chunk_x OR tile.chunk_x > profile.maximum_chunk_x
               OR tile.chunk_y < profile.minimum_chunk_y OR tile.chunk_y > profile.maximum_chunk_y
               OR tile.z <> 0);
        IF invalid_count <> 0 THEN
            RAISE EXCEPTION 'city overmap contains invalid tiles' USING ERRCODE = '23514';
        END IF;
        SELECT COUNT(*) INTO invalid_count
        FROM city_map_chunks chunk
        JOIN city_spatial_profiles profile ON profile.world_id = chunk.world_id
        LEFT JOIN city_overmap_tiles tile
          ON tile.world_id = chunk.world_id
         AND tile.chunk_x = chunk.chunk_x AND tile.chunk_y = chunk.chunk_y AND tile.z = chunk.z
        LEFT JOIN city_spatial_mutations mutation
          ON mutation.id = chunk.source_mutation_id AND mutation.world_id = chunk.world_id
        WHERE chunk.world_id = target_world_id
          AND (tile.world_id IS NULL OR mutation.posted_at IS NULL
               OR chunk.generator_id <> profile.generator_id
               OR chunk.generator_version <> profile.generator_version
               OR chunk.generated_tick <> mutation.tick);
        IF invalid_count <> 0 THEN
            RAISE EXCEPTION 'city chunk projection is inconsistent' USING ERRCODE = '23514';
        END IF;
    ELSIF profile_count <> 0 OR tile_count <> 0
          OR EXISTS (SELECT 1 FROM city_map_chunks WHERE world_id = target_world_id)
          OR EXISTS (SELECT 1 FROM city_spatial_mutations WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'legacy city engine cannot contain spatial state' USING ERRCODE = '23514';
    END IF;
END;
$$;
