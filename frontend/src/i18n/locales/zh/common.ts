export default {
  common: {
    loading: '加载中...',
    refreshing: '正在刷新…',
    submitting: '提交中...',
    justNow: '刚刚',
    peakRateTooltip: '高峰倍率：{window}',
    peakRateImageNote: '；token 计费的图片 token 同样适用，图片按次计费不受高峰影响',
    save: '保存',
    saved: '保存成功',
    savedViews: '已保存视图',
    savedViewsPlaceholder: '选择已保存的筛选视图',
    saveView: '保存当前视图',
    savedViewName: '视图名称',
    savedViewNamePlaceholder: '例如：活跃 Anthropic 账号',
    savedViewDescription: '保存当前搜索、筛选、排序和分页设置，之后可快速恢复。',
    deleteSavedView: '删除已保存视图',
    deleted: '删除成功',
    cancel: '取消',
    delete: '删除',
    edit: '编辑',
    create: '创建',
    update: '更新',
    confirm: '确认',
    reset: '重置',
    search: '搜索',
    filter: '筛选',
    export: '导出',
    import: '导入',
    actions: '操作',
    status: '状态',
    name: '名称',
    email: '邮箱',
    password: '密码',
    submit: '提交',
    back: '返回',
    next: '下一步',
    yes: '是',
    no: '否',
    all: '全部',
    none: '无',
    selectAll: '全选',
    selectRow: '选择 {name}',
    noData: '暂无数据',
    expand: '展开',
    collapse: '收起',
    success: '成功',
    error: '错误',
    critical: '严重',
    warning: '警告',
    info: '提示',
    active: '启用',
    inactive: '禁用',
    enable: '启用',
    disable: '停用',
    more: '更多',
    menu: '菜单',
    toggleMenu: '切换菜单',
    userMenu: '用户菜单',
    pageNotFound: '页面不存在',
    close: '关闭',
    clear: '清除',
    enabled: '已启用',
    disabled: '已禁用',
	    total: '总计',
	    balance: '余额',
	    availableBalance: '可用余额',
	    frozenBalance: '冻结金额',
	    totalBalance: '总余额',
	    available: '可用',
    copiedToClipboard: '已复制到剪贴板',
    copied: '已复制',
    copyFailed: '复制失败',
    verifying: '验证中...',
    processing: '处理中...',
    contactSupport: '联系客服',
    add: '添加',
    invalidEmail: '请输入有效的邮箱地址',
    optional: '可选',
    selectOption: '请选择',
    searchPlaceholder: '搜索...',
    noOptionsFound: '无匹配选项',
    noGroupsAvailable: '无可用分组',
    unknownError: '发生未知错误',
    saving: '保存中...',
    selectedCount: '（已选 {count} 个）',
    refresh: '刷新',
    retry: '重试',
    autoRefresh: {
      title: '自动刷新',
      enable: '启用自动刷新',
      countdown: '自动刷新: {seconds}s',
      seconds: '{n} 秒',
    },
    view: '查看',
    settings: '设置',
    chooseFile: '选择文件',
    copy: '复制',
    notAvailable: '不可用',
    now: '现在',
    today: '今天',
    tomorrow: '明天',
    unknown: '未知',
    minutes: '分钟',
    time: {
      never: '从未',
      justNow: '刚刚',
      minutesAgo: '{n}分钟前',
      hoursAgo: '{n}小时前',
      daysAgo: '{n}天前',
      countdown: {
        daysHours: '{d}d {h}h',
        hoursMinutes: '{h}h {m}m',
        minutes: '{m}m',
        withSuffix: '{time} 后解除'
      }
    }
  },

  adminCompliance: {
    title: '部署与运营合规确认',
    blockingNotice: '继续使用控制台前，须完成部署与运营合规确认。',
    riskNotice: '本确认用于以清晰、显著、可留痕的方式提示自部署实例的合规义务与运营风险。',
    version: '协议版本',
    openDocument: '在 GitHub 查看协议文件',
    documentSource: '协议正文来自本项目仓库中的 Markdown 文件。修改协议内容时必须同步递增协议版本；已确认的旧版本将失效，控制台使用者须重新确认。',
    inputLabel: '请逐字输入以下确认短语',
    inputPlaceholder: '输入确认短语以继续',
    inputMismatch: '确认短语不匹配，请逐字输入提示内容。',
    legalNote: '本确认用于明确自部署实例与开源项目、著作权人、贡献者及维护者之间的非关联关系和责任边界；部署、运营或控制相关实例的主体应独立承担其适用义务。',
    logout: '退出登录',
    accept: '确认并继续',
    accepted: '合规确认已记录',
    acceptFailed: '提交确认失败'
  },

  legal: {
    loadFailed: '文档加载失败',
    retryLater: '请稍后刷新页面重试。',
    notFound: '文档不存在',
    notFoundDescription: '当前条款文档不存在或已被管理员移除。',
    updatedAt: '更新日期：{date}',
    empty: '暂无正文内容',
    loginAgreement: '登录条款',
    adminCompliance: '部署与运营合规承诺',
    loginAgreementPrompt: {
      checkboxPrefix: '我已阅读并同意',
      documentSeparator: '、',
      noticeTitle: '继续登录前需要先同意最新条款。',
      noticeDescription: '未同意前，账号密码输入和快捷登录会保持禁用。',
      viewTerms: '查看条款',
      dialogTitle: '条款更新通知',
      dialogDescription: '我们的服务条款已于 {date} 更新。在继续使用服务之前，请仔细阅读并同意以下条款。',
      recently: '近期',
      relatedDocuments: '相关文档',
      reject: '拒绝',
      accept: '同意并继续',
      loginRejectedWarning: '未同意最新条款前，无法输入账号密码或使用快捷登录。',
      loginRequiredWarning: '请先阅读并同意最新条款后再登录。',
      registerRejectedWarning: '未同意最新条款前，无法注册或使用快捷登录。',
      registerRequiredWarning: '请先阅读并同意最新条款后再注册。'
    }
  },

  // Navigation
  nav: {
    dashboard: '仪表盘',
    announcements: '公告',
    apiKeys: 'API 密钥',
    batchImage: '批量生图',
    usage: '使用记录',
    redeem: '兑换',
    affiliate: '邀请返利',
    affiliateManagement: '邀请返利',
    affiliateInviteRecords: '邀请记录',
    affiliateRebateRecords: '返利记录',
    affiliateTransferRecords: '提取记录',
    profile: '个人资料',
    users: '用户管理',
    groups: '分组管理',
    channels: '渠道管理',
    availableChannels: '可用渠道',
    subscriptions: '订阅管理',
    virtualCurrency: '资产',
    accountAllocations: '账号分配',
    accountDirectory: '账号目录',
    citySimulation: '城市模拟',
    cityVisualPacks: '城市视觉包',
    cityAgentRuntime: '城市 Agent 运行时',
    virtualCurrencies: '虚拟货币',
    virtualCurrencyIntegrations: '货币接入',
    accounts: '账号管理',
    proxies: 'IP管理',
    redeemCodes: '兑换码',
    ops: '运维监控',
    promoCodes: '优惠码',
    settings: '系统设置',
    myAccount: '我的账户',
    lightMode: '浅色模式',
    darkMode: '深色模式',
    collapse: '收起',
    expand: '展开',
    logout: '退出登录',
    github: 'GitHub',
    mySubscriptions: '我的订阅',
    buySubscription: '充值/订阅',
    docs: '文档',
    myOrders: '我的订单',
    orderManagement: '订单管理',
    paymentDashboard: '支付概览',
    paymentConfig: '支付配置',
    paymentPlans: '订阅套餐',
    channelManagement: '渠道管理',
    channelPricing: '渠道定价',
    channelMonitor: '渠道监控',
    channelStatus: '渠道状态',
    riskControl: '风控中心',
    securityAudit: '安全审计',
    contentModeration: '内容审核',
    promptAudit: '提示词审计',
    invocationArchive: '调用归档',
    auditLogs: '操作日志',
  },

  accountAllocations: {
    title: '账号目录',
    description: '查看公开分组账号，以及您有权访问的专属分组账号。',
    readOnlyTitle: '只读账号目录',
    readOnlyDescription: '公开分组对所有用户可见；具备分组权限或有效订阅后即可查看对应专属分组账号，单独分配给您的账号也会在此显示。此页面只能查看，不能操作上游账号。',
    privacyNotice: '邮箱形式的账号名称会在服务端脱敏；凭据、代理、IP、模型和健康错误不会显示。',
    emptyTitle: '暂无可查看账号',
    emptyDescription: '当前没有公开分组账号，也没有您有权查看的专属分组账号。',
    noFilterResultsTitle: '没有匹配的账号',
    noFilterResultsDescription: '调整筛选条件或清除筛选后再试。',
    account: '账号',
    group: '分组',
    source: '来源',
    sourcePublic: '公开分组',
    sourceDedicated: '专属分组',
    groupType: '分组类型',
    platformType: '平台 / 类型',
    usage: '用量',
    masked: '已脱敏',
    capacity: '并发容量',
    concurrentRequests: '并发请求',
    requestUsage: '请求数',
    tokenUsage: 'Token 用量',
    lastActivity: '最近使用',
    upstreamQuota: '上游配额',
    cachedQuotaSnapshot: '缓存快照',
    usageDetailGrantedNoSnapshot: '已授权，当前暂无缓存配额快照',
    leaseAccountCost: '本次分配账号计费',
    leaseUserCost: '本次分配用户计费',
    requests: '请求',
    tokens: 'Token',
    assignedAt: '分配时间',
    coolingUntilLabel: '预计恢复时间',
    coolingUntil: '当前冷却，预计于 {time} 恢复。',
    unavailableHint: '当前暂不可用。',
    readyHint: '当前可调度。',
    loadFailed: '加载账号目录失败',
    autoRefreshEvery: '每 {seconds} 秒自动刷新',
    searchLabel: '搜索账号目录',
    searchPlaceholder: '搜索账号、分组、平台或类型…',
    viewMode: '显示方式',
    viewModes: {
      list: '行显示',
      grid: '网格显示'
    },
    filters: {
      allSources: '全部来源',
      allGroups: '全部分组',
      allPlatforms: '全部平台',
      allStatuses: '全部状态'
    },
    showing: '显示 {count} / {total} 个账号',
    resetFilters: '清除筛选',
    tableAriaLabel: '只读账号目录',
    viewDetails: '查看详情',
    showMore: '再显示 {count} 个',
    detailTitle: '账号详情',
    detailPrivacyNotice: '该详情仅用于查看可用性与用量。专属分配可显示缓存配额；管理员授权还可显示授权分组内的窗口聚合，但不能主动查询或重置。账号控制、凭据、代理、IP、模型列表和健康错误始终隐藏。',
    unknownPlatform: '未知平台',
    groupTypes: {
      standard: '标准分组',
      subscription: '订阅分组'
    },
    usageScopes: {
      rolling24h: '该分组近 24 小时',
      personalLease: '本人本次分配期'
    },
    usageWindows: {
      rolling24h: '24h',
      personalLease: '本次'
    },
    usageDetailAccess: {
      assignment: '专属分配',
      group: '分组授权',
      direct: '管理员授权'
    },
    summary: {
      publicGroups: '公开分组',
      dedicatedGroups: '专属分组',
      visibleAccounts: '可见账号',
      readyAccounts: '当前可用'
    },
    status: {
      ready: '可用',
      cooling: '冷却中',
      unavailable: '暂不可用'
    }
  },

  virtualCurrency: {
    title: '我的资产',
    description: '查看可用虚拟货币、适用分组和完整流水。',
    refresh: '刷新资产',
    available: '可用余额',
    reserved: '冻结余额',
    integerUnits: '整数单位',
    precision: '{count} 位小数',
    otherAssets: '其他资产',
    assetCount: '{count} 种自定义资产',
    groups: '可用分组',
    noGroups: '暂无可用分组',
    ledger: '资产流水',
    viewLedger: '查看流水',
    noWallets: '当前没有可用的虚拟货币。',
    noLedger: '暂无流水记录。',
    createdAt: '时间',
    type: '类型',
    amount: '变动',
    balanceAfter: '余额',
    source: '来源',
    reason: '说明',
    loading: '正在加载资产...',
    loadFailed: '加载资产失败',
    ledgerFailed: '加载流水失败',
    earnHint: '资产可由兑换码、活动、任务、邀请或游戏服务发放。'
  },

  citySpatial: {
    title: '城市空间模拟',
    description: '查看真实 Overmap 与 Chunk 空间事实，逐层检查城市地形及其变更。',
    classic: 'CLASSIC 空间终端',
    loading: '正在装载城市空间',
    loadingDescription: '校验规则集、Overmap 与可见 Chunk…',
    viewportAria: '城市字符地图，可使用方向键移动、滚轮缩放、方括号切换高度层。',
    empty: {
      eyebrow: '尚未建立世界',
      title: '创建第一座可验证城市',
      description: '城市世界会绑定固定规则集、生成器和世界种子。地图只展示服务端已经生成并封账的空间事实。',
      waitingForWorld: '管理员尚未将你加入可游玩的城市。加入后，你可以在这里创建角色、探索并推进自己的角色进程。'
    },
    controls: {
      world: '城市世界', selectWorld: '选择城市世界', viewMode: '视图模式',
      overmap: '区域总图', localMap: '局部地图', zoomOut: '缩小', zoomIn: '放大',
      refresh: '刷新真实空间状态', export: '导出当前 Chunk 文本', depth: '高度层',
      layerUp: '上移一层', layerDown: '下移一层',
      dragHint: '拖动地图或使用方向键平移', modeHint: '切换总图/局部图',
      depthHint: '切换 Z 层', helpHint: '快捷键'
    },
    legend: { generated: '已生成', structure: '建筑与分区', selected: '当前选择', unloaded: '未装载' },
    mapHeader: {
      overmap: 'OVERMAP / 区域总图', local: 'LOCAL / 局部字符地图',
      overmapSubtitle: '{count} 个服务端区域 Tile · {buildings} 栋建筑',
      localSubtitle: '缓存 {count} 个真实 Chunk · 当前层 {buildings} 栋建筑'
    },
    live: {
      overmap: '区域总图，当前 Chunk 坐标 {coordinate}',
      local: '局部地图，当前坐标 {coordinate}，共 {layers} 个内容层'
    },
    inspector: {
      eyebrow: '空间事实', title: '检查器', chunk: 'Chunk 坐标', district: '行政区',
      terrain: '基础地形', variant: '生成变体', roadMask: '道路连接', riverMask: '河流连接',
      state: '投影状态', generated: '已生成', notGenerated: '尚未生成', tileHash: 'Tile 哈希',
      worldCoordinate: '世界坐标 X / Y / Z', localCoordinate: 'Chunk 内坐标', revision: '修订号',
      generatedTick: '生成 Tick', cellStack: 'Cell 内容栈',
      movementCost: '移动成本：{value}', payloadHash: 'Payload 哈希', none: '无',
      parcels: '地块数', buildings: '建筑数', zoning: '用途分区',
      floorSummary: '{count} 层 · 容量 {capacity}', landStack: '土地与建筑事实',
      parcel: '地块', building: '建筑', area: '法定面积', version: '事实版本',
      floors: '高度层', floorArea: '总建筑面积', occupancy: '占用 / 容量', quality: '质量',
      allocations: '{count} 条住宅分配', actorsHere: '当前位置角色', focusActor: '定位',
      unavailableTitle: '此 Cell 尚无可用事实',
      unavailableDescription: '目标 Chunk 尚未生成、尚未装载，或当前 Z 层没有空间数据。',
      emptyTitle: '选择地图位置', emptyDescription: '点击区域 Tile 或局部 Cell 查看服务端返回的完整空间信息。'
    },
    development: {
      eyebrow: '确定性开发事实', title: '开发项目',
      description: '项目必须经过申请、审批与开工，并由城市 Tick 自动推进施工。',
      unavailable: '当前世界尚未启用 F7.4 开发项目协议。',
      tileProjects: '区域开发项目', adjustments: '完工调整',
      addedFloors: '增加楼层', addedCapacity: '增加容量', qualityGain: '质量提升',
      all: '全部', active: '进行中', newProject: '提交开发申请',
      noProjects: '当前筛选条件下没有开发项目。', selectedBuilding: '目标建筑',
      selectBuildingHint: '先在局部地图中选择一栋建筑，再提交项目。',
      projectName: '项目名称', projectNamePlaceholder: '可选的项目名称',
      projectType: '项目类型', developer: '开发者', targetFloors: '目标总楼层',
      targetQuality: '目标质量', targetQualityHint: '以千分比输入，例如 1150 表示 115%。',
      resources: '确定性需求预估', basicMaterial: '基础材料', capitalGoods: '资本品',
      labor: '劳动力', duration: '工期', ticks: '{count} Tick',
      serverAuthoritative: '最终成本、容量和工期由服务器绑定政策重新计算。',
      progress: '施工进度', submittedAt: '提交 T{tick}', completionAt: '预计 T{tick}',
      reviewNote: '审批说明', cancellationReason: '取消原因',
      actionPrompt: '填写本次操作的事实说明。',
      status: {
        submitted: '待审批', approved: '已批准', rejected: '已驳回',
        under_construction: '施工中', completed: '已完工', cancelled: '已取消'
      },
      type: { vertical_expansion: '垂直扩建', renovation: '整修改造' },
      action: {
        submit: '提交申请', approve: '批准', reject: '驳回', start: '开工',
        cancel: '取消项目', confirm: '确认操作', processing: '正在封账…'
      },
      commandSuccess: '开发项目事实已通过城市 Tick 封账。',
      commandFailed: '开发项目操作失败'
    },
    enterprise: {
      eyebrow: '企业空间事实', title: '企业场所与迁址',
      description: '查看企业在建筑中的真实占用，并通过城市 Tick 执行开设、扩缩、关闭和跨区迁址。',
      unavailable: '当前世界尚未启用 F7.5 企业空间协议。',
      noSites: '当前筛选条件下没有企业场所。', primary: '主要场所',
      version: '事实版本 v{version}',
      filter: {
        firm: '企业', district: '行政区', type: '场所类型', status: '状态',
        allFirms: '全部企业', allDistricts: '全部行政区', allTypes: '全部类型', allStatuses: '全部状态'
      },
      columns: { site: '场所', firm: '企业主体', location: '建筑与空间池', capacity: '占用单元', status: '状态' },
      siteType: { headquarters: '总部', office: '办公室', production: '生产场所', warehouse: '仓库', retail: '零售点' },
      status: { active: '运营中', closed: '已关闭' },
      factType: { opened: '开设', resized: '调整占用', closed: '关闭', relocated: '跨区迁址' },
      action: { open: '开设场所', resize: '调整占用', close: '关闭场所', relocate: '跨区迁址', confirm: '确认并提交' },
      form: {
        firm: '企业主体', siteType: '场所类型', pool: '目标空间池', name: '场所名称',
        occupiedUnits: '目标占用单元', policyMinimum: '留空使用政策最低值', reason: '事实说明',
        currentDistrict: '当前主要运营区', targetDistrict: '目标行政区',
        headquartersPool: '新总部空间池', productionPool: '新生产空间池',
        serverAuthoritative: '最低占用、用途兼容性、容量和必要场所约束由服务器绑定政策重新计算。',
        relocationWarning: '迁址会原子关闭旧主要场所、建立新总部与生产场所，并守恒转移全部非零企业库存。'
      },
      facts: { title: '不可变企业空间事实', count: '{count} 条', multiSite: '多个主要场所', empty: '尚无企业空间变更。' },
      inspector: { tileSites: '区域企业场所', poolCapacity: '空间池 {occupied} / {effective}' },
      commandSuccess: '企业空间事实已通过城市 Tick 封账。',
      commandFailed: '企业空间操作失败'
    },
    services: {
      eyebrow: '城市服务事实', title: '公共设施与城市服务',
      description: '以真实建筑、服务容量、主体需求、连接和逐 Tick 结算构成可追溯的城市服务网络。',
      loading: '正在读取公共服务投影…', queryFailed: '公共服务数据查询失败',
      commandSuccess: '公共服务事实已通过城市 Tick 封账。', commandFailed: '公共服务操作失败',
      unsupported: { title: '当前世界尚未启用公共服务底座', description: '暂停世界并升级至 {version} 后，才可创建设施、需求和连接。升级不会生成虚假数据。' },
      tabs: { catalog: '目录', facilities: '设施', demands: '需求', connections: '连接', networks: '物理网络', settlements: '结算' },
      metrics: {
        facilities: '设施', operational: '{count} 个运行中', capacity: '有效容量', capacityLines: '{count} 条有效容量',
        demand: '每 Tick 需求', activeDemands: '{count} 条有效需求', delivered: '最近交付', shortage: '最近短缺',
        quality: '加权质量', requested: '请求 {value}', requestedLabel: '请求量', noTick: '尚无结算'
      },
      catalog: {
        services: '服务定义', servicesDescription: '单位、方向和语义均由版本化目录约束。',
        facilityTypes: '设施类型', facilityTypesDescription: '设施类型限定可提供服务与最低建筑面积。',
        category: '类别', unit: '计量单位', immutable: '版本内不可变', minimumArea: '最低建筑面积 {value} m²'
      },
      filters: { service: '服务', status: '状态', district: '行政区', facility: '设施', demand: '需求', apply: '应用筛选' },
      columns: {
        facility: '设施', location: '空间锚点', status: '状态', capacities: '服务容量', demand: '需求', subject: '服务主体',
        request: '请求与优先级', latestSettlement: '最近结算', connection: '连接', route: '供给路径', flowLimit: '流量上限', policy: '损耗与偏好'
      },
      actions: {
        operations: '公共服务操作', registerFacility: '注册设施', capacity: '配置容量', transition: '转换状态',
        configureDemand: '配置需求', configureConnection: '配置连接', confirm: '确认并封账', processing: '正在封账…'
      },
      form: {
        code: '稳定编码', name: '设施名称', facilityType: '设施类型', building: '所在建筑', ownerEntity: '所有者经济主体（可选）',
        reliability: '可靠度（千分比）', facility: '设施', service: '服务', installedCapacity: '装机容量', availability: '可用率（千分比）',
        effectiveCapacity: '计算可用容量', targetStatus: '目标状态', currentStatus: '当前状态', subjectKind: '主体类型', subject: '真实主体',
        requestedUnits: '每 Tick 请求量', priority: '优先级（0–1000）', status: '状态', version: '当前版本', demand: '需求',
        flowLimit: '每 Tick 最大流量', loss: '损耗（千分比）', preference: '连接偏好（0–1000）',
        registerNote: '新设施固定以离线状态创建；配置容量并显式转换为运行中后才会形成供给。',
        casNote: '提交携带当前版本，服务端拒绝并发覆盖并重新计算有效容量。',
        statusNote: '状态转换只影响本次事实发布后的结算，既有结算保持不可变。',
        demandNote: '需求必须绑定当前世界中的真实主体；主体与服务在创建后不可更换。',
        connectionNote: '连接只能关联同一服务的设施容量和需求；流量、损耗与偏好参与确定性分配。'
      },
      status: { offline: '离线', operational: '运行中', degraded: '降级', retired: '已退役', active: '有效', suspended: '暂停' },
      subjectKind: { district: '行政区', building: '建筑', household: '家庭群体', enterprise: '企业', actor: '角色' },
      empty: { facilities: '当前筛选条件下没有设施。', demands: '当前筛选条件下没有服务需求。', connections: '当前筛选条件下没有服务连接。', settlements: '当前筛选条件下没有结算事实。' },
      pagination: { loaded: '已载入 {count} 条', more: '继续载入' },
      reliability: '可靠度 {value}', priority: '优先级 {value}', preference: '偏好 {value}', loss: '损耗 {value}',
      shortage: '短缺 {value}', lossUnits: '损耗量 {value}', allocations: '{count} 条分配明细', noCapacity: '尚未配置服务容量', noSettlement: '尚无结算',
      network: {
        title: '城市服务物理网络', loading: '正在读取物理网络投影…', queryFailed: '物理网络数据查询失败',
        unsupported: {
          title: '当前世界尚未启用物理网络协议',
          description: '暂停世界并升级至 {version} 后，才可管理节点、边、容量、故障隔离和逐段流量事实。'
        },
        tabs: { topology: '拓扑与容量', flows: '路径流量', diagnostics: '网络诊断', facts: '网络事实' },
        metrics: {
          networks: '运行网络', nodes: '{count} 个有效节点', edges: '有效边',
          edgeExceptions: '隔离 {isolated} · 故障 {failed}', capacity: '可用边容量', installed: '装机 {value}',
          dispatched: '最近发出', received: '网络到达', lossUnits: '网络损耗 {value}', deliveryRatio: '网络送达率'
        },
        filters: { network: '物理网络', allNetworks: '全部网络', edgeStatus: '边状态', phase: '事实阶段' },
        actions: { network: '配置网络', node: '配置节点', edge: '配置边', transition: '转换状态' },
        topology: {
          graphLabel: '物理网络节点、边、容量与运行状态拓扑图', selectedNode: '已选节点',
          selectedEdge: '已选边', edgeInventory: '边资产清单'
        },
        diagnostics: {
          summary: '{components} 个连通分量 · {bottlenecks} 条瓶颈边',
          selectNetwork: '请选择一个物理网络以读取实时拓扑诊断。',
          loading: '正在计算连通性、孤岛、边利用率与路由探针…',
          routeProbe: '容量约束路由探针',
          routeProbeHint: '只读复用正式确定性路由器，不写入流量、事实或剩余容量。',
          source: '源节点', sink: '汇节点', probeUnits: '探测量', runProbe: '执行探针',
          components: '弱连通分量', activeAssets: '{nodes} 个有效节点 · {edges} 条有效边',
          isolatedNodes: '孤立节点', serviceIslands: '{count} 个服务孤岛',
          bottlenecks: '瓶颈边', saturated: '{count} 条已饱和', latestFlow: '利用率样本 Tick',
          componentAssets: '{nodes} 个节点 · {edges} 条边', island: '供需孤岛', edgeLoad: '最近边利用率',
          truncated: '另有 {count} 条低优先级边未展示。', routeResult: '路由探针结果',
          reasons: {
            reachable: '可达', no_capacity_path: '无剩余容量路径',
            source_not_active: '源节点未生效', sink_not_active: '汇节点未生效'
          }
        },
        columns: {
          role: '节点角色', status: '状态', binding: '业务绑定', version: '事实版本', route: '路径',
          available: '可用 / 装机', loss: '损耗', condition: '健康度', paths: '路径 / 分段',
          tick: 'Tick', fact: '事实', subject: '主体', source: '来源'
        },
        empty: { networks: '当前筛选条件下没有物理网络。', nodes: '当前网络尚无可显示节点。', flows: '当前筛选条件下没有路径流量事实。', facts: '当前筛选条件下没有网络事实。' },
        pagination: {
          topology: '{networks} 个网络 · {nodes} 个节点 · {edges} 条边',
          networks: '继续载入网络', nodes: '继续载入节点', edges: '继续载入边'
        },
        operations: { network: '物理网络配置', node: '网络节点配置', edge: '网络边配置', edgeStatus: '网络边状态转换' },
        form: {
          topologyRevision: '拓扑修订', networkNote: '每个网络绑定一种城市服务；修改会推进拓扑修订和乐观锁版本。',
          capacityBinding: '设施服务容量', demandBinding: '服务需求', anchorCoordinates: '绑定权威 XYZ 坐标',
          nodeNote: '供给节点必须绑定设施容量，需求节点必须绑定服务需求；中继、存储和网关节点不绑定二者。',
          fromNode: '起点节点', toNode: '终点节点', direction: '方向', installedCapacity: '装机边容量', baseCost: '基础路由成本',
          edgeNote: '可用容量由装机容量与可用率整除计算；双向边的两个方向共享同一份 Tick 容量。',
          transitionNote: '隔离、故障和恢复均形成不可变事实；恢复为有效状态前服务端会重新校验网络与端点。'
        },
        direction: { directed: '单向', bidirectional: '双向共享容量' },
        role: { supply: '供给', demand: '需求', junction: '中继', storage: '存储', gateway: '网关' },
        status: { active: '有效', suspended: '暂停', retired: '已退役', isolated: '已隔离', failed: '故障', offline: '离线' },
        phase: { command: '命令', pre_network: '网络前置', settlement: '结算' },
        factType: {
          'network.configured': '网络已配置', 'node.configured': '节点已配置',
          'edge.configured': '边已配置', 'edge.state_changed': '边状态已转换',
          'network.topology_synchronized': '拓扑已同步', 'network.flow_settled': '网络流量已结算'
        },
        facts: { automatic: '自动阶段' }
      },
      serviceNames: {
        electric_power: '电力', potable_water: '饮用水', wastewater: '污水处理', solid_waste: '固体废弃物',
        education: '教育', healthcare: '医疗', fire_response: '消防响应', police_response: '治安响应'
      },
      facilityTypeNames: {
        service_hub: '综合服务枢纽', power_plant: '发电设施', water_works: '供水设施', wastewater_plant: '污水处理设施',
        waste_processing: '废弃物处理设施', school: '学校', hospital: '医院', fire_station: '消防站', police_station: '警务站'
      }
    },
    runtime: {
      eyebrow: '开放世界运行时', title: '角色、成长与规则',
      description: '角色状态、行为、职业迁移、规则案件与处罚均由服务端事实链确定。',
      unavailable: '当前世界尚未启用开放世界运行时。', actorSelection: '选择角色', unknown: '未知定义',
      processing: '正在封账…', perform: '执行', transition: '更换职业',
      attributes: '角色属性', activities: '可执行行为', roles: '身份与职业', statuses: '状态与处罚',
      cases: '规则案件', rules: '公开规则', facts: '角色事实记录',
      serverAuthoritative: '数值由服务端计算', activitiesHint: '行为会改变属性并触发世界规则',
      rolesHint: '满足属性、经历和身份条件后方可迁移',
      statusSummary: '{stacks} 层 · 强度 {intensity}', noStatuses: '当前角色没有状态或处罚记录。',
      noCases: '当前角色没有规则案件。', commandSuccess: '角色事实已通过城市 Tick 封账。',
      commandQueued: '角色命令已进入世界队列，将由世界调度 Tick 统一封账。',
      commandFailed: '开放世界操作失败',
      lifecycle: {
        queued: '世界调度命令已进入队列。',
        success: '世界调度状态已通过城市 Tick 更新。',
        failed: '世界调度操作失败'
      },
      counters: { actors: '角色', facts: '事实', cases: '案件' },
      members: {
        title: '世界成员与职责', count: '{count} 名生效成员', identity: '邮箱或用户名',
        identityPlaceholder: '精确输入邮箱或用户名', role: '世界职责', add: '添加成员', remove: '移出世界',
        addSuccess: '世界成员已添加', updateSuccess: '世界成员已更新', mutationFailed: '世界成员操作失败',
        roles: { owner: '所有者', planner: '规划者', treasurer: '财务员', trader: '交易员', viewer: '观察者' }
      },
      receipts: {
        title: '命令处理回执', awaitingTick: '等待调度 Tick',
        status: { pending: '等待处理', applied: '已封账', rejected: '已拒绝' }
      },
      creation: {
        eyebrow: '角色初始化', title: '选择基础角色并进入世界', archetype: '基础角色',
        capacity: '角色槽位 {current} / {maximum}', name: '角色名称',
        namePlaceholder: '输入角色名称', confirm: '创建角色'
      },
      roleState: {
        active: '当前生效', eligible: '已满足迁移条件', requirements: '尚未满足条件',
        cooldown: '还需等待 {count} Tick'
      },
      location: {
        title: '位置与移动', showOnMap: '在地图中定位', jurisdiction: '管辖区', space: '空间', anchor: '空间锚点',
        noAnchor: '无锚点', movement: '八方向移动控制', levelUp: '上一层', levelDown: '下一层',
        movementHint: '平面移动限制为相邻 Cell；跨层必须经过服务端登记的双向入口。',
        unavailable: '该角色尚无权威位置事实。',
        direction: {
          northWest: '向西北移动', north: '向北移动', northEast: '向东北移动', west: '向西移动',
          east: '向东移动', southWest: '向西南移动', south: '向南移动', southEast: '向东南移动'
        }
      },
      navigation: {
        title: '确定性路径规划', preview: '规划到选中 Cell', planning: '正在规划…', clear: '清除路径',
        noTarget: '尚未选择目标', hint: '在局部地图选择目标 Cell，再由服务端根据地形、建筑入口、楼梯和角色占位计算可复现路径。',
        status: '判定', steps: '步数', cost: '总代价', expanded: '展开节点', route: '规划路径',
        reachable: '可到达', arrived: '已在目标位置', unreachable: '不可到达', moveNext: '执行下一步',
        failed: '路径规划失败',
        reasons: {
          outside_world: '目标超出世界边界', chunk_not_generated: '必经 Chunk 尚未生成',
          terrain_blocked: '目标地形不可通行', furniture_blocked: '目标被设施阻挡',
          building_wall: '建筑边界不可穿越', void: '该 Z 层没有可用空间',
          actor_occupied: '目标 Cell 已被其他角色占用', portal_required: '必须通过登记入口或楼梯',
          portal_closed: '必经入口当前关闭', portal_locked: '必经入口当前锁定',
          portal_access_denied: '当前角色不满足必经入口的访问策略',
          corner_blocked: '禁止从障碍夹角斜向穿越', search_limit: '路径超出搜索步数或节点上限',
          unreachable: '当前空间拓扑中不存在可达路径'
        }
      },
      navigationIntent: {
        title: '持续移动意图', loading: '正在同步移动调度状态…', reservationCount: '当前 Tick {count} 条移动预留',
        empty: '当前角色没有移动意图。可将地图选中 Cell 设为目标，交由后续世界 Tick 持续推进。',
        destination: '目的地 XYZ', nextAttempt: '下次尝试', priority: '优先级', blockedAttempts: '连续阻塞',
        maxSteps: '搜索上限', onBlocked: '阻塞策略', budget: '行动预算', useSelectedCell: '使用地图选中 Cell',
        create: '建立移动意图', replace: '替换移动意图', cancel: '取消移动意图', stepCost: '本步成本 {cost}',
        statuses: { active: '执行中', blocked: '阻塞等待', arrived: '已到达', cancelled: '已取消', failed: '执行失败' },
        onBlockedOptions: { retry: '确定性退避后重试', cancel: '首次阻塞即取消' },
        reasons: {
          budget_insufficient: '行动预算不足，等待后续 Tick 归集', target_reached: '角色已到达目的地',
          user_cancelled: '移动意图已由用户取消', target_invalid: '目的地不再有效',
          reservation_target_conflict: '目标 Cell 已被本 Tick 的其他 Actor 预留',
          reservation_edge_conflict: '移动边已被本 Tick 的其他 Actor 预留',
          outside_world: '目标超出世界边界', chunk_not_generated: '必经 Chunk 尚未生成',
          terrain_blocked: '目标地形不可通行', furniture_blocked: '目标被设施阻挡',
          building_wall: '建筑边界不可穿越', void: '该 Z 层没有可用空间',
          actor_occupied: '目标 Cell 已被其他角色占用', portal_required: '必须通过登记入口或楼梯',
          portal_closed: '必经入口当前关闭', portal_locked: '必经入口当前锁定',
          portal_access_denied: '角色不满足必经入口访问策略', corner_blocked: '禁止从障碍夹角斜向穿越',
          search_limit: '路径超出搜索步数或节点上限', unreachable: '当前空间拓扑中不存在可达路径'
        }
      },
      portals: {
        title: '入口状态与访问控制', loading: '正在读取入口投影…', count: '{count} 个入口', empty: '当前世界没有生效入口。',
        direction: '方向', version: '版本', policy: '访问策略', actorAccess: '当前角色', fixedOpen: '楼梯保持开放',
        outOfRange: '角色需到达入口相邻 Cell', endpointRequired: '角色需站在入口端点', traverse: '通过入口', editPolicy: '编辑策略', policyEditor: '入口访问策略编辑器',
        policyHint: '策略由服务端校验并作为事实封账；保存会替换该入口的完整访问条件。', portal: '目标入口',
        policyMode: '策略条件', noDefinitions: '没有可用定义', minimumStacks: '最低状态层数', factType: '事实类型',
        windowTicks: '统计窗口（Tick）', scaledThreshold: '属性阈值', integerThreshold: '整数阈值', savePolicy: '保存访问策略',
        requirementFailure: '{condition}：当前 {actual}，要求 {required}',
        states: { open: '开放', closed: '关闭', locked: '锁定' },
        actions: { open: '开启', close: '关闭', lock: '上锁', unlock: '解锁' },
        access: { allowed: '允许通过', denied: '策略拒绝', closed: '入口关闭', locked: '入口锁定', notEvaluated: '未选择角色' },
        policyModes: {
          unchanged: '保留当前复杂策略', public: '公开通行', roleActive: '要求生效职业', roleInactive: '要求职业未生效',
          attributeGte: '属性不低于阈值', attributeLte: '属性不高于阈值', experienceGte: '属性经验不低于阈值',
          statusPresent: '要求存在状态', statusAbsent: '要求不存在状态', factCountGte: '时间窗内事实次数', worldTickGte: '世界 Tick 门槛'
        },
        definitionKinds: { attribute: '角色属性', role: '身份或职业', status: '角色状态' },
        requirements: { and: '且', or: '或', not: '非', public: '公开通行', active: '生效', inactive: '未生效', absent: '不存在' }
      },
      control: {
        title: '角色控制委托', memberUserID: '城市成员', memberUserIDPlaceholder: '选择生效成员',
        memberSearchPlaceholder: '搜索用户名、邮箱或 ID', noEligibleMembers: '没有可委托的生效成员',
        capabilities: '委托能力', actorCommand: '执行角色命令', manageControl: '管理控制权', grant: '授予',
        owner: '角色所有者', delegate: '受托成员', readOnly: '当前成员只能查看，未获得角色命令或控制权管理能力。',
        noDelegations: '暂无可见的生效控制权。',
        revokeCapability: '撤销用户 {user} 的“{capability}”能力'
      }
    },
    landUse: { residential: '住宅', commercial: '商业', industrial: '工业' },
    portalType: { entrance: '入口', stair: '楼梯' },
    context: {
      chunk: 'Chunk {x}, {y}, {z}', inspect: '进入局部地图', generate: '生成真实 Chunk',
      generating: '正在生成…', generatedSuccess: 'Chunk 已通过城市 Tick 生成并装载',
      generateFailed: 'Chunk 生成失败'
    },
    changes: {
      eyebrow: '不可变事实流', title: '空间变更记录', count: '{count} 条',
      empty: '当前世界还没有空间变更。'
    },
    createWorld: {
      action: '新建城市', title: '创建城市世界', name: '城市名称',
      namePlaceholder: '例如：港湾实验城', timezone: 'IANA 时区',
      timezoneHint: '用于城市日历边界，例如 Asia/Shanghai。', creating: '正在创建…',
      mode: '世界模式', standardMode: '标准开放世界', realtimeMode: '共享实时像素世界',
      standardHint: '使用现有开放世界运行时；适合兼容性测试和既有城市功能。',
      realtimeHint: '仅当服务端 NTP/NTS 时钟已受信任并启用时可创建；时钟、引擎和资料版本均由服务端固定。',
      style: '世界生成风格', stylePlaceholder: '选择风格',
      styleHint: '创建后会冻结生成器、国家/地区风格与字符规则集，不复用旧版 F7 固定地图。',
      styleLoadFailed: '无法读取开放世界风格目录',
      confirm: '创建并生成开放世界', success: '城市世界已创建', failed: '创建城市世界失败'
    },
    realtime: {
      world: '城市世界', sharedWorld: '共享实时世界', worldTime: '世界时间',
      zoomOut: '缩小像素地图', zoomIn: '放大像素地图', refresh: '同步世界投影', returnToSpawn: '返回出生地',
      mapTitle: 'PIXEL WORLD / 共享局部地图', mapSubtitle: '{chunks} 个缓存 Chunk · {actors} 个可见角色 · {cursor}',
      viewportAria: '共享像素世界地图。可拖动、使用方向键平移、滚轮缩放并点击查看地图事实。',
      loading: '正在装载共享像素世界', loadingDescription: '正在校验服务端空间哈希并按视野读取 Chunk…',
      loadingChunks: '正在读取相邻 Chunk', loadFailed: '无法读取共享实时世界',
      rendererDisabledTitle: '共享像素渲染已关闭',
      rendererDisabledDescription: '管理员暂时关闭了该实时世界的像素内容面。世界数据与时间线未删除，重新开启后可继续访问。',
      dragHint: '拖动地图浏览同一座城市；视野缓存不会重置世界状态。', panHint: '平移', zoomHint: '缩放', spawnHint: '返回出生地',
      clock: { live: '服务端实时投影', committed: '已提交时间线' },
      character: {
        eyebrow: '我的角色', title: '共享世界身份', loading: '正在读取你的角色状态…',
        label: '公开角色名', labelPlaceholder: '例如：春日 花子', createHint: '只显示在共享地图中；不会公开账号、提示词或 Agent 配置。',
        create: '创建角色', creating: '正在创建角色…', createFailed: '创建角色失败', loadFailed: '无法读取你的角色状态',
        runtimeUnavailable: '此历史世界尚未绑定角色运行时；可继续查看，但不能在其中创建角色。',
        selectTarget: '点击相邻的可见 Cell 作为移动目标。', selectedTarget: '目标：{x} / {y} / {z}',
        move: '移动到目标', moving: '正在提交移动…', moveFailed: '移动角色失败', adjacentOnly: '只能移动到同一层相邻的一格。',
        portals: {
          title: '通道转换', serverAuthoritative: '静态路线由服务端校验',
          completed: '已完成：{direction}', failed: '无法通过该通道',
          directions: { enter: '进入建筑', exit: '离开建筑', ascend: '上楼', descend: '下楼' }
        },
        interior: {
          title: '室内导航', floor: '第 {floor} 层', cellDescription: '{kind}：{x} / {y} / {z}',
          kinds: { wall: '墙体', window: '窗户', floor: '地面', door: '门', furniture: '陈设' }
        },
        life: {
          title: '生活状态', localOnly: '城市内本地状态，不兑换平台余额',
          energy: '精力', satiety: '饱腹', morale: '士气', standing: '市民评价',
          cityCredit: '城市信用', rations: '口粮'
        },
        progression: {
          title: '角色发展', private: '仅你可见 · 服务端封存', archetypeSelect: '起始原型', archetype: '原型',
          experience: '累计经验', currentRoles: '当前角色', available: '可转任', active: '当前任职',
          change: '转任', changing: '正在转任…', changeFailed: '转任失败', none: '无',
          changed: '角色已从「{from}」转为「{to}」',
          unavailable: {
            civic_standing: '市民评价尚未满足', experience: '累计经验尚未满足',
            role: '前置角色尚未满足', attribute: '属性尚未满足', requirements: '尚未满足条件'
          },
          requirements: {
            none: '无额外条件', standing: '市民评价 {value}', experience: '经验 {value}',
            attribute: '{attribute} {value}', role: '前置：{role}'
          },
          attributes: { communication: '沟通', coordination: '协调', discipline: '自律', reasoning: '推理', vitality: '体能' },
          archetypes: { residentGeneralist: '城市通才', residentSocial: '社群居民', residentTechnical: '技术居民' },
          roles: { resident: '居民', civicAide: '市政助理', maintenanceWorker: '维护工', communitySteward: '社区管事' }
        },
        activities: {
          title: '当前行动', serverAuthoritative: '可用性由服务端封账状态决定',
          available: '可执行', unavailable: '暂不可执行', cooldown: '冷却 {seconds}s',
          locationRequired: '需要道路或人行道', inventoryRequired: '缺少口粮', needsRequired: '精力或饱腹不足，先休息或进食',
          roleRequired: '需要对应角色', progressionRequired: '角色发展状态不可用',
          completed: '已完成「{activity}」', completedWithExperience: '已完成「{activity}」：{experience}', penalized: '「{activity}」触发处罚，城市信用 {amount}',
          failed: '执行行动失败',
          labels: {
            restShort: '短暂休息', civicShift: '市政轮班', consumeRation: '食用口粮',
            civicCleanup: '公共清洁', conductDisruption: '扰乱秩序', publicServiceStudy: '公共服务研习',
            civicService: '市政服务', maintenanceShift: '维护轮班'
          }
        },
        history: { title: '我的行动记录', private: '仅你可见', completed: '完成', penalized: '处罚' },
        agent: {
          title: '角色 Agent', description: '人格和控制模式只属于当前角色；行动仍由服务端世界规则校验并在后续时间线中结算。',
          mode: '控制模式', personalityRevision: '人格修订', queue: '队列状态',
          values: '核心价值', valuesPlaceholder: '例如：社群、好奇心（逗号或换行分隔）',
          boundaries: '硬边界', boundariesPlaceholder: '例如：避免伤害、尊重同意（逗号或换行分隔）',
          background: '背景', backgroundPlaceholder: '可选：角色已知的背景事实',
          notes: '补充说明', notesPlaceholder: '可选：仅作为人格数据，不是可执行指令',
          ownerPrivate: '人格仅返回给角色拥有者；不会出现在共享地图、其他成员投影或 Agent 观察载荷中。',
          save: '保存并应用', saving: '正在封存配置…', saved: '角色 Agent 已切换至「{mode}」。', saveFailed: '保存角色 Agent 配置失败',
          personalityRequired: '启用自主模式前，至少需要填写一项核心价值。',
          personalityInvalid: '人格字段无效：核心价值至少一项，且每类列表最多八项、不能重复。',
          manualControlUnavailable: '当前为「{mode}」模式，移动、通道、行动和职业变更由角色 Agent 管理。',
          queueIdle: '空闲', queueDecision: '等待决策', queueIntent: '等待执行',
          modes: { manual: '手动', assisted: '辅助', autonomous: '自主', suspended: '已暂停' }
        }
      },
      inspector: {
        eyebrow: '成员安全空间事实', title: '地图检查器', coordinate: '世界坐标', chunk: 'Chunk', terrain: '基础地形',
        building: '建筑', actor: '可见角色', floors: '层', layers: '内容层', noLayers: '该 Cell 没有额外的可见内容层。',
        emptyTitle: '选择地图位置', emptyDescription: '点击已读取的像素 Cell 查看服务端返回的地形、结构与可见语义层。',
        viewerScope: '查看权限', staticHash: '静态投影哈希'
      }
    },
    openWorld: {
      world: '开放世界', profile: '世界风格', generator: '生成器 {version}',
      refresh: '重新读取服务端世界事实', returnToSpawn: '返回出生地', nearestInterior: '定位最近的已物化室内',
      materializeSector: '按当前视野生成并封存相邻分区', materializeFailed: '分区生成命令未成功应用',
      lifecycle: {
        title: '世界调度', tick: 'T{tick} · {cadence}', speed: '游玩速度',
        start: '启动世界', pause: '暂停世界',
        pausedHint: '世界已暂停。持续移动与自动行动会在管理员启动世界后继续推进。',
        status: { running: '运行中', paused: '已暂停' }
      },
	  verifyCurrentRegion: '验证当前 Region 的已封存地图事实', regionVerified: '当前 Region 的地图事实已验证',
	  worldVerified: '完整世界状态与规范哈希已验证', verificationSummary: '{sectors} 个分区 · {chunks} 个 Chunk',
	  verificationFailed: '地图事实验证返回了不匹配的范围',
      mapTitle: 'OPEN WORLD / 局部字符地图',
      mapSubtitle: '{chunks} 个已物化 Chunk · {buildings} 栋带室内事实的建筑',
      viewportAria: '开放世界字符地图，可使用方向键移动和滚轮缩放。',
      dragHint: '拖动地图或使用方向键移动视野', panHint: '平移', zoomHint: '缩放',
      loadFailed: '开放世界数据读取失败',
      hovered: '悬停坐标 {x} / {y} / {z}',
      inspector: {
        eyebrow: '已物化世界事实', title: 'Cell 检查器', coordinate: '世界坐标',
        chunk: 'Chunk', terrain: '基础地形', stack: '层叠内容', building: '建筑与室内',
        floors: '{count} 层', interiorReady: '已封存 {count} 个可见室内楼层事实。',
        interiorUnavailable: '此建筑没有可读取的地面室内事实。', openInterior: '查看室内字符图', focusInterior: '定位室内入口',
        emptyTitle: '选择地图 Cell', emptyDescription: '点击地图查看服务端封存的地形、墙体、门和建筑事实。',
        seed: '世界种子', hash: '创世哈希'
      },
      interior: {
        title: '建筑室内 / 字符地图', floor: '楼层 {floor} · Z {z}', layout: '布局版本',
        openFloor: '查看第 {floor} 层', connections: '可用连接',
        cells: '{count} 个封存 Cell', loading: '正在读取服务端封存的室内事实…',
        loadFailed: '室内事实读取失败', verificationFailed: '室内事实哈希与建筑清单不一致',
        viewportAria: '建筑室内字符地图。可拖动、使用方向键移动，并用滚轮缩放。',
        dragHint: '仅显示服务端封存的房间、门、楼梯与家具',
        serverFacts: '服务端封存事实', selectCell: '点击字符查看该 Cell 的完整事实堆栈。'
      }
    },
    help: {
      action: '查看快捷键', title: 'CLASSIC 地图操作', pan: '按 Cell 平移地图', zoom: '缩放字符尺寸',
      depth: '切换当前 Z 层', surface: '返回地表 Z=0', mode: '切换区域总图与局部地图',
      inspect: '进入选择的区域或检查 Cell', back: '返回区域总图', openHelp: '打开本帮助',
      note: '焦点位于输入框时不会触发地图快捷键；所有快捷操作均有对应按钮。'
    },
    export: { unavailable: '请先选择一个已装载的 Chunk', success: 'Chunk 文本已导出' }
  },

  worldRuntime: {
    actorTypes: { character: '角色' },
    attributes: {
      vitality: '体能', reasoning: '推理', coordination: '协调', communication: '沟通', discipline: '自律'
    },
    archetypes: {
      residentGeneralist: '城市通才', residentGeneralistDescription: '属性均衡、拥有居民身份与学徒职业。',
      urbanApprentice: '城市学徒', urbanApprenticeDescription: '擅长推理与自律，适合沿技术职业路径成长。',
      fieldSurvivor: '野外生存者', fieldSurvivorDescription: '体能与协调突出，适合高强度行动与探索。'
    },
    roles: {
      resident: '居民', residentDescription: '城市共同体中的基础身份。',
      apprentice: '学徒', apprenticeDescription: '通过学习与实践积累职业能力。',
      technician: '技术员', technicianDescription: '满足推理、协调和学徒经历后的技术职业。'
    },
    activities: {
      technicalStudy: '技术研习', technicalStudyDescription: '提升推理、自律与相关经验。',
      physicalTraining: '体能训练', physicalTrainingDescription: '提升体能、协调与相关经验。',
      communityService: '社区服务', communityServiceDescription: '通过公共服务提升自律与沟通。',
      disruptiveNoise: '制造扰民噪音', disruptiveNoiseDescription: '降低自律并触发公共秩序规则。'
    },
    statuses: {
      civicWarning: '市民警告', civicWarningDescription: '公共秩序违规产生的限时警告。',
      communityServiceOrder: '社区服务令', communityServiceOrderDescription: '重复违规后产生的社区服务处罚。'
    },
    rules: {
      publicOrderNoise: '公共秩序：噪音', publicOrderNoiseDescription: '对扰民噪音按时间窗内累计次数分级处理。'
    },
    facts: {
      actor_created: '角色创建', actor_activity_performed: '行为完成',
      actor_role_transitioned: '职业迁移', actor_status_expired: '状态到期',
      rule_consequence_applied: '规则后果执行', actor_location_moved: '角色位置移动',
      actor_control_granted: '角色控制权授予', actor_control_revoked: '角色控制权撤销',
      portal_state_changed: '入口状态变更', portal_access_changed: '入口访问策略变更'
    }
  },

  // Auth
  auth: {
    welcomeBack: '欢迎回来',
    signInToAccount: '登录您的账户以继续',
    signIn: '登录',
    signingIn: '登录中...',
    createAccount: '创建账户',
    signUpToStart: '注册以开始使用 {siteName}',
    signUp: '注册',
    processing: '处理中...',
    continue: '继续',
    rememberMe: '记住我',
    dontHaveAccount: '还没有账户？',
    alreadyHaveAccount: '已有账户？',
    registrationDisabled: '注册功能暂时关闭，请联系管理员。',
    emailLabel: '邮箱',
    emailPlaceholder: '请输入邮箱',
    passwordLabel: '密码',
    passwordPlaceholder: '请输入密码',
    createPasswordPlaceholder: '创建一个安全的密码',
    passwordHint: '至少 6 个字符',
    emailRequired: '请输入邮箱',
    invalidEmail: '请输入有效的邮箱地址',
    passwordRequired: '请输入密码',
    passwordMinLength: '密码至少需要 6 个字符',
    loginFailed: '登录失败，请检查您的凭据后重试。',
    errors: {
      USER_NOT_ACTIVE: '账号已被禁用',
    },
    registrationFailed: '注册失败，请重试。',
    emailSuffixNotAllowed: '该邮箱域名不在允许注册范围内。',
    emailSuffixNotAllowedWithAllowed: '该邮箱域名不被允许。可用域名：{suffixes}',
    emailSuffixAllowedMore: '等 {count} 项',
    loginSuccess: '登录成功！欢迎回来。',
    accountCreatedSuccess: '账户创建成功！欢迎使用 {siteName}。',
    reloginRequired: '会话已过期，请重新登录。',
    turnstileExpired: '验证已过期，请重试',
    turnstileFailed: '验证失败，请重试',
    completeVerification: '请完成验证',
    verifyYourEmail: '验证您的邮箱',
    sessionExpired: '会话已过期',
    sessionExpiredDesc: '请返回注册页面重新开始。',
    verificationCode: '验证码',
    verificationCodeHint: '请输入发送到您邮箱的6位验证码',
    sendingCode: '发送中...',
    sendCode: '发送验证码',
    clickToResend: '点击重新发送验证码',
    resendCode: '重新发送验证码',
    sendCodeDesc: '我们将发送验证码到',
    codeSentSuccess: '验证码已发送！请查收您的邮箱。',
    verifying: '验证中...',
    verifyAndCreate: '验证并创建账户',
    resendCountdown: '{countdown}秒后可重新发送',
    backToRegistration: '返回注册',
    sendCodeFailed: '发送验证码失败，请重试。',
    verifyFailed: '验证失败，请重试。',
    codeRequired: '请输入验证码',
    invalidCode: '请输入有效的6位验证码',
    promoCodeLabel: '优惠码',
    promoCodePlaceholder: '输入优惠码（可选）',
    promoCodeValid: '有效！注册后将获得 ${amount} 赠送余额',
    promoCodeInvalid: '无效的优惠码',
    promoCodeNotFound: '优惠码不存在',
    promoCodeExpired: '此优惠码已过期',
    promoCodeDisabled: '此优惠码已被禁用',
    promoCodeMaxUsed: '此优惠码已达到使用上限',
    promoCodeAlreadyUsed: '您已使用过此优惠码',
    promoCodeValidating: '优惠码正在验证中，请稍候',
    promoCodeInvalidCannotRegister: '优惠码无效，请检查后重试或清空优惠码',
    invitationCodeLabel: '邀请码',
    invitationCodePlaceholder: '请输入邀请码',
    invitationCodeRequired: '请输入邀请码',
    invitationCodeValid: '邀请码有效',
    invitationCodeInvalid: '邀请码无效或已被使用',
    invitationCodeValidating: '正在验证邀请码...',
    invitationCodeInvalidCannotRegister: '邀请码无效，请检查后重试',
    oauthOrContinue: '或使用其他继续',
    linuxdo: {
      signIn: '使用 Linux.do 登录',
      orContinue: '或使用邮箱密码继续',
      callbackTitle: '正在完成登录',
      callbackProcessing: '正在验证登录信息，请稍候...',
      callbackHint: '如果页面未自动跳转，请返回登录页重试。',
      callbackMissingToken: '登录信息缺失，请返回重试。',
      backToLogin: '返回登录',
      invitationRequired: '该 Linux.do 账号尚未注册，站点已开启邀请码注册，请输入邀请码以完成注册。',
      invalidPendingToken: '注册凭证已失效，请重新使用 Linux.do 登录。',
      completeRegistration: '完成注册',
      completing: '正在完成注册...',
      completeRegistrationFailed: '注册失败，请检查邀请码后重试。'
    },
    dingtalk: {
      signIn: '钉钉登录',
      callbackTitle: '正在完成钉钉登录',
      callbackProcessing: '正在验证钉钉登录信息，请稍候...',
      callbackHint: '如果页面未自动跳转，请返回登录页重试。',
      callbackMissingToken: '登录信息缺失，请返回重试。',
      backToLogin: '返回登录',
      invitationRequired: '该钉钉账号尚未注册，站点已开启邀请码注册，请输入邀请码以完成注册。',
      invalidPendingToken: '注册凭证已失效，请重新使用钉钉登录。',
      completeRegistration: '完成注册',
      completing: '正在完成注册...',
      completeRegistrationFailed: '注册失败，请检查邀请码后重试。',
      createAccountTitle: '创建钉钉账户',
      registrationDisabledRedirectToBind: '当前已禁止注册新账户，请使用已有账户邮箱和密码绑定钉钉登录',
      error: {
        title: '钉钉登录失败',
        csrf: '登录会话已过期，请重新扫码登录',
        corp_rejected: '您的钉钉账号不属于本企业，请联系管理员',
        dingtalk_not_enabled: '钉钉登录暂未启用',
        upstream_error: '钉钉服务暂时不可用，请稍后重试',
        missing_browser_session: '浏览器会话丢失，请重新登录',
        missing_params: '请求参数不完整',
        invalid_state: '登录状态异常',
        provider_error: '钉钉授权失败',
        session_error: '会话创建失败，请重试',
        retry: '重新登录'
      }
    },
    emailOAuth: {
      signIn: '使用 {providerName} 登录'
    },
    oidc: {
      signIn: '使用 {providerName} 登录',
      callbackTitle: '正在完成 {providerName} 登录',
      callbackProcessing: '正在验证 {providerName} 登录信息，请稍候...',
      callbackHint: '如果页面未自动跳转，请返回登录页重试。',
      callbackMissingToken: '登录信息缺失，请返回重试。',
      backToLogin: '返回登录',
      invitationRequired: '该 {providerName} 账号尚未注册，站点已开启邀请码注册，请输入邀请码以完成注册。',
      invalidPendingToken: '注册凭证已失效，请重新登录。',
      completeRegistration: '完成注册',
      completing: '正在完成注册...',
      completeRegistrationFailed: '注册失败，请检查邀请码后重试。'
    },
    oauthFlow: {
      profileDetailsTitle: '使用 {providerName} 资料',
      profileDetailsDescription: '选择是否将 {providerName} 的昵称或头像应用到当前账户。',
      useDisplayName: '使用昵称',
      useAvatar: '使用头像',
      avatarAlt: '{providerName} 头像',
      reviewProfileBeforeContinue: '请先确认 {providerName} 资料后再继续。',
      chooseHowToContinue: '选择后续操作',
      chooseAccountActionHint: '请选择绑定已有账户，或创建一个新账户。',
      suggestedEmail: '建议邮箱：{email}',
      bindExistingAccount: '绑定已有账户',
      createNewAccount: '创建新账户',
      createAccountHint: '请输入邮箱地址以创建账户并继续。',
      bindLoginHint: '登录一个已有账户以绑定此次 {providerName} 登录。',
      signInThenBindDescription: '请先登录已有账户，再将此次 {providerName} 登录绑定到该账户。',
      bindSignInToExistingAccount: '将此次 {providerName} 登录绑定到已有账户。',
      bindCurrentAccountTitle: '绑定当前账户',
      bindCurrentAccountDescription: '将此次 {providerName} 登录绑定到当前浏览器已登录的账户。',
      bindCurrentAccount: '绑定当前账户',
      logInAndBind: '登录并绑定',
      useDifferentEmail: '使用其他邮箱',
      backToOptions: '返回选项',
      yourAccount: '当前账户',
      totpHint: '请输入 {account} 的 6 位验证码，以完成此次 {providerName} 登录绑定。',
      verifyAndContinue: '验证并继续',
      wechatAvailabilityUnknown: '暂时无法确认微信登录可用性，请刷新后重试。',
      wechatSystemBrowserOnly: '当前微信登录流程仅支持在系统浏览器中继续。',
      wechatBrowserOnly: '当前微信登录流程仅支持在微信内置浏览器中继续。',
      wechatNotConfigured: '微信登录尚未配置。'
    },
    linuxdoCallbackPageTitle: 'LinuxDo 登录回调',
    dingtalkCallbackPageTitle: '钉钉登录回调',
    dingtalkProviderName: '钉钉',
    oidcCallbackPageTitle: 'OIDC 登录回调',
    oauthCallbackPageTitle: 'OAuth 回调',
    wechatProviderName: '微信',
    wechatCallbackPageTitle: '微信登录回调',
    wechatPaymentCallbackPageTitle: '微信支付回调',
    wechatPayment: {
      callbackTitle: '正在恢复微信支付',
      callbackProcessing: '正在恢复微信支付...',
      backToPayment: '返回支付页',
      callbackMissingResumeToken: '微信支付回调缺少恢复令牌。'
    },
    oauth: {
      callbackTitle: 'OAuth 回调',
      callbackHint: '按需将授权码和状态值复制回后台授权流程。',
      invalidCallbackTitle: '无效的登录回调',
      invalidCallbackHint: '当前页面缺少有效的授权结果，请返回登录页重新发起快捷登录。',
      code: '授权码',
      state: '状态',
      fullUrl: '完整URL'
    },
    // 忘记密码
    forgotPassword: '忘记密码？',
    forgotPasswordTitle: '重置密码',
    forgotPasswordHint: '输入您的邮箱地址，我们将向您发送密码重置链接。',
    sendResetLink: '发送重置链接',
    sendingResetLink: '发送中...',
    sendResetLinkFailed: '发送重置链接失败，请重试。',
    resetEmailSent: '重置链接已发送',
    resetEmailSentHint:
      '如果该邮箱已注册，您将很快收到密码重置链接。请检查您的收件箱和垃圾邮件文件夹。',
    backToLogin: '返回登录',
    rememberedPassword: '想起密码了？',
    // 重置密码
    resetPasswordTitle: '设置新密码',
    resetPasswordHint: '请在下方输入您的新密码。',
    newPassword: '新密码',
    newPasswordPlaceholder: '输入新密码',
    confirmPassword: '确认密码',
    confirmPasswordPlaceholder: '再次输入新密码',
    confirmPasswordRequired: '请确认您的密码',
    passwordsDoNotMatch: '两次输入的密码不一致',
    resetPassword: '重置密码',
    resettingPassword: '重置中...',
    resetPasswordFailed: '重置密码失败，请重试。',
    passwordResetSuccess: '密码重置成功',
    passwordResetSuccessHint: '您的密码已重置。现在可以使用新密码登录。',
    invalidResetLink: '无效的重置链接',
    invalidResetLinkHint: '此密码重置链接无效或已过期。请重新请求一个新链接。',
    requestNewResetLink: '请求新的重置链接',
    invalidOrExpiredToken: '密码重置链接无效或已过期。请重新请求一个新链接。'
  },

  // Step-up（敏感操作二次验证）
  stepUp: {
    title: '需要二次验证',
    hint: '请输入身份验证器应用中的 6 位验证码以继续此敏感操作。',
    verifyFailed: '验证失败，请重试',
    notEnabled: '此操作需要开启二次验证，请先在个人资料中启用 TOTP。',
    adminApiKeyForbidden: '管理 API Key 无法执行此操作，请使用已通过二次验证的管理员会话。'
  },

  // Dashboard
}
