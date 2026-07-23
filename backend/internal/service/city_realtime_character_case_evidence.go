package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	cityRealtimeCharacterCaseEvidenceSchemaVersion  = 1
	cityRealtimeCharacterCaseEvidenceBindingVersion = "city-realtime-character-case-evidence-binding-v1"
	cityRealtimeCharacterCaseEvidenceStateVersion   = "city-realtime-character-case-evidence-state-v1"
	cityRealtimeCharacterCaseEvidenceChainVersion   = "city-realtime-character-case-evidence-chain-v1"
	cityRealtimeCharacterCaseEvidenceEventVersion   = "city-realtime-character-case-evidence-event-v1"
	cityRealtimeCharacterCaseEvidenceExpiryVersion  = "city-realtime-character-case-evidence-expiry-v1"

	// This first source is deliberately narrow: the sealed activity reducer
	// already created the referenced Law event before this adapter writes a
	// handle. A report, prompt, model response, or browser request cannot make
	// one of these records.
	cityRealtimeCharacterCaseEvidenceSourceSealedLawEvent = "server.sealed_law_event"
	cityRealtimeCharacterCaseEvidenceActive               = "active"
	cityRealtimeCharacterCaseEvidenceExpired              = "expired"
	cityRealtimeCharacterCaseEvidenceCaptured             = "source_captured"
	cityRealtimeCharacterCaseEvidenceExpiredEvent         = "source_expired"

	// The handle is intentionally short lived. Expiry revokes only the future
	// usability of this evidence handle; it never deletes or rewrites the Law
	// event that remains independently replayable in the activity ledger.
	cityRealtimeCharacterCaseEvidenceExpiryDelayUS  int64 = 30 * cityRealtimeTimeQuantumUS
	cityRealtimeCharacterCaseEvidenceExpiryPriority       = 110

	cityRealtimeDueEventTypeCharacterCaseEvidenceExpire = "system.realtime.character_case_evidence_expire"
)

// cityRealtimeCharacterCaseEvidenceBinding is a versioned, server-only
// adapter binding. It is not an Agent authorization grant and carries no
// source content, provider route, prompt, or user-provided assertion.
type cityRealtimeCharacterCaseEvidenceBinding struct {
	SchemaVersion    int    `json:"schema_version"`
	AgentBindingHash string `json:"agent_binding_hash"`
	BindingHash      string `json:"binding_hash"`
}

// cityRealtimeCharacterCaseEvidenceHead is a bounded handle over one sealed
// Law event. Its source fields are sufficient to verify provenance without
// copying Rule text, Case notes, model output, report text, or account data.
// A later Case-process adapter may only consume an active handle through an
// explicit, separately versioned assignment step.
type cityRealtimeCharacterCaseEvidenceHead struct {
	EvidenceCode             string `json:"evidence_code"`
	SubjectActorCode         string `json:"subject_actor_code"`
	SourceKind               string `json:"source_kind"`
	SourceLawEventSequence   int64  `json:"source_law_event_sequence"`
	SourceLawEventHash       string `json:"source_law_event_hash"`
	SourceFrameSequence      int64  `json:"source_frame_sequence"`
	EvidenceRevision         int64  `json:"evidence_revision"`
	EvidenceStatus           string `json:"evidence_status"`
	CapturedFrameSequence    int64  `json:"captured_frame_sequence"`
	ExpirationDueWorldTimeUS int64  `json:"expiration_due_world_time_us"`
	LastFrameSequence        int64  `json:"last_frame_sequence"`
	EventChainHash           string `json:"event_chain_hash"`
	StateHash                string `json:"state_hash"`
}

// cityRealtimeCharacterCaseEvidenceEvent is an append-only evidence-handle
// lifecycle record. It cannot express an accusation, verdict, penalty,
// reward, wallet mutation, source payload, or free-form explanation.
type cityRealtimeCharacterCaseEvidenceEvent struct {
	EvidenceCode             string
	SubjectActorCode         string
	SourceKind               string
	SourceLawEventSequence   int64
	SourceLawEventHash       string
	SourceFrameSequence      int64
	EventSequence            int64
	FrameSequence            int64
	EventType                string
	ExpirationDueWorldTimeUS int64
	PreviousEventHash        string
	EventHash                string
}

type cityRealtimeCharacterCaseEvidenceHashState struct {
	SchemaVersion int                                       `json:"schema_version"`
	Binding       *cityRealtimeCharacterCaseEvidenceBinding `json:"binding,omitempty"`
	Heads         []cityRealtimeCharacterCaseEvidenceHead   `json:"heads"`
}

type cityRealtimeCharacterCaseEvidenceExpiryDuePayload struct {
	SchemaVersion          int    `json:"schema_version"`
	EvidenceCode           string `json:"evidence_code"`
	SubjectActorCode       string `json:"subject_actor_code"`
	SourceLawEventHash     string `json:"source_law_event_hash"`
	SourceLawEventSequence int64  `json:"source_law_event_sequence"`
}

func cityRealtimeCharacterCaseEvidenceBindingHash(agentBindingHash string) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseEvidenceBindingVersion,
		agentBindingHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseEvidenceCode(sourceLawEventHash string) (string, error) {
	if !cityRealtimeSHA256Hex(sourceLawEventHash) {
		return "", ErrCityInvalidInput
	}
	code := "evidence.law." + sourceLawEventHash
	if !cityRealtimeAgentIdentifierValid(code, 96) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_code"})
	}
	return code, nil
}

