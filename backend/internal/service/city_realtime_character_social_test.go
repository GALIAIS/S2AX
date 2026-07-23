package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterSocialPairAndTargetBoundaryAreDeterministic(t *testing.T) {
	actor := cityRealtimeActorState{
		ActorCode: "character.player.0123456789abcdef0123456789abcdef",
		X:         10,
		Y:         -4,
		Z:         0,
	}
	require.True(t, cityRealtimeCharacterSocialTargetAllowed(actor, cityRealtimeCharacterSocialTarget{
		ActorCode:       "npc.citizen.0123456789abcdef0123456789abcdef",
		ActorKind:       "npc",
		LifecycleStatus: "active",
		X:               11,
		Y:               -4,
		Z:               0,
	}))
	for _, target := range []cityRealtimeCharacterSocialTarget{
		{ActorCode: actor.ActorCode, ActorKind: "npc", LifecycleStatus: "active", X: 11, Y: -4, Z: 0},
		{ActorCode: "npc.citizen.0123456789abcdef0123456789abcdef", ActorKind: "npc", LifecycleStatus: "active", X: 12, Y: -4, Z: 0},
		{ActorCode: "npc.citizen.0123456789abcdef0123456789abcdef", ActorKind: "npc", LifecycleStatus: "inactive", X: 11, Y: -4, Z: 0},
		{ActorCode: "system.root.0123456789abcdef0123456789abcdef", ActorKind: "system", LifecycleStatus: "active", X: 11, Y: -4, Z: 0},
		{ActorCode: "npc.citizen.0123456789abcdef0123456789abcdef", ActorKind: "npc", LifecycleStatus: "active", X: 11, Y: -3, Z: 1},
	} {
		require.False(t, cityRealtimeCharacterSocialTargetAllowed(actor, target))
	}

	low, high, err := cityRealtimeCharacterSocialPair(
		"npc.citizen.0123456789abcdef0123456789abcdef", actor.ActorCode,
	)
	require.NoError(t, err)
	require.Less(t, low, high)
	require.Equal(t, actor.ActorCode, low)
	_, _, err = cityRealtimeCharacterSocialPair(actor.ActorCode, actor.ActorCode)
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestCityRealtimeCharacterSocialGreetingIsAppendOnlyAndCapped(t *testing.T) {
	actor := "character.player.0123456789abcdef0123456789abcdef"
	target := "npc.citizen.0123456789abcdef0123456789abcdef"
	low, high, err := cityRealtimeCharacterSocialPair(actor, target)
	require.NoError(t, err)
	head, err := newCityRealtimeCharacterSocialGenesisHead(low, high)
	require.NoError(t, err)
	require.Equal(t, int64(0), head.RelationRevision)
	require.Equal(t, int64(0), head.AffinityMilli)

	for index := int64(1); index <= 25; index++ {
		next, event, greetErr := cityRealtimeCharacterSocialGreet(
			head, actor, target, "ait.social."+strings.Repeat("a", 64), index,
		)
		require.NoError(t, greetErr)
		require.True(t, cityRealtimeCharacterSocialEventValid(event))
		require.Equal(t, head.EventChainHash, event.PreviousEventHash)
		require.Equal(t, index, event.EventSequence)
		require.Equal(t, minInt64(index*cityRealtimeCharacterSocialAffinityStep, cityRealtimeCharacterSocialAffinityMaximum), next.AffinityMilli)
		head = next
	}
	require.Equal(t, int64(25), head.RelationRevision)
	require.Equal(t, cityRealtimeCharacterSocialAffinityMaximum, head.AffinityMilli)
	require.Equal(t, int64(25), head.InteractionCount)

	_, _, err = cityRealtimeCharacterSocialGreet(
		head, actor, target, "ait.social."+strings.Repeat("a", 64), head.LastFrameSequence,
	)
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestCityRealtimeCharacterSocialArgumentsAreStrict(t *testing.T) {
	target := "npc.citizen.0123456789abcdef0123456789abcdef"
	code, err := cityRealtimeAgentDecisionSocialTargetCodeFromArguments(map[string]any{
		"target_actor_code": target,
	})
	require.NoError(t, err)
	require.Equal(t, target, code)
	for _, arguments := range []map[string]any{
		{},
		{"target_actor_code": target, "message": "hello"},
		{"target_actor_code": "NPC.invalid"},
		{"target_actor_code": 42},
	} {
		_, err = cityRealtimeAgentDecisionSocialTargetCodeFromArguments(arguments)
		require.ErrorIs(t, err, ErrCityRealtimeAgentDecisionUnavailable)
	}
}

func TestCityRealtimeCharacterSocialRelationCursorIsBounded(t *testing.T) {
	cursor := cityRealtimeCharacterSocialRelationCursor{
		FrameSequence: 24,
		ActorCodeLow:  "character.player.0123456789abcdef0123456789abcdef",
		ActorCodeHigh: "npc.citizen.0123456789abcdef0123456789abcdef",
	}
	parsed, err := parseCityRealtimeCharacterSocialRelationCursor(cursor.String())
	require.NoError(t, err)
	require.Equal(t, cursor, parsed)
	for _, value := range []string{
		"",
		"0|character.player.0123456789abcdef0123456789abcdef|npc.citizen.0123456789abcdef0123456789abcdef",
		"24|character.player.0123456789abcdef0123456789abcdef",
		"24|NPC.player.0123456789abcdef0123456789abcdef|npc.citizen.0123456789abcdef0123456789abcdef",
		"24|npc.citizen.0123456789abcdef0123456789abcdef|character.player.0123456789abcdef0123456789abcdef",
		"24|character.player.0123456789abcdef0123456789abcdef|npc.citizen.0123456789abcdef0123456789abcdef|extra",
	} {
		if value == "" {
			continue
		}
		_, err = parseCityRealtimeCharacterSocialRelationCursor(value)
		require.ErrorIs(t, err, ErrCityInvalidInput)
	}
}

func TestCityRealtimeCharacterSocialRelationExplanationIsSealedAndDirectional(t *testing.T) {
	owner := "character.player.0123456789abcdef0123456789abcdef"
	target := "npc.citizen.0123456789abcdef0123456789abcdef"
	low, high, err := cityRealtimeCharacterSocialPair(owner, target)
	require.NoError(t, err)
	head, err := newCityRealtimeCharacterSocialGenesisHead(low, high)
	require.NoError(t, err)

	empty, err := projectCityRealtimeCharacterSocialRelationExplanation(head, owner, target, 0, 0, "")
	require.NoError(t, err)
	require.Equal(t, cityRealtimeCharacterSocialContactTierNone, empty.ContactTier)
	require.Equal(t, cityRealtimeCharacterSocialInteractionPatternNone, empty.InteractionPattern)
	require.Equal(t, []string{"no_greeting_recorded"}, empty.ReasonCodes)

	head, _, err = cityRealtimeCharacterSocialGreet(
		head, owner, target, "ait.social."+strings.Repeat("a", 64), 1,
	)
	require.NoError(t, err)
	initial, err := projectCityRealtimeCharacterSocialRelationExplanation(head, owner, target, 1, 0, owner)
	require.NoError(t, err)
	require.Equal(t, cityRealtimeCharacterSocialRelationExplanationSchemaVersion, initial.SchemaVersion)
	require.Equal(t, cityRealtimeCharacterSocialContactTierInitial, initial.ContactTier)
	require.Equal(t, cityRealtimeCharacterSocialInteractionPatternOutbound, initial.InteractionPattern)
	require.Equal(t, cityRealtimeCharacterSocialLastInteractionOutbound, initial.LastInteractionDirection)
	require.Equal(t, []string{"greeting_recorded"}, initial.ReasonCodes)

	head, _, err = cityRealtimeCharacterSocialGreet(
		head, target, owner, "ait.social."+strings.Repeat("b", 64), 2,
	)
	require.NoError(t, err)
	reciprocal, err := projectCityRealtimeCharacterSocialRelationExplanation(head, owner, target, 1, 1, target)
	require.NoError(t, err)
	require.Equal(t, cityRealtimeCharacterSocialContactTierRecurring, reciprocal.ContactTier)
	require.Equal(t, cityRealtimeCharacterSocialInteractionPatternReciprocal, reciprocal.InteractionPattern)
	require.Equal(t, cityRealtimeCharacterSocialLastInteractionInbound, reciprocal.LastInteractionDirection)
	require.Equal(t, []string{"greeting_recorded", "repeated_greeting", "reciprocal_greeting"}, reciprocal.ReasonCodes)

	_, err = projectCityRealtimeCharacterSocialRelationExplanation(head, owner, target, 2, 1, target)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
	_, err = projectCityRealtimeCharacterSocialRelationExplanation(head, owner, target, 1, 1, "system.root")
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
