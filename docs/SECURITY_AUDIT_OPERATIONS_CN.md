# 安全审计体系运维、发布与故障处置手册

> 适用版本：包含数据库迁移 `289`—`299` 的 S2AX
> 架构说明：见 `SECURITY_AUDIT_SYSTEM_V2_DESIGN_CN.md`
> 原则：先保留证据和可恢复性，再处理自动动作；任何紧急降级都必须留下操作审计与恢复条件。

## 1. 控制面边界

安全审计有两个互补控制面：

1. **Prompt Audit / 内容风控配置**控制请求热路径：启停、同步/异步、审核节点、超时、网络范围、失败模式、关键词、正则和兼容审核器。
2. **安全审计 V2 策略**控制治理：作用域、风险到动作映射、行为信号、证据保留、策略发布、案件、例外、反馈和通知。

修改 V2 策略不会替换 Prompt Audit 节点 Token、Base URL 或本地 66 条规则。判断一次变更是否会影响当前请求阻断时，必须同时检查这两个控制面。

## 2. 首次上线

### 2.1 前置条件

- PostgreSQL 与 Redis 时间同步；应用节点使用 UTC 存储时间。
- 所有应用实例使用同一个当前证据加密密钥和同一组有序历史解密密钥。
- 反向代理不会缓存 reveal 响应。
- 审核节点的网络范围按实际目的地选择：
  - 公网供应商只用 `public_https`；
  - 明确受信的内网服务用 `trusted_network`；
  - 本机审核服务用 `loopback`。
- 管理员已启用可用的 step-up 认证。
- 数据库备份包含迁移前快照，但不得导出解密后的完整提示词。

### 2.2 发布顺序

1. 部署包含迁移 `289`—`299` 的应用。
2. 确认迁移全部完成，再放入真实流量。
3. 在 Prompt Audit 页检查配置版本、运行模式、节点网络范围和探测结果。
   - 保存运行配置和执行端点探测均要求有效 step-up 会话；
4. 在安全审计工作台确认：
   - overview 可以读取；
   - endpoint health 可见；
   - action、case、signal、exception、notification 列表可读取；
   - signal watermark 没有持续错误。
5. 先使用 `async_audit` 或 V2 `shadow` 收集基线。
   - V2 shadow 由维护 Worker 持续评估新增统一判定；
   - shadow 结果只写 `security_audit_shadow_evaluations`，不会创建处置动作；
   - 工作台必须能看到 watermark、最近样本和变更比例。
6. 回放最近 24 小时样本，检查动作变化、阻断变化和误判样本。
7. 再启用 blocking 或带 pause 动作的 active 策略。

不得跳过 shadow/replay，直接把未经真实数据验证的新规则放入自动暂停路径。

## 3. 推荐基线

### 3.1 请求准入

- 本地规则错误：`allow_and_record`。
- 远程超时：`fallback_local`。
- 远程无效响应：对高风险入口使用 `block_and_record`，其余使用 `degraded_observe`。
- 远程总超时以业务 TTFT 预算为上限；审核不应无限等待。
- 自动暂停优先 API Key，再考虑用户；管理员账户受 Worker 强制保护。

### 3.2 V2 默认策略

- medium：`notify_admin`。
- high：`open_case`、`notify_admin`。
- critical：`open_case`、`notify_admin`。
- 只有在误判率和撤销路径验证后，才给高风险添加 `pause_api_key` 或 `pause_user`。
- 人工重试任何处置动作均要求 step-up，因为失败动作可能是用户/API Key 暂停；
- 不发布 `record_hash`、`skip_detector` 或任何未在服务端动作白名单中的值。

### 3.3 行为信号

阈值必须先基于至少 7 天真实流量计算。初始只通知和开案，不直接暂停：

- request burst：按 API Key 一分钟窗口；
- token/cost burst：按用户五分钟或一小时窗口；
- error ratio：设置 minimum samples，避免小样本放大；
- IP fanout：仅保存离散数量，不把原始 IP 复制进安全审计证据；
- maximum duration：用于发现异常上游和滥用，不等同于内容违规。

