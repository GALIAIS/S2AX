package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type virtualCurrencyHoldExpirerStub struct {
	calls      int
	currencyID int64
	limit      int
	err        error
}

func (s *virtualCurrencyHoldExpirerStub) ExpireExpiredHolds(_ context.Context, currencyID int64, limit int) (int64, error) {
	s.calls++
	s.currencyID = currencyID
	s.limit = limit
	if s.err != nil {
		return 0, s.err
	}
	return 3, nil
}

func TestVirtualCurrencyHoldCleanupServiceCleanupOnce(t *testing.T) {
	expirer := &virtualCurrencyHoldExpirerStub{}
	svc := NewVirtualCurrencyHoldCleanupService(expirer, 5*time.Second, 123)

	svc.cleanupOnce()

	require.Equal(t, 1, expirer.calls)
	require.Zero(t, expirer.currencyID, "zero currency id must sweep every currency")
	require.Equal(t, 123, expirer.limit)
}

func TestVirtualCurrencyHoldCleanupServiceUsesSafeDefaults(t *testing.T) {
	expirer := &virtualCurrencyHoldExpirerStub{err: errors.New("database unavailable")}
	svc := NewVirtualCurrencyHoldCleanupService(expirer, 0, 1000)

	svc.cleanupOnce()

	require.Equal(t, defaultVirtualCurrencyHoldCleanupInterval, svc.interval)
	require.Equal(t, defaultVirtualCurrencyHoldCleanupBatch, svc.batch)
	require.Equal(t, 1, expirer.calls)
}
