//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCityRealtimeAgentDecisionRuntimeSealsDeferredIntentWithoutProviderData(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-agent-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_991)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Agent Decision " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	var policyVersion string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT policy_version
FROM city_realtime_agent_world_bindings
WHERE world_id = $1`, worldID).Scan(&policyVersion))
	require.Equal(t, "1.13.0", policyVersion)

	var agentCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND agent_subtype = 'character.npc' AND lifecycle_status = 'active'
ORDER BY agent_code ASC
LIMIT 1`, worldID).Scan(&agentCode))

	requestInput := service.CityRealtimeAgentDecisionRequestInput{
		WorldID: worldID, AgentCode: agentCode, TriggerKey: "scheduler.bootstrap.npc",
	}
	queued, err := cityService.QueueRealtimeAgentDecision(adminCtx, requestInput)
	require.NoError(t, err)
	require.NotNil(t, queued.Frame)
	require.Equal(t, "queued", queued.Status)
	require.Equal(t, int64(1), queued.Frame.FrameSequence)
	require.Equal(t, "agent.decision.requested", queued.Frame.PhaseSummary["command"])

	// A trigger key is a server-owned one-shot deduplication key. Retrying it
	// after the request frame commits must not append a duplicate Observation or
	// consume another Temporal Frame.
	replayedQueue, err := cityService.QueueRealtimeAgentDecision(adminCtx, requestInput)
	require.NoError(t, err)
	require.Equal(t, queued.RequestCode, replayedQueue.RequestCode)
	require.Equal(t, queued.ObservationCode, replayedQueue.ObservationCode)
	require.Equal(t, "queued", replayedQueue.Status)
	require.Nil(t, replayedQueue.Frame)

	var observationPayload string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT payload::text
FROM city_realtime_agent_observations
WHERE world_id = $1 AND observation_code = $2`, worldID, queued.ObservationCode).Scan(&observationPayload))
	for _, forbidden := range []string{owner.Email, "owner_user_id", "provider", "memory", "credential"} {
		require.NotContains(t, strings.ToLower(observationPayload), strings.ToLower(forbidden))
	}

	resolved, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: queued.RequestCode, WorkerID: "worker.integration",
	})
	require.NoError(t, err)
	require.NotNil(t, resolved.Frame)
	require.Equal(t, "accepted", resolved.Status)
	require.NotEmpty(t, resolved.DecisionCode)
	require.NotEmpty(t, resolved.IntentCode)
	require.Equal(t, int64(2), resolved.Frame.FrameSequence)
	require.Equal(t, "agent.decision.resolved", resolved.Frame.PhaseSummary["command"])

	var requestStatus, attemptStatus, decisionStatus, intentStatus, outboxStatus string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status FROM city_realtime_agent_decision_requests
WHERE world_id = $1 AND request_code = $2`, worldID, queued.RequestCode).Scan(&requestStatus))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status FROM city_realtime_agent_decision_attempts
WHERE world_id = $1 AND request_code = $2`, worldID, queued.RequestCode).Scan(&attemptStatus))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT decision_status FROM city_realtime_agent_decisions
WHERE world_id = $1 AND decision_code = $2`, worldID, resolved.DecisionCode).Scan(&decisionStatus))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status FROM city_realtime_agent_intents
WHERE world_id = $1 AND intent_code = $2`, worldID, resolved.IntentCode).Scan(&intentStatus))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status FROM city_realtime_agent_outbox
WHERE world_id = $1 AND request_code = $2`, worldID, queued.RequestCode).Scan(&outboxStatus))
	require.Equal(t, "accepted", requestStatus)
	require.Equal(t, "succeeded", attemptStatus)
	require.Equal(t, "accepted", decisionStatus)
	require.Equal(t, "pending", intentStatus)
	require.Equal(t, "succeeded", outboxStatus)

	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	processed, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, processed.Resolved)
	require.NotNil(t, processed.Frame)
	require.Equal(t, int64(1_000_000), processed.CurrentWorldTimeUS)
	require.Equal(t, 1, processed.Frame.PhaseSummary["agent_intent_applied_count"])

	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status FROM city_realtime_agent_intents
WHERE world_id = $1 AND intent_code = $2`, worldID, resolved.IntentCode).Scan(&intentStatus))
	require.Equal(t, "applied", intentStatus)

	replayedRun, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: queued.RequestCode, WorkerID: "worker.integration",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", replayedRun.Status)
	require.Empty(t, replayedRun.DecisionCode)
	require.Nil(t, replayedRun.Frame)
}

func TestCityRealtimeCharacterAgentAutonomyClosesTheActivityLoop(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-autonomy-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_992)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Character Autonomy " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "星野 澪", ArchetypeCode: "resident.social",
		IdempotencyKey: "character-autonomy-create-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, created.Frame)
	require.Equal(t, int64(1), created.Frame.FrameSequence)

	personality := service.CityRealtimeCharacterPersonalitySeed{
		Values:         []string{"community", "curiosity"},
		HardBoundaries: []string{"avoid harm"},
		Preferences:    map[string]string{"work_style": "civic_service"},
		Background:     "Lives near the central ward.",
		FreeformNotes:  "This text is owner-private and must never enter an observation payload.",
	}
	configured, err := cityService.ConfigureRealtimeCharacterAgent(ctx, service.CityRealtimeCharacterAgentConfigureInput{
		UserID: owner.ID, WorldID: worldID, ControlMode: "autonomous", Personality: &personality,
		IdempotencyKey: "character-autonomy-configure-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, configured.Frame)
	require.Equal(t, int64(2), configured.Frame.FrameSequence)
	require.NotNil(t, configured.Agent)
	require.Equal(t, "autonomous", configured.Agent.ControlMode)
	require.NotNil(t, configured.Agent.Personality)
	require.Equal(t, int64(1), configured.Agent.Personality.Revision)
	require.NotEmpty(t, configured.Agent.Personality.SeedHash)

	replayedConfigure, err := cityService.ConfigureRealtimeCharacterAgent(ctx, service.CityRealtimeCharacterAgentConfigureInput{
		UserID: owner.ID, WorldID: worldID, ControlMode: "autonomous", Personality: &personality,
		IdempotencyKey: "character-autonomy-configure-" + suffix,
	})
	require.NoError(t, err)
	require.Equal(t, configured, replayedConfigure)

	ownerView, err := cityService.GetRealtimeMyCharacter(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.NotNil(t, ownerView.Agent)
	require.Equal(t, configured.Agent, ownerView.Agent)
	_, err = cityService.PerformRealtimeCharacterActivity(ctx, service.CityRealtimeCharacterActivityInput{
		UserID: owner.ID, WorldID: worldID, ActivityCode: "work.civic_shift",
		IdempotencyKey: "character-autonomy-manual-blocked-" + suffix,
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeCharacterControlUnavailable)

	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	wakeup, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, wakeup.Resolved)
	require.NotNil(t, wakeup.Frame)
	require.Equal(t, 1, wakeup.Frame.PhaseSummary["agent_wakeup_applied_count"])

	var agentCode, requestCode, observationPayload string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&agentCode))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.request_code, observation.payload::text
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_observations observation
  ON observation.world_id = request.world_id
 AND observation.observation_code = request.observation_code
WHERE request.world_id = $1 AND request.agent_code = $2
  AND request.status = 'queued'`, worldID, agentCode).Scan(&requestCode, &observationPayload))
	require.Contains(t, observationPayload, "personality_seed_hash")
	require.NotContains(t, observationPayload, personality.FreeformNotes)
	require.NotContains(t, observationPayload, "owner_user_id")

	resolved, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: requestCode, WorkerID: "worker.autonomy.integration",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", resolved.Status)
	require.NotEmpty(t, resolved.DecisionCode)
	require.NotEmpty(t, resolved.IntentCode)
	var actionCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT action_code FROM city_realtime_agent_decisions
WHERE world_id = $1 AND decision_code = $2`, worldID, resolved.DecisionCode).Scan(&actionCode))
	require.Equal(t, "character.activity.perform", actionCode)

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	activity, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, activity.Resolved)
	require.NotNil(t, activity.Frame)
	require.Equal(t, 1, activity.Frame.PhaseSummary["agent_intent_applied_count"])

	var appliedIntentStatus, appliedActivityCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status FROM city_realtime_agent_intents
WHERE world_id = $1 AND intent_code = $2`, worldID, resolved.IntentCode).Scan(&appliedIntentStatus))
	require.Equal(t, "applied", appliedIntentStatus)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT activity_code
FROM city_realtime_character_activity_events
WHERE world_id = $1 AND actor_code = $2
ORDER BY event_sequence DESC
LIMIT 1`, worldID, created.Character.ActorCode).Scan(&appliedActivityCode))
	require.Contains(t, []string{"work.civic_shift", "civic.cleanup", "consume.ration", "rest.short"}, appliedActivityCode)

	suspended, err := cityService.ConfigureRealtimeCharacterAgent(ctx, service.CityRealtimeCharacterAgentConfigureInput{
		UserID: owner.ID, WorldID: worldID, ControlMode: "suspended",
		IdempotencyKey: "character-autonomy-suspend-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, suspended.Agent)
	require.Equal(t, "suspended", suspended.Agent.ControlMode)

	var activeRequestsBefore int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_agent_decision_requests
WHERE world_id = $1 AND agent_code = $2 AND status IN ('queued', 'leased')`, worldID, agentCode).Scan(&activeRequestsBefore))
	require.Zero(t, activeRequestsBefore)
	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	postSuspendWakeup, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(5 * time.Second),
	})
	require.NoError(t, err)
	require.True(t, postSuspendWakeup.Resolved)
	var activeRequestsAfter int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_agent_decision_requests
