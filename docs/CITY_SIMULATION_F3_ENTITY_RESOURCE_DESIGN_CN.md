# 城市模拟 F3 实体与实物守恒详细设计

版本：v1.0（2026-07-18）  
实现版本：`city-f3-v1`  
状态：已实现并通过迁移、单元、路由和 PostgreSQL 集成测试  
上游依赖：F0 世界与主体、F1 确定性 tick、F2 城市复式总账  
下游依赖：F4 劳动/商品/住房/财政市场（已实现）、F5 快照与重放

## 1. 目标与边界

F3 的职责不是生成价格、GDP 或股票行情，而是建立城市中“谁在什么区域拥有多少实物、通过什么合法过程发生变化”的唯一事实来源。任何后续市场都只能调用本层操作，不能直接改库存或把统计指标反写成业务事实。

本阶段解决：

1. 区域、家庭群体、企业和政府具有稳定、可版本化的状态。
2. 预算授权与实际现金分离，避免产生第二套货币余额。
3. 商品、资本品、住房和土地具有明确单位、位置、所有者和库存版本。
4. 期初、转移、消费、生产和建设都形成不可变操作及逐库存分录。
5. 同资源转移严格守恒，消费显式进入 sink，生产/建设严格匹配版本化配方。
6. 每条命令在一个 tick 总事务中原子成功或稳定拒绝。
7. F3 状态进入确定性哈希，数据库 ID、时间戳和执行耗时不进入哈希。

本阶段不实现：

- 报价、订单、撮合、市场价格、通胀或价格指数演化。
- 工资协商、失业匹配、迁移决策和人口自然增长公式。
- 贷款、利息、存款创造、股票、证券持仓和公司行为。
- 自动调度、快照压缩、奖励发放和平台虚拟货币兑换。

## 2. 核心原则

### 2.1 钱与物分账

- 钱：只由 F2 `city_journals`、`city_journal_entries` 和 `city_accounts` 记录。
- 物：只由 F3 `city_resource_operations`、`city_resource_entries` 和 `city_inventory_balances` 记录。
- 预算：`city_government_budget_lines` 是授权上限，不是现金余额。
- 市场：F4 的一次结算必须在同一个 tick 事务中同时产生 F2 journal 和 F3 resource operation。

因此，任何“购买”都不能只过账不交货，也不能只交货不付款；F4 必须将两组事实作为同一结算结果提交。

### 2.2 投影不是事实

`quantity_units` 是从不可变 entry 推导并同步维护的性能投影。数据库触发器禁止普通 SQL 直接改变它。读模型、图表、GDP、库存汇总和短缺率都可以重建，不能成为库存来源。

### 2.3 所有数量使用整数

- 资源定义声明 `unit_code` 和 `unit_scale`。
- 数量使用正 `BIGINT` 最小单位。
- 比例使用显式比例尺，例如 `productivity_milli=1000` 表示 1.000。
- 不在核心状态中保存 `float64`。
- 乘法在执行前检查 `int64` 溢出，数据库约束再次检查投影。

## 3. `city-f3-v1` 基准世界

### 3.1 区域

固定初始化 6 个区域：

| 代码 | 定位 | 关键容量 |
| --- | --- | --- |
| `central` | 行政、商业与首批主体所在地 | 高住宅、商业容量 |
| `north` | 居住与轻工业 | 中住宅、中工业容量 |
| `south` | 居住与混合产业 | 高住宅、中工业容量 |
| `east` | 制造扩展区 | 高工业容量 |
| `west` | 土地与重产业储备 | 最高工业容量 |
| `harbor` | 物流与港口产业 | 高商业、工业容量 |

面积统一使用整数面积单位。`developable_area_units <= area_units` 由数据库检查；住宅、商业和工业容量互不混用，为后续分区与建设提供独立约束。

### 3.2 家庭群体

每个区域建立低、中、高收入 3 类群体，共 18 行。每行记录：

- 人口；
- 劳动年龄人口；
- 已就业人口；
- 住房需求；
- 状态版本；
- 受限 JSON metadata。

数据库保证：

```text
0 <= employed <= working_age <= population
0 <= housing_demand <= population
```

当前群体共用 `founding_household` 经济主体及 F2 账户，F4 可按群体生成工资、消费和税收分配明细；在未证明需要前，不创建逐人记录。

