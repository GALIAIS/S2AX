# 城市模拟 F8.0 通用公共设施与城市服务底座详细设计

版本：1.1（2026-07-18）
状态：已实现并完成单元、迁移、前端及真实 PostgreSQL 集成验收
目标引擎：`city-f8-v1`
前置版本：`city-f7-v9`

## 1. 目标

F8.0 建立可被电力、供水、污水、垃圾、教育、医疗、消防、治安及后续未知服务共同复用的底层协议。底座只定义设施、服务能力、需求、连接、分配和短缺，不把业务规则固化为某一种公共设施。

本阶段必须形成完整纵向闭环：

```text
版本化目录
  -> 真实建筑中的设施
  -> 设施提供的服务容量
  -> 绑定真实主体的服务需求
  -> 有容量和损耗的连接
  -> Tick 内确定性分配
  -> 不可变 Allocation / Settlement Fact
  -> 规范状态哈希
  -> Snapshot / Replay / Recovery
  -> 查询 API 与操作工作台
```

## 2. 非目标与边界

F8.0 不直接实现：

- 电网潮流、管网压力、道路交通或真实物理方程；
- 自动建设、维护库存消耗、预算拨款和人员排班；
- 服务短缺对人口、企业、建筑和 Actor 的同 Tick 反馈；
- 利率、信贷、股票或支付系统；
- 将服务容量保存为区域或用户余额旁路字段。

这些能力必须在通用底座之上追加，不得绕过 F2 复式总账、F3 实物守恒、F7 空间身份和 F7.6 通用 Fact/Effect 协议。

## 3. 核心不变量

1. 所有业务数量使用 `BIGINT` 整数单位；比例使用 0–1000 的千分比。
2. 一个设施必须绑定当前世界中存在的建筑和行政区。
3. 一个设施类型和服务定义必须绑定不可变的 `code + version + hash`。
4. 一个容量只能属于一个设施和一个服务定义。
5. 一个需求必须绑定可验证的现实主体，不能使用无来源自由文本。
6. 一个连接只能把同一服务的设施容量连接到需求。
7. 单 Tick 单连接分配量不得超过连接容量。
8. 单 Tick 设施总调度量不得超过该服务的有效容量。
9. 单 Tick 需求总交付量不得超过需求量。
10. `delivered + shortage = requested`。
11. `delivered + loss <= dispatched`；舍入余量不得凭空形成供给。
12. 历史分配和结算一经发布不可更新、不可删除。
13. 故障或状态变化只影响当前命令生效后的未来结算，不改写历史。
14. 服务短缺的跨域效果最早在下一 Tick 消费，禁止同 Tick 无限反馈。
15. 相同初始状态、命令序列和引擎版本必须生成字节级相同的规范状态。

## 4. 版本与目录

### 4.1 引擎版本

```text
city-f7-v9 --f7_v9_to_f8_v1--> city-f8-v1
```

升级只创建目录、空投影和基线，不创建虚假设施、容量、需求或结算。旧版本的 canonical 字节保持冻结。

### 4.2 公共服务目录

每个世界持有一个目录副本：

- `catalog_id = sub2api-public-services`
- `catalog_version = 1.0.0`
- `settlement_version = 1.0.0`
- `catalog_hash` 为规范 JSON 的 SHA-256

首批服务定义只作为通用协议样例和后续扩展锚点：

| code | category | unit | flow_kind | 说明 |
|---|---|---|---|---|
| `electric_power` | `utility` | `energy_unit` | `delivery` | 发电、外购、储能到负荷 |
| `potable_water` | `utility` | `volume_unit` | `delivery` | 原水、处理和供水 |
| `wastewater` | `waste` | `volume_unit` | `collection` | 污水收集与处理 |
| `solid_waste` | `waste` | `mass_unit` | `collection` | 垃圾收集与处理 |
| `education` | `social` | `service_slot` | `capacity` | 教育服务容量 |
| `healthcare` | `social` | `service_slot` | `capacity` | 医疗服务容量 |
| `fire_response` | `safety` | `coverage_unit` | `capacity` | 消防响应覆盖 |
| `police_response` | `safety` | `coverage_unit` | `capacity` | 治安响应覆盖 |

`flow_kind` 只描述结算方向，不包含具体行业公式。目录定义不可原地修改；任何语义变化必须创建新版本并提供显式迁移。

### 4.3 设施类型目录

