package service

import (
	"context"
	"testing"
	"time"
)

type sharedQuotaPoolRepoStub struct {
	config        *SharedQuotaPoolConfig
	members       []SharedQuotaPoolMember
	total         float64
	usage         map[int64]float64
	totalByWindow map[string]float64
	usageByWindow map[string]map[int64]float64
	official      map[string]*SharedQuotaOfficialSnapshot
}

func (r *sharedQuotaPoolRepoStub) GetOfficialQuotaSnapshot(_ context.Context, _ int64, windowKey string) (*SharedQuotaOfficialSnapshot, error) {
	if r.official == nil {
		return nil, nil
	}
	return r.official[windowKey], nil
}

func (r *sharedQuotaPoolRepoStub) SaveOfficialQuotaSnapshot(_ context.Context, _ int64, windowKey string, snapshot *SharedQuotaOfficialSnapshot) error {
	if r.official == nil {
		r.official = make(map[string]*SharedQuotaOfficialSnapshot)
	}
	r.official[windowKey] = snapshot
	return nil
}

func (r *sharedQuotaPoolRepoStub) GetConfig(context.Context, int64) (*SharedQuotaPoolConfig, error) {
	return r.config, nil
}

func (r *sharedQuotaPoolRepoStub) SaveConfigAndWindowsAndMembers(_ context.Context, config *SharedQuotaPoolConfig, windows []SharedQuotaPoolWindowConfig, members []SharedQuotaPoolMemberInput) error {
	r.config = config
	r.config.Windows = windows
	r.members = r.members[:0]
	for _, member := range members {
		r.members = append(r.members, SharedQuotaPoolMember{UserID: member.UserID, Weight: member.Weight, Enabled: member.Enabled, Configured: true})
	}
	return nil
}

func (r *sharedQuotaPoolRepoStub) UpsertMember(context.Context, int64, int64, float64, bool) error {
	return nil
}

func (r *sharedQuotaPoolRepoStub) DeleteMember(context.Context, int64, int64) error { return nil }

func (r *sharedQuotaPoolRepoStub) ListActiveMembers(context.Context, int64, time.Time) ([]SharedQuotaPoolMember, error) {
	return append([]SharedQuotaPoolMember(nil), r.members...), nil
}

func (r *sharedQuotaPoolRepoStub) GetUsage(_ context.Context, _ int64, windowStart, windowEnd time.Time) (float64, map[int64]float64, error) {
	windowKey := "long"
	if windowEnd.Sub(windowStart) <= 6*time.Hour {
		windowKey = "short"
	}
	if r.totalByWindow != nil || r.usageByWindow != nil {
		return r.totalByWindow[windowKey], r.usageByWindow[windowKey], nil
	}
	return r.total, r.usage, nil
}

func sharedQuotaTestConfig() *SharedQuotaPoolConfig {
	now := time.Now()
	capacity := 100.0
	return &SharedQuotaPoolConfig{
		GroupID: 1, Enabled: true, WindowSeconds: 604800, CapacityUSD: &capacity,
		ReserveRatio: 0, SoftStopRatio: 0.8, HardStopRatio: 1,
		BorrowEnabled: true, BorrowMultiplier: 1.5,
		WindowStart: now.Add(-time.Hour), WindowEnd: now.Add(6 * 24 * time.Hour),
		Windows: []SharedQuotaPoolWindowConfig{
			{Key: "short", Enabled: true, WindowSeconds: 5 * 60 * 60, CapacityUSD: &capacity,
				ReserveRatio: 0, SoftStopRatio: 0.8, HardStopRatio: 1,
				WindowStart: now.Add(-time.Hour), WindowEnd: now.Add(4 * time.Hour)},
			{Key: "long", Enabled: true, WindowSeconds: 7 * 24 * 60 * 60, CapacityUSD: &capacity,
				ReserveRatio: 0, SoftStopRatio: 0.8, HardStopRatio: 1,
				WindowStart: now.Add(-time.Hour), WindowEnd: now.Add(6 * 24 * time.Hour)},
		},
	}
}

func TestDefaultSharedQuotaPoolUsesFiveHourAndSevenDayWindows(t *testing.T) {
	config := DefaultSharedQuotaPoolConfig(1)
	if len(config.Windows) != 2 {
		t.Fatalf("default windows = %d, want 2", len(config.Windows))
	}
	if config.Windows[0].Key != "short" || config.Windows[0].WindowSeconds != 5*60*60 {
		t.Fatalf("short window = %#v", config.Windows[0])
	}
	if config.Windows[1].Key != "long" || config.Windows[1].WindowSeconds != 7*24*60*60 {
		t.Fatalf("long window = %#v", config.Windows[1])
	}
}

