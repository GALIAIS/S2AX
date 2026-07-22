package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCityRealtimeHostClockAuthorityRequiresExplicitTrustAndRejectsWallClockSteps(t *testing.T) {
	wallUTC := time.Date(2026, time.July, 21, 0, 0, 0, 0, time.UTC)
	elapsed := time.Duration(0)
	authority := newCityRealtimeHostClockAuthority(config.CityRealtimeClockConfig{
		Enabled:                true,
		TrustHostClock:         true,
		NodeID:                 "realtime-host-a",
		SourceClockMode:        "system_ntp",
		UncertaintyUS:          100,
		MaximumWallClockStepUS: 2_000_000,
	}, func() time.Time {
		return wallUTC
	}, func() time.Duration {
		return elapsed
	})
	profile := CityRealtimeClockProfile{
		ID: "production-ntp-v1", Hash: strings.Repeat("a", 64),
		SourceClockMode: "system_ntp", DeploymentScope: "production",
		TimeQuantumUS: cityRealtimeTimeQuantumUS, MaximumUncertaintyUS: 500,
	}
	require.True(t, authority.IsOperational())
	require.True(t, authority.Supports(profile))

	first, err := authority.Observe(context.Background(), profile)
	require.NoError(t, err)
	require.Equal(t, wallUTC, first.EffectiveUTC)
	require.Equal(t, "realtime-host-a", first.NodeID)

	wallUTC = wallUTC.Add(time.Second)
	elapsed += time.Second
	second, err := authority.Observe(context.Background(), profile)
	require.NoError(t, err)
	require.Equal(t, wallUTC, second.EffectiveUTC)

	// The host wall clock jumped nineteen seconds beyond the monotonic proof;
	// no database reducer can receive this observation.
	wallUTC = wallUTC.Add(20 * time.Second)
	elapsed += time.Second
	_, err = authority.Observe(context.Background(), profile)
	require.ErrorIs(t, err, ErrCityRealtimeClockUnsafe)

	privateProfile := profile
	privateProfile.SourceClockMode = "private_time_service"
	require.False(t, authority.Supports(privateProfile))
}

func TestCityRealtimeHostClockAuthorityFailsClosedWhenNotExplicitlyTrusted(t *testing.T) {
	authority := newCityRealtimeHostClockAuthority(config.CityRealtimeClockConfig{
		Enabled:                true,
		NodeID:                 "realtime-host-a",
		SourceClockMode:        "system_ntp",
		MaximumWallClockStepUS: 1_000_000,
	}, time.Now, func() time.Duration { return 0 })
	require.False(t, authority.IsOperational())
	_, err := authority.Observe(context.Background(), CityRealtimeClockProfile{
		SourceClockMode: "system_ntp", DeploymentScope: "production",
		TimeQuantumUS: cityRealtimeTimeQuantumUS,
	})
	require.ErrorIs(t, err, ErrCityRealtimeClockUnsafe)
}

func TestCityRealtimeHostClockAuthorityPinsOnlyItsConfiguredProductionProfile(t *testing.T) {
	tests := []struct {
		name       string
		sourceMode string
		wantID     string
	}{
		{name: "ntp", sourceMode: "system_ntp", wantID: cityRealtimeProductionNTPClockProfileID},
		{name: "nts", sourceMode: "system_nts", wantID: cityRealtimeProductionNTSClockProfileID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			authority := newCityRealtimeHostClockAuthority(config.CityRealtimeClockConfig{
				Enabled: true, TrustHostClock: true, NodeID: "realtime-host-a",
				SourceClockMode: tc.sourceMode, MaximumWallClockStepUS: 1_000_000,
			}, time.Now, func() time.Duration { return 0 })
			profileID, ok := authority.ProductionProfileID()
			require.True(t, ok)
			require.Equal(t, tc.wantID, profileID)
		})
	}

	untrusted := newCityRealtimeHostClockAuthority(config.CityRealtimeClockConfig{
		Enabled: true, NodeID: "realtime-host-a", SourceClockMode: "system_ntp",
		MaximumWallClockStepUS: 1_000_000,
	}, time.Now, func() time.Duration { return 0 })
	_, ok := untrusted.ProductionProfileID()
	require.False(t, ok)
}

func TestCityRealtimeLifecycleControllerRejectsRealtimeCreationWithoutTrustedAuthority(t *testing.T) {
	controller := NewCityRealtimeLifecycleController(
		NewCityEconomyService(nil),
		newCityRealtimeHostClockAuthority(config.CityRealtimeClockConfig{}, time.Now, func() time.Duration { return 0 }),
	)
	_, err := controller.CreateRealtimeWorld(WithCitySystemAdministrator(context.Background()), CityWorldCreateInput{
		OwnerUserID: 1,
		Name:        "Rejected realtime city",
		Timezone:    "UTC",
	})
	require.ErrorIs(t, err, ErrCityRealtimeClockUnsafe)
}

func TestCityRealtimeLifecycleControllerLoadsOnlyPinnedProductionProfile(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	observedUTC := time.Date(2026, time.July, 21, 0, 0, 0, 0, time.UTC)
	authority := newCityRealtimeHostClockAuthority(config.CityRealtimeClockConfig{
		Enabled:                true,
		TrustHostClock:         true,
		NodeID:                 "realtime-host-a",
		SourceClockMode:        "system_ntp",
		UncertaintyUS:          100,
		MaximumWallClockStepUS: 1_000_000,
	}, func() time.Time {
		return observedUTC
	}, func() time.Duration { return 0 })
	mock.ExpectQuery(`SELECT world\.simulation_version`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"simulation_version", "clock_profile_id", "clock_profile_hash", "source_clock_mode", "deployment_scope",
			"quantum_us", "maximum_uncertainty_us", "maximum_database_skew_us",
		}).AddRow(
			CitySimulationVersionRealtimeV1, "production-ntp-v1", strings.Repeat("a", 64), "system_ntp", "production",
			int64(1_000_000), int64(500), int64(5_000_000),
		))
	controller := NewCityRealtimeLifecycleController(NewCityEconomyService(db), authority)

	_, err = controller.observeProductionWorld(context.Background(), 7)
	require.ErrorIs(t, err, ErrCityManagementRequired)

	observation, err := controller.observeProductionWorld(WithCitySystemAdministrator(context.Background()), 7)
	require.NoError(t, err)
	require.Equal(t, observedUTC, observation.EffectiveUTC)
	require.Equal(t, "realtime-host-a", observation.NodeID)
	require.Equal(t, "system_ntp", observation.SourceClockMode)
}
