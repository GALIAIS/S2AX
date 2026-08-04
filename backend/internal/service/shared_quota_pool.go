package service

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/sync/singleflight"
)

const (
	defaultSharedQuotaWindowSeconds  = 7 * 24 * 60 * 60
	defaultSharedQuotaReserveRatio   = 0.15
	defaultSharedQuotaSoftStopRatio  = 0.85
	defaultSharedQuotaHardStopRatio  = 0.95
	defaultSharedQuotaBorrowMultiple = 1.5
	sharedQuotaSnapshotTTL           = 2 * time.Second
	officialQuotaSnapshotTTL         = 60 * time.Second
	officialQuotaMaxStale            = 15 * time.Minute
)

const (
	SharedQuotaCapacityModeUSD             = "manual_usd"
	SharedQuotaCapacityModeOfficialPercent = "official_percent"
)

var (
	ErrSharedQuotaPoolNotConfigured = infraerrors.NotFound("SHARED_QUOTA_POOL_NOT_CONFIGURED", "shared quota pool is not configured")
	ErrSharedQuotaPoolInvalid       = infraerrors.BadRequest("SHARED_QUOTA_POOL_INVALID", "invalid shared quota pool configuration")
	ErrSharedQuotaPoolUnavailable   = infraerrors.ServiceUnavailable("SHARED_QUOTA_POOL_UNAVAILABLE", "shared quota pool service is unavailable")
	ErrSharedQuotaPoolExceeded      = infraerrors.TooManyRequests("SHARED_QUOTA_POOL_EXCEEDED", "shared quota pool limit exceeded")
	ErrSharedQuotaMemberDisabled    = infraerrors.Forbidden("SHARED_QUOTA_MEMBER_DISABLED", "user is disabled in the shared quota pool")
)

// SharedQuotaPoolRepository is deliberately smaller than the Ent subscription
// repository. Pool aggregation is based on usage_logs and is kept as a raw SQL
// read model so enabling this feature does not change existing Ent entities.
type SharedQuotaPoolRepository interface {
	GetConfig(ctx context.Context, groupID int64) (*SharedQuotaPoolConfig, error)
	SaveConfigAndWindowsAndMembers(ctx context.Context, config *SharedQuotaPoolConfig, windows []SharedQuotaPoolWindowConfig, members []SharedQuotaPoolMemberInput) error
	UpsertMember(ctx context.Context, groupID, userID int64, weight float64, enabled bool) error
	DeleteMember(ctx context.Context, groupID, userID int64) error
	ListActiveMembers(ctx context.Context, groupID int64, now time.Time) ([]SharedQuotaPoolMember, error)
	GetUsage(ctx context.Context, groupID int64, windowStart, windowEnd time.Time) (float64, map[int64]float64, error)
	GetOfficialQuotaSnapshot(ctx context.Context, groupID int64, windowKey string) (*SharedQuotaOfficialSnapshot, error)
	SaveOfficialQuotaSnapshot(ctx context.Context, groupID int64, windowKey string, snapshot *SharedQuotaOfficialSnapshot) error
}

type SharedQuotaOfficialSnapshot struct {
	AccountID          int64     `json:"account_id"`
	UsedPercent        float64   `json:"used_percent"`
	LimitWindowSeconds int64     `json:"limit_window_seconds"`
	ResetAt            time.Time `json:"reset_at"`
	FetchedAt          time.Time `json:"fetched_at"`
}

type SharedQuotaOfficialQuotaSource interface {
	QueryUsage(ctx context.Context, accountID int64) (*OpenAIQuotaUsage, error)
}

type SharedQuotaPoolConfig struct {
	GroupID int64 `json:"group_id"`
	Enabled bool  `json:"enabled"`

	WindowSeconds int      `json:"window_seconds"`
	CapacityUSD   *float64 `json:"capacity_usd"`
	ReserveRatio  float64  `json:"reserve_ratio"`
	SoftStopRatio float64  `json:"soft_stop_ratio"`
	HardStopRatio float64  `json:"hard_stop_ratio"`

	BorrowEnabled    bool    `json:"borrow_enabled"`
	BorrowMultiplier float64 `json:"borrow_multiplier"`

	UpstreamCapacityUSD        *float64 `json:"upstream_capacity_usd,omitempty"`
	UpstreamUtilizationPercent *float64 `json:"upstream_utilization_percent,omitempty"`

	WindowStart       time.Time                     `json:"window_start"`
	WindowEnd         time.Time                     `json:"window_end"`
	UpdatedAt         time.Time                     `json:"updated_at"`
	Windows           []SharedQuotaPoolWindowConfig `json:"windows"`
	CapacityMode      string                        `json:"capacity_mode,omitempty"`
	UpstreamAccountID *int64                        `json:"upstream_account_id,omitempty"`
}

type SharedQuotaPoolWindowConfig struct {
	Key                        string    `json:"key"`
	Enabled                    bool      `json:"enabled"`
	WindowSeconds              int       `json:"window_seconds"`
	CapacityUSD                *float64  `json:"capacity_usd"`
	ReserveRatio               float64   `json:"reserve_ratio"`
	SoftStopRatio              float64   `json:"soft_stop_ratio"`
	HardStopRatio              float64   `json:"hard_stop_ratio"`
	UpstreamCapacityUSD        *float64  `json:"upstream_capacity_usd,omitempty"`
	UpstreamUtilizationPercent *float64  `json:"upstream_utilization_percent,omitempty"`
	WindowStart                time.Time `json:"window_start"`
	WindowEnd                  time.Time `json:"window_end"`
	UpdatedAt                  time.Time `json:"updated_at"`
	CapacityMode               string    `json:"capacity_mode,omitempty"`
	UpstreamAccountID          *int64    `json:"upstream_account_id,omitempty"`
}

type SharedQuotaPoolMemberInput struct {
	UserID  int64   `json:"user_id"`
	Weight  float64 `json:"weight"`
	Enabled bool    `json:"enabled"`
}

type SharedQuotaPoolMember struct {
	UserID         int64   `json:"user_id"`
	SubscriptionID int64   `json:"subscription_id"`
	Email          string  `json:"email"`
	Username       string  `json:"username"`
	Weight         float64 `json:"weight"`
	Enabled        bool    `json:"enabled"`
	Configured     bool    `json:"configured"`
}

type SharedQuotaPoolMemberSnapshot struct {
	SharedQuotaPoolMember
	UsedUSD          float64 `json:"used_usd"`
	BaseShareUSD     float64 `json:"base_share_usd"`
	MaximumUSD       float64 `json:"maximum_usd"`
	RemainingUSD     float64 `json:"remaining_usd"`
	BorrowedUSD      float64 `json:"borrowed_usd"`
	SharePercent     float64 `json:"share_percent"`
	Allowed          bool    `json:"allowed"`
	DecisionReason   string  `json:"decision_reason,omitempty"`
	UsedPercent      float64 `json:"used_percent,omitempty"`
	BaseSharePercent float64 `json:"base_share_percent,omitempty"`
	MaximumPercent   float64 `json:"maximum_percent,omitempty"`
	RemainingPercent float64 `json:"remaining_percent,omitempty"`
	BorrowedPercent  float64 `json:"borrowed_percent,omitempty"`
}

