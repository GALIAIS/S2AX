package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	cityRealtimeDiagnosticClockProfileID          = "realtime-diagnostic-v1"
	cityRealtimeDiagnosticClockProfileHash        = "e88e41a95fad3a148b3ffa6926d0eee9783b1473ab0ab8dd6c18f058fd8bc40a"
	cityRealtimeDiagnosticClockMode               = "frozen_test_clock"
	cityRealtimeProductionNTPClockProfileID       = "realtime-system-ntp-v1"
	cityRealtimeProductionNTSClockProfileID       = "realtime-system-nts-v1"
	cityRealtimeTimeQuantumUS               int64 = 1_000_000
	cityRealtimeGenesisFrameCursor                = "twf_000000000000"
	cityRealtimeEmptyDueEventDigest               = "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945"
	cityRealtimeDefaultTimelineLimit              = 100
	cityRealtimeMaximumTimelineLimit              = 200
)

// cityRealtimeProductionClockProfileID maps the only two host-clock modes
// that this binary can attest to a sealed, migration-provisioned profile.
// It deliberately does not accept a profile ID from an HTTP caller.
func cityRealtimeProductionClockProfileID(sourceClockMode string) (string, bool) {
	switch sourceClockMode {
	case "system_ntp":
		return cityRealtimeProductionNTPClockProfileID, true
	case "system_nts":
		return cityRealtimeProductionNTSClockProfileID, true
	default:
		return "", false
	}
}

var (
	ErrCityRealtimeWorldRequired                = infraerrors.Conflict("CITY_REALTIME_WORLD_REQUIRED", "city world does not use the realtime engine")
	ErrCityRealtimeStaticWorldRequired          = infraerrors.Conflict("CITY_REALTIME_STATIC_WORLD_REQUIRED", "city world does not use the realtime static-worldgen engine")
	ErrCityRealtimeLegacyAPI                    = infraerrors.Conflict("CITY_REALTIME_LEGACY_API_UNSUPPORTED", "realtime worlds require the temporal-frame API")
	ErrCityRealtimeClockUnsafe                  = infraerrors.Conflict("CITY_REALTIME_CLOCK_UNSAFE", "realtime clock source cannot advance this world safely")
	ErrCityRealtimeDueEventPending              = infraerrors.Conflict("CITY_REALTIME_DUE_EVENT_PENDING", "realtime world has a due event that must be resolved before advancing")
	ErrCityRealtimeDueEventConflict             = infraerrors.Conflict("CITY_REALTIME_DUE_EVENT_CONFLICT", "realtime due-event deduplication key already exists")
	ErrCityRealtimeCharacterRuntimeUnavailable  = infraerrors.Conflict("CITY_REALTIME_CHARACTER_RUNTIME_UNAVAILABLE", "realtime world does not have the player-character runtime enabled")
	ErrCityRealtimeCharacterExists              = infraerrors.Conflict("CITY_REALTIME_CHARACTER_EXISTS", "a realtime character already exists for this world member")
	ErrCityRealtimeCharacterNotFound            = infraerrors.NotFound("CITY_REALTIME_CHARACTER_NOT_FOUND", "realtime character not found")
	ErrCityRealtimeCharacterControlUnavailable  = infraerrors.Conflict("CITY_REALTIME_CHARACTER_CONTROL_UNAVAILABLE", "realtime character cannot perform this action now")
	ErrCityRealtimeCharacterActivityUnavailable = infraerrors.Conflict("CITY_REALTIME_CHARACTER_ACTIVITY_UNAVAILABLE", "realtime character activity is not available in the current shared-world state")
	ErrCityRealtimeCharacterRoleUnavailable     = infraerrors.Conflict("CITY_REALTIME_CHARACTER_ROLE_UNAVAILABLE", "realtime character does not satisfy this role's current requirements")
	ErrCityRealtimeCharacterIdempotencyConflict = infraerrors.Conflict("CITY_REALTIME_CHARACTER_IDEMPOTENCY_CONFLICT", "idempotency key was already used for a different character action")
	ErrCityRealtimeAgentRuntimeUnavailable      = infraerrors.Conflict("CITY_REALTIME_AGENT_RUNTIME_UNAVAILABLE", "realtime world does not have the current Agent decision runtime enabled")
	ErrCityRealtimeAgentDecisionUnavailable     = infraerrors.Conflict("CITY_REALTIME_AGENT_DECISION_UNAVAILABLE", "realtime Agent cannot accept a decision in its current state")
	ErrCityRealtimeAgentDecisionConflict        = infraerrors.Conflict("CITY_REALTIME_AGENT_DECISION_CONFLICT", "realtime Agent decision lease or terminal state changed")
	ErrCityRealtimeAgentDecisionNotFound        = infraerrors.NotFound("CITY_REALTIME_AGENT_DECISION_NOT_FOUND", "realtime Agent decision request not found")
)

