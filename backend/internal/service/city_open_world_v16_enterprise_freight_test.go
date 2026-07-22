package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldEnterpriseFreightPolicy(t *testing.T) CityOpenWorldEnterpriseFreightPolicy {
	t.Helper()
	hash, err := cityOpenWorldEnterpriseFreightPolicyHash()
	require.NoError(t, err)
	metadata, err := json.Marshal(map[string]any{
		"schema_version":          cityOpenWorldEnterpriseFreightSchemaVersion,
		"scope":                   "dispatch_to_v9_freight_demand_only",
		"receipt":                 "not_implemented",
		"maximum_requested_units": cityOpenWorldEnterpriseFreightMaximumRequestedUnits,
		"mode_code":               cityOpenWorldEnterpriseFreightModeCode,
		"purpose_code":            cityOpenWorldEnterpriseFreightPurposeCode,
	})
	require.NoError(t, err)
	return CityOpenWorldEnterpriseFreightPolicy{
		ProfileID:                 cityOpenWorldEnterpriseFreightProfileID,
		ProfileVersion:            cityOpenWorldEnterpriseFreightProfileVersion,
		ContentHash:               hash,
		BaselineTick:              0,
		SourceContract:            cityOpenWorldEnterpriseFreightSourceContract,
		DemandContract:            cityOpenWorldEnterpriseFreightDemandContract,
		CompletionContract:        cityOpenWorldEnterpriseFreightCompletionContract,
		TerminalContract:          cityOpenWorldEnterpriseFreightTerminalContract,
		CarrierActorCode:          cityOpenWorldEnterpriseFreightCarrierActorCode,
		MaximumSources:            cityOpenWorldEnterpriseFreightMaximumSources,
		MaximumGenerationsPerTick: cityOpenWorldEnterpriseFreightMaximumGenerationsTick,
		Revision:                  1,
		Metadata:                  metadata,
	}
}

func newValidCityOpenWorldEnterpriseFreightState(t *testing.T) *CityOpenWorldEnterpriseFreightState {
	t.Helper()
	orderCode := "supply.order.enterprise_freight_test"
	sourceCode := cityOpenWorldEnterpriseFreightSourceCode(orderCode)
	demandCode := cityOpenWorldEnterpriseFreightDemandCode(sourceCode)
	rootFact := CityOpenWorldRuntimeFactRef{Tick: 2, Sequence: 1}
	demandFact := CityOpenWorldRuntimeFactRef{Tick: 2, Sequence: 2}
	policy := newValidCityOpenWorldEnterpriseFreightPolicy(t)
	policy.SourceCount = 1
	policy.PendingCount = 1
	policy.DemandCount = 1
	policy.FactCount = 2
	policy.TransitionCount = 1
	return &CityOpenWorldEnterpriseFreightState{
		Policy: policy,
		Sources: []CityOpenWorldEnterpriseFreightSource{{
			Code: sourceCode, OrderCode: orderCode,
			SellerNodeCode: "supply.node.municipal_services",
			BuyerNodeCode:  "supply.node.openworld_trade_buyer",
			SourceHubCode:  "hub.facility.source", DestinationHubCode: "hub.facility.destination",
			CarrierActorCode: cityOpenWorldEnterpriseFreightCarrierActorCode,
			DispatchFact:     CityOpenWorldRuntimeFactRef{Tick: 1, Sequence: 1},
			DispatchTick:     1, SourceTick: 2, MobilityDeadlineTick: 34,
			RequestedUnits: 12, State: cityOpenWorldEnterpriseFreightStateDemandPending,
			DemandCode: &demandCode, SourceFact: rootFact, LastFact: demandFact,
			Version: 1, Metadata: json.RawMessage(`{"schema_version":1}`),
		}},
		Lines: []CityOpenWorldEnterpriseFreightSourceLine{{
			SourceCode: sourceCode, LineNo: 1, ResourceCode: "basic_material",
			SourceFirmCode: "municipal_services", SourceDistrictCode: "central",
			DestinationFirmCode: "openworld_trade_buyer", DestinationDistrictCode: "central",
			QuantityUnits: 12, UnitPriceUnits: 5, TotalPriceUnits: 60,
			Metadata: json.RawMessage(`{"schema_version":1}`),
		}},
		Facts: []CityOpenWorldEnterpriseFreightFact{
			{Tick: rootFact.Tick, Sequence: rootFact.Sequence, SourceCode: sourceCode,
				FactType: "source.created", RuntimeFact: rootFact, Payload: json.RawMessage(`{"schema_version":1}`)},
			{Tick: demandFact.Tick, Sequence: demandFact.Sequence, SourceCode: sourceCode,
				FactType: "demand.requested", RuntimeFact: demandFact, Payload: json.RawMessage(`{"schema_version":1}`)},
		},
		Transitions: []CityOpenWorldEnterpriseFreightTransition{{
			SourceCode: sourceCode, TransitionTick: demandFact.Tick, TransitionSequence: demandFact.Sequence,
			State:      cityOpenWorldEnterpriseFreightStateDemandPending,
			ReasonCode: cityOpenWorldEnterpriseFreightReasonDispatched,
			SourceFact: demandFact, Metadata: json.RawMessage(`{"schema_version":1,"previous_state":""}`),
		}},
	}
}

