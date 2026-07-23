-- A3.3d / realtime shared-cell traffic capacity. This is deliberately a
-- server-owned, one-quantum reservation protocol layered on the immutable
-- 1.13 navigation plan runtime. It adds no model action, browser mutation,
-- route cache, coordinate authority, wallet, reward, provider or V24 mobility
-- dependency. New worlds opt in at genesis through an immutable binding;
-- historical 1.13 worlds remain on their original navigation behavior.

CREATE TABLE IF NOT EXISTS city_realtime_character_traffic_capacity_policies (
    policy_id VARCHAR(96) NOT NULL,
    policy_version VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL,
    policy_schema_version INTEGER NOT NULL,
    manifest JSONB NOT NULL,
    policy_hash VARCHAR(64) NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (policy_id, policy_version),
    CONSTRAINT city_realtime_character_traffic_capacity_policy_check CHECK (
        policy_id = 'city-realtime-pedestrian-capacity'
        AND policy_version = '1.0.0'
        AND status = 'published'
        AND policy_schema_version = 1
        AND jsonb_typeof(manifest) = 'object'
        AND policy_hash ~ '^[0-9a-f]{64}$'
    )
);

WITH traffic_policy AS (
    SELECT '{
      "schema_version": 1,
      "allocation": "stable_due_event_order",
      "reservation_quantum_us": 1000000,
      "terrain_capacities": {
        "terrain.grass": 1,
        "terrain.ground": 1,
        "terrain.road": 1,
        "terrain.sidewalk": 1,
        "terrain.soil": 1
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_character_traffic_capacity_policies
    (policy_id, policy_version, status, policy_schema_version, manifest, policy_hash, published_at)
SELECT 'city-realtime-pedestrian-capacity', '1.0.0', 'published', 1, manifest,
       encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex'), NOW()
FROM traffic_policy
ON CONFLICT (policy_id, policy_version) DO NOTHING;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_traffic_capacity_policy()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'city realtime character traffic capacity policies are immutable'
        USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_traffic_capacity_policy_guard
    ON city_realtime_character_traffic_capacity_policies;
CREATE TRIGGER city_realtime_character_traffic_capacity_policy_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_traffic_capacity_policies
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_traffic_capacity_policy();

CREATE TABLE IF NOT EXISTS city_realtime_character_traffic_reservation_world_bindings (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_realtime_agent_world_bindings(world_id) ON DELETE RESTRICT,
    schema_version INTEGER NOT NULL,
    agent_binding_hash VARCHAR(64) NOT NULL,
    spatial_context_hash VARCHAR(64) NOT NULL,
    capacity_policy_id VARCHAR(96) NOT NULL,
    capacity_policy_version VARCHAR(32) NOT NULL,
    capacity_policy_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_character_traffic_binding_spatial_fk
        FOREIGN KEY (world_id) REFERENCES city_realtime_spatial_bindings(world_id) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_traffic_binding_policy_fk
        FOREIGN KEY (capacity_policy_id, capacity_policy_version)
        REFERENCES city_realtime_character_traffic_capacity_policies(policy_id, policy_version) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_traffic_binding_check CHECK (
        schema_version = 1
        AND agent_binding_hash ~ '^[0-9a-f]{64}$'
        AND spatial_context_hash ~ '^[0-9a-f]{64}$'
        AND capacity_policy_id = 'city-realtime-pedestrian-capacity'
        AND capacity_policy_version = '1.0.0'
        AND capacity_policy_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash ~ '^[0-9a-f]{64}$'
        AND metadata = '{}'::jsonb
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_traffic_reservation_heads (
    world_id BIGINT NOT NULL
        REFERENCES city_realtime_character_traffic_reservation_world_bindings(world_id) ON DELETE RESTRICT,
    actor_code VARCHAR(96) NOT NULL,
    navigation_run_code VARCHAR(96) NOT NULL,
    plan_revision BIGINT NOT NULL,
    reservation_code VARCHAR(96) NOT NULL,
    from_x BIGINT NOT NULL,
    from_y BIGINT NOT NULL,
    from_z SMALLINT NOT NULL,
    target_x BIGINT NOT NULL,
    target_y BIGINT NOT NULL,
    target_z SMALLINT NOT NULL,
    due_world_time_us BIGINT NOT NULL,
    reservation_revision BIGINT NOT NULL,
    reservation_status VARCHAR(24) NOT NULL,
    reason_code VARCHAR(32) NOT NULL DEFAULT '',
    accepted_frame_sequence BIGINT NOT NULL,
    last_frame_sequence BIGINT NOT NULL,
    event_chain_hash VARCHAR(64) NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, navigation_run_code, plan_revision),
    CONSTRAINT city_realtime_character_traffic_head_navigation_fk
        FOREIGN KEY (world_id, actor_code, navigation_run_code)
        REFERENCES city_realtime_character_navigation_plan_heads(world_id, actor_code, navigation_run_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_traffic_head_actor_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_traffic_head_accepted_frame_fk
        FOREIGN KEY (world_id, accepted_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_traffic_head_last_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_traffic_head_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND navigation_run_code ~ '^navigation[.]run[.][0-9a-f]{64}$'
        AND plan_revision >= 1
        AND reservation_code ~ '^traffic[.]reservation[.][0-9a-f]{64}$'
        AND from_z = 0 AND target_z = 0
        AND due_world_time_us >= 0 AND MOD(due_world_time_us, 1000000) = 0
        AND reservation_revision IN (1, 2)
        AND reservation_status IN ('granted', 'denied_capacity', 'consumed', 'released')
        AND accepted_frame_sequence > 0 AND last_frame_sequence >= accepted_frame_sequence
        AND event_chain_hash ~ '^[0-9a-f]{64}$'
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND metadata = '{}'::jsonb
        AND (
            (reservation_revision = 1 AND reservation_status = 'granted' AND reason_code = '')
            OR (reservation_revision = 1 AND reservation_status = 'denied_capacity' AND reason_code = 'capacity_unavailable')
            OR (reservation_revision = 2 AND reservation_status = 'consumed' AND reason_code = '')
            OR (reservation_revision = 2 AND reservation_status = 'released'
                AND reason_code IN ('navigation_cancelled', 'navigation_terminal'))
        )
    )
);

-- The policy's initial capacity is exactly one. A future multi-lane policy
-- must use a new table/index contract rather than weakening this invariant.
CREATE UNIQUE INDEX IF NOT EXISTS uq_city_realtime_character_traffic_active_slot
    ON city_realtime_character_traffic_reservation_heads
       (world_id, due_world_time_us, target_x, target_y, target_z)
    WHERE reservation_status = 'granted';
CREATE INDEX IF NOT EXISTS idx_city_realtime_character_traffic_heads_owner
    ON city_realtime_character_traffic_reservation_heads
       (world_id, actor_code, accepted_frame_sequence DESC, navigation_run_code DESC, plan_revision DESC);

CREATE TABLE IF NOT EXISTS city_realtime_character_traffic_reservation_events (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    navigation_run_code VARCHAR(96) NOT NULL,
    plan_revision BIGINT NOT NULL,
    reservation_code VARCHAR(96) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    event_type VARCHAR(40) NOT NULL,
    reservation_status VARCHAR(24) NOT NULL,
    reason_code VARCHAR(32) NOT NULL DEFAULT '',
    from_x BIGINT NOT NULL,
    from_y BIGINT NOT NULL,
    from_z SMALLINT NOT NULL,
    target_x BIGINT NOT NULL,
    target_y BIGINT NOT NULL,
    target_z SMALLINT NOT NULL,
    due_world_time_us BIGINT NOT NULL,
    actor_position_event_hash VARCHAR(64) NOT NULL DEFAULT '',
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, navigation_run_code, plan_revision, event_sequence),
    CONSTRAINT city_realtime_character_traffic_event_head_fk
        FOREIGN KEY (world_id, actor_code, navigation_run_code, plan_revision)
        REFERENCES city_realtime_character_traffic_reservation_heads(world_id, actor_code, navigation_run_code, plan_revision)
        ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_traffic_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_traffic_event_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND navigation_run_code ~ '^navigation[.]run[.][0-9a-f]{64}$'
        AND plan_revision >= 1
        AND reservation_code ~ '^traffic[.]reservation[.][0-9a-f]{64}$'
        AND event_sequence IN (1, 2) AND frame_sequence > 0
        AND event_type IN ('traffic_reservation_granted', 'traffic_reservation_denied',
                           'traffic_reservation_consumed', 'traffic_reservation_released')
        AND reservation_status IN ('granted', 'denied_capacity', 'consumed', 'released')
        AND from_z = 0 AND target_z = 0
        AND due_world_time_us >= 0 AND MOD(due_world_time_us, 1000000) = 0
        AND (actor_position_event_hash = '' OR actor_position_event_hash ~ '^[0-9a-f]{64}$')
        AND previous_event_hash ~ '^[0-9a-f]{64}$'
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND metadata = '{}'::jsonb
        AND (
            (event_type = 'traffic_reservation_granted' AND event_sequence = 1
             AND reservation_status = 'granted' AND reason_code = '' AND actor_position_event_hash = '')
            OR (event_type = 'traffic_reservation_denied' AND event_sequence = 1
                AND reservation_status = 'denied_capacity' AND reason_code = 'capacity_unavailable'
                AND actor_position_event_hash = '')
            OR (event_type = 'traffic_reservation_consumed' AND event_sequence = 2
                AND reservation_status = 'consumed' AND reason_code = ''
                AND actor_position_event_hash ~ '^[0-9a-f]{64}$')
            OR (event_type = 'traffic_reservation_released' AND event_sequence = 2
                AND reservation_status = 'released'
                AND reason_code IN ('navigation_cancelled', 'navigation_terminal')
                AND actor_position_event_hash = '')
        )
    )
);
CREATE INDEX IF NOT EXISTS idx_city_realtime_character_traffic_events_frame
    ON city_realtime_character_traffic_reservation_events
       (world_id, frame_sequence, actor_code, navigation_run_code, plan_revision);

CREATE OR REPLACE FUNCTION city_realtime_character_traffic_reservation_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_traffic_initialize_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_spatial_bindings spatial ON spatial.world_id = world.id
           JOIN city_realtime_character_traffic_capacity_policies policy
             ON policy.policy_id = 'city-realtime-pedestrian-capacity'
            AND policy.policy_version = '1.0.0' AND policy.status = 'published'
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND world.current_tick = 0 AND world.state_hash IS NULL
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.13.0'
             AND spatial.context_hash ~ '^[0-9a-f]{64}$'
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_character_traffic_reservation_mutation_enabled(
    target_world_id BIGINT,
    target_frame_sequence BIGINT,
    target_due_world_time_us BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_traffic_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_traffic_frame_sequence', TRUE) = target_frame_sequence::text
       AND current_setting('sub2api.city_realtime_character_traffic_due_world_time_us', TRUE) = target_due_world_time_us::text
       AND target_due_world_time_us >= 0
       AND MOD(target_due_world_time_us, 1000000) = 0
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           JOIN city_realtime_spatial_bindings spatial ON spatial.world_id = world.id
           JOIN city_realtime_character_traffic_reservation_world_bindings traffic ON traffic.world_id = world.id
           JOIN city_realtime_character_traffic_capacity_policies policy
             ON policy.policy_id = traffic.capacity_policy_id
            AND policy.policy_version = traffic.capacity_policy_version
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.13.0'
             AND traffic.agent_binding_hash = agent.binding_hash
             AND traffic.spatial_context_hash = spatial.context_hash
             AND traffic.capacity_policy_id = 'city-realtime-pedestrian-capacity'
             AND traffic.capacity_policy_version = '1.0.0'
             AND traffic.capacity_policy_hash = policy.policy_hash
             AND policy.status = 'published'
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_traffic_reservation_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_agent_binding_hash VARCHAR(64);
    expected_spatial_context_hash VARCHAR(64);
    expected_policy_hash VARCHAR(64);
    expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_traffic_reservation_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character traffic reservation binding is immutable outside genesis initialization'
            USING ERRCODE = '55000';
    END IF;
    SELECT agent.binding_hash, spatial.context_hash, policy.policy_hash
    INTO expected_agent_binding_hash, expected_spatial_context_hash, expected_policy_hash
    FROM city_realtime_agent_world_bindings agent
    JOIN city_realtime_spatial_bindings spatial ON spatial.world_id = agent.world_id
    JOIN city_realtime_character_traffic_capacity_policies policy
      ON policy.policy_id = 'city-realtime-pedestrian-capacity'
     AND policy.policy_version = '1.0.0' AND policy.status = 'published'
    WHERE agent.world_id = NEW.world_id
      AND agent.policy_id = 'city-realtime-agent-core'
      AND agent.policy_version = '1.13.0';
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-traffic-binding-v1', expected_agent_binding_hash,
        expected_spatial_context_hash, 'city-realtime-pedestrian-capacity', '1.0.0', expected_policy_hash
    ), 'UTF8')), 'hex');
    IF expected_agent_binding_hash IS NULL OR expected_spatial_context_hash IS NULL OR expected_policy_hash IS NULL
       OR NEW.schema_version <> 1
       OR NEW.agent_binding_hash <> expected_agent_binding_hash
       OR NEW.spatial_context_hash <> expected_spatial_context_hash
       OR NEW.capacity_policy_id <> 'city-realtime-pedestrian-capacity'
       OR NEW.capacity_policy_version <> '1.0.0'
       OR NEW.capacity_policy_hash <> expected_policy_hash
       OR NEW.binding_hash <> expected_binding_hash
       OR NEW.metadata <> '{}'::jsonb THEN
        RAISE EXCEPTION 'city realtime character traffic reservation binding is invalid'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_traffic_reservation_head()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    gate_frame BIGINT := NULLIF(current_setting('sub2api.city_realtime_character_traffic_frame_sequence', TRUE), '')::BIGINT;
    gate_due BIGINT := NULLIF(current_setting('sub2api.city_realtime_character_traffic_due_world_time_us', TRUE), '')::BIGINT;
    nav_status VARCHAR(16);
    nav_revision BIGINT;
    nav_next_due BIGINT;
    actor_x BIGINT;
    actor_y BIGINT;
    actor_z SMALLINT;
    active_slot_count BIGINT;
    expected_reservation_code VARCHAR(96);
    expected_genesis_hash VARCHAR(64);
    expected_event_hash VARCHAR(64);
    expected_state_hash VARCHAR(64);
    expected_dedup_key VARCHAR(160);
    expected_aggregate_key VARCHAR(160);
