package service

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const openAIResponsesLiteDisabledContextKey = "openai_responses_lite_disabled"

// isOpenAIResponsesLiteDisabled reports whether the current request has been
// downgraded to the regular Responses contract after the upstream explicitly
// rejected its Lite model/header combination.
func isOpenAIResponsesLiteDisabled(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, exists := c.Get(openAIResponsesLiteDisabledContextKey)
	disabled, _ := value.(bool)
	return exists && disabled
}

func markOpenAIResponsesLiteDisabled(c *gin.Context) {
	if c != nil {
		c.Set(openAIResponsesLiteDisabledContextKey, true)
	}
}

// isOpenAIResponsesLiteUnsupportedModelResponse identifies the deterministic
// ChatGPT error returned when a model cannot be used with the private Lite
// header. The response is safe to retry once without that header; unrelated
// unsupported_value errors must keep their normal error/failover behavior.
func isOpenAIResponsesLiteUnsupportedModelResponse(statusCode int, body []byte) bool {
	if statusCode != http.StatusBadRequest || len(body) == 0 {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	return strings.Contains(message, "not supported when using x-openai-internal-codex-responses-lite")
}
