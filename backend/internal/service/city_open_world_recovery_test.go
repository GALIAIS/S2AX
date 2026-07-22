package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func TestOpenWorldV23RetainsV3WorldgenAndSocialRuntimeContracts(t *testing.T) {
	profile, err := cityspatial.WorldgenProfileByID("jp.metropolitan")
	require.NoError(t, err)

	binding, _, err := cityOpenWorldRegionBinding(CitySimulationVersionOpenWorldV23, 23_230_023, profile)
	require.NoError(t, err)
	require.NotEmpty(t, binding.GeneratorID)
	require.NotEmpty(t, binding.GeneratorVersion)

	plan, err := cityspatial.GenerateWorldgenPlan(binding, profile, cityOpenWorldRegionBounds(0, 0))
	require.NoError(t, err)
	surface, err := cityOpenWorldSurfaceForVersion(
		CitySimulationVersionOpenWorldV23,
		plan,
		cityOpenWorldSectorBounds(0, 0),
	)
	require.NoError(t, err)
	require.NotEmpty(t, surface.ContentHash)

	runtimeID, runtimeVersion, catalogVersion, err := cityOpenWorldRuntimeProfileIdentity(CitySimulationVersionOpenWorldV23)
	require.NoError(t, err)
	require.Equal(t, cityOpenWorldSocialRuntimeID, runtimeID)
	require.Equal(t, cityOpenWorldSocialRuntimeVersion, runtimeVersion)
	require.Equal(t, cityOpenWorldSocialRuntimeCatalogVersion, catalogVersion)
}

func TestValidateCityOpenWorldRuntimeRecoveryStateAcceptsV7Contract(t *testing.T) {
	runtimeID, runtimeVersion, catalogVersion, err := cityOpenWorldRuntimeProfileIdentity(CitySimulationVersionOpenWorldV7)
	require.NoError(t, err)
	state := &cityOpenWorldRuntimeHashState{
		Profile: CityOpenWorldRuntimeProfile{
			RuntimeID: runtimeID, RuntimeVersion: runtimeVersion, CatalogVersion: catalogVersion,
			CatalogHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Revision:    1, Metadata: json.RawMessage(`{}`),
		},
		Definitions:   []CityOpenWorldRuntimeDefinition{},
		Actors:        []CityOpenWorldActor{},
		Attributes:    []CityOpenWorldActorAttribute{},
		Roles:         []CityOpenWorldActorRole{},
		Statuses:      []CityOpenWorldActorStatus{},
		Locations:     []CityOpenWorldActorLocation{},
		ControlGrants: []CityOpenWorldActorControlGrant{},
		PortalStates:  []CityOpenWorldPortalState{},
		Facts:         []CityOpenWorldRuntimeFact{},
		Effects:       []CityOpenWorldRuntimeEffect{},
		RuleCases:     []CityOpenWorldRuleCase{},
		Social: &CityOpenWorldSocialRuntimeState{
			Facilities:        []CityOpenWorldFacility{},
			NPCProfiles:       []CityOpenWorldNPCProfile{},
			NavigationIntents: []CityOpenWorldNavigationIntent{},
		},
		Services: &CityOpenWorldServiceState{
			Catalog:   []CityOpenWorldServiceCatalogEntry{},
			Providers: []CityOpenWorldServiceProvider{},
			Requests:  []CityOpenWorldServiceRequest{},
			Responses: []CityOpenWorldServiceResponse{},
		},
	}
	require.NoError(t, validateCityOpenWorldRuntimeRecoveryState(CitySimulationVersionOpenWorldV7, state))
}

