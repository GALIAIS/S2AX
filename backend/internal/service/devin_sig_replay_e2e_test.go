package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

// 签名回放 E2E：turn1 收真实 signature，turn2 把它随 assistant 历史回传，
// 上游接受（不 invalid_argument）即证明 signature/signature_type 回路成立。
func TestDevinSignatureReplay_E2E(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("DEVIN_E2E_TOKEN"))
	if token == "" {
		t.Skip("DEVIN_E2E_TOKEN not set")
	}
	model := os.Getenv("DEVIN_E2E_MODEL")
	if model == "" {
		model = "swe-2-max"
	}
	apiServer := DevinDefaultAPIServerURL

	run := func(msgs []apicompat.ChatMessage) (sig, sigType, text, thinking string) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		req := &apicompat.ChatCompletionsRequest{Model: model, Messages: msgs}
		systemPrompt, dmsgs := devinConvertMessages(req)
		traj, casc := deriveDevinSessionIDs(req, systemPrompt, token)
		events, err := devinChatStream(ctx, apiServer, token, "", &devinConnectRequest{
			SystemPrompt: systemPrompt, Messages: dmsgs, Model: model,
			TrajectoryID: traj, StepIndex: devinNextStepIndex(traj), CascadeID: casc,
		})
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		var tb, th strings.Builder
		for ev := range events {
			switch ev.Kind {
			case "signature":
				sig += ev.Signature
			case "signature_type":
				sigType = ev.SignatureType
			case "text":
				tb.WriteString(ev.Text)
			case "thinking":
				th.WriteString(ev.Text)
			case "error":
				t.Fatalf("event error: %v", ev.Err)
			}
		}
		return sig, sigType, tb.String(), th.String()
	}

	u1, _ := json.Marshal("Think briefly then say PONG.")
	sig1, st1, txt1, think1 := run([]apicompat.ChatMessage{{Role: "user", Content: u1}})
	t.Logf("turn1: sig=%q(%d) type=%q text=%q thinking=%dB", trunc(sig1, 50), len(sig1), st1, txt1, len(think1))

	u2, _ := json.Marshal("Say it again.")
	assistant := apicompat.ChatMessage{
		Role: "assistant",
		Content: func() json.RawMessage { b, _ := json.Marshal(txt1); return b }(),
		ReasoningContent: think1,
		Signature:        sig1,
		SignatureType:    st1,
	}
	_, _, txt2, _ := run([]apicompat.ChatMessage{
		{Role: "user", Content: u1}, assistant, {Role: "user", Content: u2},
	})
	t.Logf("turn2 text=%q (signature replay accepted)", txt2)
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
