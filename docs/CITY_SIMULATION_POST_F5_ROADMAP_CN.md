# 城市模拟 F5 后续基础层路线与详细设计

版本：v3.4（2026-07-21）
当前新世界版本：`city-openworld-v24`（V3 开放世界生成、V4 Actor runtime、V5 社会运行时、V6 不可变版本向量、V7 服务协同、V8 跨域 effect、V9 聚合 mobility、V10 跨尺度到达桥接、V11 版本化自动 OD source/周期指标、V12 容量受限住宅—就业绑定、V13 双向 commute source、V14 append-only assignment lifecycle、V15 F10.0 企业订单/库存保留/交付/结算、V16 F9.2.C 企业货运 source→V9 适配、V17 custody/receipt gate、V18 overflow batch freight、V19 静态 spatial node/corridor、V20 通用可变基础设施 asset lifecycle、V21 effective-capacity admission/动态替代路径、V22 F10.3 partial freight settlement / no-receipt failure closure、V23 F10.3.1a manual carrier reserve / one-to-one claim recovery，以及 V24 F10.3.1b versioned carrier service contract / cash freight fee settlement 均已完成；`city-f8-v3` 仅为历史兼容链）
主线：先建设真实城市本体；下一切片仅能在 V24 固定 quote 与现金支付边界之上评审报价输入、SLA、保险、在途库存与多企业生产，利率、银行信贷和股票仍是独立可选经济分支

> 本文保留 F5 后实施切片的详细历史。总体架构、worldgen-v2、V7–V24 与后续顺序以《城市模拟游戏总体设计》v2.4 为准。新功能必须落入 `city-openworld-v24+` 链，不能把旧 F7 固定地图重新作为默认空间底座。

## 1. 总体决策

F0–F5 已建立世界、确定性 tick、城市复式总账、实物守恒、基础市场以及快照/重放/恢复；F6.1–F6.3 已补齐日历、人口自然变化、人口迁移和独立家庭数量闭环；F7.0–F7.11 已完成真实空间、地块建筑、开发施工、企业经营场所、开放世界 Actor、空间控制、成员调度、静态通行、动态 Portal 和持续移动意图。F8.0 已完成版本化公共服务目录、真实建筑设施、容量、主体需求、连接、整数损耗、确定性分配、不可变结算、规范哈希、回放恢复、查询 API 和前端工作台；F8.1 又补齐设施投运、维护/维修/退役、人员、磨损故障、材料、总账与预算守恒；F8.2 已完成通用物理网络、容量路由、逐段损耗、流量守恒、隔离故障、诊断查询和拓扑工作台。当前按要求停止，不提前进入 F8.3。

后续功能必须遵守以下顺序：

```text
F6 日历与人口生命周期
  ↓
F7 地块、分区、建筑与开发
  ↓
F8 公共设施与城市服务
  ↓
F9 交通、通勤与物流
  ↓
F10 多企业、行业和供应链
  ↓
F11 家庭福利、迁移与城市吸引力
  ↓
F12 环境、灾害与韧性
  ↓
F13 场景、校准、调度、指标、奖励和可玩产品层
  ├── E1 银行、利率与信贷（可选）
  └── E2 证券与股票（可选）
```

E1/E2 不是 F6–F13 的替代品，也不能提前用随机利率或随机行情伪装经济深度。

## 2. 跨阶段工程契约

### 2.1 每个子系统的五类数据

每个新子系统必须明确区分：

1. **配置定义**：版本化参数、类型和规则，不在 tick 内随意变化。
2. **不可变事实**：movement、allocation、operation、settlement 或 event。
3. **在线投影**：为下一 tick 和查询优化的当前状态。
4. **规范状态**：参与 `state_hash` 的最小决定性状态。
5. **派生指标**：可由事实重算的时间序列，不得反向驱动业务，除非先固化为命令或规则输入。

任何接口都不能直接设置人口、建筑库存、服务容量、交通流量或企业余额；写入必须通过严格命令和 tick 事务。

### 2.2 数值和单位

- 人口、家庭、岗位、住房、车辆、建筑面积和资源使用整数最小单位。
- 比例统一使用 `milli`（千分）或 `ppm`（百万分），字段名必须体现比例尺。
- 距离使用米，时间使用秒，面积使用平方米，能量和水量使用定义明确的整数单位。
- 所有除法明确舍入方向和余数去向；不得把 `float64` 写入规范状态。
- 累加、乘法和时间边界运算必须检查 `int64` 溢出。

### 2.3 F5 接入门槛

每一阶段完成前必须同步增加：

- 规范状态字段与稳定排序。
- 不可变事实归约器。
- 当前投影恢复映射。
- 同种子跨数据库 ID 的确定性测试。
- 投影漂移检测与恢复测试。
- 至少一个长期黄金场景。

缺少其中任何一项都只能停留在实验开关，不能升级正式世界版本。

### 2.4 版本策略

- 每个正式阶段使用新模拟版本，例如 `city-f6-v1`。
- 旧世界不静默换规则；升级需预演、生成迁移基线并记录前后哈希。
- 参数包拥有独立 `parameter_set_code` 和版本；代码版本与参数版本同时进入规范状态。
- 规则只能读取当前世界绑定的参数包，不能读取会随部署变化的全局默认值。

