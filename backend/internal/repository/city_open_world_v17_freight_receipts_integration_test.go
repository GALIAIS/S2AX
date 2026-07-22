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

// TestCityOpenWorldV17EnterpriseFreightReceiptRequiresCustodyBeforeDelivery
// exercises the full V15 -> V16 -> V17 chain against PostgreSQL.  In
// particular it proves that an order cannot mutate inventory until the
// independent freight projection has reached its receipt boundary, and that
// the eventual delivery is linked to exactly one custody receipt.
func TestCityOpenWorldV17EnterpriseFreightReceiptRequiresCustodyBeforeDelivery(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v17-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v17-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(17_170_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V17 Freight Receipts", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV17,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)

	initial, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(0), initial.Policy.BaselineTick)
	require.Empty(t, initial.Shipments)
	require.Empty(t, initial.Facts)
	require.Empty(t, initial.Transitions)
	require.Empty(t, initial.Receipts)
	viewerInitial, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.Zero(t, viewerInitial.Policy.ShipmentCount)
	require.Empty(t, viewerInitial.Shipments)

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
		t.Fatalf("missing V17 freight-receipt shipment for order %q", orderCode)
		return service.CityOpenWorldEnterpriseFreightShipment{}
	}

	submitAndStep("v17-freight-fund-buyer", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(1_000), "memo": "fund V17 freight buyer",
	})
	submitAndStep("v17-freight-create", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
		"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
		"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5)}},
	})
	supply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, supply.Orders, 1)
	orderCode := supply.Orders[0].Code
	submitAndStep("v17-freight-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
	submitAndStep("v17-freight-dispatch", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})

	// V16 observes dispatch only at the next boundary, and V17 consumes the
	// resulting posted V16 evidence after that observer has run.
	noCommandStep := step("v17-freight-source-step")
	require.Empty(t, noCommandStep.Commands)
	pending, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	shipment := shipmentForOrder(pending, orderCode)
	require.Equal(t, "awaiting_route", shipment.State)
	require.Equal(t, int64(12), shipment.RequestedUnits)
	require.Len(t, pending.Lines, 1)
	require.Equal(t, int64(1), pending.Policy.ShipmentCount)
	require.Equal(t, int64(1), pending.Policy.AwaitingRouteCount)
	require.Equal(t, int64(2), pending.Policy.FactCount, "V17 records root source and pending-route evidence")
	require.Equal(t, int64(1), pending.Policy.TransitionCount)

	// A delivery submitted while the shipment is still in route must be a
	// business rejection, not a partial resource transfer or a system error.
	earlyDelivery := submit("v17-freight-deliver-before-receipt", service.CityCommandTypeOpenWorldSupplyOrderDeliver, map[string]any{"order_code": orderCode})
	rejected := step("v17-freight-deliver-before-receipt-step")
	require.Len(t, rejected.Commands, 1)
	require.Equal(t, earlyDelivery.ID, rejected.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusRejected, rejected.Commands[0].Status)
	require.NotNil(t, rejected.Commands[0].ErrorCode)
	require.Equal(t, "CITY_SUPPLY_CHAIN_RECEIPT_NOT_READY", *rejected.Commands[0].ErrorCode)

	var receiptReady *service.CityOpenWorldEnterpriseFreightReceiptState
	for attempt := 0; attempt < 48; attempt++ {
		step(fmt.Sprintf("v17-freight-progress-%d", currentTick+1))
		state, stateErr := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		if shipmentForOrder(state, orderCode).State == "awaiting_receipt" {
			receiptReady = state
			break
		}
	}
	require.NotNil(t, receiptReady, "freight route did not reach the V17 receipt boundary")
	shipment = shipmentForOrder(receiptReady, orderCode)
	require.Equal(t, "awaiting_receipt", shipment.State)
	require.Equal(t, int64(1), receiptReady.Policy.AwaitingReceiptCount)
	require.GreaterOrEqual(t, receiptReady.Policy.FactCount, int64(4))
	require.GreaterOrEqual(t, receiptReady.Policy.TransitionCount, int64(3))
	require.Empty(t, receiptReady.Receipts)

	deliveredStep := submitAndStep("v17-freight-deliver-after-receipt", service.CityCommandTypeOpenWorldSupplyOrderDeliver, map[string]any{"order_code": orderCode})
	require.Len(t, deliveredStep.ResourceOperations, 1)
	finalSupply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "delivered", cityOpenWorldV16SupplyOrderState(t, finalSupply, orderCode))
	require.Equal(t, int64(1), finalSupply.Policy.DeliveryCount)

	finalReceipt, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	shipment = shipmentForOrder(finalReceipt, orderCode)
	require.Equal(t, "received", shipment.State)
	require.Equal(t, int64(1), finalReceipt.Policy.ReceivedCount)
	require.Equal(t, int64(1), finalReceipt.Policy.ReceiptCount)
	require.Len(t, finalReceipt.Receipts, 1)
	require.Equal(t, orderCode, finalReceipt.Receipts[0].OrderCode)
	require.Equal(t, shipment.Code, finalReceipt.Receipts[0].ShipmentCode)
	require.Equal(t, deliveredStep.Tick.Tick, finalReceipt.Receipts[0].ReceivedTick)

	viewerFinal, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.Zero(t, viewerFinal.Policy.ShipmentCount)
	require.Empty(t, viewerFinal.Shipments)
	require.Empty(t, viewerFinal.Receipts)

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v17-freight-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v17-freight-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	recoveryCode, recoveryDetail := "", ""
	if recovery.ErrorCode != nil {
		recoveryCode = *recovery.ErrorCode
	}
	if recovery.ErrorDetail != nil {
		recoveryDetail = *recovery.ErrorDetail
	}
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "code=%s detail=%s", recoveryCode, recoveryDetail)
	restored, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, finalReceipt, restored)
}

