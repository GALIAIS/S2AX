package service

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCityRateAccrualCarriesRemaindersWithoutFloatingPoint(t *testing.T) {
	var total, remainder int64
	for range 12 {
		units, nextRemainder, err := cityRateAccrual(10_000, 12_000, remainder, 12)
		require.NoError(t, err)
		total += units
		remainder = nextRemainder
	}
	require.Equal(t, int64(120), total)
	require.Zero(t, remainder)
}

func TestCityDemographicPlanPreservesPopulationEquationForTenYears(t *testing.T) {
	policy := cityDemographyHashPolicy{
		ParameterSetCode: "baseline_v1", ParameterVersion: 1,
		PeriodsPerYear: 12, BirthRatePPM: 12_000,
		ChildDeathRatePPM: 500, WorkingDeathRatePPM: 1_000,
		SeniorDeathRatePPM: 12_000, ChildToWorkingRatePPM: 55_000,
		WorkingToSeniorRatePPM: 22_000,
	}
	ref := cityDemographicCohortRef{
		childUnits: 2_000, workingUnits: 6_500, seniorUnits: 1_500,
		employedUnits: 5_000, housingDemandUnits: 9_000,
	}
	initialPopulation := ref.childUnits + ref.workingUnits + ref.seniorUnits
	var cumulativeBirths, cumulativeDeaths int64
	for month := 0; month < 120; month++ {
		plan, err := calculateCityDemographicPlan(ref, policy)
		require.NoErrorf(t, err, "month %d", month+1)
		cumulativeBirths += plan.birthUnits
		cumulativeDeaths += plan.childDeathUnits + plan.workingDeathUnits + plan.seniorDeathUnits
		population := plan.childAfter + plan.workingAfter + plan.seniorAfter
		require.Equal(t, initialPopulation+cumulativeBirths-cumulativeDeaths, population)
		require.GreaterOrEqual(t, plan.workingAfter, ref.employedUnits)
		require.GreaterOrEqual(t, population, ref.housingDemandUnits)
		ref.childUnits = plan.childAfter
		ref.workingUnits = plan.workingAfter
		ref.seniorUnits = plan.seniorAfter
		ref.remainders = plan.afterRemainders
		ref.demographicVersion++
		ref.cohortVersion++
	}
	require.Positive(t, cumulativeBirths)
	require.Positive(t, cumulativeDeaths)
}

func TestCityDemographicPlanRejectsEmploymentAboveWorkingPopulation(t *testing.T) {
	_, err := calculateCityDemographicPlan(cityDemographicCohortRef{
		childUnits: 10, workingUnits: 20, seniorUnits: 10,
		employedUnits: 21, housingDemandUnits: 30,
	}, cityDemographyHashPolicy{PeriodsPerYear: 12})
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func TestCityDemographicPlanDefersOnlyV3HouseholdFloorToAuditedReconciliation(t *testing.T) {
	policy := cityDemographyHashPolicy{PeriodsPerYear: 12, WorkingDeathRatePPM: int(cityDemographicRateScale)}
	legacy := cityDemographicCohortRef{
		workingUnits: 10, housingDemandUnits: 10,
		remainders: cityDemographicRemainders{
			workingDeath: 12*cityDemographicRateScale - 1,
		},
	}
	_, err := calculateCityDemographicPlan(legacy, policy)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)

	legacy.allowsHouseholdReconciliation = true
	plan, err := calculateCityDemographicPlan(legacy, policy)
	require.NoError(t, err)
	require.Equal(t, int64(9), plan.workingAfter)
	require.Less(t, plan.workingAfter, legacy.housingDemandUnits)
}

