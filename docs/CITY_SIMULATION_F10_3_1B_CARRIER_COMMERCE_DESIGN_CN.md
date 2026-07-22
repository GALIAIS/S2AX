# F10.3.1b / V24 承运服务合同与运费现金结算设计

版本：v1.1（2026-07-21）

状态：已完成。本文件定义并记录 `city-openworld-v24` 的 F10.3.1b 最小商业切片：为已经具备 V22 货运结算证据、V23 承运准备金和 carrier claim 追偿能力的世界，追加版本化的服务报价合同与真实运费现金结算。

实施已覆盖 `247_city_open_world_v24_carrier_commerce.sql` 的版本注册、V23→V24 paused upgrade、profile/contract/payment SQL guard，以及其后对 V22 order-level custody、V23 command lifecycle 和 V24 successor scope 的兼容修正（`248`–`250`）。`TestCityOpenWorldV24CarrierCommerceQuotesAndSettlesAfterTheNextTick` 与 `TestCityOpenWorldV24UpgradeCreatesFutureOnlyCarrierCommerceBaseline` 已在真实 PostgreSQL/Redis 集成环境通过；V22 与 V23 回归也一并通过。

## 1. 目标与非目标

V24 必须完成：

1. 对 V24 基线之后开始的每个 V22 freight settlement case，生成唯一且不可变的 carrier service contract；
2. 将服务量、卖方、系统 carrier actor、V23 carrier reserve firm、固定费率和报价金额一起封存；
3. 仅在 V22 case 已 `settled` 后，以真实 journal 将卖方现金转为承运 firm 收入；
4. 卖方现金不足时不透支、不虚构应收账款，也不修改 V22 case；下一 tick 以相同合同重试；
5. 将 contract/payment 纳入 canonical snapshot、replay、verified recovery、version vector、SQL assertion 与 seller-scoped API。

V24 明确**不**完成：动态路线距离定价、重量/体积目录、用户自定义费率、运费预授权、账期/应收应付、滞纳金、退款、争议、SLA、保险、自动 claim adjudication、车队、多 carrier 报价、在途库存、风险所有权、COGS、税或生产配方。它们必须使用新的版本和事实，不能改写 V22/V23 历史。

## 2. 前置版本与因果边界

```text
V15 dispatched order
       ↓ V16/V17/V18 custody
V22 settlement case / line / receipt
       ↓ V23 carrier reserve + optional claim recovery
V24 carrier service contract
       ↓ only after V22 case settled and on a later automatic tick
V24 carrier fee payment journal
```

- V24 新世界默认进入 `city-openworld-v24`；V23→V24 只能在暂停世界中、经审计升级。
- `baseline_tick` 是 V24 的 fee 准入边界。仅 `source_tick > baseline_tick` 的 V22 case 可生成 V24 contract；升级前已在运的 case 永不补费。
- Contract 可在 V22 case materialize 后生成；付款只能在 case 已 `settled`、且至少跨越一次 automatic tick 后发生。当前 tick 内的新 receipt 命令不会立即收费。
- `voided`、`awaiting_outcome`、`receiving` case 不产生付款。V24 不将失败/未结 case 伪造为账款、债务或 carrier claim。

## 3. 费率与付款责任

V24 的初始 catalog 固化为：

```text
fee_per_cargo_unit = 1 base-unit
quoted_fee = Σ(case_line.quantity_units) × fee_per_cargo_unit
payer = V22 case 的唯一 source seller firm
payee = V23 system_freight_reserve firm
```

这不是现实完备的距离/重量运价；它只建立稳定、可审计的**服务量→报价→现金**路径。每条 V15 line 的单位价格已为正，因此按 `quantity_units` 计价不会依赖 float、地图坐标或后续可变路线投影。未来 V25+ 若要引入距离、重量、时段或服务等级，只能为新 contract version 写入明确的报价输入和 hash，不能更改 V24 已封存 quote。

付款 journal 必须在每个主体内部平衡：

```text
Dr seller.transfer_expense    quoted_fee
Cr seller.cash                quoted_fee

Dr carrier.cash               quoted_fee
Cr carrier.revenue            quoted_fee
```

V23 的 `system_freight_reserve` 继续是唯一 payee。它仍禁止 generic ledger command 直接注入、支出或 reversal；V24 只能经 carrier-commerce reducer 向其写入 fee journal。

