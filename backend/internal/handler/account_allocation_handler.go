package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AccountAllocationHandler exposes the deliberately limited user-facing
// allocation view. It never accepts a user ID from the request.
type AccountAllocationHandler struct {
	service *service.AccountAllocationService
}

func NewAccountAllocationHandler(service *service.AccountAllocationService) *AccountAllocationHandler {
	return &AccountAllocationHandler{service: service}
}

// ListMine returns only the authenticated user's safe allocation summaries.
// GET /api/v1/account-allocations
func (h *AccountAllocationHandler) ListMine(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h == nil || h.service == nil {
		response.Success(c, gin.H{"assignments": []service.AccountAllocationUserAssignment{}})
		return
	}
	items, err := h.service.ListUserAssignments(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"assignments": items})
}

// ListVisible returns the current user's safe account directory. It includes
// public group accounts and the caller's eligible dedicated allocations, but
// deliberately exposes no account controls or sensitive account metadata.
// GET /api/v1/account-allocations/visible
func (h *AccountAllocationHandler) ListVisible(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h == nil || h.service == nil {
		response.Success(c, service.AccountAllocationVisibleOverview{
			Items: []service.AccountAllocationVisibleAccount{},
		})
		return
	}
	overview, err := h.service.ListUserVisibleAccounts(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, overview)
}
