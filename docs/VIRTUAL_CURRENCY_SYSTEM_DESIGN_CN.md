# 可扩展虚拟货币与权益体系设计

版本：v1.2（2026-07-18）  
适用范围：个人运营、分组化 AI 网关、后续游戏/任务/社区接入

## 1. 目标与边界

本体系解决“管理员可以定义金币/积分/代币，指定哪些分组可以使用，用户通过不同渠道获得并消费”的问题。它是一个通用权益账本，不是支付系统，也不替换现有 USD 余额、订阅、支付订单或返利余额。

核心目标：

- 每种货币独立定义：代码、名称、符号、精度、状态和展示信息均可配置。
- 货币与分组建立显式策略关系：同一货币可以被多个分组使用，每个分组分别控制启用、获得和消费权限。
- 所有余额变动都有不可变流水、来源、操作者和幂等键；余额只是可快速读取的物化结果。
- 管理员发放、用户消费、兑换码、任务奖励、返利、游戏回调都通过同一套“入账/扣账”内核，避免各模块各自改余额。
- 未来扩展不把游戏业务写死在网关中：游戏只实现发奖/扣款适配器，货币核心负责授权、一致性和审计。

非目标：

- 第一阶段不接入银行卡、Stripe、支付宝等法币支付。
- 不把虚拟货币折算成 USD，也不允许虚拟货币直接增加用户 USD 余额。
- 不允许客户端通过伪造来源、重复请求或修改金额获得货币。

## 2. 领域模型

### 2.1 货币定义 `virtual_currencies`

| 字段 | 规则 |
| --- | --- |
| `code` | 全局唯一、`[a-z][a-z0-9_-]{1,31}`，作为 API 稳定标识；一旦有流水不允许改名 |
| `name` | 管理员可修改的展示名称，例如“金币” |
| `symbol` | 展示符号或短标签，例如 `🪙`、`G` |
| `scale` | 0–8；使用整数最小单位保存，`scale=0` 时 1 单位就是 1 金币 |
| `status` | `active` / `disabled`；停用只阻止新业务，不删除历史流水 |
| `metadata` | JSON 扩展字段，不能承载权限或余额 |

金额永远使用 `int64 amount_units`，例如 `scale=2` 的 12.34 记录为 1234。禁止 Go `float64` 参与记账计算，前端只负责格式化。

### 2.2 分组策略 `virtual_currency_group_policies`

货币本身不直接保存“可用分组数组”，而是使用独立策略表：

- `enabled`：该分组是否可见、可参与该货币体系。
- `can_earn`：允许该分组作为奖励/发放上下文。
- `can_spend`：允许用户以该分组上下文消费。
- `max_balance_units`：可选的用户余额上限，防止活动配置错误导致无限发放。

消费请求必须携带 `group_id`。服务端同时检查：货币处于启用状态、策略启用且允许消费、用户当前具备该分组权限。只有管理员发放才可以在没有用户 API Key 的情况下操作，但仍必须绑定一个有效分组策略。

### 2.3 钱包 `virtual_currency_wallets`

每个 `(user_id, currency_id)` 只有一个钱包：

- `available_units`：可直接消费余额。
- `reserved_units`：已被订单/游戏购买暂时锁定的余额。
- `version`：乐观并发版本，和数据库行锁一起用于发现异常更新。

钱包不是事实来源；删除或重建钱包时可以由流水重新校验。

### 2.4 封账交易与复式分录

`virtual_currency_journals` 是交易事实头，`virtual_currency_postings` 是同一交易下的复式分录。每次业务变动先创建草稿 journal，写入至少两条 posting，确认分录合计为零后设置 `posted_at` 封账；整个过程与钱包更新处于同一数据库事务。

当前逻辑账户包括：

- `user_available`：用户可用余额。
- `user_reserved`：用户预留余额。
- `system_issuance`：发行来源账户。
- `system_sink`：消费回收账户，退款表现为其反向分录。
- `system_adjustment`：人工纠错或尚未细分的系统对手账户。

