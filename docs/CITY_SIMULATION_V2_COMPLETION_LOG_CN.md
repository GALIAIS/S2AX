# V2 开放世界城市模拟完成记录

本文档记录 V2/V3 开放世界城市模拟从当前实现到可运行基础闭环的实际完成项、验证命令与已知边界。

> 状态：实施中。最终内容仅记录已落地并验证的行为。

## 已完成：Region 物化与不可变校验

- 新建带世界风格的城市默认绑定 `city-openworld-v3`；旧 `city-openworld-v1` 与 V2 事实不被重写。
- V2 使用 `1.4.0` Region 计划和地面室内契约；V3 使用 `1.5.0`，把全楼层室内和入口/楼梯拓扑纳入 Sector 内容哈希。
- Region 为 32×32 Chunk，Sector 为 8×8 Chunk；扩张经 `open_world.sector.materialize` 命令和城市 Tick 原子封存。
- 后端会验证 Region Plan、Sector Surface、Chunk 负载、建筑所有权、每层室内与 Portal 拓扑；错误只报告不变量，不会自动覆写事实。
- 提供全世界（≤256 Sector）和单 Region 的只读验证接口；前端开放世界面板可验证当前 Region，不会刷新整个页面或生成额外数据。

详细契约见 [《开放世界生成器 V2 / V3 实现契约》](CITY_OPENWORLD_GENERATOR_V2_CN.md)。

## 已验证

```text
go test ./internal/service ./internal/handler ./internal/server/routes ./internal/cityspatial ./migrations
pnpm typecheck
pnpm exec vitest run src/api/__tests__/citySpatial.openWorld.spec.ts src/features/city-spatial/__tests__/CityOpenWorldWorkspace.spec.ts
```

## 当前边界

- V3 已封存楼层与 Portal 静态拓扑，但角色位置、跨层路径、NPC 行为和动态门禁尚未接入该空间域。
- 当前 Profile 仅覆盖日本/中国都市的基础道路、地块和建筑目录，尚不涵盖完整现实交通网络或地下设施。
- 大世界必须按 Region 进行运维校验；全世界规范哈希校验有 256 Sector 保护上限。
