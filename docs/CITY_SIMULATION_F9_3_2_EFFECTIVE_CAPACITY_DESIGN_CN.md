# F9.3.2 / V21：基础设施有效容量与路径准入设计

状态：实现中
前置版本：`city-openworld-v20`
目标版本：`city-openworld-v21`
对应总体阶段：D（交通与空间基础设施）

## 1. 目标与非目标

V20 已封存 V19 node/corridor 的资产身份，并以事实记录维护、施工、封闭和限流状态；但 V9 在 V20 仍只读取冻结 edge capacity。本版本将两者通过一个**显式、可审计、只影响未来准入**的桥接协议连接起来：每个 V9 edge 的新 route reservation 必须使用其对应 V20 `corridor_segment` 的当时状态计算有效容量。

```text
V9 edge（冻结基准容量）
  └─ V19 corridor（稳定一对一空间身份）
       └─ V20 corridor_segment ordinal=1（当前状态 / 容量）
            └─ V21 admission（一次 route-edge 准入的不可变容量快照）
                 └─ V9 allocation / mobility.scheduled fact
```

V21 必须做到：

- `operational=1000` 时保持 V9 原有基准容量；`restricted` 按 milli 降低容量；`maintenance`、`construction`、`closed` 使该 edge 在新的 route 准入中不可用；
- 在有多条路线时，调度器在满足有效容量的 edge 子图内选择确定性最短路径，而不是因为冻结最短路径被封闭就直接让 demand 空等；
- 已经 scheduled/completed 的 V9 route、allocation、arrival、货运、receipt、库存和账本永不重算、撤销或改写；
- 每条 V21 后产生的 allocation 都有不可变 admission record，记录基准容量、当时 asset state、来源 transition fact、计算后的有效容量与 route schedule fact；
- V20→V21 是暂停世界的显式升级，不能隐式让旧 world 采用新容量规则。

本版本不做：

- 不引入逐车、车道、方向信号、时刻表、票价、路段长度拆分、施工成本/工期或资产耐久；
- 不让 `network_node`、港口、站台、供电或非交通资产影响 V9；首个 bridge 只消费 `corridor_segment/ordinal=1`；
- 不将 V21 容量减少追溯到已接受的 route，也不打断在途货物；
- 不建立独立的交通钱包、奖励、法律、库存或金融账本；
- 不在地图风格 reducer 中写日本、中国等国家分支。路线、走廊和 asset class 继续来自已封存的 V19 content/style binding。

## 2. 因果顺序与过渡语义

当前 tick 在普通命令之前运行 V9 自动 mobility pass：先完成到达的 route、过期 demand，并只调度较早 tick 创建的 pending demand；之后才执行普通 command。V21 不改变这个已验证的因果边界。

```text
tick T
  1. V9 自动调度读取 T 开始时已有的 V20 asset state
  2. 写入 T 的 mobility.scheduled / V21 admission（若能准入）
  3. 执行普通命令
  4. infrastructure.asset.transitioned 在 T 生效

tick T+1
  1. V9 自动调度第一次读取 T 的新 asset state
```

因此：

- T tick 内的资产 transition 不会回滚 T 中已经写入的 route 或 allocation；
- 其容量影响从 T+1 的自动调度开始；
- V21 之后的 transition fact 标记 `v9_scheduler_effect=next_tick_effective_capacity_v1`，并携带 `v9_scheduler_effective_from_tick=T+1`；
- V20 历史 fact 中的 `v9_scheduler_effect=none` 仍然正确：它们在当时没有调度器效果。V21 upgrade 的 `baseline_tick` 把这些历史状态作为新 bridge 的初始输入；
- route admission 始终记录实际读取的 state transition/baseline，而不是仅记录当前 state，故后续维护命令不会重写历史解释。

## 3. 容量计算与路径选择

对于 V9 edge `e`、其 V19 corridor `c`、V20 segment asset `a`，定义：

```text
base(e)      = V9 edge.capacity_units_per_tick
milli(a,t)   = V20 state.capacity_milli（调度时读取）
effective(e) = floor(base(e) × milli(a,t) / 1000)
```

