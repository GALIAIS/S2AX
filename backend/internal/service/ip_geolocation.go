package service

import (
	"log/slog"
	"net/netip"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	ip2regionservice "github.com/lionsoul2014/ip2region/binding/golang/service"
)

type IPGeolocationStatus string

const (
	IPGeolocationStatusSuccess     IPGeolocationStatus = "success"
	IPGeolocationStatusPrivate     IPGeolocationStatus = "private"
	IPGeolocationStatusInvalid     IPGeolocationStatus = "invalid"
	IPGeolocationStatusUnavailable IPGeolocationStatus = "unavailable"
	IPGeolocationStatusNotFound    IPGeolocationStatus = "not_found"
)

// IPGeolocationResult is intentionally a small display DTO. It never carries
// raw provider responses, which can change format and should not be exposed.
type IPGeolocationResult struct {
	IP           string              `json:"ip"`
	Status       IPGeolocationStatus `json:"status"`
	Country      string              `json:"country,omitempty"`
	CountryCode  string              `json:"country_code,omitempty"`
	Region       string              `json:"region,omitempty"`
	City         string              `json:"city,omitempty"`
	Organization string              `json:"organization,omitempty"`
}

// IPGeolocationService resolves public-address geography locally. It does not
// decide the client IP: that remains Gin trusted-proxy configuration's job.
type IPGeolocationService struct {
	provider string
	resolver *ip2regionservice.Ip2Region
	hasV4    bool
	hasV6    bool
}

func NewIPGeolocationService(cfg *config.Config) *IPGeolocationService {
	provider := "ip2region"
	cachePolicy := "vectorindex"
	searchers := 4
	var v4Path, v6Path string
	if cfg != nil {
		if configured := strings.ToLower(strings.TrimSpace(cfg.IPGeolocation.Provider)); configured != "" {
			provider = configured
		}
		if configured := strings.TrimSpace(cfg.IPGeolocation.CachePolicy); configured != "" {
			cachePolicy = configured
		}
		if cfg.IPGeolocation.Searchers > 0 {
			searchers = cfg.IPGeolocation.Searchers
		}
		v4Path = strings.TrimSpace(cfg.IPGeolocation.IPv4XDBPath)
		v6Path = strings.TrimSpace(cfg.IPGeolocation.IPv6XDBPath)
	}

	svc := &IPGeolocationService{provider: provider}
	if provider == "disabled" {
		return svc
	}
	if provider != "ip2region" {
		slog.Warn("ip geolocation provider is unsupported; local lookup disabled", "provider", provider)
		return svc
	}

	policy, err := ip2regionservice.CachePolicyFromName(cachePolicy)
	if err != nil {
		slog.Warn("invalid ip2region cache policy; using vectorindex", "cache_policy", cachePolicy, "error", err)
		policy = ip2regionservice.VIndexCache
	}

	v4Config, hasV4 := loadIP2RegionFamilyConfig("ipv4", v4Path, policy, searchers, ip2regionservice.NewV4Config)
	v6Config, hasV6 := loadIP2RegionFamilyConfig("ipv6", v6Path, policy, searchers, ip2regionservice.NewV6Config)
	if !hasV4 && !hasV6 {
		if v4Path != "" || v6Path != "" {
			slog.Warn("ip2region has no usable xdb data; compatibility fallback remains available")
		}
		return svc
	}

	resolver, err := ip2regionservice.NewIp2Region(v4Config, v6Config)
	if err != nil {
		slog.Warn("failed to initialize ip2region; compatibility fallback remains available", "error", err)
		return svc
	}
	svc.resolver = resolver
	svc.hasV4 = hasV4
	svc.hasV6 = hasV6
	slog.Info("offline IP geolocation initialized", "provider", "ip2region", "ipv4", hasV4, "ipv6", hasV6)
	return svc
}

type ip2RegionConfigFactory func(int, string, int) (*ip2regionservice.Config, error)

func loadIP2RegionFamilyConfig(
	family string,
	path string,
	policy int,
	searchers int,
	factory ip2RegionConfigFactory,
) (*ip2regionservice.Config, bool) {
	if path == "" {
		return nil, false
	}
	result, err := factory(policy, path, searchers)
	if err != nil {
		slog.Warn("failed to load ip2region xdb", "family", family, "path", path, "error", err)
		return nil, false
	}
	return result, true
}

// Lookup classifies special-use addresses before calling an offline resolver.
// unavailable means the caller may use its explicit compatibility fallback.
func (s *IPGeolocationService) Lookup(rawIP string) IPGeolocationResult {
	trimmed := strings.TrimSpace(rawIP)
	result := IPGeolocationResult{IP: trimmed}
	addr, err := netip.ParseAddr(trimmed)
	if err != nil {
		result.Status = IPGeolocationStatusInvalid
		return result
	}
	addr = addr.Unmap()
	result.IP = addr.String()
	if isPrivateOrSpecialUseIP(addr) {
		result.Status = IPGeolocationStatusPrivate
		return result
	}
	if s == nil || s.provider != "ip2region" || s.resolver == nil || (addr.Is4() && !s.hasV4) || (addr.Is6() && !s.hasV6) {
		result.Status = IPGeolocationStatusUnavailable
		return result
	}

	record, err := s.resolver.Search(addr.String())
	if err != nil {
		result.Status = IPGeolocationStatusUnavailable
		return result
	}
	parsed := parseIP2RegionRecord(record)
	if parsed.Country == "" && parsed.CountryCode == "" && parsed.Region == "" && parsed.City == "" && parsed.Organization == "" {
		result.Status = IPGeolocationStatusNotFound
		return result
	}
	parsed.IP = result.IP
	parsed.Status = IPGeolocationStatusSuccess
	return parsed
}

func (s *IPGeolocationService) Close() {
	if s != nil && s.resolver != nil {
		s.resolver.Close()
	}
}

func parseIP2RegionRecord(record string) IPGeolocationResult {
	parts := strings.Split(record, "|")
	part := func(index int) string {
		if index >= len(parts) {
			return ""
		}
		value := strings.TrimSpace(parts[index])
		if value == "0" || value == "-" {
			return ""
		}
		return value
	}
	return IPGeolocationResult{
		Country:      part(0),
		Region:       part(1),
		City:         part(2),
		Organization: part(3),
		CountryCode:  part(4),
	}
}

var nonPublicIPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func isPrivateOrSpecialUseIP(addr netip.Addr) bool {
	if !addr.IsValid() || !addr.IsGlobalUnicast() {
		return true
	}
	for _, prefix := range nonPublicIPPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
