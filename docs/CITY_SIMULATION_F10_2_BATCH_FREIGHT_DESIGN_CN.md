# F10.2 / V18 批次货运与全量收货门槛

版本：v1.1（2026-07-20）

状态：已实施并通过 PostgreSQL 端到端验证。本文定义 `city-openworld-v18` 的第一段物流扩容：把 V16 因单条 V9 freight edge 容量上限而抑制的超额订单，确定性拆成多个可审计 consignment，并把它们全部完成作为既有 V15 原子交付的前置条件。

## 1. 解决的问题

V16 的一个 `enterprise_freight_source` 必须映射为一条 V9 freight demand；冻结的最窄货运 edge 容量为 32 cargo units/tick。因此大于 32 单位的已 dispatch 订单会得到 `suppressed` source，避免伪造永远不可排程的单条 demand。V17 正确地忽略 suppressed source，因而它也不能把这种订单误报为在途货物。

这保证了 V16/V17 的诚实性，但超额订单不能拥有真实运输证据。V18 补上这一缺口，而不改写既有 V15/V16/V17 事实：

```text
V15 dispatched order
        │
        ▼
V16 suppressed overflow source (immutable predecessor)
        │
        ▼
V18 batch plan ── deterministic packing ──► N consignments (1..32 units each)
        │                                      │
        │                                      ▼
        │                               V9 freight demand / route
        │                                      │
        └──────── all consignments arrived ────┘
                                               │
                                               ▼
                                  V15 one atomic inventory delivery
                                               │
                                               ▼
                                  V18 one receipt proof per consignment
```

V18 **不是**部分库存交付。所有 consignment 只是同一 V15 order 的运输批次；卖方库存保留、买方可用库存、现金结算与 reservation release 仍只由原有 V15 `open_world.supply_order.deliver` 原子命令处理。这样不会在 V15 以外创建第二份余额、半成品现金流或不完整的所有权转移。

## 2. 版本与兼容

- 新世界使用 `city-openworld-v18`；V17→V18 upgrade 只能在 paused world 上创建 V18 profile，并把 upgrade tick 记为 baseline。
- **只有** `source_tick > V18 baseline`、状态为 V16 `suppressed`、原因是固定容量上限的 source 会被 V18 接管。V18 前的 source 是 legacy，不补造 batch、route 或 receipt，也不改变其既有 V15 delivery 语义。
- 小于等于 32 单位的 V16 source 继续走 V17 shipment/receipt 路径；V18 不复制这些订单，也不让同一 order 同时进入两种 receipt gate。
- V17 profile 和历史 shipment 在 V18 中仍受验证、replay 和 recovery 保护。它们只处理 V18 baseline 前的 V17 路径；V18 overflow plan 永远不依赖 V17 伪造的 shipment。

## 3. 数据契约

### 3.1 Profile

`city_open_world_freight_batch_profiles` 固定：

- profile/version/content hash/baseline；
- source contract：`v16_suppressed_overflow_source_v1`；
- packing contract：`stable_line_capacity_packing_v1`；
- transport contract：`v9_freight_consignment_demand_v1`；
- receipt contract：`all_consignment_arrivals_then_v15_atomic_delivery_v1`；
- 单 batch 32 units、每 order 至多 128 batches、每 tick 64 plan/observation 工作限额；
- plan、consignment、状态、fact、transition、receipt 的投影计数器。

计数器只用于投影完整性与监控；身份、数量和前序证据仍以 append-only 行为准。

### 3.2 Batch plan 与 consignment

一个 plan 一一对应一个 V16 overflow source 和一个 V15 order。它复制 V16 的 source/destination hub、carrier、dispatch source 和总数量。每个 consignment 是该 plan 中顺序稳定、总量不超过 32 的运输单元，拥有独立 V9 demand/route 与状态机。

line packing 遵循：

1. 按 V16 frozen line number 顺序遍历；
2. 当前 consignment 尽量填满 32 units；一行可在多个 batch 之间确定性切分；
3. 每个 split 保留原 `resource_code`、单价及精确整数 `quantity × unit_price`；
4. 所有 consignment lines 对每一个 V16 source line 的总和必须等于原数量；
5. plan 总量、每一 batch 总量与 V16 source 总量必须完全守恒。

### 3.3 Evidence、transition 与 receipt

