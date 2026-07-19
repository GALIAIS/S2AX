# 城市模拟 F7.7 Actor 空间与控制权详细设计

版本：v1.0（2026-07-18）
引擎版本：`city-f7-v6`
运行时版本：`1.1.0`
前置版本：`city-f7-v5` / runtime `1.0.0`
状态：首个纵向切片已实现并通过单元、迁移、PostgreSQL 集成和前端组件门禁

## 1. 目标与边界

F7.7 把 Actor 从“有属性但没有空间身份的记录”升级为可定位、可移动、可委托控制、可被空间规则解析的开放世界实体。它提供后续 NPC、任务、库存、交通、公共设施、组织和法律系统共同使用的稳定边界，不把角色固定为玩家、人类、居民或某一种玩法。

本版本完成：

1. 每个 `city-f7-v6` Actor 恰好一条权威位置投影；
2. 位置绑定世界空间、XYZ、Chunk/Local 坐标、管辖区和可选空间锚点；
3. 平面相邻移动与基于 Portal 的跨层移动；
4. `actor.command` 和 `actor.control.manage` 两种可委托能力；
5. 提交时与 Tick 执行时双重授权；
6. world、organization、jurisdiction、location 四类规则作用域解析；
7. 位置与控制权接入 Fact、Effect、canonical、snapshot、replay 和 recovery；
8. CLASSIC 地图 Actor glyph、检查器定位、移动面板和控制委托面板。

F7.7 本身不提前实现：

- NPC 决策、寻路和离线推进；
- 碰撞、地形通行成本、门锁、载具和战斗；
- 物品、容器、装备和空间占用；
- 关系网、任务图和组织治理。

这些模块必须消费本版本协议，不得直接改 Actor 坐标或用新的“玩家位置表”旁路。
其中可信 Tick 调度、城市成员目录、成员选择器和命令回执已由 F7.8 在不改变 canonical 版本的前提下补齐，见《城市模拟 F7.8 成员、调度与命令回执详细设计》；其余仍按后续依赖推进。

## 2. 版本契约

`city-f7-v5` 的 canonical 字节保持冻结。F7.7 只通过显式 `city-f7-v5 → city-f7-v6` 升级追加状态：

```text
city-f7-v5
  world_runtime runtime_version=1.0.0
  actors / attributes / roles / statuses / facts / effects / cases
        ↓ explicit upgrade + verified snapshot
city-f7-v6
  world_runtime runtime_version=1.1.0
  + locations
  + control_grants
```

新建 v6 世界直接初始化 runtime `1.1.0`。升级既有 v5 世界时：

1. 锁定并验证暂停世界；
2. 创建源快照；
3. 按 Actor code 稳定顺序生成位置基线；
4. 为每个有 owner 的 Actor 建立 owner 基线能力；
5. 切换模拟版本并重新计算 canonical hash；
6. 创建目标快照并记录 upgrade run。

旧 v5 世界不能出现位置或控制权状态；v6 世界缺任一 Actor 位置或 owner 能力时事务提交失败。

## 3. 数据模型

### 3.1 `world_actor_locations`

每个 `(world_id, actor_id)` 唯一：

| 字段 | 含义 |
|---|---|
| `space_kind / space_code` | 版本化空间解析器身份；当前为 `city_grid / primary` |
| `x / y / z` | 世界权威坐标 |
| `chunk_x / chunk_y` | 服务端从世界坐标推导的 Chunk |
| `local_x / local_y` | 服务端推导的 Chunk 内坐标 |
| `anchor_kind / anchor_code` | 可空；`chunk`、`building` 或 `site` 锚点 |
| `jurisdiction_code` | 从 Overmap Tile 和 District 推导的适用管辖区 |
| `moved_tick` | 最近位置事实所在 Tick；升级基线沿用 Actor 创建 Tick |
| `source_fact_id` | 新建/移动事实；升级基线允许为空并以 metadata 标记 |
| `version` | 位置投影乐观版本 |

数据库提交检查会重新验证负坐标下取整、Chunk/Local 一致性、空间边界、Z 边界、管辖区、锚点覆盖关系、事实类型和 Tick 一致性。客户端提交的派生坐标不会被信任。

### 3.2 `world_actor_control_grants`

Grant 是有生命周期的投影，不覆盖历史：

| 字段 | 含义 |
|---|---|
| `code` | 世界内稳定 grant 身份 |
| `actor_id / user_id` | 被控制 Actor 与受托城市成员 |
| `capability` | 当前为 `actor.command` 或 `actor.control.manage` |
| `status` | `active` / `revoked` |
| `granted_by_user_id` | 授权人审计身份 |
| `granted_tick / revoked_tick` | 模拟时间生命周期 |
| `grant_source_fact_id / revoke_source_fact_id` | 不可变事实证据 |
| `version` | 生命周期投影版本 |