数据库使用延迟约束触发器在提交时强制每个 journal 已封账、分录数不少于 2 且 `SUM(amount_units)=0`。journal 一经封账，journal、posting 与兼容流水都禁止更新和删除；用户、分组、操作者外键改为 `RESTRICT`，必须通过软删除或显式匿名化流程保留审计身份。

### 2.5 兼容流水 `virtual_currency_ledger_entries`

该表继续作为面向用户和管理员的余额变化视图，并通过不可空 `journal_id` 关联复式总账。它不是守恒校验的唯一来源，也不提供更新接口；纠错只能追加冲正记录。记录包含：

- 货币、用户、分组上下文。
- `delta_units`：经济余额变化，发放为正，消费为负，预留/释放为 0。
- `available_delta_units`、`reserved_delta_units` 及变动后的两个余额，用于解释预留流程。
- `entry_type`：`grant`、`spend`、`reserve`、`commit`、`release`、`refund`、`expire`、`adjustment`。
- `source_type` / `source_id`：`admin`、`redeem_code`、`mission`、`referral`、`game`、`api` 等可扩展来源。
- `idempotency_key`：同一用户/货币/业务操作不可重复入账。
- `reason`、`metadata`、`created_by`、`created_at`。

“撤销”不是删除原流水，而是用 `refund` 或 `adjustment` 追加反向流水并引用原 `source_id`。这样可以审计“谁、何时、为何”改变余额。

### 2.6 预留 `virtual_currency_holds`（已实现）

游戏购买、抽奖和异步订单不能直接扣账，否则客户端中断会产生误扣。预留记录包含金额、状态、过期时间和幂等键：

1. `reserve`：可用余额减少，预留余额增加。
2. `commit`：预留余额减少，经济余额减少。
3. `release`：预留余额减少，可用余额恢复。
4. 超时任务把仍为 `active` 的预留转成 `release`。

当前已实现用户侧 reserve/commit/release API、过期保护和 `expire` 分录。预留写入、钱包变动和账本写入在同一数据库事务中完成；结算时会锁定 hold 行，commit 过期会拒绝，release 对过期 hold 会记录为 expire。管理员维护接口可以按批次扫描并原子释放过期 hold，适合由外部 cron/任务调度器周期调用；扫描不会删除历史 hold 或静默改写账本。

## 3. 余额一致性与并发规则

所有变动必须在一个数据库事务中完成：

1. 标准化并校验货币、用户、分组和金额。
2. 查询或创建钱包，并对钱包行执行 `FOR UPDATE`。
3. 检查策略、余额下限、余额上限和钱包版本。
4. 按 `available_units + delta` 更新钱包，同时递增 `version`。
5. 创建 journal，写入兼容流水及用户/系统两侧 posting。
6. 封账；数据库在提交点验证分录数量、守恒和历史不可变约束。
7. 唯一幂等键命中时读取原流水并返回原结果，否则提交事务。

关键不变量：

- `available_units >= 0`、`reserved_units >= 0`。
- 任何用户/货币钱包的所有有效流水累计结果必须等于钱包余额。
- 消费金额必须大于 0；外部请求不能传入负数绕过消费校验。
- 货币停用后不能发放、消费或预留，但历史查询和管理员审计仍可用。
- 同一幂等键复用时，请求指纹不一致必须返回冲突，不允许以新金额覆盖旧操作。
- 每个已提交 journal 必须满足 `posting_count >= 2` 且分录合计严格为 0。
- journal、posting 和兼容流水只能追加；封账后不能补写、覆盖或删除。

## 4. 获取渠道与统一适配器

所有“获取”渠道最终调用同一个 `Grant` 能力，不直接修改钱包：

| 渠道 | 入口 | 信任边界 |
| --- | --- | --- |
| 管理员发放/扣回 | 管理端 API | 管理员审计 + 幂等 |
| 兑换码 | 现有 redeem 体系扩展 `currency_code` | 一次性消费 + 过期 + 原子锁 |
| 每日任务/成就 | 任务服务产生奖励事件 | 任务完成事件幂等 |
| 邀请/返利 | affiliate 结算事件 | 结算状态机，禁止重复结算 |
| 游戏 | 游戏服务端签名回调 | HMAC、时间窗、nonce、来源白名单 |
| 活动/补偿 | 活动批次任务 | 批次号 + 用户号唯一约束 |

