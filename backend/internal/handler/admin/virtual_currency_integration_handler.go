package admin

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type VirtualCurrencyIntegrationHandler struct {
	service *service.VirtualCurrencyIntegrationService
}

func NewVirtualCurrencyIntegrationHandler(integrationService *service.VirtualCurrencyIntegrationService) *VirtualCurrencyIntegrationHandler {
	return &VirtualCurrencyIntegrationHandler{service: integrationService}
}

type createVirtualCurrencyIntegrationRequest struct {
	Code     string         `json:"code"`
	Name     string         `json:"name"`
	Metadata map[string]any `json:"metadata"`
}

type updateVirtualCurrencyIntegrationRequest struct {
	Name     *string        `json:"name"`
	Metadata map[string]any `json:"metadata"`
}

type virtualCurrencyIntegrationStatusRequest struct {
	Status string `json:"status"`
}

type virtualCurrencyIntegrationScopeRequest struct {
	Enabled   bool           `json:"enabled"`
	CanEarn   bool           `json:"can_earn"`
	CanSpend  bool           `json:"can_spend"`
	CanSettle bool           `json:"can_settle"`
	Metadata  map[string]any `json:"metadata"`
}

func (h *VirtualCurrencyIntegrationHandler) List(c *gin.Context) {
	includeDisabled, err := parseOptionalBool(c.Query("include_disabled"))
	if err != nil {
		response.BadRequest(c, "include_disabled must be a boolean")
		return
	}
	items, err := h.service.List(c.Request.Context(), includeDisabled)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyIntegrationsFromService(items))
}

func (h *VirtualCurrencyIntegrationHandler) Get(c *gin.Context) {
	id, ok := parseIntegrationID(c, "id")
	if !ok {
		return
	}
	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyIntegrationFromService(item))
}

func (h *VirtualCurrencyIntegrationHandler) Create(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req createVirtualCurrencyIntegrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.service.Create(c.Request.Context(), service.VirtualCurrencyIntegrationCreateInput{
		Code: req.Code, Name: req.Name, Metadata: req.Metadata, CreatedBy: &subject.UserID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.VirtualCurrencyIntegrationSecretFromService(item))
}

func (h *VirtualCurrencyIntegrationHandler) Update(c *gin.Context) {
	id, ok := parseIntegrationID(c, "id")
	if !ok {
		return
	}
	var req updateVirtualCurrencyIntegrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.service.Update(c.Request.Context(), id, service.VirtualCurrencyIntegrationUpdateInput{Name: req.Name, Metadata: req.Metadata})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyIntegrationFromService(item))
}

func (h *VirtualCurrencyIntegrationHandler) SetStatus(c *gin.Context) {
	id, ok := parseIntegrationID(c, "id")
	if !ok {
		return
	}
	var req virtualCurrencyIntegrationStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.service.SetStatus(c.Request.Context(), id, req.Status)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyIntegrationFromService(item))
}

func (h *VirtualCurrencyIntegrationHandler) RotateSecret(c *gin.Context) {
	id, ok := parseIntegrationID(c, "id")
	if !ok {
		return
	}
	payload := struct {
		ID int64 `json:"id"`
	}{ID: id}
	executeAdminIdempotentJSON(c, "admin.virtual_currency_integrations.rotate_secret", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		item, err := h.service.RotateSecret(ctx, id)
		if err != nil {
			return nil, err
		}
		return dto.VirtualCurrencyIntegrationSecretFromService(item), nil
	})
}

func (h *VirtualCurrencyIntegrationHandler) ListScopes(c *gin.Context) {
	id, ok := parseIntegrationID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.ListScopes(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyIntegrationScopesFromService(items))
}

func (h *VirtualCurrencyIntegrationHandler) UpsertScope(c *gin.Context) {
	integrationID, ok := parseIntegrationID(c, "id")
	if !ok {
		return
	}
	currencyID, ok := parseIntegrationID(c, "currency_id")
	if !ok {
		return
	}
	groupID, ok := parseIntegrationID(c, "group_id")
	if !ok {
		return
	}
	var req virtualCurrencyIntegrationScopeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.service.UpsertScope(c.Request.Context(), service.VirtualCurrencyIntegrationScopeInput{
		IntegrationID: integrationID, CurrencyID: currencyID, GroupID: groupID,
		Enabled: req.Enabled, CanEarn: req.CanEarn, CanSpend: req.CanSpend, CanSettle: req.CanSettle, Metadata: req.Metadata,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyIntegrationScopeFromService(item))
}

func (h *VirtualCurrencyIntegrationHandler) DeleteScope(c *gin.Context) {
	integrationID, ok := parseIntegrationID(c, "id")
	if !ok {
		return
	}
	currencyID, ok := parseIntegrationID(c, "currency_id")
	if !ok {
		return
	}
	groupID, ok := parseIntegrationID(c, "group_id")
	if !ok {
		return
	}
	if err := h.service.DeleteScope(c.Request.Context(), integrationID, currencyID, groupID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "integration scope deleted"})
}

func parseIntegrationID(c *gin.Context, parameter string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param(parameter)), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid "+parameter)
		return 0, false
	}
	return id, true
}

func parseOptionalBool(value string) (bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	return parsed, err
}
