package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// CityRealtimeHostClockAuthority is the deliberately narrow production
// adapter for a host clock maintained by an operator-managed NTP/NTS service.
// It is disabled unless both enabled and trust_host_clock are explicitly set.
//
// The host-clock attestation is not treated as a browser/manual clock: every
// observation is generated in-process, checked against monotonic elapsed time
// here, and checked again against PostgreSQL time in the temporal reducer.
// A network/private-time-service adapter must be implemented as a distinct
// authority instead of widening this one with an operator-configured URL.
type CityRealtimeHostClockAuthority struct {
	enabled                bool
	nodeID                 string
	sourceClockMode        string
	uncertaintyUS          int64
	maximumWallClockStepUS int64

	wallNow          func() time.Time
	monotonicElapsed func() time.Duration

	mu               sync.Mutex
	initialized      bool
	wallAnchorUTC    time.Time
	elapsedAnchor    time.Duration
	lastEffectiveUTC time.Time
}

func NewCityRealtimeHostClockAuthority(cfg *config.Config) *CityRealtimeHostClockAuthority {
	clockConfig := config.CityRealtimeClockConfig{}
	if cfg != nil {
		clockConfig = cfg.CityRealtimeClock
	}
	processStartedAt := time.Now()
	return newCityRealtimeHostClockAuthority(
		clockConfig,
		time.Now,
		func() time.Duration { return time.Since(processStartedAt) },
	)
}

func newCityRealtimeHostClockAuthority(
	clockConfig config.CityRealtimeClockConfig,
	wallNow func() time.Time,
	monotonicElapsed func() time.Duration,
) *CityRealtimeHostClockAuthority {
	if wallNow == nil {
		wallNow = time.Now
	}
	if monotonicElapsed == nil {
		processStartedAt := time.Now()
		monotonicElapsed = func() time.Duration { return time.Since(processStartedAt) }
	}
	ready := clockConfig.Enabled && clockConfig.TrustHostClock &&
		cityRealtimeClockNodeIDValid(clockConfig.NodeID) &&
		(clockConfig.SourceClockMode == "system_ntp" || clockConfig.SourceClockMode == "system_nts") &&
		clockConfig.UncertaintyUS >= 0 && clockConfig.UncertaintyUS <= 60_000_000 &&
		clockConfig.MaximumWallClockStepUS >= 1_000 && clockConfig.MaximumWallClockStepUS <= 60_000_000
	return &CityRealtimeHostClockAuthority{
		enabled:                ready,
		nodeID:                 clockConfig.NodeID,
		sourceClockMode:        clockConfig.SourceClockMode,
		uncertaintyUS:          clockConfig.UncertaintyUS,
		maximumWallClockStepUS: clockConfig.MaximumWallClockStepUS,
		wallNow:                wallNow,
		monotonicElapsed:       monotonicElapsed,
	}
}

// IsOperational reports whether this process has an explicitly trusted host
// clock adapter. The database setting remains a separate runtime gate for the
// scheduler; this method never enables work by itself.
func (a *CityRealtimeHostClockAuthority) IsOperational() bool {
	return a != nil && a.enabled
}

// ProductionProfileID returns the only immutable profile this attested host
// may use when an administrator creates a realtime world. It keeps profile
// selection server-owned and refuses to create a world when the clock adapter
// has not been explicitly enabled and trusted.
func (a *CityRealtimeHostClockAuthority) ProductionProfileID() (string, bool) {
	if !a.IsOperational() {
		return "", false
	}
	return cityRealtimeProductionClockProfileID(a.sourceClockMode)
}

// Supports lets the scheduler leave production profiles intended for a
// different authority untouched. An unavailable or mismatched adapter is not
// a clock failure for that other profile and must not transition it to unsafe.
func (a *CityRealtimeHostClockAuthority) Supports(profile CityRealtimeClockProfile) bool {
	return a != nil && a.enabled &&
		profile.DeploymentScope == "production" &&
		profile.SourceClockMode == a.sourceClockMode &&
		profile.TimeQuantumUS == cityRealtimeTimeQuantumUS &&
		a.uncertaintyUS <= profile.MaximumUncertaintyUS
}

func (a *CityRealtimeHostClockAuthority) Observe(
	ctx context.Context,
	profile CityRealtimeClockProfile,
) (CityRealtimeClockObservation, error) {
	if err := ctx.Err(); err != nil {
		return CityRealtimeClockObservation{}, err
	}
	if !a.Supports(profile) {
		return CityRealtimeClockObservation{}, ErrCityRealtimeClockUnsafe
	}
	observedUTC := a.wallNow().UTC().Truncate(time.Microsecond)
	elapsed := a.monotonicElapsed()
	if observedUTC.IsZero() || elapsed < 0 {
		return CityRealtimeClockObservation{}, ErrCityRealtimeClockUnsafe
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.initialized {
		a.initialized = true
		a.wallAnchorUTC = observedUTC
		a.elapsedAnchor = elapsed
		a.lastEffectiveUTC = observedUTC
	} else {
		expectedUTC := a.wallAnchorUTC.Add(elapsed - a.elapsedAnchor).UTC().Truncate(time.Microsecond)
		if observedUTC.Before(a.lastEffectiveUTC) ||
			cityRealtimeDurationAbs(observedUTC.Sub(expectedUTC)) > time.Duration(a.maximumWallClockStepUS)*time.Microsecond {
			return CityRealtimeClockObservation{}, ErrCityRealtimeClockUnsafe
		}
		a.lastEffectiveUTC = observedUTC
	}
	return CityRealtimeClockObservation{
		NodeID:          a.nodeID,
		SourceClockMode: a.sourceClockMode,
		HealthState:     cityRealtimeClockStateHealthy,
		EffectiveUTC:    observedUTC,
		UncertaintyUS:   a.uncertaintyUS,
	}, nil
}

func cityRealtimeDurationAbs(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
