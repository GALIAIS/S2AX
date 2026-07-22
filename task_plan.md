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
- [ ] Phase 7 / R5: Character Agent 与 NPC Agent（A0/A1/A2/A3.1/A3.2 的用户角色受限闭环已完成；NPC 生命周期、外部模型和奖励仍待实现）

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

## Errors Encountered

- `cmd/server/wire_gen_test.go` 在新增 scheduler cleanup 依赖后缺少参数，已同步更新并通过生成接线测试。

## Status

**Paused after Phase 7 / R5 A3.2** - 用户角色创建、共享可见位置、地表移动、建筑/楼层 Portal、短休/市政工作/进食、库存、私有活动史、公共结果、规则处罚、被动 realtime needs、archetype、属性经验与职业转换，以及 1.3 policy 下的 Agent 有限移动/Portal/Role intent 已进入同一 V2 timeline，并通过真实 PostgreSQL/Redis integration 验证。下一阶段不是继续无边界扩功能：应先实施 A3.3（Rule/Case、关系、任务、交通和规划 action adapter 的逐项契约/回放），再按 A4 接入模型 profile、受控路由、预算和熔断，最后才开始 NPC LOD 与奖励 outbox。