BEGIN
    IF NOT COALESCE(city_realtime_character_traffic_reservation_mutation_enabled(NEW.world_id, gate_frame, gate_due), FALSE) THEN
        RAISE EXCEPTION 'city realtime character traffic reservation heads are immutable outside sealed reducers'
            USING ERRCODE = '55000';
    END IF;

    IF TG_OP = 'INSERT' THEN
        SELECT plan_status, plan_revision, next_due_world_time_us
        INTO nav_status, nav_revision, nav_next_due
        FROM city_realtime_character_navigation_plan_heads
        WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
          AND navigation_run_code = NEW.navigation_run_code;
        SELECT x, y, z INTO actor_x, actor_y, actor_z
        FROM city_realtime_actor_states
        WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code;
        SELECT COUNT(*) INTO active_slot_count
        FROM city_realtime_character_traffic_reservation_heads
        WHERE world_id = NEW.world_id AND due_world_time_us = NEW.due_world_time_us
          AND target_x = NEW.target_x AND target_y = NEW.target_y AND target_z = NEW.target_z
          AND reservation_status = 'granted';
        expected_reservation_code := 'traffic.reservation.' || encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-traffic-reservation-run-v1', NEW.actor_code,
            NEW.navigation_run_code, NEW.plan_revision::text, NEW.due_world_time_us::text
        ), 'UTF8')), 'hex');
        expected_dedup_key := 'traffic.reservation.request.' || encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-traffic-reservation-dedup-v1', NEW.actor_code,
            NEW.navigation_run_code, NEW.plan_revision::text, NEW.due_world_time_us::text
        ), 'UTF8')), 'hex');
        expected_aggregate_key := 'traffic.reservation.aggregate.' || encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-traffic-reservation-aggregate-v1', NEW.actor_code, NEW.navigation_run_code
        ), 'UTF8')), 'hex');
        expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-traffic-reservation-chain-v1', NEW.actor_code,
            NEW.navigation_run_code, NEW.plan_revision::text, NEW.reservation_code,
            NEW.from_x::text, NEW.from_y::text, NEW.from_z::text,
            NEW.target_x::text, NEW.target_y::text, NEW.target_z::text,
            NEW.due_world_time_us::text, NEW.accepted_frame_sequence::text
        ), 'UTF8')), 'hex');
        expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-traffic-reservation-event-v1', NEW.actor_code,
            NEW.navigation_run_code, NEW.plan_revision::text, NEW.reservation_code,
            '1', NEW.accepted_frame_sequence::text,
            CASE NEW.reservation_status
                WHEN 'granted' THEN 'traffic_reservation_granted'
                WHEN 'denied_capacity' THEN 'traffic_reservation_denied'
            END,
            NEW.reservation_status, NEW.reason_code,
            NEW.from_x::text, NEW.from_y::text, NEW.from_z::text,
            NEW.target_x::text, NEW.target_y::text, NEW.target_z::text,
            NEW.due_world_time_us::text, '', expected_genesis_hash
        ), 'UTF8')), 'hex');
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-traffic-reservation-state-v1', NEW.actor_code,
            NEW.navigation_run_code, NEW.plan_revision::text, NEW.reservation_code,
            NEW.from_x::text, NEW.from_y::text, NEW.from_z::text,
            NEW.target_x::text, NEW.target_y::text, NEW.target_z::text,
            NEW.due_world_time_us::text, '1', NEW.reservation_status, NEW.reason_code,
            NEW.accepted_frame_sequence::text, NEW.last_frame_sequence::text, expected_event_hash
        ), 'UTF8')), 'hex');
        IF nav_status IS DISTINCT FROM 'active'
           OR nav_revision IS DISTINCT FROM NEW.plan_revision
           OR nav_next_due IS DISTINCT FROM NEW.due_world_time_us
           OR actor_x IS DISTINCT FROM NEW.from_x OR actor_y IS DISTINCT FROM NEW.from_y OR actor_z IS DISTINCT FROM NEW.from_z
           OR NOT (
               (NEW.from_x < 9223372036854775807 AND NEW.target_x = NEW.from_x + 1 AND NEW.target_y = NEW.from_y)
               OR (NEW.from_x > -9223372036854775808 AND NEW.target_x = NEW.from_x - 1 AND NEW.target_y = NEW.from_y)
               OR (NEW.from_y < 9223372036854775807 AND NEW.target_y = NEW.from_y + 1 AND NEW.target_x = NEW.from_x)
               OR (NEW.from_y > -9223372036854775808 AND NEW.target_y = NEW.from_y - 1 AND NEW.target_x = NEW.from_x)
           )
           OR NEW.reservation_code <> expected_reservation_code
           OR NEW.reservation_revision <> 1
           OR NEW.accepted_frame_sequence <> gate_frame OR NEW.last_frame_sequence <> gate_frame
           OR NEW.due_world_time_us <> gate_due
           OR NEW.event_chain_hash <> expected_event_hash OR NEW.state_hash <> expected_state_hash
           OR NEW.metadata <> '{}'::jsonb
           OR (NEW.reservation_status = 'granted' AND active_slot_count <> 0)
           OR (NEW.reservation_status = 'denied_capacity' AND active_slot_count < 1)
           OR NOT EXISTS (
               SELECT 1 FROM city_due_events due
               WHERE due.world_id = NEW.world_id
                 AND due.event_type = 'system.realtime.character_traffic_reservation'
                 AND due.schema_version = 1 AND due.due_world_time_us = NEW.due_world_time_us
                 AND due.temporal_phase = 'movement' AND due.priority = 70
                 AND due.aggregate_type = 'realtime_character_traffic'
                 AND due.aggregate_key = expected_aggregate_key AND due.dedup_key = expected_dedup_key
                 AND due.source_kind = 'system' AND due.source_reference = 'realtime_character_traffic_reservation'
                 AND due.expected_version = NEW.plan_revision AND due.status = 'pending'
                 AND due.created_frame_sequence < NEW.accepted_frame_sequence
                 AND due.payload = jsonb_build_object(
                     'schema_version', 1, 'actor_code', NEW.actor_code,
                     'navigation_run_code', NEW.navigation_run_code, 'plan_revision', NEW.plan_revision
                 )
           ) THEN
            RAISE EXCEPTION 'city realtime character traffic reservation genesis is invalid'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'UPDATE' THEN
        expected_state_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-traffic-reservation-state-v1', NEW.actor_code,
            NEW.navigation_run_code, NEW.plan_revision::text, NEW.reservation_code,
            NEW.from_x::text, NEW.from_y::text, NEW.from_z::text,
            NEW.target_x::text, NEW.target_y::text, NEW.target_z::text,
            NEW.due_world_time_us::text, NEW.reservation_revision::text,
            NEW.reservation_status, NEW.reason_code, NEW.accepted_frame_sequence::text,
            NEW.last_frame_sequence::text, NEW.event_chain_hash
        ), 'UTF8')), 'hex');
        IF OLD.reservation_status <> 'granted'
           OR NEW.actor_code <> OLD.actor_code OR NEW.navigation_run_code <> OLD.navigation_run_code
           OR NEW.plan_revision <> OLD.plan_revision OR NEW.reservation_code <> OLD.reservation_code
           OR NEW.from_x <> OLD.from_x OR NEW.from_y <> OLD.from_y OR NEW.from_z <> OLD.from_z
           OR NEW.target_x <> OLD.target_x OR NEW.target_y <> OLD.target_y OR NEW.target_z <> OLD.target_z
           OR NEW.due_world_time_us <> OLD.due_world_time_us
           OR NEW.accepted_frame_sequence <> OLD.accepted_frame_sequence
           OR NEW.reservation_revision <> OLD.reservation_revision + 1
           OR NEW.last_frame_sequence <> gate_frame OR NEW.last_frame_sequence < OLD.last_frame_sequence
           OR (NEW.last_frame_sequence = OLD.last_frame_sequence AND (
               OLD.reservation_revision <> 1 OR OLD.accepted_frame_sequence <> NEW.last_frame_sequence
               OR NEW.reservation_status <> 'consumed'
           ))
           OR NEW.event_chain_hash = OLD.event_chain_hash OR NEW.state_hash <> expected_state_hash
           OR NEW.metadata <> OLD.metadata
           OR NEW.reservation_status NOT IN ('consumed', 'released') THEN
            RAISE EXCEPTION 'city realtime character traffic reservation head transition is invalid'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'city realtime character traffic reservation heads are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_traffic_reservation_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    gate_frame BIGINT := NULLIF(current_setting('sub2api.city_realtime_character_traffic_frame_sequence', TRUE), '')::BIGINT;
    gate_due BIGINT := NULLIF(current_setting('sub2api.city_realtime_character_traffic_due_world_time_us', TRUE), '')::BIGINT;
    head_revision BIGINT;
    head_status VARCHAR(24);
    head_reason VARCHAR(32);
    head_last_frame BIGINT;
    head_chain_hash VARCHAR(64);
    head_from_x BIGINT;
    head_from_y BIGINT;
    head_from_z SMALLINT;
    head_target_x BIGINT;
    head_target_y BIGINT;
    head_target_z SMALLINT;
    head_due BIGINT;
    expected_genesis_hash VARCHAR(64);
    expected_event_hash VARCHAR(64);
    previous_hash VARCHAR(64);
    previous_status VARCHAR(24);
