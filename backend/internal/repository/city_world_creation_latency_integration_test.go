//go:build integration

package repository

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCityF8V3WorldCreationCompletesWithinRequestBudget(t *testing.T) {
	isolateIntegrationData(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("city-f8v3-create-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	seed := int64(810003)
	startedAt := time.Now()
	foundation, err := service.NewCityEconomyService(integrationDB).CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "F8V3 Bootstrap",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionF8V3,
	})
	require.NoError(t, err)
	require.Less(t, time.Since(startedAt), 20*time.Second)

	engine, err := service.NewCityEconomyService(integrationDB).GetEngineInfo(ctx, owner.ID, foundation.World.ID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF8V3, engine.Version)
}
