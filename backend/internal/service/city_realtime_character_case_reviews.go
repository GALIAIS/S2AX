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
	cityRealtimeCharacterCaseReviewSchemaVersion  = 1
	cityRealtimeCharacterCaseReviewBindingVersion = "city-realtime-character-case-review-binding-v1"
	cityRealtimeCharacterCaseReviewStateVersion   = "city-realtime-character-case-review-state-v1"
	cityRealtimeCharacterCaseReviewChainVersion   = "city-realtime-character-case-review-chain-v1"
	cityRealtimeCharacterCaseReviewEventVersion   = "city-realtime-character-case-review-event-v1"
	cityRealtimeCharacterCaseReviewCloseVersion   = "city-realtime-character-case-review-close-v1"

	cityRealtimeCharacterCaseReviewNone           = "none"
	cityRealtimeCharacterCaseReviewFiled          = "filed"
	cityRealtimeCharacterCaseReviewClosedNoChange = "closed_no_change"

	cityRealtimeCharacterCaseReviewCandidateCap         = 16
	cityRealtimeCharacterCaseReviewWindowUS       int64 = 15 * 60 * cityRealtimeTimeQuantumUS
	cityRealtimeCharacterCaseReviewClosureDelayUS int64 = 30 * cityRealtimeTimeQuantumUS
	cityRealtimeCharacterCaseReviewClosePriority        = 100

	cityRealtimeDueEventTypeCharacterCaseReviewClose = "system.realtime.character_case_review_close"
)

// cityRealtimeCharacterCaseReviewBinding pins the review adapter to exactly
// the 1.6 policy binding that created a world. Older worlds must retain their
// original canonical shape and therefore never acquire review state.
type cityRealtimeCharacterCaseReviewBinding struct {
	SchemaVersion    int    `json:"schema_version"`
	AgentBindingHash string `json:"agent_binding_hash"`
	BindingHash      string `json:"binding_hash"`
}

// cityRealtimeCharacterCaseReviewHead is the bounded state of exactly one
// procedural review. A Law Case can have at most one review: the adapter is a
// receipt and deterministic closure mechanism, not an alternate adjudicator.
type cityRealtimeCharacterCaseReviewHead struct {
	ActorCode                string `json:"actor_code"`
	CaseCode                 string `json:"case_code"`
	LawEventSequence         int64  `json:"law_event_sequence"`
	LawEventHash             string `json:"law_event_hash"`
	ResponseEventSequence    int64  `json:"response_event_sequence"`
	ResponseEventHash        string `json:"response_event_hash"`
	ReviewRevision           int64  `json:"review_revision"`
	ReviewStatus             string `json:"review_status"`
	SourceIntentCode         string `json:"source_intent_code"`
	FiledFrameSequence       int64  `json:"filed_frame_sequence"`
	ResolutionDueWorldTimeUS int64  `json:"resolution_due_world_time_us"`
	LastFrameSequence        int64  `json:"last_frame_sequence"`
	EventChainHash           string `json:"event_chain_hash"`
	StateHash                string `json:"state_hash"`
}

// cityRealtimeCharacterCaseReviewEvent is an append-only procedural fact.
// It never contains prompt text, provider output, an alternate ruling, a
// fine override, city-credit movement, or platform-currency mutation.
type cityRealtimeCharacterCaseReviewEvent struct {
	ActorCode                string
	CaseCode                 string
	EventSequence            int64
	FrameSequence            int64
	EventType                string
	SourceIntentCode         string
	ResolutionDueWorldTimeUS int64
	PreviousEventHash        string
	EventHash                string
}

// cityRealtimeCharacterReviewableLawCase is a server-only join of a Law fact
// and its acknowledgement. The model sees only the opaque Case code after the
// candidate set is sealed into its observation.
type cityRealtimeCharacterReviewableLawCase struct {
	ActorCode             string
	CaseCode              string
	LawEventSequence      int64
	LawEventHash          string
	ResponseEventSequence int64
	ResponseFrameSequence int64
	ResponseEventHash     string
}

type cityRealtimeCharacterCaseReviewHashState struct {
	SchemaVersion int                                     `json:"schema_version"`
	Binding       *cityRealtimeCharacterCaseReviewBinding `json:"binding,omitempty"`
	Heads         []cityRealtimeCharacterCaseReviewHead   `json:"heads"`
}

type cityRealtimeCharacterCaseReviewCloseDuePayload struct {
	SchemaVersion    int    `json:"schema_version"`
	ActorCode        string `json:"actor_code"`
	CaseCode         string `json:"case_code"`
	SourceIntentCode string `json:"source_intent_code"`
}

