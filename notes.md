# Notes: 城市模拟共享在线世界实施起点审计

## Scope

- 仅分析当前实现和已冻结设计，不修改产品代码。
- 目标是找出共享在线 realtime world 的最小正确落点，而不是继续扩展城市经济或 Agent 功能。

## Findings

### 可直接复用的共享 world 基础

- `city_members` 已按 `world_id + user_id` 表达多名活跃成员；现有 world 查询和成员校验已沿用它。
- 命令提交已经在一个 world 内生成全局顺序，并以幂等键保护重复提交。
- V24 scheduler 已有按 world 的数据库租约和事务锁，可作为 realtime 单写者的实现参照。
- 现有 actor control grant 能区分“可操控”与“只可查看”，无需为共享 world 再造成员表。

### 直接矛盾与缺口

- 迁移 187 仍有 `idx_city_worlds_one_private_active_per_owner`：同一 owner 不能拥有两个未归档且 `group_id IS NULL` 的 world。当前创建路径没有 group，因此它与“管理员创建多个共享城市”直接冲突。
- `ListWorldMembers` 对普通成员返回完整邮箱、用户名；共享在线 world 必须在进入公共视图前做身份最小化投影。
- Open World runtime 的 actor 查询只返回本人拥有或被授权控制的 actor。这适合控制台，但不能承担其他在线成员/NPC 的公共可见性。
- V24 是小时 tick；当前城市前端是 glyph/polling 视图。没有 city 专用 Temporal Frame、timeline cursor、pixel chunk 或事件流契约。前端唯一命中的流式代码属于账号测试，不是城市同步。

### 结论

- 先做共享 world 准入硬化（S0），再独立建设 realtime 内核（R1）。
- 不修改 V24 的时间语义；不在 S0 先做角色 Agent、NPC Agent、像素素材或图像生成。

## S0 实施记录

- 新增迁移 251，删除每 owner 一个活跃私人 world 的旧 partial unique index。
- 删除已失效的 `ErrCityWorldExists` 映射；同一管理员现在可以创建多个 world。
- 非系统管理员读取 roster 时 email 被服务端清空；前端在没有公开 username 时显示 `#user_id`，不出现空白成员名。
- 增加数据库迁移静态测试、同 owner 多 world/共享成员的 integration 覆盖，以及前端脱敏 roster 回退测试。

## R1 realtime kernel 实施记录

- `city-openworld-realtime-v1` 是独立 engine；不会进入 V24 小时 tick、V24 调度器或 F7/F24 worldgen upgrade 路径。
- 迁移 252 建立 profile/node/time-state/clock-segment/Temporal Frame/due-event/continuation/realtime schedule lease 表；Frame 与 profile 不可变，due-event 的 frame FK 为 deferred，同一事务可先写 canonical queue 再封存 Frame。
- 目前只有 `realtime-diagnostic-v1` 的 frozen test profile；没有生产 profile、没有 HTTP 时钟推进端点，浏览器时间不可成为权威输入。
- 建立 member-safe `clock` / `timeline` API；timeline 不返回 worker lease、node、payload 或运维错误。
- 新增诊断 due-event 闭环：创建事件写入同一时刻的 diagnostic Frame；按 due time → phase → priority → dedup key 决定结算顺序；已知诊断事件 applied，未知类型 rejected，二者都写入结算 Frame。重复 dedup key 返回冲突；空时间区间不写 Frame；普通成员和非管理员上下文不能调用 internal primitives。
- integration 已验证：同批/跨批 due event、重复去重、阻止绕过待结算事件的诊断 clock advance、未知事件 terminal reject、state/frame cursor 连续性与 `next_due_at_world_time_us` 清空。

## R1 production profile 与恢复补充记录