## 4. 日常巡检

### 4.1 每日

- degraded 比率、blocked 比率与前一周期差异；
- failed actions 和最老 pending action；
- open/reviewing case 数量与最长年龄；
- endpoint circuit 状态、连续失败和最后错误；
- signal lag 与 `last_error`；
- signal/shadow 单轮最多各消费 10,000 条；持续达到单轮上限表示吞吐已接近维护能力，应告警并进行容量基准；
- evidence reveal 数量；
- false positive / false negative 反馈；
- 用户与管理员未读安全通知。

### 4.2 每周

- 回放 active 策略与候选策略；
- 复核永久例外是否仍必要；
- 检查即将到期例外；
- 抽样确认脱敏预览没有 Token、Cookie、邮箱、手机号或长编码；
- 检查动作失败原因是否集中于主体不存在、管理员保护或权限问题；
- 检查审核模型输出协议是否发生漂移。

### 4.3 数据库只读健康查询

以下查询只返回聚合状态，不读取完整证据：

```sql
SELECT status, action_type, COUNT(*)
FROM security_audit_actions
GROUP BY status, action_type
ORDER BY status, action_type;
```

```sql
SELECT
    MAX(EXTRACT(EPOCH FROM (NOW() - created_at)))::bigint AS oldest_seconds,
    COUNT(*) AS pending
FROM security_audit_actions
WHERE status IN ('pending', 'processing', 'retry');
```

```sql
SELECT endpoint_id, breaker_state, consecutive_failures,
       error_code, checked_at
FROM security_audit_endpoint_health
ORDER BY endpoint_id;
```

```sql
SELECT policy_key, version, from_status, to_status, actor_id, reason, created_at
FROM security_audit_policy_transitions
ORDER BY created_at DESC, id DESC
LIMIT 100;
```

```sql
SELECT policy_key, policy_version,
       COUNT(*) AS samples,
       COUNT(*) FILTER (WHERE request_action_changed OR actions_changed) AS changed
FROM security_audit_shadow_evaluations
WHERE created_at >= NOW() - INTERVAL '24 hours'
GROUP BY policy_key, policy_version
ORDER BY policy_key, policy_version;
```

```sql
SELECT last_aggregated_at, last_evaluated_at,
       last_evaluated_window_id, last_error
FROM security_audit_signal_watermark
WHERE id = 1;
```

```sql
SELECT evidence_state, COUNT(*)
FROM prompt_audit_events
GROUP BY evidence_state
ORDER BY evidence_state;
```

禁止在常规巡检中查询密文列、旧 `full_prompt` 或 reveal 内容。

## 5. 告警建议

部署规模不同，阈值必须以基线调整；以下是触发条件而非固定数字：

| 告警 | 触发建议 | 首要动作 |
|---|---|---|
| degraded 突增 | 连续 5 分钟高于基线数倍 | 检查 endpoint、超时和 Redis/PostgreSQL |
| 阻断率突增 | 同流量周期显著偏离 | 暂停新策略、回放样本、检查模型协议 |
| action failed | 任意新增 critical pause 失败，或持续增长 | 查看 error_code，先修执行器再重试 |
| action backlog | oldest age 超过动作 SLO | 检查 Worker lease、数据库锁和实例日志 |
| endpoint all open | 所有可用节点熔断 | 切换失败模式或恢复至少一个节点 |
| signal lag | 超过两个维护周期并持续增长 | 检查 watermark 错误与聚合 SQL |
| evidence cleanup failed | 任意连续失败 | 检查加密器、数据库和保留任务 |
| reveal burst | 短时间显著增加 | 审查管理员访问日志和 step-up 会话 |
| false positive 增长 | 同规则持续命中 | shadow 修订策略，勿直接扩大永久例外 |

## 6. 故障处置

### 6.1 审核节点超时或全部熔断

1. 查看 endpoint health 的错误码、HTTP 状态和连续失败。
2. 检查 DNS、TLS、网络范围与目标地址是否变化。
3. 不要把公网节点临时改成 `trusted_network` 来绕过地址校验。
4. 按业务风险临时切换为 `fallback_local` 或 `degraded_observe`。
5. 节点恢复后主动 probe，再 reset breaker。
6. 回放故障窗口，确认遗漏风险并开案。

