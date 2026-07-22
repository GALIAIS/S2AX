# F9.2.C / V16 企业货运 Source 适配层设计

版本：v1.1（2026-07-20）

状态：已实现并通过定向单元、迁移与 PostgreSQL 集成验证。本文冻结 `city-openworld-v16` 的边界：它把 V15 已经
`dispatched` 的企业订单，作为 V9 聚合货运网络的可审计需求来源；它不把
“路线完成”偷换为“实物收货”。

## 1. 目标、非目标与前置事实

V15 已经建立企业订单、库存 reservation、acceptance settlement、dispatch、
delivery 和 terminal reversal。V9 已经建立容量受限的 `freight` 聚合网络。
二者此前故意没有直接连接；否则 UI 或单个 reducer 很容易把“计划运输”错误
地写成库存转移。

V16 只补上下列窄接口：

1. 只读取 V15 的 `order.dispatched` 及其当前状态、订单行和端点；
2. 在下一离散 tick 创建一条不可变的企业货运 source，并用系统 carrier
   actor 创建一个 V9 `freight` demand；
3. 观察 V9 demand / route 的 `scheduled`、`completed`、`expired` 事实，保留
   货运运输证据；
4. 如果 V15 订单在 V9 排程前进入 `failed`、`cancelled`、`expired` 或
   `delivered`，使待排程 demand 以 V9 的 `expired` 事实终止；
5. 如果订单在 route 已经排程后终止，只写“运输孤儿化”事实，保留历史容量
   allocation，不篡改已发生的 V9 路由；
6. 禁止 route completion 自动创建 V15 `order.delivered`、F3 resource
   operation、库存转移、运费或任何钱包变化。

V16 不做：连续车辆、司机、车道、在途库存、收货签收、部分交付、损耗、
运费、保险、税、运输市场、路线重规划或多段跨城市物流。这些都需要后续
独立版本和新的事实协议。

## 2. 权威边界

```text
V15 order.dispatched fact + dispatch record + frozen order lines
                         │
                         │  T + 1 only
                         ▼
V16 freight source ─── V9 freight demand ─── V9 route / capacity allocation
       │                       │                         │
       │                       └── expired / voided       └── completed observation
       │
       └──────────────── no inventory / no settlement / no V15 state write ───────┐
                                                                                     │
future explicit receipt adapter ── receipt fact ── V15 order.deliver command/semantics
```

权威性严格如下：

| 问题 | 唯一权威 |
|---|---|
| 合同双方、货物行、reservation、款项、订单状态 | V15 supply chain |
| 路径、容量、拥堵、路线开始/完成 | V9 mobility |
| V16 source 与 V9 demand 的因果映射、货运语义状态 | V16 enterprise freight |
| Actor 的局部位置变更 | V10 arrival bridge；V16 freight 永远不参与 |
| 实物所有权和数量 | F3 `city_inventory_balances` / resource operation |
| 交付 | V15 `order.delivered`，未来 receipt adapter 显式触发 |

任何一层都不能把自己推导出的投影回写为另一层的真相。特别是 V16 不会创建
第二套“在途库存”或企业余额。

## 3. 因果时序与状态机

### 3.1 固定因果边界

```text
T:   command apply: V15 order.dispatch
     V16 automatic pass 已结束，因此不能同 tick 生效

T+1: V16 automatic pass 从 sealed V15 dispatch 创建 freight source + V9 demand
     V9 pass 不调度 requested_tick == T+1 的 demand

T+2+: V9 按原有容量、最短路径和 deadline 调度；V16 在随后的 tick 观察结果
```

这与玩家 V9 request、V11 OD、V13/V14 commute source 相同：producer 写在
当前 tick，V9 scheduler 最早下一 tick 消费。不得为了“更快”破坏这种边界。

### 3.2 货运 source 状态机

```text
created
  └─> demand_pending ──> route_scheduled ──> route_completed
          │                    │                   │
          ├─> demand_expired   └─> transport_orphaned
          ├─> voided
          └─> route_completed  (补投影：V9 已完成但 scheduled 观察未落盘)

created ──> suppressed   (仅基础设施契约不满足时；不能静默丢单)
```

- `created` 和 `demand_pending` 在同一 V16 事务写入；
- `route_scheduled`、`route_completed`、`demand_expired` 只观察 V9 的已封存
  状态，绝不重算路径；若恢复、批处理或状态追赶时只剩 completed 证据，允许从
  `demand_pending` 直接补投影为 `route_completed`，但绝不伪造一条不存在的
  `route_scheduled` V9 事实；
