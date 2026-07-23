# Task Plan: 城市模拟共享在线世界实施

## Goal

在不破坏现有 V24 城市系统的前提下，逐步实现共享在线 realtime world 的可玩闭环。

## Phases

- [x] Phase 1: 审计现有 world、成员、调度、空间和前端契约
- [x] Phase 2 / S0: 共享 world 准入硬化与成员资料最小披露
- [x] Phase 3 / R1: 独立 realtime 时间内核与持久化 Frame
- [x] Phase 4 / R2: 共享投影、cursor 与实时补丁
- [x] Phase 5 / R3: 像素场景渲染器和多人同屏
- [ ] Phase 6 / R4: 真人角色共享世界游玩闭环（R4.2 角色成长、原型与职业转换已完成）
- [ ] Phase 7 / R5: Character Agent 与 NPC Agent（A0/A1/A2、A3.1/A3.2、A3.3b 的 Case/social/process、owner-safe relation/process projection、结构化任务、A3.3c 有限导航计划、A3.3d 单格交通容量预约，以及 A4.0–A4.2 的 profile/budget/worker/breaker、审计化 dead-letter quarantine/release、陈旧隔离告警与管理员受控运行时控制台、OpenAI API-key/Agent Identity 专用外部模型 transport 已完成；其他平台 system transport、复杂关系、NPC 生命周期、owner profile selection 和奖励仍待实现）

## Key Questions

1. realtime 内核如何复用现有单 world 写入纪律而不改变 V24 小时 tick？
2. 如何为共享世界提供公共可见性投影且不泄露成员身份和控制权限？
3. 哪一个最小垂直切片能在像素与 Agent 前证明两个会话实际收敛？

## Decisions Made