func TestValidateCityOpenWorldRuntimeRecoveryStateAcceptsV8ImpactContract(t *testing.T) {
	runtimeID, runtimeVersion, catalogVersion, err := cityOpenWorldRuntimeProfileIdentity(CitySimulationVersionOpenWorldV8)
	require.NoError(t, err)
	state := &cityOpenWorldRuntimeHashState{
		Profile: CityOpenWorldRuntimeProfile{
			RuntimeID: runtimeID, RuntimeVersion: runtimeVersion, CatalogVersion: catalogVersion,
			CatalogHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Revision:    1, Metadata: json.RawMessage(`{}`),
		},
		Definitions:   []CityOpenWorldRuntimeDefinition{},
		Actors:        []CityOpenWorldActor{},
		Attributes:    []CityOpenWorldActorAttribute{},
		Roles:         []CityOpenWorldActorRole{},
		Statuses:      []CityOpenWorldActorStatus{},
		Locations:     []CityOpenWorldActorLocation{},
		ControlGrants: []CityOpenWorldActorControlGrant{},
		PortalStates:  []CityOpenWorldPortalState{},
		Facts:         []CityOpenWorldRuntimeFact{},
		Effects:       []CityOpenWorldRuntimeEffect{},
		RuleCases:     []CityOpenWorldRuleCase{},
		Social: &CityOpenWorldSocialRuntimeState{
			Facilities:        []CityOpenWorldFacility{},
			NPCProfiles:       []CityOpenWorldNPCProfile{},
			NavigationIntents: []CityOpenWorldNavigationIntent{},
		},
		Services: &CityOpenWorldServiceState{
			Catalog:   []CityOpenWorldServiceCatalogEntry{},
			Providers: []CityOpenWorldServiceProvider{},
			Requests:  []CityOpenWorldServiceRequest{},
			Responses: []CityOpenWorldServiceResponse{},
		},
		Impacts: newValidCityOpenWorldImpactState(t),
	}
	require.NoError(t, validateCityOpenWorldRuntimeRecoveryState(CitySimulationVersionOpenWorldV8, state))
	state.Impacts = nil
	require.Error(t, validateCityOpenWorldRuntimeRecoveryState(CitySimulationVersionOpenWorldV8, state))
}

func TestValidateCityOpenWorldRuntimeRecoveryStateRejectsVersionShapeMismatches(t *testing.T) {
	v6ID, v6Version, v6CatalogVersion, err := cityOpenWorldRuntimeProfileIdentity(CitySimulationVersionOpenWorldV6)
	require.NoError(t, err)
	v6 := &cityOpenWorldRuntimeHashState{
		Profile: CityOpenWorldRuntimeProfile{
			RuntimeID: v6ID, RuntimeVersion: v6Version, CatalogVersion: v6CatalogVersion,
			CatalogHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Revision:    1, Metadata: json.RawMessage(`{}`),
		},
		Social:   &CityOpenWorldSocialRuntimeState{},
		Services: &CityOpenWorldServiceState{},
	}
	require.Error(t, validateCityOpenWorldRuntimeRecoveryState(CitySimulationVersionOpenWorldV6, v6))

	v7ID, v7Version, v7CatalogVersion, err := cityOpenWorldRuntimeProfileIdentity(CitySimulationVersionOpenWorldV7)
	require.NoError(t, err)
	v7 := &cityOpenWorldRuntimeHashState{
		Profile: CityOpenWorldRuntimeProfile{
			RuntimeID: v7ID, RuntimeVersion: v7Version, CatalogVersion: v7CatalogVersion,
			CatalogHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Revision:    1, Metadata: json.RawMessage(`{}`),
		},
		Social: &CityOpenWorldSocialRuntimeState{},
	}
	require.Error(t, validateCityOpenWorldRuntimeRecoveryState(CitySimulationVersionOpenWorldV7, v7))

	v7.Services = &CityOpenWorldServiceState{}
	v7.Impacts = newValidCityOpenWorldImpactState(t)
	require.Error(t, validateCityOpenWorldRuntimeRecoveryState(CitySimulationVersionOpenWorldV7, v7))
}

func TestCityOpenWorldRecoveryFactReferencesAreExplicit(t *testing.T) {
	factIDs := map[cityOpenWorldRuntimeRecoveryIdentity]int64{
		{tick: 4, sequence: 2}: 91,
	}
	require.Equal(t, int64(91), mustCityOpenWorldRecoveryFactID(t, factIDs, CityOpenWorldRuntimeFactRef{Tick: 4, Sequence: 2}))
	_, err := requireCityOpenWorldRecoveryFactID(factIDs, CityOpenWorldRuntimeFactRef{Tick: 4, Sequence: 3})
	require.Error(t, err)
}

func mustCityOpenWorldRecoveryFactID(
	t *testing.T,
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
	reference CityOpenWorldRuntimeFactRef,
) int64 {
	t.Helper()
	id, err := requireCityOpenWorldRecoveryFactID(factIDs, reference)
	require.NoError(t, err)
	return id
}
