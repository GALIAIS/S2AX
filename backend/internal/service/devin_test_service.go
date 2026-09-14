package service

// Devin 账号连通性测试：直连 Codeium GetChatMessage（Connect-RPC），
// 发一个最小 prompt，把文本增量按现有 TestEvent 格式推给管理端 SSE。

import (
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

// testDevinPrompt 是账号测试使用的最小探针文本（与用户验证时一致）。
const testDevinPrompt = "Say exactly: PONG"

func (s *AccountTestService) testDevinAccountConnection(c *gin.Context, account *Account, modelID string, prompt string) error {
	ctx := c.Request.Context()
	testModelID := strings.TrimSpace(modelID)
	if testModelID == "" {
		testModelID = "glm-5-2"
	}
	upstreamModel := account.GetMappedModel(testModelID)
	if upstreamModel == "" {
		upstreamModel = testModelID
	}
	upstreamModel = normalizeDevinLocalModel(upstreamModel)
	if strings.TrimSpace(prompt) == "" {
		prompt = testDevinPrompt
	}

	proxyURL := resolveAccountProxyURL(account)
	token, err := resolveDevinSessionToken(ctx, account, proxyURL)
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to resolve Devin session token: "+err.Error())
	}

	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})

	contentJSON, _ := json.Marshal(prompt)
	chatReq := &apicompat.ChatCompletionsRequest{
		Model: testModelID,
		Messages: []apicompat.ChatMessage{{
			Role:    "user",
			Content: contentJSON,
		}},
	}
	_, msgs := devinConvertMessages(chatReq)
	upReq := &devinConnectRequest{
		SystemPrompt: "You are a helpful assistant.",
		Messages:     msgs,
		Model:        upstreamModel,
		CascadeID:    deriveDevinCascadeID(chatReq, token),
	}
	events, err := devinChatStream(ctx, account.DevinAPIServerURL(), token, proxyURL, upReq)
	if err != nil {
		return s.sendErrorAndEnd(c, "GetChatMessage failed: "+err.Error())
	}

	var lastUsage *devinUsageStats
	stopReason := 0
	for ev := range events {
		switch ev.Kind {
		case "error":
			return s.sendErrorAndEnd(c, "stream error: "+ev.Err.Error())
		case "text":
			s.sendEvent(c, TestEvent{Type: "content", Text: ev.Text})
		case "usage":
			lastUsage = ev.Usage
		case "stop":
			stopReason = ev.StopReason
		case "done":
		}
	}
	data := map[string]any{
		"stop_reason": stopReason,
	}
	if lastUsage != nil {
		data["input_tokens"] = devinTotalInputTokens(lastUsage)
		data["output_tokens"] = lastUsage.OutputTokens
		data["cache_read_tokens"] = lastUsage.CacheReadTokens
		data["cache_write_tokens"] = lastUsage.CacheWriteTokens
		data["model_uid"] = lastUsage.ModelUID
	}
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true, Data: data})
	return nil
}
