//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type cityRealtimeAgentGatewayProviderFixture struct {
	err      error
	calls    int
	lastCall service.CityRealtimeAgentProviderRequest
}

type cityRealtimeAgentDecisionWorkerFeatureFixture struct{}

func (cityRealtimeAgentDecisionWorkerFeatureFixture) IsCitySimulationEnabled(context.Context) bool {
	return true
}

func (cityRealtimeAgentDecisionWorkerFeatureFixture) IsCityRealtimeAgentDecisionWorkerEnabled(context.Context) bool {
	return true
}

func (p *cityRealtimeAgentGatewayProviderFixture) ProviderCode() string {
	return "sub2api.gateway"
}

func (p *cityRealtimeAgentGatewayProviderFixture) Execute(_ context.Context, request service.CityRealtimeAgentProviderRequest) (service.CityRealtimeAgentProviderResponse, error) {
	p.calls++
	p.lastCall = request
	if p.err != nil {
		return service.CityRealtimeAgentProviderResponse{}, p.err
	}
	raw, err := json.Marshal(map[string]any{
		"schema_version":    "agent-decision-v1",
		"request_code":      request.RequestCode,
		"observation_hash":  request.ObservationHash,
		"precondition_hash": request.PreconditionHash,
		"intent": map[string]any{
			"action_code": "agent.wait",
			"arguments":   map[string]any{},
		},
		"reason_code": "gateway_fixture_wait",
	})
	if err != nil {
		return service.CityRealtimeAgentProviderResponse{}, err
	}
	return service.CityRealtimeAgentProviderResponse{DecisionEnvelope: raw}, nil
}

func createCityRealtimeAgentGatewayProfileFixture(
	t *testing.T,
	cityService *service.CityEconomyService,
	adminCtx context.Context,
	administratorID int64,
	groupID int64,
	code string,
	retryLimit int,
	circuitBreakerFailures int,
) *service.CityRealtimeAgentModelProfile {
	t.Helper()
	profile, err := cityService.CreateRealtimeAgentModelProfile(adminCtx, service.CityRealtimeAgentModelProfileCreateInput{
		AdministratorUserID:         administratorID,
		Code:                        code,
		DisplayName:                 "Gateway fixture " + code,
		ProviderCode:                "sub2api.gateway",
		PlatformGroupID:             &groupID,
		ModelIdentifier:             "gpt-4.1",
		AllowedAgentDefinitionCodes: []string{"character.npc"},
		Temperature:                 0,
		MaxInputTokens:              1024,
		MaxOutputTokens:             256,
		TimeoutMS:                   1000,
		MaxConcurrency:              4,
		RetryLimit:                  retryLimit,
		MaxProfileHourlyRequests:    100,
		MaxProfileHourlyTokens:      100000,
		MaxWorldHourlyRequests:      100,
		MaxWorldHourlyTokens:        100000,
		MaxAgentHourlyRequests:      100,
		MaxAgentHourlyTokens:        100000,
		MaxOwnerHourlyRequests:      100,
		MaxOwnerHourlyTokens:        100000,
		CircuitBreakerFailures:      circuitBreakerFailures,
		CircuitBreakerCooldownSecs:  30,
		PrivacyClass:                "hash_only",
		RetentionPolicy:             "hash_only",
		FallbackPolicy:              "defer",
	})
	require.NoError(t, err)
	return profile
}

