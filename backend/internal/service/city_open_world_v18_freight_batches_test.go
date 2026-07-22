package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldFreightBatchPolicy(t *testing.T) CityOpenWorldFreightBatchPolicy {
	t.Helper()
	hash, err := cityOpenWorldFreightBatchPolicyHash()
	require.NoError(t, err)
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldFreightBatchSchemaVersion,
		"scope":          "v16_suppressed_overflow_to_v9_multi_consignment",
		"inventory":      "v15_atomic_delivery_only",
		"legacy":         "pre_v18_overflow_sources_untracked",
	})
	require.NoError(t, err)
	return CityOpenWorldFreightBatchPolicy{
		ProfileID:                  cityOpenWorldFreightBatchProfileID,
		ProfileVersion:             cityOpenWorldFreightBatchProfileVersion,
		ContentHash:                hash,
		BaselineTick:               0,
		SourceContract:             cityOpenWorldFreightBatchSourceContract,
		PackingContract:            cityOpenWorldFreightBatchPackingContract,
		TransportContract:          cityOpenWorldFreightBatchTransportContract,
		ReceiptContract:            cityOpenWorldFreightBatchReceiptContract,
		MaximumUnits:               cityOpenWorldFreightBatchMaximumUnits,
		MaximumConsignmentsPerPlan: cityOpenWorldFreightBatchMaximumConsignmentsPerPlan,
		MaximumPlansPerTick:        cityOpenWorldFreightBatchMaximumPlansPerTick,
		MaximumObservationsPerTick: cityOpenWorldFreightBatchMaximumObservationsPerTick,
		Revision:                   1,
		Metadata:                   metadata,
	}
}

