package securityaudit

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunBoundedMaintenanceBatchesDrainsFullPages(t *testing.T) {
	pages := []int64{500, 500, 73}
	calls := 0
	total, err := runBoundedMaintenanceBatches(
		context.Background(),
		500,
		20,
		func(context.Context) (int64, error) {
			value := pages[calls]
			calls++
			return value, nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, int64(1073), total)
	require.Equal(t, 3, calls)
}

func TestRunBoundedMaintenanceBatchesStopsAtSafetyLimit(t *testing.T) {
	calls := 0
	total, err := runBoundedMaintenanceBatches(
		context.Background(),
		500,
		3,
		func(context.Context) (int64, error) {
			calls++
			return 500, nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, int64(1500), total)
	require.Equal(t, 3, calls)
}

func TestRunBoundedMaintenanceBatchesPropagatesFailure(t *testing.T) {
	expected := errors.New("database unavailable")
	calls := 0
	total, err := runBoundedMaintenanceBatches(
		context.Background(),
		500,
		20,
		func(context.Context) (int64, error) {
			calls++
			if calls == 2 {
				return 0, expected
			}
			return 500, nil
		},
	)

	require.ErrorIs(t, err, expected)
	require.Equal(t, int64(500), total)
	require.Equal(t, 2, calls)
}
