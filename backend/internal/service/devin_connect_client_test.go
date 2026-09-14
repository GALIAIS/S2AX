package service

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

// devinTestFrame 构造一帧 Connect envelope。
func devinTestFrame(flag byte, payload []byte) []byte {
	var b bytes.Buffer
	b.WriteByte(flag)
	var ln [4]byte
	binary.BigEndian.PutUint32(ln[:], uint32(len(payload)))
	b.Write(ln[:])
	b.Write(payload)
	return b.Bytes()
}

// devinTestChatMsg 构造一帧 GetChatMessageResponse payload。
func devinTestChatMsg(fields func(b *bytes.Buffer)) []byte {
	var b bytes.Buffer
	fields(&b)
	return b.Bytes()
}

func devinCollectEvents(t *testing.T, stream []byte) []devinChatEvent {
	t.Helper()
	events := make(chan devinChatEvent, 64)
	go devinReadConnectStream(io.NopCloser(bytes.NewReader(stream)), events)
	var out []devinChatEvent
	for ev := range events {
		out = append(out, ev)
	}
	return out
}

func TestDevinConnectStream_TextAndDone(t *testing.T) {
	msg := devinTestChatMsg(func(b *bytes.Buffer) {
		devinPVStr(b, 3, "Hello")
		devinPVVar(b, 5, 1) // stop_reason=incomplete
	})
	stream := append(devinTestFrame(0, msg), devinTestFrame(devinConnectEndStream, []byte(`{}`))...)
	evs := devinCollectEvents(t, stream)
	if len(evs) != 3 {
		t.Fatalf("events=%d want 3: %+v", len(evs), evs)
	}
	if evs[0].Kind != "text" || evs[0].Text != "Hello" {
		t.Fatalf("ev0=%+v", evs[0])
	}
	if evs[1].Kind != "stop" || evs[1].StopReason != 1 {
		t.Fatalf("ev1=%+v", evs[1])
	}
	if evs[2].Kind != "done" {
		t.Fatalf("ev2=%+v", evs[2])
	}
}

func TestDevinConnectStream_GzipFrame(t *testing.T) {
	msg := devinTestChatMsg(func(b *bytes.Buffer) {
		devinPVStr(b, 9, "thinking hard")
	})
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(msg)
	_ = zw.Close()
	stream := append(devinTestFrame(devinConnectCompressed, gz.Bytes()),
		devinTestFrame(devinConnectEndStream, []byte(`{}`))...)
	evs := devinCollectEvents(t, stream)
	if len(evs) != 2 || evs[0].Kind != "thinking" || evs[0].Text != "thinking hard" {
		t.Fatalf("evs=%+v", evs)
	}
}

func TestDevinConnectStream_ToolCallAcrossFrames(t *testing.T) {
	// arguments_json 分三帧增量到达：'{' / '"a":1' / '}'
	mk := func(args string) []byte {
		var tc bytes.Buffer
		devinPVStr(&tc, 1, "call_1")
		devinPVStr(&tc, 2, "fn")
		devinPVStr(&tc, 3, args)
		return devinTestChatMsg(func(b *bytes.Buffer) {
			devinPVBytes(b, 6, tc.Bytes())
		})
	}
	var stream []byte
	stream = append(stream, devinTestFrame(0, mk("{"))...)
	stream = append(stream, devinTestFrame(0, mk(`"a":1`))...)
	stream = append(stream, devinTestFrame(0, mk("}"))...)
	stream = append(stream, devinTestFrame(devinConnectEndStream, []byte(`{}`))...)
	evs := devinCollectEvents(t, stream)
	var parts []string
	for _, ev := range evs {
		if ev.Kind == "toolcall" {
			parts = append(parts, ev.ToolCall.ArgumentsJSON)
		}
	}
	if len(parts) != 3 || parts[0]+parts[1]+parts[2] != `{"a":1}` {
		t.Fatalf("parts=%v", parts)
	}
}

