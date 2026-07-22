package service

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// CityRealtimeCharacterCaseReview is the owner-only projection of one
// procedural Law-Case review. It deliberately omits Agent identifiers,
// personality, model/provider data, raw prompts and outputs, and any platform
// currency details. The legal fact is shown exactly as it was already applied;
// the review status never represents an alternate ruling.
type CityRealtimeCharacterCaseReview struct {
	CaseCode                 string `json:"case_code"`
	RuleCode                 string `json:"rule_code"`
	Disposition              string `json:"disposition"`
	PenaltyCityCreditUnits   int64  `json:"penalty_city_credit_units"`
	ReviewRevision           int64  `json:"review_revision"`
	ReviewStatus             string `json:"review_status"`
	FiledFrameSequence       int64  `json:"filed_frame_sequence"`
	ResolutionDueWorldTimeUS int64  `json:"resolution_due_world_time_us"`
	LastFrameSequence        int64  `json:"last_frame_sequence"`
}

type CityRealtimeCharacterCaseReviewPage struct {
	Items      []CityRealtimeCharacterCaseReview `json:"items"`
	NextCursor *string                           `json:"next_cursor,omitempty"`
}

type CityRealtimeCharacterCaseReviewListInput struct {
	UserID       int64
	WorldID      int64
	BeforeCursor string
	Limit        int
}

type cityRealtimeCharacterCaseReviewCursor struct {
	LastFrameSequence int64
	CaseCode          string
}

func (cursor cityRealtimeCharacterCaseReviewCursor) String() string {
	return strings.Join([]string{strconv.FormatInt(cursor.LastFrameSequence, 10), cursor.CaseCode}, "|")
}

func parseCityRealtimeCharacterCaseReviewCursor(value string) (cityRealtimeCharacterCaseReviewCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return cityRealtimeCharacterCaseReviewCursor{}, nil
	}
	parts := strings.Split(value, "|")
	if len(parts) != 2 {
		return cityRealtimeCharacterCaseReviewCursor{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "before_cursor"})
	}
	frameSequence, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || frameSequence <= 0 || !cityRealtimeCharacterLawCaseCodeValid(parts[1]) {
		return cityRealtimeCharacterCaseReviewCursor{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "before_cursor"})
	}
	return cityRealtimeCharacterCaseReviewCursor{LastFrameSequence: frameSequence, CaseCode: parts[1]}, nil
}

func validateCityRealtimeCharacterCaseReviewProjection(item CityRealtimeCharacterCaseReview) error {
	if !cityRealtimeCharacterLawCaseCodeValid(item.CaseCode) || !cityRealtimeAgentIdentifierValid(item.RuleCode, 64) ||
		item.PenaltyCityCreditUnits < 0 || item.LastFrameSequence <= 0 || item.ReviewRevision < 0 ||
		item.FiledFrameSequence < 0 || item.ResolutionDueWorldTimeUS < 0 ||
		item.ResolutionDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_projection"})
	}
	switch item.Disposition {
	case "warning", "fine", "service":
	default:
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_projection"})
	}
	switch item.ReviewRevision {
	case 0:
		if item.ReviewStatus != cityRealtimeCharacterCaseReviewNone || item.FiledFrameSequence != 0 || item.ResolutionDueWorldTimeUS != 0 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_projection"})
		}
	case 1:
		if item.ReviewStatus != cityRealtimeCharacterCaseReviewFiled || item.FiledFrameSequence <= 0 ||
			item.ResolutionDueWorldTimeUS <= 0 || item.LastFrameSequence != item.FiledFrameSequence {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_projection"})
		}
	case 2:
		if item.ReviewStatus != cityRealtimeCharacterCaseReviewClosedNoChange || item.FiledFrameSequence <= 0 ||
			item.ResolutionDueWorldTimeUS <= 0 || item.LastFrameSequence <= item.FiledFrameSequence {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_projection"})
		}
	default:
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_projection"})
	}
	return nil
}