func TestCityOpenWorldEnterpriseFreightStatePinsAtomicDemandEvidence(t *testing.T) {
	state := newValidCityOpenWorldEnterpriseFreightState(t)
	require.NoError(t, validateCityOpenWorldEnterpriseFreightState(state))

	brokenLastFact := newValidCityOpenWorldEnterpriseFreightState(t)
	brokenLastFact.Sources[0].LastFact = brokenLastFact.Sources[0].SourceFact
	require.Error(t, validateCityOpenWorldEnterpriseFreightState(brokenLastFact))

	brokenLineTotal := newValidCityOpenWorldEnterpriseFreightState(t)
	brokenLineTotal.Lines[0].QuantityUnits--
	brokenLineTotal.Lines[0].TotalPriceUnits -= 5
	require.Error(t, validateCityOpenWorldEnterpriseFreightState(brokenLineTotal))

	brokenReason := newValidCityOpenWorldEnterpriseFreightState(t)
	brokenReason.Transitions[0].ReasonCode = cityOpenWorldEnterpriseFreightReasonExpired
	require.Error(t, validateCityOpenWorldEnterpriseFreightState(brokenReason))
}

func TestCityOpenWorldEnterpriseFreightStateSuppressesLoadsAboveFrozenEdgeCapacity(t *testing.T) {
	state := newValidCityOpenWorldEnterpriseFreightState(t)
	state.Sources[0].RequestedUnits = cityOpenWorldEnterpriseFreightMaximumRequestedUnits + 1
	state.Sources[0].State = cityOpenWorldEnterpriseFreightStateSuppressed
	state.Sources[0].DemandCode = nil
	state.Facts[1].FactType = "source.suppressed"
	state.Transitions[0].State = cityOpenWorldEnterpriseFreightStateSuppressed
	state.Transitions[0].ReasonCode = cityOpenWorldEnterpriseFreightReasonUnitsExceeded
	state.Lines[0].QuantityUnits = cityOpenWorldEnterpriseFreightMaximumRequestedUnits + 1
	state.Lines[0].TotalPriceUnits = state.Lines[0].QuantityUnits * state.Lines[0].UnitPriceUnits
	state.Policy.PendingCount = 0
	state.Policy.DemandCount = 0
	state.Policy.SuppressedCount = 1
	require.NoError(t, validateCityOpenWorldEnterpriseFreightState(state))

	state.Policy.Metadata = json.RawMessage(`{"schema_version":1,"scope":"dispatch_to_v9_freight_demand_only","receipt":"not_implemented","maximum_requested_units":1000,"mode_code":"freight","purpose_code":"enterprise.freight"}`)
	require.Error(t, validateCityOpenWorldEnterpriseFreightState(state))
}

func TestCityOpenWorldEnterpriseFreightStateAcceptsCompressedCompletedObservation(t *testing.T) {
	state := newValidCityOpenWorldEnterpriseFreightState(t)
	source := &state.Sources[0]
	completedFact := CityOpenWorldRuntimeFactRef{Tick: 5, Sequence: 1}
	routeCode := "mobility.route.enterprise_freight_test"
	source.State = cityOpenWorldEnterpriseFreightStateRouteCompleted
	source.RouteCode = &routeCode
	source.LastFact = completedFact
	source.Version = 2
	state.Policy.PendingCount = 0
	state.Policy.CompletedCount = 1
	state.Policy.FactCount++
	state.Policy.TransitionCount++
	state.Facts = append(state.Facts, CityOpenWorldEnterpriseFreightFact{
		Tick: completedFact.Tick, Sequence: completedFact.Sequence, SourceCode: source.Code,
		FactType: "route.completed", RuntimeFact: completedFact,
		Payload: json.RawMessage(`{"schema_version":1}`),
	})
	state.Transitions = append(state.Transitions, CityOpenWorldEnterpriseFreightTransition{
		SourceCode: source.Code, TransitionTick: completedFact.Tick, TransitionSequence: completedFact.Sequence,
		State:      cityOpenWorldEnterpriseFreightStateRouteCompleted,
		ReasonCode: cityOpenWorldEnterpriseFreightReasonCompleted,
		SourceFact: completedFact, Metadata: json.RawMessage(`{"schema_version":1,"previous_state":"demand_pending"}`),
	})

	require.True(t, cityOpenWorldEnterpriseFreightTransitionAllowed(
		cityOpenWorldEnterpriseFreightStateDemandPending,
		cityOpenWorldEnterpriseFreightStateRouteCompleted,
	))
	require.NoError(t, validateCityOpenWorldEnterpriseFreightState(state))
}

func TestCityOpenWorldEnterpriseFreightStaticCheckpointIgnoresOnlyDerivedEvidence(t *testing.T) {
	state := newValidCityOpenWorldEnterpriseFreightState(t)
	checkpoint := *state
	checkpoint.Policy.PendingCount = 0
	checkpoint.Policy.DemandCount = 0
	checkpoint.Policy.FactCount = 99
	checkpoint.Policy.TransitionCount = 99
	checkpoint.Policy.Revision = 99
	require.True(t, cityOpenWorldEnterpriseFreightStaticCheckpointEqual(state, &checkpoint))

	checkpoint.Policy.Metadata = json.RawMessage(`{"schema_version":1}`)
	require.False(t, cityOpenWorldEnterpriseFreightStaticCheckpointEqual(state, &checkpoint))
}
