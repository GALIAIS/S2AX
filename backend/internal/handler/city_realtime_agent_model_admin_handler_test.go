package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newCityRealtimeAgentModelAdminHandlerTestContext(t *testing.T, method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 41})
	return c, recorder
}

func TestCityRealtimeAgentModelProfileCreateRejectsUnknownSensitiveFields(t *testing.T) {
	h := NewCityEconomyHandler(nil)
	c, recorder := newCityRealtimeAgentModelAdminHandlerTestContext(t,
		http.MethodPost,
		"/api/v1/admin/city/agent-model-profiles",
		`{"code":"safe-profile","api_key":"must-not-be-accepted"}`,
	)

	h.CreateRealtimeAgentModelProfile(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "unknown field")
}

func TestCityRealtimeAgentModelProfileHeadRejectsUnknownRouteFields(t *testing.T) {
	h := NewCityEconomyHandler(nil)
	c, recorder := newCityRealtimeAgentModelAdminHandlerTestContext(t,
		http.MethodPatch,
		"/api/v1/admin/city/agent-model-profiles/safe-profile/head",
		`{"version":1,"status":"active","provider_url":"https://must-not-be-accepted.invalid"}`,
	)
	c.Params = gin.Params{{Key: "profile_code", Value: "safe-profile"}}

	h.UpdateRealtimeAgentModelProfileHead(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "unknown field")
}

func TestCityRealtimeAgentModelWorldBindingRejectsUnknownAccountFields(t *testing.T) {
	h := NewCityEconomyHandler(nil)
	c, recorder := newCityRealtimeAgentModelAdminHandlerTestContext(t,
		http.MethodPost,
		"/api/v1/admin/city/worlds/7/agent-model-bindings",
		`{"agent_definition_code":"character.npc","profile_code":"safe-profile","account_id":99}`,
	)
	c.Params = gin.Params{{Key: "world_id", Value: "7"}}

	h.BindRealtimeAgentModelProfileToWorld(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "unknown field")
}

func TestCityRealtimeAgentDecisionQueueRejectsUnboundedQuery(t *testing.T) {
	h := NewCityEconomyHandler(nil)
	c, recorder := newCityRealtimeAgentModelAdminHandlerTestContext(t,
		http.MethodGet,
		"/api/v1/admin/city/agent-decision-queue?world_id=0",
		"",
	)

	h.ListRealtimeAgentDecisionQueue(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestCityRealtimeAgentDecisionQueueRejectsOversizedCursor(t *testing.T) {
	h := NewCityEconomyHandler(nil)
	c, recorder := newCityRealtimeAgentModelAdminHandlerTestContext(t,
		http.MethodGet,
		"/api/v1/admin/city/agent-decision-queue?world_id=7&before_cursor="+strings.Repeat("a", 161),
		"",
	)

	h.ListRealtimeAgentDecisionQueue(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestCityRealtimeAgentDecisionDeadLetterEventsRejectInvalidCursor(t *testing.T) {
	h := NewCityEconomyHandler(nil)
	c, recorder := newCityRealtimeAgentModelAdminHandlerTestContext(t,
		http.MethodGet,
		"/api/v1/admin/city/worlds/7/agent-decision-queue/adr.queue.one/dead-letter/events?before_event_id=-1",
		"",
	)
	c.Params = gin.Params{{Key: "world_id", Value: "7"}, {Key: "request_code", Value: "adr.queue.one"}}

	h.ListRealtimeAgentDecisionDeadLetterEvents(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestCityRealtimeAgentDecisionRetryRejectsEmptyRequestCode(t *testing.T) {
	h := NewCityEconomyHandler(nil)
	c, recorder := newCityRealtimeAgentModelAdminHandlerTestContext(t,
		http.MethodPost,
		"/api/v1/admin/city/worlds/7/agent-decision-queue//retry",
		"",
	)
	c.Params = gin.Params{{Key: "world_id", Value: "7"}, {Key: "request_code", Value: ""}}

	h.RetryRealtimeAgentDecisionNow(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestCityRealtimeAgentDecisionDeadLetterRejectsUnknownFields(t *testing.T) {
	h := NewCityEconomyHandler(nil)
	c, recorder := newCityRealtimeAgentModelAdminHandlerTestContext(t,
		http.MethodPost,
		"/api/v1/admin/city/worlds/7/agent-decision-queue/adr.queue.one/dead-letter",
		`{"reason_code":"operator_review","provider_url":"https://must-not-be-accepted.invalid"}`,
	)
	c.Params = gin.Params{{Key: "world_id", Value: "7"}, {Key: "request_code", Value: "adr.queue.one"}}

	h.QuarantineRealtimeAgentDecision(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "unknown field")
}

func TestCityRealtimeAgentDecisionDeadLetterReleaseRejectsEmptyRequestCode(t *testing.T) {
	h := NewCityEconomyHandler(nil)
	c, recorder := newCityRealtimeAgentModelAdminHandlerTestContext(t,
		http.MethodPost,
		"/api/v1/admin/city/worlds/7/agent-decision-queue//dead-letter/release",
		"",
	)
	c.Params = gin.Params{{Key: "world_id", Value: "7"}, {Key: "request_code", Value: ""}}

	h.ReleaseRealtimeAgentDecisionDeadLetter(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