建议的内部接口：

```go
type CurrencyEarnProvider interface {
    Grant(ctx context.Context, input VirtualCurrencyGrantInput) (*VirtualCurrencyLedgerEntry, error)
}

type CurrencySpendProvider interface {
    Spend(ctx context.Context, input VirtualCurrencySpendInput) (*VirtualCurrencyLedgerEntry, error)
}
```

当前已落地 `Grant` / `Spend` 以及 hold 结算契约。适配器只负责验证自己的业务事件，随后传入稳定的 `source_type`、`source_id` 和 `idempotency_key`；货币服务负责余额、分组策略、幂等和审计。这样接入游戏时不需要把游戏订单表、道具表、货币流水混为一张表。

推荐的奖励事件映射如下：

| 业务模块 | `source_type` | `source_id` 建议 | 幂等键建议 |
| --- | --- | --- | --- |
| 每日任务 | `mission` | `mission:{rule_id}:{user_id}:{date}` | 与 `source_id` 相同 |
| 邀请奖励 | `referral` | `referral:{invite_id}:{milestone}` | 与结算批次组合 |
| 游戏任务 | `game` | `{game_id}:{season}:{event_id}:{user_id}` | 外部事件 ID + 用户 ID |
| 活动批次 | `activity` | `{campaign_id}:{batch_id}:{user_id}` | 批次 ID + 用户 ID |

客户端可以展示奖励结果，但不能直接调用发放契约。游戏和第三方服务应使用后续的签名集成入口；在入口完成前，建议由服务端任务/回调消费者调用 `CurrencyEarnProvider`。

## 5. API 设计

### 5.1 管理员

```text
GET    /api/v1/admin/currencies
POST   /api/v1/admin/currencies
GET    /api/v1/admin/currencies/:id
PATCH  /api/v1/admin/currencies/:id
POST   /api/v1/admin/currencies/:id/status
GET    /api/v1/admin/currencies/:id/groups
PUT    /api/v1/admin/currencies/:id/groups/:group_id
DELETE /api/v1/admin/currencies/:id/groups/:group_id
POST   /api/v1/admin/currencies/:id/holds/expire?limit=100
GET    /api/v1/admin/currencies/:id/reconciliation?limit=20
POST   /api/v1/admin/currencies/:code/adjustments
GET    /api/v1/admin/currencies/:code/users/:user_id/ledger
```

货币定义示例：

```json
{
  "code": "gold",
  "name": "金币",
  "symbol": "🪙",
  "scale": 0,
  "description": "活动和游戏通用金币",
  "metadata": {"color": "amber"}
}
```

发放/扣回示例：

```json
{
  "user_id": 42,
  "group_id": 7,
  "amount_units": 100,
  "entry_type": "grant",
  "source_type": "admin",
  "source_id": "manual-20260717-42",
  "idempotency_key": "grant-gold-42-20260717-001",
  "reason": "活动补偿"
}
```

### 5.2 用户

```text
GET  /api/v1/user/currencies
GET  /api/v1/user/currencies/:code/ledger
POST /api/v1/user/currencies/:code/spend
POST /api/v1/user/currencies/:code/reservations       # 第二阶段
POST /api/v1/user/currencies/:code/reservations/:id/commit
POST /api/v1/user/currencies/:code/reservations/:id/release
```

用户消费只能减少自己的余额；仍必须提供 `group_id`、业务来源和幂等键，服务端会检查分组权限。预留接口会返回 hold 和对应 ledger 分录，结算请求只能操作自己的 hold。游戏服务端应走专用签名接口，不让客户端直接提交游戏奖励。

### 5.3 游戏/外部服务回调（已实现）

```text
POST /api/v1/integrations/virtual-currency/mutations
```

请求头：

- `X-Integration-Code`
- `X-Integration-Timestamp`：Unix 秒时间戳，默认允许 ±5 分钟。
- `X-Integration-Nonce`：16–128 位随机值，默认 10 分钟内不可重复。
- `X-Integration-Signature`：HMAC-SHA256 十六进制值，也接受 `sha256=` 前缀。

