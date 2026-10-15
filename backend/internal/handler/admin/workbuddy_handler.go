package admin

import (
	"encoding/json"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) WorkBuddyStartLogin(c *gin.Context) {
	var req struct {
		Platform string `json:"platform" binding:"required,oneof=workbuddy workbuddy_ai"`
		ProxyID  *int64 `json:"proxy_id"`
	}
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "Invalid WorkBuddy platform or proxy")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Authentication required")
		return
	}
	s := h.accountTestService.WorkBuddy()
	if s == nil {
		response.InternalError(c, "WorkBuddy service is unavailable")
		return
	}
	result, err := s.StartLogin(c.Request.Context(), subject.UserID, req.Platform, req.ProxyID)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, result)
}

func (h *AccountHandler) WorkBuddyPollLogin(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Authentication required")
		return
	}
	s := h.accountTestService.WorkBuddy()
	if s == nil {
		response.InternalError(c, "WorkBuddy service is unavailable")
		return
	}
	result, err := s.PollLogin(c.Request.Context(), subject.UserID, c.Param("session"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, result)
}

func (h *AccountHandler) WorkBuddyImport(c *gin.Context) {
	var req struct {
		Data json.RawMessage `json:"data" binding:"required"`
	}
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "Invalid WorkBuddy desktop JSON")
		return
	}
	credentials, err := service.ImportWorkBuddyCredentials(req.Data)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"credentials": credentials})
}

func (h *AccountHandler) WorkBuddyUsage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}
	if !account.IsWorkBuddy() {
		response.BadRequest(c, "Account is not WorkBuddy")
		return
	}
	s := h.accountTestService.WorkBuddy()
	if s == nil {
		response.InternalError(c, "WorkBuddy service is unavailable")
		return
	}
	ctx := c.Request.Context()
	usage, err := s.Usage(ctx, account)
	if err != nil {
		response.Error(c, 502, "Failed to fetch WorkBuddy usage: "+err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, usage)
}
