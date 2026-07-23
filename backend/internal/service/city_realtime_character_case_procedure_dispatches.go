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
	cityRealtimeCharacterCaseProcedureDispatchSchemaVersion  = 1
	cityRealtimeCharacterCaseProcedureDispatchBindingVersion = "city-realtime-character-case-procedure-dispatch-binding-v1"
	cityRealtimeCharacterCaseProcedureDispatchStateVersion   = "city-realtime-character-case-procedure-dispatch-state-v1"
	cityRealtimeCharacterCaseProcedureDispatchChainVersion   = "city-realtime-character-case-procedure-dispatch-chain-v1"
	cityRealtimeCharacterCaseProcedureDispatchEventVersion   = "city-realtime-character-case-procedure-dispatch-event-v1"

	// A dispatch is only a server-owned routing receipt for a future bounded
	// procedure. It is not an allegation, evidence verification, case,
	// adjudication, penalty, reward, wallet mutation, or reviewer decision.
	cityRealtimeCharacterCaseProcedureDispatchQueued             = "queued"
	cityRealtimeCharacterCaseProcedureDispatchSourceWindowClosed = "source_window_closed"
	cityRealtimeCharacterCaseProcedureDispatchQueuedEvent        = "procedure_queued"
	cityRealtimeCharacterCaseProcedureDispatchClosedEvent        = "source_window_closed"
)

// cityRealtimeCharacterCaseProcedureDispatchBinding pins the procedural
// routing adapter to an exact 1.11 Agent binding and the preceding assignment
// adapter. It grants neither a browser, a user, an administrator, nor a model
// authority to route, approve, reject, or decide a report.
type cityRealtimeCharacterCaseProcedureDispatchBinding struct {
	SchemaVersion    int    `json:"schema_version"`
	AgentBindingHash string `json:"agent_binding_hash"`
	BindingHash      string `json:"binding_hash"`
}

// cityRealtimeCharacterCaseProcedureDispatchHead is a minimal procedural
// receipt. It anchors exactly one already-sealed assignment-link event, not
// the underlying Law content. The current implementation intentionally stops
// at routing state so a later bounded authority can be added without making
// this adapter an implicit adjudicator.
type cityRealtimeCharacterCaseProcedureDispatchHead struct {
	ReporterActorCode       string `json:"reporter_actor_code"`
	SubjectActorCode        string `json:"subject_actor_code"`
	AssignmentEventSequence int64  `json:"assignment_event_sequence"`
	AssignmentLinkEventHash string `json:"assignment_link_event_hash"`
	DispatchRevision        int64  `json:"dispatch_revision"`
	DispatchStatus          string `json:"dispatch_status"`
	QueuedFrameSequence     int64  `json:"queued_frame_sequence"`
	LastFrameSequence       int64  `json:"last_frame_sequence"`
	EventChainHash          string `json:"event_chain_hash"`
	StateHash               string `json:"state_hash"`
}

// cityRealtimeCharacterCaseProcedureDispatchEvent is append-only and admits
// only creation plus source-window closure. It deliberately has no actor,
// reviewer, decision, outcome, source payload, case code, rule, or asset
// field.
type cityRealtimeCharacterCaseProcedureDispatchEvent struct {
	ReporterActorCode       string
	SubjectActorCode        string
	AssignmentEventSequence int64
	AssignmentLinkEventHash string
	EventSequence           int64
	FrameSequence           int64
	EventType               string
	PreviousEventHash       string
	EventHash               string
}

type cityRealtimeCharacterCaseProcedureDispatchHashState struct {
	SchemaVersion int                                                `json:"schema_version"`
	Binding       *cityRealtimeCharacterCaseProcedureDispatchBinding `json:"binding,omitempty"`
	Heads         []cityRealtimeCharacterCaseProcedureDispatchHead   `json:"heads"`
}

func cityRealtimeCharacterCaseProcedureDispatchBindingHash(agentBindingHash string) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseProcedureDispatchBindingVersion,
		agentBindingHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseProcedureDispatchStaticFieldsValid(
	head cityRealtimeCharacterCaseProcedureDispatchHead,
) bool {
	return cityRealtimePlayerActorCodeValid(head.ReporterActorCode) &&
		cityRealtimePlayerActorCodeValid(head.SubjectActorCode) &&
		head.ReporterActorCode != head.SubjectActorCode &&
		head.AssignmentEventSequence == 1 && cityRealtimeSHA256Hex(head.AssignmentLinkEventHash)
}

