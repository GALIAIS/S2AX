# 城市模拟实时世界时钟与像素世界改动设计

版本：v1.2（2026-07-22）

状态：设计冻结前草案，R1 与 R2 的安全底座已部分实现。本文件同时记录已落地的边界与尚未开放的资产管线；除明确标为“已实现”的内容外，不表示已对用户开放。

适用范围：Sub2API 城市模拟的新世界、开放世界 Actor、地图/建筑/室内可视化、角色外观、城市 Agent 运行时的时间接口，以及与这些能力相关的资产发布、回放、权限、运维和测试。

依赖：

- [《城市模拟游戏总体设计》](CITY_SIMULATION_GAME_DESIGN_CN.md)
- [《城市模拟 Agent 化架构改动设计》](CITY_SIMULATION_AGENT_ARCHITECTURE_DESIGN_CN.md)
- [《开放世界生成器 V2 设计》](CITY_OPENWORLD_GENERATOR_V2_CN.md)
- 现有城市空间、Actor、导航、Rule/Case、经济、Fact、Effect、快照、回放和恢复契约

本文中的“必须”“不得”“只能”均为实现门禁；“建议”表示当前默认方案，若改动必须新建 ADR、版本化配置和测试，不能在实现中静默替换。

### 0.3 当前已实现的安全切片（2026-07-22）

以下能力已经进入代码、迁移和测试覆盖；它们是内容发布控制面，不等于“图像素材生产已经上线”。

| 项目 | 已实现行为 | 明确未实现 / 不得假设 |
| --- | --- | --- |
| 实时时间与投影 | 生产时钟、只读 realtime projection、semantic pixel chunk、Canvas 2D CSP-safe renderer 与前端局部重绘边界已存在 | 不以浏览器时间、每帧渲染或模型返回时间写 canonical world state |
| 视觉包基础 | `city_visual_packs`、`city_world_visual_bindings`、pack/manifest/asset-set hash、内置 `city-pixel-core@1.0.0` 程序化包已迁移 | 还没有 atlas 文件入库、对象存储下载、PNG/WebP 解码或可投产的素材上传接口 |
| 新世界绑定 | 新 realtime world 创建时，按 `city_visual_pack_release_policies` 的“精确 spatial profile → `*` 默认”顺序选择已发布包，并写入不可变 world binding | 不会改写既有 world 的 binding；策略指向已退役/不兼容包时创建必须失败，不能静默回退 |
| R3 动态 Actor 与 Agent 基座 | V2 genesis 会创建 6 个匿名 NPC、位置事件链与 member-safe 动态投影；同一批 Actor 绑定到 `system.root → system.npc_manager → character.npc` 的无模型 policy/lifecycle 树，巡逻只在树处于 active/autonomous 时执行 | 不返回 Agent tree、owner、人格、prompt、provider、memory 或模型输出；没有用户 Character Agent、模型 worker、NPC 生成/退场管理或即时模型驱动的移动 |
| 管理控制面 | 管理员可列出/查看暂存包，创建或更新程序化 manifest，创建受控生成申请，审阅/取消申请，发布/退役包，设置发布策略，查看审计事件；所有写操作有幂等键、管理员 service 边界与开关门禁 | 普通用户没有编辑、发布、策略或来源数据接口；浏览器接口不接受原始提示词、模型选择、来源图片、URL、Base64、SVG、脚本或二进制资产 |
| 生成申请 | 只保存 asset class、有限 semantic tags、像素尺寸、帧数和固定 prompt template ID；审计中没有原始 prompt | 尚无受控 worker 将候选图片规范化、审核并写入资产目录。因此申请处于未结束/已批准状态时仍阻止包发布；当前只能拒绝、取消或记录失败后发布无资产程序化包 |
| 功能开关 | `city_simulation_enabled → city_pixel_renderer_enabled → city_visual_pack_publish_enabled` 逐层 fail-closed；读审计和写发布分离 | 关闭开关不会删除视觉包、世界绑定、时钟段或审计记录 |

管理 UI 路径为 `/admin/city/visual-packs`，仅管理员可见；普通成员仍只通过 `/city` 读取其获授权的共享世界投影。该页面是“发布账本”，不是自由美术生成器。

---

## 0. 决策摘要

本次改动同时解决两个不同层面的需求，且必须保持边界：

1. 城市世界从“每次 scheduler 运行推进一个游戏小时”的 tick 模式，升级为由服务端可信 UTC 时钟驱动的实时世界时间；
2. 玩家地图从字符地图/ASCII glyph 呈现升级为原创的日式紧凑像素城市模拟呈现，地图、建筑、室内、人物和物品均由版本化像素素材包组合；
3. 时间权威、世界语义、物理碰撞、经济账本、法律结果和 Agent 行动仍在服务端。渲染器、浏览器时间、图像素材和模型输出都不是状态权威；
4. 不把浏览器 60 FPS、每一帧动画或每秒绘制操作写入数据库。实时是“世界因果按真实时间到期”，不是“每一个像素帧都成为 simulation tick”；
5. 现有 V24 世界继续保留其小时 tick 与 ASCII 兼容读取能力。实时世界必须使用新的 engine version、时间模型、视觉包和迁移语义；不自动把旧世界升级为实时世界；
6. “以原子时间/NTP 为准”具体落实为：服务器操作系统或受控时间服务通过 NTP/NTS 维持 UTC，同一应用进程用单调时钟防止倒退，数据库负责事务顺序与租约。应用不得在玩家请求、世界 reducer 或浏览器中直接向公网查询 NTP；
7. 像素素材将使用受控的图像生成工作流生成原创基础资产，但图像生成结果不是可直接投产的地图瓦片。必须经过像素化、网格、锚点、透明度、调色板、碰撞语义、内容审核、图集打包与版本冻结；
8. “参考开罗式紧凑日式像素城市模拟”只表示空间信息密度、清晰的人物/建筑层级、明快的像素可读性和经营模拟视角；不得复制任何现成游戏的美术、角色、界面、建筑图样、品牌、素材命名或受保护表达。

### 0.1 本设计冻结的核心原则

| 主题 | 冻结结论 |
| --- | --- |
| 时间来源 | 仅服务端可信 UTC；客户端时钟只用于动画和本地显示预估 |
| 时间精度 | canonical 时间使用 UTC 微秒时间戳；首版逻辑动作按冻结的时间量子结算，推荐 1 秒，不持久化渲染帧 |
| 共享在线世界 | 同一 `world_id` 只有一份 canonical 时间线、空间、经济、规则、人口、事实、状态 hash、due event 队列与 cursor；成员只获得各自的权限、角色控制和脱敏 projection，绝不获得个人世界副本 |
| 世界暂停 | 暂停期间世界时间冻结；运行中的世界会在服务故障恢复时按真实经过时间补结算，不把停服静默当作暂停 |
| 排序 | 同一时间量子内按到期时间、phase、稳定优先级、提交序列和实体 ID 决定，绝不依赖 goroutine 或数据库返回偶然顺序 |
| 回放 | 回放读取已封存的时间段、时钟段、命令、事实和版本向量；不重新查询当前 NTP、不重新调用模型、不重新随机生成视觉变体 |
| 旧世界 | V24 继续按 tick 语义运行；它的 current_tick 不能被重新解释成实时秒数 |
| 地图语义 | terrain、structure、furniture、portal、item、entity、field、overlay、occupancy 与访问规则来自后端，不从 PNG 或像素颜色反推 |
| 可视化 | 新世界只提供像素地图作为玩家地图。ASCII 可仅保留在旧 world、测试、诊断导出或无图形降级工具中，不能继续作为新世界主界面 |
| 资产 | 素材按 visual pack、atlas、asset ID、hash 和 manifest 版本冻结；同一世界不能因为“当前默认素材”悄悄改变外观 |
| Agent | Agent 观察的是脱敏语义状态和 timeline cursor，不读取像素，不把模型延迟当作世界时间，不直接写地图或账本 |

### 0.2 不是本次改动的内容

- 不在首版做 3D、等距自由旋转、完整光照追踪、真实物理、实时多人动作战斗或 60 Hz 权威碰撞；
- 不允许用户上传人物照片、真人脸、任意图片、SVG、脚本化素材或“让模型即时生成一个可用建筑”直接进入游戏世界；
- 不把世界的内部货币、平台余额、平台虚拟货币和视觉素材购买绑定为自动兑换；
- 不把用户加入 world 解释为“为该用户分配一座隔离城市”。个人 sandbox 如有需要，必须是显式、不同 `world_id` 的管理员创建物，不属于共享城市的成员加入路径；
- 不把 NTP 叫作“绝对原子时间证明”。它是受控 UTC 同步手段，仍需要偏移监控、时钟段、故障降级与审计；
- 不对旧 V24/旧 F7 世界做隐式数据转换、重画、重播、删表或覆盖；
- 不以“图像生成看上去好看”为理由绕过版权、内容安全、性能、可读性、焦点无障碍或状态可回放要求。

---

## 1. 当前基线、问题与目标架构

### 1.1 当前可复用的基础

当前城市系统已经具备以下可复用能力：

- 城市世界、版本向量、状态 hash、命令幂等、事实、Effect、快照、回放和恢复；
- 空间语义、worldgen profile、Overmap、Chunk、地块、建筑、室内、Portal、Actor、控制权、导航和占用；
- 时间上每个基础 tick 推进一个游戏小时，scheduler 用 world.next_tick_at 扫描到期世界；
- 当前前端使用 Vue 3、Pinia、Vite、PixiJS 8；其 CityClassicViewport 以 BitmapText、glyph 和 Graphics 组合显示字符地图；
- 现有 CitySpatialDefinition 已有 terrain/furniture/structure 等语义和可选 sprite 字段，但尚无完整视觉素材目录、图集、角色外观或像素渲染协议；
- 现有 Agent 草案已经约束模型不能直接写状态、账本、Rule/Case 或奖励。

这些能力是新版本的基础，不应被替换为浏览器状态或一张不可解释的大地图图片。

### 1.2 当前阻塞点

| 现状 | 不能直接满足的需求 | 本设计的处理 |
| --- | --- | --- |
| V24 tick 固定推进 1 个游戏小时 | 玩家角色、Agent、建筑活动和现实时间关系不直观；1 秒渲染不能等价于 1 个 canonical tick | 新引擎以 world elapsed time 和 due event 驱动，逻辑与渲染帧分离 |
| speed_multiplier 改调度间隔 | 在生产世界中可能把“现实时间”变成不可解释的加速时间 | 新实时 world 禁用生产 speed multiplier；仅测试时钟可显式启用 |
| ASCII 主地图 | 无法承载建筑外观、室内、人物外观、层级遮挡和高密度可读交互 | 建立语义到像素投影、图集和 Pixi 场景渲染 |
| 当前 Pixi 视图导入 unsafe-eval 模块 | CSP 环境会报错，且安全策略被绕开 | 新渲染器禁止该导入；在 CSP-safe 配置下启动，无法启动时受控降级 |
| glyph 与颜色部分承载视觉含义 | 改一个字形可能影响玩家理解，但没有版本化视觉契约 | 语义仍为权威，视觉通过 pack manifest 单独版本化 |
| 当前地图按 cell 生成文字对象 | 大范围/密集人物时 draw call、对象数和内存不可控 | 按 chunk、图集、批处理、可见区域和 LRU 管理 |
| 角色尚无模块化外观 | 不能让用户创建外观，也无法给 NPC 保持一致的视觉身份 | appearance profile、素材槽位、方向帧、层级和可回退图块 |

### 1.3 目标拓扑

~~~text
可信 UTC / NTP-NTS
        ↓
Clock Authority（单调保护、偏移/健康、时钟段）
        ↓
World Time Projection（运行/暂停/恢复后的世界时间）
        ↓
Realtime Scheduler ──→ Due Event Queue ──→ Temporal Transaction / Reducer
                                              ↓
                                  Facts / State / Snapshot / Event Cursor
                                              ↓
                          Semantic Chunk + Actor + Visual Manifest API
                                              ↓
Vue Store ──→ PixelSceneRenderer（Pixi WebGL）──→ Canvas/CSS 像素画面

Asset Authoring → QA/切片/图集 → Visual Pack Manifest ─┘

Agent Worker → Observation（语义、脱敏、带 cursor）→ Candidate Intent
             → 后续 Temporal Transaction 验证 → Fact / Effect
~~~

其中每一条横向边界均必须可独立失败：

- 时钟源异常时，世界进入可诊断的 degraded/recovery 状态，不使用客户端时间补写；
- 素材下载失败时，后端世界照常运行，前端显示明确的资产错误或受控低保真 fallback；
- 模型调用失败时，角色等待或走确定性日程，世界时间和像素地图不停止；
- 渲染器失败时，用户不能借此绕过命令权限或修改世界。

---

## 2. 术语、时间模型与非歧义定义

### 2.1 术语

| 术语 | 定义 | 不是 |
| --- | --- | --- |
| 可信 UTC | 服务器通过操作系统/受控时间服务同步并经健康检查的协调世界时 | 浏览器 Date.now、用户设备时间或每次请求即时公网 NTP |
| 单调时间 | 自进程启动后只增不减的 elapsed duration，用于抵御 wall clock 回拨 | 可跨重启持久化的真实日期 |
| Effective UTC | Clock Authority 根据最后可信 UTC 锚点加单调 elapsed 计算出的当前 UTC | 永远准确到原子钟的断言 |
| 世界时间 | world 在 running 状态下累积的实时经过时间；暂停区间从中剔除 | 必然等于当前现实日期的本地显示 |
| 时间量子 | canonical 逻辑可处理的最小离散粒度；首版建议固定 1 秒 | 浏览器每一帧或网络每一个数据包 |
| Temporal Frame | 将同一个时间量子内的 due event、命令和确定性派生结果封装的一次原子事务记录 | 旧 V24 的一个小时 tick |
| Due Event | 有明确 world_due_at、phase 与稳定排序键的未来事件 | 普通 UI 定时器或未验证的模型输出 |
| 时钟段 | 因服务器重启、偏移校正、暂停/恢复、迁移形成的一段可审计时间映射 | 可随意修改过去时间的设置 |
| 视觉包 | 将稳定空间/实体语义映射到图片资源、调色板、图层、锚点和变体规则的版本化内容包 | 城市真实物理和碰撞规则 |
| 图集 | 已切片像素资源按 hash 打包的纹理页与 metadata | 一张任意尺寸的整张世界地图 |
| Appearance Profile | Actor 使用的 body/hair/clothing 等素材槽位和色板引用 | 用户上传的原始真人图片或属性加成 |
| 视觉投影 | 基于 canonical state 和视觉包生成的只读可绘制描述 | 允许客户端写回空间状态的模型 |

