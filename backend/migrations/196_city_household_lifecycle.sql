-- 城市模拟 F6.3：独立家庭数量、家庭生命周期与收入层迁移事实。

ALTER TABLE city_household_cohorts
    ADD COLUMN IF NOT EXISTS household_units BIGINT;

UPDATE city_household_cohorts cohort
SET household_units = GREATEST(
        1::BIGINT,
        CEIL(cohort.population_units::NUMERIC / 3)::BIGINT,
        COALESCE((
            SELECT occupancy.occupied_units
            FROM city_housing_occupancies occupancy
            WHERE occupancy.world_id = cohort.world_id
              AND occupancy.cohort_id = cohort.id
        ), 0::BIGINT)
    )
WHERE cohort.household_units IS NULL;

ALTER TABLE city_household_cohorts
    ALTER COLUMN household_units SET DEFAULT 1,
    ALTER COLUMN household_units SET NOT NULL;

ALTER TABLE city_household_cohorts
    DROP CONSTRAINT IF EXISTS city_household_cohort_population_check;
ALTER TABLE city_household_cohorts
    ADD CONSTRAINT city_household_cohort_population_check CHECK (
        working_age_units <= population_units
        AND employed_units <= working_age_units
        AND household_units > 0
    );

CREATE TABLE IF NOT EXISTS city_household_movements (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    origin VARCHAR(24) NOT NULL,
    source_command_id BIGINT,
    movement_type VARCHAR(32) NOT NULL,
    source_cohort_id BIGINT,
    target_cohort_id BIGINT,
    child_units BIGINT NOT NULL DEFAULT 0 CHECK (child_units >= 0),
    working_units BIGINT NOT NULL DEFAULT 0 CHECK (working_units >= 0),
    senior_units BIGINT NOT NULL DEFAULT 0 CHECK (senior_units >= 0),
    employed_units BIGINT NOT NULL DEFAULT 0 CHECK (employed_units >= 0),
    household_units BIGINT NOT NULL CHECK (household_units BETWEEN 1 AND 1000000000),
    occupied_units BIGINT NOT NULL DEFAULT 0 CHECK (occupied_units >= 0),
    expected_line_count SMALLINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    posted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_household_movement_origin_check CHECK (
        origin IN ('command', 'demography_guard')
    ),
    CONSTRAINT city_household_movement_type_check CHECK (
        movement_type IN ('formation', 'split', 'merge', 'dissolution', 'income_reclassification')
    ),
    CONSTRAINT city_household_movement_quantity_check CHECK (
        employed_units <= working_units
        AND occupied_units <= household_units
        AND child_units::NUMERIC + working_units::NUMERIC + senior_units::NUMERIC <= 1000000000
    ),
    CONSTRAINT city_household_movement_shape_check CHECK (
        (movement_type IN ('formation', 'split')
            AND origin = 'command' AND source_command_id IS NOT NULL
            AND source_cohort_id IS NULL AND target_cohort_id IS NOT NULL
            AND child_units = 0 AND working_units = 0 AND senior_units = 0
            AND employed_units = 0 AND occupied_units = 0 AND expected_line_count = 1)
        OR (movement_type IN ('merge', 'dissolution')
            AND source_cohort_id IS NOT NULL AND target_cohort_id IS NULL
            AND child_units = 0 AND working_units = 0 AND senior_units = 0
            AND employed_units = 0 AND expected_line_count = 1
            AND ((origin = 'command' AND source_command_id IS NOT NULL)
                 OR (origin = 'demography_guard' AND source_command_id IS NULL
                     AND movement_type = 'dissolution')))
        OR (movement_type = 'income_reclassification'
            AND origin = 'command' AND source_command_id IS NOT NULL
            AND source_cohort_id IS NOT NULL AND target_cohort_id IS NOT NULL
            AND source_cohort_id <> target_cohort_id
            AND child_units::NUMERIC + working_units::NUMERIC + senior_units::NUMERIC >= 1
            AND household_units <= child_units::NUMERIC + working_units::NUMERIC + senior_units::NUMERIC
            AND expected_line_count = 2)
    ),
    CONSTRAINT city_household_movement_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_household_movement_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_household_movement_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_household_movement_source_cohort_fk
        FOREIGN KEY (source_cohort_id, world_id)
        REFERENCES city_household_cohorts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_household_movement_target_cohort_fk
        FOREIGN KEY (target_cohort_id, world_id)
        REFERENCES city_household_cohorts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_household_movements_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_household_movements_source_command_unique UNIQUE (source_command_id),
    CONSTRAINT city_household_movements_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_household_movements_world_cursor
    ON city_household_movements (world_id, tick, sequence);

CREATE TABLE IF NOT EXISTS city_household_movement_lines (
    id BIGSERIAL PRIMARY KEY,
    movement_id BIGINT NOT NULL,
    world_id BIGINT NOT NULL,
    line_no SMALLINT NOT NULL CHECK (line_no > 0),
    cohort_id BIGINT NOT NULL,
    direction VARCHAR(8) NOT NULL,
    demographic_version_before BIGINT NOT NULL CHECK (demographic_version_before >= 0),
    demographic_version_after BIGINT NOT NULL CHECK (demographic_version_after >= 0),
    cohort_version_before BIGINT NOT NULL CHECK (cohort_version_before >= 0),
    cohort_version_after BIGINT NOT NULL CHECK (cohort_version_after > 0),
    occupancy_version_before BIGINT NOT NULL CHECK (occupancy_version_before >= 0),
    occupancy_version_after BIGINT NOT NULL CHECK (occupancy_version_after > 0),
    child_units BIGINT NOT NULL CHECK (child_units >= 0),
    working_units BIGINT NOT NULL CHECK (working_units >= 0),
    senior_units BIGINT NOT NULL CHECK (senior_units >= 0),
    employed_units BIGINT NOT NULL CHECK (employed_units >= 0),
    household_units BIGINT NOT NULL CHECK (household_units > 0),
    occupied_units BIGINT NOT NULL CHECK (occupied_units >= 0),
    child_units_before BIGINT NOT NULL CHECK (child_units_before >= 0),
    working_units_before BIGINT NOT NULL CHECK (working_units_before >= 0),
    senior_units_before BIGINT NOT NULL CHECK (senior_units_before >= 0),
    employed_units_before BIGINT NOT NULL CHECK (employed_units_before >= 0),
    household_units_before BIGINT NOT NULL CHECK (household_units_before > 0),
    occupied_units_before BIGINT NOT NULL CHECK (occupied_units_before >= 0),
    unmet_units_before BIGINT NOT NULL CHECK (unmet_units_before >= 0),
    child_units_after BIGINT NOT NULL CHECK (child_units_after >= 0),
    working_units_after BIGINT NOT NULL CHECK (working_units_after >= 0),
    senior_units_after BIGINT NOT NULL CHECK (senior_units_after >= 0),
    employed_units_after BIGINT NOT NULL CHECK (employed_units_after >= 0),
    household_units_after BIGINT NOT NULL CHECK (household_units_after > 0),
    occupied_units_after BIGINT NOT NULL CHECK (occupied_units_after >= 0),
    unmet_units_after BIGINT NOT NULL CHECK (unmet_units_after >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_household_movement_line_direction_check CHECK (direction IN ('inflow', 'outflow')),
    CONSTRAINT city_household_movement_line_summary_check CHECK (
        employed_units <= working_units AND occupied_units <= household_units
    ),
    CONSTRAINT city_household_movement_line_after_check CHECK (
        employed_units_after <= working_units_after
        AND working_units_after <= child_units_after::NUMERIC + working_units_after::NUMERIC + senior_units_after::NUMERIC
        AND household_units_after <= child_units_after::NUMERIC + working_units_after::NUMERIC + senior_units_after::NUMERIC
        AND occupied_units_after <= household_units_after
        AND occupied_units_after::NUMERIC + unmet_units_after::NUMERIC = household_units_after
    ),
    CONSTRAINT city_household_movement_line_movement_fk
        FOREIGN KEY (movement_id, world_id)
        REFERENCES city_household_movements(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_household_movement_line_cohort_fk
        FOREIGN KEY (cohort_id, world_id)
        REFERENCES city_household_cohorts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_household_movement_lines_line_unique UNIQUE (movement_id, line_no),
    CONSTRAINT city_household_movement_lines_cohort_unique UNIQUE (movement_id, cohort_id)
);

-- Keep the migration re-runnable while permitting the temporary pre-reconciliation
-- state produced by an extreme v3 demographic movement.
ALTER TABLE city_household_movement_lines
    DROP CONSTRAINT IF EXISTS city_household_movement_line_before_check;
ALTER TABLE city_household_movement_lines
    ADD CONSTRAINT city_household_movement_line_before_check CHECK (
        employed_units_before <= working_units_before
        AND working_units_before <= child_units_before::NUMERIC + working_units_before::NUMERIC + senior_units_before::NUMERIC
        AND occupied_units_before <= household_units_before
        AND occupied_units_before::NUMERIC + unmet_units_before::NUMERIC = household_units_before
    );

CREATE OR REPLACE FUNCTION city_f63_household_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_household_movements movement
        WHERE movement.id = CASE
            WHEN COALESCE(current_setting('sub2api.city_f63_household_movement_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.city_f63_household_movement_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND movement.world_id = target_world_id
          AND movement.posted_at IS NULL
    )
$$;

CREATE OR REPLACE FUNCTION city_f63_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_f63_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.current_tick = 0
              AND world.state_hash IS NULL
              AND NOT EXISTS (SELECT 1 FROM city_ticks tick WHERE tick.world_id = world.id)
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
    IF city_f62_migration_write_enabled(NEW.world_id)
       OR city_f63_household_write_enabled(NEW.world_id) THEN
        IF (NEW.birth_remainder, NEW.child_death_remainder,
            NEW.working_death_remainder, NEW.senior_death_remainder,
            NEW.child_aging_remainder, NEW.working_aging_remainder, NEW.metadata)
           IS DISTINCT FROM
           (OLD.birth_remainder, OLD.child_death_remainder,
            OLD.working_death_remainder, OLD.senior_death_remainder,
            OLD.child_aging_remainder, OLD.working_aging_remainder, OLD.metadata)
           OR NEW.version <> OLD.version + 1 THEN
            RAISE EXCEPTION 'invalid city population or household migration projection' USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city demographic cohorts can only change through a draft population or household fact'
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
          AND (NEW.employee_units, NEW.version) IS DISTINCT FROM (OLD.employee_units, OLD.version) THEN
        RAISE EXCEPTION 'city firm employment can only change through labor settlement'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_housing_occupancy_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city housing occupancy cannot be deleted' USING ERRCODE = '55000';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.world_id IS DISTINCT FROM OLD.world_id
       OR NEW.cohort_id IS DISTINCT FROM OLD.cohort_id
       OR NEW.district_id IS DISTINCT FROM OLD.district_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'city housing occupancy identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF city_recovery_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    ELSIF city_engine_upgrade_write_enabled(NEW.world_id)
          OR city_f63_initialization_write_enabled(NEW.world_id) THEN
        IF (NEW.occupied_units, NEW.rent_price_units, NEW.last_settled_tick, NEW.metadata)
           IS DISTINCT FROM
           (OLD.occupied_units, OLD.rent_price_units, OLD.last_settled_tick, OLD.metadata)
           OR NEW.version <> OLD.version + 1 THEN
            RAISE EXCEPTION 'invalid city household occupancy initialization projection'
                USING ERRCODE = '55000';
        END IF;
    ELSIF city_f63_household_write_enabled(NEW.world_id) THEN
        IF (NEW.rent_price_units, NEW.last_settled_tick, NEW.metadata)
           IS DISTINCT FROM
           (OLD.rent_price_units, OLD.last_settled_tick, OLD.metadata)
           OR NEW.version <> OLD.version + 1 THEN
            RAISE EXCEPTION 'invalid city household lifecycle occupancy projection'
                USING ERRCODE = '55000';
        END IF;
    ELSIF city_f4_write_enabled() THEN
        IF NEW.metadata IS DISTINCT FROM OLD.metadata OR NEW.version <> OLD.version + 1 THEN
            RAISE EXCEPTION 'invalid city housing settlement projection' USING ERRCODE = '55000';
        END IF;
    ELSE
        RAISE EXCEPTION 'city housing occupancy can only change through a posted projection'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_household_movement_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city household movements are immutable facts' USING ERRCODE = '55000';
    END IF;
    IF OLD.posted_at IS NOT NULL OR NEW.posted_at IS NULL
       OR (OLD.id, OLD.world_id, OLD.tick, OLD.sequence, OLD.origin,
           OLD.source_command_id, OLD.movement_type, OLD.source_cohort_id,
           OLD.target_cohort_id, OLD.child_units, OLD.working_units,
           OLD.senior_units, OLD.employed_units, OLD.household_units,
           OLD.occupied_units, OLD.expected_line_count, OLD.metadata, OLD.created_at)
          IS DISTINCT FROM
          (NEW.id, NEW.world_id, NEW.tick, NEW.sequence, NEW.origin,
           NEW.source_command_id, NEW.movement_type, NEW.source_cohort_id,
           NEW.target_cohort_id, NEW.child_units, NEW.working_units,
           NEW.senior_units, NEW.employed_units, NEW.household_units,
           NEW.occupied_units, NEW.expected_line_count, NEW.metadata, NEW.created_at) THEN
        RAISE EXCEPTION 'city household movements permit only one draft-to-posted transition'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_household_movement_write_guard ON city_household_movements;
CREATE TRIGGER city_household_movement_write_guard
BEFORE UPDATE OR DELETE ON city_household_movements
FOR EACH ROW EXECUTE FUNCTION guard_city_household_movement_write();

DROP TRIGGER IF EXISTS city_household_movement_line_immutable_guard ON city_household_movement_lines;
CREATE TRIGGER city_household_movement_line_immutable_guard
BEFORE UPDATE OR DELETE ON city_household_movement_lines
FOR EACH ROW EXECUTE FUNCTION reject_city_immutable_fact_mutation();

CREATE OR REPLACE FUNCTION assert_city_household_movement_committed()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    movement_row city_household_movements%ROWTYPE;
    command_type_value VARCHAR(64);
    command_status_value VARCHAR(16);
    command_tick BIGINT;
    expected_command_type VARCHAR(64);
    actual_line_count BIGINT;
    inflow_count BIGINT;
    outflow_count BIGINT;
    source_scope_valid BOOLEAN;
    invalid_line_count BIGINT;
    summary_valid BOOLEAN;
BEGIN
    SELECT * INTO movement_row FROM city_household_movements WHERE id = NEW.id;
    IF movement_row.posted_at IS NULL THEN
        RAISE EXCEPTION 'city household movement must be posted before commit' USING ERRCODE = '23514';
    END IF;

    IF movement_row.origin = 'command' THEN
        SELECT command_type, status, processed_tick
          INTO command_type_value, command_status_value, command_tick
        FROM city_commands
        WHERE id = movement_row.source_command_id AND world_id = movement_row.world_id;
        expected_command_type := CASE
            WHEN movement_row.movement_type = 'income_reclassification' THEN 'household.reclassify'
            ELSE 'household.adjust'
        END;
        IF command_type_value IS DISTINCT FROM expected_command_type
           OR command_status_value IS DISTINCT FROM 'applied'
           OR command_tick IS DISTINCT FROM movement_row.tick THEN
            RAISE EXCEPTION 'city household movement does not match its applied command'
                USING ERRCODE = '23514';
        END IF;
    ELSIF movement_row.source_command_id IS NOT NULL
          OR movement_row.movement_type <> 'dissolution' THEN
        RAISE EXCEPTION 'invalid system household movement origin' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*),
           COUNT(*) FILTER (WHERE direction = 'inflow'),
           COUNT(*) FILTER (WHERE direction = 'outflow')
      INTO actual_line_count, inflow_count, outflow_count
    FROM city_household_movement_lines WHERE movement_id = movement_row.id;
    IF actual_line_count <> movement_row.expected_line_count THEN
        RAISE EXCEPTION 'city household movement line count does not match header'
            USING ERRCODE = '23514';
    END IF;

    IF movement_row.movement_type IN ('formation', 'split') THEN
        source_scope_valid := inflow_count = 1 AND outflow_count = 0 AND EXISTS (
            SELECT 1 FROM city_household_movement_lines line
            WHERE line.movement_id = movement_row.id
              AND line.cohort_id = movement_row.target_cohort_id
              AND line.direction = 'inflow'
        );
    ELSIF movement_row.movement_type IN ('merge', 'dissolution') THEN
        source_scope_valid := inflow_count = 0 AND outflow_count = 1 AND EXISTS (
            SELECT 1 FROM city_household_movement_lines line
            WHERE line.movement_id = movement_row.id
              AND line.cohort_id = movement_row.source_cohort_id
              AND line.direction = 'outflow'
        );
    ELSE
        SELECT source.district_id = target.district_id
               AND source.entity_id = target.entity_id
               AND ABS(
                    CASE source.income_band WHEN 'low' THEN 1 WHEN 'middle' THEN 2 ELSE 3 END
                    - CASE target.income_band WHEN 'low' THEN 1 WHEN 'middle' THEN 2 ELSE 3 END
               ) = 1
          INTO source_scope_valid
        FROM city_household_cohorts source, city_household_cohorts target
        WHERE source.id = movement_row.source_cohort_id
          AND target.id = movement_row.target_cohort_id
          AND source.world_id = movement_row.world_id
          AND target.world_id = movement_row.world_id;
        source_scope_valid := source_scope_valid IS TRUE
            AND inflow_count = 1 AND outflow_count = 1
            AND EXISTS (
                SELECT 1 FROM city_household_movement_lines line
                WHERE line.movement_id = movement_row.id
                  AND line.cohort_id = movement_row.source_cohort_id
                  AND line.direction = 'outflow'
            )
            AND EXISTS (
                SELECT 1 FROM city_household_movement_lines line
                WHERE line.movement_id = movement_row.id
                  AND line.cohort_id = movement_row.target_cohort_id
                  AND line.direction = 'inflow'
            );
    END IF;
    IF source_scope_valid IS DISTINCT FROM TRUE THEN
        RAISE EXCEPTION 'city household movement scope or direction is invalid' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_line_count
    FROM city_household_movement_lines line
    WHERE line.movement_id = movement_row.id
      AND (
        line.cohort_version_after <> line.cohort_version_before + 1
        OR line.occupancy_version_after <> line.occupancy_version_before + 1
        OR (movement_row.movement_type = 'income_reclassification'
            AND line.demographic_version_after <> line.demographic_version_before + 1)
        OR (movement_row.movement_type <> 'income_reclassification'
            AND (line.demographic_version_after <> line.demographic_version_before
                 OR (line.child_units_before, line.working_units_before, line.senior_units_before,
                     line.employed_units_before)
                    IS DISTINCT FROM
                    (line.child_units_after, line.working_units_after, line.senior_units_after,
                     line.employed_units_after)))
        OR (line.direction = 'inflow' AND (
            line.child_units_after::NUMERIC <> line.child_units_before::NUMERIC + line.child_units::NUMERIC
            OR line.working_units_after::NUMERIC <> line.working_units_before::NUMERIC + line.working_units::NUMERIC
            OR line.senior_units_after::NUMERIC <> line.senior_units_before::NUMERIC + line.senior_units::NUMERIC
            OR line.employed_units_after::NUMERIC <> line.employed_units_before::NUMERIC + line.employed_units::NUMERIC
            OR line.household_units_after::NUMERIC <> line.household_units_before::NUMERIC + line.household_units::NUMERIC
            OR line.occupied_units_after::NUMERIC <> line.occupied_units_before::NUMERIC + line.occupied_units::NUMERIC))
        OR (line.direction = 'outflow' AND (
            line.child_units_before::NUMERIC <> line.child_units_after::NUMERIC + line.child_units::NUMERIC
            OR line.working_units_before::NUMERIC <> line.working_units_after::NUMERIC + line.working_units::NUMERIC
            OR line.senior_units_before::NUMERIC <> line.senior_units_after::NUMERIC + line.senior_units::NUMERIC
            OR line.employed_units_before::NUMERIC <> line.employed_units_after::NUMERIC + line.employed_units::NUMERIC
            OR line.household_units_before::NUMERIC <> line.household_units_after::NUMERIC + line.household_units::NUMERIC
            OR line.occupied_units_before::NUMERIC <> line.occupied_units_after::NUMERIC + line.occupied_units::NUMERIC))
      );
    IF invalid_line_count <> 0 THEN
        RAISE EXCEPTION 'city household movement line equation or version is invalid'
            USING ERRCODE = '23514';
    END IF;

    SELECT COALESCE(BOOL_AND(
        line.child_units = movement_row.child_units
        AND line.working_units = movement_row.working_units
        AND line.senior_units = movement_row.senior_units
        AND line.employed_units = movement_row.employed_units
        AND line.household_units = movement_row.household_units
        AND line.occupied_units = movement_row.occupied_units
    ), FALSE)
    INTO summary_valid
    FROM city_household_movement_lines line
    WHERE line.movement_id = movement_row.id;
    IF summary_valid IS DISTINCT FROM TRUE THEN
        RAISE EXCEPTION 'city household movement summary does not match immutable lines'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_household_movement_commit_check ON city_household_movements;
CREATE CONSTRAINT TRIGGER city_household_movement_commit_check
AFTER INSERT OR UPDATE ON city_household_movements
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_household_movement_committed();

CREATE OR REPLACE FUNCTION assert_city_household_projection(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version INTO world_version FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city household projection world does not exist' USING ERRCODE = '23514';
    END IF;

    IF world_version = 'city-f6-v3' THEN
        SELECT COUNT(*) INTO invalid_count
        FROM city_household_cohorts cohort
        LEFT JOIN city_housing_occupancies occupancy
          ON occupancy.world_id = cohort.world_id AND occupancy.cohort_id = cohort.id
        WHERE cohort.world_id = target_world_id
          AND (cohort.household_units > cohort.population_units
               OR cohort.housing_demand_units <> cohort.household_units
               OR occupancy.id IS NULL
               OR occupancy.district_id <> cohort.district_id
               OR occupancy.occupied_units::NUMERIC + occupancy.unmet_units::NUMERIC <> cohort.household_units);
    ELSE
        SELECT COUNT(*) INTO invalid_count
        FROM city_household_cohorts cohort
        LEFT JOIN city_housing_occupancies occupancy
          ON occupancy.world_id = cohort.world_id AND occupancy.cohort_id = cohort.id
        WHERE cohort.world_id = target_world_id
          AND (cohort.housing_demand_units > cohort.population_units
               OR occupancy.id IS NULL
               OR occupancy.district_id <> cohort.district_id
               OR occupancy.occupied_units::NUMERIC + occupancy.unmet_units::NUMERIC <> cohort.housing_demand_units);
    END IF;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city household and housing projections are inconsistent'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_household_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    IF TG_TABLE_NAME = 'city_worlds' THEN
        target_world_id := COALESCE(NEW.id, OLD.id);
    ELSE
        target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    END IF;
    PERFORM assert_city_household_projection(target_world_id);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_household_projection_commit_check ON city_household_cohorts;
CREATE CONSTRAINT TRIGGER city_household_projection_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_household_cohorts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_household_projection();

DROP TRIGGER IF EXISTS city_household_occupancy_commit_check ON city_housing_occupancies;
CREATE CONSTRAINT TRIGGER city_household_occupancy_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_housing_occupancies
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_household_projection();

DROP TRIGGER IF EXISTS city_household_world_commit_check ON city_worlds;
CREATE CONSTRAINT TRIGGER city_household_world_commit_check
AFTER UPDATE ON city_worlds
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_household_projection();

CREATE OR REPLACE FUNCTION initialize_city_f63_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM city_worlds WHERE id = target_world_id FOR UPDATE) THEN
        RAISE EXCEPTION 'city household initialization world does not exist' USING ERRCODE = '23503';
    END IF;
    IF EXISTS (
        SELECT 1 FROM city_household_cohorts cohort
        WHERE cohort.world_id = target_world_id AND cohort.version = 9223372036854775807
    ) OR EXISTS (
        SELECT 1 FROM city_housing_occupancies occupancy
        WHERE occupancy.world_id = target_world_id AND occupancy.version = 9223372036854775807
    ) THEN
        RAISE EXCEPTION 'city household initialization version overflow' USING ERRCODE = '22003';
    END IF;

    PERFORM set_config('sub2api.city_f63_initialize_world_id', target_world_id::TEXT, TRUE);

    WITH desired AS (
        SELECT cohort.id,
               GREATEST(1::BIGINT, occupancy.occupied_units,
                        CEIL(cohort.population_units::NUMERIC / 3)::BIGINT) AS household_units
        FROM city_household_cohorts cohort
        JOIN city_housing_occupancies occupancy
          ON occupancy.world_id = cohort.world_id AND occupancy.cohort_id = cohort.id
        WHERE cohort.world_id = target_world_id
    )
    UPDATE city_household_cohorts cohort
    SET household_units = desired.household_units,
        housing_demand_units = desired.household_units,
        version = cohort.version + 1,
        updated_at = NOW()
    FROM desired
    WHERE cohort.id = desired.id
      AND (cohort.household_units, cohort.housing_demand_units)
          IS DISTINCT FROM (desired.household_units, desired.household_units);

    UPDATE city_housing_occupancies occupancy
    SET unmet_units = cohort.household_units - occupancy.occupied_units,
        version = occupancy.version + 1,
        updated_at = NOW()
    FROM city_household_cohorts cohort
    WHERE occupancy.world_id = target_world_id
      AND cohort.id = occupancy.cohort_id
      AND occupancy.unmet_units IS DISTINCT FROM cohort.household_units - occupancy.occupied_units;

    PERFORM assert_city_household_projection(target_world_id);
END;
$$;

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f6-v3', 'supported', 'city-state-v1+gzip',
        '["control","ledger","resources","calendar_demography","population_migration","household_lifecycle","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f6-v2', 'city-f6-v3', 'f6_v2_to_f6_v3')
ON CONFLICT (from_version, to_version) DO NOTHING;