func TestReplayCityPopulationMovementValidatesEveryCohortEquation(t *testing.T) {
	postedAt := time.Now().UTC()
	state := cityHashState{
		Demography: cityDemographyHashState{
			Policy: cityDemographyHashPolicy{
				ParameterSetCode: "baseline_v1", ParameterVersion: 1, PeriodsPerYear: 12,
			},
			Cohorts: []cityDemographyHashCohort{{
				DistrictCode: "central", EntityCode: "households", IncomeBand: "low",
				ChildUnits: 10, WorkingUnits: 20, SeniorUnits: 5,
			}},
		},
		Physical: cityPhysicalHashState{HouseholdCohorts: []cityHashHouseholdCohort{{
			DistrictCode: "central", EntityCode: "households", IncomeBand: "low",
			PopulationUnits: 35, WorkingAgeUnits: 20, EmployedUnits: 15,
			HousingDemandUnits: 30,
		}}},
	}
	line := &CityPopulationMovementLine{
		MovementID: 1, WorldID: 7, LineNo: 1,
		DistrictCode: "central", EntityCode: "households", IncomeBand: "low",
		DemographicVersionBefore: 0, DemographicVersionAfter: 1,
		CohortVersionBefore: 0, CohortVersionAfter: 1,
		BirthUnits: 0, ChildDeathUnits: 0, WorkingDeathUnits: 0,
		SeniorDeathUnits: 0, ChildToWorkingUnits: 0, WorkingToSeniorUnits: 0,
		ChildUnitsBefore: 10, WorkingUnitsBefore: 20, SeniorUnitsBefore: 5,
		ChildUnitsAfter: 11, WorkingUnitsAfter: 20, SeniorUnitsAfter: 5,
	}
	movement := &CityPopulationMovement{
		ID: 1, WorldID: 7, Tick: 1, Sequence: 1,
		MovementType:     CityPopulationMovementNaturalChange,
		ParameterSetCode: "baseline_v1", ParameterVersion: 1,
		ExpectedLineCount: 1, TotalBirthUnits: 0, TotalDeathUnits: 0,
		TotalTransitionUnits: 0, PostedAt: &postedAt,
		Lines: []*CityPopulationMovementLine{line},
	}

	err := replayCityPopulationMovement(movement, &state)
	require.ErrorContains(t, err, "versioned demographic policy")

	line.ChildUnitsAfter = 10
	require.NoError(t, replayCityPopulationMovement(movement, &state))
	require.Equal(t, int64(35), state.Physical.HouseholdCohorts[0].PopulationUnits)
	require.Equal(t, int64(20), state.Physical.HouseholdCohorts[0].WorkingAgeUnits)
}

func TestReplayCityPopulationAfterRejectsOutflowAndOverflow(t *testing.T) {
	_, err := replayCityPopulationAfter(10, 0, 11)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
	_, err = replayCityPopulationAfter(math.MaxInt64, 1)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func TestResolveCityCalendarTransitionHandlesTimezoneDSTAndLeapBoundaries(t *testing.T) {
	tests := []struct {
		name          string
		from          time.Time
		to            time.Time
		timezone      string
		fromDate      string
		toDate        string
		day, month    bool
		quarter, year bool
	}{
		{
			name: "UTC leap day", from: time.Date(2024, 2, 28, 23, 0, 0, 0, time.UTC),
			to: time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC), timezone: "UTC",
			fromDate: "2024-02-28", toDate: "2024-02-29", day: true,
		},
		{
			name: "quarter boundary", from: time.Date(2024, 3, 31, 23, 0, 0, 0, time.UTC),
			to: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC), timezone: "UTC",
			fromDate: "2024-03-31", toDate: "2024-04-01", day: true, month: true, quarter: true,
		},
		{
			name: "Shanghai new year", from: time.Date(2000, 12, 31, 15, 0, 0, 0, time.UTC),
			to: time.Date(2000, 12, 31, 16, 0, 0, 0, time.UTC), timezone: "Asia/Shanghai",
			fromDate: "2000-12-31", toDate: "2001-01-01", day: true, month: true, quarter: true, year: true,
		},
		{
			name: "New York spring DST", from: time.Date(2024, 3, 10, 6, 0, 0, 0, time.UTC),
			to: time.Date(2024, 3, 10, 7, 0, 0, 0, time.UTC), timezone: "America/New_York",
			fromDate: "2024-03-10", toDate: "2024-03-10",
		},
		{
			name: "New York fall DST", from: time.Date(2024, 11, 3, 5, 0, 0, 0, time.UTC),
			to: time.Date(2024, 11, 3, 6, 0, 0, 0, time.UTC), timezone: "America/New_York",
			fromDate: "2024-11-03", toDate: "2024-11-03",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transition, err := resolveCityCalendarTransition(test.from, test.to, test.timezone)
			require.NoError(t, err)
			require.Equal(t, test.fromDate, formatCityDate(transition.fromDate))
			require.Equal(t, test.toDate, formatCityDate(transition.toDate))
			require.Equal(t, test.day, transition.dayChanged)
			require.Equal(t, test.month, transition.monthChanged)
			require.Equal(t, test.quarter, transition.quarterChanged)
			require.Equal(t, test.year, transition.yearChanged)
		})
	}
	_, err := resolveCityCalendarTransition(time.Now(), time.Now().Add(time.Hour), "Not/A_Timezone")
	require.Error(t, err)
}

func TestResolveCityCalendarTransitionCountsTenYearsWithoutBoundaryDrift(t *testing.T) {
	current := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2010, time.January, 1, 0, 0, 0, 0, time.UTC)
	var days, months, quarters, years int
	for current.Before(end) {
		next := current.Add(time.Hour)
		transition, err := resolveCityCalendarTransition(current, next, "UTC")
		require.NoError(t, err)
		if transition.dayChanged {
			days++
		}
		if transition.monthChanged {
			months++
		}
		if transition.quarterChanged {
			quarters++
		}
		if transition.yearChanged {
			years++
		}
		current = next
	}
	require.Equal(t, 3653, days)
	require.Equal(t, 120, months)
	require.Equal(t, 40, quarters)
	require.Equal(t, 10, years)
}
