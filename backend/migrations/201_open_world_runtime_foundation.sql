-- 开放世界模拟 F7.6：版本化定义目录、Actor、Requirement、Rule、Fact 与 Effect 运行时。

CREATE TABLE IF NOT EXISTS world_runtime_profiles (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    runtime_id VARCHAR(64) NOT NULL,
    runtime_version VARCHAR(24) NOT NULL,
    catalog_version VARCHAR(24) NOT NULL,
    catalog_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    maximum_player_actors_per_member INTEGER NOT NULL DEFAULT 1
        CHECK (maximum_player_actors_per_member BETWEEN 1 AND 64),
    actor_count BIGINT NOT NULL DEFAULT 0 CHECK (actor_count >= 0),
    fact_count BIGINT NOT NULL DEFAULT 0 CHECK (fact_count >= 0),
    effect_count BIGINT NOT NULL DEFAULT 0 CHECK (effect_count >= 0),
    case_count BIGINT NOT NULL DEFAULT 0 CHECK (case_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_runtime_profile_identity_check CHECK (
        runtime_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND runtime_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND catalog_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND catalog_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT world_runtime_profile_revision_check CHECK (revision = fact_count + 1),
    CONSTRAINT world_runtime_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS world_runtime_definitions (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    definition_kind VARCHAR(32) NOT NULL,
    code VARCHAR(128) NOT NULL,
    definition_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    visibility VARCHAR(16) NOT NULL DEFAULT 'public'
        CHECK (visibility IN ('public', 'discoverable', 'hidden')),
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_runtime_definition_identity_check CHECK (
        definition_kind ~ '^[a-z][a-z0-9_]{1,31}$'
        AND code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND definition_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND content_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT world_runtime_definition_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT world_runtime_definitions_world_identity_unique
        UNIQUE (world_id, definition_kind, code),
    CONSTRAINT world_runtime_definitions_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_world_runtime_definitions_catalog
    ON world_runtime_definitions (world_id, definition_kind, code);

CREATE TABLE IF NOT EXISTS world_actors (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(128) NOT NULL,
    owner_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    actor_type_code VARCHAR(128) NOT NULL,
    name VARCHAR(96) NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    archetype_code VARCHAR(128),
    archetype_version VARCHAR(24),
    created_tick BIGINT NOT NULL CHECK (created_tick >= 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= created_tick),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_actor_code_check CHECK (code ~ '^[a-z][a-z0-9_.-]{1,127}$'),
    CONSTRAINT world_actor_type_check CHECK (actor_type_code ~ '^[a-z][a-z0-9_.-]{1,127}$'),
    CONSTRAINT world_actor_name_check CHECK (
        char_length(name) BETWEEN 1 AND 96 AND name = btrim(name)
    ),
    CONSTRAINT world_actor_status_check CHECK (status ~ '^[a-z][a-z0-9_.-]{1,23}$'),
    CONSTRAINT world_actor_archetype_check CHECK (
        (archetype_code IS NULL AND archetype_version IS NULL)
        OR (archetype_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
            AND archetype_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$')
    ),
    CONSTRAINT world_actor_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT world_actors_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT world_actors_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_world_actors_owner
    ON world_actors (world_id, owner_user_id, status, code);

CREATE TABLE IF NOT EXISTS world_actor_attributes (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    actor_id BIGINT NOT NULL,
    attribute_code VARCHAR(128) NOT NULL,
    value_units BIGINT NOT NULL,
    experience_units BIGINT NOT NULL DEFAULT 0 CHECK (experience_units >= 0),
    last_changed_tick BIGINT NOT NULL CHECK (last_changed_tick >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_actor_attribute_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT world_actor_attribute_code_check
        CHECK (attribute_code ~ '^[a-z][a-z0-9_.-]{1,127}$'),
    CONSTRAINT world_actor_attribute_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT world_actor_attributes_identity_unique
        UNIQUE (world_id, actor_id, attribute_code),
    CONSTRAINT world_actor_attributes_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_world_actor_attributes_actor
    ON world_actor_attributes (world_id, actor_id, attribute_code);

CREATE TABLE IF NOT EXISTS world_actor_roles (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    actor_id BIGINT NOT NULL,
    role_code VARCHAR(128) NOT NULL,
    category_code VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'revoked')),
    granted_tick BIGINT NOT NULL CHECK (granted_tick >= 0),
    revoked_tick BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_actor_role_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT world_actor_role_code_check CHECK (
        role_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND category_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
    ),
    CONSTRAINT world_actor_role_lifecycle_check CHECK (
        (status = 'active' AND revoked_tick IS NULL)
        OR (status = 'revoked' AND revoked_tick IS NOT NULL AND revoked_tick >= granted_tick)
    ),
    CONSTRAINT world_actor_role_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT world_actor_roles_identity_unique
        UNIQUE (world_id, actor_id, role_code, granted_tick),
    CONSTRAINT world_actor_roles_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_world_actor_roles_active_category
    ON world_actor_roles (world_id, actor_id, category_code)
    WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_world_actor_roles_history
    ON world_actor_roles (world_id, actor_id, category_code, granted_tick, role_code);

CREATE TABLE IF NOT EXISTS world_actor_statuses (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    actor_id BIGINT NOT NULL,
    instance_code VARCHAR(160) NOT NULL,
    status_code VARCHAR(128) NOT NULL,
    lifecycle_status VARCHAR(16) NOT NULL CHECK (
        lifecycle_status IN ('active', 'revoked', 'expired')
    ),
    intensity_units BIGINT NOT NULL DEFAULT 1000 CHECK (intensity_units >= 0),
    stacks INTEGER NOT NULL DEFAULT 1 CHECK (stacks BETWEEN 1 AND 1000000),
    granted_tick BIGINT NOT NULL CHECK (granted_tick >= 0),
    expires_tick BIGINT,
    ended_tick BIGINT,
    source_fact_id BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_actor_status_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT world_actor_status_identity_check CHECK (
        instance_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND status_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
    ),
    CONSTRAINT world_actor_status_lifecycle_check CHECK (
        (lifecycle_status = 'active' AND ended_tick IS NULL
            AND (expires_tick IS NULL OR expires_tick > granted_tick))
        OR (lifecycle_status IN ('revoked', 'expired') AND ended_tick IS NOT NULL
            AND ended_tick >= granted_tick)
    ),
    CONSTRAINT world_actor_status_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT world_actor_statuses_world_instance_unique UNIQUE (world_id, instance_code),
    CONSTRAINT world_actor_statuses_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_world_actor_statuses_active_code
    ON world_actor_statuses (world_id, actor_id, status_code)
    WHERE lifecycle_status = 'active';
CREATE INDEX IF NOT EXISTS idx_world_actor_statuses_expiration
    ON world_actor_statuses (world_id, expires_tick, actor_id)
    WHERE lifecycle_status = 'active' AND expires_tick IS NOT NULL;

CREATE TABLE IF NOT EXISTS world_runtime_facts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    source_command_id BIGINT,
    parent_fact_id BIGINT,
    actor_id BIGINT,
    fact_type VARCHAR(128) NOT NULL,
    definition_kind VARCHAR(32),
    definition_code VARCHAR(128),
    definition_version VARCHAR(24),
    definition_hash VARCHAR(64),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted_at TIMESTAMPTZ,
    CONSTRAINT world_runtime_fact_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_runtime_fact_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_runtime_fact_parent_fk
        FOREIGN KEY (parent_fact_id, world_id)
        REFERENCES world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_runtime_fact_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES world_actors(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_runtime_fact_type_check
        CHECK (fact_type ~ '^[a-z][a-z0-9_.-]{1,127}$'),
    CONSTRAINT world_runtime_fact_definition_check CHECK (
        (definition_kind IS NULL AND definition_code IS NULL
            AND definition_version IS NULL AND definition_hash IS NULL)
        OR (definition_kind ~ '^[a-z][a-z0-9_]{1,31}$'
            AND definition_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
            AND definition_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
            AND definition_hash ~ '^[0-9a-f]{64}$')
    ),
    CONSTRAINT world_runtime_fact_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT world_runtime_fact_posted_check CHECK (posted_at IS NULL OR posted_at >= created_at),
    CONSTRAINT world_runtime_facts_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT world_runtime_facts_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_world_runtime_facts_root_command
    ON world_runtime_facts (source_command_id)
    WHERE source_command_id IS NOT NULL AND parent_fact_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_world_runtime_facts_actor_history
    ON world_runtime_facts (world_id, actor_id, tick, sequence);
CREATE INDEX IF NOT EXISTS idx_world_runtime_facts_definition_history
    ON world_runtime_facts (world_id, definition_kind, definition_code, tick, sequence);

ALTER TABLE world_actor_statuses
    ADD CONSTRAINT world_actor_status_source_fact_fk
    FOREIGN KEY (source_fact_id, world_id)
    REFERENCES world_runtime_facts(id, world_id) ON DELETE RESTRICT
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE IF NOT EXISTS world_effect_operations (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    source_fact_id BIGINT NOT NULL,
    operation_index INTEGER NOT NULL CHECK (operation_index > 0),
    effect_type VARCHAR(128) NOT NULL,
    executor_version VARCHAR(24) NOT NULL,
    target_actor_id BIGINT,
    target_key VARCHAR(160),
    before_units BIGINT,
    delta_units BIGINT,
    after_units BIGINT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_effect_operation_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_effect_operation_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES world_runtime_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT world_effect_operation_actor_fk
        FOREIGN KEY (target_actor_id, world_id)
        REFERENCES world_actors(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_effect_operation_identity_check CHECK (
        effect_type ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND executor_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND (target_key IS NULL OR target_key ~ '^[a-z][a-z0-9_.-]{1,159}$')
    ),
    CONSTRAINT world_effect_operation_units_check CHECK (
        (before_units IS NULL AND delta_units IS NULL AND after_units IS NULL)
        OR (before_units IS NOT NULL AND delta_units IS NOT NULL AND after_units IS NOT NULL
            AND after_units::NUMERIC = before_units::NUMERIC + delta_units::NUMERIC)
    ),
    CONSTRAINT world_effect_operation_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT world_effect_operations_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT world_effect_operations_fact_index_unique UNIQUE (source_fact_id, operation_index),
    CONSTRAINT world_effect_operations_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_world_effect_operations_actor_history
    ON world_effect_operations (world_id, target_actor_id, tick, sequence);

CREATE TABLE IF NOT EXISTS world_rule_cases (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    source_fact_id BIGINT NOT NULL,
    consequence_fact_id BIGINT,
    subject_actor_id BIGINT NOT NULL,
    rule_code VARCHAR(128) NOT NULL,
    rule_version VARCHAR(24) NOT NULL,
    rule_hash VARCHAR(64) NOT NULL,
    category_code VARCHAR(128) NOT NULL,
    scope_kind VARCHAR(64) NOT NULL,
    scope_code VARCHAR(160) NOT NULL,
    status VARCHAR(24) NOT NULL CHECK (status IN ('open', 'decided', 'closed', 'dismissed')),
    severity_units BIGINT NOT NULL CHECK (severity_units >= 0),
    decision_code VARCHAR(128),
    created_tick BIGINT NOT NULL CHECK (created_tick > 0),
    decided_tick BIGINT,
    closed_tick BIGINT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT world_rule_case_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES world_runtime_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT world_rule_case_consequence_fact_fk
        FOREIGN KEY (consequence_fact_id, world_id)
        REFERENCES world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT world_rule_case_actor_fk
        FOREIGN KEY (subject_actor_id, world_id)
        REFERENCES world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT world_rule_case_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND rule_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND rule_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND rule_hash ~ '^[0-9a-f]{64}$'
        AND category_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND scope_kind ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND scope_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND (decision_code IS NULL OR decision_code ~ '^[a-z][a-z0-9_.-]{1,127}$')
    ),
    CONSTRAINT world_rule_case_lifecycle_check CHECK (
        (status = 'open' AND decision_code IS NULL AND decided_tick IS NULL AND closed_tick IS NULL)
        OR (status = 'decided' AND decision_code IS NOT NULL AND decided_tick IS NOT NULL
            AND decided_tick >= created_tick AND closed_tick IS NULL)
        OR (status IN ('closed', 'dismissed') AND decision_code IS NOT NULL
            AND decided_tick IS NOT NULL AND closed_tick IS NOT NULL
            AND closed_tick >= decided_tick AND decided_tick >= created_tick)
    ),
    CONSTRAINT world_rule_case_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT world_rule_cases_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT world_rule_cases_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT world_rule_cases_source_rule_unique UNIQUE (source_fact_id, rule_code),
    CONSTRAINT world_rule_cases_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_world_rule_cases_actor_history
    ON world_rule_cases (world_id, subject_actor_id, tick, sequence);

CREATE OR REPLACE FUNCTION world_runtime_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.world_runtime_bootstrap_world_id', TRUE), '')
               = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND ((world.simulation_version = 'city-f7-v5' AND world.current_tick = 0)
                   OR city_engine_upgrade_write_enabled(target_world_id))
       )
$$;

CREATE OR REPLACE FUNCTION world_runtime_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM world_runtime_facts fact
        WHERE fact.id = CASE
            WHEN COALESCE(current_setting('sub2api.world_runtime_fact_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.world_runtime_fact_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND fact.world_id = target_world_id
          AND fact.posted_at IS NULL
    )
$$;

CREATE OR REPLACE FUNCTION guard_world_runtime_catalog_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id)
       OR world_runtime_bootstrap_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'world runtime catalog is immutable outside bootstrap or recovery'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS world_runtime_definition_immutable_guard ON world_runtime_definitions;
CREATE TRIGGER world_runtime_definition_immutable_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_runtime_definitions
FOR EACH ROW EXECUTE FUNCTION guard_world_runtime_catalog_immutable();

CREATE OR REPLACE FUNCTION guard_world_runtime_profile_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id)
       OR world_runtime_bootstrap_write_enabled(target_world_id)
       OR world_runtime_fact_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'world runtime profile requires bootstrap, draft fact, or recovery context'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS world_runtime_profile_projection_guard ON world_runtime_profiles;
CREATE TRIGGER world_runtime_profile_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_runtime_profiles
FOR EACH ROW EXECUTE FUNCTION guard_world_runtime_profile_projection();

CREATE OR REPLACE FUNCTION guard_world_runtime_fact_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    world_tick BIGINT;
    world_version VARCHAR(32);
    command_status_value VARCHAR(16);
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version
    FROM city_worlds WHERE id = NEW.world_id;
    IF world_version IS DISTINCT FROM 'city-f7-v5'
       OR NEW.tick IS DISTINCT FROM world_tick + 1
       OR NEW.posted_at IS NOT NULL THEN
        RAISE EXCEPTION 'world runtime fact must be a draft for the next F7.6 tick'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.source_command_id IS NOT NULL THEN
        SELECT status INTO command_status_value
        FROM city_commands
        WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
        IF command_status_value IS DISTINCT FROM 'pending' THEN
            RAISE EXCEPTION 'world runtime fact requires a pending source command'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF NEW.source_command_id IS NULL AND NEW.parent_fact_id IS NULL THEN
        RAISE EXCEPTION 'derived world runtime fact requires a parent fact'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS world_runtime_fact_insert_guard ON world_runtime_facts;
CREATE TRIGGER world_runtime_fact_insert_guard
BEFORE INSERT ON world_runtime_facts
FOR EACH ROW EXECUTE FUNCTION guard_world_runtime_fact_insert();

CREATE OR REPLACE FUNCTION guard_world_runtime_fact_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(OLD.world_id) THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'world runtime facts are immutable' USING ERRCODE = '55000';
    END IF;
    IF city_recovery_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    IF OLD.posted_at IS NOT NULL OR NEW.posted_at IS NULL
       OR (OLD.id, OLD.world_id, OLD.tick, OLD.sequence, OLD.source_command_id,
           OLD.parent_fact_id, OLD.actor_id, OLD.fact_type, OLD.definition_kind,
           OLD.definition_code, OLD.definition_version, OLD.definition_hash,
           OLD.payload, OLD.created_at)
          IS DISTINCT FROM
          (NEW.id, NEW.world_id, NEW.tick, NEW.sequence, NEW.source_command_id,
           NEW.parent_fact_id, NEW.actor_id, NEW.fact_type, NEW.definition_kind,
           NEW.definition_code, NEW.definition_version, NEW.definition_hash,
           NEW.payload, NEW.created_at) THEN
        RAISE EXCEPTION 'world runtime facts permit only one draft-to-posted transition'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS world_runtime_fact_immutable_guard ON world_runtime_facts;
CREATE TRIGGER world_runtime_fact_immutable_guard
BEFORE UPDATE OR DELETE ON world_runtime_facts
FOR EACH ROW EXECUTE FUNCTION guard_world_runtime_fact_immutable();

CREATE OR REPLACE FUNCTION guard_world_runtime_projection_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id)
       OR world_runtime_fact_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'world runtime projection requires a draft fact or recovery context'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS world_actor_projection_guard ON world_actors;
CREATE TRIGGER world_actor_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_actors
FOR EACH ROW EXECUTE FUNCTION guard_world_runtime_projection_write();

DROP TRIGGER IF EXISTS world_actor_attribute_projection_guard ON world_actor_attributes;
CREATE TRIGGER world_actor_attribute_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_actor_attributes
FOR EACH ROW EXECUTE FUNCTION guard_world_runtime_projection_write();

DROP TRIGGER IF EXISTS world_actor_role_projection_guard ON world_actor_roles;
CREATE TRIGGER world_actor_role_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_actor_roles
FOR EACH ROW EXECUTE FUNCTION guard_world_runtime_projection_write();

DROP TRIGGER IF EXISTS world_actor_status_projection_guard ON world_actor_statuses;
CREATE TRIGGER world_actor_status_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_actor_statuses
FOR EACH ROW EXECUTE FUNCTION guard_world_runtime_projection_write();

CREATE OR REPLACE FUNCTION guard_world_effect_operation_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND world_runtime_fact_write_enabled(target_world_id) THEN
        IF NEW.source_fact_id::TEXT IS DISTINCT FROM
           current_setting('sub2api.world_runtime_fact_id', TRUE) THEN
            RAISE EXCEPTION 'world effect source fact does not match active draft fact'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'world effect operations are immutable and require an active draft fact'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS world_effect_operation_immutable_guard ON world_effect_operations;
CREATE TRIGGER world_effect_operation_immutable_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_effect_operations
FOR EACH ROW EXECUTE FUNCTION guard_world_effect_operation_immutable();

CREATE OR REPLACE FUNCTION guard_world_rule_case_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND world_runtime_fact_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'world rule cases require an active draft fact or recovery context'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS world_rule_case_projection_guard ON world_rule_cases;
CREATE TRIGGER world_rule_case_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON world_rule_cases
FOR EACH ROW EXECUTE FUNCTION guard_world_rule_case_projection();

CREATE OR REPLACE FUNCTION assert_world_runtime_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    profile_row world_runtime_profiles%ROWTYPE;
    actual_count BIGINT;
BEGIN
    SELECT simulation_version INTO world_version FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'world runtime world does not exist' USING ERRCODE = '23514';
    END IF;
    SELECT * INTO profile_row FROM world_runtime_profiles WHERE world_id = target_world_id;
    IF world_version <> 'city-f7-v5' THEN
        IF FOUND OR EXISTS (SELECT 1 FROM world_runtime_definitions WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM world_actors WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM world_runtime_facts WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM world_effect_operations WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM world_rule_cases WHERE world_id = target_world_id) THEN
            RAISE EXCEPTION 'legacy city engine cannot contain world runtime state'
                USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;
    IF NOT FOUND OR profile_row.runtime_id <> 'sub2api-open-world-runtime'
       OR profile_row.runtime_version <> '1.0.0'
       OR profile_row.catalog_hash !~ '^[0-9a-f]{64}$' THEN
        RAISE EXCEPTION 'world runtime profile is missing or invalid' USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*) INTO actual_count FROM world_actors WHERE world_id = target_world_id;
    IF actual_count <> profile_row.actor_count THEN
        RAISE EXCEPTION 'world runtime actor count is inconsistent' USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*) INTO actual_count FROM world_runtime_facts WHERE world_id = target_world_id;
    IF actual_count <> profile_row.fact_count
       OR EXISTS (SELECT 1 FROM world_runtime_facts WHERE world_id = target_world_id AND posted_at IS NULL) THEN
        RAISE EXCEPTION 'world runtime fact count or commit state is inconsistent' USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*) INTO actual_count FROM world_effect_operations WHERE world_id = target_world_id;
    IF actual_count <> profile_row.effect_count THEN
        RAISE EXCEPTION 'world runtime effect count is inconsistent' USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*) INTO actual_count FROM world_rule_cases WHERE world_id = target_world_id;
    IF actual_count <> profile_row.case_count THEN
        RAISE EXCEPTION 'world runtime case count is inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM world_actors actor
        LEFT JOIN world_runtime_definitions actor_type
          ON actor_type.world_id = actor.world_id
         AND actor_type.definition_kind = 'actor_type'
         AND actor_type.code = actor.actor_type_code
        LEFT JOIN world_runtime_definitions archetype
          ON archetype.world_id = actor.world_id
         AND archetype.definition_kind = 'archetype'
         AND archetype.code = actor.archetype_code
        WHERE actor.world_id = target_world_id
          AND (actor_type.id IS NULL OR (actor.archetype_code IS NOT NULL AND archetype.id IS NULL))
    ) THEN
        RAISE EXCEPTION 'world runtime actor references an unknown definition' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM world_actor_attributes value
        LEFT JOIN world_runtime_definitions definition
          ON definition.world_id = value.world_id
         AND definition.definition_kind = 'attribute'
         AND definition.code = value.attribute_code
        WHERE value.world_id = target_world_id AND definition.id IS NULL
    ) THEN
        RAISE EXCEPTION 'world actor attribute references an unknown definition' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM world_actor_roles role
        LEFT JOIN world_runtime_definitions definition
          ON definition.world_id = role.world_id
         AND definition.definition_kind = 'role'
         AND definition.code = role.role_code
        WHERE role.world_id = target_world_id
          AND (definition.id IS NULL OR definition.payload->>'category_code' <> role.category_code)
    ) THEN
        RAISE EXCEPTION 'world actor role references an inconsistent definition' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM world_actor_statuses status
        LEFT JOIN world_runtime_definitions definition
          ON definition.world_id = status.world_id
         AND definition.definition_kind = 'status'
         AND definition.code = status.status_code
        WHERE status.world_id = target_world_id AND definition.id IS NULL
    ) THEN
        RAISE EXCEPTION 'world actor status references an unknown definition' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM world_effect_operations effect
        JOIN world_runtime_facts fact
          ON fact.id = effect.source_fact_id AND fact.world_id = effect.world_id
        WHERE effect.world_id = target_world_id
          AND (fact.posted_at IS NULL OR fact.tick <> effect.tick)
    ) THEN
        RAISE EXCEPTION 'world effect does not belong to a posted same-tick fact' USING ERRCODE = '23514';
    END IF;
END;
$$;

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f7-v5', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","spatial","development","enterprise_location","world_runtime","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f7-v4', 'city-f7-v5', 'f7_v4_to_f7_v5')
ON CONFLICT (from_version, to_version) DO NOTHING;

-- F7.6 继承 F7.1-F7.5 的冻结空间事实域；仅扩展兼容版本集合，不改变旧版本语义。
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
    IF world_version NOT IN ('city-f7-v4', 'city-f7-v5')
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
    IF world_version NOT IN ('city-f7-v4', 'city-f7-v5') THEN
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
    IF world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5')
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
    IF world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5') THEN
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

    IF world_version NOT IN ('city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5') THEN
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
       OR world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5')
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
    IF world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5') THEN
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
