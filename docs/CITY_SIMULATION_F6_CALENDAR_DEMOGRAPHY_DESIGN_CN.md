# 城市模拟 F6.1：日历边界与人口自然变化详细设计

版本：v1.0（2026-07-18）  
实现版本：`city-f6-v1`  
状态：已实现并通过 F0–F6.1 组合集成验证  
前置：F0 世界与主体、F1 确定性 tick、F2 总账、F3 实物、F4 市场、F5 快照/重放/恢复

## 1. 结论与范围

F6.1 是城市本体层的第一个正式垂直切片。它解决两个基础问题：世界如何从 UTC 小时 tick 得到唯一、可审计的本地日历边界，以及人口如何在不使用浮点、不直接改聚合数字的前提下长期发生出生、死亡和年龄迁移。

本阶段已经闭合以下链路：

```text
世界模拟时钟
  → 本地日/月/季度/年边界事实
  → 月度自然变化 movement/line
  → 日历、年龄结构和家庭人口投影
  → 规范状态哈希与版本化快照
  → 逐 tick 事实重放
  → verified replay 驱动的投影恢复
  → 成员读取 API 与长期黄金测试
```

本阶段明确不包含：

- 城市外部迁入、迁出和区域间迁移。
- 家庭形成、拆分、收入带迁移和个体居民。
- 生育、死亡或迁移的玩家直接设置接口。
- 利率、银行信贷、存款创造、股票、订单或证券持仓。
- 自动 tick 调度、可玩前端和平台虚拟货币奖励。

这些能力必须分别通过 F6.2、F6.3、F7–F13 或 E1/E2 的新版本化事实接入，不能修改 F6.1 历史。

## 2. 设计原则

1. **UTC 驱动，本地解释。** 基础 tick 固定一小时；世界 IANA 时区只用于判断本地日期边界。
2. **边界是事实。** 日、月、季度和年边界写入不可变表，不靠查询时临时推断。
3. **人口变化是过账。** 自然变化先形成 draft movement，再更新投影和写 line，最后一次封账。
4. **总量与结构同时守恒。** 年龄迁移只改变年龄结构，不改变总人口；出生和死亡显式改变总量。
5. **整数和余数。** 年率使用 ppm，月度除法余数进入下一期，不使用 `float64` 或随机四舍五入。
6. **稳定业务键。** 哈希、重放和恢复按区域代码、家庭主体代码和收入带定位，不依赖数据库 ID。
7. **F5 同步接入。** 新投影没有不可变事实、规范哈希、重放 reducer 和恢复 mapper 就不能进入正式 tick。

## 3. 日历语义

### 3.1 基础时钟

- 每个显式 step 固定推进一个 UTC 小时。
- 世界保存 `simulated_at` 和 IANA `timezone`。
- 创建世界默认从 `2000-01-01T00:00:00Z` 开始。
- 墙钟 `NOW()` 只记录事实创建时间和执行耗时，不参与边界、人口计算或状态哈希。

### 3.2 边界判定

对 tick 的 `simulated_from`、`simulated_to`：

1. 在世界时区中分别取本地日期。
2. 日期相同则本 tick 无日历事实。
3. 日期必须相邻；若一次推进跳过本地日期，整个 tick 以确定性不变量错误回滚。
4. 跨日固定先写 `day`。
5. 月份变化再写 `month`。
6. 季度变化再写 `quarter`。
7. 年份变化最后写 `year`。

季度为自然季度：1–3、4–6、7–9、10–12 月。跨年必然同时产生日、月、季度和年四条边界。事件顺序与事实顺序一致：

```text
city.calendar.day_started
city.calendar.month_started       # 条件产生
city.calendar.quarter_started     # 条件产生
city.calendar.year_started        # 条件产生
city.population.natural_change_posted  # 月边界产生
```

夏令时只会使本地小时跳跃或重复，不会改变 UTC tick 长度。只要本地日期没有变化就不产生边界；正常跨午夜仍只产生一次日边界。

### 3.3 日历投影

`city_calendar_states` 每世界一行，保存：

