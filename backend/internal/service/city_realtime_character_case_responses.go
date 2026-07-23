package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	cityRealtimeCharacterCaseResponseSchemaVersion  = 1
	cityRealtimeCharacterCaseResponseBindingVersion = "city-realtime-character-case-response-binding-v1"
	cityRealtimeCharacterCaseResponseStateVersion   = "city-realtime-character-case-response-state-v1"
	cityRealtimeCharacterCaseResponseChainVersion   = "city-realtime-character-case-response-chain-v1"
	cityRealtimeCharacterCaseResponseEventVersion   = "city-realtime-character-case-response-event-v1"

	cityRealtimeCharacterCaseResponseAcknowledged = "acknowledged"
	cityRealtimeCharacterCaseResponseCandidateCap = 16
)

// cityRealtimeCharacterCaseResponseBinding pins the first Rule/Case adapter
// to the Agent policy that created the world. It prevents a newer binary from
// silently adding Case state to an existing 1.3.0 world hash.
type cityRealtimeCharacterCaseResponseBinding struct {
	SchemaVersion    int    `json:"schema_version"`
	AgentBindingHash string `json:"agent_binding_hash"`
	BindingHash      string `json:"binding_hash"`
}

// cityRealtimeCharacterCaseResponseHead keeps the Case-response hash state
// bounded. The immutable events retain the complete audit history, while the
// head lets normal world hashing verify the current chain in constant space.
type cityRealtimeCharacterCaseResponseHead struct {
	ActorCode         string `json:"actor_code"`
	ResponseRevision  int64  `json:"response_revision"`
	LastFrameSequence int64  `json:"last_frame_sequence"`
	EventChainHash    string `json:"event_chain_hash"`
	StateHash         string `json:"state_hash"`
}

type cityRealtimeCharacterCaseResponseEvent struct {
	ActorCode         string
	EventSequence     int64
	FrameSequence     int64
	CaseCode          string
	LawEventSequence  int64
	LawEventHash      string
	ResponseCode      string
	SourceIntentCode  string
	PreviousEventHash string
	EventHash         string
}

// cityRealtimeCharacterOpenLawCase is a server-owned projection of an
// already-applied law fact that has not received an acknowledgement. It is
// never inferred from a model reason or free-form Case identifier.
type cityRealtimeCharacterOpenLawCase struct {
	ActorCode              string
	LawEventSequence       int64
	LawFrameSequence       int64
	CaseCode               string
	RuleCode               string
	Disposition            string
	PenaltyCityCreditUnits int64
	LawEventHash           string
}

type cityRealtimeCharacterCaseResponseHashState struct {
	SchemaVersion int                                       `json:"schema_version"`
	Binding       *cityRealtimeCharacterCaseResponseBinding `json:"binding,omitempty"`
	Heads         []cityRealtimeCharacterCaseResponseHead   `json:"heads"`
}

func cityRealtimeCharacterLawCaseCodeValid(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] != "law" || len(parts[1]) != 16 || len(value) > 64 {
		return false
	}
	for _, character := range parts[1] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	sequence, err := strconv.ParseInt(parts[2], 10, 64)
	return err == nil && sequence > 0
}

func cityRealtimeCharacterCaseResponseBindingHash(agentBindingHash string) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseResponseBindingVersion,
		agentBindingHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseResponseChainGenesisHash(actorCode string, spawnedFrameSequence int64) (string, error) {
	if !cityRealtimePlayerActorCodeValid(actorCode) || spawnedFrameSequence <= 0 {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseResponseChainVersion,
		actorCode,
		strconv.FormatInt(spawnedFrameSequence, 10),
	}, "\x1f"))), nil
}