## 4. 数据模型

### 4.1 Profile

`city_open_world_carrier_commerce_profiles` 封存：

- profile/version/content hash/baseline；
- V16 carrier actor 与 V23 carrier firm code；
- contract/payment contract、`fee_per_cargo_unit`、每 tick 上限；
- contract、payment、quoted unit、paid unit 与 paid amount 的只增计数器。

Profile 不是余额真相；卖方和 carrier 的 cash/revenue/expense 始终由 `city_journals` 与账户投影决定。

### 4.2 Service contract

`city_open_world_carrier_service_contracts` 为 V22 case 的不可变报价证据：

- 一条 V22 case 最多一个 contract；
- 绑定 case code、source kind/code、source tick、唯一 seller firm、carrier firm/actor；
- 封存 billed cargo units、unit fee 与 quoted fee；
- 不保存可变 payment state，也不作为第二套运输或库存投影。

### 4.3 Fee payment

`city_open_world_carrier_fee_payments` 是实际结清的不可变现金事实：

- 一条 contract 最多一条 payment；
- 只有 `settled` V22 case 才能创建；
- 绑定 contract、case、seller、carrier、tick、fee、journal；
- 现金不足时**不插入** payment；“待结清”由 `contract + settled case + no payment` 派生，后续 tick 以稳定排序重试；
- 不存在部分付款、欠款、手工冲销或隐式费用豁免。

## 5. 自动顺序与确定性

V24 automatic stage 位于 V22 settlement materialization 后、V9 mobility 前：

1. 锁定 profile；
2. 以 `(source_tick, case_code)` 稳定排序，为符合 baseline 的 V22 case 创建 contract；
3. 以 `(case.source_tick, contract.code)` 稳定排序查找已 settled、未 payment 的 contract；
4. 锁定 payer/payee accounts；可支付则 post `freight_fee` journal，再 append payment；不可支付则跳过且不改变任何余额；
5. 更新只增 profile counter，并执行 V22/V23/V24 assertion。

自动 stage 在命令执行之前运行，因此同 tick 的 V22 receipt 最早在下一个 tick 进入 V24 payment 候选集合。每个 SQL projection 都有 write gate；snapshot recovery 仅在 recovery gate 下重建。

## 6. 版本、恢复与可见性

- V23→V24 paused upgrade 创建空 profile，不回填旧 case/contract/payment；新内容通过 `sub2api-open-world-carrier-commerce-catalog` 写入 version vector。
- Snapshot 的 `carrier_commerce` 仅保存 code、stable entity code、tick、fee、source cursor 和 journal cursor；不保存 database id。
- Recovery 先恢复 V22 case、再恢复 V23 reserve、最后恢复 V24 contract/payment；replay checkpoint 验证每个 payment 对应 settled case、原始 quote 与已经存在的 journal。
- `GET /api/v1/city/worlds/:world_id/open-world/carrier-commerce`：owner/system administrator 可读取完整 profile/contracts/payments；普通成员仅可读取其作为 seller 的 contract/payment，不能读取其他 firm、准备金余额、政府 funding 或账户行。

## 7. 验收门槛

1. V24 genesis 和 V23→V24 upgrade 均创建 profile、保留所有 predecessor state，并且升级前 source tick 不被补费；
2. contract 对每个新 case 唯一、卖方唯一、计费单位与 quote 可由 V22 lines 重算；
3. settled case 在有足额 cash 时只产生一次四行 `freight_fee` journal；seller expense/cash 和 carrier cash/revenue 分别平衡；
4. cash 不足不产生负数、payment、journal 或隐式 debt；现金恢复后只按同一 quote 结清一次；
5. voided/未结 case 永不收费；V22/V23 的 receipt、refund、claim/recovery 和账本历史不被改写；
6. SQL guard、version vector、snapshot/replay/recovery、seller-scoped API 与 V22/V23 回归全部通过。

## 8. 后续拆分

- **V25**：报价输入扩展、服务级别和 SLA 时钟/违约事实；
- **V26**：保险 firm/policy、claim adjudication 与受限付款责任链；
- **V27+**：在途库存、风险/所有权转移、COGS、税、生产配方、行业投入产出与破产。
