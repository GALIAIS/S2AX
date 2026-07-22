package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentDecisionHashStateIsVersionedAndCanonical(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	state := newCityRealtimeAgentDecisionHashState(binding)
	require.NoError(t, validateCityRealtimeAgentDecisionHashState(binding, *state))

	state.PendingRequests = []cityRealtimeAgentPendingDecisionRequest{
		{
			RequestCode:           "adr.first",
			AgentCode:             "agent.npc.first",
			ObservationHash:       strings.Repeat("c", 64),
			PreconditionHash:      strings.Repeat("d", 64),
			ObservedFrameSequence: 7,
			ExpiresAtWorldTimeUS:  900000000,
		},
		{
			RequestCode:           "adr.second",
			AgentCode:             "agent.npc.second",
			ObservationHash:       strings.Repeat("a", 64),
			PreconditionHash:      strings.Repeat("b", 64),
			ObservedFrameSequence: 7,
			ExpiresAtWorldTimeUS:  900000000,
		},
	}
	require.NoError(t, validateCityRealtimeAgentDecisionHashState(binding, *state))
	state.PendingRequests[0], state.PendingRequests[1] = state.PendingRequests[1], state.PendingRequests[0]
	require.Error(t, validateCityRealtimeAgentDecisionHashState(binding, *state))

	legacy := binding
	legacy.PolicyVersion = cityRealtimeAgentCorePolicyVersionLegacy
	legacy.BindingHash = cityRealtimeAgentBindingHash(legacy)
	require.False(t, cityRealtimeAgentDecisionRuntimeEnabled(legacy))
	require.Error(t, validateCityRealtimeAgentDecisionHashState(legacy, *newCityRealtimeAgentDecisionHashState(binding)))
}

func TestCityRealtimeAgentLegacyPolicyKeepsItsOriginalHashShape(t *testing.T) {
	legacy := cityRealtimeAgentTestBinding()
	legacy.PolicyVersion = cityRealtimeAgentCorePolicyVersionLegacy
	legacy.BindingHash = cityRealtimeAgentBindingHash(legacy)
	root, err := newCityRealtimeAgentBootstrapInstance(
		legacy, cityRealtimeAgentRootCode, "simulation", "system.root", nil, nil, nil, "system",
	)
	require.NoError(t, err)
	rootCode := cityRealtimeAgentRootCode
	manager, err := newCityRealtimeAgentBootstrapInstance(
		legacy, cityRealtimeAgentNPCManagerCode, "simulation", "system.npc_manager", &rootCode, nil, nil, "system",
	)
	require.NoError(t, err)
	state := &cityRealtimeAgentHashState{
		SchemaVersion: cityRealtimeAgentRuntimeSchemaVersion,
		Binding:       &legacy,
		Agents:        []cityRealtimeAgentInstance{manager, root},
	}
	require.NoError(t, validateCityRealtimeAgentHashState(state))

	state.Decisions = newCityRealtimeAgentDecisionHashState(cityRealtimeAgentTestBinding())
	require.Error(t, validateCityRealtimeAgentHashState(state))
}

func TestCityRealtimeAgentDecisionEnvelopeRejectsUnscopedArguments(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	root, err := newCityRealtimeAgentBootstrapInstance(
		binding, cityRealtimeAgentRootCode, "simulation", "system.root", nil, nil, nil, "system",
	)
	require.NoError(t, err)
	request := cityRealtimeAgentDecisionRequestRecord{
		RequestCode:      "adr.test",
		ObservationHash:  strings.Repeat("a", 64),
		PreconditionHash: strings.Repeat("b", 64),
	}
	envelope := cityRealtimeAgentDecisionEnvelope{
		SchemaVersion:    cityRealtimeAgentDecisionEnvelopeVersion,
		RequestCode:      request.RequestCode,
		ObservationHash:  request.ObservationHash,
		PreconditionHash: request.PreconditionHash,
		Intent: cityRealtimeAgentEnvelopeIntent{
			ActionCode: cityRealtimeAgentIntentActionWait,
			Arguments:  map[string]any{},
		},
		ReasonCode: "fake_provider_wait",
	}
	arguments, hash, err := validateCityRealtimeAgentDecisionEnvelope(binding, root, request, envelope)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(arguments))
	require.True(t, cityRealtimeSHA256Hex(hash))

	envelope.Intent.Arguments = map[string]any{"move_to": "x"}
	_, _, err = validateCityRealtimeAgentDecisionEnvelope(binding, root, request, envelope)
	require.ErrorIs(t, err, ErrCityRealtimeAgentDecisionUnavailable)

	envelope.Intent.Arguments = map[string]any{}
	envelope.Intent.ActionCode = "character.move"
	_, _, err = validateCityRealtimeAgentDecisionEnvelope(binding, root, request, envelope)
	require.ErrorIs(t, err, ErrCityRealtimeAgentDecisionUnavailable)
}

