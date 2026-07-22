# 账号添加、导入与导出功能完善设计（Account Intake & Transfer V2）

**状态：** 设计完成，待按阶段实施
**范围：** 管理员账号添加、批量导入、备份导出、恢复、审计与结果展示
**不在本期范围：** 上游账号购买、自动注册、绕过上游验证、普通用户直接管理上游凭据

**现有实现依据：** `backend/internal/handler/admin/account_handler.go`、`account_data.go`、`account_codex_import.go`、`frontend/src/components/account/CreateAccountModal.vue`、`frontend/src/components/admin/account/ImportDataModal.vue`、`frontend/src/views/admin/AccountsView.vue`。

---

## 1. 背景与目标

账号是当前系统的调度、计费、风控和账号分配的基础资源。现有实现已经覆盖多平台创建、备份包导入、Codex 会话导入、Agent Identity 导入和管理员导出，但入口、数据语义和结果反馈仍然分散：

- 创建账号的配置能力很强，但新手无法判断哪些是凭据、调度、路由和高级参数；
- 通用备份导入是一次性直接写入，缺少预检、冲突策略、可恢复结果和持久化历史；
- Codex 导入具备较完善的身份匹配，却与通用导入的分组、代理、默认绑定和结果展示逻辑不一致；
- 导出包适合当前实例备份，但格式版本没有实际写出，且不能完整表达分组、状态、影子账号关系和所有账号类型；
- 导入大量账号时，即使列表加载已优化，操作本身仍会在一个 HTTP 请求中做解析、匹配、写入和部分上游调用，难以给出可靠进度、取消、重试或回滚。

本设计把这三类操作统一为“账号接入与迁移”能力，但不引入通用插件框架或第二套账号模型。现有 `Account`、`Proxy`、`Group`、`CreateAccountInput`、`UpdateAccountInput` 继续是唯一的业务真相；V2 只增加一个可审计的计划/任务层和版本化交换格式。

### 1.1 目标

1. 任何创建、导入或恢复动作在提交前都能看到**会创建、更新、跳过、失败的具体对象**。
2. 备份包可稳定地跨版本、跨实例迁移，并完整表达账号、代理、分组、调度配置和关系。
3. 上游凭据始终按敏感数据处理：不在普通日志、浏览器持久化、任务错误详情或审计事件中出现明文。
4. 小批量操作保持直接、快速；大批量操作转为可恢复任务，不依赖浏览器长连接或 120 秒请求超时。
5. 账号列表、导入结果和导出确认界面能够按“身份 / 路由 / 调度 / 用量 / 风险”理解对象，而不是堆叠原始字段。
6. 保持现有 API 向后兼容；旧 `sub2api-data` / `sub2api-bundle` V1 包仍可导入。

### 1.2 非目标

- 不将不同上游的凭据协议强行抽象为一个可执行插件系统；每一种来源仍使用显式的解析器和验证器。
- 不在导入过程中默认访问上游、刷新令牌或发送测试请求；这些操作可能昂贵、触发风控或改变上游状态。
- 不提供“无提示全量覆盖”模式；覆盖必须在预检结果中按字段显示并二次确认。
- 不把导出包作为在线同步协议；跨实例实时同步另行设计。

---

## 2. 现状审查

### 2.1 已有能力

| 能力 | 当前实现 | 保留原则 |
| --- | --- | --- |
| 单账号创建 | `POST /admin/accounts`；创建弹窗支持 OAuth、Setup Token、API Key、上游转发、Bedrock、Service Account 等，并能配置代理、分组、并发、优先级、倍率、到期策略、模型映射等 | 保持平台特有表单和后端校验，不重写为通用 JSON 表单 |
| 批量创建 | `POST /admin/accounts/batch`，逐项返回成功或失败 | 作为“已通过预检计划”的提交执行器复用 |
| 通用备份导出 | `GET /admin/accounts/data`；可导出选中账号或当前筛选结果，可选代理；已受二次验证保护 | 保持管理员显式备份的全凭据语义 |
| 通用备份导入 | `POST /admin/accounts/data`；支持 V1 与旧 bundle，代理按连接指纹复用或创建 | V1 继续可读，新增 V2 迁移器 |
| Codex 导入 | `POST /admin/accounts/import/codex-session`；支持 OAuth auth.json、会话 JSON、单行 token、Agent Identity auth.json、ChatGPT session JSON | 保留身份键去重、更新已有账号、凭据合并与过期规则 |
| Agent Identity | ChatGPT session JSON 会完成上游 Agent Identity 注册，最终只保存身份密钥；已有 auth.json 可直接导入 | 继续禁止把单独 `at-...` 当作 Agent Identity 输入 |
| 导入幂等 | 通用与 Codex 导入都接入管理员写操作幂等包装 | 将幂等键显式呈现为任务/计划标识 |

### 2.2 当前格式与链路的明确缺口

