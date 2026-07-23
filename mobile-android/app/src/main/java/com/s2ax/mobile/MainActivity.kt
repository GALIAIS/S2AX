package com.s2ax.mobile

import android.os.Bundle
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountBalanceWallet
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Autorenew
import androidx.compose.material.icons.outlined.Block
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.ManageAccounts
import androidx.compose.material.icons.outlined.MoreHoriz
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.People
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.PowerSettingsNew
import androidx.compose.material.icons.outlined.QueryStats
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CenterAlignedTopAppBar
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.ScrollableTabRow
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import java.text.NumberFormat
import java.math.BigDecimal
import java.math.BigInteger
import java.util.Locale

class MainActivity : ComponentActivity() {
    private val viewModel by viewModels<MainViewModel>()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // ponytail: only a locally launched debug activity may opt into capture; release builds always retain FLAG_SECURE.
        if (BuildConfig.DEBUG && intent.getBooleanExtra(EXTRA_ALLOW_SCREEN_CAPTURE, false)) {
            window.clearFlags(WindowManager.LayoutParams.FLAG_SECURE)
        } else {
            window.setFlags(WindowManager.LayoutParams.FLAG_SECURE, WindowManager.LayoutParams.FLAG_SECURE)
        }
        enableEdgeToEdge()
        setContent {
            S2AXTheme {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = MaterialTheme.colorScheme.background,
                    contentColor = MaterialTheme.colorScheme.onBackground,
                ) {
                    S2AXApp(viewModel)
                }
            }
        }
    }
}

private const val EXTRA_ALLOW_SCREEN_CAPTURE = "com.s2ax.mobile.extra.ALLOW_SCREEN_CAPTURE"

private enum class Destination(val title: String) {
    Home("概览"),
    Keys("密钥"),
    Usage("用量"),
    Manage("管理"),
    More("工作台"),
}

private enum class ManageTab(val title: String) {
    Accounts("账号"),
    Users("用户"),
    Groups("分组"),
    Policies("分配"),
    Currencies("货币"),
    Announcements("公告"),
}

private sealed interface CurrencyAction {
    data object Toggle : CurrencyAction
    data object EnableForAll : CurrencyAction
    data object ExpireHolds : CurrencyAction
    data object Reconcile : CurrencyAction
}

@Composable
private fun S2AXApp(viewModel: MainViewModel) {
    val snackbar = remember { SnackbarHostState() }
    var destination by rememberSaveable { mutableStateOf(Destination.Home) }

    LaunchedEffect(viewModel.notice) {
        viewModel.notice?.let {
            snackbar.showSnackbar(it)
            viewModel.dismissNotice()
        }
    }

    when {
        viewModel.booting -> LoadingScreen("正在恢复安全会话…")
        viewModel.session == null -> LoginFlow(viewModel)
        else -> {
            if (!viewModel.isAdmin && destination == Destination.Manage) destination = Destination.Home
            AuthenticatedApp(viewModel, destination, { destination = it }, snackbar)
        }
    }

    viewModel.newlyCreatedKey?.let { secret ->
        KeyRevealDialog(secret, onDismiss = viewModel::dismissNewKey)
    }
}

@Composable
private fun LoginFlow(viewModel: MainViewModel) {
    val challenge = viewModel.totpChallenge
    if (challenge == null) {
        LoginScreen(
            initialEndpoint = viewModel.savedBaseUrl.orEmpty(),
            loading = viewModel.authLoading,
            error = viewModel.authError,
            onLogin = viewModel::signIn,
        )
    } else {
        TotpScreen(
            maskedEmail = challenge.maskedEmail,
            loading = viewModel.authLoading,
            error = viewModel.authError,
            onSubmit = viewModel::completeTotp,
            onBack = viewModel::cancelTotp,
        )
    }
}

