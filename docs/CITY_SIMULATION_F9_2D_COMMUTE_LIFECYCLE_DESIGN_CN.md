# F9.2.B.2 / V14 通勤生命周期与 Assignment Epoch 设计

版本：v1.1（2026-07-20）

状态：已实现为 `city-openworld-v14` 并完成服务层、迁移结构及 PostgreSQL 集成验证。V14 不是对 V12 binding 或 V13 source 的原地修补；它在两者之上建立可追加、可回放的 assignment epoch 层，使迁居、换岗、停工、设施停用与恢复都有明确的因果和历史边界。

## 1. 问题与不可违反的边界

V12 binding 和 V13 source 都有意冻结：前者冻结某一时刻的 residence/employment 基线，后者冻结该基线对应的双向 source identity。直接更新其 home/work Facility、role、phase 或删除旧 source 会同时破坏 capacity 解释、历史 route/arrival 归属、snapshot 哈希和 replay。

因此 V14 采用如下规则：

1. V12/V13 行永远是历史证据，不 UPDATE、不 DELETE、不借“当前配置”重解释；
2. 任何有效通勤关系都由一个新的 **Assignment Epoch** 表示；epoch 固定其 Actor、role、home/work Facility+hub、phase、周期与来源；
3. pause/resume/supersede/terminate 是 append-only lifecycle transition，不修改旧 epoch；
4. relocation/job change 创建新 epoch，旧 epoch 以 fact-backed `superseded` 收束；
5. V14 source 只消费当前 active epoch，V13 source 在 V14 world 中保留审计但不继续生成 demand；
6. V11 generic source 继续被抑制，不能在 V14 assignment 暂停时绕过 lifecycle 再次制造未授权流量；
7. 管理员命令本 tick 只写生命周期事实；新 epoch 的 source 最早下一个 tick 才可成为 due，仍由 V9/V10 完成后续链路。

## 2. 领域模型

```text
V12 immutable binding ──────┐
V13 immutable source pair ──┤ historical/audit baseline
                            │
                            ▼
V14 assignment epoch ──── V14 direction source pair
        │                         │
        ├── lifecycle transitions │ generated/suppressed fact
        │                         ▼
        └── active effective assignment → V9 demand → V10 arrival
```

### 2.1 Assignment Epoch

`city_open_world_commute_assignment_epochs` 是不可变的有效通勤快照：

```text
code, binding_code, actor, epoch_number, assignment_kind
employment_role_code
home_facility_code/home_hub_code
work_facility_code/work_hub_code
period_ticks, outbound_phase, return_phase
origin_kind, opened_tick, opened_fact
metadata
```

- `origin_kind=v13_baseline`：V14 genesis/V13→V14 upgrade 从 V12 binding 逐条建立第 1 个 epoch；
- `origin_kind=admin_rebind`：管理员 command 经验证后建立 successor epoch；
- 后续版本可增加经事实批准的 `household_relocation`、`employment_transfer` 等 origin，但不能复用 `admin_rebind` 伪装自动变化；
- `epoch_number` 对一个 V12 binding 单调递增；epoch code 由 `binding_code + epoch_number` 的固定 hash 得出；
- home/work、hub、role 和 phase 是 epoch 身份的一部分，历史 epoch 永远保留，即使其目标设施后续关闭。

### 2.2 Lifecycle Transition

`city_open_world_commute_assignment_transitions` 追加记录一个 epoch 的状态演进：

| 状态 | 允许后继 | 含义 |
| --- | --- | --- |
| `active` | `suspended`、`superseded`、`terminated` | V14 source 可在到期时验证 origin 并生成 demand |
| `suspended` | `active`、`superseded`、`terminated` | 当前不生成 demand；不以周期性 suppression 伪造活动 |
| `superseded` | 无 | 已被 successor epoch 取代，终态 |
| `terminated` | 无 | Actor 不可恢复、binding 被明确废止等终态 |

每个 transition 必有 `system.commute.assignment.*` runtime fact，transition tick 严格递增。当前状态由每个 epoch 的最后一条 transition 派生；不保存可被后台直接篡改的“current status”字段。

标准原因：

```text
baseline_initialized
admin_rebind
admin_suspended
admin_resumed
employment_role_inactive
employment_role_restored
origin_facility_unavailable
origin_facility_restored
destination_facility_unavailable
destination_facility_restored
actor_inactive
profile_assignment_mismatch
```

`profile_assignment_mismatch` 只暂停并暴露管理员待处理项：V14 不会把 V5 NPC profile 的字段变化自动当作合法换房/换岗，也不会在没有 capacity/role 证据时“猜测”新 assignment。