func cityRealtimeCharacterCaseReviewBindingHash(agentBindingHash string) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseReviewBindingVersion,
		agentBindingHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseReviewChainGenesisHash(
	actorCode, caseCode, lawEventHash, responseEventHash string,
) (string, error) {
	if !cityRealtimePlayerActorCodeValid(actorCode) || !cityRealtimeCharacterLawCaseCodeValid(caseCode) ||
		!cityRealtimeSHA256Hex(lawEventHash) || !cityRealtimeSHA256Hex(responseEventHash) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseReviewChainVersion,
		actorCode,
		caseCode,
		lawEventHash,
		responseEventHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterCaseReviewHeadStateHashUnchecked(head cityRealtimeCharacterCaseReviewHead) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseReviewStateVersion,
		head.ActorCode,
		head.CaseCode,
		strconv.FormatInt(head.LawEventSequence, 10),
		head.LawEventHash,
		strconv.FormatInt(head.ResponseEventSequence, 10),
		head.ResponseEventHash,
		strconv.FormatInt(head.ReviewRevision, 10),
		head.ReviewStatus,
		head.SourceIntentCode,
		strconv.FormatInt(head.FiledFrameSequence, 10),
		strconv.FormatInt(head.ResolutionDueWorldTimeUS, 10),
		strconv.FormatInt(head.LastFrameSequence, 10),
		head.EventChainHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseReviewHeadValid(head cityRealtimeCharacterCaseReviewHead) bool {
	if !cityRealtimePlayerActorCodeValid(head.ActorCode) || !cityRealtimeCharacterLawCaseCodeValid(head.CaseCode) ||
		head.LawEventSequence <= 0 || !cityRealtimeSHA256Hex(head.LawEventHash) ||
		head.ResponseEventSequence <= 0 || !cityRealtimeSHA256Hex(head.ResponseEventHash) ||
		head.LastFrameSequence <= 0 || !cityRealtimeSHA256Hex(head.EventChainHash) ||
		!cityRealtimeSHA256Hex(head.StateHash) {
		return false
	}
	switch head.ReviewRevision {
	case 0:
		if head.ReviewStatus != cityRealtimeCharacterCaseReviewNone || head.SourceIntentCode != "" ||
			head.FiledFrameSequence != 0 || head.ResolutionDueWorldTimeUS != 0 {
			return false
		}
	case 1:
		if head.ReviewStatus != cityRealtimeCharacterCaseReviewFiled ||
			!cityRealtimeAgentIdentifierValid(head.SourceIntentCode, 96) ||
			head.FiledFrameSequence <= 0 || head.LastFrameSequence != head.FiledFrameSequence ||
			head.ResolutionDueWorldTimeUS <= 0 ||
			head.ResolutionDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
			return false
		}
	case 2:
		if head.ReviewStatus != cityRealtimeCharacterCaseReviewClosedNoChange ||
			!cityRealtimeAgentIdentifierValid(head.SourceIntentCode, 96) ||
			head.FiledFrameSequence <= 0 || head.LastFrameSequence <= head.FiledFrameSequence ||
			head.ResolutionDueWorldTimeUS <= 0 ||
			head.ResolutionDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
			return false
		}
	default:
		return false
	}
	return head.StateHash == cityRealtimeCharacterCaseReviewHeadStateHashUnchecked(head)
}

func cityRealtimeCharacterCaseReviewEventValid(event cityRealtimeCharacterCaseReviewEvent) bool {
	if !cityRealtimePlayerActorCodeValid(event.ActorCode) || !cityRealtimeCharacterLawCaseCodeValid(event.CaseCode) ||
		event.EventSequence <= 0 || event.FrameSequence <= 0 ||
		!cityRealtimeAgentIdentifierValid(event.SourceIntentCode, 96) ||
		event.ResolutionDueWorldTimeUS <= 0 ||
		event.ResolutionDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 ||
		!cityRealtimeSHA256Hex(event.PreviousEventHash) ||
		(event.EventHash != "" && !cityRealtimeSHA256Hex(event.EventHash)) {
		return false
	}
	return (event.EventSequence == 1 && event.EventType == cityRealtimeCharacterCaseReviewFiled) ||
		(event.EventSequence == 2 && event.EventType == cityRealtimeCharacterCaseReviewClosedNoChange)
}

