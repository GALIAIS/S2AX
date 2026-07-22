# V2–V24 开放世界城市模拟完成记录

本文档记录 V2–V24 开放世界城市模拟从当前实现到可运行基础闭环的实际完成项、验证命令与已知边界。

> 状态：已更新至 V24。本文仅记录已落地并验证的行为。

## 已完成：Region 物化与不可变校验

- 新建城市默认绑定 `city-openworld-v24`；旧 `city-openworld-v1` 至 V23 与 F7/F8 历史事实不被重写。
- V2 使用 `1.4.0` Region 计划和地面室内契约；V3 使用 `1.5.0`，把全楼层室内和入口/楼梯拓扑纳入 Sector 内容哈希。
- Region 为 32×32 Chunk，Sector 为 8×8 Chunk；扩张经 `open_world.sector.materialize` 命令和城市 Tick 原子封存。
- 后端会验证 Region Plan、Sector Surface、Chunk 负载、建筑所有权、每层室内与 Portal 拓扑；错误只报告不变量，不会自动覆写事实。
- 提供全世界（≤256 Sector）和单 Region 的只读验证接口；前端开放世界面板可验证当前 Region，不会刷新整个页面或生成额外数据。
- V6 在 Genesis snapshot 前封存七项版本向量：引擎、场景、经济政策、空间 profile、worldgen plan、运行时内容目录和规则包。运行中的 world 不会因部署而静默采用新的默认配置；V5 可在暂停且审计的升级事务内生成 V6 基线。

## 已完成：V7–V24 服务、effect、mobility、跨尺度到达、自动 OD、通勤生命周期、供应链、空间交通身份、承运追偿与运费结算层

