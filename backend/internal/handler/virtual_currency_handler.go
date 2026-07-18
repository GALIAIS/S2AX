package handler

import (
	"context"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// VirtualCurrencyHandler serves the authenticated user's virtual asset wallet.
type VirtualCurrencyHandler struct {
	virtualCurrencyService *service.VirtualCurrencyService
}

func NewVirtualCurrencyHandler(virtualCurrencyService *service.VirtualCurrencyService) *VirtualCurrencyHandler {
	return &VirtualCurrencyHandler{virtualCurrencyService: virtualCurrencyService}
}

type virtualCurrencySpendRequest struct {
	GroupID  int64          `json:"group_id"`
	Amount   int64          `json:"amount_units"`
	SourceID string         `json:"source_id"`
	Reason   string         `json:"reason"`
	Metadata map[string]any `json:"metadata"`
}

type virtualCurrencyReserveRequest struct {
	GroupID     int64          `json:"group_id"`
	AmountUnits int64          `json:"amount_units"`
	ExpiresAt   *time.Time     `json:"expires_at"`
	SourceID    string         `json:"source_id"`
	Reason      string         `json:"reason"`
	Metadata    map[string]any `json:"metadata"`
}

type virtualCurrencyHoldActionRequest struct {
	SourceID string         `json:"source_id"`
	Reason   string         `json:"reason"`
	Metadata map[string]any `json:"metadata"`
}

func (h *VirtualCurrencyHandler) ListWallets(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	items, err := h.virtualCurrencyService.ListUserWallets(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyWalletsFromService(items))
}

func (h *VirtualCurrencyHandler) ListLedger(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, paging, err := h.virtualCurrencyService.ListLedger(c.Request.Context(), service.VirtualCurrencyLedgerQuery{
		UserID:       subject.UserID,
		CurrencyCode: strings.TrimSpace(c.Param("code")),
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

func (h *VirtualCurrencyHandler) ListHolds(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, paging, err := h.virtualCurrencyService.ListHolds(c.Request.Context(), service.VirtualCurrencyHoldQuery{
		UserID:       subject.UserID,
		CurrencyCode: c.Query("currency_code"),
		Status:       c.Query("status"),
		Page:         page,
		PageSize:     pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if paging == nil {
		response.Paginated(c, dto.VirtualCurrencyHoldsFromService(items), 0, page, pageSize)
		return
	}
	response.Paginated(c, dto.VirtualCurrencyHoldsFromService(items), paging.Total, paging.Page, paging.PageSize)
}

func (h *VirtualCurrencyHandler) Reserve(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req virtualCurrencyReserveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	code := strings.TrimSpace(c.Param("code"))
	expiresAt := time.Time{}
	if req.ExpiresAt != nil {
		expiresAt = req.ExpiresAt.UTC()
	}
	payload := struct {
		Code string `json:"code"`
		virtualCurrencyReserveRequest
	}{Code: code, virtualCurrencyReserveRequest: req}
	executeUserIdempotentJSON(c, "user.virtual_currencies.holds.reserve", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		result, err := h.virtualCurrencyService.ReserveHold(ctx, service.VirtualCurrencyReserveInput{
			CurrencyCode:   code,
			UserID:         subject.UserID,
			GroupID:        req.GroupID,
			AmountUnits:    req.AmountUnits,
			ExpiresAt:      expiresAt,
			SourceType:     service.VirtualCurrencySourceAPI,
			SourceID:       req.SourceID,
			IdempotencyKey: c.GetHeader("Idempotency-Key"),
			Reason:         req.Reason,
			Metadata:       req.Metadata,
		})
		if err != nil {
			return nil, err
		}
		return dto.VirtualCurrencyHoldResultFromService(result), nil
	})
}

func (h *VirtualCurrencyHandler) CommitHold(c *gin.Context) {
	h.settleHold(c, true)
}

func (h *VirtualCurrencyHandler) ReleaseHold(c *gin.Context) {
	h.settleHold(c, false)
}

func (h *VirtualCurrencyHandler) settleHold(c *gin.Context, commit bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	holdID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || holdID <= 0 {
		response.BadRequest(c, "invalid hold id")
		return
	}
	var req virtualCurrencyHoldActionRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
			response.BadRequest(c, "Invalid request: "+err.Error())
			return
		}
	}
	payload := struct {
		HoldID int64 `json:"hold_id"`
		virtualCurrencyHoldActionRequest
	}{HoldID: holdID, virtualCurrencyHoldActionRequest: req}
	scope := "user.virtual_currencies.holds.release"
	if commit {
		scope = "user.virtual_currencies.holds.commit"
	}
	executeUserIdempotentJSON(c, scope, payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		input := service.VirtualCurrencyHoldSettlementInput{
			HoldID:         holdID,
			UserID:         subject.UserID,
			SourceType:     service.VirtualCurrencySourceAPI,
			SourceID:       req.SourceID,
			IdempotencyKey: c.GetHeader("Idempotency-Key"),
			Reason:         req.Reason,
			Metadata:       req.Metadata,
		}
		var result *service.VirtualCurrencyHoldResult
		var err error
		if commit {
			result, err = h.virtualCurrencyService.CommitHold(ctx, input)
		} else {
			result, err = h.virtualCurrencyService.ReleaseHold(ctx, input)
		}
		if err != nil {
			return nil, err
		}
		return dto.VirtualCurrencyHoldResultFromService(result), nil
	})
}

// Spend uses a server-owned source type. Game and mission adapters should use
// a signed integration endpoint rather than allowing a browser to impersonate them.
func (h *VirtualCurrencyHandler) Spend(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	code := strings.TrimSpace(c.Param("code"))
	var req virtualCurrencySpendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	payload := struct {
		Code string `json:"code"`
		virtualCurrencySpendRequest
	}{Code: code, virtualCurrencySpendRequest: req}
	executeUserIdempotentJSON(c, "user.virtual_currencies.spend", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		entry, err := h.virtualCurrencyService.Spend(ctx, service.VirtualCurrencySpendInput{
			CurrencyCode:   code,
			UserID:         subject.UserID,
			GroupID:        req.GroupID,
			AmountUnits:    req.Amount,
			SourceType:     service.VirtualCurrencySourceAPI,
			SourceID:       req.SourceID,
			IdempotencyKey: c.GetHeader("Idempotency-Key"),
			Reason:         req.Reason,
			Metadata:       req.Metadata,
		})
		if err != nil {
			return nil, err
		}
		return dto.VirtualCurrencyLedgerFromService(entry), nil
	})
}
