package service

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltInWorldRuntimeCatalogIsDeterministicAndReferenceComplete(t *testing.T) {
	first, firstHash, err := builtInWorldRuntimeDefinitions()
	require.NoError(t, err)
	second, secondHash, err := builtInWorldRuntimeDefinitions()
	require.NoError(t, err)
	require.Equal(t, firstHash, secondHash)
	require.Len(t, firstHash, 64)
	require.Equal(t, first, second)
	require.GreaterOrEqual(t, len(first), 15)
	require.NoError(t, validateWorldRuntimeCatalog(first))

	definitions := make(map[string]WorldRuntimeDefinition, len(first))
	for _, definition := range first {
		require.NoError(t, validateWorldRuntimeDefinition(definition))
		definitions[definition.Kind+"/"+definition.Code] = definition
	}
	for _, definition := range first {
		switch definition.Kind {
		case WorldRuntimeDefinitionArchetype:
			value, decodeErr := decodeWorldRuntimeDefinition[worldRuntimeArchetypeDefinition](&definition)
			require.NoError(t, decodeErr)
			require.Contains(t, definitions, WorldRuntimeDefinitionActorType+"/"+value.ActorTypeCode)
			for code := range value.InitialAttributes {
				require.Contains(t, definitions, WorldRuntimeDefinitionAttribute+"/"+code)
			}
			for _, code := range value.InitialRoles {
				require.Contains(t, definitions, WorldRuntimeDefinitionRole+"/"+code)
			}
		case WorldRuntimeDefinitionActivity:
			value, decodeErr := decodeWorldRuntimeDefinition[worldRuntimeActivityDefinition](&definition)
			require.NoError(t, decodeErr)
			for _, effect := range value.Effects {
				if effect.Type == WorldRuntimeEffectAttributeAdd || effect.Type == WorldRuntimeEffectAttributeSet || effect.Type == WorldRuntimeEffectExperienceAdd {
					require.Contains(t, definitions, WorldRuntimeDefinitionAttribute+"/"+effect.Key)
				}
			}
		case WorldRuntimeDefinitionRule:
			value, decodeErr := decodeWorldRuntimeDefinition[worldRuntimeRuleDefinition](&definition)
			require.NoError(t, decodeErr)
			for _, tier := range value.Tiers {
				for _, effect := range tier.Effects {
					require.Contains(t, definitions, WorldRuntimeDefinitionStatus+"/"+effect.Key)
				}
			}
		}
	}
}

func TestWorldRuntimeCatalogRejectsMissingAndDuplicateReferences(t *testing.T) {
	definitions, _, err := builtInWorldRuntimeDefinitions()
	require.NoError(t, err)

	missingAttribute := make([]WorldRuntimeDefinition, 0, len(definitions)-1)
	for _, definition := range definitions {
		if definition.Kind != WorldRuntimeDefinitionAttribute || definition.Code != "reasoning" {
			missingAttribute = append(missingAttribute, definition)
		}
	}
	require.ErrorContains(t, validateWorldRuntimeCatalog(missingAttribute), "missing referenced definition")

	duplicate := append(append([]WorldRuntimeDefinition(nil), definitions...), definitions[0])
	require.ErrorContains(t, validateWorldRuntimeCatalog(duplicate), "duplicate definition")
}

func TestWorldRuntimeCommandNormalizationRejectsClientDerivedState(t *testing.T) {
	value, handled, err := normalizeWorldRuntimeCommand(
		CityCommandTypeActorCreate,
		json.RawMessage(`{"archetype_code":" Urban_Apprentice ","name":" 角色 "}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, worldActorCreatePayload{ArchetypeCode: "urban_apprentice", Name: "角色"}, value)

	_, handled, err = normalizeWorldRuntimeCommand(
		CityCommandTypeActorActivityPerform,
		json.RawMessage(`{"actor_code":"actor_00000001","activity_code":"technical_study","value_units":999999}`),
	)
	require.True(t, handled)
	require.Error(t, err)

	_, handled, err = normalizeWorldRuntimeCommand("actor.attribute.set", json.RawMessage(`{}`))
	require.False(t, handled)
	require.NoError(t, err)
}

func TestWorldRequirementValidationEnforcesBoundedDeclarativeAST(t *testing.T) {
	require.NoError(t, validateWorldRequirement(WorldRequirementNode{
		Operator: WorldRequirementAll,
		Items: []WorldRequirementNode{
			{Operator: WorldRequirementAttributeGTE, AttributeCode: "reasoning", ValueUnits: 60000},
			{Operator: WorldRequirementRoleActive, RoleCode: "profession.apprentice"},
		},
	}))
	require.Error(t, validateWorldRequirement(WorldRequirementNode{Operator: "javascript", FactType: "x"}))
	require.Error(t, validateWorldRequirement(WorldRequirementNode{
		Operator: WorldRequirementAny,
		Items:    make([]WorldRequirementNode, worldRequirementMaximumItems+1),
	}))
}

func TestWorldRuntimeSaturatingIntegerArithmetic(t *testing.T) {
	require.Equal(t, int64(math.MaxInt64), saturatingWorldRuntimeAdd(math.MaxInt64-1, 10))
	require.Equal(t, int64(math.MinInt64), saturatingWorldRuntimeAdd(math.MinInt64+1, -10))
	require.Equal(t, int64(7), saturatingWorldRuntimeAdd(3, 4))
}

func TestReplayWorldRuntimeEffectRejectsBeforeValueDrift(t *testing.T) {
	before := int64(10)
	delta := int64(5)
	after := int64(15)
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"attribute_after": WorldActorAttribute{
			ActorCode: "actor_00000001", AttributeCode: "reasoning", ValueUnits: 15,
			Metadata: json.RawMessage(`{}`),
		},
	})
	require.NoError(t, err)
	runtime := &worldRuntimeHashState{
		Actors: []WorldActor{{Code: "actor_00000001"}},
		Attributes: []WorldActorAttribute{{
			ActorCode: "actor_00000001", AttributeCode: "reasoning", ValueUnits: 9,
		}},
	}
	err = replayWorldRuntimeEffect(runtime, WorldEffectOperation{
		EffectType:      WorldRuntimeEffectAttributeAdd,
		TargetActorCode: stringPointer("actor_00000001"), TargetKey: stringPointer("reasoning"),
		BeforeUnits: &before, DeltaUnits: &delta, AfterUnits: &after, Payload: payload,
	})
	require.ErrorContains(t, err, "before value mismatch")
}
