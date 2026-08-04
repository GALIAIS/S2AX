# 共享订阅额度池与动态公平分配设计

状态：V2 实现中（多窗口；官方百分比 + Analytics credit 校准）
适用版本：SAX 分支
最后更新：2026-08-04

## 1. 背景与目标

当前订阅分组的 `weekly_limit_usd` 是“每个用户一份同额上限”。这适合独立额度，但不适合多人共用一个上游订阅：

- 分组上限被重复乘以订阅人数，可能高于上游真实可用量；
- 固定 33% 不能反映用户权重、已用量和空闲额度；
- 一个用户不用额度时，其他用户无法安全借用；
- 用户端只能看到自己的固定额度，管理员无法看清共享池整体风险；
- OpenAI/Codex Pro 的上游限制不是稳定的美元或 token 数，直接把美元换算为 token 会产生误导。

V1 的目标是建立一个以**真实已记录费用**为计量单位、以**共享池硬上限**为安全边界、以**权重公平份额**为默认分配、允许**受控借用**的控制平面。对于支持官方滚动窗口的 OpenAI/Codex 账号，V1 还支持按官方百分比作为容量单位，避免管理员猜测美元容量。它不改变现有计费价格、不改变余额模式、不伪造上游供应商的额度数据。

> 重要修订：单一 `window_seconds` 只能表达一个周期，不能正确覆盖 20x 拼车常见的短周期与长周期约束。本设计已改为可扩展的多窗口模型；默认模板包含 `short=5h` 与 `long=7d` 两个独立窗口。请求必须同时通过所有启用窗口，旧单窗口字段只作为迁移兼容和长窗口回退，不再是策略主来源。

## 2. 非目标与明确边界

V1 不做以下事情：

1. 不把 Pro 5h/7d 等上游窗口硬编码成某个固定 token 数；
2. 不把模型价格表反向当作上游官方额度；
3. 不在网关热路径调用上游 quota API；
4. 不删除现有每日/每月订阅限额；共享池只替代订阅分组的周限额检查，并可额外提供 5h、7d 或自定义窗口；
5. 不给正在进行的请求预留未知费用。预检查是 admission control，最终以 `usage_logs.actual_cost` 记账；因此极高并发下允许小幅超出软阈值，硬上限由现有限额与共享池共同保护。

上游真实额度若能由某个平台稳定获取，应通过独立的 quota adapter 保存官方观测值；没有官方滚动窗口时才使用管理员配置的保守美元等价上限。

## 3. 核心概念与不变量

### 3.1 计量口径

- 计费单位：手动模式使用美元等价的 `actual_cost`，与现有订阅扣费一致；官方百分比模式使用上游滚动窗口的 `used_percent`；
- 统计来源：`usage_logs.actual_cost`，仅统计当前共享池窗口内、指定分组且 `subscription_id IS NOT NULL` 的记录；
- token：仅用于分析展示，不能作为 V1 的强制额度单位；
- 窗口：共享池自己维护多个固定长度滚动窗口，不依赖某一个用户的首次请求时间。默认模板包含 5 小时短窗和 7 天长窗；是否启用、容量和安全线都由管理员分别配置。

### 3.2 容量与安全线

手动 USD 模式为每个窗口配置 `capacity_usd` 为该上游窗口的保守美元等价容量。官方百分比模式把上游窗口容量规范化为 `100%`，每个窗口独立计算：

```text
distributable(window) = capacity_usd(window) × (1 - reserve_ratio(window))
soft_limit(window)    = distributable(window) × soft_stop_ratio(window)
hard_limit(window)    = distributable(window) × hard_stop_ratio(window)
```

默认值：reserve 15%、soft stop 85%、hard stop 95%。因此配置 $1,000 时，默认可分配 $850、硬停止线为 $807.50；官方百分比模式对应可分配 85%、硬停止线 80.75%。剩余空间不是必须消耗的额度，而是供应商波动、迟到用量和并发超支的安全缓冲。

