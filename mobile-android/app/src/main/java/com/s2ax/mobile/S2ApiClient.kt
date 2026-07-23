package com.s2ax.mobile

import android.content.Context
import androidx.core.content.edit
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import org.json.JSONTokener
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URI
import java.net.URLEncoder
import java.net.URL
import java.nio.charset.StandardCharsets
import java.util.Locale
import java.util.UUID

data class UserSummary(
    val id: Long,
    val email: String,
    val username: String,
    val role: String,
    val balance: Double,
    val frozenBalance: Double,
    val concurrency: Int,
    val status: String,
)

data class AuthSession(
    val baseUrl: String,
    val accessToken: String,
    val refreshToken: String?,
    val expiresAtMillis: Long,
    val user: UserSummary,
)

sealed interface LoginResult {
    data class Authenticated(val session: AuthSession) : LoginResult
    data class RequiresTotp(val tempToken: String, val maskedEmail: String?) : LoginResult
}

data class UsageOverview(
    val totalRequests: Long,
    val totalTokens: Long,
    val totalCost: Double,
    val todayRequests: Long,
    val todayTokens: Long,
    val todayCost: Double,
    val averageDurationMs: Long,
)

data class Metric(val label: String, val value: String)

data class AdminOverview(val metrics: List<Metric>)

data class ApiKeySummary(
    val id: Long,
    val name: String,
    val keyPrefix: String,
    val status: String,
    val groupName: String?,
    val quota: Double?,
    val usedQuota: Double?,
)

data class UsageEntry(
    val id: Long,
    val occurredAt: String,
    val model: String,
    val group: String,
    val totalTokens: Long,
    val cost: Double,
    val status: String,
)

data class AccountSummary(
    val id: Long,
    val name: String,
    val platform: String,
    val type: String,
    val status: String,
    val schedulable: Boolean,
    val concurrency: Int,
    val currentConcurrency: Int,
    val groupNames: List<String>,
    val errorMessage: String?,
)

data class AdminUserSummary(
    val id: Long,
    val email: String,
    val username: String,
    val role: String,
    val status: String,
    val balance: Double,
    val concurrency: Int,
)

data class GroupSummary(
    val id: Long,
    val name: String,
    val platform: String,
    val status: String,
    val rateMultiplier: Double,
    val accountCount: Int?,
)

data class WalletSummary(
    val code: String,
    val name: String,
    val symbol: String,
    val scale: Int,
    val availableUnits: Long,
    val reservedUnits: Long,
)

data class AllocationSummary(
    val accountName: String,
    val platform: String,
    val accountType: String,
    val status: String,
    val concurrency: Int,
    val totalTokens: Long,
)

data class AnnouncementSummary(
    val id: Long,
    val title: String,
    val content: String,
    val createdAt: String,
    val isRead: Boolean,
)

data class AllocationPolicySummary(
    val id: Long,
    val userEmail: String,
    val groupName: String,
    val desiredCount: Int,
    val activeCount: Int,
    val shortage: Int,
    val status: String,
    val autoReplenish: Boolean,
)

data class VirtualCurrencySummary(
    val id: Long,
    val code: String,
    val name: String,
    val symbol: String,
    val description: String,
    val scale: Int,
    val status: String,
)

data class CurrencyReconciliationSummary(
    val mismatchCount: Long,
    val walletCount: Long,
    val checkedAt: String,
)

data class AdminAnnouncementSummary(
    val id: Long,
    val title: String,
    val content: String,
    val status: String,
    val notifyMode: String,
    val createdAt: String,
)

class ApiException(val statusCode: Int, message: String) : IllegalStateException(message)

/** Strict, testable normalization so a saved endpoint cannot silently downgrade transport security. */
object ApiUrl {
    fun normalize(input: String): String {
        val uri = try {
            URI(input.trim())
        } catch (error: Exception) {
            throw IllegalArgumentException("服务地址格式不正确", error)
        }
        require(uri.scheme.equals("https", ignoreCase = true)) { "移动端只允许 HTTPS 服务地址" }
        require(!uri.host.isNullOrBlank()) { "服务地址缺少主机名" }
        require(uri.userInfo.isNullOrBlank() && uri.query.isNullOrBlank() && uri.fragment.isNullOrBlank()) {
            "服务地址不能包含账号、查询参数或锚点"
        }

        val basePath = uri.path.orEmpty().trimEnd('/')
        val apiPath = when {
            basePath.isEmpty() -> "/api/v1"
            basePath.endsWith("/api/v1") -> basePath
            else -> "$basePath/api/v1"
        }
        return URI(uri.scheme.lowercase(Locale.ROOT), null, uri.host, uri.port, apiPath, null, null)
            .toString()
            .trimEnd('/')
    }
}

