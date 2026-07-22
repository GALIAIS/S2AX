-- Character progression 1.3.0 is a successor catalog and sealed private
-- state family. It adds selectable archetypes, bounded attributes, generic
-- category-scoped role assignments, and append-only progression facts without
-- mutating 1.0/1.1/1.2 worlds or platform-wallet balances.

WITH character_core_catalog_v130 AS (
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
          "minimum_energy_milli": 0,
          "minimum_satiety_milli": 0,
          "energy_delta": 160,
          "satiety_delta": -20,
          "morale_delta": 10,
          "civic_standing_delta": 0,
          "city_credit_delta": 0,
          "item_code": "",
          "item_quantity_delta": 0,
          "progression": {"experience_rewards": [{"attribute_code": "vitality", "experience_units": 4}]}
        },
        {
          "code": "work.civic_shift",
          "category": "work",
          "location_requirement": "road_or_sidewalk",
          "public_visibility": true,
          "minimum_interval_us": 5000000,
          "minimum_energy_milli": 160,
          "minimum_satiety_milli": 120,
          "energy_delta": -120,
          "satiety_delta": -75,
          "morale_delta": 20,
          "civic_standing_delta": 10,
          "city_credit_delta": 24,
          "item_code": "item.food.ration",
          "item_quantity_delta": 1,
          "progression": {"experience_rewards": [
            {"attribute_code": "communication", "experience_units": 12},
            {"attribute_code": "discipline", "experience_units": 24}
          ]}
        },
        {
          "code": "consume.ration",
          "category": "consumption",
          "location_requirement": "traversable",
          "public_visibility": false,
          "minimum_interval_us": 5000000,
          "minimum_energy_milli": 0,
          "minimum_satiety_milli": 0,
          "energy_delta": 35,
          "satiety_delta": 260,
          "morale_delta": 10,
          "civic_standing_delta": 0,
          "city_credit_delta": 0,
          "item_code": "item.food.ration",
          "item_quantity_delta": -1,
          "progression": {"experience_rewards": [{"attribute_code": "vitality", "experience_units": 2}]}
        },
        {
          "code": "civic.cleanup",
          "category": "civic",
          "location_requirement": "road_or_sidewalk",
          "public_visibility": true,
          "minimum_interval_us": 5000000,
          "minimum_energy_milli": 100,
          "minimum_satiety_milli": 80,
          "energy_delta": -70,
          "satiety_delta": -50,
          "morale_delta": 30,
          "civic_standing_delta": 20,
          "city_credit_delta": 10,
          "item_code": "",
          "item_quantity_delta": 0,
          "progression": {"experience_rewards": [
            {"attribute_code": "coordination", "experience_units": 26},
            {"attribute_code": "vitality", "experience_units": 12}
          ]}
        },
        {
          "code": "conduct.disruption",
          "category": "conduct",
          "location_requirement": "road_or_sidewalk",
          "public_visibility": true,
          "minimum_interval_us": 5000000,
          "minimum_energy_milli": 40,
          "minimum_satiety_milli": 40,
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
        },
        {
          "code": "study.public_service",
          "category": "training",
          "location_requirement": "traversable",
          "public_visibility": false,
          "minimum_interval_us": 5000000,
          "minimum_energy_milli": 70,
          "minimum_satiety_milli": 60,
          "energy_delta": -35,
          "satiety_delta": -25,
          "morale_delta": 12,
          "civic_standing_delta": 0,
          "city_credit_delta": -3,
          "item_code": "",
          "item_quantity_delta": 0,
          "progression": {"experience_rewards": [
            {"attribute_code": "communication", "experience_units": 12},
            {"attribute_code": "reasoning", "experience_units": 24}
          ]}
        },
        {
          "code": "work.civic_service",
          "category": "work",
          "location_requirement": "road_or_sidewalk",
          "public_visibility": true,
          "minimum_interval_us": 5000000,
          "minimum_energy_milli": 180,
          "minimum_satiety_milli": 140,
          "energy_delta": -135,
          "satiety_delta": -82,
          "morale_delta": 24,
          "civic_standing_delta": 18,
          "city_credit_delta": 42,
          "item_code": "item.food.ration",
          "item_quantity_delta": 1,
          "progression": {
            "required_role_codes": ["profession.civic_aide"],
            "experience_rewards": [
              {"attribute_code": "communication", "experience_units": 28},
              {"attribute_code": "discipline", "experience_units": 32}
            ]
          }
        },
        {
          "code": "work.maintenance_shift",
          "category": "work",
          "location_requirement": "road_or_sidewalk",
          "public_visibility": true,
          "minimum_interval_us": 5000000,
          "minimum_energy_milli": 190,
          "minimum_satiety_milli": 145,
          "energy_delta": -150,
          "satiety_delta": -90,
          "morale_delta": 18,
          "civic_standing_delta": 14,
          "city_credit_delta": 48,
          "item_code": "item.food.ration",
          "item_quantity_delta": 1,
          "progression": {
            "required_role_codes": ["profession.maintenance_worker"],
            "experience_rewards": [
              {"attribute_code": "coordination", "experience_units": 32},
              {"attribute_code": "vitality", "experience_units": 26}
            ]
          }
        }
      ],
      "metabolism": {
        "interval_us": 60000000,
        "energy_delta": -6,
        "satiety_delta": -8,
        "morale_delta": -2
      },
      "progression": {
        "schema_version": 1,
        "attributes": [
          {"code": "communication", "experience_per_value_milli": 5, "maximum_experience_units": 250000},
          {"code": "coordination", "experience_per_value_milli": 5, "maximum_experience_units": 250000},
          {"code": "discipline", "experience_per_value_milli": 5, "maximum_experience_units": 250000},
          {"code": "reasoning", "experience_per_value_milli": 5, "maximum_experience_units": 250000},
          {"code": "vitality", "experience_per_value_milli": 5, "maximum_experience_units": 250000}
        ],
        "archetypes": [
          {
            "code": "resident.generalist",
            "initial_role_code": "profession.resident",
            "initial_attributes": [
              {"attribute_code": "communication", "initial_value_milli": 410},
              {"attribute_code": "coordination", "initial_value_milli": 430},
              {"attribute_code": "discipline", "initial_value_milli": 420},
              {"attribute_code": "reasoning", "initial_value_milli": 440},
              {"attribute_code": "vitality", "initial_value_milli": 460}
            ]
          },
          {
            "code": "resident.social",
            "initial_role_code": "profession.resident",
            "initial_attributes": [
              {"attribute_code": "communication", "initial_value_milli": 490},
              {"attribute_code": "coordination", "initial_value_milli": 400},
              {"attribute_code": "discipline", "initial_value_milli": 410},
              {"attribute_code": "reasoning", "initial_value_milli": 430},
              {"attribute_code": "vitality", "initial_value_milli": 430}
            ]
          },
          {
            "code": "resident.technical",
            "initial_role_code": "profession.resident",
            "initial_attributes": [
              {"attribute_code": "communication", "initial_value_milli": 380},
              {"attribute_code": "coordination", "initial_value_milli": 480},
              {"attribute_code": "discipline", "initial_value_milli": 430},
              {"attribute_code": "reasoning", "initial_value_milli": 500},
              {"attribute_code": "vitality", "initial_value_milli": 450}
            ]
          }
        ],
        "roles": [
          {
            "code": "profession.civic_aide",
            "category_code": "profession",
            "requirements": {
              "minimum_civic_standing_milli": 820,
              "minimum_total_experience_units": 64,
              "attributes": [
                {"attribute_code": "communication", "minimum_value_milli": 450},
                {"attribute_code": "discipline", "minimum_value_milli": 465}
              ]
            }
          },
          {
            "code": "profession.community_steward",
            "category_code": "profession",
            "requirements": {
              "minimum_civic_standing_milli": 900,
              "minimum_total_experience_units": 240,
              "attributes": [
                {"attribute_code": "communication", "minimum_value_milli": 520},
                {"attribute_code": "discipline", "minimum_value_milli": 520},
                {"attribute_code": "reasoning", "minimum_value_milli": 500}
              ]
            }
          },
          {
            "code": "profession.maintenance_worker",
            "category_code": "profession",
            "requirements": {
              "minimum_civic_standing_milli": 800,
              "minimum_total_experience_units": 80,
              "attributes": [
                {"attribute_code": "coordination", "minimum_value_milli": 470},
                {"attribute_code": "vitality", "minimum_value_milli": 480}
              ]
            }
          },
          {"code": "profession.resident", "category_code": "profession", "requirements": {}}
        ]
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_character_activity_catalogs
    (catalog_id, catalog_version, status, catalog_schema_version, manifest, catalog_hash, published_at)
SELECT 'city-realtime-character-core', '1.3.0', 'published', 1, manifest,
       encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex'), NOW()
FROM character_core_catalog_v130
ON CONFLICT (catalog_id, catalog_version) DO NOTHING;

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
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_metabolism","realtime_character_metabolism_due_events"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_portals","realtime_character_interiors"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_character_progression","realtime_character_roles"]'::jsonb
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_progression' THEN capabilities
    ELSE capabilities || '["realtime_character_progression","realtime_character_roles"]'::jsonb
END
WHERE version = 'city-openworld-realtime-v2';

ALTER TABLE city_realtime_character_profiles
    ADD COLUMN IF NOT EXISTS archetype_code VARCHAR(96),
    ADD COLUMN IF NOT EXISTS progression_revision BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS progression_event_chain_hash VARCHAR(64),
    ADD COLUMN IF NOT EXISTS progression_state_hash VARCHAR(64);

ALTER TABLE city_realtime_character_profiles
    DROP CONSTRAINT IF EXISTS city_realtime_character_profile_check;

ALTER TABLE city_realtime_character_profiles
    ADD CONSTRAINT city_realtime_character_profile_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND state_schema_version IN (1, 2, 3)
        AND energy_milli BETWEEN 0 AND 1000
        AND satiety_milli BETWEEN 0 AND 1000
        AND morale_milli BETWEEN 0 AND 1000
        AND civic_standing_milli BETWEEN 0 AND 1000
        AND city_credit_units BETWEEN -100000000 AND 100000000
        AND revision > 0
        AND activity_revision >= 0 AND activity_revision < revision
        AND law_revision >= 0 AND law_revision < revision
        AND metabolism_revision >= 0 AND metabolism_revision < revision
        AND progression_revision >= 0 AND progression_revision < revision
        AND spawned_frame_sequence > 0
        AND last_frame_sequence >= spawned_frame_sequence
        AND last_activity_world_time_us >= 0
        AND last_metabolism_world_time_us >= 0
        AND (
            (state_schema_version = 1
             AND metabolism_revision = 0
             AND last_metabolism_world_time_us = 0
             AND progression_revision = 0
             AND archetype_code IS NULL
             AND progression_event_chain_hash IS NULL
             AND progression_state_hash IS NULL)
            OR
            (state_schema_version = 2
             AND progression_revision = 0
             AND archetype_code IS NULL
             AND progression_event_chain_hash IS NULL
             AND progression_state_hash IS NULL)
            OR
            (state_schema_version = 3
             AND archetype_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
             AND progression_event_chain_hash ~ '^[0-9a-f]{64}$'
             AND progression_state_hash ~ '^[0-9a-f]{64}$')
        )
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND activity_event_chain_hash ~ '^[0-9a-f]{64}$'
        AND law_event_chain_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    );