func newValidCityOpenWorldFreightBatchState(t *testing.T) *CityOpenWorldFreightBatchState {
	t.Helper()
	const (
		orderCode  = "supply.order.freight.batch.test"
		sourceCode = "enterprise.freight.source.batch.test"
	)
	planCode := cityOpenWorldFreightBatchPlanCode(sourceCode)
	consignmentOne := cityOpenWorldFreightBatchConsignmentCode(planCode, 1)
	consignmentTwo := cityOpenWorldFreightBatchConsignmentCode(planCode, 2)
	metadata := json.RawMessage(`{"schema_version":1}`)
	planSource := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime, Tick: 1, Sequence: 1}
	firstRoot := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime, Tick: 2, Sequence: 1}
	firstPending := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime, Tick: 2, Sequence: 2}
	firstScheduled := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime, Tick: 3, Sequence: 1}
	firstCompleted := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime, Tick: 4, Sequence: 1}
	secondRoot := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime, Tick: 2, Sequence: 3}
	secondPending := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime, Tick: 2, Sequence: 4}
	secondScheduled := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime, Tick: 3, Sequence: 2}
	secondCompleted := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime, Tick: 5, Sequence: 1}
	receiptOne := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceSupplyChain, Tick: 6, Sequence: 1}
	receiptTwo := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceSupplyChain, Tick: 6, Sequence: 1}
	routeOne, routeTwo := "mobility.route.batch.one", "mobility.route.batch.two"
	policy := newValidCityOpenWorldFreightBatchPolicy(t)
	policy.PlanCount = 1
	policy.ConsignmentCount = 2
	policy.ReceivedCount = 2
	policy.FactCount = 10
	policy.TransitionCount = 8
	policy.ReceiptCount = 2
	return &CityOpenWorldFreightBatchState{
		Policy: policy,
		Plans: []CityOpenWorldFreightBatchPlan{{
			Code: planCode, OverflowSourceCode: sourceCode, OrderCode: orderCode,
			SellerNodeCode: "supply.node.municipal_services", BuyerNodeCode: "supply.node.openworld_trade_buyer",
			SourceHubCode: "hub.facility.source", DestinationHubCode: "hub.facility.destination",
			CarrierActorCode: cityOpenWorldEnterpriseFreightCarrierActorCode, SourceTick: 1, RequiredUnits: 64,
			ConsignmentCount: 2, State: cityOpenWorldFreightBatchPlanStateReceived,
			SourceFact: planSource, LastFact: secondCompleted, Version: 4, Metadata: metadata,
		}},
		Consignments: []CityOpenWorldFreightBatchConsignment{
			{Code: consignmentOne, PlanCode: planCode, BatchNo: 1, RequestedUnits: 32,
				State: cityOpenWorldFreightBatchConsignmentStateReceived, DemandCode: cityOpenWorldFreightBatchDemandCode(consignmentOne),
				RouteCode: &routeOne, SourceFact: firstRoot, LastFact: firstCompleted, Version: 4, Metadata: metadata},
			{Code: consignmentTwo, PlanCode: planCode, BatchNo: 2, RequestedUnits: 32,
				State: cityOpenWorldFreightBatchConsignmentStateReceived, DemandCode: cityOpenWorldFreightBatchDemandCode(consignmentTwo),
				RouteCode: &routeTwo, SourceFact: secondRoot, LastFact: secondCompleted, Version: 4, Metadata: metadata},
		},
		Lines: []CityOpenWorldFreightBatchLine{
			{ConsignmentCode: consignmentOne, SourceLineNo: 1, ResourceCode: "basic_material", QuantityUnits: 32, UnitPriceUnits: 5, TotalPriceUnits: 160, Metadata: metadata},
			{ConsignmentCode: consignmentTwo, SourceLineNo: 1, ResourceCode: "basic_material", QuantityUnits: 32, UnitPriceUnits: 5, TotalPriceUnits: 160, Metadata: metadata},
		},
		Facts: []CityOpenWorldFreightBatchFact{
			{ConsignmentCode: consignmentOne, Tick: firstRoot.Tick, Sequence: firstRoot.Sequence, FactType: "consignment.created", EvidenceKind: firstRoot.EvidenceKind, Payload: metadata},
			{ConsignmentCode: consignmentOne, Tick: firstPending.Tick, Sequence: firstPending.Sequence, FactType: "demand.requested", EvidenceKind: firstPending.EvidenceKind, Payload: metadata},
			{ConsignmentCode: consignmentOne, Tick: firstScheduled.Tick, Sequence: firstScheduled.Sequence, FactType: "route.scheduled", EvidenceKind: firstScheduled.EvidenceKind, Payload: metadata},
			{ConsignmentCode: consignmentOne, Tick: firstCompleted.Tick, Sequence: firstCompleted.Sequence, FactType: "route.completed", EvidenceKind: firstCompleted.EvidenceKind, Payload: metadata},
			{ConsignmentCode: consignmentOne, Tick: receiptOne.Tick, Sequence: receiptOne.Sequence, FactType: "receipt.confirmed", EvidenceKind: receiptOne.EvidenceKind, Payload: metadata},
			{ConsignmentCode: consignmentTwo, Tick: secondRoot.Tick, Sequence: secondRoot.Sequence, FactType: "consignment.created", EvidenceKind: secondRoot.EvidenceKind, Payload: metadata},
			{ConsignmentCode: consignmentTwo, Tick: secondPending.Tick, Sequence: secondPending.Sequence, FactType: "demand.requested", EvidenceKind: secondPending.EvidenceKind, Payload: metadata},
			{ConsignmentCode: consignmentTwo, Tick: secondScheduled.Tick, Sequence: secondScheduled.Sequence, FactType: "route.scheduled", EvidenceKind: secondScheduled.EvidenceKind, Payload: metadata},
			{ConsignmentCode: consignmentTwo, Tick: secondCompleted.Tick, Sequence: secondCompleted.Sequence, FactType: "route.completed", EvidenceKind: secondCompleted.EvidenceKind, Payload: metadata},
			{ConsignmentCode: consignmentTwo, Tick: receiptTwo.Tick, Sequence: receiptTwo.Sequence, FactType: "receipt.confirmed", EvidenceKind: receiptTwo.EvidenceKind, Payload: metadata},
		},
		Transitions: []CityOpenWorldFreightBatchTransition{
			{ConsignmentCode: consignmentOne, TransitionTick: firstPending.Tick, TransitionSequence: firstPending.Sequence, State: cityOpenWorldFreightBatchConsignmentStateAwaitingRoute, ReasonCode: cityOpenWorldFreightBatchReasonDispatched, SourceFact: firstPending, Metadata: metadata},
			{ConsignmentCode: consignmentOne, TransitionTick: firstScheduled.Tick, TransitionSequence: firstScheduled.Sequence, State: cityOpenWorldFreightBatchConsignmentStateInTransit, ReasonCode: cityOpenWorldFreightBatchReasonScheduled, SourceFact: firstScheduled, Metadata: metadata},
			{ConsignmentCode: consignmentOne, TransitionTick: firstCompleted.Tick, TransitionSequence: firstCompleted.Sequence, State: cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt, ReasonCode: cityOpenWorldFreightBatchReasonCompleted, SourceFact: firstCompleted, Metadata: metadata},
			{ConsignmentCode: consignmentOne, TransitionTick: receiptOne.Tick, TransitionSequence: receiptOne.Sequence, State: cityOpenWorldFreightBatchConsignmentStateReceived, ReasonCode: cityOpenWorldFreightBatchReasonReceived, SourceFact: receiptOne, Metadata: metadata},
			{ConsignmentCode: consignmentTwo, TransitionTick: secondPending.Tick, TransitionSequence: secondPending.Sequence, State: cityOpenWorldFreightBatchConsignmentStateAwaitingRoute, ReasonCode: cityOpenWorldFreightBatchReasonDispatched, SourceFact: secondPending, Metadata: metadata},
			{ConsignmentCode: consignmentTwo, TransitionTick: secondScheduled.Tick, TransitionSequence: secondScheduled.Sequence, State: cityOpenWorldFreightBatchConsignmentStateInTransit, ReasonCode: cityOpenWorldFreightBatchReasonScheduled, SourceFact: secondScheduled, Metadata: metadata},
			{ConsignmentCode: consignmentTwo, TransitionTick: secondCompleted.Tick, TransitionSequence: secondCompleted.Sequence, State: cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt, ReasonCode: cityOpenWorldFreightBatchReasonCompleted, SourceFact: secondCompleted, Metadata: metadata},
			{ConsignmentCode: consignmentTwo, TransitionTick: receiptTwo.Tick, TransitionSequence: receiptTwo.Sequence, State: cityOpenWorldFreightBatchConsignmentStateReceived, ReasonCode: cityOpenWorldFreightBatchReasonReceived, SourceFact: receiptTwo, Metadata: metadata},
		},
		Receipts: []CityOpenWorldFreightBatchReceipt{
			{ConsignmentCode: consignmentOne, PlanCode: planCode, OrderCode: orderCode, ReceivedTick: 6, DeliveryFact: CityOpenWorldRuntimeFactRef{Tick: 6, Sequence: 1}, ResourceOperation: CityResourceOperationCursor{Tick: 6, Sequence: 1}, SourceFact: receiptOne, Metadata: metadata},
			{ConsignmentCode: consignmentTwo, PlanCode: planCode, OrderCode: orderCode, ReceivedTick: 6, DeliveryFact: CityOpenWorldRuntimeFactRef{Tick: 6, Sequence: 1}, ResourceOperation: CityResourceOperationCursor{Tick: 6, Sequence: 1}, SourceFact: receiptTwo, Metadata: metadata},
		},
	}
}

