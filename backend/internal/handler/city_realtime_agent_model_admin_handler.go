package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// createCityRealtimeAgentModelProfileRequest is intentionally a constrained
// administrative configuration surface. It does not accept provider URLs,
// credentials, account identifiers, prompts, response bodies, or arbitrary
// provider options. The service derives the executable route reference from
// the selected safe provider kind and, for gateway profiles, a local group ID.
type createCityRealtimeAgentModelProfileRequest struct {
	Code                        string   `json:"code" binding:"required"`
	DisplayName                 string   `json:"display_name" binding:"required"`
	ProviderCode                string   `json:"provider_code" binding:"required"`
	PlatformGroupID             *int64   `json:"platform_group_id"`
	ModelIdentifier             string   `json:"model_identifier" binding:"required"`
	AllowedAgentDefinitionCodes []string `json:"allowed_agent_definition_codes" binding:"required"`
	Temperature                 float64  `json:"temperature"`
	MaxInputTokens              int      `json:"max_input_tokens"`
	MaxOutputTokens             int      `json:"max_output_tokens"`
	TimeoutMS                   int      `json:"timeout_ms"`
	MaxConcurrency              int      `json:"max_concurrency"`
	RetryLimit                  int      `json:"retry_limit"`
	MaxProfileHourlyRequests    int      `json:"max_profile_hourly_requests"`
	MaxProfileHourlyTokens      int64    `json:"max_profile_hourly_tokens"`
	MaxWorldHourlyRequests      int      `json:"max_world_hourly_requests"`
	MaxWorldHourlyTokens        int64    `json:"max_world_hourly_tokens"`
	MaxAgentHourlyRequests      int      `json:"max_agent_hourly_requests"`
	MaxAgentHourlyTokens        int64    `json:"max_agent_hourly_tokens"`
	MaxOwnerHourlyRequests      int      `json:"max_owner_hourly_requests"`
	MaxOwnerHourlyTokens        int64    `json:"max_owner_hourly_tokens"`
	CircuitBreakerFailures      int      `json:"circuit_breaker_failure_threshold"`
	CircuitBreakerCooldownSecs  int      `json:"circuit_breaker_cooldown_seconds"`
	PrivacyClass                string   `json:"privacy_class" binding:"required"`
	RetentionPolicy             string   `json:"retention_policy" binding:"required"`
	FallbackPolicy              string   `json:"fallback_policy" binding:"required"`
}

type updateCityRealtimeAgentModelProfileHeadRequest struct {
	Version int    `json:"version"`
	Status  string `json:"status" binding:"required"`
}

type bindCityRealtimeAgentModelProfileWorldRequest struct {
	AgentDefinitionCode string `json:"agent_definition_code" binding:"required"`
	ProfileCode         string `json:"profile_code" binding:"required"`
}

type retryCityRealtimeAgentDecisionRequest struct {
	WorldID     int64  `json:"world_id"`
	RequestCode string `json:"request_code"`
}

type quarantineCityRealtimeAgentDecisionRequest struct {
	WorldID     int64  `json:"world_id"`
	RequestCode string `json:"request_code"`
	ReasonCode  string `json:"reason_code" binding:"required"`
}

type releaseCityRealtimeAgentDecisionDeadLetterRequest struct {
	WorldID     int64  `json:"world_id"`
	RequestCode string `json:"request_code"`
}

// decodeStrictCityRealtimeAgentModelJSON makes the browser contract
// deliberately closed. In particular, an accidental or malicious api_key,
// provider_url, prompt, account_id, or arbitrary provider option must not be
// silently ignored by a configuration endpoint.
func decodeStrictCityRealtimeAgentModelJSON(c *gin.Context, target any, label string) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		response.BadRequest(c, "Invalid "+label+": "+err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		response.BadRequest(c, "Invalid "+label+": request body must contain exactly one JSON object")
		return false
	}
	return true
}

