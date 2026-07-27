# 安全审计体系 V2 设计与实施文档

> 状态：核心闭环已实现；本文同时作为架构基线、实施清单和运维验收依据
> 审查基线：`cbaea943e8ac34e4991d5bbe54c795a3afe7e328`
> 审查日期：2026-07-24
> 实施迁移：`289`—`299`
> 适用对象：单管理员运营、向其他用户提供 AI 网关服务的 S2AX 部署
> 实施约束：不拆微服务、不引入消息队列、不改变现有网关协议、不把审核模型当作最终处置者

## 1. 结论

审查开始时，项目存在两套能力较强但尚未完全收敛的安全链路：

1. 旧内容风控 `ContentModerationService`：负责关键词、哈希、66 条 RE2 规则、外部 Moderations/Chat 审核、同步阻断、自动封禁、邮件与 `cyber_policy`。
2. 新提示词审计 `internal/securityaudit`：负责统一网关接入、提示词规范化、可靠任务、Redis 短期载荷、节点池、Qwen3Guard、同步 Guard、事件管理和运行状态。

问题不是“缺少审核器”，而是“缺少统一决策、可靠处置、证据保护和复核闭环”。本轮实现继续复用 `securityaudit.Coordinator` 作为唯一网关入口，将新提示词审计、旧内容风控和上游 `cyber_policy` 结果写入统一决策与证据模型，并把用户/API Key 状态变化、开案和站内通知迁入持久化动作链。

当前采用两个边界清晰的控制面，而不是伪装成一个尚未具备完整运行时解析能力的“大一统策略”：

1. **检测与请求准入面**：现有 Prompt Audit 配置负责运行模式、审核节点、检测协议、超时、网络范围和失败模式；旧内容风控配置负责 66 条本地规则、关键词、哈希和兼容审核器。它们直接影响当前请求是否放行。
2. **治理与处置面**：V2 版本化策略负责作用域、风险级别到候选动作的映射、行为信号、证据保留、例外、回放、发布和回滚。所有非请求级副作用由动作 Worker 执行。

这个划分避免了“管理端显示已发布策略，但请求热路径实际上没有执行”的虚假安全。后续若要把检测器选择也迁入 V2，必须先实现带缓存、原子发布和请求级不可变快照的运行时解析器；在此之前不删除现有 Prompt Audit/内容风控配置。

V2 的上线硬门槛：

- 请求阻断可以同步发生；用户、API Key、会话状态变化必须先有持久化决策和动作意图。
- 未脱敏提示词不得再明文写入 PostgreSQL、普通日志、操作审计或管理端列表。
- 审核依赖失败必须产生可查询的 `degraded`/`failed` 决策，不能静默放行或静默丢弃。
- 审核节点目的地必须显式受控；公网、私网和 loopback 使用不同策略。
- 每次决定都能追到策略版本、检测器版本、证据、请求、处置和人工结论。
- 自动处置默认 API Key/会话优先，不因单次外部模型结果直接永久封禁用户。

### 1.1 已实现闭环

| 能力 | 当前实现 |
|---|---|
| 统一接入 | `securityaudit.Coordinator` 覆盖主要协议入口，并有顺序回归测试 |
| 证据保护 | 新写入不保存明文 prompt；AES-GCM 密文、摘要、脱敏预览、到期销毁、step-up reveal、`no-store`、访问审计 |
| 出站安全 | `public_https` / `trusted_network` / `loopback` 显式范围，阻止 metadata、链路本地、保留地址、非法重定向与 DNS 重绑定 |
| 失败语义 | allow-and-record、block-and-record、fallback-local、degraded-observe；同步降级事件与异步 admission 聚合可查询 |
| 统一记录 | prompt、legacy moderation、`cyber_policy` 和行为信号均产生 decision/evidence |
| 可靠动作 | decision/action/outbox 同事务；lease、fencing、重试、失败、陈旧租约回收、幂等、条件撤销 |
| 执行动作 | `pause_api_key`、`pause_user`、`notify_user`、`notify_admin`、`open_case` |
| 人工治理 | 案件、时间线、反馈、误判、例外、到期、动作撤销 |
| 策略治理 | draft、validate、simulate、replay、shadow、activate、rollback、不可变版本 |
| 行为信号 | 请求、Token、费用、错误率、业务限流率、耗时、IP/模型离散度；UTC 窗口与多实例 advisory lock |
| 用户闭环 | 用户仅能读取属于自己的安全通知，服务端强制接收人隔离，不暴露内部 decision/action 标识 |
| 管理端 | 概览、决策、案件、策略、动作、信号、例外、端点健康、通知工作台 |

### 1.2 明确不做的虚假能力

- `record_hash` 已从 V2 策略动作中移除：统一 Worker 没有连接请求热路径哈希拒绝缓存，不能把“数据库记了一条成功结果”冒充真正生效。旧内容风控的哈希缓存仍按原链路工作。
- `skip_detector` 已从例外效果移除：例外是在检测证据产生后应用，只能降级处置为 `allow_and_record` 或 `warn_only`，不能声称跳过了已经执行的检测器。
- `quarantine_session`、`throttle_api_key`、`resume_*` 等动作在具备真实执行器、撤销器和缓存一致性前不得出现在可发布策略中。

## 2. 范围

### 2.1 本期必须完成

- 多协议提示词提取与内容检测；
- 本地关键词、正则、哈希和远程模型检测；
- 上游 `cyber_policy` 安全信号；
- 分组、用户、API Key、模型、协议、端点作用域；
- 同步阻断、异步审计和事后上游信号；
- 版本化策略、影子比较、发布和回滚；
- 统一决策、证据、动作、Outbox；
- 会话/API Key/用户分级处置；
- 案件、人工复核、误判、例外、动作撤销；
- 提示词证据加密、保留和访问审计；
- 节点探测、熔断、失败转移和出站限制；
- 运行指标、积压、错误、阻断率和误判率；
- 管理端完整工作台；
- 迁移、兼容、故障恢复和端到端测试。

### 2.2 后续按真实需求触发