func cityRealtimeCharacterCaseReviewEventHash(event cityRealtimeCharacterCaseReviewEvent) (string, error) {
	if !cityRealtimeCharacterCaseReviewEventValid(event) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseReviewEventVersion,
		event.ActorCode,
		event.CaseCode,
		strconv.FormatInt(event.EventSequence, 10),
		strconv.FormatInt(event.FrameSequence, 10),
		event.EventType,
		event.SourceIntentCode,
		strconv.FormatInt(event.ResolutionDueWorldTimeUS, 10),
		event.PreviousEventHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterReviewableLawCaseValid(item cityRealtimeCharacterReviewableLawCase) bool {
	return cityRealtimePlayerActorCodeValid(item.ActorCode) &&
		cityRealtimeCharacterLawCaseCodeValid(item.CaseCode) &&
		item.LawEventSequence > 0 && cityRealtimeSHA256Hex(item.LawEventHash) &&
		item.ResponseEventSequence > 0 && item.ResponseFrameSequence > 0 &&
		cityRealtimeSHA256Hex(item.ResponseEventHash)
}

func newCityRealtimeCharacterCaseReviewGenesisHead(
	item cityRealtimeCharacterReviewableLawCase,
) (cityRealtimeCharacterCaseReviewHead, error) {
	if !cityRealtimeCharacterReviewableLawCaseValid(item) {
		return cityRealtimeCharacterCaseReviewHead{}, ErrCityInvalidInput
	}
	chainHash, err := cityRealtimeCharacterCaseReviewChainGenesisHash(
		item.ActorCode, item.CaseCode, item.LawEventHash, item.ResponseEventHash,
	)
	if err != nil {
		return cityRealtimeCharacterCaseReviewHead{}, err
	}
	head := cityRealtimeCharacterCaseReviewHead{
		ActorCode:             item.ActorCode,
		CaseCode:              item.CaseCode,
		LawEventSequence:      item.LawEventSequence,
		LawEventHash:          item.LawEventHash,
		ResponseEventSequence: item.ResponseEventSequence,
		ResponseEventHash:     item.ResponseEventHash,
		ReviewStatus:          cityRealtimeCharacterCaseReviewNone,
		LastFrameSequence:     item.ResponseFrameSequence,
		EventChainHash:        chainHash,
	}
	head.StateHash = cityRealtimeCharacterCaseReviewHeadStateHashUnchecked(head)
	if !cityRealtimeCharacterCaseReviewHeadValid(head) {
		return cityRealtimeCharacterCaseReviewHead{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_genesis"})
	}
	return head, nil
}

func cityRealtimeCharacterFileCaseReview(
	previous cityRealtimeCharacterCaseReviewHead,
	item cityRealtimeCharacterReviewableLawCase,
	sourceIntentCode string,
	frameSequence, resolutionDueWorldTimeUS int64,
) (cityRealtimeCharacterCaseReviewHead, cityRealtimeCharacterCaseReviewEvent, error) {
	if !cityRealtimeCharacterCaseReviewHeadValid(previous) || !cityRealtimeCharacterReviewableLawCaseValid(item) ||
		previous.ReviewRevision != 0 || previous.ReviewStatus != cityRealtimeCharacterCaseReviewNone ||
		previous.ActorCode != item.ActorCode || previous.CaseCode != item.CaseCode ||
		previous.LawEventSequence != item.LawEventSequence || previous.LawEventHash != item.LawEventHash ||
		previous.ResponseEventSequence != item.ResponseEventSequence || previous.ResponseEventHash != item.ResponseEventHash ||
		!cityRealtimeAgentIdentifierValid(sourceIntentCode, 96) || frameSequence <= previous.LastFrameSequence ||
		resolutionDueWorldTimeUS <= 0 || resolutionDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return cityRealtimeCharacterCaseReviewHead{}, cityRealtimeCharacterCaseReviewEvent{}, ErrCityInvalidInput
	}
	event := cityRealtimeCharacterCaseReviewEvent{
		ActorCode:                previous.ActorCode,
		CaseCode:                 previous.CaseCode,
		EventSequence:            1,
		FrameSequence:            frameSequence,
		EventType:                cityRealtimeCharacterCaseReviewFiled,
		SourceIntentCode:         sourceIntentCode,
		ResolutionDueWorldTimeUS: resolutionDueWorldTimeUS,
		PreviousEventHash:        previous.EventChainHash,
	}
	var err error
	event.EventHash, err = cityRealtimeCharacterCaseReviewEventHash(event)
	if err != nil {
		return cityRealtimeCharacterCaseReviewHead{}, cityRealtimeCharacterCaseReviewEvent{}, err
	}
	next := previous
	next.ReviewRevision = event.EventSequence
	next.ReviewStatus = cityRealtimeCharacterCaseReviewFiled
	next.SourceIntentCode = sourceIntentCode
	next.FiledFrameSequence = frameSequence
	next.ResolutionDueWorldTimeUS = resolutionDueWorldTimeUS
	next.LastFrameSequence = frameSequence
	next.EventChainHash = event.EventHash
	next.StateHash = cityRealtimeCharacterCaseReviewHeadStateHashUnchecked(next)
	if !cityRealtimeCharacterCaseReviewHeadValid(next) {
		return cityRealtimeCharacterCaseReviewHead{}, cityRealtimeCharacterCaseReviewEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_filed"})
	}
	return next, event, nil
}

func cityRealtimeCharacterCloseCaseReview(
	previous cityRealtimeCharacterCaseReviewHead,
	frameSequence, dueWorldTimeUS int64,
) (cityRealtimeCharacterCaseReviewHead, cityRealtimeCharacterCaseReviewEvent, error) {
	if !cityRealtimeCharacterCaseReviewHeadValid(previous) || previous.ReviewRevision != 1 ||
		previous.ReviewStatus != cityRealtimeCharacterCaseReviewFiled || frameSequence <= previous.LastFrameSequence ||
		dueWorldTimeUS != previous.ResolutionDueWorldTimeUS {
		return cityRealtimeCharacterCaseReviewHead{}, cityRealtimeCharacterCaseReviewEvent{}, ErrCityInvalidInput
	}
	event := cityRealtimeCharacterCaseReviewEvent{
		ActorCode:                previous.ActorCode,
		CaseCode:                 previous.CaseCode,
		EventSequence:            2,
		FrameSequence:            frameSequence,
		EventType:                cityRealtimeCharacterCaseReviewClosedNoChange,
		SourceIntentCode:         previous.SourceIntentCode,
		ResolutionDueWorldTimeUS: dueWorldTimeUS,
		PreviousEventHash:        previous.EventChainHash,
	}
	var err error
	event.EventHash, err = cityRealtimeCharacterCaseReviewEventHash(event)
	if err != nil {
		return cityRealtimeCharacterCaseReviewHead{}, cityRealtimeCharacterCaseReviewEvent{}, err
	}
	next := previous
	next.ReviewRevision = event.EventSequence
	next.ReviewStatus = cityRealtimeCharacterCaseReviewClosedNoChange
	next.LastFrameSequence = frameSequence
	next.EventChainHash = event.EventHash
	next.StateHash = cityRealtimeCharacterCaseReviewHeadStateHashUnchecked(next)
	if !cityRealtimeCharacterCaseReviewHeadValid(next) {
		return cityRealtimeCharacterCaseReviewHead{}, cityRealtimeCharacterCaseReviewEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_closed"})
	}
	return next, event, nil
}

func initializeCityRealtimeCharacterCaseReviewFoundation(
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
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_policy"})
	}
	if !cityRealtimeAgentCharacterCaseReviewRuntimeEnabled(*agentState.Binding) {
		return nil
	}
	caseBinding, err := loadCityRealtimeCharacterCaseResponseBinding(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if caseBinding == nil || caseBinding.AgentBindingHash != agentState.Binding.BindingHash {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_case_scope"})
	}
	binding := cityRealtimeCharacterCaseReviewBinding{
		SchemaVersion:    cityRealtimeCharacterCaseReviewSchemaVersion,
		AgentBindingHash: agentState.Binding.BindingHash,
	}
	binding.BindingHash = cityRealtimeCharacterCaseReviewBindingHash(binding.AgentBindingHash)
	if !validateCityRealtimeCharacterCaseReviewBinding(binding) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_binding"})
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_character_case_review_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("activate realtime character case-review initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_review_world_bindings
    (world_id, schema_version, agent_binding_hash, binding_hash, metadata)
VALUES ($1, $2, $3, $4, '{}'::jsonb)`,
		worldID, binding.SchemaVersion, binding.AgentBindingHash, binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime character case-review binding: %w", err)
	}
	return nil
}

func enableCityRealtimeCharacterCaseReviewMutationGate(
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
		{name: "sub2api.city_realtime_character_case_review_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_case_review_frame_sequence", value: frameSequence},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character case-review gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func validateCityRealtimeCharacterCaseReviewBinding(binding cityRealtimeCharacterCaseReviewBinding) bool {
	return binding.SchemaVersion == cityRealtimeCharacterCaseReviewSchemaVersion &&
		cityRealtimeSHA256Hex(binding.AgentBindingHash) && cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeCharacterCaseReviewBindingHash(binding.AgentBindingHash)
}

func loadCityRealtimeCharacterCaseReviewBinding(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseReviewBinding, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterCaseReviewBinding{}
	var policyID, policyVersion, currentAgentBindingHash string
	err := queryer.QueryRowContext(ctx, `
SELECT review.schema_version, review.agent_binding_hash, review.binding_hash,
       agent.policy_id, agent.policy_version, agent.binding_hash
FROM city_realtime_character_case_review_world_bindings review
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = review.world_id
WHERE review.world_id = $1`, worldID).Scan(
		&binding.SchemaVersion, &binding.AgentBindingHash, &binding.BindingHash,
		&policyID, &policyVersion, &currentAgentBindingHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-review binding: %w", err)
	}
	if policyID != cityRealtimeAgentCorePolicyID || policyVersion != cityRealtimeAgentCorePolicyVersionReview ||
		binding.AgentBindingHash != currentAgentBindingHash || !validateCityRealtimeCharacterCaseReviewBinding(binding) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_binding"})
	}
	return &binding, nil
}

func loadCityRealtimeCharacterReviewableLawCase(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, worldTimeUS int64,
	actorCode, caseCode string,
	forUpdate bool,
) (cityRealtimeCharacterReviewableLawCase, bool, error) {
	if worldID <= 0 || worldTimeUS < 0 || !cityRealtimePlayerActorCodeValid(actorCode) ||
		!cityRealtimeCharacterLawCaseCodeValid(caseCode) {
		return cityRealtimeCharacterReviewableLawCase{}, false, ErrCityInvalidInput
	}
	windowStart := int64(0)
	if worldTimeUS > cityRealtimeCharacterCaseReviewWindowUS {
		windowStart = worldTimeUS - cityRealtimeCharacterCaseReviewWindowUS
	}
	query := `
SELECT law.actor_code, law.case_code, law.event_sequence, law.event_hash,
       response.event_sequence, response.frame_sequence, response.event_hash
FROM city_realtime_character_law_events law
JOIN city_realtime_character_case_response_events response
  ON response.world_id = law.world_id
 AND response.actor_code = law.actor_code
 AND response.case_code = law.case_code
JOIN city_temporal_frames response_frame
  ON response_frame.world_id = response.world_id
 AND response_frame.frame_sequence = response.frame_sequence
WHERE law.world_id = $1
  AND law.actor_code = $2
  AND law.case_code = $3
  AND response.response_code = 'acknowledged'
  AND response_frame.world_time_to_us >= $4
  AND response_frame.world_time_to_us <= $5
  AND NOT EXISTS (
      SELECT 1
      FROM city_realtime_character_case_review_heads review
      WHERE review.world_id = law.world_id
        AND review.actor_code = law.actor_code
        AND review.case_code = law.case_code
  )`
	if forUpdate {
		query += " FOR UPDATE OF law, response"
	}
	item := cityRealtimeCharacterReviewableLawCase{}
	err := queryer.QueryRowContext(ctx, query, worldID, actorCode, caseCode, windowStart, worldTimeUS).Scan(
		&item.ActorCode, &item.CaseCode, &item.LawEventSequence, &item.LawEventHash,
		&item.ResponseEventSequence, &item.ResponseFrameSequence, &item.ResponseEventHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterReviewableLawCase{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterReviewableLawCase{}, false, fmt.Errorf("load realtime character reviewable law case: %w", err)
	}
	if !cityRealtimeCharacterReviewableLawCaseValid(item) {
		return cityRealtimeCharacterReviewableLawCase{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_reviewable_law_case"})
	}
	return item, true, nil
}

func cityRealtimeAgentDecisionAvailableCaseReviewCodes(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, worldTimeUS int64,
	actorCode string,
	agentBinding cityRealtimeAgentPolicyBinding,
) ([]string, error) {
	if worldID <= 0 || worldTimeUS < 0 || !cityRealtimePlayerActorCodeValid(actorCode) ||
		!cityRealtimeAgentCharacterCaseReviewRuntimeEnabled(agentBinding) {
		return nil, ErrCityInvalidInput
	}
	reviewBinding, err := loadCityRealtimeCharacterCaseReviewBinding(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	if reviewBinding == nil || reviewBinding.AgentBindingHash != agentBinding.BindingHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_scope"})
	}
	windowStart := int64(0)
	if worldTimeUS > cityRealtimeCharacterCaseReviewWindowUS {
		windowStart = worldTimeUS - cityRealtimeCharacterCaseReviewWindowUS
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT law.case_code
FROM city_realtime_character_law_events law
JOIN city_realtime_character_case_response_events response
  ON response.world_id = law.world_id
 AND response.actor_code = law.actor_code
 AND response.case_code = law.case_code
JOIN city_temporal_frames response_frame
  ON response_frame.world_id = response.world_id
 AND response_frame.frame_sequence = response.frame_sequence
WHERE law.world_id = $1
  AND law.actor_code = $2
  AND response.response_code = 'acknowledged'
  AND response_frame.world_time_to_us >= $3
  AND response_frame.world_time_to_us <= $4
  AND NOT EXISTS (
      SELECT 1
      FROM city_realtime_character_case_review_heads review
      WHERE review.world_id = law.world_id
        AND review.actor_code = law.actor_code
        AND review.case_code = law.case_code
  )
ORDER BY law.case_code ASC
LIMIT $5`, worldID, actorCode, windowStart, worldTimeUS, cityRealtimeCharacterCaseReviewCandidateCap)
	if err != nil {
		return nil, fmt.Errorf("list realtime character reviewable law cases: %w", err)
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
		return nil, fmt.Errorf("iterate realtime character reviewable law cases: %w", err)
	}
	if !cityRealtimeAgentActionContextSortedUnique(codes, cityRealtimeCharacterLawCaseCodeValid) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_reviewable_law_cases"})
	}
	return codes, nil
}

