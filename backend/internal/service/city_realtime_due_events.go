package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	cityRealtimeDueEventTypeDiagnostic = "system.realtime.diagnostic"
	// cityRealtimeDueEventTypeNoop is a canonical server-owned barrier. It
	// intentionally has no domain side effect, but gives the production clock
	// path a safe, replayable event when an authority needs to establish or
	// recover temporal progress before richer reducers are attached.
	cityRealtimeDueEventTypeNoop = "system.realtime.noop"

	cityRealtimeDueEventStatusPending    = "pending"
	cityRealtimeDueEventStatusLeased     = "leased"
	cityRealtimeDueEventStatusApplied    = "applied"
	cityRealtimeDueEventStatusCancelled  = "cancelled"
	cityRealtimeDueEventStatusRejected   = "rejected"
	cityRealtimeDueEventStatusDeadLetter = "dead_letter"

	cityRealtimeDueEventPhasePostSchedule = "post_schedule"
	cityRealtimeDueEventSourceKindSystem  = "system"
)

// CityRealtimeDiagnosticDueEventScheduleInput is an internal-only test and
// diagnostic event source. There is deliberately no HTTP handler for it. The
// production scheduler will receive due events only from server-owned command
// and system reducers after Clock Authority is available.
type CityRealtimeDiagnosticDueEventScheduleInput struct {
	WorldID         int64
	EventType       string
	SchemaVersion   int
	DueWorldTimeUS  int64
	TemporalPhase   string
	Priority        int
	AggregateType   string
	AggregateKey    string
	DedupKey        string
	SourceReference string
	Payload         map[string]any
	ExpectedVersion *int64
}

// CityRealtimeSystemDueEventScheduleInput is the server-owned production
// scheduling boundary. It deliberately has no HTTP handler: only trusted
// reducers, the scheduler bootstrap, and future command processors may append
// canonical due facts through a server-marked administrator context.
//
// Unknown event types are retained and terminally rejected by the reducer
// instead of being executed implicitly. That makes rollout/replay behavior
// fail closed while allowing future immutable event schemas to be introduced.
type CityRealtimeSystemDueEventScheduleInput struct {
	WorldID         int64
	EventType       string
	SchemaVersion   int
	DueWorldTimeUS  int64
	TemporalPhase   string
	Priority        int
	AggregateType   string
	AggregateKey    string
	DedupKey        string
	SourceReference string
	Payload         map[string]any
	ExpectedVersion *int64
}

type CityRealtimeDiagnosticDueEventScheduleResult struct {
	EventID            int64              `json:"event_id"`
	DueWorldTimeUS     int64              `json:"due_world_time_us"`
	Status             string             `json:"status"`
	CurrentWorldTimeUS int64              `json:"current_world_time_us"`
	TimelineCursor     string             `json:"timeline_cursor"`
	Frame              *CityTemporalFrame `json:"frame"`
}

// CityRealtimeDiagnosticDueEventProcessInput is internal-only. EffectiveUTC
// is accepted exclusively under the frozen diagnostic profile and a
// server-marked administrator context; a browser can never advance time.
type CityRealtimeDiagnosticDueEventProcessInput struct {
	WorldID      int64
	EffectiveUTC time.Time
}

type CityRealtimeDiagnosticDueEventProcessResult struct {
	Resolved           bool               `json:"resolved"`
	AppliedCount       int                `json:"applied_count"`
	RejectedCount      int                `json:"rejected_count"`
	CurrentWorldTimeUS int64              `json:"current_world_time_us"`
	TimelineCursor     string             `json:"timeline_cursor"`
	Frame              *CityTemporalFrame `json:"frame,omitempty"`
}

// cityRealtimeDueEventHash contains every domain-relevant field of the due
// queue while deliberately excluding DB identifiers, leases, retry times and
// created/updated timestamps. That keeps canonical hashes replayable across
// workers and deployments.
type cityRealtimeDueEventHash struct {
	EventType             string          `json:"event_type"`
	SchemaVersion         int             `json:"schema_version"`
	DueWorldTimeUS        int64           `json:"due_world_time_us"`
	TemporalPhase         string          `json:"temporal_phase"`
	Priority              int             `json:"priority"`
	AggregateType         string          `json:"aggregate_type"`
	AggregateKey          string          `json:"aggregate_key"`
	DedupKey              string          `json:"dedup_key"`
	SourceKind            string          `json:"source_kind"`
	SourceReference       string          `json:"source_reference"`
	Payload               json.RawMessage `json:"payload"`
	PayloadHash           string          `json:"payload_hash"`
	ExpectedVersion       *int64          `json:"expected_version,omitempty"`
	Status                string          `json:"status"`
	CreatedFrameSequence  *int64          `json:"created_frame_sequence,omitempty"`
	ResolvedFrameSequence *int64          `json:"resolved_frame_sequence,omitempty"`
}

type cityRealtimeDueEventRecord struct {
	id int64
	cityRealtimeDueEventHash
}

type normalizedCityRealtimeDiagnosticDueEvent struct {
	cityRealtimeDueEventHash
}

type cityRealtimeFrameInsertInput struct {
	WorldID               int64
	TemporalEngineVersion string
	FrameSequence         int64
	TimelineCursor        string
	WorldTimeFromUS       int64
	WorldTimeToUS         int64
	ClockSegmentID        int64
	ClockSegmentSequence  int64
	ClockProfileHash      string
	FrameKind             string
	EffectiveUTCFrom      time.Time
	EffectiveUTCTo        time.Time
	PreviousStateHash     *string
	StateHash             string
	DueEventDigest        string
	PhaseSummary          map[string]any
}

// ScheduleRealtimeDiagnosticDueEvent appends a same-time diagnostic frame
// which owns the event's creation fact. The deferred FK in migration 252 is
// intentional: the queue row and its immutable frame must commit atomically.
func (s *CityEconomyService) ScheduleRealtimeDiagnosticDueEvent(
	ctx context.Context,
	input CityRealtimeDiagnosticDueEventScheduleInput,
) (*CityRealtimeDiagnosticDueEventScheduleResult, error) {
	normalized, err := normalizeCityRealtimeDiagnosticDueEvent(input)
	if err != nil {
		return nil, err
	}
	return s.scheduleRealtimeDueEvent(ctx, input.WorldID, normalized, cityRealtimeDueEventScheduleScopeDiagnostic)
}