WHERE world_id = $1 AND agent_code = $2 AND status IN ('queued', 'leased')`, worldID, agentCode).Scan(&activeRequestsAfter))
	require.Zero(t, activeRequestsAfter)
}

func TestCityRealtimeCharacterStructuredTaskIsAgentBoundAndCompletesThroughExactActivity(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-task-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-task-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_201_121)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Character Structured Task " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "白石 結", ArchetypeCode: "resident.social",
		IdempotencyKey: "character-task-create-" + suffix,
	})
	require.NoError(t, err)
	configured, err := cityService.ConfigureRealtimeCharacterAgent(ctx, service.CityRealtimeCharacterAgentConfigureInput{
		UserID: owner.ID, WorldID: worldID, ControlMode: "autonomous",
		Personality: &service.CityRealtimeCharacterPersonalitySeed{
			Values: []string{"community", "diligence"}, HardBoundaries: []string{"avoid harm"},
			Preferences: map[string]string{"work_style": "civic_service"},
			Background:  "Lives and works in the shared city.",
		},
		IdempotencyKey: "character-task-configure-" + suffix,
	})
	require.NoError(t, err)
	require.Equal(t, "autonomous", configured.Agent.ControlMode)

	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	wakeup, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, wakeup.Resolved)
	require.Equal(t, 1, wakeup.Frame.PhaseSummary["agent_wakeup_applied_count"])

	var agentCode, taskRequestCode, observationPayload string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&agentCode))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.request_code, observation.payload::text
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_observations observation
  ON observation.world_id = request.world_id
 AND observation.observation_code = request.observation_code
WHERE request.world_id = $1 AND request.agent_code = $2 AND request.status = 'queued'
ORDER BY request.requested_frame_sequence DESC
LIMIT 1`, worldID, agentCode).Scan(&taskRequestCode, &observationPayload))
	require.Contains(t, observationPayload, `"schema_version": 6`)
	require.Contains(t, observationPayload, `"available_task_codes"`)
	require.Contains(t, observationPayload, "task.civic.shift")

	var creditBeforeTask, standingBeforeTask int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT city_credit_units, civic_standing_milli
FROM city_realtime_character_profiles
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&creditBeforeTask, &standingBeforeTask))

	taskDecision, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: taskRequestCode, WorkerID: "worker.task.accept.integration",
		PreferredAction: "character.task.accept",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", taskDecision.Status)
	require.NotEmpty(t, taskDecision.IntentCode)
	var taskDecisionAction string
	var taskDecisionArguments string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT decision.action_code, intent.arguments::text
FROM city_realtime_agent_decisions decision
JOIN city_realtime_agent_intents intent
  ON intent.world_id = decision.world_id AND intent.decision_code = decision.decision_code
WHERE decision.world_id = $1 AND decision.decision_code = $2`, worldID, taskDecision.DecisionCode).Scan(&taskDecisionAction, &taskDecisionArguments))
	require.Equal(t, "character.task.accept", taskDecisionAction)
	require.JSONEq(t, `{"task_code":"task.civic.shift"}`, taskDecisionArguments)

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	accepted, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, accepted.Resolved)
	require.Equal(t, 1, accepted.Frame.PhaseSummary["agent_intent_applied_count"])

	var taskRunCode, taskCode, taskActivityCode, taskStatus, sourceIntentCode string
	var taskRevision, acceptedFrame, expirationDue, lastFrame int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT task_run_code, task_code, activity_code, task_status, source_intent_code,
       task_revision, accepted_frame_sequence, expiration_due_world_time_us, last_frame_sequence
FROM city_realtime_character_task_heads
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(
		&taskRunCode, &taskCode, &taskActivityCode, &taskStatus, &sourceIntentCode,
		&taskRevision, &acceptedFrame, &expirationDue, &lastFrame,
	))
	require.Equal(t, "task.civic.shift", taskCode)
	require.Equal(t, "work.civic_shift", taskActivityCode)
	require.Equal(t, "accepted", taskStatus)
	require.Equal(t, taskDecision.IntentCode, sourceIntentCode)
	require.Equal(t, int64(1), taskRevision)
	require.Equal(t, acceptedFrame, lastFrame)
	require.Greater(t, expirationDue, accepted.CurrentWorldTimeUS)
	require.Contains(t, taskRunCode, "task.run.")

	var acceptedTaskEvents, pendingExpirations int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_character_task_events
WHERE world_id = $1 AND actor_code = $2 AND task_run_code = $3`, worldID, created.Character.ActorCode, taskRunCode).Scan(&acceptedTaskEvents))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_due_events
WHERE world_id = $1 AND event_type = 'system.realtime.character_task_expire'
  AND due_world_time_us = $2 AND status = 'pending'`, worldID, expirationDue).Scan(&pendingExpirations))
	require.Equal(t, 1, acceptedTaskEvents)
	require.Equal(t, 1, pendingExpirations)

	var creditAfterAccept, standingAfterAccept int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT city_credit_units, civic_standing_milli
FROM city_realtime_character_profiles
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&creditAfterAccept, &standingAfterAccept))
	require.Equal(t, creditBeforeTask, creditAfterAccept, "accepting a task cannot mint city credit")
	require.Equal(t, standingBeforeTask, standingAfterAccept, "accepting a task cannot alter civic standing")

	ownerTasks, err := cityService.ListRealtimeCharacterTasks(ctx, service.CityRealtimeCharacterTaskListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, ownerTasks, 1)
	require.Equal(t, taskRunCode, ownerTasks[0].TaskRunCode)
	require.Equal(t, "accepted", ownerTasks[0].Status)
	publicTaskJSON, err := json.Marshal(ownerTasks[0])
	require.NoError(t, err)
	require.NotContains(t, string(publicTaskJSON), "source_intent")
	require.NotContains(t, string(publicTaskJSON), "activity_event_hash")
	_, err = cityService.ListRealtimeCharacterTasks(ctx, service.CityRealtimeCharacterTaskListInput{
		UserID: outsider.ID, WorldID: worldID, Limit: 10,
	})
	require.Error(t, err, "a non-owner cannot read a character task ledger")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_task_heads
SET task_status = 'expired'
WHERE world_id = $1 AND actor_code = $2 AND task_run_code = $3`, worldID, created.Character.ActorCode, taskRunCode)
	require.Error(t, err, "direct task mutations must be rejected by the SQL gate")

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	nextWakeup, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, nextWakeup.Resolved)
	require.Equal(t, 1, nextWakeup.Frame.PhaseSummary["agent_wakeup_applied_count"])

	var activityRequestCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request_code
FROM city_realtime_agent_decision_requests
WHERE world_id = $1 AND agent_code = $2 AND status = 'queued'
ORDER BY requested_frame_sequence DESC
LIMIT 1`, worldID, agentCode).Scan(&activityRequestCode))
	activityDecision, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: activityRequestCode, WorkerID: "worker.task.complete.integration",
		PreferredAction: "character.activity.perform",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", activityDecision.Status)

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	completed, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, completed.Resolved)
	require.Equal(t, 1, completed.Frame.PhaseSummary["agent_intent_applied_count"])

	var completionActivitySequence int64
	var completionActivityHash string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT task_status, task_revision, completion_activity_event_sequence,
       completion_activity_event_hash, last_frame_sequence
FROM city_realtime_character_task_heads
WHERE world_id = $1 AND actor_code = $2 AND task_run_code = $3`, worldID, created.Character.ActorCode, taskRunCode).Scan(
		&taskStatus, &taskRevision, &completionActivitySequence, &completionActivityHash, &lastFrame,
	))
	require.Equal(t, "completed", taskStatus)
	require.Equal(t, int64(2), taskRevision)
	require.Greater(t, completionActivitySequence, int64(0))
	require.Len(t, completionActivityHash, 64)
	require.Equal(t, completed.Frame.FrameSequence, lastFrame)
	var matchingActivityCode, matchingActivityHash string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT activity_code, event_hash
FROM city_realtime_character_activity_events
WHERE world_id = $1 AND actor_code = $2 AND event_sequence = $3`, worldID, created.Character.ActorCode, completionActivitySequence).Scan(
		&matchingActivityCode, &matchingActivityHash,
	))
	require.Equal(t, "work.civic_shift", matchingActivityCode)
	require.Equal(t, matchingActivityHash, completionActivityHash)
	var completedTaskEvents, activeTasks int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_character_task_events
WHERE world_id = $1 AND actor_code = $2 AND task_run_code = $3`, worldID, created.Character.ActorCode, taskRunCode).Scan(&completedTaskEvents))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_character_task_heads
WHERE world_id = $1 AND actor_code = $2 AND task_status = 'accepted'`, worldID, created.Character.ActorCode).Scan(&activeTasks))
	require.Equal(t, 2, completedTaskEvents)
	require.Zero(t, activeTasks)

	ownerTasks, err = cityService.ListRealtimeCharacterTasks(ctx, service.CityRealtimeCharacterTaskListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, ownerTasks, 1)
	require.Equal(t, "completed", ownerTasks[0].Status)
	require.Equal(t, completed.Frame.FrameSequence, ownerTasks[0].CompletedFrameSequence)
}

func TestCityRealtimeCharacterAgentMoveUsesSealedFiniteContext(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-agent-move-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_993)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Character Agent Move " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "朝比奈 凛", ArchetypeCode: "resident.social",
		IdempotencyKey: "character-agent-move-create-" + suffix,
	})
	require.NoError(t, err)
	configured, err := cityService.ConfigureRealtimeCharacterAgent(ctx, service.CityRealtimeCharacterAgentConfigureInput{
		UserID: owner.ID, WorldID: worldID, ControlMode: "autonomous",
		Personality: &service.CityRealtimeCharacterPersonalitySeed{
			Values:         []string{"curiosity"},
			HardBoundaries: []string{"avoid harm"},
			Preferences:    map[string]string{"movement": "walkable_routes"},
			Background:     "Lives in the shared city.",
		},
		IdempotencyKey: "character-agent-move-configure-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, configured.Agent)
	require.Equal(t, "autonomous", configured.Agent.ControlMode)
	_, err = cityService.MoveRealtimeCharacter(ctx, service.CityRealtimeCharacterMoveInput{
		UserID: owner.ID, WorldID: worldID, X: created.Character.X + 1, Y: created.Character.Y, Z: created.Character.Z,
		IdempotencyKey: "character-agent-move-manual-blocked-" + suffix,
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeCharacterControlUnavailable)

	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	wakeup, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, wakeup.Resolved)
	require.NotNil(t, wakeup.Frame)
	require.Equal(t, 1, wakeup.Frame.PhaseSummary["agent_wakeup_applied_count"])

	var agentCode, requestCode, observationPayload string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&agentCode))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.request_code, observation.payload::text
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_observations observation
  ON observation.world_id = request.world_id
 AND observation.observation_code = request.observation_code
WHERE request.world_id = $1 AND request.agent_code = $2
  AND request.status = 'queued'`, worldID, agentCode).Scan(&requestCode, &observationPayload))
	require.Contains(t, observationPayload, `"action_context"`)
	require.Contains(t, observationPayload, `"available_move_targets"`)
	require.NotContains(t, observationPayload, "owner_user_id")
	require.NotContains(t, observationPayload, "provider")
	_, err = cityService.RunRealtimeAgentFakeDecision(ctx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: requestCode, WorkerID: "worker.move.non-admin",
		PreferredAction: "character.move",
	})
	require.ErrorIs(t, err, service.ErrCityManagementRequired)

	resolved, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: requestCode, WorkerID: "worker.move.integration",
		PreferredAction: "character.move",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", resolved.Status)
	require.NotEmpty(t, resolved.IntentCode)

	var actionCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT action_code
