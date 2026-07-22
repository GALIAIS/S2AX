# F9.3.1 / V20：可变基础设施资产与生命周期设计

状态：已实现并验证
前置版本：`city-openworld-v19`
目标版本：`city-openworld-v20`
对应总体阶段：D（交通与空间基础设施）

## 1. 目标与边界

V19 已把 V9 的 hub/edge 冻结为可审计的空间 `node/corridor` 身份，但它故意不拥有实时容量、封路、维护或施工。V20 在不改写任何 V9 需求、路线、到达、货运、库存或账本历史的前提下，为这些静态身份建立可变且可回放的基础设施资产层。

V20 的交付是通用资产状态机，不是某个国家、某种交通工具或某个玩法专用的道路系统：

- 每一个 V19 node 有一个 `network_node` 资产；
- 每一个 V19 corridor 有一个起始 `corridor_segment` 资产，`segment_ordinal=1`；
- 日后可以在不改变资产协议的情况下把一个 corridor 拆成多个 ordered segment，并追加港口、站台、供电、管网、门禁或非交通资产类型；
- `state/capacity_milli` 是 V20 的权威状态，V9 调度器仍只读其原有 edge 容量。V21 或以后的显式版本才可把动态资产状态接入 route feasibility、容量预约、旅行时间、服务可达性或货运调度。

因此本版本**不做**：

- 不在 V20 改动 V9 路由、排队、已接受 demand 或已完成 route；
- 不引入车道、逐车、信号灯、时刻表、票价、施工资源成本或施工工期；
- 不让客户端直接更新资产表、容量、版本、事实引用或版本向量；
- 不把日本/中国等风格写进 reducer。资产 class 由已经封存的 V19 style/content 选择，生命周期 reducer 只处理通用 asset kind/state；
- 不把维护状态偷接入 F10 库存、平台余额、奖励、法律或角色系统。

## 2. 权威模型

```text
V9 mobility hub / edge (immutable topology)
          │
          ▼
V19 spatial node / corridor (immutable spatial identity)
          │
          ▼
V20 infrastructure asset definition (immutable)
          │
          ├── current asset state (authoritative projection)
          └── append-only transition history ──► open-world runtime fact
```

| 数据 | 归属 | 可变性 | 写入方式 |
| --- | --- | --- | --- |
| V19 node/corridor | V19 | 冻结 | genesis、升级基线、恢复 |
| infrastructure profile | V20 | 内容身份冻结；计数/revision 受保护更新 | genesis、升级、命令 reducer、恢复 |
| asset definition | V20 | 冻结 | genesis、升级基线、恢复 |
| asset state | V20 | 当前投影 | 受保护的 V20 命令 reducer、恢复 |
| asset transition | V20 | append-only | 与 runtime fact 同一事务写入 |
| `infrastructure.asset.transitioned` runtime fact | 已有 V4 事实日志 | append-only | 命令 reducer |

数据库 surrogate ID 不进入 canonical state。跨表引用使用稳定的 asset code 或 runtime fact 的 `(tick, sequence)` 身份；恢复时重新解析数据库 ID。

## 3. 资产模型

### 3.1 Profile

`CityOpenWorldInfrastructurePolicy` 封存：

- `profile_id/version/content_hash`：V20 reducer 和状态机协议；
- `baseline_tick`：生成或 V19→V20 升级时的 tick；
- `asset_contract`：`v19_node_corridor_asset_seed_v1`；
- `state_contract`：`append_only_asset_transition_state_v1`；
- `maximum_assets`、`asset_count`、`node_asset_count`、`segment_asset_count`、`transition_count`；
- `revision = 1 + (transition_count - asset_count)`，即 baseline 是 revision 1，后续每次业务状态变更递增一次；
- 固定 metadata，明确 V20 只发布状态，不影响 V9 scheduler。

### 3.2 Definition

`CityOpenWorldInfrastructureAsset` 是 immutable identity：

