package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type CityEconomyHandler struct {
	service *service.CityEconomyService
}

func NewCityEconomyHandler(cityService *service.CityEconomyService) *CityEconomyHandler {
	return &CityEconomyHandler{service: cityService}
}

type createCityWorldRequest struct {
	Name         string `json:"name" binding:"required"`
	Timezone     string `json:"timezone"`
	MonetaryUnit struct {
		Code   string `json:"code"`
		Name   string `json:"name"`
		Symbol string `json:"symbol"`
		Scale  *int   `json:"scale"`
	} `json:"monetary_unit"`
}

type submitCityCommandRequest struct {
	CommandType       string          `json:"command_type" binding:"required"`
	Payload           json.RawMessage `json:"payload"`
	ExpectedWorldTick *int64          `json:"expected_world_tick"`
}

type stepCityWorldRequest struct {
	ExpectedWorldTick *int64 `json:"expected_world_tick"`
}

type startCityReplayRequest struct {
	FromTick   *int64 `json:"from_tick"`
	TargetTick *int64 `json:"target_tick"`
}

type startCityRecoveryRequest struct {
	ReplayRunID int64 `json:"replay_run_id" binding:"required"`
}

type startCityUpgradeRequest struct {
	TargetVersion string `json:"target_version" binding:"required"`
	DryRun        *bool  `json:"dry_run"`
}

func (h *CityEconomyHandler) ListWorlds(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	items, err := h.service.ListWorlds(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) ListSpatialRuleSets(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	items, err := h.service.ListSpatialRuleSets(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) GetSpatialRuleSet(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	ruleSetID := strings.TrimSpace(c.Param("rule_set_id"))
	if ruleSetID == "" {
		response.BadRequest(c, "Invalid spatial rule set ID")
		return
	}
	item, err := h.service.GetSpatialRuleSet(c.Request.Context(), subject.UserID, ruleSetID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) CreateWorld(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req createCityWorldRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeUserIdempotentJSON(c, "user.city.worlds.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		return h.service.CreateWorld(ctx, service.CityWorldCreateInput{
			OwnerUserID: subject.UserID,
			Name:        req.Name,
			Timezone:    req.Timezone,
			MonetaryUnit: service.CityMonetaryUnitCreateInput{
				Code: req.MonetaryUnit.Code, Name: req.MonetaryUnit.Name,
				Symbol: req.MonetaryUnit.Symbol, Scale: req.MonetaryUnit.Scale,
			},
		})
	})
}

