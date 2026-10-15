-- 平台退役只停用配置，保留账号凭证、订阅、历史用量与账单，便于审计和恢复。
-- 与停用同事务发布调度事件，分组状态变更同时触发现有鉴权缓存失效 outbox。
WITH retired_accounts AS (
    UPDATE accounts
       SET status = 'disabled',
           schedulable = FALSE,
           updated_at = NOW()
     WHERE platform = 'devin'
       AND deleted_at IS NULL
       AND (status <> 'disabled' OR schedulable)
    RETURNING id
)
INSERT INTO scheduler_outbox (event_type, account_id)
SELECT 'account_changed', id FROM retired_accounts;

WITH retired_groups AS (
    UPDATE groups
       SET status = 'disabled',
           updated_at = NOW()
     WHERE platform = 'devin'
       AND deleted_at IS NULL
       AND status <> 'disabled'
    RETURNING id
)
INSERT INTO scheduler_outbox (event_type, group_id)
SELECT 'group_changed', id FROM retired_groups;

WITH retired_routes AS (
    UPDATE composite_model_routes
       SET enabled = FALSE,
           updated_at = NOW()
     WHERE target_platform = 'devin'
       AND deleted_at IS NULL
       AND enabled
    RETURNING group_id
)
INSERT INTO scheduler_outbox (event_type, group_id)
SELECT DISTINCT 'group_changed', group_id FROM retired_routes;

UPDATE channel_monitors
   SET enabled = FALSE,
       updated_at = NOW()
 WHERE provider = 'devin'
   AND enabled;
