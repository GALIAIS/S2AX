-- city-openworld-v20 / F9.3.1: generic mutable infrastructure assets over
-- V19's frozen node/corridor identities. The lifecycle is fact-backed but is
-- intentionally not yet read by V9 routing or capacity allocation.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
SELECT
    'city-openworld-v20',
    'supported',
    'city-state-v1+gzip',
    COALESCE(
        (SELECT capabilities FROM city_engine_versions WHERE version = 'city-openworld-v19'),
        '[]'::jsonb
    ) || '["mutable_infrastructure_assets","infrastructure_lifecycle_facts","infrastructure_asset_state_machine"]'::jsonb
ON CONFLICT (version) DO UPDATE
SET status = EXCLUDED.status,
    canonical_format = EXCLUDED.canonical_format,
    capabilities = EXCLUDED.capabilities;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v19', 'city-openworld-v20', 'openworld_v19_to_v20')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_open_world_infrastructure_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_spatial_network_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    asset_contract VARCHAR(96) NOT NULL,
    state_contract VARCHAR(96) NOT NULL,
    maximum_assets INTEGER NOT NULL CHECK (maximum_assets BETWEEN 1 AND 1000000),
    asset_count BIGINT NOT NULL DEFAULT 0 CHECK (asset_count >= 0),
    node_asset_count BIGINT NOT NULL DEFAULT 0 CHECK (node_asset_count >= 0),
    segment_asset_count BIGINT NOT NULL DEFAULT 0 CHECK (segment_asset_count >= 0),
    transition_count BIGINT NOT NULL DEFAULT 0 CHECK (transition_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_infrastructure_profile_identity_check CHECK (
        profile_id = 'sub2api-open-world-infrastructure-assets'
        AND profile_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND asset_contract = 'v19_node_corridor_asset_seed_v1'
        AND state_contract = 'append_only_asset_transition_state_v1'
        AND maximum_assets = 65536
        AND asset_count <= maximum_assets
        AND node_asset_count + segment_asset_count = asset_count
        AND transition_count >= asset_count
    ),
    CONSTRAINT city_open_world_infrastructure_profile_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND metadata->>'schema_version' = '1'
        AND metadata->>'scope' = 'v19_assets_mutable_state_only'
        AND metadata->>'scheduler' = 'not_consumed_by_v9'
        AND metadata->>'legacy' = 'v19_topology_seeded_at_baseline'
    )
);

