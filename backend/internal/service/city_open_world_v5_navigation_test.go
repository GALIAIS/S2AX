package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV5NavigationCommandNormalizationAndVersionGate(t *testing.T) {
	v4, err := cityEngineForVersion(CitySimulationVersionOpenWorldV4)
	require.NoError(t, err)
	v5, err := cityEngineForVersion(CitySimulationVersionOpenWorldV5)
	require.NoError(t, err)
	require.False(t, v4.supportsCommand(CityCommandTypeOpenWorldActorNavigationSet))
	require.False(t, v4.supportsCommand(CityCommandTypeOpenWorldActorNavigationCancel))
	require.True(t, v5.supportsCommand(CityCommandTypeOpenWorldActorNavigationSet))
	require.True(t, v5.supportsCommand(CityCommandTypeOpenWorldActorNavigationCancel))

	normalized, handled, err := normalizeCityOpenWorldRuntimeCommand(
		CityCommandTypeOpenWorldActorNavigationSet,
		json.RawMessage(`{"actor_code":" Actor.17 ","space_kind":"surface","floor_index":0,"x":17,"y":-9,"z":0}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	surface, ok := normalized.(cityOpenWorldActorNavigationSetPayload)
	require.True(t, ok)
	require.Equal(t, "actor.17", surface.ActorCode)
	require.Equal(t, cityOpenWorldV5NavigationDefaultMaximumSteps, surface.MaximumSteps)
	require.True(t, cityOpenWorldV5NavigationLocationPayloadValid(surface))

	_, handled, err = normalizeCityOpenWorldRuntimeCommand(
		CityCommandTypeOpenWorldActorNavigationSet,
		json.RawMessage(`{"actor_code":"actor.17","space_kind":"surface","floor_index":1,"x":17,"y":-9,"z":0}`),
	)
	require.True(t, handled)
	require.Error(t, err)

	normalized, handled, err = normalizeCityOpenWorldRuntimeCommand(
		CityCommandTypeOpenWorldActorNavigationSet,
		json.RawMessage(`{"actor_code":"actor.17","space_kind":"interior","building_code":"Building.Central","floor_index":2,"x":17,"y":-9,"z":2,"maximum_steps":2048}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	interior, ok := normalized.(cityOpenWorldActorNavigationSetPayload)
	require.True(t, ok)
	require.Equal(t, "building.central", interior.BuildingCode)
	require.Equal(t, cityOpenWorldV5NavigationMaximumSteps, interior.MaximumSteps)

	_, handled, err = normalizeCityOpenWorldRuntimeCommand(
		CityCommandTypeOpenWorldActorNavigationSet,
		json.RawMessage(`{"actor_code":"actor.17","space_kind":"interior","building_code":"building.central","floor_index":2,"x":17,"y":-9,"z":0}`),
	)
	require.True(t, handled)
	require.Error(t, err)
}

func TestCityOpenWorldV5NavigationHelpersStayDeterministicAndBounded(t *testing.T) {
	current, err := cityOpenWorldRuntimeLocationFromPayload(cityOpenWorldActorMovePayload{
		ActorCode: "actor.17", SpaceKind: "surface", X: 10, Y: 10, Z: 0,
	})
	require.NoError(t, err)
	goal, err := cityOpenWorldRuntimeLocationFromPayload(cityOpenWorldActorMovePayload{
		ActorCode: "actor.17", SpaceKind: "surface", X: 13, Y: 12, Z: 0,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), cityOpenWorldV5NavigationDistance(current, goal))

	neighbors := cityOpenWorldV5NavigationNeighbors(current, goal)
	require.Len(t, neighbors, 8)
	require.Equal(t, int64(11), neighbors[0].X)
	require.Equal(t, int64(10), neighbors[0].Y)
	seen := make(map[string]struct{}, len(neighbors))
	for _, neighbor := range neighbors {
		key := cityOpenWorldV5NavigationLocationKey(neighbor)
		_, duplicate := seen[key]
		require.False(t, duplicate, key)
		seen[key] = struct{}{}
		require.LessOrEqual(t, cityOpenWorldV5NavigationDistance(neighbor, goal), int64(4))
	}

	require.Equal(t, int64(1), cityOpenWorldV5NavigationRetryDelay(1))
	require.Equal(t, int64(2), cityOpenWorldV5NavigationRetryDelay(4))
	require.Equal(t, int64(cityOpenWorldV5NavigationMaximumRetryDelay), cityOpenWorldV5NavigationRetryDelay(99))
}
