package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func TestCityEngineDefinitionsKeepLegacyAndCurrentPipelinesSeparate(t *testing.T) {
	f5, err := cityEngineForVersion(CitySimulationVersionF5)
	require.NoError(t, err)
	require.False(t, f5.hasStage(cityEngineStageCalendarDemography))
	require.Equal(t, []string{CitySimulationVersionF6}, cityEngineUpgradeTargets(CitySimulationVersionF5))

	f6, err := cityEngineForVersion(CitySimulationVersionF6)
	require.NoError(t, err)
	require.True(t, f6.hasStage(cityEngineStageCalendarDemography))
	require.Equal(t, []string{CitySimulationVersionF6V2}, cityEngineUpgradeTargets(CitySimulationVersionF6))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF5, CitySimulationVersionF6))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionF6, CitySimulationVersionF5))
	require.True(t, f6.supportsCommand(CityCommandTypeWorldRename))
	require.True(t, f6.supportsCommand(CityCommandTypeLedgerCashTransfer))
	require.True(t, f6.supportsCommand(CityCommandTypeResourceTransfer))
	require.False(t, f6.supportsCommand(CityCommandTypePopulationRelocate))
	require.False(t, f6.supportsCommand("unknown.command"))

	f6v2, err := cityEngineForVersion(CitySimulationVersionF6V2)
	require.NoError(t, err)
	require.True(t, f6v2.hasStage(cityEngineStageCalendarDemography))
	require.True(t, f6v2.supportsCommand(CityCommandTypePopulationImmigrate))
	require.True(t, f6v2.supportsCommand(CityCommandTypePopulationEmigrate))
	require.True(t, f6v2.supportsCommand(CityCommandTypePopulationRelocate))
	require.False(t, f6v2.supportsCommand(CityCommandTypeHouseholdAdjust))
	require.Equal(t, []string{CitySimulationVersionF6V3}, cityEngineUpgradeTargets(CitySimulationVersionF6V2))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF6, CitySimulationVersionF6V2))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionF6V2, CitySimulationVersionF6))

	f6v3, err := cityEngineForVersion(CitySimulationVersionF6V3)
	require.NoError(t, err)
	require.True(t, f6v3.supportsCommand(CityCommandTypePopulationRelocate))
	require.True(t, f6v3.supportsCommand(CityCommandTypeHouseholdAdjust))
	require.True(t, f6v3.supportsCommand(CityCommandTypeHouseholdReclassify))
	require.False(t, f6v3.supportsCommand(CityCommandTypeSpatialGenerateChunk))
	require.Equal(t, []string{CitySimulationVersionF7}, cityEngineUpgradeTargets(CitySimulationVersionF6V3))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF6V2, CitySimulationVersionF6V3))

	f7, err := cityEngineForVersion(CitySimulationVersionF7)
	require.NoError(t, err)
	require.True(t, f7.hasStage(cityEngineStageSpatial))
	require.True(t, f7.supportsCommand(CityCommandTypePopulationRelocate))
	require.True(t, f7.supportsCommand(CityCommandTypeHouseholdAdjust))
	require.True(t, f7.supportsCommand(CityCommandTypeSpatialGenerateChunk))
	require.Equal(t, []string{CitySimulationVersionF7V2}, cityEngineUpgradeTargets(CitySimulationVersionF7))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF6V3, CitySimulationVersionF7))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionF7, CitySimulationVersionF6V3))

	f7v2, err := cityEngineForVersion(CitySimulationVersionF7V2)
	require.NoError(t, err)
	require.True(t, f7v2.hasStage(cityEngineStageSpatial))
	require.True(t, f7v2.supportsCommand(CityCommandTypeSpatialGenerateChunk))
	require.False(t, f7v2.supportsCommand(CityCommandTypeDevelopmentSubmit))
	require.Equal(t, []string{CitySimulationVersionF7V3}, cityEngineUpgradeTargets(CitySimulationVersionF7V2))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7, CitySimulationVersionF7V2))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V2, CitySimulationVersionF7V3))

	f7v3, err := cityEngineForVersion(CitySimulationVersionF7V3)
	require.NoError(t, err)
	require.True(t, f7v3.hasStage(cityEngineStageSpatial))
	require.True(t, cityEngineSupportsLand(CitySimulationVersionF7V3))
	require.True(t, f7v3.hasStage(cityEngineStageDevelopment))
	require.True(t, f7v3.supportsCommand(CityCommandTypeDevelopmentSubmit))
	require.True(t, f7v3.supportsCommand(CityCommandTypeDevelopmentReview))
	require.True(t, f7v3.supportsCommand(CityCommandTypeDevelopmentStart))
	require.True(t, f7v3.supportsCommand(CityCommandTypeDevelopmentCancel))
	require.False(t, f7v3.supportsCommand(CityCommandTypeEnterpriseSiteOpen))
	require.Equal(t, []string{CitySimulationVersionF7V4}, cityEngineUpgradeTargets(CitySimulationVersionF7V3))

	f7v4, err := cityEngineForVersion(CitySimulationVersionF7V4)
	require.NoError(t, err)
	require.True(t, f7v4.hasStage(cityEngineStageEnterpriseLocation))
	require.True(t, f7v4.supportsCommand(CityCommandTypeDevelopmentSubmit))
	require.True(t, f7v4.supportsCommand(CityCommandTypeEnterpriseSiteOpen))
	require.True(t, f7v4.supportsCommand(CityCommandTypeEnterpriseSiteResize))
	require.True(t, f7v4.supportsCommand(CityCommandTypeEnterpriseSiteClose))
	require.True(t, f7v4.supportsCommand(CityCommandTypeEnterpriseRelocate))
	require.Equal(t, []string{CitySimulationVersionF7V5}, cityEngineUpgradeTargets(CitySimulationVersionF7V4))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V3, CitySimulationVersionF7V4))

	f7v5, err := cityEngineForVersion(CitySimulationVersionF7V5)
	require.NoError(t, err)
	require.True(t, f7v5.hasStage(cityEngineStageWorldRuntime))
	require.True(t, f7v5.supportsCommand(CityCommandTypeActorCreate))
	require.True(t, f7v5.supportsCommand(CityCommandTypeActorActivityPerform))
	require.True(t, f7v5.supportsCommand(CityCommandTypeActorRoleTransition))
	require.Empty(t, cityEngineUpgradeTargets(CitySimulationVersionF7V5))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V4, CitySimulationVersionF7V5))
	require.False(t, f7v4.supportsCommand(CityCommandTypeActorCreate))

	_, err = cityEngineForVersion("city-unknown-v1")
	require.Error(t, err)
}