签名原文为：

```text
UPPER(METHOD) + "\n" + PATH + "\n" + TIMESTAMP + "\n" + NONCE + "\n" + RAW_BODY
```

请求体的 `operation` 支持 `grant`、`spend`、`reserve`、`commit`、`release`。其中 `commit` / `release` 使用 `hold_id`，其它操作使用 `currency_code`、`user_id`、`group_id` 和 `amount_units`。每个接入只能访问管理端配置的货币-分组 scope，并分别受 `can_earn`、`can_spend`、`can_settle` 控制；请求体不能提升权限。

每个请求必须提供稳定的 `source_id`，用于外部事件追踪；`idempotency_key` 在同一接入下跨 operation 共享命名空间，不能用同一个键伪造另一种操作。服务端只接受上述固定路径，并在 Redis 可用时按客户端 IP 限制为每分钟 120 次；限流存储故障时该外部入口 fail-close。

管理员接入管理 API：

```text
GET    /api/v1/admin/currency-integrations
POST   /api/v1/admin/currency-integrations
GET    /api/v1/admin/currency-integrations/:id
PATCH  /api/v1/admin/currency-integrations/:id
POST   /api/v1/admin/currency-integrations/:id/status
POST   /api/v1/admin/currency-integrations/:id/rotate-secret
GET    /api/v1/admin/currency-integrations/:id/scopes
PUT    /api/v1/admin/currency-integrations/:id/scopes/:currency_id/:group_id
DELETE /api/v1/admin/currency-integrations/:id/scopes/:currency_id/:group_id
```

完整密钥只在创建或轮换时返回一次；数据库保存应用层加密后的密文和末尾指纹。轮换密钥会立即使旧密钥失效。管理端已提供接入创建、轮换、停用、scope 配置和一次性密钥复制界面。

## 6. 管理与运营能力

第一阶段必须具备：

- 货币定义的新增、编辑展示字段、启用/停用。
- 分组策略的批量绑定、解绑和权限切换。
- 管理员按用户发放、扣回，并显示余额变动前后值。
- 用户余额、预留余额、流水筛选（货币、分组、来源、时间、正负方向）。
- 幂等冲突、余额不足、分组无权限等可读错误码。
- 过期 hold 批量扫描、钱包/账本只读对账，以及可展示的差异样本。

后续增加：

- 任务/成就规则、每日上限、活动批次和发放预览。
- 游戏商品目录、价格快照、订单、退款和对账。
- 货币仪表盘：发行量、消费量、流通余额、沉淀余额、来源占比和异常用户。
- 余额变动 webhook / SSE，仅推送脱敏后的资产事件。

## 7. 安全、风控与反作弊

- 所有外部接口必须有集成级最小权限，不信任客户端传来的 `source_type`、`group_id` 和 `currency_code`。
- 管理员调整必须经过现有审计中间件；敏感的大批量调整可复用 step-up TOTP。
- 发放设置单次、单用户、单批次上限，并支持日/月限额。
- 游戏回调必须 HMAC + 时间窗 + nonce + 幂等键，密钥只保存哈希或加密密文。
- 未知接入代码统一返回签名失败；签名路径固定，避免接入枚举和跨路径重放。
- 集成入口采用 Redis 客户端 IP 限流并 fail-close；客户端重试必须复用原幂等键。
- 记录客户端 IP、用户代理、集成 ID、请求指纹和关联订单，但禁止写入 API Key、Bearer Token 等秘密。
- 检测短时间内异常领取、同设备多账号、同一游戏事件跨账号重放；检测结果先进入风险事件，不在账本层偷偷改余额。
- 货币停用、策略解绑和用户封禁只影响新交易，不破坏历史可追溯性。

## 8. 规模承载评估与升级边界

### 8.1 当前实现能够承担的角色

