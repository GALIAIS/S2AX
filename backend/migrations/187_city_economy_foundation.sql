-- 城市模拟经济基础：世界、成员、独立货币、经济主体和科目表。
-- 城市内部货币与平台 virtual_currencies 完全隔离；金额统一使用 BIGINT 最小单位。

CREATE TABLE IF NOT EXISTS city_worlds (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(80) NOT NULL,
    owner_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    group_id BIGINT REFERENCES groups(id) ON DELETE RESTRICT,
    status VARCHAR(16) NOT NULL DEFAULT 'paused',
    simulation_version VARCHAR(32) NOT NULL,
    seed BIGINT NOT NULL CHECK (seed > 0),
    current_tick BIGINT NOT NULL DEFAULT 0 CHECK (current_tick >= 0),
    simulated_at TIMESTAMPTZ,
    next_tick_at TIMESTAMPTZ,
    speed_multiplier NUMERIC(8, 3) NOT NULL DEFAULT 1.000
        CHECK (speed_multiplier > 0 AND speed_multiplier <= 1000),
    timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
    state_hash VARCHAR(64),
    settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_world_name_check CHECK (char_length(btrim(name)) BETWEEN 1 AND 80),
    CONSTRAINT city_world_status_check CHECK (status IN ('creating', 'running', 'paused', 'failed', 'archived')),
    CONSTRAINT city_world_state_hash_check CHECK (state_hash IS NULL OR state_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT city_world_settings_object_check CHECK (jsonb_typeof(settings) = 'object')
);

-- 首版每位用户只能拥有一个未归档的私人城市；多人/分组城市不受此索引限制。
CREATE UNIQUE INDEX IF NOT EXISTS idx_city_worlds_one_private_active_per_owner
    ON city_worlds (owner_user_id)
    WHERE group_id IS NULL AND status <> 'archived';
CREATE INDEX IF NOT EXISTS idx_city_worlds_group_status
    ON city_worlds (group_id, status, id);
CREATE INDEX IF NOT EXISTS idx_city_worlds_schedule
    ON city_worlds (status, next_tick_at, id)
    WHERE status = 'running';

CREATE TABLE IF NOT EXISTS city_members (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    role VARCHAR(16) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at TIMESTAMPTZ,
    banned_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_member_role_check CHECK (role IN ('owner', 'planner', 'treasurer', 'trader', 'viewer')),
    CONSTRAINT city_member_status_check CHECK (status IN ('active', 'left', 'banned')),
    CONSTRAINT city_member_lifecycle_check CHECK (
        (status = 'active' AND left_at IS NULL AND banned_at IS NULL)
        OR (status = 'left' AND left_at IS NOT NULL AND banned_at IS NULL)
        OR (status = 'banned' AND banned_at IS NOT NULL)
    ),
    CONSTRAINT city_members_world_user_unique UNIQUE (world_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_city_members_user_status
    ON city_members (user_id, status, world_id);

CREATE TABLE IF NOT EXISTS city_monetary_units (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(16) NOT NULL,
    name VARCHAR(64) NOT NULL,
    symbol VARCHAR(16) NOT NULL DEFAULT '',
    scale SMALLINT NOT NULL DEFAULT 2 CHECK (scale BETWEEN 0 AND 8),
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    is_base BOOLEAN NOT NULL DEFAULT FALSE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_monetary_unit_code_check CHECK (code ~ '^[a-z][a-z0-9_]{1,15}$'),
    CONSTRAINT city_monetary_unit_name_check CHECK (char_length(btrim(name)) BETWEEN 1 AND 64),
    CONSTRAINT city_monetary_unit_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT city_monetary_unit_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_monetary_units_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_monetary_units_id_world_unique UNIQUE (id, world_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_monetary_units_one_base_per_world
    ON city_monetary_units (world_id)
    WHERE is_base;

CREATE TABLE IF NOT EXISTS city_economic_entities (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    entity_type VARCHAR(16) NOT NULL,
    code VARCHAR(48) NOT NULL,
    name VARCHAR(96) NOT NULL,
    owner_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_economic_entity_type_check CHECK (entity_type IN ('household', 'firm', 'government', 'clearing')),
    CONSTRAINT city_economic_entity_code_check CHECK (code ~ '^[a-z][a-z0-9_]{1,47}$'),
    CONSTRAINT city_economic_entity_name_check CHECK (char_length(btrim(name)) BETWEEN 1 AND 96),
    CONSTRAINT city_economic_entity_status_check CHECK (status IN ('active', 'inactive', 'insolvent', 'liquidating', 'closed')),
    CONSTRAINT city_economic_entity_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_economic_entities_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_economic_entities_id_world_type_unique UNIQUE (id, world_id, entity_type)
);

CREATE INDEX IF NOT EXISTS idx_city_economic_entities_world_type_status
    ON city_economic_entities (world_id, entity_type, status, id);
CREATE INDEX IF NOT EXISTS idx_city_economic_entities_owner
    ON city_economic_entities (owner_user_id, world_id)
    WHERE owner_user_id IS NOT NULL;

-- 科目模板属于世界和主体类型。新主体必须从同类型模板实例化账户，避免业务代码散落科目定义。
CREATE TABLE IF NOT EXISTS city_account_templates (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    entity_type VARCHAR(16) NOT NULL,
    code VARCHAR(48) NOT NULL,
    name VARCHAR(96) NOT NULL,
    account_class VARCHAR(16) NOT NULL,
    normal_side VARCHAR(8) NOT NULL,
    allow_negative BOOLEAN NOT NULL DEFAULT FALSE,
    is_required BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_account_template_entity_type_check CHECK (entity_type IN ('household', 'firm', 'government', 'clearing')),
    CONSTRAINT city_account_template_code_check CHECK (code ~ '^[a-z][a-z0-9_]{1,47}$'),
    CONSTRAINT city_account_template_name_check CHECK (char_length(btrim(name)) BETWEEN 1 AND 96),
    CONSTRAINT city_account_template_class_check CHECK (account_class IN ('asset', 'liability', 'equity', 'revenue', 'expense')),
    CONSTRAINT city_account_template_normal_side_check CHECK (normal_side IN ('debit', 'credit')),
    CONSTRAINT city_account_template_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_account_templates_world_type_code_unique UNIQUE (world_id, entity_type, code),
    CONSTRAINT city_account_templates_id_world_type_unique UNIQUE (id, world_id, entity_type),
    CONSTRAINT city_account_templates_id_world_type_negative_unique
        UNIQUE (id, world_id, entity_type, allow_negative)
);

CREATE INDEX IF NOT EXISTS idx_city_account_templates_world_type_order
    ON city_account_templates (world_id, entity_type, sort_order, id);

CREATE TABLE IF NOT EXISTS city_accounts (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    entity_id BIGINT NOT NULL,
    entity_type VARCHAR(16) NOT NULL,
    monetary_unit_id BIGINT NOT NULL,
    template_id BIGINT NOT NULL,
    allow_negative BOOLEAN NOT NULL DEFAULT FALSE,
    current_balance_units BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_accounts_entity_fk
        FOREIGN KEY (entity_id, world_id, entity_type)
        REFERENCES city_economic_entities(id, world_id, entity_type) ON DELETE RESTRICT,
    CONSTRAINT city_accounts_monetary_unit_fk
        FOREIGN KEY (monetary_unit_id, world_id)
        REFERENCES city_monetary_units(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_accounts_template_fk
        FOREIGN KEY (template_id, world_id, entity_type, allow_negative)
        REFERENCES city_account_templates(id, world_id, entity_type, allow_negative) ON DELETE RESTRICT,
    CONSTRAINT city_account_status_check CHECK (status IN ('active', 'closed')),
    CONSTRAINT city_account_balance_check CHECK (allow_negative OR current_balance_units >= 0),
    CONSTRAINT city_account_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_accounts_entity_unit_template_unique UNIQUE (entity_id, monetary_unit_id, template_id)
);

CREATE INDEX IF NOT EXISTS idx_city_accounts_world_entity
    ON city_accounts (world_id, entity_id, status, id);
CREATE INDEX IF NOT EXISTS idx_city_accounts_world_template
    ON city_accounts (world_id, template_id, id);

-- 一个已提交的世界必须始终拥有一个活跃 owner 成员和且仅一个基础货币。
-- 约束延迟到事务提交，允许世界及其基础记录在同一事务内按顺序创建。
CREATE OR REPLACE FUNCTION assert_city_world_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    target_owner_user_id BIGINT;
    owner_memberships BIGINT;
    target_owner_memberships BIGINT;
    base_units BIGINT;
BEGIN
    SELECT owner_user_id
    INTO target_owner_user_id
    FROM city_worlds
    WHERE id = target_world_id;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT COUNT(*), COUNT(*) FILTER (WHERE user_id = target_owner_user_id)
    INTO owner_memberships, target_owner_memberships
    FROM city_members
    WHERE world_id = target_world_id
      AND role = 'owner'
      AND status = 'active';

    SELECT COUNT(*)
    INTO base_units
    FROM city_monetary_units
    WHERE world_id = target_world_id
      AND is_base;

    IF owner_memberships <> 1 OR target_owner_memberships <> 1 THEN
        RAISE EXCEPTION 'city world % must have exactly one active owner membership', target_world_id
            USING ERRCODE = '23514';
    END IF;
    IF base_units <> 1 THEN
        RAISE EXCEPTION 'city world % must have exactly one base monetary unit', target_world_id
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_world_foundation_from_world()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM assert_city_world_foundation(CASE WHEN TG_OP = 'DELETE' THEN OLD.id ELSE NEW.id END);
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_world_foundation_from_child()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    old_world_id BIGINT;
    new_world_id BIGINT;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        old_world_id := OLD.world_id;
        PERFORM assert_city_world_foundation(old_world_id);
    END IF;
    IF TG_OP <> 'DELETE' THEN
        new_world_id := NEW.world_id;
        IF old_world_id IS NULL OR new_world_id IS DISTINCT FROM old_world_id THEN
            PERFORM assert_city_world_foundation(new_world_id);
        END IF;
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_world_foundation_world_check ON city_worlds;
CREATE CONSTRAINT TRIGGER city_world_foundation_world_check
AFTER INSERT OR UPDATE OR DELETE ON city_worlds
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_world_foundation_from_world();

DROP TRIGGER IF EXISTS city_world_foundation_member_check ON city_members;
CREATE CONSTRAINT TRIGGER city_world_foundation_member_check
AFTER INSERT OR UPDATE OR DELETE ON city_members
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_world_foundation_from_child();

DROP TRIGGER IF EXISTS city_world_foundation_unit_check ON city_monetary_units;
CREATE CONSTRAINT TRIGGER city_world_foundation_unit_check
AFTER INSERT OR UPDATE OR DELETE ON city_monetary_units
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_world_foundation_from_child();