BEGIN
    IF TG_OP <> 'INSERT'
       OR NOT COALESCE(city_realtime_character_traffic_reservation_mutation_enabled(NEW.world_id, gate_frame, gate_due), FALSE) THEN
        RAISE EXCEPTION 'city realtime character traffic reservation events are append-only sealed facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT reservation_revision, reservation_status, reason_code, last_frame_sequence,
           event_chain_hash, from_x, from_y, from_z, target_x, target_y, target_z, due_world_time_us
    INTO head_revision, head_status, head_reason, head_last_frame,
         head_chain_hash, head_from_x, head_from_y, head_from_z,
         head_target_x, head_target_y, head_target_z, head_due
    FROM city_realtime_character_traffic_reservation_heads
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
      AND navigation_run_code = NEW.navigation_run_code AND plan_revision = NEW.plan_revision;
    IF NOT FOUND
       OR NEW.event_sequence <> head_revision OR NEW.frame_sequence <> head_last_frame
       OR NEW.event_hash <> head_chain_hash OR NEW.reservation_status <> head_status
       OR NEW.reason_code <> head_reason
       OR NEW.from_x <> head_from_x OR NEW.from_y <> head_from_y OR NEW.from_z <> head_from_z
       OR NEW.target_x <> head_target_x OR NEW.target_y <> head_target_y OR NEW.target_z <> head_target_z
       OR NEW.due_world_time_us <> head_due THEN
        RAISE EXCEPTION 'city realtime character traffic reservation event head mismatch'
            USING ERRCODE = '23514';
    END IF;
    expected_event_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-traffic-reservation-event-v1', NEW.actor_code,
        NEW.navigation_run_code, NEW.plan_revision::text, NEW.reservation_code,
        NEW.event_sequence::text, NEW.frame_sequence::text, NEW.event_type,
        NEW.reservation_status, NEW.reason_code,
        NEW.from_x::text, NEW.from_y::text, NEW.from_z::text,
        NEW.target_x::text, NEW.target_y::text, NEW.target_z::text,
        NEW.due_world_time_us::text, NEW.actor_position_event_hash, NEW.previous_event_hash
    ), 'UTF8')), 'hex');
    IF NEW.event_hash <> expected_event_hash THEN
        RAISE EXCEPTION 'city realtime character traffic reservation event hash mismatch'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.event_sequence = 1 THEN
        expected_genesis_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-character-traffic-reservation-chain-v1', NEW.actor_code,
            NEW.navigation_run_code, NEW.plan_revision::text, NEW.reservation_code,
            NEW.from_x::text, NEW.from_y::text, NEW.from_z::text,
            NEW.target_x::text, NEW.target_y::text, NEW.target_z::text,
            NEW.due_world_time_us::text, NEW.frame_sequence::text
        ), 'UTF8')), 'hex');
        IF NEW.previous_event_hash <> expected_genesis_hash
           OR NEW.actor_position_event_hash <> ''
           OR (NEW.reservation_status = 'granted' AND NEW.event_type <> 'traffic_reservation_granted')
           OR (NEW.reservation_status = 'denied_capacity' AND NEW.event_type <> 'traffic_reservation_denied') THEN
            RAISE EXCEPTION 'city realtime character traffic reservation genesis event is invalid'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    SELECT event_hash, reservation_status INTO previous_hash, previous_status
    FROM city_realtime_character_traffic_reservation_events
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
      AND navigation_run_code = NEW.navigation_run_code AND plan_revision = NEW.plan_revision
      AND event_sequence = NEW.event_sequence - 1;
    IF NEW.event_sequence <> 2 OR previous_hash IS DISTINCT FROM NEW.previous_event_hash
       OR previous_status IS DISTINCT FROM 'granted' THEN
        RAISE EXCEPTION 'city realtime character traffic reservation event chain is invalid'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.event_type = 'traffic_reservation_consumed' THEN
        IF NEW.reservation_status <> 'consumed' OR NEW.reason_code <> ''
           OR NOT EXISTS (
               SELECT 1 FROM city_realtime_actor_position_events position_event
               WHERE position_event.world_id = NEW.world_id AND position_event.actor_code = NEW.actor_code
                 AND position_event.frame_sequence = NEW.frame_sequence AND position_event.event_kind = 'move'
                 AND position_event.from_x = NEW.from_x AND position_event.from_y = NEW.from_y AND position_event.from_z = NEW.from_z
                 AND position_event.to_x = NEW.target_x AND position_event.to_y = NEW.target_y AND position_event.to_z = NEW.target_z
                 AND position_event.event_hash = NEW.actor_position_event_hash
           ) THEN
            RAISE EXCEPTION 'city realtime character traffic reservation consumption lacks its exact position event'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.event_type = 'traffic_reservation_released' THEN
        IF NEW.reservation_status <> 'released' OR NEW.actor_position_event_hash <> ''
           OR NEW.reason_code NOT IN ('navigation_cancelled', 'navigation_terminal') THEN
            RAISE EXCEPTION 'city realtime character traffic reservation release is invalid'
                USING ERRCODE = '23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'city realtime character traffic reservation transition type is invalid'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_realtime_character_traffic_reservation_head_facts()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    event_count BIGINT;
    current_revision BIGINT;
    current_chain_hash VARCHAR(64);
    current_last_frame BIGINT;
    current_status VARCHAR(24);
    current_reason VARCHAR(32);
    terminal_event_hash VARCHAR(64);
    terminal_event_frame BIGINT;
    terminal_event_status VARCHAR(24);
    terminal_event_reason VARCHAR(32);
