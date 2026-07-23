package com.s2ax.mobile

import android.app.Application
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import org.json.JSONArray
import org.json.JSONObject

data class LoadState<T>(
    val data: T? = null,
    val loading: Boolean = false,
    val error: String? = null,
)

data class TotpChallenge(val tempToken: String, val maskedEmail: String?)

data class HomeData(
    val profile: UserSummary,
    val usage: UsageOverview,
    val wallets: List<WalletSummary>,
    val allocations: List<AllocationSummary>,
    val admin: AdminOverview?,
)

class MainViewModel(application: Application) : AndroidViewModel(application) {
    private val sessionStore = SessionStore(application)
    private val api = S2ApiClient(sessionStore)

    var session by mutableStateOf(sessionStore.read())
        private set
    var booting by mutableStateOf(true)
        private set
    var authLoading by mutableStateOf(false)
        private set
    var authError by mutableStateOf<String?>(null)
        private set
    var totpChallenge by mutableStateOf<TotpChallenge?>(null)
        private set
    var notice by mutableStateOf<String?>(null)
        private set
    var newlyCreatedKey by mutableStateOf<String?>(null)
        private set

    var home by mutableStateOf(LoadState<HomeData>())
        private set
    var keys by mutableStateOf(LoadState<List<ApiKeySummary>>())
        private set
    var usage by mutableStateOf(LoadState<List<UsageEntry>>())
        private set
    var accounts by mutableStateOf(LoadState<List<AccountSummary>>())
        private set
    var users by mutableStateOf(LoadState<List<AdminUserSummary>>())
        private set
    var groups by mutableStateOf(LoadState<List<GroupSummary>>())
        private set
    var policies by mutableStateOf(LoadState<List<AllocationPolicySummary>>())
        private set
    var announcements by mutableStateOf(LoadState<List<AnnouncementSummary>>())
        private set
    var currencies by mutableStateOf(LoadState<List<VirtualCurrencySummary>>())
        private set
    var adminAnnouncements by mutableStateOf(LoadState<List<AdminAnnouncementSummary>>())
        private set
    var explorer by mutableStateOf(LoadState<ExplorerPayload>())
        private set

    private var keySearch = ""
    private var accountSearch = ""
    private var userSearch = ""
    private var adminAnnouncementSearch = ""

    init {
        restoreSession()
    }

    val isAdmin: Boolean
        get() = session?.user?.role == "admin"

    val savedBaseUrl: String?
        get() = sessionStore.baseUrl()

    fun dismissNotice() {
        notice = null
    }

    fun dismissNewKey() {
        newlyCreatedKey = null
    }

    fun signIn(baseUrl: String, email: String, password: String) {
        if (authLoading) return
        authLoading = true
        authError = null
        viewModelScope.launch {
            try {
                when (val result = api.login(baseUrl, email, password)) {
                    is LoginResult.Authenticated -> {
                        session = result.session
                        totpChallenge = null
                        loadHome(force = true)
                    }
                    is LoginResult.RequiresTotp -> {
                        totpChallenge = TotpChallenge(result.tempToken, result.maskedEmail)
                    }
                }
            } catch (error: Throwable) {
                authError = error.userMessage()
            } finally {
                authLoading = false
            }
        }
    }

    fun completeTotp(code: String) {
        val challenge = totpChallenge ?: return
        if (authLoading) return
        authLoading = true
        authError = null
        viewModelScope.launch {
            try {
                session = api.completeTotp(challenge.tempToken, code)
                totpChallenge = null
                loadHome(force = true)
            } catch (error: Throwable) {
                authError = error.userMessage()
            } finally {
                authLoading = false
            }
        }
    }

    fun cancelTotp() {
        totpChallenge = null
        authError = null
    }

    fun signOut() {
        viewModelScope.launch {
            api.logout()
            session = null
            clearContent()
        }
    }

    fun loadHome(force: Boolean = false) {
        if (session == null || (home.loading || (home.data != null && !force))) return
        val retained = home.data
        home = LoadState(data = retained, loading = true)
        viewModelScope.launch {
            try {
                val profile = api.currentUser()
                session = sessionStore.read()
                val loaded = coroutineScope {
                    val usageRequest = async { api.userUsageOverview() }
                    val walletRequest = async { optional { api.listWallets() }.orEmpty() }
                    val allocationRequest = async { optional { api.listMyAllocations() }.orEmpty() }
                    val adminRequest = async { if (profile.role == "admin") optional { api.adminOverview() } else null }
                    HomeData(profile, usageRequest.await(), walletRequest.await(), allocationRequest.await(), adminRequest.await())
                }
                home = LoadState(data = loaded)
            } catch (error: Throwable) {
                home = LoadState(data = retained, error = error.userMessage())
            }
        }
    }

