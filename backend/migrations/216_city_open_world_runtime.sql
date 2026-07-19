-- Open-world V4 runtime.  This is deliberately independent from F7's fixed
-- overmap runtime: its locations reference the immutable V3 Region/Sector,
-- interior and portal facts instead of the legacy F7 spatial projections.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v4', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","rules","markets","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

-- V4 keeps the V3 static generator contract and only layers runtime facts on
-- top.  Its static records must therefore be admitted by the existing genesis
-- and command-gated materialization guards without changing any prior hash.
CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN (
                  'city-openworld-v1', 'city-openworld-v2', 'city-openworld-v3', 'city-openworld-v4'
              )
              AND world.current_tick = 0
              AND world.state_hash IS NULL
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_materialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_materialization_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v2', 'city-openworld-v3', 'city-openworld-v4')
              AND world.current_tick >= 0
              AND world.state_hash IS NOT NULL
       )
$$;

CREATE TABLE IF NOT EXISTS city_open_world_runtime_profiles (
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
    CONSTRAINT city_open_world_runtime_profile_identity_check CHECK (
        runtime_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND runtime_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND catalog_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND catalog_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_open_world_runtime_profile_revision_check CHECK (revision = fact_count + 1),
    CONSTRAINT city_open_world_runtime_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_runtime_definitions (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
    definition_kind VARCHAR(32) NOT NULL,
    code VARCHAR(128) NOT NULL,
    definition_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    visibility VARCHAR(16) NOT NULL DEFAULT 'public'
        CHECK (visibility IN ('public', 'discoverable', 'hidden')),
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_runtime_definition_identity_check CHECK (
        definition_kind ~ '^[a-z][a-z0-9_]{1,31}$'
        AND code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND definition_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND content_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_open_world_runtime_definition_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT city_open_world_runtime_definitions_world_identity_unique
        UNIQUE (world_id, definition_kind, code),
    CONSTRAINT city_open_world_runtime_definitions_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_runtime_definitions_catalog
    ON city_open_world_runtime_definitions (world_id, definition_kind, code);

CREATE TABLE IF NOT EXISTS city_open_world_actors (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
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
    CONSTRAINT city_open_world_actor_code_check CHECK (code ~ '^[a-z][a-z0-9_.-]{1,127}$'),
    CONSTRAINT city_open_world_actor_type_check CHECK (actor_type_code ~ '^[a-z][a-z0-9_.-]{1,127}$'),
    CONSTRAINT city_open_world_actor_name_check CHECK (char_length(name) BETWEEN 1 AND 96 AND name = btrim(name)),
    CONSTRAINT city_open_world_actor_status_check CHECK (status ~ '^[a-z][a-z0-9_.-]{1,23}$'),
    CONSTRAINT city_open_world_actor_archetype_check CHECK (
        (archetype_code IS NULL AND archetype_version IS NULL)
        OR (archetype_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
            AND archetype_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$')
    ),
    CONSTRAINT city_open_world_actor_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_actors_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_actors_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_actors_owner
    ON city_open_world_actors (world_id, owner_user_id, status, code);

CREATE TABLE IF NOT EXISTS city_open_world_actor_attributes (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
    actor_id BIGINT NOT NULL,
    attribute_code VARCHAR(128) NOT NULL,
    value_units BIGINT NOT NULL,
    experience_units BIGINT NOT NULL DEFAULT 0 CHECK (experience_units >= 0),
    last_changed_tick BIGINT NOT NULL CHECK (last_changed_tick >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_actor_attribute_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_actor_attribute_code_check
        CHECK (attribute_code ~ '^[a-z][a-z0-9_.-]{1,127}$'),
    CONSTRAINT city_open_world_actor_attribute_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_actor_attributes_identity_unique
        UNIQUE (world_id, actor_id, attribute_code)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_actor_attributes_actor
    ON city_open_world_actor_attributes (world_id, actor_id, attribute_code);

CREATE TABLE IF NOT EXISTS city_open_world_actor_roles (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
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
    CONSTRAINT city_open_world_actor_role_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_actor_role_code_check CHECK (
        role_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND category_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
    ),
    CONSTRAINT city_open_world_actor_role_lifecycle_check CHECK (
        (status = 'active' AND revoked_tick IS NULL)
        OR (status = 'revoked' AND revoked_tick IS NOT NULL AND revoked_tick >= granted_tick)
    ),
    CONSTRAINT city_open_world_actor_role_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_actor_roles_identity_unique
        UNIQUE (world_id, actor_id, role_code, granted_tick)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_open_world_actor_roles_active_category
    ON city_open_world_actor_roles (world_id, actor_id, category_code)
    WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_city_open_world_actor_roles_history
    ON city_open_world_actor_roles (world_id, actor_id, category_code, granted_tick, role_code);

CREATE TABLE IF NOT EXISTS city_open_world_actor_statuses (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
    actor_id BIGINT NOT NULL,
    instance_code VARCHAR(160) NOT NULL,
    status_code VARCHAR(128) NOT NULL,
    lifecycle_status VARCHAR(16) NOT NULL CHECK (lifecycle_status IN ('active', 'revoked', 'expired')),
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
    CONSTRAINT city_open_world_actor_status_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_actor_status_identity_check CHECK (
        instance_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND status_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
    ),
    CONSTRAINT city_open_world_actor_status_lifecycle_check CHECK (
        (lifecycle_status = 'active' AND ended_tick IS NULL
            AND (expires_tick IS NULL OR expires_tick > granted_tick))
        OR (lifecycle_status IN ('revoked', 'expired') AND ended_tick IS NOT NULL
            AND ended_tick >= granted_tick)
    ),
    CONSTRAINT city_open_world_actor_status_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_actor_statuses_world_instance_unique UNIQUE (world_id, instance_code)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_open_world_actor_statuses_active_code
    ON city_open_world_actor_statuses (world_id, actor_id, status_code)
    WHERE lifecycle_status = 'active';
CREATE INDEX IF NOT EXISTS idx_city_open_world_actor_statuses_expiration
    ON city_open_world_actor_statuses (world_id, expires_tick, actor_id)
    WHERE lifecycle_status = 'active' AND expires_tick IS NOT NULL;

CREATE TABLE IF NOT EXISTS city_open_world_runtime_facts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
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
    CONSTRAINT city_open_world_runtime_fact_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_runtime_fact_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_runtime_fact_parent_fk
        FOREIGN KEY (parent_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_runtime_fact_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_runtime_fact_type_check CHECK (fact_type ~ '^[a-z][a-z0-9_.-]{1,127}$'),
    CONSTRAINT city_open_world_runtime_fact_definition_check CHECK (
        (definition_kind IS NULL AND definition_code IS NULL
            AND definition_version IS NULL AND definition_hash IS NULL)
        OR (definition_kind ~ '^[a-z][a-z0-9_]{1,31}$'
            AND definition_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
            AND definition_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
            AND definition_hash ~ '^[0-9a-f]{64}$')
    ),
    CONSTRAINT city_open_world_runtime_fact_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT city_open_world_runtime_fact_posted_check CHECK (posted_at IS NULL OR posted_at >= created_at),
    CONSTRAINT city_open_world_runtime_facts_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_open_world_runtime_facts_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_open_world_runtime_facts_root_command
    ON city_open_world_runtime_facts (source_command_id)
    WHERE source_command_id IS NOT NULL AND parent_fact_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_city_open_world_runtime_facts_actor_history
    ON city_open_world_runtime_facts (world_id, actor_id, tick, sequence);
CREATE INDEX IF NOT EXISTS idx_city_open_world_runtime_facts_definition_history
    ON city_open_world_runtime_facts (world_id, definition_kind, definition_code, tick, sequence);

ALTER TABLE city_open_world_actor_statuses
    ADD CONSTRAINT city_open_world_actor_status_source_fact_fk
    FOREIGN KEY (source_fact_id, world_id)
    REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE IF NOT EXISTS city_open_world_actor_controls (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    actor_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    capability VARCHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'revoked')),
    granted_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    granted_tick BIGINT NOT NULL CHECK (granted_tick >= 0),
    revoked_tick BIGINT,
    grant_source_fact_id BIGINT,
    revoke_source_fact_id BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_actor_control_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_actor_control_grant_fact_fk
        FOREIGN KEY (grant_source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_actor_control_revoke_fact_fk
        FOREIGN KEY (revoke_source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_actor_control_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND capability IN ('actor.command', 'actor.control.manage')
    ),
    CONSTRAINT city_open_world_actor_control_lifecycle_check CHECK (
        (status = 'active' AND revoked_tick IS NULL AND revoke_source_fact_id IS NULL)
        OR (status = 'revoked' AND revoked_tick IS NOT NULL
            AND revoked_tick >= granted_tick AND revoke_source_fact_id IS NOT NULL)
    ),
    CONSTRAINT city_open_world_actor_control_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_actor_controls_world_code_unique UNIQUE (world_id, code)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_open_world_actor_controls_active
    ON city_open_world_actor_controls (world_id, actor_id, user_id, capability)
    WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_city_open_world_actor_controls_authorization
    ON city_open_world_actor_controls (world_id, user_id, capability, status, actor_id);

CREATE TABLE IF NOT EXISTS city_open_world_actor_locations (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
    actor_id BIGINT NOT NULL,
    space_kind VARCHAR(16) NOT NULL,
    location_scope VARCHAR(128) NOT NULL,
    building_code VARCHAR(96),
    floor_index INTEGER NOT NULL DEFAULT 0,
    x BIGINT NOT NULL,
    y BIGINT NOT NULL,
    z SMALLINT NOT NULL CHECK (z BETWEEN 0 AND 127),
    sector_x BIGINT NOT NULL,
    sector_y BIGINT NOT NULL,
    chunk_x BIGINT NOT NULL,
    chunk_y BIGINT NOT NULL,
    local_x SMALLINT NOT NULL,
    local_y SMALLINT NOT NULL,
    moved_tick BIGINT NOT NULL CHECK (moved_tick >= 0),
    source_fact_id BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_actor_location_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_actor_location_building_fk
        FOREIGN KEY (world_id, building_code)
        REFERENCES city_open_world_buildings(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_actor_location_interior_fk
        FOREIGN KEY (world_id, building_code, floor_index)
        REFERENCES city_open_world_building_interiors(world_id, building_code, floor_index) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_actor_location_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_actor_location_space_check CHECK (
        (space_kind = 'surface' AND location_scope = 'surface' AND building_code IS NULL
            AND floor_index = 0 AND z = 0)
        OR (space_kind = 'interior' AND building_code IS NOT NULL
            AND location_scope = building_code AND floor_index >= 0 AND z = floor_index)
    ),
    CONSTRAINT city_open_world_actor_location_local_check CHECK (
        local_x BETWEEN 0 AND 31 AND local_y BETWEEN 0 AND 31
    ),
    CONSTRAINT city_open_world_actor_location_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_actor_locations_world_actor_unique UNIQUE (world_id, actor_id),
    CONSTRAINT city_open_world_actor_locations_cell_occupancy_unique
        UNIQUE (world_id, space_kind, location_scope, floor_index, x, y, z)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_actor_locations_sector
    ON city_open_world_actor_locations (world_id, sector_x, sector_y, space_kind, actor_id);
CREATE INDEX IF NOT EXISTS idx_city_open_world_actor_locations_building
    ON city_open_world_actor_locations (world_id, building_code, floor_index, actor_id)
    WHERE building_code IS NOT NULL;

CREATE TABLE IF NOT EXISTS city_open_world_portal_states (
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
    portal_code VARCHAR(128) NOT NULL,
    state_code VARCHAR(16) NOT NULL CHECK (state_code IN ('open', 'closed', 'locked')),
    access_requirement JSONB NOT NULL DEFAULT '{"operator":"all","items":[]}'::jsonb,
    access_policy_hash VARCHAR(64) NOT NULL,
    changed_tick BIGINT NOT NULL CHECK (changed_tick >= 0),
    source_fact_id BIGINT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, portal_code),
    CONSTRAINT city_open_world_portal_state_portal_fk
        FOREIGN KEY (world_id, portal_code)
        REFERENCES city_open_world_portals(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_portal_state_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_portal_state_requirement_check CHECK (jsonb_typeof(access_requirement) = 'object'),
    CONSTRAINT city_open_world_portal_state_hash_check CHECK (access_policy_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT city_open_world_portal_state_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_runtime_effects (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    source_fact_id BIGINT NOT NULL,
    operation_index INTEGER NOT NULL CHECK (operation_index > 0),
    effect_type VARCHAR(128) NOT NULL,
    target_actor_id BIGINT,
    target_key VARCHAR(192),
    before_units BIGINT,
    delta_units BIGINT,
    after_units BIGINT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_runtime_effect_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_runtime_effect_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_runtime_effect_actor_fk
        FOREIGN KEY (target_actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_runtime_effect_identity_unique
        UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_open_world_runtime_effect_source_index_unique
        UNIQUE (source_fact_id, operation_index),
    CONSTRAINT city_open_world_runtime_effect_type_check CHECK (effect_type ~ '^[a-z][a-z0-9_.-]{1,127}$'),
    CONSTRAINT city_open_world_runtime_effect_payload_check CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_runtime_effects_actor
    ON city_open_world_runtime_effects (world_id, target_actor_id, tick, sequence);

CREATE TABLE IF NOT EXISTS city_open_world_rule_cases (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_open_world_runtime_profiles(world_id) ON DELETE RESTRICT,
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
    scope_kind VARCHAR(32) NOT NULL,
    scope_code VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('open', 'decided', 'closed')),
    severity_units BIGINT NOT NULL CHECK (severity_units >= 0),
    decision_code VARCHAR(128),
    created_tick BIGINT NOT NULL CHECK (created_tick >= 0),
    decided_tick BIGINT,
    closed_tick BIGINT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_rule_case_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_rule_case_consequence_fact_fk
        FOREIGN KEY (consequence_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_rule_case_actor_fk
        FOREIGN KEY (subject_actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_rule_case_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND rule_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND rule_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND rule_hash ~ '^[0-9a-f]{64}$'
        AND category_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND scope_kind ~ '^[a-z][a-z0-9_]{1,31}$'
        AND scope_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
    ),
    CONSTRAINT city_open_world_rule_case_lifecycle_check CHECK (
        (status = 'open' AND decision_code IS NULL AND decided_tick IS NULL AND closed_tick IS NULL)
        OR (status = 'decided' AND decision_code IS NOT NULL AND decided_tick IS NOT NULL AND closed_tick IS NULL)
        OR (status = 'closed' AND decision_code IS NOT NULL AND decided_tick IS NOT NULL AND closed_tick IS NOT NULL
            AND closed_tick >= decided_tick)
    ),
    CONSTRAINT city_open_world_rule_case_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT city_open_world_rule_cases_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_rule_cases_world_tick_sequence_unique UNIQUE (world_id, tick, sequence)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_rule_cases_subject
    ON city_open_world_rule_cases (world_id, subject_actor_id, status, tick, sequence);

CREATE OR REPLACE FUNCTION city_open_world_runtime_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v4'
              AND world.current_tick = 0
              AND world.state_hash IS NULL
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_fact_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v4'
              AND world.current_tick >= 0
              AND world.state_hash IS NOT NULL
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_definition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF TG_OP = 'INSERT' AND city_open_world_runtime_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world runtime definitions are immutable outside genesis'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id)
       OR city_open_world_runtime_bootstrap_write_enabled(target_world_id)
       OR city_open_world_runtime_fact_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world runtime projection requires bootstrap, draft fact, or recovery context'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_fact_insert()
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
    IF world_version IS DISTINCT FROM 'city-openworld-v4'
       OR NEW.tick IS DISTINCT FROM world_tick + 1
       OR NEW.posted_at IS NOT NULL THEN
        RAISE EXCEPTION 'open-world runtime fact must be a draft for the next V4 tick'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.source_command_id IS NOT NULL THEN
        SELECT status INTO command_status_value
        FROM city_commands
        WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
        IF command_status_value IS DISTINCT FROM 'pending' THEN
            RAISE EXCEPTION 'open-world runtime fact requires a pending source command'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF NEW.source_command_id IS NULL AND NEW.parent_fact_id IS NULL THEN
        RAISE EXCEPTION 'derived open-world runtime fact requires a parent fact'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_fact_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(OLD.world_id) THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'open-world runtime facts are immutable' USING ERRCODE = '55000';
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
        RAISE EXCEPTION 'open-world runtime facts permit only one draft-to-posted transition'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_effect_immutable()
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
    IF TG_OP = 'INSERT' AND city_open_world_runtime_fact_write_enabled(target_world_id) THEN
        IF NEW.source_fact_id IS NULL OR NOT EXISTS (
            SELECT 1 FROM city_open_world_runtime_facts fact
            WHERE fact.id = NEW.source_fact_id AND fact.world_id = NEW.world_id
              AND fact.posted_at IS NULL
        ) THEN
            RAISE EXCEPTION 'open-world runtime effect requires a draft source fact'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world runtime effects are immutable outside a draft fact'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_runtime_profile_guard ON city_open_world_runtime_profiles;
CREATE TRIGGER city_open_world_runtime_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_runtime_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_projection();

DROP TRIGGER IF EXISTS city_open_world_runtime_definition_guard ON city_open_world_runtime_definitions;
CREATE TRIGGER city_open_world_runtime_definition_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_runtime_definitions
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_definition();

DROP TRIGGER IF EXISTS city_open_world_actor_guard ON city_open_world_actors;
CREATE TRIGGER city_open_world_actor_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_actors
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_projection();

DROP TRIGGER IF EXISTS city_open_world_actor_attribute_guard ON city_open_world_actor_attributes;
CREATE TRIGGER city_open_world_actor_attribute_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_actor_attributes
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_projection();

DROP TRIGGER IF EXISTS city_open_world_actor_role_guard ON city_open_world_actor_roles;
CREATE TRIGGER city_open_world_actor_role_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_actor_roles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_projection();

DROP TRIGGER IF EXISTS city_open_world_actor_status_guard ON city_open_world_actor_statuses;
CREATE TRIGGER city_open_world_actor_status_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_actor_statuses
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_projection();

DROP TRIGGER IF EXISTS city_open_world_actor_control_guard ON city_open_world_actor_controls;
CREATE TRIGGER city_open_world_actor_control_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_actor_controls
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_projection();

DROP TRIGGER IF EXISTS city_open_world_actor_location_guard ON city_open_world_actor_locations;
CREATE TRIGGER city_open_world_actor_location_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_actor_locations
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_projection();

DROP TRIGGER IF EXISTS city_open_world_portal_state_guard ON city_open_world_portal_states;
CREATE TRIGGER city_open_world_portal_state_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_portal_states
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_projection();

DROP TRIGGER IF EXISTS city_open_world_runtime_fact_insert_guard ON city_open_world_runtime_facts;
CREATE TRIGGER city_open_world_runtime_fact_insert_guard
BEFORE INSERT ON city_open_world_runtime_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_fact_insert();

DROP TRIGGER IF EXISTS city_open_world_runtime_fact_immutable_guard ON city_open_world_runtime_facts;
CREATE TRIGGER city_open_world_runtime_fact_immutable_guard
BEFORE UPDATE OR DELETE ON city_open_world_runtime_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_fact_immutable();

DROP TRIGGER IF EXISTS city_open_world_runtime_effect_guard ON city_open_world_runtime_effects;
CREATE TRIGGER city_open_world_runtime_effect_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_runtime_effects
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_effect_immutable();

DROP TRIGGER IF EXISTS city_open_world_rule_case_guard ON city_open_world_rule_cases;
CREATE TRIGGER city_open_world_rule_case_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_rule_cases
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_runtime_projection();
