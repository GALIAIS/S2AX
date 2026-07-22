-- Realtime player-character life is a sealed, world-bound action layer. It
-- deliberately keeps city-earned credits separate from platform wallets: a
-- later reward outbox may consume verified city facts, but browser actions
-- can never mint, debit, or transfer a platform virtual currency directly.

-- Migration 194 keeps engine definitions immutable. Extend the narrow
-- realtime-v2 transition bridge by exactly one declarative capability set;
-- normal writes remain forbidden.
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
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_actors","actor_position_events","member_safe_actor_projection"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_agents","agent_policy_binding","agent_lifecycle"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_life","realtime_character_activity","realtime_character_inventory"]'::jsonb
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_life' THEN capabilities
    ELSE capabilities ||
        '["realtime_character_life","realtime_character_activity","realtime_character_inventory"]'::jsonb
END
WHERE version = 'city-openworld-realtime-v2';

CREATE TABLE IF NOT EXISTS city_realtime_character_activity_catalogs (
    catalog_id VARCHAR(96) NOT NULL,
    catalog_version VARCHAR(24) NOT NULL,
    status VARCHAR(16) NOT NULL,
    catalog_schema_version SMALLINT NOT NULL,
    manifest JSONB NOT NULL,
    catalog_hash VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    PRIMARY KEY (catalog_id, catalog_version),
    CONSTRAINT city_realtime_character_activity_catalog_check CHECK (
        catalog_id ~ '^city-[a-z][a-z0-9_.-]{2,95}$'
        AND catalog_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND status IN ('published', 'retired')
        AND catalog_schema_version = 1
        AND jsonb_typeof(manifest) = 'object'
        AND jsonb_typeof(manifest -> 'activities') = 'array'
        AND manifest::TEXT !~* '"(prompt|provider_url|api_key|secret|memory|response_body)"[[:space:]]*:'
        AND catalog_hash ~ '^[0-9a-f]{64}$'
        AND catalog_hash = encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex')
        AND ((status = 'published' AND published_at IS NOT NULL)
             OR status = 'retired')
    )
);

