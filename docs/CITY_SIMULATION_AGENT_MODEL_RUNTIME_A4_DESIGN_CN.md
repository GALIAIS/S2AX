# A4：Agent Model Profile、受控路由与预算运行时设计

## 1. 目标、边界与发布策略

本设计为共享 realtime world 的 Agent 增加可审计、可撤销、可限额的模型运行时。它解决的不是“让模型直接操纵世界”，而是把已经存在的 `Observation → Decision → Intent → reducer` 链路接到一个受控的模型选择与执行边界上。

本阶段必须同时满足：

- 管理员能定义模型档案（Model Profile）并将其绑定到指定 world/Agent definition；
- 角色、NPC 或系统 Agent 不能得到 API Key、账号池、原始 URL、其他用户用量、真实成本或任意路由权限；
- 每个 queued request 在创建时固定 profile snapshot；管理员之后改档案、换线路或禁用 profile，不会改写历史 request/attempt/decision；
- 调用在数据库事务之外执行，返回结果仍须经过现有 schema、precondition、Action adapter 和 sealed reducer；
- 预算、并发、超时、重试与熔断在 provider 前 fail-closed，且不会影响 realtime 时钟或直接发放城市/平台资产；
- 旧 world 与旧 `fake.deterministic` 历史保持可读、可回放；新世界默认绑定显式的 deterministic profile，而不是隐式依赖硬编码。

本阶段不做以下事情：

- 不允许浏览器提交 provider URL、API Key、自由 system prompt、模型原始响应或账户选择；
- 不把模型调用费用自动扣到用户余额、城市账本或虚拟货币；
- 不让模型输出坐标、奖励、法律结论、金额、角色属性或任意 JSON patch；
- 不在 A4.0 中把网关 HTTP handler 反向作为内部 provider 调用，也不复用普通用户 API Key；
- 不因 profile 缺失、禁用、预算耗尽或 provider 故障暂停整个 world。

当前实现状态：A4.0 已完成；A4.1 已完成进程内 provider registry、严格 envelope parser、deterministic adapter，以及专用 `sub2api.gateway` 系统身份 transport。该 transport 已支持管理员专用分组中的 OpenAI-compatible API-key account 与已完整 provision 的 OpenAI Agent Identity account；普通 OAuth bearer account、其他平台/账户类型一律 fail-closed，绝不伪装成可执行。A4.2 已完成独立决策 worker、持久化延迟、provider retry/backoff、circuit breaker、管理员聚合健康投影、陈旧隔离告警，以及可审计的 request quarantine/release 控制台；A4.3 尚未开放。每一阶段均保持先前 request 的可解释性。

发布顺序仍保持不变：A4.0 建立不可变档案、绑定、request/attempt snapshot 与预算账本；A4.1 接入受控 provider adapter；A4.2 加入 retry/backoff、circuit breaker worker；A4.3 才开放 owner 可选 profile 与最小 UI。实际外部模型传输只有在专用系统身份 adapter 完成后才会启用，不能以浏览器、loopback HTTP、普通用户 API Key 或伪造 Gin context 代替。

---

## 2. 信任模型

| 主体 | 能做什么 | 明确不能做什么 |
| --- | --- | --- |
| 平台管理员 | 创建 immutable profile version、启停 profile、绑定 world definition、查看聚合审计 | 读取用户人格原文、通过 profile 写入世界状态、绕过预算/SQL guard |
| owner 用户 | 后续只可在管理员允许的交集内选择展示档案；看到名称、能力、隐私和自身预算摘要 | 看 provider、group、账号、密钥、真实成本、他人选择或调用正文 |
| Agent/模型 | 读取已脱敏 Observation；返回严格 `agent-decision-v1` 候选 | 调路由、换模型、扩大 token、调用工具、写数据库、决定奖励/处罚 |
| runtime worker | 领取请求、预留预算、调用一个已解析 adapter、记录 hash/计量、交给 finalizer | 持有开放式 provider 配置、在事务中做网络调用、跳过 reducer |
| PostgreSQL | 约束不可变 version、snapshot 对应、binding 状态、worker/cfg mutation gate | 信任前端或 provider 的“成功/用量”声明 |

