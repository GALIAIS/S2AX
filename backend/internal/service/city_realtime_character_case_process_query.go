package service

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

const cityRealtimeCharacterCaseProcessIndependentRecordNotMatched = "not_matched"

const cityRealtimeCharacterCaseProcessProcedureDispatchNotRouted = "not_routed"

// CityRealtimeCharacterCaseProcess is the owner-safe view of a procedural
// receipt. It intentionally shows only the public subject already selected by
// the caller and coarse lifecycle states. It never reveals a Law rule, case,
// disposition, penalty, evidence code/hash, source timestamp, other owner,
// Agent, model, prompt, report text, account, wallet, reward, or adjudication.
type CityRealtimeCharacterCaseProcess struct {
	SubjectActorCode        string `json:"subject_actor_code"`
	SubjectActorKind        string `json:"subject_actor_kind"`
	SubjectPublicLabel      string `json:"subject_public_label"`
	SubjectLifecycle        string `json:"subject_lifecycle_status"`
	ReportStatus            string `json:"report_status"`
	IntakeStatus            string `json:"intake_status"`
	IndependentRecordStatus string `json:"independent_record_status"`
	ProcedureDispatchStatus string `json:"procedure_dispatch_status"`
	FiledFrameSequence      int64  `json:"filed_frame_sequence"`
	LastFrameSequence       int64  `json:"last_frame_sequence"`
}

type CityRealtimeCharacterCaseProcessPage struct {
	Items      []CityRealtimeCharacterCaseProcess `json:"items"`
	NextCursor *string                            `json:"next_cursor,omitempty"`
}

type CityRealtimeCharacterCaseProcessListInput struct {
	UserID       int64
	WorldID      int64
	BeforeCursor string
	Limit        int
}

type cityRealtimeCharacterCaseProcessCursor struct {
	LastFrameSequence int64
	SubjectActorCode  string
}

func (cursor cityRealtimeCharacterCaseProcessCursor) String() string {
	return strings.Join([]string{strconv.FormatInt(cursor.LastFrameSequence, 10), cursor.SubjectActorCode}, "|")
}

func parseCityRealtimeCharacterCaseProcessCursor(value string) (cityRealtimeCharacterCaseProcessCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return cityRealtimeCharacterCaseProcessCursor{}, nil
	}
	parts := strings.Split(value, "|")
	if len(parts) != 2 {
		return cityRealtimeCharacterCaseProcessCursor{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "before_cursor"})
	}
	frameSequence, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || frameSequence <= 0 || !cityRealtimePlayerActorCodeValid(parts[1]) {
		return cityRealtimeCharacterCaseProcessCursor{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "before_cursor"})
	}
	return cityRealtimeCharacterCaseProcessCursor{LastFrameSequence: frameSequence, SubjectActorCode: parts[1]}, nil
}

func validateCityRealtimeCharacterCaseProcessProjection(item CityRealtimeCharacterCaseProcess) error {
	if !cityRealtimePlayerActorCodeValid(item.SubjectActorCode) || item.SubjectActorKind == "" ||
		strings.TrimSpace(item.SubjectPublicLabel) == "" || item.SubjectLifecycle == "" ||
		item.ReportStatus != cityRealtimeCharacterCaseReportFiledUnverified ||
		item.FiledFrameSequence <= 0 || item.LastFrameSequence < item.FiledFrameSequence {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_process_projection"})
	}
	if item.IntakeStatus != cityRealtimeCharacterCaseIntakeEvidenceRequired &&
		item.IntakeStatus != cityRealtimeCharacterCaseIntakeExpiredNoEvidence {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_process_projection"})
	}
	if item.IndependentRecordStatus != cityRealtimeCharacterCaseProcessIndependentRecordNotMatched &&
		item.IndependentRecordStatus != cityRealtimeCharacterCaseEvidenceAssignmentLinked &&
		item.IndependentRecordStatus != cityRealtimeCharacterCaseEvidenceAssignmentClosed {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_process_projection"})
	}
	if item.ProcedureDispatchStatus != cityRealtimeCharacterCaseProcessProcedureDispatchNotRouted &&
		item.ProcedureDispatchStatus != cityRealtimeCharacterCaseProcedureDispatchQueued &&
		item.ProcedureDispatchStatus != cityRealtimeCharacterCaseProcedureDispatchSourceWindowClosed {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_process_projection"})
	}
	return nil
}

