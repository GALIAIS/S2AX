package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterProgressionCatalogRejectsAmbiguousReferences(t *testing.T) {
	base := cityRealtimeCharacterProgressionCatalogDefinition()
	require.True(t, cityRealtimeCharacterProgressionDefinitionValid(*base))

	duplicateArchetype := *base
	duplicateArchetype.Archetypes = append([]cityRealtimeCharacterArchetypeDefinition(nil), base.Archetypes...)
	duplicateArchetype.Archetypes = append(duplicateArchetype.Archetypes, base.Archetypes[0])
	require.False(t, cityRealtimeCharacterProgressionDefinitionValid(duplicateArchetype))

	missingRoleRequirement := *base
	missingRoleRequirement.Roles = append([]cityRealtimeCharacterRoleDefinition(nil), base.Roles...)
	missingRoleRequirement.Roles[0].Requirements.RequiredRoleCodes = []string{"profession.missing"}
	require.False(t, cityRealtimeCharacterProgressionDefinitionValid(missingRoleRequirement))

	invalidActivityRule := &cityRealtimeCharacterActivityProgressionDefinition{
		RequiredRoleCodes: []string{"profession.missing"},
	}
	require.False(t, cityRealtimeCharacterActivityProgressionDefinitionValid(invalidActivityRule, base))
}

func TestCityRealtimeCharacterProgressionSealsExperienceAndRoleTransitions(t *testing.T) {
	runtime := cityRealtimeCharacterProgressionTestRuntime(t)
	profile := cityRealtimeCharacterProgressionTestProfile(t, runtime, "resident.social")

	_, _, _, _, _, err := cityRealtimeCharacterApplyRoleChange(
		profile, runtime, "profession.civic_aide", profile.LastFrameSequence+1,
	)
	require.ErrorIs(t, err, ErrCityRealtimeCharacterRoleUnavailable)

	work := runtime.Definitions["work.civic_shift"]
	transition, err := cityRealtimeCharacterApplyActivityWithRuntime(
		profile, runtime, work, profile.LastFrameSequence+1, cityRealtimeCharacterActivityMinimumIntervalUS,
	)
	require.NoError(t, err)
	require.NotNil(t, transition.ProgressionEvent)
	require.Equal(t, int64(1), transition.Profile.ProgressionRevision)
	require.Equal(t, []CityRealtimeCharacterExperienceDelta{
		{AttributeCode: "communication", ExperienceUnits: 12},
		{AttributeCode: "discipline", ExperienceUnits: 24},
	}, transition.Result.ExperienceDeltas)
	require.True(t, cityRealtimeCharacterProfileMatchesRuntime(transition.Profile, runtime))

	matured, updates, deltas, err := cityRealtimeCharacterApplyExperienceRewards(
		transition.Profile,
		runtime,
		[]CityRealtimeCharacterExperienceDelta{{AttributeCode: "discipline", ExperienceUnits: 255}},
		transition.Profile.LastFrameSequence+1,
	)
	require.NoError(t, err)
	require.Len(t, updates, 1)
	require.Equal(t, []CityRealtimeCharacterExperienceDelta{{AttributeCode: "discipline", ExperienceUnits: 255}}, deltas)
	matured.Revision++
	matured.ProgressionRevision++
	matured.LastFrameSequence++
	matured.CivicStandingMilli = 820
	activitySequence := transition.Activity.EventSequence + 1
	progressionEvent := cityRealtimeCharacterProgressionEventRecord{
		ActorCode:             matured.ActorCode,
		EventSequence:         matured.ProgressionRevision,
		FrameSequence:         matured.LastFrameSequence,
		EventKind:             "activity",
		ActivityEventSequence: &activitySequence,
		ExperienceDeltas:      deltas,
		PreviousEventHash:     transition.Profile.ProgressionEventChainHash,
	}
	progressionEvent.EventHash, err = cityRealtimeCharacterProgressionEventHash(progressionEvent)
	require.NoError(t, err)
	matured.ProgressionEventChainHash = progressionEvent.EventHash
	matured.ProgressionStateHash, err = cityRealtimeCharacterProgressionStateHash(matured)
	require.NoError(t, err)
	matured.StateHash, err = cityRealtimeCharacterProfileStateHash(matured)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterProfileMatchesRuntime(matured, runtime))

	next, previous, assignment, roleEvent, roleChange, err := cityRealtimeCharacterApplyRoleChange(
		matured, runtime, "profession.civic_aide", matured.LastFrameSequence+1,
	)
	require.NoError(t, err)
	require.NotNil(t, previous)
	require.NotNil(t, assignment)
	require.Equal(t, "profession.resident", previous.RoleCode)
	require.Equal(t, "profession.civic_aide", assignment.RoleCode)
	require.Equal(t, "profession", roleChange.CategoryCode)
	require.Equal(t, "profession.resident", roleChange.FromRoleCode)
	require.Equal(t, "profession.civic_aide", roleChange.ToRoleCode)
	require.Equal(t, "role", roleEvent.EventKind)
	require.Equal(t, int64(3), next.ProgressionRevision)
	require.True(t, cityRealtimeCharacterProfileMatchesRuntime(next, runtime))
}

func cityRealtimeCharacterProgressionTestRuntime(t *testing.T) *cityRealtimeCharacterLifeRuntime {
	t.Helper()
	spec, supported := cityRealtimeCharacterActivityCatalogSpecForVersion(cityRealtimeCharacterActivityCatalogVersion)
	require.True(t, supported)
	require.NotNil(t, spec.Metabolism)
	require.NotNil(t, spec.Progression)
	definitions := make(map[string]cityRealtimeCharacterActivityDefinition, len(spec.Definitions))
	for _, definition := range spec.Definitions {
		definitions[definition.Code] = definition
	}
	return &cityRealtimeCharacterLifeRuntime{
		Definitions: definitions,
		Metabolism:  spec.Metabolism,
		Progression: spec.Progression,
	}
}

func cityRealtimeCharacterProgressionTestProfile(
	t *testing.T,
	runtime *cityRealtimeCharacterLifeRuntime,
	archetypeCode string,
) cityRealtimeCharacterProfile {
	t.Helper()
	profile := cityRealtimeCharacterLifeTestProfile(t)
	archetype, err := cityRealtimeCharacterResolveArchetype(runtime, archetypeCode)
	require.NoError(t, err)
	require.NoError(t, cityRealtimeCharacterSeedProgression(&profile, runtime, archetype, profile.SpawnedFrameSequence))
	profile.StateHash, err = cityRealtimeCharacterProfileStateHash(profile)
	require.NoError(t, err)
	require.True(t, cityRealtimeCharacterProfileMatchesRuntime(profile, runtime))
	return profile
}