`route_ref` 是受控内部引用，不是 URL：deterministic profile 仅允许 `system.fake.deterministic`；未来 Sub2API route 仅允许引用一个已存在、活动的内部 group ID。profile 从不储存、返回或拼接 secret、endpoint、cookie、account ID 或 API key。

---

## 3. 模型档案（Model Profile）

### 3.1 不可变 version

每个 profile code 可以有多个 append-only version。version 是完整的执行合同，包含：

```text
profile_code, profile_version, display_name
provider_code, provider_class, route_ref, platform_group_id, model_identifier
allowed_agent_definition_codes
request_schema_version, response_schema_version
temperature, max_input_tokens, max_output_tokens, timeout_ms
max_concurrency, retry_limit
hourly request/token caps: profile / world / agent / owner
circuit breaker failure threshold / cooldown
privacy_class, retention_policy, fallback_policy
profile_hash, created_by_user_id, created_at
```

`profile_hash` 覆盖上述所有可执行字段。`display_name` 也归入 version，使审计页面能解释当时给用户展示的名称。version 绝不 UPDATE/DELETE；改模型、预算、路由、隐私或任何语义字段只能创建下一个 version。

`allowed_agent_definition_codes` 直接使用已经封存的 definition code（当前为 `system.root`、`system.npc_manager`、`character.npc`、`character.user`）。它不是给模型追加权限的手段；最终 action 权限仍以 world 的 Agent policy binding 和 Observation action context 为准。

### 3.2 可变 head 只是选择指针

`city_realtime_agent_model_profile_heads` 仅保存某 code 的当前可用 version 与状态：

- `active`：将来 binding 可引用；已排队 request 仍使用自己的 snapshot；
- `disabled`：不再租约新的调用；不会改写已完成历史；
- `retired`：不能建立新 binding，已存在 binding 需显式替换或 disable。

head 不是历史权威来源。request 和 attempt 均保存 profile code/version/hash，因此 profile head 指向变化不会污染已发出的工作。

### 3.3 初始 deterministic profile

迁移发布 `system.fake.deterministic@1`：

- `provider_code = fake.deterministic`，`provider_class = deterministic`；
- 仅 `route_ref = system.fake.deterministic`，无 group、无网络、无账户、无 secret；
- 只用于现有 A2/A3 的稳定测试/辅助 adapter；
- 显式 profile 化后，新的 world 不再依赖服务层“默认硬编码 provider”。

该 profile 不代表真实大模型已经可用，也不得被 UI 伪装为第三方模型。

---

## 4. World Binding 与可见性

### 4.1 Binding revision

`city_realtime_agent_model_profile_world_bindings` 以 `(world_id, agent_definition_code, binding_version)` 追加保存选择历史：

- 同一 world + definition 最多一条 `active` binding；
- 新 binding 必须引用 `active` profile head 的精确 version/hash；
- 替换时旧 binding 只能从 `active` 变为 `superseded` 或 `disabled`，不能改写 code/version/hash；
- binding 写入 audit actor、source 与时间；后续可增加 owner selection 的独立 override，而不能直接覆盖 world binding。

新 realtime world 在 genesis 时为已知 definition 创建 deterministic bindings。旧 world 不回填，继续走 legacy deterministic compatibility path；它们永远不会因升级自动调用外部模型。

### 4.2 解析顺序

未来 request 的 profile 解析顺序固定为：

1. 已封存、且适用于该 Agent definition 的 owner override（A4.3 后）；
2. world active binding；
3. 仅旧 world 兼容的 `legacy.fake.deterministic`；
4. 无 profile → 不生成外部调用，记录可解释的 runtime-unavailable/assisted 状态。

普通用户仅能得到经过 group、world binding、definition 和 profile head 同时过滤的安全 projection。A4.0 只实现管理员配置与 owner-safe 读取基础，不提前承诺浏览器可选择任意 profile。

---

