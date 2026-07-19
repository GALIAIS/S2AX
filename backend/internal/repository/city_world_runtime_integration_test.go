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
	require.Equal(t, []string{service.CitySimulationVersionF7V6}, engineAfter.UpgradeTargets)
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

	spatialUpgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "open-world-spatial-control-upgrade",
		TargetVersion: service.CitySimulationVersionF7V6,
	})
	require.NoError(t, err)
	if spatialUpgrade.ErrorDetail != nil {
		t.Logf("spatial-control upgrade detail: %s", *spatialUpgrade.ErrorDetail)
	}
	require.Equal(t, service.CityUpgradeStatusApplied, spatialUpgrade.Status, "%+v", spatialUpgrade)
	upgradedActors, err := cityService.ListWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, upgradedActors, 1)
	require.NotNil(t, upgradedActors[0].Location)
	upgradedActor, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, upgradedActors[0].Code)
	require.NoError(t, err)
	require.NotNil(t, upgradedActor.Location)
	require.Len(t, upgradedActor.ControlGrants, 2)
	require.ElementsMatch(t, []string{
		service.WorldActorCapabilityCommand,
		service.WorldActorCapabilityManageControl,
	}, upgradedActor.Capabilities)
	navigationUpgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "open-world-navigation-upgrade",
		TargetVersion: service.CitySimulationVersionF7V7,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, navigationUpgrade.Status, "%+v", navigationUpgrade)
	navigationEngine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7V7, navigationEngine.Version)
	require.Equal(t, []string{service.CitySimulationVersionF7V8}, navigationEngine.UpgradeTargets)
	navigationActor, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, upgradedActors[0].Code)
	require.NoError(t, err)
	require.Equal(t, upgradedActor, navigationActor)
	require.NotNil(t, navigationActor.Location)
	unavailablePath, err := cityService.FindWorldActorPath(ctx, service.CityNavigationPathInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: upgradedActors[0].Code,
		Destination: service.CityNavigationCoordinate{
			X: navigationActor.Location.X + 1,
			Y: navigationActor.Location.Y,
			Z: navigationActor.Location.Z,
		},
		MaxSteps: 8,
	})
	require.NoError(t, err)
	require.False(t, unavailablePath.Reachable)
	require.Equal(t, service.CityNavigationBlockChunkUnavailable, unavailablePath.Reason)

	portalUpgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "open-world-portal-access-upgrade",
		TargetVersion: service.CitySimulationVersionF7V8,
	})
	require.NoError(t, err)
	if portalUpgrade.ErrorDetail != nil {
		t.Logf("portal-access upgrade detail: %s", *portalUpgrade.ErrorDetail)
	}
	require.Equal(t, service.CityUpgradeStatusApplied, portalUpgrade.Status, "%+v", portalUpgrade)
	portalEngine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7V8, portalEngine.Version)
	require.Equal(t, []string{service.CitySimulationVersionF7V9}, portalEngine.UpgradeTargets)
	portalActor, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, upgradedActors[0].Code)
	require.NoError(t, err)
	require.Equal(t, navigationActor, portalActor)
	portalCatalog, err := cityService.GetWorldRuntimeCatalog(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "1.2.0", portalCatalog.Profile.RuntimeVersion)
	portals, err := cityService.ListWorldPortalStates(ctx, service.WorldPortalAccessQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: upgradedActors[0].Code,
	})
	require.NoError(t, err)
	require.NotEmpty(t, portals)
	for _, portal := range portals {
		require.Equal(t, service.WorldPortalStateOpen, portal.State.StateCode)
		require.NotNil(t, portal.Accessible)
		require.True(t, *portal.Accessible)
	}

	intentUpgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "open-world-navigation-intent-upgrade",
		TargetVersion: service.CitySimulationVersionF7V9,
	})
	require.NoError(t, err)
	if intentUpgrade.ErrorDetail != nil {
		t.Logf("navigation-intent upgrade detail: %s", *intentUpgrade.ErrorDetail)
	}
	require.Equal(t, service.CityUpgradeStatusApplied, intentUpgrade.Status, "%+v", intentUpgrade)
	intentEngine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7V9, intentEngine.Version)
	require.Equal(t, []string{service.CitySimulationVersionF8}, intentEngine.UpgradeTargets)
	intentCatalog, err := cityService.GetWorldRuntimeCatalog(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "1.3.0", intentCatalog.Profile.RuntimeVersion)
	intents, err := cityService.ListWorldNavigationIntents(ctx, service.WorldNavigationIntentQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	require.Empty(t, intents)

	servicePlan, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "open-world-public-service-plan",
		TargetVersion: service.CitySimulationVersionF8, DryRun: true,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusPlanned, servicePlan.Status, "%+v", servicePlan)
	var serviceProfileCount int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM city_service_profiles WHERE world_id = $1`, worldID,
	).Scan(&serviceProfileCount))
	require.Zero(t, serviceProfileCount)

	serviceUpgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "open-world-public-service-upgrade",
		TargetVersion: service.CitySimulationVersionF8,
	})
	require.NoError(t, err)
	if serviceUpgrade.ErrorDetail != nil {
		t.Logf("public-service upgrade detail: %s", *serviceUpgrade.ErrorDetail)
	}
	require.Equal(t, service.CityUpgradeStatusApplied, serviceUpgrade.Status, "%+v", serviceUpgrade)
	serviceEngine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF8, serviceEngine.Version)
	require.Contains(t, serviceEngine.Stages, "public_services")
	require.Empty(t, serviceEngine.UpgradeTargets)
	serviceCatalog, err := cityService.GetCityServiceCatalog(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityServiceAvailabilityAvailable, serviceCatalog.Availability)
	require.NotNil(t, serviceCatalog.Profile)
	require.Zero(t, serviceCatalog.Profile.FacilityCount)
	require.Zero(t, serviceCatalog.Profile.FactCount)
	require.EqualValues(t, 1, serviceCatalog.Profile.Revision)
	actorAfterServiceUpgrade, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, upgradedActors[0].Code)
	require.NoError(t, err)
	require.Equal(t, portalActor, actorAfterServiceUpgrade)
}

func TestWorldPortalAccessStatePolicyNavigationAndRecovery(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("portal-access-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(760008)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Portal Access Runtime", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V8,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	currentTick := int64(0)
	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	submit := func(key, commandType string, payload any) *service.CityCommand {
		expectedTick := currentTick
		command, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: marshalPayload(payload), ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, submitErr)
		return command
	}
	step := func(key string) *service.CityStepResult {
		expectedTick := currentTick
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, stepErr)
		currentTick++
		require.Equal(t, currentTick, result.Tick.Tick)
		return result
	}
	stepApplied := func(key string) *service.CityStepResult {
		result := step(key)
		for _, command := range result.Commands {
			require.Equal(t, service.CityCommandStatusApplied, command.Status, "%+v", command)
		}
		return result
	}

	submit("portal-actor-create", service.CityCommandTypeActorCreate, map[string]any{
		"archetype_code": "urban_apprentice", "name": "Aster",
	})
	stepApplied("portal-actor-create-step")
	actors, err := cityService.ListWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	actorCode := actors[0].Code
	actorState, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	require.NotNil(t, actorState.Location)

	portals, err := cityService.ListWorldPortalStates(ctx, service.WorldPortalAccessQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
	})
	require.NoError(t, err)
	require.NotEmpty(t, portals)
	floorChunk := func(value int64) int64 {
		result := value / 32
		if value < 0 && value%32 != 0 {
			result--
		}
		return result
	}
	abs := func(value int64) int64 {
		if value < 0 {
			return -value
		}
		return value
	}
	var selected *service.WorldPortalAccessView
	bestDistance := int64(1 << 62)
	for index := range portals {
		candidate := &portals[index]
		if candidate.State.PortalType != "entrance" || candidate.From.Z != actorState.Location.Z ||
			floorChunk(candidate.From.X) != actorState.Location.ChunkX ||
			floorChunk(candidate.From.Y) != actorState.Location.ChunkY {
			continue
		}
		distance := abs(candidate.From.X-actorState.Location.X) + abs(candidate.From.Y-actorState.Location.Y)
		if distance < bestDistance {
			selected, bestDistance = candidate, distance
		}
	}
	require.NotNil(t, selected, "expected a surface entrance in the actor's spawn Chunk")
	require.NotNil(t, selected.Accessible)
	require.True(t, *selected.Accessible)

	submit("portal-generate-spawn-chunk", service.CityCommandTypeSpatialGenerateChunk, map[string]any{
		"chunk_x": actorState.Location.ChunkX, "chunk_y": actorState.Location.ChunkY, "z": actorState.Location.Z,
	})
	stepApplied("portal-generate-spawn-chunk-step")
	path, err := cityService.FindWorldActorPath(ctx, service.CityNavigationPathInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
		Destination: selected.From, MaxSteps: 256,
	})
	require.NoError(t, err)
	require.True(t, path.Reachable, "%+v", path)
	for index, pathStep := range path.Steps[1:] {
		submit(fmt.Sprintf("portal-approach-%03d", index), service.CityCommandTypeActorLocationMove, map[string]any{
			"actor_code": actorCode, "x": pathStep.Coordinate.X,
			"y": pathStep.Coordinate.Y, "z": pathStep.Coordinate.Z,
		})
	}
	if len(path.Steps) > 1 {
		stepApplied("portal-approach-step")
	}
	actorState, err = cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	require.Equal(t, selected.From.X, actorState.Location.X)
	require.Equal(t, selected.From.Y, actorState.Location.Y)
	require.Equal(t, selected.From.Z, actorState.Location.Z)

	submit("portal-close", service.CityCommandTypePortalStateTransition, map[string]any{
		"actor_code": actorCode, "building_code": selected.State.BuildingCode,
		"portal_code": selected.State.PortalCode, "action": service.WorldPortalActionClose,
	})
	closedStep := stepApplied("portal-close-step")
	require.Len(t, closedStep.WorldRuntimeFacts, 1)
	require.Equal(t, service.WorldRuntimeFactPortalStateChanged, closedStep.WorldRuntimeFacts[0].FactType)
	portals, err = cityService.ListWorldPortalStates(ctx, service.WorldPortalAccessQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
	})
	require.NoError(t, err)
	closedPortal := portals[0]
	for _, portal := range portals {
		if portal.State.BuildingCode == selected.State.BuildingCode && portal.State.PortalCode == selected.State.PortalCode {
			closedPortal = portal
			break
		}
	}
	require.Equal(t, service.WorldPortalStateClosed, closedPortal.State.StateCode)
	closedPath, err := cityService.FindWorldActorPath(ctx, service.CityNavigationPathInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
		Destination: selected.To, MaxSteps: 64,
	})
	require.NoError(t, err)
	require.False(t, closedPath.Reachable)
	require.Equal(t, service.CityNavigationBlockPortalClosed, closedPath.Reason)

	submit("portal-closed-move", service.CityCommandTypeActorLocationMove, map[string]any{
		"actor_code": actorCode, "x": selected.To.X, "y": selected.To.Y, "z": selected.To.Z,
	})
	rejectedMove := step("portal-closed-move-step")
	require.Len(t, rejectedMove.Commands, 1)
	require.Equal(t, service.CityCommandStatusRejected, rejectedMove.Commands[0].Status)
	require.NotNil(t, rejectedMove.Commands[0].ErrorCode)
	require.Equal(t, "WORLD_NAVIGATION_PORTAL_CLOSED", *rejectedMove.Commands[0].ErrorCode)

	submit("portal-open", service.CityCommandTypePortalStateTransition, map[string]any{
		"actor_code": actorCode, "building_code": selected.State.BuildingCode,
		"portal_code": selected.State.PortalCode, "action": service.WorldPortalActionOpen,
	})
	submit("portal-policy-technician", service.CityCommandTypePortalAccessConfigure, map[string]any{
		"building_code": selected.State.BuildingCode, "portal_code": selected.State.PortalCode,
		"requirements": service.WorldRequirementNode{
			Operator: service.WorldRequirementRoleActive, RoleCode: "profession.technician",
		},
	})
	policyStep := stepApplied("portal-policy-technician-step")
	require.Len(t, policyStep.WorldRuntimeFacts, 2)
	portals, err = cityService.ListWorldPortalStates(ctx, service.WorldPortalAccessQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
	})
	require.NoError(t, err)
	for _, portal := range portals {
		if portal.State.BuildingCode == selected.State.BuildingCode && portal.State.PortalCode == selected.State.PortalCode {
			require.Equal(t, service.WorldPortalStateOpen, portal.State.StateCode)
			require.NotNil(t, portal.Accessible)
			require.False(t, *portal.Accessible)
			require.NotNil(t, portal.AccessEvaluation)
			require.False(t, portal.AccessEvaluation.Satisfied)
		}
	}
	deniedPath, err := cityService.FindWorldActorPath(ctx, service.CityNavigationPathInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
		Destination: selected.To, MaxSteps: 64,
	})
	require.NoError(t, err)
	require.False(t, deniedPath.Reachable)
	require.Equal(t, service.CityNavigationBlockPortalAccess, deniedPath.Reason)

	for index := 0; index < 2; index++ {
		submit(fmt.Sprintf("portal-technical-study-%d", index), service.CityCommandTypeActorActivityPerform, map[string]any{
			"actor_code": actorCode, "activity_code": "technical_study",
		})
	}
	submit("portal-technician-role", service.CityCommandTypeActorRoleTransition, map[string]any{
		"actor_code": actorCode, "role_code": "profession.technician",
	})
	stepApplied("portal-technician-role-step")
	portals, err = cityService.ListWorldPortalStates(ctx, service.WorldPortalAccessQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
	})
	require.NoError(t, err)
	var authorizedPortal service.WorldPortalAccessView
	for _, portal := range portals {
		if portal.State.BuildingCode == selected.State.BuildingCode && portal.State.PortalCode == selected.State.PortalCode {
			authorizedPortal = portal
			break
		}
	}
	require.NotNil(t, authorizedPortal.Accessible)
	require.True(t, *authorizedPortal.Accessible)

	submit("portal-authorized-move", service.CityCommandTypeActorLocationMove, map[string]any{
		"actor_code": actorCode, "x": selected.To.X, "y": selected.To.Y, "z": selected.To.Z,
	})
	stepApplied("portal-authorized-move-step")
	canonicalActor, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	require.Equal(t, selected.To.X, canonicalActor.Location.X)
	canonicalPortals, err := cityService.ListWorldPortalStates(ctx, service.WorldPortalAccessQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
	})
	require.NoError(t, err)

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "portal-access-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	if replay.ErrorDetail != nil {
		t.Logf("portal-access replay detail: %s", *replay.ErrorDetail)
	}
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "portal-access-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restoredActor, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	require.Equal(t, canonicalActor, restoredActor)
	restoredPortals, err := cityService.ListWorldPortalStates(ctx, service.WorldPortalAccessQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
	})
	require.NoError(t, err)
	require.Equal(t, canonicalPortals, restoredPortals)
}

func TestWorldNavigationIntentBudgetProgressReplayAndRecovery(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("navigation-intent-owner-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(760011)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Navigation Intent Runtime", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V9,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	currentTick := int64(0)
	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	submit := func(key, commandType string, payload any) *service.CityCommand {
		expectedTick := currentTick
		command, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: marshalPayload(payload),
			ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, submitErr)
		return command
	}
	step := func(key string) *service.CityStepResult {
		expectedTick := currentTick
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, stepErr)
		currentTick++
		require.Equal(t, currentTick, result.Tick.Tick)
		return result
	}

	submit("intent-actor-create", service.CityCommandTypeActorCreate, map[string]any{
		"archetype_code": "resident_generalist", "name": "Walker",
	})
	created := step("intent-actor-create-step")
	require.Len(t, created.Commands, 1)
	require.Equal(t, service.CityCommandStatusApplied, created.Commands[0].Status)
	actors, err := cityService.ListWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	actorCode := actors[0].Code
	actorState, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	require.NotNil(t, actorState.Location)
	origin := service.CityNavigationCoordinate{
		X: actorState.Location.X, Y: actorState.Location.Y, Z: actorState.Location.Z,
	}

	submit("intent-generate-spawn-chunk", service.CityCommandTypeSpatialGenerateChunk, map[string]any{
		"chunk_x": actorState.Location.ChunkX, "chunk_y": actorState.Location.ChunkY,
		"z": actorState.Location.Z,
	})
	generated := step("intent-generate-spawn-chunk-step")
	require.Equal(t, service.CityCommandStatusApplied, generated.Commands[0].Status)

	var destination service.CityNavigationCoordinate
	var selectedPath *service.CityNavigationPath
	for _, offset := range []service.CityNavigationCoordinate{
		{X: 1}, {X: -1}, {Y: 1}, {Y: -1},
		{X: 1, Y: 1}, {X: -1, Y: 1}, {X: 1, Y: -1}, {X: -1, Y: -1},
	} {
		candidate := service.CityNavigationCoordinate{
			X: origin.X + offset.X, Y: origin.Y + offset.Y, Z: origin.Z,
		}
		path, pathErr := cityService.FindWorldActorPath(ctx, service.CityNavigationPathInput{
			UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
			Destination: candidate, MaxSteps: 8,
		})
		require.NoError(t, pathErr)
		if path.Reachable && len(path.Steps) == 2 {
			destination, selectedPath = candidate, path
			break
		}
	}
	require.NotNil(t, selectedPath, "expected a reachable adjacent cell from the spawn position")

	submit("intent-set", service.CityCommandTypeActorNavigationIntentSet, map[string]any{
		"actor_code": actorCode, "destination": destination,
		"priority": 2, "max_steps": 8, "on_blocked": service.WorldNavigationOnBlockedRetry,
	})
	setStep := step("intent-set-step")
	require.Equal(t, service.CityCommandStatusApplied, setStep.Commands[0].Status)
	require.GreaterOrEqual(t, len(setStep.WorldRuntimeFacts), 2)
	require.Equal(t, service.WorldRuntimeFactNavigationIntentCreated, setStep.WorldRuntimeFacts[0].FactType)
	require.Equal(t, service.WorldRuntimeFactNavigationIntentWaited, setStep.WorldRuntimeFacts[1].FactType)
	intents, err := cityService.ListWorldNavigationIntents(ctx, service.WorldNavigationIntentQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, service.WorldNavigationReasonBudgetInsufficient, *intents[0].LastReason)

	progressTick := int64(0)
	for attempt := 0; attempt < 6; attempt++ {
		result := step(fmt.Sprintf("intent-automatic-step-%d", attempt))
		for _, fact := range result.WorldRuntimeFacts {
			if fact.FactType == service.WorldRuntimeFactNavigationIntentProgressed {
				progressTick = fact.Tick
			}
		}
		intent, loadErr := cityService.GetWorldNavigationIntent(ctx, service.WorldNavigationIntentQueryInput{
			UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
		})
		require.NoError(t, loadErr)
		if intent.Status == service.WorldNavigationIntentStatusArrived {
			break
		}
	}
	require.NotZero(t, progressTick)
	intent, err := cityService.GetWorldNavigationIntent(ctx, service.WorldNavigationIntentQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
	})
	require.NoError(t, err)
	require.Equal(t, service.WorldNavigationIntentStatusArrived, intent.Status)
	actorState, err = cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	require.Equal(t, destination.X, actorState.Location.X)
	require.Equal(t, destination.Y, actorState.Location.Y)
	require.NotNil(t, actorState.NavigationIntent)
	require.Equal(t, intent.Version, actorState.NavigationIntent.Version)
	reservations, err := cityService.ListWorldNavigationReservations(
		ctx, service.WorldNavigationReservationQueryInput{
			UserID: owner.ID, WorldID: worldID, Tick: &progressTick,
		},
	)
	require.NoError(t, err)
	require.Len(t, reservations, 1)
	require.Equal(t, actorCode, reservations[0].ActorCode)
	require.Equal(t, destination, reservations[0].To)

	arrivedIntent := *intent
	submit("intent-replace", service.CityCommandTypeActorNavigationIntentSet, map[string]any{
		"actor_code": actorCode, "destination": origin,
		"priority": -1, "max_steps": 8, "on_blocked": service.WorldNavigationOnBlockedRetry,
	})
	replaced := step("intent-replace-step")
	require.Equal(t, service.CityCommandStatusApplied, replaced.Commands[0].Status)
	require.GreaterOrEqual(t, len(replaced.WorldRuntimeFacts), 2)
	require.Equal(t, service.WorldRuntimeFactNavigationIntentReplaced, replaced.WorldRuntimeFacts[0].FactType)
	require.Equal(t, service.WorldRuntimeFactNavigationIntentWaited, replaced.WorldRuntimeFacts[1].FactType)
	intent, err = cityService.GetWorldNavigationIntent(ctx, service.WorldNavigationIntentQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
	})
	require.NoError(t, err)
	require.Equal(t, service.WorldNavigationIntentStatusActive, intent.Status)
	require.Equal(t, origin, intent.Destination)
	require.NotEqual(t, arrivedIntent.IntentCode, intent.IntentCode)
	require.Greater(t, intent.Version, arrivedIntent.Version)

	submit("intent-cancel", service.CityCommandTypeActorNavigationIntentCancel, map[string]any{
		"actor_code": actorCode,
	})
	cancelled := step("intent-cancel-step")
	require.Equal(t, service.CityCommandStatusApplied, cancelled.Commands[0].Status)
	require.Equal(t, service.WorldRuntimeFactNavigationIntentCancelled, cancelled.WorldRuntimeFacts[0].FactType)
	intent, err = cityService.GetWorldNavigationIntent(ctx, service.WorldNavigationIntentQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
	})
	require.NoError(t, err)
	require.Equal(t, service.WorldNavigationIntentStatusCancelled, intent.Status)
	require.Equal(t, service.WorldNavigationReasonUserCancelled, *intent.LastReason)

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "navigation-intent-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	if replay.ErrorDetail != nil {
		t.Logf("navigation-intent replay detail: %s", *replay.ErrorDetail)
	}
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "navigation-intent-recovery",
		ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restoredIntent, err := cityService.GetWorldNavigationIntent(ctx, service.WorldNavigationIntentQueryInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: actorCode,
	})
	require.NoError(t, err)
	require.Equal(t, intent, restoredIntent)
	restoredReservations, err := cityService.ListWorldNavigationReservations(
		ctx, service.WorldNavigationReservationQueryInput{
			UserID: owner.ID, WorldID: worldID, Tick: &progressTick,
		},
	)
	require.NoError(t, err)
	require.Equal(t, reservations, restoredReservations)
}

func TestWorldActorSpatialMovementDelegationRevocationAndRecovery(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("spatial-control-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	delegate := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("spatial-control-delegate-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(760003)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Actor Spatial Control", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V6,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: delegate.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)
	members, err := cityService.ListWorldMembers(ctx, delegate.ID, worldID)
	require.NoError(t, err)
	require.Len(t, members, 2)
	updatedMember, err := cityService.UpdateWorldMember(ctx, service.CityMemberUpdateInput{
		UserID: owner.ID, WorldID: worldID, TargetUserID: delegate.ID, Role: service.CityMemberRolePlanner,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityMemberRolePlanner, updatedMember.Role)

	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	submit := func(userID, expectedTick int64, key, commandType string, payload any) *service.CityCommand {
		command, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: userID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: marshalPayload(payload), ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, submitErr)
		return command
	}
	step := func(expectedTick int64, key string) *service.CityStepResult {
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, stepErr)
		for _, command := range result.Commands {
			require.Equal(t, service.CityCommandStatusApplied, command.Status, "%+v", command)
		}
		return result
	}

	catalog, err := cityService.GetWorldRuntimeCatalog(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "1.1.0", catalog.Profile.RuntimeVersion)
	submit(owner.ID, 0, "spatial-create", service.CityCommandTypeActorCreate, map[string]any{
		"archetype_code": "resident_generalist", "name": "Nova",
	})
	created := step(0, "spatial-create-step")
	require.Len(t, created.WorldRuntimeFacts, 1)
	actors, err := cityService.ListWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	require.NotNil(t, actors[0].Location)
	actorCode := actors[0].Code
	ownerState, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	require.NotNil(t, ownerState.Location)
	require.Len(t, ownerState.ControlGrants, 2)
	require.ElementsMatch(t, []string{
		service.WorldActorCapabilityCommand,
		service.WorldActorCapabilityManageControl,
	}, ownerState.Capabilities)

	grantCommand := submit(owner.ID, 1, "spatial-control-grant", service.CityCommandTypeActorControlGrant, map[string]any{
		"actor_code": actorCode, "user_id": delegate.ID,
		"capabilities": []string{service.WorldActorCapabilityCommand},
	})
	granted := step(1, "spatial-control-grant-step")
	require.Len(t, granted.WorldRuntimeFacts, 1)
	require.Equal(t, service.WorldRuntimeFactControlGranted, granted.WorldRuntimeFacts[0].FactType)
	delegateState, err := cityService.GetWorldActorState(ctx, delegate.ID, worldID, actorCode)
	require.NoError(t, err)
	require.Equal(t, []string{service.WorldActorCapabilityCommand}, delegateState.Capabilities)
	require.Empty(t, delegateState.ControlGrants)
	_, err = cityService.UpdateWorldMember(ctx, service.CityMemberUpdateInput{
		UserID: owner.ID, WorldID: worldID, TargetUserID: delegate.ID, Status: service.CityMemberStatusLeft,
	})
	require.ErrorIs(t, err, service.ErrCityMemberGrants)

	start := *delegateState.Location
	moveCommand := submit(delegate.ID, 2, "spatial-move-east", service.CityCommandTypeActorLocationMove, map[string]any{
		"actor_code": actorCode, "x": start.X + 1, "y": start.Y, "z": start.Z,
	})
	scheduler := service.NewCityTickScheduler(integrationDB, cityService, time.Hour, 10)
	schedulerReport, err := scheduler.ProcessDue(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, schedulerReport.Processed)
	var schedulerFailures int
	var schedulerLeaseCleared, schedulerRetryCleared, schedulerSucceeded bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT consecutive_failures, lease_token IS NULL, retry_not_before IS NULL, last_success_at IS NOT NULL
FROM city_world_schedule_states WHERE world_id = $1`, worldID).Scan(
		&schedulerFailures, &schedulerLeaseCleared, &schedulerRetryCleared, &schedulerSucceeded,
	))
	require.Zero(t, schedulerFailures)
	require.True(t, schedulerLeaseCleared)
	require.True(t, schedulerRetryCleared)
	require.True(t, schedulerSucceeded)
	moveReceipt, err := cityService.GetCommand(ctx, delegate.ID, worldID, moveCommand.ID)
	require.NoError(t, err)
	require.Equal(t, service.CityCommandStatusApplied, moveReceipt.Status)
	require.NotNil(t, moveReceipt.ProcessedTick)
	require.Equal(t, int64(3), *moveReceipt.ProcessedTick)
	delegateCommandPage, err := cityService.ListCommands(ctx, service.CityCommandListInput{
		UserID: delegate.ID, WorldID: worldID, Limit: 20,
	})
	require.NoError(t, err)
	require.Len(t, delegateCommandPage.Items, 1)
	require.Equal(t, moveCommand.ID, delegateCommandPage.Items[0].ID)
	_, err = cityService.GetCommand(ctx, delegate.ID, worldID, grantCommand.ID)
	require.ErrorIs(t, err, service.ErrCityCommandNotFound)
	movedState, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	require.Equal(t, start.X+1, movedState.Location.X)
	require.Equal(t, int64(3), movedState.Location.MovedTick)

	submit(owner.ID, 3, "spatial-control-revoke", service.CityCommandTypeActorControlRevoke, map[string]any{
		"actor_code": actorCode, "user_id": delegate.ID,
		"capabilities": []string{service.WorldActorCapabilityCommand},
	})
	revoked := step(3, "spatial-control-revoke-step")
	require.Len(t, revoked.WorldRuntimeFacts, 1)
	require.Equal(t, service.WorldRuntimeFactControlRevoked, revoked.WorldRuntimeFacts[0].FactType)
	_, err = cityService.GetWorldActorState(ctx, delegate.ID, worldID, actorCode)
	require.ErrorIs(t, err, service.ErrWorldActorNotFound)
	delegateActors, err := cityService.ListWorldActors(ctx, delegate.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, delegateActors)
	leftMember, err := cityService.UpdateWorldMember(ctx, service.CityMemberUpdateInput{
		UserID: owner.ID, WorldID: worldID, TargetUserID: delegate.ID, Status: service.CityMemberStatusLeft,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityMemberStatusLeft, leftMember.Status)
	_, err = cityService.ListWorldMembers(ctx, delegate.ID, worldID)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: delegate.Email, Role: service.CityMemberRolePlanner,
	})
	require.NoError(t, err)

	canonicalBeforeRecovery, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	fromGenesis, targetTick := int64(0), int64(4)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "spatial-control-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	if replay.Status != service.CityReplayStatusVerified {
		t.Logf("spatial-control replay divergence: tick=%v path=%v expected=%v actual=%v detail=%v",
			replay.DivergenceTick, replay.DivergencePath, replay.ExpectedStateHash, replay.ActualStateHash, replay.ErrorDetail)
		if replay.DivergenceTick != nil {
			t.Logf("spatial-control replay divergence tick value: %d", *replay.DivergenceTick)
		}
		if replay.DivergencePath != nil {
			t.Logf("spatial-control replay divergence path value: %s", *replay.DivergencePath)
		}
	}
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "spatial-control-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, actorCode)
	require.NoError(t, err)
	require.Equal(t, canonicalBeforeRecovery, restored)

	submit(delegate.ID, 4, "delegate-spatial-create", service.CityCommandTypeActorCreate, map[string]any{
		"archetype_code": "resident_generalist", "name": "Mira",
	})
	delegateCreated := step(4, "delegate-spatial-create-step")
	require.Len(t, delegateCreated.WorldRuntimeFacts, 1)
	delegateActors, err = cityService.ListWorldActors(ctx, delegate.ID, worldID)
	require.NoError(t, err)
	require.Len(t, delegateActors, 1)
	require.NotNil(t, delegateActors[0].Location)
	expectedOwnerDeniedTick := int64(5)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "owner-cannot-command-member-actor",
		CommandType: service.CityCommandTypeActorLocationMove,
		Payload: marshalPayload(map[string]any{
			"actor_code": delegateActors[0].Code,
			"x":          delegateActors[0].Location.X + 1,
			"y":          delegateActors[0].Location.Y,
			"z":          delegateActors[0].Location.Z,
		}),
		ExpectedWorldTick: &expectedOwnerDeniedTick,
	})
	require.ErrorIs(t, err, service.ErrCityPermissionDenied)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE world_actor_locations SET x = x + 1 WHERE world_id = $1`, worldID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE world_actor_control_grants SET status = 'revoked' WHERE world_id = $1`, worldID)
	require.Error(t, err)
}

