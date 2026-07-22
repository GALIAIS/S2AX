# F9.2.B.0 住宅—就业绑定底座设计

版本：v1.0（2026-07-20）

状态：`city-openworld-v12` 已实现并通过新世界、V11→V12 升级、replay/recovery 回归。该切片先把微观 NPC 的住宅、就业地点与日程关系变成可验证的 canonical state；它**不**抢跑生成第二种自动出行需求，也不把宏观 household cohort、企业库存或订单错误地映射为某个 NPC 的私人信息。

## 1. 为什么必须先做这一层

V11 的 `npc.assigned_facility_visit` 只使用当前已验证的位置和一个工作 Facility 目标。它故意没有把工作地点叫作“家”，也没有假定 V5 `home_facility_id` 一定已经填写。

真实的通勤 source 至少需要同时回答四个问题：

1. 这个 Actor 是否真的是在岗劳动者；
2. 他/她的住宅是哪一个实际存在、可容纳的 residence Facility；
3. 就业地点是哪一个实际存在、处于 active 状态的 Facility；
4. 两个地点、两个 hub 与日程相位是否在世界版本冻结时已经确定。

没有这四项，"家到工作地" 只是根据当前位置、建筑名或 UI 字符串的猜测。V12 将它们固化为一次性基线，后续 V13+ 才能安全地从该基线生成双向 commute demand。

## 2. 版本边界与所有权

```text
V5  Actor / NPC profile / Facility / role
 │        │                  │
 │        └── active employment role
 │
 ▼
V12 commute binding profile ── immutable residence + employment binding
 │
 ├── V13+ home → work / work → home automatic OD source
 ├── F10 enterprise staffing / freight orders
 └── F11 household welfare, housing burden and accessibility
```

| 层 | 持有的真相 | V12 不得改写 |
| --- | --- | --- |
| V5 | Actor、NPC profile、Facility、角色、局部位置 | Actor 位置、NPC 行为、Facility 生命周期 |
| V9 | hub/edge/mode 与聚合 demand/route | 路由和容量状态机 |
| V10 | 完成 route 的受验证局部到达 | 历史 arrival |
| V11 | 自动 OD source 与交通周期指标 | V5/V9/V10 的归属 |
| V12 | 住宅—就业绑定、容量受限的住宅分配与双向日程相位 | 上述任一历史或投影 |

V12 profile 的 ID 为 `sub2api-open-world-commute-binding`，版本为 `1.0.0`。它进入版本向量的 content catalog；已有 V11 世界只能通过暂停的 `V11 → V12` upgrade 获得它，升级绝不把旧 V11 demand/route/arrival 重分类为通勤。

## 3. 权威输入与候选资格

一个 binding 只能从满足全部条件的 V5 NPC 建立：

- Actor 状态为 `active`，NPC profile 的 LOD 不是 `dormant`；
- 有一个 active、类别为 `employment` 的 Actor role；
- `work_facility_id` 指向 active、非 `residence` Facility，且存在正确绑定的 V9 facility hub；
- home 取自 active `residence` Facility，且存在正确的 V9 facility hub；
- 同一 Actor、同一 Facility、同一 hub 的所有 FK 均属于当前 world。

V5 的 `home_facility_id` 若已存在且仍是有效 residence，则优先作为输入；它不会因为 V12 的出现而被写回或修改。没有有效 V5 home 时，V12 仅在真实 residence Facility 的空余容量中作确定性分配。它绝不从 Actor 当前坐标、work Facility 或宏观 household cohort 猜测住宅。

## 4. 容量受限的确定性住宅分配

基线按照 Actor code 排序，并按下列顺序分配：

1. 先接受有效且尚未超容量的既有 V5 home；
2. 其余候选在按 `SHA-256("commute.residence.v1\\0" + actorCode + "\\0" + facilityCode)` 排序的 residence 候选中，选择第一个还有容量的 Facility；
3. 一旦所有真实 residence 都耗尽，不创建 binding，并把该劳动者计入 `unbound_candidate_count`。

`capacity_units` 是 V5 Facility 的上界，V12 只在自己的 `city_open_world_commute_bindings` 中占用一个 resident slot。数据库 deferred constraint 会同时检查：

- 每个 active home binding 的数量不超过 residence Facility 的容量；
- 每个 active work binding 的数量不超过就业 Facility 的容量；
- profile 的 candidate、binding、unbound 和 residence-used 计数与实际行相符。

因此容量不足会成为明确、可观察的城市状态，而不是静默溢出或虚构居住地。

## 5. Canonical state

### 5.1 `city_open_world_commute_profiles`

每个 V12 world 恰有一行，封存：

