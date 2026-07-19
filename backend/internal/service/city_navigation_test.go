package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func newCityNavigationTestContext(t *testing.T, chunkCount int) *cityNavigationContext {
	t.Helper()
	registry, err := cityspatial.DefaultRegistry()
	require.NoError(t, err)
	ruleSet, err := registry.Get(cityspatial.DefaultRuleSetID)
	require.NoError(t, err)

	navigation := &cityNavigationContext{
		ctx: context.Background(),
		profile: &CitySpatialProfile{
			ChunkSize: 4, MinimumZ: -1, MaximumZ: 1,
			MinimumChunkX: 0, MaximumChunkX: int64(chunkCount - 1),
			MinimumChunkY: 0, MaximumChunkY: 0,
		},
		ruleHash:            ruleSet.ContentHash,
		minimumMovementCost: 80,
		definitions:         make(map[string]cityNavigationDefinition, len(ruleSet.Definitions)),
		portalsByCell:       make(map[CityNavigationCoordinate][]cityNavigationPortal),
		occupiedByCell:      make(map[CityNavigationCoordinate][]string),
		jurisdictionByChunk: make(map[cityspatial.ChunkCoordinate]string),
		chunks:              make(map[cityspatial.ChunkCoordinate]*cityNavigationChunk),
		missingChunks:       make(map[cityspatial.ChunkCoordinate]struct{}),
		actorIDByCode:       make(map[string]int64),
		portalAccessCache:   make(map[string]bool),
	}
	for _, definition := range ruleSet.Definitions {
		passable := false
		for _, flag := range definition.Flags {
			if flag == "passable" {
				passable = true
				break
			}
		}
		navigation.definitions[definition.ID] = cityNavigationDefinition{
			movementCost: int64(definition.MovementCost),
			passable:     passable,
		}
	}
	for chunkX := 0; chunkX < chunkCount; chunkX++ {
		coordinate := cityspatial.ChunkCoordinate{X: int64(chunkX), Y: 0, Z: 0}
		navigation.jurisdictionByChunk[coordinate] = "district.core"
		navigation.chunks[coordinate] = &cityNavigationChunk{
			terrain:   repeatedCityNavigationTerrain("terrain.road", 16),
			furniture: make(map[int]string),
		}
	}
	return navigation
}

func repeatedCityNavigationTerrain(definitionID string, count int) []string {
	result := make([]string, count)
	for index := range result {
		result[index] = definitionID
	}
	return result
}

func cityNavigationCoordinates(steps []CityNavigationPathStep) []CityNavigationCoordinate {
	coordinates := make([]CityNavigationCoordinate, len(steps))
	for index := range steps {
		coordinates[index] = steps[index].Coordinate
	}
	return coordinates
}

func TestCityNavigationCellResolvesTerrainFurnitureAndOccupancy(t *testing.T) {
	navigation := newCityNavigationTestContext(t, 1)
	chunk := navigation.chunks[cityspatial.ChunkCoordinate{X: 0, Y: 0, Z: 0}]
	chunk.terrain[5] = "terrain.void"
	chunk.furniture[6] = "furniture.tree"
	navigation.occupiedByCell[CityNavigationCoordinate{X: 3, Y: 1, Z: 0}] = []string{"actor.blocker"}

	terrain, err := navigation.resolveCell(CityNavigationCoordinate{X: 1, Y: 1, Z: 0}, "actor.mover")
	require.NoError(t, err)
	require.False(t, terrain.passable)
	require.Equal(t, CityNavigationBlockTerrain, terrain.blockReason)

	furniture, err := navigation.resolveCell(CityNavigationCoordinate{X: 2, Y: 1, Z: 0}, "actor.mover")
	require.NoError(t, err)
	require.False(t, furniture.passable)
	require.Equal(t, CityNavigationBlockFurniture, furniture.blockReason)

	occupied, err := navigation.resolveCell(CityNavigationCoordinate{X: 3, Y: 1, Z: 0}, "actor.mover")
	require.NoError(t, err)
	require.False(t, occupied.passable)
	require.Equal(t, CityNavigationBlockOccupied, occupied.blockReason)
	require.Equal(t, []string{"actor.blocker"}, occupied.occupiedActors)

	navigation.occupiedByCell[CityNavigationCoordinate{X: 0, Y: 0, Z: 0}] = []string{"actor.mover"}
	self, err := navigation.resolveCell(CityNavigationCoordinate{X: 0, Y: 0, Z: 0}, "actor.mover")
	require.NoError(t, err)
	require.True(t, self.passable)
}