// ScheduleRealtimeSystemDueEvent appends a production due-event creation fact
// under the same lock, hash, and immutable-frame discipline as diagnostics.
// It is intentionally server-only; it never accepts a client clock or a
// caller-selected source kind.
func (s *CityEconomyService) ScheduleRealtimeSystemDueEvent(
	ctx context.Context,
	input CityRealtimeSystemDueEventScheduleInput,
) (*CityRealtimeDiagnosticDueEventScheduleResult, error) {
	normalized, err := normalizeCityRealtimeSystemDueEvent(input)
	if err != nil {
		return nil, err
	}
	return s.scheduleRealtimeDueEvent(ctx, input.WorldID, normalized, cityRealtimeDueEventScheduleScopeProduction)
}

type cityRealtimeDueEventScheduleScope string

const (
	cityRealtimeDueEventScheduleScopeDiagnostic cityRealtimeDueEventScheduleScope = "diagnostic"
	cityRealtimeDueEventScheduleScopeProduction cityRealtimeDueEventScheduleScope = "production"
)

func (s *CityEconomyService) scheduleRealtimeDueEvent(
	ctx context.Context,
	worldID int64,
	normalized normalizedCityRealtimeDiagnosticDueEvent,
	scope cityRealtimeDueEventScheduleScope,
) (*CityRealtimeDiagnosticDueEventScheduleResult, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city realtime due-event schedule transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(worldID)); err != nil {
		return nil, fmt.Errorf("lock city realtime due-event world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, 0, worldID)
	if err != nil {
		return nil, err
	}
	if !cityEngineIsRealtime(world.simulationVersion) {
		return nil, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": world.simulationVersion})
	}
	if world.status != CityWorldStatusRunning || world.stateHash == nil {
		return nil, ErrCityWorldUnavailable.WithMetadata(map[string]string{"status": world.status})
	}
	state, err := lockCityRealtimeState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	frameKind := ""
	frameReason := ""
	switch scope {
	case cityRealtimeDueEventScheduleScopeDiagnostic:
		if err = requireCityRealtimeDiagnosticState(state); err != nil {
			return nil, err
		}
		frameKind = "diagnostic"
		frameReason = "frozen_test_clock"
	case cityRealtimeDueEventScheduleScopeProduction:
		if state.deploymentScope != "production" || state.lifecycleStatus != CityWorldStatusRunning {
			return nil, ErrCityManagementRequired.WithMetadata(map[string]string{"field": "realtime_production_profile"})
		}
		if state.clockState != cityRealtimeClockStateInitializing && state.clockState != cityRealtimeClockStateHealthy {
			return nil, ErrCityRealtimeClockUnsafe
		}
		frameKind = "lifecycle"
		frameReason = "system_schedule"
	default:
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_due_event_scope"})
	}
	if normalized.DueWorldTimeUS < state.currentWorldTimeUS {
		return nil, ErrCityRealtimeClockUnsafe.WithMetadata(map[string]string{
			"current_world_time_us": fmt.Sprintf("%d", state.currentWorldTimeUS),
		})
	}
	if state.timelineFrameSequence == cityRealtimeMaximumTimelineSequence {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_timeline"})
	}
	frameSequence := state.timelineFrameSequence + 1
	cursor, err := cityRealtimeTimelineCursor(frameSequence)
	if err != nil {
		return nil, err
	}
	createdFrameSequence := frameSequence
	normalized.CreatedFrameSequence = &createdFrameSequence
	normalized.Status = cityRealtimeDueEventStatusPending

	var eventID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_due_events
    (world_id, event_type, schema_version, due_world_time_us, temporal_phase,
     priority, aggregate_type, aggregate_key, dedup_key, source_kind,
     source_reference, payload, payload_hash, expected_version, status,
     created_frame_sequence)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
        $11, $12::jsonb, $13, $14, $15, $16)
ON CONFLICT (world_id, dedup_key) DO NOTHING
RETURNING id`,
		worldID, normalized.EventType, normalized.SchemaVersion,
		normalized.DueWorldTimeUS, normalized.TemporalPhase, normalized.Priority,
		normalized.AggregateType, normalized.AggregateKey, normalized.DedupKey,
		normalized.SourceKind, normalized.SourceReference, []byte(normalized.Payload),
		normalized.PayloadHash, normalized.ExpectedVersion, normalized.Status,
		normalized.CreatedFrameSequence,
	).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityRealtimeDueEventConflict.WithMetadata(map[string]string{"dedup_key": normalized.DedupKey})
	}
	if err != nil {
		return nil, fmt.Errorf("create city realtime due event: %w", err)
	}

	nextDueAtWorldTimeUS, err := cityRealtimeNextPendingDue(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if err = updateCityRealtimeTimeStateForFrame(
		ctx, tx, worldID, state.currentWorldTimeUS, state.lastCommittedEffectiveUTC,
		frameSequence, cursor, nextDueAtWorldTimeUS, state.catchupTargetWorldTimeUS,
		state.clockState, state.recoveryState,
	); err != nil {
		return nil, err
	}
	_, _, stateHash, err := canonicalCityWorldState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2, updated_at = NOW() WHERE id = $1`, worldID, stateHash); err != nil {
		return nil, fmt.Errorf("store city realtime due-event schedule state hash: %w", err)
	}
	dueEventDigest, err := cityRealtimeDueEventDigest([]cityRealtimeDueEventRecord{{
		id: eventID, cityRealtimeDueEventHash: normalized.cityRealtimeDueEventHash,
	}})
	if err != nil {
		return nil, err
	}
	frame, err := insertCityRealtimeFrame(ctx, tx, cityRealtimeFrameInsertInput{
		WorldID:               worldID,
		TemporalEngineVersion: state.temporalEngineVersion,
		FrameSequence:         frameSequence,
		TimelineCursor:        cursor,
		WorldTimeFromUS:       state.currentWorldTimeUS,
		WorldTimeToUS:         state.currentWorldTimeUS,
		ClockSegmentID:        state.clockSegmentID,
		ClockSegmentSequence:  state.clockSegmentSequence,
		ClockProfileHash:      state.clockProfileHash,
		FrameKind:             frameKind,
		EffectiveUTCFrom:      state.lastCommittedEffectiveUTC,
		EffectiveUTCTo:        state.lastCommittedEffectiveUTC,
		PreviousStateHash:     world.stateHash,
		StateHash:             stateHash,
		DueEventDigest:        dueEventDigest,
		PhaseSummary: map[string]any{
			"schema_version":            1,
			"frame_kind":                frameKind,
			"command_count":             0,
			"due_event_count":           0,
			"scheduled_due_event_count": 1,
			"reason":                    frameReason,
		},
	})
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city realtime due-event schedule: %w", err)
	}
	return &CityRealtimeDiagnosticDueEventScheduleResult{
		EventID:            eventID,
		DueWorldTimeUS:     normalized.DueWorldTimeUS,
		Status:             normalized.Status,
		CurrentWorldTimeUS: state.currentWorldTimeUS,
		TimelineCursor:     cursor,
		Frame:              frame,
	}, nil
}

