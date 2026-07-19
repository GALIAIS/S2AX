package cityspatial

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateBuildingLayoutsIsDeterministicAndPortalAnchored(t *testing.T) {
	t.Parallel()
	building := buildingLayoutTestBuilding("building_central_q0", LandUseResidential)
	portals := buildingLayoutTestPortals(building)

	first, err := GenerateBuildingLayouts([]GeneratedBuilding{building}, portals)
	require.NoError(t, err)
	second, err := GenerateBuildingLayouts([]GeneratedBuilding{building}, portals)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Len(t, first, 1)
	require.Equal(t, DefaultBuildingLayoutVersion, first[0].LayoutVersion)
	require.Contains(t, first[0].Archetype, "residential.")

	cells := make(map[string]GeneratedBuildingLayoutCell, len(first[0].Cells))
	kinds := make(map[BuildingLayoutCellKind]int)
	for _, cell := range first[0].Cells {
		key := buildingLayoutCellKey(cell.X, cell.Y, cell.Z)
		_, duplicate := cells[key]
		require.False(t, duplicate, "duplicate layout cell %s", key)
		cells[key] = cell
		kinds[cell.Kind]++
		require.GreaterOrEqual(t, cell.X, int64(4))
		require.LessOrEqual(t, cell.X, int64(9))
		require.GreaterOrEqual(t, cell.Y, int64(4))
		require.LessOrEqual(t, cell.Y, int64(9))
		require.GreaterOrEqual(t, cell.Z, int32(0))
		require.LessOrEqual(t, cell.Z, int32(1))
	}
	require.Positive(t, kinds[BuildingLayoutCellWall])
	require.Positive(t, kinds[BuildingLayoutCellFloor])
	require.Positive(t, kinds[BuildingLayoutCellFurniture])

	for _, portal := range portals {
		for _, endpoint := range []struct {
			x int64
			y int64
			z int32
		}{
			{x: portal.FromX, y: portal.FromY, z: portal.FromZ},
			{x: portal.ToX, y: portal.ToY, z: portal.ToZ},
		} {
			cell, exists := cells[buildingLayoutCellKey(endpoint.x, endpoint.y, endpoint.z)]
			if !exists {
				continue
			}
			require.True(t, BuildingLayoutCellPassable(cell.Kind), "portal %s must land on a passable cell", portal.Code)
		}
	}

	entrance := cells[buildingLayoutCellKey(4, 6, 0)]
	require.Equal(t, BuildingLayoutCellDoor, entrance.Kind)
	stairs := cells[buildingLayoutCellKey(6, 6, 1)]
	require.Equal(t, BuildingLayoutCellFloor, stairs.Kind)
	require.Equal(t, "stairs", stairs.Feature)
}

func TestGenerateBuildingLayoutsVariesArchetypesAndOutlinesWithoutRandomState(t *testing.T) {
	t.Parallel()
	buildings := make([]GeneratedBuilding, 0, 48)
	portals := make([]GeneratedBuildingPortal, 0, 96)
	for _, use := range []LandUse{LandUseResidential, LandUseCommercial, LandUseIndustrial} {
		for index := 0; index < 16; index++ {
			building := buildingLayoutTestBuilding(fmt.Sprintf("building_%s_%02d", use, index), use)
			buildings = append(buildings, building)
			portals = append(portals, buildingLayoutTestPortals(building)...)
		}
	}

	layouts, err := GenerateBuildingLayouts(buildings, portals)
	require.NoError(t, err)
	require.Len(t, layouts, len(buildings))

	archetypesByUse := map[LandUse]map[string]struct{}{
		LandUseResidential: {},
		LandUseCommercial:  {},
		LandUseIndustrial:  {},
	}
	irregularByUse := map[LandUse]bool{}
	buildingByCode := make(map[string]GeneratedBuilding, len(buildings))
	for _, building := range buildings {
		buildingByCode[building.Code] = building
	}
	for _, layout := range layouts {
		building := buildingByCode[layout.BuildingCode]
		archetypesByUse[building.PrimaryUse][layout.Archetype] = struct{}{}
		floorZeroCells := 0
		for _, cell := range layout.Cells {
			if cell.Z == building.BaseZ {
				floorZeroCells++
			}
		}
		if floorZeroCells < 36 {
			irregularByUse[building.PrimaryUse] = true
		}
	}
	for _, use := range []LandUse{LandUseResidential, LandUseCommercial, LandUseIndustrial} {
		require.GreaterOrEqual(t, len(archetypesByUse[use]), 3, "expected varied %s archetypes", use)
		require.True(t, irregularByUse[use], "expected at least one irregular %s outline", use)
	}
}

func TestGenerateBuildingLayoutsScaleFurnishingsWithLargeInteriorArea(t *testing.T) {
	t.Parallel()
	building := GeneratedBuilding{
		Code:       "building_large_workshop",
		PrimaryUse: LandUseIndustrial,
		Footprint: LandRectangle{
			ChunkX: 0, ChunkY: 0, Z: SurfaceZ,
			LocalMinX: 5, LocalMinY: 5, LocalMaxX: 24, LocalMaxY: 22,
		},
		BaseZ: SurfaceZ, TopZ: SurfaceZ, FloorCount: 1,
	}
	portals := []GeneratedBuildingPortal{{
		Code: "entrance", BuildingCode: building.Code, PortalType: "entrance",
		FromX: 4, FromY: 13, FromZ: SurfaceZ, ToX: 5, ToY: 13, ToZ: SurfaceZ,
		Status: "active",
	}}

	layout, err := GenerateBuildingLayout(building, portals)
	require.NoError(t, err)
	furniture := 0
	for _, cell := range layout.Cells {
		if cell.Kind == BuildingLayoutCellFurniture {
			furniture++
		}
	}
	require.GreaterOrEqual(t, furniture, 18)
}

func buildingLayoutTestBuilding(code string, use LandUse) GeneratedBuilding {
	return GeneratedBuilding{
		Code: code, PrimaryUse: use,
		Footprint: LandRectangle{
			ChunkX: 0, ChunkY: 0, Z: SurfaceZ,
			LocalMinX: 4, LocalMinY: 4, LocalMaxX: 9, LocalMaxY: 9,
		},
		BaseZ: 0, TopZ: 1, FloorCount: 2,
	}
}

func buildingLayoutTestPortals(building GeneratedBuilding) []GeneratedBuildingPortal {
	return []GeneratedBuildingPortal{
		{
			Code: "entrance", BuildingCode: building.Code, PortalType: "entrance",
			FromX: 3, FromY: 6, FromZ: 0, ToX: 4, ToY: 6, ToZ: 0,
			Status: "active",
		},
		{
			Code: "stair_000_001", BuildingCode: building.Code, PortalType: "stair",
			FromX: 6, FromY: 6, FromZ: 0, ToX: 6, ToY: 6, ToZ: 1,
			Status: "active",
		},
	}
}

func buildingLayoutCellKey(x, y int64, z int32) string {
	return fmt.Sprintf("%d/%d/%d", x, y, z)
}
