-- 为共享池成员增加可选的独立基础金额；为空时继续使用原有权重分配。
ALTER TABLE shared_quota_pool_members
    ADD COLUMN IF NOT EXISTS quota_usd DECIMAL(20,10);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'shared_quota_pool_members_quota_usd_check'
    ) THEN
        ALTER TABLE shared_quota_pool_members
            ADD CONSTRAINT shared_quota_pool_members_quota_usd_check
            CHECK (quota_usd IS NULL OR quota_usd > 0);
    END IF;
END $$;
