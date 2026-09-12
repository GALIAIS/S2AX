package service

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

// TestAccountTLSFingerprintDefaultsToCodexForOAuth 锁定 OpenAI OAuth-like 账号的默认模板选择，
// 同时保留显式关闭开关和 API Key 不启用指纹的边界。
func TestAccountTLSFingerprintDefaultsToCodexForOAuth(t *testing.T) {
	tests := []struct {
		name        string
		accountType string
		extra       map[string]any
		wantEnabled bool
	}{
		{name: "oauth default", accountType: AccountTypeOAuth, wantEnabled: true},
		{name: "setup token default", accountType: AccountTypeSetupToken, wantEnabled: true},
		{name: "oauth explicit disable", accountType: AccountTypeOAuth, extra: map[string]any{"enable_tls_fingerprint": false}, wantEnabled: false},
		{name: "api key", accountType: AccountTypeAPIKey, wantEnabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: tt.accountType, Extra: tt.extra}
			if got := account.IsTLSFingerprintEnabled(); got != tt.wantEnabled {
				t.Fatalf("IsTLSFingerprintEnabled() = %v, want %v", got, tt.wantEnabled)
			}
		})
	}
}

// TestOpenAIUpstreamUsesCodexTLSProfile 锁定真实 OpenAI 网关出口调用 DoWithTLS，
// 防止新增的 Codex 模板只存在于配置层却没有进入请求发送链路。
func TestOpenAIUpstreamUsesCodexTLSProfile(t *testing.T) {
	upstream := &recordingTLSUpstream{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     make(http.Header),
		},
	}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	req, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/backend-api/codex/models", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	resp, err := svc.doOpenAIUpstream(req, "", account)
	if err != nil {
		t.Fatalf("doOpenAIUpstream: %v", err)
	}
	if resp != upstream.response {
		t.Fatal("gateway must return the upstream response")
	}
	if upstream.doWithTLSCalls != 1 {
		t.Fatalf("DoWithTLS calls = %d, want 1", upstream.doWithTLSCalls)
	}
	if upstream.profile == nil || upstream.profile.Name != "Codex CLI rustls 0.23" {
		t.Fatalf("TLS profile = %#v, want Codex CLI rustls profile", upstream.profile)
	}
}

// TestResolveTLSFingerprintProfileHonorsExplicitDisable 锁定显式关闭不会被内置默认覆盖。
func TestResolveTLSFingerprintProfileHonorsExplicitDisable(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"enable_tls_fingerprint": false},
	}
	if profile := resolveTLSFingerprintProfile(nil, account); profile != nil {
		t.Fatalf("explicitly disabled TLS profile = %#v, want nil", profile)
	}
}

type recordingTLSUpstream struct {
	response       *http.Response
	profile        *tlsfingerprint.Profile
	doWithTLSCalls int
}

func (u *recordingTLSUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return u.response, nil
}

func (u *recordingTLSUpstream) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.profile = profile
	u.doWithTLSCalls++
	return u.response, nil
}
