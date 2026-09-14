package service

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"testing"
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
