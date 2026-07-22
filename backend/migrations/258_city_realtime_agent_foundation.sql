-- The realtime Agent foundation is a sealed policy/lifecycle layer for the
-- shared realtime-v2 world. It is intentionally separate from the legacy
-- tick-driven actor-control and command tables: it records only
-- versioned Agent identity, tree ownership, lifecycle, and policy bindings.
-- No provider credentials, prompts, private memories, model responses, or
-- user-facing account fields are stored in this slice.

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_agents' THEN capabilities
    ELSE capabilities || '["realtime_agents","agent_policy_binding","agent_lifecycle"]'::jsonb
END
WHERE version = 'city-openworld-realtime-v2';

CREATE TABLE IF NOT EXISTS city_realtime_agent_policy_bundles (
    policy_id VARCHAR(96) NOT NULL,
    policy_version VARCHAR(24) NOT NULL,
    status VARCHAR(16) NOT NULL,
    policy_schema_version SMALLINT NOT NULL,
    manifest JSONB NOT NULL,
    policy_hash VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    PRIMARY KEY (policy_id, policy_version),
    CONSTRAINT city_realtime_agent_policy_bundle_check CHECK (
        policy_id ~ '^city-[a-z][a-z0-9_.-]{2,95}$'
        AND policy_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND status IN ('published', 'retired')
        AND policy_schema_version = 1
        AND jsonb_typeof(manifest) = 'object'
        AND jsonb_typeof(manifest -> 'definitions') = 'array'
        AND manifest::TEXT !~* '"(prompt|provider_url|api_key|secret|memory|response_body)"[[:space:]]*:'
        AND policy_hash ~ '^[0-9a-f]{64}$'
        AND policy_hash = encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex')
        AND ((status = 'published' AND published_at IS NOT NULL)
             OR (status = 'retired'))
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_policy_bundles_status
    ON city_realtime_agent_policy_bundles (status, policy_id, policy_version DESC);

-- The built-in policy is deliberately finite and model-agnostic. It fixes
-- only the initial root/manager/NPC hierarchy and has no raw prompt or route.
WITH core_policy AS (
    SELECT '{
      "schema_version": 1,
      "definitions": [
        {"code":"system.root","kind":"simulation","allowed_parents":[],"allowed_children":["system.npc_manager"]},
        {"code":"system.npc_manager","kind":"simulation","allowed_parents":["system.root"],"allowed_children":["character.npc"]},
        {"code":"character.npc","kind":"character","allowed_parents":["system.npc_manager"],"allowed_children":[]},
        {"code":"character.user","kind":"character","allowed_parents":[],"allowed_children":[]}
      ]
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_agent_policy_bundles
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-agent-core', '1.0.0', 'published', 1, manifest,
       encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex'), NOW()
FROM core_policy
ON CONFLICT (policy_id, policy_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_realtime_agent_world_bindings (
    world_id BIGINT PRIMARY KEY REFERENCES city_world_time_states(world_id) ON DELETE RESTRICT,
    policy_id VARCHAR(96) NOT NULL,
    policy_version VARCHAR(24) NOT NULL,
    policy_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    genesis_frame_sequence BIGINT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_agent_world_binding_policy_fk
        FOREIGN KEY (policy_id, policy_version)
        REFERENCES city_realtime_agent_policy_bundles(policy_id, policy_version) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_world_binding_frame_fk
        FOREIGN KEY (world_id, genesis_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_world_binding_check CHECK (
        genesis_frame_sequence = 0
        AND policy_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_agent_instances (
    world_id BIGINT NOT NULL REFERENCES city_realtime_agent_world_bindings(world_id) ON DELETE RESTRICT,
    agent_code VARCHAR(96) NOT NULL,
    agent_kind VARCHAR(16) NOT NULL,
    agent_subtype VARCHAR(96) NOT NULL,
    parent_agent_code VARCHAR(96),
    actor_code VARCHAR(96),
    owner_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    lifecycle_status VARCHAR(16) NOT NULL,
    control_mode VARCHAR(16) NOT NULL,
    definition_code VARCHAR(96) NOT NULL,
    definition_version VARCHAR(24) NOT NULL,
    definition_hash VARCHAR(64) NOT NULL,
    authorization_hash VARCHAR(64) NOT NULL,
    lifecycle_revision BIGINT NOT NULL DEFAULT 1,
    spawned_frame_sequence BIGINT NOT NULL,
    last_frame_sequence BIGINT NOT NULL,
    instance_hash VARCHAR(64) NOT NULL,
    event_chain_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, agent_code),
    CONSTRAINT city_realtime_agent_instance_parent_fk
        FOREIGN KEY (world_id, parent_agent_code)
        REFERENCES city_realtime_agent_instances(world_id, agent_code) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_instance_actor_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_instance_spawn_frame_fk
        FOREIGN KEY (world_id, spawned_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_instance_last_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_instance_check CHECK (
        agent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND agent_kind IN ('simulation', 'character')
        AND agent_subtype ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND lifecycle_status IN ('draft', 'active', 'waiting', 'suspended', 'degraded', 'retiring', 'terminated')
        AND control_mode IN ('system', 'autonomous', 'assisted', 'manual', 'suspended')
        AND definition_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND definition_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND definition_hash ~ '^[0-9a-f]{64}$'
        AND authorization_hash ~ '^[0-9a-f]{64}$'
        AND lifecycle_revision > 0
        AND spawned_frame_sequence >= 0
        AND last_frame_sequence >= spawned_frame_sequence
        AND instance_hash ~ '^[0-9a-f]{64}$'
        AND event_chain_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
        AND (
            (agent_kind = 'simulation'
             AND actor_code IS NULL AND owner_user_id IS NULL
             AND agent_subtype ~ '^system\.' AND control_mode = 'system')
            OR
            (agent_kind = 'character' AND agent_subtype = 'character.npc'
             AND actor_code IS NOT NULL AND owner_user_id IS NULL
             AND parent_agent_code IS NOT NULL AND control_mode IN ('autonomous', 'suspended'))
            OR
            (agent_kind = 'character' AND agent_subtype = 'character.user'
             AND actor_code IS NOT NULL AND owner_user_id IS NOT NULL
             AND parent_agent_code IS NULL AND control_mode IN ('autonomous', 'assisted', 'manual', 'suspended'))
        )
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_realtime_agent_instances_live_actor
    ON city_realtime_agent_instances (world_id, actor_code)
    WHERE agent_kind = 'character'
      AND lifecycle_status NOT IN ('retiring', 'terminated');

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_realtime_agent_instances_live_user_character
    ON city_realtime_agent_instances (world_id, owner_user_id)
    WHERE agent_subtype = 'character.user'
      AND lifecycle_status NOT IN ('retiring', 'terminated');

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_instances_world_status
    ON city_realtime_agent_instances (world_id, lifecycle_status, agent_kind, agent_code);

CREATE TABLE IF NOT EXISTS city_realtime_agent_lifecycle_events (
    world_id BIGINT NOT NULL,
    agent_code VARCHAR(96) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    event_type VARCHAR(24) NOT NULL,
    from_status VARCHAR(16),
    to_status VARCHAR(16) NOT NULL,
    control_mode VARCHAR(16) NOT NULL,
    reason_code VARCHAR(64) NOT NULL,
    previous_event_hash VARCHAR(64),
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, agent_code, event_sequence),
    CONSTRAINT city_realtime_agent_lifecycle_event_agent_fk
        FOREIGN KEY (world_id, agent_code)
        REFERENCES city_realtime_agent_instances(world_id, agent_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_lifecycle_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_agent_lifecycle_event_check CHECK (
        event_sequence >= 0
        AND frame_sequence >= 0
        AND event_type IN ('spawn', 'lifecycle')
        AND (from_status IS NULL OR from_status IN ('draft', 'active', 'waiting', 'suspended', 'degraded', 'retiring', 'terminated'))
        AND to_status IN ('draft', 'active', 'waiting', 'suspended', 'degraded', 'retiring', 'terminated')
        AND control_mode IN ('system', 'autonomous', 'assisted', 'manual', 'suspended')
        AND reason_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND (previous_event_hash IS NULL OR previous_event_hash ~ '^[0-9a-f]{64}$')
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
        AND ((event_type = 'spawn' AND event_sequence = 0 AND from_status IS NULL)
             OR (event_type = 'lifecycle' AND event_sequence > 0 AND from_status IS NOT NULL))
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_lifecycle_events_world_frame
    ON city_realtime_agent_lifecycle_events (world_id, frame_sequence, agent_code, event_sequence);

CREATE OR REPLACE FUNCTION city_realtime_agent_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_agent_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0
             AND world.state_hash IS NULL
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_agent_mutation_enabled(
    target_world_id BIGINT,
    target_frame_sequence BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_agent_mutation_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_agent_mutation_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_policy_bundle()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent policy bundles are immutable versions'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_world_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_policy_hash VARCHAR(64);
    expected_binding_hash VARCHAR(64);
    expected_status VARCHAR(16);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_agent_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime agent world binding is immutable outside genesis initialization'
            USING ERRCODE = '55000';
    END IF;
    SELECT policy_hash, status
    INTO expected_policy_hash, expected_status
    FROM city_realtime_agent_policy_bundles
    WHERE policy_id = NEW.policy_id AND policy_version = NEW.policy_version;
    IF NOT FOUND OR expected_status <> 'published' OR NEW.policy_hash <> expected_policy_hash THEN
        RAISE EXCEPTION 'city realtime agent world binding references an invalid published policy'
            USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-agent-binding-v1', NEW.policy_id, NEW.policy_version, NEW.policy_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime agent world binding hash mismatch'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_instance()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_initialization_enabled(NEW.world_id)
       AND NEW.lifecycle_revision = 1
       AND NEW.spawned_frame_sequence = 0
       AND NEW.last_frame_sequence = 0 THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.last_frame_sequence)
       AND NEW.lifecycle_revision = 1
       AND NEW.spawned_frame_sequence = NEW.last_frame_sequence THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.last_frame_sequence)
       AND NEW.lifecycle_revision = OLD.lifecycle_revision + 1
       AND NEW.last_frame_sequence > OLD.last_frame_sequence
       AND NEW.event_chain_hash <> OLD.event_chain_hash
       AND NEW.instance_hash <> OLD.instance_hash
       AND NEW.agent_kind = OLD.agent_kind
       AND NEW.agent_subtype = OLD.agent_subtype
       AND NEW.parent_agent_code IS NOT DISTINCT FROM OLD.parent_agent_code
       AND NEW.actor_code IS NOT DISTINCT FROM OLD.actor_code
       AND NEW.owner_user_id IS NOT DISTINCT FROM OLD.owner_user_id
       AND NEW.definition_code = OLD.definition_code
       AND NEW.definition_version = OLD.definition_version
       AND NEW.definition_hash = OLD.definition_hash
       AND NEW.authorization_hash = OLD.authorization_hash
       AND NEW.spawned_frame_sequence = OLD.spawned_frame_sequence
       AND NEW.metadata = OLD.metadata THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent instances may change only through the sealed frame reducer'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_lifecycle_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_initialization_enabled(NEW.world_id)
       AND NEW.event_type = 'spawn'
       AND NEW.event_sequence = 0
       AND NEW.frame_sequence = 0 THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.frame_sequence)
       AND (NEW.event_type = 'lifecycle' OR (NEW.event_type = 'spawn' AND NEW.event_sequence = 0)) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent lifecycle events are append-only sealed frame facts'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_agent_policy_bundle_guard ON city_realtime_agent_policy_bundles;
CREATE TRIGGER city_realtime_agent_policy_bundle_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_policy_bundles
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_policy_bundle();

DROP TRIGGER IF EXISTS city_realtime_agent_world_binding_guard ON city_realtime_agent_world_bindings;
CREATE TRIGGER city_realtime_agent_world_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_world_binding();

DROP TRIGGER IF EXISTS city_realtime_agent_instance_guard ON city_realtime_agent_instances;
CREATE TRIGGER city_realtime_agent_instance_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_instances
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_instance();

DROP TRIGGER IF EXISTS city_realtime_agent_lifecycle_event_guard ON city_realtime_agent_lifecycle_events;
CREATE TRIGGER city_realtime_agent_lifecycle_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_lifecycle_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_lifecycle_event();

COMMENT ON TABLE city_realtime_agent_policy_bundles IS
    'Immutable, model-agnostic Agent policy versions. Prompts, provider secrets, private memories, and responses are forbidden.';
COMMENT ON TABLE city_realtime_agent_world_bindings IS
    'Immutable realtime-v2 world pin to one published Agent policy bundle.';
COMMENT ON TABLE city_realtime_agent_instances IS
    'Sealed Agent identity/lifecycle state. External model output cannot write this table directly.';
COMMENT ON TABLE city_realtime_agent_lifecycle_events IS
    'Append-only Agent lifecycle chain; state stores the current event-chain head for bounded canonical hashing.';
