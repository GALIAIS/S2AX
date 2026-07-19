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
	Name           string `json:"name" binding:"required"`
	Timezone       string `json:"timezone"`
	StyleProfileID string `json:"style_profile_id"`
	SpawnPolicy    string `json:"spawn_policy"`
	MonetaryUnit   struct {
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

type addCityWorldMemberRequest struct {
	Identity string `json:"identity" binding:"required"`
	Role     string `json:"role" binding:"required"`
}

type updateCityWorldMemberRequest struct {
	Role   string `json:"role"`
	Status string `json:"status"`
}

type findCityNavigationPathRequest struct {
	ActorCode   string                            `json:"actor_code" binding:"required"`
	Destination *service.CityNavigationCoordinate `json:"destination" binding:"required"`
	MaxSteps    int                               `json:"max_steps"`
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

func (h *CityEconomyHandler) ListOpenWorldStyleProfiles(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	items, err := h.service.ListOpenWorldStyleProfiles(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) GetOpenWorldStyleProfile(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	profileID := strings.TrimSpace(c.Param("profile_id"))
	if profileID == "" {
		response.BadRequest(c, "Invalid open-world style profile ID")
		return
	}
	item, err := h.service.GetOpenWorldStyleProfile(c.Request.Context(), subject.UserID, profileID)
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
			OwnerUserID:    subject.UserID,
			Name:           req.Name,
			Timezone:       req.Timezone,
			StyleProfileID: req.StyleProfileID,
			SpawnPolicy:    req.SpawnPolicy,
			MonetaryUnit: service.CityMonetaryUnitCreateInput{
				Code: req.MonetaryUnit.Code, Name: req.MonetaryUnit.Name,
				Symbol: req.MonetaryUnit.Symbol, Scale: req.MonetaryUnit.Scale,
			},
		})
	})
}

