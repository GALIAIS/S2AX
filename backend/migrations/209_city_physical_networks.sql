-- F8.2 generic physical networks, deterministic routed flow, and loss conservation.

ALTER TABLE city_service_allocations
    ADD COLUMN IF NOT EXISTS network_received_units BIGINT,
    ADD COLUMN IF NOT EXISTS network_loss_units BIGINT,
    ADD COLUMN IF NOT EXISTS connection_loss_units BIGINT,
    ADD COLUMN IF NOT EXISTS network_path_count INTEGER;

ALTER TABLE city_service_allocations
    DROP CONSTRAINT IF EXISTS city_service_allocation_units_check;
ALTER TABLE city_service_allocations
    ADD CONSTRAINT city_service_allocation_units_check CHECK (
        dispatched_units <= facility_capacity_units
        AND dispatched_units <= connection_capacity_units
        AND delivered_units + loss_units = dispatched_units
        AND (
            (network_received_units IS NULL AND network_loss_units IS NULL
             AND connection_loss_units IS NULL AND network_path_count IS NULL
             AND delivered_units = FLOOR(
                 dispatched_units::NUMERIC * (1000 - loss_milli) / 1000
             )::BIGINT)
            OR
            (network_received_units IS NOT NULL AND network_loss_units IS NOT NULL
             AND connection_loss_units IS NOT NULL AND network_path_count IS NOT NULL
             AND network_received_units BETWEEN 0 AND dispatched_units
             AND network_loss_units >= 0 AND connection_loss_units >= 0
             AND network_path_count > 0
             AND network_received_units + network_loss_units = dispatched_units
             AND delivered_units + connection_loss_units = network_received_units
             AND loss_units = network_loss_units + connection_loss_units
             AND delivered_units = FLOOR(
                 network_received_units::NUMERIC * (1000 - loss_milli) / 1000
             )::BIGINT)
        )
    );