// CityRealtimeClock is the member-safe realtime clock projection. It exposes
// only the committed shared cursor and never a node identifier, time-source
// secret, or browser-supplied timestamp.
type CityRealtimeClock struct {
	WorldID               int64                 `json:"world_id"`
	TemporalEngineVersion string                `json:"temporal_engine_version"`
	TimelineCursor        string                `json:"timeline_cursor"`
	ClockProfileID        string                `json:"clock_profile_id"`
	ClockProfileHash      string                `json:"clock_profile_hash"`
	TimeQuantumUS         int64                 `json:"time_quantum_us"`
	WorldTime             CityRealtimeWorldTime `json:"world_time"`
	committedElapsedUS    int64
	committedEffectiveUTC time.Time
	nextDueAtWorldTimeUS  *int64
}

type CityRealtimeWorldTime struct {
	ElapsedUS                int64     `json:"elapsed_us"`
	CommittedElapsedUS       int64     `json:"committed_elapsed_us"`
	LiveProjection           bool      `json:"live_projection"`
	Timezone                 string    `json:"timezone"`
	LocalTime                time.Time `json:"local_time"`
	SourceEffectiveUTC       time.Time `json:"source_effective_utc"`
	ClockState               string    `json:"clock_state"`
	RecoveryState            string    `json:"recovery_state"`
	CatchupTargetWorldTimeUS *int64    `json:"catchup_target_world_time_us,omitempty"`
	SourceClockMode          string    `json:"source_clock_mode"`
}

// CityTemporalFrame is deliberately a safe summary. Operational worker
// diagnostics and due-event payloads remain internal even when a member can
// see the shared world timeline.
type CityTemporalFrame struct {
	WorldID              int64          `json:"world_id"`
	FrameSequence        int64          `json:"frame_sequence"`
	TimelineCursor       string         `json:"timeline_cursor"`
	WorldTimeFromUS      int64          `json:"world_time_from_us"`
	WorldTimeToUS        int64          `json:"world_time_to_us"`
	ClockSegmentSequence int64          `json:"clock_segment_sequence"`
	FrameKind            string         `json:"frame_kind"`
	StateHash            string         `json:"state_hash"`
	PreviousStateHash    *string        `json:"previous_state_hash,omitempty"`
	DueEventDigest       string         `json:"due_event_digest"`
	PhaseSummary         map[string]any `json:"phase_summary"`
	EffectiveUTCFrom     time.Time      `json:"effective_utc_from"`
	EffectiveUTCTo       time.Time      `json:"effective_utc_to"`
	CreatedAt            time.Time      `json:"created_at"`
}

type CityTemporalFramePage struct {
	Items                  []*CityTemporalFrame `json:"items"`
	NextAfterFrameSequence *int64               `json:"next_after_frame_sequence,omitempty"`
}

type CityTemporalFrameListInput struct {
	UserID             int64
	WorldID            int64
	AfterFrameSequence int64
	Limit              int
}

// CityRealtimeDiagnosticAdvanceInput is an internal-only clock exercise
// primitive. There is deliberately no HTTP handler for it: only the frozen
// diagnostic profile and a server-marked administrator context can supply an
// effective UTC value. Production advancement will be driven by Clock
// Authority rather than any client payload.
type CityRealtimeDiagnosticAdvanceInput struct {
	WorldID      int64
	EffectiveUTC time.Time
}

type CityRealtimeDiagnosticAdvanceResult struct {
	Advanced           bool               `json:"advanced"`
	CurrentWorldTimeUS int64              `json:"current_world_time_us"`
	TimelineCursor     string             `json:"timeline_cursor"`
	Frame              *CityTemporalFrame `json:"frame,omitempty"`
}

type cityRealtimeHashState struct {
	TemporalEngineVersion     string                     `json:"temporal_engine_version"`
	ClockProfileID            string                     `json:"clock_profile_id"`
	ClockProfileHash          string                     `json:"clock_profile_hash"`
	LifecycleStatus           string                     `json:"lifecycle_status"`
	ClockState                string                     `json:"clock_state"`
	CurrentWorldTimeUS        int64                      `json:"current_world_time_us"`
	LastCommittedEffectiveUTC string                     `json:"last_committed_effective_utc"`
	ClockSegmentSequence      int64                      `json:"clock_segment_sequence"`
	TimelineFrameSequence     int64                      `json:"timeline_frame_sequence"`
	TimelineCursor            string                     `json:"timeline_cursor"`
	RecoveryState             string                     `json:"recovery_state"`
	CatchupTargetWorldTimeUS  *int64                     `json:"catchup_target_world_time_us,omitempty"`
	Version                   int64                      `json:"version"`
	DueEvents                 []cityRealtimeDueEventHash `json:"due_events"`
}