## 3. F6：日历、人口与家庭生命周期

F6 先让人口成为可解释、守恒、可长期演化的事实，再加入迁移和家庭行为。F6.1、F6.2 与 F6.3 均已完成并通过组合集成验证。

### 3.1 F6.1 日历边界与人口自然变化

#### 目标（已实现）

- 从基础小时 tick 派生唯一的日、月、季度和年边界。
- 以家庭群体为聚合单位记录出生、死亡和年龄迁移。
- 保证人口、劳动年龄人口和家庭需求在任何边界都不凭空变化。

#### 数据表

`city_calendar_states`

- 每世界一行。
- 当前日期、日/月/季度/年序号。
- `last_daily_tick`、`last_monthly_tick`、`last_quarterly_tick`、`last_annual_tick`。
- 投影版本和规范元数据。

`city_demographic_policies`

- 年龄分组定义版本。
- 各年龄/收入群体出生率、死亡率、劳动年龄进入/退出率。
- 最小/最大边界和余数累计规则。

`city_population_movements`

- 不可变 movement 头：世界、tick、序列、月边界、参数版本、出生/死亡/年龄迁移合计和封账时间。
- 首批使用原子的 `natural_change`，避免出生、死亡和年龄迁移只封账一部分。
- 只允许 `draft → posted`，封账后不可更新/删除。

`city_population_movement_lines`

- 每个家庭 cohort 的出生、三类死亡、两类年龄迁移、六类余数、变更前后年龄结构和投影版本。
- 同一 movement 内按稳定 line number 排序。

#### 日历算法

基础时钟仍是 UTC 小时 tick，边界由世界 IANA 时区解释：

1. 比较 tick 前后模拟时间在世界时区中的日期。
2. 跨日时写 `city.calendar.day_started`；跨月、季度、年时按固定顺序写对应唯一事实和事件。
3. 每个基础 tick 最多跨一个本地日期；任何跳日都会触发确定性不变量错误。
4. 夏令时只影响本地边界映射，不改变基础模拟时长。

所有边界由 `(world_id, boundary_type, local_date)` 唯一约束，重复 step 不得再次结算。

#### 人口算法

首版采用聚合 cohort 和确定性余数累计：

```text
numerator = population_units × annual_rate_ppm + carried_remainder
movement  = floor(numerator / periods_per_year / 1_000_000)
remainder = numerator mod (periods_per_year × 1_000_000)
```

- 自然变化按月结算，避免每小时四舍五入造成长期偏差。
- 出生进入原家庭 cohort 的儿童年龄组；收入带和区域不在自然变化阶段迁移。
- 死亡不能超过来源人口。
- 年龄迁移的来源减少必须等于目标增加。
- 劳动年龄人口变化只能来自年龄迁移和迁移事件，不由就业市场直接修改。
- 首版完全由整数公式和累计余数决定，不需要随机抽样；未来若细分个体，只能使用版本化 PRNG 且不得改变聚合守恒量。

#### 不变量

```text
期末总人口 = 期初总人口 + 出生 - 死亡
年龄迁移全城净变化 = 0
0 <= working_age_units <= population_units
0 <= employed_units <= working_age_units
每个 movement 的 line 前后版本连续
同一月同一规则版本最多一次自然变化结算
```

#### 命令与 API

F6.1 不开放“设置人口”命令。管理员/玩家只可通过后续政策命令变更未来使用的参数版本，且不能追溯改写已经结算的月份。

读取接口：

```text
GET /api/v1/city/worlds/:world_id/calendar
GET /api/v1/city/worlds/:world_id/population
GET /api/v1/city/worlds/:world_id/population-movements
GET /api/v1/city/worlds/:world_id/population-movements/:tick/:sequence
```

列表使用 `(after_tick, after_sequence)` 游标。

#### 验收（已通过）

- UTC、Asia/Shanghai 和 America/New_York 夏令时、月末、季度末、年末、闰日边界唯一。
- 同 seed、同参数、不同数据库 ID 的跨年人口结算和状态哈希完全相同；10 年日历与 120 个月人口纯函数黄金场景无漂移。
- 任意 movement 都满足人口守恒和版本连续。
- 直接更新 cohort 人口或封账 movement 被数据库拒绝。
- F5 能从 tick 0 重放、发现人口投影漂移并恢复。

### 3.2 F6.2 迁入、迁出和区域迁移（已实现）

在 F6.1 长周期稳定后已增加：

- `migration_in`、`migration_out`、`district_move` 三类 movement。
- 迁移需求由住房可得性、就业机会、实际收入、通勤、公共服务和环境组成的版本化评分决定。
- 评分只使用上一个已封账周期指标，避免同 tick 循环依赖。
- 全城内部区域迁移净人口为零；外部迁移显式改变城市人口。
- 迁入受住房容量和城市接纳能力限制，迁出不超过来源 cohort。

### 3.3 F6.3 家庭形成、拆分与收入层迁移

F6.3 已完成以下底层闭环：