type SharedQuotaPoolSnapshot struct {
	Config SharedQuotaPoolConfig `json:"config"`

	BaseCapacityUSD       float64                         `json:"base_capacity_usd"`
	DistributableUSD      float64                         `json:"distributable_usd"`
	EstimatedCapacityUSD  float64                         `json:"estimated_capacity_usd,omitempty"`
	TotalUsedUSD          float64                         `json:"total_used_usd"`
	RemainingUSD          float64                         `json:"remaining_usd"`
	UtilizationPercent    float64                         `json:"utilization_percent"`
	SoftLimitUSD          float64                         `json:"soft_limit_usd"`
	HardLimitUSD          float64                         `json:"hard_limit_usd"`
	SoftStopReached       bool                            `json:"soft_stop_reached"`
	HardStopReached       bool                            `json:"hard_stop_reached"`
	ActiveMemberCount     int                             `json:"active_member_count"`
	ConfiguredMemberCount int                             `json:"configured_member_count"`
	Members               []SharedQuotaPoolMemberSnapshot `json:"members"`
	Windows               []SharedQuotaPoolWindowSnapshot `json:"windows"`
	SnapshotAt            time.Time                       `json:"snapshot_at"`
	CapacityMode          string                          `json:"capacity_mode,omitempty"`
}

type SharedQuotaPoolWindowSnapshot struct {
	Config                SharedQuotaPoolWindowConfig     `json:"config"`
	BaseCapacityUSD       float64                         `json:"base_capacity_usd"`
	DistributableUSD      float64                         `json:"distributable_usd"`
	EstimatedCapacityUSD  float64                         `json:"estimated_capacity_usd,omitempty"`
	TotalUsedUSD          float64                         `json:"total_used_usd"`
	RemainingUSD          float64                         `json:"remaining_usd"`
	UtilizationPercent    float64                         `json:"utilization_percent"`
	SoftLimitUSD          float64                         `json:"soft_limit_usd"`
	HardLimitUSD          float64                         `json:"hard_limit_usd"`
	SoftStopReached       bool                            `json:"soft_stop_reached"`
	HardStopReached       bool                            `json:"hard_stop_reached"`
	Members               []SharedQuotaPoolMemberSnapshot `json:"members"`
	CapacityMode          string                          `json:"capacity_mode,omitempty"`
	OfficialDataAvailable bool                            `json:"official_data_available,omitempty"`
	OfficialDataStale     bool                            `json:"official_data_stale,omitempty"`
	OfficialUsedPercent   float64                         `json:"official_used_percent,omitempty"`
	OfficialResetAt       time.Time                       `json:"official_reset_at,omitempty"`
	OfficialFetchedAt     time.Time                       `json:"official_fetched_at,omitempty"`
	BaseCapacityPercent   float64                         `json:"base_capacity_percent,omitempty"`
	DistributablePercent  float64                         `json:"distributable_percent,omitempty"`
	TotalUsedPercent      float64                         `json:"total_used_percent,omitempty"`
	RemainingPercent      float64                         `json:"remaining_percent,omitempty"`
	SoftLimitPercent      float64                         `json:"soft_limit_percent,omitempty"`
	HardLimitPercent      float64                         `json:"hard_limit_percent,omitempty"`
}

type SharedQuotaPoolDecision struct {
	Enabled          bool
	Allowed          bool
	Reason           string
	BaseShareUSD     float64
	MaximumUSD       float64
	UsedUSD          float64
	BorrowedUSD      float64
	SharePercent     float64
	BaseSharePercent float64
	MaximumPercent   float64
	UsedPercent      float64
	BorrowedPercent  float64
	Snapshot         *SharedQuotaPoolSnapshot
}

// SharedQuotaUserProgress is the private, user-scoped view of a shared pool.
// It intentionally contains no other member identity or usage data.
type SharedQuotaUserProgress struct {
	Enabled                bool                            `json:"enabled"`
	BaseShareUSD           float64                         `json:"base_share_usd"`
	MaximumUSD             float64                         `json:"maximum_usd"`
	UsedUSD                float64                         `json:"used_usd"`
	RemainingUSD           float64                         `json:"remaining_usd"`
	BorrowedUSD            float64                         `json:"borrowed_usd"`
	SharePercent           float64                         `json:"share_percent"`
	PoolTotalUsedUSD       float64                         `json:"pool_total_used_usd"`
	PoolDistributableUSD   float64                         `json:"pool_distributable_usd"`
	PoolUtilizationPercent float64                         `json:"pool_utilization_percent"`
	CapacityMode           string                          `json:"capacity_mode,omitempty"`
	UsedPercent            float64                         `json:"used_percent,omitempty"`
	BaseSharePercent       float64                         `json:"base_share_percent,omitempty"`
	MaximumPercent         float64                         `json:"maximum_percent,omitempty"`
	RemainingPercent       float64                         `json:"remaining_percent,omitempty"`
	BorrowedPercent        float64                         `json:"borrowed_percent,omitempty"`
	SoftStopReached        bool                            `json:"soft_stop_reached"`
	HardStopReached        bool                            `json:"hard_stop_reached"`
	Allowed                bool                            `json:"allowed"`
	WindowStart            time.Time                       `json:"window_start"`
	WindowEnd              time.Time                       `json:"window_end"`
	Windows                []SharedQuotaUserWindowProgress `json:"windows,omitempty"`
}

type SharedQuotaUserWindowProgress struct {
	Key                    string    `json:"key"`
	WindowSeconds          int       `json:"window_seconds"`
	BaseShareUSD           float64   `json:"base_share_usd"`
	MaximumUSD             float64   `json:"maximum_usd"`
	UsedUSD                float64   `json:"used_usd"`
	RemainingUSD           float64   `json:"remaining_usd"`
	BorrowedUSD            float64   `json:"borrowed_usd"`
	SharePercent           float64   `json:"share_percent"`
	PoolTotalUsedUSD       float64   `json:"pool_total_used_usd"`
	PoolDistributableUSD   float64   `json:"pool_distributable_usd"`
	PoolUtilizationPercent float64   `json:"pool_utilization_percent"`
	SoftStopReached        bool      `json:"soft_stop_reached"`
	HardStopReached        bool      `json:"hard_stop_reached"`
	Allowed                bool      `json:"allowed"`
	WindowStart            time.Time `json:"window_start"`
	WindowEnd              time.Time `json:"window_end"`
	CapacityMode           string    `json:"capacity_mode,omitempty"`
	OfficialDataAvailable  bool      `json:"official_data_available,omitempty"`
	OfficialDataStale      bool      `json:"official_data_stale,omitempty"`
	OfficialUsedPercent    float64   `json:"official_used_percent,omitempty"`
	OfficialResetAt        time.Time `json:"official_reset_at,omitempty"`
	OfficialFetchedAt      time.Time `json:"official_fetched_at,omitempty"`
	BaseSharePercent       float64   `json:"base_share_percent,omitempty"`
	MaximumPercent         float64   `json:"maximum_percent,omitempty"`
	UsedPercent            float64   `json:"used_percent,omitempty"`
	RemainingPercent       float64   `json:"remaining_percent,omitempty"`
	BorrowedPercent        float64   `json:"borrowed_percent,omitempty"`
}