| 编号 | 问题 | 影响 | V2 决策 |
| --- | --- | --- | --- |
| G-01 | `DataPayload` 定义了 `type` 与 `version`，但当前导出实际未填充二者 | 未来无法可靠区分格式与迁移路径 | V2 导出必须写出 `format`、`version`、`manifest_id`、`checksum` |
| G-02 | V1 `DataAccount` 未表达状态、分组、`load_factor`、账号稳定来源 ID、影子父子关系等 | 恢复后调度行为与原实例不一致 | V2 显式表达关系与调度字段 |
| G-03 | 通用导入只支持 OAuth / Setup Token / API Key / Upstream；Bedrock、Service Account 无法通过备份包恢复 | 全量灾备不完整 | V2 使用账号类型对应的严格 schema，覆盖全部已支持类型 |
| G-04 | 通用导入默认逐项创建，缺少账号级冲突预览和可选更新策略 | 重复账号、误覆盖风险 | 所有导入先生成计划，再选择 create / merge / replace / skip |
| G-05 | 多个文件在浏览器直接拼接后提交，结果只显示汇总及错误文本 | 无法定位来源文件、无法审查合并冲突 | 预检保留 `source_file`、`source_index`、候选身份与差异 |
| G-06 | 当前代理备用关系通过名称恢复 | 重名或改名会导致错误关联 | V2 使用稳定 `proxy_ref`，名称仅作显示 |
| G-07 | Spark 影子账号被导出时排除 | 备份恢复不完整且只有数量提示 | V2 可表达父账号引用与影子配置；无法安全导出时必须逐项说明原因 |
| G-08 | Codex 导入有详细 item 结果，但弹窗主要显示警告/错误 | 创建、更新、跳过的具体结果不可审阅 | 统一结果表、可筛选、可下载 JSON/CSV 报告 |
| G-09 | 通用导入默认跳过默认分组绑定，Codex 导入默认遵循默认分组绑定 | 同一“导入”操作出现不可见的路由差异 | UI 明示“分组策略”，后端统一默认值与来源覆盖规则 |
| G-10 | 导入在单一 HTTP 请求内完成；Agent Identity 还可能同步调用上游 | 大批量超时、断线后状态未知、不能取消 | 引入持久化导入任务；小批量可复用同一计划执行路径同步等待 |
| G-11 | 全凭据导出虽有 step-up，但没有格式化用途说明、校验和、加密封装或下载失效机制 | 凭据备份外泄与误用风险 | 采用导出档案级别、短期下载、摘要与可选加密 |

### 2.3 设计约束

1. 账号凭据永远以服务器端加密存储为前提；任务表、审计日志、应用日志只记录指纹和掩码。
2. 导入不可使用一个跨所有对象的大事务：Agent Identity、代理健康检查或其他上游动作不可回滚。每个候选对象独立原子提交，任务层记录可补偿信息。
3. 代理、分组和账号三者均可能被其他对象引用。任何 replace 或 rollback 都必须检查最后修改版本，不能静默覆盖导入后的人工修改。
4. 默认行为必须保守：默认不覆盖、默认不触发上游测试、默认不保存浏览器草稿中的秘密。

---

## 3. 总体产品结构

账号页面保留现有列表作为日常工作面，在工具菜单中调整为四个清晰入口：

1. **添加账号**：为单个或同配置批量账号的向导。
2. **导入账号**：解析外部输入、预检、提交和结果查看。
3. **导出 / 备份**：按范围与安全等级产生版本化档案。
4. **传输历史**：查看导入、导出、回滚、失败重试与审计。

“添加”不等于“导入”：添加是管理员主动配置一个账号；导入是从外部材料恢复或转换。两者最终都会产生相同的账号配置确认页和相同的审计事件。

```mermaid
flowchart LR
    A[添加账号 / 导入材料] --> B[解析与规范化]
    B --> C[预检计划]
    C --> D{管理员确认}
    D -->|取消| X[不写入]
    D -->|提交| E[账号传输任务]
    E --> F[逐项原子写入]
    F --> G[结果、审计、可回滚范围]
    G --> H[账号列表增量刷新]
```

### 3.1 统一对象分类

所有添加、导入预览和导出确认都使用下列分类，避免把数十个字段平铺：

| 分类 | 内容 | 典型字段 |
| --- | --- | --- |
| 身份与来源 | 名称、平台、账号类型、来源、外部身份、备注 | `name`、`platform`、`type`、邮箱、组织、导入来源 |
| 凭据与安全 | 凭据存在状态、认证模式、更新时间、脱敏指纹 | 不展示 token / refresh token / 私钥明文 |
| 路由与关联 | 分组、代理、备用代理、父/影子关系、独占分配约束 | `group_ids`、`proxy_ref`、`parent_ref` |
| 调度与容量 | 状态、可调度、并发、优先级、负载因子、倍率、到期策略 | `status`、`concurrency`、`priority`、`load_factor` |
| 能力与模型 | 模型白名单/映射、端点能力、特殊上游模式 | `model_mapping`、端点开关 |
| 运行与风险 | 最近错误、401/429、隐私状态、最近探测、健康度 | 运行时数据仅作预览，不作为备份真相 |

