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

// TestCityOpenWorldV22FreightSettlementRecordsPartialOutcomeAndRecovery
// proves the complete V22 successor path: a V17 shipment reaches custody,
// legacy atomic delivery is denied, a partial receipt transfers only accepted
// cargo, loss/refusal creates a dedicated refund and carrier claim, then V15
// / V17 reach their settled successor states without a legacy delivery row.
func TestCityOpenWorldV22FreightSettlementRecordsPartialOutcomeAndRecovery(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v22-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v22-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(22_220_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V22 Freight Settlement", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV22,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)

	initial, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Zero(t, initial.Policy.BaselineTick)
	require.Empty(t, initial.Orders)
	require.Empty(t, initial.Cases)
	require.Empty(t, initial.Receipts)

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
	shipmentForOrder := func(state *service.CityOpenWorldEnterpriseFreightReceiptState, orderCode string) service.CityOpenWorldEnterpriseFreightShipment {
		t.Helper()
		for _, shipment := range state.Shipments {
			if shipment.OrderCode == orderCode {
				return shipment
			}
		}
		t.Fatalf("missing V17 shipment for supply order %q", orderCode)
		return service.CityOpenWorldEnterpriseFreightShipment{}
	}

	submitAndStep("v22-freight-fund-buyer", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(1_000), "memo": "fund V22 settlement buyer",
	})
	submitAndStep("v22-freight-create", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
		"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
		"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5)}},
	})
	supply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, supply.Orders, 1)
	orderCode := supply.Orders[0].Code
	submitAndStep("v22-freight-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
	submitAndStep("v22-freight-dispatch", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})

	var settlementReady *service.CityOpenWorldFreightSettlementState
	for attempt := 0; attempt < 64; attempt++ {
		step(fmt.Sprintf("v22-freight-progress-%d", currentTick+1))
		shipments, shipmentErr := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
		require.NoError(t, shipmentErr)
		settlements, settlementErr := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
		require.NoError(t, settlementErr)
		if shipmentForOrder(shipments, orderCode).State == "awaiting_receipt" && len(settlements.Cases) == 1 {
			settlementReady = settlements
			break
		}
	}
	require.NotNil(t, settlementReady, "V22 did not materialize an actionable V17 settlement case")
	require.Len(t, settlementReady.Orders, 1)
	require.Len(t, settlementReady.Cases, 1)
	require.Len(t, settlementReady.Lines, 1)
	require.Equal(t, "receiving", settlementReady.Orders[0].State)
	require.Equal(t, "awaiting_receipt", settlementReady.Cases[0].TransportState)
	require.Equal(t, "awaiting_outcome", settlementReady.Cases[0].State)

	legacyDelivery := submit("v22-freight-legacy-deliver", service.CityCommandTypeOpenWorldSupplyOrderDeliver, map[string]any{"order_code": orderCode})
	legacyStep := step("v22-freight-legacy-deliver-step")
	require.Len(t, legacyStep.Commands, 1)
	require.Equal(t, legacyDelivery.ID, legacyStep.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusRejected, legacyStep.Commands[0].Status)
	require.NotNil(t, legacyStep.Commands[0].ErrorCode)
	require.Equal(t, "CITY_SUPPLY_CHAIN_SETTLEMENT_REQUIRED", *legacyStep.Commands[0].ErrorCode)

	deniedPayload, err := json.Marshal(map[string]any{
		"case_code": settlementReady.Cases[0].Code, "liability_party": "seller",
		"lines": []map[string]any{{"source_line_no": 1, "accepted_units": int64(1), "lost_units": int64(0), "rejected_units": int64(0)}},
	})
	require.NoError(t, err)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: viewer.ID, WorldID: worldID, IdempotencyKey: "v22-freight-viewer-settle-denied",
		CommandType: service.CityCommandTypeOpenWorldFreightSettlementReceipt,
		Payload:     deniedPayload, ExpectedWorldTick: &currentTick,
	})
	require.ErrorIs(t, err, service.ErrCityPermissionDenied)

	firstSettlement := submit("v22-freight-settle-first-partial", service.CityCommandTypeOpenWorldFreightSettlementReceipt, map[string]any{
		"case_code": settlementReady.Cases[0].Code, "liability_party": "seller",
		"lines": []map[string]any{{
			"source_line_no": 1, "accepted_units": int64(4), "lost_units": int64(0), "rejected_units": int64(0),
		}},
	})
	firstStep := step("v22-freight-settle-first-partial-step")
	require.Len(t, firstStep.Commands, 1)
	require.Equal(t, firstSettlement.ID, firstStep.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusApplied, firstStep.Commands[0].Status)
	require.Len(t, firstStep.ResourceOperations, 1)
	require.Empty(t, firstStep.Journals)
	middleState, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "receiving", middleState.Orders[0].State)
	require.Equal(t, "receiving", middleState.Cases[0].State)
	require.Len(t, middleState.Receipts, 1)
	require.Equal(t, int64(4), middleState.Policy.AcceptedUnits)
	require.Zero(t, middleState.Policy.LostUnits)
	require.Zero(t, middleState.Policy.RejectedUnits)

	settlementCommand := submit("v22-freight-settle-partial", service.CityCommandTypeOpenWorldFreightSettlementReceipt, map[string]any{
		"case_code": settlementReady.Cases[0].Code, "liability_party": "carrier",
		"lines": []map[string]any{{
			"source_line_no": 1, "accepted_units": int64(4), "lost_units": int64(2), "rejected_units": int64(2),
		}},
	})
	settledStep := step("v22-freight-settle-partial-step")
	require.Len(t, settledStep.Commands, 1)
	require.Equal(t, settlementCommand.ID, settledStep.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusApplied, settledStep.Commands[0].Status)
	require.Len(t, settledStep.ResourceOperations, 1)
	require.Len(t, settledStep.Journals, 1)
	require.Equal(t, "freight_settlement", settledStep.ResourceOperations[0].OperationType)
	require.Equal(t, "freight_refund", settledStep.Journals[0].JournalType)

	finalState, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, finalState.Orders, 1)
	require.Len(t, finalState.Cases, 1)
	require.Len(t, finalState.Receipts, 2)
	require.Len(t, finalState.ReceiptLines, 2)
	require.Len(t, finalState.Claims, 1)
	require.Equal(t, "settled", finalState.Orders[0].State)
	require.Equal(t, "settled", finalState.Cases[0].State)
	require.Equal(t, int64(8), finalState.Policy.AcceptedUnits)
	require.Equal(t, int64(2), finalState.Policy.LostUnits)
	require.Equal(t, int64(2), finalState.Policy.RejectedUnits)
	require.Equal(t, int64(20), finalState.Policy.RefundedUnits)
	carrierReceiptFound := false
	for _, receipt := range finalState.Receipts {
		require.NotNil(t, receipt.ResourceOperation)
		if receipt.LiabilityParty == "carrier" {
			carrierReceiptFound = true
			require.NotNil(t, receipt.Journal)
		} else {
			require.Equal(t, "seller", receipt.LiabilityParty)
			require.Nil(t, receipt.Journal)
		}
	}
	require.True(t, carrierReceiptFound)
	require.Equal(t, int64(20), finalState.Claims[0].ClaimAmount)

	finalSupply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "settled", cityOpenWorldV16SupplyOrderState(t, finalSupply, orderCode))
	require.Zero(t, finalSupply.Policy.DeliveryCount, "V22 must not create a legacy V15 delivery")
	finalShipments, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "settled", shipmentForOrder(finalShipments, orderCode).State)
	require.Equal(t, int64(1), finalShipments.Policy.SettledCount)
	require.Zero(t, finalShipments.Policy.ReceiptCount)

	viewerState, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, viewerState.Orders)
	require.Empty(t, viewerState.Cases)
	require.Empty(t, viewerState.Receipts)
	require.Zero(t, viewerState.Policy.Revision)

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v22-freight-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v22-freight-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, finalState, restored)
}

