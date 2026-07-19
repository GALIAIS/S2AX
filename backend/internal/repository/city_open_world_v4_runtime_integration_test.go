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

// TestCityOpenWorldV4RuntimeProgressionAndExpiration exercises the real V4
// schema and command path.  It deliberately creates an open-world style
// world, rather than reusing the F7 runtime test fixture, so a future change
// cannot quietly reconnect V4 to F7 spatial tables.
func TestCityOpenWorldV4RuntimeProgressionAndExpiration(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v4-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(9410001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V4 Runtime", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV4,
		StyleProfileID:    "jp.metropolitan",
		SpawnPolicy:       "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	catalog, err := cityService.GetCityOpenWorldRuntimeCatalog(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "sub2api-city-open-world-runtime", catalog.Profile.RuntimeID)
	require.GreaterOrEqual(t, len(catalog.Definitions), 16)

	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	submit := func(expectedTick int64, key, commandType string, payload json.RawMessage) {
		_, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: payload, ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, submitErr)
	}
	step := func(expectedTick int64, key string, expectedCommands int) *service.CityStepResult {
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, stepErr)
		require.Len(t, result.Commands, expectedCommands)
		for _, command := range result.Commands {
			require.Equal(t, service.CityCommandStatusApplied, command.Status, "%+v", command)
		}
		return result
	}

	submit(0, "v4-create-character", service.CityCommandTypeOpenWorldActorCreate, marshalPayload(map[string]any{
		"archetype_code": "urban_apprentice", "name": "Aster",
	}))
	created := step(0, "v4-create-character-step", 1)
	require.Len(t, created.OpenWorldRuntimeFacts, 1)
	require.Equal(t, service.CityOpenWorldRuntimeFactActorCreated, created.OpenWorldRuntimeFacts[0].FactType)
	actors, err := cityService.ListCityOpenWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	require.NotNil(t, actors[0].Location)
	actorCode := actors[0].Code

	for index := 0; index < 2; index++ {
		submit(1, fmt.Sprintf("v4-study-%d", index), service.CityCommandTypeOpenWorldActorActivityPerform,
			marshalPayload(map[string]any{"actor_code": actorCode, "activity_code": "technical_study"}))
	}
	studied := step(1, "v4-study-step", 2)
	require.Len(t, studied.OpenWorldRuntimeFacts, 2)
	require.Len(t, studied.OpenWorldRuntimeEffects, 6)

	submit(2, "v4-role-technician", service.CityCommandTypeOpenWorldActorRoleTransition,
		marshalPayload(map[string]any{"actor_code": actorCode, "role_code": "profession.technician"}))
	transitioned := step(2, "v4-role-technician-step", 1)
	require.Len(t, transitioned.OpenWorldRuntimeFacts, 1)
	require.Equal(t, service.CityOpenWorldRuntimeFactRoleTransitioned, transitioned.OpenWorldRuntimeFacts[0].FactType)

	for index := 0; index < 3; index++ {
		submit(3, fmt.Sprintf("v4-noise-%d", index), service.CityCommandTypeOpenWorldActorActivityPerform,
			marshalPayload(map[string]any{"actor_code": actorCode, "activity_code": "disruptive_noise"}))
	}
	violations := step(3, "v4-noise-step", 3)
	require.Len(t, violations.OpenWorldRuleCases, 3)
	require.Len(t, violations.OpenWorldRuntimeFacts, 9)

	step(4, "v4-idle-step-5", 0)
	step(5, "v4-idle-step-6", 0)
	expired := step(6, "v4-idle-step-7", 0)
	require.Len(t, expired.OpenWorldRuntimeFacts, 1)
	require.Equal(t, service.CityOpenWorldRuntimeFactStatusExpired, expired.OpenWorldRuntimeFacts[0].FactType)
	require.Len(t, expired.OpenWorldRuntimeEffects, 1)
	require.Equal(t, service.WorldRuntimeEffectStatusExpire, expired.OpenWorldRuntimeEffects[0].EffectType)

	state, err := cityService.GetCityOpenWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	for _, status := range state.Statuses {
		if status.StatusCode == "civic_warning" {
			require.Equal(t, "expired", status.Lifecycle)
			require.NotNil(t, status.EndedTick)
			require.Equal(t, int64(7), *status.EndedTick)
		}
	}
	verification, err := cityService.VerifyOpenWorldMaterialization(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.True(t, verification.CanonicalStateVerified)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_open_world_actor_attributes
SET value_units = value_units + 1
WHERE world_id = $1`, worldID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `
DELETE FROM city_open_world_runtime_facts WHERE world_id = $1`, worldID)
	require.Error(t, err)
}
