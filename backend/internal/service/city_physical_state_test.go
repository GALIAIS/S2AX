package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityPhysicalStateJSONKeepsHouseholdSemanticsVersionExplicit(t *testing.T) {
	t.Parallel()
	legacy, err := json.Marshal(CityPhysicalState{
		WorldID: 1, SimulationVersion: CitySimulationVersionF6V2,
		HouseholdCohorts: []*CityHouseholdCohort{{
			PopulationUnits: 12, HousingDemandUnits: 9,
		}},
	})
	require.NoError(t, err)
	require.Contains(t, string(legacy), `"simulation_version":"city-f6-v2"`)
	require.NotContains(t, string(legacy), `"household_units"`)
	require.NotContains(t, string(legacy), `"average_household_size_milli"`)

	current, err := json.Marshal(CityPhysicalState{
		WorldID: 1, SimulationVersion: CitySimulationVersionF6V3,
		HouseholdCohorts: []*CityHouseholdCohort{{
			PopulationUnits: 12, HouseholdUnits: 4,
			AverageHouseholdSizeMilli: 3_000, HousingDemandUnits: 4,
		}},
	})
	require.NoError(t, err)
	require.Contains(t, string(current), `"household_units":4`)
	require.Contains(t, string(current), `"average_household_size_milli":3000`)
}
