package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const cityRealtimeLifecycleDueBatchLimit = 256

var ErrCityRealtimeLifecycleBacklog = infraerrors.Conflict(
	"CITY_REALTIME_LIFECYCLE_BACKLOG",
	"realtime lifecycle transition requires more due-event settlement than one bounded operation permits",
)

// CityRealtimeLifecycleInput is a server-owned lifecycle command. Its clock
// observation must come from a configured Clock Authority; it is never
// populated from an HTTP request body or browser timestamp.
type CityRealtimeLifecycleInput struct {
	WorldID     int64
	Observation CityRealtimeClockObservation
}

// CityRealtimeDiagnosticLifecycleInput is restricted to the frozen test
// profile. It exists for deterministic integration/replay tests and is not a
// public control surface for production worlds.
type CityRealtimeDiagnosticLifecycleInput struct {
	WorldID      int64
	EffectiveUTC time.Time
}

// CityRealtimeLifecycleResult is an auditable summary of a pause/resume
// transition. It intentionally excludes worker lease details and raw clock
// provider diagnostics.
type CityRealtimeLifecycleResult struct {
	WorldID              int64              `json:"world_id"`
	Changed              bool               `json:"changed"`
	LifecycleStatus      string             `json:"lifecycle_status"`
	ClockState           string             `json:"clock_state"`
	RecoveryState        string             `json:"recovery_state"`
	CurrentWorldTimeUS   int64              `json:"current_world_time_us"`
	TimelineCursor       string             `json:"timeline_cursor"`
	ClockSegmentSequence int64              `json:"clock_segment_sequence"`
	Frame                *CityTemporalFrame `json:"frame,omitempty"`
}

// PauseRealtimeWorld freezes a production realtime world at a server-observed
// time. Due events that were already due at that boundary are settled first;
// future events remain pending and the time spent paused never becomes world
// elapsed time.
func (s *CityEconomyService) PauseRealtimeWorld(
	ctx context.Context,
	input CityRealtimeLifecycleInput,
) (*CityRealtimeLifecycleResult, error) {
	return s.pauseRealtimeWorldWithObservation(
		ctx,
		input.WorldID,
		input.Observation,
		false,
		"administrative_pause",
	)
}

// PauseRealtimeDiagnosticWorld provides deterministic pause coverage for the
// compiled frozen-test profile. It cannot pause a production profile.
func (s *CityEconomyService) PauseRealtimeDiagnosticWorld(
	ctx context.Context,
	input CityRealtimeDiagnosticLifecycleInput,
) (*CityRealtimeLifecycleResult, error) {
	if input.EffectiveUTC.IsZero() {
		return nil, ErrCityInvalidInput
	}
	return s.pauseRealtimeWorldWithObservation(
		ctx,
		input.WorldID,
		cityRealtimeDiagnosticClockObservation(input.EffectiveUTC),
		true,
		"diagnostic_pause",
	)
}

func (s *CityEconomyService) pauseRealtimeWorldWithObservation(
	ctx context.Context,
	worldID int64,
	observation CityRealtimeClockObservation,
	requireDiagnosticProfile bool,
	reason string,
) (*CityRealtimeLifecycleResult, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	for attempt := 0; attempt < cityRealtimeLifecycleDueBatchLimit; attempt++ {
		result, err := s.pauseRealtimeWorldAtObservation(
			ctx, worldID, observation, requireDiagnosticProfile, reason,
		)
		if !errors.Is(err, ErrCityRealtimeDueEventPending) {
			return result, err
		}
		processed, processErr := s.processRealtimeDueEventsWithClock(
			ctx, worldID, observation, requireDiagnosticProfile, reason,
		)
		if processErr != nil {
			return nil, processErr
		}
		// A concurrent scheduler may have resolved the event between the
		// lifecycle boundary check and this reducer attempt. Retry the
		// boundary transaction in that case; it remains serialized by the
		// world advisory lock.
		if !processed.Resolved {
			continue
		}
	}
	return nil, ErrCityRealtimeLifecycleBacklog.WithMetadata(map[string]string{
		"world_id": fmt.Sprintf("%d", worldID),
	})
}

