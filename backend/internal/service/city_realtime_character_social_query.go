package service

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// CityRealtimeCharacterSocialRelation is the member-safe view of one
// relation involving the caller's own character. It deliberately exposes no
// owner id, agent code, private personality, message, model output, or
// provider data.
type CityRealtimeCharacterSocialRelation struct {
	ActorCode         string `json:"actor_code"`
	ActorKind         string `json:"actor_kind"`
	PublicLabel       string `json:"public_label"`
	LifecycleStatus   string `json:"lifecycle_status"`
	RelationRevision  int64  `json:"relation_revision"`
	LastFrameSequence int64  `json:"last_frame_sequence"`
	AffinityMilli     int64  `json:"affinity_milli"`
	InteractionCount  int64  `json:"interaction_count"`
}

type CityRealtimeCharacterSocialRelationPage struct {
	Items      []CityRealtimeCharacterSocialRelation `json:"items"`
	NextCursor *string                               `json:"next_cursor,omitempty"`
}

type CityRealtimeCharacterSocialRelationListInput struct {
	UserID       int64
	WorldID      int64
	BeforeCursor string
	Limit        int
}

type cityRealtimeCharacterSocialRelationCursor struct {
	FrameSequence int64
	ActorCodeLow  string
	ActorCodeHigh string
}

func (cursor cityRealtimeCharacterSocialRelationCursor) String() string {
	return strings.Join([]string{
		strconv.FormatInt(cursor.FrameSequence, 10),
		cursor.ActorCodeLow,
		cursor.ActorCodeHigh,
	}, "|")
}

func parseCityRealtimeCharacterSocialRelationCursor(value string) (cityRealtimeCharacterSocialRelationCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return cityRealtimeCharacterSocialRelationCursor{}, nil
	}
	parts := strings.Split(value, "|")
	if len(parts) != 3 {
		return cityRealtimeCharacterSocialRelationCursor{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "before_cursor"})
	}
	frameSequence, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || frameSequence <= 0 ||
		!cityRealtimeAgentIdentifierValid(parts[1], 96) ||
		!cityRealtimeAgentIdentifierValid(parts[2], 96) || parts[1] >= parts[2] {
		return cityRealtimeCharacterSocialRelationCursor{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "before_cursor"})
	}
	return cityRealtimeCharacterSocialRelationCursor{
		FrameSequence: frameSequence,
		ActorCodeLow:  parts[1],
		ActorCodeHigh: parts[2],
	}, nil
}

func validateCityRealtimeCharacterSocialRelationTarget(
	actorCode, actorKind, publicLabel, lifecycleStatus string,
) error {
	if !cityRealtimeAgentIdentifierValid(actorCode, 96) ||
		!cityRealtimeActorPublicLabelValid(publicLabel) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_target_projection"})
	}
	if actorKind != "npc" && actorKind != "character" {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_target_kind"})
	}
	switch lifecycleStatus {
	case "active", "inactive", "retired":
		return nil
	default:
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_target_lifecycle"})
	}
}

