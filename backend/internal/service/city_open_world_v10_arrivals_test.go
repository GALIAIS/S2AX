package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldMobilityArrivalPolicy(t *testing.T) CityOpenWorldMobilityArrivalPolicy {
	t.Helper()
	hash, err := cityOpenWorldMobilityArrivalPolicyHash(
		cityOpenWorldMobilityArrivalBridgeContract,
		cityOpenWorldMobilityArrivalLandingContract,
		cityOpenWorldMobilityArrivalMaximumPerTick,
		cityOpenWorldMobilityArrivalLandingSearchRadius,
		cityOpenWorldMobilityArrivalMaximumBlocked,
	)
	require.NoError(t, err)
	return CityOpenWorldMobilityArrivalPolicy{
		ProfileID: cityOpenWorldMobilityArrivalProfileID, ProfileVersion: cityOpenWorldMobilityArrivalProfileVersion,
		ContentHash: hash, BaselineTick: 0, BridgeContract: cityOpenWorldMobilityArrivalBridgeContract,
		LandingContract:        cityOpenWorldMobilityArrivalLandingContract,
		MaximumArrivalsPerTick: cityOpenWorldMobilityArrivalMaximumPerTick,
		LandingSearchRadius:    cityOpenWorldMobilityArrivalLandingSearchRadius,
		MaximumBlockedAttempts: cityOpenWorldMobilityArrivalMaximumBlocked,
		Revision:               1, Metadata: json.RawMessage(`{}`),
	}
}

func TestCityOpenWorldMobilityArrivalStatePinsStaticBridgePolicy(t *testing.T) {
	state := &CityOpenWorldMobilityArrivalState{
		Policy:   newValidCityOpenWorldMobilityArrivalPolicy(t),
		Arrivals: []CityOpenWorldMobilityArrival{},
	}
	require.NoError(t, validateCityOpenWorldMobilityArrivalState(state))
	state.Policy.BridgeContract = "unsealed_bridge"
	require.Error(t, validateCityOpenWorldMobilityArrivalState(state))
}

func TestCityOpenWorldMobilityArrivalDemandOriginRequiresCompleteLocalCoordinate(t *testing.T) {
	location, err := cityOpenWorldRuntimeLocationFromPayload(cityOpenWorldActorMovePayload{
		ActorCode: "actor.test", SpaceKind: "surface", X: 128, Y: -64, Z: 0,
	})
	require.NoError(t, err)
	location.MovedTick = 7
	location.Version = 3
	location.Metadata = json.RawMessage(`{}`)
	raw, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"arrival_bridge": map[string]any{
			"contract":        "captured_request_location_v1",
			"expected_origin": location,
		},
	})
	require.NoError(t, err)
	origin, err := cityOpenWorldMobilityArrivalExpectedOrigin(raw, "actor.test")
	require.NoError(t, err)
	require.Equal(t, &location, origin)
	location.ActorCode = "actor.other"
	raw, err = json.Marshal(map[string]any{
		"arrival_bridge": map[string]any{
			"contract":        "captured_request_location_v1",
			"expected_origin": location,
		},
	})
	require.NoError(t, err)
	_, err = cityOpenWorldMobilityArrivalExpectedOrigin(raw, "actor.test")
	require.Error(t, err)
}