设施类型定义独立于服务定义。一个设施类型可以提供多个服务，容量仍逐服务配置。首批类型包含通用 `service_hub`，以及电站、水厂、污水厂、垃圾处理站、学校、医院、消防站和警务站。类型只约束允许服务、最低建筑面积、默认可靠度和空间类别，不保存运行时容量。

## 5. 数据模型

### 5.1 Profile

`city_service_profiles`：

- 世界、目录 ID/版本/哈希、结算器版本；
- 基线 Tick；
- definition/facility/capacity/demand/connection/fact/allocation/settlement 计数；
- revision 与 metadata。

Profile 计数必须等于真实投影，revision 必须随发布事实单调递增。

### 5.2 Definition

`city_service_definitions`：

- `code + version + definition_hash`；
- category、unit_code、flow_kind；
- active 状态、排序和规范 payload。

`city_facility_type_definitions`：

- `code + version + definition_hash`；
- 允许服务列表、最小建筑面积、规范 payload。

目录行只允许在升级 bootstrap 或 recovery 上下文写入。

### 5.3 Facility

`city_facilities`：

- 稳定 code/name；
- type code/version/hash；
- building、district、可选 owner economic entity；
- `offline | operational | degraded | retired`；
- reliability_milli；
- created_tick、updated_tick、version、source_fact。

设施状态机：

```text
offline -> operational -> degraded -> operational
   |             |             |
   +-----------> retired <-----+
```

`retired` 为终态。容量在 offline/retired 状态下有效值为 0；degraded 状态下由容量 availability 继续确定，不隐式修改 installed capacity。

### 5.4 Capacity

`city_facility_service_capacities`：

- facility + service definition；
- installed_capacity_units；
- availability_milli；
- available_capacity_units；
- updated_tick、version、source_fact。

配置可用容量公式：

```text
available_capacity_units =
  floor(installed_capacity_units * availability_milli / 1000)

Tick 调度有效容量 =
  operational/degraded ? available_capacity_units : 0
```

这样设施状态切换不会改写容量定义或历史来源事实。本阶段通过严格命令配置容量；后续 F8.1/F8.2 将配置命令替换或约束为建设、维修、资源和人员事实派生，不改变投影协议。

### 5.5 Demand

`city_service_demands`：

- stable demand code；
- service code/version/hash；
- subject_kind + subject_code；
- district_id 和可选 building_id；
- requested_units_per_tick；
- priority 0–1000；
- `active | suspended | retired`；
- created/updated Tick、version、source_fact。

支持的初始主体：district、building、household、enterprise、actor。每一种主体必须在命令处理和数据库断言中解析为当前世界的真实 ID；不能只验证字符串格式。

### 5.6 Connection

`city_service_connections`：

- stable connection code；
- facility capacity + demand；
- max_flow_units_per_tick；
- loss_milli 0–999；
- preference 0–1000；
- `active | suspended | retired`；
- created/updated Tick、version、source_fact。

F8.0 的连接代表一个经过汇总的可达服务通道。F8.2 可以把一个连接展开为图节点和物理边，但必须保持当前连接 ID、容量、损耗和历史结算可解释。

### 5.7 Fact

`city_service_facts` 按 `(world_id, tick, sequence)` 唯一，类型包括：

- `facility.registered`
- `facility.status.changed`
- `facility.capacity.configured`
- `service.demand.configured`
- `service.connection.configured`
- `service.allocation.settled`

命令事实绑定 source_command；自动结算事实不绑定命令。事实必须先以 draft 写入，在对应投影、分配和结算完成后统一 posted。

### 5.8 Allocation 与 Settlement

`city_service_allocations` 每行表示一个设施经一个连接向一个需求的调度：

- tick/sequence/allocation_index；
- facility、capacity、demand、connection、service；
- dispatched_units、delivered_units、loss_units；
- capacity/demand/connection 版本，以及当时的设施容量、连接容量和损耗快照；
- source_fact；
- immutable metadata。

`city_service_settlements` 每行表示一个需求在一个 Tick 的最终结果：

- requested_units；
- delivered_units；
- shortage_units；
- allocation_count；
- quality_milli；
- source_fact。

质量基础值：

```text
requested = 0: quality_milli = 1000
requested > 0: floor(delivered * 1000 / requested)
```

后续可靠性、可达性、响应时间和行业质量维度追加为独立分量，不重写历史基础质量。

## 6. 命令协议

所有命令仍通过 `/api/v1/city/worlds/:world_id/commands` 提交并在 Tick 内执行。

### 6.1 `facility.register`