// ListRealtimeMyCharacterCaseReviews returns only the caller's own bounded
// procedural-review state. Historical worlds without policy 1.6 retain their
// original canonical shape and therefore receive an empty list rather than a
// synthetic adapter projection.
func (s *CityEconomyService) ListRealtimeMyCharacterCaseReviews(
	ctx context.Context,
	input CityRealtimeCharacterCaseReviewListInput,
) (*CityRealtimeCharacterCaseReviewPage, error) {
	limit, err := normalizeCityRealtimeCharacterActivityEventLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	cursor, err := parseCityRealtimeCharacterCaseReviewCursor(input.BeforeCursor)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin realtime character case-review projection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireCityRealtimeWorldRead(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	page := &CityRealtimeCharacterCaseReviewPage{Items: make([]CityRealtimeCharacterCaseReview, 0, limit)}
	binding, err := loadCityRealtimeCharacterCaseReviewBinding(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit historical realtime character case-review projection: %w", err)
		}
		return page, nil
	}
	owned, found, err := loadCityRealtimeOwnedCharacter(ctx, tx, input.WorldID, input.UserID, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterNotFound
	}
	rows, err := tx.QueryContext(ctx, `
SELECT review.case_code, law.rule_code, law.disposition, law.penalty_city_credit_units,
       review.review_revision, review.review_status, review.filed_frame_sequence,
       review.resolution_due_world_time_us, review.last_frame_sequence,
       review.actor_code, review.law_event_sequence, review.law_event_hash,
       review.response_event_sequence, review.response_event_hash,
       review.source_intent_code,
       review.event_chain_hash, review.state_hash
FROM city_realtime_character_case_review_heads review
JOIN city_realtime_character_law_events law
  ON law.world_id = review.world_id
 AND law.actor_code = review.actor_code
 AND law.event_sequence = review.law_event_sequence
 AND law.case_code = review.case_code
 AND law.event_hash = review.law_event_hash
WHERE review.world_id = $1
  AND review.actor_code = $2
  AND (
      $3 = 0
      OR review.last_frame_sequence < $3
      OR (review.last_frame_sequence = $3 AND review.case_code < $4)
  )
ORDER BY review.last_frame_sequence DESC, review.case_code DESC
LIMIT $5`, input.WorldID, owned.identity.ActorCode, cursor.LastFrameSequence, cursor.CaseCode, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list realtime character case reviews: %w", err)
	}
	defer func() { _ = rows.Close() }()
	heads := make([]cityRealtimeCharacterCaseReviewHead, 0, limit+1)
	for rows.Next() {
		item := CityRealtimeCharacterCaseReview{}
		head := cityRealtimeCharacterCaseReviewHead{}
		if err = rows.Scan(
			&item.CaseCode, &item.RuleCode, &item.Disposition, &item.PenaltyCityCreditUnits,
			&item.ReviewRevision, &item.ReviewStatus, &item.FiledFrameSequence,
			&item.ResolutionDueWorldTimeUS, &item.LastFrameSequence,
			&head.ActorCode, &head.LawEventSequence, &head.LawEventHash,
			&head.ResponseEventSequence, &head.ResponseEventHash,
			&head.SourceIntentCode,
			&head.EventChainHash, &head.StateHash,
		); err != nil {
			return nil, fmt.Errorf("scan realtime character case review: %w", err)
		}
		head.CaseCode = item.CaseCode
		head.ReviewRevision = item.ReviewRevision
		head.ReviewStatus = item.ReviewStatus
		head.FiledFrameSequence = item.FiledFrameSequence
		head.ResolutionDueWorldTimeUS = item.ResolutionDueWorldTimeUS
		head.LastFrameSequence = item.LastFrameSequence
		if head.ActorCode != owned.identity.ActorCode || !cityRealtimeCharacterCaseReviewHeadValid(head) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_case_review_scope"})
		}
		if err = validateCityRealtimeCharacterCaseReviewProjection(item); err != nil {
			return nil, err
		}
		heads = append(heads, head)
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character case reviews: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character case reviews: %w", err)
	}
	for _, head := range heads {
		if err = validateCityRealtimeCharacterCaseReviewHeadHistory(ctx, tx, input.WorldID, head); err != nil {
			return nil, err
		}
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		next := (cityRealtimeCharacterCaseReviewCursor{
			LastFrameSequence: last.LastFrameSequence,
			CaseCode:          last.CaseCode,
		}).String()
		page.NextCursor = &next
		page.Items = page.Items[:limit]
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character case-review projection: %w", err)
	}
	return page, nil
}