FROM city_realtime_agent_decisions
WHERE world_id = $1 AND decision_code = $2`, worldID, resolved.DecisionCode).Scan(&actionCode))
	require.Equal(t, "character.move", actionCode)

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	applied, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, applied.Resolved)
	require.NotNil(t, applied.Frame)
	require.Equal(t, 1, applied.Frame.PhaseSummary["agent_intent_applied_count"])

	var intentStatus, eventKind string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status FROM city_realtime_agent_intents
WHERE world_id = $1 AND intent_code = $2`, worldID, resolved.IntentCode).Scan(&intentStatus))
	require.Equal(t, "applied", intentStatus)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT event_kind
FROM city_realtime_actor_position_events
WHERE world_id = $1 AND actor_code = $2
ORDER BY event_sequence DESC
LIMIT 1`, worldID, created.Character.ActorCode).Scan(&eventKind))
	require.Equal(t, "move", eventKind)
	var positionEventCountBeforeReplay int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_actor_position_events
WHERE world_id = $1 AND actor_code = $2 AND event_kind = 'move'`, worldID, created.Character.ActorCode).Scan(&positionEventCountBeforeReplay))
	require.Equal(t, 1, positionEventCountBeforeReplay)

	replayedRun, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: requestCode, WorkerID: "worker.move.integration",
		PreferredAction: "character.move",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", replayedRun.Status)
	require.Empty(t, replayedRun.DecisionCode)
	require.Empty(t, replayedRun.IntentCode)
	require.Nil(t, replayedRun.Frame)
	var positionEventCountAfterReplay int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_actor_position_events
WHERE world_id = $1 AND actor_code = $2 AND event_kind = 'move'`, worldID, created.Character.ActorCode).Scan(&positionEventCountAfterReplay))
	require.Equal(t, positionEventCountBeforeReplay, positionEventCountAfterReplay)

	view, err := cityService.GetRealtimeMyCharacter(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.NotNil(t, view.Character)
	require.NotEqual(t,
		fmt.Sprintf("%d/%d/%d", created.Character.X, created.Character.Y, created.Character.Z),
		fmt.Sprintf("%d/%d/%d", view.Character.X, view.Character.Y, view.Character.Z),
	)

	var pendingWakeups int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_due_events
WHERE world_id = $1 AND event_type = 'system.realtime.agent_wakeup'
  AND status = 'pending'`, worldID).Scan(&pendingWakeups))
	require.Equal(t, 1, pendingWakeups)
}

func TestCityRealtimeCharacterNavigationPlanIsAgentBoundAndSealsMovement(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-navigation-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-navigation-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_201_131)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Character Navigation " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "真壁 葵", ArchetypeCode: "resident.social",
		IdempotencyKey: "character-navigation-create-" + suffix,
	})
	require.NoError(t, err)
	configured, err := cityService.ConfigureRealtimeCharacterAgent(ctx, service.CityRealtimeCharacterAgentConfigureInput{
		UserID: owner.ID, WorldID: worldID, ControlMode: "autonomous",
		Personality: &service.CityRealtimeCharacterPersonalitySeed{
			Values:         []string{"curiosity", "community"},
			HardBoundaries: []string{"avoid harm"},
			Preferences:    map[string]string{"movement": "walkable_routes"},
			Background:     "Moves through the shared city by public entrances.",
		},
		IdempotencyKey: "character-navigation-configure-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, configured.Agent)
	require.Equal(t, "autonomous", configured.Agent.ControlMode)

	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	wakeup, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, wakeup.Resolved)
	require.Equal(t, 1, wakeup.Frame.PhaseSummary["agent_wakeup_applied_count"])

	var agentCode, requestCode, observationPayload string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&agentCode))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.request_code, observation.payload::text
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_observations observation
  ON observation.world_id = request.world_id
 AND observation.observation_code = request.observation_code
WHERE request.world_id = $1 AND request.agent_code = $2 AND request.status = 'queued'
ORDER BY request.requested_frame_sequence DESC
LIMIT 1`, worldID, agentCode).Scan(&requestCode, &observationPayload))

	var observation struct {
		AllowedActions []string `json:"allowed_actions"`
		Character      struct {
			ActionContext struct {
				SchemaVersion                             int      `json:"schema_version"`
				AvailableNavigationDestinationPortalCodes []string `json:"available_navigation_destination_portal_codes"`
			} `json:"action_context"`
		} `json:"character"`
	}
	require.NoError(t, json.Unmarshal([]byte(observationPayload), &observation))
	require.Contains(t, observation.AllowedActions, "character.navigation.plan")
	require.Equal(t, 6, observation.Character.ActionContext.SchemaVersion)
	require.NotEmpty(t, observation.Character.ActionContext.AvailableNavigationDestinationPortalCodes,
		"the server must publish at least one bounded, reachable static entrance for this deterministic realtime world")
	require.NotContains(t, observationPayload, "owner_user_id")
	require.NotContains(t, observationPayload, "provider")
	require.NotContains(t, observationPayload, "source_intent_code")

	decision, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: requestCode, WorkerID: "worker.navigation.integration",
		PreferredAction: "character.navigation.plan",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", decision.Status)
	require.NotEmpty(t, decision.DecisionCode)
	require.NotEmpty(t, decision.IntentCode)
	var decisionAction, decisionArguments string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT decision.action_code, intent.arguments::text
FROM city_realtime_agent_decisions decision
JOIN city_realtime_agent_intents intent
  ON intent.world_id = decision.world_id AND intent.decision_code = decision.decision_code
WHERE decision.world_id = $1 AND decision.decision_code = $2`, worldID, decision.DecisionCode).Scan(&decisionAction, &decisionArguments))
	require.Equal(t, "character.navigation.plan", decisionAction)
	var navigationArguments struct {
		DestinationPortalCode string `json:"destination_portal_code"`
	}
	require.NoError(t, json.Unmarshal([]byte(decisionArguments), &navigationArguments))
	require.Contains(t, observation.Character.ActionContext.AvailableNavigationDestinationPortalCodes, navigationArguments.DestinationPortalCode)

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	accepted, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, accepted.Resolved)
	require.Equal(t, 1, accepted.Frame.PhaseSummary["agent_intent_applied_count"])

	var runCode, destinationPortalCode, planStatus, sourceIntentCode string
	var planRevision, stepsCompleted, maximumSteps, nextDueWorldTimeUS int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT navigation_run_code, destination_portal_code, plan_status, source_intent_code,
       plan_revision, steps_completed, maximum_steps, next_due_world_time_us
FROM city_realtime_character_navigation_plan_heads
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(
		&runCode, &destinationPortalCode, &planStatus, &sourceIntentCode,
		&planRevision, &stepsCompleted, &maximumSteps, &nextDueWorldTimeUS,
	))
	require.Equal(t, navigationArguments.DestinationPortalCode, destinationPortalCode)
	require.Equal(t, decision.IntentCode, sourceIntentCode)
	require.Equal(t, "active", planStatus)
	require.Equal(t, int64(1), planRevision)
	require.Zero(t, stepsCompleted)
	require.Equal(t, int64(32), maximumSteps)
	require.Greater(t, nextDueWorldTimeUS, accepted.CurrentWorldTimeUS)
	var trafficBindingCount, pendingTrafficReservations int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_character_traffic_reservation_world_bindings
WHERE world_id = $1`, worldID).Scan(&trafficBindingCount))
	require.Equal(t, 1, trafficBindingCount, "new realtime worlds must opt into the immutable traffic binding at genesis")
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_due_events
WHERE world_id = $1 AND event_type = 'system.realtime.character_traffic_reservation'
  AND due_world_time_us = $2 AND status = 'pending'`, worldID, nextDueWorldTimeUS).Scan(&pendingTrafficReservations))
	require.Equal(t, 1, pendingTrafficReservations, "an active traffic-bound navigation plan needs one exact next capacity boundary")

	ownerPlans, err := cityService.ListRealtimeCharacterNavigationPlans(ctx, service.CityRealtimeCharacterNavigationPlanListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, ownerPlans, 1)
	require.Equal(t, runCode, ownerPlans[0].NavigationRunCode)
	require.Equal(t, destinationPortalCode, ownerPlans[0].DestinationPortalCode)
	publicPlanJSON, err := json.Marshal(ownerPlans[0])
	require.NoError(t, err)
	require.NotContains(t, string(publicPlanJSON), "source_intent")
	require.NotContains(t, string(publicPlanJSON), "event_chain_hash")
	_, err = cityService.ListRealtimeCharacterNavigationPlans(ctx, service.CityRealtimeCharacterNavigationPlanListInput{
		UserID: outsider.ID, WorldID: worldID, Limit: 10,
	})
	require.Error(t, err, "a non-owner cannot read a character navigation ledger")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_navigation_plan_heads