-- Effects remain finite, declarative, and server-interpreted. They are not
-- prompts and do not carry provider/model configuration. The first catalogue
-- intentionally gives a small complete life loop: rest, civic work, consume
-- a ration, public cleanup, and an auditable rule violation with a fine.
WITH character_core_catalog AS (
    SELECT '{
      "schema_version": 1,
      "credit_unit": "city_credit",
      "activities": [
        {
          "code": "rest.short",
          "category": "recovery",
          "location_requirement": "traversable",
          "public_visibility": false,
          "minimum_interval_us": 5000000,
          "energy_delta": 160,
          "satiety_delta": -20,
          "morale_delta": 10,
          "civic_standing_delta": 0,
          "city_credit_delta": 0,
          "item_code": "",
          "item_quantity_delta": 0
        },
        {
          "code": "work.civic_shift",
          "category": "work",
          "location_requirement": "road_or_sidewalk",
          "public_visibility": true,
          "minimum_interval_us": 5000000,
          "energy_delta": -120,
          "satiety_delta": -75,
          "morale_delta": 20,
          "civic_standing_delta": 10,
          "city_credit_delta": 24,
          "item_code": "",
          "item_quantity_delta": 0
        },
        {
          "code": "consume.ration",
          "category": "consumption",
          "location_requirement": "traversable",
          "public_visibility": false,
          "minimum_interval_us": 5000000,
          "energy_delta": 35,
          "satiety_delta": 260,
          "morale_delta": 10,
          "civic_standing_delta": 0,
          "city_credit_delta": 0,
          "item_code": "item.food.ration",
          "item_quantity_delta": -1
        },
        {
          "code": "civic.cleanup",
          "category": "civic",
          "location_requirement": "road_or_sidewalk",
          "public_visibility": true,
          "minimum_interval_us": 5000000,
          "energy_delta": -70,
          "satiety_delta": -50,
          "morale_delta": 30,
          "civic_standing_delta": 20,
          "city_credit_delta": 10,
          "item_code": "",
          "item_quantity_delta": 0
        },
        {
          "code": "conduct.disruption",
          "category": "conduct",
          "location_requirement": "road_or_sidewalk",
          "public_visibility": true,
          "minimum_interval_us": 5000000,
          "energy_delta": -15,
          "satiety_delta": -15,
          "morale_delta": -50,
          "civic_standing_delta": -140,
          "city_credit_delta": -12,
          "item_code": "",
          "item_quantity_delta": 0,
          "law": {
            "rule_code": "rule.public_disruption",
            "disposition": "fine",
            "penalty_city_credit_units": 12,
            "standing_delta_milli": -140
          }
        }
      ]
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_character_activity_catalogs
    (catalog_id, catalog_version, status, catalog_schema_version, manifest, catalog_hash, published_at)
SELECT 'city-realtime-character-core', '1.0.0', 'published', 1, manifest,
       encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex'), NOW()
FROM character_core_catalog
ON CONFLICT (catalog_id, catalog_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_realtime_character_activity_world_bindings (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_realtime_agent_world_bindings(world_id) ON DELETE RESTRICT,
    catalog_id VARCHAR(96) NOT NULL,
    catalog_version VARCHAR(24) NOT NULL,
    catalog_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    genesis_frame_sequence BIGINT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_character_activity_binding_catalog_fk
        FOREIGN KEY (catalog_id, catalog_version)
        REFERENCES city_realtime_character_activity_catalogs(catalog_id, catalog_version)
        ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_activity_binding_frame_fk
        FOREIGN KEY (world_id, genesis_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_activity_binding_check CHECK (
        genesis_frame_sequence = 0
        AND catalog_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_profiles (
    world_id BIGINT NOT NULL
        REFERENCES city_realtime_character_activity_world_bindings(world_id) ON DELETE RESTRICT,
    actor_code VARCHAR(96) NOT NULL,
    energy_milli INTEGER NOT NULL,
    satiety_milli INTEGER NOT NULL,
    morale_milli INTEGER NOT NULL,
    civic_standing_milli INTEGER NOT NULL,
    city_credit_units BIGINT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    activity_revision BIGINT NOT NULL DEFAULT 0,
    law_revision BIGINT NOT NULL DEFAULT 0,
    spawned_frame_sequence BIGINT NOT NULL,
    last_frame_sequence BIGINT NOT NULL,
    last_activity_world_time_us BIGINT NOT NULL DEFAULT 0,
    state_hash VARCHAR(64) NOT NULL,
    activity_event_chain_hash VARCHAR(64) NOT NULL,
    law_event_chain_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code),
    CONSTRAINT city_realtime_character_profile_actor_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_actor_identities(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_profile_spawn_frame_fk
        FOREIGN KEY (world_id, spawned_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_profile_last_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_profile_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND energy_milli BETWEEN 0 AND 1000
        AND satiety_milli BETWEEN 0 AND 1000
        AND morale_milli BETWEEN 0 AND 1000
        AND civic_standing_milli BETWEEN 0 AND 1000
        AND city_credit_units BETWEEN -100000000 AND 100000000
        AND revision > 0
        AND activity_revision >= 0 AND activity_revision < revision
        AND law_revision >= 0 AND law_revision < revision
        AND spawned_frame_sequence > 0
        AND last_frame_sequence >= spawned_frame_sequence
        AND last_activity_world_time_us >= 0
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND activity_event_chain_hash ~ '^[0-9a-f]{64}$'
        AND law_event_chain_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_inventory_stacks (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    item_code VARCHAR(64) NOT NULL,
    quantity BIGINT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    last_frame_sequence BIGINT NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, item_code),
    CONSTRAINT city_realtime_character_inventory_profile_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_character_profiles(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_inventory_frame_fk
        FOREIGN KEY (world_id, last_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_inventory_check CHECK (
        item_code ~ '^item[.][a-z][a-z0-9_.-]{1,59}$'
        AND quantity BETWEEN 0 AND 1000000
        AND revision > 0
        AND last_frame_sequence > 0
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_activity_events (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    activity_code VARCHAR(64) NOT NULL,
    category_code VARCHAR(32) NOT NULL,
    outcome VARCHAR(16) NOT NULL,
    public_visibility BOOLEAN NOT NULL,
    energy_delta_milli INTEGER NOT NULL,
    satiety_delta_milli INTEGER NOT NULL,
    morale_delta_milli INTEGER NOT NULL,
    civic_standing_delta_milli INTEGER NOT NULL,
    city_credit_delta_units BIGINT NOT NULL,
    item_code VARCHAR(64),
    item_quantity_delta BIGINT NOT NULL DEFAULT 0,
    law_case_code VARCHAR(64),
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, event_sequence),
    CONSTRAINT city_realtime_character_activity_event_profile_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_character_profiles(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_activity_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_activity_event_check CHECK (
        event_sequence > 0
        AND frame_sequence > 0
        AND activity_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND category_code ~ '^[a-z][a-z0-9_.-]{1,31}$'
        AND outcome IN ('completed', 'penalized')
        AND energy_delta_milli BETWEEN -1000 AND 1000
        AND satiety_delta_milli BETWEEN -1000 AND 1000
        AND morale_delta_milli BETWEEN -1000 AND 1000
        AND civic_standing_delta_milli BETWEEN -1000 AND 1000
        AND city_credit_delta_units BETWEEN -1000000 AND 1000000
        AND (item_code IS NULL OR item_code ~ '^item[.][a-z][a-z0-9_.-]{1,59}$')
        AND item_quantity_delta BETWEEN -1000000 AND 1000000
        AND (law_case_code IS NULL OR law_case_code ~ '^law[.][0-9a-f]{16}[.][1-9][0-9]*$')
        AND previous_event_hash ~ '^[0-9a-f]{64}$'
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_activity_events_public_frame
    ON city_realtime_character_activity_events (world_id, frame_sequence DESC, actor_code, event_sequence DESC)
    WHERE public_visibility;

CREATE TABLE IF NOT EXISTS city_realtime_character_law_events (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    event_sequence BIGINT NOT NULL,
    activity_event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    case_code VARCHAR(64) NOT NULL,
    rule_code VARCHAR(64) NOT NULL,
    disposition VARCHAR(16) NOT NULL,
    penalty_city_credit_units BIGINT NOT NULL,
    standing_delta_milli INTEGER NOT NULL,
    public_visibility BOOLEAN NOT NULL,
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, event_sequence),
    CONSTRAINT city_realtime_character_law_event_activity_fk
        FOREIGN KEY (world_id, actor_code, activity_event_sequence)
        REFERENCES city_realtime_character_activity_events(world_id, actor_code, event_sequence)
        ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_law_event_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_law_event_case_unique UNIQUE (world_id, case_code),
    CONSTRAINT city_realtime_character_law_event_check CHECK (
        event_sequence > 0
        AND activity_event_sequence > 0
        AND frame_sequence > 0
        AND case_code ~ '^law[.][0-9a-f]{16}[.][1-9][0-9]*$'
        AND rule_code ~ '^rule[.][a-z][a-z0-9_.-]{1,58}$'
        AND disposition IN ('warning', 'fine', 'service')
        AND penalty_city_credit_units BETWEEN 0 AND 1000000
        AND standing_delta_milli BETWEEN -1000 AND 0
        AND previous_event_hash ~ '^[0-9a-f]{64}$'
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_law_events_public_frame
    ON city_realtime_character_law_events (world_id, frame_sequence DESC, actor_code, event_sequence DESC)
    WHERE public_visibility;

-- Extend the already-sealed receipt envelope; only a server-created activity
-- result may use the third action type.
ALTER TABLE city_realtime_character_action_receipts
    DROP CONSTRAINT IF EXISTS city_realtime_character_action_receipt_check;

ALTER TABLE city_realtime_character_action_receipts
    ADD CONSTRAINT city_realtime_character_action_receipt_check CHECK (
        idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$'
        AND actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND action_type IN ('character.create', 'character.move', 'character.activity')
        AND request_hash ~ '^[0-9a-f]{64}$'
        AND frame_sequence > 0
        AND jsonb_typeof(result_payload) = 'object'
        AND result_payload::TEXT !~* '"(email|username|owner_user_id|prompt|provider|api_key|secret|memory|response)"[[:space:]]*:'
        AND result_hash ~ '^[0-9a-f]{64}$'
        AND result_hash = encode(sha256(convert_to(result_payload::text, 'UTF8')), 'hex')
    );

CREATE OR REPLACE FUNCTION city_realtime_character_activity_initialization_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_activity_initialize_world_id', TRUE) = target_world_id::text
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

CREATE OR REPLACE FUNCTION city_realtime_character_activity_mutation_enabled(
    target_world_id BIGINT,
    target_frame_sequence BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_character_activity_world_id', TRUE) = target_world_id::text
       AND current_setting('sub2api.city_realtime_character_activity_frame_sequence', TRUE) = target_frame_sequence::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_world_time_states time_state ON time_state.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND world.status = 'running'
             AND time_state.temporal_engine_version = 'city-openworld-realtime-v2'
             AND time_state.timeline_frame_sequence + 1 = target_frame_sequence
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_activity_catalog()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character activity catalogs are immutable versions'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_activity_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_hash VARCHAR(64);
    expected_status VARCHAR(16);
    expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_character_activity_initialization_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime character activity binding is immutable outside genesis initialization'
            USING ERRCODE = '55000';
    END IF;
    SELECT catalog_hash, status INTO expected_hash, expected_status
    FROM city_realtime_character_activity_catalogs
    WHERE catalog_id = NEW.catalog_id AND catalog_version = NEW.catalog_version;
    IF NOT FOUND OR expected_status <> 'published' OR expected_hash <> NEW.catalog_hash THEN
        RAISE EXCEPTION 'city realtime character activity binding references an invalid catalog'
            USING ERRCODE = '23514';
    END IF;
    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-character-activity-binding-v1',
        NEW.catalog_id, NEW.catalog_version, NEW.catalog_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime character activity binding hash mismatch'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_profile()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    actor_kind_value VARCHAR(16);
    agent_count BIGINT;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NOT city_realtime_character_activity_mutation_enabled(NEW.world_id, NEW.last_frame_sequence)
           OR NEW.revision <> 1 OR NEW.activity_revision <> 0 OR NEW.law_revision <> 0
           OR NEW.spawned_frame_sequence <> NEW.last_frame_sequence THEN
            RAISE EXCEPTION 'city realtime character profiles may be created only through a sealed character frame'
                USING ERRCODE = '55000';
        END IF;
        SELECT actor_kind INTO actor_kind_value
        FROM city_realtime_actor_identities
        WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code;
        SELECT COUNT(*) INTO agent_count
        FROM city_realtime_agent_instances
        WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code
          AND agent_subtype = 'character.user' AND lifecycle_status = 'active';
        IF actor_kind_value <> 'character' OR agent_count <> 1 THEN
            RAISE EXCEPTION 'city realtime character profile must belong to one active user character agent'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'UPDATE'
       AND city_realtime_character_activity_mutation_enabled(NEW.world_id, NEW.last_frame_sequence)
       AND NEW.revision = OLD.revision + 1
       AND NEW.last_frame_sequence > OLD.last_frame_sequence
       AND NEW.last_activity_world_time_us >= OLD.last_activity_world_time_us
       AND NEW.state_hash <> OLD.state_hash
       AND NEW.spawned_frame_sequence = OLD.spawned_frame_sequence
       AND NEW.metadata = OLD.metadata
       AND (
           (NEW.activity_revision = OLD.activity_revision + 1
            AND NEW.activity_event_chain_hash <> OLD.activity_event_chain_hash
            AND EXISTS (
                SELECT 1 FROM city_realtime_character_activity_events event
                WHERE event.world_id = NEW.world_id AND event.actor_code = NEW.actor_code
                  AND event.event_sequence = NEW.activity_revision
                  AND event.frame_sequence = NEW.last_frame_sequence
                  AND event.event_hash = NEW.activity_event_chain_hash
            ))
           OR
           (NEW.activity_revision = OLD.activity_revision
            AND NEW.activity_event_chain_hash = OLD.activity_event_chain_hash)
       )
       AND (
           (NEW.law_revision = OLD.law_revision + 1
            AND NEW.law_event_chain_hash <> OLD.law_event_chain_hash
            AND EXISTS (
                SELECT 1 FROM city_realtime_character_law_events event
                WHERE event.world_id = NEW.world_id AND event.actor_code = NEW.actor_code
                  AND event.event_sequence = NEW.law_revision
                  AND event.frame_sequence = NEW.last_frame_sequence
                  AND event.event_hash = NEW.law_event_chain_hash
            ))
           OR
           (NEW.law_revision = OLD.law_revision
            AND NEW.law_event_chain_hash = OLD.law_event_chain_hash)
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character profiles may change only through the sealed activity reducer'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_inventory_stack()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_character_activity_mutation_enabled(NEW.world_id, NEW.last_frame_sequence)
       AND NEW.revision = 1 THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_character_activity_mutation_enabled(NEW.world_id, NEW.last_frame_sequence)
       AND NEW.revision = OLD.revision + 1
       AND NEW.last_frame_sequence > OLD.last_frame_sequence
       AND NEW.state_hash <> OLD.state_hash
       AND NEW.world_id = OLD.world_id
       AND NEW.actor_code = OLD.actor_code
       AND NEW.item_code = OLD.item_code
       AND NEW.metadata = OLD.metadata THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character inventory may change only through the sealed activity reducer'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_activity_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    profile_activity_revision BIGINT;
    profile_last_frame BIGINT;
    profile_chain_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT'
       OR NOT city_realtime_character_activity_mutation_enabled(NEW.world_id, NEW.frame_sequence) THEN
        RAISE EXCEPTION 'city realtime character activity events are append-only sealed facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT activity_revision, last_frame_sequence, activity_event_chain_hash
    INTO profile_activity_revision, profile_last_frame, profile_chain_hash
    FROM city_realtime_character_profiles
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code;
    IF NOT FOUND
       OR NEW.event_sequence <> profile_activity_revision + 1
       OR NEW.frame_sequence <= profile_last_frame
       OR NEW.previous_event_hash <> profile_chain_hash THEN
        RAISE EXCEPTION 'city realtime character activity event chain mismatch'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_law_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    profile_law_revision BIGINT;
    profile_last_frame BIGINT;
    profile_chain_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT'
       OR NOT city_realtime_character_activity_mutation_enabled(NEW.world_id, NEW.frame_sequence) THEN
        RAISE EXCEPTION 'city realtime character law events are append-only sealed facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT law_revision, last_frame_sequence, law_event_chain_hash
    INTO profile_law_revision, profile_last_frame, profile_chain_hash
    FROM city_realtime_character_profiles
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code;
    IF NOT FOUND
       OR NEW.event_sequence <> profile_law_revision + 1
       OR NEW.frame_sequence <= profile_last_frame
       OR NEW.previous_event_hash <> profile_chain_hash THEN
        RAISE EXCEPTION 'city realtime character law event chain mismatch'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_activity_catalog_guard
    ON city_realtime_character_activity_catalogs;
CREATE TRIGGER city_realtime_character_activity_catalog_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_activity_catalogs
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_activity_catalog();

DROP TRIGGER IF EXISTS city_realtime_character_activity_binding_guard
    ON city_realtime_character_activity_world_bindings;
CREATE TRIGGER city_realtime_character_activity_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_activity_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_activity_binding();

DROP TRIGGER IF EXISTS city_realtime_character_profile_guard
    ON city_realtime_character_profiles;
CREATE TRIGGER city_realtime_character_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_profile();

DROP TRIGGER IF EXISTS city_realtime_character_inventory_stack_guard
    ON city_realtime_character_inventory_stacks;
CREATE TRIGGER city_realtime_character_inventory_stack_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_inventory_stacks
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_inventory_stack();

DROP TRIGGER IF EXISTS city_realtime_character_activity_event_guard
    ON city_realtime_character_activity_events;
CREATE TRIGGER city_realtime_character_activity_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_activity_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_activity_event();

DROP TRIGGER IF EXISTS city_realtime_character_law_event_guard
    ON city_realtime_character_law_events;
CREATE TRIGGER city_realtime_character_law_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_law_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_law_event();

COMMENT ON TABLE city_realtime_character_activity_catalogs IS
    'Immutable finite character activity catalog versions. It contains no prompts, provider routes, private memory, or model responses.';
COMMENT ON TABLE city_realtime_character_activity_world_bindings IS
    'Genesis-pinned character activity catalog for a realtime-v2 shared world.';
COMMENT ON TABLE city_realtime_character_profiles IS
    'Owner-private life state for a public player Actor; city_credit is local and nonredeemable until a separate verified reward outbox is introduced.';
COMMENT ON TABLE city_realtime_character_activity_events IS
    'Append-only character action facts. Public rows are member-safe summaries; private effects are returned only to the owner.';
COMMENT ON TABLE city_realtime_character_law_events IS
    'Append-only rule outcome chain derived only by the sealed character activity reducer.';