- realtime world 的 clock profile 已成为显式的 server-only 创建输入：非 realtime world 拒绝 profile；realtime world 必须处于 `WithCitySystemAdministrator` 上下文。空 realtime profile 只会回退到编译期固定的 diagnostic profile。
- production profile 必须是数据库中已发布的不可变 `system_ntp` / `system_nts` / `private_time_service` 配置；不会在 migration 中伪造可运行的 production 时钟配置。
- 新增 server-only system due-event 入口。它固定 source kind 为 `system`，走与诊断事件相同的 canonical queue/hash/frame 事务，并使用可安全处理的 `system.realtime.noop` 作为 bootstrap/recovery barrier；未知 event schema 仍 terminal reject，不会隐式执行。
- integration 已覆盖：无管理员标记拒绝创建 production world、unsafe Clock Authority 将 world canonical hold、健康观测建立新 segment 和 bounded catchup、已排队 event 及最终空区间各产出一帧 recovery、最终回到 `healthy/idle`，且没有任何 V24 tick。

## R1 生命周期、运维与启动接线完成记录

- `CityRealtimeHostClockAuthority` 默认不可运行；只有配置同时显式启用 `city_realtime_clock.enabled` 与 `trust_host_clock`，且 node/mode/uncertainty/skew 边界均合法时才产生观测。它用进程单调经过时间检查 wall-clock 回拨或大跳变；PostgreSQL reducer 仍二次检查数据库时间偏差。
- 当前内置 authority 仅支持 `system_ntp` / `system_nts` 的受控宿主机；它不接受用户或管理员提供的网络 URL，因此不会把私有时间服务配置变成 SSRF 面。私有时间服务必须作为独立、可审计的 authority 实现。
- scheduler 只有 host authority 可运行、城市模拟总开关及 `city_realtime_scheduler_enabled` 均开启时才启动；authority 不支持的 production profile 仅计入 `unsupported`，不会污染该世界的 canonical clock state。
- 管理员可读 `/api/v1/admin/city/clock-health`；返回 world/segment/recovery/scheduler 的安全摘要，不返回 worker lease token、provider endpoint、凭据或原始错误。
- 管理员 pause/resume 路由没有请求 body，也不接收客户端时间。controller 在服务端读取世界固定 profile 后向 authority 取 observation；pause 在 due-event 边界稳定后关闭 segment，resume 建立新 segment，二者都封存 lifecycle Frame 并要求幂等键。
- 验证：`go test ./internal/service -run '^TestCityRealtime' -count=1 -v`、configuration/route/wire 单元测试和 `go test -tags integration ./internal/repository -run '^TestCityRealtime' -count=1 -v` 全部通过。

## R2 共享投影、静态世界与 patch 实施记录

- 新增 `city-openworld-realtime-v2`，它继续使用 R1 的 Temporal Frame、可信时钟、生命周期和 due-event 内核，但明确不属于 `cityEngineSupportsOpenWorld`；因此 V24 小时 tick、`city_open_world_*` 物化、旧 Actor runtime 和升级链不会被误调用。
- 迁移 253 建立独立 `city_realtime_spatial_*` 表：binding、region、sector、surface chunk、building、interior 和 portal 均以 genesis frame `0` 建立 deferred 因果 FK，且通过 transaction-local world-id gate 和 trigger 在 genesis 后完全冻结。
- V2 创建时以冻结的 profile/seed 通过 C:DDA 风格的 V3 planner 与 V2 surface generator 物化起始 region/sector；日本与中国 metropolitan profile 保留为 generator binding，而不是前端主题分支。
- V2 canonical hash 包含 temporal hash 和全部 static semantic hash；PNG、sprite atlas、浏览器纹理、客户端缓存、在线状态与用户邮件不进入 canonical state。
- 新增成员安全读取契约：`/realtime/projection` 返回同一 shared cursor 与本人的最小权限范围；`/realtime/pixel-chunks/:x/:y/:z` 返回经 payload hash 验证的语义 surface chunk；`/realtime/patches` 使用 frame cursor 补拉，初始 `after_frame_sequence=-1` 明确要求 static bootstrap。
- integration 覆盖证明：owner/viewer 读到相同 cursor/static hash，viewer DTO 不含任何邮箱，outsider 被拒绝，V2 不写 legacy worldgen/tick 表，chunk 可验证读取，世界生成后拒绝任意 static 表 UPDATE，V1 拒绝 V2 projection API。
