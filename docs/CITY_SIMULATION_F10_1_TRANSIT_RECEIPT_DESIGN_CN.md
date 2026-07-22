# F10.1 / V17 在途货物、到达观察与收货确认底座

版本：v1.0（2026-07-20）

状态：已实现并完成 V17 版本向量、PostgreSQL 集成、replay/recovery 与升级验证。本文锁定 `city-openworld-v17` 的边界：它在 V15 合同库存与 V16 货运需求之间补上可审计的**货物在途状态**和**到货后收货门槛**，但不伪造车辆坐标、不会把 V9 路线完成直接解释成库存到帐。

## 1. 目标与非目标

V15 已能将企业订单从 `proposed` 推进到 `dispatched`，并在既有 `order.deliver` 命令中一次性完成库存转移；V16 又能把已 dispatch 的订单映射成容量受限的 V9 freight demand。但 V16 的 `route_completed` 只代表网络层运输完成：它不代表买方收货，不代表卖方库存已经转出，也不能作为支付、损耗或责任转移的隐式捷径。

V17 的目标是建立一个可扩展的中间协议：

1. 为 V17 基线之后产生的每个可运输 V16 source 建立一个不可变 shipment；
2. 以 V16 的 append-only transport evidence 驱动 shipment 的 `awaiting_route → in_transit → awaiting_receipt` 状态；
3. 只有已经看到 `route_completed` 的 shipment 才允许沿用 V15 的 `open_world.supply_order.deliver` 命令；该命令仍是唯一实际库存转移与 reservation release 的入口；
4. 在同一事务内把 V15 delivery evidence 固化为 V17 receipt，从而把 shipment 终止为 `received`；
5. 对 V16 expired、voided、orphaned 形成对应的 shipment 终态，不让未收货的订单被伪造为已到帐；
6. 给未来的拆单、部分收货、货损、保险、运费、独立 in-transit balance、承运人排班和多车队留下稳定的事实边界。

本版明确**不做**：

- 不建立第二份可扣减的 `city_inventory_balances`；V15 的卖方库存仍在收货命令实际执行前保持其账面归属，并由 reservation 防超卖；
- 不自动替用户提交 `order.deliver`，不把 V9 或 V16 的 completed 视为买方已经验收；
- 不重写 V15/V16 历史，V17 upgrade 前已经产生的 source 保持 legacy 行为；
- 不实现部分数量、分批车辆、货损、拒收、税费、COGS、应收账款或真实车辆连续位置；这些都需要单独的事实和账务合同。

因此此处的“在途库存”是**货物 custody / quantity obligation 投影**：它精确说明哪些订货行正在等待运输或等待收货，但不会与 F3 的实际库存余额相加，避免同一单位货物被双记。

## 2. 版本、兼容与边界

```text
V15: order / reservation / settlement / atomic inventory delivery
                         │ dispatched fact
                         ▼
V16: dispatch → V9 freight demand → scheduled/completed observation
                         │ immutable V16 evidence
                         ▼
V17: shipment custody → awaiting receipt ── buyer delivery command
                                                   │
                                                   ▼
                                            V15 atomic transfer + V17 receipt
```

- `city-openworld-v17` 是 V16 的严格后继；新世界默认使用 V17；
- `city-openworld-v16` 与更早版本继续以原有快照、replay 和 read API 运行，不会被 V17 表或规则回写；
- V16→V17 upgrade 只在 upgrade baseline 创建 V17 profile。**早于或等于 baseline 的 V16 source 不回填成假造的 V17 历史**，它们被定义为 legacy source，仍可按 V16 规则交付；
- baseline 之后生成的 V16 source 必须在同一 tick 的 V17 stage 建立 shipment。任何缺失 shipment 的新 source 都会被 DB 约束和 replay 验证发现；
- V17 只消费 V16 source，绝不重新运行 V9 路由计算，也不改变 V10 arrival bridge 的 exclusion。

## 3. 权威状态机

```text
V16 demand_pending ────────────────────────► awaiting_route
V16 route_scheduled ───────────────────────► in_transit
V16 route_completed ───────────────────────► awaiting_receipt
                                                    │
                         V15 order.deliver + atomic transfer
                                                    ▼
                                                 received

V16 demand_expired ─────────────────────────► expired
V16 voided ─────────────────────────────────► voided
V16 transport_orphaned ─────────────────────► orphaned
```

允许的 V17 transition：

```text
""              → awaiting_route
awaiting_route  → in_transit | expired | voided
in_transit      → awaiting_receipt | orphaned
awaiting_receipt→ received | orphaned
```

`received`、`expired`、`voided`、`orphaned` 均为终态。V17 不把 `route_completed` 直接变成 `received`：后者必须存在同订单、同一张 V15 delivery record、对应的 V15 `order.delivered` fact、已 posted 的 F3 resource operation 和 V17 receipt fact。

## 4. 数据与事实协议

### 4.1 Profile

`city_open_world_enterprise_freight_receipt_profiles` 固定：

- profile/version/hash、baseline tick；
- `shipment_contract = v16_source_custody_snapshot_v1`；
- `receipt_contract = v15_atomic_delivery_receipt_gate_v1`；
- `legacy_contract = pre_v17_source_legacy_delivery_v1`；
- 最大 shipment 和每 tick observation 限额；
- 由投影重算的 state、fact、transition、receipt counter。

