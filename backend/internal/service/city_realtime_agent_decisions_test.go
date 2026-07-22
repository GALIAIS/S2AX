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
		cityRealtimeAgentIntentActionCase,
		cityRealtimeAgentIntentActionCaseReview,
		cityRealtimeAgentIntentActionMove,
		cityRealtimeAgentIntentActionPortal,
		cityRealtimeAgentIntentActionRole,
		cityRealtimeAgentIntentActionSocial,
	}, actions)

	socialAdapters := binding
	socialAdapters.PolicyVersion = cityRealtimeAgentCorePolicyVersionSocial
	socialAdapters.BindingHash = cityRealtimeAgentBindingHash(socialAdapters)
	actions, available = cityRealtimeAgentDecisionAllowedActions(socialAdapters, agent)
	require.True(t, available)
	require.Equal(t, []string{
		cityRealtimeAgentIntentActionWait,
		cityRealtimeAgentIntentActionActivity,
		cityRealtimeAgentIntentActionCase,
		cityRealtimeAgentIntentActionMove,
		cityRealtimeAgentIntentActionPortal,
		cityRealtimeAgentIntentActionRole,
		cityRealtimeAgentIntentActionSocial,
	}, actions, "1.5.0 stays byte-for-byte scoped to the social catalogue")

	caseAdapters := binding
	caseAdapters.PolicyVersion = cityRealtimeAgentCorePolicyVersionCase
	caseAdapters.BindingHash = cityRealtimeAgentBindingHash(caseAdapters)
	actions, available = cityRealtimeAgentDecisionAllowedActions(caseAdapters, agent)
	require.True(t, available)
	require.Equal(t, []string{
		cityRealtimeAgentIntentActionWait,
		cityRealtimeAgentIntentActionActivity,
		cityRealtimeAgentIntentActionCase,
		cityRealtimeAgentIntentActionMove,
		cityRealtimeAgentIntentActionPortal,
		cityRealtimeAgentIntentActionRole,
	}, actions, "1.4.0 stays byte-for-byte scoped to the Case catalogue")

	actionAdapters := binding
	actionAdapters.PolicyVersion = cityRealtimeAgentCorePolicyVersionActions
	actionAdapters.BindingHash = cityRealtimeAgentBindingHash(actionAdapters)
	actions, available = cityRealtimeAgentDecisionAllowedActions(actionAdapters, agent)
	require.True(t, available)
	require.Equal(t, []string{
		cityRealtimeAgentIntentActionWait,
		cityRealtimeAgentIntentActionActivity,
		cityRealtimeAgentIntentActionMove,
		cityRealtimeAgentIntentActionPortal,
		cityRealtimeAgentIntentActionRole,
	}, actions, "1.3.0 stays byte-for-byte scoped to its A3.2 catalogue")

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
	require.True(t, cityRealtimeAgentPolicyVersionSupported(cityRealtimeAgentCorePolicyVersionActions))
	require.True(t, cityRealtimeAgentPolicyVersionSupported(cityRealtimeAgentCorePolicyVersionCase))
	require.True(t, cityRealtimeAgentPolicyVersionSupported(cityRealtimeAgentCorePolicyVersionSocial))
	require.True(t, cityRealtimeAgentPolicyVersionSupported(cityRealtimeAgentCorePolicyVersion))
	require.False(t, cityRealtimeAgentPolicyVersionSupported("9.9.0"))
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
				ActionCode: cityRealtimeAgentIntentActionCase,
				Arguments:  map[string]any{"case_code": "law.0123456789abcdef.1"},
			}, ReasonCode: "fake_provider_case",
		},
		{
			SchemaVersion: cityRealtimeAgentDecisionEnvelopeVersion, RequestCode: request.RequestCode,
			ObservationHash: request.ObservationHash, PreconditionHash: request.PreconditionHash,
			Intent: cityRealtimeAgentEnvelopeIntent{
				ActionCode: cityRealtimeAgentIntentActionCaseReview,
				Arguments:  map[string]any{"case_code": "law.0123456789abcdef.1"},
			}, ReasonCode: "fake_provider_case_review",
		},
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
	binding.PolicyVersion = cityRealtimeAgentCorePolicyVersionCase
	binding.BindingHash = cityRealtimeAgentBindingHash(binding)
	actorCode := "character.player.0123456789abcdef0123456789abcdef"
	agent := cityRealtimeAgentInstance{
		AgentCode:       "agent.user.0123456789abcdef0123456789abcdef",
		AgentSubtype:    "character.user",
		LifecycleStatus: "active",
		ControlMode:     "autonomous",
		ActorCode:       &actorCode,
	}
	contextPayload := cityRealtimeAgentDecisionActionContext{
		SchemaVersion:          2,
		AvailableActivityCodes: []string{"rest.short"},
		AvailableMoveTargets: []cityRealtimeAgentMoveTarget{
			{X: 12, Y: 7, Z: 0},
		},
		AvailablePortalCodes: []string{"building.market.entrance"},
		AvailableRoleCodes:   []string{"profession.civic_worker"},
		AvailableCaseCodes:   []string{"law.0123456789abcdef.1"},
	}
	payload, err := json.Marshal(map[string]any{
		"allowed_actions": []string{
			cityRealtimeAgentIntentActionWait,
			cityRealtimeAgentIntentActionActivity,
			cityRealtimeAgentIntentActionCase,
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
	require.NoError(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionCase,
		map[string]any{"case_code": "law.0123456789abcdef.1"},
	))
	require.ErrorIs(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionMove,
		map[string]any{"x": int64(13), "y": int64(7), "z": int32(0)},
	), ErrCityRealtimeAgentDecisionUnavailable)
	require.ErrorIs(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionPortal,
		map[string]any{"portal_code": "building.secret.entrance"},
	), ErrCityRealtimeAgentDecisionUnavailable)
	require.ErrorIs(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionCase,
		map[string]any{"case_code": "law.0123456789abcdef.2"},
	), ErrCityRealtimeAgentDecisionUnavailable)

	// 1.3.0 accepts the frozen schema-v1 context and never receives a Case
	// candidate just because a later executable supports 1.4.0.
	actionAdapters := binding
	actionAdapters.PolicyVersion = cityRealtimeAgentCorePolicyVersionActions
	actionAdapters.BindingHash = cityRealtimeAgentBindingHash(actionAdapters)
	legacyContext := contextPayload
	legacyContext.SchemaVersion = 1
	legacyContext.AvailableCaseCodes = nil
	legacyPayload, err := json.Marshal(map[string]any{
		"allowed_actions": []string{
			cityRealtimeAgentIntentActionWait,
			cityRealtimeAgentIntentActionActivity,
			cityRealtimeAgentIntentActionMove,
			cityRealtimeAgentIntentActionPortal,
			cityRealtimeAgentIntentActionRole,
		},
		"character": map[string]any{"action_context": legacyContext},
	})
	require.NoError(t, err)
	legacyObservation := cityRealtimeAgentObservationRecord{Payload: legacyPayload}
	require.NoError(t, cityRealtimeAgentDecisionValidatePublishedAction(
		actionAdapters, agent, legacyObservation, cityRealtimeAgentIntentActionActivity,
		map[string]any{"activity_code": "rest.short"},
	))
	require.ErrorIs(t, cityRealtimeAgentDecisionValidatePublishedAction(
		actionAdapters, agent, legacyObservation, cityRealtimeAgentIntentActionCase,
		map[string]any{"case_code": "law.0123456789abcdef.1"},
	), ErrCityRealtimeAgentDecisionUnavailable)

	legacy := binding
	legacy.PolicyVersion = cityRealtimeAgentCorePolicyVersionAutonomy
	legacy.BindingHash = cityRealtimeAgentBindingHash(legacy)
	require.NoError(t, cityRealtimeAgentDecisionValidatePublishedAction(
		legacy, agent, observation, cityRealtimeAgentIntentActionActivity,
		map[string]any{"activity_code": "rest.short"},
	), "A3.1 keeps its original observation boundary")
}

