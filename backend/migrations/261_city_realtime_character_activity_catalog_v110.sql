-- Publish a successor catalog instead of editing 1.0.0. Existing worlds stay
-- bound to their original immutable catalog; newly created worlds bind to this
-- version through the server's current catalog selector.
--
-- The added thresholds prevent exhausted characters from repeatedly working or
-- committing civic/conduct actions. A completed civic shift replenishes one
-- ration, closing the initial survival/work loop without touching platform
-- wallets or virtual-currency balances.
WITH character_core_catalog_v110 AS (
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
      ]
    }'::jsonb AS manifest
)
INSERT INTO city_realtime_character_activity_catalogs
    (catalog_id, catalog_version, status, catalog_schema_version, manifest, catalog_hash, published_at)
SELECT 'city-realtime-character-core', '1.1.0', 'published', 1, manifest,
       encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex'), NOW()
FROM character_core_catalog_v110
ON CONFLICT (catalog_id, catalog_version) DO NOTHING;
