package cityspatial

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func testWorldgenPlan(t *testing.T, seed int64) *GeneratedWorldgenPlan {
	t.Helper()
	profile, err := DefaultWorldgenProfile()
	require.NoError(t, err)
	binding, err := DefaultWorldgenBinding("city-f8-v3", seed, strings.Repeat("a", 64), profile)
	require.NoError(t, err)
	plan, err := GenerateWorldgenPlan(binding, profile, WorldgenBounds{
		MinimumChunkX: -4, MaximumChunkX: 4, MinimumChunkY: -4, MaximumChunkY: 4, Z: SurfaceZ,
	})
	require.NoError(t, err)
	return plan
}

func TestGenerateWorldgenPlanIsDeterministicAndProducesConnectedCityFabric(t *testing.T) {
	first := testWorldgenPlan(t, 8110042)
	second := testWorldgenPlan(t, 8110042)

	require.Equal(t, first, second)
	require.NotEmpty(t, first.BaselineHash)
	require.Len(t, first.Terrain, 81)
	require.NotEmpty(t, first.Cities)
	require.NotEmpty(t, first.Roads)
	require.NotEmpty(t, first.Lots)
	require.NotEmpty(t, first.Buildings)
	require.NoError(t, validateGeneratedWorldgenPlan(first))
	for _, road := range first.Roads {
		for _, point := range road.Points {
			require.Truef(t, worldgenPointInBounds(first.Bounds, point), "road %s leaves the generated bounds", road.Code)
		}
	}

	hash, err := ComputeWorldgenPlanHash(first)
	require.NoError(t, err)
	require.Equal(t, first.BaselineHash, hash)

	roadCells := worldgenRoadCells(first.Bounds, first.Roads)
	lots := make(map[string]GeneratedWorldgenLot, len(first.Lots))
	for _, lot := range first.Lots {
		lots[lot.Code] = lot
		for y := lot.Bounds.MinimumY; y <= lot.Bounds.MaximumY; y++ {
			for x := lot.Bounds.MinimumX; x <= lot.Bounds.MaximumX; x++ {
				_, road := roadCells[WorldgenPoint{X: x, Y: y, Z: lot.Bounds.Z}]
				require.Falsef(t, road, "lot %s overlaps a road", lot.Code)
			}
		}
	}
	for _, building := range first.Buildings {
		lot, found := lots[building.LotCode]
		require.True(t, found)
		entranceFound := false
		for _, point := range building.Footprint {
			require.True(t, worldgenPointInRectangle(point, lot.Bounds))
			if point == building.Entrance {
				entranceFound = true
			}
		}
		require.Truef(t, entranceFound, "building %s has no entrance in its footprint", building.Code)
	}
	require.Len(t, first.Buildings, len(first.Lots), "every generated lot must have exactly one building plan")
}

func TestOpenWorldV2RegionPlansAreStableAndNamespaceMaterializedBuildings(t *testing.T) {
	profile, err := WorldgenProfileByID(WorldgenProfileJapanMetropolitan)
	require.NoError(t, err)
	binding, err := DefaultOpenWorldgenBindingV2("city-openworld-v2", 8110042, profile)
	require.NoError(t, err)
	require.Equal(t, OpenWorldRegionGeneratorVersion, binding.GeneratorVersion)

	first, err := GenerateWorldgenPlan(binding, profile, WorldgenBounds{
		MinimumChunkX: 0, MaximumChunkX: 31, MinimumChunkY: 0, MaximumChunkY: 31, Z: SurfaceZ,
	})
	require.NoError(t, err)
	second, err := GenerateWorldgenPlan(binding, profile, first.Bounds)
	require.NoError(t, err)
	require.Equal(t, first, second)

	adjacent, err := GenerateWorldgenPlan(binding, profile, WorldgenBounds{
		MinimumChunkX: 32, MaximumChunkX: 63, MinimumChunkY: 0, MaximumChunkY: 31, Z: SurfaceZ,
	})
	require.NoError(t, err)
	require.NotEqual(t, first.BaselineHash, adjacent.BaselineHash)
	firstCodes := make(map[string]struct{}, len(first.Buildings))
	for _, building := range first.Buildings {
		firstCodes[building.Code] = struct{}{}
	}
	for _, building := range adjacent.Buildings {
		_, duplicate := firstCodes[building.Code]
		require.Falsef(t, duplicate, "region-local building code collided: %s", building.Code)
	}
}

