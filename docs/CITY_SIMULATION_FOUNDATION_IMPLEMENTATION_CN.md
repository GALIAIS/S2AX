# 城市模拟基础内核实施设计

版本：v1.0（2026-07-18）  
状态：F0–F7.1 基础内核、版本生命周期与空间状态闭环已实现；F7.2 字符前端待实施  
关联文档：《城市模拟游戏总体设计》《城市模拟 F6.2 人口迁移设计与验收》《城市模拟 F6.3 家庭生命周期与收入层迁移详细设计》《城市模拟 F7.0 空间规则与坐标底座详细设计》《可扩展虚拟货币与权益体系设计》

## 1. 决策与范围

城市模拟系统首先建设可验证的通用基础，不把利率、银行信贷、证券交易或股票行情当作底层前提。银行和交易所也不作为基础主体预置；贷款合同、存款创造、利息计提、违约、订单、成交和公司行为只能在基础内核通过验收后，通过新迁移和版本化规则作为独立分支接入。

基础内核必须先解决六件事：

1. 世界、成员、版本、时间和权限有唯一事实来源。
2. 所有经济主体拥有稳定身份，且不等同于平台用户。
3. 城市内部货币、平台余额和平台虚拟货币完全隔离。
4. 金额、商品和状态更新使用确定性表示，不依赖浮点误差或系统当前时间。
5. 命令、tick、账本、快照和事件能重放、校验和恢复。
6. 每个上层模型只能通过命令、事件和交易边界改变状态，不能直接改余额或聚合指标。

### 1.1 当前阶段包含

- 世界与成员访问边界。
- 世界内独立基础货币。
- 家庭、企业、政府和清算主体的稳定身份。
- 按主体类型定义的科目模板及账户实例。
- 一个私人世界一个所有者、一个基础货币、同世界主体/模板/账户一致性的数据库约束。
- 创建、列表和读取世界基础结构的认证 API。
- 带稳定序列、请求指纹、预期 tick 和终态结果的命令事实。
- 单世界事务锁、固定模拟时钟、版本化确定性 PRNG、单步 tick 和规范状态哈希。
- 不可修改的 tick 与事件事实，以及按稳定游标读取事件的认证 API。
- 按 tick 封账的多主体复式 journal/entry、账户版本投影和数据库级不可变约束。
- 现金转移、工资、采购、税收、补贴、冲正命令，以及总账游标和试算平衡认证 API。
- 固定 6 区域、18 个家庭群体、企业/政府能力、预算授权、资源目录、生产配方和库存投影。
- 不可变实物 operation/entry、期初入库、转移、消费、生产/建设命令及资源审计 API。
- 版本化经济周期与政策、劳动/商品/住房市场、税收/财政支出和受保护投影。
- 市场 settlement/allocation、预算 movement、市场游标/详情和完整 tick 结算结果 API。
- 创世、升级基线和逐 tick 的不可变规范快照，以及双 SHA-256/gzip 完整性校验。
- 从任意可用基线按命令、总账、实物和市场事实逐 tick 归约的重放审计与 JSON Pointer 差异诊断。
- 只允许 verified replay 驱动的当前 tick 投影恢复、事务 savepoint 和不可变恢复审计。
- 世界本地日/月/季度/年边界、18 个家庭 cohort 的聚合年龄结构、月度人口自然变化和整数余数累计。
- 人口 movement/line 不可变事实、日历与人口读取 API，以及人口状态的规范哈希、重放和恢复闭环。
- F5/F6 引擎并存、不可变版本目录、有向无环升级路径、显式 dry-run/apply 升级和历史版本快照读取。
- 失败 tick 独立审计、跨世界故障隔离、阶段依赖/命令归属校验和完整引擎运行观测。
- `city-f6-v2` 外部迁入、外部迁出和同收入层区域迁移命令，以及不可变 migration/line 事实。
- 迁移人口、就业和住房下界，逐年龄段区域守恒，F6 v1→v2 原子升级、旧快照兼容与迁移事实重放恢复。
- `city-f6-v3` 独立 `household_units`、四类家庭数量变化、相邻收入层迁移和极端人口变化的可审计家庭解体修复。
- household movement/line 不可变事实、家庭/人口/占住房守恒、v2→v3 原子升级、旧规范字节兼容、重放与 verified recovery。
- F7.0 固定 32×32 Chunk、负坐标下取整、离散 Z、稳定 Chunk key，以及严格 JSON 空间规则集、无环 `looks_like` 和固定 SHA-256。
- 内置 `sub2api-classic@1.0.0` 语义规则及认证规则集列表/详情 API；本阶段不伪造地块、建筑或 NPC。
- `city-f7-v1` 世界规则/生成器绑定、9×9 Overmap、确定性 32×32 Chunk Mapgen、按需生成命令和不可变空间 mutation/line。
- 空间 profile/Overmap/Chunk 的数据库写闸门、规范 Chunk 根、版本化快照、逐 tick replay、verified recovery，以及 ruleset/Overmap/Chunk/changes 成员读取 API。