### 3.3 企业状态

`city_firm_states` 把 F0 企业主体连接到区域和产业，并保存：

- `employee_units`；
- `capital_stock_units`；
- `production_capacity_units`；
- `productivity_milli`；
- `version`。

首批 `municipal_services` 位于 `central`，拥有 `basic_services` 产业代码。企业的现金、收入、费用和资产价值仍在 F2；F3 的资本存量与产能是实物/技术状态，不能替代会计金额。

### 3.4 政府与预算授权

政府状态包含行政能力和公共服务能力。预算初始化 7 个授权科目：教育、医疗、公共安全、交通、环境、社会保障和资本项目。

预算不变量：

```text
committed_units + spent_units <= appropriated_units
```

预算金额跟随世界基础货币精度初始化，但实际支出必须由 F4 同时写入预算版本和 F2 journal。预算行不能直接制造政府现金。

## 4. 资源目录与库存

### 4.1 资源定义

`city_resources` 当前包含：

| 代码 | 类型 | 单位 | 用途 |
| --- | --- | --- | --- |
| `basic_material` | `raw_material` | unit | 基础生产投入 |
| `food` | `consumer_good` | unit | 家庭消费 |
| `consumer_goods` | `consumer_good` | unit | 基础商品与家庭消费 |
| `capital_goods` | `capital_good` | unit | 生产/建设投入 |
| `housing_units` | `housing` | dwelling | 可用住房存量 |
| `developable_land` | `land` | square_meter | 可开发土地 |

资源代码、类型、单位、精度、可储存性和状态都进入世界状态哈希。新增资源必须通过新模拟版本迁移或显式配置版本，不允许运行中静默改义。

### 4.2 库存作用域

一条库存投影由以下稳定作用域唯一确定：

```text
(world, economic_entity, district, resource)
```

库存保存：

- 期初授权数量 `opening_quantity_units`；
- 当前数量 `quantity_units`；
- 乐观版本 `version`；
- active/closed 状态；
- metadata。

资源转入一个合法但尚无库存行的作用域时，服务只创建数量为零的新投影，再通过 operation entry 入库。若命令随后失败，命令 savepoint 会连同新投影一起回滚。

### 4.3 期初库存

新世界的库存投影初始数量全部为零。首次执行任意资源命令时，同一 tick 内按主体代码、区域顺序和资源代码稳定排序，生成 3 个期初 operation：

- 家庭：食物与消费品；
- 企业：基础材料、消费品和资本品；
- 政府：资本品、住房与可开发土地。

期初授权数量是配置，当前可用库存必须由 `opening` entries 形成。这样状态可以从 operation 历史校验，不存在绕过实物账的 SQL seed 余额。

## 5. 配方与产能

### 5.1 配方结构

配方头定义稳定代码、产业、每批产能消耗和状态；配方行定义每种资源是 input 或 output 及每批数量。一个配方中同一资源只能出现一次，避免同库存同操作重复分录。

当前配方：

```text
basic_goods:
  input  basic_material  2
  output consumer_goods  1
  capacity              1 / batch

housing_construction:
  input  developable_land 100
  input  capital_goods       5
  output housing_units       1
  capacity                  20 / batch
```

建设没有“凭空增加住房”。政府或其他土地所有者必须先把土地与资本品转给建设企业，企业再通过固定配方消耗投入并生成住房。

### 5.2 配方授权

`city_firm_recipes` 明确哪些企业可执行哪些配方。生产命令必须同时满足：

1. 企业主体 active；
2. 企业 F3 状态存在；
3. 企业所在区域与命令区域一致；
4. 配方及授权 active；
5. 所有投入库存充足；
6. 本 tick 已用产能加本次产能不超过企业上限。

数据库在封账时重新汇总同企业同 tick 的 posted production operations，不能仅相信应用层预检。

## 6. 不可变实物操作

### 6.1 操作头

`city_resource_operations` 保存：

- `(world_id, tick, sequence)` 稳定游标；
- 世界内唯一 `operation_key`；
- `opening`、`transfer`、`consumption` 或 `production` 类型；
- 来源命令、行动主体、区域；
- 可选配方和批次数；
- 描述、metadata、创建与封账时间。

