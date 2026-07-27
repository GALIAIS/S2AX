package securityaudit

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisPayloadStoreRoundTripTTLNamespaceAndDelete(t *testing.T) {
	address := strings.TrimSpace(os.Getenv(promptAuditRedisTestEnv))
	if address == "" {
		t.Skip(promptAuditRedisTestEnv + " is not set")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	store := NewRedisPayloadStore(client)
	ctx := context.Background()
	const jobID int64 = 987654321
	const canary = "PROMPT_CANARY_REDIS_ONLY_PAYLOAD"
	_ = store.Delete(ctx, jobID)
	require.NoError(t, store.Set(ctx, jobID, canary, 2*DefaultPayloadTTL))
	require.Equal(t, PayloadKeyPrefix+"987654321", payloadKey(jobID))
	value, err := store.Get(ctx, jobID)
	require.NoError(t, err)
	require.Equal(t, canary, value)
	ttl, err := client.TTL(ctx, payloadKey(jobID)).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, time.Duration(0))
	require.LessOrEqual(t, ttl, DefaultPayloadTTL)
	ttls, err := store.TTLs(ctx, []int64{jobID, jobID + 1})
	require.NoError(t, err)
	require.Greater(t, ttls[jobID], time.Duration(0))
	require.LessOrEqual(t, ttls[jobID], DefaultPayloadTTL)
	require.LessOrEqual(t, ttls[jobID+1], time.Duration(0))
	require.NoError(t, store.Delete(ctx, jobID))
	_, err = store.Get(ctx, jobID)
	require.ErrorIs(t, err, redis.Nil)
}

func TestPromptRuntimeAggregatesConfigWorkersQueueRedisEndpointsAndGuardMetrics(t *testing.T) {
	address := strings.TrimSpace(os.Getenv(promptAuditRedisTestEnv))
	if address == "" {
		t.Skip(promptAuditRedisTestEnv + " is not set")
	}
	db := openPromptAuditIntegrationDB(t)
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	config := &fakeConfigStore{active: true, cfg: ActiveConfig{
		RiskControlEnabled: true, Enabled: true, WorkerCount: 3, QueueCapacity: 123,
		ConfigVersion: 9, AllGroups: true,
	}}
	metrics := NewAtomicMetrics()
	metrics.Observe(DecisionBlock, 25*time.Millisecond)
	metrics.IncFailover()
	metrics.IncEnqueued()
	metrics.IncDropped()
	service := NewPromptService(
		config,
		NewPostgreSQLRepository(db),
		NewRedisPayloadStore(client),
		NewOpenAICompatibleScanner(),
		metrics,
	)
	service.probes["guard-1"] = ProbeResult{OK: true, Status: "healthy", HTTPStatus: 200}

	runtime := service.Runtime(context.Background())
	require.Equal(t, ModeAsync, runtime.EffectiveMode)
	require.Equal(t, int64(9), runtime.ExpectedConfigVersion)
	require.Equal(t, int64(9), runtime.ActiveConfigVersion)
	require.Equal(t, 3, runtime.WorkerTotal)
	require.Equal(t, 123, runtime.QueueCapacity)
	require.Equal(t, "ok", runtime.DatabaseStatus)
	require.Equal(t, "ok", runtime.RedisStatus)
	require.Contains(t, runtime.Endpoints, "guard-1")
	require.Equal(t, int64(1), runtime.GuardMetrics.Total)
	require.Equal(t, int64(1), runtime.GuardMetrics.Blocked)
	require.Equal(t, int64(1), runtime.GuardMetrics.Failovers)
	require.Equal(t, int64(25), runtime.GuardMetrics.LatencyP95MS)
	require.Equal(t, int64(1), runtime.EnqueuedTotal)
	require.Equal(t, int64(1), runtime.DroppedTotal)
	// The runner has not been started in this integration test, so the honest
	// process status is degraded rather than a fabricated running heartbeat.
	require.Equal(t, "degraded", runtime.ProcessStatus)
}

func TestPromptAuditJobGovernanceUsesRealRedisPayloadState(t *testing.T) {
	address := strings.TrimSpace(os.Getenv(promptAuditRedisTestEnv))
	if address == "" {
		t.Skip(promptAuditRedisTestEnv + " is not set")
	}
	db := openPromptAuditIntegrationDB(t)
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	payload := NewRedisPayloadStore(client)
	repo := NewPostgreSQLRepository(db)
	service := &PromptService{repo: repo, payload: payload}
	ctx := context.Background()
	actorID := insertIdentity(t, db, "users")

	retryJob, err := repo.CreateStagingWithCapacity(ctx, integrationSnapshot("retry"), 1, 3, 10)
	require.NoError(t, err)
	require.NoError(t, repo.MarkStagingFailed(ctx, retryJob.ID, "endpoint_unavailable", "sanitized"))
	require.NoError(t, payload.Set(ctx, retryJob.ID, "transient retry canary", time.Minute))
	retried, err := service.RetryJob(ctx, retryJob.ID, actorID, "endpoint recovered")
	require.NoError(t, err)
	require.Equal(t, "queued", retried.Status)

	discardJob, err := repo.CreateStagingWithCapacity(ctx, integrationSnapshot("discard"), 1, 3, 10)
	require.NoError(t, err)
	require.NoError(t, repo.MarkStagingFailed(ctx, discardJob.ID, "invalid_response", "sanitized"))
	require.NoError(t, payload.Set(ctx, discardJob.ID, "transient discard canary", time.Minute))
	discarded, err := service.DiscardJob(ctx, discardJob.ID, actorID, "invalid detector contract")
	require.NoError(t, err)
	require.Equal(t, "discarded", discarded.Status)
	_, err = payload.Get(ctx, discardJob.ID)
	require.ErrorIs(t, err, redis.Nil)

	expiredJob, err := repo.CreateStagingWithCapacity(ctx, integrationSnapshot("expired"), 1, 3, 10)
	require.NoError(t, err)
	require.NoError(t, repo.MarkStagingFailed(ctx, expiredJob.ID, "payload_missing", "sanitized"))
	_, err = service.RetryJob(ctx, expiredJob.ID, actorID, "try missing payload")
	require.ErrorIs(t, err, ErrJobPayloadUnavailable)

	page, err := service.ListJobs(ctx, JobFilter{}, 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(3), page.Total)
	states := map[int64]string{}
	for _, item := range page.Items {
		states[item.Job.ID] = item.PayloadState
	}
	require.Equal(t, payloadStateExpired, states[expiredJob.ID])
	require.Contains(t, []string{payloadStateNotApplicable, payloadStateExpired}, states[retryJob.ID],
		"a concurrently running integration worker may consume the retried job before this read")
	require.Equal(t, payloadStateNotApplicable, states[discardJob.ID])
}
