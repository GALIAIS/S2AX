# 城市模拟 F7.8 成员、调度与命令回执详细设计

版本：v1.0（2026-07-18）
适用引擎：`city-f7-v6` 及其兼容版本
状态：首个运行闭环已实现；本切片不改变 canonical 模拟字节和引擎版本

## 1. 目标

F7.8 把 F7.7 的“成员可以提交 Actor 命令，但只能等待 owner 手动 Step”补成可持续运行闭环：

1. 世界 owner 可按邮箱或用户名维护 active 成员；
2. 控制权委托从手填用户 ID 改为服务端成员目录选择；
3. running 世界按速度到期推进，paused 世界只在存在 pending command 时推进；
4. 多实例 worker 通过数据库 lease 竞争，不依赖单机内存锁；
5. 每次自动推进仍复用原有 `StepWorld`、事务 advisory lock、预期 Tick 和幂等事实；
6. 非 owner 只能读取自己的命令回执，owner 可审计本世界全部回执；
7. 前端后台轮询终态回执，不刷新页面、不冒充 owner，也不清空地图投影。

本切片是产品运行层，不创建新的模拟事实类型。成员目录、lease、重试时间和网络轮询都不进入 canonical state；真正改变世界的仍只有已提交 Tick、Command、Fact、Effect、Ledger 和受保护投影。

## 2. 成员模型

复用 `city_members`，角色集合保持：

| 角色 | 当前权限 | 预留语义 |
|---|---|---|
| `owner` | 管理成员、推进 Tick、执行 owner 命令 | 世界最终治理者 |
| `planner` | active member 基础读取与自己的 Actor 能力 | 后续规划政策能力 |
| `treasurer` | active member 基础读取与自己的 Actor 能力 | 后续预算/财政能力 |
| `trader` | active member 基础读取与自己的 Actor 能力 | 后续市场操作能力 |
| `viewer` | active member 基础读取与自己的 Actor 能力 | 只读协作身份 |

除 `owner` 外，本版本不把角色名直接等同于具体城市命令权限。Actor 操作继续由 Actor ownership 与 `world_actor_control_grants` 判定，避免未来增加组织、任务或设施权限时与成员角色硬耦合。

### 2.1 添加成员

- 只接受精确邮箱或精确用户名，不开放模糊搜索用户目录，避免枚举平台账号；
- 目标必须是 active、未删除用户，并且必须唯一匹配；
- owner 记录不可覆盖；
- 已 left/banned 的记录通过同一入口重新激活，清理旧生命周期时间；
- 写接口使用统一 `Idempotency-Key` 协调器。

### 2.2 更新与离开

- owner 可修改非 owner 的 delegated role；
- `active → left/banned` 前必须先撤销该成员持有的 active Actor grants；
- owner 成员不可修改或移除；
- 非 active 成员的世界读取返回 world not found，不泄露世界存在性；
- 当前列表只返回 active 且平台账号仍 active 的成员。

成员自己拥有的 Actor 会自动拥有 owner-baseline grants，因此不能在未转移、归档或删除 Actor 前离开世界。这是有意的完整性门禁；Actor 生命周期与转移将在开放世界运行层的后续切片提供，不用直接 SQL 绕过。

## 3. API 契约

```text
GET   /api/v1/city/worlds/:world_id/members
POST  /api/v1/city/worlds/:world_id/members
PATCH /api/v1/city/worlds/:world_id/members/:user_id

GET   /api/v1/city/worlds/:world_id/commands
GET   /api/v1/city/worlds/:world_id/commands/:command_id
```

添加请求：

```json
{
  "identity": "member@example.com",
  "role": "planner"
}
```

更新请求至少包含一项：

```json
{
  "role": "viewer",
  "status": "active"
}
```

命令列表参数：

- `status=pending|applied|rejected`；
- `after_sequence`：正向稳定游标；
- `limit`：默认 100，最大 200；
- `latest=true`：读取最新一页，供回执工作台恢复页面状态。

授权边界：

- owner 列表/详情可读取该世界所有命令；
- 非 owner 只可读取 `city_commands.user_id = 当前用户` 的命令；
- 不满足条件统一返回 command not found，不能借 ID 探测其他成员 payload、结果或错误。

## 4. Tick 调度算法

### 4.1 到期判定

候选世界必须处于 `paused` 或 `running`：

```text
running AND (next_tick_at IS NULL OR next_tick_at <= database_clock)
OR
存在 pending command
```

因此：

- running 世界即使没有命令也会持续模拟；
- paused 世界不会空转，但成员命令仍可被封账；
- failed/upgrading/recovering 等状态不会被调度。

速度到真实时间的映射为：

```text
一个模拟 Tick = 一个模拟小时
real_delay_ms = ceil(3_600_000 × 1000 / speed_milli)
```

`1.000x` 每小时推进一次，`1000.000x` 最快约 3.6 秒一次。所有计算使用整数和向上取整；数据库墙钟只决定何时尝试，不进入模拟状态哈希。

### 4.2 持久 lease

`city_world_schedule_states` 只保存运行协调状态：

- `lease_token / lease_expires_at`：worker 所有权与崩溃自动释放；
- `consecutive_failures / retry_not_before`：跨进程持久退避；
- `last_attempt_at / last_success_at`：运行健康；
- `last_error_code / last_error_detail`：有界诊断信息。

