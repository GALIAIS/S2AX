package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	defaultVirtualCurrencyHoldCleanupInterval = time.Minute
	defaultVirtualCurrencyHoldCleanupBatch    = 500
	virtualCurrencyHoldCleanupTimeout         = 30 * time.Second
)

type virtualCurrencyHoldExpirer interface {
	ExpireExpiredHolds(ctx context.Context, currencyID int64, limit int) (int64, error)
}

// VirtualCurrencyHoldCleanupService releases expired reservations even when
// no administrator opens the maintenance screen.
type VirtualCurrencyHoldCleanupService struct {
	expirer  virtualCurrencyHoldExpirer
	interval time.Duration
	batch    int

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
}

func NewVirtualCurrencyHoldCleanupService(expirer virtualCurrencyHoldExpirer, interval time.Duration, batch int) *VirtualCurrencyHoldCleanupService {
	if interval <= 0 {
		interval = defaultVirtualCurrencyHoldCleanupInterval
	}
	if batch < 1 || batch > 500 {
		batch = defaultVirtualCurrencyHoldCleanupBatch
	}
	return &VirtualCurrencyHoldCleanupService{
		expirer:  expirer,
		interval: interval,
		batch:    batch,
		stopCh:   make(chan struct{}),
	}
}

func (s *VirtualCurrencyHoldCleanupService) Start() {
	if s == nil || s.expirer == nil {
		return
	}
	s.startOnce.Do(func() {
		logger.LegacyPrintf("service.virtual_currency_hold_cleanup", "[VirtualCurrencyHoldCleanup] started interval=%s batch=%d", s.interval, s.batch)
		go s.runLoop()
	})
}

func (s *VirtualCurrencyHoldCleanupService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
		logger.LegacyPrintf("service.virtual_currency_hold_cleanup", "[VirtualCurrencyHoldCleanup] stopped")
	})
}

func (s *VirtualCurrencyHoldCleanupService) runLoop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.cleanupOnce()
	for {
		select {
		case <-ticker.C:
			s.cleanupOnce()
		case <-s.stopCh:
			return
		}
	}
}

func (s *VirtualCurrencyHoldCleanupService) cleanupOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), virtualCurrencyHoldCleanupTimeout)
	defer cancel()

	expired, err := s.expirer.ExpireExpiredHolds(ctx, 0, s.batch)
	if err != nil {
		logger.LegacyPrintf("service.virtual_currency_hold_cleanup", "[VirtualCurrencyHoldCleanup] sweep failed err=%v", err)
		return
	}
	if expired > 0 {
		logger.LegacyPrintf("service.virtual_currency_hold_cleanup", "[VirtualCurrencyHoldCleanup] released expired holds count=%d", expired)
	}
}
