# R4.2 角色成长、原型与职业转换设计

版本：v1.0（2026-07-22）
状态：已实现首个可玩切片；后续内容扩展必须保持本文件的封存、权限与版本边界。

适用范围：`city-openworld-realtime-v2` 共享在线世界中的**用户角色**成长、活动经验、职业角色（Role）转换、拥有者投影和回放验证。本设计不改变旧 V24/F7 世界，不把城市内信用转成平台余额，也不授权浏览器、用户提示词或 Agent 直接写属性、职业、货币或法律结果。

依赖：

- [《城市模拟游戏总体设计》](CITY_SIMULATION_GAME_DESIGN_CN.md)
- [《城市模拟实时世界时钟与像素世界改动设计》](CITY_SIMULATION_REALTIME_PIXEL_WORLD_DESIGN_CN.md)
- [《城市模拟 Agent 化架构改动设计》](CITY_SIMULATION_AGENT_ARCHITECTURE_DESIGN_CN.md)
- realtime temporal frame、角色生活目录、空间/Portal、Rule/Case、幂等收据与 canonical hash 基础设施

---

## 1. 决策摘要

### 1.1 已冻结的边界

| 主题 | 冻结结论 |
| --- | --- |
| 世界范围 | 每个角色只属于一个共享 `world_id`；成长状态是该世界内的私有角色状态，不复制世界、地图、NPC 或经济 |
| 起点 | 新角色只能从版本化 archetype 目录中选择一个起始原型；客户端只提交 `archetype_code`，不得提交初始属性、经验、职业或奖励 |
| 属性 | 属性值由 archetype 基线和封存经验确定性推导；浏览器永远没有“设置属性值”接口 |
| 职业 | `Role` 按 category 持有，首版 `profession` 类别最多一个当前角色；未来 license、faction、office 等类别可并存，不要把“职业”硬编码为唯一身份系统 |
| 成长来源 | 只有服务端目录定义的活动可产生经验；经验奖励、前置角色、位置、needs、冷却、规则与处罚均在 world lock 内重算 |
| 可见性 | archetype、属性、经验、可转任角色、角色历史、角色转换结果均仅返回 owner projection；公共 Actor/地图/时间线不包含它们 |
| 货币 | `city_credit_units` 是城市内局部状态；职业活动可以影响它，但绝不直接兑换 Sub2API 平台余额、虚拟货币或充值余额 |
| 旧世界 | catalog 1.0–1.2 保持原行为；只有绑定 `city-realtime-character-core@1.3.0` 的新 world 使用 progression schema 3 |
| Agent | 人类玩家 UI 只是当前可玩验证面。后续 Character Agent/NPC Agent 只能提交同一受控 activity/role command，不能绕开 reducer 或伪造经验 |

### 1.2 非目标

- 不在 R4.2 建立工资、岗位空缺、组织雇佣合同、真实银行、股票、平台奖励 outbox 或自动现金兑换；
- 不把角色原型解释为现实身份、职业资格证、用户账号权限、管理员身份或敏感画像；
- 不让用户编辑他人角色、NPC 属性、任意 Role、目录定义、SQL 行或状态 hash；
- 不在浏览器以动画帧、设备时间、离线时长或模型回复 token 数计经验；
- 不升级、补写或静默修复历史 world 的角色目录。需要迁移时必须新增版本、迁移计划和 replay proof。

---

## 2. 当前可玩闭环

玩家在受支持 realtime world 中可按如下路径完成第一个无管理员介入的成长循环：

```text
加入共享 world
    ↓
选择一个起始 archetype + 创建公开名称
    ↓
在道路/人行道、室内与楼层间移动；Portal 由服务端验证
    ↓
短休 / 进食 / 市政轮班 / 清洁 / 公共服务研习
    ↓
服务端结算 needs、城市信用、口粮、法律结果、经验事件
    ↓
owner projection 显示属性、经验、职业条件与可用职业
    ↓
申请同类别的新 Role（原 Role 被受控替换）
    ↓
解锁需要该 Role 的市政服务或维护轮班
```

