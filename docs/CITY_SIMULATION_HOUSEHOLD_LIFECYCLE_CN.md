# 城市模拟 F6.3 家庭生命周期与收入层迁移详细设计

版本：v1.1（2026-07-18）  
目标引擎：`city-f6-v3`  
状态：已实现并通过单元、数据库集成、重放与恢复验收

## 1. 目标与边界

F6.3 修复现有模型把“人口数”近似当作“家庭数/住房需求”的语义问题，并建立家庭数量与收入层变化的可审计底座。

本阶段完成：

1. `population_units` 与 `household_units` 分离；住房市场按家庭而不是按人口表达需求。
2. 家庭形成、拆分、合并、解体使用不可变 household movement 表达。
3. 同一区域相邻收入层之间可迁移一组完整家庭，并同步人口年龄、就业、家庭和住房占用投影。
4. 命令、事实、规范状态、版本升级、快照、重放、恢复和读取 API 闭环。
5. 极端人口自然减少时，系统生成可追溯的家庭解体事实，避免家庭数超过人口数。

本阶段不实现：

- 单个家庭、婚姻、亲属或个人收入档案；
- 自动社会流动评分器；
- 家庭级独立银行账户、贷款或证券；
- 地块、住宅单元或租赁合同；
- 直接向外部用户发放虚拟货币奖励。

这些能力只能在本阶段守恒和回放稳定后追加，不能旁路修改聚合投影。

## 2. 核心语义

每个区域和收入层仍有一个固定的 household cohort。cohort 是聚合统计桶，不是假装存在的单一家庭。

```text
population_units       人数
working_age_units      劳动年龄人数
employed_units         已就业人数
household_units        家庭数
housing_demand_units   需要住房的家庭数；F6.3 中恒等于 household_units
occupied_units         已占用住房的家庭数
unmet_units            尚未获得住房的家庭数
```

必须始终满足：

```text
0 < household_units <= population_units
housing_demand_units = household_units
0 <= occupied_units <= household_units
unmet_units = household_units - occupied_units
0 <= employed_units <= working_age_units <= population_units
```

平均家庭规模只作为派生读模型返回：

```text
average_household_size_milli = floor(population_units * 1000 / household_units)
```

它不是可直接写入的状态，避免与人口、家庭数量发生双向漂移。

## 3. 引擎版本与能力边界

新增 `city-f6-v3`，阶段顺序保持不变：

```text
control → ledger → resources → calendar_demography → markets
```

能力增加 `household_lifecycle`。版本规则：

- `city-f6-v1`：自然人口变化；
- `city-f6-v2`：在 v1 上增加人口迁入、迁出和区域迁移；
- `city-f6-v3`：在 v2 上增加独立家庭数量和收入层迁移；
- v2 的人口迁移命令在 v3 中继续可用；
- household 命令只允许提交给 v3 世界；
- 只增加 `city-f6-v2 → city-f6-v3` 的单向显式升级路径。

世界必须暂停、操作者必须是 owner、待处理命令必须为空、源快照必须匹配，才能 dry-run 或 apply。升级失败回滚到 savepoint，成功时在原 tick 创建 v3 基线快照。

## 4. v2 → v3 数据升级

### 4.1 初始家庭数

每个 cohort 的初始家庭数使用确定性整数规则：

```text
estimated = ceil(population_units / 3)
household_units = max(1, occupied_units, estimated)
```

由于 v2 已保证 `occupied_units <= housing_demand_units <= population_units`，升级结果不会超过人口数。

### 4.2 住房需求修正

```text
housing_demand_units = household_units
occupied_units       = occupied_units_before
unmet_units           = household_units - occupied_units
```

升级不创建或销毁 `housing_units` 库存，也不修改租金、现金、账户、就业或人口。cohort 与 occupancy 版本各推进一次，使投影变化可被哈希和恢复识别。

### 4.3 旧版本兼容

数据库可以为旧世界保存惰性的 `household_units` 列，但 F5、F6 v1 和 F6 v2 的规范 JSON 必须继续省略该字段。旧快照解码后 `household_units=0` 表示“该规范版本没有该语义”，恢复旧快照时不得把数据库列写成零。

## 5. 命令协议

所有对象采用严格 JSON 解码：拒绝未知字段、重复键、尾随值、越界整数和非规范 scope。区域与收入层去空白后转为小写；reason 去首尾空白，最多 120 个 Unicode 字符。

### 5.1 `household.adjust`

```json
{
  "district_code": "central",
  "income_band": "middle",
  "movement_type": "split",
  "household_units": 12,
  "reason": "adult children formed independent households"
}
```

`movement_type` 只允许：

