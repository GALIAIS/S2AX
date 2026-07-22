# F10.3.1a / V23 承运准备金与理赔追偿设计

版本：v1.1（2026-07-21）

状态：已完成并验证。本文件冻结 `city-openworld-v23` 的第一个 F10.3.1 切片。它为 V22 已生成的承运责任 claim 建立真实、可审计的经济付款主体与准备金闭环；不把运输 actor 误当作经济实体，也不提前宣称已经实现保险、运费、SLA 或在途库存。

## 1. 问题与边界

V22 已经能在货损/拒收时完成买方退款，并在责任属于 carrier 时创建不可变的 `carrier` claim。但 V16 的 `system.freight.carrier` 只是开放世界 actor：它没有货币账户、资本、费用或付款能力。因此直接把 V22 claim 标成已解决、或伪造 carrier 到卖方的现金流，都会破坏账本和因果链。

V23 只解决下列问题：

1. 为既有 V16 系统承运 actor 建立一个独立、无用户所有者的 **carrier reserve firm**；
2. 用真实政府资金建立/补充准备金，绝不在理赔时凭空造钱；
3. 以一条已入账的双重记账 journal 解决一条 V22 open carrier claim；
4. 将 V22 claim 的 `open -> resolved` 作为 V23 append-only recovery evidence 的受控投影；
5. 让升级前和升级后的 V22 open claim 都能被人工追偿，且回放、恢复、SQL assertion 和 scoped API 能证明相同事实。

V23 **不**实现：第三方保险公司、保费/运费计价、SLA 自动赔付、争议/仲裁、分期理赔、部分理赔、追偿坏账、在途存货、风险所有权、税/COGS、车辆/车队或多 carrier 竞价。这些留给后续版本，不能借 V23 的 profile 或 metadata 偷渡。

## 2. 版本、兼容与主权

```text
V16 system.freight.carrier actor     （交通执行身份，无现金账户）
                │ immutable code link
                ▼
V23 carrier reserve firm             （经济付款主体，无 user owner）
                │
                ├── reserve.fund command（政府准备金拨付）
                └── freight_claim.resolve command（V22 claim 追偿）
                              │
                              ▼
                 V22 claim state: open → resolved
```

- 新世界默认使用 `city-openworld-v23`；V22→V23 只能通过 paused-world audited upgrade。
- V23 profile 记录 upgrade/genesis `baseline_tick`，但 **不**把它当作 V22 claim 的准入门槛：任意符合 V22 合同且仍为 `open` 的 carrier claim 都可以由 V23 解决。这是新后继事实，不会重写 claim 的原始金额、receipt、refund 或责任归属。
- V22 仍然拥有 claim 的原始事实（claim code、receipt、case、amount、created tick）；V23 仅拥有 funding/recovery 事实和受控 `resolved` 投影。
- `system.freight.carrier` actor 与 `system_freight_reserve` firm 是不同表、不同身份。profile 固化二者 code 的一对一关系，禁止通过 actor id 或任意 firm 进行替换。
- carrier reserve firm 带 `opening_policy=manual_reserve_only` 元数据；它被排除出通用自动 opening balance。新/升级世界均从零准备金起步，必须走可审计 fund command。

## 3. 领域模型

### 3.1 Profile

`city_open_world_carrier_recovery_profiles` 封存：

- profile/version/content hash/baseline；
- V16 carrier actor code、V23 reserve firm code；
- funding / claim / recovery contract；
- 每 tick funding/recovery 限额、每笔金额限额；
- funding、recovery、已注入金额、已支付金额 counter 和 revision。

profile counter 是 append-only funding/recovery 表的投影，不是资金真相；可用准备金始终以 reserve firm 的 `cash` account 为准。

### 3.2 Funding

`city_open_world_carrier_reserve_fundings` 是一条不可变的政府拨款事实：

- 由 `open_world.carrier_reserve.fund` 命令创建；
- 绑定 source command、政府 entity、carrier firm、amount、tick 与 journal cursor；
- 同一 command 只能生成一条 funding；
- 只允许世界 owner 或系统管理员，普通成员不能耗用公共财政。

记账为一条 `subsidy` journal：

```text
Dr city_government.subsidy_expense     amount
Dr system.freight.reserve.cash         amount
Cr city_government.cash                amount
Cr system.freight.reserve.equity       amount
```

政府与 carrier 的 cash 都不允许为负，因而不足额财政会整条命令拒绝，不产生半条 funding 或脏 projection。

### 3.3 Claim recovery

`city_open_world_freight_claim_recoveries` 是一条不可变的 resolved claim 事实：

- 每条 recovery 一一对应一个 V22 `carrier` claim；
- source claim 必须 `open`，且必须可经 V22 case line 唯一定位到 active seller firm；
- 只允许全额解决，金额严格等于 V22 `claim_amount`；
- 同一 claim 不能被两次 recovery；唯一键、row lock 和 `UPDATE ... state='open'` 共同提供并发安全；
- 成功时 V22 claim 更新为 `resolved`，metadata 只追加 V23 recovery code/cursor，不改变原始责任金额。

记账为一条 `cash_transfer` journal（四行、全局平衡）：

```text
Dr system.freight.reserve.transfer_expense   claim_amount
Dr seller.cash                                claim_amount
Cr system.freight.reserve.cash                claim_amount
Cr seller.other_income                        claim_amount
```

这表达“卖方先退款给买方；carrier reserve 之后补偿卖方”。它不会再次修改买方库存、买方 cash、V15 reservation 或 V22 refund journal。

