package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CityRealtimeLifecycleController is the sole HTTP-facing bridge for
// production pause/resume. It obtains its observation from a server-owned
// authority after loading the immutable world profile; handlers never accept
// timestamps, source modes, node IDs, or uncertainty values from clients.
type CityRealtimeLifecycleController struct {
	economy   *CityEconomyService
	authority CityRealtimeClockAuthority
}

func NewCityRealtimeLifecycleController(
	economy *CityEconomyService,
	authority *CityRealtimeHostClockAuthority,
) *CityRealtimeLifecycleController {
	return &CityRealtimeLifecycleController{
		economy:   economy,
		authority: authority,
	}
}

// CreateRealtimeWorld is the sole HTTP-facing creation path for a shared
// realtime pixel world. The browser may opt into realtime mode, but it never
// supplies an engine version, time source, profile ID, timestamp, or clock
// tolerance. Those values are derived from the configured host authority and
// verified again by CityEconomyService in the same creation transaction.
func (c *CityRealtimeLifecycleController) CreateRealtimeWorld(
	ctx context.Context,
	input CityWorldCreateInput,
) (*CityWorldFoundation, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if c == nil || c.economy == nil || c.authority == nil {
		return nil, ErrCityRealtimeClockUnsafe
	}
	profileProvider, ok := c.authority.(interface {
		ProductionProfileID() (string, bool)
	})
	if !ok {
		return nil, ErrCityRealtimeClockUnsafe
	}
	profileID, ok := profileProvider.ProductionProfileID()
	if !ok {
		return nil, ErrCityRealtimeClockUnsafe
	}
	input.SimulationVersion = CitySimulationVersionRealtimeV2
	input.ClockProfileID = profileID
	return c.economy.CreateWorld(ctx, input)
}

