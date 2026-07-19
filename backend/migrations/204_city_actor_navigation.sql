-- F7.9 deterministic terrain, building, portal, and actor-occupancy navigation.
-- Navigation adds command semantics and a bounded read model; it deliberately
-- reuses the F7.7 canonical projections and therefore creates no mutable table.

CREATE OR REPLACE FUNCTION migration_204_replace_function(
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
        RAISE EXCEPTION 'migration 204 predecessor function % does not contain expected text', target
            USING ERRCODE = '23514';
    END IF;
    BEGIN
        EXECUTE patched;
    EXCEPTION WHEN OTHERS THEN
        RAISE EXCEPTION 'migration 204 failed to extend predecessor function % at %: %',
            target, needle, SQLERRM
            USING ERRCODE = SQLSTATE;
    END;
END;
$migration$;

SELECT migration_204_replace_function(
    'world_runtime_bootstrap_write_enabled(bigint)'::REGPROCEDURE,
    $$world.simulation_version IN ('city-f7-v5', 'city-f7-v6')$$,
    $$world.simulation_version IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$
);
SELECT migration_204_replace_function(
    'guard_world_runtime_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6')$$,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$
);
SELECT migration_204_replace_function(
    'assert_world_runtime_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6')$$,
    $$world_version NOT IN ('city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$
);
SELECT migration_204_replace_function(
    'assert_world_runtime_foundation(bigint)'::REGPROCEDURE,
    $$CASE world_version WHEN 'city-f7-v6' THEN '1.1.0' ELSE '1.0.0' END$$,
    $$CASE WHEN world_version IN ('city-f7-v6', 'city-f7-v7') THEN '1.1.0' ELSE '1.0.0' END$$
);
SELECT migration_204_replace_function(
    'guard_city_enterprise_location_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$
);
SELECT migration_204_replace_function(
    'assert_city_enterprise_location_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$,
    $$world_version NOT IN ('city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$
);
SELECT migration_204_replace_function(
    'guard_city_development_fact_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$
);
SELECT migration_204_replace_function(
    'assert_city_development_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$,
    $$world_version NOT IN ('city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$
);
SELECT migration_204_replace_function(
    'assert_city_land_foundation(bigint)'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$,
    $$world_version NOT IN ('city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$
);
SELECT migration_204_replace_function(
    'guard_city_spatial_mutation_insert()'::REGPROCEDURE,
    $$world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$,
    $$world_version NOT IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$
);
SELECT migration_204_replace_function(
    'assert_city_spatial_foundation(bigint)'::REGPROCEDURE,
    $$world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6')$$,
    $$world_version IN ('city-f7-v1', 'city-f7-v2', 'city-f7-v3', 'city-f7-v4', 'city-f7-v5', 'city-f7-v6', 'city-f7-v7')$$
);
SELECT migration_204_replace_function(
    'assert_world_actor_spatial_control_foundation(bigint)'::REGPROCEDURE,
    $$IF world_version <> 'city-f7-v6' THEN$$,
    $$IF world_version NOT IN ('city-f7-v6', 'city-f7-v7') THEN$$
);

DROP FUNCTION migration_204_replace_function(REGPROCEDURE, TEXT, TEXT);

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f7-v7', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","spatial","development","enterprise_location","world_runtime","actor_spatial_control","actor_navigation","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f7-v6', 'city-f7-v7', 'f7_v6_to_f7_v7')
ON CONFLICT (from_version, to_version) DO NOTHING;
