package invocationarchive

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type passthroughEncryptor struct{}

func (passthroughEncryptor) Encrypt(plaintext string) (string, error) {
	return "sealed:" + plaintext, nil
}
func (passthroughEncryptor) Decrypt(ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "sealed:"), nil
}

func TestConfigResolvesMostSpecificScope(t *testing.T) {
	groupID := int64(22)
	config := DefaultConfig()
	config.DefaultMode = ModeRequestOnly
	config.Rules = []ScopeRule{
		{Scope: ScopeGroup, SubjectID: groupID, Mode: ModeFull},
		{Scope: ScopeUser, SubjectID: 11, Mode: ModeOff},
		{Scope: ScopeAPIKey, SubjectID: 33, Mode: ModeRequestOnly},
	}
	if got := config.Resolve(11, &groupID, 33); got != ModeRequestOnly {
		t.Fatalf("API key rule must take precedence, got %q", got)
	}
	if got := config.Resolve(11, &groupID, 34); got != ModeOff {
		t.Fatalf("user rule must take precedence over group, got %q", got)
	}
	if got := config.Resolve(12, &groupID, 34); got != ModeFull {
		t.Fatalf("group rule must take precedence over default, got %q", got)
	}
}

func TestConfigRejectsOversizedRuleSet(t *testing.T) {
	config := DefaultConfig()
	config.Rules = make([]ScopeRule, maxScopeRules+1)
	for index := range config.Rules {
		config.Rules[index] = ScopeRule{Scope: ScopeUser, SubjectID: int64(index + 1), Mode: ModeFull}
	}
	if err := validateConfig(config); err == nil {
		t.Fatal("oversized rule set must be rejected")
	}
}

func TestConfigPublicKeepsEmptyRulesAsArray(t *testing.T) {
	raw, err := json.Marshal(DefaultConfig().Public())
	if err != nil {
		t.Fatalf("marshal public config: %v", err)
	}
	if !strings.Contains(string(raw), `"rules":[]`) {
		t.Fatalf("empty rules must encode as an array, got %s", raw)
	}
}

func TestArchiveCandidateSnapshotsIdentityAtIngress(t *testing.T) {
	groupID := int64(22)
	apiKey := &service.APIKey{
		ID: 33, UserID: 11, GroupID: &groupID, Name: "original-key",
		User:  &service.User{ID: 11, Email: "alice@example.test", Username: "alice"},
		Group: &service.Group{ID: groupID, Name: "original-group"},
	}
	candidate := NewService(nil, nil, nil).newCandidate(nil, apiKey, DefaultConfig(), ModeFull, "http", 0)

	replacementGroupID := int64(99)
	apiKey.ID, apiKey.UserID, apiKey.GroupID, apiKey.Name = 44, 55, &replacementGroupID, "replacement-key"
	apiKey.User.Email, apiKey.User.Username, apiKey.Group.Name = "bob@example.test", "bob", "replacement-group"

	if got, ok := candidate.identity.userID.(int64); !ok || got != 11 {
		t.Fatalf("candidate user identity mutated: %#v", candidate.identity.userID)
	}
	if got, ok := candidate.identity.apiKeyID.(int64); !ok || got != 33 {
		t.Fatalf("candidate API key identity mutated: %#v", candidate.identity.apiKeyID)
	}
	if got, ok := candidate.identity.groupID.(int64); !ok || got != groupID {
		t.Fatalf("candidate group identity mutated: %#v", candidate.identity.groupID)
	}
	if candidate.identity.userLabel != "alice <alice@example.test>" || candidate.identity.apiKeyName != "original-key" || candidate.identity.groupName != "original-group" {
		t.Fatalf("candidate labels mutated: %#v", candidate.identity)
	}
}

func TestPayloadEnvelopeRoundTripAndBoundedCapture(t *testing.T) {
	capture := newBoundedCapture(4)
	if _, err := capture.Write([]byte("abcdef")); err != nil {
		t.Fatalf("capture write: %v", err)
	}
	payload, total, truncated := capture.snapshot()
	if string(payload) != "abcd" || total != 6 || !truncated {
		t.Fatalf("unexpected bounded capture payload=%q total=%d truncated=%t", payload, total, truncated)
	}
	ciphertext, _, err := protectPayload(passthroughEncryptor{}, []byte("hello"))
	if err != nil {
		t.Fatalf("protect payload: %v", err)
	}
	view, err := revealPayload(passthroughEncryptor{}, ciphertext, "application/json", "captured", 5, 5, false)
	if err != nil {
		t.Fatalf("reveal payload: %v", err)
	}
	if !view.Available || view.Encoding != "utf8" || view.Data != "hello" {
		t.Fatalf("unexpected revealed payload: %#v", view)
	}
}

func TestMediaTypeRetainsDeclaredCharsetForPayloadReview(t *testing.T) {
	if got := mediaType("Application/JSON; Charset=GB18030"); got != "application/json; charset=gb18030" {
		t.Fatalf("unexpected normalized content type: %q", got)
	}
}

func TestConfigAcceptsGatewaySizedArchiveCaptureLimit(t *testing.T) {
	config := DefaultConfig()
	config.MaxRequestBytes = maxCaptureBytes
	config.MaxResponseBytes = maxCaptureBytes
	if err := validateConfig(config); err != nil {
		t.Fatalf("gateway-sized archive limits must be accepted: %v", err)
	}
	config.MaxResponseBytes++
	if err := validateConfig(config); err == nil {
		t.Fatal("capture limit above the gateway-sized ceiling must be rejected")
	}
}

