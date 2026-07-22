package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterPersonalitySeedIsCanonicalAndBounded(t *testing.T) {
	normalized, err := normalizeCityRealtimeCharacterPersonalitySeed(CityRealtimeCharacterPersonalitySeed{
		Values:         []string{"  community  ", "curiosity"},
		HardBoundaries: []string{"avoid harm", "respect consent"},
		Preferences: map[string]string{
			"work_style": " public service ",
			"leisure":    "reading",
		},
		Background:    "  Raised in a busy district.  ",
		FreeformNotes: "Keep this owner-private and data-only.",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"community", "curiosity"}, normalized.Values)
	require.Equal(t, []string{"avoid harm", "respect consent"}, normalized.HardBoundaries)
	require.Equal(t, map[string]string{
		"leisure":    "reading",
		"work_style": "public service",
	}, normalized.Preferences)
	require.Equal(t, "Raised in a busy district.", normalized.Background)

	for _, input := range []CityRealtimeCharacterPersonalitySeed{
		{Values: []string{"repeat", " repeat "}},
		{Values: []string{"unsafe\nvalue"}},
		{Values: []string{"unsafe\u202evalue"}},
		{Values: []string{strings.Repeat("x", cityRealtimeCharacterPersonalityValueMaximumRunes+1)}},
		{Values: []string{"valid"}, Preferences: map[string]string{"Invalid Key": "value"}},
	} {
		_, normalizeErr := normalizeCityRealtimeCharacterPersonalitySeed(input)
		require.ErrorIs(t, normalizeErr, ErrCityInvalidInput)
	}
}

func TestCityRealtimeCharacterAgentControlEventIsVersionScopedAndChained(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	actorCode := "character.player.0123456789abcdef0123456789abcdef"
	ownerUserID := int64(42)
	instance, err := newCityRealtimeAgentSpawnInstance(
		binding,
		"agent.user.0123456789abcdef0123456789abcdef",
		"character",
		"character.user",
		nil,
		&actorCode,
		&ownerUserID,
		"manual",
		1,
		"character.create",
	)
	require.NoError(t, err)

	updated, control, err := cityRealtimeCharacterAgentControlUpdate(
		binding, instance, "autonomous", 2, cityRealtimeCharacterAgentConfigureAction,
	)
	require.NoError(t, err)
	require.Equal(t, "control", control.EventType)
	require.Equal(t, "active", control.ToStatus)
	require.Equal(t, "autonomous", control.ControlMode)
	require.Equal(t, int64(2), updated.LastFrameSequence)

	spawn := cityRealtimeAgentLifecycleEvent{
		AgentCode: instance.AgentCode, EventSequence: 0, FrameSequence: 1,
		EventType: "spawn", ToStatus: "active", ControlMode: "manual",
		ReasonCode: "character.create", EventHash: instance.EventChainHash,
	}
	require.NoError(t, validateCityRealtimeAgentLifecycleChain(binding, updated, []cityRealtimeAgentLifecycleEvent{spawn, control}))

	legacy := binding
	legacy.PolicyVersion = cityRealtimeAgentCorePolicyVersionDecision
	legacy.BindingHash = cityRealtimeAgentBindingHash(legacy)
	_, _, err = cityRealtimeCharacterAgentControlUpdate(
		legacy, instance, "autonomous", 2, cityRealtimeCharacterAgentConfigureAction,
	)
	require.ErrorIs(t, err, ErrCityInvalidInput)
}