func TestWorldActorNavigationUsesGeneratedTerrainAndCanonicalOccupancy(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("navigation-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	blockerOwner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("navigation-blocker-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(790001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Deterministic Navigation", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V7,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: blockerOwner.Email, Role: service.CityMemberRolePlanner,
	})
	require.NoError(t, err)
	expectedZero := int64(0)
	commands := []service.CityCommandSubmitInput{
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "navigation-generate-center",
			CommandType: service.CityCommandTypeSpatialGenerateChunk,
			Payload:     []byte(`{"chunk_x":0,"chunk_y":0,"z":0}`), ExpectedWorldTick: &expectedZero,
		},
		{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: "navigation-create-mover",
			CommandType: service.CityCommandTypeActorCreate,
			Payload:     []byte(`{"archetype_code":"resident_generalist","name":"Mover"}`), ExpectedWorldTick: &expectedZero,
		},
		{
			UserID: blockerOwner.ID, WorldID: worldID, IdempotencyKey: "navigation-create-blocker",
			CommandType: service.CityCommandTypeActorCreate,
			Payload:     []byte(`{"archetype_code":"resident_generalist","name":"Blocker"}`), ExpectedWorldTick: &expectedZero,
		},
	}
	for _, input := range commands {
		_, err = cityService.SubmitCommand(ctx, input)
		require.NoError(t, err)
	}
	created, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "navigation-create-step",
		ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, 3, created.Tick.AppliedCommandCount)
	require.Zero(t, created.Tick.RejectedCommandCount)
	require.Equal(t, service.CitySimulationVersionF7V7, created.Tick.SimulationVersion)

	actors, err := cityService.ListWorldActors(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	mover := actors[0]
	require.NotNil(t, mover.Location)
	origin := service.CityNavigationCoordinate{X: mover.Location.X, Y: mover.Location.Y, Z: mover.Location.Z}
	candidates := []service.CityNavigationCoordinate{
		{X: origin.X, Y: origin.Y - 1, Z: origin.Z},
		{X: origin.X + 1, Y: origin.Y, Z: origin.Z},
		{X: origin.X, Y: origin.Y + 1, Z: origin.Z},
		{X: origin.X - 1, Y: origin.Y, Z: origin.Z},
	}
	var path *service.CityNavigationPath
	for _, destination := range candidates {
		candidate, pathErr := cityService.FindWorldActorPath(ctx, service.CityNavigationPathInput{
			UserID: owner.ID, WorldID: worldID, ActorCode: mover.Code,
			Destination: destination, MaxSteps: 16,
		})
		require.NoError(t, pathErr)
		if candidate.Reachable && len(candidate.Steps) >= 2 {
			path = candidate
			break
		}
	}
	require.NotNil(t, path, "generated center Chunk must expose at least one traversable neighboring Cell")
	require.Equal(t, service.CityNavigationVersion, path.NavigationVersion)
	require.Equal(t, int64(1), path.WorldTick)
	require.Equal(t, origin, path.From)
	require.NotEmpty(t, path.SpatialRuleHash)
	repeated, err := cityService.FindWorldActorPath(ctx, service.CityNavigationPathInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: mover.Code,
		Destination: path.To, MaxSteps: 16,
	})
	require.NoError(t, err)
	require.Equal(t, path, repeated)

	next := path.Steps[1].Coordinate
	expectedOne := int64(1)
	movePayload, err := json.Marshal(map[string]any{
		"actor_code": mover.Code, "x": next.X, "y": next.Y, "z": next.Z,
	})
	require.NoError(t, err)
	moveCommand, err := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "navigation-move-first-step",
		CommandType: service.CityCommandTypeActorLocationMove,
		Payload:     movePayload, ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	moved, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "navigation-move-step",
		ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityCommandStatusApplied, moved.Commands[0].Status)
	require.Equal(t, moveCommand.ID, moved.Commands[0].ID)
	movedState, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, mover.Code)
	require.NoError(t, err)
	require.Equal(t, next.X, movedState.Location.X)
	require.Equal(t, next.Y, movedState.Location.Y)
	require.Equal(t, next.Z, movedState.Location.Z)

	occupiedPath, err := cityService.FindWorldActorPath(ctx, service.CityNavigationPathInput{
		UserID: owner.ID, WorldID: worldID, ActorCode: mover.Code,
		Destination: origin, MaxSteps: 16,
	})
	require.NoError(t, err)
	require.False(t, occupiedPath.Reachable)
	require.Equal(t, service.CityNavigationBlockOccupied, occupiedPath.Reason)

	expectedTwo := int64(2)
	blockedPayload, err := json.Marshal(map[string]any{
		"actor_code": mover.Code, "x": origin.X, "y": origin.Y, "z": origin.Z,
	})
	require.NoError(t, err)
	blockedCommand, err := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "navigation-blocked-move",
		CommandType: service.CityCommandTypeActorLocationMove,
		Payload:     blockedPayload, ExpectedWorldTick: &expectedTwo,
	})
	require.NoError(t, err)
	blocked, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "navigation-blocked-step",
		ExpectedWorldTick: &expectedTwo,
	})
	require.NoError(t, err)
	require.Len(t, blocked.Commands, 1)
	require.Equal(t, blockedCommand.ID, blocked.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusRejected, blocked.Commands[0].Status)
	require.NotNil(t, blocked.Commands[0].ErrorCode)
	require.Equal(t, "WORLD_NAVIGATION_CELL_OCCUPIED", *blocked.Commands[0].ErrorCode)
	unchanged, err := cityService.GetWorldActorState(ctx, owner.ID, worldID, mover.Code)
	require.NoError(t, err)
	require.Equal(t, movedState.Location, unchanged.Location)
}

