# F9.2 版本化 OD Source 与交通周期指标设计

版本：v1.0（2026-07-20）

状态：V11 已实现首个 `npc.assigned_facility_visit` source adapter；本文件同时冻结后续 household、enterprise 等 adapter 的接入门槛。

## 1. 目标与非目标

F9.2 的目标不是把 NPC 直接传送到“工作地点”，也不是立即实现车道、车辆或真实地理路网。它是在既有 V9 聚合 mobility 与 V10 跨尺度 arrival 之间增加一个可演进的需求入口：由版本化、可审计的世界事实自动提出 origin/destination（OD）需求，并将交通结果以关闭后的周期指标暴露给后续经济域。

本切片必须保证：

- 自动 source 不会绕开 V9 的容量、路径、等待和过期状态机；
- 自动 source 不会绕开 V10 的 origin guard、Sector 物化、可通行/占用校验或 landing fact；
- 同一 tick 新生成的 demand 不会在同 tick 被调度；
- source、metrics、fact 和版本向量均进入 canonical snapshot，并可 replay/recovery；
- 新 source kind 只能在其上游身份事实已经权威化后接入，不能用启发式字符串或 UI 状态补造数据。

非目标：家庭居住地建模、雇佣关系、企业订单、货物库存、车辆实体、路线时刻表、实时外部地图数据和平台奖励。这些需要各自的版本化事实与独立验收。

## 2. 版本与责任归属

```text
V5 NPC profile / validated local location
        │
        │ source adapter reads sealed identifiers only
        ▼
V11 OD source ──generated/suppressed fact──► V9 mobility demand
                                                   │
                                                   ▼
                                       V9 route + capacity allocation
                                                   │
                                                   ▼
                                      V10 arrival bridge / local landing
                                                   │
                                                   ▼
                                     future labor / enterprise / market input
```

| 版本 | 拥有的真相 | V11 不得改写 |
| --- | --- | --- |
| V5 | Actor、NPC profile、设施、已验证局部位置、navigation intent | Actor location、NPC 行为或设施身份 |
| V9 | demand、route、allocation、等待/完成/过期和容量 | 既有调度算法、静态 hub/edge 图 |
| V10 | completed route 的受验证局部 arrival | 历史 route、已发生位置事实 |
| V11 | automatic OD source、source transition、closed cycle metric | V5/V9/V10 的状态归属 |

V11 的 profile ID 为 `sub2api-open-world-mobility-od`，版本为 `1.0.0`。V11 content catalog 进入版本向量，旧 world 只有通过暂停的 `V10 → V11` 升级才会获得 source；升级不会重写历史 trip。

## 3. 持久化模型

### 3.1 `city_open_world_mobility_od_profiles`

每个 V11 world 仅一行，封存：

- generation contract：`versioned_source_od_adapter_v1`；
- metric contract：`next_cycle_mobility_metrics_v1`；
- baseline tick、24-tick cycle、单 tick 最大 source 处理数；
- source / generated / suppressed / metric 的审计计数器；
- hash、revision 与 object metadata。

它不是全局配置表。改变 cadence、计量语义或 source 基线应发布新的 engine/profile 版本，不得原地修改。

### 3.2 `city_open_world_mobility_od_sources`

每个 source 绑定一个 Actor 和一个已封存的 destination facility/hub。V11 只允许：

```text
source_kind   = npc.assigned_facility_visit
mode_code     = walk
purpose_code  = routine.facility_visit
period_ticks  = 24
status        = active
```

source 的 `phase_offset` 把现有 V5 `schedule_offset` 映射到 24 tick 周期；`next_due_tick`、`last_transition_tick`、transition fact、generated/suppressed counter 和 version 共同构成不可跳步的生命周期。创建时下一到期 tick 必为 `baseline + 1 + phase`；每次 transition 后必为 `last_transition + period`。

### 3.3 `city_open_world_mobility_od_cycle_metrics`

每个窗口一行，主键为 `(world_id, cycle_start_tick)`。第一个窗口为 `[baseline+1, baseline+24]`，在 tick `baseline+25` 关闭。它记录：

- 本窗口 V11 source 的 generated/suppressed 事实数；
- 全网络在窗口内发生的 request/schedule/complete/expire；
- 关闭时 pending demand 快照；
- 全网络 arrival landed/blocked/failed、route travel/congestion 累计和 allocation peak occupancy；
- source fact 和 metadata。

`network_*` 是整个 V9/V10 mobility network 的发生窗口指标，`generated/suppressed` 是 V11 source 范围指标。两种 scope 必须在 API/面板中明显标示，不能把某一 source 的流量误报为整个城市交通量。

## 4. Tick 时序与事实链

```text
tick T
  1. V11 读取 due source、Actor location、navigation/mobility conflict
  2. 生成：system.mobility.od.generated
       └─ mobility.requested（child fact） ──► V9 pending demand
     或抑制：system.mobility.od.suppressed
  3. V9 只选择 requested_tick < T 的 demand
  4. V10 只消费 completion_fact.tick < T 的 route

tick T+1
  V9 才可能调度 T 的 automatic demand

closed tick C
  关闭 [C-24, C-1] 的 metric，之后才处理 C 的 due sources
```