BEGIN
    -- INSERT and same-frame consumption intentionally emit two row changes
    -- before the deferred constraint fires.  Re-read the immutable-key head
    -- so both queued trigger invocations validate the final sealed state,
    -- rather than rejecting the superseded grant snapshot.
    SELECT reservation_revision, event_chain_hash, last_frame_sequence,
           reservation_status, reason_code
    INTO current_revision, current_chain_hash, current_last_frame,
         current_status, current_reason
    FROM city_realtime_character_traffic_reservation_heads
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
      AND navigation_run_code = NEW.navigation_run_code AND plan_revision = NEW.plan_revision;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city realtime character traffic reservation head disappeared before fact validation'
            USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*) INTO event_count
    FROM city_realtime_character_traffic_reservation_events
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
      AND navigation_run_code = NEW.navigation_run_code AND plan_revision = NEW.plan_revision;
    SELECT event_hash, frame_sequence, reservation_status, reason_code
    INTO terminal_event_hash, terminal_event_frame, terminal_event_status, terminal_event_reason
    FROM city_realtime_character_traffic_reservation_events
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
      AND navigation_run_code = NEW.navigation_run_code AND plan_revision = NEW.plan_revision
      AND event_sequence = current_revision;
    IF event_count <> current_revision
       OR terminal_event_hash IS DISTINCT FROM current_chain_hash
       OR terminal_event_frame IS DISTINCT FROM current_last_frame
       OR terminal_event_status IS DISTINCT FROM current_status
       OR terminal_event_reason IS DISTINCT FROM current_reason THEN
        RAISE EXCEPTION 'city realtime character traffic reservation head lacks its sealed event chain'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_traffic_reservation_binding_guard
    ON city_realtime_character_traffic_reservation_world_bindings;