// GetRealtimeClock returns the persisted member-safe clock and, for a running
// production world, a bounded no-write projection derived from the trusted
// server clock. An unavailable authority never turns a normal map read into a
// privileged clock error; callers retain the last committed time with
// LiveProjection=false until a scheduler/lifecycle operation can recover it.
func (c *CityRealtimeLifecycleController) GetRealtimeClock(
	ctx context.Context,
	userID, worldID int64,
) (*CityRealtimeClock, error) {
	if c == nil || c.economy == nil {
		return nil, ErrCityRealtimeClockUnsafe
	}
	clock, err := c.economy.GetRealtimeClock(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	if c.authority == nil ||
		(clock.WorldTime.SourceClockMode != "system_ntp" && clock.WorldTime.SourceClockMode != "system_nts") ||
		(clock.WorldTime.ClockState != cityRealtimeClockStateInitializing && clock.WorldTime.ClockState != cityRealtimeClockStateHealthy) {
		return clock, nil
	}
	internalCtx := WithCitySystemAdministrator(ctx)
	profile, err := c.economy.loadRealtimeProductionClockProfile(internalCtx, worldID)
	if err != nil {
		return clock, nil
	}
	if supported, ok := c.authority.(interface {
		Supports(CityRealtimeClockProfile) bool
	}); ok && !supported.Supports(profile) {
		return clock, nil
	}
	observation, err := c.authority.Observe(internalCtx, profile)
	if err != nil {
		return clock, nil
	}
	if err = projectCityRealtimeClockAtObservation(clock, observation); err != nil {
		return clock, nil
	}
	return clock, nil
}

func (c *CityRealtimeLifecycleController) Pause(
	ctx context.Context,
	worldID int64,
) (*CityRealtimeLifecycleResult, error) {
	observation, err := c.observeProductionWorld(ctx, worldID)
	if err != nil {
		return nil, err
	}
	return c.economy.PauseRealtimeWorld(ctx, CityRealtimeLifecycleInput{
		WorldID:     worldID,
		Observation: observation,
	})
}

func (c *CityRealtimeLifecycleController) Resume(
	ctx context.Context,
	worldID int64,
) (*CityRealtimeLifecycleResult, error) {
	observation, err := c.observeProductionWorld(ctx, worldID)
	if err != nil {
		return nil, err
	}
	return c.economy.ResumeRealtimeWorld(ctx, CityRealtimeLifecycleInput{
		WorldID:     worldID,
		Observation: observation,
	})
}

func (c *CityRealtimeLifecycleController) observeProductionWorld(
	ctx context.Context,
	worldID int64,
) (CityRealtimeClockObservation, error) {
	if !IsCitySystemAdministrator(ctx) {
		return CityRealtimeClockObservation{}, ErrCityManagementRequired
	}
	if c == nil || c.economy == nil || c.authority == nil || worldID <= 0 {
		return CityRealtimeClockObservation{}, ErrCityRealtimeClockUnsafe
	}
	profile, err := c.economy.loadRealtimeProductionClockProfile(ctx, worldID)
	if err != nil {
		return CityRealtimeClockObservation{}, err
	}
	if supported, ok := c.authority.(interface {
		Supports(CityRealtimeClockProfile) bool
	}); ok && !supported.Supports(profile) {
		return CityRealtimeClockObservation{}, ErrCityRealtimeClockUnsafe
	}
	observation, err := c.authority.Observe(ctx, profile)
	if err != nil {
		return CityRealtimeClockObservation{}, err
	}
	return observation, nil
}

func (s *CityEconomyService) loadRealtimeProductionClockProfile(
	ctx context.Context,
	worldID int64,
) (CityRealtimeClockProfile, error) {
	if !IsCitySystemAdministrator(ctx) {
		return CityRealtimeClockProfile{}, ErrCityManagementRequired
	}
	if s == nil || s.db == nil || worldID <= 0 {
		return CityRealtimeClockProfile{}, ErrCityInvalidInput
	}
	var version string
	profile := CityRealtimeClockProfile{}
	err := s.db.QueryRowContext(ctx, `
SELECT world.simulation_version,
       state.clock_profile_id, state.clock_profile_hash,
       profile.source_clock_mode, profile.deployment_scope, profile.quantum_us,
       profile.maximum_uncertainty_us, profile.maximum_database_skew_us
FROM city_worlds world
JOIN city_world_time_states state ON state.world_id = world.id
JOIN city_clock_profiles profile ON profile.id = state.clock_profile_id
WHERE world.id = $1`, worldID).Scan(
		&version, &profile.ID, &profile.Hash, &profile.SourceClockMode,
		&profile.DeploymentScope, &profile.TimeQuantumUS,
		&profile.MaximumUncertaintyUS, &profile.MaximumDatabaseSkewUS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		var existingVersion string
		existsErr := s.db.QueryRowContext(ctx, `
SELECT simulation_version
FROM city_worlds
WHERE id = $1`, worldID).Scan(&existingVersion)
		if errors.Is(existsErr, sql.ErrNoRows) {
			return CityRealtimeClockProfile{}, ErrCityWorldNotFound
		}
		if existsErr != nil {
			return CityRealtimeClockProfile{}, fmt.Errorf("check city realtime world: %w", existsErr)
		}
		if cityEngineIsRealtime(existingVersion) {
			return CityRealtimeClockProfile{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_time_state"})
		}
		return CityRealtimeClockProfile{}, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": existingVersion})
	}
	if err != nil {
		return CityRealtimeClockProfile{}, fmt.Errorf("load city realtime production clock profile: %w", err)
	}
	if !cityEngineIsRealtime(version) {
		return CityRealtimeClockProfile{}, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": version})
	}
	if profile.DeploymentScope != "production" ||
		(profile.SourceClockMode != "system_ntp" &&
			profile.SourceClockMode != "system_nts" &&
			profile.SourceClockMode != "private_time_service") ||
		profile.TimeQuantumUS != cityRealtimeTimeQuantumUS ||
		!cityRealtimeSHA256Hex(profile.Hash) ||
		profile.MaximumUncertaintyUS < 0 || profile.MaximumDatabaseSkewUS < 0 {
		return CityRealtimeClockProfile{}, ErrCityRealtimeClockUnsafe
	}
	return profile, nil
}
