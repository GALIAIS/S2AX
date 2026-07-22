# 城市模拟共享在线世界实施起点

状态：S0–R3 已实施；R4 正在收敛为可玩的共享角色闭环。当前 realtime 新世界已使用版本化角色生活目录与服务器调度的被动需求更新；Agent 自主决策仍未接入。

## 结论

下一项应从 **S0：共享 world 准入硬化** 开始，而不是从像素地图、Agent、NTP 时钟或素材生成开始。

现有城市系统已经有足够的共享状态骨架：`city_members` 表达多人加入同一 `world_id`，命令在同一 world 内按序写入，调度器也按 world 取得单写者租约。现在阻塞共享在线世界的不是缺少另一套“多人世界”模型，而是一条仍把 world 当作私人资源的数据库约束，以及公共可见性和 realtime 时间线尚未分层。

## 现状对照

| 范围 | 已有能力 | 缺口或风险 | 实施含义 |
| --- | --- | --- | --- |
| World 与成员 | `city_members` 支持一个 `world_id` 下多个活跃成员；现有查询已做成员校验 | 迁移 187 的 owner 私人 world 唯一索引与多城市共享模式冲突 | 复用成员表，先移除过期私人约束 |
| 命令与并发 | 同一 world 的命令有全局序号、幂等键、事务锁 | V24 命令结果仍服务于小时 tick | realtime 复用顺序与单写者模式，不复用小时 tick 语义 |
| 调度 | V24 有按 world 的数据库租约 | 没有 realtime 的时间锚、Frame、到期事件和 cursor | 新 engine/version 专用时间内核 |
| Actor | 所有权和 control grant 已分开 | 查询仅返回本人或被授权操控的 actor；不能直接作为公共在线视图 | 新增只读可见性投影，绝不放宽控制接口 |
| 前端 | 已有城市工作台、Pixi glyph 视图和轮询状态 | 没有 city 专用 SSE、patch store、pixel chunk 渲染器 | 先建立共享快照与 patch，再替换为像素场景 |
| 隐私 | 管理员已有成员管理路径 | 普通成员列表会暴露邮箱/用户名 | 公共成员/actor 投影必须最小化身份字段 |

## 实施顺序

### S0：共享 world 准入硬化（第一项）

目标：让管理员能稳定创建多个共享城市，用户以成员身份进入同一个 canonical `world_id`，且不创建任何用户私有副本。

实施内容：

1. 新增一条迁移，删除 `idx_city_worlds_one_private_active_per_owner`。迁移编号应在现有 250 之后确定，不能修改已经执行过的 187。
2. 清理 `ErrCityWorldExists` 及其专用于“每 owner 一个私人城市”的错误映射、测试预期和文案。
3. 保留 `city_members`、现有管理员成员增删改和现有 control grant；不新增 `world_mode`、分片表或第二套成员表。
4. 将普通成员看到的成员数据收紧为公共显示字段或仅自身记录；管理员保留管理所需完整身份。公共 actor 投影以后也沿用同一最小披露原则。
5. 增加集成测试：同一管理员可建多个活跃 world；两个普通用户加入同一 world；两人读取到相同 world；命令序列只在这一份 world 状态中递增。

完成门禁：

- 同一 owner 可创建两个未归档 world，不影响既有 V24 world。
- 成员 A 与成员 B 都只能访问加入的相同 `world_id`，不会获得 clone/copy。
- 普通成员接口不泄露其他成员邮箱、用户名或控制权限。
- 现有城市迁移和 world/member 集成测试通过。

### R1：独立 realtime 时间内核

目标：在不改变 V24 小时 tick 的情况下，为新的 realtime engine 创建可恢复、可重放、单调推进的世界时间线。

实施内容：

1. 为新的 engine/version 建立世界时间状态、时钟段、Temporal Frame、到期事件和 `timeline_cursor`；表名与字段以 realtime 设计文档为准。
2. 以服务器 UTC 和单调计时为运行基础；NTP 只用于校准/健康观测，不让客户端时间成为权威。
3. 沿用现有“一个 world 一个租约/锁”的原则，确保多实例部署时每个 realtime world 只有一个推进者。
4. 每次推进在同一事务中写入 Frame、状态变更和 cursor；失败可根据持久化 Frame 重放，不能靠内存补偿。
5. 管理员创建 realtime world 时明确选择 engine version；已有 V24 world 永不被自动升级或改写。

完成门禁：

- 重启和抢占租约后 world time 与 cursor 单调，不会出现双推进或重复 Frame。
- 两名成员读取相同 realtime world 时得到同一个 cursor 和相同公共世界时间。
- 一个公共状态命令只产生一次持久化结果，并能通过重放重建。

### R2：共享投影与实时补丁

目标：让不同用户看到同一份共享世界的不同权限视图，而不是各自的世界副本。

实施内容：

1. 提供按 `world_id` 读取的 snapshot/chunk 接口，响应包含 `timeline_cursor` 与 `view_scope_hash`。
2. 增加 city 专用补丁流：客户端从 cursor 续接；若 cursor 过期、权限变化或 scope 不匹配，服务端明确要求全量快照重建。
3. 补丁只包含公共或当前可见信息；私有背包、提示词、内部 Agent 推理、完整成员身份和 control grant 不进入公共 payload。
4. 前端建立独立 patch store，以 cursor 驱动局部更新；不通过整页刷新、全量重拉或当前 private runtime 查询模拟多人在线。
5. 保留现有 actor 控制 API 的私有语义，新增只读投影 API，而不是把“所有 actor”塞进控制台接口。

