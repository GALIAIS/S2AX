# 城市模拟 F7.5 企业空间、经营场所与迁址事实链详细设计

版本：v1.0（2026-07-18）  
目标引擎：`city-f7-v4`  
前置引擎：`city-f7-v3`  
状态：已完成（后端事实链、升级、规范哈希、重放恢复、查询 API 与前端操作闭环）

## 1. 目标与阶段边界

F7.5 把 F3 中仅挂在 district 上的企业状态，落到 F7.3/F7.4 的真实商业和工业建筑池。完成后，“企业位于某区”不再是一列抽象字段，而是一组可查询、可迁址、受建筑容量约束、可重放恢复的经营场所事实。

本阶段闭环实现：

1. 每个 active firm 恰有一个总部场所，并按生产能力拥有必要的生产场所；
2. 总部、办公室、生产、仓储、零售五类场所及用途兼容规则；
3. `开设 → 调整 → 关闭` 与“整组迁址”命令；
4. 商业/工业 unit pool 的有效占用投影，不改写 F7.3 基线；
5. 迁址时企业 district、库存 district、生产约束和经营场所同事务切换；
6. 迁址库存使用 F3 守恒 resource operation，不能直接搬余额；
7. `city-f7-v4` 规范状态、升级、快照、逐 tick 重放、漂移诊断和恢复；
8. 成员授权查询、CLASSIC 地图叠层、检查器和企业场所面板。

本阶段不实现：

- 新企业创建、破产、行业投入产出网络和多企业市场；属于 F10；
- 土地或建筑产权、商业租金竞价、合同现金流、押金和违约；这些需独立经济协议，不能用场所占用伪装；
- 跨企业转租、共享工位、楼层级产权和独立室内 unit 实例；
- 通勤路径和货运路径；F7.5 只发布可供 F9 消费的稳定起讫点；
- 利率、信贷和证券。

## 2. 核心原则

### 2.1 基线与有效投影分离

F7.3 的 `city_buildings` 与 `city_building_unit_pools` 继续不可变。F7.4 adjustment 只增加有效供给，F7.5 site 只占用有效供给：

```text
F7.3 unit-pool baseline
  + F7.4 completed capacity adjustments
  = effective unit-pool supply

F7.3 residential allocations
  + F7.5 active enterprise sites
  = effective occupied units
```

住宅池只能被 housing allocation 占用；商业/工业池只能被 enterprise site 占用。有效 occupied 不回写基线行，而在读取、assertion 和规范状态中由事实投影合成。

### 2.2 场所不是企业本身

`city_economic_entities` 是法律/经济主体；`city_firm_states` 是企业聚合生产状态；enterprise site 是该主体在具体建筑中的空间占用。一个企业可有多个场所，但主体、账户、库存和历史不会因关闭某一场所而删除。

### 2.3 命令表达经营意图，容量由政策派生

客户端可指定目标池和场所类型，但不能提交有效容量、员工密度、生产密度、库存迁移明细或 district 更新值。首版政策从 firm state 派生最低占用：

```text
headquarters/office units = ceil(employee_units / 4)
production units          = ceil(production_capacity_units / 2)
warehouse units           = ceil(capital_stock_units / 10)
retail units              = ceil(employee_units / 8)
```

所有正需求至少为 1。显式扩容可提交 `target_occupied_units`，但不得低于政策最低值；缩容同样重新校验企业当前状态。

### 2.4 迁址是一个原子业务动作

跨区迁址必须在同一 tick 事务中完成：

1. 锁定 firm、旧场所、目标池、全部库存余额和活动开发项目；
2. 校验目标场所容量与用途；
3. 将旧 district 的全部非零库存按 resource code 排序，发布一笔包含逐资源出入配对的守恒 district transfer operation；
4. 关闭旧 primary sites，建立新总部和生产场所；
5. 更新 `city_firm_states.district_id` 与 version；
6. 发布单个 `relocated` location fact，固化前后组合和 resource operation 游标；
7. 运行资源、场所、建筑占用和 firm district assertions。

任一步失败则整个命令回滚。禁止先改 district、下一 tick 再搬库存，也禁止直接更新库存余额。

### 2.5 事实和投影职责

- location fact 是场所状态变化的唯一历史依据；
- site 表是下一 tick 和查询使用的在线投影；
- effective occupancy 是可由基线、adjustment 和 active site 重算的派生投影；
- `city_firm_states.district_id` 是企业主运营 district，必须与 active headquarters district 一致；
- 地图颜色、图标和本地化文本不进入规范状态。

