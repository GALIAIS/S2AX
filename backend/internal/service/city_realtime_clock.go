package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	cityRealtimeClockStateInitializing = "initializing"
	cityRealtimeClockStateHealthy      = "healthy"
	cityRealtimeClockStateDegraded     = "degraded"
	cityRealtimeClockStateUnsafe       = "unsafe"
	cityRealtimeClockStateRecovering   = "recovering"

	cityRealtimeRecoveryStateIdle       = "idle"
	cityRealtimeRecoveryStateCatchingUp = "catching_up"
	cityRealtimeRecoveryStateHeld       = "held"
)

// CityRealtimeClockProfile is the immutable profile snapshot handed to a
// server-owned Clock Authority. It intentionally contains no world member or
// client data.
type CityRealtimeClockProfile struct {
	ID                    string
	Hash                  string
	SourceClockMode       string
	DeploymentScope       string
	TimeQuantumUS         int64
	MaximumUncertaintyUS  int64
	MaximumDatabaseSkewUS int64
}

// CityRealtimeClockObservation is obtained from a server-owned time source,
// never from an HTTP request. A healthy observation is required before a
// production scheduler may commit a due-event frame.
type CityRealtimeClockObservation struct {
	NodeID          string
	SourceClockMode string
	HealthState     string
	EffectiveUTC    time.Time
	UncertaintyUS   int64
}

// CityRealtimeClockAuthority is the narrow production seam for NTP/NTS or a
// private time service. The repository deliberately ships without an
// authority that claims a host wall clock is verified.
type CityRealtimeClockAuthority interface {
	Observe(ctx context.Context, profile CityRealtimeClockProfile) (CityRealtimeClockObservation, error)
}

type cityRealtimeDenyClockAuthority struct{}

func (cityRealtimeDenyClockAuthority) Observe(
	context.Context,
	CityRealtimeClockProfile,
) (CityRealtimeClockObservation, error) {
	return CityRealtimeClockObservation{}, ErrCityRealtimeClockUnsafe
}

func cityRealtimeClockProfileFromState(state *lockedCityRealtimeState) CityRealtimeClockProfile {
	return CityRealtimeClockProfile{
		ID:                    state.clockProfileID,
		Hash:                  state.clockProfileHash,
		SourceClockMode:       state.sourceClockMode,
		DeploymentScope:       state.deploymentScope,
		TimeQuantumUS:         state.timeQuantumUS,
		MaximumUncertaintyUS:  state.maximumUncertaintyUS,
		MaximumDatabaseSkewUS: state.maximumDatabaseSkewUS,
	}
}

func cityRealtimeDiagnosticClockObservation(effectiveUTC time.Time) CityRealtimeClockObservation {
	return CityRealtimeClockObservation{
		NodeID:          "realtime-diagnostic",
		SourceClockMode: cityRealtimeDiagnosticClockMode,
		HealthState:     cityRealtimeClockStateHealthy,
		EffectiveUTC:    effectiveUTC.UTC().Truncate(time.Microsecond),
		UncertaintyUS:   0,
	}
}

func validateCityRealtimeClockObservation(
	state *lockedCityRealtimeState,
	observation CityRealtimeClockObservation,
) (time.Time, error) {
	if state == nil || observation.EffectiveUTC.IsZero() ||
		observation.SourceClockMode != state.sourceClockMode ||
		observation.HealthState != cityRealtimeClockStateHealthy ||
		!cityRealtimeClockNodeIDValid(observation.NodeID) ||
		observation.UncertaintyUS < 0 || observation.UncertaintyUS > state.maximumUncertaintyUS {
		return time.Time{}, ErrCityRealtimeClockUnsafe
	}
	return observation.EffectiveUTC.UTC().Truncate(time.Microsecond), nil
}

