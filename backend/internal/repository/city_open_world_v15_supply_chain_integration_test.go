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

func TestCityOpenWorldV15SupplyChainIsFactBackedRecoverable(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v15-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v15-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(15_150_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V15 Supply Chain", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV15,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)

	initial, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(0), initial.Policy.BaselineTick)
	require.Equal(t, int64(2), initial.Policy.NodeCount)
	require.Len(t, initial.Nodes, 2)
	require.Empty(t, initial.Orders)
	require.Empty(t, initial.Facts)
	require.Equal(t, cityOpenWorldV15SupplyChainNodeCodes(), cityOpenWorldV15SupplyChainNodeCodesFromState(initial))

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

	// The frozen buyer node has no implicit cash grant. Funding it through the
	// existing F2 subsidy command proves that the order contract consumes the
	// authoritative ledger instead of maintaining a parallel balance.
	submitAndStep("v15-supply-fund-buyer", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(1_000), "memo": "fund V15 buyer",
	})
	submitAndStep("v15-supply-create", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
		"buyer_node_code":  "supply.node.openworld_trade_buyer",
		"seller_node_code": "supply.node.municipal_services",
		"lines": []map[string]any{{
			"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5),
		}},
	})

	proposed, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, proposed.Orders, 1)
	orderCode := proposed.Orders[0].Code
	require.Equal(t, "proposed", cityOpenWorldV15SupplyChainCurrentState(t, proposed, orderCode))
	require.Equal(t, int64(1), proposed.Policy.OrderCount)
	require.Equal(t, int64(1), proposed.Policy.ActiveOrderCount)
	require.Equal(t, int64(1), proposed.Policy.FactCount)
	viewerProjection, err := cityService.GetCityOpenWorldSupplyChainState(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.Zero(t, viewerProjection.Policy.NodeCount)
	require.Zero(t, viewerProjection.Policy.OrderCount)
	require.Zero(t, viewerProjection.Policy.FactCount)
	require.Empty(t, viewerProjection.Nodes)
	require.Empty(t, viewerProjection.Orders)
	require.Empty(t, viewerProjection.Facts)

	submitAndStep("v15-supply-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
	accepted, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "accepted", cityOpenWorldV15SupplyChainCurrentState(t, accepted, orderCode))
	require.Len(t, accepted.Reservations, 1)
	require.Len(t, accepted.Settlements, 1)
	require.Equal(t, "acceptance", accepted.Settlements[0].SettlementKind)

	submitAndStep("v15-supply-dispatch", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})
	dispatched, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "dispatched", cityOpenWorldV15SupplyChainCurrentState(t, dispatched, orderCode))
	require.Len(t, dispatched.Dispatches, 1)
	require.Equal(t, currentTick, dispatched.Dispatches[0].DispatchedTick)

	deliveryStep := submitAndStep("v15-supply-deliver", service.CityCommandTypeOpenWorldSupplyOrderDeliver, map[string]any{"order_code": orderCode})
	require.Len(t, deliveryStep.ResourceOperations, 1)
	require.Equal(t, "transfer", deliveryStep.ResourceOperations[0].OperationType)
	final, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "delivered", cityOpenWorldV15SupplyChainCurrentState(t, final, orderCode))
	require.Equal(t, int64(1), final.Policy.OrderCount)
	require.Zero(t, final.Policy.ActiveOrderCount)
	require.Equal(t, int64(4), final.Policy.FactCount)
	require.Equal(t, int64(1), final.Policy.ReservationCount)
	require.Equal(t, int64(1), final.Policy.ReleaseCount)
	require.Equal(t, int64(1), final.Policy.DispatchCount)
	require.Equal(t, int64(1), final.Policy.DeliveryCount)
	require.Equal(t, int64(1), final.Policy.SettlementCount)
	require.Len(t, final.Releases, 1)
	require.Equal(t, "delivered", final.Releases[0].ReasonCode)
	require.Len(t, final.Deliveries, 1)

	physical, err := cityService.GetPhysicalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(988), cityOpenWorldV15InventoryQuantity(t, physical, "municipal_services", "basic_material"))
	require.Equal(t, int64(12), cityOpenWorldV15InventoryQuantity(t, physical, "openworld_trade_buyer", "basic_material"))

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v15-supply-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equalf(t, service.CityReplayStatusVerified, replay.Status,
		"replay=%+v divergence_path=%q error_detail=%q",
		replay, cityOpenWorldV15StringPointer(replay.DivergencePath), cityOpenWorldV15StringPointer(replay.ErrorDetail))
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v15-supply-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, final, restored)
}

