package service

// Devin 平台转发器：OpenAI /v1/chat/completions 入站 -> Devin Cloud ACP/WS 出站。
//
// 与 HTTP 透传平台的关键差异：上游不是 HTTP 端点而是
// wss://app.devin.ai/api/acp/live 的 JSON-RPC 会话（见 devin_acp_client.go）。
// 每个请求建立一条短连接 + 一个云端 session，turn 结束后尽力关闭。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// devinUpstreamEndpoint 记录在 OpenAIForwardResult.UpstreamEndpoint，标识非 HTTP 通道。
const devinUpstreamEndpoint = "acp://app.devin.ai/api/acp/live"

// forwardAsDevinACP 处理 devin 平台账号的 chat completions 请求。
// 入站与调度语义与 OpenAI 兼容平台一致；出站为 ACP session/prompt。
func (s *OpenAIGatewayService) forwardAsDevinACP(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	SetActualOpenAIUpstreamEndpoint(c, devinUpstreamEndpoint)

	// 1. 解析请求（只取 ACP 需要的字段）
	var chatReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &chatReq); err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse chat completions request: %w", err)
	}
	originalModel := chatReq.Model
	if originalModel == "" {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, errors.New("missing model in request")
	}
	clientStream := chatReq.Stream

	// 2. 模型映射：公共名 -> devin_version 档位值
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	SetOpsUpstreamModel(c, upstreamModel)

	// 3. messages -> ACP prompt 内容块
	promptBlocks := devinBuildPromptBlocks(&chatReq)
	if len(promptBlocks) == 0 {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "messages are required")
		return nil, errors.New("empty prompt after conversion")
	}

	// 4. 取 session token（api_key -> GetSelfDevinSessionToken 换发）
	proxyURL := resolveAccountProxyURL(account)
	token, err := resolveDevinSessionToken(ctx, account, proxyURL)
	if err != nil {
		return nil, devinWrapUpstreamError(c, err, true)
	}

	// 5. 建立 ACP 连接并初始化
	cl, err := devinDialACP(ctx, account, token, proxyURL)
	if err != nil {
		return nil, devinWrapUpstreamError(c, err, true)
	}
	defer cl.Close()

	// 6. 建会话 + 选模型档位
	sessionID, err := cl.devinNewSession(ctx, "/")
	if err != nil {
		return nil, devinWrapUpstreamError(c, err, true)
	}
	defer func() { go cl.devinCloseSession(sessionID) }()
	if upstreamModel != "" {
		if err := cl.devinSetConfig(ctx, sessionID, DevinConfigIDVersion, upstreamModel); err != nil {
			return nil, devinWrapUpstreamError(c, err, true)
		}
	}
	if org := account.DevinOrgID(); org != "" {
		_ = cl.devinSetConfig(ctx, sessionID, DevinConfigIDOrg, org)
	}

	// 7. 发起 prompt，按客户端 stream 选项分流
	if clientStream {
		return s.devinStreamPrompt(ctx, c, cl, sessionID, promptBlocks, originalModel, upstreamModel, billingModel, startTime, &chatReq)
	}
	return s.devinBufferedPrompt(ctx, c, cl, sessionID, promptBlocks, originalModel, upstreamModel, billingModel, startTime)
}