CREATE TRIGGER city_realtime_character_traffic_reservation_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_traffic_reservation_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_traffic_reservation_binding();

DROP TRIGGER IF EXISTS city_realtime_character_traffic_reservation_head_guard
    ON city_realtime_character_traffic_reservation_heads;
CREATE TRIGGER city_realtime_character_traffic_reservation_head_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_traffic_reservation_heads
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_traffic_reservation_head();

DROP TRIGGER IF EXISTS city_realtime_character_traffic_reservation_event_guard
    ON city_realtime_character_traffic_reservation_events;
CREATE TRIGGER city_realtime_character_traffic_reservation_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_traffic_reservation_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_traffic_reservation_event();

DROP TRIGGER IF EXISTS city_realtime_character_traffic_reservation_head_fact_guard
    ON city_realtime_character_traffic_reservation_heads;
CREATE CONSTRAINT TRIGGER city_realtime_character_traffic_reservation_head_fact_guard
AFTER INSERT OR UPDATE ON city_realtime_character_traffic_reservation_heads
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_realtime_character_traffic_reservation_head_facts();

-- A traffic-bound world may never retain an active navigation plan without the
-- exact next capacity request.  This is deferred because the reducer schedules
-- both pending due events before it seals the navigation head and event chain.
CREATE OR REPLACE FUNCTION assert_city_realtime_character_navigation_traffic_boundary()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_aggregate_key VARCHAR(160);
    expected_dedup_key VARCHAR(160);
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM city_realtime_character_traffic_reservation_world_bindings traffic
        WHERE traffic.world_id = NEW.world_id
    ) OR NEW.plan_status <> 'active' THEN
        RETURN NULL;
    END IF;

    expected_aggregate_key := 'traffic.reservation.aggregate.' || encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-traffic-reservation-aggregate-v1', NEW.actor_code, NEW.navigation_run_code
    ), 'UTF8')), 'hex');
    expected_dedup_key := 'traffic.reservation.request.' || encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-traffic-reservation-dedup-v1', NEW.actor_code,
        NEW.navigation_run_code, NEW.plan_revision::text, NEW.next_due_world_time_us::text
    ), 'UTF8')), 'hex');
    IF NEW.next_due_world_time_us IS NULL OR NOT EXISTS (
        SELECT 1
        FROM city_due_events due
        WHERE due.world_id = NEW.world_id
          AND due.event_type = 'system.realtime.character_traffic_reservation'
          AND due.schema_version = 1
          AND due.due_world_time_us = NEW.next_due_world_time_us
          AND due.temporal_phase = 'movement' AND due.priority = 70
          AND due.aggregate_type = 'realtime_character_traffic'
          AND due.aggregate_key = expected_aggregate_key AND due.dedup_key = expected_dedup_key
          AND due.source_kind = 'system' AND due.source_reference = 'realtime_character_traffic_reservation'
          AND due.expected_version = NEW.plan_revision AND due.status = 'pending'
          AND due.created_frame_sequence = NEW.last_frame_sequence
          AND due.payload = jsonb_build_object(
              'schema_version', 1, 'actor_code', NEW.actor_code,
              'navigation_run_code', NEW.navigation_run_code, 'plan_revision', NEW.plan_revision
          )
    ) THEN
        RAISE EXCEPTION 'traffic-bound navigation active head lacks its next capacity boundary'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