func TestCityRealtimeCharacterActionPoliciesPreserveHistoricalCatalogues(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	actorCode := "character.player.0123456789abcdef0123456789abcdef"
	agent := cityRealtimeAgentInstance{
		AgentCode:       "agent.user.0123456789abcdef0123456789abcdef",
		AgentSubtype:    "character.user",
		LifecycleStatus: "active",
		ControlMode:     "autonomous",
		ActorCode:       &actorCode,
	}

	actions, available := cityRealtimeAgentDecisionAllowedActions(binding, agent)
	require.True(t, available)
	require.Equal(t, []string{
		cityRealtimeAgentIntentActionWait,
		cityRealtimeAgentIntentActionActivity,
		cityRealtimeAgentIntentActionMove,
		cityRealtimeAgentIntentActionPortal,
		cityRealtimeAgentIntentActionRole,
	}, actions)

	autonomy := binding
	autonomy.PolicyVersion = cityRealtimeAgentCorePolicyVersionAutonomy
	autonomy.BindingHash = cityRealtimeAgentBindingHash(autonomy)
	actions, available = cityRealtimeAgentDecisionAllowedActions(autonomy, agent)
	require.True(t, available)
	require.Equal(t, []string{cityRealtimeAgentIntentActionWait, cityRealtimeAgentIntentActionActivity}, actions)

	legacyDecision := binding
	legacyDecision.PolicyVersion = cityRealtimeAgentCorePolicyVersionDecision
	legacyDecision.BindingHash = cityRealtimeAgentBindingHash(legacyDecision)
	actions, available = cityRealtimeAgentDecisionAllowedActions(legacyDecision, agent)
	require.True(t, available)
	require.Equal(t, []string{cityRealtimeAgentIntentActionWait}, actions)
	require.False(t, cityRealtimeAgentDecisionActionAllowed(legacyDecision, agent, cityRealtimeAgentIntentActionActivity))
	require.True(t, cityRealtimeAgentPolicyVersionSupported(cityRealtimeAgentCorePolicyVersionLegacy))
	require.True(t, cityRealtimeAgentPolicyVersionSupported(cityRealtimeAgentCorePolicyVersionDecision))
	require.True(t, cityRealtimeAgentPolicyVersionSupported(cityRealtimeAgentCorePolicyVersionAutonomy))
	require.True(t, cityRealtimeAgentPolicyVersionSupported(cityRealtimeAgentCorePolicyVersion))
	require.False(t, cityRealtimeAgentPolicyVersionSupported("1.4.0"))
}

