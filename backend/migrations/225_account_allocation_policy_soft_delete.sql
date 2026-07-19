-- Policies are control-plane configuration, not audit facts. Keep their
-- assignments and events for audit, while allowing an administrator to remove
-- a policy and later create a replacement for the same user/group pair.

ALTER TABLE account_allocation_policies
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE account_allocation_policies
    DROP CONSTRAINT IF EXISTS account_allocation_policies_user_group_unique;

CREATE UNIQUE INDEX IF NOT EXISTS account_allocation_policies_user_group_live_unique
    ON account_allocation_policies (user_id, group_id)
    WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS idx_account_allocation_policies_active;

CREATE INDEX IF NOT EXISTS idx_account_allocation_policies_active
    ON account_allocation_policies (status, group_id, user_id, id)
    WHERE status = 'active' AND deleted_at IS NULL;
