# 城市模拟 F7.11 NPC 行动预算、确定性重规划与拥堵控制详细设计

版本：v1.0（2026-07-18）
状态：已实现并通过真实数据库重放/恢复验收
目标模拟版本：`city-f7-v9`
目标运行时协议：`runtime_version = 1.3.0`

## 1. 目标

F7.11 把 F7.9/F7.10 的“客户端查询路径并逐步提交移动”扩展为世界可以在 Tick 内持续推进的通用 Actor movement intent。该协议同时服务玩家委托、NPC 日程、任务、设施服务、通勤和物流，但底层不把 Actor 固化为某一种 NPC，也不把目的地固化为工作或住宅。

本阶段必须解决：

1. 目的地是版本化、可取消、可重放的 intent，而不是内存任务。
2. 移动能力使用整数行动预算，允许跨 Tick 积累并有明确上限。
3. 每次执行都基于当前 Terrain、Building、Portal、授权和 Occupancy 重新规划。
4. 多 Actor 冲突使用稳定公平顺序和短期 Cell/Edge reservation，不依赖 goroutine 调度或数据库 ID。
5. 到达、等待、阻塞、取消和预算不足都有结构化原因与事实，不静默卡死。
6. Intent、位置和 reservation 可以由同一条事实链重放并通过 verified recovery 重建。

## 2. 非目标

本阶段不实现：

- 任务选择、需求效用、工作排班或社会关系 AI；
- 车辆、道路车道、公交班次和物流批次；
- 群体编队、战斗追逐、推挤或交换位置；
- 天气、火灾、污染和 Field 动态成本；
- 客户端预测移动或绕过 Tick 的实时插值。

这些系统后续只负责产生/更新 intent 或提供动态成本，不得直接写 Actor Location。

## 3. 版本边界

```text
city-f7-v8 --f7_v8_to_f7_v9--> city-f7-v9
```

升级创建空 intent 投影和导航运行参数，不移动任何 Actor。`city-f7-v8` 及更早版本 canonical 字节保持冻结。v9 canonical 必须显式包含 navigation profile 和按 ActorCode 稳定排序的 intent 列表。

## 4. 导航运行参数

每个 v9 世界有一条 `world_navigation_profiles`：

```text
profile_version                 1.0.0
maximum_intents_per_tick        默认 128
default_budget_gain_units       默认 100
default_budget_cap_units        默认 400
default_max_steps               默认 256
maximum_blocked_attempts        默认 64
maximum_retry_delay_ticks       默认 8
fairness_aging_cap              默认 1024
revision                        从 1 开始
metadata                        schema_version=1
```

参数均为整数并进入规范状态。首版只在升级/创世时建立固定版本化参数，不开放任意在线修改；后续若需要配置，必须新增命令、版本和参数变更事实。

## 5. Movement Intent 投影

每个 Actor 至多一条当前 intent：

```text
intent_code              由设置命令序列稳定派生
actor_code               稳定 Actor 身份
destination_x/y/z        权威目的地
status                   active | blocked | arrived | cancelled | failed
on_blocked               retry | cancel
priority                 -10..10
max_steps                1..1024
budget_units             当前整数预算
budget_gain_units        每 Tick 增量
budget_cap_units         累积上限
blocked_attempts         连续失败次数
last_reason              结构化导航/调度原因
next_attempt_tick        确定性退避边界
created_tick             本 intent 创建 Tick
updated_tick             最近事实 Tick
source_fact              最近事实引用
version                  每次变化严格 +1
metadata                 schema_version=1
```

重新设置会替换该 Actor 的旧终态或活动 intent，并生成新 `intent_code`；不会修改旧事实。取消只接受 active/blocked intent。到达、取消和 failed 为终态，除非通过新的 set 命令显式替换。

## 6. 命令

### 6.1 设置

```json
{
  "command_type": "actor.navigation.intent.set",
  "payload": {
    "actor_code": "actor_00000001",
    "destination": { "x": 18, "y": 12, "z": 0 },
    "priority": 0,
    "max_steps": 256,
    "on_blocked": "retry"
  }
}
```