func TestCityNavigationRequiresBuildingPortalsAndSupportsVerticalTraversal(t *testing.T) {
	navigation := newCityNavigationTestContext(t, 1)
	building := cityNavigationBuilding{
		code: "building.hall", jurisdictionCode: "district.core",
		minimumX: 1, maximumX: 3, minimumY: 1, maximumY: 3,
		minimumZ: 0, maximumZ: 1,
	}
	entrance := cityNavigationPortal{
		code: "entrance.main", buildingCode: building.code, portalType: "entrance",
		from: CityNavigationCoordinate{X: 0, Y: 2, Z: 0},
		to:   CityNavigationCoordinate{X: 1, Y: 2, Z: 0}, bidirectional: true,
	}
	stairs := cityNavigationPortal{
		code: "stairs.main", buildingCode: building.code, portalType: "stair",
		from: CityNavigationCoordinate{X: 2, Y: 2, Z: 0},
		to:   CityNavigationCoordinate{X: 2, Y: 2, Z: 1}, bidirectional: true,
	}
	navigation.buildings = []cityNavigationBuilding{building}
	navigation.portals = []cityNavigationPortal{entrance, stairs}
	for _, portal := range navigation.portals {
		navigation.portalsByCell[portal.from] = append(navigation.portalsByCell[portal.from], portal)
		navigation.portalsByCell[portal.to] = append(navigation.portalsByCell[portal.to], portal)
	}

	entranceCell, entranceCost, reason, err := navigation.resolveStep(entrance.from, entrance.to, "actor.mover")
	require.NoError(t, err)
	require.Empty(t, reason)
	require.True(t, entranceCell.insideBuilding)
	require.Equal(t, building.code, entranceCell.anchorCode)
	require.Equal(t, int64(100), entranceCost)

	_, _, reason, err = navigation.resolveStep(
		CityNavigationCoordinate{X: 0, Y: 1, Z: 0},
		CityNavigationCoordinate{X: 1, Y: 1, Z: 0},
		"actor.mover",
	)
	require.NoError(t, err)
	require.Equal(t, CityNavigationBlockBuildingWall, reason)

	upperFloor, stairCost, reason, err := navigation.resolveStep(stairs.from, stairs.to, "actor.mover")
	require.NoError(t, err)
	require.Empty(t, reason)
	require.True(t, upperFloor.insideBuilding)
	require.Equal(t, int64(130), stairCost)

	_, _, reason, err = navigation.resolveStep(
		CityNavigationCoordinate{X: 2, Y: 1, Z: 0},
		CityNavigationCoordinate{X: 2, Y: 1, Z: 1},
		"actor.mover",
	)
	require.NoError(t, err)
	require.Equal(t, CityNavigationBlockBuildingWall, reason)
}