func cityRealtimeAgentDecisionCaseReviewCodeFromArguments(arguments map[string]any) (string, error) {
	code, err := cityRealtimeAgentDecisionCaseCodeFromArguments(arguments)
	if err != nil {
		return "", err
	}
	return code, nil
}

func cityRealtimeAgentDecisionCaseReviewCodeFromRawArguments(arguments []byte) (string, error) {
	decoded := make(map[string]any)
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_arguments"}).WithCause(err)
	}
	return cityRealtimeAgentDecisionCaseReviewCodeFromArguments(decoded)
}

func loadCityRealtimeCharacterCaseReviewHead(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode, caseCode string,
	forUpdate bool,
) (cityRealtimeCharacterCaseReviewHead, bool, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) || !cityRealtimeCharacterLawCaseCodeValid(caseCode) {
		return cityRealtimeCharacterCaseReviewHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT actor_code, case_code, law_event_sequence, law_event_hash,
       response_event_sequence, response_event_hash, review_revision,
       review_status, source_intent_code, filed_frame_sequence,
       resolution_due_world_time_us, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_case_review_heads
WHERE world_id = $1 AND actor_code = $2 AND case_code = $3`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head := cityRealtimeCharacterCaseReviewHead{}
	err := queryer.QueryRowContext(ctx, query, worldID, actorCode, caseCode).Scan(
		&head.ActorCode, &head.CaseCode, &head.LawEventSequence, &head.LawEventHash,
		&head.ResponseEventSequence, &head.ResponseEventHash, &head.ReviewRevision,
		&head.ReviewStatus, &head.SourceIntentCode, &head.FiledFrameSequence,
		&head.ResolutionDueWorldTimeUS, &head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterCaseReviewHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterCaseReviewHead{}, false, fmt.Errorf("load realtime character case-review head: %w", err)
	}
	if !cityRealtimeCharacterCaseReviewHeadValid(head) {
		return cityRealtimeCharacterCaseReviewHead{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_head"})
	}
	return head, true, nil
}

func validateCityRealtimeCharacterCaseReviewHeadHistory(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterCaseReviewHead,
) error {
	if worldID <= 0 || !cityRealtimeCharacterCaseReviewHeadValid(head) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_history"})
	}
	var responseSequence, responseFrameSequence, lawSequence int64
	var responseHash, lawHash, responseCode string
	err := queryer.QueryRowContext(ctx, `
SELECT response.event_sequence, response.frame_sequence, response.law_event_sequence,
       response.law_event_hash, response.event_hash, response.response_code
FROM city_realtime_character_case_response_events response
WHERE response.world_id = $1 AND response.actor_code = $2 AND response.case_code = $3`,
		worldID, head.ActorCode, head.CaseCode,
	).Scan(&responseSequence, &responseFrameSequence, &lawSequence, &lawHash, &responseHash, &responseCode)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_response"})
	}
	if err != nil {
		return fmt.Errorf("load realtime character case-review acknowledgement: %w", err)
	}
	if responseCode != cityRealtimeCharacterCaseResponseAcknowledged ||
		responseSequence != head.ResponseEventSequence || responseHash != head.ResponseEventHash ||
		lawSequence != head.LawEventSequence || lawHash != head.LawEventHash ||
		head.LastFrameSequence < responseFrameSequence {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_response"})
	}
	genesisHash, err := cityRealtimeCharacterCaseReviewChainGenesisHash(
		head.ActorCode, head.CaseCode, head.LawEventHash, head.ResponseEventHash,
	)
	if err != nil {
		return err
	}
	if head.ReviewRevision == 0 {
		if head.LastFrameSequence != responseFrameSequence || head.EventChainHash != genesisHash {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_genesis"})
		}
		return nil
	}
	lastEvent := cityRealtimeCharacterCaseReviewEvent{}
	err = queryer.QueryRowContext(ctx, `
SELECT actor_code, case_code, event_sequence, frame_sequence, event_type,
       source_intent_code, resolution_due_world_time_us, previous_event_hash, event_hash
FROM city_realtime_character_case_review_events
WHERE world_id = $1 AND actor_code = $2 AND case_code = $3
ORDER BY event_sequence DESC
LIMIT 1`, worldID, head.ActorCode, head.CaseCode).Scan(
		&lastEvent.ActorCode, &lastEvent.CaseCode, &lastEvent.EventSequence, &lastEvent.FrameSequence,
		&lastEvent.EventType, &lastEvent.SourceIntentCode, &lastEvent.ResolutionDueWorldTimeUS,
		&lastEvent.PreviousEventHash, &lastEvent.EventHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_event_head"})
	}
	if err != nil {
		return fmt.Errorf("load realtime character case-review event head: %w", err)
	}
	if !cityRealtimeCharacterCaseReviewEventValid(lastEvent) || lastEvent.EventSequence != head.ReviewRevision ||
		lastEvent.FrameSequence != head.LastFrameSequence || lastEvent.EventHash != head.EventChainHash ||
		lastEvent.SourceIntentCode != head.SourceIntentCode ||
		lastEvent.ResolutionDueWorldTimeUS != head.ResolutionDueWorldTimeUS {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_event_head"})
	}
	return nil
}

func insertCityRealtimeCharacterCaseReviewHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	head cityRealtimeCharacterCaseReviewHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseReviewHeadValid(head) || head.ReviewRevision != 0 {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_review_heads
    (world_id, actor_code, case_code, law_event_sequence, law_event_hash,
     response_event_sequence, response_event_hash, review_revision, review_status,
     source_intent_code, filed_frame_sequence, resolution_due_world_time_us,
     last_frame_sequence, event_chain_hash, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, '{}'::jsonb)`,
		worldID, head.ActorCode, head.CaseCode, head.LawEventSequence, head.LawEventHash,
		head.ResponseEventSequence, head.ResponseEventHash, head.ReviewRevision, head.ReviewStatus,
		head.SourceIntentCode, head.FiledFrameSequence, head.ResolutionDueWorldTimeUS,
		head.LastFrameSequence, head.EventChainHash, head.StateHash,
	); err != nil {
		return fmt.Errorf("insert realtime character case-review head: %w", err)
	}
	return nil
}

func insertCityRealtimeCharacterCaseReviewEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	event cityRealtimeCharacterCaseReviewEvent,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseReviewEventValid(event) || !cityRealtimeSHA256Hex(event.EventHash) {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_review_events
    (world_id, actor_code, case_code, event_sequence, frame_sequence, event_type,
     source_intent_code, resolution_due_world_time_us, previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '{}'::jsonb)`,
		worldID, event.ActorCode, event.CaseCode, event.EventSequence, event.FrameSequence,
		event.EventType, event.SourceIntentCode, event.ResolutionDueWorldTimeUS,
		event.PreviousEventHash, event.EventHash,
	); err != nil {
		return fmt.Errorf("append realtime character case-review event: %w", err)
	}
	return nil
}

func updateCityRealtimeCharacterCaseReviewHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previous, next cityRealtimeCharacterCaseReviewHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseReviewHeadValid(previous) ||
		!cityRealtimeCharacterCaseReviewHeadValid(next) || previous.ActorCode != next.ActorCode ||
		previous.CaseCode != next.CaseCode || next.ReviewRevision != previous.ReviewRevision+1 ||
		next.LastFrameSequence <= previous.LastFrameSequence || next.EventChainHash == previous.EventChainHash {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_case_review_heads
SET review_revision = $4, review_status = $5, source_intent_code = $6,
    filed_frame_sequence = $7, resolution_due_world_time_us = $8,
    last_frame_sequence = $9, event_chain_hash = $10, state_hash = $11, updated_at = NOW()
WHERE world_id = $1 AND actor_code = $2 AND case_code = $3
  AND review_revision = $12 AND review_status = $13 AND source_intent_code = $14
  AND filed_frame_sequence = $15 AND resolution_due_world_time_us = $16
  AND last_frame_sequence = $17 AND event_chain_hash = $18 AND state_hash = $19`,
		worldID, next.ActorCode, next.CaseCode, next.ReviewRevision, next.ReviewStatus, next.SourceIntentCode,
		next.FiledFrameSequence, next.ResolutionDueWorldTimeUS, next.LastFrameSequence,
		next.EventChainHash, next.StateHash,
		previous.ReviewRevision, previous.ReviewStatus, previous.SourceIntentCode,
		previous.FiledFrameSequence, previous.ResolutionDueWorldTimeUS, previous.LastFrameSequence,
		previous.EventChainHash, previous.StateHash,
	)
	if err != nil {
		return fmt.Errorf("advance realtime character case-review head: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime character case-review head update: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_revision"})
	}
	return nil
}

