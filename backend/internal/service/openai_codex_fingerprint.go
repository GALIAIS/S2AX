package service

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// codexFingerprintIDsContextKey 是暂存在 gin context 的收敛 ID 集合键。
// 由 Forward（非透传）或 forwardOpenAIPassthrough（透传）解析后写入，请求
// 构造器读取用于出站头改写——请求体与出站头必须共享同一份 IDs，保证
// turn_id 等随机字段一致。
const codexFingerprintIDsContextKey = "codex_fingerprint_ids"

// stageCodexFingerprintIDs 将本 attempt 解析出的收敛 ID 暂存到 gin context。
// 必须无条件覆写（含 nil）：failover 从收敛账号切到 off 账号时，上一账号的
// IDs 不得残留并被误应用到新账号的出站头（typed-nil 由应用侧 nil 守卫吸收）。
func stageCodexFingerprintIDs(c *gin.Context, ids *codexFingerprintIDs) {
	if c != nil {
		c.Set(codexFingerprintIDsContextKey, ids)
	}
}

func stagedCodexFingerprintIDs(c *gin.Context, account *Account) *codexFingerprintIDs {
	if c == nil || account == nil || !account.UsesOpenAICodexProtocol() {
		return nil
	}
	value, ok := c.Get(codexFingerprintIDsContextKey)
	if !ok {
		return nil
	}
	ids, ok := value.(*codexFingerprintIDs)
	if !ok || ids == nil || ids.accountID != account.ID {
		return nil
	}
	return ids
}

// applyStagedCodexFingerprintHeaders 读取 context 暂存的收敛 ID 并改写出站头。
// 非透传与透传两个请求构造器共用本函数，防止应用语义漂移。仅解析该
// snapshot 的 OpenAI OAuth-like 账号可读取，避免 stale context 跨账号 failover 泄漏。
func applyStagedCodexFingerprintHeaders(c *gin.Context, account *Account, h http.Header) {
	applyCodexFingerprintHeaders(h, stagedCodexFingerprintIDs(c, account))
}

// applyStagedCodexFingerprintHeadersForTransport 是出站构造器使用的路径感知投影。
func applyStagedCodexFingerprintHeadersForTransport(c *gin.Context, account *Account, h http.Header, compact bool) {
	applyCodexFingerprintHeadersForTransport(h, stagedCodexFingerprintIDs(c, account), compact)
}

func applyStagedCodexFingerprintClientMetadata(c *gin.Context, account *Account, reqBody map[string]any) bool {
	return applyCodexFingerprintClientMetadata(reqBody, stagedCodexFingerprintIDs(c, account))
}

// codexFingerprintMode 控制 OpenAI OAuth-like 账号（OAuth/SetupToken）出站请求的
// 设备指纹收敛强度。多人共享同一账号时，每个用户的 Codex 客户端会携带各自不同的
// installation_id / session_id / thread_id，上游据此判定设备数和会话数。
// 收敛模式将这些标识改写为账号级恒定值，减少上游可见的设备/会话指纹。
type codexFingerprintMode string

const (
	// codexFingerprintOff 不做任何收敛，原样透传客户端标识。
	// 这是默认值：收敛是显式 opt-in 的（见 GetCodexFingerprintMode）。
	codexFingerprintOff codexFingerprintMode = "off"
	// codexFingerprintDevice 仅收敛 installation_id 为账号级恒定值。
	// 上游看到 1 台设备 + 多会话（每用户各自的 session）。
	codexFingerprintDevice codexFingerprintMode = "device"
	// codexFingerprintSession 收敛 installation_id + session_id，
	// thread_id 按客户端原始 session-id 确定性派生（每个真实 Codex 会话一个独立线程）。
	// 上游看到 1 台设备 + 1 会话 + N 线程，最接近正常用户 spawn 子代理的模式。
	codexFingerprintSession codexFingerprintMode = "session"
	// codexFingerprintFull 收敛所有标识：installation_id + session_id + thread_id。
	// 上游看到 1 台设备 + 1 会话 + 1 线程，最激进。
	codexFingerprintFull codexFingerprintMode = "full"
)

const (
	codexFingerprintModeExtraKey = "codex_fingerprint_mode"
	codexFingerprintSeedExtraKey = "codex_fingerprint_seed"
)

func canonicalCodexFingerprintSeed(value any) (string, bool) {
	raw, ok := value.(string)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(raw)
	parsed, err := uuid.Parse(trimmed)
	if err != nil || parsed == uuid.Nil || trimmed != parsed.String() {
		return "", false
	}
	return trimmed, true
}

// newCodexFingerprintSeed 为每个开启收敛的账号生成独立种子。使用 UUIDv7
// 让后续从种子派生的稳定 UUID 也能继承一次真实的 Unix-ms 创建时间。
func newCodexFingerprintSeed() string {
	return uuid.Must(uuid.NewV7()).String()
}

func stripCodexFingerprintSeed(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	stripped := maps.Clone(extra)
	delete(stripped, codexFingerprintSeedExtraKey)
	return stripped
}

func codexFingerprintModeFromExtra(extra map[string]any) codexFingerprintMode {
	if extra == nil {
		return codexFingerprintOff
	}
	raw, _ := extra[codexFingerprintModeExtraKey].(string)
	switch codexFingerprintMode(strings.TrimSpace(raw)) {
	case codexFingerprintOff, codexFingerprintDevice, codexFingerprintSession, codexFingerprintFull:
		return codexFingerprintMode(strings.TrimSpace(raw))
	default:
		return codexFingerprintOff
	}
}

func codexFingerprintModeRequiresSeed(mode codexFingerprintMode) bool {
	switch mode {
	case codexFingerprintDevice, codexFingerprintSession, codexFingerprintFull:
		return true
	default:
		return false
	}
}

func codexFingerprintSeed(extra map[string]any) (string, bool) {
	if extra == nil {
		return "", false
	}
	return canonicalCodexFingerprintSeed(extra[codexFingerprintSeedExtraKey])
}

func prepareCodexFingerprintExtraForCreate(platform, accountType string, extra map[string]any) map[string]any {
	prepared := stripCodexFingerprintSeed(extra)
	if platform != PlatformOpenAI || (accountType != AccountTypeOAuth && accountType != AccountTypeSetupToken) || !codexFingerprintModeRequiresSeed(codexFingerprintModeFromExtra(prepared)) {
		return prepared
	}
	if prepared == nil {
		prepared = make(map[string]any, 1)
	}
	prepared[codexFingerprintSeedExtraKey] = newCodexFingerprintSeed()
	return prepared
}

func prepareCodexFingerprintExtraForUpdate(account *Account, extra map[string]any) map[string]any {
	prepared := stripCodexFingerprintSeed(extra)
	if account == nil || !account.IsOpenAIOAuthLike() {
		return prepared
	}
	if seed, ok := codexFingerprintSeed(account.Extra); ok {
		if prepared == nil {
			prepared = make(map[string]any, 1)
		}
		prepared[codexFingerprintSeedExtraKey] = seed
		return prepared
	}
	if codexFingerprintModeRequiresSeed(codexFingerprintModeFromExtra(prepared)) {
		if prepared == nil {
			prepared = make(map[string]any, 1)
		}
		prepared[codexFingerprintSeedExtraKey] = newCodexFingerprintSeed()
	}
	return prepared
}

