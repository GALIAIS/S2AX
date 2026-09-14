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
	"math/rand"
	"net"
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
	// SignatureType/OutputID 必须随 signature 原样回传：实测错配
	// signature_type 触发上游 invalid_argument。
	SignatureType    string
	OutputID         string
	ThinkingRedacted bool
	ToolCalls        []devinToolCall // assistant 消息携带的工具调用
	ToolCallID       string          // source=TOOL 时关联的调用 id
	ToolError        bool
	Images           []devinImage
	// PromptCache 标记 EPHEMERAL 断点：缓存到该消息为止的全部历史前缀。
	PromptCache bool
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
	if m.PromptCache {
		// 8: prompt_cache_options {type=CACHE_CONTROL_TYPE_EPHEMERAL(1)}
		var pc bytes.Buffer
		devinPVVar(&pc, 1, 1)
		devinPVBytes(&inner, 8, pc.Bytes())
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
	if m.ThinkingRedacted {
		devinPVVar(&inner, 13, 1)
	}
	if m.OutputID != "" {
		devinPVStr(&inner, 15, m.OutputID)
	}
	if m.SignatureType != "" {
		devinPVStr(&inner, 18, m.SignatureType)
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
	TopK         *int
	Seed         *int64
	Stop         []string
	TrajectoryID string // cortex 轨迹标识，会话内稳定
	StepIndex    int32  // 会话内单调步数（真实 CLI 每请求发送）
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
	topK := uint64(50)
	if req.TopK != nil && *req.TopK > 0 {
		topK = uint64(*req.TopK)
	}
	devinPVVar(&cfg, 7, topK)
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
	if req.Seed != nil {
		devinPVVar(&cfg, 10, uint64(*req.Seed)) // seed
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
	// 12: tool_choice oneof {1:option_name, 2:tool_name}
	if oc, tn := devinToolChoiceOption(req.ToolChoice); oc != "" || tn != "" {
		var tc bytes.Buffer
		if tn != "" {
			devinPVStr(&tc, 2, tn)
		} else {
			devinPVStr(&tc, 1, oc)
		}
		devinPVBytes(&b, 12, tc.Bytes())
	}
	// 13: system_prompt_cache_options {type=EPHEMERAL(1)}
	var cache bytes.Buffer
	devinPVVar(&cache, 1, 1)
	devinPVBytes(&b, 13, cache.Bytes())
	// 15: trajectory_reference {1:trajectory_id, 2:step_index, 3:trajectory_type, 4:step_type}
	// 真实 CLI 必发：会话内单调 step_index 关联同一 trajectory。
	if req.TrajectoryID != "" {
		var tr bytes.Buffer
		devinPVStr(&tr, 1, req.TrajectoryID)
		devinPVVar(&tr, 2, uint64(req.StepIndex))
		devinPVVar(&tr, 3, 4)  // CORTEX_TRAJECTORY_TYPE_CASCADE
		devinPVVar(&tr, 4, 14) // CORTEX_STEP_TYPE_USER_INPUT
		devinPVBytes(&b, 15, tr.Bytes())
	}
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

// devinToolChoiceOption 把 OpenAI tool_choice 映射成上游 ChatToolChoice oneof：
// "auto"/"required"/"none" -> option_name(field 1)；
// {"type":"function","function":{"name":X}} -> tool_name(field 2)。
// auto 上游缺省即等价，仍发送以保持显式语义。
func devinToolChoiceOption(raw json.RawMessage) (option, toolName string) {
	if len(raw) == 0 {
		return "", ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch s {
		case "auto", "required", "none":
			return s, ""
		}
		return "", ""
	}
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Type == "function" && obj.Function.Name != "" {
		return "", obj.Function.Name
	}
	return "auto", ""
}

// ---------------------------------------------------------------------------
// Connect streaming 客户端
// ---------------------------------------------------------------------------

// devinChatEvent 是 GetChatMessage 响应流里的一帧。
type devinChatEvent struct {
	Kind          string // "text" | "thinking" | "toolcall" | "usage" | "done" | "error"
	Text          string
	Signature     string
	SignatureType string
	OutputID      string
	ToolCall      *devinToolCall
	Usage         *devinUsageStats
	StopReason    int
	Err           error
}

type devinUsageStats struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	ModelUID         string
}

// devinTotalInputTokens 上游 input_tokens 只计未命中缓存的输入部分；
// 按 OpenAIUsage 惯例 InputTokens 记总量（=input+cache_read+cache_write），
// 计费层再按 cache 子集反算实际输入。
func devinTotalInputTokens(u *devinUsageStats) int {
	return u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

const (
	devinConnectCompressed = 0x01
	devinConnectEndStream  = 0x02
	devinMaxFramePayload   = 16 << 20
)

// devinMaxConnectAttempts 是 GetChatMessage 建流阶段对瞬时传输错误的最大尝试次数。
const devinMaxConnectAttempts = 3

// devinChatStream 发起 GetChatMessage 调用并把响应帧解析成事件通道。
// 建流阶段对瞬时传输错误（EOF/连接重置/超时）重试最多 3 次，递增退避
// 加 ±25% 抖动；流一旦建立，错误只通过事件通道上报不再重发。
func devinChatStream(ctx context.Context, apiBase, token, proxyURL string, req *devinConnectRequest) (<-chan devinChatEvent, error) {
	var lastErr error
	for attempt := 0; attempt < devinMaxConnectAttempts; attempt++ {
		if attempt > 0 {
			base := time.Duration(attempt) * 400 * time.Millisecond
			backoff := time.Duration(float64(base) * (0.75 + 0.5*devinRandFloat()))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		events, err := devinChatStreamOnce(ctx, apiBase, token, proxyURL, req)
		if err == nil {
			return events, nil
		}
		lastErr = err
		if !devinIsTransientStreamError(err) {
			break
		}
	}
	return nil, lastErr
}

// devinRandFloat 取 [0,1) 随机数做退避抖动。
var devinRandFloat = func() float64 {
	return rand.Float64()
}

// devinIsTransientStreamError 判断建流/流内错误是否为传输层断裂：
// 只对 EOF、UnexpectedEOF、net.Error（含超时/连接重置）重试；上游语义
// 拒绝（非 200 状态、trailer 错误）重试只会复现同样失败，直接放行。
func devinIsTransientStreamError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// client.Do 失败时已包成 RequestScopedTransient 的 502——可重试。
	var fo *UpstreamFailoverError
	if errors.As(err, &fo) {
		return fo.RequestScopedTransient && fo.ResponseBody == nil
	}
	return false
}

// devinChatStreamOnce 执行单次建流：发送请求帧，成功后启动读协程。
func devinChatStreamOnce(ctx context.Context, apiBase, token, proxyURL string, req *devinConnectRequest) (<-chan devinChatEvent, error) {
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
	// 真实 CLI 不发送 User-Agent；置空对齐 wire 形态（Go 默认会补
	// Go-http-client，空值显式抑制）。
	httpReq.Header["User-Agent"] = []string{}
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
// 7=usage 9=delta_thinking 10=delta_signature 15=output_id
// 21=delta_signature_type。
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
		case 15:
			if wt == 2 {
				if s := r.str(); s != "" {
					events <- devinChatEvent{Kind: "output_id", OutputID: s}
				}
			} else {
				r.skip(wt)
			}
		case 21:
			if wt == 2 {
				if s := r.str(); s != "" {
					events <- devinChatEvent{Kind: "signature_type", SignatureType: s}
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
//   - user -> source=USER（仅「当前轮」允许携带图片）
//   - assistant -> source=SYSTEM + tool_calls + thinking/signature 回传
//   - tool -> source=TOOL + tool_call_id
//
// 与 devin.exe 的差异：不注入任何 devin 侧 system 提示/工具提示——agent loop
// 由 OpenAI 客户端驱动，上下文即用户给的 messages 本身。
func devinConvertMessages(req *apicompat.ChatCompletionsRequest) (system string, msgs []devinChatMsg) {
	var sysParts []string
	// Devin/Cascade 只可靠接受「当前轮」图片（最后一条 assistant 之后的
	// user/tool 消息）；历史图进 Images 触发上游 invalid_argument，降级为
	// 文本占位保住上下文语义。
	lastAssistant := -1
	for i, m := range req.Messages {
		if m.Role == "assistant" {
			lastAssistant = i
		}
	}
	for i, m := range req.Messages {
		switch m.Role {
		case "system", "developer":
			if s := devinMessageText(m); s != "" {
				sysParts = append(sysParts, s)
			}
		case "assistant":
			dm := devinChatMsg{Source: 2, Prompt: devinMessageText(m)}
			if think := m.ReasoningContent + m.Reasoning; think != "" {
				dm.Thinking = think
			}
			dm.Signature = m.Signature
			dm.SignatureType = m.SignatureType
			dm.OutputID = m.OutputID
			// 有签名而无 thinking 明文 = redacted 推理轮次，原样标回。
			dm.ThinkingRedacted = dm.Signature != "" && dm.Thinking == ""
			for _, tc := range m.ToolCalls {
				dm.ToolCalls = append(dm.ToolCalls, devinToolCall{
					ID:            tc.ID,
					Name:          tc.Function.Name,
					ArgumentsJSON: tc.Function.Arguments,
				})
			}
			// 完全空的 assistant（无文本无调用）会诱发上游空回复退化，跳过。
			if dm.Prompt == "" && len(dm.ToolCalls) == 0 && dm.Thinking == "" && dm.Signature == "" {
				continue
			}
			msgs = append(msgs, dm)
		case "tool", "function":
			dm := devinChatMsg{
				Source:     4,
				Prompt:     devinMessageText(m),
				ToolCallID: m.ToolCallID,
			}
			if dm.Prompt == "" {
				dm.Prompt = "[tool result]" // 上游不接受空工具结果文本
			}
			msgs = append(msgs, dm)
		default: // user 及其他
			dm := devinChatMsg{Source: 1, Prompt: devinMessageText(m)}
			images := devinMessageImages(m)
			if i > lastAssistant {
				dm.Images = images
			} else {
				for range images {
					if dm.Prompt != "" {
						dm.Prompt += "\n"
					}
					dm.Prompt += "[Image omitted from history]"
				}
			}
			msgs = append(msgs, dm)
		}
	}
	// 上游要求 call→result 紧邻配对；OpenAI 历史是「全部调用→全部结果」
	// 分组结构，按 call id 重排成交错序列；无配对的孤立结果降级为 USER。
	msgs = devinPairToolCallsWithResults(msgs)
	msgs = devinDemoteOrphanToolResults(msgs)
	// 重排后的最终位置决定内容寻址 ID；最后一条消息标 EPHEMERAL 缓存断点，
	// 下一轮新消息追加在断点后即可命中历史前缀缓存。
	for i := range msgs {
		msgs[i].ID = deterministicDevinMsgID(i, &msgs[i])
	}
	if n := len(msgs); n > 0 {
		msgs[n-1].PromptCache = true
	}
	return strings.Join(sysParts, "\n\n"), msgs
}

// devinPairToolCallsWithResults 把「连续调用消息 + 连续结果消息」的分组
// 序列重排为 call_i, result_i, call_j, result_j 交错序列。已交错的序列
// 保持不变；找不到匹配结果的调用与孤立结果都按原序保留（后者由
// devinDemoteOrphanToolResults 处理）。
func devinPairToolCallsWithResults(msgs []devinChatMsg) []devinChatMsg {
	isCall := func(m *devinChatMsg) bool { return m.Source == 2 && len(m.ToolCalls) > 0 }
	isResult := func(m *devinChatMsg) bool { return m.Source == 4 }
	var out []*devinChatMsg
	for i := 0; i < len(msgs); {
		if !isCall(&msgs[i]) {
			out = append(out, &msgs[i])
			i++
			continue
		}
		var calls []*devinChatMsg
		for i < len(msgs) && isCall(&msgs[i]) {
			calls = append(calls, &msgs[i])
			i++
		}
		byID := make(map[string]*devinChatMsg)
		j := i
		for j < len(msgs) && isResult(&msgs[j]) {
			byID[msgs[j].ToolCallID] = &msgs[j]
			j++
		}
		consumed := make(map[string]struct{})
		for _, cp := range calls {
			out = append(out, cp)
			for _, call := range cp.ToolCalls {
				// 同 id 重复调用按位置绑定：配对消费后即移除，第二个
				// 同 id 调用不再挂到同一份结果上。
				if res, ok := byID[call.ID]; ok {
					out = append(out, res)
					consumed[call.ID] = struct{}{}
					delete(byID, call.ID)
				}
			}
		}
		for k := i; k < j; k++ {
			if _, ok := consumed[msgs[k].ToolCallID]; !ok {
				out = append(out, &msgs[k])
			}
		}
		i = j
	}
	res := make([]devinChatMsg, 0, len(out))
	for _, p := range out {
		res = append(res, *p)
	}
	return res
}

// devinDemoteOrphanToolResults 把找不到对应 tool call 的孤立 TOOL 结果
// （客户端压缩丢掉 function_call 时产生）降级为 USER 文本消息：
// 上游对无配对 TOOL prompt 返回 invalid_argument，降级保住结果内容。
func devinDemoteOrphanToolResults(msgs []devinChatMsg) []devinChatMsg {
	callIDs := make(map[string]struct{})
	for _, m := range msgs {
		for _, c := range m.ToolCalls {
			callIDs[c.ID] = struct{}{}
		}
	}
	for i := range msgs {
		m := &msgs[i]
		if m.Source != 4 {
			continue
		}
		if _, ok := callIDs[m.ToolCallID]; ok {
			continue
		}
		msgs[i] = devinChatMsg{
			Source:      1,
			Prompt:      "[tool result, original call lost]\n" + m.Prompt,
			Images:      m.Images,
			PromptCache: m.PromptCache,
		}
	}
	return msgs
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

// devinUUIDFromBytes 把 16 字节格式化为 UUID 字符串。
func devinUUIDFromBytes(b []byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// deterministicDevinMsgID 生成内容寻址的消息 id：同一对话历史中同位置
// 同内容的消息跨请求得到相同 id，保证请求字节流前缀稳定（上游
// EPHEMERAL prompt cache 才能命中）。不得混入每请求随机值。
func deterministicDevinMsgID(idx int, m *devinChatMsg) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d\x00%d\x00%s\x00%s\x00%s\x00%t",
		idx, m.Source, m.Prompt, m.ToolCallID, m.Thinking, m.ToolError)
	for _, tc := range m.ToolCalls {
		fmt.Fprintf(h, "\x00%s\x00%s\x00%s", tc.ID, tc.Name, tc.ArgumentsJSON)
	}
	for _, img := range m.Images {
		fmt.Fprintf(h, "\x00%s\x00%s", img.MimeType, img.Base64)
	}
	sum := h.Sum(nil)
	return devinUUIDFromBytes(sum[:16])
}

// deriveDevinSessionIDs 为一次请求派生上游 trajectory/cascade ID：
//   - 优先取客户端显式会话键（prompt_cache_key / user）：线程级标识，
//     压缩改写历史也不打断轨迹连续性；
//   - 无显式键时退回「system 头 4KB + 首条非 system 消息文本头 1KB」
//     内容哈希：同会话多轮前缀不变 → 稳定，不同会话 → 自然分散；
//   - token 作账号级盐混种：不同账号即使前缀相同也不共享轨迹。
//
// 同一 hash 拆两半：前 16 字节 trajectory_id，后 16 字节 cascade_id。
func deriveDevinSessionIDs(req *apicompat.ChatCompletionsRequest, systemPrompt, token string) (trajectoryID, cascadeID string) {
	var seed strings.Builder
	fmt.Fprintf(&seed, "acct\x00%s\x00", token)
	sessionKey := req.PromptCacheKey
	if sessionKey == "" {
		sessionKey = req.User
	}
	if sessionKey != "" {
		fmt.Fprintf(&seed, "sess\x00%s", sessionKey)
	} else {
		head := systemPrompt
		if len(head) > 4096 {
			head = head[:4096]
		}
		seed.WriteString(head)
		for _, m := range req.Messages {
			if m.Role == "system" || m.Role == "developer" {
				continue
			}
			text := devinMessageText(m)
			if text == "" {
				continue
			}
			if len(text) > 1024 {
				text = text[:1024]
			}
			seed.WriteByte(0)
			seed.WriteString(text)
			break
		}
	}
	sum := sha256.Sum256([]byte(seed.String()))
	return devinUUIDFromBytes(sum[:16]), devinUUIDFromBytes(sum[16:32])
}

// devinStepIndexRegistry 按 trajectory_id 记录已发送的上游步数：真实 CLI
// 每请求发送会话内单调递增的 step_index（抓包实测）。计数随进程重启归零，
// 与 CLI 重启行为一致；容量封顶防止会话数累积成无界 map。
var devinStepIndexRegistry = struct {
	sync.Mutex
	counts map[string]int32
}{counts: make(map[string]int32)}

func devinNextStepIndex(trajectoryID string) int32 {
	devinStepIndexRegistry.Lock()
	defer devinStepIndexRegistry.Unlock()
	if len(devinStepIndexRegistry.counts) >= 65536 {
		devinStepIndexRegistry.counts = make(map[string]int32)
	}
	devinStepIndexRegistry.counts[trajectoryID]++
	return devinStepIndexRegistry.counts[trajectoryID]
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
