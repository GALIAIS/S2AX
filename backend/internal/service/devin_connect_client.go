package service

// Devin local 直连链路：不依赖 devin.exe，直接向 server.codeium.com 发起
// Connect-RPC `ApiServerService/GetChatMessage`（application/connect+proto
// 流式）。devin.exe 本身只是本地 agent loop（session DB + 工具执行 + 注入
// ~19k 系统提示）；对 OpenAI 兼容网关而言，agent loop 由调用方驱动，这里只做
// 纯推理 + 工具协议透传，不注入任何 devin 侧系统提示。
//
// 协议要点（逆向自 devin.exe 流量 + 公开 exa.api_server_pb proto）：
//   - 请求/响应均为 5 字节 envelope：flags(1) + len(4, BE) + payload
//   - flags bit0=gzip，bit1=end-of-stream trailer（JSON，可含 error）
//   - Metadata.api_key 携带 devin-session-token$...（api_key 先经
//     GetSelfDevinSessionToken 换发）；Authorization: Basic <tok>-<tok>

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// protobuf wire 编码助手（字段号对照 exa.api_server_pb / chat_pb /
// codeium_common_pb 生成代码）
// ---------------------------------------------------------------------------

func devinPVVarint(buf *bytes.Buffer, v uint64) {
	var tmp [10]byte
	n := binary.PutUvarint(tmp[:], v)
	buf.Write(tmp[:n])
}

func devinPVTags(buf *bytes.Buffer, field int, wt int) {
	devinPVVarint(buf, uint64(field<<3|wt))
}

func devinPVVar(buf *bytes.Buffer, field int, v uint64) {
	devinPVTags(buf, field, 0)
	devinPVVarint(buf, v)
}

func devinPVBytes(buf *bytes.Buffer, field int, b []byte) {
	devinPVTags(buf, field, 2)
	devinPVVarint(buf, uint64(len(b)))
	buf.Write(b)
}

func devinPVStr(buf *bytes.Buffer, field int, s string) {
	devinPVBytes(buf, field, []byte(s))
}

func devinPVF64(buf *bytes.Buffer, field int, f float64) {
	devinPVTags(buf, field, 1)
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], math.Float64bits(f))
	buf.Write(tmp[:])
}

// devinPVReader 是 GetChatMessageResponse 的极简 proto 解码器。
type devinPVReader struct {
	buf []byte
	i   int
}

func (r *devinPVReader) next() (field int, wt int, ok bool) {
	if r.i >= len(r.buf) {
		return 0, 0, false
	}
	tag, n := binary.Uvarint(r.buf[r.i:])
	if n <= 0 {
		return 0, 0, false
	}
	r.i += n
	return int(tag >> 3), int(tag & 7), true
}

func (r *devinPVReader) varint() uint64 {
	v, n := binary.Uvarint(r.buf[r.i:])
	if n <= 0 {
		return 0
	}
	r.i += n
	return v
}

func (r *devinPVReader) bytes() []byte {
	ln := r.varint()
	if int(ln) > len(r.buf)-r.i {
		r.i = len(r.buf)
		return nil
	}
	b := r.buf[r.i : r.i+int(ln)]
	r.i += int(ln)
	return b
}

func (r *devinPVReader) str() string { return string(r.bytes()) }

func (r *devinPVReader) skip(wt int) {
	switch wt {
	case 0:
		r.varint()
	case 2:
		r.bytes()
	case 5:
		r.i += 4
	case 1:
		r.i += 8
	default:
		r.i = len(r.buf)
	}
	if r.i > len(r.buf) {
		r.i = len(r.buf)
	}
}

// ---------------------------------------------------------------------------
// 请求构造
// ---------------------------------------------------------------------------

// devinChatMsg 对应 chat_pb.ChatMessagePrompt。
type devinChatMsg struct {
	ID         string
	Source     int // 1=USER 2=SYSTEM(assistant) 4=TOOL
	Prompt     string
	Thinking   string
	Signature  string
	ToolCalls  []devinToolCall // assistant 消息携带的工具调用
	ToolCallID string          // source=TOOL 时关联的调用 id
	ToolError  bool
	Images     []devinImage
}

