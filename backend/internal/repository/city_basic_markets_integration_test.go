//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type cityMarketFixture struct {
	worldID      int64
	householdID  int64
	firmID       int64
	governmentID int64
}

func TestCityBasicMarketsSettleMoneyResourcesLaborHousingAndBudgetsAtomically(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	ownerA := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-market-a-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	ownerB := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-market-b-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-market-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)

	createFixture := func(ownerID int64) cityMarketFixture {
		seed := int64(774411)
		foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
			OwnerUserID: ownerID, Name: "Market Cycle City", Timezone: "Asia/Shanghai", Seed: &seed,
		})
		require.NoError(t, err)
		require.Equal(t, service.CitySimulationVersionV1, foundation.World.SimulationVersion)
		require.NotNil(t, foundation.Markets)
		require.NotNil(t, foundation.Markets.Cycle)
		require.NotNil(t, foundation.Markets.Policy)
		require.Len(t, foundation.Markets.Markets, 3)
		require.Len(t, foundation.Markets.Occupancies, 18)
		require.Zero(t, foundation.Markets.Cycle.CycleIndex)
		fixture := cityMarketFixture{worldID: foundation.World.ID}
		for _, entity := range foundation.Entities {
			switch entity.EntityType {
			case service.CityEntityTypeHousehold:
				fixture.householdID = entity.ID
			case service.CityEntityTypeFirm:
				fixture.firmID = entity.ID
			case service.CityEntityTypeGovernment:
				fixture.governmentID = entity.ID
			}
		}
		require.Positive(t, fixture.householdID)
		require.Positive(t, fixture.firmID)
		require.Positive(t, fixture.governmentID)
		return fixture
	}
	worldA := createFixture(ownerA.ID)
	worldB := createFixture(ownerB.ID)

	resume := func(ownerID int64, fixture cityMarketFixture) {
		expected := int64(0)
		_, err := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "market-resume-0",
			CommandType: service.CityCommandTypeWorldResume, Payload: json.RawMessage(`{}`),
			ExpectedWorldTick: &expected,
		})
		require.NoError(t, err)
		result, err := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "market-resume-step-0",
			ExpectedWorldTick: &expected,
		})
		require.NoError(t, err)
		require.Empty(t, result.MarketSettlements)
	}
	resume(ownerA.ID, worldA)
	resume(ownerB.ID, worldB)

	expectedOne := int64(1)
	stepA, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "market-cycle-step-1",
		ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	stepB, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerB.ID, WorldID: worldB.worldID, IdempotencyKey: "market-cycle-step-1",
		ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	require.Equal(t, stepA.Tick.StateHash, stepB.Tick.StateHash)
	require.Len(t, stepA.MarketSettlements, 4)
	require.Len(t, stepA.Journals, 10)
	require.Len(t, stepA.ResourceOperations, 6)
	require.Equal(t, []string{
		service.CityMarketLabor, service.CityMarketBasicGoods,
		service.CityMarketHousing, service.CitySettlementFiscal,
	}, []string{
		stepA.MarketSettlements[0].SettlementType,
		stepA.MarketSettlements[1].SettlementType,
		stepA.MarketSettlements[2].SettlementType,
		stepA.MarketSettlements[3].SettlementType,
	})
	for _, settlement := range stepA.MarketSettlements {
		require.Equal(t, int64(1), settlement.CycleIndex)
		require.Equal(t, int64(2), settlement.Tick)
		require.Len(t, settlement.Allocations, settlement.AllocationCount)
		require.Len(t, settlement.BudgetMovements, settlement.BudgetMovementCount)
	}
	require.Equal(t, int64(400), stepA.MarketSettlements[0].ClearedUnits)
	require.Equal(t, int64(400000), stepA.MarketSettlements[0].GrossAmountUnits)
	require.Equal(t, int64(500), stepA.MarketSettlements[1].ClearedUnits)
	require.Equal(t, int64(250000), stepA.MarketSettlements[1].GrossAmountUnits)
	require.Equal(t, int64(5000), stepA.MarketSettlements[2].ClearedUnits)
	require.Equal(t, int64(500000), stepA.MarketSettlements[2].GrossAmountUnits)
	require.Equal(t, int64(70875), stepA.MarketSettlements[3].GrossAmountUnits)
	require.Len(t, stepA.MarketSettlements[3].BudgetMovements, 2)

	for _, journal := range stepA.Journals {
		if journal.JournalType == "opening" {
			require.Nil(t, journal.MarketSettlementID)
		} else {
			require.NotNil(t, journal.MarketSettlementID)
		}
		require.Equal(t, journal.DebitTotalUnits, journal.CreditTotalUnits)
	}
	for _, operation := range stepA.ResourceOperations {
		if operation.OperationType == "opening" {
			require.Nil(t, operation.MarketSettlementID)
		} else {
			require.NotNil(t, operation.MarketSettlementID)
		}
	}

	overview, err := cityService.GetMarketOverview(ctx, ownerA.ID, worldA.worldID)
	require.NoError(t, err)
	require.Equal(t, int64(1), overview.Cycle.CycleIndex)
	require.Equal(t, int64(2), *overview.Cycle.LastSettledTick)
	require.Equal(t, int64(26), overview.Cycle.NextDueTick)
	quotes := map[string]int64{}
	for _, market := range overview.Markets {
		quotes[market.MarketCode] = market.QuoteUnits
		require.Equal(t, int64(2), *market.LastClearingTick)
	}
	require.Equal(t, int64(952), quotes[service.CityMarketLabor])
	require.Equal(t, int64(518), quotes[service.CityMarketBasicGoods])
	require.Equal(t, int64(101), quotes[service.CityMarketHousing])
	var occupied int64
	for _, occupancy := range overview.Occupancies {
		occupied += occupancy.OccupiedUnits
		require.Equal(t, occupancy.OccupiedUnits+occupancy.UnmetUnits,
			findCityCohortHousingDemand(t, stepA, occupancy.CohortID, worldA.worldID))
	}
	require.Equal(t, int64(5000), occupied)

	physical, err := cityService.GetPhysicalState(ctx, ownerA.ID, worldA.worldID)
	require.NoError(t, err)
	var householdEmployment, firmEmployment int64
	for _, cohort := range physical.HouseholdCohorts {
		householdEmployment += cohort.EmployedUnits
	}
	for _, firm := range physical.Firms {
		firmEmployment += firm.EmployeeUnits
	}
	require.Equal(t, int64(400), householdEmployment)
	require.Equal(t, householdEmployment, firmEmployment)
	quantities := map[string]int64{}
	for _, balance := range physical.Inventories {
		key := fmt.Sprintf("%d:%s", balance.EntityID, balance.ResourceCode)
		quantities[key] = balance.QuantityUnits
	}
	require.Equal(t, int64(200), quantities[fmt.Sprintf("%d:basic_material", worldA.firmID)])
	require.Zero(t, quantities[fmt.Sprintf("%d:consumer_goods", worldA.firmID)])
	require.Equal(t, int64(30), quantities[fmt.Sprintf("%d:consumer_goods", worldA.householdID)])
	budgetSpent := map[string]int64{}
	for _, budget := range physical.BudgetLines {
		budgetSpent[budget.Code] = budget.SpentUnits
	}
	require.Equal(t, int64(13125), budgetSpent["healthcare"])
	require.Equal(t, int64(5250), budgetSpent["social_protection"])

	trial, err := cityService.GetTrialBalance(ctx, ownerA.ID, worldA.worldID)
	require.NoError(t, err)
	require.True(t, trial.Balanced)
	require.Zero(t, trial.Units[0].ProjectionMismatchCount)

	page, err := cityService.ListMarketSettlements(ctx, service.CityMarketSettlementListInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.NextCursor)
	next, err := cityService.ListMarketSettlements(ctx, service.CityMarketSettlementListInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, AfterTick: page.NextCursor.Tick,
		AfterSequence: page.NextCursor.Sequence, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, next.Items, 2)
	detail, err := cityService.GetMarketSettlement(ctx, ownerA.ID, worldA.worldID, 2, 4)
	require.NoError(t, err)
	require.Len(t, detail.BudgetMovements, 2)
	_, err = cityService.GetMarketOverview(ctx, outsider.ID, worldA.worldID)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	replayed, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "market-cycle-step-1",
		ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	require.Equal(t, stepA.Tick.ID, replayed.Tick.ID)
	require.Len(t, replayed.MarketSettlements, 4)

	expectedTwo := int64(2)
	notDue, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "market-not-due-step-2",
		ExpectedWorldTick: &expectedTwo,
	})
	require.NoError(t, err)
	require.Empty(t, notDue.MarketSettlements)
	require.Empty(t, notDue.Journals)
	require.Empty(t, notDue.ResourceOperations)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_market_states SET quote_units = quote_units + 1 WHERE world_id = $1`, worldA.worldID)
	require.ErrorContains(t, err, "only change through a draft settlement")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_housing_occupancies SET occupied_units = occupied_units + 1 WHERE world_id = $1`, worldA.worldID)
	require.ErrorContains(t, err, "only change through a posted projection")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_market_settlements SET gross_amount_units = gross_amount_units + 1
WHERE world_id = $1 AND tick = 2 AND sequence = 1`, worldA.worldID)
	require.ErrorContains(t, err, "draft-to-posted transition")
	_, err = integrationDB.ExecContext(ctx, `
DELETE FROM city_market_allocations WHERE settlement_id = $1`, detail.ID)
	require.ErrorContains(t, err, "immutable facts")
}

func findCityCohortHousingDemand(t *testing.T, _ *service.CityStepResult, cohortID, worldID int64) int64 {
	t.Helper()
	var demand int64
	require.NoError(t, integrationDB.QueryRow(`
SELECT housing_demand_units FROM city_household_cohorts
WHERE id = $1 AND world_id = $2`, cohortID, worldID).Scan(&demand))
	return demand
}