func (s *CityEconomyService) GetRealtimeClock(
	ctx context.Context,
	userID, worldID int64,
) (*CityRealtimeClock, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := s.requireRealtimeWorldRead(ctx, userID, worldID); err != nil {
		return nil, err
	}

	item := &CityRealtimeClock{WorldID: worldID}
	var elapsedUS int64
	var effectiveUTC time.Time
	var timezone string
	var catchupTargetWorldTimeUS sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT state.temporal_engine_version, state.timeline_cursor,
       state.clock_profile_id, state.clock_profile_hash,
       profile.quantum_us, state.current_world_time_us,
	       state.last_committed_effective_utc, state.clock_state, state.recovery_state,
	       state.catchup_target_world_time_us, state.next_due_at_world_time_us,
       profile.source_clock_mode, world.timezone
FROM city_world_time_states state
JOIN city_clock_profiles profile ON profile.id = state.clock_profile_id
JOIN city_worlds world ON world.id = state.world_id
WHERE state.world_id = $1`, worldID).Scan(
		&item.TemporalEngineVersion, &item.TimelineCursor,
		&item.ClockProfileID, &item.ClockProfileHash,
		&item.TimeQuantumUS, &elapsedUS,
		&effectiveUTC, &item.WorldTime.ClockState, &item.WorldTime.RecoveryState,
		&catchupTargetWorldTimeUS, &item.nextDueAtWorldTimeUS,
		&item.WorldTime.SourceClockMode, &timezone,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_time_state"})
	}
	if err != nil {
		return nil, fmt.Errorf("load city realtime clock: %w", err)
	}
	localTime, err := cityRealtimeLocalTime(elapsedUS, timezone)
	if err != nil {
		return nil, err
	}
	item.WorldTime.ElapsedUS = elapsedUS
	item.WorldTime.CommittedElapsedUS = elapsedUS
	item.WorldTime.Timezone = timezone
	item.WorldTime.CatchupTargetWorldTimeUS = nullInt64Pointer(catchupTargetWorldTimeUS)
	item.WorldTime.LocalTime = localTime
	item.WorldTime.SourceEffectiveUTC = effectiveUTC.UTC().Truncate(time.Microsecond)
	item.committedElapsedUS = elapsedUS
	item.committedEffectiveUTC = item.WorldTime.SourceEffectiveUTC
	return item, nil
}

// projectCityRealtimeClockAtObservation advances only a member-facing clock
// projection. It does not write a frame, mutate canonical world state, or
// move a temporal cursor. This lets an idle production world display the
// server-observed NTP/NTS time without creating empty per-second transactions.
//
// The projected value is capped at the next unresolved due-event boundary:
// consumers never see time beyond an event whose causal effects have not yet
// been committed to a Temporal Frame.
func projectCityRealtimeClockAtObservation(
	clock *CityRealtimeClock,
	observation CityRealtimeClockObservation,
) error {
	if clock == nil || clock.TimeQuantumUS <= 0 || observation.EffectiveUTC.IsZero() {
		return ErrCityRealtimeClockUnsafe
	}
	committedEffectiveUTC := clock.committedEffectiveUTC.UTC().Truncate(time.Microsecond)
	if committedEffectiveUTC.IsZero() {
		committedEffectiveUTC = clock.WorldTime.SourceEffectiveUTC.UTC().Truncate(time.Microsecond)
	}
	if committedEffectiveUTC.IsZero() {
		return ErrCityRealtimeClockUnsafe
	}
	committedElapsedUS := clock.committedElapsedUS
	if committedElapsedUS == 0 && clock.WorldTime.CommittedElapsedUS != 0 {
		committedElapsedUS = clock.WorldTime.CommittedElapsedUS
	}
	clock.WorldTime.ElapsedUS = committedElapsedUS
	clock.WorldTime.CommittedElapsedUS = committedElapsedUS
	clock.WorldTime.LiveProjection = false
	clock.WorldTime.SourceEffectiveUTC = committedEffectiveUTC
	observedEffectiveUTC := observation.EffectiveUTC.UTC().Truncate(time.Microsecond)
	if !observedEffectiveUTC.After(committedEffectiveUTC) {
		return nil
	}
	deltaUS := observedEffectiveUTC.UnixMicro() - committedEffectiveUTC.UnixMicro()
	if deltaUS <= 0 {
		return nil
	}
	quantizedDeltaUS := (deltaUS / clock.TimeQuantumUS) * clock.TimeQuantumUS
	if quantizedDeltaUS <= 0 {
		return nil
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if committedElapsedUS > maxInt64-quantizedDeltaUS {
		return ErrCityRealtimeClockUnsafe
	}
	projectedElapsedUS := committedElapsedUS + quantizedDeltaUS
	if clock.nextDueAtWorldTimeUS != nil && projectedElapsedUS > *clock.nextDueAtWorldTimeUS {
		projectedElapsedUS = *clock.nextDueAtWorldTimeUS
	}
	if projectedElapsedUS <= committedElapsedUS {
		return nil
	}
	localTime, err := cityRealtimeLocalTime(projectedElapsedUS, clock.WorldTime.Timezone)
	if err != nil {
		return err
	}
	clock.WorldTime.ElapsedUS = projectedElapsedUS
	clock.WorldTime.LocalTime = localTime
	clock.WorldTime.SourceEffectiveUTC = cityRealtimeAddMicroseconds(
		committedEffectiveUTC,
		projectedElapsedUS-committedElapsedUS,
	)
	clock.WorldTime.LiveProjection = true
	return nil
}

func (s *CityEconomyService) ListTemporalFrames(
	ctx context.Context,
	input CityTemporalFrameListInput,
) (*CityTemporalFramePage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterFrameSequence < -1 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityRealtimeDefaultTimelineLimit
	}
	if input.Limit > cityRealtimeMaximumTimelineLimit {
		return nil, ErrCityInvalidInput
	}
	if err := s.requireRealtimeWorldRead(ctx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT frame.world_id, frame.frame_sequence, frame.timeline_cursor,
       frame.world_time_from_us, frame.world_time_to_us,
       segment.segment_sequence, frame.frame_kind, frame.state_hash,
       frame.previous_state_hash, frame.due_event_digest, frame.phase_summary,
       frame.effective_utc_from, frame.effective_utc_to, frame.created_at
FROM city_temporal_frames frame
JOIN city_world_clock_segments segment
  ON segment.world_id = frame.world_id AND segment.id = frame.clock_segment_id
WHERE frame.world_id = $1 AND frame.frame_sequence > $2
ORDER BY frame.frame_sequence ASC
LIMIT $3`, input.WorldID, input.AfterFrameSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city temporal frames: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]*CityTemporalFrame, 0, input.Limit)
	for rows.Next() {
		item, scanErr := scanCityTemporalFrame(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city temporal frames: %w", err)
	}

	page := &CityTemporalFramePage{Items: items}
	if len(items) > input.Limit {
		page.Items = items[:input.Limit]
		cursor := page.Items[len(page.Items)-1].FrameSequence
		page.NextAfterFrameSequence = &cursor
	}
	return page, nil
}

