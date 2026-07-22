# F10.3.0 / V22 分批收货、货损、拒收与承运责任结算

版本：v1.0（2026-07-20）

状态：已实现并验证。本文件冻结 `city-openworld-v22` 的首个 F10.3 切片：它把 V17 单 shipment 与 V18 overflow consignment 的“已到达、等待收货”状态，连接到可重复、按行按数量结算的实际库存、资金调整和承运责任证据。它不重写 V15–V21 的历史事实，也不把系统 carrier 伪装成已经拥有现金账户的企业。

## 1. 问题与目标

V15 的交付是整单原子库存转移；V17 将单 shipment 的 route completion 收紧为 `awaiting_receipt`；V18 将超额订单拆成多个 consignment，但仍要求全部到达后才允许一笔 V15 原子交付。因此下列真实情况尚无可信表达：

- 一辆/一批货物只收到了其中一部分；
- 一部分货物在途中损毁、短少或被买方拒收；
- 一张订单的前几批已经验收，而后续批次仍在运输；
- 卖方先向买方退款，但损失责任最终应归于系统承运人；
- 同一订单的库存可用量、订单保留量、付款资产与退款金额必须同时守恒。

V22 的目标是建立一个可长期扩展的 **freight settlement overlay**：

1. 仅对 V22 baseline 之后的 V17 shipment 或 V18 consignment 建立可审计 settlement order/case；
2. 一个 case 可以收到任意多次 append-only receipt，每次按 V15 原始 line 记录 accepted、lost、rejected 三种整数数量；
3. 每次 receipt 立即写入真实资源操作：accepted 从卖方库存转入买方库存，lost 从卖方库存消费，rejected 保留在卖方库存；
4. lost/rejected 按冻结单价即时退还买方，抵销 V15 acceptance purchase 中相应的 inventory、cash 与 revenue；
5. `seller` 或 `carrier` 被显式记录为非接受数量的责任方。当前系统 carrier 没有独立经济实体时，卖方先完成退款，`carrier` 责任只创建可追索 claim，绝不伪造 carrier 现金转账；
6. 每一条已解析数量即时从 V15 reservation 的有效占用中扣除；最后一个 case 结清后才写入完整 reservation release 与 V15 `settled` 终态；
7. V17/V18 仍保留其 transport provenance，但在 V22 case 全部解析时由新的 `settled` 终态关闭，不把 V22 receipt 冒充为旧 V15 atomic-delivery receipt；
8. canonical snapshot、version vector、replay、recovery、SQL deferred assertion、成员只读 API 与升级都验证同一因果链。

## 2. 版本与兼容边界

```text
V15 accepted / dispatched order
              │
              ├── V17 shipment ───────────────┐
              │                                │
              └── V18 batch consignments ─────┤ transport completion / terminal evidence
                                               ▼
V22 settlement order → settlement case(s) → repeated line receipt(s)
                                               │
             ┌──────── accepted ──────────────┼── transfer source → buyer inventory
             ├──────── lost ──────────────────┼── consume source inventory + refund/claim
             └──────── rejected ──────────────┴── retain source inventory + refund/claim
                                               │
                                 all cases resolved
                                               ▼
                       V15 order.settled + full reservation release
```

- 新世界使用 `city-openworld-v22`；V21→V22 只能在 paused world 上升级。
- V22 profile 的 `baseline_tick` 将 upgrade tick 或 genesis tick 封存。只有 source/plan 的 source tick **严格大于** baseline 才进入 overlay；更早的 V15–V21 shipment、batch、receipt、allocation、admission 永远保持原语义。
- V22 不为历史 V17/V18 `awaiting_receipt` 反向创建 case；这会伪造本来不存在的 partial-receipt 合同。
- 一张订单恰好属于一种运输入口：V17 normal shipment 或 V18 overflow plan。V22 不允许两种入口或 V15 旧 atomic-delivery gate 与 overlay 同时结算同一订单。
- V22 只新增 `settled` 作为 V15 的后继终态。它只允许从已 dispatched 的 V22 overlay order 进入；旧版本不能产生该状态，也不会因升级被回填。

## 3. 领域模型

### 3.1 Profile

`city_open_world_freight_settlement_profiles` 封存：

- profile/version/content hash/baseline；
- `source_contract = v17_shipment_or_v18_consignment_v1`；
- `receipt_contract = append_only_line_quantity_outcome_v1`；
- `resource_contract = immediate_partial_transfer_loss_consumption_v1`；
- `financial_contract = accepted_price_refund_and_carrier_claim_v1`；
- `liability_contract = seller_refund_or_carrier_claim_v1`；
- maximum order/case/receipt/line/claim 限额与投影 counter。