// ponytail: AndroidX wraps Keystore encryption here; use an encrypted DataStore only if multi-process access or migrations are needed.
@Suppress("DEPRECATION")
class SessionStore(context: Context) {
    private val preferences = EncryptedSharedPreferences.create(
        context,
        "s2ax_mobile_session",
        MasterKey.Builder(context).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build(),
        EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
        EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
    )

    fun baseUrl(): String? = preferences.getString(KEY_BASE_URL, null)

    fun read(): AuthSession? {
        val baseUrl = baseUrl() ?: return null
        val accessToken = preferences.getString(KEY_ACCESS_TOKEN, null)?.takeIf(String::isNotBlank) ?: return null
        val user = preferences.getString(KEY_USER, null)?.let(::userFromStored) ?: return null
        return AuthSession(
            baseUrl = baseUrl,
            accessToken = accessToken,
            refreshToken = preferences.getString(KEY_REFRESH_TOKEN, null)?.takeIf(String::isNotBlank),
            expiresAtMillis = preferences.getLong(KEY_EXPIRES_AT, 0L),
            user = user,
        )
    }

    fun saveBaseUrl(baseUrl: String) {
        preferences.edit { putString(KEY_BASE_URL, baseUrl) }
    }

    fun save(session: AuthSession) {
        preferences.edit {
            putString(KEY_BASE_URL, session.baseUrl)
            putString(KEY_ACCESS_TOKEN, session.accessToken)
            putString(KEY_REFRESH_TOKEN, session.refreshToken)
            putLong(KEY_EXPIRES_AT, session.expiresAtMillis)
            putString(KEY_USER, userToStored(session.user))
        }
    }

    fun updateTokens(accessToken: String, refreshToken: String?, expiresAtMillis: Long) {
        preferences.edit {
            putString(KEY_ACCESS_TOKEN, accessToken)
            putString(KEY_REFRESH_TOKEN, refreshToken)
            putLong(KEY_EXPIRES_AT, expiresAtMillis)
        }
    }

    fun updateUser(user: UserSummary) {
        preferences.edit { putString(KEY_USER, userToStored(user)) }
    }

    fun clearSession() {
        preferences.edit {
            remove(KEY_ACCESS_TOKEN)
            remove(KEY_REFRESH_TOKEN)
            remove(KEY_EXPIRES_AT)
            remove(KEY_USER)
        }
    }

    private fun userToStored(user: UserSummary) = JSONObject()
        .put("id", user.id)
        .put("email", user.email)
        .put("username", user.username)
        .put("role", user.role)
        .put("balance", user.balance)
        .put("frozen_balance", user.frozenBalance)
        .put("concurrency", user.concurrency)
        .put("status", user.status)
        .toString()

    private fun userFromStored(raw: String): UserSummary? = runCatching { userFromJson(JSONObject(raw)) }.getOrNull()

    private companion object {
        const val KEY_BASE_URL = "base_url"
        const val KEY_ACCESS_TOKEN = "access_token"
        const val KEY_REFRESH_TOKEN = "refresh_token"
        const val KEY_EXPIRES_AT = "expires_at"
        const val KEY_USER = "user"
    }
}

class S2ApiClient(private val sessionStore: SessionStore) {
    private val refreshMutex = Mutex()

    suspend fun login(baseUrlInput: String, email: String, password: String): LoginResult {
        val baseUrl = ApiUrl.normalize(baseUrlInput)
        sessionStore.saveBaseUrl(baseUrl)
        val response = request(
            method = "POST",
            path = "/auth/login",
            body = JSONObject().put("email", email.trim()).put("password", password),
            authenticated = false,
            refreshOnUnauthorized = false,
        ).asObject()
        if (response.optBoolean("requires_2fa", false)) {
            val temporaryToken = response.optText("temp_token")
                ?: throw ApiException(200, "服务器未返回二次验证令牌")
            return LoginResult.RequiresTotp(temporaryToken, response.optText("user_email_masked"))
        }
        return LoginResult.Authenticated(saveAuthResponse(baseUrl, response))
    }