// AdvanceRealtimeDiagnosticWorld commits one explicit test/diagnostic frame.
// It is not a generic clock setter: production worlds, client-originated time,
// and sub-quantum empty wall-clock intervals are all rejected or ignored.
func (s *CityEconomyService) AdvanceRealtimeDiagnosticWorld(
	ctx context.Context,
	input CityRealtimeDiagnosticAdvanceInput,
) (*CityRealtimeDiagnosticAdvanceResult, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if input.WorldID <= 0 || input.EffectiveUTC.IsZero() {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city realtime diagnostic transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(input.WorldID)); err != nil {
		return nil, fmt.Errorf("lock city realtime world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, 0, input.WorldID)
	if err != nil {
		return nil, err
	}
	if !cityEngineIsRealtime(world.simulationVersion) {
		return nil, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": world.simulationVersion})
	}
	if world.status != CityWorldStatusRunning || world.stateHash == nil {
		return nil, ErrCityWorldUnavailable.WithMetadata(map[string]string{"status": world.status})
	}

	state, err := lockCityRealtimeState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if err = requireCityRealtimeDiagnosticState(state); err != nil {
		return nil, err
	}
	effectiveUTC := input.EffectiveUTC.UTC().Truncate(time.Microsecond)
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
	result := &CityRealtimeDiagnosticAdvanceResult{
		CurrentWorldTimeUS: state.currentWorldTimeUS,
		TimelineCursor:     state.timelineCursor,
	}
	if quantizedDeltaUS == 0 {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit city realtime diagnostic no-op: %w", err)
		}
		return result, nil
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if state.currentWorldTimeUS > maxInt64-quantizedDeltaUS || state.timelineFrameSequence == maxInt64 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_timeline"})
	}
	targetWorldTimeUS := state.currentWorldTimeUS + quantizedDeltaUS
	if state.nextDueAtWorldTimeUS != nil && targetWorldTimeUS >= *state.nextDueAtWorldTimeUS {
		return nil, ErrCityRealtimeDueEventPending.WithMetadata(map[string]string{
			"next_due_at_world_time_us": fmt.Sprintf("%d", *state.nextDueAtWorldTimeUS),
		})
	}
	targetEffectiveUTC := cityRealtimeAddMicroseconds(state.lastCommittedEffectiveUTC, quantizedDeltaUS)
	if targetEffectiveUTC.After(effectiveUTC) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_quantum"})
	}
	frameSequence := state.timelineFrameSequence + 1
	cursor, err := cityRealtimeTimelineCursor(frameSequence)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE city_world_time_states
SET current_world_time_us = $2,
    last_committed_effective_utc = $3,
    timeline_frame_sequence = $4,
    timeline_cursor = $5,
    version = version + 1,
    updated_at = NOW()
WHERE world_id = $1`,
		input.WorldID, targetWorldTimeUS, targetEffectiveUTC, frameSequence, cursor,
	); err != nil {
		return nil, fmt.Errorf("advance city realtime time state: %w", err)
	}
	_, _, stateHash, err := canonicalCityWorldState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2, updated_at = NOW() WHERE id = $1`, input.WorldID, stateHash); err != nil {
		return nil, fmt.Errorf("store city realtime frame state hash: %w", err)
	}
	phaseSummary, err := json.Marshal(map[string]any{
		"schema_version":  1,
		"frame_kind":      "diagnostic",
		"command_count":   0,
		"due_event_count": 0,
		"reason":          "frozen_test_clock",
	})
	if err != nil {
		return nil, fmt.Errorf("marshal city realtime diagnostic summary: %w", err)
	}
	frame := &CityTemporalFrame{
		WorldID:              input.WorldID,
		FrameSequence:        frameSequence,
		TimelineCursor:       cursor,
		WorldTimeFromUS:      state.currentWorldTimeUS,
		WorldTimeToUS:        targetWorldTimeUS,
		ClockSegmentSequence: state.clockSegmentSequence,
		FrameKind:            "diagnostic",
		StateHash:            stateHash,
		PreviousStateHash:    world.stateHash,
		DueEventDigest:       cityRealtimeEmptyDueEventDigest,
		EffectiveUTCFrom:     state.lastCommittedEffectiveUTC,
		EffectiveUTCTo:       targetEffectiveUTC,
	}
	if err = json.Unmarshal(phaseSummary, &frame.PhaseSummary); err != nil {
		return nil, fmt.Errorf("decode city realtime diagnostic summary: %w", err)
	}
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_temporal_frames
    (world_id, frame_sequence, timeline_cursor,
     world_time_from_us, world_time_to_us, clock_segment_id,
     temporal_engine_version, clock_profile_hash, frame_kind,
     effective_utc_from, effective_utc_to,
     previous_state_hash, state_hash, due_event_digest, phase_summary)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'diagnostic',
        $9, $10, $11, $12, $13, $14::jsonb)