func cityRealtimeCharacterCaseProcedureDispatchChainGenesisHash(
	head cityRealtimeCharacterCaseProcedureDispatchHead,
) (string, error) {
	if !cityRealtimeCharacterCaseProcedureDispatchStaticFieldsValid(head) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseProcedureDispatchChainVersion,
		head.ReporterActorCode,
		head.SubjectActorCode,
		strconv.FormatInt(head.AssignmentEventSequence, 10),
		head.AssignmentLinkEventHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterCaseProcedureDispatchHeadStateHashUnchecked(
	head cityRealtimeCharacterCaseProcedureDispatchHead,
) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseProcedureDispatchStateVersion,
		head.ReporterActorCode,
		head.SubjectActorCode,
		strconv.FormatInt(head.AssignmentEventSequence, 10),
		head.AssignmentLinkEventHash,
		strconv.FormatInt(head.DispatchRevision, 10),
		head.DispatchStatus,
		strconv.FormatInt(head.QueuedFrameSequence, 10),
		strconv.FormatInt(head.LastFrameSequence, 10),
		head.EventChainHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseProcedureDispatchHeadValid(
	head cityRealtimeCharacterCaseProcedureDispatchHead,
) bool {
	if !cityRealtimeCharacterCaseProcedureDispatchStaticFieldsValid(head) ||
		head.QueuedFrameSequence <= 0 || head.LastFrameSequence < head.QueuedFrameSequence ||
		!cityRealtimeSHA256Hex(head.EventChainHash) || !cityRealtimeSHA256Hex(head.StateHash) {
		return false
	}
	switch head.DispatchRevision {
	case 1:
		if head.DispatchStatus != cityRealtimeCharacterCaseProcedureDispatchQueued ||
			head.LastFrameSequence != head.QueuedFrameSequence {
			return false
		}
	case 2:
		if head.DispatchStatus != cityRealtimeCharacterCaseProcedureDispatchSourceWindowClosed ||
			head.LastFrameSequence <= head.QueuedFrameSequence {
			return false
		}
	default:
		return false
	}
	return head.StateHash == cityRealtimeCharacterCaseProcedureDispatchHeadStateHashUnchecked(head)
}

func cityRealtimeCharacterCaseProcedureDispatchEventHash(
	event cityRealtimeCharacterCaseProcedureDispatchEvent,
) (string, error) {
	if !cityRealtimeCharacterCaseProcedureDispatchEventValid(event) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseProcedureDispatchEventVersion,
		event.ReporterActorCode,
		event.SubjectActorCode,
		strconv.FormatInt(event.AssignmentEventSequence, 10),
		event.AssignmentLinkEventHash,
		strconv.FormatInt(event.EventSequence, 10),
		strconv.FormatInt(event.FrameSequence, 10),
		event.EventType,
		event.PreviousEventHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterCaseProcedureDispatchEventValid(
	event cityRealtimeCharacterCaseProcedureDispatchEvent,
) bool {
	return cityRealtimePlayerActorCodeValid(event.ReporterActorCode) &&
		cityRealtimePlayerActorCodeValid(event.SubjectActorCode) &&
		event.ReporterActorCode != event.SubjectActorCode &&
		event.AssignmentEventSequence == 1 && cityRealtimeSHA256Hex(event.AssignmentLinkEventHash) &&
		event.EventSequence > 0 && event.FrameSequence > 0 &&
		cityRealtimeSHA256Hex(event.PreviousEventHash) &&
		(event.EventHash == "" || cityRealtimeSHA256Hex(event.EventHash)) &&
		((event.EventSequence == 1 && event.EventType == cityRealtimeCharacterCaseProcedureDispatchQueuedEvent) ||
			(event.EventSequence == 2 && event.EventType == cityRealtimeCharacterCaseProcedureDispatchClosedEvent))
}

func cityRealtimeCharacterQueueCaseProcedureDispatch(
	assignment cityRealtimeCharacterCaseEvidenceAssignmentHead,
	frameSequence int64,
) (cityRealtimeCharacterCaseProcedureDispatchHead, cityRealtimeCharacterCaseProcedureDispatchEvent, error) {
	if !cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(assignment) ||
		assignment.AssignmentRevision != 1 || assignment.AssignmentStatus != cityRealtimeCharacterCaseEvidenceAssignmentLinked ||
		assignment.AssignedFrameSequence != frameSequence || assignment.LastFrameSequence != frameSequence || frameSequence <= 0 {
		return cityRealtimeCharacterCaseProcedureDispatchHead{}, cityRealtimeCharacterCaseProcedureDispatchEvent{}, ErrCityInvalidInput
	}
	head := cityRealtimeCharacterCaseProcedureDispatchHead{
		ReporterActorCode:       assignment.ReporterActorCode,
		SubjectActorCode:        assignment.SubjectActorCode,
		AssignmentEventSequence: 1,
		AssignmentLinkEventHash: assignment.EventChainHash,
		DispatchRevision:        1,
		DispatchStatus:          cityRealtimeCharacterCaseProcedureDispatchQueued,
		QueuedFrameSequence:     frameSequence,
		LastFrameSequence:       frameSequence,
	}
	genesisHash, err := cityRealtimeCharacterCaseProcedureDispatchChainGenesisHash(head)
	if err != nil {
		return cityRealtimeCharacterCaseProcedureDispatchHead{}, cityRealtimeCharacterCaseProcedureDispatchEvent{}, err
	}
	event := cityRealtimeCharacterCaseProcedureDispatchEvent{
		ReporterActorCode:       head.ReporterActorCode,
		SubjectActorCode:        head.SubjectActorCode,
		AssignmentEventSequence: head.AssignmentEventSequence,
		AssignmentLinkEventHash: head.AssignmentLinkEventHash,
		EventSequence:           1,
		FrameSequence:           frameSequence,
		EventType:               cityRealtimeCharacterCaseProcedureDispatchQueuedEvent,
		PreviousEventHash:       genesisHash,
	}
	event.EventHash, err = cityRealtimeCharacterCaseProcedureDispatchEventHash(event)
	if err != nil {
		return cityRealtimeCharacterCaseProcedureDispatchHead{}, cityRealtimeCharacterCaseProcedureDispatchEvent{}, err
	}
	head.EventChainHash = event.EventHash
	head.StateHash = cityRealtimeCharacterCaseProcedureDispatchHeadStateHashUnchecked(head)
	if !cityRealtimeCharacterCaseProcedureDispatchHeadValid(head) {
		return cityRealtimeCharacterCaseProcedureDispatchHead{}, cityRealtimeCharacterCaseProcedureDispatchEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_queue"})
	}
	return head, event, nil
}

func cityRealtimeCharacterCloseCaseProcedureDispatch(
	head cityRealtimeCharacterCaseProcedureDispatchHead,
	assignment cityRealtimeCharacterCaseEvidenceAssignmentHead,
	frameSequence int64,
) (cityRealtimeCharacterCaseProcedureDispatchHead, cityRealtimeCharacterCaseProcedureDispatchEvent, error) {
	if !cityRealtimeCharacterCaseProcedureDispatchHeadValid(head) ||
		head.DispatchRevision != 1 || head.DispatchStatus != cityRealtimeCharacterCaseProcedureDispatchQueued ||
		!cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(assignment) ||
		assignment.AssignmentRevision != 2 || assignment.AssignmentStatus != cityRealtimeCharacterCaseEvidenceAssignmentClosed ||
		assignment.ReporterActorCode != head.ReporterActorCode || assignment.SubjectActorCode != head.SubjectActorCode ||
		assignment.LastFrameSequence != frameSequence || frameSequence <= head.LastFrameSequence {
		return cityRealtimeCharacterCaseProcedureDispatchHead{}, cityRealtimeCharacterCaseProcedureDispatchEvent{}, ErrCityInvalidInput
	}
	next := head
	next.DispatchRevision = 2
	next.DispatchStatus = cityRealtimeCharacterCaseProcedureDispatchSourceWindowClosed
	next.LastFrameSequence = frameSequence
	event := cityRealtimeCharacterCaseProcedureDispatchEvent{
		ReporterActorCode:       head.ReporterActorCode,
		SubjectActorCode:        head.SubjectActorCode,
		AssignmentEventSequence: head.AssignmentEventSequence,
		AssignmentLinkEventHash: head.AssignmentLinkEventHash,
		EventSequence:           2,
		FrameSequence:           frameSequence,
		EventType:               cityRealtimeCharacterCaseProcedureDispatchClosedEvent,
		PreviousEventHash:       head.EventChainHash,
	}
	var err error
	event.EventHash, err = cityRealtimeCharacterCaseProcedureDispatchEventHash(event)
	if err != nil {
		return cityRealtimeCharacterCaseProcedureDispatchHead{}, cityRealtimeCharacterCaseProcedureDispatchEvent{}, err
	}
	next.EventChainHash = event.EventHash
	next.StateHash = cityRealtimeCharacterCaseProcedureDispatchHeadStateHashUnchecked(next)
	if !cityRealtimeCharacterCaseProcedureDispatchHeadValid(next) {
		return cityRealtimeCharacterCaseProcedureDispatchHead{}, cityRealtimeCharacterCaseProcedureDispatchEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_close"})
	}
	return next, event, nil
}

func initializeCityRealtimeCharacterCaseProcedureDispatchFoundation(
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
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_policy"})
	}
	if !cityRealtimeAgentCharacterCaseProcedureDispatchRuntimeEnabled(*agentState.Binding) {
		return nil
	}
	assignmentBinding, err := loadCityRealtimeCharacterCaseEvidenceAssignmentBinding(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if assignmentBinding == nil || assignmentBinding.AgentBindingHash != agentState.Binding.BindingHash {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_dependencies"})
	}
	binding := cityRealtimeCharacterCaseProcedureDispatchBinding{
		SchemaVersion:    cityRealtimeCharacterCaseProcedureDispatchSchemaVersion,
		AgentBindingHash: agentState.Binding.BindingHash,
	}
	binding.BindingHash = cityRealtimeCharacterCaseProcedureDispatchBindingHash(binding.AgentBindingHash)
	if !cityRealtimeCharacterCaseProcedureDispatchBindingValid(binding) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_binding"})
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_character_case_procedure_dispatch_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("activate realtime character case-procedure dispatch initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_procedure_dispatch_world_bindings
    (world_id, schema_version, agent_binding_hash, binding_hash, metadata)
VALUES ($1, $2, $3, $4, '{}'::jsonb)`,
		worldID, binding.SchemaVersion, binding.AgentBindingHash, binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime character case-procedure dispatch binding: %w", err)
	}
	return nil
}

func enableCityRealtimeCharacterCaseProcedureDispatchMutationGate(
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
		{name: "sub2api.city_realtime_character_case_procedure_dispatch_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_case_procedure_dispatch_frame_sequence", value: frameSequence},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character case-procedure dispatch gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func cityRealtimeCharacterCaseProcedureDispatchBindingValid(
	binding cityRealtimeCharacterCaseProcedureDispatchBinding,
) bool {
	return binding.SchemaVersion == cityRealtimeCharacterCaseProcedureDispatchSchemaVersion &&
		cityRealtimeSHA256Hex(binding.AgentBindingHash) && cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeCharacterCaseProcedureDispatchBindingHash(binding.AgentBindingHash)
}

func loadCityRealtimeCharacterCaseProcedureDispatchBinding(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseProcedureDispatchBinding, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterCaseProcedureDispatchBinding{}
	var policyID, policyVersion, agentBindingHash string
	err := queryer.QueryRowContext(ctx, `
SELECT dispatch.schema_version, dispatch.agent_binding_hash, dispatch.binding_hash,
       agent.policy_id, agent.policy_version, agent.binding_hash
FROM city_realtime_character_case_procedure_dispatch_world_bindings dispatch
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = dispatch.world_id
WHERE dispatch.world_id = $1`, worldID).Scan(
		&binding.SchemaVersion, &binding.AgentBindingHash, &binding.BindingHash,
		&policyID, &policyVersion, &agentBindingHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-procedure dispatch binding: %w", err)
	}
	if policyID != cityRealtimeAgentCorePolicyID ||
		(policyVersion != cityRealtimeAgentCorePolicyVersionProcedureDispatch &&
			policyVersion != cityRealtimeAgentCorePolicyVersionTask &&
			policyVersion != cityRealtimeAgentCorePolicyVersionNavigationPlan) ||
		binding.AgentBindingHash != agentBindingHash || !cityRealtimeCharacterCaseProcedureDispatchBindingValid(binding) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_binding"})
	}
	return &binding, nil
}

func loadCityRealtimeCharacterCaseProcedureDispatchHead(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	reporterActorCode, subjectActorCode string,
	forUpdate bool,
) (cityRealtimeCharacterCaseProcedureDispatchHead, bool, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(reporterActorCode) ||
		!cityRealtimePlayerActorCodeValid(subjectActorCode) || reporterActorCode == subjectActorCode {
		return cityRealtimeCharacterCaseProcedureDispatchHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT reporter_actor_code, subject_actor_code, assignment_event_sequence,
       assignment_link_event_hash, dispatch_revision, dispatch_status,
       queued_frame_sequence, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_case_procedure_dispatch_heads
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head := cityRealtimeCharacterCaseProcedureDispatchHead{}
	err := queryer.QueryRowContext(ctx, query, worldID, reporterActorCode, subjectActorCode).Scan(
		&head.ReporterActorCode, &head.SubjectActorCode, &head.AssignmentEventSequence,
		&head.AssignmentLinkEventHash, &head.DispatchRevision, &head.DispatchStatus,
		&head.QueuedFrameSequence, &head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterCaseProcedureDispatchHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterCaseProcedureDispatchHead{}, false, fmt.Errorf("load realtime character case-procedure dispatch head: %w", err)
	}
	if !cityRealtimeCharacterCaseProcedureDispatchHeadValid(head) {
		return cityRealtimeCharacterCaseProcedureDispatchHead{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_head"})
	}
	return head, true, nil
}

func validateCityRealtimeCharacterCaseProcedureDispatchHeadHistory(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterCaseProcedureDispatchHead,
) error {
	if worldID <= 0 || !cityRealtimeCharacterCaseProcedureDispatchHeadValid(head) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_history"})
	}
	assignment, assignmentFound, err := loadCityRealtimeCharacterCaseEvidenceAssignmentHead(
		ctx, queryer, worldID, head.ReporterActorCode, head.SubjectActorCode, false,
	)
	if err != nil || !assignmentFound {
		if err != nil {
			return err
		}
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_assignment"})
	}
	if err = validateCityRealtimeCharacterCaseEvidenceAssignmentHeadHistory(ctx, queryer, worldID, assignment); err != nil {
		return err
	}
	assignmentEvent := cityRealtimeCharacterCaseEvidenceAssignmentEvent{}
	err = queryer.QueryRowContext(ctx, `
SELECT reporter_actor_code, subject_actor_code, report_event_sequence, report_event_hash,
       evidence_code, source_law_event_sequence, source_law_event_hash,
       source_frame_sequence, event_sequence, frame_sequence, event_type,
       previous_event_hash, event_hash
FROM city_realtime_character_case_evidence_assignment_events
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3 AND event_sequence = 1`,
		worldID, head.ReporterActorCode, head.SubjectActorCode,
	).Scan(
		&assignmentEvent.ReporterActorCode, &assignmentEvent.SubjectActorCode,
		&assignmentEvent.ReportEventSequence, &assignmentEvent.ReportEventHash,
		&assignmentEvent.EvidenceCode, &assignmentEvent.SourceLawEventSequence,
		&assignmentEvent.SourceLawEventHash, &assignmentEvent.SourceFrameSequence,
		&assignmentEvent.EventSequence, &assignmentEvent.FrameSequence, &assignmentEvent.EventType,
		&assignmentEvent.PreviousEventHash, &assignmentEvent.EventHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_assignment_event"})
	}
	if err != nil {
		return fmt.Errorf("load realtime character case-procedure assignment link event: %w", err)
	}
	expectedAssignmentEventHash, err := cityRealtimeCharacterCaseEvidenceAssignmentEventHash(assignmentEvent)
	if err != nil || !cityRealtimeCharacterCaseEvidenceAssignmentEventValid(assignmentEvent) ||
		assignmentEvent.EventHash != expectedAssignmentEventHash ||
		assignmentEvent.EventType != cityRealtimeCharacterCaseEvidenceAssignmentLinkedEvent ||
		assignmentEvent.EventHash != head.AssignmentLinkEventHash ||
		assignmentEvent.FrameSequence != head.QueuedFrameSequence ||
		assignmentEvent.ReporterActorCode != head.ReporterActorCode || assignmentEvent.SubjectActorCode != head.SubjectActorCode {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_assignment_link"})
	}
	if (head.DispatchRevision == 1 &&
		(assignment.AssignmentRevision != 1 || assignment.AssignmentStatus != cityRealtimeCharacterCaseEvidenceAssignmentLinked ||
			assignment.AssignedFrameSequence != head.QueuedFrameSequence)) ||
		(head.DispatchRevision == 2 &&
			(assignment.AssignmentRevision != 2 || assignment.AssignmentStatus != cityRealtimeCharacterCaseEvidenceAssignmentClosed ||
				assignment.LastFrameSequence != head.LastFrameSequence)) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_assignment_status"})
	}
	genesisHash, err := cityRealtimeCharacterCaseProcedureDispatchChainGenesisHash(head)
	if err != nil {
		return err
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT reporter_actor_code, subject_actor_code, assignment_event_sequence,
       assignment_link_event_hash, event_sequence, frame_sequence, event_type,
       previous_event_hash, event_hash
FROM city_realtime_character_case_procedure_dispatch_events
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3
ORDER BY event_sequence ASC`, worldID, head.ReporterActorCode, head.SubjectActorCode)
	if err != nil {
		return fmt.Errorf("load realtime character case-procedure dispatch history: %w", err)
	}
	previousHash := genesisHash
	eventCount := int64(0)
	var last cityRealtimeCharacterCaseProcedureDispatchEvent
	for rows.Next() {
		event := cityRealtimeCharacterCaseProcedureDispatchEvent{}
		if err = rows.Scan(
			&event.ReporterActorCode, &event.SubjectActorCode, &event.AssignmentEventSequence,
			&event.AssignmentLinkEventHash, &event.EventSequence, &event.FrameSequence,
			&event.EventType, &event.PreviousEventHash, &event.EventHash,
		); err != nil {
			_ = rows.Close()
			return err
		}
		eventCount++
		expectedHash, hashErr := cityRealtimeCharacterCaseProcedureDispatchEventHash(event)
		if hashErr != nil || !cityRealtimeCharacterCaseProcedureDispatchEventValid(event) ||
			event.EventSequence != eventCount || event.PreviousEventHash != previousHash ||
			event.EventHash != expectedHash || event.ReporterActorCode != head.ReporterActorCode ||
			event.SubjectActorCode != head.SubjectActorCode ||
			event.AssignmentEventSequence != head.AssignmentEventSequence ||
			event.AssignmentLinkEventHash != head.AssignmentLinkEventHash {
			_ = rows.Close()
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_event_chain"})
		}
		previousHash = event.EventHash
		last = event
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate realtime character case-procedure dispatch history: %w", err)
	}
	if err = rows.Close(); err != nil {
		return fmt.Errorf("close realtime character case-procedure dispatch history: %w", err)
	}
	if eventCount != head.DispatchRevision || eventCount == 0 || last.EventHash != head.EventChainHash ||
		last.FrameSequence != head.LastFrameSequence ||
		(last.EventSequence == 1 && (last.EventType != cityRealtimeCharacterCaseProcedureDispatchQueuedEvent ||
			last.FrameSequence != head.QueuedFrameSequence)) ||
		(last.EventSequence == 2 && last.EventType != cityRealtimeCharacterCaseProcedureDispatchClosedEvent) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_event_head"})
	}
	return nil
}

func insertCityRealtimeCharacterCaseProcedureDispatchHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	head cityRealtimeCharacterCaseProcedureDispatchHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseProcedureDispatchHeadValid(head) ||
		head.DispatchRevision != 1 {
		return ErrCityInvalidInput
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_procedure_dispatch_heads
    (world_id, reporter_actor_code, subject_actor_code, assignment_event_sequence,
     assignment_link_event_hash, dispatch_revision, dispatch_status,
     queued_frame_sequence, last_frame_sequence, event_chain_hash, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, '{}'::jsonb)`,
		worldID, head.ReporterActorCode, head.SubjectActorCode, head.AssignmentEventSequence,
		head.AssignmentLinkEventHash, head.DispatchRevision, head.DispatchStatus,
		head.QueuedFrameSequence, head.LastFrameSequence, head.EventChainHash, head.StateHash,
	)
	if err != nil {
		return fmt.Errorf("insert realtime character case-procedure dispatch head: %w", err)
	}
	return nil
}

func insertCityRealtimeCharacterCaseProcedureDispatchEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	event cityRealtimeCharacterCaseProcedureDispatchEvent,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseProcedureDispatchEventValid(event) ||
		!cityRealtimeSHA256Hex(event.EventHash) {
		return ErrCityInvalidInput
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_procedure_dispatch_events
    (world_id, reporter_actor_code, subject_actor_code, assignment_event_sequence,
     assignment_link_event_hash, event_sequence, frame_sequence, event_type,
     previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '{}'::jsonb)`,
		worldID, event.ReporterActorCode, event.SubjectActorCode, event.AssignmentEventSequence,
		event.AssignmentLinkEventHash, event.EventSequence, event.FrameSequence, event.EventType,
		event.PreviousEventHash, event.EventHash,
	)
	if err != nil {
		return fmt.Errorf("append realtime character case-procedure dispatch event: %w", err)
	}
	return nil
}