func TestCityOpenWorldV15SupplyChainTerminalPathsReleaseAndReverseExactlyOnce(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v15-terminal-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(15_150_003)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V15 Supply Terminal Paths", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV15,
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
	submitAndStep := func(key, commandType string, payload map[string]any) {
		t.Helper()
		raw, marshalErr := json.Marshal(payload)
		require.NoError(t, marshalErr)
		_, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
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
		currentTick = result.Tick.Tick
	}
	stepWithoutCommand := func(key string) {
		t.Helper()
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.NotNil(t, result.Tick)
		require.Equal(t, currentTick+1, result.Tick.Tick)
		currentTick = result.Tick.Tick
	}
	createOrder := func(key string) string {
		t.Helper()
		before, stateErr := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		known := make(map[string]struct{}, len(before.Orders))
		for _, order := range before.Orders {
			known[order.Code] = struct{}{}
		}
		submitAndStep(key+"-create", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
			"buyer_node_code":  "supply.node.openworld_trade_buyer",
			"seller_node_code": "supply.node.municipal_services",
			"lines": []map[string]any{{
				"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5),
			}},
		})
		after, stateErr := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		for _, order := range after.Orders {
			if _, exists := known[order.Code]; !exists {
				return order.Code
			}
		}
		t.Fatalf("new V15 supply-chain order was not found")
		return ""
	}
	orderDeadline := func(orderCode string, dispatch bool) int64 {
		t.Helper()
		state, stateErr := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		for _, order := range state.Orders {
			if order.Code == orderCode {
				if dispatch {
					return order.DispatchDeadlineTick
				}
				return order.AcceptDeadlineTick
			}
		}
		t.Fatalf("V15 supply-chain order %q was not found", orderCode)
		return 0
	}
	orderEvidence := func(orderCode string) (reservations, releases, settlements int) {
		t.Helper()
		state, stateErr := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		for _, item := range state.Reservations {
			if item.OrderCode == orderCode {
				reservations++
			}
		}
		for _, item := range state.Releases {
			if item.OrderCode == orderCode {
				releases++
			}
		}
		for _, item := range state.Settlements {
			if item.OrderCode == orderCode {
				settlements++
			}
		}
		return reservations, releases, settlements
	}
	assertState := func(orderCode, expected string, expectedReservations, expectedReleases, expectedSettlements int) {
		t.Helper()
		state, stateErr := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		require.Equal(t, expected, cityOpenWorldV15SupplyChainCurrentState(t, state, orderCode))
		reservations, releases, settlements := orderEvidence(orderCode)
		require.Equal(t, expectedReservations, reservations)
		require.Equal(t, expectedReleases, releases)
		require.Equal(t, expectedSettlements, settlements)
	}

	submitAndStep("v15-terminal-fund-buyer", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(5_000), "memo": "fund V15 terminal paths",
	})

	proposedCancel := createOrder("v15-terminal-proposed-cancel")
	submitAndStep("v15-terminal-proposed-cancel-action", service.CityCommandTypeOpenWorldSupplyOrderCancel, map[string]any{"order_code": proposedCancel})
	assertState(proposedCancel, "cancelled", 0, 0, 0)

	acceptedCancel := createOrder("v15-terminal-accepted-cancel")
	submitAndStep("v15-terminal-accepted-cancel-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": acceptedCancel})
	submitAndStep("v15-terminal-accepted-cancel-action", service.CityCommandTypeOpenWorldSupplyOrderCancel, map[string]any{"order_code": acceptedCancel})
	assertState(acceptedCancel, "cancelled", 1, 1, 2)

	failedOrder := createOrder("v15-terminal-failed")
	submitAndStep("v15-terminal-failed-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": failedOrder})
	submitAndStep("v15-terminal-failed-dispatch", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": failedOrder})
	submitAndStep("v15-terminal-failed-action", service.CityCommandTypeOpenWorldSupplyOrderFail, map[string]any{"order_code": failedOrder})
	assertState(failedOrder, "failed", 1, 1, 2)

	proposedExpiry := createOrder("v15-terminal-proposed-expiry")
	for currentTick <= orderDeadline(proposedExpiry, false) {
		stepWithoutCommand("v15-terminal-proposed-expiry-step-" + strconv.FormatInt(currentTick+1, 10))
	}
	assertState(proposedExpiry, "expired", 0, 0, 0)

	acceptedExpiry := createOrder("v15-terminal-accepted-expiry")
	submitAndStep("v15-terminal-accepted-expiry-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": acceptedExpiry})
	for currentTick <= orderDeadline(acceptedExpiry, true) {
		stepWithoutCommand("v15-terminal-accepted-expiry-step-" + strconv.FormatInt(currentTick+1, 10))
	}
	assertState(acceptedExpiry, "expired", 1, 1, 2)

	final, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(5), final.Policy.OrderCount)
	require.Zero(t, final.Policy.ActiveOrderCount)
	require.Equal(t, int64(14), final.Policy.FactCount)
	require.Equal(t, int64(3), final.Policy.ReservationCount)
	require.Equal(t, int64(3), final.Policy.ReleaseCount)
	require.Equal(t, int64(1), final.Policy.DispatchCount)
	require.Zero(t, final.Policy.DeliveryCount)
	require.Equal(t, int64(6), final.Policy.SettlementCount)
	physical, err := cityService.GetPhysicalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(1_000), cityOpenWorldV15InventoryQuantity(t, physical, "municipal_services", "basic_material"))
	require.Equal(t, int64(0), cityOpenWorldV15InventoryQuantity(t, physical, "openworld_trade_buyer", "basic_material"))

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v15-terminal-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equalf(t, service.CityReplayStatusVerified, replay.Status,
		"replay=%+v divergence_path=%q error_detail=%q",
		replay, cityOpenWorldV15StringPointer(replay.DivergencePath), cityOpenWorldV15StringPointer(replay.ErrorDetail))
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v15-terminal-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, final, restored)
}