```json
{
  "code": "facility-central-plant",
  "name": "Central Plant",
  "facility_type_code": "service_hub",
  "building_code": "building-civic-001",
  "owner_entity_code": "city-government",
  "reliability_milli": 1000,
  "metadata": {}
}
```

新设施固定从 offline 开始，避免注册即产生未经配置的供给。

### 6.2 `facility.capacity.configure`

```json
{
  "facility_code": "facility-central-plant",
  "service_code": "electric_power",
  "installed_capacity_units": 100000,
  "availability_milli": 950,
  "expected_version": 0,
  "metadata": {}
}
```

`expected_version = 0` 表示创建；大于 0 表示 CAS 更新。

### 6.3 `facility.status.transition`

```json
{
  "facility_code": "facility-central-plant",
  "to_status": "operational",
  "expected_version": 1,
  "metadata": {}
}
```

### 6.4 `service.demand.configure`

```json
{
  "code": "demand-district-core-power",
  "service_code": "electric_power",
  "subject_kind": "district",
  "subject_code": "district-core",
  "requested_units_per_tick": 60000,
  "priority": 700,
  "status": "active",
  "expected_version": 0,
  "metadata": {}
}
```

### 6.5 `service.connection.configure`

```json
{
  "code": "connection-central-core-power",
  "facility_code": "facility-central-plant",
  "demand_code": "demand-district-core-power",
  "max_flow_units_per_tick": 80000,
  "loss_milli": 20,
  "preference": 800,
  "status": "active",
  "expected_version": 0,
  "metadata": {}
}
```

所有配置命令必须校验 expected_version，防止后台并发覆盖。

## 7. Tick 结算算法

结算只在世界为 running 且 F8 stage 启用时运行。

### 7.1 稳定顺序

1. 服务按 service code 排序；
2. 需求按 priority 降序、created_tick、demand code 排序；
3. 连接按 preference 降序、connection code 排序；
4. 相同连接只使用一次；
5. Fact sequence 与 allocation_index 按上述顺序连续递增。

### 7.2 分配

对每个需求：

1. `remaining = requested_units_per_tick`；
2. 依次读取 active connection；
3. 取设施该服务当前剩余有效容量；
4. 计算满足 remaining 所需的最小 dispatch：

```text
required_dispatch = ceil(remaining * 1000 / (1000 - loss_milli))
dispatch = min(required_dispatch, connection_remaining, facility_remaining)
delivered = floor(dispatch * (1000 - loss_milli) / 1000)
loss = dispatch - delivered
```

5. 若 dispatch 大于 0，写 allocation；
6. `remaining -= min(remaining, delivered)`；
7. 写一个 settlement，即使没有可用连接也必须写短缺事实。

未被交付的舍入余量留在 shortage 中。禁止使用浮点数或随机 tie-break。

### 7.3 时间边界

- 本 Tick 处理成功的设施/容量/需求/连接命令可参与本 Tick 结算；
- 本 Tick 结算结果只进入事实和查询，不在同 Tick 改变人口、企业、市场或 Actor；
- 后续 stage 只能读取上一 Tick 已发布的 settlement，避免闭环振荡。

## 8. API

新增只读接口：

```text
GET /api/v1/city/worlds/:world_id/services/catalog
GET /api/v1/city/worlds/:world_id/services/facilities
GET /api/v1/city/worlds/:world_id/services/demands
GET /api/v1/city/worlds/:world_id/services/connections
GET /api/v1/city/worlds/:world_id/services/settlements
```

查询支持 service/status/district/facility/demand、Tick 游标和 limit。响应不得伪造统计；availability 为 unsupported 时前端显示明确版本升级入口。

## 9. 前端工作台

当前城市空间页新增“设施与服务”工作台：

- 总览：设施、有效容量、需求、交付、短缺和服务质量；
- 目录：服务定义和设施类型；
- 设施：建筑、状态、容量、可用率、服务列表；
- 需求：真实主体、优先级、请求量、最近质量；
- 连接：来源设施、目标需求、上限、损耗、偏好；
- 结算：按 Tick 和服务查看 allocation 明细及 shortage；
- 操作：注册、容量配置、状态转换、需求配置、连接配置；
- 刷新使用请求代次与局部 skeleton，不卸载地图和已有数据。

## 10. 恢复、断言与兼容

