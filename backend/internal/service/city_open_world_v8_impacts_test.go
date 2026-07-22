package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldImpactState(t *testing.T) *CityOpenWorldImpactState {
	t.Helper()
	catalog, catalogHash, err := builtInCityOpenWorldImpactCatalog()
	require.NoError(t, err)
	profileHash, err := cityOpenWorldImpactProfileHash(catalogHash)
	require.NoError(t, err)
	return &CityOpenWorldImpactState{
		Policy: CityOpenWorldImpactPolicy{
			ProfileID:               cityOpenWorldImpactProfileID,
			ProfileVersion:          cityOpenWorldImpactProfileVersion,
			ContentHash:             profileHash,
			BaselineTick:            0,
			SourceContractVersion:   cityOpenWorldImpactSourceContractVersion,
			DeliveryContractVersion: cityOpenWorldImpactDeliveryContractVersion,
			MaximumSchedulesPerTick: cityOpenWorldImpactMaximumSchedulesPerTick,
			Revision:                1,
			Metadata:                json.RawMessage(`{}`),
		},
		Catalog: catalog,
		Effects: []CityOpenWorldImpactEffect{},
		Metrics: []CityOpenWorldImpactMetric{},
	}
}

func TestCityOpenWorldV8ImpactCatalogIsDeterministicAndSealed(t *testing.T) {
	first, firstHash, err := builtInCityOpenWorldImpactCatalog()
	require.NoError(t, err)
	second, secondHash, err := builtInCityOpenWorldImpactCatalog()
	require.NoError(t, err)
	require.Equal(t, firstHash, secondHash)
	require.Equal(t, first, second)
	require.Len(t, first, 8)
	for _, entry := range first {
		require.Equal(t, cityOpenWorldImpactSourceKindService, entry.SourceKind)
		require.Equal(t, cityOpenWorldImpactTargetDomainActor, entry.TargetDomain)
		require.NotZero(t, entry.DeltaUnitsPerSourceUnit)
		require.True(t, cityWorldVersionHashValid(entry.ContentHash))
	}
}

func TestCityOpenWorldV8ImpactStateRequiresStrictNextTickApplication(t *testing.T) {
	state := newValidCityOpenWorldImpactState(t)
	entry := state.Catalog[0]
	effect := CityOpenWorldImpactEffect{
		SourceResponseCode: "service.response.17",
		CatalogCode:        entry.Code,
		TargetDomain:       cityOpenWorldImpactTargetDomainActor,
		TargetCode:         "actor.demo",
		MetricCode:         entry.MetricCode,
		SourceUnits:        2,
		DeltaUnits:         2 * entry.DeltaUnitsPerSourceUnit,
		ScheduledTick:      4,
		EffectiveTick:      5,
		Status:             "scheduled",
		Version:            1,
		SourceFact:         CityOpenWorldRuntimeFactRef{Tick: 4, Sequence: 3},
		Metadata:           json.RawMessage(`{}`),
	}
	effect.Code = cityOpenWorldImpactEffectCode(effect.SourceResponseCode, effect.CatalogCode)
	state.Effects = []CityOpenWorldImpactEffect{effect}
	state.Policy.EffectCount = 1
	require.NoError(t, validateCityOpenWorldImpactState(state))

	state.Effects[0].EffectiveTick = state.Effects[0].ScheduledTick
	require.Error(t, validateCityOpenWorldImpactState(state))
	state.Effects[0].EffectiveTick = 5
	state.Effects[0].ScheduledTick = state.Policy.BaselineTick
	require.Error(t, validateCityOpenWorldImpactState(state))
}

func TestCityOpenWorldV8ImpactStateBindsMetricsToAppliedEffects(t *testing.T) {
	state := newValidCityOpenWorldImpactState(t)
	entry := state.Catalog[0]
	appliedTick := int64(5)
	before := int64(0)
	after := entry.DeltaUnitsPerSourceUnit
	effect := CityOpenWorldImpactEffect{
		SourceResponseCode: "service.response.31",
		CatalogCode:        entry.Code,
		TargetDomain:       cityOpenWorldImpactTargetDomainActor,
		TargetCode:         "actor.demo",
		MetricCode:         entry.MetricCode,
		SourceUnits:        1,
		DeltaUnits:         entry.DeltaUnitsPerSourceUnit,
		ScheduledTick:      4,
		EffectiveTick:      5,
		Status:             "applied",
		AppliedTick:        &appliedTick,
		ApplicationFact:    &CityOpenWorldRuntimeFactRef{Tick: 5, Sequence: 7},
		BeforeUnits:        &before,
		AfterUnits:         &after,
		Version:            2,
		SourceFact:         CityOpenWorldRuntimeFactRef{Tick: 4, Sequence: 3},
		Metadata:           json.RawMessage(`{}`),
	}
	effect.Code = cityOpenWorldImpactEffectCode(effect.SourceResponseCode, effect.CatalogCode)
	state.Effects = []CityOpenWorldImpactEffect{effect}
	state.Metrics = []CityOpenWorldImpactMetric{{
		TargetDomain:    cityOpenWorldImpactTargetDomainActor,
		TargetCode:      effect.TargetCode,
		MetricCode:      effect.MetricCode,
		ValueUnits:      after,
		LastAppliedTick: appliedTick,
		LastEffectCode:  effect.Code,
		Version:         1,
		Metadata:        json.RawMessage(`{}`),
	}}
	state.Policy.EffectCount = 1
	state.Policy.AppliedCount = 1
	state.Policy.MetricCount = 1
	require.NoError(t, validateCityOpenWorldImpactState(state))

	state.Metrics[0].LastEffectCode = "impact.unknown"
	require.Error(t, validateCityOpenWorldImpactState(state))
}
