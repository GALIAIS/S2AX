package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func TestFilterCityLandStateKeepsTheCompleteSelectedBuildingFactChain(t *testing.T) {
	full := &CityLandState{
		Profile: CityLandProfile{
			ParcelCount: 2, BuildingCount: 2, UnitPoolCount: 2,
			HousingAllocationCount: 1, PortalCount: 3,
		},
		ZoningRules: []cityspatial.LandZoningRule{{Code: "residential"}},
		Parcels: []cityspatial.GeneratedParcel{
			{
				Code: "parcel_selected", DistrictCode: "central", ZoneCode: "residential",
				Geometry: cityspatial.LandRectangle{
					ChunkX: 0, ChunkY: 0, Z: 0,
					LocalMinX: 1, LocalMinY: 1, LocalMaxX: 12, LocalMaxY: 12,
				},
			},
			{
				Code: "parcel_outside", DistrictCode: "east", ZoneCode: "commercial",
				Geometry: cityspatial.LandRectangle{
					ChunkX: 1, ChunkY: 0, Z: 0,
					LocalMinX: 1, LocalMinY: 1, LocalMaxX: 12, LocalMaxY: 12,
				},
			},
		},
		Buildings: []cityspatial.GeneratedBuilding{
			{
				Code: "building_selected", ParcelCode: "parcel_selected", DistrictCode: "central",
				PrimaryUse: "residential", Footprint: cityspatial.LandRectangle{ChunkX: 0, ChunkY: 0, Z: 0},
				BaseZ: 0, TopZ: 2,
			},
			{
				Code: "building_outside", ParcelCode: "parcel_outside", DistrictCode: "east",
				PrimaryUse: "commercial", Footprint: cityspatial.LandRectangle{ChunkX: 1, ChunkY: 0, Z: 0},
				BaseZ: 0, TopZ: 0,
			},
		},
		UnitPools: []cityspatial.GeneratedBuildingUnitPool{
			{Code: "pool_selected", BuildingCode: "building_selected"},
			{Code: "pool_outside", BuildingCode: "building_outside"},
		},
		HousingAllocations: []cityspatial.GeneratedHousingAllocation{
			{PoolCode: "pool_selected", CohortKey: "central/household/medium", AllocatedUnits: 12},
		},
		Portals: []cityspatial.GeneratedBuildingPortal{
			{Code: "entrance", BuildingCode: "building_selected", PortalType: "entrance", FromZ: 0, ToZ: 0},
			{Code: "stair_000_001", BuildingCode: "building_selected", PortalType: "stair", FromZ: 0, ToZ: 1},
			{Code: "outside", BuildingCode: "building_outside", PortalType: "entrance", FromZ: 0, ToZ: 0},
		},
	}

	filtered := filterCityLandState(full, CityLandQueryInput{
		MinimumX: 0, MaximumX: 0, MinimumY: 0, MaximumY: 0, Z: 1,
	})

	require.Equal(t, full.Profile, filtered.Profile)
	require.Equal(t, full.ZoningRules, filtered.ZoningRules)
	require.Len(t, filtered.Parcels, 1)
	require.Equal(t, "parcel_selected", filtered.Parcels[0].Code)
	require.Len(t, filtered.Buildings, 1)
	require.Equal(t, "building_selected", filtered.Buildings[0].Code)
	require.Len(t, filtered.UnitPools, 1)
	require.Len(t, filtered.HousingAllocations, 1)
	require.Len(t, filtered.Portals, 1)
	require.Equal(t, "stair_000_001", filtered.Portals[0].Code)

	filtered.Parcels[0].Code = "mutated-copy"
	require.Equal(t, "parcel_selected", full.Parcels[0].Code)
}

