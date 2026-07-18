# 城市模拟 F7.2 CLASSIC 字符前端实施与验收

版本：v1.0（2026-07-18）  
目标引擎：`city-f7-v1`  
前置阶段：F7.0 空间规则、F7.1 Overmap/Chunk 事实闭环  
状态：已实现并通过前端验收；下一阶段为 F7.3 地块、分区和建筑事实底座

## 1. 阶段边界

F7.2 只把 F7.1 已存在的真实空间状态变成可检查、可导航的 CLASSIC 字符视图，不增加客户端推断事实。闭环为：

```text
世界成员授权 API
  → 世界绑定规则与 Overmap
  → 视口 bbox Chunk 摘要
  → 单 Chunk 规范 payload
  → renderer-neutral scene projection
  → PixiJS bitmap glyph atlas
  → 坐标拾取、检查器、Z 导航与文本导出
```

客户端不根据人口、区域容量或名称生成地块、建筑、NPC、车辆和物品；不存在的 Chunk 明确显示为未加载。生成 Chunk 仍必须提交 `spatial.generate_chunk` 命令并由世界 tick 应用，前端没有地图 PUT 或本地写回通道。

## 2. 页面与导航

- 认证用户和管理员的个人导航均提供“城市模拟”入口，路由固定为 `/city`。
- 页面内世界切换通过 Pinia 状态完成，不进行整页导航或浏览器刷新。
- URL 的 `world` 查询参数用于深链接当前世界；非法或无权限 ID 由世界列表回退，不用于绕过服务端授权。
- 页面使用三段工作区：命令甲板、Z 深度轨、地图与检查器；窄屏退化为纵向布局，不另建功能缩水的移动页面。

## 3. API 接入

前端 `citySpatial` API 模块覆盖：

```text
GET  /city/worlds
POST /city/worlds
GET  /city/worlds/:id/spatial/ruleset
GET  /city/worlds/:id/spatial/overmap
GET  /city/worlds/:id/spatial/chunks?min_x=&max_x=&min_y=&max_y=&z=
GET  /city/worlds/:id/spatial/chunks/:chunk_x/:chunk_y/:z
GET  /city/worlds/:id/spatial/changes
POST /city/worlds/:id/commands
POST /city/worlds/:id/step
```

所有写请求沿用现有幂等键；Chunk 生成先提交带 `expected_world_tick` 的命令，再执行同预期 tick 的 step。客户端只有在返回命令状态为 `applied` 后更新世界 tick，并重新读取服务端摘要与 payload。

## 4. 状态模型

`useCitySpatialStore` 分离以下状态：

1. 世界目录和当前世界；
2. 当前世界不可变规则绑定与 Overmap；
3. Chunk 摘要索引；
4. 有界 Chunk payload 投影缓存；
5. 相机、视口、Z、视图模式和选择；
6. 空间 mutation 历史；
7. 首次加载、后台刷新、Chunk 加载、创世和生成命令状态。

世界切换使用 generation token 丢弃过期响应；同一 Chunk 的并发详情请求按世界和稳定 Chunk key 合并。后台刷新保留当前 scene，加载完成后再发布新投影，因此刷新按钮、视图切换和相机操作不会卸载整页或闪白。世界绑定规则哈希与规则内容哈希不一致时拒绝渲染；同一已加载世界的规则绑定若在普通刷新中变化，同样作为协议错误拒绝，避免旧 Chunk 被新规则静默解释。

## 5. Chunk 缓存与预取

- 缓存使用容量 64 的 LRU；键固定为 `z:{z}/x:{chunk_x}/y:{chunk_y}`。
- bbox 由相机中心、实际视口、cell size 和一圈预取环计算，并裁剪到服务端 Overmap 边界。
- 负世界坐标使用数学下取整，和后端坐标协议一致。
- 列表只取摘要；revision 或 payload hash 与缓存相同则复用，变化时才读取详情并重投影。
- 相机拖动、缩放、ResizeObserver 和 Z 切换以短延迟合并 bbox 请求，避免每个指针像素触发网络调用。
- 页面离开时清理计时器、ResizeObserver、Pixi 场景、bitmap font 和 GPU/Canvas 资源。

## 6. Scene projection

投影层不依赖 Vue 或 Pixi，负责：

- 验证 `city-chunk-v1`、32×32 尺寸和 RLE 精确覆盖；
- 验证家具局部坐标、重复位置和定义 kind；
- 解析 definition、palette、`looks_like` 与 missing fallback；
- 将 xterm-256 索引稳定转换为 RGB；
- 按 furniture > terrain 生成当前 CLASSIC 主 glyph，同时保留完整语义 stack；
- 将 Chunk 局部坐标转换为带负数支持的世界坐标；
- 生成 local/Overmap scene、坐标命中测试和规范文本导出。