CREATE TABLE IF NOT EXISTS city_realtime_character_attribute_states (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    attribute_code VARCHAR(64) NOT NULL,
    experience_units BIGINT NOT NULL,
    revision BIGINT NOT NULL,
    last_frame_sequence BIGINT NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, attribute_code),
    CONSTRAINT city_realtime_character_attribute_profile_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_character_profiles(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_attribute_check CHECK (
        attribute_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND experience_units BETWEEN 0 AND 250000
        AND revision > 0
        AND last_frame_sequence > 0
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_role_assignments (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    category_code VARCHAR(64) NOT NULL,
    role_code VARCHAR(96) NOT NULL,
    granted_frame_sequence BIGINT NOT NULL,
    revision BIGINT NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, category_code),
    CONSTRAINT city_realtime_character_role_profile_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_character_profiles(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_role_check CHECK (
        category_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND role_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND granted_frame_sequence > 0
        AND revision > 0
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_realtime_character_progression_events (
    world_id BIGINT NOT NULL,
    actor_code VARCHAR(96) NOT NULL,
    event_sequence BIGINT NOT NULL,
    frame_sequence BIGINT NOT NULL,
    event_kind VARCHAR(16) NOT NULL,
    activity_event_sequence BIGINT,
    category_code VARCHAR(64),
    from_role_code VARCHAR(96),
    to_role_code VARCHAR(96),
    experience_deltas JSONB NOT NULL,
    previous_event_hash VARCHAR(64) NOT NULL,
    event_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, actor_code, event_sequence),
    CONSTRAINT city_realtime_character_progression_profile_fk
        FOREIGN KEY (world_id, actor_code)
        REFERENCES city_realtime_character_profiles(world_id, actor_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_character_progression_activity_fk
        FOREIGN KEY (world_id, actor_code, activity_event_sequence)
        REFERENCES city_realtime_character_activity_events(world_id, actor_code, event_sequence)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_progression_frame_fk
        FOREIGN KEY (world_id, frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_realtime_character_progression_event_check CHECK (
        event_sequence > 0
        AND frame_sequence > 0
        AND event_kind IN ('activity', 'role')
        AND jsonb_typeof(experience_deltas) = 'array'
        AND previous_event_hash ~ '^[0-9a-f]{64}$'
        AND event_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
        AND (
            (event_kind = 'activity'
             AND activity_event_sequence IS NOT NULL
             AND category_code IS NULL
             AND from_role_code IS NULL
             AND to_role_code IS NULL)
            OR
            (event_kind = 'role'
             AND activity_event_sequence IS NULL
             AND category_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
             AND (from_role_code IS NULL OR from_role_code ~ '^[a-z][a-z0-9_.-]{1,95}$')
             AND to_role_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
             AND experience_deltas = '[]'::jsonb)
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_character_progression_events_actor_frame
    ON city_realtime_character_progression_events (world_id, actor_code, frame_sequence DESC);

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
           OR NEW.metabolism_revision <> 0 OR NEW.progression_revision <> 0
           OR NEW.state_schema_version NOT IN (1, 2, 3)
           OR (NEW.state_schema_version = 1 AND NEW.last_metabolism_world_time_us <> 0)
           OR (NEW.state_schema_version IN (1, 2) AND (
                NEW.archetype_code IS NOT NULL
                OR NEW.progression_event_chain_hash IS NOT NULL
                OR NEW.progression_state_hash IS NOT NULL))
           OR (NEW.state_schema_version = 3 AND (
                NEW.archetype_code IS NULL
                OR NEW.progression_event_chain_hash IS NULL
                OR NEW.progression_state_hash IS NULL))
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
       AND NEW.state_schema_version = OLD.state_schema_version
       AND NEW.revision = OLD.revision + 1
       AND NEW.last_frame_sequence > OLD.last_frame_sequence
       AND NEW.last_activity_world_time_us >= OLD.last_activity_world_time_us
       AND NEW.state_hash <> OLD.state_hash
       AND NEW.spawned_frame_sequence = OLD.spawned_frame_sequence
       AND NEW.archetype_code IS NOT DISTINCT FROM OLD.archetype_code
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
       )
       AND (
           (NEW.state_schema_version = 1
            AND NEW.metabolism_revision = 0 AND OLD.metabolism_revision = 0
            AND NEW.last_metabolism_world_time_us = 0 AND OLD.last_metabolism_world_time_us = 0)
           OR
           (NEW.state_schema_version IN (2, 3)
            AND (
                (NEW.metabolism_revision = OLD.metabolism_revision + 1
                 AND NEW.last_metabolism_world_time_us > OLD.last_metabolism_world_time_us
                 AND NEW.last_activity_world_time_us = OLD.last_activity_world_time_us
                 AND NEW.activity_revision = OLD.activity_revision
                 AND NEW.law_revision = OLD.law_revision
                 AND NEW.activity_event_chain_hash = OLD.activity_event_chain_hash
                 AND NEW.law_event_chain_hash = OLD.law_event_chain_hash)
                OR
                (NEW.metabolism_revision = OLD.metabolism_revision
                 AND NEW.last_metabolism_world_time_us = OLD.last_metabolism_world_time_us)
            ))
       )
       AND (
           (NEW.state_schema_version IN (1, 2)
            AND NEW.progression_revision = 0 AND OLD.progression_revision = 0
            AND NEW.progression_event_chain_hash IS NULL AND OLD.progression_event_chain_hash IS NULL
            AND NEW.progression_state_hash IS NULL AND OLD.progression_state_hash IS NULL)
           OR
           (NEW.state_schema_version = 3
            AND (
                (NEW.progression_revision = OLD.progression_revision + 1
                 AND NEW.progression_event_chain_hash <> OLD.progression_event_chain_hash
                 AND NEW.progression_state_hash <> OLD.progression_state_hash
                 AND EXISTS (
                    SELECT 1 FROM city_realtime_character_progression_events event
                    WHERE event.world_id = NEW.world_id AND event.actor_code = NEW.actor_code
                      AND event.event_sequence = NEW.progression_revision
                      AND event.frame_sequence = NEW.last_frame_sequence
                      AND event.event_hash = NEW.progression_event_chain_hash
                 ))
                OR
                (NEW.progression_revision = OLD.progression_revision
                 AND NEW.progression_event_chain_hash = OLD.progression_event_chain_hash
                 AND NEW.progression_state_hash = OLD.progression_state_hash)
            ))
       )
       AND (
           NEW.activity_revision > OLD.activity_revision
           OR NEW.law_revision > OLD.law_revision
           OR NEW.metabolism_revision > OLD.metabolism_revision
           OR NEW.progression_revision > OLD.progression_revision
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character profiles may change only through sealed activity, metabolism, or progression reducers'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_attribute_state()
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
       AND NEW.attribute_code = OLD.attribute_code
       AND NEW.metadata = OLD.metadata THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character attributes may change only through sealed progression reducers'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_role_assignment()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_character_activity_mutation_enabled(NEW.world_id, NEW.granted_frame_sequence)
       AND NEW.revision = 1 THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_character_activity_mutation_enabled(NEW.world_id, NEW.granted_frame_sequence)
       AND NEW.revision = OLD.revision + 1
       AND NEW.granted_frame_sequence > OLD.granted_frame_sequence
       AND NEW.state_hash <> OLD.state_hash
       AND NEW.world_id = OLD.world_id
       AND NEW.actor_code = OLD.actor_code
       AND NEW.category_code = OLD.category_code
       AND NEW.metadata = OLD.metadata THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character roles may change only through sealed progression reducers'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_character_progression_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    profile_progression_revision BIGINT;
    profile_last_frame BIGINT;
    profile_chain_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT'
       OR NOT city_realtime_character_activity_mutation_enabled(NEW.world_id, NEW.frame_sequence) THEN
        RAISE EXCEPTION 'city realtime character progression events are append-only sealed facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT progression_revision, last_frame_sequence, progression_event_chain_hash
    INTO profile_progression_revision, profile_last_frame, profile_chain_hash
    FROM city_realtime_character_profiles
    WHERE world_id = NEW.world_id AND actor_code = NEW.actor_code;
    IF NOT FOUND
       OR NEW.event_sequence <> profile_progression_revision + 1
       OR NEW.frame_sequence <= profile_last_frame
       OR NEW.previous_event_hash <> profile_chain_hash THEN
        RAISE EXCEPTION 'city realtime character progression event chain mismatch'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.event_kind = 'activity' AND NOT EXISTS (
        SELECT 1 FROM city_realtime_character_activity_events event
        WHERE event.world_id = NEW.world_id AND event.actor_code = NEW.actor_code
          AND event.event_sequence = NEW.activity_event_sequence
          AND event.frame_sequence = NEW.frame_sequence
    ) THEN
        RAISE EXCEPTION 'city realtime progression activity source mismatch'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.event_kind = 'role' AND NOT EXISTS (
        SELECT 1 FROM city_realtime_character_role_assignments assignment
        WHERE assignment.world_id = NEW.world_id AND assignment.actor_code = NEW.actor_code
          AND assignment.category_code = NEW.category_code
          AND assignment.role_code = NEW.to_role_code
          AND assignment.granted_frame_sequence = NEW.frame_sequence
    ) THEN
        RAISE EXCEPTION 'city realtime progression role source mismatch'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_realtime_character_attribute_state_guard
    ON city_realtime_character_attribute_states;
CREATE TRIGGER city_realtime_character_attribute_state_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_attribute_states
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_attribute_state();

DROP TRIGGER IF EXISTS city_realtime_character_role_assignment_guard
    ON city_realtime_character_role_assignments;
CREATE TRIGGER city_realtime_character_role_assignment_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_role_assignments
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_role_assignment();

DROP TRIGGER IF EXISTS city_realtime_character_progression_event_guard
    ON city_realtime_character_progression_events;
CREATE TRIGGER city_realtime_character_progression_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_character_progression_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_character_progression_event();

ALTER TABLE city_realtime_character_action_receipts
    DROP CONSTRAINT IF EXISTS city_realtime_character_action_receipt_check;

ALTER TABLE city_realtime_character_action_receipts
    ADD CONSTRAINT city_realtime_character_action_receipt_check CHECK (
        idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$'
        AND actor_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND action_type IN ('character.create', 'character.move', 'character.activity', 'character.portal', 'character.role')
        AND request_hash ~ '^[0-9a-f]{64}$'
        AND frame_sequence > 0
        AND jsonb_typeof(result_payload) = 'object'
        AND result_payload::TEXT !~* '"(email|username|owner_user_id|prompt|provider|api_key|secret|memory|response)"[[:space:]]*:'
        AND result_hash ~ '^[0-9a-f]{64}$'
        AND result_hash = encode(sha256(convert_to(result_payload::text, 'UTF8')), 'hex')
    );

COMMENT ON TABLE city_realtime_character_attribute_states IS
    'Owner-private, server-derived attribute experience snapshots. Attribute value is derived from the immutable archetype and catalog curve.';
COMMENT ON TABLE city_realtime_character_role_assignments IS
    'Current category-scoped character roles. Realtime role history is append-only in progression events.';
COMMENT ON TABLE city_realtime_character_progression_events IS
    'Append-only sealed progression chain that links activity rewards and role changes without storing prompts, providers, account identity, or platform-wallet facts.';