func cityRealtimeCharacterCaseResponseHeadStateHash(head cityRealtimeCharacterCaseResponseHead) (string, error) {
	if !cityRealtimePlayerActorCodeValid(head.ActorCode) || head.ResponseRevision < 0 || head.LastFrameSequence <= 0 ||
		!cityRealtimeSHA256Hex(head.EventChainHash) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseResponseStateVersion,
		head.ActorCode,
		strconv.FormatInt(head.ResponseRevision, 10),
		strconv.FormatInt(head.LastFrameSequence, 10),
		head.EventChainHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterCaseResponseEventHash(event cityRealtimeCharacterCaseResponseEvent) (string, error) {
	if !cityRealtimeCharacterCaseResponseEventValid(event) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseResponseEventVersion,
		event.ActorCode,
		strconv.FormatInt(event.EventSequence, 10),
		strconv.FormatInt(event.FrameSequence, 10),
		event.CaseCode,
		strconv.FormatInt(event.LawEventSequence, 10),
		event.LawEventHash,
		event.ResponseCode,
		event.SourceIntentCode,
		event.PreviousEventHash,
	}, "\x1f"))), nil
}

func validateCityRealtimeCharacterCaseResponseBinding(binding cityRealtimeCharacterCaseResponseBinding) bool {
	return binding.SchemaVersion == cityRealtimeCharacterCaseResponseSchemaVersion &&
		cityRealtimeSHA256Hex(binding.AgentBindingHash) &&
		cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeCharacterCaseResponseBindingHash(binding.AgentBindingHash)
}

func cityRealtimeCharacterCaseResponseHeadValid(head cityRealtimeCharacterCaseResponseHead) bool {
	if !cityRealtimePlayerActorCodeValid(head.ActorCode) || head.ResponseRevision < 0 || head.LastFrameSequence <= 0 ||
		!cityRealtimeSHA256Hex(head.EventChainHash) || !cityRealtimeSHA256Hex(head.StateHash) {
		return false
	}
	expectedHash, err := cityRealtimeCharacterCaseResponseHeadStateHash(head)
	return err == nil && expectedHash == head.StateHash
}

func cityRealtimeCharacterCaseResponseEventValid(event cityRealtimeCharacterCaseResponseEvent) bool {
	return cityRealtimePlayerActorCodeValid(event.ActorCode) && event.EventSequence > 0 && event.FrameSequence > 0 &&
		cityRealtimeCharacterLawCaseCodeValid(event.CaseCode) && event.LawEventSequence > 0 &&
		cityRealtimeSHA256Hex(event.LawEventHash) && event.ResponseCode == cityRealtimeCharacterCaseResponseAcknowledged &&
		cityRealtimeAgentIdentifierValid(event.SourceIntentCode, 96) && cityRealtimeSHA256Hex(event.PreviousEventHash) &&
		(event.EventHash == "" || cityRealtimeSHA256Hex(event.EventHash))
}

func cityRealtimeCharacterOpenLawCaseValid(item cityRealtimeCharacterOpenLawCase) bool {
	if !cityRealtimePlayerActorCodeValid(item.ActorCode) || item.LawEventSequence <= 0 || item.LawFrameSequence <= 0 ||
		!cityRealtimeCharacterLawCaseCodeValid(item.CaseCode) || !cityRealtimeAgentIdentifierValid(item.RuleCode, 64) ||
		item.PenaltyCityCreditUnits < 0 || !cityRealtimeSHA256Hex(item.LawEventHash) {
		return false
	}
	switch item.Disposition {
	case "warning", "fine", "service":
		return true
	default:
		return false
	}
}

func newCityRealtimeCharacterCaseResponseGenesisHead(
	actorCode string,
	spawnedFrameSequence int64,
) (cityRealtimeCharacterCaseResponseHead, error) {
	chainHash, err := cityRealtimeCharacterCaseResponseChainGenesisHash(actorCode, spawnedFrameSequence)
	if err != nil {
		return cityRealtimeCharacterCaseResponseHead{}, err
	}
	head := cityRealtimeCharacterCaseResponseHead{
		ActorCode: actorCode, LastFrameSequence: spawnedFrameSequence, EventChainHash: chainHash,
	}
	head.StateHash, err = cityRealtimeCharacterCaseResponseHeadStateHash(head)
	if err != nil || !cityRealtimeCharacterCaseResponseHeadValid(head) {
		if err == nil {
			err = ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_genesis"})
		}
		return cityRealtimeCharacterCaseResponseHead{}, err
	}
	return head, nil
}

