package securityaudit

import (
	"errors"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func (h *PromptAdminHandler) coreService(c *gin.Context) (SecurityAuditCoreService, bool) {
	if h == nil || h.service == nil {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("security_audit_unavailable", "安全审计服务不可用"))
		return nil, false
	}
	service, ok := h.service.(SecurityAuditCoreService)
	if !ok {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("security_audit_unavailable", "安全审计核心尚未启用"))
		return nil, false
	}
	return service, true
}

func (h *PromptAdminHandler) SecurityOverview(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	window, err := positiveInt64Query(c, "window_hours", 24, 24*90)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := service.SecurityOverview(c.Request.Context(), window)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) ListPolicies(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	result, err := service.ListPolicies(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) CreatePolicy(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	var request CreatePolicyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		coreMutationError(c, "security_audit_policy_create", "security_audit_invalid_policy", "策略请求无效", nil)
		return
	}
	result, err := service.CreatePolicy(c.Request.Context(), request, adminID(c))
	if err != nil {
		coreMutationServiceError(c, "security_audit_policy_create", err, map[string]any{"policy_key": request.PolicyKey})
		return
	}
	coreMutationSuccess(c, "security_audit_policy_create", map[string]any{
		"policy_key": result.PolicyKey, "version": result.Version, "config_digest": result.ConfigDigest,
	})
	response.Success(c, result)
}