func (s *CityEconomyService) pauseRealtimeWorldAtObservation(
	ctx context.Context,
	worldID int64,
	observation CityRealtimeClockObservation,
	requireDiagnosticProfile bool,
	reason string,
) (*CityRealtimeLifecycleResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city realtime pause transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(worldID)); err != nil {
		return nil, fmt.Errorf("lock city realtime pause world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, 0, worldID)
	if err != nil {
		return nil, err
	}
	if !cityEngineIsRealtime(world.simulationVersion) {
		return nil, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": world.simulationVersion})
	}
	if world.stateHash == nil {
		return nil, ErrCityWorldUnavailable.WithMetadata(map[string]string{"status": world.status})
	}
	state, err := lockCityRealtimeState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if world.status == CityWorldStatusPaused && state.lifecycleStatus == CityWorldStatusPaused {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit existing city realtime pause: %w", err)
		}
		return cityRealtimeLifecycleResultFromState(worldID, false, state, nil), nil
	}
	if world.status != CityWorldStatusRunning || state.lifecycleStatus != CityWorldStatusRunning {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_lifecycle_status"})
	}
	if err = requireCityRealtimeLifecycleProfile(state, requireDiagnosticProfile); err != nil {
		return nil, err
	}
	if !requireDiagnosticProfile &&
		state.clockState != cityRealtimeClockStateInitializing &&
		state.clockState != cityRealtimeClockStateHealthy {
		return nil, ErrCityRealtimeClockUnsafe
	}
	effectiveUTC, err := validateCityRealtimeClockObservation(state, observation)
	if err != nil {
		return nil, err
	}
	if effectiveUTC.Before(state.lastCommittedEffectiveUTC) {
		return nil, ErrCityRealtimeClockUnsafe.WithMetadata(map[string]string{
			"last_committed_effective_utc": state.lastCommittedEffectiveUTC.Format(time.RFC3339Nano),
		})
	}
	if err = validateCityRealtimeClockObservationDatabaseSkew(ctx, tx, state, effectiveUTC); err != nil {
		return nil, err
	}
	if err = recordCityRealtimeClockObservation(ctx, tx, observation); err != nil {
		return nil, err
	}
	deltaUS := effectiveUTC.UnixMicro() - state.lastCommittedEffectiveUTC.UnixMicro()
	quantizedDeltaUS := (deltaUS / state.timeQuantumUS) * state.timeQuantumUS
	if state.currentWorldTimeUS > cityRealtimeMaximumWorldTimeUS-quantizedDeltaUS ||
		state.timelineFrameSequence == cityRealtimeMaximumTimelineSequence {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_pause"})
	}
	targetWorldTimeUS := state.currentWorldTimeUS + quantizedDeltaUS
	pendingDue, err := cityRealtimeFirstPendingDueAtOrBefore(ctx, tx, worldID, targetWorldTimeUS)
	if err != nil {
		return nil, err
	}
	if pendingDue != nil {
		return nil, ErrCityRealtimeDueEventPending.WithMetadata(map[string]string{
			"next_due_at_world_time_us": fmt.Sprintf("%d", *pendingDue),
		})
	}
	targetEffectiveUTC := cityRealtimeAddMicroseconds(state.lastCommittedEffectiveUTC, quantizedDeltaUS)
	if targetEffectiveUTC.After(effectiveUTC) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_pause_quantum"})
	}
	closeResult, err := tx.ExecContext(ctx, `
UPDATE city_world_clock_segments
SET closed_at = NOW(), close_reason = 'pause'
WHERE world_id = $1 AND id = $2 AND closed_at IS NULL`, worldID, state.clockSegmentID)
	if err != nil {
		return nil, fmt.Errorf("close city realtime clock segment for pause: %w", err)
	}
	closedRows, err := closeResult.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("check city realtime clock segment pause close: %w", err)
	}
	if closedRows != 1 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_clock_segment"})
	}
	frameSequence := state.timelineFrameSequence + 1
	cursor, err := cityRealtimeTimelineCursor(frameSequence)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_worlds
