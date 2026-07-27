package invocationarchive

import (
	"bytes"
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

func TestNilServiceRuntimeIsSafe(t *testing.T) {
	var archive *Service
	if runtime := archive.Runtime(); runtime.Started || runtime.QueueCapacity != archiveQueueCapacity {
		t.Fatalf("unexpected nil runtime: %#v", runtime)
	}
}

func TestRevealRequiresAnAccountableReasonBeforeStorageAccess(t *testing.T) {
	archive := NewService(nil, nil, nil)
	_, err := archive.RevealRecord(t.Context(), 1, 1, "", "", "")
	if err != ErrInvalidRevealReason {
		t.Fatalf("expected reveal reason rejection, got %v", err)
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