// TestCityOpenWorldV16UpgradeToV17KeepsPreBaselineFreightLegacy verifies the
// explicit compatibility contract: V17 does not invent custody history for a
// source that already existed before the upgrade, and therefore does not
// strand its valid V15 delivery behind a receipt that can never exist.
func TestCityOpenWorldV16UpgradeToV17KeepsPreBaselineFreightLegacy(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v16-v17-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(17_170_002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V16 to V17 Legacy Freight", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV16,
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
	submitAndStep := func(key, commandType string, payload map[string]any) *service.CityStepResult {
		t.Helper()
		raw, marshalErr := json.Marshal(payload)
		require.NoError(t, marshalErr)
		command, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: raw, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, submitErr)
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key + "-step", ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.NotNil(t, result.Tick)
		require.Equal(t, currentTick+1, result.Tick.Tick)
		require.Len(t, result.Commands, 1)
		require.Equal(t, command.ID, result.Commands[0].ID)
		require.Equal(t, service.CityCommandStatusApplied, result.Commands[0].Status)
		currentTick = result.Tick.Tick
		return result
	}
	step := func(key string) *service.CityStepResult {
		t.Helper()
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.NotNil(t, result.Tick)
		require.Equal(t, currentTick+1, result.Tick.Tick)
		require.Empty(t, result.Commands)
		currentTick = result.Tick.Tick
		return result
	}

	submitAndStep("v16-v17-fund-buyer", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(1_000), "memo": "fund legacy freight buyer",
	})
	submitAndStep("v16-v17-create", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
		"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
		"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5)}},
	})
	supply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, supply.Orders, 1)
	orderCode := supply.Orders[0].Code
	submitAndStep("v16-v17-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
	submitAndStep("v16-v17-dispatch", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})
	step("v16-v17-observe-source")

	freightBefore, err := cityService.GetCityOpenWorldEnterpriseFreightState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	legacySource := cityOpenWorldV16EnterpriseFreightSource(t, freightBefore, orderCode)
	require.Equal(t, currentTick, legacySource.SourceTick)

	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v16-v17-receipt-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV17,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)
	info, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV17, info.Version)

	receipts, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, currentTick, receipts.Policy.BaselineTick)
	require.Empty(t, receipts.Shipments)
	require.Empty(t, receipts.Receipts)

	// The source was sealed before V17's baseline. It remains deliverable by
	// the original V15 contract even if V16's route observation has not
	// completed, because V17 deliberately has no receipt chain for it.
	submitAndStep("v16-v17-deliver-legacy", service.CityCommandTypeOpenWorldSupplyOrderDeliver, map[string]any{"order_code": orderCode})
	finalSupply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "delivered", cityOpenWorldV16SupplyOrderState(t, finalSupply, orderCode))
	require.Equal(t, int64(1), finalSupply.Policy.DeliveryCount)
	finalReceipts, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, finalReceipts.Shipments)
	require.Empty(t, finalReceipts.Receipts)
}

