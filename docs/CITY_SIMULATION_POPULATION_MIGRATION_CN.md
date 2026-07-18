# 城市模拟 F6.2 人口迁移设计与验收

版本：v1.0（2026-07-18）  
状态：已实现  
引擎版本：`city-f6-v2`  
前置版本：`city-f6-v1`

## 1. 阶段目标

F6.2 在既有日历、年龄结构和自然人口变化之上，增加三类可审计人口流动：

- 外部迁入：城市外人口进入指定区域和收入层 cohort；
- 外部迁出：指定 cohort 的人口离开城市；
- 区域迁移：同一收入层人口在两个区域之间转移。

所有流动都必须经过“命令 → 不可变迁移事实 → 版本化投影 → 规范哈希 → 快照”的单一状态机。客户端不能直接设置人口，数据库也不允许绕过草稿事实修改人口投影。

F6.2 不提前实现家庭形成、收入层变化、就业岗位随迁、住房合同或吸引力自动决策。这些模型需要独立事实和守恒规则，分别进入 F6.3 及后续版本。

## 2. 版本边界

`city-f6-v1` 保持原有语义，只接受自然人口变化；人口迁移命令会被版本能力检查拒绝。`city-f6-v2` 复用相同的阶段顺序，并在 `calendar_demography` 阶段增加人口迁移命令能力：

```text
control → ledger → resources → calendar_demography → markets
                                  │
                                  ├─ 已应用迁移命令（按命令序列）
                                  ├─ 本地日历边界
                                  └─ 跨月自然人口变化
```

迁移命令先于同 tick 的日历和自然变化执行。因此，跨月 tick 的自然变化以迁移后的 cohort 为期初状态；重放器按完全相同的顺序归约事实。

版本目录只追加 `city-f6-v2`，升级图只追加 `city-f6-v1 → city-f6-v2`。世界必须暂停、由所有者发起、没有待处理命令且源快照一致，才允许 dry-run 或 apply。升级失败回滚到 savepoint，并保留不可修改的失败审计；成功后在同一 tick 创建 v2 基线快照，不覆盖 v1 快照。

## 3. 命令契约

所有命令通过现有接口提交：

```text
POST /api/v1/city/worlds/:world_id/commands
```

### 3.1 外部迁入

命令类型：`population.immigrate`

```json
{
  "target_district_code": "north",
  "income_band": "low",
  "child_units": 6,
  "working_units": 8,
  "senior_units": 4,
  "reason": "regional inflow"
}
```

### 3.2 外部迁出

命令类型：`population.emigrate`

```json
{
  "source_district_code": "north",
  "income_band": "low",
  "child_units": 2,
  "working_units": 3,
  "senior_units": 1
}
```

### 3.3 区域迁移

命令类型：`population.relocate`

```json
{
  "source_district_code": "central",
  "target_district_code": "north",
  "income_band": "low",
  "child_units": 3,
  "working_units": 5,
  "senior_units": 2
}
```

区域迁移必须改变区域并保持收入层不变。收入层变化属于 F6.3，不能借区域迁移命令旁路实现。

### 3.4 输入规则

- 区域代码和收入层会去空白并转为小写；对象采用严格 JSON 解码，未知字段和重复键被拒绝。
- 年龄段人数必须是非负整数，三者总和必须在 `1..1,000,000,000` 内。
- `reason` 可省略，最多 256 个 Unicode 字符。
- 提交需要幂等键和可选的预期世界 tick；相同幂等键复用不同意图会冲突。

## 4. 不可变事实模型

### 4.1 `city_population_migrations`

每个已应用命令只生成一条 header：

- 世界、tick 和 tick 内迁移序列；
- 唯一源命令；
- `immigration`、`emigration` 或 `district_relocation`；
- 可空源 cohort、可空目标 cohort；
- 三个年龄段总量；
- 预期 line 数、元数据、封账时间。

数据库检查 header 形状：迁入只能有目标，迁出只能有源，区域迁移必须同时有不同的源和目标。事实通过延迟外键绑定同事务稍后写入的 tick。

### 4.2 `city_population_migration_lines`

每个受影响 cohort 写一条 line：

- `inflow` 或 `outflow`；
- 三个年龄段流量；
- 人口投影和家庭 cohort 的前后版本；
- 三个年龄段的前后值。

外部迁入和迁出各一条 line，区域迁移固定两条 line，且顺序为源流出、目标流入。数据库直接检查每个年龄段的前后等式以及两个投影版本均严格加一。

### 4.3 封账约束

事务提交前，延迟约束会重新验证：

1. header 已封账；
2. 唯一源命令已在相同 tick 应用，且命令类型与迁移类型一致；
3. 源、目标和收入层关系合法；
4. line 数、方向、cohort 和三类人口合计与 header 一致；
5. 区域迁移的流入和流出在每个年龄段完全相等。

header 只允许一次“草稿 → 已封账”转换；封账后的 header 和所有 line 都不可更新或删除。

## 5. 投影事务与守恒

同一世界的 tick 已由事务级 advisory lock 串行化。迁移命令还使用命令级 savepoint，使预期业务拒绝只拒绝该命令，不污染同 tick 的其他命令。

每条 line 在一个事务中完成：

