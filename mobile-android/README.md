# S2AX Mobile

原生 Kotlin / Jetpack Compose 管理端，采用固定的 Material 3 深浅色方案。它直接调用当前项目的 `/api/v1` 接口，覆盖登录与 TOTP、用户概览、密钥、用量、公告、虚拟货币、账号分配，以及管理员的账号、用户、分组、分配策略、虚拟货币和公告快捷操作。

## 构建

Android SDK 使用 `D:\sdk\Android`，JDK 使用本机的 `D:\Java\jdk-21`：

```bash
JAVA_HOME=/d/Java/jdk-21 ANDROID_HOME=/d/sdk/Android ./gradlew.bat :app:assembleDebug
```

生成的 APK 位于 `app/build/outputs/apk/debug/app-debug.apk`。已连接设备时可用：

```bash
ADB=/d/sdk/Android/platform-tools/adb.exe
$ADB install -r app/build/outputs/apk/debug/app-debug.apk
```

首次进入应用时填写服务地址。应用只接受 HTTPS 地址；例如 `https://api.example.com` 或 `https://api.example.com/api/v1`。访问本地或内网实例时，请通过 TLS 反向代理暴露，不降低应用的明文传输限制。

## 范围

- 用户：概览、API Key、使用记录、公告一键已读、虚拟货币钱包、管理员分配给自己的账号摘要。
- 管理员：系统概览、账号测试/启停/恢复/刷新/清错、用户启停与余额/虚拟货币调整、分组启停、账号分配策略启停与立即补齐、虚拟货币创建/发布/账本核对/过期冻结清理，以及公告草稿/发布/归档/删除。
- 工作台：按权限汇总网页端的服务、资产、运营、资源、计费和治理数据；支持模块搜索、分页、筛选、核心指标卡、趋势折线、模型/分组分布和逐层数据详情预览。字段会按移动端语义显示时间、毫秒与字节单位，系统返回的模块路径固定在应用内，不允许输入任意管理接口。
- 适配：手机端采用窄屏优先的 Material 3 布局；指标、余额和数据页操作会在窄屏自动换行，在较宽窗口中自动增大边距并限制内容宽度，避免数据页横向撑满。
- 隐私：通用预览会隐藏密码、密钥、凭据、Cookie 和访问令牌字段，并对 JWT、PAT、Bearer 值及带敏感查询参数的 URL 进行二次脱敏。
- 凭据：访问与刷新令牌使用 Android Keystore 加密存储；应用不请求或展示上游账号原始凭据，也不提供账号数据导出。