### 2.2 NTP 与“原子时间”的正确边界

用户希望以 NTP/原子时间为准是合理的，但工程上必须区分：

1. 原子钟是时间基准的物理实现；应用服务器通常并不直接连接原子钟；
2. NTP 或更强的 NTS 是服务器同步 UTC 的协议。它会有网络抖动、层级、偏移和失联风险；
3. 服务器应用只能在同步后的系统时钟基础上建立“可信时间”，并持续监控偏移；
4. PostgreSQL 的 clock_timestamp 用于数据库事务中判断租约/到期与审计交叉校验，但它并不自动解决多节点时间漂移；
5. 客户端只接收 server_time，用于倒计时平滑和动画，不可提交自己的当前时间作为命令、奖励、移动或处罚依据。

因此，产品文案可称“服务器实时世界”，技术文档不得承诺“客户端看到的是绝对原子时间”。

### 2.3 三个时间轴

每个实时 world 同时存在三个不可混用的时间轴：

| 时间轴 | 数据类型 | 用途 | 是否可由客户端影响 |
| --- | --- | --- | --- |
| source UTC | timestamptz UTC | 日志、时钟健康、事件接收、外部审计 | 否 |
| world elapsed time | 微秒整数或 UTC anchor + duration | 活动持续时间、生产、到期、日夜、日历、角色行程 | 否 |
| render time | 浏览器高分辨率本地时钟 | 插值、镜头、粒子、光效、loading | 可本地变化，但不写后端 |

世界本地日期/时刻由 world elapsed time 与 world timezone 映射。DST、时区切换和本地日历规则必须由服务端按冻结 timezone data 计算，浏览器只显示结果。

---

## 3. 新实时引擎的版本与兼容策略

### 3.1 引擎版本边界

实时世界不是给 city-openworld-v24 增加一个 bool。必须新增独立 engine version，例如在实际实施时登记为 city-openworld-realtime-v1；最终名称由版本注册表冻结。

该版本向量至少包含：

| 向量项 | 说明 |
| --- | --- |
| engine version | Temporal Frame、命令、事件阶段、恢复规则和时间量子 |
| clock profile version | 时钟精度、健康阈值、暂停/恢复、离线追赶和日历策略 |
| spatial profile | 地形、道路、地块、建筑、室内与碰撞语义 |
| worldgen plan | 宏观地理到 Chunk 的确定性生成 |
| visual pack binding | 像素资产、图集、调色板、变体算法和角色衣橱 |
| content catalog | 建筑、物品、职业、设施、组织与行为目录 |
| rule bundle | 法律、处罚、任务和服务规则 |
| agent policy bundle | 若启用 Agent，定义其观察、行动、时间预算和模型边界 |

任何一项变化都必须可追踪到 hash。不得以“更新图片不影响逻辑”为理由让已运行世界无记录地换成不同视觉包；视觉不影响物理，但它影响玩家认知、截图、复盘和内容一致性。

### 3.2 旧世界处置

| world 类型 | 时间语义 | 玩家地图 | 允许的操作 |
| --- | --- | --- | --- |
| 历史 F7/兼容 world | 现有定义 | 现有兼容查看器 | 只读、现有明确迁移或诊断 |
| city-openworld-v24 | 每基础 tick 一游戏小时 | 旧 ASCII/Classic viewer 可保留 | 继续运行、回放、现有功能修复 |
| realtime-v1 新 world | running 时按服务器实时经过时间 | PixelSceneRenderer | 新建、测试、受控灰度 |
| V24 到 realtime 升级候选 | 迁移前冻结 | 迁移预览像素图 | 仅管理员暂停、预演、验证、显式确认后升级 |

首版不实现 V24 到 realtime 的生产升级按钮。原因不是数据不能转换，而是它同时改变了时间、事件、命令并发、Agent 触发和前端视觉，这不是一个可逆的小迁移。必须先在复制世界上完成预演、hash 检查和人工验收。

### 3.3 新世界创建

新 world 的创建请求必须明确选择：

- world engine：只允许管理员发布且用户有权使用的 realtime engine；
- spatial style profile：例如日式城市原型、中国城市原型等；它控制道路、地块、建筑拓扑和交通约束；
- visual style binding：与 spatial profile 兼容的视觉包；它控制素材、调色板和视觉变体；
- timezone：IANA 名称而非浏览器偏移；
- clock profile：默认生产实时 profile；测试 profile 仅管理员/测试环境可选；
- seed：由服务端生成或管理员受控提供，写入版本向量；
- 初始人口、密度、内容包和 Agent 是否可用：均为版本化 policy，不允许用户 body 任意覆盖。

创建成功后，必须先完成 genesis snapshot、world time anchor、clock segment、spatial root hash、visual binding 和初始 manifest hash，再将 world 标记为 running。

### 3.4 共享在线 world、成员加入与状态边界

新 realtime world 的创建发生一次，创建者只能是管理员或由管理员明确触发的受控自动化流程；创建者不是唯一玩家。普通用户不会因注册、登录、首次打开页面、创建角色或被分组策略命中而获得一个个人 world。它们只能进入一个已存在、已获准访问的 `world_id`，并在这座同一城市里拥有自己的成员身份和可选 Character Agent/Actor。

下表是不可改变的身份与状态划分：

| 层 | 同一 `world_id` 内是否共享 | 说明 |
| --- | --- | --- |
| canonical world | 是 | 世界时间、clock segment、版本向量、seed、Chunk、建筑、NPC/公共 Actor、经济/市场、Rule/Case、Fact/Effect、snapshot、state hash、due event 和 timeline cursor 均为唯一实例 |
| 成员/治理权限 | 否 | membership、role、邀请/封禁、可操作的治理范围按用户保存；它们改变授权，不改变 world state |
| 角色控制 | 否 | control grant 与 Character Agent 绑定具体 Actor；控制一个 Actor 不表示拥有或克隆整个 world |
| 私有资料与 Agent memory | 否 | 人格种子、私有记忆、未公开任务、私人物品细节和受限证据按 policy 脱敏；它们仍引用同一 world 的事实，不可成为第二条经济/地图/时间线 |
| 可见 projection | 可以不同 | 两名成员可因位置、建筑门禁、发现状态、隐私或管理员范围收到不同 DTO；每个 DTO 必须带同一 shared timeline cursor 与 view scope，而不是生成私有 state hash |

成员加入是现有 world membership 域的受控状态变更：请求/邀请/组策略满足后创建或恢复 membership，分配最小角色，并在需要时再创建或绑定该用户的 Actor 与 control grant。成员加入完成前不得预生成个人地图、复制 NPC、复制库存/市场、重置世界时间，或把该用户的角色放到一份私有 Chunk 数据里。world owner 的转让、成员封禁、角色撤销和用户退出也同样只影响权限/角色绑定，不能删除或分裂共享世界。

多人同时在线时，world scheduler 仍只持有一份 world write lease，所有因果写入沿同一 Temporal Frame 序列提交。成员 A 对公开门、道路、设施或自身可见移动的命令一旦被接受，产生的 Fact/Effect 与 cursor 对整个 world 唯一；成员 B 在有可见权限时收到这个 cursor 对应的 patch，而不是一份 A/B 各自独立推进的结果。并发命令使用 `Idempotency-Key`、expected timeline cursor、实体版本、容量/占用锁和稳定排序处理；竞争失败必须返回可解释的 stale/conflict/Rule 拒绝，不允许给不同成员写出互相矛盾的成功结果。

浏览器的连接状态只代表一个 delivery session。断线、重连、切换设备或多标签页不会暂停世界、冻结 Character Agent、改变经济速率或触发奖励；客户端以最后确认的同一 cursor 补拉。可选 online/presence 只能是短保留、可见范围受限的非权威投影，不能作为工资、任务、货币、处罚、NPC 生成或任何 world reducer 的输入。

未来若需要容量分片、赛季副本、测试 sandbox 或不同城市，必须显式创建新的 `world_id`/shard identity、独立世界时间和独立经济边界，并让 UI 明示“这是另一个世界”。按用户、会话、浏览器、Actor 或 projection scope 自动复制同一城市，均违反本设计。

---

## 4. 服务端可信时钟设计

### 4.1 Clock Authority

每个应用节点运行一个 Clock Authority，它向业务层提供：

~~~text
NowUTC() -> effective_utc, source_state, uncertainty, clock_segment_id
Health() -> healthy | degraded | unsafe
~~~

它不得把 wall clock 直接原样暴露给 reducer。推荐实现逻辑：

1. 在进程启动或一次可信校准成功时保存 anchor_utc 与 anchor_monotonic；
2. 当前 effective UTC = anchor_utc + monotonic_now - anchor_monotonic；
3. 定期读取系统 UTC、时间服务状态和可选数据库时间，对偏移、跳变、失联和不确定性做健康判断；
4. 小幅校正使用缓慢 slew 或新时钟段，不能使已经发给 reducer 的 effective UTC 倒退；
5. 超过阈值的前跳、后跳、无法确认源质量或跨节点偏移超标时，停止接收新的实时 world 写事务，进入 degraded/unsafe；
6. 恢复后创建新的 clock segment，保留旧段，按照明确的恢复策略继续。

### 4.2 时间源配置

生产部署必须支持以下 source mode，且选择由管理员的部署配置完成，不由 world 或用户修改：

| mode | 用途 | 允许生产 world | 说明 |
| --- | --- | --- | --- |
| system_ntp | 操作系统已使用可信 NTP 同步 | 是 | 默认；应用读取系统 UTC 和健康状态 |
| system_nts | 操作系统已使用 NTS 或私有可信时间服务 | 是 | 高安全部署优先 |
| private_time_service | 内网受控时间服务/API | 是 | 需记录源 ID、签名/健康与故障策略 |
| frozen_test_clock | 集成测试、回放、开发 | 否 | 只能测试环境/管理员隔离 world |
| manual_admin_clock | 演示或调试 | 否 | 禁止与奖励、真实 Agent 配额或共享经济 |

当前 R1 落地的最小生产适配器只实现 `system_ntp` / `system_nts` 的**显式受信任宿主机时钟**：

- 配置 `city_realtime_clock.enabled=true` 且 `trust_host_clock=true` 是必要但非充分条件；它表示运维已经保证宿主机由对应 NTP/NTS 服务持续校时；
- 适配器每次观测同时比较 wall clock 与进程单调 elapsed，超过 `maximum_wall_clock_step_us`、wall clock 回退、profile source 不匹配或声明不确定性超过 profile 上限，均拒绝提供观测；
- Temporal reducer 仍会在事务中对 PostgreSQL `clock_timestamp()` 做 profile 级 skew 校验，因而“宿主机配置开启”本身不能绕过数据库交叉校验；
- scheduler 只有在该适配器可运行、城市总开关和 `city_realtime_scheduler_enabled` 三者都启用时才会启动/领取租约；不属于当前 authority 的 production profile 只记录为未支持，不会被误写成 `unsafe`；
- `private_time_service` 是后续独立 provider 的工作项，不能通过增加任意 URL 配置到 host adapter，避免把时间配置变成 SSRF/凭据泄露入口。

当前运维 API 也冻结为无浏览器时间输入：

- `GET /api/v1/admin/city/clock-health`：管理员只读健康投影，省略 lease token、provider endpoint、原始错误和凭据；
- `POST /api/v1/admin/city/worlds/:world_id/pause` 与 `.../resume`：管理员、幂等键必填、无 JSON 时间字段；服务端先加载 world 固定 profile，再由 Clock Authority 产生观测；
- production world genesis 会写入一个 `system.realtime.noop` bootstrap due event（world time `0`）。第一条健康观测通过普通 canonical reducer 结算该事件并把状态转为 `healthy`，不以空秒帧填满时间线。

规则：

- 不在每个 HTTP 请求中向 pool.ntp.org 或任意公网主机发包；
- 不接受客户端提交的 server time、timezone offset、milliseconds 或“我已经等待了多久”；
- 运行时不得因时间源 URL、DNS 或 TLS 错误自动退化为用户浏览器时间；
- 部署层必须配置至少两个可验证源或一个受监控的企业时间服务，具体拓扑由运维 ADR 冻结；
- 时间源凭据、NTS cookie、内部地址和诊断原文不出现在用户 API、Agent observation 或普通日志。

### 4.3 多节点与数据库时间

实时调度可能在多个实例上运行，因此：

1. 每个节点启动后必须向 clock health registry 报告 node_id、offset estimate、uncertainty、last sync 和 source state；
2. scheduler 只从 healthy 节点领取 realtime world 租约；
3. 超过允许 skew 的节点不能取得租约，也不能提交 Temporal Frame；
4. 数据库使用 clock_timestamp 判断 lease 是否过期，并保存 transaction commit ordering；应用 Clock Authority 生成 effective world time；
5. 写事务中要校验应用 effective UTC 与数据库 UTC 的偏差仍在 clock profile 阈值内；异常时拒绝或降级，不编造时间；
6. 不以数据库行 ID、goroutine 执行顺序或不同节点收到消息的先后决定同一 due time 内事件顺序。

### 4.4 时钟健康状态机

~~~text
initializing → healthy
healthy → degraded       小偏移、校准陈旧、单源失效
degraded → healthy       连续稳定校验通过
healthy/degraded → unsafe 大回拨、大前跳、跨节点漂移、无法判定
unsafe → recovering      时间源恢复且已建立新段
recovering → healthy     时间段审计、待处理 frame 预演通过
~~~