## 3. 绑定政策 `sub2api-enterprise-location@1.0.0`

政策使用规范 JSON 和 SHA-256 固定，写入每个世界的 location profile。首版字段：

```json
{
  "id": "sub2api-enterprise-location",
  "version": "1.0.0",
  "employee_units_per_office_unit": 4,
  "production_capacity_per_production_unit": 2,
  "capital_stock_per_warehouse_unit": 10,
  "employee_units_per_retail_unit": 8,
  "maximum_active_sites_per_firm": 32,
  "site_types": {
    "headquarters": ["commercial"],
    "office": ["commercial"],
    "production": ["industrial"],
    "warehouse": ["industrial"],
    "retail": ["commercial"]
  }
}
```

### 3.1 必需场所

- 每个 active firm 恰有一个 active headquarters；
- `production_capacity_units > 0` 时至少有一个 active production site；
- inactive/insolvent/liquidating firm 可保留场所，closed firm 不得有 active site；
- 首版 opening/upgrade 为每个已有 firm 建立总部和生产场所。

### 3.2 用途兼容

场所只能引用 active building、active parcel 和同用途 active pool：

| site type | allowed pool use |
| --- | --- |
| headquarters | commercial |
| office | commercial |
| retail | commercial |
| production | industrial |
| warehouse | industrial |

混合用途必须在未来土地规则版本中显式增加，不能靠 metadata 绕过。

### 3.3 容量与占用

```text
effective_pool_units = baseline_pool.unit_count
                     + completed_adjustment.added_capacity_units
                       / capacity_units_per_unit

enterprise_occupied = sum(active_site.occupied_units)
available            = effective_pool_units - residential_occupied
                     - enterprise_occupied
```

商业/工业池的 residential occupied 必须为零。任何命令都需在 `FOR UPDATE` 锁下重新计算 available，避免两个并发开设命令超卖同一池。

### 3.4 首次放置算法

创世或 v3→v4 升级按 firm code 排序。对每个 firm：

1. 在 firm district 内按 pool code 选择首个有足够空间的 commercial pool 作为总部；
2. 按 pool code 选择首个有足够空间的 industrial pool 作为生产场所；
3. 多个 firm 顺序扣减临时可用容量；
4. 任何 firm 无法完整放置时，整个创世/升级失败，不生成部分基线；
5. 基线 site code 固定为 `site_<firm_code>_headquarters` 与 `site_<firm_code>_production`。

算法只读取冻结的 firm state、有效 land state 和绑定政策，不读取数据库 ID 或无序查询结果。

## 4. 状态机

单个场所：

```text
site.open
   │
   ▼
 active ── site.resize ──► active (version + 1)
   │
   └────── site.close ───► closed
```

整组迁址：

```text
old primary sites active
   │ enterprise.relocate
   ├─ transfer all non-zero firm inventory
   ├─ close old headquarters/production sites
   ├─ open new headquarters/production sites
   └─ change firm primary district
                 ▼
new primary sites active
```

closed site 永不重开或删除；重新使用同一建筑必须创建新 site code。调整只允许 active → active，目标占用必须为正。

## 5. 命令协议

### 5.1 `enterprise.site.open`

```json
{
  "firm_entity_id": 42,
  "pool_code": "pool_building_central_p0_p0_ne",
  "site_type": "warehouse",
  "name": "Central Materials Depot",
  "target_occupied_units": 20
}
```

服务器分配 `enterprise_site_<command-sequence>`。`target_occupied_units` 可省略，此时使用政策最低值；提供时必须不低于最低值。

### 5.2 `enterprise.site.resize`

```json
{
  "site_code": "enterprise_site_17",
  "target_occupied_units": 32
}
```

只能调整 active site；不能把总部/生产场所缩到政策最低值以下。

### 5.3 `enterprise.site.close`

```json
{
  "site_code": "enterprise_site_17",
  "reason": "Consolidated into the primary plant"
}
```

不能直接关闭 active primary site；总部或 primary production 必须由迁址命令原子替换。非 primary production 在生产能力为正时也不能关闭到零个。

### 5.4 `enterprise.relocate`

```json
{
  "firm_entity_id": 42,
  "headquarters_pool_code": "pool_building_north_p1_p0_ne",
  "production_pool_code": "pool_building_north_p1_p0_se",
  "reason": "Move closer to the northern labor market"
}
```