- `local_date`。
- `day_index`、`month_index`、`quarter_index`、`year_index`。
- `last_daily_tick`、`last_monthly_tick`、`last_quarterly_tick`、`last_annual_tick`。
- `version` 和 JSON object 元数据。

投影只有跨日时版本加一。边界事实使用 `(world_id, tick, sequence)` 排序，并以 `(world_id, boundary_type, local_date)` 防止同一本地边界重复结算。

## 4. 人口模型

### 4.1 聚合层级

F3 已有 6 个区域，每区低、中、高收入三个家庭 cohort，共 18 个。F6.1 不创建第二套家庭身份，而是为每个既有 cohort 增加年龄结构投影：

- `child_units`：儿童人口。
- `working_units`：劳动年龄人口。
- `senior_units`：老年人口。

三者之和必须等于 `city_household_cohorts.population_units`；`working_units` 必须等于其 `working_age_units`。就业仍由 F4 劳动市场决定，且必须满足 `employed_units <= working_units`。

升级初始化时，既有劳动年龄人口保持不变；非劳动年龄人口按固定 4:3 拆为儿童和老年，整数余数归老年。初始化只建立当前投影和新版本基线，不伪造旧月份的人口历史。

### 4.2 版本化政策

`city_demographic_policies` 当前默认参数包：

| 参数 | 默认值 | 含义 |
| --- | ---: | --- |
| `parameter_set_code` | `baseline_v1` | 参数包稳定代码 |
| `parameter_version` | 1 | 参数版本 |
| `periods_per_year` | 12 | 每年自然变化结算次数 |
| `birth_rate_ppm` | 12,000 | 总人口年出生率 |
| `child_death_rate_ppm` | 500 | 儿童年死亡率 |
| `working_death_rate_ppm` | 1,000 | 劳动年龄年死亡率 |
| `senior_death_rate_ppm` | 12,000 | 老年年死亡率 |
| `child_to_working_rate_ppm` | 55,000 | 儿童进入劳动年龄年率 |
| `working_to_senior_rate_ppm` | 22,000 | 劳动年龄进入老年年率 |

当前没有原地修改政策的 API。未来政策变更必须通过命令创建新参数版本，并从未来边界生效；已封账月份继续引用当时的代码和版本。

### 4.3 余数累计

每个 cohort 为出生、三类死亡和两类年龄迁移保存六个余数。单期计算为：

```text
denominator = periods_per_year × 1_000_000
numerator   = source_units × annual_rate_ppm + remainder_before
units       = floor(numerator / denominator)
remainder_after = numerator mod denominator
```

所有乘法和加法检查 `int64` 溢出。余数必须处于 `[0, denominator)`。这使低人口 cohort 的小数概率跨月累积，120 个月结果不会因每月独立四舍五入而系统性丢失。

### 4.4 单 cohort 方程

```text
child_after = child_before
            + births
            - child_deaths
            - child_to_working

working_after = working_before
              + child_to_working
              - working_deaths
              - working_to_senior

senior_after = senior_before
             + working_to_senior
             - senior_deaths
```

因此：

```text
population_after = population_before + births - all_deaths
citywide_age_transition_net = 0
```

任何来源流出不得超过来源年龄组；期末劳动年龄人口不得低于已就业人口，期末总人口不得低于既有住房需求，且总人口必须为正。

## 5. 不可变人口事实

### 5.1 `city_population_movements`

每个跨月 tick 恰有一条 `natural_change` movement：

- 世界、tick、sequence 和对应月边界。
- 本地月份第一天。
- 参数包代码和版本。
- 预期 line 数。
- 出生、死亡、年龄迁移总数。
- `posted_at` 和规范元数据。

同世界同月份只能结算一次。movement 只允许一次 `draft → posted`；封账后更新或删除均被数据库拒绝。

### 5.2 `city_population_movement_lines`

每个 cohort 一条 line，保存：

- cohort 身份及 line number。
- demographic/household 投影前后版本。
- 出生、三类死亡和两类年龄迁移数量。
- 三个年龄组前后数量。
- 六类余数前后值。

line 在插入后不可更新或删除。数据库 check 直接验证三个年龄方程和版本连续性；延迟 constraint trigger 在提交前复核 line 数和 movement 总计。

## 6. Tick 事务顺序

