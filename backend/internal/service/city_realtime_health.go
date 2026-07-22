package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	cityRealtimeDefaultHealthLimit = 100
	cityRealtimeMaximumHealthLimit = 200
	cityRealtimeMaximumNodeHealth  = 100
)

// CityRealtimeOperationalHealthInput scopes the administrator-only realtime
// health projection. A zero WorldID requests all realtime worlds; it never
// changes scheduling, time, profile, or canonical state.
type CityRealtimeOperationalHealthInput struct {
	WorldID int64
	Limit   int
}

// CityRealtimeOperationalHealth is intentionally separate from the member
// clock DTO. It contains operational state useful to administrators while
// withholding raw provider failures, endpoints, credentials, payloads, and
// worker lease tokens.
type CityRealtimeOperationalHealth struct {
	Worlds []*CityRealtimeWorldOperationalHealth `json:"worlds"`
	Nodes  []*CityRealtimeClockNodeHealth        `json:"nodes"`
}

type CityRealtimeWorldOperationalHealth struct {
	WorldID                   int64                       `json:"world_id"`
	WorldName                 string                      `json:"world_name"`
	WorldStatus               string                      `json:"world_status"`
	TemporalEngineVersion     string                      `json:"temporal_engine_version"`
	ClockProfileID            string                      `json:"clock_profile_id"`
	ClockProfileHash          string                      `json:"clock_profile_hash"`
	SourceClockMode           string                      `json:"source_clock_mode"`
	DeploymentScope           string                      `json:"deployment_scope"`
	LifecycleStatus           string                      `json:"lifecycle_status"`
	ClockState                string                      `json:"clock_state"`
	RecoveryState             string                      `json:"recovery_state"`
	CurrentWorldTimeUS        int64                       `json:"current_world_time_us"`
	LastCommittedEffectiveUTC time.Time                   `json:"last_committed_effective_utc"`
	TimelineFrameSequence     int64                       `json:"timeline_frame_sequence"`
	TimelineCursor            string                      `json:"timeline_cursor"`
	ClockSegmentSequence      int64                       `json:"clock_segment_sequence"`
	NextDueAtWorldTimeUS      *int64                      `json:"next_due_at_world_time_us,omitempty"`
	CatchupTargetWorldTimeUS  *int64                      `json:"catchup_target_world_time_us,omitempty"`
	Scheduler                 CityRealtimeSchedulerHealth `json:"scheduler"`
}