func cityRealtimeCharacterCaseEvidenceChainGenesisHash(head cityRealtimeCharacterCaseEvidenceHead) (string, error) {
	if !cityRealtimeCharacterCaseEvidenceSourceFieldsValid(head) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseEvidenceChainVersion,
		head.EvidenceCode,
		head.SubjectActorCode,
		head.SourceKind,
		strconv.FormatInt(head.SourceLawEventSequence, 10),
		head.SourceLawEventHash,
		strconv.FormatInt(head.SourceFrameSequence, 10),
	}, "\x1f"))), nil
}

func cityRealtimeCharacterCaseEvidenceSourceFieldsValid(head cityRealtimeCharacterCaseEvidenceHead) bool {
	expectedCode, err := cityRealtimeCharacterCaseEvidenceCode(head.SourceLawEventHash)
	return err == nil && head.EvidenceCode == expectedCode &&
		cityRealtimePlayerActorCodeValid(head.SubjectActorCode) &&
		head.SourceKind == cityRealtimeCharacterCaseEvidenceSourceSealedLawEvent &&
		head.SourceLawEventSequence > 0 && cityRealtimeSHA256Hex(head.SourceLawEventHash) &&
		head.SourceFrameSequence > 0
}

func cityRealtimeCharacterCaseEvidenceHeadStateHashUnchecked(head cityRealtimeCharacterCaseEvidenceHead) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseEvidenceStateVersion,
		head.EvidenceCode,
		head.SubjectActorCode,
		head.SourceKind,
		strconv.FormatInt(head.SourceLawEventSequence, 10),
		head.SourceLawEventHash,
		strconv.FormatInt(head.SourceFrameSequence, 10),
		strconv.FormatInt(head.EvidenceRevision, 10),
		head.EvidenceStatus,
		strconv.FormatInt(head.CapturedFrameSequence, 10),
		strconv.FormatInt(head.ExpirationDueWorldTimeUS, 10),
		strconv.FormatInt(head.LastFrameSequence, 10),
		head.EventChainHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseEvidenceHeadValid(head cityRealtimeCharacterCaseEvidenceHead) bool {
	if !cityRealtimeCharacterCaseEvidenceSourceFieldsValid(head) ||
		head.CapturedFrameSequence <= 0 || head.ExpirationDueWorldTimeUS <= 0 ||
		head.ExpirationDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 ||
		head.LastFrameSequence < head.CapturedFrameSequence ||
		!cityRealtimeSHA256Hex(head.EventChainHash) || !cityRealtimeSHA256Hex(head.StateHash) {
		return false
	}
	switch head.EvidenceRevision {
	case 1:
		if head.EvidenceStatus != cityRealtimeCharacterCaseEvidenceActive ||
			head.LastFrameSequence != head.CapturedFrameSequence {
			return false
		}
	case 2:
		if head.EvidenceStatus != cityRealtimeCharacterCaseEvidenceExpired ||
			head.LastFrameSequence <= head.CapturedFrameSequence {
			return false
		}
	default:
		return false
	}
	return head.StateHash == cityRealtimeCharacterCaseEvidenceHeadStateHashUnchecked(head)
}

func cityRealtimeCharacterCaseEvidenceEventValid(event cityRealtimeCharacterCaseEvidenceEvent) bool {
	code, err := cityRealtimeCharacterCaseEvidenceCode(event.SourceLawEventHash)
	if err != nil || event.EvidenceCode != code || !cityRealtimePlayerActorCodeValid(event.SubjectActorCode) ||
		event.SourceKind != cityRealtimeCharacterCaseEvidenceSourceSealedLawEvent ||
		event.SourceLawEventSequence <= 0 || event.SourceFrameSequence <= 0 ||
		event.EventSequence <= 0 || event.FrameSequence <= 0 ||
		event.ExpirationDueWorldTimeUS <= 0 || event.ExpirationDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 ||
		!cityRealtimeSHA256Hex(event.PreviousEventHash) ||
		(event.EventHash != "" && !cityRealtimeSHA256Hex(event.EventHash)) {
		return false
	}
	return (event.EventSequence == 1 && event.EventType == cityRealtimeCharacterCaseEvidenceCaptured) ||
		(event.EventSequence == 2 && event.EventType == cityRealtimeCharacterCaseEvidenceExpiredEvent)
}

