# 城市模拟空间系统与字符/像素可视化详细设计

版本：v0.4（2026-07-18）  
状态：总体设计冻结；F7.0–F7.3 已实现，当前阶段为 F7.4 动态开发项目  
适用范围：城市 Overmap、区域地图、建筑内外、离散 Z 层、NPC、车辆、物品、字段效果和分析覆盖层

## 1. 决策摘要

本项目采用“语义字符优先、像素图块增强”的正交俯视空间系统，不采用纯 DOM ASCII，也不以等距 2.5D 或完整 Three.js 三维作为主视图。

核心决策：

1. 模拟状态与显示方式分离。同一地形、建筑、NPC 或物品必须同时拥有稳定语义 ID、字符回退和可选像素图块。
2. 世界按 World → Overmap → District → Chunk → Cell → Z-level 分层；只加载视口需要的 Chunk。
3. Z 轴使用离散整数层。地铁、地下室、地面、楼层、屋顶和高架均使用同一坐标模型。
4. 地图生成由世界种子、规则集版本、Chunk 坐标、Z 层和生成器 ID 唯一决定；生成结果首次落库后不再隐式重算。
5. 后端是唯一事实来源。前端动画、NPC 插值和视觉特效不能反向改变模拟状态。
6. 当前人口仍是 cohort 聚合模型。普通可见 NPC 是确定性视觉代理；重要角色拥有永久身份。不得假装每个视觉代理都是后端真实居民。
7. F7.0 不生成伪地块、伪建筑或伪 NPC；F7.1 只建立真实基础地形、道路、水体与静态植被 Chunk。地块、建筑和 NPC 仍必须等待各自版本化事实模型。

## 2. 参考系统与取舍

### 2.1 Cataclysm: Dark Days Ahead

借鉴：

- Overmap 提供抽象世界上下文，局部地图只在需要时生成。
- 固定尺寸局部区域和数据驱动 Mapgen。
- 字符模板、地形/家具/物品定义和可组合 palette。
- `looks_like` 图形回退链；没有专用图块时最终回退到字符。
- 模拟定义和美术素材解耦，同一规则可使用不同 tileset。

不照搬：

- 不固定采用 CDDA 的 24×24 Mapgen 尺寸。本项目 Chunk 统一为 32×32，便于索引、裁剪和后续分级。
- 不把玩家视野泡直接等同于城市模拟边界。城市宏观经济始终运行，局部实例只负责空间细节。
- 不复制战斗、辐射、怪物或生存物品规则。

参考：

