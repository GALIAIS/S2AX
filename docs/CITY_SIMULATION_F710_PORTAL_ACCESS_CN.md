# 城市模拟 F7.10 动态 Portal 与访问控制详细设计

版本：v1.0（2026-07-18）
状态：已实现
模拟版本：`city-f7-v8`
运行时协议：`runtime_version = 1.2.0`

## 1. 目标与边界

F7.10 在 F7.9 的静态通行解析器上增加可审计的动态 Portal 状态和声明式 Actor 访问策略。Portal 不再永远可通行；路径预览和 Tick 内实际移动都会读取同一份状态、同一棵 Requirement 树和同一世界快照。

本阶段完成以下闭环：

1. 每个 active Building Portal 都有唯一、版本化的动态状态投影。
2. Entrance 支持 `open → closed → locked` 的显式状态机，状态变化必须由 Actor 在交互范围内执行。
3. 每个 Portal 都绑定规范化 Requirement 树与 SHA-256 policy hash。
4. 路径查询和实际移动统一拒绝 closed、locked 和访问策略不满足的 Portal。
5. 状态、策略、来源 Fact、Effect、版本和元数据进入规范状态、快照、重放与恢复。
6. 前端提供 Portal 图谱、Actor 访问诊断、状态操作和结构化策略编辑，不允许直接编辑数据库 JSON。

本阶段不实现 Key 物品库存、门锁耐久、破门、NPC 群体调度、Cell 预留、交通信号或动态危险成本。这些能力必须复用本阶段协议继续扩展，不能绕过 Portal 投影直接修改可达性。

## 2. 版本与升级

F7.9 的 `city-f7-v7` canonical 字节保持冻结。F7.10 使用显式升级边：

```text
city-f7-v7 --f7_v7_to_f7_v8--> city-f7-v8
```

迁移 `205_world_portal_access.sql` 完成：

- 注册 `city-f7-v8` 引擎、`portal_access` capability 和唯一 v7→v8 升级边；
- 将开放世界运行时协议从 `1.1.0` 提升为 `1.2.0`；
- 为每个 active Portal 建立一条 open/public/version 1 基线状态；
- 扩展所有冻结前序基础断言，但不改变旧版本 canonical 字节；
- 新增写闸门、延迟一致性断言和来源 Fact/Effect 验证。

旧世界不会因部署新代码而获得动态状态。只有显式升级成功后，`world_portal_states` 才会成为该世界的权威投影。pre-v8 世界访问 Portal 状态 API 时返回 `WORLD_PORTAL_ACCESS_UNAVAILABLE`。

## 3. 权威状态

`world_portal_states` 以 `(world_id, portal_id)` 唯一，核心字段为：

```text
state_code              open | closed | locked
access_requirement      规范 Requirement 对象
access_policy_hash      SHA-256(canonical requirement)
changed_tick            最近变更 Tick
source_fact_id          最近变更来源；基线为空
version                 每次 Fact-backed 更新严格 +1
metadata                版本化对象
```

状态通过 Portal 和 Building 联合得到稳定业务身份：

```text
building_code + portal_code + portal_type
from(x,y,z) + to(x,y,z) + bidirectional
```

数据库断言要求：

- 每个 active Building Portal 恰好有一条动态状态；
- Stair 始终保持 `open`，但仍可绑定访问策略；
- `changed_tick` 不得超过 world tick；
- 基线必须为 version 1、无 source fact，且 metadata 标记 `source=baseline`；
- 非基线必须引用已 posted 的 `portal.state.changed` 或 `portal.access.changed` Fact；
- 对应 Effect 的 executor、target key、前后版本和完整 after state 必须与投影一致。

普通 SQL 不能插入、更新或删除 Portal 状态。只有 bootstrap、已授权的 draft Fact 写上下文和 verified recovery 可以通过触发器。

## 4. 状态机与操作授权

Entrance 只允许以下转换：

```text
open   --close-->  closed
closed --open-->   open
closed --lock-->   locked
locked --unlock--> closed
```