func TestGenerateWorldgenPlanGrowsBentLocalStreetsAndIrregularFootprints(t *testing.T) {
	plan := testWorldgenPlan(t, 8110042)
	hasBentLocalStreet := false
	for _, road := range plan.Roads {
		if road.Class == WorldgenRoadLocal && len(road.Points) >= 3 {
			hasBentLocalStreet = true
			break
		}
	}
	require.True(t, hasBentLocalStreet)

	hasIrregularFootprint := false
	for _, building := range plan.Buildings {
		minimumX, maximumX := building.Footprint[0].X, building.Footprint[0].X
		minimumY, maximumY := building.Footprint[0].Y, building.Footprint[0].Y
		for _, point := range building.Footprint[1:] {
			if point.X < minimumX {
				minimumX = point.X
			}
			if point.X > maximumX {
				maximumX = point.X
			}
			if point.Y < minimumY {
				minimumY = point.Y
			}
			if point.Y > maximumY {
				maximumY = point.Y
			}
		}
		if int64(len(building.Footprint)) < (maximumX-minimumX+1)*(maximumY-minimumY+1) {
			hasIrregularFootprint = true
			break
		}
	}
	require.True(t, hasIrregularFootprint)
}

func TestDefaultWorldgenPlanProvidesCddaScaleBuildingEnvelopes(t *testing.T) {
	plan := testWorldgenPlan(t, 8110042)
	largest := plan.Buildings[0]
	for _, building := range plan.Buildings[1:] {
		if len(building.Footprint) > len(largest.Footprint) {
			largest = building
		}
	}
	require.GreaterOrEqual(t, len(largest.Footprint), 144)

	interior, err := GenerateWorldgenBuildingInterior(plan.Binding, largest, 0)
	require.NoError(t, err)
	furniture := 0
	for _, cell := range interior.Cells {
		if cell.Kind == BuildingLayoutCellFurniture {
			furniture++
		}
	}
	require.GreaterOrEqual(t, furniture, 10)
}

func TestGenerateWorldgenPlanChangesWithSeedAndFiltersWithoutBreakingGlobalRoads(t *testing.T) {
	first := testWorldgenPlan(t, 8110042)
	changed := testWorldgenPlan(t, 8110043)
	require.NotEqual(t, first.BaselineHash, changed.BaselineHash)

	window, err := FilterWorldgenPlan(first, WorldgenBounds{
		MinimumChunkX: -1, MaximumChunkX: 1, MinimumChunkY: -1, MaximumChunkY: 1, Z: SurfaceZ,
	})
	require.NoError(t, err)
	require.Equal(t, first.BaselineHash, window.PlanHash)
	require.Len(t, window.Terrain, 9)
	for _, lot := range window.Lots {
		require.True(t, worldgenRectangleIntersectsBounds(lot.Bounds, window.Bounds))
	}
	for _, road := range window.Roads {
		require.True(t, worldgenRoadIntersectsBounds(road, window.Bounds))
	}

	upperLevel, err := FilterWorldgenPlan(first, WorldgenBounds{
		MinimumChunkX: -1, MaximumChunkX: 1, MinimumChunkY: -1, MaximumChunkY: 1, Z: 1,
	})
	require.NoError(t, err)
	require.Empty(t, upperLevel.Terrain)
	require.Empty(t, upperLevel.Roads)
	require.Empty(t, upperLevel.Lots)
	require.Empty(t, upperLevel.Buildings)
}