// ProcessRealtimeDiagnosticDueEvents resolves exactly the earliest due batch
// visible to the supplied server-owned diagnostic time. If no due batch is
// ready it intentionally writes no frame: empty wall-clock intervals are not
// canonical events.
func (s *CityEconomyService) ProcessRealtimeDiagnosticDueEvents(
	ctx context.Context,
	input CityRealtimeDiagnosticDueEventProcessInput,
) (*CityRealtimeDiagnosticDueEventProcessResult, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if input.WorldID <= 0 || input.EffectiveUTC.IsZero() {
		return nil, ErrCityInvalidInput
	}
	return s.processRealtimeDueEventsWithClock(
		ctx,
		input.WorldID,
		cityRealtimeDiagnosticClockObservation(input.EffectiveUTC),
		true,
		"frozen_test_clock",
	)
}

// processRealtimeDueEventsWithClock is the single server-side reducer for a
// temporal due batch. It is intentionally unexported: only the diagnostic
// wrapper and the future lease-owning scheduler may supply an observation.
func (s *CityEconomyService) processRealtimeDueEventsWithClock(
	ctx context.Context,
	worldID int64,
	observation CityRealtimeClockObservation,
	requireDiagnosticProfile bool,
	frameReason string,
) (*CityRealtimeDiagnosticDueEventProcessResult, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city realtime due-event transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(worldID)); err != nil {
		return nil, fmt.Errorf("lock city realtime due-event world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, 0, worldID)
	if err != nil {
		return nil, err
	}
	if !cityEngineIsRealtime(world.simulationVersion) {
		return nil, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": world.simulationVersion})
	}
	if world.status != CityWorldStatusRunning || world.stateHash == nil {
		return nil, ErrCityWorldUnavailable.WithMetadata(map[string]string{"status": world.status})
	}
	state, err := lockCityRealtimeState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if requireDiagnosticProfile {
		if err = requireCityRealtimeDiagnosticState(state); err != nil {
			return nil, err
		}
	} else {
		if state.lifecycleStatus != CityWorldStatusRunning ||
			(state.clockState != cityRealtimeClockStateInitializing &&
				state.clockState != cityRealtimeClockStateHealthy &&
				state.clockState != cityRealtimeClockStateRecovering) {
			return nil, ErrCityRealtimeClockUnsafe
		}
		if state.clockState == cityRealtimeClockStateRecovering &&
			(state.recoveryState != cityRealtimeRecoveryStateCatchingUp || state.catchupTargetWorldTimeUS == nil) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_recovery_state"})
		}
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
	deltaUS := effectiveUTC.UnixMicro() - state.lastCommittedEffectiveUTC.UnixMicro()
	if deltaUS < 0 {
		return nil, ErrCityRealtimeClockUnsafe
	}
	quantizedDeltaUS := (deltaUS / state.timeQuantumUS) * state.timeQuantumUS
	if state.currentWorldTimeUS > cityRealtimeMaximumWorldTimeUS-quantizedDeltaUS {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_timeline"})
	}
	targetWorldTimeUS := state.currentWorldTimeUS + quantizedDeltaUS
	recoveryMode := !requireDiagnosticProfile && state.clockState == cityRealtimeClockStateRecovering
	if recoveryMode && targetWorldTimeUS > *state.catchupTargetWorldTimeUS {
		targetWorldTimeUS = *state.catchupTargetWorldTimeUS
	}
	result := &CityRealtimeDiagnosticDueEventProcessResult{
		CurrentWorldTimeUS: state.currentWorldTimeUS,
		TimelineCursor:     state.timelineCursor,
	}
	nextDueAtWorldTimeUS, err := cityRealtimeFirstPendingDueAtOrBefore(ctx, tx, worldID, targetWorldTimeUS)
	if err != nil {
		return nil, err
	}
	if nextDueAtWorldTimeUS == nil {
		if recoveryMode {
			if err = validateCityRealtimeClockObservationDatabaseSkew(ctx, tx, state, effectiveUTC); err != nil {
				return nil, err
			}
			if err = recordCityRealtimeClockObservation(ctx, tx, observation); err != nil {
				return nil, err
			}
			if err = completeCityRealtimeClockRecovery(ctx, tx, worldID, world, state, targetWorldTimeUS, effectiveUTC, result); err != nil {
				return nil, err
			}
			if err = tx.Commit(); err != nil {
				return nil, fmt.Errorf("commit city realtime clock recovery completion: %w", err)
			}
			return result, nil
		}
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit city realtime due-event no-op: %w", err)
		}
		return result, nil
	}
	if *nextDueAtWorldTimeUS < state.currentWorldTimeUS || *nextDueAtWorldTimeUS%state.timeQuantumUS != 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_due_event_time"})
	}
	if state.timelineFrameSequence == cityRealtimeMaximumTimelineSequence {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_timeline"})
	}
	if err = validateCityRealtimeClockObservationDatabaseSkew(ctx, tx, state, effectiveUTC); err != nil {
		return nil, err
	}
	if err = recordCityRealtimeClockObservation(ctx, tx, observation); err != nil {
		return nil, err
	}
	events, err := lockCityRealtimePendingDueEventsAt(ctx, tx, worldID, *nextDueAtWorldTimeUS)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_due_event_batch"})
	}
	frameSequence := state.timelineFrameSequence + 1
	cursor, err := cityRealtimeTimelineCursor(frameSequence)
	if err != nil {
		return nil, err
	}
	resolvedFrameSequence := frameSequence
	appliedCount := 0
	rejectedCount := 0
	actorPatrolCount := 0
	characterMetabolismCount := 0
	caseReviewClosureCount := 0
	agentIntentAppliedCount := 0
	agentIntentRejectedCount := 0
	agentWakeupAppliedCount := 0
	agentWakeupRejectedCount := 0
	for index := range events {
		status := cityRealtimeDueEventStatusRejected
		if (events[index].EventType == cityRealtimeDueEventTypeDiagnostic ||
			events[index].EventType == cityRealtimeDueEventTypeNoop) &&
			events[index].SchemaVersion == 1 &&
			events[index].SourceKind == cityRealtimeDueEventSourceKindSystem {
			status = cityRealtimeDueEventStatusApplied
			appliedCount++
		} else if cityEngineSupportsRealtimeStaticWorldgen(world.simulationVersion) {
			handled, applied, applyErr := applyCityRealtimeAgentDecisionWakeupDueEvent(
				ctx, tx, worldID, frameSequence, events[index],
			)
			if applyErr != nil {
				return nil, applyErr
			}
			if handled {
				if applied {
					status = cityRealtimeDueEventStatusApplied
					appliedCount++
					agentWakeupAppliedCount++
				} else {
					rejectedCount++
					agentWakeupRejectedCount++
				}
			} else {
				handled, applied, applyErr = applyCityRealtimeAgentIntentDueEvent(
					ctx, tx, worldID, frameSequence, events[index],
				)
				if applyErr != nil {
					return nil, applyErr
				}
				if handled {
					if applied {
						status = cityRealtimeDueEventStatusApplied
						appliedCount++
						agentIntentAppliedCount++
					} else {
						rejectedCount++
						agentIntentRejectedCount++
					}
				} else {
					applied, applyErr = applyCityRealtimeCharacterCaseReviewCloseDueEvent(
						ctx, tx, worldID, frameSequence, events[index],
					)
					if applyErr != nil {
						return nil, applyErr
					}
					if applied {
						status = cityRealtimeDueEventStatusApplied
						appliedCount++
						caseReviewClosureCount++
					} else {
						applied, applyErr = applyCityRealtimeCharacterMetabolismDueEvent(
							ctx, tx, worldID, frameSequence, events[index],
						)
						if applyErr != nil {
							return nil, applyErr
						}
						if applied {
							status = cityRealtimeDueEventStatusApplied
							appliedCount++
							characterMetabolismCount++
						} else {
							applied, applyErr = applyCityRealtimeActorPatrolDueEvent(
								ctx, tx, worldID, frameSequence, events[index],
							)
							if applyErr != nil {
								return nil, applyErr
							}
							if applied {
								status = cityRealtimeDueEventStatusApplied
								appliedCount++
								actorPatrolCount++
							} else {
								rejectedCount++
							}
						}
					}
				}
			}
		} else {
			rejectedCount++
		}
		resultUpdate, updateErr := tx.ExecContext(ctx, `
UPDATE city_due_events
SET status = $3,
    lease_token = NULL,
    lease_expires_at = NULL,
    resolved_frame_sequence = $4,
    updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'pending'`,
			worldID, events[index].id, status, resolvedFrameSequence,
		)
		if updateErr != nil {
			return nil, fmt.Errorf("resolve city realtime due event: %w", updateErr)
		}
		rowsAffected, updateErr := resultUpdate.RowsAffected()
		if updateErr != nil {
			return nil, fmt.Errorf("check city realtime due event resolution: %w", updateErr)
		}
		if rowsAffected != 1 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_due_event_status"})
		}
		events[index].Status = status
		events[index].ResolvedFrameSequence = &resolvedFrameSequence
	}
	targetEffectiveUTC := cityRealtimeAddMicroseconds(
		state.lastCommittedEffectiveUTC,
		*nextDueAtWorldTimeUS-state.currentWorldTimeUS,
	)
	if targetEffectiveUTC.After(effectiveUTC) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_due_event_clock"})
	}
	nextPendingDueAtWorldTimeUS, err := cityRealtimeNextPendingDue(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	nextClockState := cityRealtimeClockStateHealthy
	nextRecoveryState := cityRealtimeRecoveryStateIdle
	var nextCatchupTargetWorldTimeUS *int64
	frameKind := "due_event"
	resolvedReason := frameReason
	if recoveryMode {
		nextClockState = cityRealtimeClockStateRecovering
		nextRecoveryState = cityRealtimeRecoveryStateCatchingUp
		nextCatchupTargetWorldTimeUS = state.catchupTargetWorldTimeUS
		frameKind = "recovery"
		resolvedReason = "clock_recovery"
	}
	if err = updateCityRealtimeTimeStateForFrame(
		ctx, tx, worldID, *nextDueAtWorldTimeUS, targetEffectiveUTC,
		frameSequence, cursor, nextPendingDueAtWorldTimeUS, nextCatchupTargetWorldTimeUS,
		nextClockState, nextRecoveryState,
	); err != nil {
		return nil, err
	}
	_, _, stateHash, err := canonicalCityWorldState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2, updated_at = NOW() WHERE id = $1`, worldID, stateHash); err != nil {
		return nil, fmt.Errorf("store city realtime due-event resolution state hash: %w", err)
	}
	dueEventDigest, err := cityRealtimeDueEventDigest(events)
	if err != nil {
		return nil, err
	}
	frame, err := insertCityRealtimeFrame(ctx, tx, cityRealtimeFrameInsertInput{
		WorldID:               worldID,
		TemporalEngineVersion: state.temporalEngineVersion,
		FrameSequence:         frameSequence,
		TimelineCursor:        cursor,
		WorldTimeFromUS:       state.currentWorldTimeUS,
		WorldTimeToUS:         *nextDueAtWorldTimeUS,
		ClockSegmentID:        state.clockSegmentID,
		ClockSegmentSequence:  state.clockSegmentSequence,
		ClockProfileHash:      state.clockProfileHash,
		FrameKind:             frameKind,
		EffectiveUTCFrom:      state.lastCommittedEffectiveUTC,
		EffectiveUTCTo:        targetEffectiveUTC,
		PreviousStateHash:     world.stateHash,
		StateHash:             stateHash,
		DueEventDigest:        dueEventDigest,
		PhaseSummary: map[string]any{
			"schema_version":              1,
			"frame_kind":                  frameKind,
			"command_count":               0,
			"due_event_count":             len(events),
			"applied_count":               appliedCount,
			"rejected_count":              rejectedCount,
			"actor_patrol_count":          actorPatrolCount,
			"character_metabolism_count":  characterMetabolismCount,
			"case_review_closure_count":   caseReviewClosureCount,
			"agent_intent_applied_count":  agentIntentAppliedCount,
			"agent_intent_rejected_count": agentIntentRejectedCount,
			"agent_wakeup_applied_count":  agentWakeupAppliedCount,
			"agent_wakeup_rejected_count": agentWakeupRejectedCount,
			"reason":                      resolvedReason,
		},
	})
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city realtime due-event resolution: %w", err)
	}
	result.Resolved = true
	result.AppliedCount = appliedCount
	result.RejectedCount = rejectedCount
	result.CurrentWorldTimeUS = *nextDueAtWorldTimeUS
	result.TimelineCursor = cursor
	result.Frame = frame
	return result, nil
}

func normalizeCityRealtimeDiagnosticDueEvent(
	input CityRealtimeDiagnosticDueEventScheduleInput,
) (normalizedCityRealtimeDiagnosticDueEvent, error) {
	if input.WorldID <= 0 || input.DueWorldTimeUS < 0 || input.DueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return normalizedCityRealtimeDiagnosticDueEvent{}, ErrCityInvalidInput
	}
	item := normalizedCityRealtimeDiagnosticDueEvent{cityRealtimeDueEventHash: cityRealtimeDueEventHash{
		EventType:       input.EventType,
		SchemaVersion:   input.SchemaVersion,
		DueWorldTimeUS:  input.DueWorldTimeUS,
		TemporalPhase:   input.TemporalPhase,
		Priority:        input.Priority,
		AggregateType:   input.AggregateType,
		AggregateKey:    input.AggregateKey,
		DedupKey:        input.DedupKey,
		SourceKind:      cityRealtimeDueEventSourceKindSystem,
		SourceReference: input.SourceReference,
		ExpectedVersion: input.ExpectedVersion,
	}}
	if item.EventType == "" {
		item.EventType = cityRealtimeDueEventTypeDiagnostic
	}
	if item.SchemaVersion == 0 {
		item.SchemaVersion = 1
	}
	if item.TemporalPhase == "" {
		item.TemporalPhase = cityRealtimeDueEventPhasePostSchedule
	}
	if item.AggregateType == "" {
		item.AggregateType = "realtime_world"
	}
	if item.AggregateKey == "" {
		item.AggregateKey = fmt.Sprintf("world:%d", input.WorldID)
	}
	if item.SourceReference == "" {
		item.SourceReference = "realtime_diagnostic"
	}
	if item.SchemaVersion < 1 || item.SchemaVersion > 32767 ||
		item.Priority < -2147483648 || item.Priority > 2147483647 ||
		(item.ExpectedVersion != nil && *item.ExpectedVersion < 0) ||
		!cityRealtimeDueEventIdentifierValid(item.EventType, 96) ||
		!cityRealtimeDueEventIdentifierValid(item.AggregateType, 64) ||
		!cityRealtimeDueEventIdentifierValid(item.AggregateKey, 160) ||
		!cityRealtimeDueEventIdentifierValid(item.DedupKey, 160) ||
		!cityRealtimeDueEventIdentifierValid(item.SourceReference, 160) ||
		!cityRealtimeDueEventPhaseValid(item.TemporalPhase) {
		return normalizedCityRealtimeDiagnosticDueEvent{}, ErrCityInvalidInput
	}
	payload, payloadHash, err := cityRealtimeCanonicalJSONObject(input.Payload)
	if err != nil {
		return normalizedCityRealtimeDiagnosticDueEvent{}, ErrCityInvalidInput
	}
	item.Payload = payload
	item.PayloadHash = payloadHash
	return item, nil
}

func normalizeCityRealtimeSystemDueEvent(
	input CityRealtimeSystemDueEventScheduleInput,
) (normalizedCityRealtimeDiagnosticDueEvent, error) {
	// Keep system scheduling on the one canonical normalization path. The only
	// different defaults identify the server-owned source and its harmless
	// liveness barrier; all schema/identifier/payload validation remains exact.
	eventType := input.EventType
	if eventType == "" {
		eventType = cityRealtimeDueEventTypeNoop
	}
	sourceReference := input.SourceReference
	if sourceReference == "" {
		sourceReference = "realtime_system"
	}
	return normalizeCityRealtimeDiagnosticDueEvent(CityRealtimeDiagnosticDueEventScheduleInput{
		WorldID:         input.WorldID,
		EventType:       eventType,
		SchemaVersion:   input.SchemaVersion,
		DueWorldTimeUS:  input.DueWorldTimeUS,
		TemporalPhase:   input.TemporalPhase,
		Priority:        input.Priority,
		AggregateType:   input.AggregateType,
		AggregateKey:    input.AggregateKey,
		DedupKey:        input.DedupKey,
		SourceReference: sourceReference,
		Payload:         input.Payload,
		ExpectedVersion: input.ExpectedVersion,
	})
}

func cityRealtimeNextPendingDue(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*int64, error) {
	var nextDue sql.NullInt64
	if err := queryer.QueryRowContext(ctx, `
SELECT MIN(due_world_time_us)
FROM city_due_events
WHERE world_id = $1 AND status = 'pending'`, worldID).Scan(&nextDue); err != nil {
		return nil, fmt.Errorf("load city realtime next due event: %w", err)
	}
	return nullInt64Pointer(nextDue), nil
}

func cityRealtimeFirstPendingDueAtOrBefore(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, targetWorldTimeUS int64,
) (*int64, error) {
	var nextDue sql.NullInt64
	if err := queryer.QueryRowContext(ctx, `
SELECT MIN(due_world_time_us)
FROM city_due_events
WHERE world_id = $1
  AND status = 'pending'
  AND due_world_time_us <= $2`, worldID, targetWorldTimeUS).Scan(&nextDue); err != nil {
		return nil, fmt.Errorf("load city realtime due-event candidate: %w", err)
	}
	return nullInt64Pointer(nextDue), nil
}

func lockCityRealtimePendingDueEventsAt(
	ctx context.Context,
	tx *sql.Tx,
	worldID, dueWorldTimeUS int64,
) ([]cityRealtimeDueEventRecord, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, event_type, schema_version, due_world_time_us, temporal_phase,
       priority, aggregate_type, aggregate_key, dedup_key, source_kind,
       source_reference, payload, payload_hash, expected_version, status,
       created_frame_sequence, resolved_frame_sequence
FROM city_due_events
WHERE world_id = $1
  AND status = 'pending'
  AND due_world_time_us = $2
ORDER BY CASE temporal_phase
           WHEN 'pre_clock' THEN 0
           WHEN 'pre_command' THEN 1
           WHEN 'pre_lifecycle' THEN 2
           WHEN 'movement' THEN 3
           WHEN 'activity' THEN 4
           WHEN 'city_settlement' THEN 5
           WHEN 'rule_effect' THEN 6
           WHEN 'post_schedule' THEN 7
         END ASC,
         priority ASC,
         dedup_key ASC
FOR UPDATE`, worldID, dueWorldTimeUS)
	if err != nil {
		return nil, fmt.Errorf("lock city realtime due-event batch: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]cityRealtimeDueEventRecord, 0)
	for rows.Next() {
		item, scanErr := scanCityRealtimeDueEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if err = validateCityRealtimeDueEventHash(item.cityRealtimeDueEventHash); err != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_due_event"})
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city realtime due-event batch: %w", err)
	}
	return items, nil
}

func loadCityRealtimeDueEventHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]cityRealtimeDueEventHash, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, event_type, schema_version, due_world_time_us, temporal_phase,
       priority, aggregate_type, aggregate_key, dedup_key, source_kind,
       source_reference, payload, payload_hash, expected_version, status,
       created_frame_sequence, resolved_frame_sequence