- 聚合家庭单位与人口分离，`household_units` 成为住房需求事实，平均家庭规模只作为派生读模型。
- formation、split、merge、dissolution 和相邻收入层 reclassification 产生不可变 household movement/line。
- 收入层迁移同步年龄人口、就业、家庭、occupied/unmet 投影，不创建 journal，也不改变聚合家庭主体账户余额。
- 极端人口自然减少时在同一事务生成 `demography_guard` dissolution，提交前恢复家庭数不超过人口数。
- `city-f6-v2 → city-f6-v3` 支持 dry-run/apply；新状态接入规范哈希、快照、逐 tick 重放、漂移诊断和 verified recovery。
- 自动社会流动评分仍暂缓，未来只能读取完整历史窗口并生成下一 tick 命令。

详细协议和验收证据见《城市模拟 F6.3 家庭生命周期与收入层迁移详细设计》。

## 4. F7：地块、分区、建筑与开发

### 4.1 目标

把 F3 的区域聚合容量拆为可追溯的土地和建筑供给，使住房、企业选址和公共设施拥有真实空间约束。

### 4.2 核心模型

- `city_parcels`：地块、区域、面积、可建设面积、地形和环境约束。
- `city_zoning_rules`：用途、容积率、建筑高度、覆盖率和兼容用途。
- `city_buildings`：用途、楼面面积、容量、质量、建成年份和状态。
- `city_building_units`：住宅/商业/工业单位及占用。
- `city_development_projects`：申请、审批、建设、完工、取消的状态机。
- `city_land_movements` / `city_construction_operations`：土地占用和建设实物事实。

### 4.3 不变量

- 地块占用面积不超过可建设面积。
- 建筑面积满足分区容积率、高度和覆盖率。
- 建设消耗土地使用权、劳动力、资本品和材料，并通过 F2/F3 结算。
- 单位容量之和不超过建筑容量。
- 拆除不会删除历史建筑，只进入终态并释放后续可用土地。
- F3 区域聚合容量必须等于 F7 明细投影之和，直到迁移完成前使用明确的兼容桥接版本。

### 4.4 实施切片

1. F7.0 空间规则、坐标、字符/像素回退与固定 hash（已完成）。
2. F7.1 世界规则绑定、Overmap、Chunk Mapgen、空间事实与重放恢复（已完成）。
3. F7.2 CLASSIC 字符视口、Chunk 缓存、检查器和文本导出（已完成）。
4. F7.3 地块、分区、住宅/商业/工业建筑、单位池和住房占用迁移（已完成）。
5. F7.4 开发项目、审批、资源结算、施工、完工和动态建筑调整（已完成）。
6. F7.5 商业/工业建筑占用、企业经营场所和原子迁址（已完成）。
7. F7.6–F7.8 开放世界 Actor、空间控制、成员、可信调度和命令回执（已完成）。
8. F7.9 静态通行、Actor 占用、移动重校验和确定性路径查询（已完成）。
9. F7.10 动态 Door/Portal、声明式访问授权和导航双重阻断（已完成）。
10. F7.11 NPC movement intent、行动预算、确定性重规划和拥堵预留（已完成）。

## 5. F8：公共设施与城市服务

### 5.1 F8.0 通用服务协议（已完成）

- 版本化服务与设施类型目录。
- 绑定真实建筑的设施及逐服务容量。
- 绑定 district/building/household/enterprise/actor 的需求。
- 连接容量、千分损耗、优先级和确定性竞争分配。
- 不可变 allocation/settlement fact、canonical、snapshot、replay、recovery。
- 稳定游标查询 API 与无整页刷新的操作工作台。

### 5.2 领域子系统

- 电力：发电/外购、变电容量、区域需求、停电和储备率。
- 供水：水源、处理、管网容量、需求和漏损。
- 污水/垃圾：处理能力、收集覆盖和环境外部性。
- 教育、医疗、消防、治安：设施容量、服务半径、人员和预算。

### 5.3 数据与事实

- 设施定义和区域服务连接。
- 每周期 service demand、allocation、shortage settlement。
- 建设、维护、故障和修复 operation。
- 服务质量是供给/需求、可达性和可靠性的派生投影。

### 5.4 不变量

- 分配量不超过设施可用容量和网络输送容量。
- 能源、水、材料和维护资金均由 F2/F3 事实提供。
- 故障只降低未来可用容量，不修改过去结算。
- 服务短缺通过健康、生产率和吸引力的下一周期输入传播，禁止同 tick 无限反馈。

## 6. F9：交通、通勤与物流

### 6.1 V9.0 已完成：封存的聚合 mobility 图

- 每个 V9 world 在 genesis 或 V8→V9 暂停升级时，封存设施 hub、区域 hub 与中央 interchange；其模式、边、容量、版本和 content hash 进入 canonical state。
- 内置 `walk`、`transit`、`freight` 三个模式；边以设施—区域 local 与区域—中心 trunk 表达，不声称是车道级网络。
- Actor 通过 `open_world.actor.mobility.request` 提交需求；请求记录来源 hub、目的 hub、模式、目的、单位、最早出发 tick 与截止 tick，客户端不能提交路线或结算结果。
- 调度只处理此前 tick 已接受的需求，按稳定顺序做最短路径和每边容量预约；拥堵延迟由已预约占用率以整数 milli 计算。
- route 在到达 tick 生成完成 fact；它不会修改 Actor 坐标。需求、route、allocation 和 actor metric 都可从快照恢复并参与回放验证。
- V9 配置、拓扑和动态投影由数据库触发器保护；路线/需求循环外键在恢复事务中延迟到 commit 校验，不放松引用完整性。