---

## 4. V2 交换格式

### 4.1 格式原则

- V2 是唯一的**完整备份格式**；V1 只保留导入兼容。
- 所有关联均使用档案内稳定引用，而不是数据库自增 ID、名称或明文代理指纹。
- 区分“配置真相”和“运行时观测”：状态、策略和关联可恢复；短期限流、会话数、缓存窗口用量不恢复。
- 账号凭据可以是完整、脱敏或不含凭据三种档案级别；消费者必须依据 `secret_mode` 决定是否可恢复。

### 4.2 Manifest 顶层结构

```json
{
  "format": "s2ax-account-transfer",
  "version": 2,
  "manifest_id": "01J...",
  "created_at": "2026-07-22T12:00:00Z",
  "created_by": { "kind": "admin", "id": 1 },
  "scope": { "kind": "selected", "account_count": 12 },
  "secret_mode": "encrypted-full",
  "checksum": { "algorithm": "sha256", "value": "..." },
  "groups": [],
  "proxies": [],
  "accounts": [],
  "relations": [],
  "omissions": []
}
```

### 4.3 稳定引用与对象字段

| 对象 | 稳定引用 | 关键字段 | 不导出的字段 |
| --- | --- | --- | --- |
| 分组 | `group_ref`（UUID） | 名称、状态、计费/订阅模式、路由属性、导出策略允许的配置 | 用户成员、实时用量、支付记录 |
| 代理 | `proxy_ref`（UUID） | 协议、主机、端口、状态、到期、备用代理引用、健康策略 | 运行时探测缓存 |
| 账号 | `account_ref`（UUID） | 平台、类型、身份、完整调度配置、凭据包、扩展配置 | 数据库 ID、实时会话、限流临时状态 |
| 关系 | `from_ref` / `to_ref` | `uses_proxy`、`member_of_group`、`shadow_of`、`fallback_to` | 由数据库自动生成的反向关系 |

`account_ref` 是导出时生成的档案内 UUID，不暴露实例内部 ID；再次导出同一实例时，若账号已有持久化 `external_ref` 则复用，否则生成并写入受控 metadata。这样后续差异导出和安全回滚才能稳定匹配。

### 4.4 凭据封装

| 档案类型 | 用途 | 凭据内容 | 恢复能力 |
| --- | --- | --- | --- |
| 配置模板 | 复制路由、调度、模型配置 | 完全不含凭据 | 只能作为添加/导入预设 |
| 脱敏清单 | 审计、盘点、迁移预览 | 只含类型、存在性、SHA-256 指纹前缀 | 不可创建可用账号 |
| 完整备份 | 灾备与实例迁移 | 加密后的凭据 payload | 可完整恢复 |

完整备份采用“明文 manifest + 加密 secret envelope”结构：manifest 中只有对象引用、`secret_ref` 和不可逆指纹；secret envelope 使用经审计的 AEAD 加密方案，密钥来自管理员选择的服务器托管密钥或一次性口令派生密钥。口令不上传、不写日志、不写任务记录。实现阶段必须复用项目已有的密钥/secret 存储能力；若没有合适能力，则完整备份先只允许服务器端加密并短期下载，不能临时手写加密算法。

### 4.5 兼容与迁移

1. V1 无 `type` / `version` 的历史包按 V1 解释；V1 的空头部不再被 V2 导出产生。
2. V1 代理指纹在导入计划中转换为临时 `proxy_ref`；`backup_proxy_name` 仅作最佳努力匹配，匹配失败必须成为 warning。
3. V1 未表达的字段在预览中标记为“来源未提供，使用目标默认值”，不能伪装成已恢复。
4. Bedrock、Service Account、Spark 影子账号仅在 V2 中保证完整表达；V1 继续按当前限制处理并给出迁移提示。

---

## 5. 添加账号设计

### 5.1 创建向导

现有 `CreateAccountModal` 的平台能力保留，但重新分为五步，不删除任何已支持平台字段：

1. **来源与认证**：选择平台、账号类型、认证方式；只显示该组合需要的凭据字段。
2. **身份与关联**：账号名称、备注、分组、代理、可选账号分配策略。
3. **调度与成本**：并发、优先级、负载因子、倍率、到期及自动暂停、可调度状态。
4. **模型与高级选项**：模型白名单/映射、端点能力、上游特有选项、请求头覆盖等。默认折叠，仅在该平台支持时出现。
5. **预检与创建**：显示规范化后的配置摘要、校验结果、混合渠道风险和可选连接测试。

所有平台特有逻辑仍位于现有凭据构建路径中；向导只调整展示与提交前审查，不创建一套通用“字段 DSL”。

