package com.s2ax.mobile

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountBalanceWallet
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.ManageAccounts
import androidx.compose.material.icons.outlined.MoreHoriz
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.People
import androidx.compose.material.icons.outlined.QueryStats
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import org.json.JSONArray
import org.json.JSONObject
import java.util.Locale

enum class MobileDataArea(
    val title: String,
    val description: String,
    val glyph: MobileGlyph,
) {
    Service("服务与用量", "请求、模型、可用渠道与配额", MobileGlyph.Usage),
    Assets("资产与订阅", "余额、兑换、订单与订阅状态", MobileGlyph.Wallet),
    City("城市模拟", "共享世界、角色与实时运行态", MobileGlyph.City),
    Operations("运营监控", "实时流量、告警、日志与审计", MobileGlyph.Operations),
    Resources("资源调度", "账号、用户、分组与上游资源", MobileGlyph.Resources),
    Commerce("计费与增长", "货币、订单、订阅与邀请返利", MobileGlyph.Commerce),
    Governance("安全与系统", "风控、提示词审计、设置与版本", MobileGlyph.Governance),
}

enum class MobileGlyph {
    Overview,
    Usage,
    Wallet,
    City,
    Operations,
    Resources,
    Commerce,
    Governance,
    People,
    Notices,
}

data class MobileDataModule(
    val id: String,
    val title: String,
    val description: String,
    val area: MobileDataArea,
    val path: String,
    val glyph: MobileGlyph = area.glyph,
    val adminOnly: Boolean = false,
    val paged: Boolean = false,
    val query: Map<String, String> = emptyMap(),
    val pageSize: Int = 50,
    val section: String = "",
) {
    fun requestQuery(page: Int): Map<String, String> = query.toMutableMap().apply {
        if (paged) {
            put("page", page.toString())
            if (!containsKey("page_size")) put("page_size", pageSize.toString())
        }
    }
}

data class ExplorerPayload(
    val module: MobileDataModule,
    val payload: Any,
    val page: Int,
    val loadedAtMillis: Long = System.currentTimeMillis(),
) {
    val hasMore: Boolean
        get() {
            if (!module.paged) return false
            val total = previewTotal(payload)
            return if (total != null) page.toLong() * module.pageSize < total else previewRows(payload).size >= module.pageSize
        }
}

object MobileDataModules {
    val cityWorlds = MobileDataModule(
        id = "city-worlds",
        title = "共享世界",
        description = "选择可进入的城市世界，查看时间、角色和运行态。",
        area = MobileDataArea.City,
        path = "/city/worlds",
        glyph = MobileGlyph.City,
    )

    private val common = listOf(
        MobileDataModule("usage-snapshot", "使用快照", "请求、Token、耗时与模型使用概览。", MobileDataArea.Service, "/usage/dashboard/snapshot-v2", MobileGlyph.Overview),
        MobileDataModule("usage-records", "使用记录", "分页查看自己的请求、模型、费用与处理状态。", MobileDataArea.Service, "/usage", MobileGlyph.Usage, paged = true),
        MobileDataModule("usage-stats", "用量汇总", "查看当前筛选窗口的请求、Token 与费用统计。", MobileDataArea.Service, "/usage/stats", MobileGlyph.Usage),
        MobileDataModule("usage-trend", "使用趋势", "按时间查看请求量和费用的变化。", MobileDataArea.Service, "/usage/dashboard/trend", MobileGlyph.Usage),
        MobileDataModule("usage-models", "模型统计", "按模型查看请求、Token 和费用分布。", MobileDataArea.Service, "/usage/dashboard/models", MobileGlyph.Usage),
        MobileDataModule("usage-errors", "请求错误", "查看自己的失败请求和错误分类。", MobileDataArea.Service, "/usage/errors", MobileGlyph.Usage, paged = true),
        MobileDataModule("available-channels", "可用渠道", "当前账号可调用的服务分组与模型能力。", MobileDataArea.Service, "/channels/available", MobileGlyph.Resources),
        MobileDataModule("channel-status", "渠道状态", "只读查看渠道健康状态和最近探测结果。", MobileDataArea.Service, "/channel-monitors", MobileGlyph.Operations, paged = true),
        MobileDataModule("available-groups", "可用分组", "创建密钥时可选择的分组和访问范围。", MobileDataArea.Service, "/groups/available", MobileGlyph.Resources),
        MobileDataModule("group-rates", "分组倍率", "查看获准分组的价格倍率。", MobileDataArea.Service, "/groups/rates", MobileGlyph.Usage),
        MobileDataModule("platform-quotas", "平台配额", "查看各平台剩余配额与窗口。", MobileDataArea.Service, "/user/platform-quotas", MobileGlyph.Usage),
        MobileDataModule("totp-status", "安全验证", "查看 TOTP 二次验证和登录保护状态。", MobileDataArea.Service, "/user/totp/status", MobileGlyph.Governance),
        MobileDataModule("user-profile", "账户资料", "完整查看个人资料与账户限制。", MobileDataArea.Service, "/user/profile", MobileGlyph.People),
        MobileDataModule("account-allocations", "已分配账号", "只读查看管理员分配的账号类型、容量、状态和用量。", MobileDataArea.Service, "/account-allocations", MobileGlyph.Resources),
        MobileDataModule("my-currencies", "我的虚拟货币", "查看每种货币的可用、预约和冻结余额。", MobileDataArea.Assets, "/user/currencies", MobileGlyph.Wallet),
        MobileDataModule("currency-holds", "资产冻结", "查看虚拟货币的预约、冻结与到期状态。", MobileDataArea.Assets, "/user/currencies/holds", MobileGlyph.Wallet, paged = true),
        MobileDataModule("subscription-summary", "订阅概览", "当前订阅、权益和剩余周期摘要。", MobileDataArea.Assets, "/subscriptions/summary", MobileGlyph.Commerce),
        MobileDataModule("subscription-progress", "订阅进度", "查看当前周期的权益使用和续期进度。", MobileDataArea.Assets, "/subscriptions/progress", MobileGlyph.Commerce),
        MobileDataModule("subscriptions", "我的订阅", "查看全部订阅记录和状态。", MobileDataArea.Assets, "/subscriptions", MobileGlyph.Commerce, paged = true),
        MobileDataModule("payment-plans", "可购套餐", "查看当前可用订阅套餐和价格。", MobileDataArea.Assets, "/payment/plans", MobileGlyph.Commerce),
        MobileDataModule("payment-limits", "支付限制", "查看当前账户可创建订单的限制。", MobileDataArea.Assets, "/payment/limits", MobileGlyph.Commerce),
        MobileDataModule("payment-orders", "我的订单", "查看支付、取消与退款状态。", MobileDataArea.Assets, "/payment/orders/my", MobileGlyph.Commerce, paged = true),
        MobileDataModule("redeem-history", "兑换记录", "查看已经提交的兑换码和结果。", MobileDataArea.Assets, "/redeem/history", MobileGlyph.Wallet, paged = true),
        MobileDataModule("affiliate", "邀请返利", "查看邀请链接、返利与可转余额。", MobileDataArea.Assets, "/user/aff", MobileGlyph.Commerce),
        cityWorlds,
    )

