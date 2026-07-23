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
	cityRealtimeCharacterCaseEvidenceAssignmentSchemaVersion  = 1
	cityRealtimeCharacterCaseEvidenceAssignmentBindingVersion = "city-realtime-character-case-evidence-assignment-binding-v1"
	cityRealtimeCharacterCaseEvidenceAssignmentStateVersion   = "city-realtime-character-case-evidence-assignment-state-v1"
	cityRealtimeCharacterCaseEvidenceAssignmentChainVersion   = "city-realtime-character-case-evidence-assignment-chain-v1"
	cityRealtimeCharacterCaseEvidenceAssignmentEventVersion   = "city-realtime-character-case-evidence-assignment-event-v1"

	// An assignment is deliberately a procedural correlation, not an evidence
	// determination. It can only point to one independently captured, still
	// active source record and carries no Law/Case source content.
	cityRealtimeCharacterCaseEvidenceAssignmentLinked      = "linked_active"
	cityRealtimeCharacterCaseEvidenceAssignmentClosed      = "source_window_closed"
	cityRealtimeCharacterCaseEvidenceAssignmentLinkedEvent = "independent_record_linked"
	cityRealtimeCharacterCaseEvidenceAssignmentClosedEvent = "source_window_closed"
)

// cityRealtimeCharacterCaseEvidenceAssignmentBinding pins the correlation
// adapter to the exact Agent binding that also owns report, intake, and
// evidence-source contracts. It is never an authorization grant for a user,
// browser, model, or administrator to submit evidence.
type cityRealtimeCharacterCaseEvidenceAssignmentBinding struct {
	SchemaVersion    int    `json:"schema_version"`
	AgentBindingHash string `json:"agent_binding_hash"`
	BindingHash      string `json:"binding_hash"`
}

// cityRealtimeCharacterCaseEvidenceAssignmentHead records only that a
// short-lived independent system record matched a procedural intake at one
// frame. It does not contain the Law rule, disposition, penalty, report
// content, model output, prompt, identity, account, wallet, or reward data.
type cityRealtimeCharacterCaseEvidenceAssignmentHead struct {
	ReporterActorCode      string `json:"reporter_actor_code"`
	SubjectActorCode       string `json:"subject_actor_code"`
	ReportEventSequence    int64  `json:"report_event_sequence"`
	ReportEventHash        string `json:"report_event_hash"`
	EvidenceCode           string `json:"evidence_code"`
	SourceLawEventSequence int64  `json:"source_law_event_sequence"`
	SourceLawEventHash     string `json:"source_law_event_hash"`
	SourceFrameSequence    int64  `json:"source_frame_sequence"`
	AssignmentRevision     int64  `json:"assignment_revision"`
	AssignmentStatus       string `json:"assignment_status"`
	AssignedFrameSequence  int64  `json:"assigned_frame_sequence"`
	LastFrameSequence      int64  `json:"last_frame_sequence"`
	EventChainHash         string `json:"event_chain_hash"`
	StateHash              string `json:"state_hash"`
}

// cityRealtimeCharacterCaseEvidenceAssignmentEvent is append-only. Its only
// permitted transitions are the server-created correlation and the source
// window closing. It cannot express a charge, verdict, fine, reward, or a
// free-form assertion.
type cityRealtimeCharacterCaseEvidenceAssignmentEvent struct {
	ReporterActorCode      string
	SubjectActorCode       string
	ReportEventSequence    int64
	ReportEventHash        string
	EvidenceCode           string
	SourceLawEventSequence int64
	SourceLawEventHash     string
	SourceFrameSequence    int64
	EventSequence          int64
	FrameSequence          int64
	EventType              string
	PreviousEventHash      string
	EventHash              string
}

type cityRealtimeCharacterCaseEvidenceAssignmentHashState struct {
	SchemaVersion int                                                 `json:"schema_version"`
	Binding       *cityRealtimeCharacterCaseEvidenceAssignmentBinding `json:"binding,omitempty"`
	Heads         []cityRealtimeCharacterCaseEvidenceAssignmentHead   `json:"heads"`
}

func cityRealtimeCharacterCaseEvidenceAssignmentBindingHash(agentBindingHash string) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseEvidenceAssignmentBindingVersion,
		agentBindingHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseEvidenceAssignmentStaticFieldsValid(head cityRealtimeCharacterCaseEvidenceAssignmentHead) bool {
	expectedEvidenceCode, err := cityRealtimeCharacterCaseEvidenceCode(head.SourceLawEventHash)
	return err == nil &&
		cityRealtimePlayerActorCodeValid(head.ReporterActorCode) &&
		cityRealtimePlayerActorCodeValid(head.SubjectActorCode) &&
		head.ReporterActorCode != head.SubjectActorCode &&
		head.ReportEventSequence == 1 && cityRealtimeSHA256Hex(head.ReportEventHash) &&
		head.EvidenceCode == expectedEvidenceCode &&
		head.SourceLawEventSequence > 0 && cityRealtimeSHA256Hex(head.SourceLawEventHash) &&
		head.SourceFrameSequence > 0
}

func cityRealtimeCharacterCaseEvidenceAssignmentChainGenesisHash(
	head cityRealtimeCharacterCaseEvidenceAssignmentHead,
) (string, error) {
	if !cityRealtimeCharacterCaseEvidenceAssignmentStaticFieldsValid(head) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseEvidenceAssignmentChainVersion,
		head.ReporterActorCode,
		head.SubjectActorCode,
		strconv.FormatInt(head.ReportEventSequence, 10),
		head.ReportEventHash,
		head.EvidenceCode,
		strconv.FormatInt(head.SourceLawEventSequence, 10),
		head.SourceLawEventHash,
		strconv.FormatInt(head.SourceFrameSequence, 10),
	}, "\x1f"))), nil
}

