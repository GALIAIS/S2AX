package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	cityRealtimeAgentRuntimeSchemaVersion = 1
	cityRealtimeAgentCorePolicyID         = "city-realtime-agent-core"
	// New worlds bind 1.3.0. Each historical policy remains an explicit
	// compatibility branch: a world pin commits both its authorization hashes
	// and its canonical decision-state shape, so an upgraded binary must never
	// reinterpret an old binding as the current policy.
	cityRealtimeAgentCorePolicyVersion         = "1.3.0"
	cityRealtimeAgentCorePolicyVersionAutonomy = "1.2.0"
	cityRealtimeAgentCorePolicyVersionDecision = "1.1.0"
	cityRealtimeAgentCorePolicyVersionLegacy   = "1.0.0"
	cityRealtimeAgentBindingVersion            = "city-realtime-agent-binding-v1"
	cityRealtimeAgentDefinitionVersion         = "city-realtime-agent-definition-v1"
	cityRealtimeAgentAuthorizationVersion      = "city-realtime-agent-authorization-v1"

	cityRealtimeAgentRootCode       = "system.root"
	cityRealtimeAgentNPCManagerCode = "system.npc-manager"
)

// cityRealtimeAgentPolicyBinding is an immutable, model-agnostic policy pin.
// It intentionally carries only content-addressed policy identity. Provider
// routes, prompts, private memories, and model output do not belong to the
// realtime canonical state.
type cityRealtimeAgentPolicyBinding struct {
	PolicyID      string `json:"policy_id"`
	PolicyVersion string `json:"policy_version"`
	PolicyHash    string `json:"policy_hash"`
	BindingHash   string `json:"binding_hash"`
}

// cityRealtimeAgentInstance is the bounded state needed to verify the Agent
// tree during canonical hashing. The append-only lifecycle ledger remains in
// a separate table; EventChainHash points to its verified terminal event.
type cityRealtimeAgentInstance struct {
	AgentCode            string  `json:"agent_code"`
	AgentKind            string  `json:"agent_kind"`
	AgentSubtype         string  `json:"agent_subtype"`
	ParentAgentCode      *string `json:"parent_agent_code"`
	ActorCode            *string `json:"actor_code"`
	OwnerUserID          *int64  `json:"owner_user_id"`
	LifecycleStatus      string  `json:"lifecycle_status"`
	ControlMode          string  `json:"control_mode"`
	DefinitionCode       string  `json:"definition_code"`
	DefinitionVersion    string  `json:"definition_version"`
	DefinitionHash       string  `json:"definition_hash"`
	AuthorizationHash    string  `json:"authorization_hash"`
	LifecycleRevision    int64   `json:"lifecycle_revision"`
	SpawnedFrameSequence int64   `json:"spawned_frame_sequence"`
	LastFrameSequence    int64   `json:"last_frame_sequence"`
	InstanceHash         string  `json:"instance_hash"`
	EventChainHash       string  `json:"event_chain_hash"`
}

type cityRealtimeAgentLifecycleEvent struct {
	AgentCode         string
	EventSequence     int64
	FrameSequence     int64
	EventType         string
	FromStatus        *string
	ToStatus          string
	ControlMode       string
	ReasonCode        string
	PreviousEventHash *string
	EventHash         string
}

// cityRealtimeAgentHashState is omitted for worlds created before the Agent
// foundation migration. That preserves their historical canonical state.
// Worlds created after the foundation binds one policy and a deterministic
// minimum tree of root, NPC manager, and NPC character agents.
type cityRealtimeAgentHashState struct {
	SchemaVersion int                                 `json:"schema_version"`
	Binding       *cityRealtimeAgentPolicyBinding     `json:"binding,omitempty"`
	Agents        []cityRealtimeAgentInstance         `json:"agents"`
	Decisions     *cityRealtimeAgentDecisionHashState `json:"decisions,omitempty"`
}

func initializeCityRealtimeAgentFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	simulationVersion string,
) error {
	if tx == nil || worldID <= 0 || !cityEngineSupportsRealtimeStaticWorldgen(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}

	binding, err := loadCityRealtimeAgentCorePolicyBinding(ctx, tx)
	if err != nil {
		return err
	}
	actorCodes, err := loadCityRealtimeAgentBootstrapNPCActorCodes(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if len(actorCodes) != cityRealtimeActorBootstrapCount {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_bootstrap_npcs"})
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_agent_initialize_world_id', $1, TRUE)`,
		fmt.Sprintf("%d", worldID),
	); err != nil {
		return fmt.Errorf("activate realtime agent initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_world_bindings
    (world_id, policy_id, policy_version, policy_hash, binding_hash,
     genesis_frame_sequence, metadata)
VALUES ($1, $2, $3, $4, $5, 0, '{}'::jsonb)`,
		worldID, binding.PolicyID, binding.PolicyVersion, binding.PolicyHash, binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime agent world binding: %w", err)
	}

	instances := make([]cityRealtimeAgentInstance, 0, len(actorCodes)+2)
	root, err := newCityRealtimeAgentBootstrapInstance(
		binding,
		cityRealtimeAgentRootCode,
		"simulation",
		"system.root",
		nil,
		nil,
		nil,
		"system",
	)
	if err != nil {
		return err
	}
	instances = append(instances, root)
	managerParent := cityRealtimeAgentRootCode
	manager, err := newCityRealtimeAgentBootstrapInstance(
		binding,
		cityRealtimeAgentNPCManagerCode,
		"simulation",
		"system.npc_manager",
		&managerParent,
		nil,
		nil,
		"system",
	)
	if err != nil {
		return err
	}
	instances = append(instances, manager)
	for _, actorCode := range actorCodes {
		npcAgentCode, codeErr := cityRealtimeAgentCodeForNPC(actorCode)
		if codeErr != nil {
			return codeErr
		}
		npcParent := cityRealtimeAgentNPCManagerCode
		npcActorCode := actorCode
		npc, bootstrapErr := newCityRealtimeAgentBootstrapInstance(
			binding,
			npcAgentCode,
			"character",
			"character.npc",
			&npcParent,
			&npcActorCode,
			nil,
			"autonomous",
		)
		if bootstrapErr != nil {
			return bootstrapErr
		}
		instances = append(instances, npc)
	}
	sort.Slice(instances, func(left, right int) bool {
		return instances[left].AgentCode < instances[right].AgentCode
	})
	hashState := &cityRealtimeAgentHashState{
		SchemaVersion: cityRealtimeAgentRuntimeSchemaVersion,
		Binding:       &binding,
		Agents:        instances,
	}
	if cityRealtimeAgentDecisionRuntimeEnabled(binding) {
		hashState.Decisions = newCityRealtimeAgentDecisionHashState(binding)
	}
	if err = validateCityRealtimeAgentHashState(hashState); err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_bootstrap"}).WithCause(err)
	}

	instanceStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_realtime_agent_instances
    (world_id, agent_code, agent_kind, agent_subtype, parent_agent_code,
     actor_code, owner_user_id, lifecycle_status, control_mode,
     definition_code, definition_version, definition_hash, authorization_hash,
     lifecycle_revision, spawned_frame_sequence, last_frame_sequence,
     instance_hash, event_chain_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare realtime agent instance insert: %w", err)
	}
	defer func() { _ = instanceStatement.Close() }()
	eventStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_realtime_agent_lifecycle_events
    (world_id, agent_code, event_sequence, frame_sequence, event_type,
     from_status, to_status, control_mode, reason_code, previous_event_hash,
     event_hash, metadata)
VALUES ($1, $2, 0, 0, 'spawn', NULL, $3, $4, 'genesis.bootstrap', NULL,
        $5, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare realtime agent spawn event insert: %w", err)
	}
	defer func() { _ = eventStatement.Close() }()

	for _, instance := range instances {
		if _, err = instanceStatement.ExecContext(ctx,
			worldID, instance.AgentCode, instance.AgentKind, instance.AgentSubtype,
			instance.ParentAgentCode, instance.ActorCode, instance.OwnerUserID,
			instance.LifecycleStatus, instance.ControlMode, instance.DefinitionCode,
			instance.DefinitionVersion, instance.DefinitionHash, instance.AuthorizationHash,
			instance.LifecycleRevision, instance.SpawnedFrameSequence, instance.LastFrameSequence,
			instance.InstanceHash, instance.EventChainHash,
		); err != nil {
			return fmt.Errorf("insert realtime agent instance %s: %w", instance.AgentCode, err)
		}
		if _, err = eventStatement.ExecContext(ctx,
			worldID, instance.AgentCode, instance.LifecycleStatus, instance.ControlMode,
			instance.EventChainHash,
		); err != nil {
			return fmt.Errorf("insert realtime agent spawn event %s: %w", instance.AgentCode, err)
		}
	}
	return nil
}

func loadCityRealtimeAgentCorePolicyBinding(
	ctx context.Context,
	queryer citySQLQueryer,
) (cityRealtimeAgentPolicyBinding, error) {
	binding := cityRealtimeAgentPolicyBinding{
		PolicyID:      cityRealtimeAgentCorePolicyID,
		PolicyVersion: cityRealtimeAgentCorePolicyVersion,
	}
	var status string
	err := queryer.QueryRowContext(ctx, `
SELECT policy_hash, status
FROM city_realtime_agent_policy_bundles
WHERE policy_id = $1 AND policy_version = $2`,
		binding.PolicyID, binding.PolicyVersion,
	).Scan(&binding.PolicyHash, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeAgentPolicyBinding{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_core_policy"})
	}
	if err != nil {
		return cityRealtimeAgentPolicyBinding{}, fmt.Errorf("load realtime agent core policy: %w", err)
	}
	binding.BindingHash = cityRealtimeAgentBindingHash(binding)
	if status != "published" || !validateCityRealtimeAgentPolicyBinding(binding) {
		return cityRealtimeAgentPolicyBinding{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_core_policy"})
	}
	return binding, nil
}

func loadCityRealtimeAgentBootstrapNPCActorCodes(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code
FROM city_realtime_actor_identities
WHERE world_id = $1 AND actor_kind = 'npc' AND lifecycle_status = 'active'
ORDER BY actor_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime agent bootstrap NPC actors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	actorCodes := make([]string, 0, cityRealtimeActorBootstrapCount)
	for rows.Next() {
		var actorCode string
		if err = rows.Scan(&actorCode); err != nil {
			return nil, err
		}
		if !cityRealtimeAgentIdentifierValid(actorCode, 96) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_actor_code"})
		}
		actorCodes = append(actorCodes, actorCode)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime agent bootstrap NPC actors: %w", err)
	}
	return actorCodes, nil
}

func newCityRealtimeAgentBootstrapInstance(
	binding cityRealtimeAgentPolicyBinding,
	agentCode, agentKind, agentSubtype string,
	parentAgentCode, actorCode *string,
	ownerUserID *int64,
	controlMode string,
) (cityRealtimeAgentInstance, error) {
	return newCityRealtimeAgentSpawnInstance(
		binding, agentCode, agentKind, agentSubtype, parentAgentCode,
		actorCode, ownerUserID, controlMode, 0, "genesis.bootstrap",
	)
}

// newCityRealtimeAgentSpawnInstance builds the sealed state for both genesis
// and later character creation. The caller must write its matching spawn
// lifecycle event in the same Temporal Frame; no caller can manufacture an
// instance that lacks the hash-chain head for its birth event.
func newCityRealtimeAgentSpawnInstance(
	binding cityRealtimeAgentPolicyBinding,
	agentCode, agentKind, agentSubtype string,
	parentAgentCode, actorCode *string,
	ownerUserID *int64,
	controlMode string,
	spawnedFrameSequence int64,
	reasonCode string,
) (cityRealtimeAgentInstance, error) {
	if spawnedFrameSequence < 0 || !cityRealtimeAgentIdentifierValid(reasonCode, 64) {
		return cityRealtimeAgentInstance{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "realtime_agent_spawn"})
	}
	instance := cityRealtimeAgentInstance{
		AgentCode:            agentCode,
		AgentKind:            agentKind,
		AgentSubtype:         agentSubtype,
		ParentAgentCode:      parentAgentCode,
		ActorCode:            actorCode,
		OwnerUserID:          ownerUserID,
		LifecycleStatus:      "active",
		ControlMode:          controlMode,
		DefinitionCode:       agentSubtype,
		DefinitionVersion:    binding.PolicyVersion,
		DefinitionHash:       cityRealtimeAgentDefinitionHash(binding, agentSubtype),
		AuthorizationHash:    cityRealtimeAgentAuthorizationHash(binding, agentSubtype),
		LifecycleRevision:    1,
		SpawnedFrameSequence: spawnedFrameSequence,
		LastFrameSequence:    spawnedFrameSequence,
	}
	eventHash, err := cityRealtimeAgentLifecycleEventHash(binding, cityRealtimeAgentLifecycleEvent{
		AgentCode:     instance.AgentCode,
		EventSequence: 0,
		FrameSequence: spawnedFrameSequence,
		EventType:     "spawn",
		ToStatus:      instance.LifecycleStatus,
		ControlMode:   instance.ControlMode,
		ReasonCode:    reasonCode,
	})
	if err != nil {
		return cityRealtimeAgentInstance{}, err
	}
	instance.EventChainHash = eventHash
	instanceHash, err := cityRealtimeAgentInstanceHash(binding, instance)
	if err != nil {
		return cityRealtimeAgentInstance{}, err
	}
	instance.InstanceHash = instanceHash
	return instance, nil
}

func cityRealtimeAgentBindingHash(binding cityRealtimeAgentPolicyBinding) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeAgentBindingVersion,
		binding.PolicyID,
		binding.PolicyVersion,
		binding.PolicyHash,
	}, "\x1f")))
}

func cityRealtimeAgentDefinitionHash(binding cityRealtimeAgentPolicyBinding, definitionCode string) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeAgentDefinitionVersion,
		binding.BindingHash,
		definitionCode,
	}, "\x1f")))
}

func cityRealtimeAgentAuthorizationHash(binding cityRealtimeAgentPolicyBinding, agentSubtype string) string {
	scopes, ok := cityRealtimeAgentAuthorizationScopes(binding, agentSubtype)
	if !ok {
		return ""
	}
	parts := []string{cityRealtimeAgentAuthorizationVersion, binding.BindingHash, agentSubtype}
	parts = append(parts, scopes...)
	return cityOpenWorldPayloadHash([]byte(strings.Join(parts, "\x1f")))
}

func cityRealtimeAgentAuthorizationScopes(binding cityRealtimeAgentPolicyBinding, agentSubtype string) ([]string, bool) {
	if !cityRealtimeAgentPolicyVersionSupported(binding.PolicyVersion) {
		return nil, false
	}
	// Preserve the exact 1.0.0 authorization catalogue for existing worlds.
	// The binding already commits the version, but keeping the old scope list
	// explicit lets an upgraded binary validate historical instance hashes.
	if binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionLegacy {
		switch agentSubtype {
		case "system.root":
			return []string{"agent.lifecycle.observe", "agent.lifecycle.supervise"}, true
		case "system.npc_manager":
			return []string{"npc.lifecycle.observe", "npc.lifecycle.supervise"}, true
		case "character.npc":
			return []string{"actor.patrol.deterministic", "character.observe.public"}, true
		case "character.user":
			return []string{"character.observe.private", "character.intent.propose"}, true
		default:
			return nil, false
		}
	}
	// Policy 1.1.0 is the sealed A2 decision-runtime catalogue. Keep it
	// byte-for-byte independent from the newer Character Agent scopes so its
	// historical instance hashes still validate after an upgrade.
	if binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionDecision {
		switch agentSubtype {
		case "system.root":
			return []string{"agent.lifecycle.observe", "agent.lifecycle.supervise", "agent.decision.request"}, true
		case "system.npc_manager":
			return []string{"npc.lifecycle.observe", "npc.lifecycle.supervise", "agent.decision.request"}, true
		case "character.npc":
			return []string{"actor.patrol.deterministic", "character.observe.public", "character.intent.propose", "agent.decision.request"}, true
		case "character.user":
			return []string{"character.observe.private", "character.intent.propose", "agent.decision.request"}, true
		default:
			return nil, false
		}
	}
	// Policy 1.2.0 is the sealed A3.1 control/activity catalogue.  It must
	// remain byte-for-byte independent from A3.2 navigation/role scopes so an
	// upgraded binary continues to validate historical instance hashes.
	if binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionAutonomy {
		switch agentSubtype {
		case "system.root":
			return []string{"agent.lifecycle.observe", "agent.lifecycle.supervise", "agent.decision.request"}, true
		case "system.npc_manager":
			return []string{"npc.lifecycle.observe", "npc.lifecycle.supervise", "agent.decision.request"}, true
		case "character.npc":
			return []string{"actor.patrol.deterministic", "character.observe.public", "character.intent.propose", "agent.decision.request"}, true
		case "character.user":
			return []string{
				"character.observe.private",
				"character.intent.propose",
				"character.activity.propose",
				"character.control.configure",
				"agent.decision.request",
			}, true
		default:
			return nil, false
		}
	}
	switch agentSubtype {
	case "system.root":
		return []string{"agent.lifecycle.observe", "agent.lifecycle.supervise", "agent.decision.request"}, true
	case "system.npc_manager":
		return []string{"npc.lifecycle.observe", "npc.lifecycle.supervise", "agent.decision.request"}, true
	case "character.npc":
		return []string{"actor.patrol.deterministic", "character.observe.public", "character.intent.propose", "agent.decision.request"}, true
	case "character.user":
		return []string{
			"character.observe.private",
			"character.intent.propose",
			"character.activity.propose",
			"character.navigation.propose",
			"character.role.propose",
			"character.control.configure",
			"agent.decision.request",
		}, true
	default:
		return nil, false
	}
}

func cityRealtimeAgentInstanceHash(
	binding cityRealtimeAgentPolicyBinding,
	instance cityRealtimeAgentInstance,
) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":         cityRealtimeAgentRuntimeSchemaVersion,
		"binding_hash":           binding.BindingHash,
		"agent_code":             instance.AgentCode,
		"agent_kind":             instance.AgentKind,
		"agent_subtype":          instance.AgentSubtype,
		"parent_agent_code":      instance.ParentAgentCode,
		"actor_code":             instance.ActorCode,
		"owner_user_id":          instance.OwnerUserID,
		"lifecycle_status":       instance.LifecycleStatus,
		"control_mode":           instance.ControlMode,
		"definition_code":        instance.DefinitionCode,
		"definition_version":     instance.DefinitionVersion,
		"definition_hash":        instance.DefinitionHash,
		"authorization_hash":     instance.AuthorizationHash,
		"lifecycle_revision":     instance.LifecycleRevision,
		"spawned_frame_sequence": instance.SpawnedFrameSequence,
		"last_frame_sequence":    instance.LastFrameSequence,
		"event_chain_hash":       instance.EventChainHash,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime agent instance: %w", err)
	}
	return hash, nil
}

func cityRealtimeAgentLifecycleEventHash(
	binding cityRealtimeAgentPolicyBinding,
	event cityRealtimeAgentLifecycleEvent,
) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":      cityRealtimeAgentRuntimeSchemaVersion,
		"binding_hash":        binding.BindingHash,
		"agent_code":          event.AgentCode,
		"event_sequence":      event.EventSequence,
		"frame_sequence":      event.FrameSequence,
		"event_type":          event.EventType,
		"from_status":         event.FromStatus,
		"to_status":           event.ToStatus,
		"control_mode":        event.ControlMode,
		"reason_code":         event.ReasonCode,
		"previous_event_hash": event.PreviousEventHash,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime agent lifecycle event: %w", err)
	}
	return hash, nil
}

func loadCityRealtimeAgentHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeAgentHashState, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeAgentPolicyBinding{}
	err := queryer.QueryRowContext(ctx, `
SELECT policy_id, policy_version, policy_hash, binding_hash
FROM city_realtime_agent_world_bindings
WHERE world_id = $1`, worldID).Scan(
		&binding.PolicyID, &binding.PolicyVersion, &binding.PolicyHash, &binding.BindingHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if countErr := queryer.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM city_realtime_agent_instances WHERE world_id = $1`, worldID,
		).Scan(&count); countErr != nil {
			return nil, fmt.Errorf("check historical realtime agent state: %w", countErr)
		}
		if count != 0 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_binding"})
		}
		// Existing V2 worlds predate this foundation. Returning nil keeps their
		// prior canonical state intact instead of silently rewriting its hash.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime agent world binding: %w", err)
	}
	if !validateCityRealtimeAgentPolicyBinding(binding) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_binding"})
	}
	var storedPolicyHash, policyStatus string
	err = queryer.QueryRowContext(ctx, `
SELECT policy_hash, status
FROM city_realtime_agent_policy_bundles
WHERE policy_id = $1 AND policy_version = $2`,
		binding.PolicyID, binding.PolicyVersion,
	).Scan(&storedPolicyHash, &policyStatus)
	if errors.Is(err, sql.ErrNoRows) || policyStatus != "published" || storedPolicyHash != binding.PolicyHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_policy"})
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime agent bound policy: %w", err)
	}

	state := &cityRealtimeAgentHashState{
		SchemaVersion: cityRealtimeAgentRuntimeSchemaVersion,
		Binding:       &binding,
		Agents:        make([]cityRealtimeAgentInstance, 0),
	}
	if cityRealtimeAgentDecisionRuntimeEnabled(binding) {
		state.Decisions = newCityRealtimeAgentDecisionHashState(binding)
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT agent_code, agent_kind, agent_subtype, parent_agent_code, actor_code,
       owner_user_id, lifecycle_status, control_mode, definition_code,
       definition_version, definition_hash, authorization_hash,
       lifecycle_revision, spawned_frame_sequence, last_frame_sequence,
       instance_hash, event_chain_hash
FROM city_realtime_agent_instances
WHERE world_id = $1
ORDER BY agent_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime agent instances: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		instance := cityRealtimeAgentInstance{}
		var parentAgentCode, actorCode sql.NullString
		var ownerUserID sql.NullInt64
		if err = rows.Scan(
			&instance.AgentCode, &instance.AgentKind, &instance.AgentSubtype,
			&parentAgentCode, &actorCode, &ownerUserID, &instance.LifecycleStatus,
			&instance.ControlMode, &instance.DefinitionCode, &instance.DefinitionVersion,
			&instance.DefinitionHash, &instance.AuthorizationHash, &instance.LifecycleRevision,
			&instance.SpawnedFrameSequence, &instance.LastFrameSequence,
			&instance.InstanceHash, &instance.EventChainHash,
		); err != nil {
			return nil, err
		}
		instance.ParentAgentCode = cityRealtimeAgentNullStringPointer(parentAgentCode)
		instance.ActorCode = cityRealtimeAgentNullStringPointer(actorCode)
		instance.OwnerUserID = cityRealtimeAgentNullInt64Pointer(ownerUserID)
		state.Agents = append(state.Agents, instance)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime agent instances: %w", err)
	}
	if err = validateCityRealtimeAgentLifecycleChains(ctx, queryer, worldID, binding, state.Agents); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_lifecycle"}).WithCause(err)
	}
	if err = validateCityRealtimeAgentNPCBindings(ctx, queryer, worldID, state.Agents); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_npc_binding"}).WithCause(err)
	}
	if err = validateCityRealtimeAgentUserCharacterBindings(ctx, queryer, worldID, state.Agents); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_user_character_binding"}).WithCause(err)
	}
	if state.Decisions != nil {
		if err = loadCityRealtimeAgentDecisionHashState(ctx, queryer, worldID, binding, state.Decisions); err != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_decision_state"}).WithCause(err)
		}
	}
	if err = validateCityRealtimeAgentHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeAgentPolicyBinding(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		cityRealtimeAgentPolicyVersionSupported(binding.PolicyVersion) &&
		cityRealtimeSHA256Hex(binding.PolicyHash) &&
		cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeAgentBindingHash(binding)
}

