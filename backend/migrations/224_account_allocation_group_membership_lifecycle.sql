-- Removing an account from a group must take effect immediately. Waiting for
-- the periodic reconciler leaves a manual-only lease usable through a sticky
-- session after the account is no longer eligible for that group.
CREATE OR REPLACE FUNCTION release_account_allocation_leases_after_account_group_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    WITH released AS (
        UPDATE account_allocation_assignments
        SET status = 'released',
            released_at = NOW(),
            release_reason = 'account_group_unbound',
            last_reconciled_at = NOW(),
            updated_at = NOW()
        WHERE account_id = OLD.account_id
          AND group_id = OLD.group_id
          AND status = 'active'
        RETURNING id, policy_id
    )
    INSERT INTO account_allocation_events (policy_id, assignment_id, event_type, metadata)
    SELECT policy_id,
           id,
           'assignment_released',
           jsonb_build_object('reason', 'account_group_unbound', 'source', 'account_group_delete')
    FROM released;

    RETURN OLD;
END;
$$;

DROP TRIGGER IF EXISTS account_allocation_release_after_account_group_delete ON account_groups;
CREATE TRIGGER account_allocation_release_after_account_group_delete
    AFTER DELETE ON account_groups
    FOR EACH ROW
    EXECUTE FUNCTION release_account_allocation_leases_after_account_group_delete();