不存在隐式 `locked → open`，也不能对已处于目标状态的 Portal 重复执行动作。状态命令执行时必须同时满足：

1. 调用者是 active world member；
2. 调用者拥有目标 Actor，或持有 active `actor.command` grant；
3. Actor 位于 Portal 任一端点，或与同 Z 端点八邻接；
4. Portal 类型为 `entrance`；
5. Actor 满足 Portal 当前访问 Requirement；
6. 当前状态到目标动作存在合法转换。

校验在 Tick 事务中重新加载并锁定 Actor Location 与 Portal state，前端预览不能替代最终授权。失败命令形成稳定 rejected 回执，不写部分 Fact、Effect 或投影。

## 5. 声明式访问策略

策略复用开放世界运行时的通用 `WorldRequirementNode`，不是 Portal 专用脚本。支持：

- `all` / `any` / `not` 组合；
- `attribute_gte` / `attribute_lte`；
- `experience_gte`；
- `role_active` / `role_inactive`；
- `status_present` / `status_absent`；
- `fact_count_gte`，可带确定性 Tick 窗口；
- `world_tick_gte`。

树深、节点数和每层 items 数均受运行时上限约束。代码字段统一 trim/lowercase，空 items 规范为 nil，随后对 canonical JSON 计算 SHA-256。Attribute、Role 和 Status 引用必须存在于当前世界的版本化定义目录中。

只有 world owner 可以提交 `portal.access.configure`。相同 policy hash 的重复更新被拒绝，避免制造无意义版本和事实。`all` 且 items 为空表示公开通行，不需要旁路布尔字段。

策略判断读取目标 Tick 的 Actor Attribute、Experience、active Role、Status、Fact 计数和 world tick。查询 API 可携带 `actor_code`，返回 `accessible` 与结构化 `access_evaluation.failures`，供 UI 解释具体失败节点。

## 6. Fact、Effect 与事务顺序

状态变化产生：

```text
Fact   portal.state.changed
Effect portal.state.set
Event  world.portal.state_changed
```

策略变化产生：

```text
Fact   portal.access.changed
Effect portal.access.set
Event  world.portal.access_changed
```

单次变更顺序固定：

```text
严格命令解码
  → 成员/Owner/Actor 控制权校验
  → 锁定当前 Portal state
  → Requirement 或状态机校验
  → 插入 draft Fact
  → 开启该 Fact 的投影写闸门
  → 更新 Portal projection(version + 1)
  → 插入带 before/after 的 Effect
  → touch Actor（仅状态动作）
  → 更新 runtime profile 计数
  → post Fact
  → 写稳定事件与命令终态
```

Effect envelope 使用 Portal version 作为 `before_units` / `after_units`，delta 固定为 1；`target_key` 为 `building_code.portal_code`。状态动作 Effect 必须有 Actor，策略 Effect 必须无 Actor，重放器会逐字段验证这一差异。

## 7. 导航集成

F7.10 沿用 F7.9 的统一 Cell/Edge 解析器。解析到 Portal transition 后按固定顺序检查：

```text
Portal state
  → Actor identity
  → canonical access requirement
  → access evaluation cache
  → movement cost
```

结构化路径原因新增：

```text
portal_closed
portal_locked
portal_access_denied
```

实际移动对应稳定拒绝码：

```text
WORLD_NAVIGATION_PORTAL_CLOSED
WORLD_NAVIGATION_PORTAL_LOCKED
WORLD_NAVIGATION_PORTAL_ACCESS_DENIED
```

路径查询仍运行于 `REPEATABLE READ READ ONLY`；同一次查询中的 world tick、Actor 状态、Portal 状态和 Requirement 评估来自同一快照。每个策略判断按 `actor + portal + policy hash` 在查询上下文内缓存。实际 `actor.location.move` 在 Tick 事务中重新校验，因此 Portal 在预览后关闭或策略变更时不会穿透旧路径。

## 8. API 与命令协议

### 8.1 读取 Portal 图谱

```http
GET /api/v1/city/worlds/:world_id/navigation/portals?actor_code=actor_00000001
```

