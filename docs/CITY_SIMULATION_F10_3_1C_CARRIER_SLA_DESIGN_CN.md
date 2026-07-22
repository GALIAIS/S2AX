# F10.3.1c / V25 承运服务等级与 SLA 事实设计

版本：v1.0（2026-07-21）

状态：实施设计。本文件定义 `city-openworld-v25` 的最小服务等级与 SLA 事实层。它建立在 V16/V17/V18 的货运运输证据、V22 的部分收货结算、V23 的准备金追偿和 V24 的现金运费合同之上，但不重写其中任一版本的历史含义。

## 1. 目标与范围

V25 只完成一件可审计的事：对 V25 baseline 之后创建的每个运输单元（一个 V17 shipment 或一个 V18 consignment）封存一个不可变的 `standard` 承运服务承诺，并以运输事实而不是用户收货动作判定是否按时完成。

该切片必须提供：

1. 按 source kind/code 一对一生成 service commitment，冻结 seller、系统 carrier、服务等级、source tick 和已存在的 V9 mobility deadline；
2. 在同一自动阶段观察 V17/V18 的 transport state 与其 append-only transition 证据；
3. 以 `fulfilled`、`breached` 或 `cancelled` 形成唯一终态，并为创建、合同绑定、终态各保留独立 append-only event；
4. 当 V24 以后续 tick 为同一 source 生成 carrier service contract 时，只绑定该既有 contract，不修改价格、费用、付款、V22 case 或总账；
5. 将 profile、commitment、event 纳入 canonical snapshot、replay、verified recovery、version vector、SQL assertion 和 seller-scoped read API。

V25 明确不实现：动态运价、客户可选加急/优先级、罚金、自动退款、credit/应收、保险理赔、争议、手工改 SLA、载具、在途库存、风险/所有权转移、COGS、税、生产配方或信用评分。这些能力只能通过后续版本追加新的事实和账务边界。

## 2. 为什么 SLA 绑定运输单元，而不是 V22 case

```text
V15 dispatched order
       ↓
V16 freight source
       ↓
V17 shipment ────────────────┐
V18 consignment ─────────────┼── V25 service commitment / SLA clock
       ↓                      │
V17/V18 route completion      │ transport evidence only
       ↓                      │
V22 receipt / settlement case │ user-controlled receipt is downstream
       ↓                      │
V24 service contract/payment ─┘ optional later causal link
```

V22 case 只会在 V17/V18 运输已达到可收货状态后物化；若从 V22 case 才开始计时，SLA 会遗漏真实的运输等待和路径执行时间，并把收货命令的延迟误归责给 carrier。因此 V25 的 SLA 来源是已封存的 V17 shipment 或 V18 consignment：

- V17：复制其 V16 freight source 的 `mobility_deadline_tick`；
- V18：复制该 consignment 自己 V9 mobility demand 的 `deadline_tick`；
- `awaiting_receipt` 的原始 transition tick 表示 carrier 已完成运输；它不等同于 V22 receipt、库存入账或 V15/V22 settled；
- V24 contract 只是与同一 source 建立的后续商业引用，不能成为 SLA 起点或改变 SLA 判定。

## 3. 不可变 profile 与服务承诺

### 3.1 Profile

`city_open_world_carrier_sla_profiles` 仅封存：

- profile/version/content hash/baseline；
- carrier actor code 与唯一 `standard` service level；
- source、deadline、event、contract-link contract 名称；
- 每 tick 最大创建/观察数量和只增 counters。

它不保存可变费率、现金、余额、客户规则或实时延迟。V25 的 `standard` 仅是一个冻结的可观察服务等级，而非一个可购买、可修改或可结算的产品目录。

### 3.2 Commitment

`city_open_world_carrier_sla_commitments` 一条对应一个 `(source_kind, source_code)`：

- `source_tick` 和 `deadline_tick` 从 predecessor 事实复制，不能由当前墙钟或可变配置重新计算；
- seller/系统 carrier identity 在创建时冻结；
- `carrier_service_contract_code` 初始可为空，只有 V24 对同一 source 的唯一 contract 出现时才可一次性绑定；
- `state` 是受 event 驱动的当前投影，`finalized_tick` 和 `completion_tick` 只在终态写入；
- `version` 只随受控 contract link 或唯一终态递增。没有 DELETE、没有手工 UPDATE、没有重开。

