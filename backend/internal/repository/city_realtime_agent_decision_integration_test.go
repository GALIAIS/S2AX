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
	require.Equal(t, "1.5.0", policyVersion)

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
	require.Contains(t, observationPayload, `"schema_version": 3`)
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

	var nextObservationPayload string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT observation.payload::text
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_observations observation
  ON observation.world_id = request.world_id
 AND observation.observation_code = request.observation_code
WHERE request.world_id = $1 AND request.agent_code = $2 AND request.status = 'queued'`, worldID, agentCode).Scan(&nextObservationPayload))
	require.Contains(t, nextObservationPayload, `"available_case_codes": []`)
	require.Contains(t, nextObservationPayload, `"available_social_target_codes": []`)
	require.NotContains(t, nextObservationPayload, lawCaseCode)
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
	require.Contains(t, observationPayload, `"schema_version": 3`)
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
