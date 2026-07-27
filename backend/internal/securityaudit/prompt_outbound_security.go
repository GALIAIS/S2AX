package securityaudit

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const maxGuardResponseBytes int64 = 256 * 1024

type NetworkScope string

const (
	NetworkScopePublicHTTPS NetworkScope = "public_https"
	NetworkScopeTrusted     NetworkScope = "trusted_network"
	NetworkScopeLoopback    NetworkScope = "loopback"
)

var blockedAuditDestinationPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", infraerrors.BadRequest("prompt_audit_invalid_base_url", "审计节点地址无效")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", infraerrors.BadRequest("prompt_audit_invalid_base_url_scheme", "审计节点仅支持 HTTP(S)")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", infraerrors.BadRequest("prompt_audit_unsafe_base_url", "审计节点地址不能包含凭据、查询参数或片段")
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", infraerrors.BadRequest("prompt_audit_invalid_base_url", "审计节点地址无效")
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	if strings.EqualFold(path, "/v1") {
		path = ""
	}
	parsed.Path = path
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func InferNetworkScope(baseURL string) NetworkScope {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return NetworkScopePublicHTTPS
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return NetworkScopeLoopback
	}
	if ip := net.ParseIP(host); ip != nil {
		switch {
		case ip.IsLoopback():
			return NetworkScopeLoopback
		case ip.IsPrivate():
			return NetworkScopeTrusted
		}
	}
	if strings.EqualFold(parsed.Scheme, "http") {
		return NetworkScopeTrusted
	}
	return NetworkScopePublicHTTPS
}

func NormalizeNetworkScope(scope NetworkScope, baseURL string) NetworkScope {
	switch NetworkScope(strings.TrimSpace(string(scope))) {
	case NetworkScopePublicHTTPS, NetworkScopeTrusted, NetworkScopeLoopback:
		return NetworkScope(strings.TrimSpace(string(scope)))
	default:
		return InferNetworkScope(baseURL)
	}
}

func ValidateEndpointNetworkPolicy(baseURL string, scope NetworkScope) error {
	normalized, err := NormalizeBaseURL(baseURL)
	if err != nil {
		return err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return infraerrors.BadRequest("prompt_audit_invalid_base_url", "审计节点地址无效")
	}
	scope = NormalizeNetworkScope(scope, normalized)
	if scope == NetworkScopePublicHTTPS && parsed.Scheme != "https" {
		return infraerrors.BadRequest("prompt_audit_https_required", "公网审计节点必须使用 HTTPS")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		if scope != NetworkScopeLoopback {
			return infraerrors.BadRequest("prompt_audit_network_scope_mismatch", "本机审计节点必须使用回环网络范围")
		}
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && !networkScopeAllowsIP(scope, ip) {
		return infraerrors.BadRequest("prompt_audit_destination_blocked", "审计节点地址不属于所选网络范围")
	}
	return nil
}

func ChatCompletionsURL(base string) (string, error) {
	normalized, err := NormalizeBaseURL(base)
	if err != nil {
		return "", err
	}
	return normalized + "/v1/chat/completions", nil
}

func ModerationsURL(base string) (string, error) {
	normalized, err := NormalizeBaseURL(base)
	if err != nil {
		return "", err
	}
	return normalized + "/v1/moderations", nil
}

func ModelsURL(base string) (string, error) {
	normalized, err := NormalizeBaseURL(base)
	if err != nil {
		return "", err
	}
	return normalized + "/v1/models", nil
}

func NewSecureHTTPClient(endpoint ActiveEndpoint) (*http.Client, error) {
	normalized, err := NormalizeBaseURL(endpoint.BaseURL)
	if err != nil {
		return nil, err
	}
	scope := NormalizeNetworkScope(endpoint.NetworkScope, normalized)
	if err := ValidateEndpointNetworkPolicy(normalized, scope); err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		// Do not inherit HTTP(S)_PROXY. A proxy would move the actual destination
		// dial outside secureDialContext and bypass this module's DNS/IP validation.
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: time.Duration(endpoint.TimeoutMS) * time.Millisecond,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	transport.DialContext = secureAuditDialContext(dialer, scope)
	timeout := time.Duration(endpoint.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = DefaultTimeoutMS * time.Millisecond
	}
	base, _ := url.Parse(normalized)
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("prompt audit redirect limit exceeded")
			}
			if !sameAuditOrigin(base, req.URL) {
				return errors.New("prompt audit cross-origin redirect blocked")
			}
			return ValidateEndpointNetworkPolicy(req.URL.String(), scope)
		},
	}, nil
}

func secureAuditDialContext(dialer *net.Dialer, scope NetworkScope) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("prompt audit destination is invalid")
		}
		ips := make([]net.IP, 0, 4)
		if literal := net.ParseIP(host); literal != nil {
			ips = append(ips, literal)
		} else {
			resolved, resolveErr := net.DefaultResolver.LookupIPAddr(ctx, host)
			if resolveErr != nil {
				return nil, fmt.Errorf("prompt audit destination lookup failed")
			}
			for _, item := range resolved {
				ips = append(ips, item.IP)
			}
		}
		for _, ip := range ips {
			if !networkScopeAllowsIP(scope, ip) {
				continue
			}
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, fmt.Errorf("prompt audit destination is blocked or unavailable")
	}
}

func networkScopeAllowsIP(scope NetworkScope, ip net.IP) bool {
	if ip == nil {
		return false
	}
	if scope == NetworkScopeLoopback {
		return ip.IsLoopback()
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, prefix := range blockedAuditDestinationPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	if scope == NetworkScopePublicHTTPS {
		return !addr.IsPrivate()
	}
	return scope == NetworkScopeTrusted
}

func sameAuditOrigin(base, target *url.URL) bool {
	if base == nil || target == nil || !strings.EqualFold(base.Scheme, target.Scheme) || !strings.EqualFold(base.Hostname(), target.Hostname()) {
		return false
	}
	return effectivePort(base) == effectivePort(target)
}

func effectivePort(value *url.URL) string {
	if value.Port() != "" {
		return value.Port()
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	return "80"
}