func cityRealtimeCharacterCaseEvidenceAssignmentHeadStateHashUnchecked(
	head cityRealtimeCharacterCaseEvidenceAssignmentHead,
) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseEvidenceAssignmentStateVersion,
		head.ReporterActorCode,
		head.SubjectActorCode,
		strconv.FormatInt(head.ReportEventSequence, 10),
		head.ReportEventHash,
		head.EvidenceCode,
		strconv.FormatInt(head.SourceLawEventSequence, 10),
		head.SourceLawEventHash,
		strconv.FormatInt(head.SourceFrameSequence, 10),
		strconv.FormatInt(head.AssignmentRevision, 10),
		head.AssignmentStatus,
		strconv.FormatInt(head.AssignedFrameSequence, 10),
		strconv.FormatInt(head.LastFrameSequence, 10),
		head.EventChainHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(head cityRealtimeCharacterCaseEvidenceAssignmentHead) bool {
	if !cityRealtimeCharacterCaseEvidenceAssignmentStaticFieldsValid(head) ||
		head.AssignedFrameSequence <= 0 || head.LastFrameSequence < head.AssignedFrameSequence ||
		!cityRealtimeSHA256Hex(head.EventChainHash) || !cityRealtimeSHA256Hex(head.StateHash) {
		return false
	}
	switch head.AssignmentRevision {
	case 1:
		if head.AssignmentStatus != cityRealtimeCharacterCaseEvidenceAssignmentLinked ||
			head.LastFrameSequence != head.AssignedFrameSequence ||
			head.SourceFrameSequence >= head.AssignedFrameSequence {
			return false
		}
	case 2:
		if head.AssignmentStatus != cityRealtimeCharacterCaseEvidenceAssignmentClosed ||
			head.LastFrameSequence <= head.AssignedFrameSequence ||
			head.SourceFrameSequence >= head.AssignedFrameSequence {
			return false
		}
	default:
		return false
	}
	return head.StateHash == cityRealtimeCharacterCaseEvidenceAssignmentHeadStateHashUnchecked(head)
}

func cityRealtimeCharacterCaseEvidenceAssignmentEventValid(event cityRealtimeCharacterCaseEvidenceAssignmentEvent) bool {
	expectedEvidenceCode, err := cityRealtimeCharacterCaseEvidenceCode(event.SourceLawEventHash)
	if err != nil || !cityRealtimePlayerActorCodeValid(event.ReporterActorCode) ||
		!cityRealtimePlayerActorCodeValid(event.SubjectActorCode) ||
		event.ReporterActorCode == event.SubjectActorCode || event.ReportEventSequence != 1 ||
		!cityRealtimeSHA256Hex(event.ReportEventHash) || event.EvidenceCode != expectedEvidenceCode ||
		event.SourceLawEventSequence <= 0 || event.SourceFrameSequence <= 0 ||
		event.EventSequence <= 0 || event.FrameSequence <= 0 ||
		!cityRealtimeSHA256Hex(event.PreviousEventHash) ||
		(event.EventHash != "" && !cityRealtimeSHA256Hex(event.EventHash)) {
		return false
	}
	return (event.EventSequence == 1 && event.EventType == cityRealtimeCharacterCaseEvidenceAssignmentLinkedEvent) ||
		(event.EventSequence == 2 && event.EventType == cityRealtimeCharacterCaseEvidenceAssignmentClosedEvent)
}

func cityRealtimeCharacterCaseEvidenceAssignmentEventHash(event cityRealtimeCharacterCaseEvidenceAssignmentEvent) (string, error) {
	if !cityRealtimeCharacterCaseEvidenceAssignmentEventValid(event) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseEvidenceAssignmentEventVersion,
		event.ReporterActorCode,
		event.SubjectActorCode,
		strconv.FormatInt(event.ReportEventSequence, 10),
		event.ReportEventHash,
		event.EvidenceCode,
		strconv.FormatInt(event.SourceLawEventSequence, 10),
		event.SourceLawEventHash,
		strconv.FormatInt(event.SourceFrameSequence, 10),
		strconv.FormatInt(event.EventSequence, 10),
		strconv.FormatInt(event.FrameSequence, 10),
		event.EventType,
		event.PreviousEventHash,
	}, "\x1f"))), nil
}

// cityRealtimeCharacterCaseEvidenceAssignmentEligible deliberately requires a
// source that existed before the receipt's sealed frame and is still active at
// that exact world time. The matching logic never reads report text (none is
// stored), model output, browser input, or legal source content.
func cityRealtimeCharacterCaseEvidenceAssignmentEligible(
	report cityRealtimeCharacterCaseReportHead,
	reportEvent cityRealtimeCharacterCaseReportEvent,
	intake cityRealtimeCharacterCaseIntakeHead,
	evidence cityRealtimeCharacterCaseEvidenceHead,
	frameSequence, currentWorldTimeUS int64,
) bool {
	return cityRealtimeCharacterCaseIntakeReportReceiptValid(report, reportEvent) &&
		cityRealtimeCharacterCaseIntakeHeadValid(intake) &&
		cityRealtimeCharacterCaseEvidenceHeadValid(evidence) &&
		intake.IntakeRevision == 1 && intake.IntakeStatus == cityRealtimeCharacterCaseIntakeEvidenceRequired &&
		intake.ReporterActorCode == report.ReporterActorCode && intake.SubjectActorCode == report.SubjectActorCode &&
		intake.ReportEventHash == reportEvent.EventHash && intake.OpenedFrameSequence == frameSequence &&
		reportEvent.FrameSequence == frameSequence && evidence.EvidenceRevision == 1 &&
		evidence.EvidenceStatus == cityRealtimeCharacterCaseEvidenceActive &&
		evidence.SubjectActorCode == report.SubjectActorCode &&
		evidence.SourceFrameSequence < frameSequence && evidence.CapturedFrameSequence < frameSequence &&
		evidence.ExpirationDueWorldTimeUS > currentWorldTimeUS
}