| 字段 | `network_node` | `corridor_segment` |
| --- | --- | --- |
| `asset_kind` | `network_node` | `corridor_segment` |
| `spatial_node_code` | 必填 | `null` |
| `spatial_corridor_code` | `null` | 必填 |
| `segment_ordinal` | `0` | 正整数；V20 genesis 为 `1` |
| `asset_class` | 从 V19 node class 派生 | 从 V19 corridor class 派生 |
| `definition_version/content_hash` | 固定、可校验 | 固定、可校验 |

代码为稳定 hash：

```text
infrastructure.asset.node.<hash(v20 + spatial_node_code)>
infrastructure.asset.segment.<hash(v20 + spatial_corridor_code + ordinal)>
```

V20 的 `asset_class` 只用于显示/未来内容规则，不包含状态，不允许管理者手工改写。

### 3.3 State 与 transition

`CityOpenWorldInfrastructureAssetState` 为当前状态，`CityOpenWorldInfrastructureAssetTransition` 为完整历史。baseline 为每个 asset 写一条 `operational` transition，`source_fact=nil`；其后的 transition 必须引用同 tick 的 runtime fact。

状态机：

```text
operational ──► restricted ──► operational
     │              │
     ├──────────────┴──► maintenance ──► operational
     │                                  └──► closed ──► construction ──► operational
     └───────────────────────────────────────────────► closed
```

允许边为：

| 当前 | 可达状态 |
| --- | --- |
| `operational` | `restricted`、`maintenance`、`closed` |
| `restricted` | `operational`、`maintenance`、`closed` |
| `maintenance` | `operational`、`closed` |
| `closed` | `construction` |
| `construction` | `operational`、`closed` |

容量值是 `[0,1000]` 的 milli：

- `operational` 固定 `1000`；
- `restricted` 必须显式指定 `1..999`；
- `maintenance`、`construction`、`closed` 固定 `0`；
- V20 不允许高于 baseline 的扩容。这需要独立的建设/升级契约与成本、工期、内容版本，因此留给后续版本。

每一条 transition 还具有 `reason_code`、`transition_tick/sequence`、`source_fact`、`previous_state` metadata。禁止同 state+capacity 的空操作。

## 4. 命令、权限与事实

唯一外部命令：

```json
{
  "command_type": "open_world.infrastructure.asset.transition",
  "payload": {
    "asset_code": "infrastructure.asset.segment.…",
    "state": "restricted",
    "capacity_milli": 650,
    "reason_code": "operator.temporary_restriction"
  }
}
```

规范化规则：

- 严格 JSON object、未知字段/重复字段拒绝；
- code 与 reason 统一小写、受现有安全 code grammar 限制；
- `capacity_milli` 根据目标 state 做 canonical default/范围校验；
- target tick 由 server 的正常命令队列决定，客户端不能直接指定 `effective_tick` 或 source fact；
- 命令在保存点内执行，业务冲突会被标为 rejected 而不会留下半条 transition。

权限：

- 所有 world member 可只读查询；
- 只有 active `owner` 能提交 V20 基础设施生命周期命令；
- 系统管理员应先通过正式的 world 管理流程取得 owner 权限，不能依赖浏览器传入的“管理员”字段绕过 reducer；
- reducer 再次锁定/校验 owner membership，避免“提交后被移除仍然执行”的 TOCTOU。

一次成功命令在同一事务内：

1. 锁定 V20 world、asset definition 和 current state；
2. 验证 state-machine edge、容量和 owner；
3. 写 `infrastructure.asset.transitioned` runtime fact；
4. 更新 current state、追加 transition、递增 policy counter/revision；
5. 更新已有 runtime fact counter，发布 city event；
6. 调用 SQL 和 Go foundation assertion；
7. 与 tick snapshot/hash 一次提交。

## 5. 版本、生成、升级与恢复

### 5.1 Genesis

新 V20 world 按既有 V2→V19 foundation 顺序完成，然后从冻结的 V19 topology 确定性生成 assets/states/transitions。它不创建 V9 demand、route、arrival、货运、receipt、inventory operation 或 journal entry。

### 5.2 V19 → V20

升级必须暂停 world，并复用现有 upgrade 事务/版本向量机制：