实现采用整数分解而非浮点：

```text
floor(base / 1000) × milli + floor((base mod 1000) × milli / 1000)
```

结果规则：

- 输出范围为 `[0, base]`；`capacity_milli=0` 或低比例向下取整到 `0` 时，该 edge 不可准入；
- 若 `used(e, departure_tick) + requested_units > effective(e)`，该 edge 对当前 demand 不可用；
- 在同一 tick 中按既有 demand 稳定排序，每成功写入一条 route 都即时把其 allocation 加入本 tick `used`，后续 demand 能看到它；
- 可用 edge 子图上的路径代价继续是 V9 的 `base_travel_ticks`，并以 hub code/edge code 作为相同代价的稳定 tie-break；
- congestion occupancy 和 delay 使用 `effective(e)` 而非冻结 base capacity；
- 无可达的可用路径并不产生伪造的失败 route：demand 保持 `pending`，仍受原有 deadline/expiration 机制约束。

这意味着 V21 的“封闭”是**未来 reservation 的准入阻断**，不是路线撤销。未来版本若要模拟事故改道、疏散、在途中断或多段替代，必须另建显式 route-transition/custody 协议。

## 4. 权威状态与审计模型

新增 V21 state：

| 数据 | 可变性 | 权威来源 | 说明 |
| --- | --- | --- | --- |
| effective-capacity profile | 内容身份冻结；计数递增 | V21 reducer / recovery | 固定 bridge 和 admission 协议 |
| effective-capacity admission | append-only | V9 scheduler 与 `mobility.scheduled` fact 同一事务 | 每个 V21 route-edge reservation 的容量证据 |
| V20 asset state/transition | V20 权威 | V20 command reducer | V21 只读，不可写回 |
| V9 route/allocation | V9 权威 | V9 scheduler | `capacity_units_per_tick` 在 V21 route 中表示实际 admission capacity |

`CityOpenWorldEffectiveCapacityAdmission` 至少保存：

- `route_code`、`edge_code`、`departure_tick`；
- `corridor_code`、`asset_code`、`asset_state`；
- `baseline_capacity_units_per_tick`、`capacity_milli`、`effective_capacity_units_per_tick`、`allocated_units`、`occupancy_milli`、`delay_ticks`；
- `state_effective_tick`、可空 `state_source_fact`（baseline 无 fact）；
- 必填 `schedule_fact`，即相同 route 的 `mobility.scheduled` runtime fact `(tick, sequence)`；
- 可校验 metadata，包含 `effective_infrastructure_capacity_v1` 协议和 V19/V20 映射来源。

V9 allocation 仍是容量预约的主投影；V21 admission 是其跨域来源证据。它们一对一，避免既复制一份“当前有效容量”又复制一份“不明来源的流量”。

V21 profile：

```text
profile_id                 = sub2api-open-world-effective-capacity
profile_version            = 1.0.0
source_topology_contract   = v19_edge_corridor_mapping_v1
source_asset_contract      = v20_corridor_segment_ordinal_1_v1
admission_contract         = effective_infrastructure_capacity_v1
transition_visibility      = next_tick_after_command_v1
revision                   = 1 + admission_count
```

初始 profile 没有 admission；V20→V21 的旧 V9 allocations 不补写 admission，也不改变它们的容量字段。所有 `departure_tick > V21 baseline_tick` 的 allocation 必须恰有一条 V21 admission。

## 5. 数据库防护

迁移新增：

```text
city_open_world_effective_capacity_profiles
city_open_world_effective_capacity_admissions
```

约束/trigger 必须保证：