| 状态 | 读 | 新命令 | Due Event | Agent 推理 | 管理员动作 |
| --- | --- | --- | --- | --- | --- |
| initializing | 显示启动中 | 拒绝 | 不领取 | 暂停创建 | 查看诊断 |
| healthy | 正常 | 接收 | 正常执行 | 可调度 | 正常 |
| degraded | 正常，展示时钟告警 | 可接受不依赖精确到期的读/低风险命令；高风险命令可排队 | 受 profile 限制 | 新推理可延后 | 查看/确认 |
| unsafe | 可读最后稳定快照 | 拒绝或仅保存不可执行草稿 | 不执行 | 不调度新 request | 修复时间源、恢复 |
| recovering | 可读并标识追赶 | 暂时拒绝世界写 | 受控追赶 | 不输出新 intent | 预演/继续/暂停 |

“高风险”首版必须保守：奖励资格、法律期限、交易、资产结算、角色死亡/永久退场和所有跨域到期 effect 均不得在 degraded 状态下猜测执行。

### 4.5 时钟段

每个实时 world 需要 append-only 的 world clock segment：

| 字段 | 含义 |
| --- | --- |
| segment_id | 稳定主键 |
| world_id | 所属 world |
| source_clock_mode | 当前时间源模式 |
| effective_utc_anchor | 本段生效时的可信 UTC |
| world_elapsed_anchor_us | 本段开始时的世界累计微秒 |
| monotonic_anchor_proof | 仅用于当前节点诊断的匿名化/哈希证明，不作为跨重启计算来源 |
| reason | create、resume、recover、source_switch、upgrade、test |
| uncertainty_us | 锚点估计不确定性 |
| profile_hash | 使用的 clock profile |
| created_by | internal scheduler 或管理员的受审计命令 |
| closed_at / close_reason | 后续段创建时封存 |

世界时间计算只能从最新有效 segment 推导；历史 frame 中写入 segment_id 与 world_time_from/to，使回放不需要调用当前时钟。

### 4.6 暂停、恢复、停服和维护

| 场景 | 世界 elapsed time | 事件处理 | 备注 |
| --- | --- | --- | --- |
| 管理员 pause 成功 | 冻结 | 未到期事件保持 pending | 维护窗口不消耗角色生活时间 |
| 管理员 resume | 从新的 segment 继续 | 后续 due time 平移到新 world time | 不补暂停区间 |
| 应用进程崩溃，world 仍 running | 持续流逝 | 恢复后按真实经过世界时间追赶 | 不能把故障默认为暂停 |
| 数据库不可用 | 不提交任何 frame | 恢复后按 running 语义处理 | 不靠内存补写 |
| 时钟 unsafe | 管理策略决定是否自动 pause 或 recovery hold | 不猜测期限 | 必须产生审计/告警 |
| 显式 archive | 冻结且只读 | 不执行 | 重新激活必须新 segment + 审计 |

停服期间的连续生产、饥饿、耐久或市场变化不能按“每秒循环”回放。它们必须由时间区间积分/离散到期事件的确定性算法处理，见第 5 节。

### 4.7 闰秒、格式与精度

时间实现必须冻结以下约定：

- API 与审计使用 RFC 3339 UTC，持久化精度为微秒；不把浏览器毫秒或浮点秒作为 canonical 值；
- 生产系统采用操作系统/受控时间源的明确闰秒策略。若系统使用 leap smear，则 smear policy/version 写入 clock profile；若系统以 step 处理，则 Clock Authority 必须防止 effective UTC 回退；
- 不在业务层使用本地时间字符串作为主键或排序键，所有本地时间都附带 timezone、calendar profile 与 UTC 结果；
- 日长、月长、季节和工作日由日历规则算出，不假设每一天总是 86,400 秒；
- 同一 due time 内的微秒精度不足以表达的顺序，继续用 phase/priority/稳定 ID 打破平局；
- 任何把时间序列转成图表、动画或日期文本的 API 都返回源时区与 cursor，不能由前端重新猜测。

---

## 5. 实时推进模型：Temporal Frame 而不是渲染 tick

### 5.1 时间量子

首版 realtime-v1 固定 canonical time quantum 为 1 秒，写入 engine/clock profile hash。理由：

- 人物行走、到达、工作、服务排队和 UI 倒计时可达到可感知实时；
- 能够稳定排序、重放、测试和跨节点协调；
- 不会把 60 FPS 或网络抖动变成数据库写放大；
- 未来如果需要 100 ms 或更高精度，必须发布新的 engine version，不允许改变已运行 world 的粒度。

1 秒是逻辑精度，不是画面帧率。前端可在相邻已知位置间以 requestAnimationFrame 插值，但任何碰撞、到达、扣费、法律、奖励和状态变化都只发生在服务端的有效时间量子边界。

### 5.2 Temporal Frame 的定义

一个 Temporal Frame 表示：

~~~text
[world_time_from, world_time_to] 内
在同一 engine/clock segment/版本向量下
所有已到期且有资格执行的事件、命令和确定性派生结果
的一次原子提交。
~~~

Frame 不是“每秒必须有一行数据库记录”。只有以下情况需要创建：

- 有一条 due event 到期；
- 有一条可执行命令、Agent-origin intent 或恢复任务；
- 有连续量必须结算到一个边界；
- 需要写 snapshot/checkpoint；
- 世界状态从 running/paused/recovering 等生命周期切换；
- 管理员显式请求可审计诊断/测试推进。

没有事件时，世界可以无写入地从 T 前进到 T+N；下一次 frame 以区间形式结算连续变化和到期事件。这样既是真实时间，也不会产生一天 86,400 次空事务。

### 5.3 事件类别和节奏

| 类别 | 例子 | 触发方式 | 是否需要每秒写入 |
| --- | --- | --- | --- |
| 即时行动 | 角色开始移动、开门、提交申请、取消导航 | 命令入队，在下个可执行量子 | 否 |
| 持续行动边界 | 到达、工作班次开始/结束、门禁占用过期 | 明确 due_at | 否 |
| 连续结算 | 能源、耐久、饥饿、工资累积、库存腐败 | 从上次结算到当前边界的固定公式 | 否 |
| 离散日历 | 日出、营业、税期、周任务、租约到期 | 本地 calendar 转 UTC 后入队 | 否 |
| 宏观城市周期 | 人口、市场指标、交通统计 | 内容 policy 定义 cadence | 否 |
| Agent 决策 | observation 到期、行动结束、重大事件 | 决策 scheduler；模型异步 | 否 |
| 渲染 | 镜头、脚步摆动、光照渐变、粒子 | 浏览器本地帧 | 是，但不得写后端 |

### 5.4 同一时间量子的稳定排序

同一个 world_time_to 内，事件必须按以下稳定比较器排序：

1. due_at；
2. temporal phase；
3. event priority；
4. source sequence 或提交序列；
5. stable entity/event ID。

不能使用 map 遍历、SQL 未排序结果、网络完成先后、模型响应先后或 Go goroutine 调度顺序。

建议的 phase：

| phase | 处理内容 | 禁止形成的因果 |
| --- | --- | --- |
| PRE_CLOCK | clock segment、resume、恢复门禁、过期租约 | 同 frame 内跳过恢复验证 |
| PRE_COMMAND | 到期命令、预条件、权限、幂等判定 | 新命令直接看见本 frame 后续效果 |
| PRE_LIFECYCLE | Actor/设施/订单/预约/Portal 到期 | 同 frame 新创建对象自动完成 |
| MOVEMENT | 移动步进、路径 reservation、到达、占用 | 客户端指定落点结果 |
| ACTIVITY | 工作、休息、训练、服务、交互状态边界 | 行动绕过空间/资源/Rule |
| CITY_SETTLEMENT | 资源、生产、市场、财政、连续量积分 | 读未封存的未来事件 |
| RULE_EFFECT | Rule/Case、事实、跨域 effect、可见事件 | 规则结果反向修改原始命令 |
| POST_SCHEDULE | 安排下一 due event、失效、outbox、snapshot | 同 frame 再消费自身新 effect |

任何未来新增域必须在其设计中说明属于哪个 phase、能消费哪些前序事实、只能产生哪些后续事实。

### 5.5 命令语义升级

现有 expected_world_tick 不能直接用于实时 world。新的命令协议应使用：

| 字段 | 目的 |
| --- | --- |
| client_request_id / Idempotency-Key | 同一 issuer 的幂等请求 |
| issuer | user、agent、internal_system；遵循 Agent 文档的来源模型 |
| submitted_effective_utc | 服务端接收时间，仅审计 |
| requested_world_time | 服务端依据当前 segment 计算，不信任 body |
| expected_timeline_cursor | 客户端所见的世界 frame/cursor；用于过期提示 |
| expected_entity_versions | 只覆盖该动作真正依赖的 Actor、目标、portal、reservation、余额等 |
| action schema version | 严格参数验证 |
| precondition hash | 服务端生成，Agent/客户端只回显 |

处理规则：

- 命令在当前时间量子内收到，默认排到下一个尚未封存的逻辑边界；
- 若该 world 正在追赶/unsafe/paused，返回明确的状态错误，不把命令偷偷放到过去；
- 仅 world cursor 变化、但动作依赖实体未变时，可按定义继续；相关实体变化则拒绝为 stale；
- 同一 user/Agent 同一 idempotency key 但 payload 不同，返回冲突；
- 旧 V24 API 保持 expected_world_tick；新 realtime API 不复用这个字段以免客户端误解。

### 5.6 追赶与长时间离线

恢复 running world 时，scheduler 计算：

~~~text
target_world_time = 当前有效 UTC 映射得到的 world elapsed time
pending range = (last_committed_world_time, target_world_time]
~~~

处理步骤：

1. 加载最后稳定 snapshot、clock segment、due event cursor 和版本向量；
2. 先收集区间内离散到期事件，按稳定排序执行；
3. 对连续域按定义的积分边界分段结算；边界至少包括 due event、日历变更、规则阈值、资源归零/满值、版本/暂停段；
4. 每个恢复批次受 CPU、数据库事务时间、事件数量和 wall timeout 上限约束；
5. 批次之间提交稳定 snapshot/cursor；用户读 API 可显示“恢复至 world time X/Y”；
6. 如果有无法确定的版本、时钟段、坏数据或不变量失败，世界停在 recovery_failed，禁止写入，不能跳过事件继续；
7. 只有追赶完成并校验 state hash、due queue 和关键不变量后才恢复用户命令。

不得为了“追赶快”直接把所有一个小时/一天的效果乘一个倍率后写入最终余额。连续公式可合并区间，但离散阈值、资源耗尽、规则触发、预约和事件顺序必须保留。

### 5.7 日历、时区与 DST

每个 world 固定 IANA timezone。日历调度必须使用以下规则：

- 所有持久化到期时间用 UTC；
- “每天 09:00 开门”等规则由 world timezone 计算下一次有效 UTC；
- DST 跳过的本地时刻必须有内容 policy：默认选择该日下一个存在的本地时刻；
- DST 重复时刻必须明确只触发一次或触发两次；默认按 local date + schedule key 去重，只触发一次；
- timezone 不允许玩家随意修改。管理员迁移 timezone 必须暂停、预演、创建新 clock/calendar segment 并审计；
- 周/月/季等统计必须标记其 timezone 和 calendar profile，不能以浏览器 locale 推断。

### 5.8 speed multiplier 的处置

| world 类型 | speed_multiplier |
| --- | --- |
| V24 和历史 world | 保持现有语义 |
| realtime production world | 固定 1.0，不提供 user/admin 加速按钮 |
| realtime test/sandbox world | 仅显式 test clock profile 可设置；不能连接奖励、真实 Agent 配额或共享经济 |
| replay | 使用历史 frame，不调用 live clock |

world.set_speed 对 realtime production engine 必须返回版本不支持错误；不得把它显示为“可点但无效”的 UI。

### 5.9 移动、导航和持续活动

实时不意味着把 Actor 坐标以浏览器帧率连续写入。一个移动/活动生命周期必须有明确的服务端边界：

| 阶段 | 服务端事实 | 前端可以做的事情 | 前端不得做的事情 |
| --- | --- | --- | --- |
| route accepted | 已验证的起点、目的、路径、reservation、速度定义与版本 | 绘制路线和开始位置 | 重算更短路径或更改目的 |
| movement started | start world time、首个区间、预计到达边界 | 用已确认起终点插值行走 | 把插值位置当成权威坐标 |
| waypoint due | 到期 waypoint、占用/道路/Portal 再验证 | 等待 patch 或局部平滑纠正 | 穿过新障碍、门禁或其他 Actor |
| arrival/blocked | 到达、失败、等待或 reroute 事实 | 更新角色状态、提示 Agent/用户 | 伪造抵达或无代价重试 |
| activity started | 资源/位置/资格/持续时间已锁定 | 显示工作、休息、训练、服务动画 | 直接完成收益、经验或任务 |
| activity finished | 结算、Rule/Case、产出、下一 observation trigger | 显示结果 | 重新解释时长或跳过处罚 |

行动 duration 使用整数微秒或冻结的时间量子数量，不使用浮点秒。移动速度、疲劳、道路/室内成本、交通容量、天气、装备和角色属性只能由服务端的版本化公式计算；前端只消费结果。

如果地图、Portal、道路容量、Rule、控制权或目标实体在运动中变化，后续 waypoint 必须重新验证。必要时写 blocked/cancelled/rerouted 事实并通知角色 Agent；不能继续沿用旧路线把 Actor 送到已不可达位置。

### 5.10 连续量、离散阈值与区间积分

实时世界中的连续量必须在公式与阈值之间明确分层：

| 类型 | 例子 | 处理规则 |
| --- | --- | --- |
| 单调消耗/恢复 | 饥饿、体力、耐久、能源、库存腐败 | 以固定点/整数率在时间区间上积分，遇到阈值切分区间 |
| 周期性累计 | 工资、租金、产出、维护费 | 只在已定义结算边界写账本，期间保留可重建 rate/version |
| 状态阈值 | 资源归零、设施损坏、许可到期、角色昏厥 | 在首次跨越阈值的逻辑时间创建离散 due event |
| 日历触发 | 营业、税期、睡眠窗口、季节变化 | 由 timezone/calendar profile 计算下一 UTC 到期点 |
| 随机事件 | 天气、事故、市场冲击 | 只在已封存 frame 中以版本化 PRNG/种子产生 |