RETURNING created_at`,
		input.WorldID, frameSequence, cursor,
		state.currentWorldTimeUS, targetWorldTimeUS, state.clockSegmentID,
		state.temporalEngineVersion, state.clockProfileHash,
		state.lastCommittedEffectiveUTC, targetEffectiveUTC,
		world.stateHash, stateHash, cityRealtimeEmptyDueEventDigest, phaseSummary,
	).Scan(&frame.CreatedAt); err != nil {
		return nil, fmt.Errorf("create city realtime diagnostic frame: %w", err)
	}
	frame.EffectiveUTCFrom = frame.EffectiveUTCFrom.UTC().Truncate(time.Microsecond)
	frame.EffectiveUTCTo = frame.EffectiveUTCTo.UTC().Truncate(time.Microsecond)
	frame.CreatedAt = frame.CreatedAt.UTC().Truncate(time.Microsecond)
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city realtime diagnostic frame: %w", err)
	}
	result.Advanced = true
	result.CurrentWorldTimeUS = targetWorldTimeUS
	result.TimelineCursor = cursor
	result.Frame = frame
	return result, nil
}

func (s *CityEconomyService) requireRealtimeWorldRead(ctx context.Context, userID, worldID int64) error {
	return requireCityRealtimeWorldRead(ctx, s.db, userID, worldID)
}

func requireCityRealtimeWorldRead(ctx context.Context, queryer citySQLQueryer, userID, worldID int64) error {
	if err := authorizeCityWorldRead(ctx, queryer, userID, worldID); err != nil {
		return err
	}
	var version string
	if err := queryer.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCityWorldNotFound
		}
		return fmt.Errorf("load city realtime world version: %w", err)
	}
	if !cityEngineIsRealtime(version) {
		return ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": version})
	}
	return nil
}

// cityRealtimeClockProfileSelection is the validated immutable profile that a
// newly-created realtime world pins into its canonical temporal state. It is
// deliberately loaded in the same transaction as world creation, so a world
// can never be born with a browser-supplied or mutable clock configuration.
type cityRealtimeClockProfileSelection struct {
	ID                    string
	Hash                  string
	SourceClockMode       string
	DeploymentScope       string
	TimeQuantumUS         int64
	MaximumUncertaintyUS  int64
	MaximumDatabaseSkewUS int64
}

func loadCityRealtimeClockProfile(
	ctx context.Context,
	tx *sql.Tx,
	profileID string,
) (cityRealtimeClockProfileSelection, error) {
	profile := cityRealtimeClockProfileSelection{}
	if !cityRealtimeDueEventIdentifierValid(profileID, 96) {
		return profile, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "clock_profile_id"})
	}
	err := tx.QueryRowContext(ctx, `
SELECT id, profile_hash, source_clock_mode, deployment_scope,
       quantum_us, maximum_uncertainty_us, maximum_database_skew_us