### 1.2 当前阶段不包含

- 利率曲线、贷款审批、存款创造、准备金或违约处置。
- 股票、订单簿、撮合、持仓、分红或公司行为。
- 自动家庭形成率/社会流动评分、企业进入退出、私人住房合同和公共服务质量反馈。
- 后台自动 tick 调度、奖励发放和可玩前端；当前仍提供显式、幂等的单步执行 API。

这些不是被删除，而是明确依赖后续基础层；提前实现会产生无法回放的孤立余额和伪造指标。

## 2. 分层边界

```text
平台身份/分组
    │
    ▼
世界与成员 ── 模拟版本 ── 世界时钟
    │
    ▼
经济主体 ── 科目模板 ── 城市内部账户
    │
    ▼
命令日志 ── 确定性 tick ── 城市复式总账
    │
    ▼
人口/企业/政府/商品/住房等实体状态
    │
    ▼
劳动、商品、住房和财政清算
    │
    ├── 银行与信贷扩展
    ├── 证券市场扩展
    └── 奖励 Outbox → 平台虚拟货币
```

城市内部账户只描述游戏经济。平台 `users.balance` 和 `virtual_currencies` 不得成为城市货币的外键或余额来源。游戏奖励只能由已验证的奖励事件经 Outbox 调用平台虚拟货币接口。

## 3. 基础阶段与依赖门槛

### F0：世界、主体和科目表

目标：建立所有后续模型共享的身份与账户边界。

完成条件：

- 每个未归档私人世界只有一个所有者。
- 每个世界恰有一个基础货币。
- 世界所有者始终有一个活跃 `owner` 成员关系。
- 账户引用的主体、货币和科目模板必须属于同一世界。
- 主体类型与科目模板类型由复合外键强制一致。
- 禁止负数的账户不能写入负余额。
- 非成员读取世界时返回不存在，不泄露世界是否存在。

### F1：确定性命令与 tick 内核

依赖 F0，现已实现：

- `city_commands`：客户端意图、稳定幂等键、预期 tick、状态和拒绝原因。
- `city_ticks`：版本、起止 tick、输入命令范围、PRNG 状态、状态哈希、执行耗时和结果。
- `city_events`：tick 产生的领域事实，按 `(world_id, tick, sequence)` 排序。
- 单世界数据库 advisory lock 或等价租约，保证同一时刻只有一个写入者。
- 版本化 PRNG；随机值只能从 `(simulation_version, seed, tick, subsystem, sequence)` 派生。
- 所有规则使用传入的模拟时钟，禁止在规则内部读取 `time.Now()`。

当前控制命令为 `world.rename`、`world.set_speed`、`world.pause` 和 `world.resume`。命令在提交时做严格 JSON 校验并规范化；同一世界内按数据库分配的连续序列执行。显式 `step` 无论世界当前是暂停还是运行都会推进一个模拟小时；暂停/运行状态留给后续自动调度器判断，不改变显式单步的确定性语义。

事务顺序：取得 advisory lock → 锁定世界行 → 检查 step 幂等事实 → 读取待执行命令 → 计算内存候选状态 → 写入世界投影 → 计算状态哈希 → 写入 tick → 终结命令并写入事件 → 更新世界哈希 → 一次提交。

F1 首次使用 `city-f1-v1` 版本；F2、F3、F4、F5、F6.1–F6.3、F7.1–F7.6 依次升级状态契约，当前执行版本为 `city-f7-v5`。旧版本继续作为可运行、可回放的兼容版本保留，升级只能沿显式单步路径进行。默认模拟时间从 `2000-01-01T00:00:00Z` 开始，每个 tick 固定推进一小时；数据库当前时间只记录提交和耗时，不进入规则或状态哈希。

### F2：通用多主体复式总账

依赖 F1，现已实现 `city_journals` 与 `city_journal_entries`：

- journal 在事务内从 draft 变为 posted，封账后不可修改或删除。
- entry 使用 `int64` 最小单位，借方和贷方均为非负数；每条 entry 只能有一侧非零。
- 同一 journal、同一货币的借方合计必须等于贷方合计；每个参与主体内部也必须借贷平衡。
- 账户余额是总账投影，使用账户版本做并发检查。
- 现金、库存资金和其他受限账户在提交后不能为负。
- 冲正通过新 journal 关联原 journal，不修改历史。

F2 已实现现金转移、工资、采购、税收和补贴五种基础交易。第一次出现这些交易时，在同一 tick 内先按稳定主体顺序写入家庭、企业和政府的平衡期初 journal；初始余额不是 SQL seed，也不能绕过总账。现金转移同时记录付款方转账费用和收款方其他收入，保证每个主体自身会计恒等式成立。