func sanitizedCodexFingerprintExtraUpdates(updates map[string]any) map[string]any {
	if updates == nil {
		return nil
	}
	sanitized := maps.Clone(updates)
	delete(sanitized, codexFingerprintSeedExtraKey)
	return sanitized
}

// ShouldEnsureCodexFingerprintSeedForExtraUpdates reports whether a JSONB key-level
// extra update is enabling Codex fingerprint convergence and therefore must atomically
// preserve or create the system-managed per-account seed in the repository update.
func ShouldEnsureCodexFingerprintSeedForExtraUpdates(updates map[string]any) bool {
	if updates == nil {
		return false
	}
	return codexFingerprintModeRequiresSeed(codexFingerprintModeFromExtra(updates))
}

// GetCodexFingerprintMode 从账号 extra JSON 读取指纹收敛模式。
//
// **收敛是显式 opt-in**：未设置、空值或非法值一律按 off 处理，只有管理员
// 明确配置 device / session / full 才收敛。
//
// 历史：v0.1.175（#5553）把缺省值当作 session，导致升级后存量 OAuth-like 账号
// （普遍没有这个 extra 键）的每个非透传请求都被静默改写 installation /
// session / thread / turn / window 五类标识；#5555、#5556、#5582 报告的额度
// 缩水都卡在该版本边界，并有"回退 v0.1.173 即恢复"与"新账号开收敛后降额"
// 的 A/B 实测。上游的配额判定策略不可观测，因此这里取兼容安全的一侧：
// 不显式 opt-in 就保持 v0.1.175 之前的客户端身份（#5610）。
func (a *Account) GetCodexFingerprintMode() codexFingerprintMode {
	if a == nil || !a.IsOpenAIOAuthLike() {
		return codexFingerprintOff
	}
	return codexFingerprintModeFromExtra(a.Extra)
}

// deriveStableUUIDv4 从种子确定性派生一个 UUIDv4 格式的字符串。
// installation_id 遵循 Codex CLI 的持久化 UUIDv4 形态；同一种子永远返回同一值。
func deriveStableUUIDv4(seed string) string {
	h := sha256.Sum256([]byte(seed))
	b := h[:16]
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 1
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(b[0:4]),
		binary.BigEndian.Uint16(b[4:6]),
		binary.BigEndian.Uint16(b[6:8]),
		binary.BigEndian.Uint16(b[8:10]),
		b[10:16])
}

// deriveStableUUIDv7 从种子确定性派生一个符合 UUIDv7 布局的字符串。
// Codex CLI 的 session/thread/context-window 标识均使用 UUIDv7。网关需要在
// 重试、账号切换和进程重启后保持同一收敛关系，因此保留种子 UUIDv7 的真实
// Unix-ms 时间位，并只用哈希填充 rand_a/rand_b；历史 UUIDv4 种子则使用稳定的
// 有界时间回退，避免把随机哈希的高 48 位误当成时间戳。
func deriveStableUUIDv7(seed string) string {
	h := sha256.Sum256([]byte(seed))
	timestamp := stableUUIDv7Timestamp(seed, h)
	var b [16]byte
	b[0] = byte(timestamp >> 40)
	b[1] = byte(timestamp >> 32)
	b[2] = byte(timestamp >> 24)
	b[3] = byte(timestamp >> 16)
	b[4] = byte(timestamp >> 8)
	b[5] = byte(timestamp)
	b[6] = (h[6] & 0x0f) | 0x70
	b[7] = h[7]
	b[8] = (h[8] & 0x3f) | 0x80
	copy(b[9:], h[9:])
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(b[0:4]),
		binary.BigEndian.Uint16(b[4:6]),
		binary.BigEndian.Uint16(b[6:8]),
		binary.BigEndian.Uint16(b[8:10]),
		b[10:16])
}

// stableUUIDv7Timestamp 返回用于确定性 UUIDv7 的 48 位 Unix-ms 时间戳。
// 新种子本身是 UUIDv7，直接继承其创建时间；旧版本已经落库的 UUIDv4 种子没有
// 时间信息，因此映射到固定的 2020-2030 区间，保证跨进程稳定且仍是合法时间位。
func stableUUIDv7Timestamp(seed string, hash [32]byte) uint64 {
	if parsed, err := uuid.Parse(strings.TrimSpace(seed)); err == nil && parsed.Version() == 7 {
		return uint64(parsed[0])<<40 |
			uint64(parsed[1])<<32 |
			uint64(parsed[2])<<24 |
			uint64(parsed[3])<<16 |
			uint64(parsed[4])<<8 |
			uint64(parsed[5])
	}
	const (
		stableUUIDv7FallbackStartMs = uint64(1577836800000) // 2020-01-01T00:00:00Z
		stableUUIDv7FallbackSpanMs  = uint64(315532800000)  // 10 年（含一个闰日）
	)
	return stableUUIDv7FallbackStartMs + binary.BigEndian.Uint64(hash[:8])%stableUUIDv7FallbackSpanMs
}

// resolveConvergedInstallationID 返回账号级恒定的 installation_id。
// 优先使用管理员配置的真实 device_id，无则从系统管理的账号随机种子确定性派生。
func resolveConvergedInstallationID(account *Account, seed string) string {
	if account == nil {
		return ""
	}
	if deviceID := account.GetOpenAIDeviceID(); deviceID != "" {
		return deviceID
	}
	if seed == "" {
		return ""
	}
	return deriveStableUUIDv4("sub2api:codex-install-id:v2:" + seed)
}

// resolveConvergedSessionID 返回账号级恒定的 session_id。
func resolveConvergedSessionID(seed string) string {
	if seed == "" {
		return ""
	}
	return deriveStableUUIDv7("sub2api:codex-session-id:v3:" + seed)
}

// resolveConvergedChildThreadID 按客户端原始 session/thread 确定性派生子线程 ID。
// 根线程在官方 Codex 中满足 session_id == thread_id；只有请求明确携带不同的
// thread_id 时才创建此映射，避免把每个根请求误判成子线程。
func resolveConvergedChildThreadID(seed, clientSessionID, clientThreadID string) string {
	if seed == "" || clientSessionID == "" || clientThreadID == "" {
		return ""
	}
	return deriveStableUUIDv7("sub2api:codex-child-thread:v3:" + seed + ":" + clientSessionID + ":" + clientThreadID)
}

// resolveConvergedWindowID 生成官方 Codex 的 window_id。该字段不是 UUID，真实
// 客户端把当前 thread ID 与 context window 编号拼成 `<thread_id>:<window_number>`；
// UUIDv7 只用于同一 metadata 中独立的 context_window_id 字段。
func resolveConvergedWindowID(_ string, threadID string, windowNumber uint64) string {
	if threadID == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", threadID, windowNumber)
}

// codexFingerprintRequestIdentity 汇总一次请求中由客户端提供的 Codex 会话信息。
// 官方 CLI 同时把身份放在标准请求头、client_metadata 平铺字段和内嵌的
// x-codex-turn-metadata 中；window_id 是 `<thread_id>:<window_number>`，
// window_number 同时作为独立字段传输；读取时按“标准头优先、body 补充”的顺序合并。
type codexFingerprintRequestIdentity struct {
	sessionID       string
	threadID        string
	windowID        string
	windowNumber    uint64
	hasWindowNumber bool
	contextWindowID string
	requestKind     string
}