两个池必须属于同一且不同于当前主运营区的目标 district；跨区迁址必须搬迁全部非零企业库存。区内场所整合使用 `site.open/resize/close`，不伪造 firm district 变更。存在 under-construction 项目且该企业是 developer 时拒绝迁址，避免施工责任和劳动力 district 在中途漂移。

## 6. 稳定业务拒绝码

| code | 语义 |
| --- | --- |
| `CITY_ENTERPRISE_FIRM_NOT_FOUND` | firm 不存在、非 active 或无 firm state |
| `CITY_ENTERPRISE_SITE_NOT_FOUND` | site 不存在或不属于世界 |
| `CITY_ENTERPRISE_POOL_NOT_FOUND` | pool/building/parcel 不存在或非 active |
| `CITY_ENTERPRISE_USE_INCOMPATIBLE` | site type 与 pool use 不兼容 |
| `CITY_ENTERPRISE_CAPACITY_INSUFFICIENT` | 有效 pool 可用容量不足 |
| `CITY_ENTERPRISE_MINIMUM_CAPACITY` | 目标占用低于政策最低值 |
| `CITY_ENTERPRISE_SITE_LIMIT` | active site 数超过政策上限 |
| `CITY_ENTERPRISE_REQUIRED_SITE` | 操作会失去唯一总部或必要生产场所 |
| `CITY_ENTERPRISE_STATE_CONFLICT` | site 状态/version 与命令不兼容 |
| `CITY_ENTERPRISE_RELOCATION_CONFLICT` | 迁址与活动开发项目或同 tick 操作冲突 |
| `CITY_ENTERPRISE_INVENTORY_TRANSFER` | 库存无法守恒迁移 |

业务拒绝只拒绝当前命令；数据库错误、不变量失败和 canonical hash 漂移仍中止整个 tick。

## 7. 数据模型

### 7.1 `city_enterprise_location_profiles`

每世界一行：policy ID/version/hash、baseline tick/hash、site/fact 计数和 revision。revision 每个 posted fact 增长一次。

### 7.2 `city_enterprise_sites`

当前投影字段：

- world、stable code、firm、district、building、pool；
- site type、name、occupied units；
- `is_primary`（总部和 primary production）；
- status、opened/updated/closed tick、version；
- metadata。

状态字段只可由 location reducer 在 tick/upgrade/recovery gate 中改变；禁止物理删除。

### 7.3 `city_enterprise_location_facts`

不可变事实头：world、tick、sequence、source command、firm、site code、fact type、from/to status、occupied before/after、site version before/after、metadata、posted_at。

fact type：`opened`、`resized`、`closed`、`relocated`。`relocated` metadata 固化排序后的 before/after primary site 列表、旧/新 district 和库存 resource operation 游标。

只允许 draft → posted；posted 后更新/删除禁止。同 tick `(world, sequence)` 唯一。

### 7.4 `city_enterprise_location_baselines`

创世或 v3→v4 升级时保存：tick、policy hash、baseline hash、site count 和规范化初始 sites。基线不可更新/删除。

## 8. 数据库硬约束

提交前 assertion 至少验证：

1. 只有 `city-f7-v4` 世界可存在 enterprise location 数据；
2. profile/baseline policy、tick、hash 和计数一致；
3. 每个 site 的 firm/district/building/pool 同世界且用途兼容；
4. active site occupied units 为正，closed site 有 closed tick；
5. 每个 active firm 恰有一个 active headquarters；
6. 有生产能力的 active firm 至少一个 active production site；
7. firm primary district 等于 active headquarters district；
8. 每个 pool 的 active site 占用不超过 F7.4 effective supply；
9. 商业/工业 building effective occupied 等于其 active sites 汇总；
10. fact 链状态、occupied、version 和 source command 连续；
11. relocated fact 的库存 operations 覆盖旧 district 全部非零 firm balance，前后资源总量守恒；
12. profile site/fact/revision 计数与真实投影一致；
13. site/fact/profile/baseline 和受管 firm district 写入只允许 tick、升级或 verified recovery gate。

## 9. Tick 与重放顺序

`city-f7-v4` 引擎顺序：

```text
control
→ ledger
→ resources
→ calendar_demography
→ spatial
→ development
→ enterprise_location
→ markets
```

