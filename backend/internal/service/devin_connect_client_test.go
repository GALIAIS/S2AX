package service

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"io"
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
	b1 := devinBuildChatRequestBody("tok", &devinConnectRequest{Messages: msgs1, Model: "swe-2-max", CascadeID: deriveDevinCascadeID(mk())})
	b2 := devinBuildChatRequestBody("tok", &devinConnectRequest{Messages: msgs2, Model: "swe-2-max", CascadeID: deriveDevinCascadeID(mk())})
	// field 22 execution_id 是尾部随机字段：前缀（到 field 22 之前）必须一致。
	if !bytes.Equal(b1[:len(b1)-50], b2[:len(b2)-50]) {
		t.Fatal("request body prefix differs across identical histories")
	}
}

func TestDeriveDevinCascadeID_StablePerConversation(t *testing.T) {
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
	// 追加消息不改变 cascade（前缀一致）。
	if deriveDevinCascadeID(mk(false)) != deriveDevinCascadeID(mk(true)) {
		t.Fatal("cascade id must be stable when history only appends")
	}
	// 不同首条消息 -> 不同 cascade。
	other, _ := json.Marshal("different question")
	req2 := &apicompat.ChatCompletionsRequest{Messages: []apicompat.ChatMessage{
		{Role: "system", Content: json.RawMessage(`"you are helpful"`)},
		{Role: "user", Content: other},
	}}
	if deriveDevinCascadeID(mk(false)) == deriveDevinCascadeID(req2) {
		t.Fatal("different conversations must not share cascade id")
	}
}
