package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestIPGeolocationLookupClassifiesPrivateAndUnavailableAddresses(t *testing.T) {
	svc := NewIPGeolocationService(&config.Config{
		IPGeolocation: config.IPGeolocationConfig{Provider: "ip2region"},
	})

	for _, address := range []string{"10.0.0.1", "100.64.0.1", "127.0.0.1", "192.0.2.7", "2001:db8::1", "fc00::1"} {
		result := svc.Lookup(address)
		require.Equal(t, IPGeolocationStatusPrivate, result.Status, address)
	}

	require.Equal(t, IPGeolocationStatusInvalid, svc.Lookup("not-an-ip").Status)
	require.Equal(t, IPGeolocationStatusUnavailable, svc.Lookup("8.8.8.8").Status)
}

func TestParseIP2RegionRecord(t *testing.T) {
	result := parseIP2RegionRecord("中国|广东省|深圳市|电信|CN")
	require.Equal(t, "中国", result.Country)
	require.Equal(t, "广东省", result.Region)
	require.Equal(t, "深圳市", result.City)
	require.Equal(t, "电信", result.Organization)
	require.Equal(t, "CN", result.CountryCode)

	empty := parseIP2RegionRecord("0|0|0|0|0")
	require.Empty(t, empty.Country)
	require.Empty(t, empty.CountryCode)
}