玩家在此流程中只能选择**可接受的意图**，并不能指定结果。例：按钮显示“市政轮班”不表示必定成功；服务端仍会检查时间线、当前位置、能量、饱腹、冷却、角色状态、Role requirement 与 Rule/Case 状态。

### 2.1 初始 archetype

`city-realtime-character-core@1.3.0` 目前提供三种内容目录原型。它们是平衡起点，不是永久数值类别或现实人格标签。

| archetype code | UI 名称 | 初始 profession | 侧重点 |
| --- | --- | --- | --- |
| `resident.generalist` | 城市通才 | `profession.resident` | 协调、推理、体能相对均衡 |
| `resident.social` | 社群居民 | `profession.resident` | 沟通较高，适合较早满足 civic aide 的属性门槛 |
| `resident.technical` | 技术居民 | `profession.resident` | 协调、推理较高，适合维护路径 |

所有原型都生成同一组五项属性，并且只授予初始 `profession.resident`。初始 Role 不是收入、权限、城市所有权或平台奖励资格。

### 2.2 当前活动与经验来源

| activity code | 类型 | 经验奖励 | 额外门槛 / 结果 |
| --- | --- | --- | --- |
| `rest.short` | recovery | vitality +4 | 维持可玩的 needs 循环 |
| `consume.ration` | recovery | vitality +2 | 消耗服务端库存内口粮 |
| `work.civic_shift` | work | communication +12、discipline +24 | 在道路/人行道，结算基础市政工作 |
| `civic.cleanup` | civic | coordination +26、vitality +12 | 在道路/人行道，结算公共清洁 |
| `study.public_service` | training | communication +12、reasoning +24 | 可通行位置，消耗 needs 与少量城市信用 |
| `work.civic_service` | work | communication +28、discipline +32 | 必须持有 `profession.civic_aide` |
| `work.maintenance_shift` | work | coordination +32、vitality +26 | 必须持有 `profession.maintenance_worker` |
| `conduct.disruption` | law test content | 无 progression reward | 用于 Rule/Case/处罚路径验证，不能被当作奖励来源 |

活动奖励是内容目录数据，绝不是请求字段。前端只显示 mutation 返回的 `experience_deltas`，并在随后读取 owner projection 以服务端快照为准。

### 2.3 当前职业目录

| role code | category | 入口要求 | 解锁方向 |
| --- | --- | --- | --- |
| `profession.resident` | profession | 无 | 起始角色 |
| `profession.civic_aide` | profession | civic standing、总经验、沟通/自律 | `work.civic_service` |
| `profession.maintenance_worker` | profession | civic standing、总经验、协调/体能 | `work.maintenance_shift` |
| `profession.community_steward` | profession | 更高 civic standing、总经验、沟通/自律/推理 | 后续公共服务与组织内容的安全入口 |

具体阈值属于 catalog 版本，不应由前端复制。owner projection 返回每项 Role 的 requirements 和首个不可满足原因，UI 仅用作说明；角色转换 mutation 在锁内重新判定，防止过时页面、并发请求或篡改 payload 绕过条件。

---

## 3. 内容、版本与状态模型

### 3.1 catalog 不可变性

新世界在 genesis 时绑定角色生活 catalog 与 progression definition 的 canonical hash。目录至少包含：

```text
schema_version
attribute definitions (experience_per_value_milli, maximum_experience_units)
archetypes (initial role, all initial attributes)
roles (code, category, requirements)
activities (base effect, role requirement, experience rewards)
```

目录检查必须拒绝：重复 attribute/archetype/role code、未知 archetype 初始 Role、缺失或重复初始属性、未知属性条件、未知前置 Role、重复经验奖励、负值/超限值以及不能 canonicalize 的定义。运行时读取的 Go catalog 与 migration 中封存的 catalog 必须逐字段相等；它们不同即视为 simulation invariant failure，而不是选择其中一方静默继续。

