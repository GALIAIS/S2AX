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
	cityRealtimeCharacterCaseIntakeSchemaVersion  = 1
	cityRealtimeCharacterCaseIntakeBindingVersion = "city-realtime-character-case-intake-binding-v1"
	cityRealtimeCharacterCaseIntakeStateVersion   = "city-realtime-character-case-intake-state-v1"
	cityRealtimeCharacterCaseIntakeChainVersion   = "city-realtime-character-case-intake-chain-v1"
	cityRealtimeCharacterCaseIntakeEventVersion   = "city-realtime-character-case-intake-event-v1"
	cityRealtimeCharacterCaseIntakeExpiryVersion  = "city-realtime-character-case-intake-expiry-v1"

	// evidence_required deliberately means that this record has no evidence at
	// all. It is a bounded server work item created beside a report receipt,
	// not an allegation, Law Case, or adjudication input.
	cityRealtimeCharacterCaseIntakeEvidenceRequired  = "evidence_required"
	cityRealtimeCharacterCaseIntakeExpiredNoEvidence = "expired_no_evidence"

	// The first intake adapter has only one automatic resolution: expire the
	// unverified work item after a short server-owned evidence window. Future
	// evidence adapters must be a separate, sealed source and cannot promote a
	// report receipt by themselves.
	cityRealtimeCharacterCaseIntakeExpiryDelayUS  int64 = 30 * cityRealtimeTimeQuantumUS
	cityRealtimeCharacterCaseIntakeExpiryPriority       = 100

	cityRealtimeDueEventTypeCharacterCaseIntakeExpire = "system.realtime.character_case_intake_expire"
)

// cityRealtimeCharacterCaseIntakeBinding pins the work-item adapter to a
// 1.8 policy binding. The pre-existing 1.7 report receipt remains immutable;
// it never gains this state shape retroactively.
type cityRealtimeCharacterCaseIntakeBinding struct {
	SchemaVersion    int    `json:"schema_version"`
	AgentBindingHash string `json:"agent_binding_hash"`
	BindingHash      string `json:"binding_hash"`
}

// cityRealtimeCharacterCaseIntakeHead is the bounded state of a procedural
// work item. The report event is an immutable foreign fact, not evidence. A
// pair can have at most one work item because the associated report receipt is
// already one-time per reporter/subject pair.
type cityRealtimeCharacterCaseIntakeHead struct {
	ReporterActorCode        string `json:"reporter_actor_code"`
	SubjectActorCode         string `json:"subject_actor_code"`
	ReportEventSequence      int64  `json:"report_event_sequence"`
	ReportEventHash          string `json:"report_event_hash"`
	IntakeRevision           int64  `json:"intake_revision"`
	IntakeStatus             string `json:"intake_status"`
	SourceIntentCode         string `json:"source_intent_code"`
	OpenedFrameSequence      int64  `json:"opened_frame_sequence"`
	ExpirationDueWorldTimeUS int64  `json:"expiration_due_world_time_us"`
	LastFrameSequence        int64  `json:"last_frame_sequence"`
	EventChainHash           string `json:"event_chain_hash"`
	StateHash                string `json:"state_hash"`
}

// cityRealtimeCharacterCaseIntakeEvent is an append-only procedural fact. It
// intentionally has no natural-language report, evidence claim, case code,
// rule code, verdict, penalty, reward, or financial field.
type cityRealtimeCharacterCaseIntakeEvent struct {
	ReporterActorCode        string
	SubjectActorCode         string
	ReportEventSequence      int64
	ReportEventHash          string
	EventSequence            int64
	FrameSequence            int64
	EventType                string
	SourceIntentCode         string
	ExpirationDueWorldTimeUS int64
	PreviousEventHash        string
	EventHash                string
}

type cityRealtimeCharacterCaseIntakeHashState struct {
	SchemaVersion int                                     `json:"schema_version"`
	Binding       *cityRealtimeCharacterCaseIntakeBinding `json:"binding,omitempty"`
	Heads         []cityRealtimeCharacterCaseIntakeHead   `json:"heads"`
}

type cityRealtimeCharacterCaseIntakeExpiryDuePayload struct {
	SchemaVersion       int    `json:"schema_version"`
	ReporterActorCode   string `json:"reporter_actor_code"`
	SubjectActorCode    string `json:"subject_actor_code"`
	ReportEventSequence int64  `json:"report_event_sequence"`
	ReportEventHash     string `json:"report_event_hash"`
	SourceIntentCode    string `json:"source_intent_code"`
}