func cityRealtimeCharacterLinkCaseEvidenceAssignment(
	report cityRealtimeCharacterCaseReportHead,
	reportEvent cityRealtimeCharacterCaseReportEvent,
	intake cityRealtimeCharacterCaseIntakeHead,
	evidence cityRealtimeCharacterCaseEvidenceHead,
	frameSequence, currentWorldTimeUS int64,
) (cityRealtimeCharacterCaseEvidenceAssignmentHead, cityRealtimeCharacterCaseEvidenceAssignmentEvent, error) {
	if !cityRealtimeCharacterCaseEvidenceAssignmentEligible(report, reportEvent, intake, evidence, frameSequence, currentWorldTimeUS) {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, cityRealtimeCharacterCaseEvidenceAssignmentEvent{}, ErrCityInvalidInput
	}
	head := cityRealtimeCharacterCaseEvidenceAssignmentHead{
		ReporterActorCode:      report.ReporterActorCode,
		SubjectActorCode:       report.SubjectActorCode,
		ReportEventSequence:    reportEvent.EventSequence,
		ReportEventHash:        reportEvent.EventHash,
		EvidenceCode:           evidence.EvidenceCode,
		SourceLawEventSequence: evidence.SourceLawEventSequence,
		SourceLawEventHash:     evidence.SourceLawEventHash,
		SourceFrameSequence:    evidence.SourceFrameSequence,
		AssignmentRevision:     1,
		AssignmentStatus:       cityRealtimeCharacterCaseEvidenceAssignmentLinked,
		AssignedFrameSequence:  frameSequence,
		LastFrameSequence:      frameSequence,
	}
	genesisHash, err := cityRealtimeCharacterCaseEvidenceAssignmentChainGenesisHash(head)
	if err != nil {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, cityRealtimeCharacterCaseEvidenceAssignmentEvent{}, err
	}
	event := cityRealtimeCharacterCaseEvidenceAssignmentEvent{
		ReporterActorCode:      head.ReporterActorCode,
		SubjectActorCode:       head.SubjectActorCode,
		ReportEventSequence:    head.ReportEventSequence,
		ReportEventHash:        head.ReportEventHash,
		EvidenceCode:           head.EvidenceCode,
		SourceLawEventSequence: head.SourceLawEventSequence,
		SourceLawEventHash:     head.SourceLawEventHash,
		SourceFrameSequence:    head.SourceFrameSequence,
		EventSequence:          1,
		FrameSequence:          frameSequence,
		EventType:              cityRealtimeCharacterCaseEvidenceAssignmentLinkedEvent,
		PreviousEventHash:      genesisHash,
	}
	event.EventHash, err = cityRealtimeCharacterCaseEvidenceAssignmentEventHash(event)
	if err != nil {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, cityRealtimeCharacterCaseEvidenceAssignmentEvent{}, err
	}
	head.EventChainHash = event.EventHash
	head.StateHash = cityRealtimeCharacterCaseEvidenceAssignmentHeadStateHashUnchecked(head)
	if !cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(head) {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, cityRealtimeCharacterCaseEvidenceAssignmentEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_link"})
	}
	return head, event, nil
}

func cityRealtimeCharacterCloseCaseEvidenceAssignment(
	previous cityRealtimeCharacterCaseEvidenceAssignmentHead,
	expiredEvidence cityRealtimeCharacterCaseEvidenceHead,
	frameSequence int64,
) (cityRealtimeCharacterCaseEvidenceAssignmentHead, cityRealtimeCharacterCaseEvidenceAssignmentEvent, error) {
	if !cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(previous) ||
		previous.AssignmentRevision != 1 || previous.AssignmentStatus != cityRealtimeCharacterCaseEvidenceAssignmentLinked ||
		!cityRealtimeCharacterCaseEvidenceHeadValid(expiredEvidence) ||
		expiredEvidence.EvidenceRevision != 2 || expiredEvidence.EvidenceStatus != cityRealtimeCharacterCaseEvidenceExpired ||
		previous.EvidenceCode != expiredEvidence.EvidenceCode ||
		previous.SubjectActorCode != expiredEvidence.SubjectActorCode ||
		previous.SourceLawEventSequence != expiredEvidence.SourceLawEventSequence ||
		previous.SourceLawEventHash != expiredEvidence.SourceLawEventHash ||
		previous.SourceFrameSequence != expiredEvidence.SourceFrameSequence ||
		frameSequence != expiredEvidence.LastFrameSequence || frameSequence <= previous.LastFrameSequence {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, cityRealtimeCharacterCaseEvidenceAssignmentEvent{}, ErrCityInvalidInput
	}
	event := cityRealtimeCharacterCaseEvidenceAssignmentEvent{
		ReporterActorCode:      previous.ReporterActorCode,
		SubjectActorCode:       previous.SubjectActorCode,
		ReportEventSequence:    previous.ReportEventSequence,
		ReportEventHash:        previous.ReportEventHash,
		EvidenceCode:           previous.EvidenceCode,
		SourceLawEventSequence: previous.SourceLawEventSequence,
		SourceLawEventHash:     previous.SourceLawEventHash,
		SourceFrameSequence:    previous.SourceFrameSequence,
		EventSequence:          2,
		FrameSequence:          frameSequence,
		EventType:              cityRealtimeCharacterCaseEvidenceAssignmentClosedEvent,
		PreviousEventHash:      previous.EventChainHash,
	}
	var err error
	event.EventHash, err = cityRealtimeCharacterCaseEvidenceAssignmentEventHash(event)
	if err != nil {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, cityRealtimeCharacterCaseEvidenceAssignmentEvent{}, err
	}
	next := previous
	next.AssignmentRevision = event.EventSequence
	next.AssignmentStatus = cityRealtimeCharacterCaseEvidenceAssignmentClosed
	next.LastFrameSequence = frameSequence
	next.EventChainHash = event.EventHash
	next.StateHash = cityRealtimeCharacterCaseEvidenceAssignmentHeadStateHashUnchecked(next)
	if !cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(next) {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, cityRealtimeCharacterCaseEvidenceAssignmentEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_close"})
	}
	return next, event, nil
}