### 3.2 profile schema 3

角色 profile 从 schema 3 起持有：

```text
actor_code
archetype_code
progression_revision
progression_event_chain_hash
progression_state_hash
attributes[]
roles[]
```

属性持久化为 `city_realtime_character_attribute_states`；Role assignment 持久化为 `city_realtime_character_role_assignments`；成长事件持久化为 `city_realtime_character_progression_events`。每一层有以下约束：

| 数据 | 关键不变量 |
| --- | --- |
| attribute state | 每个 actor/attribute 唯一；经验只增不减且有上限；revision、frame、state hash 一致 |
| role assignment | 每个 actor/category 唯一；role code 与 catalog category 一致；替换同类别 Role 是一次原子写入 |
| progression event | sequence 严格递增；`previous_event_hash → event_hash` 链连续；activity 或 role 事件都有对应封存 frame |
| profile | 所有状态行按稳定 code 排序；profile state hash 覆盖 archetype、属性和 Role state hash；event chain hash 连接最后一条 event |
| temporal frame | 创建、活动经验、Role transition 都记录在同一个服务端 frame 中；不能在 frame 之外修改 progression 表 |

数据库触发器/guard 拒绝直接 `INSERT/UPDATE/DELETE` 属性和 Role 表。应用层仍需在每个 reducer 输入/输出处验证 hash；SQL guard 不是唯一安全边界。

### 3.3 属性公式

对属性 `a`：

```text
value_milli(a) = min(1000,
  archetype_initial_value_milli(a)
  + floor(experience_units(a) / experience_per_value_milli(a))
)
```

首版所有属性每 5 点经验提高 1 个 `milli`。公式、上限和初始值均由 catalog 版本冻结。客户端只接收投影后的 `value_milli` 与 `experience_units`，不会获得可写的成长系数接口。

### 3.4 Role category 模型

同一角色可同时持有不同 category 的 assignment，例如未来可组合：

```text
profession.maintenance_worker
license.electrical_basic
faction.neighborhood_coop_member
office.block_delegate
```

首版只提供 `profession`，因此转任同类别职业会替换原 profession，但不会删除未来其他类别的 license/faction/office。新 category 需要目录版本、前置条件、撤销/过期语义、公开性 policy、迁移与 replay 测试；不得在 `profession` 字符串上堆条件分支。

---

## 4. 命令、投影与权限契约

### 4.1 创建角色

```http
POST /api/v1/city/worlds/:world_id/realtime/character
Idempotency-Key: realtime-character-create-…

{
  "public_label": "…",
  "archetype_code": "resident.social"
}
```

服务端以调用者身份确定 owner，验证 membership、world engine、runtime readiness、唯一角色限制、label 规则和 archetype catalog。缺省 `archetype_code` 只在 progression runtime 已启用时解析为 `resident.generalist`；不支持 progression 的历史 world 仍按照其已封存行为处理，且不能借此写 schema 3 状态。

### 4.2 读取 owner projection

```http
GET /api/v1/city/worlds/:world_id/realtime/character
```

返回调用者自己的 character、life、inventory、`progression`、`available_archetypes`、`available_activities`、`available_portals` 和当前室内投影。以下信息不得出现在公共 map/actor/event API：

- archetype、属性值、经验、成长事件链、Role availability、未公开 activity；
- 账号、邮箱、模型、提示词、Agent memory、控制 grant、inventory 明细、Case 证据；
- 宿主机时钟源、内部 hash 计算输入、SQL primary key 或其他用户 profile。

### 4.3 执行活动

```http
POST /api/v1/city/worlds/:world_id/realtime/character/activities
Idempotency-Key: realtime-character-activity-…

{ "activity_code": "work.civic_shift" }
```

服务端先运行既有 activity/life/law reducer，再运行 progression reducer。成功时 mutation result 可含：

