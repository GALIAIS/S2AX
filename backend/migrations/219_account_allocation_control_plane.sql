-- Per-user account allocation control plane.
-- Policies declare desired capacity; active assignments are exclusive leases.

CREATE TABLE IF NOT EXISTS account_allocation_policies (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    desired_count INTEGER NOT NULL DEFAULT 0 CHECK (desired_count >= 0 AND desired_count <= 1000),
    auto_replenish BOOLEAN NOT NULL DEFAULT FALSE,
    replace_on_401 BOOLEAN NOT NULL DEFAULT TRUE,
    replace_on_429 BOOLEAN NOT NULL DEFAULT TRUE,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    last_reconciled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_allocation_policies_user_group_unique UNIQUE (user_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_account_allocation_policies_active
    ON account_allocation_policies (status, group_id, user_id, id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS account_allocation_assignments (
    id BIGSERIAL PRIMARY KEY,
    policy_id BIGINT NOT NULL REFERENCES account_allocation_policies(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    -- Keep the lease history intact. Account deletion must first be an
    -- explicit release/soft-delete operation instead of silently erasing the
    -- user allocation and its audit trail.
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'released')),
    assigned_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at TIMESTAMPTZ,
    release_reason VARCHAR(64),
    last_reconciled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_allocation_assignment_release_shape CHECK (
        (status = 'active' AND released_at IS NULL AND release_reason IS NULL)
        OR (status = 'released' AND released_at IS NOT NULL AND release_reason IS NOT NULL)
    )
);

-- An upstream account can never be actively allocated to two users, including
-- through different groups. Historical released rows are retained for audit.
CREATE UNIQUE INDEX IF NOT EXISTS account_allocation_assignments_one_active_account
    ON account_allocation_assignments (account_id)
    WHERE status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS account_allocation_assignments_one_active_policy_account
    ON account_allocation_assignments (policy_id, account_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_account_allocation_assignments_policy_active
    ON account_allocation_assignments (policy_id, status, assigned_at DESC, id);

CREATE INDEX IF NOT EXISTS idx_account_allocation_assignments_user_group_active
    ON account_allocation_assignments (user_id, group_id, status, account_id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS account_allocation_events (
    id BIGSERIAL PRIMARY KEY,
    policy_id BIGINT NOT NULL REFERENCES account_allocation_policies(id) ON DELETE RESTRICT,
    assignment_id BIGINT REFERENCES account_allocation_assignments(id) ON DELETE SET NULL,
    event_type VARCHAR(64) NOT NULL,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_allocation_events_metadata_object CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_account_allocation_events_policy_created
    ON account_allocation_events (policy_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_account_allocation_events_assignment_created
    ON account_allocation_events (assignment_id, created_at DESC, id DESC)
    WHERE assignment_id IS NOT NULL;