F6.1 接入后的关键顺序为：

```text
advisory lock + world row lock
  → 校验当前版本快照与在线投影
  → 读取并执行命令
  → 计算本地日期变化
  → 写 calendar boundary facts
  → 更新 calendar projection
  → 月边界：计算全部 cohort 计划
  → 写 draft population movement
  → 更新 demographic + household projections
  → 写 immutable movement lines
  → movement 封账
  → 到期时执行 F4 市场结算
  → 更新世界 tick/模拟时间
  → 计算规范状态与哈希
  → 写 tick、事件和同版本快照
  → 强制延迟约束并一次提交
```

人口自然变化在 F4 市场之前发生，因此月初市场读取新的劳动年龄人口；就业存量不能超过自然变化后的劳动年龄人口，否则 tick 整体失败，不允许静默裁剪就业。

## 7. 数据库写保护

F6.1 采用绑定事实 ID 的事务局部写闸门：

- 日历投影只能在当前事务存在对应 `day` boundary 时推进，或由 running recovery 恢复。
- demographic cohort 只能在对应未封账 movement 存在时推进，或由 running recovery 恢复。
- household cohort 的人口/劳动年龄字段只能由 F6 movement 更新；就业由 F4 settlement 更新。
- 边界、封账 movement 和 line 均不可更新或删除。
- demographic 与 household 的 1:1 数量和人口等式由延迟触发器在事务提交时复核。

仅设置 GUC 名称无法绕过保护；数据库函数还会核对事实 ID、世界 ID、事实状态和 recovery 运行状态。

## 8. 规范状态、快照与版本升级

`cityHashState.demography` 包含：

- 日历日期、四级序号、最近边界 tick、版本和元数据。
- 人口参数代码、版本、全部 ppm、投影版本和元数据。
- 按区域顺序和收入带顺序排列的 18 个年龄结构、余数、版本和元数据。

规范状态不包含人口表 ID、cohort ID、movement ID、平台用户 ID、创建时间或更新时间。同 seed、同参数、同命令和同起始时间的世界即使数据库 ID 不同，也得到相同哈希。

F6 把快照唯一键升级为 `(world_id, tick, simulation_version)`：

- 原 `city-f5-v1` 快照原样保留。
- 世界升级为 `city-f6-v1` 后可在同 tick 建立 F6 基线。
- 正常 API、重放和恢复只选择世界当前版本。
- 完整性验证仍能按 F5 原字段顺序解码旧快照。

跨版本不直接重放。升级点是明确的新基线；未来每次人口规则或规范字段变化都必须再次升级模拟版本。

## 9. 重放与恢复

### 9.1 重放

每 tick 在 F2/F3 事实之后、F4 市场事实之前执行 F6 reducer：

1. 按 sequence 读取 boundary。
2. 验证日期连续、顺序、边界类型和序号。
3. 月边界必须恰有一条已封账自然变化 movement；非月边界不得有人口 movement。
4. 按 line number 验证 cohort 业务键、前版本、前状态、六类余数和家庭投影。
5. 使用快照中的版本化政策和前期余数重新计算出生、死亡、年龄迁移及期末余数，拒绝只满足等式但不符合规则的伪造事实。
6. 重新验证三个年龄方程、余数范围、就业和住房约束。
7. 归约年龄结构、家庭人口/劳动年龄和版本。
8. 复核 movement 总计。
9. 生成规范字节并与 tick 和同版本快照比较。

重放不读取在线日历或人口投影作为下一步输入，因此人为投影漂移不会被继承。

### 9.2 恢复

只有目标为当前 tick 的 verified replay 可触发恢复。F6 mapper 按稳定业务键恢复：

- 日历投影。
- 人口政策投影。
- 18 个 demographic cohort 投影。
- 对应 household cohort 的人口和劳动年龄投影由 F3 mapper 同步恢复。

恢复后重新加载完整 F0–F6 状态，比较规范字节和目标哈希，并在释放 savepoint 前执行全部延迟约束。任何行缺失、身份损坏或守恒失败都会回滚所有投影修改，只保留 failed recovery 审计。

## 10. API 与权限