每个 ledger 命令都在 tick 总事务内使用 savepoint 隔离预期业务拒绝。余额不足、主体类型不符、科目不存在、重复冲正等会终结为稳定的 `rejected` 命令，不留下 draft journal、entry 或部分余额；数据库约束、溢出和投影失配仍中止整个 tick。账户按 ID 固定顺序加锁，journal 只允许一次 `draft → posted`，entry 只允许由锁定账户投影产生，封账后 journal 与 entry 均不可更新或删除。

冲正命令使用稳定的 `(journal_tick, journal_sequence)` 引用，而不是依赖不同数据库间不稳定的自增 ID。期初和冲正 journal 不能再次冲正；同一原 journal 只能有一个冲正。贷款、利息和证券结算仍不在这一阶段。

### F3：实体状态与实物守恒

依赖 F2，现已实现区域、家庭群体、企业、政府预算授权、资源目录、生产配方和库存：

- 金额使用 `int64`；比例、价格指数和技术参数使用定点整数或有明确舍入规则的十进制定点值。
- 商品库存、住房、土地、劳动力和产能分别有单位、来源和去向。
- 生产必须消耗允许的投入；消费必须减少库存；建设必须占用土地和资本品。
- 聚合读模型不能反向成为事实来源。

当前 `city-f3-v1` 固定创建 6 个区域和低/中/高收入 18 个家庭群体；企业与政府能力使用整数单位和版本字段。预算行只表达可承诺上限，实际现金仍完全来自 F2 账户与 journal，避免第二套余额。

实物采用与总账同等级的不可变事实模型：`city_resource_operations` 是 draft→posted 操作头，`city_resource_entries` 保存每个库存投影的方向、数量、前后余额和前后版本。库存投影只能通过数据库函数 `post_city_resource_entry` 改变；直接更新、预封账、修改或删除历史都会被触发器拒绝。转移必须同资源等量流出/流入；消费必须产生一个显式 sink；生产必须逐资源、逐方向精确匹配版本化配方，且同企业同 tick 的产能消耗不能超过上限。

当前命令为 `resource.transfer`、`resource.consume` 和 `resource.produce`。`basic_goods` 配方把基础材料转为消费品；`housing_construction` 必须先取得土地和资本品，再消耗二者生成住房。第一次出现资源命令时，同一 tick 按主体和资源稳定顺序写入期初 operation，初始库存不能由 SQL 直接写成可用余额。详细约束见《城市模拟 F3 实体与实物守恒详细设计》。

### F4：基础市场与财政循环

依赖 F3，现已实现。运行世界到达 `next_due_tick` 时，按劳动、基础商品、公共租赁住房、财政四阶段形成一个完整经济周期。工资来自企业现金；商品生产受配方、原料和当期剩余产能限制；购买同时交付并消费实物；住房占用受政府住房库存和家庭现金约束；税收来自工资与销售，公共采购和社会保障同时受现金与预算授权限制。

每个阶段写入不可变 settlement/allocation，并通过 `market_settlement_id` 关联 F2 journal 与 F3 resource operation。就业、市场报价、住房占用、预算核销和周期时钟只能由版本化 posting 函数更新；数据库在封账与事务提交时复核子事实数量、金额合计、阶段类型、劳动/住房守恒和投影版本。价格只根据已结算供需做有界整数调整。详细规则见《城市模拟 F4 基础市场与财政循环详细设计》。

### F5：快照、重放、恢复与观测

依赖 F4，核心重放/恢复闭环现已实现：

- `city_snapshots` 保存版本、tick、源 tick、规范 gzip 状态、未压缩状态哈希和压缩载荷哈希；创世与每个成功 tick 在原事务内写快照。
- 既有 F4 世界升级后先建立当前 tick 基线，不伪造旧版本历史；随后每个 tick 都能从该基线向前重放。
- 重放不读取在线投影，而是按不可变 command、journal entry、resource entry、settlement/allocation 和 budget movement 逐 tick 归约。
- 每个检查点同时比较 tick 哈希、快照哈希和规范 JSON 字节；差异记录首个 tick 与稳定 JSON Pointer。
- 恢复只允许 verified replay 对当前 tick 投影重建，不允许删除后续事实或把世界倒回旧 tick。
- `city_replay_runs` 与 `city_recovery_runs` 保存幂等、不可删除的运行审计；恢复失败回滚 savepoint，不留下半恢复投影。

后台自动调度、多世界失败退避和指标采集属于后续产品运行层；详细契约见《城市模拟 F5 快照、逐 Tick 重放与投影恢复详细设计》与《城市模拟 F5 后续基础层路线与详细设计》。

### F6.1：日历边界与人口自然变化

依赖 F5，首个城市本体垂直切片现已实现：