func TestFilterCityLandStateRejectsObjectsOutsideTheRequestedBoundsAndLevel(t *testing.T) {
	full := &CityLandState{
		Parcels: []cityspatial.GeneratedParcel{{
			Code: "surface", Geometry: cityspatial.LandRectangle{ChunkX: 0, ChunkY: 0, Z: 0},
		}},
		Buildings: []cityspatial.GeneratedBuilding{{
			Code: "surface", ParcelCode: "surface",
			Footprint: cityspatial.LandRectangle{ChunkX: 0, ChunkY: 0, Z: 0}, BaseZ: 0, TopZ: 0,
		}},
	}

	filtered := filterCityLandState(full, CityLandQueryInput{
		MinimumX: 0, MaximumX: 0, MinimumY: 0, MaximumY: 0, Z: 1,
	})

	require.Empty(t, filtered.Parcels)
	require.Empty(t, filtered.Buildings)
}

func TestApplyCityBuildingAdjustmentsBuildsEffectiveProjectionAndVerticalPortals(t *testing.T) {
	state := &CityLandState{
		Buildings: []cityspatial.GeneratedBuilding{{
			Code: "building_central", DistrictCode: "central", PrimaryUse: "residential",
			Footprint: cityspatial.LandRectangle{
				ChunkX: 1, ChunkY: -1, LocalMinX: 4, LocalMinY: 6, LocalMaxX: 10, LocalMaxY: 12,
			},
			BaseZ: 0, TopZ: 1, FloorCount: 2, FloorAreaSQM: 400,
			CapacityUnits: 8, QualityMilli: 1000, CompletedTick: 0, Version: 1,
		}},
		UnitPools: []cityspatial.GeneratedBuildingUnitPool{{
			Code: "pool_building_central", BuildingCode: "building_central",
			DistrictCode: "central", UnitCount: 8, CapacityUnitsPerUnit: 1, Version: 1,
		}},
		Portals: []cityspatial.GeneratedBuildingPortal{{
			Code: "stair_000_001", BuildingCode: "building_central", DistrictCode: "central",
			PortalType: "stair", FromZ: 0, ToZ: 1, Status: "active", Version: 1,
		}},
	}
	adjustments := []CityBuildingAdjustment{
		{
			ProjectCode: "development_7", BuildingCode: "building_central", DistrictCode: "central",
			AddedFloorCount: 2, AddedTopZ: 2, AddedFloorAreaSQM: 400,
			AddedCapacityUnits: 8, CompletedTick: 12,
		},
		{
			ProjectCode: "development_9", BuildingCode: "building_central", DistrictCode: "central",
			QualityDeltaMilli: 125, CompletedTick: 20,
		},
	}

	require.NoError(t, applyCityBuildingAdjustments(state, adjustments))
	require.Equal(t, int32(4), state.Buildings[0].FloorCount)
	require.Equal(t, int32(3), state.Buildings[0].TopZ)
	require.Equal(t, int64(800), state.Buildings[0].FloorAreaSQM)
	require.Equal(t, int64(16), state.Buildings[0].CapacityUnits)
	require.Equal(t, int64(1125), state.Buildings[0].QualityMilli)
	require.Equal(t, int64(20), state.Buildings[0].CompletedTick)
	require.Equal(t, int64(3), state.Buildings[0].Version)
	require.Equal(t, int64(16), state.UnitPools[0].UnitCount)
	require.Equal(t, int64(3), state.UnitPools[0].Version)
	require.Len(t, state.Portals, 3)
	require.Equal(t, int32(1), state.Portals[1].FromZ)
	require.Equal(t, int32(2), state.Portals[1].ToZ)
	require.Equal(t, int64(39), state.Portals[1].FromX)
	require.Equal(t, int64(-23), state.Portals[1].FromY)
	require.Equal(t, int32(2), state.Portals[2].FromZ)
	require.Equal(t, int32(3), state.Portals[2].ToZ)
}

func TestApplyCityBuildingAdjustmentsRejectsBrokenProjectionLinks(t *testing.T) {
	state := &CityLandState{
		Buildings: []cityspatial.GeneratedBuilding{{
			Code: "building_central", DistrictCode: "central", FloorCount: 1, Version: 1,
		}},
	}
	err := applyCityBuildingAdjustments(state, []CityBuildingAdjustment{{
		ProjectCode: "development_7", BuildingCode: "building_missing", DistrictCode: "central",
	}})
	require.Error(t, err)
}