func TestWorldgenUsesTerrainAwareCitySitingAndProfileDefinedDistrictMixes(t *testing.T) {
	plan := testWorldgenPlan(t, 8110042)
	terrainByChunk := make(map[WorldgenPoint]GeneratedWorldgenTerrainPatch, len(plan.Terrain))
	for _, terrain := range plan.Terrain {
		terrainByChunk[WorldgenPoint{X: terrain.ChunkX, Y: terrain.ChunkY, Z: terrain.Z}] = terrain
	}
	for _, city := range plan.Cities {
		chunkX, chunkY := worldgenChunkFromWorld(city.Center.X), worldgenChunkFromWorld(city.Center.Y)
		terrain, found := terrainByChunk[WorldgenPoint{X: chunkX, Y: chunkY, Z: city.Center.Z}]
		require.True(t, found)
		require.Equal(t, terrain.BiomeCode, city.BiomeCode)
		require.Equal(t, terrain.ElevationMilli, city.ElevationMilli)
		require.Equal(t, terrain.MoistureMilli, city.MoistureMilli)
		if city.PlacementMode == "preferred" {
			require.True(t, worldgenCityPlacementSuitable(plan.Profile, plan.Bounds, terrainByChunk, chunkX, chunkY))
		}
	}
	districts := make(map[string]WorldgenDistrictRule, len(plan.Profile.DistrictRules))
	for _, district := range plan.Profile.DistrictRules {
		districts[district.Code] = district
	}
	for _, lot := range plan.Lots {
		district, found := districts[lot.DistrictCode]
		require.True(t, found)
		require.True(t, worldgenDistrictAllowsUse(district, lot.PrimaryUse))
	}
}

func TestWorldgenProfileCanAddArchetypesWithoutChangingGeneratorCode(t *testing.T) {
	profile, err := DefaultWorldgenProfile()
	require.NoError(t, err)
	profile.BuildingArchetypes = []WorldgenBuildingArchetype{
		{Code: "commercial.test", PrimaryUse: LandUseCommercial, Weight: 1, MinimumWidth: 4, MaximumWidth: 8, MinimumDepth: 4, MaximumDepth: 8, MinimumFloors: 1, MaximumFloors: 2, LayoutStyle: "shopfront"},
		{Code: "industrial.test", PrimaryUse: LandUseIndustrial, Weight: 1, MinimumWidth: 4, MaximumWidth: 8, MinimumDepth: 4, MaximumDepth: 8, MinimumFloors: 1, MaximumFloors: 2, LayoutStyle: "workshop"},
		{Code: "residential.library", PrimaryUse: LandUseResidential, Weight: 1, MinimumWidth: 4, MaximumWidth: 8, MinimumDepth: 4, MaximumDepth: 8, MinimumFloors: 1, MaximumFloors: 2, LayoutStyle: "courtyard"},
	}
	normalized, err := normalizeWorldgenProfile(*profile)
	require.NoError(t, err)
	require.NotEqual(t, profile.ContentHash, normalized.ContentHash)
	binding, err := DefaultWorldgenBinding("city-f8-v3", 8110042, strings.Repeat("b", 64), &normalized)
	require.NoError(t, err)
	lot := GeneratedWorldgenLot{
		Code: "lot_test", PrimaryUse: LandUseResidential,
		Bounds: WorldgenRectangle{MinimumX: 0, MaximumX: 10, MinimumY: 0, MaximumY: 10, Z: SurfaceZ},
	}
	archetype, found := worldgenSelectArchetype(binding, normalized, lot)
	require.True(t, found)
	require.Equal(t, "residential.library", archetype.Code)
}

