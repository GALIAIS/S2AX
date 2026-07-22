package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldEnterpriseFreightReceiptPolicy(t *testing.T) CityOpenWorldEnterpriseFreightReceiptPolicy {
	t.Helper()
	hash, err := cityOpenWorldEnterpriseFreightReceiptPolicyHash()
	require.NoError(t, err)
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightReceiptSchemaVersion,
		"scope":          "v16_transport_custody_and_v15_receipt_gate",
		"inventory":      "v15_only_until_delivery",
		"legacy":         "pre_v17_sources_untracked",
	})
	require.NoError(t, err)
	return CityOpenWorldEnterpriseFreightReceiptPolicy{
		ProfileID:                  cityOpenWorldEnterpriseFreightReceiptProfileID,
		ProfileVersion:             cityOpenWorldEnterpriseFreightReceiptProfileVersion,
		ContentHash:                hash,
		BaselineTick:               0,
		ShipmentContract:           cityOpenWorldEnterpriseFreightReceiptShipmentContract,
		ReceiptContract:            cityOpenWorldEnterpriseFreightReceiptReceiptContract,
		LegacyContract:             cityOpenWorldEnterpriseFreightReceiptLegacyContract,
		MaximumShipments:           cityOpenWorldEnterpriseFreightReceiptMaximumShipments,
		MaximumObservationsPerTick: cityOpenWorldEnterpriseFreightReceiptMaximumObservationsTick,
		Revision:                   1,
		Metadata:                   metadata,
	}
}

