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

	WindowStart time.Time                     `json:"window_start"`
	WindowEnd   time.Time                     `json:"window_end"`
	UpdatedAt   time.Time                     `json:"updated_at"`
	Windows     []SharedQuotaPoolWindowConfig `json:"windows"`
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
	UsedUSD        float64 `json:"used_usd"`
	BaseShareUSD   float64 `json:"base_share_usd"`
	MaximumUSD     float64 `json:"maximum_usd"`
	RemainingUSD   float64 `json:"remaining_usd"`
	BorrowedUSD    float64 `json:"borrowed_usd"`
	SharePercent   float64 `json:"share_percent"`
	Allowed        bool    `json:"allowed"`
	DecisionReason string  `json:"decision_reason,omitempty"`
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
}

type SharedQuotaPoolWindowSnapshot struct {
	Config               SharedQuotaPoolWindowConfig     `json:"config"`
	BaseCapacityUSD      float64                         `json:"base_capacity_usd"`
	DistributableUSD     float64                         `json:"distributable_usd"`
	EstimatedCapacityUSD float64                         `json:"estimated_capacity_usd,omitempty"`
	TotalUsedUSD         float64                         `json:"total_used_usd"`
	RemainingUSD         float64                         `json:"remaining_usd"`
	UtilizationPercent   float64                         `json:"utilization_percent"`
	SoftLimitUSD         float64                         `json:"soft_limit_usd"`
	HardLimitUSD         float64                         `json:"hard_limit_usd"`
	SoftStopReached      bool                            `json:"soft_stop_reached"`
	HardStopReached      bool                            `json:"hard_stop_reached"`
	Members              []SharedQuotaPoolMemberSnapshot `json:"members"`
}

type SharedQuotaPoolDecision struct {
	Enabled      bool
	Allowed      bool
	Reason       string
	BaseShareUSD float64
	MaximumUSD   float64
	UsedUSD      float64
	BorrowedUSD  float64
	SharePercent float64
	Snapshot     *SharedQuotaPoolSnapshot
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

	cacheMu sync.Mutex
	cache   map[int64]sharedQuotaSnapshotCacheEntry
	flight  singleflight.Group
}

func NewSharedQuotaPoolService(repo SharedQuotaPoolRepository) *SharedQuotaPoolService {
	return &SharedQuotaPoolService{
		repo:  repo,
		now:   time.Now,
		cache: make(map[int64]sharedQuotaSnapshotCacheEntry),
	}
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
		item := SharedQuotaUserWindowProgress{
			Key: window.Config.Key, WindowSeconds: window.Config.WindowSeconds,
			PoolTotalUsedUSD: window.TotalUsedUSD, PoolDistributableUSD: window.DistributableUSD,
			PoolUtilizationPercent: window.UtilizationPercent, SoftStopReached: window.SoftStopReached,
			HardStopReached: window.HardStopReached, Allowed: false,
			WindowStart: window.Config.WindowStart, WindowEnd: window.Config.WindowEnd,
		}
		if member != nil {
			item.BaseShareUSD = member.BaseShareUSD
			item.MaximumUSD = member.MaximumUSD
			item.UsedUSD = member.UsedUSD
			item.RemainingUSD = member.RemainingUSD
			item.BorrowedUSD = member.BorrowedUSD
			item.SharePercent = member.SharePercent
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
	return snapshot, nil
}

func (s *SharedQuotaPoolService) calculateWindowSnapshot(ctx context.Context, groupID int64, pool SharedQuotaPoolConfig, window SharedQuotaPoolWindowConfig, members []SharedQuotaPoolMember) (*SharedQuotaPoolWindowSnapshot, error) {
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
		Members: make([]SharedQuotaPoolMemberSnapshot, 0, len(members)),
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
		}
		result = append([]SharedQuotaPoolWindowConfig{shortWindow}, result...)
	}
	return result
}

func legacyWindowFromPoolConfig(config SharedQuotaPoolConfig) SharedQuotaPoolWindowConfig {
	return SharedQuotaPoolWindowConfig{Key: "long", Enabled: config.Enabled, WindowSeconds: config.WindowSeconds,
		CapacityUSD: config.CapacityUSD, ReserveRatio: config.ReserveRatio, SoftStopRatio: config.SoftStopRatio,
		HardStopRatio: config.HardStopRatio, UpstreamCapacityUSD: config.UpstreamCapacityUSD,
		UpstreamUtilizationPercent: config.UpstreamUtilizationPercent, WindowStart: config.WindowStart, WindowEnd: config.WindowEnd}
}

func validateSharedQuotaPoolConfig(config *SharedQuotaPoolConfig) error {
	if config.GroupID <= 0 || config.WindowSeconds < 3600 || config.WindowSeconds > 2678400 {
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
		if window.WindowStart.IsZero() || window.WindowEnd.IsZero() || !window.WindowEnd.After(window.WindowStart) {
			return ErrSharedQuotaPoolInvalid
		}
		if window.UpstreamUtilizationPercent != nil && (*window.UpstreamUtilizationPercent < 0 || *window.UpstreamUtilizationPercent > 100) {
			return ErrSharedQuotaPoolInvalid
		}
		if config.Enabled && window.Enabled {
			if window.CapacityUSD == nil || *window.CapacityUSD <= 0 {
				return ErrSharedQuotaPoolInvalid
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