func cityRealtimeCharacterAcknowledgeLawCase(
	previous cityRealtimeCharacterCaseResponseHead,
	lawCase cityRealtimeCharacterOpenLawCase,
	sourceIntentCode string,
	frameSequence int64,
) (cityRealtimeCharacterCaseResponseHead, cityRealtimeCharacterCaseResponseEvent, error) {
	if !cityRealtimeCharacterCaseResponseHeadValid(previous) || !cityRealtimeCharacterOpenLawCaseValid(lawCase) ||
		previous.ActorCode != lawCase.ActorCode || !cityRealtimeAgentIdentifierValid(sourceIntentCode, 96) ||
		frameSequence <= previous.LastFrameSequence {
		return cityRealtimeCharacterCaseResponseHead{}, cityRealtimeCharacterCaseResponseEvent{}, ErrCityInvalidInput
	}
	event := cityRealtimeCharacterCaseResponseEvent{
		ActorCode:         previous.ActorCode,
		EventSequence:     previous.ResponseRevision + 1,
		FrameSequence:     frameSequence,
		CaseCode:          lawCase.CaseCode,
		LawEventSequence:  lawCase.LawEventSequence,
		LawEventHash:      lawCase.LawEventHash,
		ResponseCode:      cityRealtimeCharacterCaseResponseAcknowledged,
		SourceIntentCode:  sourceIntentCode,
		PreviousEventHash: previous.EventChainHash,
	}
	var err error
	event.EventHash, err = cityRealtimeCharacterCaseResponseEventHash(event)
	if err != nil {
		return cityRealtimeCharacterCaseResponseHead{}, cityRealtimeCharacterCaseResponseEvent{}, err
	}
	next := previous
	next.ResponseRevision = event.EventSequence
	next.LastFrameSequence = frameSequence
	next.EventChainHash = event.EventHash
	next.StateHash, err = cityRealtimeCharacterCaseResponseHeadStateHash(next)
	if err != nil || !cityRealtimeCharacterCaseResponseHeadValid(next) {
		if err == nil {
			err = ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_next"})
		}
		return cityRealtimeCharacterCaseResponseHead{}, cityRealtimeCharacterCaseResponseEvent{}, err
	}
	return next, event, nil
}