func updateCityRealtimeCharacterCaseProcedureDispatchHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previous, next cityRealtimeCharacterCaseProcedureDispatchHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseProcedureDispatchHeadValid(previous) ||
		!cityRealtimeCharacterCaseProcedureDispatchHeadValid(next) ||
		previous.ReporterActorCode != next.ReporterActorCode || previous.SubjectActorCode != next.SubjectActorCode ||
		previous.AssignmentEventSequence != next.AssignmentEventSequence ||
		previous.AssignmentLinkEventHash != next.AssignmentLinkEventHash ||
		next.DispatchRevision != previous.DispatchRevision+1 || next.LastFrameSequence <= previous.LastFrameSequence ||
		next.EventChainHash == previous.EventChainHash {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_case_procedure_dispatch_heads
SET dispatch_revision = $4, dispatch_status = $5, last_frame_sequence = $6,
    event_chain_hash = $7, state_hash = $8, updated_at = NOW()
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3
  AND dispatch_revision = $9 AND dispatch_status = $10
  AND last_frame_sequence = $11 AND event_chain_hash = $12 AND state_hash = $13`,
		worldID, next.ReporterActorCode, next.SubjectActorCode, next.DispatchRevision,
		next.DispatchStatus, next.LastFrameSequence, next.EventChainHash, next.StateHash,
		previous.DispatchRevision, previous.DispatchStatus, previous.LastFrameSequence,
		previous.EventChainHash, previous.StateHash,
	)
	if err != nil {
		return fmt.Errorf("update realtime character case-procedure dispatch head: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check realtime character case-procedure dispatch head update: %w", err)
	}
	if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_revision"})
	}
	return nil
}

// closeCityRealtimeCharacterCaseProcedureDispatchForClosedAssignment closes
// the routing receipt only after the independently captured source window has
// been closed. It cannot create a verdict or modify the report, intake, Law,
// Rule, profile, inventory, ledger, virtual currency, platform balance, or
// reward state.
func closeCityRealtimeCharacterCaseProcedureDispatchForClosedAssignment(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	closedAssignment cityRealtimeCharacterCaseEvidenceAssignmentHead,
) (int, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 ||
		!cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(closedAssignment) ||
		closedAssignment.AssignmentRevision != 2 ||
		closedAssignment.AssignmentStatus != cityRealtimeCharacterCaseEvidenceAssignmentClosed ||
		closedAssignment.LastFrameSequence != frameSequence {
		return 0, ErrCityInvalidInput
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if err != nil {
		return 0, err
	}
	if agentState == nil || agentState.Binding == nil ||
		!cityRealtimeAgentCharacterCaseProcedureDispatchRuntimeEnabled(*agentState.Binding) {
		return 0, nil
	}
	binding, err := loadCityRealtimeCharacterCaseProcedureDispatchBinding(ctx, tx, worldID)
	if err != nil {
		return 0, err
	}
	if binding == nil || binding.AgentBindingHash != agentState.Binding.BindingHash {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_scope"})
	}
	head, found, err := loadCityRealtimeCharacterCaseProcedureDispatchHead(
		ctx, tx, worldID, closedAssignment.ReporterActorCode, closedAssignment.SubjectActorCode, true,
	)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_missing"})
	}
	next, event, err := cityRealtimeCharacterCloseCaseProcedureDispatch(head, closedAssignment, frameSequence)
	if err != nil {
		return 0, err
	}
	if err = enableCityRealtimeCharacterCaseProcedureDispatchMutationGate(ctx, tx, worldID, frameSequence); err != nil {
		return 0, err
	}
	if err = insertCityRealtimeCharacterCaseProcedureDispatchEvent(ctx, tx, worldID, event); err != nil {
		return 0, err
	}
	if err = updateCityRealtimeCharacterCaseProcedureDispatchHead(ctx, tx, worldID, head, next); err != nil {
		return 0, err
	}
	return 1, nil
}

func loadCityRealtimeCharacterCaseProcedureDispatchHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseProcedureDispatchHashState, error) {
	binding, err := loadCityRealtimeCharacterCaseProcedureDispatchBinding(ctx, queryer, worldID)
	if err != nil || binding == nil {
		return nil, err
	}
	state := &cityRealtimeCharacterCaseProcedureDispatchHashState{
		SchemaVersion: cityRealtimeCharacterCaseProcedureDispatchSchemaVersion,
		Binding:       binding,
		Heads:         make([]cityRealtimeCharacterCaseProcedureDispatchHead, 0),
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT reporter_actor_code, subject_actor_code, assignment_event_sequence,
       assignment_link_event_hash, dispatch_revision, dispatch_status,
       queued_frame_sequence, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_case_procedure_dispatch_heads
WHERE world_id = $1
ORDER BY reporter_actor_code ASC, subject_actor_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-procedure dispatch heads: %w", err)
	}
	heads := make([]cityRealtimeCharacterCaseProcedureDispatchHead, 0)
	for rows.Next() {
		head := cityRealtimeCharacterCaseProcedureDispatchHead{}
		if err = rows.Scan(
			&head.ReporterActorCode, &head.SubjectActorCode, &head.AssignmentEventSequence,
			&head.AssignmentLinkEventHash, &head.DispatchRevision, &head.DispatchStatus,
			&head.QueuedFrameSequence, &head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		heads = append(heads, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character case-procedure dispatch heads: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character case-procedure dispatch heads: %w", err)
	}
	for _, head := range heads {
		if err = validateCityRealtimeCharacterCaseProcedureDispatchHeadHistory(ctx, queryer, worldID, head); err != nil {
			return nil, err
		}
		state.Heads = append(state.Heads, head)
	}
	if err = validateCityRealtimeCharacterCaseProcedureDispatchHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_procedure_dispatch_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeCharacterCaseProcedureDispatchHashState(
	state *cityRealtimeCharacterCaseProcedureDispatchHashState,
) error {
	if state == nil || state.SchemaVersion != cityRealtimeCharacterCaseProcedureDispatchSchemaVersion ||
		state.Binding == nil || state.Heads == nil || !cityRealtimeCharacterCaseProcedureDispatchBindingValid(*state.Binding) {
		return errors.New("invalid realtime character case-procedure dispatch hash state")
	}
	seenAssignmentLinks := make(map[string]struct{}, len(state.Heads))
	for index, head := range state.Heads {
		if !cityRealtimeCharacterCaseProcedureDispatchHeadValid(head) {
			return errors.New("invalid realtime character case-procedure dispatch head")
		}
		if index > 0 && (state.Heads[index-1].ReporterActorCode > head.ReporterActorCode ||
			(state.Heads[index-1].ReporterActorCode == head.ReporterActorCode &&
				state.Heads[index-1].SubjectActorCode >= head.SubjectActorCode)) {
			return errors.New("realtime character case-procedure dispatch heads are not in canonical order")
		}
		if _, exists := seenAssignmentLinks[head.AssignmentLinkEventHash]; exists {
			return errors.New("realtime character case-procedure dispatch assignment link is reused")
		}
		seenAssignmentLinks[head.AssignmentLinkEventHash] = struct{}{}
	}
	return nil
}