候选查询与 lease upsert 在同一 SQL statement 中完成。`ON CONFLICT ... WHERE lease 已过期` 确保多实例同时发现世界时只有一个取得 lease。worker 崩溃后 lease 到期，其他实例可接管。

lease 不是世界写锁。取得 lease 后仍必须调用唯一 `StepWorld`：

```text
claim durable lease
  → StepWorld(owner, expected_tick, scheduler idempotency key)
      → transaction advisory lock
      → lock world row
      → verify expected tick
      → process commands and stages
      → post tick and canonical hash
  → complete / release / defer lease
```

调度幂等键为 `city-scheduler-v1-{world_id}-{current_tick}`。并发 worker、人工 Step 或重试导致 expected-tick/idempotency 冲突时，调度器只释放 lease，不把它记录成世界故障。

### 4.3 失败退避

非冲突错误会：

1. 释放 lease；
2. 增加 `consecutive_failures`；
3. 设置 `retry_not_before`；
4. 保存截断到 1000 Unicode 字符的错误详情；
5. 保留最后成功 Tick，不跳过失败 Tick。

退避从 1 秒指数增长，最大 5 分钟。成功后清零失败计数、重试时间和错误；进程重启不会丢失退避状态。

## 5. 命令回执

提交响应中的 pending command 立即进入前端本地回执列。非 owner 路径：

```text
submit command
  → 显示 pending
  → 后台有界轮询 command detail
  → scheduler 封账
  → applied / rejected receipt
  → 拉取最新 world tick 与 runtime projection
```

轮询具有以下约束：

- 首次立即查询，随后递增间隔，单次提交总窗口有界；
- 切换世界后使用 generation token 丢弃旧响应；
- 同 command ID 只允许一个 in-flight poll；
- 短暂网络失败只重试回执，不清空已显示状态；
- terminal receipt 可先更新 processed tick，随后尝试拉取完整 world snapshot；
- applied 后只刷新 runtime 数据，不触发路由跳转或整页刷新。

owner 手动封账路径同样写入 pending 和 terminal receipt，确保两条操作路径显示一致。

## 6. 前端交互

开放世界工作台新增：

1. active 成员 roster，显示用户名、邮箱、用户 ID 与职责；
2. owner 的精确身份添加、职责切换和移出操作；
3. Actor control grant 使用可搜索 Select，只列 active 非 owner 成员；
4. delegation 列显示可读成员身份，不再只显示裸 ID；
5. 最近 24 条命令回执，区分 pending、applied、rejected 和 processed tick；
6. 所有请求使用局部 loading key，不卸载地图、Actor 状态或其他列表。

## 7. 安全与不变量

- 成员管理在事务中锁世界，并要求 active owner membership；
- 角色和状态使用服务端白名单，空更新拒绝；
- 成员移除前检查 active Actor grants；
- command list/detail 均在 SQL 中施加 owner/own-command 条件，不在读取后过滤；
- scheduler 不直接更新 current tick、命令终态、Fact 或投影；
- lease 表不参与 canonical、snapshot、replay 或 recovery；
- Step 的数据库 advisory lock 和 expected tick 仍是最终并发正确性边界；
- scheduler 停止被接入应用 cleanup，关闭时等待当前 sweep 完成或超时上下文结束。

## 8. 验收门禁

已覆盖：

1. 速度映射的 paused、最慢、1x、最快和非法边界；
2. 确定性 scheduler 请求键和 expected tick；
3. 成功、并发冲突和失败退避三条 lease 终结路径；
4. migration 中 lease pair、失败计数和重试索引；
5. PostgreSQL 下成员添加、职责修改、active grant 离开阻断、离开与重新加入；
6. PostgreSQL 下 delegate 命令由 scheduler 封账并得到 terminal receipt；
7. 非 owner 命令列表/详情不能读取 owner 命令；
8. scheduler 成功后 lease、retry 和 failure 状态归零；
9. 前端 roster、Select 委托、member mutation 和无整页刷新回执轮询；
10. world 切换 generation token、地图投影引用保持和 owner/member 两类 mutation 路径。

## 9. 下一依赖

F7.8 之后按顺序推进：

1. **已完成 F7.9 通行、占用与确定性路径查询**：Terrain/Furniture/Building/Site 通行、Actor 碰撞、静态 Portal、路径成本、有界寻路和 Tick 内重校验；
2. **F7.10 动态通行控制**：Door/Portal 状态、credential/organization/rule 授权、NPC 移动预算和可选占用预留；
3. **Actor 生命周期**：归档、转移、死亡/失能和 orphan recovery，使有 Actor 的成员可通过显式事实安全离开；
4. **运行观测**：管理员查看 lease、积压、失败退避和每世界 Tick 延迟，不提供修改历史的旁路；
5. **F8 公共设施网络**：电、水、数据、污水和垃圾拓扑、容量、故障与服务范围；
6. **F9 交通物流**：道路图、站点、线路、通勤、载具与货物流；
7. **NPC/任务/物品层**：全部消费同一成员、Actor、位置、Fact/Effect 和 scheduler 协议；
8. **奖励 Outbox**：只从已提交事实生成幂等奖励，不把城市内部货币直接兑换为平台钱包。
