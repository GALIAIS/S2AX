-- The core entities in this project use Ent soft deletion. A database DELETE
-- trigger alone therefore cannot release an allocation when an administrator
-- removes an account, user, or group through the normal control plane.
--
-- Keep the historical lease, but never leave it active after its owner or
-- scope has been soft deleted. This is intentionally database-enforced so
-- every deletion entrypoint (including future services) has the same result.

ALTER TABLE account_allocation_assignments
    DROP CONSTRAINT IF EXISTS account_allocation_assignment_release_shape;

ALTER TABLE account_allocation_assignments
    ADD CONSTRAINT account_allocation_assignment_release_shape CHECK (
        (status = 'active'
            AND account_id IS NOT NULL
            AND released_at IS NULL
            AND release_reason IS NULL)
        OR (status = 'released'
            AND released_at IS NOT NULL
            AND release_reason IS NOT NULL)
    );

CREATE OR REPLACE FUNCTION release_account_allocation_leases_after_account_soft_delete()
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
        WHERE account_id = NEW.id
          AND status = 'active'
        RETURNING id, policy_id
    )
    INSERT INTO account_allocation_events (policy_id, assignment_id, event_type, metadata)
    SELECT policy_id,
           id,
           'assignment_released',
           jsonb_build_object('reason', 'account_removed', 'source', 'account_soft_delete')
    FROM released;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS account_allocation_release_after_account_soft_delete ON accounts;
CREATE TRIGGER account_allocation_release_after_account_soft_delete
    AFTER UPDATE OF deleted_at ON accounts
    FOR EACH ROW
    WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION release_account_allocation_leases_after_account_soft_delete();

CREATE OR REPLACE FUNCTION disable_account_allocation_policies_after_user_soft_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    WITH disabled AS (
        UPDATE account_allocation_policies
        SET status = 'disabled',
            last_reconciled_at = NOW(),
            updated_at = NOW()
        WHERE user_id = NEW.id
          AND status = 'active'
        RETURNING id
    ),
    released AS (
        UPDATE account_allocation_assignments
        SET status = 'released',
            released_at = NOW(),
            release_reason = 'user_removed',
            last_reconciled_at = NOW(),
            updated_at = NOW()
        WHERE user_id = NEW.id
          AND status = 'active'
        RETURNING id, policy_id
    )
    INSERT INTO account_allocation_events (policy_id, assignment_id, event_type, metadata)
    SELECT id,
           NULL::BIGINT,
           'policy_disabled',
           jsonb_build_object('reason', 'user_removed', 'source', 'user_soft_delete')
    FROM disabled
    UNION ALL
    SELECT policy_id,
           id,
           'assignment_released',
           jsonb_build_object('reason', 'user_removed', 'source', 'user_soft_delete')
    FROM released;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS account_allocation_disable_after_user_soft_delete ON users;
CREATE TRIGGER account_allocation_disable_after_user_soft_delete
    AFTER UPDATE OF deleted_at ON users
    FOR EACH ROW
    WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION disable_account_allocation_policies_after_user_soft_delete();

CREATE OR REPLACE FUNCTION disable_account_allocation_policies_after_group_soft_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    WITH disabled AS (
        UPDATE account_allocation_policies
        SET status = 'disabled',
            last_reconciled_at = NOW(),
            updated_at = NOW()
        WHERE group_id = NEW.id
          AND status = 'active'
        RETURNING id
    ),
    released AS (
        UPDATE account_allocation_assignments
        SET status = 'released',
            released_at = NOW(),
            release_reason = 'group_removed',
            last_reconciled_at = NOW(),
            updated_at = NOW()
        WHERE group_id = NEW.id
          AND status = 'active'
        RETURNING id, policy_id
    )
    INSERT INTO account_allocation_events (policy_id, assignment_id, event_type, metadata)
    SELECT id,
           NULL::BIGINT,
           'policy_disabled',
           jsonb_build_object('reason', 'group_removed', 'source', 'group_soft_delete')
    FROM disabled
    UNION ALL
    SELECT policy_id,
           id,
           'assignment_released',
           jsonb_build_object('reason', 'group_removed', 'source', 'group_soft_delete')
    FROM released;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS account_allocation_disable_after_group_soft_delete ON groups;
CREATE TRIGGER account_allocation_disable_after_group_soft_delete
    AFTER UPDATE OF deleted_at ON groups
    FOR EACH ROW
    WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION disable_account_allocation_policies_after_group_soft_delete();