func TestCityEngineDefinitionRejectsInvalidSubsystemGraphs(t *testing.T) {
	for _, engine := range []cityEngineDefinition{
		{version: "missing-control", stages: []cityEngineStage{cityEngineStageLedger}},
		{version: "duplicate", stages: []cityEngineStage{cityEngineStageControl, cityEngineStageControl}},
		{version: "missing-ledger", stages: []cityEngineStage{cityEngineStageControl, cityEngineStageResources}},
		{version: "wrong-order", stages: []cityEngineStage{
			cityEngineStageControl, cityEngineStageLedger, cityEngineStageMarkets, cityEngineStageResources,
		}},
		{version: "unknown", stages: []cityEngineStage{cityEngineStageControl, "mystery"}},
	} {
		require.Error(t, engine.validate(), engine.version)
	}
}

func TestMarshalCanonicalCityStatePreservesVersionedShape(t *testing.T) {
	f5, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF5,
		Demography:        cityDemographyHashState{Calendar: cityDemographyHashCalendar{LocalDate: "2000-01-01"}},
	})
	require.NoError(t, err)
	require.NotContains(t, string(f5), `"demography"`)

	f6, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF6,
		Demography:        cityDemographyHashState{Calendar: cityDemographyHashCalendar{LocalDate: "2000-01-01"}},
	})
	require.NoError(t, err)
	require.Contains(t, string(f6), `"demography"`)

	f6v2, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF6V2,
		Demography:        cityDemographyHashState{Calendar: cityDemographyHashCalendar{LocalDate: "2000-01-01"}},
		Physical:          cityPhysicalHashState{HouseholdCohorts: []cityHashHouseholdCohort{{HouseholdUnits: 2}}},
	})
	require.NoError(t, err)
	require.Contains(t, string(f6v2), `"demography"`)
	require.NotContains(t, string(f6v2), `"household_units"`)
	require.NotEqual(t, string(f6), string(f6v2))

	f6v3, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF6V3,
		Physical:          cityPhysicalHashState{HouseholdCohorts: []cityHashHouseholdCohort{{HouseholdUnits: 2}}},
	})
	require.NoError(t, err)
	require.Contains(t, string(f6v3), `"household_units":2`)
	require.NotContains(t, string(f6v3), `"spatial"`)

	f7, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7,
		Physical:          cityPhysicalHashState{HouseholdCohorts: []cityHashHouseholdCohort{{HouseholdUnits: 2}}},
		Spatial: &citySpatialHashState{
			Profile: cityHashSpatialProfile{Metadata: json.RawMessage(`{}`)},
			Chunks:  make([]cityHashSpatialChunk, 0),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7), `"spatial"`)
	require.Contains(t, string(f7), `"household_units":2`)
	require.NotContains(t, string(f7), `"land"`)

	f7v2, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7V2,
		Spatial: &citySpatialHashState{
			Profile: cityHashSpatialProfile{Metadata: json.RawMessage(`{}`)},
			Chunks:  make([]cityHashSpatialChunk, 0),
		},
		Land: &cityLandHashState{
			ZoningRules:        make([]cityspatial.LandZoningRule, 0),
			Parcels:            make([]cityspatial.GeneratedParcel, 0),
			Buildings:          make([]cityspatial.GeneratedBuilding, 0),
			UnitPools:          make([]cityspatial.GeneratedBuildingUnitPool, 0),
			HousingAllocations: make([]cityspatial.GeneratedHousingAllocation, 0),
			Portals:            make([]cityspatial.GeneratedBuildingPortal, 0),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v2), `"land"`)
	require.NotContains(t, string(f7v2), `"development"`)

	f7v3, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7V3,
		Spatial: &citySpatialHashState{
			Profile: cityHashSpatialProfile{Metadata: json.RawMessage(`{}`)},
			Chunks:  make([]cityHashSpatialChunk, 0),
		},
		Land: &cityLandHashState{
			ZoningRules:        make([]cityspatial.LandZoningRule, 0),
			Parcels:            make([]cityspatial.GeneratedParcel, 0),
			Buildings:          make([]cityspatial.GeneratedBuilding, 0),
			UnitPools:          make([]cityspatial.GeneratedBuildingUnitPool, 0),
			HousingAllocations: make([]cityspatial.GeneratedHousingAllocation, 0),
			Portals:            make([]cityspatial.GeneratedBuildingPortal, 0),
		},
		Development: &cityDevelopmentHashState{
			Projects:    make([]CityDevelopmentProject, 0),
			Facts:       make([]CityDevelopmentFact, 0),
			Adjustments: make([]CityBuildingAdjustment, 0),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v3), `"development"`)
	require.NotContains(t, string(f7v3), `"enterprise_location"`)

	f7v4, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7V4,
		Spatial:           &citySpatialHashState{},
		Land:              &cityLandHashState{},
		Development:       &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{
			BaselineSites: make([]CityEnterpriseSite, 0),
			Sites:         make([]CityEnterpriseSite, 0),
			Facts:         make([]CityEnterpriseLocationFact, 0),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v4), `"enterprise_location"`)
	require.NotContains(t, string(f7v4), `"world_runtime"`)

	f7v5, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V5,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
			Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
			Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
			Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v5), `"world_runtime"`)

	_, err = marshalCanonicalCityState(cityHashState{SimulationVersion: CitySimulationVersionF7})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7V2,
		Spatial:           &citySpatialHashState{},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7V3,
		Spatial:           &citySpatialHashState{},
		Land:              &cityLandHashState{},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7V4,
		Spatial:           &citySpatialHashState{},
		Land:              &cityLandHashState{},
		Development:       &cityDevelopmentHashState{},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V5,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
	})
	require.Error(t, err)
}
