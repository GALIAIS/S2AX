package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type ipGeolocationSettingsRepoStub struct {
	values map[string]string
}

func (s *ipGeolocationSettingsRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *ipGeolocationSettingsRepoStub) GetValue(context.Context, string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *ipGeolocationSettingsRepoStub) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (s *ipGeolocationSettingsRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *ipGeolocationSettingsRepoStub) SetMultiple(_ context.Context, updates map[string]string) error {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	for key, value := range updates {
		s.values[key] = value
	}
	return nil
}

func (s *ipGeolocationSettingsRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *ipGeolocationSettingsRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func TestSettingServiceIPGeolocationSettingsPersistAndReload(t *testing.T) {
	ctx := context.Background()
	repo := &ipGeolocationSettingsRepoStub{values: map[string]string{}}
	geo := NewIPGeolocationService(&config.Config{
		IPGeolocation: config.IPGeolocationConfig{Provider: "ip2region"},
	})
	svc := NewSettingService(repo, &config.Config{
		IPGeolocation: config.IPGeolocationConfig{Provider: "ip2region"},
	})
	svc.SetIPGeolocationService(geo)

	updated, err := svc.UpdateIPGeolocationSettings(ctx, IPGeolocationSettings{
		Provider:    IPGeolocationProviderGeoJS,
		CachePolicy: "vectorindex",
		Searchers:   4,
	})
	require.NoError(t, err)
	require.Equal(t, IPGeolocationProviderGeoJS, updated.Provider)
	require.True(t, updated.CompatibilityFallbackEnabled)
	require.Equal(t, "geojs", repo.values[SettingKeyIPGeolocationProvider])
	require.Equal(t, "true", repo.values[SettingKeyIPGeolocationCompatibilityFallbackEnabled])
	require.True(t, geo.Lookup("8.8.8.8").FallbackAllowed)

	updated, err = svc.UpdateIPGeolocationSettings(ctx, IPGeolocationSettings{
		Provider:                     IPGeolocationProviderDisabled,
		CachePolicy:                  "vectorindex",
		Searchers:                    4,
		CompatibilityFallbackEnabled: true,
	})
	require.NoError(t, err)
	require.Equal(t, IPGeolocationProviderDisabled, updated.Provider)
	require.False(t, updated.CompatibilityFallbackEnabled)
	require.Equal(t, "false", repo.values[SettingKeyIPGeolocationCompatibilityFallbackEnabled])
	require.False(t, geo.Lookup("8.8.8.8").FallbackAllowed)
}