func TestCityNavigationResolvesSharedBuildingLayoutCells(t *testing.T) {
	navigation := newCityNavigationTestContext(t, 1)
	building := cityNavigationBuilding{
		code: "building.layout", jurisdictionCode: "district.core",
		minimumX: 1, maximumX: 3, minimumY: 1, maximumY: 3,
		minimumZ: 0, maximumZ: 0,
		layoutCells: map[CityNavigationCoordinate]cityspatial.BuildingLayoutCellKind{
			{X: 1, Y: 1, Z: 0}: cityspatial.BuildingLayoutCellFloor,
			{X: 2, Y: 1, Z: 0}: cityspatial.BuildingLayoutCellWall,
			{X: 1, Y: 2, Z: 0}: cityspatial.BuildingLayoutCellFurniture,
		},
	}
	navigation.buildings = []cityNavigationBuilding{building}

	floor, err := navigation.resolveCell(CityNavigationCoordinate{X: 1, Y: 1, Z: 0}, "actor.mover")
	require.NoError(t, err)
	require.True(t, floor.passable)
	require.True(t, floor.insideBuilding)

	wall, err := navigation.resolveCell(CityNavigationCoordinate{X: 2, Y: 1, Z: 0}, "actor.mover")
	require.NoError(t, err)
	require.False(t, wall.passable)
	require.Equal(t, CityNavigationBlockBuildingWall, wall.blockReason)

	furniture, err := navigation.resolveCell(CityNavigationCoordinate{X: 1, Y: 2, Z: 0}, "actor.mover")
	require.NoError(t, err)
	require.True(t, furniture.passable)

	portal := cityNavigationPortal{
		code: "entry", buildingCode: building.code, portalType: "entrance",
		from: CityNavigationCoordinate{X: 0, Y: 1, Z: 0},
		to:   CityNavigationCoordinate{X: 2, Y: 1, Z: 0}, bidirectional: true,
	}
	navigation.portalsByCell[portal.to] = []cityNavigationPortal{portal}
	portalWall, err := navigation.resolveCell(portal.to, "actor.mover")
	require.NoError(t, err)
	require.True(t, portalWall.passable)
}

func TestCityNavigationEnforcesDynamicPortalStateAndDeclarativeAccess(t *testing.T) {
	navigation := newCityNavigationTestContext(t, 1)
	navigation.dynamicPortalAccess = true
	navigation.worldID = 7
	navigation.worldTick = 12
	navigation.actorIDByCode["actor.mover"] = 41
	building := cityNavigationBuilding{
		code: "building.hall", jurisdictionCode: "district.core",
		minimumX: 1, maximumX: 3, minimumY: 1, maximumY: 3,
		minimumZ: 0, maximumZ: 0,
	}
	public, _, publicHash, err := canonicalWorldPortalAccessRequirement(
		publicWorldPortalAccessRequirement(),
	)
	require.NoError(t, err)
	portal := cityNavigationPortal{
		code: "entrance.main", buildingCode: building.code, portalType: "entrance",
		from: CityNavigationCoordinate{X: 0, Y: 2, Z: 0},
		to:   CityNavigationCoordinate{X: 1, Y: 2, Z: 0}, bidirectional: true,
		stateCode: WorldPortalStateClosed, accessRequirement: public,
		accessPolicyHash: publicHash,
	}
	navigation.buildings = []cityNavigationBuilding{building}
	setPortal := func(value cityNavigationPortal) {
		navigation.portals = []cityNavigationPortal{value}
		navigation.portalsByCell[value.from] = []cityNavigationPortal{value}
		navigation.portalsByCell[value.to] = []cityNavigationPortal{value}
	}
	setPortal(portal)

	_, _, reason, err := navigation.resolveStep(portal.from, portal.to, "actor.mover")
	require.NoError(t, err)
	require.Equal(t, CityNavigationBlockPortalClosed, reason)

	portal.stateCode = WorldPortalStateLocked
	setPortal(portal)
	_, _, reason, err = navigation.resolveStep(portal.from, portal.to, "actor.mover")
	require.NoError(t, err)
	require.Equal(t, CityNavigationBlockPortalLocked, reason)

	portal.stateCode = WorldPortalStateOpen
	setPortal(portal)
	_, _, reason, err = navigation.resolveStep(portal.from, portal.to, "actor.mover")
	require.NoError(t, err)
	require.Empty(t, reason)

	denied, _, deniedHash, err := canonicalWorldPortalAccessRequirement(WorldRequirementNode{
		Operator: WorldRequirementNot,
		Item:     &WorldRequirementNode{Operator: WorldRequirementAll, Items: []WorldRequirementNode{}},
	})
	require.NoError(t, err)
	portal.accessRequirement, portal.accessPolicyHash = denied, deniedHash
	setPortal(portal)
	_, _, reason, err = navigation.resolveStep(portal.from, portal.to, "actor.mover")
	require.NoError(t, err)
	require.Equal(t, CityNavigationBlockPortalAccess, reason)
}