func cityRealtimeCharacterCaseReviewCloseDedupKey(
	actorCode, caseCode, sourceIntentCode string,
) (string, error) {
	if !cityRealtimePlayerActorCodeValid(actorCode) || !cityRealtimeCharacterLawCaseCodeValid(caseCode) ||
		!cityRealtimeAgentIdentifierValid(sourceIntentCode, 96) {
		return "", ErrCityInvalidInput
	}
	key := "case-review-close." + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseReviewCloseVersion,
		actorCode,
		caseCode,
		sourceIntentCode,
	}, "\x1f")))
	if !cityRealtimeDueEventIdentifierValid(key, 160) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_dedup"})
	}
	return key, nil
}

func cityRealtimeCharacterCaseReviewAggregateKey(actorCode, caseCode string) (string, error) {
	if !cityRealtimePlayerActorCodeValid(actorCode) || !cityRealtimeCharacterLawCaseCodeValid(caseCode) {
		return "", ErrCityInvalidInput
	}
	key := "case-review:" + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseReviewCloseVersion,
		actorCode,
		caseCode,
	}, "\x1f")))
	if !cityRealtimeDueEventIdentifierValid(key, 160) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_aggregate"})
	}
	return key, nil
}

func cityRealtimeCharacterCaseReviewResolutionDueWorldTime(
	currentWorldTimeUS int64,
) (int64, error) {
	if currentWorldTimeUS < 0 || currentWorldTimeUS > cityRealtimeMaximumWorldTimeUS-cityRealtimeCharacterCaseReviewClosureDelayUS {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_due_time"})
	}
	dueWorldTimeUS := currentWorldTimeUS + cityRealtimeCharacterCaseReviewClosureDelayUS
	if dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_due_time"})
	}
	return dueWorldTimeUS, nil
}