FROM city_clock_profiles
WHERE id = $1 AND status = 'published'`, profileID).Scan(
		&profile.ID, &profile.Hash, &profile.SourceClockMode, &profile.DeploymentScope,
		&profile.TimeQuantumUS, &profile.MaximumUncertaintyUS, &profile.MaximumDatabaseSkewUS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return profile, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "clock_profile_id"})
	}
	if err != nil {
		return profile, fmt.Errorf("load city realtime clock profile: %w", err)
	}
	if !cityRealtimeSHA256Hex(profile.Hash) || profile.TimeQuantumUS != cityRealtimeTimeQuantumUS ||
		profile.MaximumUncertaintyUS < 0 || profile.MaximumDatabaseSkewUS < 0 {
		return profile, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_clock_profile"})
	}
	switch profile.DeploymentScope {
	case "test":
		// Test profiles must remain the one compiled diagnostic profile. This
		// prevents a deployment from silently creating a browser/manual clock
		// world under a different mutable-looking label.
		if profile.ID != cityRealtimeDiagnosticClockProfileID ||
			profile.Hash != cityRealtimeDiagnosticClockProfileHash ||
			profile.SourceClockMode != cityRealtimeDiagnosticClockMode {
			return profile, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_diagnostic_profile"})
		}
	case "production":
		if profile.SourceClockMode != "system_ntp" &&
			profile.SourceClockMode != "system_nts" &&
			profile.SourceClockMode != "private_time_service" {
			return profile, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_production_clock_source"})
		}
	default:
		// Development/manual profiles have no production scheduler semantics
		// and are intentionally not creatable through this world path.
		return profile, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "clock_profile_id"})
	}
	return profile, nil
}

func initializeCityRealtimeFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	temporalEngineVersion string,
	profile cityRealtimeClockProfileSelection,
) error {
	if worldID <= 0 || !cityEngineIsRealtime(temporalEngineVersion) || profile.ID == "" || !cityRealtimeSHA256Hex(profile.Hash) ||
		profile.TimeQuantumUS != cityRealtimeTimeQuantumUS {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_clock_profile"})
	}

	var effectiveUTC time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&effectiveUTC); err != nil {
		return fmt.Errorf("read city realtime genesis clock: %w", err)
	}
	effectiveUTC = effectiveUTC.UTC().Truncate(time.Microsecond)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_world_time_states
    (world_id, temporal_engine_version, clock_profile_id, clock_profile_hash,
     lifecycle_status, clock_state, current_world_time_us,
     last_committed_effective_utc, timeline_frame_sequence, timeline_cursor,
     recovery_state, version)
VALUES ($1, $2, $3, $4, 'running', 'initializing', 0, $5, 0, $6, 'idle', 1)`,
		worldID, temporalEngineVersion, profile.ID,
		profile.Hash, effectiveUTC, cityRealtimeGenesisFrameCursor,
	); err != nil {
		return fmt.Errorf("create city realtime time state: %w", err)
	}

	var segmentID int64
	if err := tx.QueryRowContext(ctx, `
INSERT INTO city_world_clock_segments
    (world_id, segment_sequence, clock_profile_id, clock_profile_hash,
     source_clock_mode, effective_utc_anchor, world_elapsed_anchor_us,
     uncertainty_us, reason, monotonic_anchor_proof)
VALUES ($1, 0, $2, $3, $4, $5, 0, 0, 'create', NULL)
RETURNING id`,
		worldID, profile.ID, profile.Hash, profile.SourceClockMode, effectiveUTC,
	).Scan(&segmentID); err != nil {
		return fmt.Errorf("create city realtime clock segment: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE city_world_time_states
SET current_clock_segment_id = $2, updated_at = NOW()
WHERE world_id = $1`, worldID, segmentID); err != nil {
		return fmt.Errorf("attach city realtime clock segment: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_schedule_states (world_id)
VALUES ($1)`, worldID); err != nil {
		return fmt.Errorf("create city realtime schedule state: %w", err)
	}
	if profile.DeploymentScope == "production" {
		// A production world must be observable by the scheduler immediately.
		// The bootstrap barrier is a harmless, replayable system event at time
		// zero; it lets the first verified Clock Authority observation turn the
		// world from initializing to healthy without introducing empty-second
		// writes or a browser-controlled clock advance.
		payload, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
			"purpose": "production_clock_bootstrap",
		})
		if err != nil {
			return fmt.Errorf("canonicalize city realtime bootstrap payload: %w", err)
		}
		const genesisFrameSequence int64 = 0
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_due_events
    (world_id, event_type, schema_version, due_world_time_us, temporal_phase,
     priority, aggregate_type, aggregate_key, dedup_key, source_kind,
     source_reference, payload, payload_hash, expected_version, status,
     created_frame_sequence)
VALUES ($1, $2, 1, 0, $3, 0, 'realtime_world', $4, 'realtime.bootstrap',
        'system', 'realtime_bootstrap', $5::jsonb, $6, NULL, 'pending', $7)`,
			worldID, cityRealtimeDueEventTypeNoop, cityRealtimeDueEventPhasePostSchedule,
			fmt.Sprintf("world:%d", worldID), []byte(payload), payloadHash, genesisFrameSequence,
		); err != nil {
			return fmt.Errorf("create city realtime production bootstrap due event: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE city_world_time_states
SET next_due_at_world_time_us = 0, updated_at = NOW()
WHERE world_id = $1`, worldID); err != nil {
			return fmt.Errorf("attach city realtime production bootstrap due event: %w", err)
		}
	}
	return nil
}

type lockedCityRealtimeState struct {
	temporalEngineVersion     string
	clockProfileID            string
	clockProfileHash          string
	sourceClockMode           string
	deploymentScope           string
	maximumUncertaintyUS      int64
	maximumDatabaseSkewUS     int64
	lifecycleStatus           string
	clockState                string
	recoveryState             string
	clockSegmentID            int64
	clockSegmentSequence      int64
	currentWorldTimeUS        int64
	lastCommittedEffectiveUTC time.Time
	timelineFrameSequence     int64
	timelineCursor            string
	timeQuantumUS             int64
	nextDueAtWorldTimeUS      *int64
	catchupTargetWorldTimeUS  *int64
}