- 一个 V21 world 恰有一个 profile，并以 V20 infrastructure profile 为前置；
- 每个 V9 edge 恰可通过 V19 corridor 映射到一个 V20 `corridor_segment/ordinal=1`；
- admission 的 route/edge/allocations、schedule fact、asset/state/source fact、milli 公式、occupancy/delay 均与同一事务中的实际投影一致；
- `admission_count`、`revision` 与 admission 表精确对应；
- V21 基线以后每一个 V9 allocation 都必须存在一条 admission；升级前历史 allocation 允许没有该表记录；
- profile/admission 只允许 V21 bootstrap、scheduler write 或 verified recovery 上下文写入，禁止客户端、后台 SQL 或普通服务直接改写；
- V20 predecessor write gates 扩展到 V21，保证 V21 genesis 和后续基础设施 transition 仍经过既有审计 guard。

## 6. Snapshot、replay、recovery 与升级

canonical state 追加 `effective_capacity`：profile + 稳定排序后的 admissions。它不缓存可随 V20 state 改变的 edge capacity；当前 capacity 在调度时由 V9/V19/V20 权威状态确定。

恢复清理顺序：V21 admissions/profile → V20 infrastructure → V19 spatial network → V9 mobility 及其前置域；恢复写入顺序相反，在 V9 route/allocation、V19 corridor、V20 asset/state 都恢复后再写 V21 admissions。

回放分层验证：

1. V20 state timeline 已逐条证明 transition fact；
2. V9 route/allocation 已证明 `mobility.scheduled` fact；
3. V21 admission 再证明 route-edge 与 V19/V20 映射、历史 asset state、整数公式、当时累计 occupancy/delay 和 schedule fact 的一对一关系；
4. V21 profile 的静态协议在普通 tick 间不可变，计数和 admission 作为 fact-backed 动态投影验证。

V20→V21 upgrade 必须：暂停 world、验证 V20 foundation、切换版本/失效旧 state hash/递增 version-vector generation、写 V21 profile 基线和 V21 content-catalog binding、写新的 canonical snapshot/hash。它不重算旧 route，不回填旧 admission，不创建 runtime fact。

## 7. API、可观测性与权限

新增成员只读 API：

```text
GET /api/v1/city/worlds/:world_id/open-world/effective-capacity
```

响应包含 V21 profile、当前每个可映射 edge 的基准/有效容量与来源 asset state、以及 admission history。它不暴露账户、控制 grant、库存、管理者身份、密钥或内部事务 ID。

写入不新增浏览器直写接口：基础设施状态仍经统一 city command API；V21 scheduler 是 server-side automatic reducer。UI 应将容量显示为“从下一自动调度 tick 生效”，并能从 route/edge 链接到对应 asset transition 与 schedule evidence。

## 8. 验收矩阵

- 新 V21 world：每个 V9 edge 都有一对一 V19 corridor / V20 segment mapping；profile/version vector 正确；无伪造 admission；
- V20→V21：旧 demand/route/allocation/arrival/货运/receipt/库存/账本逐字保持；只有 profile/vector/snapshot 新增；
- 限流：在 `restricted` capacity 下 allocation 使用 floor 后的有效容量，occupancy/delay 基于有效容量；
- 封闭：关闭最短 corridor 后，若替代 path 有足够容量则确定性改道；若没有则 demand 继续 pending，最终按原 deadline 过期；
- 因果：在 tick T 命令 transition 不能影响 T 的自动调度，必须从 T+1 首次影响；
- 审计：每条 V21 allocation 都有一条 admission，精确引用 V20 baseline/transition 与 `mobility.scheduled` fact；
- SQL：直接写 V21 profile/admission、伪造 mapping/source fact、篡改容量公式或删改 admission 全部被拒绝；
- snapshot/replay/recovery：限制、封闭、替代路径、恢复状态都能跨数据库 ID 精确重建；
- 回归：V20 及更早版本继续使用原 V9 冻结容量和行为，绝不因部署 V21 而静默变化。

## 9. 后续边界

V22 以后可在不改写 V21 evidence 的前提下扩展：多 segment 走廊、node/station capacity、施工订单与工期、退化/检查、拥堵预测、时刻表、在途中断/事故 reroute、货物损耗与承运责任。每一项都必须重新声明其对已接受 route、V15 delivery、V17 receipt 和城市经济的因果边界。
