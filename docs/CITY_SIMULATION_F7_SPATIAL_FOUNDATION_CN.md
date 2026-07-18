# 城市模拟 F7.0 空间规则与坐标底座详细设计

版本：v1.0（2026-07-18）  
前置引擎：`city-f6-v3`  
状态：F7.0–F7.2 已实现并通过验收；下一阶段为 F7.3 地块与建筑  
关联文档：《城市模拟空间系统与字符/像素可视化详细设计》《城市模拟 F6.3 家庭生命周期与收入层迁移详细设计》

## 1. 阶段目标

F7.0 只建立后续所有空间状态共同依赖的确定性语义，不提前创建地块、建筑、道路、NPC 或可玩地图。完成后应具备：

1. 对负坐标、Chunk 边界和离散 Z 层定义唯一且跨平台一致的换算规则；
2. 建立可数据驱动、可验证、可哈希的空间规则集；
3. 同一语义对象同时提供字符显示、颜色令牌和未来像素图块回退链；
4. 规则集内容和哈希可通过认证只读 API 获取，供前端、地图生成器和诊断工具复用；
5. 无效规则、循环回退、重复语义 ID、非法字符和越界参数在启动或测试阶段立即失败。

F7.0 不改变任何世界投影和规范状态，所以不发布新的世界模拟版本。`city-f7-v1` 只在 F7.1 首次把 Overmap/Chunk 状态绑定到世界、快照、重放和恢复时发布。

## 2. 依赖顺序

```text
F6.3 household_units 与家庭事实闭环
  ↓
F7.0A 坐标、Chunk 与稳定键
  ↓
F7.0B 规则集 Schema、校验和规范哈希
  ↓
F7.0C 内置基础规则集与只读 API
  ↓
F7.1A city-f7-v1 / 世界规则集绑定
  ↓
F7.1B Overmap、Chunk Mapgen 与空间 mutation
  ↓
F7.1C snapshot / replay / recovery
  ↓
F7.2 CLASSIC 字符前端
  ↓
F7.3 地块、建筑与 TILES
```

后续阶段只能引用 F7.0 的坐标和语义 ID，不得各自重新定义除法、Chunk 大小、Z 轴方向或字符含义。

## 3. 坐标规范

### 3.1 坐标系

- 世界坐标为有符号 `int64`：`x` 向东增加、`y` 向南增加、`z` 向上增加；
- `z=0` 是地表基准，负数是地下，正数是楼层、高架或空中层；
- 默认 Chunk 固定为 `32 × 32` Cell；同一 Chunk 只包含一个 Z 层；
- F7.0 默认规则只允许 `-32 <= z <= 127`，规则集可以收紧但不能超出引擎安全界限；
- Cell 是离散整数位置，不保存浮点坐标。

### 3.2 负坐标换算

语言默认的“向零截断”不能直接用于 Chunk 坐标。必须使用数学下取整：

```text
chunk_x = floor(world_x / chunk_size)
local_x = world_x - chunk_x × chunk_size

chunk_y = floor(world_y / chunk_size)
local_y = world_y - chunk_y × chunk_size
```

因此：

| 世界坐标 | Chunk | Local |
|---:|---:|---:|
| `0` | `0` | `0` |
| `31` | `0` | `31` |
| `32` | `1` | `0` |
| `-1` | `-1` | `31` |
| `-32` | `-1` | `0` |
| `-33` | `-2` | `31` |

正向与反向转换必须互逆，并在乘法、加法前检查 `int64` 溢出。

### 3.3 稳定键与排序

Chunk 稳定键采用文本：

```text
z:{signed-z}/x:{signed-chunk-x}/y:{signed-chunk-y}
```

该键用于日志、API 游标、快照诊断和缓存命名，不作为数据库主键。数据库稳定排序固定为 `(z, chunk_y, chunk_x)`；视口绘制固定为 `(z, world_y, world_x)`。任何哈希都不得依赖 map 迭代顺序或数据库未声明顺序。