### 6.2 Redis 不可用

- blocking 检测不应依赖 Redis；
- async 载荷和入队不得伪装成功；
- 查看 admission 聚合和 failed job；
- Redis 恢复后只重试仍有有效载荷的任务；
- payload 已过期时保留失败记录，不构造虚假审核结论。

### 6.3 PostgreSQL 不可用

- 不执行任何用户/API Key 状态副作用；
- 请求级 block 可按已经得到的同步结果返回；
- 若部署要求“无审计不可阻断”，应返回 503；
- 数据库恢复后检查 action lease、outbox、admission counter 和事件缺口；
- 不从应用日志回填完整提示词。

### 6.4 动作积压或 Worker 重启

- Worker 使用 lease 与 fencing；等待陈旧租约被维护任务回收。
- action 和 outbox 必须成对转换；出现缺失 Outbox 或 fencing 行数为 0 时，事务会回滚并记录执行失败。
- 不直接把 `processing` 行改成 `succeeded`。
- failed 动作先确认错误原因；只有可重试原因才从管理端 retry。
- 对 pause 动作，先检查主体当前状态，避免覆盖管理员手工恢复。
- 撤销只对支持的成功动作执行，并使用 before state 条件恢复。

### 6.5 阻断率突增

1. 记录发生时间、active policy 版本、Prompt Audit 配置版本和受影响作用域。
2. 使用 replay 比较当前版本与上一个版本。
3. 若由 V2 动作变化导致，rollback 到已验证版本。
4. 若由审核模型/本地规则导致，在请求准入配置中切换 shadow/async 或关闭具体规则。
5. 对已执行暂停建立案件并逐个条件撤销，不能批量盲目启用所有主体。
6. 误判结论写 feedback，禁止只在外部文档记录。

### 6.6 `unsupported_action`

- 表示策略或历史数据包含没有真实执行器的动作。
- 不要反复 retry。
- 创建新策略版本移除该动作，validate、replay 后 activate。
- 迁移 `295` 会清理历史策略中的 `record_hash` 并将非终态动作标为失败。
- 迁移 `298` 会将所有没有真实执行器的历史非终态动作/Outbox 显式失败，并阻止新的未知动作写入；历史终态行保留用于审计。

### 6.7 证据疑似泄露

1. 立即限制管理端访问并审查 `security_audit_evidence_access_logs` 与兼容访问日志。
2. 保留数据库和应用访问审计，不导出更多证据。
3. 轮换证据加密主密钥时，先确认旧密钥仍可用于受控迁移；不可直接丢弃旧密钥导致证据永久不可读。
4. 缩短受影响数据的保留期并执行受控销毁。
5. 检查备份、日志平台、浏览器缓存和反向代理缓存。
6. reveal 响应必须继续保持 `Cache-Control: no-store`。

## 7. 案件与误判

- `open`：待处理；
- `reviewing`：已由管理员接手；
- `confirmed`：确认风险；
- `false_positive`：误判，应写 feedback，并按条件撤销动作；
- `dismissed`：证据不足或无需处理；
- `expired`：超过运营处理窗口。

误判处理顺序：

1. 打开决策安全摘要；
2. 必要时 step-up reveal，并填写具体理由；
3. 记录反馈与复核说明；
4. 检查关联动作 before/after；
5. 只撤销由该动作造成且当前状态仍等于动作 after state 的变更；
6. 需要临时放行时创建最小作用域、最短期限例外；
7. 用 replay 验证策略修订，避免长期堆叠例外。

## 8. 例外治理

- 优先级：API Key > 用户 > 分组 > 模型/端点 > 检测器 > 类别。
- 临时例外最长 30 天；更长必须显式选择永久并定期复核。
- `allow_and_record` 只取消后续动作，仍保留决策和证据。
- 人工使例外失效必须通过 step-up 并填写原因；数据库记录 `revoked_by`、`revoked_reason` 和 `expired_at`，不得直接无理由更新状态。
- `warn_only` 只允许通知和开案类动作。
- detector/category 例外只有在本次证据确实命中对应 detector/category 时才生效。
- 例外不能绕过请求热路径已经执行的检测器。

