package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestCityRealtimeTimelineCursor(t *testing.T) {
	cursor, err := cityRealtimeTimelineCursor(0)
	require.NoError(t, err)
	require.Equal(t, cityRealtimeGenesisFrameCursor, cursor)

	cursor, err = cityRealtimeTimelineCursor(321)
	require.NoError(t, err)
	require.Equal(t, "twf_000000000321", cursor)

	_, err = cityRealtimeTimelineCursor(-1)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
	_, err = cityRealtimeTimelineCursor(1_000_000_000_000)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func TestCityRealtimeLocalTimeUsesWorldTimezone(t *testing.T) {
	localTime, err := cityRealtimeLocalTime(cityRealtimeTimeQuantumUS, "Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, "2000-01-01T08:00:01+08:00", localTime.Format(time.RFC3339))

	_, err = cityRealtimeLocalTime(-1, "UTC")
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func TestGetRealtimeClockReturnsOnlyCommittedSharedState(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	committedAt := time.Date(2026, time.July, 21, 1, 2, 3, 456000000, time.UTC)
	mock.ExpectQuery(`SELECT 1 FROM city_members`).
		WithArgs(int64(7), int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
	mock.ExpectQuery(`SELECT simulation_version FROM city_worlds`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"simulation_version"}).AddRow(CitySimulationVersionRealtimeV1))
	mock.ExpectQuery(`SELECT state.temporal_engine_version`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"temporal_engine_version", "timeline_cursor", "clock_profile_id", "clock_profile_hash",
			"quantum_us", "current_world_time_us", "last_committed_effective_utc", "clock_state", "recovery_state",
			"catchup_target_world_time_us", "next_due_at_world_time_us",
			"source_clock_mode", "timezone",
		}).AddRow(
			CitySimulationVersionRealtimeV1, "twf_000000000000", cityRealtimeDiagnosticClockProfileID,
			cityRealtimeDiagnosticClockProfileHash, cityRealtimeTimeQuantumUS, int64(0), committedAt,
			cityRealtimeClockStateInitializing, cityRealtimeRecoveryStateIdle, nil,
			nil,
			cityRealtimeDiagnosticClockMode, "Asia/Shanghai",
		))

	item, err := NewCityEconomyService(db).GetRealtimeClock(context.Background(), 11, 7)
	require.NoError(t, err)
	require.Equal(t, int64(7), item.WorldID)
	require.Equal(t, CitySimulationVersionRealtimeV1, item.TemporalEngineVersion)
	require.Equal(t, "twf_000000000000", item.TimelineCursor)
	require.Equal(t, int64(0), item.WorldTime.ElapsedUS)
	require.Equal(t, int64(0), item.WorldTime.CommittedElapsedUS)
	require.False(t, item.WorldTime.LiveProjection)
	require.Equal(t, "2000-01-01T08:00:00+08:00", item.WorldTime.LocalTime.Format(time.RFC3339))
	require.Equal(t, committedAt, item.WorldTime.SourceEffectiveUTC)
	require.Equal(t, cityRealtimeClockStateInitializing, item.WorldTime.ClockState)
	require.Equal(t, cityRealtimeRecoveryStateIdle, item.WorldTime.RecoveryState)
	require.Nil(t, item.WorldTime.CatchupTargetWorldTimeUS)
}

func TestProjectCityRealtimeClockAtObservationUsesBoundedNoWriteProjection(t *testing.T) {
	committedUTC := time.Date(2026, time.July, 21, 0, 0, 0, 0, time.UTC)
	nextDueAtWorldTimeUS := int64(3 * cityRealtimeTimeQuantumUS)
	clock := &CityRealtimeClock{
		TimeQuantumUS:         cityRealtimeTimeQuantumUS,
		committedElapsedUS:    cityRealtimeTimeQuantumUS,
		committedEffectiveUTC: committedUTC,
		nextDueAtWorldTimeUS:  &nextDueAtWorldTimeUS,
		WorldTime: CityRealtimeWorldTime{
			ElapsedUS:          cityRealtimeTimeQuantumUS,
			CommittedElapsedUS: cityRealtimeTimeQuantumUS,
			Timezone:           "Asia/Shanghai",
			LocalTime:          mustCityRealtimeLocalTime(t, cityRealtimeTimeQuantumUS, "Asia/Shanghai"),
			SourceEffectiveUTC: committedUTC,
		},
	}

	err := projectCityRealtimeClockAtObservation(clock, CityRealtimeClockObservation{
		EffectiveUTC: committedUTC.Add(5*time.Second + 500*time.Millisecond),
	})
	require.NoError(t, err)
	require.Equal(t, int64(3*cityRealtimeTimeQuantumUS), clock.WorldTime.ElapsedUS)
	require.Equal(t, int64(cityRealtimeTimeQuantumUS), clock.WorldTime.CommittedElapsedUS)
	require.True(t, clock.WorldTime.LiveProjection)
	require.Equal(t, committedUTC.Add(2*time.Second), clock.WorldTime.SourceEffectiveUTC)
	require.Equal(t, "2000-01-01T08:00:03+08:00", clock.WorldTime.LocalTime.Format(time.RFC3339))

	// Reusing the same response object must restart from the committed
	// baseline instead of cumulatively drifting on every poll.
	err = projectCityRealtimeClockAtObservation(clock, CityRealtimeClockObservation{
		EffectiveUTC: committedUTC.Add(12 * time.Second),
	})
	require.NoError(t, err)
	require.Equal(t, int64(3*cityRealtimeTimeQuantumUS), clock.WorldTime.ElapsedUS)
	require.Equal(t, int64(cityRealtimeTimeQuantumUS), clock.WorldTime.CommittedElapsedUS)
	require.Equal(t, committedUTC.Add(2*time.Second), clock.WorldTime.SourceEffectiveUTC)
}