func lockCityRealtimeState(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*lockedCityRealtimeState, error) {
	item := &lockedCityRealtimeState{}
	var nextDueAtWorldTimeUS, catchupTargetWorldTimeUS sql.NullInt64
	err := tx.QueryRowContext(ctx, `
	SELECT state.temporal_engine_version, state.clock_profile_id, state.clock_profile_hash,
       state.current_clock_segment_id, segment.segment_sequence,
	       state.current_world_time_us, state.last_committed_effective_utc,
	       state.timeline_frame_sequence, state.timeline_cursor,
	       state.next_due_at_world_time_us,
	       state.catchup_target_world_time_us,
       profile.quantum_us, profile.source_clock_mode, profile.deployment_scope,
	       profile.maximum_uncertainty_us, profile.maximum_database_skew_us,
	       state.lifecycle_status, state.clock_state, state.recovery_state
FROM city_world_time_states state
JOIN city_world_clock_segments segment
  ON segment.world_id = state.world_id AND segment.id = state.current_clock_segment_id
JOIN city_clock_profiles profile ON profile.id = state.clock_profile_id
WHERE state.world_id = $1
FOR UPDATE OF state`, worldID).Scan(
		&item.temporalEngineVersion, &item.clockProfileID, &item.clockProfileHash,
		&item.clockSegmentID, &item.clockSegmentSequence,
		&item.currentWorldTimeUS, &item.lastCommittedEffectiveUTC,
		&item.timelineFrameSequence, &item.timelineCursor,
		&nextDueAtWorldTimeUS,
		&catchupTargetWorldTimeUS,
		&item.timeQuantumUS, &item.sourceClockMode, &item.deploymentScope,
		&item.maximumUncertaintyUS, &item.maximumDatabaseSkewUS,
		&item.lifecycleStatus, &item.clockState, &item.recoveryState,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_time_state"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock city realtime state: %w", err)
	}
	if !cityEngineIsRealtime(item.temporalEngineVersion) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_temporal_engine_version"})
	}
	item.nextDueAtWorldTimeUS = nullInt64Pointer(nextDueAtWorldTimeUS)
	item.catchupTargetWorldTimeUS = nullInt64Pointer(catchupTargetWorldTimeUS)
	item.lastCommittedEffectiveUTC = item.lastCommittedEffectiveUTC.UTC().Truncate(time.Microsecond)
	return item, nil
}

func requireCityRealtimeDiagnosticProfile(state *lockedCityRealtimeState) error {
	if state == nil || state.clockProfileID != cityRealtimeDiagnosticClockProfileID ||
		state.clockProfileHash != cityRealtimeDiagnosticClockProfileHash ||
		state.sourceClockMode != cityRealtimeDiagnosticClockMode || state.deploymentScope != "test" ||
		state.timeQuantumUS != cityRealtimeTimeQuantumUS {
		return ErrCityManagementRequired.WithMetadata(map[string]string{"field": "realtime_diagnostic_profile"})
	}
	return nil
}

func requireCityRealtimeDiagnosticState(state *lockedCityRealtimeState) error {
	if err := requireCityRealtimeDiagnosticProfile(state); err != nil {
		return err
	}
	if state.lifecycleStatus != CityWorldStatusRunning {
		return ErrCityWorldUnavailable.WithMetadata(map[string]string{"status": state.lifecycleStatus})
	}
	if state.clockState == "unsafe" {
		return ErrCityRealtimeClockUnsafe
	}
	return nil
}