SET status = $2, updated_at = NOW()
WHERE id = $1`, worldID, CityWorldStatusPaused); err != nil {
		return nil, fmt.Errorf("pause city realtime world: %w", err)
	}
	if err = updateCityRealtimeLifecycleStateForFrame(
		ctx, tx, worldID, CityWorldStatusPaused, state.clockSegmentID,
		targetWorldTimeUS, targetEffectiveUTC, frameSequence, cursor,
		state.nextDueAtWorldTimeUS, nil,
		cityRealtimeClockStateHealthy, cityRealtimeRecoveryStateIdle,
	); err != nil {
		return nil, err
	}
	_, _, stateHash, err := canonicalCityWorldState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2, updated_at = NOW() WHERE id = $1`, worldID, stateHash); err != nil {
		return nil, fmt.Errorf("store city realtime pause state hash: %w", err)
	}
	frame, err := insertCityRealtimeFrame(ctx, tx, cityRealtimeFrameInsertInput{
		WorldID:               worldID,
		TemporalEngineVersion: state.temporalEngineVersion,
		FrameSequence:         frameSequence,
		TimelineCursor:        cursor,
		WorldTimeFromUS:       state.currentWorldTimeUS,
		WorldTimeToUS:         targetWorldTimeUS,
		ClockSegmentID:        state.clockSegmentID,
		ClockSegmentSequence:  state.clockSegmentSequence,
		ClockProfileHash:      state.clockProfileHash,
		FrameKind:             "lifecycle",
		EffectiveUTCFrom:      state.lastCommittedEffectiveUTC,
		EffectiveUTCTo:        targetEffectiveUTC,
		PreviousStateHash:     world.stateHash,
		StateHash:             stateHash,
		DueEventDigest:        cityRealtimeEmptyDueEventDigest,
		PhaseSummary: map[string]any{
			"schema_version":         1,
			"frame_kind":             "lifecycle",
			"command_count":          0,
			"due_event_count":        0,
			"reason":                 reason,
			"lifecycle_state_before": CityWorldStatusRunning,
			"lifecycle_state_after":  CityWorldStatusPaused,
			"clock_state_before":     state.clockState,
			"clock_state_after":      cityRealtimeClockStateHealthy,
		},
	})
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city realtime pause: %w", err)
	}
	return &CityRealtimeLifecycleResult{
		WorldID:              worldID,
		Changed:              true,
		LifecycleStatus:      CityWorldStatusPaused,
		ClockState:           cityRealtimeClockStateHealthy,
		RecoveryState:        cityRealtimeRecoveryStateIdle,
		CurrentWorldTimeUS:   targetWorldTimeUS,
		TimelineCursor:       cursor,
		ClockSegmentSequence: state.clockSegmentSequence,
		Frame:                frame,
	}, nil
}

// ResumeRealtimeWorld resumes a production realtime world from a fresh
// server-owned observation. It creates a new clock segment at the observed
// UTC and deliberately does not add the paused real-time interval to world
// elapsed time.
func (s *CityEconomyService) ResumeRealtimeWorld(
	ctx context.Context,
	input CityRealtimeLifecycleInput,
) (*CityRealtimeLifecycleResult, error) {
	return s.resumeRealtimeWorldWithObservation(
		ctx,
		input.WorldID,
		input.Observation,
		false,
		"administrative_resume",
	)
}

// ResumeRealtimeDiagnosticWorld is the deterministic frozen-clock counterpart
// of ResumeRealtimeWorld. It remains server-only and cannot target production
// profiles.
func (s *CityEconomyService) ResumeRealtimeDiagnosticWorld(
	ctx context.Context,
	input CityRealtimeDiagnosticLifecycleInput,
) (*CityRealtimeLifecycleResult, error) {
	if input.EffectiveUTC.IsZero() {
		return nil, ErrCityInvalidInput
	}
	return s.resumeRealtimeWorldWithObservation(
		ctx,
		input.WorldID,
		cityRealtimeDiagnosticClockObservation(input.EffectiveUTC),
		true,
		"diagnostic_resume",
	)
}

