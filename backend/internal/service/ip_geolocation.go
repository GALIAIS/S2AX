package service

import (
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"

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

type IPGeolocationProvider string

const (
	IPGeolocationProviderIP2Region IPGeolocationProvider = "ip2region"
	// IPGeolocationProviderGeoJS delegates to the browser compatibility path.
	// The server deliberately does not proxy third-party geolocation responses.
	IPGeolocationProviderGeoJS    IPGeolocationProvider = "geojs"
	IPGeolocationProviderDisabled IPGeolocationProvider = "disabled"
)

// IPGeolocationSettings is the admin-editable configuration together with
// runtime readiness flags. The latter are output-only and let the UI distinguish
// a configured local resolver from a compatibility fallback.
type IPGeolocationSettings struct {
	Provider                     IPGeolocationProvider `json:"provider"`
	IPv4XDBPath                  string                `json:"ipv4_xdb_path"`
	IPv6XDBPath                  string                `json:"ipv6_xdb_path"`
	CachePolicy                  string                `json:"cache_policy"`
	Searchers                    int                   `json:"searchers"`
	CompatibilityFallbackEnabled bool                  `json:"compatibility_fallback_enabled"`
	IPv4DatabaseLoaded           bool                  `json:"ipv4_database_loaded"`
	IPv6DatabaseLoaded           bool                  `json:"ipv6_database_loaded"`
	LocalResolverAvailable       bool                  `json:"local_resolver_available"`
}

// IPGeolocationResult is intentionally a small display DTO. It never carries
// raw provider responses, which can change format and should not be exposed.
type IPGeolocationResult struct {
	IP              string              `json:"ip"`
	Status          IPGeolocationStatus `json:"status"`
	FallbackAllowed bool                `json:"fallback_allowed"`
	Country         string              `json:"country,omitempty"`
	CountryCode     string              `json:"country_code,omitempty"`
	Region          string              `json:"region,omitempty"`
	City            string              `json:"city,omitempty"`
	Organization    string              `json:"organization,omitempty"`
}

// IPGeolocationService resolves public-address geography locally. It does not
// decide the client IP: that remains Gin trusted-proxy configuration's job.
type IPGeolocationService struct {
	mu      sync.RWMutex
	runtime ipGeolocationRuntime
}

type ipGeolocationRuntime struct {
	settings IPGeolocationSettings
	resolver *ip2regionservice.Ip2Region
}

func NewIPGeolocationService(cfg *config.Config) *IPGeolocationService {
	svc := &IPGeolocationService{}
	if err := svc.Reload(defaultIPGeolocationSettings(cfg)); err != nil {
		// Configuration validation normally prevents this path. Keep the server
		// usable if a caller constructs Config directly with an invalid value.
		slog.Warn("invalid initial IP geolocation configuration; using safe compatibility defaults", "error", err)
		_ = svc.Reload(IPGeolocationSettings{
			Provider:                     IPGeolocationProviderIP2Region,
			CachePolicy:                  "vectorindex",
			Searchers:                    4,
			CompatibilityFallbackEnabled: true,
		})
	}
	return svc
}

func defaultIPGeolocationSettings(cfg *config.Config) IPGeolocationSettings {
	settings := IPGeolocationSettings{
		Provider:                     IPGeolocationProviderIP2Region,
		CachePolicy:                  "vectorindex",
		Searchers:                    4,
		CompatibilityFallbackEnabled: true,
	}
	if cfg == nil {
		return settings
	}
	if provider := strings.ToLower(strings.TrimSpace(cfg.IPGeolocation.Provider)); provider != "" {
		settings.Provider = IPGeolocationProvider(provider)
	}
	if policy := strings.ToLower(strings.TrimSpace(cfg.IPGeolocation.CachePolicy)); policy != "" {
		settings.CachePolicy = policy
	}
	if cfg.IPGeolocation.Searchers > 0 {
		settings.Searchers = cfg.IPGeolocation.Searchers
	}
	settings.IPv4XDBPath = strings.TrimSpace(cfg.IPGeolocation.IPv4XDBPath)
	settings.IPv6XDBPath = strings.TrimSpace(cfg.IPGeolocation.IPv6XDBPath)
	return settings
}

func normalizeIPGeolocationSettings(settings IPGeolocationSettings) (IPGeolocationSettings, error) {
	settings.Provider = IPGeolocationProvider(strings.ToLower(strings.TrimSpace(string(settings.Provider))))
	if settings.Provider == "" {
		settings.Provider = IPGeolocationProviderIP2Region
	}
	switch settings.Provider {
	case IPGeolocationProviderIP2Region, IPGeolocationProviderGeoJS, IPGeolocationProviderDisabled:
	default:
		return IPGeolocationSettings{}, fmt.Errorf("ip geolocation provider must be one of: ip2region/geojs/disabled")
	}

	settings.IPv4XDBPath = strings.TrimSpace(settings.IPv4XDBPath)
	settings.IPv6XDBPath = strings.TrimSpace(settings.IPv6XDBPath)
	settings.CachePolicy = strings.ToLower(strings.TrimSpace(settings.CachePolicy))
	if settings.CachePolicy == "" {
		settings.CachePolicy = "vectorindex"
	}
	if _, err := ip2regionservice.CachePolicyFromName(settings.CachePolicy); err != nil {
		return IPGeolocationSettings{}, fmt.Errorf("invalid ip2region cache policy: %w", err)
	}
	if settings.Searchers == 0 {
		settings.Searchers = 4
	}
	if settings.Searchers < 1 || settings.Searchers > 64 {
		return IPGeolocationSettings{}, fmt.Errorf("ip2region searchers must be between 1 and 64")
	}

	// GeoJS is explicitly the compatibility path, while disabled must not cause
	// the browser to issue a third-party lookup at all.
	if settings.Provider == IPGeolocationProviderGeoJS {
		settings.CompatibilityFallbackEnabled = true
	}
	if settings.Provider == IPGeolocationProviderDisabled {
		settings.CompatibilityFallbackEnabled = false
	}
	settings.IPv4DatabaseLoaded = false
	settings.IPv6DatabaseLoaded = false
	settings.LocalResolverAvailable = false
	return settings, nil
}

func buildIPGeolocationRuntime(settings IPGeolocationSettings) (ipGeolocationRuntime, error) {
	normalized, err := normalizeIPGeolocationSettings(settings)
	if err != nil {
		return ipGeolocationRuntime{}, err
	}
	runtime := ipGeolocationRuntime{settings: normalized}
	if normalized.Provider != IPGeolocationProviderIP2Region {
		return runtime, nil
	}

	policy, err := ip2regionservice.CachePolicyFromName(normalized.CachePolicy)
	if err != nil {
		return ipGeolocationRuntime{}, fmt.Errorf("resolve ip2region cache policy: %w", err)
	}
	v4Config, hasV4 := loadIP2RegionFamilyConfig("ipv4", normalized.IPv4XDBPath, policy, normalized.Searchers, ip2regionservice.NewV4Config)
	v6Config, hasV6 := loadIP2RegionFamilyConfig("ipv6", normalized.IPv6XDBPath, policy, normalized.Searchers, ip2regionservice.NewV6Config)
	if !hasV4 && !hasV6 {
		if normalized.IPv4XDBPath != "" || normalized.IPv6XDBPath != "" {
			slog.Warn("ip2region has no usable xdb data", "compatibility_fallback", normalized.CompatibilityFallbackEnabled)
		}
		return runtime, nil
	}

	resolver, err := ip2regionservice.NewIp2Region(v4Config, v6Config)
	if err != nil {
		slog.Warn("failed to initialize ip2region", "compatibility_fallback", normalized.CompatibilityFallbackEnabled, "error", err)
		return runtime, nil
	}
	runtime.resolver = resolver
	runtime.settings.IPv4DatabaseLoaded = hasV4
	runtime.settings.IPv6DatabaseLoaded = hasV6
	runtime.settings.LocalResolverAvailable = true
	slog.Info("offline IP geolocation initialized", "provider", "ip2region", "ipv4", hasV4, "ipv6", hasV6)
	return runtime, nil
}

// Settings returns the active configuration plus local database readiness.
func (s *IPGeolocationService) Settings() IPGeolocationSettings {
	if s == nil {
		return IPGeolocationSettings{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runtime.settings
}

// Reload applies a validated settings snapshot without restarting the server.
// Lookups hold the read lock for the resolver call, so closing the old resolver
// cannot race an in-flight request.
func (s *IPGeolocationService) Reload(settings IPGeolocationSettings) error {
	if s == nil {
		return fmt.Errorf("ip geolocation service is not initialized")
	}
	next, err := buildIPGeolocationRuntime(settings)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.runtime.resolver
	s.runtime = next
	if previous != nil {
		previous.Close()
	}
	return nil
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
// unavailable exposes whether the explicit browser compatibility fallback may
// run; it never hides local resolver failures as a successful lookup.
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
	if s == nil {
		result.Status = IPGeolocationStatusUnavailable
		result.FallbackAllowed = true
		return result
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	runtime := s.runtime
	fallbackAllowed := runtime.settings.CompatibilityFallbackEnabled
	if runtime.settings.Provider != IPGeolocationProviderIP2Region || runtime.resolver == nil || (addr.Is4() && !runtime.settings.IPv4DatabaseLoaded) || (addr.Is6() && !runtime.settings.IPv6DatabaseLoaded) {
		result.Status = IPGeolocationStatusUnavailable
		result.FallbackAllowed = fallbackAllowed
		return result
	}

	record, err := runtime.resolver.Search(addr.String())
	if err != nil {
		result.Status = IPGeolocationStatusUnavailable
		result.FallbackAllowed = fallbackAllowed
		return result
	}
	parsed := parseIP2RegionRecord(record)
	if parsed.Country == "" && parsed.CountryCode == "" && parsed.Region == "" && parsed.City == "" && parsed.Organization == "" {
		result.Status = IPGeolocationStatusNotFound
		result.FallbackAllowed = fallbackAllowed
		return result
	}
	parsed.IP = result.IP
	parsed.Status = IPGeolocationStatusSuccess
	return parsed
}

func (s *IPGeolocationService) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtime.resolver != nil {
		s.runtime.resolver.Close()
		s.runtime.resolver = nil
	}
	s.runtime.settings.IPv4DatabaseLoaded = false
	s.runtime.settings.IPv6DatabaseLoaded = false
	s.runtime.settings.LocalResolverAvailable = false
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