func validateCityRealtimeClockObservationDatabaseSkew(
	ctx context.Context,
	tx *sql.Tx,
	state *lockedCityRealtimeState,
	effectiveUTC time.Time,
) error {
	var databaseUTC time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&databaseUTC); err != nil {
		return fmt.Errorf("read city realtime database clock: %w", err)
	}
	databaseUTC = databaseUTC.UTC().Truncate(time.Microsecond)
	skewUS := effectiveUTC.UnixMicro() - databaseUTC.UnixMicro()
	if skewUS < 0 {
		skewUS = -skewUS
	}
	if skewUS > state.maximumDatabaseSkewUS {
		return ErrCityRealtimeClockUnsafe.WithMetadata(map[string]string{
			"maximum_database_skew_us": fmt.Sprintf("%d", state.maximumDatabaseSkewUS),
		})
	}
	return nil
}

func recordCityRealtimeClockObservation(
	ctx context.Context,
	tx *sql.Tx,
	observation CityRealtimeClockObservation,
) error {
	result, err := tx.ExecContext(ctx, `
INSERT INTO city_clock_nodes
    (node_id, source_clock_mode, health_state, offset_estimate_us,
     uncertainty_us, last_sync_at, observed_at, metadata)
VALUES ($1, $2, 'healthy', NULL, $3, $4, NOW(),
        '{"schema_version":1,"writer":"city_realtime_kernel"}'::jsonb)
ON CONFLICT (node_id) DO UPDATE
SET health_state = EXCLUDED.health_state,
    offset_estimate_us = EXCLUDED.offset_estimate_us,
    uncertainty_us = EXCLUDED.uncertainty_us,
    last_sync_at = EXCLUDED.last_sync_at,
    observed_at = EXCLUDED.observed_at,
    metadata = EXCLUDED.metadata
WHERE city_clock_nodes.source_clock_mode = EXCLUDED.source_clock_mode`,
		observation.NodeID, observation.SourceClockMode, observation.UncertaintyUS,
		observation.EffectiveUTC.UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		return fmt.Errorf("record city realtime clock observation: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check city realtime clock observation: %w", err)
	}
	if rowsAffected != 1 {
		return ErrCityRealtimeClockUnsafe.WithMetadata(map[string]string{"field": "clock_node_source"})
	}
	return nil
}

func cityRealtimeClockNodeIDValid(value string) bool {
	if len(value) < 2 || len(value) > 128 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '.' || character == '-' {
			continue
		}
		return false
	}
	return true
}

