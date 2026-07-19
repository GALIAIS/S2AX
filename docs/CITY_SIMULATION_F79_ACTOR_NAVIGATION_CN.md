# 城市模拟 F7.9 Actor 通行、占用与确定性导航详细设计

版本：v1.0（2026-07-18）
状态：已实现
模拟版本：`city-f7-v7`
协议版本：`navigation_version = 1.0.0`

## 1. 目标

F7.9 把 F7.7 的“Actor 可以向相邻坐标提交移动”升级为由权威空间事实约束的导航闭环。地图不再只是显示层；Terrain、Furniture、Building、Portal、已生成 Chunk 和其他 Actor 的当前位置共同决定一格是否可进入。路径查询和 Tick 内最终移动使用同一套解析器，避免前端显示可达而服务端按另一套规则执行。

本阶段解决以下基础问题：

1. 从绑定的版本化空间规则集读取 `passable` 与整数 `movement_cost`。
2. 把建筑边界、入口、楼梯、未生成 Chunk、Void 和 Actor 占用统一为 Cell 通行协议。
3. 提供有界、可取消、稳定排序的确定性 A* 路径查询。
4. 每个 `actor.location.move` 在实际 Tick 事务中重新校验目标，防止预览后世界变化造成穿墙或重叠。
5. 路径返回世界 Tick、规则哈希、逐步成本和空间锚点，使 UI、NPC、任务、交通与审计可以识别结果属于哪一版世界。

F7.9 不把路径查询当成预留或移动事实。查询不写数据库；只有封账成功的移动命令才产生 Fact、Effect 和 Actor 位置新版本。

## 2. 版本边界

`city-f7-v6` 的 canonical 字节保持冻结。F7.9 通过显式升级路径：

```text
city-f7-v6 --f7_v6_to_f7_v7--> city-f7-v7
```

升级本身不创建可变投影行。它只把既有空间规则、Chunk、Building、Portal、Actor Location 与 Occupancy 绑定到新的导航语义，然后按 `city-f7-v7` 重新生成规范快照和状态哈希。旧世界不会因部署新代码而静默改变移动规则。

数据库迁移 `204_city_actor_navigation.sql` 同步完成：

- 注册 `city-f7-v7` 引擎与 `actor_navigation` capability；
- 注册唯一的 v6→v7 升级边；
- 扩展历史投影保护函数，使 v7 继续满足 F7.0–F7.7 的全部基础断言；
- 保持 Actor Spatial Profile `1.1.0`，不伪造新的基线事实。

## 3. 权威输入与优先级

导航只读取同一世界的权威投影：

1. `city_spatial_profiles`：Chunk 尺寸、XYZ 边界、规则集 ID/版本/哈希。
2. 绑定规则集：Terrain/Furniture/Portal 定义的 passability 和 movement cost。
3. `city_overmap_tiles` + `city_districts`：地表 Chunk 是否属于世界及其管辖区。
4. `city_map_chunks`：已经生成的 Terrain Runs 和 Furniture。
5. `city_buildings`：建筑三维包围盒和状态。
6. `city_building_portals`：入口和楼梯的有向/双向连接。
7. `world_actor_locations`：所有 active Actor 的规范占用。

Cell 解析顺序固定：

```text
世界边界
  → 地表管辖区 / 非地表空间
  → active Building 包围盒
  → 已生成 Chunk Terrain
  → Furniture 覆盖
  → Portal 覆盖
  → active Actor Occupancy
  → 规范 Anchor / Jurisdiction
```

建筑内部由 `terrain.floor` 提供基础成本；建筑边缘默认是墙，仅 Portal 所在 Cell 可以跨越。非建筑的非地表 Cell 是 Void。地表 Chunk 未生成时返回 `chunk_not_generated`，服务端不会在只读路径查询中偷偷触发 Mapgen。

## 4. 通行规则

### 4.1 平面移动

- 支持八方向相邻移动。
- 正交步成本等于目标 Cell 的整数 movement cost。
- 对角步成本使用 `ceil(cost × 1414 / 1000)`，不使用浮点数。
- 对角线两侧的正交 Cell 都必须可通行，并且从起点到两侧均满足 Portal 边界；否则返回 `corner_blocked`。
- 起点允许包含当前 Actor 自己；任何目标 Cell 上的其他 active Actor 都会阻塞。

### 4.2 建筑与 Portal

- 室外↔室内只能沿 `entrance` Portal。
- 不同建筑之间不能通过相邻包围盒直接穿越。
- Z 层变化只能沿明确连接两端坐标的 `stair` Portal。
- 楼梯成本至少为规则集中的 `portal.stairs_up` movement cost。
- Portal 的方向性由 `bidirectional` 决定。

### 4.3 位置锚点

每个成功移动目标都会由解析器重新推导：

- Chunk Cell：规范 Chunk anchor；
- Building Cell：规范 Building anchor；
- Site anchor：仅在请求的 Site 仍通过既有 Site/Building 一致性校验时保留；
- Jurisdiction：由目标地表 Chunk 或 Building 所属 District 推导。

客户端不能伪造 anchor 或 jurisdiction 来绕过空间规则。

## 5. 确定性路径算法

路径查询使用有界 A*：