- `city_calendar_states` 保存世界本地日期、日/月/季度/年序号、最近边界 tick 和投影版本。
- `city_calendar_boundaries` 按日、月、季度、年保存不可变边界事实；同世界、类型、本地日期唯一。
- `city_demographic_policies` 保存版本化 ppm 年率和每年 12 个结算周期。
- `city_demographic_cohort_states` 为 18 个家庭 cohort 保存儿童、劳动年龄、老年人口及六类整数余数。
- 每个跨月 tick 写一条已封账 `natural_change` movement 和逐 cohort line，原子记录出生、三类死亡和两类年龄迁移。
- 人口变化使用 `int64`、ppm 和余数累计，不使用浮点或墙钟；期末人口严格等于期初人口加出生减死亡。
- 家庭 cohort 的总人口与劳动年龄投影随 movement 同事务推进，就业与住房需求约束继续成立。
- F6 状态进入规范哈希；日历边界和 population movement 可从 F5 基线逐 tick 归约，并可由 verified replay 恢复。

F6.1 不开放设置人口或原地修改人口政策的旁路。F6.2 已通过 `city-f6-v2` 的迁入、迁出和区域迁移事实扩展；F6.3 已通过 `city-f6-v3` 把家庭数与人口数分离，并把家庭数量变化和收入层迁移纳入不可变事实、规范哈希、重放和恢复。银行、信贷或证券继续与人口/家庭基础隔离。详细协议见 F6.2 与 F6.3 专项文档。

### H1：版本生命周期与长期运行硬化

F0–F7.1 复用的 H1 不改变城市玩法，而是固化扩展协议：`city-f5-v1`、`city-f6-v1`、`city-f6-v2`、`city-f6-v3` 与 `city-f7-v1` 可同时运行；世界版本只能经 paused、owner、无待处理命令、源快照一致的显式升级切换。升级支持预演，目标投影、规范哈希和目标基线快照原子提交；失败回到 savepoint 并保留不可修改审计。数据库拒绝反向/循环升级路径、原地版本修改和不匹配的源/目标证据。

引擎阶段定义会检查 control、ledger、resources、calendar_demography、markets 的依赖与顺序，命令只能进入所属阶段。10,000 tick 门禁覆盖中途重建服务实例、逐 tick 快照、0→10,000 全量事实重放、固定最终哈希、余额/库存非负和人口就业边界。详细协议见《城市模拟引擎底座硬化设计与验收》。

## 4. F0 数据模型

### 4.1 `city_worlds`

保存世界身份、所有者、状态、模拟版本、随机种子、当前 tick、模拟时间、调度时间、速度、时区、状态哈希和非权限设置。

关键规则：

- 私人世界使用部分唯一索引限制一个未归档世界。
- `simulation_version` 创建后不由普通更新接口修改。
- `seed > 0`，`current_tick >= 0`。
- `state_hash` 为空或 64 位小写十六进制 SHA-256。
- `settings` 必须是 JSON object，不能保存角色、余额或权限。

### 4.2 `city_members`

角色：`owner`、`planner`、`treasurer`、`trader`、`viewer`。  
状态：`active`、`left`、`banned`。

首版只创建所有者成员。后续邀请和角色变更必须通过独立命令和审计记录完成；客户端不能在创建世界时指定自己的角色。

### 4.3 `city_monetary_units`

每个世界至少且恰有一个 `is_base=true` 的单位。代码只允许小写字母、数字和下划线；金额精度为 0–8 位。城市单位不能引用平台货币定义。

### 4.4 `city_economic_entities`

主体类型：

| 类型 | 基础职责 | 当前是否执行行为 |
| --- | --- | --- |
| household | 收入、消费、租金、税费和资本 | 否，仅建立身份与账户 |
| firm | 生产、库存、工资、收入、负债和权益 | 否 |
| government | 税收、公共服务、投资和财政余额 | 否 |
| clearing | 商品、工资、证券和舍入差异的清算主体 | 否 |

`owner_user_id` 只表达平台用户与游戏主体的控制关系，不代表主体本身就是平台用户。

`bank` 与 `exchange` 不在 F0 的类型约束和初始化数据中；E1/E2 实施时分别通过新迁移加入，避免把金融分支固化成城市内核的必需依赖。

### 4.5 `city_account_templates`

科目模板由世界、主体类型和稳定代码标识，保存会计类别、正常余额方向、是否允许负数、是否必需和显示顺序。新主体从同类型模板实例化账户。

首批科目覆盖：

- 家庭：现金、应收、应付、资本、工资收入、其他收入、消费、租金、转账费用和税费。
- 企业：现金、应收、库存、固定资产、应付、债务、权益、收入、其他收入、工资、转账费用和税费。
- 政府：现金、税收应收、公共资产、应付、债务、基金余额、税收、公共服务、资本支出和补贴。
- 清算：商品、工资的应收/应付，以及允许正负的舍入差额；证券清算科目由 E2 增加。

### 4.6 `city_accounts`

账户引用主体、货币和模板，并保存物化余额、版本和状态。三组复合外键同时验证世界与主体类型，避免跨世界挂账或把家庭科目挂到企业。

F0 账户余额均为零；F2 之后只有封账 journal 能改变余额。届时禁止提供通用“设置余额”接口。

## 5. F0 创建事务

创建私人世界必须在一个数据库事务中完成：