func cityRealtimeCharacterCaseEvidenceEventHash(event cityRealtimeCharacterCaseEvidenceEvent) (string, error) {
	if !cityRealtimeCharacterCaseEvidenceEventValid(event) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseEvidenceEventVersion,
		event.EvidenceCode,
		event.SubjectActorCode,
		event.SourceKind,
		strconv.FormatInt(event.SourceLawEventSequence, 10),
		event.SourceLawEventHash,
		strconv.FormatInt(event.SourceFrameSequence, 10),
		strconv.FormatInt(event.EventSequence, 10),
		strconv.FormatInt(event.FrameSequence, 10),
		event.EventType,
		strconv.FormatInt(event.ExpirationDueWorldTimeUS, 10),
		event.PreviousEventHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterCaseEvidenceLawSourceValid(law cityRealtimeCharacterLawEventRecord) bool {
	if !cityRealtimePlayerActorCodeValid(law.ActorCode) || law.EventSequence <= 0 || law.FrameSequence <= 0 ||
		!cityRealtimeSHA256Hex(law.EventHash) {
		return false
	}
	expectedHash, err := cityRealtimeCharacterLawEventHash(law)
	return err == nil && expectedHash == law.EventHash
}

func cityRealtimeCharacterCaptureCaseEvidenceFromLaw(
	law cityRealtimeCharacterLawEventRecord,
	expirationDueWorldTimeUS int64,
) (cityRealtimeCharacterCaseEvidenceHead, cityRealtimeCharacterCaseEvidenceEvent, error) {
	if !cityRealtimeCharacterCaseEvidenceLawSourceValid(law) || expirationDueWorldTimeUS <= 0 ||
		expirationDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return cityRealtimeCharacterCaseEvidenceHead{}, cityRealtimeCharacterCaseEvidenceEvent{}, ErrCityInvalidInput
	}
	code, err := cityRealtimeCharacterCaseEvidenceCode(law.EventHash)
	if err != nil {
		return cityRealtimeCharacterCaseEvidenceHead{}, cityRealtimeCharacterCaseEvidenceEvent{}, err
	}
	head := cityRealtimeCharacterCaseEvidenceHead{
		EvidenceCode:             code,
		SubjectActorCode:         law.ActorCode,
		SourceKind:               cityRealtimeCharacterCaseEvidenceSourceSealedLawEvent,
		SourceLawEventSequence:   law.EventSequence,
		SourceLawEventHash:       law.EventHash,
		SourceFrameSequence:      law.FrameSequence,
		EvidenceRevision:         1,
		EvidenceStatus:           cityRealtimeCharacterCaseEvidenceActive,
		CapturedFrameSequence:    law.FrameSequence,
		ExpirationDueWorldTimeUS: expirationDueWorldTimeUS,
		LastFrameSequence:        law.FrameSequence,
	}
	genesisHash, err := cityRealtimeCharacterCaseEvidenceChainGenesisHash(head)
	if err != nil {
		return cityRealtimeCharacterCaseEvidenceHead{}, cityRealtimeCharacterCaseEvidenceEvent{}, err
	}
	event := cityRealtimeCharacterCaseEvidenceEvent{
		EvidenceCode:             head.EvidenceCode,
		SubjectActorCode:         head.SubjectActorCode,
		SourceKind:               head.SourceKind,
		SourceLawEventSequence:   head.SourceLawEventSequence,
		SourceLawEventHash:       head.SourceLawEventHash,
		SourceFrameSequence:      head.SourceFrameSequence,
		EventSequence:            1,
		FrameSequence:            law.FrameSequence,
		EventType:                cityRealtimeCharacterCaseEvidenceCaptured,
		ExpirationDueWorldTimeUS: head.ExpirationDueWorldTimeUS,
		PreviousEventHash:        genesisHash,
	}
	event.EventHash, err = cityRealtimeCharacterCaseEvidenceEventHash(event)
	if err != nil {
		return cityRealtimeCharacterCaseEvidenceHead{}, cityRealtimeCharacterCaseEvidenceEvent{}, err
	}
	head.EventChainHash = event.EventHash
	head.StateHash = cityRealtimeCharacterCaseEvidenceHeadStateHashUnchecked(head)
	if !cityRealtimeCharacterCaseEvidenceHeadValid(head) {
		return cityRealtimeCharacterCaseEvidenceHead{}, cityRealtimeCharacterCaseEvidenceEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_capture"})
	}
	return head, event, nil
}

func cityRealtimeCharacterExpireCaseEvidence(
	previous cityRealtimeCharacterCaseEvidenceHead,
	frameSequence, dueWorldTimeUS int64,
) (cityRealtimeCharacterCaseEvidenceHead, cityRealtimeCharacterCaseEvidenceEvent, error) {
	if !cityRealtimeCharacterCaseEvidenceHeadValid(previous) || previous.EvidenceRevision != 1 ||
		previous.EvidenceStatus != cityRealtimeCharacterCaseEvidenceActive ||
		frameSequence <= previous.LastFrameSequence || dueWorldTimeUS != previous.ExpirationDueWorldTimeUS {
		return cityRealtimeCharacterCaseEvidenceHead{}, cityRealtimeCharacterCaseEvidenceEvent{}, ErrCityInvalidInput
	}
	event := cityRealtimeCharacterCaseEvidenceEvent{
		EvidenceCode:             previous.EvidenceCode,
		SubjectActorCode:         previous.SubjectActorCode,
		SourceKind:               previous.SourceKind,
		SourceLawEventSequence:   previous.SourceLawEventSequence,
		SourceLawEventHash:       previous.SourceLawEventHash,
		SourceFrameSequence:      previous.SourceFrameSequence,
		EventSequence:            2,
		FrameSequence:            frameSequence,
		EventType:                cityRealtimeCharacterCaseEvidenceExpiredEvent,
		ExpirationDueWorldTimeUS: dueWorldTimeUS,
		PreviousEventHash:        previous.EventChainHash,
	}
	var err error
	event.EventHash, err = cityRealtimeCharacterCaseEvidenceEventHash(event)
	if err != nil {
		return cityRealtimeCharacterCaseEvidenceHead{}, cityRealtimeCharacterCaseEvidenceEvent{}, err
	}
	next := previous
	next.EvidenceRevision = event.EventSequence
	next.EvidenceStatus = cityRealtimeCharacterCaseEvidenceExpired
	next.LastFrameSequence = frameSequence
	next.EventChainHash = event.EventHash
	next.StateHash = cityRealtimeCharacterCaseEvidenceHeadStateHashUnchecked(next)
	if !cityRealtimeCharacterCaseEvidenceHeadValid(next) {
		return cityRealtimeCharacterCaseEvidenceHead{}, cityRealtimeCharacterCaseEvidenceEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_expire"})
	}
	return next, event, nil
}