func TestCityRealtimeCharacterActivityEnvelopeRequiresOnePublishedActionArgument(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	actorCode := "character.player.0123456789abcdef0123456789abcdef"
	agent := cityRealtimeAgentInstance{
		AgentCode:       "agent.user.0123456789abcdef0123456789abcdef",
		AgentSubtype:    "character.user",
		LifecycleStatus: "active",
		ControlMode:     "autonomous",
		ActorCode:       &actorCode,
	}
	request := cityRealtimeAgentDecisionRequestRecord{
		RequestCode:      "adr.test",
		ObservationHash:  strings.Repeat("a", 64),
		PreconditionHash: strings.Repeat("b", 64),
	}
	envelope := cityRealtimeAgentDecisionEnvelope{
		SchemaVersion:    cityRealtimeAgentDecisionEnvelopeVersion,
		RequestCode:      request.RequestCode,
		ObservationHash:  request.ObservationHash,
		PreconditionHash: request.PreconditionHash,
		Intent: cityRealtimeAgentEnvelopeIntent{
			ActionCode: cityRealtimeAgentIntentActionActivity,
			Arguments:  map[string]any{"activity_code": "work.civic_shift"},
		},
		ReasonCode: "fake_provider_activity",
	}
	arguments, hash, err := validateCityRealtimeAgentDecisionEnvelope(binding, agent, request, envelope)
	require.NoError(t, err)
	require.JSONEq(t, `{"activity_code":"work.civic_shift"}`, string(arguments))
	require.True(t, cityRealtimeSHA256Hex(hash))

	envelope.Intent.Arguments["untrusted"] = true
	_, _, err = validateCityRealtimeAgentDecisionEnvelope(binding, agent, request, envelope)
	require.ErrorIs(t, err, ErrCityRealtimeAgentDecisionUnavailable)

	envelope.Intent.Arguments = map[string]any{"activity_code": "work.civic_shift"}
	legacyDecision := binding
	legacyDecision.PolicyVersion = cityRealtimeAgentCorePolicyVersionDecision
	legacyDecision.BindingHash = cityRealtimeAgentBindingHash(legacyDecision)
	_, _, err = validateCityRealtimeAgentDecisionEnvelope(legacyDecision, agent, request, envelope)
	require.ErrorIs(t, err, ErrCityRealtimeAgentDecisionUnavailable)
}

func TestCityRealtimeCharacterActionEnvelopeRequiresStrictFiniteArguments(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	actorCode := "character.player.0123456789abcdef0123456789abcdef"
	agent := cityRealtimeAgentInstance{
		AgentCode:       "agent.user.0123456789abcdef0123456789abcdef",
		AgentSubtype:    "character.user",
		LifecycleStatus: "active",
		ControlMode:     "autonomous",
		ActorCode:       &actorCode,
	}
	request := cityRealtimeAgentDecisionRequestRecord{
		RequestCode:      "adr.action",
		ObservationHash:  strings.Repeat("a", 64),
		PreconditionHash: strings.Repeat("b", 64),
	}
	for _, envelope := range []cityRealtimeAgentDecisionEnvelope{
		{
			SchemaVersion: cityRealtimeAgentDecisionEnvelopeVersion, RequestCode: request.RequestCode,
			ObservationHash: request.ObservationHash, PreconditionHash: request.PreconditionHash,
			Intent: cityRealtimeAgentEnvelopeIntent{
				ActionCode: cityRealtimeAgentIntentActionMove,
				Arguments:  map[string]any{"x": int64(41), "y": int64(-7), "z": int32(0)},
			}, ReasonCode: "fake_provider_move",
		},
		{
			SchemaVersion: cityRealtimeAgentDecisionEnvelopeVersion, RequestCode: request.RequestCode,
			ObservationHash: request.ObservationHash, PreconditionHash: request.PreconditionHash,
			Intent: cityRealtimeAgentEnvelopeIntent{
				ActionCode: cityRealtimeAgentIntentActionPortal,
				Arguments:  map[string]any{"portal_code": "building.market.entrance"},
			}, ReasonCode: "fake_provider_portal",
		},
		{
			SchemaVersion: cityRealtimeAgentDecisionEnvelopeVersion, RequestCode: request.RequestCode,
			ObservationHash: request.ObservationHash, PreconditionHash: request.PreconditionHash,
			Intent: cityRealtimeAgentEnvelopeIntent{
				ActionCode: cityRealtimeAgentIntentActionRole,
				Arguments:  map[string]any{"role_code": "profession.civic_worker"},
			}, ReasonCode: "fake_provider_role",
		},
	} {
		_, _, err := validateCityRealtimeAgentDecisionEnvelope(binding, agent, request, envelope)
		require.NoError(t, err)
	}

	invalid := cityRealtimeAgentDecisionEnvelope{
		SchemaVersion: cityRealtimeAgentDecisionEnvelopeVersion, RequestCode: request.RequestCode,
		ObservationHash: request.ObservationHash, PreconditionHash: request.PreconditionHash,
		Intent: cityRealtimeAgentEnvelopeIntent{
			ActionCode: cityRealtimeAgentIntentActionMove,
			Arguments:  map[string]any{"x": int64(41), "y": int64(-7), "z": int32(0), "route": "untrusted"},
		}, ReasonCode: "fake_provider_move",
	}
	_, _, err := validateCityRealtimeAgentDecisionEnvelope(binding, agent, request, invalid)
	require.ErrorIs(t, err, ErrCityRealtimeAgentDecisionUnavailable)

	autonomy := binding
	autonomy.PolicyVersion = cityRealtimeAgentCorePolicyVersionAutonomy
	autonomy.BindingHash = cityRealtimeAgentBindingHash(autonomy)
	invalid.Intent.Arguments = map[string]any{"x": int64(41), "y": int64(-7), "z": int32(0)}
	_, _, err = validateCityRealtimeAgentDecisionEnvelope(autonomy, agent, request, invalid)
	require.ErrorIs(t, err, ErrCityRealtimeAgentDecisionUnavailable)
}

