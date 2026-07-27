package securityaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const promptAuditPostgresTestEnv = "PROMPT_AUDIT_TEST_POSTGRES_DSN"

func openPromptAuditIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(promptAuditPostgresTestEnv))
	if dsn == "" {
		t.Skip(promptAuditPostgresTestEnv + " is not set")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(16)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(ctx))
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id BIGSERIAL PRIMARY KEY,
			email VARCHAR(320) NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS groups (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(160) NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS api_keys (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(160) NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS settings (
			key VARCHAR(255) PRIMARY KEY,
			value TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		ALTER TABLE users ADD COLUMN IF NOT EXISTS email VARCHAR(320) NOT NULL DEFAULT '';
		ALTER TABLE groups ADD COLUMN IF NOT EXISTS name VARCHAR(160) NOT NULL DEFAULT '';
		ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS name VARCHAR(160) NOT NULL DEFAULT '';
		ALTER TABLE users ALTER COLUMN email SET DEFAULT '';
		ALTER TABLE groups ALTER COLUMN name SET DEFAULT '';
		ALTER TABLE api_keys ALTER COLUMN name SET DEFAULT '';
		CREATE TABLE IF NOT EXISTS usage_logs (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL,
			api_key_id BIGINT NOT NULL,
			group_id BIGINT,
			model VARCHAR(256) NOT NULL DEFAULT '',
			requested_model VARCHAR(256) NOT NULL DEFAULT '',
			ip_address VARCHAR(128) NOT NULL DEFAULT '',
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			actual_cost NUMERIC(24, 10) NOT NULL DEFAULT 0,
			duration_ms INTEGER,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS ops_error_logs (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT,
			api_key_id BIGINT,
			group_id BIGINT,
			model VARCHAR(256) NOT NULL DEFAULT '',
			requested_model VARCHAR(256) NOT NULL DEFAULT '',
			client_ip INET,
			is_business_limited BOOLEAN NOT NULL DEFAULT FALSE,
			is_count_tokens BOOLEAN NOT NULL DEFAULT FALSE,
			duration_ms INTEGER,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	require.NoError(t, err)
	for _, name := range []string{
		"181_prompt_audit.sql",
		"182_prompt_audit_full_prompt.sql",
		"289_prompt_audit_evidence_protection.sql",
		"290_prompt_audit_failure_semantics.sql",
		"291_security_audit_core.sql",
		"292_security_audit_behavior_signals.sql",
		"293_security_audit_endpoint_runtime_health.sql",
		"294_security_audit_exception_semantics.sql",
		"295_security_audit_remove_unsupported_record_hash.sql",
		"296_security_audit_policy_transition_history.sql",
		"297_security_audit_live_shadow_evaluation.sql",
		"298_security_audit_enforce_action_whitelist.sql",
		"299_security_audit_exception_revocation_audit.sql",
		"300_prompt_audit_job_operations.sql",
		"301_prompt_audit_detector_metadata.sql",
		"302_prompt_audit_config_versions.sql",
	} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		require.NoError(t, err)
		// The migration runner can retry an interrupted deployment; the migration
		// must therefore be safe to execute more than once.
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	resetPromptAuditIntegrationDB(t, db)
	return db
}

func resetPromptAuditIntegrationDB(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`TRUNCATE TABLE security_audit_notifications, security_audit_shadow_evaluations, security_audit_shadow_watermark, security_audit_signal_evaluations, security_audit_signal_windows, security_audit_signal_watermark, security_audit_evidence_access_logs, security_audit_feedback, security_audit_case_events, security_audit_cases, security_audit_outbox, security_audit_actions, security_audit_evidence, security_audit_decisions, security_audit_policy_versions, security_audit_exceptions, security_audit_endpoint_health, prompt_audit_admission_counters, prompt_audit_evidence_access_logs, prompt_audit_job_operations, prompt_audit_events, prompt_audit_jobs, prompt_audit_config_versions, usage_logs, ops_error_logs, api_keys, users, groups, settings RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
	_, err = db.Exec(`
INSERT INTO security_audit_signal_watermark(
    id,last_aggregated_at,last_evaluated_at,last_evaluated_window_id
) VALUES (1,date_trunc('minute',NOW()),date_trunc('minute',NOW()),0)
ON CONFLICT (id) DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(`
INSERT INTO security_audit_shadow_watermark(id,last_decision_pk)
VALUES (1,0)
ON CONFLICT (id) DO NOTHING`)
	require.NoError(t, err)
}

func insertIdentity(t *testing.T, db *sql.DB, table string) int64 {
	t.Helper()
	var id int64
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	switch table {
	case "users":
		require.NoError(t, db.QueryRow(
			`INSERT INTO users(email,password_hash) VALUES ($1,'integration-test') RETURNING id`,
			"security-audit-"+suffix+"@example.test",
		).Scan(&id))
	case "groups":
		require.NoError(t, db.QueryRow(
			`INSERT INTO groups(name) VALUES ($1) RETURNING id`,
			"security-audit-"+suffix,
		).Scan(&id))
	case "api_keys":
		var ownerID int64
		require.NoError(t, db.QueryRow(
			`INSERT INTO users(email,password_hash) VALUES ($1,'integration-test') RETURNING id`,
			"security-audit-key-owner-"+suffix+"@example.test",
		).Scan(&ownerID))
		require.NoError(t, db.QueryRow(
			`INSERT INTO api_keys(user_id,key,name) VALUES ($1,$2,$3) RETURNING id`,
			ownerID, "sk-security-audit-"+suffix, "security-audit-"+suffix,
		).Scan(&id))
	default:
		t.Fatalf("unsupported identity table %q", table)
	}
	return id
}

func integrationSnapshot(seed string) PromptSnapshot {
	return PromptSnapshot{
		RequestID: "request-" + seed, UsernameSnapshot: "user-" + seed,
		UserEmailSnapshot: "user-" + seed + "@example.test", APIKeyNameSnapshot: "key-" + seed,
		GroupName: "group-" + seed, Provider: "openai", Endpoint: "/v1/chat/completions",
		Protocol: "openai_chat", Model: "gpt-test", PromptHash: strings.Repeat(seed[:1], 64),
		RedactedPreview: "redacted-" + seed, PromptLength: len([]rune(seed)), MessageCount: 1,
	}
}

func integrationResult(decision EventDecision) *NormalizedResult {
	result := &NormalizedResult{
		Decision: decision, RiskLevel: RiskLow, Action: ActionAllow, Safety: "Safe",
		Categories: []string{}, MatchedScanners: []string{}, ScannerScores: map[string]float64{},
		ScannerEvidence: map[string]string{}, ScannerBackend: "qwen3guard-openai",
		ScannerVersion: "test", GuardEndpointID: "guard-1", PolicyID: "priority",
		PolicyVersion: 1, ChunkTotal: 1, LatencyMS: 2,
	}
	if decision != EventPass {
		result.RiskLevel = RiskCritical
		result.Action = ActionBlock
		result.Safety = "Unsafe"
		result.Categories = []string{"pii"}
		result.MatchedScanners = []string{"pii"}
		result.ScannerScores["pii"] = 1
		result.ScannerEvidence["pii"] = "redacted evidence"
	}
	return result
}

func TestPromptAuditMigrationSchemaAndLeakageGate(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	ctx := context.Background()

	rows, err := db.QueryContext(ctx, `SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema='public' AND table_name IN ('prompt_audit_jobs','prompt_audit_events')`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	forbidden := []string{"raw_prompt", "raw_request", "payload", "token", "authorization", "credential"}
	for rows.Next() {
		var tableName, columnName string
		require.NoError(t, rows.Scan(&tableName, &columnName))
		lower := strings.ToLower(columnName)
		for _, word := range forbidden {
			require.NotContainsf(t, lower, word, "%s.%s is a forbidden raw/credential column", tableName, columnName)
		}
	}
	require.NoError(t, rows.Err())
	var encryptedEvidenceColumns int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='prompt_audit_events'
		  AND column_name='evidence_ciphertext'`).Scan(&encryptedEvidenceColumns))
	require.Equal(t, 1, encryptedEvidenceColumns, "encrypted evidence storage must remain available")

	indexRows, err := db.QueryContext(ctx, `SELECT indexname FROM pg_indexes
		WHERE schemaname='public' AND tablename IN ('prompt_audit_jobs','prompt_audit_events')`)
	require.NoError(t, err)
	defer func() { _ = indexRows.Close() }()
	indexes := map[string]bool{}
	for indexRows.Next() {
		var name string
		require.NoError(t, indexRows.Scan(&name))
		indexes[name] = true
	}
	for _, name := range []string{
		"idx_prompt_audit_jobs_schedule", "idx_prompt_audit_jobs_request", "idx_prompt_audit_jobs_user_created",
		"idx_prompt_audit_jobs_api_key_created", "idx_prompt_audit_jobs_group_created", "idx_prompt_audit_jobs_prompt_hash",
		"idx_prompt_audit_jobs_created", "idx_prompt_audit_events_job", "idx_prompt_audit_events_request",
		"idx_prompt_audit_events_decision_created", "idx_prompt_audit_events_risk_created",
		"idx_prompt_audit_events_user_created", "idx_prompt_audit_events_api_key_created",
		"idx_prompt_audit_events_group_created", "idx_prompt_audit_events_prompt_hash", "idx_prompt_audit_events_created",
	} {
		require.Truef(t, indexes[name], "missing index %s", name)
	}

	_, err = db.ExecContext(ctx, `INSERT INTO prompt_audit_jobs(status) VALUES ('unknown')`)
	require.Error(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO prompt_audit_jobs(prompt_length) VALUES (-1)`)
	require.Error(t, err)
	var jobID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO prompt_audit_jobs DEFAULT VALUES RETURNING id`).Scan(&jobID))
	_, err = db.ExecContext(ctx, `INSERT INTO prompt_audit_events(job_id,chunk_total) VALUES ($1,-1)`, jobID)
	require.Error(t, err)
}

func TestPromptAuditDatabaseEncryptsEvidenceAndNeverReturnsPlaintextByDefault(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	repo := NewPostgreSQLRepository(db)
	ctx := context.Background()
	const promptCanary = "PROMPT_AUDIT_CANARY_SECRET_DO_NOT_PERSIST"
	request := Request{
		RequestID: "canary-request", Provider: "openai",
		Endpoint: "/v1/chat/completions", Protocol: "openai_chat", Model: "gpt-test", Stage: "http",
		Body: []byte(`{"messages":[{"role":"user","content":"` + promptCanary + `"}]}`),
	}
	snapshot, err := ExtractPromptSnapshot(request)
	require.NoError(t, err)
	require.NotContains(t, snapshot.RedactedPreview, promptCanary)
	require.Contains(t, snapshot.FullPrompt, promptCanary)
	protected, err := protectPromptEvidence(snapshot, integrationResult(EventCritical), prefixEncryptor{}, time.Now())
	require.NoError(t, err)
	event, err := repo.RecordBlocking(ctx, protected, 1, integrationResult(EventCritical), true)
	require.NoError(t, err)
	adminJSON, err := json.Marshal(event)
	require.NoError(t, err)
	require.NotContains(t, string(adminJSON), promptCanary)
	require.NotContains(t, event.Snapshot.RedactedPreview, promptCanary)
	require.True(t, event.EvidenceAvailable)
	require.Equal(t, EvidenceEncrypted, event.EvidenceStatus)

	var storedFullPrompt, storedCiphertext string
	require.NoError(t, db.QueryRow(`SELECT full_prompt,evidence_ciphertext FROM prompt_audit_events WHERE id=$1`, event.ID).Scan(&storedFullPrompt, &storedCiphertext))
	require.Empty(t, storedFullPrompt)
	require.NotEqual(t, promptCanary, storedCiphertext)
	require.Contains(t, storedCiphertext, promptCanary)

	detail, err := repo.GetEvent(ctx, event.ID)
	require.NoError(t, err)
	detailJSON, err := json.Marshal(detail)
	require.NoError(t, err)
	require.NotContains(t, string(detailJSON), promptCanary)

	service := &PromptService{config: &fakeConfigStore{}, repo: repo, clock: realClock{}}
	adminID := insertIdentity(t, db, "users")
	reveal, err := service.RevealEventEvidence(ctx, event.ID, adminID, "investigate blocked request")
	require.NoError(t, err)
	require.Contains(t, reveal.FullPrompt, promptCanary)
	var accessCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM prompt_audit_evidence_access_logs WHERE event_id=$1 AND outcome='revealed'`, event.ID).Scan(&accessCount))
	require.Equal(t, 1, accessCount)

	var jobJSON string
	require.NoError(t, db.QueryRow(`SELECT row_to_json(j)::text FROM prompt_audit_jobs j WHERE id=$1`, event.JobID).Scan(&jobJSON))
	require.NotContains(t, jobJSON, promptCanary)

	failedJob, err := repo.CreateStagingWithCapacity(ctx, integrationSnapshot("error"), 1, 3, 10)
	require.NoError(t, err)
	const errorCanary = "GUARD_RAW_RESPONSE_CANARY_SECRET"
	require.NoError(t, repo.MarkStagingFailed(ctx, failedJob.ID, "payload_store_failed", "raw guard body: "+errorCanary))
	var code, message string
	require.NoError(t, db.QueryRow(`SELECT last_error_code,last_error_message FROM prompt_audit_jobs WHERE id=$1`, failedJob.ID).Scan(&code, &message))
	require.Equal(t, "payload_store_failed", code)
	require.Equal(t, stableErrorMessage(code), message)
	require.NotContains(t, message, errorCanary)
	require.LessOrEqual(t, len([]rune(message)), 160)
}

func TestPromptAuditFailedJobOperationsAreAuditedAndStateChecked(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	repo := NewPostgreSQLRepository(db)
	ctx := context.Background()
	actorID := insertIdentity(t, db, "users")

	job, err := repo.CreateStagingWithCapacity(ctx, integrationSnapshot("failure"), 7, 3, 10)
	require.NoError(t, err)
	require.NoError(t, repo.MarkStagingFailed(ctx, job.ID, "endpoint_unavailable", "must not persist"))

	page, err := repo.ListJobs(ctx, JobFilter{Status: "failed"}, 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, "endpoint_unavailable", page.Items[0].Job.LastErrorCode)
	require.NotEmpty(t, page.FailureReasons)
	require.Equal(t, "endpoint_unavailable", page.FailureReasons[0].ErrorCode)

	_, err = repo.TransitionJob(ctx, job.ID, "retry", actorID, "operator retry", false)
	require.ErrorIs(t, err, ErrJobPayloadUnavailable)

	retried, err := repo.TransitionJob(ctx, job.ID, "retry", actorID, "operator retry", true)
	require.NoError(t, err)
	require.Equal(t, "queued", retried.Status)
	require.Zero(t, retried.Attempts)
	require.Empty(t, retried.LastErrorCode)

	_, err = repo.TransitionJob(ctx, job.ID, "quarantine", actorID, "unexpected state", true)
	require.ErrorIs(t, err, ErrJobTransitionConflict)
	_, err = db.ExecContext(ctx, `
UPDATE prompt_audit_jobs
SET status='failed',processed_at=NOW(),last_error_code='invalid_response',last_error_message=''
WHERE id=$1`, job.ID)
	require.NoError(t, err)

	quarantined, err := repo.TransitionJob(ctx, job.ID, "quarantine", actorID, "manual investigation", true)
	require.NoError(t, err)
	require.Equal(t, "quarantined", quarantined.Status)
	discarded, err := repo.TransitionJob(ctx, job.ID, "discard", actorID, "investigation completed", true)
	require.NoError(t, err)
	require.Equal(t, "discarded", discarded.Status)

	operations, err := repo.ListJobOperations(ctx, []int64{job.ID})
	require.NoError(t, err)
	require.Len(t, operations[job.ID], 3)
	require.Equal(t, "discard", operations[job.ID][0].Operation)
	require.Equal(t, actorID, operations[job.ID][0].ActorID)
	require.Equal(t, "quarantine", operations[job.ID][1].Operation)
	require.Equal(t, "retry", operations[job.ID][2].Operation)

	_, err = db.ExecContext(ctx, `
INSERT INTO prompt_audit_job_operations(
    job_id,operation,from_status,to_status,actor_id,reason
) VALUES ($1,'retry','failed','queued',$2,'x')`, job.ID, actorID)
	require.Error(t, err, "database must reject operation reasons shorter than three characters")
}

func TestPromptAuditRepositoryAdmissionClaimFencingAndEventTransaction(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	repo := NewPostgreSQLRepository(db)
	ctx := context.Background()

	start := make(chan struct{})
	type admissionResult struct {
		job *Job
		err error
	}
	results := make(chan admissionResult, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			job, err := repo.CreateStagingWithCapacity(ctx, integrationSnapshot(string(rune('a'+index))), 1, 3, 1)
			results <- admissionResult{job: job, err: err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	var accepted *Job
	rejected := 0
	for result := range results {
		if result.err == nil {
			require.Nil(t, accepted)
			accepted = result.job
			continue
		}
		require.True(t, errors.Is(result.err, ErrQueueFull) || errors.Is(result.err, ErrQueueAdmissionBusy))
		rejected++
	}
	require.NotNil(t, accepted)
	require.Equal(t, 1, rejected)
	stats, err := repo.QueueStats(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.Active)
	require.NoError(t, repo.PublishQueued(ctx, accepted.ID))

	claimStart := make(chan struct{})
	claims := make(chan *Job, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-claimStart
			job, claimed, claimErr := repo.ClaimNextJob(ctx, time.Now().Add(time.Second))
			require.NoError(t, claimErr)
			if claimed {
				claims <- job
			}
		}()
	}
	close(claimStart)
	wg.Wait()
	close(claims)
	claimedJobs := make([]*Job, 0, 1)
	for job := range claims {
		claimedJobs = append(claimedJobs, job)
	}
	require.Len(t, claimedJobs, 1)
	firstClaim := claimedJobs[0]
	require.Equal(t, int64(1), firstClaim.ClaimVersion)

	reclaimed, err := repo.ReclaimStale(ctx, time.Now().Add(time.Hour), time.Now().Add(time.Hour), 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), reclaimed)
	secondClaim, claimed, err := repo.ClaimNextJob(ctx, time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, claimed)
	require.Greater(t, secondClaim.ClaimVersion, firstClaim.ClaimVersion)
	require.ErrorIs(t, repo.RefreshLease(ctx, firstClaim.ID, firstClaim.ClaimVersion, time.Now()), ErrLeaseLost)
	_, err = repo.Complete(ctx, firstClaim, integrationResult(EventCritical), true)
	require.ErrorIs(t, err, ErrLeaseLost)

	event, err := repo.Complete(ctx, secondClaim, integrationResult(EventCritical), true)
	require.NoError(t, err)
	require.NotNil(t, event)
	var status string
	var eventCount int
	require.NoError(t, db.QueryRow(`SELECT status FROM prompt_audit_jobs WHERE id=$1`, secondClaim.ID).Scan(&status))
	require.Equal(t, "done", status)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM prompt_audit_events WHERE job_id=$1`, secondClaim.ID).Scan(&eventCount))
	require.Equal(t, 1, eventCount)

	staging, err := repo.CreateStagingWithCapacity(ctx, integrationSnapshot("stale"), 1, 3, 10)
	require.NoError(t, err)
	reclaimed, err = repo.ReclaimStale(ctx, time.Now().Add(time.Hour), time.Now().Add(time.Hour), 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), reclaimed)
	require.NoError(t, db.QueryRow(`SELECT status FROM prompt_audit_jobs WHERE id=$1`, staging.ID).Scan(&status))
	require.Equal(t, "failed", status)
}

func TestPromptAuditRepositoryForeignKeysFiltersAndStableIdentitySnapshots(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	repo := NewPostgreSQLRepository(db)
	ctx := context.Background()
	userID := insertIdentity(t, db, "users")
	apiKeyID := insertIdentity(t, db, "api_keys")
	groupID := insertIdentity(t, db, "groups")
	snapshot := integrationSnapshot("identity")
	snapshot.UserID, snapshot.APIKeyID, snapshot.GroupID = userID, apiKeyID, &groupID
	event, err := repo.RecordBlocking(ctx, snapshot, 7, integrationResult(EventCritical), true)
	require.NoError(t, err)
	require.NotNil(t, event)

	start, end := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	page, err := repo.ListEvents(ctx, EventFilter{
		Decision: string(EventCritical), RiskLevel: string(RiskCritical), Endpoint: snapshot.Endpoint,
		GroupID: &groupID, UserID: &userID, APIKeyID: &apiKeyID, RequestID: snapshot.RequestID,
		PromptHash: snapshot.PromptHash, Keyword: snapshot.UsernameSnapshot, StartAt: &start, EndAt: &end,
	}, 1, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.Items, 1)
	require.NotEmpty(t, page.Items[0].IssueSummaries)
	require.Equal(t, snapshot.UsernameSnapshot, page.Items[0].Snapshot.UsernameSnapshot)
	require.Equal(t, snapshot.UserEmailSnapshot, page.Items[0].Snapshot.UserEmailSnapshot)
	require.Equal(t, snapshot.APIKeyNameSnapshot, page.Items[0].Snapshot.APIKeyNameSnapshot)

	_, err = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM api_keys WHERE id=$1`, apiKeyID)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM groups WHERE id=$1`, groupID)
	require.NoError(t, err)
	stored, err := repo.GetEvent(ctx, event.ID)
	require.NoError(t, err)
	require.Zero(t, stored.Snapshot.UserID)
	require.Zero(t, stored.Snapshot.APIKeyID)
	require.Nil(t, stored.Snapshot.GroupID)
	require.Equal(t, snapshot.UsernameSnapshot, stored.Snapshot.UsernameSnapshot)
	require.Equal(t, snapshot.UserEmailSnapshot, stored.Snapshot.UserEmailSnapshot)
	require.Equal(t, snapshot.APIKeyNameSnapshot, stored.Snapshot.APIKeyNameSnapshot)

	_, err = db.Exec(`DELETE FROM prompt_audit_jobs WHERE id=$1`, event.JobID)
	require.NoError(t, err)
	_, err = repo.GetEvent(ctx, event.ID)
	require.ErrorIs(t, err, ErrEventNotFound)
}

func TestPromptAuditRepositoryHighWaterAndSafeDeletion(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	repo := NewPostgreSQLRepository(db)
	ctx := context.Background()
	first, err := repo.RecordBlocking(ctx, integrationSnapshot("first"), 1, integrationResult(EventCritical), true)
	require.NoError(t, err)
	second, err := repo.RecordBlocking(ctx, integrationSnapshot("second"), 1, integrationResult(EventCritical), true)
	require.NoError(t, err)
	start, end := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	filter := EventFilter{Decision: string(EventCritical), StartAt: &start, EndAt: &end}
	preview, err := repo.PreviewDelete(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, int64(2), preview.MatchedCount)
	require.Equal(t, second.ID, preview.SnapshotMaxID)
	require.Equal(t, FilterHash(preview.FilterSummary, preview.SnapshotMaxID), preview.FilterHash)

	newer, err := repo.RecordBlocking(ctx, integrationSnapshot("newer"), 1, integrationResult(EventCritical), true)
	require.NoError(t, err)
	result, err := repo.DeleteEventsByFilter(ctx, filter, preview.SnapshotMaxID, 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), result.DeletedEvents)
	require.Equal(t, int64(2), result.DeletedJobs)
	_, err = repo.GetEvent(ctx, first.ID)
	require.ErrorIs(t, err, ErrEventNotFound)
	_, err = repo.GetEvent(ctx, second.ID)
	require.ErrorIs(t, err, ErrEventNotFound)
	_, err = repo.GetEvent(ctx, newer.ID)
	require.NoError(t, err, "an event created after preview must survive high-water deletion")

	processingEvent, err := repo.RecordBlocking(ctx, integrationSnapshot("processing"), 1, integrationResult(EventCritical), true)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE prompt_audit_jobs SET status='processing' WHERE id=$1`, processingEvent.JobID)
	require.NoError(t, err)
	deleteResult, err := repo.DeleteEvent(ctx, processingEvent.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleteResult.DeletedEvents)
	require.Zero(t, deleteResult.DeletedJobs)
	var remaining int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM prompt_audit_jobs WHERE id=$1`, processingEvent.JobID).Scan(&remaining))
	require.Equal(t, 1, remaining, "processing jobs must not be deleted as orphans")

	batchOne, err := repo.RecordBlocking(ctx, integrationSnapshot("batch-one"), 1, integrationResult(EventCritical), true)
	require.NoError(t, err)
	batchTwo, err := repo.RecordBlocking(ctx, integrationSnapshot("batch-two"), 1, integrationResult(EventCritical), true)
	require.NoError(t, err)
	ids := []int64{batchTwo.ID, batchOne.ID, batchOne.ID}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	batchResult, err := repo.DeleteEventsByIDs(ctx, ids)
	require.NoError(t, err)
	require.Equal(t, int64(2), batchResult.DeletedEvents)
}

func TestPromptAuditServiceConfirmationKeepsPostPreviewEventsAndConcurrentDeletesAreSafe(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	repo := NewPostgreSQLRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	filter := EventFilter{Decision: string(EventCritical), StartAt: &start, EndAt: &end}

	for i := 0; i < 12; i++ {
		_, err := repo.RecordBlocking(ctx, integrationSnapshot(fmt.Sprintf("event-%02d", i)), 1, integrationResult(EventCritical), true)
		require.NoError(t, err)
	}
	service := &PromptService{
		config: &fakeConfigStore{}, repo: repo, payload: NewRedisPayloadStore(nil), clock: fixedClock{now: now},
	}
	preview, err := service.PreviewDelete(ctx, filter, 77)
	require.NoError(t, err)
	require.Equal(t, int64(12), preview.MatchedCount)

	newer, err := repo.RecordBlocking(ctx, integrationSnapshot("post-preview"), 1, integrationResult(EventCritical), true)
	require.NoError(t, err)
	result, err := service.DeleteByFilter(ctx, DeleteByFilterRequest{
		Filter: filter, SnapshotMaxID: preview.SnapshotMaxID, FilterHash: preview.FilterHash,
		ConfirmationToken: preview.ConfirmationToken, Confirm: true,
	}, 77)
	require.NoError(t, err)
	require.Equal(t, int64(12), result.DeletedEvents)
	_, err = repo.GetEvent(ctx, newer.ID)
	require.NoError(t, err, "events created after delete-preview must survive")

	resetPromptAuditIntegrationDB(t, db)
	for i := 0; i < 24; i++ {
		_, err := repo.RecordBlocking(ctx, integrationSnapshot(fmt.Sprintf("race-%02d", i)), 1, integrationResult(EventCritical), true)
		require.NoError(t, err)
	}
	preview, err = repo.PreviewDelete(ctx, filter)
	require.NoError(t, err)

	type deleteOutcome struct {
		result *DeleteResult
		err    error
	}
	outcomes := make(chan deleteOutcome, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deleted, deleteErr := repo.DeleteEventsByFilter(ctx, filter, preview.SnapshotMaxID, 1)
			outcomes <- deleteOutcome{result: deleted, err: deleteErr}
		}()
	}
	wg.Wait()
	close(outcomes)
	var deletedTotal int64
	for outcome := range outcomes {
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		deletedTotal += outcome.result.DeletedEvents
	}
	require.Equal(t, int64(24), deletedTotal, "concurrent deleters must neither double-count nor strand matching events")
	remaining, err := repo.ListEvents(ctx, filter, 1, 100)
	require.NoError(t, err)
	require.Zero(t, remaining.Total)
}

func TestSecurityAuditExceptionRevocationPersistsActorReasonAndConstraint(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	repo := NewPostgreSQLRepository(db)
	ctx := context.Background()
	actorID := insertIdentity(t, db, "users")

	item, err := repo.CreateException(ctx, CreateExceptionRequest{
		Name: "incident exception", ScopeType: "user", ScopeID: "42",
		Effect: "warn_only", Reason: "approved for incident response", Permanent: true,
	}, actorID)
	require.NoError(t, err)

	revoked, err := repo.ExpireException(ctx, item.ID, actorID, "incident response completed")
	require.NoError(t, err)
	require.Equal(t, "revoked", revoked.Status)
	require.NotNil(t, revoked.RevokedBy)
	require.Equal(t, actorID, *revoked.RevokedBy)
	require.Equal(t, "incident response completed", revoked.RevokedReason)
	require.NotNil(t, revoked.ExpiredAt)

	_, err = db.ExecContext(ctx, `
UPDATE security_audit_exceptions
SET status='revoked',expired_at=NOW(),revoked_reason=''
WHERE id=$1`, item.ID)
	require.Error(t, err, "database must reject unattributed exception revocation")
}

func TestSecurityAuditActionOutboxWorkerAndRevertCompleteTransactionally(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	repo := NewPostgreSQLRepository(db)
	ctx := context.Background()

	event, err := repo.RecordBlocking(ctx, integrationSnapshot("action-worker"), 1, integrationResult(EventCritical), true)
	require.NoError(t, err)
	var actionID int64
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT a.id
FROM security_audit_actions a
JOIN security_audit_decisions d ON d.id=a.decision_pk
WHERE d.source_type='prompt_audit' AND d.source_event_id=$1`, event.ID).Scan(&actionID))

	claimed, ok, err := repo.ClaimNextAction(ctx, "integration-worker", 30*time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, actionID, claimed.ID)
	require.Equal(t, ActionStatusProcessing, claimed.Status)
	require.NoError(t, repo.ExecuteClaimedAction(ctx, claimed, "integration-worker"))

	succeeded, err := repo.GetAction(ctx, actionID)
	require.NoError(t, err)
	require.Equal(t, ActionStatusSucceeded, succeeded.Status)
	var outboxStatus, caseStatus string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT status FROM security_audit_outbox WHERE action_id=$1`, actionID).Scan(&outboxStatus))
	require.Equal(t, "published", outboxStatus)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT status FROM security_audit_cases WHERE primary_decision_pk=$1`, succeeded.DecisionPK).Scan(&caseStatus))
	require.Equal(t, "open", caseStatus)

	actorID := insertIdentity(t, db, "users")
	reverted, err := repo.RevertAction(ctx, actionID, actorID)
	require.NoError(t, err)
	require.Equal(t, ActionStatusReverted, reverted.Status)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT status FROM security_audit_cases WHERE primary_decision_pk=$1`, succeeded.DecisionPK).Scan(&caseStatus))
	require.Equal(t, "dismissed", caseStatus)
}

func TestSecurityAuditBehaviorSignalAggregationEvaluationAndIdempotence(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	repo := NewPostgreSQLRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)
	userID := insertIdentity(t, db, "users")
	apiKeyID := insertIdentity(t, db, "api_keys")
	groupID := insertIdentity(t, db, "groups")
	_, err := db.ExecContext(ctx, `UPDATE users SET email='behavior@example.test' WHERE id=$1`, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE api_keys SET name='behavior-key' WHERE id=$1`, apiKeyID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE groups SET name='behavior-group' WHERE id=$1`, groupID)
	require.NoError(t, err)

	config := canonicalSecurityPolicy(defaultSecurityPolicyConfig())
	config.Signals.Enabled = true
	config.Signals.Rules = []BehaviorSignalRule{{
		ID: "integration_request_burst", Enabled: true, Metric: "request_count",
		SubjectType: "api_key", WindowMinutes: 1, Threshold: 1,
		MinimumSamples: 1, Severity: "high",
	}}
	config.Actions.High = []string{"open_case", "notify_admin"}
	configRaw, err := json.Marshal(config)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
INSERT INTO security_audit_policy_versions(
    policy_key,version,name,status,priority,config,config_digest,
    validation_errors,change_reason,activated_at
) VALUES ('behavior_integration',1,$1,'active',$2,$3::jsonb,$4,'[]'::jsonb,$5,NOW())`,
		config.Name, config.Priority, configRaw, strings.Repeat("b", 64), "integration behavior policy")
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
UPDATE security_audit_signal_watermark
SET last_aggregated_at=$1,last_evaluated_at=$1,last_evaluated_window_id=0,last_error=''
WHERE id=1`, now.Add(-2*time.Minute))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
INSERT INTO usage_logs(
    user_id,api_key_id,group_id,model,requested_model,ip_address,
    input_tokens,output_tokens,actual_cost,duration_ms,created_at
) VALUES ($1,$2,$3,'gpt-internal','gpt-public','203.0.113.10',120,30,0.125,480,$4)`,
		userID, apiKeyID, groupID, now.Add(-90*time.Second))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
