# 城市模拟 F7.1 Overmap 与 Chunk 详细设计

版本：v1.1（2026-07-18）  
目标引擎：`city-f7-v1`  
前置引擎：`city-f6-v3`  
状态：已实现并通过验收；F7.2 CLASSIC 字符前端亦已完成，下一阶段为 F7.3

## 1. 阶段边界

F7.1 把 F7.0 的坐标与规则语义第一次接入真实世界状态，完成如下闭环：

```text
世界绑定规则集与生成器
  → 确定性 Overmap
  → spatial.generate_chunk 命令
  → 不可变空间 mutation
  → Chunk 投影
  → 世界规范哈希
  → snapshot / replay / recovery
  → 授权只读 API
```

本阶段只生成地表基础地形、道路、水体和静态植被，不创建地块、建筑、门户、永久 NPC、车辆或物品。上述对象分别属于 F7.3 及后续阶段，不能从区域容量或人口聚合数据伪造。

## 2. 版本与兼容

- 新世界默认使用 `city-f7-v1`。
- 现有 `city-f6-v3` 世界只能在 paused、无待处理命令且源快照有效时，经审计升级到 `city-f7-v1`。
- F5、F6 v1/v2/v3 的规范 JSON 形状与历史哈希保持不变；它们不读取空间表。
- F7 继承 F6.3 的日历、迁移与家庭生命周期能力，并在 `calendar_demography` 后、`markets` 前增加 `spatial` stage。
- 规则集和生成器绑定后不跟随进程默认值静默变化；未来更换必须发布新引擎升级路径。

## 3. 固定生成契约

首版固定：

| 项目 | 值 |
| --- | --- |
| 规则集 | `sub2api-classic@1.0.0` |
| Chunk | `32 × 32` Cell |
| Z 安全范围 | `-32..127` |
| 首版可生成层 | `z=0` |
| Overmap 范围 | `chunk_x/chunk_y = -4..4` |
| Overmap Tile 数 | `81` |
| 生成器 | `sub2api-classic-mapgen@1.0.0` |
| Chunk payload | `city-chunk-v1` |

生成输入固定为：

```text
simulation_version
world_seed
spatial_rule_set_hash
generator_id
generator_version
chunk_x
chunk_y
z
overmap tile context
```

所有伪随机位都从带长度前缀的 SHA-256 命名空间派生。不得使用墙钟、数据库 ID、进程随机源、请求顺序或 map 迭代顺序。

## 4. Overmap

Overmap 是 9×9 的有界 Chunk 级上下文。Tile 按 `(chunk_y, chunk_x)` 规范排序，每项保存：

- `chunk_x`、`chunk_y`、固定 `z=0`；
- 现有 F3 district 的稳定 `district_code`；
- `terrain_id`；
- 四方向 `road_mask` 与 `river_mask`；
- 生成器派生的 `variant`；
- 规范 `tile_hash`。

道路必须连通中心区并到达四个边界方向；河流形成连续轴线，水体与道路相交时 Chunk 仍只表达基础地形，不提前创建桥梁对象。区域归属使用稳定几何分区，不根据数据库行 ID 决策。

Overmap 根哈希为规范 Tile 数组的 SHA-256。首次创世或升级时一次性生成并落库，之后不会隐式重算。

## 5. Chunk payload

Chunk 使用 y-major 的 1024 Cell 稠密地形层和稀疏装饰层。为了避免一格一行，规范 JSON 使用游程编码：

```json
{
  "format": "city-chunk-v1",
  "width": 32,
  "height": 32,
  "terrain_runs": [
    {"definition_id": "terrain.grass", "length": 496},
    {"definition_id": "terrain.road", "length": 4}
  ],
  "furniture": [
    {"x": 3, "y": 7, "definition_id": "furniture.tree"}
  ]
}
```

约束：

1. `terrain_runs.length` 之和必须恰好为 1024；相邻同 ID run 必须合并。
2. definition 必须存在于绑定规则集且 kind 正确。
3. 稀疏项按 `(y,x,definition_id)` 排序，同 Cell/层不能重复。
4. payload hash 是规范 JSON 的 SHA-256；首次落库后生成器升级不能改写。
5. F7.1 不在 payload 中写入 structure、portal、actor、item 或虚构建筑。

## 6. 数据模型

### 6.1 `city_spatial_profiles`

一世界一行，保存规则集 ID/版本/hash、Chunk/Z 约束、生成器 ID/版本、Overmap 边界、revision 和 metadata。除审计恢复外不可修改或删除。

### 6.2 `city_overmap_tiles`

保存 81 个规范 Tile，主键为 `(world_id, chunk_x, chunk_y, z)`。Tile 引用同世界 district，并保存 tile hash。创世/升级后不可修改；恢复按确定性生成结果核验并修复。

### 6.3 `city_map_chunks`

保存已生成 Chunk 的坐标、district、生成器版本、revision、payload、payload hash、生成 tick 和来源 mutation。主键为 `(world_id, chunk_x, chunk_y, z)`。

### 6.4 `city_spatial_mutations` 与 lines

`spatial.generate_chunk` 在目标 tick 创建一条 `chunk_generated` mutation 和一条 line：

- before revision 为 0、before hash 为 null；
- after revision 为 1、after hash 等于 Chunk payload hash；
- header 只允许一次 draft → posted；
- header、line 封账后禁止更新和删除；
- mutation、命令、tick、Chunk 来源和 after hash 在 deferred constraint 中相互核对。

