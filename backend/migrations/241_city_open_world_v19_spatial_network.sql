-- F9.3.0 / V19: immutable spatial identities for V9 hubs and edges.
-- This is intentionally not a lane simulator or a mutable road-life-cycle
-- system. It freezes the profile-selected node/corridor vocabulary that later
-- F9.3 revisions can extend without rewriting V9 history.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
SELECT
    'city-openworld-v19',
    'supported',
    'city-state-v1+gzip',
    COALESCE(
        (SELECT capabilities FROM city_engine_versions WHERE version = 'city-openworld-v18'),
        '[]'::jsonb
    ) || '["spatial_transport_identity","worldgen_transport_styles","static_node_corridor_topology"]'::jsonb
ON CONFLICT (version) DO UPDATE
SET status = EXCLUDED.status,
    canonical_format = EXCLUDED.canonical_format,
    capabilities = EXCLUDED.capabilities;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v18', 'city-openworld-v19', 'openworld_v18_to_v19')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_spatial_network_profiles (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    topology_contract VARCHAR(96) NOT NULL,
    style_contract VARCHAR(96) NOT NULL,
    transport_style_id VARCHAR(128) NOT NULL,
    transport_style_version VARCHAR(24) NOT NULL,
    transport_style_hash VARCHAR(64) NOT NULL,
    source_worldgen_profile_id VARCHAR(96) NOT NULL,
    source_worldgen_profile_version VARCHAR(24) NOT NULL,
    source_worldgen_profile_hash VARCHAR(64) NOT NULL,
    maximum_nodes INTEGER NOT NULL CHECK (maximum_nodes BETWEEN 1 AND 100000),
    maximum_corridors INTEGER NOT NULL CHECK (maximum_corridors BETWEEN 1 AND 1000000),
    node_count BIGINT NOT NULL DEFAULT 0 CHECK (node_count >= 0),
    corridor_count BIGINT NOT NULL DEFAULT 0 CHECK (corridor_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_spatial_network_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-spatial-network'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND topology_contract = 'v9_hub_edge_spatial_corridor_v1'
        AND style_contract = 'worldgen_transport_style_catalog_v1'
        AND transport_style_hash ~ '^[0-9a-f]{64}$'
        AND source_worldgen_profile_hash ~ '^[0-9a-f]{64}$'
        AND maximum_nodes = 4096
        AND maximum_corridors = 32768
        AND node_count <= maximum_nodes
        AND corridor_count <= maximum_corridors
    ),
    CONSTRAINT city_open_world_spatial_network_profile_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'scope' = 'v9_hub_edge_spatial_identity_only'
        AND metadata->>'mutability' = 'static_until_future_f9_3_revision'
        AND metadata->>'legacy' = 'v18_topology_mapped_at_baseline'
    )
);

