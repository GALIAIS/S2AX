-- Add a balanced journal/posting layer beneath the existing wallet read model.
-- Existing APIs keep their current contract; every mutation now has an equal
-- and opposite posting in the same database transaction.

CREATE TABLE IF NOT EXISTS virtual_currency_journals (
    id BIGSERIAL PRIMARY KEY,
    currency_id BIGINT NOT NULL REFERENCES virtual_currencies(id) ON DELETE RESTRICT,
    initiator_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    group_id BIGINT REFERENCES groups(id) ON DELETE RESTRICT,
    entry_type VARCHAR(24) NOT NULL,
    source_type VARCHAR(32) NOT NULL,
    source_id VARCHAR(128),
    idempotency_key VARCHAR(128) NOT NULL,
    request_fingerprint CHAR(64) NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted_at TIMESTAMPTZ,
    CONSTRAINT virtual_currency_journal_identity_unique
        UNIQUE (currency_id, initiator_user_id, idempotency_key),
    CONSTRAINT virtual_currency_journal_id_currency_unique
        UNIQUE (id, currency_id),
    CONSTRAINT virtual_currency_journal_idempotency_key_check
        CHECK (length(trim(idempotency_key)) BETWEEN 1 AND 128),
    CONSTRAINT virtual_currency_journal_request_fingerprint_check
        CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT virtual_currency_journal_entry_type_check
        CHECK (entry_type IN ('grant', 'spend', 'refund', 'adjustment', 'reserve', 'commit', 'release', 'expire')),
    CONSTRAINT virtual_currency_journal_source_type_check
        CHECK (source_type ~ '^[a-z][a-z0-9_.-]{1,31}$'),
    CONSTRAINT virtual_currency_journal_posted_at_check
        CHECK (posted_at IS NULL OR posted_at >= created_at)
);