任何积分函数都必须标明：输入单位、取整方向、最大/最小钳制、阈值分割、版本 hash、暂停语义、离线追赶语义和回放测试。不得把浮点累计误差、当前 wall clock 或渲染帧数量混入公式。

---

## 6. 时间数据模型、事件队列与不变量

### 6.1 新表与受控扩展

以下名称为设计级建议，实际 migration 必须沿用项目命名规则；无论名称如何，职责不能合并为一个 metadata JSON。

| 表/投影 | 最小职责 | 关键约束 |
| --- | --- | --- |
| city_clock_profiles | 生产/测试时钟阈值、粒度、恢复策略、日历策略 | immutable publish；hash；生产/测试隔离 |
| city_clock_nodes | 节点健康、偏移、uncertainty、source 状态 | 短保留；无秘密；不作为 world state |
| city_world_time_states | world 当前时间、状态、最后 frame、当前 segment、追赶状态 | 一 world 一行；写入者租约 |
| city_world_clock_segments | append-only 时间映射与暂停/恢复证据 | 不可覆盖历史段 |
| city_temporal_frames | realtime canonical frame、区间、state hash、phase 摘要 | world + frame_sequence 唯一；时间单调 |
| city_due_events | 到期事件、payload hash、phase、priority、lease、状态 | dedup key 唯一；不允许匿名无 owner 的事件 |
| city_temporal_continuations | 长区间积分/追赶的 checkpoint | 能从 snapshot 重建；不得替代 fact |
| city_world_visual_bindings | world 绑定的 visual pack、manifest、palette、style profile hash | 与版本向量同步冻结 |
| city_visual_packs | 已发布视觉包及兼容矩阵 | pack id + version 唯一；immutable hash |
| city_visual_assets | 资源 ID、用途、像素尺寸、atlas、anchor、provenance、hash | 内容 hash；禁止可执行格式 |
| city_visual_asset_variants | semantic rule 到具体 visual variant 的映射 | 语义/pack/variant 唯一 |
| city_actor_appearances | Actor 外观槽位、色板、appearance version | 不保存原始用户图片；slot 校验 |
| city_visual_generation_jobs | 管理端生成/审核/切片/发布工作流记录 | 与 runtime world 状态隔离 |

### 6.2 city_world_time_states

建议字段：

~~~json
{
  "world_id": 0,
  "temporal_engine_version": "city-openworld-realtime-v1",
  "clock_profile_id": "realtime-production-v1",
  "status": "running",
  "current_world_time_us": 0,
  "last_committed_effective_utc": "2026-07-21T00:00:00.000000Z",
  "current_clock_segment_id": 0,
  "timeline_cursor": "twf_...",
  "next_due_at_world_time_us": 0,
  "last_snapshot_id": 0,
  "catchup_target_world_time_us": 0,
  "recovery_state": "idle",
  "version": 0
}
~~~

禁止字段：

- 用浏览器时间覆盖 current_world_time_us；
- 用 float 保存权威时间/持续量；
- 把 entire due queue、视觉包、Agent memory、错误原文塞进 metadata；
- 因改视觉图集而递增逻辑时间版本。

### 6.3 city_due_events

每个 due event 至少包含：

| 字段 | 说明 |
| --- | --- |
| event_id / world_id | 稳定身份 |
| event_type / schema_version | 内容化事件类型与严格 payload schema |
| due_world_time_us | 世界时间的逻辑到期点 |
| phase / priority | 稳定排序 |
| aggregate_type / aggregate_id | 归属实体 |
| dedup_key | 同源事件防重复 |
| source_fact_id / source_command_id | 因果来源 |
| expected_version/hash | 执行前必须验证的最小前置 |
| payload_hash | payload 完整性 |
| status | pending、leased、applied、cancelled、rejected、dead_letter |
| lease_token / lease_expires_at | 仅调度协调 |
| created_frame / resolved_frame | 回放关联 |

due_world_time_us 相同的两个事件不能依赖数据库插入顺序。priority 与 stable ID 是契约的一部分。

### 6.4 Temporal Frame 记录

Temporal Frame 至少记录：

- frame_sequence；
- world_time_from_us、world_time_to_us；
- effective UTC 提交范围与 clock_segment_id；
- 前/后 state hash；
- engine、clock、spatial、visual、content、rule、agent bundle hash；
- 接受/拒绝命令序列范围；
- 执行 due event IDs 的有序 hash；
- 连续结算区间摘要和公式版本；
- snapshot/checkpoint 引用；
- duration、worker/node 诊断摘要；
- 可安全对用户显示的 cursor。

其中 node_id、内部失败细节和原始模型文本只能进入受限审计，不进入玩家 SSE。

### 6.5 必须保持的不变量

1. 同一 world 的 committed frame_sequence 严格递增；
2. 同一 world 的 world_time_to_us 不得小于上一个 committed world_time_to_us；
3. 同一 due event 只能有一个终态；重复领取/重试不得产生重复事实；
4. 每个 applied event 的 source 版本、命令、事实或 schedule key 必须可追溯；
5. 任何经济、库存、位置、Rule/Case、奖励资格变化都必须落在某个 Temporal Frame 内；
6. visual asset/pack 的改变不得改变 canonical physical state hash；
7. world 在 paused、archived、unsafe、recovery_failed 时不得产生普通行为 frame；
8. 同一 Actor 同一时刻的互斥控制、移动 reservation 和活动状态必须遵守已有/新增的唯一约束；
9. 指向已禁用/不存在 visual asset 的 appearance slot 不能发布；
10. 任一 snapshot + 之前的 Frame/Fact 序列必须能恢复同一 canonical state，不需要网络时间、图像生成或模型响应。

### 6.6 迁移与回滚

迁移顺序：

1. 先创建新时间/视觉表、索引、约束、只读健康 API 和 feature flags；
2. 发布 realtime engine、clock profile、空视觉包 schema，但不允许创建生产 world；
3. 用 fixture 创建测试 world，验证时间段、frame、暂停、故障恢复和空像素地图；
4. 发布第一套审核通过的 visual pack，完成 manifest/atlas loading；
5. 仅管理员创建 shadow/test world，压测和截图验收；
6. 开启白名单新 world；
7. 任何回退只暂停 realtime feature、冻结新 frame 和资产发布；绝不删除 frame、事实、素材审计或对 V24 表做 reset。

---

## 7. 空间语义到像素世界的投影

### 7.1 语义优先

像素渲染必须读取现有/后续空间语义：

~~~text
Worldgen / Chunk / Cell / Building / Interior / Portal / Actor / Item / Field
        ↓
Semantic Projection DTO
        ↓
Visual Pack Resolver
        ↓
Tile, sprite, animation, z-layer, tint, variant
        ↓
PixelSceneRenderer
~~~

任何以下逻辑都不得从图像像素判断：

- 是否可通行；
- 门是否可使用；
- 建筑有几层；
- 物品数量；
- 地块所有权；
- Actor 位置和相遇；
- 交通容量；
- 墙体是否阻挡视线；
- 法律、职业、库存、金钱或奖励状态。

### 7.2 坐标、格子与缩放

| 层级 | 逻辑单位 | 视觉表达 |
| --- | --- | --- |
| World / Region / Sector | 现有空间语义 | 地图缩略、地形色块、城市/道路标识，不加载细节图块 |
| Chunk | 现有 chunk size | viewport 所见 tilemap 页、LOD 切换和缓存边界 |
| Cell | 现有 world x/y/z | 一个逻辑 tile position，可绘制多个视觉图层 |
| Floor / z | 现有 z 与 building floor | floor selector、切面、屋顶隐藏、室内层 |
| Logical tile | 固定 16×16 像素基准 | CSS 以整数倍放大，禁止非整数平滑缩放 |
| Actor | 一个 surface anchor | 推荐 16×24 或 16×32 frame，脚底对齐 cell bottom-center |

默认镜头只使用正交 2D top-down。等距或 3D 必须另开 renderer，不能复用 top-down 的视觉坐标假装兼容。

### 7.3 Pixel scene 图层顺序

每个 cell 可以有多个层。默认从后到前：

1. base ground：土、草、砂、岩、水、雪、地砖、柏油；
2. terrain variation：边缘、坡、积水、裂纹、季节/天气覆盖；
3. infrastructure：道路、标线、轨道、桥、围栏、电线等；
4. building exterior base：墙、屋顶、招牌、外部阴影；
5. interior floor/wall：在室内/切面模式显示；
6. furniture、设施、固定物；
7. items、field、作物、临时构造；
8. actor shadow；
9. actor body 与衣物层；
10. 载具、可移动物、前景遮挡；
11. weather/effect；
12. selection、path、hover、debug overlay 和无障碍 focus ring。

z-order 由统一的 render sort key 计算，例如 floor、worldY、layer、subLayer、stableEntityID。不得依赖网络 patch 先后造成角色忽前忽后。

### 7.4 室内、建筑与多楼层

城市建筑不是一个“房屋 PNG”。逻辑建筑继续由 footprint、floor_count、layout、portal、unit pool、设施和状态定义；视觉上使用以下模式：

| 模式 | 显示 | 交互规则 |
| --- | --- | --- |
| 室外 | 屋顶/外墙、街道、外部标识 | 只能操作外部可见对象和允许的 Portal |
| 自动切面 | 选中/进入建筑时隐藏阻挡屋顶，显示当前 floor | 不暴露用户无权限的室内实体 |
| 室内 | 当前 z/floor 的墙、地板、家具、角色、物品 | path、occupancy、portal 仍由服务端判定 |
| 建筑蓝图 | 建设/规划时显示半透明占位 tile 和阶段纹理 | 不能让客户端据此直接提交施工结果 |
| 诊断 | 管理员可查看语义 grid、collision、portal、render IDs | 仅管理权限，不作为普通玩家地图 |

不同楼层不会同时把全部家具画在同一画布。镜头有明确 floor selection；楼层之间的 portal 通过服务端事实同步。

### 7.5 变体与确定性

草地花纹、树木朝向、砖墙颜色、屋顶细节和路边物件可以有视觉随机性，但必须确定：

~~~text
variant = Hash(
  world_seed,
  spatial_profile_hash,
  visual_pack_hash,
  cell_or_entity_stable_id,
  visual_layer_code
)
~~~

规则：

- 变体只改变从同一语义映射出的已批准素材，不改变容量、阻挡、物品或规则；
- 同一 world、同一 visual binding、同一 cell/entity 必须重载后保持相同外观；
- 视觉包升级是显式绑定变化；升级前后的截图差异可解释；
- 不在客户端用 Math.random 选地图变体；
- 用户可选角色外观通过 appearance version 固定，不能因刷新随机换脸。

### 7.6 可见性、隐私与多人

前端只能获取当前用户有权看到的 semantic chunk projection：

- 未探索区、私有室内、其他用户私有物品、法律证据和管理员标记不得通过“图片资源已经下载”泄露；
- 可视范围可以由世界内容定义，但不等于安全边界；真正的字段脱敏在后端 DTO；
- 同一 chunk 的静态地形可以缓存，动态 Actor/物品/门状态必须按访问权限和 cursor 返回；
- 截图/导出只导出当前授权范围；管理员诊断图单独加水印和审计；
- 不允许用户传入任意 asset ID、atlas path 或 hidden entity ID 试探资源。

可见性过滤不是世界隔离。服务器先在同一 canonical world/revision 上计算事实，再按 member、Actor、位置、发现状态和访问 policy 生成 projection；不得按用户执行不同 reducer、不同地图生成、不同市场结算或不同 cursor。响应中可包含 `view_scope_hash`/`redaction_policy_version` 以解释字段差异，但 canonical `world_id`、timeline cursor、semantic revision 与版本向量必须可以追溯到同一共享世界。

### 7.7 地图层级与数据密度

地图层级必须服务于现有开放世界的 Macro geography、Settlement layout、Sector、Chunk、Parcel、Building、Floor 与 Cell，而不是把大范围 Cell 全加载：

| 缩放层级 | 默认信息 | 不显示的信息 | 点击后的行为 |
| --- | --- | --- | --- |
| 世界/区域 | 地形、城市群、主要道路/水系、行政标识 | 家具、物品、私有 Actor | 进入 Sector 或定位城市 |
| Sector/城区 | 街区、道路网络、地标、建筑密度、公共设施 | 室内家具、未授权角色 | 加载相邻 Chunk |
| Chunk/街区 | 地面、道路、建筑外观、公开 Actor、施工 | 未进入室内细节 | 选择建筑/角色/地块 |
| 建筑室外 | Portal、门口、外墙、附近角色 | 私有房间/物品 | 请求进入/查看楼层 |
| 室内 floor | 墙、地板、家具、可见物品、授权 Actor | 其他楼层和无权限房间 | 合法移动/活动操作 |

切换 LOD 只改变数据密度和渲染策略，不改变 world 坐标、不重新随机生成实体、不绕过可见性。

---

## 8. 原创日式像素美术体系

### 8.1 风格目标

目标是高可读、紧凑、温暖而有经营模拟层次的原创像素城市：

- 顶视/轻微俯视的正交布局；
- 清晰的道路、建筑轮廓、室内家具和角色脚点；
- 小尺寸下优先识别功能，放大后显示装饰和生活感；
- 颜色、线条、阴影和动画统一，不使用玻璃拟态、模糊、照片贴图或随机写实素材；
- 国家/城市原型改变建筑、道路、植物、公共设施和生活物件，而不是只改一张滤镜；
- UI 与场景分层。UI 可采用项目统一的组件体系，地图画面本身保持像素风，不把网站背景效果压在 tile 上。

### 8.2 原创与参考边界

允许借鉴的抽象特征：

- 像素尺寸、经营模拟的信息密度；
- 城市街区与室内的可读层级；
- 角色移动、营业、施工、天气等小循环表现；
- 清晰的色彩语义和微小但连续的生活动画。