type sharedQuotaSnapshotCacheEntry struct {
	snapshot  *SharedQuotaPoolSnapshot
	expiresAt time.Time
}

// SharedQuotaPoolService owns the allocation policy and the short-lived
// aggregate snapshot. Billing remains authoritative in the existing billing
// repository; this service is only an opt-in admission controller.
type SharedQuotaPoolService struct {
	repo SharedQuotaPoolRepository
	now  func() time.Time

	cacheMu             sync.Mutex
	cache               map[int64]sharedQuotaSnapshotCacheEntry
	flight              singleflight.Group
	officialQuotaSource SharedQuotaOfficialQuotaSource
	accountRepo         AccountRepository
	officialFlight      singleflight.Group
}

func NewSharedQuotaPoolService(repo SharedQuotaPoolRepository) *SharedQuotaPoolService {
	return &SharedQuotaPoolService{
		repo:  repo,
		now:   time.Now,
		cache: make(map[int64]sharedQuotaSnapshotCacheEntry),
	}
}

func (s *SharedQuotaPoolService) SetOfficialQuotaSource(accountRepo AccountRepository, source SharedQuotaOfficialQuotaSource) {
	if s == nil {
		return
	}
	s.accountRepo = accountRepo
	s.officialQuotaSource = source
}

func (s *SharedQuotaPoolService) GetSnapshot(ctx context.Context, groupID int64) (*SharedQuotaPoolSnapshot, error) {
	return s.getSnapshot(ctx, groupID, false)
}

func (s *SharedQuotaPoolService) RefreshSnapshot(ctx context.Context, groupID int64) (*SharedQuotaPoolSnapshot, error) {
	s.invalidate(groupID)
	return s.getSnapshot(ctx, groupID, true)
}