type devinToolCall struct {
	ID            string
	Name          string
	ArgumentsJSON string
}

type devinImage struct {
	Base64   string
	MimeType string
}

func (m *devinChatMsg) marshal(buf *bytes.Buffer) {
	var inner bytes.Buffer
	devinPVStr(&inner, 1, m.ID)
	devinPVVar(&inner, 2, uint64(m.Source))
	if m.Prompt != "" {
		devinPVStr(&inner, 3, m.Prompt)
	}
	for _, tc := range m.ToolCalls {
		var t bytes.Buffer
		devinPVStr(&t, 1, tc.ID)
		devinPVStr(&t, 2, tc.Name)
		devinPVStr(&t, 3, tc.ArgumentsJSON)
		devinPVBytes(&inner, 6, t.Bytes())
	}
	if m.ToolCallID != "" {
		devinPVStr(&inner, 7, m.ToolCallID)
	}
	if m.ToolError {
		devinPVVar(&inner, 9, 1)
	}
	for _, img := range m.Images {
		var im bytes.Buffer
		devinPVStr(&im, 1, img.Base64)
		devinPVStr(&im, 2, img.MimeType)
		devinPVBytes(&inner, 10, im.Bytes())
	}
	if m.Thinking != "" {
		devinPVStr(&inner, 11, m.Thinking)
	}
	if m.Signature != "" {
		devinPVStr(&inner, 12, m.Signature)
	}
	devinPVBytes(buf, 3, inner.Bytes())
}

// devinConnectRequest 一次 GetChatMessage 调用的参数。
type devinConnectRequest struct {
	SystemPrompt string
	Messages     []devinChatMsg
	Model        string // chat_model_uid，如 glm-5-2 / swe-2-max
	Tools        []apicompat.ChatTool
	ToolChoice   json.RawMessage
	MaxTokens    int
	Temperature  *float64
	TopP         *float64
	Stop         []string
	CascadeID    string
}

// devinBuildMetadata 编码 codeium_common_pb.Metadata。
// 字段对应：1=ide_name, 7=ide_version, 12=extension_name, 2=extension_version,
// 3=api_key, 4=locale, 5=os。
func devinBuildMetadata(token string) []byte {
	var m bytes.Buffer
	devinPVStr(&m, 1, "windsurf-next")
	devinPVStr(&m, 7, "3000.10.1023")
	devinPVStr(&m, 12, "chisel")
	devinPVStr(&m, 28, "chisel")
	devinPVStr(&m, 2, "3000.10.1023")
	devinPVStr(&m, 3, token)
	devinPVStr(&m, 4, "en")
	devinPVStr(&m, 5, runtime.GOOS)
	return m.Bytes()
}

