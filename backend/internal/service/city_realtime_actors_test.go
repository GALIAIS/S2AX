package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func TestCityRealtimeActorBootstrapSelectionIsDeterministicAndUnique(t *testing.T) {
	candidates := make([]cityRealtimeActorSpawnCandidate, 32)
	for index := range candidates {
		candidates[index] = cityRealtimeActorSpawnCandidate{X: int64(index), Y: int64(index / 8), Z: cityspatial.SurfaceZ}
	}
	seen := make(map[cityRealtimeActorSpawnCandidate]struct{})
	for index := 0; index < cityRealtimeActorBootstrapCount; index++ {
		candidate := cityRealtimeActorBootstrapCandidate(8_100_042, index, candidates)
		_, duplicate := seen[candidate]
		require.False(t, duplicate)
		seen[candidate] = struct{}{}
		require.Equal(t, candidate, cityRealtimeActorBootstrapCandidate(8_100_042, index, candidates))
	}
}

func TestCityRealtimeActorPublicLabelPolicySupportsNamesWithoutPublicIdentifiers(t *testing.T) {
	for _, label := range []string{"Resident 01", "春日 花子", "李-明", "O'Connor", "山田・太郎"} {
		require.Truef(t, cityRealtimeActorPublicLabelValid(label), "expected public label to be valid: %q", label)
	}
	for _, label := range []string{"", " Leading", "Trailing ", "player@example.com", "https://example.test", "name/path", "<script>", "a\nname"} {
		require.Falsef(t, cityRealtimeActorPublicLabelValid(label), "expected public label to be rejected: %q", label)
	}
}

func TestCityRealtimeActorHashStateUsesBoundedChainHead(t *testing.T) {
	identity := cityRealtimeActorIdentity{
		ActorCode: "npc.resident.01", ActorKind: "npc", PublicLabel: "Resident 01",
		AppearanceVariant: "resident.ochre", LifecycleStatus: "active",
		SpawnX: 12, SpawnY: 8, SpawnZ: cityspatial.SurfaceZ, SpawnFrameSequence: 0,
	}
	var err error
	identity.IdentityHash, err = cityRealtimeActorIdentityHash(identity)
	require.NoError(t, err)
	spawnHash, err := cityRealtimeActorPositionEventHash(cityRealtimeActorPositionEventInput{
		ActorCode: identity.ActorCode, EventSequence: 0, FrameSequence: 0,
		EventKind: "spawn", To: cityRealtimeActorSpawnCandidate{X: 12, Y: 8, Z: cityspatial.SurfaceZ}, MotionState: "idle",
	})
	require.NoError(t, err)
	state := cityRealtimeActorState{
		ActorCode: identity.ActorCode, X: 12, Y: 8, Z: cityspatial.SurfaceZ,
		MotionState: "idle", PositionRevision: 1, LastFrameSequence: 0, EventChainHash: spawnHash,
	}
	state.StateHash, err = cityRealtimeActorStateHash(state)
	require.NoError(t, err)

	hashState := &cityRealtimeActorHashState{
		SchemaVersion: cityRealtimeActorRuntimeSchemaVersion,
		Actors: []cityRealtimeActorHash{{
			ActorCode: identity.ActorCode, ActorKind: identity.ActorKind, PublicLabel: identity.PublicLabel,
			AppearanceVariant: identity.AppearanceVariant, LifecycleStatus: identity.LifecycleStatus,
			SpawnX: identity.SpawnX, SpawnY: identity.SpawnY, SpawnZ: identity.SpawnZ,
			SpawnFrameSequence: identity.SpawnFrameSequence, IdentityHash: identity.IdentityHash,
			X: state.X, Y: state.Y, Z: state.Z, MotionState: state.MotionState,
			PositionRevision: state.PositionRevision, LastFrameSequence: state.LastFrameSequence,
			StateHash: state.StateHash, EventChainHash: state.EventChainHash,
		}},
	}
	require.NoError(t, validateCityRealtimeActorHashState(hashState))

	hashState.Actors[0].EventChainHash = strings.Repeat("0", 64)
	require.Error(t, validateCityRealtimeActorHashState(hashState))
}

func TestCityRealtimeActorProjectionIsAccountBlindAndStable(t *testing.T) {
	snapshot := &CityRealtimeActorSnapshot{
		WorldID: 7, TimelineFrameSequence: 12, TimelineCursor: "twf_000000000012",
		StaticProjectionHash: strings.Repeat("a", 64),
		MinimumChunkX:        0, MaximumChunkX: 1, MinimumChunkY: 0, MaximumChunkY: 1, Z: cityspatial.SurfaceZ,
		Actors: []CityRealtimePublicActor{
			{ActorCode: "npc.resident.02", ActorKind: "npc", PublicLabel: "Resident 02", AppearanceVariant: "resident.teal", LifecycleStatus: "active", X: 3, Y: 2, Z: 0, MotionState: "walking", PositionRevision: 2, LastFrameSequence: 12},
			{ActorCode: "npc.resident.01", ActorKind: "npc", PublicLabel: "Resident 01", AppearanceVariant: "resident.ochre", LifecycleStatus: "active", X: 2, Y: 2, Z: 0, MotionState: "idle", PositionRevision: 1, LastFrameSequence: 0},
		},
	}
	first, err := cityRealtimeActorProjectionHash(snapshot)
	require.NoError(t, err)
	snapshot.Actors[0], snapshot.Actors[1] = snapshot.Actors[1], snapshot.Actors[0]
	second, err := cityRealtimeActorProjectionHash(snapshot)
	require.NoError(t, err)
	require.Equal(t, first, second)

	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	for _, privateField := range []string{"email", "username", "owner_user_id", "prompt", "model", "memory"} {
		require.NotContains(t, string(raw), privateField)
	}
}

func TestCityRealtimeActorSnapshotInputIsBoundedToRendererWindow(t *testing.T) {
	valid := CityRealtimeActorSnapshotInput{
		UserID: 1, WorldID: 2, MinimumChunkX: -3, MaximumChunkX: 4,
		MinimumChunkY: -3, MaximumChunkY: 4, Z: cityspatial.SurfaceZ, Limit: 128,
	}
	require.NoError(t, validateCityRealtimeActorSnapshotInput(valid))
	valid.MaximumChunkX = valid.MinimumChunkX + cityRealtimeActorMaximumChunkSpan
	require.ErrorIs(t, validateCityRealtimeActorSnapshotInput(valid), ErrCityInvalidInput)
	valid.MaximumChunkX = 4
	valid.Z = 1
	require.ErrorIs(t, validateCityRealtimeActorSnapshotInput(valid), ErrCityInvalidInput)
}
