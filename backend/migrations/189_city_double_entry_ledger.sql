-- 城市模拟 F2：不可变复式总账、账户投影、冲正和试算平衡基础。

UPDATE city_worlds
SET simulation_version = 'city-f2-v1', state_hash = NULL
WHERE simulation_version = 'city-f1-v1';

-- 补齐补贴/转账所需科目；新世界由应用层同一模板创建。
INSERT INTO city_account_templates
    (world_id, entity_type, code, name, account_class, normal_side,
     allow_negative, is_required, sort_order, metadata)
SELECT w.id, 'household', 'other_income', 'Other Income', 'revenue', 'credit',
       FALSE, TRUE, 55, '{}'::jsonb
FROM city_worlds w
ON CONFLICT (world_id, entity_type, code) DO NOTHING;

INSERT INTO city_account_templates
    (world_id, entity_type, code, name, account_class, normal_side,
     allow_negative, is_required, sort_order, metadata)
SELECT w.id, 'household', 'transfer_expense', 'Transfer Expense', 'expense', 'debit',
       FALSE, TRUE, 75, '{}'::jsonb
FROM city_worlds w
ON CONFLICT (world_id, entity_type, code) DO NOTHING;

INSERT INTO city_account_templates
    (world_id, entity_type, code, name, account_class, normal_side,
     allow_negative, is_required, sort_order, metadata)
SELECT w.id, 'firm', 'other_income', 'Other Income', 'revenue', 'credit',
       FALSE, TRUE, 85, '{}'::jsonb
FROM city_worlds w
ON CONFLICT (world_id, entity_type, code) DO NOTHING;

INSERT INTO city_account_templates
    (world_id, entity_type, code, name, account_class, normal_side,
     allow_negative, is_required, sort_order, metadata)
SELECT w.id, 'firm', 'transfer_expense', 'Transfer Expense', 'expense', 'debit',
       FALSE, TRUE, 95, '{}'::jsonb
FROM city_worlds w
ON CONFLICT (world_id, entity_type, code) DO NOTHING;

INSERT INTO city_accounts
    (world_id, entity_id, entity_type, monetary_unit_id, template_id,
     allow_negative, current_balance_units, version, status, metadata)
SELECT e.world_id, e.id, e.entity_type, u.id, t.id,
       t.allow_negative, 0, 0, 'active', '{}'::jsonb
FROM city_economic_entities e
JOIN city_monetary_units u
  ON u.world_id = e.world_id AND u.is_base
JOIN city_account_templates t
  ON t.world_id = e.world_id
 AND t.entity_type = e.entity_type
 AND t.code IN ('other_income', 'transfer_expense')
