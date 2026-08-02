package securityaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fakePromptAdminService struct {
	config       PublicConfig
	save         func(context.Context, UpdateConfigRequest, int64) (PublicConfig, error)
	probe        func(context.Context, ProbeRequest) ProbeResult
	runtime      RuntimeSnapshot
	listJobs     func(context.Context, JobFilter, int, int) (*JobPage, error)
	retryJob     func(context.Context, int64, int64, string) (*Job, error)
	quarantine   func(context.Context, int64, int64, string) (*Job, error)
	discard      func(context.Context, int64, int64, string) (*Job, error)
	list         func(context.Context, EventFilter, int, int) (*EventPage, error)
	get          func(context.Context, int64) (*Event, error)
	reveal       func(context.Context, int64, int64, string) (*EvidenceReveal, error)
	deleteOne    func(context.Context, int64) (*DeleteResult, error)
	deleteIDs    func(context.Context, []int64) (*DeleteResult, error)
	preview      func(context.Context, EventFilter, int64) (*DeletePreview, error)
	deleteFilter func(context.Context, DeleteByFilterRequest, int64) (*DeleteResult, error)
}

func (s *fakePromptAdminService) GetConfig() (PublicConfig, error) {
	return s.config, nil
}
func (s *fakePromptAdminService) SaveConfig(ctx context.Context, req UpdateConfigRequest, actorID int64) (PublicConfig, error) {
	if s.save == nil {
		return PublicConfig{}, errors.New("unexpected SaveConfig call")
	}
	return s.save(ctx, req, actorID)
}
func (s *fakePromptAdminService) Probe(ctx context.Context, req ProbeRequest) ProbeResult {
	if s.probe == nil {
		return ProbeResult{}
	}
	return s.probe(ctx, req)
}
func (s *fakePromptAdminService) Runtime(context.Context) RuntimeSnapshot { return s.runtime }
func (s *fakePromptAdminService) ListJobs(ctx context.Context, filter JobFilter, page, pageSize int) (*JobPage, error) {
	if s.listJobs == nil {
		return &JobPage{}, nil
	}
	return s.listJobs(ctx, filter, page, pageSize)
}
func (s *fakePromptAdminService) RetryJob(ctx context.Context, id, actorID int64, reason string) (*Job, error) {
	if s.retryJob == nil {
		return nil, ErrJobNotFound
	}
	return s.retryJob(ctx, id, actorID, reason)
}
func (s *fakePromptAdminService) QuarantineJob(ctx context.Context, id, actorID int64, reason string) (*Job, error) {
	if s.quarantine == nil {
		return nil, ErrJobNotFound
	}
	return s.quarantine(ctx, id, actorID, reason)
}
func (s *fakePromptAdminService) DiscardJob(ctx context.Context, id, actorID int64, reason string) (*Job, error) {
	if s.discard == nil {
		return nil, ErrJobNotFound
	}
	return s.discard(ctx, id, actorID, reason)
}
func (s *fakePromptAdminService) ListEvents(ctx context.Context, filter EventFilter, page, pageSize int) (*EventPage, error) {
	if s.list == nil {
		return &EventPage{}, nil
	}
	return s.list(ctx, filter, page, pageSize)
}
func (s *fakePromptAdminService) GetEvent(ctx context.Context, id int64) (*Event, error) {
	if s.get == nil {
		return nil, ErrEventNotFound
	}
	return s.get(ctx, id)
}
func (s *fakePromptAdminService) RevealEventEvidence(ctx context.Context, eventID, actorID int64, reason string) (*EvidenceReveal, error) {
	if s.reveal == nil {
		return nil, ErrEvidenceUnavailable
	}
	return s.reveal(ctx, eventID, actorID, reason)
}
func (s *fakePromptAdminService) DeleteEvent(ctx context.Context, id int64) (*DeleteResult, error) {
	if s.deleteOne == nil {
		return &DeleteResult{}, nil
	}
	return s.deleteOne(ctx, id)
}
func (s *fakePromptAdminService) DeleteEventsByIDs(ctx context.Context, ids []int64) (*DeleteResult, error) {
	if s.deleteIDs == nil {
		return &DeleteResult{}, nil
	}
	return s.deleteIDs(ctx, ids)
}
func (s *fakePromptAdminService) PreviewDelete(ctx context.Context, filter EventFilter, actorID int64) (*DeletePreview, error) {
	if s.preview == nil {
		return &DeletePreview{}, nil
	}
	return s.preview(ctx, filter, actorID)
}
func (s *fakePromptAdminService) DeleteByFilter(ctx context.Context, req DeleteByFilterRequest, actorID int64) (*DeleteResult, error) {
	if s.deleteFilter == nil {
		return &DeleteResult{}, nil
	}
	return s.deleteFilter(ctx, req, actorID)
}

