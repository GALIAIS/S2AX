# 城市模拟 F5：快照、逐 Tick 重放与投影恢复详细设计

版本：v1.1（2026-07-18）  
首次实现版本：`city-f5-v1`；当前扩展版本：`city-f6-v1`  
状态：已实现；F6.1 已完成规范状态、事实归约和恢复扩展验证

## 1. 目标与边界

F5 解决的不是“把数据库整库备份回来”，而是证明城市模拟的当前投影能够由不可变事实确定地复算，并在投影漂移时安全重建。它提供三层能力：

1. **规范快照**：保存某一 tick 的完整、稳定、可验证状态检查点。
2. **逐 tick 重放**：从检查点出发，只读取不可变事实，逐步归约并与每个检查点比较。
3. **当前投影恢复**：只有已验证重放才能重建当前 tick 的可变投影，且失败时不留下部分修改。

F5 不做以下事情：

- 不删除 journal、resource operation、settlement、command、tick 或 event。
- 不把世界倒回历史 tick，也不重写已经发生的历史。
- 不把快照当作新的业务事实来源。
- 不跨模拟版本猜测迁移规则。
- 不替代 PostgreSQL 物理备份、WAL 归档或灾难恢复。

命令、总账、实物流转和市场结算仍是状态迁移的唯一事实；快照是检查点，重放是审计，恢复只修复可重建投影。

## 2. 故障模型

系统明确处理以下故障：

| 故障 | 检测方式 | 处理方式 |
| --- | --- | --- |
| 在线投影被误更新 | 当前规范哈希与同 tick 快照不一致 | 阻止下一 tick；要求 verified replay 后恢复 |
| 快照载荷损坏 | 压缩载荷 SHA-256、长度和 gzip 校验失败 | 拒绝读取、重放和恢复 |
| 快照元数据被伪造 | 状态 SHA-256、tick、版本和规范 JSON 不一致 | 标记完整性失败 |
| 归约器与事实不一致 | 每 tick 比较 tick hash、snapshot hash 和规范字节 | replay 终态为 `diverged`，记录首个差异位置 |
| 恢复中途失败 | savepoint、延迟约束立即检查、终态再哈希 | 回滚到 savepoint，保留 `failed` 审计记录 |
| 客户端重试 | 请求指纹与唯一幂等键 | 返回原运行记录；同键不同意图冲突 |
| 非成员探测世界 | 统一成员授权 | 返回世界不存在，避免信息泄露 |

F5 不声称能从不可变事实本身被恶意删除或数据库介质整体损坏中恢复；此类场景依赖数据库备份和独立审计副本。

## 3. 数据模型

### 3.1 `city_snapshots`

F5 初版每个世界每个 tick 最多一条；F6 起唯一键升级为 `(world_id, tick, simulation_version)`，允许保留旧版本检查点并在同 tick 建立新版本升级基线：

- `world_id`、`tick`：稳定检查点身份。
- `source_tick_id`：tick 0 为空；正 tick 必须指向同世界同 tick 的不可变 `city_ticks`。
- `simulation_version`：必须与规范状态和源 tick 一致。
- `snapshot_format`：固定为 `city-state-v1+gzip`。
- `reason`：`genesis`、`baseline`、`tick` 或预留的 `manual`。
- `state_hash`：未压缩规范 JSON 的 SHA-256。
- `payload_hash`：压缩字节的 SHA-256。
- `payload`、`uncompressed_size`、`compressed_size`。

快照创建后禁止更新和删除。`tick` 原因快照还必须与源 tick 的 `state_hash`、`simulation_version` 完全一致。

正常列表、详情、重放和恢复只选择世界当前模拟版本，避免跨版本归约；旧 `city-f5-v1` 规范载荷仍可按原字段顺序完成完整性验证，不会因 F6 新增字段失去审计可读性。

### 3.2 `city_replay_runs`

重放运行是不可删除审计事实，保存：

- 发起用户、幂等键、请求指纹。
- 基线快照、起始 tick、目标 tick。
- `running → verified | diverged | failed` 的一次性状态转换。
- 期望哈希、实际哈希、已验证 tick 数。
- 首个差异 tick、稳定 JSON Pointer、错误代码与受限错误摘要。