func validateCityRealtimeAgentHashState(state *cityRealtimeAgentHashState) error {
	if state == nil || state.SchemaVersion != cityRealtimeAgentRuntimeSchemaVersion || state.Binding == nil || state.Agents == nil {
		return fmt.Errorf("invalid realtime agent hash state")
	}
	if !validateCityRealtimeAgentPolicyBinding(*state.Binding) {
		return fmt.Errorf("invalid realtime agent policy binding")
	}
	if cityRealtimeAgentDecisionRuntimeEnabled(*state.Binding) {
		if state.Decisions == nil {
			return fmt.Errorf("realtime agent decision state is required by policy")
		}
		if err := validateCityRealtimeAgentDecisionHashState(*state.Binding, *state.Decisions); err != nil {
			return err
		}
	} else if state.Decisions != nil {
		return fmt.Errorf("legacy realtime agent policy cannot include decision state")
	}
	if len(state.Agents) < 2 {
		return fmt.Errorf("realtime agent root tree is incomplete")
	}
	agentsByCode := make(map[string]cityRealtimeAgentInstance, len(state.Agents))
	rootCount := 0
	managerCount := 0
	for index, instance := range state.Agents {
		if index > 0 && state.Agents[index-1].AgentCode >= instance.AgentCode {
			return fmt.Errorf("realtime agents are not in stable canonical order")
		}
		if err := validateCityRealtimeAgentInstance(*state.Binding, instance); err != nil {
			return err
		}
		if _, exists := agentsByCode[instance.AgentCode]; exists {
			return fmt.Errorf("duplicate realtime agent code")
		}
		agentsByCode[instance.AgentCode] = instance
		switch instance.AgentCode {
		case cityRealtimeAgentRootCode:
			rootCount++
		case cityRealtimeAgentNPCManagerCode:
			managerCount++
		}
	}
	if rootCount != 1 || managerCount != 1 {
		return fmt.Errorf("realtime agent root topology is incomplete")
	}
	root := agentsByCode[cityRealtimeAgentRootCode]
	if root.AgentSubtype != "system.root" || root.ParentAgentCode != nil {
		return fmt.Errorf("invalid realtime agent root")
	}
	manager := agentsByCode[cityRealtimeAgentNPCManagerCode]
	if manager.AgentSubtype != "system.npc_manager" ||
		manager.ParentAgentCode == nil || *manager.ParentAgentCode != cityRealtimeAgentRootCode {
		return fmt.Errorf("invalid realtime NPC manager")
	}
	for _, instance := range state.Agents {
		if instance.ParentAgentCode != nil {
			if _, exists := agentsByCode[*instance.ParentAgentCode]; !exists {
				return fmt.Errorf("realtime agent parent is absent")
			}
		}
		if instance.AgentSubtype == "character.npc" &&
			(instance.ParentAgentCode == nil || *instance.ParentAgentCode != cityRealtimeAgentNPCManagerCode) {
			return fmt.Errorf("realtime NPC agent has invalid manager parent")
		}
	}
	return nil
}

