# 城市模拟 F4 基础市场与财政循环详细设计

版本：v1.0  
实现版本：`city-f4-v1`  
前置依赖：F1 确定性命令与 tick、F2 复式总账、F3 实体与实物守恒  
后续依赖：F5 快照/重放/恢复、人口迁移、企业扩展和公共服务产出

## 1. 目标与边界

F4 把“城市有实体和库存”推进到“城市能够按固定周期自行运行”。首个闭环只包含四个不可省略的阶段：

1. 劳动力清算与工资支付；
2. 基础商品生产、交付、支付和消费；
3. 公共租赁住房分配与租金支付；
4. 工资/销售税收、公共采购、社会保障和预算核销。

利率、银行信贷、债券、股票和证券交易不属于本阶段。它们只有在 F4 现金流、实物交割、就业、住房和财政可长期重放后，才可以作为上层经济分支接入。

## 2. 设计原则

### 2.1 一项经济行为只有一组事实

- 钱只能由 F2 `city_journals` 与 `city_journal_entries` 改变。
- 商品和住房供给只能由 F3 `city_resource_operations` 与 `city_resource_entries` 改变。
- 就业、市场报价、住房占用和预算余额是受保护的投影，不能作为第二套事实来源。
- 市场结算头只汇总结果，不自行改变余额或库存。

### 2.2 先计算，后原子提交

每个经济周期先在事务内读取并锁定世界、政策、市场、账户、库存、群体、企业、住房和预算版本，生成完整结算计划：

```text
EconomicCyclePlan
  settlements[4]
    allocations[]
    journals[]
    resource_operations[]
    projection_mutations[]
    budget_movements[]
```

计划完成整数运算、现金可负担性、库存、产能、预算和数量守恒检查后，才按稳定顺序提交。任一 journal、resource operation、投影或延迟约束失败，整个 tick 回滚。

### 2.3 所有参数显式存储

周期频率、劳动需求比例、商品需求基数、税率、财政支出比例、当前报价、价格上下限和单期最大调价幅度均在数据库中版本化，不从 UI 文案或隐含常量读取。

## 3. 周期时钟与暂停语义

`city_economic_cycle_states` 保存：

- `cycle_index`：最后完成的经济周期序号；
- `cadence_ticks`：周期之间的 tick 数，首版为 24；
- `next_due_tick`：下次到期 tick；
- `last_settled_tick` 与投影版本。

世界在 tick 开始时为 `running` 且 `target_tick >= next_due_tick` 才执行经济周期。暂停世界仍可通过手动 tick 处理管理命令，但不会结算经济活动，也不会跳过到期周期。恢复命令在当前 tick 生效，经济周期从下一个 tick 开始执行，避免同一批控制命令改变本 tick 阶段集合。

每个周期必须形成且只形成四个 posted settlement，随后 `advance_city_economic_cycle` 才能推进时钟。部分结算不能提交。

## 4. 参数模型

`city_economic_policies` 首版字段：

| 参数 | 初始值 | 含义 |
| --- | ---: | --- |
| `labor_demand_capacity_milli` | 1000 | 企业产能中转换为岗位需求的千分比 |
| `goods_demand_population_divisor` | 10 | 每多少人口形成一个商品需求单位 |
| `household_wage_tax_milli` | 100 | 工资税率，千分比 |
| `firm_sales_tax_milli` | 50 | 商品销售税率，千分比 |
| `procurement_share_milli` | 250 | 当期税收用于公共采购的比例 |
| `social_support_share_milli` | 100 | 当期税收用于社会保障的比例 |

`city_market_states` 分别保存 `labor`、`basic_goods`、`housing` 的当前报价、下限、上限、最大调价幅度和最近一次供需/成交投影。

所有数量与金额使用非负整数最小单位。比例运算使用大整数中间值后向下取整；价格调整使用确定性四舍五入。禁止浮点参与模拟状态。

## 5. 劳动力市场

### 5.1 供需