func initializeCityRealtimeCharacterCaseEvidenceFoundation(
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
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_policy"})
	}
	if !cityRealtimeAgentCharacterCaseEvidenceRuntimeEnabled(*agentState.Binding) {
		return nil
	}
	intakeBinding, err := loadCityRealtimeCharacterCaseIntakeBinding(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if intakeBinding == nil || intakeBinding.AgentBindingHash != agentState.Binding.BindingHash {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_intake_scope"})
	}
	binding := cityRealtimeCharacterCaseEvidenceBinding{
		SchemaVersion:    cityRealtimeCharacterCaseEvidenceSchemaVersion,
		AgentBindingHash: agentState.Binding.BindingHash,
	}
	binding.BindingHash = cityRealtimeCharacterCaseEvidenceBindingHash(binding.AgentBindingHash)
	if !cityRealtimeCharacterCaseEvidenceBindingValid(binding) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_binding"})
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_character_case_evidence_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("activate realtime character case-evidence initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_evidence_world_bindings
    (world_id, schema_version, agent_binding_hash, binding_hash, metadata)
VALUES ($1, $2, $3, $4, '{}'::jsonb)`,
		worldID, binding.SchemaVersion, binding.AgentBindingHash, binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime character case-evidence binding: %w", err)
	}
	return nil
}

func enableCityRealtimeCharacterCaseEvidenceMutationGate(
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
		{name: "sub2api.city_realtime_character_case_evidence_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_case_evidence_frame_sequence", value: frameSequence},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character case-evidence gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func cityRealtimeCharacterCaseEvidenceBindingValid(binding cityRealtimeCharacterCaseEvidenceBinding) bool {
	return binding.SchemaVersion == cityRealtimeCharacterCaseEvidenceSchemaVersion &&
		cityRealtimeSHA256Hex(binding.AgentBindingHash) && cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeCharacterCaseEvidenceBindingHash(binding.AgentBindingHash)
}

func loadCityRealtimeCharacterCaseEvidenceBinding(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseEvidenceBinding, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterCaseEvidenceBinding{}
	var policyID, policyVersion, agentBindingHash string
	err := queryer.QueryRowContext(ctx, `
SELECT evidence.schema_version, evidence.agent_binding_hash, evidence.binding_hash,
       agent.policy_id, agent.policy_version, agent.binding_hash
FROM city_realtime_character_case_evidence_world_bindings evidence
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = evidence.world_id
WHERE evidence.world_id = $1`, worldID).Scan(
		&binding.SchemaVersion, &binding.AgentBindingHash, &binding.BindingHash,
		&policyID, &policyVersion, &agentBindingHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-evidence binding: %w", err)
	}
	if policyID != cityRealtimeAgentCorePolicyID ||
		(policyVersion != cityRealtimeAgentCorePolicyVersionEvidence &&
			policyVersion != cityRealtimeAgentCorePolicyVersionEvidenceAssignment &&
			policyVersion != cityRealtimeAgentCorePolicyVersionProcedureDispatch &&
			policyVersion != cityRealtimeAgentCorePolicyVersionTask &&
			policyVersion != cityRealtimeAgentCorePolicyVersionNavigationPlan) ||
		binding.AgentBindingHash != agentBindingHash || !cityRealtimeCharacterCaseEvidenceBindingValid(binding) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_binding"})
	}
	return &binding, nil
}

func loadCityRealtimeCharacterCaseEvidenceHead(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	evidenceCode string,
	forUpdate bool,
) (cityRealtimeCharacterCaseEvidenceHead, bool, error) {
	if worldID <= 0 || !cityRealtimeAgentIdentifierValid(evidenceCode, 96) {
		return cityRealtimeCharacterCaseEvidenceHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT evidence_code, subject_actor_code, source_kind, source_law_event_sequence,
       source_law_event_hash, source_frame_sequence, evidence_revision,
       evidence_status, captured_frame_sequence, expiration_due_world_time_us,
       last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_case_evidence_heads
WHERE world_id = $1 AND evidence_code = $2`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head := cityRealtimeCharacterCaseEvidenceHead{}
	err := queryer.QueryRowContext(ctx, query, worldID, evidenceCode).Scan(
		&head.EvidenceCode, &head.SubjectActorCode, &head.SourceKind, &head.SourceLawEventSequence,
		&head.SourceLawEventHash, &head.SourceFrameSequence, &head.EvidenceRevision,
		&head.EvidenceStatus, &head.CapturedFrameSequence, &head.ExpirationDueWorldTimeUS,
		&head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterCaseEvidenceHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterCaseEvidenceHead{}, false, fmt.Errorf("load realtime character case-evidence head: %w", err)
	}
	if !cityRealtimeCharacterCaseEvidenceHeadValid(head) {
		return cityRealtimeCharacterCaseEvidenceHead{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_head"})
	}
	return head, true, nil
}

func validateCityRealtimeCharacterCaseEvidenceHeadHistory(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterCaseEvidenceHead,
) error {
	if worldID <= 0 || !cityRealtimeCharacterCaseEvidenceHeadValid(head) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_history"})
	}
	var sourceActorCode, sourceEventHash string
	var sourceEventSequence, sourceFrameSequence int64
	err := queryer.QueryRowContext(ctx, `
SELECT actor_code, event_sequence, frame_sequence, event_hash
FROM city_realtime_character_law_events
WHERE world_id = $1 AND actor_code = $2 AND event_sequence = $3`,
		worldID, head.SubjectActorCode, head.SourceLawEventSequence,
	).Scan(&sourceActorCode, &sourceEventSequence, &sourceFrameSequence, &sourceEventHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_source"})
	}
	if err != nil {
		return fmt.Errorf("load realtime character case-evidence source: %w", err)
	}
	if sourceActorCode != head.SubjectActorCode || sourceEventSequence != head.SourceLawEventSequence ||
		sourceFrameSequence != head.SourceFrameSequence || sourceEventHash != head.SourceLawEventHash {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_source"})
	}
	genesisHash, err := cityRealtimeCharacterCaseEvidenceChainGenesisHash(head)
	if err != nil {
		return err
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT evidence_code, subject_actor_code, source_kind, source_law_event_sequence,
       source_law_event_hash, source_frame_sequence, event_sequence,
       frame_sequence, event_type, expiration_due_world_time_us,
       previous_event_hash, event_hash
FROM city_realtime_character_case_evidence_events
WHERE world_id = $1 AND evidence_code = $2
ORDER BY event_sequence ASC`, worldID, head.EvidenceCode)
	if err != nil {
		return fmt.Errorf("load realtime character case-evidence history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	previousHash := genesisHash
	eventCount := int64(0)
	var last cityRealtimeCharacterCaseEvidenceEvent
	for rows.Next() {
		event := cityRealtimeCharacterCaseEvidenceEvent{}
		if err = rows.Scan(
			&event.EvidenceCode, &event.SubjectActorCode, &event.SourceKind, &event.SourceLawEventSequence,
			&event.SourceLawEventHash, &event.SourceFrameSequence, &event.EventSequence,
			&event.FrameSequence, &event.EventType, &event.ExpirationDueWorldTimeUS,
			&event.PreviousEventHash, &event.EventHash,
		); err != nil {
			return err
		}
		eventCount++
		expectedHash, hashErr := cityRealtimeCharacterCaseEvidenceEventHash(event)
		if hashErr != nil || !cityRealtimeCharacterCaseEvidenceEventValid(event) ||
			event.EventSequence != eventCount || event.PreviousEventHash != previousHash ||
			event.EventHash != expectedHash || event.SubjectActorCode != head.SubjectActorCode ||
			event.SourceKind != head.SourceKind || event.SourceLawEventSequence != head.SourceLawEventSequence ||
			event.SourceLawEventHash != head.SourceLawEventHash || event.SourceFrameSequence != head.SourceFrameSequence ||
			event.ExpirationDueWorldTimeUS != head.ExpirationDueWorldTimeUS {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_event_chain"})
		}
		previousHash = event.EventHash
		last = event
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate realtime character case-evidence history: %w", err)
	}
	if eventCount != head.EvidenceRevision || eventCount == 0 || last.EventHash != head.EventChainHash ||
		last.FrameSequence != head.LastFrameSequence ||
		(last.EventSequence == 1 && last.EventType != cityRealtimeCharacterCaseEvidenceCaptured) ||
		(last.EventSequence == 2 && last.EventType != cityRealtimeCharacterCaseEvidenceExpiredEvent) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_event_head"})
	}
	return nil
}

func insertCityRealtimeCharacterCaseEvidenceHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	head cityRealtimeCharacterCaseEvidenceHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseEvidenceHeadValid(head) || head.EvidenceRevision != 1 {
		return ErrCityInvalidInput
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_evidence_heads
    (world_id, evidence_code, subject_actor_code, source_kind,
     source_law_event_sequence, source_law_event_hash, source_frame_sequence,
     evidence_revision, evidence_status, captured_frame_sequence,
     expiration_due_world_time_us, last_frame_sequence, event_chain_hash,
     state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, '{}'::jsonb)`,
		worldID, head.EvidenceCode, head.SubjectActorCode, head.SourceKind,
		head.SourceLawEventSequence, head.SourceLawEventHash, head.SourceFrameSequence,
		head.EvidenceRevision, head.EvidenceStatus, head.CapturedFrameSequence,
		head.ExpirationDueWorldTimeUS, head.LastFrameSequence, head.EventChainHash, head.StateHash,
	)
	if err != nil {
		return fmt.Errorf("insert realtime character case-evidence head: %w", err)
	}
	return nil
}

func updateCityRealtimeCharacterCaseEvidenceHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previous, next cityRealtimeCharacterCaseEvidenceHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseEvidenceHeadValid(previous) ||
		!cityRealtimeCharacterCaseEvidenceHeadValid(next) || previous.EvidenceCode != next.EvidenceCode ||
		next.EvidenceRevision != previous.EvidenceRevision+1 ||
		next.LastFrameSequence <= previous.LastFrameSequence {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_case_evidence_heads
SET evidence_revision = $3, evidence_status = $4, last_frame_sequence = $5,
    event_chain_hash = $6, state_hash = $7, updated_at = NOW()
WHERE world_id = $1 AND evidence_code = $2
  AND evidence_revision = $8 AND evidence_status = $9
  AND last_frame_sequence = $10 AND event_chain_hash = $11 AND state_hash = $12`,
		worldID, next.EvidenceCode, next.EvidenceRevision, next.EvidenceStatus,
		next.LastFrameSequence, next.EventChainHash, next.StateHash,
		previous.EvidenceRevision, previous.EvidenceStatus, previous.LastFrameSequence,
		previous.EventChainHash, previous.StateHash,
	)
	if err != nil {
		return fmt.Errorf("update realtime character case-evidence head: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check realtime character case-evidence head update: %w", err)
	}
	if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_revision"})
	}
	return nil
}

func insertCityRealtimeCharacterCaseEvidenceEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	event cityRealtimeCharacterCaseEvidenceEvent,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseEvidenceEventValid(event) ||
		!cityRealtimeSHA256Hex(event.EventHash) {
		return ErrCityInvalidInput
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_evidence_events
    (world_id, evidence_code, subject_actor_code, source_kind,
     source_law_event_sequence, source_law_event_hash, source_frame_sequence,
     event_sequence, frame_sequence, event_type, expiration_due_world_time_us,
     previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, '{}'::jsonb)`,
		worldID, event.EvidenceCode, event.SubjectActorCode, event.SourceKind,
		event.SourceLawEventSequence, event.SourceLawEventHash, event.SourceFrameSequence,
		event.EventSequence, event.FrameSequence, event.EventType,
		event.ExpirationDueWorldTimeUS, event.PreviousEventHash, event.EventHash,
	)
	if err != nil {
		return fmt.Errorf("append realtime character case-evidence event: %w", err)
	}
	return nil
}