当前实现已经具备正确平台资产内核所需的关键能力：整数最小单位、钱包行锁、事务内钱包投影、封账 journal、复式 posting、请求指纹幂等、余额/预留非负约束、分组权限、hold 状态机、签名接入和只读对账。并发写入分散在 `(user_id, currency_id)` 钱包行上，不存在单一全局余额锁，因此适合承担奖励、兑换、道具消费、活动发放和普通游戏结算。

对账接口同时返回钱包投影、用户 posting、发行/回收账户、journal/posting 数量、投影差额和全账守恒差额。总量以十进制字符串返回，避免聚合结果经过 JavaScript `Number` 丢失精度。

但是当前没有基准压测，不能承诺具体 QPS、用户数或流水规模。它是“功能闭环、并发正确”的第一版，不是已经验证过的超大规模资产平台。

### 8.2 不能直接承担的角色

当前已经是复式总账，但业务写入契约仍围绕“单用户钱包 ↔ 系统账户”以及同钱包可用/预留迁移；尚未开放家庭、企业、政府、银行等任意主体账户，也没有证券持仓、订单撮合和跨主体原子转账。因此以下业务不能直接把平台钱包表当作城市经济底层模型：

- 城市中家庭、企业、政府、银行之间的大量资金和商品流转。
- 股票订单、资金/持仓预留、成交、分红、增发和清算。
- 玩家间交易所、兑换市场、借贷、利息、做空或杠杆。
- 需要证明全系统资产负债平衡的金融业务。

这些系统应拥有自己的复式账本和领域状态，只把经服务端验证的最终奖励或消费事件接入平台虚拟货币。

### 8.3 大规模上线前的硬化项

| 优先级 | 当前限制 | 必须升级的能力 |
| --- | --- | --- |
| P0 | 游戏奖励只能同步逐笔调用 | 增加事务 Outbox/Inbox、后台重试、死信和端到端关联 ID |
| P0 | 接入端点固定按来源 IP 每分钟 120 次 | 改为集成级可配置限额，并提供受限批量 mutation 或队列消费者；保留 IP 防护作为第二层 |
| 已完成 | 资产历史可能被级联删除或事后覆盖 | 用户/分组/操作者改为限制删除；journal 封账后 journal、posting 和兼容流水由数据库拒绝更新/删除 |
| P0 | 没有可复现容量数据 | 增加并发同钱包、分散钱包、重复幂等、hold 结算和故障恢复压测，按目标 VPS 给出 P95/P99 |
| P1 | 列表使用 `COUNT(*) + OFFSET` | 大流水查询改为基于 `(created_at, id)` 的游标分页，统计异步物化 |
| P1 | 对账使用全量 `DISTINCT ON` 扫描 | 增加按钱包检查点/分片的增量对账和离线全量审计 |
| P1 | 单表持续追加 | 达到阈值后拆出幂等请求表，并按时间或哈希分区账本；建立在线保留和归档策略 |
| P1 | 缺少资产业务指标 | 增加 mutation 吞吐、钱包锁等待、幂等命中、失败码、账本延迟、hold 过期和对账差异指标 |
| P1 | 当前写入契约仍是单钱包与系统账户结算 | 出现玩家转账时增加单 journal、多钱包确定顺序加锁的原子转账契约，禁止用两次独立增减模拟转账 |
| P2 | 同步依赖单 PostgreSQL 主库 | 规模证明需要时再增加只读副本、分片路由或独立资产服务，不提前拆微服务 |

### 8.4 容量决策门槛

是否拆分资产服务不按“用户看起来很多”判断，而按以下指标触发：

- 钱包行锁 P99、数据库提交 P99 或连接池等待持续超过目标。
- 账本索引和 VACUUM 已无法在维护窗口内稳定完成。
- 对账、统计或归档明显干扰在线 mutation。
- 单部署故障域不再满足奖励/消费业务的恢复目标。

在触发前保持 PostgreSQL 单一事实来源更容易保证一致性；先完成异步接入、游标分页、增量对账和压测，再决定分区或服务拆分。

## 9. 迁移与上线策略

