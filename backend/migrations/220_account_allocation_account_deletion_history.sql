-- Keep allocation history and do not let an active lease block normal account
-- deletion. The trigger changes the live lease into an auditable release before
-- the foreign key clears the historical account reference.

ALTER TABLE account_allocation_assignments
    ALTER COLUMN account_id DROP NOT NULL;

DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    -- Replace every FK from the allocation table to accounts, including the
    -- default name emitted by migration 219 and any deployment-local rename.
    FOR constraint_name IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'account_allocation_assignments'::regclass
          AND contype = 'f'
          AND confrelid = 'accounts'::regclass
    LOOP
        EXECUTE format('ALTER TABLE account_allocation_assignments DROP CONSTRAINT %I', constraint_name);
    END LOOP;

    ALTER TABLE account_allocation_assignments
        ADD CONSTRAINT account_allocation_assignments_account_id_fkey
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE SET NULL;
END $$;

CREATE OR REPLACE FUNCTION release_account_allocation_leases_before_account_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    WITH released AS (
        UPDATE account_allocation_assignments
        SET status = 'released',
            released_at = NOW(),
            release_reason = 'account_removed',
            last_reconciled_at = NOW(),
            updated_at = NOW()
        WHERE account_id = OLD.id
          AND status = 'active'
        RETURNING id, policy_id
    )
    INSERT INTO account_allocation_events (policy_id, assignment_id, event_type, metadata)
    SELECT policy_id,
           id,
           'assignment_released',
           jsonb_build_object('reason', 'account_removed', 'source', 'account_delete')
    FROM released;

    RETURN OLD;
END;
$$;

DROP TRIGGER IF EXISTS account_allocation_release_before_account_delete ON accounts;
CREATE TRIGGER account_allocation_release_before_account_delete
    BEFORE DELETE ON accounts
    FOR EACH ROW
    EXECUTE FUNCTION release_account_allocation_leases_before_account_delete();
