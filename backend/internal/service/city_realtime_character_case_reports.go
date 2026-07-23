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
	cityRealtimeCharacterCaseReportSchemaVersion  = 1
	cityRealtimeCharacterCaseReportBindingVersion = "city-realtime-character-case-report-binding-v1"
	cityRealtimeCharacterCaseReportStateVersion   = "city-realtime-character-case-report-state-v1"
	cityRealtimeCharacterCaseReportChainVersion   = "city-realtime-character-case-report-chain-v1"
	cityRealtimeCharacterCaseReportEventVersion   = "city-realtime-character-case-report-event-v1"

	// cityRealtimeCharacterCaseReportFiledUnverified is deliberately not a
	// Law Case, ruling, or evidence finding. The report carries no free text
	// and cannot be promoted into a sanction without a later, independent,
	// server-evidenced case-process adapter.
	cityRealtimeCharacterCaseReportFiledUnverified = "filed_unverified"
)

// cityRealtimeCharacterCaseReportBinding pins the non-evidentiary intake
// adapter to the policy that created the world. It is separate from the Law
// Case and review adapters so future adjudication can consume only explicitly
// verified evidence rather than silently reinterpret a report as a ruling.
type cityRealtimeCharacterCaseReportBinding struct {
	SchemaVersion    int    `json:"schema_version"`
	AgentBindingHash string `json:"agent_binding_hash"`
	BindingHash      string `json:"binding_hash"`
}

// cityRealtimeCharacterCaseReportHead is an immutable, one-time intake
// receipt between a reporting Character and an adjacent public Actor. The
// single revision intentionally caps one report per ordered pair and prevents
// an Agent from using this adapter as a repeated accusation channel.
type cityRealtimeCharacterCaseReportHead struct {
	ReporterActorCode  string `json:"reporter_actor_code"`
	SubjectActorCode   string `json:"subject_actor_code"`
	ReportRevision     int64  `json:"report_revision"`
	ReportStatus       string `json:"report_status"`
	SourceIntentCode   string `json:"source_intent_code"`
	FiledFrameSequence int64  `json:"filed_frame_sequence"`
	LastFrameSequence  int64  `json:"last_frame_sequence"`
	EventChainHash     string `json:"event_chain_hash"`
	StateHash          string `json:"state_hash"`
}

// cityRealtimeCharacterCaseReportEvent is the append-only receipt for a
// report. It contains no model output, human identity, asserted reason,
// evidence, ruling, penalty, reward, wallet, or provider data.
type cityRealtimeCharacterCaseReportEvent struct {
	ReporterActorCode string
	SubjectActorCode  string
	EventSequence     int64
	FrameSequence     int64
	EventType         string
	SourceIntentCode  string
	PreviousEventHash string
	EventHash         string
}

type cityRealtimeCharacterCaseReportHashState struct {
	SchemaVersion int                                     `json:"schema_version"`
	Binding       *cityRealtimeCharacterCaseReportBinding `json:"binding,omitempty"`
	Heads         []cityRealtimeCharacterCaseReportHead   `json:"heads"`
}

func cityRealtimeCharacterCaseReportBindingHash(agentBindingHash string) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseReportBindingVersion,
		agentBindingHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseReportChainGenesisHash(reporterActorCode, subjectActorCode string) (string, error) {
	if !cityRealtimePlayerActorCodeValid(reporterActorCode) ||
		!cityRealtimeAgentIdentifierValid(subjectActorCode, 96) || reporterActorCode == subjectActorCode {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseReportChainVersion,
		reporterActorCode,
		subjectActorCode,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterCaseReportHeadStateHashUnchecked(head cityRealtimeCharacterCaseReportHead) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseReportStateVersion,
		head.ReporterActorCode,
		head.SubjectActorCode,
		strconv.FormatInt(head.ReportRevision, 10),
		head.ReportStatus,
		head.SourceIntentCode,
		strconv.FormatInt(head.FiledFrameSequence, 10),
		strconv.FormatInt(head.LastFrameSequence, 10),
		head.EventChainHash,
	}, "\x1f")))
}