### 3.3 多窗口准入与权重份额

对共享池窗口开始时或当前有效的订阅用户，默认权重为 1。用户权重可由管理员调整：

```text
base_share(user, window) = distributable(window) × user_weight / Σ(active_user_weight)
```

显示给用户的“100%”应表示“已用 / 自己当前分配份额”，而不是把三人的用量相加后都显示同一个百分比。

单次请求的准入结果是所有启用窗口结果的 AND：

```text
allow(request, user) = allow(short) AND allow(long) AND allow(other_enabled_windows)
```

因此，短窗剩余但长窗耗尽时仍然拒绝；长窗充足但 5 小时突发超限时也拒绝。这是 20x 拼车不能只做周限额的关键不变量。

### 3.4 借用规则（每个窗口独立计算）

- 用户先使用自己的 `base_share`；
- `borrow_enabled=true` 且该窗口尚未达到 `soft_limit(window)` 时，可借用未使用份额；
- 单用户借用后的最大准入值为 `base_share(user, window) × borrow_multiplier`，默认 1.5；
- 任一启用窗口达到 `hard_limit(window)` 后，该请求停止；
- 借用不改变其他用户的权重，也不把已借用金额永久转移给借用者；
- 只要用户仍低于自己的 base share，即使共享池接近 soft stop，也优先允许其使用自己的份额。

### 3.5 成员生命周期

- 当前有效订阅自动成为池成员，未配置成员权重时使用 1；
- 管理员显式禁用成员时，该用户在共享池中拒绝请求，但订阅记录仍保留；
- 撤销/过期订阅不再参与新窗口分配；历史 `usage_logs` 只用于原窗口审计；
- 新增成员不追溯历史用量，加入后按当前窗口剩余容量重新计算；
- 权重修改立即影响当前窗口的“未使用份额”，不修改已用金额。

## 4. 数据模型

### 4.1 `shared_quota_pools`

一行对应一个订阅分组，是共享池的总开关和全局借用策略。它保留旧版本
的单窗口字段，作为迁移兼容和 `long` 窗口回退；运行时的主策略来自
`shared_quota_pool_windows`，不能只读取这一行来判断额度。

| 字段 | 说明 |
| --- | --- |
| `group_id` | 主键，关联 `groups` |
| `enabled` | 是否启用共享池；默认关闭，保证旧分组行为不变 |
| `window_seconds`、`capacity_usd` | 旧单窗口字段；只用于兼容迁移和长窗口回退 |
| `reserve_ratio`、`soft_stop_ratio`、`hard_stop_ratio` | 旧字段；新配置写入对应窗口 |
| `borrow_enabled` | 是否允许借用 |
| `borrow_multiplier` | 最大借用倍率 |
| `capacity_mode` | `manual_usd` 或 `official_percent`；运行时以窗口配置为准 |
| `upstream_account_id` | 可选的 OpenAI OAuth 账号；留空时仅允许分组内唯一的活跃 OpenAI OAuth 账号自动选择 |
| `upstream_capacity_usd` | 可选的上游观测容量，供校准与审计 |
| `upstream_utilization_percent` | 可选的上游观测利用率，供校准与审计 |
| `window_start/window_end` | 旧长窗口边界；新窗口边界存放在窗口表 |
| `created_at/updated_at` | 审计时间 |

总表字段仍由数据库约束保护；真正启用时，每个启用窗口还必须单独满足容量、比例和边界校验。

### 4.2 `shared_quota_pool_windows`

一行对应一个分组的独立时间窗口。默认模板是 `short=5h` 和 `long=7d`，也可以添加
任意 5 分钟到 31 天的自定义窗口。