### 3.3 Event

`city_open_world_carrier_sla_events` 是 SLA 的权威变化证据，事件 code 由 commitment 和事件种类确定：

- `commitment.created`：承诺已从 V17/V18 source 物化；
- `carrier_contract.bound`：同一 source 的 V24 contract 已存在，且只绑定一次；
- `deadline.met`：运输到达证据的 tick 不晚于 frozen deadline；
- `deadline.breached`：运输 source 明确 expired、到达晚于 deadline，或 deadline 后仍没有到达；
- `commitment.cancelled`：运输 source 在 breach 之前被 V15 terminal 事实置为 voided/orphaned。

创建、绑定和终态每类最多一次。终态不允许反转；迟到后即使运输随后到达，也只保留 breach，不把 projection 改回 fulfilled。

## 4. 判定协议与因果顺序

V25 automatic stage 位于 V24 carrier-commerce 后、V9 mobility 调度前：

1. V16/V17/V18 已在同 tick 先创建或同步其运输投影；
2. V22 物化任何已可收货 case，V24 可为已有 V22 case 创建 quote；
3. V25 按 `(source_tick, source_kind, source_code)` 稳定排序创建尚不存在的 commitment；
4. V25 对 active commitment 读取 V17/V18 当前 state 和原始 arrival transition：
   - arrival tick `<= deadline_tick`：`fulfilled`；
   - source `expired`、arrival tick `> deadline_tick`，或当前 tick 已越过 deadline 仍无 arrival：`breached`；
   - source `voided`/`orphaned` 且尚未 breach：`cancelled`；
5. 对有 V24 contract 的 source 写唯一 contract-binding event；
6. 更新 profile counters，再执行 V22/V23/V24/V25 assertions。

V25 不把 V22 receipt、V15 payment/reversal、V23 recovery 或 V24 payment 当作 carrier 的按时服务证据。它也不由“当前时间”修改历史：世界 tick 逐步推进，所有 deadline 的比较只针对已封存的整数 tick。

## 5. 版本、升级、恢复与可见性

- 新 V25 world 在 genesis 创建 profile，baseline 为 tick 0；只跟踪 `source_tick > baseline_tick` 的 V17/V18 source。
- V24→V25 paused upgrade 创建空 profile；upgrade tick 之前的 shipment/consignment、V22 case、V24 contract/payment 一律不回填 commitment，也不被重新判定。
- version vector 以 `sub2api-open-world-carrier-sla-catalog` 封存 V25 profile hash、source/deadline/event/link contracts、service level 和上限。
- snapshot 仅存稳定 code、tick、source、deadline、state、event 和 V24 contract code；recovery 先还原 V22/V23/V24，再还原 V25，避免 storage ID 进入 canonical state。
- `GET /api/v1/city/worlds/:world_id/open-world/carrier-sla`：owner/system administrator 读取完整 profile、commitment 和 event；普通成员只读取其卖方 firm 的 SLA 状态与事件，不读取系统 carrier identity、全局 counters、其他 firm 或准备金资料。

## 6. 验收门槛

1. V25 genesis 与 V24→V25 paused upgrade 均创建 profile；upgrade 前 source 永不补写 SLA；
2. shipment/consignment 对每个 post-baseline source 至多创建一个 `standard` commitment，deadline 必须等于 predecessor 的 frozen mobility deadline；
3. `awaiting_receipt` transition 的原始 tick 在 deadline 内时仅产生一次 `deadline.met`；过期、迟到或 deadline 超时时仅产生一次 `deadline.breached`；
4. voided/orphaned 在 breach 之前只产生 `commitment.cancelled`，不得伪造 carrier breach；
5. V24 contract 只能一次性绑定到相同 source，不能改写 quote/payment 或成为 SLA 起点；
6. SQL guard、version vector、snapshot/replay/recovery、seller scope、V22–V24 回归和真实 PostgreSQL integration 全部通过。

## 7. 后续拆分

- **V26**：不可变报价输入包（距离、重量、时段、服务等级）与多档服务 catalog；
- **V27**：SLA 补偿、保险 policy/claim adjudication 与严格受限的付款责任链；
- **V28+**：在途库存、所有权/风险转移、COGS、税、生产配方、行业投入产出、破产与更完整的市场结算。