-- A navigation event that actually moved an Actor must consume the matching
-- one-quantum reservation.  The condition is deliberately attached to the
-- sealed navigation event rather than an API path, so a future reducer cannot
-- bypass capacity merely by calling the position primitive directly.
CREATE OR REPLACE FUNCTION assert_city_realtime_character_navigation_traffic_consumption()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM city_realtime_character_traffic_reservation_world_bindings traffic
        WHERE traffic.world_id = NEW.world_id
    ) OR NEW.actor_position_event_hash = '' THEN
        RETURN NULL;
    END IF;

    IF NEW.event_type NOT IN ('navigation_step', 'navigation_arrived') OR NOT EXISTS (
        SELECT 1
        FROM city_realtime_character_traffic_reservation_heads head
        JOIN city_realtime_character_traffic_reservation_events event
          ON event.world_id = head.world_id
         AND event.actor_code = head.actor_code
         AND event.navigation_run_code = head.navigation_run_code
         AND event.plan_revision = head.plan_revision
         AND event.event_sequence = 2
        WHERE head.world_id = NEW.world_id
          AND head.actor_code = NEW.actor_code
          AND head.navigation_run_code = NEW.navigation_run_code
          -- A navigation event seals the transition from its prior active
          -- revision to the next head revision.  Its capacity receipt is
          -- therefore keyed by the revision carried by the due event, i.e.
          -- event_sequence - 1, not the post-transition head revision.
          AND head.plan_revision = NEW.event_sequence - 1
          AND head.reservation_revision = 2 AND head.reservation_status = 'consumed'
          AND head.from_x = NEW.from_x AND head.from_y = NEW.from_y AND head.from_z = NEW.from_z
          AND head.target_x = NEW.to_x AND head.target_y = NEW.to_y AND head.target_z = NEW.to_z
          AND head.last_frame_sequence = NEW.frame_sequence
          AND event.event_type = 'traffic_reservation_consumed'
          AND event.reservation_status = 'consumed'
          AND event.actor_position_event_hash = NEW.actor_position_event_hash
          AND event.event_hash = head.event_chain_hash
    ) THEN
        RAISE EXCEPTION 'traffic-bound navigation movement lacks its matching consumed reservation'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_navigation_traffic_boundary_guard
    ON city_realtime_character_navigation_plan_heads;
