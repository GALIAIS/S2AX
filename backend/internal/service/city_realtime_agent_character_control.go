package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	cityRealtimeCharacterAgentConfigureAction          = "character.agent.configure"
	cityRealtimeCharacterPersonalitySchemaVersion      = 1
	cityRealtimeCharacterPersonalityValuesMaximum      = 8
	cityRealtimeCharacterPersonalityBoundariesMaximum  = 8
	cityRealtimeCharacterPersonalityPreferencesMaximum = 8
	cityRealtimeCharacterPersonalityValueMaximumRunes  = 48
	cityRealtimeCharacterPersonalityTextMaximumRunes   = 600
)

// CityRealtimeCharacterPersonalitySeed is deliberately data-shaped rather
// than a free-form execution prompt.  It remains private to the owning user
// and is not placed in the common actor projection, decision audit, or A2
// observation payload.  A4 will add encrypted provider-context assembly;
// this A3 seed is used only for versioning and deterministic local policy.
type CityRealtimeCharacterPersonalitySeed struct {
	Values         []string          `json:"values"`
	Preferences    map[string]string `json:"preferences"`
	Background     string            `json:"background"`
	HardBoundaries []string          `json:"hard_boundaries"`
	FreeformNotes  string            `json:"freeform_notes"`
}

// CityRealtimeCharacterPersonalityProjection is owner-private.  SeedHash is
// an audit handle, not a platform account identifier or provider payload.
type CityRealtimeCharacterPersonalityProjection struct {
	SchemaVersion int                                  `json:"schema_version"`
	Revision      int64                                `json:"revision"`
	SeedHash      string                               `json:"seed_hash"`
	Seed          CityRealtimeCharacterPersonalitySeed `json:"seed"`
}

// CityRealtimeCharacterAgentConfiguration is returned only beside the
// caller's own Character.  It intentionally excludes Agent code, worker
// leases, model/provider configuration, raw decision output, and any private
// data belonging to another Actor.
type CityRealtimeCharacterAgentConfiguration struct {
	ControlMode              string                                      `json:"control_mode"`
	Personality              *CityRealtimeCharacterPersonalityProjection `json:"personality,omitempty"`
	PendingDecision          bool                                        `json:"pending_decision"`
	PendingIntent            bool                                        `json:"pending_intent"`
	AutonomyRuntimeAvailable bool                                        `json:"autonomy_runtime_available"`
}

// CityRealtimeCharacterAgentConfigureInput is the owner-scoped command for
// a user Character Agent.  ControlMode may be omitted to revise only the
// personality seed.  Normal members may request only autonomous/suspended;
// manual and assisted remain administrative/test modes.
type CityRealtimeCharacterAgentConfigureInput struct {
	UserID         int64
	WorldID        int64
	ControlMode    string
	Personality    *CityRealtimeCharacterPersonalitySeed
	IdempotencyKey string
}

type cityRealtimeCharacterPersonalityRevision struct {
	AgentCode            string
	Revision             int64
	Seed                 CityRealtimeCharacterPersonalitySeed
	SeedHash             string
	PreviousSeedHash     *string
	ChangedByUserID      int64
	CreatedFrameSequence int64
}

func (revision cityRealtimeCharacterPersonalityRevision) projection() CityRealtimeCharacterPersonalityProjection {
	return CityRealtimeCharacterPersonalityProjection{
		SchemaVersion: cityRealtimeCharacterPersonalitySchemaVersion,
		Revision:      revision.Revision,
		SeedHash:      revision.SeedHash,
		Seed:          revision.Seed,
	}
}

