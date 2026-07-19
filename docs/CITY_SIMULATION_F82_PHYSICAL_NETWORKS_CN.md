# 城市模拟 F8.2：通用物理网络、容量路由与流量守恒

版本：1.0
目标引擎：`city-f8-v3`
前置版本：`city-f8-v2`（F8.1 设施生命周期）

## 1. 目标与边界

F8.2 在 F8.0 的服务需求/连接/结算与 F8.1 的设施有效容量之下增加一层真正参与分配的物理网络。它不是电力、供水、污水和垃圾各自独立的一组汇总字段，而是四者共享的版本化容量图、端点、路径、流量和故障隔离协议。

首版覆盖：

1. 有向或双向的通用多重图；
2. 设施容量端点、需求端点和中继节点；
3. 边容量、可用率、整数损耗、稳定成本和运行状态；
4. 多源、多需求、共享瓶颈和替代路径下的确定性路由；
5. 每笔 F8.0 allocation 到具体路径/边流量的不可变分解；
6. 网络损耗与连接损耗分账，数量守恒；
7. 节点/边配置、隔离、恢复和版本冲突；
8. canonical、snapshot、genesis replay、verified recovery、查询 API 和前端拓扑工作台。

本阶段不实现：

- 交流潮流、无功功率、电压相角、水压瞬态等领域专用连续方程；
- 道路通勤和物流（F9）；
- 教育、医疗、消防和治安的时间可达性（F8.3）；
- 服务短缺对健康、生产率、迁移和环境的跨域反馈（F8.4）；
- 自动施工网络资产所需的材料市场细分。边的维护/修复先保留通用状态与事实接口，后续可接 F8.1 同类 operation 协议。

## 2. 兼容原则

- `city-f8-v1` 与 `city-f8-v2` 的历史 allocation/settlement、快照字节和 hash 不改变。
- F8.2 新状态只出现在 `city-f8-v3` canonical 中。
- 升级 `city-f8-v2 → city-f8-v3` 时，为每个已有网络型 service connection 建立可审计的直连基线边；边损耗为零，原 connection loss 保留，因此升级前后的服务结果不变。
- 新建 F8.2 世界没有伪造设施或需求；只初始化策略目录和空网络 profile。
- 社会服务在 F8.2 继续使用 F8.0 直连分配。只有策略目录标记为 `network_required` 的服务必须经过物理网络。

首版网络型服务：

```text
electric_power  delivery
potable_water   delivery
wastewater      collection
solid_waste     collection
```

`delivery` 的 source 是设施、sink 是需求；`collection` 在业务语义上从需求流向设施，但 F8.0 allocation 仍以“设施为供给能力、需求为被满足对象”记账。F8.2 路由器通过策略的 `route_direction` 决定图方向，最终 allocation 的 dispatched/delivered 语义保持统一。

## 3. Tick 时序

```text
COMMAND
  ├─ network.configure
  ├─ network.node.configure
  ├─ network.edge.configure
  └─ network.edge.transition
        ↓
F8.1 PRE_SERVICE
  └─ 工程完成、人员和设施有效容量更新
        ↓
F8.2 + F8.0 COMBINED SETTLEMENT
  ├─ 锁定服务需求、连接、设施容量、端点和边版本
  ├─ 确定性选择剩余容量路径
  ├─ 原子生成 network flow decomposition
  ├─ 生成 service allocation / settlement
  └─ 同一事务封账两类事实
        ↓
F8.1 POST_SERVICE
  └─ 依据已结算设施利用率磨损和抽样故障
        ↓
F8.2 POST_NETWORK（后续启用）
  └─ 依据已固化边利用率产生下一 Tick 的磨损/故障
```

服务 allocation 与网络 flow 必须同成同败，禁止先写服务结果、再异步“补齐路径”。

## 4. 领域模型

### 4.1 Network Profile 与 Policy

每个世界一行 profile：

- policy ID/version/hash；
- baseline tick；
- network/node/edge/fact/batch/path/segment 计数；
- revision 和 schema metadata。

每个服务一条不可变 policy：