func (h *CityEconomyHandler) ListRealtimeAgentModelProfiles(c *gin.Context) {
	items, err := h.service.ListRealtimeAgentModelProfiles(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

// ListRealtimeAgentDecisionQueue is a bounded, administrator-only view of
// one realtime world's dispatch queue. It never changes retry state or emits a
// provider call; replay/remediation remains a separately audited operation.
func (h *CityEconomyHandler) ListRealtimeAgentDecisionQueue(c *gin.Context) {
	worldID, ok := parseCityQueryInt(c, "world_id", 0)
	if !ok || worldID <= 0 {
		if ok {
			response.BadRequest(c, "Invalid realtime agent decision queue query")
		}
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		if ok {
			response.BadRequest(c, "Invalid realtime agent decision queue query")
		}
		return
	}
	beforeCursor := strings.TrimSpace(c.Query("before_cursor"))
	if len(beforeCursor) > 160 {
		response.BadRequest(c, "Invalid realtime agent decision queue query")
		return
	}
	page, err := h.service.ListRealtimeAgentDecisionQueue(c.Request.Context(), service.CityRealtimeAgentDecisionQueueListInput{
		WorldID:      worldID,
		Status:       strings.TrimSpace(c.Query("status")),
		BeforeCursor: beforeCursor,
		Limit:        int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

// ListRealtimeAgentDecisionDeadLetterEvents returns the redacted operator
// receipt history for exactly one queued decision. The path does not expose a
// generic audit-log search and cannot reveal provider, observation or billing
// data.
func (h *CityEconomyHandler) ListRealtimeAgentDecisionDeadLetterEvents(c *gin.Context) {
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	requestCode := strings.TrimSpace(c.Param("request_code"))
	if requestCode == "" || len(requestCode) > 96 {
		response.BadRequest(c, "Invalid realtime agent decision request code")
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		if ok {
			response.BadRequest(c, "Invalid realtime agent decision dead letter event query")
		}
		return
	}
	beforeEventID, ok := parseCityQueryInt(c, "before_event_id", 0)
	if !ok || beforeEventID < 0 {
		if ok {
			response.BadRequest(c, "Invalid realtime agent decision dead letter event query")
		}
		return
	}
	page, err := h.service.ListRealtimeAgentDecisionDeadLetterEvents(c.Request.Context(), service.CityRealtimeAgentDecisionDeadLetterEventListInput{
		WorldID:       worldID,
		RequestCode:   requestCode,
		BeforeEventID: beforeEventID,
		Limit:         int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

// RetryRealtimeAgentDecisionNow only wakes an already-deferred queue item. It
// has no body and requires an idempotency key; the normal worker is still the
// only component that may lease, call a provider and finalize a decision.
func (h *CityEconomyHandler) RetryRealtimeAgentDecisionNow(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	requestCode := strings.TrimSpace(c.Param("request_code"))
	if requestCode == "" || len(requestCode) > 96 {
		response.BadRequest(c, "Invalid realtime agent decision request code")
		return
	}
	payload := retryCityRealtimeAgentDecisionRequest{WorldID: worldID, RequestCode: requestCode}
	executeUserIdempotentJSON(
		c,
		fmt.Sprintf("admin.city.worlds.%d.agent-decision-queue.%s.retry", worldID, requestCode),
		payload,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.RetryRealtimeAgentDecisionNow(ctx, service.CityRealtimeAgentDecisionRetryInput{
				AdministratorUserID: subject.UserID,
				WorldID:             worldID,
				RequestCode:         requestCode,
			})
		},
	)
}

// QuarantineRealtimeAgentDecision creates an audited, administrative
// operational pause for a queued request. It accepts only a finite reason
// code; provider route, account, prompt and transcript fields are never part
// of this browser contract.
func (h *CityEconomyHandler) QuarantineRealtimeAgentDecision(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	requestCode := strings.TrimSpace(c.Param("request_code"))
	if requestCode == "" || len(requestCode) > 96 {
		response.BadRequest(c, "Invalid realtime agent decision request code")
		return
	}
	var req quarantineCityRealtimeAgentDecisionRequest
	if !decodeStrictCityRealtimeAgentModelJSON(c, &req, "realtime agent decision quarantine request") {
		return
	}
	req.WorldID = worldID
	req.RequestCode = requestCode
	executeUserIdempotentJSON(
		c,
		fmt.Sprintf("admin.city.worlds.%d.agent-decision-queue.%s.dead-letter", worldID, requestCode),
		req,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.QuarantineRealtimeAgentDecision(ctx, service.CityRealtimeAgentDecisionDeadLetterInput{
				AdministratorUserID: subject.UserID,
				WorldID:             worldID,
				RequestCode:         requestCode,
				ReasonCode:          req.ReasonCode,
			})
		},
	)
}

// ReleaseRealtimeAgentDecisionDeadLetter lifts only the administrative pause.
// It intentionally does not execute or wake the request; a separate retry
// action is required if a future retry deadline should be removed.
func (h *CityEconomyHandler) ReleaseRealtimeAgentDecisionDeadLetter(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	requestCode := strings.TrimSpace(c.Param("request_code"))
	if requestCode == "" || len(requestCode) > 96 {
		response.BadRequest(c, "Invalid realtime agent decision request code")
		return
	}
	payload := releaseCityRealtimeAgentDecisionDeadLetterRequest{WorldID: worldID, RequestCode: requestCode}
	executeUserIdempotentJSON(
		c,
		fmt.Sprintf("admin.city.worlds.%d.agent-decision-queue.%s.dead-letter.release", worldID, requestCode),
		payload,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.ReleaseRealtimeAgentDecisionDeadLetter(ctx, service.CityRealtimeAgentDecisionDeadLetterReleaseInput{
				AdministratorUserID: subject.UserID,
				WorldID:             worldID,
				RequestCode:         requestCode,
			})
		},
	)
}

func (h *CityEconomyHandler) CreateRealtimeAgentModelProfile(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req createCityRealtimeAgentModelProfileRequest
	if !decodeStrictCityRealtimeAgentModelJSON(c, &req, "realtime agent model profile request") {
		return
	}
	executeUserIdempotentJSON(c,
		"admin.city.agent-model-profiles.create",
		req,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.CreateRealtimeAgentModelProfile(ctx, service.CityRealtimeAgentModelProfileCreateInput{
				AdministratorUserID:         subject.UserID,
				Code:                        req.Code,
				DisplayName:                 req.DisplayName,
				ProviderCode:                req.ProviderCode,
				PlatformGroupID:             req.PlatformGroupID,
				ModelIdentifier:             req.ModelIdentifier,
				AllowedAgentDefinitionCodes: req.AllowedAgentDefinitionCodes,
				Temperature:                 req.Temperature,
				MaxInputTokens:              req.MaxInputTokens,
				MaxOutputTokens:             req.MaxOutputTokens,
				TimeoutMS:                   req.TimeoutMS,
				MaxConcurrency:              req.MaxConcurrency,
				RetryLimit:                  req.RetryLimit,
				MaxProfileHourlyRequests:    req.MaxProfileHourlyRequests,
				MaxProfileHourlyTokens:      req.MaxProfileHourlyTokens,
				MaxWorldHourlyRequests:      req.MaxWorldHourlyRequests,
				MaxWorldHourlyTokens:        req.MaxWorldHourlyTokens,
				MaxAgentHourlyRequests:      req.MaxAgentHourlyRequests,
				MaxAgentHourlyTokens:        req.MaxAgentHourlyTokens,
				MaxOwnerHourlyRequests:      req.MaxOwnerHourlyRequests,
				MaxOwnerHourlyTokens:        req.MaxOwnerHourlyTokens,
				CircuitBreakerFailures:      req.CircuitBreakerFailures,
				CircuitBreakerCooldownSecs:  req.CircuitBreakerCooldownSecs,
				PrivacyClass:                req.PrivacyClass,
				RetentionPolicy:             req.RetentionPolicy,
				FallbackPolicy:              req.FallbackPolicy,
			})
		},
	)
}

func (h *CityEconomyHandler) UpdateRealtimeAgentModelProfileHead(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	profileCode := strings.ToLower(strings.TrimSpace(c.Param("profile_code")))
	if profileCode == "" {
		response.BadRequest(c, "Invalid realtime agent model profile code")
		return
	}
	var req updateCityRealtimeAgentModelProfileHeadRequest
	if !decodeStrictCityRealtimeAgentModelJSON(c, &req, "realtime agent model profile head request") {
		return
	}
	executeUserIdempotentJSON(c,
		fmt.Sprintf("admin.city.agent-model-profiles.%s.head.update", profileCode),
		req,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.SetRealtimeAgentModelProfileHead(ctx, service.CityRealtimeAgentModelProfileHeadUpdateInput{
				AdministratorUserID: subject.UserID,
				Code:                profileCode,
				Version:             req.Version,
				Status:              req.Status,
			})
		},
	)
}

func (h *CityEconomyHandler) ListRealtimeAgentModelProfileWorldBindings(c *gin.Context) {
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	items, err := h.service.ListRealtimeAgentModelProfileWorldBindings(c.Request.Context(), worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) BindRealtimeAgentModelProfileToWorld(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	var req bindCityRealtimeAgentModelProfileWorldRequest
	if !decodeStrictCityRealtimeAgentModelJSON(c, &req, "realtime agent model world binding request") {
		return
	}
	executeUserIdempotentJSON(c,
		fmt.Sprintf("admin.city.worlds.%d.agent-model-bindings.create", worldID),
		req,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.BindRealtimeAgentModelProfileToWorld(ctx, service.CityRealtimeAgentModelProfileWorldBindingInput{
				AdministratorUserID: subject.UserID,
				WorldID:             worldID,
				AgentDefinitionCode: req.AgentDefinitionCode,
				ProfileCode:         req.ProfileCode,
			})
		},
	)
}