操作只能经历一次：

```text
draft -> posted
```

预封账插入、封账后更新、二次封账和删除都由数据库触发器拒绝。事务提交时，延迟约束确认所有新操作已经 posted。

### 6.2 操作分录

每条 `city_resource_entries` 保存：

- 库存作用域与资源；
- `in` 或 `out`；
- 正整数数量；
- 更新前后库存；
- 更新前后版本；
- 稳定行号和 memo。

分录只能由数据库函数 `post_city_resource_entry` 创建。函数按库存行加锁、检查溢出/不足、设置事务内操作上下文、更新投影并写入匹配分录。触发器再次验证分录与锁定后的投影完全一致。

### 6.3 各操作不变量

`opening`：

- 无来源命令；
- 只能包含 `in`；
- 所有库存属于行动主体和操作区域。

`transfer`：

- 恰有一条 `out` 和一条 `in`；
- 两条分录必须是同一资源、相同数量、不同库存作用域；
- `out` 必须属于行动主体和操作源区域；
- 总流入等于总流出。

`consumption`：

- 恰有一条 `out`；
- 无流入；
- `purpose` 必填，明确资源进入何种 sink；
- 库存不足时不允许负数。

`production`：

- 输入配方行映射为 `out`，输出配方行映射为 `in`；
- 每个实际数量必须等于 `每批数量 × batch_count`；
- 不允许缺行、多行或额外资源；
- 所有库存属于同一企业和区域；
- 本 tick 产能总消耗不超过企业上限。

## 7. 命令协议

所有命令通过现有入口提交：

```text
POST /api/v1/city/worlds/:world_id/commands
```

### 7.1 转移

```json
{
  "command_type": "resource.transfer",
  "payload": {
    "from_entity_id": 3,
    "to_entity_id": 2,
    "from_district_code": "central",
    "to_district_code": "central",
    "resource_code": "developable_land",
    "quantity_units": 100,
    "memo": "construction allocation"
  },
  "expected_world_tick": 0
}
```

### 7.2 消费

```json
{
  "command_type": "resource.consume",
  "payload": {
    "entity_id": 1,
    "district_code": "central",
    "resource_code": "food",
    "quantity_units": 5,
    "purpose": "household consumption"
  },
  "expected_world_tick": 0
}
```

### 7.3 生产/建设

```json
{
  "command_type": "resource.produce",
  "payload": {
    "firm_entity_id": 2,
    "district_code": "central",
    "recipe_code": "housing_construction",
    "batch_count": 1,
    "memo": "first housing unit"
  },
  "expected_world_tick": 0
}
```

字段严格校验，未知字段、重复 JSON key、浮点数量、空 purpose、无效代码和越界批次数在入队前拒绝。业务期内的库存不足、配方无权和产能超限会把命令终结为稳定 `rejected`，不会中止同 tick 其他合法命令。

## 8. Tick 事务顺序

F3 完整执行顺序：

1. 取得世界 advisory transaction lock。
2. 锁定世界并验证成员、版本、状态和 expected tick。
3. 重放命中时直接读取原 tick 结果。
4. 按命令序列锁定待执行命令。
5. 必要时创建 F2 期初 journals。
6. 必要时创建 F3 期初 resource operations。
7. 按全局命令顺序执行控制、总账和资源命令。
8. 每个可预期业务失败使用 savepoint 回滚局部写入。
9. 更新世界时钟与投影。
10. 对 F0–F3 稳定状态计算 SHA-256。
11. 写入 tick；延迟外键此时关联 journal/operation 与 tick。
12. 终结命令，写入期初、命令和 tick 完成事件。
13. 保存世界状态哈希并一次提交。

事务失败时，世界、账户、库存、journal、operation、命令和事件全部不可见；不存在半个 tick。

## 9. 状态哈希

F3 哈希新增：

- 区域身份、面积和容量；
- 家庭群体人口、劳动、就业、住房需求与版本；
- 企业位置、产业、人员、资本、产能、生产率与版本；
- 政府能力与预算授权；
- 资源定义；
- 配方、配方行及企业授权；
- 库存作用域、期初授权、当前数量、版本和状态。

以下内容不进入哈希：