### 5.2 配置预设

增加**无秘密账号预设**，用于重复的调度与路由配置：

- 预设包含：分组策略、代理选择策略、并发/优先级/倍率/负载因子、到期策略、模型策略、`extra` 白名单字段；
- 不包含：任何 access token、refresh token、私钥、代理密码、上游 API Key；
- 创建时先选预设再填写凭据；预设应用后，管理员可看到每个字段来源（系统默认 / 预设 / 本次覆盖）；
- 预设变化不追溯修改已有账号，避免隐式批量变更；需要修改已有账号时继续使用现有批量编辑。

### 5.3 测试与提交语义

| 操作 | 默认 | 结果 |
| --- | --- | --- |
| 本地校验 | 必做 | 校验字段结构、范围、模型映射、代理存在性、分组存在性 |
| 风险校验 | 必做 | 显示混合渠道、覆盖、过期、默认分组绑定等风险 |
| 上游测试 | 可选 | 使用最小无副作用请求；失败不自动阻止保存，除非管理员勾选“测试通过才创建” |
| 创建 | 明确确认 | 由现有 `POST /admin/accounts` 执行，并写审计事件 |

凭据输入只存在于组件内存；关闭、切换平台、提交完成或失败后均清空。不得写入 localStorage、URL、错误 toast 或浏览器分析事件。

### 5.4 同配置批量创建

用于“一批不同凭据，其他配置相同”的场景：管理员先完成步骤 1–4 的公共配置，再上传或粘贴多条凭据。系统生成候选表，每行可覆盖名称、代理、分组或跳过。提交复用 `POST /admin/accounts/batch` 的逐项语义，但通过计划任务获得进度和可下载结果。

---

## 6. 导入设计

### 6.1 支持的来源与明确边界

| 来源 | 输入 | 当前能力 | V2 完善点 |
| --- | --- | --- |
| S2AX / Sub2API V1 备份 | JSON | 代理与账号创建 | 迁移到候选对象，显示字段缺失和关联降级 |
| S2AX V2 备份 | 加密或非秘密 manifest | 新增 | 完整恢复、差异、回滚与跨实例映射 |
| Codex OAuth | auth.json、session JSON、多行 token | 已支持 | 预览 token 过期、身份、更新影响；不显示 token 明文 |
| Agent Identity | Agent Identity auth.json、ChatGPT session JSON | 已支持 | 显示是否需上游 bootstrap、账户 ID/用户 ID 匹配和计划档位提示 |
| Codex PAT | 单独的 `at-...` | 已有单独创建路径 | 继续独立于 Agent Identity；在导入中心明确选择“PAT 创建” |
| GPTSession 转换产物 | 标准 Sub2API envelope / session JSON | 已能解析部分格式 | 作为 Codex 来源兼容项，预检显示识别到的格式 |
| 配置模板 | V2 `secret_mode=none` | 新增 | 只生成“待填写凭据”的草稿，不创建不可用账号 |

**安全边界：** Agent Identity 模式只接受 ChatGPT session JSON 或已生成的 Agent Identity auth.json。单独 `at-...` 永远走 PAT 校验/创建路径，不自动升级为 Agent Identity。

### 6.2 导入工作流

#### 阶段 A：选择来源与输入

- 选择来源后，界面只展示该来源允许的粘贴区、文件类型和说明；
- 多文件保留文件名、大小、SHA-256 摘要和顺序，不在浏览器中无提示地扁平拼接；
- 客户端仅做格式、大小、JSON 语法和基础 schema 预检查；凭据真实性、账号冲突和上游能力由服务端处理；
- 单次输入默认上限为 10 MiB 或 2,000 候选对象；超过阈值要求分批或使用管理员上传接口，避免浏览器内存和反向代理超时。

#### 阶段 B：规范化与身份识别

每个来源解析为内部 `ImportCandidate`，不持久化原始秘密：

```text
source_file, source_index, source_kind,
account_identity { platform, account_id?, user_id?, email?, credential_fingerprint? },
credential_mode, proposed_account_config,
proposed_proxy_ref, proposed_group_refs,
warnings, errors
```

身份匹配顺序按平台显式定义，不能使用“名称相同”作为自动更新依据：

1. Agent Identity：`chatgpt_account_id + chatgpt_user_id`；缺少 user ID 时仅允许人工确认的 account ID 回填。
2. Codex OAuth：上游 account ID / user ID，其次经盐化的 refresh token 指纹；access-token-only 不得覆盖已有 refresh token。
3. API Key / Setup Token / Upstream：平台 + 不可逆凭据指纹 + 关键 endpoint 组合。
4. Service Account：`project_id + client_email`。
5. Bedrock：认证模式 + 账户/角色/区域组合。

同一批次内命中同一身份时，默认保留第一条并将后续条目列为重复；管理员可在预览中选择“采用最后一条”，但不能默默合并。