func TestCityRealtimeCharacterActionContextKeepsVersionedWireShape(t *testing.T) {
	base := cityRealtimeAgentDecisionActionContext{
		AvailableActivityCodes: make([]string, 0),
		AvailableMoveTargets:   make([]cityRealtimeAgentMoveTarget, 0),
		AvailablePortalCodes:   make([]string, 0),
		AvailableRoleCodes:     make([]string, 0),
	}
	v1 := base
	v1.SchemaVersion = 1
	v1Raw, err := json.Marshal(v1)
	require.NoError(t, err)
	require.NotContains(t, string(v1Raw), "available_case_codes")

	v2 := base
	v2.SchemaVersion = 2
	v2.AvailableCaseCodes = make([]string, 0)
	v2Raw, err := json.Marshal(v2)
	require.NoError(t, err)
	require.Contains(t, string(v2Raw), `"available_case_codes":[]`)
	var decoded cityRealtimeAgentDecisionActionContext
	require.NoError(t, json.Unmarshal(v2Raw, &decoded))
	require.True(t, cityRealtimeAgentDecisionActionContextValid(decoded))

	v3 := base
	v3.SchemaVersion = 3
	v3.AvailableCaseCodes = make([]string, 0)
	v3.AvailableSocialTargets = make([]string, 0)
	v3Raw, err := json.Marshal(v3)
	require.NoError(t, err)
	require.Contains(t, string(v3Raw), `"available_case_codes":[]`)
	require.Contains(t, string(v3Raw), `"available_social_target_codes":[]`)
	require.NoError(t, json.Unmarshal(v3Raw, &decoded))
	require.True(t, cityRealtimeAgentDecisionActionContextValid(decoded))

	v4 := base
	v4.SchemaVersion = 4
	v4.AvailableCaseCodes = make([]string, 0)
	v4.AvailableSocialTargets = make([]string, 0)
	v4.AvailableCaseReviewCodes = make([]string, 0)
	v4Raw, err := json.Marshal(v4)
	require.NoError(t, err)
	require.Contains(t, string(v4Raw), `"available_case_codes":[]`)
	require.Contains(t, string(v4Raw), `"available_social_target_codes":[]`)
	require.Contains(t, string(v4Raw), `"available_case_review_codes":[]`)
	require.NoError(t, json.Unmarshal(v4Raw, &decoded))
	require.True(t, cityRealtimeAgentDecisionActionContextValid(decoded))
}