func mustCityRealtimeLocalTime(t *testing.T, elapsedUS int64, timezone string) time.Time {
	t.Helper()
	value, err := cityRealtimeLocalTime(elapsedUS, timezone)
	require.NoError(t, err)
	return value
}

func TestGetRealtimeClockRejectsLegacyWorld(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	mock.ExpectQuery(`SELECT 1 FROM city_members`).
		WithArgs(int64(7), int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
	mock.ExpectQuery(`SELECT simulation_version FROM city_worlds`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"simulation_version"}).AddRow(CitySimulationVersionOpenWorldV24))

	_, err = NewCityEconomyService(db).GetRealtimeClock(context.Background(), 11, 7)
	require.ErrorIs(t, err, ErrCityRealtimeWorldRequired)
}

func TestNormalizeCityRealtimeSystemDueEventUsesServerOwnedDefaults(t *testing.T) {
	normalized, err := normalizeCityRealtimeSystemDueEvent(CityRealtimeSystemDueEventScheduleInput{
		WorldID:        7,
		DueWorldTimeUS: cityRealtimeTimeQuantumUS,
		DedupKey:       "realtime.noop.bootstrap",
	})
	require.NoError(t, err)
	require.Equal(t, cityRealtimeDueEventTypeNoop, normalized.EventType)
	require.Equal(t, "realtime_system", normalized.SourceReference)
	require.Equal(t, cityRealtimeDueEventSourceKindSystem, normalized.SourceKind)
	require.Equal(t, "realtime_world", normalized.AggregateType)
	require.Equal(t, "world:7", normalized.AggregateKey)

	_, err = normalizeCityRealtimeSystemDueEvent(CityRealtimeSystemDueEventScheduleInput{
		WorldID:        7,
		DueWorldTimeUS: cityRealtimeTimeQuantumUS,
		DedupKey:       "invalid payload",
	})
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestCityRealtimeVisualManifestValidationRejectsUnsafeRendererPayload(t *testing.T) {
	valid := json.RawMessage(`{
  "schema_version": 1,
  "render_mode": "procedural_pixel_v1",
  "logical_tile_px": 16,
  "profile_palettes": {"default": {"ground": "#5f8259", "road": "#77736b"}},
  "assets": []
}`)
	require.NoError(t, validateCityRealtimeVisualManifest(valid, cityRealtimeProceduralPixelRenderContract))

	unsafePalette := json.RawMessage(`{
  "schema_version": 1,
  "render_mode": "procedural_pixel_v1",
  "logical_tile_px": 16,
  "profile_palettes": {"default": {"ground": "url(javascript:alert(1))"}},
  "assets": []
}`)
	require.ErrorIs(t, validateCityRealtimeVisualManifest(unsafePalette, cityRealtimeProceduralPixelRenderContract), ErrCitySimulationInvariant)

	unsafeTransport := json.RawMessage(`{
  "schema_version": 1,
  "render_mode": "procedural_pixel_v1",
  "logical_tile_px": 16,
  "profile_palettes": {"default": {"ground": "#5f8259"}},
  "assets": ["https://untrusted.invalid/atlas.png"]
}`)
	require.ErrorIs(t, validateCityRealtimeVisualManifest(unsafeTransport, cityRealtimeProceduralPixelRenderContract), ErrCitySimulationInvariant)
}

func TestCityRealtimeVisualBindingHashPinsEveryContentPlaneField(t *testing.T) {
	binding := CityRealtimeVisualBinding{
		PackID:                    cityRealtimeDefaultVisualPackID,
		PackVersion:               cityRealtimeDefaultVisualPackVersion,
		SpatialProfileID:          "jp.metropolitan",
		SemanticProjectionVersion: cityRealtimeSemanticProjectionVersion,
		RenderContractVersion:     cityRealtimeProceduralPixelRenderContract,
		ManifestHash:              strings.Repeat("a", 64),
		AssetSetHash:              strings.Repeat("b", 64),
	}
	binding.BindingHash = cityRealtimeVisualBindingHash(binding)
	require.NoError(t, validateCityRealtimeVisualBinding(binding))

	mutated := binding
	mutated.AssetSetHash = strings.Repeat("c", 64)
	require.ErrorIs(t, validateCityRealtimeVisualBinding(mutated), ErrCitySimulationInvariant)

	mutated = binding
	mutated.SpatialProfileID = "cn.metropolitan"
	require.ErrorIs(t, validateCityRealtimeVisualBinding(mutated), ErrCitySimulationInvariant)
}
