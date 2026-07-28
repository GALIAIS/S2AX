-- Administrators can explicitly expose cached per-account usage windows in the
-- read-only user account directory. A grant never changes group access,
-- subscription checks, account scheduling, or API-key routing.

CREATE TABLE IF NOT EXISTS account_usage_visibility_grants (
    id          BIGSERIAL PRIMARY KEY,
    grant_scope VARCHAR(32) NOT NULL,
    group_id    BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id     BIGINT REFERENCES users(id) ON DELETE CASCADE,
    account_id  BIGINT REFERENCES accounts(id) ON DELETE CASCADE,
    created_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_account_usage_visibility_grant_scope
        CHECK (grant_scope IN ('exclusive_group', 'user_account')),
    CONSTRAINT chk_account_usage_visibility_grant_shape
        CHECK (
            (
                grant_scope = 'exclusive_group'
                AND user_id IS NULL
                AND account_id IS NULL
            )
            OR (
                grant_scope = 'user_account'
                AND user_id IS NOT NULL
                AND account_id IS NOT NULL
            )
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_usage_visibility_exclusive_group
    ON account_usage_visibility_grants(group_id)
    WHERE grant_scope = 'exclusive_group';

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_usage_visibility_user_account
    ON account_usage_visibility_grants(user_id, group_id, account_id)
    WHERE grant_scope = 'user_account';

COMMENT ON TABLE account_usage_visibility_grants IS
    'Admin-managed grants for cached account quota details in the read-only user account directory.';
COMMENT ON COLUMN account_usage_visibility_grants.grant_scope IS
    'exclusive_group applies dynamically to eligible users of one exclusive group; user_account applies to one user/account/group tuple.';