投影失败不会以问号吞掉结构错误。只有规则明确提供的 `missing.<kind>` 用作未知定义回退；RLE 越界、不完整 payload 和哈希/摘要不一致均停止加载并显示错误。

## 7. PixiJS CLASSIC renderer

- 使用 PixiJS v8 `Application` 与按规则 glyph 集动态安装的 `BitmapFont` atlas。
- terrain/furniture 背景按颜色批量合并绘制；glyph 使用 bitmap text，不创建每格 DOM。
- 禁用 antialias，Canvas 使用整数尺寸与 `image-rendering: pixelated`。
- renderer 不运行常驻 ticker；仅 scene、选择、生成标记或尺寸变化时显式 render。
- 每个视口拥有唯一 font 名，卸载时注销 atlas，避免多实例互相覆盖。
- WebGL 初始化失败时保留带 `role=alert` 的明确错误层，而不是让页面空白。

Overmap 直接读取真实 `road_mask`、`river_mask`、terrain definition 和 Chunk 摘要；已生成、当前选择和未加载三种状态同时通过图形/边框表达，不只依赖颜色。

## 8. 交互与可访问性

完整输入映射：

| 输入 | 操作 |
| --- | --- |
| 拖动 / 方向键 | Local 相机平移 |
| 滚轮 / `+` / `-` | 五级整数像素缩放 |
| `Shift+滚轮` / `[` / `]` | 切换 Z |
| `0` | 回到地表 |
| `M` | Overmap / Local 切换 |
| `Enter` / 双击 | 进入当前 Overmap Tile 或检查选择 |
| `Esc` | 返回 Overmap |
| `?` | 打开快捷键帮助 |

所有快捷键都有可点击控件或帮助说明。地图宿主可聚焦、声明 `role=application` 和本地化标签；当前坐标与语义层通过节流后的屏幕阅读器摘要表达。输入框、文本域、选择器和 contenteditable 获得焦点时不执行地图快捷键。降低动画偏好会停止非必要进度运动。

## 9. 检查器与导出

检查器只显示服务端事实和规则定义：

- Overmap：Chunk 坐标、district、terrain ID、道路/河流 mask、variant、tile hash 和生成状态；
- Local：世界/Chunk/局部 XYZ、terrain/furniture、glyph、移动成本、flags、revision、generated tick 和 payload hash；
- stack：逐层列出 kind、definition ID、名称、glyph、移动成本和 flags。

文本导出按 y-major 输出选中 Chunk 的 glyph 行，并附带坐标、revision、payload hash 和 generated tick，供可访问性、错误报告和重放诊断使用。

## 10. 错误与并发语义

- 首次加载失败显示可重试错误；后台刷新失败保留旧地图。
- 重复点击刷新在已有刷新进行时直接复用当前状态，不创建并发全页 loading。
- 旧世界响应、摘要与详情不一致、规则 hash 不一致和命令 rejected 都不会写入缓存。
- Chunk 加载计数支持重叠 bbox 请求，只有最后一个完成后才结束局部 busy 指示。
- busy 只显示地图顶边的细进度状态，不替换地图节点，所以不会造成整组件闪烁。

## 11. 验收结果

已覆盖 16 个 F7.2 专项测试：

1. xterm 色板、负坐标、RLE、家具叠层、fallback、Overmap mask、命中测试、bbox 和文本导出；
2. LRU 读取提升、更新和容量淘汰；
3. 世界整体读取、刷新保留旧 projection、真实命令+tick 生成 Chunk；
4. renderer 键盘/滚轮映射、Overmap 选择和激活。

前端全量结果：183 个测试文件、1228 个测试全部通过；`vue-tsc --noEmit`、专项 ESLint 和 production build 通过。构建仍报告项目既有的大 chunk 与静态/动态混合 import 警告，但不影响 F7.2 正确性，后续应在独立性能切片按实际网络测量处理。

## 12. 下一阶段约束

F7.3 必须先建设地块、分区、建筑 footprint、楼层、unit 和 portal 的后端事实链，再让本视口投影这些层。不得在前端根据道路旁空地随机画房屋，也不得直接修改 Chunk payload：

```text
版本化 zoning/建筑规则
  → city-f7-v2 升级基线
  → parcel/building 命令
  → immutable land/construction facts
  → 当前投影 + canonical hash
  → replay/recovery
  → 授权空间查询
  → CLASSIC structure/portal layer
  → 可选 TILES renderer
```

像素图块只改变渲染，不改变检查器、碰撞、容量、所有权或状态哈希；F7.3 后端闭环未完成前不发布伪 TILES 内容。
