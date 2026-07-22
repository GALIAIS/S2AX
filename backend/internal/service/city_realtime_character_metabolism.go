package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const (
	cityRealtimeDueEventTypeCharacterMetabolism = "system.realtime.character_metabolism"
	cityRealtimeCharacterMetabolismPriority     = 200
)

// cityRealtimeCharacterMetabolismDuePayload is deliberately finite. The
// payload identifies an already-created character and the next passive-needs
// revision; it contains no browser clock, model output, or player choice.
type cityRealtimeCharacterMetabolismDuePayload struct {
	ActorCode string `json:"actor_code"`
	Step      int64  `json:"step"`
}

// cityRealtimeCharacterProfileMatchesRuntime prevents a catalog/profile
// schema mismatch from becoming a partially functional character. Legacy
// worlds remain on schema 1; passive metabolism uses schema 2, and the
// progression successor uses schema 3 while preserving the same reducer.
func cityRealtimeCharacterProfileMatchesRuntime(
	profile cityRealtimeCharacterProfile,
	runtime *cityRealtimeCharacterLifeRuntime,
) bool {
	if runtime == nil || !cityRealtimeCharacterProfileValid(profile) {
		return false
	}
	if runtime.Progression != nil {
		return profile.StateSchemaVersion == cityRealtimeCharacterProfileSchemaProgression &&
			runtime.Metabolism != nil && cityRealtimeCharacterMetabolismDefinitionValid(*runtime.Metabolism) &&
			cityRealtimeCharacterProfileMatchesProgressionRuntime(profile, runtime)
	}
	if runtime.Metabolism == nil {
		return profile.StateSchemaVersion == cityRealtimeCharacterProfileSchemaLegacy
	}
	return profile.StateSchemaVersion == cityRealtimeCharacterProfileSchemaMetabolism &&
		cityRealtimeCharacterMetabolismDefinitionValid(*runtime.Metabolism)
}

func cityRealtimeCharacterNextMetabolismWorldTime(
	profile cityRealtimeCharacterProfile,
	definition cityRealtimeCharacterMetabolismDefinition,
) (int64, error) {
	if !cityRealtimeCharacterProfileValid(profile) ||
		(profile.StateSchemaVersion != cityRealtimeCharacterProfileSchemaMetabolism &&
			profile.StateSchemaVersion != cityRealtimeCharacterProfileSchemaProgression) ||
		!cityRealtimeCharacterMetabolismDefinitionValid(definition) ||
		profile.LastMetabolismWorldTimeUS > cityRealtimeMaximumWorldTimeUS-definition.IntervalUS {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "realtime_character_metabolism_time",
		})
	}
	return profile.LastMetabolismWorldTimeUS + definition.IntervalUS, nil
}

// scheduleCityRealtimeCharacterMetabolismDueEvent appends the single next
// passive-needs event for one metabolism-capable profile. expected_version deliberately
// tracks the metabolism revision rather than the general profile revision, so
// normal player actions between two real-time intervals cannot invalidate the
// already scheduled passive update.
func scheduleCityRealtimeCharacterMetabolismDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	runtime *cityRealtimeCharacterLifeRuntime,
	profile cityRealtimeCharacterProfile,
	createdFrameSequence int64,
) error {
	if tx == nil || worldID <= 0 || createdFrameSequence <= 0 ||
		!cityRealtimeCharacterProfileMatchesRuntime(profile, runtime) || runtime.Metabolism == nil ||
		profile.MetabolismRevision >= cityRealtimeMaximumWorldTimeUS {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "realtime_character_metabolism_schedule",
		})
	}
	dueWorldTimeUS, err := cityRealtimeCharacterNextMetabolismWorldTime(profile, *runtime.Metabolism)
	if err != nil {
		return err
	}
	step := profile.MetabolismRevision + 1
	payload, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"actor_code": profile.ActorCode,
		"step":       step,
	})
	if err != nil {
		return fmt.Errorf("canonicalize realtime character metabolism payload: %w", err)
	}
	dedupKey := fmt.Sprintf("character.metabolism.%s.%012d", profile.ActorCode, step)
	if !cityRealtimeDueEventIdentifierValid(dedupKey, 160) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "realtime_character_metabolism_dedup",
		})
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_due_events
    (world_id, event_type, schema_version, due_world_time_us, temporal_phase,
     priority, aggregate_type, aggregate_key, dedup_key, source_kind,
     source_reference, payload, payload_hash, expected_version, status,
     created_frame_sequence)