- V7 把开放世界设施锚点接入版本化服务目录、服务提供者、可达性、有限队列和 fact-backed response；V8 将 response 以单独 catalog 变成下一 tick 才应用的跨域 effect，避免同 tick 回路。
- V9 在 genesis 或 V8→V9 暂停升级基线封存 `walk`、`transit`、`freight` 模式以及设施/区域/中央换乘 hub 图；静态模式、hub 和 edge 都有独立 hash，数据库禁止运行中修改。
- `open_world.actor.mobility.request` 只写需求事实；后续 tick 才做稳定最短路径与容量预约，产生 route、allocation、完成/过期事实和 actor metric。路线完成明确不改变 Actor 局部坐标。
- V9 state 已接入 canonical snapshot、replay、recovery、升级版本向量和只读 API；恢复时 route/demand 的双向引用在同一事务内延迟校验，确保投影可清空重建且不削弱外键约束。
- V10 仅为新接受的 demand 捕获 V5 起点；完成 route 必须跨越一个 tick 才能注册 arrival。桥接会复用绑定的 V2/V3 worldgen 与 V5 可通行/占用校验，记录 pending、blocked、landed 或 failed 事实，并在成功时通过位置 effect 落到确定性 surface Cell。
- V9→V10 暂停升级只封存 arrival profile 基线，绝不为旧 V9 demand 或已完成 route 伪造 origin、arrival 或坐标；arrival state 已接入 canonical snapshot、replay、recovery、升级向量与只读 API。
- V11 从非 dormant V5 NPC 的 `work_facility` 封存首个 `npc.assigned_facility_visit` source adapter。source 到期时取 Actor 当时真实局部位置作为 origin，以封存的 Facility hub 为 destination；不会伪造 home、household 或 enterprise order。
- V11 在当前 tick 写 `system.mobility.od.generated`/`system.mobility.od.suppressed` 和根于其下的 V9 request fact，V9 仅在后续 tick 预约路径容量，V10 更后续才尝试本地落点。每 24 tick 以 `system.mobility.od.cycle.closed` 封存 source 结果与全网络发生窗口指标，已经关闭的窗口不会被未来完成的路线回写。
- V10→V11 暂停升级仅建立未来 automatic source 基线，不重分类历史 demand、route 或 arrival；OD profile、source、metric 已接入 canonical snapshot、replay、recovery、升级版本向量与只读 `open-world/mobility/od` API。
- V12 将 V5 NPC 的 residence/employment 输入固化为容量受限的 `npc.residence_employment` binding：优先使用有效 V5 home，否则在真实 residence Facility 中确定性分配；无容量时显式计入 unbound，不伪造住宅。
- V12 不创建第二条自动需求，不修改 V11 source 或历史交通证据。profile/binding 已接入 canonical snapshot、replay、recovery、V11→V12 暂停升级、版本向量和只读 `open-world/commutes` API；未来双向 source 必须重新验证当前 building origin。
- V13 为每个 V12 binding 封存 `npc.residence_to_work`/`npc.work_to_residence` source pair；source 每次 due 都重验 Actor 的当前 Facility Presence Domain、role、facility、navigation 与 mobility 冲突，失败会留下 suppression fact 而不是传送。V13 source、方向周期指标、replay/recovery 与只读 `open-world/commute-sources` API 均独立于 V11 历史。
- V14 在 V12/V13 不可变证据之上建立 append-only assignment epoch lifecycle。管理员 rebind 只能以 supersede 旧 epoch、创建 successor epoch/source pair 的方式变更有效 assignment；自动状态机只依据 actor、role、设施和 profile 事实追加 suspend/resume/terminate transition。epoch/source/transition/cycle metric 已进入 canonical、snapshot、replay、recovery、V13→V14 paused upgrade 和只读 `open-world/commute-lifecycle` API，并由 SQL deferred assertion 阻止 opening fact、epoch 连续性、source cadence 或周期窗口被破坏。
- V15 完成 F10.0：两个冻结的 firm/facility/district supply-chain node、订单/行/transition/fact、库存 reservation/release、acceptance purchase journal、dispatch、atomic delivery 与 terminal reversal 都被追加式事实保护。交付只允许由匹配的 V15 `order.delivered` fact 驱动 F3 transfer；失败、取消和自动过期不会留下锁定库存或不可逆付款。
- V15 新世界在 genesis 预建各 node 的 storable inventory topology；早期 V15 world 的 replay 会依据已存在订单事实补回历史惰性零余额，保证不修改数量也不破坏 snapshot 可重放性。V15 profile、投影和事实被 session write gate/数据库 assertion 保护，并接入 V14→V15 paused upgrade、canonical、replay、verified recovery 与只读 `open-world/supply-chain` API。
- V15 普通成员 API 只返回自有 firm 参与的合同及相邻 node，计数按可见集合重算且不暴露全局 revision；world owner/system administrator 才读取完整经营视图。
- V16 仅将 V15 `dispatched` 证据适配为 V9 企业货运 source；V17 将路线完成收紧为 custody 的 `awaiting_receipt` 与显式 receipt gate；V18 仅对 V16 overflow source 进行确定性 32-unit 拆批，并要求全部 consignment 到达后才提交单一 V15 原子交付。
- V19/F9.3.0 将每个冻结 V9 hub/edge 一对一映射为 static spatial node/corridor，并封存默认、日本都市、中国都市的 transport style catalog。该层进入 canonical snapshot、version vector、replay、recovery 和成员只读 `open-world/spatial-network` API；V18→V19 paused upgrade 只映射既有拓扑，不补造 demand、route、库存、账本或交付事实。
- V20/F9.3.1 将每个 V19 node/corridor 确定性映射为 immutable `network_node`/`corridor_segment` asset，并以 current state + append-only transition 实现 `operational`、`restricted`、`maintenance`、`closed`、`construction` 生命周期。每个非 baseline transition 必须匹配同 tick runtime fact 的 asset、状态、容量、理由和 `v9_scheduler_effect=none`；SQL guard、canonical snapshot、静态/事实分层 replay、recovery、成员只读 `open-world/infrastructure` API 和 V19→V20 paused upgrade 均已完成。V20 不回写 V9 route、capacity、货运、库存或账本历史。
- 已修正 JSONB 键排序不能参与拓扑 hash 的问题：新世界使用语义 checksum，早期短暂生成的原始 metadata checksum 仍可读取和恢复。

