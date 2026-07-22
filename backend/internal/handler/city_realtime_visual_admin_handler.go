package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// The visual-pack API deliberately accepts no binary payload, remote URL,
// model selector, source image or raw prompt. This control plane only creates
// reviewed, data-only procedural manifests and structured worker intents.
type createCityRealtimeVisualPackRequest struct {
	PackID            string          `json:"pack_id" binding:"required"`
	PackVersion       string          `json:"pack_version" binding:"required"`
	SpatialProfileIDs []string        `json:"spatial_profile_ids" binding:"required"`
	Manifest          json.RawMessage `json:"manifest" binding:"required"`
}

type updateCityRealtimeVisualPackRequest struct {
	SpatialProfileIDs []string        `json:"spatial_profile_ids" binding:"required"`
	Manifest          json.RawMessage `json:"manifest" binding:"required"`
}

type createCityRealtimeVisualGenerationJobRequest struct {
	AssetClass   string   `json:"asset_class" binding:"required"`
	SemanticTags []string `json:"semantic_tags" binding:"required"`
	PixelWidth   int      `json:"pixel_width" binding:"required"`
	PixelHeight  int      `json:"pixel_height" binding:"required"`
	FrameCount   int      `json:"frame_count" binding:"required"`
}

type reviewCityRealtimeVisualGenerationJobRequest struct {
	Decision   string `json:"decision" binding:"required"`
	ReasonCode string `json:"reason_code"`
}

type setCityRealtimeVisualReleasePolicyRequest struct {
	PackID      string `json:"pack_id" binding:"required"`
	PackVersion string `json:"pack_version" binding:"required"`
}

func (h *CityEconomyHandler) ListRealtimeVisualPacks(c *gin.Context) {
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid visual pack list query")
		return
	}
	items, err := h.service.ListRealtimeVisualPacks(c.Request.Context(), int(limit))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) GetRealtimeVisualPack(c *gin.Context) {
	packID, packVersion, ok := parseCityRealtimeVisualPackPath(c)
	if !ok {
		return
	}
	item, err := h.service.GetRealtimeVisualPack(c.Request.Context(), packID, packVersion)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *CityEconomyHandler) CreateRealtimeVisualPack(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req createCityRealtimeVisualPackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid city visual pack request: "+err.Error())
		return
	}
	executeUserIdempotentJSON(c, "admin.city.visual-packs.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		return h.service.CreateRealtimeVisualPack(ctx, service.CityRealtimeVisualPackCreateInput{
			ActorUserID: subject.UserID,
			PackID:      req.PackID, PackVersion: req.PackVersion,
			SpatialProfileIDs: req.SpatialProfileIDs, Manifest: req.Manifest,
		})
	})
}

func (h *CityEconomyHandler) UpdateRealtimeVisualPack(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	packID, packVersion, ok := parseCityRealtimeVisualPackPath(c)
	if !ok {
		return
	}
	var req updateCityRealtimeVisualPackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid city visual pack request: "+err.Error())
		return
	}
	executeUserIdempotentJSON(c,
		fmt.Sprintf("admin.city.visual-packs.%s.%s.update", packID, packVersion),
		req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
			return h.service.UpdateRealtimeVisualPack(ctx, service.CityRealtimeVisualPackUpdateInput{
				ActorUserID: subject.UserID,
				PackID:      packID, PackVersion: packVersion,
				SpatialProfileIDs: req.SpatialProfileIDs, Manifest: req.Manifest,
			})
		},
	)
}

func (h *CityEconomyHandler) ListRealtimeVisualGenerationJobs(c *gin.Context) {
	packID, packVersion, ok := parseCityRealtimeVisualPackPath(c)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid visual generation job query")
		return
	}
	items, err := h.service.ListRealtimeVisualGenerationJobs(c.Request.Context(), packID, packVersion, int(limit))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) CreateRealtimeVisualGenerationJob(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	packID, packVersion, ok := parseCityRealtimeVisualPackPath(c)
	if !ok {
		return
	}
	var req createCityRealtimeVisualGenerationJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid city visual generation request: "+err.Error())
		return
	}
	executeUserIdempotentJSON(c,
		fmt.Sprintf("admin.city.visual-packs.%s.%s.generation-jobs.create", packID, packVersion),
		req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
			return h.service.CreateRealtimeVisualGenerationJob(ctx, service.CityRealtimeVisualGenerationJobCreateInput{
				ActorUserID: subject.UserID,
				PackID:      packID, PackVersion: packVersion,
				AssetClass: req.AssetClass, SemanticTags: req.SemanticTags,
				PixelWidth: req.PixelWidth, PixelHeight: req.PixelHeight, FrameCount: req.FrameCount,
			})
		},
	)
}