// TestCityOpenWorldV22FreightSettlementSettlesEveryOverflowConsignment proves
// that V22 owns a V18 overflow plan as one multi-case settlement order.  Each
// consignment can be resolved independently, but V15 and the V18 plan do not
// reach their settled successors until every source consignment is closed.
func TestCityOpenWorldV22FreightSettlementSettlesEveryOverflowConsignment(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v22-batch-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(22_220_064)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V22 Batch Settlement", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV22,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

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

	submitAndStep("v22-batch-fund-buyer", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(2_000), "memo": "fund V22 batch settlement buyer",
	})
	submitAndStep("v22-batch-create", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
		"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
		"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(64), "unit_price_units": int64(5)}},
	})
	supply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, supply.Orders, 1)
	orderCode := supply.Orders[0].Code
	submitAndStep("v22-batch-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
	submitAndStep("v22-batch-dispatch", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})

	var readySettlements *service.CityOpenWorldFreightSettlementState
	for attempt := 0; attempt < 64; attempt++ {
		step(fmt.Sprintf("v22-batch-progress-%d", currentTick+1))
		batches, batchErr := cityService.GetCityOpenWorldFreightBatchState(ctx, owner.ID, worldID)
		require.NoError(t, batchErr)
		settlements, settlementErr := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
		require.NoError(t, settlementErr)
		if len(batches.Plans) != 1 || len(batches.Consignments) != 2 || len(settlements.Orders) != 1 || len(settlements.Cases) != 2 {
			continue
		}
		allReady := true
		for _, consignment := range batches.Consignments {
			allReady = allReady && consignment.State == "awaiting_receipt"
		}
		for _, settlementCase := range settlements.Cases {
			allReady = allReady && settlementCase.State == "awaiting_outcome" && settlementCase.TransportState == "awaiting_receipt"
		}
		if allReady {
			readySettlements = settlements
			break
		}
	}
	require.NotNil(t, readySettlements, "V22 did not materialize both actionable V18 settlement cases")
	require.Equal(t, "consignment", readySettlements.Orders[0].SourceKind)
	require.Equal(t, "receiving", readySettlements.Orders[0].State)
	require.Len(t, readySettlements.Lines, 2)

	firstCase := readySettlements.Cases[0]
	firstSettlement := submit("v22-batch-settle-first", service.CityCommandTypeOpenWorldFreightSettlementReceipt, map[string]any{
		"case_code": firstCase.Code, "liability_party": "seller",
		"lines": []map[string]any{{"source_line_no": 1, "accepted_units": int64(32), "lost_units": int64(0), "rejected_units": int64(0)}},
	})
	firstStep := step("v22-batch-settle-first-step")
	require.Len(t, firstStep.Commands, 1)
	require.Equal(t, firstSettlement.ID, firstStep.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusApplied, firstStep.Commands[0].Status)
	require.Len(t, firstStep.ResourceOperations, 1)
	require.Empty(t, firstStep.Journals)

	middleSettlements, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "receiving", middleSettlements.Orders[0].State)
	settledCases := 0
	for _, settlementCase := range middleSettlements.Cases {
		if settlementCase.State == "settled" {
			settledCases++
		}
	}
	require.Equal(t, 1, settledCases)
	middleSupply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "dispatched", cityOpenWorldV16SupplyOrderState(t, middleSupply, orderCode))
	middleBatches, err := cityService.GetCityOpenWorldFreightBatchState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	// A V22 case can be financially resolved before the remaining V18 cases.
	// Predecessor transport custody is intentionally still awaiting receipt
	// until the one V15 order-level settlement fact exists for every case.
	require.Zero(t, middleBatches.Policy.SettledCount)
	for _, consignment := range middleBatches.Consignments {
		require.Equal(t, "awaiting_receipt", consignment.State)
	}
	require.Zero(t, middleBatches.Policy.ReceiptCount)

	secondCaseCode := ""
	for _, settlementCase := range middleSettlements.Cases {
		if settlementCase.State == "awaiting_outcome" {
			secondCaseCode = settlementCase.Code
			break
		}
	}
	require.NotEmpty(t, secondCaseCode)
	secondSettlement := submit("v22-batch-settle-second", service.CityCommandTypeOpenWorldFreightSettlementReceipt, map[string]any{
		"case_code": secondCaseCode, "liability_party": "seller",
		"lines": []map[string]any{{"source_line_no": 1, "accepted_units": int64(32), "lost_units": int64(0), "rejected_units": int64(0)}},
	})
	secondStep := step("v22-batch-settle-second-step")
	require.Len(t, secondStep.Commands, 1)
	require.Equal(t, secondSettlement.ID, secondStep.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusApplied, secondStep.Commands[0].Status)
	require.Len(t, secondStep.ResourceOperations, 1)
	require.Empty(t, secondStep.Journals)

	finalSettlements, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, finalSettlements.Orders, 1)
	require.Len(t, finalSettlements.Cases, 2)
	require.Len(t, finalSettlements.Receipts, 2)
	require.Len(t, finalSettlements.ReceiptLines, 2)
	require.Empty(t, finalSettlements.Claims)
	require.Equal(t, "settled", finalSettlements.Orders[0].State)
	require.Equal(t, int64(64), finalSettlements.Policy.AcceptedUnits)
	require.Zero(t, finalSettlements.Policy.LostUnits)
	require.Zero(t, finalSettlements.Policy.RejectedUnits)
	require.Zero(t, finalSettlements.Policy.RefundedUnits)

	finalSupply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "settled", cityOpenWorldV16SupplyOrderState(t, finalSupply, orderCode))
	require.Zero(t, finalSupply.Policy.DeliveryCount)
	finalBatches, err := cityService.GetCityOpenWorldFreightBatchState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, finalBatches.Plans, 1)
	require.Equal(t, "settled", finalBatches.Plans[0].State)
	require.Equal(t, int64(2), finalBatches.Policy.SettledCount)
	require.Zero(t, finalBatches.Policy.ReceiptCount)
	require.Empty(t, finalBatches.Receipts)

	// Exercise the same no-receipt failure closure through V18's multi-case
	// source. All materialized consignments are voided as a group while their
	// V18 custody remains awaiting_receipt and no old batch receipt is forged.
	submitAndStep("v22-batch-failure-create", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
		"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
		"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(64), "unit_price_units": int64(5)}},
	})
	newSupply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	failureOrderCode := ""
	for _, candidate := range newSupply.Orders {
		if candidate.Code != orderCode {
			failureOrderCode = candidate.Code
			break
		}
	}
	require.NotEmpty(t, failureOrderCode)
	submitAndStep("v22-batch-failure-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": failureOrderCode})
	submitAndStep("v22-batch-failure-dispatch", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": failureOrderCode})

	var failureSettlementOrder service.CityOpenWorldFreightSettlementOrder
	for attempt := 0; attempt < 64; attempt++ {
		step(fmt.Sprintf("v22-batch-failure-progress-%d", currentTick+1))
		state, stateErr := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		for _, candidate := range state.Orders {
			if candidate.OrderCode != failureOrderCode {
				continue
			}
			caseCount := 0
			allAwaiting := true
			for _, settlementCase := range state.Cases {
				if settlementCase.SettlementOrderCode != candidate.Code {
					continue
				}
				caseCount++
				allAwaiting = allAwaiting && settlementCase.State == "awaiting_outcome" && settlementCase.TransportState == "awaiting_receipt"
			}
			if caseCount == 2 && allAwaiting {
				failureSettlementOrder = candidate
			}
		}
		if failureSettlementOrder.Code != "" {
			break
		}
	}
	require.NotEmpty(t, failureSettlementOrder.Code, "V22 did not materialize all V18 cases for no-receipt failure")

	batchFailureCommand := submit("v22-batch-failure-fail", service.CityCommandTypeOpenWorldSupplyOrderFail, map[string]any{"order_code": failureOrderCode})
	batchFailureStep := step("v22-batch-failure-fail-step")
	require.Len(t, batchFailureStep.Commands, 1)
	require.Equal(t, batchFailureCommand.ID, batchFailureStep.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusApplied, batchFailureStep.Commands[0].Status)
	require.Equal(t, "voided", batchFailureStep.Commands[0].Result["freight_settlement_state"])
	require.Empty(t, batchFailureStep.ResourceOperations)
	require.Len(t, batchFailureStep.Journals, 1)

	afterBatchFailure, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, afterBatchFailure.Orders, 2)
	failedCases := make(map[string]struct{})
	for _, candidate := range afterBatchFailure.Orders {
		if candidate.Code == failureSettlementOrder.Code {
			require.Equal(t, "voided", candidate.State)
		}
	}
	for _, settlementCase := range afterBatchFailure.Cases {
		if settlementCase.SettlementOrderCode == failureSettlementOrder.Code {
			require.Equal(t, "voided", settlementCase.State)
			failedCases[settlementCase.Code] = struct{}{}
		}
	}
	require.Len(t, failedCases, 2)
	for _, receipt := range afterBatchFailure.Receipts {
		_, isFailedCase := failedCases[receipt.CaseCode]
		require.False(t, isFailedCase, "no V22 receipt may be fabricated by the V15 failure closure")
	}
	afterFailureSupply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "failed", cityOpenWorldV16SupplyOrderState(t, afterFailureSupply, failureOrderCode))
	afterFailureBatches, err := cityService.GetCityOpenWorldFreightBatchState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	failurePlanFound := false
	failureConsignments := 0
	for _, plan := range afterFailureBatches.Plans {
		if plan.OrderCode == failureOrderCode {
			failurePlanFound = true
			require.Equal(t, "ready", plan.State)
			for _, consignment := range afterFailureBatches.Consignments {
				if consignment.PlanCode == plan.Code {
					failureConsignments++
					require.Equal(t, "awaiting_receipt", consignment.State)
				}
			}
		}
	}
	require.True(t, failurePlanFound)
	require.Equal(t, 2, failureConsignments)
	require.Equal(t, int64(2), afterFailureBatches.Policy.SettledCount)
	require.Zero(t, afterFailureBatches.Policy.ReceiptCount)

	// Subsequent runtime ticks must not rematerialize cases for the failed
	// order even though the V18 custody records remain visible.
	step("v22-batch-failure-post-void-observation")
	postVoidState, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, afterBatchFailure, postVoidState)

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v22-batch-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v22-batch-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, postVoidState, restored)
}

