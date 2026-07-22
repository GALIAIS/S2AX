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

func TestCityOpenWorldV16EnterpriseFreightObservesTransportWithoutDelivery(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v16-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v16-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(16_160_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V16 Enterprise Freight", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV16,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)

	initial, err := cityService.GetCityOpenWorldEnterpriseFreightState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(0), initial.Policy.BaselineTick)
	require.Empty(t, initial.Sources)
	require.Empty(t, initial.Facts)
	require.Empty(t, initial.Transitions)
	viewerState, err := cityService.GetCityOpenWorldEnterpriseFreightState(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.Zero(t, viewerState.Policy.SourceCount)
	require.Empty(t, viewerState.Sources)

	var carrierLocationCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_open_world_actor_locations location
JOIN city_open_world_actors actor
  ON actor.id = location.actor_id AND actor.world_id = location.world_id
WHERE actor.world_id = $1 AND actor.code = 'system.freight.carrier'`, worldID).Scan(&carrierLocationCount))
	require.Zero(t, carrierLocationCount)

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

	submitAndStep("v16-freight-fund-buyer", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(1_000), "memo": "fund V16 freight buyer",
	})
	submitAndStep("v16-freight-create", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
		"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
		"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5)}},
	})
	supply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, supply.Orders, 1)
	orderCode := supply.Orders[0].Code
	submitAndStep("v16-freight-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
	submitAndStep("v16-freight-dispatch", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})

	dispatched, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "dispatched", cityOpenWorldV16SupplyOrderState(t, dispatched, orderCode))
	require.Equal(t, int64(1), dispatched.Policy.DispatchCount)
	require.Zero(t, dispatched.Policy.DeliveryCount)

	// The V16 pass runs at the next tick. It cannot create same-tick transport
	// evidence for the V15 dispatch and it cannot schedule the newly-created
	// V9 demand until another tick has elapsed.
	step("v16-freight-source-step")
	pending, err := cityService.GetCityOpenWorldEnterpriseFreightState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, pending.Sources, 1)
	source := cityOpenWorldV16EnterpriseFreightSource(t, pending, orderCode)
	require.Equal(t, "demand_pending", source.State)
	require.Equal(t, int64(12), source.RequestedUnits)
	require.Equal(t, currentTick, source.SourceTick)
	require.NotNil(t, source.DemandCode)
	require.Nil(t, source.RouteCode)
	require.Len(t, pending.Lines, 1)
	require.Len(t, pending.Facts, 2)
	require.Len(t, pending.Transitions, 1)
	require.Equal(t, int64(1), pending.Policy.PendingCount)
	require.Equal(t, int64(1), pending.Policy.DemandCount)

	mobility, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	demand := cityOpenWorldV16MobilityDemand(t, mobility, *source.DemandCode)
	require.Equal(t, "pending", demand.Status)
	require.Equal(t, "freight", demand.ModeCode)
	require.Equal(t, "enterprise.freight", demand.PurposeCode)
	var demandMetadata map[string]any
	require.NoError(t, json.Unmarshal(demand.Metadata, &demandMetadata))
	adapter, ok := demandMetadata["transport_adapter"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "enterprise_freight_v1", adapter["kind"])
	require.Equal(t, "excluded", adapter["arrival_bridge"])

	observedScheduled := false
	for attempt := 0; attempt < 40; attempt++ {
		step(fmt.Sprintf("v16-freight-progress-%d", currentTick+1))
		state, stateErr := cityService.GetCityOpenWorldEnterpriseFreightState(ctx, owner.ID, worldID)
		require.NoError(t, stateErr)
		source = cityOpenWorldV16EnterpriseFreightSource(t, state, orderCode)
		if source.State == "route_scheduled" {
			observedScheduled = true
		}
		if source.State == "route_completed" {
			pending = state
			break
		}
	}
	require.True(t, observedScheduled)
	source = cityOpenWorldV16EnterpriseFreightSource(t, pending, orderCode)
	require.Equal(t, "route_completed", source.State)
	require.NotNil(t, source.RouteCode)
	require.GreaterOrEqual(t, pending.Policy.CompletedCount, int64(1))

	arrivals, err := cityService.GetCityOpenWorldMobilityArrivalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	for _, arrival := range arrivals.Arrivals {
		require.NotEqual(t, *source.DemandCode, arrival.DemandCode)
	}
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_open_world_actor_locations location
JOIN city_open_world_actors actor
  ON actor.id = location.actor_id AND actor.world_id = location.world_id
WHERE actor.world_id = $1 AND actor.code = 'system.freight.carrier'`, worldID).Scan(&carrierLocationCount))
	require.Zero(t, carrierLocationCount)

	afterTransport, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "dispatched", cityOpenWorldV16SupplyOrderState(t, afterTransport, orderCode))
	require.Zero(t, afterTransport.Policy.DeliveryCount, "route completion must not deliver the V15 order")

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v16-freight-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v16-freight-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldEnterpriseFreightState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, pending, restored)
}