CREATE TABLE IF NOT EXISTS city_open_world_spatial_network_nodes (
    world_id BIGINT NOT NULL REFERENCES city_open_world_spatial_network_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    hub_code VARCHAR(160) NOT NULL,
    hub_kind VARCHAR(24) NOT NULL,
    node_class VARCHAR(96) NOT NULL,
    anchor_x BIGINT NOT NULL,
    anchor_y BIGINT NOT NULL,
    anchor_z INTEGER NOT NULL CHECK (anchor_z >= 0),
    definition_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_spatial_network_node_hub_unique UNIQUE (world_id, hub_code),
    CONSTRAINT city_open_world_spatial_network_node_hub_fk
        FOREIGN KEY (world_id, hub_code)
        REFERENCES city_open_world_mobility_hubs(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_spatial_network_node_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND hub_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND hub_kind IN ('interchange', 'zone', 'facility')
        AND node_class ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND definition_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_open_world_spatial_network_node_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_spatial_network_corridors (
    world_id BIGINT NOT NULL REFERENCES city_open_world_spatial_network_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    edge_code VARCHAR(160) NOT NULL,
    mode_code VARCHAR(64) NOT NULL,
    from_node_code VARCHAR(160) NOT NULL,
    to_node_code VARCHAR(160) NOT NULL,
    corridor_class VARCHAR(96) NOT NULL,
    tier VARCHAR(24) NOT NULL,
    distance_units BIGINT NOT NULL CHECK (distance_units > 0),
    base_travel_ticks BIGINT NOT NULL CHECK (base_travel_ticks > 0),
    capacity_units_per_tick BIGINT NOT NULL CHECK (capacity_units_per_tick > 0),
    definition_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_spatial_network_corridor_edge_unique UNIQUE (world_id, edge_code),
    CONSTRAINT city_open_world_spatial_network_corridor_edge_fk
        FOREIGN KEY (world_id, edge_code)
        REFERENCES city_open_world_mobility_edges(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_spatial_network_corridor_from_node_fk
        FOREIGN KEY (world_id, from_node_code)
        REFERENCES city_open_world_spatial_network_nodes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_spatial_network_corridor_to_node_fk
        FOREIGN KEY (world_id, to_node_code)
        REFERENCES city_open_world_spatial_network_nodes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_spatial_network_corridor_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND edge_code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND mode_code IN ('walk', 'transit', 'freight')
        AND from_node_code <> to_node_code
        AND corridor_class ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND tier IN ('local', 'trunk')
        AND definition_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_open_world_spatial_network_corridor_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_spatial_network_nodes_hub
    ON city_open_world_spatial_network_nodes (world_id, hub_kind, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_spatial_network_corridors_mode
    ON city_open_world_spatial_network_corridors (world_id, mode_code, tier, code);

CREATE OR REPLACE FUNCTION city_open_world_spatial_network_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_spatial_network_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v19'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1
                    FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v19'
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_spatial_network_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_spatial_network_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V19 spatial-network profile is immutable outside bootstrap/recovery'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_spatial_network_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    target_world_id BIGINT;
    row_data JSONB;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP <> 'INSERT' OR NOT city_open_world_spatial_network_bootstrap_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V19 spatial-network projections require audited bootstrap/recovery context'
            USING ERRCODE = '55000';
    END IF;
    row_data := to_jsonb(NEW);
    IF TG_TABLE_NAME = 'city_open_world_spatial_network_nodes' THEN
        IF NOT EXISTS (
            SELECT 1
            FROM city_open_world_mobility_hubs hub
            WHERE hub.world_id = target_world_id
              AND hub.code = row_data->>'hub_code'
              AND hub.hub_kind = row_data->>'hub_kind'
              AND hub.anchor_x = (row_data->>'anchor_x')::BIGINT
              AND hub.anchor_y = (row_data->>'anchor_y')::BIGINT
              AND hub.anchor_z = (row_data->>'anchor_z')::INTEGER
        ) THEN
            RAISE EXCEPTION 'open-world V19 node must exactly map its V9 hub' USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_open_world_spatial_network_corridors' THEN
        IF NOT EXISTS (
            SELECT 1
            FROM city_open_world_mobility_edges edge
            JOIN city_open_world_spatial_network_nodes source_node
              ON source_node.world_id = edge.world_id AND source_node.code = row_data->>'from_node_code'
            JOIN city_open_world_spatial_network_nodes destination_node
              ON destination_node.world_id = edge.world_id AND destination_node.code = row_data->>'to_node_code'
            WHERE edge.world_id = target_world_id
              AND edge.code = row_data->>'edge_code'
              AND edge.mode_code = row_data->>'mode_code'
              AND edge.from_hub_code = source_node.hub_code
              AND edge.to_hub_code = destination_node.hub_code
              AND edge.tier = row_data->>'tier'
              AND edge.distance_units = (row_data->>'distance_units')::BIGINT
              AND edge.base_travel_ticks = (row_data->>'base_travel_ticks')::BIGINT
              AND edge.capacity_units_per_tick = (row_data->>'capacity_units_per_tick')::BIGINT
        ) THEN
            RAISE EXCEPTION 'open-world V19 corridor must exactly map its V9 edge' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_spatial_network_profile_guard ON city_open_world_spatial_network_profiles;
CREATE TRIGGER city_open_world_spatial_network_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_spatial_network_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_spatial_network_profile();

DROP TRIGGER IF EXISTS city_open_world_spatial_network_node_guard ON city_open_world_spatial_network_nodes;
CREATE TRIGGER city_open_world_spatial_network_node_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_spatial_network_nodes
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_spatial_network_projection();

DROP TRIGGER IF EXISTS city_open_world_spatial_network_corridor_guard ON city_open_world_spatial_network_corridors;
CREATE TRIGGER city_open_world_spatial_network_corridor_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_spatial_network_corridors
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_spatial_network_projection();

CREATE OR REPLACE FUNCTION assert_city_open_world_spatial_network_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version VARCHAR(32); world_tick BIGINT; profile_tick BIGINT;
    profile_nodes BIGINT; profile_corridors BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v19' THEN RETURN; END IF;
    IF NOT EXISTS (SELECT 1 FROM city_open_world_freight_batch_profiles WHERE world_id = target_world_id)
       OR NOT EXISTS (SELECT 1 FROM city_open_world_mobility_profiles WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V19 spatial-network predecessor foundations are missing' USING ERRCODE = '23514';
    END IF;
    SELECT baseline_tick, node_count, corridor_count
      INTO profile_tick, profile_nodes, profile_corridors
    FROM city_open_world_spatial_network_profiles
    WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V19 spatial-network profile is missing or invalid' USING ERRCODE = '23514';
    END IF;
    IF profile_nodes <> (SELECT COUNT(*) FROM city_open_world_spatial_network_nodes WHERE world_id = target_world_id)
       OR profile_corridors <> (SELECT COUNT(*) FROM city_open_world_spatial_network_corridors WHERE world_id = target_world_id)
       OR profile_nodes <> (SELECT COUNT(*) FROM city_open_world_mobility_hubs WHERE world_id = target_world_id)
       OR profile_corridors <> (SELECT COUNT(*) FROM city_open_world_mobility_edges WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V19 spatial-network counters are inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_spatial_network_profiles profile
        JOIN city_open_world_bindings binding ON binding.world_id = profile.world_id
        WHERE profile.world_id = target_world_id
          AND (profile.source_worldgen_profile_id <> binding.profile_id
               OR profile.source_worldgen_profile_version <> binding.profile_version
               OR profile.source_worldgen_profile_hash <> binding.profile_hash)
    ) THEN
        RAISE EXCEPTION 'city open-world V19 spatial-network worldgen binding is inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_spatial_network_nodes node
        JOIN city_open_world_mobility_hubs hub
          ON hub.world_id = node.world_id AND hub.code = node.hub_code
        WHERE node.world_id = target_world_id
          AND (node.hub_kind <> hub.hub_kind
               OR node.anchor_x <> hub.anchor_x OR node.anchor_y <> hub.anchor_y OR node.anchor_z <> hub.anchor_z)
    ) THEN
        RAISE EXCEPTION 'city open-world V19 spatial-network node mapping is inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_spatial_network_corridors corridor
        JOIN city_open_world_mobility_edges edge
          ON edge.world_id = corridor.world_id AND edge.code = corridor.edge_code
        JOIN city_open_world_spatial_network_nodes source_node
          ON source_node.world_id = corridor.world_id AND source_node.code = corridor.from_node_code
        JOIN city_open_world_spatial_network_nodes destination_node
          ON destination_node.world_id = corridor.world_id AND destination_node.code = corridor.to_node_code
        WHERE corridor.world_id = target_world_id
          AND (corridor.mode_code <> edge.mode_code
               OR source_node.hub_code <> edge.from_hub_code OR destination_node.hub_code <> edge.to_hub_code
               OR corridor.tier <> edge.tier OR corridor.distance_units <> edge.distance_units
               OR corridor.base_travel_ticks <> edge.base_travel_ticks
               OR corridor.capacity_units_per_tick <> edge.capacity_units_per_tick)
    ) THEN
        RAISE EXCEPTION 'city open-world V19 spatial-network corridor mapping is inconsistent' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_world_version_vector(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32); vector_generation SMALLINT; vector_version VARCHAR(32);
BEGIN
    SELECT simulation_version, version_vector_generation
      INTO world_version, vector_generation
    FROM city_worlds WHERE id = target_world_id;
    IF world_version !~ '^city-openworld-v[0-9]+$' OR vector_generation < 1 THEN
        RAISE EXCEPTION 'city world version vector only applies to versioned open worlds' USING ERRCODE = '23514';
    END IF;
    SELECT engine_version INTO vector_version
    FROM city_world_version_vectors
    WHERE world_id = target_world_id AND generation = vector_generation;
    IF vector_version IS DISTINCT FROM world_version THEN
        RAISE EXCEPTION 'city world active version vector header is missing or stale' USING ERRCODE = '23514';
    END IF;
    IF (SELECT COUNT(*) FROM city_world_version_bindings
        WHERE world_id = target_world_id AND generation = vector_generation) <> 7 THEN
        RAISE EXCEPTION 'city open-world version vector requires exactly seven bindings' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM (VALUES ('content_catalog'), ('economic_policy'), ('engine'),
                              ('rule_bundle'), ('scenario'), ('spatial_profile'), ('worldgen_plan')) AS required(component_code)
        WHERE NOT EXISTS (SELECT 1 FROM city_world_version_bindings binding
                          WHERE binding.world_id = target_world_id
                            AND binding.generation = vector_generation
                            AND binding.component_code = required.component_code)
    ) THEN
        RAISE EXCEPTION 'city open-world version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v14' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-commute-lifecycle-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V14 commute lifecycle version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v15' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-supply-chain-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V15 supply-chain version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v16' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-enterprise-freight-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V16 enterprise-freight version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v17' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-enterprise-freight-receipt-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V17 freight-receipt version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v18' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-freight-batch-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V18 freight-batch version vector is incomplete' USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v19' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-spatial-network-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V19 spatial-network version vector is incomplete' USING ERRCODE = '23514';
    END IF;
END;
$$;

-- Every predecessor projection must remain writable by an audited V19 tick.
-- Recompile the exact earlier definitions rather than manually drifting their
-- guards. Both forms occur in historical migrations: IN (..., v18), = v18,
-- and <> v18.
DO $$
DECLARE
    target_function REGPROCEDURE;
    definition TEXT;
BEGIN
    FOREACH target_function IN ARRAY ARRAY[
        'city_open_world_supply_chain_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_supply_chain_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_enterprise_freight_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_enterprise_freight_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_enterprise_freight_receipt_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_enterprise_freight_receipt_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_freight_batch_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_freight_batch_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_commute_lifecycle_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_commute_lifecycle_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_commute_source_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_commute_source_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_commute_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_mobility_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_mobility_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_arrival_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_arrival_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_mobility_od_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_mobility_od_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_service_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_service_fact_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_impact_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_impact_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_initialization_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_materialization_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_runtime_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
        'city_open_world_runtime_fact_write_enabled(bigint)'::REGPROCEDURE,
        'guard_city_open_world_runtime_fact_insert()'::REGPROCEDURE,
        'assert_city_open_world_enterprise_freight_foundation(bigint)'::REGPROCEDURE
    ] LOOP
        definition := pg_get_functiondef(target_function);
        definition := replace(definition, $old$'city-openworld-v18')$old$, $new$'city-openworld-v18','city-openworld-v19')$new$);
        definition := replace(definition, $old$= 'city-openworld-v18'$old$, $new$IN ('city-openworld-v18','city-openworld-v19')$new$);
        definition := replace(definition, $old$<> 'city-openworld-v18'$old$, $new$NOT IN ('city-openworld-v18','city-openworld-v19')$new$);
        IF position($needle$city-openworld-v19$needle$ IN definition) = 0 THEN
            RAISE EXCEPTION 'cannot extend V19 predecessor write gate %', target_function USING ERRCODE = '23514';
        END IF;
        EXECUTE definition;
    END LOOP;
END;
$$;

DO $$
DECLARE definition TEXT;
BEGIN
    definition := pg_get_functiondef('assert_city_open_world_enterprise_freight_receipt_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$IF world_version NOT IN ('city-openworld-v17','city-openworld-v18') THEN RETURN; END IF;$old$,
        $new$IF world_version NOT IN ('city-openworld-v17','city-openworld-v18','city-openworld-v19') THEN RETURN; END IF;$new$
    );
    IF position($needle$city-openworld-v19$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V17 receipt foundation to V19' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;

    definition := pg_get_functiondef('assert_city_open_world_freight_batch_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$IF world_version <> 'city-openworld-v18' THEN RETURN; END IF;$old$,
        $new$IF world_version NOT IN ('city-openworld-v18','city-openworld-v19') THEN RETURN; END IF;$new$
    );
    IF position($needle$city-openworld-v19$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V18 freight-batch foundation to V19' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;

DO $$
DECLARE definition TEXT;
BEGIN
    definition := pg_get_functiondef('city_open_world_supply_delivery_resource_operation_authorized(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$'city-openworld-v15','city-openworld-v16','city-openworld-v17','city-openworld-v18'$old$,
        $new$'city-openworld-v15','city-openworld-v16','city-openworld-v17','city-openworld-v18','city-openworld-v19'$new$
    );
    definition := replace(
        definition,
        $old$'city-openworld-v17','city-openworld-v18'$old$,
        $new$'city-openworld-v17','city-openworld-v18','city-openworld-v19'$new$
    );
    definition := replace(
        definition,
        $old$world.simulation_version <> 'city-openworld-v18'$old$,
        $new$world.simulation_version NOT IN ('city-openworld-v18','city-openworld-v19')$new$
    );
    IF position($needle$city-openworld-v19$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V18 supply delivery gate to V19' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;