// TestCityOpenWorldV21UpgradeToV22DoesNotBackfillHistoricalCustody seals the
// forward-only boundary: custody evidence created before the upgrade remains
// governed by V21/V17, while only a later dispatch can enter V22's settlement
// overlay.
func TestCityOpenWorldV21UpgradeToV22DoesNotBackfillHistoricalCustody(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v21-v22-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(21_220_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V21 to V22 Settlement Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV21,
		StyleProfileID:    "cn.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

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
	submitAndStep := func(key, commandType string, payload map[string]any) {
		t.Helper()
		command := submit(key, commandType, payload)
		result := step(key + "-step")
		require.Len(t, result.Commands, 1)
		require.Equal(t, command.ID, result.Commands[0].ID)
		require.Equal(t, service.CityCommandStatusApplied, result.Commands[0].Status)
	}
	knownOrderCodes := make(map[string]struct{})
	createOrder := func(prefix string) string {
		t.Helper()
		submitAndStep(prefix+"-create", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
			"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
			"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5)}},
		})
		supply, supplyErr := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
		require.NoError(t, supplyErr)
		for _, order := range supply.Orders {
			if _, known := knownOrderCodes[order.Code]; !known {
				knownOrderCodes[order.Code] = struct{}{}
				return order.Code
			}
		}
		t.Fatal("missing newly created V15 supply order")
		return ""
	}
	dispatchOrder := func(prefix, orderCode string) {
		t.Helper()
		submitAndStep(prefix+"-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
		submitAndStep(prefix+"-dispatch", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})
	}

	submitAndStep("v21-v22-fund-buyer", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(1_000), "memo": "fund V21/V22 upgrade buyer",
	})
	historicalOrderCode := createOrder("v21-v22-historical")
	dispatchOrder("v21-v22-historical", historicalOrderCode)

	var historicalShipment service.CityOpenWorldEnterpriseFreightShipment
	for attempt := 0; attempt < 64; attempt++ {
		step(fmt.Sprintf("v21-v22-historical-progress-%d", currentTick+1))
		shipments, shipmentErr := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
		require.NoError(t, shipmentErr)
		for _, shipment := range shipments.Shipments {
			if shipment.OrderCode == historicalOrderCode && shipment.State == "awaiting_receipt" {
				historicalShipment = shipment
				break
			}
		}
		if historicalShipment.Code != "" {
			break
		}
	}
	require.NotEmpty(t, historicalShipment.Code, "V21 did not produce a historical V17 receipt boundary")

	upgradeTick := currentTick
	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v21-v22-freight-settlement-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV22,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)
	engine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV22, engine.Version)
	require.NotNil(t, engine.VersionVector)
	require.Equal(t, upgradeTick, engine.VersionVector.BaselineTick)
	contentCatalog := requireCityOpenWorldVersionBinding(t, engine.VersionVector, "content_catalog")
	require.Equal(t, "sub2api-open-world-freight-settlement-catalog", contentCatalog.BundleID)

	settlementAfterUpgrade, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, upgradeTick, settlementAfterUpgrade.Policy.BaselineTick)
	require.Empty(t, settlementAfterUpgrade.Orders)
	require.Empty(t, settlementAfterUpgrade.Cases)
	shipmentsAfterUpgrade, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	legacyFound := false
	for _, shipment := range shipmentsAfterUpgrade.Shipments {
		if shipment.Code == historicalShipment.Code {
			legacyFound = true
			require.Equal(t, "awaiting_receipt", shipment.State)
		}
	}
	require.True(t, legacyFound)

	step("v21-v22-post-upgrade-observation")
	noBackfill, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, noBackfill.Orders)
	require.Empty(t, noBackfill.Cases)

	futureOrderCode := createOrder("v21-v22-future")
	dispatchOrder("v21-v22-future", futureOrderCode)
	var futureSettlement *service.CityOpenWorldFreightSettlementState
	for attempt := 0; attempt < 64; attempt++ {
		step(fmt.Sprintf("v21-v22-future-progress-%d", currentTick+1))
		state, stateErr := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		if len(state.Orders) == 1 && len(state.Cases) == 1 && state.Orders[0].OrderCode == futureOrderCode {
			futureSettlement = state
			break
		}
	}
	require.NotNil(t, futureSettlement, "post-upgrade V17 shipment did not enter V22 settlement overlay")
	require.Greater(t, futureSettlement.Orders[0].SourceTick, upgradeTick)
	require.Equal(t, "awaiting_outcome", futureSettlement.Cases[0].State)
}

