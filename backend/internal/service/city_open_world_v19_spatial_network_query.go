package service

import (
	"context"
	"fmt"
)

// GetCityOpenWorldSpatialNetworkState returns the public static transport
// topology for a world member. V19 contains no account, wallet, control-grant,
// or bilateral order data, so the entire descriptor can be shared without the
// V15/V16 private-data projection rules.
func (s *CityEconomyService) GetCityOpenWorldSpatialNetworkState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldSpatialNetworkState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V19 spatial-network world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldSpatialNetwork(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	return loadCityOpenWorldSpatialNetworkState(ctx, s.db, worldID)
}