func TestWebSocketArchivePreservesFrameBoundariesAndToolOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	archive := NewService(nil, nil, passthroughEncryptor{})
	config := DefaultConfig()
	config.DefaultMode = ModeFull
	config.MaxRequestBytes = 1024
	config.MaxResponseBytes = 1024
	archive.snapshot.Store(&config)
	archive.accepting = true

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	apiKey := &service.APIKey{ID: 33, UserID: 11, Name: "archive-test"}
	request := []byte(`{"type":"response.create","input":[{"type":"function_call_output","call_id":"call_1","output":"tool result"}]}`)
	session := archive.BeginWebSocketSession(context, apiKey, request, "test-model")
	if session == nil {
		t.Fatal("expected enabled WebSocket archive session")
	}
	firstFrame := []byte(`{"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_1","name":"bash","arguments":"{}"}}`)
	secondFrame := []byte{0x00, 0xff, 0x01}
	session.CaptureClientFrame(1, "text", firstFrame)
	session.CaptureClientFrame(1, "binary", secondFrame)
	session.FinishTurn(1, nil, nil)

	select {
	case candidate := <-archive.queue:
		if got := string(candidate.request.bytes); got != string(request) {
			t.Fatalf("WebSocket request capture mismatch: %q", got)
		}
		if got := string(candidate.response.bytes); got != string(append(append([]byte(nil), firstFrame...), secondFrame...)) {
			t.Fatalf("WebSocket response capture mismatch: %q", got)
		}
		if len(candidate.response.frames) != 2 || candidate.response.frames[0].offset != 0 || candidate.response.frames[1].offset != int64(len(firstFrame)) {
			t.Fatalf("WebSocket frame boundaries not retained: %#v", candidate.response.frames)
		}
		ciphertext, status, err := protectCapturedPayload(passthroughEncryptor{}, candidate.response)
		if err != nil || status != "captured" {
			t.Fatalf("protect WebSocket response: status=%s err=%v", status, err)
		}
		view, err := revealPayload(passthroughEncryptor{}, ciphertext, "application/json", status, candidate.response.total, int64(len(candidate.response.bytes)), candidate.response.truncated)
		if err != nil {
			t.Fatalf("reveal WebSocket response: %v", err)
		}
		if len(view.Frames) != 2 || view.Frames[0].Data != string(firstFrame) || view.Frames[1].Encoding != "base64" || view.Frames[1].CapturedBytes != int64(len(secondFrame)) {
			t.Fatalf("unexpected revealed WebSocket frames: %#v", view.Frames)
		}
	default:
		t.Fatal("WebSocket archive did not enqueue candidate")
	}
}

func TestWebSocketFrameMetadataLimitKeepsRawPayload(t *testing.T) {
	capture := newWebSocketFrameCapture(maxWebSocketFrameMetadata + 1)
	for range maxWebSocketFrameMetadata + 1 {
		capture.Write("text", []byte("x"))
	}
	payload, total, truncated, frames, framesTruncated := capture.snapshot()
	if string(payload) != strings.Repeat("x", maxWebSocketFrameMetadata+1) || total != int64(maxWebSocketFrameMetadata+1) || truncated {
		t.Fatalf("raw frame payload must remain complete: payload=%d total=%d truncated=%t", len(payload), total, truncated)
	}
	if len(frames) != maxWebSocketFrameMetadata || !framesTruncated {
		t.Fatalf("expected bounded frame metadata with intact raw payload: frames=%d truncated=%t", len(frames), framesTruncated)
	}
}

func TestNilServiceRuntimeIsSafe(t *testing.T) {
	var archive *Service
	if runtime := archive.Runtime(); runtime.Started || runtime.QueueCapacity != archiveQueueCapacity {
		t.Fatalf("unexpected nil runtime: %#v", runtime)
	}
}

func TestRevealHandlerAcceptsAnEmptyPOSTBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/records/:id/reveal", NewAdminHandler(nil).RevealRecord)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/records/1/reveal", nil)
	router.ServeHTTP(recorder, request)

	requireStatus(t, recorder.Code, http.StatusForbidden)
	if strings.Contains(recorder.Body.String(), "invocation_archive_invalid_reveal_request") {
		t.Fatalf("empty reveal body must reach authorization instead of JSON binding: %s", recorder.Body.String())
	}
}

func TestGatewayMiddlewareCapturesBoundedRequestAndResponseWithoutChangingHandlerIO(t *testing.T) {
	gin.SetMode(gin.TestMode)
	archive := NewService(nil, nil, nil)
	config := DefaultConfig()
	config.DefaultMode = ModeFull
	config.MaxRequestBytes = 1024
	config.MaxResponseBytes = 1024
	archive.snapshot.Store(&config)
	archive.accepting = true

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 33, UserID: 11, Name: "archive-test"})
		c.Next()
	})
	router.Use(archive.GatewayMiddleware())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusCreated, "application/json", append([]byte(`{"echo":`), append(body, '}')...))
	})

	requestBody := []byte(`{"model":"test","input":"hello"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	requireStatus(t, recorder.Code, http.StatusCreated)

	select {
	case candidate := <-archive.queue:
		if got := string(candidate.request.bytes); got != string(requestBody) {
			t.Fatalf("request capture mismatch: %q", got)
		}
		if got := string(candidate.response.bytes); got != `{"echo":{"model":"test","input":"hello"}}` {
			t.Fatalf("response capture mismatch: %q", got)
		}
		if candidate.outcome != "completed" || candidate.httpStatus != http.StatusCreated {
			t.Fatalf("unexpected candidate result: %#v", candidate)
		}
	default:
		t.Fatal("gateway response did not enqueue archive candidate")
	}
}

func requireStatus(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("unexpected status: got %d want %d", got, want)
	}
}
