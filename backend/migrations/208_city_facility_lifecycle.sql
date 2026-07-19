-- F8.1 facility lifecycle, staffing, incident, budget, and resource-conservation foundation.

CREATE TABLE IF NOT EXISTS city_facility_lifecycle_profiles (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    policy_id VARCHAR(64) NOT NULL,
    policy_version VARCHAR(24) NOT NULL,
    policy_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    policy_count BIGINT NOT NULL CHECK (policy_count >= 0),
    state_count BIGINT NOT NULL DEFAULT 0 CHECK (state_count BETWEEN 0 AND 10000),
    operation_count BIGINT NOT NULL DEFAULT 0 CHECK (operation_count BETWEEN 0 AND 100000),
    staffing_count BIGINT NOT NULL DEFAULT 0 CHECK (staffing_count BETWEEN 0 AND 100000),
    incident_count BIGINT NOT NULL DEFAULT 0 CHECK (incident_count BETWEEN 0 AND 100000),
    fact_count BIGINT NOT NULL DEFAULT 0 CHECK (fact_count >= 0),
    budget_movement_count BIGINT NOT NULL DEFAULT 0 CHECK (budget_movement_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_facility_lifecycle_profile_identity_check CHECK (
        policy_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND policy_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND policy_hash ~ '^[0-9a-f]{64}$'
    )
);

CREATE TABLE IF NOT EXISTS city_facility_lifecycle_policies (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    facility_type_id BIGINT NOT NULL,
    policy_version VARCHAR(24) NOT NULL,
    policy_hash VARCHAR(64) NOT NULL,
    maintenance_interval_ticks BIGINT NOT NULL CHECK (maintenance_interval_ticks > 0),
    base_decay_milli INTEGER NOT NULL CHECK (base_decay_milli BETWEEN 0 AND 1000),
    utilization_decay_milli INTEGER NOT NULL CHECK (utilization_decay_milli BETWEEN 0 AND 1000),
    overdue_decay_milli INTEGER NOT NULL CHECK (overdue_decay_milli BETWEEN 0 AND 1000),
    failure_threshold_milli INTEGER NOT NULL CHECK (failure_threshold_milli BETWEEN 0 AND 1000),
    base_failure_ppm INTEGER NOT NULL CHECK (base_failure_ppm BETWEEN 0 AND 1000000),
    condition_failure_ppm INTEGER NOT NULL CHECK (condition_failure_ppm BETWEEN 0 AND 1000000),
    capacity_units_per_staff BIGINT NOT NULL CHECK (capacity_units_per_staff > 0),
    maintenance_restore_milli INTEGER NOT NULL CHECK (maintenance_restore_milli BETWEEN 0 AND 1000),
    repair_restore_milli INTEGER NOT NULL CHECK (repair_restore_milli BETWEEN 0 AND 1000),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_facility_lifecycle_policy_type_fk
        FOREIGN KEY (facility_type_id, world_id)
        REFERENCES city_facility_type_definitions(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_lifecycle_policy_identity_check CHECK (
        policy_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND policy_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_facility_lifecycle_policies_type_unique UNIQUE (world_id, facility_type_id),
    CONSTRAINT city_facility_lifecycle_policies_id_world_unique UNIQUE (id, world_id)
);

CREATE TABLE IF NOT EXISTS city_facility_lifecycle_facts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    phase VARCHAR(16) NOT NULL CHECK (phase IN ('command', 'pre_service', 'post_service')),
    source_command_id BIGINT,
    fact_type VARCHAR(48) NOT NULL CHECK (fact_type IN (
        'facility.initialized', 'capacity.changed',
        'operation.scheduled', 'operation.started', 'operation.cancelled',
        'operation.progressed', 'operation.completed', 'staffing.configured',
        'condition.changed', 'incident.opened', 'incident.resolved'
    )),
    subject_kind VARCHAR(16) NOT NULL CHECK (subject_kind IN (
        'operation', 'staffing', 'facility', 'incident'
    )),
    subject_code VARCHAR(192) NOT NULL CHECK (
        subject_code ~ '^[a-z][a-z0-9_.-]{1,191}$'
    ),
    version_before BIGINT NOT NULL CHECK (version_before >= 0),
    version_after BIGINT NOT NULL CHECK (version_after = version_before + 1),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted_at TIMESTAMPTZ,
    CONSTRAINT city_facility_lifecycle_fact_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_facility_lifecycle_fact_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_facility_lifecycle_fact_origin_check CHECK (
        (phase = 'command' AND source_command_id IS NOT NULL
         AND fact_type IN ('facility.initialized', 'capacity.changed',
                           'operation.scheduled', 'operation.started',
                           'operation.cancelled', 'staffing.configured'))
        OR (phase = 'pre_service' AND source_command_id IS NULL
            AND fact_type IN ('operation.progressed', 'operation.completed', 'incident.resolved'))
        OR (phase = 'post_service' AND source_command_id IS NULL
            AND fact_type IN ('condition.changed', 'incident.opened'))
    ),
    CONSTRAINT city_facility_lifecycle_fact_posted_check
        CHECK (posted_at IS NULL OR posted_at >= created_at),
    CONSTRAINT city_facility_lifecycle_facts_tick_sequence_unique
        UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_facility_lifecycle_facts_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_facility_lifecycle_facts_one_per_command
    ON city_facility_lifecycle_facts (source_command_id)
    WHERE source_command_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_city_facility_lifecycle_facts_subject
    ON city_facility_lifecycle_facts (world_id, subject_kind, subject_code, tick, sequence);

CREATE TABLE IF NOT EXISTS city_facility_lifecycle_states (
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    facility_id BIGINT NOT NULL,
    policy_id BIGINT NOT NULL,
    lifecycle_status VARCHAR(24) NOT NULL CHECK (lifecycle_status IN (
        'uncommissioned', 'operational', 'maintenance', 'failed',
        'decommissioning', 'retired'
    )),
    condition_milli INTEGER NOT NULL CHECK (condition_milli BETWEEN 0 AND 1000),
    staff_required_units BIGINT NOT NULL CHECK (staff_required_units >= 0),
    staff_assigned_units BIGINT NOT NULL CHECK (staff_assigned_units >= 0),
    staffing_factor_milli INTEGER NOT NULL CHECK (staffing_factor_milli BETWEEN 0 AND 1000),
    operation_factor_milli INTEGER NOT NULL CHECK (operation_factor_milli BETWEEN 0 AND 1000),
    effective_factor_milli INTEGER NOT NULL CHECK (effective_factor_milli BETWEEN 0 AND 1000),
    last_maintenance_tick BIGINT CHECK (last_maintenance_tick >= 0),
    maintenance_due_tick BIGINT NOT NULL CHECK (maintenance_due_tick >= 0),
    active_operation_code VARCHAR(96),
    open_incident_code VARCHAR(96),
    failure_count BIGINT NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= 0),
    version BIGINT NOT NULL CHECK (version > 0),
    source_fact_id BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, facility_id),
    CONSTRAINT city_facility_lifecycle_state_facility_fk
        FOREIGN KEY (facility_id, world_id)
        REFERENCES city_facilities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_lifecycle_state_policy_fk
        FOREIGN KEY (policy_id, world_id)
        REFERENCES city_facility_lifecycle_policies(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_lifecycle_state_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_facility_lifecycle_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_facility_lifecycle_state_factor_check CHECK (
        staffing_factor_milli = CASE
            WHEN staff_required_units = 0 THEN 1000
            ELSE LEAST(1000, FLOOR(staff_assigned_units::NUMERIC * 1000 / staff_required_units)::INTEGER)
        END
        AND effective_factor_milli = CASE
            WHEN lifecycle_status <> 'operational' THEN 0
            ELSE LEAST(condition_milli, staffing_factor_milli, operation_factor_milli)
        END
    ),
    CONSTRAINT city_facility_lifecycle_state_codes_check CHECK (
        (active_operation_code IS NULL OR active_operation_code ~ '^[a-z][a-z0-9_.-]{1,95}$')
        AND (open_incident_code IS NULL OR open_incident_code ~ '^[a-z][a-z0-9_.-]{1,95}$')
    )
);

CREATE TABLE IF NOT EXISTS city_facility_operations (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(96) NOT NULL CHECK (code ~ '^[a-z][a-z0-9_.-]{1,95}$'),
    facility_id BIGINT NOT NULL,
    operation_type VARCHAR(24) NOT NULL CHECK (operation_type IN (
        'commission', 'maintenance', 'repair', 'decommission'
    )),
    status VARCHAR(16) NOT NULL CHECK (status IN (
        'planned', 'active', 'completed', 'cancelled'
    )),
    sponsor_entity_id BIGINT NOT NULL,
    executor_entity_id BIGINT NOT NULL,
    budget_line_id BIGINT,
    planned_start_tick BIGINT NOT NULL CHECK (planned_start_tick > 0),
    started_tick BIGINT,
    completed_tick BIGINT,
    duration_ticks BIGINT NOT NULL CHECK (duration_ticks > 0),
    progress_milli INTEGER NOT NULL CHECK (progress_milli BETWEEN 0 AND 1000),
    required_basic_material_units BIGINT NOT NULL CHECK (required_basic_material_units >= 0),
    required_capital_goods_units BIGINT NOT NULL CHECK (required_capital_goods_units >= 0),
    required_labor_units BIGINT NOT NULL CHECK (required_labor_units > 0),
    budget_units BIGINT NOT NULL CHECK (budget_units > 0),
    budget_committed_units BIGINT NOT NULL CHECK (budget_committed_units >= 0),
    budget_spent_units BIGINT NOT NULL CHECK (budget_spent_units >= 0),
    created_tick BIGINT NOT NULL CHECK (created_tick > 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= created_tick),
    version BIGINT NOT NULL CHECK (version > 0),
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_facility_operation_facility_fk
        FOREIGN KEY (facility_id, world_id)
        REFERENCES city_facilities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_operation_sponsor_fk
        FOREIGN KEY (sponsor_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_operation_executor_fk
        FOREIGN KEY (executor_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_operation_budget_fk
        FOREIGN KEY (budget_line_id, world_id)
        REFERENCES city_government_budget_lines(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_operation_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_facility_lifecycle_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_facility_operation_budget_shape_check CHECK (
        budget_committed_units + budget_spent_units <= budget_units
    ),
    CONSTRAINT city_facility_operation_tick_shape_check CHECK (
        (status = 'planned' AND started_tick IS NULL AND completed_tick IS NULL
         AND progress_milli = 0 AND budget_spent_units = 0)
        OR (status = 'active' AND started_tick IS NOT NULL AND completed_tick IS NULL
            AND progress_milli < 1000 AND budget_committed_units = 0
            AND budget_spent_units = budget_units)
        OR (status = 'completed' AND started_tick IS NOT NULL AND completed_tick IS NOT NULL
            AND completed_tick >= started_tick AND progress_milli = 1000
            AND budget_committed_units = 0 AND budget_spent_units = budget_units)
        OR (status = 'cancelled' AND started_tick IS NULL AND completed_tick IS NULL
            AND progress_milli = 0 AND budget_committed_units = 0 AND budget_spent_units = 0)
    ),
    CONSTRAINT city_facility_operations_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_facility_operations_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_facility_operations_one_open
    ON city_facility_operations (world_id, facility_id)
    WHERE status IN ('planned', 'active');
CREATE INDEX IF NOT EXISTS idx_city_facility_operations_progress
    ON city_facility_operations (world_id, status, planned_start_tick, code);

CREATE TABLE IF NOT EXISTS city_facility_staff_assignments (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(96) NOT NULL CHECK (code ~ '^[a-z][a-z0-9_.-]{1,95}$'),
    facility_id BIGINT NOT NULL,
    role_code VARCHAR(64) NOT NULL CHECK (role_code ~ '^[a-z][a-z0-9_.-]{1,63}$'),
    subject_kind VARCHAR(16) NOT NULL CHECK (subject_kind IN ('entity', 'actor')),
    subject_code VARCHAR(128) NOT NULL CHECK (subject_code ~ '^[a-z][a-z0-9_.-]{1,127}$'),
    entity_id BIGINT,
    actor_id BIGINT,
    assigned_units BIGINT NOT NULL CHECK (assigned_units > 0),
    qualification_milli INTEGER NOT NULL CHECK (qualification_milli BETWEEN 0 AND 1000),
    effective_units BIGINT NOT NULL CHECK (
        effective_units = FLOOR(assigned_units::NUMERIC * qualification_milli / 1000)::BIGINT
    ),
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'released')),
    created_tick BIGINT NOT NULL CHECK (created_tick > 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= created_tick),
    version BIGINT NOT NULL CHECK (version > 0),
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_facility_staff_facility_fk
        FOREIGN KEY (facility_id, world_id)
        REFERENCES city_facilities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_staff_entity_fk
        FOREIGN KEY (entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_staff_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_staff_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_facility_lifecycle_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_facility_staff_subject_shape_check CHECK (
        (subject_kind = 'entity' AND entity_id IS NOT NULL AND actor_id IS NULL)
        OR (subject_kind = 'actor' AND entity_id IS NULL AND actor_id IS NOT NULL AND assigned_units = 1)
    ),
    CONSTRAINT city_facility_staff_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_facility_staff_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_facility_staff_actor_active
    ON city_facility_staff_assignments (world_id, actor_id)
    WHERE status = 'active' AND actor_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_city_facility_staff_facility_active
    ON city_facility_staff_assignments (world_id, facility_id, role_code, code)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS city_facility_incidents (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(96) NOT NULL CHECK (code ~ '^[a-z][a-z0-9_.-]{1,95}$'),
    facility_id BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('open', 'resolved')),
    severity_milli INTEGER NOT NULL CHECK (severity_milli BETWEEN 1 AND 1000),
    condition_before_milli INTEGER NOT NULL CHECK (condition_before_milli BETWEEN 0 AND 1000),
    failure_probability_ppm INTEGER NOT NULL CHECK (failure_probability_ppm BETWEEN 0 AND 1000000),
    sample_value_ppm INTEGER NOT NULL CHECK (sample_value_ppm BETWEEN 0 AND 999999),
    prng_proof VARCHAR(64) NOT NULL CHECK (prng_proof ~ '^[0-9a-f]{64}$'),
    opened_tick BIGINT NOT NULL CHECK (opened_tick > 0),
    resolved_tick BIGINT,
    repair_operation_code VARCHAR(96),
    version BIGINT NOT NULL CHECK (version > 0),
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_facility_incident_facility_fk
        FOREIGN KEY (facility_id, world_id)
        REFERENCES city_facilities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_incident_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_facility_lifecycle_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_facility_incident_status_shape_check CHECK (
        (status = 'open' AND resolved_tick IS NULL AND repair_operation_code IS NULL)
        OR (status = 'resolved' AND resolved_tick IS NOT NULL
            AND resolved_tick >= opened_tick AND repair_operation_code IS NOT NULL)
    ),
    CONSTRAINT city_facility_incidents_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_facility_incidents_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_facility_incidents_one_open
    ON city_facility_incidents (world_id, facility_id) WHERE status = 'open';

CREATE TABLE IF NOT EXISTS city_facility_budget_movements (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    source_fact_id BIGINT NOT NULL,
    operation_id BIGINT NOT NULL,
    budget_line_id BIGINT NOT NULL,
    movement_type VARCHAR(16) NOT NULL CHECK (movement_type IN ('commit', 'spend', 'release')),
    amount_units BIGINT NOT NULL CHECK (amount_units > 0),
    committed_before_units BIGINT NOT NULL CHECK (committed_before_units >= 0),
    committed_after_units BIGINT NOT NULL CHECK (committed_after_units >= 0),
    spent_before_units BIGINT NOT NULL CHECK (spent_before_units >= 0),
    spent_after_units BIGINT NOT NULL CHECK (spent_after_units >= 0),
    budget_version_before BIGINT NOT NULL CHECK (budget_version_before >= 0),
    budget_version_after BIGINT NOT NULL CHECK (budget_version_after = budget_version_before + 1),
    memo VARCHAR(256) NOT NULL DEFAULT '' CHECK (char_length(memo) <= 256),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_facility_budget_movement_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_facility_lifecycle_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_budget_movement_operation_fk
        FOREIGN KEY (operation_id, world_id)
        REFERENCES city_facility_operations(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_budget_movement_budget_fk
        FOREIGN KEY (budget_line_id, world_id)
        REFERENCES city_government_budget_lines(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_budget_movement_projection_check CHECK (
        (movement_type = 'commit'
         AND committed_after_units = committed_before_units + amount_units
         AND spent_after_units = spent_before_units)
        OR (movement_type = 'spend'
            AND committed_before_units = committed_after_units + amount_units
            AND spent_after_units = spent_before_units + amount_units)
        OR (movement_type = 'release'
            AND committed_before_units = committed_after_units + amount_units
            AND spent_after_units = spent_before_units)
    ),
    CONSTRAINT city_facility_budget_movement_fact_unique UNIQUE (source_fact_id),
    CONSTRAINT city_facility_budget_movements_id_world_unique UNIQUE (id, world_id)
);

ALTER TABLE city_journals DROP CONSTRAINT IF EXISTS city_journal_type_check;
ALTER TABLE city_journals ADD CONSTRAINT city_journal_type_check CHECK (
    journal_type IN (
        'opening', 'cash_transfer', 'wage', 'purchase', 'tax', 'subsidy',
        'reversal', 'government_spend', 'rent', 'facility_capital', 'facility_operation'
    )
);

CREATE OR REPLACE FUNCTION city_facility_lifecycle_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_facility_lifecycle_bootstrap_world_id', TRUE), '')
               = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND ((world.simulation_version = 'city-f8-v2' AND world.current_tick = 0)
                   OR city_engine_upgrade_write_enabled(target_world_id))
       )
$$;

CREATE OR REPLACE FUNCTION city_facility_lifecycle_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_facility_lifecycle_facts fact
        WHERE fact.id = CASE
            WHEN COALESCE(current_setting('sub2api.city_facility_lifecycle_fact_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.city_facility_lifecycle_fact_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND fact.world_id = target_world_id
          AND fact.posted_at IS NULL
    )
$$;

CREATE OR REPLACE FUNCTION guard_city_facility_lifecycle_catalog()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id)
       OR city_facility_lifecycle_bootstrap_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city facility lifecycle catalog requires bootstrap or recovery context'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_facility_lifecycle_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id)
       OR city_facility_lifecycle_bootstrap_write_enabled(target_world_id)
       OR city_facility_lifecycle_fact_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city facility lifecycle projection requires a draft fact'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_facility_lifecycle_profile_guard ON city_facility_lifecycle_profiles;
CREATE TRIGGER city_facility_lifecycle_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_facility_lifecycle_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_facility_lifecycle_projection();

DROP TRIGGER IF EXISTS city_facility_lifecycle_policy_guard ON city_facility_lifecycle_policies;
CREATE TRIGGER city_facility_lifecycle_policy_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_facility_lifecycle_policies
FOR EACH ROW EXECUTE FUNCTION guard_city_facility_lifecycle_catalog();

DROP TRIGGER IF EXISTS city_facility_lifecycle_state_guard ON city_facility_lifecycle_states;
CREATE TRIGGER city_facility_lifecycle_state_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_facility_lifecycle_states
FOR EACH ROW EXECUTE FUNCTION guard_city_facility_lifecycle_projection();

DROP TRIGGER IF EXISTS city_facility_operation_guard ON city_facility_operations;
CREATE TRIGGER city_facility_operation_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_facility_operations
FOR EACH ROW EXECUTE FUNCTION guard_city_facility_lifecycle_projection();

DROP TRIGGER IF EXISTS city_facility_staff_guard ON city_facility_staff_assignments;
CREATE TRIGGER city_facility_staff_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_facility_staff_assignments
FOR EACH ROW EXECUTE FUNCTION guard_city_facility_lifecycle_projection();

DROP TRIGGER IF EXISTS city_facility_incident_guard ON city_facility_incidents;
CREATE TRIGGER city_facility_incident_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_facility_incidents
FOR EACH ROW EXECUTE FUNCTION guard_city_facility_lifecycle_projection();

CREATE OR REPLACE FUNCTION guard_city_facility_lifecycle_fact_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    world_tick BIGINT;
    world_version VARCHAR(32);
    command_type_value VARCHAR(64);
    command_status_value VARCHAR(16);
    expected_fact_type VARCHAR(48);
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN RETURN NEW; END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version
    FROM city_worlds WHERE id = NEW.world_id;
    IF world_version IS DISTINCT FROM 'city-f8-v2' OR NEW.tick IS DISTINCT FROM world_tick + 1 THEN
        RAISE EXCEPTION 'city facility lifecycle fact must target the next F8.1 tick'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.source_command_id IS NULL THEN
        IF COALESCE(current_setting('sub2api.city_facility_lifecycle_auto_world_id', TRUE), '')
               IS DISTINCT FROM NEW.world_id::TEXT THEN
            RAISE EXCEPTION 'automatic city facility lifecycle fact is not authorized'
                USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    SELECT command_type, status INTO command_type_value, command_status_value
    FROM city_commands WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
    expected_fact_type := CASE command_type_value
        WHEN 'facility.register' THEN 'facility.initialized'
        WHEN 'facility.capacity.configure' THEN 'capacity.changed'
        WHEN 'facility.operation.schedule' THEN 'operation.scheduled'
        WHEN 'facility.operation.start' THEN 'operation.started'
        WHEN 'facility.operation.cancel' THEN 'operation.cancelled'
        WHEN 'facility.staffing.configure' THEN 'staffing.configured'
        ELSE NULL
    END;
    IF command_status_value IS DISTINCT FROM 'pending'
       OR expected_fact_type IS DISTINCT FROM NEW.fact_type THEN
        RAISE EXCEPTION 'city facility lifecycle fact does not match source command'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_facility_lifecycle_fact_insert_guard ON city_facility_lifecycle_facts;
CREATE TRIGGER city_facility_lifecycle_fact_insert_guard
BEFORE INSERT ON city_facility_lifecycle_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_facility_lifecycle_fact_insert();

-- A facility start command may consume two independently conserved resources.
-- Keep the predecessor one-command/one-operation rule for ordinary commands,
-- while giving facility starts an explicit one-command/one-resource identity.
-- The insert guard below makes the metadata predicates non-spoofable.
DROP INDEX IF EXISTS idx_city_resource_operations_one_per_command;
CREATE UNIQUE INDEX idx_city_resource_operations_one_per_command
    ON city_resource_operations (source_command_id)
    WHERE source_command_id IS NOT NULL
      AND NOT (metadata ? 'facility_lifecycle_fact_id');
CREATE UNIQUE INDEX IF NOT EXISTS idx_city_resource_operations_one_per_facility_command_resource
    ON city_resource_operations (source_command_id, (metadata ->> 'resource_code'))
    WHERE source_command_id IS NOT NULL
      AND metadata ? 'facility_lifecycle_fact_id';

CREATE OR REPLACE FUNCTION guard_city_facility_resource_operation_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    command_row city_commands%ROWTYPE;
    fact_row city_facility_lifecycle_facts%ROWTYPE;
    operation_row city_facility_operations%ROWTYPE;
    facility_district_id BIGINT;
    fact_id_value BIGINT;
    quantity_value BIGINT;
    quantity_numeric NUMERIC;
    expected_quantity BIGINT;
    operation_code_value TEXT;
    resource_code_value TEXT;
    facility_bound BOOLEAN;
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN RETURN NEW; END IF;

    IF NEW.source_command_id IS NOT NULL THEN
        SELECT * INTO command_row
        FROM city_commands
        WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
    END IF;
    facility_bound := COALESCE(command_row.command_type = 'facility.operation.start', FALSE)
        OR NEW.metadata ? 'facility_lifecycle_fact_id'
        OR NEW.metadata ? 'facility_operation_code';
    IF NOT facility_bound THEN RETURN NEW; END IF;

    IF NEW.operation_type IS DISTINCT FROM 'consumption'
       OR NEW.source_command_id IS NULL
       OR NEW.market_settlement_id IS NOT NULL
       OR NEW.recipe_id IS NOT NULL OR NEW.batch_count IS NOT NULL
       OR jsonb_typeof(NEW.metadata -> 'schema_version') IS DISTINCT FROM 'number'
       OR NEW.metadata ->> 'schema_version' IS DISTINCT FROM '1'
       OR jsonb_typeof(NEW.metadata -> 'facility_lifecycle_fact_id') IS DISTINCT FROM 'number'
       OR COALESCE(NEW.metadata ->> 'facility_lifecycle_fact_id', '') !~ '^[1-9][0-9]*$'
       OR jsonb_typeof(NEW.metadata -> 'facility_operation_code') IS DISTINCT FROM 'string'
       OR COALESCE(NEW.metadata ->> 'facility_operation_code', '') !~ '^[a-z][a-z0-9_.-]{1,95}$'
       OR jsonb_typeof(NEW.metadata -> 'resource_code') IS DISTINCT FROM 'string'
       OR COALESCE(NEW.metadata ->> 'resource_code', '') NOT IN ('basic_material', 'capital_goods')
       OR jsonb_typeof(NEW.metadata -> 'quantity_units') IS DISTINCT FROM 'number'
       OR COALESCE(NEW.metadata ->> 'quantity_units', '') !~ '^[1-9][0-9]*$' THEN
        RAISE EXCEPTION 'facility resource operation has an invalid origin shape'
            USING ERRCODE = '23514';
    END IF;

    quantity_numeric := (NEW.metadata ->> 'quantity_units')::NUMERIC;
    IF quantity_numeric > 9223372036854775807 THEN
        RAISE EXCEPTION 'facility resource operation quantity is out of range'
            USING ERRCODE = '23514';
    END IF;
    fact_id_value := (NEW.metadata ->> 'facility_lifecycle_fact_id')::BIGINT;
    quantity_value := quantity_numeric::BIGINT;
    operation_code_value := NEW.metadata ->> 'facility_operation_code';
    resource_code_value := NEW.metadata ->> 'resource_code';

    SELECT * INTO fact_row
    FROM city_facility_lifecycle_facts
    WHERE id = fact_id_value AND world_id = NEW.world_id;
    SELECT * INTO operation_row
    FROM city_facility_operations
    WHERE world_id = NEW.world_id AND code = operation_code_value;
    IF operation_row.id IS NOT NULL THEN
        SELECT district_id INTO facility_district_id
        FROM city_facilities
        WHERE id = operation_row.facility_id AND world_id = NEW.world_id;
    END IF;
    expected_quantity := CASE resource_code_value
        WHEN 'basic_material' THEN operation_row.required_basic_material_units
        WHEN 'capital_goods' THEN operation_row.required_capital_goods_units
        ELSE NULL
    END;

    IF command_row.id IS NULL
       OR command_row.command_type IS DISTINCT FROM 'facility.operation.start'
       OR command_row.status IS DISTINCT FROM 'pending'
       OR command_row.payload ->> 'operation_code' IS DISTINCT FROM operation_code_value
       OR command_row.payload ->> 'expected_operation_version' IS DISTINCT FROM operation_row.version::TEXT
       OR fact_row.id IS NULL
       OR COALESCE(current_setting('sub2api.city_facility_lifecycle_fact_id', TRUE), '')
            IS DISTINCT FROM fact_id_value::TEXT
       OR fact_row.tick IS DISTINCT FROM NEW.tick
       OR fact_row.phase IS DISTINCT FROM 'command'
       OR fact_row.fact_type IS DISTINCT FROM 'operation.started'
       OR fact_row.source_command_id IS DISTINCT FROM NEW.source_command_id
       OR fact_row.subject_kind IS DISTINCT FROM 'operation'
       OR fact_row.subject_code IS DISTINCT FROM operation_code_value
       OR fact_row.posted_at IS NOT NULL
       OR operation_row.id IS NULL OR operation_row.status IS DISTINCT FROM 'planned'
       OR operation_row.executor_entity_id IS DISTINCT FROM NEW.actor_entity_id
       OR facility_district_id IS DISTINCT FROM NEW.district_id
       OR expected_quantity IS NULL OR expected_quantity <= 0
       OR expected_quantity IS DISTINCT FROM quantity_value
       OR NEW.operation_key IS DISTINCT FROM
            'facility:' || operation_code_value || ':' || resource_code_value THEN
        RAISE EXCEPTION 'facility resource operation does not match its draft lifecycle fact'
            USING ERRCODE = '23514';
    END IF;

    IF jsonb_typeof(fact_row.payload -> 'resource_operations') IS DISTINCT FROM 'array' THEN
        RAISE EXCEPTION 'facility lifecycle fact is missing resource operation declarations'
            USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM jsonb_array_elements(fact_row.payload -> 'resource_operations') declaration
        WHERE declaration ->> 'tick' = NEW.tick::TEXT
          AND declaration ->> 'sequence' = NEW.sequence::TEXT
          AND declaration ->> 'operation_key' = NEW.operation_key
          AND declaration ->> 'resource_code' = resource_code_value
          AND declaration ->> 'quantity_units' = quantity_value::TEXT
    ) THEN
        RAISE EXCEPTION 'facility resource operation is not declared by its lifecycle fact'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_facility_resource_operation_insert_guard ON city_resource_operations;
CREATE TRIGGER city_facility_resource_operation_insert_guard
BEFORE INSERT ON city_resource_operations
FOR EACH ROW EXECUTE FUNCTION guard_city_facility_resource_operation_insert();

CREATE OR REPLACE FUNCTION guard_city_facility_lifecycle_fact_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(OLD.world_id) THEN RETURN OLD; END IF;
        RAISE EXCEPTION 'city facility lifecycle facts are immutable' USING ERRCODE = '55000';
    END IF;
    IF city_recovery_write_enabled(NEW.world_id) THEN RETURN NEW; END IF;
    IF OLD.posted_at IS NOT NULL OR NEW.posted_at IS NULL
       OR (NEW.id, NEW.world_id, NEW.tick, NEW.sequence, NEW.phase,
           NEW.source_command_id, NEW.fact_type, NEW.subject_kind, NEW.subject_code,
           NEW.version_before, NEW.version_after, NEW.payload, NEW.created_at)
          IS DISTINCT FROM
          (OLD.id, OLD.world_id, OLD.tick, OLD.sequence, OLD.phase,
           OLD.source_command_id, OLD.fact_type, OLD.subject_kind, OLD.subject_code,
           OLD.version_before, OLD.version_after, OLD.payload, OLD.created_at) THEN
        RAISE EXCEPTION 'city facility lifecycle fact can only be sealed once'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_facility_lifecycle_fact_immutable_guard ON city_facility_lifecycle_facts;
CREATE TRIGGER city_facility_lifecycle_fact_immutable_guard
BEFORE UPDATE OR DELETE ON city_facility_lifecycle_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_facility_lifecycle_fact_immutable();

CREATE OR REPLACE FUNCTION guard_city_facility_budget_movement()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF city_recovery_write_enabled(NEW.world_id)
           OR city_facility_lifecycle_fact_write_enabled(NEW.world_id) THEN
            RETURN NEW;
        END IF;
    ELSIF city_recovery_write_enabled(OLD.world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city facility budget movements are immutable lifecycle facts'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_facility_budget_movement_guard ON city_facility_budget_movements;
CREATE TRIGGER city_facility_budget_movement_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_facility_budget_movements
FOR EACH ROW EXECUTE FUNCTION guard_city_facility_budget_movement();

CREATE OR REPLACE FUNCTION post_city_facility_budget_movement(
    target_fact_id BIGINT,
    target_operation_id BIGINT,
    target_budget_line_id BIGINT,
    expected_budget_version BIGINT,
    target_movement_type VARCHAR,
    target_amount_units BIGINT,
    target_memo TEXT
)
RETURNS BIGINT
LANGUAGE plpgsql
AS $$
DECLARE
    fact_row city_facility_lifecycle_facts%ROWTYPE;
    operation_row city_facility_operations%ROWTYPE;
    budget_row city_government_budget_lines%ROWTYPE;
    next_committed BIGINT;
    next_spent BIGINT;
    movement_id BIGINT;
BEGIN
    SELECT * INTO fact_row FROM city_facility_lifecycle_facts
    WHERE id = target_fact_id AND posted_at IS NULL FOR UPDATE;
    SELECT * INTO operation_row FROM city_facility_operations
    WHERE id = target_operation_id FOR UPDATE;
    SELECT * INTO budget_row FROM city_government_budget_lines
    WHERE id = target_budget_line_id FOR UPDATE;
    IF NOT FOUND OR fact_row.id IS NULL OR operation_row.id IS NULL
       OR fact_row.world_id <> operation_row.world_id
       OR fact_row.world_id <> budget_row.world_id
       OR operation_row.budget_line_id IS DISTINCT FROM budget_row.id
       OR budget_row.version <> expected_budget_version
       OR target_amount_units <= 0 OR char_length(COALESCE(target_memo, '')) > 256
       OR target_movement_type NOT IN ('commit', 'spend', 'release') THEN
        RAISE EXCEPTION 'invalid city facility budget movement' USING ERRCODE = '23514';
    END IF;
    next_committed := budget_row.committed_units;
    next_spent := budget_row.spent_units;
    IF target_movement_type = 'commit' THEN
        next_committed := next_committed + target_amount_units;
    ELSIF target_movement_type = 'spend' THEN
        next_committed := next_committed - target_amount_units;
        next_spent := next_spent + target_amount_units;
    ELSE
        next_committed := next_committed - target_amount_units;
    END IF;
    IF next_committed < 0 OR next_spent < 0
       OR next_committed::NUMERIC + next_spent::NUMERIC > budget_row.appropriated_units::NUMERIC THEN
        RAISE EXCEPTION 'city facility budget authorization exceeded' USING ERRCODE = '23514';
    END IF;
    PERFORM set_config('sub2api.city_f4_write', 'on', TRUE);
    UPDATE city_government_budget_lines
    SET committed_units = next_committed, spent_units = next_spent,
        version = version + 1, updated_at = NOW()
    WHERE id = budget_row.id;
    PERFORM set_config('sub2api.city_f4_write', 'off', TRUE);
    INSERT INTO city_facility_budget_movements
        (world_id, source_fact_id, operation_id, budget_line_id, movement_type,
         amount_units, committed_before_units, committed_after_units,
         spent_before_units, spent_after_units, budget_version_before,
         budget_version_after, memo)
    VALUES
        (fact_row.world_id, fact_row.id, operation_row.id, budget_row.id,
         target_movement_type, target_amount_units, budget_row.committed_units,
         next_committed, budget_row.spent_units, next_spent,
         budget_row.version, budget_row.version + 1, COALESCE(target_memo, ''))
    RETURNING id INTO movement_id;
    RETURN movement_id;
END;
$$;

-- Extend all frozen predecessor guards to the explicit F8.1 compatibility set.
CREATE OR REPLACE FUNCTION migration_208_replace_function(
    target REGPROCEDURE, needle TEXT, replacement TEXT
)
RETURNS VOID
LANGUAGE plpgsql
AS $migration$
DECLARE definition TEXT; patched TEXT;
BEGIN
    SELECT pg_get_functiondef(target) INTO definition;
    patched := replace(definition, needle, replacement);
    IF patched = definition THEN
        IF POSITION(replacement IN definition) > 0 THEN RETURN; END IF;
        RAISE EXCEPTION 'migration 208 predecessor function % does not contain expected text', target
            USING ERRCODE = '23514';
    END IF;
    EXECUTE patched;
END;
$migration$;

SELECT migration_208_replace_function(
    'world_runtime_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'guard_world_runtime_fact_insert()'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'assert_world_runtime_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'guard_city_enterprise_location_fact_insert()'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'assert_city_enterprise_location_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'guard_city_development_fact_insert()'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'assert_city_development_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'assert_city_land_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'guard_city_spatial_mutation_insert()'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'assert_city_spatial_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'assert_world_actor_spatial_control_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'assert_world_portal_access_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'assert_world_navigation_intent_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1')$$, $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'city_service_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
    $$world.simulation_version = 'city-f8-v1'$$,
    $$world.simulation_version IN ('city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'guard_city_service_fact_insert()'::REGPROCEDURE,
    $$world_version IS DISTINCT FROM 'city-f8-v1'$$,
    $$world_version NOT IN ('city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'assert_city_service_foundation(bigint)'::REGPROCEDURE,
    $$world_version <> 'city-f8-v1'$$,
    $$world_version NOT IN ('city-f8-v1', 'city-f8-v2')$$);
SELECT migration_208_replace_function(
    'assert_city_resource_operation_ready(bigint)'::REGPROCEDURE,
    $$IF expected_command_type IS DISTINCT FROM required_command_type THEN$$,
    $$IF expected_command_type IS DISTINCT FROM required_command_type
           AND NOT (target_type = 'consumption'
                    AND expected_command_type = 'facility.operation.start') THEN$$);

DROP FUNCTION migration_208_replace_function(REGPROCEDURE, TEXT, TEXT);

CREATE OR REPLACE FUNCTION assert_city_facility_lifecycle_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    expected_policies BIGINT;
    actual_policies BIGINT;
    actual_states BIGINT;
    actual_operations BIGINT;
    actual_staffing BIGINT;
    actual_incidents BIGINT;
    actual_facts BIGINT;
    actual_budget_movements BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city world does not exist' USING ERRCODE = '23503';
    END IF;
    IF world_version <> 'city-f8-v2' THEN
        IF EXISTS (SELECT 1 FROM city_facility_lifecycle_profiles WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_facility_lifecycle_policies WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_facility_lifecycle_states WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_facility_operations WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_facility_staff_assignments WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_facility_incidents WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_facility_lifecycle_facts WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_facility_budget_movements WHERE world_id = target_world_id) THEN
            RAISE EXCEPTION 'pre-F8.1 world contains facility lifecycle state' USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;
    SELECT COUNT(*) INTO expected_policies FROM city_facility_type_definitions
    WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_policies FROM city_facility_lifecycle_policies
    WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_states FROM city_facility_lifecycle_states
    WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_operations FROM city_facility_operations
    WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_staffing FROM city_facility_staff_assignments
    WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_incidents FROM city_facility_incidents
    WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_facts FROM city_facility_lifecycle_facts
    WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_budget_movements FROM city_facility_budget_movements
    WHERE world_id = target_world_id;
    IF NOT EXISTS (
        SELECT 1 FROM city_facility_lifecycle_profiles profile
        WHERE profile.world_id = target_world_id
          AND profile.baseline_tick <= world_tick
          AND profile.policy_count = expected_policies
          AND profile.state_count = actual_states
          AND profile.policy_count = actual_policies
          AND profile.operation_count = actual_operations
          AND profile.staffing_count = actual_staffing
          AND profile.incident_count = actual_incidents
          AND profile.fact_count = actual_facts
          AND profile.budget_movement_count = actual_budget_movements
          AND profile.revision = actual_facts + 1
          AND profile.policy_id = 'sub2api-facility-lifecycle'
          AND profile.policy_version = '1.0.0'
    ) THEN
        RAISE EXCEPTION 'city F8.1 lifecycle profile is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_facility_lifecycle_facts fact
    LEFT JOIN city_commands command ON command.id = fact.source_command_id
    WHERE fact.world_id = target_world_id
      AND (fact.tick > world_tick OR fact.posted_at IS NULL
           OR (fact.phase = 'command' AND
               (command.id IS NULL OR command.status <> 'applied'
                OR command.processed_tick <> fact.tick))
           OR (fact.phase <> 'command' AND command.id IS NOT NULL));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.1 lifecycle fact origin is inconsistent' USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*) INTO invalid_count
    FROM (
        SELECT tick, MIN(sequence) AS first_sequence,
               MAX(sequence) AS last_sequence, COUNT(*) AS sequence_count
        FROM city_facility_lifecycle_facts
        WHERE world_id = target_world_id
        GROUP BY tick
    ) tick_facts
    WHERE tick_facts.first_sequence <> 1
       OR tick_facts.last_sequence <> tick_facts.sequence_count;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.1 lifecycle fact sequence is not contiguous' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_facilities facility
    LEFT JOIN city_facility_lifecycle_states state
      ON state.world_id = facility.world_id AND state.facility_id = facility.id
    LEFT JOIN city_facility_lifecycle_policies policy
      ON policy.world_id = facility.world_id AND policy.id = state.policy_id
    WHERE facility.world_id = target_world_id
      AND (state.facility_id IS NULL OR policy.facility_type_id <> facility.facility_type_id
           OR state.updated_tick > world_tick
           OR (COALESCE(state.metadata->>'staffing_source', 'assignments') <> 'legacy_baseline'
               AND state.staff_assigned_units IS DISTINCT FROM (
                SELECT COALESCE(SUM(assignment.effective_units), 0)::BIGINT
                FROM city_facility_staff_assignments assignment
                WHERE assignment.world_id = facility.world_id
                  AND assignment.facility_id = facility.id
                  AND assignment.status = 'active'))
           OR state.active_operation_code IS DISTINCT FROM (
                SELECT operation.code FROM city_facility_operations operation
                WHERE operation.world_id = facility.world_id
                  AND operation.facility_id = facility.id
                  AND operation.status IN ('planned', 'active') LIMIT 1)
           OR state.open_incident_code IS DISTINCT FROM (
                SELECT incident.code FROM city_facility_incidents incident
                WHERE incident.world_id = facility.world_id
                  AND incident.facility_id = facility.id
                  AND incident.status = 'open' LIMIT 1));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.1 facility lifecycle head is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_facility_operations operation
    JOIN city_economic_entities executor ON executor.id = operation.executor_entity_id
    JOIN city_economic_entities sponsor ON sponsor.id = operation.sponsor_entity_id
    LEFT JOIN city_firm_states firm ON firm.world_id = operation.world_id
      AND firm.entity_id = operation.executor_entity_id
    LEFT JOIN city_government_budget_lines budget ON budget.id = operation.budget_line_id
    LEFT JOIN LATERAL (
        SELECT
            COALESCE(SUM(CASE movement.movement_type
                WHEN 'commit' THEN movement.amount_units
                WHEN 'spend' THEN -movement.amount_units
                WHEN 'release' THEN -movement.amount_units
                ELSE 0 END), 0)::BIGINT AS committed_units,
            COALESCE(SUM(CASE WHEN movement.movement_type = 'spend'
                THEN movement.amount_units ELSE 0 END), 0)::BIGINT AS spent_units,
            COUNT(*)::BIGINT AS movement_count
        FROM city_facility_budget_movements movement
        WHERE movement.world_id = operation.world_id
          AND movement.operation_id = operation.id
    ) movement_projection ON TRUE
    WHERE operation.world_id = target_world_id
      AND (executor.world_id <> operation.world_id
           OR executor.status <> 'active' OR executor.entity_type <> 'firm' OR firm.id IS NULL
           OR sponsor.world_id <> operation.world_id OR sponsor.status <> 'active'
           OR sponsor.entity_type NOT IN ('government', 'firm')
           OR (sponsor.entity_type = 'government' AND
               (budget.id IS NULL OR budget.world_id <> operation.world_id
                OR budget.government_entity_id <> sponsor.id))
           OR (sponsor.entity_type = 'firm' AND budget.id IS NOT NULL)
           OR movement_projection.committed_units <> operation.budget_committed_units
           OR movement_projection.spent_units <> operation.budget_spent_units
           OR (budget.id IS NULL AND movement_projection.movement_count <> 0));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.1 operation ownership or budget is invalid' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_firm_states firm
    JOIN LATERAL (
        SELECT
            COALESCE((SELECT SUM(operation.required_labor_units)::BIGINT
                FROM city_facility_operations operation
                WHERE operation.world_id = firm.world_id
                  AND operation.executor_entity_id = firm.entity_id
                  AND operation.status = 'active'), 0)
            + COALESCE((SELECT SUM(assignment.assigned_units)::BIGINT
                FROM city_facility_staff_assignments assignment
                WHERE assignment.world_id = firm.world_id
                  AND assignment.entity_id = firm.entity_id
                  AND assignment.status = 'active'), 0)
            + COALESCE((SELECT SUM(project.required_labor_units)::BIGINT
                FROM city_development_projects project
                WHERE project.world_id = firm.world_id
                  AND project.developer_entity_id = firm.entity_id
                  AND project.status = 'under_construction'), 0) AS reserved
    ) reservation ON TRUE
    WHERE firm.world_id = target_world_id AND reservation.reserved > firm.employee_units;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.1 labor reservation exceeds entity capacity' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_facility_lifecycle_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM assert_city_facility_lifecycle_foundation(COALESCE(NEW.id, OLD.id));
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_facility_lifecycle_world_check ON city_worlds;
CREATE CONSTRAINT TRIGGER city_facility_lifecycle_world_check
AFTER INSERT OR UPDATE ON city_worlds DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_facility_lifecycle_foundation();

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f8-v2', 'supported', 'city-state-v1+gzip',
        '["f1","f2","f3","f4","f5","f6","f6.2","f6.3","f7","f7.3","f7.4","f7.5","f7.6","f7.7","f7.9","f7.10","f7.11","f8","f8.1","facility_lifecycle","facility_staffing","facility_incidents","facility_budget"]'::jsonb)
ON CONFLICT (version) DO UPDATE SET status = EXCLUDED.status,
    canonical_format = EXCLUDED.canonical_format, capabilities = EXCLUDED.capabilities;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f8-v1', 'city-f8-v2', 'f8_v1_to_f8_v2')
ON CONFLICT (from_version, to_version) DO NOTHING;
