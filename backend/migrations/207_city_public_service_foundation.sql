-- F8.0 generic public-facility, service-capacity, demand, connection, and settlement foundation.

CREATE TABLE IF NOT EXISTS city_service_profiles (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    catalog_id VARCHAR(64) NOT NULL,
    catalog_version VARCHAR(24) NOT NULL,
    catalog_hash VARCHAR(64) NOT NULL,
    settlement_version VARCHAR(24) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    service_definition_count BIGINT NOT NULL CHECK (service_definition_count >= 0),
    facility_type_count BIGINT NOT NULL CHECK (facility_type_count >= 0),
    facility_count BIGINT NOT NULL DEFAULT 0 CHECK (facility_count BETWEEN 0 AND 10000),
    capacity_count BIGINT NOT NULL DEFAULT 0 CHECK (capacity_count BETWEEN 0 AND 10000),
    demand_count BIGINT NOT NULL DEFAULT 0 CHECK (demand_count BETWEEN 0 AND 10000),
    connection_count BIGINT NOT NULL DEFAULT 0 CHECK (connection_count BETWEEN 0 AND 10000),
    fact_count BIGINT NOT NULL DEFAULT 0 CHECK (fact_count >= 0),
    allocation_count BIGINT NOT NULL DEFAULT 0 CHECK (allocation_count >= 0),
    settlement_count BIGINT NOT NULL DEFAULT 0 CHECK (settlement_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_service_profile_identity_check CHECK (
        catalog_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND catalog_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND settlement_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND catalog_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_service_profile_revision_check CHECK (revision = fact_count + 1),
    CONSTRAINT city_service_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_service_definitions (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(96) NOT NULL,
    definition_version VARCHAR(24) NOT NULL,
    definition_hash VARCHAR(64) NOT NULL,
    name VARCHAR(96) NOT NULL,
    category VARCHAR(32) NOT NULL,
    unit_code VARCHAR(64) NOT NULL,
    flow_kind VARCHAR(16) NOT NULL CHECK (flow_kind IN ('delivery', 'collection', 'capacity')),
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'retired')),
    sort_order INTEGER NOT NULL DEFAULT 0,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_service_definition_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND definition_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND definition_hash ~ '^[0-9a-f]{64}$'
        AND category ~ '^[a-z][a-z0-9_]{1,31}$'
        AND unit_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND char_length(name) BETWEEN 1 AND 96 AND name = btrim(name)
    ),
    CONSTRAINT city_service_definition_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT city_service_definitions_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_service_definitions_id_world_unique UNIQUE (id, world_id)
);

CREATE TABLE IF NOT EXISTS city_facility_type_definitions (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(96) NOT NULL,
    definition_version VARCHAR(24) NOT NULL,
    definition_hash VARCHAR(64) NOT NULL,
    name VARCHAR(96) NOT NULL,
    minimum_floor_area_sqm BIGINT NOT NULL DEFAULT 1 CHECK (minimum_floor_area_sqm > 0),
    default_reliability_milli INTEGER NOT NULL DEFAULT 1000
        CHECK (default_reliability_milli BETWEEN 0 AND 1000),
    allowed_service_codes JSONB NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'retired')),
    sort_order INTEGER NOT NULL DEFAULT 0,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_facility_type_definition_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND definition_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND definition_hash ~ '^[0-9a-f]{64}$'
        AND char_length(name) BETWEEN 1 AND 96 AND name = btrim(name)
    ),
    CONSTRAINT city_facility_type_definition_services_check CHECK (
        jsonb_typeof(allowed_service_codes) = 'array'
        AND jsonb_array_length(allowed_service_codes) > 0
    ),
    CONSTRAINT city_facility_type_definition_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT city_facility_type_definitions_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_facility_type_definitions_id_world_unique UNIQUE (id, world_id)
);

CREATE TABLE IF NOT EXISTS city_service_facts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    source_command_id BIGINT,
    fact_type VARCHAR(48) NOT NULL CHECK (fact_type IN (
        'facility.registered', 'facility.status.changed',
        'facility.capacity.configured', 'service.demand.configured',
        'service.connection.configured', 'service.allocation.settled'
    )),
    subject_kind VARCHAR(24) NOT NULL CHECK (subject_kind IN (
        'facility', 'capacity', 'demand', 'connection', 'settlement'
    )),
    subject_code VARCHAR(192) NOT NULL,
    version_before BIGINT NOT NULL CHECK (version_before >= 0),
    version_after BIGINT NOT NULL CHECK (version_after >= 0),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted_at TIMESTAMPTZ,
    CONSTRAINT city_service_fact_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_service_fact_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_service_fact_subject_code_check
        CHECK (subject_code ~ '^[a-z][a-z0-9_.-]{1,191}$'),
    CONSTRAINT city_service_fact_version_check CHECK (
        (fact_type = 'service.allocation.settled'
            AND source_command_id IS NULL AND subject_kind = 'settlement'
            AND version_before = 0 AND version_after = 0)
        OR (fact_type <> 'service.allocation.settled'
            AND source_command_id IS NOT NULL
            AND version_after = version_before + 1)
    ),
    CONSTRAINT city_service_fact_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT city_service_fact_posted_check CHECK (posted_at IS NULL OR posted_at >= created_at),
    CONSTRAINT city_service_facts_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_service_facts_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_service_facts_one_per_command
    ON city_service_facts (source_command_id) WHERE source_command_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_city_service_facts_subject_history
    ON city_service_facts (world_id, subject_kind, subject_code, tick, sequence);

