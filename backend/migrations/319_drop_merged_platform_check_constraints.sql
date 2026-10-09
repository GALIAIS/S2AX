-- 316 和 318 曾为本地 Devin 与上游平台重建白名单；统一平台目录后由应用层校验。
-- 保留历史迁移的校验值，在现有数据库和新安装上统一删除这两个约束。
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