func createCityRealtimeAgentGatewayWorldFixture(
	t *testing.T,
	cityService *service.CityEconomyService,
	adminCtx context.Context,
	ownerID int64,
	profileCode string,
	suffix string,
) (int64, string) {
	t.Helper()
	seed := int64(28_400_000 + len(suffix))
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       ownerID,
		Name:              "Realtime Gateway " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.BindRealtimeAgentModelProfileToWorld(adminCtx, service.CityRealtimeAgentModelProfileWorldBindingInput{
		AdministratorUserID: ownerID,
		WorldID:             worldID,
		AgentDefinitionCode: "character.npc",
		ProfileCode:         profileCode,
	})
	require.NoError(t, err)
	var agentCode string
	require.NoError(t, integrationDB.QueryRowContext(adminCtx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND agent_subtype = 'character.npc' AND lifecycle_status = 'active'
ORDER BY agent_code ASC
LIMIT 1`, worldID).Scan(&agentCode))
	return worldID, agentCode
}

func TestCityRealtimeAgentGatewayProviderRequiresRegisteredAdapterAndUsesSafeSnapshot(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("realtime-gateway-provider-owner-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	group := mustCreateGroup(t, client, &service.Group{Name: "realtime-gateway-provider-" + suffix})
	cityService := service.NewCityEconomyService(integrationDB)
	profile := createCityRealtimeAgentGatewayProfileFixture(t, cityService, adminCtx, owner.ID, group.ID, "gateway-provider-"+suffix, 1, 3)
	worldID, agentCode := createCityRealtimeAgentGatewayWorldFixture(t, cityService, adminCtx, owner.ID, profile.Code, suffix)
	queued, err := cityService.QueueRealtimeAgentDecision(adminCtx, service.CityRealtimeAgentDecisionRequestInput{
		WorldID: worldID, AgentCode: agentCode, TriggerKey: "scheduler.gateway.provider",
	})
	require.NoError(t, err)

	_, err = cityService.RunRealtimeAgentDecision(adminCtx, service.CityRealtimeAgentDecisionRunInput{
		WorldID: worldID, RequestCode: queued.RequestCode, WorkerID: "worker.gateway.provider",
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeAgentProviderUnavailable)
	var attemptsBeforeRegistration int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_agent_decision_attempts
WHERE world_id = $1 AND request_code = $2`, worldID, queued.RequestCode).Scan(&attemptsBeforeRegistration))
	require.Zero(t, attemptsBeforeRegistration, "an unavailable external provider must never fall back to fake or reserve a call")

	provider := &cityRealtimeAgentGatewayProviderFixture{}
	require.NoError(t, cityService.RegisterRealtimeAgentDecisionProvider(provider))
	resolved, err := cityService.RunRealtimeAgentDecision(adminCtx, service.CityRealtimeAgentDecisionRunInput{
		WorldID: worldID, RequestCode: queued.RequestCode, WorkerID: "worker.gateway.provider",
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", resolved.Status)
	require.Equal(t, 1, provider.calls)
	require.NotNil(t, provider.lastCall.Profile)
	require.Equal(t, profile.Code, provider.lastCall.Profile.Code)
	require.Equal(t, profile.Version, provider.lastCall.Profile.Version)
	require.Equal(t, "sub2api.gateway", provider.lastCall.Profile.ProviderCode)
	require.Equal(t, group.ID, *provider.lastCall.Profile.PlatformGroupID)
	require.NotEmpty(t, provider.lastCall.Observation)

	var providerCode, attemptStatus string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT provider_code, status
FROM city_realtime_agent_decision_attempts
WHERE world_id = $1 AND request_code = $2`, worldID, queued.RequestCode).Scan(&providerCode, &attemptStatus))
	require.Equal(t, "sub2api.gateway", providerCode)
	require.Equal(t, "succeeded", attemptStatus)
}

func TestCityRealtimeAgentGatewayTransientFailureRequeuesWithBackoff(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("realtime-gateway-retry-owner-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	group := mustCreateGroup(t, client, &service.Group{Name: "realtime-gateway-retry-" + suffix})
	cityService := service.NewCityEconomyService(integrationDB)
	profile := createCityRealtimeAgentGatewayProfileFixture(t, cityService, adminCtx, owner.ID, group.ID, "gateway-retry-"+suffix, 1, 3)
	worldID, agentCode := createCityRealtimeAgentGatewayWorldFixture(t, cityService, adminCtx, owner.ID, profile.Code, suffix)
	queued, err := cityService.QueueRealtimeAgentDecision(adminCtx, service.CityRealtimeAgentDecisionRequestInput{
		WorldID: worldID, AgentCode: agentCode, TriggerKey: "scheduler.gateway.retry",
	})
	require.NoError(t, err)
	provider := &cityRealtimeAgentGatewayProviderFixture{err: &service.CityRealtimeAgentDecisionProviderError{Code: "provider_timeout"}}
	require.NoError(t, cityService.RegisterRealtimeAgentDecisionProvider(provider))

	result, err := cityService.RunRealtimeAgentDecision(adminCtx, service.CityRealtimeAgentDecisionRunInput{
		WorldID: worldID, RequestCode: queued.RequestCode, WorkerID: "worker.gateway.retry",
	})
	require.NoError(t, err)
	require.Equal(t, "queued", result.Status)
	require.Equal(t, "provider_timeout", result.ErrorCode)
	require.NotNil(t, result.RetryNotBefore)
	require.True(t, result.RetryNotBefore.After(time.Now().UTC()))
	require.Equal(t, 1, provider.calls)

	var requestStatus, attemptStatus, attemptError, outboxStatus string
	var retryNotBefore time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.status, request.retry_not_before, attempt.status, attempt.error_code, outbox.status
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_decision_attempts attempt
  ON attempt.world_id = request.world_id AND attempt.request_code = request.request_code
JOIN city_realtime_agent_outbox outbox
  ON outbox.world_id = request.world_id AND outbox.request_code = request.request_code
WHERE request.world_id = $1 AND request.request_code = $2`, worldID, queued.RequestCode).Scan(
		&requestStatus, &retryNotBefore, &attemptStatus, &attemptError, &outboxStatus,
	))
	require.Equal(t, "queued", requestStatus)
	require.True(t, retryNotBefore.After(time.Now().UTC()))
	require.Equal(t, "failed", attemptStatus)
	require.Equal(t, "provider_timeout", attemptError)
	require.Equal(t, "queued", outboxStatus)

	notDue, err := cityService.RunRealtimeAgentDecision(adminCtx, service.CityRealtimeAgentDecisionRunInput{
		WorldID: worldID, RequestCode: queued.RequestCode, WorkerID: "worker.gateway.retry",
	})
	require.NoError(t, err)
	require.Equal(t, "queued", notDue.Status)
	require.NotNil(t, notDue.RetryNotBefore)
	require.Equal(t, 1, provider.calls, "not-due work must not make another provider call")
	var attemptCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_agent_decision_attempts
WHERE world_id = $1 AND request_code = $2`, worldID, queued.RequestCode).Scan(&attemptCount))
	require.Equal(t, 1, attemptCount)
}

func TestCityRealtimeAgentDecisionWorkerConsumesDefaultDeterministicProfile(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-agent-worker-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(28_600_000 + len(suffix))
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Agent Worker " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	var agentCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND agent_subtype = 'character.npc' AND lifecycle_status = 'active'
ORDER BY agent_code ASC
LIMIT 1`, worldID).Scan(&agentCode))
	queued, err := cityService.QueueRealtimeAgentDecision(adminCtx, service.CityRealtimeAgentDecisionRequestInput{
		WorldID: worldID, AgentCode: agentCode, TriggerKey: "worker.default.deterministic",
	})
	require.NoError(t, err)

	worker := service.NewCityRealtimeAgentDecisionWorker(integrationDB, cityService, time.Second, 4)
	worker.SetFeatureChecker(cityRealtimeAgentDecisionWorkerFeatureFixture{})
	report, err := worker.ProcessDue(ctx)
	require.NoError(t, err)
	require.Equal(t, service.CityRealtimeAgentDecisionWorkerReport{Candidates: 1, Completed: 1}, report)

	var requestStatus, attemptStatus, outboxStatus string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.status, attempt.status, outbox.status
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_decision_attempts attempt
  ON attempt.world_id = request.world_id AND attempt.request_code = request.request_code
