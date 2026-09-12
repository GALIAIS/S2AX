package service

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// AccountTestModeDefault drives the standard /responses connection test.
	AccountTestModeDefault = "default"
	// AccountTestModeImage 显式执行 OpenAI 生图测试，使自定义图片工具 ID 不依赖模型
	// 名称前缀判断。
	AccountTestModeImage = "image"
	// AccountTestModeCompact drives the remote-compaction probe test
	// (native v2: streaming /responses with a compaction_trigger input item).
	AccountTestModeCompact = "compact"
)

func normalizeAccountTestMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case AccountTestModeCompact:
		return AccountTestModeCompact
	case AccountTestModeImage:
		return AccountTestModeImage
	default:
		return AccountTestModeDefault
	}
}

// createOpenAICompactProbePayload 构造原生 remote compaction v2 探测载荷：
// 流式 /responses + input 末尾 {"type":"compaction_trigger"}。上游已下线
// legacy unary /responses/compact（v1 形态恒 404，#5598/#5624），现行 codex
// 默认协议即 v2（RemoteCompactionV2 Stable + default_enabled）。
func createOpenAICompactProbePayload(model string, isOAuth bool) map[string]any {
	payload := map[string]any{
		"model":        strings.TrimSpace(model),
		"instructions": "You are a helpful coding assistant.",
		"input": []any{
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": "Respond with OK.",
			},
			map[string]any{"type": "compaction_trigger"},
		},
		"stream": true,
	}
	// ChatGPT internal API 要求 store: false，与真实转发一致。
	if isOAuth {
		payload["store"] = false
	}
	return payload
}

// openAICompactProbeFoundCompactionItem 判定探测响应是否产出了 compaction
// 输出 item——v2 契约的核心（codex 缺它即 fatal "got 0 items"）。三种形态都
// 认：① SSE 的 output_item.done/added（原生 v2 主形态，codex 只从这里收集
// item）；② SSE 终态 response.completed 的 response.output[]（部分上游只在
// 终态给出 item）；③ 整体 JSON 的 output[]（老网关链把请求降级成 unary）。
func openAICompactProbeFoundCompactionItem(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	bodyText := string(body)
	if _, found := findRawCompactionItemFromSSE(bodyText); found {
		return true
	}
	if finalResponse, ok := extractCodexFinalResponse(bodyText); ok &&
		responsesOutputHasCompactionItem(finalResponse) {
		return true
	}
	return responsesOutputHasCompactionItem(body)
}

func shouldMarkOpenAICompactUnsupported(status int, body []byte) bool {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	case http.StatusBadRequest, http.StatusForbidden, http.StatusUnprocessableEntity:
		lower := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(body) + " " + string(body)))
		if strings.Contains(lower, "compact") {
			for _, keyword := range []string{
				"unsupported",
				"not support",
				"does not support",
				"not available",
				"disabled",
			} {
				if strings.Contains(lower, keyword) {
					return true
				}
			}
		}
	}
	return false
}

// buildOpenAICompactProbeExtraUpdates 计算探测结果的账号 extra 更新。
// compactionFound 是 v2 契约判据：HTTP 2xx 但响应无 compaction item 时同样
// 记为不支持（链路把 compaction_trigger 吞掉的形态，等价 codex 的 "got 0
// items" fatal，#5478/#5648）。极端场景（上游链只支持 legacy unary compact）
// 可用账号级 openai_compact_mode=force_on 人工覆盖。
func buildOpenAICompactProbeExtraUpdates(resp *http.Response, body []byte, probeErr error, compactionFound bool, now time.Time) map[string]any {
	updates := map[string]any{
		"openai_compact_checked_at":  now.Format(time.RFC3339),
		"openai_compact_last_status": nil,
	}

	if resp != nil {
		updates["openai_compact_last_status"] = resp.StatusCode
	}

	switch {
	case probeErr != nil:
		updates["openai_compact_last_error"] = truncateString(sanitizeUpstreamErrorMessage(probeErr.Error()), 2048)
	case resp == nil:
		updates["openai_compact_last_error"] = "compact probe failed"
	default:
		errMsg := strings.TrimSpace(extractUpstreamErrorMessage(body))
		if errMsg == "" && len(body) > 0 {
			errMsg = strings.TrimSpace(string(body))
		}
		if errMsg == "" && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
			errMsg = "HTTP " + strconv.Itoa(resp.StatusCode)
		}
		errMsg = truncateString(sanitizeUpstreamErrorMessage(errMsg), 2048)
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300 && compactionFound:
			updates["openai_compact_supported"] = true
			updates["openai_compact_last_error"] = ""
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			updates["openai_compact_supported"] = false
			updates["openai_compact_last_error"] = "upstream returned 2xx without a compaction output item (native remote compaction v2 unsupported)"
		default:
			if shouldMarkOpenAICompactUnsupported(resp.StatusCode, body) {
				updates["openai_compact_supported"] = false
			}
			updates["openai_compact_last_error"] = errMsg
		}
	}

	return updates
}