func TestDevinConnectStream_SignatureAndUsage(t *testing.T) {
	var usage bytes.Buffer
	devinPVVar(&usage, 2, 100) // input
	devinPVVar(&usage, 3, 20)  // output
	devinPVVar(&usage, 5, 40)  // cache_read
	msg := devinTestChatMsg(func(b *bytes.Buffer) {
		devinPVStr(b, 10, "sig-abc")
		devinPVBytes(b, 7, usage.Bytes())
	})
	stream := append(devinTestFrame(0, msg), devinTestFrame(devinConnectEndStream, []byte(`{}`))...)
	evs := devinCollectEvents(t, stream)
	var sig string
	var inTok, outTok, cacheRead int
	for _, ev := range evs {
		switch ev.Kind {
		case "signature":
			sig = ev.Signature
		case "usage":
			inTok, outTok, cacheRead = ev.Usage.InputTokens, ev.Usage.OutputTokens, ev.Usage.CacheReadTokens
		}
	}
	if sig != "sig-abc" || inTok != 100 || outTok != 20 || cacheRead != 40 {
		t.Fatalf("sig=%q in=%d out=%d cr=%d evs=%+v", sig, inTok, outTok, cacheRead, evs)
	}
}

func TestDevinConnectStream_TrailerError(t *testing.T) {
	trailer := []byte(`{"error":{"code":"invalid_argument","message":"bad model"}}`)
	stream := devinTestFrame(devinConnectEndStream, trailer)
	evs := devinCollectEvents(t, stream)
	if len(evs) != 1 || evs[0].Kind != "error" {
		t.Fatalf("evs=%+v", evs)
	}
	if evs[0].Err == nil || !bytes.Contains([]byte(evs[0].Err.Error()), []byte("invalid_argument")) {
		t.Fatalf("err=%v", evs[0].Err)
	}
}

func TestDevinConnectStream_OversizedFrameRejected(t *testing.T) {
	var b bytes.Buffer
	b.WriteByte(0)
	var ln [4]byte
	binary.BigEndian.PutUint32(ln[:], devinMaxFramePayload+1)
	b.Write(ln[:])
	evs := devinCollectEvents(t, b.Bytes())
	if len(evs) != 1 || evs[0].Kind != "error" {
		t.Fatalf("evs=%+v", evs)
	}
}

// 同一对话历史多次转换必须产出相同消息 ID 与请求字节流前缀，
// 否则上游 EPHEMERAL prompt cache 永远无法命中（回归：此前消息 ID
// 混入每请求随机 cascadeID 导致缓存全 miss）。
func TestDevinConvertMessages_StableIDsAcrossCalls(t *testing.T) {
	mk := func() *apicompat.ChatCompletionsRequest {
		u1, _ := json.Marshal("hello")
		a1, _ := json.Marshal("hi there")
		u2, _ := json.Marshal("what is 2+2")
		return &apicompat.ChatCompletionsRequest{
			Messages: []apicompat.ChatMessage{
				{Role: "user", Content: u1},
				{Role: "assistant", Content: a1, ReasoningContent: "thinking"},
				{Role: "user", Content: u2},
			},
		}
	}
	_, msgs1 := devinConvertMessages(mk())
	_, msgs2 := devinConvertMessages(mk())
	if len(msgs1) != len(msgs2) || len(msgs1) != 3 {
		t.Fatalf("msgs=%d", len(msgs1))
	}
	for i := range msgs1 {
		if msgs1[i].ID != msgs2[i].ID {
			t.Fatalf("msg %d id unstable: %q vs %q", i, msgs1[i].ID, msgs2[i].ID)
		}
	}
	// 完整请求体（除末尾 execution_id）也应逐字节一致。
	sys1, _ := devinConvertMessages(mk())
	_, cascade := deriveDevinSessionIDs(mk(), sys1, "tok")
	traj, _ := deriveDevinSessionIDs(mk(), sys1, "tok")
	b1 := devinBuildChatRequestBody("tok", &devinConnectRequest{Messages: msgs1, Model: "swe-2-max", TrajectoryID: traj, StepIndex: 1, CascadeID: cascade})
	b2 := devinBuildChatRequestBody("tok", &devinConnectRequest{Messages: msgs2, Model: "swe-2-max", TrajectoryID: traj, StepIndex: 1, CascadeID: cascade})
	// field 22 execution_id 是尾部随机字段：前缀（到 field 22 之前）必须一致。
	if !bytes.Equal(b1[:len(b1)-50], b2[:len(b2)-50]) {
		t.Fatal("request body prefix differs across identical histories")
	}
}

