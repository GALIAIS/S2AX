package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityProportionalAllocationConservesTotalAndRemainders(t *testing.T) {
	weights := []cityAllocationWeight{
		{key: 10, weight: 5},
		{key: 20, weight: 3},
		{key: 30, weight: 2},
	}
	allocation, err := cityProportionalAllocation(7, weights)
	require.NoError(t, err)
	require.Equal(t, int64(4), allocation[10])
	require.Equal(t, int64(2), allocation[20])
	require.Equal(t, int64(1), allocation[30])
	require.Equal(t, int64(7), allocation[10]+allocation[20]+allocation[30])

	zero, err := cityProportionalAllocation(0, weights)
	require.NoError(t, err)
	require.Equal(t, map[int64]int64{10: 0, 20: 0, 30: 0}, zero)

	_, err = cityProportionalAllocation(1, []cityAllocationWeight{
		{key: 10, weight: 1},
		{key: 10, weight: 1},
	})
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func TestCityNextMarketQuoteIsBoundedAndRespondsToImbalance(t *testing.T) {
	shortage, err := cityNextMarketQuote(500, 100, 1000, 50, 100, 50)
	require.NoError(t, err)
	require.Equal(t, int64(513), shortage)

	excess, err := cityNextMarketQuote(500, 100, 1000, 50, 50, 100)
	require.NoError(t, err)
	require.Equal(t, int64(488), excess)

	floor, err := cityNextMarketQuote(100, 100, 1000, 50, 0, 1000)
	require.NoError(t, err)
	require.Equal(t, int64(100), floor)

	ceiling, err := cityNextMarketQuote(1000, 100, 1000, 50, 1000, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1000), ceiling)

	unchanged, err := cityNextMarketQuote(500, 100, 1000, 50, 100, 100)
	require.NoError(t, err)
	require.Equal(t, int64(500), unchanged)
}

func TestCityMarketIntegerHelpersRejectOverflow(t *testing.T) {
	_, err := cityMultiplyUnits(1<<62, 4)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)

	value, err := cityMulDivFloor(52500, 250, 1000)
	require.NoError(t, err)
	require.Equal(t, int64(13125), value)

	value, err = cityDivideRoundUp(19000, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1900), value)
}