响应按 Building/Portal 稳定排序，每项包含 state、from、to、bidirectional；提供 Actor 时额外返回 accessible 和 access evaluation。

### 8.2 变更状态

通过现有命令入口提交：

```json
{
  "command_type": "portal.state.transition",
  "payload": {
    "actor_code": "actor_00000001",
    "building_code": "building.central.hall",
    "portal_code": "entrance.main",
    "action": "close"
  },
  "expected_world_tick": 42
}
```

### 8.3 配置策略

```json
{
  "command_type": "portal.access.configure",
  "payload": {
    "building_code": "building.central.hall",
    "portal_code": "entrance.main",
    "requirements": {
      "op": "role_active",
      "role_code": "profession.technician"
    }
  },
  "expected_world_tick": 42
}
```

所有命令继续使用幂等 key、请求指纹、expected tick、私有回执和 scheduler/显式 step 的统一处理链。

## 9. Canonical、重放与恢复

`city-f7-v8` 要求 `WorldRuntime.PortalStates` 非 nil，并按 BuildingCode、PortalCode 稳定排序进入 canonical state。pre-v8 序列化会显式移除该字段，保证旧快照字节不变。

逐 Tick 重放对每个 Portal Effect 验证：

- Portal 身份、类型、target key、executor version；
- before state 与当前归约状态逐字段一致；
- Requirement canonical hash 与保存的 policy hash 一致；
- source fact、changed tick 和版本连续；
- 状态动作合法且策略不变，或策略动作状态不变且策略确实变化；
- mutation metadata 符合协议版本。

JSON metadata 使用语义 canonical 比较，空白和对象键顺序不影响验证，但字段值、类型或额外内容的篡改仍会失败。恢复先清理受保护运行时投影，再用 verified replay 的规范状态重建 Portal 行和 source fact 外键，最后重新计算全世界状态哈希。

## 10. 前端闭环

当前空间工作台已增加 Portal 图谱：

- 列出 Building/Portal、端点、方向、类型、状态和版本；
- 基于选中 Actor 显示可访问状态及 Requirement 失败原因；
- 仅在 Actor 有控制能力且处于交互范围时启用合法状态动作；
- owner 可用结构化表单组合公开、Role、Attribute、Experience、Status、Fact Count 和 World Tick 策略；
- mutation 成功后局部刷新 Actor、Portal 和导航数据，不触发整页卸载或闪烁；
- pre-v8 世界优雅降级，不显示伪造 Portal 状态。

## 11. 验收证据

已覆盖：

1. migration table、约束、索引、触发器、版本目录、升级边和基础断言；
2. public Requirement canonical hash、引用校验、树限制和 Actor 评估；
3. Entrance 状态机、距离、控制权、Owner 策略权限和拒绝语义；
4. closed/locked/access denied 的路径查询与实际移动双重阻断；
5. Portal Fact/Effect、投影版本、canonical、重放篡改检测和 verified recovery；
6. `city-f7-v7 → city-f7-v8` 原子升级及旧版本 canonical 兼容；
7. 真实 PostgreSQL 中“关闭 → 路径阻断 → 打开并限制职业 → 成长取得职业 → 授权穿越 → replay → recovery”的端到端场景；
8. API、store 降级、Portal 操作、策略 payload、i18n 与前端交互测试。

## 12. 下一依赖切片

F7.10 已把单 Actor 的动态通行控制固化为可复用底座。下一阶段按依赖顺序进入 F7.11 NPC 行动与拥堵基础：

1. 版本化 movement intent、逐 Tick 行动点/预算和确定性重规划；
2. 有期限的 Cell/Edge reservation，解决对向冲突而不引入永久锁；
3. 公平、稳定的冲突排序和饥饿上限；
4. 路径失效原因、等待/绕行/放弃事实与恢复；
5. 为 F8 设施服务和 F9 交通物流提供统一批量移动协议。

Hazard、天气、交通模式和信号控制随后作为动态成本提供者接入；它们不得各自复制 Portal 授权或直接修改 Actor Location。