终态记录不能再次修改，避免事后把失败重放伪装为成功。

### 3.3 `city_recovery_runs`

恢复运行保存：

- 所属世界、发起用户、幂等键和请求指纹。
- 已验证 replay、目标快照与目标 tick。
- 恢复前哈希、目标哈希、恢复后哈希。
- 成功修改的投影行数。
- `running → applied | failed` 的一次性状态转换和错误摘要。

数据库恢复写闸门同时校验事务局部 `recovery_run_id`、世界 ID 和运行状态。普通请求不能仅设置会话变量绕过投影保护。

## 4. 规范状态契约

规范状态由 `cityHashState` 统一编码，当前包含：

- 世界名称、状态、版本、种子、tick、模拟时间、速度、时区和规范设置。
- 世界内货币、科目模板、经济主体和账户投影。
- 区域、家庭群体、企业、政府、预算、资源、配方和库存投影。
- 经济周期、政策、市场报价、住房占用等 F4 投影。
- 世界本地日历、人口政策、18 个 cohort 年龄结构和确定性余数等 F6.1 投影。

编码规则：

1. 金额、数量、tick、版本号使用整数；速度使用千分整数。
2. JSON 扩展字段先验证再规范化。
3. 数据库查询和内存集合按稳定业务键排序。
4. 不包含数据库自增 ID、平台用户 ID、墙钟时间、更新时间和执行耗时。
5. 使用标准 JSON 编码生成唯一字节序列，再计算 SHA-256。
6. 新模型若影响未来行为，必须进入规范状态；只用于展示且可由事实重算的数据不得反向成为事实。

任何字段增删、排序变化或舍入规则变化都必须升级 `simulation_version`，不能静默改变旧世界哈希。

## 5. 快照生命周期

### 5.1 创世快照

创建世界的同一事务内完成全部 F0–F4 初始化，加载规范状态，写入 tick 0 `genesis` 快照和世界 `state_hash` 后再提交。任何一步失败都会回滚整个世界。

### 5.2 升级基线

从 F4 升级的既有世界没有可信的 F5 历史快照。系统只在当前 tick 建立 `baseline`：

- 不伪造旧 tick 快照。
- 首次继续运行前，当前投影必须与已有世界哈希一致。
- 后续重放只能从实际存在的最早基线开始。

### 5.3 每 Tick 快照

每次 `StepWorld` 在同一事务中执行：

1. 锁定世界并验证当前 tick 快照与在线投影一致。
2. 应用命令和到期市场周期。
3. 生成新的规范状态与哈希。
4. 写不可变 tick、事件和业务事实。
5. 写同 tick `tick` 快照。
6. 更新世界哈希并一次提交。

因此不存在“tick 已提交但快照缺失”或“快照领先于业务事实”的可见状态。

### 5.4 压缩与完整性

- gzip 使用固定 header 时间和 OS 字段，保证同一规范字节得到同一压缩字节。
- 未压缩状态上限 32 MiB；压缩载荷也有独立上限。
- 解压使用受限 reader，禁止压缩炸弹扩张。
- 解压后重新计算状态哈希、严格 JSON 解码并再次编码；只有字节完全相同才是规范状态。

列表 API 只读取快照元数据，不把大载荷从数据库加载到应用内存；详情读取会验证完整性，但不会向客户端返回原始载荷。

## 6. 逐 Tick 重放算法

### 6.1 输入与限制

- 只有世界 owner 可以发起重放；活跃成员可以读取运行记录。
- 起始 tick 必须存在快照，目标 tick 不得超过世界当前 tick。
- 单次最多重放 10,000 tick，避免同步请求无限占用数据库连接。
- 省略起始 tick 时选择不晚于目标的最早可用快照；省略目标 tick 时绑定发起时的当前 tick。
- 幂等请求指纹保存“用户实际提交的可选参数”，因此同一省略参数请求在世界继续推进后仍返回原运行，而不会改变原意图。

### 6.2 事实读取顺序

从基线状态开始，对每个 `from_tick + 1 ... target_tick`：