func cityRealtimeCharacterCaseEvidenceExpirationDueWorldTime(currentWorldTimeUS int64) (int64, error) {
	if currentWorldTimeUS < 0 ||
		currentWorldTimeUS > cityRealtimeMaximumWorldTimeUS-cityRealtimeCharacterCaseEvidenceExpiryDelayUS {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_due_time"})
	}
	dueWorldTimeUS := currentWorldTimeUS + cityRealtimeCharacterCaseEvidenceExpiryDelayUS
	if dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_due_time"})
	}
	return dueWorldTimeUS, nil
}

func cityRealtimeCharacterCaseEvidenceAggregateKey(evidenceCode string) (string, error) {
	if !cityRealtimeAgentIdentifierValid(evidenceCode, 96) {
		return "", ErrCityInvalidInput
	}
	key := "case-evidence:" + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseEvidenceExpiryVersion,
		evidenceCode,
	}, "\x1f")))
	if !cityRealtimeDueEventIdentifierValid(key, 160) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_aggregate"})
	}
	return key, nil
}

func cityRealtimeCharacterCaseEvidenceExpiryDedupKey(head cityRealtimeCharacterCaseEvidenceHead) (string, error) {
	if !cityRealtimeCharacterCaseEvidenceHeadValid(head) {
		return "", ErrCityInvalidInput
	}
	key := "case-evidence-expire:" + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseEvidenceExpiryVersion,
		head.EvidenceCode,
		head.SourceLawEventHash,
	}, "\x1f")))
	if !cityRealtimeDueEventIdentifierValid(key, 160) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_dedup"})
	}
	return key, nil
}

