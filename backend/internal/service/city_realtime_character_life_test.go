package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterLifeReducerPinsInventoryAndLawChains(t *testing.T) {
	profile := cityRealtimeCharacterLifeTestProfile(t)
	definitions := make(map[string]cityRealtimeCharacterActivityDefinition)
	definitionsForVersion, supported := cityRealtimeCharacterActivityDefinitionsForVersion(cityRealtimeCharacterActivityCatalogV110)
	require.True(t, supported)
	for _, definition := range definitionsForVersion {
		definitions[definition.Code] = definition
	}

	worked, workEvent, workLaw, workInventory, workResult, err := cityRealtimeCharacterApplyActivity(
		profile, definitions["work.civic_shift"], 2, 0,
	)
	require.NoError(t, err)
	require.Nil(t, workLaw)
	require.NotNil(t, workInventory)
	require.Equal(t, cityRealtimeCharacterRationItemCode, workInventory.ItemCode)
	require.Equal(t, int64(3), workInventory.Quantity)
	require.Equal(t, int64(2), worked.Revision)
	require.Equal(t, int64(1), worked.ActivityRevision)
	require.Equal(t, int64(0), worked.LawRevision)
	require.Equal(t, int64(24), worked.CityCreditUnits)
	require.Equal(t, int64(810), worked.CivicStandingMilli)
	require.Equal(t, "work.civic_shift", workEvent.ActivityCode)
	require.Equal(t, "completed", workResult.Outcome)
	require.Equal(t, int64(1), workResult.ItemQuantityDelta)
	require.NotNil(t, workEvent.ItemCode)
	require.NoError(t, validateCityRealtimeCharacterActivityEventProjection(CityRealtimeCharacterActivityEvent{
		Sequence: workEvent.EventSequence, FrameSequence: workEvent.FrameSequence,
		ActivityCode: workEvent.ActivityCode, CategoryCode: workEvent.CategoryCode,
		Outcome: workEvent.Outcome, PublicVisibility: workEvent.PublicVisibility,
		EnergyDeltaMilli: workEvent.EnergyDeltaMilli, SatietyDeltaMilli: workEvent.SatietyDeltaMilli,
		MoraleDeltaMilli: workEvent.MoraleDeltaMilli, CivicStandingDeltaMilli: workEvent.CivicStandingDeltaMilli,
		CityCreditDeltaUnits: workEvent.CityCreditDeltaUnits, ItemCode: *workEvent.ItemCode,
		ItemQuantityDelta: workEvent.ItemQuantityDelta,
	}, definitions))

	conducted, conductEvent, conductLaw, conductInventory, conductResult, err := cityRealtimeCharacterApplyActivity(
		worked, definitions["conduct.disruption"], 3, cityRealtimeCharacterActivityMinimumIntervalUS,
	)
	require.NoError(t, err)
	require.Nil(t, conductInventory)
	require.NotNil(t, conductLaw)
	require.Equal(t, int64(3), conducted.Revision)
	require.Equal(t, int64(2), conducted.ActivityRevision)
	require.Equal(t, int64(1), conducted.LawRevision)
	require.Equal(t, int64(12), conducted.CityCreditUnits)
	require.Equal(t, int64(670), conducted.CivicStandingMilli)
	require.Equal(t, "penalized", conductEvent.Outcome)
	require.Equal(t, conductLaw.CaseCode, conductResult.LawCaseCode)
	require.Equal(t, conducted.ActivityEventChainHash, conductEvent.EventHash)
	require.Equal(t, conducted.LawEventChainHash, conductLaw.EventHash)
	require.True(t, cityRealtimeCharacterProfileValid(conducted))

	mutatedInventory := conducted
	mutatedInventory.Inventory = append([]cityRealtimeCharacterInventoryStack(nil), conducted.Inventory...)
	mutatedInventory.Inventory[0].StateHash = strings.Repeat("0", 64)
	mutatedInventoryHash, err := cityRealtimeCharacterProfileStateHash(mutatedInventory)
	require.NoError(t, err)
	require.NotEqual(t, conducted.StateHash, mutatedInventoryHash, "profile state must commit inventory state")
}

