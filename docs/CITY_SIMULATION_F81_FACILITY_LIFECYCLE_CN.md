# 城市模拟 F8.1：设施生命周期、人员、预算与资源守恒

版本：1.0
目标引擎：`city-f8-v2`
前置版本：`city-f8-v1`（F8.0 通用公共服务协议）

## 1. 目标

F8.1 不增加某一种城市服务的专用捷径，而是在 F8.0 的设施、容量、需求、连接和结算协议之上补齐所有设施共用的真实生命周期：

1. 设施从未投运、投运、运行、维护、故障、维修到退役均由状态机和不可变事实驱动；
2. 建设、维护和维修必须占用执行方劳动力，消耗 F3 库存，并通过 F2 复式总账支付；
3. 政府项目还必须经过预算承诺、支出或释放，预算授权不能代替现金余额；
4. 人员编制同时支持经济实体的聚合岗位和开放世界 Actor 的具名岗位；
5. 可派发容量由行政状态、设施状况、人员覆盖和工程/故障门控共同派生；
6. 每个 Tick 的服务结算保持不可变，磨损和新故障只能影响下一 Tick；
7. snapshot、replay、verified recovery 和断点升级必须完整覆盖新增状态。

F8.1 不负责管线、电网拓扑、道路可达性和跨域健康效果；这些分别属于 F8.2、F8.3 和 F8.4。

## 2. 核心时序

每个运行 Tick 固定为以下顺序：

```text
待处理命令
  ├─ 生命周期操作排期/启动/取消
  └─ 人员配置
        ↓
PRE_SERVICE
  ├─ 推进在建/维护/维修/退役工程
  ├─ 完成工程并更新当期有效门控
  └─ 重算人员覆盖与有效容量因子
        ↓
F8.0 SERVICE_SETTLEMENT
  ├─ 按有效派发容量分配
  └─ 固化 allocation / settlement
        ↓
POST_SERVICE
  ├─ 根据当期利用率结算磨损
  ├─ 计算确定性故障抽样
  └─ 新故障从下一 Tick 起降低容量
```

该顺序是协议的一部分。回放时必须先归约 `PRE_SERVICE` 生命周期事实，再归约 F8.0 服务事实，最后归约 `POST_SERVICE` 生命周期事实。

## 3. 版本与兼容

- `city-f8-v1` 的目录、设施事实、历史分配和结算不修改。
- `city-f8-v2` 新增独立生命周期状态；F8.0 的 `available_capacity_units` 仍表示设备配置层可用量。
- F8.1 的 `effective_factor_milli` 是额外门控，最终派发容量为：

```text
dispatch = floor(F8.0_available × effective_factor_milli / 1000)
```

- 升级时，既有运行设施获得可审计的 `legacy_baseline` 状态，保持升级前派发能力；后续容量增加不会自动获得人员。
- 新注册设施以 `uncommissioned` 初始化，必须完成投运工程才能产生派发容量。
- F8.0 快照序列化时不出现 F8.1 字段，历史 hash 保持原语义。

## 4. 领域对象

### 4.1 Lifecycle Profile

每个世界一行，固定策略 ID、版本、hash、基线 Tick、各类投影计数和 revision。策略绑定后不可原地替换；策略升级必须提升模拟版本。

### 4.2 Lifecycle Policy

每个设施类型绑定一个版本化策略，包含：

- 维护周期；
- 基础磨损、利用率磨损、逾期磨损；
- 故障阈值、基础故障概率和状况惩罚；
- 每单位人员可覆盖容量；
- 投运、维护、维修、退役的材料、资本品、劳动、时长和预算系数；
- 维护恢复值、维修恢复值。

所有系数均为整数或千分数。在线 Tick 不使用浮点数。

### 4.3 Facility Lifecycle State

每个设施一行当前投影：

- `lifecycle_status`：`uncommissioned | operational | maintenance | failed | decommissioning | retired`；
- `condition_milli`：`0..1000`；
- `staff_required_units`、`staff_assigned_units`、`staffing_factor_milli`；
- `operation_factor_milli`、`effective_factor_milli`；
- 上次维护、下次到期 Tick；
- 当前操作、开放故障、累计故障数；
- optimistic version 和 source fact。

有效因子取行政门控、生命周期门控、状况和人员覆盖的最小值；任何一项为零时不能派发。

### 4.4 Facility Operation

操作类型：

- `commission`：首次投运；
- `maintenance`：预防性维护；
- `repair`：修复开放故障；
- `decommission`：永久退役。

状态机：

```text
planned ──start──> active ──progress──> completed
   └────cancel────> cancelled
```

启动后的材料、资本品和已付费用均为沉没成本，不允许通过取消凭空返还。每个设施最多存在一个 `planned/active` 操作。

计划由策略和当时已安装容量派生并固化，包含 sponsor、executor、预算行、计划开始、时长、材料、资本品、并发劳动和总费用。后续策略变化不能改变已排期计划。

### 4.5 Staffing Assignment

岗位分配支持两种主体：

- `entity`：经济实体提供的聚合人员单位；
- `actor`：具名开放世界 Actor，资格由当前职业和属性事实派生。

实体提供的活动分配不得超过企业 `employee_units` 或政府 `public_service_capacity_units`。同一 Actor 同时只能有一个活动设施岗位。资格折算后形成有效人员单位；用户不能直接填写资格值。

### 4.6 Incident

故障事件具有稳定 code、触发 Tick、确定性抽样 proof、故障前状况、严重度、容量影响和状态。状态为 `open | resolved`，只能由完成的 repair 操作关闭。重试和回放不会重复产生故障。

### 4.7 Budget Movement

预算事实类型：

