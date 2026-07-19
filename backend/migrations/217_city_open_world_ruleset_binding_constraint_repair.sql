-- Migration 212 accidentally stored a doubled regular-expression escape for
-- rule_set_version.  PostgreSQL then expects literal backslashes in an
-- otherwise valid semantic version and rejects every new open-world binding.
-- Keep historical migrations immutable and replace only the defective
-- constraint for both fresh and already-upgraded installations.

ALTER TABLE city_open_world_bindings
    DROP CONSTRAINT IF EXISTS city_open_world_binding_rule_set_identity_check;

ALTER TABLE city_open_world_bindings
    ADD CONSTRAINT city_open_world_binding_rule_set_identity_check CHECK (
        rule_set_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND rule_set_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND rule_set_hash ~ '^[0-9a-f]{64}$'
    );