// recordRealtimeClockUnsafe commits the operationally observed loss of a
// production time source as a zero-duration lifecycle frame. It stores only a
// stable error code; raw provider failures stay in the scheduler's restricted
// operational state and never enter member-visible timeline data.
func (s *CityEconomyService) recordRealtimeClockUnsafe(
	ctx context.Context,
	worldID int64,
	errorCode string,
) (bool, error) {
	if !IsCitySystemAdministrator(ctx) {
		return false, ErrCityManagementRequired
	}
	if worldID <= 0 || errorCode != "CLOCK_UNSAFE" {
		return false, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin city realtime clock hold transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(worldID)); err != nil {
		return false, fmt.Errorf("lock city realtime clock hold world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, 0, worldID)
	if err != nil {
		return false, err
	}
	if !cityEngineIsRealtime(world.simulationVersion) {
		return false, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": world.simulationVersion})
	}
	if world.status != CityWorldStatusRunning || world.stateHash == nil {
		return false, ErrCityWorldUnavailable.WithMetadata(map[string]string{"status": world.status})
	}
	state, err := lockCityRealtimeState(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if state.deploymentScope != "production" || state.lifecycleStatus != CityWorldStatusRunning {
		return false, ErrCityRealtimeClockUnsafe
	}
	if state.clockState == cityRealtimeClockStateUnsafe {
		if err = tx.Commit(); err != nil {
			return false, fmt.Errorf("commit city realtime existing clock hold: %w", err)
		}
		return false, nil
	}
	if state.timelineFrameSequence == cityRealtimeMaximumTimelineSequence {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_timeline"})
	}
	frameSequence := state.timelineFrameSequence + 1
	cursor, err := cityRealtimeTimelineCursor(frameSequence)
	if err != nil {
		return false, err
	}
	if err = updateCityRealtimeTimeStateForFrame(
		ctx, tx, worldID, state.currentWorldTimeUS, state.lastCommittedEffectiveUTC,
		frameSequence, cursor, state.nextDueAtWorldTimeUS, nil,
		cityRealtimeClockStateUnsafe, cityRealtimeRecoveryStateHeld,
	); err != nil {
		return false, err
	}
	_, _, stateHash, err := canonicalCityWorldState(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2, updated_at = NOW() WHERE id = $1`, worldID, stateHash); err != nil {
		return false, fmt.Errorf("store city realtime clock hold state hash: %w", err)
	}
	if _, err = insertCityRealtimeFrame(ctx, tx, cityRealtimeFrameInsertInput{
		WorldID:               worldID,
		TemporalEngineVersion: state.temporalEngineVersion,
		FrameSequence:         frameSequence,
		TimelineCursor:        cursor,
		WorldTimeFromUS:       state.currentWorldTimeUS,
		WorldTimeToUS:         state.currentWorldTimeUS,
		ClockSegmentID:        state.clockSegmentID,
		ClockSegmentSequence:  state.clockSegmentSequence,
		ClockProfileHash:      state.clockProfileHash,
		FrameKind:             "lifecycle",
		EffectiveUTCFrom:      state.lastCommittedEffectiveUTC,
		EffectiveUTCTo:        state.lastCommittedEffectiveUTC,
		PreviousStateHash:     world.stateHash,
		StateHash:             stateHash,
		DueEventDigest:        cityRealtimeEmptyDueEventDigest,
		PhaseSummary: map[string]any{
			"schema_version":       1,
			"frame_kind":           "lifecycle",
			"command_count":        0,
			"due_event_count":      0,
			"reason":               "clock_unsafe",
			"clock_error_code":     errorCode,
			"clock_state_before":   state.clockState,
			"clock_state_after":    cityRealtimeClockStateUnsafe,
			"recovery_state_after": cityRealtimeRecoveryStateHeld,
		},
	}); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit city realtime clock hold: %w", err)
	}
	return true, nil
}

// recoverRealtimeClockWithObservation begins a controlled recovery after a
// previously unsafe production clock becomes healthy again. It never leaps the
// world to wall time: it appends a recover segment and persists a bounded
// catch-up target. Due events and the final empty recovery range are then
// committed by the normal temporal reducer in deterministic batches.
func (s *CityEconomyService) recoverRealtimeClockWithObservation(
	ctx context.Context,
	worldID int64,
	observation CityRealtimeClockObservation,
) (bool, error) {
	if !IsCitySystemAdministrator(ctx) {
		return false, ErrCityManagementRequired
	}
	if worldID <= 0 {
		return false, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin city realtime clock recovery transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(worldID)); err != nil {
		return false, fmt.Errorf("lock city realtime clock recovery world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, 0, worldID)
	if err != nil {
		return false, err
	}
	if !cityEngineIsRealtime(world.simulationVersion) {
		return false, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": world.simulationVersion})
	}
	if world.status != CityWorldStatusRunning || world.stateHash == nil {
		return false, ErrCityWorldUnavailable.WithMetadata(map[string]string{"status": world.status})
	}
	state, err := lockCityRealtimeState(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if state.deploymentScope != "production" || state.lifecycleStatus != CityWorldStatusRunning {
		return false, ErrCityRealtimeClockUnsafe
	}
	if state.clockState != cityRealtimeClockStateUnsafe || state.recoveryState != cityRealtimeRecoveryStateHeld {
		if err = tx.Commit(); err != nil {
			return false, fmt.Errorf("commit city realtime clock recovery no-op: %w", err)
		}
		return false, nil
	}
	effectiveUTC, err := validateCityRealtimeClockObservation(state, observation)
	if err != nil {
		return false, err
	}
	if effectiveUTC.Before(state.lastCommittedEffectiveUTC) {
		return false, ErrCityRealtimeClockUnsafe.WithMetadata(map[string]string{
			"last_committed_effective_utc": state.lastCommittedEffectiveUTC.Format(time.RFC3339Nano),
		})
	}
	if err = validateCityRealtimeClockObservationDatabaseSkew(ctx, tx, state, effectiveUTC); err != nil {
		return false, err
	}
	if err = recordCityRealtimeClockObservation(ctx, tx, observation); err != nil {
		return false, err
	}
	deltaUS := effectiveUTC.UnixMicro() - state.lastCommittedEffectiveUTC.UnixMicro()
	quantizedDeltaUS := (deltaUS / state.timeQuantumUS) * state.timeQuantumUS
	if state.currentWorldTimeUS > cityRealtimeMaximumWorldTimeUS-quantizedDeltaUS ||
		state.timelineFrameSequence == cityRealtimeMaximumTimelineSequence {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_recovery_target"})
	}
	targetWorldTimeUS := state.currentWorldTimeUS + quantizedDeltaUS
	closeResult, err := tx.ExecContext(ctx, `
UPDATE city_world_clock_segments
SET closed_at = NOW(), close_reason = 'recover'
WHERE world_id = $1 AND id = $2 AND closed_at IS NULL`, worldID, state.clockSegmentID)
	if err != nil {
		return false, fmt.Errorf("close city realtime clock segment for recovery: %w", err)
	}
	closedRows, err := closeResult.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check city realtime clock segment recovery close: %w", err)
	}
	if closedRows != 1 {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_clock_segment"})
	}
	var segmentID int64
	segmentSequence := state.clockSegmentSequence + 1
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_world_clock_segments
    (world_id, segment_sequence, clock_profile_id, clock_profile_hash,
     source_clock_mode, effective_utc_anchor, world_elapsed_anchor_us,
     uncertainty_us, reason, monotonic_anchor_proof)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'recover', NULL)
RETURNING id`,
		worldID, segmentSequence, state.clockProfileID, state.clockProfileHash,
		state.sourceClockMode, state.lastCommittedEffectiveUTC, state.currentWorldTimeUS,
		observation.UncertaintyUS,
	).Scan(&segmentID); err != nil {
		return false, fmt.Errorf("create city realtime recovery clock segment: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_world_time_states
SET current_clock_segment_id = $2, updated_at = NOW()
WHERE world_id = $1`, worldID, segmentID); err != nil {
		return false, fmt.Errorf("attach city realtime recovery clock segment: %w", err)
	}
	frameSequence := state.timelineFrameSequence + 1
	cursor, err := cityRealtimeTimelineCursor(frameSequence)
	if err != nil {
		return false, err
	}
	if err = updateCityRealtimeTimeStateForFrame(
		ctx, tx, worldID, state.currentWorldTimeUS, state.lastCommittedEffectiveUTC,
		frameSequence, cursor,
		state.nextDueAtWorldTimeUS, &targetWorldTimeUS,
		cityRealtimeClockStateRecovering, cityRealtimeRecoveryStateCatchingUp,
	); err != nil {
		return false, err
	}
	_, _, stateHash, err := canonicalCityWorldState(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2, updated_at = NOW() WHERE id = $1`, worldID, stateHash); err != nil {
		return false, fmt.Errorf("store city realtime recovery state hash: %w", err)
	}
	if _, err = insertCityRealtimeFrame(ctx, tx, cityRealtimeFrameInsertInput{
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
		EffectiveUTCTo:        state.lastCommittedEffectiveUTC,
		PreviousStateHash:     world.stateHash,
		StateHash:             stateHash,
		DueEventDigest:        cityRealtimeEmptyDueEventDigest,
		PhaseSummary: map[string]any{
			"schema_version":               1,
			"frame_kind":                   "lifecycle",
			"command_count":                0,
			"due_event_count":              0,
			"reason":                       "clock_recovery_started",
			"clock_state_before":           state.clockState,
			"clock_state_after":            cityRealtimeClockStateRecovering,
			"recovery_state_after":         cityRealtimeRecoveryStateCatchingUp,
			"catchup_target_world_time_us": targetWorldTimeUS,
		},
	}); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit city realtime clock recovery: %w", err)
	}
	return true, nil
}