- baseline tick、profile identity、内容 hash 与 assignment contract；
- 24 tick cadence、最大 binding 数；
- `candidate_count`、`binding_count`、`unbound_candidate_count`、`residence_count` 与 `used_residence_units`；
- revision 与 object metadata。

profile 是静态输入目录，不是管理员可在运行中任意修改的设置。变更住宅分配算法、相位语义或容量意义必须发布新 profile/engine 版本。

### 5.2 `city_open_world_commute_bindings`

每行只表示一个在基线时合格劳动者的冻结关系：

```text
binding_kind            = npc.residence_employment
actor                   = V5 active NPC
home_facility/hub       = active residence Facility + its V9 hub
work_facility/hub       = active non-residence Facility + its V9 hub
period_ticks            = 24
outbound_phase          = schedule_offset mod 24
return_phase            = (outbound_phase + 12) mod 24
status                  = active
version                 = 1
```

binding 保存稳定 code、Actor code、Facility/hub codes、employment role、两条方向的 phase、baseline source 和 metadata。它不保存 Actor 的实时位置，也不保存家庭成员、钱包、订单、库存或任何私密自然人资料。

`status = active` 只表示 V12 binding 自己仍是该版本的有效历史关系，不把 Actor、role 或 Facility 的**当前**状态伪装成静态事实。未来 engine 的 source reducer 必须重新读取并验证实时 active/status/origin；这使后续离职、迁居、停业和角色撤销不会把历史 snapshot/recovery 锁死在旧状态。

## 6. 生命周期与未来 source

V12 本身不在 normal tick 修改 binding；它只是 V13+ 的安全输入。V13 的双向 commute source 必须遵守：

```text
home → work：只有 Actor 当前已验证地处 home building，且没有 navigation/mobility conflict 时才请求
work → home：只有 Actor 当前已验证地处 work building，且没有 navigation/mobility conflict 时才请求
```

每个方向必须有独立 source code、目的 hub、期望 origin、生成/抑制 fact、计划相位和 V9/V10 metadata。若 Actor 不在期望起点，V13 必须记录 `expected_origin_unavailable`，绝不能传送或用其它建筑替代。

未来发生迁居、离职、停业、Facility closure 或角色撤销时，也不能直接 UPDATE V12 binding；必须在新版本中引入具有 source fact、旧/新版本、停用/rebind 原因和 upgrade 策略的 lifecycle reducer。

## 7. 升级、回放与恢复

- 新 V12 world 在 genesis 先建立 V5/V9/V10/V11 前提，再建立 commute profile/bindings，随后写入 V12 version vector；
- `V11 → V12` 仅用暂停 tick 的有效 V5/V9 identity 建立未来绑定，不修改旧 source、demand、route、arrival 或 metrics；
- binding/profile 进入 runtime snapshot、state hash、replay static-check 与 recovery；
- replay 时 profile identity、算法 contract、binding identity、home/work/hub、role、phase 和 metadata 必保持不变；
- recovery 从 snapshot 的稳定 code 重建 surrogate ID，并重新执行数据库 capacity/foundation assertion；
- V12 之前携带 commute state、或 V12 缺少 state/version-vector catalog，均应使 canonical validation 失败。

## 8. API 与隐私

只读接口：

```text
GET /api/v1/city/worlds/:world_id/open-world/commutes
```

返回 profile 计数与调用者可见 Actor 的 binding。世界 owner、系统管理员与具有完整 world-read 权限的调用方可查看全部 binding；其他成员只会看到自己被控制 Actor 的 binding。未绑定候选只以 profile 聚合数暴露，不列出其私有住宅偏好或原因细节。

## 9. 验收

V12 的最低验收包括：

1. 新世界仅为合格的 employment NPC 建立 binding；
2. home/work/hub/FK 和 capacity 约束均被数据库与 Go validation 双重验证；
3. 缺少 residence 容量时增加 unbound count 而不伪造 residence；
4. V11→V12 paused upgrade 不修改历史 V11 traffic evidence；
5. snapshot、replay、recovery 后 commute state 字节语义一致；
6. version vector、API route、migration guards 和 predecessor assertions 已覆盖 V12。

## 10. 下一步

V13 已在 V12 binding 之上实现实际的双向 `npc.residence_to_work` / `npc.work_to_residence` source，详见[《F9.2.B.1 / V13 双向通勤 Source 设计》](CITY_SIMULATION_F9_2C_COMMUTE_SOURCES_DESIGN_CN.md)。V14 才处理迁居、入职/离职、设施关闭和班次导致的 lifecycle/rebind；企业 freight 仍要等待 F10.0 的订单、库存 ownership、交付与 journal 事实，不得把 V12 binding 当作企业订单的替代品。