不得复制：

- 任何特定游戏的 sprite、tile、角色脸、服装、家具、建筑平面、配色表、UI 排版、标题、logo、音效或命名；
- 通过 prompt 要求模型“照着某游戏/某作品画一模一样”；
- 从截图、资源包、ROM、wiki 图集或受版权保护素材中提取、描摹、训练或切片；
- 把“生成模型输出”当作自动获得商业使用权的保证。

每一个 visual pack 发布前都需要 provenance、人工审查和相似性风险检查。

### 8.3 调色板与像素规范

| 项目 | 规范 |
| --- | --- |
| 基准 tile | 16×16 logical pixel |
| 人物帧 | 16×24 或 16×32，统一脚底 anchor |
| 放大 | 2x、3x、4x 等整数倍；CSS 使用 image-rendering: pixelated |
| 抗锯齿 | 禁止半像素、模糊、线性缩放、照片噪点、未审核半透明边缘 |
| 调色板 | 每个 visual pack 有冻结 palette；同一大类资产共享命名色板 |
| 明暗 | 使用离散 shade band；夜晚/天气使用预定义 tint/overlay，不运行时随意滤镜 |
| 透明度 | 只允许预定义 alpha 级别；阴影、玻璃、水面等独立素材/层 |
| 文本 | 不把用户可变文本烘焙到 tile；招牌使用本地化 UI overlay 或内容目录中的安全短标签 |
| 色盲/无障碍 | 不能只用颜色表达道路、警报、可通行或角色选中；必须有纹理/图标/outline 辅助 |

### 8.4 资产分类

| 分类 | 示例 | 必需 metadata |
| --- | --- | --- |
| terrain | 草、泥、沙、岩、水、雪、地砖 | autotile mask、walkability display class、season tags |
| road/infrastructure | 人行道、车道、轨道、桥、路灯、护栏 | direction、连接 mask、draw layer、style tags |
| exterior building | 住宅、商店、工厂、学校、医院 | footprint module、roof/wall/entrance、floor compatibility |
| interior | 地板、墙、门、桌椅、床、工作台 | room tags、wall edge、anchor、occlusion |
| vegetation/decoration | 树、花、长椅、邮箱、广告牌 | density group、season/weather variant |
| item | 食物、工具、包、箱、材料 | item category、stack display rule、anchor |
| vehicle | 自行车、轿车、公交、货车 | direction frames、occupancy visual |
| character base | 体型、肤色、脸/眼睛 | body archetype、frame rig、gender-neutral slot support |
| character wear | 头发、帽子、上衣、下装、鞋、饰品 | slot、遮挡 mask、palette/recolor compatibility |
| effect | 脚步、施工、雨、烟、状态 icon | duration、blend class、reduced-motion fallback |
| UI/world marker | selection、路线、portal、任务、风险 | semantic code、accessibility label |

### 8.5 模块化角色外观

角色外观必须是可组合但受控的 appearance profile：

~~~text
body archetype
  + skin palette
  + face / eye style
  + hair back
  + top / outerwear
  + lower garment
  + shoes
  + accessory back/front
  + held item / work tool
  + animation state / direction
~~~

最小槽位：

| slot | 必填 | 说明 |
| --- | --- | --- |
| body | 是 | 体型/基础姿势，不改变 Actor 属性 |
| skin_palette | 是 | 只引用批准色板 |
| face_eyes | 是 | 脸与眼睛，受 body rig 兼容性约束 |
| hair_back / hair_front | 否 | 分层以避免衣物和头饰遮挡错误 |
| top | 是 | 上衣/外套；可受职业制服 rule 替换或覆盖 |
| lower | 是 | 裤子/裙装等，不表示游戏性别限制 |
| shoes | 是 | 行走帧对齐 |
| accessory | 否 | 眼镜、包、帽子、首饰等，可多槽但有限制 |
| held_item | 否 | 来自实际 inventory/活动视觉投影，不能凭外观凭空生成物品 |

每个可移动角色至少需要：

- idle、walk、run/fast、work、sit/bed 等允许状态的帧定义；
- 四方向或更多方向的统一索引；
- 服装遮挡顺序、手臂/工具覆盖规则；
- missing asset fallback；
- 角色缩略图渲染和场景帧共享相同 appearance resolver；
- version 与 hash，用于重放截图和用户审计。

首版禁止：

- 自由上传头像、真人照片、名人脸、裸露/成人素材；
- 用户直接写 prompt 生成自己角色并立即发布；
- 外观给予速度、容量、职业资格、法律豁免或奖励倍率。

### 8.6 国家/城市原型与视觉包

国家风格不能只影响装饰。它由两个可独立版本化但兼容绑定的包组成：

| 包 | 负责内容 | 不能负责内容 |
| --- | --- | --- |
| spatial/city profile | 路网、地块、建筑拓扑、交通约束、密度、公共空间、地形规则 | 固定 PNG、用户 UI、账本规则 |
| visual pack | 瓦片、建筑外观、植物、家具、角色衣橱、色板、动画 | 碰撞、道路容量、Portal 权限、法律效果 |

示例：

- 日式原型可以采用窄街、站前商业带、独栋住宅、便利店、路侧设施和特定植物视觉语言；
- 中国城市原型可以采用更宽道路、街区商业、园区、公共设施和不同建筑层级；
- 两者都必须通过同一 Chunk/Cell/Building/Portal 语义和交通接口工作；
- 所有差异以内容包和 profile 表表达，不在 reducer 中写 country 特判。

### 8.7 动画与天气

动画必须从语义状态派生，并且具备降级：

| 语义 | 可用动画 | 不能表达为 |
| --- | --- | --- |
| Actor walking | 步行帧、朝向、短时阴影 | 真实位置或碰撞事实 |
| 建设中 | 脚手架、轻微施工粒子、进度纹理 | 未结算的建设完成 |
| 开门/关门 | 门帧/高亮 | Portal access 绕过 |
| 天气 | 雨雪粒子、地面覆盖、光照 tint | 客户端修改温度/农业/交通 |
| 工作/营业 | 工具、灯光、招牌状态 | 收入/库存/角色职业资格 |
| 警报/处罚 | 非文字状态 icon 和颜色纹理 | 未公开 Case 证据 |

用户若选择 reduced motion，必须停止/减少视觉循环，但不改变世界真实时间或角色行为。

---

## 9. 图像生成、审核、切片与图集发布管线

### 9.1 总原则

图像生成服务只用于生产“候选原创素材”，不能直接产生线上运行时世界状态。发布链路必须是：

~~~text
Art Direction Spec
  → Generation Request
  → Candidate Raster
  → Safety/IP/quality review
  → Pixel normalization and grid slicing
  → Metadata/anchor/collision-semantic binding
  → Atlas pack + manifest hash
  → Staging visual pack
  → Screenshot/performance approval
  → Immutable production publish
~~~

首发视觉包中的地面、道路、建筑外观、室内、家具、物品、车辆、特效、人物 body、头发、眼睛、饰品、衣物、下装和鞋类，统一通过受控 ImageGen/image2 资产任务产生候选，再经过本节规定的规范化与审核；不得混入未登记的第三方游戏资源。人工像素修订仅用于网格、锚点、遮挡、调色板和无障碍质量控制，仍须保留候选来源、修改记录和最终 hash。

### 9.2 美术需求单

每个生成任务必须由管理员创建的 Asset Request 表达，不允许用户自由文本直通生成模型。字段至少包括：

| 字段 | 作用 |
| --- | --- |
| asset_request_id | 全流程审计 |
| pack target | 目标 visual pack 与版本草案 |
| asset class / semantic tags | 例如 terrain.grass、building.shop、character.top |
| required dimensions | 原始候选尺寸、最终 tile/frame 尺寸、帧数 |
| style brief | 原创日式紧凑像素语言、调色板、视角、禁止项 |
| composition constraints | 空白边距、脚底/门口 anchor、透明背景、方向、摆放规则 |
| palette reference | 仅引用本 pack 的批准 palette |
| generation model/version | 用于复现和质量追踪 |
| seed/parameters | 如提供，保存于受限审计 |
| reviewer policy | 内容、IP、像素技术 QA 的要求 |
| source rights/provenance | 生成来源、人工编辑来源、许可证说明 |

人格、用户名、邮箱、现实肖像、聊天、API Key、城市私有数据、第三方凭据均不得进入 Asset Request。

### 9.3 生成提示的结构

允许的提示词由模板拼装，而不是由用户自由输入。例如：

~~~text
原创像素游戏素材；16 像素网格；顶视/轻微俯视；
对象：小型街边书店外观模块；透明背景；无文字、无 logo、无人物；
使用批准调色板；清晰 1 像素轮廓；无抗锯齿；适合作为模块化城市 tile；
禁止参考或模仿任何现有游戏、品牌、角色或截图。
~~~

模板需要按 terrain、建筑、家具、服装、角色部件、特效分别定义。不得把“模仿开罗/模仿某游戏”的字样加入生产提示。

### 9.4 候选资源的技术 QA

候选 raster 通过人工与自动检查后，才能进入切片：

1. 格子对齐：边缘、门口、脚底、墙角、道路连接点必须落在规定像素网格；
2. 透明度：无无意的白底、半透明毛边、颜色溢出或黑边；
3. 调色板：颜色数、色板索引、对比度和夜晚 tint 兼容；
4. 尺寸：符合 tile/frame 标准；不允许运行时任意缩放裁切；
5. 方向：北/南/东/西帧、连接 mask、角色脚点一致；
6. 语义：资源的展示标签符合其绑定的 terrain/building/item，不暗示不真实的通行/门禁；
7. 相似性/IP：无现有作品 logo、角色、招牌、可识别资产或过度近似表达；
8. 内容安全：无成人、仇恨、隐私人物、政治/现实敏感标识、真实商标等未授权内容；
9. 性能：图块数量、atlas 页数、alpha 使用、文件体积在 pack budget 内；
10. 本地化：任何文字均拆为 UI overlay 或 content label，不烘焙到全球通用 tile。

失败资源应被标记 rejected 或 needs_revision，不得覆写已发布 asset。

### 9.5 切片、元数据与图集

像素资源发布前必须产出：

~~~json
{
  "asset_id": "visual.jp.v1.building.shop.corner_a",
  "content_hash": "sha256:...",
  "atlas_id": "visual.jp.v1.terrain-buildings.001",
  "frame": {"x": 0, "y": 0, "w": 16, "h": 16},
  "anchor": {"x": 8, "y": 16},
  "layer": "building_exterior",
  "semantic_tags": ["building.shop", "roof.corner"],
  "variant_group": "shop_corner",
  "palette_id": "jp-urban-v1",
  "provenance_id": "asset-job-...",
  "status": "published"
}
~~~

规则：

- 图集文件和 manifest 均为内容寻址；发布后不得原地替换；
- atlas 页尺寸根据目标设备 WebGL 限制和 pack budget 决定，构建时检测，超限自动拆分而不是让浏览器失败；
- 资源 URL 使用 hash，静态缓存可长期保存；权限仍由 manifest 和地图 API 控制；
- 角色层可以使用共享图集，也可以在客户端缓存合成纹理；合成结果不能回写为身份数据；
- 每个 asset 必须有 fallback，例如缺失商店屋顶时使用同语义的 neutral roof，不显示空白或错误的可通行格。

### 9.6 视觉包发布状态机

~~~text
安全目标状态机（完整资产管线完成后）：
draft → generating → review_required → staging → published → retired
                 ↘ rejected

当前已实现的控制面状态机：
staging → published → retired
staging ──→ generation job: queued → cancelled / rejected / failed
~~~

| 状态 | 可编辑 | 可被新 world 绑定 | 可被已运行 world 替换 |
| --- | --- | --- | --- |
| draft/generating/review_required | 目标资产管线状态，当前未在 pack 表中开放 | 否 | 否 |
| staging | 受限；仅程序化 manifest 可编辑 | 否 | 否 |
| published | 否，只能新版本 | 是 | 仅显式 visual upgrade |
| retired | 否 | 否 | 已绑定 world 继续读取；新 world 不可选择 |

当前实现补充规则：

1. `city_visual_pack_release_policies` 只能指向 `published` 且声明兼容目标 spatial profile 的程序化包；`*` 是默认策略，精确 profile 优先；
2. world genesis 把选中的 pack id、version、manifest hash、asset-set hash 和 profile 固化到 `city_world_visual_bindings`。之后改策略、发布新包或退役旧包均不改历史 binding；
3. 退役前必须先移走引用它的发布策略。这个限制保证下一次新 world 创建不会因控制台中“看似已退役”的默认包而失败；
4. 生成 job 目前只记录受控意图，尚未产生可发布资产。因而 `queued`、`generated`、`reviewing` 和 `approved` 都会阻止发布；只有 `rejected`、`cancelled`、`failed` 不阻止一个无资产程序化包发布；
5. 管理页面、API 与审计事件都不得携带原始 prompt、模型名、源 URL、存储 key、图片二进制或用户私有资料。后续 worker 若需要这些受限数据，必须在独立凭据边界内保存，并只向此控制面回传 hash、尺寸、审核结论和安全摘要。

### 9.7 运行时禁止路径

以下路径必须明确拒绝：

- 玩家 API 传 image URL、base64 图片、prompt 或 asset ID 直接作为角色/建筑；
- Agent action 直接创建或替换 PNG、atlas、appearance；
- 地图渲染器下载未经 manifest 签发的任意 URL；
- 视觉包编辑器对已经 published 的 hash 原地更新；
- 服务端根据图像内容推断真实世界人脸、身份、地理位置或用户画像；
- 为了“更真实”把用户人格或私密聊天送入图像生成模型。

---

## 10. 前端渲染架构

### 10.1 Renderer 抽象

前端在现有 Vue 3/Pinia 技术栈内新增明确的 PixelSceneRenderer 边界：

