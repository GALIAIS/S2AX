package service

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"
)

type sharedQuotaPoolRepoStub struct {
	config            *SharedQuotaPoolConfig
	members           []SharedQuotaPoolMember
	total             float64
	usage             map[int64]float64
	totalByWindow     map[string]float64
	usageByWindow     map[string]map[int64]float64
	official          map[string]*SharedQuotaOfficialSnapshot
	lastScope         SharedQuotaUsageScope
	lastMemberWindows []SharedQuotaMemberUsageWindow
}

type sharedQuotaAccountRepoStub struct {
	AccountRepository
	accounts map[int64]*Account
}

// sharedQuotaOfficialSourceStub 同时模拟官方窗口和 Analytics，验证模式切换时的基线继承。
type sharedQuotaOfficialSourceStub struct {
	usage     *OpenAIQuotaUsage
	analytics *OpenAIAnalyticsUsage
}

func (s *sharedQuotaOfficialSourceStub) QueryUsage(context.Context, int64) (*OpenAIQuotaUsage, error) {
	return s.usage, nil
}

func (s *sharedQuotaOfficialSourceStub) QueryAnalytics(context.Context, int64, time.Time, time.Time) (*OpenAIAnalyticsUsage, error) {
	return s.analytics, nil
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
		r.members = append(r.members, SharedQuotaPoolMember{UserID: member.UserID, Weight: member.Weight, QuotaUSD: member.QuotaUSD, Enabled: member.Enabled, Configured: true})
	}
	return nil
}

func (r *sharedQuotaPoolRepoStub) UpsertMember(context.Context, int64, int64, float64, *float64, bool) error {
	return nil
}

func (r *sharedQuotaPoolRepoStub) DeleteMember(context.Context, int64, int64) error { return nil }

func (r *sharedQuotaPoolRepoStub) ListActiveMembers(context.Context, int64, time.Time) ([]SharedQuotaPoolMember, error) {
	return append([]SharedQuotaPoolMember(nil), r.members...), nil
}