### 3.4 状态机

```text
V22 claim open
      │  owner/admin resolve, reserve cash sufficient
      ▼
V23 recovery resolved + posted journal
      │
      ▼
V22 claim resolved
```

没有 `pending`、`partial` 或 `rejected` 持久状态：命令在 transaction/savepoint 内全成或全败。业务性拒绝包括：claim 不存在、已解决、责任方不匹配、carrier reserve 不足、seller 不唯一、每 tick 限额和版本不支持。

## 4. 命令、权限与同 tick 顺序

```text
open_world.carrier_reserve.fund
{ "amount_units": 10000, "memo": "initial reserve" }

open_world.freight_claim.resolve
{ "claim_code": "freight.settlement.claim.…", "memo": "carrier recovery" }
```

- 命令入队与 reducer 均要求 world owner 或 system administrator；二次检查可防止权限在队列等待期间被撤销。
- 两类命令进入 V23 `open_world_carrier_recovery` stage。它位于 V22 settlement 后、V20/V21 infrastructure/mobility 之前；V23 没有自动理赔，只有明确命令。
- 同 tick 先执行 V22 receipt，再执行 V23 recovery；因此同 tick 生成的新 claim 可在后续 command sequence 立即解决，但无法越过 reserve funding/claim 的同一 command 原子边界。
- funding/recovery 都使用标准 journal sequence，成功后递增；失败回滚 savepoint，不消耗 sequence。

## 5. 持久化、写门与 SQL 不变量

新表：

```text
city_open_world_carrier_recovery_profiles
city_open_world_carrier_recovery_fundings
city_open_world_carrier_recoveries
```

写门与 V15–V22 一样区分 bootstrap、normal write 与 recovery write。V23 migration 同时将 V22 settlement write/assertion 的允许版本扩展到 V23，因为 V23 需要受控更新 claim state；旧 V22 world 的行为保持不变。

Deferred SQL assertion 至少验证：

1. profile 固定 hash/contract/counter 与真实 funding/recovery 行数、金额总和一致；
2. V16 actor 为 active、unowned `system.freight_carrier`，V23 firm 为 active、unowned `firm`，且 metadata 声明 manual reserve；
3. 每条 funding 的 command、government/career firm、journal、tick、金额和 journal 余额关系一致；
4. 每条 recovery 的 claim、seller、carrier、command、journal、金额一致，claim 恰有一条 recovery；
5. `resolved` V22 carrier claim 恰有一条 V23 recovery，`open` claim 没有 recovery；
6. recovery journal 不得使 reserve cash 为负，且具体借贷行反映 carrier expense/cash 与 seller cash/other income；
7. recovery 不得替换、删除或修改 V22 receipt、resource operation、refund、claim amount/case/receipt identity。

## 6. Snapshot、replay、recovery 与版本向量

canonical `CityOpenWorldRuntimeState` 增加 `carrier_recovery`。profile、funding 与 recovery 的 code、tick、amount、journal cursor、command sequence 进入 snapshot；数据库 storage id 不进入 canonical hash。

- recovery 先清除 V23 rows，再恢复 V15–V22 predecessor，最后恢复 V23；V22 claims 在 restore 后按 source command cursor 重新关联；
- V23 restore 以 recovery write gate 插入 rows，并受 SQL assertion 验证；
- replay 在 V22 checkpoint 之后验证 V23：每个 recovery 的 journal 和 claim transition 都不能晚于 checkpoint；
- version vector 使用 `sub2api-open-world-carrier-recovery-catalog`，锁定 profile hash、actor/firm code、三种 contract、限额和 manual reserve policy；普通运行不允许 vector 漂移。

## 7. API 与可见性

```text
GET /api/v1/city/worlds/:world_id/open-world/carrier-recovery
```

- world owner/system admin：看到完整 profile、funding history、recoveries、carrier code 和 aggregate；
- 普通成员：只看到其 seller firm 可见的 recovery 与对应 claim 摘要；不返回 reserve firm code、reserve balance、government funding、其他 firm claim 或 journal lines；
- 资金与理赔写入继续经通用 `POST .../commands`，不提供绕过 command/replay 的专用写 API。

## 8. 验收与后续拆分

V23 验收覆盖：

1. V23 genesis/upgrade 初始化 carrier actor/firm link，carrier 不获得自动 opening cash；
2. reserve fund 的四行 journal、余额、counter、未授权拒绝、超额财政拒绝；
3. V22 carrier claim 在无准备金时拒绝、fund 后可解决、重复解决拒绝；
4. claim `open -> resolved`、seller compensation、carrier expense、V22 receipt/refund 不变；
5. member scoped query、SQL assertion、snapshot/replay/recovery、V22→V23 upgrade；
6. 旧 V22 open claims 与 V23 后新增 claim 都能在同一合同下被解决。

已执行的回归证据包括：V23 genesis 的版本向量/content catalog、V3 worldgen 与社会运行时契约；V22→V23 paused upgrade 后保留历史 open claim 并在 funding 后解决；V22 货运结算回归；以及 V23 的 snapshot/replay/verified recovery、成员 scoped API 和迁移结构测试。

下一切片（F10.3.1b / V24）才引入：carrier 服务合同、按 route/weight/distance 形成 freight fee、SLA 时钟、保险 policy/insurer firm、claim adjudication 和可配置付款责任链。F10.3.2 之后才可加入在途库存、风险转移、COGS 与库存估值。