每个 source transition 与 metric row 必有同 tick draft runtime fact；投影、计数器和 effect（若有）写完后才 post fact。这样数据库 trigger 既能验证事实引用，又不会允许未投影的事实被提前封存。

## 5. V11 adapter 的判定规则

### 5.1 成功生成

当 source 到期且以下条件全部成立时：

1. Actor 仍 active，位置完整、actor code 匹配并满足 V10 location validity；
2. 没有 active V5 navigation intent；
3. 没有 pending/scheduled V9 mobility demand；
4. `walk` mode 与 destination facility hub 仍可读取，且 facility/hub 绑定一致；
5. 可由当前局部坐标解析出 source hub，且 source/destination 不相同；

V11 写 `system.mobility.od.generated`、其子 `mobility.requested`、V9 demand、V9 actor metric 和 source transition。demand metadata 必含 `od_source_code` 与 V10 `arrival_bridge.expected_origin`，所以 arrival 不会覆盖 Actor 在路线期间的新位置。

### 5.2 抑制

无法安全生成时写一个 `system.mobility.od.suppressed`，再推进 source 周期；首版原因包括：

- `actor_location_invalid`
- `navigation_intent_active`
- `mobility_demand_active`
- `mode_unavailable`
- `destination_unavailable`
- `origin_unavailable`

抑制不是失败 route，也不是删除 source。它表示该周期明确没有产生需求，避免 source 在每个 tick 重复扫描同一个已经失效的到期记录。

## 6. 后续 source kind 的准入门槛

### 6.1 Household commute（未实现）

只能在以下权威事实都存在后新增，例如 `household.residence_to_employment`：

- household 与成员身份；
- 住宅 facility/parcel 与其可用性；
- employment relation、岗位 facility、工作状态与排班；
- 迁居、失业、停业后 source 的可审计停用或 rebind 策略；
- 迁移/升级时旧 residence 的历史语义。

不能从“NPC 当前所在位置”或 `work_facility` 推断居住地。

### 6.2 Enterprise freight（未实现）

只能在 `enterprise.order_to_delivery` 中封存：

- 订单、货物品类、数量、卖方/买方 inventory ownership；
- source/destination facility、装卸容量、货运 mode；
- 预约、超期、部分交付、取消和库存守恒；
- V9 route 完成与 F10 交付 journal 的下一 tick 结算边界。

不能因为 route completed 就直接增减库存或现金。

### 6.3 Emergency / visitor / event source（未实现）

每一种 source 都必须定义：稳定 code、版本、baseline、trigger fact、destination identity、suppress reason、cadence、最大处理量、隐私可见性、replay/recovery 规则和对 V9/V10 metadata 的兼容策略。若语义不同，发布新的 profile/engine version；不得用 metadata 随意改变已封存 source 的含义。

## 7. API、隐私与可视化

只读接口：

```text
GET /api/v1/city/worlds/:world_id/open-world/mobility/od
```

返回 profile、调用者可见 Actor 的 source，以及全局 closed metrics。世界 owner/具备完整读取授权者可见所有 source；普通成员仅见其可见 Actor 的 source。metric 不暴露 source 的私密位置、未脱敏的个人意图或内部 fact surrogate ID。

面板应按以下方式呈现：

- source：下一次 due、上次 transition、generated/suppressed、最后原因；
- closed metric：明确窗口起止与“全网络”标签；
- route：从既有 V9 详情链接到 demand/route/arrival，不复制或篡改其状态；
- 缺失数据：显示尚未关闭窗口，而不是把 0 当作已完成统计。

## 8. 不变量、升级与恢复

- profile/source/metric 表由 trigger 守卫，只允许 bootstrap、runtime fact 或 recovery context 写入；
- source identity、destination、mode、purpose、cadence 与 metadata 不可在运行中改写；
- generated/suppressed 和 source version 只能各次加一；profile counter 必等于 source/metric 行的可验证汇总；
- cycle metric 必引用对应 close tick 的 `system.mobility.od.cycle.closed` fact；
- canonical state 缺少 V11 OD state、V11 前版本携带 OD state、或 version vector catalog 不匹配时，snapshot/replay/recovery 必失败；
- V10→V11 baseline 只读取当时 V5 NPC 的有效 work facility，不 retroactively 给旧 demand 加 `od_source_code` 或 origin。

## 9. 验收与后续工作

V11 验收至少覆盖：

1. 新世界 source baseline 与 V10→V11 upgrade baseline；
2. source 在 T 生成 demand、V9 在 T+1 调度、V10 在更后续 tick landing；
3. source transition 的 generated/suppressed fact 与计数器；
4. 24 tick 窗口 close、metric scope、重复关闭拒绝；
5. snapshot、replay、recovery 后 OD state 字节语义一致；
6. 数据库 migration、FK、guard trigger 与 API route 注册。

下一实现切片是 household/enterprise 上游事实设计，而非直接增加更多 source 字符串。只有这些事实完成并有独立升级/恢复测试后，才可以进入 F9.2.B；F9.3 多模式站点网络仍必须由 worldgen/style version vector 提供。
