package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldInfrastructureState(t *testing.T) *CityOpenWorldInfrastructureState {
	t.Helper()
	network := newValidCityOpenWorldSpatialNetworkState(t)
	assets, err := buildCityOpenWorldInfrastructureAssets(network)
	require.NoError(t, err)
	policyHash, err := cityOpenWorldInfrastructurePolicyHash()
	require.NoError(t, err)
	policyMetadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldInfrastructureSchemaVersion,
		"scope":          "v19_assets_mutable_state_only",
		"scheduler":      "not_consumed_by_v9",
		"legacy":         "v19_topology_seeded_at_baseline",
	})
	require.NoError(t, err)
	baselineMetadata, err := cityOpenWorldInfrastructureBaselineMetadata()
	require.NoError(t, err)
	state := &CityOpenWorldInfrastructureState{
		Policy: CityOpenWorldInfrastructurePolicy{
			ProfileID:       cityOpenWorldInfrastructureProfileID,
			ProfileVersion:  cityOpenWorldInfrastructureProfileVersion,
			ContentHash:     policyHash,
			BaselineTick:    0,
			AssetContract:   cityOpenWorldInfrastructureAssetContract,
			StateContract:   cityOpenWorldInfrastructureStateContract,
			MaximumAssets:   cityOpenWorldInfrastructureMaximumAssets,
			AssetCount:      int64(len(assets)),
			TransitionCount: int64(len(assets)),
			Revision:        1,
			Metadata:        policyMetadata,
		},
		Assets:      assets,
		States:      make([]CityOpenWorldInfrastructureAssetState, 0, len(assets)),
		Transitions: make([]CityOpenWorldInfrastructureAssetTransition, 0, len(assets)),
	}
	for _, asset := range assets {
		if asset.AssetKind == cityOpenWorldInfrastructureAssetKindNode {
			state.Policy.NodeAssetCount++
		} else {
			state.Policy.SegmentAssetCount++
		}
		state.States = append(state.States, cityOpenWorldInfrastructureBaselineState(asset.Code, 0, baselineMetadata))
		state.Transitions = append(state.Transitions, cityOpenWorldInfrastructureBaselineTransition(asset.Code, 0, baselineMetadata))
	}
	sortCityOpenWorldInfrastructureState(state)
	return state
}

func TestCityOpenWorldInfrastructureAssetsAreDeterministicV19Seeds(t *testing.T) {
	network := newValidCityOpenWorldSpatialNetworkState(t)
	first, err := buildCityOpenWorldInfrastructureAssets(network)
	require.NoError(t, err)
	second, err := buildCityOpenWorldInfrastructureAssets(network)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Len(t, first, len(network.Nodes)+len(network.Corridors))

	for _, asset := range first {
		require.NotEmpty(t, asset.ContentHash)
		switch asset.AssetKind {
		case cityOpenWorldInfrastructureAssetKindNode:
			require.NotNil(t, asset.SpatialNodeCode)
			require.Nil(t, asset.SpatialCorridorCode)
			require.Equal(t, 0, asset.SegmentOrdinal)
		case cityOpenWorldInfrastructureAssetKindSegment:
			require.Nil(t, asset.SpatialNodeCode)
			require.NotNil(t, asset.SpatialCorridorCode)
			require.Equal(t, 1, asset.SegmentOrdinal)
		default:
			t.Fatalf("unexpected asset kind %q", asset.AssetKind)
		}
	}
}

func TestCityOpenWorldInfrastructureStateValidatesLifecycleHistory(t *testing.T) {
	state := newValidCityOpenWorldInfrastructureState(t)
	require.NoError(t, validateCityOpenWorldInfrastructureState(state))

	assetCode := state.Assets[0].Code
	metadata, err := cityOpenWorldInfrastructureCommandMetadata(cityOpenWorldInfrastructureStateOperational)
	require.NoError(t, err)
	fact := &CityOpenWorldRuntimeFactRef{Tick: 1, Sequence: 1}
	state.Transitions = append(state.Transitions, CityOpenWorldInfrastructureAssetTransition{
		AssetCode: assetCode, TransitionTick: 1, TransitionSeq: 1,
		FromState:     cityOpenWorldInfrastructureStateOperational,
		ToState:       cityOpenWorldInfrastructureStateRestricted,
		CapacityMilli: 650, ReasonCode: "operator.restricted",
		SourceFact: fact, Metadata: metadata,
	})
	for index := range state.States {
		if state.States[index].AssetCode == assetCode {
			state.States[index] = CityOpenWorldInfrastructureAssetState{
				AssetCode: assetCode, State: cityOpenWorldInfrastructureStateRestricted,
				CapacityMilli: 650, EffectiveTick: 1, SourceFact: fact,
				Version: 2, Metadata: metadata,
			}
		}
	}
	state.Policy.TransitionCount++
	state.Policy.Revision++
	sortCityOpenWorldInfrastructureState(state)
	require.NoError(t, validateCityOpenWorldInfrastructureState(state))

	badCapacity := *state
	badCapacity.States = append([]CityOpenWorldInfrastructureAssetState(nil), state.States...)
	badCapacity.States[0].CapacityMilli = 1000
	require.Error(t, validateCityOpenWorldInfrastructureState(&badCapacity))

	badTransition := *state
	badTransition.Transitions = append([]CityOpenWorldInfrastructureAssetTransition(nil), state.Transitions...)
	badTransition.Transitions[len(badTransition.Transitions)-1].ToState = cityOpenWorldInfrastructureStateConstruction
	require.Error(t, validateCityOpenWorldInfrastructureState(&badTransition))
}