    suspend fun completeTotp(tempToken: String, code: String): AuthSession {
        val baseUrl = sessionStore.baseUrl() ?: throw ApiException(-1, "请先填写服务地址")
        val response = request(
            method = "POST",
            path = "/auth/login/2fa",
            body = JSONObject().put("temp_token", tempToken).put("totp_code", code.trim()),
            authenticated = false,
            refreshOnUnauthorized = false,
        ).asObject()
        return saveAuthResponse(baseUrl, response)
    }

    suspend fun logout() {
        val refreshToken = sessionStore.read()?.refreshToken
        if (!refreshToken.isNullOrBlank()) {
            runCatching {
                request("POST", "/auth/logout", JSONObject().put("refresh_token", refreshToken), authenticated = false)
            }
        }
        sessionStore.clearSession()
    }

    suspend fun currentUser(): UserSummary {
        val user = userFromJson(request("GET", "/auth/me").asObject())
        sessionStore.updateUser(user)
        return user
    }

    suspend fun userUsageOverview(): UsageOverview {
        val json = request("GET", "/usage/dashboard/stats").asObject()
        return UsageOverview(
            totalRequests = json.optLongSafe("total_requests"),
            totalTokens = json.optLongSafe("total_tokens"),
            totalCost = json.optDoubleSafe("total_actual_cost", json.optDoubleSafe("total_cost")),
            todayRequests = json.optLongSafe("today_requests"),
            todayTokens = json.optLongSafe("today_tokens"),
            todayCost = json.optDoubleSafe("today_actual_cost", json.optDoubleSafe("today_cost")),
            averageDurationMs = json.optLongSafe("average_duration_ms"),
        )
    }

    suspend fun updateMyProfile(username: String): UserSummary {
        val user = userFromJson(
            request("PUT", "/user", JSONObject().put("username", username.trim())).asObject(),
        )
        sessionStore.updateUser(user)
        return user
    }

    suspend fun changeMyPassword(oldPassword: String, newPassword: String): String =
        request(
            "PUT",
            "/user/password",
            JSONObject().put("old_password", oldPassword).put("new_password", newPassword),
        ).asObjectOrNull()?.optText("message") ?: "密码已更新"

    suspend fun redeemCode(code: String): String =
        request("POST", "/redeem", JSONObject().put("code", code.trim())).asObjectOrNull()?.optText("message") ?: "兑换成功"

    suspend fun transferAffiliateQuota(): String =
        request("POST", "/user/aff/transfer", JSONObject()).asObjectOrNull()?.optText("message") ?: "返利已转入余额"

    suspend fun adminOverview(): AdminOverview {
        val json = request("GET", "/admin/dashboard/stats").asObject()
        val metricDefinitions = listOf(
            "用户" to listOf("total_users", "users"),
            "API Key" to listOf("total_api_keys", "api_keys"),
            "账号" to listOf("total_accounts", "accounts"),
            "活跃账号" to listOf("active_accounts"),
            "请求" to listOf("total_requests"),
            "Token" to listOf("total_tokens"),
            "消耗" to listOf("total_actual_cost", "total_cost"),
        )
        return AdminOverview(metricDefinitions.mapNotNull { (label, names) ->
            names.firstNotNullOfOrNull { name -> json.optNumberString(name) }?.let { Metric(label, it) }
        })
    }

    /**
     * Reads a route declared by the native module registry. The UI never accepts
     * a user-entered path, so this keeps the mobile data browser inside the API
     * surface deliberately shipped with the app.
     */
    suspend fun previewPayload(path: String, query: Map<String, String> = emptyMap()): Any =
        request("GET", path, query = query)

    suspend fun listKeys(page: Int = 1, search: String = ""): List<ApiKeySummary> =
        pagedObjects(request("GET", "/keys", query = mapOf("page" to page.toString(), "page_size" to "30", "search" to search)))
            .map(::apiKeyFromJson)

    suspend fun createKey(name: String, groupId: Long?): Pair<ApiKeySummary, String?> {
        val body = JSONObject().put("name", name.trim())
        if (groupId != null) body.put("group_id", groupId)
        val json = request("POST", "/keys", body).asObject()
        val secret = json.optText("key") ?: json.optText("api_key") ?: json.optText("secret")
        return apiKeyFromJson(json) to secret
    }

