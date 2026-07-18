-- 城市模拟引擎硬化：版本目录、显式升级路径、原子升级审计和版本写保护。
-- 规则版本由代码实现；数据库目录负责约束世界版本迁移和保留升级证据。

CREATE TABLE IF NOT EXISTS city_engine_versions (
    version VARCHAR(32) PRIMARY KEY,
    status VARCHAR(16) NOT NULL,
    canonical_format VARCHAR(48) NOT NULL,
    capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_engine_version_code_check CHECK (version ~ '^city-f[0-9]+-v[0-9]+$'),
    CONSTRAINT city_engine_version_status_check CHECK (status = 'supported'),
    CONSTRAINT city_engine_version_capabilities_check CHECK (jsonb_typeof(capabilities) = 'array')
);

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES
    ('city-f5-v1', 'supported', 'city-state-v1+gzip',
     '["control","ledger","resources","markets","snapshot","replay","recovery"]'::jsonb),
    ('city-f6-v1', 'supported', 'city-state-v1+gzip',
     '["control","ledger","resources","calendar_demography","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'city_worlds_engine_version_fk'
    ) THEN
        ALTER TABLE city_worlds
            ADD CONSTRAINT city_worlds_engine_version_fk
            FOREIGN KEY (simulation_version)
            REFERENCES city_engine_versions(version) ON DELETE RESTRICT;
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS city_engine_upgrade_paths (
    from_version VARCHAR(32) NOT NULL REFERENCES city_engine_versions(version) ON DELETE RESTRICT,
    to_version VARCHAR(32) NOT NULL REFERENCES city_engine_versions(version) ON DELETE RESTRICT,
    upgrade_code VARCHAR(48) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_engine_upgrade_paths_pk PRIMARY KEY (from_version, to_version),
    CONSTRAINT city_engine_upgrade_path_direction_check CHECK (from_version <> to_version),
    CONSTRAINT city_engine_upgrade_path_code_check CHECK (upgrade_code ~ '^[a-z][a-z0-9_]{1,47}$'),
    CONSTRAINT city_engine_upgrade_path_status_check CHECK (status = 'active')
);

CREATE OR REPLACE FUNCTION guard_city_engine_upgrade_path_acyclic()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (
        WITH RECURSIVE reachable(version) AS (
            SELECT NEW.to_version
            UNION
            SELECT path.to_version
            FROM city_engine_upgrade_paths path
            JOIN reachable ON path.from_version = reachable.version
        )
        SELECT 1 FROM reachable WHERE version = NEW.from_version
    ) THEN
        RAISE EXCEPTION 'city engine upgrade paths must remain acyclic'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_engine_upgrade_path_acyclic_guard ON city_engine_upgrade_paths;