- 先验证共享 world 的后端权威与事件收敛，再做像素渲染、Agent、图像素材或更复杂经济内容。
- 旧 V24 保持不变；realtime 以独立 engine/version 路径实现。
- 第一项实现任务定为 S0：移除每个 owner 只能拥有一个活跃私人 world 的遗留约束，并以现有 city_members 作为共享 world 的唯一成员关系。
- S0 验收后才启动 R1 realtime 时间内核；不在 V24 的小时 tick 上硬改实时语义。
- S0 已实施：迁移 251 删除旧 owner 唯一索引；普通成员 roster 不再返回 email；前端为被脱敏成员提供稳定的 `#user_id` 显示回退。
- R1 已建立独立 realtime engine、共享 clock state、不可变 Temporal Frame 与只读 clock/timeline API；旧 V24 tick scheduler 显式排除 realtime world。
- R1 的诊断时钟仅允许 server-marked administrator + `frozen_test_clock` profile。它现在可原子创建、去重、排序并结算 due event，且 due queue 的业务字段、payload hash、创建/结算 frame 都纳入 canonical state；空时间区间不写 Frame。
- R1 的 production world 现在必须由 server-marked administrator 显式选择不可变 production clock profile 创建；普通创建路径不能选择 engine/profile。scheduler 默认关闭，只有通过 server-owned Clock Authority 的观察才会推进。
- R1 已完成受控生产生命周期与运行接线：默认关闭的 host-clock authority、生产 profile 适配检查、scheduler 运行期双开关、管理员只读 clock-health、无浏览器时间输入的 pause/resume 以及 immutable lifecycle Frame。暂停会先结算边界内 due event，恢复会新建 clock segment 而不计入暂停时长。
- 当前 host authority 只接受明确声明为 `system_ntp`/`system_nts` 的已校时宿主机，不接受任意私有时间服务 URL；不同 authority 所属的 profile 会被 scheduler 安全跳过而非误标为 unsafe。
- R2 已引入独立 `city-openworld-realtime-v2`：它共享 R1 的可信时钟和 Temporal Frame，但使用隔离的 `city_realtime_spatial_*` 不可变空间表，而不触碰旧 `city_open_world_*` tick/materialization 族。
- R2 的静态空间绑定将生成器、规则集、国家/城市 profile、seed、spawn、region/sector/chunk/building/interior/portal hash 纳入 V2 canonical state；视觉资源仍不进入物理地图或 state hash。
- R2 成员读取通过 projection、semantic chunk 和 cursor patch 三个 API 交付；projection 只包含调用者自己的 membership scope，Timeline Frame 沿用既有安全摘要，不返回 roster、邮箱、控制授权、due payload、worker lease 或时钟源机密。
- R3 已完成 CSP-safe Pixi/Canvas 像素渲染、版本化视觉包绑定、共享匿名 Actor 投影与服务器侧确定性巡逻；同一 chunk window 内的成员读取相同 cursor/Actor 投影，静态地图与视觉 binding 不会因为 Actor 移动而改写。
- Agent A0/A1 已完成：`city-realtime-agent-core@1.0.0` policy pin、world binding、root/NPC manager/NPC character tree、实例/生命周期 hash 与数据库 sealed-frame 约束已进入 V2 genesis 和 canonical state；它不调用模型，也不向成员暴露 Agent 私有面。
- R4 已补齐版本化角色生活目录 `city-realtime-character-core@1.2.0`：新 world 的 profile 使用 schema 2，服务器以 `system.realtime.character_metabolism` due event 每 60 秒推进精力、饱腹与士气。事件的 expected version 绑定独立 `metabolism_revision`，因此用户正常移动/活动不会让被动更新过期；活动、库存、法律链与 city credit 不会被该 reducer 修改。目录和 profile hash 保持 append-only，1.0/1.1 world 不会被自动升级。
- R4.2 已发布 `city-realtime-character-core@1.3.0`：新 world 选择受限 archetype，schema 3 封存五项属性、category-scoped Role assignment、活动经验和 progression event hash chain；owner projection 显示成长与可转任职业，公共 Actor/map/event 投影不泄漏 progression。Role/经验/属性/活动限制在 world lock 与 SQL guard 中重算，历史目录不被修改。详细契约见[《R4.2 角色成长、原型与职业转换设计》](docs/CITY_SIMULATION_R4_CHARACTER_PROGRESSION_DESIGN_CN.md)。
- Agent A3.1 已完成：`city-realtime-agent-core@1.2.0` 为新 world 增加 owner-private 人格 immutable revision、`autonomous/suspended` 控制、world-time wakeup 和受限活动 adapter；暂停会撤销未执行工作，已租约结果通过 precondition 变为 stale。
- Agent A3.2 已完成：迁移 267 发布 `city-realtime-agent-core@1.3.0`，只为新 world 增加有限的 `character.move`、`character.portal.traverse`、`character.role.change` adapter。Observation 发布经过服务端验证、排序、去重的有限 `action_context`，其 hash 进入 precondition；finalizer 验证候选归属，due-event 再重检相邻/地形/室内/占用、Portal 拓扑和 Role progression。旧 1.0/1.1/1.2 policy、授权散列和 canonical 形状不变。
- Agent A3.3a 已完成第一、第二个独立 action slice：迁移 268 发布 `city-realtime-agent-core@1.4.0` 的既有 Law Case acknowledgement，迁移 269 发布 `@1.5.0` 的 bounded social greeting；新 realtime world 默认绑定 1.5，旧 1.0–1.4 policy 仍按原版本执行。Case 只能确认本人已封存的 open Case，不改变裁决/罚款/城市信用；social 只能选择服务端公布的相邻 active NPC/Character，写入匿名 pair 的粗粒度 affinity/interaction append-only chain，不保存聊天、不发奖励、不改其他领域。Action context v3 兼容保留 v1/v2 wire shape；新增 owner-scoped relation cursor API，已补充纯 reducer/hash、迁移、路由与真实 PostgreSQL Case/social integration 验证。
- Agent A3.3b 已完成第三、第四个受限 action slice、Case-process 与程序分派 foundation：迁移 273 发布 `city-realtime-agent-core@1.6.0` 与 action-context v4 的程序性 `character.case.review.file`；迁移 274 发布 `@1.7.0` 的 `character.case.report.file`；迁移 275 发布不新增模型 action 的 `@1.8.0`；迁移 276 发布仍不新增模型 action 的 `@1.9.0`；迁移 277 发布同样不新增模型 action 的 `@1.10.0`；迁移 278 发布仍不新增模型 action 的 `@1.11.0`。review 只能处理本人已确认、15 分钟内、尚无 review head 的 Case，并固定在 30 秒后 `closed_no_change`；report 只能使用同一 Observation 中服务端公布的相邻 active NPC/Character target，写入一条 ordered reporter/subject pair 的 `filed_unverified` receipt；1.8 仅把该 receipt 的 event hash 绑定到独立 `evidence_required` 工作项，并固定在 30 秒后 `expired_no_evidence`；1.9 仅允许既有活动 reducer 已封存的 Law event 创建独立的短时 `server.sealed_law_event` source handle，30 秒后固定 `expired`；1.10 仅在同 subject 下恰好存在一个先于 receipt、仍 active 且未占用的 source handle 时创建 `linked_active` assignment，source 失效时只能关闭为 `source_window_closed`；1.11 仅在同一 frame 已存在 assignment link 时追加 `queued` dispatch，并在 source 失效时关闭为 `source_window_closed`。report/intake/handle/assignment/dispatch 都不含自由文本、理由、证据声明或裁决；它们不验证 evidence，不能创建或改变 Law Case、处罚、city credit、库存、任何账本或平台/虚拟货币变化。所有切片均由 binding/state/event hash chain、sealed intent、due-event reducer 与 PostgreSQL guard 约束；Case-review、Case-report、Case-intake、sealed-Law evidence-source、source correlation 与程序分派的真实 PostgreSQL 端到端链路均已覆盖。
- Agent A3.3b 已完成首个结构化任务切片：迁移 279 发布 `city-realtime-agent-core@1.12.0` 与 action-context v5 的 `character.task.accept`。当前 catalog 仅有 `task.civic.cleanup → civic.cleanup` 与 `task.civic.shift → work.civic_shift` 两个固定定义；Agent 只能从其 sealed Observation 的 `available_task_codes` 回显一个 code。接受任务会在同一权威链路封存 pending intent、owner-private task head/event 和唯一 5 分钟 expiry due event；只有相同 autonomous Character Agent 在 deadline 前完成任务指定的既有 activity，才能追加 `completed`。任务不含自由文本、奖励、钱包、Case、Rule 或资产变化；错配/手工活动、deadline 边界、浏览器 API 和 direct SQL 都不能完成或改写任务。owner-scoped task projection 仅返回调用者自己的任务运行摘要；真实 PostgreSQL 已覆盖接受、权限隔离、SQL guard 和匹配活动完成链路。
- Agent A3.3c 已完成有限导航计划：迁移 280 发布 `city-realtime-agent-core@1.13.0` 与 action-context v6 的 `character.navigation.plan`。Agent 只能从当前 sealed Observation 中服务端发布的静态 `entrance` Portal code 候选选择目的地；服务端以固定邻居顺序、最大 8 个候选、最多 32 步和最多 4096 个展开节点生成有限地表路线。计划不会缓存或暴露完整路径：每个 movement quantum 只重新验证并消费一格，静态地形、占用、控制模式、计划 revision 或候选变化都会令计划成为 `blocked`/`cancelled`，而不是改写目标或跳跃移动。binding/head/event/due-event 形成独立 append-only hash chain；`planned → step* → arrived|blocked|cancelled` 的事件只可由已封存 intent 和服务端 reducer 写入。owner 只能读取自己的最小运行摘要，不能创建、改写或取消计划；控制撤销会同时撤销未执行的导航计划。真实 PostgreSQL 已覆盖候选约束、Agent intent、逐格移动、封存、权限隔离和 direct SQL guard。
- Agent A3.3d 已完成共享单格交通容量预约：迁移 281 以不可变 `city-realtime-pedestrian-capacity@1.0.0` 和新 world genesis binding 为 `1.13.0` 有限导航增加 server-only capacity due event。每个下一格先在 `movement` priority 70 以稳定 dedup 顺序请求容量，再由 priority 90 的导航 reducer 在同一 frame 精确消费；`granted → consumed` 同帧转换必须链接同一 Actor 的 position-event hash，容量不足只封存 owner-private `denied_capacity` 并让既有导航安全结束为 `blocked_occupied`。预约不向 Agent、浏览器或 owner 暴露坐标、路径、其他 Actor、slot、容量参数、资产或写入面；旧 1.13 world 没有 binding 时保留原有限导航语义。真实 PostgreSQL 已覆盖 genesis binding、pending boundary、同帧 grant/consume、owner projection、outsider 隔离及 direct SQL guard。
- Agent A4.0–A4.2 已完成受控模型运行时基座：迁移 282–287 建立 immutable model profile/head/world binding、request/attempt 精确 snapshot、保守 hourly budget ledger、provider failure breaker、retry deadline、worker-only queued deferral、管理员 retry wake audit receipt，以及审核化 dead-letter quarantine/release。默认 deterministic provider 通过严格 `agent-decision-v1` parser 运行；wire bootstrap 还会注册唯一 `sub2api.gateway` 系统身份 adapter。该 adapter 用 profile 指定的管理员 group 走既有 scheduler+账号并发 slot，但不复用 Gin handler、loopback HTTP、浏览器上下文或用户 API Key。它目前支持 OpenAI-compatible API-key 的 non-streaming Chat Completions，以及完整 provision 的 OpenAI Agent Identity 经固定 Codex Responses SSE + fresh `AgentAssertion` 的专用路径；普通 OAuth bearer account 会被 scheduler 排除，过期 Agent task fail-closed 且绝不自动注册。原始 response/error/endpoint/credential/account 不写入 city runtime，普通 usage/billing/余额/城市或虚拟货币账本也不会被触发。独立 Agent Decision Worker 受 `city_simulation_enabled` 与 `city_realtime_agent_decision_worker_enabled` 双开关控制，逐条消费 V2 running world 的 due request；未注册 adapter、breaker 冷却或临时预算不足只写 future retry deadline，绝不伪造 attempt 或 temporal frame，其中 budget exhaustion 直接等待下一 UTC 小时窗口。管理员可读取单 world 的安全队列 projection，并仅能 wake 仍延迟、未租约的 request。管理员也可用闭集 reason quarantine 一个 queued/unleased request：worker candidate 与直接 lease 都会 fail-closed，release 不会执行、不清 retry，状态/事件均由 operator gate 与 append-only receipt 约束；单 request 的只读事件检索只返回最小审计字段。`/admin/city/agent-runtime` 已完成单 world 控制台：只显示安全 queue projection、受状态机约束的隔离/解除/唤醒操作、keyset event receipt 与固定 24 小时陈旧隔离告警，既不显示 payload/credential，也不支持同步执行 provider。下一项为其他平台逐个扩展 system transport；随后进入 A4.3 owner-safe profile selection 与 A5 NPC LOD/reward outbox。

