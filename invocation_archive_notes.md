# Notes: 调用归档审查工作台

## Current implementation findings

- `AppHeader` 已从路由 meta 渲染“调用归档”及说明；`InvocationArchiveView.vue` 又渲染一组相同标题，形成截图中的重复标题。
- 顶部“刷新”仅调用 `loadRuntime()`。记录页不会刷新，因此用户在“归档记录”标签页看不到任何结果变化。
- `loadRecords()` 每次完成都会清空 `selectedIDs`，并在后台刷新时把整张表替换为 loading 行，不适合作为自动刷新基础。
- 详情查看需要 `revealReason`，前端要求至少 3 字，API 要求 `{ reason }`，服务端同样拒绝空理由。
- 直接查看已经具备关键安全边界：管理员路由、二次验证、`no-store` 响应、AES-GCM 解密、访问日志与载荷独立保留期。应复用这些边界，而不是另建查看通道。

## Chosen implementation boundaries

- 自动刷新周期为 15 秒；仅在浏览器标签可见时运行，避免后台数据库轮询。
- 配置草稿未保存时不拉取配置，防止覆盖管理员编辑；记录和运行态仍可安全刷新。
- 查看请求无 body；服务端接受空 POST，并以空理由写入现有访问日志模式。

## Completed implementation notes

- 记录列表在首次加载后会保留现有行，后台刷新不再把整个表格切换为 loading 状态；旧响应由单调请求序号忽略。
- 手动刷新同步记录和运行态；策略仅在没有未保存草稿时同步。页面可见且无敏感操作时每 15 秒执行同一安全刷新。
- 详情页直接查看不再收集理由，但仍通过管理员二次验证、`direct_view_enabled`、`no-store`、加密解密和访问日志保护。

## Payload presentation findings

- 当前后端会将无效 UTF-8 原始字节以 Base64 加密保存，但前端只把 Base64 原样放进 `<pre>`，没有利用正文类型或提供解码预览。
- 当前 `mediaType` 丢弃 `Content-Type` 的 `charset` 参数，因此新归档无法按上游声明的 GB18030、Big5 等字符集在前端正确读取。
- `U+FFFD` 已在归档明文中出现时，原始字节在归档前已经丢失；只能显式告知，不应伪造恢复。

## Payload presentation implementation

- 详情对话框提供结构化、格式化、原始和可选本地修复预览；支持 JSON、JSON Lines、SSE、表单字段、OpenAI 兼容消息与工具调用卡片，所有内容都以文本节点渲染。
- Base64 正文可根据新归档保留的 `Content-Type charset` 自动无损解码，也可在内存中手动切换 UTF-8、GB18030、Big5、Shift_JIS、Windows-1252、UTF-16 LE/BE；源密文、原始 Base64 和数据库记录不会被修改。
- 示例中的 `����` 属于 `U+FFFD` 替换字符：这是工具/终端在进入网关前已用错误字符集解码后的结果，原始字节不在归档内，无法可靠恢复。页面会展示可操作的明确提示，而不是伪造“修复成功”。

## Tool and WebSocket archive completion

- HTTP 路径已经在网关请求读取与响应写入处观察原始字节，因此工具调用参数和工具输出会随其协议正文进入归档；本次补齐的是所有兼容协议的可读呈现。
- 旧 WebSocket 路径把成功写出的客户端消息直接拼接，无法审查每条工具事件的边界；现已把帧目录与连续原始字节一同加密，直接查看时可看到文本/二进制类型、写入顺序、时间、字节数和原始内容。
- 该实现不捕获请求头、Cookie、API Key 或上游凭据，也不会为了“绝不截断”移除管理员配置的内存/存储安全上限。任何达到上限的正文或帧目录都会向管理员明确报告。