func cityRealtimeCharacterCaseReportHeadValid(head cityRealtimeCharacterCaseReportHead) bool {
	if !cityRealtimePlayerActorCodeValid(head.ReporterActorCode) ||
		!cityRealtimeAgentIdentifierValid(head.SubjectActorCode, 96) ||
		head.ReporterActorCode == head.SubjectActorCode || head.ReportRevision != 1 ||
		head.ReportStatus != cityRealtimeCharacterCaseReportFiledUnverified ||
		!cityRealtimeAgentIdentifierValid(head.SourceIntentCode, 96) ||
		head.FiledFrameSequence <= 0 || head.LastFrameSequence != head.FiledFrameSequence ||
		!cityRealtimeSHA256Hex(head.EventChainHash) || !cityRealtimeSHA256Hex(head.StateHash) {
		return false
	}
	return head.StateHash == cityRealtimeCharacterCaseReportHeadStateHashUnchecked(head)
}

func cityRealtimeCharacterCaseReportEventValid(event cityRealtimeCharacterCaseReportEvent) bool {
	return cityRealtimePlayerActorCodeValid(event.ReporterActorCode) &&
		cityRealtimeAgentIdentifierValid(event.SubjectActorCode, 96) &&
		event.ReporterActorCode != event.SubjectActorCode && event.EventSequence == 1 &&
		event.FrameSequence > 0 && event.EventType == cityRealtimeCharacterCaseReportFiledUnverified &&
		cityRealtimeAgentIdentifierValid(event.SourceIntentCode, 96) &&
		cityRealtimeSHA256Hex(event.PreviousEventHash) &&
		(event.EventHash == "" || cityRealtimeSHA256Hex(event.EventHash))
}

func cityRealtimeCharacterCaseReportEventHash(event cityRealtimeCharacterCaseReportEvent) (string, error) {
	if !cityRealtimeCharacterCaseReportEventValid(event) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterCaseReportEventVersion,
		event.ReporterActorCode,
		event.SubjectActorCode,
		strconv.FormatInt(event.EventSequence, 10),
		strconv.FormatInt(event.FrameSequence, 10),
		event.EventType,
		event.SourceIntentCode,
		event.PreviousEventHash,
	}, "\x1f"))), nil
}

// cityRealtimeCharacterFileCaseReport creates a final, non-evidentiary
// receipt. There is no generic “accusation” payload and no mutable state
// transition: the future case-process adapter must independently establish
// evidence and due process before it can create an actual Law Case.
func cityRealtimeCharacterFileCaseReport(
	reporterActorCode, subjectActorCode, sourceIntentCode string,
	frameSequence int64,
) (cityRealtimeCharacterCaseReportHead, cityRealtimeCharacterCaseReportEvent, error) {
	if !cityRealtimePlayerActorCodeValid(reporterActorCode) ||
		!cityRealtimeAgentIdentifierValid(subjectActorCode, 96) || reporterActorCode == subjectActorCode ||
		!cityRealtimeAgentIdentifierValid(sourceIntentCode, 96) || frameSequence <= 0 {
		return cityRealtimeCharacterCaseReportHead{}, cityRealtimeCharacterCaseReportEvent{}, ErrCityInvalidInput
	}
	genesisHash, err := cityRealtimeCharacterCaseReportChainGenesisHash(reporterActorCode, subjectActorCode)
	if err != nil {
		return cityRealtimeCharacterCaseReportHead{}, cityRealtimeCharacterCaseReportEvent{}, err
	}
	event := cityRealtimeCharacterCaseReportEvent{
		ReporterActorCode: reporterActorCode,
		SubjectActorCode:  subjectActorCode,
		EventSequence:     1,
		FrameSequence:     frameSequence,
		EventType:         cityRealtimeCharacterCaseReportFiledUnverified,
		SourceIntentCode:  sourceIntentCode,
		PreviousEventHash: genesisHash,
	}
	event.EventHash, err = cityRealtimeCharacterCaseReportEventHash(event)
	if err != nil {
		return cityRealtimeCharacterCaseReportHead{}, cityRealtimeCharacterCaseReportEvent{}, err
	}
	head := cityRealtimeCharacterCaseReportHead{
		ReporterActorCode:  reporterActorCode,
		SubjectActorCode:   subjectActorCode,
		ReportRevision:     event.EventSequence,
		ReportStatus:       cityRealtimeCharacterCaseReportFiledUnverified,
		SourceIntentCode:   sourceIntentCode,
		FiledFrameSequence: frameSequence,
		LastFrameSequence:  frameSequence,
		EventChainHash:     event.EventHash,
	}
	head.StateHash = cityRealtimeCharacterCaseReportHeadStateHashUnchecked(head)
	if !cityRealtimeCharacterCaseReportHeadValid(head) {
		return cityRealtimeCharacterCaseReportHead{}, cityRealtimeCharacterCaseReportEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_filed"})
	}
	return head, event, nil
}

