package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDetectCodexTerminalUserAgent 对齐 codex-terminal-detection 的环境变量优先级，
// 防止默认 UA 因探测顺序变化而偏离官方 CLI。
func TestDetectCodexTerminalUserAgent(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		set  map[string]bool
		want string
	}{
		{name: "term program with version", env: map[string]string{
			"TERM_PROGRAM":         "iTerm.app",
			"TERM_PROGRAM_VERSION": "3.5.0",
		}, want: "iTerm.app/3.5.0"},
		{name: "tmux falls through to TERM", env: map[string]string{
			"TERM_PROGRAM": "tmux",
			"TERM":         "screen-256color",
		}, want: "screen-256color"},
		{name: "wezterm presence with empty version", set: map[string]bool{
			"WEZTERM_VERSION": true,
		}, want: "WezTerm"},
		{name: "kitty capability", env: map[string]string{
			"TERM": "xterm-kitty",
		}, want: "kitty"},
		{name: "missing environment", want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				value, ok := tt.env[key]
				if ok {
					return value, true
				}
				if tt.set[key] {
					return "", true
				}
				return "", false
			}
			require.Equal(t, tt.want, detectCodexTerminalUserAgent(lookup))
		})
	}
}

// TestCodexSystemIdentityFormatting 锁定 os_info 风格的系统名与架构映射。
func TestCodexSystemIdentityFormatting(t *testing.T) {
	require.Equal(t, "Ubuntu", codexOSDisplayName("ubuntu"))
	require.Equal(t, "Mac OS", codexOSDisplayName("darwin"))
	require.Equal(t, "Windows", codexOSDisplayName("windows"))
	require.Equal(t, "x86_64", codexArchitecture("amd64"))
	require.Equal(t, "arm64", codexArchitecture("aarch64"))
}

// TestDefaultCodexUserAgentUsesGeneratedSuffix 确保默认身份的后缀确实来自源码探测逻辑，
// 而不是另外维护一份与 codex-rs 格式脱节的固定 UA。
func TestDefaultCodexUserAgentUsesGeneratedSuffix(t *testing.T) {
	require.NotEmpty(t, codexCLIUserAgentSuffix)
	require.Equal(t,
		"codex_cli_rs/"+codexCLIVersion+codexCLIUserAgentSuffix,
		codexCLIUserAgent,
	)
}