func TestWorldgenProfileCanAddTerrainAndDistrictsWithoutChangingGeneratorCode(t *testing.T) {
	profile, err := DefaultWorldgenProfile()
	require.NoError(t, err)
	profile.TerrainRules = []WorldgenTerrainRule{
		{Code: "test.upland", GroundDefinitionID: "terrain.grass", Priority: 1, MinimumElevationMilli: 0, MaximumElevationMilli: 1000, MinimumMoistureMilli: 0, MaximumMoistureMilli: 1000},
	}
	profile.CityPlacement = WorldgenCityPlacementRule{MinimumElevationMilli: 0, MaximumMoistureMilli: 1000, MinimumDryNeighbors: 0}
	profile.DistrictRules = []WorldgenDistrictRule{
		{Code: "maker_quarter", MinimumDistanceMilli: 0, MaximumDistanceMilli: 1000, UseWeights: []WorldgenLandUseWeight{{PrimaryUse: LandUseIndustrial, Weight: 1}}},
	}
	profile.BuildingArchetypes = []WorldgenBuildingArchetype{
		{Code: "industrial.makerspace", PrimaryUse: LandUseIndustrial, Weight: 1, MinimumWidth: 4, MaximumWidth: 8, MinimumDepth: 4, MaximumDepth: 8, MinimumFloors: 1, MaximumFloors: 2, LayoutStyle: "workshop", DistrictCodes: []string{"maker_quarter"}},
	}
	normalized, err := normalizeWorldgenProfile(*profile)
	require.NoError(t, err)
	binding, err := DefaultWorldgenBinding("city-f8-v3", 8110042, strings.Repeat("c", 64), &normalized)
	require.NoError(t, err)
	plan, err := GenerateWorldgenPlan(binding, &normalized, WorldgenBounds{
		MinimumChunkX: -4, MaximumChunkX: 4, MinimumChunkY: -4, MaximumChunkY: 4, Z: SurfaceZ,
	})
	require.NoError(t, err)
	require.NotEmpty(t, plan.Lots)
	for _, terrain := range plan.Terrain {
		require.Equal(t, "test.upland", terrain.BiomeCode)
	}
	for _, city := range plan.Cities {
		require.Equal(t, "preferred", city.PlacementMode)
		require.Equal(t, "test.upland", city.BiomeCode)
	}
	for _, lot := range plan.Lots {
		require.Equal(t, "maker_quarter", lot.DistrictCode)
		require.Equal(t, LandUseIndustrial, lot.PrimaryUse)
	}
	for _, building := range plan.Buildings {
		require.Equal(t, "industrial.makerspace", building.ArchetypeCode)
	}
}

func TestWorldgenMarksAForcedUnsuitableCitySeedAsFallback(t *testing.T) {
	profile, err := DefaultWorldgenProfile()
	require.NoError(t, err)
	profile.CityPlacement = WorldgenCityPlacementRule{
		ExcludedBiomeCodes:    []string{"water.deep", "water.shallow", "wetland", "woodland", "meadow"},
		MinimumElevationMilli: 0,
		MaximumMoistureMilli:  1000,
		MinimumDryNeighbors:   0,
	}
	normalized, err := normalizeWorldgenProfile(*profile)
	require.NoError(t, err)
	binding, err := DefaultWorldgenBinding("city-f8-v3", 8110042, strings.Repeat("d", 64), &normalized)
	require.NoError(t, err)
	plan, err := GenerateWorldgenPlan(binding, &normalized, WorldgenBounds{
		MinimumChunkX: -4, MaximumChunkX: 4, MinimumChunkY: -4, MaximumChunkY: 4, Z: SurfaceZ,
	})
	require.NoError(t, err)
	require.NotEmpty(t, plan.Cities)
	for _, city := range plan.Cities {
		require.Equal(t, "fallback", city.PlacementMode)
	}
}

func TestWorldgenProfileRejectsASelectableDistrictUseWithoutAViableArchetype(t *testing.T) {
	profile, err := DefaultWorldgenProfile()
	require.NoError(t, err)
	profile.DistrictRules = []WorldgenDistrictRule{
		{Code: "tower_only", MinimumDistanceMilli: 0, MaximumDistanceMilli: 1000, UseWeights: []WorldgenLandUseWeight{{PrimaryUse: LandUseCommercial, Weight: 1}}},
	}
	profile.BuildingArchetypes = []WorldgenBuildingArchetype{
		{Code: "commercial.too_large", PrimaryUse: LandUseCommercial, Weight: 1, MinimumWidth: 8, MaximumWidth: 9, MinimumDepth: 8, MaximumDepth: 9, MinimumFloors: 1, MaximumFloors: 2, LayoutStyle: "tower", DistrictCodes: []string{"tower_only"}},
	}
	_, err = normalizeWorldgenProfile(*profile)
	require.ErrorIs(t, err, ErrInvalidWorldgenInput)
}
