-- 城市模拟 F7.4：开发项目、审批、施工、资源投入和建筑调整事实链。

CREATE TABLE IF NOT EXISTS city_development_profiles (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    policy_id VARCHAR(64) NOT NULL,
    policy_version VARCHAR(24) NOT NULL,
    policy_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    project_count BIGINT NOT NULL DEFAULT 0 CHECK (project_count >= 0),
    fact_count BIGINT NOT NULL DEFAULT 0 CHECK (fact_count >= 0),
    adjustment_count BIGINT NOT NULL DEFAULT 0 CHECK (adjustment_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_development_profile_policy_check CHECK (
        policy_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND policy_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND policy_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_development_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_development_projects (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(96) NOT NULL,
    name VARCHAR(128) NOT NULL,
    project_type VARCHAR(32) NOT NULL CHECK (project_type IN ('vertical_expansion', 'renovation')),
    district_id BIGINT NOT NULL,
    parcel_id BIGINT NOT NULL,
    building_id BIGINT NOT NULL,
    developer_entity_id BIGINT NOT NULL,
    target_floor_count SMALLINT,
    target_quality_milli INTEGER,
    added_floor_count SMALLINT NOT NULL DEFAULT 0 CHECK (added_floor_count >= 0),
    added_floor_area_sqm BIGINT NOT NULL DEFAULT 0 CHECK (added_floor_area_sqm >= 0),
    added_capacity_units BIGINT NOT NULL DEFAULT 0 CHECK (added_capacity_units >= 0),
    quality_delta_milli INTEGER NOT NULL DEFAULT 0 CHECK (quality_delta_milli >= 0),
    required_basic_material_units BIGINT NOT NULL CHECK (required_basic_material_units > 0),
    required_capital_goods_units BIGINT NOT NULL CHECK (required_capital_goods_units > 0),
    required_labor_units BIGINT NOT NULL CHECK (required_labor_units > 0),
    planned_duration_ticks BIGINT NOT NULL CHECK (planned_duration_ticks BETWEEN 1 AND 720),
    status VARCHAR(24) NOT NULL CHECK (
        status IN ('submitted', 'approved', 'rejected', 'under_construction', 'completed', 'cancelled')
    ),
    progress_milli INTEGER NOT NULL DEFAULT 0 CHECK (progress_milli BETWEEN 0 AND 1000),
    submitted_tick BIGINT NOT NULL CHECK (submitted_tick > 0),
    reviewed_tick BIGINT,
    started_tick BIGINT,
    planned_completion_tick BIGINT,
    completed_tick BIGINT,
    cancelled_tick BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_development_project_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_development_project_parcel_fk
        FOREIGN KEY (parcel_id, world_id)
        REFERENCES city_parcels(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_development_project_building_fk
        FOREIGN KEY (building_id, world_id)
        REFERENCES city_buildings(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_development_project_developer_fk
        FOREIGN KEY (developer_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_development_project_code_check CHECK (code ~ '^development_[1-9][0-9]*$'),
    CONSTRAINT city_development_project_name_check CHECK (
        char_length(btrim(name)) BETWEEN 1 AND 128 AND name = btrim(name)
    ),
    CONSTRAINT city_development_project_shape_check CHECK (
        (project_type = 'vertical_expansion'
         AND target_floor_count IS NOT NULL AND target_floor_count > 0
         AND target_quality_milli IS NULL
         AND added_floor_count > 0 AND added_floor_area_sqm > 0
         AND added_capacity_units > 0 AND quality_delta_milli = 0)
        OR
        (project_type = 'renovation'
         AND target_floor_count IS NULL
         AND target_quality_milli BETWEEN 1 AND 1500
         AND added_floor_count = 0 AND added_floor_area_sqm = 0
         AND added_capacity_units = 0 AND quality_delta_milli > 0)
    ),
    CONSTRAINT city_development_project_tick_order_check CHECK (
        (reviewed_tick IS NULL OR reviewed_tick >= submitted_tick)
        AND (started_tick IS NULL OR (reviewed_tick IS NOT NULL AND started_tick >= reviewed_tick))
        AND (planned_completion_tick IS NULL OR (
            started_tick IS NOT NULL AND planned_completion_tick = started_tick + planned_duration_ticks
        ))
        AND (completed_tick IS NULL OR (
            started_tick IS NOT NULL AND planned_completion_tick IS NOT NULL
            AND completed_tick = planned_completion_tick
        ))
        AND (cancelled_tick IS NULL OR cancelled_tick >= submitted_tick)
    ),
    CONSTRAINT city_development_project_status_shape_check CHECK (
        (status = 'submitted' AND reviewed_tick IS NULL AND started_tick IS NULL
         AND completed_tick IS NULL AND cancelled_tick IS NULL AND progress_milli = 0)
        OR (status IN ('approved', 'rejected') AND reviewed_tick IS NOT NULL
         AND started_tick IS NULL AND completed_tick IS NULL AND cancelled_tick IS NULL
         AND progress_milli = 0)
        OR (status = 'under_construction' AND reviewed_tick IS NOT NULL AND started_tick IS NOT NULL
         AND planned_completion_tick IS NOT NULL AND completed_tick IS NULL
         AND cancelled_tick IS NULL AND progress_milli BETWEEN 0 AND 999)
        OR (status = 'completed' AND reviewed_tick IS NOT NULL AND started_tick IS NOT NULL
         AND planned_completion_tick IS NOT NULL AND completed_tick IS NOT NULL
         AND cancelled_tick IS NULL AND progress_milli = 1000)
        OR (status = 'cancelled' AND completed_tick IS NULL AND cancelled_tick IS NOT NULL
         AND progress_milli BETWEEN 0 AND 999)
    ),
    CONSTRAINT city_development_project_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_development_projects_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_development_projects_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_development_projects_one_active_building
    ON city_development_projects (world_id, building_id)
    WHERE status IN ('submitted', 'approved', 'under_construction');
CREATE INDEX IF NOT EXISTS idx_city_development_projects_world_status
    ON city_development_projects (world_id, status, submitted_tick DESC, code);
CREATE INDEX IF NOT EXISTS idx_city_development_projects_developer_active
    ON city_development_projects (world_id, developer_entity_id, status, code);

CREATE TABLE IF NOT EXISTS city_development_facts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    project_code VARCHAR(96) NOT NULL,
    source_command_id BIGINT,
    fact_type VARCHAR(24) NOT NULL CHECK (
        fact_type IN ('submitted', 'approved', 'rejected', 'started', 'progressed', 'completed', 'cancelled')
    ),
    from_status VARCHAR(24),
    to_status VARCHAR(24) NOT NULL,
    progress_before_milli INTEGER NOT NULL CHECK (progress_before_milli BETWEEN 0 AND 1000),
    progress_after_milli INTEGER NOT NULL CHECK (progress_after_milli BETWEEN 0 AND 1000),
    project_version_before BIGINT NOT NULL CHECK (project_version_before >= 0),
    project_version_after BIGINT NOT NULL CHECK (project_version_after > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted_at TIMESTAMPTZ,
    CONSTRAINT city_development_fact_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_development_fact_project_fk
        FOREIGN KEY (world_id, project_code)
        REFERENCES city_development_projects(world_id, code) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_development_fact_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_development_fact_transition_check CHECK (
        project_version_after = project_version_before + 1
        AND progress_after_milli >= progress_before_milli
        AND (
            (fact_type = 'submitted' AND from_status IS NULL AND to_status = 'submitted'
             AND progress_before_milli = 0 AND progress_after_milli = 0
             AND project_version_before = 0 AND project_version_after = 1)
            OR (fact_type = 'approved' AND from_status = 'submitted' AND to_status = 'approved'
             AND progress_before_milli = 0 AND progress_after_milli = 0)
            OR (fact_type = 'rejected' AND from_status = 'submitted' AND to_status = 'rejected'
             AND progress_before_milli = 0 AND progress_after_milli = 0)
            OR (fact_type = 'started' AND from_status = 'approved' AND to_status = 'under_construction'
             AND progress_before_milli = 0 AND progress_after_milli = 0)
            OR (fact_type = 'progressed' AND from_status = 'under_construction'
             AND to_status = 'under_construction' AND progress_after_milli < 1000
             AND progress_after_milli > progress_before_milli)
            OR (fact_type = 'completed' AND from_status = 'under_construction'
             AND to_status = 'completed' AND progress_after_milli = 1000
             AND progress_before_milli < 1000)
            OR (fact_type = 'cancelled' AND from_status IN ('submitted', 'approved', 'under_construction')
             AND to_status = 'cancelled' AND progress_after_milli = progress_before_milli)
        )
    ),
    CONSTRAINT city_development_fact_origin_check CHECK (
        (fact_type IN ('progressed', 'completed') AND source_command_id IS NULL)
        OR (fact_type NOT IN ('progressed', 'completed') AND source_command_id IS NOT NULL)
    ),
    CONSTRAINT city_development_fact_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_development_fact_posted_check CHECK (posted_at IS NULL OR posted_at >= created_at),
    CONSTRAINT city_development_facts_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_development_facts_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_development_facts_one_per_command
    ON city_development_facts (source_command_id) WHERE source_command_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_city_development_facts_project_history
    ON city_development_facts (world_id, project_code, project_version_after);

CREATE TABLE IF NOT EXISTS city_building_adjustments (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    project_code VARCHAR(96) NOT NULL,
    building_id BIGINT NOT NULL,
    district_id BIGINT NOT NULL,
    completion_fact_id BIGINT NOT NULL,
    added_floor_count SMALLINT NOT NULL DEFAULT 0 CHECK (added_floor_count >= 0),
    added_top_z SMALLINT NOT NULL DEFAULT 0 CHECK (added_top_z >= 0),
    added_floor_area_sqm BIGINT NOT NULL DEFAULT 0 CHECK (added_floor_area_sqm >= 0),
    added_capacity_units BIGINT NOT NULL DEFAULT 0 CHECK (added_capacity_units >= 0),
    quality_delta_milli INTEGER NOT NULL DEFAULT 0 CHECK (quality_delta_milli >= 0),
    completed_tick BIGINT NOT NULL CHECK (completed_tick > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    posted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_building_adjustment_project_fk
        FOREIGN KEY (world_id, project_code)
        REFERENCES city_development_projects(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_building_adjustment_building_fk
        FOREIGN KEY (building_id, world_id)
        REFERENCES city_buildings(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_building_adjustment_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_building_adjustment_fact_fk
        FOREIGN KEY (completion_fact_id, world_id)
        REFERENCES city_development_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_building_adjustment_effect_check CHECK (
        added_floor_count > 0 OR added_floor_area_sqm > 0
        OR added_capacity_units > 0 OR quality_delta_milli > 0
    ),
    CONSTRAINT city_building_adjustment_floor_shape_check CHECK (
        added_top_z = added_floor_count
        AND ((added_floor_count = 0 AND added_floor_area_sqm = 0 AND added_capacity_units = 0)
             OR (added_floor_count > 0 AND added_floor_area_sqm > 0 AND added_capacity_units > 0))
    ),
    CONSTRAINT city_building_adjustment_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_building_adjustments_world_project_unique UNIQUE (world_id, project_code),
    CONSTRAINT city_building_adjustments_fact_unique UNIQUE (completion_fact_id),
    CONSTRAINT city_building_adjustments_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_building_adjustments_world_building
    ON city_building_adjustments (world_id, building_id, project_code);

CREATE TABLE IF NOT EXISTS city_development_baselines (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick >= 0),
    policy_hash VARCHAR(64) NOT NULL CHECK (policy_hash ~ '^[0-9a-f]{64}$'),
    baseline_hash VARCHAR(64) NOT NULL CHECK (baseline_hash ~ '^[0-9a-f]{64}$'),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    posted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_development_baseline_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE OR REPLACE FUNCTION city_development_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_development_facts fact
        WHERE fact.id = CASE
            WHEN COALESCE(current_setting('sub2api.city_development_fact_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.city_development_fact_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND fact.world_id = target_world_id
          AND fact.posted_at IS NULL
    )
$$;

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
    IF world_version IS DISTINCT FROM 'city-f7-v3' OR NEW.tick IS DISTINCT FROM world_tick + 1 THEN
        RAISE EXCEPTION 'city development fact must target the next F7.4 tick'
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

DROP TRIGGER IF EXISTS city_development_fact_insert_guard ON city_development_facts;
CREATE TRIGGER city_development_fact_insert_guard
BEFORE INSERT ON city_development_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_development_fact_insert();

CREATE OR REPLACE FUNCTION guard_city_development_fact_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(OLD.world_id) THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'city development facts are immutable' USING ERRCODE = '55000';
    END IF;
    IF city_recovery_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    IF OLD.posted_at IS NOT NULL OR NEW.posted_at IS NULL
       OR (OLD.id, OLD.world_id, OLD.tick, OLD.sequence, OLD.project_code,
           OLD.source_command_id, OLD.fact_type, OLD.from_status, OLD.to_status,
           OLD.progress_before_milli, OLD.progress_after_milli,
           OLD.project_version_before, OLD.project_version_after,
           OLD.metadata, OLD.created_at)
          IS DISTINCT FROM
          (NEW.id, NEW.world_id, NEW.tick, NEW.sequence, NEW.project_code,
           NEW.source_command_id, NEW.fact_type, NEW.from_status, NEW.to_status,
           NEW.progress_before_milli, NEW.progress_after_milli,
           NEW.project_version_before, NEW.project_version_after,
           NEW.metadata, NEW.created_at) THEN
        RAISE EXCEPTION 'city development facts permit only one draft-to-posted transition'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_development_fact_immutable_guard ON city_development_facts;
CREATE TRIGGER city_development_fact_immutable_guard
BEFORE UPDATE OR DELETE ON city_development_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_development_fact_immutable();

CREATE OR REPLACE FUNCTION guard_city_development_project_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(OLD.world_id) THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'city development projects cannot be deleted' USING ERRCODE = '55000';
    END IF;
    IF city_recovery_write_enabled(CASE WHEN TG_OP = 'INSERT' THEN NEW.world_id ELSE OLD.world_id END) THEN
        RETURN NEW;
    END IF;
    IF NOT city_development_fact_write_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city development project writes require a matching draft fact'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NOT EXISTS (
            SELECT 1 FROM city_development_facts fact
            WHERE fact.id = current_setting('sub2api.city_development_fact_id', TRUE)::BIGINT
              AND fact.world_id = NEW.world_id AND fact.project_code = NEW.code
              AND fact.fact_type = 'submitted' AND fact.project_version_after = NEW.version
              AND fact.posted_at IS NULL
        ) THEN
            RAISE EXCEPTION 'city development project insert does not match submitted fact'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF (NEW.id, NEW.world_id, NEW.code, NEW.name, NEW.project_type,
        NEW.district_id, NEW.parcel_id, NEW.building_id, NEW.developer_entity_id,
        NEW.target_floor_count, NEW.target_quality_milli,
        NEW.added_floor_count, NEW.added_floor_area_sqm, NEW.added_capacity_units,
        NEW.quality_delta_milli, NEW.required_basic_material_units,
        NEW.required_capital_goods_units, NEW.required_labor_units,
        NEW.planned_duration_ticks, NEW.submitted_tick, NEW.created_at, NEW.metadata)
       IS DISTINCT FROM
       (OLD.id, OLD.world_id, OLD.code, OLD.name, OLD.project_type,
        OLD.district_id, OLD.parcel_id, OLD.building_id, OLD.developer_entity_id,
        OLD.target_floor_count, OLD.target_quality_milli,
        OLD.added_floor_count, OLD.added_floor_area_sqm, OLD.added_capacity_units,
        OLD.quality_delta_milli, OLD.required_basic_material_units,
        OLD.required_capital_goods_units, OLD.required_labor_units,
        OLD.planned_duration_ticks, OLD.submitted_tick, OLD.created_at, OLD.metadata)
       OR NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'city development project identity or version is immutable'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_development_project_projection_guard ON city_development_projects;
CREATE TRIGGER city_development_project_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_development_projects
FOR EACH ROW EXECUTE FUNCTION guard_city_development_project_projection();

CREATE OR REPLACE FUNCTION guard_city_development_profile_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := CASE WHEN TG_OP = 'DELETE' THEN OLD.world_id ELSE NEW.world_id END;
    IF TG_OP = 'INSERT' THEN
        IF city_f7_initialization_write_enabled(target_world_id)
           OR city_engine_upgrade_write_enabled(target_world_id)
           OR city_recovery_write_enabled(target_world_id) THEN
            RETURN NEW;
        END IF;
    ELSIF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(target_world_id) THEN
            RETURN OLD;
        END IF;
    ELSIF city_recovery_write_enabled(target_world_id) THEN
        RETURN NEW;
    ELSIF city_development_fact_write_enabled(target_world_id)
          AND NEW.world_id IS NOT DISTINCT FROM OLD.world_id
          AND NEW.policy_id IS NOT DISTINCT FROM OLD.policy_id
          AND NEW.policy_version IS NOT DISTINCT FROM OLD.policy_version
          AND NEW.policy_hash IS NOT DISTINCT FROM OLD.policy_hash
          AND NEW.baseline_tick IS NOT DISTINCT FROM OLD.baseline_tick
          AND NEW.metadata IS NOT DISTINCT FROM OLD.metadata
          AND NEW.created_at IS NOT DISTINCT FROM OLD.created_at
          AND NEW.revision = OLD.revision + 1 THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city development profile write is not authorized' USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_development_profile_projection_guard ON city_development_profiles;
CREATE TRIGGER city_development_profile_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_development_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_development_profile_projection();

CREATE OR REPLACE FUNCTION guard_city_building_adjustment_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(OLD.world_id) THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'city building adjustments are immutable' USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF city_recovery_write_enabled(NEW.world_id) THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'city building adjustments cannot be updated' USING ERRCODE = '55000';
    END IF;
    IF city_recovery_write_enabled(NEW.world_id) OR (
        city_development_fact_write_enabled(NEW.world_id)
        AND EXISTS (
            SELECT 1 FROM city_development_facts fact
            WHERE fact.id = NEW.completion_fact_id AND fact.world_id = NEW.world_id
              AND fact.project_code = NEW.project_code AND fact.fact_type = 'completed'
              AND fact.posted_at IS NULL
        )
    ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city building adjustment requires a draft completion fact'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_building_adjustment_immutable_guard ON city_building_adjustments;
CREATE TRIGGER city_building_adjustment_immutable_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_building_adjustments
FOR EACH ROW EXECUTE FUNCTION guard_city_building_adjustment_immutable();

CREATE OR REPLACE FUNCTION guard_city_development_baseline_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := CASE WHEN TG_OP = 'DELETE' THEN OLD.world_id ELSE NEW.world_id END;
    IF TG_OP = 'INSERT' AND (
        city_f7_initialization_write_enabled(target_world_id)
        OR city_engine_upgrade_write_enabled(target_world_id)
        OR city_recovery_write_enabled(target_world_id)
    ) THEN
        RETURN NEW;
    END IF;
    IF city_recovery_write_enabled(target_world_id) THEN
        RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
    END IF;
    RAISE EXCEPTION 'city development baseline is immutable outside initialization or recovery'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_development_baseline_immutable_guard ON city_development_baselines;
CREATE TRIGGER city_development_baseline_immutable_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_development_baselines
FOR EACH ROW EXECUTE FUNCTION guard_city_development_baseline_immutable();

-- Development starts are represented by two ordinary, conserved consumption
-- operations. The exceptional NULL source command remains bound to a draft
-- started fact and cannot be used as a general resource-write bypass.
ALTER TABLE city_resource_operations
    DROP CONSTRAINT IF EXISTS city_resource_operation_origin_check;
ALTER TABLE city_resource_operations
    ADD CONSTRAINT city_resource_operation_origin_check CHECK (
        (operation_type = 'opening'
            AND source_command_id IS NULL AND market_settlement_id IS NULL)
        OR (operation_type <> 'opening'
            AND ((source_command_id IS NOT NULL)::INTEGER
               + (market_settlement_id IS NOT NULL)::INTEGER) = 1)
        OR (operation_type = 'consumption' AND source_command_id IS NULL
            AND market_settlement_id IS NULL
            AND metadata ? 'development_fact_id'
            AND metadata ? 'development_project_code')
    );

CREATE OR REPLACE FUNCTION guard_city_development_resource_operation_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    fact_id_value BIGINT;
    fact_row city_development_facts%ROWTYPE;
    project_row city_development_projects%ROWTYPE;
BEGIN
    IF NEW.operation_type <> 'consumption' OR NEW.source_command_id IS NOT NULL
       OR NEW.market_settlement_id IS NOT NULL THEN
        RETURN NEW;
    END IF;
    IF COALESCE(NEW.metadata ->> 'development_fact_id', '') !~ '^[1-9][0-9]*$' THEN
        RAISE EXCEPTION 'development resource operation is missing its draft fact'
            USING ERRCODE = '23514';
    END IF;
    fact_id_value := (NEW.metadata ->> 'development_fact_id')::BIGINT;
    SELECT * INTO fact_row FROM city_development_facts WHERE id = fact_id_value;
    SELECT * INTO project_row FROM city_development_projects
    WHERE world_id = NEW.world_id AND code = NEW.metadata ->> 'development_project_code';
    IF NOT FOUND OR fact_row.id IS NULL
       OR fact_row.id::TEXT IS DISTINCT FROM current_setting('sub2api.city_development_fact_id', TRUE)
       OR fact_row.world_id <> NEW.world_id OR fact_row.tick <> NEW.tick
       OR fact_row.project_code <> project_row.code OR fact_row.fact_type <> 'started'
       OR fact_row.posted_at IS NOT NULL OR project_row.status <> 'approved'
       OR NEW.actor_entity_id <> project_row.developer_entity_id
       OR NEW.district_id <> project_row.district_id THEN
        RAISE EXCEPTION 'development resource operation does not match a draft started fact'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_development_resource_operation_insert_guard ON city_resource_operations;
CREATE TRIGGER city_development_resource_operation_insert_guard
BEFORE INSERT ON city_resource_operations
FOR EACH ROW EXECUTE FUNCTION guard_city_development_resource_operation_insert();

CREATE OR REPLACE FUNCTION assert_city_development_fact_committed()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM city_development_facts fact
        WHERE fact.id = NEW.id AND fact.posted_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'city development fact must be posted before commit' USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_development_fact_commit_check ON city_development_facts;
CREATE CONSTRAINT TRIGGER city_development_fact_commit_check
AFTER INSERT OR UPDATE ON city_development_facts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_development_fact_committed();

CREATE OR REPLACE FUNCTION initialize_city_f74_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    target_tick BIGINT;
BEGIN
    SELECT current_tick INTO target_tick FROM city_worlds WHERE id = target_world_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city F7.4 world does not exist' USING ERRCODE = '23514';
    END IF;
    INSERT INTO city_development_profiles
        (world_id, policy_id, policy_version, policy_hash, baseline_tick,
         project_count, fact_count, adjustment_count, revision, metadata)
    VALUES
        (target_world_id, 'sub2api-development', '1.0.0',
         'b1bbc919b39020a5bc4760fb0ee80468d286a4d74b97d4bbae8f8601c5bb9f3f',
         target_tick, 0, 0, 0, 1, '{}'::jsonb)
    ON CONFLICT (world_id) DO NOTHING;

    INSERT INTO city_development_baselines
        (world_id, tick, policy_hash, baseline_hash, metadata, posted_at)
    VALUES
        (target_world_id, target_tick,
         'b1bbc919b39020a5bc4760fb0ee80468d286a4d74b97d4bbae8f8601c5bb9f3f',
         'fcb3ae78e18e4b3adb2db1cd9535403f61f28a04fee5eb13ac6ad284ca89459c',
         '{}'::jsonb, NOW())
    ON CONFLICT (world_id) DO NOTHING;
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
    IF world_version <> 'city-f7-v3' THEN
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

CREATE OR REPLACE FUNCTION check_city_development_foundation()
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
    PERFORM assert_city_development_foundation(target_world_id);
    RETURN NULL;
END;
$$;

DO $$
DECLARE
    table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_development_profiles', 'city_development_projects',
        'city_development_facts', 'city_building_adjustments',
        'city_development_baselines'
    ] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', table_name || '_commit_check', table_name);
        EXECUTE format(
            'CREATE CONSTRAINT TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I '
            || 'DEFERRABLE INITIALLY DEFERRED FOR EACH ROW '
            || 'EXECUTE FUNCTION check_city_development_foundation()',
            table_name || '_commit_check', table_name
        );
    END LOOP;
END;
$$;

DROP TRIGGER IF EXISTS city_development_world_commit_check ON city_worlds;
CREATE CONSTRAINT TRIGGER city_development_world_commit_check
AFTER INSERT OR UPDATE ON city_worlds
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_development_foundation();

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f7-v3', 'supported', 'city-state-v1+gzip',
        '["control","ledger","resources","calendar_demography","population_migration","household_lifecycle","spatial","land","development","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f7-v2', 'city-f7-v3', 'f7_v2_to_f7_v3')
ON CONFLICT (from_version, to_version) DO NOTHING;

-- F7.4 keeps the F7.3 baseline immutable while district aggregates track the
-- effective baseline + posted adjustments. Replace the F7.3 assertion with a
-- version-aware form; every other F7.3 invariant remains unchanged.
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

    IF world_version NOT IN ('city-f7-v2', 'city-f7-v3') THEN
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

-- F7.4 still consumes the frozen F7.1 map-generation domain.
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
       OR world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3')
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
    IF world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3') THEN
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
