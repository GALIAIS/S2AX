package service

import (
	"context"
	"math"
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

type sharedQuotaAccountRepoStub struct {
	AccountRepository
	accounts map[int64]*Account
}

func (r *sharedQuotaAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	return r.accounts[id], nil
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

func TestDisabledSharedQuotaPoolDoesNotExposeActiveWindows(t *testing.T) {
	config := sharedQuotaTestConfig()
	config.Enabled = false
	repo := &sharedQuotaPoolRepoStub{config: config}

	snapshot, err := NewSharedQuotaPoolService(repo).GetSnapshot(context.Background(), config.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	for _, window := range snapshot.Windows {
		if window.Config.Enabled {
			t.Fatalf("disabled pool exposed active window %q", window.Config.Key)
		}
	}
}

func TestStoredOfficialQuotaWindowUsesFreshAccountSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	fetchedAt := now.Add(-time.Minute)
	resetAt := now.Add(6 * 24 * time.Hour)
	account := &Account{Extra: map[string]any{
		"codex_7d_used_percent":   55,
		"codex_7d_window_minutes": 10080,
		"codex_7d_reset_at":       resetAt.Format(time.RFC3339),
		"codex_usage_updated_at":  fetchedAt.Format(time.RFC3339),
	}}

	window, gotFetchedAt := storedOfficialQuotaWindow(account, 7*24*60*60, now)
	if window == nil {
		t.Fatal("stored official quota window = nil")
	}
	if window.UsedPercent != 55 || window.LimitWindowSeconds != 7*24*60*60 {
		t.Fatalf("stored official quota window = %#v", window)
	}
	if window.ResetAt != resetAt.Unix() || !gotFetchedAt.Equal(fetchedAt) {
		t.Fatalf("stored official quota timestamps = reset %d fetched %s", window.ResetAt, gotFetchedAt)
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

func TestSharedQuotaPoolOfficialPercentPrefersFreshAccountSnapshotOverStalePoolRow(t *testing.T) {
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	config := sharedQuotaTestConfig()
	config.Windows[0].Enabled = false
	config.Windows[1].CapacityUSD = nil
	config.Windows[1].CapacityMode = SharedQuotaCapacityModeOfficialPercent
	config.Windows[1].UpstreamAccountID = func() *int64 { id := int64(42); return &id }()
	staleFetchedAt := now.Add(-20 * time.Minute)
	freshFetchedAt := now.Add(-time.Minute)
	repo := &sharedQuotaPoolRepoStub{
		config:  config,
		members: []SharedQuotaPoolMember{{UserID: 1, Weight: 1, Enabled: true}},
		official: map[string]*SharedQuotaOfficialSnapshot{
			"long": {AccountID: 42, UsedPercent: 12, LimitWindowSeconds: 7 * 24 * 60 * 60, FetchedAt: staleFetchedAt},
		},
	}
	accountRepo := &sharedQuotaAccountRepoStub{accounts: map[int64]*Account{
		42: {ID: 42, Extra: map[string]any{
			"codex_7d_used_percent":   56,
			"codex_7d_window_minutes": 10080,
			"codex_usage_updated_at":  freshFetchedAt.Format(time.RFC3339),
		}},
	}}
	svc := NewSharedQuotaPoolService(repo)
	svc.accountRepo = accountRepo
	svc.now = func() time.Time { return now }

	snapshot, err := svc.GetSnapshot(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	window := snapshot.Windows[1]
	if !window.OfficialDataAvailable || window.OfficialDataStale {
		t.Fatalf("official data state = available:%v stale:%v", window.OfficialDataAvailable, window.OfficialDataStale)
	}
	if window.OfficialUsedPercent != 56 {
		t.Fatalf("official used percent = %v, want 56", window.OfficialUsedPercent)
	}
}

func TestSharedQuotaPoolOfficialAnalyticsSubtractsPrePoolBaseline(t *testing.T) {
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	config := sharedQuotaTestConfig()
	config.Windows[0].Enabled = false
	config.Windows[1].CapacityUSD = nil
	config.Windows[1].CapacityMode = SharedQuotaCapacityModeOfficialPercent
	config.Windows[1].ReserveRatio = 0
	config.Windows[1].HardStopRatio = 0.95
	config.Windows[1].UpstreamAccountID = func() *int64 { id := int64(42); return &id }()
	repo := &sharedQuotaPoolRepoStub{
		config: config,
		members: []SharedQuotaPoolMember{
			{UserID: 1, Weight: 1, Enabled: true},
			{UserID: 2, Weight: 1, Enabled: true},
		},
		usageByWindow: map[string]map[int64]float64{"long": {1: 2, 2: 1}},
		totalByWindow: map[string]float64{"long": 3},
		official: map[string]*SharedQuotaOfficialSnapshot{
			"long": {
				AccountID: 42, UsedPercent: 29, LimitWindowSeconds: 7 * 24 * 60 * 60,
				FetchedAt: now, AnalyticsUsedCredits: 290, AnalyticsStatus: "available",
				AnalyticsFetchedAt: now, AnalyticsCreditsPerUSD: 25,
				BaselineUsedCredits: 100, BaselineUsedPercent: 10,
				BaselineCapturedAt: now.Add(-time.Hour),
			},
		},
	}
	svc := NewSharedQuotaPoolService(repo)
	svc.now = func() time.Time { return now }
	snapshot, err := svc.GetSnapshot(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	window := snapshot.Windows[1]
	if window.OfficialAllocationMode != "analytics_credit" {
		t.Fatalf("allocation mode = %q", window.OfficialAllocationMode)
	}
	if math.Abs(window.OfficialEstimatedCapacityCredits-1000) > 0.0001 {
		t.Fatalf("estimated capacity = %v, want 1000", window.OfficialEstimatedCapacityCredits)
	}
	if math.Abs(window.OfficialAvailablePoolCredits-660) > 0.0001 {
		t.Fatalf("available pool credits = %v, want 660", window.OfficialAvailablePoolCredits)
	}
	if math.Abs(window.Members[0].BaseShareCredits-330) > 0.0001 || math.Abs(window.Members[1].BaseShareCredits-330) > 0.0001 {
		t.Fatalf("base shares = %#v", window.Members)
	}
	if math.Abs(window.Members[0].UsedCredits-50) > 0.0001 || math.Abs(window.Members[1].UsedCredits-25) > 0.0001 {
		t.Fatalf("member credits = %#v", window.Members)
	}
}