1. 读取不可变 `city_tick` 并校验前一哈希链。
2. 按命令序列归约控制命令结果。
3. 按 journal/entry 稳定顺序归约账户余额和版本。
4. 按 resource operation/entry 稳定顺序归约库存和版本。
5. 按日/月/季度/年 boundary 归约本地日历，并按 population movement/line 归约年龄结构和家庭人口投影。
6. 按 settlement 顺序归约劳动分配、家庭就业和企业员工。
7. 归约住房 allocation、住房占用和租金。
8. 归约财政 budget movement。
9. 归约市场报价、供需结果和经济周期。
10. 验证 PRNG 证明、命令计数和事实链。
11. 生成规范状态，与 tick hash 和同 tick 快照逐字节比较。

重放器不读取在线账户余额、库存、就业、住房、预算或市场投影作为下一步输入；否则投影漂移会被重放“继承”而无法发现。

### 6.3 差异报告

若哈希不同，系统对期望规范 JSON 和实际规范 JSON 做稳定深度比较：

- object key 先排序。
- array 使用稳定索引。
- JSON Pointer 对 `~`、`/` 正确转义。
- 记录首个差异路径和对应 tick。

错误详情先折叠空白，再按 Unicode code point 限制为 512 字符，避免截断 UTF-8 或突破数据库列限制。

## 7. 当前 Tick 投影恢复

### 7.1 前置条件

恢复必须同时满足：

- 发起者是世界 owner。
- replay 终态为 `verified`。
- replay 的期望哈希与实际哈希相同。
- replay 目标 tick 等于世界当前 tick。
- 当前 tick 快照完整性通过，且哈希等于 replay 结果。
- 快照模拟版本等于世界模拟版本。

这些条件防止使用旧重放回滚历史、使用失败重放写状态，或跨版本恢复。

### 7.2 事务流程

1. 取得单世界 advisory lock 并锁定世界。
2. 写入 `running` 恢复审计记录。
3. 建立 `SAVEPOINT city_projection_recovery`。
4. 激活仅本事务可见、绑定恢复运行的写闸门。
5. 按稳定业务键更新世界、账户、实体、实物和市场投影。
   F6.1 同时恢复日历、人口政策、年龄结构和对应家庭人口/劳动年龄投影。
6. 重新加载完整规范状态，比较目标哈希和规范字节。
7. 执行 `SET CONSTRAINTS ALL IMMEDIATE`，在释放 savepoint 前强制检查延迟守恒约束。
8. 成功则释放 savepoint 并终结为 `applied`；失败则回滚到 savepoint，终结为 `failed`。
9. 提交恢复审计。

失败恢复的 `restored_projection_count` 固定为 0；它不会留下部分恢复结果，但保留可调查的失败记录。

### 7.3 可恢复与不可恢复内容

可恢复内容是规范状态中已有且拥有稳定业务键的可变投影，例如余额、库存、就业、住房占用、预算和市场状态。

主体代码、世界归属、历史事实等身份键不允许由恢复流程猜测重建。若稳定身份键本身损坏，应先通过受审计的数据库运维修复身份，再重新执行恢复。F5 不提供删除/重建历史身份的危险旁路。

## 8. API 契约

```text
GET  /api/v1/city/worlds/:world_id/snapshots
GET  /api/v1/city/worlds/:world_id/snapshots/:tick

GET  /api/v1/city/worlds/:world_id/replay-runs
POST /api/v1/city/worlds/:world_id/replay-runs
GET  /api/v1/city/worlds/:world_id/replay-runs/:run_id

GET  /api/v1/city/worlds/:world_id/recovery-runs
POST /api/v1/city/worlds/:world_id/recovery-runs
GET  /api/v1/city/worlds/:world_id/recovery-runs/:run_id
```

重放请求：

```json
{
  "from_tick": 0,
  "target_tick": 240
}
```

恢复请求：

```json
{
  "replay_run_id": 17
}
```

两个写接口都强制 `Idempotency-Key`。列表使用稳定游标和受限 page size，不使用 offset。运行结果是审计事实：业务层重放差异或恢复失败会返回可读取的终态运行记录，而不是抹掉记录后只返回瞬时错误。

## 9. 错误语义