1. 插入暂停状态世界，服务端生成随机种子并固定模拟版本。
2. 插入所有者成员。
3. 插入一个基础货币。
4. 插入四类主体的科目模板。
5. 插入首批家庭、企业、政府和清算主体。
6. 按主体类型实例化账户。
7. 初始化 F3 区域、家庭群体、企业/政府状态、预算授权、资源、配方和零值库存投影。
8. 校验 F3 基线数量、劳动力上限和资源配置。
9. 初始化 F4 周期、政策、三个市场和 18 个住房占用投影，并把家庭就业规范化为企业岗位总量。
10. 校验 F4 劳动、住房和市场基线。
11. 初始化 F6.1 世界本地日历、人口政策和 18 个 cohort 年龄结构，并校验人口投影与家庭群体一致。
12. 计算完整规范状态，写入 tick 0 创世快照和世界状态哈希。
13. 在同一事务中读取完整基础结构。
14. 提交时由延迟约束检查所有者成员、基础货币、劳动、住房和人口守恒。

任一步失败都回滚；并发创建由数据库部分唯一索引裁决，应用层返回稳定冲突错误。

## 6. F0 API

```text
GET  /api/v1/city/worlds
POST /api/v1/city/worlds
GET  /api/v1/city/worlds/:world_id
```

创建请求：

```json
{
  "name": "海港市",
  "timezone": "Asia/Shanghai",
  "monetary_unit": {
    "code": "city_credit",
    "name": "City Credit",
    "symbol": "CC",
    "scale": 2
  }
}
```

`Idempotency-Key` 由统一幂等协调器处理。所有者 ID、角色、状态、版本和种子只由服务端决定。读取接口必须通过活跃成员关系授权。

### 6.1 F1 命令与 tick API

```text
POST /api/v1/city/worlds/:world_id/commands
GET  /api/v1/city/worlds/:world_id/commands/:command_id
POST /api/v1/city/worlds/:world_id/step
GET  /api/v1/city/worlds/:world_id/events
```

所有写接口强制要求可打印 ASCII `Idempotency-Key`，长度不超过 128；城市路由请求体上限为 16 KiB。统一 HTTP 幂等协调器负责请求级重放，`city_commands` 和 `city_ticks` 的唯一约束负责领域事实级重放；即使协调器缓存过期，同一键也不会再次执行。

提交改名命令：

```json
{
  "command_type": "world.rename",
  "payload": {"name": "海港市"},
  "expected_world_tick": 0
}
```

设置速度使用整数千分比，例如 `{"speed_milli": 1250}` 表示 `1.250x`。暂停与恢复命令的 `payload` 必须是空对象。未知字段、浮点速度、越界值和未知命令类型在入队前拒绝。

执行单步：

```json
{"expected_world_tick": 0}
```

返回值同时包含不可变 tick、该 tick 终结的命令和有序事件。重复使用相同 step 键和相同请求会返回原 tick；同键不同请求返回冲突。事件读取使用 `(after_tick, after_sequence)` 游标和最大 200 条限制，不使用不稳定 offset。

F2/F3/F4 后，step 还返回该 tick 的完整 `journals`、`resource_operations`（含 entries）和 `market_settlements`（含 allocations/budget movements），重放同一 step 键只读取原事实，不重新过账、移动库存或清算市场。

### 6.2 F1 数据不变量

- `city_commands` 在 `(world_id, sequence)` 和 `(world_id, user_id, client_request_id)` 上唯一。
- 命令身份与意图不可修改，只允许一次 `pending → applied/rejected` 转换；终态命令不可再次更新或删除。
- `city_ticks` 在 `(world_id, tick)` 和 `(world_id, step_request_id)` 上唯一，写入后不可更新或删除。
- `city_events` 在 `(world_id, tick, sequence)` 上唯一，并通过复合外键保证命令事件与命令的实际处理 tick 一致。
- tick 的应用数与拒绝数之和必须等于命令总数；无命令时首尾命令序列必须为空。
- 服务先取得按世界派生的 PostgreSQL transaction advisory lock，再锁定世界行；同世界写入串行，不同世界可并行。
- 状态哈希不包含数据库 ID、平台用户 ID、墙钟时间或执行耗时，只包含版本声明的稳定模拟状态。

### 6.3 F2 总账命令与读取 API

所有总账写入继续复用 `POST /api/v1/city/worlds/:world_id/commands`，不存在设置余额、直接过账或直接删除流水的旁路接口。支持的命令及严格 payload 为：

```text
ledger.cash_transfer  {from_entity_id, to_entity_id, amount_units, memo?}
ledger.wage          {firm_entity_id, household_entity_id, amount_units, memo?}
ledger.purchase      {household_entity_id, firm_entity_id, amount_units, memo?}
ledger.tax           {payer_entity_id, amount_units, memo?}
ledger.subsidy       {recipient_entity_id, amount_units, memo?}
ledger.reverse       {journal_tick, journal_sequence, reason}
```