| 类型 | 方向 | 语义 |
|---|---:|---|
| `formation` | 增加 | 新家庭形成 |
| `split` | 增加 | 现有家庭拆分 |
| `merge` | 减少 | 多个家庭合并 |
| `dissolution` | 减少 | 家庭解体 |

`household_units` 是正数，不使用带符号输入。形成/拆分增加家庭和未满足住房需求；合并/解体先减少未满足需求，仍不足时释放已占住房：

```text
after_households = before_households ± requested_households
after_occupied   = min(before_occupied, after_households)
after_unmet      = after_households - after_occupied
```

结果必须至少保留一个家庭，并且家庭数不能超过人口数。

### 5.2 `household.reclassify`

```json
{
  "district_code": "central",
  "source_income_band": "low",
  "target_income_band": "middle",
  "child_units": 8,
  "working_units": 18,
  "senior_units": 2,
  "employed_units": 14,
  "household_units": 10,
  "occupied_units": 7,
  "reason": "sustained real-income growth"
}
```

约束：

1. 只能在同一区域迁移；
2. 只允许 `low ↔ middle ↔ high` 相邻移动，不能跨级；
3. 人口总数和家庭数必须为正；
4. `employed_units <= working_units`；
5. `occupied_units <= household_units <= child+working+senior`；
6. 来源具有足够的各年龄人口、就业、家庭和占住房；
7. 来源迁移后仍至少保留一个人口和一个家庭；
8. 来源和目标迁移后均满足全部 cohort 与 occupancy 不变量；
9. 来源与目标必须由同一聚合 household economic entity 控制。

第 9 条是当前账户模型的诚实边界。F6.3 不移动账户余额，也不创建现金；收入层变化通过人口进入新的 income band 自动改变后续需求参数。未来只有在家庭主体拆分为独立经济实体后，才增加账户控制权转移 journal。

## 6. 不可变事实

### 6.1 `city_household_movements`

header 保存：

- 世界、tick、household sequence；
- `origin = command | demography_guard`；
- 可空的源命令；
- movement type；
- source/target cohort；
- 年龄、就业、家庭和占住房汇总数量；
- 期望 line 数、规范 metadata、posted 时间。

形状：

- formation/split：source 为空、target 非空、1 条 inflow line；
- merge/dissolution：source 非空、target 为空、1 条 outflow line；
- income_reclassification：source/target 都存在且不同，2 条相同数量的 outflow/inflow line；
- demography_guard 只能产生 dissolution，且没有源命令。

### 6.2 `city_household_movement_lines`

line 保存每个受影响 cohort 的：

- 稳定 line 顺序和方向；
- demographic、cohort、occupancy 的 before/after version；
- 各年龄人口、就业、家庭、占住房和未满足住房的 before/after 值；
- 本 line 流入或流出的汇总数量。

家庭数量调整不修改 demographic：其 demographic version 与年龄 before/after 相等。收入层迁移同时更新三个投影：三个 version 都严格 `+1`。

header 只允许一次 `draft → posted`，line 一经插入不可更新或删除。延迟约束在事务提交前验证命令状态、line 数、方向、scope、汇总守恒和 before/after 方程。

## 7. 写入门与事务顺序

投影只能在以下可验证上下文写入：

1. F4 市场结算；
2. F6.1 自然人口 movement；
3. F6.2 人口 migration；
4. F6.3 draft household movement；
5. 已登记且 running 的引擎升级；
6. verified replay 驱动的恢复。

每条 household 命令使用 savepoint。业务拒绝只把命令记为 rejected，不回滚同 tick 的其他合法命令；数据库、版本链或守恒错误回滚整个 tick。

同一 tick 的 `calendar_demography` 顺序：

```text
按 command.sequence 交错执行人口 migration 与人工 household movement
→ 日历边界
→ 月度自然人口 movement
→ 必要的 demography_guard household dissolution
→ 市场结算
```

人工事实按源命令顺序回放；系统解体事实在自然人口变化之后回放。因此，即使两类事实各自使用局部 sequence，也不会丢失跨类型因果顺序。

## 8. 极端人口减少保护

正常人口变化不会接近家庭下限，但规则必须对极端参数仍然封闭。

v3 的自然人口计算允许人口暂时低于当前家庭数；同一事务随后对每个超限 cohort 生成 `origin=demography_guard`、`movement_type=dissolution` 的系统事实：

```text
required_dissolution = household_units - population_units
```

系统解体沿用“先减少 unmet、再释放 occupied”的规则。事务提交前恢复 `household_units <= population_units`。F5/F6 v1/v2 保持原有“人口不得低于住房需求”的计算规则和历史哈希。

## 9. 规范状态、哈希与恢复

v3 的 household cohort 规范状态增加：

```json
"household_units": 420
```