func scheduleCityRealtimeCharacterCaseReviewCloseDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, dueWorldTimeUS, createdFrameSequence int64,
	item cityRealtimeCharacterReviewableLawCase,
	sourceIntentCode string,
) error {
	if tx == nil || worldID <= 0 || dueWorldTimeUS <= 0 ||
		dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 || createdFrameSequence <= 0 ||
		!cityRealtimeCharacterReviewableLawCaseValid(item) || !cityRealtimeAgentIdentifierValid(sourceIntentCode, 96) {
		return ErrCityInvalidInput
	}
	dedupKey, err := cityRealtimeCharacterCaseReviewCloseDedupKey(item.ActorCode, item.CaseCode, sourceIntentCode)
	if err != nil {
		return err
	}
	aggregateKey, err := cityRealtimeCharacterCaseReviewAggregateKey(item.ActorCode, item.CaseCode)
	if err != nil {
		return err
	}
	payload, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":     cityRealtimeCharacterCaseReviewSchemaVersion,
		"actor_code":         item.ActorCode,
		"case_code":          item.CaseCode,
		"source_intent_code": sourceIntentCode,
	})
	if err != nil {
		return fmt.Errorf("canonicalize realtime character case-review closure payload: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_due_events
    (world_id, event_type, schema_version, due_world_time_us, temporal_phase,
     priority, aggregate_type, aggregate_key, dedup_key, source_kind,
     source_reference, payload, payload_hash, expected_version, status,
     created_frame_sequence)
VALUES ($1, $2, 1, $3, 'rule_effect', $4, 'realtime_case_review', $5, $6, 'system',
        'realtime_character_case_review', $7::jsonb, $8, 1, 'pending', $9)`,
		worldID, cityRealtimeDueEventTypeCharacterCaseReviewClose, dueWorldTimeUS,
		cityRealtimeCharacterCaseReviewClosePriority, aggregateKey, dedupKey,
		[]byte(payload), payloadHash, createdFrameSequence,
	); err != nil {
		return fmt.Errorf("schedule realtime character case-review closure: %w", err)
	}
	return nil
}

func decodeCityRealtimeCharacterCaseReviewCloseDuePayload(
	event cityRealtimeDueEventRecord,
) (cityRealtimeCharacterCaseReviewCloseDuePayload, bool) {
	payload := cityRealtimeCharacterCaseReviewCloseDuePayload{}
	if err := decodeStrictCityObject(event.Payload, &payload); err != nil ||
		payload.SchemaVersion != cityRealtimeCharacterCaseReviewSchemaVersion ||
		!cityRealtimePlayerActorCodeValid(payload.ActorCode) ||
		!cityRealtimeCharacterLawCaseCodeValid(payload.CaseCode) ||
		!cityRealtimeAgentIdentifierValid(payload.SourceIntentCode, 96) {
		return cityRealtimeCharacterCaseReviewCloseDuePayload{}, false
	}
	_, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":     payload.SchemaVersion,
		"actor_code":         payload.ActorCode,
		"case_code":          payload.CaseCode,
		"source_intent_code": payload.SourceIntentCode,
	})
	if err != nil || payloadHash != event.PayloadHash {
		return cityRealtimeCharacterCaseReviewCloseDuePayload{}, false
	}
	return payload, true
}

// applyCityRealtimeCharacterCaseReviewCloseDueEvent closes exactly one
// previously-filed review after its server-owned delay. The only legal outcome
// in this first procedural slice is closed_no_change: it cannot revise the
// originating Law fact, its penalty, city-credit, inventory, or wallet.
func applyCityRealtimeCharacterCaseReviewCloseDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	event cityRealtimeDueEventRecord,
) (bool, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 ||
		event.EventType != cityRealtimeDueEventTypeCharacterCaseReviewClose ||
		event.SchemaVersion != cityRealtimeCharacterCaseReviewSchemaVersion ||
		event.SourceKind != cityRealtimeDueEventSourceKindSystem || event.TemporalPhase != "rule_effect" ||
		event.AggregateType != "realtime_case_review" ||
		event.SourceReference != "realtime_character_case_review" ||
		event.ExpectedVersion == nil || *event.ExpectedVersion != 1 {
		return false, nil
	}
	payload, validPayload := decodeCityRealtimeCharacterCaseReviewCloseDuePayload(event)
	if !validPayload {
		return false, nil
	}
	expectedAggregateKey, aggregateErr := cityRealtimeCharacterCaseReviewAggregateKey(payload.ActorCode, payload.CaseCode)
	if aggregateErr != nil || event.AggregateKey != expectedAggregateKey {
		return false, nil
	}
	expectedDedupKey, dedupErr := cityRealtimeCharacterCaseReviewCloseDedupKey(
		payload.ActorCode, payload.CaseCode, payload.SourceIntentCode,
	)
	if dedupErr != nil || event.DedupKey != expectedDedupKey {
		return false, nil
	}
	binding, err := loadCityRealtimeCharacterCaseReviewBinding(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if binding == nil {
		return false, nil
	}
	head, found, err := loadCityRealtimeCharacterCaseReviewHead(
		ctx, tx, worldID, payload.ActorCode, payload.CaseCode, true,
	)
	if err != nil {
		return false, err
	}
	if !found || head.ReviewRevision != 1 || head.ReviewStatus != cityRealtimeCharacterCaseReviewFiled ||
		head.SourceIntentCode != payload.SourceIntentCode || head.ResolutionDueWorldTimeUS != event.DueWorldTimeUS {
		return false, nil
	}
	if err = validateCityRealtimeCharacterCaseReviewHeadHistory(ctx, tx, worldID, head); err != nil {
		return false, err
	}
	nextHead, reviewEvent, transitionErr := cityRealtimeCharacterCloseCaseReview(head, frameSequence, event.DueWorldTimeUS)
	if transitionErr != nil {
		return false, transitionErr
	}
	if err = enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); err != nil {
		return false, err
	}
	if err = enableCityRealtimeCharacterCaseReviewMutationGate(ctx, tx, worldID, frameSequence); err != nil {
		return false, err
	}
	if err = insertCityRealtimeCharacterCaseReviewEvent(ctx, tx, worldID, reviewEvent); err != nil {
		return false, err
	}
	if err = updateCityRealtimeCharacterCaseReviewHead(ctx, tx, worldID, head, nextHead); err != nil {
		return false, err
	}
	return true, nil
}

func loadCityRealtimeCharacterCaseReviewHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseReviewHashState, error) {
	binding, err := loadCityRealtimeCharacterCaseReviewBinding(ctx, queryer, worldID)
	if err != nil || binding == nil {
		return nil, err
	}
	state := &cityRealtimeCharacterCaseReviewHashState{
		SchemaVersion: cityRealtimeCharacterCaseReviewSchemaVersion,
		Binding:       binding,
		Heads:         make([]cityRealtimeCharacterCaseReviewHead, 0),
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code, case_code, law_event_sequence, law_event_hash,
       response_event_sequence, response_event_hash, review_revision,
       review_status, source_intent_code, filed_frame_sequence,
       resolution_due_world_time_us, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_case_review_heads
WHERE world_id = $1
ORDER BY actor_code ASC, case_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-review heads: %w", err)
	}
	heads := make([]cityRealtimeCharacterCaseReviewHead, 0)
	for rows.Next() {
		head := cityRealtimeCharacterCaseReviewHead{}
		if err = rows.Scan(
			&head.ActorCode, &head.CaseCode, &head.LawEventSequence, &head.LawEventHash,
			&head.ResponseEventSequence, &head.ResponseEventHash, &head.ReviewRevision,
			&head.ReviewStatus, &head.SourceIntentCode, &head.FiledFrameSequence,
			&head.ResolutionDueWorldTimeUS, &head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		heads = append(heads, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character case-review heads: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character case-review heads: %w", err)
	}
	for _, head := range heads {
		if err = validateCityRealtimeCharacterCaseReviewHeadHistory(ctx, queryer, worldID, head); err != nil {
			return nil, err
		}
		state.Heads = append(state.Heads, head)
	}
	if err = validateCityRealtimeCharacterCaseReviewHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeCharacterCaseReviewHashState(state *cityRealtimeCharacterCaseReviewHashState) error {
	if state == nil || state.SchemaVersion != cityRealtimeCharacterCaseReviewSchemaVersion || state.Binding == nil ||
		state.Heads == nil || !validateCityRealtimeCharacterCaseReviewBinding(*state.Binding) {
		return errors.New("invalid realtime character case-review hash state")
	}
	for index, head := range state.Heads {
		if !cityRealtimeCharacterCaseReviewHeadValid(head) {
			return errors.New("invalid realtime character case-review head")
		}
		if index > 0 {
			previous := state.Heads[index-1]
			if previous.ActorCode > head.ActorCode ||
				(previous.ActorCode == head.ActorCode && previous.CaseCode >= head.CaseCode) {
				return errors.New("unordered realtime character case-review heads")
			}
		}
	}
	return nil
}