- `voided` 只适用于仍为 V9 `pending` 的 demand。V16 追加一个 V9
  `mobility.expired` fact 并走 V9 原有 metric/policy 计数更新；
- `transport_orphaned` 说明 V15 合同在 route 已排程或完成后已终态。V9 的
  capacity allocation 是已发生历史，不能删除或伪造释放；
- `route_completed` 只是“运输网络到达已被观察”，不是收货、不是库存所有权
  转移、也不是 V15 `delivered`。

V15 在 source 创建前已经 `delivered` 或任何终态时，V16 不创建 source；这
不是遗漏，因为 dispatch 仍可在 V15 审计中完整追踪。

## 4. 端点、单位和 carrier

### 4.1 端点冻结

每个 source 永久复制下列输入，而不是在未来按当前配置重新查找：

- `order_code`、V15 dispatch fact cursor `(tick, sequence)` 和 dispatch tick；
- seller / buyer supply-chain node code；
- 两个 node 对应的 facility mobility hub code；
- 每个 order line 的 resource、quantity、价格和源/目的企业/区域 identity；
- V15 `dispatch_deadline_tick` 仅作为合同来源审计字段。

V15 的 dispatch deadline 不是运输 deadline：它已经在 dispatch 前被消费。
V16 demand deadline 由 source 创建时冻结的 V9 `maximum_wait_ticks` 计算，
即 `source_tick + maximum_wait_ticks`。这样既不把已过期的合同 deadline 错当
运输 SLA，也不会让后续修改 V9 profile 重写历史 demand。

### 4.2 系统 carrier actor

V9 demand 的既有 schema 要求 `actor_id`。V16 因此在 genesis / V15→V16
paused upgrade 建立一个无 owner 的 `system.freight.carrier` actor：

- `actor_type_code = system.freight_carrier`；
- 不拥有局部坐标、角色、钱包或用户权限；
- 只能被 V16 automatic pass 使用，普通命令不能把它作为玩家角色操作；
- 所有 V16 demand 固定 `mode_code = freight`、`purpose_code = enterprise.freight`；
- requested unit 由订单所有行的数量总和映射为有上限的 cargo capacity units；
  本版的映射规则本身冻结在 profile/hash 中。尽管 V9 通用 request schema
  接受最多 1000 单位，冻结 freight 图的最窄 local edge 仅有 **32** cargo
  units/tick；V16 目前是一张订单对应一条原子 demand，故可调度上限固定为
  **32**。总量超过 32 的订单必须生成可审计的 `suppressed` source，且不创建
  V9 demand；不能截断、拆单或让一个永远不可调度的 33–1000 单位 request 悄悄
  超时。拆单、车队和在途库存只能由后续独立 logistics 版本引入。

carrier 是 schema adapter，不代表一辆真实货车；未来车队系统必须作为新
actor/vehicle contract 加入，而不能修改既有 source 的 actor identity。

### 4.3 V10 隔离

V16 demand metadata 含 `transport_adapter.kind = enterprise_freight_v1` 和
`arrival_bridge = excluded`。V10 arrival 注册查询必须显式排除这类 demand。
因此 completed freight route 永远不会尝试 materialize carrier、移动坐标或
生成 actor landing。

## 5. 持久化、事实与数据库门禁

V16 新增以下 world-scoped projection：

```text
city_open_world_enterprise_freight_profiles
city_open_world_enterprise_freight_sources
city_open_world_enterprise_freight_source_lines
city_open_world_enterprise_freight_facts
city_open_world_enterprise_freight_transitions
```

profile 固定 profile ID/version/hash、四个合同字符串、system carrier、单
tick source 上限和 counters。source 是每个 V15 order 一条的唯一映射；line
单独保存不可变的 copied snapshot，避免 metadata 被当成可更新明细表。

每一个 V16 状态变更同时具备：

1. 一个 posted `city_open_world_runtime_facts` 事实；
2. 一个指向该 runtime fact 的 V16 freight fact；
3. 一条 append-only freight transition；
4. 受 session-scoped V16 write gate 保护的 source/profile 投影更新。

V9 demand 的 source / last fact 和 V16 fact 的 runtime fact cursor 都必须
可交叉验证；恢复时只从 canonical state 重新建立投影，不能临时生成新需求。
迁移会同时将 V9/V10/V11–V15 的 write gate successor 范围扩展到 V16，保留
既有 V15 命令在 V16 world 中工作。

## 6. 终止、失败与一致性