SET plan_status = 'cancelled'
WHERE world_id = $1 AND actor_code = $2 AND navigation_run_code = $3`, worldID, created.Character.ActorCode, runCode)
	require.Error(t, err, "direct navigation plan mutations must be rejected by the SQL gate")

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	moved, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, moved.Resolved)
	require.Equal(t, 1, moved.Frame.PhaseSummary["character_traffic_reservation_count"])
	require.Equal(t, 1, moved.Frame.PhaseSummary["character_navigation_plan_step_count"])

	var eventType, actorPositionEventHash string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT event_type, actor_position_event_hash, steps_completed
FROM city_realtime_character_navigation_plan_events
WHERE world_id = $1 AND actor_code = $2 AND navigation_run_code = $3
ORDER BY event_sequence DESC
LIMIT 1`, worldID, created.Character.ActorCode, runCode).Scan(&eventType, &actorPositionEventHash, &stepsCompleted))
	require.Contains(t, []string{"navigation_step", "navigation_arrived"}, eventType)
	require.Equal(t, int64(1), stepsCompleted)
	require.Len(t, actorPositionEventHash, 64)
	var matchingPositionEvents int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_actor_position_events position_event
JOIN city_realtime_character_navigation_plan_events navigation_event
  ON navigation_event.world_id = position_event.world_id
 AND navigation_event.actor_code = position_event.actor_code
 AND navigation_event.frame_sequence = position_event.frame_sequence
 AND navigation_event.actor_position_event_hash = position_event.event_hash
WHERE navigation_event.world_id = $1 AND navigation_event.actor_code = $2
  AND navigation_event.navigation_run_code = $3
		AND navigation_event.event_sequence = 2
  AND position_event.event_kind = 'move'`, worldID, created.Character.ActorCode, runCode).Scan(&matchingPositionEvents))
	require.Equal(t, 1, matchingPositionEvents)

	ownerReservations, err := cityService.ListRealtimeCharacterTrafficReservations(ctx, service.CityRealtimeCharacterTrafficReservationListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, ownerReservations, 1)
	require.Equal(t, runCode, ownerReservations[0].NavigationRunCode)
	require.Equal(t, int64(1), ownerReservations[0].PlanRevision)
	require.Equal(t, "consumed", ownerReservations[0].Status)
	require.Equal(t, int64(2), ownerReservations[0].Revision)
	ownerReservationJSON, err := json.Marshal(ownerReservations[0])
	require.NoError(t, err)
	require.NotContains(t, string(ownerReservationJSON), "target_x")
	require.NotContains(t, string(ownerReservationJSON), "event_chain_hash")
	require.NotContains(t, string(ownerReservationJSON), "provider")
	_, err = cityService.ListRealtimeCharacterTrafficReservations(ctx, service.CityRealtimeCharacterTrafficReservationListInput{
		UserID: outsider.ID, WorldID: worldID, Limit: 10,
	})
	require.Error(t, err, "a non-owner cannot read a character traffic reservation ledger")

	var reservationStatus, reservationPositionEventHash string
	var reservationRevision int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT head.reservation_status, head.reservation_revision, event.actor_position_event_hash
FROM city_realtime_character_traffic_reservation_heads head
JOIN city_realtime_character_traffic_reservation_events event
  ON event.world_id = head.world_id AND event.actor_code = head.actor_code
 AND event.navigation_run_code = head.navigation_run_code AND event.plan_revision = head.plan_revision
 AND event.event_sequence = head.reservation_revision
WHERE head.world_id = $1 AND head.actor_code = $2 AND head.navigation_run_code = $3 AND head.plan_revision = 1`,
		worldID, created.Character.ActorCode, runCode,
	).Scan(&reservationStatus, &reservationRevision, &reservationPositionEventHash))
	require.Equal(t, "consumed", reservationStatus)
	require.Equal(t, int64(2), reservationRevision)
	require.Equal(t, actorPositionEventHash, reservationPositionEventHash)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_traffic_reservation_heads
SET reservation_status = 'released'
WHERE world_id = $1 AND actor_code = $2 AND navigation_run_code = $3 AND plan_revision = 1`,
		worldID, created.Character.ActorCode, runCode,
	)
	require.Error(t, err, "direct traffic reservation mutations must be rejected by the SQL gate")
}

func TestCityRealtimeCharacterLawEventCreatesAndExpiresIndependentEvidenceHandle(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-evidence-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_201_009)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Evidence Source " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "橘 真琴", ArchetypeCode: "resident.social",
		IdempotencyKey: "character-evidence-create-" + suffix,
	})
	require.NoError(t, err)

	conducted, err := cityService.PerformRealtimeCharacterActivity(ctx, service.CityRealtimeCharacterActivityInput{
		UserID: owner.ID, WorldID: worldID, ActivityCode: "conduct.disruption",
		IdempotencyKey: "character-evidence-conduct-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, conducted.Life)
	require.NotEmpty(t, conducted.Activity.LawCaseCode)
	creditBeforeExpiry := conducted.Life.CityCreditUnits
	standingBeforeExpiry := conducted.Life.CivicStandingMilli

	var evidenceCode, sourceHash, sourceKind, evidenceStatus string
	var sourceSequence, sourceFrame, evidenceRevision, evidenceDue int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT evidence_code, source_kind, source_law_event_sequence, source_law_event_hash,
       source_frame_sequence, evidence_revision, evidence_status, expiration_due_world_time_us
FROM city_realtime_character_case_evidence_heads
WHERE world_id = $1`, worldID).Scan(
		&evidenceCode, &sourceKind, &sourceSequence, &sourceHash,
		&sourceFrame, &evidenceRevision, &evidenceStatus, &evidenceDue,
	))
	require.Equal(t, "server.sealed_law_event", sourceKind)
	require.Equal(t, int64(1), sourceSequence)
	require.Greater(t, sourceFrame, int64(0))
	require.Equal(t, int64(1), evidenceRevision)
	require.Equal(t, "active", evidenceStatus)
	require.Greater(t, evidenceDue, int64(0))
	require.Contains(t, evidenceCode, "evidence.law.")
	require.Len(t, sourceHash, 64)

	var lawHash string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT event_hash
FROM city_realtime_character_law_events
WHERE world_id = $1 AND actor_code = $2 AND event_sequence = $3`,
		worldID, created.Character.ActorCode, sourceSequence,
	).Scan(&lawHash))
	require.Equal(t, lawHash, sourceHash)

	// Neither a manual Law result nor its evidence handle manufactures a report,
	// intake, extra Law case, reward, or financial side effect.
	var reportCount, intakeCount, lawCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM city_realtime_character_case_report_heads WHERE world_id = $1`, worldID).Scan(&reportCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM city_realtime_character_case_intake_heads WHERE world_id = $1`, worldID).Scan(&intakeCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM city_realtime_character_law_events WHERE world_id = $1`, worldID).Scan(&lawCount))
	require.Zero(t, reportCount)
	require.Zero(t, intakeCount)
	require.Equal(t, 1, lawCount)

	// Direct mutations cannot make a handle permanent or turn it into a verdict.
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_case_evidence_heads
SET evidence_status = 'active'
WHERE world_id = $1 AND evidence_code = $2`, worldID, evidenceCode)
	require.Error(t, err)

	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	time.Sleep(31 * time.Second)
	targetEffectiveUTC := clock.WorldTime.SourceEffectiveUTC.Add(30 * time.Second)
	expired := false
	for attempt := 0; attempt < 128; attempt++ {
		processed, processErr := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
			WorldID: worldID, EffectiveUTC: targetEffectiveUTC,
		})
		require.NoError(t, processErr)
		if !processed.Resolved {
			break
		}
		if count, ok := processed.Frame.PhaseSummary["case_evidence_expiry_count"].(int); ok && count == 1 {
			expired = true
			break
		}
	}
	require.True(t, expired, "expected the server-only evidence handle to expire")

	var eventCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT evidence_revision, evidence_status
FROM city_realtime_character_case_evidence_heads
WHERE world_id = $1 AND evidence_code = $2`, worldID, evidenceCode).Scan(&evidenceRevision, &evidenceStatus))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_character_case_evidence_events
WHERE world_id = $1 AND evidence_code = $2`, worldID, evidenceCode).Scan(&eventCount))
	require.Equal(t, int64(2), evidenceRevision)
	require.Equal(t, "expired", evidenceStatus)
	require.Equal(t, 2, eventCount)

	var creditAfterExpiry, standingAfterExpiry int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT city_credit_units, civic_standing_milli
FROM city_realtime_character_profiles
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&creditAfterExpiry, &standingAfterExpiry))
	require.Equal(t, creditBeforeExpiry, creditAfterExpiry)
	require.Equal(t, standingBeforeExpiry, standingAfterExpiry)
}

func TestCityRealtimeCharacterAgentAcknowledgesOnlyItsSealedLawCase(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-agent-case-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_994)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Character Agent Case " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "綾瀬 千夏", ArchetypeCode: "resident.social",
		IdempotencyKey: "character-agent-case-create-" + suffix,
	})
	require.NoError(t, err)

	// A manual, sealed activity creates the law fact before autonomy is enabled.
	// The Agent will only be allowed to acknowledge that existing fact; it cannot
	// create, reclassify, waive, or otherwise modify it.
	conducted, err := cityService.PerformRealtimeCharacterActivity(ctx, service.CityRealtimeCharacterActivityInput{
		UserID: owner.ID, WorldID: worldID, ActivityCode: "conduct.disruption",
		IdempotencyKey: "character-agent-case-conduct-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, conducted.Life)
	require.NotEmpty(t, conducted.Activity.LawCaseCode)
	creditBefore := conducted.Life.CityCreditUnits
	standingBefore := conducted.Life.CivicStandingMilli

	configured, err := cityService.ConfigureRealtimeCharacterAgent(ctx, service.CityRealtimeCharacterAgentConfigureInput{
		UserID: owner.ID, WorldID: worldID, ControlMode: "autonomous",
		Personality: &service.CityRealtimeCharacterPersonalitySeed{
			Values:         []string{"accountability"},
			HardBoundaries: []string{"avoid harm"},
			Preferences:    map[string]string{"conduct": "repair consequences"},
			Background:     "Lives in the shared city.",
		},
		IdempotencyKey: "character-agent-case-configure-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, configured.Agent)
	require.Equal(t, "autonomous", configured.Agent.ControlMode)

	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	wakeup, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, wakeup.Resolved)
	require.Equal(t, 1, wakeup.Frame.PhaseSummary["agent_wakeup_applied_count"])

	var agentCode, requestCode, observationPayload, lawCaseCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&agentCode))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.request_code, observation.payload::text
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_observations observation
  ON observation.world_id = request.world_id
 AND observation.observation_code = request.observation_code
WHERE request.world_id = $1 AND request.agent_code = $2 AND request.status = 'queued'`, worldID, agentCode).Scan(&requestCode, &observationPayload))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT case_code
FROM city_realtime_character_law_events
WHERE world_id = $1 AND actor_code = $2
ORDER BY event_sequence DESC
LIMIT 1`, worldID, created.Character.ActorCode).Scan(&lawCaseCode))
	require.Equal(t, conducted.Activity.LawCaseCode, lawCaseCode)
	require.Contains(t, observationPayload, `"schema_version": 4`)
	require.Contains(t, observationPayload, lawCaseCode)
	require.NotContains(t, observationPayload, "owner_user_id")
	require.NotContains(t, observationPayload, "provider")

	resolved, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: requestCode, WorkerID: "worker.case.integration",
		PreferredAction: "character.case.acknowledge",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", resolved.Status)
	require.NotEmpty(t, resolved.IntentCode)

	var actionCode, decisionCaseCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT action_code, arguments ->> 'case_code'
FROM city_realtime_agent_decisions
WHERE world_id = $1 AND decision_code = $2`, worldID, resolved.DecisionCode).Scan(&actionCode, &decisionCaseCode))
	require.Equal(t, "character.case.acknowledge", actionCode)
	require.Equal(t, lawCaseCode, decisionCaseCode)

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	applied, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, applied.Resolved)
	require.Equal(t, 1, applied.Frame.PhaseSummary["agent_intent_applied_count"])

	var intentStatus, responseCode, responseIntentCode string
	var responseRevision, cityCredit, civicStanding int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status FROM city_realtime_agent_intents