func normalizeCityRealtimeCharacterPersonalitySeed(
	input CityRealtimeCharacterPersonalitySeed,
) (CityRealtimeCharacterPersonalitySeed, error) {
	values, err := normalizeCityRealtimeCharacterPersonalityList(
		input.Values,
		1,
		cityRealtimeCharacterPersonalityValuesMaximum,
		"values",
	)
	if err != nil {
		return CityRealtimeCharacterPersonalitySeed{}, err
	}
	boundaries, err := normalizeCityRealtimeCharacterPersonalityList(
		input.HardBoundaries,
		0,
		cityRealtimeCharacterPersonalityBoundariesMaximum,
		"hard_boundaries",
	)
	if err != nil {
		return CityRealtimeCharacterPersonalitySeed{}, err
	}
	preferences, err := normalizeCityRealtimeCharacterPersonalityPreferences(input.Preferences)
	if err != nil {
		return CityRealtimeCharacterPersonalitySeed{}, err
	}
	background, err := normalizeCityRealtimeCharacterPersonalityText(
		input.Background,
		0,
		cityRealtimeCharacterPersonalityTextMaximumRunes,
		"background",
	)
	if err != nil {
		return CityRealtimeCharacterPersonalitySeed{}, err
	}
	notes, err := normalizeCityRealtimeCharacterPersonalityText(
		input.FreeformNotes,
		0,
		cityRealtimeCharacterPersonalityTextMaximumRunes,
		"freeform_notes",
	)
	if err != nil {
		return CityRealtimeCharacterPersonalitySeed{}, err
	}
	return CityRealtimeCharacterPersonalitySeed{
		Values:         values,
		Preferences:    preferences,
		Background:     background,
		HardBoundaries: boundaries,
		FreeformNotes:  notes,
	}, nil
}

func normalizeCityRealtimeCharacterPersonalityList(
	values []string,
	minimum, maximum int,
	field string,
) ([]string, error) {
	if len(values) < minimum || len(values) > maximum {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": field})
	}
	items := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized, err := normalizeCityRealtimeCharacterPersonalityText(
			value,
			1,
			cityRealtimeCharacterPersonalityValueMaximumRunes,
			field,
		)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[normalized]; duplicate {
			return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": field})
		}
		seen[normalized] = struct{}{}
		items = append(items, normalized)
	}
	sort.Strings(items)
	return items, nil
}

func normalizeCityRealtimeCharacterPersonalityPreferences(
	input map[string]string,
) (map[string]string, error) {
	if input == nil {
		return map[string]string{}, nil
	}
	if len(input) > cityRealtimeCharacterPersonalityPreferencesMaximum {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "preferences"})
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if !cityRealtimeAgentIdentifierValid(key, 32) {
			return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "preferences"})
		}
		normalized, err := normalizeCityRealtimeCharacterPersonalityText(
			value,
			1,
			cityRealtimeCharacterPersonalityValueMaximumRunes,
			"preferences",
		)
		if err != nil {
			return nil, err
		}
		result[key] = normalized
	}
	return result, nil
}

func normalizeCityRealtimeCharacterPersonalityText(
	value string,
	minimum, maximum int,
	field string,
) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrCityInvalidInput.WithMetadata(map[string]string{"field": field})
	}
	value = strings.TrimSpace(value)
	length := 0
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return "", ErrCityInvalidInput.WithMetadata(map[string]string{"field": field})
		}
		length++
	}
	if length < minimum || length > maximum {
		return "", ErrCityInvalidInput.WithMetadata(map[string]string{"field": field})
	}
	return value, nil
}