- 支付欺诈、拒付、设备指纹和跨站身份图谱；
- 自研规则 DSL；
- 独立风控微服务、Kafka 或流处理平台；
- 多管理员双人审批；
- 自动训练或微调审核模型；
- 机器学习异常检测。

上述能力在当前单管理员部署中没有足够收益，先用强类型策略、PostgreSQL、Redis 和现有 Go 进程完成闭环。

## 3. 审查基线与实施结果

### 3.1 请求接入

`securityaudit.Coordinator` 已接入主要网关入口。AST 回归测试保证 Prompt Audit 在账号选择、计费检查、并发占用、任务创建和上游转发之前执行。

审查基线模式（当前仍作为检测与请求准入面保留）：

| 新提示词审计模式 | 实际行为 |
|---|---|
| `off` | 仅执行旧内容风控 |
| `async_audit` | best-effort 异步入队，再同步执行旧内容风控 |
| `blocking` | 新旧两套检测并行；任一阻断即拒绝 |

优点：

- 已有唯一协调入口；
- blocking 并行执行可控制额外延迟；
- 入口覆盖测试能防止新增路由绕过。

审查时缺口及处理结果：

- 两套结果没有统一 `decision_id`：已增加统一 decision/evidence 兼容写入；
- 两套系统各自记录事件：旧表继续兼容，V2 表作为治理真相源；
- 旧链路副作用不受新链路动作状态机控制：生产仓储已原子生成动作/outbox，请求线程不再直接改用户状态；
- async 入队失败不可见：已增加可查询的 admission 聚合和稳定失败语义。

### 3.2 输入规范化

`prompt_snapshot.go` 已覆盖：

- OpenAI Chat Completions；
- OpenAI Responses 与 WebSocket `response.create`；
- Anthropic Messages；
- Gemini contents、system instruction、instances 与 batch requests；
- OpenAI Images/Grok/媒体提示词；
- system、developer、user、assistant、tool 等客户端可控角色。

当前算法优先扫描最后一段用户文本，再扫描其他上下文，避免长上下文分片时漏掉最新用户请求。图片二进制、URL 和长 base64 不进入文本扫描。

缺口：

- `PromptSnapshot.Redacted()` 只清空 `ScanText`，没有清空 `FullPrompt`；
- blocking 请求在写事件时仍携带未脱敏 `FullPrompt`；
- async worker 从 Redis 载荷重建完整提示词并明文落库；
- 默认最长保存 65,536 rune，泄露面过大；
- 对“无可提取文本”和“请求 JSON 无效”的策略不可配置；
- 缺少 canonicalizer 版本字段，历史事件不能准确复现提取算法。

### 3.3 新提示词审计可靠性

已具备：

- 配置 CAS 版本；
- Redis 多实例失效通知；
- 节点 Token 加密保存；
- PostgreSQL staging/queued/processing/retry/done/failed；
- 容量准入；
- `FOR UPDATE SKIP LOCKED` 领取；
- claim fencing；
- lease refresh 与 stale reclaim；
- Redis 短期载荷；
- 节点 failover；
- 全局/每节点并发隔离；
- blocking 事件与 job 同事务写入；
- 按筛选条件快照删除。

缺口：

- handler 线程只把 async 请求交给 128 槽本地 goroutine；槽满直接 dropped；
- 入队数据库、Redis 或发布失败只记录进程指标；
- Redis 载荷 TTL 固定 30 分钟，积压超过 TTL 后任务不可恢复；
- worker 消费时使用最新活动配置而非 job 入队时的不可变配置快照；
- 节点没有持久化熔断状态、连续失败阈值或冷却时间；
- 指标在进程内，重启归零，多实例不聚合；
- failed job 没有管理端重试、放弃、隔离和原因统计。

### 3.4 旧内容风控可靠性

已具备：

- 关键词自动机；
- 66 条默认且可编辑的 RE2 规则；
- 精确哈希预阻断；
- Moderations 和 Chat Completions；
- API Key 轮转和临时冻结；
- observe/pre-block；
- 分组、模型、采样和上下文范围；
- 违规邮件、自动封禁、解封；
- 命中/非命中保留期；
- `cyber_policy` 记录与会话屏蔽。

已确认的 P0：

1. `persistContentModerationLog` 先写哈希、封禁用户和发送邮件，最后 `CreateLog`。数据库写失败可产生无审计封禁。
2. 违规次数使用“查询历史数量 + 1”，并发命中可能得到相同计数。
3. 同步命中后的记录仍进入进程 channel，队列满时阻断已经发生但事件永久丢失。
4. `cyber_policy` 也可能先封禁后写日志。
5. 旧远程审核失败固定放行，缺少显式降级决策。
6. API Key 仍在旧内容风控配置路径中管理，与新审计节点密钥形成两套秘密生命周期。

### 3.5 证据与隐私

当前 `prompt_audit_events.full_prompt` 保存未脱敏完整提示词，事件详情直接返回；列表虽只返回预览，但所有管理员都能读取详情，读取行为不进入操作审计。

风险：

- 数据库、备份和只读副本泄露后可直接恢复用户提示词、密钥和个人信息；
- 管理员浏览事件详情无访问记录；
- 删除是物理删除，没有案件保全、法务保留或自动过期；
- migration 181 的“原文不入 PostgreSQL”注释已被 migration 182 的行为推翻；
- pass event 若开启保存，同样可能保存完整提示词。

### 3.6 审核模型与出站

当前节点：

- 仅支持 OpenAI-compatible Chat Completions；
- 默认模型 `sileader/qwen3guard:0.6b`；
- 只接受严格两行 `Safety:`/`Categories:`；
- 响应限制 256 KiB，`max_tokens=64`；
- 非 2xx、超时和解析失败已区分；
- 禁用环境代理，TLS 最低 1.2。

缺口：