| 字段 | 说明 |
| --- | --- |
| `group_id/window_key` | 联合主键；`short`、`long` 是约定键，其它键必须是安全 ASCII 标识 |
| `enabled` | 是否参与准入 AND 判断 |
| `window_seconds` | 当前窗口长度；每个窗口独立滚动 |
| `capacity_usd` | 该窗口的保守容量，启用时必填 |
| `capacity_mode` | `manual_usd` 使用本地费用；`official_percent` 使用官方滚动窗口百分比 |
| `upstream_account_id` | 官方百分比同步使用的上游账号，可留空自动选择 |
| `reserve_ratio` | 该窗口预留比例 |
| `soft_stop_ratio`、`hard_stop_ratio` | 该窗口的借用停止线和全局停止线 |
| `upstream_*` | 可选的上游观测值，不直接覆盖管理员容量 |
| `window_start/window_end` | 当前窗口边界；过期后由条件更新推进 |

历史单窗口配置迁移为 `long`，并创建一个默认关闭的 `short` 行，不猜测 5 小时容量。
手动 USD 模式下，管理员必须给短窗配置容量并启用后，5h 才会成为强制限制；官方百分比模式只在上游确实返回匹配的 5h 窗口时启用。

官方百分比模式不要求填写 `capacity_usd`。后台首次读取和后续刷新均异步执行；没有快照时暂时放行并标记为待同步，快照超过 15 分钟后不再作为硬限制使用。快照有效期内，官方百分比负责总池硬线，本地 `usage_logs` 只用于在成员之间按实际使用比例分摊和展示。

### 4.3 `shared_quota_pool_members`

一行对应一个分组/用户覆盖配置。

| 字段 | 说明 |
| --- | --- |
| `group_id/user_id` | 联合主键 |
| `weight` | 正数权重，默认 1 |
| `enabled` | 是否允许该用户使用共享池 |
| `created_at/updated_at` | 审计时间 |

没有覆盖行代表默认成员，不需要为每个订阅预先写入配置。

## 5. 请求链路

```text
API key 鉴权
  -> 读取订阅缓存
  -> 维护订阅日/月窗口
  -> 共享池 admission check（每个启用窗口都必须通过）
  -> 上游转发
  -> 统一计费 Apply
  -> 写入 usage_logs.actual_cost 与订阅用量
```

共享池检查位于转发前，不参与 token 首字节计时；默认 2 秒本地快照缓存只对已启用的共享池生效。数据库查询失败时 V1 采用 fail-open 并记录告警，避免可选控制面故障把所有 API 变成 503；现有分组周限额仍然作为兜底。管理员可通过控制台快照判断是否应关闭该池或恢复固定周限额。

## 6. 并发、一致性与失败处理

1. 每个窗口使用条件更新推进，实例间不会重复推进同一窗口；
2. 快照读取是只读聚合，最终扣费仍由现有原子 billing repository 执行；
3. V1 不为未知请求金额建立冻结账本，避免估算错误造成额度锁死；
4. 快照缓存 TTL 为 2 秒，降低热路径 SQL 压力。硬限制仍由 `hard_limit` 和现有订阅限额共同决定；
5. 配置非法时 API 返回 400，不写入部分配置；
6. 配置删除成员覆盖行后恢复默认权重 1；
7. 关闭共享池后，现有分组日/周/月限额立即恢复为唯一准入口径；关闭某一个窗口只移除该窗口的 AND 条件。

## 7. 管理端 API

- `GET /api/v1/admin/groups/:id/shared-quota`：读取配置、窗口、池用量、成员份额；
- `PUT /api/v1/admin/groups/:id/shared-quota`：原子替换全部窗口配置，可同时批量更新成员权重；
- `PUT /api/v1/admin/groups/:id/shared-quota/members/:user_id`：单独更新成员覆盖；
- `DELETE /api/v1/admin/groups/:id/shared-quota/members/:user_id`：删除覆盖并恢复默认成员。

管理员接口只返回邮箱/用户名、权重、用量和计算份额，不返回 API key、上游账号凭据或请求内容。