func TestCityOpenWorldV14UpgradeToV15PreservesSealedPredecessorEvidence(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v14-v15-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(15_150_002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V14 to V15 Supply Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV14,
		StyleProfileID:    "cn.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	lifecycleBefore, err := cityService.GetCityOpenWorldCommuteLifecycleState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v14-v15-supply-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV15,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)

	lifecycleAfter, err := cityService.GetCityOpenWorldCommuteLifecycleState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, lifecycleBefore, lifecycleAfter)
	supply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, supply.Nodes, 2)
	require.Empty(t, supply.Orders)
	require.Empty(t, supply.Facts)
	require.Equal(t, int64(2), supply.Policy.NodeCount)
}

func cityOpenWorldV15SupplyChainNodeCodes() []string {
	return []string{"supply.node.municipal_services", "supply.node.openworld_trade_buyer"}
}

func cityOpenWorldV15SupplyChainNodeCodesFromState(state *service.CityOpenWorldSupplyChainState) []string {
	codes := make([]string, 0, len(state.Nodes))
	for _, node := range state.Nodes {
		codes = append(codes, node.Code)
	}
	return codes
}

func cityOpenWorldV15SupplyChainCurrentState(
	t *testing.T,
	state *service.CityOpenWorldSupplyChainState,
	orderCode string,
) string {
	t.Helper()
	var current *service.CityOpenWorldSupplyChainOrderTransition
	for index := range state.Transitions {
		transition := &state.Transitions[index]
		if transition.OrderCode != orderCode {
			continue
		}
		if current == nil || transition.TransitionTick > current.TransitionTick ||
			(transition.TransitionTick == current.TransitionTick && transition.TransitionSequence > current.TransitionSequence) {
			current = transition
		}
	}
	require.NotNil(t, current, "supply-chain transition for %q was not found", orderCode)
	return current.State
}

func cityOpenWorldV15InventoryQuantity(
	t *testing.T,
	state *service.CityPhysicalState,
	entityCode, resourceCode string,
) int64 {
	t.Helper()
	for _, balance := range state.Inventories {
		if balance.EntityCode == entityCode && balance.ResourceCode == resourceCode && balance.Status == "active" {
			return balance.QuantityUnits
		}
	}
	t.Fatalf("inventory %s/%s was not found", entityCode, resourceCode)
	return 0
}

func cityOpenWorldV15StringPointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