    private val admin = listOf(
        MobileDataModule("admin-dashboard", "系统总览", "管理员仪表盘的完整聚合快照。", MobileDataArea.Operations, "/admin/dashboard/snapshot-v2", MobileGlyph.Overview, adminOnly = true),
        MobileDataModule("admin-dashboard-realtime", "实时指标", "系统当前 QPS、Token、消耗和在线运行态。", MobileDataArea.Operations, "/admin/dashboard/realtime", MobileGlyph.Operations, adminOnly = true),
        MobileDataModule("admin-dashboard-trend", "全局趋势", "管理员视角的使用、费用和吞吐趋势。", MobileDataArea.Operations, "/admin/dashboard/trend", MobileGlyph.Usage, adminOnly = true),
        MobileDataModule("admin-dashboard-models", "全局模型分布", "按模型汇总请求、Token 和实际消耗。", MobileDataArea.Operations, "/admin/dashboard/models", MobileGlyph.Usage, adminOnly = true),
        MobileDataModule("admin-dashboard-groups", "全局分组分布", "按服务分组查看消耗、容量与活跃情况。", MobileDataArea.Operations, "/admin/dashboard/groups", MobileGlyph.Resources, adminOnly = true),
        MobileDataModule("admin-dashboard-users", "用户消耗排行", "按用户查看用量、费用和活跃度。", MobileDataArea.Operations, "/admin/dashboard/users-ranking", MobileGlyph.People, adminOnly = true),
        MobileDataModule("ops-snapshot", "运维快照", "QPS、TPS、延迟、错误与系统健康。", MobileDataArea.Operations, "/admin/ops/dashboard/snapshot-v2", MobileGlyph.Operations, adminOnly = true),
        MobileDataModule("ops-overview", "运维概览", "服务可用性、告警和系统资源概览。", MobileDataArea.Operations, "/admin/ops/dashboard/overview", MobileGlyph.Operations, adminOnly = true),
        MobileDataModule("ops-throughput", "吞吐趋势", "QPS、TPS 和请求吞吐的时间序列。", MobileDataArea.Operations, "/admin/ops/dashboard/throughput-trend", MobileGlyph.Usage, adminOnly = true),
        MobileDataModule("ops-latency", "延迟分布", "请求时延直方图、分位数和长尾变化。", MobileDataArea.Operations, "/admin/ops/dashboard/latency-histogram", MobileGlyph.Operations, adminOnly = true),
        MobileDataModule("ops-error-trend", "错误趋势", "按时间追踪失败请求和异常波动。", MobileDataArea.Operations, "/admin/ops/dashboard/error-trend", MobileGlyph.Operations, adminOnly = true),
        MobileDataModule("ops-error-distribution", "错误分布", "按来源、状态和原因汇总异常。", MobileDataArea.Operations, "/admin/ops/dashboard/error-distribution", MobileGlyph.Operations, adminOnly = true),
        MobileDataModule("ops-openai-token-stats", "OpenAI Token 统计", "查看 OpenAI 上游的 Token 处理状态。", MobileDataArea.Operations, "/admin/ops/dashboard/openai-token-stats", MobileGlyph.Usage, adminOnly = true),
        MobileDataModule("ops-concurrency", "并发与排队", "平台并发、排队和账号容量。", MobileDataArea.Operations, "/admin/ops/concurrency", MobileGlyph.Operations, adminOnly = true),
        MobileDataModule("ops-availability", "账号可用性", "各上游账号的可调度与异常概览。", MobileDataArea.Operations, "/admin/ops/account-availability", MobileGlyph.Operations, adminOnly = true),
        MobileDataModule("ops-traffic", "实时流量", "当前窗口的请求、Token 与吞吐汇总。", MobileDataArea.Operations, "/admin/ops/realtime-traffic", MobileGlyph.Usage, adminOnly = true),
        MobileDataModule("ops-alert-rules", "告警规则", "告警阈值、通知方式和启用状态。", MobileDataArea.Operations, "/admin/ops/alert-rules", MobileGlyph.Operations, adminOnly = true),
        MobileDataModule("ops-alert-events", "告警事件", "待处理、已确认和静默事件。", MobileDataArea.Operations, "/admin/ops/alert-events", MobileGlyph.Operations, adminOnly = true, paged = true),
        MobileDataModule("ops-request-errors", "请求错误", "客户端失败请求的筛选结果与原因。", MobileDataArea.Operations, "/admin/ops/request-errors", MobileGlyph.Operations, adminOnly = true, paged = true),
        MobileDataModule("ops-upstream-errors", "上游错误", "上游服务调用失败与恢复状态。", MobileDataArea.Operations, "/admin/ops/upstream-errors", MobileGlyph.Operations, adminOnly = true, paged = true),
        MobileDataModule("ops-request-drilldown", "请求明细", "查看成功与失败请求的运行明细。", MobileDataArea.Operations, "/admin/ops/requests", MobileGlyph.Usage, adminOnly = true, paged = true),
        MobileDataModule("ops-ingress-rejections", "入口拒绝", "查看入口限流、拒绝与保护机制统计。", MobileDataArea.Operations, "/admin/ops/ingress-rejections", MobileGlyph.Operations, adminOnly = true, paged = true),
        MobileDataModule("ops-system-logs", "系统日志", "索引后的系统日志与诊断上下文。", MobileDataArea.Operations, "/admin/ops/system-logs", MobileGlyph.Operations, adminOnly = true, paged = true),
        MobileDataModule("ops-log-health", "日志健康", "查看日志索引、摄取和查询健康状态。", MobileDataArea.Operations, "/admin/ops/system-logs/health", MobileGlyph.Operations, adminOnly = true),
        MobileDataModule("audit-logs", "操作审计", "管理面操作、来源和审计结果。", MobileDataArea.Operations, "/admin/audit-logs", MobileGlyph.Governance, adminOnly = true, paged = true),
        MobileDataModule("admin-usage", "全局使用记录", "按用户、分组、模型和请求状态审阅用量。", MobileDataArea.Operations, "/admin/usage", MobileGlyph.Usage, adminOnly = true, paged = true),
        MobileDataModule("group-usage-summary", "分组用量摘要", "各分组的请求、Token 和费用汇总。", MobileDataArea.Resources, "/admin/groups/usage-summary", MobileGlyph.Usage, adminOnly = true),
        MobileDataModule("group-capacity-summary", "分组容量", "分组账号容量、并发与空闲资源。", MobileDataArea.Resources, "/admin/groups/capacity-summary", MobileGlyph.Resources, adminOnly = true),
        MobileDataModule("allocation-capabilities", "分配能力", "查看账号分配策略支持的条件、上限和行为。", MobileDataArea.Resources, "/admin/account-allocations/capabilities", MobileGlyph.Resources, adminOnly = true),
        MobileDataModule("admin-channels", "渠道与定价", "模型渠道、价格和路由配置概览。", MobileDataArea.Resources, "/admin/channels", MobileGlyph.Resources, adminOnly = true, paged = true),
        MobileDataModule("admin-channel-monitors", "渠道监控", "监控任务、状态和最近检查。", MobileDataArea.Resources, "/admin/channel-monitors", MobileGlyph.Operations, adminOnly = true, paged = true),
        MobileDataModule("admin-proxies", "代理资源", "代理池、归属与质量信息。", MobileDataArea.Resources, "/admin/proxies", MobileGlyph.Resources, adminOnly = true, paged = true),
        MobileDataModule("tls-profiles", "TLS 指纹模板", "可供账号使用的浏览器指纹模板。", MobileDataArea.Resources, "/admin/tls-fingerprint-profiles", MobileGlyph.Resources, adminOnly = true),
        MobileDataModule("channel-monitor-templates", "监控模板", "渠道监控模板和可复用的探测配置。", MobileDataArea.Resources, "/admin/channel-monitor-templates", MobileGlyph.Operations, adminOnly = true),
        MobileDataModule("error-passthrough", "错误透传规则", "上游错误映射和客户端返回策略。", MobileDataArea.Resources, "/admin/error-passthrough-rules", MobileGlyph.Governance, adminOnly = true, paged = true),
        MobileDataModule("admin-user-attributes", "用户属性", "可配置用户属性定义与排序。", MobileDataArea.Resources, "/admin/user-attributes", MobileGlyph.People, adminOnly = true),
        MobileDataModule("admin-currencies", "虚拟货币定义", "货币代码、精度、启用状态与适用范围。", MobileDataArea.Commerce, "/admin/currencies", MobileGlyph.Wallet, adminOnly = true),
        MobileDataModule("currency-integrations", "货币集成", "外部货币来源及其已授权范围。", MobileDataArea.Commerce, "/admin/currency-integrations", MobileGlyph.Wallet, adminOnly = true),
        MobileDataModule("redeem-stats", "兑换码统计", "兑换码总量、已用、过期与分发金额。", MobileDataArea.Commerce, "/admin/redeem-codes/stats", MobileGlyph.Commerce, adminOnly = true),
        MobileDataModule("redeem-codes", "兑换码", "兑换码列表、状态与使用对象。", MobileDataArea.Commerce, "/admin/redeem-codes", MobileGlyph.Commerce, adminOnly = true, paged = true),
        MobileDataModule("promo-codes", "优惠码", "优惠规则、有效期和使用记录。", MobileDataArea.Commerce, "/admin/promo-codes", MobileGlyph.Commerce, adminOnly = true, paged = true),
        MobileDataModule("admin-subscriptions", "订阅管理", "用户订阅、权益和进度记录。", MobileDataArea.Commerce, "/admin/subscriptions", MobileGlyph.Commerce, adminOnly = true, paged = true),
        MobileDataModule("payment-dashboard", "支付总览", "订单金额、支付状态和转化统计。", MobileDataArea.Commerce, "/admin/payment/dashboard", MobileGlyph.Commerce, adminOnly = true),
        MobileDataModule("admin-payment-orders", "订单管理", "支付、履约、取消与退款状态。", MobileDataArea.Commerce, "/admin/payment/orders", MobileGlyph.Commerce, adminOnly = true, paged = true),
        MobileDataModule("payment-plans-admin", "套餐管理", "订阅套餐、价格和上架状态。", MobileDataArea.Commerce, "/admin/payment/plans", MobileGlyph.Commerce, adminOnly = true, paged = true),
        MobileDataModule("payment-providers", "支付服务商", "支付服务商实例与启用状态。", MobileDataArea.Commerce, "/admin/payment/providers", MobileGlyph.Commerce, adminOnly = true),
        MobileDataModule("affiliate-invites", "邀请记录", "邀请关系和累计返利。", MobileDataArea.Commerce, "/admin/affiliates/invites", MobileGlyph.Commerce, adminOnly = true, paged = true),
        MobileDataModule("affiliate-rebates", "返利记录", "订单返利明细与结算状态。", MobileDataArea.Commerce, "/admin/affiliates/rebates", MobileGlyph.Commerce, adminOnly = true, paged = true),
        MobileDataModule("affiliate-transfers", "返利转移", "返利额度转入余额的流水。", MobileDataArea.Commerce, "/admin/affiliates/transfers", MobileGlyph.Commerce, adminOnly = true, paged = true),
        MobileDataModule("risk-status", "风控运行态", "内容审核服务健康、策略和最近统计。", MobileDataArea.Governance, "/admin/risk-control/status", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("risk-logs", "风控记录", "命中结果、原因与处置记录。", MobileDataArea.Governance, "/admin/risk-control/logs", MobileGlyph.Governance, adminOnly = true, paged = true),
        MobileDataModule("prompt-audit-runtime", "提示词审计运行态", "审计模型、队列和实时健康。", MobileDataArea.Governance, "/admin/prompt-audit/runtime", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("prompt-audit-events", "提示词审计事件", "独立提示词审计的事件与判定详情。", MobileDataArea.Governance, "/admin/prompt-audit/events", MobileGlyph.Governance, adminOnly = true, paged = true),
        MobileDataModule("ip-geolocation", "IP 归属设置", "当前 IP 数据库、解析源和回退策略。", MobileDataArea.Governance, "/admin/settings/ip-geolocation", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("system-settings", "系统设置", "全局开关与已生效配置的只读预览。", MobileDataArea.Governance, "/admin/settings", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("ops-advanced-settings", "运维高级设置", "限流、日志和运行时阈值配置预览。", MobileDataArea.Governance, "/admin/ops/advanced-settings", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("ops-email-notifications", "运维通知", "告警邮件接收方、阈值和通知开关。", MobileDataArea.Governance, "/admin/ops/email-notification/config", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("ops-runtime-alert", "告警运行配置", "在线生效的告警窗口、频率和抑制设置。", MobileDataArea.Governance, "/admin/ops/runtime/alert", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("ops-runtime-logging", "日志运行配置", "运行时日志级别、保留和输出配置。", MobileDataArea.Governance, "/admin/ops/runtime/logging", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("ops-metric-thresholds", "监控阈值", "CPU、内存、队列与告警阈值。", MobileDataArea.Governance, "/admin/ops/settings/metric-thresholds", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("payment-config", "支付配置", "支付能力、可见服务商和默认配置。", MobileDataArea.Governance, "/admin/payment/config", MobileGlyph.Commerce, adminOnly = true),
        MobileDataModule("data-management", "数据管理", "数据库、Redis、对象存储和归档配置状态。", MobileDataArea.Governance, "/admin/data-management/config", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("data-agent-health", "数据代理健康", "数据管理代理、连通性和任务队列状态。", MobileDataArea.Governance, "/admin/data-management/agent/health", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("data-backup-jobs", "数据备份任务", "数据管理模块创建的备份作业和结果。", MobileDataArea.Governance, "/admin/data-management/backups", MobileGlyph.Governance, adminOnly = true, paged = true),
        MobileDataModule("backups", "备份任务", "数据库与对象存储备份记录。", MobileDataArea.Governance, "/admin/backups", MobileGlyph.Governance, adminOnly = true, paged = true),
        MobileDataModule("compliance", "部署合规", "当前部署确认与合规状态。", MobileDataArea.Governance, "/admin/compliance", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("system-version", "版本信息", "当前部署版本和构建信息。", MobileDataArea.Governance, "/admin/system/version", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("system-updates", "版本更新", "当前项目更新源的可用版本。", MobileDataArea.Governance, "/admin/system/check-updates", MobileGlyph.Governance, adminOnly = true),
        MobileDataModule("city-clock-health", "城市时钟健康", "NTP 对齐、世界时钟和实时服务健康。", MobileDataArea.City, "/admin/city/clock-health", MobileGlyph.City, adminOnly = true),
        MobileDataModule("city-visual-packs", "城市视觉包", "像素世界素材包、版本和发布状态。", MobileDataArea.City, "/admin/city/visual-packs", MobileGlyph.City, adminOnly = true),
        MobileDataModule("city-visual-policies", "视觉发布策略", "世界视觉包的生效策略与范围。", MobileDataArea.City, "/admin/city/visual-release-policies", MobileGlyph.City, adminOnly = true),
    )

    fun visible(isAdmin: Boolean): List<MobileDataModule> = if (isAdmin) common + admin else common

    fun byId(id: String, isAdmin: Boolean): MobileDataModule? = visible(isAdmin).firstOrNull { it.id == id }

    fun cityWorldModules(worldID: Long, isAdmin: Boolean): List<MobileDataModule> {
        val prefix = "/city/worlds/$worldID"
        val modules = listOf(
            MobileDataModule("city-$worldID-summary", "世界总览", "世界设定、成员和当前经济摘要。", MobileDataArea.City, prefix, MobileGlyph.City, section = "世界总览"),
            MobileDataModule("city-$worldID-clock", "实时日历", "原子时间、世界时钟和日历状态。", MobileDataArea.City, "$prefix/clock", MobileGlyph.City, section = "世界总览"),
            MobileDataModule("city-$worldID-timeline", "时间线", "当前世界的时间帧、阶段和实时推进状态。", MobileDataArea.City, "$prefix/timeline", MobileGlyph.City, section = "世界总览"),
            MobileDataModule("city-$worldID-members", "世界成员", "可进入当前共享世界的成员与角色权限。", MobileDataArea.City, "$prefix/members", MobileGlyph.People, section = "世界总览"),
            MobileDataModule("city-$worldID-state", "物理状态", "地形、建筑、实体和空间状态。", MobileDataArea.City, "$prefix/state", MobileGlyph.City, section = "世界总览"),
            MobileDataModule("city-$worldID-calendar", "城市日历", "城市日期、天气周期和公共时段状态。", MobileDataArea.City, "$prefix/calendar", MobileGlyph.City, section = "世界总览"),
            MobileDataModule("city-$worldID-population", "人口与家庭", "人口、迁移、家庭和就业摘要。", MobileDataArea.City, "$prefix/population", MobileGlyph.People, section = "世界总览"),
            MobileDataModule("city-$worldID-markets", "城市市场", "市场、资源、价格和结算状态。", MobileDataArea.City, "$prefix/markets", MobileGlyph.Commerce, section = "世界总览"),
            MobileDataModule("city-$worldID-land", "土地与分区", "土地属性、地块分区和使用状态。", MobileDataArea.City, "$prefix/land", MobileGlyph.City, section = "世界总览"),
            MobileDataModule("city-$worldID-development", "城市发展", "建筑开发、升级和空间演化状态。", MobileDataArea.City, "$prefix/development", MobileGlyph.City, section = "世界总览"),
            MobileDataModule("city-$worldID-trial-balance", "经济试算", "世界经济试算与平衡状态。", MobileDataArea.City, "$prefix/trial-balance", MobileGlyph.Commerce, section = "世界总览"),

            MobileDataModule("city-$worldID-ruleset", "空间规则集", "当前世界适用的空间生成规则和参数。", MobileDataArea.City, "$prefix/spatial/ruleset", MobileGlyph.Governance, section = "地图与空间"),
            MobileDataModule("city-$worldID-overmap", "区域总览地图", "城市区域、道路、地标与宏观空间布局。", MobileDataArea.City, "$prefix/spatial/overmap", MobileGlyph.City, section = "地图与空间"),
            MobileDataModule("city-$worldID-map", "开放世界地图", "道路、地块、建筑和空间生成验证。", MobileDataArea.City, "$prefix/open-world/map", MobileGlyph.City, section = "地图与空间"),
            MobileDataModule("city-$worldID-generation", "世界生成报告", "生成器版本、地形风格和规划结果。", MobileDataArea.City, "$prefix/open-world/generation", MobileGlyph.City, section = "地图与空间"),
            MobileDataModule("city-$worldID-verification", "地图校验", "道路、建筑、服务和空间约束的校验结果。", MobileDataArea.City, "$prefix/open-world/verification", MobileGlyph.Governance, section = "地图与空间"),
            MobileDataModule("city-$worldID-chunks", "空间区块", "当前世界已生成区块及其层级状态。", MobileDataArea.City, "$prefix/spatial/chunks", MobileGlyph.City, paged = true, section = "地图与空间"),
            MobileDataModule("city-$worldID-spatial-changes", "空间变更", "地图、地块与设施的空间变更流水。", MobileDataArea.City, "$prefix/spatial/changes", MobileGlyph.City, paged = true, section = "地图与空间"),
            MobileDataModule("city-$worldID-portals", "通行节点", "建筑入口、传送节点和可达状态。", MobileDataArea.City, "$prefix/navigation/portals", MobileGlyph.City, section = "地图与空间"),
            MobileDataModule("city-$worldID-navigation-intents", "导航意图", "角色移动意图、目标与路径状态。", MobileDataArea.City, "$prefix/navigation/intents", MobileGlyph.People, paged = true, section = "地图与空间"),
            MobileDataModule("city-$worldID-navigation-reservations", "路径预约", "通道、道路与空间资源预约状态。", MobileDataArea.City, "$prefix/navigation/reservations", MobileGlyph.City, paged = true, section = "地图与空间"),

            MobileDataModule("city-$worldID-services", "公共服务总览", "设施、需求、连接和服务供给。", MobileDataArea.City, "$prefix/open-world/services", MobileGlyph.Resources, section = "服务与基础设施"),
            MobileDataModule("city-$worldID-service-catalog", "服务目录", "当前世界可用的公共服务类型与规则。", MobileDataArea.City, "$prefix/services/catalog", MobileGlyph.Resources, section = "服务与基础设施"),
            MobileDataModule("city-$worldID-facilities", "服务设施", "设施容量、状态、位置和服务范围。", MobileDataArea.City, "$prefix/services/facilities", MobileGlyph.Resources, paged = true, section = "服务与基础设施"),
            MobileDataModule("city-$worldID-demands", "服务需求", "居民、企业与区域的服务需求。", MobileDataArea.City, "$prefix/services/demands", MobileGlyph.Resources, paged = true, section = "服务与基础设施"),
            MobileDataModule("city-$worldID-connections", "服务连接", "服务网络连接、覆盖和传输状态。", MobileDataArea.City, "$prefix/services/connections", MobileGlyph.Resources, paged = true, section = "服务与基础设施"),
            MobileDataModule("city-$worldID-service-network", "物理服务网络", "网络节点、边、流量和当前运行态。", MobileDataArea.City, "$prefix/services/networks", MobileGlyph.Resources, paged = true, section = "服务与基础设施"),
            MobileDataModule("city-$worldID-network-diagnostics", "网络诊断", "服务网络容量、拥塞和异常诊断。", MobileDataArea.City, "$prefix/services/networks/diagnostics", MobileGlyph.Operations, section = "服务与基础设施"),
            MobileDataModule("city-$worldID-infrastructure", "基础设施", "道路、管网、能源等基础设施状态。", MobileDataArea.City, "$prefix/open-world/infrastructure", MobileGlyph.Resources, section = "服务与基础设施"),
            MobileDataModule("city-$worldID-capacity", "有效容量", "基础设施和服务的有效可用容量。", MobileDataArea.City, "$prefix/open-world/effective-capacity", MobileGlyph.Resources, section = "服务与基础设施"),
            MobileDataModule("city-$worldID-service-operations", "设施运行", "设施运营、排班、事件和预算动作。", MobileDataArea.City, "$prefix/services/lifecycle/operations", MobileGlyph.Operations, paged = true, section = "服务与基础设施"),
            MobileDataModule("city-$worldID-service-incidents", "设施事件", "服务中断、事故和处置记录。", MobileDataArea.City, "$prefix/services/lifecycle/incidents", MobileGlyph.Operations, paged = true, section = "服务与基础设施"),

            MobileDataModule("city-$worldID-mobility", "通勤与流动", "出行、到达、OD 与通勤生命周期。", MobileDataArea.City, "$prefix/open-world/mobility", MobileGlyph.Operations, section = "交通与经济"),
            MobileDataModule("city-$worldID-mobility-arrivals", "到达状态", "出行到达、等待和拥堵状态。", MobileDataArea.City, "$prefix/open-world/mobility/arrivals", MobileGlyph.Operations, section = "交通与经济"),
            MobileDataModule("city-$worldID-mobility-od", "OD 流向", "起终点、交通需求和路径流向。", MobileDataArea.City, "$prefix/open-world/mobility/od", MobileGlyph.Operations, section = "交通与经济"),
            MobileDataModule("city-$worldID-commutes", "通勤记录", "角色通勤、出行方式和工作地匹配。", MobileDataArea.City, "$prefix/open-world/commutes", MobileGlyph.People, paged = true, section = "交通与经济"),
            MobileDataModule("city-$worldID-supply", "供应链", "企业货运、结算和承运恢复。", MobileDataArea.City, "$prefix/open-world/supply-chain", MobileGlyph.Resources, section = "交通与经济"),
            MobileDataModule("city-$worldID-freight", "企业货运", "企业货运任务、运输进度和收货状态。", MobileDataArea.City, "$prefix/open-world/enterprise-freight", MobileGlyph.Resources, paged = true, section = "交通与经济"),
            MobileDataModule("city-$worldID-freight-batches", "货运批次", "批次、装载、运力和配送状态。", MobileDataArea.City, "$prefix/open-world/freight-batches", MobileGlyph.Resources, paged = true, section = "交通与经济"),
            MobileDataModule("city-$worldID-freight-settlements", "货运结算", "货运成本、结算与状态变更。", MobileDataArea.City, "$prefix/open-world/freight-settlements", MobileGlyph.Commerce, paged = true, section = "交通与经济"),
            MobileDataModule("city-$worldID-carrier-recovery", "承运恢复", "承运资源恢复、失败重试和可用性。", MobileDataArea.City, "$prefix/open-world/carrier-recovery", MobileGlyph.Operations, paged = true, section = "交通与经济"),
            MobileDataModule("city-$worldID-carrier-commerce", "承运商业", "承运订单、收益和交易状态。", MobileDataArea.City, "$prefix/open-world/carrier-commerce", MobileGlyph.Commerce, paged = true, section = "交通与经济"),
            MobileDataModule("city-$worldID-resource-operations", "资源操作", "城市资源生产、消耗和调拨流水。", MobileDataArea.City, "$prefix/resource-operations", MobileGlyph.Resources, paged = true, section = "交通与经济"),
            MobileDataModule("city-$worldID-market-settlements", "市场结算", "市场成交、价格和资源结算流水。", MobileDataArea.City, "$prefix/market-settlements", MobileGlyph.Commerce, paged = true, section = "交通与经济"),
            MobileDataModule("city-$worldID-population-movements", "人口流动", "居民移动、迁移和生活地点变更。", MobileDataArea.City, "$prefix/population-movements", MobileGlyph.People, paged = true, section = "交通与经济"),
            MobileDataModule("city-$worldID-population-migrations", "人口迁移", "迁入、迁出和迁移原因。", MobileDataArea.City, "$prefix/population-migrations", MobileGlyph.People, paged = true, section = "交通与经济"),
            MobileDataModule("city-$worldID-household-movements", "家庭变动", "家庭搬迁、合并、分离和住房变化。", MobileDataArea.City, "$prefix/household-movements", MobileGlyph.People, paged = true, section = "交通与经济"),

            MobileDataModule("city-$worldID-runtime-catalog", "角色运行目录", "当前世界的 Agent、角色类型和运行能力。", MobileDataArea.City, "$prefix/runtime/catalog", MobileGlyph.People, section = "角色与规则"),
            MobileDataModule("city-$worldID-actors", "世界角色", "NPC、用户角色和 Agent 运行状态。", MobileDataArea.City, "$prefix/runtime/actors", MobileGlyph.People, paged = true, section = "角色与规则"),
            MobileDataModule("city-$worldID-rules", "城市规则", "法规、案件和当前执行状态。", MobileDataArea.City, "$prefix/runtime/rules", MobileGlyph.Governance, paged = true, section = "角色与规则"),
            MobileDataModule("city-$worldID-cases", "规则案件", "规则命中、调查、处罚与复核状态。", MobileDataArea.City, "$prefix/runtime/cases", MobileGlyph.Governance, paged = true, section = "角色与规则"),
            MobileDataModule("city-$worldID-events", "共享角色事件", "共享世界发生的角色事件和公共影响。", MobileDataArea.City, "$prefix/realtime/events", MobileGlyph.Notices, paged = true, section = "角色与规则"),
            MobileDataModule("city-$worldID-character", "我的角色", "当前角色、人格 Agent 和生活状态。", MobileDataArea.City, "$prefix/realtime/character", MobileGlyph.People, section = "角色与规则"),
            MobileDataModule("city-$worldID-character-events", "角色事件", "角色的经历、奖励和待处理事件。", MobileDataArea.City, "$prefix/realtime/character/events", MobileGlyph.Notices, paged = true, section = "角色与规则"),
            MobileDataModule("city-$worldID-character-relations", "角色关系", "关系网络、亲密度、信任和社交事件。", MobileDataArea.City, "$prefix/realtime/character/relations", MobileGlyph.People, paged = true, section = "角色与规则"),
            MobileDataModule("city-$worldID-character-case-reviews", "案件复核", "与当前角色有关的案件复核和裁定。", MobileDataArea.City, "$prefix/realtime/character/case-reviews", MobileGlyph.Governance, paged = true, section = "角色与规则"),
            MobileDataModule("city-$worldID-character-case-process", "案件流程", "当前角色案件的执行流程与阶段。", MobileDataArea.City, "$prefix/realtime/character/case-process", MobileGlyph.Governance, paged = true, section = "角色与规则"),
            MobileDataModule("city-$worldID-character-tasks", "角色任务", "角色 Agent 的当前任务与完成状态。", MobileDataArea.City, "$prefix/realtime/character/tasks", MobileGlyph.People, paged = true, section = "角色与规则"),

            MobileDataModule("city-$worldID-events-history", "世界事件记录", "系统事件、经济变化和公共影响的历史记录。", MobileDataArea.City, "$prefix/events", MobileGlyph.Notices, paged = true, section = "历史与审计"),
            MobileDataModule("city-$worldID-journals", "世界日志", "城市世界的决策、变更和实时日志。", MobileDataArea.City, "$prefix/journals", MobileGlyph.Notices, paged = true, section = "历史与审计"),
            MobileDataModule("city-$worldID-snapshots", "世界快照", "可复查的世界状态快照和时间点。", MobileDataArea.City, "$prefix/snapshots", MobileGlyph.City, paged = true, section = "历史与审计"),
        )
        return if (isAdmin) modules + listOf(
            MobileDataModule("city-$worldID-engine", "世界引擎", "引擎版本、实时调度、升级和回放基础信息。", MobileDataArea.City, "$prefix/engine", MobileGlyph.Operations, adminOnly = true, section = "管理员运行控制"),
            MobileDataModule("city-$worldID-commands", "世界命令", "管理员提交的模拟命令与执行结果。", MobileDataArea.City, "$prefix/commands", MobileGlyph.Operations, adminOnly = true, paged = true, section = "管理员运行控制"),
            MobileDataModule("city-$worldID-upgrades", "升级任务", "世界版本升级和迁移任务状态。", MobileDataArea.City, "$prefix/upgrade-runs", MobileGlyph.Operations, adminOnly = true, paged = true, section = "管理员运行控制"),
            MobileDataModule("city-$worldID-replays", "回放任务", "世界回放和恢复任务运行记录。", MobileDataArea.City, "$prefix/replay-runs", MobileGlyph.Operations, adminOnly = true, paged = true, section = "管理员运行控制"),
            MobileDataModule("city-$worldID-recoveries", "恢复任务", "世界状态恢复、校验和执行记录。", MobileDataArea.City, "$prefix/recovery-runs", MobileGlyph.Operations, adminOnly = true, paged = true, section = "管理员运行控制"),
        ) else modules
    }
}

@Composable
internal fun WorkspaceScreen(viewModel: MainViewModel) {
    var route by rememberSaveable { mutableStateOf<String?>(null) }
    BackHandler(enabled = route != null && route != "city") { route = null }
    when (route) {
        "account" -> AccountCenterScreen(viewModel, onBack = { route = null })
        "city" -> CityWorkspace(viewModel, onBack = { route = null })
        null -> WorkspaceHome(viewModel, onOpen = { route = it })
        else -> {
            val module = MobileDataModules.byId(route.orEmpty(), viewModel.isAdmin)
            if (module == null) WorkspaceHome(viewModel, onOpen = { route = it })
            else DataExplorerScreen(viewModel, module, onBack = { route = null })
        }
    }
}

@Composable
private fun WorkspaceHome(viewModel: MainViewModel, onOpen: (String) -> Unit) {
    val session = viewModel.session ?: return
    val modules = MobileDataModules.visible(viewModel.isAdmin)
    var search by rememberSaveable { mutableStateOf("") }
    val matchedModules = remember(modules, search) {
        if (search.isBlank()) modules else modules.filter { module ->
            listOf(module.title, module.description, module.area.title)
                .any { value -> value.contains(search.trim(), ignoreCase = true) }
        }
    }
    val edge = responsiveEdgePadding()
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.TopCenter) {
        LazyColumn(
            modifier = Modifier.fillMaxWidth().widthIn(max = 900.dp),
            contentPadding = PaddingValues(horizontal = edge, vertical = 16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item { WorkspaceHero(session.user, viewModel.isAdmin) }
            item { WorkspaceQuickAccess(onOpen) }
            item {
                OutlinedTextField(
                    value = search,
                    onValueChange = { search = it },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("搜索功能或数据") },
                    placeholder = { Text("如：订单、风控、模型、城市…") },
                    singleLine = true,
                    trailingIcon = {
                        Text(
                            "${matchedModules.size}",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    },
                )
            }
            MobileDataArea.entries.forEach { area ->
                val areaModules = matchedModules.filter { it.area == area && it.id != MobileDataModules.cityWorlds.id }
                val showCityEntry = area == MobileDataArea.City && (search.isBlank() || listOf("共享城市", "城市模拟", "世界").any { it.contains(search.trim(), ignoreCase = true) })
                if (areaModules.isNotEmpty() || showCityEntry) {
                    item { AreaHeader(area) }
                    if (showCityEntry) {
                        item {
                            ModuleCard(
                                module = MobileDataModules.cityWorlds,
                                onClick = { onOpen("city") },
                                highlight = true,
                            )
                        }
                    }
                    items(areaModules, key = { it.id }) { module ->
                        ModuleCard(module = module, onClick = { onOpen(module.id) })
                    }
                }
            }
            if (matchedModules.isEmpty() && !showCitySearchMatch(search)) {
                item { WorkspaceEmpty("没有匹配的数据模块") }
            }
            item { Spacer(Modifier.height(8.dp)) }
        }
    }
}

private fun showCitySearchMatch(search: String): Boolean = search.isBlank() || listOf("共享城市", "城市模拟", "世界")
    .any { it.contains(search.trim(), ignoreCase = true) }

@Composable
private fun WorkspaceHero(user: UserSummary, isAdmin: Boolean) {
    Card(
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer),
    ) {
        Column(
            Modifier.fillMaxWidth().padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Surface(
                    modifier = Modifier.size(40.dp),
                    color = MaterialTheme.colorScheme.primary,
                    contentColor = MaterialTheme.colorScheme.onPrimary,
                    shape = MaterialTheme.shapes.medium,
                ) {
                    Box(contentAlignment = Alignment.Center) { Icon(Icons.Outlined.Home, null) }
                }
                Column(Modifier.weight(1f)) {
                    Text("移动工作台", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
                    Text(
                        if (isAdmin) "管理、运营与数据预览已按权限归类" else "服务、资产与个人数据集中查看",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.78f),
                    )
                }
                AssistChip(
                    onClick = {},
                    label = { Text(if (isAdmin) "管理员" else "用户") },
                    colors = AssistChipDefaults.assistChipColors(
                        containerColor = MaterialTheme.colorScheme.primary.copy(alpha = 0.12f),
                        labelColor = MaterialTheme.colorScheme.onPrimaryContainer,
                    ),
                )
            }
            HorizontalDivider(color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.14f))
            Text(user.email, style = MaterialTheme.typography.labelLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                "已加密会话 · 账户状态 ${if (user.status == "active") "正常" else user.status}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.72f),
            )
        }
    }
}

@Composable
private fun WorkspaceQuickAccess(onOpen: (String) -> Unit) {
    Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.34f))) {
        Column(Modifier.fillMaxWidth().padding(8.dp)) {
            Text("常用操作", modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
            WorkspaceQuickRow("账户与公告", "查看个人资料、公告并安全退出", MobileGlyph.Notices) { onOpen("account") }
            WorkspaceQuickRow("使用快照", "快速检查当前请求、费用与模型使用", MobileGlyph.Overview) { onOpen("usage-snapshot") }
            WorkspaceQuickRow("共享城市", "进入可参与的城市世界和角色数据", MobileGlyph.City) { onOpen("city") }
        }
    }
}

@Composable
private fun WorkspaceQuickRow(title: String, subtitle: String, glyph: MobileGlyph, onClick: () -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 12.dp, vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(glyph.icon(), null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(20.dp))
        Column(Modifier.weight(1f)) {
            Text(title, fontWeight = FontWeight.SemiBold)
            Text(subtitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Text("查看", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary)
    }
}

@Composable
private fun AreaHeader(area: MobileDataArea) {
    Column(Modifier.padding(top = 10.dp, bottom = 2.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(area.glyph.icon(), null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
            Text(area.title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
        }
        Text(area.description, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun ModuleCard(module: MobileDataModule, onClick: () -> Unit, highlight: Boolean = false) {
    Card(
        modifier = Modifier.fillMaxWidth().clickable(onClick = onClick),
        colors = CardDefaults.cardColors(
            containerColor = if (highlight) MaterialTheme.colorScheme.secondaryContainer.copy(alpha = 0.58f) else MaterialTheme.colorScheme.surface,
        ),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Surface(
                modifier = Modifier.size(40.dp),
                color = if (highlight) MaterialTheme.colorScheme.secondary else MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.6f),
                contentColor = if (highlight) MaterialTheme.colorScheme.onSecondary else MaterialTheme.colorScheme.primary,
                shape = MaterialTheme.shapes.small,
            ) {
                Box(contentAlignment = Alignment.Center) { Icon(module.glyph.icon(), null, modifier = Modifier.size(21.dp)) }
            }
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text(module.title, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
                Text(module.description, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            Text("›", style = MaterialTheme.typography.headlineSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
internal fun DataExplorerScreen(viewModel: MainViewModel, module: MobileDataModule, onBack: () -> Unit) {
    LaunchedEffect(module.id) { viewModel.loadExplorer(module) }
    val state = viewModel.explorer
    val data = state.data?.takeIf { it.module.id == module.id }
    val edge = responsiveEdgePadding()
    var search by rememberSaveable(module.id) { mutableStateOf("") }
    val allRows = data?.let(::explorerRows).orEmpty()
    val allScalars = data?.let { previewScalarItems(it.payload) }.orEmpty()
    val metrics = data?.let { previewMetricEntries(it.payload) }.orEmpty()
    val rows = if (search.isBlank()) allRows else allRows.filter { row ->
        previewEntries(row).any { (field, value) -> "$field ${renderPreviewValue(field, value)}".contains(search, ignoreCase = true) }
    }
    val scalars = if (search.isBlank()) allScalars else allScalars.filter { value ->
        renderPreviewValue(value).contains(search, ignoreCase = true)
    }

    when {
        data == null && state.loading -> WorkspaceLoading("正在加载${module.title}…")
        data == null -> WorkspaceFailure(state.error ?: "无法加载数据", onRetry = { viewModel.loadExplorer(module, force = true) }, onBack = onBack)
        else -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.TopCenter) {
            LazyColumn(
                modifier = Modifier.fillMaxWidth().widthIn(max = 900.dp),
                contentPadding = PaddingValues(horizontal = edge, vertical = 12.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                item {
                    ExplorerHeader(module = module, payload = data, onBack = onBack, onRefresh = { viewModel.loadExplorer(module, force = true) })
                }
                if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
                state.error?.let { item { WorkspaceError(it) } }
                if (metrics.size >= 2) {
                    item { ExplorerMetricGrid(metrics) }
                }
                trendSamples(data.payload).takeIf { it.size > 1 }?.let { samples ->
                    item { TrendPreview(samples) }
                }
                distributionRows(data.payload, "models", "model").takeIf { it.isNotEmpty() }?.let { rows ->
                    item { DistributionPreview(rows, "模型分布") }
                }
                distributionRows(data.payload, "groups", "group_name").takeIf { it.isNotEmpty() }?.let { rows ->
                    item { DistributionPreview(rows, "分组分布") }
                }
                if (allRows.size > 1 || allScalars.size > 1) {
                    item {
                        OutlinedTextField(
                            value = search,
                            onValueChange = { search = it },
                            modifier = Modifier.fillMaxWidth(),
                            label = { Text("筛选已加载数据") },
                            placeholder = { Text("名称、邮箱、状态、模型…") },
                            singleLine = true,
                            trailingIcon = {
                                val shown = if (allRows.isNotEmpty()) rows.size else scalars.size
                                val total = if (allRows.isNotEmpty()) allRows.size else allScalars.size
                                Text("$shown/$total", style = MaterialTheme.typography.labelMedium)
                            },
                        )
                    }
                }
                run {
                    if (rows.isNotEmpty() && hasCollection(payload = data.payload)) {
                        itemsIndexed(rows, key = { index, row -> previewStableKey(row, index) }) { index, row ->
                            ExplorerRecordCard(row, index)
                        }
                    } else if (rows.isNotEmpty()) {
                        item { ExplorerRecordCard(rows.first(), 0, expandedByDefault = true) }
                    } else if (scalars.isNotEmpty()) {
                        itemsIndexed(scalars, key = { index, _ -> "scalar-$index" }) { index, value ->
                            ExplorerScalarCard(value, index)
                        }
                    } else {
                        item { WorkspaceEmpty("当前筛选条件下没有数据") }
                    }
                }
                if (data.hasMore) {
                    item {
                        OutlinedButton(
                            onClick = viewModel::loadMoreExplorer,
                            enabled = !state.loading,
                            modifier = Modifier.fillMaxWidth(),
                        ) {
                            if (state.loading) CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                            else Text("加载更多")
                        }
                    }
                }
                item { Spacer(Modifier.height(12.dp)) }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ExplorerHeader(
    module: MobileDataModule,
    payload: ExplorerPayload,
    onBack: () -> Unit,
    onRefresh: () -> Unit,
) {
    val loadedRows = explorerRows(payload).size.takeIf { it > 0 } ?: previewScalarItems(payload.payload).size
    val collectionSummary = if (hasCollection(payload.payload)) {
        previewTotal(payload.payload)?.let { "已加载 $loadedRows / $it 条" } ?: "已加载 $loadedRows 条"
    } else {
        "数据概览"
    }
    Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer)) {
        Column(Modifier.fillMaxWidth().padding(18.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Surface(
                    modifier = Modifier.size(40.dp),
                    color = MaterialTheme.colorScheme.primary,
                    contentColor = MaterialTheme.colorScheme.onPrimary,
                    shape = MaterialTheme.shapes.small,
                ) { Box(contentAlignment = Alignment.Center) { Icon(module.glyph.icon(), null) } }
                Column(Modifier.weight(1f)) {
                    Text(module.title, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
                    Text(module.description, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.74f))
                }
                IconButton(onClick = onRefresh) { Icon(Icons.Outlined.Refresh, "刷新数据") }
            }
            FlowRow(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(4.dp),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                TextButton(onClick = onBack) { Text("返回工作台") }
                Text(
                    "$collectionSummary · 更新于 ${relativeLoadedTime(payload.loadedAtMillis)}",
                    modifier = Modifier.padding(start = 6.dp, top = 12.dp, bottom = 12.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.68f),
                )
            }
        }
    }
}

@Composable
private fun ExplorerRecordCard(row: JSONObject, index: Int, expandedByDefault: Boolean = false) {
    val key = previewStableKey(row, index)
    var expanded by rememberSaveable(key) { mutableStateOf(expandedByDefault) }
    val title = previewRecordTitle(row, index)
    val entries = previewEntries(row)
    Card {
        Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(title, modifier = Modifier.weight(1f), style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis)
                previewStatus(row)?.let { status -> PreviewStatus(status) }
            }
            if (entries.isEmpty()) {
                Text("没有可安全显示的字段", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            entries.take(if (expanded) entries.size else 4).forEach { (field, value) ->
                PreviewProperty(field, value)
            }
            if (entries.size > 4) {
                TextButton(onClick = { expanded = !expanded }, modifier = Modifier.align(Alignment.End)) {
                    Text(if (expanded) "收起详情" else "查看详情（${entries.size} 项）")
                }
            }
        }
    }
}

@Composable
private fun ExplorerScalarCard(value: Any?, index: Int) {
    Card {
        Row(
            Modifier.fillMaxWidth().padding(16.dp),
            verticalAlignment = Alignment.Top,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                "项目 ${index + 1}",
                modifier = Modifier.weight(0.32f),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                renderPreviewValue(value),
                modifier = Modifier.weight(0.68f),
                style = MaterialTheme.typography.bodyMedium,
                maxLines = 8,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun ExplorerMetricGrid(metrics: List<Pair<String, Any?>>) {
    Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow)) {
        Column(
            modifier = Modifier.fillMaxWidth().padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text("核心指标", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
            metrics.chunked(2).forEach { row ->
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    row.forEach { (field, value) ->
                        Surface(
                            modifier = Modifier.weight(1f),
                            color = MaterialTheme.colorScheme.surface,
                            shape = MaterialTheme.shapes.medium,
                        ) {
                            Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                Text(
                                    prettyFieldName(field),
                                    style = MaterialTheme.typography.labelMedium,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                Text(
                                    renderPreviewValue(field, value),
                                    style = MaterialTheme.typography.titleMedium,
                                    fontWeight = FontWeight.Bold,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                    }
                    if (row.size == 1) Spacer(Modifier.weight(1f))
                }
            }
        }
    }
}

private data class TrendSample(val label: String, val value: Double)

@Composable
private fun TrendPreview(samples: List<TrendSample>) {
    val maxValue = samples.maxOf { it.value }.coerceAtLeast(1.0)
    val minValue = samples.minOf { it.value }
    val primary = MaterialTheme.colorScheme.primary
    val gridLine = MaterialTheme.colorScheme.outline.copy(alpha = 0.22f)
    Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface)) {
        Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
                Column {
                    Text("请求趋势", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
                    Text("当前时间窗口的请求量", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                AssistChip(onClick = {}, label = { Text("峰值 ${formatPreviewNumber(maxValue)}") })
            }
            Canvas(modifier = Modifier.fillMaxWidth().height(116.dp)) {
                val horizontalPadding = 8.dp.toPx()
                val verticalPadding = 12.dp.toPx()
                val width = size.width - horizontalPadding * 2
                val height = size.height - verticalPadding * 2
                val span = (maxValue - minValue).takeIf { it > 0.0 } ?: 1.0
                val path = Path()
                samples.forEachIndexed { index, sample ->
                    val x = horizontalPadding + width * index / (samples.lastIndex.coerceAtLeast(1)).toFloat()
                    val y = verticalPadding + height * (1f - ((sample.value - minValue) / span).toFloat())
                    if (index == 0) path.moveTo(x, y) else path.lineTo(x, y)
                }
                drawLine(
                    color = gridLine,
                    start = Offset(horizontalPadding, size.height - verticalPadding),
                    end = Offset(size.width - horizontalPadding, size.height - verticalPadding),
                    strokeWidth = 1.dp.toPx(),
                )
                drawPath(path, color = primary, style = Stroke(width = 3.dp.toPx(), cap = StrokeCap.Round))
                samples.forEachIndexed { index, sample ->
                    val x = horizontalPadding + width * index / (samples.lastIndex.coerceAtLeast(1)).toFloat()
                    val y = verticalPadding + height * (1f - ((sample.value - minValue) / span).toFloat())
                    drawCircle(primary, radius = 3.5.dp.toPx(), center = Offset(x, y))
                }
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                Text(samples.first().label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(samples.last().label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

@Composable
private fun DistributionPreview(rows: List<Pair<String, Double>>, title: String) {
    val top = rows.sortedByDescending { it.second }.take(5)
    val maximum = top.maxOfOrNull { it.second }?.coerceAtLeast(1.0) ?: 1.0
    Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface)) {
        Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(title, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
            top.forEach { (label, value) ->
                Column(verticalArrangement = Arrangement.spacedBy(5.dp)) {
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
                        Text(label, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(formatPreviewNumber(value), style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.SemiBold)
                    }
                    LinearProgressIndicator(
                        progress = { (value / maximum).toFloat().coerceIn(0f, 1f) },
                        modifier = Modifier.fillMaxWidth().height(6.dp),
                    )
                }
            }
        }
    }
}

private const val NestedPreviewInitialItems = 8
private const val NestedPreviewBatchItems = 16
private const val NestedPreviewMaxDepth = 2

@Composable
private fun PreviewProperty(field: String, value: Any?, depth: Int = 0) {
    when {
        value is JSONObject && depth < NestedPreviewMaxDepth -> PreviewObjectProperty(field, value, depth)
        value is JSONArray && depth < NestedPreviewMaxDepth -> PreviewArrayProperty(field, value, depth)
        else -> PreviewValueLine(field, renderPreviewValue(field, value))
    }
}

@Composable
private fun PreviewValueLine(field: String, renderedValue: String) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(
            prettyFieldName(field),
            modifier = Modifier.weight(0.42f),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            renderedValue,
            modifier = Modifier.weight(0.58f),
            style = MaterialTheme.typography.bodySmall,
            maxLines = 4,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
private fun PreviewObjectProperty(field: String, value: JSONObject, depth: Int) {
    val entries = previewEntries(value)
    var expanded by rememberSaveable(field, depth, "object") { mutableStateOf(false) }
    var visibleCount by rememberSaveable(field, depth, "object-count") { mutableIntStateOf(NestedPreviewInitialItems) }
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(
                prettyFieldName(field),
                modifier = Modifier.weight(0.42f),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Column(Modifier.weight(0.58f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text("${entries.size} 个字段", style = MaterialTheme.typography.bodySmall)
                if (entries.isNotEmpty()) {
                    TextButton(onClick = { expanded = !expanded }) {
                        Text(if (expanded) "收起结构" else "查看结构")
                    }
                }
            }
        }
        if (expanded && entries.isNotEmpty()) {
            Column(
                Modifier.fillMaxWidth().padding(start = 12.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                HorizontalDivider()
                entries.take(visibleCount).forEach { (nestedField, nestedValue) ->
                    PreviewProperty(nestedField, nestedValue, depth + 1)
                }
                if (visibleCount < entries.size) {
                    TextButton(onClick = { visibleCount = (visibleCount + NestedPreviewBatchItems).coerceAtMost(entries.size) }) {
                        Text("继续显示（还剩 ${entries.size - visibleCount} 项）")
                    }
                }
            }
        }
    }
}

@Composable
private fun PreviewArrayProperty(field: String, value: JSONArray, depth: Int) {
    val values = value.previewValues()
    var expanded by rememberSaveable(field, depth, "array") { mutableStateOf(false) }
    var visibleCount by rememberSaveable(field, depth, "array-count") { mutableIntStateOf(NestedPreviewInitialItems) }
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(
                prettyFieldName(field),
                modifier = Modifier.weight(0.42f),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Column(Modifier.weight(0.58f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text("${values.size} 项", style = MaterialTheme.typography.bodySmall)
                if (values.isNotEmpty()) {
                    TextButton(onClick = { expanded = !expanded }) {
                        Text(if (expanded) "收起列表" else "查看列表")
                    }
                }
            }
        }
        if (expanded && values.isNotEmpty()) {
            Column(
                Modifier.fillMaxWidth().padding(start = 12.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                HorizontalDivider()
                values.take(visibleCount).forEachIndexed { index, nestedValue ->
                    PreviewProperty("第 ${index + 1} 项", nestedValue, depth + 1)
                }
                if (visibleCount < values.size) {
                    TextButton(onClick = { visibleCount = (visibleCount + NestedPreviewBatchItems).coerceAtMost(values.size) }) {
                        Text("继续显示（还剩 ${values.size - visibleCount} 项）")
                    }
                }
            }
        }
    }
}

@Composable
private fun PreviewStatus(status: String) {
    val normalized = status.lowercase(Locale.ROOT)
    val positive = normalized in setOf("active", "enabled", "healthy", "success", "normal", "running", "completed", "paid")
    val color = if (positive) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.tertiary
    AssistChip(
        onClick = {},
        label = { Text(prettyStatus(status)) },
        colors = AssistChipDefaults.assistChipColors(
            containerColor = color.copy(alpha = 0.14f),
            labelColor = color,
        ),
    )
}

@Composable
private fun WorkspaceLoading(label: String) {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(14.dp)) {
            CircularProgressIndicator()
            Text(label, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun WorkspaceFailure(message: String, onRetry: () -> Unit, onBack: () -> Unit) {
    Box(Modifier.fillMaxSize().padding(24.dp), contentAlignment = Alignment.Center) {
        Card {
            Column(Modifier.fillMaxWidth().padding(20.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Icon(Icons.Outlined.ErrorOutline, null, tint = MaterialTheme.colorScheme.error, modifier = Modifier.size(30.dp))
                Text("数据暂时不可用", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                Text(message, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    TextButton(onClick = onBack) { Text("返回") }
                    FilledTonalButton(onClick = onRetry) { Text("重试") }
                }
            }
        }
    }
}

@Composable
private fun WorkspaceError(message: String) {
    Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.errorContainer)) {
        Row(Modifier.fillMaxWidth().padding(14.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.WarningAmber, null, tint = MaterialTheme.colorScheme.error)
            Text(message, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onErrorContainer)
        }
    }
}

@Composable
private fun WorkspaceEmpty(message: String) {
    Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.45f))) {
        Column(Modifier.fillMaxWidth().padding(28.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(Icons.Outlined.MoreHoriz, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(message, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun CityWorkspace(viewModel: MainViewModel, onBack: () -> Unit) {
    var selectedWorldID by rememberSaveable { mutableStateOf<Long?>(null) }
    var selectedWorldName by rememberSaveable { mutableStateOf("") }
    var selectedModuleID by rememberSaveable { mutableStateOf<String?>(null) }
    BackHandler(enabled = selectedModuleID != null) { selectedModuleID = null }
    BackHandler(enabled = selectedWorldID != null && selectedModuleID == null) {
        selectedWorldID = null
        selectedWorldName = ""
    }
    BackHandler(enabled = selectedWorldID == null) { onBack() }
    when {
        selectedWorldID == null -> CityWorldPicker(viewModel, onBack) { id, name ->
            selectedWorldID = id
            selectedWorldName = name
        }
        selectedModuleID == null -> CityWorldHub(
            worldID = selectedWorldID ?: return,
            worldName = selectedWorldName,
            isAdmin = viewModel.isAdmin,
            onBack = { selectedWorldID = null },
            onOpen = { selectedModuleID = it },
        )
        else -> {
            val module = MobileDataModules.cityWorldModules(selectedWorldID ?: return, viewModel.isAdmin)
                .firstOrNull { it.id == selectedModuleID }
            if (module == null) selectedModuleID = null
            else DataExplorerScreen(viewModel, module, onBack = { selectedModuleID = null })
        }
    }
}

@Composable
private fun CityWorldPicker(viewModel: MainViewModel, onBack: () -> Unit, onSelect: (Long, String) -> Unit) {
    LaunchedEffect(Unit) { viewModel.loadExplorer(MobileDataModules.cityWorlds) }
    val state = viewModel.explorer
    val data = state.data?.takeIf { it.module.id == MobileDataModules.cityWorlds.id }
    val worlds = data?.let { previewRows(it.payload) }.orEmpty().mapNotNull { row ->
        val id = row.longValue("id") ?: row.longValue("world_id") ?: return@mapNotNull null
        id to (row.stringValue("name") ?: row.stringValue("world_name") ?: "世界 #$id")
    }
    val edge = responsiveEdgePadding()
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.TopCenter) {
        LazyColumn(
            modifier = Modifier.fillMaxWidth().widthIn(max = 900.dp),
            contentPadding = PaddingValues(horizontal = edge, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.secondaryContainer)) {
                    Column(Modifier.fillMaxWidth().padding(18.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text("共享城市世界", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
                        Text("所有成员进入同一实时世界。选择世界后可查看城市、角色与 Agent 状态。", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSecondaryContainer.copy(alpha = 0.74f))
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            TextButton(onClick = onBack) { Text("返回工作台") }
                            TextButton(onClick = { viewModel.loadExplorer(MobileDataModules.cityWorlds, force = true) }) { Text("刷新世界") }
                        }
                    }
                }
            }
            if (state.loading && data == null) item { WorkspaceLoading("正在读取可进入的世界…") }
            state.error?.let { item { WorkspaceError(it) } }
            if (data != null && worlds.isEmpty()) item { WorkspaceEmpty("当前账号尚未加入任何城市世界") }
            items(worlds, key = { it.first }) { (id, name) ->
                Card(modifier = Modifier.fillMaxWidth().clickable { onSelect(id, name) }) {
                    Row(Modifier.fillMaxWidth().padding(17.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        Icon(Icons.Outlined.Home, null, tint = MaterialTheme.colorScheme.primary)
                        Column(Modifier.weight(1f)) {
                            Text(name, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
                            Text("世界编号 $id · 共享实时模拟", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        Text("进入", color = MaterialTheme.colorScheme.primary, style = MaterialTheme.typography.labelLarge)
                    }
                }
            }
        }
    }
}

@Composable
private fun CityWorldHub(worldID: Long, worldName: String, isAdmin: Boolean, onBack: () -> Unit, onOpen: (String) -> Unit) {
    val modules = remember(worldID, isAdmin) { MobileDataModules.cityWorldModules(worldID, isAdmin) }
    var search by rememberSaveable(worldID) { mutableStateOf("") }
    val matchedModules = remember(modules, search) {
        if (search.isBlank()) modules else modules.filter { module ->
            listOf(module.title, module.description, module.section)
                .any { value -> value.contains(search.trim(), ignoreCase = true) }
        }
    }
    val sections = matchedModules.groupBy { it.section.ifBlank { "世界数据" } }
    val edge = responsiveEdgePadding()
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.TopCenter) {
        LazyColumn(
            modifier = Modifier.fillMaxWidth().widthIn(max = 900.dp),
            contentPadding = PaddingValues(horizontal = edge, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer)) {
                    Column(Modifier.fillMaxWidth().padding(18.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(worldName.ifBlank { "城市世界 #$worldID" }, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
                        Text("世界 #$worldID · 数据将来自同一实时模拟实例。", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.75f))
                        TextButton(onClick = onBack, modifier = Modifier.align(Alignment.Start)) { Text("切换世界") }
                    }
                }
            }
            item {
                OutlinedTextField(
                    value = search,
                    onValueChange = { search = it },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("搜索世界数据") },
                    placeholder = { Text("地图、角色、市场、服务、回放…") },
                    singleLine = true,
                    trailingIcon = { Text("${matchedModules.size}", style = MaterialTheme.typography.labelMedium) },
                )
            }
            sections.forEach { (section, sectionModules) ->
                item {
                    Text(
                        section,
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.Bold,
                        modifier = Modifier.padding(top = 8.dp),
                    )
                }
                items(sectionModules, key = { it.id }) { module -> ModuleCard(module, onClick = { onOpen(module.id) }) }
            }
            if (matchedModules.isEmpty()) item { WorkspaceEmpty("没有匹配的世界数据") }
        }
    }
}

private fun MobileGlyph.icon(): ImageVector = when (this) {
    MobileGlyph.Overview -> Icons.Outlined.Home
    MobileGlyph.Usage -> Icons.Outlined.QueryStats
    MobileGlyph.Wallet -> Icons.Outlined.AccountBalanceWallet
    MobileGlyph.City -> Icons.Outlined.Home
    MobileGlyph.Operations -> Icons.Outlined.WarningAmber
    MobileGlyph.Resources -> Icons.Outlined.AccountTree
    MobileGlyph.Commerce -> Icons.Outlined.AccountBalanceWallet
    MobileGlyph.Governance -> Icons.Outlined.Key
    MobileGlyph.People -> Icons.Outlined.People
    MobileGlyph.Notices -> Icons.Outlined.Notifications
}

@Composable
private fun responsiveEdgePadding() = with(LocalDensity.current) {
    if (LocalWindowInfo.current.containerSize.width.toDp() >= 600.dp) 28.dp else 16.dp
}

private fun previewRows(payload: Any): List<JSONObject> = when (payload) {
    is JSONArray -> payload.objects()
    is JSONObject -> payload.collectionMember()?.objects() ?: listOf(payload)
    else -> emptyList()
}

private fun explorerRows(data: ExplorerPayload): List<JSONObject> = previewRows(data.payload)

private fun previewScalarItems(payload: Any): List<Any?> {
    val collection = when (payload) {
        is JSONArray -> payload
        is JSONObject -> payload.collectionMember()
        else -> null
    } ?: return emptyList()
    return collection.previewValues().filter { value -> value !is JSONObject }
}

private fun hasCollection(payload: Any): Boolean = payload is JSONArray || (payload is JSONObject && payload.collectionMember() != null)

internal fun previewMetricEntries(payload: Any): List<Pair<String, Any?>> {
    val objectPayload = payload as? JSONObject ?: return emptyList()
    val entries = buildList {
        val keys = objectPayload.keys()
        while (keys.hasNext()) {
            val key = keys.next()
            add(key to objectPayload.opt(key))
        }
    }
    return selectPreviewMetricEntries(entries)
}

internal fun selectPreviewMetricEntries(entries: Iterable<Pair<String, Any?>>): List<Pair<String, Any?>> {
    val valuesByField = entries.associate { (field, value) -> field.lowercase(Locale.ROOT) to value }
    return previewMetricFields.mapNotNull { field ->
        val value = valuesByField[field]
        if (value is Number || value is String && value.toDoubleOrNull() != null) field to value else null
    }
}

private fun trendSamples(payload: Any): List<TrendSample> {
    val points = (payload as? JSONObject)?.optJSONArray("trend")?.objects().orEmpty()
    return points.mapNotNull { point ->
        val value = point.doubleValue("requests") ?: point.doubleValue("total_requests") ?: return@mapNotNull null
        TrendSample(point.stringValue("date") ?: point.stringValue("timestamp") ?: "", value)
    }
}

private fun distributionRows(payload: Any, member: String, labelField: String): List<Pair<String, Double>> {
    val entries = (payload as? JSONObject)?.optJSONArray(member)?.objects().orEmpty()
    return entries.mapNotNull { entry ->
        val label = entry.stringValue(labelField) ?: entry.stringValue("name") ?: entry.stringValue("code") ?: return@mapNotNull null
        val value = entry.doubleValue("requests") ?: entry.doubleValue("total_tokens") ?: entry.doubleValue("actual_cost") ?: return@mapNotNull null
        label to value
    }
}

private fun previewTotal(payload: Any): Long? {
    val objectPayload = payload as? JSONObject ?: return null
    val direct = objectPayload.longValue("total") ?: objectPayload.longValue("total_count")
    if (direct != null) return direct
    val pagination = objectPayload.optJSONObject("pagination") ?: return null
    return pagination.longValue("total") ?: pagination.longValue("total_count")
}

private fun JSONObject.collectionMember(): JSONArray? = listOf("items", "data", "results", "records", "list", "worlds", "assignments")
    .firstNotNullOfOrNull { key -> opt(key) as? JSONArray }

private fun JSONArray.objects(): List<JSONObject> = buildList {
    for (index in 0 until length()) (opt(index) as? JSONObject)?.let(::add)
}

private fun JSONArray.previewValues(): List<Any?> = buildList {
    for (index in 0 until length()) add(opt(index))
}

private fun previewEntries(obj: JSONObject): List<Pair<String, Any?>> {
    val entries = buildList {
        val keys = obj.keys()
        while (keys.hasNext()) {
            val key = keys.next()
            if (!isSensitiveField(key)) add(key to obj.opt(key))
        }
    }
    val preferred = listOf("status", "name", "title", "email", "username", "code", "platform", "model", "created_at", "updated_at", "message", "id")
    return entries.sortedWith(compareBy({ preferred.indexOf(it.first).let { index -> if (index < 0) Int.MAX_VALUE else index } }, { it.first }))
}

private fun previewRecordTitle(row: JSONObject, index: Int): String = listOf("name", "title", "email", "username", "model", "code", "world_name", "id")
    .firstNotNullOfOrNull { row.stringValue(it) }
    ?: "记录 #${index + 1}"

private fun previewStableKey(row: JSONObject, index: Int): String = row.stringValue("id") ?: row.stringValue("uuid") ?: row.stringValue("code") ?: "row-$index"

private fun previewStatus(row: JSONObject): String? = row.stringValue("status") ?: row.stringValue("state") ?: row.stringValue("health")

private fun JSONObject.stringValue(key: String): String? = when (val value: Any? = opt(key)) {
    null, JSONObject.NULL -> null
    is String -> value.trim().takeIf(String::isNotBlank)
    is Number, is Boolean -> value.toString()
    else -> null
}

private fun JSONObject.longValue(key: String): Long? = when (val value: Any? = opt(key)) {
    is Number -> value.toLong()
    is String -> value.toLongOrNull()
    else -> null
}

private fun JSONObject.doubleValue(key: String): Double? = when (val value: Any? = opt(key)) {
    is Number -> value.toDouble()
    is String -> value.toDoubleOrNull()
    else -> null
}

internal fun isSensitiveField(field: String): Boolean {
    val normalized = field.lowercase(Locale.ROOT)
    if (normalized in safeTokenMetricFields) return false
    return normalized in exactSensitiveFields ||
        normalized.contains("password") ||
        normalized.contains("secret") ||
        normalized.contains("credential") ||
        normalized.contains("authorization") ||
        normalized.contains("cookie") ||
        normalized.contains("api_key") ||
        normalized.contains("apikey") ||
        normalized.contains("private_key") ||
        normalized.contains("privatekey") ||
        normalized.contains("access_key") ||
        normalized.contains("accesskey") ||
        normalized.contains("refresh_token") ||
        normalized.contains("access_token") ||
        normalized.contains("id_token") ||
        normalized.contains("session_token") ||
        normalized.contains("bearer_token") ||
        normalized.endsWith("_token") ||
        normalized.startsWith("token_")
}

internal fun renderPreviewValue(field: String, value: Any?): String {
    val normalizedField = field.lowercase(Locale.ROOT)
    return when {
        value is String && isPreviewTimestampField(normalizedField) -> formatPreviewTimestamp(value)
        value is Number && isPreviewTimestampField(normalizedField) -> formatPreviewEpoch(value.toLong())
        value is Number && normalizedField.endsWith("_ms") -> "${formatPreviewNumber(value.toDouble())} ms"
        value is Number && normalizedField.endsWith("_bytes") -> formatPreviewBytes(value.toLong())
        else -> renderPreviewValue(value)
    }
}

private fun renderPreviewValue(value: Any?): String = when (value) {
    null, JSONObject.NULL -> "—"
    is Boolean -> if (value) "是" else "否"
    is JSONArray -> "${value.length()} 项"
    is JSONObject -> "${previewEntries(value).size} 个字段"
    is Number -> when {
        value.toDouble() % 1.0 == 0.0 -> String.format(Locale.US, "%,d", value.toLong())
        else -> String.format(Locale.US, "%,.4f", value.toDouble()).trimEnd('0').trimEnd('.')
    }
    else -> redactPreviewText(value.toString())
}

private fun isPreviewTimestampField(field: String): Boolean = field in previewTimestampFields ||
    field.endsWith("_at") || field.endsWith("_time") || field.endsWith("_timestamp")

private fun formatPreviewTimestamp(value: String): String {
    val safe = redactPreviewText(value)
    if (safe == "已隐藏" || safe == "—") return safe
    return if (previewTimestampPattern.containsMatchIn(safe)) safe.replace('T', ' ').take(19) else safe
}

private fun formatPreviewEpoch(value: Long): String = runCatching {
    val epochMillis = if (value in 1..99_999_999_999L) value * 1_000L else value
    java.time.Instant.ofEpochMilli(epochMillis)
        .atZone(java.time.ZoneId.systemDefault())
        .format(java.time.format.DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss", Locale.ROOT))
}.getOrElse { formatPreviewNumber(value.toDouble()) }

private fun formatPreviewBytes(value: Long): String {
    if (value < 1_024L) return "$value B"
    val units = listOf("KB", "MB", "GB", "TB")
    var amount = value.toDouble()
    var index = -1
    while (amount >= 1_024.0 && index < units.lastIndex) {
        amount /= 1_024.0
        index += 1
    }
    val display = String.format(Locale.US, "%.2f", amount).trimEnd('0').trimEnd('.')
    return "$display ${units[index]}"
}

internal fun redactPreviewText(value: String): String {
    val compact = value.replace(Regex("\\s+"), " ").trim()
    if (compact.isBlank()) return "—"
    val lower = compact.lowercase(Locale.ROOT)
    val containsSensitiveQuery = sensitiveQueryKeys.any { key -> lower.contains("$key=") || lower.contains("$key%3d") }
    val looksCredential = credentialValuePattern.matches(compact) || jwtValuePattern.matches(compact)
    if (looksCredential || containsSensitiveQuery || lower.startsWith("bearer ") || "-----begin " in lower) return "已隐藏"
    return compact.take(280)
}

private val exactSensitiveFields = setOf(
    "password", "secret", "token", "authorization", "cookie", "credential", "private_key", "access_key",
    "client_secret", "webhook_secret", "signing_key", "session_key",
)

private val safeTokenMetricFields = setOf(
    "tokens", "token_count", "total_tokens", "input_tokens", "output_tokens", "cached_tokens", "reasoning_tokens",
    "today_tokens", "last_tokens", "token_usage", "token_stats", "token_limit", "token_quota", "token_rate",
)

private val previewMetricFields = listOf(
    "total_requests", "today_requests", "request_count", "requests",
    "total_tokens", "today_tokens", "token_count", "total_actual_cost", "total_cost", "actual_cost",
    "average_duration_ms", "p99_latency_ms", "p95_latency_ms", "qps", "tps", "error_rate",
    "active_accounts", "total_accounts", "available_accounts", "total_users", "queue_depth",
)

private val previewTimestampFields = setOf(
    "timestamp", "time", "created", "updated", "occurred", "expires", "last_seen", "checked", "generated",
)

private val previewTimestampPattern = Regex("^\\d{4}-\\d{2}-\\d{2}[T ]\\d{2}:\\d{2}")

private val sensitiveQueryKeys = setOf("token", "access_token", "refresh_token", "api_key", "apikey", "secret", "password", "authorization")
private val credentialValuePattern = Regex("^(?:sk|at)-[A-Za-z0-9_-]{16,}$")
private val jwtValuePattern = Regex("^[A-Za-z0-9_-]{8,}\\.[A-Za-z0-9_-]{8,}\\.[A-Za-z0-9_-]{8,}$")

private fun prettyFieldName(key: String): String = fieldLabels[key] ?: key.replace('_', ' ')
    .replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.ROOT) else it.toString() }

private fun prettyStatus(status: String): String = statusLabels[status.lowercase(Locale.ROOT)] ?: status.replace('_', ' ')

private fun relativeLoadedTime(timestamp: Long): String {
    val seconds = ((System.currentTimeMillis() - timestamp) / 1_000).coerceAtLeast(0)
    return if (seconds < 60) "刚刚" else "${seconds / 60} 分钟前"
}

private fun formatPreviewNumber(value: Double): String = if (value % 1.0 == 0.0) {
    String.format(Locale.US, "%,d", value.toLong())
} else {
    String.format(Locale.US, "%,.2f", value).trimEnd('0').trimEnd('.')
}

private val fieldLabels = mapOf(
    "id" to "编号",
    "status" to "状态",
    "state" to "状态",
    "health" to "健康状态",
    "name" to "名称",
    "title" to "标题",
    "email" to "邮箱",
    "username" to "用户名",
    "code" to "代码",
    "platform" to "平台",
    "model" to "模型",
    "group_name" to "分组",
    "created_at" to "创建时间",
    "updated_at" to "更新时间",
    "generated_at" to "生成时间",
    "start_date" to "开始日期",
    "end_date" to "结束日期",
    "granularity" to "统计粒度",
    "models" to "模型分布",
    "trend" to "趋势数据",
    "total" to "总数",
    "count" to "数量",
    "message" to "说明",
    "error" to "错误",
    "error_message" to "错误信息",
    "available" to "可用",
    "enabled" to "已启用",
    "currency_code" to "货币代码",
    "currency_name" to "货币名称",
    "world_id" to "世界编号",
    "world_name" to "世界名称",
    "request_id" to "请求编号",
    "account_id" to "账号编号",
    "user_id" to "用户编号",
    "group_id" to "分组编号",
    "api_key_id" to "密钥编号",
    "total_requests" to "总请求数",
    "today_requests" to "今日请求数",
    "request_count" to "请求数",
    "success_requests" to "成功请求数",
    "failed_requests" to "失败请求数",
    "error_count" to "错误数",
    "total_tokens" to "总 Token",
    "today_tokens" to "今日 Token",
    "input_tokens" to "输入 Token",
    "output_tokens" to "输出 Token",
    "cached_tokens" to "缓存 Token",
    "reasoning_tokens" to "推理 Token",
    "total_cost" to "总消耗",
    "actual_cost" to "实际消耗",
    "total_actual_cost" to "总实际消耗",
    "today_cost" to "今日消耗",
    "today_actual_cost" to "今日实际消耗",
    "standard_cost" to "标准消耗",
    "currency" to "货币",
    "balance" to "余额",
    "frozen_balance" to "冻结余额",
    "available_balance" to "可用余额",
    "available_units" to "可用最小单位",
    "reserved_units" to "预约最小单位",
    "scale" to "精度倍率",
    "symbol" to "符号",
    "quota" to "配额",
    "used_quota" to "已用配额",
    "rate_multiplier" to "费率倍率",
    "concurrency" to "并发上限",
    "current_concurrency" to "当前并发",
    "queue_depth" to "队列深度",
    "qps" to "QPS",
    "tps" to "TPS",
    "average_duration_ms" to "平均耗时",
    "duration_ms" to "耗时",
    "latency_ms" to "延迟",
    "p50_latency_ms" to "P50 延迟",
    "p90_latency_ms" to "P90 延迟",
    "p95_latency_ms" to "P95 延迟",
    "p99_latency_ms" to "P99 延迟",
    "ttft_ms" to "首字延迟",
    "error_rate" to "错误率",
    "active_accounts" to "活跃账号",
    "available_accounts" to "可用账号",
    "total_accounts" to "账号总数",
    "total_users" to "用户总数",
    "account_type" to "账号类型",
    "account_name" to "账号名称",
    "schedulable" to "可调度",
    "capacity" to "容量",
    "capacity_used" to "已用容量",
    "capacity_available" to "可用容量",
    "limit" to "限制",
    "retry_count" to "重试次数",
    "retry_after" to "重试时间",
    "source" to "来源",
    "target" to "目标",
    "reason" to "原因",
    "details" to "详情",
    "metadata" to "元数据",
    "description" to "说明",
    "version" to "版本",
    "environment" to "环境",
    "provider" to "服务商",
    "provider_name" to "服务商名称",
    "channel" to "渠道",
    "channel_name" to "渠道名称",
    "endpoint" to "端点",
    "path" to "路径",
    "method" to "方法",
    "http_status" to "HTTP 状态",
    "status_code" to "状态码",
    "ip" to "IP 地址",
    "ip_location" to "IP 归属",
    "country" to "国家/地区",
    "region" to "地区",
    "city" to "城市",
    "last_seen_at" to "最近出现",
    "expires_at" to "到期时间",
    "checked_at" to "检查时间",
    "started_at" to "开始时间",
    "finished_at" to "完成时间",
    "occurred_at" to "发生时间",
    "world_time" to "世界时间",
    "population" to "人口",
    "market" to "市场",
    "price" to "价格",
    "quantity" to "数量",
    "position" to "位置",
    "coordinates" to "坐标",
    "actor" to "角色",
    "actor_id" to "角色编号",
    "character" to "角色",
    "agent" to "Agent",
    "event_type" to "事件类型",
    "event_count" to "事件数",
    "rule" to "规则",
    "case" to "案件",
    "severity" to "严重程度",
    "priority" to "优先级",
    "progress" to "进度",
    "percentage" to "百分比",
    "file_size_bytes" to "文件大小",
    "memory_bytes" to "内存占用",
)

private val statusLabels = mapOf(
    "active" to "正常",
    "enabled" to "已启用",
    "healthy" to "健康",
    "success" to "成功",
    "normal" to "正常",
    "running" to "运行中",
    "completed" to "已完成",
    "paid" to "已支付",
    "inactive" to "已停用",
    "disabled" to "已禁用",
    "failed" to "失败",
    "error" to "错误",
    "warning" to "警告",
    "degraded" to "降级",
    "offline" to "离线",
    "unavailable" to "不可用",
    "pending" to "处理中",
    "queued" to "排队中",
    "processing" to "处理中",
    "paused" to "已暂停",
    "suspended" to "已暂停",
    "cancelled" to "已取消",
    "canceled" to "已取消",
    "expired" to "已过期",
    "draft" to "草稿",
    "archived" to "已归档",
)