- `network_required`；
- `route_direction = supply_to_demand | demand_to_facility`；
- 最大节点、边、单 allocation 路径数和单路径 hop 数；
- 路径成本权重；
- 是否允许双向边；
- 默认 edge availability、condition 和故障参数；
- 整数算法版本。

策略原地修改被禁止；变化必须提升模拟版本。

### 4.2 Physical Network

一个 network 绑定且只绑定一个 service definition：

- `code/name`；
- `status = active | suspended | retired`；
- `topology_revision`；
- source fact、optimistic version、metadata。

同一世界/服务首版最多一个非 retired network。一个 network 可以包含多个不连通岛，查询层必须显式报告孤岛。

### 4.3 Network Node

节点角色：

- `supply`：绑定一个 F8.0 facility service capacity；
- `demand`：绑定一个 F8.0 service demand；
- `junction`：纯中继；
- `storage`：预留给后续库存/储能，不在首版跨 Tick 蓄积数量；
- `gateway`：网络边界或跨层接口。

节点可锚定 district、building 或权威 XYZ；锚点只描述空间身份，不直接决定流量。约束：

- supply 节点的 capacity 与 network service 必须一致；
- demand 节点的 demand 与 network service 必须一致；
- 每个 active capacity/demand 在对应 network 中最多一个 active 端点；
- retired 节点不能被 active edge 引用。

### 4.4 Network Edge

边字段：

- `from_node/to_node`，禁止自环；
- `direction = directed | bidirectional`；
- `installed_capacity_units`；
- `availability_milli`；
- `available_capacity_units = floor(installed × availability / 1000)`；
- `loss_milli = 0..999`；
- `base_cost_units > 0`；
- `status = active | isolated | failed | retired`；
- `condition_milli`、failure count；
- optimistic version、source fact、metadata。

双向边的两个方向共享同一份 Tick 剩余容量，不能各自使用满额。并行边合法，以 edge code 稳定区分。

### 4.5 Network Fact

命令事实：

- `network.configured`；
- `node.configured`；
- `edge.configured`；
- `edge.state_changed`。

自动事实：

- `network.flow_settled`：一个 network 在一个 Tick 的完整流量批次；
- `edge.condition_changed`、`edge.failed`（后续维护切片启用）。

事实包含严格 schema version、before/after 投影、命令来源或自动阶段、版本链和 source fact。事实发布后不可变。

### 4.6 Flow Batch、Path 与 Segment

每个 network/Tick 最多一个 posted batch，记录：

- service code、network/topology revision；
- service allocation 数量；
- path/segment 数量；
- source/dispatched、network received、network loss；
- posted fact。

每个 allocation 可分解成多条 path：

- 绑定 service fact 的 `(tick, sequence, allocation_index)`；
- connection、source node、sink node；
- path index、hop count；
- dispatched、network received、network loss；
- path cost 和稳定 path hash。

每个 segment 记录：

- edge code/version 和实际方向；
- edge capacity snapshot；
- input/output/loss；
- segment index。

约束：首段 input 等于 path dispatched；相邻 segment 的前一 output 等于后一 input；末段 output 等于 path network received；各 path 聚合必须与 allocation 的网络扩展字段相等。

## 5. 数量语义与守恒

F8.2 allocation 扩展为：

```text
facility_dispatched
  ──network edges──> network_received
  ──connection loss──> delivered

network_loss    = facility_dispatched - network_received
connection_loss = network_received - delivered
total_loss      = network_loss + connection_loss
```

旧版本 allocation 的网络扩展字段全部为 NULL，继续满足：

```text
delivered = floor(dispatched × (1000 - connection_loss_milli) / 1000)
```

F8.2 网络型 allocation 必须满足：

```text
network_received = Σ path.network_received
dispatched       = Σ path.dispatched
network_loss     = dispatched - network_received
delivered        = floor(network_received × (1000 - connection_loss_milli) / 1000)
loss_units       = dispatched - delivered
```

每条 edge 在一个 Tick 的所有方向 input 合计不得超过其 available capacity snapshot。设施 dispatched 仍不得超过 F8.1 lifecycle-gated capacity；需求 delivered 不得超过 requested；connection dispatched 不得超过 max flow。

## 6. 确定性路由算法

### 6.1 稳定输入顺序