- `NormalizeBaseURL` 允许 HTTP；
- dialer 允许 loopback、私网、link-local、保留地址和云元数据地址；
- 没有 DNS 重绑定防护；
- 没有主机 allowlist 或每节点网络范围；
- 新建任意节点不要求敏感操作二次验证；
- 只有 Qwen3Guard 适配器，不能复用标准 Moderations 或结构化 JSON 审核模型；
- 外部模型判定类别与旧 66 条网络滥用规则缺少稳定映射；
- 未记录供应商 request ID、finish reason 和模型原始版本摘要。

### 3.7 管理端

新提示词审计只有：

- 配置；
- 运行状态；
- 节点池与探测；
- 分组范围；
- 事件列表与详情；
- 单个、批量、按筛选删除。

缺少：

- 策略版本和发布历史；
- 决策/动作状态；
- 案件和人工结论；
- 例外和到期；
- failed jobs；
- 阻断、降级、误判和节点趋势；
- 证据访问二次验证和访问记录；
- 旧内容风控与新提示词审计的统一视图。

## 4. 目标架构

```text
Gateway
  │
  ▼
SecurityAudit Coordinator
  ├─ Canonicalizer ─────────────── canonicalizer_version
  ├─ Admission Config Resolver ─── immutable prompt/content config snapshot
  ├─ Local Detectors
  │    ├─ keyword
  │    ├─ regex
  │    └─ exact hash
  ├─ Remote Detector Planner
  │    ├─ OpenAI Moderations
  │    ├─ Qwen3Guard
  │    └─ Strict JSON Chat Audit
  ├─ Upstream Signal Adapter ───── cyber_policy / provider safety
  └─ Decision Engine
       ├─ synchronous request result
       ├─ Governance Policy Resolver ─ immutable policy_version
       └─ transaction
            ├─ security_audit_decisions
            ├─ security_audit_evidence
            ├─ security_audit_actions
            └─ security_audit_outbox
                         │
                         ▼
                Enforcement Worker
                 ├─ API Key pause
                 ├─ user pause
                 ├─ case creation
                 └─ admin/user notification
```

设计约束：

- `Coordinator` 仍是网关唯一入口；
- `ContentModerationService` 第一阶段保留为 local/legacy detector adapter；
- 所有账户级副作用只由 Enforcement Worker 执行；
- 请求级阻断不等待异步动作执行；
- PostgreSQL 是决策、动作和案件真相源；
- Redis 只保存短期敏感载荷、缓存、滚动窗口和会话隔离；
- 检测配置解析后形成不可变运行快照，job 必须绑定快照版本；
- V2 策略只允许发布已有真实执行器的动作，未知动作进入显式 failed，不能伪成功。

## 5. 核心领域模型

### 5.1 AuditRequest

```go
type AuditRequest struct {
    AuditID       string
    RequestID     string
    Stage         string // pre_request | async_request | post_upstream
    UserID        int64
    APIKeyID      int64
    GroupID       *int64
    Provider      string
    Endpoint      string
    Protocol      string
    RequestedModel string
    ClientIPHash  string
    SessionHash   string
    Body          []byte // 仅请求期内存在
}
```

`AuditID` 在进入 Coordinator 时生成，并贯穿本地检测、远程检测、上游安全信号、决策和动作。

### 5.2 CanonicalSnapshot

必须包含：

- canonicalizer 版本；
- 提示词指纹；
- 消息数和字符数；
- 最新用户段指纹；
- 协议、端点和模型；
- 脱敏预览；
- 临时载荷引用；
- 不包含明文 Token、Cookie、账号凭据。

### 5.3 DetectorEvidence

```go
type DetectorEvidence struct {
    DetectorID      string
    DetectorVersion string
    Category        string
    Score           float64
    Severity        string
    Outcome         string // matched | clear | error | skipped
    ErrorCode       string
    EvidenceDigest  string
    SafeSummary     string
    LatencyMS       int
}
```

检测器只能提交证据，不得直接封禁主体。

### 5.4 AuditDecision

```go
type AuditDecision struct {
    DecisionID       string
    AuditID          string
    PolicyID         string
    PolicyVersion    int64
    EvaluationStatus string // complete | degraded | failed
    RiskLevel        string // none | low | medium | high | critical
    RequestAction    string // allow | warn | block
    CandidateActions []string
    FailureMode      string
    FailureReason    string
    DecisionDigest   string
}
```

`CandidateActions` 是策略计算结果；实际动作必须由 `security_audit_actions` 状态机执行。

### 5.5 EnforcementAction

状态：

```text
pending -> processing -> succeeded
                    └-> retry
                    └-> failed
pending/failed/succeeded -> cancelled|reverted
```

动作类型：

- `pause_api_key`；
- `pause_user`；
- `notify_user`；
- `notify_admin`；
- `open_case`。

其中 pause、通知和开案均已有真实执行器。其他动作只有在同时具备执行、幂等、失败、回滚、缓存失效与测试后才能进入 `validPolicyActions`。动作白名单与 Worker switch 必须同步审查。

数据库迁移 `298` 将 `security_audit_actions` 的新写入约束收紧到上述五类真实执行器；历史终态的保留动作名继续作为审计事实存在，历史非终态未知动作被显式失败，不能再进入 Worker 队列。约束使用 `NOT VALID` 仅用于兼容历史终态行，不会放宽新写入。

幂等键：

```text
decision_id + action_type + subject_type + subject_id + policy_action_version
```

### 5.6 Case

状态：

```text
open -> reviewing -> confirmed
                  ├-> false_positive
                  ├-> dismissed
                  └-> expired
```

案件必须保存不可变时间线：

- 创建原因；
- 关联决策；
- 关联动作；
- 查看证据记录；
- 管理员备注；
- 人工标签；
- 最终结论；
- 撤销动作结果。

### 5.7 Exception

例外是有期限的策略作用域覆盖，不是删除命中记录。

范围：

- 用户；
- API Key；
- 分组；
- 模型；
- 端点；
- detector/category。

必须包含原因、创建人、开始/结束时间、最大作用范围和状态。默认最长 30 天；永久例外需要显式选择。

## 6. 策略

### 6.1 强类型结构

不实现通用 DSL。策略版本使用经过 Go 结构体验证的 JSON：

