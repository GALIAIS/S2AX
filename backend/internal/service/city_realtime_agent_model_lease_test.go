package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentDecisionLeaseDurationForProfileCoversProviderTimeout(t *testing.T) {
	require.Equal(t, cityRealtimeAgentDecisionLeaseDuration,
		cityRealtimeAgentDecisionLeaseDurationForProfile(nil))
	require.Equal(t, cityRealtimeAgentDecisionLeaseDuration,
		cityRealtimeAgentDecisionLeaseDurationForProfile(&CityRealtimeAgentModelProfile{TimeoutMS: 5_000}))
	require.Equal(t, 75*time.Second,
		cityRealtimeAgentDecisionLeaseDurationForProfile(&CityRealtimeAgentModelProfile{TimeoutMS: 60_000}))
	require.Equal(t, 315*time.Second,
		cityRealtimeAgentDecisionLeaseDurationForProfile(&CityRealtimeAgentModelProfile{TimeoutMS: 300_000}))
}
