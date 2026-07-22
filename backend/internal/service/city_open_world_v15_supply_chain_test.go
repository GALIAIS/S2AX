package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCityOpenWorldSupplyChainCommandCanonicalizesAndRejectsAmbiguity(t *testing.T) {
	value, handled, err := normalizeCityOpenWorldSupplyChainCommand(
		CityCommandTypeOpenWorldSupplyOrderCreate,
		json.RawMessage(`{
			"buyer_node_code":"SUPPLY.NODE.OPENWORLD_TRADE_BUYER",
			"seller_node_code":"SUPPLY.NODE.MUNICIPAL_SERVICES",
			"lines":[
				{"resource_code":"consumer_goods","quantity_units":2,"unit_price_units":7},
				{"resource_code":"BASIC_MATERIAL","quantity_units":3,"unit_price_units":5}
			]
		}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	create, ok := value.(cityOpenWorldSupplyChainOrderCreatePayload)
	require.True(t, ok)
	require.Equal(t, cityOpenWorldSupplyChainBuyerNodeCode, create.BuyerNodeCode)
	require.Equal(t, cityOpenWorldSupplyChainSupplierNodeCode, create.SellerNodeCode)
	require.Equal(t, []cityOpenWorldSupplyChainOrderLinePayload{
		{ResourceCode: "basic_material", QuantityUnits: 3, UnitPriceUnits: 5},
		{ResourceCode: "consumer_goods", QuantityUnits: 2, UnitPriceUnits: 7},
	}, create.Lines)

	_, handled, err = normalizeCityOpenWorldSupplyChainCommand(
		CityCommandTypeOpenWorldSupplyOrderCreate,
		json.RawMessage(`{
			"buyer_node_code":"supply.node.openworld_trade_buyer",
			"seller_node_code":"supply.node.municipal_services",
			"lines":[
				{"resource_code":"basic_material","quantity_units":1,"unit_price_units":1},
				{"resource_code":"BASIC_MATERIAL","quantity_units":1,"unit_price_units":1}
			]
		}`),
	)
	require.True(t, handled)
	require.ErrorIs(t, err, ErrCityInvalidInput)

	value, handled, err = normalizeCityOpenWorldSupplyChainCommand(
		CityCommandTypeOpenWorldSupplyOrderDispatch,
		json.RawMessage(`{"order_code":"SUPPLY.ORDER.EXAMPLE"}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	action, ok := value.(cityOpenWorldSupplyChainOrderActionPayload)
	require.True(t, ok)
	require.Equal(t, "supply.order.example", action.OrderCode)
}

func TestCityOpenWorldSupplyChainStaticCheckpointIgnoresOnlyDerivedEvidence(t *testing.T) {
	metadata := json.RawMessage(`{"schema_version":1}`)
	base := &CityOpenWorldSupplyChainState{
		Policy: CityOpenWorldSupplyChainPolicy{
			ProfileID: cityOpenWorldSupplyChainProfileID, ProfileVersion: cityOpenWorldSupplyChainProfileVersion,
			ContentHash:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			BaselineTick: 0, NodeContract: cityOpenWorldSupplyChainNodeContract,
			OrderContract:             cityOpenWorldSupplyChainOrderContract,
			SettlementContract:        cityOpenWorldSupplyChainSettlementContract,
			DeliveryContract:          cityOpenWorldSupplyChainDeliveryContract,
			MaximumOrders:             cityOpenWorldSupplyChainMaximumOrders,
			MaximumOrderLines:         cityOpenWorldSupplyChainMaximumOrderLines,
			MaximumTransitionsPerTick: cityOpenWorldSupplyChainMaximumTransitionsTick,
			AcceptTimeoutTicks:        cityOpenWorldSupplyChainAcceptTimeoutTicks,
			DispatchTimeoutTicks:      cityOpenWorldSupplyChainDispatchTimeoutTicks,
			NodeCount:                 2, Revision: 1, Metadata: metadata,
		},
		Nodes: []CityOpenWorldSupplyChainNode{
			{Code: cityOpenWorldSupplyChainSupplierNodeCode, FirmCode: cityOpenWorldSupplyChainSupplierFirmCode,
				FacilityCode: "facility.industry", DistrictCode: "central", State: "active", Metadata: metadata},
			{Code: cityOpenWorldSupplyChainBuyerNodeCode, FirmCode: cityOpenWorldSupplyChainBuyerFirmCode,
				FacilityCode: "facility.commerce", DistrictCode: "central", State: "active", Metadata: metadata},
		},
	}
	checkpoint := *base
	checkpoint.Policy.OrderCount = 19
	checkpoint.Policy.ActiveOrderCount = 4
	checkpoint.Policy.FactCount = 81
	checkpoint.Policy.ReservationCount = 7
	checkpoint.Policy.ReleaseCount = 6
	checkpoint.Policy.DispatchCount = 5
	checkpoint.Policy.DeliveryCount = 4
	checkpoint.Policy.SettlementCount = 8
	checkpoint.Policy.Revision = 99
	require.True(t, cityOpenWorldSupplyChainStaticCheckpointEqual(base, &checkpoint))

	checkpoint.Nodes = append([]CityOpenWorldSupplyChainNode(nil), checkpoint.Nodes...)
	checkpoint.Nodes[0].FacilityCode = "facility.changed"
	require.False(t, cityOpenWorldSupplyChainStaticCheckpointEqual(base, &checkpoint))
}

func TestReplayCityOpenWorldSupplyChainInventoryTopologyRestoresLegacyLazyBalance(t *testing.T) {
	state := &cityHashState{
		Physical: cityPhysicalHashState{
			Districts: []cityHashDistrict{{Code: "central", SortOrder: 10}},
			Inventories: []cityHashInventory{{
				EntityType: "firm", EntityCode: cityOpenWorldSupplyChainSupplierFirmCode,
				DistrictCode: "central", ResourceCode: "basic_material",
				OpeningQuantityUnits: 1_000, QuantityUnits: 1_000, Status: "active",
				Metadata: json.RawMessage(`{}`),
			}},
		},
	}
	supplyChain := &CityOpenWorldSupplyChainState{
		Orders: []CityOpenWorldSupplyChainOrder{{Code: "supply.order.1", CreatedTick: 3}},
		Lines: []CityOpenWorldSupplyChainOrderLine{{
			OrderCode: "supply.order.1", LineNo: 1, ResourceCode: "basic_material",
			SourceFirmCode: cityOpenWorldSupplyChainSupplierFirmCode, SourceDistrictCode: "central",
			DestinationFirmCode: cityOpenWorldSupplyChainBuyerFirmCode, DestinationDistrictCode: "central",
			QuantityUnits: 12, UnitPriceUnits: 5, TotalPriceUnits: 60, Metadata: json.RawMessage(`{}`),
		}},
	}

	require.NoError(t, replayCityOpenWorldSupplyChainInventoryTopology(2, state, supplyChain))
	require.Len(t, state.Physical.Inventories, 1, "future orders must not create an inventory during replay")

	require.NoError(t, replayCityOpenWorldSupplyChainInventoryTopology(3, state, supplyChain))
	require.Len(t, state.Physical.Inventories, 2)
	var buyer *cityHashInventory
	for index := range state.Physical.Inventories {
		candidate := &state.Physical.Inventories[index]
		if candidate.EntityCode == cityOpenWorldSupplyChainBuyerFirmCode {
			buyer = candidate
			break
		}
	}
	require.NotNil(t, buyer)
	require.Equal(t, int64(0), buyer.QuantityUnits)
	require.Equal(t, int64(0), buyer.Version)
	require.Equal(t, json.RawMessage(`{}`), buyer.Metadata)

	require.NoError(t, replayCityOpenWorldSupplyChainInventoryTopology(3, state, supplyChain))
	require.Len(t, state.Physical.Inventories, 2, "topology restoration must be idempotent")
}

func TestProjectCityOpenWorldSupplyChainStateForOwnedFirmsScopesRelationsAndCounters(t *testing.T) {
	visibleOrderCode := "supply.order.visible"
	hiddenOrderCode := "supply.order.hidden"
	state := &CityOpenWorldSupplyChainState{
		Policy: CityOpenWorldSupplyChainPolicy{
			NodeCount: 3, OrderCount: 2, ActiveOrderCount: 2, FactCount: 2,
			ReservationCount: 2, ReleaseCount: 2, DispatchCount: 2,
			DeliveryCount: 2, SettlementCount: 2, Revision: 91,
		},
		Nodes: []CityOpenWorldSupplyChainNode{
			{Code: "supply.node.owned", FirmCode: "owned_firm"},
			{Code: "supply.node.counterparty", FirmCode: "counterparty_firm"},
			{Code: "supply.node.hidden", FirmCode: "hidden_firm"},
		},
		Facts: []CityOpenWorldSupplyChainFact{
			{OrderCode: &visibleOrderCode, FactType: "order.proposed"},
			{OrderCode: &hiddenOrderCode, FactType: "order.proposed"},
		},
		Orders: []CityOpenWorldSupplyChainOrder{
			{Code: visibleOrderCode, BuyerNodeCode: "supply.node.owned", SellerNodeCode: "supply.node.counterparty"},
			{Code: hiddenOrderCode, BuyerNodeCode: "supply.node.counterparty", SellerNodeCode: "supply.node.hidden"},
		},
		Lines: []CityOpenWorldSupplyChainOrderLine{
			{OrderCode: visibleOrderCode}, {OrderCode: hiddenOrderCode},
		},
		Transitions: []CityOpenWorldSupplyChainOrderTransition{
			{OrderCode: visibleOrderCode, State: cityOpenWorldSupplyChainStateProposed},
			{OrderCode: hiddenOrderCode, State: cityOpenWorldSupplyChainStateProposed},
		},
		Reservations: []CityOpenWorldSupplyChainReservation{
			{OrderCode: visibleOrderCode}, {OrderCode: hiddenOrderCode},
		},
		Releases: []CityOpenWorldSupplyChainReservationRelease{
			{OrderCode: visibleOrderCode}, {OrderCode: hiddenOrderCode},
		},
		Dispatches: []CityOpenWorldSupplyChainDispatch{
			{OrderCode: visibleOrderCode}, {OrderCode: hiddenOrderCode},
		},
		Deliveries: []CityOpenWorldSupplyChainDelivery{
			{OrderCode: visibleOrderCode}, {OrderCode: hiddenOrderCode},
		},
		Settlements: []CityOpenWorldSupplyChainSettlement{
			{OrderCode: visibleOrderCode}, {OrderCode: hiddenOrderCode},
		},
	}

	view := projectCityOpenWorldSupplyChainStateForOwnedFirms(state, map[string]struct{}{"owned_firm": {}})
	require.Equal(t, int64(2), view.Policy.NodeCount)
	require.Equal(t, int64(1), view.Policy.OrderCount)
	require.Equal(t, int64(1), view.Policy.ActiveOrderCount)
	require.Equal(t, int64(1), view.Policy.FactCount)
	require.Equal(t, int64(1), view.Policy.ReservationCount)
	require.Equal(t, int64(1), view.Policy.ReleaseCount)
	require.Equal(t, int64(1), view.Policy.DispatchCount)
	require.Equal(t, int64(1), view.Policy.DeliveryCount)
	require.Equal(t, int64(1), view.Policy.SettlementCount)
	require.Zero(t, view.Policy.Revision, "global revision must not reveal unrelated activity")
	require.Equal(t, []string{"supply.node.owned", "supply.node.counterparty"}, []string{view.Nodes[0].Code, view.Nodes[1].Code})
	require.Equal(t, []CityOpenWorldSupplyChainOrder{{
		Code: visibleOrderCode, BuyerNodeCode: "supply.node.owned", SellerNodeCode: "supply.node.counterparty",
	}}, view.Orders)
	require.Len(t, view.Facts, 1)
	require.Equal(t, &visibleOrderCode, view.Facts[0].OrderCode)
	require.Len(t, view.Lines, 1)
	require.Len(t, view.Transitions, 1)
	require.Len(t, view.Reservations, 1)
	require.Len(t, view.Releases, 1)
	require.Len(t, view.Dispatches, 1)
	require.Len(t, view.Deliveries, 1)
	require.Len(t, view.Settlements, 1)
}