| 错误码 | 含义 |
| --- | --- |
| `CITY_SNAPSHOT_NOT_FOUND` | 请求的检查点不存在 |
| `CITY_SNAPSHOT_INTEGRITY_FAILED` | 快照、在线投影或规范状态不一致 |
| `CITY_REPLAY_RANGE_INVALID` | 起止 tick 非法、超当前 tick 或超过上限 |
| `CITY_REPLAY_CONFLICT` | 同一重放幂等键被用于不同请求意图 |
| `CITY_REPLAY_NOT_FOUND` | 重放运行不存在或不属于该世界 |
| `CITY_RECOVERY_PRECONDITION_FAILED` | replay、tick、版本或哈希不满足恢复条件 |
| `CITY_RECOVERY_CONFLICT` | 同一恢复幂等键被用于不同 replay |
| `CITY_RECOVERY_NOT_FOUND` | 恢复运行不存在 |

对非成员仍优先返回世界不存在，避免泄露审计记录。

## 10. 可观测性与运行策略

当前同步 API 已保存完整审计事实；产品运行层应进一步导出：

- 快照大小、压缩率、生成耗时和完整性失败数。
- replay 范围、耗时、吞吐 tick/s、verified/diverged/failed 数。
- 首个差异 tick、差异路径和模拟版本。
- recovery applied/failed 数、恢复行数和失败阶段。
- 世界当前 tick、最后快照 tick、最后 verified replay tick。

日志只记录世界、tick、运行 ID、错误码和受限摘要，不输出快照原文、用户密钥或其他敏感字段。

后台批量审计应按世界限流，使用任务队列和租约，不在一个请求内跨世界并行持有事务。超过 10,000 tick 的历史审计应分段验证相邻检查点。

## 11. 验收矩阵

- 同一规范状态两次 gzip 字节完全相同。
- 任意载荷字节、长度、哈希、版本或 tick 篡改均被拒绝。
- 创世、基线和每 tick 快照与世界状态在同一事务提交。
- 从 tick 0 经控制、账本、实物和 F4 市场事实重放到目标 tick，哈希和规范字节完全一致。
- 从 tick 0 经日历 boundary 和人口 movement 重放跨年自然变化，F6 规范哈希和字节完全一致。
- replay 省略默认 tick 后，即使世界继续推进，同一幂等键仍返回原运行。
- 人为修改当前投影后，下一 tick 被阻止。
- verified replay 能恢复当前投影并恢复目标哈希。
- 故意破坏稳定身份导致恢复中途失败时，savepoint 回滚所有部分修改，失败运行仍可读取。
- 延迟守恒约束在释放 savepoint 前完成检查。
- 快照、终态 replay 和终态 recovery 不能修改或删除。
- 非成员不能读取，非 owner 不能发起重放或恢复。

## 12. 后续模型接入清单

F6 及以后每增加一个影响模拟结果的子系统，必须同时完成：

1. 把新投影加入规范状态并定义稳定排序键。
2. 为状态变更增加不可变 movement/entry/settlement 事实。
3. 在逐 tick replay reducer 中只依赖这些事实归约新状态。
4. 在 recovery mapper 中按稳定业务键恢复新投影。
5. 增加创世、逐 tick、差异、恢复失败和长期重放测试。
6. 升级模拟版本，并保留旧版本读取和重放能力。

未同时完成上述六项的功能不能进入正式 tick；否则它会成为无法证明、无法恢复的隐式状态。

## 13. 代码映射

- 数据库约束：`backend/migrations/192_city_snapshots_replay_recovery.sql`
- 快照、重放与恢复服务：`backend/internal/service/city_recovery.go`
- 规范状态：`backend/internal/service/city_simulation.go`
- 世界创世与 tick 接入：`backend/internal/service/city_economy.go`、`city_simulation.go`
- HTTP 接口：`backend/internal/handler/city_economy_handler.go`
- 路由：`backend/internal/server/routes/user.go`
- 单元测试：`backend/internal/service/city_recovery_test.go`
- 迁移约束测试：`backend/migrations/city_snapshots_replay_recovery_migration_test.go`
- 闭环集成测试：`backend/internal/repository/city_snapshot_replay_recovery_integration_test.go`