// devinStreamPrompt 流式：agent_message_chunk -> content delta，
// agent_thought_chunk -> reasoning_content delta。
func (s *OpenAIGatewayService) devinStreamPrompt(
	ctx context.Context,
	c *gin.Context,
	cl *devinACPClient,
	sessionID string,
	promptBlocks []map[string]any,
	originalModel, upstreamModel, billingModel string,
	startTime time.Time,
	chatReq *apicompat.ChatCompletionsRequest,
) (*OpenAIForwardResult, error) {
	chunkID := "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	created := time.Now().Unix()

	events := make(chan devinACPEvent, 64)
	cl.onEvent = func(_ string, ev devinACPEvent) {
		select {
		case events <- ev:
		default:
		}
	}

	setSSEHeaders(c)
	w := c.Writer
	firstTokenMs := int64(0)

	// role chunk
	if err := devinWriteChunk(w, chunkID, created, originalModel, &apicompat.ChatDelta{Role: "assistant"}, nil); err != nil {
		return nil, err
	}
	w.Flush()

	promptDone := make(chan *devinACPTurnResult, 1)
	promptErr := make(chan error, 1)
	go func() {
		res, err := cl.devinPrompt(ctx, sessionID, promptBlocks)
		if err != nil {
			promptErr <- err
			return
		}
		promptDone <- res
	}()

	var turn *devinACPTurnResult
	clientGone := false
loop:
	for {
		select {
		case ev := <-events:
			if firstTokenMs == 0 {
				firstTokenMs = time.Since(startTime).Milliseconds()
			}
			var delta *apicompat.ChatDelta
			if ev.Kind == "thought" {
				delta = &apicompat.ChatDelta{ReasoningContent: &ev.Text}
			} else {
				delta = &apicompat.ChatDelta{Content: &ev.Text}
			}
			if err := devinWriteChunk(w, chunkID, created, originalModel, delta, nil); err != nil {
				break loop
			}
			w.Flush()
		case turn = <-promptDone:
			break loop
		case err := <-promptErr:
			return nil, devinWrapUpstreamError(c, err, false)
		case <-ctx.Done():
			clientGone = true
			break loop
		}
	}

	if clientGone {
		// 客户端断开：尽力取消远端 turn，避免云端继续跑烧 ACU。
		go func() {
			cancelCtx, cancel := context.WithTimeout(context.Background(), devinACPCloseTimeout)
			defer cancel()
			_, _ = cl.call(cancelCtx, "session/cancel", map[string]any{"sessionId": sessionID})
		}()
		return &OpenAIForwardResult{
			Model:            originalModel,
			UpstreamModel:    upstreamModel,
			BillingModel:     billingModel,
			Stream:           true,
			Duration:         time.Since(startTime),
			ClientDisconnect: true,
			UpstreamEndpoint: devinUpstreamEndpoint,
			FirstTokenMs:     intPtrFrom(firstTokenMs),
		}, nil
	}

	finishReason := devinMapStopReason(turn.StopReason)
	if err := devinWriteChunk(w, chunkID, created, originalModel, &apicompat.ChatDelta{}, &finishReason); err != nil {
		return nil, err
	}
	usage := &apicompat.ChatUsage{
		PromptTokens:     turn.InputTokens,
		CompletionTokens: turn.OutputTokens,
		TotalTokens:      turn.TotalTokens,
	}
	if chatReq.StreamOptions != nil && chatReq.StreamOptions.IncludeUsage {
		if err := devinWriteUsageChunk(w, chunkID, created, originalModel, usage); err != nil {
			return nil, err
		}
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	w.Flush()

	return &OpenAIForwardResult{
		Model:            originalModel,
		UpstreamModel:    upstreamModel,
		BillingModel:     billingModel,
		Stream:           true,
		Duration:         time.Since(startTime),
		UpstreamEndpoint: devinUpstreamEndpoint,
		FirstTokenMs:     intPtrFrom(firstTokenMs),
		Usage: OpenAIUsage{
			InputTokens:  turn.InputTokens,
			OutputTokens: turn.OutputTokens,
		},
	}, nil
}

// devinBufferedPrompt 非流式：聚合全部增量后一次性返回 chat.completion。
func (s *OpenAIGatewayService) devinBufferedPrompt(
	ctx context.Context,
	c *gin.Context,
	cl *devinACPClient,
	sessionID string,
	promptBlocks []map[string]any,
	originalModel, upstreamModel, billingModel string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	var textBuf, thoughtBuf strings.Builder
	firstTokenMs := int64(0)
	cl.onEvent = func(_ string, ev devinACPEvent) {
		if firstTokenMs == 0 {
			firstTokenMs = time.Since(startTime).Milliseconds()
		}
		if ev.Kind == "thought" {
			thoughtBuf.WriteString(ev.Text)
		} else {
			textBuf.WriteString(ev.Text)
		}
	}

	turn, err := cl.devinPrompt(ctx, sessionID, promptBlocks)
	if err != nil {
		return nil, devinWrapUpstreamError(c, err, false)
	}

	msg := apicompat.ChatMessage{Role: "assistant"}
	if contentJSON, err := json.Marshal(textBuf.String()); err == nil {
		msg.Content = apicompatChatContent(contentJSON)
	}
	if thought := thoughtBuf.String(); thought != "" {
		msg.ReasoningContent = thought
	}
	resp := apicompat.ChatCompletionsResponse{
		ID:      "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24],
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   originalModel,
		Choices: []apicompat.ChatChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: devinMapStopReason(turn.StopReason),
		}},
		Usage: &apicompat.ChatUsage{
			PromptTokens:     turn.InputTokens,
			CompletionTokens: turn.OutputTokens,
			TotalTokens:      turn.TotalTokens,
		},
	}
	respBody, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(respBody)

	return &OpenAIForwardResult{
		Model:            originalModel,
		UpstreamModel:    upstreamModel,
		BillingModel:     billingModel,
		Duration:         time.Since(startTime),
		UpstreamEndpoint: devinUpstreamEndpoint,
		FirstTokenMs:     intPtrFrom(firstTokenMs),
		Usage: OpenAIUsage{
			InputTokens:  turn.InputTokens,
			OutputTokens: turn.OutputTokens,
		},
	}, nil
}

