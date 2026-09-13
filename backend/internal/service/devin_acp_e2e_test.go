package service

// 真实云端联调用例：设置 DEVIN_E2E_TOKEN（devin-session-token$... 原文）后
//   go test ./internal/service -run TestDevinACP_E2E -v
// 可选环境变量：DEVIN_E2E_ORG（org-...）、DEVIN_E2E_MODEL（默认 devin-swe-2-max）。

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDevinACP_E2E(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("DEVIN_E2E_TOKEN"))
	if token == "" {
		t.Skip("DEVIN_E2E_TOKEN not set")
	}
	account := &Account{
		ID:       1,
		Platform: PlatformDevin,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"session_token": token,
			"org_id":        os.Getenv("DEVIN_E2E_ORG"),
		},
	}
	model := os.Getenv("DEVIN_E2E_MODEL")
	if model == "" {
		model = "devin-swe-2-max"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	got, err := resolveDevinSessionToken(ctx, account, "")
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	cl, err := devinDialACP(ctx, account, got, "")
	if err != nil {
		var fo *UpstreamFailoverError
		if errors.As(err, &fo) {
			t.Fatalf("dial+initialize: %v status=%d body=%s", err, fo.StatusCode, string(fo.ResponseBody))
		}
		t.Fatalf("dial+initialize: %v", err)
	}
	defer cl.Close()

	sessionID, err := cl.devinNewSession(ctx, "/")
	if err != nil {
		t.Fatalf("session/new: %v", err)
	}
	t.Logf("sessionId=%s", sessionID)
	defer func() { go cl.devinCloseSession(sessionID) }()

	if err := cl.devinSetConfig(ctx, sessionID, DevinConfigIDVersion, model); err != nil {
		t.Fatalf("set devin_version=%s: %v", model, err)
	}
	if org := account.DevinOrgID(); org != "" {
		if err := cl.devinSetConfig(ctx, sessionID, DevinConfigIDOrg, org); err != nil {
			t.Fatalf("set org_id=%s: %v", org, err)
		}
	}

	var streamed strings.Builder
	var usages []string
	cl.onEvent = func(sid string, ev devinACPEvent) {
		switch ev.Kind {
		case "message":
			streamed.WriteString(ev.Text)
		case "usage":
			if ev.Usage != nil {
				usages = append(usages, string(ev.Usage.Raw))
			}
		}
	}

	// 用原始 call 发 prompt，打印完整 JSON 响应以确认 usage 字段真实结构
	raw, err := cl.call(ctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []map[string]any{{"type": "text", "text": "Say exactly: PONG"}},
	})
	if err != nil {
		t.Fatalf("session/prompt: %v", err)
	}
	t.Logf("raw result: %s", string(raw))
	turn := &devinACPTurnResult{StopReason: "end_turn", SessionID: sessionID}
	if err != nil {
		t.Fatalf("session/prompt: %v", err)
	}
	for i, u := range usages {
		t.Logf("usage_update[%d]: %s", i, u)
	}
	t.Logf("text=%q", streamed.String())
	if turn.StopReason == "" {
		t.Fatal("empty stopReason")
	}
	if !strings.Contains(strings.ToUpper(streamed.String()), "PONG") {
		t.Fatalf("expected PONG in reply, got %q", streamed.String())
	}
}