## 4. 空间规则集

### 4.1 顶层结构

规则集使用严格 JSON，顶层字段为：

| 字段 | 语义 |
|---|---|
| `id` | 稳定规则集 ID，如 `sub2api-classic` |
| `version` | 只递增版本，如 `1.0.0` |
| `name` | 显示名称，不参与业务判断 |
| `chunk_size` | F7.0 必须为 32 |
| `min_z` / `max_z` | 可用离散 Z 范围 |
| `palette` | 语义颜色令牌目录 |
| `definitions` | 地形、家具、结构、入口、物品、实体、字段与覆盖层定义 |

加载器必须拒绝未知字段、重复 JSON key、尾随内容和空规则集。成功加载后先规范化排序，再计算规范 JSON 的 SHA-256；API 返回的 `content_hash` 始终是 64 位小写十六进制。

### 4.2 Definition

每个 definition 包含：

| 字段 | 约束 |
|---|---|
| `id` | 全规则集唯一，格式 `[a-z][a-z0-9_.-]{1,63}` |
| `kind` | `terrain`、`furniture`、`structure`、`portal`、`item`、`entity`、`field`、`overlay` |
| `name` | 非空 UTF-8，最多 64 个 Unicode 字符 |
| `glyph` | 可选，恰好一个可打印 Unicode rune |
| `foreground` / `background` | 引用 palette token；背景可为空 |
| `looks_like` | 可选，引用同 kind definition，必须无环 |
| `sprite` | 可选稳定像素图块 ID；F7.0 不加载图片 |
| `movement_cost` | `0..10000`；不可通行对象必须为 0 |
| `flags` | 排序去重后的稳定能力集合 |
| `metadata` | 只读扩展对象，不能改变引擎核心语义 |

显示解析顺序固定为：

```text
自身 sprite
→ looks_like 链上的 sprite
→ 自身 glyph
→ looks_like 链上的 glyph
→ kind 对应的 missing definition
```

每种 kind 必须有 `missing.<kind>` 终点；missing definition 必须有 glyph，且不能再声明 `looks_like`。

### 4.3 颜色与主题边界

规则集保存语义令牌而不是硬编码页面颜色，例如：

- `surface.default`
- `surface.water`
- `structure.wall`
- `entity.civilian`
- `danger.warning`

每个 palette 项只定义稳定的 CLASSIC 前景/背景色索引和语义用途。网页深浅主题、色盲方案和像素 tileset 在客户端将同一令牌映射为实际颜色；模拟和 Mapgen 不得读取 RGB 值做规则判断。

### 4.4 核心 Flag

F7.0 首批只承认以下稳定 flag：

- `passable`
- `transparent`
- `supports_roof`
- `supports_weight`
- `liquid`
- `flammable`
- `outdoor`
- `indoor`
- `portal_up`
- `portal_down`
- `openable`
- `closable`
- `hazard`
- `blocks_items`

未知 flag 必须被拒绝，避免拼写错误静默进入地图。后续增加 flag 必须提升规则集版本并补充兼容测试。

## 5. 内置规则集

首个规则集 `sub2api-classic@1.0.0` 至少覆盖：

- 8 类 missing 回退；
- void、地面、草地、土壤、浅水、深水、道路、人行道；
- 墙、地板、关闭/打开的门、窗、楼梯、电梯；
- 树木、桌椅、床、路灯；
- 家庭居民、工作人员、政府人员、车辆；
- 通用物品堆、烟、火、选择框、路径和危险覆盖层。

这些定义是显示/生成语义，不代表 F7.0 已创建真实地图实体。

## 6. 服务与 API

认证用户可读取规则目录：

```text
GET /api/v1/city/spatial/rule-sets
GET /api/v1/city/spatial/rule-sets/:rule_set_id
```

列表只返回 `id`、`version`、`name`、`content_hash`、Chunk/Z 约束和 definition 数量；详情返回规范化完整内容。接口不接受上传、修改或删除，响应对象加载后不得被调用方修改全局注册表。