```json
{
  "name": "default-safety",
  "priority": 100,
  "scope": {
    "all_groups": true,
    "group_ids": [],
    "user_ids": [],
    "api_key_ids": [],
    "protocols": [],
    "endpoints": [],
    "models": []
  },
  "mode": "blocking",
  "detectors": [
    {"id": "builtin_regex", "enabled": true, "timeout_ms": 20},
    {"id": "remote_guard", "enabled": true, "timeout_ms": 2500}
  ],
  "failure": {
    "local_error": "allow_and_record",
    "remote_timeout": "fallback_local",
    "remote_invalid": "block_and_record"
  },
  "actions": {
    "low": [],
    "medium": ["notify_admin"],
    "high": ["open_case", "notify_admin"],
    "critical": ["open_case", "notify_admin"]
  },
  "evidence": {
    "mode": "findings_encrypted",
    "retention_days": 30
  }
}
```

### 6.2 作用域优先级

作用域不是所有字段的宽松“或”关系。匹配语义固定为：

- `api_key_ids`、`user_ids`、`group_ids` 是主体选择器，三者内部为“或”；
- 没有主体选择器时表示任意主体；`all_groups=true` 也表示任意主体，但不能再同时配置三个主体选择器；
- `protocols`、`endpoints`、`models` 是请求过滤器；每个已配置维度都必须命中，维度之间为“且”，单个维度的多个值内部为“或”；
- 所有字符串比较不区分大小写；
- 因此“分组 A + 模型 B”只匹配分组 A 对模型 B 的请求，不能扩大为“分组 A 的全部请求或全站对模型 B 的请求”。

多个策略同时命中时，选择顺序：

1. API Key 精确策略；
2. 用户精确策略；
3. 分组策略；
4. 任意主体策略。

同一主体层级中，请求过滤维度更多的策略优先，然后按显式 priority 降序，再按 policy ID 和版本稳定排序。最终只能选一个治理/处置策略，禁止多个策略分别执行副作用。

行为信号窗口只按 API Key、用户或分组聚合，不持有单次请求的协议、端点和模型。因此启用 `signals` 的策略禁止同时配置 `protocols`、`endpoints` 或 `models`；服务端在验证/发布时拒绝这种不可真实计算的组合，运行时也会防御性跳过旧的非法策略。

当前 `detectors` 与 `failure` 字段用于校验、模拟、回放和迁移目标描述；请求热路径的检测器实例、节点 Token、网络范围和失败模式仍由 Prompt Audit/内容风控运行配置提供。管理端必须明确展示这一区别。

策略 `mode` 的运行语义固定如下，不能由调用方自行解释：

| mode | 请求准入预测 | 统一 decision | action/outbox |
|---|---|---|---|
| `off` | `allow` | 可记录已有检测链产生的事实，但策略不追加处置 | 禁止产生 |
| `async_audit` | `allow` | 记录审计事实、证据摘要与命中范围 | 禁止产生 |
| `blocking` | 按风险得到 `allow/warn/block` | 记录审计事实 | 按风险动作表可靠产生 |

`off` 和 `async_audit` 可以保留已配置动作，便于不可变版本审查和后续复制为 blocking 草稿，但运行时必须通过单一 `effectiveCandidateActionsForRisk` 边界清空动作；统一 decision、行为信号、simulate、replay 和 shadow 都使用相同语义。这样“异步只审计”不会意外暂停用户、暂停 API Key、开案或发送通知。V2 的请求准入仍是预测信息，实际热路径阻断由 Prompt Audit/内容风控运行配置执行。

### 6.3 生命周期

```text
draft -> validated -> shadow -> active -> retired
                              └-> rollback target
```

- draft 可编辑；
- validated 生成 canonical JSON 与 digest；
- shadow 计算但不处置；
- active 不可原地修改；
- rollback 激活历史不可变版本；
- 每次状态转换与策略版本在同一事务中保存 from/to、actor 和原因；配置 diff 可由两个不可变版本的 canonical JSON 确定。
- shadow 版本由维护 Worker 按统一 decision 主键增量评估，使用 PostgreSQL watermark 与 advisory lock 保证多实例下单一推进。
- signal 与 shadow 维护每轮最多连续消费 20 个 500 条批次；返回值表示已推进的窗口/decision 数而不是命中数，避免“整批无命中”被误判为空队列并造成持续积压。
- shadow 只写准入预测/候选动作比较结果，绝不创建 action/outbox；状态变更后只评估 `shadowed_at` 之后的新判定。

单管理员部署不要求双人审批；策略 activate/rollback、完整证据 reveal、审计事件物理删除、例外创建/人工失效、动作人工重试/撤销、审核运行配置保存和端点探测要求 step-up 2FA。私网/loopback 节点还必须在运行配置中显式选择网络范围。

### 6.4 失败模式

允许值：

- `allow_and_record`；
- `block_and_record`；
- `fallback_local`；
- `degraded_observe`。

任何失败都必须写 decision：

- `evaluation_status=degraded|failed`；
- `failure_reason` 使用稳定枚举；
- 不保存原始上游错误正文；
- 运行指标按失败原因聚合。

## 7. 检测器

### 7.1 本地检测

复用现有实现：

- `blocked_keywords`；
- 66 条内置/用户编辑 RE2；
- flagged hash。

改造要求：

- 输出统一 `DetectorEvidence`；
- 规则集合绑定不可变版本和 digest；
- hash 按策略版本命名空间保存并带 TTL；
- defensive/educational context 只影响分数，不绕过严格规则；
- 正则编译失败阻止策略发布，不影响当前活动版本。

### 7.2 远程检测适配器

首版支持三类：

1. `qwen3guard_chat`：现有两行协议；
2. `openai_moderations`：标准分类分数；
3. `strict_json_chat`：使用固定 system prompt 和严格 JSON 对象。

每类适配器必须限制：

- 请求体大小；
- 输入字符数；
- 输出 token；
- 响应字节数；
- 重定向；
- 超时；
- 重试；
- 并发；
- finish reason；
- 可接受字段；
- 未知分类处理。

模型输出里的 `flagged` 不是最终决定。服务端根据策略类别和阈值重新计算风险级别。