### 2.3 V14 Source

`city_open_world_commute_lifecycle_sources` 对每个 epoch 只有两条行：

| 方向 | source kind | 起点 | 终点 | purpose |
| --- | --- | --- | --- |
| outbound | `npc.residence_to_work` | epoch home | epoch work | `routine.commute.outbound` |
| return | `npc.work_to_residence` | epoch work | epoch home | `routine.commute.return` |

source 的成功、origin 失败、导航/未完成 mobility 冲突以及 V9/V10 因果约束沿用 V13。不同之处是：只有 assignment 的 latest transition 为 `active` 才会进入 source 准入；`suspended` epoch 不按每个周期重复写 source suppression，恢复为 active 后由已经到期的 source 以单次正常 due 处理并推进 cadence。

旧 epoch source 在 `superseded`/`terminated` 后保留历史行和 counter，但再不进入 due 集。V14 demand metadata 必须同时带：

```text
commute_lifecycle_source_code
commute_assignment_epoch_code
commute_binding_code
commute_direction
arrival_bridge.expected_origin
```

不能只使用 V13 `commute_source_code`，否则 V13 closed-cycle metric 会把 V14 traffic 混入历史窗口。

## 3. 自动生命周期判定与人工命令

### 3.1 自动判定

在每个 V14 tick、V14 source pass 前，系统读取 epoch 当前 Actor、employment role、home/work Facility 和 NPC profile：

1. `actor.status != active`：append `terminated(actor_inactive)`；
2. employment role 非 active：active epoch append `suspended(employment_role_inactive)`；role 恢复且全部静态端点重新有效时 append `active(employment_role_restored)`；
3. home 或 work Facility 非 active：append `suspended(origin|destination_facility_unavailable)`；设施恢复后按同一原则 resume；
4. NPC profile 的 home/work 与 active epoch 不同：append `suspended(profile_assignment_mismatch)`，只提供可审计待处理，不自动 rebind；
5. active epoch 的所有资格有效时不写事实；同 tick 重复运行不应产生第二条 transition。

自动判定不使用实时 Actor 位置作为 rebind 输入。位置只属于 source generation 的 Facility Presence Domain。

### 3.2 管理员命令

新增命令仅供 world owner / system administrator：

```text
open_world.commute.assignment.rebind
open_world.commute.assignment.set_state
```

`rebind` payload 固定包含 `actor_code`、`home_facility_code`、`work_facility_code`、`employment_role_code`、受控 `reason_code`；`outbound_phase` 可选，缺失时由稳定 hash 计算，return phase 永远是半周期偏移。它在 command 应用阶段：

1. 验证目标 Actor active、role active、Facility/hub active、home/work 类型、不同端点、phase 与 effective capacity；
2. 追加旧 epoch `superseded` fact/transition；
3. 追加新 epoch opened fact/`active` transition，并建立新 V14 source pair；
4. 不改变 V12 binding、V13 source、旧 V9 demand/route/arrival 或旧 epoch；
5. 若 command 在 T 处理，新 source 最早从 T+1 进入 due 语义。

`set_state` 只能将当前非终态 epoch 在 `active` 与 `suspended` 之间转换；管理员不能“复活” `superseded` 或 `terminated` epoch，也不能绕过 rebind capacity 校验。

## 4. Capacity 与有效 assignment

V12 的 capacity 只解释其 genesis binding。V14 另定义 **effective assignment set**：每个 V12 binding 至多有一个 nonterminal latest epoch；home/work capacity 只计算该集合中的 active 或 suspended epoch，不把已经 superseded 的旧 epoch 与 successor 双重计数。

在 rebind transaction 中，用 world-scoped lock、稳定 Actor/Facility 排序和 deferred assertion 同时检查：

```text
effective home occupants <= home capacity
effective work occupants <= work capacity
one latest nonterminal epoch per binding
one latest active/suspended assignment per actor
one source pair per epoch
```

没有空位时 command 被拒绝并保留旧 epoch；不以当前位置、任意同类设施或 UI 名称替代用户指定的 endpoint。

## 5. 时序、版本与历史保持

V14 engine 顺序：

```text
V11 legacy OD compatibility
→ V14 lifecycle transition pass
→ V14 active-epoch source pass
→ V9 scheduler
→ V10 arrival bridge
→ command application
→ V5 navigation
```

V14 不执行 V13 source pass；但 V13 profile/source/metric 仍进入 canonical state、replay/recovery，且继续让 V11 generic OD 保持被抑制。V14 的 runtime state 是 V13 的 successor projection，不是替换旧表的 destructive migration。

版本关系：