func (h *CityEconomyHandler) GetOpenWorldGeneration(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	item, err := h.service.GetOpenWorldGeneration(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

// GetOpenWorldVerification replays immutable generated facts without writing
// them. Supplying both region_x and region_y scopes the proof to one bounded
// region; leaving both absent asks for the whole-world canonical proof.
func (h *CityEconomyHandler) GetOpenWorldVerification(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	regionX, hasRegionX, ok := parseCityOptionalSignedQueryInt(c, "region_x")
	if !ok {
		return
	}
	regionY, hasRegionY, ok := parseCityOptionalSignedQueryInt(c, "region_y")
	if !ok {
		return
	}
	if hasRegionX != hasRegionY {
		response.BadRequest(c, "region_x and region_y must be supplied together")
		return
	}
	var item *service.CityOpenWorldVerification
	var err error
	if hasRegionX {
		item, err = h.service.VerifyOpenWorldRegionMaterialization(
			c.Request.Context(), subject.UserID, worldID, regionX, regionY,
		)
	} else {
		item, err = h.service.VerifyOpenWorldMaterialization(c.Request.Context(), subject.UserID, worldID)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetOpenWorldMap(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	minimumX, ok := parseCitySignedQueryInt(c, "min_x", -128)
	if !ok {
		return
	}
	maximumX, ok := parseCitySignedQueryInt(c, "max_x", 127)
	if !ok {
		return
	}
	minimumY, ok := parseCitySignedQueryInt(c, "min_y", -128)
	if !ok {
		return
	}
	maximumY, ok := parseCitySignedQueryInt(c, "max_y", 127)
	if !ok {
		return
	}
	zValue, ok := parseCitySignedQueryInt(c, "z", 0)
	if !ok || zValue < -1<<31 || zValue > 1<<31-1 {
		if ok {
			response.BadRequest(c, "Invalid z")
		}
		return
	}
	item, err := h.service.GetOpenWorldMap(c.Request.Context(), service.CityOpenWorldMapInput{
		UserID: subject.UserID, WorldID: worldID,
		MinimumX: minimumX, MaximumX: maximumX, MinimumY: minimumY, MaximumY: maximumY, Z: int32(zValue),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) GetOpenWorldBuildingInterior(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	buildingCode := strings.TrimSpace(c.Param("building_code"))
	if buildingCode == "" {
		response.BadRequest(c, "Invalid building code")
		return
	}
	floorIndex, err := strconv.ParseInt(strings.TrimSpace(c.Param("floor_index")), 10, 32)
	if err != nil || floorIndex < 0 {
		response.BadRequest(c, "Invalid floor index")
		return
	}
	item, err := h.service.GetOpenWorldBuildingInterior(
		c.Request.Context(), subject.UserID, worldID, buildingCode, int32(floorIndex),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListOpenWorldBuildingPortals(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	buildingCode := strings.TrimSpace(c.Param("building_code"))
	if buildingCode == "" {
		response.BadRequest(c, "Invalid building code")
		return
	}
	items, err := h.service.ListOpenWorldBuildingPortals(
		c.Request.Context(), subject.UserID, worldID, buildingCode,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
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

func (h *CityEconomyHandler) ListWorldMembers(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	items, err := h.service.ListWorldMembers(c.Request.Context(), subject.UserID, worldID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) AddWorldMember(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	var req addCityWorldMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeUserIdempotentJSON(
		c,
		fmt.Sprintf("user.city.world.%d.members.add", worldID),
		req,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.AddWorldMember(ctx, service.CityMemberAddInput{
				UserID: subject.UserID, WorldID: worldID,
				Identity: req.Identity, Role: req.Role,
			})
		},
	)
}

func (h *CityEconomyHandler) UpdateWorldMember(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	targetUserID, ok := parseCityPathID(c, "user_id", "member")
	if !ok {
		return
	}
	var req updateCityWorldMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeUserIdempotentJSON(
		c,
		fmt.Sprintf("user.city.world.%d.members.%d.update", worldID, targetUserID),
		req,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.UpdateWorldMember(ctx, service.CityMemberUpdateInput{
				UserID: subject.UserID, WorldID: worldID, TargetUserID: targetUserID,
				Role: req.Role, Status: req.Status,
			})
		},
	)
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

func (h *CityEconomyHandler) FindWorldActorPath(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	var req findCityNavigationPathRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Destination == nil {
		response.BadRequest(c, "Invalid city navigation path request")
		return
	}
	item, err := h.service.FindWorldActorPath(c.Request.Context(), service.CityNavigationPathInput{
		UserID: subject.UserID, WorldID: worldID, ActorCode: req.ActorCode,
		Destination: *req.Destination, MaxSteps: req.MaxSteps,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListWorldPortalStates(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	items, err := h.service.ListWorldPortalStates(c.Request.Context(), service.WorldPortalAccessQueryInput{
		UserID: subject.UserID, WorldID: worldID, ActorCode: c.Query("actor_code"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) ListWorldNavigationIntents(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	items, err := h.service.ListWorldNavigationIntents(
		c.Request.Context(),
		service.WorldNavigationIntentQueryInput{
			UserID: subject.UserID, WorldID: worldID,
		},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) GetWorldNavigationIntent(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	item, err := h.service.GetWorldNavigationIntent(
		c.Request.Context(),
		service.WorldNavigationIntentQueryInput{
			UserID: subject.UserID, WorldID: worldID,
			ActorCode: c.Param("actor_code"),
		},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListWorldNavigationReservations(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	var tick *int64
	if raw := strings.TrimSpace(c.Query("tick")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 1 {
			response.BadRequest(c, "Invalid navigation reservation tick")
			return
		}
		tick = &value
	}
	items, err := h.service.ListWorldNavigationReservations(
		c.Request.Context(),
		service.WorldNavigationReservationQueryInput{
			UserID: subject.UserID, WorldID: worldID, Tick: tick,
		},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
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

func (h *CityEconomyHandler) GetCityServiceCatalog(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	item, err := h.service.GetCityServiceCatalog(c.Request.Context(), service.CityServiceQueryInput{
		UserID: subject.UserID, WorldID: worldID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) ListCityServiceFacilities(c *gin.Context) {
	h.cityServiceList(c, func(ctx context.Context, input service.CityServiceQueryInput) (any, error) {
		return h.service.ListCityServiceFacilities(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityServiceDemands(c *gin.Context) {
	h.cityServiceList(c, func(ctx context.Context, input service.CityServiceQueryInput) (any, error) {
		return h.service.ListCityServiceDemands(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityServiceConnections(c *gin.Context) {
	h.cityServiceList(c, func(ctx context.Context, input service.CityServiceQueryInput) (any, error) {
		return h.service.ListCityServiceConnections(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityServiceSettlements(c *gin.Context) {
	h.cityServiceList(c, func(ctx context.Context, input service.CityServiceQueryInput) (any, error) {
		return h.service.ListCityServiceSettlements(ctx, input)
	})
}

func (h *CityEconomyHandler) GetCityFacilityLifecycleCatalog(c *gin.Context) {
	h.cityFacilityLifecycleList(c, func(ctx context.Context, input service.CityFacilityLifecycleQueryInput) (any, error) {
		return h.service.GetCityFacilityLifecycleCatalog(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityFacilityLifecycleStates(c *gin.Context) {
	h.cityFacilityLifecycleList(c, func(ctx context.Context, input service.CityFacilityLifecycleQueryInput) (any, error) {
		return h.service.ListCityFacilityLifecycleStates(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityFacilityOperations(c *gin.Context) {
	h.cityFacilityLifecycleList(c, func(ctx context.Context, input service.CityFacilityLifecycleQueryInput) (any, error) {
		return h.service.ListCityFacilityOperations(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityFacilityStaffAssignments(c *gin.Context) {
	h.cityFacilityLifecycleList(c, func(ctx context.Context, input service.CityFacilityLifecycleQueryInput) (any, error) {
		return h.service.ListCityFacilityStaffAssignments(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityFacilityIncidents(c *gin.Context) {
	h.cityFacilityLifecycleList(c, func(ctx context.Context, input service.CityFacilityLifecycleQueryInput) (any, error) {
		return h.service.ListCityFacilityIncidents(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityFacilityBudgetMovements(c *gin.Context) {
	h.cityFacilityLifecycleList(c, func(ctx context.Context, input service.CityFacilityLifecycleQueryInput) (any, error) {
		return h.service.ListCityFacilityBudgetMovements(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityFacilityLifecycleFacts(c *gin.Context) {
	h.cityFacilityLifecycleList(c, func(ctx context.Context, input service.CityFacilityLifecycleQueryInput) (any, error) {
		return h.service.ListCityFacilityLifecycleFacts(ctx, input)
	})
}

func (h *CityEconomyHandler) GetCityPhysicalNetworkCatalog(c *gin.Context) {
	h.cityPhysicalNetworkList(c, func(ctx context.Context, input service.CityPhysicalNetworkQueryInput) (any, error) {
		return h.service.GetCityPhysicalNetworkCatalog(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityPhysicalNetworks(c *gin.Context) {
	h.cityPhysicalNetworkList(c, func(ctx context.Context, input service.CityPhysicalNetworkQueryInput) (any, error) {
		return h.service.ListCityPhysicalNetworks(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityPhysicalNetworkNodes(c *gin.Context) {
	h.cityPhysicalNetworkList(c, func(ctx context.Context, input service.CityPhysicalNetworkQueryInput) (any, error) {
		return h.service.ListCityPhysicalNetworkNodes(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityPhysicalNetworkEdges(c *gin.Context) {
	h.cityPhysicalNetworkList(c, func(ctx context.Context, input service.CityPhysicalNetworkQueryInput) (any, error) {
		return h.service.ListCityPhysicalNetworkEdges(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityPhysicalNetworkFlows(c *gin.Context) {
	h.cityPhysicalNetworkList(c, func(ctx context.Context, input service.CityPhysicalNetworkQueryInput) (any, error) {
		return h.service.ListCityPhysicalNetworkFlows(ctx, input)
	})
}

func (h *CityEconomyHandler) ListCityPhysicalNetworkFacts(c *gin.Context) {
	h.cityPhysicalNetworkList(c, func(ctx context.Context, input service.CityPhysicalNetworkQueryInput) (any, error) {
		return h.service.ListCityPhysicalNetworkFacts(ctx, input)
	})
}

func (h *CityEconomyHandler) GetCityPhysicalNetworkDiagnostics(c *gin.Context) {
	h.cityPhysicalNetworkList(c, func(ctx context.Context, input service.CityPhysicalNetworkQueryInput) (any, error) {
		return h.service.GetCityPhysicalNetworkDiagnostics(ctx, input)
	})
}

func (h *CityEconomyHandler) cityPhysicalNetworkList(
	c *gin.Context,
	load func(context.Context, service.CityPhysicalNetworkQueryInput) (any, error),
) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		if ok {
			response.BadRequest(c, "Invalid physical network query")
		}
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
	probeUnits, ok := parseCityQueryInt(c, "probe_units", 0)
	if !ok {
		return
	}
	item, err := load(c.Request.Context(), service.CityPhysicalNetworkQueryInput{
		UserID: subject.UserID, WorldID: worldID,
		ServiceCode: c.Query("service"), NetworkCode: c.Query("network"),
		Status: c.Query("status"), Role: c.Query("role"),
		Phase: c.Query("phase"), FactType: c.Query("fact_type"),
		SourceNodeCode: c.Query("source"), SinkNodeCode: c.Query("sink"),
		ProbeUnits: probeUnits,
		AfterCode:  c.Query("after_code"), AfterTick: afterTick,
		AfterSequence: afterSequence, Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) cityFacilityLifecycleList(
	c *gin.Context,
	load func(context.Context, service.CityFacilityLifecycleQueryInput) (any, error),
) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		if ok {
			response.BadRequest(c, "Invalid facility lifecycle query")
		}
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
	item, err := load(c.Request.Context(), service.CityFacilityLifecycleQueryInput{
		UserID: subject.UserID, WorldID: worldID,
		FacilityCode: c.Query("facility"), FacilityTypeCode: c.Query("facility_type"),
		LifecycleStatus: c.Query("lifecycle_status"), OperationType: c.Query("operation_type"),
		OperationStatus: c.Query("operation_status"), StaffingStatus: c.Query("staffing_status"),
		IncidentStatus: c.Query("incident_status"), BudgetCode: c.Query("budget"),
		AfterCode: c.Query("after_code"), AfterTick: afterTick,
		AfterSequence: afterSequence, Limit: int(limit),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) cityServiceList(
	c *gin.Context,
	load func(context.Context, service.CityServiceQueryInput) (any, error),
) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		if ok {
			response.BadRequest(c, "Invalid city service query")
		}
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
	item, err := load(c.Request.Context(), service.CityServiceQueryInput{
		UserID: subject.UserID, WorldID: worldID,
		ServiceCode: c.Query("service"), Status: c.Query("status"),
		DistrictCode: c.Query("district"), FacilityCode: c.Query("facility"),
		DemandCode: c.Query("demand"), AfterCode: c.Query("after_code"),
		AfterTick: afterTick, AfterSequence: afterSequence, Limit: int(limit),
	})
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
	if !isCitySystemAdministrator(c) && !isCityPlayerCommand(req.CommandType) {
		response.ErrorFrom(c, service.ErrCityManagementRequired)
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

func isCitySystemAdministrator(c *gin.Context) bool {
	role, ok := middleware2.GetUserRoleFromContext(c)
	return ok && role == service.RoleAdmin
}

func isCityPlayerCommand(commandType string) bool {
	switch strings.ToLower(strings.TrimSpace(commandType)) {
	case service.CityCommandTypeActorCreate,
		service.CityCommandTypeActorActivityPerform,
		service.CityCommandTypeActorRoleTransition,
		service.CityCommandTypeActorLocationMove,
		service.CityCommandTypePortalStateTransition,
		service.CityCommandTypeActorNavigationIntentSet,
		service.CityCommandTypeActorNavigationIntentCancel,
		service.CityCommandTypeOpenWorldActorCreate,
		service.CityCommandTypeOpenWorldActorActivityPerform,
		service.CityCommandTypeOpenWorldActorRoleTransition,
		service.CityCommandTypeOpenWorldActorMove,
		service.CityCommandTypeOpenWorldActorPortalUse,
		service.CityCommandTypeOpenWorldActorNavigationSet,
		service.CityCommandTypeOpenWorldActorNavigationCancel:
		return true
	default:
		return false
	}
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

func (h *CityEconomyHandler) ListCommands(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	worldID, ok := parseCityPathID(c, "world_id", "world")
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
	items, err := h.service.ListCommands(c.Request.Context(), service.CityCommandListInput{
		UserID: subject.UserID, WorldID: worldID,
		Status:        strings.ToLower(strings.TrimSpace(c.Query("status"))),
		AfterSequence: afterSequence, Limit: int(limit),
		Latest: strings.EqualFold(strings.TrimSpace(c.Query("latest")), "true"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
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

func parseCityOptionalSignedQueryInt(c *gin.Context, name string) (int64, bool, bool) {
	raw, present := c.GetQuery(name)
	if !present {
		return 0, false, true
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid "+name)
		return 0, true, false
	}
	return value, true, true
}
