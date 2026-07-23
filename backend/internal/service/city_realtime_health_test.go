package service

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestGetRealtimeOperationalHealthProjectsStaleQuarantineWithoutPayloadData(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	now := time.Date(2026, time.July, 23, 0, 14, 15, 123456000, time.UTC)
	oldest := now.Add(-cityRealtimeAgentDecisionDeadLetterStaleAfter - time.Minute)
	nextRetry := now.Add(time.Minute)
	mock.ExpectQuery(`SELECT world\.id, world\.name, world\.status`).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{
			"world_id", "world_name", "world_status",
			"temporal_engine_version", "clock_profile_id", "clock_profile_hash",
			"source_clock_mode", "deployment_scope",
			"lifecycle_status", "clock_state", "recovery_state",
			"current_world_time_us", "last_committed_effective_utc",
			"timeline_frame_sequence", "timeline_cursor", "clock_segment_sequence",
			"next_due_at_world_time_us", "catchup_target_world_time_us",
			"node_id", "consecutive_failures", "scheduler_retry_not_before",
			"scheduler_last_attempt_at", "scheduler_last_success_at", "scheduler_last_error_code",
			"queued_requests", "leased_requests", "retry_scheduled", "quarantined_requests",
			"stale_quarantined_requests", "oldest_quarantined_at", "next_retry_not_before",
			"last_failure_code", "open_circuit_breakers",
		}).AddRow(
			int64(7), "Realtime Test", "active",
			CitySimulationVersionRealtimeV2, "clock-profile", "clock-hash",
			"system_ntp", "production",
			"running", "running", "healthy",
			int64(100), now, int64(4), "cursor", int64(2),
			nil, nil,
			"node-a", 0, nil,
			nil, now, nil,
			4, 0, 2, 2,
			1, oldest, nextRetry,
			"PROVIDER_TIMEOUT", 1,
		))
	mock.ExpectQuery(`SELECT node_id, source_clock_mode, health_state`).
		WithArgs(cityRealtimeMaximumNodeHealth).
		WillReturnRows(sqlmock.NewRows([]string{
			"node_id", "source_clock_mode", "health_state", "uncertainty_us", "last_sync_at", "observed_at",
		}))

	service := &CityEconomyService{db: db}
	health, err := service.GetRealtimeOperationalHealth(WithCitySystemAdministrator(context.Background()), CityRealtimeOperationalHealthInput{Limit: 1})
	require.NoError(t, err)
	require.Len(t, health.Worlds, 1)
	worker := health.Worlds[0].AgentDecisionWorker
	require.Equal(t, 4, worker.QueuedRequests)
	require.Equal(t, 2, worker.QuarantinedRequests)
	require.Equal(t, 1, worker.StaleQuarantinedRequests)
	require.Equal(t, int(cityRealtimeAgentDecisionDeadLetterStaleAfter/time.Second), worker.QuarantineStaleAfterSeconds)
	require.NotNil(t, worker.OldestQuarantinedAt)
	require.Equal(t, oldest, *worker.OldestQuarantinedAt)
	require.NotNil(t, worker.NextRetryNotBefore)
	require.Equal(t, nextRetry, *worker.NextRetryNotBefore)
}