沿用 F8.0：

1. service code；
2. demand priority 降序；
3. demand created tick；
4. demand code；
5. connection preference 降序；
6. connection code。

### 6.2 路径选择

对每个仍有需求和设施余量的 connection：

1. 建立只包含 active network/node/edge 的残量图；
2. 排除剩余容量为零的边；
3. 使用整数 Dijkstra，路径成本为各边 `base_cost + loss_weight × loss_milli` 之和；
4. 成本相同时按 hop 数、完整 edge-code 序列、node-code 序列比较；
5. 根据路径逐段损耗，用单调整数二分求不超过各边残量的最大 source dispatch；
6. 预留共享边容量并形成 path；
7. 若需求、设施和 connection 仍有余量，重新寻路，直到无路径或达到策略 path 上限。

算法不得迭代“每一个单位”，不得使用 map 遍历顺序或浮点数。

### 6.3 损耗舍入

每段：

```text
output = floor(input × (1000 - edge.loss_milli) / 1000)
```

零输出路径不产生事实。二分上界取设施、connection 和首边残量最小值；所有乘法先做溢出检查。

### 6.4 Collection 方向

对 `demand_to_facility`：路径搜索从 demand node 到 supply node，但 allocation 仍记录 facility/demand/connection。路径 segment 的 direction 反映实际图方向，数量仍以“被处理的需求单位”解释。

## 7. 命令协议

### 7.1 `network.configure`

创建或版本化更新 network 名称/状态。创建时验证 service policy；retired 为终态，且必须先将全部子 node/edge 退役，不能留下任何非 retired 资产。

### 7.2 `network.node.configure`

创建或更新节点。输入 network、role、锚点或 capacity/demand code、状态和 expected version。变更绑定前必须不存在依赖该节点的 active edge；端点服务必须匹配。

### 7.3 `network.edge.configure`

创建或更新边的端点、方向、容量、可用率、损耗、成本和状态。端点变更只允许在非 active 状态；容量降低必须在命令 Tick 的下一次 settlement 生效，不修改历史流量。

### 7.4 `network.edge.transition`

隔离、恢复、标记故障或退役。`failed` 不能直接恢复为 `active`，必须先转换为 `isolated`，完成检查或配置修复后再激活；所有转换保留不可变命令事实，历史故障和既有流量不可删除。

所有命令使用 expected world tick、idempotency key、expected version、owner 权限和严格 JSON；拒绝返回稳定业务码且不写半条事实。

## 8. 数据库不变量

1. policy、network、node、edge 的 world/service 复合外键完整；
2. source/demand 节点绑定对象服务一致且活动绑定唯一；
3. active edge 的两个端点 active、同 network 且不同；
4. 边 available capacity 是安装容量与可用率的整数派生值；
5. 每 Tick/network 至多一个 posted batch；
6. flow path 必须引用同 Tick 已发布 service allocation；
7. segment 链连续、方向合法、损耗公式正确；
8. 每边输入聚合不超过容量快照；
9. path、allocation、batch 三层数量和计数完全相等；
10. network-required allocation 必须存在 flow path；非网络服务不能伪造网络 path；
11. profile 计数、revision、事实序列和 projection source fact 一致；
12. posted fact/flow/segment 不可更新或删除；恢复只能在显式 recovery gate 内重建。

## 9. Canonical、Replay 与 Recovery

`city-f8-v3` hash 新增 `physical_networks`：profile、policies、networks、nodes、edges、facts、batches、paths、segments 全部稳定排序。旧版本强制置空该字段。

回放顺序：

```text
F8.1 PRE_SERVICE facts
→ network command facts
→ combined service/network settlement facts
→ F8.1 POST_SERVICE facts
→ network post facts
```

network flow reducer同时校验并重建 service allocation 的网络扩展字段，任何 edge version、path hash、容量或损耗差异都返回首个 JSON pointer。

Recovery：

1. 验证目标 snapshot 与完整 replay；
2. 建立稳定 identity map；
3. 清理网络当前投影和 flow 子事实；
4. 按事实恢复 profile/policy/network/node/edge；
5. 恢复 batch/path/segment 并重新执行全量守恒断言；
6. 重载状态并要求 canonical hash 与目标 snapshot 完全一致；
7. 任一步失败整事务回滚。