func TestCityPublicServiceAllocationQueriesReplayAndRecovery(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("public-service-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(760012)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Public Service Foundation", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF8,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	currentTick := int64(0)

	land, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	require.NotEmpty(t, land.Buildings)
	building := land.Buildings[0]
	districtCode := building.DistrictCode

	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	submit := func(key, commandType string, payload any) *service.CityCommand {
		expectedTick := currentTick
		command, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: marshalPayload(payload),
			ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, submitErr)
		return command
	}
	step := func(key string, expectedCommandCount int) *service.CityStepResult {
		expectedTick := currentTick
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, stepErr)
		currentTick++
		require.Equal(t, currentTick, result.Tick.Tick)
		require.Len(t, result.Commands, expectedCommandCount)
		for _, command := range result.Commands {
			require.Equal(t, service.CityCommandStatusApplied, command.Status, "%+v", command)
		}
		return result
	}

	catalog, err := cityService.GetCityServiceCatalog(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityServiceAvailabilityAvailable, catalog.Availability)
	require.Equal(t, service.CitySimulationVersionF8, catalog.SimulationVersion)
	require.Len(t, catalog.ServiceDefinitions, 8)
	require.Len(t, catalog.FacilityTypes, 9)
	require.NotNil(t, catalog.Profile)
	require.Zero(t, catalog.Profile.FacilityCount)

	submit("service-resume-world", service.CityCommandTypeWorldResume, map[string]any{})
	resumed := step("service-resume-world-step", 1)
	require.Empty(t, resumed.ServiceFacts)
	require.Empty(t, resumed.ServiceSettlements)

	submit("service-register-facility", service.CityCommandTypeFacilityRegister, map[string]any{
		"code": "facility_central_power", "name": "Central Power",
		"facility_type_code": "power_plant", "building_code": building.Code,
		"reliability_milli": 950, "metadata": map[string]any{"purpose": "integration"},
	})
	registered := step("service-register-facility-step", 1)
	require.Len(t, registered.ServiceFacts, 1)
	require.Equal(t, service.CityServiceFactFacilityRegistered, registered.ServiceFacts[0].FactType)
	require.Empty(t, registered.ServiceSettlements)

	submit("service-configure-capacity", service.CityCommandTypeFacilityCapacityConfigure, map[string]any{
		"facility_code": "facility_central_power", "service_code": "electric_power",
		"installed_capacity_units": 1000, "availability_milli": 1000,
		"expected_version": 0, "metadata": map[string]any{},
	})
	submit("service-start-facility", service.CityCommandTypeFacilityStatusTransition, map[string]any{
		"facility_code": "facility_central_power", "to_status": service.CityFacilityStatusOperational,
		"expected_version": 1, "metadata": map[string]any{},
	})
	configured := step("service-configure-facility-step", 2)
	require.Len(t, configured.ServiceFacts, 2)
	require.Empty(t, configured.ServiceSettlements)

	submit("service-demand-high", service.CityCommandTypeServiceDemandConfigure, map[string]any{
		"code": "demand_power_high", "service_code": "electric_power",
		"subject_kind": "district", "subject_code": districtCode,
		"requested_units_per_tick": 800, "priority": 900,
		"status": service.CityServiceProjectionStatusActive, "expected_version": 0,
		"metadata": map[string]any{},
	})
	submit("service-demand-low", service.CityCommandTypeServiceDemandConfigure, map[string]any{
		"code": "demand_power_low", "service_code": "electric_power",
		"subject_kind": "building", "subject_code": building.Code,
		"requested_units_per_tick": 800, "priority": 100,
		"status": service.CityServiceProjectionStatusActive, "expected_version": 0,
		"metadata": map[string]any{},
	})
	submit("service-connect-high", service.CityCommandTypeServiceConnectionConfigure, map[string]any{
		"code": "connection_power_high", "facility_code": "facility_central_power",
		"service_code": "electric_power", "demand_code": "demand_power_high",
		"max_flow_units_per_tick": 1000, "loss_milli": 0, "preference": 900,
		"status": service.CityServiceProjectionStatusActive, "expected_version": 0,
		"metadata": map[string]any{},
	})
	submit("service-connect-low", service.CityCommandTypeServiceConnectionConfigure, map[string]any{
		"code": "connection_power_low", "facility_code": "facility_central_power",
		"service_code": "electric_power", "demand_code": "demand_power_low",
		"max_flow_units_per_tick": 1000, "loss_milli": 0, "preference": 900,
		"status": service.CityServiceProjectionStatusActive, "expected_version": 0,
		"metadata": map[string]any{},
	})
	settled := step("service-demand-settlement-step", 4)
	require.Len(t, settled.ServiceFacts, 6)
	require.Len(t, settled.ServiceAllocations, 2)
	require.Len(t, settled.ServiceSettlements, 2)
	require.Equal(t, "demand_power_high", settled.ServiceSettlements[0].DemandCode)
	require.Equal(t, int64(800), settled.ServiceSettlements[0].DeliveredUnits)
	require.Zero(t, settled.ServiceSettlements[0].ShortageUnits)
	require.Equal(t, "demand_power_low", settled.ServiceSettlements[1].DemandCode)
	require.Equal(t, int64(200), settled.ServiceSettlements[1].DeliveredUnits)
	require.Equal(t, int64(600), settled.ServiceSettlements[1].ShortageUnits)
	dispatched := int64(0)
	for _, allocation := range settled.ServiceAllocations {
		dispatched += allocation.DispatchedUnits
	}
	require.Equal(t, int64(1000), dispatched)
	overviewCatalog, err := cityService.GetCityServiceCatalog(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	require.NotNil(t, overviewCatalog.Overview)
	require.Equal(t, int64(1600), overviewCatalog.Overview.LatestRequestedUnits)
	require.Equal(t, int64(1000), overviewCatalog.Overview.LatestDeliveredUnits)
	require.Equal(t, int64(600), overviewCatalog.Overview.LatestShortageUnits)
	require.Equal(t, 625, overviewCatalog.Overview.LatestWeightedQualityMilli)
	require.NotNil(t, overviewCatalog.Overview.LatestSettlementTick)
	require.Equal(t, currentTick, *overviewCatalog.Overview.LatestSettlementTick)

	facilityPage, err := cityService.ListCityServiceFacilities(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID, ServiceCode: "electric_power", Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, facilityPage.Items, 1)
	require.Equal(t, int64(1000), facilityPage.Items[0].Capacities[0].DispatchCapacityUnits)
	demandPage, err := cityService.ListCityServiceDemands(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, demandPage.Items, 1)
	require.NotNil(t, demandPage.NextCode)
	connectionPage, err := cityService.ListCityServiceConnections(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID, FacilityCode: "facility_central_power", Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, connectionPage.Items, 1)
	require.NotNil(t, connectionPage.NextCode)
	settlementPage, err := cityService.ListCityServiceSettlements(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID, ServiceCode: "electric_power", Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, settlementPage.Items, 1)
	require.Len(t, settlementPage.Items[0].Allocations, 1)
	require.NotNil(t, settlementPage.NextCursor)

	submit("service-stop-facility", service.CityCommandTypeFacilityStatusTransition, map[string]any{
		"facility_code": "facility_central_power", "to_status": service.CityFacilityStatusOffline,
		"expected_version": 2, "metadata": map[string]any{},
	})
	offline := step("service-stop-facility-step", 1)
	require.Len(t, offline.ServiceAllocations, 0)
	require.Len(t, offline.ServiceSettlements, 2)
	for _, settlement := range offline.ServiceSettlements {
		require.Zero(t, settlement.DeliveredUnits)
		require.Equal(t, settlement.RequestedUnits, settlement.ShortageUnits)
	}

	canonicalCatalog, err := cityService.GetCityServiceCatalog(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	canonicalFacilities, err := cityService.ListCityServiceFacilities(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID, Limit: 100,
	})
	require.NoError(t, err)
	canonicalDemands, err := cityService.ListCityServiceDemands(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID, Limit: 100,
	})
	require.NoError(t, err)
	canonicalConnections, err := cityService.ListCityServiceConnections(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID, Limit: 100,
	})
	require.NoError(t, err)
	canonicalSettlements, err := cityService.ListCityServiceSettlements(ctx, service.CityServiceQueryInput{
		UserID: owner.ID, WorldID: worldID, Limit: 100,
	})
	require.NoError(t, err)

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "public-service-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	if replay.ErrorDetail != nil {
		t.Logf("public-service replay detail: %s", *replay.ErrorDetail)
	}
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "public-service-recovery",
		ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	if recovery.ErrorDetail != nil {
		t.Logf("public-service recovery detail: %s", *recovery.ErrorDetail)
	}
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)

	restoredCatalog, err := cityService.GetCityServiceCatalog(ctx, service.CityServiceQueryInput{UserID: owner.ID, WorldID: worldID})
	require.NoError(t, err)
	restoredFacilities, err := cityService.ListCityServiceFacilities(ctx, service.CityServiceQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 100})
	require.NoError(t, err)
	restoredDemands, err := cityService.ListCityServiceDemands(ctx, service.CityServiceQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 100})
	require.NoError(t, err)
	restoredConnections, err := cityService.ListCityServiceConnections(ctx, service.CityServiceQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 100})
	require.NoError(t, err)
	restoredSettlements, err := cityService.ListCityServiceSettlements(ctx, service.CityServiceQueryInput{UserID: owner.ID, WorldID: worldID, Limit: 100})
	require.NoError(t, err)
	require.Equal(t, canonicalCatalog, restoredCatalog)
	require.Equal(t, canonicalFacilities, restoredFacilities)
	require.Equal(t, canonicalDemands, restoredDemands)
	require.Equal(t, canonicalConnections, restoredConnections)
	require.Equal(t, canonicalSettlements, restoredSettlements)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_service_settlements
SET delivered_units = delivered_units + 1
WHERE world_id = $1`, worldID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_facilities SET reliability_milli = 1 WHERE world_id = $1`, worldID)
	require.Error(t, err)
}