### 6.2 V10 / F9.1 已完成：受验证的跨尺度到达

- 仅 V10 新 demand 会在接受时封存 V5 origin；V9 completed fact 在后续 tick 变成 `mobility.arrival.pending`，不会同 tick 改写坐标或复写 V9 route。
- 目标 Sector 由已绑定的 V2/V3 worldgen 物化，V5 surface passability 与 occupancy 选出确定性落点；缺少落点会产生有限次数的 blocked 事实，之后明确 failed。
- V9→V10 的 arrival baseline 排除旧 demand，升级不会为旧 route 或历史世界伪造局部位置。

### 6.3 V11 / F9.2.A 已完成：版本化自动 OD source 与闭合指标

- genesis 或 V10→V11 暂停升级时，从 V5 的非 dormant NPC `work_facility` 封存 `npc.assigned_facility_visit` source；source code、Actor、Facility hub、模式、目的、周期、相位、版本和 metadata 全部进入 canonical state。
- source 不伪造 home、household、企业订单或任意“常住地”。到期时读取 Actor 当时已验证的 V5 局部位置作为 origin；有 active navigation intent、尚未结束的 mobility demand、位置无效、目的 hub/模式不可用时写 `system.mobility.od.suppressed`，不偷偷改写位置或补造需求。
- 成功时先写 `system.mobility.od.generated`，再写子事实 `mobility.requested` 并创建带 `od_source_code`/captured-origin metadata 的 V9 demand。V9 仍只在后续 tick 调度；V10 仍只在 route completed 的更后续 tick 落地。
- 每个 24 tick 窗口在下一 tick 关闭一次，封存 automatic source 的 generated/suppressed 数和全网络 request/schedule/complete/expire、arrival、travel/congestion、peak occupancy 指标。关闭后不回写历史窗口；长期 route 的完成会出现在它实际发生的窗口。
- profile、source、metric、fact 关系由数据库触发器和 deferred FK 保护；state 已进入 snapshot、replay、recovery、版本向量及只读 `open-world/mobility/od` API。V10→V11 只建立未来 source baseline，不重分类历史 V9/V10 行程。

### 6.4 V12 / F9.2.B.0 已完成：住宅—就业绑定底座

- genesis 或 V11→V12 暂停升级时，只为基线时 active、非 dormant、具有 active employment role 和有效 work Facility/hub 的 NPC 封存 `npc.residence_employment` binding。
- valid V5 home 优先；否则在真实 active residence Facility 的容量中按 Actor/Facility 的稳定 hash 分配。无法分配时只增加 `unbound_candidate_count`，不以当前位置、工作地、household cohort 或 UI 字段伪造住宅。
- binding 固化 home/work Facility+hubs、employment role、24 tick outbound/return phase、算法 contract 与 metadata；它不产生 mobility demand，不改写 V11 source、V9 route 或 V10 arrival。
- profile/binding 由不可变写闸门、capacity/foundation assertion、canonical snapshot、replay、recovery、V12 version-vector catalog 和只读 `open-world/commutes` API 保护。V13 必须重新验证活跃状态、当前位置和预期 building origin，不能把 V12 historical binding 当成即时移动许可。

### 6.5 V13/V14 已完成：从绑定到可演进通勤

- V13 已完成家—工作/工作—家 source：它只消费 V12 binding，必须用当前已验证的 Facility Presence Domain 作为起点门槛；Actor 不在预期起点或发生冲突时写明确 suppression fact 而非传送。V11 generic source 对拥有完整 V13 pair 的 Actor 以 `superseded_by_commute_source` 保留审计而不产生重复需求。
- V14 source lifecycle 已通过新的 assignment epoch、transition 与 source pair 处理离职、迁居、设施关闭、临时停工和管理员 rebind；它不 UPDATE 或删除 V12/V13 历史证据，且以 deferred assertion、canonical/replay/recovery 和 V13→V14 paused upgrade 保护完整性。
- 企业订单、库存 ownership、交付与 journal 事实完整前不得引入 enterprise freight source；不能把 V5 work facility 或 V12 binding 误当企业物流订单。
- 逐 source 输出未满足出行、旅行时间和拥堵输入；按下一周期规则供人口、企业和市场消费，不能让指标同 tick 反向改写路由。
- 由 style profile/worldgen 内容包提供道路等级、站点、换乘与货运设施；不同城市原型不能由 reducer 内的国家分支实现。

### 6.6 V16 / F9.2.C 已完成：企业货运 source→V9 适配

- 只消费 V15 已 `dispatched` 的订单、冻结行快照、企业节点和 facility hub，下一 tick
  建立一个 `system.freight.carrier` 驱动的 V9 `freight` demand；不写库存、journal、
  V15 `delivered` 或任何钱包变化。