func TestSharedQuotaPoolWeightedSharesAndBorrowing(t *testing.T) {
	repo := &sharedQuotaPoolRepoStub{
		config: sharedQuotaTestConfig(),
		members: []SharedQuotaPoolMember{
			{UserID: 1, Weight: 1, Enabled: true, Configured: true},
			{UserID: 2, Weight: 2, Enabled: true, Configured: true},
			{UserID: 3, Weight: 1, Enabled: true, Configured: true},
		},
		total: 25,
		usage: map[int64]float64{1: 25},
	}
	service := NewSharedQuotaPoolService(repo)

	snapshot, err := service.GetSnapshot(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Members[0].BaseShareUSD; got != 25 {
		t.Fatalf("user 1 base share = %v, want 25", got)
	}
	if got := snapshot.Members[1].BaseShareUSD; got != 50 {
		t.Fatalf("user 2 base share = %v, want 50", got)
	}
	decision, err := service.Check(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || decision.MaximumUSD != 37.5 {
		t.Fatalf("borrow decision = %#v, want allowed with 37.5 maximum", decision)
	}
}

func TestSharedQuotaPoolStopsBorrowingAtSoftAndAllAtHard(t *testing.T) {
	repo := &sharedQuotaPoolRepoStub{
		config: sharedQuotaTestConfig(),
		members: []SharedQuotaPoolMember{
			{UserID: 1, Weight: 1, Enabled: true},
			{UserID: 2, Weight: 1, Enabled: true},
		},
		total: 85,
		usage: map[int64]float64{1: 50},
	}
	service := NewSharedQuotaPoolService(repo)
	baseUser, err := service.Check(context.Background(), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !baseUser.Allowed {
		t.Fatal("user under base share should remain allowed after soft stop")
	}
	borrower, err := service.Check(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if borrower.Allowed {
		t.Fatal("borrower should be denied after soft stop")
	}

	repo.total = 100
	service.RefreshSnapshot(context.Background(), 1)
	decision, err := service.Check(context.Background(), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed {
		t.Fatal("all members should be denied at hard stop")
	}
}

func TestSharedQuotaPoolRequiresEveryEnabledWindow(t *testing.T) {
	repo := &sharedQuotaPoolRepoStub{
		config:        sharedQuotaTestConfig(),
		members:       []SharedQuotaPoolMember{{UserID: 1, Weight: 1, Enabled: true}},
		totalByWindow: map[string]float64{"short": 100, "long": 0},
		usageByWindow: map[string]map[int64]float64{"short": {1: 0}, "long": {1: 0}},
	}
	quota := NewSharedQuotaPoolService(repo)
	decision, err := quota.Check(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed {
		t.Fatal("long window must not bypass an exhausted short window")
	}

	repo.totalByWindow["short"] = 0
	repo.totalByWindow["long"] = 100
	quota.RefreshSnapshot(context.Background(), 1)
	decision, err = quota.Check(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed {
		t.Fatal("short window must not bypass an exhausted long window")
	}
}

func TestSharedQuotaPoolRejectsInvalidConfiguration(t *testing.T) {
	repo := &sharedQuotaPoolRepoStub{}
	service := NewSharedQuotaPoolService(repo)
	capacity := 100.0
	_, err := service.UpdateConfig(context.Background(), 1, &SharedQuotaPoolConfig{
		GroupID: 1, Enabled: true, WindowSeconds: 60, CapacityUSD: &capacity,
		ReserveRatio: 0, SoftStopRatio: 0.9, HardStopRatio: 0.8, BorrowMultiplier: 1,
	}, nil, nil)
	if err != ErrSharedQuotaPoolInvalid {
		t.Fatalf("invalid configuration error = %v, want ErrSharedQuotaPoolInvalid", err)
	}
}

func TestSharedQuotaPoolOfficialPercentUsesProviderWindowAndLocalFairness(t *testing.T) {
	now := time.Date(2026, 8, 3, 4, 0, 0, 0, time.UTC)
	config := sharedQuotaTestConfig()
	config.Windows[0].Enabled = false
	config.Windows[1].CapacityUSD = nil
	config.Windows[1].CapacityMode = SharedQuotaCapacityModeOfficialPercent
	config.Windows[1].UpstreamAccountID = func() *int64 { id := int64(42); return &id }()
	repo := &sharedQuotaPoolRepoStub{
		config: config,
		members: []SharedQuotaPoolMember{
			{UserID: 1, Weight: 1, Enabled: true},
			{UserID: 2, Weight: 1, Enabled: true},
		},
		totalByWindow: map[string]float64{"long": 100},
		usageByWindow: map[string]map[int64]float64{"long": {1: 60, 2: 40}},
		official: map[string]*SharedQuotaOfficialSnapshot{
			"long": {AccountID: 42, UsedPercent: 45, LimitWindowSeconds: 7 * 24 * 60 * 60, FetchedAt: now},
		},
	}
	svc := NewSharedQuotaPoolService(repo)
	svc.now = func() time.Time { return now }
	snapshot, err := svc.GetSnapshot(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	window := snapshot.Windows[1]
	if window.CapacityMode != SharedQuotaCapacityModeOfficialPercent || window.TotalUsedPercent != 45 {
		t.Fatalf("official snapshot = %#v", window)
	}
	if got := window.Members[0].UsedPercent; got != 27 {
		t.Fatalf("user 1 provider-normalized usage = %v, want 27", got)
	}
	if got := window.Members[1].UsedPercent; got != 18 {
		t.Fatalf("user 2 provider-normalized usage = %v, want 18", got)
	}
}
