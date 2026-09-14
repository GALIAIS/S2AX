package service

// Devin 平台转发器：OpenAI /v1/chat/completions 入站 -> Codeium GetChatMessage
// Connect-RPC 直连出站（见 devin_connect_client.go）。
//
// 不经过 Devin Cloud ACP（app.devin.ai 云端会话按订阅/ACU 计费），也不需要
// devin.exe：devin.exe 的 agent loop（session DB、本地工具执行、~19k 系统
// 提示注入）对 OpenAI 兼容网关没有意义——调用方客户端自己驱动 agent loop，
// 这里只做纯推理 + tools 协议透传。经 server.codeium.com 的本地模型档
// （glm-5-2 / swe-1-7 / swe-2-* / adaptive 等）不消耗 Devin 云端额度。

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

// devinUpstreamEndpoint 记录在 OpenAIForwardResult.UpstreamEndpoint，标识 Connect-RPC 通道。
const devinUpstreamEndpoint = "connect+proto://server.codeium.com/ApiServerService/GetChatMessage"

// forwardAsDevinACP 处理 devin 平台账号的 chat completions 请求。
// 入站与调度语义与 OpenAI 兼容平台一致；出站为 GetChatMessage 流。
func (s *OpenAIGatewayService) forwardAsDevinACP(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	SetActualOpenAIUpstreamEndpoint(c, devinUpstreamEndpoint)

	// 1. 解析请求
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

	// 2. 模型映射：公共名 -> 本地 chat_model_uid
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeDevinLocalModel(normalizeOpenAIModelForUpstream(account, billingModel))
	SetOpsUpstreamModel(c, upstreamModel)

	// 3. messages -> chatMessagePrompts（system 单独提到顶层 prompt）
	cascadeID := uuid.NewString()
	systemPrompt, msgs := devinConvertMessages(&chatReq, cascadeID)
	if len(msgs) == 0 {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "messages are required")
		return nil, errors.New("empty prompt after conversion")
	}

	// 4. 取 session token（api_key -> GetSelfDevinSessionToken 换发）
	proxyURL := resolveAccountProxyURL(account)
	token, err := resolveDevinSessionToken(ctx, account, proxyURL)
	if err != nil {
		return nil, devinWrapUpstreamError(c, err, true)
	}

	// 5. 发起 GetChatMessage 流
	maxTok := 0
	if chatReq.MaxCompletionTokens != nil {
		maxTok = *chatReq.MaxCompletionTokens
	} else if chatReq.MaxTokens != nil {
		maxTok = *chatReq.MaxTokens
	}
	upReq := &devinConnectRequest{
		SystemPrompt: systemPrompt,
		Messages:     msgs,
		Model:        upstreamModel,
		Tools:        chatReq.Tools,
		ToolChoice:   chatReq.ToolChoice,
		MaxTokens:    maxTok,
		Temperature:  chatReq.Temperature,
		TopP:         chatReq.TopP,
		Stop:         devinParseStop(chatReq.Stop),
		CascadeID:    cascadeID,
	}
	events, err := devinChatStream(ctx, account.DevinAPIServerURL(), token, proxyURL, upReq)
	if err != nil {
		return nil, devinWrapUpstreamError(c, err, true)
	}

	// 6. 按客户端 stream 选项分流
	if clientStream {
		return s.devinStreamPrompt(ctx, c, events, originalModel, upstreamModel, billingModel, startTime, &chatReq)
	}
	return s.devinBufferedPrompt(ctx, c, events, originalModel, upstreamModel, billingModel, startTime)
}

// devinParseStop 把 OpenAI stop（string 或 []string）展平成列表。
func devinParseStop(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	return nil
}

// normalizeDevinLocalModel 把公共模型名归一成本地 chat_model_uid：
//   - 剥掉 "devin-" / "devin_" 前缀（历史 cloud 档位名兼容）
//   - glm-5.2 -> glm-5-2 之类的点分隔版本号
//   - 其余原样透传（上游枚举即本地 uid）
func normalizeDevinLocalModel(m string) string {
	m = strings.TrimSpace(m)
	if lm := strings.ToLower(m); strings.HasPrefix(lm, "devin-") || strings.HasPrefix(lm, "devin_") {
		m = m[6:]
	}
	return strings.ReplaceAll(m, ".", "-")
}

// devinToolAcc 聚合流式 tool call：arguments_json 可能是全量前缀或增量。
type devinToolAcc struct {
	idx  int
	call apicompat.ChatToolCall
	args string
}