func mergeExtraUpdates(base map[string]any, more map[string]any) map[string]any {
	if len(base) == 0 && len(more) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(more))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range more {
		out[key] = value
	}
	return out
}

// compactProbeSessionID 返回探测请求使用的会话标识。真实 Codex 的
// session-id / thread-id 恒为 UUID（codex-protocol ThreadId 是 UUIDv7），
// 探测既然与真实流量走同一个 /responses 端点，标识形态就必须同构——
// 否则上游能凭 "probe_compact_5" 这类字面量一眼区分出探测流量。
// 账号级稳定派生：重复探测复用同一会话，而不是每次新开一个；UUIDv7
// 与官方 ThreadId/SessionId 的线格式保持一致。
func compactProbeSessionID(accountID int64) string {
	if accountID <= 0 {
		return deriveStableUUIDv7("sub2api:codex-compact-probe:v2:anonymous")
	}
	return deriveStableUUIDv7("sub2api:codex-compact-probe:v2:" + strconv.FormatInt(accountID, 10))
}

// buildOpenAICodexAccountTestIdentity 构造账号连通性测试使用的原生 Codex 身份。
// 管理员测试没有真实 CLI 会话状态，但上游仍应看到与 Codex CLI 相同的字段形态：
// installation_id 为 UUIDv4，session/thread/turn/context_window 为 UUIDv7，
// window_id 为 <thread_id>:<window_number>，并且 flat metadata 与 canonical JSON
// 使用完全相同的一组值。
func buildOpenAICodexAccountTestIdentity(accountID int64, requestKind string) (string, string, map[string]any) {
	if strings.TrimSpace(requestKind) == "" {
		requestKind = "turn"
	}
	sessionID := compactProbeSessionID(accountID)
	installationID := deriveStableUUIDv4(
		"sub2api:codex-account-test-installation:" + strconv.FormatInt(accountID, 10),
	)
	turnID := uuid.Must(uuid.NewV7()).String()
	contextWindowID := uuid.Must(uuid.NewV7()).String()
	windowID := sessionID + ":0"
	turnMetadata := map[string]any{
		"installation_id":         installationID,
		"session_id":              sessionID,
		"thread_id":               sessionID,
		"turn_id":                 turnID,
		"window_id":               windowID,
		"window_number":           uint64(0),
		"context_window_id":       contextWindowID,
		"request_kind":            requestKind,
		"turn_started_at_unix_ms": time.Now().UnixMilli(),
	}
	if requestKind == "compaction" {
		// 远程压缩请求在官方 metadata 中还会声明压缩触发原因和实现。
		turnMetadata["compaction"] = map[string]any{
			"trigger":        "manual",
			"reason":         "user_requested",
			"implementation": "responses_compaction_v2",
			"phase":          "standalone_turn",
			"strategy":       "memento",
		}
	}
	turnMetadataJSON, _ := json.Marshal(turnMetadata)
	return sessionID, string(turnMetadataJSON), map[string]any{
		"x-codex-installation-id": installationID,
		"session_id":              sessionID,
		"thread_id":               sessionID,
		"turn_id":                 turnID,
		"x-codex-window-id":       windowID,
		"x-codex-turn-metadata":   string(turnMetadataJSON),
	}
}