```text
GET /api/v1/city/worlds/:world_id/calendar
GET /api/v1/city/worlds/:world_id/population
GET /api/v1/city/worlds/:world_id/population-movements
GET /api/v1/city/worlds/:world_id/population-movements/:tick/:sequence
```

- 活跃成员可读；非成员统一返回世界不存在，避免探测世界。
- movement 列表使用 `(after_tick, after_sequence)` 升序游标，默认 50、最大 200。
- 列表返回 movement 头；详情返回完整 line。
- 非法 world/tick/sequence/page size 返回稳定输入错误。
- 不存在的详情返回 `CITY_POPULATION_MOVEMENT_NOT_FOUND`。
- 当前没有人口写 API；人口只能由 step 产生事实。

## 11. 验收与验证证据

### 11.1 纯函数

- ppm 余数跨 12 个月准确归并，不使用浮点。
- 120 个月人口方程、就业和住房约束持续成立。
- 非法来源流出、乘加溢出和就业超过劳动年龄人口被拒绝。
- UTC、Asia/Shanghai、America/New_York DST、闰日、季度末和年末边界正确。
- 从 2000 到 2010 的 87,672 个 UTC 小时得到 3,653 日、120 月、40 季度和 10 年边界。

### 11.2 数据库与服务

- 创世同时初始化日历、政策和 18 个 demographic cohort。
- 跨年单 tick 原子产生日/月/季度/年边界、自然变化和完整 line。
- 两个不同 owner/数据库 ID、相同 seed 的世界得到相同 PRNG 证明和状态哈希。
- population API、游标、详情和非成员授权正确。
- 直接更新日历、人口、家庭人口或历史事实被数据库拒绝。
- 从 tick 0 到跨年 tick 的重放为 verified。
- 人为漂移日历、政策、年龄结构和家庭人口后，verified replay 恢复原规范哈希。
- F0–F6.1 组合集成测试全部通过。

## 12. 运行与性能边界

单个跨月 tick 当前写入：最多 4 条边界、1 条 movement、18 条 movement line、18 条 demographic 更新和 18 条 household 更新，复杂度与 cohort 数线性相关。普通非边界小时不写人口事实。

上线观测至少包含：

- 边界类型计数、每月 movement/line 数和结算耗时。
- 出生、死亡、年龄迁移总量和人口增长率。
- cohort 人口、劳动年龄、就业约束失败数。
- 重放 tick/s、首个差异路径和恢复结果。
- 快照大小与 F6 demography 字段占比。

正式扩大 cohort 数前需测量月边界写放大、快照尺寸和重放吞吐；未压测前不承诺具体 VPS 世界容量。

## 13. 下一阶段 F6.2 入口条件

F6.2 只能在 F6.1 保持绿色后增加以下新事实：

- `migration_in`：城市外部人口进入，受住房接纳能力限制。
- `migration_out`：人口离开，不超过来源 cohort。
- `district_move`：城市内部区域迁移，全城净人口为零。

迁移评分必须只读取上一个已封账周期的住房、就业、实际收入、通勤、服务和环境指标，避免同 tick 循环。首个 F6.2 版本在 F7–F11 指标尚未完成时只能使用显式版本化的简化评分包；后续替换评分必须升级版本并保留旧 reducer。

F6.2 仍需完整重复 F6.1 的工程链：迁移与约束、事实与投影、规范哈希、快照、重放、恢复、权限 API、长期确定性和投影漂移测试。完成前不开始利率、信贷或股票分支。

## 14. 实现文件索引

- 数据库：`backend/migrations/193_city_calendar_demography_foundation.sql`
- 迁移结构测试：`backend/migrations/city_calendar_demography_migration_test.go`
- 日历/人口领域、归约和恢复：`backend/internal/service/city_demography.go`
- tick 与规范状态接入：`backend/internal/service/city_simulation.go`
- 快照、重放与恢复编排：`backend/internal/service/city_recovery.go`
- 用户 API：`backend/internal/handler/city_economy_handler.go`
- 路由：`backend/internal/server/routes/user.go`
- 长期纯函数测试：`backend/internal/service/city_demography_test.go`
- 垂直集成测试：`backend/internal/repository/city_calendar_demography_integration_test.go`