CREATE TABLE IF NOT EXISTS city_open_world_infrastructure_assets (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_infrastructure_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    asset_kind VARCHAR(32) NOT NULL,
    spatial_node_code VARCHAR(160),
    spatial_corridor_code VARCHAR(160),
    segment_ordinal INTEGER NOT NULL CHECK (segment_ordinal >= 0),
    asset_class VARCHAR(128) NOT NULL,
    definition_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_infrastructure_asset_node_fk
        FOREIGN KEY (world_id, spatial_node_code)
        REFERENCES city_open_world_spatial_network_nodes(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_infrastructure_asset_corridor_fk
        FOREIGN KEY (world_id, spatial_corridor_code)
        REFERENCES city_open_world_spatial_network_corridors(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_infrastructure_asset_node_unique UNIQUE (world_id, spatial_node_code),
    CONSTRAINT city_open_world_infrastructure_asset_corridor_segment_unique
        UNIQUE (world_id, spatial_corridor_code, segment_ordinal),
    CONSTRAINT city_open_world_infrastructure_asset_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND asset_class ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND definition_version = '1.0.0'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND (
            (asset_kind = 'network_node' AND spatial_node_code IS NOT NULL
             AND spatial_corridor_code IS NULL AND segment_ordinal = 0
             AND asset_class LIKE 'node.%')
            OR
            (asset_kind = 'corridor_segment' AND spatial_node_code IS NULL
             AND spatial_corridor_code IS NOT NULL AND segment_ordinal >= 1
             AND asset_class LIKE 'segment.%')
        )
    ),
    CONSTRAINT city_open_world_infrastructure_asset_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_infrastructure_asset_states (
    world_id BIGINT NOT NULL,
    asset_code VARCHAR(160) NOT NULL,
    state VARCHAR(24) NOT NULL,
    capacity_milli BIGINT NOT NULL,
    effective_tick BIGINT NOT NULL CHECK (effective_tick >= 0),
    source_fact_id BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, asset_code),
    CONSTRAINT city_open_world_infrastructure_asset_state_asset_fk
        FOREIGN KEY (world_id, asset_code)
        REFERENCES city_open_world_infrastructure_assets(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_infrastructure_asset_state_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_infrastructure_asset_state_shape_check CHECK (
        (state = 'operational' AND capacity_milli = 1000)
        OR (state = 'restricted' AND capacity_milli BETWEEN 1 AND 999)
        OR (state IN ('maintenance', 'construction', 'closed') AND capacity_milli = 0)
    ),
    CONSTRAINT city_open_world_infrastructure_asset_state_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_infrastructure_asset_transitions (
    world_id BIGINT NOT NULL,
    asset_code VARCHAR(160) NOT NULL,
    transition_tick BIGINT NOT NULL CHECK (transition_tick >= 0),
    transition_sequence BIGINT NOT NULL CHECK (transition_sequence >= 0),
    from_state VARCHAR(24) NOT NULL,
    to_state VARCHAR(24) NOT NULL,
    capacity_milli BIGINT NOT NULL,
    reason_code VARCHAR(96) NOT NULL,
    source_fact_id BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, asset_code, transition_tick, transition_sequence),
    CONSTRAINT city_open_world_infrastructure_transition_asset_fk
        FOREIGN KEY (world_id, asset_code)
        REFERENCES city_open_world_infrastructure_assets(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_infrastructure_transition_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_infrastructure_transition_shape_check CHECK (
        reason_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND (
            (from_state = '' AND to_state = 'operational' AND capacity_milli = 1000
             AND reason_code = 'baseline_initialized' AND source_fact_id IS NULL
             AND transition_sequence = 0)
            OR
            (from_state IN ('operational', 'restricted', 'maintenance', 'construction', 'closed')
             AND to_state IN ('operational', 'restricted', 'maintenance', 'construction', 'closed')
             AND source_fact_id IS NOT NULL
             AND ((to_state = 'operational' AND capacity_milli = 1000)
                  OR (to_state = 'restricted' AND capacity_milli BETWEEN 1 AND 999)
                  OR (to_state IN ('maintenance', 'construction', 'closed') AND capacity_milli = 0)))
        )
    ),
    CONSTRAINT city_open_world_infrastructure_transition_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_infrastructure_assets_kind
    ON city_open_world_infrastructure_assets (world_id, asset_kind, code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_infrastructure_states_status
    ON city_open_world_infrastructure_asset_states (world_id, state, asset_code);
CREATE INDEX IF NOT EXISTS idx_city_open_world_infrastructure_transitions_timeline
    ON city_open_world_infrastructure_asset_transitions (world_id, transition_tick, transition_sequence, asset_code);

CREATE OR REPLACE FUNCTION city_open_world_infrastructure_recovery_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT city_recovery_write_enabled(target_world_id)
       OR COALESCE(current_setting('sub2api.city_open_world_infrastructure_recovery_world_id', TRUE), '') = target_world_id::TEXT
$$;

CREATE OR REPLACE FUNCTION city_open_world_infrastructure_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_infrastructure_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v20'
              AND world.state_hash IS NULL
              AND (world.current_tick = 0 OR EXISTS (
                    SELECT 1 FROM city_world_upgrade_runs upgrade
                    WHERE upgrade.id = CASE WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                        THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT ELSE NULL END
                      AND upgrade.world_id = target_world_id
                      AND upgrade.to_version = 'city-openworld-v20'
                      AND upgrade.status = 'running'
              ))
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_infrastructure_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_infrastructure_world_id', TRUE), '') = target_world_id::TEXT
       AND city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v20'
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_infrastructure_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_infrastructure_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_infrastructure_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_infrastructure_write_enabled(target_world_id)
       AND NEW.transition_count = OLD.transition_count + 1
       AND NEW.revision = OLD.revision + 1
       AND (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.asset_contract, OLD.state_contract,
            OLD.maximum_assets, OLD.asset_count, OLD.node_asset_count,
            OLD.segment_asset_count, OLD.metadata, OLD.created_at)
           IS NOT DISTINCT FROM
           (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.asset_contract, NEW.state_contract,
            NEW.maximum_assets, NEW.asset_count, NEW.node_asset_count,
            NEW.segment_asset_count, NEW.metadata, NEW.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V20 infrastructure profile requires audited bootstrap, mutation, or recovery context'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_infrastructure_asset()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_infrastructure_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP <> 'INSERT' OR NOT city_open_world_infrastructure_bootstrap_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure assets are immutable outside bootstrap/recovery'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.asset_kind = 'network_node' AND EXISTS (
        SELECT 1 FROM city_open_world_spatial_network_nodes node
        WHERE node.world_id = target_world_id AND node.code = NEW.spatial_node_code
          AND NEW.asset_class = 'node.' || node.node_class
          AND NEW.metadata->>'schema_version' = '1'
          AND NEW.metadata->>'source' = 'v19_spatial_network'
          AND NEW.metadata->>'asset_kind' = 'network_node'
          AND NEW.metadata->>'source_code' = node.code
    ) THEN
        RETURN NEW;
    END IF;
    IF NEW.asset_kind = 'corridor_segment' AND EXISTS (
        SELECT 1 FROM city_open_world_spatial_network_corridors corridor
        WHERE corridor.world_id = target_world_id AND corridor.code = NEW.spatial_corridor_code
          AND NEW.segment_ordinal = 1
          AND NEW.asset_class = 'segment.' || corridor.corridor_class
          AND NEW.metadata->>'schema_version' = '1'
          AND NEW.metadata->>'source' = 'v19_spatial_network'
          AND NEW.metadata->>'asset_kind' = 'corridor_segment'
          AND NEW.metadata->>'source_code' = corridor.code
    ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V20 infrastructure asset must exactly map a V19 node or corridor'
        USING ERRCODE = '23514';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_infrastructure_state()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_infrastructure_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_infrastructure_bootstrap_write_enabled(target_world_id)
       AND NEW.source_fact_id IS NULL AND NEW.state = 'operational'
       AND NEW.capacity_milli = 1000 AND NEW.version = 1
       AND NEW.metadata->>'schema_version' = '1'
       AND NEW.metadata->>'origin' = 'baseline'
       AND NEW.metadata->>'scheduler' = 'not_consumed_by_v9'
       AND EXISTS (
            SELECT 1 FROM city_open_world_infrastructure_profiles profile
            WHERE profile.world_id = target_world_id AND profile.baseline_tick = NEW.effective_tick
       ) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND city_open_world_infrastructure_write_enabled(target_world_id)
       AND NEW.version = OLD.version + 1
       AND NEW.effective_tick >= OLD.effective_tick
       AND NEW.source_fact_id IS NOT NULL
       AND NEW.metadata->>'schema_version' = '1'
       AND NEW.metadata->>'origin' = 'command'
       AND NEW.metadata->>'previous_state' = OLD.state
       AND NEW.metadata->>'scheduler' = 'not_consumed_by_v9'
       AND (OLD.world_id, OLD.asset_code, OLD.created_at)
           IS NOT DISTINCT FROM (NEW.world_id, NEW.asset_code, NEW.created_at)
       AND EXISTS (
            SELECT 1 FROM city_open_world_runtime_facts fact
            WHERE fact.id = NEW.source_fact_id AND fact.world_id = target_world_id
              AND fact.tick = NEW.effective_tick AND fact.sequence > 0
              AND fact.fact_type = 'infrastructure.asset.transitioned'
       )
       AND (OLD.source_fact_id IS NULL OR EXISTS (
            SELECT 1
            FROM city_open_world_runtime_facts old_fact
            JOIN city_open_world_runtime_facts new_fact
              ON new_fact.id = NEW.source_fact_id AND new_fact.world_id = target_world_id
            WHERE old_fact.id = OLD.source_fact_id AND old_fact.world_id = target_world_id
              AND (new_fact.tick > old_fact.tick
                   OR (new_fact.tick = old_fact.tick AND new_fact.sequence > old_fact.sequence))
       )) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world V20 infrastructure state requires an audited transition context'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_infrastructure_transition()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    target_world_id BIGINT;
    previous_state VARCHAR(24);
    previous_tick BIGINT;
    previous_sequence BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_infrastructure_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_infrastructure_bootstrap_write_enabled(target_world_id)
       AND NEW.from_state = '' AND NEW.to_state = 'operational'
       AND NEW.capacity_milli = 1000 AND NEW.reason_code = 'baseline_initialized'
       AND NEW.source_fact_id IS NULL AND NEW.transition_sequence = 0
       AND NEW.metadata->>'schema_version' = '1'
       AND NEW.metadata->>'origin' = 'baseline'
       AND NEW.metadata->>'scheduler' = 'not_consumed_by_v9'
       AND EXISTS (
            SELECT 1 FROM city_open_world_infrastructure_profiles profile
            WHERE profile.world_id = target_world_id AND profile.baseline_tick = NEW.transition_tick
       ) THEN
        RETURN NEW;
    END IF;
    IF TG_OP <> 'INSERT' OR NOT city_open_world_infrastructure_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure transitions are append-only audited facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT transition.to_state, transition.transition_tick, transition.transition_sequence
      INTO previous_state, previous_tick, previous_sequence
    FROM city_open_world_infrastructure_asset_transitions transition
    WHERE transition.world_id = target_world_id AND transition.asset_code = NEW.asset_code
      AND (transition.transition_tick < NEW.transition_tick
           OR (transition.transition_tick = NEW.transition_tick
               AND transition.transition_sequence < NEW.transition_sequence))
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1;
    IF previous_state IS NULL OR NEW.from_state <> previous_state
       OR NEW.reason_code = 'baseline_initialized'
       OR NEW.metadata->>'schema_version' <> '1'
       OR NEW.metadata->>'origin' <> 'command'
       OR NEW.metadata->>'previous_state' <> NEW.from_state
       OR NEW.metadata->>'scheduler' <> 'not_consumed_by_v9'
       OR NOT ((NEW.from_state = 'operational' AND NEW.to_state IN ('restricted', 'maintenance', 'closed'))
               OR (NEW.from_state = 'restricted' AND NEW.to_state IN ('operational', 'maintenance', 'closed'))
               OR (NEW.from_state = 'maintenance' AND NEW.to_state IN ('operational', 'closed'))
               OR (NEW.from_state = 'closed' AND NEW.to_state = 'construction')
               OR (NEW.from_state = 'construction' AND NEW.to_state IN ('operational', 'closed'))) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure transition violates the lifecycle state machine'
            USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM city_open_world_runtime_facts fact
        WHERE fact.id = NEW.source_fact_id AND fact.world_id = target_world_id
          AND fact.tick = NEW.transition_tick AND fact.sequence = NEW.transition_sequence
          AND fact.fact_type = 'infrastructure.asset.transitioned'
          AND fact.payload->>'asset_code' = NEW.asset_code
          AND fact.payload->>'from_state' = NEW.from_state
          AND fact.payload->>'to_state' = NEW.to_state
          AND fact.payload->>'capacity_milli' = NEW.capacity_milli::TEXT
          AND fact.payload->>'reason_code' = NEW.reason_code
          AND fact.payload->>'v9_scheduler_effect' = 'none'
    ) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure transition must match its runtime fact cursor'
            USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM city_open_world_infrastructure_asset_states state
        WHERE state.world_id = target_world_id AND state.asset_code = NEW.asset_code
          AND state.state = NEW.to_state AND state.capacity_milli = NEW.capacity_milli
          AND state.effective_tick = NEW.transition_tick
          AND state.source_fact_id = NEW.source_fact_id
          AND state.metadata->>'previous_state' = NEW.from_state
    ) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure transition must match the current state projection'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_infrastructure_profile_guard ON city_open_world_infrastructure_profiles;
CREATE TRIGGER city_open_world_infrastructure_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_infrastructure_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_infrastructure_profile();

DROP TRIGGER IF EXISTS city_open_world_infrastructure_asset_guard ON city_open_world_infrastructure_assets;
CREATE TRIGGER city_open_world_infrastructure_asset_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_infrastructure_assets
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_infrastructure_asset();

DROP TRIGGER IF EXISTS city_open_world_infrastructure_state_guard ON city_open_world_infrastructure_asset_states;
CREATE TRIGGER city_open_world_infrastructure_state_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_infrastructure_asset_states
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_infrastructure_state();

DROP TRIGGER IF EXISTS city_open_world_infrastructure_transition_guard ON city_open_world_infrastructure_asset_transitions;
CREATE TRIGGER city_open_world_infrastructure_transition_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_infrastructure_asset_transitions
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_infrastructure_transition();

CREATE OR REPLACE FUNCTION assert_city_open_world_infrastructure_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version VARCHAR(32); world_tick BIGINT; profile_tick BIGINT;
    profile_assets BIGINT; profile_nodes BIGINT; profile_segments BIGINT;
    profile_transitions BIGINT; profile_revision BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v20' THEN RETURN; END IF;
    PERFORM assert_city_open_world_spatial_network_foundation(target_world_id);
    IF NOT EXISTS (SELECT 1 FROM city_open_world_runtime_profiles WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V20 runtime predecessor foundation is missing' USING ERRCODE = '23514';
    END IF;
    SELECT baseline_tick, asset_count, node_asset_count, segment_asset_count,
           transition_count, revision
      INTO profile_tick, profile_assets, profile_nodes, profile_segments,
           profile_transitions, profile_revision
    FROM city_open_world_infrastructure_profiles
    WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V20 infrastructure profile is missing or invalid' USING ERRCODE = '23514';
    END IF;
    IF profile_assets <> (SELECT COUNT(*) FROM city_open_world_infrastructure_assets WHERE world_id = target_world_id)
       OR profile_nodes <> (SELECT COUNT(*) FROM city_open_world_infrastructure_assets WHERE world_id = target_world_id AND asset_kind = 'network_node')
       OR profile_segments <> (SELECT COUNT(*) FROM city_open_world_infrastructure_assets WHERE world_id = target_world_id AND asset_kind = 'corridor_segment')
       OR profile_transitions <> (SELECT COUNT(*) FROM city_open_world_infrastructure_asset_transitions WHERE world_id = target_world_id)
       OR profile_nodes <> (SELECT COUNT(*) FROM city_open_world_spatial_network_nodes WHERE world_id = target_world_id)
       OR profile_segments <> (SELECT COUNT(*) FROM city_open_world_spatial_network_corridors WHERE world_id = target_world_id)
       OR profile_revision <> 1 + profile_transitions - profile_assets THEN
        RAISE EXCEPTION 'city open-world V20 infrastructure counters are inconsistent' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_spatial_network_nodes node
        LEFT JOIN city_open_world_infrastructure_assets asset
          ON asset.world_id = node.world_id AND asset.asset_kind = 'network_node'
         AND asset.spatial_node_code = node.code
        WHERE node.world_id = target_world_id
          AND (asset.code IS NULL OR asset.asset_class <> 'node.' || node.node_class
               OR asset.metadata->>'schema_version' <> '1'
               OR asset.metadata->>'source' <> 'v19_spatial_network'
               OR asset.metadata->>'asset_kind' <> 'network_node'
               OR asset.metadata->>'source_code' <> node.code)
    ) OR EXISTS (
        SELECT 1
        FROM city_open_world_spatial_network_corridors corridor
        LEFT JOIN city_open_world_infrastructure_assets asset
          ON asset.world_id = corridor.world_id AND asset.asset_kind = 'corridor_segment'
         AND asset.spatial_corridor_code = corridor.code AND asset.segment_ordinal = 1
        WHERE corridor.world_id = target_world_id
          AND (asset.code IS NULL OR asset.asset_class <> 'segment.' || corridor.corridor_class
               OR asset.metadata->>'schema_version' <> '1'
               OR asset.metadata->>'source' <> 'v19_spatial_network'
               OR asset.metadata->>'asset_kind' <> 'corridor_segment'
               OR asset.metadata->>'source_code' <> corridor.code)
    ) THEN
        RAISE EXCEPTION 'city open-world V20 infrastructure assets do not exactly map V19 topology' USING ERRCODE = '23514';
    END IF;
    IF (SELECT COUNT(*) FROM city_open_world_infrastructure_asset_states WHERE world_id = target_world_id) <> profile_assets THEN
        RAISE EXCEPTION 'city open-world V20 infrastructure state coverage is incomplete' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_infrastructure_assets asset
        LEFT JOIN city_open_world_infrastructure_asset_transitions baseline
          ON baseline.world_id = asset.world_id AND baseline.asset_code = asset.code
         AND baseline.transition_tick = profile_tick AND baseline.transition_sequence = 0
        WHERE asset.world_id = target_world_id
          AND (baseline.asset_code IS NULL OR baseline.from_state <> ''
               OR baseline.to_state <> 'operational' OR baseline.capacity_milli <> 1000
               OR baseline.reason_code <> 'baseline_initialized' OR baseline.source_fact_id IS NOT NULL
               OR baseline.metadata->>'schema_version' <> '1'
               OR baseline.metadata->>'origin' <> 'baseline'
               OR baseline.metadata->>'scheduler' <> 'not_consumed_by_v9')
    ) THEN
        RAISE EXCEPTION 'city open-world V20 infrastructure baseline transitions are incomplete' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        WITH ordered AS (
            SELECT transition.*,
                   row_number() OVER (
                       PARTITION BY transition.world_id, transition.asset_code
                       ORDER BY transition.transition_tick, transition.transition_sequence
                   ) AS ordinal,
                   lag(transition.to_state) OVER (
                       PARTITION BY transition.world_id, transition.asset_code
                       ORDER BY transition.transition_tick, transition.transition_sequence
                   ) AS prior_state
            FROM city_open_world_infrastructure_asset_transitions transition
            WHERE transition.world_id = target_world_id
        )
        SELECT 1 FROM ordered transition
        LEFT JOIN city_open_world_runtime_facts fact
          ON fact.id = transition.source_fact_id AND fact.world_id = transition.world_id
        WHERE (transition.ordinal = 1 AND (transition.transition_tick <> profile_tick
                                           OR transition.transition_sequence <> 0
                                           OR transition.source_fact_id IS NOT NULL))
           OR (transition.ordinal > 1 AND (
                transition.transition_tick <= profile_tick
                OR transition.source_fact_id IS NULL
                OR transition.from_state <> transition.prior_state
                OR NOT ((transition.from_state = 'operational' AND transition.to_state IN ('restricted', 'maintenance', 'closed'))
                        OR (transition.from_state = 'restricted' AND transition.to_state IN ('operational', 'maintenance', 'closed'))
                        OR (transition.from_state = 'maintenance' AND transition.to_state IN ('operational', 'closed'))
                        OR (transition.from_state = 'closed' AND transition.to_state = 'construction')
                        OR (transition.from_state = 'construction' AND transition.to_state IN ('operational', 'closed')))
                OR transition.reason_code = 'baseline_initialized'
                OR transition.metadata->>'schema_version' <> '1'
                OR transition.metadata->>'origin' <> 'command'
                OR transition.metadata->>'previous_state' <> transition.from_state
                OR transition.metadata->>'scheduler' <> 'not_consumed_by_v9'
                OR fact.id IS NULL OR fact.tick <> transition.transition_tick
                OR fact.sequence <> transition.transition_sequence
                OR fact.fact_type <> 'infrastructure.asset.transitioned'
                OR fact.payload->>'asset_code' <> transition.asset_code
                OR fact.payload->>'from_state' <> transition.from_state
                OR fact.payload->>'to_state' <> transition.to_state
                OR fact.payload->>'capacity_milli' <> transition.capacity_milli::TEXT
                OR fact.payload->>'reason_code' <> transition.reason_code
                OR fact.payload->>'v9_scheduler_effect' <> 'none'
           ))
    ) THEN
        RAISE EXCEPTION 'city open-world V20 infrastructure transition timeline is invalid' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM city_open_world_infrastructure_asset_states state
        JOIN LATERAL (
            SELECT transition.*
            FROM city_open_world_infrastructure_asset_transitions transition
            WHERE transition.world_id = state.world_id AND transition.asset_code = state.asset_code
            ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
            LIMIT 1
        ) latest ON TRUE
        WHERE state.world_id = target_world_id
          AND (state.state <> latest.to_state
               OR state.capacity_milli <> latest.capacity_milli
               OR state.effective_tick <> latest.transition_tick
               OR state.source_fact_id IS DISTINCT FROM latest.source_fact_id
               OR state.version <> (
                    SELECT COUNT(*) FROM city_open_world_infrastructure_asset_transitions transition
                    WHERE transition.world_id = state.world_id AND transition.asset_code = state.asset_code
               )
               OR (state.source_fact_id IS NULL AND (state.metadata->>'origin' <> 'baseline'
                                                    OR state.metadata->>'scheduler' <> 'not_consumed_by_v9'))
               OR (state.source_fact_id IS NOT NULL AND (state.metadata->>'origin' <> 'command'
                                                         OR state.metadata->>'previous_state' <> latest.from_state
                                                         OR state.metadata->>'scheduler' <> 'not_consumed_by_v9')))
    ) THEN
        RAISE EXCEPTION 'city open-world V20 infrastructure current state is inconsistent' USING ERRCODE = '23514';
    END IF;
END;
$$;

-- V20 must still initialize and validate V19's frozen topology. Recompile the
-- assertion rather than weakening the static V19 data model.
CREATE OR REPLACE FUNCTION assert_city_open_world_spatial_network_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version VARCHAR(32); world_tick BIGINT; profile_tick BIGINT;
    profile_nodes BIGINT; profile_corridors BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v19','city-openworld-v20') THEN RETURN; END IF;
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
    IF world_version = 'city-openworld-v20' AND NOT EXISTS (
        SELECT 1 FROM city_world_version_bindings binding WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-infrastructure-catalog' AND binding.bundle_version = '1.0.0') THEN
        RAISE EXCEPTION 'city open-world V20 infrastructure version vector is incomplete' USING ERRCODE = '23514';
    END IF;
END;
$$;

-- Every predecessor projection must remain writable by an audited V20 tick.
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
        definition := replace(definition, $old$'city-openworld-v18','city-openworld-v19')$old$, $new$'city-openworld-v18','city-openworld-v19','city-openworld-v20')$new$);
        definition := replace(definition, $old$IN ('city-openworld-v18','city-openworld-v19')$old$, $new$IN ('city-openworld-v18','city-openworld-v19','city-openworld-v20')$new$);
        definition := replace(definition, $old$NOT IN ('city-openworld-v18','city-openworld-v19')$old$, $new$NOT IN ('city-openworld-v18','city-openworld-v19','city-openworld-v20')$new$);
        IF position($needle$city-openworld-v20$needle$ IN definition) = 0 THEN
            RAISE EXCEPTION 'cannot extend V20 predecessor write gate %', target_function USING ERRCODE = '23514';
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
        $old$IF world_version NOT IN ('city-openworld-v17','city-openworld-v18','city-openworld-v19') THEN RETURN; END IF;$old$,
        $new$IF world_version NOT IN ('city-openworld-v17','city-openworld-v18','city-openworld-v19','city-openworld-v20') THEN RETURN; END IF;$new$
    );
    IF position($needle$city-openworld-v20$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V17 receipt foundation to V20' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;

    definition := pg_get_functiondef('assert_city_open_world_freight_batch_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$IF world_version NOT IN ('city-openworld-v18','city-openworld-v19') THEN RETURN; END IF;$old$,
        $new$IF world_version NOT IN ('city-openworld-v18','city-openworld-v19','city-openworld-v20') THEN RETURN; END IF;$new$
    );
    IF position($needle$city-openworld-v20$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V18 freight-batch foundation to V20' USING ERRCODE = '23514';
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
        $old$'city-openworld-v15','city-openworld-v16','city-openworld-v17','city-openworld-v18','city-openworld-v19'$old$,
        $new$'city-openworld-v15','city-openworld-v16','city-openworld-v17','city-openworld-v18','city-openworld-v19','city-openworld-v20'$new$
    );
    definition := replace(
        definition,
        $old$'city-openworld-v17','city-openworld-v18','city-openworld-v19'$old$,
        $new$'city-openworld-v17','city-openworld-v18','city-openworld-v19','city-openworld-v20'$new$
    );
    definition := replace(
        definition,
        $old$world.simulation_version NOT IN ('city-openworld-v18','city-openworld-v19')$old$,
        $new$world.simulation_version NOT IN ('city-openworld-v18','city-openworld-v19','city-openworld-v20')$new$
    );
    IF position($needle$city-openworld-v20$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V18 supply delivery gate to V20' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;

-- V20 genesis executes V19's initializer first. Its bootstrap guard must
-- therefore allow the V20 world while retaining V19's original upgrade check.
DO $$
DECLARE definition TEXT;
BEGIN
    definition := pg_get_functiondef('city_open_world_spatial_network_bootstrap_write_enabled(bigint)'::REGPROCEDURE);
    definition := regexp_replace(
        definition,
        $pattern$world\.simulation_version = 'city-openworld-v19'(::character varying)?$pattern$,
        $replacement$world.simulation_version IN ('city-openworld-v19','city-openworld-v20')$replacement$,
        'g'
    );
    IF position($needle$city-openworld-v20$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V19 spatial-network bootstrap gate to V20' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;
