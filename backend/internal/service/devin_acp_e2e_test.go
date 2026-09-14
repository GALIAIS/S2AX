package service

// 真实联调用例（direct Connect-RPC，不经 devin.exe / app.devin.ai）：
//   设置 DEVIN_E2E_TOKEN（devin-session-token$... 原文或可换发的 api_key）后
//   go test ./internal/service -run TestDevinConnect_E2E -v
// 可选环境变量：DEVIN_E2E_MODEL（默认 glm-5-2）、DEVIN_E2E_API_SERVER。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

func TestDevinConnect_E2E(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("DEVIN_E2E_TOKEN"))
	if token == "" {
		t.Skip("DEVIN_E2E_TOKEN not set")
	}
	apiServer := strings.TrimSpace(os.Getenv("DEVIN_E2E_API_SERVER"))
	if apiServer == "" {
		apiServer = DevinDefaultAPIServerURL
	}
	account := &Account{
		ID:       1,
		Platform: PlatformDevin,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"session_token":  token,
			"api_server_url": apiServer,
		},
	}
	model := os.Getenv("DEVIN_E2E_MODEL")
	if model == "" {
		model = "glm-5-2"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	got, err := resolveDevinSessionToken(ctx, account, "")
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}

	contentJSON, _ := json.Marshal("Say exactly: PONG")
	chatReq := &apicompat.ChatCompletionsRequest{
		Model: model,
		Messages: []apicompat.ChatMessage{{
			Role:    "user",
			Content: contentJSON,
		}},
	}
	sysPrompt := "You are a helpful assistant."
	_, msgs := devinConvertMessages(chatReq)
	trajectoryID, cascadeID := deriveDevinSessionIDs(chatReq, sysPrompt, got)
	upReq := &devinConnectRequest{
		SystemPrompt: sysPrompt,
		Messages:     msgs,
		Model:        normalizeDevinLocalModel(model),
		TrajectoryID: trajectoryID,
		StepIndex:    devinNextStepIndex(trajectoryID),
		CascadeID:    cascadeID,
	}

	events, err := devinChatStream(ctx, account.DevinAPIServerURL(), got, "", upReq)
	if err != nil {
		var fo *UpstreamFailoverError
		if errors.As(err, &fo) {
			t.Fatalf("GetChatMessage: %v status=%d body=%s", err, fo.StatusCode, string(fo.ResponseBody))
		}
		t.Fatalf("GetChatMessage: %v", err)
	}

	var streamed strings.Builder
	var usage *devinUsageStats
	stopReason := -1
	done := false
	for ev := range events {
		switch ev.Kind {
		case "error":
			t.Fatalf("stream error: %v", ev.Err)
		case "text":
			streamed.WriteString(ev.Text)
		case "thinking":
			t.Logf("thinking: %q", ev.Text)
		case "toolcall":
			t.Logf("toolcall: %+v", ev.ToolCall)
		case "usage":
			usage = ev.Usage
		case "stop":
			stopReason = ev.StopReason
		case "done":
			done = true
		}
	}
	if !done {
		t.Fatal("stream ended without done frame")
	}
	t.Logf("stopReason=%d usage=%+v text=%q", stopReason, usage, streamed.String())
	if !strings.Contains(strings.ToUpper(streamed.String()), "PONG") {
		t.Fatalf("expected PONG in reply, got %q", streamed.String())
	}
}
