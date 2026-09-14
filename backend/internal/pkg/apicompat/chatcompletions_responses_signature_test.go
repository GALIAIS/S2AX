package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Devin 上游的 thinking_signature 经 encrypted_content 回放进下一轮 input，
// 桥接层必须把它挂到紧随其后的 assistant 消息上供 Connect 编码回传。
func TestResponsesToChat_EncryptedContentSealed_AttachesToAssistant(t *testing.T) {
	req := &ResponsesRequest{
		Model: "swe-2",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"sealed.abc123"},
			{"type":"function_call","call_id":"call_1","name":"get_value","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"go on"}]}
		]`),
	}

	out, err := ResponsesToChatCompletionsRequestWithOptions(req, nil)
	require.NoError(t, err)
	require.Len(t, out.Messages, 3)
	require.Equal(t, "assistant", out.Messages[0].Role)
	require.Equal(t, "sealed.abc123", out.Messages[0].Signature)
	require.Equal(t, "sealed", out.Messages[0].SignatureType)
}

// openai 型签名（序列化 reasoning item 数组）识别为 signature_type=openai，
// message 项的 id 回放为上游 output_id。
func TestResponsesToChat_EncryptedContentOpenAI_CarriesOutputID(t *testing.T) {
	blob := `[{"id":"rs_abc","type":"reasoning","summary":[]}]`
	input, _ := json.Marshal([]map[string]any{
		{"type": "reasoning", "id": "rs_abc", "summary": []any{}, "encrypted_content": blob},
		{"type": "message", "id": "msg_xyz", "role": "assistant", "content": []map[string]any{
			{"type": "output_text", "text": "done"},
		}},
		{"type": "message", "role": "user", "content": []map[string]any{
			{"type": "input_text", "text": "next"},
		}},
	})
	req := &ResponsesRequest{Model: "swe-2", Input: json.RawMessage(input)}

	out, err := ResponsesToChatCompletionsRequestWithOptions(req, nil)
	require.NoError(t, err)
	require.Len(t, out.Messages, 2)
	require.Equal(t, "assistant", out.Messages[0].Role)
	require.Equal(t, blob, out.Messages[0].Signature)
	require.Equal(t, "openai", out.Messages[0].SignatureType)
	require.Equal(t, "msg_xyz", out.Messages[0].OutputID)
}

// 外来不透明载荷（非本网关下发的形态）不可解，必须丢弃而不是原样回传。
func TestResponsesToChat_EncryptedContentForeign_Dropped(t *testing.T) {
	req := &ResponsesRequest{
		Model: "swe-2",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"v4_unrelated_blob"},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}
		]`),
	}

	out, err := ResponsesToChatCompletionsRequestWithOptions(req, nil)
	require.NoError(t, err)
	require.Len(t, out.Messages, 1)
	require.Empty(t, out.Messages[0].Signature)
	require.Empty(t, out.Messages[0].SignatureType)
}

// 出站非流式：signature → reasoning item encrypted_content；openai 型签名
// 的 item id 取内层 rs_*；message item id 用上游 output_id。
func TestChatToResponses_SignatureRoundTrip(t *testing.T) {
	blob := `[{"id":"rs_real","type":"reasoning","summary":[]}]`
	resp := &ChatCompletionsResponse{
		Choices: []ChatChoice{{
			Message: ChatMessage{
				Role:             "assistant",
				Content:          json.RawMessage(`"answer"`),
				ReasoningContent: "thinking",
				Signature:        blob,
				SignatureType:    "openai",
				OutputID:         "msg_upstream",
			},
		}},
	}

	out := ChatCompletionsResponseToResponses(resp, "swe-2", nil, nil, false, nil)
	require.Len(t, out.Output, 2)
	require.Equal(t, "reasoning", out.Output[0].Type)
	require.Equal(t, "rs_real", out.Output[0].ID)
	require.Equal(t, blob, out.Output[0].EncryptedContent)
	require.Equal(t, "message", out.Output[1].Type)
	require.Equal(t, "msg_upstream", out.Output[1].ID)
}