WHERE world_id = $1 AND intent_code = $2`, worldID, resolved.IntentCode).Scan(&intentStatus))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT response_code, source_intent_code
FROM city_realtime_character_case_response_events
WHERE world_id = $1 AND actor_code = $2 AND case_code = $3`, worldID, created.Character.ActorCode, lawCaseCode).Scan(&responseCode, &responseIntentCode))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT response_revision
FROM city_realtime_character_case_response_heads
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&responseRevision))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT city_credit_units, civic_standing_milli
FROM city_realtime_character_profiles
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&cityCredit, &civicStanding))
	require.Equal(t, "applied", intentStatus)
	require.Equal(t, "acknowledged", responseCode)
	require.Equal(t, resolved.IntentCode, responseIntentCode)
	require.Equal(t, int64(1), responseRevision)
	require.Equal(t, creditBefore, cityCredit, "acknowledgement cannot alter the already-applied fine")
	require.Equal(t, standingBefore, civicStanding, "acknowledgement cannot alter the already-applied ruling")

	// The immutable response head cannot be changed out of band.
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_case_response_heads
SET response_revision = response_revision + 1
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode)
	require.Error(t, err)

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	nextWakeup, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, nextWakeup.Resolved)

	var nextRequestCode, nextObservationPayload string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.request_code, observation.payload::text
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_observations observation
  ON observation.world_id = request.world_id
 AND observation.observation_code = request.observation_code
WHERE request.world_id = $1 AND request.agent_code = $2 AND request.status = 'queued'
ORDER BY request.requested_frame_sequence DESC
LIMIT 1`, worldID, agentCode).Scan(&nextRequestCode, &nextObservationPayload))
	require.Contains(t, nextObservationPayload, `"available_case_codes": []`)
	require.Contains(t, nextObservationPayload, `"available_social_target_codes": []`)
	require.Contains(t, nextObservationPayload, `"available_case_review_codes": ["`+lawCaseCode+`"]`)

	reviewDecision, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: nextRequestCode, WorkerID: "worker.case-review.integration",
		PreferredAction: "character.case.review.file",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", reviewDecision.Status)
	require.NotEmpty(t, reviewDecision.IntentCode)

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	filed, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, filed.Resolved)
	require.Equal(t, 1, filed.Frame.PhaseSummary["agent_intent_applied_count"])

	var reviewRevision, resolutionDueWorldTimeUS int64
	var reviewStatus, reviewIntentCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT review_revision, review_status, source_intent_code, resolution_due_world_time_us
FROM city_realtime_character_case_review_heads
WHERE world_id = $1 AND actor_code = $2 AND case_code = $3`, worldID, created.Character.ActorCode, lawCaseCode).Scan(
		&reviewRevision, &reviewStatus, &reviewIntentCode, &resolutionDueWorldTimeUS,
	))
	require.Equal(t, int64(1), reviewRevision)
	require.Equal(t, "filed", reviewStatus)
	require.Equal(t, reviewDecision.IntentCode, reviewIntentCode)
	require.Greater(t, resolutionDueWorldTimeUS, int64(0))

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	// The diagnostic clock is intentionally bounded to PostgreSQL by 30 seconds.
	// Wait until the fixed procedural closure timestamp enters that trusted window
	// rather than weakening the immutable clock profile for a test.
	time.Sleep(31 * time.Second)
	targetEffectiveUTC := clock.WorldTime.SourceEffectiveUTC.Add(30 * time.Second)
	caseReviewClosed := false
	for attempt := 0; attempt < 128; attempt++ {
		processed, processErr := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
			WorldID: worldID, EffectiveUTC: targetEffectiveUTC,
		})
		require.NoError(t, processErr)
		if !processed.Resolved {
			break
		}
		require.NotNil(t, processed.Frame)
		if count, ok := processed.Frame.PhaseSummary["case_review_closure_count"].(int); ok && count == 1 {
			caseReviewClosed = true
			break
		}
	}
	require.True(t, caseReviewClosed, "expected the procedural case-review closure to resolve")

	var reviewEventCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT review_revision, review_status
FROM city_realtime_character_case_review_heads
WHERE world_id = $1 AND actor_code = $2 AND case_code = $3`, worldID, created.Character.ActorCode, lawCaseCode).Scan(
		&reviewRevision, &reviewStatus,
	))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_character_case_review_events
WHERE world_id = $1 AND actor_code = $2 AND case_code = $3`, worldID, created.Character.ActorCode, lawCaseCode).Scan(&reviewEventCount))
	require.Equal(t, int64(2), reviewRevision)
	require.Equal(t, "closed_no_change", reviewStatus)
	require.Equal(t, 2, reviewEventCount)

	reviews, err := cityService.ListRealtimeMyCharacterCaseReviews(ctx, service.CityRealtimeCharacterCaseReviewListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, reviews.Items, 1)
	require.Equal(t, lawCaseCode, reviews.Items[0].CaseCode)
	require.Equal(t, "closed_no_change", reviews.Items[0].ReviewStatus)

	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT city_credit_units, civic_standing_milli
FROM city_realtime_character_profiles
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&cityCredit, &civicStanding))
	require.Equal(t, creditBefore, cityCredit, "case review cannot alter the already-applied fine")
	require.Equal(t, standingBefore, civicStanding, "case review cannot alter the already-applied ruling")
}

func TestCityRealtimeCharacterAgentGreetsOnlyASealedAdjacentPublicActor(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-agent-social-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_995)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Realtime Character Agent Social " + suffix,
		Timezone: "Asia/Tokyo", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "高橋 葵", ArchetypeCode: "resident.social",
		IdempotencyKey: "character-agent-social-create-" + suffix,
	})
	require.NoError(t, err)
	actorCode := created.Character.ActorCode

	traversable := loadIntegrationRealtimeTraversableSurface(t, ctx, worldID)
	var currentX, currentY int64
	var currentZ int32
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT x, y, z
FROM city_realtime_actor_states
WHERE world_id = $1 AND actor_code = $2`, worldID, actorCode).Scan(&currentX, &currentY, &currentZ))
	targets, occupied := loadIntegrationRealtimeSocialTargets(t, ctx, worldID, actorCode)
	path, target := findIntegrationRealtimeSocialPath(
		integrationRealtimePoint{X: currentX, Y: currentY}, targets, traversable, occupied,
	)
	require.NotEmpty(t, path, "the generated surface must connect the character to an adjacent public actor")
	require.LessOrEqual(t, len(path), 256)
	for index, step := range path {
		_, err = cityService.MoveRealtimeCharacter(ctx, service.CityRealtimeCharacterMoveInput{
			UserID: owner.ID, WorldID: worldID, X: step.X, Y: step.Y, Z: currentZ,
			IdempotencyKey: fmt.Sprintf("character-agent-social-position-%s-%03d", suffix, index),
		})
		require.NoError(t, err)
	}
	targetCode := target.ActorCode

	configured, err := cityService.ConfigureRealtimeCharacterAgent(ctx, service.CityRealtimeCharacterAgentConfigureInput{
		UserID: owner.ID, WorldID: worldID, ControlMode: "autonomous",
		Personality: &service.CityRealtimeCharacterPersonalitySeed{
			Values: []string{"community"}, HardBoundaries: []string{"do not expose private data"},
			Preferences: map[string]string{"social": "greet neighbors"},
			Background:  "Lives in the shared city.",
		},
		IdempotencyKey: "character-agent-social-configure-" + suffix,
	})
	require.NoError(t, err)
	require.Equal(t, "autonomous", configured.Agent.ControlMode)

	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	wakeup, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, wakeup.Resolved)

	var agentCode, requestCode, observationPayload string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND actor_code = $2`, worldID, actorCode).Scan(&agentCode))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.request_code, observation.payload::text
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_observations observation
  ON observation.world_id = request.world_id
 AND observation.observation_code = request.observation_code
