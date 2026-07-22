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

func TestCityOpenWorldV14CommuteLifecycleIsFactBackedAndRecoverable(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v14-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(14_140_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V14 Commute Lifecycle", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV14,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	initial, err := cityService.GetCityOpenWorldCommuteLifecycleState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(0), initial.Policy.BaselineTick)
	require.Greater(t, initial.Policy.AssignmentCount, int64(0))
	require.Equal(t, int64(len(initial.Assignments)), initial.Policy.AssignmentCount)
	require.Equal(t, int64(len(initial.Sources)), initial.Policy.SourceCount)
	require.Equal(t, initial.Policy.AssignmentCount*2, initial.Policy.SourceCount)
	require.Empty(t, initial.Metrics)
	assertCityOpenWorldV14LifecyclePairs(t, initial)

	// V13 is retained as sealed evidence under V14. Its source rows must never
	// become the mutable generator again.
	v13Before, err := cityService.GetCityOpenWorldCommuteSourceState(ctx, owner.ID, worldID)
	require.NoError(t, err)

	currentTick := int64(0)
	generatedFacts := 0
	for currentTick < 25 {
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID,
			IdempotencyKey:    fmt.Sprintf("v14-lifecycle-step-%d", currentTick+1),
			ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		for _, fact := range result.OpenWorldRuntimeFacts {
			if fact.FactType == "system.commute.lifecycle.source.generated" {
				generatedFacts++
			}
		}
		currentTick = result.Tick.Tick
	}
	require.Greater(t, generatedFacts, 0)

	advanced, err := cityService.GetCityOpenWorldCommuteLifecycleState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Greater(t, advanced.Policy.GeneratedCount, int64(0))
	require.Len(t, advanced.Metrics, 1)
	require.Equal(t, int64(1), advanced.Metrics[0].CycleStartTick)
	require.Equal(t, int64(24), advanced.Metrics[0].CycleEndTick)
	require.Equal(t, int64(25), advanced.Metrics[0].ClosedTick)
	v13AfterStepping, err := cityService.GetCityOpenWorldCommuteSourceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, v13Before, v13AfterStepping)

	predecessor := advanced.Assignments[0]
	rebindPayload, err := json.Marshal(map[string]any{
		"actor_code":           predecessor.ActorCode,
		"employment_role_code": predecessor.EmploymentRole,
		"home_facility_code":   predecessor.HomeFacilityCode,
		"work_facility_code":   predecessor.WorkFacilityCode,
		"outbound_phase":       predecessor.OutboundPhase,
	})
	require.NoError(t, err)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v14-lifecycle-rebind-member-denied",
		CommandType: service.CityCommandTypeOpenWorldCommuteAssignmentRebind,
		Payload:     rebindPayload, ExpectedWorldTick: &currentTick,
	})
	require.ErrorIs(t, err, service.ErrCityPermissionDenied)
	_, err = cityService.SubmitCommand(adminCtx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v14-lifecycle-rebind",
		CommandType: service.CityCommandTypeOpenWorldCommuteAssignmentRebind,
		Payload:     rebindPayload, ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	rebindStep, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v14-lifecycle-rebind-step", ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	require.Len(t, rebindStep.Commands, 1)
	currentTick = rebindStep.Tick.Tick

	rebound, err := cityService.GetCityOpenWorldCommuteLifecycleState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, advanced.Policy.AssignmentCount+1, rebound.Policy.AssignmentCount)
	require.Equal(t, advanced.Policy.SourceCount+2, rebound.Policy.SourceCount)
	require.Equal(t, advanced.Policy.TransitionCount+2, rebound.Policy.TransitionCount)
	successor := requireCityOpenWorldV14SuccessorAssignment(t, rebound, predecessor)
	require.NotNil(t, successor.OpenedFact)
	require.Equal(t, currentTick, successor.OpenedTick)
	predecessorLatest := requireCityOpenWorldV14LatestTransition(t, rebound, predecessor.Code)
	require.Equal(t, "superseded", predecessorLatest.State)
	successorLatest := requireCityOpenWorldV14LatestTransition(t, rebound, successor.Code)
	require.Equal(t, "active", successorLatest.State)
	assertCityOpenWorldV14LifecyclePairs(t, rebound)
	v13AfterRebind, err := cityService.GetCityOpenWorldCommuteSourceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, v13Before, v13AfterRebind)

	suspendPayload, err := json.Marshal(map[string]any{
		"actor_code": successor.ActorCode,
		"state":      "suspended",
	})
	require.NoError(t, err)
	_, err = cityService.SubmitCommand(adminCtx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v14-lifecycle-suspend",
		CommandType: service.CityCommandTypeOpenWorldCommuteAssignmentSetState,
		Payload:     suspendPayload, ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	suspendStep, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v14-lifecycle-suspend-step", ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	require.Len(t, suspendStep.Commands, 1)
	currentTick = suspendStep.Tick.Tick
	suspended, err := cityService.GetCityOpenWorldCommuteLifecycleState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "suspended", requireCityOpenWorldV14LatestTransition(t, suspended, successor.Code).State)

	resumePayload, err := json.Marshal(map[string]any{
		"actor_code": successor.ActorCode,
		"state":      "active",
	})
	require.NoError(t, err)
	_, err = cityService.SubmitCommand(adminCtx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v14-lifecycle-resume",
		CommandType: service.CityCommandTypeOpenWorldCommuteAssignmentSetState,
		Payload:     resumePayload, ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	resumeStep, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v14-lifecycle-resume-step", ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	require.Len(t, resumeStep.Commands, 1)
	currentTick = resumeStep.Tick.Tick
	final, err := cityService.GetCityOpenWorldCommuteLifecycleState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "active", requireCityOpenWorldV14LatestTransition(t, final, successor.Code).State)

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v14-lifecycle-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v14-lifecycle-recovery", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldCommuteLifecycleState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, final, restored)
}

