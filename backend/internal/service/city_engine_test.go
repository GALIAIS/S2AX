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

	openWorldV2, err := cityEngineForVersion(CitySimulationVersionOpenWorldV2)
	require.NoError(t, err)
	require.True(t, openWorldV2.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV2.supportsCommand(CityCommandTypeOpenWorldSectorMaterialize))
	require.False(t, f6.supportsCommand(CityCommandTypeOpenWorldSectorMaterialize))
	require.True(t, cityEngineSupportsOpenWorld(CitySimulationVersionOpenWorldV2))
	require.True(t, cityEngineSupportsOpenWorldMaterialization(CitySimulationVersionOpenWorldV2))
	require.False(t, cityEngineSupportsOpenWorldMaterialization(CitySimulationVersionOpenWorld))
	openWorldV3, err := cityEngineForVersion(CitySimulationVersionOpenWorldV3)
	require.NoError(t, err)
	require.True(t, openWorldV3.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV3.supportsCommand(CityCommandTypeOpenWorldSectorMaterialize))
	require.True(t, cityEngineSupportsOpenWorldMaterialization(CitySimulationVersionOpenWorldV3))
	require.True(t, cityEngineSupportsOpenWorldVerticalTopology(CitySimulationVersionOpenWorldV3))
	require.False(t, cityEngineSupportsOpenWorldVerticalTopology(CitySimulationVersionOpenWorldV2))

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
	require.False(t, f7v5.supportsCommand(CityCommandTypeActorLocationMove))
	require.Equal(t, []string{CitySimulationVersionF7V6}, cityEngineUpgradeTargets(CitySimulationVersionF7V5))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V4, CitySimulationVersionF7V5))
	require.False(t, f7v4.supportsCommand(CityCommandTypeActorCreate))

	f7v6, err := cityEngineForVersion(CitySimulationVersionF7V6)
	require.NoError(t, err)
	require.True(t, f7v6.hasStage(cityEngineStageWorldRuntime))
	require.True(t, f7v6.supportsCommand(CityCommandTypeActorLocationMove))
	require.True(t, f7v6.supportsCommand(CityCommandTypeActorControlGrant))
	require.True(t, f7v6.supportsCommand(CityCommandTypeActorControlRevoke))
	require.Equal(t, []string{CitySimulationVersionF7V7}, cityEngineUpgradeTargets(CitySimulationVersionF7V6))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V5, CitySimulationVersionF7V6))
	require.False(t, cityEngineSupportsWorldActorNavigation(CitySimulationVersionF7V6))

	f7v7, err := cityEngineForVersion(CitySimulationVersionF7V7)
	require.NoError(t, err)
	require.True(t, f7v7.hasStage(cityEngineStageWorldRuntime))
	require.True(t, f7v7.supportsCommand(CityCommandTypeActorLocationMove))
	require.True(t, cityEngineSupportsWorldActorNavigation(CitySimulationVersionF7V7))
	require.False(t, cityEngineSupportsWorldPortalAccess(CitySimulationVersionF7V7))
	require.Equal(t, []string{CitySimulationVersionF7V8}, cityEngineUpgradeTargets(CitySimulationVersionF7V7))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V6, CitySimulationVersionF7V7))

	f7v8, err := cityEngineForVersion(CitySimulationVersionF7V8)
	require.NoError(t, err)
	require.True(t, f7v8.hasStage(cityEngineStageWorldRuntime))
	require.True(t, f7v8.supportsCommand(CityCommandTypeActorLocationMove))
	require.True(t, f7v8.supportsCommand(CityCommandTypePortalStateTransition))
	require.True(t, f7v8.supportsCommand(CityCommandTypePortalAccessConfigure))
	require.True(t, cityEngineSupportsWorldActorNavigation(CitySimulationVersionF7V8))
	require.True(t, cityEngineSupportsWorldPortalAccess(CitySimulationVersionF7V8))
	require.False(t, cityEngineSupportsWorldNavigationIntents(CitySimulationVersionF7V8))
	require.Equal(t, []string{CitySimulationVersionF7V9}, cityEngineUpgradeTargets(CitySimulationVersionF7V8))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V7, CitySimulationVersionF7V8))

	f7v9, err := cityEngineForVersion(CitySimulationVersionF7V9)
	require.NoError(t, err)
	require.True(t, f7v9.supportsCommand(CityCommandTypeActorNavigationIntentSet))
	require.True(t, f7v9.supportsCommand(CityCommandTypeActorNavigationIntentCancel))
	require.True(t, cityEngineSupportsWorldNavigationIntents(CitySimulationVersionF7V9))
	require.False(t, f7v9.supportsCommand(CityCommandTypeFacilityRegister))
	require.Equal(t, []string{CitySimulationVersionF8}, cityEngineUpgradeTargets(CitySimulationVersionF7V9))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V8, CitySimulationVersionF7V9))

	f8, err := cityEngineForVersion(CitySimulationVersionF8)
	require.NoError(t, err)
	require.True(t, f8.hasStage(cityEngineStagePublicServices))
	require.True(t, f8.supportsCommand(CityCommandTypeFacilityRegister))
	require.True(t, f8.supportsCommand(CityCommandTypeFacilityStatusTransition))
	require.True(t, f8.supportsCommand(CityCommandTypeFacilityCapacityConfigure))
	require.True(t, f8.supportsCommand(CityCommandTypeServiceDemandConfigure))
	require.True(t, f8.supportsCommand(CityCommandTypeServiceConnectionConfigure))
	require.True(t, cityEngineSupportsWorldActorSpatialControl(CitySimulationVersionF8))
	require.True(t, cityEngineSupportsWorldActorNavigation(CitySimulationVersionF8))
	require.True(t, cityEngineSupportsWorldPortalAccess(CitySimulationVersionF8))
	require.True(t, cityEngineSupportsWorldNavigationIntents(CitySimulationVersionF8))
	require.True(t, cityEngineSupportsPublicServices(CitySimulationVersionF8))
	require.False(t, cityEngineSupportsFacilityLifecycle(CitySimulationVersionF8))
	require.Equal(t, []string{CitySimulationVersionF8V2}, cityEngineUpgradeTargets(CitySimulationVersionF8))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V9, CitySimulationVersionF8))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF8, CitySimulationVersionF8V2))

	f8v2, err := cityEngineForVersion(CitySimulationVersionF8V2)
	require.NoError(t, err)
	require.True(t, f8v2.hasStage(cityEngineStagePublicServices))
	require.True(t, f8v2.supportsCommand(CityCommandTypeFacilityOperationSchedule))
	require.True(t, f8v2.supportsCommand(CityCommandTypeFacilityOperationStart))
	require.True(t, f8v2.supportsCommand(CityCommandTypeFacilityOperationCancel))
	require.True(t, f8v2.supportsCommand(CityCommandTypeFacilityStaffingConfigure))
	require.True(t, cityEngineSupportsFacilityLifecycle(CitySimulationVersionF8V2))
	require.False(t, f8v2.supportsCommand(CityCommandTypePhysicalNetworkConfigure))
	require.Equal(t, []string{CitySimulationVersionF8V3}, cityEngineUpgradeTargets(CitySimulationVersionF8V2))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF8V2, CitySimulationVersionF8V3))

	f8v3, err := cityEngineForVersion(CitySimulationVersionF8V3)
	require.NoError(t, err)
	require.True(t, f8v3.hasStage(cityEngineStagePublicServices))
	require.True(t, cityEngineSupportsFacilityLifecycle(CitySimulationVersionF8V3))
	require.True(t, cityEngineSupportsPhysicalNetworks(CitySimulationVersionF8V3))
	require.True(t, f8v3.supportsCommand(CityCommandTypePhysicalNetworkConfigure))
	require.True(t, f8v3.supportsCommand(CityCommandTypePhysicalNodeConfigure))
	require.True(t, f8v3.supportsCommand(CityCommandTypePhysicalEdgeConfigure))
	require.True(t, f8v3.supportsCommand(CityCommandTypePhysicalEdgeTransition))
	require.Empty(t, cityEngineUpgradeTargets(CitySimulationVersionF8V3))

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
		{version: "service-without-runtime", stages: []cityEngineStage{
			cityEngineStageControl, cityEngineStageLedger, cityEngineStageResources,
			cityEngineStagePublicServices, cityEngineStageMarkets,
		}},
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
	require.NotContains(t, string(f7v5), `"locations"`)
	require.NotContains(t, string(f7v5), `"control_grants"`)

	locations := make([]WorldActorLocation, 0)
	controlGrants := make([]WorldActorControlGrant, 0)
	f7v6, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V6,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
			Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
			Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
			Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
			Locations: &locations, ControlGrants: &controlGrants,
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v6), `"locations":[]`)
	require.Contains(t, string(f7v6), `"control_grants":[]`)

	f7v7, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V7,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
			Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
			Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
			Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
			Locations: &locations, ControlGrants: &controlGrants,
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v7), `"locations":[]`)
	require.Contains(t, string(f7v7), `"control_grants":[]`)
	require.NotContains(t, string(f7v7), `"portal_states"`)

	portalStates := make([]WorldPortalState, 0)
	f7v8, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V8,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
			Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
			Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
			Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
			Locations: &locations, ControlGrants: &controlGrants, PortalStates: &portalStates,
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v8), `"portal_states":[]`)
	require.NotContains(t, string(f7v8), `"navigation_profile"`)

	navigationIntents := make([]WorldActorNavigationIntent, 0)
	f7v9, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V9,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
			Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
			Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
			Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
			Locations: &locations, ControlGrants: &controlGrants, PortalStates: &portalStates,
			NavigationProfile: &WorldNavigationProfile{
				ProfileVersion: worldNavigationProfileVersion, Revision: 1,
			},
			NavigationIntents: &navigationIntents,
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v9), `"navigation_profile"`)
	require.Contains(t, string(f7v9), `"navigation_intents":[]`)
	require.NotContains(t, string(f7v9), `"public_services"`)

	f8, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF8,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
			Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
			Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
			Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
			Locations: &locations, ControlGrants: &controlGrants, PortalStates: &portalStates,
			NavigationProfile: &WorldNavigationProfile{
				ProfileVersion: worldNavigationProfileVersion, Revision: 1,
			},
			NavigationIntents: &navigationIntents,
		},
		PublicServices: &cityPublicServiceHashState{
			ServiceDefinitions: make([]CityServiceDefinition, 0),
			FacilityTypes:      make([]CityFacilityTypeDefinition, 0),
			Facilities:         make([]CityFacility, 0), Capacities: make([]CityFacilityServiceCapacity, 0),
			Demands: make([]CityServiceDemand, 0), Connections: make([]CityServiceConnection, 0),
			Facts: make([]CityServiceFact, 0), Allocations: make([]CityServiceAllocation, 0),
			Settlements: make([]CityServiceSettlement, 0),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f8), `"public_services"`)

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
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V6,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime:       &worldRuntimeHashState{},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V7,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime:       &worldRuntimeHashState{},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V8,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Locations: &locations, ControlGrants: &controlGrants,
		},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V9,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Locations: &locations, ControlGrants: &controlGrants, PortalStates: &portalStates,
		},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF8,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Locations: &locations, ControlGrants: &controlGrants, PortalStates: &portalStates,
			NavigationProfile: &WorldNavigationProfile{}, NavigationIntents: &navigationIntents,
		},
	})
	require.Error(t, err)
}
