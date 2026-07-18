package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCityPopulationMigrationCommandCanonicalizesScopesAndBoundsUnits(t *testing.T) {
	payload, handled, err := normalizeCityPopulationMigrationCommand(
		CityCommandTypePopulationRelocate,
		json.RawMessage(`{"source_district_code":" CENTRAL ","target_district_code":" NORTH ","income_band":" LOW ","child_units":1,"working_units":2,"senior_units":3,"reason":"  housing  "}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, cityPopulationRelocationPayload{
		SourceDistrictCode: "central", TargetDistrictCode: "north", IncomeBand: "low",
		ChildUnits: 1, WorkingUnits: 2, SeniorUnits: 3, Reason: "housing",
	}, payload)

	_, handled, err = normalizeCityPopulationMigrationCommand(
		CityCommandTypePopulationRelocate,
		json.RawMessage(`{"source_district_code":"central","target_district_code":"central","income_band":"low","child_units":1,"working_units":0,"senior_units":0}`),
	)
	require.True(t, handled)
	require.ErrorIs(t, err, ErrCityInvalidInput)

	_, handled, err = normalizeCityPopulationMigrationCommand(
		CityCommandTypePopulationImmigrate,
		json.RawMessage(`{"target_district_code":"central","income_band":"low","child_units":0,"working_units":0,"senior_units":0}`),
	)
	require.True(t, handled)
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestCalculateCityPopulationMigrationPlanProtectsEmploymentHousingAndOverflow(t *testing.T) {
	ref := cityDemographicCohortRef{
		childUnits: 10, workingUnits: 20, seniorUnits: 5,
		employedUnits: 15, housingDemandUnits: 30,
	}
	plan, err := calculateCityPopulationMigrationPlan(
		ref, cityPopulationMigrationDirectionOutflow,
		cityPopulationUnits{child: 1, working: 2, senior: 1},
	)
	require.NoError(t, err)
	require.Equal(t, int64(9), plan.childAfter)
	require.Equal(t, int64(18), plan.workingAfter)
	require.Equal(t, int64(4), plan.seniorAfter)

	_, err = calculateCityPopulationMigrationPlan(
		ref, cityPopulationMigrationDirectionOutflow,
		cityPopulationUnits{working: 6},
	)
	var businessErr *cityPopulationMigrationBusinessError
	require.ErrorAs(t, err, &businessErr)
	require.Equal(t, cityPopulationMigrationRejectionEmployment, businessErr.code)

	_, err = calculateCityPopulationMigrationPlan(
		ref, cityPopulationMigrationDirectionOutflow,
		cityPopulationUnits{child: 6},
	)
	require.ErrorAs(t, err, &businessErr)
	require.Equal(t, cityPopulationMigrationRejectionHousing, businessErr.code)

	_, err = calculateCityPopulationMigrationPlan(
		cityDemographicCohortRef{childUnits: int64(^uint64(0) >> 1), workingUnits: 1, seniorUnits: 1},
		cityPopulationMigrationDirectionInflow,
		cityPopulationUnits{child: 1},
	)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func TestReplayCityPopulationRelocationConservesEveryAgeBucket(t *testing.T) {
	postedAt := time.Now().UTC()
	state := cityHashState{
		SimulationVersion: CitySimulationVersionF6V2,
		Demography: cityDemographyHashState{Cohorts: []cityDemographyHashCohort{
			{DistrictCode: "central", EntityCode: "households", IncomeBand: "low", ChildUnits: 10, WorkingUnits: 20, SeniorUnits: 5},
			{DistrictCode: "north", EntityCode: "households", IncomeBand: "low", ChildUnits: 7, WorkingUnits: 15, SeniorUnits: 3},
		}},
		Physical: cityPhysicalHashState{HouseholdCohorts: []cityHashHouseholdCohort{
			{DistrictCode: "central", EntityCode: "households", IncomeBand: "low", PopulationUnits: 35, WorkingAgeUnits: 20, EmployedUnits: 15, HousingDemandUnits: 30},
			{DistrictCode: "north", EntityCode: "households", IncomeBand: "low", PopulationUnits: 25, WorkingAgeUnits: 15, EmployedUnits: 8, HousingDemandUnits: 20},
		}},
	}
	sourceDistrict, targetDistrict := "central", "north"
	entity, band := "households", "low"
	migration := &CityPopulationMigration{
		ID: 1, WorldID: 7, Tick: 1, Sequence: 1, SourceCommandID: 11,
		MigrationType:      CityPopulationMigrationDistrictRelocation,
		SourceDistrictCode: &sourceDistrict, SourceEntityCode: &entity, SourceIncomeBand: &band,
		TargetDistrictCode: &targetDistrict, TargetEntityCode: &entity, TargetIncomeBand: &band,
		ChildUnits: 1, WorkingUnits: 2, SeniorUnits: 1,
		ExpectedLineCount: 2, PostedAt: &postedAt,
		Lines: []*CityPopulationMigrationLine{
			{
				MigrationID: 1, WorldID: 7, LineNo: 1, DistrictCode: "central", EntityCode: "households", IncomeBand: "low",
				Direction:                cityPopulationMigrationDirectionOutflow,
				DemographicVersionBefore: 0, DemographicVersionAfter: 1, CohortVersionBefore: 0, CohortVersionAfter: 1,
				ChildUnits: 1, WorkingUnits: 2, SeniorUnits: 1,
				ChildUnitsBefore: 10, WorkingUnitsBefore: 20, SeniorUnitsBefore: 5,
				ChildUnitsAfter: 9, WorkingUnitsAfter: 18, SeniorUnitsAfter: 4,
			},
			{
				MigrationID: 1, WorldID: 7, LineNo: 2, DistrictCode: "north", EntityCode: "households", IncomeBand: "low",
				Direction:                cityPopulationMigrationDirectionInflow,
				DemographicVersionBefore: 0, DemographicVersionAfter: 1, CohortVersionBefore: 0, CohortVersionAfter: 1,
				ChildUnits: 1, WorkingUnits: 2, SeniorUnits: 1,
				ChildUnitsBefore: 7, WorkingUnitsBefore: 15, SeniorUnitsBefore: 3,
				ChildUnitsAfter: 8, WorkingUnitsAfter: 17, SeniorUnitsAfter: 4,
			},
		},
	}

	require.NoError(t, replayCityPopulationMigration(migration, &state))
	require.Equal(t, int64(17), state.Demography.Cohorts[0].ChildUnits+state.Demography.Cohorts[1].ChildUnits)
	require.Equal(t, int64(35), state.Demography.Cohorts[0].WorkingUnits+state.Demography.Cohorts[1].WorkingUnits)
	require.Equal(t, int64(8), state.Demography.Cohorts[0].SeniorUnits+state.Demography.Cohorts[1].SeniorUnits)
	require.Equal(t, int64(31), state.Physical.HouseholdCohorts[0].PopulationUnits)
	require.Equal(t, int64(29), state.Physical.HouseholdCohorts[1].PopulationUnits)
}