func TestCityRealtimeCharacterActionMustComeFromSealedFiniteContext(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	actorCode := "character.player.0123456789abcdef0123456789abcdef"
	agent := cityRealtimeAgentInstance{
		AgentCode:       "agent.user.0123456789abcdef0123456789abcdef",
		AgentSubtype:    "character.user",
		LifecycleStatus: "active",
		ControlMode:     "autonomous",
		ActorCode:       &actorCode,
	}
	contextPayload := cityRealtimeAgentDecisionActionContext{
		SchemaVersion:          1,
		AvailableActivityCodes: []string{"rest.short"},
		AvailableMoveTargets: []cityRealtimeAgentMoveTarget{
			{X: 12, Y: 7, Z: 0},
		},
		AvailablePortalCodes: []string{"building.market.entrance"},
		AvailableRoleCodes:   []string{"profession.civic_worker"},
	}
	payload, err := json.Marshal(map[string]any{
		"allowed_actions": []string{
			cityRealtimeAgentIntentActionWait,
			cityRealtimeAgentIntentActionActivity,
			cityRealtimeAgentIntentActionMove,
			cityRealtimeAgentIntentActionPortal,
			cityRealtimeAgentIntentActionRole,
		},
		"character": map[string]any{"action_context": contextPayload},
	})
	require.NoError(t, err)
	observation := cityRealtimeAgentObservationRecord{Payload: payload}

	require.NoError(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionMove,
		map[string]any{"x": int64(12), "y": int64(7), "z": int32(0)},
	))
	require.NoError(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionPortal,
		map[string]any{"portal_code": "building.market.entrance"},
	))
	require.NoError(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionRole,
		map[string]any{"role_code": "profession.civic_worker"},
	))
	require.ErrorIs(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionMove,
		map[string]any{"x": int64(13), "y": int64(7), "z": int32(0)},
	), ErrCityRealtimeAgentDecisionUnavailable)
	require.ErrorIs(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionPortal,
		map[string]any{"portal_code": "building.secret.entrance"},
	), ErrCityRealtimeAgentDecisionUnavailable)

	legacy := binding
	legacy.PolicyVersion = cityRealtimeAgentCorePolicyVersionAutonomy
	legacy.BindingHash = cityRealtimeAgentBindingHash(legacy)
	require.NoError(t, cityRealtimeAgentDecisionValidatePublishedAction(
		legacy, agent, observation, cityRealtimeAgentIntentActionActivity,
		map[string]any{"activity_code": "rest.short"},
	), "A3.1 keeps its original observation boundary")
}

func TestCityRealtimeAgentDecisionCodesAreStableAndBounded(t *testing.T) {
	first, err := cityRealtimeAgentDecisionStableCode("adr", "binding", "agent.npc.demo", "observation")
	require.NoError(t, err)
	second, err := cityRealtimeAgentDecisionStableCode("adr", "binding", "agent.npc.demo", "observation")
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.True(t, cityRealtimeAgentIdentifierValid(first, 96))

	changed, err := cityRealtimeAgentDecisionStableCode("adr", "binding", "agent.npc.demo", "other-observation")
	require.NoError(t, err)
	require.NotEqual(t, first, changed)
}
