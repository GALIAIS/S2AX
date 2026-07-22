package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentBootstrapTreeIsDeterministicAndAccountBlind(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	root, err := newCityRealtimeAgentBootstrapInstance(
		binding, cityRealtimeAgentRootCode, "simulation", "system.root", nil, nil, nil, "system",
	)
	require.NoError(t, err)
	rootCode := cityRealtimeAgentRootCode
	manager, err := newCityRealtimeAgentBootstrapInstance(
		binding, cityRealtimeAgentNPCManagerCode, "simulation", "system.npc_manager", &rootCode, nil, nil, "system",
	)
	require.NoError(t, err)
	actorCode := "npc.resident.01"
	npcCode, err := cityRealtimeAgentCodeForNPC(actorCode)
	require.NoError(t, err)
	managerCode := cityRealtimeAgentNPCManagerCode
	npc, err := newCityRealtimeAgentBootstrapInstance(
		binding, npcCode, "character", "character.npc", &managerCode, &actorCode, nil, "autonomous",
	)
	require.NoError(t, err)

	state := &cityRealtimeAgentHashState{
		SchemaVersion: cityRealtimeAgentRuntimeSchemaVersion,
		Binding:       &binding,
		Agents:        []cityRealtimeAgentInstance{npc, manager, root},
	}
	state.Decisions = newCityRealtimeAgentDecisionHashState(binding)
	// The instance order follows the database reader's stable agent_code order.
	require.NoError(t, validateCityRealtimeAgentHashState(state))

	raw, err := json.Marshal(state)
	require.NoError(t, err)
	for _, forbidden := range []string{"email", "username", "prompt", "provider", "memory", "response"} {
		require.NotContains(t, strings.ToLower(string(raw)), forbidden)
	}

	state.Agents[2].EventChainHash = strings.Repeat("0", 64)
	require.Error(t, validateCityRealtimeAgentHashState(state))
}

func TestCityRealtimeAgentLifecycleChainRejectsTampering(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	root, err := newCityRealtimeAgentBootstrapInstance(
		binding, cityRealtimeAgentRootCode, "simulation", "system.root", nil, nil, nil, "system",
	)
	require.NoError(t, err)
	spawn := cityRealtimeAgentLifecycleEvent{
		AgentCode: root.AgentCode, EventSequence: 0, FrameSequence: 0,
		EventType: "spawn", ToStatus: "active", ControlMode: "system", ReasonCode: "genesis.bootstrap",
		EventHash: root.EventChainHash,
	}
	fromActive := "active"
	previousHash := spawn.EventHash
	suspend := cityRealtimeAgentLifecycleEvent{
		AgentCode: root.AgentCode, EventSequence: 1, FrameSequence: 4,
		EventType: "lifecycle", FromStatus: &fromActive, ToStatus: "suspended", ControlMode: "system",
		ReasonCode: "policy.pause", PreviousEventHash: &previousHash,
	}
	suspend.EventHash, err = cityRealtimeAgentLifecycleEventHash(binding, suspend)
	require.NoError(t, err)
	root.LifecycleStatus = suspend.ToStatus
	root.LifecycleRevision = 2
	root.LastFrameSequence = suspend.FrameSequence
	root.EventChainHash = suspend.EventHash
	root.InstanceHash, err = cityRealtimeAgentInstanceHash(binding, root)
	require.NoError(t, err)

	require.NoError(t, validateCityRealtimeAgentLifecycleChain(binding, root, []cityRealtimeAgentLifecycleEvent{spawn, suspend}))
	suspend.PreviousEventHash = nil
	require.Error(t, validateCityRealtimeAgentLifecycleChain(binding, root, []cityRealtimeAgentLifecycleEvent{spawn, suspend}))
}

func TestCityRealtimeAgentCodeForNPCIsBounded(t *testing.T) {
	code, err := cityRealtimeAgentCodeForNPC("npc.resident.01")
	require.NoError(t, err)
	require.Equal(t, "agent.npc.resident.01", code)
	_, err = cityRealtimeAgentCodeForNPC("npc:" + strings.Repeat("x", 90))
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func TestCityRealtimeAgentUserCharacterSpawnBindsOpaquePlayerActor(t *testing.T) {
	binding := cityRealtimeAgentTestBinding()
	actorCode := "character.player.0123456789abcdef0123456789abcdef"
	agentCode, err := cityRealtimeAgentCodeForUserCharacter(actorCode)
	require.NoError(t, err)
	ownerUserID := int64(42)
	actorCodePointer := actorCode
	instance, err := newCityRealtimeAgentSpawnInstance(
		binding, agentCode, "character", "character.user", nil, &actorCodePointer,
		&ownerUserID, "manual", 7, "character.create",
	)
	require.NoError(t, err)
	require.Equal(t, int64(7), instance.SpawnedFrameSequence)
	require.Equal(t, int64(7), instance.LastFrameSequence)
	require.NoError(t, validateCityRealtimeAgentInstance(binding, instance))

	_, err = cityRealtimeAgentCodeForUserCharacter("character.player.not-a-valid-random-handle")
	require.Error(t, err)
}

func cityRealtimeAgentTestBinding() cityRealtimeAgentPolicyBinding {
	binding := cityRealtimeAgentPolicyBinding{
		PolicyID:      cityRealtimeAgentCorePolicyID,
		PolicyVersion: cityRealtimeAgentCorePolicyVersion,
		PolicyHash:    strings.Repeat("a", 64),
	}
	binding.BindingHash = cityRealtimeAgentBindingHash(binding)
	return binding
}