func TestDeriveDevinSessionIDs_StablePerConversation(t *testing.T) {
	mk := func(extra bool) *apicompat.ChatCompletionsRequest {
		s, _ := json.Marshal("you are helpful")
		u, _ := json.Marshal("hi")
		msgs := []apicompat.ChatMessage{
			{Role: "system", Content: s},
			{Role: "user", Content: u},
		}
		if extra {
			a, _ := json.Marshal("hello!")
			msgs = append(msgs, apicompat.ChatMessage{Role: "assistant", Content: a})
		}
		return &apicompat.ChatCompletionsRequest{Messages: msgs}
	}
	cascadeOf := func(req *apicompat.ChatCompletionsRequest, token string) string {
		sys, _ := devinConvertMessages(req)
		_, cascade := deriveDevinSessionIDs(req, sys, token)
		return cascade
	}
	// 追加消息不改变 cascade（前缀一致）。
	if cascadeOf(mk(false), "tok") != cascadeOf(mk(true), "tok") {
		t.Fatal("cascade id must be stable when history only appends")
	}
	// 不同首条消息 -> 不同 cascade。
	other, _ := json.Marshal("different question")
	req2 := &apicompat.ChatCompletionsRequest{Messages: []apicompat.ChatMessage{
		{Role: "system", Content: json.RawMessage(`"you are helpful"`)},
		{Role: "user", Content: other},
	}}
	if cascadeOf(mk(false), "tok") == cascadeOf(req2, "tok") {
		t.Fatal("different conversations must not share cascade id")
	}
	// 同一会话前缀但不同账号 token -> 不同 cascade（账号级盐隔离）。
	if cascadeOf(mk(false), "tok") == cascadeOf(mk(false), "other-token") {
		t.Fatal("different accounts must not share cascade id")
	}
	// 显式会话键优先于内容哈希：压缩改写历史后轨迹仍连续。
	keyed := mk(true)
	keyed.PromptCacheKey = "thread-42"
	tra1, casc1 := deriveDevinSessionIDs(keyed, "", "tok")
	keyed.Messages = append(keyed.Messages, apicompat.ChatMessage{
		Role: "user", Content: json.RawMessage(`"follow up"`),
	})
	tra2, casc2 := deriveDevinSessionIDs(keyed, "", "tok")
	if tra1 != tra2 || casc1 != casc2 {
		t.Fatal("explicit session key must keep trajectory/cascade stable across edits")
	}
}

// devinNextStepIndex 必须在同一 trajectory 内单调递增。
func TestDevinNextStepIndex_Monotonic(t *testing.T) {
	a := devinNextStepIndex("traj-test-monotonic")
	b := devinNextStepIndex("traj-test-monotonic")
	if b != a+1 {
		t.Fatalf("step_index not monotonic: %d then %d", a, b)
	}
	if devinNextStepIndex("traj-test-other") == b {
		t.Fatal("different trajectory must have independent counter")
	}
}

// 最后一条消息必须带 prompt_cache_options 断点（field 8, type=1），
// 否则上游只缓存 system prompt 前缀、消息历史不进缓存。
func TestDevinConvertMessages_LastMsgCacheBreakpoint(t *testing.T) {
	u1, _ := json.Marshal("hello")
	a1, _ := json.Marshal("hi")
	u2, _ := json.Marshal("again")
	_, msgs := devinConvertMessages(&apicompat.ChatCompletionsRequest{Messages: []apicompat.ChatMessage{
		{Role: "user", Content: u1},
		{Role: "assistant", Content: a1},
		{Role: "user", Content: u2},
	}})
	for i, m := range msgs {
		if want := i == len(msgs)-1; m.PromptCache != want {
			t.Fatalf("msg %d PromptCache=%v want %v", i, m.PromptCache, want)
		}
	}
	// wire 层验证 field 8 存在且 type=1。
	var buf bytes.Buffer
	msgs[len(msgs)-1].marshal(&buf)
	r := &devinPVReader{buf: buf.Bytes()}
	fn, wt, ok := r.next()
	if !ok || fn != 3 || wt != 2 {
		t.Fatalf("outer field = %d wt %d", fn, wt)
	}
	inner := &devinPVReader{buf: r.bytes()}
	var sawCache bool
	for {
		f, w, ok := inner.next()
		if !ok {
			break
		}
		if f == 8 && w == 2 {
			pc := &devinPVReader{buf: inner.bytes()}
			pf, pw, _ := pc.next()
			if pf == 1 && pw == 0 && pc.varint() == 1 {
				sawCache = true
			}
			continue
		}
		inner.skip(w)
	}
	if !sawCache {
		t.Fatal("last message missing prompt_cache_options{type=EPHEMERAL}")
	}
}