// ListRealtimeMyCharacterCaseProcess exposes only the caller's own bounded
// procedural records in a 1.10 world. An independent-record status is never
// a finding: it only describes whether the server saw one unique active
// sealed-Law handle at filing time and whether that handle's short window has
// since closed. Historical worlds intentionally receive an empty projection.
func (s *CityEconomyService) ListRealtimeMyCharacterCaseProcess(
	ctx context.Context,
	input CityRealtimeCharacterCaseProcessListInput,
) (*CityRealtimeCharacterCaseProcessPage, error) {
	limit, err := normalizeCityRealtimeCharacterActivityEventLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	cursor, err := parseCityRealtimeCharacterCaseProcessCursor(input.BeforeCursor)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin realtime character case-process projection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireCityRealtimeWorldRead(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	page := &CityRealtimeCharacterCaseProcessPage{Items: make([]CityRealtimeCharacterCaseProcess, 0, limit)}
	binding, err := loadCityRealtimeCharacterCaseEvidenceAssignmentBinding(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit historical realtime character case-process projection: %w", err)
		}
		return page, nil
	}
	procedureDispatchBinding, err := loadCityRealtimeCharacterCaseProcedureDispatchBinding(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	owned, found, err := loadCityRealtimeOwnedCharacter(ctx, tx, input.WorldID, input.UserID, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterNotFound
	}
	rows, err := tx.QueryContext(ctx, `
SELECT report.reporter_actor_code, report.subject_actor_code, report.report_revision,
       report.report_status, report.source_intent_code, report.filed_frame_sequence,
       report.last_frame_sequence, report.event_chain_hash, report.state_hash,
       intake.report_event_sequence, intake.report_event_hash, intake.intake_revision,
       intake.intake_status, intake.source_intent_code, intake.opened_frame_sequence,
       intake.expiration_due_world_time_us, intake.last_frame_sequence,
       intake.event_chain_hash, intake.state_hash,
       assignment.reporter_actor_code, assignment.subject_actor_code, assignment.report_event_sequence,
       assignment.report_event_hash, assignment.evidence_code, assignment.source_law_event_sequence,
       assignment.source_law_event_hash, assignment.source_frame_sequence, assignment.assignment_revision,
       assignment.assignment_status, assignment.assigned_frame_sequence, assignment.last_frame_sequence,
       assignment.event_chain_hash, assignment.state_hash,
	       dispatch.reporter_actor_code, dispatch.subject_actor_code, dispatch.assignment_event_sequence,
	       dispatch.assignment_link_event_hash, dispatch.dispatch_revision, dispatch.dispatch_status,
	       dispatch.queued_frame_sequence, dispatch.last_frame_sequence,
	       dispatch.event_chain_hash, dispatch.state_hash,
       identity.actor_code, identity.actor_kind, identity.public_label, identity.lifecycle_status,
       GREATEST(report.last_frame_sequence, intake.last_frame_sequence, COALESCE(assignment.last_frame_sequence, 0), COALESCE(dispatch.last_frame_sequence, 0)) AS process_last_frame
FROM city_realtime_character_case_report_heads report
JOIN city_realtime_character_case_intake_heads intake
  ON intake.world_id = report.world_id
 AND intake.reporter_actor_code = report.reporter_actor_code
 AND intake.subject_actor_code = report.subject_actor_code
LEFT JOIN city_realtime_character_case_evidence_assignment_heads assignment
  ON assignment.world_id = report.world_id
 AND assignment.reporter_actor_code = report.reporter_actor_code
 AND assignment.subject_actor_code = report.subject_actor_code
LEFT JOIN city_realtime_character_case_procedure_dispatch_heads dispatch
  ON dispatch.world_id = report.world_id
 AND dispatch.reporter_actor_code = report.reporter_actor_code
 AND dispatch.subject_actor_code = report.subject_actor_code
JOIN city_realtime_actor_identities identity
  ON identity.world_id = report.world_id AND identity.actor_code = report.subject_actor_code
WHERE report.world_id = $1
  AND report.reporter_actor_code = $2
  AND (
      $3 = 0
      OR GREATEST(report.last_frame_sequence, intake.last_frame_sequence, COALESCE(assignment.last_frame_sequence, 0), COALESCE(dispatch.last_frame_sequence, 0)) < $3
      OR (GREATEST(report.last_frame_sequence, intake.last_frame_sequence, COALESCE(assignment.last_frame_sequence, 0), COALESCE(dispatch.last_frame_sequence, 0)) = $3
          AND report.subject_actor_code < $4)
  )
ORDER BY process_last_frame DESC, report.subject_actor_code DESC
LIMIT $5`, input.WorldID, owned.identity.ActorCode, cursor.LastFrameSequence, cursor.SubjectActorCode, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list realtime character case-process records: %w", err)
	}
	type processRecord struct {
		report     cityRealtimeCharacterCaseReportHead
		intake     cityRealtimeCharacterCaseIntakeHead
		assignment *cityRealtimeCharacterCaseEvidenceAssignmentHead
		dispatch   *cityRealtimeCharacterCaseProcedureDispatchHead
	}
	records := make([]processRecord, 0, limit+1)
	for rows.Next() {
		item := CityRealtimeCharacterCaseProcess{}
		record := processRecord{}
		var assignmentReporter, assignmentSubject, assignmentReportHash sql.NullString
		var assignmentReportSequence, assignmentSourceSequence, assignmentSourceFrame sql.NullInt64
		var assignmentEvidenceCode, assignmentSourceHash, assignmentStatus sql.NullString
		var assignmentRevision, assignmentAssignedFrame, assignmentLastFrame sql.NullInt64
		var assignmentEventChainHash, assignmentStateHash sql.NullString
		var dispatchReporter, dispatchSubject, dispatchLinkHash, dispatchStatus sql.NullString
		var dispatchAssignmentSequence, dispatchRevision, dispatchQueuedFrame, dispatchLastFrame sql.NullInt64
		var dispatchEventChainHash, dispatchStateHash sql.NullString
		if err = rows.Scan(
			&record.report.ReporterActorCode, &record.report.SubjectActorCode, &record.report.ReportRevision,
			&record.report.ReportStatus, &record.report.SourceIntentCode, &record.report.FiledFrameSequence,
			&record.report.LastFrameSequence, &record.report.EventChainHash, &record.report.StateHash,
			&record.intake.ReportEventSequence, &record.intake.ReportEventHash, &record.intake.IntakeRevision,
			&record.intake.IntakeStatus, &record.intake.SourceIntentCode, &record.intake.OpenedFrameSequence,
			&record.intake.ExpirationDueWorldTimeUS, &record.intake.LastFrameSequence,
			&record.intake.EventChainHash, &record.intake.StateHash,
			&assignmentReporter, &assignmentSubject, &assignmentReportSequence,
			&assignmentReportHash, &assignmentEvidenceCode, &assignmentSourceSequence,
			&assignmentSourceHash, &assignmentSourceFrame, &assignmentRevision,
			&assignmentStatus, &assignmentAssignedFrame, &assignmentLastFrame,
			&assignmentEventChainHash, &assignmentStateHash,
			&dispatchReporter, &dispatchSubject, &dispatchAssignmentSequence,
			&dispatchLinkHash, &dispatchRevision, &dispatchStatus,
			&dispatchQueuedFrame, &dispatchLastFrame,
			&dispatchEventChainHash, &dispatchStateHash,
			&item.SubjectActorCode, &item.SubjectActorKind, &item.SubjectPublicLabel, &item.SubjectLifecycle,
			&item.LastFrameSequence,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan realtime character case-process record: %w", err)
		}
		record.intake.ReporterActorCode = record.report.ReporterActorCode
		record.intake.SubjectActorCode = record.report.SubjectActorCode
		if assignmentReporter.Valid {
			if !assignmentSubject.Valid || !assignmentReportSequence.Valid || !assignmentReportHash.Valid ||
				!assignmentEvidenceCode.Valid || !assignmentSourceSequence.Valid || !assignmentSourceHash.Valid ||
				!assignmentSourceFrame.Valid || !assignmentRevision.Valid || !assignmentStatus.Valid ||
				!assignmentAssignedFrame.Valid || !assignmentLastFrame.Valid || !assignmentEventChainHash.Valid || !assignmentStateHash.Valid {
				_ = rows.Close()
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_process_assignment_nullability"})
			}
			record.assignment = &cityRealtimeCharacterCaseEvidenceAssignmentHead{
				ReporterActorCode:      assignmentReporter.String,
				SubjectActorCode:       assignmentSubject.String,
				ReportEventSequence:    assignmentReportSequence.Int64,
				ReportEventHash:        assignmentReportHash.String,
				EvidenceCode:           assignmentEvidenceCode.String,
				SourceLawEventSequence: assignmentSourceSequence.Int64,
				SourceLawEventHash:     assignmentSourceHash.String,
				SourceFrameSequence:    assignmentSourceFrame.Int64,
				AssignmentRevision:     assignmentRevision.Int64,
				AssignmentStatus:       assignmentStatus.String,
				AssignedFrameSequence:  assignmentAssignedFrame.Int64,
				LastFrameSequence:      assignmentLastFrame.Int64,
				EventChainHash:         assignmentEventChainHash.String,
				StateHash:              assignmentStateHash.String,
			}
		}
		if dispatchReporter.Valid {
			if !dispatchSubject.Valid || !dispatchAssignmentSequence.Valid || !dispatchLinkHash.Valid ||
				!dispatchRevision.Valid || !dispatchStatus.Valid || !dispatchQueuedFrame.Valid ||
				!dispatchLastFrame.Valid || !dispatchEventChainHash.Valid || !dispatchStateHash.Valid {
				_ = rows.Close()
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_process_dispatch_nullability"})
			}
			record.dispatch = &cityRealtimeCharacterCaseProcedureDispatchHead{
				ReporterActorCode:       dispatchReporter.String,
				SubjectActorCode:        dispatchSubject.String,
				AssignmentEventSequence: dispatchAssignmentSequence.Int64,
				AssignmentLinkEventHash: dispatchLinkHash.String,
				DispatchRevision:        dispatchRevision.Int64,
				DispatchStatus:          dispatchStatus.String,
				QueuedFrameSequence:     dispatchQueuedFrame.Int64,
				LastFrameSequence:       dispatchLastFrame.Int64,
				EventChainHash:          dispatchEventChainHash.String,
				StateHash:               dispatchStateHash.String,
			}
		}
		if record.report.ReporterActorCode != owned.identity.ActorCode ||
			item.SubjectActorCode != record.report.SubjectActorCode ||
			!cityRealtimeCharacterCaseReportHeadValid(record.report) ||
			!cityRealtimeCharacterCaseIntakeHeadValid(record.intake) {
			_ = rows.Close()
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_process_scope"})
		}
		if err = validateCityRealtimeCharacterSocialRelationTarget(
			item.SubjectActorCode, item.SubjectActorKind, item.SubjectPublicLabel, item.SubjectLifecycle,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.ReportStatus = record.report.ReportStatus
		item.IntakeStatus = record.intake.IntakeStatus
		item.FiledFrameSequence = record.report.FiledFrameSequence
		item.IndependentRecordStatus = cityRealtimeCharacterCaseProcessIndependentRecordNotMatched
		item.ProcedureDispatchStatus = cityRealtimeCharacterCaseProcessProcedureDispatchNotRouted
		if record.assignment != nil {
			if !cityRealtimeCharacterCaseEvidenceAssignmentHeadValid(*record.assignment) {
				_ = rows.Close()
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_process_assignment"})
			}
			item.IndependentRecordStatus = record.assignment.AssignmentStatus
		}
		if procedureDispatchBinding == nil {
			if record.dispatch != nil {
				_ = rows.Close()
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_process_dispatch_policy"})
			}
		} else if record.assignment == nil {
			if record.dispatch != nil {
				_ = rows.Close()
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_process_dispatch_without_assignment"})
			}
		} else {
			if record.dispatch == nil || !cityRealtimeCharacterCaseProcedureDispatchHeadValid(*record.dispatch) ||
				record.dispatch.ReporterActorCode != record.assignment.ReporterActorCode ||
				record.dispatch.SubjectActorCode != record.assignment.SubjectActorCode ||
				record.dispatch.AssignmentLinkEventHash != record.assignment.EventChainHash && record.assignment.AssignmentRevision == 1 {
				_ = rows.Close()
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_process_dispatch"})
			}
			expectedStatus := cityRealtimeCharacterCaseProcedureDispatchQueued
			if record.assignment.AssignmentStatus == cityRealtimeCharacterCaseEvidenceAssignmentClosed {
				expectedStatus = cityRealtimeCharacterCaseProcedureDispatchSourceWindowClosed
			}
			if record.dispatch.DispatchStatus != expectedStatus {
				_ = rows.Close()
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_process_dispatch_status"})
			}
			item.ProcedureDispatchStatus = record.dispatch.DispatchStatus
		}
		if err = validateCityRealtimeCharacterCaseProcessProjection(item); err != nil {
			_ = rows.Close()
			return nil, err
		}
		records = append(records, record)
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character case-process records: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character case-process records: %w", err)
	}
	for _, record := range records {
		if err = validateCityRealtimeCharacterCaseReportHeadHistory(ctx, tx, input.WorldID, record.report); err != nil {
			return nil, err
		}
		if err = validateCityRealtimeCharacterCaseIntakeHeadHistory(ctx, tx, input.WorldID, record.intake); err != nil {
			return nil, err
		}
		if record.assignment != nil {
			if err = validateCityRealtimeCharacterCaseEvidenceAssignmentHeadHistory(ctx, tx, input.WorldID, *record.assignment); err != nil {
				return nil, err
			}
		}
		if record.dispatch != nil {
			if err = validateCityRealtimeCharacterCaseProcedureDispatchHeadHistory(ctx, tx, input.WorldID, *record.dispatch); err != nil {
				return nil, err
			}
		}
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		next := (cityRealtimeCharacterCaseProcessCursor{
			LastFrameSequence: last.LastFrameSequence,
			SubjectActorCode:  last.SubjectActorCode,
		}).String()
		page.NextCursor = &next
		page.Items = page.Items[:limit]
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character case-process projection: %w", err)
	}
	return page, nil
}