// devinBuildChatRequestBody 编码完整 GetChatMessageRequest。
func devinBuildChatRequestBody(token string, req *devinConnectRequest) []byte {
	var b bytes.Buffer
	devinPVBytes(&b, 1, devinBuildMetadata(token))
	if req.SystemPrompt != "" {
		devinPVStr(&b, 2, req.SystemPrompt)
	}
	for i := range req.Messages {
		req.Messages[i].marshal(&b)
	}
	// 7: request_type = CASCADE(5)
	devinPVVar(&b, 7, 5)
	// 8: configuration
	var cfg bytes.Buffer
	devinPVVar(&cfg, 1, 1) // num_completions
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = 64000
	}
	devinPVVar(&cfg, 2, uint64(maxTok))
	devinPVVar(&cfg, 3, 200) // max_newlines
	temp := 0.4
	if req.Temperature != nil {
		temp = *req.Temperature
	}
	// 上游拒绝 temperature<=0（采样器除零）；钳到最小正值近似贪婪解码。
	if temp <= 0 {
		temp = 0.001
	}
	devinPVF64(&cfg, 5, temp)
	devinPVF64(&cfg, 6, temp) // first_temperature
	devinPVVar(&cfg, 7, 50)   // top_k
	topP := 1.0
	if req.TopP != nil {
		topP = *req.TopP
	}
	devinPVF64(&cfg, 8, topP)
	stops := append([]string{}, devinDefaultStopPatterns...)
	stops = append(stops, req.Stop...)
	for _, s := range stops {
		devinPVStr(&cfg, 9, s)
	}
	devinPVF64(&cfg, 11, 1) // fim_eot_prob_threshold
	devinPVBytes(&b, 8, cfg.Bytes())
	// 10: tools
	for _, t := range req.Tools {
		if t.Function == nil || t.Function.Name == "" {
			continue
		}
		var td bytes.Buffer
		devinPVStr(&td, 1, t.Function.Name)
		if t.Function.Description != "" {
			devinPVStr(&td, 2, t.Function.Description)
		}
		if len(t.Function.Parameters) > 0 {
			devinPVStr(&td, 3, string(t.Function.Parameters))
		}
		if t.Function.Strict != nil && *t.Function.Strict {
			devinPVVar(&td, 12, 1)
		}
		devinPVBytes(&b, 10, td.Bytes())
	}
	// 11: disable_parallel_tool_calls
	devinPVVar(&b, 11, 1)
	// 12: tool_choice {option_name}
	if oc := devinToolChoiceOption(req.ToolChoice); oc != "" {
		var tc bytes.Buffer
		devinPVStr(&tc, 1, oc)
		devinPVBytes(&b, 12, tc.Bytes())
	}
	// 13: system_prompt_cache_options {type=EPHEMERAL(1)}
	var cache bytes.Buffer
	devinPVVar(&cache, 1, 1)
	devinPVBytes(&b, 13, cache.Bytes())
	// 16: cascade_id
	cascadeID := req.CascadeID
	if cascadeID == "" {
		cascadeID = uuid.NewString()
	}
	devinPVStr(&b, 16, cascadeID)
	// 20: planner_mode = DEFAULT(1)
	devinPVVar(&b, 20, 1)
	// 21: chat_model_uid
	devinPVStr(&b, 21, req.Model)
	// 22: execution_id
	devinPVStr(&b, 22, uuid.NewString())
	return b.Bytes()
}

var devinDefaultStopPatterns = []string{
	"<|user|>", "<|bot|>", "<|context_request|>", "<|endoftext|>", "<|end_of_turn|>",
}

// devinToolChoiceOption 把 OpenAI tool_choice 映射成 option_name。
// "auto"/"required"/"none" 原样；{"type":"function","function":{"name":X}}
// 仍走 optionName=auto（上游 ChatToolChoice.tool_name 语义不同，不强转）。
func devinToolChoiceOption(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch s {
		case "auto", "required", "none":
			return s
		}
		return ""
	}
	return "auto"
}

// ---------------------------------------------------------------------------
// Connect streaming 客户端
// ---------------------------------------------------------------------------

// devinChatEvent 是 GetChatMessage 响应流里的一帧。
type devinChatEvent struct {
	Kind       string // "text" | "thinking" | "toolcall" | "usage" | "done" | "error"
	Text       string
	Signature  string
	ToolCall   *devinToolCall
	Usage      *devinUsageStats
	StopReason int
	Err        error
}

type devinUsageStats struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	ModelUID         string
}

const (
	devinConnectCompressed = 0x01
	devinConnectEndStream  = 0x02
	devinMaxFramePayload   = 16 << 20
)