func TestCityOpenWorldV16EnterpriseFreightVoidsPendingDemandAfterTerminalOrder(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v16-void-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(16_160_002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V16 Freight Terminal", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV16,
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
	submitBatchAndStep := func(key string, specs []commandSpec) {
		t.Helper()
		require.NotEmpty(t, specs)
		for index, spec := range specs {
			raw, marshalErr := json.Marshal(spec.payload)
			require.NoError(t, marshalErr)
			_, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
				UserID: owner.ID, WorldID: worldID, IdempotencyKey: fmt.Sprintf("%s-%d", key, index),
				CommandType: spec.commandType, Payload: raw, ExpectedWorldTick: &currentTick,
			})
			require.NoError(t, submitErr)
		}
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key + "-step", ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.Len(t, result.Commands, len(specs))
		for _, command := range result.Commands {
			require.Equal(t, service.CityCommandStatusApplied, command.Status)
		}
		currentTick = result.Tick.Tick
	}
	step := func(key string) {
		t.Helper()
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		currentTick = result.Tick.Tick
	}

	submitBatchAndStep("v16-void-fund", []commandSpec{{
		commandType: service.CityCommandTypeLedgerSubsidy,
		payload: map[string]any{
			"recipient_entity_id": buyerEntityID, "amount_units": int64(1_000), "memo": "fund V16 terminal buyer",
		},
	}})
	// Both dispatches use the same frozen freight edge. The first exactly fills
	// the 32-unit V9 cargo capacity; the second remains pending long enough to
	// exercise V16's terminal-pending void path through normal public commands.
	submitBatchAndStep("v16-void-create", []commandSpec{
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
	orderCodes := make([]string, 0, len(supply.Orders))
	acceptSpecs := make([]commandSpec, 0, len(supply.Orders))
	for _, order := range supply.Orders {
		orderCodes = append(orderCodes, order.Code)
		acceptSpecs = append(acceptSpecs, commandSpec{
			commandType: service.CityCommandTypeOpenWorldSupplyOrderAccept,
			payload:     map[string]any{"order_code": order.Code},
		})
	}
	submitBatchAndStep("v16-void-accept", acceptSpecs)
	dispatchSpecs := make([]commandSpec, 0, len(orderCodes))
	for _, orderCode := range orderCodes {
		dispatchSpecs = append(dispatchSpecs, commandSpec{
			commandType: service.CityCommandTypeOpenWorldSupplyOrderDispatch,
			payload:     map[string]any{"order_code": orderCode},
		})
	}
	submitBatchAndStep("v16-void-dispatch", dispatchSpecs)
	step("v16-void-source")
	pending, err := cityService.GetCityOpenWorldEnterpriseFreightState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, pending.Sources, 2)
	mobility, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	var source service.CityOpenWorldEnterpriseFreightSource
	for _, candidate := range pending.Sources {
		require.NotNil(t, candidate.DemandCode)
		demand := cityOpenWorldV16MobilityDemand(t, mobility, *candidate.DemandCode)
		if demand.Status == "pending" {
			source = candidate
			break
		}
	}
	require.NotEmpty(t, source.Code, "expected a capacity-blocked freight demand")
	require.Equal(t, "demand_pending", source.State)

	submitBatchAndStep("v16-void-order-fail", []commandSpec{{
		commandType: service.CityCommandTypeOpenWorldSupplyOrderFail,
		payload:     map[string]any{"order_code": source.OrderCode},
	}})
	// Command application is sealed before the subsequent automatic pass sees
	// the terminal V15 transition. Confirm V9 capacity still holds the demand,
	// then advance one reconciliation tick.
	mobility, err = cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "pending", cityOpenWorldV16MobilityDemand(t, mobility, *source.DemandCode).Status)
	step("v16-void-reconcile")
	pending, err = cityService.GetCityOpenWorldEnterpriseFreightState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	source = cityOpenWorldV16EnterpriseFreightSource(t, pending, source.OrderCode)
	require.Equal(t, "voided", source.State)
	require.Equal(t, int64(1), pending.Policy.VoidedCount)
	mobility, err = cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	demand := cityOpenWorldV16MobilityDemand(t, mobility, *source.DemandCode)
	require.Equal(t, "expired", demand.Status)
	require.Nil(t, demand.RouteCode)
	supply, err = cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "failed", cityOpenWorldV16SupplyOrderState(t, supply, source.OrderCode))
	require.Zero(t, supply.Policy.DeliveryCount)
}

