-- 城市模拟 F6.1：世界本地日历边界、聚合年龄结构和自然人口变化。
-- 人口投影只能由已封账 movement 推进；利率、信贷和证券不属于本迁移。

-- 版本升级允许同一 tick 保留旧版本检查点并创建新版本基线，绝不覆盖 F5 快照。
ALTER TABLE city_snapshots
    DROP CONSTRAINT IF EXISTS city_snapshots_world_tick_unique;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'city_snapshots_world_tick_version_unique'
    ) THEN
        ALTER TABLE city_snapshots
            ADD CONSTRAINT city_snapshots_world_tick_version_unique
            UNIQUE (world_id, tick, simulation_version);
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_city_snapshots_world_version_cursor
    ON city_snapshots (world_id, simulation_version, tick DESC);

CREATE TABLE IF NOT EXISTS city_calendar_states (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    local_date DATE NOT NULL,
    day_index BIGINT NOT NULL DEFAULT 0 CHECK (day_index >= 0),
    month_index BIGINT NOT NULL DEFAULT 0 CHECK (month_index >= 0),
    quarter_index BIGINT NOT NULL DEFAULT 0 CHECK (quarter_index >= 0),
    year_index BIGINT NOT NULL DEFAULT 0 CHECK (year_index >= 0),
    last_daily_tick BIGINT CHECK (last_daily_tick IS NULL OR last_daily_tick > 0),
    last_monthly_tick BIGINT CHECK (last_monthly_tick IS NULL OR last_monthly_tick > 0),
    last_quarterly_tick BIGINT CHECK (last_quarterly_tick IS NULL OR last_quarterly_tick > 0),
    last_annual_tick BIGINT CHECK (last_annual_tick IS NULL OR last_annual_tick > 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    metadata JSONB NOT NULL DEFAULT '{"schema_version":1}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_calendar_state_tick_order_check CHECK (
        (last_monthly_tick IS NULL OR last_daily_tick IS NOT NULL)
        AND (last_quarterly_tick IS NULL OR last_monthly_tick IS NOT NULL)
        AND (last_annual_tick IS NULL OR last_quarterly_tick IS NOT NULL)
        AND (last_daily_tick IS NULL OR last_monthly_tick IS NULL OR last_monthly_tick <= last_daily_tick)
        AND (last_monthly_tick IS NULL OR last_quarterly_tick IS NULL OR last_quarterly_tick <= last_monthly_tick)
        AND (last_quarterly_tick IS NULL OR last_annual_tick IS NULL OR last_annual_tick <= last_quarterly_tick)
    ),
    CONSTRAINT city_calendar_state_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_calendar_boundaries (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence SMALLINT NOT NULL CHECK (sequence BETWEEN 1 AND 4),
    boundary_type VARCHAR(8) NOT NULL,
    previous_local_date DATE NOT NULL,
    local_date DATE NOT NULL,
    period_index BIGINT NOT NULL CHECK (period_index > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_calendar_boundary_type_check CHECK (boundary_type IN ('day', 'month', 'quarter', 'year')),
    CONSTRAINT city_calendar_boundary_date_check CHECK (local_date = previous_local_date + 1),
    CONSTRAINT city_calendar_boundary_sequence_type_check CHECK (
        (boundary_type = 'day' AND sequence = 1)
        OR (boundary_type = 'month' AND sequence = 2)
        OR (boundary_type = 'quarter' AND sequence = 3)
        OR (boundary_type = 'year' AND sequence = 4)
    ),
    CONSTRAINT city_calendar_boundary_period_date_check CHECK (
        boundary_type = 'day'
        OR (boundary_type = 'month' AND EXTRACT(DAY FROM local_date) = 1)
        OR (boundary_type = 'quarter' AND EXTRACT(DAY FROM local_date) = 1
            AND EXTRACT(MONTH FROM local_date) IN (1, 4, 7, 10))
        OR (boundary_type = 'year' AND EXTRACT(DAY FROM local_date) = 1
            AND EXTRACT(MONTH FROM local_date) = 1)
    ),
    CONSTRAINT city_calendar_boundary_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_calendar_boundaries_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_calendar_boundaries_world_tick_type_unique UNIQUE (world_id, tick, boundary_type),
    CONSTRAINT city_calendar_boundaries_world_type_date_unique UNIQUE (world_id, boundary_type, local_date),
    CONSTRAINT city_calendar_boundaries_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_calendar_boundaries_world_cursor
    ON city_calendar_boundaries (world_id, tick, sequence);

CREATE TABLE IF NOT EXISTS city_demographic_policies (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    parameter_set_code VARCHAR(32) NOT NULL DEFAULT 'baseline_v1',
    parameter_version INTEGER NOT NULL DEFAULT 1 CHECK (parameter_version > 0),
    periods_per_year SMALLINT NOT NULL DEFAULT 12 CHECK (periods_per_year = 12),
    birth_rate_ppm INTEGER NOT NULL DEFAULT 12000 CHECK (birth_rate_ppm BETWEEN 0 AND 1000000),
    child_death_rate_ppm INTEGER NOT NULL DEFAULT 500 CHECK (child_death_rate_ppm BETWEEN 0 AND 1000000),
    working_death_rate_ppm INTEGER NOT NULL DEFAULT 1000 CHECK (working_death_rate_ppm BETWEEN 0 AND 1000000),
    senior_death_rate_ppm INTEGER NOT NULL DEFAULT 12000 CHECK (senior_death_rate_ppm BETWEEN 0 AND 1000000),
    child_to_working_rate_ppm INTEGER NOT NULL DEFAULT 55000 CHECK (child_to_working_rate_ppm BETWEEN 0 AND 1000000),
    working_to_senior_rate_ppm INTEGER NOT NULL DEFAULT 22000 CHECK (working_to_senior_rate_ppm BETWEEN 0 AND 1000000),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    metadata JSONB NOT NULL DEFAULT '{"schema_version":1}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_demographic_policy_code_check CHECK (parameter_set_code ~ '^[a-z][a-z0-9_]{1,31}$'),
    CONSTRAINT city_demographic_policy_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_demographic_cohort_states (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    cohort_id BIGINT NOT NULL,
    child_units BIGINT NOT NULL CHECK (child_units >= 0),
    working_units BIGINT NOT NULL CHECK (working_units >= 0),
    senior_units BIGINT NOT NULL CHECK (senior_units >= 0),
    birth_remainder BIGINT NOT NULL DEFAULT 0 CHECK (birth_remainder BETWEEN 0 AND 11999999),
    child_death_remainder BIGINT NOT NULL DEFAULT 0 CHECK (child_death_remainder BETWEEN 0 AND 11999999),
    working_death_remainder BIGINT NOT NULL DEFAULT 0 CHECK (working_death_remainder BETWEEN 0 AND 11999999),
    senior_death_remainder BIGINT NOT NULL DEFAULT 0 CHECK (senior_death_remainder BETWEEN 0 AND 11999999),
    child_aging_remainder BIGINT NOT NULL DEFAULT 0 CHECK (child_aging_remainder BETWEEN 0 AND 11999999),
    working_aging_remainder BIGINT NOT NULL DEFAULT 0 CHECK (working_aging_remainder BETWEEN 0 AND 11999999),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    metadata JSONB NOT NULL DEFAULT '{"schema_version":1}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_demographic_cohort_fk
        FOREIGN KEY (cohort_id, world_id)
        REFERENCES city_household_cohorts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_demographic_cohort_population_check CHECK (
        child_units::numeric + working_units::numeric + senior_units::numeric > 0
    ),
    CONSTRAINT city_demographic_cohort_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_demographic_cohorts_world_cohort_unique UNIQUE (world_id, cohort_id),
    CONSTRAINT city_demographic_cohorts_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_demographic_cohorts_world
    ON city_demographic_cohort_states (world_id, cohort_id);

CREATE TABLE IF NOT EXISTS city_population_movements (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    boundary_id BIGINT NOT NULL,
    movement_type VARCHAR(24) NOT NULL DEFAULT 'natural_change',
    local_month DATE NOT NULL,
    parameter_set_code VARCHAR(32) NOT NULL,
    parameter_version INTEGER NOT NULL CHECK (parameter_version > 0),
    expected_line_count INTEGER NOT NULL CHECK (expected_line_count > 0),
    total_birth_units BIGINT NOT NULL CHECK (total_birth_units >= 0),
    total_death_units BIGINT NOT NULL CHECK (total_death_units >= 0),
    total_transition_units BIGINT NOT NULL CHECK (total_transition_units >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    posted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_population_movement_type_check CHECK (movement_type = 'natural_change'),
    CONSTRAINT city_population_movement_month_check CHECK (EXTRACT(DAY FROM local_month) = 1),
    CONSTRAINT city_population_movement_parameter_code_check CHECK (parameter_set_code ~ '^[a-z][a-z0-9_]{1,31}$'),
    CONSTRAINT city_population_movement_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_population_movement_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_population_movement_boundary_fk
        FOREIGN KEY (boundary_id, world_id)
        REFERENCES city_calendar_boundaries(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_population_movements_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_population_movements_world_month_unique UNIQUE (world_id, movement_type, local_month),
    CONSTRAINT city_population_movements_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_population_movements_world_cursor
    ON city_population_movements (world_id, tick, sequence);

CREATE TABLE IF NOT EXISTS city_population_movement_lines (
    id BIGSERIAL PRIMARY KEY,
    movement_id BIGINT NOT NULL,
    world_id BIGINT NOT NULL,
    line_no INTEGER NOT NULL CHECK (line_no > 0),
    cohort_id BIGINT NOT NULL,
    demographic_version_before BIGINT NOT NULL CHECK (demographic_version_before >= 0),
    demographic_version_after BIGINT NOT NULL CHECK (demographic_version_after > 0),
    cohort_version_before BIGINT NOT NULL CHECK (cohort_version_before >= 0),
    cohort_version_after BIGINT NOT NULL CHECK (cohort_version_after > 0),
    birth_units BIGINT NOT NULL CHECK (birth_units >= 0),
    child_death_units BIGINT NOT NULL CHECK (child_death_units >= 0),
    working_death_units BIGINT NOT NULL CHECK (working_death_units >= 0),
    senior_death_units BIGINT NOT NULL CHECK (senior_death_units >= 0),
    child_to_working_units BIGINT NOT NULL CHECK (child_to_working_units >= 0),
    working_to_senior_units BIGINT NOT NULL CHECK (working_to_senior_units >= 0),
    child_units_before BIGINT NOT NULL CHECK (child_units_before >= 0),
    working_units_before BIGINT NOT NULL CHECK (working_units_before >= 0),
    senior_units_before BIGINT NOT NULL CHECK (senior_units_before >= 0),
    child_units_after BIGINT NOT NULL CHECK (child_units_after >= 0),
    working_units_after BIGINT NOT NULL CHECK (working_units_after >= 0),
    senior_units_after BIGINT NOT NULL CHECK (senior_units_after >= 0),
    birth_remainder_before BIGINT NOT NULL CHECK (birth_remainder_before BETWEEN 0 AND 11999999),
    child_death_remainder_before BIGINT NOT NULL CHECK (child_death_remainder_before BETWEEN 0 AND 11999999),
    working_death_remainder_before BIGINT NOT NULL CHECK (working_death_remainder_before BETWEEN 0 AND 11999999),
    senior_death_remainder_before BIGINT NOT NULL CHECK (senior_death_remainder_before BETWEEN 0 AND 11999999),
    child_aging_remainder_before BIGINT NOT NULL CHECK (child_aging_remainder_before BETWEEN 0 AND 11999999),
    working_aging_remainder_before BIGINT NOT NULL CHECK (working_aging_remainder_before BETWEEN 0 AND 11999999),
    birth_remainder_after BIGINT NOT NULL CHECK (birth_remainder_after BETWEEN 0 AND 11999999),
    child_death_remainder_after BIGINT NOT NULL CHECK (child_death_remainder_after BETWEEN 0 AND 11999999),
    working_death_remainder_after BIGINT NOT NULL CHECK (working_death_remainder_after BETWEEN 0 AND 11999999),
    senior_death_remainder_after BIGINT NOT NULL CHECK (senior_death_remainder_after BETWEEN 0 AND 11999999),
    child_aging_remainder_after BIGINT NOT NULL CHECK (child_aging_remainder_after BETWEEN 0 AND 11999999),
    working_aging_remainder_after BIGINT NOT NULL CHECK (working_aging_remainder_after BETWEEN 0 AND 11999999),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_population_movement_line_movement_fk
        FOREIGN KEY (movement_id, world_id)
        REFERENCES city_population_movements(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_population_movement_line_cohort_fk
        FOREIGN KEY (cohort_id, world_id)
        REFERENCES city_household_cohorts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_population_movement_line_version_check CHECK (
        demographic_version_after = demographic_version_before + 1
        AND cohort_version_after = cohort_version_before + 1
    ),
    CONSTRAINT city_population_movement_line_child_check CHECK (
        child_units_after::numeric = child_units_before::numeric + birth_units::numeric
            - child_death_units::numeric - child_to_working_units::numeric
    ),
    CONSTRAINT city_population_movement_line_working_check CHECK (
        working_units_after::numeric = working_units_before::numeric + child_to_working_units::numeric
            - working_death_units::numeric - working_to_senior_units::numeric
    ),
    CONSTRAINT city_population_movement_line_senior_check CHECK (
        senior_units_after::numeric = senior_units_before::numeric + working_to_senior_units::numeric
            - senior_death_units::numeric
    ),
    CONSTRAINT city_population_movement_lines_movement_line_unique UNIQUE (movement_id, line_no),
    CONSTRAINT city_population_movement_lines_movement_cohort_unique UNIQUE (movement_id, cohort_id)
);

CREATE OR REPLACE FUNCTION city_f6_boundary_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_calendar_boundaries boundary
        WHERE boundary.id = CASE
            WHEN COALESCE(current_setting('sub2api.city_f6_boundary_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.city_f6_boundary_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND boundary.world_id = target_world_id
          AND boundary.boundary_type = 'day'
    )
$$;

CREATE OR REPLACE FUNCTION city_f6_movement_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_population_movements movement
        WHERE movement.id = CASE
            WHEN COALESCE(current_setting('sub2api.city_f6_movement_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.city_f6_movement_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND movement.world_id = target_world_id
          AND movement.posted_at IS NULL
    )
$$;

CREATE OR REPLACE FUNCTION guard_city_calendar_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city calendar projection cannot be deleted' USING ERRCODE = '55000';
    END IF;
    IF NEW.world_id IS DISTINCT FROM OLD.world_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'city calendar projection identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF NOT city_recovery_write_enabled(NEW.world_id)
       AND NOT city_f6_boundary_write_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city calendar can only advance through an immutable boundary'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_calendar_projection_guard ON city_calendar_states;
CREATE TRIGGER city_calendar_projection_guard
BEFORE UPDATE OR DELETE ON city_calendar_states
FOR EACH ROW EXECUTE FUNCTION guard_city_calendar_projection();

CREATE OR REPLACE FUNCTION assert_city_calendar_projection(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    calendar city_calendar_states%ROWTYPE;
BEGIN
    SELECT * INTO calendar FROM city_calendar_states WHERE world_id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city calendar projection is missing' USING ERRCODE = '23514';
    END IF;
    IF (calendar.day_index = 0) <> (calendar.last_daily_tick IS NULL)
       OR (calendar.month_index = 0) <> (calendar.last_monthly_tick IS NULL)
       OR (calendar.quarter_index = 0) <> (calendar.last_quarterly_tick IS NULL)
       OR (calendar.year_index = 0) <> (calendar.last_annual_tick IS NULL) THEN
        RAISE EXCEPTION 'city calendar indexes and boundary ticks are inconsistent'
            USING ERRCODE = '23514';
    END IF;
    IF calendar.day_index > 0 AND NOT EXISTS (
        SELECT 1 FROM city_calendar_boundaries boundary
        WHERE boundary.world_id = target_world_id
          AND boundary.tick = calendar.last_daily_tick
          AND boundary.boundary_type = 'day'
          AND boundary.period_index = calendar.day_index
          AND boundary.local_date = calendar.local_date
    ) THEN
        RAISE EXCEPTION 'city calendar daily projection does not match its boundary'
            USING ERRCODE = '23514';
    END IF;
    IF calendar.month_index > 0 AND NOT EXISTS (
        SELECT 1 FROM city_calendar_boundaries boundary
        WHERE boundary.world_id = target_world_id
          AND boundary.tick = calendar.last_monthly_tick
          AND boundary.boundary_type = 'month'
          AND boundary.period_index = calendar.month_index
    ) THEN
        RAISE EXCEPTION 'city calendar monthly projection does not match its boundary'
            USING ERRCODE = '23514';
    END IF;
    IF calendar.quarter_index > 0 AND NOT EXISTS (
        SELECT 1 FROM city_calendar_boundaries boundary
        WHERE boundary.world_id = target_world_id
          AND boundary.tick = calendar.last_quarterly_tick
          AND boundary.boundary_type = 'quarter'
          AND boundary.period_index = calendar.quarter_index
    ) THEN
        RAISE EXCEPTION 'city calendar quarterly projection does not match its boundary'
            USING ERRCODE = '23514';
    END IF;
    IF calendar.year_index > 0 AND NOT EXISTS (
        SELECT 1 FROM city_calendar_boundaries boundary
        WHERE boundary.world_id = target_world_id
          AND boundary.tick = calendar.last_annual_tick
          AND boundary.boundary_type = 'year'
          AND boundary.period_index = calendar.year_index
    ) THEN
        RAISE EXCEPTION 'city calendar annual projection does not match its boundary'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_calendar_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM assert_city_calendar_projection(NEW.world_id);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_calendar_state_commit_check ON city_calendar_states;
CREATE CONSTRAINT TRIGGER city_calendar_state_commit_check
AFTER INSERT OR UPDATE ON city_calendar_states
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_calendar_projection();

CREATE OR REPLACE FUNCTION guard_city_demographic_policy_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city demographic policy requires a future versioned policy command'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.world_id IS DISTINCT FROM OLD.world_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'city demographic policy identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF NOT city_recovery_write_enabled(OLD.world_id) THEN
        RAISE EXCEPTION 'city demographic policy requires a future versioned policy command'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_demographic_policy_projection_guard ON city_demographic_policies;
CREATE TRIGGER city_demographic_policy_projection_guard
BEFORE UPDATE OR DELETE ON city_demographic_policies
FOR EACH ROW EXECUTE FUNCTION guard_city_demographic_policy_projection();

CREATE OR REPLACE FUNCTION guard_city_demographic_cohort_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city demographic cohort projection cannot be deleted' USING ERRCODE = '55000';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.world_id IS DISTINCT FROM OLD.world_id
       OR NEW.cohort_id IS DISTINCT FROM OLD.cohort_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'city demographic cohort identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF NOT city_recovery_write_enabled(NEW.world_id)
       AND NOT city_f6_movement_write_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city demographic cohorts can only change through a draft population movement'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_demographic_cohort_projection_guard ON city_demographic_cohort_states;
CREATE TRIGGER city_demographic_cohort_projection_guard
BEFORE UPDATE OR DELETE ON city_demographic_cohort_states
FOR EACH ROW EXECUTE FUNCTION guard_city_demographic_cohort_projection();

-- 收紧 F4 的 cohort 投影保护，同时允许 F6 movement 与受审计恢复。
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
        ELSIF city_f4_write_enabled() THEN
            IF (NEW.population_units, NEW.working_age_units, NEW.housing_demand_units, NEW.metadata)
               IS DISTINCT FROM
               (OLD.population_units, OLD.working_age_units, OLD.housing_demand_units, OLD.metadata)
               OR NEW.version <> OLD.version + 1 THEN
                RAISE EXCEPTION 'invalid city household labor settlement projection'
                    USING ERRCODE = '55000';
            END IF;
        ELSIF city_f6_movement_write_enabled(NEW.world_id) THEN
            IF (NEW.employed_units, NEW.housing_demand_units, NEW.metadata)
               IS DISTINCT FROM (OLD.employed_units, OLD.housing_demand_units, OLD.metadata)
               OR NEW.version <> OLD.version + 1 THEN
                RAISE EXCEPTION 'invalid city household demographic projection'
                    USING ERRCODE = '55000';
            END IF;
        ELSIF (NEW.population_units, NEW.working_age_units, NEW.employed_units,
               NEW.housing_demand_units, NEW.version, NEW.metadata)
              IS DISTINCT FROM
              (OLD.population_units, OLD.working_age_units, OLD.employed_units,
               OLD.housing_demand_units, OLD.version, OLD.metadata) THEN
            RAISE EXCEPTION 'city household cohort can only change through posted projections'
                USING ERRCODE = '55000';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_firm_states'
          AND NOT city_recovery_write_enabled(NEW.world_id)
          AND NOT city_f4_write_enabled()
          AND (NEW.employee_units, NEW.version) IS DISTINCT FROM (OLD.employee_units, OLD.version) THEN
        RAISE EXCEPTION 'city firm employment can only change through labor settlement'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_calendar_boundary_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'city calendar boundaries are immutable facts' USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_calendar_boundary_immutable_guard ON city_calendar_boundaries;
CREATE TRIGGER city_calendar_boundary_immutable_guard
BEFORE UPDATE OR DELETE ON city_calendar_boundaries
FOR EACH ROW EXECUTE FUNCTION guard_city_calendar_boundary_immutable();

CREATE OR REPLACE FUNCTION guard_city_population_movement_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city population movements are immutable facts' USING ERRCODE = '55000';
    END IF;
    IF OLD.posted_at IS NOT NULL OR NEW.posted_at IS NULL
       OR (OLD.id, OLD.world_id, OLD.tick, OLD.sequence, OLD.boundary_id,
           OLD.movement_type, OLD.local_month, OLD.parameter_set_code,
           OLD.parameter_version, OLD.expected_line_count, OLD.total_birth_units,
           OLD.total_death_units, OLD.total_transition_units, OLD.metadata, OLD.created_at)
          IS DISTINCT FROM
          (NEW.id, NEW.world_id, NEW.tick, NEW.sequence, NEW.boundary_id,
           NEW.movement_type, NEW.local_month, NEW.parameter_set_code,
           NEW.parameter_version, NEW.expected_line_count, NEW.total_birth_units,
           NEW.total_death_units, NEW.total_transition_units, NEW.metadata, NEW.created_at) THEN
        RAISE EXCEPTION 'city population movements permit only one draft-to-posted transition'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_population_movement_write_guard ON city_population_movements;
CREATE TRIGGER city_population_movement_write_guard
BEFORE UPDATE OR DELETE ON city_population_movements
FOR EACH ROW EXECUTE FUNCTION guard_city_population_movement_write();

CREATE OR REPLACE FUNCTION guard_city_population_movement_line_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'city population movement lines are immutable facts' USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_population_movement_line_immutable_guard ON city_population_movement_lines;
CREATE TRIGGER city_population_movement_line_immutable_guard
BEFORE UPDATE OR DELETE ON city_population_movement_lines
FOR EACH ROW EXECUTE FUNCTION guard_city_population_movement_line_immutable();

CREATE OR REPLACE FUNCTION assert_city_population_movement_committed()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    movement_row city_population_movements%ROWTYPE;
    actual_line_count BIGINT;
    actual_births NUMERIC;
    actual_deaths NUMERIC;
    actual_transitions NUMERIC;
    boundary_count BIGINT;
    demographic_count BIGINT;
BEGIN
    SELECT * INTO movement_row FROM city_population_movements WHERE id = NEW.id;
    IF movement_row.posted_at IS NULL THEN
        RAISE EXCEPTION 'city population movement must be posted before commit'
            USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*) INTO boundary_count
    FROM city_calendar_boundaries boundary
    WHERE boundary.id = movement_row.boundary_id
      AND boundary.world_id = movement_row.world_id
      AND boundary.tick = movement_row.tick
      AND boundary.boundary_type = 'month'
      AND boundary.local_date = movement_row.local_month;
    SELECT COUNT(*) INTO demographic_count
    FROM city_demographic_cohort_states
    WHERE world_id = movement_row.world_id;
    IF boundary_count <> 1 OR demographic_count = 0
       OR movement_row.expected_line_count <> demographic_count THEN
        RAISE EXCEPTION 'city population movement does not match its monthly boundary and cohort set'
            USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*), COALESCE(SUM(birth_units::NUMERIC), 0),
           COALESCE(SUM(child_death_units::NUMERIC
                        + working_death_units::NUMERIC
                        + senior_death_units::NUMERIC), 0),
           COALESCE(SUM(child_to_working_units::NUMERIC
                        + working_to_senior_units::NUMERIC), 0)
      INTO actual_line_count, actual_births, actual_deaths, actual_transitions
    FROM city_population_movement_lines WHERE movement_id = NEW.id;
    IF actual_line_count <> movement_row.expected_line_count
       OR actual_births <> movement_row.total_birth_units
       OR actual_deaths <> movement_row.total_death_units
       OR actual_transitions <> movement_row.total_transition_units THEN
        RAISE EXCEPTION 'city population movement summary does not match immutable lines'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_population_movement_commit_check ON city_population_movements;
CREATE CONSTRAINT TRIGGER city_population_movement_commit_check
AFTER INSERT OR UPDATE ON city_population_movements
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_population_movement_committed();

CREATE OR REPLACE FUNCTION assert_city_demography_projection(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    cohort_count BIGINT;
    demographic_count BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT COUNT(*) INTO cohort_count FROM city_household_cohorts WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO demographic_count FROM city_demographic_cohort_states WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO invalid_count
    FROM city_household_cohorts cohort
    LEFT JOIN city_demographic_cohort_states demographic
      ON demographic.cohort_id = cohort.id AND demographic.world_id = cohort.world_id
    WHERE cohort.world_id = target_world_id
      AND (demographic.id IS NULL
        OR demographic.child_units::numeric + demographic.working_units::numeric
             + demographic.senior_units::numeric <> cohort.population_units::numeric
        OR demographic.working_units <> cohort.working_age_units);
    IF cohort_count = 0 OR demographic_count <> cohort_count OR invalid_count <> 0 THEN
        RAISE EXCEPTION 'city demographic projection does not match household cohorts'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_demography_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM assert_city_demography_projection(CASE WHEN TG_OP = 'DELETE' THEN OLD.world_id ELSE NEW.world_id END);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_demographic_state_commit_check ON city_demographic_cohort_states;
CREATE CONSTRAINT TRIGGER city_demographic_state_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_demographic_cohort_states
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_demography_projection();

DROP TRIGGER IF EXISTS city_household_demography_commit_check ON city_household_cohorts;
CREATE CONSTRAINT TRIGGER city_household_demography_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_household_cohorts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_demography_projection();

CREATE OR REPLACE FUNCTION initialize_city_f6_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO city_calendar_states (world_id, local_date)
    SELECT world.id, (world.simulated_at AT TIME ZONE world.timezone)::DATE
    FROM city_worlds world WHERE world.id = target_world_id
    ON CONFLICT (world_id) DO NOTHING;

    INSERT INTO city_demographic_policies (world_id)
    VALUES (target_world_id)
    ON CONFLICT (world_id) DO NOTHING;

    INSERT INTO city_demographic_cohort_states
        (world_id, cohort_id, child_units, working_units, senior_units)
    SELECT cohort.world_id, cohort.id,
           FLOOR((cohort.population_units - cohort.working_age_units)::NUMERIC * 4 / 7)::BIGINT,
           cohort.working_age_units,
           cohort.population_units - cohort.working_age_units
             - FLOOR((cohort.population_units - cohort.working_age_units)::NUMERIC * 4 / 7)::BIGINT
    FROM city_household_cohorts cohort
    WHERE cohort.world_id = target_world_id
    ON CONFLICT (world_id, cohort_id) DO NOTHING;

    PERFORM assert_city_demography_projection(target_world_id);
END;
$$;

SELECT initialize_city_f6_foundation(id) FROM city_worlds;

UPDATE city_worlds
SET simulation_version = 'city-f6-v1', state_hash = NULL
WHERE simulation_version = 'city-f5-v1';
