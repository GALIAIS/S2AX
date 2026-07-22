# F9.2.B.1 / V13 双向通勤 Source 设计

版本：v1.1（2026-07-20）

状态：`city-openworld-v13` 已实现并通过新世界、V12→V13 升级、V11/V12 回归、replay/recovery 验证。本切片把 V12 的住宅—就业绑定变成真实、双向、可回放的 V9 mobility demand；它不重写 V5 社会投影、V9 路线、V10 arrival 或 V11 历史 source。

## 1. 先解决的设计缺口

V12 正确地冻结了 home/work Facility、hub、employment role 和日程相位，但它不拥有 Actor 的实时位置。直接要求“Actor 必须在 building interior”会让系统无法启动：当前 V5 genesis NPC 在其 work Facility 的入口附近 surface cell 生成，V10 也会在目标 hub 锚点附近选择可通行 surface landing，而不是凭空把角色写进室内。

这不是允许任意“同一区域即在设施”的理由。V13 定义严格的 **Facility Presence Domain（设施在场域）**：

1. **室内在场**：`space_kind=interior`，且 `building_code` 精确等于该 Facility 的静态 building；
2. **入口在场**：`space_kind=surface`、`location_scope=surface`、`z=0`，并且到该 Facility 静态入口锚点的 Chebyshev 距离不大于冻结的 `surface_egress_radius`；
3. 位置必须先通过 V10 完整 location validity 与当前 world 的 passability/occupancy 规则，不接受仅凭 sector、hub code、建筑名或客户端字段的推断。

入口在场表示“已在该建筑可验证的出入口/集散域”，不是“在室内”。V9 的聚合 hub 本身就是设施入口的跨尺度出行边界，因此这个契约既不传送角色，也不会把 surface 位置伪装为房间。V13 的首版半径固定为 `24`，与 V5 genesis 的最大入口附近生成半径一致，并覆盖 V10 的 12-cell 安全 landing 范围。改变该半径必须创建新的 profile/engine 版本。

## 2. 目标与边界

V13 必须做到：

- 为每个 V12 binding 建立两条独立、稳定 code 的 source：`npc.residence_to_work` 与 `npc.work_to_residence`；
- 每条 source 到期时重新验证实时 Actor、employment role、Facility、hub、冲突和 Facility Presence Domain；
- 成功时创建 V9-compatible demand，V9 仍在后续 tick 调度，V10 仍在更后续 tick 作受验证 landing；
- 失败时写明确的 suppression fact 并推进到下一周期，不忙循环、不传送、不选择替代 home/work；
- 为 V13 source 创建按方向的闭合周期指标、canonical state、version vector、replay、recovery、只读 API 与数据库 guard；
- 保留 V11 source 的历史投影，同时避免同一 NPC 同时生成 V11 generic work visit 和 V13 commute 的双重需求。

本切片明确不做：

- 室内连续步行、门禁流程、上下班打卡、工资结算、企业班表、车辆、站台或真实车道；
- residence/employment rebinding、迁居、离职、停业后的重新分配；
- household、订单、库存或货运。它们需要独立生命周期事实，不能借用 commute binding。

## 3. 版本、所有权与时序

```text
V5 actor / local location / facility / role
             │
V12 immutable residence-employment binding
             │
             ▼
V13 verified facility-presence source
  generated / suppressed fact
             │
             ▼
V9 demand → route / allocation → completed
             │
             ▼
V10 arrival → validated facility-entry surface landing
```

V13 新增 engine stage `open_world_commute_sources`，顺序固定为：

```text
V11 legacy OD compatibility pass
→ V13 commute source pass
→ V9 mobility scheduler
→ V10 arrival bridge
→ command application
→ V5 local navigation intents
```

V13 在 tick `T` 创建的 demand 的 `requested_tick=T`，因此 V9 只会在 `T+1` 或之后调度；V10 只处理更早 tick 的 completed route。这保持已有因果边界。

## 4. 静态 source 基线

### 4.1 Profile

`city_open_world_commute_source_profiles` 每个 V13 world 仅一行：

```text
profile_id                  = sub2api-open-world-commute-source
profile_version             = 1.0.0
generation_contract         = verified_facility_presence_od_v1
origin_contract             = facility_interior_or_surface_egress_v1
period_ticks                = 24
surface_egress_radius       = 24
maximum_generations_tick    = 128
```