恢复映射同步恢复 household count、housing demand 和 occupancy。所有 v3 tick snapshot 都包含该字段；v2→v3 升级在同 tick 产生新版本 baseline，旧版本 snapshot 不被覆盖。

重放验证：

1. 人工 household movement 与人口 migration 按源命令顺序归约；
2. 日历和自然人口 movement 随后归约；
3. system household movement 最后归约；
4. 每条 line 的 before version 和绝对 before 状态必须命中当前归约状态；
5. 归约后重新执行 household projection invariant；
6. 逐 tick 规范 hash 必须与保存的 tick hash 相同。

verified recovery 使用目标规范状态恢复 projection，并由数据库写入门和延迟约束复核；不能直接恢复事实表。

## 10. 读取 API

现有物理状态接口扩展：

```text
GET /api/v1/city/worlds/:world_id/state
```

每个 household cohort 增加 `household_units` 和 `average_household_size_milli`。

新增：

```text
GET /api/v1/city/worlds/:world_id/household-movements
GET /api/v1/city/worlds/:world_id/household-movements/:tick/:sequence
```

列表使用 `(after_tick, after_sequence)` 升序游标，默认 50、最大 200。成员可读，非成员得到与不存在世界相同的响应。命令继续通过通用 command endpoint 提交。

## 11. 业务拒绝码

| 错误码 | 含义 |
|---|---|
| `CITY_HOUSEHOLD_SCOPE_NOT_FOUND` | 区域或收入层 cohort 不存在 |
| `CITY_HOUSEHOLD_POPULATION_FLOOR` | 结果人口/家庭关系非法 |
| `CITY_HOUSEHOLD_EMPLOYMENT_FLOOR` | 来源就业或劳动年龄约束不足 |
| `CITY_HOUSEHOLD_OCCUPANCY_FLOOR` | 来源占住房数量不足 |
| `CITY_HOUSEHOLD_RECLASSIFICATION_NON_ADJACENT` | 收入层跨级或未变化 |
| `CITY_HOUSEHOLD_ENTITY_BOUNDARY` | 当前账户控制模型不支持跨经济实体迁移 |

格式、未知字段和越界在命令提交时返回 `CITY_INVALID_INPUT`；版本不支持在提交时返回版本错误；投影链损坏属于 simulation invariant，不能降级成业务拒绝。

## 12. 验收门禁

### 12.1 纯函数与命令

- 四种家庭数量 movement 的正负方向、边界、溢出和规范化测试；
- 收入层相邻关系、年龄、就业、家庭和住房约束测试；
- 相同语义 payload 得到相同规范 payload 和幂等指纹；
- 未知字段、重复键、尾随 JSON 和非法 reason 被拒绝。

### 12.2 数据库

- 直接更新 cohort、demographic 或 occupancy 被拒绝；
- draft 未 posted、line 缺失、方向错误、汇总不平和版本不连续在 commit 被拒绝；
- facts/lines 不能更新或删除；
- 并发相同 step 只生成一组事实；
- v2→v3 dry-run、失败注入和 apply 均保持原子审计。

### 12.3 守恒与重放

- 家庭数量调整不改变人口、就业或现金；
- 收入层迁移逐年龄、就业、家庭、occupied 和 unmet 全区守恒；
- 迁移不创建 journal，不改变账户余额；
- 0→N 和中间 baseline→N 重放 hash 一致；
- 投影漂移可被发现并由 verified recovery 恢复；
- v1/v2 旧快照 canonical bytes 与既有黄金值不变；
- 长周期极端人口参数会生成 system dissolution，而不是提交非法投影或静默截断。

## 13. 后续依赖

F6.3 通过后，F7 才可以把家庭数映射为住宅需求、建筑单元占用和可见居民代理。空间层不得重新从人口数猜测家庭数。

后续自动社会流动必须使用至少一个完整观察窗口的版本化指标（实际收入、就业稳定性、资产和住房负担），只生成下一 tick 命令，不能读取同 tick 未封账结果。家庭级经济实体、账户控制转移、贷款和证券继续作为独立版本扩展。

## 14. 实施结果

- 数据迁移已增加 household 投影、movement/line 事实、写闸门、延迟约束、初始化器、`city-f6-v3` 目录和 v2→v3 升级路径。
- 正向 tick 已按源命令序列交错处理 population migration 与 household movement，并在自然人口变化后执行系统家庭下限修复。
- v3 规范状态包含 `household_units`；F5/F6 v1/v2 继续省略该字段，旧规范字节和历史快照语义不变。
- 列表/详情 API、逐 tick reducer、verified recovery、直接写入保护、并发幂等和 48 tick 场景均已覆盖。
- F7.0 已开始只消费“家庭数”语义；后续空间层不得退回按人口估算家庭数量。