VALUES ($1, $2, 1, $3, 'activity', $4, 'realtime_character', $5, $6, 'system',
        'realtime_character_life', $7::jsonb, $8, $9, 'pending', $10)`,
		worldID, cityRealtimeDueEventTypeCharacterMetabolism, dueWorldTimeUS,
		cityRealtimeCharacterMetabolismPriority, "character:"+profile.ActorCode,
		dedupKey, []byte(payload), payloadHash, profile.MetabolismRevision,
		createdFrameSequence,
	); err != nil {
		return fmt.Errorf("schedule realtime character metabolism %s: %w", profile.ActorCode, err)
	}
	return nil
}

func decodeCityRealtimeCharacterMetabolismDuePayload(
	event cityRealtimeDueEventRecord,
) (cityRealtimeCharacterMetabolismDuePayload, bool) {
	payload := cityRealtimeCharacterMetabolismDuePayload{}
	if err := json.Unmarshal(event.Payload, &payload); err != nil ||
		!cityRealtimePlayerActorCodeValid(payload.ActorCode) || payload.Step <= 0 {
		return cityRealtimeCharacterMetabolismDuePayload{}, false
	}
	_, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"actor_code": payload.ActorCode,
		"step":       payload.Step,
	})
	if err != nil || payloadHash != event.PayloadHash {
		return cityRealtimeCharacterMetabolismDuePayload{}, false
	}
	return payload, true
}

// cityRealtimeCharacterApplyMetabolism is a pure, bounded transition. Its
// only effects are the three needs values and the dedicated metabolism head;
// inventory, city-credit, activity facts, and law facts are intentionally
// unchanged.
func cityRealtimeCharacterApplyMetabolism(
	profile cityRealtimeCharacterProfile,
	definition cityRealtimeCharacterMetabolismDefinition,
	frameSequence, dueWorldTimeUS int64,
) (cityRealtimeCharacterProfile, error) {
	if !cityRealtimeCharacterProfileValid(profile) ||
		(profile.StateSchemaVersion != cityRealtimeCharacterProfileSchemaMetabolism &&
			profile.StateSchemaVersion != cityRealtimeCharacterProfileSchemaProgression) ||
		!cityRealtimeCharacterMetabolismDefinitionValid(definition) ||
		frameSequence <= profile.LastFrameSequence ||
		frameSequence > cityRealtimeMaximumTimelineSequence ||
		profile.Revision >= cityRealtimeMaximumWorldTimeUS ||
		profile.MetabolismRevision >= cityRealtimeMaximumWorldTimeUS {
		return cityRealtimeCharacterProfile{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "realtime_character_metabolism_state",
		})
	}
	expectedDueWorldTimeUS, err := cityRealtimeCharacterNextMetabolismWorldTime(profile, definition)
	if err != nil || dueWorldTimeUS != expectedDueWorldTimeUS {
		return cityRealtimeCharacterProfile{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "realtime_character_metabolism_due_time",
		}).WithCause(err)
	}
	next := profile
	next.Revision++
	next.MetabolismRevision++
	next.LastFrameSequence = frameSequence
	next.LastMetabolismWorldTimeUS = dueWorldTimeUS
	next.EnergyMilli = clampCityRealtimeCharacterMilli(profile.EnergyMilli + definition.EnergyDeltaMilli)
	next.SatietyMilli = clampCityRealtimeCharacterMilli(profile.SatietyMilli + definition.SatietyDeltaMilli)
	next.MoraleMilli = clampCityRealtimeCharacterMilli(profile.MoraleMilli + definition.MoraleDeltaMilli)
	next.StateHash, err = cityRealtimeCharacterProfileStateHash(next)
	if err != nil {
		return cityRealtimeCharacterProfile{}, err
	}
	if !cityRealtimeCharacterProfileValid(next) {
		return cityRealtimeCharacterProfile{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "realtime_character_metabolism_next_state",
		})
	}
	return next, nil
}

// applyCityRealtimeCharacterMetabolismDueEvent is called only by the locked
// server-side due-event reducer. A malformed, stale, or retired event returns
// false so it is terminally rejected; an invariant or database failure still
// aborts the batch rather than silently corrupting canonical state.
func applyCityRealtimeCharacterMetabolismDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	event cityRealtimeDueEventRecord,
) (bool, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 ||
		event.EventType != cityRealtimeDueEventTypeCharacterMetabolism || event.SchemaVersion != 1 ||
		event.SourceKind != cityRealtimeDueEventSourceKindSystem || event.TemporalPhase != "activity" ||
		event.AggregateType != "realtime_character" || event.SourceReference != "realtime_character_life" ||
		event.ExpectedVersion == nil {
		return false, nil
	}
	payload, validPayload := decodeCityRealtimeCharacterMetabolismDuePayload(event)
	if !validPayload || event.AggregateKey != "character:"+payload.ActorCode {
		return false, nil
	}
	runtime, err := loadCityRealtimeCharacterLifeRuntime(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if runtime == nil || runtime.Metabolism == nil {
		return false, nil
	}
	profile, found, err := loadCityRealtimeCharacterProfile(ctx, tx, worldID, payload.ActorCode, true)
	if err != nil {
		return false, err
	}
	if !found || !cityRealtimeCharacterProfileMatchesRuntime(profile, runtime) ||
		*event.ExpectedVersion != profile.MetabolismRevision ||
		payload.Step != profile.MetabolismRevision+1 {
		return false, nil
	}
	expectedDueWorldTimeUS, dueErr := cityRealtimeCharacterNextMetabolismWorldTime(profile, *runtime.Metabolism)
	if dueErr != nil {
		return false, dueErr
	}
	if event.DueWorldTimeUS != expectedDueWorldTimeUS {
		return false, nil
	}
	if err = enableCityRealtimeCharacterActivityMutationGates(ctx, tx, worldID, frameSequence); err != nil {
		return false, err
	}
	nextProfile, err := cityRealtimeCharacterApplyMetabolism(profile, *runtime.Metabolism, frameSequence, event.DueWorldTimeUS)
	if err != nil {
		return false, err
	}
	if err = updateCityRealtimeCharacterProfile(ctx, tx, worldID, profile, nextProfile); err != nil {
		return false, err
	}
	if err = scheduleCityRealtimeCharacterMetabolismDueEvent(ctx, tx, worldID, runtime, nextProfile, frameSequence); err != nil {
		return false, err
	}
	return true, nil
}