func (r *sharedQuotaPoolRepoStub) GetUsage(_ context.Context, scope SharedQuotaUsageScope, windowStart, windowEnd time.Time, memberWindows ...[]SharedQuotaMemberUsageWindow) (float64, map[int64]float64, error) {
	r.lastScope = scope
	if len(memberWindows) > 0 {
		r.lastMemberWindows = append([]SharedQuotaMemberUsageWindow(nil), memberWindows[0]...)
	}
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

// TestSharedQuotaPoolUsesAccountScopeAndIndividualAmounts 验证同账号跨分组用量
// 会进入目标池资源账，同时显式金额优先于成员权重。
func TestSharedQuotaPoolUsesAccountScopeAndIndividualAmounts(t *testing.T) {
	config := sharedQuotaTestConfig()
	config.Windows[0].Enabled = false
	accountID := int64(42)
	config.Windows[1].UpstreamAccountID = &accountID
	quotaA, quotaB := 60.0, 20.0
	repo := &sharedQuotaPoolRepoStub{
		config: config,
		members: []SharedQuotaPoolMember{
			{UserID: 1, Weight: 1, QuotaUSD: &quotaA, Enabled: true},
			{UserID: 2, Weight: 1, QuotaUSD: &quotaB, Enabled: true},
			{UserID: 3, Weight: 1, Enabled: true},
		},
		total: 90,
		usage: map[int64]float64{1: 65, 2: 10},
	}

	snapshot, err := NewSharedQuotaPoolService(repo).GetSnapshot(context.Background(), config.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	if repo.lastScope.AccountID == nil || *repo.lastScope.AccountID != accountID || repo.lastScope.GroupID != config.GroupID {
		t.Fatalf("usage scope = %#v, want account %d with target group %d", repo.lastScope, accountID, config.GroupID)
	}
	window := snapshot.Windows[1]
	if got := window.Members[0].BaseShareUSD; got != quotaA {
		t.Fatalf("user 1 base share = %v, want %v", got, quotaA)
	}
	if got := window.Members[1].BaseShareUSD; got != quotaB {
		t.Fatalf("user 2 base share = %v, want %v", got, quotaB)
	}
	if got := window.Members[2].BaseShareUSD; got != 20 {
		t.Fatalf("weight fallback base share = %v, want 20", got)
	}
	if math.Abs(window.Members[0].QuotaUtilizationPercent-(65.0/90.0*100)) > 0.0001 {
		t.Fatalf("user 1 quota utilization = %v, want %v", window.Members[0].QuotaUtilizationPercent, 65.0/90.0*100)
	}
	if math.Abs(window.Members[1].QuotaUtilizationPercent-(10.0/30.0*100)) > 0.0001 {
		t.Fatalf("user 2 quota utilization = %v, want %v", window.Members[1].QuotaUtilizationPercent, 10.0/30.0*100)
	}
	if window.Members[0].Allowed {
		t.Fatal("user 1 should be denied after cross-group usage exhausted the explicit amount")
	}
}

// TestSharedQuotaPoolUsesMemberSubscriptionResetWindows 验证成员用量按订阅自己的
// weekly_window_start 归集，并自动推进已经过期但尚未回写的旧重置点。
func TestSharedQuotaPoolUsesMemberSubscriptionResetWindows(t *testing.T) {
	now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	legacyReset := now.Add(-8 * 24 * time.Hour)
	memberReset := now.Add(-2 * time.Hour)
	config := sharedQuotaTestConfig()
	config.Windows[0].Enabled = false
	repo := &sharedQuotaPoolRepoStub{
		config: config,
		members: []SharedQuotaPoolMember{
			{UserID: 1, Weight: 1, Enabled: true, WeeklyWindowStart: &legacyReset},
			{UserID: 2, Weight: 1, Enabled: true, WeeklyWindowStart: &memberReset},
		},
		totalByWindow: map[string]float64{"long": 10},
		usageByWindow: map[string]map[int64]float64{"long": {1: 6, 2: 4}},
	}
	svc := NewSharedQuotaPoolService(repo)
	svc.now = func() time.Time { return now }
	if _, err := svc.GetSnapshot(context.Background(), config.GroupID); err != nil {
		t.Fatal(err)
	}
	if len(repo.lastMemberWindows) != 2 {
		t.Fatalf("member usage windows = %#v", repo.lastMemberWindows)
	}
	windows := make(map[int64]time.Time, len(repo.lastMemberWindows))
	for _, window := range repo.lastMemberWindows {
		windows[window.UserID] = window.WindowStart
	}
	if got, want := windows[1], legacyReset.Add(7*24*time.Hour); !got.Equal(want) {
		t.Fatalf("user 1 usage start = %s, want %s", got, want)
	}
	if got, want := windows[2], memberReset; !got.Equal(want) {
		t.Fatalf("user 2 usage start = %s, want %s", got, want)
	}
}

// TestSharedQuotaPoolShortWindowIgnoresWeeklySubscriptionReset 验证 5 小时窗口仍
// 使用自身边界，不会因为成员有周订阅重置点而把短窗扩大成整周。
func TestSharedQuotaPoolShortWindowIgnoresWeeklySubscriptionReset(t *testing.T) {
	now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	weeklyReset := now.Add(-24 * time.Hour)
	config := sharedQuotaTestConfig()
	config.Windows[1].Enabled = false
	repo := &sharedQuotaPoolRepoStub{
		config:        config,
		members:       []SharedQuotaPoolMember{{UserID: 1, Weight: 1, Enabled: true, WeeklyWindowStart: &weeklyReset}},
		totalByWindow: map[string]float64{"short": 1},
		usageByWindow: map[string]map[int64]float64{"short": {1: 1}},
	}
	svc := NewSharedQuotaPoolService(repo)
	svc.now = func() time.Time { return now }
	if _, err := svc.GetSnapshot(context.Background(), config.GroupID); err != nil {
		t.Fatal(err)
	}
	if len(repo.lastMemberWindows) != 1 || !repo.lastMemberWindows[0].WindowStart.Equal(config.Windows[0].WindowStart) {
		t.Fatalf("short-window usage start = %#v, want %s", repo.lastMemberWindows, config.Windows[0].WindowStart)
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
	_, _ = service.RefreshSnapshot(context.Background(), 1)
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
	_, _ = quota.RefreshSnapshot(context.Background(), 1)
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
	if math.Abs(window.Members[0].QuotaUtilizationPercent-36) > 0.0001 {
		t.Fatalf("user 1 quota utilization = %v, want 36", window.Members[0].QuotaUtilizationPercent)
	}
	if math.Abs(window.Members[1].QuotaUtilizationPercent-24) > 0.0001 {
		t.Fatalf("user 2 quota utilization = %v, want 24", window.Members[1].QuotaUtilizationPercent)
	}
}

// TestSharedQuotaPoolOfficialUsageUsesSubscriptionWindows 验证官方百分比只用于
// 容量校准；成员已用量必须来自各自订阅重置后的本地日志，不能被账号 Analytics 基线截断。
func TestSharedQuotaPoolOfficialUsageUsesSubscriptionWindows(t *testing.T) {
	now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	resetAt := now.Add(6 * 24 * time.Hour)
	legacyReset := now.Add(-8 * 24 * time.Hour)
	memberReset := now.Add(-24 * time.Hour)
	config := sharedQuotaTestConfig()
	config.Windows[0].Enabled = false
	config.Windows[1].CapacityUSD = nil
	config.Windows[1].CapacityMode = SharedQuotaCapacityModeOfficialPercent
	config.Windows[1].ReserveRatio = 0
	config.Windows[1].HardStopRatio = 0.95
	config.BorrowEnabled = false
	config.Windows[1].UpstreamAccountID = func() *int64 { id := int64(42); return &id }()
	repo := &sharedQuotaPoolRepoStub{
		config: config,
		members: []SharedQuotaPoolMember{
			{UserID: 1, Weight: 1, Enabled: true, WeeklyWindowStart: &legacyReset},
			{UserID: 2, Weight: 1, Enabled: true, WeeklyWindowStart: &memberReset},
		},
		totalByWindow: map[string]float64{"long": 100},
		usageByWindow: map[string]map[int64]float64{"long": {1: 61, 2: 39}},
		official: map[string]*SharedQuotaOfficialSnapshot{
			"long": {
				AccountID: 42, UsedPercent: 29, LimitWindowSeconds: 7 * 24 * 60 * 60,
				ResetAt: resetAt, FetchedAt: now, BaselineUsedPercent: 27,
				BaselineCapturedAt: now.Add(-time.Hour), BaselineResetAt: resetAt,
			},
		},
	}
	svc := NewSharedQuotaPoolService(repo)
	svc.now = func() time.Time { return now }
	snapshot, err := svc.GetSnapshot(context.Background(), config.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	window := snapshot.Windows[1]
	if got := window.Members[0].UsedPercent; math.Abs(got-17.69) > 0.0001 {
		t.Fatalf("user 1 provider-normalized usage = %v, want 17.69", got)
	}
	if got := window.Members[1].UsedPercent; math.Abs(got-11.31) > 0.0001 {
		t.Fatalf("user 2 provider-normalized usage = %v, want 11.31", got)
	}
	if math.Abs(window.Members[0].QuotaUtilizationPercent-(17.69/47.5*100)) > 0.0001 {
		t.Fatalf("user 1 quota utilization = %v", window.Members[0].QuotaUtilizationPercent)
	}
	if len(repo.lastMemberWindows) != 2 || !repo.lastMemberWindows[0].WindowStart.Equal(legacyReset.Add(7*24*time.Hour)) {
		t.Fatalf("member usage windows = %#v", repo.lastMemberWindows)
	}
}

func TestSharedQuotaPoolOfficialAnalyticsIsNotUsedForShortWindow(t *testing.T) {
	now := time.Date(2026, 8, 3, 4, 0, 0, 0, time.UTC)
	config := sharedQuotaTestConfig()
	config.Windows[1].Enabled = false
	config.Windows[0].CapacityUSD = nil
	config.Windows[0].CapacityMode = SharedQuotaCapacityModeOfficialPercent
	config.Windows[0].UpstreamAccountID = func() *int64 { id := int64(42); return &id }()
	repo := &sharedQuotaPoolRepoStub{
		config: config,
		members: []SharedQuotaPoolMember{
			{UserID: 1, Weight: 1, Enabled: true},
			{UserID: 2, Weight: 1, Enabled: true},
		},
		totalByWindow: map[string]float64{"short": 100},
		usageByWindow: map[string]map[int64]float64{"short": {1: 60, 2: 40}},
		official: map[string]*SharedQuotaOfficialSnapshot{
			"short": {
				AccountID: 42, UsedPercent: 45, LimitWindowSeconds: 5 * 60 * 60,
				FetchedAt: now, AnalyticsUsedCredits: 450, AnalyticsStatus: "available",
				AnalyticsFetchedAt: now, AnalyticsCreditsPerUSD: 25,
			},
		},
	}
	svc := NewSharedQuotaPoolService(repo)
	svc.now = func() time.Time { return now }
	snapshot, err := svc.GetSnapshot(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	window := snapshot.Windows[0]
	if window.OfficialAllocationMode != "provider_percent_fallback" {
		t.Fatalf("short-window allocation mode = %q, want provider_percent_fallback", window.OfficialAllocationMode)
	}
	if window.OfficialAnalyticsAvailable {
		t.Fatal("short official window must not use daily analytics calibration")
	}
	if window.Members[0].UsedPercent != 27 || window.Members[1].UsedPercent != 18 {
		t.Fatalf("short-window provider-normalized usage = %#v", window.Members)
	}
}

func TestRefreshOfficialQuotaCarriesFallbackBaselineIntoAnalytics(t *testing.T) {
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	resetAt := now.Add(6 * 24 * time.Hour)
	baselineAt := now.Add(-2 * time.Hour)
	config := sharedQuotaTestConfig()
	config.Windows[0].Enabled = false
	config.Windows[1].CapacityUSD = nil
	config.Windows[1].CapacityMode = SharedQuotaCapacityModeOfficialPercent
	config.Windows[1].UpstreamAccountID = func() *int64 { id := int64(42); return &id }()
	repo := &sharedQuotaPoolRepoStub{
		config: config,
		official: map[string]*SharedQuotaOfficialSnapshot{
			"long": {
				AccountID: 42, UsedPercent: 24, LimitWindowSeconds: 7 * 24 * 60 * 60,
				ResetAt: resetAt, FetchedAt: now.Add(-time.Minute), AnalyticsStatus: "unavailable",
				BaselineUsedPercent: 10, BaselineCapturedAt: baselineAt, BaselineResetAt: resetAt,
			},
		},
	}
	source := &sharedQuotaOfficialSourceStub{
		usage: &OpenAIQuotaUsage{RateLimit: &OpenAIRateLimit{PrimaryWindow: &OpenAIRateLimitWindow{
			UsedPercent: 27, LimitWindowSeconds: 7 * 24 * 60 * 60, ResetAt: resetAt.Unix(),
		}}},
		analytics: &OpenAIAnalyticsUsage{
			Credits: 60134.6, CreditsAvailable: true, Status: "available", FetchedAt: now,
			StartDate: now.Add(-7 * 24 * time.Hour), EndDate: now, CreditsPerUSD: 25,
		},
	}
	svc := NewSharedQuotaPoolService(repo)
	svc.now = func() time.Time { return now }
	svc.SetOfficialQuotaSource(nil, source)
	if err := svc.refreshOfficialQuota(context.Background(), config.GroupID, config.Windows[1]); err != nil {
		t.Fatal(err)
	}
	snapshot := repo.official["long"]
	if snapshot == nil || snapshot.AnalyticsStatus != "available" {
		t.Fatalf("saved analytics snapshot = %#v", snapshot)
	}
	if !snapshot.BaselineCapturedAt.Equal(baselineAt) {
		t.Fatalf("baseline captured at = %s, want original %s", snapshot.BaselineCapturedAt, baselineAt)
	}
	if snapshot.BaselineUsedPercent != 10 {
		t.Fatalf("baseline percent = %v, want 10", snapshot.BaselineUsedPercent)
	}
	wantBaselineCredits := 60134.6 / 0.27 * 0.10
	if math.Abs(snapshot.BaselineUsedCredits-wantBaselineCredits) > 0.0001 {
		t.Fatalf("baseline credits = %v, want %v", snapshot.BaselineUsedCredits, wantBaselineCredits)
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

// Analytics 仅供观测，不能改变已归集的个人用量、份额或准入判断。
func TestSharedQuotaPoolOfficialAnalyticsDoesNotChangeAccounting(t *testing.T) {
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
	if window.OfficialAllocationMode != "provider_percent_fallback" {
		t.Fatalf("allocation mode = %q", window.OfficialAllocationMode)
	}
	if window.OfficialEstimatedCapacityCredits != 0 {
		t.Fatalf("unexpected credit capacity = %v", window.OfficialEstimatedCapacityCredits)
	}
	if window.OfficialAvailablePoolCredits != 0 {
		t.Fatalf("unexpected credit allowance = %v", window.OfficialAvailablePoolCredits)
	}
	if window.Members[0].BaseSharePercent != 47.5 || window.Members[1].BaseSharePercent != 47.5 {
		t.Fatalf("base shares = %#v", window.Members)
	}
	if math.Abs(window.Members[0].UsedPercent-29.0*2/3) > 0.0001 || math.Abs(window.Members[1].UsedPercent-29.0/3) > 0.0001 {
		t.Fatalf("member credits = %#v", window.Members)
	}
	if math.Abs(window.Members[0].QuotaUtilizationPercent-(29.0*2/3/71.25*100)) > 0.0001 ||
		math.Abs(window.Members[1].QuotaUtilizationPercent-(29.0/3/71.25*100)) > 0.0001 {
		t.Fatalf("member quota utilization = %#v", window.Members)
	}
}

// 覆盖 Analytics 从缺失到恢复、过期再恢复的完整过程，确保不会重置或放宽个人额度。
func TestSharedQuotaPoolAnalyticsTransitionsPreserveSubscriptionUsage(t *testing.T) {
	for _, providerUsage := range []float64{0, 49, 99} {
		for _, explicitAmount := range []bool{false, true} {
			config := sharedQuotaTestConfig()
			config.BorrowEnabled = false
			config.Windows[0].Enabled = false
			config.Windows[1].CapacityMode = SharedQuotaCapacityModeOfficialPercent
			config.Windows[1].CapacityUSD = nil
			config.Windows[1].ReserveRatio = 0.01
			config.Windows[1].HardStopRatio = 0.99
			now := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
			start := now.Add(-32 * time.Hour)
			accountID := int64(42)
			config.Windows[1].UpstreamAccountID = &accountID
			members := []SharedQuotaPoolMember{
				{UserID: 1, Weight: 1, Enabled: true, WeeklyWindowStart: &start},
				{UserID: 5, Weight: 0.9, Enabled: true, WeeklyWindowStart: &start},
				{UserID: 9, Weight: 1, Enabled: true, WeeklyWindowStart: &start},
			}
			if explicitAmount {
				amount := 500.0
				members[1].QuotaUSD = &amount
			}
			official := &SharedQuotaOfficialSnapshot{AccountID: accountID, UsedPercent: providerUsage,
				LimitWindowSeconds: 604800, ResetAt: now.Add(5 * 24 * time.Hour), FetchedAt: now}
			repo := &sharedQuotaPoolRepoStub{config: config, members: members,
				totalByWindow: map[string]float64{"long": 771},
				usageByWindow: map[string]map[int64]float64{"long": {1: 352, 5: 418, 9: 1}},
				official:      map[string]*SharedQuotaOfficialSnapshot{"long": official}}
			svc := NewSharedQuotaPoolService(repo)
			svc.now = func() time.Time { return now }
			baseline, err := svc.RefreshSnapshot(context.Background(), config.GroupID)
			if err != nil {
				t.Fatal(err)
			}
			for _, state := range []struct {
				status  string
				age     time.Duration
				credits float64
			}{
				{"available", 0, 60134.6},
				{"available", 20 * time.Minute, 60134.6},
				{"unavailable", 0, 0},
				{"available", 0, 0},
				{"available", 0, 120269.2},
			} {
				official.AnalyticsStatus = state.status
				official.AnalyticsFetchedAt = now.Add(-state.age)
				official.AnalyticsUsedCredits = state.credits
				official.AnalyticsCreditsPerUSD = 25
				official.BaselineUsedCredits = state.credits
				official.BaselineCapturedAt = now
				got, err := svc.RefreshSnapshot(context.Background(), config.GroupID)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(baseline.Members, got.Members) {
					t.Fatalf("provider=%v explicit=%v state=%+v changed member accounting", providerUsage, explicitAmount, state)
				}
				window := got.Windows[1]
				// 即使 Analytics 数据恢复，原始订阅累计费用也必须完整保留。
				if window.TotalUsedUSD != 771 || got.Members[0].UsedUSD != 352 || got.Members[1].UsedUSD != 418 || got.Members[2].UsedUSD != 1 {
					t.Fatal("subscription costs were reset or replaced by Analytics")
				}
				if window.OfficialAccountingStatus != "provider_percent_fallback" || window.OfficialAllocationMode != "provider_percent_fallback" || window.BaseCapacityCredits != 0 {
					t.Fatalf("unexpected accounting mode: %+v", window)
				}
				if len(repo.lastMemberWindows) != 3 || !repo.lastMemberWindows[0].WindowStart.Equal(start) || repo.lastScope.AccountID == nil || *repo.lastScope.AccountID != accountID {
					t.Fatal("Analytics changed subscription window or cross-group account scope")
				}
			}
		}
	}
}