func initializeCityRealtimeCharacterCaseResponseFoundation(
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
	if agentState == nil || agentState.Binding == nil || !cityRealtimeAgentCharacterCaseRuntimeEnabled(*agentState.Binding) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_policy"})
	}
	binding := cityRealtimeCharacterCaseResponseBinding{
		SchemaVersion:    cityRealtimeCharacterCaseResponseSchemaVersion,
		AgentBindingHash: agentState.Binding.BindingHash,
	}
	binding.BindingHash = cityRealtimeCharacterCaseResponseBindingHash(binding.AgentBindingHash)
	if !validateCityRealtimeCharacterCaseResponseBinding(binding) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_binding"})
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_character_case_response_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("activate realtime character case-response initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_response_world_bindings
    (world_id, schema_version, agent_binding_hash, binding_hash, metadata)
VALUES ($1, $2, $3, $4, '{}'::jsonb)`,
		worldID, binding.SchemaVersion, binding.AgentBindingHash, binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime character case-response binding: %w", err)
	}
	return nil
}

func enableCityRealtimeCharacterCaseResponseMutationGate(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
) error {
	if tx == nil || worldID <= 0 || frameSequence <= 0 {
		return ErrCityInvalidInput
	}
	for _, setting := range []struct {
		name  string
		value int64
	}{
		{name: "sub2api.city_realtime_character_case_response_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_case_response_frame_sequence", value: frameSequence},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character case-response gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func loadCityRealtimeCharacterCaseResponseBinding(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseResponseBinding, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterCaseResponseBinding{}
	var policyID, policyVersion, currentAgentBindingHash string
	err := queryer.QueryRowContext(ctx, `
SELECT response.schema_version, response.agent_binding_hash, response.binding_hash,
       agent.policy_id, agent.policy_version, agent.binding_hash
FROM city_realtime_character_case_response_world_bindings response
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = response.world_id
WHERE response.world_id = $1`, worldID).Scan(
		&binding.SchemaVersion, &binding.AgentBindingHash, &binding.BindingHash,
		&policyID, &policyVersion, &currentAgentBindingHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-response binding: %w", err)
	}
	if policyID != cityRealtimeAgentCorePolicyID ||
		(policyVersion != cityRealtimeAgentCorePolicyVersionCase &&
			policyVersion != cityRealtimeAgentCorePolicyVersionSocial &&
			policyVersion != cityRealtimeAgentCorePolicyVersionReview &&
			policyVersion != cityRealtimeAgentCorePolicyVersionReport &&
			policyVersion != cityRealtimeAgentCorePolicyVersionIntake &&
			policyVersion != cityRealtimeAgentCorePolicyVersionEvidence &&
			policyVersion != cityRealtimeAgentCorePolicyVersionEvidenceAssignment &&
			policyVersion != cityRealtimeAgentCorePolicyVersionProcedureDispatch &&
			policyVersion != cityRealtimeAgentCorePolicyVersionTask &&
			policyVersion != cityRealtimeAgentCorePolicyVersionNavigationPlan) ||
		binding.AgentBindingHash != currentAgentBindingHash || !validateCityRealtimeCharacterCaseResponseBinding(binding) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_binding"})
	}
	return &binding, nil
}

func loadCityRealtimeCharacterCaseResponseHead(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	forUpdate bool,
) (cityRealtimeCharacterCaseResponseHead, bool, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) {
		return cityRealtimeCharacterCaseResponseHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT actor_code, response_revision, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_case_response_heads
WHERE world_id = $1 AND actor_code = $2`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head := cityRealtimeCharacterCaseResponseHead{}
	err := queryer.QueryRowContext(ctx, query, worldID, actorCode).Scan(
		&head.ActorCode, &head.ResponseRevision, &head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterCaseResponseHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterCaseResponseHead{}, false, fmt.Errorf("load realtime character case-response head: %w", err)
	}
	if !cityRealtimeCharacterCaseResponseHeadValid(head) {
		return cityRealtimeCharacterCaseResponseHead{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_head"})
	}
	return head, true, nil
}

func validateCityRealtimeCharacterCaseResponseHeadHistory(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	profile cityRealtimeCharacterProfile,
	head cityRealtimeCharacterCaseResponseHead,
) error {
	if worldID <= 0 || !cityRealtimeCharacterProfileValid(profile) || !cityRealtimeCharacterCaseResponseHeadValid(head) ||
		head.ActorCode != profile.ActorCode || head.LastFrameSequence < profile.SpawnedFrameSequence {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_history"})
	}
	genesisHash, err := cityRealtimeCharacterCaseResponseChainGenesisHash(profile.ActorCode, profile.SpawnedFrameSequence)
	if err != nil {
		return err
	}
	if head.ResponseRevision == 0 {
		if head.LastFrameSequence != profile.SpawnedFrameSequence || head.EventChainHash != genesisHash {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_genesis"})
		}
		return nil
	}
	var eventSequence, frameSequence int64
	var eventHash string
	err = queryer.QueryRowContext(ctx, `
SELECT event_sequence, frame_sequence, event_hash
FROM city_realtime_character_case_response_events
WHERE world_id = $1 AND actor_code = $2
ORDER BY event_sequence DESC
LIMIT 1`, worldID, profile.ActorCode).Scan(&eventSequence, &frameSequence, &eventHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_event_head"})
	}
	if err != nil {
		return fmt.Errorf("load realtime character case-response event head: %w", err)
	}
	if eventSequence != head.ResponseRevision || frameSequence != head.LastFrameSequence || eventHash != head.EventChainHash {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_event_head"})
	}
	return nil
}

func loadCityRealtimeCharacterOpenLawCase(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode, caseCode string,
	forUpdate bool,
) (cityRealtimeCharacterOpenLawCase, bool, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) || !cityRealtimeCharacterLawCaseCodeValid(caseCode) {
		return cityRealtimeCharacterOpenLawCase{}, false, ErrCityInvalidInput
	}
	query := `
SELECT law.actor_code, law.event_sequence, law.frame_sequence, law.case_code,
       law.rule_code, law.disposition, law.penalty_city_credit_units, law.event_hash
FROM city_realtime_character_law_events law
WHERE law.world_id = $1
  AND law.actor_code = $2
  AND law.case_code = $3
  AND NOT EXISTS (
      SELECT 1
      FROM city_realtime_character_case_response_events response
      WHERE response.world_id = law.world_id AND response.case_code = law.case_code
  )`
	if forUpdate {
		query += " FOR UPDATE OF law"
	}
	item := cityRealtimeCharacterOpenLawCase{}
	err := queryer.QueryRowContext(ctx, query, worldID, actorCode, caseCode).Scan(
		&item.ActorCode, &item.LawEventSequence, &item.LawFrameSequence, &item.CaseCode,
		&item.RuleCode, &item.Disposition, &item.PenaltyCityCreditUnits, &item.LawEventHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterOpenLawCase{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterOpenLawCase{}, false, fmt.Errorf("load realtime character open law case: %w", err)
	}
	if !cityRealtimeCharacterOpenLawCaseValid(item) {
		return cityRealtimeCharacterOpenLawCase{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_open_law_case"})
	}
	return item, true, nil
}

func cityRealtimeAgentDecisionAvailableCaseCodes(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	agentBinding cityRealtimeAgentPolicyBinding,
) ([]string, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) ||
		!cityRealtimeAgentCharacterCaseRuntimeEnabled(agentBinding) {
		return nil, ErrCityInvalidInput
	}
	caseBinding, err := loadCityRealtimeCharacterCaseResponseBinding(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	if caseBinding == nil || caseBinding.AgentBindingHash != agentBinding.BindingHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_scope"})
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT law.case_code
FROM city_realtime_character_law_events law
WHERE law.world_id = $1
  AND law.actor_code = $2
  AND NOT EXISTS (
      SELECT 1
      FROM city_realtime_character_case_response_events response
      WHERE response.world_id = law.world_id AND response.case_code = law.case_code
  )
ORDER BY law.case_code ASC
LIMIT $3`, worldID, actorCode, cityRealtimeCharacterCaseResponseCandidateCap)
	if err != nil {
		return nil, fmt.Errorf("list realtime character open law cases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	codes := make([]string, 0)
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character open law cases: %w", err)
	}
	if !cityRealtimeAgentActionContextSortedUnique(codes, cityRealtimeCharacterLawCaseCodeValid) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_open_law_cases"})
	}
	return codes, nil
}

func cityRealtimeAgentDecisionCaseCodeFromArguments(arguments map[string]any) (string, error) {
	if len(arguments) != 1 {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	rawCode, exists := arguments["case_code"]
	code, ok := rawCode.(string)
	code = strings.TrimSpace(code)
	if !exists || !ok || !cityRealtimeCharacterLawCaseCodeValid(code) {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	return code, nil
}

func cityRealtimeAgentDecisionCaseCodeFromRawArguments(arguments []byte) (string, error) {
	decoded := make(map[string]any)
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_arguments"}).WithCause(err)
	}
	return cityRealtimeAgentDecisionCaseCodeFromArguments(decoded)
}

func insertCityRealtimeCharacterCaseResponseHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	head cityRealtimeCharacterCaseResponseHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseResponseHeadValid(head) || head.ResponseRevision != 0 {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_response_heads
    (world_id, actor_code, response_revision, last_frame_sequence,
     event_chain_hash, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, '{}'::jsonb)`,
		worldID, head.ActorCode, head.ResponseRevision, head.LastFrameSequence,
		head.EventChainHash, head.StateHash,
	); err != nil {
		return fmt.Errorf("insert realtime character case-response head: %w", err)
	}
	return nil
}

func insertCityRealtimeCharacterCaseResponseEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	event cityRealtimeCharacterCaseResponseEvent,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseResponseEventValid(event) || !cityRealtimeSHA256Hex(event.EventHash) {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_response_events
    (world_id, actor_code, event_sequence, frame_sequence, case_code,
     law_event_sequence, law_event_hash, response_code, source_intent_code,
     previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, '{}'::jsonb)`,
		worldID, event.ActorCode, event.EventSequence, event.FrameSequence, event.CaseCode,
		event.LawEventSequence, event.LawEventHash, event.ResponseCode, event.SourceIntentCode,
		event.PreviousEventHash, event.EventHash,
	); err != nil {
		return fmt.Errorf("append realtime character case-response event: %w", err)
	}
	return nil
}

func updateCityRealtimeCharacterCaseResponseHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previous, next cityRealtimeCharacterCaseResponseHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseResponseHeadValid(previous) ||
		!cityRealtimeCharacterCaseResponseHeadValid(next) || previous.ActorCode != next.ActorCode ||
		next.ResponseRevision != previous.ResponseRevision+1 || next.LastFrameSequence <= previous.LastFrameSequence ||
		next.EventChainHash == previous.EventChainHash {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_case_response_heads
SET response_revision = $3, last_frame_sequence = $4, event_chain_hash = $5,
    state_hash = $6, updated_at = NOW()
WHERE world_id = $1 AND actor_code = $2 AND response_revision = $7
  AND last_frame_sequence = $8 AND event_chain_hash = $9 AND state_hash = $10`,
		worldID, next.ActorCode, next.ResponseRevision, next.LastFrameSequence, next.EventChainHash,
		next.StateHash, previous.ResponseRevision, previous.LastFrameSequence, previous.EventChainHash, previous.StateHash,
	)
	if err != nil {
		return fmt.Errorf("advance realtime character case-response head: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime character case-response head update: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_revision"})
	}
	return nil
}

func loadCityRealtimeCharacterCaseResponseHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseResponseHashState, error) {
	binding, err := loadCityRealtimeCharacterCaseResponseBinding(ctx, queryer, worldID)
	if err != nil || binding == nil {
		return nil, err
	}
	state := &cityRealtimeCharacterCaseResponseHashState{
		SchemaVersion: cityRealtimeCharacterCaseResponseSchemaVersion,
		Binding:       binding,
		Heads:         make([]cityRealtimeCharacterCaseResponseHead, 0),
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code, response_revision, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_case_response_heads
WHERE world_id = $1
ORDER BY actor_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-response heads: %w", err)
	}
	heads := make([]cityRealtimeCharacterCaseResponseHead, 0)
	for rows.Next() {
		head := cityRealtimeCharacterCaseResponseHead{}
		if err = rows.Scan(&head.ActorCode, &head.ResponseRevision, &head.LastFrameSequence, &head.EventChainHash, &head.StateHash); err != nil {
			_ = rows.Close()
			return nil, err
		}
		heads = append(heads, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character case-response heads: %w", err)
	}
	// lib/pq permits only one active result stream per transaction connection.
	// Close the head query before loading each profile/event-chain proof below;
	// otherwise a newly-created acknowledgement can corrupt the protocol stream
	// during the canonical-state refresh.
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character case-response heads: %w", err)
	}
	for _, head := range heads {
		profile, found, profileErr := loadCityRealtimeCharacterProfile(ctx, queryer, worldID, head.ActorCode, false)
		if profileErr != nil {
			return nil, profileErr
		}
		if !found {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_profile"})
		}
		if validateErr := validateCityRealtimeCharacterCaseResponseHeadHistory(ctx, queryer, worldID, profile, head); validateErr != nil {
			return nil, validateErr
		}
		state.Heads = append(state.Heads, head)
	}
	if err = validateCityRealtimeCharacterCaseResponseHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_response_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeCharacterCaseResponseHashState(state *cityRealtimeCharacterCaseResponseHashState) error {
	if state == nil || state.SchemaVersion != cityRealtimeCharacterCaseResponseSchemaVersion || state.Binding == nil ||
		state.Heads == nil || !validateCityRealtimeCharacterCaseResponseBinding(*state.Binding) {
		return errors.New("invalid realtime character case-response hash state")
	}
	for index, head := range state.Heads {
		if !cityRealtimeCharacterCaseResponseHeadValid(head) ||
			(index > 0 && state.Heads[index-1].ActorCode >= head.ActorCode) {
			return errors.New("invalid or unordered realtime character case-response heads")
		}
	}
	return nil
}