- 邻居顺序固定为北、东北、东、东南、南、西南、西、西北，再按 Portal 稳定顺序追加跨层邻居；
- 优先队列依次比较 `g+h`、`h`、Z、Y、X；
- 启发式使用规则集内最小正通行成本，保持可采纳；
- 相同 Cell 的更高成本或相同成本但不更浅路径不会替换父节点；
- 返回路径从起点开始，逐步记录 `step_cost`、`total_cost`、anchor 和 jurisdiction；
- 默认最多 256 步，允许 1–1024 步；展开节点最多为 `min(max_steps × 64, 65536)`；
- 每 64 个展开节点检查请求取消，避免断开连接后继续占用 CPU。

没有路径不是服务端错误，而是结构化结果：

```text
outside_world
chunk_not_generated
terrain_blocked
furniture_blocked
building_wall
void
actor_occupied
portal_required
corner_blocked
search_limit
unreachable
```

## 6. 一致性与并发

路径查询在 PostgreSQL `REPEATABLE READ READ ONLY` 事务中完成。成员权限、Actor 控制权、Actor 起点、world tick、规则绑定、Chunk、Building、Portal 和 Occupancy 均来自同一个数据库快照。响应中的 `world_tick` 与该快照一致。

路径仍是建议，不是预留：

1. UI 在 Tick N 查询路径。
2. 其他 Actor 可能在 Tick N+1 占用下一格。
3. 客户端提交下一步 `actor.location.move`。
4. Tick 执行事务重新加载最新导航上下文并校验。
5. 若目标已被占用，命令以 `WORLD_NAVIGATION_CELL_OCCUPIED` 终态拒绝，Actor 位置不变。

其余执行拒绝码：

- `WORLD_NAVIGATION_UNAVAILABLE`：Chunk 或导航基础不可用；
- `WORLD_NAVIGATION_CELL_BLOCKED`：Terrain/Furniture/Building/Void 阻塞；
- `WORLD_NAVIGATION_PORTAL_REQUIRED`：缺少 Portal 或对角切角；
- `WORLD_NAVIGATION_CELL_OCCUPIED`：被其他 active Actor 占用。

命令拒绝仍进入私有命令回执，不会让整个 Tick 回滚。

## 7. API

### 7.1 查询路径

```http
POST /api/v1/city/worlds/:world_id/navigation/path
Content-Type: application/json

{
  "actor_code": "actor_00000002",
  "destination": { "x": 18, "y": 12, "z": 0 },
  "max_steps": 256
}
```

调用者必须是 active world member，且必须拥有该 Actor 的 owner 权限或 active `actor.command` grant。响应示例：

```json
{
  "navigation_version": "1.0.0",
  "world_tick": 42,
  "spatial_rule_hash": "...",
  "actor_code": "actor_00000002",
  "from": { "x": 16, "y": 12, "z": 0 },
  "to": { "x": 18, "y": 12, "z": 0 },
  "reachable": true,
  "total_cost": 160,
  "expanded_nodes": 3,
  "steps": [
    {
      "coordinate": { "x": 16, "y": 12, "z": 0 },
      "step_cost": 0,
      "total_cost": 0,
      "anchor_kind": "chunk",
      "anchor_code": "chunk.z0.x0.y0",
      "jurisdiction_code": "district.central"
    }
  ]
}
```

### 7.2 执行移动

路径执行继续复用版本化命令：

```json
{
  "command_type": "actor.location.move",
  "payload": {
    "actor_code": "actor_00000002",
    "x": 17,
    "y": 12,
    "z": 0
  },
  "expected_world_tick": 42
}
```

每条命令只移动一格。长路径不会在一个 Tick 内瞬移，也不会生成无法部分审计的批量位置覆盖。

## 8. 前端闭环

空间工作台已接入：

- 在 CLASSIC 视口选择 Cell 后，可为当前 Actor 预览路径；
- 可达路径用独立 overlay 绘制，不修改底层 Cell 投影；
- 工作台显示目标、可达原因、步数、总成本、展开节点和前若干步；
- “执行下一步”只提交路径的第二个坐标；
- 移动封账后自动基于新 Tick 重新规划剩余路径；
- 切换世界、Actor、地图 Cell、刷新或提交其他运行时命令时清除旧路径；
- store 使用 generation token 丢弃慢请求的过期响应，避免快速选择时旧路径覆盖新路径；
- 所有状态更新保持局部，不触发整页刷新或卸载视口。

## 9. 验收证据

已覆盖：

1. Terrain、Furniture、自身占用和其他 Actor 占用解析；
2. Building edge、Entrance、Stair 和跨层成本；
3. 对角切角阻断；
4. 固定路径、稳定成本和重复运行一致；
5. 缺失 Chunk、步数/节点上限和请求取消；
6. `city-f7-v6 → city-f7-v7` 升级不改变 Actor Spatial/Control 投影；
7. 真实迁移数据库中的 Mapgen Terrain、跨成员 Occupancy、移动成功及占用拒绝；
8. API 请求结构、前端 stale response、路径 overlay、工作台预览和下一步执行。

## 10. 后续依赖

F7.9 固化的是“静态空间事实 + 当前 Actor Occupancy”的确定性基础。按依赖顺序，下一切片是 F7.10 动态通行控制：

1. Door/Portal open、closed、locked 的版本化状态与 Fact/Effect；
2. Key、credential、organization、role 和 rule 驱动的通行授权；
3. NPC movement intent、路径失效与逐 Tick 重规划预算；
4. 可选的短期 Cell/Edge reservation，解决大批 Actor 对向拥堵；
5. Hazard、field、天气和交通模式的动态成本层。

完成动态通行控制后，F8 公共设施、F9 交通物流、任务和城市法律执行都必须复用这一导航协议，不得各自维护一套“可达性”旁路。
