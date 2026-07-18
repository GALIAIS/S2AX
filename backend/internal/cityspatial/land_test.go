package cityspatial

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func testLandDistricts() []DistrictLandSeed {
	capacities := map[string][3]int64{
		"central": {18000, 9000, 2500}, "north": {14000, 3500, 7000},
		"south": {16000, 4500, 5000}, "east": {12000, 5000, 9000},
		"west": {11000, 3000, 10500}, "harbor": {8000, 6000, 8000},
	}
	areas := map[string][2]int64{
		"central": {12000000, 5400000}, "north": {18000000, 10800000},
		"south": {16000000, 9600000}, "east": {20000000, 13000000},
		"west": {22000000, 14300000}, "harbor": {14000000, 7000000},
	}
	result := make([]DistrictLandSeed, 0, len(requiredDistrictCodes))
	for index, code := range requiredDistrictCodes {
		result = append(result, DistrictLandSeed{
			Code: code, SortOrder: (index + 1) * 10, AreaSQM: areas[code][0],
			DevelopableAreaSQM: areas[code][1], ResidentialCapacityUnits: capacities[code][0],
			CommercialCapacityUnits: capacities[code][1], IndustrialCapacityUnits: capacities[code][2],
			Households: []HouseholdLandSeed{
				{EntityCode: "founding_household", IncomeBand: "low", HouseholdUnits: 400},
				{EntityCode: "founding_household", IncomeBand: "middle", HouseholdUnits: 250},
				{EntityCode: "founding_household", IncomeBand: "high", HouseholdUnits: 150},
			},
		})
	}
	return result
}

func testLandFoundation(t *testing.T) *GeneratedLandFoundation {
	t.Helper()
	registry, err := DefaultRegistry()
	require.NoError(t, err)
	spatialRules, err := registry.Get(DefaultRuleSetID)
	require.NoError(t, err)
	spatialBinding, err := DefaultGeneratorBinding("city-f7-v1", 710042, spatialRules)
	require.NoError(t, err)
	overmap, err := GenerateDefaultOvermap(spatialBinding, requiredDistrictCodes)
	require.NoError(t, err)
	landRules, err := DefaultLandRuleSet()
	require.NoError(t, err)
	landBinding, err := DefaultLandGeneratorBinding(
		"city-f7-v2", 710042, spatialRules.ContentHash, overmap.RootHash, landRules,
	)
	require.NoError(t, err)
	foundation, err := GenerateDefaultLandFoundation(landBinding, landRules, overmap, testLandDistricts())
	require.NoError(t, err)
	return foundation
}

func TestDefaultLandRuleSetHasStableHash(t *testing.T) {
	ruleSet, err := DefaultLandRuleSet()
	require.NoError(t, err)
	require.Equal(t, "4275912ce56d967b3596c5449ef28097623b0d1a9b80ea5f60e1a882f79e60c2", ruleSet.ContentHash)
	require.Len(t, ruleSet.Rules, 3)

	copyRuleSet, err := DefaultLandRuleSet()
	require.NoError(t, err)
	copyRuleSet.Rules[0].MaxFloors = 1
	again, err := DefaultLandRuleSet()
	require.NoError(t, err)
	require.NotEqual(t, copyRuleSet.Rules[0].MaxFloors, again.Rules[0].MaxFloors)
}

func TestGenerateDefaultLandFoundationIsDeterministicAndConservesCapacity(t *testing.T) {
	first := testLandFoundation(t)
	second := testLandFoundation(t)
	require.Equal(t, "d12efde84b840613b6e21f2e2ae3b846f8968c45bbedb1155fb5479003d49a51", first.BaselineHash)
	require.Equal(t, first.BaselineHash, second.BaselineHash)
	require.Equal(t, first, second)
	require.NotEmpty(t, first.Parcels)
	require.Len(t, first.Buildings, len(first.UnitPools))

	districts := testLandDistricts()
	for _, district := range districts {
		var area, residential, commercial, industrial, occupied int64
		for _, parcel := range first.Parcels {
			if parcel.DistrictCode == district.Code {
				area += parcel.AreaSQM
			}
		}
		for _, building := range first.Buildings {
			if building.DistrictCode != district.Code {
				continue
			}
			switch building.PrimaryUse {
			case LandUseResidential:
				residential += building.CapacityUnits
				occupied += building.OccupiedUnits
			case LandUseCommercial:
				commercial += building.CapacityUnits
			case LandUseIndustrial:
				industrial += building.CapacityUnits
			}
		}
		require.Equal(t, district.DevelopableAreaSQM, area, district.Code)
		require.Equal(t, district.ResidentialCapacityUnits, residential, district.Code)
		require.Equal(t, district.CommercialCapacityUnits, commercial, district.Code)
		require.Equal(t, district.IndustrialCapacityUnits, industrial, district.Code)
		require.Equal(t, int64(800), occupied, district.Code)
	}
}

func TestGenerateDefaultLandFoundationChangesWithSeedAndRejectsHousingOverflow(t *testing.T) {
	base := testLandFoundation(t)
	registry, err := DefaultRegistry()
	require.NoError(t, err)
	spatialRules, err := registry.Get(DefaultRuleSetID)
	require.NoError(t, err)
	spatialBinding, err := DefaultGeneratorBinding("city-f7-v1", 710043, spatialRules)
	require.NoError(t, err)
	overmap, err := GenerateDefaultOvermap(spatialBinding, requiredDistrictCodes)
	require.NoError(t, err)
	landRules, err := DefaultLandRuleSet()
	require.NoError(t, err)
	binding, err := DefaultLandGeneratorBinding("city-f7-v2", 710043, spatialRules.ContentHash, overmap.RootHash, landRules)
	require.NoError(t, err)
	changed, err := GenerateDefaultLandFoundation(binding, landRules, overmap, testLandDistricts())
	require.NoError(t, err)
	require.NotEqual(t, base.BaselineHash, changed.BaselineHash)

	overflow := testLandDistricts()
	overflow[0].Households[0].HouseholdUnits = overflow[0].ResidentialCapacityUnits + 1
	_, err = GenerateDefaultLandFoundation(binding, landRules, overmap, overflow)
	require.ErrorIs(t, err, ErrLandCapacityOverflow)
}

func TestLandFootprintsAvoidTheRoadCorridorAndPortalsConnectAdjacentLevels(t *testing.T) {
	foundation := testLandFoundation(t)
	for _, parcel := range foundation.Parcels {
		require.True(t, parcel.Geometry.LocalMaxX <= 12 || parcel.Geometry.LocalMinX >= 19)
		require.True(t, parcel.Geometry.LocalMaxY <= 12 || parcel.Geometry.LocalMinY >= 19)
	}
	for _, portal := range foundation.Portals {
		if portal.PortalType == "stair" {
			require.Equal(t, portal.FromZ+1, portal.ToZ)
			require.Equal(t, portal.FromX, portal.ToX)
			require.Equal(t, portal.FromY, portal.ToY)
		}
	}
}