func validateCityRealtimeAgentInstance(
	binding cityRealtimeAgentPolicyBinding,
	instance cityRealtimeAgentInstance,
) error {
	if !cityRealtimeAgentIdentifierValid(instance.AgentCode, 96) ||
		!cityRealtimeAgentIdentifierValid(instance.AgentSubtype, 96) ||
		!cityRealtimeAgentIdentifierValid(instance.DefinitionCode, 96) ||
		!cityRealtimeAgentVersionValid(instance.DefinitionVersion) ||
		!cityRealtimeSHA256Hex(instance.DefinitionHash) ||
		!cityRealtimeSHA256Hex(instance.AuthorizationHash) ||
		!cityRealtimeSHA256Hex(instance.InstanceHash) ||
		!cityRealtimeSHA256Hex(instance.EventChainHash) ||
		instance.LifecycleRevision <= 0 || instance.SpawnedFrameSequence < 0 ||
		instance.LastFrameSequence < instance.SpawnedFrameSequence ||
		!cityRealtimeAgentLifecycleStatusValid(instance.LifecycleStatus) ||
		!cityRealtimeAgentControlModeValid(instance.ControlMode) {
		return fmt.Errorf("invalid realtime agent instance")
	}
	if instance.DefinitionCode != instance.AgentSubtype ||
		instance.DefinitionVersion != binding.PolicyVersion ||
		instance.DefinitionHash != cityRealtimeAgentDefinitionHash(binding, instance.DefinitionCode) ||
		instance.AuthorizationHash != cityRealtimeAgentAuthorizationHash(binding, instance.AgentSubtype) {
		return fmt.Errorf("realtime agent definition or authorization hash mismatch")
	}
	switch instance.AgentSubtype {
	case "system.root":
		if instance.AgentKind != "simulation" || instance.AgentCode != cityRealtimeAgentRootCode ||
			instance.ParentAgentCode != nil || instance.ActorCode != nil || instance.OwnerUserID != nil ||
			instance.ControlMode != "system" {
			return fmt.Errorf("invalid realtime system root")
		}
	case "system.npc_manager":
		if instance.AgentKind != "simulation" || instance.AgentCode != cityRealtimeAgentNPCManagerCode ||
			instance.ParentAgentCode == nil || *instance.ParentAgentCode != cityRealtimeAgentRootCode ||
			instance.ActorCode != nil || instance.OwnerUserID != nil || instance.ControlMode != "system" {
			return fmt.Errorf("invalid realtime NPC manager")
		}
	case "character.npc":
		if instance.AgentKind != "character" || instance.ParentAgentCode == nil ||
			*instance.ParentAgentCode != cityRealtimeAgentNPCManagerCode || instance.ActorCode == nil ||
			instance.OwnerUserID != nil || (instance.ControlMode != "autonomous" && instance.ControlMode != "suspended") {
			return fmt.Errorf("invalid realtime NPC character agent")
		}
		expectedCode, err := cityRealtimeAgentCodeForNPC(*instance.ActorCode)
		if err != nil || instance.AgentCode != expectedCode {
			return fmt.Errorf("invalid realtime NPC character agent code")
		}
	case "character.user":
		if instance.AgentKind != "character" || instance.ParentAgentCode != nil ||
			instance.ActorCode == nil || instance.OwnerUserID == nil || *instance.OwnerUserID <= 0 ||
			(instance.ControlMode != "autonomous" && instance.ControlMode != "assisted" &&
				instance.ControlMode != "manual" && instance.ControlMode != "suspended") {
			return fmt.Errorf("invalid realtime user character agent")
		}
		expectedCode, err := cityRealtimeAgentCodeForUserCharacter(*instance.ActorCode)
		if err != nil || instance.AgentCode != expectedCode {
			return fmt.Errorf("invalid realtime user character agent code")
		}
	default:
		return fmt.Errorf("unsupported realtime agent subtype")
	}
	expectedHash, err := cityRealtimeAgentInstanceHash(binding, instance)
	if err != nil || expectedHash != instance.InstanceHash {
		return fmt.Errorf("realtime agent instance hash mismatch")
	}
	return nil
}