错误语义：

| 错误码 | 含义 |
|---|---|
| `CITY_SPATIAL_RULE_SET_NOT_FOUND` | 规则集 ID 不存在 |
| `CITY_SPATIAL_RULE_SET_INVALID` | 内置/加载规则不合法；属于启动或服务器错误 |

F7.1 已通过 `city_spatial_profiles` 在世界上保存规则集 ID、版本与 hash。世界创建后只能通过显式引擎升级切换规则，不能跟随进程中的“默认最新版”静默变化。

## 7. 确定性与安全约束

1. 规则 JSON 不允许脚本、URL、模板表达式或运行时反射；
2. 单规则集最多 4096 个 definition，回退深度最多 32；
3. 所有字符串先验证 UTF-8 和长度，再参与哈希；
4. canonical hash 只基于规范化语义内容，不包含加载路径、构建时间或服务器环境；
5. 注册表初始化一次，返回深拷贝，调用方不能污染后续请求；
6. 坐标转换不分配大数组，不根据不受信任坐标创建文件路径；
7. F7.1 Mapgen 的随机输入固定为世界 seed、规则 hash、Chunk 坐标、Z 和生成器 ID。

## 8. 验收门禁

### 8.1 坐标

- 表格边界、负坐标和随机往返属性测试；
- `MinInt64` / `MaxInt64` 与 Chunk 原点溢出测试；
- 相邻 Chunk 局部坐标连续性；
- 稳定键在负数和多 Z 情况下唯一。

### 8.2 规则

- 严格 JSON、ID、kind、颜色、flag、字符、数量和范围测试；
- duplicate、missing reference、跨 kind fallback、自环和多节点环测试；
- 输入 definition/palette 顺序不同仍得到同一 hash；
- 任意语义变化导致 hash 变化；
- 内置规则固定黄金 hash；
- 所有 definition 都能解析到 glyph，带 sprite 的对象仍有字符回退。

### 8.3 API

- 两条认证路由存在；
- 列表顺序稳定，详情 hash 与服务端重算一致；
- 不存在 ID 返回稳定错误；
- 修改一次响应对象不会改变下一次响应。

## 9. F7.0 完成定义

只有同时满足以下条件才能进入 F7.1：

1. 设计文档、类型、加载器、内置规则和 API 已提交；
2. 单元、属性、黄金哈希和路由测试全部通过；
3. 现有 F0–F6.3 单元与城市集成测试无回归；
4. 未修改 `CurrentCitySimulationVersion`，也未伪造任何地块、建筑或 NPC 数据；
5. F7.1 只能复用本阶段公开的坐标和规则解析结果，不得复制实现。

## 10. 实施结果

- 坐标与规则内核位于 `backend/internal/cityspatial`，不依赖数据库或墙钟，可由 Mapgen、API 和后续回放器共同复用。
- 内置 `sub2api-classic@1.0.0` 共包含 14 个 palette token 和 44 个 definition；固定规范 hash 为 `136ce6b71a6ebd0f9db4fdfe2662dc7530485330e565e0a7feebcec4399b5277`。
- 已实现负坐标/边界/溢出/随机往返测试，严格 JSON、回退环、跨 kind、非法 flag、glyph、passability、顺序无关哈希与响应深拷贝测试。
- 已注册 `GET /api/v1/city/spatial/rule-sets` 与 `GET /api/v1/city/spatial/rule-sets/:rule_set_id`。
- F7.0 验收时 `CurrentCitySimulationVersion` 仍为 `city-f6-v3`，且没有创建或伪造世界空间投影；F7.1 完成全部门禁后已显式提升为 `city-f7-v1`。
- F7.1 已复用本文件的坐标、规则解析和固定规则 hash，完成 Overmap/Chunk、空间事实、规范状态、重放、恢复与成员读取 API；F7.2 随后完成真实 CLASSIC 字符视口，下一依赖为 F7.3。
