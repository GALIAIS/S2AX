package service

import (
	"context"
	"fmt"
)

// GetCityOpenWorldEffectiveCapacityState exposes V21's route-admission audit
// projection to world members. It intentionally exposes corridor identity and
// aggregate capacity evidence only; it contains no credentials, private user
// data, or administrative mutation controls.
func (s *CityEconomyService) GetCityOpenWorldEffectiveCapacityState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldEffectiveCapacityState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V21 effective-capacity world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldEffectiveCapacity(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	return loadCityOpenWorldEffectiveCapacityState(ctx, s.db, worldID)
}