## 8. 用户展示

共享池开启后，用户订阅进度返回 `shared.windows[]`，每个窗口独立返回：

- 自己的 base share、最大准入额度、已用、借用金额、份额百分比、窗口边界和准入状态；
- `shared` 顶层旧字段保留为 `long` 窗口兼容视图，不能替代 `windows[]`；
- 只显示当前用户的分配，不暴露其他用户的邮箱和明细；
- 管理员快照可看到全体成员、总用量、soft/hard 状态。

V1 先复用现有订阅进度 API 字段兼容返回；新增共享池快照字段只扩展 JSON，不删除旧字段。

## 9. 上游官方百分比同步

OpenAI/Codex OAuth 账号可选用现有 `OpenAIQuotaService` 调用官方 `/backend-api/wham/usage`。该响应直接提供 primary/secondary 滚动窗口的 `used_percent`、窗口长度和重置时间；它比把日历日 analytics 的 credit 相加更适合做硬准入，因为 analytics 按日切分，而 Pro 周限额是滚动窗口。

实现规则：

1. 只保存账号、窗口长度、已用百分比、重置时间和抓取时间，不保存 access token；
2. V1 兼容模式下，官方百分比只决定总池使用率和 soft/hard 线，成员分摊仍以本地真实 `usage_logs.actual_cost` 按比例计算；V2 Analytics 可用时改用第 13 节的 credit 校准公式；
3. 默认 60 秒刷新，15 分钟内的旧快照可继续用于限制并标记为 stale；超过 15 分钟或首次没有快照时，不阻塞网关请求，后台继续刷新；
4. 配置了账号 ID 时严格使用该账号；未配置时只有分组内恰好一个活跃 OpenAI OAuth 账号才自动选择，多账号直接标记刷新失败，避免误用其它账号；
5. V2 会将同一官方窗口内的 Analytics credit 与 `/wham/usage` 百分比配对，按第 13 节估算容量；该估算只对同窗口、同周期快照有效，换算率 `1000 credit = 40 USD` 随快照保存，不能跨供应商、跨窗口或跨价格规则复用。

对于 Pro 20x 拼车，推荐启用 `long=7d` 的官方百分比模式；只有确认官方存在并返回 5h secondary window 时，再单独启用 `short=5h`。如果官方没有 5h 窗口，就不要人为填一个 5h 容量伪造限制。

## 10. 运维与审计

管理员每次配置修改应进入现有 admin audit log，记录：操作者、分组、旧配置、新配置、窗口增删/启停、成员权重变更和结果。控制台应按窗口显示：剩余时间、共享池使用率、soft/hard 状态、每个成员的 base share 与 borrow amount。

应监控：共享池查询失败次数、fail-open 次数、窗口推进失败、池达到 soft/hard 的次数、快照年龄和 usage log 对账差异。

## 11. 部署与回滚

1. 先应用 311 基础快照迁移，再应用 312 Analytics credit 与 baseline 增量迁移；
2. 发布后默认所有池关闭，不影响旧用户；
3. 管理员配置并观察至少一个窗口；
4. 若策略异常，关闭 `enabled` 即回到原有分组日/周/月限额；也可只关闭异常窗口；
5. 数据表保留，不删除历史配置，便于恢复与审计。

## 12. 测试验收

- 3 人等权时份额为 33.333…%，不能因为显示舍入导致总份额超过 100%；
- 权重 2:1:1 时份额为 50/25/25；
- 用户未超过 base share 时允许；超过 base 且 soft stop 未到时仅在借用开启时允许；
- soft stop 后借用拒绝但其他用户自己的 base share仍可用；
- hard stop 后所有成员拒绝；
- 短窗耗尽而长窗未耗尽时拒绝，长窗耗尽而短窗未耗尽时同样拒绝；
- 同时启用自定义窗口时，所有启用窗口必须通过，关闭一个窗口不影响其它窗口；
- 成员禁用、订阅过期、窗口滚动、配置关闭均有覆盖；
- 数据库不可用时不改变旧计费链路，并有明确日志；
- API 更新非法比例/容量时不产生部分写入；
- 官方快照按窗口匹配、按账号隔离、过期后不继续作为硬限制；
- 官方百分比 45%、三名等权且本地用量 60/40/0 时，成员分摊为 27%/18%/0%，总池仍以 45% 判定；
- 现有订阅、余额、计费、Google middleware 测试全部保持通过。