func (h *CityEconomyHandler) ReviewRealtimeVisualGenerationJob(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	packID, packVersion, ok := parseCityRealtimeVisualPackPath(c)
	if !ok {
		return
	}
	jobID, ok := parseCityPathID(c, "job_id", "visual generation job")
	if !ok {
		return
	}
	var req reviewCityRealtimeVisualGenerationJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid city visual generation review: "+err.Error())
		return
	}
	executeUserIdempotentJSON(c,
		fmt.Sprintf("admin.city.visual-packs.%s.%s.generation-jobs.%d.review", packID, packVersion, jobID),
		req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
			return h.service.ReviewRealtimeVisualGenerationJob(ctx, service.CityRealtimeVisualGenerationJobReviewInput{
				ActorUserID: subject.UserID,
				PackID:      packID, PackVersion: packVersion, JobID: jobID,
				Decision: req.Decision, ReasonCode: req.ReasonCode,
			})
		},
	)
}

func (h *CityEconomyHandler) PublishRealtimeVisualPack(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	packID, packVersion, ok := parseCityRealtimeVisualPackPath(c)
	if !ok {
		return
	}
	payload := map[string]string{"pack_id": packID, "pack_version": packVersion, "operation": "publish"}
	executeUserIdempotentJSON(c,
		fmt.Sprintf("admin.city.visual-packs.%s.%s.publish", packID, packVersion),
		payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
			return h.service.PublishRealtimeVisualPack(ctx, subject.UserID, packID, packVersion)
		},
	)
}

func (h *CityEconomyHandler) RetireRealtimeVisualPack(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	packID, packVersion, ok := parseCityRealtimeVisualPackPath(c)
	if !ok {
		return
	}
	payload := map[string]string{"pack_id": packID, "pack_version": packVersion, "operation": "retire"}
	executeUserIdempotentJSON(c,
		fmt.Sprintf("admin.city.visual-packs.%s.%s.retire", packID, packVersion),
		payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
			return h.service.RetireRealtimeVisualPack(ctx, subject.UserID, packID, packVersion)
		},
	)
}

func (h *CityEconomyHandler) ListRealtimeVisualReleasePolicies(c *gin.Context) {
	items, err := h.service.ListRealtimeVisualReleasePolicies(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *CityEconomyHandler) SetRealtimeVisualReleasePolicy(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	profileID := strings.TrimSpace(c.Param("spatial_profile_id"))
	if profileID == "" {
		response.BadRequest(c, "Invalid spatial profile ID")
		return
	}
	var req setCityRealtimeVisualReleasePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid city visual release policy request: "+err.Error())
		return
	}
	executeUserIdempotentJSON(c,
		fmt.Sprintf("admin.city.visual-release-policies.%s.assign", profileID),
		req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
			return h.service.SetRealtimeVisualReleasePolicy(ctx, service.CityRealtimeVisualReleasePolicySetInput{
				ActorUserID: subject.UserID, SpatialProfileID: profileID,
				PackID: req.PackID, PackVersion: req.PackVersion,
			})
		},
	)
}

func (h *CityEconomyHandler) ListRealtimeVisualReviewEvents(c *gin.Context) {
	packID, packVersion, ok := parseCityRealtimeVisualPackPath(c)
	if !ok {
		return
	}
	limit, ok := parseCityQueryInt(c, "limit", 0)
	if !ok || limit > int64(^uint(0)>>1) {
		response.BadRequest(c, "Invalid visual review event query")
		return
	}
	items, err := h.service.ListRealtimeVisualReviewEvents(c.Request.Context(), packID, packVersion, int(limit))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func parseCityRealtimeVisualPackPath(c *gin.Context) (string, string, bool) {
	packID := strings.TrimSpace(c.Param("pack_id"))
	packVersion := strings.TrimSpace(c.Param("pack_version"))
	if packID == "" || packVersion == "" {
		response.BadRequest(c, "Invalid visual pack ID")
		return "", "", false
	}
	return packID, packVersion, true
}
