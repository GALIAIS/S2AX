-- An assignment stores user_id/group_id for fast scheduler lookup. Bind those
-- denormalized columns to the owning policy at the database boundary so a
-- future write path cannot accidentally create a lease for another user or
-- group while still referencing a valid policy ID.
ALTER TABLE account_allocation_policies
    ADD CONSTRAINT account_allocation_policies_id_user_group_unique
    UNIQUE (id, user_id, group_id);

ALTER TABLE account_allocation_assignments
    ADD CONSTRAINT account_allocation_assignments_policy_scope_fkey
    FOREIGN KEY (policy_id, user_id, group_id)
    REFERENCES account_allocation_policies (id, user_id, group_id)
    ON DELETE RESTRICT;