- plan root 通过 V16 source root runtime fact 建立因果边；
- `consignment.created` 与 `demand.requested`、`route.scheduled`、`route.completed`、`demand.expired`、`demand.voided`、`transport.orphaned` 都只引用已 posted 的 runtime fact；
- V15 `order.delivered` 仅在所有 batch 为 `awaiting_receipt` 时才能发生；其单个 resource operation 会被每个 consignment 各自的 `receipt.confirmed` 事实引用；
- 多个 V18 receipt 合法地共享同一个 V15 delivery/resource operation，因为它们是同一原子交付的多个运输证明，而非多次库存转移。

状态机：

```text
awaiting_route → in_transit → awaiting_receipt → received
awaiting_route → expired | voided
in_transit     → orphaned
awaiting_receipt → orphaned
```

`received`、`expired`、`voided`、`orphaned` 终态不可回退。plan 状态从 consignment 状态确定性归约：只要有 active batch 即为 active；全部 awaiting receipt 时为 ready；全部 received 时为 received；出现终态且未完整 received 时为 blocked。`blocked` 不能调用 delivery。

## 4. Tick 时序与命令门槛

```text
tick T
  V15 automatic expiry
  V16 observes prior dispatch → overflow source is suppressed
  V17 ignores suppressed source
  V18 creates/observes/reconciles batch consignment evidence
  V9 schedules only older pending consignment demand
  ...
  command batch: V15 deliver allowed only when every V18 batch awaits receipt
```

V18 不新增客户端可伪造结果的命令。现有 delivery 命令会先执行 V17 legacy gate，再执行 V18 overflow gate；恰好一种 gate 可以拥有该 order。收货后 V15 原子库存 transfer、reservation release 和 V18 N 个 receipt 在同一数据库事务内完成。任何 runtime evidence、plan 或 receipt 缺失都拒绝提交，而不是降级为无运输交付。

若 V15 order 在所有 batch 到达前 `cancelled`、`expired` 或 `failed`：pending demand 被审计性地 expire/void；已 schedule/completed 的 consignment 标记 orphaned。V18 不试图把已经发生的 V9 route 倒带，也不偷偷释放或转移库存。

## 5. 不变量、恢复与查询

数据库 trigger 只允许 bootstrap、runtime/recovery 受控上下文写入 V18 projection。deferred assertion 至少验证：

1. V18 plan 的 source 是 baseline 后的 V16 suppressed overflow source；
2. plan/consignment/profile counter、batch 数量和状态机一致；
3. consignment line 之和与 V16 source line 精确守恒；
4. 每个 demand/route 的 hub、carrier、mode、数量、前序 runtime fact 与 consignment 一致；
5. receipt 只来自 ready consignment，且同 V15 delivery/order/resource operation 对应；
6. V17 legacy shipment 与 V18 overflow plan 不会争夺同一 order；
7. canonical snapshot、replay checkpoint 和 recovery 能重建同一 plan、batch、事实、transition 和 receipt 语义。

只读 API：

```text
GET /api/v1/city/worlds/:world_id/open-world/freight-batches
```

world owner/system administrator 看完整运营视图；普通成员只看其所属 buyer/seller firm 的 plan、batch 状态、数量和 public cursor，不看数据库主键、carrier 内部信息、命令 payload、其他企业订单或全局计数。

## 6. 验收

1. 64-unit 和跨 line overflow order 被稳定拆成多个不超过 32-unit consignment；
2. 每个 consignment 必须在下一 tick 后才可由 V9 调度，且全部 V9 completion 后才允许 V15 delivery；
3. 提前 delivery、缺一 batch、终态 batch 和双 gate 冲突均被拒绝；
4. 完整 delivery 只产生一个 V15 resource operation，但产生每 batch 一张 V18 receipt；
5. legacy V17 source、V17→V18 upgrade、snapshot/replay/recovery、成员范围查询、SQL guard 与 PostgreSQL integration 均通过；当前实现已用 64-unit overflow order 验证两个 32-unit consignment、提前交付拒绝、单一 V15 resource operation、双 receipt、replay 与 recovery；
6. V15 订单/库存/现金/settlement 的既有守恒不因 batch 数量改变。

## 7. 后续边界

V19 才能在 V18 的 batch identity 上增加部分库存 receipt、拒收、损耗、承运责任、运费、保险与独立 in-transit balance。它必须用新的 resource operation / ledger fact 表达比例结算；不能把 V18 的多个 receipt 误解为多次库存转移，也不能更改已封存的 V15/V16/V17/V18 事实。