func TestCityOpenWorldV13UpgradeToV14PreservesSealedCommuteEvidence(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v13-v14-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(14_140_002)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V13 to V14 Lifecycle Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV13,
		StyleProfileID:    "cn.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	bindingsBefore, err := cityService.GetCityOpenWorldCommuteState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	sourcesBefore, err := cityService.GetCityOpenWorldCommuteSourceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v13-v14-lifecycle-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV14,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)

	bindingsAfter, err := cityService.GetCityOpenWorldCommuteState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	sourcesAfter, err := cityService.GetCityOpenWorldCommuteSourceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, bindingsBefore, bindingsAfter)
	require.Equal(t, sourcesBefore, sourcesAfter)
	lifecycle, err := cityService.GetCityOpenWorldCommuteLifecycleState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(len(bindingsBefore.Bindings)), lifecycle.Policy.AssignmentCount)
	require.Equal(t, lifecycle.Policy.AssignmentCount*2, lifecycle.Policy.SourceCount)
	require.Equal(t, int64(len(lifecycle.Assignments)), lifecycle.Policy.AssignmentCount)
	assertCityOpenWorldV14LifecyclePairs(t, lifecycle)
}

func assertCityOpenWorldV14LifecyclePairs(t *testing.T, state *service.CityOpenWorldCommuteLifecycleState) {
	t.Helper()
	pairs := make(map[string]map[string]bool)
	for _, source := range state.Sources {
		if pairs[source.AssignmentCode] == nil {
			pairs[source.AssignmentCode] = map[string]bool{}
		}
		pairs[source.AssignmentCode][source.Direction] = true
	}
	for _, assignment := range state.Assignments {
		directions := pairs[assignment.Code]
		require.Truef(t, directions["outbound"], "%s lacks an outbound lifecycle source", assignment.Code)
		require.Truef(t, directions["return"], "%s lacks a return lifecycle source", assignment.Code)
	}
}

func requireCityOpenWorldV14SuccessorAssignment(
	t *testing.T,
	state *service.CityOpenWorldCommuteLifecycleState,
	predecessor service.CityOpenWorldCommuteAssignmentEpoch,
) service.CityOpenWorldCommuteAssignmentEpoch {
	t.Helper()
	for _, assignment := range state.Assignments {
		if assignment.BindingCode == predecessor.BindingCode && assignment.EpochNumber == predecessor.EpochNumber+1 {
			return assignment
		}
	}
	t.Fatalf("successor epoch for %q was not found", predecessor.Code)
	return service.CityOpenWorldCommuteAssignmentEpoch{}
}

func requireCityOpenWorldV14LatestTransition(
	t *testing.T,
	state *service.CityOpenWorldCommuteLifecycleState,
	assignmentCode string,
) service.CityOpenWorldCommuteAssignmentTransition {
	t.Helper()
	var latest *service.CityOpenWorldCommuteAssignmentTransition
	for index := range state.Transitions {
		transition := &state.Transitions[index]
		if transition.AssignmentCode != assignmentCode {
			continue
		}
		if latest == nil || transition.TransitionTick > latest.TransitionTick ||
			(transition.TransitionTick == latest.TransitionTick && transition.TransitionSeq > latest.TransitionSeq) {
			latest = transition
		}
	}
	require.NotNil(t, latest, "lifecycle transition for %q was not found", assignmentCode)
	return *latest
}