func validateCityRealtimeAgentLifecycleChains(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	binding cityRealtimeAgentPolicyBinding,
	instances []cityRealtimeAgentInstance,
) error {
	eventsByAgent := make(map[string][]cityRealtimeAgentLifecycleEvent, len(instances))
	rows, err := queryer.QueryContext(ctx, `
SELECT agent_code, event_sequence, frame_sequence, event_type, from_status,
       to_status, control_mode, reason_code, previous_event_hash, event_hash
FROM city_realtime_agent_lifecycle_events
WHERE world_id = $1
ORDER BY agent_code ASC, event_sequence ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load realtime agent lifecycle events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		event := cityRealtimeAgentLifecycleEvent{}
		var fromStatus, previousHash sql.NullString
		if err = rows.Scan(
			&event.AgentCode, &event.EventSequence, &event.FrameSequence, &event.EventType,
			&fromStatus, &event.ToStatus, &event.ControlMode, &event.ReasonCode,
			&previousHash, &event.EventHash,
		); err != nil {
			return err
		}
		event.FromStatus = cityRealtimeAgentNullStringPointer(fromStatus)
		event.PreviousEventHash = cityRealtimeAgentNullStringPointer(previousHash)
		eventsByAgent[event.AgentCode] = append(eventsByAgent[event.AgentCode], event)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate realtime agent lifecycle events: %w", err)
	}
	for _, instance := range instances {
		if err = validateCityRealtimeAgentLifecycleChain(binding, instance, eventsByAgent[instance.AgentCode]); err != nil {
			return err
		}
	}
	for agentCode := range eventsByAgent {
		found := false
		for _, instance := range instances {
			if instance.AgentCode == agentCode {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("realtime agent lifecycle event has no instance")
		}
	}
	return nil
}

func validateCityRealtimeAgentLifecycleChain(
	binding cityRealtimeAgentPolicyBinding,
	instance cityRealtimeAgentInstance,
	events []cityRealtimeAgentLifecycleEvent,
) error {
	if len(events) == 0 || int64(len(events)) != instance.LifecycleRevision {
		return fmt.Errorf("realtime agent lifecycle revision is inconsistent")
	}
	var previous *cityRealtimeAgentLifecycleEvent
	for index := range events {
		event := &events[index]
		if event.AgentCode != instance.AgentCode || event.EventSequence != int64(index) ||
			event.FrameSequence < instance.SpawnedFrameSequence ||
			!cityRealtimeAgentIdentifierValid(event.ReasonCode, 64) ||
			!cityRealtimeAgentLifecycleStatusValid(event.ToStatus) ||
			!cityRealtimeAgentControlModeValid(event.ControlMode) || !cityRealtimeSHA256Hex(event.EventHash) {
			return fmt.Errorf("invalid realtime agent lifecycle event")
		}
		if index == 0 {
			if event.EventType != "spawn" || event.EventSequence != 0 || event.FrameSequence != instance.SpawnedFrameSequence ||
				event.FromStatus != nil || event.PreviousEventHash != nil {
				return fmt.Errorf("invalid realtime agent spawn event")
			}
		} else {
			if event.FromStatus == nil || previous == nil || *event.FromStatus != previous.ToStatus ||
				event.PreviousEventHash == nil || *event.PreviousEventHash != previous.EventHash ||
				event.FrameSequence <= previous.FrameSequence {
				return fmt.Errorf("invalid realtime agent lifecycle transition")
			}
			switch event.EventType {
			case "lifecycle":
				if !cityRealtimeAgentLifecycleTransitionAllowed(previous.ToStatus, event.ToStatus) {
					return fmt.Errorf("invalid realtime agent lifecycle transition")
				}
			case "control":
				if !cityRealtimeAgentCharacterControlRuntimeEnabled(binding) || event.ToStatus != previous.ToStatus ||
					event.ControlMode == previous.ControlMode {
					return fmt.Errorf("invalid realtime agent control transition")
				}
			default:
				return fmt.Errorf("invalid realtime agent lifecycle event type")
			}
		}
		expectedHash, err := cityRealtimeAgentLifecycleEventHash(binding, *event)
		if err != nil || expectedHash != event.EventHash {
			return fmt.Errorf("realtime agent lifecycle event hash mismatch")
		}
		previous = event
	}
	last := events[len(events)-1]
	if last.ToStatus != instance.LifecycleStatus || last.ControlMode != instance.ControlMode ||
		last.FrameSequence != instance.LastFrameSequence || last.EventHash != instance.EventChainHash {
		return fmt.Errorf("realtime agent lifecycle chain head mismatch")
	}
	return nil
}

func validateCityRealtimeAgentNPCBindings(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	agents []cityRealtimeAgentInstance,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code
FROM city_realtime_actor_identities
WHERE world_id = $1 AND actor_kind = 'npc' AND lifecycle_status = 'active'
ORDER BY actor_code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load realtime NPC actor bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	activeActors := make(map[string]struct{}, cityRealtimeActorBootstrapCount)
	for rows.Next() {
		var actorCode string
		if err = rows.Scan(&actorCode); err != nil {
			return err
		}
		activeActors[actorCode] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate realtime NPC actor bindings: %w", err)
	}
	npcAgents := make(map[string]struct{}, len(activeActors))
	for _, agent := range agents {
		if agent.AgentSubtype != "character.npc" {
			continue
		}
		if agent.ActorCode == nil {
			return fmt.Errorf("NPC agent has no actor")
		}
		if _, duplicate := npcAgents[*agent.ActorCode]; duplicate {
			return fmt.Errorf("multiple realtime NPC agents bind one actor")
		}
		npcAgents[*agent.ActorCode] = struct{}{}
	}
	for actorCode := range activeActors {
		if _, exists := npcAgents[actorCode]; !exists {
			return fmt.Errorf("active realtime NPC actor has no agent")
		}
	}
	for actorCode := range npcAgents {
		if _, exists := activeActors[actorCode]; !exists {
			return fmt.Errorf("realtime NPC agent references inactive actor")
		}
	}
	return nil
}

// validateCityRealtimeAgentUserCharacterBindings closes the other half of
// the Character Agent invariant. An Actor identity cannot be treated as a
// user-controlled character unless exactly one sealed character.user Agent
// owns it; conversely, a character.user Agent may not point at a synthetic or
// missing Actor. The actor code stays opaque and this check deliberately does
// not load user account attributes.
func validateCityRealtimeAgentUserCharacterBindings(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	agents []cityRealtimeAgentInstance,
) error {
	userAgentsByActor := make(map[string]struct{})
	for _, agent := range agents {
		if agent.AgentSubtype != "character.user" {
			continue
		}
		if agent.ActorCode == nil || agent.OwnerUserID == nil || *agent.OwnerUserID <= 0 {
			return fmt.Errorf("user character agent has no sealed actor owner")
		}
		if _, duplicate := userAgentsByActor[*agent.ActorCode]; duplicate {
			return fmt.Errorf("multiple realtime user character agents bind one actor")
		}
		userAgentsByActor[*agent.ActorCode] = struct{}{}
	}

	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code
FROM city_realtime_actor_identities
WHERE world_id = $1 AND actor_kind = 'character'
ORDER BY actor_code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load realtime user character actor bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	actorsByCode := make(map[string]struct{}, len(userAgentsByActor))
	for rows.Next() {
		var actorCode string
		if err = rows.Scan(&actorCode); err != nil {
			return err
		}
		if _, duplicate := actorsByCode[actorCode]; duplicate {
			return fmt.Errorf("duplicate realtime character actor identity")
		}
		actorsByCode[actorCode] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate realtime user character actor bindings: %w", err)
	}
	for actorCode := range actorsByCode {
		if _, exists := userAgentsByActor[actorCode]; !exists {
			return fmt.Errorf("realtime character actor has no user agent")
		}
	}
	for actorCode := range userAgentsByActor {
		if _, exists := actorsByCode[actorCode]; !exists {
			return fmt.Errorf("realtime user character agent references missing actor")
		}
	}
	return nil
}

// cityRealtimeNPCAgentCanPatrol is the first runtime bridge from the sealed
// Agent tree to an existing deterministic NPC behavior. It does not ask a
// model for a route and it does not mutate Agent state. A world created before
// the Agent foundation retains its historical patrol behavior; a bound world
// requires the NPC, its manager, and the root to remain active before the
// server-owned patrol reducer may move the Actor.
func cityRealtimeNPCAgentCanPatrol(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
) (bool, error) {
	var bound bool
	if err := queryer.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM city_realtime_agent_world_bindings
    WHERE world_id = $1
)`, worldID).Scan(&bound); err != nil {
		return false, fmt.Errorf("check realtime agent world binding: %w", err)
	}
	if !bound {
		return true, nil
	}
	var npcStatus, npcControlMode, managerStatus, rootStatus string
	err := queryer.QueryRowContext(ctx, `
SELECT npc.lifecycle_status, npc.control_mode,
       manager.lifecycle_status, root.lifecycle_status
FROM city_realtime_agent_instances npc
JOIN city_realtime_agent_instances manager
  ON manager.world_id = npc.world_id
 AND manager.agent_code = npc.parent_agent_code
JOIN city_realtime_agent_instances root
  ON root.world_id = manager.world_id
 AND root.agent_code = manager.parent_agent_code
WHERE npc.world_id = $1
  AND npc.actor_code = $2
  AND npc.agent_subtype = 'character.npc'
  AND manager.agent_subtype = 'system.npc_manager'
  AND root.agent_subtype = 'system.root'`, worldID, actorCode).Scan(
		&npcStatus, &npcControlMode, &managerStatus, &rootStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_npc_agent"})
	}
	if err != nil {
		return false, fmt.Errorf("load realtime NPC agent patrol state: %w", err)
	}
	return npcStatus == "active" && npcControlMode == "autonomous" &&
		managerStatus == "active" && rootStatus == "active", nil
}

