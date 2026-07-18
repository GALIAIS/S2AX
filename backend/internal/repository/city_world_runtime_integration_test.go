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

func TestOpenWorldRuntimeCharacterProgressionRulesExpirationAndRecovery(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("open-world-owner-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("open-world-outsider-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(760001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World Runtime", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V5,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	catalog, err := cityService.GetWorldRuntimeCatalog(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "sub2api-open-world-runtime", catalog.Profile.RuntimeID)
	require.GreaterOrEqual(t, len(catalog.Definitions), 15)

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
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, stepErr)
		require.Len(t, result.Commands, expectedCommands)
		for _, command := range result.Commands {
			require.Equal(t, service.CityCommandStatusApplied, command.Status, "%+v", command)
		}
		return result
	}

	submit(0, "runtime-create-character", service.CityCommandTypeActorCreate, marshalPayload(map[string]any{
		"archetype_code": "urban_apprentice", "name": "Aster",
	}))
	created := step(0, "runtime-create-character-step", 1)
	require.Len(t, created.WorldRuntimeFacts, 1)
	require.Equal(t, service.WorldRuntimeFactActorCreated, created.WorldRuntimeFacts[0].FactType)
	actors, err := cityService.ListWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	actorCode := actors[0].Code
	actorBefore, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	require.Len(t, actorBefore.Attributes, 5)
	require.Len(t, actorBefore.Roles, 2)

	for index := 0; index < 2; index++ {
		submit(1, fmt.Sprintf("runtime-study-%d", index), service.CityCommandTypeActorActivityPerform,
			marshalPayload(map[string]any{"actor_code": actorCode, "activity_code": "technical_study"}))
	}
	studied := step(1, "runtime-study-step", 2)
	require.Len(t, studied.WorldRuntimeFacts, 2)
	roleOptions, err := cityService.GetWorldActorRoleOptions(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	technicianReady := false
	for _, option := range roleOptions {
		if option.Definition.Code == "profession.technician" {
			technicianReady = option.Evaluation.Satisfied
		}
	}
	require.True(t, technicianReady)

	submit(2, "runtime-role-technician", service.CityCommandTypeActorRoleTransition,
		marshalPayload(map[string]any{"actor_code": actorCode, "role_code": "profession.technician"}))
	transitioned := step(2, "runtime-role-technician-step", 1)
	require.Len(t, transitioned.WorldRuntimeFacts, 1)
	require.Equal(t, service.WorldRuntimeFactRoleTransitioned, transitioned.WorldRuntimeFacts[0].FactType)

	for index := 0; index < 3; index++ {
		submit(3, fmt.Sprintf("runtime-noise-%d", index), service.CityCommandTypeActorActivityPerform,
			marshalPayload(map[string]any{"actor_code": actorCode, "activity_code": "disruptive_noise"}))
	}
	violations := step(3, "runtime-noise-step", 3)
	require.Len(t, violations.WorldRuleCases, 3)
	require.Len(t, violations.WorldRuntimeFacts, 6)
	firstCasePage, err := cityService.QueryWorldRuleCases(ctx, service.WorldRuleCaseQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode, Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, firstCasePage.Items, 2)
	require.NotNil(t, firstCasePage.NextCursor)
	require.Greater(t, firstCasePage.Items[0].Sequence, firstCasePage.Items[1].Sequence)
	secondCasePage, err := cityService.QueryWorldRuleCases(ctx, service.WorldRuleCaseQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode, Limit: 2,
		AfterTick: firstCasePage.NextCursor.Tick, AfterSequence: firstCasePage.NextCursor.Sequence,
	})
	require.NoError(t, err)
	require.Len(t, secondCasePage.Items, 1)
	require.Nil(t, secondCasePage.NextCursor)
	_, err = cityService.QueryWorldRuleCases(ctx, service.WorldRuleCaseQueryInput{
		UserID: outsider.ID, WorldID: worldID, Limit: 2,
	})
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)
	actorSanctioned, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	require.Len(t, actorSanctioned.Statuses, 2)
	activeStatuses := make(map[string]service.WorldActorStatus)
	for _, status := range actorSanctioned.Statuses {
		if status.Lifecycle == "active" {
			activeStatuses[status.StatusCode] = status
		}
	}
	require.Equal(t, 2, activeStatuses["civic_warning"].Stacks)
	require.Equal(t, 1, activeStatuses["community_service_order"].Stacks)

	step(4, "runtime-idle-step-5", 0)
	step(5, "runtime-idle-step-6", 0)
	expired := step(6, "runtime-idle-step-7", 0)
	require.Len(t, expired.WorldRuntimeFacts, 1)
	require.Equal(t, service.WorldRuntimeFactStatusExpired, expired.WorldRuntimeFacts[0].FactType)
	require.Len(t, expired.WorldEffectOperations, 1)
	require.Equal(t, service.WorldRuntimeEffectStatusExpire, expired.WorldEffectOperations[0].EffectType)
	actorAfterExpiration, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	for _, status := range actorAfterExpiration.Statuses {
		if status.StatusCode == "civic_warning" {
			require.Equal(t, "expired", status.Lifecycle)
			require.NotNil(t, status.EndedTick)
			require.Equal(t, int64(7), *status.EndedTick)
		}
	}

	fromGenesis, targetTick := int64(0), int64(7)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "runtime-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	if replay.ErrorDetail != nil {
		t.Logf("world runtime replay detail: %s", *replay.ErrorDetail)
	}
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "runtime-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	if recovery.ErrorDetail != nil {
		t.Logf("world runtime recovery detail: %s", *recovery.ErrorDetail)
	}
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	require.Equal(t, actorAfterExpiration, restored)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE world_actor_attributes
SET value_units = value_units + 1
WHERE world_id = $1`, worldID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `
DELETE FROM world_runtime_facts WHERE world_id = $1`, worldID)
	require.Error(t, err)
}

func TestCityF7V4ToV5UpgradeInstallsVersionedOpenWorldRuntime(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("open-world-upgrade-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(760002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V4,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	enterpriseBefore, err := cityService.GetEnterpriseLocationState(ctx, service.CityEnterpriseLocationQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	_, err = cityService.GetWorldRuntimeCatalog(ctx, owner.ID, worldID)
	require.ErrorIs(t, err, service.ErrWorldRuntimeStateNotFound)
	engineBefore, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, []string{service.CitySimulationVersionF7V5}, engineBefore.UpgradeTargets)

	planned, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "open-world-upgrade-plan",
		TargetVersion: service.CitySimulationVersionF7V5, DryRun: true,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusPlanned, planned.Status, "%+v", planned)
	var profileCount int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM world_runtime_profiles WHERE world_id = $1`, worldID,
	).Scan(&profileCount))
	require.Zero(t, profileCount)

	applied, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "open-world-upgrade-apply",
		TargetVersion: service.CitySimulationVersionF7V5,
	})
	require.NoError(t, err)
	if applied.ErrorDetail != nil {
		t.Logf("open world upgrade detail: %s", *applied.ErrorDetail)
	}
	require.Equal(t, service.CityUpgradeStatusApplied, applied.Status, "%+v", applied)
	engineAfter, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7V5, engineAfter.Version)
	require.Contains(t, engineAfter.Stages, "world_runtime")
	require.Empty(t, engineAfter.UpgradeTargets)
	catalog, err := cityService.GetWorldRuntimeCatalog(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Zero(t, catalog.Profile.ActorCount)
	require.Zero(t, catalog.Profile.FactCount)
	require.Equal(t, int64(1), catalog.Profile.Revision)
	enterpriseAfter, err := cityService.GetEnterpriseLocationState(ctx, service.CityEnterpriseLocationQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	require.Equal(t, enterpriseBefore.Profile, enterpriseAfter.Profile)
	require.Equal(t, enterpriseBefore.Sites, enterpriseAfter.Sites)

	expectedZero := int64(0)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "open-world-upgraded-character",
		CommandType:       service.CityCommandTypeActorCreate,
		Payload:           []byte(`{"archetype_code":"resident_generalist","name":"Nova"}`),
		ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	step, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "open-world-upgraded-step",
		ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7V5, step.Tick.SimulationVersion)
	require.Len(t, step.WorldRuntimeFacts, 1)
	fromUpgrade, targetTick := int64(0), int64(1)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "open-world-upgraded-replay",
		FromTick: &fromUpgrade, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
}