## 13. V2：官方 Analytics credit 校准与已用额度基线

### 13.1 为什么不能继续只显示 USD 或直接把官方百分比分给成员

`/wham/usage` 的百分比是上游滚动窗口的**绝对使用率**，不是本共享池启用后的使用率。
如果账号在启用拼车前已经使用了 29%，把当前 29% 按成员本地费用比例分摊，会把历史用量错误计入新成员；
如果把每个人都按 100% 的独立上限处理，又会重复出售同一份上游额度。手动 USD 只能表达管理员的保守预算，
不能证明 OpenAI 的真实周上限，因此官方模式不再把 USD 当作计算主单位。

V2 采用明确的“双层账本”：

1. **上游事实层**：官方 `/wham/usage` 提供当前滚动窗口百分比、窗口长度和重置时间；
2. **容量校准层**：官方 `daily-workspace-usage-counts?group_by=day` 提供同一时段的 credit/token 聚合；
3. **共享池层**：启用或新周期第一次同步时保存 `baseline`，baseline 以前的上游用量不归属于任何共享池成员；
4. **成员归属层**：官方接口没有按网关用户拆分的 credit，因此成员用量使用本项目同一窗口内的 `usage_logs.actual_cost` 比例归属，
   并在界面明确标记为“网关本地归属”。它用于公平准入，不伪造官方的逐用户明细。

因此界面和准入口径如下：

- `official_percent` 仍保留为配置兼容名称，但运行时优先使用 `analytics_credit`；
- Analytics 可用时，**credit 是计算主单位，百分比是上游事实和进度辅助显示，USD 仅是按当前换算率的展示值**；
- Analytics 不可用时，退回 `provider_percent_fallback`，只能按官方百分比减去安全线进行保守准入，管理员看到明确的“未校准”状态，
  不再显示看似精确的 `$600` 额度；
- 当前实现默认 `25 credits = 1 USD`（即 1000 credits = 40 USD），该换算率随每个快照保存，便于官方价格规则变化时审计和迁移。

### 13.2 Analytics 请求与数据边界

后台刷新任务复用 OpenAI OAuth quota service 的 access token、代理、Agent Identity 和重试机制，异步请求：

```text
GET /backend-api/wham/analytics/daily-workspace-usage-counts
  ?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD&group_by=day
```

日期范围由当前官方窗口计算：优先取 `reset_at - limit_window_seconds` 到当前时间，按 UTC 日传递，
不把未来的 reset 日期作为查询结束日。响应解析采用兼容字段读取，接受 `credits/credit/total_credits`、
输入/缓存输入/输出 token 的常见命名和 `data`/`daily` 数组结构；未知字段保留为不可用，不会因为私有接口结构变动阻塞网关。

Analytics 不是网关热路径：首次同步和刷新均在后台 singleflight 中执行，Analytics 失败只保留上一份有效快照，
并将状态标为 `unavailable` 或 `stale`。默认 5 分钟认为需要刷新，15 分钟后不再作为精确校准依据。

### 13.3 容量与成员分配公式

令 `P` 为 `/wham/usage` 的当前使用百分比，`C` 为同窗口 Analytics 累计 credits，`B` 为共享池 baseline credits：