## 10. 查询与前端

查询 API：

- `GET /services/networks/catalog`：策略、profile 与健康总览；
- `GET /services/networks`、`nodes`、`edges`：稳定 code 游标列表与过滤；
- `GET /services/networks/flows`：稳定 Tick/sequence 游标的 batch、path 与 segment；
- `GET /services/networks/facts`：命令、拓扑与结算事实历史；
- `GET /services/networks/diagnostics`：弱连通分量、孤立节点、供需孤岛、最近边利用率、瓶颈/饱和边，以及指定 source/sink/probe units 的只读可达性诊断。

前端：

- 在城市模拟工作台增加“网络”分页；
- 拓扑图与 CLASSIC 地图叠层共享真实 node/edge API；
- source、sink、junction、isolated/failed edge 使用可访问的形状与状态色，不只依赖颜色；
- 选择 allocation 可高亮完整 path 和逐段 input/output/loss；
- 筛选、分页、刷新和命令更新保留已有图，不卸载组件、不整页闪烁；
- 大图只渲染视口或聚合层，详情通过侧栏按需加载。

## 11. 依赖顺序与验收

1. 版本常量、引擎能力和设计文档；
2. migration：profile/policy/network/node/edge/fact/flow；
3. 纯图算法及舍入/溢出/稳定 tie-break 单元测试；
4. command normalization、授权、mutation 和数据库 guard；
5. F8.0 planner 接入及 allocation 扩展；
6. canonical/snapshot/replay/recovery；
7. query/handler/routes；
8. 前端 API/store/topology/flow inspector；
9. 真实 PostgreSQL 升级兼容和多路径瓶颈集成测试；
10. 全量后端、前端 typecheck/test/build 与长周期回归。

完成门槛：

- 直连升级场景的服务结果与 F8.1 完全一致；
- 两源、两需求、共享瓶颈、替代路径和双向共享容量结果固定；
- 相同 seed/命令得到相同 path hash 与世界 hash；
- 断边后只影响下一次 settlement，历史不变；
- 从 genesis replay verified，recovery 后 hash 完全相等；
- 任何伪造 path、超容量 segment、错误损耗或跨服务绑定均被数据库拒绝。

## 12. 实现状态与验证记录

F8.2 已按 `city-f8-v3` 完成纵向闭环：

1. migration 209 建立 profile、policy、network、node、edge、fact、batch、path、segment 表及复合外键、不可变触发器、projection write gate 和全量断言；
2. `network.configure`、`network.node.configure`、`network.edge.configure`、`network.edge.transition` 使用严格 JSON、owner 授权、world tick、幂等键和 expected version；所有投影更新检查影响行数，CAS 失配不会被静默吞掉；
3. network 退役前必须退役全部子资产；active node 的绑定/角色变更在 active edge 仍引用时拒绝；active edge 不允许通过 configure 偷换端点或方向；
4. 确定性整数残量图支持有向/双向共享容量、多路径、稳定 tie-break、逐段损耗、容量上界和 path hash；服务 allocation 与网络分解在同一事务发布；
5. profile、policy、topology、facts、flow decomposition 已进入 canonical、snapshot、genesis replay 和 verified recovery；重放会拒绝伪造退役链、活动端点换绑和活动边拓扑突变；
6. 六类投影查询与 diagnostics 查询均经过世界成员授权；diagnostics 只读复用正式路由器，不修改剩余容量，也不生成流量或事实；
7. Vue 工作台提供拓扑、容量、状态转换、逐路径/逐段流量、事实、连通分量、孤岛、瓶颈和路由探针；筛选与刷新保留旧投影直至新响应提交，并通过请求代次抑制过期响应；
8. 单元测试覆盖图算法、命令 normalization、mutation/replay 防篡改、诊断连通性/利用率/路由和查询约束；真实 PostgreSQL 集成覆盖 F8.1→F8.2 升级、基线兼容、结算、查询、诊断、replay 与 recovery。

当前切片完成后停止；F8.3 社会服务时间可达性仅保留为下一独立切片，不在 F8.2 内提前实现。