    fun updateMyProfile(username: String) {
        if (username.trim().isBlank()) {
            notice = "用户名不能为空"
            return
        }
        runAction("资料已更新") {
            api.updateMyProfile(username)
            session = sessionStore.read()
            loadHome(force = true)
            "资料已更新"
        }
    }

    fun changeMyPassword(oldPassword: String, newPassword: String) {
        if (oldPassword.isBlank() || newPassword.length < 8) {
            notice = "请输入当前密码，并设置至少 8 位的新密码"
            return
        }
        runAction("密码已更新") { api.changeMyPassword(oldPassword, newPassword) }
    }

    fun redeemCode(code: String) {
        if (code.isBlank()) {
            notice = "请输入兑换码"
            return
        }
        runAction("兑换成功") {
            val message = api.redeemCode(code)
            loadHome(force = true)
            message
        }
    }

    fun transferAffiliateQuota() {
        runAction("返利已转入余额") {
            val message = api.transferAffiliateQuota()
            loadHome(force = true)
            message
        }
    }

    fun loadExplorer(module: MobileDataModule, force: Boolean = false) {
        val existing = explorer.data?.takeIf { it.module.id == module.id }
        if (explorer.loading || (existing != null && !force)) return
        explorer = LoadState(data = existing, loading = true)
        viewModelScope.launch {
            try {
                explorer = LoadState(
                    data = ExplorerPayload(
                        module = module,
                        payload = api.previewPayload(module.path, module.requestQuery(page = 1)),
                        page = 1,
                    ),
                )
            } catch (error: Throwable) {
                if (error is CancellationException) throw error
                explorer = LoadState(data = existing, error = error.userMessage())
            }
        }
    }

    fun loadMoreExplorer() {
        val current = explorer.data ?: return
        if (!current.module.paged || explorer.loading || !current.hasMore) return
        explorer = LoadState(data = current, loading = true)
        viewModelScope.launch {
            try {
                val nextPage = current.page + 1
                val next = api.previewPayload(current.module.path, current.module.requestQuery(nextPage))
                explorer = LoadState(
                    data = current.copy(
                        payload = mergeExplorerPayload(current.payload, next),
                        page = nextPage,
                    ),
                )
            } catch (error: Throwable) {
                if (error is CancellationException) throw error
                explorer = LoadState(data = current, error = error.userMessage())
            }
        }
    }

    fun refreshExplorer() {
        explorer.data?.module?.let { loadExplorer(it, force = true) }
    }

    fun loadKeys(search: String = keySearch, force: Boolean = false) {
        val changed = search != keySearch
        keySearch = search
        if (keys.loading || (keys.data != null && !force && !changed)) return
        loadList(
            current = keys,
            update = { keys = it },
            block = { api.listKeys(search = keySearch) },
        )
    }

    fun createKey(name: String, groupIdText: String) {
        val groupId = groupIdText.trim().takeIf(String::isNotEmpty)?.toLongOrNull()
        viewModelScope.launch {
            try {
                val (created, secret) = api.createKey(name, groupId)
                keys = LoadState(data = listOf(created) + keys.data.orEmpty())
                newlyCreatedKey = secret
                notice = if (secret == null) "已创建 API Key" else "请立即保存新 API Key"
            } catch (error: Throwable) {
                if (error is CancellationException) throw error
                notice = error.userMessage()
            }
        }
    }

    fun updateKeyStatus(key: ApiKeySummary) {
        val next = if (key.status == "active") "inactive" else "active"
        runAction("API Key 状态已更新") {
            api.updateKeyStatus(key.id, next)
            loadKeys(force = true)
            "API Key 已${if (next == "active") "启用" else "停用"}"
        }
    }

    fun deleteKey(key: ApiKeySummary) {
        runAction("API Key 已删除") {
            api.deleteKey(key.id)
            keys = LoadState(data = keys.data.orEmpty().filterNot { it.id == key.id })
            "API Key 已删除"
        }
    }

    fun loadUsage(force: Boolean = false) {
        if (usage.loading || (usage.data != null && !force)) return
        loadList(usage, { usage = it }) { api.listUsage() }
    }

    fun loadAccounts(search: String = accountSearch, force: Boolean = false) {
        val changed = search != accountSearch
        accountSearch = search
        if (!isAdmin || accounts.loading || (accounts.data != null && !force && !changed)) return
        loadList(accounts, { accounts = it }) { api.listAccounts(search = accountSearch) }
    }

    fun accountAction(account: AccountSummary, action: AccountAction) {
        runAction("账号操作已完成") {
            val message = api.accountAction(account.id, action)
            loadAccounts(force = true)
            message
        }
    }