## 5. Request / Attempt Snapshot 与生命周期

### 5.1 Request snapshot

在 sealed frame 中创建 `city_realtime_agent_decision_requests` 时，同步写入：

```text
model_profile_code, model_profile_version, model_profile_hash
model_budget_snapshot_hash
```

四者要么全部为 `NULL`（只允许历史 world legacy fake），要么全部有效。request guard 禁止任何后续修改。模型 profile 信息不进入 canonical state hash：canonical hash 只要求未决 request 能被重放；模型是否被调用与何时重试属于外部 I/O，不得改变时间线。已接受的 Decision/Intent 仍是权威、可回放事实。

### 5.2 Attempt snapshot

每个 attempt 复制 request 的 exact profile snapshot，再记录：

```text
provider_code, request_hash, response_hash
reserved_input_tokens, reserved_output_tokens
provider_input_tokens?, provider_output_tokens?, latency_ms?
failure_class?, error_code?
```

attempt 是 worker audit，使用 append-only `started → succeeded|failed`。provider usage 只是观测，不能反向修改预算保留值、城市货币、角色状态或结果。

### 5.3 失败处置

- 禁用/不匹配/超预算/熔断：不租约或以确定性的 terminal/deferred 行为结束；不偷偷换模型；
- 网络、429、5xx：由 A4.2 的 classified transient 错误路径重试，采用固定上限的指数退避；
- JSON/schema/权限/content/precondition：terminal，不重试；
- 已 lease 的 worker 在超时后由 lease recovery 接管；同一 request 最终最多一条 terminal decision、至多一条 Agent intent；
- provider 在 transaction 外执行；finalizer 重新读取 request、Observation、Agent、precondition 和 action context。

---

## 6. 预算、并发与熔断

### 6.1 保守预留

预算不信任 provider 自报。开始一个 attempt 前，worker 以 profile 的 `max_input_tokens + max_output_tokens` 作为保守上限，同时写入四个 hourly window：

| scope | scope key |
| --- | --- |
| profile | `profile_code@version` |
| world | `world_id` |
| agent | `world_id/agent_code` |
| owner | `world_id/user_id`（system Agent 无 owner 时不写） |

窗口为 UTC 的整小时、按 profile version 隔离，更新使用同一数据库事务和 request worker gate。尝试一旦已经开始，预留不会因网络异常而回退，避免“失败重试无限免费”。后续可从 attempt ledger 重建窗口；窗口只用于运营限额，不写入 canonical state。

### 6.2 并发与熔断

- `max_concurrency` 统计同 profile version 的 started attempt；
- profile 按连续可归因 provider failure 维护 `closed / open / half_open`；
- `open` 期间不创建新的 provider attempt，直到 cooldown；`half_open` 至多一个 probe；
- schema、预算、配置或 precondition 错误不记为 provider failure；
- circuit breaker 只延迟该 profile 的外部调用，不能停止 due reducer 或关闭 world。

### 6.3 城市货币隔离

模型运行时帐本不等于：

- Sub2API 余额、支付订单、用户实际账单；
- 自定义虚拟货币、游戏奖励、城市内部 city credit；
- 账号池或分组吞吐统计。

任何未来“角色获得货币”都必须走单独的资格 policy 与 reward outbox，不能由 usage window、模型 response 或 profile 事件触发。

---

## 7. Provider Adapter 契约（A4.1 起）

运行时只认一个内部 interface：

```go
type CityRealtimeAgentDecisionProvider interface {
    ProviderCode() string
    Execute(ctx context.Context, request CityRealtimeAgentProviderRequest) (CityRealtimeAgentProviderResponse, error)
}
```

请求对象只带已脱敏 Observation、固定 profile snapshot、trace/request hash、timeout 与 output schema；没有 DB handle、API key、world mutation function 或任意 tool。

返回对象只带 strict JSON decision envelope、可选可信 usage 与 provider error class。任何文本外壳、Markdown、工具调用、坐标、未知字段或不是 `agent-decision-v1` 的内容均由 adapter/finalizer 拒绝。provider adapter 不能直接调用 reducer。

