# 城市模拟 F7.4 开发项目、审批与施工事实链详细设计

版本：v1.0（2026-07-18）  
目标引擎：`city-f7-v3`  
前置引擎：`city-f7-v2`  
状态：已完成并通过全量验收门禁

## 1. 目标与边界

F7.4 在 F7.3 的不可变地块/建筑基线上增加可持续演化的开发项目。玩家不再直接修改建筑容量，而是提交开发意图，经过审批、开工、资源结算、逐 tick 施工和完工后形成不可变建筑调整事实。

本阶段闭环实现：

1. `申请 → 批准/驳回 → 开工 → 施工 → 完工/取消` 状态机；
2. 垂直扩建和整修改造两类确定性项目；
3. 分区、FAR、楼层、质量、开发者资格、劳动力容量和资源库存约束；
4. 开工时一次性消耗材料与资本品，施工中占用劳动力容量；
5. 项目事实、资源 operation、建筑 adjustment、district 聚合容量的单事务闭环；
6. `city-f7-v3` 规范状态、同 tick 升级、快照、逐 tick 重放、漂移诊断和恢复；
7. 成员授权的项目查询以及 CLASSIC 地图中的施工/完工叠层与检查器；
8. 所有命令的严格 payload、幂等、稳定业务拒绝码和无旁路写入。

本阶段不实现：

- 新地块划分、建筑 footprint 迁移、产权交易和征地；
- 拆除、重建及历史住户安置；这些需要 F7.4 后续独立版本，不能伪装成负 adjustment；
- 企业租约和商业/工业选址；属于 F7.5；
- 现金融资、贷款、利率、股票和证券；
- 玩家指定资源成本、工期、进度或完工 tick；这些全部由世界绑定政策计算。

## 2. 核心原则

### 2.1 F7.3 基线永不改写

`city_buildings`、`city_building_unit_pools` 和 portal 基线继续保持 F7.3 封账状态。F7.4 不直接更新这些行：

```text
F7.3 immutable building baseline
  + posted F7.4 building adjustments
  = effective building projection
```

因此历史 `city-f7-v2` 快照逐字节不变，F7.4 的恢复也能分别验证“基线是否漂移”和“开发事实是否漂移”。

### 2.2 命令只表达意图

客户端可提交目标楼层或目标质量，但不能提交：资源成本、增加容量、工期、进度、审批结果之外的状态、完工 tick、建筑调整值。

所有派生值由世界绑定的 `sub2api-development@1.0.0` 政策和当前规范状态计算，并在提交事实中固化：

```text
normalized intent
  + effective building state
  + zoning snapshot
  + developer capacity
  + bound policy hash
  = immutable project plan
```

### 2.3 资源与施工事实原子封账

开工命令在同一 tick 事务中：

1. 锁定项目、开发者、资源余额；
2. 校验劳动力可用容量；
3. 分别发布 `basic_material` 与 `capital_goods` 两笔守恒的 development consumption resource operation；
4. 两笔 operation 都绑定同一个项目、开工命令与 draft `started` 事实 ID；
5. 发布 `started` 项目事实；
6. 把项目投影推进到 `under_construction`。

任一步失败则整个命令回滚并得到稳定业务拒绝码。资源在开工时视为动员与采购后投入，开工后取消不退款；这条规则进入政策版本，不能由客户端选择。

### 2.4 自动进度由 tick 决定

施工进度不接受写接口。设开工 tick 为 `S`、工期为 `D`，目标 tick 为 `T`：

```text
progress_milli = min(1000, floor((T - S) × 1000 / D))
```

开工当 tick 进度为 0；随后每个运行中的 tick 至多生成一个 progress/completed 事实。暂停世界仍可处理控制命令，但不推进施工。

## 3. 绑定政策 `sub2api-development@1.0.0`

政策内容使用稳定规范 JSON 计算 SHA-256，并写入每个世界的 development profile。

### 3.1 垂直扩建 `vertical_expansion`

输入：现有建筑、`target_floor_count`、开发者。

约束：

- 目标楼层严格大于当前有效楼层；
- 不超过地块 zoning 的 `max_floors`；
- 新有效总楼面面积不超过地块 FAR；
- 建筑、地块、开发者均为 active；
- 同一建筑同一时刻最多有一个非终态项目。