// devinChatStream 发起 GetChatMessage 调用并把响应帧解析成事件通道。
func devinChatStream(ctx context.Context, apiBase, token, proxyURL string, req *devinConnectRequest) (<-chan devinChatEvent, error) {
	body := devinBuildChatRequestBody(token, req)
	var frame bytes.Buffer
	frame.WriteByte(0) // uncompressed
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(body)))
	frame.Write(lenBuf[:])
	frame.Write(body)

	endpoint := strings.TrimRight(apiBase, "/") + "/exa.api_server_pb.ApiServerService/GetChatMessage"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &frame)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/connect+proto")
	httpReq.Header.Set("Connect-Protocol-Version", "1")
	httpReq.Header.Set("Accept-Encoding", "identity")
	httpReq.Header.Set("Connect-Accept-Encoding", "gzip")
	httpReq.Header.Set("User-Agent", "connect-go/1.18.1")
	httpReq.Header.Set("Authorization", "Basic "+token+"-"+token)

	client := &http.Client{Timeout: 0}
	if p := strings.TrimSpace(proxyURL); p != "" {
		if parsed, err := url.Parse(p); err == nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(parsed)}
		}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, RequestScopedTransient: true}
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, &UpstreamFailoverError{
			StatusCode:   resp.StatusCode,
			ResponseBody: snippet,
			Stage:        GatewayFailureStageInference,
			Scope:        GatewayFailureScopeProvider,
		}
	}

	events := make(chan devinChatEvent, 64)
	go devinReadConnectStream(resp.Body, events)
	return events, nil
}

// devinReadConnectStream 逐帧解析 connect 流并产出事件。
func devinReadConnectStream(body io.ReadCloser, events chan<- devinChatEvent) {
	defer close(events)
	defer func() { _ = body.Close() }()

	br := bufio.NewReaderSize(body, 64<<10)
	for {
		hdr := make([]byte, 5)
		if _, err := io.ReadFull(br, hdr); err != nil {
			if !errors.Is(err, io.EOF) {
				events <- devinChatEvent{Kind: "error", Err: err}
			}
			return
		}
		flag := hdr[0]
		ln := binary.BigEndian.Uint32(hdr[1:])
		if ln > devinMaxFramePayload {
			events <- devinChatEvent{Kind: "error", Err: fmt.Errorf("connect frame %dB exceeds cap", ln)}
			return
		}
		payload := make([]byte, ln)
		if _, err := io.ReadFull(br, payload); err != nil {
			events <- devinChatEvent{Kind: "error", Err: err}
			return
		}
		if flag&devinConnectCompressed != 0 {
			zr, err := gzip.NewReader(bytes.NewReader(payload))
			if err != nil {
				events <- devinChatEvent{Kind: "error", Err: err}
				return
			}
			dec, err := io.ReadAll(zr)
			_ = zr.Close()
			if err != nil {
				events <- devinChatEvent{Kind: "error", Err: err}
				return
			}
			payload = dec
		}
		if flag&devinConnectEndStream != 0 {
			if e := devinParseTrailerError(payload); e != nil {
				events <- devinChatEvent{Kind: "error", Err: e}
				return
			}
			events <- devinChatEvent{Kind: "done"}
			return
		}
		devinParseChatMessage(payload, events)
	}
}

// devinParseTrailerError 解析 end-of-stream JSON trailer 的 error 字段。
func devinParseTrailerError(payload []byte) error {
	var t struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &t); err != nil || t.Error == nil {
		return nil
	}
	return fmt.Errorf("devin stream error %s: %s", t.Error.Code, t.Error.Message)
}

// devinParseChatMessage 解码一帧 GetChatMessageResponse：
// 1=message_id 3=delta_text 5=stop_reason 6=delta_tool_calls
// 7=usage 9=delta_thinking 10=delta_signature。
func devinParseChatMessage(payload []byte, events chan<- devinChatEvent) {
	r := &devinPVReader{buf: payload}
	for {
		fn, wt, ok := r.next()
		if !ok {
			return
		}
		switch fn {
		case 3:
			if wt == 2 {
				if s := r.str(); s != "" {
					events <- devinChatEvent{Kind: "text", Text: s}
				}
			} else {
				r.skip(wt)
			}
		case 5:
			if wt == 0 {
				events <- devinChatEvent{Kind: "stop", StopReason: int(r.varint())}
			} else {
				r.skip(wt)
			}
		case 6:
			if wt == 2 {
				if tc := devinParseToolCall(r.bytes()); tc != nil {
					events <- devinChatEvent{Kind: "toolcall", ToolCall: tc}
				}
			} else {
				r.skip(wt)
			}
		case 7:
			if wt == 2 {
				if u := devinParseUsageStats(r.bytes()); u != nil {
					events <- devinChatEvent{Kind: "usage", Usage: u}
				}
			} else {
				r.skip(wt)
			}
		case 9:
			if wt == 2 {
				if s := r.str(); s != "" {
					events <- devinChatEvent{Kind: "thinking", Text: s}
				}
			} else {
				r.skip(wt)
			}
		case 10:
			if wt == 2 {
				if s := r.str(); s != "" {
					events <- devinChatEvent{Kind: "signature", Signature: s}
				}
			} else {
				r.skip(wt)
			}
		default:
			r.skip(wt)
		}
	}
}