func initializeCityRealtimeGenesisFrame(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	stateHash string,
) error {
	var segmentID int64
	var cursor string
	var effectiveUTC time.Time
	var profileHash, temporalEngineVersion string
	err := tx.QueryRowContext(ctx, `
SELECT current_clock_segment_id, timeline_cursor,
       last_committed_effective_utc, clock_profile_hash, temporal_engine_version
FROM city_world_time_states
WHERE world_id = $1`, worldID).Scan(&segmentID, &cursor, &effectiveUTC, &profileHash, &temporalEngineVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_genesis_state"})
	}
	if err != nil {
		return fmt.Errorf("load city realtime genesis state: %w", err)
	}
	if !cityEngineIsRealtime(temporalEngineVersion) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_temporal_engine_version"})
	}
	dueEventDigest, scheduledDueEventCount, err := cityRealtimeGenesisDueEventDigest(ctx, tx, worldID)
	if err != nil {
		return err
	}
	phaseSummary, err := json.Marshal(map[string]any{
		"schema_version":            1,
		"frame_kind":                "genesis",
		"command_count":             0,
		"due_event_count":           0,
		"scheduled_due_event_count": scheduledDueEventCount,
	})
	if err != nil {
		return fmt.Errorf("marshal city realtime genesis summary: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_temporal_frames
    (world_id, frame_sequence, timeline_cursor,
     world_time_from_us, world_time_to_us, clock_segment_id,
     temporal_engine_version, clock_profile_hash, frame_kind,
     effective_utc_from, effective_utc_to,
     previous_state_hash, state_hash, due_event_digest, phase_summary)
VALUES ($1, 0, $2, 0, 0, $3, $4, $5, 'genesis',
        $6, $6, NULL, $7, $8, $9::jsonb)`,
		worldID, cursor, segmentID, temporalEngineVersion,
		profileHash, effectiveUTC.UTC().Truncate(time.Microsecond), stateHash,
		dueEventDigest, phaseSummary,
	); err != nil {
		return fmt.Errorf("create city realtime genesis frame: %w", err)
	}
	return nil
}

// cityRealtimeGenesisDueEventDigest records every queue fact created as part
// of the genesis transaction. At present this is the production bootstrap
// barrier; keeping it in the genesis frame makes profile activation fully
// replayable instead of relying on scheduler startup timing.
func cityRealtimeGenesisDueEventDigest(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (string, int, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, event_type, schema_version, due_world_time_us, temporal_phase,
       priority, aggregate_type, aggregate_key, dedup_key, source_kind,
       source_reference, payload, payload_hash, expected_version, status,
       created_frame_sequence, resolved_frame_sequence
FROM city_due_events
WHERE world_id = $1 AND created_frame_sequence = 0
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
		return "", 0, fmt.Errorf("load city realtime genesis due events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := make([]cityRealtimeDueEventRecord, 0, 1)
	for rows.Next() {
		item, scanErr := scanCityRealtimeDueEvent(rows)
		if scanErr != nil {
			return "", 0, scanErr
		}
		if validateErr := validateCityRealtimeDueEventHash(item.cityRealtimeDueEventHash); validateErr != nil {
			return "", 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_genesis_due_event"})
		}
		events = append(events, item)
	}
	if err = rows.Err(); err != nil {
		return "", 0, fmt.Errorf("iterate city realtime genesis due events: %w", err)
	}
	digest, err := cityRealtimeDueEventDigest(events)
	if err != nil {
		return "", 0, err
	}
	return digest, len(events), nil
}

func loadCityRealtimeHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeHashState, error) {
	item := &cityRealtimeHashState{DueEvents: make([]cityRealtimeDueEventHash, 0)}
	var committedUTC time.Time
	var catchupTargetWorldTimeUS sql.NullInt64
	err := queryer.QueryRowContext(ctx, `
SELECT state.temporal_engine_version, state.clock_profile_id, state.clock_profile_hash,
       state.lifecycle_status, state.clock_state, state.current_world_time_us,
       state.last_committed_effective_utc, segment.segment_sequence,
	       state.timeline_frame_sequence, state.timeline_cursor, state.recovery_state,
	       state.catchup_target_world_time_us,
       state.version
FROM city_world_time_states state
JOIN city_world_clock_segments segment
  ON segment.world_id = state.world_id AND segment.id = state.current_clock_segment_id
WHERE state.world_id = $1`, worldID).Scan(
		&item.TemporalEngineVersion, &item.ClockProfileID, &item.ClockProfileHash,
		&item.LifecycleStatus, &item.ClockState, &item.CurrentWorldTimeUS,
		&committedUTC, &item.ClockSegmentSequence, &item.TimelineFrameSequence,
		&item.TimelineCursor, &item.RecoveryState, &catchupTargetWorldTimeUS, &item.Version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_time_state"})
	}
	if err != nil {
		return nil, fmt.Errorf("load city realtime hash state: %w", err)
	}
	item.LastCommittedEffectiveUTC = committedUTC.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
	item.CatchupTargetWorldTimeUS = nullInt64Pointer(catchupTargetWorldTimeUS)
	item.DueEvents, err = loadCityRealtimeDueEventHashState(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func scanCityTemporalFrame(scanner cityScannable) (*CityTemporalFrame, error) {
	item := &CityTemporalFrame{}
	var previousHash sql.NullString
	var phaseSummary []byte
	if err := scanner.Scan(
		&item.WorldID, &item.FrameSequence, &item.TimelineCursor,
		&item.WorldTimeFromUS, &item.WorldTimeToUS,
		&item.ClockSegmentSequence, &item.FrameKind, &item.StateHash,
		&previousHash, &item.DueEventDigest, &phaseSummary,
		&item.EffectiveUTCFrom, &item.EffectiveUTCTo, &item.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan city temporal frame: %w", err)
	}
	if previousHash.Valid {
		item.PreviousStateHash = &previousHash.String
	}
	decodedSummary, err := decodeCityJSONMap(phaseSummary)
	if err != nil {
		return nil, fmt.Errorf("decode city temporal frame summary: %w", err)
	}
	item.PhaseSummary = decodedSummary
	item.EffectiveUTCFrom = item.EffectiveUTCFrom.UTC().Truncate(time.Microsecond)
	item.EffectiveUTCTo = item.EffectiveUTCTo.UTC().Truncate(time.Microsecond)
	item.CreatedAt = item.CreatedAt.UTC().Truncate(time.Microsecond)
	return item, nil
}

func cityRealtimeLocalTime(elapsedUS int64, timezone string) (time.Time, error) {
	if elapsedUS < 0 {
		return time.Time{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "current_world_time_us"})
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "timezone"})
	}
	seconds := elapsedUS / cityRealtimeTimeQuantumUS
	microseconds := elapsedUS % cityRealtimeTimeQuantumUS
	localTime := time.Unix(cityTickEpochTime.Unix()+seconds, microseconds*1_000).UTC().In(location)
	return localTime, nil
}

func cityRealtimeAddMicroseconds(base time.Time, deltaUS int64) time.Time {
	seconds := deltaUS / cityRealtimeTimeQuantumUS
	microseconds := deltaUS % cityRealtimeTimeQuantumUS
	return time.Unix(base.Unix()+seconds, int64(base.Nanosecond())+microseconds*1_000).UTC()
}

func cityRealtimeTimelineCursor(sequence int64) (string, error) {
	if sequence < 0 || sequence > 999_999_999_999 {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "timeline_frame_sequence"})
	}
	return fmt.Sprintf("twf_%012d", sequence), nil
}