调用者必须拥有 Actor 的 `actor.command` capability。提交时做格式和范围校验；Tick 执行时重新验证控制权、Actor 当前 Location、目的地 Cell 与 v9 导航能力。设置成功不会在同一命令 Effect 中瞬移，自动执行从随后统一调度阶段开始。

### 6.2 取消

```json
{
  "command_type": "actor.navigation.intent.cancel",
  "payload": { "actor_code": "actor_00000001" }
}
```

取消和重新设置都通过相同 projection 写闸门、Fact/Effect 与命令回执，不允许 DELETE intent。

## 7. 行动预算

处理 Actor 时先计算：

```text
elapsed = target_tick - updated_tick
accrued = min(budget_cap, budget_units + elapsed * budget_gain)
```

乘法和加法必须检查 int64 溢出。若下一步成本大于 accrued：

- 不移动；
- 保存 accrued；
- `last_reason = budget_insufficient`；
- 下一 Tick 可重试；
- 写 waited Fact/Effect，使预算状态可重放。

成功移动后扣除该步 `step_cost`。预算不得为负，也不得超过 cap。首版一个 Actor 每 Tick 最多执行一步，避免一个高预算 Actor 在同 Tick 穿过多个尚未获得公平处理机会的 Actor；预算余额为后续不同速度、负重和车辆模式保留扩展点。

## 8. 稳定公平调度

每 Tick 在用户命令处理完成后，选择 `active/blocked` 且 `next_attempt_tick <= target_tick` 的 intent。有效优先级：

```text
effective_priority = priority + min(target_tick - updated_tick, fairness_aging_cap)
```

稳定排序：

1. effective priority 降序；
2. blocked attempts 降序；
3. created tick 升序；
4. actor code 升序。

只处理前 `maximum_intents_per_tick` 条。未入选 Actor 不改变状态，其等待时间继续增长，因此最终会通过 aging 获得机会。排序不使用自增 ID、墙钟或 goroutine 完成顺序。

## 9. 重规划与失败策略

每次处理都从 Actor 当前权威位置调用同一 v9 导航上下文执行有界 A*。不保存可失效的完整路径作为事实，只保存当前尝试的路径证明：world tick、rule hash、Portal policy hash 结果、下一坐标、step cost 和 expanded nodes。

结果：

- 起点等于目的地：`arrived`；
- 有路径且预算足够：申请 reservation，重新验证并移动一步；
- 有路径但预算不足：`budget_insufficient`，保持 active；
- 暂时性阻塞（occupancy、closed/locked Portal、授权、Chunk 未生成、search limit）：进入 blocked 并退避；
- 永久/输入错误（outside world、无效目标）或达到最大失败次数：failed；
- `on_blocked=cancel`：第一次无法执行即 cancelled。

退避公式：

```text
delay = min(1 + blocked_attempts / 4, maximum_retry_delay_ticks)
next_attempt_tick = target_tick + delay
```

不使用随机抖动；多 Actor 的错峰由稳定排序和不同 blocked age 形成。

## 10. Cell/Edge Reservation

成功自动移动前写入 Tick 级 reservation：

```text
world_id + tick + sequence
actor + intent_code
from_coordinate + to_coordinate
target_key
undirected_edge_key
source_fact
step_cost
status = consumed
```

同一 Tick：

- target key 唯一，禁止两个 Actor 获得同一目标 Cell；
- undirected edge key 唯一，禁止相反方向交换或同边重复穿越；
- reservation 只能引用同 Tick 的 intent progress Fact；
- reservation 是不可修改历史，不能成为跨 Tick 永久锁。

Occupancy 仍在执行时重新验证。Reservation 解决“同一 Tick 多个计划均认为目标可用”的竞争；Location 投影解决执行后的实际占用。失败 reservation 不落库，等待原因由 intent Fact 记录。

## 11. Fact/Effect 协议

Fact 类型：

```text
actor.navigation.intent.created
actor.navigation.intent.replaced
actor.navigation.intent.cancelled
actor.navigation.intent.waited
actor.navigation.intent.blocked
actor.navigation.intent.progressed
actor.navigation.intent.arrived
actor.navigation.intent.failed
```

Effect 类型：