// TestCityOpenWorldV17EnterpriseFreightReceiptOrphansTerminalCargo covers a
// non-receipt terminal path. The V15 failure races with transport that has
// already been scheduled, so V16 records transport_orphaned and V17 must
// preserve that custody state without creating a receipt or inventory transfer.
func TestCityOpenWorldV17EnterpriseFreightReceiptOrphansTerminalCargo(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v17-orphan-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(17_170_003)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V17 Freight Receipt Orphan", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV17,
		StyleProfileID:    "cn.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	var buyerEntityID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_economic_entities
WHERE world_id = $1 AND entity_type = 'firm' AND code = 'openworld_trade_buyer'`, worldID).Scan(&buyerEntityID))

	currentTick := int64(0)
	type commandSpec struct {
		commandType string
		payload     map[string]any
	}
	submitBatchAndStep := func(key string, specs []commandSpec) *service.CityStepResult {
		t.Helper()
		require.NotEmpty(t, specs)
		commands := make([]*service.CityCommand, 0, len(specs))
		for index, spec := range specs {
			raw, marshalErr := json.Marshal(spec.payload)
			require.NoError(t, marshalErr)
			command, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
				UserID: owner.ID, WorldID: worldID, IdempotencyKey: fmt.Sprintf("%s-%d", key, index),
				CommandType: spec.commandType, Payload: raw, ExpectedWorldTick: &currentTick,
			})
			require.NoError(t, submitErr)
			commands = append(commands, command)
		}
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key + "-step", ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.NotNil(t, result.Tick)
		require.Equal(t, currentTick+1, result.Tick.Tick)
		require.Len(t, result.Commands, len(commands))
		for index, command := range result.Commands {
			require.Equal(t, commands[index].ID, command.ID)
			require.Equal(t, service.CityCommandStatusApplied, command.Status)
		}
		currentTick = result.Tick.Tick
		return result
	}
	step := func(key string) *service.CityStepResult {
		t.Helper()
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.NotNil(t, result.Tick)
		require.Equal(t, currentTick+1, result.Tick.Tick)
		require.Empty(t, result.Commands)
		currentTick = result.Tick.Tick
		return result
	}
	shipmentForSource := func(state *service.CityOpenWorldEnterpriseFreightReceiptState, sourceCode string) service.CityOpenWorldEnterpriseFreightShipment {
		t.Helper()
		for _, shipment := range state.Shipments {
			if shipment.FreightSourceCode == sourceCode {
				return shipment
			}
		}
		t.Fatalf("missing V17 freight-receipt shipment for V16 source %q", sourceCode)
		return service.CityOpenWorldEnterpriseFreightShipment{}
	}

	submitBatchAndStep("v17-orphan-fund", []commandSpec{{
		commandType: service.CityCommandTypeLedgerSubsidy,
		payload: map[string]any{
			"recipient_entity_id": buyerEntityID, "amount_units": int64(1_000), "memo": "fund V17 orphan buyer",
		},
	}})
	// The first order consumes the frozen 32-unit cargo edge. Once its demand
	// is scheduled, the remaining order stays pending and can follow V17's
	// deterministic void path.
	submitBatchAndStep("v17-orphan-create", []commandSpec{
		{
			commandType: service.CityCommandTypeOpenWorldSupplyOrderCreate,
			payload: map[string]any{
				"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
				"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(32), "unit_price_units": int64(5)}},
			},
		},
		{
			commandType: service.CityCommandTypeOpenWorldSupplyOrderCreate,
			payload: map[string]any{
				"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
				"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5)}},
			},
		},
	})
	supply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, supply.Orders, 2)
	accepts := make([]commandSpec, 0, len(supply.Orders))
	dispatches := make([]commandSpec, 0, len(supply.Orders))
	for _, order := range supply.Orders {
		accepts = append(accepts, commandSpec{commandType: service.CityCommandTypeOpenWorldSupplyOrderAccept, payload: map[string]any{"order_code": order.Code}})
		dispatches = append(dispatches, commandSpec{commandType: service.CityCommandTypeOpenWorldSupplyOrderDispatch, payload: map[string]any{"order_code": order.Code}})
	}
	submitBatchAndStep("v17-orphan-accept", accepts)
	submitBatchAndStep("v17-orphan-dispatch", dispatches)
	step("v17-orphan-source")
	// Let V9 consume the 32-unit demand once. This establishes a stable pending
	// source independent of command ordering inside the next tick.
	step("v17-orphan-schedule-leading-demand")

	freight, err := cityService.GetCityOpenWorldEnterpriseFreightState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	mobility, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	var pendingSource service.CityOpenWorldEnterpriseFreightSource
	for _, source := range freight.Sources {
		require.NotNil(t, source.DemandCode)
		demand := cityOpenWorldV16MobilityDemand(t, mobility, *source.DemandCode)
		if demand.Status == "pending" {
			pendingSource = source
			break
		}
	}
	require.NotEmpty(t, pendingSource.Code, "expected one demand to remain capacity-blocked")
	require.Equal(t, "demand_pending", pendingSource.State)
	receiptsBefore, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "awaiting_route", shipmentForSource(receiptsBefore, pendingSource.Code).State)

	submitBatchAndStep("v17-orphan-order-fail", []commandSpec{{
		commandType: service.CityCommandTypeOpenWorldSupplyOrderFail,
		payload:     map[string]any{"order_code": pendingSource.OrderCode},
	}})
	step("v17-orphan-reconcile")

	freight, err = cityService.GetCityOpenWorldEnterpriseFreightState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	orphanedSource := cityOpenWorldV16EnterpriseFreightSource(t, freight, pendingSource.OrderCode)
	require.Equal(t, "transport_orphaned", orphanedSource.State)
	terminalReceipts, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	orphanedShipment := shipmentForSource(terminalReceipts, orphanedSource.Code)
	require.Equal(t, "orphaned", orphanedShipment.State)
	require.Equal(t, int64(1), terminalReceipts.Policy.OrphanedCount)
	require.Empty(t, terminalReceipts.Receipts)
	supply, err = cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "failed", cityOpenWorldV16SupplyOrderState(t, supply, pendingSource.OrderCode))
	require.Zero(t, supply.Policy.DeliveryCount)

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v17-orphan-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v17-orphan-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldEnterpriseFreightReceiptState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, terminalReceipts, restored)
}