// devinStreamPrompt 流式：text->content delta、thinking->reasoning_content
// delta、toolcall->tool_calls delta、usage->末帧 usage。
func (s *OpenAIGatewayService) devinStreamPrompt(
	ctx context.Context,
	c *gin.Context,
	events <-chan devinChatEvent,
	originalModel, upstreamModel, billingModel string,
	startTime time.Time,
	chatReq *apicompat.ChatCompletionsRequest,
) (*OpenAIForwardResult, error) {
	chunkID := "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	created := time.Now().Unix()

	setSSEHeaders(c)
	w := c.Writer
	firstTokenMs := int64(0)

	if err := devinWriteChunk(w, chunkID, created, originalModel, &apicompat.ChatDelta{Role: "assistant"}, nil); err != nil {
		return nil, err
	}
	w.Flush()

	var usage *devinUsageStats
	stopReason := 0
	tools := map[string]*devinToolAcc{}
	toolOrder := []string{}
	activeToolID := ""
	mark := func() {
		if firstTokenMs == 0 {
			firstTokenMs = time.Since(startTime).Milliseconds()
		}
	}

loop:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break loop
			}
			switch ev.Kind {
			case "done":
				break loop
			case "error":
				return nil, devinWrapUpstreamError(c, ev.Err, false)
			case "usage":
				usage = ev.Usage
			case "stop":
				stopReason = ev.StopReason
			case "thinking":
				mark()
				if err := devinWriteChunk(w, chunkID, created, originalModel,
					&apicompat.ChatDelta{ReasoningContent: &ev.Text}, nil); err != nil {
					break loop
				}
				w.Flush()
			case "text":
				mark()
				if err := devinWriteChunk(w, chunkID, created, originalModel,
					&apicompat.ChatDelta{Content: &ev.Text}, nil); err != nil {
					break loop
				}
				w.Flush()
			case "toolcall":
				mark()
				tc := ev.ToolCall
				id := tc.ID
				if id == "" {
					id = activeToolID
				}
				if id == "" {
					continue
				}
				acc := tools[id]
				if acc == nil {
					acc = &devinToolAcc{idx: len(toolOrder), call: apicompat.ChatToolCall{ID: id, Type: "function"}}
					tools[id] = acc
					toolOrder = append(toolOrder, id)
					activeToolID = id
				}
				if tc.Name != "" {
					acc.call.Function.Name = tc.Name
				}
				// arguments_json：全量前缀则取增量，否则视为增量片段
				var argDelta string
				if strings.HasPrefix(tc.ArgumentsJSON, acc.args) {
					argDelta = tc.ArgumentsJSON[len(acc.args):]
					acc.args = tc.ArgumentsJSON
				} else {
					argDelta = tc.ArgumentsJSON
					acc.args += tc.ArgumentsJSON
				}
				delta := apicompat.ChatToolCall{
					Index: &acc.idx,
					ID:    acc.call.ID,
					Type:  "function",
					Function: apicompat.ChatFunctionCall{
						Name:      acc.call.Function.Name,
						Arguments: argDelta,
					},
				}
				if err := devinWriteChunk(w, chunkID, created, originalModel,
					&apicompat.ChatDelta{ToolCalls: []apicompat.ChatToolCall{delta}}, nil); err != nil {
					break loop
				}
				w.Flush()
			}
		case <-ctx.Done():
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
	}

	finishReason := devinFinishReason(stopReason, len(tools) > 0)
	if err := devinWriteChunk(w, chunkID, created, originalModel, &apicompat.ChatDelta{}, &finishReason); err != nil {
		return nil, err
	}
	inTok, outTok := 0, 0
	var usageOut *apicompat.ChatUsage
	if usage != nil {
		inTok, outTok = usage.InputTokens, usage.OutputTokens
		usageOut = &apicompat.ChatUsage{
			PromptTokens:     inTok,
			CompletionTokens: outTok,
			TotalTokens:      inTok + outTok,
		}
		if usage.CacheReadTokens > 0 || usage.CacheWriteTokens > 0 {
			usageOut.PromptTokensDetails = &apicompat.ChatTokenDetails{
				CachedTokens:        usage.CacheReadTokens,
				CacheCreationTokens: usage.CacheWriteTokens,
			}
		}
	}
	if chatReq.StreamOptions != nil && chatReq.StreamOptions.IncludeUsage && usageOut != nil {
		if err := devinWriteUsageChunk(w, chunkID, created, originalModel, usageOut); err != nil {
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
			InputTokens:  inTok,
			OutputTokens: outTok,
		},
	}, nil
}

