-- 城市模拟 F5：不可变规范快照、逐 tick 重放审计，以及同 tick 投影恢复。
-- 快照不是新的业务事实来源；journal/resource/settlement/command 仍是状态迁移的唯一事实。

UPDATE city_worlds
SET simulation_version = 'city-f5-v1', state_hash = NULL
WHERE simulation_version = 'city-f4-v1';

CREATE TABLE IF NOT EXISTS city_snapshots (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick >= 0),
    source_tick_id BIGINT REFERENCES city_ticks(id) ON DELETE RESTRICT,
    simulation_version VARCHAR(32) NOT NULL,
    snapshot_format VARCHAR(48) NOT NULL DEFAULT 'city-state-v1+gzip',
    reason VARCHAR(16) NOT NULL,
    state_hash VARCHAR(64) NOT NULL,
    payload_hash VARCHAR(64) NOT NULL,
    payload BYTEA NOT NULL,
    uncompressed_size BIGINT NOT NULL CHECK (uncompressed_size > 0),
    compressed_size BIGINT NOT NULL CHECK (compressed_size > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_snapshot_format_check CHECK (snapshot_format = 'city-state-v1+gzip'),
    CONSTRAINT city_snapshot_reason_check CHECK (reason IN ('genesis', 'baseline', 'tick', 'manual')),
    CONSTRAINT city_snapshot_hash_check CHECK (
        state_hash ~ '^[0-9a-f]{64}$' AND payload_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_snapshot_payload_size_check CHECK (octet_length(payload) = compressed_size),
    CONSTRAINT city_snapshot_tick_source_check CHECK (
        (tick = 0 AND source_tick_id IS NULL AND reason IN ('genesis', 'baseline', 'manual'))
        OR (tick > 0 AND source_tick_id IS NOT NULL AND reason IN ('baseline', 'tick', 'manual'))
    ),
    CONSTRAINT city_snapshots_world_tick_unique UNIQUE (world_id, tick),
    CONSTRAINT city_snapshots_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_snapshots_world_cursor
    ON city_snapshots (world_id, tick DESC);

CREATE TABLE IF NOT EXISTS city_replay_runs (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    requested_by_user_id BIGINT NOT NULL,
    client_request_id VARCHAR(128) NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    base_snapshot_id BIGINT NOT NULL,
    from_tick BIGINT NOT NULL CHECK (from_tick >= 0),
    target_tick BIGINT NOT NULL CHECK (target_tick >= from_tick),
    status VARCHAR(16) NOT NULL DEFAULT 'running',
    expected_state_hash VARCHAR(64),
    actual_state_hash VARCHAR(64),
    verified_tick_count BIGINT NOT NULL DEFAULT 0 CHECK (verified_tick_count >= 0),
    divergence_tick BIGINT,
    divergence_path VARCHAR(512),
    error_code VARCHAR(64),
    error_detail VARCHAR(512),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT city_replay_runs_snapshot_fk
        FOREIGN KEY (base_snapshot_id, world_id)
        REFERENCES city_snapshots(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_replay_run_status_check CHECK (status IN ('running', 'verified', 'diverged', 'failed')),
    CONSTRAINT city_replay_run_hash_check CHECK (
        (expected_state_hash IS NULL OR expected_state_hash ~ '^[0-9a-f]{64}$')
        AND (actual_state_hash IS NULL OR actual_state_hash ~ '^[0-9a-f]{64}$')
    ),
    CONSTRAINT city_replay_run_fingerprint_check CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT city_replay_run_terminal_check CHECK (
        (status = 'running' AND completed_at IS NULL)
        OR (status <> 'running' AND completed_at IS NOT NULL)
    ),
    CONSTRAINT city_replay_run_divergence_check CHECK (
        (status = 'diverged' AND divergence_tick IS NOT NULL)
        OR (status <> 'diverged')
    ),
    CONSTRAINT city_replay_runs_request_unique
        UNIQUE (world_id, requested_by_user_id, client_request_id),
    CONSTRAINT city_replay_runs_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_replay_runs_world_created
    ON city_replay_runs (world_id, id DESC);

CREATE TABLE IF NOT EXISTS city_recovery_runs (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    requested_by_user_id BIGINT NOT NULL,
    client_request_id VARCHAR(128) NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    replay_run_id BIGINT NOT NULL,
    target_snapshot_id BIGINT NOT NULL,
    target_tick BIGINT NOT NULL CHECK (target_tick >= 0),
    status VARCHAR(16) NOT NULL DEFAULT 'running',
    before_state_hash VARCHAR(64),
    target_state_hash VARCHAR(64) NOT NULL,
    after_state_hash VARCHAR(64),
    restored_projection_count INTEGER NOT NULL DEFAULT 0 CHECK (restored_projection_count >= 0),
    error_code VARCHAR(64),
    error_detail VARCHAR(512),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT city_recovery_runs_replay_fk
        FOREIGN KEY (replay_run_id, world_id)
        REFERENCES city_replay_runs(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_recovery_runs_snapshot_fk
        FOREIGN KEY (target_snapshot_id, world_id)
        REFERENCES city_snapshots(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_recovery_run_status_check CHECK (status IN ('running', 'applied', 'failed')),
    CONSTRAINT city_recovery_run_hash_check CHECK (
        target_state_hash ~ '^[0-9a-f]{64}$'
        AND (before_state_hash IS NULL OR before_state_hash ~ '^[0-9a-f]{64}$')
        AND (after_state_hash IS NULL OR after_state_hash ~ '^[0-9a-f]{64}$')
    ),
    CONSTRAINT city_recovery_run_fingerprint_check CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT city_recovery_run_terminal_check CHECK (
        (status = 'running' AND completed_at IS NULL)
        OR (status <> 'running' AND completed_at IS NOT NULL)
    ),
    CONSTRAINT city_recovery_runs_request_unique
        UNIQUE (world_id, requested_by_user_id, client_request_id),
    CONSTRAINT city_recovery_runs_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_recovery_runs_world_created
    ON city_recovery_runs (world_id, id DESC);

CREATE OR REPLACE FUNCTION guard_city_snapshot_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    source_world_id BIGINT;
    source_tick BIGINT;
    source_hash VARCHAR(64);
    source_version VARCHAR(32);
BEGIN
    IF NEW.tick = 0 THEN
        RETURN NEW;
    END IF;
    SELECT world_id, tick, state_hash, simulation_version
    INTO source_world_id, source_tick, source_hash, source_version
    FROM city_ticks WHERE id = NEW.source_tick_id;
    IF source_world_id IS DISTINCT FROM NEW.world_id OR source_tick IS DISTINCT FROM NEW.tick THEN
        RAISE EXCEPTION 'city snapshot source tick does not match world and tick'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.reason = 'tick'
       AND (source_hash IS DISTINCT FROM NEW.state_hash
            OR source_version IS DISTINCT FROM NEW.simulation_version) THEN
        RAISE EXCEPTION 'city tick snapshot does not match its immutable tick fact'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_snapshot_insert_guard ON city_snapshots;
CREATE TRIGGER city_snapshot_insert_guard
BEFORE INSERT ON city_snapshots
FOR EACH ROW EXECUTE FUNCTION guard_city_snapshot_insert();

DROP TRIGGER IF EXISTS city_snapshot_immutable_guard ON city_snapshots;
CREATE TRIGGER city_snapshot_immutable_guard
BEFORE UPDATE OR DELETE ON city_snapshots
FOR EACH ROW EXECUTE FUNCTION reject_city_immutable_fact_mutation();

CREATE OR REPLACE FUNCTION guard_city_replay_run_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city replay runs are immutable audit records' USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'running' OR NEW.status = 'running'
       OR (OLD.id, OLD.world_id, OLD.requested_by_user_id, OLD.client_request_id,
           OLD.request_fingerprint, OLD.base_snapshot_id, OLD.from_tick,
           OLD.target_tick, OLD.started_at)
          IS DISTINCT FROM
          (NEW.id, NEW.world_id, NEW.requested_by_user_id, NEW.client_request_id,
           NEW.request_fingerprint, NEW.base_snapshot_id, NEW.from_tick,
           NEW.target_tick, NEW.started_at) THEN
        RAISE EXCEPTION 'city replay run permits only one running-to-terminal transition'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_replay_run_write_guard ON city_replay_runs;
CREATE TRIGGER city_replay_run_write_guard
BEFORE UPDATE OR DELETE ON city_replay_runs
FOR EACH ROW EXECUTE FUNCTION guard_city_replay_run_write();

CREATE OR REPLACE FUNCTION guard_city_recovery_run_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city recovery runs are immutable audit records' USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'running' OR NEW.status = 'running'
       OR (OLD.id, OLD.world_id, OLD.requested_by_user_id, OLD.client_request_id,
           OLD.request_fingerprint, OLD.replay_run_id, OLD.target_snapshot_id,
           OLD.target_tick, OLD.target_state_hash, OLD.started_at)
          IS DISTINCT FROM
          (NEW.id, NEW.world_id, NEW.requested_by_user_id, NEW.client_request_id,
           NEW.request_fingerprint, NEW.replay_run_id, NEW.target_snapshot_id,
           NEW.target_tick, NEW.target_state_hash, NEW.started_at) THEN
        RAISE EXCEPTION 'city recovery run permits only one running-to-terminal transition'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_recovery_run_write_guard ON city_recovery_runs;
CREATE TRIGGER city_recovery_run_write_guard
BEFORE UPDATE OR DELETE ON city_recovery_runs
FOR EACH ROW EXECUTE FUNCTION guard_city_recovery_run_write();

CREATE OR REPLACE FUNCTION city_recovery_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_recovery_runs recovery
        WHERE recovery.id = CASE
            WHEN COALESCE(current_setting('sub2api.city_recovery_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.city_recovery_run_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND recovery.world_id = target_world_id
          AND recovery.status = 'running'
    )
$$;

-- F2 账户投影在恢复事务内允许回写，但身份和静态归属仍不可更改。
CREATE OR REPLACE FUNCTION guard_city_account_balance_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    active_journal TEXT;
    journal_is_draft BOOLEAN;
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.world_id IS DISTINCT FROM OLD.world_id
       OR NEW.entity_id IS DISTINCT FROM OLD.entity_id
       OR NEW.entity_type IS DISTINCT FROM OLD.entity_type
       OR NEW.monetary_unit_id IS DISTINCT FROM OLD.monetary_unit_id
       OR NEW.template_id IS DISTINCT FROM OLD.template_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'city account identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.current_balance_units IS NOT DISTINCT FROM OLD.current_balance_units
       AND NEW.version IS NOT DISTINCT FROM OLD.version THEN
        RETURN NEW;
    END IF;
    IF city_recovery_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    active_journal := current_setting('city.active_journal_id', TRUE);
    IF active_journal IS NULL OR active_journal !~ '^[1-9][0-9]*$' THEN
        RAISE EXCEPTION 'city account balances can only change through a draft journal'
            USING ERRCODE = '55000';
    END IF;
    SELECT posted_at IS NULL INTO journal_is_draft
    FROM city_journals
    WHERE id = active_journal::BIGINT
      AND world_id = NEW.world_id
      AND monetary_unit_id = NEW.monetary_unit_id;
    IF journal_is_draft IS DISTINCT FROM TRUE OR NEW.version <> OLD.version + 1
       OR NEW.allow_negative IS DISTINCT FROM OLD.allow_negative
       OR NEW.status IS DISTINCT FROM OLD.status
       OR NEW.metadata IS DISTINCT FROM OLD.metadata THEN
        RAISE EXCEPTION 'invalid city account balance projection update'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

-- F3 库存投影使用相同的受审计恢复闸门。
CREATE OR REPLACE FUNCTION guard_city_inventory_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    active_operation TEXT;
    operation_is_draft BOOLEAN;
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.world_id IS DISTINCT FROM OLD.world_id
       OR NEW.entity_id IS DISTINCT FROM OLD.entity_id
       OR NEW.entity_type IS DISTINCT FROM OLD.entity_type
       OR NEW.district_id IS DISTINCT FROM OLD.district_id
       OR NEW.resource_id IS DISTINCT FROM OLD.resource_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'city inventory balance identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.quantity_units IS NOT DISTINCT FROM OLD.quantity_units
       AND NEW.version IS NOT DISTINCT FROM OLD.version THEN
        RETURN NEW;
    END IF;
    IF city_recovery_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    active_operation := current_setting('city.active_resource_operation_id', TRUE);
    IF active_operation IS NULL OR active_operation !~ '^[1-9][0-9]*$' THEN
        RAISE EXCEPTION 'city inventory balances can only change through a draft resource operation'
            USING ERRCODE = '55000';
    END IF;
    SELECT posted_at IS NULL INTO operation_is_draft
    FROM city_resource_operations
    WHERE id = active_operation::BIGINT AND world_id = NEW.world_id;
    IF operation_is_draft IS DISTINCT FROM TRUE OR NEW.version <> OLD.version + 1
       OR NEW.opening_quantity_units IS DISTINCT FROM OLD.opening_quantity_units
       OR NEW.status IS DISTINCT FROM OLD.status
       OR NEW.metadata IS DISTINCT FROM OLD.metadata THEN
        RAISE EXCEPTION 'invalid city inventory projection update'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