WHERE request.world_id = $1 AND request.agent_code = $2 AND request.status = 'queued'
ORDER BY request.created_at DESC
LIMIT 1`, worldID, agentCode).Scan(&requestCode, &observationPayload))
	require.Contains(t, observationPayload, `"schema_version": 4`)
	require.Contains(t, observationPayload, targetCode)
	require.NotContains(t, observationPayload, "owner_user_id")
	require.NotContains(t, observationPayload, "provider")

	resolved, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: requestCode, WorkerID: "worker.social.integration",
		PreferredAction: "character.social.greet",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", resolved.Status)
	require.NotEmpty(t, resolved.IntentCode)

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	applied, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, applied.Resolved)
	require.Equal(t, 1, applied.Frame.PhaseSummary["agent_intent_applied_count"])

	var eventInitiator, eventRecipient, interactionCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT initiator_code, recipient_code, interaction_code
FROM city_realtime_character_social_events
WHERE world_id = $1 AND source_intent_code = $2`, worldID, resolved.IntentCode).Scan(
		&eventInitiator, &eventRecipient, &interactionCode,
	))
	require.Equal(t, actorCode, eventInitiator)
	require.Equal(t, targetCode, eventRecipient)
	require.Equal(t, "greeted", interactionCode)

	page, err := cityService.ListRealtimeMyCharacterSocialRelations(ctx, service.CityRealtimeCharacterSocialRelationListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, targetCode, page.Items[0].ActorCode)
	require.Equal(t, int64(1), page.Items[0].RelationRevision)
	require.Equal(t, int64(50), page.Items[0].AffinityMilli)
	require.Equal(t, int64(1), page.Items[0].InteractionCount)
	require.Equal(t, 1, page.Items[0].Explanation.SchemaVersion)
	require.Equal(t, "initial_contact", page.Items[0].Explanation.ContactTier)
	require.Equal(t, "outbound_only", page.Items[0].Explanation.InteractionPattern)
	require.Equal(t, "outbound", page.Items[0].Explanation.LastInteractionDirection)
	require.Equal(t, []string{"greeting_recorded"}, page.Items[0].Explanation.ReasonCodes)
}

func TestCityRealtimeCharacterAgentFilesReportAndExpiresEvidenceIsolatedCaseIntake(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-agent-case-report-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_996)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Realtime Character Agent Case Report " + suffix,
		Timezone: "Asia/Tokyo", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "佐藤 凪", ArchetypeCode: "resident.social",
		IdempotencyKey: "character-agent-case-report-create-" + suffix,
	})
	require.NoError(t, err)
	actorCode := created.Character.ActorCode

	traversable := loadIntegrationRealtimeTraversableSurface(t, ctx, worldID)
	var currentX, currentY int64
	var currentZ int32
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT x, y, z
FROM city_realtime_actor_states
WHERE world_id = $1 AND actor_code = $2`, worldID, actorCode).Scan(&currentX, &currentY, &currentZ))
	targets, occupied := loadIntegrationRealtimeSocialTargets(t, ctx, worldID, actorCode)
	path, target := findIntegrationRealtimeSocialPath(
		integrationRealtimePoint{X: currentX, Y: currentY}, targets, traversable, occupied,
	)
	require.NotEmpty(t, path, "the generated surface must connect the character to an adjacent public actor")
	require.LessOrEqual(t, len(path), 256)
	for index, step := range path {
		_, err = cityService.MoveRealtimeCharacter(ctx, service.CityRealtimeCharacterMoveInput{
			UserID: owner.ID, WorldID: worldID, X: step.X, Y: step.Y, Z: currentZ,
			IdempotencyKey: fmt.Sprintf("character-agent-case-report-position-%s-%03d", suffix, index),
		})
		require.NoError(t, err)
	}
	targetCode := target.ActorCode

	var creditBefore, standingBefore int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT city_credit_units, civic_standing_milli
FROM city_realtime_character_profiles
WHERE world_id = $1 AND actor_code = $2`, worldID, actorCode).Scan(&creditBefore, &standingBefore))

	configured, err := cityService.ConfigureRealtimeCharacterAgent(ctx, service.CityRealtimeCharacterAgentConfigureInput{
		UserID: owner.ID, WorldID: worldID, ControlMode: "autonomous",
		Personality: &service.CityRealtimeCharacterPersonalitySeed{
			Values: []string{"careful"}, HardBoundaries: []string{"do not make accusations"},
			Preferences: map[string]string{"civic": "file only bounded neutral receipts"},
			Background:  "Lives in the shared city.",
		},
		IdempotencyKey: "character-agent-case-report-configure-" + suffix,
	})
	require.NoError(t, err)
	require.Equal(t, "autonomous", configured.Agent.ControlMode)

	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	wakeup, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, wakeup.Resolved)

	var agentCode, requestCode, observationPayload string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND actor_code = $2`, worldID, actorCode).Scan(&agentCode))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.request_code, observation.payload::text
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_observations observation
  ON observation.world_id = request.world_id
 AND observation.observation_code = request.observation_code
WHERE request.world_id = $1 AND request.agent_code = $2 AND request.status = 'queued'
ORDER BY request.created_at DESC
LIMIT 1`, worldID, agentCode).Scan(&requestCode, &observationPayload))
	require.Contains(t, observationPayload, `"schema_version": 4`)
	require.Contains(t, observationPayload, targetCode)
	require.NotContains(t, observationPayload, "owner_user_id")
	require.NotContains(t, observationPayload, "provider")

	resolved, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: requestCode, WorkerID: "worker.case-report.integration",
		PreferredAction: "character.case.report.file",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", resolved.Status)
	require.NotEmpty(t, resolved.IntentCode)

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	applied, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, applied.Resolved)
	require.Equal(t, 1, applied.Frame.PhaseSummary["agent_intent_applied_count"])

	var eventReporter, eventSubject, eventType string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT reporter_actor_code, subject_actor_code, event_type
FROM city_realtime_character_case_report_events
WHERE world_id = $1 AND source_intent_code = $2`, worldID, resolved.IntentCode).Scan(
		&eventReporter, &eventSubject, &eventType,
	))
	require.Equal(t, actorCode, eventReporter)
	require.Equal(t, targetCode, eventSubject)
	require.Equal(t, "filed_unverified", eventType)

	var reportRevision int64
	var reportStatus, reportIntentCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT report_revision, report_status, source_intent_code
FROM city_realtime_character_case_report_heads
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`, worldID, actorCode, targetCode).Scan(
		&reportRevision, &reportStatus, &reportIntentCode,
	))
	require.Equal(t, int64(1), reportRevision)
	require.Equal(t, "filed_unverified", reportStatus)
	require.Equal(t, resolved.IntentCode, reportIntentCode)

	var intakeRevision, intakeReportSequence, intakeDueWorldTimeUS int64
	var intakeStatus, intakeIntentCode, intakeReportHash string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT intake_revision, intake_status, source_intent_code, report_event_sequence,
       report_event_hash, expiration_due_world_time_us
FROM city_realtime_character_case_intake_heads
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`, worldID, actorCode, targetCode).Scan(
		&intakeRevision, &intakeStatus, &intakeIntentCode, &intakeReportSequence,
		&intakeReportHash, &intakeDueWorldTimeUS,
	))
	require.Equal(t, int64(1), intakeRevision)
	require.Equal(t, "evidence_required", intakeStatus)
	require.Equal(t, resolved.IntentCode, intakeIntentCode)
	require.Equal(t, int64(1), intakeReportSequence)
	require.NotEmpty(t, intakeReportHash)
	require.Greater(t, intakeDueWorldTimeUS, int64(0))

	// The receipt is intentionally inert: it cannot change the reporter's
	// economy/progression state or create a Law Case by itself.
	var creditAfter, standingAfter int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT city_credit_units, civic_standing_milli
FROM city_realtime_character_profiles
WHERE world_id = $1 AND actor_code = $2`, worldID, actorCode).Scan(&creditAfter, &standingAfter))
	require.Equal(t, creditBefore, creditAfter)
	require.Equal(t, standingBefore, standingAfter)
	var lawCaseCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_character_law_events