INSERT INTO ops_error_logs(
    user_id,api_key_id,group_id,model,requested_model,client_ip,
    is_business_limited,is_count_tokens,duration_ms,created_at
) VALUES ($1,$2,$3,'gpt-internal','gpt-public','203.0.113.11',TRUE,FALSE,920,$4)`,
		userID, apiKeyID, groupID, now.Add(-80*time.Second))
	require.NoError(t, err)

	aggregated, err := repo.AggregateClosedBehaviorSignalWindows(ctx, now)
	require.NoError(t, err)
	require.Equal(t, int64(3), aggregated, "one user, API key and group aggregate must be materialized")
	evaluated, err := repo.EvaluateBehaviorSignals(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), evaluated, "all three subject windows must advance the durable watermark")

	var requestCount, errorCount, distinctIPs, matchedRules int64
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT w.request_count,w.error_count,w.distinct_ip_count,
       COUNT(e.id) FILTER (WHERE e.matched)
FROM security_audit_signal_windows w
LEFT JOIN security_audit_signal_evaluations e ON e.anchor_window_id=w.id
WHERE w.subject_type='api_key' AND w.subject_id=$1
GROUP BY w.id`, apiKeyID).Scan(&requestCount, &errorCount, &distinctIPs, &matchedRules))
	require.Equal(t, int64(2), requestCount)
	require.Equal(t, int64(1), errorCount)
	require.Equal(t, int64(2), distinctIPs)
	require.Equal(t, int64(1), matchedRules)

	var decisionCount, evidenceCount, actionCount int
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM security_audit_decisions
WHERE source_type='behavior' AND api_key_id=$1`, apiKeyID).Scan(&decisionCount))
	require.Equal(t, 1, decisionCount)
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM security_audit_evidence e
JOIN security_audit_decisions d ON d.id=e.decision_pk
WHERE d.source_type='behavior' AND d.api_key_id=$1`, apiKeyID).Scan(&evidenceCount))
	require.Equal(t, 1, evidenceCount)
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM security_audit_actions a
JOIN security_audit_decisions d ON d.id=a.decision_pk
WHERE d.source_type='behavior' AND d.api_key_id=$1`, apiKeyID).Scan(&actionCount))
	require.Equal(t, 2, actionCount)

	aggregated, err = repo.AggregateClosedBehaviorSignalWindows(ctx, now)
	require.NoError(t, err)
	require.Zero(t, aggregated)
	evaluated, err = repo.EvaluateBehaviorSignals(ctx)
	require.NoError(t, err)
	require.Zero(t, evaluated)
}

func TestSecurityAuditShadowEvaluationUsesRealSchemaWithoutCreatingActions(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	repo := NewPostgreSQLRepository(db)
	ctx := context.Background()

	config := canonicalSecurityPolicy(defaultSecurityPolicyConfig())
	config.Mode = ModeOff
	config.Actions.High = []string{}
	config.Actions.Critical = []string{}
	configRaw, err := json.Marshal(config)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
INSERT INTO security_audit_policy_versions(
    policy_key,version,name,status,priority,config,config_digest,
    validation_errors,change_reason,validated_at,shadowed_at
) VALUES ('shadow_integration',1,$1,'shadow',$2,$3::jsonb,$4,'[]'::jsonb,$5,NOW(),NOW()-INTERVAL '1 minute')`,
		config.Name, config.Priority, configRaw, strings.Repeat("s", 64), "integration shadow policy")
	require.NoError(t, err)

	event, err := repo.RecordBlocking(ctx, integrationSnapshot("shadow-real"), 1, integrationResult(EventCritical), true)
	require.NoError(t, err)
	var decisionPK int64
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT id FROM security_audit_decisions
WHERE source_type='prompt_audit' AND source_event_id=$1`, event.ID).Scan(&decisionPK))
	var actionCountBefore int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_audit_actions`).Scan(&actionCountBefore))

	processed, err := repo.EvaluateShadowPolicies(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), processed)
	var total, changed, actionCountAfter int
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*),COUNT(*) FILTER (WHERE request_action_changed)
FROM security_audit_shadow_evaluations`).Scan(&total, &changed))
	require.Equal(t, 1, total)
	require.Equal(t, 1, changed)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_audit_actions`).Scan(&actionCountAfter))
	require.Equal(t, actionCountBefore, actionCountAfter, "shadow evaluation must never create enforcement actions")

	summary, err := repo.ListPolicyShadowEvaluations(ctx, "shadow_integration", 1, 24, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), summary.Total)
	require.Equal(t, int64(1), summary.LooserChanges)
	require.Len(t, summary.Items, 1)
	require.Equal(t, decisionPK, summary.Items[0].DecisionPK)

	processed, err = repo.EvaluateShadowPolicies(ctx)
	require.NoError(t, err)
	require.Zero(t, processed)
}