1. 锁定世界全部 demographic/household cohort，按稳定顺序读取；
2. 根据方向用 `int64` 安全加减计算年龄段期末值；
3. 检查人口、就业和住房下界；
4. 写人口年龄投影，版本加一；
5. 写家庭总人口和劳动年龄投影，版本加一；
6. 写不可变 line；
7. 全部 line 成功后封账 header。

核心等式：

```text
迁入：P_after(age) = P_before(age) + flow(age)
迁出：P_after(age) = P_before(age) - flow(age)
区域迁移：Σdistrict P_after(age) = Σdistrict P_before(age)
城市总人口：P_after = P_before + immigration - emigration
```

任何加法溢出、源人口不足、迁出后 cohort 为空、劳动年龄人口低于已就业人数，或总人口低于既有住房需求，都会阻止投影写入。

## 6. F6.2 的明确不变量

- 人口年龄投影之和始终等于家庭 cohort 总人口。
- 劳动年龄人口始终等于家庭 cohort 的 `working_age_units`。
- `employed_units <= working_age_units`。
- `housing_demand_units <= population_units`。
- 区域迁移不改变城市总人口，也不改变任一年龄段总人口。
- 外部迁入只增加总人口；外部迁出只减少总人口。
- 迁移不修改出生、死亡和年龄转移的整数余数；余数继续归属原 cohort。
- 迁移不修改就业人数、住房需求、收入层或 demographic 元数据。
- 事实序列、投影版本和源命令必须形成连续链。

保留就业和住房下界意味着 F6.2 只能迁出当前未被就业/住房投影占用的安全余量。岗位随迁、失业重算、家庭拆分和住房腾退需要更高版本同时推进对应市场事实，不能在本版本隐式猜测。

## 7. 业务拒绝

预期业务错误会把命令标记为 `rejected`，tick 本身仍可提交：

| 错误码 | 含义 |
|---|---|
| `CITY_POPULATION_MIGRATION_SCOPE_NOT_FOUND` | 区域或收入层 cohort 不存在 |
| `CITY_POPULATION_MIGRATION_POPULATION_UNAVAILABLE` | 源年龄段人口不足或迁出后 cohort 为空 |
| `CITY_POPULATION_MIGRATION_EMPLOYMENT_FLOOR` | 迁出后劳动年龄人口低于已就业人数 |
| `CITY_POPULATION_MIGRATION_HOUSING_FLOOR` | 迁出后总人口低于住房需求 |

结构错误、越界单位和未知命令在提交阶段返回输入错误；数据库、版本链、溢出或投影链损坏属于模拟不变量失败，会回滚整个 tick 并写失败审计。

## 8. 读取 API

```text
GET /api/v1/city/worlds/:world_id/population-migrations
GET /api/v1/city/worlds/:world_id/population-migrations/:tick/:sequence
```

列表使用 `(after_tick, after_sequence)` 稳定游标，默认 50 条、最大 200 条。详情返回完整 line。接口沿用世界成员读取授权，不向非成员泄露世界结构。

单步结果增加 `population_migrations`，tick 完成事件增加 `population_migration_count`。每个成功命令分别发出：

- `city.population.immigrated`
- `city.population.emigrated`
- `city.population.relocated`

## 9. 规范状态、重放与恢复

F6.2 没有创建第二套人口投影。规范状态仍由 demographic cohort 与 household cohort 共同表达；`simulation_version = city-f6-v2` 使 v2 编码与 v1 编码明确分离。不可变迁移事实不是重复状态，而是从基线推导投影的输入。

逐 tick 重放顺序为：

1. 按迁移序列验证并归约 migration line；
2. 归约日历边界；
3. 如跨月，归约自然人口 movement；
4. 归约市场事实；
5. 重新编码规范状态并与 tick、快照哈希逐字节比较。

快照解码器显式支持 F5、F6 v1 和 F6 v2，避免“当前版本别名变化”破坏旧快照。恢复仍只接受 verified replay，并恢复同一套 demographic/household 投影；不复制或重写历史迁移事实。

## 10. 验收覆盖

已实现的自动化门禁包括：

- 三类命令规范化、严格 JSON、单位上下限和同区域拒绝；
- 年龄段加减、`int64` 溢出、就业下界和住房下界；
- 区域迁移逐年龄段守恒；
- header/line 形状、版本链、命令绑定、延迟封账与事实不可变；
- 同 tick 迁移后自然变化的顺序重放；
- 列表游标、详情和非成员授权；
- 命令幂等与两个并发客户端的同一步幂等；
- F6 v1 → v2 dry-run、失败注入、原子 apply 和目标基线快照；
- F5、F6 v1、F6 v2 快照兼容读取；
- 投影漂移后的 verified recovery；
- 48 tick 跨日场景、多次迁移和 1→48 完整事实重放；
- 全部城市集成测试、全仓 Go 测试和静态检查。

## 11. 后续扩展顺序

F6.2 为人口流动提供了稳定事实协议，但不宣称人口系统已经达到最终真实程度。下一阶段按以下顺序扩展：

1. F6.3 家庭形成、拆分、合并和收入层迁移；
2. 住房空置、租约、搬迁成本和住房需求随迁；
3. 就业状态与岗位随迁、通勤和失业重算；
4. 基于工资、住房、公共服务、环境和拥堵的版本化迁移意愿；
5. 受容量与政策约束的确定性自动迁移批次；
6. 年龄更细分、教育、健康和生命周期事件。

每一步都必须新增版本、事实、投影约束和重放归约器，不得修改已发布事实含义。
