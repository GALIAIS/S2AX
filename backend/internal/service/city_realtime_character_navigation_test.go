package service

import (
	"math"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterPortalRecordPinsCanonicalTopology(t *testing.T) {
	canonical := cityspatial.GeneratedOpenWorldPortal{
		Code:           "building.harbor.entrance",
		BuildingCode:   "building.harbor",
		PortalType:     "entrance",
		FromFloorIndex: 0,
		ToFloorIndex:   0,
		From:           cityspatial.WorldgenPoint{X: 12, Y: 8, Z: cityspatial.SurfaceZ},
		To:             cityspatial.WorldgenPoint{X: 13, Y: 8, Z: cityspatial.SurfaceZ},
		Bidirectional:  true,
	}
	hash, err := cityspatial.ComputeOpenWorldPortalHash(canonical)
	require.NoError(t, err)

	portal := cityRealtimeCharacterPortalRecord{
		Code: canonical.Code, BuildingCode: canonical.BuildingCode, PortalType: canonical.PortalType,
		FromFloorIndex: canonical.FromFloorIndex, ToFloorIndex: canonical.ToFloorIndex,
		From:          cityRealtimeActorSpawnCandidate{X: canonical.From.X, Y: canonical.From.Y, Z: canonical.From.Z},
		To:            cityRealtimeActorSpawnCandidate{X: canonical.To.X, Y: canonical.To.Y, Z: canonical.To.Z},
		Bidirectional: canonical.Bidirectional, TopologyHash: hash, Revision: 1,
	}
	require.NoError(t, portal.validate())

	portal.TopologyHash = strings.Repeat("0", 64)
	require.Error(t, portal.validate())
}

func TestCityRealtimeCharacterPortalEventHashCommitsPortalOnlyWhenPresent(t *testing.T) {
	from := cityRealtimeActorSpawnCandidate{X: 4, Y: 5, Z: cityspatial.SurfaceZ}
	base := cityRealtimeActorPositionEventInput{
		ActorCode:     "character.player.0123456789abcdef0123456789abcdef",
		EventSequence: 1,
		FrameSequence: 2,
		EventKind:     "move",
		From:          &from,
		To:            cityRealtimeActorSpawnCandidate{X: 5, Y: 5, Z: cityspatial.SurfaceZ},
		MotionState:   "walking",
	}
	withoutPortal, err := cityRealtimeActorPositionEventHash(base)
	require.NoError(t, err)
	withEmptyPortal, err := cityRealtimeActorPositionEventHash(cityRealtimeActorPositionEventInput{
		ActorCode: base.ActorCode, EventSequence: base.EventSequence, FrameSequence: base.FrameSequence,
		EventKind: base.EventKind, PortalCode: "", From: base.From, To: base.To, MotionState: base.MotionState,
	})
	require.NoError(t, err)
	require.Equal(t, withoutPortal, withEmptyPortal, "historic event hashes must remain byte-for-byte stable")

	withPortal, err := cityRealtimeActorPositionEventHash(cityRealtimeActorPositionEventInput{
		ActorCode: base.ActorCode, EventSequence: base.EventSequence, FrameSequence: base.FrameSequence,
		EventKind: "portal", PortalCode: "building.harbor.entrance", From: base.From, To: base.To, MotionState: "inside",
	})
	require.NoError(t, err)
	require.NotEqual(t, withoutPortal, withPortal, "portal topology must become part of the actor event chain")
}

func TestCityRealtimeCharacterInteriorWalkingKeepsVerticalTravelPortalBound(t *testing.T) {
	state := cityRealtimeActorState{X: 40, Y: 22, Z: 2}
	require.True(t, cityRealtimeCharacterAdjacentStep(state, cityRealtimeActorSpawnCandidate{X: 41, Y: 22, Z: 2}))
	require.False(t, cityRealtimeCharacterAdjacentStep(state, cityRealtimeActorSpawnCandidate{X: 40, Y: 22, Z: 3}))
	require.True(t, cityRealtimeCharacterInteriorCellTraversable(cityspatial.GeneratedWorldgenInteriorCell{Kind: cityspatial.BuildingLayoutCellFloor}))
	require.True(t, cityRealtimeCharacterInteriorCellTraversable(cityspatial.GeneratedWorldgenInteriorCell{Kind: cityspatial.BuildingLayoutCellDoor}))
	require.False(t, cityRealtimeCharacterInteriorCellTraversable(cityspatial.GeneratedWorldgenInteriorCell{Kind: cityspatial.BuildingLayoutCellFurniture}))
}

func TestCityRealtimeCharacterAdjacentStepRejectsIntegerOverflowCoordinates(t *testing.T) {
	minimum := cityRealtimeActorState{X: math.MinInt64, Y: 0, Z: cityspatial.SurfaceZ}
	require.True(t, cityRealtimeCharacterAdjacentStep(minimum, cityRealtimeActorSpawnCandidate{
		X: math.MinInt64 + 1, Y: 0, Z: cityspatial.SurfaceZ,
	}))
	require.False(t, cityRealtimeCharacterAdjacentStep(minimum, cityRealtimeActorSpawnCandidate{
		X: math.MaxInt64, Y: 0, Z: cityspatial.SurfaceZ,
	}))

	maximum := cityRealtimeActorState{X: math.MaxInt64, Y: 0, Z: cityspatial.SurfaceZ}
	require.True(t, cityRealtimeCharacterAdjacentStep(maximum, cityRealtimeActorSpawnCandidate{
		X: math.MaxInt64 - 1, Y: 0, Z: cityspatial.SurfaceZ,
	}))
	require.False(t, cityRealtimeCharacterAdjacentStep(maximum, cityRealtimeActorSpawnCandidate{
		X: math.MinInt64, Y: 0, Z: cityspatial.SurfaceZ,
	}))
}