// devinParseToolCall 解码 codeium_common_pb.ChatToolCall{1:id,2:name,3:args}。
func devinParseToolCall(b []byte) *devinToolCall {
	r := &devinPVReader{buf: b}
	tc := &devinToolCall{}
	for {
		fn, wt, ok := r.next()
		if !ok {
			break
		}
		if wt != 2 {
			r.skip(wt)
			continue
		}
		switch fn {
		case 1:
			tc.ID = r.str()
		case 2:
			tc.Name = r.str()
		case 3:
			tc.ArgumentsJSON = r.str()
		default:
			r.skip(wt)
		}
	}
	if tc.ID == "" && tc.Name == "" && tc.ArgumentsJSON == "" {
		return nil
	}
	return tc
}

// devinParseUsageStats 解码 ModelUsageStats{2:input,3:output,4:cache_write,5:cache_read,9:model_uid}。
func devinParseUsageStats(b []byte) *devinUsageStats {
	r := &devinPVReader{buf: b}
	u := &devinUsageStats{}
	for {
		fn, wt, ok := r.next()
		if !ok {
			break
		}
		switch {
		case wt == 0 && fn == 2:
			u.InputTokens = int(r.varint())
		case wt == 0 && fn == 3:
			u.OutputTokens = int(r.varint())
		case wt == 0 && fn == 4:
			u.CacheWriteTokens = int(r.varint())
		case wt == 0 && fn == 5:
			u.CacheReadTokens = int(r.varint())
		case wt == 2 && fn == 9:
			u.ModelUID = r.str()
		default:
			r.skip(wt)
		}
	}
	return u
}

// ---------------------------------------------------------------------------
// OpenAI messages -> chatMessagePrompts
// ---------------------------------------------------------------------------

// devinConvertMessages 把 ChatCompletions 消息映射成 ChatMessagePrompt 列表：
//   - system/developer -> 顶层 prompt（不混入历史）
//   - user -> source=USER
//   - assistant -> source=SYSTEM + tool_calls + reasoning(signature)
//   - tool -> source=TOOL + tool_call_id + tool_result_is_error
//
// 与 devin.exe 的差异：不注入任何 devin 侧 system 提示/工具提示——agent loop
// 由 OpenAI 客户端驱动，上下文即用户给的 messages 本身。
func devinConvertMessages(req *apicompat.ChatCompletionsRequest, cascadeID string) (system string, msgs []devinChatMsg) {
	var sysParts []string
	for i, m := range req.Messages {
		id := deterministicDevinMsgID(cascadeID, i, m.Role)
		switch m.Role {
		case "system", "developer":
			if s := devinMessageText(m); s != "" {
				sysParts = append(sysParts, s)
			}
		case "assistant":
			dm := devinChatMsg{ID: id, Source: 2, Prompt: devinMessageText(m)}
			if think := m.ReasoningContent + m.Reasoning; think != "" {
				dm.Thinking = think
			}
			for _, tc := range m.ToolCalls {
				dm.ToolCalls = append(dm.ToolCalls, devinToolCall{
					ID:            tc.ID,
					Name:          tc.Function.Name,
					ArgumentsJSON: tc.Function.Arguments,
				})
			}
			msgs = append(msgs, dm)
		case "tool", "function":
			dm := devinChatMsg{
				ID:         id,
				Source:     4,
				Prompt:     devinMessageText(m),
				ToolCallID: m.ToolCallID,
			}
			msgs = append(msgs, dm)
		default: // user 及其他
			dm := devinChatMsg{ID: id, Source: 1, Prompt: devinMessageText(m)}
			dm.Images = devinMessageImages(m)
			msgs = append(msgs, dm)
		}
	}
	return strings.Join(sysParts, "\n\n"), msgs
}