### 7.3 上游安全信号

`cyber_policy` 改为 `post_upstream` evidence：

- 使用同一 `AuditID`/request ID；
- 不再次扫描提示词；
- 不直接调用旧自动封禁；
- 按策略决定会话隔离、API Key 暂停或案件；
- 上游 body 只转成安全错误码和摘要；
- 与用量记录、账号 ID、路由尝试关联。

### 7.4 行为、用量和费用

不做机器学习，复用已有数据：

- API Key 请求速率和并发；
- 401/429/5xx 比率；
- 单用户费用增长；
- 单请求 token/费用异常；
- 会话连续安全命中；
- 多 API Key 共享异常 IP；
- 账号切换和 failover 异常。

首版使用滚动窗口阈值和冷却时间。只输出证据，最终动作仍由策略决定。

## 8. 同步和异步执行

### 8.1 同步阻断

时间预算：

- 本地检测：目标 P99 < 10 ms；
- 远程检测：由策略设置，默认 2500 ms；
- 总预算：默认 3000 ms；
- 超时后按 failure mode 决定。

顺序：

1. 规范化；
2. 解析不可变策略；
3. 本地检测；
4. 若本地已达到立即阻断且策略允许，跳过远程；
5. 否则调用远程；
6. 计算决策；
7. 写决策/证据/动作/outbox；
8. 返回 allow/warn/block。

数据库写失败时：

- 请求级 `block` 可按已计算结果返回；
- 不允许执行任何账户级动作；
- 写结构化本地错误和持久化失败指标；
- 若策略要求“无审计不可阻断”，返回 503 而不是伪装成内容违规。

### 8.2 异步审计

不再把入队失败当作不可见 dropped：

- 本地 enqueue 槽满：写最小 `degraded` admission event；
- PostgreSQL 容量满：写聚合 overflow counter；
- Redis 载荷失败：job 标记 `failed` 并可查询；
- payload TTL 必须覆盖 `max(queue_age + retry_window)`；
- job 绑定 policy version、detector plan digest 和 evidence mode；
- worker 不使用后来变更的活动策略。

### 8.3 载荷存储

Redis 仅保存加密或受保护的短期扫描载荷：

- key 包含 job ID；
- TTL 按策略计算；
- worker 完成/失败后删除；
- reclaimer 清理孤儿 key；
- Redis 不可用时 async 不伪装为成功；
- blocking 不依赖 Redis。

## 9. 证据保护

### 9.1 保存模式

```text
none
digest_only
findings_encrypted
full_encrypted
```

默认：`findings_encrypted`。

- `none`：统一 decision 保留，但不写 detector evidence；
- `digest_only`：保留 detector identity、类别、分值与 digest，不保留 safe summary；
- `findings_encrypted`：保留已脱敏 finding，不保留可 reveal 的完整提示词；
- `full_encrypted`：flag/critical 可保存加密完整提示词，并按策略保留天数销毁；
- pass event 始终不保存完整提示词；
- 明文只存在请求内存、短期载荷和授权后的响应；
- PostgreSQL 不新增明文 prompt；V2 策略与 prompt event 在同一事务内收窄证据。

### 9.2 加密

复用现有 `SecretEncryptor`。当前兼容事件保存：

- `evidence_ciphertext`；
- `evidence_status`；
- `evidence_expires_at`。

不把 ciphertext 塞回 `full_prompt` 字段。新代码停止写明文；后台分批迁移 migration 182 已有数据，成功加密后清空旧列。

加密器使用有界密钥环：

- `totp.encryption_key` 是唯一写密钥，新产生的 TOTP、审核证据和其他共享受保护字段只使用该密钥；
- `totp.previous_encryption_keys` 最多保存 4 把只读历史密钥，解密时严格按当前密钥、历史密钥顺序尝试；
- 当前密钥与历史密钥必须都是 64 位十六进制 AES-256 密钥，且不得重复；
- 数据库已持久化的当前密钥只有在其明确出现在 `previous_encryption_keys` 时才允许被新当前密钥条件更新，防止配置错误静默破坏全部密文；
- 密文格式保持 `base64(nonce+ciphertext+tag)`，现有数据无需格式迁移；
- 当前没有假定所有共享加密字段都具备统一后台重加密作业。旧密钥只有在相关数据已被业务重写、受控迁移或超过保留期后才能移除。

### 9.3 查看

普通事件详情只返回：

- 脱敏预览；
- 证据摘要；
- digest；
- 是否存在加密证据；
- 保留截止时间。

完整证据使用独立接口：

```text
POST /api/v1/admin/security-audit/decisions/{id}/evidence/reveal
```

要求：

- 当前管理员；
- step-up 2FA；
- 必填查看原因；
- 响应 `Cache-Control: no-store`；
- 不进入普通前端 store；
- 每次查看写操作审计；
- 不支持批量导出原文。

### 9.4 保留

- pass 不保存完整提示词；
- `full_encrypted` 原文按活动 V2 策略的 `retention_days` 到期销毁；没有活动 V2 策略时兼容默认值为 30 天；
- 行为窗口保留 30 天；
- 已读/已忽略通知保留 90 天；
- decision/action/case 等最小治理元数据默认保留，避免删除仍生效暂停动作的审计依据；
- 案件不会自动延长完整 prompt 的保留期；管理员应优先依靠 safe summary、digest 和复核结论；
- cleanup 使用数据库小批量或有界 SQL，不把明文写入删除日志。

## 10. 审核节点安全

### 10.1 网络范围

每节点新增：

```text
public_https
trusted_network
loopback
```

- `public_https`：只允许 HTTPS，拒绝私网、loopback、link-local、multicast、unspecified、保留地址和元数据地址；
- `trusted_network`：允许管理员明确配置的内网审核服务，仍执行主机、端口、解析地址和重定向校验；
- `loopback`：只允许 `127.0.0.0/8`、`::1` 或 localhost，适用于本机模型；
- HTTP 只可用于 `trusted_network` / `loopback`；
- 每次 dial 后校验实际 IP；
- 重定向默认禁止；若启用，只允许同 scheme/host/port；
- 不继承环境代理。