1. 校验 V19 node/corridor foundation；
2. 切换版本、使旧 state hash 失效、递增 version-vector generation；
3. 生成 V20 baseline asset definitions/states/transitions；
4. 写 V20 content catalog binding；
5. 写新 canonical snapshot/hash；
6. 保留 V9–V19 的所有历史行不变。

升级本身不触发命令事实；baseline transition 的 `source_fact` 为 null 并有 `origin=baseline/upgrade` metadata。

### 5.3 Snapshot/replay/recovery

V20 canonical state 必须包含完整 policy、immutable definitions、current states 和 append-only transitions。恢复顺序为：runtime facts → V19 spatial network → V20 asset definitions → V20 states/transitions；恢复期间允许专用 recovery context，正常 SQL write guard 仍保持有效。

回放检查分两层：V19 topology、V20 profile 的内容身份与 immutable asset definition 必须完全一致；V20 的 current state/transition 则逐条验证为 runtime fact 的唯一投影，fact payload 必须精确匹配 asset、状态、容量、理由和 `v9_scheduler_effect=none`。这避免把可变状态错误当成 static profile，同时也不允许 snapshot 单独伪造状态变化。

## 6. SQL 防护与不变量

迁移新增四表：

```text
city_open_world_infrastructure_profiles
city_open_world_infrastructure_assets
city_open_world_infrastructure_asset_states
city_open_world_infrastructure_asset_transitions
```

触发器保证：

- definition 仅允许 bootstrap/recovery insert，禁止 update/delete；
- state/transition/profile mutable writes 必须有匹配的 V20 reducer session setting；
- 非 baseline transition 必须引用同 world、同 tick 的 runtime fact；
- transition 的 `from_state` 必须等于写入前 state；
- state row、latest transition 和 policy counters/revision 一致；
- 每个 V19 node 恰好一个 node asset，每个 V19 corridor 在 V20 恰好一个 ordinal-1 segment asset；
- asset definition 的 endpoint、class、content hash 与 V19 identity 逐项一致；
- V20 world 保留完整 V18、V19 predecessor foundations 与 version vector。

## 7. API 与显示

成员只读 API：

```text
GET /api/v1/city/worlds/:world_id/open-world/infrastructure
```

响应包含 profile、asset definition、current state、transition history。它不返回用户控制 grant、账本、隐藏运行时 payload、密钥或操作员身份以外的敏感资料。写入继续走统一的 city command API，以复用 idempotency、expected tick、审计和 command result。

未来 UI 应显示：资产类型/空间位置/状态/有效容量/最后变更 tick 与理由。它不能声称 V20 已改变实时路由容量；在 V21 接入之前必须标识“规划状态，尚未影响 V9 调度”。

## 8. 验收矩阵

- 新 V20 world：node/corridor asset 对应关系、代码/hash、baseline states/transitions、version vector 正确；
- V19→V20：暂停升级成功，V9/V15–V19 历史 hash 与行数不变；
- 命令：owner 能 restricted → maintenance → operational；viewer/non-owner 被拒；非法边、空操作、非法容量被拒；
- DB：直接 insert/update/delete profile/asset/state/transition 被拒；
- API：member 可读，non-member 无权；
- snapshot/replay/recovery：转态后的完整 canonical state 完全恢复；
- 回归：V19 scheduler capacity/route 行为在 V20 无基础设施命令和有基础设施命令时均不被 V20 改写；
- 审计：每个非 baseline state 都能回溯到 runtime fact 与 city command。

已验证的实现覆盖新建 V20 world、V19→V20 paused upgrade、owner/viewer/outside 权限、`restricted → maintenance → operational` 生命周期、SQL 直写拒绝、runtime-fact payload 证明、snapshot replay 和 verified recovery。

## 9. 后续边界

V21 可在明确版本升级后让 V9 scheduler 消费 effective capacity，必须定义已接受 route 的过渡语义、reservations 的重新校验和同 tick 因果顺序。再后的版本可追加多 segment 切分、资产施工订单/成本/工期、检查/退化、设施站台和跨网络依赖；这些功能均不得回写 V20 baseline 或 V9 历史。