func TestCityOpenWorldV16EnterpriseFreightOrphansScheduledDemandAfterTerminalOrder(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v16-orphan-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(16_160_004)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V16 Freight Orphan", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV16,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	var buyerEntityID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_economic_entities
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
		require.Equal(t, currentTick+1, result.Tick.Tick)
		currentTick = result.Tick.Tick
	}
	step := func(key string) {
		t.Helper()
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.Equal(t, currentTick+1, result.Tick.Tick)
		currentTick = result.Tick.Tick
	}

	submitAndStep("v16-orphan-fund", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(1_000), "memo": "fund V16 orphan buyer",
	})
	submitAndStep("v16-orphan-create", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
		"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
		"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5)}},
	})
	supply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, supply.Orders, 1)
	orderCode := supply.Orders[0].Code
	submitAndStep("v16-orphan-accept", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
	submitAndStep("v16-orphan-dispatch", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})
	step("v16-orphan-source")
	freight, err := cityService.GetCityOpenWorldEnterpriseFreightState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	source := cityOpenWorldV16EnterpriseFreightSource(t, freight, orderCode)
	require.Equal(t, "demand_pending", source.State)
	require.NotNil(t, source.DemandCode)

	// The terminal V15 transition is sealed by its command tick. On the next
	// automatic tick V16 first observes V9's scheduled route, then records the
	// terminal transport orphan without deleting historical allocations.
	submitAndStep("v16-orphan-order-fail", service.CityCommandTypeOpenWorldSupplyOrderFail, map[string]any{"order_code": orderCode})
	step("v16-orphan-reconcile")
	freight, err = cityService.GetCityOpenWorldEnterpriseFreightState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	source = cityOpenWorldV16EnterpriseFreightSource(t, freight, orderCode)
	require.Equal(t, "transport_orphaned", source.State)
	require.NotNil(t, source.RouteCode)
	require.Equal(t, int64(1), freight.Policy.OrphanedCount)

	mobility, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	demand := cityOpenWorldV16MobilityDemand(t, mobility, *source.DemandCode)
	require.Contains(t, []string{"scheduled", "completed"}, demand.Status)
	require.NotNil(t, demand.RouteCode)
	supply, err = cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "failed", cityOpenWorldV16SupplyOrderState(t, supply, orderCode))
	require.Zero(t, supply.Policy.DeliveryCount)
}

func TestCityOpenWorldV15UpgradeToV16CreatesOnlyFutureFreightAdapterEvidence(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v15-v16-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(16_160_003)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V15 to V16 Freight Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV15,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	supplyBefore, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v15-v16-freight-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV16,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)
	supplyAfter, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, supplyBefore, supplyAfter)
	freight, err := cityService.GetCityOpenWorldEnterpriseFreightState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, freight.Sources)
	require.Equal(t, int64(0), freight.Policy.BaselineTick)

	var carrierCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_open_world_actors
WHERE world_id = $1 AND code = 'system.freight.carrier'
  AND actor_type_code = 'system.freight_carrier' AND owner_user_id IS NULL`, worldID).Scan(&carrierCount))
	require.Equal(t, 1, carrierCount)
}

func cityOpenWorldV16EnterpriseFreightSource(
	t *testing.T,
	state *service.CityOpenWorldEnterpriseFreightState,
	orderCode string,
) service.CityOpenWorldEnterpriseFreightSource {
	t.Helper()
	for _, source := range state.Sources {
		if source.OrderCode == orderCode {
			return source
		}
	}
	t.Fatalf("V16 enterprise-freight source for %q was not found", orderCode)
	return service.CityOpenWorldEnterpriseFreightSource{}
}

func cityOpenWorldV16MobilityDemand(
	t *testing.T,
	state *service.CityOpenWorldMobilityState,
	code string,
) service.CityOpenWorldMobilityDemand {
	t.Helper()
	for _, demand := range state.Demands {
		if demand.Code == code {
			return demand
		}
	}
	t.Fatalf("V16 enterprise-freight mobility demand %q was not found", code)
	return service.CityOpenWorldMobilityDemand{}
}

func cityOpenWorldV16SupplyOrderState(
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
	require.NotNil(t, current, "V15 transition for %q was not found", orderCode)
	return current.State
}