// 分组结构「assistant{calls a,b} → tool a → tool b」需重排为
// call→result 紧邻；S2AX 单 prompt 多 call 时结果应紧跟其后按 id 对齐。
func TestDevinConvertMessages_ToolCallResultPairing(t *testing.T) {
	text := func(s string) json.RawMessage { b, _ := json.Marshal(s); return b }
	req := &apicompat.ChatCompletionsRequest{Messages: []apicompat.ChatMessage{
		{Role: "user", Content: text("do stuff")},
		{Role: "assistant", ToolCalls: []apicompat.ChatToolCall{
			{ID: "a", Type: "function", Function: apicompat.ChatFunctionCall{Name: "fn1", Arguments: "{}"}},
			{ID: "b", Type: "function", Function: apicompat.ChatFunctionCall{Name: "fn2", Arguments: "{}"}},
		}},
		{Role: "tool", ToolCallID: "b", Content: text("res-b")},
		{Role: "tool", ToolCallID: "a", Content: text("res-a")},
		{Role: "user", Content: text("thanks")},
	}}
	_, msgs := devinConvertMessages(req)
	// 期望顺序：user, assistant(calls), tool-b, tool-a, user
	// ——单条 assistant prompt 携带 [a,b] calls，结果块按调用顺序紧跟。
	if len(msgs) != 5 {
		t.Fatalf("msgs=%d", len(msgs))
	}
	if msgs[1].Source != 2 || len(msgs[1].ToolCalls) != 2 {
		t.Fatalf("msgs[1]=%+v", msgs[1])
	}
	if msgs[2].ToolCallID != "a" || msgs[3].ToolCallID != "b" {
		t.Fatalf("results not ordered by call sequence: %q then %q",
			msgs[2].ToolCallID, msgs[3].ToolCallID)
	}
}

// 多段 assistant call 消息 + 结果消息交叉错序时按 call id 配对。
func TestDevinPairToolCallsWithResults_Interleave(t *testing.T) {
	callMsg := func(ids ...string) devinChatMsg {
		m := devinChatMsg{Source: 2}
		for _, id := range ids {
			m.ToolCalls = append(m.ToolCalls, devinToolCall{ID: id, Name: "fn"})
		}
		return m
	}
	resMsg := func(id, text string) devinChatMsg {
		return devinChatMsg{Source: 4, ToolCallID: id, Prompt: text}
	}
	in := []devinChatMsg{
		{Source: 1, Prompt: "u"},
		callMsg("a"),
		callMsg("b"),
		resMsg("b", "rb"),
		resMsg("a", "ra"),
		{Source: 1, Prompt: "u2"},
	}
	out := devinPairToolCallsWithResults(in)
	got := []string{}
	for _, m := range out {
		switch {
		case m.Source == 4:
			got = append(got, "R:"+m.ToolCallID)
		case len(m.ToolCalls) > 0:
			got = append(got, "C:"+m.ToolCalls[0].ID)
		default:
			got = append(got, "U:"+m.Prompt)
		}
	}
	want := []string{"U:u", "C:a", "R:a", "C:b", "R:b", "U:u2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order=%v want %v", got, want)
		}
	}
}

// 无配对 tool call 的孤立结果降级为 USER 文本，不进 TOOL 通道。
func TestDevinConvertMessages_OrphanToolResultDemoted(t *testing.T) {
	text := func(s string) json.RawMessage { b, _ := json.Marshal(s); return b }
	_, msgs := devinConvertMessages(&apicompat.ChatCompletionsRequest{Messages: []apicompat.ChatMessage{
		{Role: "user", Content: text("hi")},
		{Role: "tool", ToolCallID: "missing-call", Content: text("partial output")},
	}})
	if len(msgs) != 2 {
		t.Fatalf("msgs=%d", len(msgs))
	}
	if msgs[1].Source != 1 || !strings.Contains(msgs[1].Prompt, "partial output") {
		t.Fatalf("orphan result not demoted: %+v", msgs[1])
	}
}

