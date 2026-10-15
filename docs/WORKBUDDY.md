# WorkBuddy 账号接入

S2AX 将 [magpie-community WorkBuddy 插件](https://github.com/magpie-community/plugins/tree/main/packages/workbuddy) 0.1.22 移植为两个原生 OAuth 平台：

| 平台 | 标识 | 官方服务 |
| --- | --- | --- |
| WorkBuddy 国内版 | `workbuddy` | `https://copilot.tencent.com` |
| WorkBuddy AI 国际版 | `workbuddy_ai` | `https://www.workbuddy.ai` |

协议参考版本：`index.mjs` blob `bd6f4bd2397862b11f7adb5990046079d92fba74`。原作者采用 MIT 许可，完整许可见 [WORKBUDDY_LICENSE.txt](WORKBUDDY_LICENSE.txt)。

## 配置

1. 在「账号管理 → 添加账号」选择对应 WorkBuddy 平台，填写名称、代理和分组。
2. 点击「生成登录链接」，打开授权页面并完成登录。页面自动轮询，授权有效期为 5 分钟。
3. 显示凭证就绪后保存。默认并发为 1，可按实际账号能力调整。
4. 在账号编辑界面同步上游模型，获取该个人或企业套餐 `/v3/config` 中 `cli` 代理的模型和能力。可按现有方式配置模型白名单及别名。
5. 给分组或渠道配置实际收费价格。官方积分倍率保存在模型能力快照的 `credit_rate` / `credit_rate_before_discount` 中，**不等同于美元价格，也不会自动设置免费计费**。

账号列表展示官方积分总量、已用、剩余；企业套餐在个人资源包为空时读取企业周期用量。套餐明细可用时还会展示最近一批非日切积分的到期时间和数量。明细读取失败会明确提示，已取得的周期用量仍可展示。

## 登录与刷新

浏览器授权保留 `uid`、`domain`、企业 ID 和令牌有效期。企业请求同时发送 `X-Enterprise-Id` 与 `X-Tenant-Id`；无法唯一确认企业时拒绝落入个人套餐。创建和编辑时均会校验凭证。

令牌通过现有统一 OAuth 刷新服务自动更新，后台刷新、推理、模型目录、积分查询共用同一个锁及数据库重读流程。提前 5 分钟刷新；刷新失败时只有尚未过期的访问令牌可继续使用。临时 WorkBuddy API 错误不会清空账号登录或误触发 ChatGPT 重新授权。

浏览器待授权状态保存在当前服务进程中，与管理员身份绑定并有数量及时间上限。**多实例部署需让授权开始与轮询请求落在同一实例；重启服务后需重新开始未完成的授权。** 已保存的账号和令牌不受此限制。

可导入桌面端的明文登录 JSON：

```json
{
  "auth": {
    "accessToken": "<access token>",
    "refreshToken": "<refresh token>",
    "expiresAt": 1792000000000,
    "domain": "copilot.tencent.com"
  },
  "account": {
    "uid": "<uid>",
    "type": "personal"
  }
}
```

时间戳 `expiresAt` / `refreshExpiresAt` 按毫秒解析，也支持相对秒数 `expiresIn` / `refreshExpiresIn`。企业账号需包含 `enterpriseId`。桌面端 5.6+ 的 `$wbEncrypted` 令牌不能在服务端解密，应改用浏览器登录。

原插件运行在用户电脑上，可每次读取桌面凭证文件；中转服务部署在服务器上，改为用户主动导入。复制桌面端正在轮换的刷新令牌会使两个消费者竞争，可能挤出登录，建议独立浏览器授权。服务端不会读取或改写用户电脑上的桌面凭证文件。

## 推理兼容

支持现有 `/v1/chat/completions`、`/v1/responses`、`/v1/messages` 入站，统一转为官方 `/v2/chat/completions`。上游始终使用 SSE，非流式客户端由服务端聚合返回，保留工具调用和官方 token 用量。

移植的兼容处理包括 developer/system、最大输出字段、上游渠道提示兼容、工具名称及历史调用映射、Schema 形状修正和返回工具名还原。国际版遇到明确的 11153 工具顺序错误时，仅修正消息顺序重试一次。

会话与用户轮次 ID 根据下游 API Key 和会话来源生成，工具循环和重试保留同一轮次 ID，每次实际出站生成新的消息和请求 ID。相同首条消息的不同 API Key 不会共享会话。客户端应发送稳定的 `session_id`、`x-session-id`、`x-conversation-id` 或 `x-magpie-workbuddy-session`（也支持 `prompt_cache_key`），以区分同一 Key 下内容相同的独立会话。未提供会话标识时回退到第一条用户消息摘要。客户端自报的 WorkBuddy 专用身份头不会直接透传。

模型能力使用当前套餐目录声明的上下文、图片支持和推理强度，不将某个 OpenAI 模型的能力强加给其他模型。国际版 GLM 图片能力按上游插件的已知兼容规则关闭。按小时促销不会作为长期免费状态缓存。

错误码 14018 表示积分不足（免费模型也需要有效积分包），11140 表示账号受限，非法调用渠道会返回明确的权限错误。积分错误不伪装成成功的零余额，截断流不伪装成完整响应。