func cityRealtimeAgentCodeForNPC(actorCode string) (string, error) {
	if !cityRealtimeAgentIdentifierValid(actorCode, 88) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_actor_code"})
	}
	code := "agent." + actorCode
	if !cityRealtimeAgentIdentifierValid(code, 96) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_code"})
	}
	return code, nil
}

// Player Actor codes are random server-generated handles rather than a
// derivation of the platform user ID. The shared actor projection can safely
// show this simulation identifier without disclosing account identity.
func cityRealtimeAgentCodeForUserCharacter(actorCode string) (string, error) {
	const prefix = "character.player."
	if !strings.HasPrefix(actorCode, prefix) || len(actorCode) != len(prefix)+32 ||
		!cityRealtimeAgentIdentifierValid(actorCode, 96) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_user_character_actor_code"})
	}
	for _, character := range actorCode[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_user_character_actor_code"})
		}
	}
	code := "agent." + actorCode
	if !cityRealtimeAgentIdentifierValid(code, 96) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_user_character_agent_code"})
	}
	return code, nil
}

func cityRealtimeAgentIdentifierValid(value string, maximumLength int) bool {
	if len(value) < 2 || len(value) > maximumLength || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			character == '_' || character == '.' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func cityRealtimeAgentVersionValid(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 8 {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func cityRealtimeAgentLifecycleStatusValid(status string) bool {
	switch status {
	case "draft", "active", "waiting", "suspended", "degraded", "retiring", "terminated":
		return true
	default:
		return false
	}
}

func cityRealtimeAgentControlModeValid(controlMode string) bool {
	switch controlMode {
	case "system", "autonomous", "assisted", "manual", "suspended":
		return true
	default:
		return false
	}
}

func cityRealtimeAgentLifecycleTransitionAllowed(fromStatus, toStatus string) bool {
	switch fromStatus {
	case "draft":
		return toStatus == "active" || toStatus == "suspended" || toStatus == "terminated"
	case "active":
		return toStatus == "waiting" || toStatus == "suspended" || toStatus == "degraded" || toStatus == "retiring"
	case "waiting":
		return toStatus == "active" || toStatus == "suspended" || toStatus == "degraded"
	case "suspended":
		return toStatus == "active" || toStatus == "retiring"
	case "degraded":
		return toStatus == "active" || toStatus == "suspended" || toStatus == "retiring"
	case "retiring":
		return toStatus == "terminated"
	default:
		return false
	}
}

func cityRealtimeAgentNullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func cityRealtimeAgentNullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