func (s *SharedQuotaPoolService) Check(ctx context.Context, groupID, userID int64) (*SharedQuotaPoolDecision, error) {
	snapshot, err := s.GetSnapshot(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if !snapshot.Config.Enabled {
		return &SharedQuotaPoolDecision{Enabled: false, Allowed: true, Snapshot: snapshot}, nil
	}

	var first *SharedQuotaPoolDecision
	for _, window := range snapshot.Windows {
		if !window.Config.Enabled {
			continue
		}
		member := findPoolMember(window.Members, userID)
		decision := &SharedQuotaPoolDecision{Enabled: true, Allowed: false, Snapshot: snapshot}
		if member == nil {
			decision.Reason = "user is not an active member of the shared quota pool"
		} else {
			decision.Allowed = member.Allowed
			decision.Reason = member.DecisionReason
			decision.BaseShareUSD = member.BaseShareUSD
			decision.MaximumUSD = member.MaximumUSD
			decision.UsedUSD = member.UsedUSD
			decision.BorrowedUSD = member.BorrowedUSD
			decision.SharePercent = member.SharePercent
			decision.BaseSharePercent = member.BaseSharePercent
			decision.MaximumPercent = member.MaximumPercent
			decision.UsedPercent = member.UsedPercent
			decision.BorrowedPercent = member.BorrowedPercent
			if !member.Enabled {
				decision.Allowed = false
				decision.Reason = ErrSharedQuotaMemberDisabled.Error()
			}
		}
		if first == nil {
			first = decision
		}
		if !decision.Allowed {
			return decision, nil
		}
	}
	if first != nil {
		return first, nil
	}
	return &SharedQuotaPoolDecision{Enabled: true, Allowed: false, Reason: "no enabled shared quota window", Snapshot: snapshot}, nil
}

// UserProgress returns only the requesting user's allocation view.
func (s *SharedQuotaPoolService) UserProgress(ctx context.Context, groupID, userID int64) (*SharedQuotaUserProgress, error) {
	snapshot, err := s.GetSnapshot(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if !snapshot.Config.Enabled {
		return nil, nil
	}
	progress := &SharedQuotaUserProgress{Enabled: true}
	for _, window := range snapshot.Windows {
		if !window.Config.Enabled {
			continue
		}
		member := findPoolMember(window.Members, userID)
		windowEnd := window.Config.WindowEnd
		if window.CapacityMode == SharedQuotaCapacityModeOfficialPercent && !window.OfficialResetAt.IsZero() {
			windowEnd = window.OfficialResetAt
		}
		item := SharedQuotaUserWindowProgress{
			Key: window.Config.Key, WindowSeconds: window.Config.WindowSeconds,
			PoolTotalUsedUSD: window.TotalUsedUSD, PoolDistributableUSD: window.DistributableUSD,
			PoolUtilizationPercent: window.UtilizationPercent, SoftStopReached: window.SoftStopReached,
			HardStopReached: window.HardStopReached, Allowed: false,
			WindowStart: window.Config.WindowStart, WindowEnd: windowEnd,
			CapacityMode:          window.CapacityMode,
			OfficialDataAvailable: window.OfficialDataAvailable,
			OfficialDataStale:     window.OfficialDataStale,
			OfficialUsedPercent:   window.OfficialUsedPercent,
			OfficialResetAt:       window.OfficialResetAt,
			OfficialFetchedAt:     window.OfficialFetchedAt,
		}
		if member != nil {
			item.BaseShareUSD = member.BaseShareUSD
			item.MaximumUSD = member.MaximumUSD
			item.UsedUSD = member.UsedUSD
			item.RemainingUSD = member.RemainingUSD
			item.BorrowedUSD = member.BorrowedUSD
			item.SharePercent = member.SharePercent
			item.UsedPercent = member.UsedPercent
			item.BaseSharePercent = member.BaseSharePercent
			item.MaximumPercent = member.MaximumPercent
			item.RemainingPercent = member.RemainingPercent
			item.BorrowedPercent = member.BorrowedPercent
			item.Allowed = member.Allowed && member.Enabled
		}
		progress.Windows = append(progress.Windows, item)
	}
	primary := progress.Windows
	for _, item := range primary {
		if item.Key == "long" {
			primary = []SharedQuotaUserWindowProgress{item}
			break
		}
	}
	if len(primary) > 0 {
		item := primary[0]
		progress.BaseShareUSD = item.BaseShareUSD
		progress.MaximumUSD = item.MaximumUSD
		progress.UsedUSD = item.UsedUSD
		progress.RemainingUSD = item.RemainingUSD
		progress.BorrowedUSD = item.BorrowedUSD
		progress.SharePercent = item.SharePercent
		progress.CapacityMode = item.CapacityMode
		progress.UsedPercent = item.UsedPercent
		progress.BaseSharePercent = item.BaseSharePercent
		progress.MaximumPercent = item.MaximumPercent
		progress.RemainingPercent = item.RemainingPercent
		progress.BorrowedPercent = item.BorrowedPercent
		progress.PoolTotalUsedUSD = item.PoolTotalUsedUSD
		progress.PoolDistributableUSD = item.PoolDistributableUSD
		progress.PoolUtilizationPercent = item.PoolUtilizationPercent
		progress.SoftStopReached = item.SoftStopReached
		progress.HardStopReached = item.HardStopReached
		progress.Allowed = item.Allowed
		progress.WindowStart = item.WindowStart
		progress.WindowEnd = item.WindowEnd
	}
	return progress, nil
}

// DefaultSharedQuotaPoolConfig returns the safe disabled defaults used when a
// group has not been configured yet.
func DefaultSharedQuotaPoolConfig(groupID int64) SharedQuotaPoolConfig {
	return defaultSharedQuotaPoolConfig(groupID)
}

func (s *SharedQuotaPoolService) UpdateConfig(ctx context.Context, groupID int64, config *SharedQuotaPoolConfig, windows []SharedQuotaPoolWindowConfig, members []SharedQuotaPoolMemberInput) (*SharedQuotaPoolSnapshot, error) {
	if config == nil {
		return nil, ErrSharedQuotaPoolInvalid
	}
	config.GroupID = groupID
	if existing, err := s.repo.GetConfig(ctx, groupID); err != nil {
		return nil, err
	} else if existing != nil {
		if len(windows) == 0 && len(existing.Windows) > 0 {
			windows = append([]SharedQuotaPoolWindowConfig(nil), existing.Windows...)
		}
		for i := range windows {
			for _, oldWindow := range existing.Windows {
				if windows[i].Key == oldWindow.Key {
					sameDuration := windows[i].WindowSeconds == 0 || windows[i].WindowSeconds == oldWindow.WindowSeconds
					if sameDuration && windows[i].WindowStart.IsZero() {
						windows[i].WindowStart = oldWindow.WindowStart
					}
					if sameDuration && windows[i].WindowEnd.IsZero() {
						windows[i].WindowEnd = oldWindow.WindowEnd
					}
					break
				}
			}
		}
	}
	config.Windows = normalizeSharedQuotaWindows(config, windows)
	// The master switch owns enforcement. Persisting enabled child windows while
	// the pool is disabled creates an ambiguous state where the settings page
	// looks active but subscriptions still use the legacy group limit.
	if !config.Enabled {
		for i := range config.Windows {
			config.Windows[i].Enabled = false
		}
	}
	if err := validateSharedQuotaPoolConfig(config); err != nil {
		return nil, err
	}
	if err := validateSharedQuotaPoolMembers(members); err != nil {
		return nil, err
	}
	if err := s.repo.SaveConfigAndWindowsAndMembers(ctx, config, config.Windows, members); err != nil {
		return nil, err
	}
	s.invalidate(groupID)
	return s.RefreshSnapshot(ctx, groupID)
}

func (s *SharedQuotaPoolService) UpdateMember(ctx context.Context, groupID int64, member SharedQuotaPoolMemberInput) (*SharedQuotaPoolSnapshot, error) {
	if groupID <= 0 || member.UserID <= 0 || member.Weight <= 0 || member.Weight > 100000 {
		return nil, ErrSharedQuotaPoolInvalid
	}
	if err := s.repo.UpsertMember(ctx, groupID, member.UserID, member.Weight, member.Enabled); err != nil {
		return nil, err
	}
	s.invalidate(groupID)
	return s.RefreshSnapshot(ctx, groupID)
}

func (s *SharedQuotaPoolService) DeleteMember(ctx context.Context, groupID, userID int64) (*SharedQuotaPoolSnapshot, error) {
	if groupID <= 0 || userID <= 0 {
		return nil, ErrSharedQuotaPoolInvalid
	}
	if err := s.repo.DeleteMember(ctx, groupID, userID); err != nil {
		return nil, err
	}
	s.invalidate(groupID)
	return s.RefreshSnapshot(ctx, groupID)
}

func (s *SharedQuotaPoolService) getSnapshot(ctx context.Context, groupID int64, force bool) (*SharedQuotaPoolSnapshot, error) {
	if s == nil || s.repo == nil || groupID <= 0 {
		return &SharedQuotaPoolSnapshot{Config: defaultSharedQuotaPoolConfig(groupID), SnapshotAt: time.Now()}, nil
	}
	if !force {
		s.cacheMu.Lock()
		entry, ok := s.cache[groupID]
		s.cacheMu.Unlock()
		if ok && time.Now().Before(entry.expiresAt) {
			return entry.snapshot, nil
		}
	}

	value, err, _ := s.flight.Do(fmt.Sprintf("shared-quota:%d", groupID), func() (any, error) {
		if !force {
			s.cacheMu.Lock()
			entry, ok := s.cache[groupID]
			s.cacheMu.Unlock()
			if ok && time.Now().Before(entry.expiresAt) {
				return entry.snapshot, nil
			}
		}
		return s.loadSnapshot(ctx, groupID)
	})
	if err != nil {
		return nil, err
	}
	snapshot, ok := value.(*SharedQuotaPoolSnapshot)
	if !ok || snapshot == nil {
		return nil, fmt.Errorf("shared quota snapshot has invalid type")
	}
	s.cacheMu.Lock()
	s.cache[groupID] = sharedQuotaSnapshotCacheEntry{snapshot: snapshot, expiresAt: time.Now().Add(sharedQuotaSnapshotTTL)}
	s.cacheMu.Unlock()
	return snapshot, nil
}

func (s *SharedQuotaPoolService) loadSnapshot(ctx context.Context, groupID int64) (*SharedQuotaPoolSnapshot, error) {
	config, err := s.repo.GetConfig(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if config == nil {
		defaultConfig := defaultSharedQuotaPoolConfig(groupID)
		config = &defaultConfig
	}
	config.Windows = normalizeSharedQuotaWindows(config, config.Windows)
	snapshot := &SharedQuotaPoolSnapshot{Config: *config, SnapshotAt: s.now(), Members: []SharedQuotaPoolMemberSnapshot{}, Windows: []SharedQuotaPoolWindowSnapshot{}}
	members, err := s.repo.ListActiveMembers(ctx, groupID, s.now())
	if err != nil {
		return nil, err
	}
	snapshot.ActiveMemberCount = len(members)
	for _, member := range members {
		if member.Configured {
			snapshot.ConfiguredMemberCount++
		}
	}
	if !config.Enabled {
		for i := range config.Windows {
			config.Windows[i].Enabled = false
		}
		snapshot.Config = *config
		for _, member := range members {
			snapshot.Members = append(snapshot.Members, SharedQuotaPoolMemberSnapshot{
				SharedQuotaPoolMember: member,
				Allowed:               member.Enabled,
				DecisionReason:        "shared quota pool disabled",
			})
		}
		for _, window := range config.Windows {
			snapshot.Windows = append(snapshot.Windows, SharedQuotaPoolWindowSnapshot{
				Config:  window,
				Members: []SharedQuotaPoolMemberSnapshot{},
			})
		}
		return snapshot, nil
	}
	for _, window := range config.Windows {
		if !window.Enabled {
			snapshot.Windows = append(snapshot.Windows, SharedQuotaPoolWindowSnapshot{Config: window, Members: []SharedQuotaPoolMemberSnapshot{}})
			continue
		}
		windowSnapshot, err := s.calculateWindowSnapshot(ctx, groupID, *config, window, members)
		if err != nil {
			return nil, err
		}
		snapshot.Windows = append(snapshot.Windows, *windowSnapshot)
	}
	if len(snapshot.Windows) == 0 {
		return nil, fmt.Errorf("%w: enabled pool requires at least one enabled window", ErrSharedQuotaPoolInvalid)
	}
	primary := snapshot.Windows[0]
	for _, window := range snapshot.Windows {
		if window.Config.Key == "long" {
			primary = window
			break
		}
	}
	snapshot.BaseCapacityUSD = primary.BaseCapacityUSD
	snapshot.DistributableUSD = primary.DistributableUSD
	snapshot.EstimatedCapacityUSD = primary.EstimatedCapacityUSD
	snapshot.TotalUsedUSD = primary.TotalUsedUSD
	snapshot.RemainingUSD = primary.RemainingUSD
	snapshot.UtilizationPercent = primary.UtilizationPercent
	snapshot.SoftLimitUSD = primary.SoftLimitUSD
	snapshot.HardLimitUSD = primary.HardLimitUSD
	snapshot.SoftStopReached = primary.SoftStopReached
	snapshot.HardStopReached = primary.HardStopReached
	snapshot.Members = primary.Members
	snapshot.CapacityMode = primary.CapacityMode
	return snapshot, nil
}

func (s *SharedQuotaPoolService) calculateWindowSnapshot(ctx context.Context, groupID int64, pool SharedQuotaPoolConfig, window SharedQuotaPoolWindowConfig, members []SharedQuotaPoolMember) (*SharedQuotaPoolWindowSnapshot, error) {
	if window.CapacityMode == SharedQuotaCapacityModeOfficialPercent {
		return s.calculateOfficialWindowSnapshot(ctx, groupID, pool, window, members)
	}
	if window.CapacityUSD == nil || *window.CapacityUSD <= 0 {
		return nil, fmt.Errorf("%w: window %s requires capacity_usd", ErrSharedQuotaPoolInvalid, window.Key)
	}
	baseCapacity := *window.CapacityUSD
	distributable := baseCapacity * (1 - window.ReserveRatio)
	softLimit := distributable * window.SoftStopRatio
	hardLimit := distributable * window.HardStopRatio
	totalUsed, usageByUser, err := s.repo.GetUsage(ctx, groupID, window.WindowStart, window.WindowEnd)
	if err != nil {
		return nil, err
	}
	totalWeight := 0.0
	for _, member := range members {
		if member.Enabled {
			totalWeight += member.Weight
		}
	}
	windowSnapshot := &SharedQuotaPoolWindowSnapshot{
		Config: window, BaseCapacityUSD: baseCapacity, DistributableUSD: distributable,
		TotalUsedUSD: totalUsed, RemainingUSD: math.Max(0, distributable-totalUsed),
		SoftLimitUSD: softLimit, HardLimitUSD: hardLimit,
		SoftStopReached: totalUsed >= softLimit, HardStopReached: totalUsed >= hardLimit,
		CapacityMode: SharedQuotaCapacityModeUSD,
		Members:      make([]SharedQuotaPoolMemberSnapshot, 0, len(members)),
	}
	if distributable > 0 {
		windowSnapshot.UtilizationPercent = totalUsed / distributable * 100
	}
	if window.UpstreamUtilizationPercent != nil && *window.UpstreamUtilizationPercent > 0 && totalUsed > 0 {
		windowSnapshot.EstimatedCapacityUSD = totalUsed / (*window.UpstreamUtilizationPercent / 100)
	}
	for _, member := range members {
		used := usageByUser[member.UserID]
		baseShare := 0.0
		if member.Enabled && totalWeight > 0 {
			baseShare = distributable * member.Weight / totalWeight
		}
		maximum := baseShare
		if pool.BorrowEnabled {
			maximum *= pool.BorrowMultiplier
		}
		allowed := member.Enabled && baseShare > 0 && totalUsed < hardLimit
		reason := "within base share"
		if !member.Enabled {
			allowed, reason = false, ErrSharedQuotaMemberDisabled.Error()
		} else if used >= baseShare {
			if pool.BorrowEnabled && totalUsed < softLimit && used < maximum {
				reason = "borrowing unused pool capacity"
			} else {
				allowed, reason = false, "base share exhausted"
			}
		}
		sharePercent := 0.0
		if distributable > 0 {
			sharePercent = baseShare / distributable * 100
		}
		windowSnapshot.Members = append(windowSnapshot.Members, SharedQuotaPoolMemberSnapshot{
			SharedQuotaPoolMember: member, UsedUSD: used, BaseShareUSD: baseShare,
			MaximumUSD: maximum, RemainingUSD: math.Max(0, maximum-used),
			BorrowedUSD: math.Max(0, used-baseShare), SharePercent: sharePercent,
			Allowed: allowed, DecisionReason: reason,
		})
	}
	return windowSnapshot, nil
}

func (s *SharedQuotaPoolService) calculateOfficialWindowSnapshot(ctx context.Context, groupID int64, pool SharedQuotaPoolConfig, window SharedQuotaPoolWindowConfig, members []SharedQuotaPoolMember) (*SharedQuotaPoolWindowSnapshot, error) {
	official, err := s.repo.GetOfficialQuotaSnapshot(ctx, groupID, window.Key)
	if err != nil {
		return nil, err
	}
	if official != nil && window.UpstreamAccountID != nil && official.AccountID != *window.UpstreamAccountID {
		official = nil
	}
	now := s.now()
	// Prefer the newest account-level snapshot even when the shared-pool row
	// already exists. The pool row is a derived cache; keeping an older row here
	// made the UI stay in "syncing" after the account itself had fresh quota data.
	if s.accountRepo != nil && window.UpstreamAccountID != nil {
		if account, accountErr := s.accountRepo.GetByID(ctx, *window.UpstreamAccountID); accountErr == nil {
			if storedWindow, fetchedAt := storedOfficialQuotaWindow(account, int64(window.WindowSeconds), now); storedWindow != nil {
				if official == nil || fetchedAt.After(official.FetchedAt) {
					resetAt := time.Time{}
					if storedWindow.ResetAt > 0 {
						resetAt = time.Unix(storedWindow.ResetAt, 0)
					}
					official = &SharedQuotaOfficialSnapshot{
						AccountID:          *window.UpstreamAccountID,
						UsedPercent:        storedWindow.UsedPercent,
						LimitWindowSeconds: storedWindow.LimitWindowSeconds,
						ResetAt:            resetAt,
						FetchedAt:          fetchedAt,
					}
				}
			}
		}
	}
	available := official != nil && official.FetchedAt.After(time.Time{}) && now.Sub(official.FetchedAt) <= officialQuotaMaxStale
	stale := official == nil || now.Sub(official.FetchedAt) > officialQuotaSnapshotTTL
	if stale {
		s.scheduleOfficialQuotaRefresh(groupID, window)
	}

	baseCapacity := 100.0
	distributable := baseCapacity * (1 - window.ReserveRatio)
	softLimit := distributable * window.SoftStopRatio
	hardLimit := distributable * window.HardStopRatio
	usedPercent := 0.0
	if available {
		usedPercent = clampPercent(official.UsedPercent)
	} else {
		// The first provider refresh is asynchronous so enabling this mode never
		// adds provider latency to a gateway request. Until it arrives, fail open
		// and expose the pending state to the administrator.
		available = false
	}

	usageStart, usageEnd := window.WindowStart, window.WindowEnd
	if available && !official.ResetAt.IsZero() {
		usageEnd = official.ResetAt
		usageStart = usageEnd.Add(-time.Duration(maxInt64(official.LimitWindowSeconds, int64(window.WindowSeconds))) * time.Second)
	}
	localTotal, usageByUser, err := s.repo.GetUsage(ctx, groupID, usageStart, usageEnd)
	if err != nil {
		return nil, err
	}
	if localTotal < 0 {
		localTotal = 0
	}
	totalWeight := enabledMemberWeight(members)
	windowSnapshot := &SharedQuotaPoolWindowSnapshot{
		Config: window, CapacityMode: SharedQuotaCapacityModeOfficialPercent,
		BaseCapacityPercent: baseCapacity, DistributablePercent: distributable,
		SoftLimitPercent: softLimit, HardLimitPercent: hardLimit,
		OfficialDataAvailable: available, OfficialDataStale: official != nil && stale,
		Members: make([]SharedQuotaPoolMemberSnapshot, 0, len(members)),
	}
	if official != nil {
		windowSnapshot.OfficialUsedPercent = clampPercent(official.UsedPercent)
		windowSnapshot.OfficialResetAt = official.ResetAt
		windowSnapshot.OfficialFetchedAt = official.FetchedAt
	}
	if available {
		windowSnapshot.TotalUsedPercent = usedPercent
		windowSnapshot.RemainingPercent = math.Max(0, distributable-usedPercent)
		windowSnapshot.UtilizationPercent = usedPercent / distributable * 100
		windowSnapshot.SoftStopReached = usedPercent >= softLimit
		windowSnapshot.HardStopReached = usedPercent >= hardLimit
	}
	for _, member := range members {
		baseShare := 0.0
		if member.Enabled && totalWeight > 0 {
			baseShare = distributable * member.Weight / totalWeight
		}
		maximum := baseShare
		if pool.BorrowEnabled {
			maximum *= pool.BorrowMultiplier
		}
		localUsed := math.Max(0, usageByUser[member.UserID])
		memberUsed := 0.0
		if available && localTotal > 0 {
			memberUsed = usedPercent * localUsed / localTotal
		}
		allowed := member.Enabled
		reason := "official quota snapshot pending"
		if !member.Enabled {
			allowed, reason = false, ErrSharedQuotaMemberDisabled.Error()
		} else if available {
			reason = "within base share"
			if usedPercent >= hardLimit {
				allowed, reason = false, "official upstream hard limit reached"
			} else if memberUsed >= baseShare {
				if pool.BorrowEnabled && usedPercent < softLimit && memberUsed < maximum {
					reason = "borrowing unused pool capacity"
				} else {
					allowed, reason = false, "base share exhausted"
				}
			}
		}
		sharePercent := 0.0
		if distributable > 0 {
			sharePercent = baseShare / distributable * 100
		}
		windowSnapshot.Members = append(windowSnapshot.Members, SharedQuotaPoolMemberSnapshot{
			SharedQuotaPoolMember: member,
			Allowed:               allowed,
			DecisionReason:        reason,
			SharePercent:          sharePercent,
			UsedPercent:           memberUsed,
			BaseSharePercent:      baseShare,
			MaximumPercent:        maximum,
			RemainingPercent:      math.Max(0, maximum-memberUsed),
			BorrowedPercent:       math.Max(0, memberUsed-baseShare),
		})
	}
	return windowSnapshot, nil
}

func (s *SharedQuotaPoolService) scheduleOfficialQuotaRefresh(groupID int64, window SharedQuotaPoolWindowConfig) {
	// The account snapshot is a valid fallback when the active upstream probe is
	// temporarily unavailable. Keep the refresh scheduled as long as either
	// source is available; otherwise an official pool would remain pending
	// forever with no observable error.
	if s == nil || s.repo == nil || (s.officialQuotaSource == nil && s.accountRepo == nil) {
		return
	}
	key := fmt.Sprintf("official-quota:%d:%s", groupID, window.Key)
	resultCh := s.officialFlight.DoChan(key, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), openaiQuotaUpstreamTimeout)
		defer cancel()
		err := s.refreshOfficialQuota(ctx, groupID, window)
		return nil, err
	})
	go func() {
		if result := <-resultCh; result.Err != nil {
			log.Printf("official quota refresh failed group=%d window=%s: %v", groupID, window.Key, result.Err)
		}
	}()
}