静态 policy hash 与 version vector content catalog 一起封存。动态 counter 不作为业务事实，只用于可观测性和 DB 交叉校验。

### 4.2 Shipment 与 line snapshot

一个 shipment 一一对应一个 V16 source / V15 order。它复制 source/order/node/hub/行集合的稳定身份和 `requested_units`，并保留 source root evidence。`shipment_lines` 是只读 snapshot，行数量必须等于 V16 source line 总和；它不是可修改库存余额。

### 4.3 Evidence fact

每个 V17 fact 至少精确引用一种已 posted 的前序证据：

- `enterprise_freight`：指向 V16 `source.created`、`demand.requested`、`route.scheduled`、`route.completed`、`demand.expired`、`demand.voided`、`transport.orphaned` 事实；
- `supply_chain`：仅在 receipt 时指向同订单 V15 的 `order.delivered` fact。

事实行保存 source/order、tick、sequence、evidence kind 和外键；数据库检查“恰有一种 evidence”。transition 只引用 V17 fact，状态、reason、fact type 与 predecessor state 必须匹配。

### 4.4 Receipt

`city_open_world_enterprise_freight_receipts` 与 shipment/order/V15 delivery/resource operation 一一对应。它不是额外的库存操作，而是对既有 V15 原子 transfer 的验收证明。

## 5. 命令、权限与时间顺序

V17 不新增普通用户 command type，而是收紧既有 `open_world.supply_order.deliver`：

1. V17 tracked shipment 的订单只能在状态为 `awaiting_receipt` 时执行；
2. 该命令原有的 buyer/owner/admin 授权、V15 状态机、库存 transfer 和 reservation release 仍完全生效；
3. V15 `order.delivered` fact 和 delivery projection 写入后，同一事务写入 V17 receipt fact/transition/receipt row；
4. 事务提交时，V17 profile/receipt 与 V15 delivery 的双向一致性被 deferred assertion 验证；
5. V17 upgrade 前的 legacy source 不受该门槛影响，避免为已发生的 V16 历史伪造 receipt chain。

自动观察在 V15 command expiry 之后、V16 enterprise freight 之后、V9 mobility 之前运行。这样 V17 只看到已经封存的 V16 projection，并且新 shipment 不会在创建事实的同 tick 获得 V9 调度。

## 6. Recovery、replay、隐私与 API

canonical runtime state 包含 profile、shipment、line、fact、transition 和 receipt。recovery 按 evidence identity 重新解析 V16 / V15 外键；它不根据当前库存、当前路由或 UI 状态猜测历史。

replay 至少验证：

1. shipment 的 source、order、行、node/hub 与 V16 source 一致；
2. 每个 V17 observation 只消费正确、已出现的 V16 fact，且没有 future evidence；
3. `in_transit` / `awaiting_receipt` 与其 V16 route 状态一致；
4. `received` 的 V15 delivery、`order.delivered` transition、resource operation、V17 receipt evidence 四者一一对应；
5. V15/V16 历史 source 不会因 V17 upgrade 被替换或伪造。

只读 API 返回当前用户可见企业参与的 shipment、行摘要、状态时间线和 receipt cursor。普通成员不读取其他企业 shipment、V16 carrier 内部信息、数据库主键、命令 payload 或全局计数；world owner/system administrator 可读取完整运营视图。

## 7. 验收矩阵

1. V17 genesis、V16→V17 paused upgrade 和 version vector 都封存 profile，不更改 predecessor evidence；
2. V16 dispatch source 只能生成一个 V17 shipment，suppressed source 不会伪造在途货物；
3. route scheduled/completed 只通过对应 V16 fact 进入 `in_transit`/`awaiting_receipt`；
4. 无 completed evidence 的 V17 tracked order 被 `order.deliver` 拒绝；
5. completed → deliver 在同一 tick 命令路径后形成 V15 transfer + V17 receipt，一次且仅一次；
6. expired、voided、orphaned 不产生 receipt、库存 transfer 或二次结算；
7. migration guards、canonical state、replay、verified recovery、upgrade 和 PostgreSQL integration 测试均覆盖。

本实现额外验证了 V16→V17 baseline 前 source 的 legacy delivery 不会被 receipt gate 误拦截、V17 tracked shipment 的提前交付会被拒绝、到达后 V15 原子库存转移与 V17 receipt 只会各写入一次，以及 `transport_orphaned` 终态不会产生 receipt 或库存变更。V17 recovery 会按受控顺序解除 shipment 与 receipt evidence 的派生引用，确保 provenance 外键不影响恢复重建。

## 8. 后续扩展接口

V17 固化的 shipment/evidence/receipt 边界支持后续独立版本扩展：

- V18：订单拆分、批次 shipment、部分 receipt、拒收与损耗；
- V19：独立 in-transit inventory balance、所有权/风险转移、COGS 与 carrier custody accounting；
- V20：承运人 fleet、车辆位置、装卸时间窗、运输费与 SLA；
- F11：企业生产配方、采购计划、市场报价与多节点库存调拨。

这些版本必须追加新事实与新 profile；不得把新含义塞进 V15/V16/V17 的既有状态字段。
