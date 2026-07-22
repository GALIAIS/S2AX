# F10.0 / V15 企业订单、库存保留与交付底座

版本：v1.0（2026-07-20）

状态：已实现为 `city-openworld-v15` 并完成服务层、迁移结构与 PostgreSQL 回归验证。F10.0 的唯一目标是建立能被回放、恢复与后续 freight 消费的企业间货物合同事实；它不提前实现企业 AI、连续车辆、企业破产、银行信贷或证券。

## 0. 实现状态与已验证边界

V15 已实现且锁定以下闭环：

1. 新世界与暂停的 V14→V15 upgrade 都封存 profile、两个基线企业/节点及其 storable inventory topology；旧 V15 world 仍可由订单事实在 replay 时补回历史的惰性零余额，不改变任何既有数量或订单事实；
2. `create → accept → dispatch → deliver` 的资源转移、acceptance journal、reservation/release、delivery cursor 与状态 transition 均在同一个 tick 事务中追加；
3. `cancel`、自动 `expired` 与 dispatch 后 `failed` 均会对已 acceptance 的订单完整释放 reservation，并只生成一次与 acceptance journal 一对一的 reversal；不会遗留被锁库存或不可逆付款；
4. runtime fact gate、F3 resource-operation gate 与 V15 delivery proof 已在迁移中连锁校验，不能以普通 transfer 命令伪造交付；
5. canonical snapshot、from-genesis replay 与 verified recovery 都覆盖交付、取消、失败、两种自动过期和 V14→V15 upgrade；
6. 普通成员的读取结果只含其自有 firm 参与的订单及相邻 node，所有计数都是该读取范围内的计数，且不返回 world 全局 revision；world owner 与系统管理员才读取完整经营视图。

已验证命令：

```text
go test ./internal/service -run 'Test(NormalizeCityOpenWorldSupplyChain|CityOpenWorldSupplyChainStaticCheckpoint|ReplayCityOpenWorldSupplyChainInventoryTopology|ProjectCityOpenWorldSupplyChainStateForOwnedFirms)' -count=1
go test ./migrations -run '^TestCityOpenWorldV15SupplyChainMigrationPinsFactBackedLifecycle$' -count=1
go test -tags=integration ./internal/repository -run '^(TestCityOpenWorldV15SupplyChainIsFactBackedRecoverable|TestCityOpenWorldV15SupplyChainTerminalPathsReleaseAndReverseExactlyOnce|TestCityOpenWorldV14UpgradeToV15PreservesSealedPredecessorEvidence)$' -count=1
```

## 1. 问题、范围与硬边界

V9 的 `freight` mode 只是容量图中的一种运输模式，不能凭空说明谁在运什么、货物属于谁、何时付款或何时可交付。V12/V13/V14 的住宅、岗位与通勤 source 是 NPC 的人身出行证据，也绝不能被重解释成企业订单。

F10.0 因此只建立下列不可替代的底座：

1. 一个订单由买方企业、卖方企业、真实 source/destination supply-chain node 和一组资源行组成；
2. `accepted` 时冻结卖方可用库存、一次性结算买卖双方的货款，并留下可逆转的 journal；
3. `dispatched` 是独立事实，后续 F9.2.C 只能从它生成 freight source；
4. `delivered` 才通过现有 `city_resource_operations` 转移实物库存；
5. 取消、过期或运输失败都只能追加事实、释放 reservation 并对 acceptance journal 做一次 reversal；
6. 所有订单状态由 append-only transition 派生，不能在 UI 或后台直接更新 `current_state`。

F10.0 不做：

- 不把订单自动映射为 V9 route；F9.2.C 才建立 dispatch → freight demand 的 producer；
- 不把库存复制成第二套“仓库余额”；真实所有权仍以 `city_inventory_balances` 和资源 operation 为准；
- 不从 NPC work facility、Actor 位置、通勤 binding 或当前 UI 选择猜测货运端点；
- 不实现信用、应收账款、税、运费、拆单、部分交付、损耗或自动报价；这些都要求后续独立事实协议；
- 不将 F10 货币与平台余额、自定义虚拟货币或真实金融产品相连。

## 2. 权威模型

```text
supply-chain node + inventory balance
        │
        ▼
order header ── order lines ── proposed fact
        │                         │
        │                  accepted fact + reservation + purchase journal
        │                         │
        │                  dispatched fact + dispatch record
        │                         │
        └──── delivered/failed/cancelled/expired fact
                                  │
                    resource transfer / journal reversal
```