    suspend fun updateKeyStatus(id: Long, status: String) {
        request("PUT", "/keys/$id", JSONObject().put("status", status))
    }

    suspend fun deleteKey(id: Long) {
        request("DELETE", "/keys/$id")
    }

    suspend fun listUsage(page: Int = 1): List<UsageEntry> =
        pagedObjects(request("GET", "/usage", query = mapOf("page" to page.toString(), "page_size" to "40")))
            .map(::usageFromJson)

    suspend fun listWallets(): List<WalletSummary> = arrayObjects(request("GET", "/user/currencies")).map(::walletFromJson)

    suspend fun listMyAllocations(): List<AllocationSummary> {
        val json = request("GET", "/account-allocations").asObject()
        return json.optJSONArray("assignments").jsonObjects().map(::allocationFromJson)
    }

    suspend fun listAnnouncements(): List<AnnouncementSummary> =
        arrayObjects(request("GET", "/announcements")).map(::announcementFromJson)

    suspend fun markAnnouncementRead(id: Long) {
        request("POST", "/announcements/$id/read", JSONObject())
    }

    suspend fun markAllAnnouncementsRead() {
        request("POST", "/announcements/read-all", JSONObject())
    }

    suspend fun listAccounts(page: Int = 1, search: String = ""): List<AccountSummary> =
        pagedObjects(request("GET", "/admin/accounts", query = mapOf("page" to page.toString(), "page_size" to "30", "search" to search)))
            .map(::accountFromJson)

    suspend fun accountAction(id: Long, action: AccountAction): String {
        val response = when (action) {
            AccountAction.Test -> request("POST", "/admin/accounts/$id/test", JSONObject())
            AccountAction.Refresh -> request("POST", "/admin/accounts/$id/refresh", JSONObject())
            AccountAction.Recover -> request("POST", "/admin/accounts/$id/recover-state", JSONObject())
            AccountAction.ClearError -> request("POST", "/admin/accounts/$id/clear-error", JSONObject())
            is AccountAction.SetStatus -> request("PUT", "/admin/accounts/$id", JSONObject().put("status", action.status))
        }
        return response.asObjectOrNull()?.optText("message") ?: when (action) {
            AccountAction.Test -> "测试请求已完成"
            AccountAction.Refresh -> "账号刷新已完成"
            AccountAction.Recover -> "账号状态已恢复"
            AccountAction.ClearError -> "错误状态已清理"
            is AccountAction.SetStatus -> "账号状态已更新"
        }
    }

    suspend fun listAdminUsers(page: Int = 1, search: String = ""): List<AdminUserSummary> =
        pagedObjects(request("GET", "/admin/users", query = mapOf("page" to page.toString(), "page_size" to "30", "search" to search)))
            .map(::adminUserFromJson)

    suspend fun updateAdminUserStatus(id: Long, status: String) {
        request("PUT", "/admin/users/$id", JSONObject().put("status", status))
    }

    suspend fun adjustUserBalance(id: Long, amount: Double, operation: String, notes: String) {
        request(
            "POST",
            "/admin/users/$id/balance",
            JSONObject().put("balance", amount).put("operation", operation).put("notes", notes),
        )
    }

    suspend fun listGroups(): List<GroupSummary> =
        arrayOrPagedObjects(request("GET", "/admin/groups")).map(::groupFromJson)

    suspend fun updateGroupStatus(id: Long, status: String) {
        request("PUT", "/admin/groups/$id", JSONObject().put("status", status))
    }

    suspend fun listAllocationPolicies(): List<AllocationPolicySummary> =
        pagedObjects(request("GET", "/admin/account-allocations/policies", query = mapOf("page" to "1", "page_size" to "40")))
            .map(::policyFromJson)

    suspend fun toggleAllocationPolicy(id: Long, enabled: Boolean) {
        request("POST", "/admin/account-allocations/policies/$id/status", JSONObject().put("enabled", enabled))
    }

    suspend fun reconcileAllocationPolicy(id: Long): String {
        val result = request("POST", "/admin/account-allocations/policies/$id/reconcile", JSONObject()).asObject()
        return "已补齐：${result.optLongSafe("assigned_count")}；仍缺：${result.optLongSafe("shortage")}"
    }