func codexFingerprintStringValue(value any) string {
	valueString, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(valueString)
}

func codexFingerprintUint64Value(value any) (uint64, bool) {
	switch typed := value.(type) {
	case uint64:
		return typed, true
	case uint:
		return uint64(typed), true
	case int:
		if typed >= 0 {
			return uint64(typed), true
		}
	case int64:
		if typed >= 0 {
			return uint64(typed), true
		}
	case float64:
		if typed >= 0 && typed == float64(uint64(typed)) {
			return uint64(typed), true
		}
	case json.Number:
		parsed, err := strconv.ParseUint(typed.String(), 10, 64)
		if err == nil {
			return parsed, true
		}
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// codexFingerprintWindowNumberFromID 兼容解析旧版代理曾生成的 `<thread_id>:<number>`
// 形式。官方 Codex 的 window_id 是 UUID，正常请求会从独立的 window_number 字段读取。
func codexFingerprintWindowNumberFromID(windowID string) (uint64, bool) {
	windowID = strings.TrimSpace(windowID)
	separator := strings.LastIndexByte(windowID, ':')
	if separator < 0 || separator == len(windowID)-1 {
		return 0, false
	}
	return codexFingerprintUint64Value(windowID[separator+1:])
}

func mergeCodexFingerprintRequestIdentity(dst *codexFingerprintRequestIdentity, src codexFingerprintRequestIdentity) {
	if dst == nil {
		return
	}
	if dst.sessionID == "" {
		dst.sessionID = src.sessionID
	}
	if dst.threadID == "" {
		dst.threadID = src.threadID
	}
	if dst.windowID == "" {
		dst.windowID = src.windowID
	}
	if !dst.hasWindowNumber && src.hasWindowNumber {
		dst.windowNumber = src.windowNumber
		dst.hasWindowNumber = true
	}
	if dst.contextWindowID == "" {
		dst.contextWindowID = src.contextWindowID
	}
	if dst.requestKind == "" {
		dst.requestKind = src.requestKind
	}
}

func codexFingerprintRequestIdentityFromMetadataMap(metadata map[string]any) codexFingerprintRequestIdentity {
	identity := codexFingerprintRequestIdentity{}
	if metadata == nil {
		return identity
	}
	identity.sessionID = codexFingerprintStringValue(metadata["session_id"])
	identity.threadID = codexFingerprintStringValue(metadata["thread_id"])
	identity.windowID = codexFingerprintStringValue(metadata["x-codex-window-id"])
	if identity.windowID == "" {
		identity.windowID = codexFingerprintStringValue(metadata["window_id"])
	}
	if windowNumber, ok := codexFingerprintUint64Value(metadata["window_number"]); ok {
		identity.windowNumber = windowNumber
		identity.hasWindowNumber = true
	} else if windowNumber, ok := codexFingerprintWindowNumberFromID(identity.windowID); ok {
		identity.windowNumber = windowNumber
		identity.hasWindowNumber = true
	}
	identity.contextWindowID = codexFingerprintStringValue(metadata["context_window_id"])
	identity.requestKind = codexFingerprintStringValue(metadata["request_kind"])
	return identity
}

func codexFingerprintRequestIdentityFromEmbeddedMetadata(raw string) codexFingerprintRequestIdentity {
	metadata := make(map[string]any)
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &metadata) != nil {
		return codexFingerprintRequestIdentity{}
	}
	return codexFingerprintRequestIdentityFromMetadataMap(metadata)
}

func codexFingerprintRequestIdentityFromHeaders(headers http.Header) codexFingerprintRequestIdentity {
	identity := codexFingerprintRequestIdentity{}
	if headers == nil {
		return identity
	}
	identity.sessionID = extractClientSessionID(headers)
	identity.threadID = extractClientThreadID(headers)
	identity.windowID = strings.TrimSpace(headers.Get("x-codex-window-id"))
	if windowNumber, ok := codexFingerprintWindowNumberFromID(identity.windowID); ok {
		identity.windowNumber = windowNumber
		identity.hasWindowNumber = true
	}
	mergeCodexFingerprintRequestIdentity(&identity, codexFingerprintRequestIdentityFromEmbeddedMetadata(headers.Get("x-codex-turn-metadata")))
	return identity
}

func codexFingerprintRequestIdentityFromMap(body map[string]any) codexFingerprintRequestIdentity {
	identity := codexFingerprintRequestIdentity{}
	if body == nil {
		return identity
	}
	clientMetadata, _ := body["client_metadata"].(map[string]any)
	if clientMetadata == nil {
		if stringMetadata, ok := body["client_metadata"].(map[string]string); ok {
			clientMetadata = make(map[string]any, len(stringMetadata))
			for key, value := range stringMetadata {
				clientMetadata[key] = value
			}
		}
	}
	if clientMetadata == nil {
		return identity
	}
	identity = codexFingerprintRequestIdentityFromMetadataMap(clientMetadata)
	if embedded, ok := clientMetadata[openAIWSTurnMetadataHeader].(string); ok {
		mergeCodexFingerprintRequestIdentity(&identity, codexFingerprintRequestIdentityFromEmbeddedMetadata(embedded))
	}
	return identity
}

func codexFingerprintRequestIdentityFromRaw(body []byte) codexFingerprintRequestIdentity {
	identity := codexFingerprintRequestIdentity{}
	if len(body) == 0 {
		return identity
	}
	clientMetadata := gjson.GetBytes(body, "client_metadata")
	if !clientMetadata.IsObject() {
		return identity
	}
	identity.sessionID = strings.TrimSpace(gjson.GetBytes(body, "client_metadata.session_id").String())
	identity.threadID = strings.TrimSpace(gjson.GetBytes(body, "client_metadata.thread_id").String())
	identity.windowID = strings.TrimSpace(gjson.GetBytes(body, "client_metadata.x-codex-window-id").String())
	if identity.windowID == "" {
		identity.windowID = strings.TrimSpace(gjson.GetBytes(body, "client_metadata.window_id").String())
	}
	if windowNumber := gjson.GetBytes(body, "client_metadata.window_number"); windowNumber.Exists() {
		if parsed, ok := codexFingerprintUint64Value(windowNumber.Raw); ok {
			identity.windowNumber = parsed
			identity.hasWindowNumber = true
		}
	}
	if !identity.hasWindowNumber {
		if parsed, ok := codexFingerprintWindowNumberFromID(identity.windowID); ok {
			identity.windowNumber = parsed
			identity.hasWindowNumber = true
		}
	}
	identity.contextWindowID = strings.TrimSpace(gjson.GetBytes(body, "client_metadata.context_window_id").String())
	if embedded := strings.TrimSpace(gjson.GetBytes(body, "client_metadata.x-codex-turn-metadata").String()); embedded != "" {
		mergeCodexFingerprintRequestIdentity(&identity, codexFingerprintRequestIdentityFromEmbeddedMetadata(embedded))
	}
	return identity
}

// codexFingerprintIDs 收敛后的完整 ID 集合。
// 由 resolveCodexFingerprintIDs 一次性生成，同一个实例在头改写和体改写之间共享，
// 确保所有载体中的 turn_id 等随机字段一致。体改写时还会补记原始
// client_metadata.session_id，用于识别 root prompt_cache_key 的默认值。
type codexFingerprintIDs struct {
	accountID                     int64
	mode                          codexFingerprintMode
	seed                          string
	clientSessionID               string
	clientThreadID                string
	installationID                string
	sessionID                     string
	threadID                      string
	turnID                        string
	windowID                      string
	windowNumber                  uint64
	contextWindowID               string
	requestKind                   string
	turnStartedAtUnixMs           int64
	originalBodySessionID         string
	originalBodySessionIDCaptured bool
	lineageValues                 map[string]string
	lineageOutputs                map[string]struct{}
}

// resolveCodexFingerprintIDs 按收敛模式计算出站 ID 集合。
// clientSessionID 是客户端原始的 session-id 头值（连字符形式），用于 session 模式下
// 的 thread_id 派生——每个真实 Codex 会话得到一个独立线程。
// 返回 nil 表示 off 模式，不需要改写。
// 注意：包含随机生成的 turn_id，调用方必须只调用一次并共享结果给头改写和体改写。
func resolveCodexFingerprintIDs(account *Account, clientSessionID string, mode codexFingerprintMode) *codexFingerprintIDs {
	return resolveCodexFingerprintIDsWithSource(account, account, clientSessionID, mode)
}

// resolveCodexFingerprintIDsWithSource 使用 selected account 记录本次 attempt 的归属，
// 使用 credential source 读取 seed/device 配置。影子账号不持有凭据，必须沿用母账号的
// 收敛配置，但 accountID 仍保留影子账号 ID，避免 failover 时旧快照被新账号误用。
func resolveCodexFingerprintIDsWithSource(selectedAccount, credentialSource *Account, clientSessionID string, mode codexFingerprintMode) *codexFingerprintIDs {
	return resolveCodexFingerprintIDsWithRequestIdentity(
		selectedAccount,
		credentialSource,
		codexFingerprintRequestIdentity{sessionID: clientSessionID},
		mode,
		true,
	)
}

// resolveCodexFingerprintIDsWithRequestIdentity 按官方 Codex CLI 的身份层级生成快照。
// session 模式共享账号级根 session，但只有输入明确表示 child thread 时才派生新的
// thread；因此 root 请求仍保持 `session_id == thread_id`。window/context-window
// 则按客户端窗口编号稳定映射，避免每个 turn 错误地创建新上下文。
func resolveCodexFingerprintIDsWithRequestIdentity(
	selectedAccount, credentialSource *Account,
	identity codexFingerprintRequestIdentity,
	mode codexFingerprintMode,
	includeTurn bool,
) *codexFingerprintIDs {
	if selectedAccount == nil || mode == codexFingerprintOff {
		return nil
	}
	if credentialSource == nil {
		credentialSource = selectedAccount
	}
	seed, ok := codexFingerprintSeed(credentialSource.Extra)
	if !ok {
		return nil
	}

	ids := &codexFingerprintIDs{
		accountID:       selectedAccount.ID,
		mode:            mode,
		seed:            seed,
		clientSessionID: identity.sessionID,
		clientThreadID:  identity.threadID,
		windowNumber:    identity.windowNumber,
		requestKind:     identity.requestKind,
	}

	ids.installationID = resolveConvergedInstallationID(credentialSource, seed)
	if ids.installationID == "" {
		return nil
	}

	switch mode {
	case codexFingerprintDevice:
		return ids

	case codexFingerprintSession:
		ids.sessionID = resolveConvergedSessionID(seed)
		ids.threadID = ids.sessionID
		if identity.threadID != "" && identity.threadID != identity.sessionID {
			ids.threadID = resolveConvergedChildThreadID(seed, identity.sessionID, identity.threadID)
		}

	case codexFingerprintFull:
		ids.sessionID = resolveConvergedSessionID(seed)
		ids.threadID = ids.sessionID
	}

	if mode != codexFingerprintSession && mode != codexFingerprintFull {
		return ids
	}
	if !identity.hasWindowNumber {
		ids.windowNumber = 0
	}
	ids.windowID = resolveConvergedWindowID(seed, ids.threadID, ids.windowNumber)
	ids.contextWindowID = deriveStableUUIDv7(fmt.Sprintf(
		"sub2api:codex-context-window:v4:%s:%s:%s",
		seed,
		ids.threadID,
		ids.windowID,
	))
	if includeTurn {
		ids.turnID = uuid.Must(uuid.NewV7()).String()
		ids.turnStartedAtUnixMs = time.Now().UnixMilli()
	}
	if ids.requestKind == "" {
		ids.requestKind = "turn"
	}
	return ids
}

// extractClientSessionID 从请求头中提取客户端原始的会话标识。
// 优先取 session-id（连字符形式，Codex CLI 标准），回退到 session_id（下划线形式）。
// 返回的值尚未被 isolateOpenAISessionID 改写，是客户端的真实标识。
func extractClientSessionID(h http.Header) string {
	if h == nil {
		return ""
	}
	if v := strings.TrimSpace(h.Get("session-id")); v != "" {
		return v
	}
	return strings.TrimSpace(h.Get("session_id"))
}

// extractClientThreadID 提取客户端用于请求关联的原始 thread 标识。它只用于 session
// 模式下保持 parent_thread_id 的血缘关系，不会把该值误当成当前 turn_id。
func extractClientThreadID(h http.Header) string {
	if h == nil {
		return ""
	}
	if v := strings.TrimSpace(h.Get("thread-id")); v != "" {
		return v
	}
	return strings.TrimSpace(h.Get("x-client-request-id"))
}

// resolveCodexFingerprintIDsFromRequest 从客户端原始请求头中提取 session-id，
// 结合账号配置一次性解析收敛 ID 集合。调用方应将返回的 ids 同时传给
// applyCodexFingerprintHeaders 和 applyCodexFingerprintClientMetadata。
func resolveCodexFingerprintIDsFromRequest(account *Account, clientHeaders http.Header) *codexFingerprintIDs {
	return resolveCodexFingerprintIDsFromRequestWithSource(account, account, clientHeaders)
}

// resolveCodexFingerprintIDsFromRequestWithSource 按本次选中的账号和实际凭据源解析
// 收敛快照。普通账号两者相同；影子账号使用母账号的 seed/device，但快照归属仍指向
// 当前选中行，保证上下文中的 fingerprint 只能被当前 attempt 使用。
func resolveCodexFingerprintIDsFromRequestWithSource(selectedAccount, credentialSource *Account, clientHeaders http.Header) *codexFingerprintIDs {
	if selectedAccount == nil {
		return nil
	}
	if credentialSource == nil {
		credentialSource = selectedAccount
	}
	mode := credentialSource.GetCodexFingerprintMode()
	if mode == codexFingerprintOff {
		return nil
	}
	return resolveCodexFingerprintIDsWithRequestIdentity(
		selectedAccount,
		credentialSource,
		codexFingerprintRequestIdentityFromHeaders(clientHeaders),
		mode,
		true,
	)
}

func codexFingerprintRequestKind(c *gin.Context, bodyIdentity codexFingerprintRequestIdentity, body map[string]any, raw []byte) string {
	if bodyIdentity.requestKind != "" {
		return bodyIdentity.requestKind
	}
	if c != nil && isOpenAIResponsesCompactPath(c) {
		return "compaction"
	}
	if body != nil {
		if generated, ok := body["generate"].(bool); ok && !generated {
			return "prewarm"
		}
	}
	if len(raw) > 0 && gjson.GetBytes(raw, "generate").Type == gjson.False {
		return "prewarm"
	}
	return "turn"
}

func mergeCodexFingerprintRequestSources(c *gin.Context, bodyIdentity codexFingerprintRequestIdentity) codexFingerprintRequestIdentity {
	identity := codexFingerprintRequestIdentityFromHeaders(nil)
	if c != nil && c.Request != nil {
		identity = codexFingerprintRequestIdentityFromHeaders(c.Request.Header)
	}
	mergeCodexFingerprintRequestIdentity(&identity, bodyIdentity)
	return identity
}

func resolveAndStageCodexFingerprintIDsForIdentity(
	c *gin.Context,
	account *Account,
	identity codexFingerprintRequestIdentity,
	requestKind string,
	includeTurn bool,
) *codexFingerprintIDs {
	if requestKind != "" {
		identity.requestKind = requestKind
	}
	credentialSource := codexAccountIdentitySource(c, account)
	mode := codexFingerprintOff
	if credentialSource != nil {
		mode = credentialSource.GetCodexFingerprintMode()
	}
	ids := resolveCodexFingerprintIDsWithRequestIdentity(account, credentialSource, identity, mode, includeTurn)
	stageCodexFingerprintIDs(c, ids)
	return ids
}

// resolveAndStageCodexFingerprintIDs 为一个出站请求生成并暂存唯一快照。HTTP、ctx-pool
// WS 和 passthrough WS 都通过这里进入，确保头部和 body 不会各自生成不同的 turn_id。
func resolveAndStageCodexFingerprintIDs(c *gin.Context, account *Account) *codexFingerprintIDs {
	identity := codexFingerprintRequestIdentityFromHeaders(nil)
	if c != nil && c.Request != nil {
		identity = codexFingerprintRequestIdentityFromHeaders(c.Request.Header)
	}
	return resolveAndStageCodexFingerprintIDsForIdentity(c, account, identity, identity.requestKind, true)
}

// resolveAndStageCodexFingerprintIDsForMap 从已解码请求体补充标准头缺失的身份字段。
// 普通 Responses 与 compact 共用解析，但 compact 只在头部投影指纹字段。
func resolveAndStageCodexFingerprintIDsForMap(c *gin.Context, account *Account, body map[string]any) *codexFingerprintIDs {
	bodyIdentity := codexFingerprintRequestIdentityFromMap(body)
	identity := mergeCodexFingerprintRequestSources(c, bodyIdentity)
	return resolveAndStageCodexFingerprintIDsForIdentity(
		c,
		account,
		identity,
		codexFingerprintRequestKind(c, bodyIdentity, body, nil),
		true,
	)
}

// resolveAndStageCodexFingerprintIDsForRaw 是 passthrough/WS 热路径使用的等价入口，
// 只解析 client_metadata 小对象，不解码完整 input 历史。
func resolveAndStageCodexFingerprintIDsForRaw(c *gin.Context, account *Account, body []byte) *codexFingerprintIDs {
	bodyIdentity := codexFingerprintRequestIdentityFromRaw(body)
	identity := mergeCodexFingerprintRequestSources(c, bodyIdentity)
	return resolveAndStageCodexFingerprintIDsForIdentity(
		c,
		account,
		identity,
		codexFingerprintRequestKind(c, bodyIdentity, nil, body),
		true,
	)
}

// resolveAndStageCodexFingerprintExistingIDsForRaw 处理 session.update 等非 turn 帧。
// 这些帧可以同步稳定 session/thread/window，但不能凭空创建新的 turn_id。
func resolveAndStageCodexFingerprintExistingIDsForRaw(c *gin.Context, account *Account, body []byte) *codexFingerprintIDs {
	bodyIdentity := codexFingerprintRequestIdentityFromRaw(body)
	identity := mergeCodexFingerprintRequestSources(c, bodyIdentity)
	return resolveAndStageCodexFingerprintIDsForIdentity(c, account, identity, "", false)
}

// applyCodexFingerprintHeaders 按普通 Responses HTTP/WS 请求的官方头集合改写身份。
// compact 请求必须使用 applyCodexFingerprintHeadersForTransport 的 compact=true
// 投影，因为只有 compact 会直接携带 x-codex-installation-id。
func applyCodexFingerprintHeaders(h http.Header, ids *codexFingerprintIDs) {
	applyCodexFingerprintHeadersForTransport(h, ids, false)
}

// applyCodexFingerprintHeadersForTransport 按 Codex CLI 的传输路径投影身份头。
// 普通 Responses/WS：session-id、thread-id、x-client-request-id、x-codex-window-id
// 和 x-codex-turn-metadata；compact 额外携带 x-codex-installation-id。下划线形式
// session_id 与 conversation_id 是兼容桥的内部字段，不应进入原生 Codex 上游。
func applyCodexFingerprintHeadersForTransport(h http.Header, ids *codexFingerprintIDs, compact bool) {
	if h == nil || ids == nil {
		return
	}

	if compact {
		h.Set("x-codex-installation-id", ids.installationID)
		h.Del("x-client-request-id")
	} else {
		h.Del("x-codex-installation-id")
		// device 模式只接管 installation_id，保留前置账号隔离阶段生成的
		// x-client-request-id；session/full 才将其收敛为目标 thread_id。
		if ids.mode != codexFingerprintDevice {
			h.Set("x-client-request-id", ids.threadID)
		}
	}

	if ids.mode == codexFingerprintDevice {
		rewriteCodexTurnMetadataFields(h, map[string]any{
			"installation_id": ids.installationID,
		})
		return
	}

	// session / full 模式：改写官方标准头，并移除兼容桥专用身份头。
	h.Set("x-codex-window-id", ids.windowID)
	h.Set("session-id", ids.sessionID)
	h.Set("thread-id", ids.threadID)
	h.Del("session_id")
	h.Del("conversation_id")
	applyCodexFingerprintLineageHeaders(h, ids)

	metadataFields := map[string]any{
		"installation_id": ids.installationID,
		"session_id":      ids.sessionID,
		"thread_id":       ids.threadID,
		"window_id":       ids.windowID,
		"window_number":   ids.windowNumber,
	}
	if ids.contextWindowID != "" {
		metadataFields["context_window_id"] = ids.contextWindowID
	}
	if ids.turnID != "" {
		metadataFields["turn_id"] = ids.turnID
		metadataFields["turn_started_at_unix_ms"] = ids.turnStartedAtUnixMs
	}
	if ids.requestKind != "" {
		metadataFields["request_kind"] = ids.requestKind
	}
	rewriteCodexTurnMetadataFields(h, metadataFields)
	rewriteCodexTurnMetadataLineageFields(h, ids)
}

var codexFingerprintLineageFields = []struct {
	name string
	kind string
}{
	{name: "x-codex-parent-thread-id", kind: "thread"},
	{name: "forked_from_thread_id", kind: "thread"},
	{name: "parent_thread_id", kind: "thread"},
	{name: "parent_turn_id", kind: "turn"},
	{name: "root_turn_id", kind: "turn"},
}

// convergeCodexFingerprintLineageValue 为线程类和 turn 类 lineage 建立稳定映射，既
// 隐藏原始身份，又保留同一线程或 turn 在多处 metadata 中的相等关系。缓存结果还使
// header、body 以及重试路径重复经过同一快照时保持幂等。
func (ids *codexFingerprintIDs) convergeCodexFingerprintLineageValue(kind, raw string) string {
	if ids == nil {
		return strings.TrimSpace(raw)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if raw == ids.installationID || raw == ids.sessionID || raw == ids.threadID || raw == ids.turnID {
		return raw
	}
	if kind == "thread" && raw == ids.clientThreadID && ids.threadID != "" {
		return ids.threadID
	}
	if ids.lineageValues == nil {
		ids.lineageValues = make(map[string]string)
	}
	if ids.lineageOutputs == nil {
		ids.lineageOutputs = make(map[string]struct{})
	}
	key := kind + "\x00" + raw
	if value, ok := ids.lineageValues[key]; ok {
		return value
	}
	if _, ok := ids.lineageOutputs[raw]; ok {
		return raw
	}
	derive := deriveStableUUIDv4
	if kind == "thread" || kind == "turn" {
		derive = deriveStableUUIDv7
	}
	value := derive("sub2api:codex-lineage:v3:" + ids.seed + ":" + kind + ":" + raw)
	ids.lineageValues[key] = value
	ids.lineageOutputs[value] = struct{}{}
	return value
}

// applyCodexFingerprintLineageHeaders 只处理官方确实使用的 lineage 头及其兼容别名，
// 不把 parent_turn_id/root_turn_id 错写为本次新生成的 turn_id。
func applyCodexFingerprintLineageHeaders(h http.Header, ids *codexFingerprintIDs) {
	if h == nil || ids == nil {
		return
	}
	for _, field := range codexFingerprintLineageFields {
		raw := strings.TrimSpace(h.Get(field.name))
		if raw == "" {
			continue
		}
		h.Set(field.name, ids.convergeCodexFingerprintLineageValue(field.kind, raw))
	}
}

// rewriteCodexTurnMetadataLineageFields 同步 canonical turn metadata 中的 lineage 字段，
// 使它与 direct header projection 使用相同的收敛结果。
func rewriteCodexTurnMetadataLineageFields(h http.Header, ids *codexFingerprintIDs) {
	if h == nil || ids == nil {
		return
	}
	raw := strings.TrimSpace(h.Get("x-codex-turn-metadata"))
	if raw == "" {
		return
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil || metadata == nil {
		metadata = make(map[string]any)
	}
	if !applyCodexFingerprintLineageFields(metadata, ids) {
		return
	}
	if rebuilt, err := json.Marshal(metadata); err == nil {
		h.Set("x-codex-turn-metadata", string(rebuilt))
	}
}

// applyCodexFingerprintLineageFields 改写 client_metadata 或 canonical metadata 中的
// thread/turn lineage 字段；缺失字段保持缺失，避免网关凭空制造不存在的代理血缘。
func applyCodexFingerprintLineageFields(values map[string]any, ids *codexFingerprintIDs) bool {
	if values == nil || ids == nil {
		return false
	}
	changed := false
	for _, field := range codexFingerprintLineageFields {
		raw, ok := values[field.name].(string)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		next := ids.convergeCodexFingerprintLineageValue(field.kind, raw)
		if next != raw {
			values[field.name] = next
			changed = true
		}
	}
	return changed
}

// rewriteClientMetadataEmbeddedLineageFields 同步 client_metadata 内嵌的 canonical
// metadata。非法 JSON 保持现有基础字段重建策略，不额外伪造 parent/root 字段。
func rewriteClientMetadataEmbeddedLineageFields(clientMetadata map[string]any, ids *codexFingerprintIDs) {
	if clientMetadata == nil || ids == nil {
		return
	}
	raw, ok := clientMetadata["x-codex-turn-metadata"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil || metadata == nil {
		return
	}
	if !applyCodexFingerprintLineageFields(metadata, ids) {
		return
	}
	if rebuilt, err := json.Marshal(metadata); err == nil {
		clientMetadata["x-codex-turn-metadata"] = string(rebuilt)
	}
}

// rewriteCodexTurnMetadataFields 解析 x-codex-turn-metadata 头中的 JSON，
// 替换指定字段后回写。合法对象保留未指定字段（如 sandbox、thread_source）；
// 非法/非对象值重建为最小合法 metadata，避免 flat 与 embedded identity 分裂。
func rewriteCodexTurnMetadataFields(h http.Header, fields map[string]any) {
	if h == nil || len(fields) == 0 {
		return
	}
	raw := strings.TrimSpace(h.Get("x-codex-turn-metadata"))
	var metadata map[string]any
	if raw == "" || json.Unmarshal([]byte(raw), &metadata) != nil || metadata == nil {
		metadata = make(map[string]any, len(fields))
	}
	for k, v := range fields {
		metadata[k] = v
	}
	rebuilt, err := json.Marshal(metadata)
	if err != nil {
		return
	}
	h.Set("x-codex-turn-metadata", string(rebuilt))
}

// applyCodexFingerprintClientMetadata 按预计算的收敛 ID 改写请求体中的 client_metadata。
// 使用与头改写相同的 ids 实例，确保 turn_id 等随机字段一致。
func applyCodexFingerprintClientMetadata(reqBody map[string]any, ids *codexFingerprintIDs) bool {
	return applyCodexFingerprintClientMetadataWithOptions(reqBody, ids, true)
}

// applyCodexFingerprintClientMetadataWithoutPromptCache 用于 Chat Completions/Messages
// 兼容桥。该桥自身有独立的 prompt-cache 路由，不应把 Responses 默认字段注入到
// 兼容请求体；其 client_metadata 身份仍需与原生 Codex 路径保持一致。
func applyCodexFingerprintClientMetadataWithoutPromptCache(reqBody map[string]any, ids *codexFingerprintIDs) bool {
	return applyCodexFingerprintClientMetadataWithOptions(reqBody, ids, false)
}

func applyCodexFingerprintClientMetadataWithOptions(reqBody map[string]any, ids *codexFingerprintIDs, injectPromptCacheKey bool) bool {
	if reqBody == nil || ids == nil {
		return false
	}

	captureCodexFingerprintOriginalBodySessionID(ids, reqBody["client_metadata"])
	var existing map[string]any
	switch metadata := reqBody["client_metadata"].(type) {
	case map[string]any:
		existing = metadata
	case map[string]string:
		existing = make(map[string]any, len(metadata))
		for key, value := range metadata {
			existing[key] = value
		}
	default:
		existing = make(map[string]any)
	}

	modified := false
	if applyCodexFingerprintToClientMetadataMap(existing, ids) {
		reqBody["client_metadata"] = existing
		modified = true
	}
	if injectPromptCacheKey && applyCodexFingerprintPromptCacheKey(reqBody, ids) {
		modified = true
	}
	return modified
}

// applyCodexFingerprintToClientMetadataMap 是 client_metadata 改写的共享核心，
// map 版（非透传，body 已解码）与 raw 字节版（透传热路径）都经由它，保证两条
// 路径的收敛语义永不漂移。
func applyCodexFingerprintToClientMetadataMap(existing map[string]any, ids *codexFingerprintIDs) bool {
	if existing == nil || ids == nil {
		return false
	}

	modified := false

	if ids.installationID != "" {
		existing["x-codex-installation-id"] = ids.installationID
		modified = true
	}

	if ids.mode == codexFingerprintDevice {
		rewriteClientMetadataEmbeddedTurnMetadata(existing, map[string]any{
			"installation_id": ids.installationID,
		})
		return modified
	}

	// session / full 模式：平铺字段与内嵌 canonical metadata 必须共享同一快照。
	existing["session_id"] = ids.sessionID
	existing["thread_id"] = ids.threadID
	existing["x-codex-window-id"] = ids.windowID
	if ids.turnID != "" {
		existing["turn_id"] = ids.turnID
	}
	applyCodexFingerprintLineageFields(existing, ids)

	metadataFields := map[string]any{
		"installation_id": ids.installationID,
		"session_id":      ids.sessionID,
		"thread_id":       ids.threadID,
		"window_id":       ids.windowID,
		"window_number":   ids.windowNumber,
	}
	if ids.contextWindowID != "" {
		metadataFields["context_window_id"] = ids.contextWindowID
	}
	if ids.turnID != "" {
		metadataFields["turn_id"] = ids.turnID
		metadataFields["turn_started_at_unix_ms"] = ids.turnStartedAtUnixMs
	}
	if ids.requestKind != "" {
		metadataFields["request_kind"] = ids.requestKind
	}
	rewriteClientMetadataEmbeddedTurnMetadata(existing, metadataFields)
	rewriteClientMetadataEmbeddedLineageFields(existing, ids)
	return true
}

// applyExistingCodexFingerprintFields 仅改写已经存在的身份字段，供 session.update
// 这类非 turn 帧使用。该帧不能凭空新增 session/thread/turn/window，否则会把会话级更新
// 误报成一次新的推理 turn；字段值为非字符串时也按已有键处理，避免畸形身份原样外泄。
func applyExistingCodexFingerprintFields(values map[string]any, ids *codexFingerprintIDs) bool {
	if values == nil || ids == nil {
		return false
	}
	type fingerprintField struct {
		name  string
		value string
	}
	fields := []fingerprintField{
		{name: "installation_id", value: ids.installationID},
		{name: "x-codex-installation-id", value: ids.installationID},
	}
	if ids.mode != codexFingerprintDevice {
		fields = append(fields,
			fingerprintField{name: "session_id", value: ids.sessionID},
			fingerprintField{name: "session-id", value: ids.sessionID},
			fingerprintField{name: "thread_id", value: ids.threadID},
			fingerprintField{name: "thread-id", value: ids.threadID},
			fingerprintField{name: "window_id", value: ids.windowID},
			fingerprintField{name: "x-codex-window-id", value: ids.windowID},
		)
		if ids.turnID != "" {
			fields = append(fields,
				fingerprintField{name: "turn_id", value: ids.turnID},
				fingerprintField{name: "turn-id", value: ids.turnID},
			)
		}
	}

	changed := false
	for _, field := range fields {
		current, exists := values[field.name]
		if !exists {
			continue
		}
		currentString, isString := current.(string)
		if !isString || currentString != field.value {
			values[field.name] = field.value
			changed = true
		}
	}
	if ids.mode != codexFingerprintDevice && applyCodexFingerprintLineageFields(values, ids) {
		changed = true
	}
	if ids.mode != codexFingerprintDevice {
		if _, exists := values["window_number"]; exists && values["window_number"] != ids.windowNumber {
			values["window_number"] = ids.windowNumber
			changed = true
		}
		if ids.contextWindowID != "" {
			if _, exists := values["context_window_id"]; exists && values["context_window_id"] != ids.contextWindowID {
				values["context_window_id"] = ids.contextWindowID
				changed = true
			}
		}
	}
	return changed
}

// applyExistingCodexFingerprintEmbeddedMetadata 同步 session.update 中已有的 canonical
// metadata 字段，但保留其对象形状和缺失字段，避免把普通会话更新扩展成完整 turn 元数据。
func applyExistingCodexFingerprintEmbeddedMetadata(values map[string]any, ids *codexFingerprintIDs) bool {
	if values == nil || ids == nil {
		return false
	}
	raw, ok := values[openAIWSTurnMetadataHeader].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return false
	}
	metadata := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil || metadata == nil {
		return false
	}
	if !applyExistingCodexFingerprintFields(metadata, ids) {
		return false
	}
	rebuilt, err := json.Marshal(metadata)
	if err != nil {
		return false
	}
	values[openAIWSTurnMetadataHeader] = string(rebuilt)
	return true
}

// applyCodexFingerprintExistingClientMetadataMap 是 session.update 的 map 版改写核心；
// raw 版通过相同核心处理，保证 passthrough 与非透传的字段保留语义一致。
func applyCodexFingerprintExistingClientMetadataMap(existing map[string]any, ids *codexFingerprintIDs) bool {
	if existing == nil || ids == nil {
		return false
	}
	changed := applyExistingCodexFingerprintFields(existing, ids)
	if applyExistingCodexFingerprintEmbeddedMetadata(existing, ids) {
		changed = true
	}
	return changed
}

func captureCodexFingerprintOriginalBodySessionID(ids *codexFingerprintIDs, clientMetadata any) {
	if ids == nil || ids.originalBodySessionIDCaptured {
		return
	}
	ids.originalBodySessionIDCaptured = true
	if clientMetadata == nil {
		return
	}
	switch metadata := clientMetadata.(type) {
	case map[string]any:
		if sessionID, ok := metadata["session_id"].(string); ok {
			ids.originalBodySessionID = strings.TrimSpace(sessionID)
		}
	case map[string]string:
		ids.originalBodySessionID = strings.TrimSpace(metadata["session_id"])
	}
}

func captureCodexFingerprintOriginalBodySessionIDRaw(ids *codexFingerprintIDs, value gjson.Result) {
	if ids == nil || ids.originalBodySessionIDCaptured {
		return
	}
	ids.originalBodySessionIDCaptured = true
	if value.Exists() && value.Type == gjson.String {
		ids.originalBodySessionID = strings.TrimSpace(value.String())
	}
}

func shouldRewriteCodexFingerprintPromptCacheKey(ids *codexFingerprintIDs, promptCacheKey string) bool {
	if ids == nil || ids.sessionID == "" {
		return false
	}
	if ids.mode != codexFingerprintSession && ids.mode != codexFingerprintFull {
		return false
	}
	if ids.originalBodySessionIDCaptured && ids.originalBodySessionID != "" {
		return promptCacheKey == ids.originalBodySessionID
	}
	return strings.TrimSpace(ids.clientSessionID) != "" && promptCacheKey == ids.clientSessionID
}

func applyCodexFingerprintPromptCacheKey(reqBody map[string]any, ids *codexFingerprintIDs) bool {
	if reqBody == nil || ids == nil || (ids.mode != codexFingerprintSession && ids.mode != codexFingerprintFull) || ids.sessionID == "" {
		return false
	}
	// compact 请求只调用 prompt_cache_key 投影，不经过完整 client_metadata 改写；
	// 先记录 body 中的原始 session，才能识别官方默认 cache key 并收敛到同一 session。
	captureCodexFingerprintOriginalBodySessionID(ids, reqBody["client_metadata"])
	promptCacheKey, ok := reqBody["prompt_cache_key"].(string)
	if !ok {
		if _, exists := reqBody["prompt_cache_key"]; !exists {
			reqBody["prompt_cache_key"] = ids.sessionID
			return true
		}
		// 非字符串值属于客户端显式的非法输入，交给上游校验，不把错误请求
		// 静默变成成功形态。
		return false
	}
	if strings.TrimSpace(promptCacheKey) == "" {
		reqBody["prompt_cache_key"] = ids.sessionID
		return true
	}
	if !shouldRewriteCodexFingerprintPromptCacheKey(ids, promptCacheKey) {
		return false
	}
	if promptCacheKey == ids.sessionID {
		return false
	}
	reqBody["prompt_cache_key"] = ids.sessionID
	return true
}

// applyCodexFingerprintPromptCacheKeyRaw 在不解码完整请求体的前提下补齐官方默认
// prompt_cache_key。显式的非字符串值保留给上游校验，避免网关吞掉客户端错误。
func applyCodexFingerprintPromptCacheKeyRaw(body []byte, ids *codexFingerprintIDs) ([]byte, bool, error) {
	if len(body) == 0 || ids == nil || (ids.mode != codexFingerprintSession && ids.mode != codexFingerprintFull) || ids.sessionID == "" {
		return body, false, nil
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return body, false, nil
	}
	captureCodexFingerprintOriginalBodySessionIDRaw(ids, gjson.GetBytes(body, "client_metadata.session_id"))
	promptCacheKey := gjson.GetBytes(body, "prompt_cache_key")
	shouldSet := !promptCacheKey.Exists()
	if promptCacheKey.Exists() && promptCacheKey.Type == gjson.String {
		value := strings.TrimSpace(promptCacheKey.String())
		shouldSet = value == "" || shouldRewriteCodexFingerprintPromptCacheKey(ids, promptCacheKey.String())
	}
	if !shouldSet {
		return body, false, nil
	}
	rewritten, err := sjson.SetBytes(body, "prompt_cache_key", ids.sessionID)
	if err != nil {
		return body, false, fmt.Errorf("splice converged prompt_cache_key: %w", err)
	}
	return rewritten, true, nil
}

// applyCodexFingerprintClientMetadataRaw 在原始 JSON 字节上改写 client_metadata，
// 供透传路径使用——透传是热路径，禁止对可能高达数十 MB 的 body 做全量
// Unmarshal（见 forwardOpenAIPassthrough 的轻量提取注释）。实现为：gjson 提取
// client_metadata 小对象单独解码，经共享核心改写后 sjson 一次性拼回，body
// 其余字节原样保留；root prompt_cache_key 仅在可证明是 body session 默认值时
// 做标量改写。语义与 applyCodexFingerprintClientMetadata 逐点一致（含
// "非对象值整体替换为收敛集合"的行为）。
func applyCodexFingerprintClientMetadataRaw(body []byte, ids *codexFingerprintIDs) ([]byte, bool, error) {
	if len(body) == 0 || ids == nil {
		return body, false, nil
	}
	// 非 JSON 对象的 body（数组/标量/畸形）没有 client_metadata 语义，
	// sjson 在这类根上写字段会改写整体结构，直接放行保持原样。
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		captureCodexFingerprintOriginalBodySessionIDRaw(ids, gjson.Result{})
		return body, false, nil
	}

	existing := map[string]any{}
	if cm := gjson.GetBytes(body, "client_metadata"); cm.IsObject() {
		captureCodexFingerprintOriginalBodySessionIDRaw(ids, gjson.GetBytes(body, "client_metadata.session_id"))
		if err := json.Unmarshal([]byte(cm.Raw), &existing); err != nil {
			return body, false, fmt.Errorf("decode client_metadata for fingerprint: %w", err)
		}
	} else {
		captureCodexFingerprintOriginalBodySessionIDRaw(ids, gjson.Result{})
	}

	next := body
	modified := false
	if applyCodexFingerprintToClientMetadataMap(existing, ids) {
		raw, err := json.Marshal(existing)
		if err != nil {
			return body, false, fmt.Errorf("encode converged client_metadata: %w", err)
		}
		var setErr error
		next, setErr = sjson.SetRawBytes(body, "client_metadata", raw)
		if setErr != nil {
			return body, false, fmt.Errorf("splice converged client_metadata: %w", setErr)
		}
		modified = true
	}
	if rewritten, promptChanged, promptErr := applyCodexFingerprintPromptCacheKeyRaw(next, ids); promptErr != nil {
		return body, false, promptErr
	} else if promptChanged {
		next = rewritten
		modified = true
	}
	return next, modified, nil
}

// applyCodexFingerprintExistingClientMetadataRaw 只改写 session.update 中已有的
// client_metadata 身份字段。缺失、null、非对象或非法 embedded metadata 均保持原样。
func applyCodexFingerprintExistingClientMetadataRaw(body []byte, ids *codexFingerprintIDs) ([]byte, bool, error) {
	if len(body) == 0 || ids == nil {
		return body, false, nil
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return body, false, nil
	}
	clientMetadata := gjson.GetBytes(body, "client_metadata")
	if !clientMetadata.IsObject() {
		return body, false, nil
	}
	existing := map[string]any{}
	if err := json.Unmarshal([]byte(clientMetadata.Raw), &existing); err != nil {
		return body, false, fmt.Errorf("decode existing client_metadata for fingerprint: %w", err)
	}
	if !applyCodexFingerprintExistingClientMetadataMap(existing, ids) {
		return body, false, nil
	}
	raw, err := json.Marshal(existing)
	if err != nil {
		return body, false, fmt.Errorf("encode existing converged client_metadata: %w", err)
	}
	next, err := sjson.SetRawBytes(body, "client_metadata", raw)
	if err != nil {
		return body, false, fmt.Errorf("splice existing converged client_metadata: %w", err)
	}
	return next, true, nil
}

// rewriteClientMetadataEmbeddedTurnMetadata 改写 client_metadata 中内嵌的
// x-codex-turn-metadata JSON 字符串里的指定字段。非法/非对象值会重建，
// 避免 flat client_metadata 与 embedded metadata 暴露两套身份。
func rewriteClientMetadataEmbeddedTurnMetadata(clientMetadata map[string]any, fields map[string]any) {
	if clientMetadata == nil || len(fields) == 0 {
		return
	}
	raw, _ := clientMetadata["x-codex-turn-metadata"].(string)
	var metadata map[string]any
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &metadata) != nil || metadata == nil {
		metadata = make(map[string]any, len(fields))
	}
	for k, v := range fields {
		metadata[k] = v
	}
	if rebuilt, err := json.Marshal(metadata); err == nil {
		clientMetadata["x-codex-turn-metadata"] = string(rebuilt)
	}
}