| 层 | 职责 | 不得承担 |
| --- | --- | --- |
| City map store | 请求 manifest、semantic chunks、dynamic patches、cursor、viewport cache | 推断物理、写世界状态 |
| Visual resolver | 根据 manifest 将语义 DTO 映射到 asset/frame/layer | 根据像素推断语义 |
| PixelSceneRenderer | 初始化 WebGL、批量绘制、相机、选中、视觉插值 | 业务 API、账本、权限 |
| Interaction adapter | screen 到 world 坐标换算、发出选择/命令意图 | 直接修改 Actor 位置 |
| UI panels | 详情、操作、错误、loading、无障碍文本 | 作为地图对象数千节点渲染 |

首选实现为 PixiJS 8 的 CSP-safe WebGL renderer。当前 CityClassicViewport 对 unsafe-eval 模块的导入不得进入新 renderer；构建和浏览器测试必须证明在禁止 unsafe-eval 的 CSP 下正常加载。

### 10.2 CSP、fallback 与浏览器能力

启动顺序：

1. 检测 WebGL/WebGL2、纹理尺寸、设备像素比、reduced motion 和内存预算；
2. 加载 manifest，并验证 pack/version/hash；
3. 初始化 CSP-safe Pixi application，不导入 unsafe-eval；
4. 预加载首屏必要 atlas，先绘制静态 ground，再逐层加入建筑/Actor；
5. 如果 WebGL 初始化失败，切换到 Canvas 2D reduced renderer；它仍支持查看、选择和命令，但降级装饰/动画；
6. 如果资源完整性或 manifest 不匹配，显示明确错误和重试，不用错误的旧 atlas 静默渲染；
7. 无论何种 fallback，都不能导致整页重挂载或触发全局 loading 闪烁。

Canvas fallback 不是重新启用 ASCII 主界面。它使用同一 tile manifest 的低性能、低装饰像素绘制。

### 10.3 Chunk 流式加载

地图不能拼成一张全世界图片。请求/缓存单位：

| 数据 | 单位 | 缓存策略 |
| --- | --- | --- |
| visual manifest/atlas | visual pack hash | 内容寻址、长期缓存、版本切换失效 |
| static semantic chunk | world + chunk + z + revision | ETag、局部 LRU、可预取相邻一圈 |
| dynamic patch | world + chunk + cursor | SSE cursor 或短轮询补拉 |
| actor snapshot | 可视范围 + visibility scope | 短时缓存、按 patch 替换 |
| building interior | building/floor + access scope | 进入或选择后加载，离开释放 |
| route/selection overlay | 当前交互 | 内存态，不进入持久资产缓存 |

相机移动不会清空整个 store。应保留仍可见 chunk，异步加载新边缘，使用局部 loading/低细节占位，避免用户此前报告的全页闪烁。

### 10.4 绘制与性能预算

| 项目 | 设计要求 |
| --- | --- |
| tile 绘制 | 以 texture atlas 批处理，避免每个 tile 一个 DOM 节点或一个文本对象 |
| 空间裁剪 | 仅渲染 viewport + 小 buffer 内的图层 |
| LOD | 远景使用聚合 tile/缩略层；近景才加载家具、物品和完整角色帧 |
| dirty update | dynamic patch 只重建受影响 chunk/layer，不重建整个 scene |
| 动画 | screen visible actor/effect 才更新；离屏冻结或低频 |
| 拖拽/缩放 | CSS/renderer 变换即时响应，网络加载节流，镜头状态不写后端 |
| 高 DPI | 内部 resolution 有上限；优先整像素清晰而非无限像素比 |
| 内存 | chunk、atlas、合成角色纹理均 LRU；清理时保留 manifest 元数据 |
| 诊断 | 暴露只读 FPS、draw calls、visible chunks、atlas bytes、patch lag 指标给管理员 |

性能目标必须在目标 VPS/用户浏览器矩阵中测量后冻结。不得只在开发机空世界中宣称“地图性能足够”。

### 10.5 角色渲染

角色渲染流程：

1. 后端返回 Actor 的脱敏 appearance projection：appearance version、允许的 slot asset IDs、direction、activity visual state、world position、插值所需的已验证移动段；
2. 前端 resolver 校验这些 asset 属于已绑定 visual pack；
3. renderer 按固定层顺序组合 body、衣物、饰品、held item；
4. 若出现移动，使用服务端已确认的 start/end world time 计算视觉插值；到达/碰撞仍以新 patch 为准；
5. 有新的 authoritative patch 时平滑校正，不能在视觉插值结束后擅自继续走；
6. Agent/NPC 的模型思考、私有 memory、完整 reason 不显示为头顶气泡。可显示内容目录允许的状态图标；
7. 用户能自定义的仅是已开放 appearance slot 和颜色选项；服务器验证所有组合。

### 10.6 交互和无障碍

地图必须有可键盘操作与非纯视觉路径：

- 方向键/快捷键控制镜头；焦点环与选中 cell 具有可见高对比标记；
- 选中对象的名称、类型、位置、状态、可执行操作和拒绝原因以可读面板呈现；
- 缩放、动画、天气特效可遵守 prefers-reduced-motion；
- 色盲模式替换关键状态色，并保留纹理/图标；
- 屏幕阅读器获取语义摘要，不读取数千 tile；
- 高对比模式不修改实际 world/资产，仅切换呈现层；
- 所有可执行命令仍由后端返回 action availability；地图点击不等于自动有权限。

---

## 11. API、SSE 与缓存契约

### 11.1 新 realtime world 读取模型

Realtime API 返回的顶层时间字段必须明确：

~~~json
{
  "world_id": 42,
  "temporal_engine_version": "city-openworld-realtime-v1",
  "timeline_cursor": "twf_000000000321",
  "world_scope": {
    "mode": "shared_online",
    "view_scope_hash": "sha256:...",
    "redaction_policy_version": "member-visibility-v1"
  },
  "world_time": {
    "elapsed_us": 123456789,
    "timezone": "Asia/Shanghai",
    "local_time": "2026-07-21T12:34:56+08:00",
    "source_effective_utc": "2026-07-21T04:34:56.000000Z",
    "clock_state": "healthy"
  },
  "visual_binding": {
    "pack_id": "jp-urban",
    "version": "1.0.0",
    "manifest_hash": "sha256:..."
  }
}
~~~

不得把 realtime 的 elapsed_us 塞回 current_tick 并让客户端猜单位。V24 响应维持 current_tick；新接口可以并行提供 legacy 字段，但字段名必须区分。

### 11.2 建议端点