func initializeCityRealtimeCharacterCaseReportFoundation(
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
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_policy"})
	}
	if !cityRealtimeAgentCharacterCaseReportRuntimeEnabled(*agentState.Binding) {
		return nil
	}
	socialBinding, err := loadCityRealtimeCharacterSocialBinding(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if socialBinding == nil || socialBinding.AgentBindingHash != agentState.Binding.BindingHash {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_social_scope"})
	}
	binding := cityRealtimeCharacterCaseReportBinding{
		SchemaVersion:    cityRealtimeCharacterCaseReportSchemaVersion,
		AgentBindingHash: agentState.Binding.BindingHash,
	}
	binding.BindingHash = cityRealtimeCharacterCaseReportBindingHash(binding.AgentBindingHash)
	if !cityRealtimeCharacterCaseReportBindingValid(binding) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_binding"})
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_character_case_report_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("activate realtime character case-report initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_report_world_bindings
    (world_id, schema_version, agent_binding_hash, binding_hash, metadata)
VALUES ($1, $2, $3, $4, '{}'::jsonb)`,
		worldID, binding.SchemaVersion, binding.AgentBindingHash, binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime character case-report binding: %w", err)
	}
	return nil
}

func enableCityRealtimeCharacterCaseReportMutationGate(
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
		{name: "sub2api.city_realtime_character_case_report_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_case_report_frame_sequence", value: frameSequence},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character case-report gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func cityRealtimeCharacterCaseReportBindingValid(binding cityRealtimeCharacterCaseReportBinding) bool {
	return binding.SchemaVersion == cityRealtimeCharacterCaseReportSchemaVersion &&
		cityRealtimeSHA256Hex(binding.AgentBindingHash) && cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeCharacterCaseReportBindingHash(binding.AgentBindingHash)
}

func loadCityRealtimeCharacterCaseReportBinding(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseReportBinding, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterCaseReportBinding{}
	var policyID, policyVersion, agentBindingHash string
	err := queryer.QueryRowContext(ctx, `
SELECT report.schema_version, report.agent_binding_hash, report.binding_hash,
       agent.policy_id, agent.policy_version, agent.binding_hash
FROM city_realtime_character_case_report_world_bindings report
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = report.world_id
WHERE report.world_id = $1`, worldID).Scan(
		&binding.SchemaVersion, &binding.AgentBindingHash, &binding.BindingHash,
		&policyID, &policyVersion, &agentBindingHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-report binding: %w", err)
	}
	if policyID != cityRealtimeAgentCorePolicyID ||
		(policyVersion != cityRealtimeAgentCorePolicyVersionReport &&
			policyVersion != cityRealtimeAgentCorePolicyVersionIntake &&
			policyVersion != cityRealtimeAgentCorePolicyVersionEvidence &&
			policyVersion != cityRealtimeAgentCorePolicyVersionEvidenceAssignment &&
			policyVersion != cityRealtimeAgentCorePolicyVersionProcedureDispatch &&
			policyVersion != cityRealtimeAgentCorePolicyVersionTask &&
			policyVersion != cityRealtimeAgentCorePolicyVersionNavigationPlan) ||
		binding.AgentBindingHash != agentBindingHash || !cityRealtimeCharacterCaseReportBindingValid(binding) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_binding"})
	}
	return &binding, nil
}

func loadCityRealtimeCharacterCaseReportHead(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	reporterActorCode, subjectActorCode string,
	forUpdate bool,
) (cityRealtimeCharacterCaseReportHead, bool, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(reporterActorCode) ||
		!cityRealtimeAgentIdentifierValid(subjectActorCode, 96) || reporterActorCode == subjectActorCode {
		return cityRealtimeCharacterCaseReportHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT reporter_actor_code, subject_actor_code, report_revision, report_status,
       source_intent_code, filed_frame_sequence, last_frame_sequence,
       event_chain_hash, state_hash
FROM city_realtime_character_case_report_heads
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head := cityRealtimeCharacterCaseReportHead{}
	err := queryer.QueryRowContext(ctx, query, worldID, reporterActorCode, subjectActorCode).Scan(
		&head.ReporterActorCode, &head.SubjectActorCode, &head.ReportRevision, &head.ReportStatus,
		&head.SourceIntentCode, &head.FiledFrameSequence, &head.LastFrameSequence,
		&head.EventChainHash, &head.StateHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterCaseReportHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterCaseReportHead{}, false, fmt.Errorf("load realtime character case-report head: %w", err)
	}
	if !cityRealtimeCharacterCaseReportHeadValid(head) {
		return cityRealtimeCharacterCaseReportHead{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_head"})
	}
	return head, true, nil
}

func validateCityRealtimeCharacterCaseReportHeadHistory(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterCaseReportHead,
) error {
	if worldID <= 0 || !cityRealtimeCharacterCaseReportHeadValid(head) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_history"})
	}
	genesisHash, err := cityRealtimeCharacterCaseReportChainGenesisHash(head.ReporterActorCode, head.SubjectActorCode)
	if err != nil {
		return err
	}
	var event cityRealtimeCharacterCaseReportEvent
	err = queryer.QueryRowContext(ctx, `
SELECT reporter_actor_code, subject_actor_code, event_sequence, frame_sequence,
       event_type, source_intent_code, previous_event_hash, event_hash
FROM city_realtime_character_case_report_events
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3
ORDER BY event_sequence ASC
LIMIT 1`, worldID, head.ReporterActorCode, head.SubjectActorCode).Scan(
		&event.ReporterActorCode, &event.SubjectActorCode, &event.EventSequence, &event.FrameSequence,
		&event.EventType, &event.SourceIntentCode, &event.PreviousEventHash, &event.EventHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_event_head"})
	}
	if err != nil {
		return fmt.Errorf("load realtime character case-report event head: %w", err)
	}
	expectedEventHash, hashErr := cityRealtimeCharacterCaseReportEventHash(event)
	if !cityRealtimeCharacterCaseReportEventValid(event) || hashErr != nil || expectedEventHash != event.EventHash ||
		event.PreviousEventHash != genesisHash || event.EventHash != head.EventChainHash ||
		event.FrameSequence != head.FiledFrameSequence || event.SourceIntentCode != head.SourceIntentCode {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_event_head"})
	}
	return nil
}

func loadCityRealtimeCharacterCaseReportHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterCaseReportHashState, error) {
	binding, err := loadCityRealtimeCharacterCaseReportBinding(ctx, queryer, worldID)
	if err != nil || binding == nil {
		return nil, err
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT reporter_actor_code, subject_actor_code, report_revision, report_status,
       source_intent_code, filed_frame_sequence, last_frame_sequence,
       event_chain_hash, state_hash
FROM city_realtime_character_case_report_heads
WHERE world_id = $1
ORDER BY reporter_actor_code ASC, subject_actor_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime character case-report heads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	heads := make([]cityRealtimeCharacterCaseReportHead, 0)
	for rows.Next() {
		head := cityRealtimeCharacterCaseReportHead{}
		if err = rows.Scan(
			&head.ReporterActorCode, &head.SubjectActorCode, &head.ReportRevision, &head.ReportStatus,
			&head.SourceIntentCode, &head.FiledFrameSequence, &head.LastFrameSequence,
			&head.EventChainHash, &head.StateHash,
		); err != nil {
			return nil, err
		}
		heads = append(heads, head)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character case-report heads: %w", err)
	}
	state := &cityRealtimeCharacterCaseReportHashState{
		SchemaVersion: cityRealtimeCharacterCaseReportSchemaVersion,
		Binding:       binding,
		Heads:         heads,
	}
	for _, head := range heads {
		if err = validateCityRealtimeCharacterCaseReportHeadHistory(ctx, queryer, worldID, head); err != nil {
			return nil, err
		}
	}
	if err = validateCityRealtimeCharacterCaseReportHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeCharacterCaseReportHashState(state *cityRealtimeCharacterCaseReportHashState) error {
	if state == nil || state.SchemaVersion != cityRealtimeCharacterCaseReportSchemaVersion ||
		state.Binding == nil || state.Heads == nil || !cityRealtimeCharacterCaseReportBindingValid(*state.Binding) {
		return errors.New("invalid realtime character case-report hash state")
	}
	for index, head := range state.Heads {
		if !cityRealtimeCharacterCaseReportHeadValid(head) || (index > 0 &&
			(state.Heads[index-1].ReporterActorCode > head.ReporterActorCode ||
				(state.Heads[index-1].ReporterActorCode == head.ReporterActorCode &&
					state.Heads[index-1].SubjectActorCode >= head.SubjectActorCode))) {
			return errors.New("invalid or unordered realtime character case-report heads")
		}
	}
	return nil
}

func cityRealtimeAgentDecisionCaseReportTargetCodeFromArguments(arguments map[string]any) (string, error) {
	return cityRealtimeAgentDecisionSocialTargetCodeFromArguments(arguments)
}

func cityRealtimeAgentDecisionCaseReportTargetCodeFromRawArguments(arguments []byte) (string, error) {
	decoded := make(map[string]any)
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_arguments"}).WithCause(err)
	}
	return cityRealtimeAgentDecisionCaseReportTargetCodeFromArguments(decoded)
}

// cityRealtimeAgentDecisionAvailableCaseReportTargetCodes deliberately reuses
// the same sealed public-adjacency catalogue as greeting. The additional
// report binding is checked here and again in the reducer so an Agent cannot
// target a hidden, stale, remote, or unsupported Actor.
func cityRealtimeAgentDecisionAvailableCaseReportTargetCodes(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	current cityRealtimeActorState,
	binding cityRealtimeAgentPolicyBinding,
) ([]string, error) {
	if !cityRealtimeAgentCharacterCaseReportRuntimeEnabled(binding) {
		return nil, ErrCityInvalidInput
	}
	reportBinding, err := loadCityRealtimeCharacterCaseReportBinding(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	if reportBinding == nil || reportBinding.AgentBindingHash != binding.BindingHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_scope"})
	}
	return cityRealtimeAgentDecisionAvailableSocialTargetCodes(ctx, queryer, worldID, actorCode, current, binding)
}

func insertCityRealtimeCharacterCaseReportHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	head cityRealtimeCharacterCaseReportHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseReportHeadValid(head) {
		return ErrCityInvalidInput
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_report_heads
    (world_id, reporter_actor_code, subject_actor_code, report_revision,
     report_status, source_intent_code, filed_frame_sequence,
     last_frame_sequence, event_chain_hash, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '{}'::jsonb)`,
		worldID, head.ReporterActorCode, head.SubjectActorCode, head.ReportRevision,
		head.ReportStatus, head.SourceIntentCode, head.FiledFrameSequence,
		head.LastFrameSequence, head.EventChainHash, head.StateHash,
	)
	if err != nil {
		return fmt.Errorf("insert realtime character case-report head: %w", err)
	}
	return nil
}

func insertCityRealtimeCharacterCaseReportEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	event cityRealtimeCharacterCaseReportEvent,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterCaseReportEventValid(event) ||
		!cityRealtimeSHA256Hex(event.EventHash) {
		return ErrCityInvalidInput
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_case_report_events
    (world_id, reporter_actor_code, subject_actor_code, event_sequence,
     frame_sequence, event_type, source_intent_code, previous_event_hash,
     event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '{}'::jsonb)`,
		worldID, event.ReporterActorCode, event.SubjectActorCode, event.EventSequence,
		event.FrameSequence, event.EventType, event.SourceIntentCode,
		event.PreviousEventHash, event.EventHash,
	)
	if err != nil {
		return fmt.Errorf("append realtime character case-report event: %w", err)
	}
	return nil
}