    suspend fun listVirtualCurrencies(): List<VirtualCurrencySummary> =
        arrayObjects(request("GET", "/admin/currencies", query = mapOf("include_disabled" to "true")))
            .map(::virtualCurrencyFromJson)

    suspend fun createVirtualCurrency(
        code: String,
        name: String,
        symbol: String,
        scale: Int,
        description: String,
    ) {
        request(
            "POST",
            "/admin/currencies",
            JSONObject()
                .put("code", code.trim())
                .put("name", name.trim())
                .put("symbol", symbol.trim())
                .put("scale", scale)
                .put("description", description.trim()),
        )
    }

    suspend fun setVirtualCurrencyStatus(id: Long, status: String) {
        request("POST", "/admin/currencies/$id/status", JSONObject().put("status", status))
    }

    suspend fun enableCurrencyForAllUsers(id: Long): Int =
        request("POST", "/admin/currencies/$id/enable-for-all-users", JSONObject())
            .asObject()
            .optLongSafe("group_count")
            .toInt()

    suspend fun expireCurrencyHolds(id: Long): Int =
        request("POST", "/admin/currencies/$id/holds/expire", JSONObject(), query = mapOf("limit" to "100"))
            .asObject()
            .optLongSafe("expired")
            .toInt()

    suspend fun reconcileCurrency(id: Long): CurrencyReconciliationSummary {
        val json = request("GET", "/admin/currencies/$id/reconciliation", query = mapOf("limit" to "20")).asObject()
        return CurrencyReconciliationSummary(
            mismatchCount = json.optLongSafe("mismatch_count"),
            walletCount = json.optLongSafe("wallet_count"),
            checkedAt = json.optText("checked_at").orEmpty(),
        )
    }

    suspend fun adjustVirtualCurrency(
        code: String,
        userId: Long,
        amountUnits: Long,
        reason: String,
    ) {
        request(
            method = "POST",
            path = "/admin/currencies/${URLEncoder.encode(code, "UTF-8")}/adjustments",
            body = JSONObject()
                .put("user_id", userId)
                .put("amount_units", amountUnits)
                .put("reason", reason.trim()),
            extraHeaders = mapOf("Idempotency-Key" to "mobile-currency-adjust-${UUID.randomUUID()}"),
        )
    }

    suspend fun listAdminAnnouncements(page: Int = 1, search: String = ""): List<AdminAnnouncementSummary> =
        pagedObjects(request("GET", "/admin/announcements", query = mapOf("page" to page.toString(), "page_size" to "30", "search" to search)))
            .map(::adminAnnouncementFromJson)

    suspend fun createAdminAnnouncement(title: String, content: String, status: String, notifyMode: String) {
        request(
            "POST",
            "/admin/announcements",
            JSONObject()
                .put("title", title.trim())
                .put("content", content.trim())
                .put("status", status)
                .put("notify_mode", notifyMode)
                .put("targeting", JSONObject()),
        )
    }

    suspend fun updateAdminAnnouncementStatus(id: Long, status: String) {
        request("PUT", "/admin/announcements/$id", JSONObject().put("status", status))
    }

    suspend fun deleteAdminAnnouncement(id: Long) {
        request("DELETE", "/admin/announcements/$id")
    }

    private fun saveAuthResponse(baseUrl: String, response: JSONObject): AuthSession {
        val accessToken = response.optText("access_token") ?: throw ApiException(200, "服务器未返回访问令牌")
        val session = AuthSession(
            baseUrl = baseUrl,
            accessToken = accessToken,
            refreshToken = response.optText("refresh_token"),
            expiresAtMillis = System.currentTimeMillis() + response.optLongSafe("expires_in") * 1000L,
            user = userFromJson(response.optJSONObject("user") ?: throw ApiException(200, "服务器未返回用户信息")),
        )
        sessionStore.save(session)
        return session
    }

    private suspend fun request(
        method: String,
        path: String,
        body: JSONObject? = null,
        query: Map<String, String> = emptyMap(),
        authenticated: Boolean = true,
        refreshOnUnauthorized: Boolean = true,
        extraHeaders: Map<String, String> = emptyMap(),
    ): Any {
        val baseUrl = sessionStore.baseUrl() ?: throw ApiException(-1, "请先填写服务地址")
        val tokenUsed = if (authenticated) sessionStore.read()?.accessToken else null
        val response = execute(baseUrl, method, path, body, query, tokenUsed, extraHeaders)
        if (response.statusCode == HttpURLConnection.HTTP_UNAUTHORIZED && authenticated && refreshOnUnauthorized) {
            if (refreshAfterUnauthorized(tokenUsed)) {
                return request(method, path, body, query, authenticated, refreshOnUnauthorized = false, extraHeaders = extraHeaders)
            }
        }
        return unwrap(response)
    }