func TestCityRealtimeCharacterLifeConsumeAndPublicProjectionStayBounded(t *testing.T) {
	profile := cityRealtimeCharacterLifeTestProfile(t)
	definitions := make(map[string]cityRealtimeCharacterActivityDefinition)
	definitionsForVersion, supported := cityRealtimeCharacterActivityDefinitionsForVersion(cityRealtimeCharacterActivityCatalogV110)
	require.True(t, supported)
	for _, definition := range definitionsForVersion {
		definitions[definition.Code] = definition
	}

	next, event, law, stack, result, err := cityRealtimeCharacterApplyActivity(
		profile, definitions["consume.ration"], 2, 0,
	)
	require.NoError(t, err)
	require.Nil(t, law)
	require.NotNil(t, stack)
	require.Equal(t, int64(1), stack.Quantity)
	require.Equal(t, int64(2), stack.Revision)
	require.Equal(t, int64(-1), result.ItemQuantityDelta)
	require.Equal(t, int64(-1), event.ItemQuantityDelta)
	require.Equal(t, int64(1), next.Inventory[0].Quantity)
	require.True(t, cityRealtimeCharacterProfileValid(next))

	public := CityRealtimePublicCharacterEvent{
		FrameSequence: 2, ActorCode: profile.ActorCode, PublicLabel: "森川 凛",
		ActivityCode: "work.civic_shift", CategoryCode: "work", Outcome: "completed",
	}
	require.NoError(t, validateCityRealtimePublicCharacterEventProjection(public, definitions))
	raw, err := json.Marshal(public)
	require.NoError(t, err)
	for _, forbidden := range []string{"energy", "satiety", "morale", "credit", "inventory", "owner", "agent"} {
		require.NotContains(t, strings.ToLower(string(raw)), forbidden)
	}
}