- 数据库自增 ID；
- 平台用户 ID；
- `created_at`、`updated_at`、`posted_at`；
- tick 墙钟耗时；
- operation 历史本身（当前投影与版本已进入哈希）。

所有查询按稳定业务代码、区域顺序和方向排序。相同版本、种子和等价命令在不同数据库 ID 下必须得到相同哈希。

## 10. 读取 API

```text
GET /api/v1/city/worlds/:world_id/state
GET /api/v1/city/worlds/:world_id/resource-operations
GET /api/v1/city/worlds/:world_id/resource-operations/:tick/:sequence
```

`state` 返回 F3 完整可查询状态和 `as_of_tick`。operation 列表采用 `(after_tick, after_sequence)` 游标，默认 50、最大 200；详情包含所有 entry。`step` 结果也返回当前 tick 的完整 `resource_operations`，幂等重放不会再次执行。

所有读取先验证活跃城市成员；非成员统一返回不存在，避免泄露世界身份。

## 11. 错误与拒绝语义

| 代码 | 含义 | 事务行为 |
| --- | --- | --- |
| `CITY_RESOURCE_SCOPE_NOT_FOUND` | 主体、区域、资源或库存作用域无效 | 拒绝单命令 |
| `CITY_RESOURCE_INSUFFICIENT_INVENTORY` | 流出后将为负数 | 拒绝单命令 |
| `CITY_RESOURCE_RECIPE_NOT_ALLOWED` | 企业、区域、配方或授权无效 | 拒绝单命令 |
| `CITY_RESOURCE_CAPACITY_EXCEEDED` | 同 tick 产能不足 | 拒绝单命令 |
| `CITY_SIMULATION_INVARIANT_FAILED` | 配方、投影、版本或数据库事实不一致 | 中止整个 tick |

应用错误只能映射明确可预期的业务条件。溢出、投影失配、历史篡改和约束错误不能被吞成普通拒绝，否则会掩盖损坏。

## 12. 已完成测试

迁移测试验证所有表、复合外键、投影守卫、配方校验、不可变触发器和提交约束存在，且 F3 不含利率、贷款或证券业务。

服务单元测试验证：

- 命令代码与 memo 规范化后指纹稳定；
- 重排 JSON 字段不改变命令意图；
- 重复 key、未知字段、浮点/零/越界数量和非法作用域在入队前拒绝；
- 版本化 PRNG 在 F3 稳定且与旧版本隔离。

PostgreSQL 集成测试验证：

- 创建事务形成 6 区域、18 群体、1 企业状态、1 政府状态、7 预算行、6 资源、2 配方和 8 个期初库存投影；
- 两个数据库 ID 不同但种子和业务命令等价的世界产生相同状态哈希；
- 生产、转移、消费和住房建设形成正确前后库存；
- 住房建设实际消耗土地与资本品；
- 游标分页、详情、成员授权和 step 幂等重放正确；
- 库存不足和产能超限不留下 operation；
- 直接修改库存、修改 posted operation 或删除 entry 被数据库拒绝；
- F0、F1、F2、F3 城市集成测试组合运行通过。

## 13. F4 接口契约（已由 `city-f4-v1` 实现）

F4 不得绕开 F2/F3。每个市场清算器必须输出一个确定性结算计划：

```text
SettlementPlan
  monetary journals[]
  resource operations[]
  budget mutations[]
  domain events[]
```

计划在写入前完成纯计算和不变量检查，随后在一个世界 tick 事务中按稳定顺序提交。建议实施顺序：

1. 劳动力市场：家庭可用劳动、企业岗位、工资 journal、就业状态版本。
2. 基础商品市场：订单/需求汇总、库存预留、现金 journal、资源 transfer。
3. 住房市场：住房库存、家庭需求、租金/购买结算和区域占用。
4. 政府财政：预算承诺、税收 journal、采购/补贴 journal、公共资产或资源 operation。
5. 指标读模型：只从已提交事实汇总就业、产量、短缺、价格与财政，不反向写状态。

F4 验收前不接入利率、信贷或股票。只有劳动、商品、住房和财政在长时间确定性运行中保持资金与实物守恒，金融扩展才有可信的现金流和资产基础。

具体 settlement、allocation、价格、就业、住房和预算 posting 规则见《城市模拟 F4 基础市场与财政循环详细设计》。
