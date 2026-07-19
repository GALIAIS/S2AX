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

func TestWorldActorSpatialControlCommandNormalizationIsStrictAndCanonical(t *testing.T) {
	move, handled, err := normalizeWorldRuntimeCommand(
		CityCommandTypeActorLocationMove,
		json.RawMessage(`{"actor_code":" Actor_00000001 ","x":-1,"y":32,"z":1,"anchor_kind":" Building ","anchor_code":" Building_Central "}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, worldActorLocationMovePayload{
		ActorCode: "actor_00000001", X: -1, Y: 32, Z: 1,
		AnchorKind: "building", AnchorCode: "building_central",
	}, move)

	control, handled, err := normalizeWorldRuntimeCommand(
		CityCommandTypeActorControlGrant,
		json.RawMessage(`{"actor_code":"actor_00000001","user_id":22,"capabilities":[" actor.control.manage ","actor.command"]}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, worldActorControlPayload{
		ActorCode: "actor_00000001", UserID: 22,
		Capabilities: []string{WorldActorCapabilityCommand, WorldActorCapabilityManageControl},
	}, control)

	_, _, err = normalizeWorldRuntimeCommand(
		CityCommandTypeActorControlRevoke,
		json.RawMessage(`{"actor_code":"actor_00000001","user_id":22,"capabilities":["actor.command","actor.command"]}`),
	)
	require.Error(t, err)
	_, _, err = normalizeWorldRuntimeCommand(
		CityCommandTypeActorLocationMove,
		json.RawMessage(`{"actor_code":"actor_00000001","x":1,"y":2,"z":0,"anchor_kind":"building"}`),
	)
	require.Error(t, err)
}

func TestWorldPortalAccessCommandsAreStrictCanonicalAndVersioned(t *testing.T) {
	transition, handled, err := normalizeWorldRuntimeCommand(
		CityCommandTypePortalStateTransition,
		json.RawMessage(`{"actor_code":" Actor_00000001 ","building_code":" Building_Hall ","portal_code":" Entrance_Main ","action":" CLOSE "}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, worldPortalStateTransitionPayload{
		ActorCode: "actor_00000001", BuildingCode: "building_hall",
		PortalCode: "entrance_main", Action: WorldPortalActionClose,
	}, transition)

	policy, handled, err := normalizeWorldRuntimeCommand(
		CityCommandTypePortalAccessConfigure,
		json.RawMessage(`{"building_code":" Building_Hall ","portal_code":" Entrance_Main ","requirements":{"op":" ROLE_ACTIVE ","role_code":" Profession.Technician "}}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, worldPortalAccessConfigurePayload{
		BuildingCode: "building_hall", PortalCode: "entrance_main",
		Requirements: WorldRequirementNode{
			Operator: WorldRequirementRoleActive, RoleCode: "profession.technician",
		},
	}, policy)

	_, _, err = normalizeWorldRuntimeCommand(
		CityCommandTypePortalStateTransition,
		json.RawMessage(`{"actor_code":"actor_00000001","building_code":"building_hall","portal_code":"entrance_main","action":"force"}`),
	)
	require.Error(t, err)
	_, _, err = normalizeWorldRuntimeCommand(
		CityCommandTypePortalAccessConfigure,
		json.RawMessage(`{"building_code":"building_hall","portal_code":"entrance_main","requirements":{"op":"javascript"}}`),
	)
	require.Error(t, err)

	public, firstRaw, firstHash, err := canonicalWorldPortalAccessRequirement(
		publicWorldPortalAccessRequirement(),
	)
	require.NoError(t, err)
	_, secondRaw, secondHash, err := canonicalWorldPortalAccessRequirement(public)
	require.NoError(t, err)
	require.Equal(t, string(firstRaw), string(secondRaw))
	require.Equal(t, firstHash, secondHash)
	require.Len(t, firstHash, 64)

	require.Equal(t, WorldPortalStateClosed, mustNextWorldPortalState(t, WorldPortalStateOpen, WorldPortalActionClose))
	require.Equal(t, WorldPortalStateLocked, mustNextWorldPortalState(t, WorldPortalStateClosed, WorldPortalActionLock))
	require.Equal(t, WorldPortalStateClosed, mustNextWorldPortalState(t, WorldPortalStateLocked, WorldPortalActionUnlock))
	require.Equal(t, WorldPortalStateOpen, mustNextWorldPortalState(t, WorldPortalStateClosed, WorldPortalActionOpen))
	_, valid := nextWorldPortalState(WorldPortalStateOpen, WorldPortalActionLock)
	require.False(t, valid)
}

func mustNextWorldPortalState(t *testing.T, current, action string) string {
	t.Helper()
	next, valid := nextWorldPortalState(current, action)
	require.True(t, valid)
	return next
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

func TestReplayWorldActorSpatialControlEffectsPreservesLifecycle(t *testing.T) {
	locations := make([]WorldActorLocation, 0)
	grants := make([]WorldActorControlGrant, 0)
	runtime := &worldRuntimeHashState{
		Actors:        []WorldActor{{Code: "actor_00000001"}},
		Locations:     &locations,
		ControlGrants: &grants,
	}
	location := WorldActorLocation{
		ActorCode: "actor_00000001", SpaceKind: "world", SpaceCode: "world",
		X: 10, Y: 20, Z: 0, ChunkX: 0, ChunkY: 0, LocalX: 10, LocalY: 20,
		JurisdictionCode: "central", MovedTick: 1, Version: 1, Metadata: json.RawMessage(`{}`),
	}
	locationPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "location_after": location,
	})
	require.NoError(t, err)
	zero, one := int64(0), int64(1)
	actorCode, position := "actor_00000001", "position"
	require.NoError(t, replayWorldRuntimeEffect(runtime, WorldEffectOperation{
		EffectType: WorldRuntimeEffectLocationSet, ExecutorVersion: worldRuntimeSpatialControlVersion,
		TargetActorCode: &actorCode, TargetKey: &position,
		BeforeUnits: &zero, DeltaUnits: &one, AfterUnits: &one, Payload: locationPayload,
	}))
	require.Equal(t, []WorldActorLocation{location}, locations)

	grant := WorldActorControlGrant{
		Code: "grant_actor_00000001_22_actor_command_1", ActorCode: actorCode,
		UserID: 22, Capability: WorldActorCapabilityCommand, Status: "active",
		GrantedByUserID: 9, GrantedTick: 1, Version: 1, Metadata: json.RawMessage(`{}`),
	}
	grantPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "control_grant_after": grant,
	})
	require.NoError(t, err)
	capability := WorldActorCapabilityCommand
	require.NoError(t, replayWorldRuntimeEffect(runtime, WorldEffectOperation{
		EffectType: WorldRuntimeEffectControlGrant, ExecutorVersion: worldRuntimeSpatialControlVersion,
		TargetActorCode: &actorCode, TargetKey: &capability,
		BeforeUnits: &zero, DeltaUnits: &one, AfterUnits: &one, Payload: grantPayload,
	}))
	require.Equal(t, "active", grants[0].Status)

	revokedTick := int64(2)
	revoked := grant
	revoked.Status = "revoked"
	revoked.RevokedTick = &revokedTick
	revoked.Version = 2
	revokePayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "control_grant_after": revoked,
	})
	require.NoError(t, err)
	minusOne := int64(-1)
	require.NoError(t, replayWorldRuntimeEffect(runtime, WorldEffectOperation{
		EffectType: WorldRuntimeEffectControlRevoke, ExecutorVersion: worldRuntimeSpatialControlVersion,
		TargetActorCode: &actorCode, TargetKey: &capability,
		BeforeUnits: &one, DeltaUnits: &minusOne, AfterUnits: &zero, Payload: revokePayload,
	}))
	require.Equal(t, "revoked", grants[0].Status)
}