`amount_units` 必须是正 `int64` 整数，并限制在 `MaxInt64 / 2` 以内，使四分录交易的总借贷额不会溢出。`memo` 可为空且最多 256 个 Unicode 字符；冲正原因必填。工资只允许企业付款给家庭，采购只允许家庭付款给企业，税收付款方和补贴收款方只允许家庭或企业。

```text
GET /api/v1/city/worlds/:world_id/journals
GET /api/v1/city/worlds/:world_id/journals/:tick/:sequence
GET /api/v1/city/worlds/:world_id/trial-balance
```

journal 列表使用 `(after_tick, after_sequence)` 升序游标，默认 50、最大 200 条；详情返回主体、科目、借贷、前后余额和前后账户版本。试算平衡在只读 `REPEATABLE READ` 快照内读取，逐货币返回借方、贷方、差额、主体不平衡数和账户投影失配数。只有差额、主体不平衡数和投影失配数同时为零时，结果才为平衡。

### 6.4 F2 tick 原子顺序

取得世界锁 → 读取待执行命令 → 必要时写入期初 journal → 按命令序列建立 savepoint 并过账/拒绝 → 更新世界投影 → 对含账户余额和版本的状态求哈希 → 写入 tick → 终结命令 → 写入期初/命令/完成事件 → 更新世界哈希 → 一次提交。journal 到 tick、journal 到命令的复合外键和延迟检查在提交时确认所有事实属于同一世界与同一 tick。

### 6.5 F3 实物命令与读取 API

```text
resource.transfer {from_entity_id, to_entity_id, from_district_code, to_district_code,
                   resource_code, quantity_units, memo?}
resource.consume  {entity_id, district_code, resource_code, quantity_units, purpose}
resource.produce  {firm_entity_id, district_code, recipe_code, batch_count, memo?}
```

所有代码在入队前规范为小写稳定代码；数量必须是正 `int64`，生产批次数不超过 1,000,000。生产只能使用已授予企业、属于企业所在区域且处于 active 状态的配方。预期业务失败使用命令级 savepoint，仅把该命令终结为稳定拒绝，不留下新库存行、draft operation 或部分 entry。

```text
GET /api/v1/city/worlds/:world_id/state
GET /api/v1/city/worlds/:world_id/resource-operations
GET /api/v1/city/worlds/:world_id/resource-operations/:tick/:sequence
```

状态接口返回区域、家庭群体、企业、政府、预算授权、资源、配方和库存；operation 列表使用 `(after_tick, after_sequence)` 升序游标，默认 50、最大 200 条。详情返回资源、主体、区域、方向、数量、前后库存和前后版本。

### 6.6 F3 tick 原子顺序

取得世界锁 → 读取命令 → 必要时建立 F2 期初 journal → 必要时建立 F3 期初 operation → 按全局命令序列使用各自 savepoint 执行总账/实物/控制命令 → 更新世界 → 对实体配置、配方、预算授权、账户与库存投影求哈希 → 写 tick → 终结命令与事件 → 一次提交。tick、journal、resource operation 和 command 的复合外键均在同一事务中检查，因此不存在“钱已扣但货未动”或半个生产批次可见的状态。

### 6.7 F4 市场与财政 API/原子顺序

```text
GET /api/v1/city/worlds/:world_id/markets
GET /api/v1/city/worlds/:world_id/market-settlements
GET /api/v1/city/worlds/:world_id/market-settlements/:tick/:sequence
```

运行世界到期时，命令先按序应用，随后读取命令完成后的现金、库存和产能，纯计算四阶段计划，再按稳定顺序写入 allocation、journal、resource operation、就业/住房/市场/预算投影并逐 settlement 封账。四个 settlement 全部 posted 后推进周期；最后更新世界、计算包含 F4 投影的状态哈希、写 tick 与事件并一次提交。暂停命令在本 tick 结束生效，是否执行市场以 tick 开始时状态决定。

### 6.8 F5 快照、重放与恢复 API

```text
GET  /api/v1/city/worlds/:world_id/snapshots
GET  /api/v1/city/worlds/:world_id/snapshots/:tick
GET  /api/v1/city/worlds/:world_id/replay-runs
POST /api/v1/city/worlds/:world_id/replay-runs
GET  /api/v1/city/worlds/:world_id/replay-runs/:run_id
GET  /api/v1/city/worlds/:world_id/recovery-runs
POST /api/v1/city/worlds/:world_id/recovery-runs
GET  /api/v1/city/worlds/:world_id/recovery-runs/:run_id
```

快照读取返回元数据与完整性结果，不返回原始载荷。重放和恢复写请求强制领域幂等；恢复要求 owner、verified replay、目标等于当前 tick。快照、重放和恢复事实均不可修改或删除。

### 6.9 F6.1 日历与人口 API/原子顺序

```text
GET /api/v1/city/worlds/:world_id/calendar
GET /api/v1/city/worlds/:world_id/population
GET /api/v1/city/worlds/:world_id/population-movements
GET /api/v1/city/worlds/:world_id/population-movements/:tick/:sequence
```