func (s *SharedQuotaPoolService) refreshOfficialQuota(ctx context.Context, groupID int64, window SharedQuotaPoolWindowConfig) error {
	accountID := int64(0)
	if window.UpstreamAccountID != nil {
		accountID = *window.UpstreamAccountID
	} else if s.accountRepo != nil {
		accounts, err := s.accountRepo.ListByGroup(ctx, groupID)
		if err != nil {
			return err
		}
		for _, account := range accounts {
			if account.Platform == PlatformOpenAI && account.Type == AccountTypeOAuth && account.Status == StatusActive {
				if accountID != 0 {
					return fmt.Errorf("official quota source is ambiguous for group %d", groupID)
				}
				accountID = account.ID
			}
		}
	}
	if accountID <= 0 {
		return fmt.Errorf("official quota source account is not configured for group %d", groupID)
	}
	var providerWindow *OpenAIRateLimitWindow
	var refreshErr error
	if s.officialQuotaSource != nil {
		usage, err := s.officialQuotaSource.QueryUsage(ctx, accountID)
		if err != nil {
			refreshErr = err
		} else {
			providerWindow = selectOfficialQuotaWindow(usage, int64(window.WindowSeconds))
			if providerWindow == nil {
				refreshErr = fmt.Errorf("official quota window %s is not available", window.Key)
			}
		}
	} else {
		refreshErr = fmt.Errorf("official quota upstream source is not configured")
	}

	fetchedAt := time.Now()
	if providerWindow == nil && s.accountRepo != nil {
		account, err := s.accountRepo.GetByID(ctx, accountID)
		if err == nil {
			providerWindow, fetchedAt = storedOfficialQuotaWindow(account, int64(window.WindowSeconds), s.now())
			if providerWindow != nil {
				log.Printf("official quota refresh used account snapshot group=%d window=%s account=%d upstream_error=%v", groupID, window.Key, accountID, refreshErr)
			}
		}
	}
	if providerWindow == nil {
		if refreshErr == nil {
			refreshErr = fmt.Errorf("official quota window %s is not available", window.Key)
		}
		return refreshErr
	}
	snapshot := &SharedQuotaOfficialSnapshot{
		AccountID: accountID, UsedPercent: clampPercent(providerWindow.UsedPercent),
		LimitWindowSeconds: providerWindow.LimitWindowSeconds, FetchedAt: fetchedAt,
	}
	if providerWindow.ResetAt > 0 {
		snapshot.ResetAt = time.Unix(providerWindow.ResetAt, 0)
	}
	if err := s.repo.SaveOfficialQuotaSnapshot(ctx, groupID, window.Key, snapshot); err != nil {
		return err
	}
	s.invalidate(groupID)
	return nil
}