func newValidCityOpenWorldEnterpriseFreightReceiptState(t *testing.T) *CityOpenWorldEnterpriseFreightReceiptState {
	t.Helper()
	orderCode := "supply.order.freight_receipt_test"
	sourceCode := cityOpenWorldEnterpriseFreightSourceCode(orderCode)
	shipmentCode := cityOpenWorldEnterpriseFreightShipmentCode(sourceCode)
	rootRef := CityOpenWorldEnterpriseFreightReceiptFactRef{
		EvidenceKind: cityOpenWorldEnterpriseFreightReceiptEvidenceFreight, Tick: 2, Sequence: 1,
	}
	pendingRef := CityOpenWorldEnterpriseFreightReceiptFactRef{
		EvidenceKind: cityOpenWorldEnterpriseFreightReceiptEvidenceFreight, Tick: 2, Sequence: 2,
	}
	scheduledRef := CityOpenWorldEnterpriseFreightReceiptFactRef{
		EvidenceKind: cityOpenWorldEnterpriseFreightReceiptEvidenceFreight, Tick: 4, Sequence: 1,
	}
	completedRef := CityOpenWorldEnterpriseFreightReceiptFactRef{
		EvidenceKind: cityOpenWorldEnterpriseFreightReceiptEvidenceFreight, Tick: 6, Sequence: 1,
	}
	receiptRef := CityOpenWorldEnterpriseFreightReceiptFactRef{
		EvidenceKind: cityOpenWorldEnterpriseFreightReceiptEvidenceSupplyChain, Tick: 8, Sequence: 1,
	}
	policy := newValidCityOpenWorldEnterpriseFreightReceiptPolicy(t)
	policy.ShipmentCount = 1
	policy.ReceivedCount = 1
	policy.FactCount = 5
	policy.TransitionCount = 4
	policy.ReceiptCount = 1
	metadata := json.RawMessage(`{"schema_version":1}`)
	return &CityOpenWorldEnterpriseFreightReceiptState{
		Policy: policy,
		Shipments: []CityOpenWorldEnterpriseFreightShipment{{
			Code: shipmentCode, FreightSourceCode: sourceCode, OrderCode: orderCode,
			SellerNodeCode: "supply.node.municipal_services", BuyerNodeCode: "supply.node.openworld_trade_buyer",
			SourceHubCode: "hub.facility.source", DestinationHubCode: "hub.facility.destination",
			SourceTick: 2, RequestedUnits: 12, State: cityOpenWorldEnterpriseFreightReceiptStateReceived,
			SourceEvidence: rootRef, LastFact: receiptRef, Version: 4, Metadata: metadata,
		}},
		Lines: []CityOpenWorldEnterpriseFreightShipmentLine{{
			ShipmentCode: shipmentCode, LineNo: 1, ResourceCode: "basic_material",
			SourceFirmCode: "municipal_services", SourceDistrictCode: "central",
			DestinationFirmCode: "openworld_trade_buyer", DestinationDistrictCode: "central",
			QuantityUnits: 12, UnitPriceUnits: 5, TotalPriceUnits: 60, Metadata: metadata,
		}},
		Facts: []CityOpenWorldEnterpriseFreightReceiptFact{
			{ShipmentCode: shipmentCode, Tick: rootRef.Tick, Sequence: rootRef.Sequence,
				FactType: "shipment.created", EvidenceKind: rootRef.EvidenceKind,
				FreightSourceCode: &sourceCode, Payload: metadata},
			{ShipmentCode: shipmentCode, Tick: pendingRef.Tick, Sequence: pendingRef.Sequence,
				FactType: "route.awaiting", EvidenceKind: pendingRef.EvidenceKind,
				FreightSourceCode: &sourceCode, Payload: metadata},
			{ShipmentCode: shipmentCode, Tick: scheduledRef.Tick, Sequence: scheduledRef.Sequence,
				FactType: "transport.in_transit", EvidenceKind: scheduledRef.EvidenceKind,
				FreightSourceCode: &sourceCode, Payload: metadata},
			{ShipmentCode: shipmentCode, Tick: completedRef.Tick, Sequence: completedRef.Sequence,
				FactType: "transport.arrived", EvidenceKind: completedRef.EvidenceKind,
				FreightSourceCode: &sourceCode, Payload: metadata},
			{ShipmentCode: shipmentCode, Tick: receiptRef.Tick, Sequence: receiptRef.Sequence,
				FactType: "receipt.confirmed", EvidenceKind: receiptRef.EvidenceKind,
				SupplyOrderCode: &orderCode, Payload: metadata},
		},
		Transitions: []CityOpenWorldEnterpriseFreightShipmentTransition{
			{ShipmentCode: shipmentCode, TransitionTick: pendingRef.Tick, TransitionSequence: pendingRef.Sequence,
				State:      cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute,
				ReasonCode: cityOpenWorldEnterpriseFreightReceiptReasonDemandPending, SourceFact: pendingRef, Metadata: metadata},
			{ShipmentCode: shipmentCode, TransitionTick: scheduledRef.Tick, TransitionSequence: scheduledRef.Sequence,
				State:      cityOpenWorldEnterpriseFreightReceiptStateInTransit,
				ReasonCode: cityOpenWorldEnterpriseFreightReceiptReasonScheduled, SourceFact: scheduledRef, Metadata: metadata},
			{ShipmentCode: shipmentCode, TransitionTick: completedRef.Tick, TransitionSequence: completedRef.Sequence,
				State:      cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt,
				ReasonCode: cityOpenWorldEnterpriseFreightReceiptReasonCompleted, SourceFact: completedRef, Metadata: metadata},
			{ShipmentCode: shipmentCode, TransitionTick: receiptRef.Tick, TransitionSequence: receiptRef.Sequence,
				State:      cityOpenWorldEnterpriseFreightReceiptStateReceived,
				ReasonCode: cityOpenWorldEnterpriseFreightReceiptReasonReceived, SourceFact: receiptRef, Metadata: metadata},
		},
		Receipts: []CityOpenWorldEnterpriseFreightReceipt{{
			ShipmentCode: shipmentCode, OrderCode: orderCode, ReceivedTick: 8,
			DeliveryFact:      CityOpenWorldRuntimeFactRef{Tick: 8, Sequence: 1},
			ResourceOperation: CityResourceOperationCursor{Tick: 8, Sequence: 1},
			SourceFact:        receiptRef, Metadata: metadata,
		}},
	}
}

func TestCityOpenWorldEnterpriseFreightReceiptStatePinsCustodyAndDeliveryProof(t *testing.T) {
	state := newValidCityOpenWorldEnterpriseFreightReceiptState(t)
	require.NoError(t, validateCityOpenWorldEnterpriseFreightReceiptState(state))

	brokenLastFact := newValidCityOpenWorldEnterpriseFreightReceiptState(t)
	brokenLastFact.Shipments[0].LastFact = brokenLastFact.Facts[3].factRef()
	require.Error(t, validateCityOpenWorldEnterpriseFreightReceiptState(brokenLastFact))

	brokenReceipt := newValidCityOpenWorldEnterpriseFreightReceiptState(t)
	brokenReceipt.Receipts[0].SourceFact.EvidenceKind = cityOpenWorldEnterpriseFreightReceiptEvidenceFreight
	require.Error(t, validateCityOpenWorldEnterpriseFreightReceiptState(brokenReceipt))

	brokenLine := newValidCityOpenWorldEnterpriseFreightReceiptState(t)
	brokenLine.Lines[0].QuantityUnits = 11
	brokenLine.Lines[0].TotalPriceUnits = 55
	require.Error(t, validateCityOpenWorldEnterpriseFreightReceiptState(brokenLine))
}