| 方法 | 端点 | 权限 | 说明 |
| --- | --- | --- | --- |
| GET | /api/v1/city/worlds/{id}/clock | 成员 | 时间、cursor、时钟健康的安全摘要 |
| GET | /api/v1/city/worlds/{id}/membership | 成员 | 当前用户 membership、治理 role、可见范围与 control grant 摘要；不返回私人 world 或其他成员敏感资料 |
| POST | /api/v1/city/worlds/{id}/join-requests | 已认证用户/受策略限制 | 只申请加入既有共享 world；批准后只创建 membership，不创建世界副本 |
| GET | /api/v1/city/worlds/{id}/timeline | 成员/管理员范围不同 | frame/事件的分页摘要，不泄露私有实体 |
| GET | /api/v1/city/worlds/{id}/visual-manifest | 成员 | world 绑定的 pack manifest/ETag |
| GET | /api/v1/city/worlds/{id}/pixel-chunks | 成员 | viewport/static+dynamic semantic projection |
| GET | /api/v1/city/worlds/{id}/buildings/{code}/floors/{z}/pixel-projection | 有访问权限成员 | 室内层 |
| GET | /api/v1/city/worlds/{id}/actors/{code}/appearance | 有可见权限成员 | 脱敏外观投影 |
| POST | /api/v1/city/worlds/{id}/commands | 有 action grant | 新实时命令，支持 expected_timeline_cursor |
| POST | /api/v1/city/worlds/{id}/appearance | 角色 owner | 仅批准 slot 组合，幂等 |
| GET | /api/v1/admin/city/clock-health | 管理员 | 节点、偏移、恢复、告警 |
| GET/POST | /api/v1/admin/city/visual-packs/* | 管理员 | 资产草案、审核、staging、发布 |
| POST | /api/v1/admin/city/worlds/{id}/pause/resume | 管理员/受控 owner | 显式创建 time segment |

路由最终命名必须和现有 API 规范一致，但 API 面必须区分 user、member、owner、admin 和 internal worker。

### 11.3 Pixel chunk DTO

pixel-chunks 不返回“已经画好的截图”，而返回：

| 部分 | 内容 |
| --- | --- |
| chunk identity | x/y/z、revision、semantic hash、visual binding hash |
| terrain layers | terrain/road/water/structure 的 semantic definition 与 deterministic variant key |
| building projection | 可见 footprint、外观 module、construction/status visual tags |
| interior gate | 是否可加载、允许 floor、访问拒绝码 |
| actors | 只含可见/可公开 Actor 的位置、运动段、appearance ref、状态 icon |
| dynamic entities | 允许可见的 item/field/furniture/vehicle delta |
| overlays | 仅当前用户可见的选中/路径/任务/警告语义 |
| cursor | 该数据对应的 timeline cursor 和 next patch cursor |

client resolver 再用 manifest 把 semantic definition/variant key 变成 atlas frame。这样避免后端为每个用户返回重复的 asset URL，也避免客户端任意猜测语义。

### 11.4 SSE 事件

建议事件：

~~~text
city.timeline.frame_committed
city.map.chunk_patch
city.map.visibility_changed
city.actor.patch
city.actor.appearance_changed
city.world.clock_state_changed
city.world.recovery_progress
city.visual.binding_changed
city.command.resolved
~~~

规则：

- SSE payload 是 cursor + patch hint，不是全世界 JSON；
- 客户端 gap、重连、manifest hash 不匹配时按 cursor 补拉；
- 不能因收到新 frame 让 Vue 路由或整个世界工作区重挂载；
- 旧 cursor、跨 world cursor、未授权 cursor 必须拒绝；
- 一条 patch 可被重复投递，前端必须按 revision/cursor 幂等应用；
- asset binding 变化期间先加载新 manifest，再切换 scene；不可出现半新半旧 atlas。
- scheduler 对一个 world 只提交一次 frame；SSE fan-out 只按可见性过滤同一 frame/patch，不能为每位成员重新运行 reducer 或产生 per-user cursor；
- 成员 A 的可见命令结果必须在成员 B 的订阅中以同一 shared cursor 收敛；B 无权查看的私有字段可省略，但不得借此回退、替换或分叉公共世界状态；
- membership/control grant/presence 变化只影响后续 projection 和可操作性。它们不重置地图、NPC、市场、世界时间或其他成员的订阅 cursor。

### 11.5 错误码建议

| code | 含义 | 前端行为 |
| --- | --- | --- |
| CITY_REALTIME_CLOCK_UNSAFE | 时钟不可信 | 停止写操作，显示可恢复告警 |
| CITY_WORLD_RECOVERING | 世界正在追赶 | 保留现有场景，显示局部进度并重试 |
| CITY_TIMELINE_CURSOR_STALE | 依赖状态已变化 | 刷新目标实体/操作面板，不重载整页 |
| CITY_VISUAL_MANIFEST_MISMATCH | manifest 与 chunk 不兼容 | 重新获取 manifest，失败则显示资源错误 |
| CITY_VISUAL_ASSET_UNAVAILABLE | 静态资源不可用 | fallback/retry，不改世界状态 |
| CITY_APPEARANCE_SLOT_INVALID | 外观槽位不兼容/未授权 | 在外观编辑器定位错误项 |
| CITY_REALTIME_SPEED_UNSUPPORTED | realtime production 不可加速 | 隐藏/禁用加速 UI |
| CITY_WORLD_TIMEZONE_IMMUTABLE | 不允许直接改时区 | 引导管理员走迁移流程 |

### 11.6 缓存和版本失效

缓存键必须同时包含：

~~~text
world_id
engine/spatial/content/rule version hashes
visual_pack_manifest_hash
chunk coordinates and z
semantic revision
visibility scope
~~~

禁止只用 URL 路径或 chunk 坐标缓存不同世界/不同权限的数据。浏览器缓存的 atlas 可长寿命，但包含私有动态对象的 API 响应必须使用合适的 cache-control 和 authorization vary 策略。

---

## 12. Agent、NPC 与实时世界的衔接

### 12.1 观察时间

Agent 文档中的 observed_tick 在 realtime-v1 中必须等价升级为：

~~~text
observed_timeline_cursor
observed_world_time_us
clock_segment_id
relevant_entity_versions
precondition_hash
~~~

它们共同定义 Observation 的有效性。模型返回慢 2 秒、20 秒或 5 分钟不改变 world；结果只在仍满足前置条件且进入后续 frame 时转为候选命令。

### 12.2 Agent 调度

| 触发 | Agent 行为 | 世界引擎行为 |
| --- | --- | --- |
| 角色活动完成 | 创建 observation request | 活动状态先由 due event 确定结束 |
| 角色到达/受阻 | 允许下一次决策 | 位置、occupancy、Rule 已封存 |
| 工作/休息日程到期 | 生成有限 observation | 日历/活动由服务端确定 |
| 重大 Rule/Case | 按权限给摘要 | 不把私有证据或处罚结果交模型修改 |
| 模型超时/429 | request 退避/降级 | world 时间继续；角色可等待或走确定性日程 |
| NPC LOD 切换 | NPC Manager 分配/暂停子 Agent | LOD 守恒和位置仍由引擎事实决定 |

禁止“每秒让所有 Agent 决策一次”。Agent 调度 cadence 由 policy bundle、角色活动边界、预算与事件重要性共同控制。

### 12.3 视觉与 Agent 的严格隔离

- Agent Observation 使用地图语义、位置、可见实体摘要和行动候选，不发送 PNG、atlas、屏幕截图或 UI 文本；
- Agent 不可请求“把角色外观改成某图”“创建一个建筑贴图”“通过看像素识别门”；
- 人格种子影响行为权重和记忆，不影响角色基础身体部件以外的未授权资源；
- NPC appearance 是 NPC Manager/内容 policy 的受控分配，不由 NPC 的模型文本任意决定；
- user Character Agent 的奖励资格继续只读取封存游戏事实，不能读取“停留在线”“渲染了多少帧”或“模型做了多少次思考”。

### 12.4 模型延迟与实时体验

模型延迟必须对世界时间无权：

- 角色可处于 thinking/waiting，但其世界中的饥饿、租约、日程、法律期限和天气照常实时前进；
- 到达一个需要决策的节点后，可由 policy 使用有限等待窗；超时后执行确定性 fallback，例如休息、原地等待、回家或暂停任务；
- 任何 fallback 也是 normal command/due event，遵循 Rule/资源/路径验证；
- 玩家 UI 显示“等待下一决策/模型暂不可用”的状态摘要，不泄露 provider、密钥、原始 prompt 或其他用户信息；
- 不以模型更快/更慢改变角色移动速度、城市经济速度或奖励倍率。

### 12.5 实时 Agent 的负载阈值

任何 Agent policy 需要同时定义：

| 限制 | 说明 |
| --- | --- |
| 最小观察间隔 | 同一 Agent 连续无事件时的最低真实时间间隔 |
| 事件触发白名单 | 哪些 due event/Case/活动完成可唤醒 |
| 每真实小时调用/Token 上限 | 与城市内部货币、奖励分离 |
| 并发和队列上限 | 防止单城市耗尽模型与 worker |
| 最大等待窗 | 模型未响应后进入 fallback 的界限 |
| 观测有效期 | cursor/实体版本变化后 response 自动 stale |
| NPC LOD 策略 | 远景 NPC 默认聚合，不调用模型 |
| 失败熔断 | 429/5xx/无效 JSON/安全拒绝的不同处理 |

这些阈值是 agent policy bundle 的一部分，不能放在前端常量或用户 prompt 里。

---

## 13. 安全、隐私、反作弊与内容治理

### 13.1 时间安全

- 客户端所有时间字段视为显示信息，不能决定 due_at、收益、到达、处罚、冷却或奖励；
- 服务器校验 Clock Authority 健康与数据库时钟偏差；异常时 fail closed；
- world pause/resume、clock profile 切换、测试时钟推进、timezone migration 都是管理员受审计命令；
- 不允许用户通过修改系统时间、重放请求、快速切换时区或延迟网络包让世界回退；
- 所有时间相关奖励判定绑定已封存 frame/fact/time segment，而非请求到达时间。

### 13.2 资产安全

- 只接受 PNG/WebP 等白名单 raster 格式；不接受 SVG、HTML、JS、可执行图像容器或远程 URL 直链；
- 解码后验证像素尺寸、透明度、文件大小、色彩空间和帧数；
- 静态资源使用 content hash、受控 object storage/CDN 路径、CSP img-src 白名单和完整性检查；
- 管理端资产上传/生成结果隔离在 staging，发布操作需要权限和审计；
- 资产 metadata 不包含第三方 API Key、生成服务 token、用户 prompt、内部主机名或隐私数据；
- 不允许 asset ID 越权枚举未发布资源。

### 13.3 用户外观和身份保护

- 用户只选择平台提供的抽象像素部件，不能上传真人照片或要求生成某现实人物；
- 外观编辑不收集生物特征、性别证件、种族推断、真实住址或现实职业；
- 昵称/文本标签走既有文本安全与长度/本地化处理，不能烘焙进可共享 atlas；
- 删除账号/角色时遵循世界审计保留策略：保留必要 hash/事实，删除或匿名化私有 appearance 选择的直接关联；
- NPC 不根据真实国家人群、用户画像、现实人物或模型臆测生成敏感身份。

### 13.4 客户端与 CSP

- 新 pixel renderer 不能依赖 unsafe-eval、动态函数编译、未审核 worker 脚本或远程代码；
- CSP 中 script-src、worker-src、connect-src、img-src 均只允许项目需要的受控源；
- WebGL shader/asset 构建以项目依赖的 CSP-safe 方式打包；若某依赖要求 unsafe-eval，必须替换/配置为安全入口，不可降低全站 CSP；
- canvas 不可成为跨用户读取像素/截图的侧信道；跨域资源使用正确 CORS/COEP 策略；
- 用户不可通过地图 query、atlas 坐标、调试 overlay 访问未授权实体。

### 13.5 反作弊与奖励

- 虚拟货币资格仅由服务端封存的、内容定义的完成事实触发；
- 角色自动运行、真实时间、模型输出、地图停留、动画播放、切换外观、重复创建 world 都不直接产生收益；
- 奖励策略绑定 world engine、content/rule/agent policy、用户/角色/周期限额和风险状态；
- test clock、sandbox、管理员角色、回放、NPC、系统 Agent 默认 reward_ineligible；
- 时钟异常、世界恢复、重复命令、可疑加速、异常多角色和异常模型调用生成风险事件，暂停资格或进入人工审核；
- 不能通过清缓存、伪造 SSE、修改浏览器变量或下载资源来影响服务端奖励事实。

### 13.6 内容和版权审核

审核至少分为：

| 审核层 | 目标 | 拒绝示例 |
| --- | --- | --- |
| 安全审核 | 避免不当/成人/仇恨/隐私内容 | 现实人物像素肖像、露骨素材 |
| IP 审核 | 避免复制受保护作品 | 特定游戏角色、logo、建筑图样或界面复刻 |
| 技术审核 | 确保像素/图集/性能可用 | 非网格资源、模糊边缘、巨大 PNG |
| 语义审核 | 视觉不误导系统事实 | 关闭的门画成开放、不可走地面画成道路 |
| 本地化审核 | 文本/符号不导致误解或泄露 | 将用户昵称写入共享图集 |

审核结论、审核人、理由、revision 和资源 hash 都必须可追溯。

---

## 14. 回放、恢复、升级与可观察性

### 14.1 回放

给定：

- genesis/upgrade snapshot；
- 版本向量与 visual binding；
- clock segments；
- Temporal Frames、accepted/rejected commands、due event terminal records、Fact/Effect；
- deterministic content/rule/agent policy；

必须恢复同一 canonical state hash。回放不得：

- 读取当前系统时间；
- 查询 NTP；
- 调用第三方模型；
- 再生成图片；
- 根据当前默认 visual pack 修改历史 semantic state；
- 依赖某个已过期前端版本。

视觉回放的目标是可在同一 pack 下重现；若历史 visual asset 被 retired，归档存储必须保留对应 manifest/atlas 或提供明确“历史素材不可用”的只读降级，不可替换为看似相同但 hash 不同的图片。

### 14.2 Snapshot

snapshot 需要增加：

- temporal engine/version；
- current_world_time_us、clock segment、timeline cursor；
- pending due event cursor 和必要 scheduler state；
- visual binding hash；
- appearance projection version 摘要；
- Agent lifecycle/decision cursor 摘要（如已启用）；
- state hash、schema hash 与创建 UTC。

不把静态 atlas 二进制、模型原文、用户私密人格或全部渲染缓存写入 snapshot。

### 14.3 升级

任何 realtime engine、clock profile、spatial profile、visual pack 或 agent policy 升级必须：

1. 验证兼容矩阵；
2. pause/drain 当前 world，终止/固化未完成 lease；
3. 生成升级前 snapshot；
4. 在 clone/shadow world 预演迁移，比较关键不变量、due queue、map projection 和 screenshot；
5. 创建新的 version/clock/visual segment，不覆盖旧 hash；
6. 管理员确认后提交；
7. 发生异常时停在可回滚的 pause 状态，恢复旧 snapshot/绑定，而不是半升级运行。

### 14.4 指标与告警

| 域 | 指标 |
| --- | --- |
| 时钟 | offset、uncertainty、source age、node skew、state transitions、unsafe duration |
| scheduler | due lag、lease conflict、frames/s、recovery backlog、frame duration、dead letters |
| state | state hash mismatch、snapshot age、replay mismatch、invariant failures |
| map | manifest load failure、atlas bytes、visible chunks、cache hit、patch lag、draw calls、FPS 分位 |
| asset | generation/review queue、rejected ratio、atlas count、hash mismatch、fallback rate |
| Agent | decision lag、stale rate、budget/circuit state、fallback rate、queue depth |
| reward | eligible/pending/delivered/reversed、risk hold、dedup conflict |

告警不能只发到前端 toast。clock unsafe、state hash mismatch、due queue 持续积压、视觉 manifest 与语义版本不兼容、资产供应链污染和 replay mismatch 均需要后台告警与运行手册。

### 14.5 灾难恢复与备份

备份必须包括：

- 数据库中的 world/snapshot/frame/fact/clock/asset metadata；
- 已发布 visual pack 的 manifest、atlas、provenance 和 hash；
- 版本化内容/profile/rule/agent bundle；
- migration 版本、对象存储版本和保留策略。

恢复演练必须验证：数据库恢复后能读取与历史 manifest 一致的静态资源；没有其中任一项时不能把世界误标记为健康可玩。

---

## 15. 测试与验收矩阵

### 15.1 时钟和 Temporal Frame 测试

1. 同一 clock segment、命令和 due events 在跨进程重启后产生相同 frame/hash；
2. 系统 wall clock 后拨时，effective UTC 不倒退且新写入被正确降级/拒绝；
3. 小幅 slew、大幅前跳、NTP 失联、双源分歧、节点 skew、数据库时钟偏差分别进入预期状态；
4. pause/resume 后 world elapsed time 不包含暂停区间；
5. 进程停服但 world running 时，恢复可按区间结算，不漏掉离散事件；
6. 长时间离线的资源临界点、活动结束、Rule 期限、日历事件按顺序执行；
7. DST 跳过/重复时间、闰日、跨月/跨年、时区迁移被明确处理；
8. test clock 无法用于 production/reward world；
9. 同时间量子内重复命令、并发 node、lease 丢失和 retry 不产生双执行；
10. 旧 V24 tick API 和 realtime API 不混淆字段/单位。

### 15.2 语义与地图投影测试

1. 每种 terrain/structure/furniture/portal/item/entity 语义都映射到批准 asset 或明确 fallback；
2. visual pack 无法改变 collision、path、portal access、建筑容量和 Rule 结果；
3. 同一 seed/profile/pack/cell/entity 的 variant 在刷新、不同浏览器、回放中一致；
4. building exterior、interior、floor selector、portal、遮挡和可见性与后端 DTO 一致；
5. 动态 patch 只更新目标 chunk/layer，不让无关 chunk 重绘或全页重挂载；
6. 未授权室内、物品、Actor、Case 和 overlay 不出现在 payload/atlas 访问中；
7. actor movement 的视觉插值不能越过服务端最后确认位置；
8. visual pack 变更时 manifest 先于 chunk 切换，不能混用图集；
9. lost asset、slow CDN、WebGL failure 和 Canvas fallback 不影响 world state；
10. 所有动态图层有稳定 z-order，无角色穿过屋顶/家具的随机前后跳。

### 15.3 资产管线测试

1. 输入文件白名单、尺寸、透明度、palette、hash、metadata schema 与 atlas 分页验证；
2. 自动检查 sprite anchor、tile grid、seam/autotile、方向帧、缺失 fallback；
3. staging 包无法被生产 world 绑定；
4. published asset 不可覆写；新版本 hash 改变后必须走显式 publish；
5. provenance 缺失、IP/内容审核未通过、prompt 带敏感数据时拒绝；
6. 资源缓存命中与旧 manifest 并存时不读取错误 hash；
7. 角色槽位组合、制服覆盖、饰品遮挡、手持物与动作帧通过截图基准测试；
8. image generation service 失效时，仅资产作业失败，不影响已发布 world。

### 15.4 前端与真实浏览器测试

| 场景 | 必须验证 |
| --- | --- |
| CSP strict | 不出现 unsafe-eval 依赖；pixel renderer 可初始化 |
| 首次加载 | 首屏地面→建筑→Actor 逐层稳定出现，无全页闪烁 |
| 相机平移/缩放 | 不全量卸载，热点区域有局部 loading |
| SSE 断线/重连 | 按 cursor 补拉，无重复实体/回退位置 |
| 低端设备 | Canvas fallback 可查看/选择/执行合法命令 |
| 无障碍 | 键盘、focus、屏幕阅读摘要、高对比、reduced motion |
| 多语言 | 不把可变文本烘焙到 tile；布局不因长文本遮挡场景 |
| 黑暗/浅色 UI | 场景 canvas 和周边组件对比、边界、loading 均清晰 |
| 长会话 | LRU 生效，内存/atlas/scene object 不无界增长 |
| 错误恢复 | manifest/asset/clock/recovery 错误有局部、可解释、可重试显示 |

### 15.5 性能与容量测试

测试必须在目标 VPS 与代表性浏览器矩阵中以真实素材、真实 chunk 密度、动态 Actor 和 SSE patch 进行。至少报告：

- scheduler 在 N 个 running world、M 个 due event、K 个 active Agent 下的 frame P50/P95/P99；
- 长时间恢复的吞吐、事务大小、暂停/恢复时延；
- 单视口可见 tile、建筑、家具、Actor、effect 下的 FPS、draw calls、内存；
- 低网络/高 RTT/丢包时的首次可交互时间和 patch 收敛；
- manifest/atlas 缓存命中率、CDN/Object Storage 失败回退；
- 数据库行增长、snapshot/asset 体积和保留成本。

没有这些报告，不得将 realtime engine/Agent/高密度像素世界标记为“可对所有用户开放”。

### 15.6 端到端可玩性场景

至少实现以下 E2E 场景后才可声明“基本能玩”：

1. 管理员发布已审核 visual pack 和 realtime engine，创建新 world；
2. 用户进入像素地图，看到正确的世界时间和稳定加载的 Chunk；
3. 用户创建外观受控的角色，角色在室外/室内之间移动，进入合法 Portal；
4. 角色开始一项持续活动，刷新页面、断线重连或短暂停服后状态仍正确；
5. 角色触发 Rule/Case，用户看到有权限的摘要，不能通过地图或网络包获取其他证据；
6. 用户角色 Agent 因活动完成而做一次模型/模拟决策，模型延迟或失败时走 fallback，不停世界；
7. 一项内容定义任务完成，奖励先形成资格/Outbox，再按策略发放且重复重放不重复发币；
8. 管理员 pause/resume、时钟告警、visual binding 升级预演、资源缺失和回放诊断都有可操作结果。
9. 两名已授权用户进入同一 `world_id`：A 的角色对公开/相互可见对象产生已接受命令后，B 在同一 timeline cursor 收到 patch；并发争用由版本/占用规则确定性解决；B 仍无法读取 A 的人格、私有记忆、受限室内或未公开证据。两名用户的断线/重连均不暂停或复制世界。

---

## 16. 管理面、用户面与权限

### 16.1 管理员功能

管理员可：

- 发布/停用 clock profile、查看时间源健康和恢复状态；
- 创建、暂停、恢复、归档、克隆 realtime world；
- 发布 spatial/visual/content/rule/agent bundle 并查看兼容矩阵；
- 创建资产请求、审核候选、运行 staging 截图、发布或废弃 visual pack；
- 查看 scheduler、Frame、due event、asset/manifest、Agent 与奖励诊断；
- 在受控测试环境使用 frozen test clock；
- 执行升级预演和明确升级确认。

管理员不得：

- 编辑已经 committed 的 world time、Frame、Fact、state hash；
- 通过 UI 直接修改角色坐标、余额、处罚或奖励结果；
- 给生产 world 加速、回拨时间或跳过 due event；
- 直接把未审核图片/提示词发布到玩家世界。

### 16.2 普通用户功能

普通用户可：

- 在被授予的共享 realtime world 中与其他成员在线查看同一像素地图、当前时间、自己的角色和可见公开对象；
- 创建/调整自己的 approved appearance profile；
- 查看自己的角色 Agent 状态、授权模型概要、生活事件、合法可执行操作和奖励摘要；
- 提交合法行动意图，查看服务端拒绝原因；
- 使用镜头、地图筛选、无障碍和显示偏好。

普通用户不得：

- 创建/暂停/加速城市、切换 clock profile、发布素材、直接生成图片；
- 查看其他用户私有室内、人格、Agent memory、未公开资产或时钟节点诊断；
- 上传图片、任意 asset ID、任意模型 prompt 或修改世界视觉包；
- 让角色 Agent 绕过时间、空间、Rule、资源、职业或奖励限制。

### 16.3 角色 owner、world member 与平台管理员

| 主体 | 可读 | 可写 | 明确禁止 |
| --- | --- | --- | --- |
| 平台管理员 | 全局健康、受限审计、所有已授权 world 配置 | 发布 bundle、生命周期、资产审批、恢复控制 | 改历史 Frame/Fact 或伪造玩家命令 |
| World owner/治理成员 | 同一共享 world 的摘要、成员可见空间、有限治理信息 | 已授权治理命令 | 将 world 变成个人副本、时钟/资产全局配置、其他用户私密数据 |
| Character owner | 自己的角色、appearance、Agent 摘要、可见地图 | 合法角色命令、approved appearance | 直接改坐标/外观资源/奖励 |
| 普通 member | 被授权公共视图与操作 | 由角色/成员角色允许的命令 | 私有室内、NPC 私有 memory、管理诊断 |
| NPC Manager/系统 Agent | 定义域的脱敏语义 | allowlist 候选命令 | 平台资产、原始用户人格、视觉包写入 |
| Renderer/浏览器 | 已授权 projection/manifest | 本地镜头/视觉偏好 | canonical state、权限、时间和资产发布 |

### 16.4 功能开关

建议分离以下开关，默认全部关闭：

| 开关 | 作用 |
| --- | --- |
| city_realtime_engine_enabled | 是否允许新 realtime engine 注册/创建 |
| city_realtime_scheduler_enabled | 是否实际领取并提交实时 Frame |
| city_pixel_renderer_enabled | 是否向授权前端提供 pixel manifest/chunk API |
| city_visual_pack_publish_enabled | 是否允许发布新素材包 |
| city_character_appearance_enabled | 是否允许用户编辑 approved 外观 |
| city_agent_realtime_enabled | 是否允许 Agent 使用 realtime observation/scheduler |
| city_realtime_rewards_enabled | 是否允许 realtime world 产生奖励资格 |

开关关闭时的行为必须为“安全暂停/只读/明确提示”，而不是删除状态、降级到客户端时间或回到未版本化 ASCII。

---

## 17. 分阶段实施计划与停止条件

### R0：设计冻结与契约审查

内容：

- 冻结 realtime engine 命名、clock profile、time quantum、Temporal Frame、暂停/追赶/DST、错误码和版本向量；
- 审查总体设计、Agent 文档、空间/生成器文档的冲突；
- 冻结 visual pack、asset metadata、appearance profile 和版权/审核流程。

停止条件：

- 时钟/回放/视觉/Agent 的责任边界没有互相矛盾；
- 可回答“停服一天、系统时钟回拨、视觉包升级、模型晚到、角色进楼、资源缺失”分别如何处理；
- 无需写业务代码即可画出所有持久化表与状态转换。

### R1：可信时钟与无视觉的 realtime kernel

内容：

- Clock Authority、节点健康、clock segment、world time state、Temporal Frame、due event、lease/recovery；
- 只接入一个小型测试 world 和无视觉的诊断 API；
- 将现有 V24 代码路径完全隔离。

停止条件：

- 真实/模拟时钟异常、暂停、恢复、DST、长时间离线、跨节点竞争和重放测试通过；
- 生产 world 不存在 speed multiplier 绕过；
- 无空秒事务风暴。

### R2：视觉包基础与静态像素地图

内容：

- asset request/review/atlas/manifest、第一套原创 staging pack；
- semantic chunk 到 pixel projection、Vue store、CSP-safe Pixi renderer、Canvas fallback；
- 地形、道路、静态建筑、镜头、选择、键盘与无障碍。

当前进度：

- 已完成：程序化 pack manifest、不可变 world binding、发布策略、管理员审计控制面、受控 generation request、CSP-safe Canvas 2D projection 与 feature-gated 像素入口；
- 未完成：安全 worker、候选 raster 隔离存储、自动/人工技术 QA、atlas 规范化、对象存储签发、素材导入、真实 asset resolver、Pixi atlas 批渲染、角色外观和动态对象；
- 因此“生成申请”目前是审核/队列契约的占位实现，不代表后台会偷偷调用任何第三方图像模型，也不代表已生成图片会进入游戏。

停止条件：

- 不再使用 ASCII 作为 realtime world 玩家主地图；
- 严格 CSP 下无 unsafe-eval；
- 全地图不重挂载，chunk 局部加载/缓存/ETag 正常；
- 视觉包不可改变逻辑状态。

### R3：建筑室内、角色外观与动态对象

内容：

- floor/cutaway、家具、Portal 视觉、item/field/vehicle；
- appearance profile、角色分层、步行动画、activity 状态、动态 patch；
- 用户外观编辑和管理员包管理。

停止条件：

- 室内权限、层级遮挡、位置插值、衣物组合、缺失 asset fallback 和截图基准通过；
- 外观不泄露私人数据、不影响属性、不允许任意图像。

### R4：实时 Actor、经济/规则和 Agent 接入

内容：

- 移动/活动/服务/Rule/连续结算转为 due event/Temporal Frame；
- Agent Observation 从 tick 升级为 timeline cursor；
- NPC Manager 与用户角色 Agent 按实时活动边界工作；
- 受控奖励资格以 realtime facts 为输入。

停止条件：

- 用户角色能完整自主生活而不靠每秒模型调用；
- 模型延迟/故障、时钟异常、世界恢复和奖励 outbox 无状态破坏；
- Agent、NPC、用户和系统权限完全隔离。

### R5：灰度、观测与生产开放

内容：

- 管理员 shadow world、白名单用户、容量压测、运行手册、审计导出；
- 逐步扩大 world/Agent/视觉包范围；
- 保持 V24 和旧世界只读/运行兼容。

停止条件：

- 指标、告警、回滚、备份、视觉包回退和客服/管理诊断流程可用；
- 真实浏览器、目标 VPS、严格 CSP、长时间运行报告全部达标；
- 无未解决的 clock safety、state/replay、权限、资产 provenance 或奖励重复问题。

---

## 18. 实施前必须冻结的开放问题

下表不是可在编码时“顺便决定”的事项。进入对应阶段前必须确定：

| 主题 | 需要冻结的选择 | 当前建议 |
| --- | --- | --- |
| clock source | OS NTP/NTS、私有服务、监控和故障阈值 | system_ntp 或 system_nts + 多源健康监控 |
| quantum | 1 s、100 ms 或其他 | realtime-v1 固定 1 s |
| 停服语义 | running 是否包含故障时间 | 包含；只有显式 pause 排除 |
| 暂停语义 | world elapsed time 是否冻结 | 冻结，resume 新建 segment |
| DST policy | 跳过/重复本地时刻 | 跳过取下一个存在时刻，重复 schedule key 去重 |
| 多节点拓扑 | 单 scheduler/HA、数据库时间容差 | 先单活性正确，后 HA；均需 lease 和 skew 门禁 |
| engine 名称 | realtime engine 的正式版本号 | 实施时按阶段注册表分配 |
| V24 升级 | 是否/何时支持 | 首版不提供生产升级，只做 clone 预演 |
| visual pack 首发 | 首发原型包、审查人、版权存档 | 一个原创 staging pack，审核后再发布 |
| 图像生成服务 | 具体模型、数据协议、费用、存储地域 | 管理端受控 provider；不将用户数据送入生成 |
| asset 存储 | repo、对象存储/CDN、备份与保留 | 内容寻址对象存储 + manifest 归档 |
| 人物细节 | 首发 body/衣物数量、动画状态 | 小而完整的兼容矩阵，禁止无限槽位 |
| UI 风格 | 地图周边组件与像素场景的边界 | 项目组件体系 + pixel scene，不把网站特效盖在场景上 |
| agent fallback | 模型延迟/失败时的行动 | content-defined deterministic fallback |
| 奖励 | 是否在 realtime/Agent 首发同时开启 | 默认关闭，先完成事实/outbox/反刷闭环 |
| 可视范围 | 迷雾、室内、多人可见规则 | 后端 DTO 权限优先，视觉只呈现允许数据 |
| 性能目标 | 最低设备、目标 viewport、峰值 Actor/chunk | 用目标 VPS/浏览器压测后签字冻结 |

---

## 19. 完成定义

实时像素世界只有在以下条件同时满足时，才可称为“可玩”：

1. 新 world 的时间由服务端可信 UTC 和可审计 segment 驱动，客户端不能改变；
2. 没有渲染帧进入 canonical state，世界可在闲置时无空写入地前进；
3. 暂停、时钟漂移、停服恢复、DST、追赶和多节点租约均有测试与可操作告警；
4. 世界的空间、经济、Actor、Portal、Rule/Case、Agent 和奖励仍以 Fact/Effect/账本为权威；
5. 玩家能在没有 ASCII 主地图的情况下查看、移动、进入建筑、理解角色/物品/状态并执行合法操作；
6. 像素地图、室内、建筑、角色、衣物、物品和效果来自已审核、版本化、可追溯的原创视觉包；
7. 严格 CSP 下无 unsafe-eval；资源故障、WebGL 失败和 SSE 断线均有局部、稳定、可解释的降级；
8. 用户外观只使用批准部件，不泄露隐私、不引入真人/任意上传图片，也不影响游戏数值；
9. Agent 的模型延迟、失败、人格文本和模型供应商故障不影响时钟、回放、账本或地图权威；
10. 旧 world 未被隐式修改，新 realtime world 的版本向量、视觉绑定、快照和 replay 均完整；
11. 目标环境完成容量、内存、时钟健康、资产安全和长会话验证；
12. 管理员能诊断问题、暂停/恢复、升级预演和回退，而无需直接修改数据库状态。

---

## 附录 A：当前组件到目标组件的映射

| 当前组件/概念 | 目标替代或延续 | 迁移原则 |
| --- | --- | --- |
| cityTickDuration = 1 hour | realtime clock profile + Temporal Frame | V24 保持，realtime 新版本使用 |
| CityTickScheduler | Realtime Scheduler + due event lease | 共用调度基础可复用，不能混淆事件语义 |
| city_commands.expected_world_tick | expected_timeline_cursor + entity versions | 新 API 平行演进 |
| CityClassicViewport | PixelSceneRenderer | 旧 world 保留 classic viewer；新 world 不使用 glyph 主渲染 |
| projection.ts glyph/color | semantic projection + visual resolver | glyph 仅兼容/诊断，sprite/atlas 映射版本化 |
| CityChunkCache | 静态 semantic chunk + dynamic patch + atlas LRU | 保持局部缓存，避免全页刷新 |
| CitySpatialDefinition.sprite | visual pack mapping | 不允许未经 manifest 的任意 URL |
| speed_multiplier | production fixed real-time / test clock | 不做“实时世界加速” |
| Agent observed_tick | timeline cursor + observed world time | Agent 只在新 engine 下切换 |

## 附录 B：读者自检问题

在进入实现前，任何评审者应能够仅凭本文回答：

1. 服务器停机 36 小时、world 没有 pause 时，恢复过程如何保证不丢事件也不写 129,600 个空 tick？
2. 用户把电脑时间调快/调慢，为什么不会影响角色移动、奖励或法律期限？
3. NTP 发生大回拨时，为什么不会产生倒退的世界时间或重复发奖励？
4. 一个视觉包升级为什么不能影响道路碰撞、地图生成或资产账本？
5. 用户看到的角色行走很流畅，但后端为什么不需要每帧保存位置？
6. 模型 30 秒后才回答时，角色和城市发生什么，为什么模型不能“补写过去”？
7. 如何保证新像素素材是原创、可审核、可定位、可回滚而不是一张随机生成图片？
8. 为什么角色的眼睛、头发、衣物可组合，但不会让用户上传真人图片或得到数值优势？
9. 为什么 V24 world 不会被实时化或换图后出现不可回放的问题？
10. 在 strict CSP、WebGL 不可用、素材 CDN 暂时失败时，玩家会看到什么，世界会不会停？

若其中任一问题没有明确答案，不得进入相应实现阶段。