CREATE TRIGGER city_engine_upgrade_path_acyclic_guard
BEFORE INSERT ON city_engine_upgrade_paths
FOR EACH ROW EXECUTE FUNCTION guard_city_engine_upgrade_path_acyclic();

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f5-v1', 'city-f6-v1', 'f5_to_f6_v1')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_world_upgrade_runs (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    requested_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    client_request_id VARCHAR(128) NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    from_version VARCHAR(32) NOT NULL,
    to_version VARCHAR(32) NOT NULL,
    from_tick BIGINT NOT NULL CHECK (from_tick >= 0),
    dry_run BOOLEAN NOT NULL DEFAULT TRUE,
    status VARCHAR(16) NOT NULL DEFAULT 'running',
    source_snapshot_id BIGINT NOT NULL,
    target_snapshot_id BIGINT,
    before_state_hash VARCHAR(64) NOT NULL,
    after_state_hash VARCHAR(64),
    error_code VARCHAR(64),
    error_detail VARCHAR(512),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT city_world_upgrade_runs_path_fk
        FOREIGN KEY (from_version, to_version)
        REFERENCES city_engine_upgrade_paths(from_version, to_version) ON DELETE RESTRICT,
    CONSTRAINT city_world_upgrade_runs_source_snapshot_fk
        FOREIGN KEY (source_snapshot_id, world_id)
        REFERENCES city_snapshots(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_world_upgrade_runs_target_snapshot_fk
        FOREIGN KEY (target_snapshot_id, world_id)
        REFERENCES city_snapshots(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_world_upgrade_run_fingerprint_check
        CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT city_world_upgrade_run_hash_check CHECK (
        before_state_hash ~ '^[0-9a-f]{64}$'
        AND (after_state_hash IS NULL OR after_state_hash ~ '^[0-9a-f]{64}$')
    ),
    CONSTRAINT city_world_upgrade_run_status_check
        CHECK (status IN ('running', 'planned', 'applied', 'failed')),
    CONSTRAINT city_world_upgrade_run_terminal_check CHECK (
        (status = 'running' AND completed_at IS NULL AND after_state_hash IS NULL
            AND target_snapshot_id IS NULL AND error_code IS NULL AND error_detail IS NULL)
        OR (status = 'planned' AND dry_run AND completed_at IS NOT NULL
            AND after_state_hash IS NOT NULL AND target_snapshot_id IS NULL
            AND error_code IS NULL AND error_detail IS NULL)
        OR (status = 'applied' AND NOT dry_run AND completed_at IS NOT NULL
            AND after_state_hash IS NOT NULL AND target_snapshot_id IS NOT NULL
            AND error_code IS NULL AND error_detail IS NULL)
        OR (status = 'failed' AND completed_at IS NOT NULL AND target_snapshot_id IS NULL
            AND after_state_hash IS NULL AND error_code IS NOT NULL AND error_detail IS NOT NULL)
    ),
    CONSTRAINT city_world_upgrade_runs_request_unique
        UNIQUE (world_id, requested_by_user_id, client_request_id),
    CONSTRAINT city_world_upgrade_runs_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_world_upgrade_runs_world_created
    ON city_world_upgrade_runs (world_id, id DESC);

CREATE TABLE IF NOT EXISTS city_tick_failures (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    requested_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    client_request_id VARCHAR(128) NOT NULL,
    simulation_version VARCHAR(32) NOT NULL REFERENCES city_engine_versions(version) ON DELETE RESTRICT,
    world_tick BIGINT NOT NULL CHECK (world_tick >= 0),
    expected_world_tick BIGINT CHECK (expected_world_tick IS NULL OR expected_world_tick >= 0),
    error_code VARCHAR(64) NOT NULL,
    error_detail VARCHAR(512) NOT NULL,
    failed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_tick_failure_request_check CHECK (char_length(client_request_id) BETWEEN 1 AND 128),
    CONSTRAINT city_tick_failure_error_code_check CHECK (error_code ~ '^[A-Z][A-Z0-9_]{1,63}$')
);

CREATE INDEX IF NOT EXISTS idx_city_tick_failures_world_created
    ON city_tick_failures (world_id, id DESC);

DROP TRIGGER IF EXISTS city_tick_failure_immutable_guard ON city_tick_failures;
CREATE TRIGGER city_tick_failure_immutable_guard
BEFORE UPDATE OR DELETE ON city_tick_failures
FOR EACH ROW EXECUTE FUNCTION reject_city_immutable_fact_mutation();

CREATE OR REPLACE FUNCTION guard_city_engine_definition_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_engine_version_immutable_guard ON city_engine_versions;
CREATE TRIGGER city_engine_version_immutable_guard
BEFORE UPDATE OR DELETE ON city_engine_versions
FOR EACH ROW EXECUTE FUNCTION guard_city_engine_definition_immutable();

DROP TRIGGER IF EXISTS city_engine_upgrade_path_immutable_guard ON city_engine_upgrade_paths;
CREATE TRIGGER city_engine_upgrade_path_immutable_guard
BEFORE UPDATE OR DELETE ON city_engine_upgrade_paths
FOR EACH ROW EXECUTE FUNCTION guard_city_engine_definition_immutable();

CREATE OR REPLACE FUNCTION guard_city_world_upgrade_run_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    snapshot_version VARCHAR(32);
    snapshot_tick BIGINT;
    snapshot_hash VARCHAR(64);
BEGIN
    SELECT simulation_version, current_tick
    INTO world_version, world_tick
    FROM city_worlds WHERE id = NEW.world_id;

    SELECT simulation_version, tick, state_hash
    INTO snapshot_version, snapshot_tick, snapshot_hash
    FROM city_snapshots
    WHERE id = NEW.source_snapshot_id AND world_id = NEW.world_id;

    IF world_version IS DISTINCT FROM NEW.from_version
       OR world_tick IS DISTINCT FROM NEW.from_tick
       OR snapshot_version IS DISTINCT FROM NEW.from_version
       OR snapshot_tick IS DISTINCT FROM NEW.from_tick
       OR snapshot_hash IS DISTINCT FROM NEW.before_state_hash THEN
        RAISE EXCEPTION 'city upgrade source does not match the locked world and snapshot'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_world_upgrade_run_evidence()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    world_hash VARCHAR(64);
    target_version VARCHAR(32);
    target_tick BIGINT;
    target_hash VARCHAR(64);
BEGIN
    IF OLD.status <> 'running' OR NEW.status = 'running' THEN
        RETURN NEW;
    END IF;

    SELECT simulation_version, current_tick, state_hash
    INTO world_version, world_tick, world_hash
    FROM city_worlds WHERE id = NEW.world_id;

    IF NEW.status = 'applied' THEN
        SELECT simulation_version, tick, state_hash
        INTO target_version, target_tick, target_hash
        FROM city_snapshots
        WHERE id = NEW.target_snapshot_id AND world_id = NEW.world_id;

        IF world_version IS DISTINCT FROM NEW.to_version
           OR world_tick IS DISTINCT FROM NEW.from_tick
           OR world_hash IS DISTINCT FROM NEW.after_state_hash
           OR target_version IS DISTINCT FROM NEW.to_version
           OR target_tick IS DISTINCT FROM NEW.from_tick
           OR target_hash IS DISTINCT FROM NEW.after_state_hash THEN
            RAISE EXCEPTION 'applied city upgrade evidence does not match the world and target snapshot'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.status IN ('planned', 'failed') THEN
        IF world_version IS DISTINCT FROM NEW.from_version
           OR world_tick IS DISTINCT FROM NEW.from_tick
           OR world_hash IS DISTINCT FROM NEW.before_state_hash THEN
            RAISE EXCEPTION 'non-applied city upgrade changed the source world'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_world_upgrade_run_evidence_guard ON city_world_upgrade_runs;
CREATE TRIGGER city_world_upgrade_run_evidence_guard
BEFORE UPDATE ON city_world_upgrade_runs
FOR EACH ROW EXECUTE FUNCTION guard_city_world_upgrade_run_evidence();

DROP TRIGGER IF EXISTS city_world_upgrade_run_insert_guard ON city_world_upgrade_runs;
CREATE TRIGGER city_world_upgrade_run_insert_guard
BEFORE INSERT ON city_world_upgrade_runs
FOR EACH ROW EXECUTE FUNCTION guard_city_world_upgrade_run_insert();

CREATE OR REPLACE FUNCTION guard_city_world_upgrade_run_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city upgrade runs are immutable audit records' USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'running' OR NEW.status = 'running'
       OR (OLD.id, OLD.world_id, OLD.requested_by_user_id, OLD.client_request_id,
           OLD.request_fingerprint, OLD.from_version, OLD.to_version, OLD.from_tick,
           OLD.dry_run, OLD.source_snapshot_id, OLD.before_state_hash, OLD.started_at)
          IS DISTINCT FROM
          (NEW.id, NEW.world_id, NEW.requested_by_user_id, NEW.client_request_id,
           NEW.request_fingerprint, NEW.from_version, NEW.to_version, NEW.from_tick,
           NEW.dry_run, NEW.source_snapshot_id, NEW.before_state_hash, NEW.started_at) THEN
        RAISE EXCEPTION 'city upgrade run permits only one running-to-terminal transition'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_world_upgrade_run_write_guard ON city_world_upgrade_runs;
CREATE TRIGGER city_world_upgrade_run_write_guard
BEFORE UPDATE OR DELETE ON city_world_upgrade_runs
FOR EACH ROW EXECUTE FUNCTION guard_city_world_upgrade_run_write();

CREATE OR REPLACE FUNCTION city_engine_upgrade_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM city_world_upgrade_runs upgrade
        WHERE upgrade.id = CASE
            WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
            THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT
            ELSE NULL
        END
          AND upgrade.world_id = target_world_id
          AND upgrade.status = 'running'
    )
$$;

CREATE OR REPLACE FUNCTION guard_city_world_engine_version()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.simulation_version IS DISTINCT FROM OLD.simulation_version
       AND NOT city_engine_upgrade_write_enabled(OLD.id) THEN
        RAISE EXCEPTION 'city simulation version can only change through an audited upgrade'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_world_engine_version_guard ON city_worlds;
CREATE TRIGGER city_world_engine_version_guard
BEFORE UPDATE OF simulation_version ON city_worlds
FOR EACH ROW EXECUTE FUNCTION guard_city_world_engine_version();

-- F5 世界可在继续运行后显式升级。其 F6 日历尚未产生边界时，升级审计允许
-- 把初始日历投影重新锚定到当前模拟日期；已生成 F6 边界的世界不能走此路径。
CREATE OR REPLACE FUNCTION guard_city_calendar_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city calendar projection cannot be deleted' USING ERRCODE = '55000';
    END IF;
    IF NEW.world_id IS DISTINCT FROM OLD.world_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'city calendar projection identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF NOT city_recovery_write_enabled(NEW.world_id)
       AND NOT city_f6_boundary_write_enabled(NEW.world_id)
       AND NOT city_engine_upgrade_write_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city calendar can only advance through an immutable boundary or audited upgrade'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