1. 已创建货币、策略、钱包、账本、hold、集成和兑换码扩展表，不迁移现有 USD 余额。
2. 仅启用管理员创建的货币；新环境不自动生成业务货币，避免误发放。
3. 先上线查询和管理员发放，再上线用户消费；观察账本与钱包校验指标。
4. 兑换码已通过现有 Ent 事务接入；任务、返利和游戏继续逐个接入适配器，每个渠道都必须有独立幂等测试。
5. 管理端提供过期 hold 扫描和“钱包/账本对账”只读校验；发现不一致时由运营告警并人工处理，禁止自动静默修复。

## 10. 本次实现范围与验收

本次已实现的核心垂直切片：

- 自定义货币定义与状态管理。
- 货币-分组策略绑定。
- 钱包物化余额和不可变流水。
- 已封账 journal 与复式 posting；发行、消费、退款、调整、预留、提交、释放和过期均使用同一守恒内核。
- 数据库提交时验证 journal 已封账、至少两条分录且合计为零，并拒绝事后更新、补写或删除历史。
- 管理员发放/扣回、用户余额/流水查询、用户消费。
- 数据库事务、钱包行锁、唯一幂等键、余额下限和策略校验。
- 管理端 API 和用户端 API，前端保留后续接入入口。
- reserve/commit/release 预留结算闭环及过期状态保护。
- 过期 hold 批量维护接口，释放时将预留金额恢复到可用余额并追加 `expire` 分录。
- 钱包/账本只读对账接口，支持总数、差异数、受限样本、发行/回收统计、投影差额和全账守恒差额。
- HMAC、时间窗、nonce、scope 和幂等保护的游戏/外部服务接入端点。
- 集成入口 Redis 客户端 IP 限流和 fail-close 策略。
- 虚拟货币兑换码生成、一次性消费与余额发放的跨 Ent 事务原子性。
- 面向任务、邀请、活动和游戏适配器的 `CurrencyEarnProvider` / `CurrencySpendProvider` 契约，统一从 `Grant` / `Spend` 进入账本。

验收条件：

- 相同幂等键重复请求只产生一条流水，并返回同一结果。
- 并发消费不会出现负余额或丢失更新。
- release/expire 会恢复可用余额，commit 会消费预留余额；三条路径均有集成测试覆盖。
- 未绑定分组或无消费权限时返回拒绝，不能通过改请求体绕过。
- 停用货币不能新发放/消费，但历史流水仍可查询。
- 流水累计值与钱包余额一致，所有金额均为整数最小单位。
- 每条流水都关联已封账 journal；任一不平衡、未封账或试图篡改历史的事务都由 PostgreSQL 拒绝。
- 兑换码、任务和游戏只增加适配器与来源校验，不复制余额更新逻辑；兑换码、签名集成客户端管理和预留结算已纳入同一核心边界。

## 11. 向现实经济上层演进的依赖顺序

当前阶段完成的是 L0“可证明的货币事实层”：精确金额、幂等命令、原子钱包投影、封账复式分录、预留状态机和只读对账。下一阶段不直接堆叠股票或宏观参数，而按依赖顺序建设：

1. **L1 经济主体与科目表**：家庭、企业、政府、银行、交易所分别拥有现金、应收、应付、资本和库存账户；主体身份与平台用户解耦。
2. **L2 通用多主体交易**：一次 journal 原子锁定并更新多个账户，支持工资、采购、税收、补贴、贷款和玩家间转账；失败时整笔回滚。
3. **L3 实物与产能守恒**：资金分录之外独立记录商品、库存、土地、劳动力和产能，禁止仅改金额而凭空生成商品。
4. **L4 周期清算与经济报表**：确定性 tick、结算期间、试算平衡表、资产负债表、利润表、现金流和可回放快照。
5. **L5 银行、财政与货币政策**：只有在信用、违约、税收、政府预算和价格指数都可校验后，才加入存款准备金、利率、公开市场操作与通胀目标。
6. **L6 证券市场**：企业财务真实产生利润和现金流后，再增加股份登记、资金/证券双预留、集合竞价、成交清算、分红和公司行为。

城市内部货币仍与平台奖励货币隔离：城市总账负责模拟真实性，平台货币只接收经过防作弊和奖励上限校验的最终结算事件。这样上层模型可以持续变复杂，而不会污染用户资产事实层。