同一 Actor、用户、能力只能有一条 active grant。被撤销的记录保留，可在后续事实中再次授予为新 grant。Actor owner 的两项基线能力不可撤销；这保证不会产生无人可恢复的 Actor。

### 3.3 投影写闸门

位置和 grant 只允许在三类上下文写入：

- world runtime bootstrap；
- 已登记的 draft Fact Effect 执行；
- verified recovery。

普通 SQL insert/update/delete 会由数据库触发器拒绝。Fact、Effect 和已撤销 grant 的历史证据不可原地改写。

## 4. 命令、事实与效果

### 4.1 移动

命令：

```json
{
  "command_type": "actor.location.move",
  "payload": {
    "actor_code": "actor_00000001",
    "x": 17,
    "y": 16,
    "z": 0,
    "anchor_kind": "chunk",
    "anchor_code": "chunk.z0.x0.y0"
  }
}
```

`anchor_kind` 与 `anchor_code` 必须同时出现或同时省略。服务端产生：

```text
actor.location.moved Fact
  → location.set Effect
  → world_actor_locations projection
  → world.actor.location_moved Event
```

平面移动要求 `abs(dx) <= 1`、`abs(dy) <= 1` 且不能原地不动，允许八方向相邻 Cell。跨 Z 要求目标仍相邻，并且起止坐标存在唯一、双向的 `city_building_portals` 记录。目的地的 Chunk、Local、辖区和锚点由服务端重新解析。

### 4.2 授权与撤销

命令：

```json
{
  "command_type": "actor.control.grant",
  "payload": {
    "actor_code": "actor_00000001",
    "user_id": 42,
    "capabilities": ["actor.command", "actor.control.manage"]
  }
}
```

撤销使用同一 payload 和 `actor.control.revoke`。能力数组会严格校验、去重拒绝并按代码排序。一次多能力命令要么全部应用，要么全部回滚。

事实链：

```text
actor.control.granted / actor.control.revoked Fact
  → control.grant / control.revoke Effect (每项 capability 一条)
  → world_actor_control_grants projection
  → world.actor.control_granted / world.actor.control_revoked Event
```

## 5. 授权矩阵

| 操作 | active membership | `actor.command` | `actor.control.manage` | Actor owner | 仅凭世界 owner 身份 |
|---|---:|---:|---:|---:|---:|
| 创建自己的 Actor | 必须 | 不要求 | 不要求 | 不适用 | 不要求 |
| 读取 Actor | 必须 | 任一项 | 任一项 | 允许 | 不允许 |
| 行为、转职、移动 | 必须 | 要求 | 不足以授权 | 允许 | 不允许 |
| 授权/撤销控制权 | 必须 | 不足以授权 | 要求 | 允许 | 不允许 |
| 推进世界 Tick | 必须 | 无关 | 无关 | 无关 | 允许 |

授权有两道检查：

1. `SubmitCommand` 在写入 pending command 前检查当前 active membership、Actor 所有权或 grant；世界 owner 不会自动获得其他成员 Actor 的控制权；
2. Tick 执行再次锁 Actor 并检查 active grant，防止提交后撤权、成员停用或并发竞态。

非 owner 成员提交成功后命令保持 pending，由世界 owner 或 F7.8 可信调度器推进。前端不会冒充 owner 调用 `step`，而是明确显示“已进入世界队列”并跟踪终态回执。

## 6. 初始位置

角色创建和 v5→v6 升级使用同一个确定性解析器：

1. 从 Z=0 Overmap Tile 中选择 `abs(chunk_x)+abs(chunk_y)` 最小者；
2. 并列时依次按 `chunk_x`、`chunk_y` 排序；
3. 放置在该 Chunk 的整数中心 Cell；
4. 绑定 `chunk.z0.x{chunk_x}.y{chunk_y}`；
5. 从 Tile 推导 `jurisdiction_code`。

算法不读取数据库 ID、墙钟或随机数，因此同一空间基线在不同数据库中得到相同位置状态。

## 7. 规则作用域

Rule 定义支持以下 `scope_kind`：

| Scope | 解析依据 | `scope_code` 示例 |
|---|---|---|
| `world` | 固定全世界 | `world` |
| `organization` | Actor 的 active `category_code=organization` Role | `organization.transit_authority` |
| `jurisdiction` | Actor 当前位置的 District | `central` |
| `location` | 锚点、精确位置、Chunk 或空间 | `building.city_hall`、`position.z0.x17.y16`、`chunk.z0.x0.y0`、`space.city_grid.primary` |

location 按锚点 → 精确位置 → Chunk → 空间的顺序匹配，并把 `matched`、`resolved`、`matched_by` 和 `location_ref` 写入 Case payload。规则若未匹配 scope，不创建 Case，也不执行 Effect。

