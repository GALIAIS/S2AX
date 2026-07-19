package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AccountAllocationHandler manages the account-lease control plane. It is
// intentionally separate from raw account management so policy operations do
// not need to expose credentials or transport details.
type AccountAllocationHandler struct {
	service *service.AccountAllocationService
}

func NewAccountAllocationHandler(service *service.AccountAllocationService) *AccountAllocationHandler {
	return &AccountAllocationHandler{service: service}
}

type createAccountAllocationPolicyRequest struct {
	UserID        int64 `json:"user_id" binding:"required,gt=0"`
	GroupID       int64 `json:"group_id" binding:"required,gt=0"`
	DesiredCount  int   `json:"desired_count" binding:"gte=0"`
	AutoReplenish bool  `json:"auto_replenish"`
	ReplaceOn401  bool  `json:"replace_on_401"`
	ReplaceOn429  bool  `json:"replace_on_429"`
}

type updateAccountAllocationPolicyRequest struct {
	DesiredCount  int  `json:"desired_count" binding:"gte=0"`
	AutoReplenish bool `json:"auto_replenish"`
	ReplaceOn401  bool `json:"replace_on_401"`
	ReplaceOn429  bool `json:"replace_on_429"`
}

type setAccountAllocationPolicyStatusRequest struct {
	Enabled bool `json:"enabled"`
}

type createAccountAllocationAssignmentRequest struct {
	AccountID int64 `json:"account_id" binding:"required,gt=0"`
}

// GetCapabilities GET /api/v1/admin/account-allocations/capabilities
// Returns only server-enforced limits, never account pool data.
func (h *AccountAllocationHandler) GetCapabilities(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	response.Success(c, h.service.Capabilities())
}

// ListPolicies GET /api/v1/admin/account-allocations/policies
func (h *AccountAllocationHandler) ListPolicies(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	page, pageSize := response.ParsePagination(c)
	filter := service.AccountAllocationPolicyFilter{Status: strings.TrimSpace(c.Query("status"))}
	if value, ok := parseOptionalPositiveQueryID(c, "user_id"); ok {
		filter.UserID = &value
	} else if strings.TrimSpace(c.Query("user_id")) != "" {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	if value, ok := parseOptionalPositiveQueryID(c, "group_id"); ok {
		filter.GroupID = &value
	} else if strings.TrimSpace(c.Query("group_id")) != "" {
		response.BadRequest(c, "Invalid group_id")
		return
	}
	items, total, err := h.service.ListPolicies(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

// CreatePolicy POST /api/v1/admin/account-allocations/policies
func (h *AccountAllocationHandler) CreatePolicy(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	var req createAccountAllocationPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid account allocation policy request")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	policy, err := h.service.CreatePolicy(c.Request.Context(), service.AccountAllocationPolicyInput{
		UserID:        req.UserID,
		GroupID:       req.GroupID,
		DesiredCount:  req.DesiredCount,
		AutoReplenish: req.AutoReplenish,
		ReplaceOn401:  req.ReplaceOn401,
		ReplaceOn429:  req.ReplaceOn429,
		ActorUserID:   subject.UserID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, policy)
}

// GetPolicy GET /api/v1/admin/account-allocations/policies/:id
func (h *AccountAllocationHandler) GetPolicy(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	policyID, ok := parsePositivePathID(c, "id")
	if !ok {
		return
	}
	policy, err := h.service.GetPolicy(c.Request.Context(), policyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

// UpdatePolicy PUT /api/v1/admin/account-allocations/policies/:id
func (h *AccountAllocationHandler) UpdatePolicy(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	policyID, ok := parsePositivePathID(c, "id")
	if !ok {
		return
	}
	var req updateAccountAllocationPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid account allocation policy request")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	policy, err := h.service.UpdatePolicy(c.Request.Context(), policyID, service.AccountAllocationPolicyUpdate{
		DesiredCount:  req.DesiredCount,
		AutoReplenish: req.AutoReplenish,
		ReplaceOn401:  req.ReplaceOn401,
		ReplaceOn429:  req.ReplaceOn429,
		ActorUserID:   subject.UserID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

// DeletePolicy DELETE /api/v1/admin/account-allocations/policies/:id
func (h *AccountAllocationHandler) DeletePolicy(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	policyID, ok := parsePositivePathID(c, "id")
	if !ok {
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if err := h.service.DeletePolicy(c.Request.Context(), policyID, subject.UserID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "deleted"})
}

// SetPolicyStatus POST /api/v1/admin/account-allocations/policies/:id/status
func (h *AccountAllocationHandler) SetPolicyStatus(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	policyID, ok := parsePositivePathID(c, "id")
	if !ok {
		return
	}
	var req setAccountAllocationPolicyStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid account allocation policy status request")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	policy, err := h.service.SetPolicyStatus(c.Request.Context(), policyID, req.Enabled, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

// ReconcilePolicy POST /api/v1/admin/account-allocations/policies/:id/reconcile
func (h *AccountAllocationHandler) ReconcilePolicy(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	policyID, ok := parsePositivePathID(c, "id")
	if !ok {
		return
	}
	result, err := h.service.ReconcilePolicy(c.Request.Context(), policyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ListAssignments GET /api/v1/admin/account-allocations/policies/:id/assignments
func (h *AccountAllocationHandler) ListAssignments(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	policyID, ok := parsePositivePathID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.ListAssignments(c.Request.Context(), policyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": items})
}

// ListCandidates GET /api/v1/admin/account-allocations/policies/:id/candidates
func (h *AccountAllocationHandler) ListCandidates(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	policyID, ok := parsePositivePathID(c, "id")
	if !ok {
		return
	}
	limit := 100
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			response.BadRequest(c, "Invalid limit")
			return
		}
		limit = value
	}
	items, err := h.service.ListManualCandidates(c.Request.Context(), policyID, c.Query("q"), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": items})
}

// CreateAssignment POST /api/v1/admin/account-allocations/policies/:id/assignments
func (h *AccountAllocationHandler) CreateAssignment(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	policyID, ok := parsePositivePathID(c, "id")
	if !ok {
		return
	}
	var req createAccountAllocationAssignmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid account allocation assignment request")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	assignment, err := h.service.AssignManual(c.Request.Context(), policyID, req.AccountID, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, assignment)
}

// ReleaseAssignment DELETE /api/v1/admin/account-allocations/policies/:id/assignments/:assignment_id
func (h *AccountAllocationHandler) ReleaseAssignment(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	policyID, ok := parsePositivePathID(c, "id")
	if !ok {
		return
	}
	assignmentID, ok := parsePositivePathID(c, "assignment_id")
	if !ok {
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if err := h.service.ReleaseAssignment(c.Request.Context(), policyID, assignmentID, subject.UserID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "released"})
}

// ListEvents GET /api/v1/admin/account-allocations/policies/:id/events
func (h *AccountAllocationHandler) ListEvents(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Account allocation service unavailable")
		return
	}
	policyID, ok := parsePositivePathID(c, "id")
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.service.ListEvents(c.Request.Context(), policyID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func parsePositivePathID(c *gin.Context, key string) (int64, bool) {
	value, err := strconv.ParseInt(c.Param(key), 10, 64)
	if err != nil || value <= 0 {
		response.BadRequest(c, "Invalid "+key)
		return 0, false
	}
	return value, true
}

func parseOptionalPositiveQueryID(c *gin.Context, key string) (int64, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}