func (s *CityEconomyService) resumeRealtimeWorldWithObservation(
	ctx context.Context,
	worldID int64,
	observation CityRealtimeClockObservation,
	requireDiagnosticProfile bool,
	reason string,
) (*CityRealtimeLifecycleResult, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city realtime resume transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(worldID)); err != nil {
		return nil, fmt.Errorf("lock city realtime resume world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, 0, worldID)
	if err != nil {
		return nil, err
	}
	if !cityEngineIsRealtime(world.simulationVersion) {
		return nil, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": world.simulationVersion})
	}
	if world.stateHash == nil {
		return nil, ErrCityWorldUnavailable.WithMetadata(map[string]string{"status": world.status})
	}
	state, err := lockCityRealtimeState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if world.status == CityWorldStatusRunning && state.lifecycleStatus == CityWorldStatusRunning {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit existing city realtime resume: %w", err)
		}
		return cityRealtimeLifecycleResultFromState(worldID, false, state, nil), nil
	}
	if world.status != CityWorldStatusPaused || state.lifecycleStatus != CityWorldStatusPaused {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_lifecycle_status"})
	}
	if err = requireCityRealtimeLifecycleProfile(state, requireDiagnosticProfile); err != nil {
		return nil, err
	}
	if !requireDiagnosticProfile &&
		state.clockState != cityRealtimeClockStateInitializing &&
		state.clockState != cityRealtimeClockStateHealthy {
		// A paused world must never use resume to bypass the canonical
		// unsafe/recovery path. Only a currently healthy authority can reopen
		// a normal segment; held/recovering states remain scheduler-owned.
		return nil, ErrCityRealtimeClockUnsafe
	}
	effectiveUTC, err := validateCityRealtimeClockObservation(state, observation)
	if err != nil {
		return nil, err
	}
	if effectiveUTC.Before(state.lastCommittedEffectiveUTC) {
		return nil, ErrCityRealtimeClockUnsafe.WithMetadata(map[string]string{
			"last_committed_effective_utc": state.lastCommittedEffectiveUTC.Format(time.RFC3339Nano),
		})
	}
	if err = validateCityRealtimeClockObservationDatabaseSkew(ctx, tx, state, effectiveUTC); err != nil {
		return nil, err
	}
	if err = recordCityRealtimeClockObservation(ctx, tx, observation); err != nil {
		return nil, err
	}
	var closedAt sql.NullTime
	if err = tx.QueryRowContext(ctx, `
SELECT closed_at
FROM city_world_clock_segments
WHERE world_id = $1 AND id = $2`, worldID, state.clockSegmentID).Scan(&closedAt); err != nil {
		return nil, fmt.Errorf("load city realtime paused clock segment: %w", err)
	}
	if !closedAt.Valid {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_clock_segment"})
	}
	if state.clockSegmentSequence == cityRealtimeMaximumTimelineSequence ||
		state.timelineFrameSequence == cityRealtimeMaximumTimelineSequence {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_resume"})
	}
	segmentSequence := state.clockSegmentSequence + 1
	var segmentID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_world_clock_segments
    (world_id, segment_sequence, clock_profile_id, clock_profile_hash,
     source_clock_mode, effective_utc_anchor, world_elapsed_anchor_us,
     uncertainty_us, reason, monotonic_anchor_proof)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'resume', NULL)
