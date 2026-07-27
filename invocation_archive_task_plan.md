# Task Plan: 调用归档审查工作台完善

## Goal

将调用归档收敛为可持续使用的管理员审查工作台：单一页面标题、可靠的手动/自动刷新、无理由但可追溯的直接查看，以及不闪烁的记录浏览体验。

## Phases

- [x] Phase 1: 审计现有调用归档的页面、API、服务、路由和审计链
- [x] Phase 2: 写入实现设计与兼容性边界
- [x] Phase 3: 实现页面层级、刷新和自动刷新
- [x] Phase 4: 实现无理由直接查看及保留审计证据
- [x] Phase 5: 补齐测试、类型检查和构建验证
- [x] Phase 6: 实现安全的结构化载荷阅读、字符集预览与乱码诊断
- [x] Phase 7: 保留 WebSocket 帧边界并补齐工具调用/工具输出的完整结构化审查

## Decisions Made

- 全局 `AppHeader` 是唯一页面标题/描述来源；归档视图不再重复渲染第二组标题。
- “设置”顶部按钮移除；已有的“归档策略”标签页继续承担配置入口。
- 自动刷新只刷新记录和运行态，并仅在页面可见、没有敏感操作或未保存策略时运行；不会清空当前筛选、分页或已选记录。
- 直接查看仍要求管理员权限与现有二次验证；仅取消人工填写理由。查看者、时间、结果、客户端信息仍写入不可随记录删除而丢失的访问证据。
- `direct_view_enabled` 仍是显式载荷可见性策略，不在本次更新中静默放宽已有部署的归档隐私策略。
- 载荷阅读只派生前端内存展示，不修改加密原文；任何自动字符集猜测都不能写回归档。
- 新归档仅保留经 MIME 解析后的 `charset` 参数；旧归档仍可手动选择字符集预览，且 Base64 原文始终可复制。
- WebSocket 下行帧的原始字节只加密保存一次；帧目录仅保存偏移和元数据，直接查看时再从同一原文派生每帧内容，避免工具输出重复入库。
- 请求与响应归档上限可配置至 256 MiB；超过管理员选择的上限或 WebSocket 帧目录安全上限时必须显式标识，不能把部分内容显示为完整内容。

## Errors Encountered

- 初次将视图、API 和后端改动合并为一个补丁时，目标上下文不一致；已拆分为受控的小补丁，未写入部分变更。
- 前端类型检查最初发现事件参数和 interval 返回类型不匹配；已改为显式无参重试与浏览器 `number` timer，随后 `pnpm.cmd typecheck` 通过。
- 初版组件测试的 `vue-i18n` mock 覆盖了应用初始化所需的 `createI18n`；已改为保留真实导出，仅替换 `useI18n`。

## Verification

- `go.exe test ./internal/invocationarchive` 通过。
- `go.exe test ./internal/server/routes -run TestInvocationArchive` 通过。
- Phase 5 基线：`pnpm.cmd test:run` 通过：228 个测试文件、1,474 个断言。
- Phase 5 基线：`pnpm.cmd build` 通过（含 Vue 类型检查与生产构建）。
- `go.exe test ./internal/invocationarchive ./internal/server/routes -run 'Test(InvocationArchive|MediaType|PayloadEnvelope)'` 通过。
- `pnpm.cmd exec vitest run src/features/invocation-archive/__tests__/payloadPresentation.spec.ts src/features/invocation-archive/__tests__/InvocationArchiveView.spec.ts src/features/invocation-archive/__tests__/api.spec.ts` 通过：3 个测试文件、8 个测试。
- `pnpm.cmd typecheck` 与 `pnpm.cmd build` 均通过；生产构建包含新的调用归档阅读工作台。
- `go.exe test ./internal/invocationarchive` 通过，覆盖 WebSocket 帧边界、二进制帧、工具结果请求及帧目录上限；`go.exe test ./internal/service -run '^$'`、`go.exe test ./internal/handler -run '^$'` 编译通过。
- `pnpm.cmd exec vitest run src/features/invocation-archive/__tests__/payloadPresentation.spec.ts` 通过（7 个测试），覆盖 Responses 工具调用、工具结果、SSE 和 WebSocket 帧展示；`pnpm.cmd typecheck`、`pnpm.cmd build` 通过。

## Status

**Complete** — 调用归档审查工作台及其安全载荷阅读、字符集预览和乱码诊断已完成。