V16 从 V15 当前 transition 读取终止状态，不自行猜测合同是否完成。每个 tick
先把已有 V9 transport 状态投影到 source，再处理 V15 terminal：这样同一窗口中
已被 V9 排程/完成的 demand 不会被错误当作 pending void，也不会遗漏对应的
transport evidence。

| V15 状态出现时机 | V16 行为 |
|---|---|
| source 尚未建立 | 不建立 source |
| V9 demand 仍 pending | 追加 V16 `voided` 与 V9 `mobility.expired`；更新 V9 指标，绝不分配路线 |
| V9 route 已 scheduled | 追加 `transport_orphaned`；保留 route/allocation 历史 |
| V9 route 已 completed | 追加 `transport_orphaned`；不产生 receipt 或库存变更 |

取消 V9 pending demand 使用既有 V9 `expired` 状态，而不是另造一个绕开 V9
守卫的 `cancelled` 状态。它的 fact payload 明确记录 `enterprise_freight_order_terminal`，
使运营面板能区分容量超时和合同终止。

## 7. 回放、恢复、升级和读取

### 7.1 canonical / replay

V16 canonical state 包含完整 freight profile、source、line、fact、transition。
replay checkpoint 验证：

- 所有 source 的 V15 dispatch cursor、订单、node、hub 和 line snapshot；
- source 的 V9 demand / route cursor 与状态机对应关系；
- V16 fact / transition 的 tick、sequence、parent/runtime cursor；
- V10 不存在对应 freight arrival；
- terminal source 不会具有新的 active demand；
- V15 supply-chain、V9 mobility 和 V10 arrival 的静态 predecessor checkpoint
  均未变化。

### 7.2 recovery

verified recovery 的顺序为：V15 supply chain → V9 mobility/V10 arrival 的
既有投影 → V16 carrier → freight profile/source/line/fact/transition。恢复只
根据已有 canonical evidence 重新连接 V9 demand / route 的自然键，不复制
库存、不补发 source、不补写 V15 delivery。

### 7.3 upgrade

仅允许 paused `city-openworld-v15 → city-openworld-v16`：

- 保留所有 V15 order、V9 demand/route、V10 arrival 和 snapshots；
- 建立 profile 与 system carrier；
- 不追溯旧 dispatch 为 freight source，除非 upgrade 后自然推进一个新的 tick；
- 新世界从 V16 genesis 起包含全部 V15/V16 foundation。

## 8. API、权限与可观测性

新增只读端点：

```text
GET /api/v1/city/worlds/:world_id/open-world/enterprise-freight
```

world owner / system administrator 看完整 source、事实及 V9 cursor。普通成员
只看其拥有 buyer 或 seller firm 参与的 source，且只得到本合同相关的 line、
node/hub code、状态与匿名化 counters；不暴露 carrier 内部 ID、其他企业 source、
系统全局 revision、V9 其他用户 demand 或 route metadata。

运营面板必须显示：dispatch tick、source tick、V9 earliest departure/deadline、
路线状态和 cursor、拥堵/完成时间、合同终止原因、`voided/orphaned` 标识和
“transport complete is not delivery”的明确说明。

## 9. 验收矩阵

1. 新 V16 world 与 paused V15→V16 upgrade 都建立唯一 carrier/profile；
2. dispatch 在 T，source/demand 最早在 T+1，V9 最早在 T+2 调度；
3. source 元数据是 V15 dispatch/order/line/node/hub 的冻结副本；
4. V16 不在 source、schedule、complete、expire 或 orphan 时写 V15 order、
   F3 resource operation 或 F2 journal；
5. V10 对 freight route 不建立 arrival，carrier 不会被移动；
6. V15 terminal 在 pending 阶段使 V9 demand 只发生一次带理由的 expiration；
7. V15 terminal 与 V9 排程/完成同窗口出现时，先落 transport observation，再
   保留 V9 allocation 并仅标记 orphan；
8. canonical、from-genesis replay、verified recovery、V15→V16 upgrade 和
   PostgreSQL migration/integration tests 都覆盖成功、超时、pending void、
   scheduled orphan、completed observation 与读取范围。

## 10. 后续依赖

V16 完成后，F9.3 才可把当前 V9 设施 hub 扩展为多模式道路、货运站、仓储和
跨城市节点；F10.1 的在途库存、收货、生产、成本、运费和多企业供需则必须
消费 V16 source/route evidence，通过一个新 receipt adapter 显式产生 V15
delivery 语义。银行、利率、股票等经济附属分支继续排在完整生产与可验证财务
报表之后。