创建或扩大 private/loopback 范围要求 step-up 2FA，并写操作审计。

### 10.2 健康与熔断

每节点维护：

- 最近成功/失败；
- 连续失败数；
- 请求、成功、timeout、429、5xx、invalid response 计数；
- latency sum、last 与 max；
- 429/5xx/timeout/invalid response；
- breaker 状态；
- 冷却截止；
- 在途请求。

熔断：

```text
closed -> open -> half_open -> closed
```

状态需要跨实例共享，使用 PostgreSQL 小表或 Redis；不只放进程内。

## 11. 可靠动作与 Outbox

### 11.1 事务

同一事务写入：

1. decision；
2. evidence；
3. enforcement action；
4. outbox。

worker 使用 `FOR UPDATE SKIP LOCKED`，领取后通过 lease 和 attempt 防止重复执行。

### 11.2 主体处置顺序

默认升级顺序：

1. 仅记录；
2. warning；
3. 通知管理员/用户；
4. 自动开案；
5. API Key pause；
6. user pause；
7. 永久删除只允许人工操作。

管理员账号不自动暂停用户，但其 API Key 仍可暂停并可自动开案。

### 11.3 撤销

动作保存 before snapshot：

- 原状态；
- 原因；
- 到期时间；
- auth cache 版本；
- 关联会话 key。

撤销只恢复该动作实际改变的状态；若管理员后来手工修改，自动撤销不得覆盖新状态。

## 12. 案件、复核和例外

### 12.1 自动开案

条件可配置：

- critical；
- 自动 API Key/user pause；
- 同用户窗口内多次 high；
- 上游 `cyber_policy`；
- 远程模型与本地规则严重冲突；
- 管理员手工创建。

### 12.2 复核操作

- `confirmed`：维持动作，可追加规则标签；
- `false_positive`：撤销可撤销动作，生成反馈样本；
- `dismissed`：关闭但不作为误判；
- `needs_more_info`：保留；
- `expire`：只适用于无处置案件。

### 12.3 反馈

反馈记录：

- case/decision；
- 人工标签；
- 预测类别和分数；
- 策略版本；
- detector 版本；
- 结论；
- 可选脱敏备注。

反馈不自动修改生产阈值。阈值变化必须走策略新版本和 shadow 对比。

## 13. 数据模型

### 13.1 首批表

#### `security_audit_policy_versions`

- `id BIGSERIAL`；
- `policy_key VARCHAR`；
- `version BIGINT`；
- `status VARCHAR`；
- `priority INT`；
- `config JSONB`；
- `config_digest VARCHAR(64)`；
- `created_by BIGINT`；
- `created_at TIMESTAMPTZ`；
- `activated_at/retired_at TIMESTAMPTZ`；
- unique `(policy_key, version)`。

#### `security_audit_decisions`

- `id BIGSERIAL`；
- `decision_id VARCHAR(64) UNIQUE`；
- `audit_id VARCHAR(64)`；
- `request_id VARCHAR(128)`；
- subject snapshots；
- request scope；
- `policy_key/policy_version`；
- `canonicalizer_version`；
- `evaluation_status`；
- `risk_level`；
- `request_action`；
- `failure_mode/failure_reason`；
- `prompt_hash`；
- `redacted_preview`；
- `decision_digest`；
- `created_at`。

#### `security_audit_evidence`

- `id BIGSERIAL`；
- `decision_id` FK；
- detector identity/version；
- outcome/category/score/severity；
- safe summary/digest；
- latency/error code；
- encrypted evidence columns；
- `expires_at`；
- `hold_until`。

#### `security_audit_actions`

- `id BIGSERIAL`；
- `action_id VARCHAR(64) UNIQUE`；
- `decision_id` FK；
- type/subject/status；
- idempotency key unique；
- attempts/lease/next attempt；
- before/after snapshot JSONB；
- error code；
- created/processed/reverted timestamps。

#### `security_audit_outbox`

- action/event identity；
- topic；
- payload JSONB（无原文和密钥）；
- status/attempts/lease/next attempt；
- created/published timestamps。

### 13.2 闭环表

- `security_audit_cases`；
- `security_audit_case_events`；
- `security_audit_exceptions`；
- `security_audit_feedback`；
- `security_audit_evidence_access_logs`；
- `security_audit_endpoint_health`。

### 13.3 索引

至少覆盖：

- created_at + id keyset；
- request ID；
- decision ID；
- user/API Key/group；
- risk/status/action；
- policy version；
- active action queue；
- open cases；
- active exceptions；
- evidence expiry。

事件工作台继续支持 offset pagination 兼容；高数据量 API 新增 keyset cursor。

## 14. API

统一前缀：

```text
/api/v1/admin/security-audit
```

旧 `/admin/prompt-audit` 与 `/admin/content-moderation` 在迁移期保留。

### 14.1 概览

- `GET /overview?window_hours=`：决策、案件、动作、策略、例外、信号延迟、反馈质量和证据查看统计；
- 旧 `GET /admin/prompt-audit/runtime`：检测任务、节点和进程级运行指标；
- 旧 `GET /admin/prompt-audit/events`：兼容事件与任务管理。

没有单独实现的 `/metrics`、`/health`、`/jobs` V2 路由不在本文中冒充可用 API；相关数据由 overview、endpoints、signals、actions 和旧 runtime/events 组合提供。

### 14.2 策略

- `GET /policies`；
- `POST /policies`；
- `GET /policies/{key}/versions`；
- `GET /policies/{key}/transitions`；
- `GET /policies/{key}/versions/{version}/shadow-evaluations`；
- `POST /policies/{key}/versions/{version}/validate`；
- `POST /policies/{key}/versions/{version}/simulate`；
- `POST /policies/{key}/versions/{version}/replay`；
- `POST /policies/{key}/versions/{version}/shadow`；
- `POST /policies/{key}/versions/{version}/activate`；
- `POST /policies/{key}/versions/{version}/rollback`。

### 14.3 节点