```text
estimated_total_credits = C / (P / 100)                    (P > 0)
distributable_credits    = estimated_total_credits × (1 - reserve)
hard_limit_credits       = distributable_credits × hard_stop
available_pool_credits   = max(0, hard_limit_credits - C)
pool_used_credits        = max(0, C - B)
member_base_credits      = available_pool_credits × weight / Σ(enabled_weight)
member_max_credits       = member_base_credits × borrow_multiplier
```

这里 `C` 是账号当前的绝对上游使用量，所以已有 29% 会直接减少可分配空间；`B` 只用于判断本共享池成员从何处开始计量，
不会把历史 29% 塞给某个用户。`local_used_credits` 由本地费用乘以快照中的 credits/USD 换算率得到，
只有在本地使用记录存在时才参与成员归属；外部或未归属的上游使用只减少池的剩余空间，不会被分摊给用户。

如果 `P=0` 或 `C` 缺失，不能推导总容量，系统使用百分比回退模式：

```text
available_pool_percent = max(0, hard_limit_percent - P)
pool_used_percent       = max(0, P - baseline_percent)
```

这是一种保守近似，必须在 API 和 UI 上标注，禁止伪装成精确金额。周期 reset_at 变化时，baseline、Analytics 范围和成员本地起点一起切换，
旧周期只留在快照审计字段中，不参与新周期准入。

### 13.4 快照、迁移和失败语义

现有 `shared_quota_pool_official_snapshots` 保留一行当前投影；新增 Analytics 统计列和 baseline 列，不新增请求热路径表：

- Analytics：credits、输入/缓存输入/输出 token、日期范围、抓取时间、来源、状态、置信度、换算率；
- baseline：抓取时的上游 credits/percent、捕获时间、对应 reset_at；
- derived：估算总容量和当前池剩余容量只在服务层计算，不重复持久化。

写快照时保留已有 baseline，只有首次捕获、reset_at 进入新周期或管理员重新启用窗口才建立新 baseline。
上游刷新失败不覆盖最后一份有效 Analytics；超过最大陈旧时间则关闭“精确校准”标志，但维持现有官方百分比的 fail-open 兼容行为。

### 13.5 管理员界面验收口径

官方窗口卡片必须同时给出：`上游已用百分比`、`Analytics credits`、`估算总 credits`、`可分配剩余 credits`、
`baseline`、`最后同步时间`和状态标签。成员表在官方校准模式显示 `已用/份额/最大值` 的 credits，
并可展开查看换算 USD；不得再把官方模式的 `100%` 与手动 `$600` 放在同一行造成二选一冲突。

管理员配置仍只需选择“官方同步”并选择上游账号，不能手填官方容量；只有手动 USD 模式才显示容量输入。
若 Analytics 私有接口不可访问，管理员可以继续使用百分比回退，也可以关闭共享池回到原订阅限额。

### 13.6 V2 验收用例

- 账号已有 29%、Analytics 为 290 credits、三名等权、reserve=0、hard=95%：估算总容量 1000 credits，
  新池硬线为 950，当前可分配 660 credits，历史 290 不计入任何成员；
- 首次启用后用户 A/B/C 产生本地费用 2/1/0 USD，Analytics 增加但未返回逐用户明细时，成员归属按 2:1:0 的本地比例，
  总池硬线仍按官方绝对 credits 判定；
- Analytics 返回 0 credits 且官方 P=0：状态为未校准，不显示估算容量，不因除零产生无限额度；
- Analytics 返回错误或超 15 分钟：保留上次快照，标记 stale；没有历史快照时不阻塞网关；
- reset_at 改变：新 baseline 建立，旧周期用量不会污染新周期；
- 同一分组连续刷新不会重复建立 baseline，也不会把旧成员历史费用重新归属；
- 私有接口返回未知字段、空 data、字符串数字或部分 token 字段时，解析不崩溃，状态和可用字段准确降级；
- 现有 manual USD、日/月限额、余额扣费和官方百分比兼容测试全部保持通过。
