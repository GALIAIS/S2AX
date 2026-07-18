package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// VirtualCurrencyHandler exposes administrator controls for configurable user assets.
type VirtualCurrencyHandler struct {
	virtualCurrencyService *service.VirtualCurrencyService
}

func NewVirtualCurrencyHandler(virtualCurrencyService *service.VirtualCurrencyService) *VirtualCurrencyHandler {
	return &VirtualCurrencyHandler{virtualCurrencyService: virtualCurrencyService}
}

type createVirtualCurrencyRequest struct {
	Code        string         `json:"code"`
	Name        string         `json:"name"`
	Symbol      string         `json:"symbol"`
	Description string         `json:"description"`
	Scale       int            `json:"scale"`
	Metadata    map[string]any `json:"metadata"`
}

type updateVirtualCurrencyRequest struct {
	Name        *string        `json:"name"`
	Symbol      *string        `json:"symbol"`
	Description *string        `json:"description"`
	Metadata    map[string]any `json:"metadata"`
}

type virtualCurrencyStatusRequest struct {
	Status string `json:"status"`
}

type virtualCurrencyPolicyRequest struct {
	Enabled         bool           `json:"enabled"`
	CanEarn         bool           `json:"can_earn"`
	CanSpend        bool           `json:"can_spend"`
	MaxBalanceUnits *int64         `json:"max_balance_units"`
	Metadata        map[string]any `json:"metadata"`
}

type virtualCurrencyAdjustmentRequest struct {
	UserID      int64          `json:"user_id"`
	GroupID     int64          `json:"group_id"`
	AmountUnits int64          `json:"amount_units"`
	EntryType   string         `json:"entry_type"`
	SourceID    string         `json:"source_id"`
	Reason      string         `json:"reason"`
	Metadata    map[string]any `json:"metadata"`
}

func parseVirtualCurrencyLimit(c *gin.Context, maximum int) (int, bool) {
	limit := 100
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maximum {
			response.BadRequest(c, fmt.Sprintf("limit must be between 1 and %d", maximum))
			return 0, false
		}
		limit = parsed
	}
	return limit, true
}

// List returns all currency definitions. Disabled definitions are included only when requested.
func (h *VirtualCurrencyHandler) List(c *gin.Context) {
	includeDisabled := false
	if raw := strings.TrimSpace(c.Query("include_disabled")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			response.BadRequest(c, "include_disabled must be a boolean")
			return
		}
		includeDisabled = parsed
	}

	items, err := h.virtualCurrencyService.ListCurrencies(c.Request.Context(), includeDisabled)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]*dto.VirtualCurrency, 0, len(items))
	for _, item := range items {
		if mapped := dto.VirtualCurrencyFromService(item); mapped != nil {
			out = append(out, mapped)
		}
	}
	response.Success(c, out)
}

func (h *VirtualCurrencyHandler) Get(c *gin.Context) {
	id, ok := parseVirtualCurrencyID(c, "currency")
	if !ok {
		return
	}
	item, err := h.virtualCurrencyService.GetCurrency(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyFromService(item))
}

func (h *VirtualCurrencyHandler) Create(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req createVirtualCurrencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.virtualCurrencyService.CreateCurrency(c.Request.Context(), service.VirtualCurrencyCreateInput{
		Code:        req.Code,
		Name:        req.Name,
		Symbol:      req.Symbol,
		Description: req.Description,
		Scale:       req.Scale,
		Metadata:    req.Metadata,
		CreatedBy:   &subject.UserID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.VirtualCurrencyFromService(item))
}

func (h *VirtualCurrencyHandler) Update(c *gin.Context) {
	id, ok := parseVirtualCurrencyID(c, "currency")
	if !ok {
		return
	}
	var req updateVirtualCurrencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.virtualCurrencyService.UpdateCurrency(c.Request.Context(), id, service.VirtualCurrencyUpdateInput{
		Name:        req.Name,
		Symbol:      req.Symbol,
		Description: req.Description,
		Metadata:    req.Metadata,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyFromService(item))
}

func (h *VirtualCurrencyHandler) SetStatus(c *gin.Context) {
	id, ok := parseVirtualCurrencyID(c, "currency")
	if !ok {
		return
	}
	var req virtualCurrencyStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.virtualCurrencyService.SetCurrencyStatus(c.Request.Context(), id, req.Status)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyFromService(item))
}

func (h *VirtualCurrencyHandler) ListGroups(c *gin.Context) {
	id, ok := parseVirtualCurrencyID(c, "currency")
	if !ok {
		return
	}
	items, err := h.virtualCurrencyService.ListGroupPolicies(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyPoliciesFromService(items))
}

func (h *VirtualCurrencyHandler) UpsertGroup(c *gin.Context) {
	currencyID, ok := parseVirtualCurrencyID(c, "currency")
	if !ok {
		return
	}
	groupID, ok := parseVirtualCurrencyID(c, "group_id")
	if !ok {
		return
	}
	var req virtualCurrencyPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.virtualCurrencyService.UpsertGroupPolicy(c.Request.Context(), service.VirtualCurrencyPolicyInput{
		CurrencyID:      currencyID,
		GroupID:         groupID,
		Enabled:         req.Enabled,
		CanEarn:         req.CanEarn,
		CanSpend:        req.CanSpend,
		MaxBalanceUnits: req.MaxBalanceUnits,
		Metadata:        req.Metadata,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyPoliciesFromService([]*service.VirtualCurrencyGroupPolicy{item})[0])
}

// EnableForAllUsers enables earning and spending on every active public
// standard group. This makes the currency available to current and future
// users without creating empty wallet rows for every account.
func (h *VirtualCurrencyHandler) EnableForAllUsers(c *gin.Context) {
	currencyID, ok := parseVirtualCurrencyID(c, "currency")
	if !ok {
		return
	}
	items, err := h.virtualCurrencyService.EnableForAllUsers(c.Request.Context(), currencyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"currency_id": currencyID,
		"group_count": len(items),
		"policies":    dto.VirtualCurrencyPoliciesFromService(items),
	})
}

func (h *VirtualCurrencyHandler) DeleteGroup(c *gin.Context) {
	currencyID, ok := parseVirtualCurrencyID(c, "currency")
	if !ok {
		return
	}
	groupID, ok := parseVirtualCurrencyID(c, "group_id")
	if !ok {
		return
	}
	if err := h.virtualCurrencyService.DeleteGroupPolicy(c.Request.Context(), currencyID, groupID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "group policy deleted"})
}