### 2.1 订单、行与端点

订单 header 固定：`code`、buyer/seller firm、source/destination active supply-chain node、created tick/fact 和 metadata。每条 line 固定：resource、数量、单价、source balance 与 destination balance。

- node 是 V15 的不可变物理-经济锚点：`firm entity + active city_open_world_facility + F3 district`。它不复用 F7 `city_enterprise_sites`，因为 V14 open-world 不依赖 F7 企业选址阶段，二者不能被错误地当作同一投影；
- source node 必须属于 seller，destination node 必须属于 buyer；两端均为 active supply-chain node；
- buyer 与 seller 不得相同；source/destination node 不得相同；
- 资源必须是 active、storable；每个订单中 resource 唯一；
- source/destination balance 仅是既有 `city_inventory_balances` 的稳定引用，不能由订单保存可修改的 quantity；
- destination balance 可在订单建立时以零余额创建，但任何数量变化仍必须由 resource operation 写入。

F10.0 的 node 与其基线参与企业是冻结的世界基础：新世界和 V14→V15 upgrade 会为供给、需求两端建立固定 node，并只绑定已经存在的 F3 firm / V5 facility / district。F10.0 不伪造“任意创建企业”的运行时命令，也不让 supply-chain 层私自维护第二份企业生命周期；F10.1 的动态企业/节点 onboarding 必须先扩展通用经济实体、账户实例化和 recovery 合同，再作为独立版本升级进入。

### 2.2 状态机

```text
proposed → accepted → dispatched → delivered
    │          │            └────→ failed
    │          ├─────────────────→ cancelled / expired
    └────────────────────────────→ cancelled / expired
```

终态为 `delivered`、`cancelled`、`expired`、`failed`。状态由 `(transition_tick, transition_sequence)` 最大行派生；每个 transition 必须指向同 tick、同 sequence 的 supply-chain fact。

- `proposed`：买方提交有效订单，不保留库存、不转移货款；
- `accepted`：卖方验证所有行的未保留库存，并一次性追加 reservation 与 purchase journal；
- `dispatched`：卖方确认货物已进入将来 freight 处理队列；F10.0 不移动库存；
- `delivered`：买方或后续 freight arrival producer 追加收货事实，并以一个 atomic resource transfer 转移全部行；
- `cancelled`：订单尚未 dispatch 时双方可取消；若已 accepted，必须反转 purchase journal；
- `expired`：系统只对未接受、或 accepted 但未 dispatch 的超时订单追加过期；accepted order 的自动过期 reversal 是受限的 machine-origin journal，metadata 必须精确声明 `open_world_supply_chain.auto_expiry.v1`，且只能由对应 supply-chain fact 触发；
- `failed`：dispatch 后的明确失败处理；F10.0 仍未离开卖方账面库存，因此只释放 reservation 并 reversal 货款。

F10.0 禁止部分交付、部分退款和对终态订单复活。它们会在后续以新的 dispatch/settlement 语义扩展，不能偷偷修改本版行数量。

## 3. 库存 reservation 与资金结算

### 3.1 库存

reservation 是订单 line 对 source balance 的不可变占用记录；`release` 是第二条不可变事实。有效 reservation 定义为“存在 reservation，且不存在 release”。

```text
sum(active reservation quantity for source balance) <= balance.quantity_units
available = quantity_units - active reservations
```

此不变量由 deferred PostgreSQL assertion 同时附着在 reservation/release 和 inventory balance 更新上。因此别的资源生产、消费、市场或恢复事务不能在提交后侵占已经承诺给 accepted order 的库存。

### 3.2 金额与 journal

F10.0 采用 **acceptance settlement**：卖方接受订单的同一 tick 立即结算货款，资金没有独立 escrow 投影。

```text
buyer firm:  Dr inventory      / Cr cash
seller firm: Dr cash           / Cr revenue
```

这样 accepted order 不会在 dispatch/delivery 时因买方已花掉资金而进入无资金死锁。若订单未交付而终止，系统只允许一次、完整反转该 purchase journal；不可创建第二笔任意退款。实物库存仍在 `delivered` 前属于卖方的 `city_inventory_balances`，并受 reservation 保护。

本版不声称实现完整成本核算或在途库存；卖方的 COGS、运费、税和账期属于 F10.1+。