#### 阶段 C：预检计划

服务端返回一个不可变的 `ImportPlan`，而不是直接写入：

| 计划状态 | 含义 | 可执行性 |
| --- | --- | --- |
| `ready_create` | 身份不存在，字段完整 | 默认可提交 |
| `ready_update` | 身份已存在，存在可合并差异 | 需选择冲突策略 |
| `duplicate_in_input` | 与本批次候选重复 | 默认跳过 |
| `warning` | 可执行但存在缺失关联、过期或风险 | 需逐项或批量确认 |
| `blocked` | 凭据无效、格式不支持、目标引用不存在 | 不可提交 |
| `needs_secret` | 配置模板缺少必要凭据 | 只能保存为草稿或补充凭据 |

预览表支持按来源、平台、账号类型、目标分组、状态和行动筛选。每行提供“差异”抽屉，按前述六类字段展示：`现有值 → 导入值 → 将采用的值`。秘密字段只显示“已提供 / 将替换 / 将保留”及指纹尾部，绝不显示原文。

#### 阶段 D：冲突策略

| 对象 | 默认策略 | 可选策略 | 禁止行为 |
| --- | --- | --- | --- |
| 账号身份已存在 | `skip` | `merge_credentials`、`replace_selected_fields`、`create_copy` | 不允许按名称自动覆盖 |
| refresh token 缺失 | 保留现有 refresh token | 明确清空（需要危险确认） | access-token-only 覆盖自动续期凭据 |
| 代理已存在 | 按稳定引用复用 | 更新导出包明确提供的非秘密字段 | 按名称覆盖密码或地址 |
| 分组不存在 | 映射到管理员选择的分组或保持未分组 | 创建分组（需单独权限） | 自动创建同名但语义未知的分组 |
| 影子父账号缺失 | `blocked` | 创建父账号后重新解析 | 创建孤立影子账号 |
| Agent Identity 计划/组织不同 | `warning` | 更新身份密钥、创建副本 | 静默替换另一个 ChatGPT 用户身份 |

#### 阶段 E：提交与执行

- 预检成功后由 `plan_id + plan_revision + action_overrides` 提交；原始输入不需要再次由浏览器传输。
- 少于 50 个无上游 bootstrap 的候选，可同步等待任务完成，但接口仍返回统一 `job_id`。
- 其余全部进入后台任务；前端通过轮询或现有实时通道获取进度，不依赖长请求。
- 并发按来源控制：本地数据库写入最多 8 项并发；同一外部身份串行；Agent Identity bootstrap 最多 2 项并发；可调参数必须在服务端受上限保护。
- 每项写入以单对象数据库事务完成；完成后才触发非关键后台动作（隐私设置、上游能力探测、Grok probe）。这些动作的失败记录为后处理 warning，不把已成功创建的账号标成失败。
- 取消仅阻止未开始项；正在进行的上游 bootstrap 允许自然完成，随后标记为“已创建但任务已取消”，供管理员决定保留或回滚。

#### 阶段 F：结果、重试与回滚

任务结果表包含：来源文件/行号、账号引用、目标数据库 ID、最终动作、警告、错误分类、耗时和重试按钮。

- **重试失败项：** 复用原计划和原 action override，仅重新解析已失效的短期 session 时要求重新提供输入。
- **回滚本次创建：** 仅删除本任务创建且未被后续人工修改的账号/代理/关联。
- **回滚本次更新：** 导入前保存加密差异快照；只有当前版本仍等于本次写入版本时才恢复。存在后续修改时跳过并报告冲突。
- **删除导入材料：** 任务完成后立即擦除原始文件/文本；仅保留脱敏指纹、计划摘要和加密差异快照至保留期结束。

### 6.3 分组、代理与默认绑定策略

导入页面必须显示一个明确的“目标关联策略”区：

| 选项 | 默认 | 行为 |
| --- | --- | --- |
| 分组策略 | `保留导出关联；无关联则不绑定` | V2 按 `group_ref` 映射；V1 显示“未提供分组” |
| 默认分组绑定 | `关闭` | 只在管理员主动开启时让目标实例默认分组参与绑定 |
| 代理策略 | `按引用复用或导入` | 无法匹配时列为 warning，不默认改为直连 |
| 代理凭据覆盖 | `关闭` | 已存在代理必须逐项确认才更新敏感地址/认证字段 |
| 影子策略 | `连同父账号恢复` | 父账号不在计划中时阻止提交 |

这消除当前通用导入与 Codex 导入对 `skip_default_group_bind` 的隐式差异：UI 只传达一种策略，后端将其写入计划快照。

---

## 7. 导出与备份设计

### 7.1 导出向导

导出不是一个只有“含代理”的确认框。向导分三步：