```json
{
  "activity": {
    "code": "work.civic_shift",
    "experience_deltas": [
      { "attribute_code": "communication", "experience_units": 12 },
      { "attribute_code": "discipline", "experience_units": 24 }
    ]
  }
}
```

这个数组只是已结算摘要；权威状态仍由随后 owner projection 与 progression event chain 表示。

### 4.4 转任 Role

```http
POST /api/v1/city/worlds/:world_id/realtime/character/roles
Idempotency-Key: realtime-character-role-…

{ "role_code": "profession.civic_aide" }
```

服务端锁定 world 与当前角色，按 frame 内最新 needs、standing、经验、属性、前置 Role、Role category、catalog binding 和 Rule 状态重算 eligibility。成功 result 返回 old/new role code 和 category；该 API 不接受 `from_role_code`、经验、属性值、站点、工资、成员 ID 或 actor code。

同一 idempotency key 重放同一 canonical result；不同 key 的竞争命令由唯一 category assignment 与 frame/revision 约束收敛，不能让一个角色在同一 category 获得两个有效 Role。

### 4.5 UI 设计规则

角色面板必须：

1. 在创建前展示有限 archetype 选择和本次起始属性预览；
2. 创建后只在 owner inspector 显示“角色发展”、属性条、总经验、当前 Role、所有目录 Role 的就绪状态与条件摘要；
3. 只为 `available=true` 的 Role 显示转任按钮；无法满足时显示服务端 reason，不让 UI 假装可以提交；
4. 活动成功后显示服务端返回的经验摘要，并 quiet refresh owner projection；
5. 请求期间保持局部 busy state，不重置像素世界、canvas、地图 cursor 或其他成员 projection；
6. 不把 progression 数据塞入 map tooltip、公共 actor label、浏览器缓存日志或 analytics payload。

---

## 5. 安全、审计与恢复

### 5.1 威胁与对策

| 威胁 | 处理 |
| --- | --- |
| 浏览器修改经验/属性/Role payload | 请求 DTO 不含可写数值；服务端目录重算；SQL guard 与 profile hash 双重校验 |
| 并发转任或活动/转任竞态 | world lock、Temporal Frame、category uniqueness、idempotency receipt、state/event hash |
| 读取他人成长或 Agent 私有资料 | owner-only query；公共 actor/event projection 白名单化；前端只使用 owner endpoint |
| catalog 发布后改变历史结果 | 新 catalog 新版本/新 hash；历史 world 保持其绑定版本；不就地修改 1.3 |
| 直接 SQL 修正数值 | guard 拒绝；事故修复只能暂停 world、产生有审计的补偿/迁移 frame，再重新验证 chain |
| 模型或提示词诱导升级 Role | Agent 与人类都只能请求固定 code；服务器忽略模型生成的数值、坐标、奖惩、Role 结论 |
| UI 误导性显示 | 所有“可转任”和活动可用性均来自当前 owner projection；mutation 后以服务端 refresh 收敛 |

### 5.2 回放与恢复

replay/recovery 必须恢复 profile、attribute rows、Role assignments、progression events 与 catalog binding，并验证：

1. 每个状态行 canonical hash；
2. profile progression state hash；
3. progression event chain genesis 和每个 `previous_event_hash`；
4. event frame sequence、activity event reference 或 Role transition 的对应性；
5. Role code/category 与历史 catalog definition；
6. Temporal Frame、world timeline cursor 与 public projection 没有泄漏 progression 字段。

任何校验失败都应让 world 进入可诊断恢复状态，而不是默默删去不一致属性或把角色降级为默认 archetype。

---

## 6. 扩展协议

### 6.1 新属性或新 archetype

新增 attribute/archetype 需要新 catalog version，且必须同时提供：

- code 命名、上限、经验公式、所有 archetype 的完整初始 attribute 集；
- catalog/migration equality test、seed test、历史 world compatibility test；
- UI 本地化、可访问 label、未知 code fallback；
- 是否属于私有投影以及对 Rule/Case/职业 requirement 的影响；
- 已存在角色的显式迁移计划。未指定迁移时，旧 world 不获得新属性。