    private suspend fun refreshAfterUnauthorized(failedToken: String?): Boolean = refreshMutex.withLock {
        val active = sessionStore.read() ?: return false
        if (active.accessToken != failedToken) return true
        val refreshToken = active.refreshToken ?: return false
        val response = execute(
            active.baseUrl,
            "POST",
            "/auth/refresh",
            JSONObject().put("refresh_token", refreshToken),
            emptyMap(),
            null,
            emptyMap(),
        )
        return try {
            val data = unwrap(response).asObject()
            val accessToken = data.optText("access_token") ?: return false
            sessionStore.updateTokens(
                accessToken = accessToken,
                refreshToken = data.optText("refresh_token") ?: refreshToken,
                expiresAtMillis = System.currentTimeMillis() + data.optLongSafe("expires_in") * 1000L,
            )
            true
        } catch (_: ApiException) {
            sessionStore.clearSession()
            false
        }
    }

    private suspend fun execute(
        baseUrl: String,
        method: String,
        path: String,
        body: JSONObject?,
        query: Map<String, String>,
        token: String?,
        extraHeaders: Map<String, String>,
    ): RawResponse = withContext(Dispatchers.IO) {
        try {
            val requestUrl = URL("$baseUrl${path.ensureLeadingSlash()}${query.asQueryString()}")
            val connection = (requestUrl.openConnection() as HttpURLConnection).apply {
                requestMethod = method
                connectTimeout = 15_000
                readTimeout = 30_000
                useCaches = false
                setRequestProperty("Accept", "application/json")
                setRequestProperty("Accept-Language", "zh-CN")
                setRequestProperty("Content-Type", "application/json; charset=utf-8")
                setRequestProperty("X-Requested-With", "S2AX-Mobile")
                if (path.startsWith("/admin/")) setRequestProperty("X-Admin-UI-Request", "1")
                if (token != null) setRequestProperty("Authorization", "Bearer $token")
                extraHeaders.forEach { (name, value) -> setRequestProperty(name, value) }
                if (body != null) {
                    val bytes = body.toString().toByteArray(StandardCharsets.UTF_8)
                    doOutput = true
                    setFixedLengthStreamingMode(bytes.size)
                    outputStream.use { it.write(bytes) }
                }
            }
            try {
                val status = connection.responseCode
                val stream = if (status >= 400) connection.errorStream else connection.inputStream
                val text = stream?.bufferedReader(StandardCharsets.UTF_8)?.use { it.readText() }.orEmpty()
                RawResponse(status, text)
            } finally {
                connection.disconnect()
            }
        } catch (error: IOException) {
            throw ApiException(-1, "无法连接服务器，请检查地址和网络")
        }
    }

    private fun unwrap(response: RawResponse): Any {
        val payload = parsePayload(response.body)
        if (response.statusCode !in 200..299) {
            throw ApiException(response.statusCode, payload.errorMessage() ?: "请求失败（HTTP ${response.statusCode}）")
        }
        if (payload is JSONObject && payload.has("code")) {
            if (payload.optInt("code", -1) != 0) {
                throw ApiException(response.statusCode, payload.optText("message") ?: "服务器拒绝了请求")
            }
            return payload.opt("data") ?: JSONObject()
        }
        return payload
    }

    private data class RawResponse(val statusCode: Int, val body: String)
}

sealed interface AccountAction {
    data object Test : AccountAction
    data object Refresh : AccountAction
    data object Recover : AccountAction
    data object ClearError : AccountAction
    data class SetStatus(val status: String) : AccountAction
}

private fun String.ensureLeadingSlash() = if (startsWith('/')) this else "/$this"

private fun Map<String, String>.asQueryString(): String {
    val entries = entries.filter { it.value.isNotBlank() }
    if (entries.isEmpty()) return ""
    return entries.joinToString(prefix = "?", separator = "&") { (key, value) ->
        "${URLEncoder.encode(key, "UTF-8")}=${URLEncoder.encode(value, "UTF-8")}"
    }
}