@Composable
private fun LoginScreen(
    initialEndpoint: String,
    loading: Boolean,
    error: String?,
    onLogin: (String, String, String) -> Unit,
) {
    var endpoint by rememberSaveable(initialEndpoint) { mutableStateOf(initialEndpoint) }
    var email by rememberSaveable { mutableStateOf("") }
    var password by rememberSaveable { mutableStateOf("") }
    var passwordVisible by rememberSaveable { mutableStateOf(false) }

    Box(
        modifier = Modifier
            .fillMaxSize()
            .imePadding()
            .padding(24.dp),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(max = 620.dp)
                .verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Text("S2AX", style = MaterialTheme.typography.displaySmall, fontWeight = FontWeight.Bold)
            Text(
                "轻量、原生的服务管理端",
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(12.dp))
            OutlinedTextField(
                value = endpoint,
                onValueChange = { endpoint = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("服务地址") },
                placeholder = { Text("https://api.example.com") },
                singleLine = true,
                supportingText = { Text("自动补齐 /api/v1；仅支持 HTTPS") },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Next),
            )
            OutlinedTextField(
                value = email,
                onValueChange = { email = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("邮箱") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, imeAction = ImeAction.Next),
            )
            OutlinedTextField(
                value = password,
                onValueChange = { password = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("密码") },
                singleLine = true,
                visualTransformation = if (passwordVisible) VisualTransformation.None else PasswordVisualTransformation(),
                trailingIcon = {
                    TextButton(onClick = { passwordVisible = !passwordVisible }) {
                        Text(if (passwordVisible) "隐藏" else "显示")
                    }
                },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
            )
            error?.let { ErrorBanner(it) }
            Button(
                onClick = { onLogin(endpoint, email, password) },
                modifier = Modifier.fillMaxWidth().height(52.dp),
                enabled = !loading && endpoint.isNotBlank() && email.isNotBlank() && password.isNotBlank(),
            ) {
                if (loading) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                else Text("安全登录")
            }
            Text(
                // ponytail: OAuth needs provider-specific App Links and redirect registration; add it when mobile OAuth is enabled for the deployment.
                "当前原生登录支持邮箱、密码和 TOTP；OAuth 登录仍使用网页端。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun TotpScreen(
    maskedEmail: String?,
    loading: Boolean,
    error: String?,
    onSubmit: (String) -> Unit,
    onBack: () -> Unit,
) {
    var code by rememberSaveable { mutableStateOf("") }
    Box(
        modifier = Modifier.fillMaxSize().imePadding().padding(24.dp),
        contentAlignment = Alignment.Center,
    ) {
        Column(modifier = Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            Text("二次验证", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
            Text("${maskedEmail ?: "此账号"} 已启用 TOTP，请输入验证器中的 6 位代码。")
            OutlinedTextField(
                value = code,
                onValueChange = { code = it.filter(Char::isDigit).take(8) },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("TOTP 验证码") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword, imeAction = ImeAction.Done),
            )
            error?.let { ErrorBanner(it) }
            Button(
                onClick = { onSubmit(code) },
                modifier = Modifier.fillMaxWidth().height(52.dp),
                enabled = !loading && code.length >= 6,
            ) {
                if (loading) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) else Text("验证并登录")
            }
            TextButton(onClick = onBack, modifier = Modifier.align(Alignment.End)) { Text("返回登录") }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun AuthenticatedApp(
    viewModel: MainViewModel,
    destination: Destination,
    onDestination: (Destination) -> Unit,
    snackbar: SnackbarHostState,
) {
    val session = viewModel.session ?: return
    val destinations = buildList {
        add(Destination.Home)
        add(Destination.Keys)
        add(Destination.Usage)
        if (viewModel.isAdmin) add(Destination.Manage)
        add(Destination.More)
    }
    Scaffold(
        topBar = {
            CenterAlignedTopAppBar(
                title = {
                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        Text(destination.title, fontWeight = FontWeight.SemiBold)
                        Text(
                            if (viewModel.isAdmin) "管理员控制台" else session.user.email,
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                },
                actions = {
                    IconButton(onClick = { refreshDestination(viewModel, destination) }) {
                        Icon(Icons.Outlined.Refresh, contentDescription = "刷新")
                    }
                },
                colors = TopAppBarDefaults.centerAlignedTopAppBarColors(
                    containerColor = MaterialTheme.colorScheme.surface.copy(alpha = 0.92f),
                ),
            )
        },
        bottomBar = {
            NavigationBar {
                destinations.forEach { item ->
                    NavigationBarItem(
                        selected = destination == item,
                        onClick = { onDestination(item) },
                        icon = { DestinationIcon(item) },
                        label = { Text(item.title) },
                    )
                }
            }
        },
        snackbarHost = { SnackbarHost(snackbar) },
    ) { padding ->
        AnimatedContent(
            targetState = destination,
            transitionSpec = { fadeIn(tween(140)) togetherWith androidx.compose.animation.fadeOut(tween(100)) },
            label = "screen",
            modifier = Modifier.padding(padding),
        ) { screen ->
            when (screen) {
                Destination.Home -> HomeScreen(viewModel)
                Destination.Keys -> KeysScreen(viewModel)
                Destination.Usage -> UsageScreen(viewModel)
                Destination.Manage -> ManageScreen(viewModel)
                Destination.More -> WorkspaceScreen(viewModel)
            }
        }
    }
}

@Composable
private fun DestinationIcon(destination: Destination) = when (destination) {
    Destination.Home -> Icon(Icons.Outlined.Home, null)
    Destination.Keys -> Icon(Icons.Outlined.Key, null)
    Destination.Usage -> Icon(Icons.Outlined.QueryStats, null)
    Destination.Manage -> Icon(Icons.Outlined.ManageAccounts, null)
    Destination.More -> Icon(Icons.Outlined.MoreHoriz, null)
}

private fun refreshDestination(viewModel: MainViewModel, destination: Destination) {
    when (destination) {
        Destination.Home -> viewModel.loadHome(force = true)
        Destination.Keys -> viewModel.loadKeys(force = true)
        Destination.Usage -> viewModel.loadUsage(force = true)
        Destination.Manage -> {
            viewModel.loadAccounts(force = true)
            viewModel.loadUsers(force = true)
            viewModel.loadGroups(force = true)
            viewModel.loadPolicies(force = true)
        }
        Destination.More -> {
            viewModel.loadAnnouncements(force = true)
            viewModel.refreshExplorer()
        }
    }
}

@Composable
private fun HomeScreen(viewModel: MainViewModel) {
    LaunchedEffect(Unit) { viewModel.loadHome() }
    val state = viewModel.home
    when {
        state.data == null && state.loading -> LoadingScreen("正在汇总服务状态…")
        state.data == null -> ErrorScreen(state.error ?: "无法加载概览", onRetry = { viewModel.loadHome(force = true) })
        else -> {
            val data = state.data
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
                state.error?.let { item { ErrorBanner(it) } }
                item { WelcomeCard(data.profile) }
                item { SectionTitle("使用概览") }
                item {
                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxWidth()) {
                        MetricCard("今日请求", formatNumber(data.usage.todayRequests), Modifier.weight(1f))
                        MetricCard("今日 Token", formatNumber(data.usage.todayTokens), Modifier.weight(1f))
                    }
                }
                item {
                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxWidth()) {
                        MetricCard("今日消耗", money(data.usage.todayCost), Modifier.weight(1f))
                        MetricCard("平均耗时", "${data.usage.averageDurationMs} ms", Modifier.weight(1f))
                    }
                }
                if (data.admin?.metrics?.isNotEmpty() == true) {
                    item { SectionTitle("系统速览") }
                    item { AdminMetricGrid(data.admin.metrics) }
                }
                if (data.wallets.isNotEmpty()) {
                    item { SectionTitle("虚拟资产") }
                    items(data.wallets, key = { it.code }) { wallet -> WalletCard(wallet) }
                }
                if (data.allocations.isNotEmpty()) {
                    item { SectionTitle("分配给我的账号") }
                    items(data.allocations, key = { "${it.accountName}-${it.accountType}" }) { allocation -> AllocationCard(allocation) }
                }
                item { Spacer(Modifier.height(12.dp)) }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun WelcomeCard(profile: UserSummary) {
    Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer)) {
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(
                "你好，${profile.username}",
                modifier = Modifier.fillMaxWidth(),
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.Bold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                profile.email,
                modifier = Modifier.fillMaxWidth(),
                color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.75f),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.height(8.dp))
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                StatusPill(if (profile.status == "active") "正常" else "已停用", profile.status == "active")
                Text("可用余额 ${money(profile.balance)}", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
            }
            if (profile.frozenBalance > 0) {
                Text("冻结 ${money(profile.frozenBalance)}", style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun AdminMetricGrid(metrics: List<Metric>) {
    FlowRow(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        metrics.forEach { metric -> MetricCard(metric.label, metric.value, Modifier.width(150.dp)) }
    }
}

@Composable
private fun MetricCard(label: String, value: String, modifier: Modifier = Modifier) {
    Card(modifier = modifier) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(label, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(value, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable
private fun WalletCard(wallet: WalletSummary) {
    Card {
        Row(
            Modifier.fillMaxWidth().padding(16.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Row(
                modifier = Modifier.weight(1f),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Icon(Icons.Outlined.AccountBalanceWallet, null, tint = MaterialTheme.colorScheme.primary)
                Column(Modifier.weight(1f)) {
                    Text(wallet.name, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(wallet.code, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            Column(horizontalAlignment = Alignment.End) {
                Text("${wallet.symbol}${formatUnits(wallet.availableUnits, wallet.scale)}", fontWeight = FontWeight.Bold)
                if (wallet.reservedUnits > 0) Text("冻结 ${formatUnits(wallet.reservedUnits, wallet.scale)}", style = MaterialTheme.typography.labelSmall)
            }
        }
    }
}

@Composable
private fun AllocationCard(item: AllocationSummary) {
    Card {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(horizontalArrangement = Arrangement.SpaceBetween, modifier = Modifier.fillMaxWidth()) {
                Text(item.accountName, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                StatusPill(item.status, item.status == "active")
            }
            Text("${item.platform.uppercase()} · ${item.accountType}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text("并发 ${item.concurrency} · 已用 ${formatNumber(item.totalTokens)} Token", style = MaterialTheme.typography.bodySmall)
        }
    }
}

@Composable
private fun KeysScreen(viewModel: MainViewModel) {
    LaunchedEffect(Unit) { viewModel.loadKeys() }
    val state = viewModel.keys
    var search by rememberSaveable { mutableStateOf("") }
    var create by rememberSaveable { mutableStateOf(false) }
    var deleteTarget by remember { mutableStateOf<ApiKeySummary?>(null) }
    Column(Modifier.fillMaxSize()) {
        SearchToolbar(
            value = search,
            onValueChange = { search = it },
            onSearch = { viewModel.loadKeys(search, force = true) },
            action = {
                IconButton(onClick = { create = true }) { Icon(Icons.Outlined.Add, "新建 API Key") }
            },
        )
        ListContent(state, onRetry = { viewModel.loadKeys(force = true) }) { items ->
            items(items, key = { it.id }) { item ->
                KeyCard(item, onToggle = { viewModel.updateKeyStatus(item) }, onDelete = { deleteTarget = item })
            }
        }
    }
    if (create) CreateKeyDialog(onDismiss = { create = false }, onCreate = { name, group ->
        create = false
        viewModel.createKey(name, group)
    })
    deleteTarget?.let { target ->
        ConfirmDialog(
            title = "删除 API Key？",
            message = "“${target.name}” 将立即失效，此操作不可恢复。",
            confirmLabel = "删除",
            destructive = true,
            onDismiss = { deleteTarget = null },
            onConfirm = { viewModel.deleteKey(target); deleteTarget = null },
        )
    }
}

@Composable
private fun KeyCard(key: ApiKeySummary, onToggle: () -> Unit, onDelete: () -> Unit) {
    var menu by remember { mutableStateOf(false) }
    Card {
        Row(
            Modifier.fillMaxWidth().padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Icon(Icons.Outlined.Key, null, tint = MaterialTheme.colorScheme.primary)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text(key.name, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(key.keyPrefix, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                key.groupName?.let { Text(it, style = MaterialTheme.typography.labelSmall) }
            }
            Column(horizontalAlignment = Alignment.End) {
                StatusPill(if (key.status == "active") "启用" else "停用", key.status == "active")
                Box {
                    IconButton(onClick = { menu = true }) { Icon(Icons.Outlined.MoreHoriz, "更多操作") }
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        DropdownMenuItem(
                            text = { Text(if (key.status == "active") "停用" else "启用") },
                            onClick = { menu = false; onToggle() },
                            leadingIcon = { Icon(if (key.status == "active") Icons.Outlined.Block else Icons.Outlined.CheckCircle, null) },
                        )
                        DropdownMenuItem(
                            text = { Text("删除") },
                            onClick = { menu = false; onDelete() },
                            leadingIcon = { Icon(Icons.Outlined.Delete, null, tint = MaterialTheme.colorScheme.error) },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun UsageScreen(viewModel: MainViewModel) {
    LaunchedEffect(Unit) { viewModel.loadUsage() }
    ListContent(viewModel.usage, onRetry = { viewModel.loadUsage(force = true) }) { rows ->
        items(rows, key = { it.id }) { row -> UsageCard(row) }
    }
}

@Composable
private fun UsageCard(entry: UsageEntry) {
    Card {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(5.dp)) {
            Row(horizontalArrangement = Arrangement.SpaceBetween, modifier = Modifier.fillMaxWidth()) {
                Text(entry.model, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                Text(money(entry.cost), fontWeight = FontWeight.Bold)
            }
            Text(entry.group, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Row(horizontalArrangement = Arrangement.SpaceBetween, modifier = Modifier.fillMaxWidth()) {
                Text("${formatNumber(entry.totalTokens)} Token", style = MaterialTheme.typography.bodySmall)
                Text(entry.occurredAt, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

@Composable
private fun ManageScreen(viewModel: MainViewModel) {
    var tab by rememberSaveable { mutableStateOf(ManageTab.Accounts) }
    LaunchedEffect(tab) {
        when (tab) {
            ManageTab.Accounts -> viewModel.loadAccounts()
            ManageTab.Users -> viewModel.loadUsers()
            ManageTab.Groups -> viewModel.loadGroups()
            ManageTab.Policies -> viewModel.loadPolicies()
            ManageTab.Currencies -> viewModel.loadCurrencies()
            ManageTab.Announcements -> viewModel.loadAdminAnnouncements()
        }
    }
    Column(Modifier.fillMaxSize()) {
        ScrollableTabRow(selectedTabIndex = tab.ordinal, edgePadding = 12.dp) {
            ManageTab.entries.forEach { item ->
                Tab(selected = item == tab, onClick = { tab = item }, text = { Text(item.title) })
            }
        }
        when (tab) {
            ManageTab.Accounts -> AccountsPanel(viewModel)
            ManageTab.Users -> UsersPanel(viewModel)
            ManageTab.Groups -> GroupsPanel(viewModel)
            ManageTab.Policies -> PoliciesPanel(viewModel)
            ManageTab.Currencies -> CurrenciesPanel(viewModel)
            ManageTab.Announcements -> AdminAnnouncementsPanel(viewModel)
        }
    }
}

@Composable
private fun AccountsPanel(viewModel: MainViewModel) {
    var search by rememberSaveable { mutableStateOf("") }
    var actionTarget by remember { mutableStateOf<Pair<AccountSummary, AccountAction>?>(null) }
    Column(Modifier.fillMaxSize()) {
        SearchToolbar(search, { search = it }, { viewModel.loadAccounts(search, force = true) })
        ListContent(viewModel.accounts, onRetry = { viewModel.loadAccounts(force = true) }) { rows ->
            items(rows, key = { it.id }) { account ->
                AccountCard(account, onAction = { action ->
                    if (action is AccountAction.SetStatus) actionTarget = account to action
                    else viewModel.accountAction(account, action)
                })
            }
        }
    }
    actionTarget?.let { (account, action) ->
        val disabling = action is AccountAction.SetStatus && action.status != "active"
        ConfirmDialog(
            title = if (disabling) "停用账号？" else "启用账号？",
            message = "${account.name} 的调度状态将立即改变。",
            confirmLabel = if (disabling) "停用" else "启用",
            destructive = disabling,
            onDismiss = { actionTarget = null },
            onConfirm = { viewModel.accountAction(account, action); actionTarget = null },
        )
    }
}

@Composable
private fun AccountCard(account: AccountSummary, onAction: (AccountAction) -> Unit) {
    var menu by remember { mutableStateOf(false) }
    Card {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
                Column(Modifier.weight(1f)) {
                    Text(account.name, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text("${account.platform.uppercase()} · ${account.type}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                StatusPill(account.status, account.status == "active")
                Box {
                    IconButton(onClick = { menu = true }) { Icon(Icons.Outlined.MoreHoriz, "账号操作") }
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        AccountMenuItem("连接测试", Icons.Outlined.PlayArrow) { menu = false; onAction(AccountAction.Test) }
                        AccountMenuItem("刷新凭据", Icons.Outlined.Refresh) { menu = false; onAction(AccountAction.Refresh) }
                        AccountMenuItem("恢复状态", Icons.Outlined.Autorenew) { menu = false; onAction(AccountAction.Recover) }
                        if (account.errorMessage != null) AccountMenuItem("清除错误", Icons.Outlined.ErrorOutline) { menu = false; onAction(AccountAction.ClearError) }
                        HorizontalDivider()
                        AccountMenuItem(
                            if (account.status == "active") "停用调度" else "启用调度",
                            if (account.status == "active") Icons.Outlined.Block else Icons.Outlined.CheckCircle,
                        ) { menu = false; onAction(AccountAction.SetStatus(if (account.status == "active") "inactive" else "active")) }
                    }
                }
            }
            val groups = account.groupNames.joinToString().ifBlank { "未分组" }
            Text(groups, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text("并发 ${account.currentConcurrency}/${account.concurrency} · ${if (account.schedulable) "可调度" else "不可调度"}", style = MaterialTheme.typography.labelMedium)
            account.errorMessage?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall, maxLines = 2, overflow = TextOverflow.Ellipsis) }
        }
    }
}

@Composable
private fun AccountMenuItem(label: String, icon: androidx.compose.ui.graphics.vector.ImageVector, onClick: () -> Unit) {
    DropdownMenuItem(
        text = { Text(label) },
        onClick = onClick,
        leadingIcon = { Icon(icon, null) },
    )
}

@Composable
private fun UsersPanel(viewModel: MainViewModel) {
    var search by rememberSaveable { mutableStateOf("") }
    var balanceTarget by remember { mutableStateOf<AdminUserSummary?>(null) }
    var currencyTarget by remember { mutableStateOf<AdminUserSummary?>(null) }
    var disableTarget by remember { mutableStateOf<AdminUserSummary?>(null) }
    LaunchedEffect(currencyTarget?.id) {
        if (currencyTarget != null) viewModel.loadCurrencies()
    }
    Column(Modifier.fillMaxSize()) {
        SearchToolbar(search, { search = it }, { viewModel.loadUsers(search, force = true) })
        ListContent(viewModel.users, onRetry = { viewModel.loadUsers(force = true) }) { rows ->
            items(rows, key = { it.id }) { user ->
                UserCard(
                    user,
                    onBalance = { balanceTarget = user },
                    onCurrency = { currencyTarget = user },
                    onToggle = { disableTarget = user },
                )
            }
        }
    }
    balanceTarget?.let { BalanceDialog(it, onDismiss = { balanceTarget = null }, onSubmit = { amount, operation, notes ->
        viewModel.adjustBalance(it, amount, operation, notes)
        balanceTarget = null
    }) }
    currencyTarget?.let { user ->
        CurrencyAdjustmentDialog(
            user = user,
            currencies = viewModel.currencies,
            onDismiss = { currencyTarget = null },
            onSubmit = { currency, amountUnits, reason ->
                viewModel.adjustVirtualCurrency(user, currency, amountUnits, reason)
                currencyTarget = null
            },
        )
    }
    disableTarget?.let { user ->
        val disabling = user.status == "active"
        ConfirmDialog(
            title = if (disabling) "停用用户？" else "启用用户？",
            message = "${user.email} 将${if (disabling) "无法继续调用服务" else "恢复服务访问"}。",
            confirmLabel = if (disabling) "停用" else "启用",
            destructive = disabling,
            onDismiss = { disableTarget = null },
            onConfirm = { viewModel.toggleUser(user); disableTarget = null },
        )
    }
}

@Composable
private fun UserCard(
    user: AdminUserSummary,
    onBalance: () -> Unit,
    onCurrency: () -> Unit,
    onToggle: () -> Unit,
) {
    var menu by remember { mutableStateOf(false) }
    Card {
        Row(
            Modifier.fillMaxWidth().padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Icon(Icons.Outlined.People, null, tint = MaterialTheme.colorScheme.primary)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(user.email, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text("${user.username} · ${user.role} · 并发 ${user.concurrency}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(money(user.balance), fontWeight = FontWeight.Medium)
            }
            Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(4.dp)) {
                StatusPill(if (user.status == "active") "正常" else "停用", user.status == "active")
                Box {
                    IconButton(onClick = { menu = true }) { Icon(Icons.Outlined.MoreHoriz, "用户操作") }
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        DropdownMenuItem(
                            text = { Text("调整余额") },
                            onClick = { menu = false; onBalance() },
                            leadingIcon = { Icon(Icons.Outlined.AccountBalanceWallet, null) },
                        )
                        DropdownMenuItem(
                            text = { Text("调整虚拟货币") },
                            onClick = { menu = false; onCurrency() },
                            leadingIcon = { Icon(Icons.Outlined.Add, null) },
                        )
                        HorizontalDivider()
                        DropdownMenuItem(
                            text = { Text(if (user.status == "active") "停用用户" else "启用用户") },
                            onClick = { menu = false; onToggle() },
                            leadingIcon = { Icon(if (user.status == "active") Icons.Outlined.Block else Icons.Outlined.CheckCircle, null) },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun GroupsPanel(viewModel: MainViewModel) {
    var target by remember { mutableStateOf<GroupSummary?>(null) }
    ListContent(viewModel.groups, onRetry = { viewModel.loadGroups(force = true) }) { rows ->
        items(rows, key = { it.id }) { group ->
            Card {
                Row(
                    Modifier.fillMaxWidth().padding(16.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Icon(Icons.Outlined.Groups, null, tint = MaterialTheme.colorScheme.primary)
                    Column(Modifier.weight(1f)) {
                        Text(group.name, fontWeight = FontWeight.SemiBold)
                        Text("${group.platform.uppercase()} · 倍率 ${group.rateMultiplier}×", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    StatusPill(if (group.status == "active") "启用" else "停用", group.status == "active")
                    IconButton(onClick = { target = group }) {
                        Icon(if (group.status == "active") Icons.Outlined.Block else Icons.Outlined.CheckCircle, "切换分组状态")
                    }
                }
            }
        }
    }
    target?.let { group ->
        val disabling = group.status == "active"
        ConfirmDialog(
            title = if (disabling) "停用分组？" else "启用分组？",
            message = "${group.name} 的 API Key 路由会受此状态影响。",
            confirmLabel = if (disabling) "停用" else "启用",
            destructive = disabling,
            onDismiss = { target = null },
            onConfirm = { viewModel.toggleGroup(group); target = null },
        )
    }
}

@Composable
private fun PoliciesPanel(viewModel: MainViewModel) {
    var target by remember { mutableStateOf<AllocationPolicySummary?>(null) }
    ListContent(viewModel.policies, onRetry = { viewModel.loadPolicies(force = true) }) { rows ->
        items(rows, key = { it.id }) { policy ->
            Card {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(7.dp)) {
                    Row(horizontalArrangement = Arrangement.SpaceBetween, modifier = Modifier.fillMaxWidth()) {
                        Column(Modifier.weight(1f)) {
                            Text(policy.userEmail, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(policy.groupName, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        StatusPill(if (policy.status == "active") "启用" else "停用", policy.status == "active")
                    }
                    Text("已分配 ${policy.activeCount}/${policy.desiredCount} · 缺口 ${policy.shortage}", style = MaterialTheme.typography.bodySmall)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        FilledTonalButton(onClick = { viewModel.reconcilePolicy(policy) }) {
                            Icon(Icons.Outlined.Autorenew, null, Modifier.size(18.dp))
                            Spacer(Modifier.width(6.dp))
                            Text("立即补齐")
                        }
                        OutlinedButton(onClick = { target = policy }) {
                            Text(if (policy.status == "active") "停用" else "启用")
                        }
                    }
                }
            }
        }
    }
    target?.let { policy ->
        val disabling = policy.status == "active"
        ConfirmDialog(
            title = if (disabling) "停用分配策略？" else "启用分配策略？",
            message = "${policy.userEmail} 的自动账号补齐将${if (disabling) "停止" else "恢复"}。",
            confirmLabel = if (disabling) "停用" else "启用",
            destructive = disabling,
            onDismiss = { target = null },
            onConfirm = { viewModel.togglePolicy(policy); target = null },
        )
    }
}

@Composable
private fun CurrenciesPanel(viewModel: MainViewModel) {
    var createDialog by remember { mutableStateOf(false) }
    var actionTarget by remember { mutableStateOf<Pair<VirtualCurrencySummary, CurrencyAction>?>(null) }
    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("虚拟货币", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
            IconButton(onClick = { createDialog = true }) { Icon(Icons.Outlined.Add, "创建虚拟货币") }
        }
        ListContent(viewModel.currencies, onRetry = { viewModel.loadCurrencies(force = true) }) { rows ->
            items(rows, key = { it.id }) { currency ->
                CurrencyCard(
                    currency = currency,
                    onAction = { action ->
                        when (action) {
                            CurrencyAction.ExpireHolds -> viewModel.expireCurrencyHolds(currency)
                            CurrencyAction.Reconcile -> viewModel.reconcileCurrency(currency)
                            else -> actionTarget = currency to action
                        }
                    },
                )
            }
        }
    }
    createDialog.takeIf { it }?.let {
        CreateCurrencyDialog(
            onDismiss = { createDialog = false },
            onCreate = { code, name, symbol, scale, description ->
                viewModel.createCurrency(code, name, symbol, scale, description)
                createDialog = false
            },
        )
    }
    actionTarget?.let { (currency, action) ->
        val isToggle = action == CurrencyAction.Toggle
        val disabling = isToggle && currency.status == "active"
        ConfirmDialog(
            title = when {
                action == CurrencyAction.EnableForAll -> "向全部用户启用？"
                disabling -> "停用虚拟货币？"
                else -> "启用虚拟货币？"
            },
            message = when {
                action == CurrencyAction.EnableForAll -> "将在所有可用标准分组开放 ${currency.name} 的获取与消费。"
                disabling -> "已持有的 ${currency.name} 不会被清除，但后续使用会受状态限制。"
                else -> "${currency.name} 将恢复为可用状态。"
            },
            confirmLabel = when {
                action == CurrencyAction.EnableForAll -> "确认启用"
                disabling -> "停用"
                else -> "启用"
            },
            destructive = disabling,
            onDismiss = { actionTarget = null },
            onConfirm = {
                when (action) {
                    CurrencyAction.Toggle -> viewModel.toggleCurrency(currency)
                    CurrencyAction.EnableForAll -> viewModel.enableCurrencyForAllUsers(currency)
                    else -> Unit
                }
                actionTarget = null
            },
        )
    }
}

@Composable
private fun CurrencyCard(currency: VirtualCurrencySummary, onAction: (CurrencyAction) -> Unit) {
    var menu by remember { mutableStateOf(false) }
    Card {
        Row(
            Modifier.fillMaxWidth().padding(16.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(Icons.Outlined.AccountBalanceWallet, null, tint = MaterialTheme.colorScheme.primary)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(
                    listOf(currency.symbol, currency.name).filter { it.isNotBlank() }.joinToString(" ").ifBlank { currency.code },
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text("${currency.code.uppercase()} · 精度 ${currency.scale}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                currency.description.takeIf { it.isNotBlank() }?.let {
                    Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
            StatusPill(if (currency.status == "active") "启用" else "停用", currency.status == "active")
            Box {
                IconButton(onClick = { menu = true }) { Icon(Icons.Outlined.MoreHoriz, "货币操作") }
                DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                    DropdownMenuItem(
                        text = { Text(if (currency.status == "active") "停用货币" else "启用货币") },
                        onClick = { menu = false; onAction(CurrencyAction.Toggle) },
                        leadingIcon = { Icon(if (currency.status == "active") Icons.Outlined.Block else Icons.Outlined.CheckCircle, null) },
                    )
                    DropdownMenuItem(
                        text = { Text("向全部用户启用") },
                        onClick = { menu = false; onAction(CurrencyAction.EnableForAll) },
                        leadingIcon = { Icon(Icons.Outlined.People, null) },
                    )
                    HorizontalDivider()
                    DropdownMenuItem(
                        text = { Text("核对账本") },
                        onClick = { menu = false; onAction(CurrencyAction.Reconcile) },
                        leadingIcon = { Icon(Icons.Outlined.QueryStats, null) },
                    )
                    DropdownMenuItem(
                        text = { Text("释放过期冻结") },
                        onClick = { menu = false; onAction(CurrencyAction.ExpireHolds) },
                        leadingIcon = { Icon(Icons.Outlined.Autorenew, null) },
                    )
                }
            }
        }
    }
}

@Composable
private fun AdminAnnouncementsPanel(viewModel: MainViewModel) {
    var search by rememberSaveable { mutableStateOf("") }
    var createDialog by remember { mutableStateOf(false) }
    var actionTarget by remember { mutableStateOf<Pair<AdminAnnouncementSummary, String>?>(null) }
    Column(Modifier.fillMaxSize()) {
        SearchToolbar(
            value = search,
            onValueChange = { search = it },
            onSearch = { viewModel.loadAdminAnnouncements(search, force = true) },
            action = {
                IconButton(onClick = { createDialog = true }) { Icon(Icons.Outlined.Add, "创建公告") }
            },
        )
        ListContent(viewModel.adminAnnouncements, onRetry = { viewModel.loadAdminAnnouncements(force = true) }) { rows ->
            items(rows, key = { it.id }) { item ->
                AdminAnnouncementCard(item, onAction = { actionTarget = item to it })
            }
        }
    }
    createDialog.takeIf { it }?.let {
        CreateAnnouncementDialog(
            onDismiss = { createDialog = false },
            onCreate = { title, content, status, notifyMode ->
                viewModel.createAdminAnnouncement(title, content, status, notifyMode)
                createDialog = false
            },
        )
    }
    actionTarget?.let { (item, action) ->
        val delete = action == "delete"
        ConfirmDialog(
            title = if (delete) "删除公告？" else "更新公告状态？",
            message = if (delete) {
                "“${item.title}” 将被永久删除，用户将无法再查看。"
            } else {
                "“${item.title}” 将${if (action == "active") "立即发布" else if (action == "archived") "归档" else "转为草稿"}。"
            },
            confirmLabel = if (delete) "删除" else if (action == "active") "发布" else "确认",
            destructive = delete,
            onDismiss = { actionTarget = null },
            onConfirm = {
                if (delete) viewModel.deleteAdminAnnouncement(item)
                else viewModel.updateAdminAnnouncementStatus(item, action)
                actionTarget = null
            },
        )
    }
}

@Composable
private fun AdminAnnouncementCard(item: AdminAnnouncementSummary, onAction: (String) -> Unit) {
    var menu by remember { mutableStateOf(false) }
    Card {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                Icon(Icons.Outlined.Notifications, null, tint = MaterialTheme.colorScheme.primary)
                Text(item.title, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                StatusPill(
                    label = when (item.status) {
                        "active" -> "已发布"
                        "archived" -> "已归档"
                        else -> "草稿"
                    },
                    positive = item.status == "active",
                )
                Box {
                    IconButton(onClick = { menu = true }) { Icon(Icons.Outlined.MoreHoriz, "公告操作") }
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        if (item.status != "active") {
                            DropdownMenuItem(
                                text = { Text("发布") },
                                onClick = { menu = false; onAction("active") },
                                leadingIcon = { Icon(Icons.Outlined.CheckCircle, null) },
                            )
                        }
                        if (item.status != "draft") {
                            DropdownMenuItem(
                                text = { Text("转为草稿") },
                                onClick = { menu = false; onAction("draft") },
                                leadingIcon = { Icon(Icons.Outlined.Refresh, null) },
                            )
                        }
                        if (item.status != "archived") {
                            DropdownMenuItem(
                                text = { Text("归档") },
                                onClick = { menu = false; onAction("archived") },
                                leadingIcon = { Icon(Icons.Outlined.Block, null) },
                            )
                        }
                        HorizontalDivider()
                        DropdownMenuItem(
                            text = { Text("删除", color = MaterialTheme.colorScheme.error) },
                            onClick = { menu = false; onAction("delete") },
                            leadingIcon = { Icon(Icons.Outlined.Delete, null, tint = MaterialTheme.colorScheme.error) },
                        )
                    }
                }
            }
            Text(item.content, style = MaterialTheme.typography.bodySmall, maxLines = 3, overflow = TextOverflow.Ellipsis)
            Text("${if (item.notifyMode == "popup") "弹窗提醒" else "静默"} · ${item.createdAt}", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
internal fun AccountCenterScreen(viewModel: MainViewModel, onBack: (() -> Unit)? = null) {
    LaunchedEffect(Unit) { viewModel.loadAnnouncements() }
    val user = viewModel.session?.user ?: return
    var editProfile by rememberSaveable { mutableStateOf(false) }
    var redeem by rememberSaveable { mutableStateOf(false) }
    var changePassword by rememberSaveable { mutableStateOf(false) }
    var transferAffiliate by rememberSaveable { mutableStateOf(false) }
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        onBack?.let { navigateBack ->
            item {
                TextButton(onClick = navigateBack) { Text("返回工作台") }
            }
        }
        item {
            Card {
                Column(Modifier.padding(18.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(user.username, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
                    Text(user.email, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text("${if (viewModel.isAdmin) "管理员" else "用户"} · 服务会话已加密保存", style = MaterialTheme.typography.bodySmall)
                    OutlinedButton(onClick = viewModel::signOut, modifier = Modifier.fillMaxWidth()) {
                        Icon(Icons.Outlined.PowerSettingsNew, null)
                        Spacer(Modifier.width(8.dp))
                        Text("退出并切换服务")
                    }
                }
            }
        }
        item {
            Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.34f))) {
                Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text("账户操作", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
                    FilledTonalButton(onClick = { editProfile = true }, modifier = Modifier.fillMaxWidth()) { Text("编辑个人资料") }
                    FilledTonalButton(onClick = { redeem = true }, modifier = Modifier.fillMaxWidth()) { Text("兑换码") }
                    OutlinedButton(onClick = { changePassword = true }, modifier = Modifier.fillMaxWidth()) { Text("修改密码") }
                    TextButton(onClick = { transferAffiliate = true }, modifier = Modifier.align(Alignment.End)) { Text("将返利转入余额") }
                }
            }
        }
        item {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween) {
                SectionTitle("公告")
                TextButton(
                    onClick = viewModel::markAllAnnouncementsRead,
                    enabled = viewModel.announcements.data.orEmpty().any { !it.isRead },
                ) { Text("全部已读") }
            }
        }
        val state = viewModel.announcements
        if (state.loading && state.data == null) item { InlineLoading() }
        state.error?.let { item { ErrorBanner(it) } }
        items(state.data.orEmpty(), key = { it.id }) { announcement ->
            AnnouncementCard(announcement, onRead = { viewModel.markAnnouncementRead(announcement) })
        }
        if (state.data.orEmpty().isEmpty() && !state.loading) item { EmptyCard("暂无公告") }
    }
    if (editProfile) EditProfileDialog(user, onDismiss = { editProfile = false }, onSubmit = {
        viewModel.updateMyProfile(it)
        editProfile = false
    })
    if (redeem) RedeemCodeDialog(onDismiss = { redeem = false }, onSubmit = {
        viewModel.redeemCode(it)
        redeem = false
    })
    if (changePassword) ChangePasswordDialog(onDismiss = { changePassword = false }, onSubmit = { oldPassword, newPassword ->
        viewModel.changeMyPassword(oldPassword, newPassword)
        changePassword = false
    })
    if (transferAffiliate) ConfirmDialog(
        title = "转入返利余额？",
        message = "可转返利额度将转入当前账户余额。",
        confirmLabel = "确认转入",
        destructive = false,
        onDismiss = { transferAffiliate = false },
        onConfirm = { viewModel.transferAffiliateQuota(); transferAffiliate = false },
    )
}

@Composable
private fun AnnouncementCard(item: AnnouncementSummary, onRead: () -> Unit) {
    Card(modifier = Modifier.clickable(onClick = onRead), colors = CardDefaults.cardColors(
        containerColor = if (item.isRead) MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.35f) else MaterialTheme.colorScheme.secondaryContainer.copy(alpha = 0.55f),
    )) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(Icons.Outlined.Notifications, null, Modifier.size(18.dp))
                Text(item.title, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (!item.isRead) Text("未读", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
            }
            Text(item.content, style = MaterialTheme.typography.bodySmall, maxLines = 3, overflow = TextOverflow.Ellipsis)
            Text(item.createdAt, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun <T> ListContent(
    state: LoadState<List<T>>,
    onRetry: () -> Unit,
    content: LazyListScope.(List<T>) -> Unit,
) {
    when {
        state.data == null && state.loading -> LoadingScreen("正在加载…")
        state.data == null -> ErrorScreen(state.error ?: "暂无数据", onRetry)
        else -> LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
            state.error?.let { item { ErrorBanner(it) } }
            content(state.data)
            if (state.data.isNullOrEmpty() && !state.loading) item { EmptyCard("暂无记录") }
            item { Spacer(Modifier.height(12.dp)) }
        }
    }
}

@Composable
private fun SearchToolbar(
    value: String,
    onValueChange: (String) -> Unit,
    onSearch: () -> Unit,
    action: @Composable (() -> Unit)? = null,
) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            modifier = Modifier.weight(1f),
            label = { Text("搜索") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
            keyboardActions = KeyboardActions(onSearch = { onSearch() }),
        )
        IconButton(onClick = onSearch) { Icon(Icons.Outlined.Refresh, "执行搜索") }
        action?.invoke()
    }
}

@Composable
private fun EditProfileDialog(user: UserSummary, onDismiss: () -> Unit, onSubmit: (String) -> Unit) {
    var username by rememberSaveable(user.id) { mutableStateOf(user.username) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("编辑个人资料") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(user.email, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                OutlinedTextField(
                    value = username,
                    onValueChange = { username = it },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("用户名") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                )
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
        confirmButton = {
            Button(onClick = { onSubmit(username) }, enabled = username.trim().isNotBlank()) { Text("保存") }
        },
    )
}

@Composable
private fun RedeemCodeDialog(onDismiss: () -> Unit, onSubmit: (String) -> Unit) {
    var code by rememberSaveable { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("兑换码") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("兑换码区分大小写，提交后会立即生效。", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                OutlinedTextField(
                    value = code,
                    onValueChange = { code = it.filterNot(Char::isWhitespace) },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("输入兑换码") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                )
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
        confirmButton = { Button(onClick = { onSubmit(code) }, enabled = code.isNotBlank()) { Text("兑换") } },
    )
}

@Composable
private fun ChangePasswordDialog(onDismiss: () -> Unit, onSubmit: (String, String) -> Unit) {
    var oldPassword by rememberSaveable { mutableStateOf("") }
    var newPassword by rememberSaveable { mutableStateOf("") }
    var confirmPassword by rememberSaveable { mutableStateOf("") }
    val passwordsMatch = newPassword == confirmPassword
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("修改密码") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                OutlinedTextField(
                    value = oldPassword,
                    onValueChange = { oldPassword = it },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("当前密码") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Next),
                )
                OutlinedTextField(
                    value = newPassword,
                    onValueChange = { newPassword = it },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("新密码") },
                    supportingText = { Text("至少 8 位") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Next),
                )
                OutlinedTextField(
                    value = confirmPassword,
                    onValueChange = { confirmPassword = it },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("确认新密码") },
                    isError = confirmPassword.isNotEmpty() && !passwordsMatch,
                    supportingText = if (confirmPassword.isNotEmpty() && !passwordsMatch) ({ Text("两次输入不一致") }) else null,
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
                )
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
        confirmButton = {
            Button(
                onClick = { onSubmit(oldPassword, newPassword) },
                enabled = oldPassword.isNotBlank() && newPassword.length >= 8 && passwordsMatch,
            ) { Text("更新密码") }
        },
    )
}

@Composable
private fun CreateKeyDialog(onDismiss: () -> Unit, onCreate: (String, String) -> Unit) {
    var name by rememberSaveable { mutableStateOf("") }
    var groupId by rememberSaveable { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("新建 API Key") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedTextField(name, { name = it }, label = { Text("名称") }, singleLine = true)
                OutlinedTextField(groupId, { groupId = it.filter(Char::isDigit) }, label = { Text("分组 ID（可选）") }, singleLine = true, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number))
                Text("密钥仅会在创建成功后显示一次。", style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = { Button(onClick = { onCreate(name, groupId) }, enabled = name.isNotBlank()) { Text("创建") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun KeyRevealDialog(secret: String, onDismiss: () -> Unit) {
    val context = LocalContext.current
    AlertDialog(
        onDismissRequest = {},
        title = { Text("保存新 API Key") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text("请立即复制并保存。关闭后应用不会再次显示此密钥。")
                SelectionContainer {
                    Text(secret, fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace, fontSize = 13.sp)
                }
                FilledTonalButton(onClick = {
                    val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                    clipboard.setPrimaryClip(ClipData.newPlainText("S2AX API Key", secret))
                }, modifier = Modifier.fillMaxWidth()) {
                    Icon(Icons.Outlined.ContentCopy, null)
                    Spacer(Modifier.width(8.dp))
                    Text("复制密钥")
                }
            }
        },
        confirmButton = { Button(onClick = onDismiss) { Text("我已保存") } },
    )
}

@Composable
private fun CreateCurrencyDialog(
    onDismiss: () -> Unit,
    onCreate: (code: String, name: String, symbol: String, scale: String, description: String) -> Unit,
) {
    var code by rememberSaveable { mutableStateOf("") }
    var name by rememberSaveable { mutableStateOf("") }
    var symbol by rememberSaveable { mutableStateOf("") }
    var scale by rememberSaveable { mutableStateOf("100") }
    var description by rememberSaveable { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("创建虚拟货币") },
        text = {
            Column(
                modifier = Modifier.heightIn(max = 440.dp).verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                OutlinedTextField(code, { code = it.uppercase(Locale.ROOT) }, label = { Text("货币代码") }, singleLine = true)
                OutlinedTextField(name, { name = it }, label = { Text("名称") }, singleLine = true)
                OutlinedTextField(symbol, { symbol = it }, label = { Text("符号（可选）") }, singleLine = true)
                OutlinedTextField(
                    scale,
                    { scale = it.filter(Char::isDigit) },
                    label = { Text("精度（最小单位倍率）") },
                    supportingText = { Text("例如 100 表示 1.00 = 100 最小单位") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                )
                OutlinedTextField(description, { description = it }, label = { Text("说明（可选）") }, minLines = 2)
            }
        },
        confirmButton = {
            Button(
                onClick = { onCreate(code, name, symbol, scale, description) },
                enabled = code.isNotBlank() && name.isNotBlank() && (scale.toIntOrNull() ?: 0) > 0,
            ) { Text("创建") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun CurrencyAdjustmentDialog(
    user: AdminUserSummary,
    currencies: LoadState<List<VirtualCurrencySummary>>,
    onDismiss: () -> Unit,
    onSubmit: (currency: VirtualCurrencySummary, amountUnits: Long, reason: String) -> Unit,
) {
    val options = currencies.data.orEmpty().filter { it.status == "active" }
    var selected by remember(options) { mutableStateOf(options.firstOrNull()) }
    var pickerOpen by remember { mutableStateOf(false) }
    var amount by rememberSaveable { mutableStateOf("") }
    var add by rememberSaveable { mutableStateOf(true) }
    var reason by rememberSaveable { mutableStateOf("") }
    val units = selected?.let { amountToUnits(amount, it.scale) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("调整虚拟货币") },
        text = {
            Column(
                modifier = Modifier.heightIn(max = 460.dp).verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text(user.email, style = MaterialTheme.typography.bodySmall)
                when {
                    currencies.data == null && currencies.error == null -> InlineLoading()
                    currencies.error != null && options.isEmpty() -> ErrorBanner(currencies.error)
                    options.isEmpty() -> Text("当前没有可调整的已启用虚拟货币。", color = MaterialTheme.colorScheme.onSurfaceVariant)
                    else -> {
                        Box {
                            OutlinedButton(onClick = { pickerOpen = true }, modifier = Modifier.fillMaxWidth()) {
                                Text(selected?.let { "${it.symbol} ${it.name} · ${it.code}" } ?: "选择货币", modifier = Modifier.weight(1f))
                                Icon(Icons.Outlined.MoreHoriz, null)
                            }
                            DropdownMenu(expanded = pickerOpen, onDismissRequest = { pickerOpen = false }) {
                                options.forEach { currency ->
                                    DropdownMenuItem(
                                        text = { Text("${currency.symbol} ${currency.name} · ${currency.code}") },
                                        onClick = { selected = currency; pickerOpen = false },
                                    )
                                }
                            }
                        }
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            AssistChip(
                                onClick = { add = true },
                                label = { Text("增加") },
                                leadingIcon = if (add) ({ Icon(Icons.Outlined.CheckCircle, null, Modifier.size(16.dp)) }) else null,
                                colors = if (add) AssistChipDefaults.assistChipColors(containerColor = MaterialTheme.colorScheme.secondaryContainer) else AssistChipDefaults.assistChipColors(),
                            )
                            AssistChip(
                                onClick = { add = false },
                                label = { Text("扣减") },
                                leadingIcon = if (!add) ({ Icon(Icons.Outlined.CheckCircle, null, Modifier.size(16.dp)) }) else null,
                                colors = if (!add) AssistChipDefaults.assistChipColors(containerColor = MaterialTheme.colorScheme.secondaryContainer) else AssistChipDefaults.assistChipColors(),
                            )
                        }
                        OutlinedTextField(
                            amount,
                            { amount = it },
                            label = { Text("数量") },
                            supportingText = { Text(selected?.let { "按精度 ${it.scale} 自动换算为最小单位" } ?: "") },
                            singleLine = true,
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                        )
                        if (amount.isNotBlank() && units == null) {
                            Text("数量精度或范围无效。", color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                        }
                        OutlinedTextField(reason, { reason = it }, label = { Text("调整原因") }, minLines = 2)
                    }
                }
            }
        },
        confirmButton = {
            Button(
                onClick = {
                    val currency = selected ?: return@Button
                    val amountUnits = units ?: return@Button
                    onSubmit(currency, if (add) amountUnits else -amountUnits, reason)
                },
                enabled = selected != null && units != null && units > 0 && reason.isNotBlank(),
            ) { Text("确认调整") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun CreateAnnouncementDialog(
    onDismiss: () -> Unit,
    onCreate: (title: String, content: String, status: String, notifyMode: String) -> Unit,
) {
    var title by rememberSaveable { mutableStateOf("") }
    var content by rememberSaveable { mutableStateOf("") }
    var status by rememberSaveable { mutableStateOf("draft") }
    var notifyMode by rememberSaveable { mutableStateOf("silent") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("创建公告") },
        text = {
            Column(
                modifier = Modifier.heightIn(max = 460.dp).verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text("快捷公告面向全部符合条件的用户。", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                OutlinedTextField(title, { title = it }, label = { Text("标题") }, singleLine = true)
                OutlinedTextField(content, { content = it }, label = { Text("内容") }, minLines = 4)
                Text("发布状态", style = MaterialTheme.typography.labelLarge)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    listOf("draft" to "草稿", "active" to "发布").forEach { (value, label) ->
                        AssistChip(
                            onClick = { status = value },
                            label = { Text(label) },
                            leadingIcon = if (status == value) ({ Icon(Icons.Outlined.CheckCircle, null, Modifier.size(16.dp)) }) else null,
                            colors = if (status == value) AssistChipDefaults.assistChipColors(containerColor = MaterialTheme.colorScheme.secondaryContainer) else AssistChipDefaults.assistChipColors(),
                        )
                    }
                }
                Text("通知方式", style = MaterialTheme.typography.labelLarge)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    listOf("silent" to "静默", "popup" to "弹窗").forEach { (value, label) ->
                        AssistChip(
                            onClick = { notifyMode = value },
                            label = { Text(label) },
                            leadingIcon = if (notifyMode == value) ({ Icon(Icons.Outlined.CheckCircle, null, Modifier.size(16.dp)) }) else null,
                            colors = if (notifyMode == value) AssistChipDefaults.assistChipColors(containerColor = MaterialTheme.colorScheme.secondaryContainer) else AssistChipDefaults.assistChipColors(),
                        )
                    }
                }
            }
        },
        confirmButton = {
            Button(onClick = { onCreate(title, content, status, notifyMode) }, enabled = title.isNotBlank() && content.isNotBlank()) {
                Text(if (status == "active") "发布" else "保存草稿")
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

internal fun amountToUnits(value: String, scale: Int): Long? = runCatching {
    val units = BigDecimal(value.trim())
        .multiply(BigDecimal.valueOf(scale.toLong()))
        .toBigIntegerExact()
    require(units >= BigInteger.valueOf(Long.MIN_VALUE) && units <= BigInteger.valueOf(Long.MAX_VALUE))
    units.toLong()
}.getOrNull()

@Composable
private fun BalanceDialog(user: AdminUserSummary, onDismiss: () -> Unit, onSubmit: (String, String, String) -> Unit) {
    var amount by rememberSaveable { mutableStateOf("") }
    var notes by rememberSaveable { mutableStateOf("") }
    var operation by rememberSaveable { mutableStateOf("add") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("调整余额") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(user.email, style = MaterialTheme.typography.bodySmall)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    listOf("add" to "增加", "subtract" to "扣减", "set" to "设为").forEach { (value, label) ->
                        AssistChip(
                            onClick = { operation = value },
                            label = { Text(label) },
                            leadingIcon = if (operation == value) ({ Icon(Icons.Outlined.CheckCircle, null, Modifier.size(16.dp)) }) else null,
                            colors = if (operation == value) AssistChipDefaults.assistChipColors(containerColor = MaterialTheme.colorScheme.secondaryContainer) else AssistChipDefaults.assistChipColors(),
                        )
                    }
                }
                OutlinedTextField(amount, { amount = it }, label = { Text("金额") }, singleLine = true, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal))
                OutlinedTextField(notes, { notes = it }, label = { Text("备注（可选）") }, minLines = 2)
            }
        },
        confirmButton = {
            Button(
                onClick = { onSubmit(amount, operation, notes) },
                enabled = amount.toDoubleOrNull()?.let { it > 0.0 } == true,
            ) { Text("确认") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun ConfirmDialog(
    title: String,
    message: String,
    confirmLabel: String,
    destructive: Boolean,
    onDismiss: () -> Unit,
    onConfirm: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = { Text(message) },
        confirmButton = {
            Button(
                onClick = onConfirm,
                colors = if (destructive) androidx.compose.material3.ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.error) else androidx.compose.material3.ButtonDefaults.buttonColors(),
            ) { Text(confirmLabel) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun SectionTitle(text: String) {
    Text(text, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold, modifier = Modifier.padding(top = 8.dp, bottom = 2.dp))
}

@Composable
private fun StatusPill(label: String, positive: Boolean) {
    val color = if (positive) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.error
    AssistChip(
        onClick = {},
        label = { Text(label, style = MaterialTheme.typography.labelSmall) },
        colors = AssistChipDefaults.assistChipColors(
            labelColor = color,
            containerColor = color.copy(alpha = 0.12f),
        ),
        border = null,
    )
}

@Composable
private fun ErrorBanner(message: String) {
    Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.errorContainer)) {
        Row(Modifier.fillMaxWidth().padding(14.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.WarningAmber, null, tint = MaterialTheme.colorScheme.error)
            Text(message, color = MaterialTheme.colorScheme.onErrorContainer, style = MaterialTheme.typography.bodySmall)
        }
    }
}

@Composable
private fun LoadingScreen(label: String) {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp)) {
            CircularProgressIndicator()
            Text(label, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun InlineLoading() {
    Row(Modifier.fillMaxWidth().padding(20.dp), horizontalArrangement = Arrangement.Center) { CircularProgressIndicator(Modifier.size(28.dp), strokeWidth = 2.dp) }
}

@Composable
private fun ErrorScreen(message: String, onRetry: () -> Unit) {
    Box(Modifier.fillMaxSize().padding(24.dp), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Icon(Icons.Outlined.ErrorOutline, null, tint = MaterialTheme.colorScheme.error, modifier = Modifier.size(36.dp))
            Text(message, color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedButton(onClick = onRetry) { Text("重试") }
        }
    }
}

@Composable
private fun EmptyCard(message: String) {
    Card {
        Text(message, modifier = Modifier.fillMaxWidth().padding(28.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

private fun money(value: Double): String = NumberFormat.getCurrencyInstance(Locale.US).format(value)
private fun formatNumber(value: Long): String = NumberFormat.getIntegerInstance(Locale.US).format(value)
private fun formatUnits(units: Long, scale: Int): String = String.format(Locale.US, "%.2f", units.toDouble() / scale.coerceAtLeast(1))
