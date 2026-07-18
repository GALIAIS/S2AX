package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsOpenAIResponsesLiteUnsupportedModelResponse(t *testing.T) {
	body := []byte(`{"error":{"type":"invalid_request_error","code":"unsupported_value","message":"This model is not supported when using X-OpenAI-Internal-Codex-Responses-Lite.","param":"model"}}`)

	require.True(t, isOpenAIResponsesLiteUnsupportedModelResponse(http.StatusBadRequest, body))
	require.False(t, isOpenAIResponsesLiteUnsupportedModelResponse(http.StatusInternalServerError, body))
	require.False(t, isOpenAIResponsesLiteUnsupportedModelResponse(http.StatusBadRequest, []byte(`{"error":{"message":"model is not supported"}}`)))
}