func loadCityRealtimeCharacterAgentPersonalityRevision(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	agentCode string,
	forUpdate bool,
) (cityRealtimeCharacterPersonalityRevision, bool, error) {
	if worldID <= 0 || !cityRealtimeAgentIdentifierValid(agentCode, 96) {
		return cityRealtimeCharacterPersonalityRevision{}, false, ErrCityInvalidInput
	}
	query := `
SELECT revision, seed, seed_hash, previous_seed_hash, changed_by_user_id,
       created_frame_sequence,
       encode(sha256(convert_to(seed::text, 'UTF8')), 'hex')
FROM city_realtime_character_agent_personality_revisions
WHERE world_id = $1 AND agent_code = $2
ORDER BY revision DESC
LIMIT 1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	item := cityRealtimeCharacterPersonalityRevision{AgentCode: agentCode}
	var rawSeed []byte
	var previousSeedHash sql.NullString
	var computedHash string
	err := queryer.QueryRowContext(ctx, query, worldID, agentCode).Scan(
		&item.Revision, &rawSeed, &item.SeedHash, &previousSeedHash, &item.ChangedByUserID,
		&item.CreatedFrameSequence, &computedHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterPersonalityRevision{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterPersonalityRevision{}, false, fmt.Errorf("load realtime character Agent personality revision: %w", err)
	}
	if err = json.Unmarshal(rawSeed, &item.Seed); err != nil {
		return cityRealtimeCharacterPersonalityRevision{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_personality_seed"}).WithCause(err)
	}
	normalized, normalizeErr := normalizeCityRealtimeCharacterPersonalitySeed(item.Seed)
	if normalizeErr != nil || !reflect.DeepEqual(normalized, item.Seed) ||
		!cityRealtimeSHA256Hex(item.SeedHash) || item.SeedHash != computedHash ||
		item.Revision <= 0 || item.ChangedByUserID <= 0 || item.CreatedFrameSequence <= 0 {
		return cityRealtimeCharacterPersonalityRevision{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_personality_revision"})
	}
	item.PreviousSeedHash = cityRealtimeAgentNullStringPointer(previousSeedHash)
	if (item.Revision == 1 && item.PreviousSeedHash != nil) ||
		(item.Revision > 1 && (item.PreviousSeedHash == nil || !cityRealtimeSHA256Hex(*item.PreviousSeedHash))) {
		return cityRealtimeCharacterPersonalityRevision{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_personality_chain"})
	}
	return item, true, nil
}

func insertCityRealtimeCharacterAgentPersonalityRevision(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	agent cityRealtimeAgentInstance,
	userID int64,
	seed CityRealtimeCharacterPersonalitySeed,
	previous *cityRealtimeCharacterPersonalityRevision,
	frameSequence int64,
) (cityRealtimeCharacterPersonalityRevision, error) {
	if tx == nil || worldID <= 0 || userID <= 0 || frameSequence <= 0 ||
		agent.AgentSubtype != "character.user" || agent.OwnerUserID == nil || *agent.OwnerUserID != userID {
		return cityRealtimeCharacterPersonalityRevision{}, ErrCityInvalidInput
	}
	normalized, err := normalizeCityRealtimeCharacterPersonalitySeed(seed)
	if err != nil {
		return cityRealtimeCharacterPersonalityRevision{}, err
	}
	rawSeed, err := json.Marshal(normalized)
	if err != nil {
		return cityRealtimeCharacterPersonalityRevision{}, fmt.Errorf("marshal realtime character Agent personality seed: %w", err)
	}
	revision := int64(1)
	var previousSeedHash *string
	if previous != nil {
		revision = previous.Revision + 1
		previousHash := previous.SeedHash
		previousSeedHash = &previousHash
	}
	result := cityRealtimeCharacterPersonalityRevision{
		AgentCode:            agent.AgentCode,
		Revision:             revision,
		Seed:                 normalized,
		PreviousSeedHash:     previousSeedHash,
		ChangedByUserID:      userID,
		CreatedFrameSequence: frameSequence,
	}
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_realtime_character_agent_personality_revisions
    (world_id, agent_code, revision, seed, seed_hash, previous_seed_hash,
     changed_by_user_id, created_frame_sequence)
VALUES ($1, $2, $3, $4::jsonb,
        encode(sha256(convert_to(($4::jsonb)::text, 'UTF8')), 'hex'), $5,
        $6, $7)
RETURNING seed_hash`,
		worldID, result.AgentCode, result.Revision, string(rawSeed), result.PreviousSeedHash,
		result.ChangedByUserID, result.CreatedFrameSequence,
	).Scan(&result.SeedHash); err != nil {
		return cityRealtimeCharacterPersonalityRevision{}, fmt.Errorf("insert realtime character Agent personality revision: %w", err)
	}
	if !cityRealtimeSHA256Hex(result.SeedHash) {
		return cityRealtimeCharacterPersonalityRevision{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_personality_hash"})
	}
	return result, nil
}

func cityRealtimeCharacterAgentPendingWork(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	agentCode string,
) (bool, bool, error) {
	if worldID <= 0 || !cityRealtimeAgentIdentifierValid(agentCode, 96) {
		return false, false, ErrCityInvalidInput
	}
	var pendingDecision, pendingIntent bool
	if err := queryer.QueryRowContext(ctx, `
SELECT EXISTS(
           SELECT 1 FROM city_realtime_agent_decision_requests
           WHERE world_id = $1 AND agent_code = $2 AND status IN ('queued', 'leased')
       ),
       EXISTS(
           SELECT 1 FROM city_realtime_agent_intents
           WHERE world_id = $1 AND agent_code = $2 AND status = 'pending'
       )`, worldID, agentCode).Scan(&pendingDecision, &pendingIntent); err != nil {
		return false, false, fmt.Errorf("load realtime character Agent pending work: %w", err)
	}
	return pendingDecision, pendingIntent, nil
}