func (h *PromptAdminHandler) ListPolicyVersions(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	result, err := service.ListPolicyVersions(c.Request.Context(), c.Param("key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) ListPolicyTransitions(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	limit, err := positiveIntQuery(c, "limit", 100, 500)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := service.ListPolicyTransitions(c.Request.Context(), c.Param("key"), limit)
	if err != nil {
		response.ErrorFrom(c, mapCoreError(err))
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) ListPolicyShadowEvaluations(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	version, err := pathID(c, "version")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	windowHours, err := positiveInt64Query(c, "window_hours", 24*7, 24*90)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	limit, err := positiveIntQuery(c, "limit", 50, 200)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := service.ListPolicyShadowEvaluations(
		c.Request.Context(),
		c.Param("key"),
		version,
		windowHours,
		limit,
	)
	if err != nil {
		response.ErrorFrom(c, mapCoreError(err))
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) ValidatePolicy(c *gin.Context) {
	h.policyTransition(c, "validate")
}

func (h *PromptAdminHandler) ShadowPolicy(c *gin.Context) {
	h.policyTransition(c, "shadow")
}

func (h *PromptAdminHandler) ActivatePolicy(c *gin.Context) {
	h.policyTransition(c, "activate")
}

func (h *PromptAdminHandler) RollbackPolicy(c *gin.Context) {
	h.policyTransition(c, "rollback")
}

type transitionReasonRequest struct {
	Reason string `json:"reason"`
}

func (h *PromptAdminHandler) policyTransition(c *gin.Context, transition string) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	version, err := pathID(c, "version")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var request transitionReasonRequest
	_ = c.ShouldBindJSON(&request)
	key := c.Param("key")
	var result *PolicyVersion
	switch transition {
	case "validate":
		result, err = service.ValidatePolicy(c.Request.Context(), key, version, adminID(c))
	case "shadow":
		result, err = service.ShadowPolicy(c.Request.Context(), key, version, adminID(c), request.Reason)
	case "activate":
		result, err = service.ActivatePolicy(c.Request.Context(), key, version, adminID(c), request.Reason)
	case "rollback":
		result, err = service.RollbackPolicy(c.Request.Context(), key, version, adminID(c), request.Reason)
	}
	action := "security_audit_policy_" + transition
	if err != nil {
		coreMutationServiceError(c, action, err, map[string]any{"policy_key": key, "version": version})
		return
	}
	coreMutationSuccess(c, action, map[string]any{
		"policy_key": key, "version": version, "status": result.Status, "config_digest": result.ConfigDigest,
	})
	response.Success(c, result)
}

func (h *PromptAdminHandler) SimulatePolicy(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	version, err := pathID(c, "version")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var request PolicySimulationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_invalid_simulation", "策略模拟请求无效"))
		return
	}
	result, err := service.SimulatePolicy(c.Request.Context(), c.Param("key"), version, request)
	if err != nil {
		response.ErrorFrom(c, mapCoreError(err))
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) ReplayPolicy(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	policyKey := strings.TrimSpace(c.Param("key"))
	version, err := pathID(c, "version")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var request PolicyReplayRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_replay_invalid", "策略回放参数无效"))
		return
	}
	if request.WindowHours < 1 || request.WindowHours > 24*90 || request.Limit < 1 || request.Limit > 5000 {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_replay_invalid", "回放窗口或样本上限超出允许范围"))
		return
	}
	result, err := service.ReplayPolicy(c.Request.Context(), policyKey, version, request)
	if err != nil {
		response.ErrorFrom(c, mapCoreError(err))
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) ListUnifiedDecisions(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	page, err := positiveIntQuery(c, "page", 1, 0)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	pageSize, err := positiveIntQuery(c, "page_size", 20, 100)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	filter, err := decisionFilterFromQuery(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := service.ListUnifiedDecisions(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) GetUnifiedDecision(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	id, err := pathID(c, "id")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := service.GetUnifiedDecision(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, mapCoreError(err))
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) RevealUnifiedEvidence(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	id, err := pathID(c, "id")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var request evidenceRevealRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_evidence_reason_invalid", "必须填写 3-256 字的查看理由"))
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if n := len([]rune(request.Reason)); n < 3 || n > 256 {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_evidence_reason_invalid", "必须填写 3-256 字的查看理由"))
		return
	}
	result, err := service.RevealUnifiedEvidence(c.Request.Context(), id, adminID(c), request.Reason)
	if err != nil {
		response.ErrorFrom(c, mapCoreError(err))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	coreMutationSuccess(c, "security_audit_evidence_reveal", map[string]any{"decision_id": id, "reason_length": len([]rune(request.Reason))})
	response.Success(c, result)
}

type openCaseRequest struct {
	Reason string `json:"reason"`
}

func (h *PromptAdminHandler) OpenDecisionCase(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	id, err := pathID(c, "id")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var request openCaseRequest
	if err := c.ShouldBindJSON(&request); err != nil || len([]rune(strings.TrimSpace(request.Reason))) < 3 {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_case_reason_invalid", "开案原因至少 3 个字符"))
		return
	}
	result, err := service.OpenDecisionCase(c.Request.Context(), id, adminID(c), request.Reason)
	if err != nil {
		coreMutationServiceError(c, "security_audit_case_open", err, map[string]any{"decision_id": id})
		return
	}
	coreMutationSuccess(c, "security_audit_case_open", map[string]any{"decision_id": id, "case_id": result.CaseID})
	response.Success(c, result)
}

func (h *PromptAdminHandler) AddDecisionFeedback(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	id, err := pathID(c, "id")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var request FeedbackRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_feedback_invalid", "反馈请求无效"))
		return
	}
	result, err := service.AddDecisionFeedback(c.Request.Context(), id, adminID(c), request)
	if err != nil {
		coreMutationServiceError(c, "security_audit_feedback_create", err, map[string]any{"decision_id": id})
		return
	}
	coreMutationSuccess(c, "security_audit_feedback_create", map[string]any{"decision_id": id, "conclusion": request.Conclusion})
	response.Success(c, result)
}

func (h *PromptAdminHandler) ListActions(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	page, err := positiveIntQuery(c, "page", 1, 0)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	pageSize, err := positiveIntQuery(c, "page_size", 20, 100)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	filter := ActionFilter{
		Status: c.Query("status"), ActionType: c.Query("action_type"),
		SubjectType: c.Query("subject_type"), DecisionID: c.Query("decision_id"),
	}
	if value := c.Query("subject_id"); value != "" {
		id, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil || id <= 0 {
			response.ErrorFrom(c, infraerrors.BadRequest("security_audit_invalid_subject_id", "主体 ID 无效"))
			return
		}
		filter.SubjectID = &id
	}
	result, err := service.ListActions(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) RetryAction(c *gin.Context)  { h.actionTransition(c, "retry") }
func (h *PromptAdminHandler) CancelAction(c *gin.Context) { h.actionTransition(c, "cancel") }
func (h *PromptAdminHandler) RevertAction(c *gin.Context) { h.actionTransition(c, "revert") }

func (h *PromptAdminHandler) actionTransition(c *gin.Context, transition string) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	id, err := pathID(c, "id")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var result *EnforcementAction
	switch transition {
	case "retry":
		result, err = service.RetryAction(c.Request.Context(), id, adminID(c))
	case "cancel":
		result, err = service.CancelAction(c.Request.Context(), id, adminID(c))
	case "revert":
		result, err = service.RevertAction(c.Request.Context(), id, adminID(c))
	}
	action := "security_audit_action_" + transition
	if err != nil {
		coreMutationServiceError(c, action, err, map[string]any{"action_id": id})
		return
	}
	coreMutationSuccess(c, action, map[string]any{"action_id": id, "status": result.Status, "action_type": result.ActionType})
	response.Success(c, result)
}

func (h *PromptAdminHandler) ListCases(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	page, err := positiveIntQuery(c, "page", 1, 0)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	pageSize, err := positiveIntQuery(c, "page_size", 20, 100)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	filter := CaseFilter{Status: c.Query("status"), Severity: c.Query("severity"), Keyword: c.Query("keyword")}
	if value := c.Query("assignee_id"); value != "" {
		id, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil || id <= 0 {
			response.ErrorFrom(c, infraerrors.BadRequest("security_audit_invalid_assignee", "负责人 ID 无效"))
			return
		}
		filter.AssigneeID = &id
	}
	result, err := service.ListCases(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) GetCase(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	id, err := pathID(c, "id")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := service.GetCase(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, mapCoreError(err))
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) TransitionCase(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	id, err := pathID(c, "id")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var request CaseTransitionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_case_transition_invalid", "案件状态请求无效"))
		return
	}
	result, err := service.TransitionCase(c.Request.Context(), id, adminID(c), request)
	if err != nil {
		coreMutationServiceError(c, "security_audit_case_transition", err, map[string]any{"case_id": id})
		return
	}
	coreMutationSuccess(c, "security_audit_case_transition", map[string]any{"case_id": id, "status": result.Status})
	response.Success(c, result)
}

func (h *PromptAdminHandler) ListExceptions(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	result, err := service.ListExceptions(c.Request.Context(), c.Query("include_inactive") == "true")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) CreateException(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	var request CreateExceptionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_exception_invalid", "例外请求无效"))
		return
	}
	result, err := service.CreateException(c.Request.Context(), request, adminID(c))
	if err != nil {
		coreMutationServiceError(c, "security_audit_exception_create", err, nil)
		return
	}
	coreMutationSuccess(c, "security_audit_exception_create", map[string]any{"exception_id": result.ExceptionID, "scope_type": result.ScopeType})
	response.Success(c, result)
}

func (h *PromptAdminHandler) ExpireException(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	id, err := pathID(c, "id")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var request ExpireExceptionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		coreMutationError(c, "security_audit_exception_expire", "security_audit_exception_expire_invalid", "例外失效请求无效", map[string]any{"exception_id": id})
		return
	}
	result, err := service.ExpireException(c.Request.Context(), id, adminID(c), request)
	if err != nil {
		coreMutationServiceError(c, "security_audit_exception_expire", err, map[string]any{"exception_id": id})
		return
	}
	coreMutationSuccess(c, "security_audit_exception_expire", map[string]any{"exception_id": id})
	response.Success(c, result)
}

func (h *PromptAdminHandler) ListEndpointHealth(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	result, err := service.ListEndpointHealth(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) ResetEndpointBreaker(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	result, err := service.ResetEndpointBreaker(c.Request.Context(), c.Param("id"))
	if err != nil {
		coreMutationServiceError(c, "security_audit_endpoint_reset", err, map[string]any{"endpoint_id": c.Param("id")})
		return
	}
	coreMutationSuccess(c, "security_audit_endpoint_reset", map[string]any{"endpoint_id": result.EndpointID})
	response.Success(c, result)
}

func (h *PromptAdminHandler) ListBehaviorSignals(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	page, err := positiveIntQuery(c, "page", 1, 0)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	pageSize, err := positiveIntQuery(c, "page_size", 20, 100)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	filter := BehaviorSignalFilter{
		SubjectType: c.Query("subject_type"),
		MatchedOnly: c.Query("matched_only") == "true",
	}
	if value := c.Query("subject_id"); value != "" {
		id, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil || id <= 0 {
			response.ErrorFrom(c, infraerrors.BadRequest("security_audit_invalid_subject_id", "主体 ID 无效"))
			return
		}
		filter.SubjectID = &id
	}
	for name, target := range map[string]**time.Time{"start_at": &filter.StartAt, "end_at": &filter.EndAt} {
		raw := c.Query(name)
		if raw == "" {
			continue
		}
		value, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("security_audit_invalid_time", "筛选时间无效"))
			return
		}
		*target = &value
	}
	result, err := service.ListBehaviorSignals(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) ListSecurityAuditNotifications(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	limit, err := positiveIntQuery(c, "limit", 50, 200)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" && !validNotificationStatus(status) {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_notification_status_invalid", "通知状态筛选无效"))
		return
	}
	result, err := service.ListSecurityAuditNotifications(
		c.Request.Context(), c.Query("status"), c.Query("audience"), limit,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

type notificationStatusRequest struct {
	Status string `json:"status"`
}

func (h *PromptAdminHandler) UpdateSecurityAuditNotificationStatus(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	id, err := pathID(c, "id")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var request notificationStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_notification_status_invalid", "通知状态请求无效"))
		return
	}
	if !validNotificationStatus(strings.TrimSpace(request.Status)) {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_notification_status_invalid", "通知状态请求无效"))
		return
	}
	result, err := service.UpdateSecurityAuditNotificationStatus(c.Request.Context(), id, request.Status)
	if err != nil {
		coreMutationServiceError(c, "security_audit_notification_update", err, map[string]any{"notification_id": id})
		return
	}
	coreMutationSuccess(c, "security_audit_notification_update", map[string]any{
		"notification_id": id, "status": result.Status,
	})
	response.Success(c, result)
}

type markAllNotificationReadRequest struct {
	Audience string `json:"audience"`
}

func (h *PromptAdminHandler) MarkAllSecurityAuditNotificationsRead(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	var request markAllNotificationReadRequest
	_ = c.ShouldBindJSON(&request)
	request.Audience = strings.TrimSpace(request.Audience)
	if request.Audience == "" {
		request.Audience = "admin"
	}
	if request.Audience != "admin" {
		response.ErrorFrom(c, infraerrors.BadRequest(
			"security_audit_notification_audience_invalid",
			"管理员通知接口不能修改用户通知状态",
		))
		return
	}
	count, err := service.MarkAllSecurityAuditNotificationsRead(c.Request.Context(), request.Audience)
	if err != nil {
		coreMutationServiceError(c, "security_audit_notification_read_all", err, map[string]any{"audience": request.Audience})
		return
	}
	coreMutationSuccess(c, "security_audit_notification_read_all", map[string]any{
		"audience": request.Audience, "updated_count": count,
	})
	response.Success(c, map[string]any{"updated_count": count})
}

func (h *PromptAdminHandler) ListMySecurityAuditNotifications(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	subject, authenticated := middleware.GetAuthSubjectFromContext(c)
	if !authenticated || subject.UserID <= 0 {
		response.ErrorFrom(c, infraerrors.Unauthorized("UNAUTHORIZED", "需要登录后查看安全通知"))
		return
	}
	limit, err := positiveIntQuery(c, "limit", 50, 100)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" && !validNotificationStatus(status) {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_notification_status_invalid", "通知状态筛选无效"))
		return
	}
	result, err := service.ListUserSecurityAuditNotifications(
		c.Request.Context(), subject.UserID, c.Query("status"), limit,
	)
	if err != nil {
		response.ErrorFrom(c, mapCoreError(err))
		return
	}
	response.Success(c, result)
}