首个真实 adapter 已通过专用内部 gateway transport 实现，并满足：

- profile 只引用已激活的 Sub2API group + model identifier；
- 运行时身份为进程内 `city-realtime-agent` workload，不存在浏览器、用户 API Key、JWT、Gin context 或 loopback HTTP；
- 账号选择仍由现有 scheduler 负责，Agent profile 不可选择具体账户；
- scheduler 选择后还会获取并在请求结束时释放账号并发 slot；不能获得 slot 时归类为可重试的 provider unavailable；
- API-key transport 固定为 non-streaming OpenAI Chat Completions，只发送服务端构造的 system/user message、模型映射后的 model、profile 温度/输出上限及最小认证头；不会透传用户请求头、浏览器 metadata、Cookie、session、工具或 header override；
- Agent Identity transport 只接受 `PlatformOpenAI + OAuth + auth_mode=agentIdentity` 且 runtime ID、私钥、task ID、ChatGPT account ID 均已完整 provision 的账号。它直接向固定 `chatgpt.com/backend-api/codex/responses` 发送 server-owned `store:false`、SSE Responses 请求，以每次新签名的 `AgentAssertion` 认证；不会发送或读取 OAuth bearer token、用户 API Key、浏览器/Gin context，也不会自动注册/更新 task。缺失或过期 task 直接归为 configuration，由既有受管账号生命周期修复；
- Agent Identity 的 ChatGPT Codex endpoint 不接受 `temperature` 和 `max_output_tokens`。该 transport 只接受 `temperature=0` 的 profile，绝不静默丢弃一个有语义的采样温度；`max_output_tokens` 仍保留为不可变审计/预算字段，并以固定 server prompt、上游受管身份 policy、按 profile 推导的本地 decision byte cap（最大 64 KiB）及 1 MiB wire cap 约束接受结果；
- API-key request 带固定 `Sub2API-CityAgent/1.0` User-Agent；Agent Identity request 使用与固定 Codex endpoint 配对的官方 Codex UA/originator/version。两者都仅带派生 `X-Client-Request-ID` 与内部 context workload marker，原始 request hash 不会直接发送到上游；
- transport 不调用 Gateway/OpenAI handler，不写普通 usage log、usage billing、用户余额、城市账本或虚拟货币；provider 文本、错误 body、endpoint、credential 和账号均不会进入 City request/attempt/player projection；
- 非 API-key/Agent-Identity、普通 OAuth、未知平台、空凭据、非法 base URL、401/403/404/405 或 schema/output 不匹配均 fail-closed；429、暂时不可达、5xx、超时和网络错误进入已有 retry/breaker 语义；
- Anthropic/Gemini/Grok/Antigravity/Bedrock 等传输尚未支持，不能因 profile 创建成功而假设它们已经可调用。

---

## 8. A4.2 决策 Worker 与安全可观测性（已实现）

`CityRealtimeAgentDecisionWorker` 是一个独立于 realtime clock scheduler 的进程内服务，默认关闭。它必须同时通过 `city_simulation_enabled` 与 `city_realtime_agent_decision_worker_enabled` 两个服务端开关才运行；启用 clock 不会隐式启用外部 Agent 调用。

Worker 只扫描满足以下条件的记录：

- V2 realtime world、world status/lifecycle 都为 `running`；
- request 与 outbox 都是 `queued`；
- `retry_not_before` 为空或已到期；
- 没有处于 `quarantined` 的管理员 dead-letter 状态；
- 每次扫描有固定 batch 上限，逐条调用已有的 `RunRealtimeAgentDecision` 边界。

每个 provider 调用有独立超时（profile 最大超时加 bounded finalizer grace），不会继承 clock scheduler 的短 sweep deadline。Worker 以 server-marked administrator context 调用，但仍经过 profile snapshot、预算、并发、breaker、lease、strict parser、precondition 和 sealed reducer 全部校验。

