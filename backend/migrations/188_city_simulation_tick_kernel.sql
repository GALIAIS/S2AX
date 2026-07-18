-- 城市模拟 F1：确定性命令、单世界 tick、不可变事件和状态哈希事实。
-- 模拟规则不依赖数据库时间；时间戳仅用于运维观测，模拟时间从固定纪元推进。

ALTER TABLE city_worlds
    ADD COLUMN IF NOT EXISTS next_command_sequence BIGINT NOT NULL DEFAULT 1;

UPDATE city_worlds
SET simulated_at = TIMESTAMPTZ '2000-01-01 00:00:00+00'
WHERE simulated_at IS NULL;

UPDATE city_worlds
SET simulation_version = 'city-f1-v1'
WHERE simulation_version = 'city-r0-v1'
  AND current_tick = 0
  AND state_hash IS NULL;

ALTER TABLE city_worlds
    ALTER COLUMN simulated_at SET DEFAULT TIMESTAMPTZ '2000-01-01 00:00:00+00',
    ALTER COLUMN simulated_at SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'city_world_next_command_sequence_check'
    ) THEN
        ALTER TABLE city_worlds
            ADD CONSTRAINT city_world_next_command_sequence_check
            CHECK (next_command_sequence > 0);
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS city_commands (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL,
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    client_request_id VARCHAR(128) NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    command_type VARCHAR(64) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    expected_world_tick BIGINT CHECK (expected_world_tick IS NULL OR expected_world_tick >= 0),
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    processed_tick BIGINT,
    result JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code VARCHAR(64),
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_commands_member_fk
        FOREIGN KEY (world_id, user_id)
        REFERENCES city_members(world_id, user_id) ON DELETE RESTRICT,
    CONSTRAINT city_command_client_request_id_check CHECK (
        char_length(client_request_id) BETWEEN 1 AND 128
        AND client_request_id = btrim(client_request_id)
    ),
    CONSTRAINT city_command_fingerprint_check CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT city_command_type_check CHECK (command_type ~ '^[a-z][a-z0-9_.-]{1,63}$'),
    CONSTRAINT city_command_payload_object_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT city_command_result_object_check CHECK (jsonb_typeof(result) = 'object'),
    CONSTRAINT city_command_status_check CHECK (status IN ('pending', 'applied', 'rejected')),
    CONSTRAINT city_command_lifecycle_check CHECK (
        (status = 'pending' AND processed_tick IS NULL AND error_code IS NULL)
        OR (status = 'applied' AND processed_tick IS NOT NULL AND error_code IS NULL)
        OR (status = 'rejected' AND processed_tick IS NOT NULL AND error_code IS NOT NULL)
    ),
    CONSTRAINT city_commands_world_sequence_unique UNIQUE (world_id, sequence),
    CONSTRAINT city_commands_world_user_request_unique UNIQUE (world_id, user_id, client_request_id),
    CONSTRAINT city_commands_id_world_tick_unique UNIQUE (id, world_id, processed_tick)
);

