# Notes: 安全审计体系实施记录

## 实施总览

- 迁移 `289`：提示词证据加密、状态、到期和独立访问日志。
- 迁移 `290`：稳定失败模式与异步 admission 聚合。
- 迁移 `291`：policy、decision、evidence、action、outbox、case、exception、feedback、endpoint health 和访问审计。
- 迁移 `292`：行为信号窗口、评估、watermark 和站内通知。
- 迁移 `293`：端点运行时熔断状态。
- 迁移 `294`：移除语义不真实的 `skip_detector`。
- 迁移 `295`：移除没有真实热路径执行器的 `record_hash` 策略动作，并显式失败历史非终态动作。
- 迁移 `296`：持久化策略状态转换的版本、actor、理由和时间线。
- 迁移 `297`：持久化实时 shadow 比较结果与多实例安全 watermark。
- 迁移 `298`：收紧 action 数据库白名单，显式失败全部无执行器的历史非终态动作/Outbox。
- 迁移 `299`：为安全例外人工失效增加操作者、必填理由和数据库约束。

## 已闭环能力

- 新提示词、旧内容风控和 `cyber_policy` 都写统一 decision/evidence。
- 新写路径不保存明文 prompt；reveal 需要 step-up、理由、no-store 和访问审计。
- 审核节点执行严格出站地址分类、拨号后二次校验和重定向限制。
- 失败模式、同步降级事件、异步 admission 丢弃、endpoint breaker 均可查询。
- 生产旧内容风控在同一事务写 legacy log、统一 decision/evidence、action/outbox；请求线程不再直接禁用用户。
- 动作 Worker 支持 claim fencing、lease、retry、failed、reclaim、幂等、管理员保护和条件撤销。
- 策略支持作用域、不可变版本、validate、simulate、replay、shadow、activate 和 rollback。
- shadow 状态会持续消费新增统一 decision，比较准入预测和候选动作；结果可查询且绝不发布处置动作。
- 策略转换历史与策略版本同事务落库；shadow/activate/rollback 强制填写理由。
- 案件、时间线、反馈、误判、例外和动作撤销可闭环。
- 行为信号从 usage/ops 数据在 PostgreSQL 聚合，不依赖前端分页。
- 用户安全通知按 `recipient_user_id` 强制隔离，DTO 不暴露内部 action/decision 标识。
- 管理员通知状态接口强制限定 `audience=admin`，不能代替用户读取或忽略其安全通知。
- Prompt Audit 运行配置保存、端点探测、处置动作重试/取消/撤销、案件状态转换和端点熔断器重置均纳入 step-up，覆盖凭据出站、私网访问和主体状态变更风险。
- 安全例外创建与人工失效均要求 step-up；失效理由和操作者持久化，避免无归属的策略边界变更。
- 审计事件的单条、批量和筛选物理删除均强制 step-up；筛选删除令牌在 step-up 重试回调内生成，避免二次认证等待造成陈旧令牌。
- signal/shadow 维护可在单个调度周期内有界排空多个满批次，并按实际推进游标数量判断积压，避免低命中流量永久滞后。
- 管理工作台覆盖概览、决策、案件、策略、动作、信号、例外、端点和通知。
- 管理工作台展示策略状态时间线、历史回放和实时 shadow 样本/变化指标。
- 动作与 Outbox 的人工重试、取消和 Worker 状态转换保持事务原子性，并对租约丢失/缺失 Outbox 失败关闭。
- `off`/`async_audit` 策略在统一 decision、行为信号、模拟、回放和 shadow 中统一禁止生成 action/outbox，消除“只审计”模式误处置主体的边界歧义。
- 共享 AES-GCM 加密器支持 1 把当前写密钥和最多 4 把只读历史密钥；持久化主密钥只有在旧值被显式列为历史密钥时才允许条件轮换。
- 真实 PostgreSQL 回归修复了端点健康 UPSERT 中同一参数被同时推断为 `INTEGER`/`BIGINT` 的错误；显式类型后 breaker 的 closed/open/half-open/恢复生命周期可用。

## 架构边界

- Prompt Audit/内容风控配置仍是检测与请求准入运行时。
- V2 policy 当前是治理与动作策略；其 detector/failure 字段不替代热路径节点配置。
- 旧表/API 为兼容层，V2 表是治理真相源。
- 旧邮件是补充 best-effort 通道，统一站内通知与用户状态由持久化动作提供。

## 验证记录

- 后端安全审计、内容风控、仓储与 migration 定向测试通过。
- PostgreSQL+Redis 安全审计集成套件通过；迁移可重复执行，行为信号、shadow、breaker、动作/Outbox、例外撤销和证据保护均在真实数据库方言上验证。
- 前端 8 个安全审计测试文件共 36 个测试、typecheck 和生产构建通过。
- 新增回归覆盖：通知所有权隔离、例外 detector/category 匹配、统一处置不直接改用户、无执行器动作拒绝、迁移语义。
- 扩大范围后端测试发现的唯一失败为既有城市模拟 UTC 窗口断言：`TestCityRealtimeAgentDecisionNextBudgetWindowUsesNextUTCHour` 期望 04:00:01、实际 03:00:01；未修改该非安全审计模块。
- 后端全包编译、相关包 `go vet` 和 diff 检查通过；diff 检查仅报告工作区既有的 LF/CRLF 转换提示。
- 真实部署故障演练和容量基准不能由本地单测替代，按 `docs/SECURITY_AUDIT_OPERATIONS_CN.md` 执行。