func scheduleCityRealtimeCharacterCaseEvidenceExpiryDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, dueWorldTimeUS, createdFrameSequence int64,
	head cityRealtimeCharacterCaseEvidenceHead,
) error {
	if tx == nil || worldID <= 0 || dueWorldTimeUS <= 0 ||
		dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 || createdFrameSequence <= 0 ||
		!cityRealtimeCharacterCaseEvidenceHeadValid(head) || head.EvidenceRevision != 1 ||
		head.EvidenceStatus != cityRealtimeCharacterCaseEvidenceActive ||
		head.ExpirationDueWorldTimeUS != dueWorldTimeUS {
		return ErrCityInvalidInput
	}
	aggregateKey, err := cityRealtimeCharacterCaseEvidenceAggregateKey(head.EvidenceCode)
	if err != nil {
		return err
	}
	dedupKey, err := cityRealtimeCharacterCaseEvidenceExpiryDedupKey(head)
	if err != nil {
		return err
	}
	payload, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":            cityRealtimeCharacterCaseEvidenceSchemaVersion,
		"evidence_code":             head.EvidenceCode,
		"subject_actor_code":        head.SubjectActorCode,
		"source_law_event_hash":     head.SourceLawEventHash,
		"source_law_event_sequence": head.SourceLawEventSequence,
	})
	if err != nil {
		return fmt.Errorf("canonicalize realtime character case-evidence expiry payload: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_due_events
    (world_id, event_type, schema_version, due_world_time_us, temporal_phase,
     priority, aggregate_type, aggregate_key, dedup_key, source_kind,
     source_reference, payload, payload_hash, expected_version, status,
     created_frame_sequence)
VALUES ($1, $2, 1, $3, 'rule_effect', $4, 'realtime_case_evidence', $5, $6, 'system',
        'realtime_character_case_evidence', $7::jsonb, $8, 1, 'pending', $9)`,
		worldID, cityRealtimeDueEventTypeCharacterCaseEvidenceExpire, dueWorldTimeUS,
		cityRealtimeCharacterCaseEvidenceExpiryPriority, aggregateKey, dedupKey,
		[]byte(payload), payloadHash, createdFrameSequence,
	); err != nil {
		return fmt.Errorf("schedule realtime character case-evidence expiry: %w", err)
	}
	return nil
}

func decodeCityRealtimeCharacterCaseEvidenceExpiryDuePayload(
	event cityRealtimeDueEventRecord,
) (cityRealtimeCharacterCaseEvidenceExpiryDuePayload, bool) {
	payload := cityRealtimeCharacterCaseEvidenceExpiryDuePayload{}
	if err := decodeStrictCityObject(event.Payload, &payload); err != nil ||
		payload.SchemaVersion != cityRealtimeCharacterCaseEvidenceSchemaVersion ||
		!cityRealtimeAgentIdentifierValid(payload.EvidenceCode, 96) ||
		!cityRealtimePlayerActorCodeValid(payload.SubjectActorCode) ||
		!cityRealtimeSHA256Hex(payload.SourceLawEventHash) || payload.SourceLawEventSequence <= 0 {
		return cityRealtimeCharacterCaseEvidenceExpiryDuePayload{}, false
	}
	expectedCode, err := cityRealtimeCharacterCaseEvidenceCode(payload.SourceLawEventHash)
	if err != nil || expectedCode != payload.EvidenceCode {
		return cityRealtimeCharacterCaseEvidenceExpiryDuePayload{}, false
	}
	_, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":            payload.SchemaVersion,
		"evidence_code":             payload.EvidenceCode,
		"subject_actor_code":        payload.SubjectActorCode,
		"source_law_event_hash":     payload.SourceLawEventHash,
		"source_law_event_sequence": payload.SourceLawEventSequence,
	})
	if err != nil || payloadHash != event.PayloadHash {
		return cityRealtimeCharacterCaseEvidenceExpiryDuePayload{}, false
	}
	return payload, true
}

func captureCityRealtimeCharacterCaseEvidenceFromLaw(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence, currentWorldTimeUS int64,
	law cityRealtimeCharacterLawEventRecord,
) (bool, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 || currentWorldTimeUS < 0 ||
		!cityRealtimeCharacterCaseEvidenceLawSourceValid(law) || law.FrameSequence != frameSequence {
		return false, ErrCityInvalidInput
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if agentState == nil || agentState.Binding == nil ||
		!cityRealtimeAgentCharacterCaseEvidenceRuntimeEnabled(*agentState.Binding) {
		return false, nil
	}
	binding, err := loadCityRealtimeCharacterCaseEvidenceBinding(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if binding == nil || binding.AgentBindingHash != agentState.Binding.BindingHash {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_scope"})
	}
	dueWorldTimeUS, err := cityRealtimeCharacterCaseEvidenceExpirationDueWorldTime(currentWorldTimeUS)
	if err != nil {
		return false, err
	}
	head, evidenceEvent, err := cityRealtimeCharacterCaptureCaseEvidenceFromLaw(law, dueWorldTimeUS)
	if err != nil {
		return false, err
	}
	if _, found, loadErr := loadCityRealtimeCharacterCaseEvidenceHead(ctx, tx, worldID, head.EvidenceCode, true); loadErr != nil {
		return false, loadErr
	} else if found {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_duplicate"})
	}
	if err = enableCityRealtimeCharacterCaseEvidenceMutationGate(ctx, tx, worldID, frameSequence); err != nil {
		return false, err
	}
	// Schedule before the head/event inserts. The database guard can then
	// prove that an active evidence handle always has its sole server-owned
	// terminal transition precommitted in the same sealed frame.
	if err = scheduleCityRealtimeCharacterCaseEvidenceExpiryDueEvent(
		ctx, tx, worldID, dueWorldTimeUS, frameSequence, head,
	); err != nil {
		return false, err
	}
	if err = insertCityRealtimeCharacterCaseEvidenceHead(ctx, tx, worldID, head); err != nil {
		return false, err
	}
	if err = insertCityRealtimeCharacterCaseEvidenceEvent(ctx, tx, worldID, evidenceEvent); err != nil {
		return false, err
	}
	return true, nil
}

func applyCityRealtimeCharacterCaseEvidenceExpiryDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	event cityRealtimeDueEventRecord,
) (bool, int, int, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 ||
		event.EventType != cityRealtimeDueEventTypeCharacterCaseEvidenceExpire ||
		event.SchemaVersion != cityRealtimeCharacterCaseEvidenceSchemaVersion ||
		event.SourceKind != cityRealtimeDueEventSourceKindSystem || event.TemporalPhase != "rule_effect" ||
		event.AggregateType != "realtime_case_evidence" ||
		event.SourceReference != "realtime_character_case_evidence" ||
		event.ExpectedVersion == nil || *event.ExpectedVersion != 1 {
		return false, 0, 0, nil
	}
	payload, validPayload := decodeCityRealtimeCharacterCaseEvidenceExpiryDuePayload(event)
	if !validPayload {
		return false, 0, 0, nil
	}
	expectedAggregateKey, err := cityRealtimeCharacterCaseEvidenceAggregateKey(payload.EvidenceCode)
	if err != nil || expectedAggregateKey != event.AggregateKey {
		return false, 0, 0, nil
	}
	binding, err := loadCityRealtimeCharacterCaseEvidenceBinding(ctx, tx, worldID)
	if err != nil {
		return false, 0, 0, err
	}
	if binding == nil {
		return false, 0, 0, nil
	}
	head, found, err := loadCityRealtimeCharacterCaseEvidenceHead(ctx, tx, worldID, payload.EvidenceCode, true)
	if err != nil {
		return false, 0, 0, err
	}
	if !found || head.EvidenceRevision != 1 || head.EvidenceStatus != cityRealtimeCharacterCaseEvidenceActive ||
		head.SubjectActorCode != payload.SubjectActorCode || head.SourceLawEventHash != payload.SourceLawEventHash ||
		head.SourceLawEventSequence != payload.SourceLawEventSequence ||
		head.ExpirationDueWorldTimeUS != event.DueWorldTimeUS {
		return false, 0, 0, nil
	}
	expectedDedupKey, err := cityRealtimeCharacterCaseEvidenceExpiryDedupKey(head)
	if err != nil || expectedDedupKey != event.DedupKey {
		return false, 0, 0, nil
	}
	if err = validateCityRealtimeCharacterCaseEvidenceHeadHistory(ctx, tx, worldID, head); err != nil {
		return false, 0, 0, err
	}
	next, evidenceEvent, err := cityRealtimeCharacterExpireCaseEvidence(head, frameSequence, event.DueWorldTimeUS)
	if err != nil {
		return false, 0, 0, err
	}
	if err = enableCityRealtimeCharacterCaseEvidenceMutationGate(ctx, tx, worldID, frameSequence); err != nil {
		return false, 0, 0, err
	}
	if err = insertCityRealtimeCharacterCaseEvidenceEvent(ctx, tx, worldID, evidenceEvent); err != nil {
		return false, 0, 0, err
	}
	if err = updateCityRealtimeCharacterCaseEvidenceHead(ctx, tx, worldID, head, next); err != nil {
		return false, 0, 0, err
	}
	assignmentClosureCount, procedureDispatchClosureCount, err := closeCityRealtimeCharacterCaseEvidenceAssignmentsForExpiredSource(
		ctx, tx, worldID, frameSequence, next,
	)
	if err != nil {
		return false, 0, 0, err
	}
	return true, assignmentClosureCount, procedureDispatchClosureCount, nil
}

func loadCityRealtimeCharacterCaseEvidenceHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseEvidenceHashState, error) {
	binding, err := loadCityRealtimeCharacterCaseEvidenceBinding(ctx, queryer, worldID)
	if err != nil || binding == nil {
		return nil, err
	}
	state := &cityRealtimeCharacterCaseEvidenceHashState{
		SchemaVersion: cityRealtimeCharacterCaseEvidenceSchemaVersion,
		Binding:       binding,
		Heads:         make([]cityRealtimeCharacterCaseEvidenceHead, 0),
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT evidence_code, subject_actor_code, source_kind, source_law_event_sequence,
       source_law_event_hash, source_frame_sequence, evidence_revision,
       evidence_status, captured_frame_sequence, expiration_due_world_time_us,
       last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_case_evidence_heads
WHERE world_id = $1
ORDER BY evidence_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-evidence heads: %w", err)
	}
	heads := make([]cityRealtimeCharacterCaseEvidenceHead, 0)
	for rows.Next() {
		head := cityRealtimeCharacterCaseEvidenceHead{}
		if err = rows.Scan(
			&head.EvidenceCode, &head.SubjectActorCode, &head.SourceKind, &head.SourceLawEventSequence,
			&head.SourceLawEventHash, &head.SourceFrameSequence, &head.EvidenceRevision,
			&head.EvidenceStatus, &head.CapturedFrameSequence, &head.ExpirationDueWorldTimeUS,
			&head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
		); err != nil {
			return nil, err
		}
		heads = append(heads, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character case-evidence heads: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character case-evidence heads: %w", err)
	}
	for _, head := range heads {
		if err = validateCityRealtimeCharacterCaseEvidenceHeadHistory(ctx, queryer, worldID, head); err != nil {
			return nil, err
		}
		state.Heads = append(state.Heads, head)
	}
	if err = validateCityRealtimeCharacterCaseEvidenceHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeCharacterCaseEvidenceHashState(state *cityRealtimeCharacterCaseEvidenceHashState) error {
	if state == nil || state.SchemaVersion != cityRealtimeCharacterCaseEvidenceSchemaVersion ||
		state.Binding == nil || state.Heads == nil || !cityRealtimeCharacterCaseEvidenceBindingValid(*state.Binding) {
		return errors.New("invalid realtime character case-evidence hash state")
	}
	for index, head := range state.Heads {
		if !cityRealtimeCharacterCaseEvidenceHeadValid(head) {
			return errors.New("invalid realtime character case-evidence head")
		}
		if index > 0 && state.Heads[index-1].EvidenceCode >= head.EvidenceCode {
			return errors.New("realtime character case-evidence heads are not in canonical order")
		}
	}
	return nil
}
