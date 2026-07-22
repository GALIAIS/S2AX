package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	cityRealtimeCharacterSocialSchemaVersion   = 1
	cityRealtimeCharacterSocialBindingVersion  = "city-realtime-character-social-binding-v1"
	cityRealtimeCharacterSocialStateVersion    = "city-realtime-character-social-state-v1"
	cityRealtimeCharacterSocialChainVersion    = "city-realtime-character-social-chain-v1"
	cityRealtimeCharacterSocialEventVersion    = "city-realtime-character-social-event-v1"
	cityRealtimeCharacterSocialGreeted         = "greeted"
	cityRealtimeCharacterSocialCandidateCap    = 32
	cityRealtimeCharacterSocialAffinityStep    = int64(50)
	cityRealtimeCharacterSocialAffinityMaximum = int64(1000)
)

type cityRealtimeCharacterSocialBinding struct {
	SchemaVersion    int    `json:"schema_version"`
	AgentBindingHash string `json:"agent_binding_hash"`
	BindingHash      string `json:"binding_hash"`
}

type cityRealtimeCharacterSocialHead struct {
	ActorCodeLow      string `json:"actor_code_low"`
	ActorCodeHigh     string `json:"actor_code_high"`
	RelationRevision  int64  `json:"relation_revision"`
	LastFrameSequence int64  `json:"last_frame_sequence"`
	AffinityMilli     int64  `json:"affinity_milli"`
	InteractionCount  int64  `json:"interaction_count"`
	EventChainHash    string `json:"event_chain_hash"`
	StateHash         string `json:"state_hash"`
}

type cityRealtimeCharacterSocialEvent struct {
	ActorCodeLow      string
	ActorCodeHigh     string
	EventSequence     int64
	FrameSequence     int64
	InteractionCode   string
	InitiatorCode     string
	RecipientCode     string
	SourceIntentCode  string
	PreviousEventHash string
	EventHash         string
}

type cityRealtimeCharacterSocialHashState struct {
	SchemaVersion int                                 `json:"schema_version"`
	Binding       *cityRealtimeCharacterSocialBinding `json:"binding,omitempty"`
	Heads         []cityRealtimeCharacterSocialHead   `json:"heads"`
}

type cityRealtimeCharacterSocialTarget struct {
	ActorCode       string
	ActorKind       string
	LifecycleStatus string
	X               int64
	Y               int64
	Z               int32
}

func cityRealtimeCharacterSocialPair(left, right string) (string, string, error) {
	if !cityRealtimeAgentIdentifierValid(left, 96) || !cityRealtimeAgentIdentifierValid(right, 96) || left == right {
		return "", "", ErrCityInvalidInput
	}
	if left < right {
		return left, right, nil
	}
	return right, left, nil
}

func cityRealtimeCharacterSocialBindingHash(agentBindingHash string) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterSocialBindingVersion,
		agentBindingHash,
	}, "\x1f")))
}