func cityRealtimeCharacterAgentProjection(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	binding cityRealtimeAgentPolicyBinding,
	agent cityRealtimeAgentInstance,
) (*CityRealtimeCharacterAgentConfiguration, error) {
	if !cityRealtimeAgentCharacterControlRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" {
		return nil, nil
	}
	personality, found, err := loadCityRealtimeCharacterAgentPersonalityRevision(ctx, queryer, worldID, agent.AgentCode, false)
	if err != nil {
		return nil, err
	}
	pendingDecision, pendingIntent, err := cityRealtimeCharacterAgentPendingWork(ctx, queryer, worldID, agent.AgentCode)
	if err != nil {
		return nil, err
	}
	item := &CityRealtimeCharacterAgentConfiguration{
		ControlMode:              agent.ControlMode,
		PendingDecision:          pendingDecision,
		PendingIntent:            pendingIntent,
		AutonomyRuntimeAvailable: true,
	}
	if found {
		projection := personality.projection()
		item.Personality = &projection
	}
	return item, nil
}

func cityRealtimeCharacterAgentConfigurationValid(item CityRealtimeCharacterAgentConfiguration) bool {
	if !cityRealtimeAgentControlModeValid(item.ControlMode) || item.ControlMode == "system" ||
		!item.AutonomyRuntimeAvailable {
		return false
	}
	if item.Personality == nil {
		return true
	}
	if item.Personality.SchemaVersion != cityRealtimeCharacterPersonalitySchemaVersion ||
		item.Personality.Revision <= 0 || !cityRealtimeSHA256Hex(item.Personality.SeedHash) {
		return false
	}
	normalized, err := normalizeCityRealtimeCharacterPersonalitySeed(item.Personality.Seed)
	return err == nil && reflect.DeepEqual(normalized, item.Personality.Seed)
}

func cityRealtimeCharacterOwnerControlModeAllowed(
	ctx context.Context,
	current, requested string,
) bool {
	if !cityRealtimeAgentControlModeValid(requested) || requested == "system" {
		return false
	}
	if IsCitySystemAdministrator(ctx) {
		return true
	}
	if requested == current {
		return true
	}
	return requested == "autonomous" || requested == "suspended"
}

func cityRealtimeCharacterAgentControlUpdate(
	binding cityRealtimeAgentPolicyBinding,
	agent cityRealtimeAgentInstance,
	controlMode string,
	frameSequence int64,
	reasonCode string,
) (cityRealtimeAgentInstance, cityRealtimeAgentLifecycleEvent, error) {
	if !cityRealtimeAgentCharacterControlRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" ||
		agent.LifecycleStatus != "active" || !cityRealtimeAgentControlModeValid(controlMode) || controlMode == "system" ||
		controlMode == agent.ControlMode || frameSequence <= agent.LastFrameSequence ||
		!cityRealtimeAgentIdentifierValid(reasonCode, 64) {
		return cityRealtimeAgentInstance{}, cityRealtimeAgentLifecycleEvent{}, ErrCityInvalidInput
	}
	previousStatus := agent.LifecycleStatus
	previousHash := agent.EventChainHash
	next := agent
	next.ControlMode = controlMode
	next.LifecycleRevision++
	next.LastFrameSequence = frameSequence
	event := cityRealtimeAgentLifecycleEvent{
		AgentCode:         next.AgentCode,
		EventSequence:     next.LifecycleRevision - 1,
		FrameSequence:     frameSequence,
		EventType:         "control",
		FromStatus:        &previousStatus,
		ToStatus:          previousStatus,
		ControlMode:       controlMode,
		ReasonCode:        reasonCode,
		PreviousEventHash: &previousHash,
	}
	var err error
	if event.EventHash, err = cityRealtimeAgentLifecycleEventHash(binding, event); err != nil {
		return cityRealtimeAgentInstance{}, cityRealtimeAgentLifecycleEvent{}, err
	}
	next.EventChainHash = event.EventHash
	if next.InstanceHash, err = cityRealtimeAgentInstanceHash(binding, next); err != nil {
		return cityRealtimeAgentInstance{}, cityRealtimeAgentLifecycleEvent{}, err
	}
	return next, event, nil
}