它还保存 baseline tick、source/generated/suppressed/metric counters、content hash、revision 与 metadata。profile 是不可变目录；运行时只能由 fact-backed transition 增加计数。

### 4.2 Source

`city_open_world_commute_sources` 每个 active V12 binding 必有且仅有两行：

| 方向 | source kind | 期望起点 | 目的地 | phase | purpose |
| --- | --- | --- | --- | --- | --- |
| outbound | `npc.residence_to_work` | home Facility/hub | work Facility/hub | `binding.outbound_phase` | `routine.commute.outbound` |
| return | `npc.work_to_residence` | work Facility/hub | home Facility/hub | `binding.return_phase` | `routine.commute.return` |

每行持有 binding code、Actor、起点/目的 Facility+hub、mode、目的、周期、phase、下一到期 tick、最后 transition fact、generated/suppressed counter、version 与 metadata。起终点、身份、cadence 和 metadata 在 V13 内不可改写。

首次 due tick 是 `baseline_tick + 1 + phase`，每次成功或抑制后都是 `last_transition_tick + period_ticks`。起点和目的地相同、源/目的 hub 不属于相应 Facility、或一个 binding 缺任意方向 source 都是 foundation failure。

## 5. 实时准入与抑制理由

source 到期时按照以下顺序验证。每一个失败都只写一条 V13 suppression fact 并推进 source。

1. Actor 仍为 `active`；
2. V12 binding 引用的 employment role 当前仍存在、类别为 `employment` 且 `active`；
3. 起点与目的 Facility 均仍为 `active`，Facility type 与 V12 identity 一致，facility hub 仍精确绑定；
4. Actor 当前 location 满足 V10 validity；
5. 当前 location 属于 source 的 expected-origin Facility Presence Domain；
6. 没有 active V5 navigation intent；
7. 没有 pending/scheduled V9 mobility demand；
8. `walk` mode 可用，起点和目的 hub 都可解析且不同。

首版标准 reason code：

```text
actor_inactive
employment_role_inactive
origin_facility_unavailable
destination_facility_unavailable
actor_location_invalid
expected_origin_unavailable
navigation_intent_active
mobility_demand_active
mode_unavailable
origin_hub_unavailable
destination_hub_unavailable
```

`expected_origin_unavailable` 的 fact payload 包含 expected Facility、origin contract、当前位置的最小审计摘要和方向，但不会暴露其他 Actor 的位置或内部 surrogate ID。它是一个受观察的未满足通勤周期，不是 route failed，也不是隐式 teleport。

## 6. 成功事实链

```text
system.commute.source.generated
  └─ mobility.requested
      └─ city_open_world_mobility_demands
             metadata.commute_source_code
             metadata.commute_binding_code
             metadata.commute_direction
             metadata.arrival_bridge.expected_origin
```

generation fact 记录 source、binding、Actor、方向、expected origin Facility 和 demand code。child request fact 和 demand metadata 捕获完整的当前 location；V10 若发现 Actor 在路线上被其他合法行为移动，会按既有 `origin_location_changed` 安全失败，而不会覆盖新位置。

## 7. V11 兼容与重复流量抑制

V11 的 `npc.assigned_facility_visit` 是历史 contract，V13 不得删除或改写其 source identity。对于具有完整 V13 commute source pair 的 V12 binding actor，V11 runtime pass 在 source due 时写 V11 自己的 `system.mobility.od.suppressed`，reason 为 `superseded_by_commute_source`，并照常推进其 V11 cadence/counter。

这样：

- V11 historical snapshot、API、counter 和 recovery 仍可解释；
- 同一 Actor 在一个周期不会同时由 generic facility visit 与 V13 commute 创建冲突 demand；
- V13 source 的成功/抑制仍完全独立可审计；
- 不通过违规 UPDATE 把 V11 immutable source status 改成“disabled”。

如果 binding 缺失、V13 source pair 不完整或 world 不是 V13，V11 保持原有行为。

## 8. 周期指标

`city_open_world_commute_cycle_metrics` 以 24 tick window 在后一个 tick 关闭，保存：