FROM city_due_events
WHERE world_id = $1
ORDER BY due_world_time_us ASC,
         CASE temporal_phase
           WHEN 'pre_clock' THEN 0
           WHEN 'pre_command' THEN 1
           WHEN 'pre_lifecycle' THEN 2
           WHEN 'movement' THEN 3
           WHEN 'activity' THEN 4
           WHEN 'city_settlement' THEN 5
           WHEN 'rule_effect' THEN 6
           WHEN 'post_schedule' THEN 7
         END ASC,
         priority ASC,
         dedup_key ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city realtime due-event hash state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]cityRealtimeDueEventHash, 0)
	for rows.Next() {
		record, scanErr := scanCityRealtimeDueEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if err = validateCityRealtimeDueEventHash(record.cityRealtimeDueEventHash); err != nil {
			return nil, fmt.Errorf("validate city realtime due-event hash state: %w", err)
		}
		items = append(items, record.cityRealtimeDueEventHash)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city realtime due-event hash state: %w", err)
	}
	return items, nil
}

func scanCityRealtimeDueEvent(scanner cityScannable) (cityRealtimeDueEventRecord, error) {
	item := cityRealtimeDueEventRecord{}
	var rawPayload []byte
	var expectedVersion, createdFrameSequence, resolvedFrameSequence sql.NullInt64
	if err := scanner.Scan(
		&item.id, &item.EventType, &item.SchemaVersion, &item.DueWorldTimeUS,
		&item.TemporalPhase, &item.Priority, &item.AggregateType, &item.AggregateKey,
		&item.DedupKey, &item.SourceKind, &item.SourceReference, &rawPayload,
		&item.PayloadHash, &expectedVersion, &item.Status,
		&createdFrameSequence, &resolvedFrameSequence,
	); err != nil {
		return cityRealtimeDueEventRecord{}, fmt.Errorf("scan city realtime due event: %w", err)
	}
	payload, payloadHash, err := cityRealtimeCanonicalJSONObjectRaw(rawPayload)
	if err != nil {
		return cityRealtimeDueEventRecord{}, fmt.Errorf("decode city realtime due-event payload: %w", err)
	}
	if payloadHash != item.PayloadHash {
		return cityRealtimeDueEventRecord{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_due_event_payload_hash"})
	}
	item.Payload = payload
	item.ExpectedVersion = nullInt64Pointer(expectedVersion)
	item.CreatedFrameSequence = nullInt64Pointer(createdFrameSequence)
	item.ResolvedFrameSequence = nullInt64Pointer(resolvedFrameSequence)
	return item, nil
}

// completeCityRealtimeClockRecovery seals the remaining recovery interval even
// when it contains no due event. This is the one intentional non-empty frame
// for an otherwise empty wall-clock range: it records the state transition
// from recovering to healthy and prevents a silent time jump after a hold.
func completeCityRealtimeClockRecovery(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	world *lockedCityWorld,
	state *lockedCityRealtimeState,
	targetWorldTimeUS int64,
	effectiveUTC time.Time,
	result *CityRealtimeDiagnosticDueEventProcessResult,
) error {
	if world == nil || state == nil || result == nil ||
		state.clockState != cityRealtimeClockStateRecovering ||
		state.recoveryState != cityRealtimeRecoveryStateCatchingUp ||
		state.catchupTargetWorldTimeUS == nil || targetWorldTimeUS != *state.catchupTargetWorldTimeUS ||
		targetWorldTimeUS < state.currentWorldTimeUS ||
		state.timelineFrameSequence == cityRealtimeMaximumTimelineSequence {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_recovery_completion"})
	}
	targetEffectiveUTC := cityRealtimeAddMicroseconds(
		state.lastCommittedEffectiveUTC,
		targetWorldTimeUS-state.currentWorldTimeUS,
	)
	if targetEffectiveUTC.After(effectiveUTC) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_recovery_clock"})
	}
	frameSequence := state.timelineFrameSequence + 1
	cursor, err := cityRealtimeTimelineCursor(frameSequence)
	if err != nil {
		return err
	}
	nextDueAtWorldTimeUS, err := cityRealtimeNextPendingDue(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if err = updateCityRealtimeTimeStateForFrame(
		ctx, tx, worldID, targetWorldTimeUS, targetEffectiveUTC,
		frameSequence, cursor, nextDueAtWorldTimeUS, nil,
		cityRealtimeClockStateHealthy, cityRealtimeRecoveryStateIdle,
	); err != nil {
		return err
	}
	_, _, stateHash, err := canonicalCityWorldState(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2, updated_at = NOW() WHERE id = $1`, worldID, stateHash); err != nil {
		return fmt.Errorf("store city realtime recovery completion state hash: %w", err)
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
		FrameKind:             "recovery",
		EffectiveUTCFrom:      state.lastCommittedEffectiveUTC,
		EffectiveUTCTo:        targetEffectiveUTC,
		PreviousStateHash:     world.stateHash,
		StateHash:             stateHash,
		DueEventDigest:        cityRealtimeEmptyDueEventDigest,
		PhaseSummary: map[string]any{
			"schema_version":        1,
			"frame_kind":            "recovery",
			"command_count":         0,
			"due_event_count":       0,
			"reason":                "clock_recovery_complete",
			"clock_state_before":    cityRealtimeClockStateRecovering,
			"clock_state_after":     cityRealtimeClockStateHealthy,
			"recovery_state_before": cityRealtimeRecoveryStateCatchingUp,
			"recovery_state_after":  cityRealtimeRecoveryStateIdle,
		},
	})
	if err != nil {
		return err
	}
	result.Resolved = true
	result.CurrentWorldTimeUS = targetWorldTimeUS
	result.TimelineCursor = cursor
	result.Frame = frame
	return nil
}

func updateCityRealtimeTimeStateForFrame(
	ctx context.Context,
	tx *sql.Tx,
	worldID, currentWorldTimeUS int64,
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
SET current_world_time_us = $2,
    last_committed_effective_utc = $3,
    timeline_frame_sequence = $4,
    timeline_cursor = $5,
    next_due_at_world_time_us = $6,
	catchup_target_world_time_us = $7,
	clock_state = $8,
	recovery_state = $9,
    version = version + 1,
    updated_at = NOW()
WHERE world_id = $1`,
		worldID, currentWorldTimeUS, lastCommittedEffectiveUTC.UTC().Truncate(time.Microsecond),
		frameSequence, timelineCursor, nextDueAtWorldTimeUS, catchupTargetWorldTimeUS,
		clockState, recoveryState,
	); err != nil {
		return fmt.Errorf("update city realtime time state: %w", err)
	}
	return nil
}

func insertCityRealtimeFrame(
	ctx context.Context,
	tx *sql.Tx,
	input cityRealtimeFrameInsertInput,
) (*CityTemporalFrame, error) {
	if input.WorldID <= 0 || !cityEngineIsRealtime(input.TemporalEngineVersion) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_temporal_engine_version"})
	}
	if input.PhaseSummary == nil {
		input.PhaseSummary = make(map[string]any)
	}
	phaseSummary, err := json.Marshal(input.PhaseSummary)
	if err != nil {
		return nil, fmt.Errorf("marshal city realtime frame summary: %w", err)
	}
	frame := &CityTemporalFrame{
		WorldID:              input.WorldID,
		FrameSequence:        input.FrameSequence,
		TimelineCursor:       input.TimelineCursor,
		WorldTimeFromUS:      input.WorldTimeFromUS,
		WorldTimeToUS:        input.WorldTimeToUS,
		ClockSegmentSequence: input.ClockSegmentSequence,
		FrameKind:            input.FrameKind,
		StateHash:            input.StateHash,
		PreviousStateHash:    input.PreviousStateHash,
		DueEventDigest:       input.DueEventDigest,
		PhaseSummary:         input.PhaseSummary,
		EffectiveUTCFrom:     input.EffectiveUTCFrom.UTC().Truncate(time.Microsecond),
		EffectiveUTCTo:       input.EffectiveUTCTo.UTC().Truncate(time.Microsecond),
	}
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_temporal_frames
    (world_id, frame_sequence, timeline_cursor,
     world_time_from_us, world_time_to_us, clock_segment_id,
     temporal_engine_version, clock_profile_hash, frame_kind,
     effective_utc_from, effective_utc_to,
     previous_state_hash, state_hash, due_event_digest, phase_summary)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
        $10, $11, $12, $13, $14, $15::jsonb)