WHERE world_id = $1 AND actor_code = $2`, worldID, actorCode).Scan(&lawCaseCount))
	require.Zero(t, lawCaseCount)

	page, err := cityService.ListRealtimeMyCharacterCaseReports(ctx, service.CityRealtimeCharacterCaseReportListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, targetCode, page.Items[0].SubjectActorCode)
	require.Equal(t, "filed_unverified", page.Items[0].ReportStatus)

	clock, err = cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	// The PostgreSQL-backed diagnostic clock intentionally permits at most a
	// 30-second trusted lead. Wait for the fixed expiry to enter that window;
	// changing the clock profile merely to make this test faster would weaken
	// the production replay boundary.
	time.Sleep(31 * time.Second)
	targetEffectiveUTC := clock.WorldTime.SourceEffectiveUTC.Add(30 * time.Second)
	intakeExpired := false
	for attempt := 0; attempt < 128; attempt++ {
		processed, processErr := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
			WorldID: worldID, EffectiveUTC: targetEffectiveUTC,
		})
		require.NoError(t, processErr)
		if !processed.Resolved {
			break
		}
		require.NotNil(t, processed.Frame)
		if count, ok := processed.Frame.PhaseSummary["case_intake_expiry_count"].(int); ok && count == 1 {
			intakeExpired = true
			break
		}
	}
	require.True(t, intakeExpired, "expected the evidence-required intake to expire without promotion")

	var intakeEventCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT intake_revision, intake_status
FROM city_realtime_character_case_intake_heads
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`, worldID, actorCode, targetCode).Scan(
		&intakeRevision, &intakeStatus,
	))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_character_case_intake_events
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`, worldID, actorCode, targetCode).Scan(&intakeEventCount))
	require.Equal(t, int64(2), intakeRevision)
	require.Equal(t, "expired_no_evidence", intakeStatus)
	require.Equal(t, 2, intakeEventCount)

	// The head is append-only and cannot be converted into a ruling out of band.
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_case_report_heads
SET report_status = 'resolved'
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`, worldID, actorCode, targetCode)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_case_intake_heads
SET intake_status = 'evidence_verified'
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`, worldID, actorCode, targetCode)
	require.Error(t, err)
}

func TestCityRealtimeCharacterReportCorrelatesOneSealedSourceWithoutPromotingIt(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	reporterOwner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-case-correlation-reporter-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	subjectOwner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-case-correlation-subject-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_201_010)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID: reporterOwner.ID, Name: "Realtime Case Correlation " + suffix,
		Timezone: "Asia/Tokyo", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	reporter, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: reporterOwner.ID, WorldID: worldID, PublicLabel: "田中 遥", ArchetypeCode: "resident.social",
		IdempotencyKey: "character-case-correlation-reporter-" + suffix,
	})
	require.NoError(t, err)
	subject, err := cityService.CreateRealtimeCharacter(adminCtx, service.CityRealtimeCharacterCreateInput{
		UserID: subjectOwner.ID, WorldID: worldID, PublicLabel: "山本 直人", ArchetypeCode: "resident.social",
		IdempotencyKey: "character-case-correlation-subject-" + suffix,
	})
	require.NoError(t, err)
	reporterCode := reporter.Character.ActorCode
	subjectCode := subject.Character.ActorCode

	// Move the reporter to the specific second player before the source is
	// captured. The policy's finite social list is lexicographically ordered,
	// and character.player.* therefore precedes generated npc.* targets.
	traversable := loadIntegrationRealtimeTraversableSurface(t, ctx, worldID)
	var reporterX, reporterY int64
	var reporterZ int32
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT x, y, z
FROM city_realtime_actor_states
WHERE world_id = $1 AND actor_code = $2`, worldID, reporterCode).Scan(&reporterX, &reporterY, &reporterZ))
	targets, occupied := loadIntegrationRealtimeSocialTargets(t, ctx, worldID, reporterCode)
	subjectTarget := integrationRealtimeSocialTarget{}
	for _, candidate := range targets {
		if candidate.ActorCode == subjectCode {
			subjectTarget = candidate
			break
		}
	}
	require.Equal(t, subjectCode, subjectTarget.ActorCode)
	path, reachedSubject := findIntegrationRealtimeSocialPath(
		integrationRealtimePoint{X: reporterX, Y: reporterY}, []integrationRealtimeSocialTarget{subjectTarget}, traversable, occupied,
	)
	require.NotEmpty(t, path, "the generated surface must connect the reporter to the selected player")
	require.Equal(t, subjectCode, reachedSubject.ActorCode)
	require.LessOrEqual(t, len(path), 512)
	for index, step := range path {
		_, err = cityService.MoveRealtimeCharacter(ctx, service.CityRealtimeCharacterMoveInput{
			UserID: reporterOwner.ID, WorldID: worldID, X: step.X, Y: step.Y, Z: reporterZ,
			IdempotencyKey: fmt.Sprintf("character-case-correlation-position-%s-%03d", suffix, index),
		})
		require.NoError(t, err)
	}

	// A manual action from the subject creates the only server-sealed source.
	// It is not visible in the report action's model-facing arguments.
	conducted, err := cityService.PerformRealtimeCharacterActivity(adminCtx, service.CityRealtimeCharacterActivityInput{
		UserID: subjectOwner.ID, WorldID: worldID, ActivityCode: "conduct.disruption",
		IdempotencyKey: "character-case-correlation-conduct-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, conducted.Life)
	require.NotEmpty(t, conducted.Activity.LawCaseCode)

	var evidenceCode, sourceHash string
	var sourceSequence, sourceFrame int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT evidence_code, source_law_event_sequence, source_law_event_hash, source_frame_sequence
FROM city_realtime_character_case_evidence_heads
WHERE world_id = $1 AND subject_actor_code = $2`, worldID, subjectCode).Scan(
		&evidenceCode, &sourceSequence, &sourceHash, &sourceFrame,
	))
	require.NotEmpty(t, evidenceCode)
	require.Greater(t, sourceSequence, int64(0))
	require.Len(t, sourceHash, 64)

	var creditBefore, standingBefore int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT city_credit_units, civic_standing_milli
FROM city_realtime_character_profiles
WHERE world_id = $1 AND actor_code = $2`, worldID, reporterCode).Scan(&creditBefore, &standingBefore))

	configured, err := cityService.ConfigureRealtimeCharacterAgent(ctx, service.CityRealtimeCharacterAgentConfigureInput{
		UserID: reporterOwner.ID, WorldID: worldID, ControlMode: "autonomous",
		Personality: &service.CityRealtimeCharacterPersonalitySeed{
			Values: []string{"careful"}, HardBoundaries: []string{"do not make accusations"},
			Preferences: map[string]string{"civic": "file only bounded neutral receipts"},
			Background:  "Lives in the shared city.",
		},
		IdempotencyKey: "character-case-correlation-configure-" + suffix,
	})
	require.NoError(t, err)
	require.Equal(t, "autonomous", configured.Agent.ControlMode)

	clock, err := cityService.GetRealtimeClock(ctx, reporterOwner.ID, worldID)
	require.NoError(t, err)
	wakeup, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, wakeup.Resolved)

	var agentCode, requestCode, observationPayload string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND actor_code = $2`, worldID, reporterCode).Scan(&agentCode))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.request_code, observation.payload::text
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_observations observation
  ON observation.world_id = request.world_id
 AND observation.observation_code = request.observation_code
WHERE request.world_id = $1 AND request.agent_code = $2 AND request.status = 'queued'
ORDER BY request.created_at DESC
LIMIT 1`, worldID, agentCode).Scan(&requestCode, &observationPayload))
	require.Contains(t, observationPayload, subjectCode)
	require.NotContains(t, observationPayload, evidenceCode)
	require.NotContains(t, observationPayload, sourceHash)
	require.NotContains(t, observationPayload, conducted.Activity.LawCaseCode)

	resolved, err := cityService.RunRealtimeAgentFakeDecision(adminCtx, service.CityRealtimeAgentFakeDecisionRunInput{
		WorldID: worldID, RequestCode: requestCode, WorkerID: "worker.case-correlation.integration",
		PreferredAction: "character.case.report.file",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", resolved.Status)

	var selectedTarget string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT arguments ->> 'target_actor_code'
FROM city_realtime_agent_decisions
WHERE world_id = $1 AND decision_code = $2`, worldID, resolved.DecisionCode).Scan(&selectedTarget))
	require.Equal(t, subjectCode, selectedTarget)

	clock, err = cityService.GetRealtimeClock(ctx, reporterOwner.ID, worldID)
	require.NoError(t, err)
	applied, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(time.Second),
	})
	require.NoError(t, err)
	require.True(t, applied.Resolved)
	require.Equal(t, 1, applied.Frame.PhaseSummary["agent_intent_applied_count"])

	var assignmentRevision, assignedFrame, assignmentLastFrame int64
	var assignmentStatus, assignedEvidenceCode, assignmentSourceHash string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT assignment_revision, assignment_status, evidence_code, source_law_event_hash,
       assigned_frame_sequence, last_frame_sequence
FROM city_realtime_character_case_evidence_assignment_heads
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`,
		worldID, reporterCode, subjectCode,
	).Scan(&assignmentRevision, &assignmentStatus, &assignedEvidenceCode, &assignmentSourceHash, &assignedFrame, &assignmentLastFrame))
	require.Equal(t, int64(1), assignmentRevision)
	require.Equal(t, "linked_active", assignmentStatus)
	require.Equal(t, evidenceCode, assignedEvidenceCode)
	require.Equal(t, sourceHash, assignmentSourceHash)
	require.Greater(t, assignedFrame, sourceFrame)
	require.Equal(t, assignedFrame, assignmentLastFrame)

	var dispatchRevision, dispatchQueuedFrame, dispatchLastFrame int64
	var dispatchStatus string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT dispatch_revision, dispatch_status, queued_frame_sequence, last_frame_sequence