1. **范围**：已选账号、当前筛选结果、指定分组、全部账号；显示精确数量及因权限/影子关系被排除的对象。
2. **内容与安全等级**：配置模板、脱敏清单、完整加密备份；是否包含代理、分组、影子关系、可恢复的调度状态。
3. **确认与下载**：显示风险、需要二次验证、生成说明、校验和、过期时间和下载次数。

全凭据备份使用醒目的危险样式，并要求已存在的 step-up 验证。导出确认文字必须说明“文件包含可调用上游服务的凭据；下载后不再受系统访问控制”。

### 7.2 导出范围规则

| 范围 | 行为 |
| --- | --- |
| 已选账号 | 导出选择集及其必要代理、分组、父账号；不会自动包含同组所有无关账号 |
| 当前筛选结果 | 服务端冻结筛选快照，记录筛选条件与最终 account refs；避免分页变化导致下载内容不确定 |
| 分组 | 包含分组对象、其中账号及所需代理；可选择是否包含子/影子关系 |
| 全量灾备 | 包含可恢复对象及 manifest；必须明确选择完整加密备份 |

任何无法表达的对象都进入 `omissions[]`，记录 `ref`、`reason_code` 与安全说明。V2 不允许像当前 Spark 影子账号一样仅给一个总数后静默排除。

### 7.3 文件生成与生命周期

- 小于 10 MiB 的配置模板或脱敏清单可同步生成并直接下载。
- 完整备份或大于阈值的导出生成 `export job`；服务端临时存储加密档案，下载 URL 单次或有限次有效，默认 24 小时失效。
- 生成后展示 SHA-256、对象计数、格式版本与创建者。管理员可以将校验和与文件分开保存。
- 不把全凭据 JSON 写入 HTTP access log、前端状态管理或错误上报；下载端点设置 `Cache-Control: no-store`。
- 导出成功、下载、过期、删除都写审计事件；审计只保存摘要和统计，不保存文件内容或秘密。

### 7.4 差异导出

V2 后续支持“从某 manifest 之后的变更”导出：

- 仅比较可恢复配置字段及 stable ref；不比较运行时计数；
- 差异包包含 `base_manifest_id` 和每个对象版本；
- 导入差异包必须先验证基线存在且对象版本可匹配，否则转为冲突预览，不能盲目应用；
- 第一阶段不实现跨实例双向合并，避免产生难以解释的凭据冲突。

---

## 8. 后端架构与接口

### 8.1 保留接口

以下接口继续可用，维持现有调用方兼容：

```text
POST /admin/accounts
POST /admin/accounts/batch
GET  /admin/accounts/data
POST /admin/accounts/data
POST /admin/accounts/import/codex-session
POST /admin/openai/create-from-codex-pat
```

`GET/POST /admin/accounts/data` 在 V2 上成为兼容入口：老请求仍处理 V1；新 UI 改走计划/任务接口。不能在同一路径上静默改变 V1 的创建语义。

### 8.2 新接口

```text
POST /admin/accounts/import-plans
GET  /admin/accounts/import-plans/:plan_id
POST /admin/accounts/import-plans/:plan_id/commit

GET  /admin/accounts/transfer-jobs/:job_id
POST /admin/accounts/transfer-jobs/:job_id/cancel
POST /admin/accounts/transfer-jobs/:job_id/retry-failed
POST /admin/accounts/transfer-jobs/:job_id/rollback

POST /admin/accounts/export-plans
POST /admin/accounts/export-plans/:plan_id/commit
GET  /admin/accounts/export-artifacts/:artifact_id
DELETE /admin/accounts/export-artifacts/:artifact_id

GET  /admin/accounts/transfer-history
```

接口均需管理员权限；完整凭据导出、下载、回滚、覆盖更新和创建分组等危险动作额外通过 step-up。所有提交接口接收 `Idempotency-Key`，相同键返回同一计划或任务结果。

### 8.3 ImportPlan 数据结构

```json
{
  "id": "plan_...",
  "revision": 1,
  "source_kind": "s2ax_v2 | bundle_v1 | codex | agent_identity | template",
  "summary": {
    "total": 100,
    "ready_create": 70,
    "ready_update": 12,
    "warning": 10,
    "blocked": 8
  },
  "target_policy": {
    "group_binding": "preserve_or_unassigned",
    "proxy_policy": "reuse_or_import",
    "default_group_binding": false
  },
  "items": [
    {
      "id": "item_...",
      "source": { "file": "accounts.json", "index": 7 },
      "identity": { "platform": "openai", "masked": "acct_...a7" },
      "action": "ready_update",
      "conflicts": [],
      "diff": [],
      "warnings": []
    }
  ],
  "expires_at": "2026-07-22T12:30:00Z"
}
```

计划仅保存清洗后的候选和加密临时秘密材料，默认 30 分钟失效。过期后需要重新预检，防止管理员在旧配置或旧分组上执行错误更新。

### 8.4 任务持久化

只增加一组传输任务表，不新增独立队列产品：