func cityRealtimeCharacterCaseIntakeBindingHash(agentBindingHash string) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseIntakeBindingVersion,
		agentBindingHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseIntakeChainGenesisHash(
	reporterActorCode, subjectActorCode string,
	reportEventSequence int64,
	reportEventHash string,
) (string, error) {
	if !cityRealtimePlayerActorCodeValid(reporterActorCode) ||
		!cityRealtimeAgentIdentifierValid(subjectActorCode, 96) || reporterActorCode == subjectActorCode ||
		reportEventSequence != 1 || !cityRealtimeSHA256Hex(reportEventHash) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseIntakeChainVersion,
		reporterActorCode,
		subjectActorCode,
		strconv.FormatInt(reportEventSequence, 10),
		reportEventHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterCaseIntakeHeadStateHashUnchecked(head cityRealtimeCharacterCaseIntakeHead) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseIntakeStateVersion,
		head.ReporterActorCode,
		head.SubjectActorCode,
		strconv.FormatInt(head.ReportEventSequence, 10),
		head.ReportEventHash,
		strconv.FormatInt(head.IntakeRevision, 10),
		head.IntakeStatus,
		head.SourceIntentCode,
		strconv.FormatInt(head.OpenedFrameSequence, 10),
		strconv.FormatInt(head.ExpirationDueWorldTimeUS, 10),
		strconv.FormatInt(head.LastFrameSequence, 10),
		head.EventChainHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseIntakeHeadValid(head cityRealtimeCharacterCaseIntakeHead) bool {
	if !cityRealtimePlayerActorCodeValid(head.ReporterActorCode) ||
		!cityRealtimeAgentIdentifierValid(head.SubjectActorCode, 96) ||
		head.ReporterActorCode == head.SubjectActorCode || head.ReportEventSequence != 1 ||
		!cityRealtimeSHA256Hex(head.ReportEventHash) || !cityRealtimeAgentIdentifierValid(head.SourceIntentCode, 96) ||
		head.OpenedFrameSequence <= 0 || head.LastFrameSequence <= 0 ||
		head.ExpirationDueWorldTimeUS <= 0 ||
		head.ExpirationDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 ||
		!cityRealtimeSHA256Hex(head.EventChainHash) || !cityRealtimeSHA256Hex(head.StateHash) {
		return false
	}
	switch head.IntakeRevision {
	case 1:
		if head.IntakeStatus != cityRealtimeCharacterCaseIntakeEvidenceRequired ||
			head.LastFrameSequence != head.OpenedFrameSequence {
			return false
		}
	case 2:
		if head.IntakeStatus != cityRealtimeCharacterCaseIntakeExpiredNoEvidence ||
			head.LastFrameSequence <= head.OpenedFrameSequence {
			return false
		}
	default:
		return false
	}
	return head.StateHash == cityRealtimeCharacterCaseIntakeHeadStateHashUnchecked(head)
}

func cityRealtimeCharacterCaseIntakeEventValid(event cityRealtimeCharacterCaseIntakeEvent) bool {
	if !cityRealtimePlayerActorCodeValid(event.ReporterActorCode) ||
		!cityRealtimeAgentIdentifierValid(event.SubjectActorCode, 96) ||
		event.ReporterActorCode == event.SubjectActorCode || event.ReportEventSequence != 1 ||
		!cityRealtimeSHA256Hex(event.ReportEventHash) || event.EventSequence <= 0 ||
		event.FrameSequence <= 0 || !cityRealtimeAgentIdentifierValid(event.SourceIntentCode, 96) ||
		event.ExpirationDueWorldTimeUS <= 0 ||
		event.ExpirationDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 ||
		!cityRealtimeSHA256Hex(event.PreviousEventHash) ||
		(event.EventHash != "" && !cityRealtimeSHA256Hex(event.EventHash)) {
		return false
	}
	return (event.EventSequence == 1 && event.EventType == cityRealtimeCharacterCaseIntakeEvidenceRequired) ||
		(event.EventSequence == 2 && event.EventType == cityRealtimeCharacterCaseIntakeExpiredNoEvidence)
}

func cityRealtimeCharacterCaseIntakeEventHash(event cityRealtimeCharacterCaseIntakeEvent) (string, error) {
	if !cityRealtimeCharacterCaseIntakeEventValid(event) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseIntakeEventVersion,
		event.ReporterActorCode,
		event.SubjectActorCode,
		strconv.FormatInt(event.ReportEventSequence, 10),
		event.ReportEventHash,
		strconv.FormatInt(event.EventSequence, 10),
		strconv.FormatInt(event.FrameSequence, 10),
		event.EventType,
		event.SourceIntentCode,
		strconv.FormatInt(event.ExpirationDueWorldTimeUS, 10),
		event.PreviousEventHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterCaseIntakeReportReceiptValid(
	report cityRealtimeCharacterCaseReportHead,
	reportEvent cityRealtimeCharacterCaseReportEvent,
) bool {
	return cityRealtimeCharacterCaseReportHeadValid(report) &&
		cityRealtimeCharacterCaseReportEventValid(reportEvent) &&
		report.ReportRevision == 1 && report.ReportStatus == cityRealtimeCharacterCaseReportFiledUnverified &&
		reportEvent.EventSequence == report.ReportRevision &&
		reportEvent.EventType == cityRealtimeCharacterCaseReportFiledUnverified &&
		reportEvent.ReporterActorCode == report.ReporterActorCode &&
		reportEvent.SubjectActorCode == report.SubjectActorCode &&
		reportEvent.SourceIntentCode == report.SourceIntentCode &&
		reportEvent.FrameSequence == report.FiledFrameSequence &&
		reportEvent.EventHash == report.EventChainHash
}

func cityRealtimeCharacterOpenCaseIntake(
	report cityRealtimeCharacterCaseReportHead,
	reportEvent cityRealtimeCharacterCaseReportEvent,
	frameSequence, expirationDueWorldTimeUS int64,
) (cityRealtimeCharacterCaseIntakeHead, cityRealtimeCharacterCaseIntakeEvent, error) {
	if !cityRealtimeCharacterCaseIntakeReportReceiptValid(report, reportEvent) ||
		frameSequence != reportEvent.FrameSequence || frameSequence <= 0 ||
		expirationDueWorldTimeUS <= 0 ||
		expirationDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return cityRealtimeCharacterCaseIntakeHead{}, cityRealtimeCharacterCaseIntakeEvent{}, ErrCityInvalidInput
	}
	genesisHash, err := cityRealtimeCharacterCaseIntakeChainGenesisHash(
		report.ReporterActorCode, report.SubjectActorCode, reportEvent.EventSequence, reportEvent.EventHash,
	)
	if err != nil {
		return cityRealtimeCharacterCaseIntakeHead{}, cityRealtimeCharacterCaseIntakeEvent{}, err
	}
	event := cityRealtimeCharacterCaseIntakeEvent{
		ReporterActorCode:        report.ReporterActorCode,
		SubjectActorCode:         report.SubjectActorCode,
		ReportEventSequence:      reportEvent.EventSequence,
		ReportEventHash:          reportEvent.EventHash,
		EventSequence:            1,
		FrameSequence:            frameSequence,
		EventType:                cityRealtimeCharacterCaseIntakeEvidenceRequired,
		SourceIntentCode:         report.SourceIntentCode,
		ExpirationDueWorldTimeUS: expirationDueWorldTimeUS,
		PreviousEventHash:        genesisHash,
	}
	event.EventHash, err = cityRealtimeCharacterCaseIntakeEventHash(event)
	if err != nil {
		return cityRealtimeCharacterCaseIntakeHead{}, cityRealtimeCharacterCaseIntakeEvent{}, err
	}
	head := cityRealtimeCharacterCaseIntakeHead{
		ReporterActorCode:        report.ReporterActorCode,
		SubjectActorCode:         report.SubjectActorCode,
		ReportEventSequence:      reportEvent.EventSequence,
		ReportEventHash:          reportEvent.EventHash,
		IntakeRevision:           event.EventSequence,
		IntakeStatus:             cityRealtimeCharacterCaseIntakeEvidenceRequired,
		SourceIntentCode:         report.SourceIntentCode,
		OpenedFrameSequence:      frameSequence,
		ExpirationDueWorldTimeUS: expirationDueWorldTimeUS,
		LastFrameSequence:        frameSequence,
		EventChainHash:           event.EventHash,
	}
	head.StateHash = cityRealtimeCharacterCaseIntakeHeadStateHashUnchecked(head)
	if !cityRealtimeCharacterCaseIntakeHeadValid(head) {
		return cityRealtimeCharacterCaseIntakeHead{}, cityRealtimeCharacterCaseIntakeEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_open"})
	}
	return head, event, nil
}

func cityRealtimeCharacterExpireCaseIntake(
	previous cityRealtimeCharacterCaseIntakeHead,
	frameSequence, dueWorldTimeUS int64,
) (cityRealtimeCharacterCaseIntakeHead, cityRealtimeCharacterCaseIntakeEvent, error) {
	if !cityRealtimeCharacterCaseIntakeHeadValid(previous) ||
		previous.IntakeRevision != 1 || previous.IntakeStatus != cityRealtimeCharacterCaseIntakeEvidenceRequired ||
		frameSequence <= previous.LastFrameSequence || dueWorldTimeUS != previous.ExpirationDueWorldTimeUS {
		return cityRealtimeCharacterCaseIntakeHead{}, cityRealtimeCharacterCaseIntakeEvent{}, ErrCityInvalidInput
	}
	event := cityRealtimeCharacterCaseIntakeEvent{
		ReporterActorCode:        previous.ReporterActorCode,
		SubjectActorCode:         previous.SubjectActorCode,
		ReportEventSequence:      previous.ReportEventSequence,
		ReportEventHash:          previous.ReportEventHash,
		EventSequence:            2,
		FrameSequence:            frameSequence,
		EventType:                cityRealtimeCharacterCaseIntakeExpiredNoEvidence,
		SourceIntentCode:         previous.SourceIntentCode,
		ExpirationDueWorldTimeUS: dueWorldTimeUS,
		PreviousEventHash:        previous.EventChainHash,
	}
	var err error
	event.EventHash, err = cityRealtimeCharacterCaseIntakeEventHash(event)
	if err != nil {
		return cityRealtimeCharacterCaseIntakeHead{}, cityRealtimeCharacterCaseIntakeEvent{}, err
	}
	next := previous
	next.IntakeRevision = event.EventSequence
	next.IntakeStatus = cityRealtimeCharacterCaseIntakeExpiredNoEvidence
	next.LastFrameSequence = frameSequence
	next.EventChainHash = event.EventHash
	next.StateHash = cityRealtimeCharacterCaseIntakeHeadStateHashUnchecked(next)
	if !cityRealtimeCharacterCaseIntakeHeadValid(next) {
		return cityRealtimeCharacterCaseIntakeHead{}, cityRealtimeCharacterCaseIntakeEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_expire"})
	}
	return next, event, nil
}

func initializeCityRealtimeCharacterCaseIntakeFoundation(
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
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_policy"})
	}
	if !cityRealtimeAgentCharacterCaseIntakeRuntimeEnabled(*agentState.Binding) {
		return nil
	}
	reportBinding, err := loadCityRealtimeCharacterCaseReportBinding(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if reportBinding == nil || reportBinding.AgentBindingHash != agentState.Binding.BindingHash {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_report_scope"})
	}
	binding := cityRealtimeCharacterCaseIntakeBinding{
		SchemaVersion:    cityRealtimeCharacterCaseIntakeSchemaVersion,
		AgentBindingHash: agentState.Binding.BindingHash,
	}
	binding.BindingHash = cityRealtimeCharacterCaseIntakeBindingHash(binding.AgentBindingHash)
	if !cityRealtimeCharacterCaseIntakeBindingValid(binding) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_binding"})
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_character_case_intake_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("activate realtime character case-intake initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_intake_world_bindings
    (world_id, schema_version, agent_binding_hash, binding_hash, metadata)
VALUES ($1, $2, $3, $4, '{}'::jsonb)`,
		worldID, binding.SchemaVersion, binding.AgentBindingHash, binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime character case-intake binding: %w", err)
	}
	return nil
}

func enableCityRealtimeCharacterCaseIntakeMutationGate(
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
		{name: "sub2api.city_realtime_character_case_intake_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_case_intake_frame_sequence", value: frameSequence},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character case-intake gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func cityRealtimeCharacterCaseIntakeBindingValid(binding cityRealtimeCharacterCaseIntakeBinding) bool {
	return binding.SchemaVersion == cityRealtimeCharacterCaseIntakeSchemaVersion &&
		cityRealtimeSHA256Hex(binding.AgentBindingHash) && cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeCharacterCaseIntakeBindingHash(binding.AgentBindingHash)
}

func loadCityRealtimeCharacterCaseIntakeBinding(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseIntakeBinding, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterCaseIntakeBinding{}
	var policyID, policyVersion, agentBindingHash string
	err := queryer.QueryRowContext(ctx, `
SELECT intake.schema_version, intake.agent_binding_hash, intake.binding_hash,
       agent.policy_id, agent.policy_version, agent.binding_hash
FROM city_realtime_character_case_intake_world_bindings intake
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = intake.world_id
WHERE intake.world_id = $1`, worldID).Scan(
		&binding.SchemaVersion, &binding.AgentBindingHash, &binding.BindingHash,
		&policyID, &policyVersion, &agentBindingHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-intake binding: %w", err)
	}
	if policyID != cityRealtimeAgentCorePolicyID ||
		(policyVersion != cityRealtimeAgentCorePolicyVersionIntake &&
			policyVersion != cityRealtimeAgentCorePolicyVersionEvidence &&
			policyVersion != cityRealtimeAgentCorePolicyVersionEvidenceAssignment &&
			policyVersion != cityRealtimeAgentCorePolicyVersionProcedureDispatch &&
			policyVersion != cityRealtimeAgentCorePolicyVersionTask &&
			policyVersion != cityRealtimeAgentCorePolicyVersionNavigationPlan) ||
		binding.AgentBindingHash != agentBindingHash || !cityRealtimeCharacterCaseIntakeBindingValid(binding) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_binding"})
	}
	return &binding, nil
}

func loadCityRealtimeCharacterCaseIntakeReportReceipt(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	reporterActorCode, subjectActorCode string,
	forUpdate bool,
) (cityRealtimeCharacterCaseReportHead, cityRealtimeCharacterCaseReportEvent, bool, error) {
	report, found, err := loadCityRealtimeCharacterCaseReportHead(
		ctx, queryer, worldID, reporterActorCode, subjectActorCode, forUpdate,
	)
	if err != nil || !found {
		return cityRealtimeCharacterCaseReportHead{}, cityRealtimeCharacterCaseReportEvent{}, found, err
	}
	if err = validateCityRealtimeCharacterCaseReportHeadHistory(ctx, queryer, worldID, report); err != nil {
		return cityRealtimeCharacterCaseReportHead{}, cityRealtimeCharacterCaseReportEvent{}, false, err
	}
	query := `
SELECT reporter_actor_code, subject_actor_code, event_sequence, frame_sequence,
       event_type, source_intent_code, previous_event_hash, event_hash
FROM city_realtime_character_case_report_events
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3
  AND event_sequence = 1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	reportEvent := cityRealtimeCharacterCaseReportEvent{}
	err = queryer.QueryRowContext(ctx, query, worldID, reporterActorCode, subjectActorCode).Scan(
		&reportEvent.ReporterActorCode, &reportEvent.SubjectActorCode, &reportEvent.EventSequence,
		&reportEvent.FrameSequence, &reportEvent.EventType, &reportEvent.SourceIntentCode,
		&reportEvent.PreviousEventHash, &reportEvent.EventHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterCaseReportHead{}, cityRealtimeCharacterCaseReportEvent{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_report_event"})
	}
	if err != nil {
		return cityRealtimeCharacterCaseReportHead{}, cityRealtimeCharacterCaseReportEvent{}, false,
			fmt.Errorf("load realtime character case-intake report receipt: %w", err)
	}
	if !cityRealtimeCharacterCaseIntakeReportReceiptValid(report, reportEvent) {
		return cityRealtimeCharacterCaseReportHead{}, cityRealtimeCharacterCaseReportEvent{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_report_receipt"})
	}
	return report, reportEvent, true, nil
}

func loadCityRealtimeCharacterCaseIntakeHead(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	reporterActorCode, subjectActorCode string,
	forUpdate bool,
) (cityRealtimeCharacterCaseIntakeHead, bool, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(reporterActorCode) ||
		!cityRealtimeAgentIdentifierValid(subjectActorCode, 96) || reporterActorCode == subjectActorCode {
		return cityRealtimeCharacterCaseIntakeHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT reporter_actor_code, subject_actor_code, report_event_sequence,
       report_event_hash, intake_revision, intake_status, source_intent_code,
       opened_frame_sequence, expiration_due_world_time_us, last_frame_sequence,
       event_chain_hash, state_hash
FROM city_realtime_character_case_intake_heads
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head := cityRealtimeCharacterCaseIntakeHead{}
	err := queryer.QueryRowContext(ctx, query, worldID, reporterActorCode, subjectActorCode).Scan(
		&head.ReporterActorCode, &head.SubjectActorCode, &head.ReportEventSequence,
		&head.ReportEventHash, &head.IntakeRevision, &head.IntakeStatus, &head.SourceIntentCode,
		&head.OpenedFrameSequence, &head.ExpirationDueWorldTimeUS, &head.LastFrameSequence,
		&head.EventChainHash, &head.StateHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterCaseIntakeHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterCaseIntakeHead{}, false, fmt.Errorf("load realtime character case-intake head: %w", err)
	}
	if !cityRealtimeCharacterCaseIntakeHeadValid(head) {
		return cityRealtimeCharacterCaseIntakeHead{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_head"})
	}
	return head, true, nil
}

func validateCityRealtimeCharacterCaseIntakeHeadHistory(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterCaseIntakeHead,
) error {
	if worldID <= 0 || !cityRealtimeCharacterCaseIntakeHeadValid(head) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_history"})
	}
	report, reportEvent, found, err := loadCityRealtimeCharacterCaseIntakeReportReceipt(
		ctx, queryer, worldID, head.ReporterActorCode, head.SubjectActorCode, false,
	)
	if err != nil || !found || reportEvent.EventSequence != head.ReportEventSequence ||
		reportEvent.EventHash != head.ReportEventHash || report.SourceIntentCode != head.SourceIntentCode {
		if err != nil {
			return err
		}
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_receipt"})
	}
	genesisHash, err := cityRealtimeCharacterCaseIntakeChainGenesisHash(
		head.ReporterActorCode, head.SubjectActorCode, head.ReportEventSequence, head.ReportEventHash,
	)
	if err != nil {
		return err
	}
	lastEvent := cityRealtimeCharacterCaseIntakeEvent{}
	err = queryer.QueryRowContext(ctx, `
SELECT reporter_actor_code, subject_actor_code, report_event_sequence,
       report_event_hash, event_sequence, frame_sequence, event_type,
       source_intent_code, expiration_due_world_time_us, previous_event_hash, event_hash
FROM city_realtime_character_case_intake_events
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3
ORDER BY event_sequence DESC
LIMIT 1`, worldID, head.ReporterActorCode, head.SubjectActorCode).Scan(
		&lastEvent.ReporterActorCode, &lastEvent.SubjectActorCode, &lastEvent.ReportEventSequence,
		&lastEvent.ReportEventHash, &lastEvent.EventSequence, &lastEvent.FrameSequence,
		&lastEvent.EventType, &lastEvent.SourceIntentCode, &lastEvent.ExpirationDueWorldTimeUS,
		&lastEvent.PreviousEventHash, &lastEvent.EventHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_event_head"})
	}
	if err != nil {
		return fmt.Errorf("load realtime character case-intake event head: %w", err)
	}
	expectedEventHash, hashErr := cityRealtimeCharacterCaseIntakeEventHash(lastEvent)
	if hashErr != nil || !cityRealtimeCharacterCaseIntakeEventValid(lastEvent) ||
		lastEvent.EventHash != expectedEventHash || lastEvent.EventSequence != head.IntakeRevision ||
		lastEvent.FrameSequence != head.LastFrameSequence || lastEvent.EventHash != head.EventChainHash ||
		lastEvent.SourceIntentCode != head.SourceIntentCode ||
		lastEvent.ExpirationDueWorldTimeUS != head.ExpirationDueWorldTimeUS {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_event_head"})
	}
	if head.IntakeRevision == 1 && lastEvent.PreviousEventHash != genesisHash {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_genesis"})
	}
	return nil
}

func insertCityRealtimeCharacterCaseIntakeHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	head cityRealtimeCharacterCaseIntakeHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseIntakeHeadValid(head) || head.IntakeRevision != 1 {
		return ErrCityInvalidInput
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_intake_heads
    (world_id, reporter_actor_code, subject_actor_code, report_event_sequence,
     report_event_hash, intake_revision, intake_status, source_intent_code,
     opened_frame_sequence, expiration_due_world_time_us, last_frame_sequence,
     event_chain_hash, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, '{}'::jsonb)`,
		worldID, head.ReporterActorCode, head.SubjectActorCode, head.ReportEventSequence,
		head.ReportEventHash, head.IntakeRevision, head.IntakeStatus, head.SourceIntentCode,
		head.OpenedFrameSequence, head.ExpirationDueWorldTimeUS, head.LastFrameSequence,
		head.EventChainHash, head.StateHash,
	)
	if err != nil {
		return fmt.Errorf("insert realtime character case-intake head: %w", err)
	}
	return nil
}

func insertCityRealtimeCharacterCaseIntakeEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	event cityRealtimeCharacterCaseIntakeEvent,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseIntakeEventValid(event) ||
		!cityRealtimeSHA256Hex(event.EventHash) {
		return ErrCityInvalidInput
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_intake_events
    (world_id, reporter_actor_code, subject_actor_code, report_event_sequence,
     report_event_hash, event_sequence, frame_sequence, event_type,
     source_intent_code, expiration_due_world_time_us, previous_event_hash,
     event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, '{}'::jsonb)`,
		worldID, event.ReporterActorCode, event.SubjectActorCode, event.ReportEventSequence,
		event.ReportEventHash, event.EventSequence, event.FrameSequence, event.EventType,
		event.SourceIntentCode, event.ExpirationDueWorldTimeUS, event.PreviousEventHash,
		event.EventHash,
	)
	if err != nil {
		return fmt.Errorf("append realtime character case-intake event: %w", err)
	}
	return nil
}

func updateCityRealtimeCharacterCaseIntakeHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previous, next cityRealtimeCharacterCaseIntakeHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseIntakeHeadValid(previous) ||
		!cityRealtimeCharacterCaseIntakeHeadValid(next) ||
		previous.ReporterActorCode != next.ReporterActorCode || previous.SubjectActorCode != next.SubjectActorCode ||
		previous.ReportEventSequence != next.ReportEventSequence || previous.ReportEventHash != next.ReportEventHash ||
		next.IntakeRevision != previous.IntakeRevision+1 || next.LastFrameSequence <= previous.LastFrameSequence ||
		next.EventChainHash == previous.EventChainHash {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_case_intake_heads
SET intake_revision = $4, intake_status = $5, source_intent_code = $6,
    opened_frame_sequence = $7, expiration_due_world_time_us = $8,
    last_frame_sequence = $9, event_chain_hash = $10, state_hash = $11, updated_at = NOW()
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3
  AND intake_revision = $12 AND intake_status = $13 AND source_intent_code = $14
  AND opened_frame_sequence = $15 AND expiration_due_world_time_us = $16
  AND last_frame_sequence = $17 AND event_chain_hash = $18 AND state_hash = $19`,
		worldID, next.ReporterActorCode, next.SubjectActorCode, next.IntakeRevision, next.IntakeStatus,
		next.SourceIntentCode, next.OpenedFrameSequence, next.ExpirationDueWorldTimeUS,
		next.LastFrameSequence, next.EventChainHash, next.StateHash,
		previous.IntakeRevision, previous.IntakeStatus, previous.SourceIntentCode,
		previous.OpenedFrameSequence, previous.ExpirationDueWorldTimeUS,
		previous.LastFrameSequence, previous.EventChainHash, previous.StateHash,
	)
	if err != nil {
		return fmt.Errorf("advance realtime character case-intake head: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime character case-intake head update: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_revision"})
	}
	return nil
}

func cityRealtimeCharacterCaseIntakeExpiryDedupKey(
	reporterActorCode, subjectActorCode, sourceIntentCode string,
	reportEventHash string,
) (string, error) {
	if !cityRealtimePlayerActorCodeValid(reporterActorCode) ||
		!cityRealtimeAgentIdentifierValid(subjectActorCode, 96) || reporterActorCode == subjectActorCode ||
		!cityRealtimeAgentIdentifierValid(sourceIntentCode, 96) || !cityRealtimeSHA256Hex(reportEventHash) {
		return "", ErrCityInvalidInput
	}
	key := "case-intake-expire." + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseIntakeExpiryVersion,
		reporterActorCode,
		subjectActorCode,
		reportEventHash,
		sourceIntentCode,
	}, "\x1f")))
	if !cityRealtimeDueEventIdentifierValid(key, 160) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_dedup"})
	}
	return key, nil
}

func cityRealtimeCharacterCaseIntakeAggregateKey(
	reporterActorCode, subjectActorCode, reportEventHash string,
) (string, error) {
	if !cityRealtimePlayerActorCodeValid(reporterActorCode) ||
		!cityRealtimeAgentIdentifierValid(subjectActorCode, 96) || reporterActorCode == subjectActorCode ||
		!cityRealtimeSHA256Hex(reportEventHash) {
		return "", ErrCityInvalidInput
	}
	key := "case-intake:" + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseIntakeExpiryVersion,
		reporterActorCode,
		subjectActorCode,
		reportEventHash,
	}, "\x1f")))
	if !cityRealtimeDueEventIdentifierValid(key, 160) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_aggregate"})
	}
	return key, nil
}

func cityRealtimeCharacterCaseIntakeExpirationDueWorldTime(currentWorldTimeUS int64) (int64, error) {
	if currentWorldTimeUS < 0 ||
		currentWorldTimeUS > cityRealtimeMaximumWorldTimeUS-cityRealtimeCharacterCaseIntakeExpiryDelayUS {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_due_time"})
	}
	dueWorldTimeUS := currentWorldTimeUS + cityRealtimeCharacterCaseIntakeExpiryDelayUS
	if dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_due_time"})
	}
	return dueWorldTimeUS, nil
}

func scheduleCityRealtimeCharacterCaseIntakeExpiryDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, dueWorldTimeUS, createdFrameSequence int64,
	head cityRealtimeCharacterCaseIntakeHead,
) error {
	if tx == nil || worldID <= 0 || dueWorldTimeUS <= 0 ||
		dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 || createdFrameSequence <= 0 ||
		!cityRealtimeCharacterCaseIntakeHeadValid(head) ||
		head.IntakeRevision != 1 || head.IntakeStatus != cityRealtimeCharacterCaseIntakeEvidenceRequired ||
		head.ExpirationDueWorldTimeUS != dueWorldTimeUS {
		return ErrCityInvalidInput
	}
	dedupKey, err := cityRealtimeCharacterCaseIntakeExpiryDedupKey(
		head.ReporterActorCode, head.SubjectActorCode, head.SourceIntentCode, head.ReportEventHash,
	)
	if err != nil {
		return err
	}
	aggregateKey, err := cityRealtimeCharacterCaseIntakeAggregateKey(
		head.ReporterActorCode, head.SubjectActorCode, head.ReportEventHash,
	)
	if err != nil {
		return err
	}
	payload, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":        cityRealtimeCharacterCaseIntakeSchemaVersion,
		"reporter_actor_code":   head.ReporterActorCode,
		"subject_actor_code":    head.SubjectActorCode,
		"report_event_sequence": head.ReportEventSequence,
		"report_event_hash":     head.ReportEventHash,
		"source_intent_code":    head.SourceIntentCode,
	})
	if err != nil {
		return fmt.Errorf("canonicalize realtime character case-intake expiry payload: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_due_events
    (world_id, event_type, schema_version, due_world_time_us, temporal_phase,
     priority, aggregate_type, aggregate_key, dedup_key, source_kind,
     source_reference, payload, payload_hash, expected_version, status,
     created_frame_sequence)
VALUES ($1, $2, 1, $3, 'rule_effect', $4, 'realtime_case_intake', $5, $6, 'system',
        'realtime_character_case_intake', $7::jsonb, $8, 1, 'pending', $9)`,
		worldID, cityRealtimeDueEventTypeCharacterCaseIntakeExpire, dueWorldTimeUS,
		cityRealtimeCharacterCaseIntakeExpiryPriority, aggregateKey, dedupKey,
		[]byte(payload), payloadHash, createdFrameSequence,
	); err != nil {
		return fmt.Errorf("schedule realtime character case-intake expiry: %w", err)
	}
	return nil
}

func decodeCityRealtimeCharacterCaseIntakeExpiryDuePayload(
	event cityRealtimeDueEventRecord,
) (cityRealtimeCharacterCaseIntakeExpiryDuePayload, bool) {
	payload := cityRealtimeCharacterCaseIntakeExpiryDuePayload{}
	if err := decodeStrictCityObject(event.Payload, &payload); err != nil ||
		payload.SchemaVersion != cityRealtimeCharacterCaseIntakeSchemaVersion ||
		!cityRealtimePlayerActorCodeValid(payload.ReporterActorCode) ||
		!cityRealtimeAgentIdentifierValid(payload.SubjectActorCode, 96) ||
		payload.ReporterActorCode == payload.SubjectActorCode || payload.ReportEventSequence != 1 ||
		!cityRealtimeSHA256Hex(payload.ReportEventHash) ||
		!cityRealtimeAgentIdentifierValid(payload.SourceIntentCode, 96) {
		return cityRealtimeCharacterCaseIntakeExpiryDuePayload{}, false
	}
	_, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":        payload.SchemaVersion,
		"reporter_actor_code":   payload.ReporterActorCode,
		"subject_actor_code":    payload.SubjectActorCode,
		"report_event_sequence": payload.ReportEventSequence,
		"report_event_hash":     payload.ReportEventHash,
		"source_intent_code":    payload.SourceIntentCode,
	})
	if err != nil || payloadHash != event.PayloadHash {
		return cityRealtimeCharacterCaseIntakeExpiryDuePayload{}, false
	}
	return payload, true
}

// applyCityRealtimeCharacterCaseIntakeExpiryDueEvent performs the only
// automatic state transition in this first case-process foundation. It does
// not evaluate a report, create a Law Case, modify a Rule, or touch any
// progression, inventory, ledger, platform balance, or virtual currency.
func applyCityRealtimeCharacterCaseIntakeExpiryDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	event cityRealtimeDueEventRecord,
) (bool, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 ||
		event.EventType != cityRealtimeDueEventTypeCharacterCaseIntakeExpire ||
		event.SchemaVersion != cityRealtimeCharacterCaseIntakeSchemaVersion ||
		event.SourceKind != cityRealtimeDueEventSourceKindSystem || event.TemporalPhase != "rule_effect" ||
		event.AggregateType != "realtime_case_intake" ||
		event.SourceReference != "realtime_character_case_intake" ||
		event.ExpectedVersion == nil || *event.ExpectedVersion != 1 {
		return false, nil
	}
	payload, validPayload := decodeCityRealtimeCharacterCaseIntakeExpiryDuePayload(event)
	if !validPayload {
		return false, nil
	}
	expectedAggregateKey, aggregateErr := cityRealtimeCharacterCaseIntakeAggregateKey(
		payload.ReporterActorCode, payload.SubjectActorCode, payload.ReportEventHash,
	)
	if aggregateErr != nil || event.AggregateKey != expectedAggregateKey {
		return false, nil
	}
	expectedDedupKey, dedupErr := cityRealtimeCharacterCaseIntakeExpiryDedupKey(
		payload.ReporterActorCode, payload.SubjectActorCode, payload.SourceIntentCode, payload.ReportEventHash,
	)
	if dedupErr != nil || event.DedupKey != expectedDedupKey {
		return false, nil
	}
	binding, err := loadCityRealtimeCharacterCaseIntakeBinding(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if binding == nil {
		return false, nil
	}
	head, found, err := loadCityRealtimeCharacterCaseIntakeHead(
		ctx, tx, worldID, payload.ReporterActorCode, payload.SubjectActorCode, true,
	)
	if err != nil {
		return false, err
	}
	if !found || head.IntakeRevision != 1 || head.IntakeStatus != cityRealtimeCharacterCaseIntakeEvidenceRequired ||
		head.SourceIntentCode != payload.SourceIntentCode ||
		head.ReportEventSequence != payload.ReportEventSequence || head.ReportEventHash != payload.ReportEventHash ||
		head.ExpirationDueWorldTimeUS != event.DueWorldTimeUS {
		return false, nil
	}
	if err = validateCityRealtimeCharacterCaseIntakeHeadHistory(ctx, tx, worldID, head); err != nil {
		return false, err
	}
	nextHead, intakeEvent, transitionErr := cityRealtimeCharacterExpireCaseIntake(
		head, frameSequence, event.DueWorldTimeUS,
	)
	if transitionErr != nil {
		return false, transitionErr
	}
	if err = enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); err != nil {
		return false, err
	}
	if err = enableCityRealtimeCharacterCaseIntakeMutationGate(ctx, tx, worldID, frameSequence); err != nil {
		return false, err
	}
	if err = insertCityRealtimeCharacterCaseIntakeEvent(ctx, tx, worldID, intakeEvent); err != nil {
		return false, err
	}
	if err = updateCityRealtimeCharacterCaseIntakeHead(ctx, tx, worldID, head, nextHead); err != nil {
		return false, err
	}
	return true, nil
}

func loadCityRealtimeCharacterCaseIntakeHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseIntakeHashState, error) {
	binding, err := loadCityRealtimeCharacterCaseIntakeBinding(ctx, queryer, worldID)
	if err != nil || binding == nil {
		return nil, err
	}
	state := &cityRealtimeCharacterCaseIntakeHashState{
		SchemaVersion: cityRealtimeCharacterCaseIntakeSchemaVersion,
		Binding:       binding,
		Heads:         make([]cityRealtimeCharacterCaseIntakeHead, 0),
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT reporter_actor_code, subject_actor_code, report_event_sequence,
       report_event_hash, intake_revision, intake_status, source_intent_code,
       opened_frame_sequence, expiration_due_world_time_us, last_frame_sequence,
       event_chain_hash, state_hash
FROM city_realtime_character_case_intake_heads
WHERE world_id = $1
ORDER BY reporter_actor_code ASC, subject_actor_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-intake heads: %w", err)
	}
	heads := make([]cityRealtimeCharacterCaseIntakeHead, 0)
	for rows.Next() {
		head := cityRealtimeCharacterCaseIntakeHead{}
		if err = rows.Scan(
			&head.ReporterActorCode, &head.SubjectActorCode, &head.ReportEventSequence,
			&head.ReportEventHash, &head.IntakeRevision, &head.IntakeStatus, &head.SourceIntentCode,
			&head.OpenedFrameSequence, &head.ExpirationDueWorldTimeUS, &head.LastFrameSequence,
			&head.EventChainHash, &head.StateHash,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		heads = append(heads, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character case-intake heads: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character case-intake heads: %w", err)
	}
	for _, head := range heads {
		if err = validateCityRealtimeCharacterCaseIntakeHeadHistory(ctx, queryer, worldID, head); err != nil {
			return nil, err
		}
		state.Heads = append(state.Heads, head)
	}
	if err = validateCityRealtimeCharacterCaseIntakeHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_intake_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeCharacterCaseIntakeHashState(state *cityRealtimeCharacterCaseIntakeHashState) error {
	if state == nil || state.SchemaVersion != cityRealtimeCharacterCaseIntakeSchemaVersion || state.Binding == nil ||
		state.Heads == nil || !cityRealtimeCharacterCaseIntakeBindingValid(*state.Binding) {
		return errors.New("invalid realtime character case-intake hash state")
	}
	for index, head := range state.Heads {
		if !cityRealtimeCharacterCaseIntakeHeadValid(head) {
			return errors.New("invalid realtime character case-intake head")
		}
		if index > 0 {
			previous := state.Heads[index-1]
			if previous.ReporterActorCode > head.ReporterActorCode ||
				(previous.ReporterActorCode == head.ReporterActorCode && previous.SubjectActorCode >= head.SubjectActorCode) {
				return errors.New("unordered realtime character case-intake heads")
			}
		}
	}
	return nil
}