基础小时 tick 先比较世界时区中的前后本地日期；跨日时依次写日、月、季度、年边界，再在跨月时封账人口自然变化，随后才执行到期 F4 市场。人口列表使用 `(after_tick, after_sequence)` 游标，详情返回完整逐 cohort line。边界、movement 和 line 都是不可变事实；日历、人口和家庭群体投影只能通过绑定草稿事实或受审计恢复写闸门更新。

## 7. 数值、时间与确定性规范

- 钱、数量和可计数资产优先使用 `int64` 最小单位。
- 比例采用明确比例尺，例如 ppm、bp 或配置声明的小数位；禁止业务状态保存 `float64`。
- 除展示外不依赖本地时区；规则运行使用世界模拟时间和 IANA 时区。
- 每个除法都规定向下、向上、银行家舍入或余数去向；余数进入清算舍入科目。
- map、数据库无序结果和并行任务进入哈希前必须按稳定键排序。
- 状态哈希只包含版本声明的事实字段，不包含更新时间、执行耗时或数据库自增 ID 等环境噪声。

## 8. 并发、权限和安全

- 用户 API 只能访问活跃成员所属世界。
- 客户端不能提交所有者 ID、主体余额、账户余额、tick、随机种子或奖励金额。
- 世界创建、命令和结算写操作必须幂等。
- 同一世界 tick 使用数据库级互斥；不同世界可并行。
- 管理员修复也通过审计命令和冲正，不直接更新历史事实。
- JSON 扩展字段设大小上限，并禁止承载权限和核心金额。
- 所有外键使用 `RESTRICT` 保存历史关系；删除用户前必须走显式匿名化/归档流程。

## 9. 测试矩阵

### F0

- 并发创建同一用户私人世界只成功一次。
- 非成员无法读取世界。
- 世界缺少 owner 成员或基础货币时事务提交失败。
- 跨世界主体、货币或模板不能组成账户。
- 主体类型与模板类型不匹配时数据库拒绝。
- 禁止负数的账户写入负余额时数据库拒绝。
- 创建失败不留下半个世界或部分账户。

### F1–F6.1

- **F1 已验证：**不同数据库 ID 和所有者、相同种子与命令序列生成相同状态哈希和 PRNG 证明。
- **F1 已验证：**命令与 step 重复提交只产生一个事实；同键不同意图冲突。
- **F1 已验证：**同一 step 键并发执行只推进一次，不同 step 键串行推进为连续 tick。
- **F1 已验证：**非成员不能读取命令或事件，tick、事件和命令意图的历史更新被数据库拒绝。
- **F2 已验证：**五类交易和期初账逐 journal、逐主体借贷平衡，账户版本与余额投影一致。
- **F2 已验证：**相同种子和等价主体命令在不同数据库 ID 下得到相同状态哈希；step 重放不重复过账。
- **F2 已验证：**余额不足只拒绝对应命令且不产生 journal；合法冲正恢复目标科目，重复冲正被拒绝。
- **F2 已验证：**非成员不能读取 journal 或试算平衡；直接改余额、预封账、非平衡封账和修改/删除历史均被数据库拒绝。
- **F3 已验证：**不同数据库 ID 下，相同种子与等价资源命令得到相同状态哈希；step 重放不重复移动库存。
- **F3 已验证：**转移守恒、消费减库、生产配方、住房建设土地/资本投入和企业 tick 产能上限均形成可执行事实。
- **F3 已验证：**库存不足与产能超限只拒绝对应命令；直接改库存、修改 operation 或删除 entry 被数据库拒绝。
- **F3 已验证：**状态、operation 游标与详情均按成员授权；6 区域、18 群体、7 预算行、资源与配方在创建事务中闭环初始化。
- **F4 已验证：**相同种子世界的劳动、商品、住房与财政周期产生相同状态哈希；非到期 tick 不重复清算。
- **F4 已验证：**工资、商品款、租金、税收、公共采购、社会保障、实物交割、就业、住房和预算在一个 tick 内闭环。
- **F4 已验证：**市场游标/详情/成员授权和 step 重放正确；直接改价、占用、预算、settlement 或 allocation 被数据库拒绝。
- **F5 已验证：**创世和逐 tick 快照与状态哈希同事务落库，规范 gzip 可重复，载荷和状态双哈希可发现篡改。
- **F5 已验证：**从 tick 0 逐事实归约控制、总账、库存、劳动、住房、预算、市场和周期到目标 tick，逐检查点哈希与规范字节一致。
- **F5 已验证：**人为制造当前世界投影漂移后，verified replay 驱动的恢复重建完整投影并恢复目标哈希；失败路径使用 savepoint 全回滚。
- **F5 已验证：**成员/owner 授权、请求幂等、游标列表、详情读取、不可变快照和终态审计约束有效。
- **F6.1 已验证：**UTC、Asia/Shanghai、闰日、季度/年末和 America/New_York 夏令时跳转得到唯一边界；10 年小时级日历计数无漂移。
- **F6.1 已验证：**月度自然变化逐 cohort 满足出生、死亡、年龄迁移、劳动年龄、就业和住房约束；120 个月整数余数累计无浮点漂移。
- **F6.1 已验证：**相同 seed、不同数据库 ID 的跨年结算状态哈希一致；人口事实可从 tick 0 重放，日历/政策/人口投影漂移可由 verified replay 恢复。
- **F6.1 已验证：**成员授权、人口 movement 游标/详情、直接投影写入和边界/movement/line 不可变约束有效。
- 随机合法交易下借贷平衡、受限余额非负。
- **H1 已验证：**固定 seed 运行 10,000 tick，中途重建服务实例后继续推进，0→10,000 全量重放 verified；最终哈希固定为 `856dd27de0f1929b9fd4b3413ed0085583dc6a02829d6de2d614ea6f638a83c4`。
- **H1 已验证：**多版本世界并行推进、dry-run/apply、历史快照、故障注入回滚、相同幂等键恢复、反向升级拒绝和跨世界失败隔离均通过。
- **F7.1 已验证：**固定 seed 的 Overmap/Chunk 黄金哈希稳定，负坐标可生成，越界、重复和非地表请求稳定拒绝。
- **F7.1 已验证：**F6.3→F7 预演不落库、应用原子生成规则绑定与 81 Tile 基线；空间事实可从 tick 0 重放，非法投影漂移可由 verified recovery 精确恢复。
- **F7.1 已验证：**profile、Overmap、Chunk、mutation 和 line 的直接改写被数据库拒绝，bbox、游标、成员授权和响应上限有效。
- F6 起每个新增子系统必须同时扩展规范状态、事实归约器、恢复映射和长周期黄金场景。

