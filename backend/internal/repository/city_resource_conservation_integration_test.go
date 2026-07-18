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

type cityResourceFixture struct {
	worldID      int64
	householdID  int64
	firmID       int64
	governmentID int64
}

func TestCityEntityResourceConservationPostsDeterministicallyAndProtectsFacts(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	ownerA := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-resource-a-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	ownerB := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-resource-b-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-resource-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)

	createFixture := func(ownerID int64) cityResourceFixture {
		seed := int64(662211)
		foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
			OwnerUserID: ownerID, Name: "Conserved Resource City", Timezone: "Asia/Shanghai", Seed: &seed,
		})
		require.NoError(t, err)
		require.Equal(t, service.CitySimulationVersionV1, foundation.World.SimulationVersion)
		require.NotNil(t, foundation.Physical)
		require.Len(t, foundation.Physical.Districts, 6)
		require.Len(t, foundation.Physical.HouseholdCohorts, 18)
		require.Len(t, foundation.Physical.Firms, 1)
		require.Len(t, foundation.Physical.BudgetLines, 7)
		require.Len(t, foundation.Physical.Resources, 6)
		require.Len(t, foundation.Physical.Recipes, 2)
		require.Len(t, foundation.Physical.Inventories, 8)
		fixture := cityResourceFixture{worldID: foundation.World.ID}
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

	marshalPayload := func(value any) json.RawMessage {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		return raw
	}
	submitBatch := func(ownerID int64, fixture cityResourceFixture) {
		expectedTick := int64(0)
		inputs := []service.CityCommandSubmitInput{
			{
				UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "resource-produce-0",
				CommandType: service.CityCommandTypeResourceProduce,
				Payload: marshalPayload(map[string]any{
					"firm_entity_id": fixture.firmID, "district_code": "central",
					"recipe_code": "basic_goods", "batch_count": 10,
				}), ExpectedWorldTick: &expectedTick,
			},
			{
				UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "resource-transfer-0",
				CommandType: service.CityCommandTypeResourceTransfer,
				Payload: marshalPayload(map[string]any{
					"from_entity_id": fixture.firmID, "to_entity_id": fixture.householdID,
					"from_district_code": "central", "to_district_code": "central",
					"resource_code": "consumer_goods", "quantity_units": 10,
				}), ExpectedWorldTick: &expectedTick,
			},
			{
				UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "resource-consume-0",
				CommandType: service.CityCommandTypeResourceConsume,
				Payload: marshalPayload(map[string]any{
					"entity_id": fixture.householdID, "district_code": "central",
					"resource_code": "food", "quantity_units": 5, "purpose": "household consumption",
				}), ExpectedWorldTick: &expectedTick,
			},
			{
				UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "resource-land-transfer-0",
				CommandType: service.CityCommandTypeResourceTransfer,
				Payload: marshalPayload(map[string]any{
					"from_entity_id": fixture.governmentID, "to_entity_id": fixture.firmID,
					"from_district_code": "central", "to_district_code": "central",
					"resource_code": "developable_land", "quantity_units": 100,
				}), ExpectedWorldTick: &expectedTick,
			},
			{
				UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "resource-capital-transfer-0",
				CommandType: service.CityCommandTypeResourceTransfer,
				Payload: marshalPayload(map[string]any{
					"from_entity_id": fixture.governmentID, "to_entity_id": fixture.firmID,
					"from_district_code": "central", "to_district_code": "central",
					"resource_code": "capital_goods", "quantity_units": 5,
				}), ExpectedWorldTick: &expectedTick,
			},
			{
				UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "resource-construct-0",
				CommandType: service.CityCommandTypeResourceProduce,
				Payload: marshalPayload(map[string]any{
					"firm_entity_id": fixture.firmID, "district_code": "central",
					"recipe_code": "housing_construction", "batch_count": 1,
				}), ExpectedWorldTick: &expectedTick,
			},
		}
		for _, input := range inputs {
			_, err := cityService.SubmitCommand(ctx, input)
			require.NoError(t, err)
		}
	}
	submitBatch(ownerA.ID, worldA)
	submitBatch(ownerB.ID, worldB)
	expectedZero := int64(0)
	stepA, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "resource-step-0", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	stepB, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerB.ID, WorldID: worldB.worldID, IdempotencyKey: "resource-step-0", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, stepA.Tick.StateHash, stepB.Tick.StateHash)
	require.Equal(t, 6, stepA.Tick.AppliedCommandCount)
	require.Zero(t, stepA.Tick.RejectedCommandCount)
	require.Len(t, stepA.ResourceOperations, 9)
	require.Len(t, stepA.Events, 10)

	physical, err := cityService.GetPhysicalState(ctx, ownerA.ID, worldA.worldID)
	require.NoError(t, err)
	require.Equal(t, int64(1), physical.AsOfTick)
	quantity := func(entityID int64, resourceCode string) int64 {
		for _, balance := range physical.Inventories {
			if balance.EntityID == entityID && balance.ResourceCode == resourceCode && balance.DistrictCode == "central" {
				return balance.QuantityUnits
			}
		}
		t.Fatalf("inventory not found for entity %d resource %s", entityID, resourceCode)
		return 0
	}
	require.Equal(t, int64(980), quantity(worldA.firmID, "basic_material"))
	require.Equal(t, int64(100), quantity(worldA.firmID, "consumer_goods"))
	require.Equal(t, int64(40), quantity(worldA.householdID, "consumer_goods"))
	require.Equal(t, int64(295), quantity(worldA.householdID, "food"))
	require.Equal(t, int64(1), quantity(worldA.firmID, "housing_units"))
	require.Equal(t, int64(999900), quantity(worldA.governmentID, "developable_land"))
	require.Equal(t, int64(995), quantity(worldA.governmentID, "capital_goods"))

	page, err := cityService.ListResourceOperations(ctx, service.CityResourceOperationListInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.NextCursor)
	nextPage, err := cityService.ListResourceOperations(ctx, service.CityResourceOperationListInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, AfterTick: page.NextCursor.Tick,
		AfterSequence: page.NextCursor.Sequence, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, nextPage.Items, 7)
	produced := stepA.ResourceOperations[3]
	require.Equal(t, "production", produced.OperationType)
	require.Len(t, produced.Entries, 2)
	loaded, err := cityService.GetResourceOperation(ctx, ownerA.ID, worldA.worldID, produced.Tick, produced.Sequence)
	require.NoError(t, err)
	require.Equal(t, produced.ID, loaded.ID)
	require.Len(t, loaded.Entries, 2)
	_, err = cityService.GetPhysicalState(ctx, outsider.ID, worldA.worldID)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)
	_, err = cityService.ListResourceOperations(ctx, service.CityResourceOperationListInput{UserID: outsider.ID, WorldID: worldA.worldID})
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	replayed, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "resource-step-0", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, stepA.Tick.ID, replayed.Tick.ID)
	require.Len(t, replayed.ResourceOperations, 9)

	expectedOne := int64(1)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "resource-insufficient-1",
		CommandType: service.CityCommandTypeResourceConsume,
		Payload: marshalPayload(map[string]any{
			"entity_id": worldA.householdID, "district_code": "central",
			"resource_code": "food", "quantity_units": 10000, "purpose": "invalid depletion",
		}), ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	insufficientStep, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "resource-step-1", ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	require.Len(t, insufficientStep.ResourceOperations, 0)
	require.Equal(t, service.CityCommandStatusRejected, insufficientStep.Commands[0].Status)
	require.Equal(t, "CITY_RESOURCE_INSUFFICIENT_INVENTORY", *insufficientStep.Commands[0].ErrorCode)

	expectedTwo := int64(2)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "resource-capacity-2",
		CommandType: service.CityCommandTypeResourceProduce,
		Payload: marshalPayload(map[string]any{
			"firm_entity_id": worldA.firmID, "district_code": "central",
			"recipe_code": "basic_goods", "batch_count": 401,
		}), ExpectedWorldTick: &expectedTwo,
	})
	require.NoError(t, err)
	capacityStep, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "resource-step-2", ExpectedWorldTick: &expectedTwo,
	})
	require.NoError(t, err)
	require.Len(t, capacityStep.ResourceOperations, 0)
	require.Equal(t, "CITY_RESOURCE_CAPACITY_EXCEEDED", *capacityStep.Commands[0].ErrorCode)

	var balanceID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT balance.id
FROM city_inventory_balances balance
JOIN city_resources resource ON resource.id = balance.resource_id
WHERE balance.world_id = $1 AND balance.entity_id = $2 AND resource.code = 'food'`,
		worldA.worldID, worldA.householdID).Scan(&balanceID))
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_inventory_balances SET quantity_units = quantity_units + 1 WHERE id = $1`, balanceID)
	require.ErrorContains(t, err, "only change through a draft resource operation")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_resource_operations SET description = 'tampered' WHERE id = $1`, produced.ID)
	require.ErrorContains(t, err, "draft-to-posted transition")
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM city_resource_entries WHERE id = $1`, produced.Entries[0].ID)
	require.ErrorContains(t, err, "immutable facts")
}