func TestNormalizeCityOpenWorldInfrastructureTransitionCommand(t *testing.T) {
	payload, known, err := normalizeCityOpenWorldInfrastructureCommand(
		json.RawMessage(`{"asset_code":"INFRASTRUCTURE.ASSET.NODE.ABC","state":"restricted","capacity_milli":500}`),
	)
	require.NoError(t, err)
	require.True(t, known)
	value := payload.(cityOpenWorldInfrastructureAssetTransitionPayload)
	require.Equal(t, "infrastructure.asset.node.abc", value.AssetCode)
	require.Equal(t, int64(500), *value.CapacityMilli)
	require.Equal(t, "operator.restricted", value.ReasonCode)

	_, _, err = normalizeCityOpenWorldInfrastructureCommand(
		json.RawMessage(`{"asset_code":"infrastructure.asset.node.abc","state":"restricted"}`),
	)
	require.Error(t, err)
}

func TestCityOpenWorldInfrastructureReplaySeparatesStaticAndFactBackedState(t *testing.T) {
	baseline := newValidCityOpenWorldInfrastructureState(t)
	mutated := newValidCityOpenWorldInfrastructureState(t)
	asset := mutated.Assets[0]
	applyCityOpenWorldInfrastructureTestTransition(t, mutated, asset.Code, "restricted", 650, 1, 1)

	// V20 state may change across a tick, but its content identity and immutable
	// asset definitions must remain fixed.
	require.True(t, cityOpenWorldInfrastructureStaticCheckpointEqual(baseline, mutated))

	payload, err := json.Marshal(map[string]any{
		"asset_code": asset.Code, "asset_kind": asset.AssetKind,
		"from_state": "operational", "to_state": "restricted",
		"capacity_milli": 650, "reason_code": "operator.restricted",
		"v9_scheduler_effect": "none",
	})
	require.NoError(t, err)
	runtime := &cityOpenWorldRuntimeHashState{
		Infrastructure: mutated,
		Facts: []CityOpenWorldRuntimeFact{{
			Tick: 1, Sequence: 1, FactType: cityOpenWorldInfrastructureFactAssetTransition,
			Payload: payload,
		}},
	}
	require.NoError(t, validateCityOpenWorldInfrastructureCheckpoint(1, runtime))

	brokenPayload := *runtime
	brokenPayload.Facts = append([]CityOpenWorldRuntimeFact(nil), runtime.Facts...)
	brokenPayload.Facts[0].FactType = "tampered.infrastructure.fact"
	require.Error(t, validateCityOpenWorldInfrastructureCheckpoint(1, &brokenPayload))

	brokenAsset := newValidCityOpenWorldInfrastructureState(t)
	brokenAsset.Assets = append([]CityOpenWorldInfrastructureAsset(nil), brokenAsset.Assets...)
	brokenAsset.Assets[0].AssetClass = "node.tampered"
	require.False(t, cityOpenWorldInfrastructureStaticCheckpointEqual(baseline, brokenAsset))
}

func applyCityOpenWorldInfrastructureTestTransition(
	t *testing.T,
	state *CityOpenWorldInfrastructureState,
	assetCode, targetState string,
	capacityMilli, tick, sequence int64,
) {
	t.Helper()
	current := cityOpenWorldInfrastructureStateOperational
	for _, item := range state.States {
		if item.AssetCode == assetCode {
			current = item.State
			break
		}
	}
	metadata, err := cityOpenWorldInfrastructureCommandMetadata(current)
	require.NoError(t, err)
	fact := &CityOpenWorldRuntimeFactRef{Tick: tick, Sequence: sequence}
	state.Transitions = append(state.Transitions, CityOpenWorldInfrastructureAssetTransition{
		AssetCode: assetCode, TransitionTick: tick, TransitionSeq: sequence,
		FromState: current, ToState: targetState, CapacityMilli: capacityMilli,
		ReasonCode: "operator." + targetState, SourceFact: fact, Metadata: metadata,
	})
	for index := range state.States {
		if state.States[index].AssetCode == assetCode {
			state.States[index] = CityOpenWorldInfrastructureAssetState{
				AssetCode: assetCode, State: targetState, CapacityMilli: capacityMilli,
				EffectiveTick: tick, SourceFact: fact, Version: 2, Metadata: metadata,
			}
			break
		}
	}
	state.Policy.TransitionCount++
	state.Policy.Revision++
	sortCityOpenWorldInfrastructureState(state)
	require.NoError(t, validateCityOpenWorldInfrastructureState(state))
}
