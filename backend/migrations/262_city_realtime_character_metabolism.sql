-- Character life catalog 1.2.0 adds a bounded, server-scheduled passive
-- needs reducer. Existing worlds keep their pinned 1.0.0/1.1.0 catalogs and
-- schema-1 profile hashes unchanged; only newly created worlds bind this
-- successor through the current catalog selector.
-- The only corresponding queue fact is system.realtime.character_metabolism;
-- it is created and resolved by server-owned realtime reducers.

WITH character_core_catalog_v120 AS (
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
          "item_quantity_delta": 0
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
          "item_quantity_delta": 1
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
          "item_quantity_delta": -1
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
          "item_quantity_delta": 0
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
        }
      ],
      "metabolism": {
        "interval_us": 60000000,
        "energy_delta": -6,
        "satiety_delta": -8,
        "morale_delta": -2
      }
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_character_activity_catalogs
    (catalog_id, catalog_version, status, catalog_schema_version, manifest, catalog_hash, published_at)
SELECT 'city-realtime-character-core', '1.2.0', 'published', 1, manifest,
       encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex'), NOW()
FROM character_core_catalog_v120
ON CONFLICT (catalog_id, catalog_version) DO NOTHING;

-- The engine capability is additive. The profile's catalog binding, not this
-- global capability marker, decides whether an individual historic world is
-- eligible to run passive metabolism.
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
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

UPDATE city_engine_versions
SET capabilities = CASE
    WHEN capabilities ? 'realtime_character_metabolism' THEN capabilities
    ELSE capabilities ||
        '["realtime_character_metabolism","realtime_character_metabolism_due_events"]'::jsonb
END
WHERE version = 'city-openworld-realtime-v2';

-- Schema 1 retains the exact legacy profile hash payload. Schema 2 commits
-- the passive reducer head, while its immutable binding is kept separately in
-- city_realtime_character_activity_world_bindings.
ALTER TABLE city_realtime_character_profiles
    ADD COLUMN IF NOT EXISTS state_schema_version SMALLINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS metabolism_revision BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_metabolism_world_time_us BIGINT NOT NULL DEFAULT 0;

ALTER TABLE city_realtime_character_profiles
    DROP CONSTRAINT IF EXISTS city_realtime_character_profile_check;

ALTER TABLE city_realtime_character_profiles
    ADD CONSTRAINT city_realtime_character_profile_check CHECK (
        actor_code ~ '^character[.]player[.][0-9a-f]{32}$'
        AND state_schema_version IN (1, 2)
        AND energy_milli BETWEEN 0 AND 1000
        AND satiety_milli BETWEEN 0 AND 1000
        AND morale_milli BETWEEN 0 AND 1000
        AND civic_standing_milli BETWEEN 0 AND 1000
        AND city_credit_units BETWEEN -100000000 AND 100000000
        AND revision > 0
        AND activity_revision >= 0 AND activity_revision < revision
        AND law_revision >= 0 AND law_revision < revision
        AND metabolism_revision >= 0 AND metabolism_revision < revision
        AND spawned_frame_sequence > 0
        AND last_frame_sequence >= spawned_frame_sequence
        AND last_activity_world_time_us >= 0
        AND last_metabolism_world_time_us >= 0
        AND (
            (state_schema_version = 1
             AND metabolism_revision = 0
             AND last_metabolism_world_time_us = 0)
            OR state_schema_version = 2
        )
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND activity_event_chain_hash ~ '^[0-9a-f]{64}$'
        AND law_event_chain_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    );

-- The mutation guard permits exactly one reducer family per profile update:
-- activity/law facts or the passive metabolism head. It intentionally does
-- not allow a general needs write, a schema transition, or direct wallet use.
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
           OR NEW.metabolism_revision <> 0
           OR NEW.state_schema_version NOT IN (1, 2)
           OR (NEW.state_schema_version = 1 AND NEW.last_metabolism_world_time_us <> 0)
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
       AND NEW.metadata = OLD.metadata
       AND (
           NEW.activity_revision > OLD.activity_revision
           OR NEW.law_revision > OLD.law_revision
           OR NEW.metabolism_revision > OLD.metabolism_revision
       )
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
            AND NEW.metabolism_revision = 0
            AND OLD.metabolism_revision = 0
            AND NEW.last_metabolism_world_time_us = 0
            AND OLD.last_metabolism_world_time_us = 0)
           OR
           (NEW.state_schema_version = 2
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
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime character profiles may change only through sealed activity or metabolism reducers'
        USING ERRCODE = '55000';
END;
$$;

COMMENT ON COLUMN city_realtime_character_profiles.state_schema_version IS
    'Versioned owner-private profile hash schema. Schema 1 is legacy; schema 2 commits the passive metabolism head.';
COMMENT ON COLUMN city_realtime_character_profiles.metabolism_revision IS
    'Append-only passive-needs reducer revision; it is independent of player activity revision.';
COMMENT ON COLUMN city_realtime_character_profiles.last_metabolism_world_time_us IS
    'Server-owned NTP-derived world time of the last passive character-needs transition.';
COMMENT ON TABLE city_realtime_character_activity_catalogs IS
    'Immutable finite character activity catalog versions, including optional server-only passive metabolism. No prompts, provider routes, private memory, model responses, or virtual currency wallets.';
