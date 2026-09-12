//go:build unit

package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

// unit 专用测试上游只关心请求结果和账号切换，不验证 TLS ClientHello；统一委托 Do，
// 避免嵌入的 HTTPUpstream 接口在 DoWithTLS 调用时落到 nil 方法。
func (u *grokCredentialHandlerUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

// 图片失败转发测试复用 Do 的账号记录和响应状态。
func (u *openAIImagesFailoverHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

// Responses 取消/故障转移测试只验证取消传播和账号选择。
func (u *openAIResponsesFailoverCancelUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

// Astra Pro 测试已经在 Do 中记录请求，TLS profile 无需参与断言。
func (u *astraProCapturedUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}
