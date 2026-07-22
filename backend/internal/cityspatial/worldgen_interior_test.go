package cityspatial

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateWorldgenBuildingInteriorKeepsIrregularEnvelopeAndScalesContents(t *testing.T) {
	plan := testWorldgenPlan(t, 8110042)
	building := worldgenInteriorTestBuilding()

	ground, err := GenerateWorldgenBuildingInterior(plan.Binding, building, 0)
	require.NoError(t, err)
	replayed, err := GenerateWorldgenBuildingInterior(plan.Binding, building, 0)
	require.NoError(t, err)
	upper, err := GenerateWorldgenBuildingInterior(plan.Binding, building, 1)
	require.NoError(t, err)

	require.Equal(t, ground, replayed)
	require.Equal(t, DefaultWorldgenInteriorVersion, ground.LayoutVersion)
	require.Len(t, ground.Cells, len(building.Footprint), "the interior must retain the envelope rather than re-box it")
	require.NotEmpty(t, ground.ContentHash)
	hash, err := ComputeWorldgenBuildingInteriorHash(&ground)
	require.NoError(t, err)
	require.Equal(t, ground.ContentHash, hash)

	groundKinds := make(map[BuildingLayoutCellKind]int)
	groundFeatures := make(map[string]int)
	for _, cell := range ground.Cells {
		groundKinds[cell.Kind]++
		groundFeatures[cell.Feature]++
		require.Equal(t, int32(0), cell.Z)
	}
	require.Positive(t, groundKinds[BuildingLayoutCellWall])
	require.Positive(t, groundKinds[BuildingLayoutCellDoor])
	require.GreaterOrEqual(t, groundKinds[BuildingLayoutCellFurniture], 18, "large interiors must not stop at one of each furniture type")
	require.Positive(t, groundFeatures["stairs"])

	entranceFound := false
	for _, cell := range ground.Cells {
		if cell.X == building.Entrance.X && cell.Y == building.Entrance.Y {
			entranceFound = true
			require.Equal(t, BuildingLayoutCellDoor, cell.Kind)
		}
	}
	require.True(t, entranceFound)
	for _, cell := range upper.Cells {
		require.Equal(t, int32(1), cell.Z)
	}
}

func TestGenerateWorldgenBuildingInteriorRejectsBrokenEnvelope(t *testing.T) {
	plan := testWorldgenPlan(t, 8110042)
	building := worldgenInteriorTestBuilding()
	building.Footprint = append(building.Footprint, building.Footprint[0])

	_, err := GenerateWorldgenBuildingInterior(plan.Binding, building, 0)
	require.ErrorIs(t, err, ErrInvalidWorldgenInput)
}