    fun loadUsers(search: String = userSearch, force: Boolean = false) {
        val changed = search != userSearch
        userSearch = search
        if (!isAdmin || users.loading || (users.data != null && !force && !changed)) return
        loadList(users, { users = it }) { api.listAdminUsers(search = userSearch) }
    }

    fun toggleUser(user: AdminUserSummary) {
        val status = if (user.status == "active") "disabled" else "active"
        runAction("用户状态已更新") {
            api.updateAdminUserStatus(user.id, status)
            loadUsers(force = true)
            "${user.email} 已${if (status == "active") "启用" else "停用"}"
        }
    }

    fun adjustBalance(user: AdminUserSummary, amountText: String, operation: String, notes: String) {
        val amount = amountText.toDoubleOrNull()
        if (amount == null || amount <= 0.0) {
            notice = "请输入大于 0 的金额"
            return
        }
        runAction("余额已调整") {
            api.adjustUserBalance(user.id, amount, operation, notes)
            loadUsers(force = true)
            "${user.email} 的余额已更新"
        }
    }

    fun loadGroups(force: Boolean = false) {
        if (!isAdmin || groups.loading || (groups.data != null && !force)) return
        loadList(groups, { groups = it }) { api.listGroups() }
    }

    fun toggleGroup(group: GroupSummary) {
        val status = if (group.status == "active") "inactive" else "active"
        runAction("分组状态已更新") {
            api.updateGroupStatus(group.id, status)
            loadGroups(force = true)
            "${group.name} 已${if (status == "active") "启用" else "停用"}"
        }
    }

    fun loadPolicies(force: Boolean = false) {
        if (!isAdmin || policies.loading || (policies.data != null && !force)) return
        loadList(policies, { policies = it }) { api.listAllocationPolicies() }
    }

    fun togglePolicy(policy: AllocationPolicySummary) {
        val enabled = policy.status != "active"
        runAction("分配策略状态已更新") {
            api.toggleAllocationPolicy(policy.id, enabled)
            loadPolicies(force = true)
            "策略已${if (enabled) "启用" else "停用"}"
        }
    }

    fun reconcilePolicy(policy: AllocationPolicySummary) {
        runAction("分配策略已执行") {
            val result = api.reconcileAllocationPolicy(policy.id)
            loadPolicies(force = true)
            result
        }
    }

    fun loadCurrencies(force: Boolean = false) {
        if (!isAdmin || currencies.loading || (currencies.data != null && !force)) return
        loadList(currencies, { currencies = it }) { api.listVirtualCurrencies() }
    }

    fun createCurrency(code: String, name: String, symbol: String, scaleText: String, description: String) {
        val scale = scaleText.toIntOrNull()
        if (code.isBlank() || name.isBlank() || scale == null || scale < 1) {
            notice = "请填写货币代码、名称和大于 0 的精度"
            return
        }
        runAction("虚拟货币已创建") {
            api.createVirtualCurrency(code, name, symbol, scale, description)
            loadCurrencies(force = true)
            "${name.trim()} 已创建"
        }
    }

    fun toggleCurrency(currency: VirtualCurrencySummary) {
        val status = if (currency.status == "active") "disabled" else "active"
        runAction("虚拟货币状态已更新") {
            api.setVirtualCurrencyStatus(currency.id, status)
            loadCurrencies(force = true)
            "${currency.name} 已${if (status == "active") "启用" else "停用"}"
        }
    }

    fun enableCurrencyForAllUsers(currency: VirtualCurrencySummary) {
        runAction("货币已向全部用户启用") {
            val groups = api.enableCurrencyForAllUsers(currency.id)
            "已在 $groups 个可用分组启用 ${currency.name}"
        }
    }

    fun expireCurrencyHolds(currency: VirtualCurrencySummary) {
        runAction("冻结清理已完成") {
            val expired = api.expireCurrencyHolds(currency.id)
            "已释放 $expired 笔过期冻结"
        }
    }

    fun reconcileCurrency(currency: VirtualCurrencySummary) {
        runAction("货币对账已完成") {
            val result = api.reconcileCurrency(currency.id)
            "已核对 ${result.walletCount} 个钱包，差异 ${result.mismatchCount} 个"
        }
    }

    fun adjustVirtualCurrency(user: AdminUserSummary, currency: VirtualCurrencySummary, amountUnits: Long, reason: String) {
        if (amountUnits == 0L) {
            notice = "调整数量不能为 0"
            return
        }
        runAction("虚拟货币余额已调整") {
            api.adjustVirtualCurrency(currency.code, user.id, amountUnits, reason)
            "${user.email} 的 ${currency.name} 已调整"
        }
    }

