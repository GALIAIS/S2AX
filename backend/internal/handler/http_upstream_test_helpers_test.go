package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

// OpenAI 失败转发测试使用 Do 记录账号与响应序列，TLS 参数对其断言没有影响。
func (u *openAIHTTPPassthroughFailoverUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

// OpenAI 鉴权失败测试沿用普通响应队列，确保 OAuth 出站新接口仍可复用该替身。
func (u *openAIHTTPPassthroughAuthFailoverUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

// SSE 限流测试只验证重试和流内容，不需要重复实现 TLS 行为。
func (u *openAIHTTPPassthroughSSERateLimitUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}