## 7. 写入协议

唯一首版空间写命令：

```json
{
  "command_type": "spatial.generate_chunk",
  "payload": {"chunk_x": 0, "chunk_y": 0, "z": 0},
  "expected_world_tick": 12
}
```

处理规则：

1. 提交时严格 JSON、拒绝重复/未知字段并检查整数范围。
2. 只有 `city-f7-v1` 支持该命令。
3. Step 持有单世界事务锁，读取绑定 profile 和 Overmap Tile。
4. 坐标越界、非地表层或 Chunk 已存在时，命令以稳定错误码 rejected；tick 继续完成。
5. 生成、mutation/line、Chunk、事件、世界 hash 和 tick snapshot 在同一事务提交。
6. 相同 step 幂等键返回同一 tick；并发首次生成最多产生一个 Chunk 和一个 mutation。

## 8. 规范状态

F7 的规范状态新增 `spatial`：

- profile 全部稳定字段；
- Overmap tile count、revision 和 root hash；
- `chunk_count`；
- `chunk_hash_root`；
- 按 `(z, chunk_y, chunk_x)` 排序的 Chunk 摘要：坐标、district code、generator、revision、payload hash、generated tick。

规范状态不内嵌 1024 Cell payload。Chunk 根哈希对排序后的摘要做一次 SHA-256；目前不引入 Merkle tree。

## 9. Replay 与 Recovery

- Replay 的 spatial stage 按 mutation sequence 读取已 posted facts，验证命令、before/after 链，并将 Chunk 摘要插入状态后重算根。
- Replay 不读取当前 Chunk 投影来决定结果；当前投影漂移不会污染重放。
- Recovery 从 verified snapshot 的 Chunk 摘要，使用快照中的世界 seed/profile 和 immutable Overmap 上下文重新生成 payload；生成 hash 不匹配立即失败。
- Recovery 对 profile、Overmap 和 Chunk 集合做精确恢复：缺失行补回，多余行删除，差异行更新。该能力只在 `city_recovery_write_enabled` 审计闸门内开放。
- 恢复结束后重新加载 live canonical，必须逐字节等于目标 snapshot。

## 10. 读取 API

```text
GET /api/v1/city/worlds/:world_id/spatial/ruleset
GET /api/v1/city/worlds/:world_id/spatial/overmap
GET /api/v1/city/worlds/:world_id/spatial/chunks?min_x=&max_x=&min_y=&max_y=&z=
GET /api/v1/city/worlds/:world_id/spatial/chunks/:chunk_x/:chunk_y/:z
GET /api/v1/city/worlds/:world_id/spatial/changes?after_tick=&after_sequence=&limit=
```

所有接口先验证活跃世界成员。Chunk bbox 每轴最多 9、总面积最多 81，Z 必须在 profile 范围；列表只返回摘要，单 Chunk 返回规范 payload。不存在或尚未生成返回稳定 404，不触发隐式生成。

## 11. 验收门禁

1. 同输入 Overmap/Chunk 黄金哈希稳定，输入变化会改变生成证明。
2. 负坐标与边界 Chunk 可生成，越界与非 `z=0` 被稳定拒绝。
3. RLE 恰好覆盖 1024 Cell，定义引用与排序可验证。
4. F6 历史 canonical 不增加 `spatial` 字段；F7 必须包含。
5. v3→F7 dry-run 不落库，apply 创建同 tick 新版本基线。
6. 生成命令形成完整 mutation、line、Chunk、event 和 snapshot。
7. tick 0→N replay 验证；人为漂移后 verified recovery 恢复逐字节 canonical。
8. 直接改写 profile、Overmap、Chunk、mutation 或 line 被数据库拒绝。
9. 路由、授权、bbox、游标和响应上限测试通过。
10. 全量 Go 单元、迁移静态检查、城市数据库集成与 `go vet` 通过。

## 12. F7.1 完成定义

上述闭环已全部通过，`CurrentCitySimulationVersion` 已切换为 `city-f7-v1`。F7.2 已在只读空间 API 上完成字符视口；F7.3 地块/建筑仍必须通过本阶段的空间命令、不可变 mutation、规范哈希和恢复协议接入。

## 13. 实施结果

- 已加入 `city-f7-v1` 引擎及 `city-f6-v3 → city-f7-v1` dry-run/apply 审计升级路径，新世界默认绑定 `sub2api-classic@1.0.0` 与 `sub2api-classic-mapgen@1.0.0`。
- 已建立 9×9 Overmap、按需 `spatial.generate_chunk`、RLE Chunk payload、mutation/line 不可变事实和数据库写闸门。
- F7 规范状态包含 profile、Overmap 根和排序 Chunk 摘要根；F5/F6 历史规范形状保持不变。
- spatial stage 已接入 tick、事件、快照、逐 tick replay 和 verified recovery；恢复会由 seed/规则/Overmap 重新生成 payload，并逐字节比较目标规范状态。
- 已开放 ruleset、Overmap、Chunk bbox/详情和空间变更游标五类世界成员读取接口；不存在的 Chunk 不会触发隐式生成。
- 黄金哈希、负坐标、越界拒绝、幂等、游标、授权、升级回滚、投影漂移恢复和直接写入拒绝均由单元、迁移和 PostgreSQL 集成测试覆盖。