// 历史图片降级文本占位，当前轮图片保留 Images。
func TestDevinConvertMessages_HistoryImagesDemoted(t *testing.T) {
	imgPart := `[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]`
	uimg := json.RawMessage(imgPart)
	a1, _ := json.Marshal("seen")
	u2, _ := json.Marshal("next")
	_, msgs := devinConvertMessages(&apicompat.ChatCompletionsRequest{Messages: []apicompat.ChatMessage{
		{Role: "user", Content: uimg}, // 历史轮图片
		{Role: "assistant", Content: a1},
		{Role: "user", Content: uimg}, // 当前轮图片（最后 assistant 之后）
		{Role: "user", Content: u2},
	}})
	if len(msgs) != 4 {
		t.Fatalf("msgs=%d", len(msgs))
	}
	if len(msgs[0].Images) != 0 || !strings.Contains(msgs[0].Prompt, "[Image omitted from history]") {
		t.Fatalf("history image not demoted: %+v", msgs[0])
	}
	if len(msgs[2].Images) != 1 || msgs[2].Images[0].Base64 != "AAAA" {
		t.Fatalf("current-turn image lost: %+v", msgs[2])
	}
}

// 空 assistant（无文本无调用）跳过；空 tool result 补占位文本。
func TestDevinConvertMessages_EmptyAssistantSkipped(t *testing.T) {
	u, _ := json.Marshal("hi")
	_, msgs := devinConvertMessages(&apicompat.ChatCompletionsRequest{Messages: []apicompat.ChatMessage{
		{Role: "user", Content: u},
		{Role: "assistant"},             // 完全空
		{Role: "tool", ToolCallID: "x"}, // 与 user 相邻的孤立结果→降级
		{Role: "user", Content: u},
	}})
	for i, m := range msgs {
		if m.Source == 2 && m.Prompt == "" && len(m.ToolCalls) == 0 {
			t.Fatalf("empty assistant kept at %d", i)
		}
	}
}

// signature/signature_type/output_id/thinking_redacted 请求侧编码。
func TestDevinChatMsgMarshal_SignatureFields(t *testing.T) {
	m := devinChatMsg{
		ID: "id-1", Source: 2, Prompt: "text",
		Signature: "sig", SignatureType: "sig-type", OutputID: "out-1",
		Thinking: "think", ThinkingRedacted: true,
	}
	var buf bytes.Buffer
	m.marshal(&buf)
	r := &devinPVReader{buf: buf.Bytes()}
	fn, _, _ := r.next()
	if fn != 3 {
		t.Fatalf("outer field=%d", fn)
	}
	inner := &devinPVReader{buf: r.bytes()}
	fields := map[int]string{}
	var redacted bool
	for {
		f, w, ok := inner.next()
		if !ok {
			break
		}
		if w == 2 {
			fields[f] = inner.str()
		} else if w == 0 {
			if f == 13 {
				redacted = inner.varint() == 1
			} else {
				inner.skip(w)
			}
		} else {
			inner.skip(w)
		}
	}
	if fields[12] != "sig" || fields[15] != "out-1" || fields[18] != "sig-type" || !redacted {
		t.Fatalf("fields=%v redacted=%v", fields, redacted)
	}
}

// 响应侧 output_id / delta_signature_type 解析。
func TestDevinConnectStream_OutputIDAndSignatureType(t *testing.T) {
	msg := devinTestChatMsg(func(b *bytes.Buffer) {
		devinPVStr(b, 15, "output-xyz")
		devinPVStr(b, 21, "st-v2")
	})
	stream := append(devinTestFrame(0, msg), devinTestFrame(devinConnectEndStream, []byte(`{}`))...)
	var outputID, sigType string
	for _, ev := range devinCollectEvents(t, stream) {
		if ev.Kind == "output_id" {
			outputID = ev.OutputID
		}
		if ev.Kind == "signature_type" {
			sigType = ev.SignatureType
		}
	}
	if outputID != "output-xyz" || sigType != "st-v2" {
		t.Fatalf("outputID=%q sigType=%q", outputID, sigType)
	}
}

// named tool_choice 走 tool_name oneof（field 2），option 走 field 1。
func TestDevinToolChoiceOption(t *testing.T) {
	opt, tn := devinToolChoiceOption(json.RawMessage(`"required"`))
	if opt != "required" || tn != "" {
		t.Fatalf("opt=%q tn=%q", opt, tn)
	}
	opt, tn = devinToolChoiceOption(json.RawMessage(`{"type":"function","function":{"name":"my_tool"}}`))
	if opt != "" || tn != "my_tool" {
		t.Fatalf("named: opt=%q tn=%q", opt, tn)
	}
}