该解析器只回答“规则是否适用”，不在规则层修改位置。后续法律、组织纪律、任务区域、危险地带和设施准入应注册新 Rule 或 resolver，而不是在活动代码中写辖区 if/else。

## 8. Canonical、重放与恢复

v6 canonical 在 v5 runtime 尾部追加：

```json
{
  "locations": [],
  "control_grants": []
}
```

稳定排序：

- Location 按 `actor_code`；
- Grant 按 `actor_code, capability, granted_tick, code`；
- Fact/Effect 继续按 `tick, sequence`。

重放从创世或快照开始逐 Effect 归约：

- `location.set` 新建或替换指定 Actor 位置；
- `control.grant` 追加新 grant；
- `control.revoke` 校验前态后替换生命周期状态。

每 Tick 归约后再次稳定排序，保证执行写入顺序不会影响 canonical hash。恢复流程先验证 snapshot hash，再清理受保护投影，按 Actor、Fact、Location、Grant 的依赖顺序恢复，最后运行数据库 foundation assertion 并比较目标 hash。缺 Actor、缺位置、未知 Fact 引用或 grant 前态不一致都会硬失败。

## 9. 查询与前端契约

现有 API 无需增加旁路 mutation endpoint：

```text
GET  /api/v1/city/worlds/:world_id/runtime/catalog
GET  /api/v1/city/worlds/:world_id/runtime/actors
GET  /api/v1/city/worlds/:world_id/runtime/actors/:actor_code
GET  /api/v1/city/worlds/:world_id/runtime/actors/:actor_code/roles
GET  /api/v1/city/worlds/:world_id/runtime/rules
GET  /api/v1/city/worlds/:world_id/runtime/cases
GET  /api/v1/city/worlds/:world_id/members
GET  /api/v1/city/worlds/:world_id/commands
GET  /api/v1/city/worlds/:world_id/commands/:command_id
POST /api/v1/city/worlds/:world_id/commands
POST /api/v1/city/worlds/:world_id/step
```

Actor 列表只返回当前用户拥有或获授能力的 Actor，并附权威 Location。Actor 详情额外返回：

- 当前用户的 `capabilities`；
- 当前 Location；
- 仅对具备 `actor.control.manage` 的用户返回控制 grant 历史。

前端在 Overmap 和 Local Scene 叠加 Actor：

- `@`：单一 Actor；
- `&`：同一位置存在多个 Actor；
- inspector 列出当前位置 Actor 并可定位；
- 工作台显示 XYZ、Chunk、Local、管辖区、空间和锚点；
- 移动、行为、转职、授权按钮依据服务端 capabilities 禁用；
- mutation 只使用局部 pending，不清空地图和工作台，成员命令排队时不调用 owner-only Step。

## 10. 已验证门禁

当前测试覆盖：

1. v6 新世界与 v5→v6 既有 Actor 升级；
2. owner 基线位置与两项能力；
3. 严格移动/控制 payload 规范化；
4. 委托成员提交移动、owner 推进、撤权后读取与执行拒绝；
5. 平面移动、位置事实和 grant/revoke 生命周期重放；
6. canonical、snapshot、projection drift、verified recovery 一致；
7. 直接 SQL 修改位置/grant 被触发器拒绝；
8. Actor glyph、位置 inspector、地图定位、移动与委托组件；
9. owner 即时封账与 member 排队两条前端 mutation 路径。

## 11. 后续依赖顺序

F7.7 之后仍需按底层依赖推进：

1. **已完成 F7.8 成员与调度闭环**：城市成员目录、用户选择器、持久 lease、失败退避、可信 Tick scheduler 和命令回执；
2. **已完成 F7.9 通行与占用**：Terrain/Furniture/Building/Site passability、碰撞、Portal 类型、确定性寻路和移动重校验；
3. **下一步 F7.10 动态通行控制**：Door/Portal 状态、锁、credential/organization/rule 权限、NPC 移动预算和拥堵预留；
4. **F8 公共设施底座**：电、水、数据、污水/垃圾的网络拓扑、容量、流量、故障和服务范围；
5. **F9 交通与物流**：道路图、站点、线路、通勤、载具和货物流，消费 Actor 与企业场所起讫位置；
6. **开放世界运行层**：NPC scheduler、需求、物品/容器、关系、任务、法律执行和时间预算；
7. **产品奖励桥**：只从已封账世界事实生成幂等奖励 Outbox，并与平台货币账本对账。

利率、信贷和股票仍是城市模拟经济分支，不能替代上述城市本体，也不能绕过既有复式账本、资源守恒、Actor、Rule、Fact 和 Effect 协议。
