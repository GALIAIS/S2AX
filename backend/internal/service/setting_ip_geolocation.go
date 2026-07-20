package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

var ipGeolocationSettingKeys = []string{
	SettingKeyIPGeolocationProvider,
	SettingKeyIPGeolocationIPv4XDBPath,
	SettingKeyIPGeolocationIPv6XDBPath,
	SettingKeyIPGeolocationCachePolicy,
	SettingKeyIPGeolocationSearchers,
	SettingKeyIPGeolocationCompatibilityFallbackEnabled,
}

// SetIPGeolocationService attaches the reloadable lookup runtime without
// changing NewSettingService, which is used directly by many focused tests.
func (s *SettingService) SetIPGeolocationService(ipGeolocationService *IPGeolocationService) {
	if s != nil {
		s.ipGeolocationService = ipGeolocationService
	}
}

func (s *SettingService) readIPGeolocationSettings(ctx context.Context) (IPGeolocationSettings, error) {
	if s == nil {
		return normalizeIPGeolocationSettings(defaultIPGeolocationSettings(nil))
	}
	settings := defaultIPGeolocationSettings(s.cfg)
	if s.settingRepo == nil {
		return normalizeIPGeolocationSettings(settings)
	}

	values, err := s.settingRepo.GetMultiple(ctx, ipGeolocationSettingKeys)
	if err != nil {
		return IPGeolocationSettings{}, fmt.Errorf("get IP geolocation settings: %w", err)
	}
	if value, ok := values[SettingKeyIPGeolocationProvider]; ok && strings.TrimSpace(value) != "" {
		settings.Provider = IPGeolocationProvider(value)
	}
	if value, ok := values[SettingKeyIPGeolocationIPv4XDBPath]; ok {
		settings.IPv4XDBPath = value
	}
	if value, ok := values[SettingKeyIPGeolocationIPv6XDBPath]; ok {
		settings.IPv6XDBPath = value
	}
	if value, ok := values[SettingKeyIPGeolocationCachePolicy]; ok && strings.TrimSpace(value) != "" {
		settings.CachePolicy = value
	}
	if value, ok := values[SettingKeyIPGeolocationSearchers]; ok && strings.TrimSpace(value) != "" {
		searchers, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr != nil {
			return IPGeolocationSettings{}, fmt.Errorf("parse ip geolocation searchers: %w", parseErr)
		}
		settings.Searchers = searchers
	}
	if value, ok := values[SettingKeyIPGeolocationCompatibilityFallbackEnabled]; ok && strings.TrimSpace(value) != "" {
		enabled, parseErr := strconv.ParseBool(strings.TrimSpace(value))
		if parseErr != nil {
			return IPGeolocationSettings{}, fmt.Errorf("parse IP geolocation compatibility fallback: %w", parseErr)
		}
		settings.CompatibilityFallbackEnabled = enabled
	}
	return normalizeIPGeolocationSettings(settings)
}

// LoadIPGeolocationSettings restores the persisted settings when the process
// starts. It deliberately reloads the resolver in-place, avoiding a restart.
func (s *SettingService) LoadIPGeolocationSettings(ctx context.Context) error {
	if s == nil || s.ipGeolocationService == nil {
		return nil
	}
	settings, err := s.readIPGeolocationSettings(ctx)
	if err != nil {
		return err
	}
	if err := s.ipGeolocationService.Reload(settings); err != nil {
		return fmt.Errorf("reload IP geolocation settings: %w", err)
	}
	return nil
}

// GetIPGeolocationSettings returns editable fields plus the readiness of the
// currently active local resolver. This makes a missing .xdb mount visible.
func (s *SettingService) GetIPGeolocationSettings(ctx context.Context) (*IPGeolocationSettings, error) {
	settings, err := s.readIPGeolocationSettings(ctx)
	if err != nil {
		return nil, err
	}
	if s != nil && s.ipGeolocationService != nil {
		runtime := s.ipGeolocationService.Settings()
		if runtime.Provider == settings.Provider &&
			runtime.IPv4XDBPath == settings.IPv4XDBPath &&
			runtime.IPv6XDBPath == settings.IPv6XDBPath &&
			runtime.CachePolicy == settings.CachePolicy &&
			runtime.Searchers == settings.Searchers {
			settings.IPv4DatabaseLoaded = runtime.IPv4DatabaseLoaded
			settings.IPv6DatabaseLoaded = runtime.IPv6DatabaseLoaded
			settings.LocalResolverAvailable = runtime.LocalResolverAvailable
		}
	}
	return &settings, nil
}

// UpdateIPGeolocationSettings persists a complete configuration snapshot, then
// atomically swaps the active resolver. Invalid paths degrade to a visible
// unavailable runtime rather than making the setting impossible to save.
func (s *SettingService) UpdateIPGeolocationSettings(ctx context.Context, settings IPGeolocationSettings) (*IPGeolocationSettings, error) {
	if s == nil || s.settingRepo == nil {
		return nil, fmt.Errorf("IP geolocation settings repository is unavailable")
	}
	normalized, err := normalizeIPGeolocationSettings(settings)
	if err != nil {
		return nil, err
	}
	updates := map[string]string{
		SettingKeyIPGeolocationProvider:                     string(normalized.Provider),
		SettingKeyIPGeolocationIPv4XDBPath:                  normalized.IPv4XDBPath,
		SettingKeyIPGeolocationIPv6XDBPath:                  normalized.IPv6XDBPath,
		SettingKeyIPGeolocationCachePolicy:                  normalized.CachePolicy,
		SettingKeyIPGeolocationSearchers:                    strconv.Itoa(normalized.Searchers),
		SettingKeyIPGeolocationCompatibilityFallbackEnabled: strconv.FormatBool(normalized.CompatibilityFallbackEnabled),
	}
	if err := s.settingRepo.SetMultiple(ctx, updates); err != nil {
		return nil, fmt.Errorf("save IP geolocation settings: %w", err)
	}
	if s.ipGeolocationService != nil {
		if err := s.ipGeolocationService.Reload(normalized); err != nil {
			return nil, fmt.Errorf("reload IP geolocation settings: %w", err)
		}
	}
	return s.GetIPGeolocationSettings(ctx)
}