func promptAdminRouter(service PromptAdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 42})
		c.Set(string(servermiddleware.ContextKeyUserRole), "admin")
		c.Next()
	})
	handler := NewPromptAdminHandler(service)
	group := router.Group("/admin/prompt-audit")
	group.GET("/config", handler.GetConfig)
	group.PUT("/config", handler.UpdateConfig)
	group.POST("/endpoints/probe", handler.ProbeEndpoint)
	group.GET("/runtime", handler.GetRuntime)
	group.GET("/jobs", handler.ListJobs)
	group.POST("/jobs/:id/retry", handler.RetryJob)
	group.POST("/jobs/:id/quarantine", handler.QuarantineJob)
	group.POST("/jobs/:id/discard", handler.DiscardJob)
	group.GET("/events", handler.ListEvents)
	group.GET("/events/:id", handler.GetEvent)
	group.POST("/events/:id/evidence/reveal", handler.RevealEventEvidence)
	group.DELETE("/events/:id", handler.DeleteEvent)
	group.POST("/events/batch-delete", handler.BatchDelete)
	group.POST("/events/delete-preview", handler.DeletePreview)
	group.POST("/events/delete-by-filter", handler.DeleteByFilter)
	return router
}

func promptAdminRequest(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestPromptAdminConfigRequiresVersionMapsConflictAndNeverEchoesToken(t *testing.T) {
	const canary = "prompt-admin-token-canary"

	t.Run("missing expected version", func(t *testing.T) {
		router := promptAdminRouter(&fakePromptAdminService{})
		response := promptAdminRequest(t, router, http.MethodPut, "/admin/prompt-audit/config", map[string]any{})
		require.Equal(t, http.StatusBadRequest, response.Code)
		require.Contains(t, response.Body.String(), "prompt_audit_invalid_config_request")
	})

	t.Run("CAS conflict", func(t *testing.T) {
		service := &fakePromptAdminService{save: func(context.Context, UpdateConfigRequest, int64) (PublicConfig, error) {
			return PublicConfig{}, infraerrors.Conflict(ErrorCodeConfigConflict, "配置已被更新")
		}}
		response := promptAdminRequest(t, promptAdminRouter(service), http.MethodPut, "/admin/prompt-audit/config", validHandlerUpdateRequest(canary))
		require.Equal(t, http.StatusConflict, response.Code)
		require.Contains(t, response.Body.String(), ErrorCodeConfigConflict)
		require.NotContains(t, response.Body.String(), canary)
	})

	t.Run("success public DTO", func(t *testing.T) {
		service := &fakePromptAdminService{save: func(_ context.Context, req UpdateConfigRequest, actorID int64) (PublicConfig, error) {
			require.Equal(t, int64(42), actorID)
			require.Equal(t, canary, req.Endpoints[0].Token)
			return PublicConfig{ConfigVersion: 8, Endpoints: []PublicEndpoint{{ID: "guard-1", HasToken: true, TokenStatus: "configured"}}}, nil
		}}
		response := promptAdminRequest(t, promptAdminRouter(service), http.MethodPut, "/admin/prompt-audit/config", validHandlerUpdateRequest(canary))
		require.Equal(t, http.StatusOK, response.Code)
		body := response.Body.String()
		require.NotContains(t, body, canary)
		require.NotContains(t, body, "token_ciphertext")
		require.NotContains(t, body, `"token":`)
		require.Contains(t, body, `"has_token":true`)
	})
}

func TestPromptAdminGetConfigReturnsSecretFreeUnavailableError(t *testing.T) {
	const canary = "persisted-config-secret-canary"
	repository := &switchableSettingRepository{loadErr: errors.New("failed to load token " + canary)}
	manager := NewConfigManager(nil, repository, nil, prefixEncryptor{}, testTotpKeyConfig())
	require.Error(t, manager.Reload(context.Background()))
	service := &PromptService{config: manager}

	response := promptAdminRequest(t, promptAdminRouter(service), http.MethodGet, "/admin/prompt-audit/config", nil)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), ErrorCodeConfigUnavailable)
	require.NotContains(t, response.Body.String(), canary)
	require.NotContains(t, response.Body.String(), `"config_version"`)
	require.NotContains(t, response.Body.String(), `"token"`)
}