func TestCityRealtimeCharacterSocialActionMustUsePublishedTarget(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	binding.PolicyVersion = cityRealtimeAgentCorePolicyVersionSocial
	binding.BindingHash = cityRealtimeAgentBindingHash(binding)
	actorCode := "character.player.0123456789abcdef0123456789abcdef"
	otherActorCode := "npc.citizen.0123456789abcdef0123456789abcdef"
	agent := cityRealtimeAgentInstance{
		AgentCode:       "agent.user.0123456789abcdef0123456789abcdef",
		AgentSubtype:    "character.user",
		LifecycleStatus: "active",
		ControlMode:     "autonomous",
		ActorCode:       &actorCode,
	}
	contextPayload := cityRealtimeAgentDecisionActionContext{
		SchemaVersion:          3,
		AvailableActivityCodes: []string{},
		AvailableMoveTargets:   []cityRealtimeAgentMoveTarget{},
		AvailablePortalCodes:   []string{},
		AvailableRoleCodes:     []string{},
		AvailableCaseCodes:     []string{},
		AvailableSocialTargets: []string{otherActorCode},
	}
	payload, err := json.Marshal(map[string]any{
		"allowed_actions": []string{
			cityRealtimeAgentIntentActionWait,
			cityRealtimeAgentIntentActionActivity,
			cityRealtimeAgentIntentActionCase,
			cityRealtimeAgentIntentActionMove,
			cityRealtimeAgentIntentActionPortal,
			cityRealtimeAgentIntentActionRole,
			cityRealtimeAgentIntentActionSocial,
		},
		"character": map[string]any{"action_context": contextPayload},
	})
	require.NoError(t, err)
	observation := cityRealtimeAgentObservationRecord{Payload: payload}
	require.NoError(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionSocial,
		map[string]any{"target_actor_code": otherActorCode},
	))
	require.ErrorIs(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionSocial,
		map[string]any{"target_actor_code": "npc.other.0123456789abcdef0123456789abcdef"},
	), ErrCityRealtimeAgentDecisionUnavailable)
}

func TestCityRealtimeCharacterCaseReviewMustUsePublishedAcknowledgement(t *testing.T) {
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
		SchemaVersion:            4,
		AvailableActivityCodes:   []string{},
		AvailableMoveTargets:     []cityRealtimeAgentMoveTarget{},
		AvailablePortalCodes:     []string{},
		AvailableRoleCodes:       []string{},
		AvailableCaseCodes:       []string{},
		AvailableSocialTargets:   []string{},
		AvailableCaseReviewCodes: []string{"law.0123456789abcdef.1"},
	}
	payload, err := json.Marshal(map[string]any{
		"allowed_actions": []string{
			cityRealtimeAgentIntentActionWait,
			cityRealtimeAgentIntentActionActivity,
			cityRealtimeAgentIntentActionCase,
			cityRealtimeAgentIntentActionCaseReview,
			cityRealtimeAgentIntentActionMove,
			cityRealtimeAgentIntentActionPortal,
			cityRealtimeAgentIntentActionRole,
			cityRealtimeAgentIntentActionSocial,
		},
		"character": map[string]any{"action_context": contextPayload},
	})
	require.NoError(t, err)
	observation := cityRealtimeAgentObservationRecord{Payload: payload}
	require.NoError(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionCaseReview,
		map[string]any{"case_code": "law.0123456789abcdef.1"},
	))
	require.ErrorIs(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionCaseReview,
		map[string]any{"case_code": "law.0123456789abcdef.2"},
	), ErrCityRealtimeAgentDecisionUnavailable)
	require.ErrorIs(t, cityRealtimeAgentDecisionValidatePublishedAction(
		binding, agent, observation, cityRealtimeAgentIntentActionCaseReview,
		map[string]any{"case_code": "law.0123456789abcdef.1", "outcome": "dismiss"},
	), ErrCityRealtimeAgentDecisionUnavailable)
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