func TestCityOpenWorldFreightBatchStateRequiresAtomicReceiptBoundary(t *testing.T) {
	state := newValidCityOpenWorldFreightBatchState(t)
	require.NoError(t, validateCityOpenWorldFreightBatchState(state))

	brokenPlanLastFact := newValidCityOpenWorldFreightBatchState(t)
	brokenPlanLastFact.Plans[0].LastFact = CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime, Tick: 99, Sequence: 1}
	require.Error(t, validateCityOpenWorldFreightBatchState(brokenPlanLastFact))

	brokenReceipt := newValidCityOpenWorldFreightBatchState(t)
	brokenReceipt.Receipts[1].ResourceOperation.Sequence++
	require.Error(t, validateCityOpenWorldFreightBatchState(brokenReceipt))

	brokenRuntimeBoundary := newValidCityOpenWorldFreightBatchState(t)
	brokenRuntimeBoundary.Consignments[0].LastFact = brokenRuntimeBoundary.Facts[2].freightBatchFactRef()
	require.Error(t, validateCityOpenWorldFreightBatchState(brokenRuntimeBoundary))
}

func TestCityOpenWorldFreightBatchSettledStateRetainsTransportRuntimeBoundary(t *testing.T) {
	state := newValidCityOpenWorldFreightBatchState(t)
	metadata := json.RawMessage(`{"schema_version":1}`)

	// V22 settles directly from V18's transport-terminal states.  It must not
	// rewrite last_runtime_fact_id to a supply-chain fact, because that column
	// remains an FK to city_open_world_runtime_facts.
	state.Policy.ReceivedCount = 0
	state.Policy.SettledCount = 2
	state.Policy.ReceiptCount = 0
	state.Plans[0].State = cityOpenWorldFreightBatchPlanStateSettled
	state.Receipts = nil
	state.Facts = append([]CityOpenWorldFreightBatchFact(nil), state.Facts[:0]...)
	state.Transitions = append([]CityOpenWorldFreightBatchTransition(nil), state.Transitions[:0]...)
	for _, fact := range newValidCityOpenWorldFreightBatchState(t).Facts {
		if fact.FactType != "receipt.confirmed" {
			state.Facts = append(state.Facts, fact)
		}
	}
	for _, transition := range newValidCityOpenWorldFreightBatchState(t).Transitions {
		if transition.State != cityOpenWorldFreightBatchConsignmentStateReceived {
			state.Transitions = append(state.Transitions, transition)
		}
	}
	for index := range state.Consignments {
		consignment := &state.Consignments[index]
		settlement := CityOpenWorldFreightBatchFactRef{
			EvidenceKind: cityOpenWorldFreightBatchEvidenceSupplyChain,
			Tick:         6,
			Sequence:     1,
		}
		state.Facts = append(state.Facts, CityOpenWorldFreightBatchFact{
			ConsignmentCode: consignment.Code, Tick: settlement.Tick, Sequence: settlement.Sequence,
			FactType: "settlement.confirmed", EvidenceKind: settlement.EvidenceKind, Payload: metadata,
		})
		state.Transitions = append(state.Transitions, CityOpenWorldFreightBatchTransition{
			ConsignmentCode: consignment.Code, TransitionTick: settlement.Tick, TransitionSequence: settlement.Sequence,
			State: cityOpenWorldFreightBatchConsignmentStateSettled, ReasonCode: cityOpenWorldFreightBatchReasonSettled,
			SourceFact: settlement, Metadata: metadata,
		})
		consignment.State = cityOpenWorldFreightBatchConsignmentStateSettled
		// LastFact remains the route.completed runtime fact supplied by the
		// pre-settlement transport transition.
	}

	require.NoError(t, validateCityOpenWorldFreightBatchState(state))

	brokenLastRuntime := newValidCityOpenWorldFreightBatchState(t)
	brokenLastRuntime.Policy.ReceivedCount = 0
	brokenLastRuntime.Policy.SettledCount = 2
	brokenLastRuntime.Policy.ReceiptCount = 0
	brokenLastRuntime.Plans[0].State = cityOpenWorldFreightBatchPlanStateSettled
	brokenLastRuntime.Receipts = nil
	brokenLastRuntime.Facts = nil
	brokenLastRuntime.Transitions = nil
	for _, fact := range newValidCityOpenWorldFreightBatchState(t).Facts {
		if fact.FactType != "receipt.confirmed" {
			brokenLastRuntime.Facts = append(brokenLastRuntime.Facts, fact)
		}
	}
	for _, transition := range newValidCityOpenWorldFreightBatchState(t).Transitions {
		if transition.State != cityOpenWorldFreightBatchConsignmentStateReceived {
			brokenLastRuntime.Transitions = append(brokenLastRuntime.Transitions, transition)
		}
	}
	for index := range brokenLastRuntime.Consignments {
		consignment := &brokenLastRuntime.Consignments[index]
		settlement := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceSupplyChain, Tick: 6, Sequence: 1}
		brokenLastRuntime.Facts = append(brokenLastRuntime.Facts, CityOpenWorldFreightBatchFact{
			ConsignmentCode: consignment.Code, Tick: settlement.Tick, Sequence: settlement.Sequence,
			FactType: "settlement.confirmed", EvidenceKind: settlement.EvidenceKind, Payload: metadata,
		})
		brokenLastRuntime.Transitions = append(brokenLastRuntime.Transitions, CityOpenWorldFreightBatchTransition{
			ConsignmentCode: consignment.Code, TransitionTick: settlement.Tick, TransitionSequence: settlement.Sequence,
			State: cityOpenWorldFreightBatchConsignmentStateSettled, ReasonCode: cityOpenWorldFreightBatchReasonSettled,
			SourceFact: settlement, Metadata: metadata,
		})
		consignment.State = cityOpenWorldFreightBatchConsignmentStateSettled
		if index == 0 {
			consignment.LastFact = settlement
		}
	}
	require.Error(t, validateCityOpenWorldFreightBatchState(brokenLastRuntime))
}

