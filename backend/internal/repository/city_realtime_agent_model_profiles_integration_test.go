//go:build integration

package repository

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentModelProfileSnapshotAndBudgetArePinned(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("realtime-model-profile-owner-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_201_282)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Model Profile " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	var bindingCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_agent_model_profile_world_bindings
WHERE world_id = $1 AND binding_status = 'active'
  AND profile_code = 'system.fake.deterministic' AND profile_version = 1`, worldID).Scan(&bindingCount))
	require.Equal(t, 4, bindingCount)

	var agentCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND agent_subtype = 'character.npc' AND lifecycle_status = 'active'
ORDER BY agent_code ASC
LIMIT 1`, worldID).Scan(&agentCode))
	queued, err := cityService.QueueRealtimeAgentDecision(adminCtx, service.CityRealtimeAgentDecisionRequestInput{
		WorldID: worldID, AgentCode: agentCode, TriggerKey: "scheduler.model.profile",
	})
	require.NoError(t, err)

	var profileCode, profileHash, budgetHash string
	var profileVersion int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT model_profile_code, model_profile_version, model_profile_hash, model_budget_hash
FROM city_realtime_agent_decision_requests
WHERE world_id = $1 AND request_code = $2`, worldID, queued.RequestCode).Scan(
		&profileCode, &profileVersion, &profileHash, &budgetHash,
	))
	require.Equal(t, "system.fake.deterministic", profileCode)
	require.Equal(t, 1, profileVersion)
	require.Len(t, profileHash, 64)
	require.Len(t, budgetHash, 64)

	resolved, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: queued.RequestCode, WorkerID: "worker.model.profile",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", resolved.Status)

	var attemptCode, attemptProfileCode, attemptProfileHash, attemptBudgetHash string
	var attemptProfileVersion, reservedInput, reservedOutput int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT attempt_code, model_profile_code, model_profile_version,
       model_profile_hash, model_budget_hash, reserved_input_tokens, reserved_output_tokens
FROM city_realtime_agent_decision_attempts
WHERE world_id = $1 AND request_code = $2`, worldID, queued.RequestCode).Scan(
		&attemptCode, &attemptProfileCode, &attemptProfileVersion,
		&attemptProfileHash, &attemptBudgetHash, &reservedInput, &reservedOutput,
	))
	require.Equal(t, profileCode, attemptProfileCode)
	require.Equal(t, profileVersion, attemptProfileVersion)
	require.Equal(t, profileHash, attemptProfileHash)
	require.Equal(t, budgetHash, attemptBudgetHash)
	require.Equal(t, 4096, reservedInput)
	require.Equal(t, 256, reservedOutput)

	var reservationCount, usageWindowCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_agent_model_attempt_budget_reservations
WHERE world_id = $1 AND attempt_code = $2
  AND profile_code = $3 AND profile_version = $4
  AND profile_hash = $5 AND budget_hash = $6`,
		worldID, attemptCode, profileCode, profileVersion, profileHash, budgetHash,
	).Scan(&reservationCount))
	require.Equal(t, 1, reservationCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_agent_model_usage_windows
WHERE profile_code = $1 AND profile_version = $2
  AND profile_hash = $3 AND budget_hash = $4
  AND source_world_id = $5 AND source_request_code = $6`,
		profileCode, profileVersion, profileHash, budgetHash, worldID, queued.RequestCode,
	).Scan(&usageWindowCount))
	// NPC agents are system-owned, so profile/world/agent are charged and no
	// owner window is created.
	require.Equal(t, 3, usageWindowCount)
}