完成门禁：

- 两个浏览器会话进入同一 world 后，A 的公共移动/活动在 B 端以相同 cursor 收敛。
- B 无法读取 A 的私有字段，也无法借公共投影执行 A 的控制动作。
- 断线重连、重复补丁、过期 cursor 和权限变化均可恢复，不出现状态闪烁或整页 reload。

### R3：像素场景渲染器与最小共享游玩面

目标：用真实共享状态驱动新像素场景，不将现有 ASCII/glyph 视图伪装成最终地图。

实施内容：

1. 新建独立 `PixelSceneRenderer`，不要在 `CityClassicViewport` 上继续堆叠；后者保持 legacy glyph fallback。
2. 先接入语义地面、建筑、门、角色朝向与可见邻近 actor 的确定性 tile resolver；素材初期可以是小型占位 visual pack，但 manifest、图层、碰撞和坐标契约必须是最终形态。
3. 渲染器消费 R2 snapshot/patch，不直接轮询修改 world 状态。
4. 建立摄像机、chunk、遮挡、interior/exterior、可见性与资源缓存边界；UI 层不保存世界真相。

完成门禁：

- 两个用户可在同一像素场景中看到同一公共地形、建筑和对方的可见角色。
- 角色位置变化只更新受影响 chunk，不闪屏、不整页刷新。
- 旧 glyph 地图与新 pixel 场景可按 engine/version 并存。

### R4：共享世界的角色游玩闭环

目标：在接入 Agent 前，先让真人角色在同一世界内完成有后果的最小生活循环。

实施内容：

1. 用户加入管理员创建的 world 后创建或绑定角色；角色数量、控制权和公开资料由现有成员/actor 语义约束。
2. 接入移动、进入/离开建筑、活动、库存/消费、基础需求、规则违法与处罚的 realtime action/result 流。
3. 角色状态、地图状态、经济账本和法律事件全部归属于同一 `world_id` 与 timeline，不按用户拆副本。
4. 提供只读世界事件记录和角色自己的私有记录，分别施行可见性过滤。
5. 基础需求必须由 server-owned realtime due event 推进，而不是由浏览器、页面刷新或 action 请求携带时间。角色生活目录以 world binding 固定；新目录可引入 `metabolism_revision` 与新 hash schema，旧 world 继续保留原目录和原 canonical hash。

完成门禁：

- 两个用户可以同时在同一城市看见彼此产生的公共后果。
- 角色无法越权操作其他成员角色；规则处罚和经济变动可追溯、可重放。
- 用户可以完整地加入、活动、获得/失去虚拟货币并离开/重连，而不依赖 Agent。
- 玩家活动不能使已排队的被动需求事件失效：due event 的 expected version 指向独立的 metabolism head，而不是通用 profile revision。

### R5：Character Agent 与 NPC Agent

目标：在已可玩的共享实时世界上接入自主角色，而不是让 Agent 替代未完成的游戏底层。

实施内容：

1. Character Agent 通过与真人角色同一组权威 action API 操作，只拥有该角色可见观测和能力。
2. NPC manager/子 Agent 也只作用于共享 world 的同一时间线；任何私有观测都不能生成第二份世界状态。
3. 主模拟 Agent 只做世界级计划、事件编排和治理；不得越过规则、账本和 action validator 直接改状态。
4. Agent 的提示词、工具结果、预算、版本、决策摘要与执行 action 均需审计和回放关联。

完成门禁：

- 角色 Agent 与真人角色产生的动作在同一 action pipeline、同一 cursor 和同一规则系统中结算。
- Agent 故障、超时、重复提交和模型更换不会破坏世界单调性或造成重复奖励。

### R6：视觉资产、国家风格与内容扩展

目标：在世界语义和渲染合同已经稳定后，再规模化生成和运营开罗式像素素材与国家模板。

实施内容：

1. 建立 visual pack、atlas、版权/来源、版本、回滚和缓存策略；由语义 tile resolver 决定素材，不把图像路径写死到 gameplay 逻辑。
2. 按国家/地区模板扩展道路、建筑、地形、室内布局和交通规则；模板只影响生成和视觉层，不能改变共享 world 的权威模型。
3. 在测试环境先验证色盲对比、缩放、遮挡、性能和热更新，再投入完整生成式素材管线。

## 明确不应抢跑的工作

- 不在 S0 前实现角色 Agent、Root Agent 或 NPC 子 Agent。
- 不在 R1 前用现有 V24 `current_tick` 伪装实时世界。
- 不在 R2 前把当前私有 actor 查询扩展成“所有在线角色”，这会泄露控制与身份数据。
- 不在 R3 前批量生成像素素材；没有稳定的 semantic/tile/patch 合同，资产会被迫重做。
- 不为共享 world 新增一套 user-world 复制表；`world_id` 必须始终指向同一份 canonical 状态。

## 首个提交边界

第一个实现提交只包含 S0：新迁移、私人 world 约束清理、成员隐私投影与测试。不夹带 realtime、Pixi、Agent、地图或素材改动。这样可以先证明“一个管理员能管理多个共享城市、多人在同一 world 内安全共存”这一条根契约，再沿上述顺序向上实现。