// storedOfficialQuotaWindow turns the latest account-level Codex snapshot into
// the same narrow window shape returned by /wham/usage. The gateway already
// persists these fields from successful upstream traffic, so using them during
// a temporary quota-probe failure prevents an official pool from being stuck
// in a permanent "syncing" state while preserving the snapshot timestamp.
func storedOfficialQuotaWindow(account *Account, windowSeconds int64, now time.Time) (*OpenAIRateLimitWindow, time.Time) {
	if account == nil || len(account.Extra) == 0 || windowSeconds <= 0 {
		return nil, time.Time{}
	}
	window := "7d"
	if windowSeconds <= 6*60*60 {
		window = "5h"
	}

	usedKey := "codex_" + window + "_used_percent"
	minutesKey := "codex_" + window + "_window_minutes"
	resetAfterKey := "codex_" + window + "_reset_after_seconds"
	resetAtKey := "codex_" + window + "_reset_at"
	updatedAtKey := "codex_usage_updated_at"
	usedRaw, ok := account.Extra[usedKey]
	if !ok {
		return nil, time.Time{}
	}
	updatedRaw, ok := account.Extra[updatedAtKey]
	if !ok {
		return nil, time.Time{}
	}
	fetchedAt, err := parseTime(fmt.Sprint(updatedRaw))
	if err != nil || fetchedAt.IsZero() {
		return nil, time.Time{}
	}

	limitSeconds := windowSeconds
	if minutes := parseExtraInt(account.Extra[minutesKey]); minutes > 0 {
		limitSeconds = int64(minutes) * 60
	}
	if absInt64(limitSeconds-windowSeconds) > 60 {
		return nil, time.Time{}
	}

	usedPercent := clampPercent(parseExtraFloat64(usedRaw))
	resetAt := int64(0)
	if raw, ok := account.Extra[resetAtKey]; ok {
		if parsed, err := parseTime(fmt.Sprint(raw)); err == nil {
			resetAt = parsed.Unix()
		}
	}
	if resetAt == 0 {
		if resetAfter := parseExtraInt(account.Extra[resetAfterKey]); resetAfter > 0 {
			resetAt = fetchedAt.Add(time.Duration(resetAfter) * time.Second).Unix()
		}
	}
	if resetAt > 0 && !now.Before(time.Unix(resetAt, 0)) {
		usedPercent = 0
	}

	return &OpenAIRateLimitWindow{
		UsedPercent: usedPercent, LimitWindowSeconds: limitSeconds, ResetAt: resetAt,
		ResetAfterSeconds: maxInt64(resetAt-now.Unix(), 0),
	}, fetchedAt
}

