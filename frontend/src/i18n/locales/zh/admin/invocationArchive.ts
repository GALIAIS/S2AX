export default {
  invocationArchive: {
    title: '调用归档',
    description: '对指定网关调用加密保存有限期副本，用于管理员审查。默认关闭；载荷明文仅可在二次验证后直接查看。',
    tabs: { records: '归档记录', config: '归档策略', runtime: '运行态' },
    modes: { off: '关闭', request_only: '仅请求', full: '请求与响应' },
    scopes: { user: '用户', group: '分组', api_key: 'API Key' },
    outcomes: { completed: '已完成', client_error: '客户端错误', server_error: '服务端错误', websocket_error: 'WebSocket 错误' },
    capture: {
      captured: '已加密捕获', empty: '空载荷', not_read: '未读取', omitted: '按策略省略', encryption_failed: '加密失败', truncated: '已截断',
    },
    records: {
      title: '归档记录',
      description: '只显示归档元数据。查看任何请求或响应正文均需单独填写理由并通过二次验证。',
      search: '搜索', searchPlaceholder: '用户、API Key、分组、模型或请求 ID', mode: '归档模式', outcome: '结果', userId: '用户 ID', groupId: '分组 ID', apiKeyId: 'API Key ID', from: '开始时间', to: '结束时间',
      time: '时间', identity: '调用主体', route: '路由 / 模型', capture: '捕获状态', result: '结果', request: '请求', response: '响应',
      empty: '没有符合条件的归档记录。', selectAll: '选择本页全部记录', selectRecord: '选择归档记录 {id}', deleteSelected: '删除已选（{count}）', deleteTitle: '永久删除调用归档？', deleteMessage: '将永久删除 {count} 条已选归档记录；此操作无法撤销，访问审计证据仍会保留。',
    },
    config: {
      title: '归档策略',
      description: '规则优先级为 API Key、用户、分组、默认策略。规则设为“关闭”可对较宽泛策略显式排除。',
      version: '配置版本 v{version}', privacyNotice: '归档只在命中启用策略时发生；不会保存请求头、Cookie、API Key 或上游凭据。载荷会在写入数据库前经 AES-GCM 加密，并按保留期自动删除。',
      defaultMode: '默认归档模式', retentionDays: '保留天数', requestLimit: '请求上限（MiB）', responseLimit: '响应上限（MiB）', directView: '允许管理员直接查看加密载荷', directViewHint: '关闭时只能查看元数据。开启后，每次明文查看仍要求二次验证并写入访问日志。',
      unsaved: '存在未保存修改', synced: '配置已同步',
    },
    rules: {
      title: '作用域规则', description: '通过主体选择器创建精确规则；保存时服务端会验证主体仍然存在且未删除。', count: '{count} 条规则', scope: '作用域', subjectSearch: '搜索主体', subjectSearchPlaceholder: '输入名称、邮箱或 ID', subject: '主体', selectSubject: '选择主体', mode: '模式', empty: '暂无覆盖规则，所有调用使用默认策略。',
    },
    runtime: {
      title: '归档运行态', description: '归档写入在网关响应后异步执行；队列饱和会丢弃归档工作，不会阻塞或影响用户调用。', status: '服务状态', running: '运行中', stopped: '未运行', version: '配置 v{version}', queue: '异步队列', queueHint: '当前深度 / 容量', persisted: '已持久化', acceptedDropped: '接收 {accepted} · 丢弃 {dropped}', purge: '已清理过期记录', failures: '持久化失败 {count}', configError: '配置加载错误', persistError: '最近持久化错误',
    },
    detail: {
      title: '归档记录 #{id}', createdAt: '创建时间', expiresAt: '过期时间', outcome: '执行结果', identity: '调用主体', group: '分组', route: '路由', model: '模型', requestId: '请求 ID', client: '客户端',
      payloads: '加密载荷', payloadsHint: '正文不会随元数据加载。明文仅保留在本对话框内存中，关闭后立即清除。', directViewDisabled: '当前策略未启用直接查看。请在“归档策略”中启用后保存；启用本身也需要二次验证。', revealReason: '查看理由', revealReasonPlaceholder: '至少输入 3 个字符，说明本次审查原因', reveal: '验证并查看载荷', revealHint: '本次查看会记录管理员、理由、时间和客户端信息。二进制载荷按 Base64 原样显示。',
      accesses: '查看访问日志', accessTime: '时间', admin: '管理员', accessOutcome: '结果', reason: '理由', noAccesses: '尚无直接查看访问记录。',
    },
    messages: { saved: '调用归档策略已保存。', deleted: '已删除 {count} 条归档记录。' },
    errors: {
      loadConfig: '加载调用归档策略失败。', loadRuntime: '加载调用归档运行态失败。', loadRecords: '加载归档记录失败。', loadSubjects: '搜索归档规则主体失败。', loadDetail: '加载归档记录详情失败。', saveConfig: '保存调用归档策略失败。', reveal: '查看归档载荷失败。', delete: '删除归档记录失败。',
      invocation_archive_rule_duplicate: '该主体已有同一作用域规则。', invocation_archive_config_conflict: '配置已被其他管理员更新，请刷新后重试。', invocation_archive_rule_subject_not_found: '所选主体不存在或已被删除。', invocation_archive_direct_view_disabled: '管理员尚未启用调用归档直接查看。', invocation_archive_payload_expired: '归档载荷已过期。', invocation_archive_payload_unavailable: '归档载荷不可用。', invocation_archive_reveal_reason_invalid: '查看理由需为 3 至 256 个字符。',
    },
  },
}