- V16 原子映射的最大总量固定为 V9 冻结 freight 图最窄 local edge 的 32 cargo
  units/tick；超过上限必须落为可审计 `suppressed` source，而不是制造永远无法
  排程的 demand。拆单、车队与在途库存仍属于后续独立版本。
- V9 schedule/complete/expire 只被观察为 V16 source 事实。V15 在 pending 阶段
  终态时只使 V9 demand expire；已 scheduled/completed 的运输保留 allocation 并标记
  `transport_orphaned`。每 tick 先投影 V9 结果，再处理 V15 terminal，避免竞态
  误判。
- demand metadata 显式 `arrival_bridge=excluded`，所以 V10 不会为 carrier 创建
  局部位置；路线完成仍不是收货。新世界、V15→V16 paused upgrade、canonical/replay/
  recovery、成员范围读取和 PostgreSQL 集成测试均已覆盖。

### 6.7 V17 / F10.1 已完成：货物 custody、到达观察与 receipt gate

- V17 为 baseline 后的每个可运输 V16 source 冻结一张 shipment 和 line snapshot；V16
  `demand_pending`、`route_scheduled`、`route_completed`、`demand_expired`、`voided`、
  `transport_orphaned` 只通过 append-only evidence 映射为 custody transition。
- `route_completed` 只会进入 `awaiting_receipt`。既有 V15 `order.deliver` 只有在该状态
  才可执行，并在同一事务将 V15 inventory/resource operation 与 V17 receipt 绑定；不会
  创建第二份库存余额，也不会自动收货。
- V16→V17 paused upgrade 以 baseline 保护前序 source：旧 source 保持 legacy delivery，
  不回填伪造 shipment 或 receipt。canonical snapshot、replay、verified recovery、迁移
  guards、成员范围查询与 PostgreSQL integration 已覆盖。
- V18 已追加 baseline 后 V16 overflow source 的确定性拆单、独立 V9 consignment 与全量 receipt gate；部分 receipt、拒收/货损和承运责任仍必须由后续版本追加，且不得改写 V17/V18 已封存的事实含义。

### 6.8 目标指标（尚未全部接入）

- 平均/分位通勤时间、未满足出行、拥堵指数。
- 区域可达岗位、可达住房和物流成本。
- 交通能源消耗、排放和财政维护成本。

### 6.9 边界

首版不在在线 tick 中嵌入 SUMO/MATSim。高精度交通可作为离线场景适配器，输出必须固化为版本化输入包，不能在同一 seed 下随外部服务结果漂移。

## 7. F10：多企业、行业与供应链

### 7.1 从单企业过渡

当前基础市场只有一个示范企业。F10 增加：

- 行业定义与投入产出表。
- 企业创建、扩张、收缩、破产和退出事实。
- 多企业生产配方、库存、订单和交付。
- 劳动力按技能/区域分配。
- 企业定价、采购和销售在市场 settlement 中闭环。

### 7.2 财务闭环

- 每家企业继续使用 F2 科目和 journal，不创建旁路利润字段。
- 利润、资产负债和现金流由总账派生并按期封账。
- 破产先冻结新承诺，再按明确优先级清算资产和债务。
- 企业进入必须带来源明确的资本和资产；退出不删除历史。

### 7.3 供应链守恒

- 每笔中间品交付同时减少卖方库存、增加买方库存并结算现金。
- 未交付订单不能提前成为可用库存。
- 投入产出表和配方版本决定技术，企业不能生产未授权商品。
- 供应中断通过积压、价格和产能利用率逐周期传播。

### 7.4 V17 已完成：receipt 与在途 custody

V17 已将 V16 route evidence 固化为 shipment custody，而非把它当作 receipt。只有
`awaiting_receipt` shipment 才能调用 V15 delivery；该原子命令在库存 transfer 后写入
V17 receipt fact 与 receipt projection。V17 仍不建立独立可扣减的在途库存余额，因而
不会重复记账。

V18 已定义并实现订单拆分、批次 consignment 与“全部到达后一次原子收货”：它不实现部分收货、拒收、损耗、运费、保险或承运责任。后续版本必须为这些新含义追加事实和 profile，不能复用或重写 V17/V18 已有字段。

## 8. F11：家庭福利、需求与城市吸引力

- 可支配收入、住房成本、通勤成本、服务质量和环境质量共同决定福利。
- 消费篮子由收入带和价格决定，必须受现金和库存约束。
- 贫困、失业、住房负担和服务缺口形成政策可解释指标。
- 迁移使用滞后福利指标，避免当前周期自激振荡。
- 政府补助通过 F2 journal，资格通过版本化规则和不可变 allocation，不能直接设置家庭余额。

F11 完成后，F6.2 的迁移评分从简化参数切换到真实城市反馈，但需升级模拟版本并保留旧规则。

## 9. F12：环境、灾害与韧性

### 9.1 环境

- 能源、交通、工业和垃圾产生排放事实。
- 空气、水和土壤质量按区域归约。
- 污染影响健康、生产率、地价和迁移，但使用周期滞后。

### 9.2 灾害