func TestReplayWorldPortalEffectsPreservesStatePolicyAndProvenance(t *testing.T) {
	public, _, publicHash, err := canonicalWorldPortalAccessRequirement(
		publicWorldPortalAccessRequirement(),
	)
	require.NoError(t, err)
	before := WorldPortalState{
		BuildingCode: "building.hall", PortalCode: "entrance.main", PortalType: "entrance",
		StateCode: WorldPortalStateOpen, AccessRequirement: public,
		AccessPolicyHash: publicHash, ChangedTick: 0, Version: 1,
		Metadata: json.RawMessage(`{"schema_version":1,"source":"baseline"}`),
	}
	stateFact := WorldRuntimeFactRef{Tick: 1, Sequence: 1}
	closed := before
	closed.StateCode = WorldPortalStateClosed
	closed.ChangedTick = 1
	closed.SourceFact = &stateFact
	closed.Version = 2
	closed.Metadata = json.RawMessage(`{"schema_version":1}`)
	statePayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "portal_before": before, "portal_after": closed,
	})
	require.NoError(t, err)
	beforeVersion, delta, afterVersion := int64(1), int64(1), int64(2)
	actorCode := "actor_00000001"
	targetKey := worldPortalTargetKey(before.BuildingCode, before.PortalCode)
	portalStates := []WorldPortalState{before}
	portalStates[0].AccessRequirement.Items = []WorldRequirementNode{}
	portalStates[0].Metadata = json.RawMessage(`{ "source": "baseline", "schema_version": 1 }`)
	runtime := &worldRuntimeHashState{
		Actors:       []WorldActor{{Code: actorCode}},
		PortalStates: &portalStates,
	}
	err = replayWorldRuntimeEffect(runtime, WorldEffectOperation{
		Tick: 1, Sequence: 1, SourceFact: stateFact, OperationIndex: 1,
		EffectType: WorldRuntimeEffectPortalStateSet, ExecutorVersion: worldRuntimePortalAccessVersion,
		TargetActorCode: &actorCode, TargetKey: &targetKey,
		BeforeUnits: &beforeVersion, DeltaUnits: &delta, AfterUnits: &afterVersion,
		Payload: statePayload,
	})
	require.NoError(t, err)
	require.Equal(t, closed, portalStates[0])

	denied, _, deniedHash, err := canonicalWorldPortalAccessRequirement(WorldRequirementNode{
		Operator: WorldRequirementRoleActive, RoleCode: "profession.technician",
	})
	require.NoError(t, err)
	policyFact := WorldRuntimeFactRef{Tick: 2, Sequence: 1}
	restricted := closed
	restricted.AccessRequirement = denied
	restricted.AccessPolicyHash = deniedHash
	restricted.ChangedTick = 2
	restricted.SourceFact = &policyFact
	restricted.Version = 3
	policyPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "portal_before": closed, "portal_after": restricted,
	})
	require.NoError(t, err)
	beforeVersion, afterVersion = 2, 3
	err = replayWorldRuntimeEffect(runtime, WorldEffectOperation{
		Tick: 2, Sequence: 1, SourceFact: policyFact, OperationIndex: 1,
		EffectType: WorldRuntimeEffectPortalAccessSet, ExecutorVersion: worldRuntimePortalAccessVersion,
		TargetKey: &targetKey, BeforeUnits: &beforeVersion, DeltaUnits: &delta,
		AfterUnits: &afterVersion, Payload: policyPayload,
	})
	require.NoError(t, err)
	require.Equal(t, restricted, portalStates[0])

	tampered := restricted
	tampered.StateCode = WorldPortalStateOpen
	tamperedPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "portal_before": tampered, "portal_after": restricted,
	})
	require.NoError(t, err)
	err = replayWorldRuntimeEffect(runtime, WorldEffectOperation{
		Tick: 2, Sequence: 2, SourceFact: policyFact, OperationIndex: 1,
		EffectType: WorldRuntimeEffectPortalAccessSet, ExecutorVersion: worldRuntimePortalAccessVersion,
		TargetKey: &targetKey, BeforeUnits: &beforeVersion, DeltaUnits: &delta,
		AfterUnits: &afterVersion, Payload: tamperedPayload,
	})
	require.ErrorContains(t, err, "before state mismatch")
}