- F8 state 进入 `city-f8-v1` canonical，旧 canonical 显式置 nil；
- snapshot decoder 严格拒绝 F8 缺失 service state，旧版本拒绝包含 F8 state；
- recovery 先保存历史 fact ID，再清理派生投影，按目录、设施、容量、需求、连接、事实、allocation、settlement 顺序恢复；
- 所有 source fact FK 使用 deferred constraint，恢复后在事务提交前执行完整 foundation assertion；
- SQL assertion 校验定义哈希、Profile 计数、真实空间锚点、版本单调、容量守恒、连接同服务、结算恒等式和事实来源；
- 旧 F7.11 世界必须暂停后显式升级；升级不会自动启用任何公共服务。

## 11. 验收矩阵

### 11.1 单元测试

- 目录规范哈希固定；
- 状态机、CAS、主体解析和同服务约束；
- 容量公式和整数损耗边界；
- 稳定优先级分配、容量竞争、完全短缺和舍入；
- canonical 稳定排序；
- 旧版本 canonical 字节不变。

### 11.2 SQL 迁移测试

- 表、唯一约束、检查约束、FK、guard、deferred assertion；
- `city-f8-v1` 和 `f7_v9_to_f8_v1` 注册；
- pre-F8 世界不能持有 F8 投影；
- 非 draft fact/recovery 上下文不能直接改投影。

### 11.3 真实数据库集成测试

1. 创建世界并逐版本升级到 F8；
2. 注册设施、配置容量、切到 operational；
3. 配置两个不同优先级需求和多条有损连接；
4. 执行 Tick，验证稳定分配和容量守恒；
5. 降级/停运后执行下一 Tick，验证短缺只影响未来；
6. 验证 API 查询；
7. 从 snapshot replay；
8. verified recovery 后重新计算相同 hash；
9. 替换/暂停配置后验证 CAS 和历史不可变。

### 11.4 前端测试

- unsupported/unknown/available 三态；
- stale response 不覆盖新世界；
- 局部刷新不卸载地图或整个工作台；
- 命令 payload 精确；
- allocation/settlement 守恒值展示；
- 深浅色、窄屏、空状态和错误状态一致。

## 12. 后续依赖顺序

1. F8.0：本通用底座；
2. F8.1：设施建设、维护、故障、人员、预算和资源消耗；
3. F8.2：电网/水网/污水/垃圾的图网络和输送约束；
4. F8.3：教育、医疗、消防、治安的可达性、排队和响应；
5. F8.4：短缺与质量对人口、企业、建筑和 Actor 的下一 Tick 效果；
6. F9：道路、公交、货运和通勤，与 F8 网络共享空间节点但不混用结算事实。

## 13. 实现结果与验收证据

F8.0 已在 `city-f8-v1` 形成可运行的完整纵向闭环：

- 迁移 `207_city_public_service_foundation.sql` 建立目录、设施、容量、需求、连接、事实、分配、结算、写闸门和延迟断言；
- `city-f7-v9 → city-f8-v1` 支持 dry-run/apply，逐级升级集成测试覆盖已有 Actor、位置、Portal 和导航意图基础不丢失；
- 新建 F8 世界和升级世界都会得到相同版本的空公共服务基线，目录 hash、Profile 计数和 revision 受规范状态与数据库双重校验；
- 五类配置命令使用稳定 idempotency key、expected world tick 和 projection CAS，所有拒绝保持结构化错误语义；
- running Tick 按服务、需求优先级、创建 Tick、需求 code、连接偏好和连接 code 稳定分配，容量、连接、损耗和短缺全部使用整数公式；
- 设施状态变化会重新派生调度容量；该派生值参与规范状态，并由回放器按事实顺序重算，避免实时投影与 replay hash 分歧；
- replay 严格验证事实版本链、容量快照、连接快照、allocation 顺序和 settlement 恒等式；recovery 保留事实身份并重建所有外键投影；
- 主体断言会验证 district/building/household/enterprise/actor 的真实跨域身份，拒绝已失效主体和缺失空间投影；
- 查询 API 已覆盖目录、设施、需求、连接、结算和 allocation 明细，全部使用稳定 code 或 Tick/sequence 游标；
- Vue 工作台已接入真实 API、服务端聚合值、CAS 操作、局部刷新、请求代次和继续载入，不使用测试数据替代后端事实。

真实 PostgreSQL 验收覆盖：两个不同优先级需求竞争 1000 单位共享容量时稳定得到 `800/200` 分配；设施停运后的下一 Tick 全量短缺；列表游标、历史不可变、从 genesis replay、verified recovery 及恢复前后查询对象完全相等。F8.0 因此可以作为 F8.1 设施生命周期与资源约束的稳定前置层，不需要重写现有事实协议。