```text
actor.navigation.intent.set
actor.location.set              复用现有位置 Effect
```

created/replaced/cancelled/waited/blocked/arrived/failed 只产生 intent Effect；progressed 同时产生 location Effect 和 intent Effect，二者共享一个 source Fact。Effect payload 保存严格 before/after；初次创建 before 为空、版本 envelope 为 0→1，其余必须连续 +1。

## 12. Tick 事务

```text
状态到期
  → 用户命令（含 intent set/cancel）
  → 加载导航 profile
  → 稳定选择 due intents
  → 每 Actor savepoint
      → 预算归集
      → 当前快照重规划
      → reservation 冲突检查
      → Tick 内最终移动校验
      → Fact + reservation + Location/Intent Effects
      → post Fact
  → 其余城市阶段
  → canonical hash / tick / events
```

预期的导航阻塞不能中止整个城市 Tick；它会转成 blocked/waited Fact。数据库不变量、哈希不一致、非法版本或写闸门失败仍中止 Tick。

## 13. API 与前端

读取接口：

```http
GET /api/v1/city/worlds/:world_id/navigation/intents
GET /api/v1/city/worlds/:world_id/navigation/intents/:actor_code
GET /api/v1/city/worlds/:world_id/navigation/reservations?tick=...
```

成员可读取世界当前 intent 与 reservation；Actor 访问权限和现有成员边界保持一致。前端在 Actor 导航区显示目的地、状态、预算、失败原因、下次尝试、版本和最近 reservation，并提供设置/替换/取消操作。所有刷新必须局部更新 store，不卸载地图。

## 14. Canonical、重放与恢复

v9 canonical 包含 profile 和当前 intent，不包含可由 Fact 重建的 reservation 历史。逐 Tick replay 按事实顺序同时归约 Location 与 Intent，验证预算公式、版本、路径步骤、reservation proof 和 Actor touch 次数。Recovery 先保存稳定 Fact/Effect ID，清理 runtime 与 reservation 投影，恢复 Fact/Effect 后从 progress Fact 重建 reservation，再恢复当前 intent。

任何 before mismatch、预算越界、重复 target/edge、缺失 source Fact、Actor 位置不连续或 metadata 非语义等价都会使 replay/recovery 失败。

## 15. 验收门槛

1. migration、版本目录、旧版本兼容、写闸门和 deferred assertion 测试；
2. 命令规范化、权限、幂等、替换和取消测试；
3. 预算累计/上限/扣减/溢出测试；
4. 稳定公平排序、处理上限和 aging 防饥饿测试；
5. 同目标、反向边、Portal 状态变化、授权变化和 Occupancy 冲突测试；
6. 到达、blocked backoff、cancel-on-blocked 和最大失败终态测试；
7. canonical、snapshot、replay 篡改检测和 verified recovery；
8. 真实数据库多 Actor 竞争、逐 Tick 前进、关闭 Portal 重规划、恢复后一致性；
9. API/store/UI 局部刷新、过期响应抑制和 i18n 测试。

## 16. 实施结果

F7.11 已形成完整纵向闭环：

```text
206 migration / 数据库写闸门与延迟断言
  → city-f7-v8 到 city-f7-v9 原子升级
  → intent set / replace / cancel 命令
  → Tick 内稳定公平选择、预算归集与有界重规划
  → Cell/Edge reservation、Location 与 Intent 双 Effect
  → canonical / snapshot / replay / verified recovery
  → 认证读取 API、Pinia 过期响应抑制与角色移动工作台
```

已验证预算上限与溢出、排序上限和 aging、公平稳定 tie-break、retry/cancel/failed 分支、target/edge 冲突分类、语义 JSON 重放、创建/替换/取消、逐 Tick 前进、到达、reservation 恢复以及旧版本兼容。真实 PostgreSQL/Redis 集成测试从 `city-f7-v9` 创世运行到终态，完成全历史 replay 后再执行 recovery，并逐字段比较恢复后的 intent 与 reservation。

后续任务、日程、通勤、设施服务和物流只能产生或替换该协议的 intent；不得绕过 Fact/Effect 直接写 Actor Location。