func TestSecurityAuditEndpointCircuitBreakerLifecycleUsesRealSchema(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	repo := NewPostgreSQLRepository(db)
	ctx := context.Background()
	endpoint := ActiveEndpoint{
		ID: "integration-guard", BaseURL: "https://guard.example.test/v1",
		NetworkScope: NetworkScopePublicHTTPS,
	}

	allowed, err := repo.BeginEndpointAttempt(ctx, endpoint, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	for index := 0; index < 5; index++ {
		require.NoError(t, repo.UpsertEndpointHealth(ctx, endpoint, ProbeResult{
			OK: false, Retryable: true, ErrorCode: "prompt_guard_timeout",
			LatencyMS: 2500 + index, HTTPStatus: 503, CheckedAt: time.Now().UTC(),
		}))
	}
	health, err := repo.ListEndpointHealth(ctx)
	require.NoError(t, err)
	require.Len(t, health, 1)
	require.Equal(t, "open", health[0].BreakerState)
	require.Equal(t, 5, health[0].ConsecutiveFailures)
	require.Equal(t, int64(5), health[0].TimeoutCount)
	require.Equal(t, int64(5), health[0].ServerErrorCount)

	allowed, err = repo.BeginEndpointAttempt(ctx, endpoint, time.Hour)
	require.NoError(t, err)
	require.False(t, allowed, "an open breaker must deny traffic before cooldown")
	_, err = db.ExecContext(ctx, `
UPDATE security_audit_endpoint_health
SET breaker_opened_at=NOW()-INTERVAL '2 minutes'
WHERE endpoint_id=$1`, endpoint.ID)
	require.NoError(t, err)
	allowed, err = repo.BeginEndpointAttempt(ctx, endpoint, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed, "exactly one half-open probe is admitted after cooldown")
	allowed, err = repo.BeginEndpointAttempt(ctx, endpoint, time.Minute)
	require.NoError(t, err)
	require.False(t, allowed, "a second concurrent half-open attempt must remain denied")

	require.NoError(t, repo.UpsertEndpointHealth(ctx, endpoint, ProbeResult{
		OK: true, Status: "healthy", LatencyMS: 120, HTTPStatus: 200, CheckedAt: time.Now().UTC(),
	}))
	health, err = repo.ListEndpointHealth(ctx)
	require.NoError(t, err)
	require.Equal(t, "closed", health[0].BreakerState)
	require.Zero(t, health[0].ConsecutiveFailures)
	require.Equal(t, int64(6), health[0].RequestCount)
	require.Equal(t, int64(1), health[0].SuccessCount)

	reset, err := repo.ResetEndpointBreaker(ctx, endpoint.ID)
	require.NoError(t, err)
	require.Equal(t, "closed", reset.BreakerState)
	require.Zero(t, reset.ConsecutiveFailures)
}
