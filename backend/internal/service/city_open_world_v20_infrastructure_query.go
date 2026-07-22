package service

import (
	"context"
	"fmt"
)

// GetCityOpenWorldInfrastructureState returns V20's public operational
// infrastructure descriptor to a world member. It exposes asset identity,
// aggregate state, and transition evidence only; it contains no account,
// credential, inventory, or administrative control data.
func (s *CityEconomyService) GetCityOpenWorldInfrastructureState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldInfrastructureState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V20 infrastructure world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldInfrastructure(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	return loadCityOpenWorldInfrastructureState(ctx, s.db, worldID)
}
