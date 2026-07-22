package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsSlowGatewayTiming(t *testing.T) {
	fastTTFT := 4999
	slowTTFT := 5000

	require.False(t, isSlowGatewayTiming(9*time.Second, &fastTTFT))
	require.True(t, isSlowGatewayTiming(9*time.Second, &slowTTFT))
	require.True(t, isSlowGatewayTiming(10*time.Second, nil))
}