func initializeCityRealtimeCharacterCaseEvidenceAssignmentFoundation(
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
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_policy"})
	}
	if !cityRealtimeAgentCharacterCaseEvidenceAssignmentRuntimeEnabled(*agentState.Binding) {
		return nil
	}
	intakeBinding, err := loadCityRealtimeCharacterCaseIntakeBinding(ctx, tx, worldID)
	if err != nil {
		return err
	}
	evidenceBinding, err := loadCityRealtimeCharacterCaseEvidenceBinding(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if intakeBinding == nil || evidenceBinding == nil ||
		intakeBinding.AgentBindingHash != agentState.Binding.BindingHash ||
		evidenceBinding.AgentBindingHash != agentState.Binding.BindingHash {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_dependencies"})
	}
	binding := cityRealtimeCharacterCaseEvidenceAssignmentBinding{
		SchemaVersion:    cityRealtimeCharacterCaseEvidenceAssignmentSchemaVersion,
		AgentBindingHash: agentState.Binding.BindingHash,
	}
	binding.BindingHash = cityRealtimeCharacterCaseEvidenceAssignmentBindingHash(binding.AgentBindingHash)
	if !cityRealtimeCharacterCaseEvidenceAssignmentBindingValid(binding) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_binding"})
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_character_case_evidence_assignment_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("activate realtime character case-evidence assignment initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_evidence_assignment_world_bindings
    (world_id, schema_version, agent_binding_hash, binding_hash, metadata)
VALUES ($1, $2, $3, $4, '{}'::jsonb)`,
		worldID, binding.SchemaVersion, binding.AgentBindingHash, binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime character case-evidence assignment binding: %w", err)
	}
	return nil
}

func enableCityRealtimeCharacterCaseEvidenceAssignmentMutationGate(
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
		{name: "sub2api.city_realtime_character_case_evidence_assignment_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_case_evidence_assignment_frame_sequence", value: frameSequence},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character case-evidence assignment gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func cityRealtimeCharacterCaseEvidenceAssignmentBindingValid(binding cityRealtimeCharacterCaseEvidenceAssignmentBinding) bool {
	return binding.SchemaVersion == cityRealtimeCharacterCaseEvidenceAssignmentSchemaVersion &&
		cityRealtimeSHA256Hex(binding.AgentBindingHash) && cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeCharacterCaseEvidenceAssignmentBindingHash(binding.AgentBindingHash)
}

func loadCityRealtimeCharacterCaseEvidenceAssignmentBinding(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseEvidenceAssignmentBinding, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterCaseEvidenceAssignmentBinding{}
	var policyID, policyVersion, agentBindingHash string
	err := queryer.QueryRowContext(ctx, `
SELECT assignment.schema_version, assignment.agent_binding_hash, assignment.binding_hash,
       agent.policy_id, agent.policy_version, agent.binding_hash
FROM city_realtime_character_case_evidence_assignment_world_bindings assignment
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = assignment.world_id
WHERE assignment.world_id = $1`, worldID).Scan(
		&binding.SchemaVersion, &binding.AgentBindingHash, &binding.BindingHash,
		&policyID, &policyVersion, &agentBindingHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-evidence assignment binding: %w", err)
	}
	if policyID != cityRealtimeAgentCorePolicyID ||
		(policyVersion != cityRealtimeAgentCorePolicyVersionEvidenceAssignment &&
			policyVersion != cityRealtimeAgentCorePolicyVersionProcedureDispatch &&
			policyVersion != cityRealtimeAgentCorePolicyVersionTask &&
			policyVersion != cityRealtimeAgentCorePolicyVersionNavigationPlan) ||
		binding.AgentBindingHash != agentBindingHash || !cityRealtimeCharacterCaseEvidenceAssignmentBindingValid(binding) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_binding"})
	}
	return &binding, nil
}

func loadCityRealtimeCharacterCaseEvidenceAssignmentHead(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	reporterActorCode, subjectActorCode string,
	forUpdate bool,
) (cityRealtimeCharacterCaseEvidenceAssignmentHead, bool, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(reporterActorCode) ||
		!cityRealtimePlayerActorCodeValid(subjectActorCode) || reporterActorCode == subjectActorCode {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT reporter_actor_code, subject_actor_code, report_event_sequence, report_event_hash,
       evidence_code, source_law_event_sequence, source_law_event_hash,
       source_frame_sequence, assignment_revision, assignment_status,
       assigned_frame_sequence, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_case_evidence_assignment_heads
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head := cityRealtimeCharacterCaseEvidenceAssignmentHead{}
	err := queryer.QueryRowContext(ctx, query, worldID, reporterActorCode, subjectActorCode).Scan(
		&head.ReporterActorCode, &head.SubjectActorCode, &head.ReportEventSequence, &head.ReportEventHash,
		&head.EvidenceCode, &head.SourceLawEventSequence, &head.SourceLawEventHash,
		&head.SourceFrameSequence, &head.AssignmentRevision, &head.AssignmentStatus,
		&head.AssignedFrameSequence, &head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, false, fmt.Errorf("load realtime character case-evidence assignment head: %w", err)
	}
	if !cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(head) {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_head"})
	}
	return head, true, nil
}

func loadCityRealtimeCharacterCaseEvidenceAssignmentByEvidenceCode(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	evidenceCode string,
	forUpdate bool,
) (cityRealtimeCharacterCaseEvidenceAssignmentHead, bool, error) {
	if worldID <= 0 || !cityRealtimeAgentIdentifierValid(evidenceCode, 96) {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT reporter_actor_code, subject_actor_code, report_event_sequence, report_event_hash,
       evidence_code, source_law_event_sequence, source_law_event_hash,
       source_frame_sequence, assignment_revision, assignment_status,
       assigned_frame_sequence, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_case_evidence_assignment_heads
WHERE world_id = $1 AND evidence_code = $2`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head := cityRealtimeCharacterCaseEvidenceAssignmentHead{}
	err := queryer.QueryRowContext(ctx, query, worldID, evidenceCode).Scan(
		&head.ReporterActorCode, &head.SubjectActorCode, &head.ReportEventSequence, &head.ReportEventHash,
		&head.EvidenceCode, &head.SourceLawEventSequence, &head.SourceLawEventHash,
		&head.SourceFrameSequence, &head.AssignmentRevision, &head.AssignmentStatus,
		&head.AssignedFrameSequence, &head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, false, fmt.Errorf("load realtime character case-evidence assignment by source: %w", err)
	}
	if !cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(head) {
		return cityRealtimeCharacterCaseEvidenceAssignmentHead{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_source"})
	}
	return head, true, nil
}

// findCityRealtimeCharacterCaseEvidenceAssignmentCandidate returns a source
// only when exactly one unclaimed, active handle belongs to the same subject
// and predates the receipt frame. Ambiguity is intentionally treated as no
// match rather than letting an Agent, caller, or heuristic pick evidence.
func findCityRealtimeCharacterCaseEvidenceAssignmentCandidate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	subjectActorCode string,
	reportFrameSequence, currentWorldTimeUS int64,
) (cityRealtimeCharacterCaseEvidenceHead, bool, error) {
	if tx == nil || worldID <= 0 || !cityRealtimePlayerActorCodeValid(subjectActorCode) ||
		reportFrameSequence <= 0 || currentWorldTimeUS < 0 {
		return cityRealtimeCharacterCaseEvidenceHead{}, false, ErrCityInvalidInput
	}
	rows, err := tx.QueryContext(ctx, `
SELECT evidence.evidence_code, evidence.subject_actor_code, evidence.source_kind,
       evidence.source_law_event_sequence, evidence.source_law_event_hash,
       evidence.source_frame_sequence, evidence.evidence_revision, evidence.evidence_status,
       evidence.captured_frame_sequence, evidence.expiration_due_world_time_us,
       evidence.last_frame_sequence, evidence.event_chain_hash, evidence.state_hash
FROM city_realtime_character_case_evidence_heads evidence
WHERE evidence.world_id = $1
  AND evidence.subject_actor_code = $2
  AND evidence.evidence_revision = 1
  AND evidence.evidence_status = 'active'
  AND evidence.source_frame_sequence < $3
  AND evidence.captured_frame_sequence < $3
  AND evidence.expiration_due_world_time_us > $4
  AND NOT EXISTS (
      SELECT 1
      FROM city_realtime_character_case_evidence_assignment_heads assignment
      WHERE assignment.world_id = evidence.world_id
        AND assignment.evidence_code = evidence.evidence_code
  )
ORDER BY evidence.source_frame_sequence DESC, evidence.evidence_code ASC
LIMIT 2
FOR UPDATE`, worldID, subjectActorCode, reportFrameSequence, currentWorldTimeUS)
	if err != nil {
		return cityRealtimeCharacterCaseEvidenceHead{}, false, fmt.Errorf("find realtime character case-evidence assignment candidate: %w", err)
	}
	candidates := make([]cityRealtimeCharacterCaseEvidenceHead, 0, 2)
	for rows.Next() {
		head := cityRealtimeCharacterCaseEvidenceHead{}
		if err = rows.Scan(
			&head.EvidenceCode, &head.SubjectActorCode, &head.SourceKind,
			&head.SourceLawEventSequence, &head.SourceLawEventHash,
			&head.SourceFrameSequence, &head.EvidenceRevision, &head.EvidenceStatus,
			&head.CapturedFrameSequence, &head.ExpirationDueWorldTimeUS,
			&head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
		); err != nil {
			_ = rows.Close()
			return cityRealtimeCharacterCaseEvidenceHead{}, false, err
		}
		candidates = append(candidates, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return cityRealtimeCharacterCaseEvidenceHead{}, false, fmt.Errorf("iterate realtime character case-evidence assignment candidates: %w", err)
	}
	if err = rows.Close(); err != nil {
		return cityRealtimeCharacterCaseEvidenceHead{}, false, fmt.Errorf("close realtime character case-evidence assignment candidates: %w", err)
	}
	if len(candidates) != 1 {
		return cityRealtimeCharacterCaseEvidenceHead{}, false, nil
	}
	if err = validateCityRealtimeCharacterCaseEvidenceHeadHistory(ctx, tx, worldID, candidates[0]); err != nil {
		return cityRealtimeCharacterCaseEvidenceHead{}, false, err
	}
	return candidates[0], true, nil
}

func validateCityRealtimeCharacterCaseEvidenceAssignmentHeadHistory(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterCaseEvidenceAssignmentHead,
) error {
	if worldID <= 0 || !cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(head) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_history"})
	}
	report, reportEvent, reportFound, err := loadCityRealtimeCharacterCaseIntakeReportReceipt(
		ctx, queryer, worldID, head.ReporterActorCode, head.SubjectActorCode, false,
	)
	if err != nil || !reportFound || reportEvent.EventSequence != head.ReportEventSequence ||
		reportEvent.EventHash != head.ReportEventHash {
		if err != nil {
			return err
		}
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_report"})
	}
	intake, intakeFound, err := loadCityRealtimeCharacterCaseIntakeHead(
		ctx, queryer, worldID, head.ReporterActorCode, head.SubjectActorCode, false,
	)
	if err != nil || !intakeFound {
		if err != nil {
			return err
		}
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_intake"})
	}
	if err = validateCityRealtimeCharacterCaseIntakeHeadHistory(ctx, queryer, worldID, intake); err != nil {
		return err
	}
	evidence, evidenceFound, err := loadCityRealtimeCharacterCaseEvidenceHead(
		ctx, queryer, worldID, head.EvidenceCode, false,
	)
	if err != nil || !evidenceFound {
		if err != nil {
			return err
		}
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_evidence"})
	}
	if err = validateCityRealtimeCharacterCaseEvidenceHeadHistory(ctx, queryer, worldID, evidence); err != nil {
		return err
	}
	if report.ReporterActorCode != head.ReporterActorCode || report.SubjectActorCode != head.SubjectActorCode ||
		intake.ReportEventHash != head.ReportEventHash || evidence.SubjectActorCode != head.SubjectActorCode ||
		evidence.SourceLawEventSequence != head.SourceLawEventSequence ||
		evidence.SourceLawEventHash != head.SourceLawEventHash ||
		evidence.SourceFrameSequence != head.SourceFrameSequence || head.AssignedFrameSequence != report.FiledFrameSequence ||
		head.AssignedFrameSequence != intake.OpenedFrameSequence || evidence.SourceFrameSequence >= head.AssignedFrameSequence {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_scope"})
	}
	if (head.AssignmentRevision == 1 && (evidence.EvidenceRevision != 1 || evidence.EvidenceStatus != cityRealtimeCharacterCaseEvidenceActive)) ||
		(head.AssignmentRevision == 2 && (evidence.EvidenceRevision != 2 || evidence.EvidenceStatus != cityRealtimeCharacterCaseEvidenceExpired || evidence.LastFrameSequence != head.LastFrameSequence)) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_source_status"})
	}
	genesisHash, err := cityRealtimeCharacterCaseEvidenceAssignmentChainGenesisHash(head)
	if err != nil {
		return err
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT reporter_actor_code, subject_actor_code, report_event_sequence, report_event_hash,
       evidence_code, source_law_event_sequence, source_law_event_hash,
       source_frame_sequence, event_sequence, frame_sequence, event_type,
       previous_event_hash, event_hash
FROM city_realtime_character_case_evidence_assignment_events
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3
ORDER BY event_sequence ASC`, worldID, head.ReporterActorCode, head.SubjectActorCode)
	if err != nil {
		return fmt.Errorf("load realtime character case-evidence assignment history: %w", err)
	}
	previousHash := genesisHash
	eventCount := int64(0)
	var last cityRealtimeCharacterCaseEvidenceAssignmentEvent
	for rows.Next() {
		event := cityRealtimeCharacterCaseEvidenceAssignmentEvent{}
		if err = rows.Scan(
			&event.ReporterActorCode, &event.SubjectActorCode, &event.ReportEventSequence, &event.ReportEventHash,
			&event.EvidenceCode, &event.SourceLawEventSequence, &event.SourceLawEventHash,
			&event.SourceFrameSequence, &event.EventSequence, &event.FrameSequence, &event.EventType,
			&event.PreviousEventHash, &event.EventHash,
		); err != nil {
			_ = rows.Close()
			return err
		}
		eventCount++
		expectedHash, hashErr := cityRealtimeCharacterCaseEvidenceAssignmentEventHash(event)
		if hashErr != nil || !cityRealtimeCharacterCaseEvidenceAssignmentEventValid(event) ||
			event.EventSequence != eventCount || event.PreviousEventHash != previousHash ||
			event.EventHash != expectedHash || event.ReporterActorCode != head.ReporterActorCode ||
			event.SubjectActorCode != head.SubjectActorCode || event.ReportEventHash != head.ReportEventHash ||
			event.EvidenceCode != head.EvidenceCode || event.SourceLawEventSequence != head.SourceLawEventSequence ||
			event.SourceLawEventHash != head.SourceLawEventHash || event.SourceFrameSequence != head.SourceFrameSequence {
			_ = rows.Close()
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_event_chain"})
		}
		previousHash = event.EventHash
		last = event
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate realtime character case-evidence assignment history: %w", err)
	}
	if err = rows.Close(); err != nil {
		return fmt.Errorf("close realtime character case-evidence assignment history: %w", err)
	}
	if eventCount != head.AssignmentRevision || eventCount == 0 || last.EventHash != head.EventChainHash ||
		last.FrameSequence != head.LastFrameSequence ||
		(last.EventSequence == 1 && (last.EventType != cityRealtimeCharacterCaseEvidenceAssignmentLinkedEvent ||
			last.FrameSequence != head.AssignedFrameSequence)) ||
		(last.EventSequence == 2 && last.EventType != cityRealtimeCharacterCaseEvidenceAssignmentClosedEvent) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_event_head"})
	}
	return nil
}

func insertCityRealtimeCharacterCaseEvidenceAssignmentHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	head cityRealtimeCharacterCaseEvidenceAssignmentHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(head) ||
		head.AssignmentRevision != 1 {
		return ErrCityInvalidInput
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_evidence_assignment_heads
    (world_id, reporter_actor_code, subject_actor_code, report_event_sequence,
     report_event_hash, evidence_code, source_law_event_sequence,
     source_law_event_hash, source_frame_sequence, assignment_revision,
     assignment_status, assigned_frame_sequence, last_frame_sequence,
     event_chain_hash, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, '{}'::jsonb)`,
		worldID, head.ReporterActorCode, head.SubjectActorCode, head.ReportEventSequence,
		head.ReportEventHash, head.EvidenceCode, head.SourceLawEventSequence,
		head.SourceLawEventHash, head.SourceFrameSequence, head.AssignmentRevision,
		head.AssignmentStatus, head.AssignedFrameSequence, head.LastFrameSequence,
		head.EventChainHash, head.StateHash,
	)
	if err != nil {
		return fmt.Errorf("insert realtime character case-evidence assignment head: %w", err)
	}
	return nil
}