派生：

```text
added_floors       = target_floor_count - effective_floor_count
added_floor_area   = footprint_area_sqm × added_floors
added_capacity     = floor(added_floor_area / sqm_per_capacity_unit)
basic_material     = ceil(added_floor_area / 1_000)
capital_goods      = ceil(added_floor_area / 10_000)
labor_capacity     = ceil(added_floor_area / 5_000)
duration_ticks     = clamp(ceil(labor_capacity / 8), 2, 720)
```

`added_capacity` 必须大于零。住宅、商业和工业容量分别进入对应 district 聚合字段。

### 3.2 整修改造 `renovation`

输入：现有建筑、`target_quality_milli`、开发者。

约束：

- 目标质量严格大于当前有效质量；
- 目标质量不超过 `1500`；
- 不改变 footprint、楼层、面积和容量；
- 同一建筑同一时刻最多有一个非终态项目。

派生（`Q = target - current`）：

```text
basic_material = ceil(effective_floor_area × Q / 50_000)
capital_goods  = ceil(effective_floor_area × Q / 200_000)
labor_capacity = ceil(effective_floor_area × Q / 100_000)
duration_ticks = clamp(ceil(labor_capacity / 8), 1, 360)
```

所有正需求至少为 1，所有乘法在执行前检查 `int64` 溢出。

### 3.3 开发者和劳动力

- 首版开发者必须是 active `firm`，且存在 active `city_firm_states`；
- 企业所在 district 必须与建筑所在 district 相同；
- 企业 `employee_units` 是劳动力总容量；
- 当前所有 `under_construction` 项目的 `required_labor_units` 之和，加上待开工项目需求，不得超过该容量；
- 完工或取消立即释放劳动力容量；劳动力不是库存，不通过 resource balance 消耗。

该边界为后续承包商、跨区施工队、技能工种和工资结算保留稳定扩展点。

## 4. 状态机

```text
development.submit
        │
        ▼
    submitted ── development.review(reject) ──► rejected
        │
        └────── development.review(approve) ─► approved
                                                   │
                          development.cancel ◄─────┤
                                                   │ development.start
                                                   ▼
                                          under_construction
                                             │          │
                                tick progress│          │development.cancel
                                             ▼          ▼
                                         completed   cancelled
```

终态：`rejected`、`completed`、`cancelled`。终态不可重开或删除。取消允许来自 `submitted`、`approved`、`under_construction`；开工后的取消保留已消耗资源和进度历史，但不形成建筑 adjustment。

## 5. 命令协议

### 5.1 `development.submit`

垂直扩建：

```json
{
  "project_type": "vertical_expansion",
  "building_code": "building_central_p0_p0_nw",
  "developer_entity_id": 42,
  "target_floor_count": 6,
  "name": "Central Housing Extension"
}
```

整修：

```json
{
  "project_type": "renovation",
  "building_code": "building_central_p0_p0_nw",
  "developer_entity_id": 42,
  "target_quality_milli": 1150,
  "name": "Central Housing Renewal"
}
```

服务器分配稳定项目 code：`development_<command-sequence>`。响应返回 code、派生成本、工期与提交事实游标。

### 5.2 `development.review`

```json
{
  "project_code": "development_12",
  "decision": "approve",
  "note": "Complies with zoning"
}
```

`decision` 只能是 `approve|reject`；note 最多 256 个 Unicode 字符。

### 5.3 `development.start`

```json
{"project_code":"development_12"}
```

开工时重新校验建筑有效状态、zoning、活动项目冲突、企业资格、劳动力和库存。提交到开工期间若事实已变化，必须拒绝而不是沿用过期计划。

### 5.4 `development.cancel`

```json
{
  "project_code": "development_12",
  "reason": "Priority changed"
}
```

reason 必填，1–256 字符。

## 6. 稳定业务拒绝码