- `GET /endpoints`；
- `POST /endpoints/{id}/reset-breaker`；
- 旧 `POST /admin/prompt-audit/endpoints/probe`：使用相同出站网络边界执行主动探测。

### 14.4 决策和证据

- `GET /decisions`；
- `GET /decisions/{id}`；
- `POST /decisions/{id}/evidence/reveal`；
- `POST /decisions/{id}/open-case`；
- `POST /decisions/{id}/feedback`。

### 14.5 动作

- `GET /actions`；
- `POST /actions/{id}/retry`；
- `POST /actions/{id}/cancel`；
- `POST /actions/{id}/revert`。

### 14.6 案件和例外

- `GET /cases`、`GET /cases/{id}`、`POST /cases/{id}/transition`；
- `GET /exceptions`、`POST /exceptions`、`POST /exceptions/{id}/expire`；创建和人工失效都要求 step-up，失效必须填写理由并持久化操作者；
- `GET /signals`；
- `GET /notifications`、`POST /notifications/read-all`、`POST /notifications/{id}/status`；
- 用户侧 `GET /security-audit/notifications`、`POST /read-all`、`POST /{id}/status`；
- 管理员状态接口只能修改 `audience=admin` 的通知；用户通知的已读/忽略状态只能由匹配 `recipient_user_id` 的用户接口修改，避免后台浏览污染用户收件状态；
- 所有 mutation 写操作审计；
- evidence reveal、策略激活/回滚、审核运行配置、端点探测、审计事件单条/批量/筛选物理删除、例外创建/人工失效、动作重试/取消/撤销、案件状态转换和端点 breaker 重置要求 step-up。

## 15. 管理端

页面使用现有 Vue 技术栈与当前项目视觉系统，不新建第二套前端。

安全审计工作台包含：

1. **概览**：请求、命中、阻断、降级、P95/P99、队列、节点、待处理案件；
2. **决策**：统一事件列表、证据摘要、检测器结果、动作状态；
3. **案件**：队列、时间线、人工结论、撤销；
4. **策略**：草稿、版本、差异、shadow、发布、回滚；
5. **运行/动作**：pending/retry/failed/succeeded/reverted；
6. **行为信号**：聚合窗口、命中规则、观测值和阈值；
7. **例外**：范围、原因、到期；
8. **端点**：健康、连续失败、熔断和重置；
9. **通知**：管理员通知与用户收件闭环。

本地规则编辑、远程节点配置、任务与兼容事件仍位于现有 Prompt Audit/内容风控页签中，工作台不复制第二套配置表单。

交互要求：

- 列表刷新保留旧数据，显示局部 loading，不整页闪烁；
- 筛选写 URL query，返回后可复现；
- 事件详情先显示安全摘要；
- 完整证据按需 reveal，关闭弹窗后立即清空内存；
- 策略编辑有脏状态、CAS 冲突、服务端校验和 diff；
- 所有状态变化使用明确确认，不用原生 `<select>`；
- 手机端卡片化显示核心字段，详情和筛选使用抽屉。

## 16. 可观测性

核心指标：

- audit requests by stage/mode/scope；
- local/remote detector outcomes；
- decisions by risk/action/status；
- admission drops/degraded；
- queue age/depth/retry/failed；
- action pending/retry/failed；
- endpoint request/timeout/429/5xx/invalid；
- detector P95；
- evidence reveal；
- cases open/age/false-positive；
- active policy 数量与每条决策绑定的 policy version。

当前 overview 指标从 PostgreSQL 聚合，跨实例一致；Prompt Audit runtime 的瞬时计数仍为进程级，不能拿它代替持久化治理指标。

告警：

- blocking unavailable；
- degraded 比率持续超阈值；
- 阻断率突增；
- queue oldest age 超阈值；
- payload missing；
- action failed；
- endpoint 全部熔断；
- evidence cleanup 失败；
- 自动 pause 数突增；
- 策略版本多实例不一致。

## 17. 迁移与兼容

### 17.1 数据迁移

1. additive migration 创建 V2 表和加密证据列；
2. 新写路径先双写 decision，旧路径继续决定处置；
3. 停止向 `prompt_audit_events.full_prompt` 写明文；
4. 后台批量加密历史 `full_prompt`，校验后清空；
5. 旧 content log 映射为 legacy evidence；
6. `cyber_policy` 双写统一 decision；
7. 动作链影子运行，只生成 candidate action；
8. 验证后启用 Enforcement Worker；
9. 旧用户状态自动封禁改为原子创建统一 pause action；旧邮件保留为补充 best-effort 通道，站内通知由统一动作提供；
10. 旧管理 API 和旧表在兼容期继续读写；
11. 稳定两个发布周期并确认无外部依赖后，另行决定是否停止旧表写入。

### 17.2 配置迁移

现有配置保持两个清晰层级：

- Prompt Audit/内容风控配置：检测运行时、节点、超时、失败和本地规则；
- V2 active policy：作用域、动作、证据、行为信号和治理发布。

只有在请求级策略解析器具备缓存、原子发布、不可变快照和完整回归后，才能把前者迁入 V2；不得仅迁移管理端字段而不改变热路径。

现有节点的主机和端口转换为精确 allowlist：

- 公网 HTTPS -> `public_https`；
- loopback -> `loopback`；
- 私网 -> `trusted_network`。

迁移不静默扩大范围。

### 17.3 回滚

- 每阶段独立 feature flag；
- 回滚执行路径，不删除新表；
- 已执行动作不会因代码回滚自动恢复；
- 由 action revert 显式恢复；
- 策略可回滚到历史版本。

## 18. 测试

### 18.1 单元测试

- 协议提取和 canonicalizer version；
- 策略匹配/优先级/例外；
- 本地规则；
- 三种远程适配器严格解析；
- failure mode；
- decision digest；
- action state machine；
- evidence encryption/redaction；
- 网络地址分类和 redirect；
- case transition。

### 18.2 集成测试