- V21/F9.3.2 将 V20 asset 的 effective capacity 接入未来 V9 allocation admission；每条 allocation 封存其实际消耗的容量、状态 transition 与替代路径决定，历史 route/reservation 不回写。
- V22/F10.3.0 为 post-baseline V17 shipment/V18 consignment 建立按行 receipt、accepted/lost/rejected、即时库存/退款与 carrier claim；零 receipt 的 V15 failed 路径以 voided order/case 闭合，所有新状态均进入 snapshot、replay、recovery 与成员 scoped API。
- V23/F10.3.1a 将 V16 的无现金 carrier actor 与独立 `system_freight_reserve` firm 分离。政府只能通过人工 reserve funding 注入资金；每条 V22 open carrier claim 仅能在 reserve 充足时进行全额、一对一的卖方追偿，按受控 `open → resolved` 投影闭合。V23 同时覆盖 version vector、snapshot、replay、verified recovery、V22→V23 paused upgrade、SQL assertion 和成员 scoped API。
- V24/F10.3.1b 对 baseline 后的 V22 settlement case 封存唯一 carrier service contract；case settled 且跨越一个 automatic tick 后，才以固定单位费率将卖方现金结算至 `system_freight_reserve`。余额不足不会透支、创建 payment/journal 或伪造应收，后续 tick 仅按原 quote 重试。V24 覆盖 version vector、snapshot、replay、verified recovery、V23→V24 paused upgrade、SQL assertion 和 seller-scoped API。

详细契约见 [《开放世界生成器 V2 / V3 实现契约》](CITY_OPENWORLD_GENERATOR_V2_CN.md)。

## 已验证

```text
go test ./internal/service ./internal/handler ./internal/server/routes ./internal/cityspatial ./migrations
go test ./internal/service -run '^TestCityOpenWorld(MobilityOD|V9Mobility|MobilityArrival|Commute)' -count=1
go test ./internal/service -run 'Test(NormalizeCityOpenWorldSupplyChain|CityOpenWorldSupplyChainStaticCheckpoint|ReplayCityOpenWorldSupplyChainInventoryTopology|ProjectCityOpenWorldSupplyChainStateForOwnedFirms)' -count=1
go test -tags=integration ./internal/repository -run '^(TestCityOpenWorldV(9|10|11|12|13|14|18UpgradeToV19|19SpatialNetwork|20Infrastructure|21EffectiveCapacity|22FreightSettlement|23CarrierRecovery|24CarrierCommerce)|TestCityOpenWorldV15SupplyChain)' -count=1
pnpm typecheck
pnpm exec vitest run src/api/__tests__/citySpatial.openWorld.spec.ts src/features/city-spatial/__tests__/CityOpenWorldWorkspace.spec.ts
```

## 当前边界

- V9–V24 仍是设施—区域—换乘层的聚合交通、最小企业订单合同、空间交通身份/资产状态、人工承运追偿与固定单位运费现金结算层，不是车道、时刻表或个体车辆仿真；V21 只让未来 V9 allocation 消费有效容量，V24 不实现动态报价、SLA、保险或在途库存。
- V10 的最低桥接只覆盖 V9 完成后、V5 surface 锚点的受验证落点；V19 node/corridor 和 V20 asset 都不会把它升级为车道、站台、Portal 级连续行程。
- 当前 Profile 仅覆盖日本/中国都市的基础道路、地块和建筑目录，尚不涵盖完整现实交通网络、地下设施或真实地理数据适配器。
- 大世界必须按 Region 进行运维校验；全世界规范哈希校验有 256 Sector 保护上限。
