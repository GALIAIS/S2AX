//go:build integration

package invocationarchive

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestInvocationArchiveRealPostgresHTTPFlow exercises the actual PostgreSQL
// migration, AES-GCM encryptor, async workers, capture middleware and HTTP
// handlers together. It deliberately uses a real TCP server rather than an
// in-memory recorder so the response writer behavior is covered too.
func TestInvocationArchiveRealPostgresHTTPFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	db := openArchiveIntegrationDB(t, ctx)
	applyArchiveIntegrationSchema(t, ctx, db)

	driver := entsql.OpenDB(dialect.Postgres, db)
	entClient := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = entClient.Close() })
	settings := repository.NewSettingRepository(entClient)
	encryptor, err := repository.NewAESEncryptor(&config.Config{Totp: config.TotpConfig{
		EncryptionKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}})
	require.NoError(t, err)

	const (
		adminID        int64 = 101
		secondUserID   int64 = 102
		groupID        int64 = 202
		apiKeyID       int64 = 303
		secondAPIKeyID int64 = 304
	)
	_, err = db.ExecContext(ctx, `INSERT INTO users(id,email,username) VALUES ($1,$2,$3)`, adminID, "archive-admin@example.test", "archive-admin")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO users(id,email,username) VALUES ($1,$2,$3)`, secondUserID, "archive-second@example.test", "archive-second")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO groups(id,name) VALUES ($1,$2)`, groupID, "archive-test-group")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO api_keys(id,user_id,group_id,name) VALUES ($1,$2,$3,$4)`, apiKeyID, adminID, groupID, "archive-test-key")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO api_keys(id,user_id,group_id,name) VALUES ($1,$2,$3,$4)`, secondAPIKeyID, secondUserID, groupID, "archive-second-key")
	require.NoError(t, err)

	archive := NewService(db, settings, encryptor)
	require.NoError(t, archive.Start(ctx))
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		require.NoError(t, archive.Shutdown(shutdownCtx))
	})

	fullConfig, err := archive.SaveConfig(ctx, UpdateConfigRequest{
		ExpectedConfigVersion: archive.GetConfig().ConfigVersion,
		DefaultMode:           ModeFull,
		RetentionDays:         7,
		MaxRequestBytes:       1024,
		MaxResponseBytes:      1024,
		DirectViewEnabled:     true,
		Rules:                 []ScopeRule{},
	}, adminID)
	require.NoError(t, err)
	require.Equal(t, int64(2), fullConfig.ConfigVersion)

	keys := map[string]*service.APIKey{
		"alpha": {
			ID: apiKeyID, UserID: adminID, GroupID: ptr(groupID), Name: "archive-test-key",
			User:  &service.User{ID: adminID, Email: "archive-admin@example.test", Username: "archive-admin"},
			Group: &service.Group{ID: groupID, Name: "archive-test-group"},
		},
		"beta": {
			ID: secondAPIKeyID, UserID: secondUserID, GroupID: ptr(groupID), Name: "archive-second-key",
			User:  &service.User{ID: secondUserID, Email: "archive-second@example.test", Username: "archive-second"},
			Group: &service.Group{ID: groupID, Name: "archive-test-group"},
		},
	}
	gateway := gin.New()
	gateway.Use(func(c *gin.Context) {
		key, ok := keys[c.GetHeader("X-Archive-Test-Key")]
		if !ok {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(middleware.ContextKeyAPIKey), key)
		c.Next()
	})
	gateway.Use(archive.GatewayMiddleware())
	gateway.POST("/v1/chat/completions", func(c *gin.Context) {
		payload, readErr := io.ReadAll(c.Request.Body)
		if readErr != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"echo": string(payload)})
	})
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)

	const (
		alphaPayload = `{"model":"archive-alpha-model","message":"archive-alpha-secret"}`
		betaPayload  = `{"model":"archive-beta-model","message":"archive-beta-secret"}`
	)
	requestErrors := make(chan error, 2)
	for _, invocation := range []struct{ key, payload string }{{"alpha", alphaPayload}, {"beta", betaPayload}} {
		invocation := invocation
		go func() {
			requestErrors <- postArchiveGateway(ctx, gatewayServer.URL, invocation.key, invocation.payload)
		}()
	}
	for range 2 {
		require.NoError(t, <-requestErrors)
	}
	fullRecords := waitForArchiveRecords(t, ctx, archive, 2)
	alphaRecord := archiveRecordByModel(t, fullRecords, "archive-alpha-model")
	betaRecord := archiveRecordByModel(t, fullRecords, "archive-beta-model")
	for _, expected := range []struct {
		record   Record
		userID   int64
		apiKeyID int64
		secret   string
	}{
		{alphaRecord, adminID, apiKeyID, "archive-alpha-secret"},
		{betaRecord, secondUserID, secondAPIKeyID, "archive-beta-secret"},
	} {
		require.Equal(t, ModeFull, expected.record.Mode)
		require.Equal(t, "captured", expected.record.RequestStatus)
		require.Equal(t, "captured", expected.record.ResponseStatus)
		require.Equal(t, http.StatusCreated, expected.record.HTTPStatus)
		require.NotNil(t, expected.record.UserID)
		require.NotNil(t, expected.record.APIKeyID)
		require.Equal(t, expected.userID, *expected.record.UserID)
		require.Equal(t, expected.apiKeyID, *expected.record.APIKeyID)
		revealed, revealErr := archive.RevealRecord(ctx, expected.record.ID, adminID, "127.0.0.1", "archive-integration-test")
		require.NoError(t, revealErr)
		require.True(t, revealed.Request.Available)
		require.True(t, revealed.Response.Available)
		require.Contains(t, revealed.Request.Data, expected.secret)
		require.Contains(t, revealed.Response.Data, expected.secret)
	}

	var requestCiphertext, responseCiphertext string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT request_ciphertext,response_ciphertext
		FROM invocation_archive_records WHERE id=$1`, alphaRecord.ID).Scan(&requestCiphertext, &responseCiphertext))
	require.NotEmpty(t, requestCiphertext)
	require.NotEmpty(t, responseCiphertext)
	require.NotContains(t, requestCiphertext, "archive-alpha-secret")
	require.NotContains(t, responseCiphertext, "archive-alpha-secret")

	accesses, err := archive.ListAccessLogs(ctx, alphaRecord.ID, 10)
	require.NoError(t, err)
	require.Len(t, accesses, 1)
	require.Empty(t, accesses[0].Reason)
	require.Equal(t, "revealed", accesses[0].Outcome)

	_, err = db.ExecContext(ctx, `UPDATE invocation_archive_config_versions SET config_digest='0' WHERE config_version=$1`, fullConfig.ConfigVersion)
	require.Error(t, err, "configuration history must remain append-only")

	require.EqualValues(t, 1, mustDeleteArchiveRecord(t, ctx, archive, alphaRecord.ID))
	assertArchivedAccessEndpointSurvivesPayloadDeletion(t, ctx, archive, alphaRecord.ID)

	requestOnlyConfig, err := archive.SaveConfig(ctx, UpdateConfigRequest{
		ExpectedConfigVersion: fullConfig.ConfigVersion,
		DefaultMode:           ModeRequestOnly,
		RetentionDays:         fullConfig.RetentionDays,
		MaxRequestBytes:       fullConfig.MaxRequestBytes,
		MaxResponseBytes:      fullConfig.MaxResponseBytes,
		DirectViewEnabled:     true,
		Rules:                 []ScopeRule{},
	}, adminID)
	require.NoError(t, err)
	require.Equal(t, int64(3), requestOnlyConfig.ConfigVersion)

	const requestOnlyPayload = `{"model":"archive-request-only","message":"request-only-secret"}`
	invokeArchiveGateway(t, ctx, gatewayServer.URL, "alpha", requestOnlyPayload)
	requestOnlyRecord := archiveRecordByModel(t, waitForArchiveRecords(t, ctx, archive, 2), "archive-request-only")
	require.Equal(t, ModeRequestOnly, requestOnlyRecord.Mode)
	require.Equal(t, "captured", requestOnlyRecord.RequestStatus)
	require.Equal(t, "omitted", requestOnlyRecord.ResponseStatus)
	revealed, err := archive.RevealRecord(ctx, requestOnlyRecord.ID, adminID, "127.0.0.1", "archive-integration-test")
	require.NoError(t, err)
	require.True(t, revealed.Request.Available)
	require.False(t, revealed.Response.Available)
}

func openArchiveIntegrationDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	container, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("invocation_archive_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	deadline := time.Now().Add(20 * time.Second)
	for {
		err = db.PingContext(ctx)
		if err == nil {
			return db
		}
		if time.Now().After(deadline) {
			require.NoError(t, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func applyArchiveIntegrationSchema(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		CREATE TABLE users (id BIGINT PRIMARY KEY, email TEXT NOT NULL DEFAULT '', username TEXT NOT NULL DEFAULT '', deleted_at TIMESTAMPTZ NULL);
		CREATE TABLE groups (id BIGINT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', deleted_at TIMESTAMPTZ NULL);
		CREATE TABLE api_keys (id BIGINT PRIMARY KEY, user_id BIGINT NOT NULL, group_id BIGINT NULL, name TEXT NOT NULL DEFAULT '', deleted_at TIMESTAMPTZ NULL);
		CREATE TABLE settings (id BIGSERIAL PRIMARY KEY, key VARCHAR(255) NOT NULL UNIQUE, value TEXT NOT NULL DEFAULT '', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "303_invocation_archive.sql"))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
}

func invokeArchiveGateway(t *testing.T, ctx context.Context, baseURL, archiveKey, payload string) {
	t.Helper()
	require.NoError(t, postArchiveGateway(ctx, baseURL, archiveKey, payload))
}

func postArchiveGateway(ctx context.Context, baseURL, archiveKey, payload string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewBufferString(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Archive-Test-Key", archiveKey)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected archive gateway status: %d", response.StatusCode)
	}
	return nil
}

func waitForArchiveRecords(t *testing.T, ctx context.Context, archive *Service, expectedTotal int64) []Record {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		page, err := archive.ListRecords(ctx, RecordFilter{}, 1, 10)
		if err == nil && page.Total == expectedTotal && int64(len(page.Items)) == expectedTotal {
			return page.Items
		}
		if time.Now().After(deadline) {
			require.NoError(t, err)
			require.Failf(t, "archive record did not persist", "expected total=%d", expectedTotal)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func archiveRecordByModel(t *testing.T, records []Record, model string) Record {
	t.Helper()
	for _, record := range records {
		if record.Model == model {
			return record
		}
	}
	t.Fatalf("archive record for model %q not found: %#v", model, records)
	return Record{}
}

func mustDeleteArchiveRecord(t *testing.T, ctx context.Context, archive *Service, id int64) int64 {
	t.Helper()
	deleted, err := archive.DeleteRecord(ctx, id)
	require.NoError(t, err)
	return deleted
}

func assertArchivedAccessEndpointSurvivesPayloadDeletion(t *testing.T, ctx context.Context, archive *Service, recordID int64) {
	t.Helper()
	router := gin.New()
	router.GET("/records/:id/accesses", NewAdminHandler(archive).ListAccessLogs)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/records/"+strconv.FormatInt(recordID, 10)+"/accesses", nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, string(body), "revealed")
}

func ptr[T any](value T) *T { return &value }
