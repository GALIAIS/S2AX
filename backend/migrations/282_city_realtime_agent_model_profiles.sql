-- A4.0: versioned Agent Model Profiles.  This is an external-I/O control
-- plane only: profile selection, provider audit and usage budgets never enter
-- a realtime world's canonical state and cannot mutate an Actor directly.

CREATE OR REPLACE FUNCTION city_realtime_agent_model_profile_config_mutation_enabled()
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_agent_model_profile_config', TRUE) = 'on'
$$;

CREATE OR REPLACE FUNCTION city_realtime_agent_model_profile_genesis_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_agent_model_profile_genesis_world_id', TRUE) = target_world_id::text
       AND EXISTS (
           SELECT 1
           FROM city_worlds world
           JOIN city_realtime_agent_world_bindings agent ON agent.world_id = world.id
           WHERE world.id = target_world_id
             AND world.simulation_version = 'city-openworld-realtime-v2'
             AND agent.policy_id = 'city-realtime-agent-core'
             AND agent.policy_version = '1.13.0'
       )
$$;

CREATE OR REPLACE FUNCTION city_realtime_agent_model_budget_worker_mutation_enabled(
    target_world_id BIGINT,
    target_request_code VARCHAR
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT current_setting('sub2api.city_realtime_agent_model_budget_worker', TRUE) = 'on'
       AND city_realtime_agent_worker_mutation_enabled(target_world_id, target_request_code)
$$;

CREATE OR REPLACE FUNCTION city_realtime_agent_model_definition_codes_valid(items JSONB)
RETURNS BOOLEAN
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT CASE
        WHEN jsonb_typeof(items) <> 'array' THEN FALSE
        WHEN jsonb_array_length(items) < 1 OR jsonb_array_length(items) > 4 THEN FALSE
        WHEN EXISTS (
            SELECT 1
            FROM jsonb_array_elements(items) AS element(value)
            WHERE jsonb_typeof(element.value) <> 'string'
               OR element.value #>> '{}' NOT IN (
                   'system.root', 'system.npc_manager', 'character.npc', 'character.user'
               )
        ) THEN FALSE
        ELSE (
            SELECT COUNT(*) = COUNT(DISTINCT element.value #>> '{}')
            FROM jsonb_array_elements(items) AS element(value)
        )
    END
$$;

CREATE TABLE IF NOT EXISTS city_realtime_agent_model_profile_versions (
    profile_code VARCHAR(64) NOT NULL,
    profile_version INTEGER NOT NULL,
    display_name VARCHAR(120) NOT NULL,
    provider_code VARCHAR(64) NOT NULL,
    provider_class VARCHAR(32) NOT NULL,
    route_ref VARCHAR(160) NOT NULL,
    platform_group_id BIGINT REFERENCES groups(id) ON DELETE RESTRICT,
    model_identifier VARCHAR(128) NOT NULL,
    allowed_agent_definition_codes JSONB NOT NULL,
    request_schema_version VARCHAR(64) NOT NULL,
    response_schema_version VARCHAR(64) NOT NULL,
    temperature NUMERIC(4, 3) NOT NULL,
    max_input_tokens INTEGER NOT NULL,
    max_output_tokens INTEGER NOT NULL,
    timeout_ms INTEGER NOT NULL,
    max_concurrency INTEGER NOT NULL,
    retry_limit SMALLINT NOT NULL,
    max_profile_hourly_requests INTEGER NOT NULL,
    max_profile_hourly_tokens BIGINT NOT NULL,
    max_world_hourly_requests INTEGER NOT NULL,
    max_world_hourly_tokens BIGINT NOT NULL,
    max_agent_hourly_requests INTEGER NOT NULL,
    max_agent_hourly_tokens BIGINT NOT NULL,
    max_owner_hourly_requests INTEGER NOT NULL,
    max_owner_hourly_tokens BIGINT NOT NULL,
    circuit_breaker_failure_threshold SMALLINT NOT NULL,
    circuit_breaker_cooldown_seconds INTEGER NOT NULL,
    privacy_class VARCHAR(24) NOT NULL,
    retention_policy VARCHAR(24) NOT NULL,
    fallback_policy VARCHAR(24) NOT NULL,
    profile_hash VARCHAR(64) NOT NULL,
    budget_hash VARCHAR(64) NOT NULL,
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (profile_code, profile_version),
    CONSTRAINT city_realtime_agent_model_profile_version_check CHECK (
        profile_code ~ '^[a-z][a-z0-9_.-]{2,63}$'
        AND profile_version > 0
        AND btrim(display_name) <> ''
        AND provider_code ~ '^[a-z][a-z0-9_.-]{2,63}$'
        AND provider_class IN ('deterministic', 'sub2api_group')
        AND route_ref ~ '^[a-z][a-z0-9_.:-]{2,159}$'
        AND route_ref !~* '(api[_-]?key|secret|token|password|https?://|://|@)'
        AND model_identifier ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'
        AND city_realtime_agent_model_definition_codes_valid(allowed_agent_definition_codes)
        AND request_schema_version = 'city-realtime-agent-observation-v1'
        AND response_schema_version = 'agent-decision-v1'
        AND temperature >= 0 AND temperature <= 2
        AND max_input_tokens BETWEEN 1 AND 262144
        AND max_output_tokens BETWEEN 1 AND 65536
        AND timeout_ms BETWEEN 100 AND 300000
        AND max_concurrency BETWEEN 1 AND 4096
        AND retry_limit BETWEEN 0 AND 8
        AND max_profile_hourly_requests BETWEEN 1 AND 1000000
        AND max_profile_hourly_tokens BETWEEN 1 AND 10000000000
        AND max_world_hourly_requests BETWEEN 1 AND 1000000
        AND max_world_hourly_tokens BETWEEN 1 AND 10000000000
        AND max_agent_hourly_requests BETWEEN 1 AND 100000
        AND max_agent_hourly_tokens BETWEEN 1 AND 1000000000
        AND max_owner_hourly_requests BETWEEN 1 AND 100000
        AND max_owner_hourly_tokens BETWEEN 1 AND 1000000000
        AND circuit_breaker_failure_threshold BETWEEN 1 AND 100
        AND circuit_breaker_cooldown_seconds BETWEEN 1 AND 86400
        AND privacy_class IN ('hash_only', 'redacted')
        AND retention_policy IN ('hash_only', 'audit_minimum')
        AND fallback_policy IN ('no_op', 'defer')
        AND profile_hash ~ '^[0-9a-f]{64}$'
        AND budget_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
        AND metadata = '{}'::jsonb
        AND (
            (provider_code = 'fake.deterministic'
             AND provider_class = 'deterministic'
             AND route_ref = 'system.fake.deterministic'
             AND platform_group_id IS NULL
             AND model_identifier = 'deterministic-v1')
            OR
            (provider_code = 'sub2api.gateway'
             AND provider_class = 'sub2api_group'
             AND platform_group_id IS NOT NULL
             AND route_ref = 'group:' || platform_group_id::text)
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_model_profile_versions_provider
    ON city_realtime_agent_model_profile_versions (provider_code, profile_code, profile_version DESC);

CREATE TABLE IF NOT EXISTS city_realtime_agent_model_profile_heads (
    profile_code VARCHAR(64) PRIMARY KEY,
    active_version INTEGER NOT NULL,
    status VARCHAR(16) NOT NULL,
    updated_by_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_agent_model_profile_head_version_fk
        FOREIGN KEY (profile_code, active_version)
        REFERENCES city_realtime_agent_model_profile_versions(profile_code, profile_version) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_model_profile_head_check CHECK (
        profile_code ~ '^[a-z][a-z0-9_.-]{2,63}$'
        AND active_version > 0
        AND status IN ('active', 'disabled', 'retired')
        AND jsonb_typeof(metadata) = 'object'
        AND metadata = '{}'::jsonb
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_model_profile_heads_status
    ON city_realtime_agent_model_profile_heads (status, profile_code);

CREATE TABLE IF NOT EXISTS city_realtime_agent_model_profile_audit_events (
    event_id BIGSERIAL PRIMARY KEY,
    profile_code VARCHAR(64) NOT NULL,
    profile_version INTEGER,
    world_id BIGINT REFERENCES city_worlds(id) ON DELETE RESTRICT,
    agent_definition_code VARCHAR(96),
    event_type VARCHAR(32) NOT NULL,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    payload_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_agent_model_profile_audit_profile_fk
        FOREIGN KEY (profile_code, profile_version)
        REFERENCES city_realtime_agent_model_profile_versions(profile_code, profile_version) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_model_profile_audit_check CHECK (
        profile_code ~ '^[a-z][a-z0-9_.-]{2,63}$'
        AND (profile_version IS NULL OR profile_version > 0)
        AND (agent_definition_code IS NULL OR agent_definition_code ~ '^[a-z][a-z0-9_.-]{1,95}$')
        AND event_type IN (
            'profile_created', 'profile_head_updated',
            'world_binding_created', 'world_binding_superseded', 'world_binding_disabled'
        )
        AND payload_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
        AND metadata = '{}'::jsonb
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_model_profile_audit_lookup
    ON city_realtime_agent_model_profile_audit_events (profile_code, created_at DESC, event_id DESC);

CREATE TABLE IF NOT EXISTS city_realtime_agent_model_profile_world_bindings (
    world_id BIGINT NOT NULL REFERENCES city_realtime_agent_world_bindings(world_id) ON DELETE RESTRICT,
    agent_definition_code VARCHAR(96) NOT NULL,
    binding_version INTEGER NOT NULL,
    profile_code VARCHAR(64) NOT NULL,
    profile_version INTEGER NOT NULL,
    profile_hash VARCHAR(64) NOT NULL,
    budget_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    binding_status VARCHAR(16) NOT NULL,
    owner_selectable BOOLEAN NOT NULL DEFAULT FALSE,
    binding_source VARCHAR(24) NOT NULL,
    configured_by_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, agent_definition_code, binding_version),
    CONSTRAINT city_realtime_agent_model_profile_world_binding_profile_fk
        FOREIGN KEY (profile_code, profile_version)
        REFERENCES city_realtime_agent_model_profile_versions(profile_code, profile_version) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_model_profile_world_binding_check CHECK (
        agent_definition_code IN ('system.root', 'system.npc_manager', 'character.npc', 'character.user')
        AND binding_version > 0
        AND profile_hash ~ '^[0-9a-f]{64}$'
        AND budget_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash ~ '^[0-9a-f]{64}$'
        AND binding_status IN ('active', 'superseded', 'disabled')
        AND binding_source IN ('system_genesis', 'administrator')
        AND jsonb_typeof(metadata) = 'object'
        AND metadata = '{}'::jsonb
        AND ((binding_source = 'system_genesis' AND configured_by_user_id IS NULL)
             OR (binding_source = 'administrator' AND configured_by_user_id IS NOT NULL))
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_city_realtime_agent_model_profile_world_binding_active
    ON city_realtime_agent_model_profile_world_bindings (world_id, agent_definition_code)
    WHERE binding_status = 'active';

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_model_profile_world_binding_profile
    ON city_realtime_agent_model_profile_world_bindings (profile_code, profile_version, world_id)
    WHERE binding_status = 'active';

CREATE TABLE IF NOT EXISTS city_realtime_agent_model_usage_windows (
    profile_code VARCHAR(64) NOT NULL,
    profile_version INTEGER NOT NULL,
    profile_hash VARCHAR(64) NOT NULL,
    budget_hash VARCHAR(64) NOT NULL,
    window_started_at TIMESTAMPTZ NOT NULL,
    scope_kind VARCHAR(16) NOT NULL,
    scope_key VARCHAR(160) NOT NULL,
    source_world_id BIGINT NOT NULL,
    source_request_code VARCHAR(96) NOT NULL,
    reserved_request_count BIGINT NOT NULL DEFAULT 0,
    reserved_input_tokens BIGINT NOT NULL DEFAULT 0,
    reserved_output_tokens BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (profile_code, profile_version, window_started_at, scope_kind, scope_key),
    CONSTRAINT city_realtime_agent_model_usage_window_profile_fk
        FOREIGN KEY (profile_code, profile_version)
        REFERENCES city_realtime_agent_model_profile_versions(profile_code, profile_version) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_model_usage_window_request_fk
        FOREIGN KEY (source_world_id, source_request_code)
        REFERENCES city_realtime_agent_decision_requests(world_id, request_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_model_usage_window_check CHECK (
        profile_hash ~ '^[0-9a-f]{64}$'
        AND budget_hash ~ '^[0-9a-f]{64}$'
        AND date_trunc('hour', window_started_at AT TIME ZONE 'UTC') = window_started_at AT TIME ZONE 'UTC'
        AND scope_kind IN ('profile', 'world', 'agent', 'owner')
        AND scope_key ~ '^[a-zA-Z0-9][a-zA-Z0-9._:/@-]{0,159}$'
        AND source_world_id > 0
        AND source_request_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND reserved_request_count >= 0
        AND reserved_input_tokens >= 0
        AND reserved_output_tokens >= 0
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_model_usage_windows_source
    ON city_realtime_agent_model_usage_windows (source_world_id, source_request_code, window_started_at DESC);

-- Request/attempt snapshots are nullable only for historical worlds that
-- predate A4.0.  New worlds receive an explicit deterministic binding.
ALTER TABLE city_realtime_agent_decision_requests
    ADD COLUMN IF NOT EXISTS model_profile_code VARCHAR(64),
    ADD COLUMN IF NOT EXISTS model_profile_version INTEGER,
    ADD COLUMN IF NOT EXISTS model_profile_hash VARCHAR(64),
    ADD COLUMN IF NOT EXISTS model_budget_hash VARCHAR(64);

ALTER TABLE city_realtime_agent_decision_attempts
    ADD COLUMN IF NOT EXISTS model_profile_code VARCHAR(64),
    ADD COLUMN IF NOT EXISTS model_profile_version INTEGER,
    ADD COLUMN IF NOT EXISTS model_profile_hash VARCHAR(64),
    ADD COLUMN IF NOT EXISTS model_budget_hash VARCHAR(64),
    ADD COLUMN IF NOT EXISTS reserved_input_tokens INTEGER,
    ADD COLUMN IF NOT EXISTS reserved_output_tokens INTEGER;

ALTER TABLE city_realtime_agent_decision_requests
    DROP CONSTRAINT IF EXISTS city_realtime_agent_decision_request_model_profile_fk;
ALTER TABLE city_realtime_agent_decision_requests
    ADD CONSTRAINT city_realtime_agent_decision_request_model_profile_fk
    FOREIGN KEY (model_profile_code, model_profile_version)
    REFERENCES city_realtime_agent_model_profile_versions(profile_code, profile_version) ON DELETE RESTRICT;

ALTER TABLE city_realtime_agent_decision_attempts
    DROP CONSTRAINT IF EXISTS city_realtime_agent_decision_attempt_model_profile_fk;
ALTER TABLE city_realtime_agent_decision_attempts
    ADD CONSTRAINT city_realtime_agent_decision_attempt_model_profile_fk
    FOREIGN KEY (model_profile_code, model_profile_version)
    REFERENCES city_realtime_agent_model_profile_versions(profile_code, profile_version) ON DELETE RESTRICT;

CREATE TABLE IF NOT EXISTS city_realtime_agent_model_attempt_budget_reservations (
    world_id BIGINT NOT NULL,
    attempt_code VARCHAR(96) NOT NULL,
    request_code VARCHAR(96) NOT NULL,
    agent_code VARCHAR(96) NOT NULL,
    owner_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    profile_code VARCHAR(64) NOT NULL,
    profile_version INTEGER NOT NULL,
    profile_hash VARCHAR(64) NOT NULL,
    budget_hash VARCHAR(64) NOT NULL,
    window_started_at TIMESTAMPTZ NOT NULL,
    reserved_input_tokens INTEGER NOT NULL,
    reserved_output_tokens INTEGER NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, attempt_code),
    CONSTRAINT city_realtime_agent_model_attempt_budget_attempt_fk
        FOREIGN KEY (world_id, attempt_code)
        REFERENCES city_realtime_agent_decision_attempts(world_id, attempt_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_model_attempt_budget_request_fk
        FOREIGN KEY (world_id, request_code)
        REFERENCES city_realtime_agent_decision_requests(world_id, request_code) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_model_attempt_budget_profile_fk
        FOREIGN KEY (profile_code, profile_version)
        REFERENCES city_realtime_agent_model_profile_versions(profile_code, profile_version) ON DELETE RESTRICT,
    CONSTRAINT city_realtime_agent_model_attempt_budget_check CHECK (
        request_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND agent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND profile_hash ~ '^[0-9a-f]{64}$'
        AND budget_hash ~ '^[0-9a-f]{64}$'
        AND date_trunc('hour', window_started_at AT TIME ZONE 'UTC') = window_started_at AT TIME ZONE 'UTC'
        AND reserved_input_tokens > 0
        AND reserved_output_tokens > 0
        AND jsonb_typeof(metadata) = 'object'
        AND metadata = '{}'::jsonb
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_agent_model_attempt_budget_request
    ON city_realtime_agent_model_attempt_budget_reservations (world_id, request_code, created_at DESC);

-- The seed is intentionally deterministic and non-networked.  Profile and
-- budget hashes are derived from the same immutable fields used by the later
-- INSERT trigger.
WITH profile_row AS (
    SELECT
        'system.fake.deterministic'::VARCHAR(64) AS profile_code,
        1 AS profile_version,
        'Deterministic test adapter'::VARCHAR(120) AS display_name,
        'fake.deterministic'::VARCHAR(64) AS provider_code,
        'deterministic'::VARCHAR(32) AS provider_class,
        'system.fake.deterministic'::VARCHAR(160) AS route_ref,
        NULL::BIGINT AS platform_group_id,
        'deterministic-v1'::VARCHAR(128) AS model_identifier,
        '["system.root","system.npc_manager","character.npc","character.user"]'::jsonb AS allowed_agent_definition_codes,
        'city-realtime-agent-observation-v1'::VARCHAR(64) AS request_schema_version,
        'agent-decision-v1'::VARCHAR(64) AS response_schema_version,
        0.000::NUMERIC(4,3) AS temperature,
        4096 AS max_input_tokens,
        256 AS max_output_tokens,
        5000 AS timeout_ms,
        1 AS max_concurrency,
        2::SMALLINT AS retry_limit,
        120 AS max_profile_hourly_requests,
        522240::BIGINT AS max_profile_hourly_tokens,
        60 AS max_world_hourly_requests,
        261120::BIGINT AS max_world_hourly_tokens,
        24 AS max_agent_hourly_requests,
        104448::BIGINT AS max_agent_hourly_tokens,
        24 AS max_owner_hourly_requests,
        104448::BIGINT AS max_owner_hourly_tokens,
        3::SMALLINT AS circuit_breaker_failure_threshold,
        60 AS circuit_breaker_cooldown_seconds,
        'hash_only'::VARCHAR(24) AS privacy_class,
        'hash_only'::VARCHAR(24) AS retention_policy,
        'no_op'::VARCHAR(24) AS fallback_policy
), hashes AS (
    SELECT profile_row.*,
        encode(sha256(convert_to(jsonb_build_object(
            'schema_version', 1,
            'profile_code', profile_code,
            'profile_version', profile_version,
            'display_name', display_name,
            'provider_code', provider_code,
            'provider_class', provider_class,
            'route_ref', route_ref,
            'platform_group_id', platform_group_id,
            'model_identifier', model_identifier,
            'allowed_agent_definition_codes', allowed_agent_definition_codes,
            'request_schema_version', request_schema_version,
            'response_schema_version', response_schema_version,
            'temperature', temperature,
            'max_input_tokens', max_input_tokens,
            'max_output_tokens', max_output_tokens,
            'timeout_ms', timeout_ms,
            'max_concurrency', max_concurrency,
            'retry_limit', retry_limit,
            'max_profile_hourly_requests', max_profile_hourly_requests,
            'max_profile_hourly_tokens', max_profile_hourly_tokens,
            'max_world_hourly_requests', max_world_hourly_requests,
            'max_world_hourly_tokens', max_world_hourly_tokens,
            'max_agent_hourly_requests', max_agent_hourly_requests,
            'max_agent_hourly_tokens', max_agent_hourly_tokens,
            'max_owner_hourly_requests', max_owner_hourly_requests,
            'max_owner_hourly_tokens', max_owner_hourly_tokens,
            'circuit_breaker_failure_threshold', circuit_breaker_failure_threshold,
            'circuit_breaker_cooldown_seconds', circuit_breaker_cooldown_seconds,
            'privacy_class', privacy_class,
            'retention_policy', retention_policy,
            'fallback_policy', fallback_policy
        )::text, 'UTF8')), 'hex') AS profile_hash,
        encode(sha256(convert_to(jsonb_build_object(
            'schema_version', 1,
            'max_concurrency', max_concurrency,
            'retry_limit', retry_limit,
            'max_input_tokens', max_input_tokens,
            'max_output_tokens', max_output_tokens,
            'timeout_ms', timeout_ms,
            'max_profile_hourly_requests', max_profile_hourly_requests,
            'max_profile_hourly_tokens', max_profile_hourly_tokens,
            'max_world_hourly_requests', max_world_hourly_requests,
            'max_world_hourly_tokens', max_world_hourly_tokens,
            'max_agent_hourly_requests', max_agent_hourly_requests,
            'max_agent_hourly_tokens', max_agent_hourly_tokens,
            'max_owner_hourly_requests', max_owner_hourly_requests,
            'max_owner_hourly_tokens', max_owner_hourly_tokens,
            'circuit_breaker_failure_threshold', circuit_breaker_failure_threshold,
            'circuit_breaker_cooldown_seconds', circuit_breaker_cooldown_seconds,
            'fallback_policy', fallback_policy
        )::text, 'UTF8')), 'hex') AS budget_hash
    FROM profile_row
)
INSERT INTO city_realtime_agent_model_profile_versions (
    profile_code, profile_version, display_name, provider_code, provider_class,
    route_ref, platform_group_id, model_identifier, allowed_agent_definition_codes,
    request_schema_version, response_schema_version, temperature,
    max_input_tokens, max_output_tokens, timeout_ms, max_concurrency, retry_limit,
    max_profile_hourly_requests, max_profile_hourly_tokens,
    max_world_hourly_requests, max_world_hourly_tokens,
    max_agent_hourly_requests, max_agent_hourly_tokens,
    max_owner_hourly_requests, max_owner_hourly_tokens,
    circuit_breaker_failure_threshold, circuit_breaker_cooldown_seconds,
    privacy_class, retention_policy, fallback_policy, profile_hash, budget_hash, metadata
)
SELECT profile_code, profile_version, display_name, provider_code, provider_class,
       route_ref, platform_group_id, model_identifier, allowed_agent_definition_codes,
       request_schema_version, response_schema_version, temperature,
       max_input_tokens, max_output_tokens, timeout_ms, max_concurrency, retry_limit,
       max_profile_hourly_requests, max_profile_hourly_tokens,
       max_world_hourly_requests, max_world_hourly_tokens,
       max_agent_hourly_requests, max_agent_hourly_tokens,
       max_owner_hourly_requests, max_owner_hourly_tokens,
       circuit_breaker_failure_threshold, circuit_breaker_cooldown_seconds,
       privacy_class, retention_policy, fallback_policy, profile_hash, budget_hash, '{}'::jsonb
FROM hashes
ON CONFLICT (profile_code, profile_version) DO NOTHING;

INSERT INTO city_realtime_agent_model_profile_heads
    (profile_code, active_version, status, metadata)
VALUES ('system.fake.deterministic', 1, 'active', '{}'::jsonb)
ON CONFLICT (profile_code) DO NOTHING;

CREATE OR REPLACE FUNCTION city_realtime_agent_model_profile_hash(
    item city_realtime_agent_model_profile_versions
)
RETURNS VARCHAR
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT encode(sha256(convert_to(jsonb_build_object(
        'schema_version', 1,
        'profile_code', item.profile_code,
        'profile_version', item.profile_version,
        'display_name', item.display_name,
        'provider_code', item.provider_code,
        'provider_class', item.provider_class,
        'route_ref', item.route_ref,
        'platform_group_id', item.platform_group_id,
        'model_identifier', item.model_identifier,
        'allowed_agent_definition_codes', item.allowed_agent_definition_codes,
        'request_schema_version', item.request_schema_version,
        'response_schema_version', item.response_schema_version,
        'temperature', item.temperature,
        'max_input_tokens', item.max_input_tokens,
        'max_output_tokens', item.max_output_tokens,
        'timeout_ms', item.timeout_ms,
        'max_concurrency', item.max_concurrency,
        'retry_limit', item.retry_limit,
        'max_profile_hourly_requests', item.max_profile_hourly_requests,
        'max_profile_hourly_tokens', item.max_profile_hourly_tokens,
        'max_world_hourly_requests', item.max_world_hourly_requests,
        'max_world_hourly_tokens', item.max_world_hourly_tokens,
        'max_agent_hourly_requests', item.max_agent_hourly_requests,
        'max_agent_hourly_tokens', item.max_agent_hourly_tokens,
        'max_owner_hourly_requests', item.max_owner_hourly_requests,
        'max_owner_hourly_tokens', item.max_owner_hourly_tokens,
        'circuit_breaker_failure_threshold', item.circuit_breaker_failure_threshold,
        'circuit_breaker_cooldown_seconds', item.circuit_breaker_cooldown_seconds,
        'privacy_class', item.privacy_class,
        'retention_policy', item.retention_policy,
        'fallback_policy', item.fallback_policy
    )::text, 'UTF8')), 'hex')
$$;

CREATE OR REPLACE FUNCTION city_realtime_agent_model_budget_hash(
    item city_realtime_agent_model_profile_versions
)
RETURNS VARCHAR
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT encode(sha256(convert_to(jsonb_build_object(
        'schema_version', 1,
        'max_concurrency', item.max_concurrency,
        'retry_limit', item.retry_limit,
        'max_input_tokens', item.max_input_tokens,
        'max_output_tokens', item.max_output_tokens,
        'timeout_ms', item.timeout_ms,
        'max_profile_hourly_requests', item.max_profile_hourly_requests,
        'max_profile_hourly_tokens', item.max_profile_hourly_tokens,
        'max_world_hourly_requests', item.max_world_hourly_requests,
        'max_world_hourly_tokens', item.max_world_hourly_tokens,
        'max_agent_hourly_requests', item.max_agent_hourly_requests,
        'max_agent_hourly_tokens', item.max_agent_hourly_tokens,
        'max_owner_hourly_requests', item.max_owner_hourly_requests,
        'max_owner_hourly_tokens', item.max_owner_hourly_tokens,
        'circuit_breaker_failure_threshold', item.circuit_breaker_failure_threshold,
        'circuit_breaker_cooldown_seconds', item.circuit_breaker_cooldown_seconds,
        'fallback_policy', item.fallback_policy
    )::text, 'UTF8')), 'hex')
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_model_profile_version()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_agent_model_profile_config_mutation_enabled() THEN
        RAISE EXCEPTION 'city realtime agent model profile versions are immutable outside administrator configuration'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.provider_class = 'sub2api_group' AND NOT EXISTS (
        SELECT 1 FROM groups
        WHERE id = NEW.platform_group_id AND status = 'active' AND deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'city realtime agent model profile route group is not active'
            USING ERRCODE = '23514';
    END IF;
    NEW.profile_hash := city_realtime_agent_model_profile_hash(NEW);
    NEW.budget_hash := city_realtime_agent_model_budget_hash(NEW);
    NEW.metadata := '{}'::jsonb;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_model_profile_head()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT city_realtime_agent_model_profile_config_mutation_enabled() THEN
        RAISE EXCEPTION 'city realtime agent model profile heads require administrator configuration'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.metadata <> '{}'::jsonb THEN
            RAISE EXCEPTION 'city realtime agent model profile head metadata is fixed' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND NEW.profile_code = OLD.profile_code
       AND NEW.created_at = OLD.created_at
       AND NEW.metadata = OLD.metadata THEN
        NEW.updated_at := NOW();
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent model profile heads are append-controlled records'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_model_profile_audit_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT'
       AND (city_realtime_agent_model_profile_config_mutation_enabled()
            OR (NEW.world_id IS NOT NULL AND city_realtime_agent_model_profile_genesis_enabled(NEW.world_id))) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent model profile audit events are append-only'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_model_profile_world_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_profile_hash VARCHAR(64);
    expected_budget_hash VARCHAR(64);
    expected_binding_hash VARCHAR(64);
    head_version INTEGER;
    head_status VARCHAR(16);
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NOT city_realtime_agent_model_profile_config_mutation_enabled()
           AND NOT city_realtime_agent_model_profile_genesis_enabled(NEW.world_id) THEN
            RAISE EXCEPTION 'city realtime agent model profile bindings require administrator configuration or genesis'
                USING ERRCODE = '55000';
        END IF;
        SELECT profile_hash, budget_hash INTO expected_profile_hash, expected_budget_hash
        FROM city_realtime_agent_model_profile_versions
        WHERE profile_code = NEW.profile_code AND profile_version = NEW.profile_version;
        SELECT active_version, status INTO head_version, head_status
        FROM city_realtime_agent_model_profile_heads
        WHERE profile_code = NEW.profile_code;
        expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
            'city-realtime-agent-model-profile-world-binding-v1', NEW.world_id::text,
            NEW.agent_definition_code, NEW.binding_version::text,
            NEW.profile_code, NEW.profile_version::text, expected_profile_hash, expected_budget_hash,
            NEW.owner_selectable::text, NEW.binding_source
        ), 'UTF8')), 'hex');
        NEW.profile_hash := expected_profile_hash;
        NEW.budget_hash := expected_budget_hash;
        NEW.binding_hash := expected_binding_hash;
        IF expected_profile_hash IS NULL OR expected_budget_hash IS NULL
           OR head_version IS DISTINCT FROM NEW.profile_version OR head_status <> 'active'
           OR NEW.binding_status <> 'active'
           OR NEW.metadata <> '{}'::jsonb
           OR (NEW.binding_source = 'system_genesis' AND NOT city_realtime_agent_model_profile_genesis_enabled(NEW.world_id))
           OR (NEW.binding_source = 'administrator' AND NOT city_realtime_agent_model_profile_config_mutation_enabled()) THEN
            RAISE EXCEPTION 'city realtime agent model profile world binding is invalid'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_agent_model_profile_config_mutation_enabled()
       AND NEW.world_id = OLD.world_id
       AND NEW.agent_definition_code = OLD.agent_definition_code
       AND NEW.binding_version = OLD.binding_version
       AND NEW.profile_code = OLD.profile_code AND NEW.profile_version = OLD.profile_version
       AND NEW.profile_hash = OLD.profile_hash AND NEW.budget_hash = OLD.budget_hash
       AND NEW.binding_hash = OLD.binding_hash
       AND NEW.owner_selectable = OLD.owner_selectable AND NEW.binding_source = OLD.binding_source
       AND NEW.configured_by_user_id IS NOT DISTINCT FROM OLD.configured_by_user_id
       AND NEW.created_at = OLD.created_at AND NEW.metadata = OLD.metadata
       AND OLD.binding_status = 'active' AND NEW.binding_status IN ('superseded', 'disabled') THEN
        NEW.updated_at := NOW();
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent model profile world bindings are revisioned append-only records'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_model_usage_window()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_profile_hash VARCHAR(64);
    expected_budget_hash VARCHAR(64);
BEGIN
    IF NOT city_realtime_agent_model_budget_worker_mutation_enabled(NEW.source_world_id, NEW.source_request_code) THEN
        RAISE EXCEPTION 'city realtime agent model usage windows require the decision worker gate'
            USING ERRCODE = '55000';
    END IF;
    SELECT profile_hash, budget_hash INTO expected_profile_hash, expected_budget_hash
    FROM city_realtime_agent_model_profile_versions
    WHERE profile_code = NEW.profile_code AND profile_version = NEW.profile_version;
    IF expected_profile_hash IS NULL OR NEW.profile_hash <> expected_profile_hash
       OR NEW.budget_hash <> expected_budget_hash THEN
        RAISE EXCEPTION 'city realtime agent model usage window profile snapshot is invalid'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'INSERT' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND NEW.profile_code = OLD.profile_code AND NEW.profile_version = OLD.profile_version
       AND NEW.profile_hash = OLD.profile_hash AND NEW.budget_hash = OLD.budget_hash
       AND NEW.window_started_at = OLD.window_started_at
       AND NEW.scope_kind = OLD.scope_kind AND NEW.scope_key = OLD.scope_key
       AND NEW.created_at = OLD.created_at
       AND NEW.reserved_request_count = OLD.reserved_request_count + 1
       AND NEW.reserved_input_tokens > OLD.reserved_input_tokens
       AND NEW.reserved_output_tokens > OLD.reserved_output_tokens THEN
        NEW.updated_at := NOW();
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent model usage windows are monotonic worker counters'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_model_attempt_budget_reservation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    request_agent_code VARCHAR(96);
    request_profile_code VARCHAR(64);
    request_profile_version INTEGER;
    request_profile_hash VARCHAR(64);
    request_budget_hash VARCHAR(64);
    request_owner_user_id BIGINT;
    attempt_profile_code VARCHAR(64);
    attempt_profile_version INTEGER;
    attempt_profile_hash VARCHAR(64);
    attempt_budget_hash VARCHAR(64);
    attempt_input_tokens INTEGER;
    attempt_output_tokens INTEGER;
BEGIN
    IF TG_OP <> 'INSERT'
       OR NOT city_realtime_agent_model_budget_worker_mutation_enabled(NEW.world_id, NEW.request_code) THEN
        RAISE EXCEPTION 'city realtime agent model attempt budgets are append-only worker facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT request.agent_code, request.model_profile_code, request.model_profile_version,
           request.model_profile_hash, request.model_budget_hash, instance.owner_user_id,
           attempt.model_profile_code, attempt.model_profile_version,
           attempt.model_profile_hash, attempt.model_budget_hash,
           attempt.reserved_input_tokens, attempt.reserved_output_tokens
    INTO request_agent_code, request_profile_code, request_profile_version,
         request_profile_hash, request_budget_hash, request_owner_user_id,
         attempt_profile_code, attempt_profile_version, attempt_profile_hash, attempt_budget_hash,
         attempt_input_tokens, attempt_output_tokens
    FROM city_realtime_agent_decision_requests request
    JOIN city_realtime_agent_instances instance
      ON instance.world_id = request.world_id AND instance.agent_code = request.agent_code
    JOIN city_realtime_agent_decision_attempts attempt
      ON attempt.world_id = request.world_id AND attempt.attempt_code = NEW.attempt_code
    WHERE request.world_id = NEW.world_id AND request.request_code = NEW.request_code;
    IF request_agent_code IS NULL
       OR NEW.agent_code <> request_agent_code
       OR NEW.owner_user_id IS DISTINCT FROM request_owner_user_id
       OR NEW.profile_code IS DISTINCT FROM request_profile_code
       OR NEW.profile_version IS DISTINCT FROM request_profile_version
       OR NEW.profile_hash IS DISTINCT FROM request_profile_hash
       OR NEW.budget_hash IS DISTINCT FROM request_budget_hash
       OR attempt_profile_code IS DISTINCT FROM request_profile_code
       OR attempt_profile_version IS DISTINCT FROM request_profile_version
       OR attempt_profile_hash IS DISTINCT FROM request_profile_hash
       OR attempt_budget_hash IS DISTINCT FROM request_budget_hash
       OR NEW.reserved_input_tokens IS DISTINCT FROM attempt_input_tokens
       OR NEW.reserved_output_tokens IS DISTINCT FROM attempt_output_tokens
       OR NEW.metadata <> '{}'::jsonb THEN
        RAISE EXCEPTION 'city realtime agent model attempt budget reservation is inconsistent'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

-- Profile-version rows now have their immutable derived hashes generated by
-- PostgreSQL.  Existing seed data was inserted before this trigger exists.
DROP TRIGGER IF EXISTS city_realtime_agent_model_profile_version_guard ON city_realtime_agent_model_profile_versions;
CREATE TRIGGER city_realtime_agent_model_profile_version_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_model_profile_versions
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_model_profile_version();

DROP TRIGGER IF EXISTS city_realtime_agent_model_profile_head_guard ON city_realtime_agent_model_profile_heads;
CREATE TRIGGER city_realtime_agent_model_profile_head_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_model_profile_heads
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_model_profile_head();

DROP TRIGGER IF EXISTS city_realtime_agent_model_profile_audit_event_guard ON city_realtime_agent_model_profile_audit_events;
CREATE TRIGGER city_realtime_agent_model_profile_audit_event_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_model_profile_audit_events
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_model_profile_audit_event();

DROP TRIGGER IF EXISTS city_realtime_agent_model_profile_world_binding_guard ON city_realtime_agent_model_profile_world_bindings;
CREATE TRIGGER city_realtime_agent_model_profile_world_binding_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_model_profile_world_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_model_profile_world_binding();

DROP TRIGGER IF EXISTS city_realtime_agent_model_usage_window_guard ON city_realtime_agent_model_usage_windows;
CREATE TRIGGER city_realtime_agent_model_usage_window_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_model_usage_windows
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_model_usage_window();

DROP TRIGGER IF EXISTS city_realtime_agent_model_attempt_budget_reservation_guard ON city_realtime_agent_model_attempt_budget_reservations;
CREATE TRIGGER city_realtime_agent_model_attempt_budget_reservation_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_realtime_agent_model_attempt_budget_reservations
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_agent_model_attempt_budget_reservation();

-- Rebuild A2 checks additively so pre-A4 rows remain valid only as explicit
-- all-NULL legacy snapshots, while new rows carry a complete immutable tuple.
ALTER TABLE city_realtime_agent_decision_requests
    DROP CONSTRAINT IF EXISTS city_realtime_agent_decision_request_identity_check;
ALTER TABLE city_realtime_agent_decision_requests
    ADD CONSTRAINT city_realtime_agent_decision_request_identity_check CHECK (
        request_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND agent_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND observation_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND observation_hash ~ '^[0-9a-f]{64}$'
        AND precondition_hash ~ '^[0-9a-f]{64}$'
        AND observed_frame_sequence > 0
        AND expires_at_world_time_us >= 0
        AND status IN ('queued', 'leased', 'accepted', 'rejected', 'stale', 'failed_terminal', 'cancelled')
        AND attempt_count >= 0 AND attempt_count <= 32767
        AND requested_frame_sequence > 0
        AND jsonb_typeof(metadata) = 'object'
        AND (
            (model_profile_code IS NULL AND model_profile_version IS NULL
             AND model_profile_hash IS NULL AND model_budget_hash IS NULL)
            OR
            (model_profile_code ~ '^[a-z][a-z0-9_.-]{2,63}$'
             AND model_profile_version > 0
             AND model_profile_hash ~ '^[0-9a-f]{64}$'
             AND model_budget_hash ~ '^[0-9a-f]{64}$')
        )
        AND (
            (status = 'queued' AND lease_owner IS NULL AND lease_expires_at IS NULL AND terminal_frame_sequence IS NULL)
            OR (status = 'leased' AND lease_owner ~ '^[a-z][a-z0-9_.-]{1,63}$' AND lease_expires_at IS NOT NULL AND terminal_frame_sequence IS NULL)
            OR (status IN ('accepted', 'rejected', 'stale', 'failed_terminal', 'cancelled')
                AND lease_owner IS NULL AND lease_expires_at IS NULL
                AND terminal_frame_sequence IS NOT NULL AND terminal_frame_sequence > requested_frame_sequence)
        )
    );

ALTER TABLE city_realtime_agent_decision_attempts
    DROP CONSTRAINT IF EXISTS city_realtime_agent_decision_attempt_identity_check;
ALTER TABLE city_realtime_agent_decision_attempts
    ADD CONSTRAINT city_realtime_agent_decision_attempt_identity_check CHECK (
        attempt_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND request_code ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND attempt_number > 0
        AND provider_code ~ '^[a-z][a-z0-9_.-]{2,63}$'
        AND status IN ('started', 'succeeded', 'failed')
        AND request_hash ~ '^[0-9a-f]{64}$'
        AND (response_hash IS NULL OR response_hash ~ '^[0-9a-f]{64}$')
        AND (error_code IS NULL OR error_code ~ '^[a-z][a-z0-9_.-]{1,63}$')
        AND jsonb_typeof(metadata) = 'object'
        AND (
            (model_profile_code IS NULL AND model_profile_version IS NULL
             AND model_profile_hash IS NULL AND model_budget_hash IS NULL
             AND reserved_input_tokens IS NULL AND reserved_output_tokens IS NULL
             AND provider_code = 'fake.deterministic')
            OR
            (model_profile_code ~ '^[a-z][a-z0-9_.-]{2,63}$'
             AND model_profile_version > 0
             AND model_profile_hash ~ '^[0-9a-f]{64}$'
             AND model_budget_hash ~ '^[0-9a-f]{64}$'
             AND reserved_input_tokens > 0 AND reserved_output_tokens > 0)
        )
        AND (
            (status = 'started' AND response_hash IS NULL AND error_code IS NULL AND completed_at IS NULL)
            OR (status = 'succeeded' AND response_hash ~ '^[0-9a-f]{64}$' AND error_code IS NULL AND completed_at IS NOT NULL)
            OR (status = 'failed' AND response_hash IS NULL AND error_code IS NOT NULL AND completed_at IS NOT NULL)
        )
    );

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_decision_request()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_profile_hash VARCHAR(64);
    expected_budget_hash VARCHAR(64);
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_decision_policy_enabled(NEW.world_id)
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.requested_frame_sequence)
       AND NEW.status = 'queued' AND NEW.attempt_count = 0
       AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL
       AND NEW.terminal_frame_sequence IS NULL THEN
        IF NEW.model_profile_code IS NOT NULL THEN
            SELECT profile.profile_hash, profile.budget_hash
            INTO expected_profile_hash, expected_budget_hash
            FROM city_realtime_agent_model_profile_world_bindings binding
            JOIN city_realtime_agent_instances instance
              ON instance.world_id = binding.world_id AND instance.agent_code = NEW.agent_code
            JOIN city_realtime_agent_model_profile_versions profile
              ON profile.profile_code = binding.profile_code AND profile.profile_version = binding.profile_version
            JOIN city_realtime_agent_model_profile_heads head
              ON head.profile_code = profile.profile_code
            WHERE binding.world_id = NEW.world_id
              AND binding.agent_definition_code = instance.definition_code
              AND binding.binding_status = 'active'
              AND head.status = 'active'
              AND binding.profile_code = NEW.model_profile_code
              AND binding.profile_version = NEW.model_profile_version;
            IF expected_profile_hash IS NULL
               OR NEW.model_profile_hash <> expected_profile_hash
               OR NEW.model_budget_hash <> expected_budget_hash THEN
                RAISE EXCEPTION 'city realtime agent decision request model profile snapshot is invalid'
                    USING ERRCODE = '23514';
            END IF;
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_agent_worker_mutation_enabled(NEW.world_id, NEW.request_code)
       AND NEW.world_id = OLD.world_id AND NEW.request_code = OLD.request_code
       AND NEW.agent_code = OLD.agent_code AND NEW.observation_code = OLD.observation_code
       AND NEW.observation_hash = OLD.observation_hash AND NEW.precondition_hash = OLD.precondition_hash
       AND NEW.observed_frame_sequence = OLD.observed_frame_sequence
       AND NEW.expires_at_world_time_us = OLD.expires_at_world_time_us
       AND NEW.requested_frame_sequence = OLD.requested_frame_sequence
       AND NEW.model_profile_code IS NOT DISTINCT FROM OLD.model_profile_code
       AND NEW.model_profile_version IS NOT DISTINCT FROM OLD.model_profile_version
       AND NEW.model_profile_hash IS NOT DISTINCT FROM OLD.model_profile_hash
       AND NEW.model_budget_hash IS NOT DISTINCT FROM OLD.model_budget_hash
       AND NEW.metadata = OLD.metadata
       AND NEW.terminal_frame_sequence IS NULL
       AND (
           (OLD.status = 'queued' AND NEW.status = 'leased'
            AND NEW.attempt_count = OLD.attempt_count + 1
            AND NEW.lease_owner IS NOT NULL AND NEW.lease_expires_at IS NOT NULL)
           OR
           (OLD.status = 'leased' AND NEW.status = 'queued'
            AND NEW.attempt_count = OLD.attempt_count
            AND OLD.lease_expires_at <= NOW()
            AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL)
       ) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_agent_worker_mutation_enabled(NEW.world_id, NEW.request_code)
       AND city_realtime_agent_mutation_enabled(NEW.world_id, NEW.terminal_frame_sequence)
       AND OLD.status = 'leased' AND NEW.status IN ('accepted', 'rejected', 'stale', 'failed_terminal', 'cancelled')
       AND NEW.world_id = OLD.world_id AND NEW.request_code = OLD.request_code
       AND NEW.agent_code = OLD.agent_code AND NEW.observation_code = OLD.observation_code
       AND NEW.observation_hash = OLD.observation_hash AND NEW.precondition_hash = OLD.precondition_hash
       AND NEW.observed_frame_sequence = OLD.observed_frame_sequence
       AND NEW.expires_at_world_time_us = OLD.expires_at_world_time_us
       AND NEW.attempt_count = OLD.attempt_count
       AND NEW.requested_frame_sequence = OLD.requested_frame_sequence
       AND NEW.model_profile_code IS NOT DISTINCT FROM OLD.model_profile_code
       AND NEW.model_profile_version IS NOT DISTINCT FROM OLD.model_profile_version
       AND NEW.model_profile_hash IS NOT DISTINCT FROM OLD.model_profile_hash
       AND NEW.model_budget_hash IS NOT DISTINCT FROM OLD.model_budget_hash
       AND NEW.metadata = OLD.metadata
       AND NEW.lease_owner IS NULL AND NEW.lease_expires_at IS NULL
       AND NEW.terminal_frame_sequence > OLD.requested_frame_sequence THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent decision requests require a sealed frame or worker lease transition'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_agent_decision_attempt()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    request_profile_code VARCHAR(64);
    request_profile_version INTEGER;
    request_profile_hash VARCHAR(64);
    request_budget_hash VARCHAR(64);
    expected_provider_code VARCHAR(64);
BEGIN
    IF TG_OP = 'INSERT'
       AND city_realtime_agent_worker_mutation_enabled(NEW.world_id, NEW.request_code)
       AND NEW.status = 'started' THEN
        SELECT request.model_profile_code, request.model_profile_version,
               request.model_profile_hash, request.model_budget_hash, profile.provider_code
        INTO request_profile_code, request_profile_version,
             request_profile_hash, request_budget_hash, expected_provider_code
        FROM city_realtime_agent_decision_requests request
        LEFT JOIN city_realtime_agent_model_profile_versions profile
          ON profile.profile_code = request.model_profile_code
         AND profile.profile_version = request.model_profile_version
        WHERE request.world_id = NEW.world_id AND request.request_code = NEW.request_code;
        IF request_profile_code IS NULL THEN
            IF NEW.provider_code <> 'fake.deterministic'
               OR NEW.model_profile_code IS NOT NULL OR NEW.model_profile_version IS NOT NULL
               OR NEW.model_profile_hash IS NOT NULL OR NEW.model_budget_hash IS NOT NULL
               OR NEW.reserved_input_tokens IS NOT NULL OR NEW.reserved_output_tokens IS NOT NULL THEN
                RAISE EXCEPTION 'legacy realtime agent attempts may only use fake.deterministic'
                    USING ERRCODE = '23514';
            END IF;
        ELSIF expected_provider_code IS NULL
           OR NEW.provider_code <> expected_provider_code
           OR NEW.model_profile_code <> request_profile_code
           OR NEW.model_profile_version <> request_profile_version
           OR NEW.model_profile_hash <> request_profile_hash
           OR NEW.model_budget_hash <> request_budget_hash THEN
            RAISE EXCEPTION 'city realtime agent attempt profile snapshot is inconsistent'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND city_realtime_agent_worker_mutation_enabled(NEW.world_id, NEW.request_code)
       AND OLD.status = 'started' AND NEW.status IN ('succeeded', 'failed')
       AND NEW.world_id = OLD.world_id AND NEW.attempt_code = OLD.attempt_code
       AND NEW.request_code = OLD.request_code AND NEW.attempt_number = OLD.attempt_number
       AND NEW.provider_code = OLD.provider_code AND NEW.request_hash = OLD.request_hash
       AND NEW.model_profile_code IS NOT DISTINCT FROM OLD.model_profile_code
       AND NEW.model_profile_version IS NOT DISTINCT FROM OLD.model_profile_version
       AND NEW.model_profile_hash IS NOT DISTINCT FROM OLD.model_profile_hash
       AND NEW.model_budget_hash IS NOT DISTINCT FROM OLD.model_budget_hash
       AND NEW.reserved_input_tokens IS NOT DISTINCT FROM OLD.reserved_input_tokens
       AND NEW.reserved_output_tokens IS NOT DISTINCT FROM OLD.reserved_output_tokens
       AND NEW.metadata = OLD.metadata THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city realtime agent decision attempts are worker-audited append-only records'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_realtime_agent_model_profile_genesis(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    binding_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO binding_count
    FROM city_realtime_agent_model_profile_world_bindings
    WHERE world_id = target_world_id
      AND binding_status = 'active'
      AND profile_code = 'system.fake.deterministic'
      AND profile_version = 1
      AND binding_source = 'system_genesis';
    IF binding_count <> 4 THEN
        RAISE EXCEPTION 'city realtime agent model profile genesis binding count is invalid'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

COMMENT ON TABLE city_realtime_agent_model_profile_versions IS
    'Immutable Agent model execution contracts. They contain no API key, endpoint, account ID, prompt body or provider response.';
COMMENT ON TABLE city_realtime_agent_model_profile_world_bindings IS
    'Revisioned world-definition model profile selection. It affects future external I/O only and is outside canonical world state.';
COMMENT ON TABLE city_realtime_agent_model_usage_windows IS
    'Conservative per-attempt hourly budget reservations. Provider-reported usage is observational and never changes city assets.';
COMMENT ON TABLE city_realtime_agent_model_attempt_budget_reservations IS
    'Append-only upper-bound budget charge for one started provider attempt.';