// TestCityOpenWorldV22FreightSettlementAllowsOnlyNoReceiptV15Failure proves
// the compatibility escape hatch is safe and narrow. A tracked order can use
// V15's established whole-order failure only before a V22 receipt changes any
// cargo/accounting quantity; after the first receipt it must remain on the
// V22 append-only settlement path.
func TestCityOpenWorldV22FreightSettlementAllowsOnlyNoReceiptV15Failure(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v22-failure-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(22_220_012)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V22 No Receipt Failure", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV22,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

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
	findSettlement := func(state *service.CityOpenWorldFreightSettlementState, orderCode string) (service.CityOpenWorldFreightSettlementOrder, service.CityOpenWorldFreightSettlementCase) {
		t.Helper()
		for _, settlementOrder := range state.Orders {
			if settlementOrder.OrderCode != orderCode {
				continue
			}
			for _, settlementCase := range state.Cases {
				if settlementCase.SettlementOrderCode == settlementOrder.Code {
					return settlementOrder, settlementCase
				}
			}
			t.Fatalf("missing V22 settlement case for order %q", orderCode)
		}
		t.Fatalf("missing V22 settlement order %q", orderCode)
		return service.CityOpenWorldFreightSettlementOrder{}, service.CityOpenWorldFreightSettlementCase{}
	}
	waitForSettlement := func(prefix, orderCode string) (service.CityOpenWorldFreightSettlementOrder, service.CityOpenWorldFreightSettlementCase) {
		t.Helper()
		for attempt := 0; attempt < 64; attempt++ {
			step(fmt.Sprintf("%s-progress-%d", prefix, currentTick+1))
			state, stateErr := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
			require.NoError(t, stateErr)
			for _, settlementOrder := range state.Orders {
				if settlementOrder.OrderCode != orderCode {
					continue
				}
				for _, settlementCase := range state.Cases {
					if settlementCase.SettlementOrderCode == settlementOrder.Code &&
						settlementCase.State == "awaiting_outcome" && settlementCase.TransportState == "awaiting_receipt" {
						return settlementOrder, settlementCase
					}
				}
			}
		}
		t.Fatalf("V22 settlement case for %q did not reach awaiting receipt", orderCode)
		return service.CityOpenWorldFreightSettlementOrder{}, service.CityOpenWorldFreightSettlementCase{}
	}
	createAndDispatch := func(prefix string) string {
		t.Helper()
		submitAndStep(prefix+"-create", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
			"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
			"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5)}},
		})
		supply, stateErr := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		var orderCode string
		for _, order := range supply.Orders {
			if order.CreatedTick == currentTick {
				orderCode = order.Code
				break
			}
		}
		require.NotEmpty(t, orderCode, "missing newly created V15 supply order")
		submitAndStep(prefix+"-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
		submitAndStep(prefix+"-dispatch", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})
		return orderCode
	}

	submitAndStep("v22-failure-fund", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(5_000), "memo": "fund V22 no-receipt failure test",
	})

	noReceiptOrderCode := createAndDispatch("v22-failure-no-receipt")
	noReceiptSettlementOrder, noReceiptSettlementCase := waitForSettlement("v22-failure-no-receipt", noReceiptOrderCode)
	require.Equal(t, "receiving", noReceiptSettlementOrder.State)
	require.Equal(t, "awaiting_outcome", noReceiptSettlementCase.State)

	failCommand := submit("v22-failure-no-receipt-fail", service.CityCommandTypeOpenWorldSupplyOrderFail, map[string]any{"order_code": noReceiptOrderCode})
	failStep := step("v22-failure-no-receipt-fail-step")
	require.Len(t, failStep.Commands, 1)
	require.Equal(t, failCommand.ID, failStep.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusApplied, failStep.Commands[0].Status)
	require.Equal(t, "voided", failStep.Commands[0].Result["freight_settlement_state"])
	require.Equal(t, noReceiptSettlementOrder.Code, failStep.Commands[0].Result["freight_settlement_order_code"])
	require.Empty(t, failStep.ResourceOperations, "no V22 resource operation may exist before any receipt")
	require.Len(t, failStep.Journals, 1, "V15 still performs its existing acceptance reversal")

	failedSettlementState, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	failedSettlementOrder, failedSettlementCase := findSettlement(failedSettlementState, noReceiptOrderCode)
	require.Equal(t, "voided", failedSettlementOrder.State)
	require.Equal(t, "voided", failedSettlementCase.State)
	require.Empty(t, failedSettlementState.Receipts)
	require.Empty(t, failedSettlementState.ReceiptLines)
	require.Empty(t, failedSettlementState.Claims)
	require.Zero(t, failedSettlementState.Policy.AcceptedUnits)
	require.Zero(t, failedSettlementState.Policy.LostUnits)
	require.Zero(t, failedSettlementState.Policy.RejectedUnits)
	require.Zero(t, failedSettlementState.Policy.RefundedUnits)
	failedSupply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "failed", cityOpenWorldV16SupplyOrderState(t, failedSupply, noReceiptOrderCode))
	require.Zero(t, failedSupply.Policy.DeliveryCount)
	failedShipments, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	shipmentStillAwaiting := false
	for _, shipment := range failedShipments.Shipments {
		if shipment.OrderCode == noReceiptOrderCode {
			shipmentStillAwaiting = shipment.State == "awaiting_receipt"
		}
	}
	require.True(t, shipmentStillAwaiting, "V15 failure must not rewrite V17 custody")

	// The successor closure survives later automatic V16/V17/V18 passes. A
	// failed V15 order must not retroactively orphan the frozen V17 custody
	// observation that the zero-receipt V22 void explicitly preserves.
	step("v22-failure-no-receipt-custody-hold")
	shipmentsAfterHold, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	shipmentStillAwaiting = false
	for _, shipment := range shipmentsAfterHold.Shipments {
		if shipment.OrderCode == noReceiptOrderCode {
			shipmentStillAwaiting = shipment.State == "awaiting_receipt"
		}
	}
	require.True(t, shipmentStillAwaiting, "V22 no-receipt void must preserve custody across later automatic ticks")

	partialOrderCode := createAndDispatch("v22-failure-after-partial")
	_, partialSettlementCase := waitForSettlement("v22-failure-after-partial", partialOrderCode)
	partialReceipt := submit("v22-failure-after-partial-receipt", service.CityCommandTypeOpenWorldFreightSettlementReceipt, map[string]any{
		"case_code": partialSettlementCase.Code, "liability_party": "seller",
		"lines": []map[string]any{{"source_line_no": 1, "accepted_units": int64(1), "lost_units": int64(0), "rejected_units": int64(0)}},
	})
	partialStep := step("v22-failure-after-partial-receipt-step")
	require.Len(t, partialStep.Commands, 1)
	require.Equal(t, partialReceipt.ID, partialStep.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusApplied, partialStep.Commands[0].Status)
	require.Len(t, partialStep.ResourceOperations, 1)

	unsafeFail := submit("v22-failure-after-partial-fail", service.CityCommandTypeOpenWorldSupplyOrderFail, map[string]any{"order_code": partialOrderCode})
	unsafeFailStep := step("v22-failure-after-partial-fail-step")
	require.Len(t, unsafeFailStep.Commands, 1)
	require.Equal(t, unsafeFail.ID, unsafeFailStep.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusRejected, unsafeFailStep.Commands[0].Status)
	require.NotNil(t, unsafeFailStep.Commands[0].ErrorCode)
	require.Equal(t, "CITY_SUPPLY_CHAIN_SETTLEMENT_REQUIRED", *unsafeFailStep.Commands[0].ErrorCode)

	afterRejectedFailure, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	partialSettlementOrder, partialSettlementCaseAfter := findSettlement(afterRejectedFailure, partialOrderCode)
	require.Equal(t, "receiving", partialSettlementOrder.State)
	require.Equal(t, "receiving", partialSettlementCaseAfter.State)
	require.Len(t, afterRejectedFailure.Receipts, 1)
	partialSupply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "dispatched", cityOpenWorldV16SupplyOrderState(t, partialSupply, partialOrderCode))

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v22-failure-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	if replay.ErrorCode != nil {
		t.Logf("V22 no-receipt replay error code: %s", *replay.ErrorCode)
	}
	if replay.ErrorDetail != nil {
		t.Logf("V22 no-receipt replay error detail: %s", *replay.ErrorDetail)
	}
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v22-failure-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, afterRejectedFailure, restored)
}