func (h *PromptAdminHandler) UpdateMySecurityAuditNotificationStatus(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	subject, authenticated := middleware.GetAuthSubjectFromContext(c)
	if !authenticated || subject.UserID <= 0 {
		response.ErrorFrom(c, infraerrors.Unauthorized("UNAUTHORIZED", "需要登录后更新安全通知"))
		return
	}
	id, err := pathID(c, "id")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var request notificationStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_notification_status_invalid", "通知状态请求无效"))
		return
	}
	if !validNotificationStatus(strings.TrimSpace(request.Status)) {
		response.ErrorFrom(c, infraerrors.BadRequest("security_audit_notification_status_invalid", "通知状态请求无效"))
		return
	}
	result, err := service.UpdateUserSecurityAuditNotificationStatus(
		c.Request.Context(), subject.UserID, id, request.Status,
	)
	if err != nil {
		response.ErrorFrom(c, mapCoreError(err))
		return
	}
	middleware.SetAuditAction(c, "user.security.audit.notification.update")
	c.Set("audit_details", map[string]any{
		"notification_id": id,
		"status":          result.Status,
	})
	response.Success(c, result)
}

func (h *PromptAdminHandler) MarkAllMySecurityAuditNotificationsRead(c *gin.Context) {
	service, ok := h.coreService(c)
	if !ok {
		return
	}
	subject, authenticated := middleware.GetAuthSubjectFromContext(c)
	if !authenticated || subject.UserID <= 0 {
		response.ErrorFrom(c, infraerrors.Unauthorized("UNAUTHORIZED", "需要登录后更新安全通知"))
		return
	}
	count, err := service.MarkAllUserSecurityAuditNotificationsRead(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, mapCoreError(err))
		return
	}
	middleware.SetAuditAction(c, "user.security.audit.notification.read.all")
	c.Set("audit_details", map[string]any{"updated_count": count})
	response.Success(c, map[string]any{"updated_count": count})
}