// redacted thinking：只有签名没有可见文本时仍要产出 reasoning item 携带
// encrypted_content，客户端才有可回放的对象。
func TestChatToResponses_SignatureOnly_StillEmitsReasoningItem(t *testing.T) {
	resp := &ChatCompletionsResponse{
		Choices: []ChatChoice{{
			Message: ChatMessage{
				Role:          "assistant",
				Content:       json.RawMessage(`"answer"`),
				Signature:     "sealed.redacted",
				SignatureType: "sealed",
			},
		}},
	}

	out := ChatCompletionsResponseToResponses(resp, "swe-2", nil, nil, false, nil)
	require.Len(t, out.Output, 2)
	require.Equal(t, "reasoning", out.Output[0].Type)
	require.Equal(t, "sealed.redacted", out.Output[0].EncryptedContent)
	require.Equal(t, "message", out.Output[1].Type)
}

// 流式：Devin 实测时序 thinking → text → signature → stop。signature 晚到
// 时 reasoning item 的收尾必须挂起等签名，done 事件带 encrypted_content；
// message item id 用上游 output_id。
func TestStream_SignatureLateArrival_DefersReasoningDone(t *testing.T) {
	state := NewChatCompletionsToResponsesStreamState("swe-2-max")
	var events []ResponsesStreamEvent
	for _, payload := range []string{
		`{"choices":[{"index":0,"delta":{"reasoning_content":"thinking hard"}}]}`,
		`{"choices":[{"index":0,"delta":{"output_id":"msg_srv1"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"answer text"}}]}`,
		`{"choices":[{"index":0,"delta":{"signature":"sealed.v1.xyz","signature_type":"sealed"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	} {
		var chunk ChatCompletionsChunk
		require.NoError(t, json.Unmarshal([]byte(payload), &chunk))
		events = append(events, ChatCompletionsChunkToResponsesEvents(&chunk, state)...)
	}
	events = append(events, FinalizeChatCompletionsResponsesStream(state)...)

	var reasoningDone, messageAdded *ResponsesStreamEvent
	var doneOrder, addedOrder []string
	for i := range events {
		e := &events[i]
		switch e.Type {
		case "response.output_item.added":
			addedOrder = append(addedOrder, e.Item.Type)
			if e.Item.Type == "message" {
				messageAdded = e
			}
		case "response.output_item.done":
			doneOrder = append(doneOrder, e.Item.Type)
			if e.Item.Type == "reasoning" {
				reasoningDone = e
			}
		}
	}
	require.NotNil(t, reasoningDone, "reasoning output_item.done missing")
	require.Equal(t, "sealed.v1.xyz", reasoningDone.Item.EncryptedContent)
	require.NotNil(t, messageAdded)
	require.Equal(t, "msg_srv1", messageAdded.Item.ID)
	// 签名一到即补发 reasoning done（早于 finalize 的 message done）。
	require.Equal(t, []string{"reasoning", "message"}, addedOrder)
	require.Equal(t, []string{"reasoning", "message"}, doneOrder)

	// 最终 output 数组同样带 encrypted_content。
	var completed *ResponsesStreamEvent
	for i := range events {
		if events[i].Type == "response.completed" {
			completed = &events[i]
		}
	}
	require.NotNil(t, completed)
	require.Equal(t, "sealed.v1.xyz", completed.Response.Output[0].EncryptedContent)
}

// 无签名上游（signature 永不到达）：reasoning item 由 finalize 兜底关闭，
// 不留挂起项。
func TestStream_NoSignature_FinalizeClosesReasoning(t *testing.T) {
	events := collectStreamEvents2(t, []string{
		`{"choices":[{"index":0,"delta":{"reasoning_content":"think"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	})
	var reasoningDone bool
	for i := range events {
		if events[i].Type == "response.output_item.done" && events[i].Item != nil && events[i].Item.Type == "reasoning" {
			reasoningDone = true
			require.Empty(t, events[i].Item.EncryptedContent)
		}
	}
	require.True(t, reasoningDone, "reasoning item must close at finalize")
}

func collectStreamEvents2(t *testing.T, chunks []string) []ResponsesStreamEvent {
	t.Helper()
	state := NewChatCompletionsToResponsesStreamState("swe-2-max")
	var events []ResponsesStreamEvent
	for _, payload := range chunks {
		var chunk ChatCompletionsChunk
		require.NoError(t, json.Unmarshal([]byte(payload), &chunk))
		events = append(events, ChatCompletionsChunkToResponsesEvents(&chunk, state)...)
	}
	return append(events, FinalizeChatCompletionsResponsesStream(state)...)
}