func updateCityRealtimeCharacterAgentControl(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previous, next cityRealtimeAgentInstance,
	event cityRealtimeAgentLifecycleEvent,
) error {
	if tx == nil || worldID <= 0 || previous.AgentCode != next.AgentCode ||
		next.LifecycleRevision != previous.LifecycleRevision+1 ||
		event.EventSequence != next.LifecycleRevision-1 || event.EventType != "control" ||
		event.EventHash != next.EventChainHash {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_instances
SET lifecycle_status = $3, control_mode = $4, lifecycle_revision = $5,
    last_frame_sequence = $6, instance_hash = $7, event_chain_hash = $8,
    updated_at = NOW()
WHERE world_id = $1 AND agent_code = $2
  AND lifecycle_revision = $9 AND instance_hash = $10 AND event_chain_hash = $11`,
		worldID, previous.AgentCode, next.LifecycleStatus, next.ControlMode,
		next.LifecycleRevision, next.LastFrameSequence, next.InstanceHash, next.EventChainHash,
		previous.LifecycleRevision, previous.InstanceHash, previous.EventChainHash,
	)
	if err != nil {
		return fmt.Errorf("update realtime character Agent control: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime character Agent control update: %w", rowsErr)
	} else if rows != 1 {
		return ErrCityRealtimeAgentDecisionConflict.WithMetadata(map[string]string{"field": "agent_control"})
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_lifecycle_events
    (world_id, agent_code, event_sequence, frame_sequence, event_type,
     from_status, to_status, control_mode, reason_code, previous_event_hash,
     event_hash, metadata)
VALUES ($1, $2, $3, $4, 'control', $5, $6, $7, $8, $9, $10, '{}'::jsonb)`,
		worldID, event.AgentCode, event.EventSequence, event.FrameSequence,
		event.FromStatus, event.ToStatus, event.ControlMode, event.ReasonCode,
		event.PreviousEventHash, event.EventHash,
	); err != nil {
		return fmt.Errorf("append realtime character Agent control event: %w", err)
	}
	return nil
}

func cancelCityRealtimeCharacterAgentPendingWork(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	agentCode string,
	frameSequence int64,
) (queuedRequests, pendingIntents int64, err error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 || !cityRealtimeAgentIdentifierValid(agentCode, 96) {
		return 0, 0, ErrCityInvalidInput
	}
	requestResult, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_requests
SET status = 'cancelled', terminal_frame_sequence = $3, updated_at = NOW()
WHERE world_id = $1 AND agent_code = $2 AND status = 'queued'`, worldID, agentCode, frameSequence)
	if err != nil {
		return 0, 0, fmt.Errorf("cancel queued realtime character Agent decisions: %w", err)
	}
	if queuedRequests, err = requestResult.RowsAffected(); err != nil {
		return 0, 0, fmt.Errorf("count cancelled realtime character Agent decisions: %w", err)
	}
	if queuedRequests > 0 {
		outboxResult, outboxErr := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_outbox outbox
SET status = 'cancelled', completed_at = NOW(), updated_at = NOW()
FROM city_realtime_agent_decision_requests request
WHERE outbox.world_id = request.world_id
  AND outbox.request_code = request.request_code
  AND request.world_id = $1 AND request.agent_code = $2
  AND request.status = 'cancelled'
  AND request.terminal_frame_sequence = $3
  AND outbox.status = 'queued'`, worldID, agentCode, frameSequence)
		if outboxErr != nil {
			return 0, 0, fmt.Errorf("cancel queued realtime character Agent outbox: %w", outboxErr)
		}
		if outboxRows, rowsErr := outboxResult.RowsAffected(); rowsErr != nil {
			return 0, 0, fmt.Errorf("count cancelled realtime character Agent outbox: %w", rowsErr)
		} else if outboxRows != queuedRequests {
			return 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_agent_outbox_cancel"})
		}
	}
	intentResult, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_intents