以下“尚未开始调用”的情况不会伪造 attempt 或失败 Frame：未注册的 process-local adapter、打开中的 breaker、暂时耗尽的模型预算。Worker 仅在数据库 worker gate 下将 request 保持为 `queued` 并写入未来 `retry_not_before`；adapter/breaker 使用短暂 defer，而预算耗尽会直接延迟到下一 UTC 小时窗口（附一秒安全边界），不会每数秒重复扫同一已耗尽 profile。这一 transition 不改变 attempt count、outbox、profile snapshot、canonical state 或 player projection。已产生的 provider attempt 才适用现有的 retry/failure ledger 与 breaker 规则。

管理员现有的 `GET /api/v1/admin/city/clock-health` 增加每个 world 的安全聚合字段：queued、leased、scheduled retry、quarantined、超过固定 24 小时的 stale quarantine、最早隔离时间、下一次 retry、最近的受控 error code、打开/half-open breaker 数。该投影不返回 Observation、人格、prompt、模型原文、route、group、账号、凭据、lease token 或 provider 原始错误。

### 8.1 审计化 dead-letter quarantine/release

管理员可以将一个仍为 `queued`、未租约、且 outbox 同样未租约的 request 放入 `quarantined` 状态。它不是 `failed_terminal`，也不修改 request/outbox/attempt/profile/budget/breaker 或任何 Temporal Frame；唯一效果是 worker candidate query 与 lease path 都 fail-closed 跳过该 request。原因码是闭集：`operator_review`、`provider_configuration`、`provider_incident`、`budget_review`、`world_maintenance`，不保存自由文字。

`release` 只把当前 dead-letter 标记为 `released`，不会清 `retry_not_before`、不会立即调用 provider，也不会重新创建 request。若管理员希望恢复一个仍处于未来 retry deadline 的 request，必须再单独调用已有 retry wake，这保证 release 与执行恢复各有 idempotency receipt。当前状态表与 append-only event 表都被 PostgreSQL administrator operator gate 约束；直接 SQL insert/update/delete 被拒绝。单 world 管理队列仅返回安全的 `dead_letter_status` 与闭集 reason code；管理员可从单 request、keyset-paginated event API 读取 event id、类型、reason、actor id 与时间，不能枚举全局审计记录。

管理员控制台 `/admin/city/agent-runtime` 只消费这些安全投影：先选择一个 realtime world，再按有限 queue scope 浏览最多 50 条 request。行操作只会显示 `查看隔离事件`、满足状态机前置条件的 `隔离`、`解除隔离` 和 `唤醒重试`；所有写操作仍经过 server-side idempotency 与 operator gate。页面绝不显示 Observation、提示词、模型输出、route、group、账号、credential、账本或原始 provider 错误。陈旧隔离告警使用 health projection 的固定阈值，而不是浏览器自行推导时间或持久化告警状态。

## 9. 管理与用户 API

管理员 API（A4.0）：

```text
GET  /api/v1/admin/city/agent-model-profiles
POST /api/v1/admin/city/agent-model-profiles
PATCH /api/v1/admin/city/agent-model-profiles/{profile_code}/head
GET  /api/v1/admin/city/worlds/{world_id}/agent-model-bindings
POST /api/v1/admin/city/worlds/{world_id}/agent-model-bindings
GET  /api/v1/admin/city/agent-decision-queue?world_id={world_id}
GET  /api/v1/admin/city/worlds/{world_id}/agent-decision-queue/{request_code}/dead-letter/events
POST /api/v1/admin/city/worlds/{world_id}/agent-decision-queue/{request_code}/retry
POST /api/v1/admin/city/worlds/{world_id}/agent-decision-queue/{request_code}/dead-letter
POST /api/v1/admin/city/worlds/{world_id}/agent-decision-queue/{request_code}/dead-letter/release
```

所有写 API 仅接受 structure fields，使用 `Idempotency-Key`，且返回 profile/binding hash。它们不接收 secret、URL 或自由 prompt。

用户 API（A4.3）：只展示可选 profile 的 label、能力标签、privacy class、最大节奏与当前个人预算摘要。任何 profile 选择是新 binding revision，只影响未来 request；执行中的 request 仍保持原 snapshot。