静态 policy 进入 version vector；counter 只能由 append-only receipt、case 与 claim 反推，不承担业务真相。

### 3.2 Settlement order 与 case

一个 `settlement_order` 一一对应一个未来 V17 shipment 或 V18 batch plan，冻结：

- V15 order、buyer/seller node、carrier、入口类型和入口 code；
- source/destination hub、source tick、required quantity、source runtime fact；
- 状态 `awaiting_transport → receiving → settled`，或在 V15 未产生任何 V22 receipt 时进入 `voided/blocked` 投影；`voided` order 的每个已物化 case 同步为 `voided`，不修改 V17/V18 custody。
- case 数、已解析数量、accepted/lost/rejected/refund/claim 聚合值。

一个 V17 shipment 产生一个 case；一个 V18 consignment 产生一个 case。case 仅在其 transport evidence 到达 `awaiting_receipt` 或终止为 `expired/voided/orphaned` 后才出现。case 复制对应的冻结 line split，并具有：

- `awaiting_outcome → receiving → settled`；
- transport terminal case 不允许 accepted quantity；
- 每个 case 所有 receipt line 的 resolved quantity 总和必须恰好等于冻结 case line quantity 后才可成为 `settled`。

### 3.3 Receipt、资源与资金

每条 receipt 由买方授权的 command 创建，含一个或多个 line outcome：

```text
line.resolved = accepted_units + lost_units + rejected_units
line.resolved <= line.quantity - historical_resolved
```

- `accepted_units`：一次 `transfer` resource operation 的 source out 与 buyer in；
- `lost_units`：一次 `consumption`-like source out（与 accepted source out 合并为同一资源 operation 的 out line）；
- `rejected_units`：不移动物理库存；
- 所有 `lost + rejected` 按冻结 unit price 形成退款金额；退款 journal 以 `purchase` 类型且带 V22 metadata 记录：buyer cash debit、seller cash credit、seller revenue debit、buyer inventory credit；
- `carrier` 责任额额外写入 immutable claim。它不是虚构的 carrier 收款；当前卖方退款已完成，未来有 carrier enterprise/insurance 后才由新版本执行 claim recovery；
- receipt 后 V15 reservation 的 **有效未释放数量** 减少 resolved quantity。最终 V15 release 只在所有 case fully settled 时写入，不能重复释放。

### 3.4 V17/V18 bridge

V22 不写 V17/V18 的旧 receipt 表，因为那些表严格证明“一笔 V15 atomic delivery”。相反：

- V17 shipment 与 V18 consignment 增加 terminal `settled` projection；
- transition 只能由对应的 fully settled V22 case 触发，并保存 V22 receipt/source fact 的 cursor；
- V18 plan 在所有 consignment `settled` 时变为 `settled`；
- V15 `order.settled` 只能在全部 expected case 已 settled 后写入。

旧 receipt (`received`) 与 V22 settlement (`settled`) 是两个互斥、不可互相伪造的合同。

## 4. 命令、权限与同 tick 顺序

新增命令：

```text
open_world.freight_settlement.receipt
```

Payload：

```json
{
  "case_code": "freight.settlement.case.…",
  "liability_party": "seller | carrier",
  "lines": [
    {
      "source_line_no": 1,
      "accepted_units": 8,
      "lost_units": 1,
      "rejected_units": 0
    }
  ]
}
```

只有 buyer node 的成员、world owner 或管理员可提交；卖方和 carrier 不能替买方伪造验收。权限沿用 V15 buyer/seller node ownership，并在 command 入队和应用时重复验证。

自动顺序：

```text
T automatic:
  V15 expiry
  V16 freight source
  V17 custody observation
  V18 batch observation
  V22 settlement order/case observation
  V9 mobility scheduling
  …
T command batch:
  V22 receipt → resource operation / refund journal / case bridge / possible V15 settled
```

同 tick 完成的 V17/V18 transport evidence 会先被 V22 observation 看到；其 case 可在该 tick 的 command batch 收货。V22 不修改 V9 路线、V20/V21 capacity admission 或已 posted transport fact。

`open_world.supply_order.deliver` 对 V22 overlay order 必须拒绝；`fail` 在已有任何 V22 resolved quantity 时也必须拒绝，防止对已经部分转移的库存和退款执行旧式整单 reversal。没有 V22 receipt 的已跟踪 order 可执行既有 V15 fail：V15 写 `failed`、释放 reservation 并完整 reversal，V22 order/case 写 `voided` 且不生成 receipt/resource operation/refund/claim，也不把 V17/V18 custody 伪造为 settled。`cancel` 仍只适用于 V15 的 pre-dispatch 生命周期。

## 5. 不变量与防御

