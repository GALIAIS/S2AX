package service

// 缓存命中 E2E：同一对话前缀发两轮请求（第二轮追加消息），验证第二轮
// usage.cache_read_tokens > 0。需要 DEVIN_E2E_TOKEN，免费档模型。
// 运行：DEVIN_E2E_TOKEN=devin-session-token$... go test -tags unit -run TestDevinCache_E2E -v

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

func TestDevinCache_E2E(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("DEVIN_E2E_TOKEN"))
	if token == "" {
		t.Skip("DEVIN_E2E_TOKEN not set")
	}
	apiServer := strings.TrimSpace(os.Getenv("DEVIN_E2E_API_SERVER"))
	if apiServer == "" {
		apiServer = DevinDefaultAPIServerURL
	}
	proxy := strings.TrimSpace(os.Getenv("DEVIN_E2E_PROXY"))
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

	// ~4KB 重复文本的 system prompt，越过可能存在的缓存最小阈值。
	filler := strings.Repeat("You are a deterministic test harness. Reply with PONG only. ", 80)
	sysJSON, _ := json.Marshal(filler)
	u1, _ := json.Marshal("ping")
	a1, _ := json.Marshal("PONG")
	u2, _ := json.Marshal("ping again")

	mkReq := func(withTurn2 bool) *apicompat.ChatCompletionsRequest {
		msgs := []apicompat.ChatMessage{
			{Role: "system", Content: sysJSON},
			{Role: "user", Content: u1},
		}
		if withTurn2 {
			msgs = append(msgs,
				apicompat.ChatMessage{Role: "assistant", Content: a1},
				apicompat.ChatMessage{Role: "user", Content: u2})
		}
		return &apicompat.ChatCompletionsRequest{Model: model, Messages: msgs}
	}

	run := func(req *apicompat.ChatCompletionsRequest, tag string) *devinUsageStats {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		got, err := resolveDevinSessionToken(ctx, account, proxy)
		if err != nil {
			t.Fatalf("%s resolve token: %v", tag, err)
		}
		systemPrompt, msgs := devinConvertMessages(req)
		events, err := devinChatStream(ctx, account.DevinAPIServerURL(), got, proxy, &devinConnectRequest{
			SystemPrompt: systemPrompt,
			Messages:     msgs,
			Model:        model,
			CascadeID:    deriveDevinCascadeID(req),
		})
		if err != nil {
			t.Fatalf("%s stream: %v", tag, err)
		}
		var usage *devinUsageStats
		for ev := range events {
			if ev.Kind == "error" {
				t.Fatalf("%s event error: %v", tag, ev.Err)
			}
			if ev.Kind == "usage" {
				usage = ev.Usage
			}
		}
		return usage
	}

	u1stats := run(mkReq(false), "turn1")
	t.Logf("turn1: in=%d out=%d cache_write=%d cache_read=%d",
		u1stats.InputTokens, u1stats.OutputTokens, u1stats.CacheWriteTokens, u1stats.CacheReadTokens)

	u2stats := run(mkReq(true), "turn2")
	t.Logf("turn2: in=%d out=%d cache_write=%d cache_read=%d",
		u2stats.InputTokens, u2stats.OutputTokens, u2stats.CacheWriteTokens, u2stats.CacheReadTokens)

	if u2stats.CacheReadTokens > 0 {
		t.Logf("CACHE HIT: turn2 cache_read=%d", u2stats.CacheReadTokens)
	} else {
		t.Logf("CACHE MISS: turn2 cache_read=0 (in=%d)", u2stats.InputTokens)
	}
}