func TestCityOpenWorldFreightBatchPackLinesIsStableAndCapacityBounded(t *testing.T) {
	packed, err := cityOpenWorldFreightBatchPackLines([]cityOpenWorldFreightBatchSourceLine{
		{LineNo: 2, ResourceCode: "basic_material", QuantityUnits: 37, UnitPriceUnits: 3, TotalPriceUnits: 111},
		{LineNo: 1, ResourceCode: "food_supply", QuantityUnits: 18, UnitPriceUnits: 2, TotalPriceUnits: 36},
		{LineNo: 3, ResourceCode: "medical_supply", QuantityUnits: 9, UnitPriceUnits: 7, TotalPriceUnits: 63},
	})
	require.NoError(t, err)
	require.Len(t, packed, 2)
	require.Equal(t, int64(32), packed[0].RequestedUnits)
	require.Equal(t, int64(32), packed[1].RequestedUnits)
	require.Equal(t, 1, packed[0].BatchNo)
	require.Equal(t, 2, packed[1].BatchNo)
	require.Equal(t, 1, packed[0].Lines[0].SourceLineNo, "packing must sort source lines deterministically")

	_, err = cityOpenWorldFreightBatchPackLines([]cityOpenWorldFreightBatchSourceLine{
		{LineNo: 1, ResourceCode: "basic_material", QuantityUnits: 33, UnitPriceUnits: 1, TotalPriceUnits: 33},
		{LineNo: 1, ResourceCode: "food_supply", QuantityUnits: 1, UnitPriceUnits: 1, TotalPriceUnits: 1},
	})
	require.Error(t, err)
}

func TestCityOpenWorldFreightBatchStateTransitionDeltaMovesCounters(t *testing.T) {
	delta := cityOpenWorldFreightBatchStateTransitionDelta(
		cityOpenWorldFreightBatchConsignmentStateAwaitingRoute,
		cityOpenWorldFreightBatchConsignmentStateInTransit,
		1, 1, 0,
	)
	require.Equal(t, int64(-1), delta.awaitingRoute)
	require.Equal(t, int64(1), delta.inTransit)
	require.Equal(t, int64(1), delta.facts)
	require.Equal(t, int64(1), delta.transitions)

	delivered := cityOpenWorldFreightBatchStateTransitionDelta(
		cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt,
		cityOpenWorldFreightBatchConsignmentStateReceived,
		1, 1, 1,
	)
	require.Equal(t, int64(-1), delivered.awaitingReceipt)
	require.Equal(t, int64(1), delivered.received)
	require.Equal(t, int64(1), delivered.receipts)
}

func (fact CityOpenWorldFreightBatchFact) freightBatchFactRef() CityOpenWorldFreightBatchFactRef {
	return CityOpenWorldFreightBatchFactRef{EvidenceKind: fact.EvidenceKind, Tick: fact.Tick, Sequence: fact.Sequence}
}
