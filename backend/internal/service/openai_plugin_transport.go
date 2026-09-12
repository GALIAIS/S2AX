package service

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// SetTLSFingerprintProfileService 注入 TLS 模板服务。
// 采用 setter 保持 OpenAIGatewayService 既有构造函数兼容现有测试和扩展调用方。
func (s *OpenAIGatewayService) SetTLSFingerprintProfileService(profileService *TLSFingerprintProfileService) {
	if s != nil {
		s.tlsFPProfileService = profileService
	}
}

// resolveTLSFingerprintProfile 复用 OpenAI 网关与账号测试的 TLS 选择规则。
// 账号测试服务可能独立于网关构造，因此不能依赖网关 receiver 才能解析模板。
func resolveTLSFingerprintProfile(profileService *TLSFingerprintProfileService, account *Account) *tlsfingerprint.Profile {
	if profileService != nil {
		return profileService.ResolveTLSProfile(account)
	}
	if account != nil && account.IsOpenAIOAuthLike() && account.IsTLSFingerprintEnabled() {
		return tlsfingerprint.CodexRustlsProfile()
	}
	return nil
}

// resolveTLSProfile 解析 OpenAI 出站请求使用的 TLS 模板。
// 生产环境优先使用数据库模板；服务未装配时仍回退到内置 Codex 模板，
// 避免测试或特殊装配路径把 OpenAI OAuth 请求退回普通 Go TLS。
func (s *OpenAIGatewayService) resolveTLSProfile(account *Account) *tlsfingerprint.Profile {
	if s == nil {
		return resolveTLSFingerprintProfile(nil, account)
	}
	return resolveTLSFingerprintProfile(s.tlsFPProfileService, account)
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	return s.httpUpstream.DoWithTLS(
		request,
		proxyURL,
		account.ID,
		account.Concurrency,
		s.resolveTLSProfile(account),
	)
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(
	request *http.Request,
	proxyURL string,
	account *Account,
	useTLSFallback bool,
) (*http.Response, error) {
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	if useTLSFallback {
		return s.httpUpstream.DoWithTLS(
			request,
			proxyURL,
			account.ID,
			account.Concurrency,
			resolveTLSFingerprintProfile(s.tlsFPProfileService, account),
		)
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}