SET status = 'cancelled', resolved_frame_sequence = $3, updated_at = NOW()
WHERE world_id = $1 AND agent_code = $2 AND status = 'pending'`, worldID, agentCode, frameSequence)
	if err != nil {
		return 0, 0, fmt.Errorf("cancel pending realtime character Agent intents: %w", err)
	}
	if pendingIntents, err = intentResult.RowsAffected(); err != nil {
		return 0, 0, fmt.Errorf("count cancelled realtime character Agent intents: %w", err)
	}
	return queuedRequests, pendingIntents, nil
}

func (s *CityEconomyService) ConfigureRealtimeCharacterAgent(
	ctx context.Context,
	input CityRealtimeCharacterAgentConfigureInput,
) (*CityRealtimeCharacterMutationResult, error) {
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	idempotencyKey, err := normalizeCityRealtimeCharacterIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	requestedMode := strings.TrimSpace(input.ControlMode)
	if requestedMode != "" && !cityRealtimeAgentControlModeValid(requestedMode) {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "control_mode"})
	}
	var requestedPersonality *CityRealtimeCharacterPersonalitySeed
	if input.Personality != nil {
		normalized, normalizeErr := normalizeCityRealtimeCharacterPersonalitySeed(*input.Personality)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		requestedPersonality = &normalized
	}
	requestHash, err := cityRealtimeCharacterRequestHash(cityRealtimeCharacterAgentConfigureAction, map[string]any{
		"world_id":     input.WorldID,
		"user_id":      input.UserID,
		"control_mode": requestedMode,
		"personality":  requestedPersonality,
	})
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime character Agent configure transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockCityRealtimeCharacterWorld(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	world, err := lockCityWorld(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	state, err := lockCityRealtimeState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if err = validateCityRealtimeCharacterCommandWindow(world, state); err != nil {
		return nil, err
	}
	if receipt, found, receiptErr := loadCityRealtimeCharacterActionReceipt(ctx, tx, input.WorldID, input.UserID, idempotencyKey); receiptErr != nil {
		return nil, receiptErr
	} else if found {
		return completeCityRealtimeCharacterReceipt(tx, receipt, cityRealtimeCharacterAgentConfigureAction, requestHash)
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if agentState == nil || agentState.Binding == nil || !cityRealtimeAgentCharacterControlRuntimeEnabled(*agentState.Binding) {
		return nil, ErrCityRealtimeAgentRuntimeUnavailable
	}
	record, found, err := loadCityRealtimeOwnedCharacter(ctx, tx, input.WorldID, input.UserID, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterNotFound
	}
	if record.agent.LifecycleStatus != "active" || record.identity.LifecycleStatus != "active" {
		return nil, ErrCityRealtimeCharacterControlUnavailable
	}
	targetMode := record.agent.ControlMode
	if requestedMode != "" {
		if !cityRealtimeCharacterOwnerControlModeAllowed(ctx, record.agent.ControlMode, requestedMode) {
			return nil, ErrCityRealtimeCharacterControlUnavailable.WithMetadata(map[string]string{"field": "control_mode"})
		}
		targetMode = requestedMode
	}
	previousPersonality, hasPersonality, err := loadCityRealtimeCharacterAgentPersonalityRevision(
		ctx, tx, input.WorldID, record.agent.AgentCode, true,
	)
	if err != nil {
		return nil, err
	}
	if targetMode == "autonomous" && requestedPersonality == nil && !hasPersonality {
		return nil, ErrCityRealtimeCharacterControlUnavailable.WithMetadata(map[string]string{"field": "personality"})
	}
	if targetMode == record.agent.ControlMode && requestedPersonality == nil {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "character_agent_configuration"})
	}
	if err = cityRealtimeRejectPendingDueAtCurrentTime(ctx, tx, input.WorldID, state.currentWorldTimeUS); err != nil {
		return nil, err
	}
	frameSequence, cursor, err := cityRealtimeCharacterNextFrame(state)
	if err != nil {
		return nil, err
	}
	if err = enableCityRealtimeCharacterMutationGates(ctx, tx, input.WorldID, frameSequence, true); err != nil {
		return nil, err
	}
	queuedRequests, pendingIntents, err := cancelCityRealtimeCharacterAgentPendingWork(
		ctx, tx, input.WorldID, record.agent.AgentCode, frameSequence,
	)
	if err != nil {
		return nil, err
	}
	nextAgent := record.agent
	if targetMode != record.agent.ControlMode {
		var controlEvent cityRealtimeAgentLifecycleEvent
		nextAgent, controlEvent, err = cityRealtimeCharacterAgentControlUpdate(
			*agentState.Binding, record.agent, targetMode, frameSequence, cityRealtimeCharacterAgentConfigureAction,
		)
		if err != nil {
			return nil, err
		}
		if err = updateCityRealtimeCharacterAgentControl(ctx, tx, input.WorldID, record.agent, nextAgent, controlEvent); err != nil {
			return nil, err
		}
	}
	var effectivePersonality *cityRealtimeCharacterPersonalityRevision
	if requestedPersonality != nil {
		var previous *cityRealtimeCharacterPersonalityRevision
		if hasPersonality {
			previous = &previousPersonality
		}
		inserted, insertErr := insertCityRealtimeCharacterAgentPersonalityRevision(
			ctx, tx, input.WorldID, nextAgent, input.UserID, *requestedPersonality, previous, frameSequence,
		)
		if insertErr != nil {
			return nil, insertErr
		}
		effectivePersonality = &inserted
	} else if hasPersonality {
		effectivePersonality = &previousPersonality
	}
	wakeupScheduled := false
	if targetMode == "autonomous" {
		if state.currentWorldTimeUS > cityRealtimeMaximumWorldTimeUS-cityRealtimeTimeQuantumUS {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_wakeup_time"})
		}
		wakeupScheduled, err = scheduleCityRealtimeAgentDecisionWakeup(
			ctx, tx, input.WorldID, nextAgent.AgentCode,
			state.currentWorldTimeUS+cityRealtimeTimeQuantumUS, frameSequence,
		)
		if err != nil {
			return nil, err
		}
	}
	state.nextDueAtWorldTimeUS, err = cityRealtimeNextPendingDue(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	result := &CityRealtimeCharacterMutationResult{
		Character: cityRealtimeCharacterRecord{agent: nextAgent, identity: record.identity, state: record.state}.projection(),
		Agent: &CityRealtimeCharacterAgentConfiguration{
			ControlMode:              nextAgent.ControlMode,
			PendingDecision:          false,
			PendingIntent:            false,
			AutonomyRuntimeAvailable: true,
		},
	}
	if effectivePersonality != nil {
		projection := effectivePersonality.projection()
		result.Agent.Personality = &projection
	}
	if result.Frame, err = sealCityRealtimeCharacterFrame(
		ctx, tx, input.WorldID, world, state, frameSequence, cursor, cityRealtimeCharacterAgentConfigureAction,
		map[string]any{
			"character_agent_control_changed":        boolToCityRealtimeCount(targetMode != record.agent.ControlMode),
			"character_agent_personality_revised":    boolToCityRealtimeCount(requestedPersonality != nil),
			"character_agent_queued_requests_closed": queuedRequests,
			"character_agent_pending_intents_closed": pendingIntents,
			"character_agent_wakeup_scheduled":       boolToCityRealtimeCount(wakeupScheduled),
		},
	); err != nil {
		return nil, err
	}
	if err = canonicalizeCityRealtimeCharacterMutationResult(result); err != nil {
		return nil, err
	}
	if err = storeCityRealtimeCharacterActionReceipt(
		ctx, tx, input.WorldID, input.UserID, idempotencyKey, record.identity.ActorCode,
		cityRealtimeCharacterAgentConfigureAction, requestHash, frameSequence, *result,
	); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character Agent configure: %w", err)
	}
	return result, nil
}

// cityRealtimeCharacterAgentWakeupTrigger uses a stable opaque key.  It
// keeps one wakeup causal event distinct from another without leaking an
// owner, personality body, or provider selection into the due-event key.
func cityRealtimeCharacterAgentWakeupTrigger(agentCode string, dueWorldTimeUS int64) (string, error) {
	if !cityRealtimeAgentIdentifierValid(agentCode, 96) || dueWorldTimeUS < 0 {
		return "", ErrCityInvalidInput
	}
	return cityRealtimeAgentDecisionStableCode("wake", agentCode, strconv.FormatInt(dueWorldTimeUS, 10))
}