| code | 语义 |
| --- | --- |
| `CITY_DEVELOPMENT_PROJECT_NOT_FOUND` | 项目不存在或不属于世界 |
| `CITY_DEVELOPMENT_BUILDING_NOT_FOUND` | 建筑不存在/不可用 |
| `CITY_DEVELOPMENT_DEVELOPER_INVALID` | 开发者不是同区 active firm |
| `CITY_DEVELOPMENT_STATE_CONFLICT` | 状态转换非法 |
| `CITY_DEVELOPMENT_ACTIVE_PROJECT_CONFLICT` | 建筑已有非终态项目 |
| `CITY_DEVELOPMENT_ZONING_REJECTED` | 楼层/FAR/质量不满足政策 |
| `CITY_DEVELOPMENT_LABOR_CAPACITY` | 企业劳动力容量不足 |
| `CITY_DEVELOPMENT_RESOURCE_INSUFFICIENT` | 材料或资本品库存不足 |
| `CITY_DEVELOPMENT_PLAN_STALE` | 开工重算结果不再等于已批准计划 |

业务拒绝只拒绝该命令，不回滚同 tick 的其他合法命令。数据库错误、hash 漂移和不变量失败仍中止整个 tick。

## 7. 数据模型

### 7.1 `city_development_profiles`

每世界一行：政策 ID/版本/hash、基线 tick、project/fact/adjustment 计数、revision 和 metadata。计数与 revision 是当前投影的一部分，每次 posted fact 连续增长。

### 7.2 `city_development_projects`

当前项目投影，保存 stable code、项目类型、建筑/地块/district/developer、目标值、派生 adjustment、三类需求、计划工期、状态、进度、关键 tick 和版本。最后事实由 `(project_id, tick, sequence)` 的事实链确定，不在可变投影中重复保存。

可变字段只能由 reducer 在 tick 写闸门内推进；删除永远禁止。

### 7.3 `city_development_facts`

不可变事实头：world、tick、sequence、project、可选 source command、fact type、from/to status、progress before/after、project version before/after、metadata 和 posted_at。

fact type：`submitted`、`approved`、`rejected`、`started`、`progressed`、`completed`、`cancelled`。只允许 draft → posted；同 tick 按 sequence 唯一。

### 7.4 `city_building_adjustments`

每个 completed 项目最多一行，保存增加楼层、top Z、楼面面积、容量和质量增量及完成事实。行不可更新/删除。有效建筑是基线加按项目 code 稳定排序的 adjustment 之和。

垂直扩建的新增 stair portal 使用 `(project code, building code, z)` 从 adjustment 确定性派生，不另建可漂移表。

### 7.5 `city_development_baselines`

创世或 v2→v3 升级时发布空 development 基线，绑定政策 hash、当前 tick 和空状态 hash。后续事实不修改它。

## 8. 数据库硬约束

提交前 assertion 至少验证：

1. 只有 `city-f7-v3` 世界可存在 development 数据；
2. profile/baseline 政策和基线 tick 一致；
3. profile 计数等于实际 projects、posted facts、adjustments；
4. 项目 stable code、类型、目标和派生需求合法；
5. 项目最后事实、状态、进度、版本和关键 tick 完全对应；
6. fact 状态转换和版本/进度连续，source command 类型匹配；
7. 每栋建筑至多一个非终态项目；
8. under-construction 劳动力汇总不超过开发者容量；
9. completed 项目恰有一个 adjustment，其他项目没有；
10. adjustment 累加后满足 zoning 最大楼层/FAR/质量边界；
11. effective building capacity 按 district/use 汇总等于 F3 district 聚合容量；
12. 每个 `started` fact 恰好对应两笔 development consumption operation；两笔 operation 分别结算 `basic_material` 与 `capital_goods`，且项目、tick、需求、source command 和绑定 fact ID 完全一致；
13. 所有写入只允许 tick、升级或已验证 recovery gate。

## 9. Tick 顺序

`city-f7-v3` 引擎顺序：

```text
control
→ ledger
→ resources
→ calendar_demography
→ spatial
→ development
→ markets
```

预扫描发现 `development.start` 时必须先完成 resource opening bootstrap。命令按全局 command sequence 执行；development fact sequence 和 resource operation sequence 都在 tick 内连续。

命令处理结束后，仅在世界处于 running 时推进已有施工项目。新开工项目同 tick 不推进。完工时在一个 reducer 中发布 fact、插入 adjustment、更新 district 容量和项目投影。