### 6.2 新 Role category、组织与岗位

未来职业系统会接入组织、设施、岗位容量、日程、工资和劳动市场。必须将“满足个人 requirement”与“获得一个实际岗位”拆开：

```text
个人成长资格 → 可申请 Role
组织/设施容量 → 可获得 assignment / contract
工作完成 receipt → 可结算城市内工资、税、库存或服务事实
```

不能因为某属性达到阈值就凭空生成组织工资、建筑门禁或平台奖励。任何工资/奖励应使用城市 journal/受控 outbox，在独立设计审核后接入。

### 6.3 Character Agent 与 NPC Agent

角色 Agent 未来从同一 owner-private observation 中读取经脱敏的角色发展摘要，再提出 activity/role intent。系统 Agent/NPC Manager 只负责 NPC population、LOD、资源分配和可验证任务委派；它们不得读写用户的 progression event chain。所有 Agent provider 失败时可走 deterministic wait/schedule，不能停止世界时钟或自行加经验。

### 6.4 城市信用、平台货币与奖励

`city_credit_units` 可作为城市内服务、培训或 Rule 后果的局部输入，但不得等同于 Sub2API 用户余额或可提现资产。若未来要把游戏行为转成平台自定义货币，必须新增资格事实、风控、奖励 outbox、人工审核/限额、幂等 provider 与可撤销 policy；R4.2 不包含该桥接。

---

## 7. 验收与测试矩阵

| 层 | 覆盖场景 |
| --- | --- |
| catalog unit | 重复 code、未知 attribute/Role 引用、无效初始属性、无效经验奖励均被拒绝 |
| progression reducer unit | archetype seed、活动经验、属性导出、Role availability、成功 category 替换、hash chain 结果稳定 |
| SQL migration | schema 3、attribute/Role/event 表、direct write guard、catalog binding 和 receipt action 约束 |
| PostgreSQL/Redis integration | 创建 social archetype、5 个 attribute + 1 个 initial Role、活动写 progression event、早期转任拒绝、公共事件不泄露私有数据 |
| HTTP route | create 接受 archetype；activity/Role endpoint 使用 owner identity 和 idempotency key；无 actor code 注入 |
| frontend API | owner routes、archetype payload、role transition route/header、公共/私有 timeline 路径分离 |
| frontend typecheck | Vue owner panel、i18n key、API DTO 与模板保持类型一致 |
| regression | 1.0/1.1/1.2 catalog 行为不因 1.3 目录改变；V24/历史 world 不被隐式升级 |

当前实现已完成 catalog、数据库门禁、owner API、Vue owner panel、API test、service/migration/integration test。后续 R4 验收应以真实受支持 world 走通“创建 → 移动/Portal → 休息/工作 → 经验 → 转任 → Role-gated 工作”的完整链路，并在多成员窗口确认公共 map 仅显示允许的 Actor 信息。

---

## 8. 后续待实现，不得提前假装完成

1. **岗位与组织层**：Role qualification 与实际岗位 assignment/容量/日程分离；
2. **任务与服务层**：可观察的目标、阶段、失败原因、公共服务需求与城市设施反馈；
3. **完整 Case UI**：只读的个人 Case/处罚/申诉摘要，继续保持证据和他人信息最小披露；
4. **Agent command bridge**：fake provider、decision request、action budget、approval、replay 和模型 provider policy；
5. **NPC LOD**：NPC Manager 的 cohort/mid/micro bridge 与无传送的近景角色行为；
6. **城市经济耦合**：组织岗位、生产/服务、税费、库存、城市 journal 与经济/交通系统按 frame 因果接入；
7. **受控奖励桥接**：仅在城市本体成熟后另行设计，不将 city credit 直接映射到平台虚拟货币。

在上述任一项完成前，产品文案必须称当前版本为“共享实时城市角色成长试运行”，不能承诺完整职业市场、真实金融系统、自治 Agent 社会或可兑换收益。