## Errors Encountered

- `cmd/server/wire_gen_test.go` 在新增 scheduler cleanup 依赖后缺少参数，已同步更新并通过生成接线测试。

## Status

**Phase 7 / R5 已推进至 A4.2 可控模型运行时闭环** - 用户角色创建、共享可见位置、地表移动、建筑/楼层 Portal、短休/市政工作/进食、库存、私有活动史、公共结果、规则处罚、被动 realtime needs、archetype、属性经验与职业转换，以及 1.3–1.13 policy 下的 Agent 有限移动/Portal/Role/Case acknowledgement/social greeting/procedural Case-review/unverified Case-report、`evidence_required → expired_no_evidence` Case-intake、`server.sealed_law_event` evidence handle、`linked_active → source_window_closed` 唯一 source assignment、`queued → source_window_closed` procedure dispatch、`accepted → completed|expired` owner-private structured task、`planned → step* → arrived|blocked|cancelled` 有限导航计划和 server-owned `granted|denied_capacity → consumed` 单格容量 receipt 已进入同一 V2 timeline。A4 进一步将 immutable profile/budget/attempt snapshot、strict provider parser、独立 worker、retry/backoff/breaker、安全健康投影、single-world queue projection、审计化 deferred-request wake、dead-letter quarantine/release、陈旧隔离告警与管理员控制台纳入非 canonical 的外部 I/O 边界；wire 还加入 API-key Chat Completions 与 Agent Identity Codex Responses 两条专用 system transport，普通 user gateway/billing 路径保持隔离。下一项按其他受控平台逐项扩展 transport；随后进入 A4.3 owner-safe profile selection 与 NPC LOD/reward outbox。