// ListRealtimeMyCharacterSocialRelations returns only relation heads that
// include the requesting user's own character. The target is resolved from
// the public actor identity table; no account or Agent ownership join is
// exposed to the caller.
func (s *CityEconomyService) ListRealtimeMyCharacterSocialRelations(
	ctx context.Context,
	input CityRealtimeCharacterSocialRelationListInput,
) (*CityRealtimeCharacterSocialRelationPage, error) {
	limit, err := normalizeCityRealtimeCharacterActivityEventLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	cursor, err := parseCityRealtimeCharacterSocialRelationCursor(input.BeforeCursor)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin realtime character social relation projection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireCityRealtimeWorldRead(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	page := &CityRealtimeCharacterSocialRelationPage{
		Items: make([]CityRealtimeCharacterSocialRelation, 0, limit),
	}
	socialBinding, err := loadCityRealtimeCharacterSocialBinding(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	// Historical 1.0–1.4 worlds are valid but do not have a social adapter.
	// Return an empty projection instead of pretending that a relation table
	// exists for a policy that never published one.
	if socialBinding == nil {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit historical realtime social relation projection: %w", err)
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
	actorCode := record.identity.ActorCode
	rows, err := tx.QueryContext(ctx, `
SELECT head.actor_code_low, head.actor_code_high, head.relation_revision,
       head.last_frame_sequence, head.affinity_milli, head.interaction_count,
       head.event_chain_hash, head.state_hash,
       identity.actor_code, identity.actor_kind, identity.public_label,
       identity.lifecycle_status
FROM city_realtime_character_social_heads head
JOIN city_realtime_actor_identities identity
  ON identity.world_id = head.world_id
 AND identity.actor_code = CASE
     WHEN head.actor_code_low = $2 THEN head.actor_code_high
     ELSE head.actor_code_low
   END
WHERE head.world_id = $1
  AND (head.actor_code_low = $2 OR head.actor_code_high = $2)
  AND (
      $3 = 0
      OR head.last_frame_sequence < $3
      OR (head.last_frame_sequence = $3 AND head.actor_code_low < $4)
      OR (head.last_frame_sequence = $3 AND head.actor_code_low = $4 AND head.actor_code_high < $5)
  )
ORDER BY head.last_frame_sequence DESC, head.actor_code_low DESC, head.actor_code_high DESC
LIMIT $6`, input.WorldID, actorCode, cursor.FrameSequence, cursor.ActorCodeLow, cursor.ActorCodeHigh, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list realtime character social relations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	heads := make([]cityRealtimeCharacterSocialHead, 0, limit+1)
	for rows.Next() {
		head := cityRealtimeCharacterSocialHead{}
		item := CityRealtimeCharacterSocialRelation{}
		if err = rows.Scan(
			&head.ActorCodeLow, &head.ActorCodeHigh, &head.RelationRevision,
			&head.LastFrameSequence, &head.AffinityMilli, &head.InteractionCount,
			&head.EventChainHash, &head.StateHash,
			&item.ActorCode, &item.ActorKind, &item.PublicLabel, &item.LifecycleStatus,
		); err != nil {
			return nil, fmt.Errorf("scan realtime character social relation: %w", err)
		}
		if head.ActorCodeLow == actorCode {
			if item.ActorCode != head.ActorCodeHigh {
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_relation_target"})
			}
		} else if head.ActorCodeHigh == actorCode {
			if item.ActorCode != head.ActorCodeLow {
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_relation_target"})
			}
		} else {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_relation_scope"})
		}
		if err = validateCityRealtimeCharacterSocialRelationTarget(
			item.ActorCode, item.ActorKind, item.PublicLabel, item.LifecycleStatus,
		); err != nil {
			return nil, err
		}
		if !cityRealtimeCharacterSocialHeadValid(head) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_social_relation_head"})
		}
		item.RelationRevision = head.RelationRevision
		item.LastFrameSequence = head.LastFrameSequence
		item.AffinityMilli = head.AffinityMilli
		item.InteractionCount = head.InteractionCount
		heads = append(heads, head)
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character social relations: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character social relations: %w", err)
	}
	for _, head := range heads {
		if err = validateCityRealtimeCharacterSocialHeadHistory(ctx, tx, input.WorldID, head); err != nil {
			return nil, err
		}
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		var lowCode, highCode string
		if actorCode < last.ActorCode {
			lowCode, highCode = actorCode, last.ActorCode
		} else {
			lowCode, highCode = last.ActorCode, actorCode
		}
		next := (cityRealtimeCharacterSocialRelationCursor{
			FrameSequence: last.LastFrameSequence,
			ActorCodeLow:  lowCode,
			ActorCodeHigh: highCode,
		}).String()
		page.NextCursor = &next
		page.Items = page.Items[:limit]
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character social relation projection: %w", err)
	}
	return page, nil
}