- 灾害概率由 seed、版本、时间和区域暴露确定。
- 事件造成设施、建筑和网络容量损失，损失通过明确 operation 记录。
- 应急预算、物资、人员和修复能力决定恢复速度。
- 同一灾害事件有稳定 ID，重试和重放不能重复造成损失或奖励。

### 9.3 韧性指标

记录服务恢复时间、未满足基本需求、财政冲击、受影响人口和重建成本。指标仅由事实派生。

## 10. F13：场景、校准与产品运行层

### 10.1 场景参数包

- 创世人口、资源、土地、设施和行业配置进入签名参数包。
- 参数包不可原地修改；新版本需有变更说明和基准结果。
- 世界绑定参数包后，规范状态记录代码和版本。

### 10.2 校准与验证

- 建立小型、均衡、资源短缺、住房危机、人口老龄化等黄金场景。
- 每个场景运行 1 年、10 年和必要的压力周期。
- 输出守恒、稳定性、敏感性和性能报告。
- 校准目标是区间和趋势，不伪造精确预测。

### 10.3 自动调度

- F7.8 已实现到期世界的数据库持久 lease、崩溃接管、指数退避和应用生命周期清理；同世界仍由 advisory lock 保证单写入者。
- F13 在此基础上继续细分可重试基础设施错误、确定性不变量错误和人工暂停，并增加管理员健康视图。
- 追赶有单次 tick 上限和时间预算，不能让一个世界饿死其他世界。
- 失败世界保留最后可信 tick，不跳过失败 tick。

### 10.4 查询与实时更新

- 当前状态走投影 API，历史指标走稳定游标/时间窗口。
- SSE/WebSocket 只推送已提交 tick 摘要，不参与状态改变。
- 图表降采样是查询层行为，原始事实和周期指标保留可审计来源。

### 10.5 平台奖励

游戏奖励必须使用：

```text
已提交城市事实
  → 唯一 city_reward_event
  → 同事务 city_outbox
  → 签名平台虚拟货币集成接口
  → 幂等 wallet ledger
  → 回执/对账
```

- 奖励规则使用里程碑、公共目标和赛季结果，不按股票利润、交易量或在线点击直接兑换。
- 每规则、用户、世界、周期有唯一键和额度上限。
- 重放、恢复和重复消息不能重复发奖。
- 城市内部货币永不直接映射为平台钱包余额。

## 11. 可选经济分支的准入门槛

### E1：银行、利率与信贷

至少满足：

- F10 多企业和家庭现金流长期稳定。
- 企业损益、资产负债、破产与清算已闭环。
- F5 可重放贷款前所依赖的全部主体状态。
- 10 年黄金场景无余额、资源或人口守恒失败。

届时新增银行主体、存款负债、贷款资产、合同、还款计划、逾期、拨备和准备金。利率只能作为政策或合同参数参与未来 journal，不能直接乘余额后写回一个汇总字段。

### E2：证券与股票

至少满足：

- F10 企业基本面真实来自总账和经营事实。
- 公司治理、股份登记和破产处置有稳定身份。
- 订单资金/证券预留可以在同事务守恒。
- 集合竞价有确定性、并发和回放证明。

股票价格由订单和基本面形成，不使用随机 K 线。首版不做杠杆、做空、衍生品或法币兑换。

## 12. 执行顺序与停止条件

### 第一批：F6.1（已完成）

1. 已完成日历、人口政策、population movement/line 和 cohort 投影迁移。
2. 已完成创世初始化、F5 世界升级基线和同 tick 多版本快照保留。
3. 已完成日/月/季度/年边界与月度自然变化。
4. 已完成人口事实不可变保护、延迟守恒检查和投影写闸门。
5. 已扩展规范状态、snapshot、replay reducer 和 recovery mapper。
6. 已增加成员读取 API、稳定游标和详情。
7. 已通过时区/DST/闰年/季度边界、10 年日历、120 个月人口守恒、确定性、重放和恢复测试。

### 第二批：F6.2–F6.3（已完成）

已在 F6.1 长周期门禁后加入外部迁移、区域迁移、家庭数量变化和收入带迁移，并完成版本升级、事实重放、恢复和并发幂等测试。

### 第三批：F7–F9（进行中）

F7.0 坐标与规则集至 F7.11 movement intent/行动预算/拥堵预留、V7 服务可达性与队列、V8 下一 tick 跨域 effect、V9 设施—区域—换乘的容量路线底座、V10 对 V5 局部空间的受验证到达桥接、V11 NPC work-facility 自动 OD source/闭合周期指标、V12 容量受限住宅—就业绑定、V13 双向设施在场域 commute source、V14 assignment epoch lifecycle、V15 F10.0 企业订单/库存 ownership/保留/交付/reversal、V16 enterprise freight source、V17 custody/receipt gate、V18 overflow batch freight/full-receipt gate、V19 F9.3.0 的静态 spatial node/corridor 身份层、V20 F9.3.1 的通用 asset state/capacity lifecycle、V21 F9.3.2 的 future-allocation effective-capacity admission、V22 F10.3.0 的按行 partial receipt、货损/拒收、即时退款和承运责任 claim、V23 F10.3.1a 的准备金拨备/全额 carrier claim 追偿，以及 V24 F10.3.1b 的唯一服务合同与延后一 tick 的现金运费结算均已完成。V21 不追溯重写历史 route/reservation，V22 不回填 upgrade tick 前的 custody，V20 命令在 T 提交后仅从 T+1 自动调度可见，V24 不向升级前 case 补费且不以余额不足伪造债务；容量封闭时才会在确定性排序内选择替代路径。下一步仅能增加报价输入、服务等级/SLA、保险、独立在途库存与多企业生产；不能绕开现有空间身份、生命周期、服务、effect、mobility、OD fact、V12–V24 evidence 建立旁路汇总。