    fun loadAdminAnnouncements(search: String = adminAnnouncementSearch, force: Boolean = false) {
        val changed = search != adminAnnouncementSearch
        adminAnnouncementSearch = search
        if (!isAdmin || adminAnnouncements.loading || (adminAnnouncements.data != null && !force && !changed)) return
        loadList(adminAnnouncements, { adminAnnouncements = it }) { api.listAdminAnnouncements(search = adminAnnouncementSearch) }
    }

    fun createAdminAnnouncement(title: String, content: String, status: String, notifyMode: String) {
        if (title.isBlank() || content.isBlank()) {
            notice = "公告标题和内容不能为空"
            return
        }
        runAction("公告已创建") {
            api.createAdminAnnouncement(title, content, status, notifyMode)
            loadAdminAnnouncements(force = true)
            "公告已${if (status == "active") "发布" else "保存为草稿"}"
        }
    }

    fun updateAdminAnnouncementStatus(item: AdminAnnouncementSummary, status: String) {
        runAction("公告状态已更新") {
            api.updateAdminAnnouncementStatus(item.id, status)
            loadAdminAnnouncements(force = true)
            "${item.title} 已更新"
        }
    }

    fun deleteAdminAnnouncement(item: AdminAnnouncementSummary) {
        runAction("公告已删除") {
            api.deleteAdminAnnouncement(item.id)
            adminAnnouncements = LoadState(data = adminAnnouncements.data.orEmpty().filterNot { it.id == item.id })
            "${item.title} 已删除"
        }
    }

    fun loadAnnouncements(force: Boolean = false) {
        if (announcements.loading || (announcements.data != null && !force)) return
        loadList(announcements, { announcements = it }) { api.listAnnouncements() }
    }

    fun markAnnouncementRead(item: AnnouncementSummary) {
        if (item.isRead) return
        runAction("公告已标记为已读") {
            api.markAnnouncementRead(item.id)
            announcements = LoadState(data = announcements.data.orEmpty().map {
                if (it.id == item.id) it.copy(isRead = true) else it
            })
            "公告已标记为已读"
        }
    }

    fun markAllAnnouncementsRead() {
        if (announcements.data.orEmpty().none { !it.isRead }) return
        runAction("全部公告已标记为已读") {
            api.markAllAnnouncementsRead()
            announcements = LoadState(data = announcements.data.orEmpty().map { it.copy(isRead = true) })
            "全部公告已标记为已读"
        }
    }

    private fun restoreSession() {
        val saved = session
        if (saved == null) {
            booting = false
            return
        }
        viewModelScope.launch {
            try {
                api.currentUser()
                session = sessionStore.read()
                loadHome(force = true)
            } catch (error: Throwable) {
                if (error is CancellationException) throw error
                sessionStore.clearSession()
                session = null
                authError = "登录已失效，请重新登录"
            } finally {
                booting = false
            }
        }
    }

    private fun clearContent() {
        home = LoadState()
        keys = LoadState()
        usage = LoadState()
        accounts = LoadState()
        users = LoadState()
        groups = LoadState()
        policies = LoadState()
        announcements = LoadState()
        currencies = LoadState()
        adminAnnouncements = LoadState()
        explorer = LoadState()
    }

    private fun <T> loadList(
        current: LoadState<T>,
        update: (LoadState<T>) -> Unit,
        block: suspend () -> T,
    ) {
        if (current.loading) return
        update(LoadState(data = current.data, loading = true))
        viewModelScope.launch {
            try {
                update(LoadState(data = block()))
            } catch (error: Throwable) {
                if (error is CancellationException) throw error
                update(LoadState(data = current.data, error = error.userMessage()))
            }
        }
    }

    private fun runAction(fallback: String, action: suspend () -> String) {
        viewModelScope.launch {
            try {
                notice = action().ifBlank { fallback }
            } catch (error: Throwable) {
                if (error is CancellationException) throw error
                notice = error.userMessage()
            }
        }
    }

    private suspend fun <T> optional(block: suspend () -> T): T? = try {
        block()
    } catch (error: ApiException) {
        if (error.statusCode == 404) null else throw error
    }
}

private fun Throwable.userMessage(): String = when (this) {
    is ApiException -> message ?: "请求失败"
    else -> "操作失败，请稍后重试"
}

private fun mergeExplorerPayload(current: Any, next: Any): Any {
    if (current !is JSONObject || next !is JSONObject) return next
    val member = listOf("items", "data", "results", "records", "list").firstOrNull {
        current.opt(it) is JSONArray && next.opt(it) is JSONArray
    } ?: return next
    val merged = JSONObject(current.toString())
    val rows = JSONArray()
    listOf(current.optJSONArray(member), next.optJSONArray(member)).forEach { source ->
        if (source == null) return@forEach
        for (index in 0 until source.length()) rows.put(source.opt(index))
    }
    merged.put(member, rows)
    next.keys().forEach { key -> if (key != member) merged.put(key, next.opt(key)) }
    return merged
}