JOIN city_realtime_agent_outbox outbox
  ON outbox.world_id = request.world_id AND outbox.request_code = request.request_code
WHERE request.world_id = $1 AND request.request_code = $2`, worldID, queued.RequestCode).Scan(
		&requestStatus, &attemptStatus, &outboxStatus,
	))
	require.Equal(t, "accepted", requestStatus)
	require.Equal(t, "succeeded", attemptStatus)
	require.Equal(t, "succeeded", outboxStatus)
}

func TestCityRealtimeAgentDecisionWorkerDefersUnregisteredGatewayWithoutAttempt(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-agent-worker-gateway-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	group := mustCreateGroup(t, client, &service.Group{Name: "realtime-agent-worker-gateway-" + suffix})
	cityService := service.NewCityEconomyService(integrationDB)
	profile := createCityRealtimeAgentGatewayProfileFixture(
		t, cityService, adminCtx, owner.ID, group.ID, "gateway-worker-"+suffix, 2, 3,
	)
	worldID, agentCode := createCityRealtimeAgentGatewayWorldFixture(t, cityService, adminCtx, owner.ID, profile.Code, suffix)
	queued, err := cityService.QueueRealtimeAgentDecision(adminCtx, service.CityRealtimeAgentDecisionRequestInput{
		WorldID: worldID, AgentCode: agentCode, TriggerKey: "worker.gateway.unregistered",
	})
	require.NoError(t, err)

	worker := service.NewCityRealtimeAgentDecisionWorker(integrationDB, cityService, time.Second, 4)
	worker.SetFeatureChecker(cityRealtimeAgentDecisionWorkerFeatureFixture{})
	report, err := worker.ProcessDue(ctx)
	require.NoError(t, err)
	require.Equal(t, service.CityRealtimeAgentDecisionWorkerReport{Candidates: 1, Deferred: 1}, report)

	var requestStatus, outboxStatus string
	var retryNotBefore time.Time
	var attempts int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.status, request.retry_not_before, outbox.status,
       (SELECT COUNT(*) FROM city_realtime_agent_decision_attempts attempt
        WHERE attempt.world_id = request.world_id AND attempt.request_code = request.request_code)
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_outbox outbox
  ON outbox.world_id = request.world_id AND outbox.request_code = request.request_code
WHERE request.world_id = $1 AND request.request_code = $2`, worldID, queued.RequestCode).Scan(
		&requestStatus, &retryNotBefore, &outboxStatus, &attempts,
	))
	require.Equal(t, "queued", requestStatus)
	require.Equal(t, "queued", outboxStatus)
	require.True(t, retryNotBefore.After(time.Now().UTC()))
	require.Zero(t, attempts)

	queue, err := cityService.ListRealtimeAgentDecisionQueue(adminCtx, service.CityRealtimeAgentDecisionQueueListInput{
		WorldID: worldID, Status: "queued", Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, queue.Items, 1)
	item := queue.Items[0]
	require.Equal(t, queued.RequestCode, item.RequestCode)
	require.Equal(t, "character.npc", item.AgentDefinitionCode)
	require.Equal(t, "queued", item.RequestStatus)
	require.Equal(t, "queued", item.OutboxStatus)
	require.Zero(t, item.AttemptCount)
	require.NotNil(t, item.RetryNotBefore)
	require.Equal(t, profile.Code, *item.ModelProfileCode)
	require.Equal(t, profile.Version, *item.ModelProfileVersion)
	require.Nil(t, item.LastAttemptStatus)
	require.Nil(t, item.LastErrorCode)

	_, err = cityService.ListRealtimeAgentDecisionQueue(ctx, service.CityRealtimeAgentDecisionQueueListInput{WorldID: worldID})
	require.ErrorIs(t, err, service.ErrCityManagementRequired)

	woken, err := cityService.RetryRealtimeAgentDecisionNow(adminCtx, service.CityRealtimeAgentDecisionRetryInput{
		AdministratorUserID: owner.ID,
		WorldID:             worldID,
		RequestCode:         queued.RequestCode,
	})
	require.NoError(t, err)
	require.Equal(t, queued.RequestCode, woken.RequestCode)
	require.Equal(t, "queued", woken.RequestStatus)
	require.NotNil(t, woken.PreviousRetryNotBefore)

	var clearedRetry sql.NullTime
	var eventType string
	var actorID int64
	var payloadHash string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.retry_not_before, event.event_type, event.actor_user_id, event.payload_hash
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_decision_operator_events event
  ON event.world_id = request.world_id AND event.request_code = request.request_code
WHERE request.world_id = $1 AND request.request_code = $2`, worldID, queued.RequestCode).Scan(
		&clearedRetry, &eventType, &actorID, &payloadHash,
	))
	require.False(t, clearedRetry.Valid)
	require.Equal(t, "retry_requested", eventType)
	require.Equal(t, owner.ID, actorID)
	require.Len(t, payloadHash, 64)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_requests
SET retry_not_before = NOW() + INTERVAL '1 minute'
WHERE world_id = $1 AND request_code = $2`, worldID, queued.RequestCode)
	require.Error(t, err, "direct SQL must not forge a worker deferral or administrator retry")

	_, err = cityService.RetryRealtimeAgentDecisionNow(ctx, service.CityRealtimeAgentDecisionRetryInput{
		AdministratorUserID: owner.ID,
		WorldID:             worldID,
		RequestCode:         queued.RequestCode,
	})
	require.ErrorIs(t, err, service.ErrCityManagementRequired)
}