type CityRealtimeSchedulerHealth struct {
	NodeID              *string    `json:"node_id,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	RetryNotBefore      *time.Time `json:"retry_not_before,omitempty"`
	LastAttemptAt       *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastErrorCode       *string    `json:"last_error_code,omitempty"`
}

type CityRealtimeClockNodeHealth struct {
	NodeID          string     `json:"node_id"`
	SourceClockMode string     `json:"source_clock_mode"`
	HealthState     string     `json:"health_state"`
	UncertaintyUS   *int64     `json:"uncertainty_us,omitempty"`
	LastSyncAt      *time.Time `json:"last_sync_at,omitempty"`
	ObservedAt      time.Time  `json:"observed_at"`
}

// GetRealtimeOperationalHealth exposes a read-only, administrator-scoped
// projection of clock, recovery, and scheduler readiness. This API must not be
// used as a time source and must never be made available to ordinary members.
func (s *CityEconomyService) GetRealtimeOperationalHealth(
	ctx context.Context,
	input CityRealtimeOperationalHealthInput,
) (*CityRealtimeOperationalHealth, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if input.WorldID < 0 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityRealtimeDefaultHealthLimit
	}
	if input.Limit > cityRealtimeMaximumHealthLimit {
		return nil, ErrCityInvalidInput
	}
	if input.WorldID > 0 {
		var version string
		err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, input.WorldID).Scan(&version)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCityWorldNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("load city realtime health world version: %w", err)
		}
		if !cityEngineIsRealtime(version) {
			return nil, ErrCityRealtimeWorldRequired.WithMetadata(map[string]string{"version": version})
		}
	}

	query := `
SELECT world.id, world.name, world.status,
       state.temporal_engine_version, state.clock_profile_id, state.clock_profile_hash,
       profile.source_clock_mode, profile.deployment_scope,
       state.lifecycle_status, state.clock_state, state.recovery_state,
       state.current_world_time_us, state.last_committed_effective_utc,
       state.timeline_frame_sequence, state.timeline_cursor,
       segment.segment_sequence, state.next_due_at_world_time_us,
       state.catchup_target_world_time_us,
       schedule.node_id, schedule.consecutive_failures, schedule.retry_not_before,
       schedule.last_attempt_at, schedule.last_success_at, schedule.last_error_code
FROM city_world_time_states state
JOIN city_worlds world ON world.id = state.world_id
JOIN city_clock_profiles profile ON profile.id = state.clock_profile_id
JOIN city_world_clock_segments segment
  ON segment.world_id = state.world_id AND segment.id = state.current_clock_segment_id
JOIN city_realtime_schedule_states schedule ON schedule.world_id = state.world_id
WHERE world.simulation_version IN ('city-openworld-realtime-v1', 'city-openworld-realtime-v2')`
	args := make([]any, 0, 2)
	if input.WorldID > 0 {
		query += ` AND state.world_id = $1`
		args = append(args, input.WorldID)
		query += ` ORDER BY state.world_id ASC`
	} else {
		query += ` ORDER BY state.updated_at DESC, state.world_id ASC`
	}
	placeholder := len(args) + 1
	query += fmt.Sprintf(" LIMIT $%d", placeholder)
	args = append(args, input.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list city realtime operational health: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := &CityRealtimeOperationalHealth{
		Worlds: make([]*CityRealtimeWorldOperationalHealth, 0, input.Limit),
		Nodes:  make([]*CityRealtimeClockNodeHealth, 0),
	}
	for rows.Next() {
		item := &CityRealtimeWorldOperationalHealth{}
		var nextDue, catchup sql.NullInt64
		var nodeID, lastErrorCode sql.NullString
		var retryNotBefore, lastAttemptAt, lastSuccessAt sql.NullTime
		if err = rows.Scan(
			&item.WorldID, &item.WorldName, &item.WorldStatus,
			&item.TemporalEngineVersion, &item.ClockProfileID, &item.ClockProfileHash,
			&item.SourceClockMode, &item.DeploymentScope,
			&item.LifecycleStatus, &item.ClockState, &item.RecoveryState,
			&item.CurrentWorldTimeUS, &item.LastCommittedEffectiveUTC,
			&item.TimelineFrameSequence, &item.TimelineCursor,
			&item.ClockSegmentSequence, &nextDue, &catchup,
			&nodeID, &item.Scheduler.ConsecutiveFailures, &retryNotBefore,
			&lastAttemptAt, &lastSuccessAt, &lastErrorCode,
		); err != nil {
			return nil, fmt.Errorf("scan city realtime operational health: %w", err)
		}
		item.LastCommittedEffectiveUTC = item.LastCommittedEffectiveUTC.UTC().Truncate(time.Microsecond)
		item.NextDueAtWorldTimeUS = nullInt64Pointer(nextDue)
		item.CatchupTargetWorldTimeUS = nullInt64Pointer(catchup)
		item.Scheduler.NodeID = cityRealtimeNullStringPointer(nodeID)
		item.Scheduler.RetryNotBefore = cityRealtimeNullTimePointer(retryNotBefore)
		item.Scheduler.LastAttemptAt = cityRealtimeNullTimePointer(lastAttemptAt)
		item.Scheduler.LastSuccessAt = cityRealtimeNullTimePointer(lastSuccessAt)
		item.Scheduler.LastErrorCode = cityRealtimeNullStringPointer(lastErrorCode)
		result.Worlds = append(result.Worlds, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city realtime operational health: %w", err)
	}

	nodeRows, err := s.db.QueryContext(ctx, `
SELECT node_id, source_clock_mode, health_state, uncertainty_us, last_sync_at, observed_at
FROM city_clock_nodes
ORDER BY observed_at DESC, node_id ASC
LIMIT $1`, cityRealtimeMaximumNodeHealth)
	if err != nil {
		return nil, fmt.Errorf("list city realtime clock nodes: %w", err)
	}
	defer func() { _ = nodeRows.Close() }()
	for nodeRows.Next() {
		item := &CityRealtimeClockNodeHealth{}
		var uncertainty sql.NullInt64
		var lastSyncAt sql.NullTime
		if err = nodeRows.Scan(
			&item.NodeID, &item.SourceClockMode, &item.HealthState,
			&uncertainty, &lastSyncAt, &item.ObservedAt,
		); err != nil {
			return nil, fmt.Errorf("scan city realtime clock node health: %w", err)
		}
		item.UncertaintyUS = nullInt64Pointer(uncertainty)
		item.LastSyncAt = cityRealtimeNullTimePointer(lastSyncAt)
		item.ObservedAt = item.ObservedAt.UTC().Truncate(time.Microsecond)
		result.Nodes = append(result.Nodes, item)
	}
	if err = nodeRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city realtime clock nodes: %w", err)
	}
	return result, nil
}

func cityRealtimeNullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func cityRealtimeNullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC().Truncate(time.Microsecond)
	return &result
}