- PostgreSQL 事务原子性；
- Redis payload TTL/丢失；
- worker claim fencing；
- process crash/reclaim；
- action 幂等；
- auth cache invalidation；
- evidence reveal step-up/audit；
- policy CAS/publish/rollback；
- historical full prompt migration；
- concurrent violation escalation。

### 18.3 端到端

- 各协议真实请求；
- local block 不调用远程；
- remote allow/flag/block；
- timeout 下四种 failure mode；
- async admission failure 可见；
- `cyber_policy` 统一记录；
- API Key pause/revert；
- false positive case；
- mobile/desktop 管理端。

### 18.4 安全测试

- prompt injection 不能改变审核输出契约；
- scanner 响应截断/多对象/markdown/超长/未知类别；
- Token、Cookie、邮箱、手机号和长编码不进日志；
- SSRF 到 metadata、loopback、私网和 DNS rebinding；
- 非管理员与未 step-up 无法 reveal；
- reveal 响应 no-store；
- 操作审计不捕获请求 body；
- SQL filter/delete token 防竞态。

### 18.5 故障演练

- kill worker；
- PostgreSQL 短暂不可用；
- Redis 丢失；
- 全节点超时；
- 单节点 429；
- 节点返回无效结构；
- action 执行中重启；
- 配置版本传播延迟；
- evidence key 轮换（包含旧密文读取、新密文仅用新密钥、错误密钥启动失败和回滚）。

## 19. 实施顺序与结果

### Phase A：P0 数据和处置安全

- 停止明文提示词新写入；
- 加密证据、按需 reveal、访问审计和保留；
- 出站 network scope；
- 旧风控改为先写 decision/action，后副作用；
- async dropped/failed 可查询；
- 明确 failure mode。

退出门槛：

- 数据库新增记录没有明文 prompt；
- 任一主体状态变化都有 decision/action；
- 队列/依赖失败在后台可见；
- 私网/loopback 必须显式授权。

实施结果：已完成，迁移 `289`、`290`。

### Phase B：统一决策和可靠动作

- V2 decision/evidence/action/outbox；
- 旧本地检测 adapter；
- `cyber_policy` adapter；
- Enforcement Worker；
- API Key 优先处置；
- 幂等、retry、revert。

退出门槛：

- 每次检测结果进入统一治理决策；
- 用户/API Key 状态副作用只由统一 Worker 执行；
- crash/restart 不丢动作。

实施结果：已完成核心动作链，迁移 `291` 建立模型，迁移 `298` 将数据库新写入白名单收紧到真实执行器；旧邮件仅作为补充通道，不承担站内通知和状态真相。

### Phase C：版本化策略

- policy versions；
- validate/simulate/shadow/activate/rollback；
- 不可变 worker snapshot；
- 例外。

退出门槛：

- 每个 decision 有可还原策略版本；
- 5 秒内可回滚；
- shadow 无真实副作用。

实施结果：已完成版本、模拟、回放、实时 shadow、activate、rollback、状态历史和例外；迁移 `294` 修正例外语义，迁移 `296`、`297` 分别固化策略状态历史和实时 shadow 结果，迁移 `299` 强制人工失效例外记录操作者和理由。

### Phase D：案件和反馈

- cases/timeline；
- evidence reveal；
- feedback；
- false-positive revert；
- 到期和保留。

退出门槛：

- 自动动作可复核、可撤销；
- 误判能进入策略评估样本。

实施结果：已完成案件时间线、反馈、条件撤销和证据访问审计。

### Phase E：行为/用量/费用信号

- PostgreSQL 持久化 UTC 窗口；
- usage/billing/ops adapters；
- 分级 throttle/pause；
- 真实聚合指标。

退出门槛：

- 异常可按 user/API Key/group 解释，并保留模型离散度；
- 不从前端分页数据推算。

实施结果：已完成持久化 UTC 分钟窗口、阈值评估、watermark、多实例锁和通知；迁移 `292`。

### Phase F：管理端与发布验证

- 完整工作台；
- 多实例指标；
- 告警；
- 回放数据集；
- 容量和故障演练；
- 旧 API 兼容验证。

实施结果：管理工作台、策略回放、实时 shadow、端点 breaker、用户通知和质量指标已完成；迁移 `293`、`297`。容量/故障演练属于每次实际部署的发布门禁，步骤见 `SECURITY_AUDIT_OPERATIONS_CN.md`。

## 20. 完成定义

只有同时满足以下条件，安全审计体系才算完成：

- [x] 所有已登记网关入口覆盖测试通过；
- [x] 新写路径不再新增明文提示词或审核 Token；
- [x] 完整证据 reveal 有 step-up、no-store 和访问审计；
- [x] 所有统一 decision 带 policy/canonicalizer/detector 身份或兼容来源标识；
- [x] 所有用户/API Key 状态动作先持久化且幂等；
- [x] worker 使用 lease/fencing，重启可回收；
- [x] failure mode 可配置且降级可查询；
- [x] 公网/私网/loopback 节点边界可验证；
- [x] 旧 regex/hash/keyword 与 `cyber_policy` 已接入统一证据；
- [x] 案件、误判、例外和 revert 闭环可用；
- [x] 策略可 simulate/replay/shadow/发布和回滚；
- [x] 管理端局部加载，完整证据关闭后清空；
- [x] 后端定向测试、迁移静态测试、前端类型与组件测试通过；
- [x] 行为信号、实时 shadow、端点 breaker 和 action/outbox 在真实 PostgreSQL 上完成事务与幂等闭环验证；
- [x] Redis 配置 CAS、失效传播、TTL 与不可用降级在真实 Redis 上完成验证；
- [x] 操作手册覆盖依赖故障、阻断突增、队列积压、密钥和证据泄露；
- [ ] 在目标部署的 PostgreSQL/Redis/反向代理环境完成故障演练与容量基准；
- [x] 执行扩大范围回归并确认与本功能无关的既有失败已单独登记。

本文替代旧设计中与当前代码不一致的实施状态，但保留
`RISK_CONTROL_ENTERPRISE_DESIGN_CN.md` 作为历史分析资料。实施以本文件的
单管理员、渐进迁移和完成定义为准。