CREATE TABLE IF NOT EXISTS city_facilities (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(96) NOT NULL,
    name VARCHAR(96) NOT NULL,
    facility_type_id BIGINT NOT NULL,
    district_id BIGINT NOT NULL,
    building_id BIGINT NOT NULL,
    owner_entity_id BIGINT,
    status VARCHAR(16) NOT NULL CHECK (status IN ('offline', 'operational', 'degraded', 'retired')),
    reliability_milli INTEGER NOT NULL CHECK (reliability_milli BETWEEN 0 AND 1000),
    created_tick BIGINT NOT NULL CHECK (created_tick > 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= created_tick),
    version BIGINT NOT NULL CHECK (version > 0),
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_facility_type_fk
        FOREIGN KEY (facility_type_id, world_id)
        REFERENCES city_facility_type_definitions(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_building_fk
        FOREIGN KEY (building_id, world_id)
        REFERENCES city_buildings(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_owner_fk
        FOREIGN KEY (owner_entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_service_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_facility_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND char_length(name) BETWEEN 1 AND 96 AND name = btrim(name)
    ),
    CONSTRAINT city_facility_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_facilities_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_facilities_world_building_unique UNIQUE (world_id, building_id),
    CONSTRAINT city_facilities_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_facilities_world_status
    ON city_facilities (world_id, status, district_id, code);

CREATE TABLE IF NOT EXISTS city_facility_service_capacities (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    facility_id BIGINT NOT NULL,
    service_definition_id BIGINT NOT NULL,
    installed_capacity_units BIGINT NOT NULL
        CHECK (installed_capacity_units BETWEEN 1 AND 922337203685477),
    availability_milli INTEGER NOT NULL CHECK (availability_milli BETWEEN 0 AND 1000),
    available_capacity_units BIGINT NOT NULL CHECK (available_capacity_units >= 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick > 0),
    version BIGINT NOT NULL CHECK (version > 0),
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_facility_service_capacity_facility_fk
        FOREIGN KEY (facility_id, world_id)
        REFERENCES city_facilities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_service_capacity_service_fk
        FOREIGN KEY (service_definition_id, world_id)
        REFERENCES city_service_definitions(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_facility_service_capacity_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_service_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_facility_service_capacity_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_facility_service_capacities_identity_unique
        UNIQUE (world_id, facility_id, service_definition_id),
    CONSTRAINT city_facility_service_capacities_id_world_unique UNIQUE (id, world_id)
);

CREATE TABLE IF NOT EXISTS city_service_demands (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(96) NOT NULL,
    service_definition_id BIGINT NOT NULL,
    subject_kind VARCHAR(16) NOT NULL CHECK (subject_kind IN (
        'district', 'building', 'household', 'enterprise', 'actor'
    )),
    subject_code VARCHAR(128) NOT NULL,
    district_id BIGINT NOT NULL,
    building_id BIGINT,
    entity_id BIGINT,
    actor_id BIGINT,
    requested_units_per_tick BIGINT NOT NULL
        CHECK (requested_units_per_tick BETWEEN 0 AND 922337203685477),
    priority INTEGER NOT NULL CHECK (priority BETWEEN 0 AND 1000),
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'suspended', 'retired')),
    created_tick BIGINT NOT NULL CHECK (created_tick > 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= created_tick),
    version BIGINT NOT NULL CHECK (version > 0),
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_service_demand_service_fk
        FOREIGN KEY (service_definition_id, world_id)
        REFERENCES city_service_definitions(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_demand_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_demand_building_fk
        FOREIGN KEY (building_id, world_id)
        REFERENCES city_buildings(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_demand_entity_fk
        FOREIGN KEY (entity_id, world_id)
        REFERENCES city_economic_entities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_demand_actor_fk
        FOREIGN KEY (actor_id, world_id)
        REFERENCES world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_demand_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_service_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_service_demand_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND subject_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
    ),
    CONSTRAINT city_service_demand_subject_shape_check CHECK (
        (subject_kind = 'district' AND building_id IS NULL AND entity_id IS NULL AND actor_id IS NULL)
        OR (subject_kind = 'building' AND building_id IS NOT NULL AND entity_id IS NULL AND actor_id IS NULL)
        OR (subject_kind IN ('household', 'enterprise') AND building_id IS NULL AND entity_id IS NOT NULL AND actor_id IS NULL)
        OR (subject_kind = 'actor' AND building_id IS NULL AND entity_id IS NULL AND actor_id IS NOT NULL)
    ),
    CONSTRAINT city_service_demand_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_service_demands_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_service_demands_subject_unique
        UNIQUE (world_id, service_definition_id, subject_kind, subject_code),
    CONSTRAINT city_service_demands_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_service_demands_settlement_order
    ON city_service_demands (world_id, service_definition_id, priority DESC, created_tick, code)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS city_service_connections (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(96) NOT NULL,
    capacity_id BIGINT NOT NULL,
    demand_id BIGINT NOT NULL,
    max_flow_units_per_tick BIGINT NOT NULL
        CHECK (max_flow_units_per_tick BETWEEN 1 AND 922337203685477),
    loss_milli INTEGER NOT NULL CHECK (loss_milli BETWEEN 0 AND 999),
    preference INTEGER NOT NULL CHECK (preference BETWEEN 0 AND 1000),
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'suspended', 'retired')),
    created_tick BIGINT NOT NULL CHECK (created_tick > 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= created_tick),
    version BIGINT NOT NULL CHECK (version > 0),
    source_fact_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_service_connection_capacity_fk
        FOREIGN KEY (capacity_id, world_id)
        REFERENCES city_facility_service_capacities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_connection_demand_fk
        FOREIGN KEY (demand_id, world_id)
        REFERENCES city_service_demands(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_connection_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_service_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_service_connection_code_check CHECK (code ~ '^[a-z][a-z0-9_.-]{1,95}$'),
    CONSTRAINT city_service_connection_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_service_connections_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_service_connections_route_unique UNIQUE (world_id, capacity_id, demand_id),
    CONSTRAINT city_service_connections_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_service_connections_dispatch_order
    ON city_service_connections (world_id, demand_id, preference DESC, code)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS city_service_allocations (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    allocation_index INTEGER NOT NULL CHECK (allocation_index > 0),
    source_fact_id BIGINT NOT NULL,
    service_definition_id BIGINT NOT NULL,
    facility_id BIGINT NOT NULL,
    capacity_id BIGINT NOT NULL,
    demand_id BIGINT NOT NULL,
    connection_id BIGINT NOT NULL,
    capacity_version BIGINT NOT NULL CHECK (capacity_version > 0),
    demand_version BIGINT NOT NULL CHECK (demand_version > 0),
    connection_version BIGINT NOT NULL CHECK (connection_version > 0),
    facility_capacity_units BIGINT NOT NULL CHECK (facility_capacity_units > 0),
    connection_capacity_units BIGINT NOT NULL CHECK (connection_capacity_units > 0),
    loss_milli INTEGER NOT NULL CHECK (loss_milli BETWEEN 0 AND 999),
    dispatched_units BIGINT NOT NULL CHECK (dispatched_units > 0),
    delivered_units BIGINT NOT NULL CHECK (delivered_units >= 0),
    loss_units BIGINT NOT NULL CHECK (loss_units >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_service_allocation_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_service_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_allocation_service_fk
        FOREIGN KEY (service_definition_id, world_id)
        REFERENCES city_service_definitions(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_allocation_facility_fk
        FOREIGN KEY (facility_id, world_id)
        REFERENCES city_facilities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_allocation_capacity_fk
        FOREIGN KEY (capacity_id, world_id)
        REFERENCES city_facility_service_capacities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_allocation_demand_fk
        FOREIGN KEY (demand_id, world_id)
        REFERENCES city_service_demands(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_allocation_connection_fk
        FOREIGN KEY (connection_id, world_id)
        REFERENCES city_service_connections(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_allocation_units_check CHECK (
        delivered_units + loss_units = dispatched_units
        AND dispatched_units <= facility_capacity_units
        AND dispatched_units <= connection_capacity_units
        AND delivered_units = FLOOR(
            dispatched_units::NUMERIC * (1000 - loss_milli) / 1000
        )::BIGINT
    ),
    CONSTRAINT city_service_allocation_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_service_allocations_fact_index_unique UNIQUE (source_fact_id, allocation_index),
    CONSTRAINT city_service_allocations_world_tick_sequence_index_unique
        UNIQUE (world_id, tick, sequence, allocation_index),
    CONSTRAINT city_service_allocations_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_service_allocations_facility_tick
    ON city_service_allocations (world_id, facility_id, tick, sequence, allocation_index);

CREATE TABLE IF NOT EXISTS city_service_settlements (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    source_fact_id BIGINT NOT NULL,
    service_definition_id BIGINT NOT NULL,
    demand_id BIGINT NOT NULL,
    demand_version BIGINT NOT NULL CHECK (demand_version > 0),
    requested_units BIGINT NOT NULL CHECK (requested_units >= 0),
    delivered_units BIGINT NOT NULL CHECK (delivered_units >= 0),
    shortage_units BIGINT NOT NULL CHECK (shortage_units >= 0),
    allocation_count INTEGER NOT NULL CHECK (allocation_count >= 0),
    quality_milli INTEGER NOT NULL CHECK (quality_milli BETWEEN 0 AND 1000),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_service_settlement_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_service_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_settlement_service_fk
        FOREIGN KEY (service_definition_id, world_id)
        REFERENCES city_service_definitions(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_settlement_demand_fk
        FOREIGN KEY (demand_id, world_id)
        REFERENCES city_service_demands(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_service_settlement_balance_check CHECK (
        delivered_units + shortage_units = requested_units
        AND quality_milli = CASE WHEN requested_units = 0 THEN 1000
            ELSE FLOOR(delivered_units::NUMERIC * 1000 / requested_units)::INTEGER END
    ),
    CONSTRAINT city_service_settlement_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_service_settlements_fact_unique UNIQUE (source_fact_id),
    CONSTRAINT city_service_settlements_demand_tick_unique UNIQUE (world_id, demand_id, tick),
    CONSTRAINT city_service_settlements_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_service_settlements_id_world_unique UNIQUE (id, world_id)
);

-- Extend every frozen predecessor foundation only by the explicit F8.0 compatibility set.
CREATE OR REPLACE FUNCTION migration_207_replace_function(
    target REGPROCEDURE,
    needle TEXT,
    replacement TEXT
)
RETURNS VOID
LANGUAGE plpgsql
AS $migration$
DECLARE
    definition TEXT;
    patched TEXT;
BEGIN
    SELECT pg_get_functiondef(target) INTO definition;
    patched := replace(definition, needle, replacement);
    IF patched = definition THEN
        IF POSITION(replacement IN definition) > 0 THEN
            RETURN;
        END IF;
        RAISE EXCEPTION 'migration 207 predecessor function % does not contain expected text', target
            USING ERRCODE = '23514';
    END IF;
    BEGIN
        EXECUTE patched;
    EXCEPTION WHEN OTHERS THEN
        RAISE EXCEPTION 'migration 207 failed to extend predecessor function % at %: %',
            target, needle, SQLERRM USING ERRCODE = SQLSTATE;
    END;
END;
$migration$;

SELECT migration_207_replace_function(
    'world_runtime_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
    $$world.simulation_version IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$,
    $$world.simulation_version IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9', 'city-f8-v1')$$
);
SELECT migration_207_replace_function(
    'guard_world_runtime_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9', 'city-f8-v1')$$
);
SELECT migration_207_replace_function(
    'assert_world_runtime_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9', 'city-f8-v1')$$
);
SELECT migration_207_replace_function(
    'assert_world_runtime_foundation(bigint)'::REGPROCEDURE,
    $$CASE WHEN world_version = 'city-f7-v9' THEN '1.3.0' WHEN world_version = 'city-f7-v8' THEN '1.2.0' WHEN world_version IN ('city-f7-v6', 'city-f7-v7') THEN '1.1.0' ELSE '1.0.0' END$$,
    $$CASE WHEN world_version IN ('city-f7-v9', 'city-f8-v1') THEN '1.3.0' WHEN world_version = 'city-f7-v8' THEN '1.2.0' WHEN world_version IN ('city-f7-v6', 'city-f7-v7') THEN '1.1.0' ELSE '1.0.0' END$$
);
SELECT migration_207_replace_function(
    'guard_city_enterprise_location_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9', 'city-f8-v1')$$
);
SELECT migration_207_replace_function(
    'assert_city_enterprise_location_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9', 'city-f8-v1')$$
);
SELECT migration_207_replace_function(
    'guard_city_development_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9', 'city-f8-v1')$$
);
SELECT migration_207_replace_function(
    'assert_city_development_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9', 'city-f8-v1')$$
);
SELECT migration_207_replace_function(
    'assert_city_land_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$,
    $$world_version NOT IN ('city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9', 'city-f8-v1')$$
);
SELECT migration_207_replace_function(
    'guard_city_spatial_mutation_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$,
    $$world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9', 'city-f8-v1')$$
);
SELECT migration_207_replace_function(
    'assert_city_spatial_foundation(bigint)'::REGPROCEDURE,
    $$world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9')$$,
    $$world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9', 'city-f8-v1')$$
);
SELECT migration_207_replace_function(
    'assert_world_actor_spatial_control_foundation(bigint)'::REGPROCEDURE,
    $$IF world_version NOT IN ('city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9') THEN$$,
    $$IF world_version NOT IN ('city-f7-v6', 'city-f7-v7', 'city-f7-v8', 'city-f7-v9', 'city-f8-v1') THEN$$
);
SELECT migration_207_replace_function(
    'assert_world_actor_spatial_control_foundation(bigint)'::REGPROCEDURE,
    $$AND runtime_version = CASE WHEN world_version = 'city-f7-v9' THEN '1.3.0' WHEN world_version = 'city-f7-v8' THEN '1.2.0' ELSE '1.1.0' END$$,
    $$AND runtime_version = CASE WHEN world_version IN ('city-f7-v9', 'city-f8-v1') THEN '1.3.0' WHEN world_version = 'city-f7-v8' THEN '1.2.0' ELSE '1.1.0' END$$
);
SELECT migration_207_replace_function(
    'assert_world_portal_access_foundation(bigint)'::REGPROCEDURE,
    $$IF world_version NOT IN ('city-f7-v8', 'city-f7-v9') THEN$$,
    $$IF world_version NOT IN ('city-f7-v8', 'city-f7-v9', 'city-f8-v1') THEN$$
);
SELECT migration_207_replace_function(
    'assert_world_portal_access_foundation(bigint)'::REGPROCEDURE,
    $$AND runtime_version = CASE WHEN world_version = 'city-f7-v9' THEN '1.3.0' ELSE '1.2.0' END$$,
    $$AND runtime_version = CASE WHEN world_version IN ('city-f7-v9', 'city-f8-v1') THEN '1.3.0' ELSE '1.2.0' END$$
);
SELECT migration_207_replace_function(
    'assert_world_navigation_intent_foundation(bigint)'::REGPROCEDURE,
    $$IF world_version <> 'city-f7-v9' THEN$$,
    $$IF world_version NOT IN ('city-f7-v9', 'city-f8-v1') THEN$$
);

DROP FUNCTION migration_207_replace_function(REGPROCEDURE, TEXT, TEXT);

CREATE OR REPLACE FUNCTION city_service_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_service_bootstrap_world_id', TRUE), '')
               = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND ((world.simulation_version = 'city-f8-v1' AND world.current_tick = 0)
                   OR city_engine_upgrade_write_enabled(target_world_id))
       )
$$;

CREATE OR REPLACE FUNCTION city_service_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_service_facts fact
        WHERE fact.id = CASE
            WHEN COALESCE(current_setting('sub2api.city_service_fact_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.city_service_fact_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND fact.world_id = target_world_id
          AND fact.posted_at IS NULL
    )
$$;

CREATE OR REPLACE FUNCTION guard_city_service_catalog_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id)
       OR city_service_bootstrap_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city service catalog is immutable outside bootstrap or recovery'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_service_definition_immutable_guard ON city_service_definitions;
CREATE TRIGGER city_service_definition_immutable_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_service_definitions
FOR EACH ROW EXECUTE FUNCTION guard_city_service_catalog_immutable();

DROP TRIGGER IF EXISTS city_facility_type_definition_immutable_guard ON city_facility_type_definitions;
CREATE TRIGGER city_facility_type_definition_immutable_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_facility_type_definitions
FOR EACH ROW EXECUTE FUNCTION guard_city_service_catalog_immutable();

CREATE OR REPLACE FUNCTION guard_city_service_profile_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_service_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_service_fact_write_enabled(target_world_id)
       AND (NEW.world_id, NEW.catalog_id, NEW.catalog_version, NEW.catalog_hash,
            NEW.settlement_version, NEW.baseline_tick, NEW.service_definition_count,
            NEW.facility_type_count, NEW.created_at)
           IS NOT DISTINCT FROM
           (OLD.world_id, OLD.catalog_id, OLD.catalog_version, OLD.catalog_hash,
            OLD.settlement_version, OLD.baseline_tick, OLD.service_definition_count,
            OLD.facility_type_count, OLD.created_at)
       AND NEW.fact_count = OLD.fact_count + 1
       AND NEW.revision = OLD.revision + 1
       AND NEW.facility_count >= OLD.facility_count
       AND NEW.capacity_count >= OLD.capacity_count
       AND NEW.demand_count >= OLD.demand_count
       AND NEW.connection_count >= OLD.connection_count
       AND NEW.allocation_count >= OLD.allocation_count
       AND NEW.settlement_count >= OLD.settlement_count THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city service profile requires bootstrap, draft fact, or recovery context'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_service_profile_projection_guard ON city_service_profiles;
CREATE TRIGGER city_service_profile_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_service_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_service_profile_projection();

CREATE OR REPLACE FUNCTION guard_city_service_fact_insert()
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
    IF world_version IS DISTINCT FROM 'city-f8-v1' OR NEW.tick IS DISTINCT FROM world_tick + 1 THEN
        RAISE EXCEPTION 'city service fact must target the next F8.0 tick'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.source_command_id IS NULL THEN
        IF NEW.fact_type <> 'service.allocation.settled'
           OR COALESCE(current_setting('sub2api.city_service_auto_world_id', TRUE), '') <> NEW.world_id::TEXT THEN
            RAISE EXCEPTION 'automatic city service fact is not authorized'
                USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    SELECT command_type, status INTO command_type_value, command_status_value
    FROM city_commands
    WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
    expected_fact_type := CASE command_type_value
        WHEN 'facility.register' THEN 'facility.registered'
        WHEN 'facility.status.transition' THEN 'facility.status.changed'
        WHEN 'facility.capacity.configure' THEN 'facility.capacity.configured'
        WHEN 'service.demand.configure' THEN 'service.demand.configured'
        WHEN 'service.connection.configure' THEN 'service.connection.configured'
        ELSE NULL
    END;
    IF command_status_value IS DISTINCT FROM 'pending'
       OR expected_fact_type IS DISTINCT FROM NEW.fact_type THEN
        RAISE EXCEPTION 'city service fact does not match its pending source command'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_service_fact_insert_guard ON city_service_facts;
CREATE TRIGGER city_service_fact_insert_guard
BEFORE INSERT ON city_service_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_service_fact_insert();

CREATE OR REPLACE FUNCTION guard_city_service_fact_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(OLD.world_id) THEN RETURN OLD; END IF;
        RAISE EXCEPTION 'city service facts are immutable' USING ERRCODE = '55000';
    END IF;
    IF city_recovery_write_enabled(NEW.world_id) THEN RETURN NEW; END IF;
    IF OLD.posted_at IS NOT NULL OR NEW.posted_at IS NULL
       OR (NEW.id, NEW.world_id, NEW.tick, NEW.sequence, NEW.source_command_id,
           NEW.fact_type, NEW.subject_kind, NEW.subject_code, NEW.version_before,
           NEW.version_after, NEW.payload, NEW.created_at)
          IS DISTINCT FROM
          (OLD.id, OLD.world_id, OLD.tick, OLD.sequence, OLD.source_command_id,
           OLD.fact_type, OLD.subject_kind, OLD.subject_code, OLD.version_before,
           OLD.version_after, OLD.payload, OLD.created_at) THEN
        RAISE EXCEPTION 'city service facts permit only one draft-to-posted transition'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_service_fact_immutable_guard ON city_service_facts;
CREATE TRIGGER city_service_fact_immutable_guard
BEFORE UPDATE OR DELETE ON city_service_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_service_fact_immutable();

CREATE OR REPLACE FUNCTION guard_city_service_versioned_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF NOT city_service_fact_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'city service projection requires a matching draft fact'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'INSERT' AND NEW.version = 1 AND NEW.source_fact_id IS NOT NULL THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND NEW.id IS NOT DISTINCT FROM OLD.id
       AND NEW.world_id IS NOT DISTINCT FROM OLD.world_id
       AND NEW.created_at IS NOT DISTINCT FROM OLD.created_at
       AND NEW.version = OLD.version + 1
       AND NEW.updated_tick >= OLD.updated_tick
       AND NEW.source_fact_id IS DISTINCT FROM OLD.source_fact_id THEN
        -- A trigger RECORD only exposes fields from its current table. Reading
        -- NEW.facility_id in a branch for city_facilities still raises at
        -- runtime, even when that branch is not selected. Compare identity
        -- fields through JSONB so this shared trigger remains table-safe.
        IF TG_TABLE_NAME = 'city_facilities'
           AND jsonb_build_array(
                   to_jsonb(NEW)->'code', to_jsonb(NEW)->'facility_type_id',
                   to_jsonb(NEW)->'district_id', to_jsonb(NEW)->'building_id',
                   to_jsonb(NEW)->'owner_entity_id', to_jsonb(NEW)->'created_tick')
               IS DISTINCT FROM jsonb_build_array(
                   to_jsonb(OLD)->'code', to_jsonb(OLD)->'facility_type_id',
                   to_jsonb(OLD)->'district_id', to_jsonb(OLD)->'building_id',
                   to_jsonb(OLD)->'owner_entity_id', to_jsonb(OLD)->'created_tick') THEN
            RAISE EXCEPTION 'city facility identity is immutable' USING ERRCODE = '55000';
        ELSIF TG_TABLE_NAME = 'city_facility_service_capacities'
           AND jsonb_build_array(
                   to_jsonb(NEW)->'facility_id', to_jsonb(NEW)->'service_definition_id')
               IS DISTINCT FROM jsonb_build_array(
                   to_jsonb(OLD)->'facility_id', to_jsonb(OLD)->'service_definition_id') THEN
            RAISE EXCEPTION 'city service capacity identity is immutable' USING ERRCODE = '55000';
        ELSIF TG_TABLE_NAME = 'city_service_demands'
           AND jsonb_build_array(
                   to_jsonb(NEW)->'code', to_jsonb(NEW)->'service_definition_id',
                   to_jsonb(NEW)->'subject_kind', to_jsonb(NEW)->'subject_code',
                   to_jsonb(NEW)->'district_id', to_jsonb(NEW)->'building_id',
                   to_jsonb(NEW)->'entity_id', to_jsonb(NEW)->'actor_id',
                   to_jsonb(NEW)->'created_tick')
               IS DISTINCT FROM jsonb_build_array(
                   to_jsonb(OLD)->'code', to_jsonb(OLD)->'service_definition_id',
                   to_jsonb(OLD)->'subject_kind', to_jsonb(OLD)->'subject_code',
                   to_jsonb(OLD)->'district_id', to_jsonb(OLD)->'building_id',
                   to_jsonb(OLD)->'entity_id', to_jsonb(OLD)->'actor_id',
                   to_jsonb(OLD)->'created_tick') THEN
            RAISE EXCEPTION 'city service demand identity is immutable' USING ERRCODE = '55000';
        ELSIF TG_TABLE_NAME = 'city_service_connections'
           AND jsonb_build_array(
                   to_jsonb(NEW)->'code', to_jsonb(NEW)->'capacity_id',
                   to_jsonb(NEW)->'demand_id', to_jsonb(NEW)->'created_tick')
               IS DISTINCT FROM jsonb_build_array(
                   to_jsonb(OLD)->'code', to_jsonb(OLD)->'capacity_id',
                   to_jsonb(OLD)->'demand_id', to_jsonb(OLD)->'created_tick') THEN
            RAISE EXCEPTION 'city service connection identity is immutable' USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'invalid fact-backed city service projection mutation'
        USING ERRCODE = '55000';
END;
$$;

DO $$
DECLARE
    table_name TEXT;
    trigger_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_facilities', 'city_facility_service_capacities',
        'city_service_demands', 'city_service_connections'
    ] LOOP
        trigger_name := table_name || '_versioned_projection_guard';
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', trigger_name, table_name);
        EXECUTE format(
            'CREATE TRIGGER %I BEFORE INSERT OR UPDATE OR DELETE ON %I '
            || 'FOR EACH ROW EXECUTE FUNCTION guard_city_service_versioned_projection()',
            trigger_name, table_name
        );
    END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_service_settlement_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_service_fact_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city service settlement history is immutable and fact-backed'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_service_allocation_projection_guard ON city_service_allocations;
CREATE TRIGGER city_service_allocation_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_service_allocations
FOR EACH ROW EXECUTE FUNCTION guard_city_service_settlement_projection();

DROP TRIGGER IF EXISTS city_service_settlement_projection_guard ON city_service_settlements;
CREATE TRIGGER city_service_settlement_projection_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_service_settlements
FOR EACH ROW EXECUTE FUNCTION guard_city_service_settlement_projection();

CREATE OR REPLACE FUNCTION assert_city_service_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    profile_row city_service_profiles%ROWTYPE;
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city service world does not exist' USING ERRCODE = '23514';
    END IF;

    IF world_version <> 'city-f8-v1' THEN
        IF EXISTS (SELECT 1 FROM city_service_profiles WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_service_definitions WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_facility_type_definitions WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_service_facts WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_facilities WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_facility_service_capacities WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_service_demands WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_service_connections WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_service_allocations WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_service_settlements WHERE world_id = target_world_id) THEN
            RAISE EXCEPTION 'pre-F8 engine cannot contain public service state'
                USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;

    SELECT * INTO profile_row FROM city_service_profiles WHERE world_id = target_world_id;
    IF NOT FOUND OR profile_row.catalog_id <> 'sub2api-public-services'
       OR profile_row.catalog_version <> '1.0.0'
       OR profile_row.settlement_version <> '1.0.0'
       OR profile_row.baseline_tick > world_tick
       OR profile_row.metadata->>'schema_version' IS DISTINCT FROM '1' THEN
        RAISE EXCEPTION 'city service profile is missing or invalid' USING ERRCODE = '23514';
    END IF;

    IF profile_row.service_definition_count <> (
           SELECT COUNT(*) FROM city_service_definitions WHERE world_id = target_world_id)
       OR profile_row.facility_type_count <> (
           SELECT COUNT(*) FROM city_facility_type_definitions WHERE world_id = target_world_id)
       OR profile_row.facility_count <> (
           SELECT COUNT(*) FROM city_facilities WHERE world_id = target_world_id)
       OR profile_row.capacity_count <> (
           SELECT COUNT(*) FROM city_facility_service_capacities WHERE world_id = target_world_id)
       OR profile_row.demand_count <> (
           SELECT COUNT(*) FROM city_service_demands WHERE world_id = target_world_id)
       OR profile_row.connection_count <> (
           SELECT COUNT(*) FROM city_service_connections WHERE world_id = target_world_id)
       OR profile_row.fact_count <> (
           SELECT COUNT(*) FROM city_service_facts WHERE world_id = target_world_id)
       OR profile_row.allocation_count <> (
           SELECT COUNT(*) FROM city_service_allocations WHERE world_id = target_world_id)
       OR profile_row.settlement_count <> (
           SELECT COUNT(*) FROM city_service_settlements WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city service profile counters are inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_facilities facility
    JOIN city_buildings building
      ON building.id = facility.building_id AND building.world_id = facility.world_id
    JOIN city_districts district
      ON district.id = facility.district_id AND district.world_id = facility.world_id
    JOIN city_facility_type_definitions definition
      ON definition.id = facility.facility_type_id AND definition.world_id = facility.world_id
    JOIN city_service_facts fact
      ON fact.id = facility.source_fact_id AND fact.world_id = facility.world_id
    WHERE facility.world_id = target_world_id
      AND (building.district_id <> facility.district_id OR building.status <> 'active'
           OR definition.status <> 'active' OR facility.updated_tick > world_tick
           OR fact.posted_at IS NULL OR fact.tick <> facility.updated_tick
           OR fact.subject_kind <> 'facility' OR fact.subject_code <> facility.code
           OR fact.version_after <> facility.version);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city facility projection is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_facility_service_capacities capacity
    JOIN city_facilities facility
      ON facility.id = capacity.facility_id AND facility.world_id = capacity.world_id
    JOIN city_service_definitions service
      ON service.id = capacity.service_definition_id AND service.world_id = capacity.world_id
    JOIN city_facility_type_definitions facility_type
      ON facility_type.id = facility.facility_type_id AND facility_type.world_id = facility.world_id
    JOIN city_service_facts fact
      ON fact.id = capacity.source_fact_id AND fact.world_id = capacity.world_id
    WHERE capacity.world_id = target_world_id
      AND (service.status <> 'active'
           OR NOT facility_type.allowed_service_codes ? service.code
           OR capacity.available_capacity_units <>
              FLOOR(capacity.installed_capacity_units::NUMERIC * capacity.availability_milli / 1000)::BIGINT
           OR capacity.updated_tick > world_tick OR fact.posted_at IS NULL
           OR fact.tick <> capacity.updated_tick OR fact.subject_kind <> 'capacity'
           OR fact.subject_code <> facility.code || '.' || service.code
           OR fact.version_after <> capacity.version);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city service capacity projection is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_service_demands demand
    JOIN city_service_definitions service
      ON service.id = demand.service_definition_id AND service.world_id = demand.world_id
    JOIN city_districts district
      ON district.id = demand.district_id AND district.world_id = demand.world_id
    LEFT JOIN city_buildings building
      ON building.id = demand.building_id AND building.world_id = demand.world_id
    LEFT JOIN city_economic_entities entity
      ON entity.id = demand.entity_id AND entity.world_id = demand.world_id
    LEFT JOIN world_actors actor
      ON actor.id = demand.actor_id AND actor.world_id = demand.world_id
    JOIN city_service_facts fact
      ON fact.id = demand.source_fact_id AND fact.world_id = demand.world_id
    WHERE demand.world_id = target_world_id
      AND (service.status <> 'active' OR demand.updated_tick > world_tick
           OR (demand.subject_kind = 'district' AND district.code <> demand.subject_code)
           OR (demand.subject_kind = 'building' AND
               (building.code <> demand.subject_code OR building.district_id <> demand.district_id
                OR building.status <> 'active'))
           OR (demand.subject_kind = 'household' AND
               (entity.code <> demand.subject_code OR entity.entity_type <> 'household'
                OR entity.status <> 'active' OR NOT EXISTS (
                    SELECT 1 FROM city_household_cohorts cohort
                    WHERE cohort.world_id = demand.world_id
                      AND cohort.entity_id = demand.entity_id
                )))
           OR (demand.subject_kind = 'enterprise' AND
               (entity.code <> demand.subject_code OR entity.entity_type <> 'firm'
                OR entity.status <> 'active' OR NOT EXISTS (
                    SELECT 1 FROM city_firm_states firm
                    WHERE firm.world_id = demand.world_id
                      AND firm.entity_id = demand.entity_id
                )))
           OR (demand.subject_kind = 'actor' AND
               (actor.code <> demand.subject_code OR actor.status <> 'active'
                OR NOT EXISTS (
                    SELECT 1 FROM world_actor_locations location
                    WHERE location.world_id = demand.world_id
                      AND location.actor_id = demand.actor_id
                )))
           OR fact.posted_at IS NULL OR fact.tick <> demand.updated_tick
           OR fact.subject_kind <> 'demand' OR fact.subject_code <> demand.code
           OR fact.version_after <> demand.version);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city service demand projection is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_service_connections connection
    JOIN city_facility_service_capacities capacity
      ON capacity.id = connection.capacity_id AND capacity.world_id = connection.world_id
    JOIN city_service_demands demand
      ON demand.id = connection.demand_id AND demand.world_id = connection.world_id
    JOIN city_service_facts fact
      ON fact.id = connection.source_fact_id AND fact.world_id = connection.world_id
    WHERE connection.world_id = target_world_id
      AND (capacity.service_definition_id <> demand.service_definition_id
           OR connection.updated_tick > world_tick OR fact.posted_at IS NULL
           OR fact.tick <> connection.updated_tick OR fact.subject_kind <> 'connection'
           OR fact.subject_code <> connection.code OR fact.version_after <> connection.version);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city service connection projection is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_service_allocations allocation
    JOIN city_service_facts fact
      ON fact.id = allocation.source_fact_id AND fact.world_id = allocation.world_id
    JOIN city_facility_service_capacities capacity
      ON capacity.id = allocation.capacity_id AND capacity.world_id = allocation.world_id
    JOIN city_service_connections connection
      ON connection.id = allocation.connection_id AND connection.world_id = allocation.world_id
    WHERE allocation.world_id = target_world_id
      AND (allocation.tick > world_tick OR allocation.tick <> fact.tick
           OR allocation.sequence <> fact.sequence
           OR fact.fact_type <> 'service.allocation.settled' OR fact.posted_at IS NULL
           OR allocation.service_definition_id <> capacity.service_definition_id
           OR allocation.facility_id <> capacity.facility_id
           OR allocation.capacity_id <> connection.capacity_id
           OR allocation.demand_id <> connection.demand_id
           OR allocation.dispatched_units > allocation.connection_capacity_units
           OR allocation.delivered_units <>
              FLOOR(allocation.dispatched_units::NUMERIC * (1000 - allocation.loss_milli) / 1000)::BIGINT);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city service allocation history is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_service_settlements settlement
    JOIN city_service_facts fact
      ON fact.id = settlement.source_fact_id AND fact.world_id = settlement.world_id
    JOIN city_service_demands demand
      ON demand.id = settlement.demand_id AND demand.world_id = settlement.world_id
    LEFT JOIN LATERAL (
        SELECT COUNT(*)::INTEGER AS allocation_count,
               COALESCE(SUM(allocation.delivered_units), 0)::BIGINT AS delivered_units
        FROM city_service_allocations allocation
        WHERE allocation.source_fact_id = settlement.source_fact_id
    ) aggregate ON TRUE
    WHERE settlement.world_id = target_world_id
      AND (settlement.tick > world_tick OR settlement.tick <> fact.tick
           OR settlement.sequence <> fact.sequence
           OR fact.fact_type <> 'service.allocation.settled' OR fact.posted_at IS NULL
           OR settlement.service_definition_id <> demand.service_definition_id
           OR settlement.allocation_count <> aggregate.allocation_count
           OR settlement.delivered_units <> aggregate.delivered_units
           OR fact.subject_kind <> 'settlement'
           OR fact.subject_code <> demand.code || '.' || settlement.tick::TEXT);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city service settlement history is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count FROM (
        SELECT allocation.tick, allocation.facility_id, allocation.capacity_id,
               SUM(allocation.dispatched_units) AS dispatched_units,
               MAX(allocation.facility_capacity_units) AS facility_capacity_units,
               MIN(allocation.facility_capacity_units) AS minimum_facility_capacity_units
        FROM city_service_allocations allocation
        JOIN city_facility_service_capacities capacity
          ON capacity.id = allocation.capacity_id AND capacity.world_id = allocation.world_id
        WHERE allocation.world_id = target_world_id
        GROUP BY allocation.tick, allocation.facility_id, allocation.capacity_id
        HAVING SUM(allocation.dispatched_units) > MAX(allocation.facility_capacity_units)
            OR MIN(allocation.facility_capacity_units) <> MAX(allocation.facility_capacity_units)
    ) over_capacity;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city service allocation exceeds facility capacity' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_service_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(
        NULLIF(to_jsonb(NEW)->>'world_id', '')::BIGINT,
        NULLIF(to_jsonb(OLD)->>'world_id', '')::BIGINT,
        NULLIF(to_jsonb(NEW)->>'id', '')::BIGINT,
        NULLIF(to_jsonb(OLD)->>'id', '')::BIGINT
    );
    PERFORM assert_city_spatial_foundation(target_world_id);
    PERFORM assert_city_land_foundation(target_world_id);
    PERFORM assert_city_development_foundation(target_world_id);
    PERFORM assert_world_runtime_foundation(target_world_id);
    PERFORM assert_city_service_foundation(target_world_id);
    RETURN NULL;
END;
$$;

DO $$
DECLARE
    table_name TEXT;
    trigger_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_service_profiles', 'city_service_definitions',
        'city_facility_type_definitions', 'city_service_facts',
        'city_facilities', 'city_facility_service_capacities',
        'city_service_demands', 'city_service_connections',
        'city_service_allocations', 'city_service_settlements'
    ] LOOP
        trigger_name := table_name || '_foundation_commit_check';
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', trigger_name, table_name);
        EXECUTE format(
            'CREATE CONSTRAINT TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I '
            || 'DEFERRABLE INITIALLY DEFERRED FOR EACH ROW '
            || 'EXECUTE FUNCTION check_city_service_foundation()',
            trigger_name, table_name
        );
    END LOOP;
END;
$$;

DROP TRIGGER IF EXISTS city_world_service_foundation_commit_check ON city_worlds;
CREATE CONSTRAINT TRIGGER city_world_service_foundation_commit_check
AFTER INSERT OR UPDATE OF simulation_version, current_tick ON city_worlds
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_service_foundation();

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f8-v1', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","spatial","development","enterprise_location","world_runtime","actor_spatial_control","actor_navigation","portal_access","navigation_intents","public_services","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f7-v9', 'city-f8-v1', 'f7_v9_to_f8_v1')
ON CONFLICT (from_version, to_version) DO NOTHING;