func selectOfficialQuotaWindow(usage *OpenAIQuotaUsage, seconds int64) *OpenAIRateLimitWindow {
	if usage == nil || usage.RateLimit == nil || seconds <= 0 {
		return nil
	}
	candidates := []*OpenAIRateLimitWindow{usage.RateLimit.PrimaryWindow, usage.RateLimit.SecondaryWindow}
	for _, candidate := range candidates {
		if candidate != nil && absInt64(candidate.LimitWindowSeconds-seconds) <= 60 {
			return candidate
		}
	}
	return nil
}

func enabledMemberWeight(members []SharedQuotaPoolMember) float64 {
	total := 0.0
	for _, member := range members {
		if member.Enabled {
			total += member.Weight
		}
	}
	return total
}

func clampPercent(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Max(0, math.Min(100, value))
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func findPoolMember(members []SharedQuotaPoolMemberSnapshot, userID int64) *SharedQuotaPoolMemberSnapshot {
	for i := range members {
		if members[i].UserID == userID {
			return &members[i]
		}
	}
	return nil
}

func (s *SharedQuotaPoolService) invalidate(groupID int64) {
	if s == nil {
		return
	}
	s.cacheMu.Lock()
	delete(s.cache, groupID)
	s.cacheMu.Unlock()
}

func defaultSharedQuotaPoolConfig(groupID int64) SharedQuotaPoolConfig {
	now := time.Now()
	config := SharedQuotaPoolConfig{
		GroupID:          groupID,
		CapacityMode:     SharedQuotaCapacityModeUSD,
		WindowSeconds:    defaultSharedQuotaWindowSeconds,
		ReserveRatio:     defaultSharedQuotaReserveRatio,
		SoftStopRatio:    defaultSharedQuotaSoftStopRatio,
		HardStopRatio:    defaultSharedQuotaHardStopRatio,
		BorrowEnabled:    true,
		BorrowMultiplier: defaultSharedQuotaBorrowMultiple,
		WindowStart:      now,
		WindowEnd:        now.Add(defaultSharedQuotaWindowSeconds * time.Second),
	}
	shortCapacity := (*float64)(nil)
	longCapacity := (*float64)(nil)
	config.Windows = []SharedQuotaPoolWindowConfig{
		{Key: "short", Enabled: true, WindowSeconds: 5 * 60 * 60, CapacityUSD: shortCapacity,
			ReserveRatio: defaultSharedQuotaReserveRatio, SoftStopRatio: defaultSharedQuotaSoftStopRatio,
			HardStopRatio: defaultSharedQuotaHardStopRatio, WindowStart: now, WindowEnd: now.Add(5 * time.Hour)},
		{Key: "long", Enabled: true, WindowSeconds: defaultSharedQuotaWindowSeconds, CapacityUSD: longCapacity,
			ReserveRatio: defaultSharedQuotaReserveRatio, SoftStopRatio: defaultSharedQuotaSoftStopRatio,
			HardStopRatio: defaultSharedQuotaHardStopRatio, WindowStart: now, WindowEnd: now.Add(7 * 24 * time.Hour)},
	}
	return config
}

func normalizeSharedQuotaWindows(config *SharedQuotaPoolConfig, windows []SharedQuotaPoolWindowConfig) []SharedQuotaPoolWindowConfig {
	if len(windows) == 0 && config != nil && len(config.Windows) > 0 {
		windows = config.Windows
	}
	if len(windows) == 0 && config != nil && config.WindowSeconds > 0 {
		windows = []SharedQuotaPoolWindowConfig{legacyWindowFromPoolConfig(*config)}
	}
	now := time.Now()
	seen := make(map[string]struct{}, len(windows)+1)
	result := make([]SharedQuotaPoolWindowConfig, 0, len(windows)+1)
	for _, window := range windows {
		if window.Key == "" {
			window.Key = "long"
		}
		if _, ok := seen[window.Key]; ok {
			continue
		}
		seen[window.Key] = struct{}{}
		if window.WindowSeconds == 0 {
			if window.Key == "short" {
				window.WindowSeconds = 5 * 60 * 60
			} else {
				window.WindowSeconds = defaultSharedQuotaWindowSeconds
			}
		}
		if window.CapacityMode == "" {
			window.CapacityMode = SharedQuotaCapacityModeUSD
			if config != nil && config.CapacityMode != "" {
				window.CapacityMode = config.CapacityMode
			}
		}
		if window.UpstreamAccountID == nil && config != nil {
			window.UpstreamAccountID = config.UpstreamAccountID
		}
		if window.SoftStopRatio == 0 && config != nil && config.SoftStopRatio != 0 {
			window.SoftStopRatio = config.SoftStopRatio
		}
		if window.HardStopRatio == 0 && config != nil && config.HardStopRatio != 0 {
			window.HardStopRatio = config.HardStopRatio
		}
		if window.WindowStart.IsZero() {
			window.WindowStart = now
		}
		if window.WindowEnd.IsZero() {
			window.WindowEnd = window.WindowStart.Add(time.Duration(window.WindowSeconds) * time.Second)
		}
		result = append(result, window)
	}
	shortPresent := false
	for _, window := range result {
		if window.Key == "short" {
			shortPresent = true
			break
		}
	}
	if !shortPresent {
		shortWindow := SharedQuotaPoolWindowConfig{
			Key: "short", Enabled: false, WindowSeconds: 5 * 60 * 60,
			ReserveRatio: defaultSharedQuotaReserveRatio, SoftStopRatio: defaultSharedQuotaSoftStopRatio,
			HardStopRatio: defaultSharedQuotaHardStopRatio, WindowStart: now, WindowEnd: now.Add(5 * time.Hour),
			CapacityMode: SharedQuotaCapacityModeUSD,
		}
		result = append([]SharedQuotaPoolWindowConfig{shortWindow}, result...)
	}
	return result
}

func legacyWindowFromPoolConfig(config SharedQuotaPoolConfig) SharedQuotaPoolWindowConfig {
	return SharedQuotaPoolWindowConfig{Key: "long", Enabled: config.Enabled, WindowSeconds: config.WindowSeconds,
		CapacityUSD: config.CapacityUSD, ReserveRatio: config.ReserveRatio, SoftStopRatio: config.SoftStopRatio,
		HardStopRatio: config.HardStopRatio, UpstreamCapacityUSD: config.UpstreamCapacityUSD,
		UpstreamUtilizationPercent: config.UpstreamUtilizationPercent, WindowStart: config.WindowStart, WindowEnd: config.WindowEnd,
		CapacityMode: config.CapacityMode, UpstreamAccountID: config.UpstreamAccountID}
}

func validateSharedQuotaPoolConfig(config *SharedQuotaPoolConfig) error {
	if config.GroupID <= 0 || config.WindowSeconds < 3600 || config.WindowSeconds > 2678400 {
		return ErrSharedQuotaPoolInvalid
	}
	if config.CapacityMode != "" && config.CapacityMode != SharedQuotaCapacityModeUSD && config.CapacityMode != SharedQuotaCapacityModeOfficialPercent {
		return ErrSharedQuotaPoolInvalid
	}
	if config.UpstreamAccountID != nil && *config.UpstreamAccountID <= 0 {
		return ErrSharedQuotaPoolInvalid
	}
	if config.BorrowMultiplier < 1 || config.BorrowMultiplier > 10 || math.IsNaN(config.BorrowMultiplier) || math.IsInf(config.BorrowMultiplier, 0) {
		return ErrSharedQuotaPoolInvalid
	}
	enabledWindows := 0
	for _, window := range config.Windows {
		if !validSharedQuotaWindowKey(window.Key) || window.WindowSeconds < 300 || window.WindowSeconds > 2678400 {
			return ErrSharedQuotaPoolInvalid
		}
		if window.ReserveRatio < 0 || window.ReserveRatio >= 1 || window.SoftStopRatio <= 0 || window.SoftStopRatio > 1 || window.HardStopRatio <= 0 || window.HardStopRatio > 1 || window.SoftStopRatio > window.HardStopRatio {
			return ErrSharedQuotaPoolInvalid
		}
		if window.CapacityUSD != nil && (*window.CapacityUSD <= 0 || math.IsNaN(*window.CapacityUSD) || math.IsInf(*window.CapacityUSD, 0)) {
			return ErrSharedQuotaPoolInvalid
		}
		if window.CapacityMode == "" {
			window.CapacityMode = SharedQuotaCapacityModeUSD
		}
		if window.CapacityMode != SharedQuotaCapacityModeUSD && window.CapacityMode != SharedQuotaCapacityModeOfficialPercent {
			return ErrSharedQuotaPoolInvalid
		}
		if window.UpstreamAccountID != nil && *window.UpstreamAccountID <= 0 {
			return ErrSharedQuotaPoolInvalid
		}
		if window.WindowStart.IsZero() || window.WindowEnd.IsZero() || !window.WindowEnd.After(window.WindowStart) {
			return ErrSharedQuotaPoolInvalid
		}
		if window.UpstreamUtilizationPercent != nil && (*window.UpstreamUtilizationPercent < 0 || *window.UpstreamUtilizationPercent > 100) {
			return ErrSharedQuotaPoolInvalid
		}
		if config.Enabled && window.Enabled {
			if window.CapacityUSD == nil || *window.CapacityUSD <= 0 {
				if window.CapacityMode != SharedQuotaCapacityModeOfficialPercent {
					return ErrSharedQuotaPoolInvalid
				}
			}
			enabledWindows++
		}
	}
	if config.Enabled && enabledWindows == 0 {
		return ErrSharedQuotaPoolInvalid
	}
	return nil
}

func validSharedQuotaWindowKey(key string) bool {
	if len(key) == 0 || len(key) > 32 || key[0] < 'a' || key[0] > 'z' {
		return false
	}
	for i := 1; i < len(key); i++ {
		if (key[i] < 'a' || key[i] > 'z') && (key[i] < '0' || key[i] > '9') && key[i] != '_' && key[i] != '-' {
			return false
		}
	}
	return true
}

func validateSharedQuotaPoolMembers(members []SharedQuotaPoolMemberInput) error {
	seen := make(map[int64]struct{}, len(members))
	for _, member := range members {
		if member.UserID <= 0 || member.Weight <= 0 || member.Weight > 100000 || math.IsNaN(member.Weight) || math.IsInf(member.Weight, 0) {
			return ErrSharedQuotaPoolInvalid
		}
		if _, ok := seen[member.UserID]; ok {
			return ErrSharedQuotaPoolInvalid
		}
		seen[member.UserID] = struct{}{}
	}
	return nil
}

// CheckSubscriptionPool is the single integration point used by subscription
// admission. Optional pool failures fail open and are logged; the legacy group
// limit remains active in that case.
func (s *SharedQuotaPoolService) CheckSubscriptionPool(ctx context.Context, groupID, userID int64) (*SharedQuotaPoolDecision, error) {
	decision, err := s.Check(ctx, groupID, userID)
	if err != nil {
		log.Printf("shared quota pool check failed group=%d user=%d: %v", groupID, userID, err)
		return nil, nil
	}
	return decision, nil
}