WHERE e.entity_type IN ('household', 'firm')
ON CONFLICT (entity_id, monetary_unit_id, template_id) DO NOTHING;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'city_accounts_id_world_unit_unique'
    ) THEN
        ALTER TABLE city_accounts
            ADD CONSTRAINT city_accounts_id_world_unit_unique
            UNIQUE (id, world_id, monetary_unit_id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'city_commands_id_world_unique'
    ) THEN
        ALTER TABLE city_commands
            ADD CONSTRAINT city_commands_id_world_unique UNIQUE (id, world_id);
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS city_journals (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    monetary_unit_id BIGINT NOT NULL,
    tick BIGINT NOT NULL CHECK (tick > 0),
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    operation_key VARCHAR(128) NOT NULL,
    journal_type VARCHAR(24) NOT NULL,
    source_command_id BIGINT,
    reversal_of_journal_id BIGINT,
    description VARCHAR(256) NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted_at TIMESTAMPTZ,
    CONSTRAINT city_journals_unit_fk
        FOREIGN KEY (monetary_unit_id, world_id)
        REFERENCES city_monetary_units(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_journals_tick_fk
        FOREIGN KEY (world_id, tick)
        REFERENCES city_ticks(world_id, tick) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_journals_source_command_fk
        FOREIGN KEY (source_command_id, world_id)
        REFERENCES city_commands(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_journal_operation_key_check CHECK (
        char_length(operation_key) BETWEEN 1 AND 128
        AND operation_key = btrim(operation_key)
    ),
    CONSTRAINT city_journal_type_check CHECK (
        journal_type IN ('opening', 'cash_transfer', 'wage', 'purchase', 'tax', 'subsidy', 'reversal')
    ),
    CONSTRAINT city_journal_origin_check CHECK (
        (journal_type = 'opening' AND source_command_id IS NULL AND reversal_of_journal_id IS NULL)
        OR (journal_type = 'reversal' AND source_command_id IS NOT NULL AND reversal_of_journal_id IS NOT NULL)
        OR (journal_type NOT IN ('opening', 'reversal') AND source_command_id IS NOT NULL AND reversal_of_journal_id IS NULL)
    ),
    CONSTRAINT city_journal_description_check CHECK (char_length(description) <= 256),
    CONSTRAINT city_journal_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_journal_posted_at_check CHECK (posted_at IS NULL OR posted_at >= created_at),
    CONSTRAINT city_journal_not_self_reversal_check CHECK (
        reversal_of_journal_id IS NULL OR reversal_of_journal_id <> id
    ),
    CONSTRAINT city_journals_world_operation_unique UNIQUE (world_id, operation_key),
    CONSTRAINT city_journals_world_tick_sequence_unique UNIQUE (world_id, tick, sequence),
    CONSTRAINT city_journals_id_world_unit_unique UNIQUE (id, world_id, monetary_unit_id)
);

ALTER TABLE city_journals
    DROP CONSTRAINT IF EXISTS city_journals_reversal_fk;
ALTER TABLE city_journals
    ADD CONSTRAINT city_journals_reversal_fk
    FOREIGN KEY (reversal_of_journal_id, world_id, monetary_unit_id)
    REFERENCES city_journals(id, world_id, monetary_unit_id) ON DELETE RESTRICT
    DEFERRABLE INITIALLY DEFERRED;

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_journals_one_reversal
    ON city_journals (reversal_of_journal_id)
    WHERE reversal_of_journal_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_city_journals_world_cursor
    ON city_journals (world_id, tick, sequence);
CREATE INDEX IF NOT EXISTS idx_city_journals_source_command
    ON city_journals (source_command_id)
    WHERE source_command_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS city_journal_entries (
    id BIGSERIAL PRIMARY KEY,
    journal_id BIGINT NOT NULL,
    world_id BIGINT NOT NULL,
    monetary_unit_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    line_no INTEGER NOT NULL CHECK (line_no > 0),
    normal_side VARCHAR(8) NOT NULL,
    debit_units BIGINT NOT NULL DEFAULT 0 CHECK (debit_units >= 0),
    credit_units BIGINT NOT NULL DEFAULT 0 CHECK (credit_units >= 0),
    balance_before_units BIGINT NOT NULL,
    balance_after_units BIGINT NOT NULL,
    account_version_before BIGINT NOT NULL CHECK (account_version_before >= 0),
    account_version_after BIGINT NOT NULL CHECK (account_version_after > 0),
    memo VARCHAR(256) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_journal_entries_journal_fk
        FOREIGN KEY (journal_id, world_id, monetary_unit_id)
        REFERENCES city_journals(id, world_id, monetary_unit_id) ON DELETE RESTRICT,
    CONSTRAINT city_journal_entries_account_fk
        FOREIGN KEY (account_id, world_id, monetary_unit_id)
        REFERENCES city_accounts(id, world_id, monetary_unit_id) ON DELETE RESTRICT,
    CONSTRAINT city_journal_entry_side_check CHECK (
        (debit_units > 0 AND credit_units = 0)
        OR (credit_units > 0 AND debit_units = 0)
    ),
    CONSTRAINT city_journal_entry_normal_side_check CHECK (normal_side IN ('debit', 'credit')),
    CONSTRAINT city_journal_entry_version_check CHECK (
        account_version_after = account_version_before + 1
    ),
    CONSTRAINT city_journal_entry_memo_check CHECK (char_length(memo) <= 256),
    CONSTRAINT city_journal_entries_line_unique UNIQUE (journal_id, line_no),
    CONSTRAINT city_journal_entries_account_unique UNIQUE (journal_id, account_id)
);

CREATE INDEX IF NOT EXISTS idx_city_journal_entries_account
    ON city_journal_entries (world_id, account_id, id);

CREATE OR REPLACE FUNCTION guard_city_account_balance_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    active_journal TEXT;
    journal_is_draft BOOLEAN;
BEGIN
    IF NEW.current_balance_units IS NOT DISTINCT FROM OLD.current_balance_units
       AND NEW.version IS NOT DISTINCT FROM OLD.version THEN
        RETURN NEW;
    END IF;

    active_journal := current_setting('city.active_journal_id', TRUE);
    IF active_journal IS NULL OR active_journal !~ '^[1-9][0-9]*$' THEN
        RAISE EXCEPTION 'city account balances can only change through a draft journal'
            USING ERRCODE = '55000';
    END IF;

    SELECT posted_at IS NULL
    INTO journal_is_draft
    FROM city_journals
    WHERE id = active_journal::BIGINT
      AND world_id = NEW.world_id
      AND monetary_unit_id = NEW.monetary_unit_id;

    IF journal_is_draft IS DISTINCT FROM TRUE
       OR NEW.version <> OLD.version + 1
       OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.world_id IS DISTINCT FROM OLD.world_id
       OR NEW.entity_id IS DISTINCT FROM OLD.entity_id
       OR NEW.entity_type IS DISTINCT FROM OLD.entity_type
       OR NEW.monetary_unit_id IS DISTINCT FROM OLD.monetary_unit_id
       OR NEW.template_id IS DISTINCT FROM OLD.template_id
       OR NEW.allow_negative IS DISTINCT FROM OLD.allow_negative
       OR NEW.status IS DISTINCT FROM OLD.status
       OR NEW.metadata IS DISTINCT FROM OLD.metadata
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'invalid city account balance projection update'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_account_balance_projection_guard ON city_accounts;
CREATE TRIGGER city_account_balance_projection_guard
BEFORE UPDATE ON city_accounts
FOR EACH ROW EXECUTE FUNCTION guard_city_account_balance_projection();

CREATE OR REPLACE FUNCTION post_city_journal_entry(
    target_journal_id BIGINT,
    target_account_id BIGINT,
    target_line_no INTEGER,
    target_debit_units BIGINT,
    target_credit_units BIGINT,
    target_memo VARCHAR
)
RETURNS BIGINT
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
    target_unit_id BIGINT;
    journal_is_draft BOOLEAN;
    account_normal_side VARCHAR(8);
    account_allows_negative BOOLEAN;
    account_status VARCHAR(16);
    balance_before BIGINT;
    balance_after BIGINT;
    version_before BIGINT;
    created_entry_id BIGINT;
BEGIN
    IF target_line_no IS NULL
       OR target_line_no <= 0
       OR target_debit_units IS NULL
       OR target_credit_units IS NULL
       OR NOT (
           (target_debit_units > 0 AND target_credit_units = 0)
           OR (target_credit_units > 0 AND target_debit_units = 0)
       )
       OR char_length(COALESCE(target_memo, '')) > 256 THEN
        RAISE EXCEPTION 'invalid city journal entry'
            USING ERRCODE = '23514';
    END IF;

    SELECT world_id, monetary_unit_id, posted_at IS NULL
    INTO target_world_id, target_unit_id, journal_is_draft
    FROM city_journals
    WHERE id = target_journal_id
    FOR UPDATE;

    IF journal_is_draft IS DISTINCT FROM TRUE THEN
        RAISE EXCEPTION 'city journal % is sealed or missing', target_journal_id
            USING ERRCODE = '55000';
    END IF;

    SELECT t.normal_side, a.allow_negative, a.status,
           a.current_balance_units, a.version
    INTO account_normal_side, account_allows_negative, account_status,
         balance_before, version_before
    FROM city_accounts a
    JOIN city_account_templates t ON t.id = a.template_id
    WHERE a.id = target_account_id
      AND a.world_id = target_world_id
      AND a.monetary_unit_id = target_unit_id
    FOR UPDATE OF a;

    IF NOT FOUND OR account_status <> 'active' THEN
        RAISE EXCEPTION 'city journal account is missing or inactive'
            USING ERRCODE = '23503';
    END IF;

    IF account_normal_side = 'debit' THEN
        balance_after := balance_before + target_debit_units - target_credit_units;
    ELSE
        balance_after := balance_before + target_credit_units - target_debit_units;
    END IF;

    IF NOT account_allows_negative AND balance_after < 0 THEN
        RAISE EXCEPTION 'city account has insufficient balance'
            USING ERRCODE = '23514', CONSTRAINT = 'city_account_balance_check';
    END IF;

    PERFORM set_config('city.active_journal_id', target_journal_id::TEXT, TRUE);
    UPDATE city_accounts
    SET current_balance_units = balance_after,
        version = version + 1,
        updated_at = NOW()
    WHERE id = target_account_id;

    INSERT INTO city_journal_entries
        (journal_id, world_id, monetary_unit_id, account_id, line_no, normal_side,
         debit_units, credit_units, balance_before_units, balance_after_units,
         account_version_before, account_version_after, memo)
    VALUES
        (target_journal_id, target_world_id, target_unit_id, target_account_id,
         target_line_no, account_normal_side, target_debit_units, target_credit_units,
         balance_before, balance_after, version_before, version_before + 1,
         COALESCE(target_memo, ''))
    RETURNING id INTO created_entry_id;

    RETURN created_entry_id;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_journal_ready(target_journal_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    target_reversal_id BIGINT;
    target_world_id BIGINT;
    target_unit_id BIGINT;
    original_type VARCHAR(24);
    entry_count BIGINT;
    debit_total NUMERIC;
    credit_total NUMERIC;
    invalid_projection_count BIGINT;
    unbalanced_entity_count BIGINT;
BEGIN
    SELECT reversal_of_journal_id, world_id, monetary_unit_id
    INTO target_reversal_id, target_world_id, target_unit_id
    FROM city_journals
    WHERE id = target_journal_id AND posted_at IS NULL;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'city journal % is sealed or missing', target_journal_id
            USING ERRCODE = '55000';
    END IF;

    SELECT COUNT(*), COALESCE(SUM(debit_units), 0), COALESCE(SUM(credit_units), 0),
           COUNT(*) FILTER (
               WHERE balance_after_units <> balance_before_units +
                   CASE normal_side
                       WHEN 'debit' THEN debit_units - credit_units
                       ELSE credit_units - debit_units
                   END
                  OR account_version_after <> account_version_before + 1
           )
    INTO entry_count, debit_total, credit_total, invalid_projection_count
    FROM city_journal_entries
    WHERE journal_id = target_journal_id;

    IF entry_count < 2 OR debit_total <= 0 OR debit_total <> credit_total
       OR invalid_projection_count <> 0 THEN
        RAISE EXCEPTION 'city journal % is not balanced or has invalid projections', target_journal_id
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*)
    INTO unbalanced_entity_count
    FROM (
        SELECT a.entity_id
        FROM city_journal_entries e
        JOIN city_accounts a ON a.id = e.account_id
        WHERE e.journal_id = target_journal_id
        GROUP BY a.entity_id
        HAVING SUM(e.debit_units) <> SUM(e.credit_units)
    ) unbalanced;

    IF unbalanced_entity_count <> 0 THEN
        RAISE EXCEPTION 'city journal % is not balanced by economic entity', target_journal_id
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_journal_entries e
        JOIN city_accounts a ON a.id = e.account_id
        WHERE e.journal_id = target_journal_id
          AND (a.current_balance_units <> e.balance_after_units
               OR a.version <> e.account_version_after)
    ) THEN
        RAISE EXCEPTION 'city journal % account projection is stale', target_journal_id
            USING ERRCODE = '23514';
    END IF;

    IF target_reversal_id IS NOT NULL THEN
        SELECT journal_type
        INTO original_type
        FROM city_journals
        WHERE id = target_reversal_id
          AND world_id = target_world_id
          AND monetary_unit_id = target_unit_id
          AND posted_at IS NOT NULL;

        IF NOT FOUND OR original_type IN ('opening', 'reversal')
           OR EXISTS (
               SELECT 1
               FROM city_journal_entries original
               LEFT JOIN city_journal_entries reversal
                 ON reversal.journal_id = target_journal_id
                AND reversal.account_id = original.account_id
                AND reversal.debit_units = original.credit_units
                AND reversal.credit_units = original.debit_units
               WHERE original.journal_id = target_reversal_id
                 AND reversal.id IS NULL
           )
           OR EXISTS (
               SELECT 1
               FROM city_journal_entries reversal
               LEFT JOIN city_journal_entries original
                 ON original.journal_id = target_reversal_id
                AND original.account_id = reversal.account_id
               WHERE reversal.journal_id = target_journal_id
                 AND original.id IS NULL
           ) THEN
            RAISE EXCEPTION 'city journal % is not a valid reversal', target_journal_id
                USING ERRCODE = '23514';
        END IF;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_journal_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.posted_at IS NOT NULL THEN
            RAISE EXCEPTION 'city journals must be inserted as drafts'
                USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city journals are immutable facts' USING ERRCODE = '55000';
    END IF;

    IF OLD.posted_at IS NOT NULL
       OR NEW.posted_at IS NULL
       OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.world_id IS DISTINCT FROM OLD.world_id
       OR NEW.monetary_unit_id IS DISTINCT FROM OLD.monetary_unit_id
       OR NEW.tick IS DISTINCT FROM OLD.tick
       OR NEW.sequence IS DISTINCT FROM OLD.sequence
       OR NEW.operation_key IS DISTINCT FROM OLD.operation_key
       OR NEW.journal_type IS DISTINCT FROM OLD.journal_type
       OR NEW.source_command_id IS DISTINCT FROM OLD.source_command_id
       OR NEW.reversal_of_journal_id IS DISTINCT FROM OLD.reversal_of_journal_id
       OR NEW.description IS DISTINCT FROM OLD.description
       OR NEW.metadata IS DISTINCT FROM OLD.metadata
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'city journals permit only one draft-to-posted transition'
            USING ERRCODE = '55000';
    END IF;

    PERFORM assert_city_journal_ready(OLD.id);
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_journal_entry_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    journal_is_draft BOOLEAN;
    active_journal TEXT;
    expected_normal_side VARCHAR(8);
    projected_balance BIGINT;
    projected_version BIGINT;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        RAISE EXCEPTION 'city journal entries are immutable facts' USING ERRCODE = '55000';
    END IF;

    active_journal := current_setting('city.active_journal_id', TRUE);
    SELECT j.posted_at IS NULL, t.normal_side,
           a.current_balance_units, a.version
    INTO journal_is_draft, expected_normal_side,
         projected_balance, projected_version
    FROM city_journals j
    JOIN city_accounts a
      ON a.id = NEW.account_id
     AND a.world_id = j.world_id
     AND a.monetary_unit_id = j.monetary_unit_id
    JOIN city_account_templates t ON t.id = a.template_id
    WHERE j.id = NEW.journal_id
      AND j.world_id = NEW.world_id
      AND j.monetary_unit_id = NEW.monetary_unit_id;

    IF journal_is_draft IS DISTINCT FROM TRUE
       OR active_journal IS DISTINCT FROM NEW.journal_id::TEXT
       OR NEW.normal_side IS DISTINCT FROM expected_normal_side
       OR NEW.balance_after_units IS DISTINCT FROM projected_balance
       OR NEW.account_version_after IS DISTINCT FROM projected_version
       OR NEW.balance_after_units <> NEW.balance_before_units +
           (CASE NEW.normal_side
               WHEN 'debit' THEN NEW.debit_units - NEW.credit_units
               ELSE NEW.credit_units - NEW.debit_units
           END) THEN
        RAISE EXCEPTION 'city journal entry must match its locked account projection'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_journal_fact_guard ON city_journals;
CREATE TRIGGER city_journal_fact_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_journals
FOR EACH ROW EXECUTE FUNCTION guard_city_journal_mutation();

DROP TRIGGER IF EXISTS city_journal_entry_fact_guard ON city_journal_entries;
CREATE TRIGGER city_journal_entry_fact_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_journal_entries
FOR EACH ROW EXECUTE FUNCTION guard_city_journal_entry_mutation();

CREATE OR REPLACE FUNCTION assert_city_journal_committed(target_journal_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    target_tick BIGINT;
    target_command_id BIGINT;
    target_posted_at TIMESTAMPTZ;
BEGIN
    SELECT tick, source_command_id, posted_at
    INTO target_tick, target_command_id, target_posted_at
    FROM city_journals
    WHERE id = target_journal_id;

    IF NOT FOUND THEN
        RETURN;
    END IF;
    IF target_posted_at IS NULL THEN
        RAISE EXCEPTION 'city journal % was not posted before commit', target_journal_id
            USING ERRCODE = '23514';
    END IF;
    IF target_command_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM city_commands c
        JOIN city_journals j ON j.id = target_journal_id
        WHERE c.id = target_command_id
          AND c.world_id = j.world_id
          AND c.processed_tick = target_tick
          AND c.status = 'applied'
    ) THEN
        RAISE EXCEPTION 'city journal % source command was not applied in the same tick', target_journal_id
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_journal_committed()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM assert_city_journal_committed(NEW.id);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS city_journal_commit_check ON city_journals;
CREATE CONSTRAINT TRIGGER city_journal_commit_check
AFTER INSERT ON city_journals
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_journal_committed();