func TestCityOpenWorldEnterpriseFreightReceiptStaticCheckpointIgnoresOnlyDerivedEvidence(t *testing.T) {
	state := newValidCityOpenWorldEnterpriseFreightReceiptState(t)
	checkpoint := *state
	checkpoint.Policy.ShipmentCount = 99
	checkpoint.Policy.FactCount = 99
	checkpoint.Policy.TransitionCount = 99
	checkpoint.Policy.ReceiptCount = 99
	checkpoint.Policy.Revision = 99
	require.True(t, cityOpenWorldEnterpriseFreightReceiptStaticCheckpointEqual(state, &checkpoint))

	checkpoint.Policy.ReceiptContract = "changed"
	require.False(t, cityOpenWorldEnterpriseFreightReceiptStaticCheckpointEqual(state, &checkpoint))
}

func TestCityOpenWorldEnterpriseFreightReceiptTransitionFromFreightPinsCustodyAndTerminalKinds(t *testing.T) {
	testCases := []struct {
		name          string
		freightState  string
		shipmentState string
		reasonCode    string
		factType      string
	}{
		{
			name:          "pending awaits route",
			freightState:  cityOpenWorldEnterpriseFreightStateDemandPending,
			shipmentState: cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute,
			reasonCode:    cityOpenWorldEnterpriseFreightReceiptReasonDemandPending,
			factType:      "route.awaiting",
		},
		{
			name:          "scheduled enters transit",
			freightState:  cityOpenWorldEnterpriseFreightStateRouteScheduled,
			shipmentState: cityOpenWorldEnterpriseFreightReceiptStateInTransit,
			reasonCode:    cityOpenWorldEnterpriseFreightReceiptReasonScheduled,
			factType:      "transport.in_transit",
		},
		{
			name:          "completed awaits receipt",
			freightState:  cityOpenWorldEnterpriseFreightStateRouteCompleted,
			shipmentState: cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt,
			reasonCode:    cityOpenWorldEnterpriseFreightReceiptReasonCompleted,
			factType:      "transport.arrived",
		},
		{
			name:          "expired remains unreceived",
			freightState:  cityOpenWorldEnterpriseFreightStateDemandExpired,
			shipmentState: cityOpenWorldEnterpriseFreightReceiptStateExpired,
			reasonCode:    cityOpenWorldEnterpriseFreightReceiptReasonExpired,
			factType:      "transport.expired",
		},
		{
			name:          "voided remains unreceived",
			freightState:  cityOpenWorldEnterpriseFreightStateVoided,
			shipmentState: cityOpenWorldEnterpriseFreightReceiptStateVoided,
			reasonCode:    cityOpenWorldEnterpriseFreightReceiptReasonVoided,
			factType:      "transport.voided",
		},
		{
			name:          "orphaned remains unreceived",
			freightState:  cityOpenWorldEnterpriseFreightStateTransportOrphaned,
			shipmentState: cityOpenWorldEnterpriseFreightReceiptStateOrphaned,
			reasonCode:    cityOpenWorldEnterpriseFreightReceiptReasonOrphaned,
			factType:      "transport.orphaned",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			shipmentState, reasonCode, factType, ok := cityOpenWorldEnterpriseFreightReceiptTransitionFromFreight(testCase.freightState)
			require.True(t, ok)
			require.Equal(t, testCase.shipmentState, shipmentState)
			require.Equal(t, testCase.reasonCode, reasonCode)
			require.Equal(t, testCase.factType, factType)
		})
	}

	shipmentState, reasonCode, factType, ok := cityOpenWorldEnterpriseFreightReceiptTransitionFromFreight("unknown")
	require.False(t, ok)
	require.Empty(t, shipmentState)
	require.Empty(t, reasonCode)
	require.Empty(t, factType)
}

func (fact CityOpenWorldEnterpriseFreightReceiptFact) factRef() CityOpenWorldEnterpriseFreightReceiptFactRef {
	return CityOpenWorldEnterpriseFreightReceiptFactRef{
		EvidenceKind: fact.EvidenceKind,
		Tick:         fact.Tick,
		Sequence:     fact.Sequence,
	}
}