func TestPromptAdminProbeSupportsTemporaryOrSavedTokenWithoutEcho(t *testing.T) {
	const canary = "probe-token-canary"
	for _, tc := range []struct {
		name         string
		token        string
		tokenApplied bool
	}{
		{name: "temporary token", token: canary, tokenApplied: true},
		{name: "saved token", token: "", tokenApplied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &fakePromptAdminService{probe: func(_ context.Context, req ProbeRequest) ProbeResult {
				require.Equal(t, tc.token, req.Endpoint.Token)
				return ProbeResult{OK: true, Status: "healthy", Message: "ok", TokenApplied: tc.tokenApplied}
			}}
			endpoint := validHandlerUpdateRequest(tc.token).Endpoints[0]
			response := promptAdminRequest(t, promptAdminRouter(service), http.MethodPost, "/admin/prompt-audit/endpoints/probe", ProbeRequest{Endpoint: endpoint})
			require.Equal(t, http.StatusOK, response.Code)
			require.NotContains(t, response.Body.String(), canary)
			require.NotContains(t, response.Body.String(), `"token":`)
			require.Contains(t, response.Body.String(), `"token_applied":true`)
		})
	}
}

func TestPromptAdminRejectsInvalidEventIDsTimesAndPagination(t *testing.T) {
	router := promptAdminRouter(&fakePromptAdminService{})
	for _, tc := range []struct {
		method string
		path   string
		body   any
		reason string
	}{
		{http.MethodGet, "/admin/prompt-audit/events/not-a-number", nil, "prompt_audit_invalid_event_id"},
		{http.MethodDelete, "/admin/prompt-audit/events/-1", nil, "prompt_audit_invalid_event_id"},
		{http.MethodGet, "/admin/prompt-audit/events?group_id=bad", nil, "prompt_audit_invalid_filter_id"},
		{http.MethodGet, "/admin/prompt-audit/events?start_at=not-time", nil, "prompt_audit_invalid_time"},
		{http.MethodGet, "/admin/prompt-audit/events?page=0", nil, "prompt_audit_invalid_pagination"},
		{http.MethodPost, "/admin/prompt-audit/events/batch-delete", map[string]any{"ids": []int64{1, -2}}, "prompt_audit_invalid_event_id"},
	} {
		response := promptAdminRequest(t, router, tc.method, tc.path, tc.body)
		require.Equalf(t, http.StatusBadRequest, response.Code, "%s %s", tc.method, tc.path)
		require.Contains(t, response.Body.String(), tc.reason)
	}
}