func TestCityRealtimeAgentDecisionDeadLetterQuarantineIsOperationalAndReleasable(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-agent-dead-letter-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(28_700_000 + len(suffix))
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Agent Dead Letter " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	var agentCode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT agent_code
FROM city_realtime_agent_instances
WHERE world_id = $1 AND agent_subtype = 'character.npc' AND lifecycle_status = 'active'
ORDER BY agent_code ASC
LIMIT 1`, worldID).Scan(&agentCode))
	queued, err := cityService.QueueRealtimeAgentDecision(adminCtx, service.CityRealtimeAgentDecisionRequestInput{
		WorldID: worldID, AgentCode: agentCode, TriggerKey: "worker.dead-letter.review",
	})
	require.NoError(t, err)

	var framesBefore int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT timeline_frame_sequence
FROM city_world_time_states
WHERE world_id = $1`, worldID).Scan(&framesBefore))

	quarantined, err := cityService.QuarantineRealtimeAgentDecision(adminCtx, service.CityRealtimeAgentDecisionDeadLetterInput{
		AdministratorUserID: owner.ID,
		WorldID:             worldID,
		RequestCode:         queued.RequestCode,
		ReasonCode:          "operator_review",
	})
	require.NoError(t, err)
	require.Equal(t, "quarantined", quarantined.DeadLetterStatus)
	require.Equal(t, "operator_review", quarantined.ReasonCode)

	worker := service.NewCityRealtimeAgentDecisionWorker(integrationDB, cityService, time.Second, 4)
	worker.SetFeatureChecker(cityRealtimeAgentDecisionWorkerFeatureFixture{})
	report, err := worker.ProcessDue(ctx)
	require.NoError(t, err)
	require.Equal(t, service.CityRealtimeAgentDecisionWorkerReport{}, report)

	_, err = cityService.RunRealtimeAgentDecision(adminCtx, service.CityRealtimeAgentDecisionRunInput{
		WorldID: worldID, RequestCode: queued.RequestCode, WorkerID: "worker.dead-letter.direct",
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeAgentDecisionQuarantined)

	var requestStatus, outboxStatus string
	var attempts int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request.status, outbox.status,
       (SELECT COUNT(*) FROM city_realtime_agent_decision_attempts attempt
        WHERE attempt.world_id = request.world_id AND attempt.request_code = request.request_code)
FROM city_realtime_agent_decision_requests request
JOIN city_realtime_agent_outbox outbox
  ON outbox.world_id = request.world_id AND outbox.request_code = request.request_code
WHERE request.world_id = $1 AND request.request_code = $2`, worldID, queued.RequestCode).Scan(
		&requestStatus, &outboxStatus, &attempts,
	))
	require.Equal(t, "queued", requestStatus)
	require.Equal(t, "queued", outboxStatus)
	require.Zero(t, attempts)

	var framesDuringQuarantine int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT timeline_frame_sequence
FROM city_world_time_states
WHERE world_id = $1`, worldID).Scan(&framesDuringQuarantine))
	require.Equal(t, framesBefore, framesDuringQuarantine, "quarantine must not seal a world frame")

	queue, err := cityService.ListRealtimeAgentDecisionQueue(adminCtx, service.CityRealtimeAgentDecisionQueueListInput{
		WorldID: worldID, Status: "queued", Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, queue.Items, 1)
	require.Equal(t, "quarantined", *queue.Items[0].DeadLetterStatus)
	require.Equal(t, "operator_review", *queue.Items[0].DeadLetterReasonCode)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_agent_decision_dead_letters
SET dead_letter_status = 'released'
WHERE world_id = $1 AND request_code = $2`, worldID, queued.RequestCode)
	require.Error(t, err, "direct SQL must not bypass the dead-letter administrator gate")

	_, err = cityService.QuarantineRealtimeAgentDecision(ctx, service.CityRealtimeAgentDecisionDeadLetterInput{
		AdministratorUserID: owner.ID,
		WorldID:             worldID,
		RequestCode:         queued.RequestCode,
		ReasonCode:          "operator_review",
	})
	require.ErrorIs(t, err, service.ErrCityManagementRequired)

	released, err := cityService.ReleaseRealtimeAgentDecisionDeadLetter(adminCtx, service.CityRealtimeAgentDecisionDeadLetterReleaseInput{
		AdministratorUserID: owner.ID,
		WorldID:             worldID,
		RequestCode:         queued.RequestCode,
	})
	require.NoError(t, err)
	require.Equal(t, "released", released.DeadLetterStatus)
	require.Equal(t, "operator_review", released.ReasonCode)

	var firstEvent, secondEvent string
	var eventCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT
    COUNT(*),
    MIN(event_type) FILTER (WHERE event_type = 'quarantined'),
    MIN(event_type) FILTER (WHERE event_type = 'released')
FROM city_realtime_agent_decision_dead_letter_events
WHERE world_id = $1 AND request_code = $2`, worldID, queued.RequestCode).Scan(&eventCount, &firstEvent, &secondEvent))
	require.Equal(t, 2, eventCount)
	require.Equal(t, "quarantined", firstEvent)
	require.Equal(t, "released", secondEvent)
	eventPage, err := cityService.ListRealtimeAgentDecisionDeadLetterEvents(adminCtx, service.CityRealtimeAgentDecisionDeadLetterEventListInput{
		WorldID: worldID, RequestCode: queued.RequestCode, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, eventPage.Items, 2)
	require.Equal(t, "released", eventPage.Items[0].EventType)
	require.Equal(t, "operator_release", eventPage.Items[0].ReasonCode)
	require.Equal(t, owner.ID, eventPage.Items[0].ActorUserID)
	require.Equal(t, "quarantined", eventPage.Items[1].EventType)
	require.Equal(t, "operator_review", eventPage.Items[1].ReasonCode)

	var framesAfterRelease int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT timeline_frame_sequence
FROM city_world_time_states
WHERE world_id = $1`, worldID).Scan(&framesAfterRelease))
	require.Equal(t, framesBefore, framesAfterRelease, "release must not seal a world frame")

	report, err = worker.ProcessDue(ctx)
	require.NoError(t, err)
	require.Equal(t, service.CityRealtimeAgentDecisionWorkerReport{Candidates: 1, Completed: 1}, report)
}