## 10. 后续经济分支

### E1：银行、利率与信贷

前置条件：F2 总账、F3 企业/家庭现金流、F4 价格与财政、F5 回放全部通过。新增贷款合同、还款计划、逾期、拨备、坏账、存款和准备金；利率是合同或政策参数，不直接改余额。每次放款、计息、还款和核销均生成平衡 journal。

### E2：证券与股票

前置条件：企业具备可验证损益、资产负债表和现金流。新增证券定义、股份登记、资金/证券预留、订单、集合竞价、成交、持仓、分红和公司行为。股票价格来源于订单和企业基本面，不能用随机曲线替代。

### E3：空间、交通与公共设施

在聚合区域模型稳定后，依次增加地块/建筑、开发商、道路/公交、通勤、电力、供水、污染和灾害。每个子模型通过版本化状态和事件接入，不修改已有命令和账本语义。

## 11. 执行顺序

1. **已完成：F0 世界、成员、货币、主体、科目表和只读结构 API。**
2. **已完成：F1 命令日志、单世界锁、确定性 PRNG、tick 状态机和状态哈希。**
3. **已完成：F2 城市 journal/entry、账户锁定、五类交易、冲正和试算平衡。**
4. **已完成：F3 区域、家庭群体、企业、政府、预算授权、资源、库存、配方和产能守恒。**
5. **已完成：F4 劳动、商品、住房、价格形成、税收、预算与财政循环。**
6. **已完成：F5 不可变快照、逐 tick 事实重放、差异诊断和当前 tick 投影恢复。**
7. **已完成：F6.1 世界本地日历边界、人口年龄结构、月度自然变化及 F5 接入闭环。**
8. **已完成：H1 多版本共存、显式原子升级、失败隔离、运行观测和 10,000 tick 黄金回放。**
9. **已完成：F6.2 迁入、迁出、区域迁移、逐年龄段守恒、F6 v1→v2 升级和事实重放恢复。**
10. **已完成：F6.3 家庭数量与人口分离、家庭生命周期事实、收入层迁移、v2→v3 升级和重放恢复。**
11. **已完成：F7.0 空间坐标、规则集、固定哈希、字符/像素回退和认证读取 API。**
12. **已完成：F7.1 `city-f7-v1`、世界规则集绑定、Overmap/Chunk Mapgen、空间事实和重放恢复。**
13. **已完成：F7.2 CLASSIC 字符视口、Chunk 缓存、检查器和文本导出。**
14. **已完成：F7.3–F7.5 地块/建筑、开发施工与企业经营场所事实链。**
15. **已完成：F7.6 `city-f7-v5` 开放世界 Actor、属性成长、Role 转换、Rule/Case 处罚、状态到期、重放恢复与角色工作台。**
16. 下一版本先追加 Actor 位置、控制 grant 和地图 glyph，再按依赖顺序建设设施、交通、产业、家庭福利、环境和产品运行层；E1 银行信贷与 E2 证券保持独立可选分支。

每一阶段必须包含迁移、服务端授权、事务不变量和可运行测试；没有通过上一阶段验收，不提前向下一经济分支写业务余额或行情逻辑。
