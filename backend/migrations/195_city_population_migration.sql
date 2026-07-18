-- 城市模拟 F6.2：迁入、迁出与区域迁移事实。
-- 迁移只推进人口年龄投影；就业转移、家庭形成和收入层迁移属于后续版本。

CREATE TABLE IF NOT EXISTS city_population_migrations (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    source_command_id BIGINT NOT NULL,
    migration_type VARCHAR(24) NOT NULL,
    source_cohort_id BIGINT,
    target_cohort_id BIGINT,
    child_units BIGINT NOT NULL CHECK (child_units >= 0),
    working_units BIGINT NOT NULL CHECK (working_units >= 0),
    senior_units BIGINT NOT NULL CHECK (senior_units >= 0),
    expected_line_count SMALLINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    posted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_population_migration_type_check CHECK (
        migration_type IN ('immigration', 'emigration', 'district_relocation')
    ),
    CONSTRAINT city_population_migration_units_check CHECK (
        child_units::numeric + working_units::numeric + senior_units::numeric BETWEEN 1 AND 1000000000
    ),
    CONSTRAINT city_population_migration_shape_check CHECK (
        (migration_type = 'immigration' AND source_cohort_id IS NULL
            AND target_cohort_id IS NOT NULL AND expected_line_count = 1)
        OR (migration_type = 'emigration' AND source_cohort_id IS NOT NULL
            AND target_cohort_id IS NULL AND expected_line_count = 1)
        OR (migration_type = 'district_relocation' AND source_cohort_id IS NOT NULL
            AND target_cohort_id IS NOT NULL AND source_cohort_id <> target_cohort_id
            AND expected_line_count = 2)
    ),
    CONSTRAINT city_population_migration_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_population_migration_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_population_migration_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_population_migration_source_cohort_fk
        FOREIGN KEY (source_cohort_id, world_id)
        REFERENCES city_household_cohorts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_population_migration_target_cohort_fk
        FOREIGN KEY (target_cohort_id, world_id)
        REFERENCES city_household_cohorts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_population_migrations_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_population_migrations_source_command_unique UNIQUE (source_command_id),
    CONSTRAINT city_population_migrations_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_population_migrations_world_cursor
    ON city_population_migrations (world_id, tick, sequence);

CREATE TABLE IF NOT EXISTS city_population_migration_lines (
    id BIGSERIAL PRIMARY KEY,
    migration_id BIGINT NOT NULL,
    world_id BIGINT NOT NULL,
    line_no SMALLINT NOT NULL CHECK (line_no > 0),
    cohort_id BIGINT NOT NULL,
    direction VARCHAR(8) NOT NULL,
    demographic_version_before BIGINT NOT NULL CHECK (demographic_version_before >= 0),
    demographic_version_after BIGINT NOT NULL CHECK (demographic_version_after > 0),
    cohort_version_before BIGINT NOT NULL CHECK (cohort_version_before >= 0),
    cohort_version_after BIGINT NOT NULL CHECK (cohort_version_after > 0),
    child_units BIGINT NOT NULL CHECK (child_units >= 0),
    working_units BIGINT NOT NULL CHECK (working_units >= 0),
    senior_units BIGINT NOT NULL CHECK (senior_units >= 0),
    child_units_before BIGINT NOT NULL CHECK (child_units_before >= 0),
    working_units_before BIGINT NOT NULL CHECK (working_units_before >= 0),
    senior_units_before BIGINT NOT NULL CHECK (senior_units_before >= 0),
    child_units_after BIGINT NOT NULL CHECK (child_units_after >= 0),
    working_units_after BIGINT NOT NULL CHECK (working_units_after >= 0),
    senior_units_after BIGINT NOT NULL CHECK (senior_units_after >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_population_migration_line_direction_check CHECK (direction IN ('inflow', 'outflow')),
    CONSTRAINT city_population_migration_line_version_check CHECK (
        demographic_version_after = demographic_version_before + 1
        AND cohort_version_after = cohort_version_before + 1
    ),
    CONSTRAINT city_population_migration_line_child_check CHECK (
        (direction = 'inflow' AND child_units_after::numeric = child_units_before::numeric + child_units::numeric)
        OR (direction = 'outflow' AND child_units_before::numeric = child_units_after::numeric + child_units::numeric)
    ),
    CONSTRAINT city_population_migration_line_working_check CHECK (
        (direction = 'inflow' AND working_units_after::numeric = working_units_before::numeric + working_units::numeric)
        OR (direction = 'outflow' AND working_units_before::numeric = working_units_after::numeric + working_units::numeric)
    ),
    CONSTRAINT city_population_migration_line_senior_check CHECK (
        (direction = 'inflow' AND senior_units_after::numeric = senior_units_before::numeric + senior_units::numeric)
        OR (direction = 'outflow' AND senior_units_before::numeric = senior_units_after::numeric + senior_units::numeric)
    ),
    CONSTRAINT city_population_migration_line_migration_fk
        FOREIGN KEY (migration_id, world_id)
        REFERENCES city_population_migrations(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_population_migration_line_cohort_fk
        FOREIGN KEY (cohort_id, world_id)
        REFERENCES city_household_cohorts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_population_migration_lines_line_unique UNIQUE (migration_id, line_no),
    CONSTRAINT city_population_migration_lines_cohort_unique UNIQUE (migration_id, cohort_id)
);

CREATE OR REPLACE FUNCTION city_f62_migration_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_population_migrations migration
        WHERE migration.id = CASE
            WHEN COALESCE(current_setting('sub2api.city_f62_migration_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.city_f62_migration_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND migration.world_id = target_world_id
          AND migration.posted_at IS NULL
    )
$$;

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
    IF city_recovery_write_enabled(NEW.world_id)
       OR city_f6_movement_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    IF city_f62_migration_write_enabled(NEW.world_id) THEN
        IF (NEW.birth_remainder, NEW.child_death_remainder,
            NEW.working_death_remainder, NEW.senior_death_remainder,
            NEW.child_aging_remainder, NEW.working_aging_remainder, NEW.metadata)
           IS DISTINCT FROM
           (OLD.birth_remainder, OLD.child_death_remainder,
            OLD.working_death_remainder, OLD.senior_death_remainder,
            OLD.child_aging_remainder, OLD.working_aging_remainder, OLD.metadata)
           OR NEW.version <> OLD.version + 1 THEN
            RAISE EXCEPTION 'invalid city population migration projection' USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city demographic cohorts can only change through a draft population movement or migration fact'
        USING ERRCODE = '55000';
END;
$$;

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
        ELSIF city_f6_movement_write_enabled(NEW.world_id)
              OR city_f62_migration_write_enabled(NEW.world_id) THEN
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

CREATE OR REPLACE FUNCTION guard_city_population_migration_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city population migrations are immutable facts' USING ERRCODE = '55000';
    END IF;
    IF OLD.posted_at IS NOT NULL OR NEW.posted_at IS NULL
       OR (OLD.id, OLD.world_id, OLD.tick, OLD.sequence, OLD.source_command_id,
           OLD.migration_type, OLD.source_cohort_id, OLD.target_cohort_id,
           OLD.child_units, OLD.working_units, OLD.senior_units,
           OLD.expected_line_count, OLD.metadata, OLD.created_at)
          IS DISTINCT FROM
          (NEW.id, NEW.world_id, NEW.tick, NEW.sequence, NEW.source_command_id,
           NEW.migration_type, NEW.source_cohort_id, NEW.target_cohort_id,
           NEW.child_units, NEW.working_units, NEW.senior_units,
           NEW.expected_line_count, NEW.metadata, NEW.created_at) THEN
        RAISE EXCEPTION 'city population migrations permit only one draft-to-posted transition'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_population_migration_write_guard ON city_population_migrations;
CREATE TRIGGER city_population_migration_write_guard
BEFORE UPDATE OR DELETE ON city_population_migrations
FOR EACH ROW EXECUTE FUNCTION guard_city_population_migration_write();

DROP TRIGGER IF EXISTS city_population_migration_line_immutable_guard ON city_population_migration_lines;
CREATE TRIGGER city_population_migration_line_immutable_guard
BEFORE UPDATE OR DELETE ON city_population_migration_lines
FOR EACH ROW EXECUTE FUNCTION reject_city_immutable_fact_mutation();

CREATE OR REPLACE FUNCTION assert_city_population_migration_committed()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    migration_row city_population_migrations%ROWTYPE;
    command_type_value VARCHAR(64);
    command_status_value VARCHAR(16);
    command_tick BIGINT;
    expected_command_type VARCHAR(64);
    actual_line_count BIGINT;
    inflow_line_count BIGINT;
    outflow_line_count BIGINT;
    inflow_child NUMERIC;
    inflow_working NUMERIC;
    inflow_senior NUMERIC;
    outflow_child NUMERIC;
    outflow_working NUMERIC;
    outflow_senior NUMERIC;
    relocation_valid BOOLEAN;
BEGIN
    SELECT * INTO migration_row FROM city_population_migrations WHERE id = NEW.id;
    IF migration_row.posted_at IS NULL THEN
        RAISE EXCEPTION 'city population migration must be posted before commit' USING ERRCODE = '23514';
    END IF;

    SELECT command_type, status, processed_tick
      INTO command_type_value, command_status_value, command_tick
    FROM city_commands
    WHERE id = migration_row.source_command_id AND world_id = migration_row.world_id;
    expected_command_type := CASE migration_row.migration_type
        WHEN 'immigration' THEN 'population.immigrate'
        WHEN 'emigration' THEN 'population.emigrate'
        ELSE 'population.relocate'
    END;
    IF command_type_value IS DISTINCT FROM expected_command_type
       OR command_status_value IS DISTINCT FROM 'applied'
       OR command_tick IS DISTINCT FROM migration_row.tick THEN
        RAISE EXCEPTION 'city population migration does not match its applied command'
            USING ERRCODE = '23514';
    END IF;

    IF migration_row.migration_type = 'district_relocation' THEN
        SELECT source.district_id <> target.district_id
               AND source.income_band = target.income_band
          INTO relocation_valid
        FROM city_household_cohorts source, city_household_cohorts target
        WHERE source.id = migration_row.source_cohort_id
          AND target.id = migration_row.target_cohort_id
          AND source.world_id = migration_row.world_id
          AND target.world_id = migration_row.world_id;
        IF relocation_valid IS DISTINCT FROM TRUE THEN
            RAISE EXCEPTION 'city district relocation must preserve income band and change district'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    SELECT COUNT(*),
           COUNT(*) FILTER (WHERE direction = 'inflow'),
           COUNT(*) FILTER (WHERE direction = 'outflow'),
           COALESCE(SUM(child_units::NUMERIC) FILTER (WHERE direction = 'inflow'), 0),
           COALESCE(SUM(working_units::NUMERIC) FILTER (WHERE direction = 'inflow'), 0),
           COALESCE(SUM(senior_units::NUMERIC) FILTER (WHERE direction = 'inflow'), 0),
           COALESCE(SUM(child_units::NUMERIC) FILTER (WHERE direction = 'outflow'), 0),
           COALESCE(SUM(working_units::NUMERIC) FILTER (WHERE direction = 'outflow'), 0),
           COALESCE(SUM(senior_units::NUMERIC) FILTER (WHERE direction = 'outflow'), 0)
      INTO actual_line_count, inflow_line_count, outflow_line_count,
           inflow_child, inflow_working, inflow_senior,
           outflow_child, outflow_working, outflow_senior
    FROM city_population_migration_lines WHERE migration_id = migration_row.id;

    IF actual_line_count <> migration_row.expected_line_count
       OR (migration_row.migration_type = 'immigration' AND (
            inflow_line_count <> 1 OR outflow_line_count <> 0
            OR NOT EXISTS (
                SELECT 1 FROM city_population_migration_lines line
                WHERE line.migration_id = migration_row.id
                  AND line.cohort_id = migration_row.target_cohort_id AND line.direction = 'inflow'
            )
            OR (inflow_child, inflow_working, inflow_senior) IS DISTINCT FROM
               (migration_row.child_units::NUMERIC, migration_row.working_units::NUMERIC,
                migration_row.senior_units::NUMERIC)))
       OR (migration_row.migration_type = 'emigration' AND (
            inflow_line_count <> 0 OR outflow_line_count <> 1
            OR NOT EXISTS (
                SELECT 1 FROM city_population_migration_lines line
                WHERE line.migration_id = migration_row.id
                  AND line.cohort_id = migration_row.source_cohort_id AND line.direction = 'outflow'
            )
            OR (outflow_child, outflow_working, outflow_senior) IS DISTINCT FROM
               (migration_row.child_units::NUMERIC, migration_row.working_units::NUMERIC,
                migration_row.senior_units::NUMERIC)))
       OR (migration_row.migration_type = 'district_relocation' AND (
            inflow_line_count <> 1 OR outflow_line_count <> 1
            OR NOT EXISTS (
                SELECT 1 FROM city_population_migration_lines line
                WHERE line.migration_id = migration_row.id
                  AND line.cohort_id = migration_row.source_cohort_id AND line.direction = 'outflow'
            )
            OR NOT EXISTS (
                SELECT 1 FROM city_population_migration_lines line
                WHERE line.migration_id = migration_row.id
                  AND line.cohort_id = migration_row.target_cohort_id AND line.direction = 'inflow'
            )
            OR (inflow_child, inflow_working, inflow_senior) IS DISTINCT FROM
               (migration_row.child_units::NUMERIC, migration_row.working_units::NUMERIC,
                migration_row.senior_units::NUMERIC)
            OR (outflow_child, outflow_working, outflow_senior) IS DISTINCT FROM
               (migration_row.child_units::NUMERIC, migration_row.working_units::NUMERIC,
                migration_row.senior_units::NUMERIC))) THEN
        RAISE EXCEPTION 'city population migration summary does not match immutable lines'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_population_migration_commit_check ON city_population_migrations;
CREATE CONSTRAINT TRIGGER city_population_migration_commit_check
AFTER INSERT OR UPDATE ON city_population_migrations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_population_migration_committed();

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f6-v2', 'supported', 'city-state-v1+gzip',
        '["control","ledger","resources","calendar_demography","population_migration","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f6-v1', 'city-f6-v2', 'f6_v1_to_f6_v2')
ON CONFLICT (from_version, to_version) DO NOTHING;