func TestCityNavigationPreventsDiagonalCornerCutting(t *testing.T) {
	navigation := newCityNavigationTestContext(t, 1)
	chunk := navigation.chunks[cityspatial.ChunkCoordinate{X: 0, Y: 0, Z: 0}]
	chunk.furniture[1*4+2] = "furniture.tree"

	_, _, reason, err := navigation.resolveStep(
		CityNavigationCoordinate{X: 1, Y: 1, Z: 0},
		CityNavigationCoordinate{X: 2, Y: 2, Z: 0},
		"actor.mover",
	)
	require.NoError(t, err)
	require.Equal(t, CityNavigationBlockCorner, reason)
}

func TestCityNavigationFindPathIsDeterministicAndCosted(t *testing.T) {
	navigation := newCityNavigationTestContext(t, 1)
	chunk := navigation.chunks[cityspatial.ChunkCoordinate{X: 0, Y: 0, Z: 0}]
	chunk.furniture[1*4+1] = "furniture.tree"
	from := CityNavigationCoordinate{X: 0, Y: 1, Z: 0}
	to := CityNavigationCoordinate{X: 3, Y: 1, Z: 0}
	want := []CityNavigationCoordinate{
		{X: 0, Y: 1, Z: 0},
		{X: 0, Y: 0, Z: 0},
		{X: 1, Y: 0, Z: 0},
		{X: 2, Y: 0, Z: 0},
		{X: 3, Y: 1, Z: 0},
	}

	for iteration := 0; iteration < 8; iteration++ {
		path, err := navigation.findPath("actor.mover", from, to, 16)
		require.NoError(t, err)
		require.True(t, path.Reachable)
		require.Empty(t, path.Reason)
		require.Equal(t, want, cityNavigationCoordinates(path.Steps))
		require.Equal(t, int64(354), path.TotalCost)
		require.Equal(t, path.TotalCost, path.Steps[len(path.Steps)-1].TotalCost)
		require.Equal(t, "district.core", path.Steps[len(path.Steps)-1].JurisdictionCode)
	}
}

func TestCityNavigationReportsMissingChunksAndStepBounds(t *testing.T) {
	navigation := newCityNavigationTestContext(t, 2)
	missingCoordinate := cityspatial.ChunkCoordinate{X: 1, Y: 0, Z: 0}
	delete(navigation.chunks, missingCoordinate)
	navigation.missingChunks[missingCoordinate] = struct{}{}

	path, err := navigation.findPath(
		"actor.mover",
		CityNavigationCoordinate{X: 0, Y: 0, Z: 0},
		CityNavigationCoordinate{X: 4, Y: 0, Z: 0},
		16,
	)
	require.NoError(t, err)
	require.False(t, path.Reachable)
	require.Equal(t, CityNavigationBlockChunkUnavailable, path.Reason)

	path, err = navigation.findPath(
		"actor.mover",
		CityNavigationCoordinate{X: 0, Y: 0, Z: 0},
		CityNavigationCoordinate{X: 3, Y: 0, Z: 0},
		2,
	)
	require.NoError(t, err)
	require.False(t, path.Reachable)
	require.Equal(t, CityNavigationBlockSearchLimit, path.Reason)
}

func TestCityNavigationHonorsRequestCancellation(t *testing.T) {
	navigation := newCityNavigationTestContext(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	navigation.ctx = ctx

	_, err := navigation.findPath(
		"actor.mover",
		CityNavigationCoordinate{X: 0, Y: 0, Z: 0},
		CityNavigationCoordinate{X: 3, Y: 3, Z: 0},
		16,
	)
	require.ErrorIs(t, err, context.Canceled)
}