| 表 | 作用 | 关键字段 |
| --- | --- | --- |
| `account_transfer_jobs` | 一次导入、导出、回滚的状态与统计 | `id`、`kind`、`state`、`created_by`、`plan_id`、`progress`、`idempotency_key`、`expires_at` |
| `account_transfer_items` | 每个候选的执行/差异结果 | `job_id`、`source_ref`、`target_account_id`、`action`、`state`、`error_code`、`before_snapshot_ref` |
| `account_transfer_artifacts` | 导出文件与下载生命周期 | `job_id`、`storage_ref`、`checksum`、`secret_mode`、`expires_at`、`download_count` |

原始凭据只能放在受控加密临时存储，并与 plan/job 过期同时删除；任务表只保存引用和指纹。后台执行器复用项目已有的进程后台任务/锁模式，并通过数据库状态抢占实现重启恢复，避免单纯 goroutine 在进程重启后丢失任务。

### 8.5 事件与审计

每个关键动作产生统一事件：

```text
account.transfer.plan_created
account.transfer.job_started
account.transfer.item_created
account.transfer.item_updated
account.transfer.item_skipped
account.transfer.item_failed
account.transfer.rollback_completed
account.transfer.export_created
account.transfer.artifact_downloaded
```

字段包括操作者、目标 refs、来源类型、数量、策略、任务 ID、错误代码和凭据指纹；不得包含 token、私钥、密码、完整邮箱以外的敏感 payload。审计检索可按时间、操作者、动作、平台、分组、任务状态过滤。

---

## 9. 前端交互与显示细节

### 9.1 导入中心布局

```text
来源与输入
  ├─ 备份包 / Codex OAuth / Agent Identity / PAT / 配置模板
  ├─ 粘贴文本或上传文件
  └─ 输入摘要：文件、对象数、格式、敏感等级

目标策略
  ├─ 分组绑定
  ├─ 代理映射
  ├─ 冲突默认动作
  └─ 是否执行上游验证

预检结果
  ├─ 状态计数卡片
  ├─ 可筛选候选表
  ├─ 单项差异抽屉
  └─ 阻塞项修复建议

提交与结果
  ├─ 任务进度
  ├─ 成功 / 更新 / 跳过 / 失败表
  ├─ 下载结果报告
  └─ 重试失败 / 安全回滚
```

表格状态使用文字、图标和颜色三重表达：创建为绿色、更新为蓝色、跳过为灰色、警告为黄色、阻塞/失败为红色；不能只依赖颜色。每条长错误以摘要显示，点击才展开受控详情。

### 9.2 防止卡顿和页面闪烁

1. 预检/任务结果使用独立数据源，不触发整个账号列表重新挂载。
2. 任务完成后通过创建/更新的 account ID 增量补丁列表；只有筛选排序受影响或结果太多时才刷新当前页。
3. 继续沿用账号用量单元格的视口懒加载和全局并发队列，导入后的新页不会立即请求每个账号的 usage。
4. 导入结果分页、服务端排序；前端不保存几千条候选对象的深层响应式副本。
5. 任务进度以节流后的计数更新，不为每项结果重绘整页；详情按需加载。

### 9.3 导出确认

导出确认页展示如下不可忽略信息：

- 范围：`已选 32 个账号 / 6 个代理 / 4 个分组 / 3 个影子关系`；
- 内容：`完整凭据备份` 或 `脱敏清单`；
- 排除：逐项 `omissions`，而不是只有总数；
- 风险：是否包含 token、私钥、代理密码；
- 安全：step-up 状态、档案加密方式、下载有效期；
- 完成后：manifest ID、SHA-256、下载次数、立即删除按钮。

---

## 10. 校验、安全与错误处理

### 10.1 校验层次

| 层级 | 责任 | 例子 |
| --- | --- | --- |
| 客户端即时校验 | 减少明显错误 | JSON 语法、文件大小、必填输入、选择的策略冲突 |
| 服务端 schema 校验 | 保护 API 边界 | 平台/类型组合、倍率、并发、端口、V2 manifest 版本 |
| 业务预检 | 防止不正确写入 | 身份冲突、分组/代理引用、影子父子关系、过期 session |
| 上游可选验证 | 反映真实可用性 | token whoami、Agent Identity bootstrap、最小模型请求 |
| 提交前版本校验 | 防止 TOCTOU | plan revision、目标账号 `updated_at`/版本、分组状态 |

### 10.2 错误代码

错误应使用稳定代码供 UI、重试逻辑和审计使用，例如：

```text
transfer.invalid_manifest
transfer.unsupported_version
transfer.invalid_credential
transfer.identity_conflict
transfer.target_modified
transfer.proxy_reference_missing
transfer.group_reference_missing
transfer.shadow_parent_missing
transfer.upstream_rejected
transfer.rate_limited
transfer.cancelled
transfer.rollback_conflict
```

面向管理员的消息可附带安全建议，但不得回显上游 token、session JSON、认证 header 或原始响应体。

