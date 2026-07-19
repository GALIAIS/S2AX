-- Backfill the immutable glyph-rule binding for installations that applied
-- the initial V2 genesis migration before rule-set pinning was introduced.
-- New installations receive the same columns directly from migration 211.

ALTER TABLE city_open_world_bindings
    ADD COLUMN IF NOT EXISTS rule_set_id VARCHAR(64),
    ADD COLUMN IF NOT EXISTS rule_set_version VARCHAR(24),
    ADD COLUMN IF NOT EXISTS rule_set_hash VARCHAR(64);

ALTER TABLE city_open_world_bindings DISABLE TRIGGER city_open_world_binding_guard;

UPDATE city_open_world_bindings
SET rule_set_id = COALESCE(rule_set_id, 'sub2api-classic'),
    rule_set_version = COALESCE(rule_set_version, '1.0.0'),
    rule_set_hash = COALESCE(rule_set_hash, '136ce6b71a6ebd0f9db4fdfe2662dc7530485330e565e0a7feebcec4399b5277')
WHERE rule_set_id IS NULL OR rule_set_version IS NULL OR rule_set_hash IS NULL;

ALTER TABLE city_open_world_bindings ENABLE TRIGGER city_open_world_binding_guard;

ALTER TABLE city_open_world_bindings
    ALTER COLUMN rule_set_id SET NOT NULL,
    ALTER COLUMN rule_set_version SET NOT NULL,
    ALTER COLUMN rule_set_hash SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'city_open_world_binding_rule_set_identity_check'
    ) THEN
        ALTER TABLE city_open_world_bindings
            ADD CONSTRAINT city_open_world_binding_rule_set_identity_check CHECK (
                rule_set_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
                AND rule_set_version ~ '^[0-9]+\\.[0-9]+\\.[0-9]+$'
                AND rule_set_hash ~ '^[0-9a-f]{64}$'
            );
    END IF;
END;
$$;