func TestGenerateWorldgenBuildingInteriorRepairsNarrowIrregularWing(t *testing.T) {
	plan := testWorldgenPlan(t, 8110042)
	building := GeneratedWorldgenBuilding{
		Code:          "building_v2_narrow_wing",
		CityCode:      "city_v2",
		LotCode:       "lot_v2_narrow_wing",
		PrimaryUse:    LandUseCommercial,
		ArchetypeCode: "commercial.shopfront",
		LayoutStyle:   "shopfront",
		FloorCount:    1,
		Entrance:      WorldgenPoint{X: 10, Y: 12, Z: SurfaceZ},
		Footprint: []WorldgenPoint{
			{X: 10, Y: 10, Z: SurfaceZ}, {X: 11, Y: 10, Z: SurfaceZ}, {X: 12, Y: 10, Z: SurfaceZ},
			{X: 13, Y: 10, Z: SurfaceZ}, {X: 14, Y: 10, Z: SurfaceZ}, {X: 18, Y: 10, Z: SurfaceZ},
			{X: 19, Y: 10, Z: SurfaceZ}, {X: 20, Y: 10, Z: SurfaceZ}, {X: 21, Y: 10, Z: SurfaceZ},
			{X: 22, Y: 10, Z: SurfaceZ}, {X: 10, Y: 11, Z: SurfaceZ}, {X: 11, Y: 11, Z: SurfaceZ},
			{X: 12, Y: 11, Z: SurfaceZ}, {X: 13, Y: 11, Z: SurfaceZ}, {X: 14, Y: 11, Z: SurfaceZ},
			{X: 18, Y: 11, Z: SurfaceZ}, {X: 19, Y: 11, Z: SurfaceZ}, {X: 20, Y: 11, Z: SurfaceZ},
			{X: 21, Y: 11, Z: SurfaceZ}, {X: 22, Y: 11, Z: SurfaceZ}, {X: 10, Y: 12, Z: SurfaceZ},
			{X: 11, Y: 12, Z: SurfaceZ}, {X: 12, Y: 12, Z: SurfaceZ}, {X: 13, Y: 12, Z: SurfaceZ},
			{X: 14, Y: 12, Z: SurfaceZ}, {X: 15, Y: 12, Z: SurfaceZ}, {X: 16, Y: 12, Z: SurfaceZ},
			{X: 17, Y: 12, Z: SurfaceZ}, {X: 18, Y: 12, Z: SurfaceZ}, {X: 19, Y: 12, Z: SurfaceZ},
			{X: 20, Y: 12, Z: SurfaceZ}, {X: 21, Y: 12, Z: SurfaceZ}, {X: 22, Y: 12, Z: SurfaceZ},
			{X: 10, Y: 13, Z: SurfaceZ}, {X: 11, Y: 13, Z: SurfaceZ}, {X: 12, Y: 13, Z: SurfaceZ},
			{X: 13, Y: 13, Z: SurfaceZ}, {X: 14, Y: 13, Z: SurfaceZ}, {X: 18, Y: 13, Z: SurfaceZ},
			{X: 19, Y: 13, Z: SurfaceZ}, {X: 20, Y: 13, Z: SurfaceZ}, {X: 21, Y: 13, Z: SurfaceZ},
			{X: 22, Y: 13, Z: SurfaceZ}, {X: 10, Y: 14, Z: SurfaceZ}, {X: 11, Y: 14, Z: SurfaceZ},
			{X: 12, Y: 14, Z: SurfaceZ}, {X: 13, Y: 14, Z: SurfaceZ}, {X: 14, Y: 14, Z: SurfaceZ},
			{X: 18, Y: 14, Z: SurfaceZ}, {X: 19, Y: 14, Z: SurfaceZ}, {X: 20, Y: 14, Z: SurfaceZ},
			{X: 21, Y: 14, Z: SurfaceZ}, {X: 22, Y: 14, Z: SurfaceZ},
		},
	}

	first, err := GenerateWorldgenBuildingInterior(plan.Binding, building, 0)
	require.NoError(t, err)
	second, err := GenerateWorldgenBuildingInterior(plan.Binding, building, 0)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Len(t, first.Cells, len(building.Footprint))

	passable := make(map[WorldgenPoint]struct{})
	for _, cell := range first.Cells {
		if BuildingLayoutCellPassable(cell.Kind) {
			passable[WorldgenPoint{X: cell.X, Y: cell.Y, Z: cell.Z}] = struct{}{}
		}
	}
	require.NotEmpty(t, passable)
	var root WorldgenPoint
	for point := range passable {
		root = point
		break
	}
	visited := map[WorldgenPoint]struct{}{root: {}}
	queue := []WorldgenPoint{root}
	for len(queue) > 0 {
		point := queue[0]
		queue = queue[1:]
		for _, offset := range []WorldgenPoint{{X: 0, Y: -1}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: -1, Y: 0}} {
			next := WorldgenPoint{X: point.X + offset.X, Y: point.Y + offset.Y, Z: point.Z}
			if _, exists := passable[next]; !exists {
				continue
			}
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	require.Len(t, visited, len(passable), "the narrow wing must remain traversable without rectangularizing its footprint")
}

func worldgenInteriorTestBuilding() GeneratedWorldgenBuilding {
	footprint := make([]WorldgenPoint, 0, 284)
	for y := int64(40); y < 56; y++ {
		for x := int64(80); x < 100; x++ {
			if x >= 94 && y >= 50 {
				continue
			}
			footprint = append(footprint, WorldgenPoint{X: x, Y: y, Z: SurfaceZ})
		}
	}
	return GeneratedWorldgenBuilding{
		Code:          "building_v2_irregular_workshop",
		CityCode:      "city_v2",
		LotCode:       "lot_v2",
		PrimaryUse:    LandUseIndustrial,
		ArchetypeCode: "industrial.loading_depot",
		LayoutStyle:   "loading_depot",
		FloorCount:    3,
		Entrance:      WorldgenPoint{X: 80, Y: 47, Z: SurfaceZ},
		Footprint:     footprint,
	}
}