private fun parsePayload(text: String): Any {
    if (text.isBlank()) return JSONObject()
    return try {
        JSONTokener(text).nextValue()
    } catch (_: Exception) {
        JSONObject().put("message", text.take(300))
    }
}

private fun Any.errorMessage(): String? = when (this) {
    is JSONObject -> optText("message") ?: optText("detail") ?: optText("error")
    else -> null
}

private fun Any.asObject(): JSONObject = this as? JSONObject ?: throw ApiException(200, "服务器返回了意外的数据格式")
private fun Any.asObjectOrNull(): JSONObject? = this as? JSONObject

private fun JSONObject.optText(key: String): String? = if (has(key) && !isNull(key)) {
    optString(key, "").trim().takeIf(String::isNotEmpty)
} else {
    null
}

private fun JSONObject.optLongSafe(key: String, default: Long = 0L): Long {
    val value = opt(key)
    return (value as? Number)?.toLong() ?: (value as? String)?.toLongOrNull() ?: default
}

private fun JSONObject.optDoubleSafe(key: String, default: Double = 0.0): Double {
    val value = opt(key)
    return (value as? Number)?.toDouble() ?: (value as? String)?.toDoubleOrNull() ?: default
}

private fun JSONObject.optNumberString(key: String): String? {
    val value = opt(key)
    return when {
        value is Number && value.toDouble() % 1.0 == 0.0 -> String.format(Locale.US, "%,d", value.toLong())
        value is Number -> String.format(Locale.US, "%.4f", value.toDouble())
        value is String -> value.takeIf(String::isNotBlank)
        else -> null
    }
}

private fun JSONArray?.jsonObjects(): List<JSONObject> = buildList {
    if (this@jsonObjects == null) return@buildList
    for (index in 0 until length()) (opt(index) as? JSONObject)?.let(::add)
}

private fun pagedObjects(payload: Any): List<JSONObject> = when (payload) {
    is JSONObject -> payload.optJSONArray("items").jsonObjects()
    is JSONArray -> payload.jsonObjects()
    else -> emptyList()
}

private fun arrayObjects(payload: Any): List<JSONObject> = when (payload) {
    is JSONArray -> payload.jsonObjects()
    is JSONObject -> payload.optJSONArray("items").jsonObjects()
    else -> emptyList()
}

private fun arrayOrPagedObjects(payload: Any) = arrayObjects(payload)

private fun userFromJson(json: JSONObject) = UserSummary(
    id = json.optLongSafe("id"),
    email = json.optText("email") ?: "—",
    username = json.optText("username") ?: json.optText("email") ?: "—",
    role = json.optText("role") ?: "user",
    balance = json.optDoubleSafe("balance"),
    frozenBalance = json.optDoubleSafe("frozen_balance"),
    concurrency = json.optLongSafe("concurrency").toInt(),
    status = json.optText("status") ?: "active",
)

private fun apiKeyFromJson(json: JSONObject) = ApiKeySummary(
    id = json.optLongSafe("id"),
    name = json.optText("name") ?: "未命名密钥",
    keyPrefix = json.optText("key_prefix") ?: json.optText("key")?.take(12) ?: "已隐藏",
    status = json.optText("status") ?: "active",
    groupName = json.optJSONObject("group")?.optText("name") ?: json.optText("group_name"),
    quota = json.takeIf { it.has("quota") && !it.isNull("quota") }?.optDoubleSafe("quota"),
    usedQuota = json.takeIf { it.has("used_quota") && !it.isNull("used_quota") }?.optDoubleSafe("used_quota"),
)

private fun usageFromJson(json: JSONObject) = UsageEntry(
    id = json.optLongSafe("id"),
    occurredAt = json.optText("created_at") ?: json.optText("request_time") ?: "—",
    model = json.optText("model") ?: json.optText("requested_model") ?: "—",
    group = json.optText("group_name") ?: json.optJSONObject("group")?.optText("name") ?: "—",
    totalTokens = json.optLongSafe("total_tokens"),
    cost = json.optDoubleSafe("actual_cost", json.optDoubleSafe("cost")),
    status = json.optText("status") ?: if (json.optBoolean("success", true)) "success" else "error",
)