// ExpireHolds is an idempotent-safe maintenance operation intended for a
// scheduler or an administrator. It only releases active holds past expiry.
func (h *VirtualCurrencyHandler) ExpireHolds(c *gin.Context) {
	currencyID, ok := parseVirtualCurrencyID(c, "currency")
	if !ok {
		return
	}
	limit, ok := parseVirtualCurrencyLimit(c, 500)
	if !ok {
		return
	}
	expired, err := h.virtualCurrencyService.ExpireExpiredHolds(c.Request.Context(), currencyID, limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"currency_id": currencyID, "expired": expired, "limit": limit})
}

func (h *VirtualCurrencyHandler) Reconcile(c *gin.Context) {
	currencyID, ok := parseVirtualCurrencyID(c, "currency")
	if !ok {
		return
	}
	limit, ok := parseVirtualCurrencyLimit(c, 100)
	if !ok {
		return
	}
	report, err := h.virtualCurrencyService.ReconcileCurrency(c.Request.Context(), currencyID, limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, report)
}

// Adjust is the only administrator balance mutation endpoint. The source is
// deliberately fixed to admin so callers cannot forge game/mission provenance.
func (h *VirtualCurrencyHandler) Adjust(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	code := strings.TrimSpace(c.Param("currency"))
	var req virtualCurrencyAdjustmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	requestPayload := struct {
		Code string `json:"code"`
		virtualCurrencyAdjustmentRequest
	}{Code: code, virtualCurrencyAdjustmentRequest: req}
	executeAdminIdempotentJSON(c, "admin.virtual_currencies.adjust", requestPayload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		entry, err := h.virtualCurrencyService.Adjust(ctx, service.VirtualCurrencyAdjustmentInput{
			CurrencyCode:   code,
			UserID:         req.UserID,
			GroupID:        req.GroupID,
			AmountUnits:    req.AmountUnits,
			EntryType:      req.EntryType,
			SourceType:     service.VirtualCurrencySourceAdmin,
			SourceID:       req.SourceID,
			IdempotencyKey: c.GetHeader("Idempotency-Key"),
			Reason:         req.Reason,
			Metadata:       req.Metadata,
			CreatedBy:      &subject.UserID,
		})
		if err != nil {
			return nil, err
		}
		return dto.VirtualCurrencyLedgerFromService(entry), nil
	})
}

func (h *VirtualCurrencyHandler) UserLedger(c *gin.Context) {
	userID, ok := parseVirtualCurrencyID(c, "user_id")
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, paging, err := h.virtualCurrencyService.ListLedger(c.Request.Context(), service.VirtualCurrencyLedgerQuery{
		UserID:       userID,
		CurrencyCode: c.Param("currency"),
		Page:         page,
		PageSize:     pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if paging == nil {
		response.Paginated(c, dto.VirtualCurrencyLedgersFromService(items), 0, page, pageSize)
		return
	}
	response.Paginated(c, dto.VirtualCurrencyLedgersFromService(items), paging.Total, paging.Page, paging.PageSize)
}

func parseVirtualCurrencyID(c *gin.Context, parameter string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(parameter), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid "+parameter)
		return 0, false
	}
	return id, true
}