func pathID(c *gin.Context, name string) (int64, error) {
	value, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || value <= 0 {
		return 0, infraerrors.BadRequest("security_audit_invalid_id", "ID 无效")
	}
	return value, nil
}

func positiveInt64Query(c *gin.Context, name string, fallback, maximum int64) (int64, error) {
	raw := c.Query(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 || (maximum > 0 && value > maximum) {
		return 0, infraerrors.BadRequest("security_audit_invalid_query", "查询参数无效")
	}
	return value, nil
}

func decisionFilterFromQuery(c *gin.Context) (DecisionFilter, error) {
	filter := DecisionFilter{
		RiskLevel: c.Query("risk_level"), RequestAction: c.Query("request_action"),
		EvaluationStatus: c.Query("evaluation_status"), SourceType: c.Query("source_type"),
		PolicyKey: c.Query("policy_key"), Keyword: c.Query("keyword"),
	}
	for name, target := range map[string]**int64{
		"user_id": &filter.UserID, "api_key_id": &filter.APIKeyID, "group_id": &filter.GroupID,
	} {
		raw := c.Query(name)
		if raw == "" {
			continue
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return DecisionFilter{}, infraerrors.BadRequest("security_audit_invalid_filter", "筛选 ID 无效")
		}
		*target = &value
	}
	for name, target := range map[string]**time.Time{"start_at": &filter.StartAt, "end_at": &filter.EndAt} {
		raw := c.Query(name)
		if raw == "" {
			continue
		}
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return DecisionFilter{}, infraerrors.BadRequest("security_audit_invalid_time", "筛选时间无效")
		}
		*target = &value
	}
	return filter, nil
}