### 10.3 权限矩阵

| 动作 | 管理员 | Step-up | 审计 |
| --- | --- | --- | --- |
| 创建、预检、导入 | 是 | 仅包含敏感覆盖或来源需要时 | 是 |
| 更新已有账号凭据 | 是 | 是 | 是，记录指纹变化 |
| 导出脱敏清单/模板 | 是 | 否 | 是 |
| 导出完整备份、下载完整备份 | 是 | 是 | 是 |
| 回滚创建 | 是 | 否 | 是 |
| 回滚凭据/关联更新 | 是 | 是 | 是 |
| 创建/覆盖分组、代理敏感字段 | 是 | 是 | 是 |

---

## 11. 实施阶段

### 阶段 1：格式与预检基础

1. 新增 V2 manifest、严格 schema、V1 迁移器和稳定对象引用。
2. 修正导出时未写入格式/版本的问题；新增配置模板和脱敏清单，但保留 V1 完整导出。
3. 增加 `ImportPlan` API，先覆盖通用 V1/V2 备份和 Codex 导入。
4. 前端把直接导入改为“上传/粘贴 → 预检 → 提交”，显示逐项结果。

**验收：** 任何 V1 包能导入；任何 V2 包可被准确识别；提交前可看到每项动作，未确认时零数据库写入。

### 阶段 2：任务、历史与恢复

1. 引入 transfer job / item / artifact 持久化与进程重启恢复。
2. 导入大批量、Agent Identity bootstrap、完整导出均改由任务执行。
3. 实现失败重试、创建回滚、受版本保护的更新回滚。
4. 添加历史页、下载报告和删除临时档案。

**验收：** 浏览器刷新或服务重启后任务可继续或明确失败；同一批 1,000 项可追踪、取消、重试，不要求保持 HTTP 请求。

### 阶段 3：完整关系恢复与高级体验

1. V2 支持分组、影子账号、Bedrock、Service Account、代理备用关系的完整恢复。
2. 添加配置预设、同配置批量创建和差异导出。
3. 以增量补丁更新账号列表，补全可访问的差异与状态展示。

**验收：** 从一个完整 V2 灾备包恢复后，配置快照对比除数据库 ID、运行时数据外无差异；所有省略对象在导出前后均可定位原因。

---

## 12. 测试与验收清单

### 12.1 后端

- V1 无 type/version、V1 带 legacy type、V2 完整/脱敏/模板三类包的解析与迁移测试；
- 每个平台/账号类型的导出-导入往返测试，至少覆盖 OAuth、Setup Token、API Key、Upstream、Bedrock、Service Account、Agent Identity；
- 代理复用、备用代理、缺失代理、分组映射、影子关系、同批重复和跨实例冲突；
- 幂等键重复提交、计划过期、提交前目标修改、任务恢复、取消与回滚冲突；
- 确认日志、任务详情、响应、panic 恢复均不包含秘密文本；
- 导出 step-up、下载 TTL、删除、下载次数与权限校验；
- 1/50/500/2,000 候选对象的性能和内存基准，验证并发上限及数据库连接池行为。

### 12.2 前端

- 来源切换清除旧输入与秘密；关闭弹窗清空内存；不写入 localStorage；
- 预检列表筛选、差异抽屉、批量选择 action override、阻塞项不可提交；
- 导入任务完成时只增量刷新受影响账号，不让整页闪烁；
- 深浅色、窄屏、键盘导航、屏幕阅读器状态与错误提示；
- 完整导出二次验证、危险提示、校验和与一次性下载链接展示；
- 导入/导出结果在大量行下虚拟化或服务端分页，不发生主线程长任务。

### 12.3 关键验收场景

1. 管理员导出 100 个账号和关联代理为完整 V2 包，在空实例预检后恢复；分组、代理、模型映射和调度参数一致。
2. 管理员导入同一个 Codex OAuth 账号两次：第二次预检标记为身份冲突，选择 merge 后保留已有 refresh token。
3. Agent Identity 导入 ChatGPT session JSON：上游 bootstrap 成功后数据库和任务日志均不保存 access token；失败可定位为上游拒绝而不泄露 session 内容。
4. V1 包中包含 Service Account：预检明确标记为 V1 不支持，不产生半创建数据；转换为 V2 后可恢复。
5. 任务执行中管理员修改了一个目标账号：回滚该账号时报告 `target_modified`，不覆盖新修改。

---

## 13. 结论与实施优先级

优先实现“版本化格式 + 预检计划 + 持久化任务”三件事。它们直接解决当前导入的可见性、完整性、超时和安全问题，并可让已有创建、Codex 导入和导出接口逐步接入同一套结果/审计模型。

不应先做通用插件市场、跨实例实时同步或自动化上游账号注册。现有平台专用解析器与创建逻辑已经足够；V2 的价值在于把它们以可预览、可恢复、可审计的方式组织起来。