CREATE TABLE IF NOT EXISTS city_physical_network_profiles (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    policy_id VARCHAR(64) NOT NULL,
    policy_version VARCHAR(24) NOT NULL,
    policy_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    policy_count BIGINT NOT NULL CHECK (policy_count >= 0),
    network_count BIGINT NOT NULL DEFAULT 0 CHECK (network_count BETWEEN 0 AND 10000),
    node_count BIGINT NOT NULL DEFAULT 0 CHECK (node_count BETWEEN 0 AND 10000),
    edge_count BIGINT NOT NULL DEFAULT 0 CHECK (edge_count BETWEEN 0 AND 50000),
    fact_count BIGINT NOT NULL DEFAULT 0 CHECK (fact_count >= 0),
    batch_count BIGINT NOT NULL DEFAULT 0 CHECK (batch_count >= 0),
    path_count BIGINT NOT NULL DEFAULT 0 CHECK (path_count >= 0),
    segment_count BIGINT NOT NULL DEFAULT 0 CHECK (segment_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_physical_network_profile_identity_check CHECK (
        policy_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND policy_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND policy_hash ~ '^[0-9a-f]{64}$'
    )
);

CREATE TABLE IF NOT EXISTS city_physical_network_policies (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    service_definition_id BIGINT NOT NULL,
    policy_version VARCHAR(24) NOT NULL,
    policy_hash VARCHAR(64) NOT NULL,
    network_required BOOLEAN NOT NULL,
    route_direction VARCHAR(24) NOT NULL CHECK (
        route_direction IN ('supply_to_demand', 'demand_to_facility')
    ),
    maximum_nodes INTEGER NOT NULL CHECK (maximum_nodes BETWEEN 1 AND 10000),
    maximum_edges INTEGER NOT NULL CHECK (maximum_edges BETWEEN 1 AND 50000),
    maximum_paths INTEGER NOT NULL CHECK (maximum_paths BETWEEN 1 AND 32),
    maximum_hops INTEGER NOT NULL CHECK (maximum_hops BETWEEN 1 AND 128),
    loss_cost_weight BIGINT NOT NULL CHECK (loss_cost_weight >= 0),
    allow_bidirectional BOOLEAN NOT NULL,
    algorithm_version VARCHAR(24) NOT NULL,
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_physical_network_policy_service_fk
        FOREIGN KEY (service_definition_id, world_id)
        REFERENCES city_service_definitions(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_policy_identity_check CHECK (
        policy_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND policy_hash ~ '^[0-9a-f]{64}$'
        AND algorithm_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
    ),
    CONSTRAINT city_physical_network_policies_service_unique
        UNIQUE (world_id, service_definition_id),
    CONSTRAINT city_physical_network_policies_id_world_unique UNIQUE (id, world_id)
);

CREATE TABLE IF NOT EXISTS city_physical_network_facts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    phase VARCHAR(16) NOT NULL CHECK (phase IN ('command', 'pre_network', 'settlement', 'post_network')),
    source_command_id BIGINT,
    fact_type VARCHAR(48) NOT NULL CHECK (fact_type IN (
        'network.configured', 'node.configured', 'edge.configured',
        'edge.state_changed', 'network.flow_settled',
        'network.topology_synchronized',
        'edge.condition_changed', 'edge.failed'
    )),
    subject_kind VARCHAR(16) NOT NULL CHECK (
        subject_kind IN ('network', 'node', 'edge', 'flow_batch')
    ),
    subject_code VARCHAR(192) NOT NULL CHECK (
        subject_code ~ '^[a-z][a-z0-9_.-]{1,191}$'
    ),
    version_before BIGINT NOT NULL CHECK (version_before >= 0),
    version_after BIGINT NOT NULL CHECK (version_after = version_before + 1),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted_at TIMESTAMPTZ,
    CONSTRAINT city_physical_network_fact_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_physical_network_fact_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_physical_network_fact_origin_check CHECK (
        (phase = 'command' AND source_command_id IS NOT NULL
         AND fact_type IN ('network.configured', 'node.configured',
                           'edge.configured', 'edge.state_changed'))
        OR (phase = 'settlement' AND source_command_id IS NULL
            AND fact_type = 'network.flow_settled')
        OR (phase = 'pre_network' AND source_command_id IS NULL
            AND fact_type = 'network.topology_synchronized')
        OR (phase = 'post_network' AND source_command_id IS NULL
            AND fact_type IN ('edge.condition_changed', 'edge.failed'))
    ),
    CONSTRAINT city_physical_network_fact_posted_check
        CHECK (posted_at IS NULL OR posted_at >= created_at),
    CONSTRAINT city_physical_network_facts_tick_sequence_unique
        UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_physical_network_facts_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_physical_network_facts_one_per_command
    ON city_physical_network_facts (source_command_id)
    WHERE source_command_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_city_physical_network_facts_subject
    ON city_physical_network_facts (world_id, subject_kind, subject_code, tick, sequence);

CREATE TABLE IF NOT EXISTS city_physical_networks (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(96) NOT NULL CHECK (code ~ '^[a-z][a-z0-9_.-]{1,95}$'),
    name VARCHAR(96) NOT NULL CHECK (char_length(name) BETWEEN 1 AND 96 AND name = btrim(name)),
    service_definition_id BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'suspended', 'retired')),
    topology_revision BIGINT NOT NULL CHECK (topology_revision > 0),
    created_tick BIGINT NOT NULL CHECK (created_tick >= 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= created_tick),
    version BIGINT NOT NULL CHECK (version > 0),
    source_fact_id BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_physical_network_service_fk
        FOREIGN KEY (service_definition_id, world_id)
        REFERENCES city_service_definitions(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_physical_network_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_physical_networks_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_physical_networks_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_physical_networks_one_live_per_service
    ON city_physical_networks (world_id, service_definition_id)
    WHERE status <> 'retired';

CREATE TABLE IF NOT EXISTS city_physical_network_nodes (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    network_id BIGINT NOT NULL,
    code VARCHAR(96) NOT NULL CHECK (code ~ '^[a-z][a-z0-9_.-]{1,95}$'),
    role VARCHAR(16) NOT NULL CHECK (role IN ('supply', 'demand', 'junction', 'storage', 'gateway')),
    capacity_id BIGINT,
    demand_id BIGINT,
    district_id BIGINT,
    building_id BIGINT,
    world_x BIGINT,
    world_y BIGINT,
    world_z INTEGER,
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'offline', 'retired')),
    created_tick BIGINT NOT NULL CHECK (created_tick >= 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= created_tick),
    version BIGINT NOT NULL CHECK (version > 0),
    source_fact_id BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_physical_network_node_network_fk
        FOREIGN KEY (network_id, world_id)
        REFERENCES city_physical_networks(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_node_capacity_fk
        FOREIGN KEY (capacity_id, world_id)
        REFERENCES city_facility_service_capacities(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_node_demand_fk
        FOREIGN KEY (demand_id, world_id)
        REFERENCES city_service_demands(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_node_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_node_building_fk
        FOREIGN KEY (building_id, world_id)
        REFERENCES city_buildings(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_node_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_physical_network_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_physical_network_node_binding_check CHECK (
        (role = 'supply' AND capacity_id IS NOT NULL AND demand_id IS NULL)
        OR (role = 'demand' AND capacity_id IS NULL AND demand_id IS NOT NULL)
        OR (role IN ('junction', 'storage', 'gateway')
            AND capacity_id IS NULL AND demand_id IS NULL)
    ),
    CONSTRAINT city_physical_network_node_coordinate_check CHECK (
        (world_x IS NULL AND world_y IS NULL AND world_z IS NULL)
        OR (world_x IS NOT NULL AND world_y IS NOT NULL AND world_z IS NOT NULL)
    ),
    CONSTRAINT city_physical_network_nodes_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_physical_network_nodes_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_physical_network_nodes_active_capacity
    ON city_physical_network_nodes (world_id, network_id, capacity_id)
    WHERE status = 'active' AND capacity_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_city_physical_network_nodes_active_demand
    ON city_physical_network_nodes (world_id, network_id, demand_id)
    WHERE status = 'active' AND demand_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_city_physical_network_nodes_network
    ON city_physical_network_nodes (world_id, network_id, status, code);

CREATE TABLE IF NOT EXISTS city_physical_network_edges (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    network_id BIGINT NOT NULL,
    code VARCHAR(96) NOT NULL CHECK (code ~ '^[a-z][a-z0-9_.-]{1,95}$'),
    from_node_id BIGINT NOT NULL,
    to_node_id BIGINT NOT NULL,
    direction VARCHAR(16) NOT NULL CHECK (direction IN ('directed', 'bidirectional')),
    installed_capacity_units BIGINT NOT NULL CHECK (installed_capacity_units > 0),
    availability_milli INTEGER NOT NULL CHECK (availability_milli BETWEEN 0 AND 1000),
    available_capacity_units BIGINT NOT NULL CHECK (available_capacity_units >= 0),
    loss_milli INTEGER NOT NULL CHECK (loss_milli BETWEEN 0 AND 999),
    base_cost_units BIGINT NOT NULL CHECK (base_cost_units > 0),
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'isolated', 'failed', 'retired')),
    condition_milli INTEGER NOT NULL CHECK (condition_milli BETWEEN 0 AND 1000),
    failure_count BIGINT NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    created_tick BIGINT NOT NULL CHECK (created_tick >= 0),
    updated_tick BIGINT NOT NULL CHECK (updated_tick >= created_tick),
    version BIGINT NOT NULL CHECK (version > 0),
    source_fact_id BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_physical_network_edge_network_fk
        FOREIGN KEY (network_id, world_id)
        REFERENCES city_physical_networks(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_edge_from_fk
        FOREIGN KEY (from_node_id, world_id)
        REFERENCES city_physical_network_nodes(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_edge_to_fk
        FOREIGN KEY (to_node_id, world_id)
        REFERENCES city_physical_network_nodes(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_edge_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_physical_network_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_physical_network_edge_shape_check CHECK (
        from_node_id <> to_node_id
        AND available_capacity_units = FLOOR(
            installed_capacity_units::NUMERIC * availability_milli / 1000
        )::BIGINT
    ),
    CONSTRAINT city_physical_network_edges_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_physical_network_edges_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_physical_network_edges_network
    ON city_physical_network_edges (world_id, network_id, status, code);

CREATE TABLE IF NOT EXISTS city_physical_network_flow_batches (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    network_id BIGINT NOT NULL,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    source_fact_id BIGINT NOT NULL,
    topology_revision BIGINT NOT NULL CHECK (topology_revision > 0),
    allocation_count INTEGER NOT NULL CHECK (allocation_count >= 0),
    path_count INTEGER NOT NULL CHECK (path_count >= 0),
    segment_count INTEGER NOT NULL CHECK (segment_count >= 0),
    dispatched_units BIGINT NOT NULL CHECK (dispatched_units >= 0),
    network_received_units BIGINT NOT NULL CHECK (network_received_units >= 0),
    network_loss_units BIGINT NOT NULL CHECK (network_loss_units >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_physical_network_flow_batch_network_fk
        FOREIGN KEY (network_id, world_id)
        REFERENCES city_physical_networks(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_flow_batch_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_physical_network_facts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_flow_batch_units_check CHECK (
        network_received_units + network_loss_units = dispatched_units
    ),
    CONSTRAINT city_physical_network_flow_batches_network_tick_unique
        UNIQUE (world_id, network_id, tick),
    CONSTRAINT city_physical_network_flow_batches_fact_unique UNIQUE (source_fact_id),
    CONSTRAINT city_physical_network_flow_batches_id_world_unique UNIQUE (id, world_id)
);

CREATE TABLE IF NOT EXISTS city_physical_network_flow_paths (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    batch_id BIGINT NOT NULL,
    service_fact_id BIGINT NOT NULL,
    allocation_index INTEGER NOT NULL CHECK (allocation_index > 0),
    path_index INTEGER NOT NULL CHECK (path_index > 0),
    connection_id BIGINT NOT NULL,
    source_node_id BIGINT NOT NULL,
    sink_node_id BIGINT NOT NULL,
    hop_count INTEGER NOT NULL CHECK (hop_count BETWEEN 1 AND 128),
    dispatched_units BIGINT NOT NULL CHECK (dispatched_units > 0),
    network_received_units BIGINT NOT NULL CHECK (network_received_units > 0),
    network_loss_units BIGINT NOT NULL CHECK (network_loss_units >= 0),
    path_cost_units BIGINT NOT NULL CHECK (path_cost_units > 0),
    path_hash VARCHAR(64) NOT NULL CHECK (path_hash ~ '^[0-9a-f]{64}$'),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_physical_network_flow_path_batch_fk
        FOREIGN KEY (batch_id, world_id)
        REFERENCES city_physical_network_flow_batches(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_flow_path_allocation_fk
        FOREIGN KEY (service_fact_id, allocation_index)
        REFERENCES city_service_allocations(source_fact_id, allocation_index) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_flow_path_connection_fk
        FOREIGN KEY (connection_id, world_id)
        REFERENCES city_service_connections(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_flow_path_source_fk
        FOREIGN KEY (source_node_id, world_id)
        REFERENCES city_physical_network_nodes(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_flow_path_sink_fk
        FOREIGN KEY (sink_node_id, world_id)
        REFERENCES city_physical_network_nodes(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_flow_path_units_check CHECK (
        network_received_units + network_loss_units = dispatched_units
    ),
    CONSTRAINT city_physical_network_flow_paths_allocation_path_unique
        UNIQUE (service_fact_id, allocation_index, path_index),
    CONSTRAINT city_physical_network_flow_paths_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_physical_network_flow_paths_batch
    ON city_physical_network_flow_paths (world_id, batch_id, service_fact_id, allocation_index, path_index);

CREATE TABLE IF NOT EXISTS city_physical_network_flow_segments (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    path_id BIGINT NOT NULL,
    segment_index INTEGER NOT NULL CHECK (segment_index > 0),
    edge_id BIGINT NOT NULL,
    edge_version BIGINT NOT NULL CHECK (edge_version > 0),
    direction VARCHAR(8) NOT NULL CHECK (direction IN ('forward', 'reverse')),
    from_node_id BIGINT NOT NULL,
    to_node_id BIGINT NOT NULL,
    edge_capacity_units BIGINT NOT NULL CHECK (edge_capacity_units > 0),
    loss_milli INTEGER NOT NULL CHECK (loss_milli BETWEEN 0 AND 999),
    input_units BIGINT NOT NULL CHECK (input_units > 0),
    output_units BIGINT NOT NULL CHECK (output_units >= 0),
    loss_units BIGINT NOT NULL CHECK (loss_units >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_physical_network_flow_segment_path_fk
        FOREIGN KEY (path_id, world_id)
        REFERENCES city_physical_network_flow_paths(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_flow_segment_edge_fk
        FOREIGN KEY (edge_id, world_id)
        REFERENCES city_physical_network_edges(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_flow_segment_from_fk
        FOREIGN KEY (from_node_id, world_id)
        REFERENCES city_physical_network_nodes(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_flow_segment_to_fk
        FOREIGN KEY (to_node_id, world_id)
        REFERENCES city_physical_network_nodes(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_physical_network_flow_segment_units_check CHECK (
        output_units + loss_units = input_units
        AND input_units <= edge_capacity_units
        AND output_units = FLOOR(
            input_units::NUMERIC * (1000 - loss_milli) / 1000
        )::BIGINT
    ),
    CONSTRAINT city_physical_network_flow_segments_path_index_unique
        UNIQUE (path_id, segment_index),
    CONSTRAINT city_physical_network_flow_segments_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_physical_network_flow_segments_edge
    ON city_physical_network_flow_segments (world_id, edge_id, path_id, segment_index);

CREATE OR REPLACE FUNCTION city_physical_network_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT city_recovery_write_enabled(target_world_id)
       OR (
           COALESCE(current_setting('sub2api.city_physical_network_bootstrap_world_id', TRUE), '')
               = target_world_id::TEXT
           AND EXISTS (
               SELECT 1 FROM city_worlds world
               WHERE world.id = target_world_id
                 AND ((world.simulation_version = 'city-f8-v3' AND world.current_tick = 0)
                      OR city_engine_upgrade_write_enabled(target_world_id))
           )
       )
$$;

CREATE OR REPLACE FUNCTION city_physical_network_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_physical_network_facts fact
        WHERE fact.id = CASE
            WHEN COALESCE(current_setting('sub2api.city_physical_network_fact_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.city_physical_network_fact_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND fact.world_id = target_world_id AND fact.posted_at IS NULL
    )
$$;

CREATE OR REPLACE FUNCTION guard_city_physical_network_catalog()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_physical_network_bootstrap_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city physical network catalog requires bootstrap or recovery context'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_physical_network_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_physical_network_bootstrap_write_enabled(target_world_id)
       OR city_physical_network_fact_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city physical network projection requires a draft fact'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_physical_network_profile_guard ON city_physical_network_profiles;
CREATE TRIGGER city_physical_network_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_physical_network_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_physical_network_projection();
DROP TRIGGER IF EXISTS city_physical_network_policy_guard ON city_physical_network_policies;
CREATE TRIGGER city_physical_network_policy_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_physical_network_policies
FOR EACH ROW EXECUTE FUNCTION guard_city_physical_network_catalog();
DROP TRIGGER IF EXISTS city_physical_network_guard ON city_physical_networks;
CREATE TRIGGER city_physical_network_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_physical_networks
FOR EACH ROW EXECUTE FUNCTION guard_city_physical_network_projection();
DROP TRIGGER IF EXISTS city_physical_network_node_guard ON city_physical_network_nodes;
CREATE TRIGGER city_physical_network_node_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_physical_network_nodes
FOR EACH ROW EXECUTE FUNCTION guard_city_physical_network_projection();
DROP TRIGGER IF EXISTS city_physical_network_edge_guard ON city_physical_network_edges;
CREATE TRIGGER city_physical_network_edge_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_physical_network_edges
FOR EACH ROW EXECUTE FUNCTION guard_city_physical_network_projection();

CREATE OR REPLACE FUNCTION guard_city_physical_network_fact_insert()
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
    IF world_version IS DISTINCT FROM 'city-f8-v3' OR NEW.tick IS DISTINCT FROM world_tick + 1 THEN
        RAISE EXCEPTION 'city physical network fact must target the next F8.2 tick'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.source_command_id IS NULL THEN
        IF COALESCE(current_setting('sub2api.city_physical_network_auto_world_id', TRUE), '')
               IS DISTINCT FROM NEW.world_id::TEXT THEN
            RAISE EXCEPTION 'automatic city physical network fact is not authorized'
                USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    SELECT command_type, status INTO command_type_value, command_status_value
    FROM city_commands WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
    expected_fact_type := CASE command_type_value
        WHEN 'network.configure' THEN 'network.configured'
        WHEN 'network.node.configure' THEN 'node.configured'
        WHEN 'network.edge.configure' THEN 'edge.configured'
        WHEN 'network.edge.transition' THEN 'edge.state_changed'
        ELSE NULL
    END;
    IF command_status_value IS DISTINCT FROM 'pending'
       OR expected_fact_type IS DISTINCT FROM NEW.fact_type THEN
        RAISE EXCEPTION 'city physical network fact does not match source command'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_physical_network_fact_insert_guard ON city_physical_network_facts;
CREATE TRIGGER city_physical_network_fact_insert_guard
BEFORE INSERT ON city_physical_network_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_physical_network_fact_insert();

CREATE OR REPLACE FUNCTION guard_city_physical_network_fact_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF city_recovery_write_enabled(OLD.world_id) THEN RETURN OLD; END IF;
        RAISE EXCEPTION 'city physical network facts are immutable' USING ERRCODE = '55000';
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
        RAISE EXCEPTION 'city physical network fact can only be sealed once'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_physical_network_fact_immutable_guard ON city_physical_network_facts;
CREATE TRIGGER city_physical_network_fact_immutable_guard
BEFORE UPDATE OR DELETE ON city_physical_network_facts
FOR EACH ROW EXECUTE FUNCTION guard_city_physical_network_fact_immutable();

CREATE OR REPLACE FUNCTION guard_city_physical_network_flow()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
    active_fact_id BIGINT;
    invalid_row BOOLEAN;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_physical_network_fact_write_enabled(target_world_id) THEN
        active_fact_id := current_setting('sub2api.city_physical_network_fact_id', TRUE)::BIGINT;
        invalid_row := FALSE;
        IF TG_TABLE_NAME = 'city_physical_network_flow_batches' THEN
            SELECT NOT (
                NEW.source_fact_id = active_fact_id
                AND fact.world_id = NEW.world_id
                AND fact.tick = NEW.tick
                AND fact.sequence = NEW.sequence
                AND fact.phase = 'settlement'
                AND fact.fact_type = 'network.flow_settled'
                AND fact.subject_kind = 'flow_batch'
                AND fact.subject_code = network.code
                AND network.world_id = NEW.world_id
                AND network.id = NEW.network_id
                AND network.topology_revision = NEW.topology_revision
            ) INTO invalid_row
            FROM city_physical_network_facts fact
            JOIN city_physical_networks network
              ON network.id = NEW.network_id AND network.world_id = NEW.world_id
            WHERE fact.id = active_fact_id;
        ELSIF TG_TABLE_NAME = 'city_physical_network_flow_paths' THEN
            SELECT NOT (
                batch.source_fact_id = active_fact_id
                AND batch.world_id = NEW.world_id
                AND allocation.world_id = NEW.world_id
                AND allocation.tick = batch.tick
                AND allocation.connection_id = NEW.connection_id
                AND source.world_id = NEW.world_id
                AND sink.world_id = NEW.world_id
                AND source.network_id = batch.network_id
                AND sink.network_id = batch.network_id
                AND source.id = NEW.source_node_id
                AND sink.id = NEW.sink_node_id
            ) INTO invalid_row
            FROM city_physical_network_flow_batches batch
            JOIN city_service_allocations allocation
              ON allocation.source_fact_id = NEW.service_fact_id
             AND allocation.allocation_index = NEW.allocation_index
            JOIN city_physical_network_nodes source ON source.id = NEW.source_node_id
            JOIN city_physical_network_nodes sink ON sink.id = NEW.sink_node_id
            WHERE batch.id = NEW.batch_id;
        ELSIF TG_TABLE_NAME = 'city_physical_network_flow_segments' THEN
            SELECT NOT (
                batch.source_fact_id = active_fact_id
                AND path.world_id = NEW.world_id
                AND edge.world_id = NEW.world_id
                AND edge.network_id = batch.network_id
                AND NEW.edge_version = edge.version
                AND NEW.edge_capacity_units = edge.available_capacity_units
                AND NEW.loss_milli = edge.loss_milli
                AND (
                    (NEW.direction = 'forward'
                     AND NEW.from_node_id = edge.from_node_id
                     AND NEW.to_node_id = edge.to_node_id)
                    OR
                    (NEW.direction = 'reverse'
                     AND edge.direction = 'bidirectional'
                     AND NEW.from_node_id = edge.to_node_id
                     AND NEW.to_node_id = edge.from_node_id)
                )
            ) INTO invalid_row
            FROM city_physical_network_flow_paths path
            JOIN city_physical_network_flow_batches batch
              ON batch.id = path.batch_id AND batch.world_id = path.world_id
            JOIN city_physical_network_edges edge
              ON edge.id = NEW.edge_id AND edge.world_id = NEW.world_id
            WHERE path.id = NEW.path_id;
        ELSE
            invalid_row := TRUE;
        END IF;
        IF COALESCE(invalid_row, TRUE) THEN
            RAISE EXCEPTION 'city physical network flow row is not bound to the active fact snapshot'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city physical network flow facts are immutable'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_physical_network_flow_batch_guard ON city_physical_network_flow_batches;
CREATE TRIGGER city_physical_network_flow_batch_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_physical_network_flow_batches
FOR EACH ROW EXECUTE FUNCTION guard_city_physical_network_flow();
DROP TRIGGER IF EXISTS city_physical_network_flow_path_guard ON city_physical_network_flow_paths;
CREATE TRIGGER city_physical_network_flow_path_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_physical_network_flow_paths
FOR EACH ROW EXECUTE FUNCTION guard_city_physical_network_flow();
DROP TRIGGER IF EXISTS city_physical_network_flow_segment_guard ON city_physical_network_flow_segments;
CREATE TRIGGER city_physical_network_flow_segment_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_physical_network_flow_segments
FOR EACH ROW EXECUTE FUNCTION guard_city_physical_network_flow();

CREATE OR REPLACE FUNCTION assert_city_physical_network_fact_committed()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM city_physical_network_facts fact
        WHERE fact.id = NEW.id AND fact.posted_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'city physical network fact must be posted before commit'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_physical_network_fact_commit_check ON city_physical_network_facts;
CREATE CONSTRAINT TRIGGER city_physical_network_fact_commit_check
AFTER INSERT OR UPDATE ON city_physical_network_facts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_physical_network_fact_committed();

-- Extend frozen predecessor compatibility gates to F8.2 and teach the F8.0
-- allocation assertion about the optional network loss decomposition.
CREATE OR REPLACE FUNCTION migration_209_replace_function(
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
        RAISE EXCEPTION 'migration 209 predecessor function % does not contain expected text', target
            USING ERRCODE = '23514';
    END IF;
    EXECUTE patched;
END;
$migration$;

SELECT migration_209_replace_function(
    'world_runtime_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'guard_world_runtime_fact_insert()'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'assert_world_runtime_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'guard_city_enterprise_location_fact_insert()'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'assert_city_enterprise_location_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'guard_city_development_fact_insert()'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'assert_city_development_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'assert_city_land_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'guard_city_spatial_mutation_insert()'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'assert_city_spatial_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'assert_world_actor_spatial_control_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'assert_world_portal_access_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'assert_world_navigation_intent_foundation(bigint)'::REGPROCEDURE,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2')$$,
    $$'city-f7-v9', 'city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'city_service_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
    $$world.simulation_version IN ('city-f8-v1', 'city-f8-v2')$$,
    $$world.simulation_version IN ('city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'guard_city_service_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f8-v1', 'city-f8-v2')$$,
    $$world_version NOT IN ('city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'assert_city_service_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f8-v1', 'city-f8-v2')$$,
    $$world_version NOT IN ('city-f8-v1', 'city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'assert_city_service_foundation(bigint)'::REGPROCEDURE,
    $$allocation.delivered_units <>
              FLOOR(allocation.dispatched_units::NUMERIC * (1000 - allocation.loss_milli) / 1000)::BIGINT$$,
    $$(allocation.network_received_units IS NULL AND allocation.delivered_units <>
              FLOOR(allocation.dispatched_units::NUMERIC * (1000 - allocation.loss_milli) / 1000)::BIGINT)
           OR (allocation.network_received_units IS NOT NULL AND (
               allocation.network_received_units + allocation.network_loss_units <> allocation.dispatched_units
               OR allocation.delivered_units + allocation.connection_loss_units <> allocation.network_received_units
               OR allocation.loss_units <> allocation.network_loss_units + allocation.connection_loss_units
               OR allocation.delivered_units <> FLOOR(
                   allocation.network_received_units::NUMERIC * (1000 - allocation.loss_milli) / 1000
               )::BIGINT))$$);
SELECT migration_209_replace_function(
    'city_facility_lifecycle_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
    $$world.simulation_version = 'city-f8-v2'$$,
    $$world.simulation_version IN ('city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'guard_city_facility_lifecycle_fact_insert()'::REGPROCEDURE,
    $$world_version IS DISTINCT FROM 'city-f8-v2'$$,
    $$world_version NOT IN ('city-f8-v2', 'city-f8-v3')$$);
SELECT migration_209_replace_function(
    'assert_city_facility_lifecycle_foundation(bigint)'::REGPROCEDURE,
    $$world_version <> 'city-f8-v2'$$,
    $$world_version NOT IN ('city-f8-v2', 'city-f8-v3')$$);

DROP FUNCTION migration_209_replace_function(REGPROCEDURE, TEXT, TEXT);

CREATE OR REPLACE FUNCTION assert_city_physical_network_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    expected_policies BIGINT;
    invalid_count BIGINT;
    profile_row city_physical_network_profiles%ROWTYPE;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN RETURN; END IF;
    IF world_version <> 'city-f8-v3' THEN
        SELECT
            (SELECT COUNT(*) FROM city_physical_network_profiles WHERE world_id = target_world_id)
          + (SELECT COUNT(*) FROM city_physical_network_policies WHERE world_id = target_world_id)
          + (SELECT COUNT(*) FROM city_physical_networks WHERE world_id = target_world_id)
          + (SELECT COUNT(*) FROM city_physical_network_nodes WHERE world_id = target_world_id)
          + (SELECT COUNT(*) FROM city_physical_network_edges WHERE world_id = target_world_id)
          + (SELECT COUNT(*) FROM city_physical_network_facts WHERE world_id = target_world_id)
          + (SELECT COUNT(*) FROM city_physical_network_flow_batches WHERE world_id = target_world_id)
          + (SELECT COUNT(*) FROM city_physical_network_flow_paths WHERE world_id = target_world_id)
          + (SELECT COUNT(*) FROM city_physical_network_flow_segments WHERE world_id = target_world_id)
          + (SELECT COUNT(*) FROM city_service_allocations
             WHERE world_id = target_world_id AND network_received_units IS NOT NULL)
        INTO invalid_count;
        IF invalid_count <> 0 THEN
            RAISE EXCEPTION 'pre-F8.2 world contains physical network state' USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;

    SELECT COUNT(*) INTO expected_policies
    FROM city_service_definitions WHERE world_id = target_world_id;
    SELECT * INTO profile_row FROM city_physical_network_profiles WHERE world_id = target_world_id;
    IF profile_row.world_id IS NULL OR profile_row.policy_id <> 'sub2api-physical-networks'
       OR profile_row.policy_version <> '1.0.0'
       OR profile_row.baseline_tick > world_tick
       OR profile_row.policy_count <> expected_policies
       OR profile_row.policy_count <> (SELECT COUNT(*) FROM city_physical_network_policies WHERE world_id = target_world_id)
       OR profile_row.network_count <> (SELECT COUNT(*) FROM city_physical_networks WHERE world_id = target_world_id)
       OR profile_row.node_count <> (SELECT COUNT(*) FROM city_physical_network_nodes WHERE world_id = target_world_id)
       OR profile_row.edge_count <> (SELECT COUNT(*) FROM city_physical_network_edges WHERE world_id = target_world_id)
       OR profile_row.fact_count <> (SELECT COUNT(*) FROM city_physical_network_facts WHERE world_id = target_world_id)
       OR profile_row.batch_count <> (SELECT COUNT(*) FROM city_physical_network_flow_batches WHERE world_id = target_world_id)
       OR profile_row.path_count <> (SELECT COUNT(*) FROM city_physical_network_flow_paths WHERE world_id = target_world_id)
       OR profile_row.segment_count <> (SELECT COUNT(*) FROM city_physical_network_flow_segments WHERE world_id = target_world_id)
       OR profile_row.revision <> profile_row.fact_count + 1 THEN
        RAISE EXCEPTION 'city F8.2 physical network profile is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_physical_network_policies policy
    JOIN city_service_definitions service
      ON service.id = policy.service_definition_id AND service.world_id = policy.world_id
    WHERE policy.world_id = target_world_id
      AND (policy.network_required IS DISTINCT FROM
             (service.code IN ('electric_power', 'potable_water', 'wastewater', 'solid_waste'))
           OR policy.route_direction IS DISTINCT FROM CASE service.flow_kind
                WHEN 'collection' THEN 'demand_to_facility' ELSE 'supply_to_demand' END
           OR (policy.allow_bidirectional = FALSE AND EXISTS (
                SELECT 1 FROM city_physical_networks network
                JOIN city_physical_network_edges edge ON edge.network_id = network.id
                WHERE network.world_id = policy.world_id
                  AND network.service_definition_id = policy.service_definition_id
                  AND edge.direction = 'bidirectional')));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.2 physical network policy is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_physical_network_nodes node
    JOIN city_physical_networks network
      ON network.id = node.network_id AND network.world_id = node.world_id
    LEFT JOIN city_facility_service_capacities capacity
      ON capacity.id = node.capacity_id AND capacity.world_id = node.world_id
    LEFT JOIN city_service_demands demand
      ON demand.id = node.demand_id AND demand.world_id = node.world_id
    WHERE node.world_id = target_world_id
      AND ((node.role = 'supply' AND capacity.service_definition_id <> network.service_definition_id)
           OR (node.role = 'demand' AND demand.service_definition_id <> network.service_definition_id)
           OR (node.building_id IS NOT NULL AND NOT EXISTS (
                SELECT 1 FROM city_buildings building
                WHERE building.id = node.building_id AND building.world_id = node.world_id
                  AND (node.district_id IS NULL OR building.district_id = node.district_id))));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.2 physical network node binding is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_physical_networks network
    WHERE network.world_id = target_world_id AND network.status = 'retired'
      AND (EXISTS (
            SELECT 1 FROM city_physical_network_nodes node
            WHERE node.network_id = network.id AND node.status <> 'retired'
          ) OR EXISTS (
            SELECT 1 FROM city_physical_network_edges edge
            WHERE edge.network_id = network.id AND edge.status <> 'retired'
          ));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'retired city F8.2 physical network retains live assets' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_physical_network_edges edge
    JOIN city_physical_network_nodes source ON source.id = edge.from_node_id
    JOIN city_physical_network_nodes sink ON sink.id = edge.to_node_id
    JOIN city_physical_networks network ON network.id = edge.network_id
    WHERE edge.world_id = target_world_id
      AND (source.network_id <> edge.network_id OR sink.network_id <> edge.network_id
           OR source.world_id <> edge.world_id OR sink.world_id <> edge.world_id
           OR network.world_id <> edge.world_id
           OR (edge.status = 'active' AND (source.status <> 'active' OR sink.status <> 'active')));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.2 physical network edge projection is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_physical_network_facts fact
    LEFT JOIN city_commands command
      ON command.id = fact.source_command_id AND command.world_id = fact.world_id
    WHERE fact.world_id = target_world_id
      AND (fact.tick > world_tick OR fact.posted_at IS NULL
           OR (fact.phase = 'command' AND command.status <> 'applied')
           OR (fact.phase <> 'command' AND fact.source_command_id IS NOT NULL));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.2 physical network fact history is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count FROM (
        SELECT fact.tick, MIN(fact.sequence) AS first_sequence,
               MAX(fact.sequence) AS last_sequence, COUNT(*) AS fact_count
        FROM city_physical_network_facts fact
        WHERE fact.world_id = target_world_id
        GROUP BY fact.tick
        HAVING MIN(fact.sequence) <> 1 OR MAX(fact.sequence) <> COUNT(*)
    ) sequence_gap;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.2 physical network fact sequence is not contiguous' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_physical_network_flow_batches batch
    JOIN city_physical_network_facts fact
      ON fact.id = batch.source_fact_id AND fact.world_id = batch.world_id
    LEFT JOIN LATERAL (
        SELECT COUNT(DISTINCT (path.service_fact_id, path.allocation_index))::INTEGER AS allocation_count,
               COUNT(*)::INTEGER AS path_count,
               COALESCE(SUM(path.dispatched_units), 0)::BIGINT AS dispatched_units,
               COALESCE(SUM(path.network_received_units), 0)::BIGINT AS received_units
        FROM city_physical_network_flow_paths path WHERE path.batch_id = batch.id
    ) path_total ON TRUE
    LEFT JOIN LATERAL (
        SELECT COUNT(*)::INTEGER AS segment_count
        FROM city_physical_network_flow_segments segment
        JOIN city_physical_network_flow_paths path ON path.id = segment.path_id
        WHERE path.batch_id = batch.id
    ) segment_total ON TRUE
    WHERE batch.world_id = target_world_id
      AND (fact.fact_type <> 'network.flow_settled' OR fact.phase <> 'settlement'
           OR fact.posted_at IS NULL OR fact.tick <> batch.tick OR fact.sequence <> batch.sequence
           OR batch.allocation_count <> path_total.allocation_count
           OR batch.path_count <> path_total.path_count
           OR batch.segment_count <> segment_total.segment_count
           OR batch.dispatched_units <> path_total.dispatched_units
           OR batch.network_received_units <> path_total.received_units);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.2 physical network flow batch is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_physical_network_flow_paths path
    JOIN city_service_allocations allocation
      ON allocation.source_fact_id = path.service_fact_id
     AND allocation.allocation_index = path.allocation_index
    JOIN city_physical_network_flow_batches batch ON batch.id = path.batch_id
    LEFT JOIN LATERAL (
        SELECT COUNT(*)::INTEGER AS segment_count,
               MIN(segment.segment_index) AS first_segment,
               MAX(segment.segment_index) AS last_segment,
               MIN(segment.input_units) FILTER (WHERE segment.segment_index = 1) AS first_input,
               MIN(segment.output_units) FILTER (WHERE segment.segment_index = path.hop_count) AS last_output
        FROM city_physical_network_flow_segments segment WHERE segment.path_id = path.id
    ) segment_total ON TRUE
    WHERE path.world_id = target_world_id
      AND (allocation.world_id <> path.world_id OR allocation.tick <> batch.tick
           OR path.connection_id <> allocation.connection_id
           OR path.hop_count <> segment_total.segment_count
           OR segment_total.first_segment <> 1 OR segment_total.last_segment <> path.hop_count
           OR segment_total.first_input <> path.dispatched_units
           OR segment_total.last_output <> path.network_received_units);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.2 physical network path chain is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_physical_network_flow_segments segment
    LEFT JOIN city_physical_network_flow_segments previous
      ON previous.path_id = segment.path_id
     AND previous.segment_index = segment.segment_index - 1
    WHERE segment.world_id = target_world_id
      AND (segment.output_units <> FLOOR(
                segment.input_units::NUMERIC * (1000 - segment.loss_milli) / 1000
              )::BIGINT
           OR (segment.segment_index > 1 AND previous.output_units <> segment.input_units)
           OR (segment.segment_index > 1 AND previous.to_node_id <> segment.from_node_id));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.2 physical network segment is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count FROM (
        SELECT batch.tick, segment.edge_id, SUM(segment.input_units) AS used_units,
               MAX(segment.edge_capacity_units) AS capacity_units,
               MIN(segment.edge_capacity_units) AS minimum_capacity_units
        FROM city_physical_network_flow_segments segment
        JOIN city_physical_network_flow_paths path ON path.id = segment.path_id
        JOIN city_physical_network_flow_batches batch ON batch.id = path.batch_id
        WHERE segment.world_id = target_world_id
        GROUP BY batch.tick, segment.edge_id
        HAVING SUM(segment.input_units) > MAX(segment.edge_capacity_units)
            OR MIN(segment.edge_capacity_units) <> MAX(segment.edge_capacity_units)
    ) over_capacity;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.2 physical network edge capacity is exceeded' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_service_allocations allocation
    JOIN city_service_definitions service ON service.id = allocation.service_definition_id
    LEFT JOIN LATERAL (
        SELECT COUNT(*)::INTEGER AS path_count,
               COALESCE(SUM(path.dispatched_units), 0)::BIGINT AS dispatched_units,
               COALESCE(SUM(path.network_received_units), 0)::BIGINT AS received_units
        FROM city_physical_network_flow_paths path
        WHERE path.service_fact_id = allocation.source_fact_id
          AND path.allocation_index = allocation.allocation_index
    ) path_total ON TRUE
    WHERE allocation.world_id = target_world_id
      AND ((service.code IN ('electric_power', 'potable_water', 'wastewater', 'solid_waste')
            AND ((allocation.tick > profile_row.baseline_tick
                  AND (allocation.network_received_units IS NULL
                       OR allocation.network_path_count <> path_total.path_count
                       OR allocation.dispatched_units <> path_total.dispatched_units
                       OR allocation.network_received_units <> path_total.received_units))
                 OR (allocation.tick <= profile_row.baseline_tick
                     AND allocation.network_received_units IS NOT NULL)))
           OR (service.code NOT IN ('electric_power', 'potable_water', 'wastewater', 'solid_waste')
               AND allocation.network_received_units IS NOT NULL));
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F8.2 service allocation network projection is inconsistent' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_physical_network_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM assert_city_physical_network_foundation(COALESCE(NEW.id, OLD.id));
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_physical_network_world_check ON city_worlds;
CREATE CONSTRAINT TRIGGER city_physical_network_world_check
AFTER INSERT OR UPDATE ON city_worlds DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_physical_network_foundation();

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f8-v3', 'supported', 'city-state-v1+gzip',
        '["f1","f2","f3","f4","f5","f6","f6.2","f6.3","f7","f7.3","f7.4","f7.5","f7.6","f7.7","f7.9","f7.10","f7.11","f8","f8.1","f8.2","facility_lifecycle","physical_networks","network_flow","network_loss"]'::jsonb)
ON CONFLICT (version) DO UPDATE SET status = EXCLUDED.status,
    canonical_format = EXCLUDED.canonical_format, capabilities = EXCLUDED.capabilities;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f8-v2', 'city-f8-v3', 'f8_v2_to_f8_v3')
ON CONFLICT (from_version, to_version) DO NOTHING;