1. 每个 V22 order 恰好映射一个 post-baseline V17 shipment 或 V18 plan，且两者互斥；
2. case lines 精确覆盖其 shipment/batch 的冻结 quantity 与 unit price；
3. receipt line 的历史累计永不超过 case line；case/order aggregate 均由 receipt line 重算；
4. accepted/lost/rejected 的资源操作、退款金额、claim amount 都精确等于同一 receipt line 的整数数量 × 冻结价格；
5. `carrier` claim 只覆盖该 receipt 的 loss/rejection refund，`seller` responsibility 不创建 claim；
6. 已解析 V22 quantity 从 V15 active reservation 计数中扣除；未解析 quantity 永远不超过当前可用 source inventory；
7. V15 `settled` 订单必须有完整 V22 order/case/receipt coverage、全部 reservation release、没有 V15 delivery，且 acceptance settlement 不被伪造为 full reversal；
8. V17/V18 `settled` 只引用 fully settled V22 case，不会与旧 `received` receipt 共存；
9. PostgreSQL trigger 仅允许 bootstrap、runtime、V22 command 或 verified recovery 写 projection；所有 assertion 在 commit 时交叉验证前序 evidence；
10. 所有 user-visible query 只返回其 buyer/seller firm 可见的摘要、数量、状态、责任方和 public cursor；不泄露 carrier actor 内部 id、其他企业订单、command payload 或全局 claim 数据。

## 6. Replay、recovery、升级与 API

canonical runtime state 追加 profile、settlement order/case/line/receipt/outcome line/claim，以及 V17/V18 settled bridge。recovery 先恢复 V15/V16/V17/V18，再按 immutable source/order/line/fact identity 重建 V22；最后验证 V15 settled 与 V17/V18 bridge。它绝不根据当前库存或当前 UI 估计历史短少。

replay 至少验证：

1. V22 baseline 前没有 case/receipt/claim；
2. order/case source、line、carrier、hub 与 V17/V18 predecessor 一致；
3. every receipt quantity、resource operation、refund journal、claim 与 source fact 连续；
4. V15 reservation effective quantity、inventory balance 与所有 partial receipt 的时序一致；
5. V15 `settled`、V17/V18 `settled` 和 V22 aggregate 一一闭合；
6. 重新回放和恢复得到相同 canonical hash。

只读 API：

```text
GET /api/v1/city/worlds/:world_id/open-world/freight-settlements
```

返回 order/case、冻结 line summary、累计 accepted/lost/rejected、refund、claim、状态时间线和 cursor；成员过滤与 V17/V18 一致。

### 6.1 已完成验收

- 新建 V22 world 会在同一 genesis transaction 中初始化 V5–V21 前置投影、V22 profile 和 version vector；V21 的 effective-capacity 前置初始化与 V5 social-runtime identity 均明确支持 V22。
- V17 单 shipment 已通过两次 append-only receipt 验证：第一次部分 accepted 不会结单，第二次 accepted/lost/rejected 后才创建 refund、carrier claim、V15/V17 settled successor 与完整 reservation release。
- V18 overflow plan 已通过两个 consignment 独立结算验证：第一个 case settled 后订单仍为 receiving/dispatched；最后一个 case settled 后 V18 plan 与 V15 order 才共同进入 settled，旧 V18 receipt/delivery 不会被伪造。
- V21→V22 paused upgrade 以 upgrade tick 封存 baseline；升级前 V17 `awaiting_receipt` custody 不回填，升级后 source tick 才可进入 overlay。
- V15 fail 的 no-receipt compatibility path 已验证：跟踪 case 在第一条 V22 receipt 前可被明确 void；第一条 partial receipt 后旧 fail 被拒绝，V15 保持 dispatched，回放与恢复保留这两种分支。
- V22 snapshot/replay/recovery、成员 scoped read、未授权 viewer receipt 拒绝、SQL assertion 和 migration contract 均由自动测试覆盖。

## 7. 明确不做与后续接口

V22 不建立独立 in-transit inventory balance、所有权/风险即时转移、真实 carrier firm/fleet、运费、保险赔付、税、COGS、应收应付、自动争议裁决或多企业生产配方。这些会在事实边界稳定后分阶段进入：

1. F10.3.1：carrier firm / insurer / claim recovery、运费与 SLA；
2. F10.3.2：独立 in-transit inventory、风险/所有权转移、COGS 与库存估值；
3. F10.4：多企业注册、采购计划、配方、投入产出和产能调度；
4. F10.5：行业市场、破产、信用与财务指标。

任何后续版本只能追加新事实和 projection，不能改变 V22 已封存 receipt、resource operation、refund 或 claim 的含义。