// devinBuildPromptBlocks 把 chat messages 展平成 ACP prompt 内容块。
// ACP prompt 无角色概念：历史轮次拼成带角色标签的文本块，最后一条 user
// 消息的 parts 直接展开（文本块 + data URI 图片块）。
func devinBuildPromptBlocks(req *apicompat.ChatCompletionsRequest) []map[string]any {
	msgs := req.Messages
	if len(msgs) == 0 {
		return nil
	}
	var blocks []map[string]any
	var transcript strings.Builder
	lastIdx := len(msgs) - 1
	for i, m := range msgs {
		isLast := i == lastIdx && m.Role == "user"
		parts := devinMessageTextAndImages(m)
		if isLast {
			// 末轮用户消息：先冲刷 transcript，再按序展开文本/图片块
			if transcript.Len() > 0 {
				blocks = append(blocks, map[string]any{"type": "text", "text": transcript.String()})
				transcript.Reset()
			}
			for _, p := range parts {
				blocks = append(blocks, p)
			}
			continue
		}
		role := m.Role
		if role == "" {
			role = "user"
		}
		for _, p := range parts {
			if p["type"] == "text" {
				transcript.WriteString(devinRoleLabel(role) + ": " + p["text"].(string) + "\n\n")
			}
			// 历史轮次图片并入文本占位（避免阻断 transcript 顺序）
		}
		// assistant 的 tool_calls 以文本形式保留调用意图
		if len(m.ToolCalls) > 0 {
			if tc, err := json.Marshal(m.ToolCalls); err == nil {
				transcript.WriteString("Assistant tool_calls: " + string(tc) + "\n\n")
			}
		}
	}
	if transcript.Len() > 0 {
		blocks = append(blocks, map[string]any{"type": "text", "text": transcript.String()})
	}
	return blocks
}

// devinMessageTextAndImages 把单条 chat message 的 content 展平成 ACP 块列表。
// content 为字符串时产出一个 text 块；为数组时 text->text 块、
// image_url(data URI)->image 块、其余类型降级为文本描述。
func devinMessageTextAndImages(m apicompat.ChatMessage) []map[string]any {
	var blocks []map[string]any
	if len(m.Content) == 0 {
		return blocks
	}
	// 字符串 content
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		if s != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": s})
		}
		return blocks
	}
	var parts []apicompat.ChatContentPart
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return blocks
	}
	for _, p := range parts {
		switch p.Type {
		case "text":
			if p.Text != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": p.Text})
			}
		case "image_url":
			if blk := devinImageBlock(p.ImageURL); blk != nil {
				blocks = append(blocks, blk)
			}
		case "file":
			if p.File != nil && p.File.Filename != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": "[file: " + p.File.Filename + "]"})
			}
		}
	}
	return blocks
}

// devinImageBlock 把 OpenAI image_url part 转成 ACP image 块。
// 仅支持 data URI；http(s) URL 降级为文本提示（网关无公网取图上下文）。
func devinImageBlock(img *apicompat.ChatImageURL) map[string]any {
	if img == nil || img.URL == "" {
		return nil
	}
	u := img.URL
	if strings.HasPrefix(u, "data:") {
		semi := strings.Index(u, ";base64,")
		if semi > 5 {
			return map[string]any{
				"type":     "image",
				"data":     u[semi+8:],
				"mimeType": u[5:semi],
			}
		}
	}
	return map[string]any{"type": "text", "text": "[image: " + u + "]"}
}

// devinWriteChunk 写一个 chat.completion.chunk SSE 帧。
func devinWriteChunk(w http.ResponseWriter, id string, created int64, model string, delta *apicompat.ChatDelta, finish *string) error {
	chunk := apicompat.ChatCompletionsChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []apicompat.ChatChunkChoice{{Index: 0, Delta: *delta, FinishReason: finish}},
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

// devinWriteUsageChunk 写 choices 为空、仅带 usage 的收尾 chunk。
func devinWriteUsageChunk(w http.ResponseWriter, id string, created int64, model string, usage *apicompat.ChatUsage) error {
	chunk := apicompat.ChatCompletionsChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []apicompat.ChatChunkChoice{},
		Usage:   usage,
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

// devinMapStopReason ACP stopReason -> OpenAI finish_reason。
func devinMapStopReason(stopReason string) string {
	switch stopReason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "refusal":
		return "content_filter"
	case "cancelled", "max_turn_requests":
		return "stop"
	default:
		return "stop"
	}
}

// devinWrapUpstreamError 统一包装 ACP 链路错误：
//   - 已是 *UpstreamFailoverError 的原样返回
//   - preWrite 且未写响应头时，给客户端补一个 JSON 错误体
//   - 其余视为可转移的上游错误（failover 到下一账号）
func devinWrapUpstreamError(c *gin.Context, err error, preWrite bool) error {
	if err == nil {
		return nil
	}
	var fo *UpstreamFailoverError
	if errors.As(err, &fo) {
		return fo
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &UpstreamFailoverError{StatusCode: http.StatusGatewayTimeout, RequestScopedTransient: true}
	}
	if preWrite && c != nil && c.Writer != nil && !c.Writer.Written() {
		writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Devin upstream error: "+err.Error())
	}
	return &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: []byte(err.Error())}
}

func intPtrFrom(v int64) *int {
	if v <= 0 {
		return nil
	}
	i := int(v)
	return &i
}

// apicompatChatContent 把已编码的 JSON 值赋给 ChatMessage.Content（json.RawMessage）。
func apicompatChatContent(v []byte) json.RawMessage { return json.RawMessage(v) }

// devinRoleLabel 归一化角色标签，避免依赖已废弃的 strings.Title。
func devinRoleLabel(role string) string {
	switch role {
	case "system", "user", "assistant", "tool", "function":
		return strings.ToUpper(role[:1]) + role[1:]
	default:
		return "User"
	}
}
