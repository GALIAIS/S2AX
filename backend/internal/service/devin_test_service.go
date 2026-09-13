package service

// Devin 账号连通性测试：拨号 ACP -> session/new -> 发一个最小 prompt，
// 把 agent_message_chunk 增量按现有 TestEvent 格式推给管理端 SSE。

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// testDevinPrompt 是账号测试使用的最小探针文本（与用户验证时一致）。
const testDevinPrompt = "Say exactly: PONG"

func (s *AccountTestService) testDevinAccountConnection(c *gin.Context, account *Account, modelID string, prompt string) error {
	ctx := c.Request.Context()
	testModelID := strings.TrimSpace(modelID)
	if testModelID == "" {
		testModelID = "devin-2-5"
	}
	upstreamModel := account.GetMappedModel(testModelID)
	if upstreamModel == "" {
		upstreamModel = testModelID
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = testDevinPrompt
	}

	proxyURL := resolveAccountProxyURL(account)
	token, err := resolveDevinSessionToken(ctx, account, proxyURL)
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to resolve Devin session token: "+err.Error())
	}

	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})

	cl, err := devinDialACP(ctx, account, token, proxyURL)
	if err != nil {
		return s.sendErrorAndEnd(c, "ACP dial failed: "+err.Error())
	}
	defer cl.Close()

	sessionID, err := cl.devinNewSession(ctx, "/")
	if err != nil {
		return s.sendErrorAndEnd(c, "session/new failed: "+err.Error())
	}
	defer func() { go cl.devinCloseSession(sessionID) }()

	if err := cl.devinSetConfig(ctx, sessionID, DevinConfigIDVersion, upstreamModel); err != nil {
		return s.sendErrorAndEnd(c, "set devin_version failed: "+err.Error())
	}
	if org := account.DevinOrgID(); org != "" {
		_ = cl.devinSetConfig(ctx, sessionID, DevinConfigIDOrg, org)
	}

	cl.onEvent = func(_ string, ev devinACPEvent) {
		if ev.Kind == "message" && ev.Text != "" {
			s.sendEvent(c, TestEvent{Type: "content", Text: ev.Text})
		}
	}

	turn, err := cl.devinPrompt(ctx, sessionID, []map[string]any{{"type": "text", "text": prompt}})
	if err != nil {
		return s.sendErrorAndEnd(c, "session/prompt failed: "+err.Error())
	}
	s.sendEvent(c, TestEvent{
		Type:    "test_complete",
		Success: true,
		Data: map[string]any{
			"session_id":    turn.SessionID,
			"stop_reason":   turn.StopReason,
			"input_tokens":  turn.InputTokens,
			"output_tokens": turn.OutputTokens,
		},
	})
	return nil
}
