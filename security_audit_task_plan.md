# Task Plan: 安全审计体系成熟化

## Goal

在保持现有网关兼容的前提下，将旧内容风控与新提示词审计收敛为可配置、可靠、可追踪、可复核、可恢复且可持续评估的安全审计体系。

## Phases

- [x] Phase 1: 审查请求接入、策略、检测、存储、处置、前端和测试
- [x] Phase 2: 完成目标架构、数据契约、迁移、权限、运维和验收设计
- [x] Phase 3: 修复证据保护、出站边界、降级语义和旧副作用顺序
- [x] Phase 4: 建立版本化策略、统一决策、动作 Outbox 和兼容适配
- [x] Phase 5: 建立案件、例外、反馈、条件撤销和操作审计闭环
- [x] Phase 6: 接入行为、用量、费用、上游安全信号和分级处置
- [x] Phase 7: 完成管理工作台、通知、质量指标、端点熔断和回放评估
- [x] Phase 8: 完成迁移 `289`—`299`、定向单元/集成测试和运维手册
- [x] Phase 9: 完成策略状态历史、真实影子评估、动作/Outbox 原子状态机与租约完整性加固
- [ ] Phase 10: 在目标部署执行 PostgreSQL/Redis/反代故障演练和容量基准

## Final Decisions

- `securityaudit.Coordinator` 仍是唯一请求入口。
- Prompt Audit/内容风控配置是检测与请求准入面；V2 policy 是治理与动作面。当前不声称 V2 detectors 字段已经替代热路径节点配置。
- V2 policy 的 `off`/`async_audit` 是无副作用观察模式；只有 `blocking` 可生成 action/outbox。
- PostgreSQL 是 decision/evidence/action/case/feedback/exception/notification 真相源；Redis 只承担短期载荷和缓存。
- 用户/API Key 状态变化只由持久化动作 Worker 执行；请求线程不得先改状态后补日志。
- 完整证据默认不可见，reveal 要求 step-up、理由、no-store 和访问审计。
- 例外只能调整证据产生后的处置；`skip_detector` 已移除。
- 没有真实执行器的 `record_hash` 已从可发布动作移除，历史非终态任务由迁移 `295` 显式失败。
- 数据库动作类型由迁移 `298` 收紧到五类真实执行器，杜绝应用校验旁路写入虚假处置。
- 审核证据复用的共享 AES-GCM 加密器采用有界轮换密钥环；当前密钥唯一负责新写，最多 4 把历史密钥只负责旧读，数据库主密钥轮换必须显式声明旧值。
- shadow 版本由维护 Worker 对新增统一 decision 持续评估，结果只写比较表，不创建 action/outbox。
- 动作与 Outbox 的 retry/cancel/claim/succeed/fail 必须在同一事务中成对转换，并校验 fencing 更新行数。
- 单管理员部署保留强审计和 step-up，不增加双人审批、微服务或外部消息队列。

## Verification Status

- 安全审计、内容风控、仓储、路由与 migration 定向测试：通过。
- 前端安全通知、工作台/API/store 定向测试：通过。
- 前端 typecheck：通过。
- 前端生产构建、安全审计 PostgreSQL+Redis 集成套件和相关后端范围测试：通过。
- 真实 PostgreSQL 已覆盖行为信号聚合/评估/幂等、shadow 无副作用、端点 breaker 全生命周期、action/outbox 执行与撤销、例外失效归属约束。
- 扩大范围后端测试只保留已登记的非安全审计失败：`TestCityRealtimeAgentDecisionNextBudgetWindowUsesNextUTCHour` 期望 04:00:01、实际 03:00:01。
- 后端全包编译、相关包 `go vet` 和 `git diff --check`：通过；仅有工作区既有行尾转换提示。
- 真实环境故障演练与容量基准：必须在目标部署执行，不能由本地单元测试替代。