func (h *CityEconomyHandler) GetWorld(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, err := strconv.ParseInt(strings.TrimSpace(c.Param("world_id")), 10, 64)
	if err != nil || worldID <= 0 {
		response.BadRequest(c, "Invalid world ID")
		return
	}
	item, err := h.service.GetWorld(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetPhysicalState(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	item, err := h.service.GetPhysicalState(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetCalendarState(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	item, err := h.service.GetCalendarState(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetPopulationState(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	item, err := h.service.GetPopulationState(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetWorldRuntimeCatalog(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	item, err := h.service.GetWorldRuntimeCatalog(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListWorldActors(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	items, err := h.service.ListWorldActors(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) GetWorldActorState(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	actorCode := strings.ToLower(strings.TrimSpace(c.Param("actor_code")))
	if actorCode == "" {
		response.BadRequest(c, "Invalid actor code")
		return
	}
	item, err := h.service.GetWorldActorState(c.Request.Context(), subject.UserID, worldID, actorCode)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetWorldActorRoleOptions(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	actorCode := strings.ToLower(strings.TrimSpace(c.Param("actor_code")))
	items, err := h.service.GetWorldActorRoleOptions(c.Request.Context(), subject.UserID, worldID, actorCode)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) ListWorldRules(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	items, err := h.service.ListWorldRules(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) ListWorldRuleCases(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterTick, ok := parseCityQueryInt(c, "after_tick", 0)
	if !ok {
		return
	}
	afterSequence, ok := parseCityQueryInt(c, "after_sequence", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 100)
	if !ok {
		return
	}
	items, err := h.service.QueryWorldRuleCases(c.Request.Context(), service.WorldRuleCaseQueryInput{
		UserID: subject.UserID, WorldID: worldID,
		ActorCode:    strings.ToLower(strings.TrimSpace(c.Query("actor_code"))),
		CategoryCode: strings.ToLower(strings.TrimSpace(c.Query("category_code"))),
		Status:       strings.ToLower(strings.TrimSpace(c.Query("status"))),
		AfterTick:    afterTick, AfterSequence: afterSequence, Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) GetWorldSpatialRuleSet(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	item, err := h.service.GetWorldSpatialRuleSet(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetOvermap(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	item, err := h.service.GetOvermap(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetLandState(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	minimumX, ok := parseCitySignedQueryInt(c, "min_x", service.CitySpatialDefaultMinimumChunk)
	if !ok {
		return
	}
	maximumX, ok := parseCitySignedQueryInt(c, "max_x", service.CitySpatialDefaultMaximumChunk)
	if !ok {
		return
	}
	minimumY, ok := parseCitySignedQueryInt(c, "min_y", service.CitySpatialDefaultMinimumChunk)
	if !ok {
		return
	}
	maximumY, ok := parseCitySignedQueryInt(c, "max_y", service.CitySpatialDefaultMaximumChunk)
	if !ok {
		return
	}
	zValue, ok := parseCitySignedQueryInt(c, "z", int64(service.CitySpatialDefaultSurfaceZ))
	if !ok || zValue < -1<<31 || zValue > 1<<31-1 {
		if ok {
			response.BadRequest(c, "Invalid z")
		}
		return
	}
	item, err := h.service.GetLandState(c.Request.Context(), service.CityLandQueryInput{
		UserID: subject.UserID, WorldID: worldID,
		MinimumX: minimumX, MaximumX: maximumX,
		MinimumY: minimumY, MaximumY: maximumY,
		Z: int32(zValue),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetDevelopmentState(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterTick, ok := parseCityQueryInt(c, "after_tick", 0)
	if !ok {
		return
	}
	afterSequence, ok := parseCityQueryInt(c, "after_sequence", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok {
		return
	}
	item, err := h.service.GetDevelopmentState(c.Request.Context(), service.CityDevelopmentQueryInput{
		UserID: subject.UserID, WorldID: worldID,
		Status: c.Query("status"), BuildingCode: c.Query("building_code"),
		AfterTick: afterTick, AfterSequence: afterSequence, Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetEnterpriseLocationState(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterTick, ok := parseCityQueryInt(c, "after_tick", 0)
	if !ok {
		return
	}
	afterSequence, ok := parseCityQueryInt(c, "after_sequence", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok {
		return
	}
	item, err := h.service.GetEnterpriseLocationState(
		c.Request.Context(),
		service.CityEnterpriseLocationQueryInput{
			UserID: subject.UserID, WorldID: worldID,
			FirmCode: c.Query("firm_code"), DistrictCode: c.Query("district_code"),
			SiteType: c.Query("site_type"), Status: c.Query("status"),
			AfterTick: afterTick, AfterSequence: afterSequence, Limit: int(limit),
		},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListMapChunks(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	minimumX, ok := parseCitySignedQueryInt(c, "min_x", service.CitySpatialDefaultMinimumChunk)
	if !ok {
		return
	}
	maximumX, ok := parseCitySignedQueryInt(c, "max_x", service.CitySpatialDefaultMaximumChunk)
	if !ok {
		return
	}
	minimumY, ok := parseCitySignedQueryInt(c, "min_y", service.CitySpatialDefaultMinimumChunk)
	if !ok {
		return
	}
	maximumY, ok := parseCitySignedQueryInt(c, "max_y", service.CitySpatialDefaultMaximumChunk)
	if !ok {
		return
	}
	zValue, ok := parseCitySignedQueryInt(c, "z", int64(service.CitySpatialDefaultSurfaceZ))
	if !ok || zValue < -1<<31 || zValue > 1<<31-1 {
		if ok {
			response.BadRequest(c, "Invalid z")
		}
		return
	}
	items, err := h.service.ListMapChunks(c.Request.Context(), service.CityMapChunkListInput{
		UserID: subject.UserID, WorldID: worldID,
		MinimumX: minimumX, MaximumX: maximumX, MinimumY: minimumY, MaximumY: maximumY,
		Z: int32(zValue),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) GetMapChunk(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	chunkX, ok := parseCityPathSigned(c, "chunk_x", "chunk x")
	if !ok {
		return
	}
	chunkY, ok := parseCityPathSigned(c, "chunk_y", "chunk y")
	if !ok {
		return
	}
	zValue, ok := parseCityPathSigned(c, "z", "chunk z")
	if !ok || zValue < -1<<31 || zValue > 1<<31-1 {
		if ok {
			response.BadRequest(c, "Invalid chunk z")
		}
		return
	}
	item, err := h.service.GetMapChunk(
		c.Request.Context(), subject.UserID, worldID, chunkX, chunkY, int32(zValue),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListSpatialMutations(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterTick, ok := parseCityQueryInt(c, "after_tick", 0)
	if !ok {
		return
	}
	afterSequence, ok := parseCityQueryInt(c, "after_sequence", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		if ok {
			response.BadRequest(c, "Invalid city spatial change query")
		}
		return
	}
	page, err := h.service.ListSpatialMutations(c.Request.Context(), service.CitySpatialMutationListInput{
		UserID: subject.UserID, WorldID: worldID, AfterTick: afterTick,
		AfterSequence: afterSequence, Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func (h *CityEconomyHandler) GetMarketOverview(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	item, err := h.service.GetMarketOverview(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) SubmitCommand(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	var req submitCityCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeUserIdempotentJSON(
		c,
		fmt.Sprintf("user.city.world.%d.commands.create", worldID),
		req,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.SubmitCommand(ctx, service.CityCommandSubmitInput{
				UserID:            subject.UserID,
				WorldID:           worldID,
				IdempotencyKey:    c.GetHeader("Idempotency-Key"),
				CommandType:       req.CommandType,
				Payload:           req.Payload,
				ExpectedWorldTick: req.ExpectedWorldTick,
			})
		},
	)
}

func (h *CityEconomyHandler) GetCommand(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	commandID, ok := parseCityPathID(c, "command_id", "command")
	if !ok {
		return
	}
	item, err := h.service.GetCommand(c.Request.Context(), subject.UserID, worldID, commandID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) StepWorld(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	var req stepCityWorldRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeUserIdempotentJSON(
		c,
		fmt.Sprintf("user.city.world.%d.step", worldID),
		req,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.StepWorld(ctx, service.CityStepInput{
				UserID:            subject.UserID,
				WorldID:           worldID,
				IdempotencyKey:    c.GetHeader("Idempotency-Key"),
				ExpectedWorldTick: req.ExpectedWorldTick,
			})
		},
	)
}

func (h *CityEconomyHandler) ListEvents(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterTick, ok := parseCityQueryInt(c, "after_tick", 0)
	if !ok {
		return
	}
	afterSequence, ok := parseCityQueryInt(c, "after_sequence", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok {
		return
	}
	if afterSequence > int64(^uint(0)>>1) || limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid city event query")
		return
	}
	page, err := h.service.ListEvents(c.Request.Context(), service.CityEventListInput{
		UserID:        subject.UserID,
		WorldID:       worldID,
		AfterTick:     afterTick,
		AfterSequence: int(afterSequence),
		Limit:         int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func (h *CityEconomyHandler) ListJournals(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterTick, ok := parseCityQueryInt(c, "after_tick", 0)
	if !ok {
		return
	}
	afterSequence, ok := parseCityQueryInt(c, "after_sequence", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok {
		return
	}
	if limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid city journal query")
		return
	}
	page, err := h.service.ListJournals(c.Request.Context(), service.CityJournalListInput{
		UserID: subject.UserID, WorldID: worldID, AfterTick: afterTick,
		AfterSequence: afterSequence, Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func (h *CityEconomyHandler) GetJournal(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	tick, ok := parseCityPathID(c, "tick", "journal tick")
	if !ok {
		return
	}
	sequence, ok := parseCityPathID(c, "sequence", "journal sequence")
	if !ok {
		return
	}
	item, err := h.service.GetJournal(c.Request.Context(), subject.UserID, worldID, tick, sequence)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetTrialBalance(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	item, err := h.service.GetTrialBalance(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListResourceOperations(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterTick, ok := parseCityQueryInt(c, "after_tick", 0)
	if !ok {
		return
	}
	afterSequence, ok := parseCityQueryInt(c, "after_sequence", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok {
		return
	}
	if limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid city resource operation query")
		return
	}
	page, err := h.service.ListResourceOperations(c.Request.Context(), service.CityResourceOperationListInput{
		UserID: subject.UserID, WorldID: worldID, AfterTick: afterTick,
		AfterSequence: afterSequence, Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func (h *CityEconomyHandler) GetResourceOperation(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	tick, ok := parseCityPathID(c, "tick", "resource operation tick")
	if !ok {
		return
	}
	sequence, ok := parseCityPathID(c, "sequence", "resource operation sequence")
	if !ok {
		return
	}
	item, err := h.service.GetResourceOperation(c.Request.Context(), subject.UserID, worldID, tick, sequence)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListMarketSettlements(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterTick, ok := parseCityQueryInt(c, "after_tick", 0)
	if !ok {
		return
	}
	afterSequence, ok := parseCityQueryInt(c, "after_sequence", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok {
		return
	}
	if afterSequence > int64(^uint(0)>>1) || limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid city market settlement query")
		return
	}
	page, err := h.service.ListMarketSettlements(c.Request.Context(), service.CityMarketSettlementListInput{
		UserID: subject.UserID, WorldID: worldID, AfterTick: afterTick,
		AfterSequence: int(afterSequence), Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func (h *CityEconomyHandler) GetMarketSettlement(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	tick, ok := parseCityPathID(c, "tick", "market settlement tick")
	if !ok {
		return
	}
	sequence, ok := parseCityPathID(c, "sequence", "market settlement sequence")
	if !ok {
		return
	}
	if sequence > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid market settlement sequence")
		return
	}
	item, err := h.service.GetMarketSettlement(c.Request.Context(), subject.UserID, worldID, tick, int(sequence))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListPopulationMovements(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterTick, ok := parseCityQueryInt(c, "after_tick", 0)
	if !ok {
		return
	}
	afterSequence, ok := parseCityQueryInt(c, "after_sequence", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || afterSequence > int64(^uint(0)>>1) || limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid city population movement query")
		return
	}
	page, err := h.service.ListPopulationMovements(c.Request.Context(), service.CityPopulationMovementListInput{
		UserID: subject.UserID, WorldID: worldID, AfterTick: afterTick,
		AfterSequence: int(afterSequence), Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func (h *CityEconomyHandler) GetPopulationMovement(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	tick, ok := parseCityPathID(c, "tick", "population movement tick")
	if !ok {
		return
	}
	sequence, ok := parseCityPathID(c, "sequence", "population movement sequence")
	if !ok || sequence > int64(^uint(0)>>1) {
		if ok {
			response.BadRequest(c, "Invalid population movement sequence")
		}
		return
	}
	item, err := h.service.GetPopulationMovement(
		c.Request.Context(), subject.UserID, worldID, tick, int(sequence),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListPopulationMigrations(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterTick, ok := parseCityQueryInt(c, "after_tick", 0)
	if !ok {
		return
	}
	afterSequence, ok := parseCityQueryInt(c, "after_sequence", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid city population migration query")
		return
	}
	page, err := h.service.ListPopulationMigrations(c.Request.Context(), service.CityPopulationMigrationListInput{
		UserID: subject.UserID, WorldID: worldID, AfterTick: afterTick,
		AfterSequence: afterSequence, Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func (h *CityEconomyHandler) GetPopulationMigration(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	tick, ok := parseCityPathID(c, "tick", "population migration tick")
	if !ok {
		return
	}
	sequence, ok := parseCityPathID(c, "sequence", "population migration sequence")
	if !ok {
		return
	}
	item, err := h.service.GetPopulationMigration(
		c.Request.Context(), subject.UserID, worldID, tick, sequence,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListHouseholdMovements(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterTick, ok := parseCityQueryInt(c, "after_tick", 0)
	if !ok {
		return
	}
	afterSequence, ok := parseCityQueryInt(c, "after_sequence", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid city household movement query")
		return
	}
	page, err := h.service.ListHouseholdMovements(c.Request.Context(), service.CityHouseholdMovementListInput{
		UserID: subject.UserID, WorldID: worldID, AfterTick: afterTick,
		AfterSequence: afterSequence, Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func (h *CityEconomyHandler) GetHouseholdMovement(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	tick, ok := parseCityPathID(c, "tick", "household movement tick")
	if !ok {
		return
	}
	sequence, ok := parseCityPathID(c, "sequence", "household movement sequence")
	if !ok {
		return
	}
	item, err := h.service.GetHouseholdMovement(
		c.Request.Context(), subject.UserID, worldID, tick, sequence,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListSnapshots(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterTick, ok := parseCityQueryInt(c, "after_tick", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid city snapshot query")
		return
	}
	page, err := h.service.ListSnapshots(c.Request.Context(), service.CitySnapshotListInput{
		UserID: subject.UserID, WorldID: worldID, AfterTick: afterTick, Limit: int(limit),
		SimulationVersion: c.Query("version"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func (h *CityEconomyHandler) GetSnapshot(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	tick, ok := parseCityPathNonNegative(c, "tick", "snapshot tick")
	if !ok {
		return
	}
	item, err := h.service.GetSnapshotVersion(
		c.Request.Context(), subject.UserID, worldID, tick, c.Query("version"),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetEngineInfo(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	item, err := h.service.GetEngineInfo(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) StartUpgrade(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	var req startCityUpgradeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}
	executeUserIdempotentJSON(
		c,
		fmt.Sprintf("user.city.world.%d.engine-upgrade", worldID),
		req,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.StartUpgrade(ctx, service.CityUpgradeInput{
				UserID: subject.UserID, WorldID: worldID,
				IdempotencyKey: c.GetHeader("Idempotency-Key"),
				TargetVersion:  req.TargetVersion, DryRun: dryRun,
			})
		},
	)
}

func (h *CityEconomyHandler) GetUpgrade(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	runID, ok := parseCityPathID(c, "run_id", "engine upgrade run")
	if !ok {
		return
	}
	item, err := h.service.GetUpgrade(c.Request.Context(), subject.UserID, worldID, runID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListUpgrades(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterID, ok := parseCityQueryInt(c, "after_id", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid city engine upgrade query")
		return
	}
	page, err := h.service.ListUpgrades(c.Request.Context(), service.CityAuditRunListInput{
		UserID: subject.UserID, WorldID: worldID, AfterID: afterID, Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func (h *CityEconomyHandler) StartReplay(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	var req startCityReplayRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeUserIdempotentJSON(
		c,
		fmt.Sprintf("user.city.world.%d.replay", worldID),
		req,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.StartReplay(ctx, service.CityReplayInput{
				UserID: subject.UserID, WorldID: worldID,
				IdempotencyKey: c.GetHeader("Idempotency-Key"),
				FromTick:       req.FromTick, TargetTick: req.TargetTick,
			})
		},
	)
}

func (h *CityEconomyHandler) GetReplay(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	runID, ok := parseCityPathID(c, "run_id", "replay run")
	if !ok {
		return
	}
	item, err := h.service.GetReplay(c.Request.Context(), subject.UserID, worldID, runID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListReplays(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterID, ok := parseCityQueryInt(c, "after_id", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid city replay query")
		return
	}
	page, err := h.service.ListReplays(c.Request.Context(), service.CityAuditRunListInput{
		UserID: subject.UserID, WorldID: worldID, AfterID: afterID, Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func (h *CityEconomyHandler) StartRecovery(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	var req startCityRecoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeUserIdempotentJSON(
		c,
		fmt.Sprintf("user.city.world.%d.recovery", worldID),
		req,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.StartRecovery(ctx, service.CityRecoveryInput{
				UserID: subject.UserID, WorldID: worldID,
				IdempotencyKey: c.GetHeader("Idempotency-Key"), ReplayRunID: req.ReplayRunID,
			})
		},
	)
}

func (h *CityEconomyHandler) GetRecovery(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	runID, ok := parseCityPathID(c, "run_id", "recovery run")
	if !ok {
		return
	}
	item, err := h.service.GetRecovery(c.Request.Context(), subject.UserID, worldID, runID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListRecoveries(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	afterID, ok := parseCityQueryInt(c, "after_id", 0)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid city recovery query")
		return
	}
	page, err := h.service.ListRecoveries(c.Request.Context(), service.CityAuditRunListInput{
		UserID: subject.UserID, WorldID: worldID, AfterID: afterID, Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func parseCityPathID(c *gin.Context, parameter, label string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(c.Param(parameter)), 10, 64)
	if err != nil || value <= 0 {
		response.BadRequest(c, "Invalid "+label+" ID")
		return 0, false
	}
	return value, true
}

func parseCityPathNonNegative(c *gin.Context, parameter, label string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(c.Param(parameter)), 10, 64)
	if err != nil || value < 0 {
		response.BadRequest(c, "Invalid "+label)
		return 0, false
	}
	return value, true
}

func parseCityPathSigned(c *gin.Context, parameter, label string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(c.Param(parameter)), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid "+label)
		return 0, false
	}
	return value, true
}

func parseCityQueryInt(c *gin.Context, name string, defaultValue int64) (int64, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return defaultValue, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		response.BadRequest(c, "Invalid "+name)
		return 0, false
	}
	return value, true
}

func parseCitySignedQueryInt(c *gin.Context, name string, defaultValue int64) (int64, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return defaultValue, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid "+name)
		return 0, false
	}
	return value, true
}