FROM city_realtime_character_case_procedure_dispatch_heads
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`,
		worldID, reporterCode, subjectCode,
	).Scan(&dispatchRevision, &dispatchStatus, &dispatchQueuedFrame, &dispatchLastFrame))
	require.Equal(t, int64(1), dispatchRevision)
	require.Equal(t, "queued", dispatchStatus)
	require.Equal(t, assignedFrame, dispatchQueuedFrame)
	require.Equal(t, dispatchQueuedFrame, dispatchLastFrame)

	// The owner sees only a coarse process receipt. Neither the source handle
	// nor the Law record that created it escapes through this projection.
	process, err := cityService.ListRealtimeMyCharacterCaseProcess(ctx, service.CityRealtimeCharacterCaseProcessListInput{
		UserID: reporterOwner.ID, WorldID: worldID, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, process.Items, 1)
	require.Equal(t, subjectCode, process.Items[0].SubjectActorCode)
	require.Equal(t, "filed_unverified", process.Items[0].ReportStatus)
	require.Equal(t, "evidence_required", process.Items[0].IntakeStatus)
	require.Equal(t, "linked_active", process.Items[0].IndependentRecordStatus)
	require.Equal(t, "queued", process.Items[0].ProcedureDispatchStatus)
	processPayload, err := json.Marshal(process)
	require.NoError(t, err)
	for _, forbidden := range []string{evidenceCode, sourceHash, conducted.Activity.LawCaseCode, "evidence_code", "source_law"} {
		require.NotContains(t, string(processPayload), forbidden)
	}

	var creditAfterReport, standingAfterReport, reporterLawCount int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT city_credit_units, civic_standing_milli
FROM city_realtime_character_profiles
WHERE world_id = $1 AND actor_code = $2`, worldID, reporterCode).Scan(&creditAfterReport, &standingAfterReport))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_character_law_events
WHERE world_id = $1 AND actor_code = $2`, worldID, reporterCode).Scan(&reporterLawCount))
	require.Equal(t, creditBefore, creditAfterReport)
	require.Equal(t, standingBefore, standingAfterReport)
	require.Zero(t, reporterLawCount)

	// Source expiry is a narrow revocation of the correlation window. It must
	// not rewrite the receipt into a finding or a verified-evidence state.
	clock, err = cityService.GetRealtimeClock(ctx, reporterOwner.ID, worldID)
	require.NoError(t, err)
	time.Sleep(31 * time.Second)
	// A diagnostic source must be near the database clock. The persisted clock
	// observation intentionally remains the prior committed frame, so it cannot
	// be reused after waiting past the bounded skew window.
	targetEffectiveUTC := time.Now().UTC()
	assignmentClosed := false
	for attempt := 0; attempt < 128; attempt++ {
		processed, processErr := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
			WorldID: worldID, EffectiveUTC: targetEffectiveUTC,
		})
		require.NoError(t, processErr)
		if !processed.Resolved {
			break
		}
		if count, ok := processed.Frame.PhaseSummary["case_evidence_assignment_source_close_count"].(int); ok && count == 1 {
			require.Equal(t, 1, processed.Frame.PhaseSummary["case_procedure_dispatch_source_close_count"])
			assignmentClosed = true
			break
		}
	}
	require.True(t, assignmentClosed, "expected the sealed source window to close its one correlation")

	var assignmentEventCount, dispatchEventCount, evidenceRevision int64
	var evidenceStatus string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT assignment_revision, assignment_status
FROM city_realtime_character_case_evidence_assignment_heads
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`,
		worldID, reporterCode, subjectCode,
	).Scan(&assignmentRevision, &assignmentStatus))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_character_case_evidence_assignment_events
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`,
		worldID, reporterCode, subjectCode,
	).Scan(&assignmentEventCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT dispatch_revision, dispatch_status
FROM city_realtime_character_case_procedure_dispatch_heads
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`,
		worldID, reporterCode, subjectCode,
	).Scan(&dispatchRevision, &dispatchStatus))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_character_case_procedure_dispatch_events
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`,
		worldID, reporterCode, subjectCode,
	).Scan(&dispatchEventCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT evidence_revision, evidence_status
FROM city_realtime_character_case_evidence_heads
WHERE world_id = $1 AND evidence_code = $2`, worldID, evidenceCode).Scan(&evidenceRevision, &evidenceStatus))
	require.Equal(t, int64(2), assignmentRevision)
	require.Equal(t, "source_window_closed", assignmentStatus)
	require.Equal(t, int64(2), assignmentEventCount)
	require.Equal(t, int64(2), dispatchRevision)
	require.Equal(t, "source_window_closed", dispatchStatus)
	require.Equal(t, int64(2), dispatchEventCount)
	require.Equal(t, int64(2), evidenceRevision)
	require.Equal(t, "expired", evidenceStatus)

	process, err = cityService.ListRealtimeMyCharacterCaseProcess(ctx, service.CityRealtimeCharacterCaseProcessListInput{
		UserID: reporterOwner.ID, WorldID: worldID, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, process.Items, 1)
	require.Equal(t, "filed_unverified", process.Items[0].ReportStatus)
	require.Contains(t, []string{"evidence_required", "expired_no_evidence"}, process.Items[0].IntakeStatus)
	require.Equal(t, "source_window_closed", process.Items[0].IndependentRecordStatus)
	require.Equal(t, "source_window_closed", process.Items[0].ProcedureDispatchStatus)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_case_evidence_assignment_heads
SET assignment_status = 'evidence_verified'
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`, worldID, reporterCode, subjectCode)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_case_procedure_dispatch_heads
SET dispatch_status = 'reviewed'
WHERE world_id = $1 AND reporter_actor_code = $2 AND subject_actor_code = $3`, worldID, reporterCode, subjectCode)
	require.Error(t, err)
}

type integrationRealtimePoint struct {
	X int64
	Y int64
}

type integrationRealtimeSocialTarget struct {
	ActorCode string
	Point     integrationRealtimePoint
}

func loadIntegrationRealtimeTraversableSurface(
	t *testing.T,
	ctx context.Context,
	worldID int64,
) map[integrationRealtimePoint]bool {
	t.Helper()
	rows, err := integrationDB.QueryContext(ctx, `
SELECT chunk_x, chunk_y, payload
FROM city_realtime_spatial_chunks
WHERE world_id = $1 AND z = 0
ORDER BY chunk_y ASC, chunk_x ASC`, worldID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	traversable := make(map[integrationRealtimePoint]bool)
	for rows.Next() {
		var chunkX, chunkY int64
		var rawPayload []byte
		require.NoError(t, rows.Scan(&chunkX, &chunkY, &rawPayload))
		payload := cityspatial.OpenWorldChunkPayload{}
		require.NoError(t, json.Unmarshal(rawPayload, &payload))
		require.NoError(t, cityspatial.ValidateOpenWorldChunkPayload(payload))
		blocked := make(map[int]bool)
		for _, layer := range payload.Layers {
			if layer.Kind == cityspatial.RuleKindStructure {
				blocked[int(layer.Y)*payload.Width+int(layer.X)] = true
			}
		}
		cellIndex := 0
		for _, run := range payload.TerrainRuns {
			for offset := 0; offset < run.Length; offset++ {
				index := cellIndex + offset
				if blocked[index] {
					continue
				}
				switch run.DefinitionID {
				case "terrain.road", "terrain.sidewalk", "terrain.grass", "terrain.ground", "terrain.soil":
					traversable[integrationRealtimePoint{
						X: chunkX*int64(payload.Width) + int64(index%payload.Width),
						Y: chunkY*int64(payload.Height) + int64(index/payload.Width),
					}] = true
				}
			}
			cellIndex += run.Length
		}
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, traversable)
	return traversable
}

func loadIntegrationRealtimeSocialTargets(
	t *testing.T,
	ctx context.Context,
	worldID int64,
	actorCode string,
) ([]integrationRealtimeSocialTarget, map[integrationRealtimePoint]bool) {
	t.Helper()
	rows, err := integrationDB.QueryContext(ctx, `
SELECT identity.actor_code, identity.actor_kind, state.x, state.y
FROM city_realtime_actor_identities identity
JOIN city_realtime_actor_states state
  ON state.world_id = identity.world_id AND state.actor_code = identity.actor_code
WHERE identity.world_id = $1 AND identity.lifecycle_status = 'active'
ORDER BY identity.actor_code ASC`, worldID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	targets := make([]integrationRealtimeSocialTarget, 0)
	occupied := make(map[integrationRealtimePoint]bool)
	for rows.Next() {
		var code, kind string
		var point integrationRealtimePoint
		require.NoError(t, rows.Scan(&code, &kind, &point.X, &point.Y))
		occupied[point] = true
		if code != actorCode && (kind == "npc" || kind == "character") {
			targets = append(targets, integrationRealtimeSocialTarget{ActorCode: code, Point: point})
		}
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, targets)
	return targets, occupied
}

func findIntegrationRealtimeSocialPath(
	start integrationRealtimePoint,
	targets []integrationRealtimeSocialTarget,
	traversable map[integrationRealtimePoint]bool,
	occupied map[integrationRealtimePoint]bool,
) ([]integrationRealtimePoint, integrationRealtimeSocialTarget) {
	directions := [...]integrationRealtimePoint{{X: 1}, {X: -1}, {Y: 1}, {Y: -1}}
	goals := make(map[integrationRealtimePoint]integrationRealtimeSocialTarget)
	for _, target := range targets {
		for _, direction := range directions {
			point := integrationRealtimePoint{X: target.Point.X + direction.X, Y: target.Point.Y + direction.Y}
			if point == start || !traversable[point] || occupied[point] {
				continue
			}
			if current, found := goals[point]; !found || target.ActorCode < current.ActorCode {
				goals[point] = target
			}
		}
	}
	if len(goals) == 0 || !traversable[start] {
		return nil, integrationRealtimeSocialTarget{}
	}
	queue := []integrationRealtimePoint{start}
	parents := map[integrationRealtimePoint]integrationRealtimePoint{start: integrationRealtimePoint{}}
	foundGoal := integrationRealtimePoint{}
	found := false
	for len(queue) > 0 && !found {
		current := queue[0]
		queue = queue[1:]
		if _, isGoal := goals[current]; isGoal {
			foundGoal = current
			found = true
			break
		}
		for _, direction := range directions {
			next := integrationRealtimePoint{X: current.X + direction.X, Y: current.Y + direction.Y}
			if !traversable[next] || occupied[next] || next == start {
				continue
			}
			if _, seen := parents[next]; seen {
				continue
			}
			parents[next] = current
			queue = append(queue, next)
		}
	}
	if !found {
		return nil, integrationRealtimeSocialTarget{}
	}
	path := make([]integrationRealtimePoint, 0)
	for current := foundGoal; current != start; current = parents[current] {
		path = append(path, current)
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	return path, goals[foundGoal]
}