截至 V24，以上段落中的 F10.3.1a/1b 已完成：系统承运 actor 与 `system_freight_reserve` 经济 firm 分离，政府只能经审计 funding 注入准备金，V22 的 `carrier` claim 仅能在资金充足时全额、一对一追偿并受控推进至 `resolved`；V24 再为 baseline 后的 V22 settled case 封存唯一 service contract，并在后续 automatic tick 以固定单位费率写入真实 seller-to-carrier cash journal。历史 V22 receipt/refund/claim、V23 recovery、历史 route 和升级前 case 均不被重写或补费。SLA、保险和在途库存仍须作为后继版本。

### 第四批：F10–F13

多企业供应链稳定后，再完成家庭福利、环境灾害和产品运行层。自动奖励只在事实、Outbox 和对账都具备后开放。

### 阶段停止条件

出现以下任一情况必须停止向上扩展并修复基础：

- 相同输入不能得到相同哈希。
- 新状态无法由不可变事实重放。
- 恢复需要跳过约束或直接修改历史。
- 守恒只能靠定期“对齐数字”修正。
- 长周期出现整数溢出、负人口、负库存、无界价格或性能失控。
- 规则依赖部署墙钟、无版本外部服务或数据库无序结果。

## 13. 测试与性能基线

每阶段至少包含：

- 迁移结构和数据库触发器测试。
- 纯函数边界/舍入/溢出单元测试。
- 服务层幂等、权限和错误语义测试。
- 单世界事务闭环集成测试。
- 多世界并发隔离测试。
- 同 seed 跨数据库身份确定性测试。
- 投影漂移、重放差异和恢复失败测试。
- 1 年快速回归、10 年黄金场景和压力参数场景。

性能报告记录每 tick 查询数、写入行数、快照尺寸、重放 tick/s、内存峰值和不同世界并行吞吐。未测量前不承诺具体 VPS 容量。

## 14. 已完成切片与下一切片

F6.1–F6.3 已完成以下完整垂直链：

```text
迁移与约束
  → 创世投影
  → tick 边界与 movement
  → 状态 API
  → 规范哈希
  → 快照
  → 逐 tick 重放
  → 当前投影恢复
  → 10 年黄金测试
```

该链已全部通过。F7.0 另外完成了不改变世界状态的坐标/规则基础：32×32 Chunk、负坐标下取整、离散 Z、严格规则加载、无环 `looks_like`、固定规则哈希和认证读取 API。F7.1 随后发布 `city-f7-v1`，完成世界规则/生成器绑定、81 Tile Overmap、按需 Chunk Mapgen、不可变 mutation，以及规范哈希、快照、重放、恢复和空间读取 API。

F7.2 已完成 Vue 空间状态、视口 Chunk 缓存、glyph atlas renderer、坐标/Z 导航、检查器和文本导出，并只消费 F7.1 的真实 Overmap/Chunk API。F7.3 随后发布 `city-f7-v2`，完成地块、分区、建筑、单位池、住房分配和 portal 基线；F7.4 发布 `city-f7-v3`，完成开发项目状态机、材料/资本品结算、施工进度、building adjustment、升级、重放、恢复和前端操作闭环。

F7.5 已发布 `city-f7-v4`：firm state 已落到商业/工业 unit pool，并完成有效占用、场所事实、库存守恒迁移、规范哈希、快照、重放、恢复和 CLASSIC 叠层。

**F7.6 开放世界模拟运行时底座已发布 `city-f7-v5`**：版本化定义目录、通用 Actor、属性、Role、Status、Requirement、Rule、Case、Fact 与 Effect 协议已经落地；角色创建、属性成长、职业转换、规则处罚、状态到期、稳定案件分页、规范哈希、重放恢复和角色工作台形成首批纵向闭环。底层没有把 Role 固化成职业、把 Rule 固化成法律或把 Effect 固化成城市处罚。其后续空间切片已由 F7.7 在新 canonical 版本完成；F8 公共设施及任何金融分支必须消费该协议，不得再建旁路投影。

**F7.7 Actor 空间与控制权已发布 `city-f7-v6`**：每个 Actor 拥有权威 XYZ/Chunk/Local 位置、管辖区与可选 Chunk/Building/Site 锚点；八方向相邻移动和 Portal 跨层移动通过 Fact/Effect 封账；`actor.command` 与 `actor.control.manage` 支持委托、撤销和提交/执行双重授权；world、organization、jurisdiction、location scope 已接入规则判定；Location/Grant 已进入 canonical、snapshot、replay、recovery 和 CLASSIC `@`/`&` 可视化。