CREATE INDEX IF NOT EXISTS idx_virtual_currency_journals_source
    ON virtual_currency_journals (source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_virtual_currency_journals_created
    ON virtual_currency_journals (currency_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS virtual_currency_postings (
    id BIGSERIAL PRIMARY KEY,
    journal_id BIGINT NOT NULL,
    currency_id BIGINT NOT NULL,
    user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    account_kind VARCHAR(32) NOT NULL,
    amount_units BIGINT NOT NULL CHECK (amount_units <> 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT virtual_currency_posting_journal_currency_fkey
        FOREIGN KEY (journal_id, currency_id)
        REFERENCES virtual_currency_journals(id, currency_id)
        ON DELETE RESTRICT,
    CONSTRAINT virtual_currency_posting_account_kind_check
        CHECK (account_kind IN (
            'user_available',
            'user_reserved',
            'system_issuance',
            'system_sink',
            'system_adjustment'
        )),
    CONSTRAINT virtual_currency_posting_owner_check CHECK (
        (account_kind IN ('user_available', 'user_reserved') AND user_id IS NOT NULL)
        OR
        (account_kind IN ('system_issuance', 'system_sink', 'system_adjustment') AND user_id IS NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_virtual_currency_postings_user_account
    ON virtual_currency_postings (journal_id, account_kind, user_id)
    WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_virtual_currency_postings_system_account
    ON virtual_currency_postings (journal_id, account_kind)
    WHERE user_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_virtual_currency_postings_account
    ON virtual_currency_postings (currency_id, account_kind, user_id, id);

ALTER TABLE virtual_currency_ledger_entries
    ADD COLUMN IF NOT EXISTS journal_id BIGINT;

-- Backfill one balanced journal for every historical wallet mutation.
INSERT INTO virtual_currency_journals
    (currency_id, initiator_user_id, group_id, entry_type, source_type, source_id,
     idempotency_key, request_fingerprint, reason, metadata, created_by, created_at, posted_at)
SELECT l.currency_id, l.user_id, l.group_id, l.entry_type, l.source_type, l.source_id,
       l.idempotency_key, l.request_fingerprint, l.reason, l.metadata, l.created_by, l.created_at, l.created_at
FROM virtual_currency_ledger_entries l
WHERE NOT EXISTS (
    SELECT 1
    FROM virtual_currency_journals j
    WHERE j.currency_id = l.currency_id
      AND j.initiator_user_id = l.user_id
      AND j.idempotency_key = l.idempotency_key
)
ON CONFLICT (currency_id, initiator_user_id, idempotency_key) DO NOTHING;

UPDATE virtual_currency_ledger_entries l
SET journal_id = j.id
FROM virtual_currency_journals j
WHERE l.journal_id IS NULL
  AND j.currency_id = l.currency_id
  AND j.initiator_user_id = l.user_id
  AND j.idempotency_key = l.idempotency_key;

INSERT INTO virtual_currency_postings
    (journal_id, currency_id, user_id, account_kind, amount_units, created_at)
SELECT l.journal_id, l.currency_id, posting.user_id, posting.account_kind,
       posting.amount_units, l.created_at
FROM virtual_currency_ledger_entries l
CROSS JOIN LATERAL (
    VALUES
        ('user_available'::VARCHAR(32), l.user_id, l.available_delta_units),
        ('user_reserved'::VARCHAR(32), l.user_id, l.reserved_delta_units),
        (
            CASE
                WHEN l.entry_type = 'grant' THEN 'system_issuance'
                WHEN l.entry_type IN ('spend', 'commit', 'refund') THEN 'system_sink'
                ELSE 'system_adjustment'
            END::VARCHAR(32),
            NULL::BIGINT,
            -l.delta_units
        )
) AS posting(account_kind, user_id, amount_units)
WHERE posting.amount_units <> 0
ON CONFLICT DO NOTHING;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM virtual_currency_ledger_entries WHERE journal_id IS NULL) THEN
        RAISE EXCEPTION 'virtual currency journal backfill left unlinked ledger entries';
    END IF;
    IF EXISTS (
        SELECT j.id
        FROM virtual_currency_journals j
        LEFT JOIN virtual_currency_postings p ON p.journal_id = j.id
        GROUP BY j.id
        HAVING COUNT(p.id) < 2 OR COALESCE(SUM(p.amount_units), 0) <> 0
    ) THEN
        RAISE EXCEPTION 'virtual currency journal backfill is not balanced';
    END IF;
END $$;

ALTER TABLE virtual_currency_ledger_entries
    ALTER COLUMN journal_id SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'virtual_currency_ledger_journal_fkey'
    ) THEN
        ALTER TABLE virtual_currency_ledger_entries
            ADD CONSTRAINT virtual_currency_ledger_journal_fkey
            FOREIGN KEY (journal_id) REFERENCES virtual_currency_journals(id) ON DELETE RESTRICT;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_virtual_currency_ledger_journal
    ON virtual_currency_ledger_entries (journal_id);

-- Currency history must survive account removal attempts. User deletion must
-- first follow an explicit archival/anonymisation process instead of cascading.
ALTER TABLE virtual_currency_wallets
    DROP CONSTRAINT IF EXISTS virtual_currency_wallets_user_id_fkey;
ALTER TABLE virtual_currency_wallets
    ADD CONSTRAINT virtual_currency_wallets_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT;

ALTER TABLE virtual_currency_ledger_entries
    DROP CONSTRAINT IF EXISTS virtual_currency_ledger_entries_user_id_fkey;
ALTER TABLE virtual_currency_ledger_entries
    ADD CONSTRAINT virtual_currency_ledger_entries_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT;

ALTER TABLE virtual_currency_ledger_entries
    DROP CONSTRAINT IF EXISTS virtual_currency_ledger_entries_group_id_fkey;
ALTER TABLE virtual_currency_ledger_entries
    ADD CONSTRAINT virtual_currency_ledger_entries_group_id_fkey
    FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE RESTRICT;

ALTER TABLE virtual_currency_ledger_entries
    DROP CONSTRAINT IF EXISTS virtual_currency_ledger_entries_created_by_fkey;
ALTER TABLE virtual_currency_ledger_entries
    ADD CONSTRAINT virtual_currency_ledger_entries_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT;

ALTER TABLE virtual_currency_holds
    DROP CONSTRAINT IF EXISTS virtual_currency_holds_user_id_fkey;
ALTER TABLE virtual_currency_holds
    ADD CONSTRAINT virtual_currency_holds_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT;

ALTER TABLE virtual_currency_holds
    DROP CONSTRAINT IF EXISTS virtual_currency_holds_group_id_fkey;
ALTER TABLE virtual_currency_holds
    ADD CONSTRAINT virtual_currency_holds_group_id_fkey
    FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE RESTRICT;

-- A journal is built as a draft inside one transaction and sealed once all
-- postings have been inserted. After sealing, history is append-only at the
-- database boundary, not merely by application convention.
CREATE OR REPLACE FUNCTION guard_virtual_currency_journal_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'virtual currency journals are immutable'
            USING ERRCODE = '55000';
    END IF;

    IF OLD.posted_at IS NOT NULL
       OR NEW.posted_at IS NULL
       OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.currency_id IS DISTINCT FROM OLD.currency_id
       OR NEW.initiator_user_id IS DISTINCT FROM OLD.initiator_user_id
       OR NEW.group_id IS DISTINCT FROM OLD.group_id
       OR NEW.entry_type IS DISTINCT FROM OLD.entry_type
       OR NEW.source_type IS DISTINCT FROM OLD.source_type
       OR NEW.source_id IS DISTINCT FROM OLD.source_id
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.request_fingerprint IS DISTINCT FROM OLD.request_fingerprint
       OR NEW.reason IS DISTINCT FROM OLD.reason
       OR NEW.metadata IS DISTINCT FROM OLD.metadata
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'virtual currency journals are immutable after creation'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_virtual_currency_child_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_journal_id BIGINT;
    journal_is_open BOOLEAN;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        RAISE EXCEPTION 'virtual currency % rows are immutable', TG_TABLE_NAME
            USING ERRCODE = '55000';
    END IF;

    target_journal_id := NEW.journal_id;
    SELECT posted_at IS NULL
    INTO journal_is_open
    FROM virtual_currency_journals
    WHERE id = target_journal_id;

    IF journal_is_open IS DISTINCT FROM TRUE THEN
        RAISE EXCEPTION 'virtual currency journal % is sealed or missing', target_journal_id
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS virtual_currency_journal_immutable ON virtual_currency_journals;
CREATE TRIGGER virtual_currency_journal_immutable
BEFORE UPDATE OR DELETE ON virtual_currency_journals
FOR EACH ROW EXECUTE FUNCTION guard_virtual_currency_journal_mutation();

DROP TRIGGER IF EXISTS virtual_currency_ledger_immutable ON virtual_currency_ledger_entries;
CREATE TRIGGER virtual_currency_ledger_immutable
BEFORE INSERT OR UPDATE OR DELETE ON virtual_currency_ledger_entries
FOR EACH ROW EXECUTE FUNCTION guard_virtual_currency_child_mutation();

DROP TRIGGER IF EXISTS virtual_currency_posting_immutable ON virtual_currency_postings;
CREATE TRIGGER virtual_currency_posting_immutable
BEFORE INSERT OR UPDATE OR DELETE ON virtual_currency_postings
FOR EACH ROW EXECUTE FUNCTION guard_virtual_currency_child_mutation();

CREATE OR REPLACE FUNCTION assert_virtual_currency_journal_balanced(target_journal_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    posting_count BIGINT;
    posting_total NUMERIC;
    journal_posted_at TIMESTAMPTZ;
BEGIN
    SELECT posted_at
    INTO journal_posted_at
    FROM virtual_currency_journals
    WHERE id = target_journal_id;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT COUNT(*), COALESCE(SUM(amount_units), 0)
    INTO posting_count, posting_total
    FROM virtual_currency_postings
    WHERE journal_id = target_journal_id;

    IF journal_posted_at IS NULL OR posting_count < 2 OR posting_total <> 0 THEN
        RAISE EXCEPTION 'virtual currency journal % is unsealed or not balanced', target_journal_id
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_virtual_currency_journal_row()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM assert_virtual_currency_journal_balanced(NEW.id);
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION check_virtual_currency_posting_row()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        PERFORM assert_virtual_currency_journal_balanced(OLD.journal_id);
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') AND (TG_OP <> 'UPDATE' OR NEW.journal_id <> OLD.journal_id) THEN
        PERFORM assert_virtual_currency_journal_balanced(NEW.journal_id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS virtual_currency_journal_balance_check ON virtual_currency_journals;
CREATE CONSTRAINT TRIGGER virtual_currency_journal_balance_check
AFTER INSERT OR UPDATE ON virtual_currency_journals
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_virtual_currency_journal_row();

DROP TRIGGER IF EXISTS virtual_currency_posting_balance_check ON virtual_currency_postings;
CREATE CONSTRAINT TRIGGER virtual_currency_posting_balance_check
AFTER INSERT OR UPDATE OR DELETE ON virtual_currency_postings
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_virtual_currency_posting_row();