RETURNING id`,
		worldID, segmentSequence, state.clockProfileID, state.clockProfileHash,
		state.sourceClockMode, effectiveUTC, state.currentWorldTimeUS,
		observation.UncertaintyUS,
	).Scan(&segmentID); err != nil {
		return nil, fmt.Errorf("create city realtime resume clock segment: %w", err)
	}
	frameSequence := state.timelineFrameSequence + 1
	cursor, err := cityRealtimeTimelineCursor(frameSequence)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_worlds
SET status = $2, updated_at = NOW()
WHERE id = $1`, worldID, CityWorldStatusRunning); err != nil {
		return nil, fmt.Errorf("resume city realtime world: %w", err)
	}
	if err = updateCityRealtimeLifecycleStateForFrame(
		ctx, tx, worldID, CityWorldStatusRunning, segmentID,
		state.currentWorldTimeUS, effectiveUTC, frameSequence, cursor,
		state.nextDueAtWorldTimeUS, nil,
		cityRealtimeClockStateHealthy, cityRealtimeRecoveryStateIdle,
	); err != nil {
		return nil, err
	}
	_, _, stateHash, err := canonicalCityWorldState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2, updated_at = NOW() WHERE id = $1`, worldID, stateHash); err != nil {
		return nil, fmt.Errorf("store city realtime resume state hash: %w", err)
	}
	frame, err := insertCityRealtimeFrame(ctx, tx, cityRealtimeFrameInsertInput{
		WorldID:               worldID,
		TemporalEngineVersion: state.temporalEngineVersion,
		FrameSequence:         frameSequence,
		TimelineCursor:        cursor,
		WorldTimeFromUS:       state.currentWorldTimeUS,
		WorldTimeToUS:         state.currentWorldTimeUS,
		ClockSegmentID:        segmentID,
		ClockSegmentSequence:  segmentSequence,
		ClockProfileHash:      state.clockProfileHash,
		FrameKind:             "lifecycle",
		EffectiveUTCFrom:      state.lastCommittedEffectiveUTC,
		EffectiveUTCTo:        effectiveUTC,
		PreviousStateHash:     world.stateHash,
		StateHash:             stateHash,
		DueEventDigest:        cityRealtimeEmptyDueEventDigest,
		PhaseSummary: map[string]any{
			"schema_version":         1,
			"frame_kind":             "lifecycle",
			"command_count":          0,
			"due_event_count":        0,
			"reason":                 reason,
			"lifecycle_state_before": CityWorldStatusPaused,
			"lifecycle_state_after":  CityWorldStatusRunning,
			"clock_state_before":     state.clockState,
			"clock_state_after":      cityRealtimeClockStateHealthy,
		},
	})
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city realtime resume: %w", err)
	}
	return &CityRealtimeLifecycleResult{
		WorldID:              worldID,
		Changed:              true,
		LifecycleStatus:      CityWorldStatusRunning,
		ClockState:           cityRealtimeClockStateHealthy,
		RecoveryState:        cityRealtimeRecoveryStateIdle,
		CurrentWorldTimeUS:   state.currentWorldTimeUS,
		TimelineCursor:       cursor,
		ClockSegmentSequence: segmentSequence,
		Frame:                frame,
	}, nil
}

func requireCityRealtimeLifecycleProfile(state *lockedCityRealtimeState, requireDiagnosticProfile bool) error {
	if state == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_time_state"})
	}
	if requireDiagnosticProfile {
		return requireCityRealtimeDiagnosticProfile(state)
	}
	if state.deploymentScope != "production" ||
		(state.sourceClockMode != "system_ntp" &&
			state.sourceClockMode != "system_nts" &&
			state.sourceClockMode != "private_time_service") ||
		state.timeQuantumUS != cityRealtimeTimeQuantumUS {
		return ErrCityManagementRequired.WithMetadata(map[string]string{"field": "realtime_production_profile"})
	}
	return nil
}

func cityRealtimeLifecycleResultFromState(
	worldID int64,
	changed bool,
	state *lockedCityRealtimeState,
	frame *CityTemporalFrame,
) *CityRealtimeLifecycleResult {
	if state == nil {
		return nil
	}
	return &CityRealtimeLifecycleResult{
		WorldID:              worldID,
		Changed:              changed,
		LifecycleStatus:      state.lifecycleStatus,
		ClockState:           state.clockState,
		RecoveryState:        state.recoveryState,
		CurrentWorldTimeUS:   state.currentWorldTimeUS,
		TimelineCursor:       state.timelineCursor,
		ClockSegmentSequence: state.clockSegmentSequence,
		Frame:                frame,
	}
}

func updateCityRealtimeLifecycleStateForFrame(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	lifecycleStatus string,
	clockSegmentID int64,
	currentWorldTimeUS int64,
	lastCommittedEffectiveUTC time.Time,
	frameSequence int64,
	timelineCursor string,
	nextDueAtWorldTimeUS *int64,
	catchupTargetWorldTimeUS *int64,
	clockState string,
	recoveryState string,
) error {
	if _, err := tx.ExecContext(ctx, `
UPDATE city_world_time_states
SET lifecycle_status = $2,
    current_clock_segment_id = $3,
    current_world_time_us = $4,
    last_committed_effective_utc = $5,
    timeline_frame_sequence = $6,
    timeline_cursor = $7,
    next_due_at_world_time_us = $8,
    catchup_target_world_time_us = $9,
    clock_state = $10,
    recovery_state = $11,
    version = version + 1,
    updated_at = NOW()
WHERE world_id = $1`,
		worldID, lifecycleStatus, clockSegmentID, currentWorldTimeUS,
		lastCommittedEffectiveUTC.UTC().Truncate(time.Microsecond),
		frameSequence, timelineCursor, nextDueAtWorldTimeUS,
		catchupTargetWorldTimeUS, clockState, recoveryState,
	); err != nil {
		return fmt.Errorf("update city realtime lifecycle state: %w", err)
	}
	return nil
}