## 10. 规范状态、重放与恢复

`city-f7-v3` 在 `land` 后增加：

```json
{
  "development": {
    "profile": {},
    "projects": [],
    "facts": [],
    "adjustments": []
  }
}
```

排序：项目 code；fact `(tick, sequence)`；adjustment `(building_code, project_code)`。不包含数据库 ID、时间戳和本地化文案。

Replay：development stage 读取该 tick posted facts，逐条校验 before/after 状态并归约项目；completed fact 同步加入 adjustment 和 district 容量。资源 stage 已先重放开工 resource operation，因此状态 hash 顺序一致。

Recovery：先保存被不可变 resource operation 引用的 development fact ID，再清空并从 snapshot 精确恢复 profile、baseline、projects、facts、adjustments；恢复事实时复用这些 ID，然后恢复 district 容量、资源余额等既有投影；运行所有 assertion，重载 canonical 必须逐字节一致。这样恢复不会破坏已封账 operation 到 `started` fact 的引用完整性。

## 11. API 与前端

读取：

```text
GET /api/v1/city/worlds/:world_id/development
  ?status=&building_code=&after_tick=&after_sequence=&limit=
```

返回 profile、过滤后的项目、事实游标、可用开发者摘要。成员只读；命令仍走统一 command endpoint，owner 才可提交。

CLASSIC 前端：

- Overmap 显示区域内 active/completed project 数；
- Local 以施工标记覆盖 under-construction 建筑，不替换基础结构语义；
- 检查器显示有效楼层/容量/质量、基线值、累计 adjustment 和当前项目；
- 项目面板提供状态 Tabs、确定性成本预览、审批/开工/取消操作；
- tick/刷新继续保留现有场景，不以整页 loading 替换内容。

## 12. 验收门禁

1. 四类命令严格规范化、幂等和稳定拒绝码测试通过；
2. 垂直扩建/整修成本、工期、整数舍入和溢出黄金测试通过；
3. zoning、同建筑活动项目、劳动力和库存并发约束通过；
4. 开工两笔 resource operation 与 started fact 原子封账；取消前后资源语义正确；
5. 多 tick 进度单调、精确完工，不受墙钟和数据库 ID 影响；
6. effective building、unit pool、portal 和 district 容量一致；
7. v2 canonical 历史不变；v2→v3 dry-run 无落库，apply 同 tick 建 development baseline；
8. v3 创世、snapshot、逐 tick replay、投影漂移诊断和 verified recovery 闭环；
9. profile/project/fact/adjustment/resource operation 直接写入被数据库拒绝；
10. API 授权、过滤、游标和上限测试通过；
11. CLASSIC 显示只消费后端事实，命令操作不触发整页闪烁；
12. 全量 Go、迁移测试、PostgreSQL 集成、前端测试、typecheck 和 production build 通过。

## 13. 实施与验证结果

F7.4 已按上述协议完成 migration、服务 reducer、规范状态、v2→v3 升级、快照、重放、verified recovery、成员查询 API、effective land 投影及 CLASSIC 前端。额外闭合了以下实现期发现的基础问题：

- development consumption 与既有 market settlement resource operation 保持兼容；
- 多世界 zoning assertion 只在目标世界关系内比较；
- recovery 复用被不可变 resource operation 引用的 development fact ID；
- 垂直扩建成本按现实创世库存重新校准，并由规范 policy JSON/hash 绑定；
- integration tenant-data 隔离保留静态引擎目录和默认分组，完整 repository 集成套件可一次运行。

验收证据（2026-07-18）：

- 后端 `go test ./... -count=1` 通过；
- PostgreSQL `go test -tags=integration ./internal/repository -count=1` 通过；
- 前端 185 个测试文件、1238 个测试通过；
- 前端 typecheck 与 production build 通过；
- v3 开发项目闭环、垂直扩建、失败原子性、v2→v3 升级、重放、漂移恢复和直接写闸门集成测试通过。

F7.4 现已完成，下一依赖切片为 F7.5 企业空间与经营场所。拆除/重建仍须使用独立版本，本文不宣称覆盖。