// devinBufferedPrompt 非流式：聚合全部增量后一次性返回 chat.completion。
func (s *OpenAIGatewayService) devinBufferedPrompt(
	ctx context.Context,
	c *gin.Context,
	events <-chan devinChatEvent,
	originalModel, upstreamModel, billingModel string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	var textBuf, thoughtBuf strings.Builder
	var usage *devinUsageStats
	stopReason := 0
	tools := map[string]*devinToolAcc{}
	toolOrder := []string{}
	activeToolID := ""
	firstTokenMs := int64(0)
	mark := func() {
		if firstTokenMs == 0 {
			firstTokenMs = time.Since(startTime).Milliseconds()
		}
	}

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				goto done
			}
			switch ev.Kind {
			case "done":
				goto done
			case "error":
				return nil, devinWrapUpstreamError(c, ev.Err, false)
			case "usage":
				usage = ev.Usage
			case "stop":
				stopReason = ev.StopReason
			case "thinking":
				mark()
				thoughtBuf.WriteString(ev.Text)
			case "text":
				mark()
				textBuf.WriteString(ev.Text)
			case "toolcall":
				mark()
				tc := ev.ToolCall
				id := tc.ID
				if id == "" {
					id = activeToolID
				}
				if id == "" {
					continue
				}
				acc := tools[id]
				if acc == nil {
					acc = &devinToolAcc{idx: len(toolOrder), call: apicompat.ChatToolCall{ID: id, Type: "function"}}
					tools[id] = acc
					toolOrder = append(toolOrder, id)
					activeToolID = id
				}
				if tc.Name != "" {
					acc.call.Function.Name = tc.Name
				}
				if strings.HasPrefix(tc.ArgumentsJSON, acc.args) {
					acc.args = tc.ArgumentsJSON
				} else {
					acc.args += tc.ArgumentsJSON
				}
			}
		case <-ctx.Done():
			return &OpenAIForwardResult{
				Model:            originalModel,
				UpstreamModel:    upstreamModel,
				BillingModel:     billingModel,
				Duration:         time.Since(startTime),
				ClientDisconnect: true,
				UpstreamEndpoint: devinUpstreamEndpoint,
				FirstTokenMs:     intPtrFrom(firstTokenMs),
			}, nil
		}
	}
done:

	msg := apicompat.ChatMessage{Role: "assistant"}
	if contentJSON, err := json.Marshal(textBuf.String()); err == nil {
		msg.Content = apicompatChatContent(contentJSON)
	}
	if thought := thoughtBuf.String(); thought != "" {
		msg.ReasoningContent = thought
	}
	for _, id := range toolOrder {
		acc := tools[id]
		msg.ToolCalls = append(msg.ToolCalls, apicompat.ChatToolCall{
			ID:   acc.call.ID,
			Type: "function",
			Function: apicompat.ChatFunctionCall{
				Name:      acc.call.Function.Name,
				Arguments: acc.args,
			},
		})
	}
	inTok, outTok := 0, 0
	usageOut := &apicompat.ChatUsage{}
	if usage != nil {
		inTok, outTok = usage.InputTokens, usage.OutputTokens
		usageOut.PromptTokens = inTok
		usageOut.CompletionTokens = outTok
		usageOut.TotalTokens = inTok + outTok
		if usage.CacheReadTokens > 0 || usage.CacheWriteTokens > 0 {
			usageOut.PromptTokensDetails = &apicompat.ChatTokenDetails{
				CachedTokens:        usage.CacheReadTokens,
				CacheCreationTokens: usage.CacheWriteTokens,
			}
		}
	}
	resp := apicompat.ChatCompletionsResponse{
		ID:      "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24],
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   originalModel,
		Choices: []apicompat.ChatChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: devinFinishReason(stopReason, len(tools) > 0),
		}},
		Usage: usageOut,
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
			InputTokens:  inTok,
			OutputTokens: outTok,
		},
	}, nil
}

// devinFinishReason 映射上游 stop_reason + 是否产出了 tool_calls。
func devinFinishReason(stopReason int, hasTools bool) string {
	if hasTools {
		return "tool_calls"
	}
	switch stopReason {
	case 3: // MAX_TOKENS
		return "length"
	case 5: // MAX_NEWLINES
		return "length"
	case 11: // CONTENT_FILTER
		return "content_filter"
	default:
		return "stop"
	}
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
