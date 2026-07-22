package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type accountWindowCostsBatchRepoStub struct {
	UsageLogRepository

	mu sync.Mutex

	batchResults map[string]map[int64]*usagestats.AccountStats
	batchErrors  map[string]error
	batchCalls   map[string]int

	singleResults map[int64]*usagestats.AccountStats
	singleCalls   int
}

func accountWindowCostsBatchKey(startTime time.Time) string {
	return startTime.UTC().Format(time.RFC3339Nano)
}

func (s *accountWindowCostsBatchRepoStub) GetAccountWindowStatsBatch(
	_ context.Context,
	accountIDs []int64,
	startTime time.Time,
) (map[int64]*usagestats.AccountStats, error) {
	key := accountWindowCostsBatchKey(startTime)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batchCalls == nil {
		s.batchCalls = make(map[string]int)
	}
	s.batchCalls[key]++
	if err := s.batchErrors[key]; err != nil {
		return nil, err
	}

	result := make(map[int64]*usagestats.AccountStats, len(accountIDs))
	for _, accountID := range accountIDs {
		if stats := s.batchResults[key][accountID]; stats != nil {
			result[accountID] = stats
		}
	}
	return result, nil
}

func (s *accountWindowCostsBatchRepoStub) GetAccountWindowStats(
	_ context.Context,
	accountID int64,
	_ time.Time,
) (*usagestats.AccountStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.singleCalls++
	if stats := s.singleResults[accountID]; stats != nil {
		return stats, nil
	}
	return &usagestats.AccountStats{}, nil
}

func TestGetAccountWindowCostsBatch_GroupsAccountsByWindowStart(t *testing.T) {
	startA := time.Date(2026, 7, 22, 6, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	startB := startA.Add(2 * time.Hour)
	keyA := accountWindowCostsBatchKey(startA)
	keyB := accountWindowCostsBatchKey(startB)
	repo := &accountWindowCostsBatchRepoStub{
		batchResults: map[string]map[int64]*usagestats.AccountStats{
			keyA: {
				101: {StandardCost: 1.25},
				102: {StandardCost: 2.50},
			},
			keyB: {
				103: {StandardCost: 3.75},
			},
		},
		batchErrors:   make(map[string]error),
		singleResults: make(map[int64]*usagestats.AccountStats),
	}
	service := &AccountUsageService{usageLogRepo: repo}

	costs := service.GetAccountWindowCostsBatch(context.Background(), []AccountWindowStatsRequest{
		{AccountID: 101, StartTime: startA},
		{AccountID: 102, StartTime: startA},
		{AccountID: 103, StartTime: startB},
		{AccountID: 0, StartTime: startA},
	})

	require.Equal(t, map[int64]float64{101: 1.25, 102: 2.50, 103: 3.75}, costs)
	require.Equal(t, 1, repo.batchCalls[keyA])
	require.Equal(t, 1, repo.batchCalls[keyB])
	require.Equal(t, 0, repo.singleCalls)
}

func TestGetAccountWindowCostsBatch_FallsBackOnlyForFailedWindowGroup(t *testing.T) {
	startA := time.Date(2026, 7, 22, 6, 0, 0, 0, time.UTC)
	startB := startA.Add(2 * time.Hour)
	keyA := accountWindowCostsBatchKey(startA)
	keyB := accountWindowCostsBatchKey(startB)
	repo := &accountWindowCostsBatchRepoStub{
		batchResults: map[string]map[int64]*usagestats.AccountStats{
			keyB: {
				203: {StandardCost: 8.25},
			},
		},
		batchErrors: map[string]error{
			keyA: errors.New("batch unavailable"),
		},
		singleResults: map[int64]*usagestats.AccountStats{
			201: {StandardCost: 4.50},
			202: {StandardCost: 6.75},
		},
	}
	service := &AccountUsageService{usageLogRepo: repo}

	costs := service.GetAccountWindowCostsBatch(context.Background(), []AccountWindowStatsRequest{
		{AccountID: 201, StartTime: startA},
		{AccountID: 202, StartTime: startA},
		{AccountID: 203, StartTime: startB},
	})

	require.Equal(t, map[int64]float64{201: 4.50, 202: 6.75, 203: 8.25}, costs)
	require.Equal(t, 1, repo.batchCalls[keyA])
	require.Equal(t, 1, repo.batchCalls[keyB])
	require.Equal(t, 2, repo.singleCalls)
}