private fun accountFromJson(json: JSONObject): AccountSummary {
    val groups = json.optJSONArray("groups").jsonObjects().mapNotNull { it.optText("name") }
    return AccountSummary(
        id = json.optLongSafe("id"),
        name = json.optText("name") ?: "未命名账号",
        platform = json.optText("platform") ?: "—",
        type = json.optText("type") ?: "—",
        status = json.optText("status") ?: "inactive",
        schedulable = json.optBoolean("schedulable", true),
        concurrency = json.optLongSafe("concurrency").toInt(),
        currentConcurrency = json.optLongSafe("current_concurrency").toInt(),
        groupNames = groups,
        errorMessage = json.optText("error_message"),
    )
}

private fun adminUserFromJson(json: JSONObject) = AdminUserSummary(
    id = json.optLongSafe("id"),
    email = json.optText("email") ?: "—",
    username = json.optText("username") ?: "—",
    role = json.optText("role") ?: "user",
    status = json.optText("status") ?: "active",
    balance = json.optDoubleSafe("balance"),
    concurrency = json.optLongSafe("concurrency").toInt(),
)

private fun groupFromJson(json: JSONObject) = GroupSummary(
    id = json.optLongSafe("id"),
    name = json.optText("name") ?: "未命名分组",
    platform = json.optText("platform") ?: "—",
    status = json.optText("status") ?: "inactive",
    rateMultiplier = json.optDoubleSafe("rate_multiplier", 1.0),
    accountCount = if (json.has("account_count")) json.optLongSafe("account_count").toInt() else null,
)

private fun walletFromJson(json: JSONObject) = WalletSummary(
    code = json.optText("currency_code") ?: "—",
    name = json.optText("currency_name") ?: "未命名货币",
    symbol = json.optText("currency_symbol") ?: "",
    scale = json.optLongSafe("currency_scale", 1).toInt().coerceAtLeast(1),
    availableUnits = json.optLongSafe("available_units"),
    reservedUnits = json.optLongSafe("reserved_units"),
)

private fun allocationFromJson(json: JSONObject) = AllocationSummary(
    accountName = json.optText("account_name") ?: "—",
    platform = json.optText("platform") ?: "—",
    accountType = json.optText("account_type") ?: "—",
    status = json.optText("status") ?: "—",
    concurrency = json.optLongSafe("capacity")
        .takeIf { it > 0 }?.toInt() ?: json.optJSONObject("capacity")?.optLongSafe("concurrency")?.toInt() ?: 0,
    totalTokens = json.optJSONObject("usage")?.optLongSafe("total_tokens") ?: 0L,
)

private fun announcementFromJson(json: JSONObject) = AnnouncementSummary(
    id = json.optLongSafe("id"),
    title = json.optText("title") ?: "公告",
    content = json.optText("content") ?: json.optText("content_md") ?: "",
    createdAt = json.optText("created_at") ?: "—",
    isRead = json.optBoolean("is_read", false),
)

private fun policyFromJson(json: JSONObject) = AllocationPolicySummary(
    id = json.optLongSafe("id"),
    userEmail = json.optText("user_email") ?: "—",
    groupName = json.optText("group_name") ?: "—",
    desiredCount = json.optLongSafe("desired_count").toInt(),
    activeCount = json.optLongSafe("active_assignment_count").toInt(),
    shortage = json.optLongSafe("shortage").toInt(),
    status = json.optText("status") ?: "disabled",
    autoReplenish = json.optBoolean("auto_replenish", false),
)

private fun virtualCurrencyFromJson(json: JSONObject) = VirtualCurrencySummary(
    id = json.optLongSafe("id"),
    code = json.optText("code") ?: "—",
    name = json.optText("name") ?: "未命名货币",
    symbol = json.optText("symbol") ?: "",
    description = json.optText("description") ?: "",
    scale = json.optLongSafe("scale", 1).toInt().coerceAtLeast(1),
    status = json.optText("status") ?: "disabled",
)

private fun adminAnnouncementFromJson(json: JSONObject) = AdminAnnouncementSummary(
    id = json.optLongSafe("id"),
    title = json.optText("title") ?: "公告",
    content = json.optText("content") ?: "",
    status = json.optText("status") ?: "draft",
    notifyMode = json.optText("notify_mode") ?: "silent",
    createdAt = json.optText("created_at") ?: "—",
)