// devinMessageText 取消息的纯文本（字符串 content 或 text parts 拼接）。
func devinMessageText(m apicompat.ChatMessage) string {
	if len(m.Content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return s
	}
	var parts []apicompat.ChatContentPart
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// devinMessageImages 提取 data URI 图片。
func devinMessageImages(m apicompat.ChatMessage) []devinImage {
	var parts []apicompat.ChatContentPart
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return nil
	}
	var out []devinImage
	for _, p := range parts {
		if p.Type != "image_url" || p.ImageURL == nil {
			continue
		}
		u := p.ImageURL.URL
		if !strings.HasPrefix(u, "data:") {
			continue
		}
		semi := strings.Index(u, ";base64,")
		if semi <= 5 {
			continue
		}
		out = append(out, devinImage{Base64: u[semi+8:], MimeType: u[5:semi]})
	}
	return out
}

// deterministicDevinMsgID 生成稳定消息 id（同 cascadeId+index+role 不变）。
func deterministicDevinMsgID(cascadeID string, idx int, role string) string {
	h := sha256.Sum256([]byte(cascadeID + "\x00" + fmt.Sprint(idx) + "\x00" + role))
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

// ---------------------------------------------------------------------------
// session token 解析（api_key -> GetSelfDevinSessionToken 换发）
// ---------------------------------------------------------------------------

var devinSessionTokenCache sync.Map // accountID -> string(session token)

// resolveDevinSessionToken 取账号可用的 devin session token：
// 凭证直接存了 session_token / api_key 为 devin-session-token$ 前缀时原样返回；
// 否则用 api_key 调 SeatManagementService/GetSelfDevinSessionToken（Connect-JSON）换发。
func resolveDevinSessionToken(ctx context.Context, account *Account, proxyURL string) (string, error) {
	if tok := account.DevinSessionToken(); tok != "" {
		return tok, nil
	}
	apiKey := account.DevinAPIKey()
	if apiKey == "" {
		return "", fmt.Errorf("account %d missing devin session_token/api_key credential", account.ID)
	}
	if cached, ok := devinSessionTokenCache.Load(account.ID); ok {
		if tok, _ := cached.(string); tok != "" {
			return tok, nil
		}
	}

	endpoint := account.DevinAPIServerURL() + "/exa.seat_management_pb.SeatManagementService/GetSelfDevinSessionToken"
	reqBody, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"api_key":           apiKey,
			"ide_name":          "sub2api",
			"extension_name":    "sub2api",
			"extension_version": "1.0.0",
		},
	})
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if proxy := strings.TrimSpace(proxyURL); proxy != "" {
		if parsed, err := url.Parse(proxy); err == nil {
			httpClient.Transport = &http.Transport{Proxy: http.ProxyURL(parsed)}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(reqBody)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("X-Api-Key", apiKey)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", &UpstreamFailoverError{StatusCode: http.StatusBadGateway, RequestScopedTransient: true}
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if resp.StatusCode != http.StatusOK {
		return "", &UpstreamFailoverError{
			StatusCode:   resp.StatusCode,
			ResponseBody: respBody,
			Stage:        GatewayFailureStageAccountAuth,
			Scope:        GatewayFailureScopeAccount,
			Reason:       GatewayFailureReason("devin_session_token_mint_failed"),
		}
	}
	var out struct {
		SessionToken string `json:"sessionToken"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil || out.SessionToken == "" {
		return "", fmt.Errorf("devin GetSelfDevinSessionToken: empty sessionToken (status=%d)", resp.StatusCode)
	}
	devinSessionTokenCache.Store(account.ID, out.SessionToken)
	return out.SessionToken, nil
}