func insertCityRealtimeCharacterCaseEvidenceAssignmentEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	event cityRealtimeCharacterCaseEvidenceAssignmentEvent,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseEvidenceAssignmentEventValid(event) ||
		!cityRealtimeSHA256Hex(event.EventHash) {
		return ErrCityInvalidInput
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_evidence_assignment_events
    (world_id, reporter_actor_code, subject_actor_code, report_event_sequence,
     report_event_hash, evidence_code, source_law_event_sequence,
     source_law_event_hash, source_frame_sequence, event_sequence,
     frame_sequence, event_type, previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, '{}'::jsonb)`,
		worldID, event.ReporterActorCode, event.SubjectActorCode, event.ReportEventSequence,
		event.ReportEventHash, event.EvidenceCode, event.SourceLawEventSequence,
		event.SourceLawEventHash, event.SourceFrameSequence, event.EventSequence,
		event.FrameSequence, event.EventType, event.PreviousEventHash, event.EventHash,
	)
	if err != nil {
		return fmt.Errorf("append realtime character case-evidence assignment event: %w", err)
	}
	return nil
}

func updateCityRealtimeCharacterCaseEvidenceAssignmentHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previous, next cityRealtimeCharacterCaseEvidenceAssignmentHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(previous) ||
		!cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(next) ||
		previous.ReporterActorCode != next.ReporterActorCode || previous.SubjectActorCode != next.SubjectActorCode ||
		previous.ReportEventHash != next.ReportEventHash || previous.EvidenceCode != next.EvidenceCode ||
		previous.SourceLawEventHash != next.SourceLawEventHash ||
		next.AssignmentRevision != previous.AssignmentRevision+1 || next.LastFrameSequence <= previous.LastFrameSequence ||
		next.EventChainHash == previous.EventChainHash {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_case_evidence_assignment_heads
SET assignment_revision = $4, assignment_status = $5, last_frame_sequence = $6,
    event_chain_hash = $7, state_hash = $8, updated_at = NOW()
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3
  AND assignment_revision = $9 AND assignment_status = $10
  AND last_frame_sequence = $11 AND event_chain_hash = $12 AND state_hash = $13`,
		worldID, next.ReporterActorCode, next.SubjectActorCode, next.AssignmentRevision,
		next.AssignmentStatus, next.LastFrameSequence, next.EventChainHash, next.StateHash,
		previous.AssignmentRevision, previous.AssignmentStatus, previous.LastFrameSequence,
		previous.EventChainHash, previous.StateHash,
	)
	if err != nil {
		return fmt.Errorf("update realtime character case-evidence assignment head: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check realtime character case-evidence assignment head update: %w", err)
	}
	if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_revision"})
	}
	return nil
}