```text
labor_supply = Σ cohort.working_age_units
labor_demand = firm.production_capacity_units × labor_demand_capacity_milli / 1000
affordable_jobs = firm.cash / wage_quote
cleared_jobs = min(labor_supply, labor_demand, affordable_jobs)
```

首版只有一个聚合企业，但家庭仍按区域和收入层保存 18 个群体。岗位按各群体劳动年龄人口比例分配，余数按稳定的区域/收入顺序分配，保证：

```text
Σ cohort.employed_units = Σ firm.employee_units = cleared_jobs
```

### 5.2 资金与投影

成交工资生成一个四行平衡 journal：企业工资费用、企业现金、家庭现金、家庭工资收入。每个正就业群体生成 employment allocation。

家庭就业和企业员工数只能通过数据库 posting 函数按版本更新；延迟约束在事务提交前再次验证总就业守恒。

## 6. 基础商品市场

### 6.1 需求、生产和产能

```text
goods_demand = ceil(total_population / goods_demand_population_divisor)
available_capacity = firm_capacity - production_capacity_used_in_this_tick
production_batches = min(
  batches_needed_for_demand,
  available_capacity / recipe_capacity_per_batch,
  every_input_inventory / recipe_input_quantity
)
```

自动生产严格使用 F3 的 `basic_goods` 配方。用户在同一 tick 先提交的生产命令会占用产能和原料，自动结算只使用剩余部分。

### 6.2 成交和交割

```text
projected_supply = opening_goods + produced_goods
affordable_goods = household.cash / goods_quote
cleared_goods = min(goods_demand, projected_supply, affordable_goods)
```

正成交必须在同一个 settlement 中形成：

1. 企业生产 resource operation（需要生产时）；
2. 企业到家庭的等量 transfer；
3. 家庭显式 consumption sink；
4. 家庭消费费用/现金与企业现金/收入 journal；
5. goods allocation。

因此不存在“只扣款未交货”“只增收入未减库存”或“商品被消费但没有 sink”的状态。

## 7. 住房市场

首版是公共租赁住房市场，政府库存中的 `housing_units` 是可出租供给；企业建设形成的开发库存暂不自动出租，后续房地产扩展再增加所有者级租赁合同。

```text
housing_demand = Σ cohort.housing_demand_units
housing_supply = government.housing_units
affordable_units = household.cash / rent_quote
occupied_units = min(housing_demand, housing_supply, affordable_units)
```

住房按群体需求比例分配。每个群体都保存 `occupied_units + unmet_units = housing_demand_units`。全城占用不得超过守恒后的住房库存；住房库存、群体需求或占用任一变化都会在提交时触发延迟校验。

租金 journal 使用家庭租金费用/现金和政府现金/租赁收入。租赁只改变使用权投影，不转移住房所有权库存。

## 8. 政府财政

### 8.1 税收

- 家庭工资税基于本周期已支付工资；
- 企业销售税基于本周期已结算商品收入；
- 税额不能超过纳税主体当前可用现金；
- 税款分别形成纳税主体税费/现金与政府现金/税收收入 journal。

### 8.2 支出与预算

公共采购与社会保障按当期税收比例计算，并同时受以下上限限制：

```text
spend <= appropriated - committed - spent
spend <= government.cash
```

公共采购进入企业现金和收入，对应政府公共服务费用；社会保障进入家庭现金和其他收入，对应政府补贴费用。

每次支出调用 `post_city_budget_spend`，同时更新预算投影并写入不可变 `city_budget_movements`。预算行不是现金，预算核销不能替代 journal，journal 也不能绕过预算授权。

## 9. 价格形成

三个市场都使用同一确定性有界调整：

```text
base = max(demand, supply)
change_milli = clamp(abs(demand - supply) × max_adjustment_milli / base,
                     1 when imbalanced,
                     max_adjustment_milli)
next_quote = round(current_quote × (1000 ± change_milli) / 1000)
next_quote = clamp(next_quote, floor_quote, ceiling_quote)
```

