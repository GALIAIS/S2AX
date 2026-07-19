package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV5ScenarioBindingsAreDeterministicAndProfileLed(t *testing.T) {
	japanFirst, err := cityOpenWorldV5ScenarioForProfile(cityspatial.WorldgenProfileJapanMetropolitan)
	require.NoError(t, err)
	japanSecond, err := cityOpenWorldV5ScenarioForProfile(cityspatial.WorldgenProfileJapanMetropolitan)
	require.NoError(t, err)
	china, err := cityOpenWorldV5ScenarioForProfile(cityspatial.WorldgenProfileChinaMetropolitan)
	require.NoError(t, err)

	require.Equal(t, japanFirst, japanSecond)
	require.Equal(t, "scenario.jp.metropolitan", japanFirst.ID)
	require.Equal(t, "scenario.cn.metropolitan", china.ID)
	require.NotEqual(t, japanFirst.ScenarioHash, china.ScenarioHash)
	require.Equal(t, 14, japanFirst.NPCBudget)
	require.Equal(t, 16, china.NPCBudget)

	jpRule, found := cityOpenWorldV5FacilityRuleForBuilding(
		japanFirst, cityspatial.LandUseCommercial, "arcade",
	)
	require.True(t, found)
	require.Equal(t, int64(9), jpRule.CapacityPerFloor)
	cnRule, found := cityOpenWorldV5FacilityRuleForBuilding(
		china, cityspatial.LandUseCommercial, "tower",
	)
	require.True(t, found)
	require.Equal(t, int64(12), cnRule.CapacityPerFloor)

	_, err = cityOpenWorldV5ScenarioForProfile("unsupported.profile")
	require.Error(t, err)
}

func TestCityOpenWorldV5CatalogContainsSocialDefinitionsWithoutMutatingV4(t *testing.T) {
	v4, v4Hash, err := builtInCityOpenWorldRuntimeDefinitionsForVersion(CitySimulationVersionOpenWorldV4)
	require.NoError(t, err)
	v5, v5Hash, err := builtInCityOpenWorldRuntimeDefinitionsForVersion(CitySimulationVersionOpenWorldV5)
	require.NoError(t, err)
	require.NotEmpty(t, v4Hash)
	require.NotEmpty(t, v5Hash)
	require.NotEqual(t, v4Hash, v5Hash)
	require.Greater(t, len(v5), len(v4))

	v4Codes := make(map[string]struct{}, len(v4))
	for _, definition := range v4 {
		v4Codes[definition.Code] = struct{}{}
	}
	_, v4HasNPCResident := v4Codes["npc.resident"]
	require.False(t, v4HasNPCResident)

	v5Codes := make(map[string]struct{}, len(v5))
	for _, definition := range v5 {
		v5Codes[definition.Code] = struct{}{}
	}
	for _, code := range []string{"npc", "npc.resident", "npc.service_worker", "employment.service"} {
		_, found := v5Codes[code]
		require.True(t, found, code)
	}
}