- outbound/return 的 generated 与 suppressed count；
- outbound/return 的 `expected_origin_unavailable` count；
- 属于 V13 commute demand 的 scheduled/completed/expired count；
- 属于 V13 commute demand 的 V10 arrival landed/blocked/failed count；
- 关闭时 pending commute demand count。

指标仅统计带强 metadata source identity 的 V13 demand，不将全网络 V11/玩家需求错误标为通勤。V11 的全网络指标继续由它自己的闭合窗口承担。

## 9. 升级、canonical、replay 与恢复

- 新 `city-openworld-v13` genesis：按 V5→V9→V10→V11→V12→V13 顺序创建基础状态，然后写 V13 version vector；
- `V12 → V13` 只能在 paused world、零 pending command 和审计 upgrade 中执行；它从冻结 binding 建立未来 source，不修改历史 V11/V9/V10 evidence；
- V13 snapshot 必同时含 V12 Commutes 和 V13 CommuteSources；V12 不能携带后者，V13 不能缺任一；
- recovery 先恢复 V5 actors/facilities、V9 topology、V10 arrival、V11 OD、V12 bindings，再恢复 V13 profile/sources/metrics；stable codes 在恢复时重新解析为 surrogate IDs；
- replay 的 static checkpoint 比较 V13 source identity、binding/direction/endpoints、origin contract、phase、mode、purpose 和 metadata，允许的动态变化只有 counters、due/transition/fact references 和 metrics；
- database deferred assertions 验证 profile counter、source pair、binding identity、facility/hub 关系、fact type、指标窗口以及 V13 version vector catalog。

## 10. API、权限与可观测性

新增只读接口：

```text
GET /api/v1/city/worlds/:world_id/open-world/commute-sources
```

返回 V13 profile、调用者可见 Actor 的 sources 与全局聚合 cycle metrics。owner/管理员/完整 world-read 可见全部 source；普通成员仅见自己控制的 Actor。接口不返回其他用户位置、内部 ID 或 V10 arrival 之外的隐私投影。

面板应将“已生成”“未在预期入口域”“路线未完成”“arrival 失败”分开显示，避免把 source suppression 误读为路线故障。

## 11. 验收矩阵

1. 新 V13 world 从 V5 NPC 在工作入口域的真实 genesis location 启动，先产生 work→home，再在回家 arrival 后产生 home→work；
2. source 在 T 生成、V9 不早于 T+1 调度、V10 不早于 route completion 后续 tick landing；
3. Actor 位于错误 Facility、失活 role/Facility、navigation/mobility conflict 均产生正确 suppression 且 source cadence 不忙循环；
4. V11 due source 对具有完整 V13 pair 的 Actor 只产生 `superseded_by_commute_source`，不生成第二条 demand；
5. V12→V13 upgrade 不重分类历史 V11 demand、route、arrival 或 bindings；
6. profile/source/metric guard、canonical state、version vector、replay/recovery 与 API privacy 均有 unit/integration 覆盖；
7. V13 metrics 只统计 `commute_source_code` 标记的 demand，不能混入玩家或 V11 generic source。

## 12. 后续依赖

V14 才应引入 source lifecycle：迁居、入职/离职、设施关闭、临时停工、排班和 rebind。F9.2.C freight 仍须等待订单、库存 ownership 与交付 journal；F9.3 多模式网络须把站点/道路层级作为 worldgen/style version vector 输入。两者都不得修改本版本的 immutable V12/V13 历史证据。

## 13. 实现闭环与验证证据

- migration `234_city_open_world_v13_commute_sources.sql` 建立 V13 capability、V12→V13 upgrade path、profile/source/metric 表、写闸门、deferred foundation assertion 与 version-vector catalog 断言；
- `open_world_commute_sources` 位于 V11 compatibility pass 与 V9 scheduler 之间；V11 对拥有完整 V13 pair 的 Actor 只记 `superseded_by_commute_source`，不再创建 generic demand；
- canonical runtime 同时持有 V12 `commutes` 与 V13 `commute_sources`；replay/recovery 按 V5→V9→V10→V11→V12→V13 依赖顺序恢复；
- 只读 API `GET /api/v1/city/worlds/:world_id/open-world/commute-sources` 已接入权限边界；
- 集成验证覆盖新 V13 world 的真实 surface egress 起点、成功/抑制事实、V11 去重、闭合指标、replay/recovery，以及 V12→V13 升级不改写旧 binding。