**F7.8 成员、调度与命令回执已完成**：owner 可按精确邮箱/用户名维护职责成员；成员离开前会检查 active Actor grant；自动调度使用数据库持久 lease、崩溃接管、指数退避、确定性 idempotency key、expected tick 和原 Step advisory lock；paused 世界仅在有 pending command 时推进；命令列表/详情对非 owner 只暴露本人命令；前端提供成员 roster、可搜索委托 Select、局部 mutation 和无整页刷新的终态回执。该运行闭环不改变 `city-f7-v6` canonical 字节；其后的 F7.9 已在新版本完成通行/占用闭环，没有跳到金融行情。

**F7.9 Actor 导航已发布 `city-f7-v7`**：规则集 passability/整数 movement cost、已生成 Chunk、Furniture、Building edge、Entrance、Stair、Actor Occupancy 和对角防切角已进入统一解析器；路径查询在 repeatable-read 只读快照中执行有界确定性 A*，响应携带 world tick 与规则 hash；实际移动在 Tick 事务中重新校验，拒绝不会污染 Actor 位置；前端完成地图目标选择、route overlay、过期响应抑制和逐步执行。动态 Door/Portal 状态、访问 credential 和群体移动预留留给 F7.10，不在本版本伪造。

**F7.10 动态 Portal 与访问控制已发布 `city-f7-v8`**：每个 active Portal 拥有唯一、版本化、Fact-backed 的 open/closed/locked 状态；访问策略复用通用 Requirement 树并绑定 canonical SHA-256；状态动作验证 Actor 控制权、交互距离和当前策略，策略配置仅允许 owner；路径查询与 Tick 内移动统一阻断关闭、上锁和授权失败；Portal state 已进入 canonical、snapshot、replay、recovery，前端已完成 Portal 图谱、访问诊断、状态动作和结构化策略编辑。

**F7.11 持续移动与拥堵底座已发布 `city-f7-v9`**：每个 Actor 最多一个版本化 movement intent；整数预算按 Tick 归集并封顶；due intent 通过 priority、aging、阻塞次数、创建 Tick 和 ActorCode 稳定排序；每步从当前 Terrain、Portal、授权和 Occupancy 重新执行有界 A*；同 Tick Cell/Edge reservation、结构化等待/阻塞/到达/取消/失败 Fact、Location/Intent Effect、canonical、snapshot、replay 和 verified recovery 已闭环。前端读取真实 intent/reservation API，支持地图目标、设置/替换/取消、预算/原因/版本诊断，并通过请求代次抑制过期刷新且不卸载地图。下一切片是 F8 公共设施通用事实协议，而不是直接写某一种设施的不可扩展汇总字段。

**F8.0 通用公共服务底座已发布 `city-f8-v1`**：八类服务和九类设施类型使用版本化目录；设施绑定真实建筑，需求绑定真实城市主体，连接持有容量、损耗和偏好；running Tick 对共享容量执行稳定整数分配并发布不可变 allocation/settlement。设施状态、有效派发容量、事实链、Profile 计数、规范状态、snapshot、replay、verified recovery、稳定游标 API 和 Vue 操作工作台均已闭环。直接创建 F8 与 `F7.11 → F8.0` 显式升级均通过真实 PostgreSQL 测试。下一切片为 F8.1 设施建设、维护、故障、人员、预算和资源守恒，不修改 F8.0 的历史结算语义。

**F8.1 设施生命周期已发布 `city-f8-v2`**：新设施从 `uncommissioned` 出发，投运、维护、维修和退役均经版本化 operation 与不可变 lifecycle fact；人员覆盖、状况、工程和故障共同门控派发容量。工程启动把 F3 基础材料/资本品消耗、F2 journal、政府预算 movement 和生命周期投影绑定到同一 draft fact，数据库同时校验命令来源、资源声明、预算前后值、劳动预留和事实序列。`PRE_SERVICE → SERVICE_SETTLEMENT → POST_SERVICE` 的阶段顺序、规范状态、从 genesis replay、verified recovery、稳定游标 API 与授权查询均已闭环；真实 PostgreSQL 测试覆盖新建、容量、人员、投运、磨损、查询分页、越权、重放、恢复和不可变保护。

**F8.2 通用物理网络已发布 `city-f8-v3`**：网络、节点、边、策略和事实使用版本化复合身份；供给/需求端点绑定真实 F8.0 容量与需求；确定性整数残量图支持有向/双向共享容量、多路径、逐段损耗、稳定成本和 path hash；网络 batch/path/segment 与服务 allocation/settlement 同事务封账。拓扑、流量和投影已进入 canonical、snapshot、genesis replay、verified recovery；数据库拒绝跨服务绑定、超容量分段、错误损耗、非法状态链和历史篡改。授权查询提供目录、稳定游标投影、流量/事实以及连通分量、孤岛、瓶颈、饱和边和只读路由探针；Vue 工作台不卸载旧投影即可筛选、刷新和执行 CAS 操作。真实 PostgreSQL 测试覆盖 F8.1→F8.2 兼容升级、基线结算、诊断、回放与恢复。当前按要求停止，下一恢复点为 F8.3。