RETURNING created_at`,
		input.WorldID, input.FrameSequence, input.TimelineCursor,
		input.WorldTimeFromUS, input.WorldTimeToUS, input.ClockSegmentID,
		input.TemporalEngineVersion, input.ClockProfileHash, input.FrameKind,
		frame.EffectiveUTCFrom, frame.EffectiveUTCTo,
		input.PreviousStateHash, input.StateHash, input.DueEventDigest, phaseSummary,
	).Scan(&frame.CreatedAt); err != nil {
		return nil, fmt.Errorf("create city realtime temporal frame: %w", err)
	}
	frame.CreatedAt = frame.CreatedAt.UTC().Truncate(time.Microsecond)
	return frame, nil
}

func cityRealtimeDueEventDigest(events []cityRealtimeDueEventRecord) (string, error) {
	items := make([]cityRealtimeDueEventHash, len(events))
	for index := range events {
		items[index] = events[index].cityRealtimeDueEventHash
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("marshal city realtime due-event digest: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func validateCityRealtimeHashState(state *cityRealtimeHashState) error {
	if state == nil || !cityEngineIsRealtime(state.TemporalEngineVersion) ||
		state.CurrentWorldTimeUS < 0 || state.TimelineFrameSequence < 0 ||
		state.Version <= 0 || !cityRealtimeSHA256Hex(state.ClockProfileHash) {
		return fmt.Errorf("invalid realtime temporal state")
	}
	cursor, err := cityRealtimeTimelineCursor(state.TimelineFrameSequence)
	if err != nil || state.TimelineCursor != cursor {
		return fmt.Errorf("invalid realtime timeline cursor")
	}
	if _, err = time.Parse(time.RFC3339Nano, state.LastCommittedEffectiveUTC); err != nil {
		return fmt.Errorf("invalid realtime effective UTC: %w", err)
	}
	if state.DueEvents == nil {
		state.DueEvents = make([]cityRealtimeDueEventHash, 0)
	}
	for index := range state.DueEvents {
		if err = validateCityRealtimeDueEventHash(state.DueEvents[index]); err != nil {
			return err
		}
		if state.DueEvents[index].CreatedFrameSequence != nil &&
			*state.DueEvents[index].CreatedFrameSequence > state.TimelineFrameSequence {
			return fmt.Errorf("due event creation frame is ahead of timeline")
		}
		if state.DueEvents[index].ResolvedFrameSequence != nil &&
			*state.DueEvents[index].ResolvedFrameSequence > state.TimelineFrameSequence {
			return fmt.Errorf("due event resolution frame is ahead of timeline")
		}
		if index > 0 && cityRealtimeDueEventHashCompare(state.DueEvents[index-1], state.DueEvents[index]) >= 0 {
			return fmt.Errorf("due events are not in stable canonical order")
		}
	}
	return nil
}

func validateCityRealtimeDueEventHash(item cityRealtimeDueEventHash) error {
	if item.SchemaVersion < 1 || item.SchemaVersion > 32767 || item.DueWorldTimeUS < 0 ||
		item.DueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 ||
		!cityRealtimeDueEventIdentifierValid(item.EventType, 96) ||
		!cityRealtimeDueEventIdentifierValid(item.AggregateType, 64) ||
		!cityRealtimeDueEventIdentifierValid(item.AggregateKey, 160) ||
		!cityRealtimeDueEventIdentifierValid(item.DedupKey, 160) ||
		!cityRealtimeDueEventIdentifierValid(item.SourceReference, 160) ||
		!cityRealtimeDueEventPhaseValid(item.TemporalPhase) ||
		!cityRealtimeDueEventStatusValid(item.Status) ||
		!cityRealtimeSHA256Hex(item.PayloadHash) ||
		(item.ExpectedVersion != nil && *item.ExpectedVersion < 0) ||
		(item.CreatedFrameSequence != nil && *item.CreatedFrameSequence < 0) ||
		(item.ResolvedFrameSequence != nil && *item.ResolvedFrameSequence < 0) {
		return fmt.Errorf("invalid realtime due event")
	}
	if item.SourceKind != "command" && item.SourceKind != "fact" && item.SourceKind != "system" && item.SourceKind != "agent" {
		return fmt.Errorf("invalid realtime due-event source kind")
	}
	_, payloadHash, err := cityRealtimeCanonicalJSONObjectRaw(item.Payload)
	if err != nil || payloadHash != item.PayloadHash {
		return fmt.Errorf("invalid realtime due-event payload")
	}
	return nil
}

func cityRealtimeDueEventHashCompare(left, right cityRealtimeDueEventHash) int {
	if left.DueWorldTimeUS != right.DueWorldTimeUS {
		if left.DueWorldTimeUS < right.DueWorldTimeUS {
			return -1
		}
		return 1
	}
	leftPhase := cityRealtimeDueEventPhaseRank(left.TemporalPhase)
	rightPhase := cityRealtimeDueEventPhaseRank(right.TemporalPhase)
	if leftPhase != rightPhase {
		if leftPhase < rightPhase {
			return -1
		}
		return 1
	}
	if left.Priority != right.Priority {
		if left.Priority < right.Priority {
			return -1
		}
		return 1
	}
	if left.DedupKey < right.DedupKey {
		return -1
	}
	if left.DedupKey > right.DedupKey {
		return 1
	}
	return 0
}

func cityRealtimeDueEventIdentifierValid(value string, maximumLength int) bool {
	if len(value) < 2 || len(value) > maximumLength || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '.' || character == '-' || character == ':' {
			continue
		}
		return false
	}
	return true
}

func cityRealtimeDueEventPhaseValid(phase string) bool {
	return cityRealtimeDueEventPhaseRank(phase) >= 0
}

func cityRealtimeDueEventPhaseRank(phase string) int {
	switch phase {
	case "pre_clock":
		return 0
	case "pre_command":
		return 1
	case "pre_lifecycle":
		return 2
	case "movement":
		return 3
	case "activity":
		return 4
	case "city_settlement":
		return 5
	case "rule_effect":
		return 6
	case cityRealtimeDueEventPhasePostSchedule:
		return 7
	default:
		return -1
	}
}

func cityRealtimeDueEventStatusValid(status string) bool {
	switch status {
	case cityRealtimeDueEventStatusPending, cityRealtimeDueEventStatusLeased,
		cityRealtimeDueEventStatusApplied, cityRealtimeDueEventStatusCancelled,
		cityRealtimeDueEventStatusRejected, cityRealtimeDueEventStatusDeadLetter:
		return true
	default:
		return false
	}
}

func cityRealtimeCanonicalJSONObject(value map[string]any) (json.RawMessage, string, error) {
	if value == nil {
		value = make(map[string]any)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	return cityRealtimeCanonicalJSONObjectRaw(raw)
}

func cityRealtimeCanonicalJSONObjectRaw(raw []byte) (json.RawMessage, string, error) {
	var value map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == nil {
		return nil, "", fmt.Errorf("payload must be a JSON object")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(canonical)
	return json.RawMessage(canonical), hex.EncodeToString(sum[:]), nil
}

func cityRealtimeSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') {
			continue
		}
		return false
	}
	return true
}

const (
	cityRealtimeMaximumTimelineSequence = int64(999_999_999_999)
	cityRealtimeMaximumWorldTimeUS      = int64(^uint64(0) >> 1)
)
