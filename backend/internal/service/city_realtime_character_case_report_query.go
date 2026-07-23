package service

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// CityRealtimeCharacterCaseReport is the owner-safe projection of one
// non-evidentiary intake receipt. It intentionally omits the reporting Agent,
// intent code, event/state hashes, owner identifiers, personality, provider
// data, text, evidence claim, adjudication, and all economic data.
type CityRealtimeCharacterCaseReport struct {
	SubjectActorCode   string `json:"subject_actor_code"`
	SubjectActorKind   string `json:"subject_actor_kind"`
	SubjectPublicLabel string `json:"subject_public_label"`
	SubjectLifecycle   string `json:"subject_lifecycle_status"`
	ReportStatus       string `json:"report_status"`
	FiledFrameSequence int64  `json:"filed_frame_sequence"`
}

type CityRealtimeCharacterCaseReportPage struct {
	Items      []CityRealtimeCharacterCaseReport `json:"items"`
	NextCursor *string                           `json:"next_cursor,omitempty"`
}

type CityRealtimeCharacterCaseReportListInput struct {
	UserID       int64
	WorldID      int64
	BeforeCursor string
	Limit        int
}

type cityRealtimeCharacterCaseReportCursor struct {
	FrameSequence    int64
	SubjectActorCode string
}

func (cursor cityRealtimeCharacterCaseReportCursor) String() string {
	return strings.Join([]string{
		strconv.FormatInt(cursor.FrameSequence, 10),
		cursor.SubjectActorCode,
	}, "|")
}

func parseCityRealtimeCharacterCaseReportCursor(value string) (cityRealtimeCharacterCaseReportCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return cityRealtimeCharacterCaseReportCursor{}, nil
	}
	parts := strings.Split(value, "|")
	if len(parts) != 2 {
		return cityRealtimeCharacterCaseReportCursor{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "before_cursor"})
	}
	frameSequence, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || frameSequence <= 0 || !cityRealtimeAgentIdentifierValid(parts[1], 96) {
		return cityRealtimeCharacterCaseReportCursor{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "before_cursor"})
	}
	return cityRealtimeCharacterCaseReportCursor{
		FrameSequence:    frameSequence,
		SubjectActorCode: parts[1],
	}, nil
}

// ListRealtimeMyCharacterCaseReports returns only reports filed by the
// caller's own Character. A report remains a neutral receipt in this view: it
// never implies that the subject broke a rule or that any Case exists.
func (s *CityEconomyService) ListRealtimeMyCharacterCaseReports(
	ctx context.Context,
	input CityRealtimeCharacterCaseReportListInput,
) (*CityRealtimeCharacterCaseReportPage, error) {
	limit, err := normalizeCityRealtimeCharacterActivityEventLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	cursor, err := parseCityRealtimeCharacterCaseReportCursor(input.BeforeCursor)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin realtime character case-report projection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireCityRealtimeWorldRead(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	page := &CityRealtimeCharacterCaseReportPage{
		Items: make([]CityRealtimeCharacterCaseReport, 0, limit),
	}
	reportBinding, err := loadCityRealtimeCharacterCaseReportBinding(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	// Historical worlds keep their original canonical shape and simply have no
	// case-report adapter or owner projection.
	if reportBinding == nil {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit historical realtime case-report projection: %w", err)
		}
		return page, nil
	}
	record, found, err := loadCityRealtimeOwnedCharacter(ctx, tx, input.WorldID, input.UserID, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterNotFound
	}
	reporterActorCode := record.identity.ActorCode
	rows, err := tx.QueryContext(ctx, `
SELECT head.reporter_actor_code, head.subject_actor_code, head.report_revision,
       head.report_status, head.source_intent_code, head.filed_frame_sequence,
       head.last_frame_sequence, head.event_chain_hash, head.state_hash,
       identity.actor_code, identity.actor_kind, identity.public_label,
       identity.lifecycle_status
FROM city_realtime_character_case_report_heads head
JOIN city_realtime_actor_identities identity
  ON identity.world_id = head.world_id
 AND identity.actor_code = head.subject_actor_code
WHERE head.world_id = $1
  AND head.reporter_actor_code = $2
  AND (
      $3 = 0
      OR head.last_frame_sequence < $3
      OR (head.last_frame_sequence = $3 AND head.subject_actor_code < $4)
  )
ORDER BY head.last_frame_sequence DESC, head.subject_actor_code DESC
LIMIT $5`, input.WorldID, reporterActorCode, cursor.FrameSequence, cursor.SubjectActorCode, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list realtime character case reports: %w", err)
	}
	defer func() { _ = rows.Close() }()
	heads := make([]cityRealtimeCharacterCaseReportHead, 0, limit+1)
	for rows.Next() {
		head := cityRealtimeCharacterCaseReportHead{}
		item := CityRealtimeCharacterCaseReport{}
		if err = rows.Scan(
			&head.ReporterActorCode, &head.SubjectActorCode, &head.ReportRevision,
			&head.ReportStatus, &head.SourceIntentCode, &head.FiledFrameSequence,
			&head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
			&item.SubjectActorCode, &item.SubjectActorKind, &item.SubjectPublicLabel,
			&item.SubjectLifecycle,
		); err != nil {
			return nil, fmt.Errorf("scan realtime character case report: %w", err)
		}
		if head.ReporterActorCode != reporterActorCode || item.SubjectActorCode != head.SubjectActorCode {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_scope"})
		}
		if err = validateCityRealtimeCharacterSocialRelationTarget(
			item.SubjectActorCode, item.SubjectActorKind, item.SubjectPublicLabel, item.SubjectLifecycle,
		); err != nil {
			return nil, err
		}
		if !cityRealtimeCharacterCaseReportHeadValid(head) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_report_head"})
		}
		item.ReportStatus = head.ReportStatus
		item.FiledFrameSequence = head.FiledFrameSequence
		heads = append(heads, head)
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character case reports: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character case reports: %w", err)
	}
	for _, head := range heads {
		if err = validateCityRealtimeCharacterCaseReportHeadHistory(ctx, tx, input.WorldID, head); err != nil {
			return nil, err
		}
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		next := (cityRealtimeCharacterCaseReportCursor{
			FrameSequence:    last.FiledFrameSequence,
			SubjectActorCode: last.SubjectActorCode,
		}).String()
		page.NextCursor = &next
		page.Items = page.Items[:limit]
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character case-report projection: %w", err)
	}
	return page, nil
}