// closeCityRealtimeCharacterCaseEvidenceAssignmentsForExpiredSource keeps an
// already-written procedural link auditable while revoking its active window.
// It never changes the underlying report/intake, Law event, Rule, Case,
// profile, inventory, ledger, platform balance, virtual currency, or reward.
func closeCityRealtimeCharacterCaseEvidenceAssignmentsForExpiredSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	expiredEvidence cityRealtimeCharacterCaseEvidenceHead,
) (int, int, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 ||
		!cityRealtimeCharacterCaseEvidenceHeadValid(expiredEvidence) ||
		expiredEvidence.EvidenceRevision != 2 || expiredEvidence.EvidenceStatus != cityRealtimeCharacterCaseEvidenceExpired ||
		expiredEvidence.LastFrameSequence != frameSequence {
		return 0, 0, ErrCityInvalidInput
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if err != nil {
		return 0, 0, err
	}
	if agentState == nil || agentState.Binding == nil ||
		!cityRealtimeAgentCharacterCaseEvidenceAssignmentRuntimeEnabled(*agentState.Binding) {
		return 0, 0, nil
	}
	binding, err := loadCityRealtimeCharacterCaseEvidenceAssignmentBinding(ctx, tx, worldID)
	if err != nil {
		return 0, 0, err
	}
	if binding == nil || binding.AgentBindingHash != agentState.Binding.BindingHash {
		return 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_scope"})
	}
	rows, err := tx.QueryContext(ctx, `
SELECT reporter_actor_code, subject_actor_code, report_event_sequence, report_event_hash,
       evidence_code, source_law_event_sequence, source_law_event_hash,
       source_frame_sequence, assignment_revision, assignment_status,
       assigned_frame_sequence, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_case_evidence_assignment_heads
WHERE world_id = $1 AND evidence_code = $2
  AND assignment_revision = 1 AND assignment_status = 'linked_active'
ORDER BY reporter_actor_code ASC, subject_actor_code ASC
FOR UPDATE`, worldID, expiredEvidence.EvidenceCode)
	if err != nil {
		return 0, 0, fmt.Errorf("load realtime character case-evidence assignments for closure: %w", err)
	}
	heads := make([]cityRealtimeCharacterCaseEvidenceAssignmentHead, 0, 1)
	for rows.Next() {
		head := cityRealtimeCharacterCaseEvidenceAssignmentHead{}
		if err = rows.Scan(
			&head.ReporterActorCode, &head.SubjectActorCode, &head.ReportEventSequence, &head.ReportEventHash,
			&head.EvidenceCode, &head.SourceLawEventSequence, &head.SourceLawEventHash,
			&head.SourceFrameSequence, &head.AssignmentRevision, &head.AssignmentStatus,
			&head.AssignedFrameSequence, &head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
		); err != nil {
			_ = rows.Close()
			return 0, 0, err
		}
		heads = append(heads, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return 0, 0, fmt.Errorf("iterate realtime character case-evidence assignments for closure: %w", err)
	}
	if err = rows.Close(); err != nil {
		return 0, 0, fmt.Errorf("close realtime character case-evidence assignments for closure: %w", err)
	}
	if len(heads) == 0 {
		return 0, 0, nil
	}
	if err = enableCityRealtimeCharacterCaseEvidenceAssignmentMutationGate(ctx, tx, worldID, frameSequence); err != nil {
		return 0, 0, err
	}
	procedureDispatchCloseCount := 0
	for _, head := range heads {
		// The evidence head has just advanced, so validate the source linkage
		// manually before deriving the second assignment event.
		if !cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(head) ||
			head.EvidenceCode != expiredEvidence.EvidenceCode || head.SubjectActorCode != expiredEvidence.SubjectActorCode ||
			head.SourceLawEventSequence != expiredEvidence.SourceLawEventSequence ||
			head.SourceLawEventHash != expiredEvidence.SourceLawEventHash ||
			head.SourceFrameSequence != expiredEvidence.SourceFrameSequence {
			return 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_close_scope"})
		}
		next, assignmentEvent, closeErr := cityRealtimeCharacterCloseCaseEvidenceAssignment(head, expiredEvidence, frameSequence)
		if closeErr != nil {
			return 0, 0, closeErr
		}
		if err = insertCityRealtimeCharacterCaseEvidenceAssignmentEvent(ctx, tx, worldID, assignmentEvent); err != nil {
			return 0, 0, err
		}
		if err = updateCityRealtimeCharacterCaseEvidenceAssignmentHead(ctx, tx, worldID, head, next); err != nil {
			return 0, 0, err
		}
		procedureCloseCount, procedureCloseErr := closeCityRealtimeCharacterCaseProcedureDispatchForClosedAssignment(
			ctx, tx, worldID, frameSequence, next,
		)
		if procedureCloseErr != nil {
			return 0, 0, procedureCloseErr
		}
		procedureDispatchCloseCount += procedureCloseCount
	}
	return len(heads), procedureDispatchCloseCount, nil
}

func loadCityRealtimeCharacterCaseEvidenceAssignmentHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseEvidenceAssignmentHashState, error) {
	binding, err := loadCityRealtimeCharacterCaseEvidenceAssignmentBinding(ctx, queryer, worldID)
	if err != nil || binding == nil {
		return nil, err
	}
	state := &cityRealtimeCharacterCaseEvidenceAssignmentHashState{
		SchemaVersion: cityRealtimeCharacterCaseEvidenceAssignmentSchemaVersion,
		Binding:       binding,
		Heads:         make([]cityRealtimeCharacterCaseEvidenceAssignmentHead, 0),
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT reporter_actor_code, subject_actor_code, report_event_sequence, report_event_hash,
       evidence_code, source_law_event_sequence, source_law_event_hash,
       source_frame_sequence, assignment_revision, assignment_status,
       assigned_frame_sequence, last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_case_evidence_assignment_heads
WHERE world_id = $1
ORDER BY reporter_actor_code ASC, subject_actor_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-evidence assignment heads: %w", err)
	}
	heads := make([]cityRealtimeCharacterCaseEvidenceAssignmentHead, 0)
	for rows.Next() {
		head := cityRealtimeCharacterCaseEvidenceAssignmentHead{}
		if err = rows.Scan(
			&head.ReporterActorCode, &head.SubjectActorCode, &head.ReportEventSequence, &head.ReportEventHash,
			&head.EvidenceCode, &head.SourceLawEventSequence, &head.SourceLawEventHash,
			&head.SourceFrameSequence, &head.AssignmentRevision, &head.AssignmentStatus,
			&head.AssignedFrameSequence, &head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		heads = append(heads, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character case-evidence assignment heads: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character case-evidence assignment heads: %w", err)
	}
	for _, head := range heads {
		if err = validateCityRealtimeCharacterCaseEvidenceAssignmentHeadHistory(ctx, queryer, worldID, head); err != nil {
			return nil, err
		}
		state.Heads = append(state.Heads, head)
	}
	if err = validateCityRealtimeCharacterCaseEvidenceAssignmentHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_evidence_assignment_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeCharacterCaseEvidenceAssignmentHashState(
	state *cityRealtimeCharacterCaseEvidenceAssignmentHashState,
) error {
	if state == nil || state.SchemaVersion != cityRealtimeCharacterCaseEvidenceAssignmentSchemaVersion ||
		state.Binding == nil || state.Heads == nil ||
		!cityRealtimeCharacterCaseEvidenceAssignmentBindingValid(*state.Binding) {
		return errors.New("invalid realtime character case-evidence assignment hash state")
	}
	seenEvidenceCodes := make(map[string]struct{}, len(state.Heads))
	for index, head := range state.Heads {
		if !cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(head) {
			return errors.New("invalid realtime character case-evidence assignment head")
		}
		if index > 0 && (state.Heads[index-1].ReporterActorCode > head.ReporterActorCode ||
			(state.Heads[index-1].ReporterActorCode == head.ReporterActorCode &&
				state.Heads[index-1].SubjectActorCode >= head.SubjectActorCode)) {
			return errors.New("realtime character case-evidence assignment heads are not in canonical order")
		}
		if _, exists := seenEvidenceCodes[head.EvidenceCode]; exists {
			return errors.New("realtime character case-evidence source is assigned more than once")
		}
		seenEvidenceCodes[head.EvidenceCode] = struct{}{}
	}
	return nil
}