func cityRealtimeCharacterSocialGenesisHash(actorCodeLow, actorCodeHigh string) (string, error) {
	if _, _, err := cityRealtimeCharacterSocialPair(actorCodeLow, actorCodeHigh); err != nil {
		return "", err
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterSocialChainVersion, actorCodeLow, actorCodeHigh,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterSocialHeadStateHash(head cityRealtimeCharacterSocialHead) (string, error) {
	if !cityRealtimeCharacterSocialHeadValid(head) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterSocialStateVersion,
		head.ActorCodeLow,
		head.ActorCodeHigh,
		strconv.FormatInt(head.RelationRevision, 10),
		strconv.FormatInt(head.LastFrameSequence, 10),
		strconv.FormatInt(head.AffinityMilli, 10),
		strconv.FormatInt(head.InteractionCount, 10),
		head.EventChainHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterSocialEventHash(event cityRealtimeCharacterSocialEvent) (string, error) {
	if !cityRealtimeCharacterSocialEventValid(event) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterSocialEventVersion,
		event.ActorCodeLow,
		event.ActorCodeHigh,
		strconv.FormatInt(event.EventSequence, 10),
		strconv.FormatInt(event.FrameSequence, 10),
		event.InteractionCode,
		event.InitiatorCode,
		event.RecipientCode,
		event.SourceIntentCode,
		event.PreviousEventHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterSocialBindingValid(binding cityRealtimeCharacterSocialBinding) bool {
	return binding.SchemaVersion == cityRealtimeCharacterSocialSchemaVersion &&
		cityRealtimeSHA256Hex(binding.AgentBindingHash) &&
		cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeCharacterSocialBindingHash(binding.AgentBindingHash)
}

func cityRealtimeCharacterSocialHeadValid(head cityRealtimeCharacterSocialHead) bool {
	low, high, err := cityRealtimeCharacterSocialPair(head.ActorCodeLow, head.ActorCodeHigh)
	if err != nil || low != head.ActorCodeLow || high != head.ActorCodeHigh || head.RelationRevision < 0 ||
		head.LastFrameSequence < 0 || head.AffinityMilli < 0 || head.AffinityMilli > cityRealtimeCharacterSocialAffinityMaximum ||
		head.InteractionCount < 0 || !cityRealtimeSHA256Hex(head.EventChainHash) || !cityRealtimeSHA256Hex(head.StateHash) {
		return false
	}
	expected, hashErr := cityRealtimeCharacterSocialHeadStateHashUnchecked(head)
	return hashErr == nil && expected == head.StateHash
}

func cityRealtimeCharacterSocialHeadStateHashUnchecked(head cityRealtimeCharacterSocialHead) (string, error) {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterSocialStateVersion,
		head.ActorCodeLow,
		head.ActorCodeHigh,
		strconv.FormatInt(head.RelationRevision, 10),
		strconv.FormatInt(head.LastFrameSequence, 10),
		strconv.FormatInt(head.AffinityMilli, 10),
		strconv.FormatInt(head.InteractionCount, 10),
		head.EventChainHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterSocialEventValid(event cityRealtimeCharacterSocialEvent) bool {
	low, high, err := cityRealtimeCharacterSocialPair(event.ActorCodeLow, event.ActorCodeHigh)
	return err == nil && low == event.ActorCodeLow && high == event.ActorCodeHigh && event.EventSequence > 0 &&
		event.FrameSequence > 0 && event.InteractionCode == cityRealtimeCharacterSocialGreeted &&
		(event.InitiatorCode == event.ActorCodeLow || event.InitiatorCode == event.ActorCodeHigh) &&
		(event.RecipientCode == event.ActorCodeLow || event.RecipientCode == event.ActorCodeHigh) &&
		event.InitiatorCode != event.RecipientCode && cityRealtimeAgentIdentifierValid(event.SourceIntentCode, 96) &&
		cityRealtimeSHA256Hex(event.PreviousEventHash) &&
		(event.EventHash == "" || cityRealtimeSHA256Hex(event.EventHash))
}

func newCityRealtimeCharacterSocialGenesisHead(actorCodeLow, actorCodeHigh string) (cityRealtimeCharacterSocialHead, error) {
	low, high, err := cityRealtimeCharacterSocialPair(actorCodeLow, actorCodeHigh)
	if err != nil {
		return cityRealtimeCharacterSocialHead{}, err
	}
	chainHash, err := cityRealtimeCharacterSocialGenesisHash(low, high)
	if err != nil {
		return cityRealtimeCharacterSocialHead{}, err
	}
	head := cityRealtimeCharacterSocialHead{
		ActorCodeLow: low, ActorCodeHigh: high, EventChainHash: chainHash,
	}
	head.StateHash, err = cityRealtimeCharacterSocialHeadStateHashUnchecked(head)
	if err != nil || !cityRealtimeCharacterSocialHeadValid(head) {
		if err == nil {
			err = ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_genesis"})
		}
		return cityRealtimeCharacterSocialHead{}, err
	}
	return head, nil
}

func cityRealtimeCharacterSocialGreet(
	previous cityRealtimeCharacterSocialHead,
	initiatorCode, recipientCode, sourceIntentCode string,
	frameSequence int64,
) (cityRealtimeCharacterSocialHead, cityRealtimeCharacterSocialEvent, error) {
	if !cityRealtimeCharacterSocialHeadValid(previous) || frameSequence <= previous.LastFrameSequence ||
		!cityRealtimeAgentIdentifierValid(initiatorCode, 96) || !cityRealtimeAgentIdentifierValid(recipientCode, 96) ||
		!cityRealtimeAgentIdentifierValid(sourceIntentCode, 96) {
		return cityRealtimeCharacterSocialHead{}, cityRealtimeCharacterSocialEvent{}, ErrCityInvalidInput
	}
	low, high, err := cityRealtimeCharacterSocialPair(initiatorCode, recipientCode)
	if err != nil || low != previous.ActorCodeLow || high != previous.ActorCodeHigh {
		return cityRealtimeCharacterSocialHead{}, cityRealtimeCharacterSocialEvent{}, ErrCityInvalidInput
	}
	event := cityRealtimeCharacterSocialEvent{
		ActorCodeLow: low, ActorCodeHigh: high, EventSequence: previous.RelationRevision + 1,
		FrameSequence: frameSequence, InteractionCode: cityRealtimeCharacterSocialGreeted,
		InitiatorCode: initiatorCode, RecipientCode: recipientCode, SourceIntentCode: sourceIntentCode,
		PreviousEventHash: previous.EventChainHash,
	}
	event.EventHash, err = cityRealtimeCharacterSocialEventHash(event)
	if err != nil {
		return cityRealtimeCharacterSocialHead{}, cityRealtimeCharacterSocialEvent{}, err
	}
	next := previous
	next.RelationRevision = event.EventSequence
	next.LastFrameSequence = frameSequence
	next.InteractionCount++
	next.AffinityMilli += cityRealtimeCharacterSocialAffinityStep
	if next.AffinityMilli > cityRealtimeCharacterSocialAffinityMaximum {
		next.AffinityMilli = cityRealtimeCharacterSocialAffinityMaximum
	}
	next.EventChainHash = event.EventHash
	next.StateHash, err = cityRealtimeCharacterSocialHeadStateHashUnchecked(next)
	if err != nil || !cityRealtimeCharacterSocialHeadValid(next) {
		if err == nil {
			err = ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_next"})
		}
		return cityRealtimeCharacterSocialHead{}, cityRealtimeCharacterSocialEvent{}, err
	}
	return next, event, nil
}

func initializeCityRealtimeCharacterSocialFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	simulationVersion string,
) error {
	if tx == nil || worldID <= 0 || !cityEngineSupportsRealtimeStaticWorldgen(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if agentState == nil || agentState.Binding == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_policy"})
	}
	if !cityRealtimeAgentCharacterSocialRuntimeEnabled(*agentState.Binding) {
		return nil
	}
	binding := cityRealtimeCharacterSocialBinding{
		SchemaVersion:    cityRealtimeCharacterSocialSchemaVersion,
		AgentBindingHash: agentState.Binding.BindingHash,
	}
	binding.BindingHash = cityRealtimeCharacterSocialBindingHash(binding.AgentBindingHash)
	if !cityRealtimeCharacterSocialBindingValid(binding) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_binding"})
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_character_social_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("activate realtime character social initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_social_world_bindings
    (world_id, schema_version, agent_binding_hash, binding_hash, metadata)
VALUES ($1, $2, $3, $4, '{}'::jsonb)`,
		worldID, binding.SchemaVersion, binding.AgentBindingHash, binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime character social binding: %w", err)
	}
	return nil
}

func enableCityRealtimeCharacterSocialMutationGate(ctx context.Context, tx *sql.Tx, worldID, frameSequence int64) error {
	if tx == nil || worldID <= 0 || frameSequence <= 0 {
		return ErrCityInvalidInput
	}
	for _, setting := range []struct {
		name  string
		value int64
	}{
		{name: "sub2api.city_realtime_character_social_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_social_frame_sequence", value: frameSequence},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character social gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func loadCityRealtimeCharacterSocialBinding(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterSocialBinding, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterSocialBinding{}
	var policyID, policyVersion, agentBindingHash string
	err := queryer.QueryRowContext(ctx, `
SELECT social.schema_version, social.agent_binding_hash, social.binding_hash,
       agent.policy_id, agent.policy_version, agent.binding_hash
FROM city_realtime_character_social_world_bindings social
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = social.world_id
WHERE social.world_id = $1`, worldID).Scan(
		&binding.SchemaVersion, &binding.AgentBindingHash, &binding.BindingHash,
		&policyID, &policyVersion, &agentBindingHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character social binding: %w", err)
	}
	if policyID != cityRealtimeAgentCorePolicyID ||
		(policyVersion != cityRealtimeAgentCorePolicyVersionSocial &&
			policyVersion != cityRealtimeAgentCorePolicyVersionReview) ||
		binding.AgentBindingHash != agentBindingHash || !cityRealtimeCharacterSocialBindingValid(binding) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_binding"})
	}
	return &binding, nil
}

func loadCityRealtimeCharacterSocialHead(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCodeLow, actorCodeHigh string,
	forUpdate bool,
) (cityRealtimeCharacterSocialHead, bool, error) {
	low, high, err := cityRealtimeCharacterSocialPair(actorCodeLow, actorCodeHigh)
	if err != nil || low != actorCodeLow || high != actorCodeHigh {
		return cityRealtimeCharacterSocialHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT actor_code_low, actor_code_high, relation_revision, last_frame_sequence,
       affinity_milli, interaction_count, event_chain_hash, state_hash
FROM city_realtime_character_social_heads
WHERE world_id = $1 AND actor_code_low = $2 AND actor_code_high = $3`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head := cityRealtimeCharacterSocialHead{}
	err = queryer.QueryRowContext(ctx, query, worldID, low, high).Scan(
		&head.ActorCodeLow, &head.ActorCodeHigh, &head.RelationRevision, &head.LastFrameSequence,
		&head.AffinityMilli, &head.InteractionCount, &head.EventChainHash, &head.StateHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterSocialHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterSocialHead{}, false, fmt.Errorf("load realtime character social head: %w", err)
	}
	if !cityRealtimeCharacterSocialHeadValid(head) {
		return cityRealtimeCharacterSocialHead{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_head"})
	}
	return head, true, nil
}

func validateCityRealtimeCharacterSocialHeadHistory(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterSocialHead,
) error {
	if worldID <= 0 || !cityRealtimeCharacterSocialHeadValid(head) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_history"})
	}
	genesisHash, err := cityRealtimeCharacterSocialGenesisHash(head.ActorCodeLow, head.ActorCodeHigh)
	if err != nil {
		return err
	}
	if head.RelationRevision == 0 {
		if head.LastFrameSequence != 0 || head.InteractionCount != 0 || head.AffinityMilli != 0 || head.EventChainHash != genesisHash {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_genesis"})
		}
		return nil
	}
	var eventSequence, frameSequence int64
	var eventHash string
	err = queryer.QueryRowContext(ctx, `
SELECT event_sequence, frame_sequence, event_hash
FROM city_realtime_character_social_events
WHERE world_id = $1 AND actor_code_low = $2 AND actor_code_high = $3
ORDER BY event_sequence DESC
LIMIT 1`, worldID, head.ActorCodeLow, head.ActorCodeHigh).Scan(&eventSequence, &frameSequence, &eventHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_event_head"})
	}
	if err != nil {
		return fmt.Errorf("load realtime character social event head: %w", err)
	}
	if eventSequence != head.RelationRevision || frameSequence != head.LastFrameSequence || eventHash != head.EventChainHash {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_event_head"})
	}
	return nil
}

func loadCityRealtimeCharacterSocialHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterSocialHashState, error) {
	binding, err := loadCityRealtimeCharacterSocialBinding(ctx, queryer, worldID)
	if err != nil || binding == nil {
		return nil, err
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code_low, actor_code_high, relation_revision, last_frame_sequence,
       affinity_milli, interaction_count, event_chain_hash, state_hash
FROM city_realtime_character_social_heads
WHERE world_id = $1
ORDER BY actor_code_low ASC, actor_code_high ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime character social heads: %w", err)
	}
	heads := make([]cityRealtimeCharacterSocialHead, 0)
	for rows.Next() {
		head := cityRealtimeCharacterSocialHead{}
		if err = rows.Scan(&head.ActorCodeLow, &head.ActorCodeHigh, &head.RelationRevision, &head.LastFrameSequence,
			&head.AffinityMilli, &head.InteractionCount, &head.EventChainHash, &head.StateHash); err != nil {
			_ = rows.Close()
			return nil, err
		}
		heads = append(heads, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character social heads: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character social heads: %w", err)
	}
	state := &cityRealtimeCharacterSocialHashState{
		SchemaVersion: cityRealtimeCharacterSocialSchemaVersion,
		Binding:       binding,
		Heads:         heads,
	}
	for _, head := range heads {
		if err = validateCityRealtimeCharacterSocialHeadHistory(ctx, queryer, worldID, head); err != nil {
			return nil, err
		}
	}
	if err = validateCityRealtimeCharacterSocialHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeCharacterSocialHashState(state *cityRealtimeCharacterSocialHashState) error {
	if state == nil || state.SchemaVersion != cityRealtimeCharacterSocialSchemaVersion || state.Binding == nil ||
		state.Heads == nil || !cityRealtimeCharacterSocialBindingValid(*state.Binding) {
		return errors.New("invalid realtime character social hash state")
	}
	for index, head := range state.Heads {
		if !cityRealtimeCharacterSocialHeadValid(head) || (index > 0 &&
			(state.Heads[index-1].ActorCodeLow > head.ActorCodeLow ||
				(state.Heads[index-1].ActorCodeLow == head.ActorCodeLow && state.Heads[index-1].ActorCodeHigh >= head.ActorCodeHigh))) {
			return errors.New("invalid or unordered realtime character social heads")
		}
	}
	return nil
}

func cityRealtimeAgentDecisionSocialTargetCodeFromArguments(arguments map[string]any) (string, error) {
	if len(arguments) != 1 {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	rawCode, exists := arguments["target_actor_code"]
	code, ok := rawCode.(string)
	code = strings.TrimSpace(code)
	if !exists || !ok || !cityRealtimeAgentIdentifierValid(code, 96) {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	return code, nil
}

func cityRealtimeAgentDecisionSocialTargetCodeFromRawArguments(arguments []byte) (string, error) {
	decoded := make(map[string]any)
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_arguments"}).WithCause(err)
	}
	return cityRealtimeAgentDecisionSocialTargetCodeFromArguments(decoded)
}

func cityRealtimeCharacterSocialTargetAllowed(current cityRealtimeActorState, target cityRealtimeCharacterSocialTarget) bool {
	if target.ActorCode == current.ActorCode || target.LifecycleStatus != "active" ||
		(target.ActorKind != "npc" && target.ActorKind != "character") || current.Z != target.Z {
		return false
	}
	if current.X > target.X && current.X-target.X == 1 || target.X > current.X && target.X-current.X == 1 {
		return current.Y == target.Y
	}
	if current.Y > target.Y && current.Y-target.Y == 1 || target.Y > current.Y && target.Y-current.Y == 1 {
		return current.X == target.X
	}
	return false
}

func loadCityRealtimeCharacterSocialTarget(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	forUpdate bool,
) (cityRealtimeCharacterSocialTarget, bool, error) {
	if worldID <= 0 || !cityRealtimeAgentIdentifierValid(actorCode, 96) {
		return cityRealtimeCharacterSocialTarget{}, false, ErrCityInvalidInput
	}
	query := `
SELECT identity.actor_code, identity.actor_kind, identity.lifecycle_status,
       state.x, state.y, state.z
FROM city_realtime_actor_identities identity
JOIN city_realtime_actor_states state
  ON state.world_id = identity.world_id AND state.actor_code = identity.actor_code
WHERE identity.world_id = $1 AND identity.actor_code = $2`
	if forUpdate {
		query += " FOR UPDATE"
	}
	target := cityRealtimeCharacterSocialTarget{}
	err := queryer.QueryRowContext(ctx, query, worldID, actorCode).Scan(
		&target.ActorCode, &target.ActorKind, &target.LifecycleStatus,
		&target.X, &target.Y, &target.Z,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterSocialTarget{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterSocialTarget{}, false, fmt.Errorf("load realtime character social target: %w", err)
	}
	return target, true, nil
}

func cityRealtimeAgentDecisionAvailableSocialTargetCodes(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	current cityRealtimeActorState,
	binding cityRealtimeAgentPolicyBinding,
) ([]string, error) {
	if worldID <= 0 || !cityRealtimeAgentIdentifierValid(actorCode, 96) || current.ActorCode != actorCode ||
		!cityRealtimeActorStateValid(current) || !cityRealtimeAgentCharacterSocialRuntimeEnabled(binding) {
		return nil, ErrCityInvalidInput
	}
	socialBinding, err := loadCityRealtimeCharacterSocialBinding(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	if socialBinding == nil || socialBinding.AgentBindingHash != binding.BindingHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_scope"})
	}
	minimumX, maximumX := current.X, current.X
	minimumY, maximumY := current.Y, current.Y
	if current.X > math.MinInt64 {
		minimumX--
	}
	if current.X < math.MaxInt64 {
		maximumX++
	}
	if current.Y > math.MinInt64 {
		minimumY--
	}
	if current.Y < math.MaxInt64 {
		maximumY++
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT identity.actor_code, identity.actor_kind, identity.lifecycle_status,
       state.x, state.y, state.z
FROM city_realtime_actor_identities identity
JOIN city_realtime_actor_states state
  ON state.world_id = identity.world_id AND state.actor_code = identity.actor_code
WHERE identity.world_id = $1
  AND identity.actor_code <> $2
  AND identity.lifecycle_status = 'active'
  AND state.z = $3
  AND state.x BETWEEN $4 AND $5
  AND state.y BETWEEN $6 AND $7
  ORDER BY identity.actor_code ASC
	  LIMIT $8`, worldID, actorCode, current.Z, minimumX, maximumX, minimumY, maximumY, cityRealtimeCharacterSocialCandidateCap)
	if err != nil {
		return nil, fmt.Errorf("list realtime character social targets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	codes := make([]string, 0)
	for rows.Next() {
		target := cityRealtimeCharacterSocialTarget{}
		if err = rows.Scan(&target.ActorCode, &target.ActorKind, &target.LifecycleStatus, &target.X, &target.Y, &target.Z); err != nil {
			return nil, err
		}
		if (target.ActorKind == "npc" || target.ActorKind == "character") && cityRealtimeCharacterSocialTargetAllowed(current, target) {
			codes = append(codes, target.ActorCode)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character social targets: %w", err)
	}
	if !cityRealtimeAgentActionContextSortedUnique(codes, func(code string) bool {
		return cityRealtimeAgentIdentifierValid(code, 96)
	}) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_targets"})
	}
	return codes, nil
}

func insertCityRealtimeCharacterSocialHead(ctx context.Context, tx *sql.Tx, worldID int64, head cityRealtimeCharacterSocialHead) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterSocialHeadValid(head) || head.RelationRevision != 0 {
		return ErrCityInvalidInput
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_social_heads
    (world_id, actor_code_low, actor_code_high, relation_revision, last_frame_sequence,
     affinity_milli, interaction_count, event_chain_hash, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '{}'::jsonb)`,
		worldID, head.ActorCodeLow, head.ActorCodeHigh, head.RelationRevision, head.LastFrameSequence,
		head.AffinityMilli, head.InteractionCount, head.EventChainHash, head.StateHash,
	)
	if err != nil {
		return fmt.Errorf("insert realtime character social head: %w", err)
	}
	return nil
}

func insertCityRealtimeCharacterSocialEvent(ctx context.Context, tx *sql.Tx, worldID int64, event cityRealtimeCharacterSocialEvent) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterSocialEventValid(event) || !cityRealtimeSHA256Hex(event.EventHash) {
		return ErrCityInvalidInput
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_social_events
    (world_id, actor_code_low, actor_code_high, event_sequence, frame_sequence,
     interaction_code, initiator_code, recipient_code, source_intent_code,
     previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, '{}'::jsonb)`,
		worldID, event.ActorCodeLow, event.ActorCodeHigh, event.EventSequence, event.FrameSequence,
		event.InteractionCode, event.InitiatorCode, event.RecipientCode, event.SourceIntentCode,
		event.PreviousEventHash, event.EventHash,
	)
	if err != nil {
		return fmt.Errorf("append realtime character social event: %w", err)
	}
	return nil
}

func updateCityRealtimeCharacterSocialHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previous, next cityRealtimeCharacterSocialHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterSocialHeadValid(previous) || !cityRealtimeCharacterSocialHeadValid(next) ||
		previous.ActorCodeLow != next.ActorCodeLow || previous.ActorCodeHigh != next.ActorCodeHigh ||
		next.RelationRevision != previous.RelationRevision+1 || next.LastFrameSequence <= previous.LastFrameSequence ||
		next.EventChainHash == previous.EventChainHash {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_social_heads
SET relation_revision = $4, last_frame_sequence = $5, affinity_milli = $6,
    interaction_count = $7, event_chain_hash = $8, state_hash = $9, updated_at = NOW()
WHERE world_id = $1 AND actor_code_low = $2 AND actor_code_high = $3
  AND relation_revision = $10 AND last_frame_sequence = $11
  AND affinity_milli = $12 AND interaction_count = $13
  AND event_chain_hash = $14 AND state_hash = $15`,
		worldID, next.ActorCodeLow, next.ActorCodeHigh, next.RelationRevision, next.LastFrameSequence,
		next.AffinityMilli, next.InteractionCount, next.EventChainHash, next.StateHash,
		previous.RelationRevision, previous.LastFrameSequence, previous.AffinityMilli, previous.InteractionCount,
		previous.EventChainHash, previous.StateHash,
	)
	if err != nil {
		return fmt.Errorf("advance realtime character social head: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime character social head update: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_revision"})
	}
	return nil
}