## 9. 证据保留和密钥

- 保存模式从低到高：`none`、`digest_only`、`findings_encrypted`、`full_encrypted`。
- 默认使用 `findings_encrypted`；只有确有复核需要才用 `full_encrypted`。
- 密文与访问日志分别设置保留期。
- 到期任务销毁密文后保留最小摘要和销毁状态，便于证明生命周期已执行。
- 备份应加密并限制访问；恢复演练必须验证密钥可用性。

### 9.1 密钥轮换

共享 `SecretEncryptor` 同时保护 TOTP、审核证据和项目内其他受保护配置，因此轮换必须作为全站变更执行：

1. 生成新的 32 字节十六进制密钥，不在工单、日志或聊天中粘贴明文。
2. 将现用 `TOTP_ENCRYPTION_KEY` 移入 `TOTP_PREVIOUS_ENCRYPTION_KEYS` 首位；历史列表以英文逗号分隔，最多 4 把且不得与新密钥重复。
3. 将新密钥写入 `TOTP_ENCRYPTION_KEY`，确保所有实例使用完全相同的当前/历史配置。
4. 先滚动一个实例。启动过程只有在数据库持久化密钥等于某把显式历史密钥时才会条件切换到新密钥；未知错配会保留旧密钥并关闭“已手动配置”安全门，不会静默覆盖。
5. 验证旧审核证据、已有 TOTP/受保护配置可读，并验证新写密文不能只用旧主密钥解密。
6. 完成全部实例滚动并观察解密错误、启动错误和 reveal 失败率。
7. 当前项目没有覆盖所有共享密文字段的统一后台重加密器。只有确认旧密钥对应数据已被业务重写、受控迁移或超过保留期后，才可从历史列表删除该密钥。

紧急回滚时，将旧密钥恢复为当前密钥、把新密钥放入历史列表，再滚动全部实例。禁止直接清空历史列表。

## 10. 发布与回滚验收

每次涉及检测、策略、动作或迁移的发布至少完成：

- Go 安全审计、内容风控、仓储、路由和 migration 定向测试；
- 前端 typecheck、工作台/API/store 组件测试；
- 前端生产构建；
- `git diff --check`；
- 迁移在测试 PostgreSQL 上重复执行；
- shadow/replay 对比；
- endpoint probe 与 breaker reset；
- pause/revert、notification ownership、evidence reveal、exception expiry 的真实环境冒烟；
- Worker 重启与 lease reclaim 演练；
- live shadow watermark 追赶、无处置副作用和多实例 advisory lock 演练；
- Redis 短暂不可用和审核节点超时演练。

高影响管理操作统一要求 step-up：运行配置保存、端点探测、策略激活/回滚、证据 reveal、审计事件单条/批量/筛选物理删除、例外创建/人工失效、动作重试/取消/撤销、案件状态转换以及端点熔断器重置。前端通过同一 step-up 控制器重试原请求，后端路由仍是最终强制边界。

回滚应用版本时不要回滚数据库迁移；新表保持 additive。策略通过历史版本 rollback，动作通过显式 revert。任何已经对用户/API Key 生效的状态变化都不能靠部署回滚隐式恢复。

## 11. 容量基准

至少记录以下基线：

- 本地检测 P50/P95/P99；
- 远程审核 P50/P95/P99 与超时率；
- decision/evidence 写入吞吐；
- action 每秒处理量和积压恢复速度；
- signal 每分钟聚合耗时与追赶速度；
- PostgreSQL 表增长量；
- Redis payload 数、平均大小和 TTL；
- 管理端 24 小时/7 天/30 天查询耗时。

当动作积压、信号追赶或查询时延接近业务 SLO 时，优先优化索引、批大小和 Worker 并发；只有确认单进程架构到达真实瓶颈后才考虑拆服务或外部消息队列。