- `commit`：排期时冻结授权；
- `spend`：启动时将承诺转为实际支出；
- `release`：未启动计划取消时释放承诺。

每条事实记录 committed/spent/version 的前后值并保持不可变。政府预算必须同时满足：

```text
committed + spent <= appropriated
```

预算只代表授权。启动时还必须由 F2 journal 验证政府现金余额，并向执行方支付；二者在同一数据库事务中提交。

## 5. 命令协议

### 5.1 `facility.operation.schedule`

输入设施、操作类型、执行实体、预算代码、计划开始 Tick 和设施生命周期 expected version。系统派生并固化资源、劳动、时长和费用；政府项目原子创建预算承诺。

### 5.2 `facility.operation.start`

验证操作 expected version、开始时间、设施状态、开放故障、执行方劳动余量、库存、预算承诺和现金。成功后原子完成：

1. 两笔 F3 消耗操作（存在非零需求时）；
2. 一笔 F2 journal；
3. 一笔预算 spend（政府项目）；
4. 操作和设施生命周期投影更新；
5. 不可变 lifecycle fact。

任一检查失败均回滚并返回稳定业务拒绝码。

### 5.3 `facility.operation.cancel`

仅允许取消 `planned` 操作；政府预算承诺同时释放。活动操作不得用取消规避沉没成本。

### 5.4 `facility.staffing.configure`

以 assignment code 进行创建或版本化更新，支持 active/released。命令完成后重新计算设施人员覆盖和下一次派发因子。

## 6. 确定性推进

### 6.1 工程进度

活动操作按固化时长推进：

```text
progress = min(1000, floor((targetTick - startedTick + 1) × 1000 / durationTicks))
```

同一 Tick 内按设施 code、操作 code 稳定排序。完成事实和状态变更只产生一次。

### 6.2 磨损

磨损使用当期已固化 allocation：

```text
utilization_milli = dispatched / effective_dispatch_capacity
decay = base + utilization_component + overdue_component
condition_after = max(0, condition_before - decay)
```

维护和维修恢复只在对应操作完成事实中发生。

### 6.3 故障

抽样输入固定为：

```text
simulation_version + world_seed + tick + facility_code + policy_hash + failure_count
```

使用 SHA-256 派生 `0..999999`，与整数 PPM 阈值比较。事实保存 proof 和阈值，便于独立审计。只有 `POST_SERVICE` 能自动打开故障，因此当期已结算服务不受回写影响。

## 7. 守恒与不变量

1. 最终派发量不超过 F8.0 可用容量乘生命周期有效因子；
2. 一个设施最多一个活动/计划操作、一个开放故障；
3. 操作资源消耗必须能追溯到同一 source fact 的 F3 operation；
4. 操作费用必须能追溯到同一 source command 的已过账 F2 journal；
5. 政府操作的预算 movement、journal 和操作费用完全相等；
6. 执行方活动工程劳动预留不超过其员工容量；
7. 实体岗位分配不超过人员容量，Actor 不重复占岗；
8. 投运只适用于未投运设施，维修只适用于开放故障，退役为终态；
9. 故障只降低未来容量，历史 allocation/settlement 永不更新；
10. profile 计数、事实头、投影 version/source fact 和开放索引必须一致；
11. canonical、从 genesis replay 和 verified recovery 的 hash 必须一致。

## 8. 查询与前端

查询层提供：

- 生命周期总览与风险计数；
- 设施状态、状况、人员覆盖、维护到期和有效容量；
- 操作稳定游标列表及资源/预算/总账凭证；
- 人员分配列表；
- 故障历史及确定性 proof；
- 预算承诺、支出和释放历史。

前端在现有公共服务工作台增加“生命周期、工程、人员、故障、预算”视图。刷新保留已有数据，不卸载主面板；命令回执和局部行更新不得触发整页闪烁。

## 9. 后续边界

- F8.2：将当前设施级有效容量接入电网、管网和收集网络容量；
- F8.3：将人员、服务半径和 F7/F9 可达性加入教育、医疗、消防、治安；
- F8.4：把短缺与可靠性以滞后一周期输入传播到健康、生产率、迁移和环境；
- F9：交通和物流网络；
- F10+：多行业供应链为设施提供真实材料、设备和专业劳动力市场。

## 10. 实施与验收状态

F8.1 已在 `city-f8-v2` 完成纵向闭环：

- `208_city_facility_lifecycle.sql` 建立策略、状态、工程、人员、事故、预算移动和事实表，并通过数据库触发器保护事实来源、投影写入、资源消耗声明、预算守恒与提交时完整性；
- 工程启动允许同一命令按资源种类生成多条守恒 operation，同时保持普通资源命令“一命令一操作”的原约束；
- 生命周期 reducer 严格验证各事实类型的封闭载荷、状态链、工程证据、预算前后值和 F8.0 派发容量联动；
- 从 genesis replay 会同时重建生命周期子状态与政府预算物理投影，恢复器按 JSON 语义而非 JSONB 字节顺序验证策略目录；
- 查询、handler 和认证路由覆盖目录、设施状态、工程、人员、事故、预算移动和事实稳定游标分页；
- 真实 PostgreSQL 集成场景覆盖资源准备、新设施注册、容量与人员、投运工程、双资源消耗、总账支付、预算承诺/支出、服务结算、分页、越权、数据库 foundation assertion、完整重放与 verified recovery。

核心验收命令：

```text
go test ./migrations -run ^TestCityFacilityLifecycleMigrationInstallsAuditableConservedProjection$ -count=1
go test ./internal/service -run '^(Test.*Facility|Test.*PublicService)' -count=1
go test -tags=integration ./internal/repository -run ^TestCityFacilityLifecycleCommissioningQueriesReplayAndRecovery$ -count=1
```