```text
V12 immutable binding → V13 source baseline → V14 assignment epoch lifecycle
```

V13→V14 upgrade 仅在 paused world、无 pending command、经过审计的 upgrade 中执行。它从每个 V12 binding 建第 1 个 V14 epoch 和 pair，不改写 V13 historical counter/due/fact；V14 baseline 从 upgrade tick 起算。

## 6. 数据、canonical、replay、recovery

V14 新增：

```text
city_open_world_commute_lifecycle_profiles
city_open_world_commute_assignment_epochs
city_open_world_commute_assignment_transitions
city_open_world_commute_lifecycle_sources
city_open_world_commute_lifecycle_cycle_metrics
```

所有表由 world/runtime fact/upgrade/recovery 作用域写闸门保护。runtime canonical state 新增 `commute_lifecycle`，同时保留 V12 `commutes` 与 V13 `commute_sources`。

replay 必须：

- 验证 epoch code、endpoint、phase、source pair 和 lifecycle transition 的静态/顺序约束；
- 验证每个 transition/source/metric 引用的 runtime fact type、tick、sequence、payload identity；
- 验证 rebind command source、old epoch final transition、新 epoch opened transition 与 source pair 的同 tick因果关系；
- 只从 tick checkpoint 安装 projection，绝不从当前 live Facility/profile 推断历史 rebind。

recovery 按 V5→V9→V10→V11→V12→V13→V14 顺序恢复，先解析 stable code 到 surrogate ID，再插入 epoch、transition、source、metric，最后运行 V14 deferred assertion。

## 7. API、权限与可观测性

新增只读：

```text
GET /api/v1/city/worlds/:world_id/open-world/commute-lifecycle
```

返回 profile、调用者可见 Actor 的 epoch/source/transition 和聚合周期指标。普通成员只能读取自己控制 Actor 的 assignment，不读取其他 NPC 的位置、command payload、内部 ID 或全量 roster。管理员使用既有 command endpoint 提交 rebind/suspend/resume，不能直接写 lifecycle 表。

面板应区分：

- 当前有效 assignment；
- `suspended` 的具体原因和恢复条件；
- 已 superseded/terminated 的历史 epoch；
- source 未到期、在预期 origin 外、V9 路由未完成、V10 arrival 受阻；
- rebind capacity rejection 与 source failure。

## 8. 验收矩阵

1. V14 genesis 和 V13→V14 upgrade 都为每个 V12 binding 建唯一 baseline epoch/source pair，不改变 V13 行；
2. facility/role/profile mismatch 只追加可解释 transition，重复 tick 不重复写；恢复条件只产生一次 resume；
3. 终态 epoch 永不复活；manual suspend/resume 不改变 endpoint；
4. rebind 原子地 supersede 旧 epoch、创建 successor/source pair，并维持 effective home/work capacity；
5. V14 source 不混入 V13 metric，V11 不因 V14 suspended 而回退生成 generic demand；
6. command 在 T 生效时，新 source 不能在 T 被 V9 调度；
7. snapshot/replay/recovery、V13→V14 upgrade、重复 command 幂等、并发 capacity race、API privacy 与数据库 guard 均覆盖。

## 9. 后续依赖

V15 才可将 household relocation、enterprise employment transfer、班次/休假、设施维修计划和劳动市场匹配作为各自的事实 producer 接到 V14 rebind command contract。F9.2.C freight 仍需 F10.0 订单/库存/交付底座；它不得复用 Assignment Epoch 作为企业物流订单。

## 10. 实现与验证记录

- `city_open_world_commute_assignment_epochs`、transition、lifecycle source 与 cycle metric 已由 V14 genesis 及 V13→V14 paused upgrade 建立；V12 binding 和 V13 source 继续作为封存证据保留。
- migration `236_city_open_world_v14_commute_lifecycle_hardening.sql` 在既有 foundation guard 之外增加 epoch 连续性、opening fact、transition/source 因果、有效 epoch 唯一性与周期窗口完整性检查；所有检查均在提交前延迟执行。
- runtime 将同 tick 的管理员 transition 计入自动 transition 的总预算，防止 command 与自动判定合计突破每 tick 上限。
- canonical snapshot、replay、recovery、版本向量和只读 lifecycle API 已接入；管理员命令在普通 owner/member 上会被拒绝，不能绕过系统管理员边界。
- 已验证：`go test ./internal/service ./internal/handler ./internal/server/routes ./migrations -count=1`、`go test -tags=integration ./internal/repository -run '^TestCityOpenWorldV14CommuteLifecycleIsFactBackedAndRecoverable$' -count=1 -v` 与 V13→V14 upgrade 集成测试。