func mapCoreError(err error) error {
	switch {
	case errors.Is(err, ErrPolicyNotFound):
		return infraerrors.NotFound("security_audit_policy_not_found", "策略版本不存在")
	case errors.Is(err, ErrDecisionNotFound):
		return infraerrors.NotFound("security_audit_decision_not_found", "安全审计判定不存在")
	case errors.Is(err, ErrActionNotFound):
		return infraerrors.NotFound("security_audit_action_not_found", "处置动作不存在")
	case errors.Is(err, ErrCaseNotFound):
		return infraerrors.NotFound("security_audit_case_not_found", "安全审计案件不存在")
	case errors.Is(err, ErrExceptionNotFound):
		return infraerrors.NotFound("security_audit_exception_not_found", "安全审计例外不存在")
	case errors.Is(err, ErrNotificationNotFound):
		return infraerrors.NotFound("security_audit_notification_not_found", "安全审计通知不存在")
	case errors.Is(err, ErrInvalidTransition):
		return infraerrors.Conflict("security_audit_invalid_transition", "当前状态不允许执行此操作")
	case errors.Is(err, ErrPolicyReasonInvalid):
		return infraerrors.BadRequest("security_audit_policy_reason_invalid", "影子、发布和回滚必须填写 3-512 字的变更原因")
	case errors.Is(err, ErrExceptionReasonInvalid):
		return infraerrors.BadRequest("security_audit_exception_reason_invalid", "例外失效必须填写 3-512 字的原因")
	case errors.Is(err, ErrEvidenceExpired):
		return infraerrors.Conflict("security_audit_evidence_expired", "审计原文已到期销毁")
	case errors.Is(err, ErrEvidenceUnavailable):
		return infraerrors.Conflict("security_audit_evidence_unavailable", "本判定没有可查看的审计原文")
	case strings.Contains(err.Error(), "策略校验失败"), strings.Contains(err.Error(), "目标历史版本未通过当前校验"):
		return infraerrors.BadRequest("security_audit_policy_validation_failed", err.Error())
	default:
		return infraerrors.InternalServer("security_audit_internal_error", "安全审计请求处理失败")
	}
}

func coreMutationError(c *gin.Context, action, code, message string, fields map[string]any) {
	middleware.SetAuditAction(c, "admin."+strings.ReplaceAll(action, "_", "."))
	setPromptAdminAudit(c, "failed", code, fields)
	response.ErrorFrom(c, infraerrors.BadRequest(code, message))
}

func coreMutationServiceError(c *gin.Context, action string, err error, fields map[string]any) {
	middleware.SetAuditAction(c, "admin."+strings.ReplaceAll(action, "_", "."))
	if fields == nil {
		fields = map[string]any{}
	}
	fields["operation"] = action
	setPromptAdminAudit(c, "failed", infraerrors.Reason(mapCoreError(err)), fields)
	response.ErrorFrom(c, mapCoreError(err))
}

func coreMutationSuccess(c *gin.Context, action string, fields map[string]any) {
	middleware.SetAuditAction(c, "admin."+strings.ReplaceAll(action, "_", "."))
	if fields == nil {
		fields = map[string]any{}
	}
	fields["operation"] = action
	setPromptAdminAudit(c, "success", "", fields)
}