## 4. 命令、权限与时序

命令：

```text
open_world.supply_chain.order.create
open_world.supply_chain.order.accept
open_world.supply_chain.order.dispatch
open_world.supply_chain.order.deliver
open_world.supply_chain.order.cancel
open_world.supply_chain.order.fail
```

- create/receive 需要 buyer firm 的 owner；accept/dispatch 需要 seller firm 的 owner；cancel/fail 需要任一合同方；world owner 与 system administrator 可作为治理例外提交；
- enqueue 时完成主体授权，apply 时再以订单/站点/库存/账本当前事实校验前置条件；
- command 在 T 被应用时，dispatch record 的 `dispatched_tick=T`；F9.2.C 的 freight source 最早 T+1 才能进入 V9 调度；
- `deliver` 要求 `target_tick > dispatched_tick`，保留至少一个离散运输边界；
- 自动过期运行在 command application 之前：过期事实不会抢占同 tick 仍有效的 dispatch command，判定以 target tick 与 deadline 的严格比较为准。

## 5. 数据、事实与恢复

V15 新增 profile、order、line、transition、reservation、reservation release、dispatch 和 settlement 表。所有可变投影写入受 session-scoped write gate 保护；删除只允许 verified recovery 清空投影。

每次业务变化在 `city_open_world_supply_chain_facts` 记录不可变事实，随后写对应 transition/reservation/dispatch/settlement。事实包括 stable order code、state、reason、resource-operation/journal cursor；没有事实的表行一律无效。

canonical state 包含 profile、orders、lines、transitions、reservations/releases、dispatches、settlements 和 facts。replay/recovery 必须验证：

- node 与 firm/facility/district 的稳定 identity，以及 line 与 source/destination balance/entity/resource identity；
- 每个状态转换、reservation/release、dispatch、journal/resource operation 的 fact tick/sequence 对齐；
- reservation 与库存的可用量约束；
- accepted purchase journal 和终止 reversal 的精确一对一关系；
- delivered resource operation 的资源、数量、from/to balance 与 order line 的精确对应；
- V14 commute lifecycle 和旧 F0–F9 历史不被重写。

## 6. API 与可观测性

只读接口：

```text
GET /api/v1/city/worlds/:world_id/open-world/supply-chain
```

返回过滤后的 profile、订单、行、当前派生状态、active reservation、dispatch、settlement 与可读 facts。普通成员只读取自己拥有的 buyer/seller firm 参与的订单与其合同端点；profile 的动态计数按该可见集合重算，`revision` 归零，不能作为其他企业活动的旁路信号。world owner/system administrator 可读取全量。响应不得暴露内部数据库 ID、其他用户的 command payload 或不相关企业的可用库存。

面板至少展示：状态时间线、端点、行数量/金额、可用库存不足原因、journal/resource-operation cursor、dispatch age、deadline、自动过期/失败原因和未来 freight readiness。它不能在前端自行计算最终状态或金额。

## 7. 验收矩阵

1. V15 genesis 与 V14→V15 paused upgrade 只建立 profile，不伪造历史订单；
2. 订单必须绑定真实企业与 enterprise site；跨企业/失活站点/重复资源行/自交易均被拒绝；
3. accepted 原子写入 reservation、purchase journal、transition/fact；并发 accept 不能超卖；
4. resource operation 或 recovery 不能把 active reservation 覆盖成负可用库存；
5. dispatch、delivery、cancel、expiry、failure 都只能按状态机追加，terminal 不可复活；
6. delivered 只在 dispatch 后的下一 tick 或更晚发生，且一次性转移所有订单行；
7. 取消/过期/失败对 accepted payment 只产生一次精确 reversal；
8. canonical、replay、recovery、V14→V15 upgrade、API privacy、命令权限、静态 migration 和 PostgreSQL 集成测试均覆盖。

## 8. 后续依赖

F9.2.C 只读取 V15 `dispatched` record，并将 source code、order code、line set、source/destination supply-chain node、deadline 和 fact cursor 复制进 freight demand metadata。它不得回写 F10 order，也不得把 V9 route completion直接当 delivered；未来 arrival/receipt adapter 必须显式产生 `order.deliver` 语义。

F10.1+ 才可增加报价、账期、税、运费、损耗、拆单/部分交付、在途库存、多企业计划、配方投入产出、破产和可验证财务报表。