CREATE INDEX IF NOT EXISTS idx_city_commands_pending
    ON city_commands (world_id, sequence)
    WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_city_commands_user_submitted
    ON city_commands (user_id, submitted_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS city_ticks (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick > 0),
    step_request_id VARCHAR(128) NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    initiated_by_user_id BIGINT NOT NULL,
    simulation_version VARCHAR(32) NOT NULL,
    previous_state_hash VARCHAR(64),
    state_hash VARCHAR(64) NOT NULL,
    prng_proof VARCHAR(64) NOT NULL,
    simulated_from TIMESTAMPTZ NOT NULL,
    simulated_to TIMESTAMPTZ NOT NULL,
    first_command_sequence BIGINT,
    last_command_sequence BIGINT,
    command_count INTEGER NOT NULL DEFAULT 0 CHECK (command_count >= 0),
    applied_command_count INTEGER NOT NULL DEFAULT 0 CHECK (applied_command_count >= 0),
    rejected_command_count INTEGER NOT NULL DEFAULT 0 CHECK (rejected_command_count >= 0),
    event_count INTEGER NOT NULL CHECK (event_count > 0),
    duration_ms BIGINT NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT city_ticks_member_fk
        FOREIGN KEY (world_id, initiated_by_user_id)
        REFERENCES city_members(world_id, user_id) ON DELETE RESTRICT,
    CONSTRAINT city_tick_request_id_check CHECK (
        char_length(step_request_id) BETWEEN 1 AND 128
        AND step_request_id = btrim(step_request_id)
    ),
    CONSTRAINT city_tick_request_fingerprint_check CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT city_tick_previous_hash_check CHECK (
        previous_state_hash IS NULL OR previous_state_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_tick_state_hash_check CHECK (state_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT city_tick_prng_proof_check CHECK (prng_proof ~ '^[0-9a-f]{64}$'),
    CONSTRAINT city_tick_simulated_time_check CHECK (simulated_to > simulated_from),
    CONSTRAINT city_tick_command_counts_check CHECK (
        applied_command_count + rejected_command_count = command_count
    ),
    CONSTRAINT city_tick_command_range_check CHECK (
        (command_count = 0 AND first_command_sequence IS NULL AND last_command_sequence IS NULL)
        OR (
            command_count > 0
            AND first_command_sequence IS NOT NULL
            AND last_command_sequence IS NOT NULL
            AND first_command_sequence > 0
            AND last_command_sequence >= first_command_sequence
        )
    ),
    CONSTRAINT city_tick_wall_time_check CHECK (completed_at >= started_at),
    CONSTRAINT city_ticks_world_tick_unique UNIQUE (world_id, tick),
    CONSTRAINT city_ticks_world_request_unique UNIQUE (world_id, step_request_id)
);

CREATE INDEX IF NOT EXISTS idx_city_ticks_world_completed
    ON city_ticks (world_id, tick DESC);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'city_commands_processed_tick_fk'
    ) THEN
        ALTER TABLE city_commands
            ADD CONSTRAINT city_commands_processed_tick_fk
            FOREIGN KEY (world_id, processed_tick)
            REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT;
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS city_events (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL,
    tick BIGINT NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    command_id BIGINT,
    event_type VARCHAR(96) NOT NULL,
    aggregate_type VARCHAR(48) NOT NULL,
    aggregate_code VARCHAR(96) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_events_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT,
    CONSTRAINT city_events_command_tick_fk
        FOREIGN KEY (command_id, world_id, tick)
        REFERENCES city_commands(id, world_id, processed_tick) ON DELETE RESTRICT,
    CONSTRAINT city_event_type_check CHECK (event_type ~ '^[a-z][a-z0-9_.-]{1,95}$'),
    CONSTRAINT city_event_aggregate_type_check CHECK (aggregate_type ~ '^[a-z][a-z0-9_.-]{1,47}$'),
    CONSTRAINT city_event_aggregate_code_check CHECK (char_length(btrim(aggregate_code)) BETWEEN 1 AND 96),
    CONSTRAINT city_event_payload_object_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT city_events_world_tick_sequence_unique UNIQUE (world_id, tick, sequence)
);

CREATE INDEX IF NOT EXISTS idx_city_events_world_cursor
    ON city_events (world_id, tick, sequence);
CREATE INDEX IF NOT EXISTS idx_city_events_command
    ON city_events (command_id)
    WHERE command_id IS NOT NULL;

CREATE OR REPLACE FUNCTION guard_city_command_fact()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city commands are immutable facts' USING ERRCODE = '55000';
    END IF;

    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.world_id IS DISTINCT FROM OLD.world_id
       OR NEW.user_id IS DISTINCT FROM OLD.user_id
       OR NEW.sequence IS DISTINCT FROM OLD.sequence
       OR NEW.client_request_id IS DISTINCT FROM OLD.client_request_id
       OR NEW.request_fingerprint IS DISTINCT FROM OLD.request_fingerprint
       OR NEW.command_type IS DISTINCT FROM OLD.command_type
       OR NEW.payload IS DISTINCT FROM OLD.payload
       OR NEW.expected_world_tick IS DISTINCT FROM OLD.expected_world_tick
       OR NEW.submitted_at IS DISTINCT FROM OLD.submitted_at THEN
        RAISE EXCEPTION 'city command identity and intent are immutable' USING ERRCODE = '55000';
    END IF;

    IF OLD.status <> 'pending' OR NEW.status NOT IN ('applied', 'rejected') THEN
        RAISE EXCEPTION 'city command permits only one pending-to-terminal transition' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_command_fact_guard ON city_commands;
CREATE TRIGGER city_command_fact_guard
BEFORE UPDATE OR DELETE ON city_commands
FOR EACH ROW EXECUTE FUNCTION guard_city_command_fact();

CREATE OR REPLACE FUNCTION reject_city_immutable_fact_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% rows are immutable facts', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS city_tick_fact_guard ON city_ticks;
CREATE TRIGGER city_tick_fact_guard
BEFORE UPDATE OR DELETE ON city_ticks
FOR EACH ROW EXECUTE FUNCTION reject_city_immutable_fact_mutation();

DROP TRIGGER IF EXISTS city_event_fact_guard ON city_events;
CREATE TRIGGER city_event_fact_guard
BEFORE UPDATE OR DELETE ON city_events
FOR EACH ROW EXECUTE FUNCTION reject_city_immutable_fact_mutation();

-- 延迟到事务提交核对 tick 摘要与命令、事件事实，允许服务按依赖顺序逐步写入。
CREATE OR REPLACE FUNCTION assert_city_tick_fact_summary(target_world_id BIGINT, target_tick BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    expected_command_count INTEGER;
    expected_applied_count INTEGER;
    expected_rejected_count INTEGER;
    expected_event_count INTEGER;
    expected_first_sequence BIGINT;
    expected_last_sequence BIGINT;
    actual_command_count BIGINT;
    actual_applied_count BIGINT;
    actual_rejected_count BIGINT;
    actual_event_count BIGINT;
    actual_first_sequence BIGINT;
    actual_last_sequence BIGINT;
    actual_first_event_sequence INTEGER;
    actual_last_event_sequence INTEGER;
    completion_event_count BIGINT;
    completion_event_sequence INTEGER;
BEGIN
    SELECT command_count, applied_command_count, rejected_command_count, event_count,
           first_command_sequence, last_command_sequence
    INTO expected_command_count, expected_applied_count, expected_rejected_count,
         expected_event_count, expected_first_sequence, expected_last_sequence
    FROM city_ticks
    WHERE world_id = target_world_id AND tick = target_tick;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT COUNT(*),
           COUNT(*) FILTER (WHERE status = 'applied'),
           COUNT(*) FILTER (WHERE status = 'rejected'),
           MIN(sequence), MAX(sequence)
    INTO actual_command_count, actual_applied_count, actual_rejected_count,
         actual_first_sequence, actual_last_sequence
    FROM city_commands
    WHERE world_id = target_world_id AND processed_tick = target_tick;

    SELECT COUNT(*), MIN(sequence), MAX(sequence),
           COUNT(*) FILTER (
               WHERE event_type = 'city.tick.completed'
                 AND command_id IS NULL
           ),
           MAX(sequence) FILTER (
               WHERE event_type = 'city.tick.completed'
                 AND command_id IS NULL
           )
    INTO actual_event_count, actual_first_event_sequence, actual_last_event_sequence,
         completion_event_count, completion_event_sequence
    FROM city_events
    WHERE world_id = target_world_id AND tick = target_tick;

    IF actual_command_count <> expected_command_count
       OR actual_applied_count <> expected_applied_count
       OR actual_rejected_count <> expected_rejected_count
       OR actual_first_sequence IS DISTINCT FROM expected_first_sequence
       OR actual_last_sequence IS DISTINCT FROM expected_last_sequence THEN
        RAISE EXCEPTION 'city tick %.% command summary does not match immutable command facts',
            target_world_id, target_tick USING ERRCODE = '23514';
    END IF;

    IF actual_event_count <> expected_event_count
       OR actual_first_event_sequence IS DISTINCT FROM 1
       OR actual_last_event_sequence IS DISTINCT FROM expected_event_count
       OR completion_event_count <> 1
       OR completion_event_sequence IS DISTINCT FROM expected_event_count THEN
        RAISE EXCEPTION 'city tick %.% event summary does not match immutable event facts',
            target_world_id, target_tick USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_tick_fact_summary_from_tick()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM assert_city_tick_fact_summary(NEW.world_id, NEW.tick);
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_tick_fact_summary_from_command()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.processed_tick IS NOT NULL THEN
        PERFORM assert_city_tick_fact_summary(NEW.world_id, NEW.processed_tick);
    END IF;
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_tick_fact_summary_from_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM assert_city_tick_fact_summary(NEW.world_id, NEW.tick);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_tick_summary_check ON city_ticks;
CREATE CONSTRAINT TRIGGER city_tick_summary_check
AFTER INSERT ON city_ticks
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_tick_fact_summary_from_tick();

DROP TRIGGER IF EXISTS city_command_tick_summary_check ON city_commands;
CREATE CONSTRAINT TRIGGER city_command_tick_summary_check
AFTER UPDATE ON city_commands
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_tick_fact_summary_from_command();

DROP TRIGGER IF EXISTS city_event_tick_summary_check ON city_events;
CREATE CONSTRAINT TRIGGER city_event_tick_summary_check
AFTER INSERT ON city_events
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_tick_fact_summary_from_event();