func TestPromptAdminJobOperationsRequireReasonMapErrorsAndBindActor(t *testing.T) {
	t.Run("list validation", func(t *testing.T) {
		router := promptAdminRouter(&fakePromptAdminService{})
		response := promptAdminRequest(t, router, http.MethodGet, "/admin/prompt-audit/jobs?status=invalid", nil)
		require.Equal(t, http.StatusBadRequest, response.Code)
		require.Contains(t, response.Body.String(), "prompt_audit_invalid_job_status")
	})

	t.Run("reason required", func(t *testing.T) {
		router := promptAdminRouter(&fakePromptAdminService{})
		response := promptAdminRequest(t, router, http.MethodPost, "/admin/prompt-audit/jobs/8/retry", map[string]any{"reason": "x"})
		require.Equal(t, http.StatusBadRequest, response.Code)
		require.Contains(t, response.Body.String(), "prompt_audit_job_reason_invalid")
	})

	t.Run("payload expired", func(t *testing.T) {
		service := &fakePromptAdminService{retryJob: func(context.Context, int64, int64, string) (*Job, error) {
			return nil, ErrJobPayloadUnavailable
		}}
		response := promptAdminRequest(t, promptAdminRouter(service), http.MethodPost, "/admin/prompt-audit/jobs/8/retry", map[string]any{"reason": "operator retry"})
		require.Equal(t, http.StatusConflict, response.Code)
		require.Contains(t, response.Body.String(), "prompt_audit_job_payload_unavailable")
	})

	t.Run("quarantine success", func(t *testing.T) {
		service := &fakePromptAdminService{quarantine: func(_ context.Context, id, actorID int64, reason string) (*Job, error) {
			require.Equal(t, int64(8), id)
			require.Equal(t, int64(42), actorID)
			require.Equal(t, "incident review", reason)
			return &Job{ID: id, Status: "quarantined"}, nil
		}}
		response := promptAdminRequest(t, promptAdminRouter(service), http.MethodPost, "/admin/prompt-audit/jobs/8/quarantine", map[string]any{"reason": "incident review"})
		require.Equal(t, http.StatusOK, response.Code)
		require.Contains(t, response.Body.String(), `"status":"quarantined"`)
	})
}

func validHandlerUpdateRequest(token string) UpdateConfigRequest {
	return UpdateConfigRequest{
		ExpectedConfigVersion: 7,
		Strategy:              "priority",
		WorkerCount:           1,
		QueueCapacity:         10,
		Scanners:              []string{"pii"},
		AllGroups:             true,
		Endpoints: []UpdateEndpoint{{
			ID: "guard-1", Name: "Guard One", Protocol: "openai_compatible",
			BaseURL: "http://127.0.0.1:18080", Model: DefaultGuardModel, Token: token,
			TimeoutMS: 1000, InputLimit: 1024, Enabled: true,
		}},
	}
}

func TestPromptAdminDeleteConfirmationErrorsStayGeneric(t *testing.T) {
	service := &fakePromptAdminService{deleteFilter: func(context.Context, DeleteByFilterRequest, int64) (*DeleteResult, error) {
		return nil, errors.New("sensitive-token-or-filter-detail")
	}}
	response := promptAdminRequest(t, promptAdminRouter(service), http.MethodPost, "/admin/prompt-audit/events/delete-by-filter", DeleteByFilterRequest{
		SnapshotMaxID: 3, FilterHash: strings.Repeat("a", 64), ConfirmationToken: "secret-confirmation", Confirm: true,
	})
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "prompt_audit_delete_confirmation_invalid")
	require.NotContains(t, response.Body.String(), "sensitive-token")
	require.NotContains(t, response.Body.String(), "secret-confirmation")
}

func TestPromptAdminEvidenceRevealRequiresReasonAndNeverCachesPlaintext(t *testing.T) {
	const canary = "PROMPT_EVIDENCE_CANARY"
	service := &fakePromptAdminService{reveal: func(_ context.Context, eventID, actorID int64, reason string) (*EvidenceReveal, error) {
		require.Equal(t, int64(9), eventID)
		require.Equal(t, int64(42), actorID)
		require.Equal(t, "incident review", reason)
		return &EvidenceReveal{EventID: eventID, FullPrompt: canary, RevealedAt: time.Now().UTC()}, nil
	}}
	router := promptAdminRouter(service)

	invalid := promptAdminRequest(t, router, http.MethodPost, "/admin/prompt-audit/events/9/evidence/reveal", map[string]any{"reason": "x"})
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	require.NotContains(t, invalid.Body.String(), canary)

	success := promptAdminRequest(t, router, http.MethodPost, "/admin/prompt-audit/events/9/evidence/reveal", map[string]any{"reason": "incident review"})
	require.Equal(t, http.StatusOK, success.Code)
	require.Equal(t, "no-store", success.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", success.Header().Get("Pragma"))
	require.Contains(t, success.Body.String(), canary)
}
