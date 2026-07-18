package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCityHouseholdCommandsCanonicalizesAndRejectsInvalidShape(t *testing.T) {
	payload, handled, err := normalizeCityHouseholdMovementCommand(
		CityCommandTypeHouseholdAdjust,
		json.RawMessage(`{"district_code":" CENTRAL ","income_band":" MIDDLE ","movement_type":" SPLIT ","household_units":12,"reason":"  independent adults  "}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, cityHouseholdAdjustmentPayload{
		DistrictCode: "central", IncomeBand: "middle", MovementType: "split",
		HouseholdUnits: 12, Reason: "independent adults",
	}, payload)

	_, handled, err = normalizeCityHouseholdMovementCommand(
		CityCommandTypeHouseholdReclassify,
		json.RawMessage(`{"district_code":"central","source_income_band":"low","target_income_band":"middle","child_units":1,"working_units":1,"senior_units":0,"employed_units":2,"household_units":1,"occupied_units":1}`),
	)
	require.True(t, handled)
	require.ErrorIs(t, err, ErrCityInvalidInput)

	_, handled, err = normalizeCityHouseholdMovementCommand(
		CityCommandTypeHouseholdAdjust,
		json.RawMessage(`{"district_code":"central","income_band":"middle","movement_type":"split","household_units":1,"unknown":true}`),
	)
	require.True(t, handled)
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestCalculateCityHouseholdCountPlanSeparatesHouseholdsFromPopulation(t *testing.T) {
	ref := cityHouseholdCohortRef{
		childUnits: 10, workingUnits: 20, seniorUnits: 5, employedUnits: 15,
		householdUnits: 12, occupiedUnits: 10, unmetUnits: 2,
	}
	formation, err := calculateCityHouseholdCountPlan(ref, CityHouseholdMovementFormation, 3)
	require.NoError(t, err)
	require.Equal(t, int64(15), formation.householdAfter)
	require.Equal(t, int64(10), formation.occupiedAfter)
	require.Equal(t, int64(5), formation.unmetAfter)
	require.Equal(t, int64(35), formation.childAfter+formation.workingAfter+formation.seniorAfter)

	dissolution, err := calculateCityHouseholdCountPlan(ref, CityHouseholdMovementDissolution, 4)
	require.NoError(t, err)
	require.Equal(t, int64(8), dissolution.householdAfter)
	require.Equal(t, int64(8), dissolution.occupiedAfter)
	require.Equal(t, int64(2), dissolution.units.occupied)
	require.Zero(t, dissolution.unmetAfter)

	_, err = calculateCityHouseholdCountPlan(ref, CityHouseholdMovementSplit, 24)
	var businessErr *cityHouseholdMovementBusinessError
	require.ErrorAs(t, err, &businessErr)
	require.Equal(t, cityHouseholdRejectionPopulation, businessErr.code)
}

func TestCalculateCityHouseholdDemographyGuardRepairsTemporaryPopulationFloor(t *testing.T) {
	ref := cityHouseholdCohortRef{
		childUnits: 1, workingUnits: 1, seniorUnits: 1,
		householdUnits: 5, occupiedUnits: 4, unmetUnits: 1,
	}
	plan, err := calculateCityHouseholdCountPlan(ref, CityHouseholdMovementDissolution, 2)
	require.NoError(t, err)
	require.Equal(t, int64(3), plan.householdAfter)
	require.Equal(t, int64(3), plan.occupiedAfter)
	require.Equal(t, int64(1), plan.units.occupied)
	require.Zero(t, plan.unmetAfter)
}

func TestCalculateCityHouseholdReclassificationConservesEveryProjection(t *testing.T) {
	source := cityHouseholdCohortRef{
		districtCode: "central", entityID: 1, incomeBand: "low",
		childUnits: 10, workingUnits: 20, seniorUnits: 5, employedUnits: 15,
		householdUnits: 12, occupiedUnits: 10, unmetUnits: 2,
	}
	target := cityHouseholdCohortRef{
		districtCode: "central", entityID: 1, incomeBand: "middle",
		childUnits: 8, workingUnits: 16, seniorUnits: 4, employedUnits: 10,
		householdUnits: 10, occupiedUnits: 8, unmetUnits: 2,
	}
	units := cityHouseholdMovementUnits{child: 2, working: 4, senior: 1, employed: 3, household: 2, occupied: 1}
	plans, err := calculateCityHouseholdReclassificationPlans(source, target, units)
	require.NoError(t, err)
	require.Len(t, plans, 2)
	require.Equal(t, int64(18), plans[0].childAfter+plans[1].childAfter)
	require.Equal(t, int64(36), plans[0].workingAfter+plans[1].workingAfter)
	require.Equal(t, int64(9), plans[0].seniorAfter+plans[1].seniorAfter)
	require.Equal(t, int64(25), plans[0].employedAfter+plans[1].employedAfter)
	require.Equal(t, int64(22), plans[0].householdAfter+plans[1].householdAfter)
	require.Equal(t, int64(18), plans[0].occupiedAfter+plans[1].occupiedAfter)

	target.incomeBand = "high"
	_, err = calculateCityHouseholdReclassificationPlans(source, target, units)
	var businessErr *cityHouseholdMovementBusinessError
	require.ErrorAs(t, err, &businessErr)
	require.Equal(t, cityHouseholdRejectionNonAdjacent, businessErr.code)
}

func TestReplayCityHouseholdReclassificationPreservesPopulationEmploymentAndHousing(t *testing.T) {
	postedAt := time.Now().UTC()
	state := cityHashState{
		SimulationVersion: CitySimulationVersionF6V3,
		Demography: cityDemographyHashState{Cohorts: []cityDemographyHashCohort{
			{DistrictCode: "central", EntityCode: "households", IncomeBand: "low", ChildUnits: 10, WorkingUnits: 20, SeniorUnits: 5},
			{DistrictCode: "central", EntityCode: "households", IncomeBand: "middle", ChildUnits: 8, WorkingUnits: 16, SeniorUnits: 4},
		}},
		Physical: cityPhysicalHashState{HouseholdCohorts: []cityHashHouseholdCohort{
			{DistrictCode: "central", EntityCode: "households", IncomeBand: "low", PopulationUnits: 35, WorkingAgeUnits: 20, EmployedUnits: 15, HouseholdUnits: 12, HousingDemandUnits: 12},
			{DistrictCode: "central", EntityCode: "households", IncomeBand: "middle", PopulationUnits: 28, WorkingAgeUnits: 16, EmployedUnits: 10, HouseholdUnits: 10, HousingDemandUnits: 10},
		}},
		Markets: cityMarketHashState{Occupancies: []cityMarketHashOccupancy{
			{DistrictCode: "central", IncomeBand: "low", OccupiedUnits: 10, UnmetUnits: 2},
			{DistrictCode: "central", IncomeBand: "middle", OccupiedUnits: 8, UnmetUnits: 2},
		}},
	}
	sourceDistrict, targetDistrict := "central", "central"
	entity, sourceBand, targetBand := "households", "low", "middle"
	movement := &CityHouseholdMovement{
		ID: 1, WorldID: 7, Tick: 1, Sequence: 1, Origin: CityHouseholdMovementOriginCommand,
		MovementType:       CityHouseholdMovementIncomeReclassification,
		SourceDistrictCode: &sourceDistrict, SourceEntityCode: &entity, SourceIncomeBand: &sourceBand,
		TargetDistrictCode: &targetDistrict, TargetEntityCode: &entity, TargetIncomeBand: &targetBand,
		ChildUnits: 2, WorkingUnits: 4, SeniorUnits: 1, EmployedUnits: 3,
		HouseholdUnits: 2, OccupiedUnits: 1, ExpectedLineCount: 2, PostedAt: &postedAt,
		Lines: []*CityHouseholdMovementLine{
			{
				MovementID: 1, WorldID: 7, LineNo: 1, DistrictCode: "central", EntityCode: "households", IncomeBand: "low",
				Direction:                cityHouseholdMovementDirectionOutflow,
				DemographicVersionBefore: 0, DemographicVersionAfter: 1,
				CohortVersionBefore: 0, CohortVersionAfter: 1, OccupancyVersionBefore: 0, OccupancyVersionAfter: 1,
				ChildUnits: 2, WorkingUnits: 4, SeniorUnits: 1, EmployedUnits: 3, HouseholdUnits: 2, OccupiedUnits: 1,
				ChildUnitsBefore: 10, WorkingUnitsBefore: 20, SeniorUnitsBefore: 5, EmployedUnitsBefore: 15,
				HouseholdUnitsBefore: 12, OccupiedUnitsBefore: 10, UnmetUnitsBefore: 2,
				ChildUnitsAfter: 8, WorkingUnitsAfter: 16, SeniorUnitsAfter: 4, EmployedUnitsAfter: 12,
				HouseholdUnitsAfter: 10, OccupiedUnitsAfter: 9, UnmetUnitsAfter: 1,
			},
			{
				MovementID: 1, WorldID: 7, LineNo: 2, DistrictCode: "central", EntityCode: "households", IncomeBand: "middle",
				Direction:                cityHouseholdMovementDirectionInflow,
				DemographicVersionBefore: 0, DemographicVersionAfter: 1,
				CohortVersionBefore: 0, CohortVersionAfter: 1, OccupancyVersionBefore: 0, OccupancyVersionAfter: 1,
				ChildUnits: 2, WorkingUnits: 4, SeniorUnits: 1, EmployedUnits: 3, HouseholdUnits: 2, OccupiedUnits: 1,
				ChildUnitsBefore: 8, WorkingUnitsBefore: 16, SeniorUnitsBefore: 4, EmployedUnitsBefore: 10,
				HouseholdUnitsBefore: 10, OccupiedUnitsBefore: 8, UnmetUnitsBefore: 2,
				ChildUnitsAfter: 10, WorkingUnitsAfter: 20, SeniorUnitsAfter: 5, EmployedUnitsAfter: 13,
				HouseholdUnitsAfter: 12, OccupiedUnitsAfter: 9, UnmetUnitsAfter: 3,
			},
		},
	}

	require.NoError(t, replayCityHouseholdMovement(movement, &state))
	require.NoError(t, validateCityHouseholdHashProjection(&state))
	require.Equal(t, int64(63), state.Physical.HouseholdCohorts[0].PopulationUnits+state.Physical.HouseholdCohorts[1].PopulationUnits)
	require.Equal(t, int64(25), state.Physical.HouseholdCohorts[0].EmployedUnits+state.Physical.HouseholdCohorts[1].EmployedUnits)
	require.Equal(t, int64(22), state.Physical.HouseholdCohorts[0].HouseholdUnits+state.Physical.HouseholdCohorts[1].HouseholdUnits)
	require.Equal(t, int64(18), state.Markets.Occupancies[0].OccupiedUnits+state.Markets.Occupancies[1].OccupiedUnits)
}
