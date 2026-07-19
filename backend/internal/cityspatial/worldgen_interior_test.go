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
