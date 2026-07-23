package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterTrafficReservationSealsGrantDenialAndConsumption(t *testing.T) {
	actor := "character.player.0123456789abcdef0123456789abcdef"
	navigationRunCode := "navigation.run." + strings.Repeat("a", 64)
	from := cityRealtimeActorSpawnCandidate{X: 100, Y: 200, Z: 0}
	target := cityRealtimeActorSpawnCandidate{X: 101, Y: 200, Z: 0}
	due := int64(12 * cityRealtimeTimeQuantumUS)

	granted, grantedEvent, err := cityRealtimeCharacterTrafficReservationNew(
		actor, navigationRunCode, 2, from, target, due, 12,
		cityRealtimeCharacterTrafficReservationGranted, "",
	)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterTrafficReservationHeadValid(granted))
	require.True(t, cityRealtimeCharacterTrafficReservationEventValid(grantedEvent))
	require.Equal(t, cityRealtimeCharacterTrafficReservationEventGranted, grantedEvent.EventType)

	consumed, consumedEvent, err := cityRealtimeCharacterTrafficReservationAdvance(
		granted, 12, cityRealtimeCharacterTrafficReservationConsumed, "", strings.Repeat("b", 64),
	)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterTrafficReservationHeadValid(consumed))
	require.True(t, cityRealtimeCharacterTrafficReservationEventValid(consumedEvent))
	require.Equal(t, int64(2), consumed.ReservationRevision)
	require.Equal(t, cityRealtimeCharacterTrafficReservationConsumed, consumed.ReservationStatus)
	require.Equal(t, grantedEvent.EventHash, consumedEvent.PreviousEventHash)
	require.Equal(t, strings.Repeat("b", 64), consumedEvent.ActorPositionEventHash)

	denied, deniedEvent, err := cityRealtimeCharacterTrafficReservationNew(
		actor, navigationRunCode, 3, from, target, due+cityRealtimeTimeQuantumUS, 13,
		cityRealtimeCharacterTrafficReservationDenied, cityRealtimeCharacterTrafficReservationReasonCapacityUnavailable,
	)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterTrafficReservationHeadValid(denied))
	require.True(t, cityRealtimeCharacterTrafficReservationEventValid(deniedEvent))
	require.Equal(t, cityRealtimeCharacterTrafficReservationEventDenied, deniedEvent.EventType)
	require.Equal(t, cityRealtimeCharacterTrafficReservationReasonCapacityUnavailable, denied.ReasonCode)

	projection := cityRealtimeCharacterTrafficReservationProjection(consumed)
	require.Equal(t, consumed.ReservationCode, projection.ReservationCode)
	require.Equal(t, consumed.NavigationRunCode, projection.NavigationRunCode)
	require.NotContains(t, strings.Join([]string{
		projection.ReservationCode,
		projection.NavigationRunCode,
		projection.Status,
		projection.ReasonCode,
	}, "\n"), "100")
}

func TestCityRealtimeCharacterTrafficReservationPolicyAndBindingRemainClosed(t *testing.T) {
	policy := cityRealtimeCharacterTrafficCapacityPolicy{
		PolicyID:      cityRealtimeCharacterTrafficCapacityPolicyID,
		PolicyVersion: cityRealtimeCharacterTrafficCapacityPolicyVersion,
		Status:        "published",
		PolicyHash:    strings.Repeat("c", 64),
		Manifest: cityRealtimeCharacterTrafficCapacityPolicyManifest{
			SchemaVersion:        cityRealtimeCharacterTrafficReservationSchemaVersion,
			Allocation:           "stable_due_event_order",
			ReservationQuantumUS: cityRealtimeTimeQuantumUS,
			TerrainCapacities: map[string]int64{
				"terrain.grass":    1,
				"terrain.ground":   1,
				"terrain.road":     1,
				"terrain.sidewalk": 1,
				"terrain.soil":     1,
			},
		},
	}
	require.True(t, cityRealtimeCharacterTrafficCapacityPolicyValid(policy))

	binding := cityRealtimeCharacterTrafficReservationBinding{
		SchemaVersion:      cityRealtimeCharacterTrafficReservationSchemaVersion,
		AgentBindingHash:   strings.Repeat("d", 64),
		SpatialContextHash: strings.Repeat("e", 64),
		CapacityPolicyID:   policy.PolicyID,
		CapacityPolicyVer:  policy.PolicyVersion,
		CapacityPolicyHash: policy.PolicyHash,
	}
	binding.BindingHash = cityRealtimeCharacterTrafficReservationBindingHash(binding)
	require.True(t, cityRealtimeCharacterTrafficReservationBindingValid(binding))

	state := &cityRealtimeCharacterTrafficReservationHashState{
		SchemaVersion: cityRealtimeCharacterTrafficReservationSchemaVersion,
		Binding:       &binding,
		Heads:         []cityRealtimeCharacterTrafficReservationHead{},
	}
	require.NoError(t, validateCityRealtimeCharacterTrafficReservationHashState(state))

	policy.Manifest.TerrainCapacities["terrain.road"] = 2
	require.False(t, cityRealtimeCharacterTrafficCapacityPolicyValid(policy))
}
