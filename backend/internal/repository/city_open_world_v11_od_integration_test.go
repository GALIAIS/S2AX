//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV11AutomaticODDemandIsFactBackedAndRecoverable(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v11-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(11_110_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V11 Automatic OD", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV11,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	initial, err := cityService.GetCityOpenWorldMobilityODState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(0), initial.Policy.BaselineTick)
	require.NotEmpty(t, initial.Sources)
	require.Empty(t, initial.Metrics)
	source := requireCityOpenWorldV11ODDueSource(t, initial, 1)

	currentTick := int64(0)
	step := func(key string) *service.CityStepResult {
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.NotNil(t, result.Tick)
		require.Equal(t, currentTick+1, result.Tick.Tick)
		currentTick = result.Tick.Tick
		return result
	}

	first := step("v11-od-first-source")
	firstTypes := make([]string, 0, len(first.OpenWorldRuntimeFacts))
	for _, fact := range first.OpenWorldRuntimeFacts {
		firstTypes = append(firstTypes, fact.FactType)
	}
	require.Contains(t, firstTypes, "system.mobility.od.generated")
	require.Contains(t, firstTypes, service.CityOpenWorldRuntimeFactMobilityRequested)

	afterGeneration, err := cityService.GetCityOpenWorldMobilityODState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	generatedSource := requireCityOpenWorldV11ODSource(t, afterGeneration, source.Code)
	require.Equal(t, int64(1), generatedSource.GeneratedCount)
	require.Equal(t, int64(0), generatedSource.SuppressedCount)
	require.Equal(t, int64(25), generatedSource.NextDueTick)
	require.NotNil(t, generatedSource.LastFact)
	require.Equal(t, int64(1), generatedSource.LastFact.Tick)

	mobility, err := cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	demand := requireCityOpenWorldV11ODDemand(t, mobility, source.Code)
	require.Equal(t, int64(1), demand.RequestedTick)
	require.Equal(t, "pending", demand.Status)
	require.Equal(t, int64(2), demand.EarliestDepartureTick)

	step("v11-od-schedule")
	mobility, err = cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	demand = requireCityOpenWorldV11ODDemand(t, mobility, source.Code)
	require.Equal(t, "scheduled", demand.Status)
	require.NotNil(t, demand.RouteCode)
	route := requireCityOpenWorldV11ODRoute(t, mobility, *demand.RouteCode)
	require.Greater(t, route.ArrivalTick, currentTick)

	for currentTick < route.ArrivalTick {
		step(fmt.Sprintf("v11-od-route-%d", currentTick+1))
	}
	mobility, err = cityService.GetCityOpenWorldMobilityState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	demand = requireCityOpenWorldV11ODDemand(t, mobility, source.Code)
	require.Equal(t, "completed", demand.Status)
	require.NotNil(t, demand.CompletedTick)

	// V10 owns the macro-to-local arrival and intentionally observes the V9
	// completion only on a later tick.
	step("v11-od-arrival-bridge")
	arrivals, err := cityService.GetCityOpenWorldMobilityArrivalState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	arrival := requireCityOpenWorldV11ODArrival(t, arrivals, demand.Code)
	require.Equal(t, "landed", arrival.Status)
	require.NotNil(t, arrival.LandingLocation)
	require.NotNil(t, arrival.LandingFact)

	for currentTick < 25 {
		step(fmt.Sprintf("v11-od-cycle-%d", currentTick+1))
	}
	closed, err := cityService.GetCityOpenWorldMobilityODState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, closed.Metrics, 1)
	metric := closed.Metrics[0]
	require.Equal(t, int64(1), metric.CycleStartTick)
	require.Equal(t, int64(24), metric.CycleEndTick)
	require.Equal(t, int64(25), metric.ClosedTick)
	require.GreaterOrEqual(t, metric.GeneratedCount, int64(1))
	require.GreaterOrEqual(t, metric.NetworkRequested, int64(1))
	require.Equal(t, int64(1), closed.Policy.MetricCount)

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v11-od-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v11-od-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldMobilityODState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, closed, restored)
}

func TestCityOpenWorldV10UpgradeToV11PinsAutomaticODBaseline(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v10-v11-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(11_110_002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V10 to V11 OD Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV10,
		StyleProfileID:    "cn.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v10-v11-od-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV11,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)

	state, err := cityService.GetCityOpenWorldMobilityODState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(0), state.Policy.BaselineTick)
	require.NotEmpty(t, state.Sources)
	require.Zero(t, state.Policy.GeneratedCount)
	require.Zero(t, state.Policy.SuppressedCount)
	require.Empty(t, state.Metrics)
}

func requireCityOpenWorldV11ODDueSource(
	t *testing.T,
	state *service.CityOpenWorldMobilityODState,
	dueTick int64,
) service.CityOpenWorldMobilityODSource {
	t.Helper()
	for _, source := range state.Sources {
		if source.NextDueTick == dueTick {
			return source
		}
	}
	t.Fatalf("no V11 OD source due at tick %d", dueTick)
	return service.CityOpenWorldMobilityODSource{}
}

func requireCityOpenWorldV11ODSource(
	t *testing.T,
	state *service.CityOpenWorldMobilityODState,
	code string,
) service.CityOpenWorldMobilityODSource {
	t.Helper()
	for _, source := range state.Sources {
		if source.Code == code {
			return source
		}
	}
	t.Fatalf("V11 OD source %q not found", code)
	return service.CityOpenWorldMobilityODSource{}
}

func requireCityOpenWorldV11ODDemand(
	t *testing.T,
	state *service.CityOpenWorldMobilityState,
	sourceCode string,
) service.CityOpenWorldMobilityDemand {
	t.Helper()
	for _, demand := range state.Demands {
		metadata := struct {
			SourceCode string `json:"od_source_code"`
		}{}
		require.NoError(t, json.Unmarshal(demand.Metadata, &metadata))
		if metadata.SourceCode == sourceCode {
			return demand
		}
	}
	t.Fatalf("V11 OD demand for source %q not found", sourceCode)
	return service.CityOpenWorldMobilityDemand{}
}

func requireCityOpenWorldV11ODRoute(
	t *testing.T,
	state *service.CityOpenWorldMobilityState,
	code string,
) service.CityOpenWorldMobilityRoute {
	t.Helper()
	for _, route := range state.Routes {
		if route.Code == code {
			return route
		}
	}
	t.Fatalf("V11 OD route %q not found", code)
	return service.CityOpenWorldMobilityRoute{}
}

func requireCityOpenWorldV11ODArrival(
	t *testing.T,
	state *service.CityOpenWorldMobilityArrivalState,
	demandCode string,
) service.CityOpenWorldMobilityArrival {
	t.Helper()
	for _, arrival := range state.Arrivals {
		if arrival.DemandCode == demandCode {
			return arrival
		}
	}
	t.Fatalf("V11 OD arrival for demand %q not found", demandCode)
	return service.CityOpenWorldMobilityArrival{}
}