短缺提高下一周期报价，过剩降低报价；本周期成交永远使用本周期开始时已锁定的报价，不能用结果反向改价。

## 10. 事实表与投影表

### 10.1 不可变事实

- `city_market_settlements`：四阶段结算头；
- `city_market_allocations`：群体就业、商品、住房、税收与支出明细；
- `city_budget_movements`：预算支出流水；
- F2 journal/entry；
- F3 resource operation/entry。

结算以 draft 插入，只允许一次 draft→posted。posted 后不能更新或删除。延迟提交检查重新读取最终行，确保结算已经封账。

### 10.2 受保护投影

- `city_economic_cycle_states`；
- `city_economic_policies`；
- `city_market_states`；
- `city_household_cohorts.employed_units`；
- `city_firm_states.employee_units`；
- `city_housing_occupancies`；
- `city_government_budget_lines`。

普通 SQL 不能直接改变这些字段。posting 函数使用事务局部写入标记并校验旧版本，避免丢失更新和绕过事实流水。

## 11. 数据库级结算约束

封账前 `assert_city_market_settlement_ready` 验证：

- journal、resource operation、allocation、budget movement 实际数量等于结算头；
- allocation 金额合计等于 `gross_amount_units`；
- 所有子 journal 和 operation 已 posted；
- 子事实类型与阶段匹配；
- 非财政市场投影已经精确推进到该 tick 的供需和成交结果；
- 预算移动后的余额和版本与预算投影一致；
- 劳动和住房守恒成立。

四个结算全部 posted 后才允许推进周期。结算引用 tick 的外键为延迟约束，因此可以在 tick 汇总行落库前完成业务写入，但提交时必须完整存在。

## 12. API

```text
GET /api/v1/city/worlds/:world_id/markets
GET /api/v1/city/worlds/:world_id/market-settlements
GET /api/v1/city/worlds/:world_id/market-settlements/:tick/:sequence
```

`markets` 返回周期、政策、三个市场报价和 18 个住房占用投影。结算列表使用 `(after_tick, after_sequence)` 游标，最大 200 条；详情返回 allocation 与 budget movement。journal 和 resource operation 通过 `market_settlement_id` 可反向追溯同一结算。

`POST /step` 返回本 tick 的 commands、journals、resource operations、market settlements 和 events；幂等重放返回同一 tick 与同一事实 ID。

## 13. 状态哈希与确定性

状态哈希新增：

- 周期序号、到期 tick 和版本；
- 经济政策；
- 市场报价与最近供需；
- 各区域/收入群体住房占用。

哈希不包含数据库自增 ID、创建时间或 JSON 显示顺序。相同版本、种子、命令和初始业务状态即使数据库 ID 不同，也必须产生相同哈希。

## 14. 已实现验收

- 两个独立世界在相同种子下完成首个周期后哈希一致；
- 劳动成交、工资、就业投影守恒；
- 商品生产、交付、消费、付款和收入同事务完成；
- 住房占用不超过库存，群体需求完整分解；
- 税收、公共采购、社会保障和预算核销闭环；
- journal 试算平衡和账户投影一致；
- 游标、详情、成员授权和 step 幂等重放有效；
- 非到期 tick 不重复结算；
- 直接改价、改占用、改预算、篡改结算或删除 allocation 被数据库拒绝。

## 15. F5 之后的扩展顺序

1. F5：规范快照、逐 tick 重放、故障恢复和哈希差异诊断；
2. F6：人口出生/死亡/迁入迁出、家庭收入分层演化；
3. F7：多企业、行业投入产出、破产与新设企业；
4. F8：地块、建设项目、私人租赁和维护折旧；
5. F9：公共服务产能、设施、可达性和治理目标；
6. 所有基础层长期运行验收后，再增加信贷和证券分支。

任何扩展都必须继续输出现有 SettlementPlan，不得绕开 F2/F3/F4 的现金、实物、预算和投影守恒。