func TestCityRealtimeCharacterMetabolismReducerIsVersionedAndBounded(t *testing.T) {
	profile, metabolism := cityRealtimeCharacterMetabolismTestProfile(t)
	next, err := cityRealtimeCharacterApplyMetabolism(
		profile, metabolism, 2, cityRealtimeCharacterMetabolismMinimumIntervalUS,
	)
	require.NoError(t, err)
	require.Equal(t, cityRealtimeCharacterProfileSchemaMetabolism, next.StateSchemaVersion)
	require.Equal(t, int64(2), next.Revision)
	require.Equal(t, int64(0), next.ActivityRevision)
	require.Equal(t, int64(0), next.LawRevision)
	require.Equal(t, int64(1), next.MetabolismRevision)
	require.Equal(t, cityRealtimeCharacterMetabolismMinimumIntervalUS, next.LastMetabolismWorldTimeUS)
	require.Equal(t, int64(754), next.EnergyMilli)
	require.Equal(t, int64(712), next.SatietyMilli)
	require.Equal(t, int64(638), next.MoraleMilli)
	require.Equal(t, profile.CivicStandingMilli, next.CivicStandingMilli)
	require.Equal(t, profile.CityCreditUnits, next.CityCreditUnits)
	require.Equal(t, profile.Inventory, next.Inventory)
	require.Equal(t, profile.ActivityEventChainHash, next.ActivityEventChainHash)
	require.Equal(t, profile.LawEventChainHash, next.LawEventChainHash)
	require.NotEqual(t, profile.StateHash, next.StateHash)
	require.True(t, cityRealtimeCharacterProfileValid(next))

	_, err = cityRealtimeCharacterApplyMetabolism(
		cityRealtimeCharacterLifeTestProfile(t), metabolism, 2, cityRealtimeCharacterMetabolismMinimumIntervalUS,
	)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func TestCityRealtimeCharacterMetabolismCatalogLeavesPriorVersionsUntouched(t *testing.T) {
	legacy, legacySupported := cityRealtimeCharacterActivityCatalogSpecForVersion(cityRealtimeCharacterActivityLegacyCatalogVersion)
	v110, v110Supported := cityRealtimeCharacterActivityCatalogSpecForVersion(cityRealtimeCharacterActivityCatalogV110)
	v120, v120Supported := cityRealtimeCharacterActivityCatalogSpecForVersion(cityRealtimeCharacterActivityCatalogV120)
	v130, v130Supported := cityRealtimeCharacterActivityCatalogSpecForVersion(cityRealtimeCharacterActivityCatalogVersion)
	require.True(t, legacySupported)
	require.True(t, v110Supported)
	require.True(t, v120Supported)
	require.True(t, v130Supported)
	require.Nil(t, legacy.Metabolism)
	require.Nil(t, v110.Metabolism)
	require.NotNil(t, v120.Metabolism)
	require.Equal(t, cityRealtimeCharacterMetabolismMinimumIntervalUS, v120.Metabolism.IntervalUS)
	require.NotNil(t, v130.Metabolism)
	require.NotNil(t, v130.Progression)
}

func TestCityRealtimeCharacterPublicEventCursorAndActorCodeValidation(t *testing.T) {
	actorCode := "character.player.0123456789abcdef0123456789abcdef"
	require.True(t, cityRealtimePlayerActorCodeValid(actorCode))
	require.False(t, cityRealtimePlayerActorCodeValid("character.player.0123456789ABCDEF0123456789abcdef"))
	require.False(t, cityRealtimePlayerActorCodeValid("character.player.not-a-random-handle"))

	cursor, err := parseCityRealtimePublicCharacterEventCursor("12|" + actorCode + "|3")
	require.NoError(t, err)
	require.Equal(t, int64(12), cursor.FrameSequence)
	require.Equal(t, actorCode, cursor.ActorCode)
	require.Equal(t, int64(3), cursor.EventSequence)
	require.Equal(t, "12|"+actorCode+"|3", cursor.String())
	_, err = parseCityRealtimePublicCharacterEventCursor("12|" + actorCode + "|0")
	require.ErrorIs(t, err, ErrCityInvalidInput)
	_, err = parseCityRealtimePublicCharacterEventCursor("malformed")
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func cityRealtimeCharacterLifeTestProfile(t *testing.T) cityRealtimeCharacterProfile {
	t.Helper()
	actorCode := "character.player.0123456789abcdef0123456789abcdef"
	activityGenesis, err := cityRealtimeCharacterActivityChainGenesisHash(actorCode, 1)
	require.NoError(t, err)
	lawGenesis, err := cityRealtimeCharacterLawChainGenesisHash(actorCode, 1)
	require.NoError(t, err)
	stack := cityRealtimeCharacterInventoryStack{
		ItemCode: cityRealtimeCharacterRationItemCode, Quantity: 2, Revision: 1, LastFrameSequence: 1,
	}
	stack.StateHash, err = cityRealtimeCharacterInventoryStateHash(actorCode, stack)
	require.NoError(t, err)
	profile := cityRealtimeCharacterProfile{
		StateSchemaVersion: cityRealtimeCharacterProfileSchemaLegacy,
		ActorCode:          actorCode, EnergyMilli: cityRealtimeCharacterInitialEnergyMilli,
		SatietyMilli: cityRealtimeCharacterInitialSatietyMilli, MoraleMilli: cityRealtimeCharacterInitialMoraleMilli,
		CivicStandingMilli: cityRealtimeCharacterInitialCivicStandingMilli,
		CityCreditUnits:    cityRealtimeCharacterInitialCityCreditUnits, Revision: 1,
		ActivityRevision: 0, LawRevision: 0, SpawnedFrameSequence: 1, LastFrameSequence: 1,
		LastActivityWorldTimeUS: 0, ActivityEventChainHash: activityGenesis, LawEventChainHash: lawGenesis,
		Inventory: []cityRealtimeCharacterInventoryStack{stack},
	}
	profile.StateHash, err = cityRealtimeCharacterProfileStateHash(profile)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterProfileValid(profile))
	return profile
}

func cityRealtimeCharacterMetabolismTestProfile(t *testing.T) (cityRealtimeCharacterProfile, cityRealtimeCharacterMetabolismDefinition) {
	t.Helper()
	profile := cityRealtimeCharacterLifeTestProfile(t)
	spec, supported := cityRealtimeCharacterActivityCatalogSpecForVersion(cityRealtimeCharacterActivityCatalogV120)
	require.True(t, supported)
	require.NotNil(t, spec.Metabolism)
	profile.StateSchemaVersion = cityRealtimeCharacterProfileSchemaMetabolism
	profile.MetabolismRevision = 0
	profile.LastMetabolismWorldTimeUS = 0
	stateHash, err := cityRealtimeCharacterProfileStateHash(profile)
	require.NoError(t, err)
	profile.StateHash = stateHash
	require.True(t, cityRealtimeCharacterProfileValid(profile))
	return profile, *spec.Metabolism
}