---

## 10. 数据库防线

1. Profile version 只能在 config mutation gate 内 INSERT，禁止 UPDATE/DELETE。
2. Profile head 与 world binding 的状态迁移也要求 config gate；binding 的实质字段不可改写。
3. Request/attempt 的 profile snapshot 必须完整匹配 profile version hash，legacy 四字段全 NULL 才被允许。
4. 尝试记录的 `provider_code` 必须与 request snapshot 的 profile provider 一致；legacy 仅允许 fake provider。
5. usage window / attempt budget ledger 只接受当前 request worker gate；直接 SQL 不能自行“消耗”或“释放”预算。
6. Profile 不能引用 deleted/disabled group，真实 adapter 启动前还会验证 group 与 model 可调度性。
7. dead-letter 仅允许 queued/unleased request 的 `quarantined ↔ released` 受 gate 状态迁移，事件表只可 append；release 不能暗中执行或 wake request。
8. 失败、预算、熔断、dead-letter 和 usage 表不进入 realtime canonical state；任何 world effect 仍由 sealed Intent/reducer 独立验证。

---

## 11. 验收矩阵

| 验收 | 必须证明 |
| --- | --- |
| immutable version | 直接 SQL update/delete 被拒绝；新 version hash 不同且旧 request 保持原 snapshot |
| genesis binding | 新 realtime world 为每种已知 definition 绑定 deterministic profile；旧 world 不被回填 |
| request consistency | 创建 request 后 profile 停用/替换不会改 request snapshot；attempt 复制 exact code/version/hash |
| provider isolation | fake runner 拒绝 external profile；系统 gateway adapter 不经 Gin/loopback/user API key，且只接受已选择的 OpenAI-compatible API-key 或已 provision Agent Identity account；普通 OAuth 必须被 scheduler 排除 |
| budget | 超并发、任一 hourly cap、retry cap 均在网络调用前拒绝；window 只能经 worker gate 写入 |
| breaker | 连续 transient failure 打开 breaker；cooldown 之前没有新 attempt；逻辑/schema 失败不污染 breaker |
| worker deferral | adapter 未注册、breaker 冷却或临时预算不足时 request 保持 queued 并获得未来 retry deadline，attempt/outbox/canonical state 均不变 |
| dead-letter quarantine | 管理员只可隔离 queued/unleased request；worker 与直接 lease 都拒绝隔离项；release 不执行、不清 retry，且两次操作不写 Frame/attempt/outbox |
| worker isolation | worker 仅在两个 server setting 同时启用时运行；关闭任一开关不扫描、不租约、不调用 provider |
| health projection | admin 只能看到计数、时间和闭集 error code；成员与公开 projection 看不到任何运行时运维信息 |
| stale quarantine | 超过固定阈值的隔离项只在 admin health/console 中聚合可见；控制台不得暴露 request payload 或通过 UI 直接执行 provider |
| authorization | 非 admin 无法管理 profile 或 binding；owner/outsider 不看到 route、group、provider secret、其它用户 usage |
| replay | profile/usage/attempt 行增删不会重写历史 frame/state hash；已完成 Decision/Intent 仍可回放 |

---

## 12. 后续切片

1. **A4.1 外部传输扩展**：在不复用 HTTP handler、浏览器身份或普通用户 API Key 的前提下，继续为 Anthropic/Gemini/Grok/Antigravity/Bedrock 等受支持平台增加各自独立的 system transport；每个平台先提供 request/response parser、最小 header contract、account capability gate、失败分类和真实上游 fixture，再允许 profile 使用。OpenAI Agent Identity 已完成第一条实现，但仍需在生产受管账号上做非破坏性 fixture 验证。
2. **A4.3**：角色 owner 的受限 profile selection、最小配置页面与 privacy-safe usage projection；选择只能形成新的 binding revision，不能改写已排队 request。
3. **A5**：NPC Manager/NPC Character Agent、LOD，只有在 A4 调用边界验证完成后才开放。
