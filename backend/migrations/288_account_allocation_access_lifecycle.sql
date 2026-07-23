-- Account allocations are exclusive leases. Release them at the database
-- boundary as soon as the target loses group access; the periodic reconciler
-- remains the fallback for time-based subscription expiry and legacy writes.

CREATE OR REPLACE FUNCTION release_account_allocations_after_allowed_group_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    WITH affected_policies AS (
        SELECT p.id
        FROM account_allocation_policies p
        JOIN groups g ON g.id = p.group_id
        WHERE p.user_id = OLD.user_id
          AND p.group_id = OLD.group_id
          AND p.status = 'active'
          AND p.deleted_at IS NULL
          AND g.is_exclusive = TRUE
          AND COALESCE(g.subscription_type, 'standard') <> 'subscription'
    ),
    released AS (
        UPDATE account_allocation_assignments aa
        SET status = 'released',
            released_at = NOW(),
            release_reason = 'target_group_access_unavailable',
            last_reconciled_at = NOW(),
            updated_at = NOW()
        FROM affected_policies p
        WHERE aa.policy_id = p.id
          AND aa.status = 'active'
        RETURNING aa.id, aa.policy_id
    )
    INSERT INTO account_allocation_events (policy_id, assignment_id, event_type, metadata)
    SELECT policy_id,
           id,
           'assignment_released',
           jsonb_build_object(
               'reason', 'target_group_access_unavailable',
               'access_status', 'group_access_required',
               'source', 'user_allowed_group_delete'
           )
    FROM released;

    RETURN OLD;
END;
$$;

DROP TRIGGER IF EXISTS account_allocation_release_after_allowed_group_delete ON user_allowed_groups;
CREATE TRIGGER account_allocation_release_after_allowed_group_delete
    AFTER DELETE ON user_allowed_groups
    FOR EACH ROW
    EXECUTE FUNCTION release_account_allocations_after_allowed_group_delete();

CREATE OR REPLACE FUNCTION release_account_allocations_after_subscription_access_loss()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_user_id BIGINT;
    target_group_id BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        target_user_id := OLD.user_id;
        target_group_id := OLD.group_id;
    ELSE
        target_user_id := NEW.user_id;
        target_group_id := NEW.group_id;
    END IF;

    -- A replacement/current subscription can keep the same user/group usable.
    IF EXISTS (
        SELECT 1
        FROM user_subscriptions us
        WHERE us.user_id = target_user_id
          AND us.group_id = target_group_id
          AND us.status = 'active'
          AND us.deleted_at IS NULL
          AND us.expires_at > NOW()
    ) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;

    WITH affected_policies AS (
        SELECT p.id
        FROM account_allocation_policies p
        JOIN groups g ON g.id = p.group_id
        WHERE p.user_id = target_user_id
          AND p.group_id = target_group_id
          AND p.status = 'active'
          AND p.deleted_at IS NULL
          AND COALESCE(g.subscription_type, 'standard') = 'subscription'
    ),
    released AS (
        UPDATE account_allocation_assignments aa
        SET status = 'released',
            released_at = NOW(),
            release_reason = 'target_group_access_unavailable',
            last_reconciled_at = NOW(),
            updated_at = NOW()
        FROM affected_policies p
        WHERE aa.policy_id = p.id
          AND aa.status = 'active'
        RETURNING aa.id, aa.policy_id
    )
    INSERT INTO account_allocation_events (policy_id, assignment_id, event_type, metadata)
    SELECT policy_id,
           id,
           'assignment_released',
           jsonb_build_object(
               'reason', 'target_group_access_unavailable',
               'access_status', 'subscription_required',
               'source', 'user_subscription_access_loss'
           )
    FROM released;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS account_allocation_release_after_subscription_update ON user_subscriptions;
CREATE TRIGGER account_allocation_release_after_subscription_update
    AFTER UPDATE OF status, expires_at, deleted_at ON user_subscriptions
    FOR EACH ROW
    EXECUTE FUNCTION release_account_allocations_after_subscription_access_loss();

DROP TRIGGER IF EXISTS account_allocation_release_after_subscription_delete ON user_subscriptions;
CREATE TRIGGER account_allocation_release_after_subscription_delete
    AFTER DELETE ON user_subscriptions
    FOR EACH ROW
    EXECUTE FUNCTION release_account_allocations_after_subscription_access_loss();

CREATE OR REPLACE FUNCTION release_account_allocations_after_user_status_loss()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status = 'active' OR NEW.status IS NOT DISTINCT FROM OLD.status THEN
        RETURN NEW;
    END IF;

    WITH released AS (
        UPDATE account_allocation_assignments aa
        SET status = 'released',
            released_at = NOW(),
            release_reason = 'target_user_unavailable',
            last_reconciled_at = NOW(),
            updated_at = NOW()
        FROM account_allocation_policies p
        WHERE p.id = aa.policy_id
          AND p.user_id = NEW.id
          AND p.status = 'active'
          AND p.deleted_at IS NULL
          AND aa.status = 'active'
        RETURNING aa.id, aa.policy_id
    )
    INSERT INTO account_allocation_events (policy_id, assignment_id, event_type, metadata)
    SELECT policy_id,
           id,
           'assignment_released',
           jsonb_build_object(
               'reason', 'target_user_unavailable',
               'access_status', 'user_unavailable',
               'source', 'user_status_update'
           )
    FROM released;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS account_allocation_release_after_user_status_loss ON users;
CREATE TRIGGER account_allocation_release_after_user_status_loss
    AFTER UPDATE OF status ON users
    FOR EACH ROW
    EXECUTE FUNCTION release_account_allocations_after_user_status_loss();

CREATE OR REPLACE FUNCTION release_account_allocations_after_group_status_loss()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status = 'active' OR NEW.status IS NOT DISTINCT FROM OLD.status THEN
        RETURN NEW;
    END IF;

    WITH released AS (
        UPDATE account_allocation_assignments aa
        SET status = 'released',
            released_at = NOW(),
            release_reason = 'target_group_unavailable',
            last_reconciled_at = NOW(),
            updated_at = NOW()
        FROM account_allocation_policies p
        WHERE p.id = aa.policy_id
          AND p.group_id = NEW.id
          AND p.status = 'active'
          AND p.deleted_at IS NULL
          AND aa.status = 'active'
        RETURNING aa.id, aa.policy_id
    )
    INSERT INTO account_allocation_events (policy_id, assignment_id, event_type, metadata)
    SELECT policy_id,
           id,
           'assignment_released',
           jsonb_build_object(
               'reason', 'target_group_unavailable',
               'access_status', 'group_unavailable',
               'source', 'group_status_update'
           )
    FROM released;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS account_allocation_release_after_group_status_loss ON groups;
CREATE TRIGGER account_allocation_release_after_group_status_loss
    AFTER UPDATE OF status ON groups
    FOR EACH ROW
    EXECUTE FUNCTION release_account_allocations_after_group_status_loss();