location command 按全局 command sequence 执行。开设/调整/关闭只写 location fact；跨区迁址先创建 draft location fact，再按稳定 resource code 在一笔多资源 transfer operation 内发布全部出入配对，最后密封 location fact。resource operation sequence 与 location fact sequence 分别在 tick 内连续。

Replay 时 resources stage 已根据 operation 重建库存；enterprise_location stage 再归约 site、firm district 和有效占用，因此与在线事务得到相同 canonical hash。

## 10. 规范状态、升级与恢复

`city-f7-v4` 在 development 后增加：

```json
{
  "enterprise_location": {
    "profile": {},
    "sites": [],
    "facts": []
  }
}
```

排序：site code；fact `(tick, sequence)`。不包含数据库 ID、时间戳、本地化文案或派生 available capacity。

### 10.1 v3→v4 升级

- dry-run 只计算 policy/baseline/canonical hash 和放置计划，不落库；
- apply 锁世界，确认仍为同 tick、同 v3 hash；
- 运行首次放置算法，写 profile/baseline/sites；
- `city_firm_states` 聚合值和全部 F7.3/F7.4 数据保持不变；
- 同 tick 写 v4 upgrade snapshot 和审计证据；
- 任一企业无法放置则完整回滚。

### 10.2 Recovery

恢复顺序：清除 enterprise facts/sites/profile/baseline，恢复 land/development 后按 snapshot 精确恢复 location 投影，再恢复 firm district 与库存投影，运行全部 assertion。重载 canonical 必须逐字节一致。

## 11. API 与前端

读取：

```text
GET /api/v1/city/worlds/:world_id/enterprise-locations
  ?firm_code=&district_code=&site_type=&status=
  &after_tick=&after_sequence=&limit=
```

返回 profile、sites、事实页、按 pool 聚合的 occupied/available、firm 场所摘要和游标。成员只读；owner 通过统一 command endpoint 修改。

CLASSIC 前端：

- Overmap tile 显示 active firm/site 数与 commercial/industrial 占用率；
- Local building glyph 使用轻量场所叠层，不覆盖地形和施工标记语义；
- Inspector 显示企业、site type、占用/有效容量、总部/primary 状态和事实时间线；
- 企业场所面板提供 district/use/status Tabs、池容量比较、开设/调整/关闭/迁址表单；
- mutation 只刷新相关 query/store，不替换整页内容或闪烁地图；
- 所有显示值来自 API，不写 demo 场所和伪造占用。

## 12. 依赖实施顺序

1. 冻结 policy canonical/hash、命令 payload、fact metadata 和业务拒绝码；
2. migration：profile/site/fact/baseline、索引、写闸门和 assertion；
3. 首次放置纯函数与黄金测试；
4. v4 genesis、v3→v4 dry-run/apply 与双快照；
5. open/resize/close reducer；
6. relocate、库存守恒 transfer 和 firm district 原子切换；
7. canonical、逐 tick replay、漂移诊断和 verified recovery；
8. 查询 API、授权、过滤和游标；
9. effective land occupancy、CLASSIC 叠层、inspector 和操作面板；
10. PostgreSQL 端到端门禁和全量回归。

## 13. 验收门禁

1. 首次放置跨数据库 ID 确定，且不会超卖 commercial/industrial pool；
2. open/resize/close 严格规范化、幂等并保持必需场所；
3. 并发开设到同一 pool 只有容量允许的命令成功；
4. 跨区迁址的 sites、firm district 和全部非零库存同事务切换；
5. 迁址 resource operations 总入等于总出，失败无部分写入；
6. effective pool/building occupancy 与 active site 汇总一致；
7. v3 历史 canonical 不变，v3→v4 dry-run/apply 原子且可审计；
8. v4 genesis、snapshot、逐 tick replay、漂移诊断和 verified recovery 闭环；
9. profile/site/fact/baseline/firm district 直接写入被数据库拒绝；
10. API 授权、过滤、稳定游标和上限测试通过；
11. CLASSIC 只消费真实事实，操作不触发整页刷新闪烁；
12. 全量 Go、迁移测试、PostgreSQL integration、前端 test/typecheck/build 通过。

F7.5 已通过对应实现门禁并发布 `city-f7-v4`。下一阶段先建设不固化城市玩法的开放世界 Actor/Rule/Effect 运行时；租约价格、产权和企业创建仍不得反向污染本版本协议。