- [CleverRaven/Cataclysm-DDA](https://github.com/CleverRaven/Cataclysm-DDA)
- [CDDA Overmap 定义](https://docs.cataclysmdda.org/JSON/OVERMAP.html)
- [CDDA Mapgen](https://docs.cataclysmdda.org/JSON/MAPGEN.html)
- [CDDA JSON 图形回退](https://docs.cataclysmdda.org/JSON/JSON_INFO.html)

### 2.2 Dwarf Fortress

借鉴：

- 离散 Z-level，而非连续浮点高度。
- 字符、前景色和背景色构成高密度基础语义。
- 当前层为主、相邻层按洞口和结构关系有限透视。
- 物体材料、所有权、状态和历史通过检查面板表达，不要求地图单格显示全部信息。
- 由多个简单系统交互产生事件历史和涌现叙事。

不照搬：

- 不继承 Code Page 437 的 256 字符限制，也不让大量无关对象永久共用同一个渲染身份。
- 不在城市规模第一版中永久实例化全部居民、全部物品和全部路径。
- 不复制难以发现的多层快捷键；所有键盘操作都必须有可见控件和可搜索命令。

参考：

- [Dwarf Fortress Z-level](https://dwarffortresswiki.org/index.php/Z-level)
- [Dwarf Fortress 字符与图形系统](https://dwarffortresswiki.org/index.php/Graphics)

## 3. 当前系统边界

当前 `city-f7-v1` 已包含世界、区域、独立家庭数量和 household movement、企业、政府、总账、资源、市场、日历、自然人口变化、人口迁移，以及已绑定规则/生成器的 Overmap 与按需 Chunk。仍没有以下世界事实：

- 地块边界、道路拓扑和建筑占地。
- 非地表 Cell 与跨 Z portal。
- 建筑楼层、房间、门、楼梯和电梯。
- 空间 NPC、车辆或物品堆。
- 地块/建筑/NPC 等后续空间对象的版本链。

当前前端已通过 F7.2 显示 F7.1 的真实基础 Chunk，但不能从区域容量推断地块、建筑或居民。所有后续对象仍必须通过显式迁移、状态哈希、快照、回放和恢复接入现有引擎。

## 4. 空间层级与坐标

```text
World
└── Overmap
    ├── District
    │   └── Chunk (32 × 32 cells, one z-level)
    │       └── Cell (x, y, z)
    ├── Road/Rail/River graph
    └── Site/Building footprint
```

### 4.1 坐标约定

- `x` 向东递增。
- `y` 向南递增。
- `z` 向上递增，地表基准为 `0`。
- Chunk 坐标使用数学地板除法，负坐标必须稳定映射。
- Local 坐标范围固定为 `0..31`。
- 所有模拟坐标为整数；前端像素位置不进入规范状态。

```text
chunk_x = floor_div(x, 32)
chunk_y = floor_div(y, 32)
local_x = floor_mod(x, 32)
local_y = floor_mod(y, 32)
```

### 4.2 Z 层语义

```text
z < 0   地下管线、停车场、地铁、地下设施
z = 0   地面、道路、河流、建筑入口
z > 0   建筑楼层、屋顶、高架、山地上层
```

跨层连接必须是显式 portal：楼梯、坡道、电梯、竖井或跌落边。路径算法不能仅因为两个 Cell 的 `x/y` 相同就允许跨层移动。

## 5. 空间定义协议

静态定义进入版本化规则集；动态状态只引用稳定 ID。首批定义类型：

- `terrain`：水、土壤、道路、地板、轨道。
- `structure`：墙、门、窗、屋顶、桥、楼梯。
- `furniture`：桌椅、设备、路灯、货架。
- `field`：烟、火、水流、污染、噪声、施工影响。
- `item`：货物、工具、原料、消费品。
- `actor_archetype`：视觉代理或重要角色的基础外观。
- `vehicle_part`：车身、车门、货舱、轨道车辆部件。

每个定义至少包含：

```json
{
  "id": "road_asphalt",
  "kind": "terrain",
  "glyph": "=",
  "foreground": "gray_12",
  "background": "gray_02",
  "sprite": "terrain/road_asphalt",
  "looks_like": "road",
  "movement_cost": 100,
  "flags": ["OUTDOOR", "ROAD"]
}
```

规则：

1. `id` 在规则集内唯一，不能在同一规则集版本中改变语义。
2. `glyph` 必须是一个可渲染 grapheme；主规则集同时提供 ASCII 安全回退。
3. `sprite` 是本地资产键，不允许成为远程 URL。
4. `looks_like` 必须无环，最大解析深度为 16。
5. 找不到精确 sprite 时沿 `looks_like` 查找；仍不存在时使用 glyph；缺少 glyph 时显示错误字符 `?`。
6. 显示字段不参与移动、容量、生产或经济规则。

## 6. Cell 内容与显示优先级

一个 Cell 是多个语义层的组合，不是一张最终图片：

```text
Cell
├── terrain_id
├── structure_id?
├── furniture_id?
├── fields[]
├── item_stack_ref?
├── actors[]
├── vehicle_part_ref?
└── annotations[]
```

字符模式每格只能显示一个主 glyph，优先级固定为：

```text
cursor/target
> dangerous field
> actor
> vehicle part
> visible item stack
> furniture
> structure
> terrain
```

像素模式可绘制多个层，但检查结果必须与字符模式相同。物品堆、多人同格和复合建筑部件通过右侧检查面板展开，不能靠闪烁多个字符表达。

## 7. 地图生成

### 7.1 两级生成

1. Overmap 生成道路、河流、铁路、区域边界和 Site 占位。
2. Chunk Mapgen 根据 Overmap 上下文生成道路断面、地块、建筑、楼层和装饰。

生成随机源只允许来自：

```text
SHA-256(
  simulation_version,
  world_seed,
  spatial_ruleset_hash,
  generator_id,
  generator_version,
  chunk_x,
  chunk_y,
  z
)
```

禁止使用数据库序列、墙钟、进程随机源或加载顺序作为地图生成输入。

### 7.2 生成生命周期

```text
missing
  → generating (transaction-local)
  → generated + canonical payload + hash
  → modified through immutable spatial facts
```

首次生成成功后保存规范 payload、生成器版本和哈希。升级生成器不会静默重生成旧 Chunk；旧世界继续使用旧生成器，或通过显式升级创建新空间基线。

### 7.3 模板与 palette

Mapgen 允许字符模板，但字符只是模板 token，不等于最终 glyph：

```text
################
#......+.......#
#......+.......#
#......#.......#
####+###########
```

palette 把 `#`、`.`、`+` 映射为候选定义或嵌套生成器。模板必须在加载规则集时验证尺寸、未知 token、跨层 portal 对称性和定义引用。

## 8. 数据持久化与哈希

F7 预期新增以下投影；最终字段以迁移实现为准：

| 投影 | 作用 |
| --- | --- |
| `city_spatial_rulesets` | 规则集版本、内容哈希、状态 |
| `city_overmap_tiles` | 抽象道路、区域、Site 和地形上下文 |
| `city_map_chunks` | Chunk 身份、生成器版本、当前 revision、payload hash |
| `city_spatial_mutations` | tick 内不可变空间变更事实 |
| `city_spatial_mutation_lines` | Cell/对象级前后状态和版本链 |
| `city_spatial_objects` | 需要永久身份的建筑、重要角色、车辆和物品容器 |

不为每个空 Cell 建立一行数据库记录。Chunk 保存规范编码后的稠密基础层和稀疏对象层；变更以不可变事实推进投影。

主城市规范状态不内嵌全部 Chunk payload，而是包含：

- 空间规则集哈希。
- 已生成 Chunk 数量。
- 按 `(z, chunk_y, chunk_x)` 排序的 Chunk 哈希根。
- 永久空间对象摘要。

初版哈希根可以对排序后的 Chunk 坐标、revision 和 payload hash 做一次 SHA-256；只有全量重算成为可测瓶颈时才引入 Merkle tree。

## 9. 模拟层级与 NPC

### 9.1 三类居民表达

| 类型 | 是否永久 | 运行位置 | 用途 |
| --- | --- | --- | --- |
| cohort | 是 | 全世界 | 人口、就业、住房和消费聚合状态 |
| visual proxy | 否 | 活跃 Chunk | 人流和日常活动可视化 |
| named actor | 是 | 全世界或活跃 Chunk | 玩家角色、关键 NPC、任务和历史人物 |

visual proxy 由 `(world seed, tick window, district, cohort, slot)` 稳定生成，不拥有独立货币或永久物品。它的消失不代表居民死亡或迁出。

named actor 必须有永久 ID、当前位置、所属 cohort、关系和状态版本；卸载 Chunk 只停止高频路径计算，不能删除其身份。

### 9.2 活跃与非活跃区域

- 宏观经济、人口和日历不因 Chunk 未加载而停止。
- 活跃 Chunk 执行局部寻路、车辆移动和可见字段动画。
- 非活跃 Chunk 只保留永久对象状态、预约事件和聚合流量。
- 任何“实例化/聚合”转换都必须保持人口、货币、资源和永久物品守恒。

第一版不把所有普通居民永久个体化。只有玩法确实需要追踪个人历史时，才将 visual proxy 晋升为 named actor。

## 10. 路径与交通依赖

路径分三级：

1. Overmap 图：城市级道路、铁路和区域连接。
2. Chunk portal 图：Chunk 边界入口、建筑入口和跨 Z portal。
3. Local grid：当前 Chunk 内 Cell 寻路。

长距离 NPC 不允许对整座城市执行单次三维 A*。先选择 Overmap/portal 路线，再在当前局部求解。交通系统 F9 复用相同空间图，但不会在 F7 第一批提前实现完整车流模型。

## 11. 渲染架构

```text
City API / Tick result
        ↓
Vue spatial store
        ↓
Scene projection (renderer-neutral)
        ├── CLASSIC glyph renderer
        ├── TILES sprite renderer
        └── ANALYSIS overlay renderer
```

### 11.1 技术选择

- Vue 负责路由、时间控制、详情、列表、命令和可访问文本。
- PixiJS Canvas 负责地图、glyph、sprite、选中框和场景动画。
- CLASSIC 使用位图字体 atlas，不为每个 Cell 创建 DOM 节点。
- TILES 使用 sprite atlas，并启用整数像素对齐和 nearest-neighbor 缩放。
- 两种主渲染器消费同一 scene projection，切换模式不重新请求 API。
- 不在 F7 使用 Three.js；任意透视镜头、连续旋转和真实光照不属于当前玩法需求。

### 11.2 场景层

```text
08 DOM UI             时间、检查器、事件、命令
07 analysis overlay   热力图、路径、区域、容量
06 cursor/effect      光标、目标、危险字段、动画
05 actor/vehicle      NPC、动物、车辆
04 item               物品堆、货物
03 furniture          家具、设备
02 structure          建筑、墙、门、屋顶
01 terrain            水、土地、道路、地板
00 background         世界边界和未加载区域
```

### 11.3 Z 层显示

- 当前层完整显示。
- 当前层上方只显示会遮挡当前层的屋顶/楼板轮廓，可一键隐藏。
- 当前层下方仅通过洞口、楼梯、玻璃和 X-Ray 模式显示。
- 深度轨显示当前 Z、地表 Z、最低已知层和最高已知层。
- `[` / `]`、鼠标滚轮加修饰键和可点击按钮执行同一操作。

## 12. 页面布局与操作

```text
┌ WORLD / DISTRICT / LOCAL ─ TIME ─ SPEED ─ Z ─ VIEW MODE ┐
│                                                         │
│  MAP CANVAS                              INSPECTOR       │
│  terrain / buildings / actors            cell stack      │
│                                           object state     │
│                                           recent history   │
│                                                         │
├ EVENT LOG ─ FILTER ─ FOLLOW ─ JUMP TO ─ REPLAY ──────────┤
└ COMMAND BAR / CONTEXT ACTIONS / STATUS ──────────────────┘
```

必要操作：

- 拖动或方向键平移；滚轮缩放。
- `[` / `]` 切换 Z；`0` 返回地表。
- `m` 在 Overmap 与 Local Map 间切换。
- `/` 搜索区域、建筑、角色、物品和事件。
- `Enter` 检查当前 Cell；`Esc` 返回上级。
- 点击事件跳转至发生位置和 tick。
- 热力图只能覆盖地图，不能替代原始数值和图例。

所有快捷键必须提供按钮、标题和帮助列表。焦点进入输入框时不能触发地图命令。

## 13. API 草案

首批只读接口：

```text
GET /api/v1/city/worlds/:world_id/spatial/ruleset
GET /api/v1/city/worlds/:world_id/overmap?bbox=...
GET /api/v1/city/worlds/:world_id/chunks?min_x=&max_x=&min_y=&max_y=&z=
GET /api/v1/city/worlds/:world_id/chunks/:chunk_x/:chunk_y/:z
GET /api/v1/city/worlds/:world_id/spatial/objects/:object_id
GET /api/v1/city/worlds/:world_id/spatial/changes?after_tick=&after_sequence=
```

写入继续走 `city_commands`，不创建绕过 tick 的地图 PUT：

```text
spatial.generate_chunk
parcel.zone
building.plan
building.cancel
structure.demolish
```

Chunk 响应至少返回坐标、revision、规则集哈希、payload hash、规范 payload 和永久对象摘要。首版使用 JSON；只有抓包和性能测试证明 JSON 是瓶颈时才增加二进制协议。

## 14. 一致性、回放与恢复

空间系统必须满足：

1. 相同版本、种子和坐标生成相同规范 Chunk hash。
2. 已封账空间 mutation 和 line 不可修改或删除。
3. 每条 line 保存对象/Chunk revision 的 before/after 链。
4. 一个成功 tick 内空间 fact sequence 连续。
5. Chunk 投影、空间哈希根、世界 state hash 和 tick snapshot 在同一事务提交。
6. Replay 从版本匹配的基线按空间事实重建 Chunk hash 根。
7. Recovery 只由 verified replay 驱动，并记录不可变恢复审计。
8. 旧引擎版本不读取或解释新空间事实；升级创建同 tick 新版本基线。

## 15. 安全和内容边界

- 规则集和 tileset 不允许脚本执行。
- 资源键只能映射到构建产物或管理员允许的本地包，不能直接加载用户 URL。
- 地图查询必须先执行世界成员授权。
- bbox、Chunk 数量、Z 范围和响应大小必须限流与限界。
- Mapgen 定义在启用前完成 schema、引用、循环、尺寸和资源预算检查。
- 用户命令只表达意图；服务器重新校验位置、权限、成本和预期 tick。

## 16. 性能策略

- 前端只保留视口 Chunk 和一圈预取区域。
- 静态层合并绘制；动态 actor、字段和光标单独更新。
- 只应用 changed cell/object patch，不因一个 NPC 移动重建完整 Chunk。
- 地图隐藏或页面失焦时暂停视觉 ticker，不暂停服务端城市 tick。
- 远景隐藏普通物品和 visual proxy，只显示建筑、道路和聚合流量。
- 服务端地图生成受单世界事务锁和幂等键保护。
- 首版采用排序 Chunk hash 全量根；只有实际测量不足时升级为增量树。

## 17. 可访问性

- CLASSIC 和 TILES 都提供同一检查文本。
- 当前 Cell、坐标、Z、主要对象和危险状态通过 `aria-live` 的节流摘要公布。
- 不只用颜色表达危险、阵营、品质或通行性；同时使用 glyph、边框或图案。
- 支持降低动画、禁止屏幕抖动和关闭闪烁。
- 地图可完全通过键盘操作，且提供可搜索命令面板。
- 提供纯文本视口导出，用于调试、辅助技术和问题报告。

## 18. 实施顺序

### 前置：F6.3（已完成）

1. 家庭数量从人口数量中分离。
2. 家庭形成/合并和收入层迁移进入不可变事实。
3. 完成 F6 v2→v3 升级、规范哈希、回放和恢复。

具体命令、事实和守恒协议见《城市模拟 F6.3 家庭生命周期与收入层迁移详细设计》。

### F7.0：空间规则与坐标（已完成）

1. 已定义严格空间规则集及 glyph/sprite/looks_like 协议。
2. 已实现 32×32 Chunk、负坐标数学下取整、Cell 往返、稳定 key 和 `-32..127` Z-level 规范。
3. 已实现严格 JSON、重复键/未知字段拒绝、无环回退、语义规范化和固定 SHA-256。
4. 已内置 `sub2api-classic@1.0.0`，黄金 hash 为 `136ce6b71a6ebd0f9db4fdfe2662dc7530485330e565e0a7feebcec4399b5277`。
5. 已提供认证规则集列表/详情 API；精确实现契约见《城市模拟 F7.0 空间规则与坐标底座详细设计》。

### F7.1：Overmap 与 Chunk（已完成）

1. 已建立规则/生成器 profile、81 Tile Overmap、Chunk 和空间 mutation/line 表及写闸门。
2. 已实现固定 seed/规则/坐标的确定性 Overmap 与 RLE Chunk Mapgen。
3. 已接入 `city-f7-v1` 规范哈希、snapshot、逐 tick replay 和 verified recovery。
4. 已提供 ruleset、Overmap、bbox Chunk、Chunk 详情和 changes 游标授权 API。

### F7.2：CLASSIC 前端（已完成）

1. 已实现 Vue 空间 store、摘要索引、64 Chunk LRU、请求合并、过期响应隔离和视口一圈预取。
2. 已实现 PixiJS v8 bitmap glyph atlas renderer、Overmap/Local scene projection 和显式按需 render。
3. 已实现相机、坐标拾取、Z 切换、检查器、键盘/指针操作、帮助和规范文本导出。
4. 刷新保留当前 scene，不卸载页面；生成 Chunk 只通过真实 command + tick。
5. 详细契约与验收证据见《城市模拟 F7.2 CLASSIC 字符前端实施与验收》。

### F7.3：地块、建筑与 TILES

1. 地块、分区、建筑 footprint、楼层和 portal。
2. 建筑计划命令和不可变空间变更。
3. 像素 tileset、主题切换和分析覆盖层。

### 后续

1. F8 公用设施接入空间位置。
2. F9 交通复用道路/portal 图。
3. 重要角色、车辆和永久物品逐步实例化。
4. 城市本体稳定后再增加奖励 Outbox、信贷和证券分支。

## 19. 验收标准

设计进入实现后，每批至少通过：

- 同种子同规则集的地图黄金哈希测试。
- 负坐标、Chunk 边界和多 Z portal 测试。
- CLASSIC/TILES 对同一 Cell 的检查结果一致性测试。
- 缺失 sprite、`looks_like` 链和 glyph 最终回退测试。
- Chunk 并发首次生成只产生一个事实和一个投影。
- 生成失败回滚且可使用同幂等键安全重试。
- 视口分页、授权、bbox 限界和响应大小测试。
- 空间事实重放、漂移检测和 verified recovery 测试。
- Chunk 卸载/重新加载不复制人口、货币、资源或永久物品。
- 长周期 tick 中未访问 Chunk 不影响宏观人口与经济结果。

## 20. 明确暂缓

以下内容没有当前依赖，不在首批实现：

- 连续三维地形和任意旋转摄像机。
- 实时光线追踪、物理刚体和破坏模拟。
- 完整玩家 Mod 沙箱和远程素材市场。
- 所有居民永久个体化。
- 全城市每 tick 的逐 Cell 流体和气体模拟。
- 为性能未经测量的问题预先引入二进制网络协议或 Merkle tree。

这些能力只有在现有玩法和测量数据明确要求时才新增。
