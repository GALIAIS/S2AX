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

// TestCityOpenWorldV18FreightBatchSplitsOverflowAndDeliversAtomically proves
// the V18 overflow path stays outside the legacy V16/V17 single-shipment
// projection, creates capacity-bounded V9 demands, and releases the one V15
// delivery only after every consignment reaches the receipt boundary.
func TestCityOpenWorldV18FreightBatchSplitsOverflowAndDeliversAtomically(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v18-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(18_180_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V18 Batch Freight", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV18,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	initial, err := cityService.GetCityOpenWorldFreightBatchState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Zero(t, initial.Policy.BaselineTick)
	require.Empty(t, initial.Plans)
	require.Empty(t, initial.Consignments)

	var buyerEntityID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id
FROM city_economic_entities
WHERE world_id = $1 AND entity_type = 'firm' AND code = 'openworld_trade_buyer'`, worldID).Scan(&buyerEntityID))

	currentTick := int64(0)
	submit := func(key, commandType string, payload map[string]any) *service.CityCommand {
		t.Helper()
		raw, marshalErr := json.Marshal(payload)
		require.NoError(t, marshalErr)
		command, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: raw, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, submitErr)
		return command
	}
	step := func(key string) *service.CityStepResult {
		t.Helper()
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.NotNil(t, result.Tick)
		require.Equal(t, currentTick+1, result.Tick.Tick)
		currentTick = result.Tick.Tick
		return result
	}
	submitAndStep := func(key, commandType string, payload map[string]any) *service.CityStepResult {
		t.Helper()
		command := submit(key, commandType, payload)
		result := step(key + "-step")
		require.Len(t, result.Commands, 1)
		require.Equal(t, command.ID, result.Commands[0].ID)
		require.Equal(t, service.CityCommandStatusApplied, result.Commands[0].Status)
		return result
	}

	submitAndStep("v18-freight-fund-buyer", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(2_000), "memo": "fund V18 freight buyer",
	})
	submitAndStep("v18-freight-create", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
		"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
		"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(64), "unit_price_units": int64(5)}},
	})
	supply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, supply.Orders, 1)
	orderCode := supply.Orders[0].Code
	submitAndStep("v18-freight-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
	submitAndStep("v18-freight-dispatch", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})

	// The next boundary creates a V16 suppressed overflow source. V18 consumes
	// that source in the same automatic phase and emits two V9 demands, while
	// V17 intentionally creates no single-shipment custody row for it.
	step("v18-freight-create-batches")
	batches, err := cityService.GetCityOpenWorldFreightBatchState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, batches.Plans, 1)
	require.Len(t, batches.Consignments, 2)
	require.Equal(t, int64(64), batches.Plans[0].RequiredUnits)
	require.Equal(t, "active", batches.Plans[0].State)
	require.Equal(t, int64(2), batches.Policy.AwaitingRouteCount)
	for index, consignment := range batches.Consignments {
		require.Equal(t, index+1, consignment.BatchNo)
		require.Equal(t, int64(32), consignment.RequestedUnits)
		require.Equal(t, "awaiting_route", consignment.State)
	}
	receiptState, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, receiptState.Shipments, "V17 must not project a suppressed overflow source")

	// Delivery is rejected before all independently scheduled consignments have
	// completed. No partial V15 inventory/cash mutation is allowed.
	earlyDelivery := submit("v18-freight-deliver-before-ready", service.CityCommandTypeOpenWorldSupplyOrderDeliver, map[string]any{"order_code": orderCode})
	rejected := step("v18-freight-deliver-before-ready-step")
	require.Len(t, rejected.Commands, 1)
	require.Equal(t, earlyDelivery.ID, rejected.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusRejected, rejected.Commands[0].Status)
	require.NotNil(t, rejected.Commands[0].ErrorCode)
	require.Equal(t, "CITY_SUPPLY_CHAIN_RECEIPT_NOT_READY", *rejected.Commands[0].ErrorCode)

	var receiptReady *service.CityOpenWorldFreightBatchState
	for attempt := 0; attempt < 64; attempt++ {
		step(fmt.Sprintf("v18-freight-progress-%d", currentTick+1))
		state, stateErr := cityService.GetCityOpenWorldFreightBatchState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		if len(state.Plans) == 1 && state.Plans[0].State == "ready" {
			receiptReady = state
			break
		}
	}
	require.NotNil(t, receiptReady, "all V18 freight consignments did not reach the atomic receipt boundary")
	require.Equal(t, int64(2), receiptReady.Policy.AwaitingReceiptCount)
	for _, consignment := range receiptReady.Consignments {
		require.Equal(t, "awaiting_receipt", consignment.State)
	}

	delivered := submitAndStep("v18-freight-deliver-after-ready", service.CityCommandTypeOpenWorldSupplyOrderDeliver, map[string]any{"order_code": orderCode})
	require.Len(t, delivered.ResourceOperations, 1, "the order must transfer resources exactly once")
	finalSupply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "delivered", cityOpenWorldV16SupplyOrderState(t, finalSupply, orderCode))
	require.Equal(t, int64(1), finalSupply.Policy.DeliveryCount)

	finalBatches, err := cityService.GetCityOpenWorldFreightBatchState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, finalBatches.Plans, 1)
	require.Equal(t, "received", finalBatches.Plans[0].State)
	require.Equal(t, int64(2), finalBatches.Policy.ReceivedCount)
	require.Equal(t, int64(2), finalBatches.Policy.ReceiptCount)
	require.Len(t, finalBatches.Receipts, 2)
	for _, receipt := range finalBatches.Receipts {
		require.Equal(t, orderCode, receipt.OrderCode)
		require.Equal(t, delivered.Tick.Tick, receipt.ReceivedTick)
		require.Equal(t, finalBatches.Receipts[0].DeliveryFact, receipt.DeliveryFact)
		require.Equal(t, finalBatches.Receipts[0].ResourceOperation, receipt.ResourceOperation)
	}

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v18-freight-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v18-freight-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldFreightBatchState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, finalBatches, restored)
}
