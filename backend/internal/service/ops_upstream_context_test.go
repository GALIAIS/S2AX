package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSafeUpstreamURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"strips query", "https://api.anthropic.com/v1/messages?beta=true", "https://api.anthropic.com/v1/messages"},
		{"strips fragment", "https://api.openai.com/v1/responses#frag", "https://api.openai.com/v1/responses"},
		{"strips both", "https://host/path?token=secret#x", "https://host/path"},
		{"no query or fragment", "https://host/path", "https://host/path"},
		{"empty string", "", ""},
		{"whitespace only", "  ", ""},
		{"query before fragment", "https://h/p?a=1#f", "https://h/p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, safeUpstreamURL(tt.input))
		})
	}
}

func TestSetOpsLatencyMsPersistsForAsyncUsageRecording(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)

	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, 123)

	value, ok := OpsLatencyMsFromContext(c.Request.Context(), OpsUpstreamLatencyMsKey)
	require.True(t, ok)
	require.EqualValues(t, 123, value)

	copyCtx := CopyOpsLatencyContext(c.Request.Context(), context.Background())
	copied, ok := OpsLatencyMsFromContext(copyCtx, OpsUpstreamLatencyMsKey)
	require.True(t, ok)
	require.EqualValues(t, 123, copied)
}