CREATE CONSTRAINT TRIGGER city_realtime_character_navigation_traffic_boundary_guard
AFTER INSERT OR UPDATE ON city_realtime_character_navigation_plan_heads
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_realtime_character_navigation_traffic_boundary();

DROP TRIGGER IF EXISTS city_realtime_character_navigation_traffic_consumption_guard
    ON city_realtime_character_navigation_plan_events;
CREATE CONSTRAINT TRIGGER city_realtime_character_navigation_traffic_consumption_guard
AFTER INSERT ON city_realtime_character_navigation_plan_events
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_city_realtime_character_navigation_traffic_consumption();

-- Preserve every previously permitted additive engine capability transition
-- while admitting this server-owned traffic protocol as one indivisible
-- capability pair.  Existing worlds are not rewritten; only the engine
-- definition advertises that freshly initialized worlds can bind the layer.
CREATE OR REPLACE FUNCTION guard_city_engine_definition_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.version = 'city-openworld-realtime-v2'
       AND NEW.version = OLD.version
       AND NEW.status = OLD.status
       AND NEW.canonical_format = OLD.canonical_format
       AND (
           NEW.capabilities = OLD.capabilities
           OR NEW.capabilities = OLD.capabilities || '["realtime_actors","actor_position_events","member_safe_actor_projection"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_agents","agent_policy_binding","agent_lifecycle"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_life","realtime_character_activity","realtime_character_inventory"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_metabolism","realtime_character_metabolism_due_events"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_portals","realtime_character_interiors"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_progression","realtime_character_roles"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_agent_observations","realtime_agent_decisions","realtime_agent_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_agent_character_control","realtime_agent_personality_revisions","realtime_agent_activity_intents","realtime_agent_wakeups"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_agent_character_navigation_intents","realtime_agent_character_role_intents","realtime_agent_action_context"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_response","realtime_agent_case_response_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_review","realtime_agent_case_review_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_report","realtime_agent_case_report_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_intake","realtime_agent_case_intake_work_items"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_evidence","realtime_agent_case_evidence_sources"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_evidence_assignment","realtime_agent_case_evidence_assignment"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_case_procedure_dispatch","realtime_agent_case_procedure_dispatch"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_tasks","realtime_agent_task_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_navigation_plans","realtime_agent_navigation_plan_intents"]'::jsonb
           OR NEW.capabilities = OLD.capabilities || '["realtime_character_traffic_reservations","realtime_agent_traffic_capacity"]'::jsonb
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_traffic_reservations' THEN capabilities
    ELSE capabilities || '["realtime_character_traffic_reservations","realtime_agent_traffic_capacity"]'::jsonb
END
WHERE version = 'city-openworld-realtime-v2';

COMMENT ON TABLE city_realtime_character_traffic_reservation_heads IS
    'A3.3d server-owned one-quantum shared-cell capacity reservations; no route cache, Agent input, provider, wallet, reward, or other-actor data.';
COMMENT ON TABLE city_realtime_character_traffic_reservation_events IS
    'A3.3d append-only grant, denial and consumption receipts paired with sealed navigation movement facts.';
